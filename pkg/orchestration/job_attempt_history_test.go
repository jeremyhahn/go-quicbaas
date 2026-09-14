// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// A tenant-routing store, the one the daemon actually writes through
// =============================================================================
//
// WHY THIS DOUBLE EXISTS AT ALL. The store beneath the orchestrator in a real
// daemon is pkg/server's tenantRoutingKVStore: it resolves the tenant on the
// OPERATION'S CONTEXT and hands the call to that tenant's backend, so a
// barrier-isolated tenant's records live in its own store and an untenanted
// call lands in the shared one. Every existing KV double in this package
// ignores the context entirely, which is exactly why go-quicbaas#822 —
// writes filed in one store, a listing looking in another — could not be
// seen by any test in the suite. A double that cannot tell two tenants apart
// cannot catch a bug about which tenant a record went to.
//
// It models the routing contract only; the per-tenant backends are the plain
// in-memory maps the rest of the package's tests use.

// routingKVStore routes each operation to the backend of the tenant on its
// context, exactly as the daemon's tenant-routing store does.
type routingKVStore struct {
	mu       sync.Mutex
	backends map[string]map[string][]byte
}

func newRoutingKVStore() *routingKVStore {
	return &routingKVStore{backends: make(map[string]map[string][]byte)}
}

// backend selects the store for the context tenant, creating it on first use.
// The empty tenant is a backend of its own: that is precisely the shared
// store an untenanted write lands in.
func (r *routingKVStore) backend(ctx context.Context) map[string][]byte {
	tenantID := TenantFromContext(ctx)
	store, ok := r.backends[tenantID]
	if !ok {
		store = make(map[string][]byte)
		r.backends[tenantID] = store
	}
	return store
}

func (r *routingKVStore) Put(ctx context.Context, key string, value []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := make([]byte, len(value))
	copy(copied, value)
	r.backend(ctx)[key] = copied
	return nil
}

func (r *routingKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.backend(ctx)[key]
	if !ok {
		return nil, &JobPersistenceError{JobID: key, Message: "key not found"}
	}
	return value, nil
}

func (r *routingKVStore) Delete(ctx context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.backend(ctx), key)
	return nil
}

func (r *routingKVStore) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	r.mu.Lock()
	snapshot := make(map[string][]byte, len(r.backend(ctx)))
	for key, value := range r.backend(ctx) {
		snapshot[key] = value
	}
	r.mu.Unlock()
	for key, value := range snapshot {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if err := fn(key, value); err != nil {
			return err
		}
	}
	return nil
}

