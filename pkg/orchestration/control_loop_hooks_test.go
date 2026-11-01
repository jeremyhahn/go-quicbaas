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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Mock CostTracker for hook tests
// =============================================================================

type hookMockCostTracker struct {
	status    BudgetStatus
	statusErr error
}

func (m *hookMockCostTracker) RecordTokenUsage(_ context.Context, _, _, _ string, _ int64) error {
	return nil
}

func (m *hookMockCostTracker) GetJobUsage(_ context.Context, _, _ string) (*UsageReport, error) {
	return nil, nil
}

func (m *hookMockCostTracker) GetAgentUsage(_ context.Context, _, _, _ string) (*UsageReport, error) {
	return nil, nil
}

func (m *hookMockCostTracker) CheckBudget(_ context.Context, _, _ string) (BudgetStatus, error) {
	return m.status, m.statusErr
}

func (m *hookMockCostTracker) RecordTurnUsage(_ context.Context, _ TurnUsage) error {
	return nil
}

func (m *hookMockCostTracker) GetAccountUsage(_ context.Context, _, _ string) (*UsageReport, error) {
	return nil, nil
}

func (m *hookMockCostTracker) GetProjectUsage(_ context.Context, _, _ string) (*UsageReport, error) {
	return nil, nil
}

func (m *hookMockCostTracker) AccountTokens(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

// =============================================================================
// Mock PeerMessagingService for hook tests
// =============================================================================

type hookMockPeerMessaging struct {
	subscribeFn func(ctx context.Context, tenantID, agentID string) (<-chan *PeerMessage, error)
}

func (m *hookMockPeerMessaging) Send(_ context.Context, _ *PeerMessage) error {
	return nil
}

func (m *hookMockPeerMessaging) Poll(_ context.Context, _, _ string, _ int64) ([]*PeerMessage, error) {
	return nil, nil
}

func (m *hookMockPeerMessaging) Subscribe(ctx context.Context, tenantID, agentID string) (<-chan *PeerMessage, error) {
	if m.subscribeFn != nil {
		return m.subscribeFn(ctx, tenantID, agentID)
	}
	ch := make(chan *PeerMessage, 1)
	return ch, nil
}

func (m *hookMockPeerMessaging) Acknowledge(_ context.Context, _, _, _ string) error {
	return nil
}

func (m *hookMockPeerMessaging) InboxSize(_ context.Context, _, _ string) (int, error) {
	return 0, nil
}

// =============================================================================
// Mock Logger for hook tests
// =============================================================================

type hookTestLogger struct {
	warnings []string
	mu       sync.Mutex
}

func (l *hookTestLogger) Debug(_ string, _ ...any)          {}
func (l *hookTestLogger) Debugf(_ string, _ ...interface{}) {}
func (l *hookTestLogger) Info(_ string, _ ...any)           {}
func (l *hookTestLogger) Infof(_ string, _ ...interface{})  {}
func (l *hookTestLogger) Warn(_ string, _ ...any)           {}
func (l *hookTestLogger) Warnf(format string, _ ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warnings = append(l.warnings, format)
}
func (l *hookTestLogger) Error(_ string, _ ...any)                              {}
func (l *hookTestLogger) Errorf(_ string, _ ...interface{})                     {}
func (l *hookTestLogger) Fatal(_ string)                                        {}
func (l *hookTestLogger) Fatalf(_ string, _ ...interface{})                     {}
func (l *hookTestLogger) WithField(_ string, _ interface{}) interfaces.Logger   { return l }
func (l *hookTestLogger) WithFields(_ map[string]interface{}) interfaces.Logger { return l }
func (l *hookTestLogger) WithError(_ error) interfaces.Logger                   { return l }

// =============================================================================
// HookCheckBudget Tests
// =============================================================================

func TestHookCheckBudget_TrackerStatesAndActions(t *testing.T) {
	tests := []struct {
		name        string
		tracker     *hookMockCostTracker
		action      string
		wantStatus  BudgetStatus
		wantAction  string
		wantWarnLog bool
	}{
		{"ok", &hookMockCostTracker{status: BudgetOK}, BudgetActionWarn, BudgetOK, "", false},
		{"budget not set is ok", &hookMockCostTracker{statusErr: ErrBudgetNotSet}, "", BudgetOK, "", false},
		{"warning", &hookMockCostTracker{status: BudgetWarning}, BudgetActionWarn, BudgetWarning, "", true},
		{"exceeded shutdown", &hookMockCostTracker{status: BudgetExceeded}, BudgetActionShutdown, BudgetExceeded, BudgetActionShutdown, true},
		{"exceeded pause", &hookMockCostTracker{status: BudgetExceeded}, BudgetActionPause, BudgetExceeded, BudgetActionPause, true},
		{"exceeded warn", &hookMockCostTracker{status: BudgetExceeded}, BudgetActionWarn, BudgetExceeded, BudgetActionWarn, true},
		{"exceeded defaults to warn", &hookMockCostTracker{status: BudgetExceeded}, "", BudgetExceeded, BudgetActionWarn, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &hookTestLogger{}
			settings := CostControlSettings{BudgetAction: tt.action}

			result, err := HookCheckBudget(context.Background(), tt.tracker, settings, "tenant-1", "job-1", logger)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if result.Status != tt.wantStatus {
				t.Errorf("expected status %q, got %q", tt.wantStatus, result.Status)
			}
			if result.Action != tt.wantAction {
				t.Errorf("expected action %q, got %q", tt.wantAction, result.Action)
			}

			logger.mu.Lock()
			defer logger.mu.Unlock()
			if gotWarnLog := len(logger.warnings) > 0; gotWarnLog != tt.wantWarnLog {
				t.Errorf("expected warning log %v, got %v", tt.wantWarnLog, gotWarnLog)
			}
		})
	}
}

func TestHookCheckBudget_NoTracker(t *testing.T) {
	logger := &hookTestLogger{}
	settings := CostControlSettings{BudgetAction: BudgetActionShutdown}

	result, err := HookCheckBudget(context.Background(), nil, settings, "tenant-1", "job-1", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result when tracker is nil, got %+v", result)
	}
}

func TestHookCheckBudget_TrackerError(t *testing.T) {
	trackerErr := errors.New("connection refused")
	tracker := &hookMockCostTracker{statusErr: trackerErr}
	logger := &hookTestLogger{}
	settings := CostControlSettings{}

	result, err := HookCheckBudget(context.Background(), tracker, settings, "tenant-1", "job-1", logger)
	if err == nil {
		t.Fatal("expected error when tracker returns error")
	}
	if result != nil {
		t.Errorf("expected nil result on error, got %+v", result)
	}
	if !errors.Is(err, ErrHookBudgetCheck) {
		t.Errorf("expected error to wrap ErrHookBudgetCheck, got %v", err)
	}
}

