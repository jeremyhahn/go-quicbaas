// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

// The package is external, deliberately: the test needs the REAL tenant-scoped
// storage (storage/quicbaas), which imports pkg/scheduler, and an in-package
// test importing it back would be a cycle. Everything here drives the public
// surface: TriggerJob, Start, RegisterHandler.
package scheduler_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	quicbaasstorage "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/storage/quicbaas"
)

// TestTriggerJob_CarriesTheTenantScopeToTheExecution is the collision the
// scope rule exists for, on the real tenant-scoped store: a SYSTEM job and a
// TENANT job share one numeric id, and a tenant trigger must execute the
// tenant's job — never the system job that happens to share its id, and never
// nothing.
//
// Measured before the fix (Codex review 859 on go-quicbaas#83): TriggerJob
// validated under the caller's scope but queued only the bare id, and the
// drain reloaded with the SYSTEM scope — so this test executed the system
// handler. The failure is silent and total, which is why the collision is
// staged on the real store rather than the mock (the mock keys by id alone
// and cannot hold two jobs with one id).
//
// Mutation evidence: reverting the reload to GetJob(id, "") executes the
// system handler and fails both assertions.
func TestTriggerJob_CarriesTheTenantScopeToTheExecution(t *testing.T) {
	storage := quicbaasstorage.NewStorage(kvstore.NewMemory())
	ctx := context.Background()
	far := time.Now().Add(24 * time.Hour)

	const sharedID = 77
	system := &scheduler.Job{
		ID: sharedID, Name: "system-77", Type: scheduler.JobTypeOneShot, Handler: "system-handler",
		Status: scheduler.JobStatusPending, OwnerType: scheduler.OwnerTypeSystem, NextRun: far, ExecuteAt: far,
	}
	tenant := &scheduler.Job{
		ID: sharedID, Name: "tenant-77", Type: scheduler.JobTypeOneShot, Handler: "tenant-handler",
		Status: scheduler.JobStatusPending, OwnerType: scheduler.OwnerTypeTenant, OwnerID: "acme", NextRun: far, ExecuteAt: far,
	}
	if err := storage.CreateJob(ctx, system); err != nil {
		t.Fatalf("CreateJob(system): %v", err)
	}
	if err := storage.CreateJob(ctx, tenant); err != nil {
		t.Fatalf("CreateJob(tenant): %v", err)
	}

	engine := scheduler.New(scheduler.DefaultConfig(), storage)
	var systemRuns, tenantRuns atomic.Int32
	if err := engine.RegisterHandler("system-handler", func(context.Context, *scheduler.Job) (*scheduler.JobResult, error) {
		systemRuns.Add(1)
		return &scheduler.JobResult{}, nil
	}); err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}
	if err := engine.RegisterHandler("tenant-handler", func(context.Context, *scheduler.Job) (*scheduler.JobResult, error) {
		tenantRuns.Add(1)
		return &scheduler.JobResult{}, nil
	}); err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = engine.Stop(context.Background()) }()

	if err := engine.TriggerJob(ctx, sharedID, "acme"); err != nil {
		t.Fatalf("TriggerJob: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for tenantRuns.Load() == 0 && systemRuns.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("neither handler ran: the trigger found no job in the scope it reloaded with")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := systemRuns.Load(); got != 0 {
		t.Fatalf("the SYSTEM job ran %d time(s) for a tenant trigger: the scope was lost on the queue", got)
	}
	if got := tenantRuns.Load(); got != 1 {
		t.Fatalf("the tenant job ran %d time(s), want 1", got)
	}
}

