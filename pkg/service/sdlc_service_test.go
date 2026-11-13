// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	quicbaaserrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/giteafake"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

// =============================================================================
// Test doubles for the binding service
// =============================================================================

type sdlcMemoryResolver struct {
	mu     sync.Mutex
	stores map[string]qrdbsdk.KVStore
}

func (r *sdlcMemoryResolver) Resolve(_ context.Context, tenantID string) (qrdbsdk.KVStore, error) {
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

// sdlcFakeProjects answers GetProject from a map and stands in for the
// project store the binding writes autonomy back through; every other verb
// of the embedded nil interface is unreachable in these tests.
type sdlcFakeProjects struct {
	ProjectService
	projects  map[string]*orchestration.Project
	updates   int
	updateErr error
	// created counts CreateProject calls that wrote a project.
	created int
}

var _ ProjectWriter = (*sdlcFakeProjects)(nil)

// Update implements ProjectWriter.
func (p *sdlcFakeProjects) Update(_ context.Context, project *orchestration.Project) error {
	if p.updateErr != nil {
		return p.updateErr
	}
	if _, ok := p.projects[project.ID]; !ok {
		return &ProjectNotFoundError{TenantID: project.TenantID, ProjectID: project.ID}
	}
	p.updates++
	p.projects[project.ID] = project
	return nil
}

func (p *sdlcFakeProjects) GetProject(_ context.Context, req *GetProjectRequest) (*GetProjectResponse, error) {
	project, ok := p.projects[req.ProjectID]
	if !ok || project.TenantID != req.TenantID {
		return nil, &ProjectNotFoundError{TenantID: req.TenantID, ProjectID: req.ProjectID}
	}
	return &GetProjectResponse{Project: project}, nil
}

func (p *sdlcFakeProjects) UpdateProject(_ context.Context, req *UpdateProjectRequest) (*UpdateProjectResponse, error) {
	project, ok := p.projects[req.ProjectID]
	if !ok || project.TenantID != req.TenantID {
		return nil, &ProjectNotFoundError{TenantID: req.TenantID, ProjectID: req.ProjectID}
	}
	if req.DefaultWorkflow != "" {
		project.DefaultWorkflow = req.DefaultWorkflow
	}
	return &UpdateProjectResponse{Project: project}, nil
}

// sdlcFakeAgents is a tenant-scoped workflow registry.
type sdlcFakeAgents struct {
	orchestration.AgentService
	mu        sync.Mutex
	workflows map[string]*orchestration.WorkflowDefinition
}

func (a *sdlcFakeAgents) key(ctx context.Context, name string) string {
	return orchestration.TenantFromContext(ctx) + "/" + name
}

func (a *sdlcFakeAgents) GetWorkflow(ctx context.Context, name string) (*orchestration.WorkflowDefinition, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if def, ok := a.workflows[a.key(ctx, name)]; ok {
		return def, nil
	}
	return nil, &orchestration.WorkflowNotFoundError{TenantID: orchestration.TenantFromContext(ctx), Name: name}
}

// ListTasksPage answers the empty page: no test in this file seeds a
// blocked task, but the attention inbox always pages tasks to build its
// facets, and the embedded orchestration.AgentService is nil — without an
// override here every Attention().List call panics on a nil interface.
func (a *sdlcFakeAgents) ListTasksPage(context.Context, string, *orchestration.TaskFilter, *orchestration.Page) (*orchestration.PageResult[*orchestration.Task], error) {
	return &orchestration.PageResult[*orchestration.Task]{Items: []*orchestration.Task{}}, nil
}

// ListFleetNodesPage answers ErrFleetRegistryRequired, exactly what the real
// agentServiceImpl answers when no fleet registry is configured — this rig
// wires no fleet, and the attention inbox's agent_offline alert already
// degrades on that sentinel (see AttentionService.agentOfflineAlerts).
func (a *sdlcFakeAgents) ListFleetNodesPage(context.Context, string, *orchestration.FleetNodeFilter, *orchestration.Page) (*orchestration.PageResult[*orchestration.FleetNode], error) {
	return nil, orchestration.ErrFleetRegistryRequired
}

func (a *sdlcFakeAgents) RegisterWorkflow(ctx context.Context, def *orchestration.WorkflowDefinition) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.workflows == nil {
		a.workflows = map[string]*orchestration.WorkflowDefinition{}
	}
	a.workflows[a.key(ctx, def.Name)] = def
	return nil
}

// sdlcFakeSecrets answers GetSecret from a map.
type sdlcFakeSecrets struct {
	SecretsService
	secrets map[string][]byte
	err     error
	// errFor fails the read of one secret only, as a store that cannot
	// answer for it.
	errFor map[string]error
	// ensured records every secret EnsureSecret generated, with the tenant
	// it was generated in.
	ensured []sdlcEnsuredSecret
}

func (s *sdlcFakeSecrets) GetSecret(_ context.Context, req *GetSecretRequest) (*GetSecretResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	if err := s.errFor[req.SecretID]; err != nil {
		return nil, err
	}
	data, ok := s.secrets[req.SecretID]
	if !ok {
		// What the real secrets service answers for an absent secret.
		return nil, &quicbaaserrors.QBError{Code: quicbaaserrors.ErrNotFound, Op: "secrets.GetSecret"}
	}
	return &GetSecretResponse{Data: data}, nil
}

const (
	sdlcTestTenant  = "tenant-a"
	sdlcTestProject = "proj-1"
)

var sdlcSeamsOnce sync.Once

func ensureSDLCSeams(t *testing.T) {
	t.Helper()
	sdlcSeamsOnce.Do(func() {
		if err := orchestration.RegisterValidator(sdlc.ValidatorKindForgeReview, func(context.Context, *orchestration.JobContext, orchestration.ReviewCriterion) (orchestration.ValidationResult, error) {
			return orchestration.ValidationResult{Passed: true}, nil
		}); err != nil && !errors.Is(err, orchestration.ErrValidatorAlreadyRegistered) {
			t.Fatal(err)
		}
	})
}

