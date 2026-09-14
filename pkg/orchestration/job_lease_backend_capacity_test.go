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
	"strconv"
	"testing"
	"time"
)

// mustProcessLeaseStore is the in-process lease store these tests admit into.
func mustProcessLeaseStore(t *testing.T) JobLeaseStore {
	t.Helper()
	store, err := NewProcessJobLeaseStore(time.Minute)
	if err != nil {
		t.Fatalf("NewProcessJobLeaseStore: %v", err)
	}
	return store
}

// backendCappedStore builds a lease store capping the named backends.
func backendCappedStore(t *testing.T, limits map[string]int) JobLeaseStore {
	t.Helper()
	store, err := NewBackendCapacityJobLeaseStore(mustProcessLeaseStore(t), limits, time.Minute)
	if err != nil {
		t.Fatalf("NewBackendCapacityJobLeaseStore: %v", err)
	}
	return store
}

// backendLease builds a lease for one job on a backend. Every lease is
// OffBudget, because that is what a review is — and the bug this file exists
// to fix was that OffBudget put reviews beyond every quota there was.
func backendLease(project, job, backend string) *JobLease {
	return &JobLease{
		TenantID: "acme", ProjectID: project, Workflow: "review", Ref: "pr-" + job,
		JobID: job, Generation: 1, Node: "node-1", Backend: backend, OffBudget: true,
	}
}

// TestBackendCapacity_RefusesBeyondTheLimit is the property the whole file
// exists for: a shared model server admits its limit and refuses the next.
func TestBackendCapacity_RefusesBeyondTheLimit(t *testing.T) {
	ctx := context.Background()
	store := backendCappedStore(t, map[string]int{"qwen": 2})

	for i := 1; i <= 2; i++ {
		if err := store.Acquire(ctx, backendLease("p1", "job-"+strconv.Itoa(i), "qwen")); err != nil {
			t.Fatalf("acquire %d within the limit: %v", i, err)
		}
	}

	err := store.Acquire(ctx, backendLease("p1", "job-3", "qwen"))
	if err == nil {
		t.Fatal("the third lease on a backend limited to two must be refused")
	}
	if !errors.Is(err, ErrBackendCapacityExceeded) {
		t.Fatalf("want ErrBackendCapacityExceeded, got %T: %v", err, err)
	}
	var capped *BackendCapacityExceededError
	if !errors.As(err, &capped) {
		t.Fatalf("want a typed BackendCapacityExceededError, got %T", err)
	}
	if capped.Live != 2 || capped.Limit != 2 || capped.Backend != "qwen" {
		t.Fatalf("refusal must name what it counted: %+v", capped)
	}
}

// TestBackendCapacity_CountsAcrossProjects is why the count is not scoped to
// the contender's project.
//
// THE MODEL SERVER IS SHARED. Counting per project would let N projects each
// run the limit against one GPU, which is the same unbounded concurrency the
// cap exists to stop — just spelled differently.
func TestBackendCapacity_CountsAcrossProjects(t *testing.T) {
	ctx := context.Background()
	store := backendCappedStore(t, map[string]int{"qwen": 2})

	if err := store.Acquire(ctx, backendLease("p1", "job-1", "qwen")); err != nil {
		t.Fatalf("acquire on p1: %v", err)
	}
	if err := store.Acquire(ctx, backendLease("p2", "job-2", "qwen")); err != nil {
		t.Fatalf("acquire on p2: %v", err)
	}
	if err := store.Acquire(ctx, backendLease("p3", "job-3", "qwen")); !errors.Is(err, ErrBackendCapacityExceeded) {
		t.Fatalf("a third project must still be refused, got %v", err)
	}
}

// TestBackendCapacity_CountsOffBudgetLeases pins the distinction that makes
// this cap work at all.
//
// OffBudget means "does not spend a PROJECT's worker slot", so that a review
// never queues behind the work it unblocks. It does NOT mean free: a review
// occupies the model exactly as a worker does. Reading OffBudget here would
// reproduce the original bug, in which five reviews were beyond every quota.
func TestBackendCapacity_CountsOffBudgetLeases(t *testing.T) {
	ctx := context.Background()
	store := backendCappedStore(t, map[string]int{"qwen": 1})

	offBudget := backendLease("p1", "job-1", "qwen")
	if !offBudget.OffBudget {
		t.Fatal("the fixture must be off-budget, as every review is")
	}
	if err := store.Acquire(ctx, offBudget); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := store.Acquire(ctx, backendLease("p1", "job-2", "qwen")); !errors.Is(err, ErrBackendCapacityExceeded) {
		t.Fatalf("an off-budget lease must still occupy the backend, got %v", err)
	}
}

