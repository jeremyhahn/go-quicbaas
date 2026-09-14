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
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// Fixtures
// =============================================================================

// fakeLiveness answers "running here" for a fixed set of jobs.
type fakeLiveness map[string]bool

func (f fakeLiveness) JobRunning(tenantID, jobID string) bool {
	return f[tenantID+"/"+jobID]
}

// recordingDeaths remembers every death it was asked to record and answers
// with a scripted error.
type recordingDeaths struct {
	mu     sync.Mutex
	leases []*JobLease
	detail []string
	err    error
}

func (r *recordingDeaths) RecordJobDeath(_ context.Context, lease *JobLease, evidence string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.leases = append(r.leases, lease)
	r.detail = append(r.detail, evidence)
	return r.err
}

func (r *recordingDeaths) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.leases)
}

// reaperFixture is a reaper over a process store with a movable clock.
type reaperFixture struct {
	store  JobLeaseStore
	clock  *fakeClock
	live   fakeLiveness
	deaths *recordingDeaths
	reaper *JobReaper
}

func newReaperFixture(t *testing.T) *reaperFixture {
	t.Helper()
	store, err := NewProcessJobLeaseStore(leaseTestTTL)
	if err != nil {
		t.Fatalf("NewProcessJobLeaseStore: %v", err)
	}
	clock := newFakeClock()
	store.(*processJobLeaseStore).now = clock.Now
	live := fakeLiveness{}
	deaths := &recordingDeaths{}
	reaper, err := NewJobReaper(store, live, deaths, leaseTestTTL)
	if err != nil {
		t.Fatalf("NewJobReaper: %v", err)
	}
	reaper.now = clock.Now
	return &reaperFixture{store: store, clock: clock, live: live, deaths: deaths, reaper: reaper}
}

func (f *reaperFixture) acquire(t *testing.T, lease *JobLease) *JobLease {
	t.Helper()
	if err := f.store.Acquire(context.Background(), lease); err != nil {
		t.Fatalf("acquire %s: %v", lease.JobID, err)
	}
	return lease
}

func findFinding(report JobReapReport, jobID string) (JobReapFinding, bool) {
	for _, f := range report.Findings {
		if f.JobID == jobID {
			return f, true
		}
	}
	return JobReapFinding{}, false
}

// =============================================================================
// Verdicts
// =============================================================================

// TestJobReaper_StaleAndNotLocalIsReaped is the founding case: a lease whose
// heartbeat aged out on a node that is not this one is concluded as a
// death, with evidence naming the node, the generation and the age.
func TestJobReaper_StaleAndNotLocalIsReaped(t *testing.T) {
	f := newReaperFixture(t)
	f.acquire(t, &JobLease{TenantID: "acme", ProjectID: "p", Ref: "ref-9", TaskID: "task-9", JobID: "job-9", Generation: 2, Node: "node-dead"})
	f.clock.Advance(leaseTestTTL + time.Second)

	report, err := f.reaper.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	finding, ok := findFinding(report, "job-9")
	if !ok || finding.Verdict != JobReapReaped {
		t.Fatalf("expected reaped, got %+v", finding)
	}
	if finding.Age < leaseTestTTL || finding.Node != "node-dead" || finding.TaskID != "task-9" || finding.TenantID != "acme" {
		t.Fatalf("finding is missing its facts: %+v", finding)
	}
	if f.deaths.count() != 1 {
		t.Fatalf("expected one death, got %d", f.deaths.count())
	}
	if lease := f.deaths.leases[0]; lease.JobID != "job-9" || lease.TaskID != "task-9" {
		t.Fatalf("the death names the wrong lease: %+v", lease)
	}
	for _, fact := range []string{"node-dead", "generation 2", "job reaper", "not running on this node"} {
		if !strings.Contains(f.deaths.detail[0], fact) {
			t.Fatalf("evidence lacks %q: %s", fact, f.deaths.detail[0])
		}
	}
	if report.Count(JobReapReaped) != 1 {
		t.Fatal("Count must agree with the findings")
	}
}