type sdlcServiceRig struct {
	svc          *SDLCService
	projects     *sdlcFakeProjects
	agents       *sdlcFakeAgents
	agentCatalog orchestration.AgentCatalogStore
	secrets      *sdlcFakeSecrets
	store        *sdlc.Store

	// resolver holds the tenant stores behind store, so a test can wrap one.
	resolver *sdlcMemoryResolver

	// forge is a STATEFUL in-memory Gitea, not a double: binding a project
	// now provisions that repository's webhook and labels, so a rig whose
	// forge answered canned values would prove nothing about whether the
	// bind actually onboarded anything. See pkg/testutil/giteafake.
	forge *giteafake.Server

	// audit records every issue filing the service audits.
	audit *recordingIssueAuditor

	// sshKeys is the tenant's SSH keys, empty until a test adds one.
	sshKeys *fakeSSHKeys
}

// sdlcTestPublicURL is the node origin the rig's onboarder provisions
// webhooks to point at.
const sdlcTestPublicURL = "https://factory.test"

// binding returns the fixture binding pointed at this rig's forge.
func (r *sdlcServiceRig) binding() *sdlc.ProjectBinding {
	binding := sdlcTestBinding()
	binding.Forge.BaseURL = r.forge.APIURL()
	return binding
}

func sdlcTestProjectRecord() *orchestration.Project {
	return &orchestration.Project{
		ID: sdlcTestProject, TenantID: sdlcTestTenant, Name: "api", DefaultWorkflow: "",
		Git: orchestration.ProjectGit{
			RepoURL: "https://forge.test/acme/api.git",
			Author:  orchestration.GitAuthor{Name: "agent-api", Email: "agent-api@example.test"},
			Signing: orchestration.CommitSigning{Enabled: true, Key: orchestration.KMSKeyRef{Name: "agent-api"}},
		},
	}
}

// The Agents the service tests' bindings reference.
const (
	serviceCoderAgent    = "claude-coder"
	serviceReviewerAgent = "codex-reviewer"
	serviceQwenAgent     = "qwen-reviewer"
)

// serviceAgentCatalog is a real in-memory catalog seeded with the Agents the
// service tests' bindings reference.
func serviceAgentCatalog(t *testing.T, tenants ...string) orchestration.AgentCatalogStore {
	t.Helper()
	store := orchestration.NewInMemoryAgentCatalogStore()
	for _, tenantID := range append([]string{sdlcTestTenant}, tenants...) {
		if _, err := store.GetAgent(context.Background(), tenantID, serviceCoderAgent); err == nil {
			continue
		}
		seedServiceAgents(t, store, tenantID)
	}
	return store
}

// seedServiceAgents creates the service tests' Agents in tenantID.
func seedServiceAgents(t *testing.T, store orchestration.AgentCatalogStore, tenantID string) {
	t.Helper()
	for _, agent := range []orchestration.Agent{
		{ID: serviceCoderAgent, Name: "Claude coder", Backend: "claude", Model: "claude-opus-5",
			Roles: []orchestration.AgentRole{orchestration.AgentRoleCoding, orchestration.AgentRoleTriage}},
		{ID: serviceReviewerAgent, Name: "Codex reviewer", Backend: "codex", Model: "gpt-5.5",
			Roles: []orchestration.AgentRole{orchestration.AgentRoleReview}},
		{ID: serviceQwenAgent, Name: "Qwen reviewer", Backend: "ollama-agent", Model: "qwen3.8:27b-q4_K_M",
			Roles: []orchestration.AgentRole{orchestration.AgentRoleReview}},
	} {
		if err := store.CreateAgent(context.Background(), tenantID, &agent); err != nil {
			t.Fatalf("seed agent %s: %v", agent.ID, err)
		}
	}
}

func sdlcTestBinding() *sdlc.ProjectBinding {
	return &sdlc.ProjectBinding{
		ProjectID: sdlcTestProject,
		Forge: sdlc.ForgeConfig{
			Type: sdlc.ForgeGitea, BaseURL: "http://forge.test", Owner: "acme", Repo: "api",
			TriggerLabel: "task:ready", WebhookSecretRef: "wh", TokenSecretRef: "tok",
		},
		Strategy:        sdlc.GitHubFlowName,
		Worker:          orchestration.AgentRefTo(serviceCoderAgent),
		Verification:    []string{"make build", "make test"},
		Review:          sdlc.ReviewPolicy{RequiredApprovals: 1},
		MaxReviewRounds: 3,
	}
}

func newSDLCServiceRig(t *testing.T) *sdlcServiceRig {
	t.Helper()
	ensureSDLCSeams(t)
	resolver := &sdlcMemoryResolver{}
	store, err := sdlc.NewStore(resolver)
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
	rig := &sdlcServiceRig{
		resolver:     resolver,
		projects:     &sdlcFakeProjects{projects: map[string]*orchestration.Project{sdlcTestProject: sdlcTestProjectRecord()}},
		agents:       &sdlcFakeAgents{},
		secrets:      &sdlcFakeSecrets{secrets: map[string][]byte{"tok": []byte("token"), "wh": []byte("secret-webhook-key")}},
		store:        store,
		forge:        giteafake.New(t, "acme", "api"),
		audit:        &recordingIssueAuditor{},
		agentCatalog: serviceAgentCatalog(t),
		sshKeys:      newFakeSSHKeys(),
	}
	svc, err := NewSDLCService(SDLCServiceConfig{
		Store: store, Routes: routes, Projects: rig.projects, ProjectWriter: rig.projects,
		Secrets: rig.secrets, Guard: guard, Verification: sdlcTestVerificationGuard(t),
		WebhookPublicURL: sdlcTestPublicURL, Tasks: rig.agents,
		Auditor: rig.audit, Agents: rig.agentCatalog, SSHKeys: rig.sshKeys,
	})
	if err != nil {
		t.Fatal(err)
	}
	rig.svc = svc
	return rig
}

// sdlcTestVerificationGuard is the operator allowlist the test bindings'
// verification commands are admitted by.
func sdlcTestVerificationGuard(t *testing.T) orchestration.ExecutionGuard {
	t.Helper()
	guard, err := orchestration.NewExecutionGuard([]string{"make build", "make test", "make lint"})
	if err != nil {
		t.Fatal(err)
	}
	return guard
}

// =============================================================================
// Tests
// =============================================================================