func TestHookCheckBudget_BudgetNotSetError(t *testing.T) {
	// ErrBudgetNotSet is non-fatal and should return BudgetOK.
	tracker := &hookMockCostTracker{statusErr: ErrBudgetNotSet}
	logger := &hookTestLogger{}
	settings := CostControlSettings{}

	result, err := HookCheckBudget(context.Background(), tracker, settings, "tenant-1", "job-1", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Status != BudgetOK {
		t.Errorf("expected status %q for budget not set, got %q", BudgetOK, result.Status)
	}
}

// =============================================================================
// HookRecoveryBootstrap Tests
// =============================================================================

func TestHookRecoveryBootstrap_CorrectOrdering(t *testing.T) {
	var order []string
	var mu sync.Mutex

	recordStep := func(name string) func(ctx context.Context) error {
		return func(_ context.Context) error {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, name)
			return nil
		}
	}

	steps := []RecoveryStep{
		{Name: "restore_permissions", Fn: recordStep("restore_permissions")},
		{Name: "scan_fleet", Fn: recordStep("scan_fleet")},
		{Name: "notify_lead", Fn: recordStep("notify_lead")},
		{Name: "subscribe_cleanup", Fn: recordStep("subscribe_cleanup")},
		{Name: "restart_agents", Fn: recordStep("restart_agents")},
	}

	err := HookRecoveryBootstrap(context.Background(), steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{
		"restore_permissions",
		"scan_fleet",
		"notify_lead",
		"subscribe_cleanup",
		"restart_agents",
	}

	mu.Lock()
	defer mu.Unlock()

	if len(order) != len(expected) {
		t.Fatalf("expected %d steps executed, got %d", len(expected), len(order))
	}
	for i, name := range expected {
		if order[i] != name {
			t.Errorf("step %d: expected %q, got %q", i, name, order[i])
		}
	}
}

func TestHookRecoveryBootstrap_StopOnError(t *testing.T) {
	stepErr := errors.New("fleet scan failed")
	var executedSteps []string
	var mu sync.Mutex

	steps := []RecoveryStep{
		{Name: "restore_permissions", Fn: func(_ context.Context) error {
			mu.Lock()
			defer mu.Unlock()
			executedSteps = append(executedSteps, "restore_permissions")
			return nil
		}},
		{Name: "scan_fleet", Fn: func(_ context.Context) error {
			mu.Lock()
			defer mu.Unlock()
			executedSteps = append(executedSteps, "scan_fleet")
			return stepErr
		}},
		{Name: "notify_lead", Fn: func(_ context.Context) error {
			mu.Lock()
			defer mu.Unlock()
			executedSteps = append(executedSteps, "notify_lead")
			return nil
		}},
	}

	err := HookRecoveryBootstrap(context.Background(), steps)
	if err == nil {
		t.Fatal("expected error when step fails")
	}
	if !errors.Is(err, ErrHookRecoveryBootstrap) {
		t.Errorf("expected error to wrap ErrHookRecoveryBootstrap, got %v", err)
	}

	// Verify only the first two steps executed (third was skipped).
	mu.Lock()
	defer mu.Unlock()
	if len(executedSteps) != 2 {
		t.Errorf("expected 2 steps executed, got %d: %v", len(executedSteps), executedSteps)
	}

	// Verify error contains the step name.
	var bootstrapErr *HookRecoveryBootstrapError
	if errors.As(err, &bootstrapErr) {
		if bootstrapErr.Step != "scan_fleet" {
			t.Errorf("expected step %q in error, got %q", "scan_fleet", bootstrapErr.Step)
		}
	}
}

func TestHookRecoveryBootstrap_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	steps := []RecoveryStep{
		{Name: "first_step", Fn: func(_ context.Context) error {
			return nil
		}},
	}

	err := HookRecoveryBootstrap(ctx, steps)
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
	if !errors.Is(err, ErrHookRecoveryBootstrap) {
		t.Errorf("expected error to wrap ErrHookRecoveryBootstrap, got %v", err)
	}
}

// =============================================================================
// HookTriggerReindex Tests
// =============================================================================

func TestHookTriggerReindex_Triggers(t *testing.T) {
	tests := []struct {
		name       string
		triggers   []string
		wantCalled bool
	}{
		{"iteration complete", []string{"manual", "iteration_complete"}, true},
		{"no triggers", nil, false},
		{"iteration complete absent", []string{"manual", "on_demand"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var called bool
			reindexFn := func(_ context.Context) error {
				called = true
				return nil
			}

			err := HookTriggerReindex(context.Background(), reindexFn, tt.triggers)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if called != tt.wantCalled {
				t.Errorf("expected reindexFn called %v, got %v", tt.wantCalled, called)
			}
		})
	}
}

