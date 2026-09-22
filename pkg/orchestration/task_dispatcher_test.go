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

	"github.com/jeremyhahn/go-quicbaas/pkg/cas"
)

// =============================================================================
// Fakes
// =============================================================================

// fakeDispatchStore is a task store over a map, so a test can state a task's
// creation instant and version exactly instead of racing the clock. It
// implements the same compare-and-swap on Version the real store does.
type fakeDispatchStore struct {
	mu    sync.Mutex
	tasks map[string]*Task

	// listErr, when set, is what ListTasks answers with.
	listErr error

	// updateErr, when set, is what UpdateTask answers with.
	updateErr error

	// beforeUpdate runs inside UpdateTask before the version check, so a
	// test can simulate another writer winning the race.
	beforeUpdate func(store *fakeDispatchStore, task *Task)

	// updates counts every UpdateTask call, conflicting ones included.
	updates int
}

func newFakeDispatchStore(tasks ...*Task) *fakeDispatchStore {
	store := &fakeDispatchStore{tasks: make(map[string]*Task, len(tasks))}
	for _, task := range tasks {
		if task.Version == 0 {
			task.Version = 1
		}
		if task.Status == "" {
			task.Status = TaskStatusPending
		}
		store.tasks[task.ID] = task
	}
	return store
}

func (s *fakeDispatchStore) ListTasks(_ context.Context, tenantID string, filter *TaskFilter) ([]*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listErr != nil {
		return nil, s.listErr
	}
	out := make([]*Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		// The store's OWN matcher, so a filter the sweep relies on cannot be
		// honoured here and ignored in production — or the other way round,
		// which is how a fake starts answering a question the real store
		// does not.
		if task.TenantID != tenantID || !taskMatchesFilter(task, filter) {
			continue
		}
		clone := *task
		out = append(out, &clone)
	}
	return out, nil
}

func (s *fakeDispatchStore) GetTask(_ context.Context, tenantID, taskID string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok || task.TenantID != tenantID {
		return nil, &TaskNotFoundError{TaskID: taskID}
	}
	clone := *task
	return &clone, nil
}

func (s *fakeDispatchStore) UpdateTask(_ context.Context, task *Task) error {
	if s.beforeUpdate != nil {
		s.beforeUpdate(s, task)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates++
	if s.updateErr != nil {
		return s.updateErr
	}
	held, ok := s.tasks[task.ID]
	if !ok {
		return &TaskNotFoundError{TaskID: task.ID}
	}
	if held.Version != task.Version {
		return &TaskVersionConflictError{
			TaskID: task.ID, ExpectedVersion: task.Version, ActualVersion: held.Version,
		}
	}
	clone := *task
	clone.Version = held.Version + 1
	s.tasks[task.ID] = &clone
	return nil
}

// reason reads a task's recorded dispatch reason.
func (s *fakeDispatchStore) reason(taskID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return ""
	}
	return task.DispatchReason
}

// bump writes a task's dispatch reason behind the dispatcher's back, taking
// the version with it.
func (s *fakeDispatchStore) bump(taskID, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if task, ok := s.tasks[taskID]; ok {
		task.DispatchReason = reason
		task.Version++
	}
}

// fakeTaskSpawner records what it was asked to start and answers with a job
// ID, or with a fixed error.
type fakeTaskSpawner struct {
	mu      sync.Mutex
	spawned []string
	err     error

	// onSpawn runs before the answer, so a test can take the lease the
	// dispatched job would have taken.
	onSpawn func(task *Task)
}

func (f *fakeTaskSpawner) SpawnTask(_ context.Context, task *Task) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	if f.onSpawn != nil {
		f.onSpawn(task)
	}
	f.spawned = append(f.spawned, task.ID)
	return "job-" + task.ID, nil
}

func (f *fakeTaskSpawner) dispatched() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.spawned...)
}

// dispatcherFixture is one dispatcher over one tenant with one autonomous
// project and one the operator left alone.
type dispatcherFixture struct {
	projects   ProjectStore
	tasks      *fakeDispatchStore
	leases     JobLeaseStore
	spawner    *fakeTaskSpawner
	logger     *controlLoopMockLogger
	dispatcher *TaskDispatcher
}

const (
	dispatchTenant     = "tenant-d"
	dispatchProject    = "proj-auto"
	dispatchManualProj = "proj-manual"
)

func newDispatcherFixture(t *testing.T, limit int, tasks ...*Task) *dispatcherFixture {
	t.Helper()
	projects := NewInMemoryProjectStore()
	ctx := context.Background()
	for _, p := range []*Project{
		{
			ID: dispatchProject, TenantID: dispatchTenant, Name: "autonomous", Autonomous: true,
			Quotas: ProjectQuotas{MaxConcurrency: limit},
		},
		{ID: dispatchManualProj, TenantID: dispatchTenant, Name: "operator-driven"},
	} {
		if err := projects.Create(ctx, p); err != nil {
			t.Fatalf("create project %s: %v", p.ID, err)
		}
	}
	store := newFakeDispatchStore(tasks...)
	leases := newTestJobLeaseStore(t)
	spawner := &fakeTaskSpawner{}
	logger := &controlLoopMockLogger{}
	dispatcher, err := NewTaskDispatcher(TaskDispatcherConfig{
		Tenants:  &fakeTenantLister{ids: []string{dispatchTenant}},
		Projects: projects,
		Tasks:    store,
		Leases:   leases,
		LeaseTTL: testPollerLeaseTTL,
		Spawner:  spawner,
		Logger:   logger,
	})
	if err != nil {
		t.Fatalf("NewTaskDispatcher: %v", err)
	}
	return &dispatcherFixture{
		projects: projects, tasks: store, leases: leases,
		spawner: spawner, logger: logger, dispatcher: dispatcher,
	}
}

