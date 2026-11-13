//go:build codec_json

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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/giteafake"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

// bindProjects is a one-project store whose writer the bind rig inspects.
type bindProjects struct {
	service.ProjectService
	project *orchestration.Project
}

func (p *bindProjects) GetProject(_ context.Context, req *service.GetProjectRequest) (*service.GetProjectResponse, error) {
	if req.ProjectID != p.project.ID {
		return nil, &service.ProjectNotFoundError{TenantID: req.TenantID, ProjectID: req.ProjectID}
	}
	return &service.GetProjectResponse{Project: p.project}, nil
}

func (p *bindProjects) Update(_ context.Context, project *orchestration.Project) error {
	p.project = project
	return nil
}

type bindSecrets struct{ service.SecretsService }

func (bindSecrets) GetSecret(context.Context, *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	return &service.GetSecretResponse{Data: []byte("x")}, nil
}

type bindAgents struct{}

func (bindAgents) Catalogue(context.Context, string) (orchestration.AgentCatalogue, error) {
	coder := orchestration.Agent{ID: "coder", Name: "coder", Backend: "claude", Model: "claude-opus-5-5",
		Roles: []orchestration.AgentRole{orchestration.AgentRoleCoding}}
	return orchestration.AgentCatalogue{
		Agents: map[string]orchestration.Agent{coder.ID: coder},
		Pools:  map[string]orchestration.AgentPool{},
	}, nil
}

// newBindRig is an SDLCServer over a real SDLC service whose one project,
// p1, binds against an in-memory Gitea.
func newBindRig(t *testing.T) (*SDLCServer, *bindProjects, *pb.ProjectBinding) {
	t.Helper()
	forge := giteafake.New(t, "acme", "p1")
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout", Format: "console"})
	require.NoError(t, err)
	tasks, err := orchestration.NewAgentService(orchestration.NewInMemoryAgentStore(), nil, logger.NewAdapter(log))
	require.NoError(t, err)
	tasks.(interface{ SetTaskStore(orchestration.TaskStore) }).SetTaskStore(orchestration.NewInMemoryTaskStore())
	store, err := sdlc.NewStore(&attentionMemoryResolver{})
	require.NoError(t, err)
	n := qrdbnode.Shared(t)
	routes, err := sdlc.NewWebhookRouteIndex(n.Transaction, n.KV)
	require.NoError(t, err)
	guard, err := sdlc.NewRepoHostGuard([]string{"forge.test"}, false)
	require.NoError(t, err)
	verification, err := orchestration.NewExecutionGuard([]string{"make test"})
	require.NoError(t, err)
	projects := &bindProjects{project: &orchestration.Project{
		ID: "p1", TenantID: "tenant-a", Name: "p1",
		Git: orchestration.ProjectGit{
			RepoURL: "https://forge.test/acme/p1.git",
			Author:  orchestration.GitAuthor{Name: "agent", Email: "agent@example.test"},
			Signing: orchestration.CommitSigning{Enabled: true, Key: orchestration.KMSKeyRef{Name: "agent"}},
		},
	}}
	svc, err := service.NewSDLCService(service.SDLCServiceConfig{
		Store: store, Routes: routes, Guard: guard, Verification: verification,
		Projects: projects, ProjectWriter: projects, Tasks: tasks, Secrets: bindSecrets{}, Agents: bindAgents{},
		WebhookPublicURL: "https://factory.test",
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := svc.Unbind(context.Background(), "tenant-a", "p1"); err != nil {
			t.Errorf("Unbind: %v", err)
		}
	})
	binding := bindingToProto(&sdlc.ProjectBinding{
		ProjectID: "p1",
		Forge: sdlc.ForgeConfig{Type: sdlc.ForgeGitea, BaseURL: forge.APIURL(), Owner: "acme", Repo: "p1",
			TriggerLabel: "task:ready", TokenSecretRef: "tok"},
		Strategy:        sdlc.GitHubFlowName,
		Worker:          orchestration.AgentRefTo("coder"),
		Verification:    []string{"make test"},
		Review:          sdlc.ReviewPolicy{RequiredApprovals: 1},
		MaxReviewRounds: 2,
	})
	return NewSDLCServer(fixedSDLCProvider{svc: svc}, sdlcTestLogger(t)), projects, binding
}

// TestSDLCServer_BindProjectPausedLeavesAutonomyOff: paused on the wire
// reaches the service, and a later plain bind still switches autonomy on.
func TestSDLCServer_BindProjectPausedLeavesAutonomyOff(t *testing.T) {
	server, projects, binding := newBindRig(t)
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

	_, err := server.BindProject(ctx, &pb.BindProjectRequest{TenantId: "tenant-a", Binding: binding, Paused: true})
	require.NoError(t, err)
	assert.False(t, projects.project.Autonomous, "a paused bind turned autonomy on")

	_, err = server.BindProject(ctx, &pb.BindProjectRequest{TenantId: "tenant-a", Binding: binding})
	require.NoError(t, err)
	assert.True(t, projects.project.Autonomous, "a plain bind left autonomy off")
}
