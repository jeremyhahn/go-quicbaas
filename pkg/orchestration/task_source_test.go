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
// Fakes
// =============================================================================

// fakeTaskSource records the projects it was polled for and answers with a
// fixed set of specs, or a fixed error.
type fakeTaskSource struct {
	name  string
	specs []TaskSourceSpec

	mu     sync.Mutex
	err    error
	polled []string
}

func (f *fakeTaskSource) Name() string { return f.name }

func (f *fakeTaskSource) Poll(_ context.Context, project *Project) ([]TaskSourceSpec, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polled = append(f.polled, project.ID)
	if f.err != nil {
		return nil, f.err
	}
	return f.specs, nil
}

func (f *fakeTaskSource) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeTaskSource) polledProjects() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.polled...)
}

// fakeTenantLister answers with fixed tenant IDs or a fixed error.
type fakeTenantLister struct {
	ids []string
	err error
}

func (f *fakeTenantLister) ListTenantIDs(context.Context) ([]string, error) { return f.ids, f.err }

// storeBackedTaskCreator is a TaskCreator over a REAL task store, so the
// guarded source index — not any bookkeeping in the poller — is what makes
// re-polling idempotent. It does what the agent service's CreateTask does
// with a request, minus the auto-spawn.
type storeBackedTaskCreator struct {
	store TaskStore

	mu        sync.Mutex
	requests  []*CreateTaskRequest
	spawnErr  error // returned WITH the created task, like an auto-spawn that found no agent
	createErr error // returned INSTEAD of a task, like a store that refused the create
}

func (c *storeBackedTaskCreator) CreateTask(ctx context.Context, req *CreateTaskRequest) (*Task, error) {
	c.mu.Lock()
	c.requests = append(c.requests, req)
	c.mu.Unlock()
	if c.createErr != nil {
		return nil, c.createErr
	}
	if err := req.Source.Validate(); err != nil {
		return nil, err
	}
	id, err := GenerateTaskID()
	if err != nil {
		return nil, err
	}
	task := &Task{
		ID: id, TenantID: req.TenantID, ProjectID: req.ProjectID, Subject: req.Subject,
		Description: req.Description, Metadata: req.Metadata, Source: req.Source,
		Priority: req.Priority, AttemptBudget: req.AttemptBudget, AutoSpawn: req.AutoSpawn,
		Runner: ResolveJobRunnerName(req.Runner),
	}
	// The park is applied through the SAME helper the agent service applies it
	// with, so a poller test of a parked adoption exercises the production rule
	// rather than a restatement of it.
	if err := applyCreateTaskPark(task, req.Park); err != nil {
		return nil, err
	}
	if err := c.store.CreateTask(ctx, task); err != nil {
		return nil, err
	}
	if c.spawnErr != nil {
		return task, c.spawnErr
	}
	return task, nil
}

func (c *storeBackedTaskCreator) requestCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.requests)
}

// testPollerLeaseTTL is the lease TTL every poller and dispatcher test is
// built with.
const testPollerLeaseTTL = time.Minute

// newTestJobLeaseStore is a single-process lease store for a test that only
// needs somewhere to count leases from.
func newTestJobLeaseStore(t *testing.T) JobLeaseStore {
	t.Helper()
	store, err := NewProcessJobLeaseStore(testPollerLeaseTTL)
	if err != nil {
		t.Fatalf("NewProcessJobLeaseStore: %v", err)
	}
	return store
}

// pollerFixture is one poller over one tenant with two projects: one the
// operator turned autonomous and one they did not.
type pollerFixture struct {
	registry *TaskSourceRegistry
	creator  *storeBackedTaskCreator
	logger   *controlLoopMockLogger
	projects ProjectStore
	leases   JobLeaseStore
	poller   *TaskSourcePoller
}

func newPollerFixture(t *testing.T) *pollerFixture {
	t.Helper()
	projects := NewInMemoryProjectStore()
	ctx := context.Background()
	for _, p := range []*Project{
		{ID: "proj-flow", TenantID: "tenant-a", Name: "autonomous", Autonomous: true, DefaultWorkflow: "delivery"},
		{ID: "proj-none", TenantID: "tenant-a", Name: "operator-driven"},
	} {
		if err := projects.Create(ctx, p); err != nil {
			t.Fatalf("create project %s: %v", p.ID, err)
		}
	}
	creator := &storeBackedTaskCreator{store: NewTaskStore(NewMemoryTaskKV(), NewProcessClaimArbiter())}
	registry := NewTaskSourceRegistry()
	logger := &controlLoopMockLogger{}
	leases := newTestJobLeaseStore(t)
	poller, err := NewTaskSourcePoller(TaskSourcePollerConfig{
		Sources:  registry,
		Tenants:  &fakeTenantLister{ids: []string{"tenant-a"}},
		Projects: projects,
		Tasks:    creator,
		Queue:    creator.store,
		Leases:   leases,
		LeaseTTL: testPollerLeaseTTL,
		Logger:   logger,
	})
	if err != nil {
		t.Fatalf("NewTaskSourcePoller: %v", err)
	}
	return &pollerFixture{
		registry: registry, creator: creator, logger: logger,
		projects: projects, leases: leases, poller: poller,
	}
}

// setMaxConcurrency bounds the fixture project's concurrency so the adoption
// cap has something to compute against.
func (f *pollerFixture) setMaxConcurrency(t *testing.T, projectID string, limit int) {
	t.Helper()
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	project, err := f.projects.Get(ctx, "tenant-a", projectID)
	if err != nil {
		t.Fatalf("get project %s: %v", projectID, err)
	}
	project.Quotas.MaxConcurrency = limit
	if err := f.projects.Update(ctx, project); err != nil {
		t.Fatalf("update project %s: %v", projectID, err)
	}
}