// hold takes a live lease under the autonomous project, as a running job
// would.
func (f *dispatcherFixture) hold(t *testing.T, jobID string) {
	t.Helper()
	lease := &JobLease{
		TenantID: dispatchTenant, ProjectID: dispatchProject,
		Workflow: "any", Ref: jobID, JobID: jobID, Generation: 1,
	}
	if err := f.leases.Acquire(context.Background(), lease); err != nil {
		t.Fatalf("acquire lease %s: %v", jobID, err)
	}
}

// pendingTask builds a dispatchable task under the autonomous project.
func pendingTask(id string, priority int, createdAt int64) *Task {
	return &Task{
		ID: id, TenantID: dispatchTenant, ProjectID: dispatchProject,
		Subject: id, Status: TaskStatusPending, AutoSpawn: true,
		Priority: priority, CreatedAt: createdAt, Version: 1,
	}
}

// =============================================================================
// Construction
// =============================================================================

func TestNewTaskDispatcher_RequiresEveryCollaborator(t *testing.T) {
	t.Parallel()
	full := TaskDispatcherConfig{
		Tenants:  &fakeTenantLister{},
		Projects: NewInMemoryProjectStore(),
		Tasks:    newFakeDispatchStore(),
		Leases:   newTestJobLeaseStore(t),
		LeaseTTL: testPollerLeaseTTL,
		Spawner:  &fakeTaskSpawner{},
		Logger:   &controlLoopMockLogger{},
	}
	if _, err := NewTaskDispatcher(full); err != nil {
		t.Fatalf("complete config refused: %v", err)
	}
	for field, mutate := range map[string]func(*TaskDispatcherConfig){
		"tenants":   func(c *TaskDispatcherConfig) { c.Tenants = nil },
		"projects":  func(c *TaskDispatcherConfig) { c.Projects = nil },
		"tasks":     func(c *TaskDispatcherConfig) { c.Tasks = nil },
		"leases":    func(c *TaskDispatcherConfig) { c.Leases = nil },
		"lease_ttl": func(c *TaskDispatcherConfig) { c.LeaseTTL = 0 },
		"spawner":   func(c *TaskDispatcherConfig) { c.Spawner = nil },
		"logger":    func(c *TaskDispatcherConfig) { c.Logger = nil },
	} {
		cfg := full
		mutate(&cfg)
		_, err := NewTaskDispatcher(cfg)
		var cfgErr *TaskDispatcherConfigError
		if !errors.As(err, &cfgErr) || cfgErr.Field != field || !errors.Is(err, ErrTaskDispatcherConfig) {
			t.Errorf("missing %s: got %v, want TaskDispatcherConfigError", field, err)
		}
	}
}

// =============================================================================
// Capacity
// =============================================================================

// TestTaskDispatcher_HonoursMaxConcurrencyLessInflight: a project allowed two
// concurrent jobs with one lease already live dispatches exactly one more,
// and says why the rest are waiting.
func TestTaskDispatcher_HonoursMaxConcurrencyLessInflight(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t, 2,
		pendingTask("t-1", 0, 100),
		pendingTask("t-2", 0, 200),
		pendingTask("t-3", 0, 300),
	)
	f.hold(t, "job-running")

	if err := f.dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := f.spawner.dispatched(); len(got) != 1 || got[0] != "t-1" {
		t.Fatalf("one slot means one dispatch, got %v", got)
	}
	for _, id := range []string{"t-2", "t-3"} {
		reason := f.tasks.reason(id)
		if !strings.Contains(reason, "project at capacity") || !strings.Contains(reason, "1 of 2") {
			t.Fatalf("%s must say the project is full: %q", id, reason)
		}
	}
	if f.tasks.reason("t-1") != "" {
		t.Fatalf("the dispatched task must carry no refusal: %q", f.tasks.reason("t-1"))
	}
}

// TestTaskDispatcher_UnlimitedConcurrencyDispatchesEverything: a project with
// no MaxConcurrency is bounded by the lease store alone.
func TestTaskDispatcher_UnlimitedConcurrencyDispatchesEverything(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t, 0,
		pendingTask("t-1", 0, 100),
		pendingTask("t-2", 0, 200),
	)
	f.hold(t, "job-running")
	if err := f.dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := f.spawner.dispatched(); len(got) != 2 {
		t.Fatalf("an unbounded project dispatches everything ready, got %v", got)
	}
}

// TestTaskDispatcher_FullProjectDispatchesNothing: every slot held means no
// dispatch at all, and every waiting task says so.
func TestTaskDispatcher_FullProjectDispatchesNothing(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t, 1, pendingTask("t-1", 0, 100))
	f.hold(t, "job-running")
	if err := f.dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := f.spawner.dispatched(); len(got) != 0 {
		t.Fatalf("a full project dispatched %v", got)
	}
	if !strings.Contains(f.tasks.reason("t-1"), "project at capacity") {
		t.Fatalf("reason = %q", f.tasks.reason("t-1"))
	}
}

// =============================================================================
// Ordering
// =============================================================================

