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

package orchestration

import (
	"context"
	"errors"
	"testing"
	"time"
)

// =============================================================================
// host_affinity.go — strict-bit & resolve-error-holder context plumbing
// =============================================================================

func TestWithHostAffinityStrict_RoundTrip(t *testing.T) {
	ctx := WithHostAffinityStrict(context.Background())
	if !HostAffinityStrictFromContext(ctx) {
		t.Fatal("strict bit should be set after WithHostAffinityStrict")
	}
}

func TestHostAffinityStrictFromContext_DefaultsFalse(t *testing.T) {
	// nil context and an unset context both report false.
	if HostAffinityStrictFromContext(nil) {
		t.Fatal("nil context must report false")
	}
	if HostAffinityStrictFromContext(context.Background()) {
		t.Fatal("unset context must report false")
	}
}

func TestResolveErrorHolder_SetFirstWins(t *testing.T) {
	h := NewResolveErrorHolder()
	if h.Err() != nil {
		t.Fatal("fresh holder must have nil error")
	}

	first := errors.New("first")
	second := errors.New("second")
	h.Set(first)
	h.Set(second) // dropped — first non-nil wins

	if !errors.Is(h.Err(), first) {
		t.Fatalf("expected first error to win, got %v", h.Err())
	}
}

func TestResolveErrorHolder_SetNilAndNilReceiver(t *testing.T) {
	h := NewResolveErrorHolder()
	h.Set(nil) // no-op
	if h.Err() != nil {
		t.Fatal("Set(nil) must not record an error")
	}

	// nil receiver paths must be safe and report no error.
	var nilHolder *ResolveErrorHolder
	nilHolder.Set(errors.New("ignored"))
	if nilHolder.Err() != nil {
		t.Fatal("nil holder Err must be nil")
	}
}

func TestResolveErrorHolderContext_RoundTrip(t *testing.T) {
	h := NewResolveErrorHolder()
	ctx := WithResolveErrorHolder(context.Background(), h)
	got := ResolveErrorHolderFromContext(ctx)
	if got != h {
		t.Fatal("holder retrieved from context must be the same instance")
	}
}

func TestWithResolveErrorHolder_NilHolderReturnsSameCtx(t *testing.T) {
	base := context.Background()
	ctx := WithResolveErrorHolder(base, nil)
	if ctx != base {
		t.Fatal("nil holder must return the original context unchanged")
	}
	if ResolveErrorHolderFromContext(nil) != nil {
		t.Fatal("nil context must yield nil holder")
	}
	if ResolveErrorHolderFromContext(context.Background()) != nil {
		t.Fatal("context without a holder must yield nil")
	}
}

func TestStashResolveError(t *testing.T) {
	h := NewResolveErrorHolder()
	ctx := WithResolveErrorHolder(context.Background(), h)

	want := &NoMatchingAgentError{Constraint: "host_affinity", Detail: "host-x offline"}
	StashResolveError(ctx, want)
	if !errors.Is(h.Err(), ErrNoMatchingAgent) {
		t.Fatalf("expected stashed NoMatchingAgentError, got %v", h.Err())
	}

	// No holder on the context: must be a silent no-op (no panic).
	StashResolveError(context.Background(), errors.New("dropped"))
}

// =============================================================================
// orchestrator.go — GPU-required context plumbing
// =============================================================================

func TestWithGpuRequired_RoundTrip(t *testing.T) {
	ctx := WithGpuRequired(context.Background())
	if !GpuRequiredFromContext(ctx) {
		t.Fatal("GPU bit should be set after WithGpuRequired")
	}
}

func TestGpuRequiredFromContext_DefaultsFalse(t *testing.T) {
	if GpuRequiredFromContext(nil) {
		t.Fatal("nil context must report false")
	}
	if GpuRequiredFromContext(context.Background()) {
		t.Fatal("unset context must report false")
	}
}

func TestHostAffinityFromContext_EmptyAndNil(t *testing.T) {
	if HostAffinityFromContext(nil) != "" {
		t.Fatal("nil context must yield empty host affinity")
	}
	// WithHostAffinity ignores empty host, so the context stays unset.
	if HostAffinityFromContext(WithHostAffinity(context.Background(), "")) != "" {
		t.Fatal("empty host affinity must round-trip to empty")
	}
	if got := HostAffinityFromContext(WithHostAffinity(context.Background(), "host-7")); got != "host-7" {
		t.Fatalf("expected host-7, got %q", got)
	}
}