func (f *pollerFixture) register(t *testing.T, kind TaskSourceKind, src TaskSource) {
	t.Helper()
	if err := f.registry.Register(kind, src); err != nil {
		t.Fatalf("register %s: %v", src.Name(), err)
	}
}

func (f *pollerFixture) tasks(t *testing.T) []*Task {
	t.Helper()
	tasks, err := f.creator.store.ListTasks(context.Background(), "tenant-a", &TaskFilter{})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	return tasks
}

func countLines(lines []string, substr string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, substr) {
			n++
		}
	}
	return n
}

// =============================================================================
// Registry
// =============================================================================

func TestTaskSourceRegistry_DuplicateNameRefused(t *testing.T) {
	t.Parallel()
	reg := NewTaskSourceRegistry()
	if err := reg.Register("tracker-dup", &fakeTaskSource{name: "dup"}); err != nil {
		t.Fatalf("first register: %v", err)
	}
	err := reg.Register("tracker-dup", &fakeTaskSource{name: "dup"})
	var dup *TaskSourceAlreadyRegisteredError
	if !errors.As(err, &dup) || dup.Name != "dup" {
		t.Fatalf("second register: got %v, want TaskSourceAlreadyRegisteredError", err)
	}
	if !errors.Is(err, ErrTaskSourceAlreadyRegistered) || !strings.Contains(err.Error(), "dup") {
		t.Fatalf("must unwrap to ErrTaskSourceAlreadyRegistered and name the source: %v", err)
	}
	if got, ok := reg.Lookup("dup"); !ok || got.Kind != "tracker-dup" {
		t.Fatalf("Lookup = %+v %v", got, ok)
	}
	if _, ok := reg.Lookup("missing"); ok {
		t.Fatal("Lookup of an unregistered name must miss")
	}
}

func TestTaskSourceRegistry_RefusesNilEmptyNameAndEmptyKind(t *testing.T) {
	t.Parallel()
	reg := NewTaskSourceRegistry()
	var regErr *TaskSourceRegistrationError
	if err := reg.Register("k", nil); !errors.As(err, &regErr) || !errors.Is(err, ErrTaskSourceRegistration) {
		t.Fatalf("nil source: %v", err)
	}
	if err := reg.Register("k", &fakeTaskSource{}); !errors.As(err, &regErr) {
		t.Fatalf("empty name: %v", err)
	}
	if err := reg.Register("", &fakeTaskSource{name: "n"}); !errors.As(err, &regErr) || regErr.Name != "n" {
		t.Fatalf("empty kind: %v", err)
	}
	if len(reg.Sources()) != 0 {
		t.Fatal("a refused registration must not register")
	}
}

func TestTaskSourceRegistry_SourcesSortedByName(t *testing.T) {
	t.Parallel()
	reg := NewTaskSourceRegistry()
	for _, name := range []string{"zeta", "alpha", "mid"} {
		if err := reg.Register("kind-"+TaskSourceKind(name), &fakeTaskSource{name: name}); err != nil {
			t.Fatal(err)
		}
	}
	sources := reg.Sources()
	if len(sources) != 3 || sources[0].Source.Name() != "alpha" || sources[1].Source.Name() != "mid" || sources[2].Source.Name() != "zeta" {
		t.Fatalf("Sources() order = %v", sources)
	}
}

// TestTaskSourceKind_OpenSet: the engine's own kinds validate, a kind a
// source declared validates from registration on, and everything else is
// refused typed — there is no module-specific constant in the engine.
func TestTaskSourceKind_OpenSet(t *testing.T) {
	t.Parallel()
	for _, kind := range []TaskSourceKind{TaskSourceKindScheduler, TaskSourceKindOperator, TaskSourceKindJob} {
		if err := kind.Validate(); err != nil {
			t.Errorf("built-in %s refused: %v", kind, err)
		}
	}
	const declared TaskSourceKind = "declared-by-a-test-source"
	var invalid *InvalidTaskSourceKindError
	if err := declared.Validate(); !errors.As(err, &invalid) {
		t.Fatalf("an undeclared kind must be refused typed, got %v", err)
	}
	registerTaskSourceForTest(t, declared, &fakeTaskSource{name: "declaring-source"})
	if err := declared.Validate(); err != nil {
		t.Fatalf("a declared kind must validate: %v", err)
	}
	if err := (&TaskSourceRef{Kind: declared, Ref: "1"}).Validate(); err != nil {
		t.Fatalf("a ref carrying a declared kind must validate: %v", err)
	}
	if _, ok := LookupTaskSource("declaring-source"); !ok {
		t.Fatal("the engine registry must carry the source")
	}
	found := false
	for _, reg := range TaskSources() {
		if reg.Source.Name() == "declaring-source" && reg.Kind == declared {
			found = true
		}
	}
	if !found || DefaultTaskSourceRegistry() != taskSources {
		t.Fatal("TaskSources must list the engine registry")
	}
}

func TestTaskSourceSpec_Validate(t *testing.T) {
	t.Parallel()
	good := &TaskSourceSpec{Source: TaskSourceRef{Kind: "k", Ref: "r"}, Subject: "s"}
	if err := good.Validate("k"); err != nil {
		t.Fatalf("valid spec refused: %v", err)
	}
	var invalid *TaskSourceSpecInvalidError
	cases := map[string]struct {
		spec *TaskSourceSpec
		kind TaskSourceKind
	}{
		"nil":           {nil, "k"},
		"no subject":    {&TaskSourceSpec{Source: TaskSourceRef{Kind: "k", Ref: "r"}}, "k"},
		"no ref":        {&TaskSourceSpec{Source: TaskSourceRef{Kind: "k"}, Subject: "s"}, "k"},
		"kind mismatch": {&TaskSourceSpec{Source: TaskSourceRef{Kind: "other", Ref: "r"}, Subject: "s"}, "k"},
	}
	for name, c := range cases {
		err := c.spec.Validate(c.kind)
		if !errors.As(err, &invalid) || !errors.Is(err, ErrTaskSourceSpecInvalid) {
			t.Errorf("%s: got %v, want TaskSourceSpecInvalidError", name, err)
		}
	}
}