// TestJobReaper_LiveLeaseIsUntouched proves a fresh heartbeat is never
// questioned, whichever node holds it.
func TestJobReaper_LiveLeaseIsUntouched(t *testing.T) {
	f := newReaperFixture(t)
	f.acquire(t, &JobLease{TenantID: "acme", Ref: "a", TaskID: "t", JobID: "job-a", Generation: 1, Node: "elsewhere"})
	f.clock.Advance(leaseTestTTL / 2)

	report, err := f.reaper.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	finding, ok := findFinding(report, "job-a")
	if !ok || finding.Verdict != JobReapHealthy {
		t.Fatalf("expected healthy, got %+v", finding)
	}
	if f.deaths.count() != 0 {
		t.Fatal("a live lease must not be concluded")
	}
}

// TestJobReaper_LocalLateHeartbeatIsRunLive proves this node's own running
// job is never reaped however late its heartbeat.
func TestJobReaper_LocalLateHeartbeatIsRunLive(t *testing.T) {
	f := newReaperFixture(t)
	f.acquire(t, &JobLease{TenantID: "acme", Ref: "a", TaskID: "t", JobID: "job-local", Generation: 1, Node: "node-a"})
	f.live["acme/job-local"] = true
	f.clock.Advance(10 * leaseTestTTL)

	report, err := f.reaper.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	finding, _ := findFinding(report, "job-local")
	if finding.Verdict != JobReapRunLive || !strings.Contains(finding.Evidence, "running in this process") {
		t.Fatalf("expected run_live, got %+v", finding)
	}
	if f.deaths.count() != 0 {
		t.Fatal("a local running job must not be concluded")
	}
}

// TestJobReaper_ReleasedLeasesAreSkipped proves a concluded job is not
// examined at all.
func TestJobReaper_ReleasedLeasesAreSkipped(t *testing.T) {
	f := newReaperFixture(t)
	lease := f.acquire(t, &JobLease{TenantID: "acme", Ref: "a", TaskID: "t", JobID: "job-done", Generation: 1, Node: "gone"})
	if err := f.store.Release(context.Background(), lease); err != nil {
		t.Fatalf("release: %v", err)
	}
	f.clock.Advance(10 * leaseTestTTL)

	report, err := f.reaper.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(report.Findings) != 0 || f.deaths.count() != 0 {
		t.Fatalf("a released lease is not a finding: %+v", report.Findings)
	}
}

// TestJobReaper_OrphanedLeases proves the two orphan shapes: a lease with no
// task, and a lease whose task already moved past the job.
func TestJobReaper_OrphanedLeases(t *testing.T) {
	f := newReaperFixture(t)
	f.acquire(t, &JobLease{TenantID: "acme", Ref: "job-lead", JobID: "job-lead", Generation: 1, Node: "gone"})
	f.acquire(t, &JobLease{TenantID: "acme", Ref: "b", TaskID: "task-b", JobID: "job-moved", Generation: 1, Node: "gone"})
	f.clock.Advance(leaseTestTTL + time.Second)

	report, err := f.reaper.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	lead, _ := findFinding(report, "job-lead")
	if lead.Verdict != JobReapOrphaned || !strings.Contains(lead.Evidence, "bound to no task") {
		t.Fatalf("a taskless dead job is orphaned: %+v", lead)
	}

	f.deaths.err = &AttemptConcludedError{TaskID: "task-b", JobID: "job-moved", SpawnedJobID: "job-next", Status: TaskStatusClaimed}
	report, err = f.reaper.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	moved, _ := findFinding(report, "job-moved")
	if moved.Verdict != JobReapOrphaned || !strings.Contains(moved.Evidence, "already moved past it") {
		t.Fatalf("a job its task no longer names is orphaned: %+v", moved)
	}
}

