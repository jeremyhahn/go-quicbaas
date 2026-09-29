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

package orchestration

import (
	"context"
	"errors"
	"testing"
)

// TestCloseBlockedTask_CompletesAParkedTaskWithoutReadmitting proves the
// operator's close-as-done ruling lands on the real store: a blocked task
// goes straight to completed, carries the ruling as its result, and no
// longer reads as parked.
func TestCloseBlockedTask_CompletesAParkedTaskWithoutReadmitting(t *testing.T) {
	t.Parallel()
	store := NewInMemoryTaskStore()
	ctx := context.Background()
	seedTaskInStatus(t, store, "tenant-a", "task-1", TaskStatusBlocked)

	if err := store.CloseBlockedTask(ctx, "tenant-a", "task-1", &TaskResult{Output: "no further work"}); err != nil {
		t.Fatalf("CloseBlockedTask: %v", err)
	}

	got, err := store.GetTask(ctx, "tenant-a", "task-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != TaskStatusCompleted {
		t.Fatalf("status = %s, want completed", got.Status)
	}
	if got.Result == nil || got.Result.Output != "no further work" {
		t.Errorf("result = %+v, want the operator's closing note", got.Result)
	}
	if got.CompletedAt == 0 {
		t.Error("CompletedAt was not stamped")
	}
	if got.BlockKind != "" || got.BlockedReason != "" {
		t.Errorf("block not cleared: kind=%q reason=%q", got.BlockKind, got.BlockedReason)
	}
	if got.ConclusionReason != TaskConcludedCompleted {
		t.Errorf("conclusion = %q, want %q", got.ConclusionReason, TaskConcludedCompleted)
	}
}

// TestCloseBlockedTask_RefusesATaskThatIsNotBlocked proves the verb is the
// blocked -> completed edge and nothing wider: it must not become a way to
// complete queued or running work without the attempt that owns it.
func TestCloseBlockedTask_RefusesATaskThatIsNotBlocked(t *testing.T) {
	t.Parallel()
	for _, status := range []TaskStatus{TaskStatusPending, TaskStatusClaimed, TaskStatusInProgress, TaskStatusFailed} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()
			store := NewInMemoryTaskStore()
			seedTaskInStatus(t, store, "tenant-a", "task-1", status)

			err := store.CloseBlockedTask(context.Background(), "tenant-a", "task-1", &TaskResult{Output: "x"})
			if !errors.Is(err, ErrTaskInvalidState) {
				t.Fatalf("got %v, want ErrTaskInvalidState", err)
			}
		})
	}
}

// TestCompleteTask_StillRefusesABlockedTask proves opening the blocked ->
// completed edge did not open it to CompleteTask: a worker reporting
// completion for a task that has since been parked is a stale report.
func TestCompleteTask_StillRefusesABlockedTask(t *testing.T) {
	t.Parallel()
	store := NewInMemoryTaskStore()
	seedTaskInStatus(t, store, "tenant-a", "task-1", TaskStatusBlocked)

	err := store.CompleteTask(context.Background(), "tenant-a", "task-1", &TaskResult{Output: "late"})
	if !errors.Is(err, ErrTaskInvalidState) {
		t.Fatalf("got %v, want ErrTaskInvalidState", err)
	}
}