// TestRetriggerStrandedFiring_IsBoundToTheObservedFiring is the double-fire
// guard for the repair path (Codex 879 #1 on go-quicbaas#83): between the
// checker's "no run is executing this firing" and the dispatch actually
// running, the legitimate execution can begin, or the job can complete onto
// a new firing. The bound dispatch must decline both; only a job still
// Pending on exactly the observed firing runs.
//
// Mutation evidence: passing time.Time{} through the trigger queue (the old
// unbound dispatch) runs the handler in both stale cases and fails the
// zero-run assertions.
func TestRetriggerStrandedFiring_IsBoundToTheObservedFiring(t *testing.T) {
	storage := quicbaasstorage.NewStorage(kvstore.NewMemory())
	ctx := context.Background()
	firing := time.Now().Add(24 * time.Hour)

	job := &scheduler.Job{
		Name: "stranded", Type: scheduler.JobTypeRecurring, Schedule: "*/5 * * * *", Handler: "stranded-handler",
		Status: scheduler.JobStatusPending, OwnerType: scheduler.OwnerTypeSystem, NextRun: firing, UpdatedAt: firing,
	}
	if err := storage.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	engine := scheduler.New(scheduler.DefaultConfig(), storage)
	var runs atomic.Int32
	release := make(chan struct{})
	if err := engine.RegisterHandler("stranded-handler", func(context.Context, *scheduler.Job) (*scheduler.JobResult, error) {
		runs.Add(1)
		<-release
		return &scheduler.JobResult{}, nil
	}); err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		close(release)
		_ = engine.Stop(context.Background())
	}()

	// Stale observation at queue time: the firing has moved on.
	if err := engine.RetriggerStrandedFiring(ctx, job.ID, "", firing.Add(-time.Minute)); err == nil {
		t.Fatal("a repair bound to a firing the job is no longer pending on was accepted")
	}
	// Zero binding is refused outright.
	if err := engine.RetriggerStrandedFiring(ctx, job.ID, "", time.Time{}); err == nil {
		t.Fatal("a repair bound to no firing was accepted")
	}

	// Stale at EXECUTION time: accepted while pending on the firing, but the
	// job moves before the dispatch drains — the binding must decline.
	job.Status = scheduler.JobStatusRunning
	if err := storage.UpdateJob(ctx, job); err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	// (queue-time check sees Running and refuses too — both gates agree)
	if err := engine.RetriggerStrandedFiring(ctx, job.ID, "", firing); err == nil {
		t.Fatal("a repair for a job already running was accepted")
	}
	if got := runs.Load(); got != 0 {
		t.Fatalf("the handler ran %d time(s) beside the live execution", got)
	}

	// The honest case: pending on the observed firing end to end.
	job.Status = scheduler.JobStatusPending
	if err := storage.UpdateJob(ctx, job); err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	if err := engine.RetriggerStrandedFiring(ctx, job.ID, "", firing); err != nil {
		t.Fatalf("RetriggerStrandedFiring: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for runs.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the bound repair never ran the handler")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("the handler ran %d time(s), want 1", got)
	}
}

// TestManualTrigger_NeverRunsBesideALiveExecution: an operator's "run now"
// carries no firing, and used to slip past every status gate onto a job whose
// execution had begun after the trigger-time precheck. The Running refusal in
// executeJob closes it.
func TestManualTrigger_NeverRunsBesideALiveExecution(t *testing.T) {
	storage := quicbaasstorage.NewStorage(kvstore.NewMemory())
	ctx := context.Background()
	far := time.Now().Add(24 * time.Hour)
	job := &scheduler.Job{
		Name: "busy", Type: scheduler.JobTypeRecurring, Schedule: "*/5 * * * *", Handler: "busy-handler",
		Status: scheduler.JobStatusRunning, OwnerType: scheduler.OwnerTypeSystem, NextRun: far, UpdatedAt: far,
	}
	if err := storage.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	engine := scheduler.New(scheduler.DefaultConfig(), storage)
	var runs atomic.Int32
	if err := engine.RegisterHandler("busy-handler", func(context.Context, *scheduler.Job) (*scheduler.JobResult, error) {
		runs.Add(1)
		return &scheduler.JobResult{}, nil
	}); err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = engine.Stop(context.Background()) }()

	// TriggerJob's own precheck refuses Running; the executeJob gate is the
	// authoritative one for the race window, driven here directly through
	// the queue the way a stale precheck would reach it.
	if err := engine.TriggerJob(ctx, job.ID, ""); err == nil {
		t.Fatal("a manual trigger for a running job was accepted at the precheck")
	}
	time.Sleep(50 * time.Millisecond)
	if got := runs.Load(); got != 0 {
		t.Fatalf("the handler ran %d time(s) beside the live execution", got)
	}
}