// TestJobReaper_DeathRecordFailureDoesNotStopThePass proves one job's
// failure to conclude is reported on its finding and the sweep goes on.
func TestJobReaper_DeathRecordFailureDoesNotStopThePass(t *testing.T) {
	f := newReaperFixture(t)
	f.acquire(t, &JobLease{TenantID: "acme", Ref: "a", TaskID: "ta", JobID: "job-a", Generation: 1, Node: "gone"})
	f.acquire(t, &JobLease{TenantID: "acme", Ref: "b", TaskID: "tb", JobID: "job-b", Generation: 1, Node: "gone"})
	f.clock.Advance(leaseTestTTL + time.Second)
	f.deaths.err = errors.New("store away")

	report, err := f.reaper.Sweep(context.Background())
	if err != nil {
		t.Fatalf("a per-lease failure must not fail the pass: %v", err)
	}
	if len(report.Findings) != 2 || f.deaths.count() != 2 {
		t.Fatalf("both leases must be examined: findings=%d deaths=%d", len(report.Findings), f.deaths.count())
	}
	for _, finding := range report.Findings {
		if finding.Verdict != JobReapRunLive || !strings.Contains(finding.Evidence, "could not be recorded") {
			t.Fatalf("an unrecorded death is left for the next pass: %+v", finding)
		}
	}
	// Sorted by tenant then job.
	if report.Findings[0].JobID != "job-a" || report.Findings[1].JobID != "job-b" {
		t.Fatal("findings are sorted by job")
	}
}

// listFailingStore cannot list.
type listFailingStore struct {
	JobLeaseStore
}

func (s *listFailingStore) List(_ context.Context, _, _ string) ([]*JobLease, error) {
	return nil, &JobLeaseError{Sentinel: ErrJobLeaseBackend, Message: "lease.scan", Cause: errors.New("down")}
}

// TestJobReaper_ListFailureFailsThePass proves the pass fails only when the
// leases cannot be read at all.
func TestJobReaper_ListFailureFailsThePass(t *testing.T) {
	f := newReaperFixture(t)
	reaper, err := NewJobReaper(&listFailingStore{JobLeaseStore: f.store}, f.live, f.deaths, leaseTestTTL)
	if err != nil {
		t.Fatalf("NewJobReaper: %v", err)
	}
	if _, err := reaper.Sweep(context.Background()); !errors.Is(err, ErrJobLeaseBackend) {
		t.Fatalf("expected ErrJobLeaseBackend, got %v", err)
	}
}

// TestNewJobReaper_RefusesMissingDependencies proves the constructor fails
// closed.
func TestNewJobReaper_RefusesMissingDependencies(t *testing.T) {
	f := newReaperFixture(t)
	for name, args := range map[string]struct {
		store JobLeaseStore
		live  JobLivenessRegistry
		death JobDeathRecorder
		ttl   time.Duration
	}{
		"no store":    {nil, f.live, f.deaths, leaseTestTTL},
		"no registry": {f.store, nil, f.deaths, leaseTestTTL},
		"no deaths":   {f.store, f.live, nil, leaseTestTTL},
		"zero ttl":    {f.store, f.live, f.deaths, 0},
	} {
		t.Run(name, func(t *testing.T) {
			reaper, err := NewJobReaper(args.store, args.live, args.death, args.ttl)
			if !errors.Is(err, ErrJobLeaseInvalidInput) || reaper != nil {
				t.Fatalf("expected a refusal, got reaper=%v err=%v", reaper, err)
			}
		})
	}
	if JobReapReaped.String() != "reaped" {
		t.Fatal("verdicts render as their name")
	}
}

// =============================================================================
// The service as death recorder
// =============================================================================

