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
}

func (s *sdlcFakeSecrets) GetSecret(_ context.Context, req *GetSecretRequest) (*GetSecretResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	data, ok := s.secrets[req.SecretID]
	if !ok {
		return nil, nil
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
	svc      *SDLCService
	projects *sdlcFakeProjects
	agents   *sdlcFakeAgents
	secrets  *sdlcFakeSecrets
	store    *sdlc.Store

	// forge is a STATEFUL in-memory Gitea, not a double: binding a project
	// now provisions that repository's webhook and labels, so a rig whose
	// forge answered canned values would prove nothing about whether the
	// bind actually onboarded anything. See pkg/testutil/giteafake.
	forge *giteafake.Server
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

func sdlcTestBinding() *sdlc.ProjectBinding {
	return &sdlc.ProjectBinding{
		ProjectID: sdlcTestProject,
		Forge: sdlc.ForgeConfig{
			Type: sdlc.ForgeGitea, BaseURL: "http://forge.test", Owner: "acme", Repo: "api",
			TriggerLabel: "task:ready", WebhookSecretRef: "wh", TokenSecretRef: "tok",
		},
		Strategy:        sdlc.GitHubFlowName,
		Worker:          sdlc.WorkerConfig{Backend: "claude", Model: "claude-opus-5"},
		Verification:    []string{"make build", "make test"},
		Review:          sdlc.ReviewPolicy{RequiredApprovals: 1},
		MaxReviewRounds: 3,
	}
}

func newSDLCServiceRig(t *testing.T) *sdlcServiceRig {
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
	rig := &sdlcServiceRig{
		projects: &sdlcFakeProjects{projects: map[string]*orchestration.Project{sdlcTestProject: sdlcTestProjectRecord()}},
		agents:   &sdlcFakeAgents{},
		secrets:  &sdlcFakeSecrets{secrets: map[string][]byte{"tok": []byte("token"), "wh": []byte("secret")}},
		store:    store,
		forge:    giteafake.New(t, "acme", "api"),
	}
	svc, err := NewSDLCService(SDLCServiceConfig{
		Store: store, Routes: routes, Projects: rig.projects, ProjectWriter: rig.projects,
		Secrets: rig.secrets, Guard: guard, WebhookPublicURL: sdlcTestPublicURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	rig.svc = svc
	return rig
}

// =============================================================================
// Tests
// =============================================================================

func TestSDLCService_BindRegistersTheRoute(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	bound, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding())
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
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, replacement); err != nil {
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

func TestSDLCService_BindRefusesWhatThePipelineCannotDeliver(t *testing.T) {
	cases := map[string]struct {
		mutate func(*sdlcServiceRig, *sdlc.ProjectBinding)
		want   error
		text   string
	}{
		"no tenant":       {func(_ *sdlcServiceRig, b *sdlc.ProjectBinding) {}, sdlc.ErrInvalidInput, "tenant"},
		"invalid binding": {func(_ *sdlcServiceRig, b *sdlc.ProjectBinding) { b.Forge.TriggerLabel = "" }, sdlc.ErrInvalidInput, "trigger_label"},
		"unknown forge":   {func(_ *sdlcServiceRig, b *sdlc.ProjectBinding) { b.Forge.Type = "gitlab" }, sdlc.ErrForgeNotRegistered, "gitlab"},
		"missing project": {func(_ *sdlcServiceRig, b *sdlc.ProjectBinding) { b.ProjectID = "nope" }, nil, "not found"},
		"repo host": {func(r *sdlcServiceRig, _ *sdlc.ProjectBinding) {
			r.projects.projects[sdlcTestProject].Git.RepoURL = "https://github.com/acme/api.git"
		}, sdlc.ErrInvalidInput, "github.com"},
		"unsigned": {func(r *sdlcServiceRig, _ *sdlc.ProjectBinding) {
			r.projects.projects[sdlcTestProject].Git.Signing.Enabled = false
		}, sdlc.ErrInvalidInput, "unsigned"},
		"signing without key": {func(r *sdlcServiceRig, _ *sdlc.ProjectBinding) {
			r.projects.projects[sdlcTestProject].Git.Signing.Key = orchestration.KMSKeyRef{}
		}, nil, "signing"},
		"token missing":   {func(r *sdlcServiceRig, _ *sdlc.ProjectBinding) { delete(r.secrets.secrets, "tok") }, sdlc.ErrInvalidInput, "token_secret_ref"},
		"webhook missing": {func(r *sdlcServiceRig, _ *sdlc.ProjectBinding) { delete(r.secrets.secrets, "wh") }, sdlc.ErrWebhookSecretMissing, "resolved empty"},
		"secrets down":    {func(r *sdlcServiceRig, _ *sdlc.ProjectBinding) { r.secrets.err = errors.New("sealed") }, nil, "sealed"},
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
			_, err := rig.svc.Bind(context.Background(), tenant, binding)
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
	if err != nil || string(secret) != "secret" {
		t.Fatalf("WebhookSecret = %q, %v", secret, err)
	}
	secret[0] = 'X'
	if again, _ := rig.svc.WebhookSecret(ctx, sdlcTestTenant, "wh"); string(again) != "secret" {
		t.Fatal("the secret returned is not a copy")
	}
	if s, err := rig.svc.WebhookSecret(ctx, sdlcTestTenant, ""); err != nil || s != nil {
		t.Fatalf("empty ref: %q %v", s, err)
	}
	if s, err := rig.svc.WebhookSecret(ctx, sdlcTestTenant, "absent"); err != nil || s != nil {
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
	}
	for name, mutate := range map[string]func(*SDLCServiceConfig){
		"store":          func(c *SDLCServiceConfig) { c.Store = nil },
		"routes":         func(c *SDLCServiceConfig) { c.Routes = nil },
		"projects":       func(c *SDLCServiceConfig) { c.Projects = nil },
		"secrets":        func(c *SDLCServiceConfig) { c.Secrets = nil },
		"project writer": func(c *SDLCServiceConfig) { c.ProjectWriter = nil },
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
		Secrets: rig.secrets, WebhookPublicURL: sdlcTestPublicURL, ForgeTransport: transport,
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
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding()); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if !rig.projects.projects[sdlcTestProject].Autonomous {
		t.Fatal("binding did not turn the project autonomous")
	}
	if rig.projects.updates != 1 {
		t.Fatalf("updates = %d, want the one write", rig.projects.updates)
	}
	// Re-binding an already autonomous project writes nothing further.
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding()); err != nil {
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
	_, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding())
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
		secrets:  &sdlcFakeSecrets{secrets: map[string][]byte{"tok": []byte("token"), "wh": []byte("secret")}},
		store:    store,
		forge:    giteafake.New(t, "acme", "api"),
	}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	svc, err := NewSDLCService(SDLCServiceConfig{
		Store: store, Routes: routes, Projects: rig.projects, ProjectWriter: rig.projects,
		Secrets: rig.secrets, Guard: guard, Logger: logger,
		WebhookPublicURL: sdlcTestPublicURL,
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
	bound, err := rig.svc.Bind(ctx, qrdbsdk.SystemTenantID, binding)
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
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, binding); err != nil {
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
		Backend:        "codex",
		Login:          "codex",
		TokenSecretRef: "sdlc/reviewer/absent/token",
	}

	_, err := rig.svc.Bind(context.Background(), sdlcTestTenant, binding)
	if err == nil {
		t.Fatal("Bind must refuse a reviewer whose token secret cannot be resolved")
	}
	if !strings.Contains(err.Error(), "sdlc/reviewer/absent/token") {
		t.Fatalf("the refusal must name the secret that could not be resolved, got: %v", err)
	}
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
	binding.Reviewer = sdlc.ReviewerConfig{Backend: "codex", Login: "codex"}

	_, err := rig.svc.Bind(context.Background(), sdlcTestTenant, binding)
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

	if _, err := rig.svc.Bind(context.Background(), sdlcTestTenant, binding); err != nil {
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
		Backend:        "codex",
		Login:          "codex",
		TokenSecretRef: "sdlc/reviewer/go-luks2/token",
	}

	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, binding); err != nil {
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
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, binding); err != nil {
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
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, binding); err != nil {
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
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, binding); err != nil {
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