// =============================================================================
// Poller
// =============================================================================

func TestNewTaskSourcePoller_RequiresEveryCollaborator(t *testing.T) {
	t.Parallel()
	queue := NewTaskStore(NewMemoryTaskKV(), NewProcessClaimArbiter())
	full := TaskSourcePollerConfig{
		Sources: NewTaskSourceRegistry(), Tenants: &fakeTenantLister{},
		Projects: NewInMemoryProjectStore(), Tasks: &storeBackedTaskCreator{},
		Queue: queue, Leases: newTestJobLeaseStore(t), LeaseTTL: testPollerLeaseTTL,
		Logger: &controlLoopMockLogger{},
	}
	if _, err := NewTaskSourcePoller(full); err != nil {
		t.Fatalf("complete config refused: %v", err)
	}
	for field, mutate := range map[string]func(*TaskSourcePollerConfig){
		"sources":   func(c *TaskSourcePollerConfig) { c.Sources = nil },
		"tenants":   func(c *TaskSourcePollerConfig) { c.Tenants = nil },
		"projects":  func(c *TaskSourcePollerConfig) { c.Projects = nil },
		"tasks":     func(c *TaskSourcePollerConfig) { c.Tasks = nil },
		"queue":     func(c *TaskSourcePollerConfig) { c.Queue = nil },
		"leases":    func(c *TaskSourcePollerConfig) { c.Leases = nil },
		"lease_ttl": func(c *TaskSourcePollerConfig) { c.LeaseTTL = 0 },
		"logger":    func(c *TaskSourcePollerConfig) { c.Logger = nil },
	} {
		cfg := full
		mutate(&cfg)
		_, err := NewTaskSourcePoller(cfg)
		var cfgErr *TaskSourcePollerConfigError
		if !errors.As(err, &cfgErr) || cfgErr.Field != field || !errors.Is(err, ErrTaskSourcePollerConfig) {
			t.Errorf("missing %s: got %v, want TaskSourcePollerConfigError", field, err)
		}
	}
}

// TestTaskSourcePoller_CreatesOncePerSourceRef: two sweeps of a source that
// keeps offering the same spec produce ONE task, because the task store's
// guarded index refuses the second adoption — the poller keeps no memory.
func TestTaskSourcePoller_CreatesOncePerSourceRef(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	const kind TaskSourceKind = "poller-test-tracker"
	src := &fakeTaskSource{name: "tracker", specs: []TaskSourceSpec{{
		Source:  TaskSourceRef{Kind: kind, Ref: "42", URL: "https://tracker.invalid/42"},
		Subject: "Fix the thing", Description: "details", Priority: 3, AttemptBudget: 2,
		Metadata: map[string]string{"label": "ready"},
	}}}
	f.register(t, kind, src)

	for i := 0; i < 2; i++ {
		if err := f.poller.Sweep(context.Background()); err != nil {
			t.Fatalf("sweep %d: %v", i, err)
		}
	}

	tasks := f.tasks(t)
	if len(tasks) != 1 {
		t.Fatalf("expected exactly one task across two sweeps, got %d", len(tasks))
	}
	task := tasks[0]
	if task.ProjectID != "proj-flow" || task.Subject != "Fix the thing" || !task.AutoSpawn ||
		task.Priority != 3 || task.AttemptBudget != 2 || task.Metadata["label"] != "ready" ||
		task.Source == nil || task.Source.Kind != kind || task.Source.Ref != "42" {
		t.Fatalf("task carries the spec: %+v source=%+v", task, task.Source)
	}
	if f.creator.requestCount() != 2 {
		t.Fatalf("both sweeps must reach the store (the index decides): %d requests", f.creator.requestCount())
	}
	if n := countLines(f.logger.infoLines(), "as task "); n != 1 {
		t.Fatalf("adoption logged once, got %d", n)
	}
}

// TestTaskSourcePoller_AdoptsTheSourcesRunner: a source that supplies work
// its own runner drives says so on the spec, and the runner reaches the Task
// — without it a requeued task could not be re-dispatched by the runner that
// ran it. A spec naming none leaves the task to the lead-driven runner.
func TestTaskSourcePoller_AdoptsTheSourcesRunner(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	const kind TaskSourceKind = "poller-runner-test-tracker"
	src := &fakeTaskSource{name: "runner-tracker", specs: []TaskSourceSpec{
		{Source: TaskSourceRef{Kind: kind, Ref: "7"}, Subject: "module work", Runner: "sdlc"},
		{Source: TaskSourceRef{Kind: kind, Ref: "8"}, Subject: "lead-driven work"},
	}}
	f.register(t, kind, src)

	if err := f.poller.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	byRef := make(map[string]*Task)
	for _, task := range f.tasks(t) {
		if task.Source != nil {
			byRef[task.Source.Ref] = task
		}
	}
	if len(byRef) != 2 {
		t.Fatalf("expected both specs adopted, got %d", len(byRef))
	}
	if got := byRef["7"].Runner; got != "sdlc" {
		t.Fatalf("the spec's runner must reach the task, got %q", got)
	}
	if got := byRef["8"].Runner; got != JobRunnerWorkflow {
		t.Fatalf("a spec naming no runner is lead-driven, got %q", got)
	}
}

// TestTaskSourcePoller_NonAutonomousProjectsNeverPolled: a project the
// operator has not turned autonomous runs nothing on its own, so its sources
// are never asked.
func TestTaskSourcePoller_NonAutonomousProjectsNeverPolled(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	src := &fakeTaskSource{name: "quiet"}
	f.register(t, "poller-test-quiet", src)
	if err := f.poller.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	polled := src.polledProjects()
	if len(polled) != 1 || polled[0] != "proj-flow" {
		t.Fatalf("polled projects = %v, want only proj-flow", polled)
	}
}

