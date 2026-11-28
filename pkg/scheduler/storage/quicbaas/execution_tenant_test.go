package quicbaas

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// A tenant job's execution is stored in the tenant's namespace, where the
// history reader looks; a system execution stays in the system namespace.
func TestSaveExecution_TenantExecutionInTenantNamespace(t *testing.T) {
	ctx := context.Background()
	s := NewStorage(newMockKVStore())

	if err := s.SaveExecution(ctx, &scheduler.JobExecution{ID: 1, JobID: 5, TenantID: "acme",
		Status: scheduler.JobStatusCompleted, StartedAt: time.Now()}); err != nil {
		t.Fatalf("SaveExecution(acme): %v", err)
	}
	if err := s.SaveExecution(ctx, &scheduler.JobExecution{ID: 2, JobID: 5,
		Status: scheduler.JobStatusCompleted, StartedAt: time.Now()}); err != nil {
		t.Fatalf("SaveExecution(system): %v", err)
	}

	ids := func(tenantID string) map[uint64]bool {
		t.Helper()
		execs, err := s.GetExecutions(ctx, 5, tenantID, 10)
		if err != nil {
			t.Fatalf("GetExecutions(%q): %v", tenantID, err)
		}
		got := map[uint64]bool{}
		for _, e := range execs {
			got[e.ID] = true
		}
		return got
	}

	acme := ids("acme")
	if !acme[1] {
		t.Errorf("acme's execution is not returned for acme: %v", acme)
	}
	if acme[2] {
		t.Errorf("the system execution is returned for acme: %v", acme)
	}
	sys := ids("")
	if !sys[2] {
		t.Errorf("the system execution is not returned for the system tenant: %v", sys)
	}
	if sys[1] {
		t.Errorf("acme's execution is returned for the system tenant: %v", sys)
	}
}

// After the engine runs a tenant job on its own, the job's history read in
// the tenant's namespace shows the run, succeeded or failed.
func TestScheduler_TenantJobHistoryAfterAutomaticRun(t *testing.T) {
	cfg := scheduler.DefaultConfig()
	cfg.TickInterval = 10 * time.Millisecond
	engine := scheduler.New(cfg, NewStorage(newMockKVStore()))

	if err := engine.RegisterHandler("noop", func(context.Context, *scheduler.Job) (*scheduler.JobResult, error) {
		return &scheduler.JobResult{Success: true}, nil
	}); err != nil {
		t.Fatalf("RegisterHandler(noop): %v", err)
	}
	if err := engine.RegisterHandler("fails", func(context.Context, *scheduler.Job) (*scheduler.JobResult, error) {
		return nil, errors.New("boom")
	}); err != nil {
		t.Fatalf("RegisterHandler(fails): %v", err)
	}
	startEngine(t, engine)

	schedule := func(name, handler string) uint64 {
		t.Helper()
		job := &scheduler.Job{
			Name:           name,
			OwnerType:      scheduler.OwnerTypeTenant,
			OwnerID:        "acme",
			TargetTenantID: "acme",
			Type:           scheduler.JobTypeOneShot,
			Handler:        handler,
			Timeout:        time.Minute,
			ExecuteAt:      time.Now().Add(50 * time.Millisecond),
		}
		if err := engine.ScheduleJob(context.Background(), job); err != nil {
			t.Fatalf("ScheduleJob(%s): %v", name, err)
		}
		return job.ID
	}
	okID := schedule("acme-ok", "noop")
	failID := schedule("acme-fail", "fails")

	waitFor := func(jobID uint64, want scheduler.JobStatus) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			execs, err := engine.GetJobHistory(context.Background(), jobID, "acme", 10)
			if err == nil && len(execs) == 1 && execs[0].Status == want {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		execs, err := engine.GetJobHistory(context.Background(), jobID, "acme", 10)
		t.Fatalf("job %d: want one %s execution in acme's history within 10s, got %d (err %v)",
			jobID, want, len(execs), err)
	}
	waitFor(okID, scheduler.JobStatusCompleted)
	waitFor(failID, scheduler.JobStatusFailed)
}
