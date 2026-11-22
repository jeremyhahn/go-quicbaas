package quicbaas

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// slowPutKVStore delays every Put, modelling a replicated store whose writes
// are slow, and honours the caller's context while it waits.
type slowPutKVStore struct {
	*mockKVStore
	putDelay time.Duration
}

func (s *slowPutKVStore) Put(ctx context.Context, key string, value []byte) error {
	select {
	case <-time.After(s.putDelay):
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.mockKVStore.Put(ctx, key, value)
}

func countIndexEntries(t *testing.T, kv *mockKVStore) int {
	t.Helper()
	n := 0
	if err := kv.Scan(context.Background(), TenantJobIndexPrefix, func(string, []byte) error {
		n++
		return nil
	}); err != nil {
		t.Fatalf("index scan: %v", err)
	}
	return n
}

// TestReconcile_ExceedingSweepBudgetStillDispatchesAndProgresses: when the
// tenant job index reconciliation needs more than one sweep's budget, opening
// still succeeds, every sweep still returns the due system job, and each
// sweep's partial reconciliation is kept, so the index completes over later
// sweeps.
func TestReconcile_ExceedingSweepBudgetStillDispatchesAndProgresses(t *testing.T) {
	const (
		legacyJobs = 20
		putDelay   = 20 * time.Millisecond
		sweep      = 200 * time.Millisecond // reconcile gets half: ~5 Puts
	)
	base := newMockKVStore()
	kv := &slowPutKVStore{mockKVStore: base, putDelay: putDelay}
	now := time.Now()

	sys := createTestJob("system-tick", scheduler.OwnerTypeSystem, "")
	sys.ID = scheduler.SystemJobIDFloor + 1
	sys.NextRun = now.Add(-time.Minute)
	putUnindexedTenantJob(t, base, sys)
	for i := 0; i < legacyJobs; i++ {
		job := createTestJob(fmt.Sprintf("legacy-%d", i), scheduler.OwnerTypeTenant, "acme")
		job.ID = uint64(100 + i)
		job.NextRun = now.Add(-time.Minute)
		putUnindexedTenantJob(t, base, job)
	}

	storage := NewStorage(kv)
	openCtx, cancel := context.WithTimeout(context.Background(), sweep)
	if err := storage.InitializeJobIDCounter(openCtx); err != nil {
		cancel()
		t.Fatalf("open must not fail when reconciliation exceeds its budget: %v", err)
	}
	cancel()
	if n := countIndexEntries(t, base); n >= legacyJobs {
		t.Fatalf("index entries after open = %d; the test needs reconciliation to exceed one budget", n)
	}

	prev := countIndexEntries(t, base)
	for i := 0; ; i++ {
		if i >= 4*legacyJobs {
			t.Fatalf("index never completed: %d of %d entries", prev, legacyJobs)
		}
		ctx, cancel := context.WithTimeout(context.Background(), sweep)
		due, err := storage.GetJobsDueForExecution(ctx, now, 100)
		cancel()
		if err != nil {
			t.Fatalf("sweep %d: %v", i, err)
		}
		names := make(map[string]bool, len(due))
		for _, j := range due {
			names[j.Name] = true
		}
		if !names["system-tick"] {
			t.Fatalf("sweep %d: due = %v, want system-tick while reconciliation is incomplete", i, names)
		}
		n := countIndexEntries(t, base)
		if n < prev {
			t.Fatalf("sweep %d: index entries fell from %d to %d", i, prev, n)
		}
		prev = n
		if n == legacyJobs && len(due) == legacyJobs+1 {
			return
		}
	}
}