// =============================================================================
// orchestrator.go — session creator context helper
// =============================================================================

func TestWithSessionCreator_RoundTrip(t *testing.T) {
	ctx := WithSessionCreator(context.Background(), "operator-42")
	if got := sessionCreator(ctx); got != "operator-42" {
		t.Fatalf("expected operator-42, got %q", got)
	}
}

func TestWithSessionCreator_EmptyFallsBackToTenant(t *testing.T) {
	// Empty operator ID leaves the context unchanged.
	base := context.Background()
	if WithSessionCreator(base, "") != base {
		t.Fatal("empty operator ID must return the original context")
	}
	// sessionCreator falls back to the tenant when no creator is stamped.
	ctx := ContextWithTenant(context.Background(), "tenant-abc")
	if got := sessionCreator(ctx); got != "tenant-abc" {
		t.Fatalf("expected tenant fallback tenant-abc, got %q", got)
	}
}

// =============================================================================
// orchestrator.go — workflow delegation
// =============================================================================

func newWorkflowTestService(t *testing.T) *agentServiceImpl {
	t.Helper()
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	return svc.(*agentServiceImpl)
}

func newValidWorkflowDef(name string) *WorkflowDefinition {
	return &WorkflowDefinition{
		Name: name,
		Mode: WorkflowModeAutonomous,
		Agents: []AgentRoleSpec{
			{Role: "lead", Backend: "claude", Model: "claude-sonnet"},
		},
	}
}

func TestAgentService_Workflows_NilLoaderBehaviour(t *testing.T) {
	ctx := context.Background()
	svc := newWorkflowTestService(t)

	// ListWorkflows with no loader returns an empty (non-nil) slice.
	list, err := svc.ListWorkflows(ctx)
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %d", len(list))
	}

	// GetWorkflow with no loader returns WorkflowNotFoundError.
	if _, err := svc.GetWorkflow(ctx, "x"); !errors.Is(err, ErrWorkflowNotFound) {
		t.Fatalf("GetWorkflow: expected ErrWorkflowNotFound, got %v", err)
	}

	// Update/Delete with no loader return WorkflowNotFoundError.
	if err := svc.UpdateWorkflow(ctx, newValidWorkflowDef("u")); !errors.Is(err, ErrWorkflowNotFound) {
		t.Fatalf("UpdateWorkflow: expected ErrWorkflowNotFound, got %v", err)
	}
	if err := svc.DeleteWorkflow(ctx, "d"); !errors.Is(err, ErrWorkflowNotFound) {
		t.Fatalf("DeleteWorkflow: expected ErrWorkflowNotFound, got %v", err)
	}
}

func TestAgentService_Workflows_RegisterCreatesLoaderRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc := newWorkflowTestService(t)

	// RegisterWorkflow lazily creates the loader and stores the definition.
	def := newValidWorkflowDef("wf-1")
	if err := svc.RegisterWorkflow(ctx, def); err != nil {
		t.Fatalf("RegisterWorkflow: %v", err)
	}

	got, err := svc.GetWorkflow(ctx, "wf-1")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if got.Name != "wf-1" {
		t.Fatalf("expected wf-1, got %q", got.Name)
	}

	list, err := svc.ListWorkflows(ctx)
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected one workflow, got %d", len(list))
	}

	// Update an existing definition.
	def2 := newValidWorkflowDef("wf-1")
	def2.Settings.MaxIterations = 9
	if err := svc.UpdateWorkflow(ctx, def2); err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}
	got, _ = svc.GetWorkflow(ctx, "wf-1")
	if got.Settings.MaxIterations != 9 {
		t.Fatalf("expected MaxIterations=9 after update, got %d", got.Settings.MaxIterations)
	}

	// Delete it.
	if err := svc.DeleteWorkflow(ctx, "wf-1"); err != nil {
		t.Fatalf("DeleteWorkflow: %v", err)
	}
	if _, err := svc.GetWorkflow(ctx, "wf-1"); !errors.Is(err, ErrWorkflowNotFound) {
		t.Fatalf("expected workflow removed, got %v", err)
	}
}

// =============================================================================
// orchestrator.go — task artifact delegation
// =============================================================================

