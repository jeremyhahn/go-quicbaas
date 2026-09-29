//go:build mcp

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

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/giteafake"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

// =============================================================================
// The MCP half of the reviewer profiles: a rebind through sdlc_bind keeps the
// alternates, and the three override tools reach the service.
// =============================================================================

type mcpSDLCResolver struct {
	mu     sync.Mutex
	stores map[string]qrdbsdk.KVStore
}

func (r *mcpSDLCResolver) Resolve(_ context.Context, tenantID string) (qrdbsdk.KVStore, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stores == nil {
		r.stores = map[string]qrdbsdk.KVStore{}
	}
	store, ok := r.stores[tenantID]
	if !ok {
		store = qrdbsdk.NewMemoryKVStore()
		r.stores[tenantID] = store
	}
	return store, nil
}

// mcpSDLCProjects answers GetProject from a map and persists the autonomy flag
// a bind turns on.
type mcpSDLCProjects struct {
	service.ProjectService
	mu       sync.Mutex
	projects map[string]*orchestration.Project
}

func (p *mcpSDLCProjects) GetProject(_ context.Context, req *service.GetProjectRequest) (*service.GetProjectResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	project, ok := p.projects[req.ProjectID]
	if !ok {
		return nil, &service.ProjectNotFoundError{TenantID: req.TenantID, ProjectID: req.ProjectID}
	}
	return &service.GetProjectResponse{Project: project}, nil
}

func (p *mcpSDLCProjects) Update(_ context.Context, project *orchestration.Project) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.projects[project.ID] = project
	return nil
}

// mcpSDLCSecrets resolves every secret, so every reviewer token binds.
type mcpSDLCSecrets struct{ service.SecretsService }

func (mcpSDLCSecrets) GetSecret(context.Context, *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	return &service.GetSecretResponse{Data: []byte("x")}, nil
}

// newMCPSDLCRegistry is a tool registry over a real SDLC service with one
// bindable project, "p1", whose forge is an in-memory Gitea.
func newMCPSDLCRegistry(t *testing.T) (*ToolRegistry, *giteafake.Server) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	forge := giteafake.New(t, "acme", "p1")
	store, err := sdlc.NewStore(&mcpSDLCResolver{})
	if err != nil {
		t.Fatal(err)
	}
	n := qrdbnode.Shared(t)
	routes, err := sdlc.NewWebhookRouteIndex(n.Transaction, n.KV)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := sdlc.NewRepoHostGuard([]string{"forge.test"}, false)
	if err != nil {
		t.Fatal(err)
	}
	projects := &mcpSDLCProjects{projects: map[string]*orchestration.Project{"p1": {
		ID: "p1", TenantID: "tenant-a", Name: "p1",
		Git: orchestration.ProjectGit{
			RepoURL: "https://forge.test/acme/p1.git",
			Author:  orchestration.GitAuthor{Name: "agent", Email: "agent@example.test"},
			Signing: orchestration.CommitSigning{Enabled: true, Key: orchestration.KMSKeyRef{Name: "agent"}},
		},
	}}}
	svc, err := service.NewSDLCService(service.SDLCServiceConfig{
		Store: store, Routes: routes, Guard: guard, Projects: projects, ProjectWriter: projects,
		Secrets: mcpSDLCSecrets{}, WebhookPublicURL: "https://factory.test", Tasks: &mockAgentService{},
		Auditor: &mcpIssueAuditor{},
		Agents:  mcpSDLCAgents{},
	})
	if err != nil {
		t.Fatal(err)
	}
	mock := testutil.NewMockQuicBaaS()
	mock.SetSDLCService(svc)
	return NewToolRegistry(mock, log), forge
}

// profiledToolBinding is the binding argument an MCP client sends, decoded
// the way a JSON-RPC argument arrives: plain maps and slices.
func profiledToolBinding(t *testing.T, forge *giteafake.Server) map[string]interface{} {
	t.Helper()
	advisory := false
	binding := sdlc.ProjectBinding{
		ProjectID: "p1",
		Forge: sdlc.ForgeConfig{Type: sdlc.ForgeGitea, BaseURL: forge.APIURL(), Owner: "acme", Repo: "p1",
			TriggerLabel: "task:ready", TokenSecretRef: "tok"},
		Strategy: sdlc.GitHubFlowName,
		Worker:   orchestration.AgentRefTo("claude-coder"),
		Review:   sdlc.ReviewPolicy{RequiredApprovals: 1},
		Reviewer: sdlc.ReviewerConfig{Agent: "codex-reviewer", Login: "codex", TokenSecretRef: "codex-tok"},
		Reviewers: []sdlc.ReviewerConfig{{
			Name: "qwen", Agent: "qwen-reviewer", Login: "qwen-reviewer", TokenSecretRef: "qwen-tok", CountsTowardMerge: &advisory,
		}},
	}
	raw, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	var arg map[string]interface{}
	if err := json.Unmarshal(raw, &arg); err != nil {
		t.Fatal(err)
	}
	return arg
}

// asArgument re-encodes a tool result the way an MCP client would send it back.
func asArgument(t *testing.T, result interface{}) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var arg map[string]interface{}
	if err := json.Unmarshal(raw, &arg); err != nil {
		t.Fatal(err)
	}
	return arg
}

