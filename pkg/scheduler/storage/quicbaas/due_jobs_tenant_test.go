package quicbaas

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// TestGetJobsDueForExecution_IncludesTenantJobs checks that due-job
// discovery returns a tenant's due jobs as well as the system's, and skips
// tenant keys that are not job records.
func TestGetJobsDueForExecution_IncludesEveryTenantsJobs(t *testing.T) {
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

	due, err := storage.GetJobsDueForExecution(ctx, now, 100, nil)
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

	due, err := storage.GetJobsDueForExecution(ctx, now, 4, nil)
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

// TestClusteredScheduler_ForeignAffinityJobsDoNotStarveOtherTenants pins the
// fix for a tenant that fills every due-job batch with jobs pinned to a node
// that does not exist: this node can never run them, so they must not crowd
// out another tenant's due job.
func TestClusteredScheduler_ForeignAffinityJobsDoNotStarveOtherTenants(t *testing.T) {
	kv := newMockKVStore()
	store := newClusteredStoreOver(t, kv)

	cfg := scheduler.DefaultConfig()
	cfg.TickInterval = 10 * time.Millisecond
	cfg.MaxConcurrentJobs = 2

	engine, err := scheduler.NewWithClustering(cfg, store, "node-a", nil)
	if err != nil {
		t.Fatalf("NewWithClustering: %v", err)
	}
	ran := make(chan uint64, 16)
	engine.RegisterHandler("noop", func(_ context.Context, job *scheduler.Job) (*scheduler.JobResult, error) {
		ran <- job.ID
		return &scheduler.JobResult{}, nil
	})
	startEngine(t, engine)

	ctx := context.Background()
	past := time.Now().Add(-time.Hour)
	ghosts := map[uint64]bool{}
	for i := 0; i < 2; i++ {
		j := createTestJob("aaa-pinned", scheduler.OwnerTypeTenant, "aaa")
		j.Handler = "noop"
		j.ExecuteAt = past
		j.NextRun = past
		j.NodeAffinity = "node-ghost"
		if err := store.CreateJob(ctx, j); err != nil {
			t.Fatalf("CreateJob(aaa): %v", err)
		}
		ghosts[j.ID] = true
	}
	z := createTestJob("zzz-job", scheduler.OwnerTypeTenant, "zzz")
	z.Handler = "noop"
	z.ExecuteAt = past
	z.NextRun = past
	if err := store.CreateJob(ctx, z); err != nil {
		t.Fatalf("CreateJob(zzz): %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case id := <-ran:
			if ghosts[id] {
				t.Fatalf("job %d pinned to node-ghost ran on node-a", id)
			}
			if id == z.ID {
				return
			}
		case <-deadline:
			t.Fatal("zzz tenant job was not run within 5s")
		}
	}
}

// neverLeader is a leader check that always reports a follower.
type neverLeader struct{}

func (neverLeader) IsLeader() (bool, error) { return false, nil }

// TestClusteredScheduler_FollowerRunsItsPinnedJobBehindLeaderOnlyJobs proves
// a follower's due batch holds only jobs it may run: a full batch of due
// unpinned tenant jobs (leader-only on a follower) ordered before a tenant
// job pinned to this node does not keep the pinned job from running.
func TestClusteredScheduler_FollowerRunsItsPinnedJobBehindLeaderOnlyJobs(t *testing.T) {
	kv := newMockKVStore()
	store := newClusteredStoreOver(t, kv)

	cfg := scheduler.DefaultConfig()
	cfg.TickInterval = 10 * time.Millisecond
	cfg.MaxConcurrentJobs = 2

	engine, err := scheduler.NewWithClustering(cfg, store, "node-a", neverLeader{})
	if err != nil {
		t.Fatalf("NewWithClustering: %v", err)
	}
	ran := make(chan uint64, 16)
	engine.RegisterHandler("noop", func(_ context.Context, job *scheduler.Job) (*scheduler.JobResult, error) {
		ran <- job.ID
		return &scheduler.JobResult{}, nil
	})
	startEngine(t, engine)

	ctx := context.Background()
	past := time.Now().Add(-time.Hour)
	leaderOnly := map[uint64]bool{}
	for i := 0; i < 2; i++ {
		j := createTestJob("aaa-unpinned", scheduler.OwnerTypeTenant, "aaa")
		j.Handler = "noop"
		j.ExecuteAt = past
		j.NextRun = past
		if err := store.CreateJob(ctx, j); err != nil {
			t.Fatalf("CreateJob(aaa): %v", err)
		}
		leaderOnly[j.ID] = true
	}
	z := createTestJob("zzz-local", scheduler.OwnerTypeTenant, "zzz")
	z.Handler = "noop"
	z.ExecuteAt = past
	z.NextRun = past
	z.NodeAffinity = "node-a"
	if err := store.CreateJob(ctx, z); err != nil {
		t.Fatalf("CreateJob(zzz): %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case id := <-ran:
			if leaderOnly[id] {
				t.Fatalf("unpinned job %d ran on follower node-a", id)
			}
			if id == z.ID {
				return
			}
		case <-deadline:
			t.Fatal("zzz job pinned to node-a was not run on the follower within 5s")
		}
	}
}

// countingKV counts the job-record reads the due scan makes and can fail
// job-record writes.
type countingKV struct {
	*mockKVStore
	recordGets    atomic.Int32
	failRecordPut bool
}

func newCountingKV() *countingKV { return &countingKV{mockKVStore: newMockKVStore()} }

func (c *countingKV) Get(ctx context.Context, key string) ([]byte, error) {
	if _, _, ok := splitTenantJobKey(key); ok {
		c.recordGets.Add(1)
	}
	return c.mockKVStore.Get(ctx, key)
}

func (c *countingKV) Put(ctx context.Context, key string, value []byte) error {
	if _, _, ok := splitTenantJobKey(key); ok && c.failRecordPut {
		return errors.New("record write refused")
	}
	return c.mockKVStore.Put(ctx, key, value)
}

// dueIDs runs the due scan and returns the IDs it found.
func dueIDs(t *testing.T, s *Storage, now time.Time) map[uint64]bool {
	t.Helper()
	jobs, err := s.GetJobsDueForExecution(context.Background(), now, 10, nil)
	if err != nil {
		t.Fatalf("GetJobsDueForExecution: %v", err)
	}
	ids := map[uint64]bool{}
	for _, j := range jobs {
		ids[j.ID] = true
	}
	return ids
}

// newTenantJob builds a tenant job of acme with the given status and next run.
func newTenantJob(name string, status scheduler.JobStatus, nextRun time.Time) *scheduler.Job {
	j := createTestJob(name, scheduler.OwnerTypeTenant, "acme")
	j.Status = status
	j.ExecuteAt = nextRun
	j.NextRun = nextRun
	return j
}

// TestGetJobsDueForExecution_SkipsTenantJobsThatCannotBeDue proves the due
// scan reads only the record of a tenant job its index summary says may be
// due: completed, canceled and future jobs are passed over unread.
func TestGetJobsDueForExecution_SkipsTenantJobsThatCannotBeDue(t *testing.T) {
	ctx := context.Background()
	kv := newCountingKV()
	s := NewStorage(kv)
	now := time.Now()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)

	jobs := []*scheduler.Job{
		newTenantJob("completed", scheduler.JobStatusCompleted, past),
		newTenantJob("future", scheduler.JobStatusPending, future),
		newTenantJob("canceled", scheduler.JobStatusCanceled, past),
	}
	due := newTenantJob("due", scheduler.JobStatusPending, past)
	jobs = append(jobs, due)
	for _, j := range jobs {
		if err := s.CreateJob(ctx, j); err != nil {
			t.Fatalf("CreateJob(%s): %v", j.Name, err)
		}
	}

	kv.recordGets.Store(0)
	ids := dueIDs(t, s, now)
	if len(ids) != 1 || !ids[due.ID] {
		t.Fatalf("due jobs = %v, want only %d", ids, due.ID)
	}
	if got := kv.recordGets.Load(); got != 1 {
		t.Fatalf("job records read = %d, want 1 (only the due job)", got)
	}
}