// TestTaskDispatcher_OrdersByPriorityThenAge: the highest priority goes
// first, and within one priority the oldest task does.
func TestTaskDispatcher_OrdersByPriorityThenAge(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t, 3,
		pendingTask("young-low", 1, 900),
		pendingTask("old-high", 5, 300),
		pendingTask("young-high", 5, 800),
	)
	if err := f.dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	got := f.spawner.dispatched()
	want := []string{"old-high", "young-high", "young-low"}
	if len(got) != len(want) {
		t.Fatalf("dispatched %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dispatch order %v, want %v", got, want)
		}
	}
}

// TestSortDispatchCandidates_TieBreaksOnID: two tasks of one priority created
// in the same instant still have exactly one order.
func TestSortDispatchCandidates_TieBreaksOnID(t *testing.T) {
	t.Parallel()
	tasks := []*Task{pendingTask("b", 1, 100), pendingTask("a", 1, 100)}
	sortDispatchCandidates(tasks)
	if tasks[0].ID != "a" || tasks[1].ID != "b" {
		t.Fatalf("tie must break on ID: %s %s", tasks[0].ID, tasks[1].ID)
	}
}

// =============================================================================
// What the dispatcher will not start
// =============================================================================

// TestTaskDispatcher_HoldsDeferredAndExhaustedTasks: a task inside its retry
// hold and a task out of budget are both held, each with its own reason, and
// neither consumes a slot.
func TestTaskDispatcher_HoldsDeferredAndExhaustedTasks(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(time.Hour).UnixNano()
	deferred := pendingTask("t-deferred", 9, 100)
	deferred.RetryNotBefore = future
	exhausted := pendingTask("t-exhausted", 9, 100)
	exhausted.AttemptBudget = 2
	exhausted.Attempts = 2
	f := newDispatcherFixture(t, 5, deferred, exhausted, pendingTask("t-ready", 0, 500))

	if err := f.dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := f.spawner.dispatched(); len(got) != 1 || got[0] != "t-ready" {
		t.Fatalf("only the ready task may run, got %v", got)
	}
	if reason := f.tasks.reason("t-deferred"); !strings.Contains(reason, "deferred until") {
		t.Fatalf("deferred reason = %q", reason)
	}
	if reason := f.tasks.reason("t-exhausted"); !strings.Contains(reason, "attempt budget exhausted") ||
		!strings.Contains(reason, "2 of 2") {
		t.Fatalf("exhausted reason = %q", reason)
	}
}

// TestPartitionDispatchCandidates_IgnoresTasksThatAreNotTheDispatchers: a
// task that never asked to be spawned, and one already spawned, are neither
// dispatched nor refused — the dispatcher has no opinion about them.
func TestPartitionDispatchCandidates_IgnoresTasksThatAreNotTheDispatchers(t *testing.T) {
	t.Parallel()
	manual := pendingTask("manual", 0, 100)
	manual.AutoSpawn = false
	running := pendingTask("running", 0, 100)
	running.SpawnedJobID = "job-1"
	ready, held, bound := partitionDispatchCandidates([]*Task{manual, running, nil, pendingTask("ready", 0, 100)}, 0)
	if len(ready) != 1 || ready[0].ID != "ready" {
		t.Fatalf("ready = %v", ready)
	}
	if len(held) != 0 {
		t.Fatalf("held = %v", held)
	}
	// A task naming a job is returned for the sweep to check against the lease
	// store, not dropped: a binding whose job is gone strands the task for ever.
	if len(bound) != 1 || bound[0].ID != "running" {
		t.Fatalf("bound = %v, want the task naming job-1", bound)
	}
	// A manual task is genuinely none of the dispatcher's business.
	for _, b := range bound {
		if b.ID == "manual" {
			t.Error("a task that does not ask to be spawned must not be examined for repair")
		}
	}
}

// TestPartitionDispatchCandidates_OnlyTheQueueIsDispatchable: since the sweep
// reads the project's running tasks as well as its queued ones
// (dispatchSweepStatuses), a task that is live and names NO job — one some
// other claimant holds — must fall out of the partition entirely. Handing it to
// `ready` would start a second attempt at work already under way
// (go-quicbaas#966).
func TestPartitionDispatchCandidates_OnlyTheQueueIsDispatchable(t *testing.T) {
	t.Parallel()
	for _, status := range []TaskStatus{TaskStatusInProgress, TaskStatusClaimed, TaskStatusBlocked} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()
			live := pendingTask("live", 0, 100)
			live.Status = status
			ready, held, bound := partitionDispatchCandidates([]*Task{live, pendingTask("queued", 0, 100)}, 0)
			if len(ready) != 1 || ready[0].ID != "queued" {
				t.Fatalf("ready = %v, want only the queued task", ready)
			}
			if len(held) != 0 || len(bound) != 0 {
				t.Fatalf("a task with another claimant is neither held nor repaired: held=%v bound=%v", held, bound)
			}
		})
	}
}

// TestTaskDispatcher_LeavesNonAutonomousProjectsAlone: a project the operator
// has not turned autonomous is never read and never dispatched for.
func TestTaskDispatcher_LeavesNonAutonomousProjectsAlone(t *testing.T) {
	t.Parallel()
	manual := &Task{
		ID: "t-manual", TenantID: dispatchTenant, ProjectID: dispatchManualProj,
		Subject: "manual", Status: TaskStatusPending, AutoSpawn: true, Version: 1,
	}
	f := newDispatcherFixture(t, 0, manual)
	if err := f.dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := f.spawner.dispatched(); len(got) != 0 {
		t.Fatalf("a project that is not autonomous dispatched %v", got)
	}
	if reason := f.tasks.reason("t-manual"); reason != "" {
		t.Fatalf("an untouched project's task carries no reason: %q", reason)
	}
	if err := f.dispatcher.SweepProject(context.Background(), dispatchTenant, dispatchManualProj); err != nil {
		t.Fatalf("SweepProject on a non-autonomous project: %v", err)
	}
	if got := f.spawner.dispatched(); len(got) != 0 {
		t.Fatalf("a nudge dispatched for a non-autonomous project: %v", got)
	}
}