// TestService_RecordJobDeath_ConcludesTheTaskAndReleasesTheLease proves the
// service's death path end to end: the persisted job is marked failed and
// abandoned, the task is requeued (charged), and the lease is released.
func TestService_RecordJobDeath_ConcludesTheTaskAndReleasesTheLease(t *testing.T) {
	svc, store := newConcludingService(t)
	kv := newMockKVStore()
	svc.kvStore = kv
	ctx := context.Background()
	seeded := seedSpawnedTask(t, store, &Task{Attempts: 1})

	persisted := &JobStatus{JobID: "job-1", TenantID: "acme", State: JobStateExecuting, Description: "d", Config: &JobConfig{TenantID: "acme", TaskID: seeded.ID}}
	svc.persistJobStatus("acme", persisted)

	lease := &JobLease{TenantID: "acme", Ref: seeded.ID, TaskID: seeded.ID, JobID: "job-1", Generation: 1, Node: "node-dead"}
	if err := svc.jobLeases.Acquire(ctx, lease); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := svc.RecordJobDeath(ctx, lease, "node-dead stopped"); err != nil {
		t.Fatalf("RecordJobDeath: %v", err)
	}

	task := readTask(t, store, seeded.ID)
	if task.Status != TaskStatusPending || task.ChargedAttempts() != 1 || task.Result.Metadata["failure_class"] != JobFailureClassAbandoned {
		t.Fatalf("expected a charged requeue, got %+v", task)
	}
	if !strings.Contains(task.Result.Output, "node-dead stopped") {
		t.Fatalf("the evidence reaches the task: %+v", task.Result)
	}
	got, err := svc.jobLeases.Get(ctx, "acme", "", "", seeded.ID)
	if err != nil || !got.Released {
		t.Fatalf("lease must be released: %+v %v", got, err)
	}
	status, err := svc.loadJobStatus("acme", "job-1")
	if err != nil {
		t.Fatalf("loadJobStatus: %v", err)
	}
	if status.State != JobStateFailed || status.FailureClass != JobFailureClassAbandoned || status.Description != "d" || status.Node != "node-dead" {
		t.Fatalf("the persisted job must be laid over with the death: %+v", status)
	}

	// A lease with no task is simply released.
	lead := &JobLease{TenantID: "acme", Ref: "job-lead", JobID: "job-lead", Generation: 1, Node: "node-dead"}
	if err := svc.jobLeases.Acquire(ctx, lead); err != nil {
		t.Fatalf("acquire lead: %v", err)
	}
	if err := svc.RecordJobDeath(ctx, lead, "gone"); err != nil {
		t.Fatalf("RecordJobDeath lead: %v", err)
	}
	if got, err := svc.jobLeases.Get(ctx, "acme", "", "", "job-lead"); err != nil || !got.Released {
		t.Fatalf("a taskless lease is released: %+v %v", got, err)
	}
	if err := svc.RecordJobDeath(ctx, nil, "x"); !errors.Is(err, ErrJobLeaseInvalidInput) {
		t.Fatalf("a nil lease is refused: %v", err)
	}
}

// TestService_Sweep_IsANoopWhenNothingIsBound proves the sweep builds its
// detectors once and does nothing on a quiet node.
func TestService_Sweep_IsANoopWhenNothingIsBound(t *testing.T) {
	svc := newTestService(t)
	report, err := svc.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(report.Reap.Findings) != 0 || len(report.Stall.Findings) != 0 {
		t.Fatalf("expected an empty report, got %+v", report)
	}
	first := svc.reaper
	if _, err := svc.Sweep(context.Background()); err != nil || svc.reaper != first {
		t.Fatal("the sweepers are built once")
	}

	// A service whose lease store was unwired cannot sweep, and says so.
	broken := newTestService(t)
	broken.jobLeases = nil
	if _, err := broken.Sweep(context.Background()); !errors.Is(err, ErrJobLeaseInvalidInput) {
		t.Fatalf("expected the reaper's refusal, got %v", err)
	}
}

// TestService_JobRunning proves the liveness registry answers from the jobs
// map and a loop's finished flag.
func TestService_JobRunning(t *testing.T) {
	svc := newTestService(t)
	if svc.JobRunning("acme", "none") {
		t.Fatal("an unknown job is not running")
	}
	run := newJobRun(JobRunConfig{Config: &JobConfig{}, Agents: svc, Logger: &controlLoopMockLogger{}})
	svc.jobs.Store("acme/job-1", &runningJob{run: run})
	if !svc.JobRunning("acme", "job-1") {
		t.Fatal("a registered, unfinished run is running")
	}
	run.finished.Store(true)
	if svc.JobRunning("acme", "job-1") {
		t.Fatal("a finished loop is not running")
	}
	svc.jobs.Store("acme/job-2", &runningJob{})
	if svc.JobRunning("acme", "job-2") {
		t.Fatal("a registration with no loop is not running")
	}
	svc.jobs.Store("acme/job-3", "not a job")
	if svc.JobRunning("acme", "job-3") {
		t.Fatal("a malformed registration is not running")
	}
}