// TestGetJobsDueForExecution_AnUpdateMovesTheSummary proves UpdateJob keeps
// the summary in step with the record, in both directions.
func TestGetJobsDueForExecution_AnUpdateMovesTheSummary(t *testing.T) {
	ctx := context.Background()
	kv := newCountingKV()
	s := NewStorage(kv)
	now := time.Now()
	past := now.Add(-time.Hour)

	j := newTenantJob("moves", scheduler.JobStatusPending, past)
	if err := s.CreateJob(ctx, j); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	j.Status = scheduler.JobStatusCompleted
	if err := s.UpdateJob(ctx, j); err != nil {
		t.Fatalf("UpdateJob(completed): %v", err)
	}
	kv.recordGets.Store(0)
	if ids := dueIDs(t, s, now); len(ids) != 0 {
		t.Fatalf("after completion, due jobs = %v, want none", ids)
	}
	if got := kv.recordGets.Load(); got != 0 {
		t.Fatalf("after completion, job records read = %d, want 0", got)
	}

	j.Status = scheduler.JobStatusPending
	j.NextRun = past
	if err := s.UpdateJob(ctx, j); err != nil {
		t.Fatalf("UpdateJob(pending): %v", err)
	}
	if ids := dueIDs(t, s, now); len(ids) != 1 || !ids[j.ID] {
		t.Fatalf("after re-pending, due jobs = %v, want %d", ids, j.ID)
	}
}