func TestAgentService_Artifacts_NoTaskStore(t *testing.T) {
	ctx := context.Background()
	svc := newWorkflowTestService(t)

	if err := svc.SubmitTaskArtifacts(ctx, "t", "task-1", nil); !errors.Is(err, ErrTaskStoreRequired) {
		t.Fatalf("SubmitTaskArtifacts: expected ErrTaskStoreRequired, got %v", err)
	}
	if _, err := svc.GetTaskArtifact(ctx, "t", "task-1", "a"); !errors.Is(err, ErrTaskStoreRequired) {
		t.Fatalf("GetTaskArtifact: expected ErrTaskStoreRequired, got %v", err)
	}
	if _, err := svc.ListTaskArtifacts(ctx, "t", "task-1"); !errors.Is(err, ErrTaskStoreRequired) {
		t.Fatalf("ListTaskArtifacts: expected ErrTaskStoreRequired, got %v", err)
	}
}

func TestAgentService_Artifacts_DelegateToTaskStore(t *testing.T) {
	ctx := context.Background()
	svc := newWorkflowTestService(t)
	svc.SetTaskStore(NewInMemoryTaskStore())

	// The store is wired; calls now reach it. With no task created the store
	// surfaces its own typed errors, proving delegation occurred.
	if err := svc.SubmitTaskArtifacts(ctx, "tenant-1", "missing-task", []Artifact{}); err == nil {
		t.Fatal("expected task store error for missing task")
	}
	if _, err := svc.GetTaskArtifact(ctx, "tenant-1", "missing-task", "a"); err == nil {
		t.Fatal("expected task store error for missing task")
	}
	if _, err := svc.ListTaskArtifacts(ctx, "tenant-1", "missing-task"); err == nil {
		t.Fatal("expected task store error for missing task")
	}
}

// =============================================================================
// orchestrator.go — session event log accessors & helpers
// =============================================================================

func TestAgentService_SessionEventLog_SetAndGet(t *testing.T) {
	svc := newWorkflowTestService(t)

	if svc.SessionEventLog() != nil {
		t.Fatal("expected nil session log on a fresh service")
	}
	log := NewMemorySessionEventLog()
	svc.SetSessionEventLog(log)
	if svc.SessionEventLog() != log {
		t.Fatal("SessionEventLog must return the installed instance")
	}
}

func TestAgentService_AppendSessionEvent(t *testing.T) {
	ctx := context.Background()
	svc := newWorkflowTestService(t)

	// No log installed: appendSessionEvent is a silent no-op.
	svc.appendSessionEvent(ctx, &SessionEvent{SessionID: "s", TenantID: "t", Sequence: 1})

	log := NewMemorySessionEventLog()
	svc.SetSessionEventLog(log)

	ev := &SessionEvent{
		SessionID: "sess-1",
		TenantID:  "tenant-1",
		Sequence:  1,
		Timestamp: time.Now().UTC(),
		Kind:      KindSystemMessage,
		System:    &SystemMessagePayload{Message: "hello"},
	}
	svc.appendSessionEvent(ctx, ev)

	got, _, err := log.Range(ctx, "tenant-1", "sess-1", 0, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one appended event, got %d", len(got))
	}

	// A caller-supplied sequence is ignored: appendSessionEvent assigns the
	// next one itself, so a stale value can never open a gap in the log.
	stale := &SessionEvent{
		SessionID: "sess-1",
		TenantID:  "tenant-1",
		Timestamp: time.Now().UTC(),
		Sequence:  99,
		Kind:      KindSystemMessage,
		System:    &SystemMessagePayload{Message: "stale"},
	}
	svc.appendSessionEvent(ctx, stale)
	if stale.Sequence != 2 {
		t.Fatalf("expected the assigned sequence to be 2, got %d", stale.Sequence)
	}
}

func TestAppendNextSessionEvent_AssignsSequences(t *testing.T) {
	ctx := context.Background()
	log := NewMemorySessionEventLog()

	for want := uint64(1); want <= 3; want++ {
		ev := &SessionEvent{
			SessionID: "sess-1",
			TenantID:  "tenant-1",
			Timestamp: time.Now().UTC(),
			Kind:      KindSystemMessage,
			System:    &SystemMessagePayload{Message: "x"},
		}
		if err := AppendNextSessionEvent(ctx, log, ev); err != nil {
			t.Fatalf("AppendNextSessionEvent: %v", err)
		}
		if ev.Sequence != want {
			t.Fatalf("expected sequence %d, got %d", want, ev.Sequence)
		}
	}
}

