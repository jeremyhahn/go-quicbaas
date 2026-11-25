package quicbaas

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// TestTenantJobIndexBackfill_UnindexedJobsRunAfterOpen is the upgrade
// fixture: a delayed and a cron tenant job stored before the index existed
// are due once the storage opens, the backfill is recorded so it runs once,
// and a second open changes nothing.
func TestTenantJobIndexBackfill_UnindexedJobsRunAfterOpen(t *testing.T) {
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
	if done, err := kv.Exists(ctx, TenantJobIndexBackfillKey); err != nil || !done {
		t.Fatalf("backfill marker present = %v (%v), want true", done, err)
	}

	// The marker no longer ends reconciliation: a later open indexes a job
	// an older writer stored without an entry.
	late := createTestJob("written-after-backfill", scheduler.OwnerTypeTenant, "acme")
	late.ID = 43
	late.NextRun = now.Add(-time.Minute)
	putUnindexedTenantJob(t, kv, late)
	if err := NewStorage(kv).InitializeJobIDCounter(ctx); err != nil {
		t.Fatalf("second open: %v", err)
	}
	names = dueJobNames(t, storage, now)
	if len(names) != 3 || !names["written-after-backfill"] {
		t.Fatalf("after second open, due jobs = %v, want all three", names)
	}
}

