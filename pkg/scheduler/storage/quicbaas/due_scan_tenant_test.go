package quicbaas

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// countingGetKV counts point reads, so a test can pin that the due scan
// reads tenant jobs by prefix scan and not one Get per indexed job.
type countingGetKV struct {
	*mockKVStore
	gets atomic.Int64
}

func (c *countingGetKV) Get(ctx context.Context, key string) ([]byte, error) {
	c.gets.Add(1)
	return c.mockKVStore.Get(ctx, key)
}

// A malformed tenant job is skipped: it cannot stop the sweep, so other
// tenants' jobs and the system jobs already collected still run.
func TestGetJobsDueForExecution_MalformedTenantJobDoesNotBlockHealthyJobs(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)
	if err := storage.InitializeJobIDCounter(ctx); err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now()

	bad := createTestJob("bad", scheduler.OwnerTypeTenant, "acme")
	bad.NextRun = now.Add(-time.Minute)
	if err := storage.CreateJob(ctx, bad); err != nil {
		t.Fatalf("CreateJob bad: %v", err)
	}
	if err := kv.Put(ctx, keyForJobByID(bad.ID, "acme"), []byte("not valid json")); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	healthy := map[string]*scheduler.Job{
		"acme-ok":   createTestJob("acme-ok", scheduler.OwnerTypeTenant, "acme"),
		"zeta-ok":   createTestJob("zeta-ok", scheduler.OwnerTypeTenant, "zeta"),
		"system-ok": createTestJob("system-ok", scheduler.OwnerTypeSystem, ""),
	}
	for name, job := range healthy {
		job.NextRun = now.Add(-time.Minute)
		if err := storage.CreateJob(ctx, job); err != nil {
			t.Fatalf("CreateJob %s: %v", name, err)
		}
	}

	due, err := storage.GetJobsDueForExecution(ctx, now, 100)
	if err != nil {
		t.Fatalf("due scan with a malformed tenant job: %v", err)
	}
	got := map[string]bool{}
	for _, j := range due {
		got[j.Name] = true
	}
	if len(due) != len(healthy) || !got["acme-ok"] || !got["zeta-ok"] || !got["system-ok"] {
		t.Fatalf("due = %v, want acme-ok, zeta-ok and system-ok", jobNames(due))
	}
}

// Retained terminal tenant jobs are read in each tenant's prefix scan, not
// by a point read each, so a sweep's cost does not grow with one read per
// historical job.
func TestGetJobsDueForExecution_TenantJobsReadByScanNotPerJobGet(t *testing.T) {
	ctx := context.Background()
	kv := &countingGetKV{mockKVStore: newMockKVStore()}
	storage := NewStorage(kv)
	if err := storage.InitializeJobIDCounter(ctx); err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now()

	for i := 0; i < 20; i++ {
		job := createTestJob("done", scheduler.OwnerTypeTenant, "acme")
		job.Status = scheduler.JobStatusCompleted
		job.NextRun = now.Add(-time.Minute)
		if err := storage.CreateJob(ctx, job); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}
	}
	pending := createTestJob("pending", scheduler.OwnerTypeTenant, "acme")
	pending.NextRun = now.Add(-time.Minute)
	if err := storage.CreateJob(ctx, pending); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	kv.gets.Store(0)
	due, err := storage.GetJobsDueForExecution(ctx, now, 100)
	if err != nil {
		t.Fatalf("due scan: %v", err)
	}
	if len(due) != 1 || due[0].Name != "pending" {
		t.Fatalf("due = %v, want pending only", jobNames(due))
	}
	if n := kv.gets.Load(); n != 0 {
		t.Fatalf("due scan made %d point reads, want 0", n)
	}
}