// =============================================================================
// The reason on the record
// =============================================================================

// TestTaskDispatcher_WritesTheReasonOnceAndClearsIt: an unchanged reason is
// neither rewritten nor re-logged, and a task that finally runs loses it.
func TestTaskDispatcher_WritesTheReasonOnceAndClearsIt(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t, 1, pendingTask("t-waiting", 0, 100))
	f.hold(t, "job-running")
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := f.dispatcher.Sweep(ctx); err != nil {
			t.Fatalf("sweep %d: %v", i, err)
		}
	}
	if f.tasks.updates != 1 {
		t.Fatalf("an unchanged reason must not be rewritten: %d updates", f.tasks.updates)
	}
	if n := countLines(f.logger.warnLines(), "not dispatched"); n != 1 {
		t.Fatalf("a refusal is warned once per change, got %d: %v", n, f.logger.warnLines())
	}

	// The running job ends; the next sweep dispatches and the reason goes.
	if err := f.leases.Release(ctx, &JobLease{
		TenantID: dispatchTenant, ProjectID: dispatchProject,
		Workflow: "any", Ref: "job-running", JobID: "job-running", Generation: 1,
	}); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := f.dispatcher.Sweep(ctx); err != nil {
		t.Fatalf("sweep after release: %v", err)
	}
	if got := f.spawner.dispatched(); len(got) != 1 {
		t.Fatalf("a freed slot must dispatch, got %v", got)
	}
	if reason := f.tasks.reason("t-waiting"); reason != "" {
		t.Fatalf("a dispatched task must lose its refusal: %q", reason)
	}
}

// TestTaskDispatcher_SpawnRefusalLandsOnTheTask: a spawner that will not
// start the job says so on the record.
func TestTaskDispatcher_SpawnRefusalLandsOnTheTask(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t, 0, pendingTask("t-1", 0, 100))
	f.spawner.err = &NoMatchingAgentError{Constraint: "agent_selection", Detail: "no online agents"}
	if err := f.dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	reason := f.tasks.reason("t-1")
	if !strings.Contains(reason, "spawn refused") || !strings.Contains(reason, "no online agents") {
		t.Fatalf("reason = %q", reason)
	}
}

// TestTaskDispatcher_ReasonWriteRetriesOnceOnConflict: another writer taking
// the version does not lose the reason.
func TestTaskDispatcher_ReasonWriteRetriesOnceOnConflict(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t, 1, pendingTask("t-1", 0, 100))
	f.hold(t, "job-running")
	raced := false
	f.tasks.beforeUpdate = func(store *fakeDispatchStore, _ *Task) {
		if raced {
			return
		}
		raced = true
		store.bump("t-1", "someone else got here first")
	}
	if err := f.dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if reason := f.tasks.reason("t-1"); !strings.Contains(reason, "project at capacity") {
		t.Fatalf("the retry must land the reason: %q", reason)
	}
	if f.tasks.updates != 2 {
		t.Fatalf("one conflict means exactly one retry: %d updates", f.tasks.updates)
	}
}

// TestTaskDispatcher_ReasonWriteFailureIsReportedNotFatal: a store that
// refuses the write leaves the sweep running and says so.
func TestTaskDispatcher_ReasonWriteFailureIsReportedNotFatal(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t, 1, pendingTask("t-1", 0, 100))
	f.hold(t, "job-running")
	f.tasks.updateErr = errors.New("kv down")
	if err := f.dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("a reason that cannot be written must not fail the sweep: %v", err)
	}
	if n := countLines(f.logger.warnLines(), "recording the dispatch reason failed"); n != 1 {
		t.Fatalf("the write failure must be reported: %v", f.logger.warnLines())
	}
}

// =============================================================================
// Sweep, SweepProject, Run
// =============================================================================

// TestTaskDispatcher_SweepProject dispatches for one project now, refuses an
// unknown one typed, and refuses a sweep that names neither.
func TestTaskDispatcher_SweepProject(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t, 0, pendingTask("t-1", 0, 100))
	ctx := context.Background()
	if err := f.dispatcher.SweepProject(ctx, dispatchTenant, dispatchProject); err != nil {
		t.Fatalf("SweepProject: %v", err)
	}
	if got := f.spawner.dispatched(); len(got) != 1 {
		t.Fatalf("a nudge must dispatch, got %v", got)
	}
	err := f.dispatcher.SweepProject(ctx, dispatchTenant, "proj-missing")
	var missing *TaskDispatchProjectNotFoundError
	if !errors.As(err, &missing) || !errors.Is(err, ErrTaskDispatchProjectNotFound) || missing.ProjectID != "proj-missing" {
		t.Fatalf("unknown project: %v", err)
	}
	if err := f.dispatcher.SweepProject(ctx, "", dispatchProject); !errors.Is(err, ErrTaskDispatcherConfig) {
		t.Fatalf("empty tenant: %v", err)
	}
	if err := f.dispatcher.SweepProject(ctx, dispatchTenant, ""); !errors.Is(err, ErrTaskDispatcherConfig) {
		t.Fatalf("empty project: %v", err)
	}
}