// TestTenantJobIndexBackfill_IsIdempotent proves a backfill interrupted
// before its marker is written can run again: indexed and unindexed jobs
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
		if err := storage.reconcileTenantJobIndex(ctx); err != nil {
			t.Fatalf("backfill %d: %v", i, err)
		}
		if err := kv.Delete(ctx, TenantJobIndexBackfillKey); err != nil {
			t.Fatalf("drop marker: %v", err)
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

// indexPutCountingKV is a mockKVStore that counts the Puts under
// TenantJobIndexPrefix.
type indexPutCountingKV struct {
	*mockKVStore
	indexPuts int
}

func (c *indexPutCountingKV) Put(ctx context.Context, key string, value []byte) error {
	if strings.HasPrefix(key, TenantJobIndexPrefix) {
		c.indexPuts++
	}
	return c.mockKVStore.Put(ctx, key, value)
}

// TestTenantJobIndexBackfill_SteadyOpenWritesNothing proves the backfill
// writes only missing entries: once an open has indexed every tenant job, a
// later open writes no index entry.
func TestTenantJobIndexBackfill_SteadyOpenWritesNothing(t *testing.T) {
	ctx := context.Background()
	kv := &indexPutCountingKV{mockKVStore: newMockKVStore()}
	storage := NewStorage(kv)

	indexed := createTestJob("indexed", scheduler.OwnerTypeTenant, "acme")
	if err := storage.CreateJob(ctx, indexed); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	legacy := createTestJob("legacy", scheduler.OwnerTypeTenant, "globex")
	legacy.ID = indexed.ID + 1
	putUnindexedTenantJob(t, kv.mockKVStore, legacy)

	kv.indexPuts = 0
	if err := storage.reconcileTenantJobIndex(ctx); err != nil {
		t.Fatalf("first backfill: %v", err)
	}
	if kv.indexPuts != 1 {
		t.Fatalf("first backfill index writes = %d, want 1 (the unindexed job only)", kv.indexPuts)
	}

	kv.indexPuts = 0
	if err := storage.reconcileTenantJobIndex(ctx); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if kv.indexPuts != 0 {
		t.Fatalf("steady-state backfill index writes = %d, want 0", kv.indexPuts)
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

// TestReconcileTenantJobIndex_OpenIndexesOlderWriterJobs checks that the
// backfill marker no longer ends reconciliation: a job an older node writes
// without an index entry after the marker exists is indexed at open.
func TestReconcileTenantJobIndex_OpenIndexesOlderWriterJobs(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	if err := kv.Put(ctx, TenantJobIndexBackfillKey, []byte{1}); err != nil {
		t.Fatalf("put marker: %v", err)
	}
	job := createTestJob("older-writer", scheduler.OwnerTypeTenant, "acme")
	job.ID = 7
	putUnindexedTenantJob(t, kv, job)

	if err := NewStorage(kv).InitializeJobIDCounter(ctx); err != nil {
		t.Fatalf("open: %v", err)
	}
	if ok, err := kv.Exists(ctx, keyForTenantJobIndex(job.ID, "acme")); err != nil || !ok {
		t.Fatalf("index entry present = %v (%v), want true", ok, err)
	}
}

// TestGetJobsDueForExecution_ReconcilesPeriodically checks that the due scan
// reconciles the index once reconcileInterval has elapsed, and not before.
func TestGetJobsDueForExecution_ReconcilesPeriodically(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)
	if err := storage.InitializeJobIDCounter(ctx); err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now()
	job := createTestJob("older-writer", scheduler.OwnerTypeTenant, "acme")
	job.ID = 9
	job.NextRun = now.Add(-time.Minute)
	putUnindexedTenantJob(t, kv, job)

	if names := dueJobNames(t, storage, now); names["older-writer"] {
		t.Fatalf("before the interval, due jobs = %v, want no older-writer", names)
	}

	storage.reconcileMu.Lock()
	storage.lastReconcile = time.Now().Add(-storage.reconcileInterval)
	storage.reconcileMu.Unlock()

	if names := dueJobNames(t, storage, now); !names["older-writer"] {
		t.Fatalf("after the interval, due jobs = %v, want older-writer", names)
	}
}

// TestGetJobsDueForExecution_ReconcileExceedingSweepBudget checks that a
// reconciliation too slow for one sweep's budget neither stops dispatch nor
// starts over: every sweep returns the due system job, each sweep indexes
// more of the retained tenant jobs, and the unindexed due tenant job runs
// once reconciliation reaches it.
func TestGetJobsDueForExecution_ReconcileExceedingSweepBudget(t *testing.T) {
	ctx := context.Background()
	slow := &slowPutKVStore{mockKVStore: newMockKVStore()}
	storage := NewStorage(slow)
	if err := storage.InitializeJobIDCounter(ctx); err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now()

	system := createTestJob("system-maintenance", scheduler.OwnerTypeSystem, "")
	system.NextRun = now.Add(-time.Minute)
	if err := storage.CreateJob(ctx, system); err != nil {
		t.Fatalf("create system job: %v", err)
	}

	// Retained history: completed tenant jobs an older writer left unindexed.
	const retained = 40
	for i := 0; i < retained; i++ {
		job := createTestJob(fmt.Sprintf("history-%d", i), scheduler.OwnerTypeTenant, "acme")
		job.ID = uint64(1000 + i)
		job.Status = scheduler.JobStatusCompleted
		putUnindexedTenantJob(t, slow.mockKVStore, job)
	}
	due := createTestJob("older-writer-due", scheduler.OwnerTypeTenant, "acme")
	due.ID = 2000
	due.NextRun = now.Add(-time.Minute)
	putUnindexedTenantJob(t, slow.mockKVStore, due)

	// Each index Put costs 20ms: a full pass (41 Puts, ~820ms) is well over
	// one 200ms sweep budget.
	slow.putDelay = 20 * time.Millisecond
	storage.reconcileMu.Lock()
	storage.lastReconcile = time.Time{}
	storage.reconcileMu.Unlock()

	indexedCount := func() int {
		n := 0
		_ = slow.mockKVStore.Scan(ctx, TenantJobIndexPrefix, func(string, []byte) error {
			n++
			return nil
		})
		return n
	}

	prev := indexedCount()
	sawTenantJob := false
	for sweep := 0; sweep < 30 && (!sawTenantJob || prev < retained+1); sweep++ {
		sweepCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		jobs, err := storage.GetJobsDueForExecution(sweepCtx, now, 100, nil)
		cancel()
		if err != nil {
			t.Fatalf("sweep %d: %v", sweep, err)
		}
		names := make(map[string]bool, len(jobs))
		for _, job := range jobs {
			names[job.Name] = true
		}
		if !names["system-maintenance"] {
			t.Fatalf("sweep %d: due jobs = %v, want the system job dispatched while reconciliation runs", sweep, names)
		}
		sawTenantJob = sawTenantJob || names["older-writer-due"]
		got := indexedCount()
		if prev < retained+1 && got <= prev {
			t.Fatalf("sweep %d: index entries %d, was %d: reconciliation made no progress", sweep, got, prev)
		}
		prev = got
	}
	if !sawTenantJob {
		t.Fatalf("the unindexed due tenant job never ran")
	}
	if got := indexedCount(); got != retained+1 {
		t.Fatalf("index entries = %d, want %d", got, retained+1)
	}
	if storage.reconcileDue() {
		t.Fatalf("a finished reconciliation must wait for the interval")
	}
}
