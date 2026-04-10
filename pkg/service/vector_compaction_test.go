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
	"errors"
	"sync"
	"testing"
)

func TestCompactionTask_NewTask(t *testing.T) {
	task := NewCompactionTask("task-1", "ns-vectors")
	if task.State() != CompactionPlanning {
		t.Errorf("expected initial state Planning, got %s", task.State())
	}
	if task.ID() != "task-1" {
		t.Errorf("expected id task-1, got %s", task.ID())
	}
	if task.Namespace() != "ns-vectors" {
		t.Errorf("expected namespace ns-vectors, got %s", task.Namespace())
	}
	if task.StartedAt().IsZero() {
		t.Error("expected non-zero started time")
	}
	if task.CompletedAt() != nil {
		t.Error("expected nil completed time for new task")
	}
	if task.IsCanceled() {
		t.Error("expected new task to not be canceled")
	}
	if task.GetError() != nil {
		t.Error("expected nil error for new task")
	}
}

func TestCompactionTask_NewTaskEmptyArgs(t *testing.T) {
	task := NewCompactionTask("", "")
	if task.State() != CompactionPlanning {
		t.Errorf("expected Planning state even with empty args, got %s", task.State())
	}
	if task.ID() != "" {
		t.Errorf("expected empty id, got %s", task.ID())
	}
}

func TestCompactionTask_ValidTransitions(t *testing.T) {
	task := NewCompactionTask("task-2", "ns-test")

	// Planning -> Executing.
	if err := task.Transition(CompactionExecuting); err != nil {
		t.Errorf("unexpected error transitioning to Executing: %v", err)
	}
	if task.State() != CompactionExecuting {
		t.Errorf("expected Executing, got %s", task.State())
	}

	// Executing -> Completed.
	if err := task.Transition(CompactionCompleted); err != nil {
		t.Errorf("unexpected error transitioning to Completed: %v", err)
	}
	if task.State() != CompactionCompleted {
		t.Errorf("expected Completed, got %s", task.State())
	}
	if task.CompletedAt() == nil {
		t.Error("expected completed time to be set")
	}
}

func TestCompactionTask_ValidTransitionsError(t *testing.T) {
	task := NewCompactionTask("task-err", "ns-test")

	// Planning -> Completed is not valid.
	err := task.Transition(CompactionCompleted)
	if err == nil {
		t.Error("expected error for invalid transition Planning -> Completed")
	}
	var transErr *InvalidTransitionError
	if !errors.As(err, &transErr) {
		t.Errorf("expected InvalidTransitionError, got %T", err)
	}
	if transErr.From != CompactionPlanning || transErr.To != CompactionCompleted {
		t.Errorf("expected from=Planning to=Completed, got from=%s to=%s", transErr.From, transErr.To)
	}
}

func TestCompactionTask_InvalidTransition(t *testing.T) {
	task := NewCompactionTask("task-3", "ns-test")

	// Move to terminal state.
	if err := task.Transition(CompactionExecuting); err != nil {
		t.Fatal(err)
	}
	if err := task.Transition(CompactionCompleted); err != nil {
		t.Fatal(err)
	}

	// Completed -> Executing should fail.
	err := task.Transition(CompactionExecuting)
	if err == nil {
		t.Error("expected error for transition from terminal state")
	}
	var transErr *InvalidTransitionError
	if !errors.As(err, &transErr) {
		t.Errorf("expected InvalidTransitionError, got %T", err)
	}
}

func TestCompactionTask_InvalidTransitionFromFailed(t *testing.T) {
	task := NewCompactionTask("task-fail", "ns-test")
	if err := task.Transition(CompactionFailed); err != nil {
		t.Fatal(err)
	}

	err := task.Transition(CompactionPlanning)
	if err == nil {
		t.Error("expected error for transition from Failed")
	}
}

func TestCompactionTask_Cancel(t *testing.T) {
	task := NewCompactionTask("task-4", "ns-cancel")
	if err := task.Transition(CompactionExecuting); err != nil {
		t.Fatal(err)
	}

	if task.IsCanceled() {
		t.Error("expected not canceled before Cancel()")
	}

	task.Cancel()

	if !task.IsCanceled() {
		t.Error("expected canceled after Cancel()")
	}

	// Verify CancelCh is closed.
	select {
	case <-task.CancelCh():
		// Expected.
	default:
		t.Error("expected CancelCh to be closed after Cancel()")
	}

	// Double cancel should not panic.
	task.Cancel()
}