// TestTaskSourcePoller_UnavailableSourceSkippedAndLoggedOnce: an outage is
// reported when it begins and when it ends, not on every sweep, and other
// sources keep being read.
func TestTaskSourcePoller_UnavailableSourceSkippedAndLoggedOnce(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	down := &fakeTaskSource{name: "down"}
	down.setErr(&TaskSourceUnavailableError{Source: "down", Cause: errors.New("connection refused")})
	up := &fakeTaskSource{name: "up", specs: []TaskSourceSpec{{
		Source: TaskSourceRef{Kind: "poller-test-up", Ref: "1"}, Subject: "still adopted",
	}}}
	f.register(t, "poller-test-down", down)
	f.register(t, "poller-test-up", up)

	for i := 0; i < 3; i++ {
		if err := f.poller.Sweep(context.Background()); err != nil {
			t.Fatalf("sweep %d: %v", i, err)
		}
	}
	if n := countLines(f.logger.warnLines(), "is unavailable and will be skipped"); n != 1 {
		t.Fatalf("outage logged once, got %d: %v", n, f.logger.warnLines())
	}
	if len(f.tasks(t)) != 1 {
		t.Fatal("the available source must still be adopted while another is down")
	}

	down.setErr(nil)
	if err := f.poller.Sweep(context.Background()); err != nil {
		t.Fatalf("recovery sweep: %v", err)
	}
	if n := countLines(f.logger.infoLines(), "is available again"); n != 1 {
		t.Fatalf("recovery logged once, got %d", n)
	}
	down.setErr(&TaskSourceUnavailableError{Source: "down"})
	if err := f.poller.Sweep(context.Background()); err != nil {
		t.Fatalf("second outage sweep: %v", err)
	}
	if n := countLines(f.logger.warnLines(), "is unavailable and will be skipped"); n != 2 {
		t.Fatalf("a new outage is a new report, got %d", n)
	}
}

// TestTaskSourcePoller_OtherPollErrorsReportedEverySweep: only an outage is
// deduplicated; any other poll failure is a defect the operator hears about
// each time.
func TestTaskSourcePoller_OtherPollErrorsReportedEverySweep(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	broken := &fakeTaskSource{name: "broken"}
	broken.setErr(errors.New("malformed answer"))
	f.register(t, "poller-test-broken", broken)
	for i := 0; i < 2; i++ {
		if err := f.poller.Sweep(context.Background()); err != nil {
			t.Fatalf("sweep %d: %v", i, err)
		}
	}
	if n := countLines(f.logger.warnLines(), "poll failed"); n != 2 {
		t.Fatalf("poll failure logged per sweep, got %d", n)
	}
}

// TestTaskSourcePoller_RefusesASpecOfAnotherKind: a source may only emit the
// kind it declared; a spec claiming another provenance is refused and logged.
func TestTaskSourcePoller_RefusesASpecOfAnotherKind(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	f.register(t, "poller-test-honest", &fakeTaskSource{name: "liar", specs: []TaskSourceSpec{{
		Source: TaskSourceRef{Kind: TaskSourceKindOperator, Ref: "1"}, Subject: "claims to be an operator",
	}}})
	if err := f.poller.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(f.tasks(t)) != 0 {
		t.Fatal("a spec of another kind must not be adopted")
	}
	if n := countLines(f.logger.warnLines(), "refused spec"); n != 1 {
		t.Fatalf("refusal logged, got %d", n)
	}
}

// TestTaskSourcePoller_CreatedButNotSpawnedIsReported: a task the store
// accepted but auto-spawn could not place stays a task; the poller reports it
// and does not retry, because the next sweep sees it already adopted.
func TestTaskSourcePoller_CreatedButNotSpawnedIsReported(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	f.creator.spawnErr = &NoMatchingAgentError{Constraint: "agent_selection"}
	f.register(t, "poller-test-unplaced", &fakeTaskSource{name: "unplaced", specs: []TaskSourceSpec{{
		Source: TaskSourceRef{Kind: "poller-test-unplaced", Ref: "7"}, Subject: "waits for an agent",
	}}})
	for i := 0; i < 2; i++ {
		if err := f.poller.Sweep(context.Background()); err != nil {
			t.Fatalf("sweep %d: %v", i, err)
		}
	}
	if len(f.tasks(t)) != 1 {
		t.Fatal("the task must exist exactly once")
	}
	if n := countLines(f.logger.warnLines(), "created but not spawned"); n != 1 {
		t.Fatalf("reported once (the second sweep is already adopted), got %d", n)
	}
}

// TestTaskSourcePoller_CreateFailureIsReported: a store refusal other than
// "already adopted" is a warning, and the sweep continues.
func TestTaskSourcePoller_CreateFailureIsReported(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	f.creator.createErr = &TaskStoreError{Sentinel: ErrTaskStoreInvalidInput, Message: "refused"}
	f.register(t, "poller-test-refused", &fakeTaskSource{name: "refused", specs: []TaskSourceSpec{{
		Source: TaskSourceRef{Kind: "poller-test-refused", Ref: "1"}, Subject: "the store refuses this",
	}}})
	if err := f.poller.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(f.tasks(t)) != 0 {
		t.Fatal("a refused create must not leave a task")
	}
	if n := countLines(f.logger.warnLines(), "creating task"); n != 1 {
		t.Fatalf("create failure logged, got %d: %v", n, f.logger.warnLines())
	}
}

