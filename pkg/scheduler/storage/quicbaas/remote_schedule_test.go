package quicbaas

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// A tenant's remote job, scheduled through production storage, is fired,
// offered to the tenant's worker, completed by it, and its output read back
// from history: the whole schedule-to-worker path over the tenant namespace.
func TestRemoteJob_TenantScheduleToWorkerOverProductionStorage(t *testing.T) {
	const tenant, handler = "acme", "remote:renew-cert"
	cfg := scheduler.DefaultConfig()
	cfg.TickInterval = 20 * time.Millisecond
	engine := scheduler.New(cfg, NewStorage(newMockKVStore()))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = engine.Stop(context.Background()) }()

	job := &scheduler.Job{
		Name:      "renew-cert",
		OwnerType: scheduler.OwnerTypeTenant,
		OwnerID:   tenant,
		Type:      scheduler.JobTypeOneShot,
		Handler:   handler,
		ExecuteAt: time.Now(),
		Timeout:   time.Minute,
	}
	if err := engine.ScheduleJob(ctx, job); err != nil {
		t.Fatalf("ScheduleJob: %v", err)
	}

	var run *scheduler.RemoteRun
	for run == nil {
		runs, err := engine.ListRemoteRuns(ctx, tenant, []string{handler})
		if err != nil {
			t.Fatalf("ListRemoteRuns: %v", err)
		}
		for _, r := range runs {
			if r.JobID == job.ID {
				run = r
			}
		}
		if run == nil {
			select {
			case <-ctx.Done():
				t.Fatal("the tenant's scheduled remote job never reached its worker")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}

	output := []byte("-----BEGIN CERTIFICATE-----")
	if err := engine.CompleteRemoteRun(ctx, run.RemoteRunRef, &scheduler.JobResult{Success: true, Output: output}, nil); err != nil {
		t.Fatalf("CompleteRemoteRun: %v", err)
	}
	for {
		history, err := engine.GetJobHistory(ctx, job.ID, tenant, 10)
		if err != nil {
			t.Fatalf("GetJobHistory: %v", err)
		}
		if len(history) == 1 {
			if string(history[0].Result) != string(output) {
				t.Fatalf("history result %q, want the worker's output", history[0].Result)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("history holds %d records after the completion, want 1", len(history))
		case <-time.After(20 * time.Millisecond):
		}
	}
}