func TestCompactionTask_CancelNotCanceled(t *testing.T) {
	task := NewCompactionTask("task-nocancel", "ns-test")
	select {
	case <-task.CancelCh():
		t.Error("expected CancelCh to be open for non-canceled task")
	default:
		// Expected.
	}
}

func TestCompactionTask_Timeout(t *testing.T) {
	task := NewCompactionTask("task-5", "ns-timeout")
	if err := task.Transition(CompactionExecuting); err != nil {
		t.Fatal(err)
	}
	if err := task.Transition(CompactionTimeout); err != nil {
		t.Errorf("unexpected error transitioning to Timeout: %v", err)
	}
	if task.State() != CompactionTimeout {
		t.Errorf("expected Timeout, got %s", task.State())
	}
	if task.CompletedAt() == nil {
		t.Error("expected completed time to be set for Timeout")
	}
}

func TestCompactionTask_TimeoutFromPlanning(t *testing.T) {
	task := NewCompactionTask("task-timeout-plan", "ns-test")
	err := task.Transition(CompactionTimeout)
	if err == nil {
		t.Error("expected error for Timeout from Planning")
	}
}

func TestCompactionTask_UpdateProgress(t *testing.T) {
	task := NewCompactionTask("task-6", "ns-progress")
	task.UpdateProgress(500, 1000)

	progress := task.GetProgress()
	if progress.BytesCompacted != 500 {
		t.Errorf("expected 500 bytes compacted, got %d", progress.BytesCompacted)
	}
	if progress.TotalBytes != 1000 {
		t.Errorf("expected 1000 total bytes, got %d", progress.TotalBytes)
	}
}

func TestCompactionTask_UpdateProgressZero(t *testing.T) {
	task := NewCompactionTask("task-prog-zero", "ns-test")
	progress := task.GetProgress()
	if progress.BytesCompacted != 0 || progress.TotalBytes != 0 {
		t.Error("expected zero progress for new task")
	}
}

func TestCompactionTask_GetProgress(t *testing.T) {
	task := NewCompactionTask("task-7", "ns-snap")
	task.UpdateProgress(250, 500)
	task.UpdateSegmentProgress(3, 1)

	progress := task.GetProgress()
	if progress.BytesCompacted != 250 {
		t.Errorf("expected 250 bytes, got %d", progress.BytesCompacted)
	}
	if progress.TotalBytes != 500 {
		t.Errorf("expected 500 total, got %d", progress.TotalBytes)
	}
	if progress.SegmentsRead != 3 {
		t.Errorf("expected 3 segments read, got %d", progress.SegmentsRead)
	}
	if progress.SegmentsWritten != 1 {
		t.Errorf("expected 1 segment written, got %d", progress.SegmentsWritten)
	}
}

func TestCompactionTask_GetProgressEmpty(t *testing.T) {
	task := NewCompactionTask("task-empty", "ns-test")
	progress := task.GetProgress()
	if progress.SegmentsRead != 0 || progress.SegmentsWritten != 0 {
		t.Error("expected zero segment progress for new task")
	}
}

func TestCompactionTask_SetAndGetError(t *testing.T) {
	task := NewCompactionTask("task-err", "ns-test")
	testErr := &InvalidTransitionError{From: CompactionPlanning, To: CompactionCompleted}
	task.SetError(testErr)

	gotErr := task.GetError()
	if gotErr == nil {
		t.Fatal("expected non-nil error")
	}
	var transErr *InvalidTransitionError
	if !errors.As(gotErr, &transErr) {
		t.Errorf("expected InvalidTransitionError, got %T", gotErr)
	}
}

func TestCompactionTask_SetAndGetErrorNil(t *testing.T) {
	task := NewCompactionTask("task-nil-err", "ns-test")
	if task.GetError() != nil {
		t.Error("expected nil error for new task")
	}
}

func TestCompactionTask_Plan(t *testing.T) {
	task := NewCompactionTask("task-plan", "ns-test")
	plan := &CompactionPlan{
		Namespace:         "ns-test",
		EstimatedBytes:    1024 * 1024,
		SegmentsToCompact: 5,
		TargetSegments:    1,
	}
	task.SetPlan(plan)
	got := task.Plan()
	if got.Namespace != "ns-test" {
		t.Errorf("expected ns-test, got %s", got.Namespace)
	}
	if got.SegmentsToCompact != 5 {
		t.Errorf("expected 5 segments to compact, got %d", got.SegmentsToCompact)
	}
}