func TestAppendNextSessionEvent_InvalidInput(t *testing.T) {
	ctx := context.Background()

	if err := AppendNextSessionEvent(ctx, nil, &SessionEvent{SessionID: "s", TenantID: "t"}); err == nil {
		t.Fatal("expected an error when no event log is configured")
	}
	if err := AppendNextSessionEvent(ctx, NewMemorySessionEventLog(), nil); err == nil {
		t.Fatal("expected an error for a nil event")
	}
	// A malformed event still surfaces the log's own validation failure
	// rather than being retried into oblivion.
	err := AppendNextSessionEvent(ctx, NewMemorySessionEventLog(), &SessionEvent{TenantID: "t"})
	if err == nil {
		t.Fatal("expected an error for an event with no session ID")
	}
}

// =============================================================================
// orchestrator.go — Infer
// =============================================================================

func TestAgentService_Infer_Validation(t *testing.T) {
	ctx := context.Background()
	svc := newWorkflowTestService(t)

	if _, err := svc.Infer(ctx, nil); !errors.Is(err, ErrBackendRequired) {
		t.Fatalf("nil request: expected ErrBackendRequired, got %v", err)
	}
	if _, err := svc.Infer(ctx, &InferRequest{}); !errors.Is(err, ErrBackendRequired) {
		t.Fatalf("empty backend: expected ErrBackendRequired, got %v", err)
	}
	if _, err := svc.Infer(ctx, &InferRequest{Backend: "mock"}); !errors.Is(err, ErrPromptRequired) {
		t.Fatalf("empty prompt: expected ErrPromptRequired, got %v", err)
	}
}

func TestAgentService_Infer_SessionBackendUnsupported(t *testing.T) {
	ctx := context.Background()
	backend := &mockAgentBackend{
		backendType: "session-backend",
		caps:        BackendCapabilities{PersistentSessions: true},
	}
	svc, err := NewAgentService(
		NewInMemoryAgentStore(),
		map[string]AgentBackend{"session-backend": backend},
		&agentTestLogger{},
	)
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}

	_, err = svc.Infer(ctx, &InferRequest{Backend: "session-backend", Prompt: "hi"})
	var unsupported *InferUnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("expected InferUnsupportedError, got %T: %v", err, err)
	}
}

func TestAgentService_Infer_LocalBackendBridgesToChannel(t *testing.T) {
	ctx := context.Background()
	backend := &mockAgentBackend{
		backendType: "local",
		caps:        BackendCapabilities{PersistentSessions: false},
		promptFn: func(_ context.Context, req *PromptRequest) (*PromptResponse, error) {
			return &PromptResponse{
				Content:    []ContentBlock{{Type: "text", Text: "inference result"}},
				Model:      req.Model,
				StopReason: "end_turn",
			}, nil
		},
	}
	svc, err := NewAgentService(
		NewInMemoryAgentStore(),
		map[string]AgentBackend{"local": backend},
		&agentTestLogger{},
	)
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}

	ch, err := svc.Infer(ctx, &InferRequest{
		Backend:      "local",
		Prompt:       "compute",
		Model:        "m1",
		HostAffinity: "host-x", // exercises the WithHostAffinity branch
		GpuRequired:  true,     // exercises the WithGpuRequired branch
	})
	if err != nil {
		t.Fatalf("Infer: %v", err)
	}

	chunk, ok := <-ch
	if !ok {
		t.Fatal("expected one chunk, channel was closed empty")
	}
	if !chunk.Final {
		t.Fatal("bridged chunk must be final")
	}
	if len(chunk.Delta) != 1 || chunk.Delta[0].Text != "inference result" {
		t.Fatalf("unexpected chunk content: %+v", chunk.Delta)
	}
	if _, ok := <-ch; ok {
		t.Fatal("channel must be closed after the final chunk")
	}
}