func TestHookTriggerReindex_NilReindexFn(t *testing.T) {
	triggers := []string{"iteration_complete"}

	err := HookTriggerReindex(context.Background(), nil, triggers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHookTriggerReindex_ReindexError(t *testing.T) {
	reindexErr := errors.New("indexing service unavailable")
	reindexFn := func(_ context.Context) error {
		return reindexErr
	}

	triggers := []string{"iteration_complete"}

	err := HookTriggerReindex(context.Background(), reindexFn, triggers)
	if err == nil {
		t.Fatal("expected error when reindexFn returns error")
	}
	if !errors.Is(err, ErrHookReindex) {
		t.Errorf("expected error to wrap ErrHookReindex, got %v", err)
	}
}

// =============================================================================
// HookStartPeerDelivery Tests
// =============================================================================

func TestHookStartPeerDelivery_Success(t *testing.T) {
	var delivered atomic.Int64

	msgCh := make(chan *PeerMessage, 1)
	mockSvc := &hookMockPeerMessaging{
		subscribeFn: func(_ context.Context, _, _ string) (<-chan *PeerMessage, error) {
			return msgCh, nil
		},
	}

	deliverFn := func(_ context.Context, _ string, _ *PeerMessage) {
		delivered.Add(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cancels, err := HookStartPeerDelivery(ctx, mockSvc, "tenant-1", []string{"agent-1"}, deliverFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cancels) != 1 {
		t.Fatalf("expected 1 cancel function, got %d", len(cancels))
	}

	// Send a message and verify delivery.
	msgCh <- &PeerMessage{ID: "msg-1", From: "agent-2", Action: "advisory"}

	// Wait for delivery using a channel signal.
	done := make(chan struct{})
	go func() {
		for delivered.Load() == 0 {
			// spin briefly for test convergence
		}
		close(done)
	}()

	select {
	case <-done:
		// Delivered successfully.
	case <-ctx.Done():
		t.Fatal("timed out waiting for message delivery")
	}

	if delivered.Load() != 1 {
		t.Errorf("expected 1 delivery, got %d", delivered.Load())
	}

	// Cancel and verify goroutine cleanup.
	cancels[0]()
}

func TestHookStartPeerDelivery_NilService(t *testing.T) {
	deliverFn := func(_ context.Context, _ string, _ *PeerMessage) {}

	cancels, err := HookStartPeerDelivery(context.Background(), nil, "tenant-1", []string{"agent-1"}, deliverFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cancels != nil {
		t.Errorf("expected nil cancels for nil service, got %d", len(cancels))
	}
}

func TestHookStartPeerDelivery_NilDeliverFn(t *testing.T) {
	mockSvc := &hookMockPeerMessaging{}

	cancels, err := HookStartPeerDelivery(context.Background(), mockSvc, "tenant-1", []string{"agent-1"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cancels != nil {
		t.Errorf("expected nil cancels for nil deliverFn, got %d", len(cancels))
	}
}

func TestHookStartPeerDelivery_SubscribeError(t *testing.T) {
	subscribeErr := errors.New("subscription failed")
	mockSvc := &hookMockPeerMessaging{
		subscribeFn: func(_ context.Context, _, agentID string) (<-chan *PeerMessage, error) {
			if agentID == "agent-2" {
				return nil, subscribeErr
			}
			ch := make(chan *PeerMessage, 1)
			return ch, nil
		},
	}

	deliverFn := func(_ context.Context, _ string, _ *PeerMessage) {}

	cancels, err := HookStartPeerDelivery(
		context.Background(),
		mockSvc,
		"tenant-1",
		[]string{"agent-1", "agent-2"},
		deliverFn,
	)
	if err == nil {
		t.Fatal("expected error when subscribe fails")
	}
	if !errors.Is(err, ErrHookPeerDelivery) {
		t.Errorf("expected error to wrap ErrHookPeerDelivery, got %v", err)
	}
	if cancels != nil {
		t.Errorf("expected nil cancels on error, got %d", len(cancels))
	}
}

func TestHookStartPeerDelivery_MultipleAgents(t *testing.T) {
	mockSvc := &hookMockPeerMessaging{
		subscribeFn: func(_ context.Context, _, _ string) (<-chan *PeerMessage, error) {
			ch := make(chan *PeerMessage, 1)
			return ch, nil
		},
	}

	deliverFn := func(_ context.Context, _ string, _ *PeerMessage) {}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cancels, err := HookStartPeerDelivery(ctx, mockSvc, "tenant-1",
		[]string{"agent-1", "agent-2", "agent-3"}, deliverFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cancels) != 3 {
		t.Fatalf("expected 3 cancel functions, got %d", len(cancels))
	}

	// Clean up all goroutines.
	for _, c := range cancels {
		c()
	}
}

func TestHookStartPeerDelivery_ChannelClosed(t *testing.T) {
	var delivered atomic.Int64

	msgCh := make(chan *PeerMessage, 1)
	mockSvc := &hookMockPeerMessaging{
		subscribeFn: func(_ context.Context, _, _ string) (<-chan *PeerMessage, error) {
			return msgCh, nil
		},
	}

	deliverFn := func(_ context.Context, _ string, _ *PeerMessage) {
		delivered.Add(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cancels, err := HookStartPeerDelivery(ctx, mockSvc, "tenant-1", []string{"agent-1"}, deliverFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cancels) != 1 {
		t.Fatalf("expected 1 cancel function, got %d", len(cancels))
	}

	// Close channel to trigger goroutine exit.
	close(msgCh)

	// The goroutine should exit cleanly. No assertion needed beyond no panic.
	cancel()
}

// =============================================================================
// Error Type Tests
// =============================================================================

func TestHookErrors_UnwrapAndMessage(t *testing.T) {
	cause := errors.New("cause")
	tests := []struct {
		name     string
		err      error
		sentinel error
	}{
		{"budget check with cause", &HookBudgetCheckError{TenantID: "t1", JobID: "j1", Cause: cause}, ErrHookBudgetCheck},
		{"budget check nil cause", &HookBudgetCheckError{TenantID: "t1", JobID: "j1"}, ErrHookBudgetCheck},
		{"recovery bootstrap with cause", &HookRecoveryBootstrapError{Step: "scan_fleet", Cause: cause}, ErrHookRecoveryBootstrap},
		{"recovery bootstrap nil cause", &HookRecoveryBootstrapError{Step: "scan_fleet"}, ErrHookRecoveryBootstrap},
		{"peer delivery with cause", &HookPeerDeliveryError{AgentID: "agent-1", Cause: cause}, ErrHookPeerDelivery},
		{"peer delivery nil cause", &HookPeerDeliveryError{AgentID: "agent-1"}, ErrHookPeerDelivery},
		{"reindex with cause", &HookReindexError{Cause: cause}, ErrHookReindex},
		{"reindex nil cause", &HookReindexError{}, ErrHookReindex},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.err, tt.sentinel) {
				t.Errorf("expected error to unwrap to %v", tt.sentinel)
			}
			if tt.err.Error() == "" {
				t.Error("expected non-empty error message")
			}
		})
	}
}

// =============================================================================
// Lifecycle seams: the five moments, bound-task mode, stage reporting
// =============================================================================

// recordingHooks is a JobLifecycleHooks that records every call in order,
// with what it was told, and answers as configured.
type recordingHooks struct {
	mu    sync.Mutex
	calls []string

	workDir    string
	prepareErr error
	prepareFn  func()
	completeEr error
	discardFn  func(ctx context.Context, job *JobContext, completed bool) error

	discardCtxErr   error
	discardDeadline bool
	discardJobDir   string
	blockedReason   BlockReason
	prepareSaw      *JobContext
}

func (h *recordingHooks) record(call string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, call)
}

func (h *recordingHooks) Calls() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.calls...)
}

func (h *recordingHooks) Prepare(_ context.Context, job *JobContext) (string, error) {
	h.record("prepare")
	h.mu.Lock()
	h.prepareSaw = job
	fn := h.prepareFn
	h.mu.Unlock()
	if fn != nil {
		fn()
	}
	return h.workDir, h.prepareErr
}

func (h *recordingHooks) Reviewable(_ context.Context, _ *JobContext) error {
	h.record("reviewable")
	return nil
}

func (h *recordingHooks) Complete(_ context.Context, _ *JobContext) error {
	h.record("complete")
	return h.completeEr
}

func (h *recordingHooks) Discard(ctx context.Context, job *JobContext, completed bool) error {
	if completed {
		h.record("discard(completed)")
	} else {
		h.record("discard(not completed)")
	}
	h.mu.Lock()
	h.discardCtxErr = ctx.Err()
	_, h.discardDeadline = ctx.Deadline()
	h.discardJobDir = job.WorkDir
	h.mu.Unlock()
	if h.discardFn != nil {
		return h.discardFn(ctx, job, completed)
	}
	return nil
}

func (h *recordingHooks) Blocked(_ context.Context, _ *JobContext, reason BlockReason) error {
	h.record("blocked")
	h.mu.Lock()
	h.blockedReason = reason
	h.mu.Unlock()
	return nil
}

// spawnRecordingService records every SpawnSessionRequest the loop makes.
type spawnRecordingService struct {
	*controlLoopMockService
	mu     sync.Mutex
	spawns []*SpawnSessionRequest
}

func (s *spawnRecordingService) SpawnSession(ctx context.Context, req *SpawnSessionRequest) (*SpawnSessionResponse, error) {
	s.mu.Lock()
	s.spawns = append(s.spawns, req)
	s.mu.Unlock()
	return s.controlLoopMockService.SpawnSession(ctx, req)
}

func (s *spawnRecordingService) Spawns() []*SpawnSessionRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*SpawnSessionRequest(nil), s.spawns...)
}

// credentialRefusingService is an agent service that REFUSES to spawn a
// pooled-account backend session that names no credential — the way the
// real backend fails closed when nothing resolved an account — so a loop
// that forgets to bind an account cannot pass a test through it.
type credentialRefusingService struct {
	*controlLoopMockService
	mu     sync.Mutex
	spawns []*SpawnSessionRequest
}

func credentialGuarded(base *controlLoopMockService) *credentialRefusingService {
	return &credentialRefusingService{controlLoopMockService: base}
}

func (s *credentialRefusingService) SpawnSession(ctx context.Context, req *SpawnSessionRequest) (*SpawnSessionResponse, error) {
	s.mu.Lock()
	s.spawns = append(s.spawns, req)
	s.mu.Unlock()
	if WorkerSpawnAccountAuto(req.Backend, req.Model) && !req.AccountAuto && req.AccountID == "" {
		return nil, &AccountResolverNotConfiguredError{Backend: req.Backend}
	}
	return s.controlLoopMockService.SpawnSession(ctx, req)
}

func (s *credentialRefusingService) Spawns() []*SpawnSessionRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*SpawnSessionRequest(nil), s.spawns...)
}

