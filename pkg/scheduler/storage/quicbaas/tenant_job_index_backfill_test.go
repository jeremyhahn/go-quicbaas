package quicbaas

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// putUnindexedTenantJob writes a tenant job the way a store written before
// the tenant-job index existed holds it: the job record under the tenant's
// namespace and no index entry.
func putUnindexedTenantJob(t *testing.T, kv *mockKVStore, job *scheduler.Job) {
	t.Helper()
	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal %s: %v", job.Name, err)
	}
	if err := kv.Put(context.Background(), keyForJob(job), data); err != nil {
		t.Fatalf("put %s: %v", job.Name, err)
	}
}

func dueJobNames(t *testing.T, storage *Storage, before time.Time) map[string]bool {
	t.Helper()
	due, err := storage.GetJobsDueForExecution(context.Background(), before, 100, nil)
	if err != nil {
		t.Fatalf("GetJobsDueForExecution: %v", err)
	}
	names := make(map[string]bool, len(due))
	for _, job := range due {
		names[job.Name] = true
	}
	return names
}

// TestTenantJobIndexBackfill_EveryOpenIndexesUnindexedJobs is the upgrade
// fixture: a delayed and a cron tenant job stored before the index existed
// are due once the storage opens, and a job an old writer stores without an
// index entry after that first open is due once the next open runs.
func TestTenantJobIndexBackfill_EveryOpenIndexesUnindexedJobs(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	now := time.Now()

	delayed := createTestJob("legacy-delayed", scheduler.OwnerTypeTenant, "acme")
	delayed.ID = 41
	delayed.Type = scheduler.JobTypeDelayed
	delayed.Delay = time.Minute
	delayed.NextRun = now.Add(-time.Minute)
	putUnindexedTenantJob(t, kv, delayed)

	cron := createTestJob("legacy-cron", scheduler.OwnerTypeTenant, "globex")
	cron.ID = 42
	cron.Type = scheduler.JobTypeRecurring
	cron.Schedule = "*/5 * * * *"
	cron.NextRun = now.Add(-time.Minute)
	putUnindexedTenantJob(t, kv, cron)

	storage := NewStorage(kv)
	if names := dueJobNames(t, storage, now); len(names) != 0 {
		t.Fatalf("before open, due jobs = %v, want none (no fallback scan)", names)
	}

	if err := storage.InitializeJobIDCounter(ctx); err != nil {
		t.Fatalf("open: %v", err)
	}
	names := dueJobNames(t, storage, now)
	if len(names) != 2 || !names["legacy-delayed"] || !names["legacy-cron"] {
		t.Fatalf("after open, due jobs = %v, want legacy-delayed and legacy-cron", names)
	}
	// A job an old writer stores after the first open is indexed by the next
	// open: the reconcile runs at every open.
	late := createTestJob("written-after-backfill", scheduler.OwnerTypeTenant, "acme")
	late.ID = 43
	late.NextRun = now.Add(-time.Minute)
	putUnindexedTenantJob(t, kv, late)
	if err := NewStorage(kv).InitializeJobIDCounter(ctx); err != nil {
		t.Fatalf("second open: %v", err)
	}
	names = dueJobNames(t, storage, now)
	if len(names) != 3 || !names["legacy-delayed"] || !names["legacy-cron"] || !names["written-after-backfill"] {
		t.Fatalf("after second open, due jobs = %v, want legacy-delayed, legacy-cron and written-after-backfill", names)
	}
}

// TestTenantJobIndexBackfill_IsIdempotent proves the reconcile can run at
// every open: across repeated runs, indexed and unindexed jobs
// each end with exactly one entry.
func TestTenantJobIndexBackfill_IsIdempotent(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)

	indexed := createTestJob("indexed", scheduler.OwnerTypeTenant, "acme")
	if err := storage.CreateJob(ctx, indexed); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	legacy := createTestJob("legacy", scheduler.OwnerTypeTenant, "acme")
	legacy.ID = indexed.ID + 1
	putUnindexedTenantJob(t, kv, legacy)

	for i := 0; i < 2; i++ {
		if err := storage.backfillTenantJobIndex(ctx); err != nil {
			t.Fatalf("backfill %d: %v", i, err)
		}
	}
	entries, err := kv.List(ctx, TenantJobIndexPrefix)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("index entries = %v, want one per tenant job", entries)
	}
}

// TestUpdateJob_KeepsTenantJobIndex proves an update writes the tenant job's
// index entry, so a job whose entry is missing is due again after it.
func TestUpdateJob_KeepsTenantJobIndex(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)
	now := time.Now()

	job := createTestJob("tenant-job", scheduler.OwnerTypeTenant, "acme")
	job.ID = 7
	job.NextRun = now.Add(-time.Minute)
	putUnindexedTenantJob(t, kv, job)

	if err := storage.UpdateJob(ctx, job); err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	if ok, err := kv.Exists(ctx, keyForTenantJobIndex(job.ID, "acme")); err != nil || !ok {
		t.Fatalf("index entry after UpdateJob = %v (%v), want present", ok, err)
	}
	if names := dueJobNames(t, storage, now); !names["tenant-job"] {
		t.Fatalf("due jobs = %v, want tenant-job", names)
	}
}