func TestAgentService_Infer_BackendPromptError(t *testing.T) {
	ctx := context.Background()
	backendErr := errors.New("boom")
	backend := &mockAgentBackend{
		backendType: "local",
		caps:        BackendCapabilities{PersistentSessions: false},
		promptFn: func(_ context.Context, _ *PromptRequest) (*PromptResponse, error) {
			return nil, backendErr
		},
	}
	svc, err := NewAgentService(
		NewInMemoryAgentStore(),
		map[string]AgentBackend{"local": backend},
		&agentTestLogger{},
	)
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}

	_, err = svc.Infer(ctx, &InferRequest{Backend: "local", Prompt: "x"})
	var inferErr *InferBackendError
	if !errors.As(err, &inferErr) {
		t.Fatalf("expected InferBackendError, got %T: %v", err, err)
	}
	if !errors.Is(err, backendErr) {
		t.Fatalf("expected wrapped backend error, got %v", err)
	}
}

func TestAgentService_Infer_UnknownBackend(t *testing.T) {
	ctx := context.Background()
	svc := newWorkflowTestService(t)
	if _, err := svc.Infer(ctx, &InferRequest{Backend: "nope", Prompt: "x"}); err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

// =============================================================================
// project_authz.go — AuthorizeAgent (project-ID-only path) & findProject
// =============================================================================

// newScopedProjectInternal builds a Project with a non-empty AgentRoles map.
// (project_authz_test.go has an equivalent helper, but it lives in the
// external orchestration_test package and is not visible here.)
func newScopedProjectInternal(id, tenantID string, roles map[string]ProjectRole) *Project {
	return &Project{
		ID:       id,
		TenantID: tenantID,
		Name:     "test-project",
		Permissions: ProjectPermissions{
			AgentRoles: roles,
		},
	}
}

// idOnlyProjectStore is a ProjectStore that supports the empty-tenant
// "find by project ID" lookup used by ProjectAuthz.AuthorizeAgent. The
// in-memory store rejects an empty tenant, so the authz "unscoped find"
// path needs a store that understands the convention.
type idOnlyProjectStore struct {
	byID map[string]*Project
}

func (s *idOnlyProjectStore) Create(ctx context.Context, p *Project) error {
	if s.byID == nil {
		s.byID = map[string]*Project{}
	}
	s.byID[p.ID] = p
	return nil
}

func (s *idOnlyProjectStore) Get(ctx context.Context, _ string, projectID string) (*Project, error) {
	if p, ok := s.byID[projectID]; ok {
		return p, nil
	}
	return nil, &ProjectStoreNotFoundError{ProjectID: projectID}
}

func (s *idOnlyProjectStore) GetByName(ctx context.Context, _ string, _ string) (*Project, error) {
	return nil, &ProjectStoreNotFoundError{}
}

func (s *idOnlyProjectStore) Update(ctx context.Context, _ *Project) error { return nil }

func (s *idOnlyProjectStore) Delete(ctx context.Context, _ string, _ string) error { return nil }

func (s *idOnlyProjectStore) ListByTenant(ctx context.Context, _ string) ([]*Project, error) {
	return nil, nil
}

func TestProjectAuthz_AuthorizeAgent_Allowed(t *testing.T) {
	ctx := context.Background()
	store := &idOnlyProjectStore{}
	_ = store.Create(ctx, newScopedProjectInternal("p1", "t1", map[string]ProjectRole{
		"agent-A": ProjectRoleLead,
	}))
	authz := NewProjectAuthz(store)

	if err := authz.AuthorizeAgent(ctx, "p1", "agent-A", ProjectRoleWorker); err != nil {
		t.Fatalf("expected authorization to succeed, got %v", err)
	}
}

func TestProjectAuthz_AuthorizeAgent_Denied(t *testing.T) {
	ctx := context.Background()
	store := &idOnlyProjectStore{}
	_ = store.Create(ctx, newScopedProjectInternal("p2", "t1", map[string]ProjectRole{
		"agent-B": ProjectRoleObserver,
	}))
	authz := NewProjectAuthz(store)

	err := authz.AuthorizeAgent(ctx, "p2", "agent-B", ProjectRoleLead)
	var denied *ProjectAccessDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected ProjectAccessDeniedError, got %T: %v", err, err)
	}
}