// leadDrivenService is the stateless happy path: one decomposition, then a
// review answered through DecomposeTask, tasks listed as completed.
func leadDrivenService(reviewJSON string) *controlLoopMockService {
	svc := &controlLoopMockService{backends: newTestBackends(), agents: newTestAgents()}
	decomposeCalls := 0
	svc.decomposeFn = func() (*DecomposeTaskResponse, error) {
		decomposeCalls++
		if decomposeCalls == 1 {
			return &DecomposeTaskResponse{WorkItems: []*WorkItem{{ID: "w1", Title: "Task 1", Description: "Do task 1"}}}, nil
		}
		return &DecomposeTaskResponse{WorkItems: []*WorkItem{{ID: "review-raw", Description: reviewJSON}}}, nil
	}
	svc.listTasksFn = func() ([]*Task, error) {
		tasks := make([]*Task, 0, len(svc.createdTasks))
		for _, ct := range svc.createdTasks {
			tasks = append(tasks, &Task{ID: ct.ID, TenantID: ct.TenantID, Subject: ct.Subject,
				Status: TaskStatusCompleted, Result: &TaskResult{Output: "done"}})
		}
		return tasks, nil
	}
	return svc
}

// drainEvents reads every event the run emitted.
func drainEvents(run *JobRun) []JobEvent {
	var events []JobEvent
	for {
		select {
		case ev := <-run.Events():
			events = append(events, ev)
		default:
			return events
		}
	}
}

func stagesOf(events []JobEvent) []string {
	var stages []string
	for _, ev := range events {
		if ev.Type == JobEventTypeStage {
			stages = append(stages, ev.Metadata["stage"])
		}
	}
	return stages
}