func TestCompactionTask_PlanNil(t *testing.T) {
	task := NewCompactionTask("task-noplan", "ns-test")
	if task.Plan() != nil {
		t.Error("expected nil plan for new task")
	}
}

func TestCompactionTask_ConcurrentAccess(t *testing.T) {
	task := NewCompactionTask("task-conc", "ns-concurrent")
	if err := task.Transition(CompactionExecuting); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	const goroutines = 100

	// Concurrent progress updates.
	wg.Add(goroutines)
	for i := range goroutines {
		go func(n int) {
			defer wg.Done()
			task.UpdateProgress(int64(n)*10, 1000)
			_ = task.GetProgress()
			_ = task.State()
			_ = task.IsCanceled()
		}(i)
	}
	wg.Wait()

	// Verify task is still in a valid state.
	state := task.State()
	if state != CompactionExecuting {
		t.Errorf("expected Executing after concurrent access, got %s", state)
	}
}

func TestCompactionTask_ConcurrentTransitions(t *testing.T) {
	task := NewCompactionTask("task-conc-trans", "ns-test")
	if err := task.Transition(CompactionExecuting); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	const goroutines = 50
	successCount := int32(0)
	var successMu sync.Mutex

	// Race multiple goroutines trying to complete the task.
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			if err := task.Transition(CompactionCompleted); err == nil {
				successMu.Lock()
				successCount++
				successMu.Unlock()
			}
		}()
	}
	wg.Wait()

	// Exactly one should succeed.
	if successCount != 1 {
		t.Errorf("expected exactly 1 successful transition, got %d", successCount)
	}
	if task.State() != CompactionCompleted {
		t.Errorf("expected Completed, got %s", task.State())
	}
}

func TestCompactionState_String(t *testing.T) {
	tests := []struct {
		state    CompactionState
		expected string
	}{
		{CompactionPlanning, "planning"},
		{CompactionExecuting, "executing"},
		{CompactionCompleted, "completed"},
		{CompactionFailed, "failed"},
		{CompactionTimeout, "timeout"},
		{CompactionCanceled, "canceled"},
		{CompactionState(99), "unknown(99)"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			if tc.state.String() != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, tc.state.String())
			}
		})
	}
}

func TestCompactionState_StringUnknown(t *testing.T) {
	unknown := CompactionState(-1)
	result := unknown.String()
	if result == "" {
		t.Error("expected non-empty string for unknown state")
	}
}

func TestInvalidTransitionError_Error(t *testing.T) {
	err := &InvalidTransitionError{From: CompactionPlanning, To: CompactionCompleted}
	expected := "invalid compaction state transition from planning to completed"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestInvalidTransitionError_DifferentStates(t *testing.T) {
	err := &InvalidTransitionError{From: CompactionFailed, To: CompactionExecuting}
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

func TestCompactionTask_PlanningToCanceled(t *testing.T) {
	task := NewCompactionTask("task-cancel-plan", "ns-test")
	if err := task.Transition(CompactionCanceled); err != nil {
		t.Errorf("unexpected error transitioning Planning -> Canceled: %v", err)
	}
	if task.State() != CompactionCanceled {
		t.Errorf("expected Canceled, got %s", task.State())
	}
}

func TestCompactionTask_PlanningToFailed(t *testing.T) {
	task := NewCompactionTask("task-fail-plan", "ns-test")
	if err := task.Transition(CompactionFailed); err != nil {
		t.Errorf("unexpected error transitioning Planning -> Failed: %v", err)
	}
	if task.State() != CompactionFailed {
		t.Errorf("expected Failed, got %s", task.State())
	}
}

func TestCompactionTask_ExecutingToCanceled(t *testing.T) {
	task := NewCompactionTask("task-exec-cancel", "ns-test")
	if err := task.Transition(CompactionExecuting); err != nil {
		t.Fatal(err)
	}
	if err := task.Transition(CompactionCanceled); err != nil {
		t.Errorf("unexpected error transitioning Executing -> Canceled: %v", err)
	}
	if task.CompletedAt() == nil {
		t.Error("expected completed time for canceled task")
	}
}