func TestProjectAuthz_AuthorizeAgent_EmptyArgsAndInvalidRole(t *testing.T) {
	ctx := context.Background()
	authz := NewProjectAuthz(&idOnlyProjectStore{})

	// Empty project or agent → unscoped legacy path → nil.
	if err := authz.AuthorizeAgent(ctx, "", "agent", ProjectRoleLead); err != nil {
		t.Fatalf("empty projectID should be a no-op: %v", err)
	}
	if err := authz.AuthorizeAgent(ctx, "p", "", ProjectRoleLead); err != nil {
		t.Fatalf("empty agentID should be a no-op: %v", err)
	}

	// Invalid required role → ProjectRoleInvalidError.
	err := authz.AuthorizeAgent(ctx, "p", "a", ProjectRole("root"))
	var invalid *ProjectRoleInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected ProjectRoleInvalidError, got %T: %v", err, err)
	}
}

func TestProjectAuthz_AuthorizeAgent_MissingProjectRefused(t *testing.T) {
	ctx := context.Background()
	authz := NewProjectAuthz(&idOnlyProjectStore{})

	// Project not found → findProject returns (nil, nil) → refusal. A check
	// that could not read the role matrix must never answer "allowed".
	err := authz.AuthorizeAgent(ctx, "no-such", "agent", ProjectRoleLead)
	if err == nil {
		t.Fatal("expected a refusal for a project the store cannot produce, got nil")
	}
	var unresolved *ProjectUnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("expected *ProjectUnresolvedError, got %T: %v", err, err)
	}
	if unresolved.ProjectID != "no-such" || unresolved.AgentID != "agent" {
		t.Errorf("refusal names project=%q agent=%q, want project=%q agent=%q",
			unresolved.ProjectID, unresolved.AgentID, "no-such", "agent")
	}
}

// errProjectStore returns an arbitrary (non-not-found) error from Get so the
// findProject error-propagation branch is exercised.
type errProjectStore struct{ err error }

func (s *errProjectStore) Create(context.Context, *Project) error { return s.err }
func (s *errProjectStore) Get(context.Context, string, string) (*Project, error) {
	return nil, s.err
}
func (s *errProjectStore) GetByName(context.Context, string, string) (*Project, error) {
	return nil, s.err
}
func (s *errProjectStore) Update(context.Context, *Project) error       { return s.err }
func (s *errProjectStore) Delete(context.Context, string, string) error { return s.err }
func (s *errProjectStore) ListByTenant(context.Context, string) ([]*Project, error) {
	return nil, s.err
}

func TestProjectAuthz_AuthorizeAgent_StorePropagatesError(t *testing.T) {
	ctx := context.Background()
	storeErr := errors.New("store unavailable")
	authz := NewProjectAuthz(&errProjectStore{err: storeErr})

	if err := authz.AuthorizeAgent(ctx, "p", "a", ProjectRoleWorker); !errors.Is(err, storeErr) {
		t.Fatalf("expected store error to propagate, got %v", err)
	}
}

// =============================================================================
// workflow_loader.go — persistor wiring (SetPersistor / Rehydrate / Update / Delete)
// =============================================================================

// memWorkflowPersistor is an in-memory WorkflowPersistor for testing.
type memWorkflowPersistor struct {
	saved     map[string]*WorkflowDefinition
	saveErr   error
	removeErr error
	loadErr   error
}

func newMemWorkflowPersistor() *memWorkflowPersistor {
	return &memWorkflowPersistor{saved: map[string]*WorkflowDefinition{}}
}

func (p *memWorkflowPersistor) Save(_ context.Context, def *WorkflowDefinition) error {
	if p.saveErr != nil {
		return p.saveErr
	}
	p.saved[def.Name] = def
	return nil
}

func (p *memWorkflowPersistor) Remove(_ context.Context, _ string, name string) error {
	if p.removeErr != nil {
		return p.removeErr
	}
	delete(p.saved, name)
	return nil
}

func (p *memWorkflowPersistor) Load(_ context.Context, tenantID string) ([]*WorkflowDefinition, error) {
	if p.loadErr != nil {
		return nil, p.loadErr
	}
	out := make([]*WorkflowDefinition, 0, len(p.saved))
	for _, d := range p.saved {
		if d.TenantID == tenantID {
			out = append(out, d)
		}
	}
	return out, nil
}

func (p *memWorkflowPersistor) LoadAll(_ context.Context) ([]*WorkflowDefinition, error) {
	if p.loadErr != nil {
		return nil, p.loadErr
	}
	out := make([]*WorkflowDefinition, 0, len(p.saved))
	for _, d := range p.saved {
		out = append(out, d)
	}
	return out, nil
}