// TestGetJobsDueForExecution_ABareIndexValueStillFindsItsJob proves an index
// value with no summary (a bare tenant ID) makes the scan read the record.
func TestGetJobsDueForExecution_ABareIndexValueStillFindsItsJob(t *testing.T) {
	ctx := context.Background()
	kv := newCountingKV()
	s := NewStorage(kv)
	now := time.Now()

	j := newTenantJob("bare", scheduler.JobStatusPending, now.Add(-time.Hour))
	if err := s.CreateJob(ctx, j); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if err := kv.mockKVStore.Put(ctx, keyForTenantJobIndex(j.ID, "acme"), []byte("acme")); err != nil {
		t.Fatalf("overwrite index entry: %v", err)
	}
	if ids := dueIDs(t, s, now); len(ids) != 1 || !ids[j.ID] {
		t.Fatalf("due jobs = %v, want %d", ids, j.ID)
	}
}

// TestTenantJobIndexBackfill_WritesTheSummary proves the open-time
// reconcile summarizes each record, so the scan skips the completed one.
func TestTenantJobIndexBackfill_WritesTheSummary(t *testing.T) {
	ctx := context.Background()
	kv := newCountingKV()
	now := time.Now()
	past := now.Add(-time.Hour)

	done := newTenantJob("legacy-done", scheduler.JobStatusCompleted, past)
	done.ID = 51
	putUnindexedTenantJob(t, kv.mockKVStore, done)
	due := newTenantJob("legacy-due", scheduler.JobStatusPending, past)
	due.ID = 52
	putUnindexedTenantJob(t, kv.mockKVStore, due)

	s := NewStorage(kv)
	if err := s.InitializeJobIDCounter(ctx); err != nil {
		t.Fatalf("open: %v", err)
	}
	kv.recordGets.Store(0)
	if ids := dueIDs(t, s, now); len(ids) != 1 || !ids[due.ID] {
		t.Fatalf("due jobs = %v, want only %d", ids, due.ID)
	}
	if got := kv.recordGets.Load(); got != 1 {
		t.Fatalf("job records read = %d, want 1", got)
	}
}

// TestUpdateJob_AFailedRecordWriteLeavesNoSummary proves a summary never
// claims a state its record does not hold: when the record write fails, the
// entry falls back to the bare tenant ID and the job is still found.
func TestUpdateJob_AFailedRecordWriteLeavesNoSummary(t *testing.T) {
	ctx := context.Background()
	kv := newCountingKV()
	s := NewStorage(kv)
	now := time.Now()

	j := newTenantJob("unwritten", scheduler.JobStatusPending, now.Add(-time.Hour))
	if err := s.CreateJob(ctx, j); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	kv.failRecordPut = true
	update := *j
	update.Status = scheduler.JobStatusCompleted
	if err := s.UpdateJob(ctx, &update); err == nil {
		t.Fatal("UpdateJob with a failing record write returned nil")
	}
	kv.failRecordPut = false

	value, err := kv.mockKVStore.Get(ctx, keyForTenantJobIndex(j.ID, "acme"))
	if err != nil {
		t.Fatalf("read index entry: %v", err)
	}
	if string(value) != "acme" {
		t.Fatalf("index value = %q, want the bare tenant ID", value)
	}
	if ids := dueIDs(t, s, now); len(ids) != 1 || !ids[j.ID] {
		t.Fatalf("due jobs = %v, want %d", ids, j.ID)
	}
}

// TestGetJobsDueForExecution_IneligibleJobsDoNotConsumeLimit checks that a
// job the eligible predicate rejects is skipped without using up the limit.
func TestGetJobsDueForExecution_IneligibleJobsDoNotConsumeLimit(t *testing.T) {
	ctx := context.Background()
	storage := NewStorage(newMockKVStore())
	now := time.Now()

	for i := 0; i < 2; i++ {
		j := createTestJob("aaa-pinned", scheduler.OwnerTypeTenant, "aaa")
		j.NextRun = now.Add(-time.Hour)
		j.NodeAffinity = "node-ghost"
		if err := storage.CreateJob(ctx, j); err != nil {
			t.Fatalf("CreateJob(aaa): %v", err)
		}
	}
	z := createTestJob("zzz-job", scheduler.OwnerTypeTenant, "zzz")
	z.NextRun = now.Add(-time.Hour)
	if err := storage.CreateJob(ctx, z); err != nil {
		t.Fatalf("CreateJob(zzz): %v", err)
	}

	due, err := storage.GetJobsDueForExecution(ctx, now, 2, func(j *scheduler.Job) bool { return j.NodeAffinity == "" })
	if err != nil {
		t.Fatalf("GetJobsDueForExecution: %v", err)
	}
	if len(due) != 1 || due[0].ID != z.ID {
		t.Fatalf("expected only the zzz job, got %d jobs", len(due))
	}
}