func (r *routingKVStore) List(ctx context.Context, prefix string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var keys []string
	for key := range r.backend(ctx) {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (r *routingKVStore) Exists(ctx context.Context, key string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.backend(ctx)[key]
	return ok, nil
}

var _ interfaces.KVStore = (*routingKVStore)(nil)

// deadlineRecordingKVStore records the deadline the scan was handed, so a
// bound the caller never asked for is visible rather than inferred.
type deadlineRecordingKVStore struct {
	routingKVStore
	scanned         bool
	scanHadDeadline bool
	scanDeadline    time.Time
}

func newDeadlineRecordingKVStore() *deadlineRecordingKVStore {
	return &deadlineRecordingKVStore{routingKVStore: *newRoutingKVStore()}
}

func (d *deadlineRecordingKVStore) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	d.scanned = true
	d.scanDeadline, d.scanHadDeadline = ctx.Deadline()
	return d.routingKVStore.Scan(ctx, prefix, fn)
}

var _ interfaces.KVStore = (*deadlineRecordingKVStore)(nil)

// concludedJobService builds a service over a routing store holding the
// records the given concluded jobs left behind, written the way the daemon
// writes them: through persistJobStatus, from the job goroutine.
func concludedJobService(t *testing.T, tenantID string, statuses ...*JobStatus) (*agentServiceImpl, *routingKVStore) {
	t.Helper()
	kv := newRoutingKVStore()
	svc := &agentServiceImpl{kvStore: kv, logger: &controlLoopMockLogger{}}
	for _, status := range statuses {
		svc.persistJobStatus(tenantID, status)
	}
	return svc, kv
}

// concludedJob is a job that ran, ended and left memory.
func concludedJob(jobID, tenantID, taskID string, attempt int, startedAt int64) *JobStatus {
	return &JobStatus{
		JobID:     jobID,
		TenantID:  tenantID,
		TaskID:    taskID,
		Attempt:   attempt,
		State:     JobStateFailed,
		StartedAt: startedAt,
		UpdatedAt: startedAt + 1,
		Config:    &JobConfig{TenantID: tenantID, TaskID: taskID, Attempt: attempt},
	}
}

// =============================================================================
// Defect: a concluded job's record is filed where the listing does not look
// =============================================================================

// TestListJobs_ReadsRecordsTheJobGoroutineWrote is the regression for
// go-quicbaas#822.
//
// MEASURED ON THE BURN-IN NODE before the fix: the daemon had been up for
// seven hours, and GET /orchestrator/jobs?tenant_id=automatethethings
// returned exactly the seven jobs started since boot. Every job older than
// the process was missing — yet GET /orchestrator/jobs/{id} answered for a
// job two days older, from the very same store. The records were durable all
// along; the LISTING was looking in another tenant's backend, because the
// write ran on a bare context and the listing on the request's.
func TestListJobs_ReadsRecordsTheJobGoroutineWrote(t *testing.T) {
	const tenantID = "tenant-attempts"
	first := concludedJob("job-1", tenantID, "task-a", 1, 100)
	second := concludedJob("job-2", tenantID, "task-a", 2, 200)
	svc, _ := concludedJobService(t, tenantID, first, second)

	// The listing arrives on a REQUEST context, which carries the tenant.
	ctx := ContextWithTenant(context.Background(), tenantID)
	jobs, err := svc.ListJobs(ctx, tenantID, nil)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("listed %d concluded jobs, want 2 — the attempt history a terminal task's page renders", len(jobs))
	}
	if jobs[0].JobID != "job-2" || jobs[1].JobID != "job-1" {
		t.Errorf("order = %q,%q; want most recent first (job-2,job-1)", jobs[0].JobID, jobs[1].JobID)
	}
}

// TestListJobs_RecordReadByIDIsAlsoListed pins the two readers together: a
// job GetJobStatus can answer for must appear in the tenant's listing. The
// defect was exactly this pair disagreeing.
func TestListJobs_RecordReadByIDIsAlsoListed(t *testing.T) {
	const tenantID = "tenant-pair"
	svc, _ := concludedJobService(t, tenantID, concludedJob("job-x", tenantID, "task-b", 1, 10))

	byID, err := svc.loadJobStatus(tenantID, "job-x")
	if err != nil {
		t.Fatalf("loadJobStatus: %v", err)
	}
	if byID.JobID != "job-x" {
		t.Fatalf("loadJobStatus returned job %q, want job-x", byID.JobID)
	}

	jobs, err := svc.ListJobs(ContextWithTenant(context.Background(), tenantID), tenantID, nil)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].JobID != "job-x" {
		t.Fatalf("a job readable by id was not listed: got %d jobs", len(jobs))
	}
}

// TestListJobs_RestartScanSeesTheSameRecords proves the third reader agrees:
// the restart scan enumerates every tenant's records with no tenant of its
// own. Moving job records into per-tenant backends would blind it.
func TestListJobs_RestartScanSeesTheSameRecords(t *testing.T) {
	svc, _ := concludedJobService(t, "tenant-one", concludedJob("job-1", "tenant-one", "task-a", 1, 10))
	svc.persistJobStatus("tenant-two", concludedJob("job-2", "tenant-two", "task-b", 1, 20))

	var seen []string
	kv := svc.kvStore
	scanCtx, cancel := jobRecordScope(context.Background())
	defer cancel()
	if err := kv.Scan(scanCtx, "job:", func(key string, _ []byte) error {
		seen = append(seen, key)
		return nil
	}); err != nil {
		t.Fatalf("restart scan: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("restart scan saw %d records (%v), want both tenants' records", len(seen), seen)
	}
}

// TestListJobs_DoesNotObserveAnotherTenant is the isolation half: the record
// scope is untenanted, so the KEY is what confines a listing. A prefix that
// stopped confining it would leak every tenant's jobs to every caller.
func TestListJobs_DoesNotObserveAnotherTenant(t *testing.T) {
	svc, _ := concludedJobService(t, "tenant-one", concludedJob("job-1", "tenant-one", "task-a", 1, 10))
	svc.persistJobStatus("tenant-two", concludedJob("job-2", "tenant-two", "task-b", 1, 20))

	jobs, err := svc.ListJobs(ContextWithTenant(context.Background(), "tenant-one"), "tenant-one", nil)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].JobID != "job-1" {
		t.Fatalf("tenant-one listed %d jobs (%+v), want only its own", len(jobs), jobs)
	}
}