func TestWorkflowLoader_Rehydrate_NoPersistor(t *testing.T) {
	loader := NewWorkflowLoader()
	n, err := loader.Rehydrate(context.Background(), SystemTenantID)
	if err != nil {
		t.Fatalf("Rehydrate without persistor should be a no-op: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 loaded, got %d", n)
	}
}

func TestWorkflowLoader_Rehydrate_LoadsFromPersistor(t *testing.T) {
	ctx := context.Background()
	persistor := newMemWorkflowPersistor()
	persistor.saved["a"] = newValidWorkflowDef("a")
	persistor.saved["b"] = newValidWorkflowDef("b")
	// A junk entry that must be skipped during rehydrate.
	persistor.saved["junk"] = &WorkflowDefinition{Name: ""}
	for _, d := range persistor.saved {
		d.TenantID = SystemTenantID
	}

	loader := NewWorkflowLoader()
	// Pre-seed an in-memory entry that should be cleared by Rehydrate.
	if err := loader.Register(SystemTenantID, newValidWorkflowDef("stale")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	loader.SetPersistor(persistor)

	n, err := loader.Rehydrate(ctx, SystemTenantID)
	if err != nil {
		t.Fatalf("Rehydrate: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 entries reported (raw count), got %d", n)
	}
	if _, err := loader.Get(SystemTenantID, "a"); err != nil {
		t.Fatalf("expected 'a' loaded: %v", err)
	}
	if _, err := loader.Get(SystemTenantID, "stale"); err == nil {
		t.Fatal("expected pre-existing 'stale' entry to be cleared")
	}
}

func TestWorkflowLoader_Rehydrate_PersistorError(t *testing.T) {
	persistor := newMemWorkflowPersistor()
	persistor.loadErr = errors.New("load failed")

	loader := NewWorkflowLoader()
	loader.SetPersistor(persistor)

	if _, err := loader.Rehydrate(context.Background(), SystemTenantID); !errors.Is(err, persistor.loadErr) {
		t.Fatalf("expected load error, got %v", err)
	}
}

func TestWorkflowLoader_Update_WithPersistor(t *testing.T) {
	persistor := newMemWorkflowPersistor()
	loader := NewWorkflowLoader()
	loader.SetPersistor(persistor)

	if err := loader.Register(SystemTenantID, newValidWorkflowDef("wf")); err != nil {
		t.Fatalf("Register: %v", err)
	}

	updated := newValidWorkflowDef("wf")
	updated.Settings.MaxIterations = 7
	if err := loader.Update(SystemTenantID, updated); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if persistor.saved["wf"].Settings.MaxIterations != 7 {
		t.Fatal("persistor should hold the updated definition")
	}
}

func TestWorkflowLoader_Update_NotFound(t *testing.T) {
	loader := NewWorkflowLoader()
	if err := loader.Update(SystemTenantID, newValidWorkflowDef("ghost")); !errors.Is(err, ErrWorkflowNotFound) {
		t.Fatalf("expected ErrWorkflowNotFound, got %v", err)
	}
}

func TestWorkflowLoader_Update_PersistorErrorRollsBack(t *testing.T) {
	persistor := newMemWorkflowPersistor()
	loader := NewWorkflowLoader()

	orig := newValidWorkflowDef("wf")
	orig.Settings.MaxIterations = 1
	if err := loader.Register(SystemTenantID, orig); err != nil {
		t.Fatalf("Register: %v", err)
	}
	loader.SetPersistor(persistor)
	persistor.saveErr = errors.New("disk full")

	updated := newValidWorkflowDef("wf")
	updated.Settings.MaxIterations = 99
	if err := loader.Update(SystemTenantID, updated); !errors.Is(err, persistor.saveErr) {
		t.Fatalf("expected persistor save error, got %v", err)
	}
	// In-memory definition must be rolled back to the original.
	got, err := loader.Get(SystemTenantID, "wf")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Settings.MaxIterations != 1 {
		t.Fatalf("expected rollback to MaxIterations=1, got %d", got.Settings.MaxIterations)
	}
}

func TestWorkflowLoader_Delete_WithPersistor(t *testing.T) {
	persistor := newMemWorkflowPersistor()
	loader := NewWorkflowLoader()
	loader.SetPersistor(persistor)

	if err := loader.Register(SystemTenantID, newValidWorkflowDef("wf")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := loader.Delete(SystemTenantID, "wf"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := persistor.saved["wf"]; ok {
		t.Fatal("persistor should have removed the definition")
	}
}

func TestWorkflowLoader_Delete_NotFound(t *testing.T) {
	loader := NewWorkflowLoader()
	if err := loader.Delete(SystemTenantID, "ghost"); !errors.Is(err, ErrWorkflowNotFound) {
		t.Fatalf("expected ErrWorkflowNotFound, got %v", err)
	}
}

func TestWorkflowLoader_Delete_PersistorErrorRestores(t *testing.T) {
	persistor := newMemWorkflowPersistor()
	loader := NewWorkflowLoader()
	if err := loader.Register(SystemTenantID, newValidWorkflowDef("wf")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	loader.SetPersistor(persistor)
	persistor.removeErr = errors.New("remove failed")

	if err := loader.Delete(SystemTenantID, "wf"); !errors.Is(err, persistor.removeErr) {
		t.Fatalf("expected persistor remove error, got %v", err)
	}
	// In-memory entry must be restored after the failed remove.
	if _, err := loader.Get(SystemTenantID, "wf"); err != nil {
		t.Fatalf("expected definition restored after remove failure: %v", err)
	}
}

// =============================================================================
// control_loop.go — host-affinity strict default
// =============================================================================

func TestControlLoop_SetHostAffinityStrictDefault(t *testing.T) {
	svc := newWorkflowTestService(t)
	cl := NewControlLoop(svc, &agentTestLogger{})

	if cl.hostAffinityStrictDefault {
		t.Fatal("strict default should start false")
	}
	cl.SetHostAffinityStrictDefault(true)
	if !cl.hostAffinityStrictDefault {
		t.Fatal("strict default should be true after Set")
	}
	cl.SetHostAffinityStrictDefault(false)
	if cl.hostAffinityStrictDefault {
		t.Fatal("strict default should be false after reset")
	}
}

// =============================================================================
// ollama_backend.go — ListModels (nil service path)
// =============================================================================

func TestOllamaBackend_ListModels_NoModelService(t *testing.T) {
	b := NewOllamaBackend(OllamaBackendConfig{})
	names, err := b.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("expected empty slice with no model service, got %v", names)
	}
}

// =============================================================================
// session_log.go / session_log_kv.go — small helpers
// =============================================================================

func TestSessionLogFanout_SubscriberCount(t *testing.T) {
	f := newSessionLogFanout()
	if got := f.subscriberCount("t1", "s1"); got != 0 {
		t.Fatalf("expected 0 subscribers, got %d", got)
	}

	_, cancel, err := f.subscribe("t1", "s1")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if got := f.subscriberCount("t1", "s1"); got != 1 {
		t.Fatalf("expected 1 subscriber, got %d", got)
	}
	cancel()
	if got := f.subscriberCount("t1", "s1"); got != 0 {
		t.Fatalf("expected 0 subscribers after cancel, got %d", got)
	}
}

func TestEncodeSeqBE(t *testing.T) {
	b := encodeSeqBE(0x0102030405060708)
	want := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	if len(b) != 8 {
		t.Fatalf("expected 8 bytes, got %d", len(b))
	}
	for i := range want {
		if b[i] != want[i] {
			t.Fatalf("byte %d = 0x%02x, want 0x%02x", i, b[i], want[i])
		}
	}
}

func TestSinceDuration(t *testing.T) {
	// Zero time → zero duration.
	if d := sinceDuration(time.Time{}); d != 0 {
		t.Fatalf("zero time must yield zero duration, got %v", d)
	}
	// A time in the past → strictly positive duration.
	past := time.Now().Add(-time.Hour)
	if d := sinceDuration(past); d <= 0 {
		t.Fatalf("past time must yield positive duration, got %v", d)
	}
}

// =============================================================================
// artifact.go — itoa64 negative path
// =============================================================================

func TestItoa64_NegativeAndZero(t *testing.T) {
	cases := map[int64]string{
		0:        "0",
		-1:       "-1",
		-123456:  "-123456",
		42:       "42",
		-9999999: "-9999999",
	}
	for in, want := range cases {
		if got := itoa64(in); got != want {
			t.Fatalf("itoa64(%d) = %q, want %q", in, got, want)
		}
	}
}