func TestSDLCService_BindRegistersTheRoute(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	bound, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if bound.ProjectID != sdlcTestProject || bound.UpdatedAt.IsZero() {
		t.Fatalf("bound = %+v", bound)
	}
	// The webhook route resolves to the tenant.
	if tenant, err := rig.svc.Routes().Tenant(ctx, sdlcTestProject); err != nil || tenant != sdlcTestTenant {
		t.Fatalf("route = %q, %v", tenant, err)
	}
	got, err := rig.svc.Binding(ctx, sdlcTestTenant, sdlcTestProject)
	if err != nil || got.Forge.TriggerLabel != "task:ready" {
		t.Fatalf("Binding = %+v, %v", got, err)
	}
	list, err := rig.svc.ListBindings(ctx, sdlcTestTenant)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListBindings = %d, %v", len(list), err)
	}
	if _, err := rig.svc.GetBinding(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatal(err)
	}
	// Re-binding replaces; unbinding forgets the route.
	replacement := rig.binding()
	replacement.Review.RequiredApprovals = 2
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, replacement, BindOptions{}); err != nil {
		t.Fatalf("re-bind: %v", err)
	}
	if err := rig.svc.Unbind(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatalf("Unbind: %v", err)
	}
	if _, err := rig.svc.Binding(ctx, sdlcTestTenant, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("after unbind: %v", err)
	}
	if _, err := rig.svc.Routes().Tenant(ctx, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("route after unbind: %v", err)
	}
	if err := rig.svc.Unbind(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatalf("unbind twice: %v", err)
	}
	if err := rig.svc.Unbind(ctx, "", sdlcTestProject); !errors.Is(err, sdlc.ErrInvalidInput) {
		t.Fatalf("unbind without a tenant: %v", err)
	}
}

// TestSDLCService_BindDefaultsGitHubsAPIRoot proves a GitHub binding that
// omits base_url passes validation on GitHub's public API root rather than
// being refused for the field. The bind names a project the tenant does not
// have, so it stops at the project lookup before anything reaches GitHub.
func TestSDLCService_BindDefaultsGitHubsAPIRoot(t *testing.T) {
	rig := newSDLCServiceRig(t)
	binding := rig.binding()
	binding.ProjectID = "no-such-project"
	binding.Forge.Type, binding.Forge.BaseURL = sdlc.ForgeGitHub, ""

	_, err := rig.svc.Bind(context.Background(), sdlcTestTenant, binding, BindOptions{})
	if errors.Is(err, sdlc.ErrInvalidInput) {
		t.Fatalf("a GitHub binding without base_url was refused: %v", err)
	}
	var missing *ProjectNotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("err = %v, want the missing project's ProjectNotFoundError", err)
	}
	if binding.Forge.BaseURL != "https://api.github.com" {
		t.Errorf("BaseURL = %q, want GitHub's public API root", binding.Forge.BaseURL)
	}
}