func statesOf(events []JobEvent) []JobState {
	var states []JobState
	for _, ev := range events {
		if ev.Type == JobEventTypeStateChange {
			states = append(states, ev.State)
		}
	}
	return states
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// runLaunch drives a lifecycle launch through the production entry path,
// under the bound a hung moment must not outlive.
func runLaunch(t *testing.T, launch jobLaunch) (*JobRun, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return launch.run(ctx, t)
}

// TestLifecycle_ExactCallOrderOnACompletedJob: prepare before initializing,
// reviewable entering reviewing, complete entering completing, discard once
// the job is over with completed=true — nothing else, in that order — and
// the status reports the last stage.
func TestLifecycle_ExactCallOrderOnACompletedJob(t *testing.T) {
	t.Parallel()
	hooks := &recordingHooks{}
	run, err := runLaunch(t, jobLaunch{
		Agents: leadDrivenService(`{"approved": true, "feedback": "all good"}`),
		Hooks:  hooks,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := []string{"prepare", "reviewable", "complete", "discard(completed)"}
	if got := hooks.Calls(); !equalStrings(got, want) {
		t.Fatalf("hook calls = %v, want %v", got, want)
	}
	status := run.Status()
	if status.State != JobStateCompleted || status.Stage != string(JobStageDiscard) || status.FailureClass != "" {
		t.Fatalf("status = state:%s stage:%s class:%s", status.State, status.Stage, status.FailureClass)
	}
	events := drainEvents(run)
	if got := stagesOf(events); !equalStrings(got, []string{"prepare", "reviewable", "complete", "discard"}) {
		t.Fatalf("stage events = %v", got)
	}
	if hooks.discardCtxErr != nil || !hooks.discardDeadline {
		t.Fatalf("discard ctx: err=%v deadline=%v; want live and bounded", hooks.discardCtxErr, hooks.discardDeadline)
	}
}

// TestLifecycle_NoHooksLeavesTheLeadDrivenPathUntouched: a workflow that
// names no hooks runs no moments and reports no stage.
func TestLifecycle_NoHooksLeavesTheLeadDrivenPathUntouched(t *testing.T) {
	t.Parallel()
	run, err := runLaunch(t, jobLaunch{
		Agents: leadDrivenService(`{"approved": true, "feedback": "all good"}`),
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if status := run.Status(); status.State != JobStateCompleted || status.Stage != "" {
		t.Fatalf("status = %+v", status)
	}
	if stages := stagesOf(drainEvents(run)); len(stages) != 0 {
		t.Fatalf("no hooks means no stage events, got %v", stages)
	}
}

// TestLifecycle_HooksResolvedFromTheWorkflow: WorkflowSettings.Hooks names a
// registered set and the envelope resolves it where it launches the job, so
// the run carries the set by name and runs its moments.
func TestLifecycle_HooksResolvedFromTheWorkflow(t *testing.T) {
	t.Parallel()
	const name = "lifecycle-test-workflow-hooks"
	discarded := make(chan struct{})
	hooks := &recordingHooks{discardFn: func(context.Context, *JobContext, bool) error {
		close(discarded)
		return nil
	}}
	registerJobHooksForTest(t, name, hooks)

	loader := NewWorkflowLoader()
	if err := loader.Register("test-tenant", &WorkflowDefinition{
		Name: "hooked-workflow", Mode: WorkflowModeAutonomous,
		Agents:   []AgentRoleSpec{{Role: "worker", Backend: "ollama"}},
		Settings: WorkflowSettings{Hooks: name},
	}); err != nil {
		t.Fatalf("register workflow: %v", err)
	}
	svc := newTestService(t)
	svc.SetWorkflowLoader(loader)
	defer cancelAllJobs(svc)

	config := newTestConfig()
	config.WorkflowName = "hooked-workflow"
	jobID, err := svc.launchJob(context.Background(), config, "", nil)
	if err != nil {
		t.Fatalf("launchJob: %v", err)
	}

	// The record exists before the goroutine does, so the set the envelope
	// resolved is readable off the run the moment the launch returns.
	val, ok := svc.jobs.Load("test-tenant/" + jobID)
	if !ok {
		t.Fatalf("job %s is not registered", jobID)
	}
	run := val.(*runningJob).run
	if run.hooksName != name {
		t.Fatalf("the run must carry the workflow's set by name, got %q", run.hooksName)
	}

	// The job runs against a service with no backends, so it fails in
	// initializing; prepare has already run and discard runs on the way out.
	<-discarded
	if got := hooks.Calls(); len(got) != 2 || got[0] != "prepare" || got[1] != "discard(not completed)" {
		t.Fatalf("hook calls = %v, want the resolved set's moments", got)
	}
	if stages := stagesOf(drainEvents(run)); len(stages) == 0 || stages[0] != "prepare" {
		t.Fatalf("stage events must carry the resolved set's moments, got %v", stages)
	}
}

// TestLifecycle_UnknownHooksRefusedBeforeAnyGoroutine: a workflow naming a
// hook set nothing registered is refused where the envelope resolves it —
// before the job is registered and before any goroutine runs, so nothing
// can spawn.
func TestLifecycle_UnknownHooksRefusedBeforeAnyGoroutine(t *testing.T) {
	t.Parallel()
	// Registration validates the name, so the way a definition naming an
	// unregistered set reaches a launch is the way it reaches one in
	// production: it was persisted on a node that had the module, and
	// rehydrated at startup on a node that does not.
	persistor := newMemWorkflowPersistor()
	persistor.saved["unknown-hooks-workflow"] = &WorkflowDefinition{
		Name: "unknown-hooks-workflow", TenantID: "test-tenant", Mode: WorkflowModeAutonomous,
		Agents:   []AgentRoleSpec{{Role: "worker", Backend: "ollama"}},
		Settings: WorkflowSettings{Hooks: "nobody-registered-this-set"},
	}
	loader := NewWorkflowLoader()
	loader.SetPersistor(persistor)
	if _, err := loader.Rehydrate(context.Background(), "test-tenant"); err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	svc := newTestService(t)
	svc.SetWorkflowLoader(loader)

	config := newTestConfig()
	config.WorkflowName = "unknown-hooks-workflow"
	_, err := svc.launchJob(context.Background(), config, "", nil)
	var unknown *UnknownJobHooksError
	if !errors.As(err, &unknown) {
		t.Fatalf("got %v, want UnknownJobHooksError", err)
	}
	if unknown.Name != "nobody-registered-this-set" {
		t.Fatalf("the refusal must name the set, got %q", unknown.Name)
	}
	count := 0
	svc.jobs.Range(func(_, _ any) bool { count++; return true })
	if count != 0 {
		t.Fatalf("a refused job must not be registered, found %d", count)
	}
}

// TestLifecycle_PrepareWorkDirReachesEverySpawn: the directory Prepare
// returns is the WorkDir of the worker's spawn request, and the lead's, and
// a spawn into a prepared directory carries the engine's grant.
func TestLifecycle_PrepareWorkDirReachesEverySpawn(t *testing.T) {
	t.Parallel()
	base := &controlLoopMockService{backends: newTestBackendsWithPersistent()}
	base.decomposeFn = func() (*DecomposeTaskResponse, error) {
		return &DecomposeTaskResponse{WorkItems: []*WorkItem{{ID: "w1", Title: "Task 1", Description: "Do it"}}}, nil
	}
	base.promptFn = func(req *PromptSessionRequest) (*PromptSessionResponse, error) {
		if req.SystemPrompt != "" {
			return newApprovedReviewResponse(), nil
		}
		return &PromptSessionResponse{Content: []ContentBlock{{Type: "text", Text: "changed the files"}}}, nil
	}
	svc := credentialGuarded(base)
	hooks := &recordingHooks{workDir: "/prepared/dir"}
	config := newTestConfig()
	config.LeadBackend = "claude"
	run, err := runLaunch(t, jobLaunch{
		Config: config, Agents: svc, Hooks: hooks,
		// A REVIEWER IS NOT OPTIONAL ANY MORE. With none, handleReview falls
		// back to the lead's backend, and a claude lead reviewing is refused —
		// claude reviews nothing in this fleet. The reviewer is on codex, which
		// is also what makes this test cover all three spawn sites.
		Workflow: &WorkflowDefinition{
			Name: "w", Mode: WorkflowModeConstrained,
			Agents: []AgentRoleSpec{
				{Role: "lead", Backend: "claude", Capabilities: []string{"persistent_sessions"}},
				{Role: "reviewer", Backend: "codex", Capabilities: []string{"persistent_sessions"}},
				{Role: "worker", Backend: "claude", Capabilities: []string{"persistent_sessions"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	spawns := svc.Spawns()
	if len(spawns) != 3 {
		t.Fatalf("expected the lead, the reviewer and the worker to spawn, got %d", len(spawns))
	}
	for _, req := range spawns {
		if req.WorkDir != "/prepared/dir" {
			t.Fatalf("spawn WorkDir = %q, want the prepared directory", req.WorkDir)
		}
	}
	for _, req := range spawns {
		// The credential is required at every site: claude and codex are both
		// pooled backends.
		if !req.AccountAuto {
			t.Fatalf("every session the loop spawns carries the engine's credential: %+v", req)
		}
		// The permission-mode grant is Claude Code's flag, so it is required
		// only where Claude Code runs. The reviewer is codex — claude reviews
		// nothing in this fleet — and asserting claude's argv on it would pin a
		// topology the engine refuses.
		if !claudeCodeBackends[req.Backend] {
			continue
		}
		if len(req.ExtraArgs) != 2 || req.ExtraArgs[0] != "--permission-mode" {
			t.Fatalf("every Claude Code session the loop spawns carries the engine's grant: %+v", req)
		}
	}
	if hooks.prepareSaw == nil || hooks.prepareSaw.JobID != run.JobID || hooks.prepareSaw.TenantID != "test-tenant" {
		t.Fatalf("Prepare must see the job: %+v", hooks.prepareSaw)
	}
	if hooks.discardJobDir != "/prepared/dir" {
		t.Fatalf("Discard must see the prepared directory, saw %q", hooks.discardJobDir)
	}
}

// TestLifecycle_EverySpawnStartsWithItsCharter pins that the lead, the
// reviewer and the worker each start with their own charter rendered from the
// job's prompt set.
func TestLifecycle_EverySpawnStartsWithItsCharter(t *testing.T) {
	t.Parallel()
	base := &controlLoopMockService{backends: newTestBackendsWithPersistent()}
	base.decomposeFn = func() (*DecomposeTaskResponse, error) {
		return &DecomposeTaskResponse{WorkItems: []*WorkItem{{ID: "w1", Title: "Task 1", Description: "Do it"}}}, nil
	}
	base.promptFn = func(req *PromptSessionRequest) (*PromptSessionResponse, error) {
		if req.SystemPrompt != "" {
			return newApprovedReviewResponse(), nil
		}
		return &PromptSessionResponse{Content: []ContentBlock{{Type: "text", Text: "changed the files"}}}, nil
	}
	svc := credentialGuarded(base)
	config := newTestConfig()
	config.LeadBackend = "claude"
	_, err := runLaunch(t, jobLaunch{
		Config: config, Agents: svc, Hooks: &recordingHooks{workDir: "/prepared/dir"},
		Workflow: &WorkflowDefinition{
			Name: "w", Mode: WorkflowModeConstrained,
			Agents: []AgentRoleSpec{
				{Role: "lead", Backend: "claude", Capabilities: []string{"persistent_sessions"}},
				{Role: "reviewer", Backend: "codex", Capabilities: []string{"persistent_sessions"}},
				{Role: "worker", Backend: "claude", Capabilities: []string{"persistent_sessions"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	spawns := svc.Spawns()
	if len(spawns) != 3 {
		t.Fatalf("expected the lead, the reviewer and the worker to spawn, got %d", len(spawns))
	}
	got := map[string]int{}
	for _, req := range spawns {
		got[req.SystemPrompt]++
	}
	// The three sites are the only spawns, so each distinct charter starting
	// exactly one session pins every site to its own.
	if spawns[0].SystemPrompt != mustRenderCharter(t, charterLead) {
		t.Fatalf("lead SystemPrompt = %q, want charter_lead", spawns[0].SystemPrompt)
	}
	for _, charter := range []string{charterLead, charterReviewer, charterWorker} {
		if got[mustRenderCharter(t, charter)] != 1 {
			t.Fatalf("%s started %d sessions, want 1: %v", charter, got[mustRenderCharter(t, charter)], got)
		}
	}
}

// TestLifecycle_DiscardRunsDetachedAfterTheParentIsCancelled: the job's
// context is cancelled mid-execution; Discard still runs, on a live context
// with its own deadline, and completed=false.
func TestLifecycle_DiscardRunsDetachedAfterTheParentIsCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	svc := leadDrivenService(`{"approved": true}`)
	svc.decomposeFn = func() (*DecomposeTaskResponse, error) {
		cancel()
		return nil, ctx.Err()
	}
	hooks := &recordingHooks{}
	_, err := jobLaunch{Agents: svc, Hooks: hooks}.run(ctx, t)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run: %v", err)
	}
	if got := hooks.Calls(); !equalStrings(got, []string{"prepare", "discard(not completed)"}) {
		t.Fatalf("hook calls = %v", got)
	}
	if hooks.discardCtxErr != nil {
		t.Fatalf("Discard ran on a cancelled context: %v", hooks.discardCtxErr)
	}
	if !hooks.discardDeadline {
		t.Fatal("Discard must be bounded by its own deadline")
	}
}

// TestLifecycle_DiscardHonoursItsTimeout: a Discard that never returns is
// cut off at the configured discard bound; on an otherwise completed job
// that is a hook failure.
func TestLifecycle_DiscardHonoursItsTimeout(t *testing.T) {
	t.Parallel()
	hooks := &recordingHooks{discardFn: func(ctx context.Context, _ *JobContext, _ bool) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	run, err := runLaunch(t, jobLaunch{
		Agents: leadDrivenService(`{"approved": true}`), Hooks: hooks,
		Policy: &JobLifecyclePolicy{StageTimeouts: StageTimeouts{Discard: 20 * time.Millisecond}},
	})
	var hookErr *JobHookError
	if !errors.As(err, &hookErr) || hookErr.Stage != JobStageDiscard || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want JobHookError(discard) wrapping DeadlineExceeded", err)
	}
	status := run.Status()
	if status.State != JobStateFailed || status.FailureClass != FailureClassHook {
		t.Fatalf("status = state:%s class:%s", status.State, status.FailureClass)
	}
}

// TestLifecycle_HookErrorFailsTheJobWithFailureClass: a Prepare error is
// the job failing, typed, with FailureClassHook on the status, the stage
// recorded, sessions released, and Discard still run.
func TestLifecycle_HookErrorFailsTheJobWithFailureClass(t *testing.T) {
	t.Parallel()
	cause := errors.New("no directory today")
	hooks := &recordingHooks{prepareErr: cause}
	svc := &spawnRecordingService{controlLoopMockService: leadDrivenService(`{"approved": true}`)}
	run, err := runLaunch(t, jobLaunch{Agents: svc, Hooks: hooks})
	var hookErr *JobHookError
	if !errors.As(err, &hookErr) || hookErr.Stage != JobStagePrepare || !errors.Is(err, cause) {
		t.Fatalf("got %v, want JobHookError(prepare) wrapping the cause", err)
	}
	status := run.Status()
	if status.State != JobStateFailed || status.FailureClass != FailureClassHook || status.Stage != string(JobStageDiscard) {
		t.Fatalf("status = state:%s class:%s stage:%s", status.State, status.FailureClass, status.Stage)
	}
	if got := hooks.Calls(); !equalStrings(got, []string{"prepare", "discard(not completed)"}) {
		t.Fatalf("hook calls = %v", got)
	}
	if len(svc.Spawns()) != 0 {
		t.Fatal("a failed Prepare must spawn nothing")
	}

	completeHooks := &recordingHooks{completeEr: errors.New("could not finalize")}
	_, err = runLaunch(t, jobLaunch{
		Agents: leadDrivenService(`{"approved": true}`), Hooks: completeHooks,
	})
	if !errors.As(err, &hookErr) || hookErr.Stage != JobStageComplete {
		t.Fatalf("got %v, want JobHookError(complete)", err)
	}
	if got := completeHooks.Calls(); !equalStrings(got, []string{"prepare", "reviewable", "complete", "discard(not completed)"}) {
		t.Fatalf("hook calls = %v", got)
	}
}

// TestLifecycle_ReviewingCriteriaErrorFailsTheJob: a review that cannot be
// judged is not an approval.
func TestLifecycle_ReviewingCriteriaErrorFailsTheJob(t *testing.T) {
	t.Parallel()
	run, err := runLaunch(t, jobLaunch{
		Agents: leadDrivenService(`{"approved": true}`),
		Workflow: &WorkflowDefinition{
			Name: "w", Mode: WorkflowModeAutonomous,
			Agents: []AgentRoleSpec{{Role: "worker", Backend: "ollama"}},
			Settings: WorkflowSettings{ReviewCriteria: []ReviewCriterion{
				{Name: "cmd", Validator: ValidatorKindCommand, Config: "true", Required: true, Phase: CriterionPhaseReviewing},
			}},
		},
	})
	if !errors.Is(err, ErrCommandRefused) {
		t.Fatalf("the zero guard refuses the command and the job fails with it: %v", err)
	}
	if run.Status().State != JobStateFailed {
		t.Fatalf("status = %s", run.Status().State)
	}
}

// TestLifecycle_PolicyFromTheAgentService: a loop with no explicit policy
// takes it from an agent service that provides one, and a plain service
// yields the zero policy with default bounds.
func TestLifecycle_PolicyFromTheAgentService(t *testing.T) {
	t.Parallel()
	guard := guardAllowing(t, "make test")
	svc := &policyProvidingService{controlLoopMockService: &controlLoopMockService{},
		policy: JobLifecyclePolicy{Guard: guard, StageTimeouts: StageTimeouts{Prepare: time.Second}}}
	cl := NewControlLoop(svc, &controlLoopMockLogger{})
	policy := cl.lifecyclePolicy()
	if err := policy.Guard.CheckCommand("c", "make test"); err != nil {
		t.Fatalf("the service's guard must reach the run: %v", err)
	}
	if policy.StageTimeouts.Prepare != time.Second || policy.StageTimeouts.Discard != DefaultStageTimeouts.Discard {
		t.Fatalf("stage timeouts = %+v", policy.StageTimeouts)
	}
	plain := NewControlLoop(&controlLoopMockService{}, &controlLoopMockLogger{})
	if err := plain.lifecyclePolicy().Guard.CheckCommand("c", "make test"); !errors.Is(err, ErrCommandRefused) {
		t.Fatalf("a plain service yields the everything-refusing zero guard: %v", err)
	}
	if cl.jobContext().Guard.CheckCommand("c", "make test") != nil {
		t.Fatal("the job context carries the guard")
	}
}

// policyProvidingService is an agent service carrying a lifecycle policy.
type policyProvidingService struct {
	*controlLoopMockService
	policy JobLifecyclePolicy
}

func (s *policyProvidingService) JobLifecyclePolicy() JobLifecyclePolicy { return s.policy }

// TestControlLoop_HookSetFromTheEnvelopeAndTheOperatorPolicy: the loop runs
// the moments of the hook set the envelope installed on the run it adopted,
// under the operator's own stage bounds.
func TestControlLoop_HookSetFromTheEnvelopeAndTheOperatorPolicy(t *testing.T) {
	t.Parallel()
	hooks := &recordingHooks{}
	cl := NewControlLoop(&controlLoopMockService{}, &controlLoopMockLogger{})
	cl.adopt(jobLaunch{
		Hooks:  hooks,
		Policy: &JobLifecyclePolicy{StageTimeouts: StageTimeouts{Blocked: 3 * time.Second}},
	}.newRun(t))
	if cl.job.hooks != hooks {
		t.Fatal("the envelope's hook set must reach the loop")
	}
	if cl.job.hooksName != "explicit" {
		t.Fatalf("an unnamed set is named explicit, got %q", cl.job.hooksName)
	}
	if cl.lifecyclePolicy().StageTimeouts.Blocked != 3*time.Second {
		t.Fatal("the operator policy the envelope installed must reach the loop")
	}
	if err := cl.enterStage(context.Background(), JobStateDecomposing); err != nil {
		t.Fatalf("a state with no moment is a no-op: %v", err)
	}
	cl.job.hooks = nil
	if err := cl.enterStage(context.Background(), JobStateInitializing); err != nil {
		t.Fatalf("no hooks is a no-op: %v", err)
	}
	if err := cl.discard(context.Background(), true); err != nil {
		t.Fatalf("discard with no hooks is a no-op: %v", err)
	}
}

// pooledAndUnpooledBackends is a backend list with two persistent backends:
// one the server pools accounts for and one it does not.
//
// THE UNPOOLED ONE IS "ollama", NOT "codex". codex became a pooled backend on
// 2026-09-13: its credential is a ChatGPT subscription in a file, and an
// unresolved account meant the reviewer spawned with none and died before its
// first turn. ollama is local inference with no provider account to pool, so it
// is the honest example of a backend the engine leaves alone.
func pooledAndUnpooledBackends() []*AgentBackendInfo {
	return []*AgentBackendInfo{
		{Name: "claude", Type: "acp", Capabilities: BackendCapabilities{PersistentSessions: true, ToolUse: true}},
		// codex is registered because it is what REVIEWS: claude reviews
		// nothing in this fleet, so threeRoleWorkflow puts the reviewer here
		// whenever the lead is Claude Code. It is pooled, like claude.
		{Name: "codex", Type: "codex", Capabilities: BackendCapabilities{PersistentSessions: true, ToolUse: true}},
		{Name: "ollama", Type: "ollama", Capabilities: BackendCapabilities{PersistentSessions: true, ToolUse: true}},
	}
}

// threeRoleWorkflow spawns a lead, a reviewer and a worker on backend.
// threeRoleWorkflow builds a lead/reviewer/worker workflow on one backend, with
// the REVIEWER moved off any Claude Code backend.
//
// Claude reviews nothing in this fleet (owner ruling 2026-09-13), so handleReview
// refuses a review that would land on claude — and a fixture that put all three
// roles there was testing a topology the engine will not run. The lead and worker
// stay on the named backend, which is what these tests are about; only the
// reviewer moves, and it moves to what the fleet actually reviews with.
func threeRoleWorkflow(backend string) *WorkflowDefinition {
	return &WorkflowDefinition{
		Name: "three", Mode: WorkflowModeConstrained,
		Agents: []AgentRoleSpec{
			{Role: "lead", Backend: backend, Capabilities: []string{"persistent_sessions"}},
			{Role: "reviewer", Backend: reviewingBackendFor(backend), Capabilities: []string{"persistent_sessions"}},
			{Role: "worker", Backend: backend, Capabilities: []string{"persistent_sessions"}},
		},
	}
}

// reviewingBackendFor returns a backend that may review. A non-Claude backend
// reviews as itself, so a test that chose ollama deliberately keeps it.
func reviewingBackendFor(backend string) string {
	if claudeCodeBackends[backend] {
		return "codex"
	}
	return backend
}

// threeRoleService answers the worker prompt with output and every review
// prompt with approval.
func threeRoleService() *controlLoopMockService {
	svc := &controlLoopMockService{backends: pooledAndUnpooledBackends()}
	svc.decomposeFn = func() (*DecomposeTaskResponse, error) {
		return &DecomposeTaskResponse{WorkItems: []*WorkItem{{ID: "w1", Title: "Task 1", Description: "Do it"}}}, nil
	}
	svc.promptFn = func(req *PromptSessionRequest) (*PromptSessionResponse, error) {
		if req.SystemPrompt != "" {
			return newApprovedReviewResponse(), nil
		}
		return &PromptSessionResponse{Content: []ContentBlock{{Type: "text", Text: "done"}}}, nil
	}
	return svc
}

// TestSpawn_EverySiteBindsAPooledAccount (#577): the lead, the reviewer
// and the worker spawn on a pooled-account backend all name their
// credential — through a service that refuses a spawn naming none — and
// all carry the engine's grant and configuration directory.
func TestSpawn_EverySiteBindsAPooledAccount(t *testing.T) {
	t.Parallel()
	svc := credentialGuarded(threeRoleService())
	config := newTestConfig()
	config.LeadBackend = "claude"
	run, err := runLaunch(t, jobLaunch{
		Config: config, Agents: svc, Workflow: threeRoleWorkflow("claude"),
		Policy: &JobLifecyclePolicy{AgentConfigDir: "/var/lib/quicbaas/agents/claude"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	spawns := svc.Spawns()
	if len(spawns) != 3 {
		t.Fatalf("expected lead, reviewer and worker to spawn, got %d", len(spawns))
	}
	for i, site := range []string{"lead", "reviewer", "worker"} {
		req := spawns[i]
		// THE SUBJECT OF THIS TEST: every site names a pooled account. It holds
		// for the codex reviewer too, because codex is pooled — its credential
		// is a subscription file (go-quicbaas#945's sibling fix).
		if !req.AccountAuto {
			t.Errorf("%s spawn names no account: %+v", site, req)
		}
		// The configuration directory and the permission grant are Claude
		// Code's, so they are asserted only where Claude Code runs. The
		// reviewer is on codex — claude reviews nothing in this fleet — and
		// asserting claude's environment on it would pin a topology the engine
		// refuses.
		if !claudeCodeBackends[req.Backend] {
			continue
		}
		if req.Env["CLAUDE_CONFIG_DIR"] != "/var/lib/quicbaas/agents/claude" {
			t.Errorf("%s spawn does not carry the engine's config dir: %v", site, req.Env)
		}
		if len(req.ExtraArgs) != 2 || req.ExtraArgs[1] != WorkerPermissionMode {
			t.Errorf("%s spawn does not carry the engine's grant: %v", site, req.ExtraArgs)
		}
	}
	if run.Status().State != JobStateCompleted {
		t.Fatalf("status = %s", run.Status().State)
	}
}

// TestSpawn_UnpooledBackendSpawnsAsItWas (#577): a backend the server does
// not pool is spawned exactly as before at all three sites — no account
// request, no grant, no configuration directory.
func TestSpawn_UnpooledBackendSpawnsAsItWas(t *testing.T) {
	t.Parallel()
	svc := credentialGuarded(threeRoleService())
	config := newTestConfig()
	config.LeadBackend = "ollama"
	if _, err := runLaunch(t, jobLaunch{
		Config: config, Agents: svc, Workflow: threeRoleWorkflow("ollama"),
		Policy: &JobLifecyclePolicy{AgentConfigDir: "/var/lib/quicbaas/agents/claude"},
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	spawns := svc.Spawns()
	if len(spawns) != 3 {
		t.Fatalf("expected three spawns, got %d", len(spawns))
	}
	for i, site := range []string{"lead", "reviewer", "worker"} {
		req := spawns[i]
		if req.AccountAuto || req.AccountID != "" || req.ExtraArgs != nil || req.Env != nil {
			t.Errorf("%s spawn on an unpooled backend must be untouched: %+v", site, req)
		}
	}
}

// TestSpawn_RefusingServiceRefusesAnUnboundAccount pins the fake itself:
// a pooled-backend spawn naming no credential is refused typed, so the
// tests above cannot pass on a loop that forgets to bind one.
func TestSpawn_RefusingServiceRefusesAnUnboundAccount(t *testing.T) {
	t.Parallel()
	svc := credentialGuarded(&controlLoopMockService{})
	_, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{Backend: "claude"})
	var unbound *AccountResolverNotConfiguredError
	if !errors.As(err, &unbound) || unbound.Backend != "claude" {
		t.Fatalf("got %v, want AccountResolverNotConfiguredError", err)
	}
	if _, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{Backend: "claude", AccountAuto: true}); err != nil {
		t.Fatalf("a bound spawn must pass: %v", err)
	}
	// ollama, not codex: codex is pooled now (its credential is a subscription
	// file), so it is no longer an example of a backend that needs no account.
	if _, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{Backend: "ollama"}); err != nil {
		t.Fatalf("an unpooled backend needs no account: %v", err)
	}
}
