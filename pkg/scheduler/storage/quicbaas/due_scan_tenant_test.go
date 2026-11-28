package quicbaas

import (
	"context"
	"fmt"
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

	due, err := storage.GetJobsDueForExecution(ctx, now, 100, nil)
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

// A tenant with more always-due jobs than the limit cannot starve the
// tenants after it: the scan's start rotates, so within one sweep per
// tenant every tenant's due job is returned.
func TestGetJobsDueForExecution_BusyTenantDoesNotStarveOthers(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)
	if err := storage.InitializeJobIDCounter(ctx); err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now()
	const limit = 3

	for i := 0; i < limit+2; i++ {
		job := createTestJob(fmt.Sprintf("acme-%d", i), scheduler.OwnerTypeTenant, "acme")
		job.NextRun = now.Add(-time.Minute)
		if err := storage.CreateJob(ctx, job); err != nil {
			t.Fatalf("CreateJob acme-%d: %v", i, err)
		}
	}
	zeta := createTestJob("zeta-due", scheduler.OwnerTypeTenant, "zeta")
	zeta.NextRun = now.Add(-time.Minute)
	if err := storage.CreateJob(ctx, zeta); err != nil {
		t.Fatalf("CreateJob zeta: %v", err)
	}

	const tenants = 2
	for sweep := 0; sweep < tenants; sweep++ {
		// acme's jobs stay due: nothing dispatches them between sweeps.
		due, err := storage.GetJobsDueForExecution(ctx, now, limit, nil)
		if err != nil {
			t.Fatalf("sweep %d: %v", sweep, err)
		}
		if len(due) > limit {
			t.Fatalf("sweep %d returned %d jobs, limit %d", sweep, len(due), limit)
		}
		for _, j := range due {
			if j.Name == "zeta-due" {
				return
			}
		}
	}
	t.Fatalf("zeta's due job was not returned within %d sweeps", tenants)
}

// TestGetJobsDueForExecution_SystemAndTenantJobsShareTheRotation checks that
// the system namespace takes its turn in the due-scan rotation: a full page of
// system jobs cannot hide every tenant job, and a busy tenant cannot hide the
// system jobs.
func TestGetJobsDueForExecution_SystemAndTenantJobsShareTheRotation(t *testing.T) {
	type seed struct {
		name      string
		ownerType scheduler.OwnerType
		ownerID   string
	}
	busyTenant := func(n int) []seed {
		seeds := make([]seed, 0, n)
		for i := 0; i < n; i++ {
			seeds = append(seeds, seed{fmt.Sprintf("acme-%d", i), scheduler.OwnerTypeTenant, "acme"})
		}
		return seeds
	}
	cases := []struct {
		name   string
		limit  int
		sweeps int
		jobs   []seed
		want   string
	}{
		{
			name:   "full system page does not starve a tenant job",
			limit:  1,
			sweeps: 2,
			jobs: []seed{
				{"system-stuck", scheduler.OwnerTypeSystem, ""},
				{"acme-due", scheduler.OwnerTypeTenant, "acme"},
			},
			want: "acme-due",
		},
		{
			name:   "busy tenant does not starve a system job",
			limit:  3,
			sweeps: 2,
			jobs: append(busyTenant(3+2),
				seed{"system-due", scheduler.OwnerTypeSystem, ""}),
			want: "system-due",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			kv := &sortedScanKV{mockKVStore: newMockKVStore()}
			storage := NewStorage(kv)
			if err := storage.InitializeJobIDCounter(ctx); err != nil {
				t.Fatalf("open: %v", err)
			}
			now := time.Now()
			for _, s := range tc.jobs {
				job := createTestJob(s.name, s.ownerType, s.ownerID)
				job.NextRun = now.Add(-time.Minute)
				if err := storage.CreateJob(ctx, job); err != nil {
					t.Fatalf("CreateJob %s: %v", s.name, err)
				}
			}
			// Every job stays pending: nothing dispatches between sweeps.
			for sweep := 0; sweep < tc.sweeps; sweep++ {
				due, err := storage.GetJobsDueForExecution(ctx, now, tc.limit, nil)
				if err != nil {
					t.Fatalf("sweep %d: %v", sweep, err)
				}
				if len(due) > tc.limit {
					t.Fatalf("sweep %d returned %d jobs, limit %d", sweep, len(due), tc.limit)
				}
				for _, j := range due {
					if j.Name == tc.want {
						return
					}
				}
			}
			t.Fatalf("%s not returned within %d sweeps", tc.want, tc.sweeps)
		})
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
	due, err := storage.GetJobsDueForExecution(ctx, now, 100, nil)
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