func TestSDLCService_BindRefusesWhatThePipelineCannotDeliver(t *testing.T) {
	cases := map[string]struct {
		mutate func(*sdlcServiceRig, *sdlc.ProjectBinding)
		want   error
		text   string
	}{
		"no tenant":       {func(_ *sdlcServiceRig, b *sdlc.ProjectBinding) {}, sdlc.ErrInvalidInput, "tenant"},
		"invalid binding": {func(_ *sdlcServiceRig, b *sdlc.ProjectBinding) { b.Forge.TriggerLabel = "" }, sdlc.ErrInvalidInput, "trigger_label"},
		"unknown forge":   {func(_ *sdlcServiceRig, b *sdlc.ProjectBinding) { b.Forge.Type = "bitbucket" }, sdlc.ErrForgeNotRegistered, "bitbucket"},
		"missing project": {func(_ *sdlcServiceRig, b *sdlc.ProjectBinding) { b.ProjectID = "nope" }, nil, "not found"},
		"repo host": {func(r *sdlcServiceRig, _ *sdlc.ProjectBinding) {
			r.projects.projects[sdlcTestProject].Git.RepoURL = "https://github.com/acme/api.git"
		}, sdlc.ErrInvalidInput, "github.com"},
		"http context repo": {func(r *sdlcServiceRig, _ *sdlc.ProjectBinding) {
			r.projects.projects[sdlcTestProject].ContextRepos = []orchestration.ContextRepo{
				{Name: "handbook", RepoURL: "http://forge.test/acme/handbook", Ref: "main"},
			}
		}, sdlc.ErrInvalidInput, `context repository "handbook" refused`},
		"context repo on an unpinned ssh host": {func(r *sdlcServiceRig, _ *sdlc.ProjectBinding) {
			r.projects.projects[sdlcTestProject].ContextRepos = []orchestration.ContextRepo{
				{Name: "handbook", RepoURL: "ssh://git@forge.test/acme/handbook.git", Ref: "main"},
			}
		}, sdlc.ErrInvalidInput, "cannot be verified"},
		"unsigned": {func(r *sdlcServiceRig, _ *sdlc.ProjectBinding) {
			r.projects.projects[sdlcTestProject].Git.Signing.Enabled = false
		}, sdlc.ErrInvalidInput, "unsigned"},
		"signing without key": {func(r *sdlcServiceRig, _ *sdlc.ProjectBinding) {
			r.projects.projects[sdlcTestProject].Git.Signing.Key = orchestration.KMSKeyRef{}
		}, nil, "signing"},
		"token empty":   {func(r *sdlcServiceRig, _ *sdlc.ProjectBinding) { r.secrets.secrets["tok"] = nil }, sdlc.ErrInvalidInput, "token_secret_ref"},
		"webhook empty": {func(r *sdlcServiceRig, _ *sdlc.ProjectBinding) { r.secrets.secrets["wh"] = nil }, sdlc.ErrWebhookSecretMissing, "resolved empty"},
		"token missing": {func(r *sdlcServiceRig, _ *sdlc.ProjectBinding) { delete(r.secrets.secrets, "tok") }, nil,
			`forge.token_secret_ref: secret "tok" does not exist`},
		"webhook missing": {func(r *sdlcServiceRig, _ *sdlc.ProjectBinding) { delete(r.secrets.secrets, "wh") }, nil,
			`forge.webhook_secret_ref: secret "wh" does not exist`},
		"secrets down": {func(r *sdlcServiceRig, _ *sdlc.ProjectBinding) { r.secrets.err = errors.New("sealed") }, nil,
			`forge.token_secret_ref: secret "tok" cannot be verified: sealed`},
		// X14: go-blockstore's binding was accepted with this command and
		// every attempt was refused after a paid worker turn.
		"verification off the allowlist": {func(_ *sdlcServiceRig, b *sdlc.ProjectBinding) {
			b.Verification = []string{"make build", "make fmt-check vet golangci-lint guards"}
		}, orchestration.ErrCommandRefused, `verification[1] "make fmt-check vet golangci-lint guards"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rig := newSDLCServiceRig(t)
			binding := rig.binding()
			tenant := sdlcTestTenant
			if name == "no tenant" {
				tenant = ""
			}
			tc.mutate(rig, binding)
			_, err := rig.svc.Bind(context.Background(), tenant, binding, BindOptions{})
			if err == nil {
				t.Fatal("bind accepted")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.text)) {
				t.Fatalf("err %q lacks %q", err.Error(), tc.text)
			}
			if _, err := rig.svc.Binding(context.Background(), sdlcTestTenant, binding.ProjectID); !errors.Is(err, sdlc.ErrNotFound) {
				t.Fatalf("a refused bind left a record: %v", err)
			}
		})
	}
}

// TestSDLCService_BindAcceptsASameForgeContextRepo proves the context-repo
// check refuses only what the worker could not clone: an https context
// repository on the project's own forge binds.
func TestSDLCService_BindAcceptsASameForgeContextRepo(t *testing.T) {
	rig := newSDLCServiceRig(t)
	rig.projects.projects[sdlcTestProject].ContextRepos = []orchestration.ContextRepo{
		{Name: "handbook", RepoURL: "https://forge.test/acme/handbook.git", Ref: "main"},
	}
	if _, err := rig.svc.Bind(context.Background(), sdlcTestTenant, rig.binding(), BindOptions{}); err != nil {
		t.Fatalf("bind refused a same-forge https context repository: %v", err)
	}
}

func TestSDLCService_PortsAndNudge(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	forge, err := rig.svc.ForgeFor(ctx, sdlcTestTenant, rig.binding().Forge)
	if err != nil || forge == nil || forge.Type() != sdlc.ForgeGitea {
		t.Fatalf("ForgeFor = %v, %v", forge, err)
	}
	cfg := rig.binding().Forge
	cfg.TokenSecretRef = ""
	if _, err := rig.svc.ForgeFor(ctx, sdlcTestTenant, cfg); !errors.Is(err, sdlc.ErrInvalidInput) {
		t.Fatalf("no token ref: %v", err)
	}
	secret, err := rig.svc.WebhookSecret(ctx, sdlcTestTenant, "wh")
	if err != nil || string(secret) != "secret-webhook-key" {
		t.Fatalf("WebhookSecret = %q, %v", secret, err)
	}
	secret[0] = 'X'
	if again, _ := rig.svc.WebhookSecret(ctx, sdlcTestTenant, "wh"); string(again) != "secret-webhook-key" {
		t.Fatal("the secret returned is not a copy")
	}
	if s, err := rig.svc.WebhookSecret(ctx, sdlcTestTenant, ""); err != nil || s != nil {
		t.Fatalf("empty ref: %q %v", s, err)
	}
	if s, err := rig.svc.WebhookSecret(ctx, sdlcTestTenant, "absent"); !quicbaaserrors.IsNotFound(err) || s != nil {
		t.Fatalf("absent secret: %q %v", s, err)
	}
	if _, err := rig.svc.Delivery(ctx, sdlcTestTenant, "task-x"); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("absent delivery: %v", err)
	}
	if err := rig.svc.Store().PutDelivery(ctx, sdlcTestTenant, &sdlc.Delivery{TaskID: "task-x", Attempt: 1, Branch: "b"}); err != nil {
		t.Fatal(err)
	}
	if d, err := rig.svc.Delivery(ctx, sdlcTestTenant, "task-x"); err != nil || d.Branch != "b" {
		t.Fatalf("Delivery = %+v, %v", d, err)
	}

	if err := rig.svc.NudgeProject(ctx, sdlcTestTenant, sdlcTestProject); !errors.Is(err, sdlc.ErrUnavailable) {
		t.Fatalf("nudge before a poller is attached: %v", err)
	}
	var nudged []string
	rig.svc.SetProjectNudger(nudgerFunc(func(_ context.Context, tenantID, projectID string) error {
		nudged = append(nudged, tenantID+"/"+projectID)
		return nil
	}))
	if err := rig.svc.NudgeProject(ctx, sdlcTestTenant, sdlcTestProject); err != nil || len(nudged) != 1 {
		t.Fatalf("nudge: %v %v", err, nudged)
	}
	rig.svc.SetProjectNudger(nil)
	if err := rig.svc.NudgeProject(ctx, sdlcTestTenant, sdlcTestProject); !errors.Is(err, sdlc.ErrUnavailable) {
		t.Fatalf("nudge after detaching: %v", err)
	}
}

type nudgerFunc func(ctx context.Context, tenantID, projectID string) error

func (f nudgerFunc) NudgeProject(ctx context.Context, tenantID, projectID string) error {
	return f(ctx, tenantID, projectID)
}

func TestNewSDLCService_RefusesIncompleteWiring(t *testing.T) {
	rig := newSDLCServiceRig(t)
	base := SDLCServiceConfig{
		Store: rig.store, Routes: rig.svc.Routes(), Projects: rig.projects,
		ProjectWriter: rig.projects, Secrets: rig.secrets, WebhookPublicURL: sdlcTestPublicURL,
		Tasks: rig.agents,
	}
	for name, mutate := range map[string]func(*SDLCServiceConfig){
		"store":          func(c *SDLCServiceConfig) { c.Store = nil },
		"routes":         func(c *SDLCServiceConfig) { c.Routes = nil },
		"projects":       func(c *SDLCServiceConfig) { c.Projects = nil },
		"secrets":        func(c *SDLCServiceConfig) { c.Secrets = nil },
		"project writer": func(c *SDLCServiceConfig) { c.ProjectWriter = nil },
		"tasks":          func(c *SDLCServiceConfig) { c.Tasks = nil },
		// A node that cannot say where it lives would bind projects whose
		// forge webhooks point nowhere, and report each one provisioned.
		"public url":          func(c *SDLCServiceConfig) { c.WebhookPublicURL = "" },
		"relative public url": func(c *SDLCServiceConfig) { c.WebhookPublicURL = "/api" },
		// A negative forge deadline would cancel every forge call before it
		// was made; refused at construction rather than at the first sweep.
		"negative forge deadline": func(c *SDLCServiceConfig) {
			c.ForgeTransport = sdlc.ForgeTransport{ReadTimeout: -time.Second}
		},
	} {
		cfg := base
		mutate(&cfg)
		if _, err := NewSDLCService(cfg); !errors.Is(err, sdlc.ErrInvalidInput) {
			t.Fatalf("%s nil accepted: %v", name, err)
		}
	}
}

// TestNewSDLCService_TheForgeTransportReachesTheAdapter proves the node's forge
// policy is what the adapters this service builds actually use: a policy set on
// the service and dropped on the way to the factory would leave every forge
// call on the defaults while the operator believes otherwise.
func TestNewSDLCService_TheForgeTransportReachesTheAdapter(t *testing.T) {
	rig := newSDLCServiceRig(t)
	transport := sdlc.ForgeTransport{ReadTimeout: 7 * time.Second, WriteTimeout: 8 * time.Second}
	svc, err := NewSDLCService(SDLCServiceConfig{
		Store: rig.store, Routes: rig.svc.Routes(), Projects: rig.projects, ProjectWriter: rig.projects,
		Secrets: rig.secrets, WebhookPublicURL: sdlcTestPublicURL, ForgeTransport: transport, Tasks: rig.agents, Agents: rig.agentCatalog,
	})
	if err != nil {
		t.Fatalf("NewSDLCService: %v", err)
	}
	if svc.transport != transport {
		t.Fatalf("service transport = %+v, want %+v", svc.transport, transport)
	}
	forge, err := svc.ForgeFor(context.Background(), sdlcTestTenant, rig.binding().Forge)
	if err != nil {
		t.Fatalf("ForgeFor: %v", err)
	}
	if forge.Type() != sdlc.ForgeGitea {
		t.Fatalf("forge type = %s", forge.Type())
	}
}

// =============================================================================
// Autonomy
// =============================================================================

// TestSDLCService_BindTurnsTheProjectAutonomous pins the owner's ruling that
// binding a forge IS the autonomy switch: the bound project is stored back
// with Autonomous set, and no default workflow is required to bind.
func TestSDLCService_BindTurnsTheProjectAutonomous(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	project := rig.projects.projects[sdlcTestProject]
	if project.Autonomous || project.DefaultWorkflow != "" {
		t.Fatalf("fixture is not the case under test: %+v", project)
	}
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if !rig.projects.projects[sdlcTestProject].Autonomous {
		t.Fatal("binding did not turn the project autonomous")
	}
	if rig.projects.updates != 1 {
		t.Fatalf("updates = %d, want the one write", rig.projects.updates)
	}
	// Re-binding an already autonomous project writes nothing further.
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{}); err != nil {
		t.Fatalf("re-bind: %v", err)
	}
	if rig.projects.updates != 1 {
		t.Fatalf("updates = %d, want no second write", rig.projects.updates)
	}
	// Unbinding is not the operator turning autonomy off.
	if err := rig.svc.Unbind(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatalf("Unbind: %v", err)
	}
	if !rig.projects.projects[sdlcTestProject].Autonomous {
		t.Fatal("unbinding turned autonomy off; it is the operator's switch, not the binding's")
	}
}

// TestSDLCService_BindRefusesWhenAutonomyCannotBePersisted covers the error
// path: a bind whose autonomy write failed must leave no binding behind.
func TestSDLCService_BindRefusesWhenAutonomyCannotBePersisted(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	rig.projects.updateErr = errors.New("store sealed")
	_, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{})
	if err == nil || !strings.Contains(err.Error(), "store sealed") {
		t.Fatalf("Bind = %v, want the store's refusal", err)
	}
	if _, err := rig.svc.Binding(ctx, sdlcTestTenant, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("a refused bind left a record: %v", err)
	}
	if _, err := rig.svc.Routes().Tenant(ctx, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("a refused bind left a route: %v", err)
	}
}

// unbindOnCleanup removes the test project's route from the package's shared
// qrdb node, so a later test's "no route" assertion starts clean.
func unbindOnCleanup(t *testing.T, rig *sdlcServiceRig) {
	t.Helper()
	t.Cleanup(func() {
		if err := rig.svc.Unbind(context.Background(), sdlcTestTenant, sdlcTestProject); err != nil {
			t.Errorf("Unbind: %v", err)
		}
	})
}

// TestSDLCService_BindLeavePausedStagesTheProject pins #1041's prerequisite:
// a project can be bound without the poller adopting its issues, because
// LeavePaused leaves autonomy off and writes nothing to the project.
func TestSDLCService_BindLeavePausedStagesTheProject(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if rig.projects.projects[sdlcTestProject].Autonomous {
		t.Fatal("fixture is not the case under test: project already autonomous")
	}
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{LeavePaused: true}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	unbindOnCleanup(t, rig)
	if rig.projects.projects[sdlcTestProject].Autonomous {
		t.Fatal("a paused bind turned the project autonomous")
	}
	if rig.projects.updates != 0 {
		t.Fatalf("updates = %d, want no project write", rig.projects.updates)
	}
	if _, err := rig.svc.Binding(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatalf("a paused bind stored no binding: %v", err)
	}
	if tenant, err := rig.svc.Routes().Tenant(ctx, sdlcTestProject); err != nil || tenant != sdlcTestTenant {
		t.Fatalf("route = %q, %v; want %q", tenant, err, sdlcTestTenant)
	}
}

// TestSDLCService_BindLeavePausedNeverTurnsAutonomyOff pins the documented
// rule: paused means "do not switch it on", not "switch it off".
func TestSDLCService_BindLeavePausedNeverTurnsAutonomyOff(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	rig.projects.projects[sdlcTestProject].Autonomous = true
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{LeavePaused: true}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	unbindOnCleanup(t, rig)
	if !rig.projects.projects[sdlcTestProject].Autonomous {
		t.Fatal("a paused bind turned autonomy off")
	}
	if rig.projects.updates != 0 {
		t.Fatalf("updates = %d, want no project write", rig.projects.updates)
	}
}

// TestSDLCService_BindLeavePausedRefusedWritesNothing: a refused paused bind
// leaves no binding, no route and no project write.
func TestSDLCService_BindLeavePausedRefusedWritesNothing(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	binding := rig.binding()
	binding.Forge.Type = "no-such-forge"
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, binding, BindOptions{LeavePaused: true}); err == nil {
		t.Fatal("Bind accepted an unregistered forge")
	}
	if rig.projects.updates != 0 {
		t.Fatalf("updates = %d, want no project write", rig.projects.updates)
	}
	if _, err := rig.svc.Binding(ctx, sdlcTestTenant, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("a refused bind left a record: %v", err)
	}
	if _, err := rig.svc.Routes().Tenant(ctx, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("a refused bind left a route: %v", err)
	}
}

// newSDLCServiceRigForTenant builds a rig whose sole project is owned by
// ownerTenant, with an injected logger whose output the caller can inspect.
// It is the parity fixture: the system tenant registers and binds an SDLC
// project exactly as any other tenant does.
func newSDLCServiceRigForTenant(t *testing.T, ownerTenant, projectID string, logs *bytes.Buffer) *sdlcServiceRig {
	t.Helper()
	ensureSDLCSeams(t)
	store, err := sdlc.NewStore(&sdlcMemoryResolver{})
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
	project := sdlcTestProjectRecord()
	project.TenantID = ownerTenant
	project.ID = projectID
	rig := &sdlcServiceRig{
		projects: &sdlcFakeProjects{projects: map[string]*orchestration.Project{projectID: project}},
		agents:   &sdlcFakeAgents{},
		secrets:  &sdlcFakeSecrets{secrets: map[string][]byte{"tok": []byte("token"), "wh": []byte("secret-webhook-key")}},
		store:    store,
		forge:    giteafake.New(t, "acme", "api"),

		agentCatalog: serviceAgentCatalog(t, ownerTenant),
	}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	svc, err := NewSDLCService(SDLCServiceConfig{
		Store: store, Routes: routes, Projects: rig.projects, ProjectWriter: rig.projects,
		Secrets: rig.secrets, Guard: guard, Logger: logger, Tasks: rig.agents, Agents: rig.agentCatalog,
		WebhookPublicURL: sdlcTestPublicURL, Verification: sdlcTestVerificationGuard(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	rig.svc = svc
	return rig
}

// TestSDLCService_BindInSystemTenantSucceedsWithWarning locks the owner ruling
// (2026-09-09, issue #658): the system tenant has parity with regular tenants
// and MAY register and bind an SDLC project. The bind must succeed — not be
// refused — and must emit exactly one operator-facing WARN that the worker
// runs at SYSTEM privilege. If this ever regresses into a refusal, or the
// warning disappears, parity has been silently broken.
func TestSDLCService_BindInSystemTenantSucceedsWithWarning(t *testing.T) {
	const projectID = "proj-system-sdlc"
	var logs bytes.Buffer
	rig := newSDLCServiceRigForTenant(t, qrdbsdk.SystemTenantID, projectID, &logs)
	ctx := context.Background()

	binding := rig.binding()
	binding.ProjectID = projectID
	bound, err := rig.svc.Bind(ctx, qrdbsdk.SystemTenantID, binding, BindOptions{})
	if err != nil {
		t.Fatalf("Bind in system tenant must succeed (parity), got: %v", err)
	}
	if bound.ProjectID != projectID || bound.UpdatedAt.IsZero() {
		t.Fatalf("bound = %+v", bound)
	}
	// The binding is durable and routable exactly as a regular tenant's is.
	if _, err := rig.svc.Binding(ctx, qrdbsdk.SystemTenantID, projectID); err != nil {
		t.Fatalf("Binding after system-tenant bind: %v", err)
	}
	if tenant, err := rig.svc.Routes().Tenant(ctx, projectID); err != nil || tenant != qrdbsdk.SystemTenantID {
		t.Fatalf("route = %q, %v", tenant, err)
	}
	// Exactly one WARN naming the SYSTEM-privilege exposure was emitted.
	out := logs.String()
	if !strings.Contains(out, "level=WARN") {
		t.Fatalf("expected a WARN line, got: %q", out)
	}
	if !strings.Contains(out, "SYSTEM privilege") || !strings.Contains(out, "system tenant") {
		t.Fatalf("WARN must name the system-tenant SYSTEM-privilege exposure, got: %q", out)
	}
	if got := strings.Count(out, "level=WARN"); got != 1 {
		t.Fatalf("expected exactly one WARN, got %d: %q", got, out)
	}
}

// TestSDLCService_BindInRegularTenantEmitsNoSystemWarning is the negative half:
// a bind in a normal tenant is the common case and must stay silent. This keeps
// the SYSTEM-privilege notice scoped to the system tenant rather than becoming
// noise on every bind.
func TestSDLCService_BindInRegularTenantEmitsNoSystemWarning(t *testing.T) {
	const projectID = "proj-regular-sdlc"
	var logs bytes.Buffer
	rig := newSDLCServiceRigForTenant(t, sdlcTestTenant, projectID, &logs)
	ctx := context.Background()

	binding := rig.binding()
	binding.ProjectID = projectID
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, binding, BindOptions{}); err != nil {
		t.Fatalf("Bind in regular tenant: %v", err)
	}
	if out := logs.String(); strings.Contains(out, "SYSTEM privilege") {
		t.Fatalf("regular-tenant bind must not warn about SYSTEM privilege, got: %q", out)
	}
}

// TestSDLCService_BindRefusesReviewerWithUnresolvableToken proves a named
// reviewer's forge token is checked at BIND time.
//
// THE ALTERNATIVE FAILS AT REVIEW TIME, WHICH IS THE WORST MOMENT. By then the
// pull request is finished and green, a review job has started, and a model run
// has already been spent reading the change — and only then can no verdict be
// published. The forge token has always been checked here for exactly this
// reason; the reviewer token was simply missed.
func TestSDLCService_BindRefusesReviewerWithUnresolvableToken(t *testing.T) {
	const projectID = "proj-reviewer-missing-secret"
	var logs bytes.Buffer
	rig := newSDLCServiceRigForTenant(t, sdlcTestTenant, projectID, &logs)

	binding := rig.binding()
	binding.ProjectID = projectID
	binding.Reviewer = sdlc.ReviewerConfig{
		Agent:          serviceReviewerAgent,
		Login:          "codex",
		TokenSecretRef: "sdlc/reviewer/absent/token",
	}

	_, err := rig.svc.Bind(context.Background(), sdlcTestTenant, binding, BindOptions{})
	if err == nil {
		t.Fatal("Bind must refuse a reviewer whose token secret cannot be resolved")
	}
	if !strings.Contains(err.Error(), "sdlc/reviewer/absent/token") {
		t.Fatalf("the refusal must name the secret that could not be resolved, got: %v", err)
	}
	requireReferenceNotFound(t, err, "reviewer.token_secret_ref")
}

// TestSDLCService_BindRefusesReviewerWithNoTokenRef covers the other way to
// arrive with a reviewer that cannot post: a backend named and no secret ref at
// all.
func TestSDLCService_BindRefusesReviewerWithNoTokenRef(t *testing.T) {
	const projectID = "proj-reviewer-no-ref"
	var logs bytes.Buffer
	rig := newSDLCServiceRigForTenant(t, sdlcTestTenant, projectID, &logs)

	binding := rig.binding()
	binding.ProjectID = projectID
	binding.Reviewer = sdlc.ReviewerConfig{Agent: serviceReviewerAgent, Login: "codex"}

	_, err := rig.svc.Bind(context.Background(), sdlcTestTenant, binding, BindOptions{})
	if err == nil {
		t.Fatal("Bind must refuse a reviewer with no token secret ref")
	}
	var invalid *sdlc.InvalidInputError
	if !errors.As(err, &invalid) || invalid.Field != "reviewer.token_secret_ref" {
		t.Fatalf("want InvalidInputError on reviewer.token_secret_ref, got %T: %v", err, err)
	}
}

// TestSDLCService_BindAcceptsNoReviewer is the case the check must NOT refuse.
//
// A PROJECT REVIEWED BY PEOPLE BINDS NO REVIEWER. That is a legitimate
// configuration, and a bind-time check that refused it would break every
// project not self-reviewed.
func TestSDLCService_BindAcceptsNoReviewer(t *testing.T) {
	const projectID = "proj-no-reviewer"
	var logs bytes.Buffer
	rig := newSDLCServiceRigForTenant(t, sdlcTestTenant, projectID, &logs)

	binding := rig.binding()
	binding.ProjectID = projectID
	binding.Reviewer = sdlc.ReviewerConfig{}

	if _, err := rig.svc.Bind(context.Background(), sdlcTestTenant, binding, BindOptions{}); err != nil {
		t.Fatalf("a project that binds no reviewer must still bind: %v", err)
	}
}

// TestSDLCService_BindAcceptsReviewerWithResolvableToken closes the loop: the
// reviewer that the go-luks2 burn-in needed, with its secret present, binds and
// comes back through Binding() intact.
func TestSDLCService_BindAcceptsReviewerWithResolvableToken(t *testing.T) {
	const projectID = "proj-reviewer-ok"
	var logs bytes.Buffer
	rig := newSDLCServiceRigForTenant(t, sdlcTestTenant, projectID, &logs)
	rig.secrets.secrets["sdlc/reviewer/go-luks2/token"] = []byte("reviewer-token")
	ctx := context.Background()

	binding := rig.binding()
	binding.ProjectID = projectID
	binding.Reviewer = sdlc.ReviewerConfig{
		Agent:          serviceReviewerAgent,
		Login:          "codex",
		TokenSecretRef: "sdlc/reviewer/go-luks2/token",
	}

	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, binding, BindOptions{}); err != nil {
		t.Fatalf("Bind with a resolvable reviewer token: %v", err)
	}
	stored, err := rig.svc.Binding(ctx, sdlcTestTenant, projectID)
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	if !stored.Reviewer.Configured() || stored.Reviewer.Login != "codex" {
		t.Fatalf("the stored reviewer must survive the bind: %+v", stored.Reviewer)
	}
}

// TestSDLCService_UnbindSurvivesAnUnresolvableForgeToken is the regression test
// for a binding that could never be deleted.
//
// THE TRAP, MEASURED ON THE BURN-IN NODE (2026-09-14): a project was deleted
// while still bound, which orphaned its SDLC binding; the triage backstop
// starts a job for every BOUND project, so the orphan failed a job on every
// pass for ever. The operator's way out is Unbind — but Unbind offboards the
// forge first, offboarding needs the binding's forge token, and that secret had
// been deleted too. `DELETE .../binding` answered 500 "secret has been deleted"
// with retryable=false, on every attempt. There was no verb that could remove
// the record.
//
// A missing secret is permanent: no retry resolves it, so refusing buys nothing
// and costs the operator their only escape. The local record goes; what is left
// on the forge is inert and `conformance` names it.
func TestSDLCService_UnbindSurvivesAnUnresolvableForgeToken(t *testing.T) {
	const projectID = "proj-unbind-secret-gone"
	var logs bytes.Buffer
	rig := newSDLCServiceRigForTenant(t, sdlcTestTenant, projectID, &logs)
	ctx := context.Background()

	binding := rig.binding()
	binding.ProjectID = projectID
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, binding, BindOptions{}); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	// The forge token secret is destroyed behind the binding's back.
	rig.secrets.err = &quicbaaserrors.QBError{
		Code: quicbaaserrors.ErrNotFound,
		Op:   "secrets.GetSecret",
		Err:  errors.New("secret has been deleted"),
	}

	if err := rig.svc.Unbind(ctx, sdlcTestTenant, projectID); err != nil {
		t.Fatalf("Unbind must remove a binding whose forge token is gone, got: %v", err)
	}
	if _, err := rig.svc.Binding(ctx, sdlcTestTenant, projectID); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("the binding must be gone after Unbind, got: %v", err)
	}
	if out := logs.String(); !strings.Contains(out, "level=WARN") ||
		!strings.Contains(out, "forge is left as it stands") {
		t.Fatalf("the operator must be told the forge was left alone, got: %q", out)
	}
}

// TestSDLCService_UnbindStillRefusesATransientForgeFailure is the other half,
// and the reason the fix is narrow rather than a blanket "ignore offboarding".
//
// A FORGE THAT IS MERELY DOWN WILL WORK ON RETRY. Dropping the local record for
// that would leave a live webhook delivering to a node that no longer routes
// it, and would report success for a cleanup that did not happen.
func TestSDLCService_UnbindStillRefusesATransientForgeFailure(t *testing.T) {
	const projectID = "proj-unbind-forge-down"
	var logs bytes.Buffer
	rig := newSDLCServiceRigForTenant(t, sdlcTestTenant, projectID, &logs)
	ctx := context.Background()

	binding := rig.binding()
	binding.ProjectID = projectID
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, binding, BindOptions{}); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	rig.secrets.err = &quicbaaserrors.QBError{
		Code: quicbaaserrors.ErrInternal,
		Op:   "secrets.GetSecret",
		Err:  errors.New("backend unavailable"),
	}

	if err := rig.svc.Unbind(ctx, sdlcTestTenant, projectID); err == nil {
		t.Fatal("Unbind must refuse while the failure is one a retry could fix")
	}
	// And the binding is still there to retry against.
	rig.secrets.err = nil
	if _, err := rig.svc.Binding(ctx, sdlcTestTenant, projectID); err != nil {
		t.Fatalf("a refused Unbind must leave the binding intact, got: %v", err)
	}
}

// TestSDLCService_UnbindSurvivesAForgeThatRefusesTheAccount is the second half
// of the real burn-in failure, and the reason the not-found check alone was not
// enough.
//
// RESTORING THE DELETED SECRET ONLY MOVED THE REFUSAL. With the token back, the
// same unbind answered `403 user should be an owner or a collaborator with
// admin write of a repository` — the agent is not a repo admin, so it cannot
// list or delete the webhook. Repeating that call can never succeed, and the
// binding was still undeletable while the triage backstop kept failing a job
// against it every pass.
func TestSDLCService_UnbindSurvivesAForgeThatRefusesTheAccount(t *testing.T) {
	const projectID = "proj-unbind-not-admin"
	var logs bytes.Buffer
	rig := newSDLCServiceRigForTenant(t, sdlcTestTenant, projectID, &logs)
	ctx := context.Background()

	binding := rig.binding()
	binding.ProjectID = projectID
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, binding, BindOptions{}); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	// The forge now answers 403 to the webhook listing offboarding needs.
	rig.forge.ForceStatus("GET", "/hooks", 403,
		"user should be an owner or a collaborator with admin write of a repository")

	if err := rig.svc.Unbind(ctx, sdlcTestTenant, projectID); err != nil {
		t.Fatalf("Unbind must remove a binding the forge refuses to offboard, got: %v", err)
	}
	if _, err := rig.svc.Binding(ctx, sdlcTestTenant, projectID); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("the binding must be gone after Unbind, got: %v", err)
	}
}

// =============================================================================
// Attention inbox — account_error wiring
// =============================================================================

// TestSDLCService_AttentionReportsAccountErrorWhenAccountsIsWired proves the
// SDLCServiceConfig.Accounts wiring reaches the attention inbox: a pooled
// account whose last credential refresh failed shows up as an
// account_error alert once an orchestration.AccountStore is handed to the
// service.
func TestSDLCService_AttentionReportsAccountErrorWhenAccountsIsWired(t *testing.T) {
	ensureSDLCSeams(t)
	store, err := sdlc.NewStore(&sdlcMemoryResolver{})
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
	projects := &sdlcFakeProjects{projects: map[string]*orchestration.Project{sdlcTestProject: sdlcTestProjectRecord()}}
	secrets := &sdlcFakeSecrets{secrets: map[string][]byte{"tok": []byte("token"), "wh": []byte("secret-webhook-key")}}
	accounts := orchestration.NewInMemoryAccountStore()
	ctx := context.Background()
	if err := accounts.Create(ctx, &orchestration.Account{
		ID: "acct-1", TenantID: sdlcTestTenant, Provider: orchestration.AccountProviderClaude,
		DisplayName: "team-max", Status: orchestration.AccountStatusError, LastError: "refresh: 401",
	}); err != nil {
		t.Fatalf("seed account: %v", err)
	}

	svc, err := NewSDLCService(SDLCServiceConfig{
		Store: store, Routes: routes, Projects: projects, ProjectWriter: projects,
		Secrets: secrets, Guard: guard, WebhookPublicURL: sdlcTestPublicURL, Tasks: &sdlcFakeAgents{},
		Accounts: accounts, Agents: serviceAgentCatalog(t), Verification: sdlcTestVerificationGuard(t),
	})
	if err != nil {
		t.Fatalf("NewSDLCService: %v", err)
	}

	page, err := svc.Attention().List(ctx, sdlcTestTenant, sdlc.AttentionFilter{Kind: sdlc.AttentionAccountError}, &orchestration.Page{})
	if err != nil {
		t.Fatalf("Attention().List: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1 erroring account", len(page.Items))
	}
	if !strings.Contains(page.Items[0].Message, "refresh: 401") {
		t.Errorf("Message = %q, want it to carry the account's last error", page.Items[0].Message)
	}
}

// TestSDLCService_AttentionOmitsAccountErrorWhenAccountsIsNil proves an
// unwired Accounts is an honest "no pooled accounts on this node", not a
// refusal to start or an error on every List call.
func TestSDLCService_AttentionOmitsAccountErrorWhenAccountsIsNil(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()

	page, err := rig.svc.Attention().List(ctx, sdlcTestTenant, sdlc.AttentionFilter{Kind: sdlc.AttentionAccountError}, &orchestration.Page{})
	if err != nil {
		t.Fatalf("Attention().List: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("Items = %v, want none: no Accounts store was wired", page.Items)
	}
}

// TestSDLCService_AdoptIssue_RefusesBeforeTouchingAnything: the verb reaches
// the adopter, which refuses a request naming no issue and a project with no
// binding, and no task is created for either.
func TestSDLCService_AdoptIssue_RefusesBeforeTouchingAnything(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.svc.AdoptIssue(ctx, sdlcTestTenant, sdlc.AdoptIssueRequest{ProjectID: sdlcTestProject}); !errors.Is(err, sdlc.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput for no issue", err)
	}
	if _, err := rig.svc.AdoptIssue(ctx, sdlcTestTenant, sdlc.AdoptIssueRequest{ProjectID: sdlcTestProject, Issue: 7}); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound for an unbound project", err)
	}
}

// TestSDLCService_OnboardRefusesABindingOffTheAllowlist: a binding written
// before the allowlist narrowed is refused by Onboard, naming the command,
// and one the allowlist admits onboards.
func TestSDLCService_OnboardRefusesABindingOffTheAllowlist(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if _, err := rig.svc.Onboard(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatalf("Onboard of an allowlisted binding: %v", err)
	}
	stale, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatal(err)
	}
	stale.Verification = []string{"make lint-everything"}
	if err := rig.store.PutBinding(ctx, sdlcTestTenant, stale); err != nil {
		t.Fatal(err)
	}
	_, err = rig.svc.Onboard(ctx, sdlcTestTenant, sdlcTestProject)
	var refused *sdlc.VerificationCommandRefusedError
	if !errors.As(err, &refused) || refused.Command != "make lint-everything" {
		t.Fatalf("Onboard = %v, want the refused command named", err)
	}
}
