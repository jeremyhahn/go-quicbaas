package quicbaas

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// TestGetJobsDueForExecution_IncludesTenantJobs checks that due-job
// discovery returns a tenant's due jobs as well as the system's, and skips
// tenant keys that are not job records.
func TestGetJobsDueForExecution_IncludesTenantJobs(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewStorage(kv)
	now := time.Now()

	sysJob := createTestJob("sys-due", scheduler.OwnerTypeSystem, "")
	sysJob.NextRun = now.Add(-time.Hour)
	if err := storage.CreateJob(ctx, sysJob); err != nil {
		t.Fatalf("CreateJob(system): %v", err)
	}

	tenantJob := createTestJob("acme-due", scheduler.OwnerTypeTenant, "acme")
	tenantJob.NextRun = now.Add(-time.Hour)
	if err := storage.CreateJob(ctx, tenantJob); err != nil {
		t.Fatalf("CreateJob(tenant due): %v", err)
	}

	futureJob := createTestJob("acme-future", scheduler.OwnerTypeTenant, "acme")
	futureJob.NextRun = now.Add(time.Hour)
	if err := storage.CreateJob(ctx, futureJob); err != nil {
		t.Fatalf("CreateJob(tenant future): %v", err)
	}

	// Job-shaped records under keys that are not job records.
	decoy := createTestJob("decoy", scheduler.OwnerTypeTenant, "acme")
	decoy.ID = 999
	decoy.NextRun = now.Add(-time.Hour)
	raw, err := json.Marshal(decoy)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := kv.Put(ctx, "t:acme:scheduler/executions/1", raw); err != nil {
		t.Fatalf("Put execution: %v", err)
	}
	if err := kv.Put(ctx, "t:acme:data/1", raw); err != nil {
		t.Fatalf("Put data: %v", err)
	}

	due, err := storage.GetJobsDueForExecution(ctx, now, 100)
	if err != nil {
		t.Fatalf("GetJobsDueForExecution: %v", err)
	}
	if len(due) != 2 {
		names := make([]string, 0, len(due))
		for _, j := range due {
			names = append(names, j.Name)
		}
		t.Fatalf("expected 2 due jobs, got %d: %v", len(due), names)
	}
	var sawSys, sawTenant bool
	for _, j := range due {
		switch j.Name {
		case "sys-due":
			sawSys = true
		case "acme-due":
			sawTenant = true
			if j.OwnerID != "acme" {
				t.Errorf("tenant job OwnerID = %q, want %q", j.OwnerID, "acme")
			}
		default:
			t.Errorf("unexpected due job %q", j.Name)
		}
	}
	if !sawSys || !sawTenant {
		t.Errorf("sawSys=%v sawTenant=%v, want both", sawSys, sawTenant)
	}
}

// TestGetJobsDueForExecution_TenantJobsShareLimit checks that the system
// and tenant scans share one limit.
func TestGetJobsDueForExecution_TenantJobsShareLimit(t *testing.T) {
	ctx := context.Background()
	storage := NewStorage(newMockKVStore())
	now := time.Now()

	for i := 0; i < 3; i++ {
		sys := createTestJob("sys", scheduler.OwnerTypeSystem, "")
		sys.NextRun = now.Add(-time.Hour)
		if err := storage.CreateJob(ctx, sys); err != nil {
			t.Fatalf("CreateJob(system): %v", err)
		}
		ten := createTestJob("acme", scheduler.OwnerTypeTenant, "acme")
		ten.NextRun = now.Add(-time.Hour)
		if err := storage.CreateJob(ctx, ten); err != nil {
			t.Fatalf("CreateJob(tenant): %v", err)
		}
	}

	due, err := storage.GetJobsDueForExecution(ctx, now, 4)
	if err != nil {
		t.Fatalf("GetJobsDueForExecution: %v", err)
	}
	if len(due) != 4 {
		t.Fatalf("expected 4 due jobs, got %d", len(due))
	}
}

// TestScheduler_RunsDueTenantJobAutomatically checks that the scheduler,
// over the real store, runs a tenant-owned job when it comes due.
func TestScheduler_RunsDueTenantJobAutomatically(t *testing.T) {
	cfg := scheduler.DefaultConfig()
	cfg.TickInterval = 10 * time.Millisecond
	engine := scheduler.New(cfg, NewStorage(newMockKVStore()))

	ran := make(chan *scheduler.Job, 4)
	engine.RegisterHandler("noop", func(_ context.Context, job *scheduler.Job) (*scheduler.JobResult, error) {
		ran <- job
		return &scheduler.JobResult{}, nil
	})
	startEngine(t, engine)

	job := &scheduler.Job{
		Name:           "acme-oneshot",
		OwnerType:      scheduler.OwnerTypeTenant,
		OwnerID:        "acme",
		TargetTenantID: "acme",
		Type:           scheduler.JobTypeOneShot,
		Handler:        "noop",
		Timeout:        time.Minute,
		ExecuteAt:      time.Now().Add(50 * time.Millisecond),
	}
	if err := engine.ScheduleJob(context.Background(), job); err != nil {
		t.Fatalf("ScheduleJob: %v", err)
	}

	select {
	case got := <-ran:
		if got.OwnerID != "acme" {
			t.Errorf("job OwnerID = %q, want %q", got.OwnerID, "acme")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("tenant job was not run within 10s")
	}
}