// TestTaskDispatcher_SweepErrors: tenants that cannot be enumerated fail the
// sweep typed; projects and tasks that cannot be read are reported and the
// sweep goes on.
func TestTaskDispatcher_SweepErrors(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t, 0, pendingTask("t-1", 0, 100))
	ctx := context.Background()

	f.tasks.listErr = errors.New("kv down")
	if err := f.dispatcher.Sweep(ctx); err != nil {
		t.Fatalf("a project that cannot be read must not fail the sweep: %v", err)
	}
	if n := countLines(f.logger.warnLines(), "listing tasks failed"); n != 1 {
		t.Fatalf("the listing failure must be reported: %v", f.logger.warnLines())
	}
	f.tasks.listErr = nil

	broken, err := NewTaskDispatcher(TaskDispatcherConfig{
		Tenants:  &fakeTenantLister{err: errors.New("iam down")},
		Projects: NewInMemoryProjectStore(),
		Tasks:    newFakeDispatchStore(),
		Leases:   newTestJobLeaseStore(t),
		LeaseTTL: testPollerLeaseTTL,
		Spawner:  &fakeTaskSpawner{},
		Logger:   &controlLoopMockLogger{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var sweepErr *TaskDispatchSweepError
	if got := broken.Sweep(ctx); !errors.As(got, &sweepErr) || !errors.Is(got, ErrTaskDispatchSweep) {
		t.Fatalf("got %v, want TaskDispatchSweepError", got)
	}
	unreadable, err := NewTaskDispatcher(TaskDispatcherConfig{
		Tenants:  &fakeTenantLister{ids: []string{dispatchTenant}},
		Projects: &failingProjectLister{err: errors.New("kv down")},
		Tasks:    newFakeDispatchStore(),
		Leases:   newTestJobLeaseStore(t),
		LeaseTTL: testPollerLeaseTTL,
		Spawner:  &fakeTaskSpawner{},
		Logger:   &controlLoopMockLogger{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := unreadable.SweepProject(ctx, dispatchTenant, dispatchProject); !errors.As(got, &sweepErr) {
		t.Fatalf("SweepProject listing failure: %v", got)
	}
	if got := unreadable.Sweep(ctx); got != nil {
		t.Fatalf("a tenant whose projects cannot be listed must not fail the whole sweep: %v", got)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := f.dispatcher.Sweep(cancelled); err == nil {
		t.Fatal("a cancelled sweep must report it")
	}
}

// TestTaskDispatcher_Run refuses a non-positive interval and returns when its
// context ends.
func TestTaskDispatcher_Run(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t, 0, pendingTask("t-1", 0, 100))
	if err := f.dispatcher.Run(context.Background(), 0); !errors.Is(err, ErrTaskDispatcherConfig) {
		t.Fatalf("a non-positive interval must be refused: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.dispatcher.Run(ctx, time.Hour) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
}

// =============================================================================
// Capacity helpers
// =============================================================================

// TestCountLiveJobLeases counts held leases and ignores released ones.
func TestCountLiveJobLeases(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t, 0)
	ctx := context.Background()
	f.hold(t, "job-a")
	f.hold(t, "job-b")
	live, err := CountLiveJobLeases(ctx, f.leases, dispatchTenant, dispatchProject, time.Now().UTC(), testPollerLeaseTTL)
	if err != nil || live != 2 {
		t.Fatalf("live = %d, %v", live, err)
	}
	if err := f.leases.Release(ctx, &JobLease{
		TenantID: dispatchTenant, ProjectID: dispatchProject,
		Workflow: "any", Ref: "job-a", JobID: "job-a", Generation: 1,
	}); err != nil {
		t.Fatalf("release: %v", err)
	}
	live, err = CountLiveJobLeases(ctx, f.leases, dispatchTenant, dispatchProject, time.Now().UTC(), testPollerLeaseTTL)
	if err != nil || live != 1 {
		t.Fatalf("after release live = %d, %v", live, err)
	}
	stale := time.Now().UTC().Add(2 * testPollerLeaseTTL)
	live, err = CountLiveJobLeases(ctx, f.leases, dispatchTenant, dispatchProject, stale, testPollerLeaseTTL)
	if err != nil || live != 0 {
		t.Fatalf("a lease past its TTL is not live: %d, %v", live, err)
	}
}

// TestCountLiveJobLeases_StoreFailure reports the store's error rather than
// counting zero.
func TestCountLiveJobLeases_StoreFailure(t *testing.T) {
	t.Parallel()
	failing := &failingLeaseLister{err: errors.New("kv down")}
	if _, err := CountLiveJobLeases(context.Background(), failing, "t", "p", time.Now(), time.Minute); err == nil {
		t.Fatal("a store that cannot list must not be counted as empty")
	}
}

// failingProjectLister answers every listing with a fixed error.
type failingProjectLister struct {
	err error
}

func (f *failingProjectLister) ListByTenant(context.Context, string) ([]*Project, error) {
	return nil, f.err
}

// failingLeaseLister answers every List with a fixed error.
type failingLeaseLister struct {
	JobLeaseStore
	err error
}

func (f *failingLeaseLister) List(context.Context, string, string) ([]*JobLease, error) {
	return nil, f.err
}

// TestCountPendingUndispatched counts only the tasks waiting on a dispatch.
func TestCountPendingUndispatched(t *testing.T) {
	t.Parallel()
	spawned := pendingTask("spawned", 0, 100)
	spawned.SpawnedJobID = "job-1"
	manual := pendingTask("manual", 0, 100)
	manual.AutoSpawn = false
	got := CountPendingUndispatched([]*Task{pendingTask("waiting", 0, 100), spawned, manual, nil})
	if got != 1 {
		t.Fatalf("CountPendingUndispatched = %d, want 1", got)
	}
}

// =============================================================================
// The real task store satisfies the dispatcher's surface
// =============================================================================

// TestTaskDispatcher_OverTheRealTaskStore proves TaskStore satisfies
// TaskDispatchStore and that the reason is written through its
// compare-and-swap.
func TestTaskDispatcher_OverTheRealTaskStore(t *testing.T) {
	t.Parallel()
	projects := NewInMemoryProjectStore()
	ctx := ContextWithTenant(context.Background(), dispatchTenant)
	if err := projects.Create(ctx, &Project{
		ID: dispatchProject, TenantID: dispatchTenant, Name: "autonomous", Autonomous: true,
		Quotas: ProjectQuotas{MaxConcurrency: 1},
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	store := NewTaskStore(NewMemoryTaskKV(), NewProcessClaimArbiter())
	id, err := GenerateTaskID()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTask(ctx, &Task{
		ID: id, TenantID: dispatchTenant, ProjectID: dispatchProject,
		Subject: "waiting", Status: TaskStatusPending, AutoSpawn: true,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	leases := newTestJobLeaseStore(t)
	if err := leases.Acquire(ctx, &JobLease{
		TenantID: dispatchTenant, ProjectID: dispatchProject,
		Workflow: "any", Ref: "job-running", JobID: "job-running", Generation: 1,
	}); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	dispatcher, err := NewTaskDispatcher(TaskDispatcherConfig{
		Tenants:  &fakeTenantLister{ids: []string{dispatchTenant}},
		Projects: projects,
		Tasks:    store,
		Leases:   leases,
		LeaseTTL: testPollerLeaseTTL,
		Spawner:  &fakeTaskSpawner{},
		Logger:   &controlLoopMockLogger{},
	})
	if err != nil {
		t.Fatalf("NewTaskDispatcher: %v", err)
	}
	if err := dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	got, err := store.GetTask(ctx, dispatchTenant, id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !strings.Contains(got.DispatchReason, "project at capacity") {
		t.Fatalf("the real store must carry the reason: %q", got.DispatchReason)
	}
}

// TestTaskDispatcher_LeaseCountFailureSkipsTheProject: a project whose live
// leases cannot be counted is left alone rather than dispatched blind.
func TestTaskDispatcher_LeaseCountFailureSkipsTheProject(t *testing.T) {
	t.Parallel()
	projects := NewInMemoryProjectStore()
	ctx := context.Background()
	if err := projects.Create(ctx, &Project{
		ID: dispatchProject, TenantID: dispatchTenant, Name: "autonomous", Autonomous: true,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	spawner := &fakeTaskSpawner{}
	logger := &controlLoopMockLogger{}
	dispatcher, err := NewTaskDispatcher(TaskDispatcherConfig{
		Tenants:  &fakeTenantLister{ids: []string{dispatchTenant}},
		Projects: projects,
		Tasks:    newFakeDispatchStore(pendingTask("t-1", 0, 100)),
		Leases:   &failingLeaseLister{err: errors.New("kv down")},
		LeaseTTL: testPollerLeaseTTL,
		Spawner:  spawner,
		Logger:   logger,
	})
	if err != nil {
		t.Fatalf("NewTaskDispatcher: %v", err)
	}
	if err := dispatcher.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := spawner.dispatched(); len(got) != 0 {
		t.Fatalf("a project whose capacity is unknown dispatched %v", got)
	}
	if n := countLines(logger.warnLines(), "counting live leases failed"); n != 1 {
		t.Fatalf("the counting failure must be reported: %v", logger.warnLines())
	}
}

// =============================================================================
// The agent service as a spawner
// =============================================================================

// newSpawnerService builds an agent service with one idle agent and a minimal
// backend, the least a dispatch needs to reach StartJob.
func newSpawnerService(t *testing.T, tenantID string) (*agentServiceImpl, TaskStore) {
	t.Helper()
	taskStore := NewInMemoryTaskStore()
	registry := NewInMemoryAgentRegistry()
	if err := registry.RegisterAgent(context.Background(), &AgentRegistration{
		ID: "agent-idle", TenantID: tenantID, Name: "worker", Host: "host-1",
		Backend: "ollama", AgentType: "worker", Status: AgentStatusIdle,
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(taskStore)
	impl.SetAgentRegistry(registry)
	impl.backends.Store("ollama", &testMinimalBackend{})
	return impl, taskStore
}

// TestAgentService_SpawnTask_DispatchesAnExistingTask proves the agent
// service satisfies TaskSpawner and that a task dispatched later routes on
// the hints its record carries.
func TestAgentService_SpawnTask_DispatchesAnExistingTask(t *testing.T) {
	t.Parallel()
	const tenantID = "tenant-spawn"
	impl, taskStore := newSpawnerService(t, tenantID)
	ctx := context.Background()

	created, err := impl.CreateTask(ctx, &CreateTaskRequest{
		TenantID: tenantID, Subject: "later", Backend: "ollama",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if created.SpawnedJobID != "" {
		t.Fatal("a task created without auto-spawn must not be dispatched")
	}
	if created.Routing.Backend != "ollama" {
		t.Fatalf("the routing hint must land on the record: %+v", created.Routing)
	}

	jobID, err := impl.SpawnTask(ctx, created)
	if err != nil {
		t.Fatalf("SpawnTask: %v", err)
	}
	if jobID == "" || jobID != created.SpawnedJobID {
		t.Fatalf("SpawnTask returned %q, task carries %q", jobID, created.SpawnedJobID)
	}
	stored, err := taskStore.GetTask(ctx, tenantID, created.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stored.SpawnedJobID != jobID || stored.Attempts != 1 {
		t.Fatalf("stored task = job %q attempts %d", stored.SpawnedJobID, stored.Attempts)
	}
}

// TestAgentService_SpawnTask_Refusals: a spawn with no task, and a spawn with
// no agent to run it, are both reported typed.
func TestAgentService_SpawnTask_Refusals(t *testing.T) {
	t.Parallel()
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(NewInMemoryTaskStore())

	if _, err := impl.SpawnTask(context.Background(), nil); err == nil {
		t.Fatal("a spawn must name the task it is an attempt at")
	}
	_, err = impl.SpawnTask(context.Background(), &Task{
		ID: "t-1", TenantID: "tenant-spawn", Subject: "no agent", Status: TaskStatusPending, AutoSpawn: true,
	})
	if !errors.Is(err, ErrNoMatchingAgent) {
		t.Fatalf("got %v, want ErrNoMatchingAgent", err)
	}
}

// =============================================================================
// Error shapes
// =============================================================================

// TestTaskDispatcherErrors_MessageAndSentinel: every dispatcher error names
// what went wrong and unwraps to its sentinel.
func TestTaskDispatcherErrors_MessageAndSentinel(t *testing.T) {
	t.Parallel()
	cfg := &TaskDispatcherConfigError{Field: "leases"}
	if !strings.Contains(cfg.Error(), "leases is required") || !errors.Is(cfg, ErrTaskDispatcherConfig) {
		t.Fatalf("config error = %q", cfg.Error())
	}
	withMsg := &TaskDispatcherConfigError{Field: "lease_ttl", Message: "must be positive"}
	if !strings.Contains(withMsg.Error(), "lease_ttl must be positive") {
		t.Fatalf("config error with message = %q", withMsg.Error())
	}
	cause := errors.New("iam down")
	sweep := &TaskDispatchSweepError{Cause: cause}
	if !errors.Is(sweep, ErrTaskDispatchSweep) || !errors.Is(sweep, cause) ||
		!strings.Contains(sweep.Error(), "iam down") {
		t.Fatalf("sweep error = %q", sweep.Error())
	}
	missing := &TaskDispatchProjectNotFoundError{TenantID: "t1", ProjectID: "p1"}
	if !errors.Is(missing, ErrTaskDispatchProjectNotFound) || !strings.Contains(missing.Error(), "p1") {
		t.Fatalf("not-found error = %q", missing.Error())
	}
}

// TestTaskDispatcher_ReasonReadFailureIsReported: a task the store cannot
// re-read leaves the sweep running and says so.
func TestTaskDispatcher_ReasonReadFailureIsReported(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t, 1)
	f.hold(t, "job-running")
	// The task is listed but not held, so the re-read inside the reason
	// write misses.
	f.tasks.mu.Lock()
	f.tasks.tasks["ghost"] = pendingTask("ghost", 0, 100)
	f.tasks.mu.Unlock()
	ctx := context.Background()
	f.tasks.beforeUpdate = func(store *fakeDispatchStore, _ *Task) {
		store.mu.Lock()
		delete(store.tasks, "ghost")
		store.mu.Unlock()
	}
	if err := f.dispatcher.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if n := countLines(f.logger.warnLines(), "recording the dispatch reason failed"); n != 1 {
		t.Fatalf("the read failure must be reported: %v", f.logger.warnLines())
	}
}

// TestTaskDispatcher_ClearingTheReasonIsReportedWhenItFails: a dispatched
// task whose refusal cannot be cleared is reported, not silently left stale.
func TestTaskDispatcher_ClearingTheReasonIsReportedWhenItFails(t *testing.T) {
	t.Parallel()
	waiting := pendingTask("t-1", 0, 100)
	waiting.DispatchReason = "project at capacity: 1 of 1 concurrent jobs are running"
	f := newDispatcherFixture(t, 0, waiting)
	f.tasks.updateErr = errors.New("kv down")
	if err := f.dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := f.spawner.dispatched(); len(got) != 1 {
		t.Fatalf("the task must still be dispatched: %v", got)
	}
	if n := countLines(f.logger.warnLines(), "clearing the dispatch reason failed"); n != 1 {
		t.Fatalf("the clear failure must be reported: %v", f.logger.warnLines())
	}
}

// TestTaskDispatcher_ReasonWriteGivesUpAfterTheRetry: a writer that keeps
// winning the race is reported rather than retried forever.
func TestTaskDispatcher_ReasonWriteGivesUpAfterTheRetry(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t, 1, pendingTask("t-1", 0, 100))
	f.hold(t, "job-running")
	f.tasks.beforeUpdate = func(store *fakeDispatchStore, _ *Task) {
		store.bump("t-1", "someone else, again")
	}
	if err := f.dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if f.tasks.updates != cas.MaxAttempts {
		t.Fatalf("the retry is bounded: %d updates", f.tasks.updates)
	}
	if n := countLines(f.logger.warnLines(), "recording the dispatch reason failed"); n != 1 {
		t.Fatalf("the lost race must be reported: %v", f.logger.warnLines())
	}
}

// =============================================================================
// The one writer of Task.DispatchReason
// =============================================================================

// TestSetTaskDispatchReason_Rules pins the three rules both callers depend on:
// an unchanged reason is not written, a task that already has a job is not
// given a NEW reason but may always have one cleared, and a conflict is
// retried rather than overwriting what changed underneath.
func TestSetTaskDispatchReason_Rules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("unchanged reason is not written", func(t *testing.T) {
		t.Parallel()
		task := pendingTask("t-1", 0, 100)
		task.DispatchReason = "already said"
		store := newFakeDispatchStore(task)
		changed, err := SetTaskDispatchReason(ctx, store, dispatchTenant, "t-1", "already said")
		if err != nil || changed {
			t.Fatalf("changed=%v err=%v", changed, err)
		}
		if store.updates != 0 {
			t.Fatalf("an unchanged reason must not reach the store: %d updates", store.updates)
		}
	})

	t.Run("a dispatched task takes no new reason", func(t *testing.T) {
		t.Parallel()
		task := pendingTask("t-2", 0, 100)
		task.SpawnedJobID = "job-1"
		store := newFakeDispatchStore(task)
		changed, err := SetTaskDispatchReason(ctx, store, dispatchTenant, "t-2", "project at capacity")
		if err != nil || changed {
			t.Fatalf("changed=%v err=%v", changed, err)
		}
		if store.reason("t-2") != "" {
			t.Fatalf("a running task must not be stamped with a refusal: %q", store.reason("t-2"))
		}
	})

	t.Run("a dispatched task may always be cleared", func(t *testing.T) {
		t.Parallel()
		task := pendingTask("t-3", 0, 100)
		task.SpawnedJobID = "job-1"
		task.DispatchReason = "stale refusal"
		store := newFakeDispatchStore(task)
		changed, err := SetTaskDispatchReason(ctx, store, dispatchTenant, "t-3", "")
		if err != nil || !changed {
			t.Fatalf("changed=%v err=%v", changed, err)
		}
		if store.reason("t-3") != "" {
			t.Fatalf("the stale refusal survived: %q", store.reason("t-3"))
		}
	})

	t.Run("a conflict is retried", func(t *testing.T) {
		t.Parallel()
		store := newFakeDispatchStore(pendingTask("t-4", 0, 100))
		raced := false
		store.beforeUpdate = func(s *fakeDispatchStore, _ *Task) {
			if raced {
				return
			}
			raced = true
			s.bump("t-4", "someone else")
		}
		changed, err := SetTaskDispatchReason(ctx, store, dispatchTenant, "t-4", "project at capacity")
		if err != nil || !changed {
			t.Fatalf("changed=%v err=%v", changed, err)
		}
		if store.reason("t-4") != "project at capacity" {
			t.Fatalf("the retry must land the reason: %q", store.reason("t-4"))
		}
		if store.updates != 2 {
			t.Fatalf("one conflict means exactly one retry: %d updates", store.updates)
		}
	})

	t.Run("a read failure is reported", func(t *testing.T) {
		t.Parallel()
		store := newFakeDispatchStore()
		if _, err := SetTaskDispatchReason(ctx, store, dispatchTenant, "missing", "why"); err == nil {
			t.Fatal("a task that cannot be read must report it")
		}
	})

	t.Run("a write failure is reported", func(t *testing.T) {
		t.Parallel()
		store := newFakeDispatchStore(pendingTask("t-5", 0, 100))
		store.updateErr = errors.New("kv down")
		if _, err := SetTaskDispatchReason(ctx, store, dispatchTenant, "t-5", "why"); err == nil {
			t.Fatal("a write that fails must report it")
		}
	})
}

// TestTaskDispatcher_DispatchesSystemTenantTask locks the owner ruling
// (2026-09-09, issue #658): the system tenant has parity with regular tenants,
// so a system-tenant project's pending task must adopt and dispatch through the
// exact same sweep path — the dispatcher must NOT refuse the system tenant.
// The spawner is mocked (the forge/worker is out of scope here); this proves
// the dispatch stage carries the system tenant end to end as far as the sweep.
func TestTaskDispatcher_DispatchesSystemTenantTask(t *testing.T) {
	projects := NewInMemoryProjectStore()
	ctx := context.Background()
	project := &Project{
		ID: "proj-sys-auto", TenantID: SystemTenantID, Name: "system-autonomous",
		Autonomous: true, Quotas: ProjectQuotas{MaxConcurrency: 0},
	}
	if err := projects.Create(ctx, project); err != nil {
		t.Fatalf("create system-tenant project: %v", err)
	}
	task := &Task{
		ID: "task-sys-1", TenantID: SystemTenantID, ProjectID: project.ID,
		Subject: "task-sys-1", Status: TaskStatusPending, AutoSpawn: true,
		Priority: 1, CreatedAt: 1, Version: 1,
	}
	store := newFakeDispatchStore(task)
	leases := newTestJobLeaseStore(t)
	spawner := &fakeTaskSpawner{}
	dispatcher, err := NewTaskDispatcher(TaskDispatcherConfig{
		Tenants:  &fakeTenantLister{ids: []string{SystemTenantID}},
		Projects: projects,
		Tasks:    store,
		Leases:   leases,
		LeaseTTL: testPollerLeaseTTL,
		Spawner:  spawner,
		Logger:   &controlLoopMockLogger{},
	})
	if err != nil {
		t.Fatalf("NewTaskDispatcher: %v", err)
	}

	if err := dispatcher.SweepProject(ctx, SystemTenantID, project.ID); err != nil {
		t.Fatalf("SweepProject for system tenant must succeed (parity), got: %v", err)
	}
	if got := spawner.dispatched(); len(got) != 1 || got[0] != task.ID {
		t.Fatalf("system-tenant task must dispatch, spawned = %v", got)
	}
}