func TestSDLCReviewerTools_ARebindKeepsTheReviewerProfiles(t *testing.T) {
	registry, forge := newMCPSDLCRegistry(t)
	ctx := context.Background()
	sent := profiledToolBinding(t, forge)

	if _, err := registry.ExecuteTool(ctx, "sdlc_bind", map[string]interface{}{
		"tenant_id": "tenant-a", "project_id": "p1", "binding": sent,
	}); err != nil {
		t.Fatalf("sdlc_bind: %v", err)
	}
	read, err := registry.ExecuteTool(ctx, "sdlc_binding", map[string]interface{}{"tenant_id": "tenant-a", "project_id": "p1"})
	if err != nil {
		t.Fatalf("sdlc_binding: %v", err)
	}
	rebound, err := registry.ExecuteTool(ctx, "sdlc_bind", map[string]interface{}{
		"tenant_id": "tenant-a", "project_id": "p1", "binding": asArgument(t, read),
	})
	if err != nil {
		t.Fatalf("rebind: %v", err)
	}
	got := rebound.(*sdlc.ProjectBinding)
	var want sdlc.ProjectBinding
	if err := json.Unmarshal(mustJSON(t, sent), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Reviewer, want.Reviewer) || !reflect.DeepEqual(got.Reviewers, want.Reviewers) {
		t.Fatalf("after a rebind through MCP: %+v / %+v, want %+v / %+v", got.Reviewer, got.Reviewers, want.Reviewer, want.Reviewers)
	}
}

func TestSDLCReviewerTools_OverrideVerbs(t *testing.T) {
	registry, forge := newMCPSDLCRegistry(t)
	ctx := context.Background()
	if _, err := registry.ExecuteTool(ctx, "sdlc_bind", map[string]interface{}{
		"tenant_id": "tenant-a", "project_id": "p1", "binding": profiledToolBinding(t, forge),
	}); err != nil {
		t.Fatalf("sdlc_bind: %v", err)
	}
	pr := map[string]interface{}{"tenant_id": "tenant-a", "project_id": "p1", "pull_request": float64(41)}

	result, err := registry.ExecuteTool(ctx, "sdlc_reviewer", pr)
	if err != nil || result.(*sdlc.ReviewerSelection).Profile != sdlc.DefaultReviewerProfileName {
		t.Fatalf("sdlc_reviewer: %+v %v", result, err)
	}
	set := map[string]interface{}{"tenant_id": "tenant-a", "project_id": "p1", "pull_request": float64(41), "profile": "qwen"}
	result, err = registry.ExecuteTool(ctx, "sdlc_reviewer_set", set)
	if err != nil || !result.(*sdlc.ReviewerSelection).Overridden || result.(*sdlc.ReviewerSelection).CountsTowardMerge {
		t.Fatalf("sdlc_reviewer_set: %+v %v", result, err)
	}
	result, err = registry.ExecuteTool(ctx, "sdlc_reviewer_clear", pr)
	if err != nil || result.(*sdlc.ReviewerSelection).Overridden {
		t.Fatalf("sdlc_reviewer_clear: %+v %v", result, err)
	}

	set["profile"] = "gpt-9"
	if _, err := registry.ExecuteTool(ctx, "sdlc_reviewer_set", set); !errors.Is(err, sdlc.ErrInvalidInput) {
		t.Fatalf("an unknown profile was set: %v", err)
	}
}

func TestSDLCReviewerTools_RegisteredGatedAndRefusedWithoutTheModule(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewToolRegistry(testutil.NewMockQuicBaaS(), log)
	registered := map[string]bool{}
	for _, tool := range registry.ListTools() {
		registered[tool.Name] = true
	}
	for tool, action := range map[string]string{"sdlc_reviewer": "read", "sdlc_reviewer_set": "bind", "sdlc_reviewer_clear": "bind"} {
		if !registered[tool] || !isSDLCTool(tool) {
			t.Fatalf("%s is not registered and dispatched", tool)
		}
		if perm, ok := qrdbToolExactMapping[tool]; !ok || perm != (qrdbToolResource{"sdlc", action}) {
			t.Fatalf("%s is gated %v, want sdlc:%s; REST and gRPC gate the same verb so", tool, perm, action)
		}
		if _, err := registry.executeSDLCTool(context.Background(), tool, map[string]interface{}{"project_id": "p", "pull_request": 1.0}); err == nil {
			t.Fatalf("%s answered without the module", tool)
		}
	}
}

func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// mcpSDLCAgents is the rig's agent catalog: the same Agents in every tenant.
type mcpSDLCAgents struct{}

func (mcpSDLCAgents) Catalogue(context.Context, string) (orchestration.AgentCatalogue, error) {
	agent := func(id, backend, model string, roles ...orchestration.AgentRole) orchestration.Agent {
		return orchestration.Agent{ID: id, Name: id, Backend: backend, Model: model, Roles: roles}
	}
	catalogue := orchestration.AgentCatalogue{Agents: map[string]orchestration.Agent{}, Pools: map[string]orchestration.AgentPool{}}
	for _, a := range []orchestration.Agent{
		agent("claude-coder", "claude", "claude-opus-5-5", orchestration.AgentRoleCoding, orchestration.AgentRoleTriage),
		agent("codex-reviewer", "codex", "gpt-5.5", orchestration.AgentRoleReview),
		agent("qwen-reviewer", "ollama-agent", "qwen3.8:27b-q4_K_M", orchestration.AgentRoleReview),
	} {
		catalogue.Agents[a.ID] = a
	}
	return catalogue, nil
}