func TestTaskSourcePoller_SweepErrors(t *testing.T) {
	t.Parallel()
	logger := &controlLoopMockLogger{}
	registry := NewTaskSourceRegistry()
	if err := registry.Register("poller-test-any", &fakeTaskSource{name: "any"}); err != nil {
		t.Fatal(err)
	}
	poller, err := NewTaskSourcePoller(TaskSourcePollerConfig{
		Sources: registry, Tenants: &fakeTenantLister{err: errors.New("iam down")},
		Projects: NewInMemoryProjectStore(), Tasks: &storeBackedTaskCreator{},
		Queue:  NewTaskStore(NewMemoryTaskKV(), NewProcessClaimArbiter()),
		Leases: newTestJobLeaseStore(t), LeaseTTL: testPollerLeaseTTL, Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	sweepErr := poller.Sweep(context.Background())
	var typed *TaskSourceSweepError
	if !errors.As(sweepErr, &typed) || !errors.Is(sweepErr, ErrTaskSourceSweep) {
		t.Fatalf("got %v, want TaskSourceSweepError", sweepErr)
	}

	empty, err := NewTaskSourcePoller(TaskSourcePollerConfig{
		Sources: NewTaskSourceRegistry(), Tenants: &fakeTenantLister{err: errors.New("never asked")},
		Projects: NewInMemoryProjectStore(), Tasks: &storeBackedTaskCreator{},
		Queue:  NewTaskStore(NewMemoryTaskKV(), NewProcessClaimArbiter()),
		Leases: newTestJobLeaseStore(t), LeaseTTL: testPollerLeaseTTL, Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := empty.Sweep(context.Background()); err != nil {
		t.Fatalf("no sources means nothing to sweep, not an error: %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := poller.Sweep(cancelled); err == nil {
		t.Fatal("a cancelled sweep must report it")
	}
}

func TestTaskSourcePoller_Run(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	if err := f.poller.Run(context.Background(), 0); !errors.Is(err, ErrTaskSourcePollerConfig) {
		t.Fatalf("a non-positive interval must be refused: %v", err)
	}

	polled := make(chan struct{}, 1)
	f.register(t, "poller-test-run", &notifyingTaskSource{name: "run", polled: polled})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.poller.Run(ctx, time.Hour) }()
	select {
	case <-polled:
	case <-time.After(5 * time.Second):
		t.Fatal("Run must sweep immediately, before the first tick")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run must return when its context is cancelled")
	}
}

// notifyingTaskSource signals the first poll on a channel.
type notifyingTaskSource struct {
	name   string
	polled chan struct{}
}

func (n *notifyingTaskSource) Name() string { return n.name }

func (n *notifyingTaskSource) Poll(context.Context, *Project) ([]TaskSourceSpec, error) {
	select {
	case n.polled <- struct{}{}:
	default:
	}
	return nil, nil
}

// =============================================================================
// Errors
// =============================================================================

func TestTaskSourceErrors(t *testing.T) {
	t.Parallel()
	cause := errors.New("cause")
	unavailable := &TaskSourceUnavailableError{Source: "s", Cause: cause}
	if !errors.Is(unavailable, ErrTaskSourceUnavailable) || !errors.Is(unavailable, cause) || !strings.Contains(unavailable.Error(), "s: cause") {
		t.Fatalf("unavailable: %v", unavailable)
	}
	if bare := (&TaskSourceUnavailableError{Source: "s"}); !errors.Is(bare, ErrTaskSourceUnavailable) || strings.HasSuffix(bare.Error(), ": ") {
		t.Fatalf("unavailable without cause: %v", bare)
	}
	regErr := &TaskSourceRegistrationError{Message: "m"}
	if !strings.HasSuffix(regErr.Error(), ": m") {
		t.Fatalf("registration error without name: %v", regErr)
	}
	spec := &TaskSourceSpecInvalidError{Field: "subject", Message: "must not be empty"}
	if !strings.Contains(spec.Error(), "subject must not be empty") {
		t.Fatalf("spec error: %v", spec)
	}
	cfg := &TaskSourcePollerConfigError{Field: "sweep_interval", Message: "must be positive"}
	if !strings.Contains(cfg.Error(), "sweep_interval must be positive") {
		t.Fatalf("config error with message: %v", cfg)
	}
	sweep := &TaskSourceSweepError{Cause: cause}
	if !errors.Is(sweep, cause) || !strings.Contains(sweep.Error(), "cause") {
		t.Fatalf("sweep error: %v", sweep)
	}
}

// TestTaskSourcePoller_SweepProject pins the module's push path: one
// project swept now, adopting what its sources hold, with the same
// idempotence as the interval sweep; an unknown project is refused typed
// and a project that is not autonomous is swept for nothing.
func TestTaskSourcePoller_SweepProject(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	src := &fakeTaskSource{name: "push", specs: []TaskSourceSpec{
		{Source: TaskSourceRef{Kind: "push-kind", Ref: "one"}, Subject: "one"},
	}}
	f.register(t, "push-kind", src)
	ctx := context.Background()
	if err := f.poller.SweepProject(ctx, "tenant-a", "proj-flow"); err != nil {
		t.Fatalf("SweepProject: %v", err)
	}
	if err := f.poller.SweepProject(ctx, "tenant-a", "proj-flow"); err != nil {
		t.Fatalf("second SweepProject: %v", err)
	}
	tasks, err := f.creator.store.ListTasks(ContextWithTenant(ctx, "tenant-a"), "tenant-a", &TaskFilter{ProjectID: "proj-flow"})
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks after two project sweeps = %d, %v", len(tasks), err)
	}
	if err := f.poller.SweepProject(ctx, "tenant-a", "proj-none"); err != nil {
		t.Fatalf("project that is not autonomous: %v", err)
	}
	none, _ := f.creator.store.ListTasks(ContextWithTenant(ctx, "tenant-a"), "tenant-a", &TaskFilter{ProjectID: "proj-none"})
	if len(none) != 0 {
		t.Fatal("a project that is not autonomous adopted work")
	}
	err = f.poller.SweepProject(ctx, "tenant-a", "proj-missing")
	var missing *TaskSourceProjectNotFoundError
	if !errors.As(err, &missing) || !errors.Is(err, ErrTaskSourceProjectNotFound) || missing.ProjectID != "proj-missing" {
		t.Fatalf("unknown project: %v", err)
	}
	if err := f.poller.SweepProject(ctx, "", "proj-flow"); !errors.Is(err, ErrTaskSourcePollerConfig) {
		t.Fatalf("empty tenant: %v", err)
	}
	empty, err := NewTaskSourcePoller(TaskSourcePollerConfig{
		Sources: NewTaskSourceRegistry(), Tenants: &fakeTenantLister{}, Projects: NewInMemoryProjectStore(),
		Tasks: &storeBackedTaskCreator{}, Queue: NewTaskStore(NewMemoryTaskKV(), NewProcessClaimArbiter()),
		Leases: newTestJobLeaseStore(t), LeaseTTL: testPollerLeaseTTL, Logger: &controlLoopMockLogger{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := empty.SweepProject(ctx, "tenant-a", "proj-flow"); err != nil {
		t.Fatalf("no sources: %v", err)
	}
}

// =============================================================================
// The adoption cap
// =============================================================================

// capSpecs builds n specs of ascending priority, so the cap has an order to
// respect.
func capSpecs(kind TaskSourceKind, n int) []TaskSourceSpec {
	specs := make([]TaskSourceSpec, 0, n)
	for i := 0; i < n; i++ {
		ref := string(rune('a' + i))
		specs = append(specs, TaskSourceSpec{
			Source:   TaskSourceRef{Kind: kind, Ref: ref},
			Subject:  ref,
			Priority: i,
		})
	}
	return specs
}

// TestTaskSourcePoller_AdoptsAtMostTheProjectsCapacity: a project allowed two
// concurrent jobs adopts two of five offered specs, highest priority first,
// and reports what it did once.
func TestTaskSourcePoller_AdoptsAtMostTheProjectsCapacity(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	f.setMaxConcurrency(t, "proj-flow", 2)
	const kind TaskSourceKind = "cap-kind"
	f.register(t, kind, &fakeTaskSource{name: "capped", specs: capSpecs(kind, 5)})

	if err := f.poller.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	tasks := f.tasks(t)
	if len(tasks) != 2 {
		t.Fatalf("a project allowed two concurrent jobs adopted %d specs", len(tasks))
	}
	adopted := map[string]bool{}
	for _, task := range tasks {
		adopted[task.Source.Ref] = true
	}
	if !adopted["e"] || !adopted["d"] {
		t.Fatalf("the cap must take the highest priorities first: %v", adopted)
	}
	if n := countLines(f.logger.infoLines(), "seen="); n != 1 {
		t.Fatalf("one summary line per sweep per project, got %d: %v", n, f.logger.infoLines())
	}
}

// TestTaskSourcePoller_CapCountsWhatIsAlreadyWaiting: the tasks a previous
// sweep adopted and nothing has dispatched hold their slots, so a second
// sweep adopts nothing more.
func TestTaskSourcePoller_CapCountsWhatIsAlreadyWaiting(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	f.setMaxConcurrency(t, "proj-flow", 2)
	const kind TaskSourceKind = "cap-waiting-kind"
	src := &fakeTaskSource{name: "waiting", specs: capSpecs(kind, 5)}
	f.register(t, kind, src)
	ctx := context.Background()
	if err := f.poller.Sweep(ctx); err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	if err := f.poller.Sweep(ctx); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if tasks := f.tasks(t); len(tasks) != 2 {
		t.Fatalf("two sweeps must not exceed the cap: %d tasks", len(tasks))
	}
}

// TestTaskSourcePoller_CapCountsRunningJobs: a live lease spends a slot the
// cap would otherwise give a new adoption.
func TestTaskSourcePoller_CapCountsRunningJobs(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	f.setMaxConcurrency(t, "proj-flow", 1)
	if err := f.leases.Acquire(context.Background(), &JobLease{
		TenantID: "tenant-a", ProjectID: "proj-flow",
		Workflow: "delivery", Ref: "job-running", JobID: "job-running", Generation: 1,
	}); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	const kind TaskSourceKind = "cap-running-kind"
	f.register(t, kind, &fakeTaskSource{name: "running", specs: capSpecs(kind, 3)})
	if err := f.poller.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if tasks := f.tasks(t); len(tasks) != 0 {
		t.Fatalf("a full project must adopt nothing, got %d tasks", len(tasks))
	}
}

// TestTaskSourcePoller_UnboundedProjectAdoptsEverything: a project that
// declares no MaxConcurrency has no cap to compute.
func TestTaskSourcePoller_UnboundedProjectAdoptsEverything(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	const kind TaskSourceKind = "cap-unbounded-kind"
	f.register(t, kind, &fakeTaskSource{name: "unbounded", specs: capSpecs(kind, 4)})
	if err := f.poller.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if tasks := f.tasks(t); len(tasks) != 4 {
		t.Fatalf("an unbounded project adopted %d of 4", len(tasks))
	}
}

// TestTaskSourcePoller_CapCountFailureSkipsTheProject: a project whose
// capacity cannot be read is not polled at all.
func TestTaskSourcePoller_CapCountFailureSkipsTheProject(t *testing.T) {
	t.Parallel()
	projects := NewInMemoryProjectStore()
	ctx := context.Background()
	if err := projects.Create(ctx, &Project{
		ID: "proj-flow", TenantID: "tenant-a", Name: "autonomous", Autonomous: true,
		Quotas: ProjectQuotas{MaxConcurrency: 1},
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	registry := NewTaskSourceRegistry()
	src := &fakeTaskSource{name: "never-polled"}
	if err := registry.Register("cap-broken-kind", src); err != nil {
		t.Fatal(err)
	}
	logger := &controlLoopMockLogger{}
	poller, err := NewTaskSourcePoller(TaskSourcePollerConfig{
		Sources: registry, Tenants: &fakeTenantLister{ids: []string{"tenant-a"}},
		Projects: projects, Tasks: &storeBackedTaskCreator{},
		Queue:  NewTaskStore(NewMemoryTaskKV(), NewProcessClaimArbiter()),
		Leases: &failingLeaseLister{err: errors.New("kv down")}, LeaseTTL: testPollerLeaseTTL,
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("NewTaskSourcePoller: %v", err)
	}
	if err := poller.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if polled := src.polledProjects(); len(polled) != 0 {
		t.Fatalf("a project whose capacity is unknown was polled: %v", polled)
	}
	if n := countLines(logger.warnLines(), "reading the adoption capacity failed"); n != 1 {
		t.Fatalf("the capacity failure must be reported: %v", logger.warnLines())
	}
}

// =============================================================================
// An origin comes back when the task holding it is finished with it (#652)
// =============================================================================

// TestTaskSourcePoller_ReAdoptsAnOriginWhoseTaskSpentItsBudget is the burn-in
// failure: a task burned every attempt against a broken dispatcher, and every
// sweep afterwards saw the re-labelled issue and adopted nothing. The next
// sweep must adopt a fresh task, name the one it superseded, and leave the
// spent task where an operator can still read it.
func TestTaskSourcePoller_ReAdoptsAnOriginWhoseTaskSpentItsBudget(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	const kind TaskSourceKind = "poller-budget-tracker"
	f.register(t, kind, &fakeTaskSource{name: "budget-tracker", specs: []TaskSourceSpec{{
		Source: TaskSourceRef{Kind: kind, Ref: "owner/repo#53"}, Subject: "fix dispatch", AttemptBudget: 3,
	}}})
	ctx := context.Background()

	if err := f.poller.Sweep(ctx); err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	first := f.tasks(t)
	if len(first) != 1 {
		t.Fatalf("the first sweep adopts once, got %d tasks", len(first))
	}
	spendTheAttemptBudget(t, f.creator.store, first[0])

	if err := f.poller.Sweep(ctx); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	tasks := f.tasks(t)
	if len(tasks) != 2 {
		t.Fatalf("the sweep after a spent budget must adopt again, got %d tasks", len(tasks))
	}
	var fresh *Task
	for _, task := range tasks {
		if task.ID != first[0].ID {
			fresh = task
		}
	}
	if fresh == nil || fresh.PredecessorTaskID() != first[0].ID {
		t.Fatalf("the new task must name the one it superseded: %+v", fresh)
	}
	if spent, err := f.creator.store.GetTask(ctx, "tenant-a", first[0].ID); err != nil || spent.Status != TaskStatusFailed {
		t.Fatalf("the spent task is kept for history, got %+v err=%v", spent, err)
	}
	if n := countLines(f.logger.infoLines(), "supersedes task"); n != 1 {
		t.Fatalf("the supersession must be logged once, got %d: %v", n, f.logger.infoLines())
	}
	if n := len(f.logger.warnLines()); n != 0 {
		t.Fatalf("re-adoption is not a warning: %v", f.logger.warnLines())
	}
}

// TestTaskSourcePoller_ReAdoptsAnOriginBlockedOnAnEnvironmentFault is the #739
// failure: go-truststrap#54 concluded blocked on no_account_resolver, and once
// the deployment fault was fixed re-labelling the issue must adopt it again.
// A block on a classified environment fault is a transient hold, so the next
// sweep supersedes it, names the class it re-armed at INFO, and leaves the
// blocked task for history — without a warning.
func TestTaskSourcePoller_ReAdoptsAnOriginBlockedOnAnEnvironmentFault(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	const kind TaskSourceKind = "poller-env-block-tracker"
	f.register(t, kind, &fakeTaskSource{name: "env-block-tracker", specs: []TaskSourceSpec{{
		Source: TaskSourceRef{Kind: kind, Ref: "owner/repo#54"}, Subject: "enrol tpm", AttemptBudget: 3,
	}}})
	ctx := context.Background()

	if err := f.poller.Sweep(ctx); err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	first := f.tasks(t)
	if len(first) != 1 {
		t.Fatalf("the first sweep adopts once, got %d tasks", len(first))
	}
	// The run concluded blocked on a resolver fault, the deployment fault was
	// then fixed out of band, and the issue re-labelled.
	first[0].Status = TaskStatusBlocked
	first[0].BlockKind = TaskBlockKindBlocked
	first[0].BlockedReason = "no account resolver was wired"
	first[0].BlockFaultClass = FaultNoAccountResolver
	if err := f.creator.store.UpdateTask(ctx, first[0]); err != nil {
		t.Fatalf("block on the environment fault: %v", err)
	}

	if err := f.poller.Sweep(ctx); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	tasks := f.tasks(t)
	if len(tasks) != 2 {
		t.Fatalf("the sweep after an environment-fault block must adopt again, got %d tasks", len(tasks))
	}
	var fresh *Task
	for _, task := range tasks {
		if task.ID != first[0].ID {
			fresh = task
		}
	}
	if fresh == nil || fresh.PredecessorTaskID() != first[0].ID {
		t.Fatalf("the new task must name the one it superseded: %+v", fresh)
	}
	if fresh.PredecessorBlockReason() != FaultNoAccountResolver {
		t.Fatalf("the new task must record the block class it re-armed, got %q", fresh.PredecessorBlockReason())
	}
	if held, err := f.creator.store.GetTask(ctx, "tenant-a", first[0].ID); err != nil || held.Status != TaskStatusBlocked {
		t.Fatalf("the blocked task is kept for history, got %+v err=%v", held, err)
	}
	if n := countLines(f.logger.infoLines(), "supersedes task"); n != 1 {
		t.Fatalf("the supersession must be logged once, got %d: %v", n, f.logger.infoLines())
	}
	if n := countLines(f.logger.infoLines(), "blocked on %s"); n != 1 {
		t.Fatalf("the supersession must name the block reason it re-armed, got %d: %v", n, f.logger.infoLines())
	}
	if n := len(f.logger.warnLines()); n != 0 {
		t.Fatalf("re-adoption is not a warning: %v", f.logger.warnLines())
	}
}

// TestTaskSourcePoller_HolderBlockedOnNonEnvironmentReasonIsHeld: a block that
// no environment fault produced is a deliberate operator hold and binds its
// source. Re-labelling must not supersede it — the origin stays a debug-level
// non-event, never re-adopted (#739).
func TestTaskSourcePoller_HolderBlockedOnNonEnvironmentReasonIsHeld(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	const kind TaskSourceKind = "poller-operator-hold-tracker"
	f.register(t, kind, &fakeTaskSource{name: "operator-hold-tracker", specs: []TaskSourceSpec{{
		Source: TaskSourceRef{Kind: kind, Ref: "owner/repo#55"}, Subject: "held for review",
	}}})
	ctx := context.Background()

	if err := f.poller.Sweep(ctx); err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	first := f.tasks(t)
	if len(first) != 1 {
		t.Fatalf("the first sweep adopts once, got %d tasks", len(first))
	}
	// A deliberate operator hold: blocked, with a reason but no fault class.
	if err := f.creator.store.BlockTask(ctx, "tenant-a", first[0].ID, TaskBlockKindBlocked, "held pending a policy decision"); err != nil {
		t.Fatalf("operator block: %v", err)
	}

	if err := f.poller.Sweep(ctx); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if tasks := f.tasks(t); len(tasks) != 1 {
		t.Fatalf("a deliberate operator hold must keep its origin, got %d tasks", len(tasks))
	}
	if n := countLines(f.logger.debugLines(), "origin already adopted"); n != 1 {
		t.Fatalf("the second sweep reports the operator hold at debug once, got %d: %v", n, f.logger.debugLines())
	}
	if n := len(f.logger.warnLines()); n != 0 {
		t.Fatalf("an operator hold is not a warning: %v", f.logger.warnLines())
	}
	if n := countLines(f.logger.infoLines(), "supersedes task"); n != 0 {
		t.Fatalf("an operator hold supersedes nothing, got %d: %v", n, f.logger.infoLines())
	}
}

// TestTaskSourcePoller_ALiveHolderIsADebugNonEvent: an origin held by a task
// that can still work it is the ordinary outcome of re-polling. It is counted
// in the sweep summary — the count #652 had no way to read — and reported at
// debug, never as a warning.
func TestTaskSourcePoller_ALiveHolderIsADebugNonEvent(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	const kind TaskSourceKind = "poller-live-holder-tracker"
	f.register(t, kind, &fakeTaskSource{name: "live-holder", specs: []TaskSourceSpec{{
		Source: TaskSourceRef{Kind: kind, Ref: "owner/repo#54"}, Subject: "in flight",
	}}})
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if err := f.poller.Sweep(ctx); err != nil {
			t.Fatalf("sweep %d: %v", i, err)
		}
	}
	if tasks := f.tasks(t); len(tasks) != 1 {
		t.Fatalf("a live holder keeps its origin, got %d tasks", len(tasks))
	}
	if n := countLines(f.logger.debugLines(), "origin already adopted"); n != 1 {
		t.Fatalf("the second sweep reports the holder at debug once, got %d: %v", n, f.logger.debugLines())
	}
	if n := len(f.logger.warnLines()); n != 0 {
		t.Fatalf("a live holder is not a warning: %v", f.logger.warnLines())
	}
	if n := countLines(f.logger.infoLines(), "reserved="); n != 2 {
		t.Fatalf("every sweep summary must count what is reserved, got %d: %v", n, f.logger.infoLines())
	}
}

// TestTaskSourcePoller_WarnsOncePerSweepWhenAHeldOriginCannotBeSuperseded:
// silence is the defect. An origin held by a task that is finished with it
// should have been superseded as the new task was created, so a sweep that is
// refused anyway says which task holds it — once per sweep, not once per
// source polled.
func TestTaskSourcePoller_WarnsOncePerSweepWhenAHeldOriginCannotBeSuperseded(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	f.creator.createErr = &TaskSourceAlreadyAdoptedError{
		TenantID: "tenant-a", ProjectID: "proj-flow", Kind: "poller-stuck-tracker",
		Ref: "owner/repo#53", TaskID: "task-stuck", HolderStatus: TaskStatusFailed,
	}
	const kind TaskSourceKind = "poller-stuck-tracker"
	f.register(t, kind, &fakeTaskSource{name: "stuck", specs: []TaskSourceSpec{{
		Source: TaskSourceRef{Kind: kind, Ref: "owner/repo#53"}, Subject: "re-labelled",
	}}})
	// A second registered source that offers nothing must not add a line.
	f.register(t, "poller-stuck-quiet", &fakeTaskSource{name: "quiet"})
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if err := f.poller.Sweep(ctx); err != nil {
			t.Fatalf("sweep %d: %v", i, err)
		}
	}
	if n := countLines(f.logger.warnLines(), "is held by a task that is finished with it"); n != 2 {
		t.Fatalf("one warning per sweep, got %d: %v", n, f.logger.warnLines())
	}
	if n := countLines(f.logger.debugLines(), "origin already adopted"); n != 0 {
		t.Fatalf("a holder that is finished with the origin is not a debug non-event: %v", f.logger.debugLines())
	}
}
