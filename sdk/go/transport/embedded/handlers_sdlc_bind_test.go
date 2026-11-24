// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package embedded

import (
	"context"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	pkgsdlc "github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/giteafake"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

type sdlcFacade struct {
	service.QuicBaaS
	svc *service.SDLCService
}

func (f sdlcFacade) SDLC() *service.SDLCService { return f.svc }

type sdlcBindResolver struct {
	mu     sync.Mutex
	stores map[string]qrdbsdk.KVStore
}

func (r *sdlcBindResolver) Resolve(_ context.Context, tenantID string) (qrdbsdk.KVStore, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	store, ok := r.stores[tenantID]
	if !ok {
		store = qrdbsdk.NewMemoryKVStore()
		r.stores[tenantID] = store
	}
	return store, nil
}

// sdlcBindProjects holds the one project and records what a bind writes.
type sdlcBindProjects struct {
	service.ProjectService
	mu      sync.Mutex
	project *orchestration.Project
}

func (p *sdlcBindProjects) GetProject(_ context.Context, req *service.GetProjectRequest) (*service.GetProjectResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if req.ProjectID != p.project.ID {
		return nil, &service.ProjectNotFoundError{TenantID: req.TenantID, ProjectID: req.ProjectID}
	}
	return &service.GetProjectResponse{Project: p.project}, nil
}

func (p *sdlcBindProjects) Update(_ context.Context, project *orchestration.Project) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.project = project
	return nil
}

func (p *sdlcBindProjects) autonomous() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.project.Autonomous
}

type sdlcBindSecrets struct{ service.SecretsService }

func (sdlcBindSecrets) GetSecret(context.Context, *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	return &service.GetSecretResponse{Data: []byte("x")}, nil
}

type sdlcBindAgents struct{}

func (sdlcBindAgents) Catalogue(context.Context, string) (orchestration.AgentCatalogue, error) {
	coder := orchestration.Agent{ID: "coder", Name: "coder", Backend: "claude", Model: "claude-opus-5-5",
		Roles: []orchestration.AgentRole{orchestration.AgentRoleCoding}}
	return orchestration.AgentCatalogue{
		Agents: map[string]orchestration.Agent{coder.ID: coder},
		Pools:  map[string]orchestration.AgentPool{},
	}, nil
}

// newSDLCBindRig is an embedded transport over a real SDLC service whose one
// project, p1, binds against an in-memory Gitea.
func newSDLCBindRig(t *testing.T) (*Transport, *sdlcBindProjects, sdlc.ProjectBinding) {
	t.Helper()
	forge := giteafake.New(t, "acme", "p1")
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout", Format: "console"})
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := orchestration.NewAgentService(orchestration.NewInMemoryAgentStore(), nil, logger.NewAdapter(log))
	if err != nil {
		t.Fatal(err)
	}
	tasks.(interface{ SetTaskStore(orchestration.TaskStore) }).SetTaskStore(orchestration.NewInMemoryTaskStore())
	store, err := pkgsdlc.NewStore(&sdlcBindResolver{stores: map[string]qrdbsdk.KVStore{}})
	if err != nil {
		t.Fatal(err)
	}
	n := qrdbnode.Shared(t)
	routes, err := pkgsdlc.NewWebhookRouteIndex(n.Transaction, n.KV)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := pkgsdlc.NewRepoHostGuard([]string{"forge.test"}, false)
	if err != nil {
		t.Fatal(err)
	}
	projects := &sdlcBindProjects{project: &orchestration.Project{
		ID: "p1", TenantID: "tenant-a", Name: "p1",
		Git: orchestration.ProjectGit{
			RepoURL: "https://forge.test/acme/p1.git",
			Author:  orchestration.GitAuthor{Name: "agent", Email: "agent@example.test"},
			Signing: orchestration.CommitSigning{Enabled: true, Key: orchestration.KMSKeyRef{Name: "agent"}},
		},
	}}
	svc, err := service.NewSDLCService(service.SDLCServiceConfig{
		Store: store, Routes: routes, Guard: guard,
		Projects: projects, ProjectWriter: projects, Tasks: tasks, Secrets: sdlcBindSecrets{}, Agents: sdlcBindAgents{},
		WebhookPublicURL: "https://factory.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := svc.Unbind(context.Background(), "tenant-a", "p1"); err != nil {
			t.Errorf("Unbind: %v", err)
		}
	})
	tr, err := New(sdlcFacade{svc: svc})
	if err != nil {
		t.Fatal(err)
	}
	binding := sdlc.ProjectBinding{
		ProjectID: "p1",
		Forge: sdlc.ForgeConfig{Type: "gitea", BaseURL: forge.APIURL(), Owner: "acme", Repo: "p1",
			TriggerLabel: "task:ready", TokenSecretRef: "tok"},
		Strategy: pkgsdlc.GitHubFlowName,
		Worker:   agents.AgentRef{AgentID: "coder"},
		Review:   sdlc.ReviewPolicy{RequiredApprovals: 1},
	}
	return tr, projects, binding
}

// TestEmbeddedSDLCBind_PausedLeavesAutonomyOff: BindRequest.Paused reaches
// the service, and a later plain bind still switches autonomy on.
func TestEmbeddedSDLCBind_PausedLeavesAutonomyOff(t *testing.T) {
	tr, projects, binding := newSDLCBindRig(t)
	ctx := context.Background()

	if _, err := tr.Request(ctx, "quicbaas.SDLC", "Bind",
		&sdlc.BindRequest{TenantID: "tenant-a", ProjectID: "p1", Binding: binding, Paused: true}); err != nil {
		t.Fatalf("paused Bind: %v", err)
	}
	if projects.autonomous() {
		t.Fatal("a paused bind turned autonomy on")
	}
	if _, err := tr.Request(ctx, "quicbaas.SDLC", "Bind",
		&sdlc.BindRequest{TenantID: "tenant-a", ProjectID: "p1", Binding: binding}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if !projects.autonomous() {
		t.Fatal("a plain bind left autonomy off")
	}
}
