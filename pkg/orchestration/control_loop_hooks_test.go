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

func TestHookCheckBudget_OK(t *testing.T) {
	tracker := &hookMockCostTracker{status: BudgetOK}
	logger := &hookTestLogger{}
	settings := CostControlSettings{BudgetAction: BudgetActionWarn}

	result, err := HookCheckBudget(context.Background(), tracker, settings, "tenant-1", "job-1", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Status != BudgetOK {
		t.Errorf("expected status %q, got %q", BudgetOK, result.Status)
	}
	if result.Action != "" {
		t.Errorf("expected empty action for ok status, got %q", result.Action)
	}
}

func TestHookCheckBudget_Warning(t *testing.T) {
	tracker := &hookMockCostTracker{status: BudgetWarning}
	logger := &hookTestLogger{}
	settings := CostControlSettings{BudgetAction: BudgetActionWarn}

	result, err := HookCheckBudget(context.Background(), tracker, settings, "tenant-1", "job-1", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Status != BudgetWarning {
		t.Errorf("expected status %q, got %q", BudgetWarning, result.Status)
	}

	// Should have logged a warning.
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if len(logger.warnings) == 0 {
		t.Error("expected at least one warning log for budget warning")
	}
}

func TestHookCheckBudget_Exceeded_Shutdown(t *testing.T) {
	tracker := &hookMockCostTracker{status: BudgetExceeded}
	logger := &hookTestLogger{}
	settings := CostControlSettings{BudgetAction: BudgetActionShutdown}

	result, err := HookCheckBudget(context.Background(), tracker, settings, "tenant-1", "job-1", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Status != BudgetExceeded {
		t.Errorf("expected status %q, got %q", BudgetExceeded, result.Status)
	}
	if result.Action != BudgetActionShutdown {
		t.Errorf("expected action %q, got %q", BudgetActionShutdown, result.Action)
	}
}

func TestHookCheckBudget_Exceeded_Pause(t *testing.T) {
	tracker := &hookMockCostTracker{status: BudgetExceeded}
	logger := &hookTestLogger{}
	settings := CostControlSettings{BudgetAction: BudgetActionPause}

	result, err := HookCheckBudget(context.Background(), tracker, settings, "tenant-1", "job-1", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Status != BudgetExceeded {
		t.Errorf("expected status %q, got %q", BudgetExceeded, result.Status)
	}
	if result.Action != BudgetActionPause {
		t.Errorf("expected action %q, got %q", BudgetActionPause, result.Action)
	}
}

func TestHookCheckBudget_Exceeded_Warn(t *testing.T) {
	tracker := &hookMockCostTracker{status: BudgetExceeded}
	logger := &hookTestLogger{}
	settings := CostControlSettings{BudgetAction: BudgetActionWarn}

	result, err := HookCheckBudget(context.Background(), tracker, settings, "tenant-1", "job-1", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Status != BudgetExceeded {
		t.Errorf("expected status %q, got %q", BudgetExceeded, result.Status)
	}
	if result.Action != BudgetActionWarn {
		t.Errorf("expected action %q, got %q", BudgetActionWarn, result.Action)
	}
}

func TestHookCheckBudget_Exceeded_DefaultsToWarn(t *testing.T) {
	tracker := &hookMockCostTracker{status: BudgetExceeded}
	logger := &hookTestLogger{}
	// Empty BudgetAction should default to "warn".
	settings := CostControlSettings{}

	result, err := HookCheckBudget(context.Background(), tracker, settings, "tenant-1", "job-1", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != BudgetActionWarn {
		t.Errorf("expected default action %q, got %q", BudgetActionWarn, result.Action)
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

func TestHookRecoveryBootstrap_EmptySteps(t *testing.T) {
	err := HookRecoveryBootstrap(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error for empty steps: %v", err)
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

func TestHookTriggerReindex_IterationComplete(t *testing.T) {
	var called bool
	reindexFn := func(_ context.Context) error {
		called = true
		return nil
	}

	triggers := []string{"manual", "iteration_complete"}

	err := HookTriggerReindex(context.Background(), reindexFn, triggers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected reindexFn to be called")
	}
}

func TestHookTriggerReindex_NoTrigger(t *testing.T) {
	var called bool
	reindexFn := func(_ context.Context) error {
		called = true
		return nil
	}

	err := HookTriggerReindex(context.Background(), reindexFn, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("expected reindexFn NOT to be called with nil triggers")
	}
}

func TestHookTriggerReindex_ManualOnly(t *testing.T) {
	var called bool
	reindexFn := func(_ context.Context) error {
		called = true
		return nil
	}

	triggers := []string{"manual", "on_demand"}

	err := HookTriggerReindex(context.Background(), reindexFn, triggers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("expected reindexFn NOT to be called when iteration_complete is absent")
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

func TestHookTriggerReindex_EmptyTriggers(t *testing.T) {
	var called bool
	reindexFn := func(_ context.Context) error {
		called = true
		return nil
	}

	err := HookTriggerReindex(context.Background(), reindexFn, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("expected reindexFn NOT to be called with empty triggers")
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
// ControlLoop Setter Tests
// =============================================================================

func TestControlLoop_SetPeerMessaging(t *testing.T) {
	cl := NewControlLoop(nil, &hookTestLogger{})
	if cl.peerMessaging != nil {
		t.Error("expected nil peerMessaging initially")
	}

	mockSvc := &hookMockPeerMessaging{}
	cl.SetPeerMessaging(mockSvc)
	if cl.peerMessaging == nil {
		t.Error("expected non-nil peerMessaging after set")
	}
}

func TestControlLoop_SetCostTracker(t *testing.T) {
	cl := NewControlLoop(nil, &hookTestLogger{})
	if cl.costTracker != nil {
		t.Error("expected nil costTracker initially")
	}

	tracker := &hookMockCostTracker{status: BudgetOK}
	cl.SetCostTracker(tracker)
	if cl.costTracker == nil {
		t.Error("expected non-nil costTracker after set")
	}
}

func TestControlLoop_SetFileLocking(t *testing.T) {
	cl := NewControlLoop(nil, &hookTestLogger{})
	if cl.fileLocking != nil {
		t.Error("expected nil fileLocking initially")
	}

	// FileLockService is an interface; just verify the setter works.
	cl.SetFileLocking(nil)
	if cl.fileLocking != nil {
		t.Error("expected nil fileLocking after setting nil")
	}
}

func TestControlLoop_SetReindexFn(t *testing.T) {
	cl := NewControlLoop(nil, &hookTestLogger{})
	if cl.reindexFn != nil {
		t.Error("expected nil reindexFn initially")
	}

	cl.SetReindexFn(func(_ context.Context) error { return nil })
	if cl.reindexFn == nil {
		t.Error("expected non-nil reindexFn after set")
	}
}

// =============================================================================
// Error Type Tests
// =============================================================================

func TestHookBudgetCheckError_Unwrap(t *testing.T) {
	cause := errors.New("timeout")
	err := &HookBudgetCheckError{TenantID: "t1", JobID: "j1", Cause: cause}

	if !errors.Is(err, ErrHookBudgetCheck) {
		t.Error("expected error to unwrap to ErrHookBudgetCheck")
	}

	msg := err.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
}

func TestHookBudgetCheckError_NilCause(t *testing.T) {
	err := &HookBudgetCheckError{TenantID: "t1", JobID: "j1"}
	msg := err.Error()
	if msg == "" {
		t.Error("expected non-empty error message with nil cause")
	}
}

func TestHookRecoveryBootstrapError_Unwrap(t *testing.T) {
	cause := errors.New("step failed")
	err := &HookRecoveryBootstrapError{Step: "scan_fleet", Cause: cause}

	if !errors.Is(err, ErrHookRecoveryBootstrap) {
		t.Error("expected error to unwrap to ErrHookRecoveryBootstrap")
	}
}

func TestHookRecoveryBootstrapError_NilCause(t *testing.T) {
	err := &HookRecoveryBootstrapError{Step: "scan_fleet"}
	msg := err.Error()
	if msg == "" {
		t.Error("expected non-empty error message with nil cause")
	}
}

func TestHookPeerDeliveryError_Unwrap(t *testing.T) {
	cause := errors.New("subscribe failed")
	err := &HookPeerDeliveryError{AgentID: "agent-1", Cause: cause}

	if !errors.Is(err, ErrHookPeerDelivery) {
		t.Error("expected error to unwrap to ErrHookPeerDelivery")
	}
}

func TestHookPeerDeliveryError_NilCause(t *testing.T) {
	err := &HookPeerDeliveryError{AgentID: "agent-1"}
	msg := err.Error()
	if msg == "" {
		t.Error("expected non-empty error message with nil cause")
	}
}

func TestHookReindexError_Unwrap(t *testing.T) {
	cause := errors.New("indexing failed")
	err := &HookReindexError{Cause: cause}

	if !errors.Is(err, ErrHookReindex) {
		t.Error("expected error to unwrap to ErrHookReindex")
	}
}

func TestHookReindexError_NilCause(t *testing.T) {
	err := &HookReindexError{}
	msg := err.Error()
	if msg == "" {
		t.Error("expected non-empty error message with nil cause")
	}
}