// TestBackendCapacity_UncappedAndUndeclaredAreUnlimited keeps the wrap from
// changing a deployment that configures nothing.
func TestBackendCapacity_UncappedAndUndeclaredAreUnlimited(t *testing.T) {
	ctx := context.Background()
	store := backendCappedStore(t, map[string]int{"qwen": 1})

	// A backend nobody capped.
	for i := 1; i <= 5; i++ {
		if err := store.Acquire(ctx, backendLease("p1", "codex-"+strconv.Itoa(i), "codex")); err != nil {
			t.Fatalf("an uncapped backend must be unlimited, refused at %d: %v", i, err)
		}
	}
	// A lease that declares no backend at all — every lease written before
	// this field existed.
	for i := 1; i <= 5; i++ {
		if err := store.Acquire(ctx, backendLease("p1", "legacy-"+strconv.Itoa(i), "")); err != nil {
			t.Fatalf("a lease with no backend must be unlimited, refused at %d: %v", i, err)
		}
	}
}

// TestBackendCapacity_ReleasedLeaseFreesTheSlot proves the cap is about LIVE
// work rather than history: finishing a review admits the next one.
func TestBackendCapacity_ReleasedLeaseFreesTheSlot(t *testing.T) {
	ctx := context.Background()
	store := backendCappedStore(t, map[string]int{"qwen": 1})

	held := backendLease("p1", "job-1", "qwen")
	if err := store.Acquire(ctx, held); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := store.Acquire(ctx, backendLease("p1", "job-2", "qwen")); !errors.Is(err, ErrBackendCapacityExceeded) {
		t.Fatalf("the slot must be occupied while the first runs, got %v", err)
	}
	if err := store.Release(ctx, held); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := store.Acquire(ctx, backendLease("p1", "job-2", "qwen")); err != nil {
		t.Fatalf("releasing the holder must free the slot: %v", err)
	}
}

// TestBackendCapacity_NormalisesNamesAndRefusesNothing covers the constructor's
// input handling: case and spacing in a config key must not silently disable a
// cap the operator believes they set.
func TestBackendCapacity_NormalisesNamesAndRefusesNothing(t *testing.T) {
	ctx := context.Background()
	store := backendCappedStore(t, map[string]int{"  QWEN  ": 1, "opencode": 0, "": 5})

	if err := store.Acquire(ctx, backendLease("p1", "job-1", "qwen")); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := store.Acquire(ctx, backendLease("p1", "job-2", "QwEn")); !errors.Is(err, ErrBackendCapacityExceeded) {
		t.Fatalf("a config key with padding and case must still cap, got %v", err)
	}
	// A zero limit is "unlimited", matching a project quota of zero.
	for i := 1; i <= 3; i++ {
		if err := store.Acquire(ctx, backendLease("p1", "oc-"+strconv.Itoa(i), "opencode")); err != nil {
			t.Fatalf("a zero limit must mean unlimited, refused at %d: %v", i, err)
		}
	}

	// A nil map is a legitimate configuration, not an error.
	if _, err := NewBackendCapacityJobLeaseStore(mustProcessLeaseStore(t), nil, time.Minute); err != nil {
		t.Fatalf("a nil limit map must be accepted as a pass-through: %v", err)
	}
	// But the store and TTL are load-bearing.
	if _, err := NewBackendCapacityJobLeaseStore(nil, map[string]int{"qwen": 1}, time.Minute); err == nil {
		t.Fatal("a capacity check with no store beneath it must be refused")
	}
	if _, err := NewBackendCapacityJobLeaseStore(mustProcessLeaseStore(t), map[string]int{"qwen": 1}, 0); err == nil {
		t.Fatal("a capacity check with no TTL cannot decide which leases are live")
	}
}