// TestPersistJobStatus_UntenantedRecordIsAddressable pins the key
// normalisation: a caller that reached the orchestrator with no tenant files
// under the system tenant, where a listing can name it, rather than at an
// empty segment no listing asks for.
func TestPersistJobStatus_UntenantedRecordIsAddressable(t *testing.T) {
	svc, kv := concludedJobService(t, "", concludedJob("job-sys", "", "task-a", 1, 10))

	if _, ok := kv.backends[""]["job::job-sys"]; ok {
		t.Fatal("an untenanted record was filed at an empty tenant segment, where no listing can name it")
	}
	jobs, err := svc.ListJobs(ContextWithTenant(context.Background(), SystemTenantID), SystemTenantID, nil)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].JobID != "job-sys" {
		t.Fatalf("system tenant listed %d jobs, want the untenanted record", len(jobs))
	}
}

// =============================================================================
// Defect: the task_id narrowing was accepted and ignored
// =============================================================================

// TestListJobs_FilterByTaskID is the regression for the measured answer: a
// request for task 8bf2fb5f's jobs returned seven jobs, none of them that
// task's.
func TestListJobs_FilterByTaskID(t *testing.T) {
	const tenantID = "tenant-filter"
	svc, _ := concludedJobService(t, tenantID,
		concludedJob("job-a1", tenantID, "task-a", 1, 100),
		concludedJob("job-a2", tenantID, "task-a", 2, 200),
		concludedJob("job-b1", tenantID, "task-b", 1, 300),
	)
	ctx := ContextWithTenant(context.Background(), tenantID)

	jobs, err := svc.ListJobs(ctx, tenantID, &JobFilter{TaskID: "task-a"})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("task-a listed %d jobs, want its 2 attempts", len(jobs))
	}
	for _, job := range jobs {
		if job.TaskID != "task-a" {
			t.Errorf("job %s belongs to task %s, not the task asked for", job.JobID, job.TaskID)
		}
	}
}

// TestListJobs_FilterMatchingNothingIsEmpty separates the two facts a task
// page must tell apart: a task with no attempts answers empty, and it must
// not answer with the tenant's other jobs.
func TestListJobs_FilterMatchingNothingIsEmpty(t *testing.T) {
	const tenantID = "tenant-filter-empty"
	svc, _ := concludedJobService(t, tenantID, concludedJob("job-a1", tenantID, "task-a", 1, 100))

	jobs, err := svc.ListJobs(ContextWithTenant(context.Background(), tenantID), tenantID,
		&JobFilter{TaskID: "task-never-dispatched"})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("a task with no attempts listed %d jobs (%+v), want none", len(jobs), jobs)
	}
}

// TestListJobs_NilAndEmptyFilterListEverything pins the other direction: "no
// narrowing asked for" must never read as "narrow to nothing".
func TestListJobs_NilAndEmptyFilterListEverything(t *testing.T) {
	const tenantID = "tenant-nofilter"
	svc, _ := concludedJobService(t, tenantID,
		concludedJob("job-a1", tenantID, "task-a", 1, 100),
		concludedJob("job-b1", tenantID, "task-b", 1, 200),
	)
	ctx := ContextWithTenant(context.Background(), tenantID)

	for name, filter := range map[string]*JobFilter{"nil": nil, "empty": {}} {
		jobs, err := svc.ListJobs(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("%s filter: ListJobs: %v", name, err)
		}
		if len(jobs) != 2 {
			t.Errorf("%s filter listed %d jobs, want all 2", name, len(jobs))
		}
	}
}

// TestListJobs_FilterReadsTheBindingOffTheConfig covers the older record
// shape: a status persisted before the binding was promoted onto JobStatus
// carries its task only in the config, and dropping it would make that
// attempt vanish from its task's history.
func TestListJobs_FilterReadsTheBindingOffTheConfig(t *testing.T) {
	const tenantID = "tenant-legacy"
	legacy := concludedJob("job-legacy", tenantID, "", 1, 100)
	legacy.Config = &JobConfig{TenantID: tenantID, TaskID: "task-a", Attempt: 1}
	svc, _ := concludedJobService(t, tenantID, legacy)

	jobs, err := svc.ListJobs(ContextWithTenant(context.Background(), tenantID), tenantID,
		&JobFilter{TaskID: "task-a"})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].JobID != "job-legacy" {
		t.Fatalf("a record binding its task only through its config was dropped: got %d jobs", len(jobs))
	}
}

