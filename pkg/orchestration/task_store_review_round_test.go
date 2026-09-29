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

// TestBumpReviewRoundBudget_IncrementsTheBlockedTaskS bonus proves the happy
// path: a blocked task's ReviewRoundBonus rises by exactly one per call, and
// the returned value matches what is persisted.
func TestBumpReviewRoundBudget_IncrementsTheBlockedTasksBonus(t *testing.T) {
	t.Parallel()
	store := NewInMemoryTaskStore()
	ctx := context.Background()
	seedTaskInStatus(t, store, "tenant-a", "task-1", TaskStatusBlocked)

	bonus, err := store.BumpReviewRoundBudget(ctx, "tenant-a", "task-1")
	if err != nil {
		t.Fatalf("BumpReviewRoundBudget: %v", err)
	}
	if bonus != 1 {
		t.Fatalf("returned bonus = %d, want 1", bonus)
	}
	got, err := store.GetTask(ctx, "tenant-a", "task-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ReviewRoundBonus != 1 {
		t.Errorf("persisted ReviewRoundBonus = %d, want 1", got.ReviewRoundBonus)
	}

	bonus, err = store.BumpReviewRoundBudget(ctx, "tenant-a", "task-1")
	if err != nil {
		t.Fatalf("second BumpReviewRoundBudget: %v", err)
	}
	if bonus != 2 {
		t.Fatalf("returned bonus after a second grant = %d, want 2", bonus)
	}
}

// TestBumpReviewRoundBudget_RefusesPastTheMaximum proves the bound is
// enforced rather than turning into an unbounded allowance.
func TestBumpReviewRoundBudget_RefusesPastTheMaximum(t *testing.T) {
	t.Parallel()
	store := NewInMemoryTaskStore()
	ctx := context.Background()
	seedTaskInStatus(t, store, "tenant-a", "task-1", TaskStatusBlocked)

	for i := 0; i < MaxReviewRoundBonus; i++ {
		if _, err := store.BumpReviewRoundBudget(ctx, "tenant-a", "task-1"); err != nil {
			t.Fatalf("grant %d: %v", i+1, err)
		}
	}

	_, err := store.BumpReviewRoundBudget(ctx, "tenant-a", "task-1")
	var exhausted *ReviewRoundBudgetExhaustedError
	if !errors.As(err, &exhausted) {
		t.Fatalf("grant past the maximum: got %v, want a ReviewRoundBudgetExhaustedError", err)
	}
}

// TestBumpReviewRoundBudget_RefusesATaskThatIsNotBlocked proves the verb
// only ever applies to a parked task — raising a pending or completed
// task's bonus would grant a round nothing is waiting to spend.
func TestBumpReviewRoundBudget_RefusesATaskThatIsNotBlocked(t *testing.T) {
	t.Parallel()
	store := NewInMemoryTaskStore()
	ctx := context.Background()
	seedTaskInStatus(t, store, "tenant-a", "task-1", TaskStatusPending)

	_, err := store.BumpReviewRoundBudget(ctx, "tenant-a", "task-1")
	var invalidState *TaskInvalidStateError
	if !errors.As(err, &invalidState) {
		t.Fatalf("bump on a pending task: got %v, want a TaskInvalidStateError", err)
	}
}

// TestBumpReviewRoundBudget_UnknownTaskFails proves a missing task is
// refused rather than silently creating accounting for nothing.
func TestBumpReviewRoundBudget_UnknownTaskFails(t *testing.T) {
	t.Parallel()
	store := NewInMemoryTaskStore()
	if _, err := store.BumpReviewRoundBudget(context.Background(), "tenant-a", "does-not-exist"); err == nil {
		t.Fatal("bump on an unknown task: want an error, got none")
	}
}