// TestListJobs_TenantRequired is the bad-input half: a listing with no tenant
// is refused rather than answered from an unbounded scan.
func TestListJobs_TenantRequired(t *testing.T) {
	svc, _ := concludedJobService(t, "tenant-x")

	if _, err := svc.ListJobs(context.Background(), "", &JobFilter{TaskID: "task-a"}); err == nil {
		t.Fatal("ListJobs with no tenant returned no error")
	}
}

// =============================================================================
// JobFilter
// =============================================================================

func TestJobFilter_Matches(t *testing.T) {
	withStatusBinding := &JobStatus{JobID: "j", TaskID: "task-a"}
	withConfigBinding := &JobStatus{JobID: "j", Config: &JobConfig{TaskID: "task-a"}}
	unbound := &JobStatus{JobID: "j"}

	tests := []struct {
		name   string
		filter *JobFilter
		status *JobStatus
		want   bool
	}{
		{"nil filter matches", nil, withStatusBinding, true},
		{"empty filter matches", &JobFilter{}, withStatusBinding, true},
		{"task matches on status", &JobFilter{TaskID: "task-a"}, withStatusBinding, true},
		{"task matches on config", &JobFilter{TaskID: "task-a"}, withConfigBinding, true},
		{"other task does not match", &JobFilter{TaskID: "task-b"}, withStatusBinding, false},
		{"unbound job does not match a task", &JobFilter{TaskID: "task-a"}, unbound, false},
		{"nil status never matches", &JobFilter{TaskID: "task-a"}, nil, false},
		{"nil status does not match a nil filter either", nil, nil, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.filter.Matches(test.status); got != test.want {
				t.Errorf("Matches() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestJobFilter_Empty(t *testing.T) {
	tests := []struct {
		name   string
		filter *JobFilter
		want   bool
	}{
		{"nil", nil, true},
		{"no fields set", &JobFilter{}, true},
		{"task set", &JobFilter{TaskID: "task-a"}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.filter.Empty(); got != test.want {
				t.Errorf("Empty() = %v, want %v", got, test.want)
			}
		})
	}
}

// =============================================================================
// Keys and the record scope
// =============================================================================

func TestJobPersistencePrefix_BoundsOneTenant(t *testing.T) {
	prefix := jobPersistencePrefix("tenant-a")
	if !strings.HasPrefix(jobPersistenceKey("tenant-a", "job-1"), prefix) {
		t.Error("a tenant's own record is not under its prefix")
	}
	if strings.HasPrefix(jobPersistenceKey("tenant-ab", "job-1"), prefix) {
		t.Error("a neighbouring tenant whose id extends this one is inside the prefix")
	}
	if got := jobPersistencePrefix(""); got != "job:system:" {
		t.Errorf("empty tenant prefix = %q, want the system tenant's", got)
	}
}

func TestJobRecordScope_KeepsTheCallerDeadline(t *testing.T) {
	deadline := time.Now().Add(90 * time.Second)
	caller, cancelCaller := context.WithDeadline(context.Background(), deadline)
	defer cancelCaller()

	scope, cancel := jobRecordScope(caller)
	defer cancel()

	got, ok := scope.Deadline()
	if !ok {
		t.Fatal("the record scope dropped the caller's deadline")
	}
	if !got.Equal(deadline) {
		t.Errorf("deadline = %v, want the caller's %v", got, deadline)
	}
}

// TestJobRecordScope_ForwardsTheCallerCancellation pins the other half of the
// deadline: a listing must stop when the request it serves is abandoned,
// even though it is not routing by that request's tenant.
func TestJobRecordScope_ForwardsTheCallerCancellation(t *testing.T) {
	caller, cancelCaller := context.WithCancel(context.Background())
	scope, cancel := jobRecordScope(caller)
	defer cancel()

	select {
	case <-scope.Done():
		t.Fatal("the record scope was already cancelled")
	default:
	}

	cancelCaller()
	<-scope.Done()
	if !errors.Is(scope.Err(), context.Canceled) {
		t.Errorf("scope error = %v, want context.Canceled", scope.Err())
	}
}

func TestJobRecordScope_DropsTheCallerTenant(t *testing.T) {
	caller := ContextWithTenant(context.Background(), "tenant-a")
	scope, cancel := jobRecordScope(caller)
	defer cancel()

	if tenantID := TenantFromContext(scope); tenantID != "" {
		t.Errorf("the record scope carried tenant %q; it must route nowhere so every reader sees one store", tenantID)
	}
}

// TestJobRecordScope_AddsNoDeadlineOfItsOwn is the regression for the first
// cut of go-quicbaas#822, which recreated the very symptom the issue is about
// through a different door.
//
// The scans — the tenant listing and the restart scan — have always been
// bounded by their caller and nothing else. Giving the scope a fallback
// timeout put a hard ceiling on both: a deadline-less REST listing would
// inherit it as the only deadline in the chain, and a tenant with thousands
// of concluded records would have its attempt history silently truncated,
// because the scan error is logged and not returned.
func TestJobRecordScope_AddsNoDeadlineOfItsOwn(t *testing.T) {
	scope, cancel := jobRecordScope(context.Background())
	defer cancel()

	if deadline, ok := scope.Deadline(); ok {
		t.Fatalf("the record scope imposed a deadline of its own (%v); a scan must be bounded only by its caller", deadline)
	}
}

// TestJobRecordOp_IsBounded is the other half: ONE record's read or write
// keeps the five-second bound it has always had, because a single Get or Put
// that has not answered by then is a stuck store, not a large one.
func TestJobRecordOp_IsBounded(t *testing.T) {
	scope, cancel := jobRecordOp()
	defer cancel()

	deadline, ok := scope.Deadline()
	if !ok {
		t.Fatal("a single-record operation ran unbounded")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > jobRecordTimeout {
		t.Errorf("remaining budget = %v, want (0, %v]", remaining, jobRecordTimeout)
	}
	if tenantID := TenantFromContext(scope); tenantID != "" {
		t.Errorf("a single-record operation carried tenant %q; it must route nowhere", tenantID)
	}
}

// TestListJobs_ScanIsNotCappedByTheRecordScope pins the bound where the
// merge scan actually runs. A deadline the caller did not ask for would
// truncate a large tenant's attempt history and report nothing, so this
// asserts the context the STORE is handed, not merely the helper's return.
func TestListJobs_ScanIsNotCappedByTheRecordScope(t *testing.T) {
	const tenantID = "tenant-unbounded"
	kv := newDeadlineRecordingKVStore()
	svc := &agentServiceImpl{kvStore: kv, logger: &controlLoopMockLogger{}}

	// A caller with no deadline of its own — an unauthenticated REST listing
	// is exactly this.
	if _, err := svc.ListJobs(ContextWithTenant(context.Background(), tenantID), tenantID, nil); err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if !kv.scanned {
		t.Fatal("ListJobs never reached the store")
	}
	if kv.scanHadDeadline {
		t.Errorf("the merge scan ran under a deadline (%v) the caller never set", kv.scanDeadline)
	}
}

// TestListJobs_ScanKeepsTheCallerDeadline is the same assertion from the
// other side: a caller that DOES bound the read still bounds it.
func TestListJobs_ScanKeepsTheCallerDeadline(t *testing.T) {
	const tenantID = "tenant-bounded"
	kv := newDeadlineRecordingKVStore()
	svc := &agentServiceImpl{kvStore: kv, logger: &controlLoopMockLogger{}}

	want := time.Now().Add(90 * time.Second)
	ctx, cancel := context.WithDeadline(ContextWithTenant(context.Background(), tenantID), want)
	defer cancel()

	if _, err := svc.ListJobs(ctx, tenantID, nil); err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if !kv.scanHadDeadline {
		t.Fatal("the merge scan dropped the caller's deadline")
	}
	if !kv.scanDeadline.Equal(want) {
		t.Errorf("scan deadline = %v, want the caller's %v", kv.scanDeadline, want)
	}
}

// TestListJobs_RunningJobSuppressesItsStaleRecord pins the merge: a job still
// in the registry is reported from memory, once, even though a record from an
// earlier state is also on the store.
func TestListJobs_RunningJobSuppressesItsStaleRecord(t *testing.T) {
	const tenantID = "tenant-merge"
	stale := concludedJob("job-live", tenantID, "task-a", 1, 100)
	stale.State = JobStateInitializing
	svc, _ := concludedJobService(t, tenantID, stale)

	live := newJobRun(JobRunConfig{
		JobID:  "job-live",
		Runner: JobRunnerWorkflow,
		Config: &JobConfig{TenantID: tenantID, TaskID: "task-a", Attempt: 1},
		Logger: &controlLoopMockLogger{},
	})
	live.snapshot(JobStateExecuting)
	svc.jobs.Store(tenantID+"/job-live", &runningJob{run: live, cancel: func() {}})

	jobs, err := svc.ListJobs(ContextWithTenant(context.Background(), tenantID), tenantID, nil)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("listed %d entries for one job, want 1 — the record must not duplicate the live run", len(jobs))
	}
	if jobs[0].State != JobStateExecuting {
		t.Errorf("state = %q, want the live run's %q", jobs[0].State, JobStateExecuting)
	}
}

// TestListJobs_FilteredOutRunningJobStillSuppressesItsRecord is the corner the
// merge has to get right: a running job the filter excludes must not let its
// own stale record through under a different task.
func TestListJobs_FilteredOutRunningJobStillSuppressesItsRecord(t *testing.T) {
	const tenantID = "tenant-merge-filter"
	stale := concludedJob("job-live", tenantID, "task-a", 1, 100)
	svc, _ := concludedJobService(t, tenantID, stale)

	live := newJobRun(JobRunConfig{
		JobID:  "job-live",
		Runner: JobRunnerWorkflow,
		Config: &JobConfig{TenantID: tenantID, TaskID: "task-a", Attempt: 1},
		Logger: &controlLoopMockLogger{},
	})
	live.snapshot(JobStateExecuting)
	svc.jobs.Store(tenantID+"/job-live", &runningJob{run: live, cancel: func() {}})

	jobs, err := svc.ListJobs(ContextWithTenant(context.Background(), tenantID), tenantID,
		&JobFilter{TaskID: "task-b"})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("a running job excluded by the filter leaked its record: got %+v", jobs)
	}
}

// TestListJobs_CorruptRecordIsSkipped is the bad-input half of the scan: one
// unreadable record must not cost the caller the rest of the history.
func TestListJobs_CorruptRecordIsSkipped(t *testing.T) {
	const tenantID = "tenant-corrupt"
	svc, kv := concludedJobService(t, tenantID, concludedJob("job-good", tenantID, "task-a", 1, 100))

	scope, cancel := jobRecordScope(context.Background())
	defer cancel()
	if err := kv.Put(scope, jobPersistenceKey(tenantID, "job-bad"), []byte("{not json")); err != nil {
		t.Fatalf("seeding the corrupt record: %v", err)
	}

	jobs, err := svc.ListJobs(ContextWithTenant(context.Background(), tenantID), tenantID, nil)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].JobID != "job-good" {
		t.Fatalf("listed %+v, want the readable record only", jobs)
	}
}

// TestPersistJobStatus_RecordSurvivesTheProcess is the durability claim in one
// assertion: a service built fresh over the same store — a restarted daemon,
// with an empty registry — still lists the attempt.
func TestPersistJobStatus_RecordSurvivesTheProcess(t *testing.T) {
	const tenantID = "tenant-restart"
	_, kv := concludedJobService(t, tenantID, concludedJob("job-1", tenantID, "task-a", 1, 100))

	restarted := &agentServiceImpl{kvStore: kv, logger: &controlLoopMockLogger{}}
	jobs, err := restarted.ListJobs(ContextWithTenant(context.Background(), tenantID), tenantID,
		&JobFilter{TaskID: "task-a"})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].JobID != "job-1" {
		t.Fatalf("after a restart the task's attempt history held %d jobs, want 1", len(jobs))
	}

	// And the record is the one that was written, not a placeholder.
	raw, err := kv.Get(context.Background(), jobPersistenceKey(tenantID, "job-1"))
	if err != nil {
		t.Fatalf("reading the record back: %v", err)
	}
	var persisted JobStatus
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("unmarshalling the record: %v", err)
	}
	if persisted.Attempt != 1 || persisted.State != JobStateFailed {
		t.Errorf("record = attempt %d state %q, want attempt 1 state %q",
			persisted.Attempt, persisted.State, JobStateFailed)
	}
}
