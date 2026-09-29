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
	"testing"
	"time"
)

// =============================================================================
// A parked project drains: new work is refused, work in flight finishes
// =============================================================================

var parkClock = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

// parkedProject is an autonomous project that has been parked.
func parkedProject(tenantID, projectID string) *Project {
	project := &Project{ID: projectID, TenantID: tenantID, Name: projectID, Autonomous: true}
	project.Park("ops", parkClock)
	return project
}

// finishingRunner is a runner whose unbound jobs finish work already started,
// the way the review and merge-train runners declare themselves.
type finishingRunner struct{ *fakeRunner }

func (finishingRunner) FinishesStartedWork() bool { return true }

func TestProject_ParkAndResume(t *testing.T) {
	t.Parallel()
	project := &Project{ID: "p", TenantID: "t", Autonomous: true}

	project.Park("ops", parkClock)
	if !project.Parked || !project.ParkedAt.Equal(parkClock) || project.ParkedBy != "ops" {
		t.Fatalf("parked record = %v %v %q, want parked at %v by ops", project.Parked, project.ParkedAt, project.ParkedBy, parkClock)
	}

	// A repeated park keeps the original answer to "since when, by whom".
	project.Park("someone-else", parkClock.Add(time.Hour))
	if !project.ParkedAt.Equal(parkClock) || project.ParkedBy != "ops" {
		t.Errorf("a second park rewrote the record: %v by %q", project.ParkedAt, project.ParkedBy)
	}

	project.Resume()
	if project.Parked || !project.ParkedAt.IsZero() || project.ParkedBy != "" {
		t.Errorf("resume left %v %v %q, want an open project with no park record", project.Parked, project.ParkedAt, project.ParkedBy)
	}
	if !project.Autonomous {
		t.Error("resume turned autonomy off; the two switches are independent")
	}
}

func TestProject_ParkAndResumeOnNilAreSafe(t *testing.T) {
	t.Parallel()
	var project *Project
	project.Park("ops", parkClock)
	project.Resume()
	if err := project.RefuseIntake("anything"); err != nil {
		t.Errorf("a nil project refused intake: %v", err)
	}
	if !project.AdmitsTask(&Task{}) {
		t.Error("a nil project refused a task")
	}
}

func TestProject_RefuseIntake(t *testing.T) {
	t.Parallel()
	open := &Project{ID: "p", TenantID: "t"}
	if err := open.RefuseIntake("task x"); err != nil {
		t.Fatalf("an open project refused intake: %v", err)
	}

	err := parkedProject("t", "p").RefuseIntake("task x")
	var parked *ProjectParkedError
	if !errors.As(err, &parked) || !errors.Is(err, ErrProjectParked) {
		t.Fatalf("err = %v, want a *ProjectParkedError carrying ErrProjectParked", err)
	}
	if parked.TenantID != "t" || parked.ProjectID != "p" || parked.ParkedBy != "ops" || !parked.ParkedAt.Equal(parkClock) {
		t.Errorf("refusal = %+v, want it to name the project and its park record", parked)
	}
	for _, want := range []string{"project p", "refused task x", "by ops", "resume the project"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not say %q", err.Error(), want)
		}
	}
}

func TestProjectParkedError_MessageWithoutARecord(t *testing.T) {
	t.Parallel()
	err := &ProjectParkedError{ProjectID: "p"}
	if strings.Contains(err.Error(), "parked 0001") || strings.Contains(err.Error(), " by ") {
		t.Errorf("a refusal with no park record invented one: %q", err.Error())
	}
}

func TestTask_Started(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		task *Task
		want bool
	}{
		"a nil task":                          {nil, false},
		"a task never dispatched":             {&Task{}, false},
		"a task dispatched once":              {&Task{Attempts: 1}, true},
		"a task requeued after two attempts":  {&Task{Attempts: 2, Status: TaskStatusPending}, true},
		"a readmitted task keeps its history": {&Task{Attempts: 1, Status: TaskStatusPending, ConclusionReason: TaskConcludedReadmitted}, true},
	}
	for name, tc := range cases {
		if got := tc.task.Started(); got != tc.want {
			t.Errorf("%s: Started() = %v, want %v", name, got, tc.want)
		}
	}
}

func TestAdmitIntake(t *testing.T) {
	t.Parallel()
	intake := &fakeRunner{name: "intake-runner"}
	finisher := finishingRunner{&fakeRunner{name: "finishing-runner"}}
	open := &Project{ID: "p", TenantID: "t"}
	parked := parkedProject("t", "p")

	cases := map[string]struct {
		project *Project
		runner  JobRunner
		task    *Task
		refused bool
	}{
		"no project":                              {nil, intake, &Task{ID: "x"}, false},
		"an open project, a new task":             {open, nil, &Task{ID: "x"}, false},
		"an open project, an intake job":          {open, intake, nil, false},
		"a parked project, a new task":            {parked, nil, &Task{ID: "x"}, true},
		"a parked project, a started task":        {parked, nil, &Task{ID: "x", Attempts: 1}, false},
		"a parked project, an intake job":         {parked, intake, nil, true},
		"a parked project, a finishing job":       {parked, finisher, nil, false},
		"a parked project, a job with no runner":  {parked, nil, nil, true},
		"a parked project, a finishing new task?": {parked, finisher, &Task{ID: "x"}, true},
	}
	for name, tc := range cases {
		err := admitIntake(tc.project, tc.runner, tc.task)
		if tc.refused != (err != nil) {
			t.Errorf("%s: err = %v, refused want %v", name, err, tc.refused)
			continue
		}
		if tc.refused && !errors.Is(err, ErrProjectParked) {
			t.Errorf("%s: err = %v, want ErrProjectParked", name, err)
		}
	}
}

func TestHoldUnstartedWhileParked(t *testing.T) {
	t.Parallel()
	fresh := &Task{ID: "fresh"}
	started := &Task{ID: "started", Attempts: 1}

	ready, held := holdUnstartedWhileParked(&Project{ID: "p"}, []*Task{fresh, started})
	if len(ready) != 2 || len(held) != 0 {
		t.Fatalf("an open project held %d and kept %d, want nothing held", len(held), len(ready))
	}

	ready, held = holdUnstartedWhileParked(parkedProject("t", "p"), []*Task{fresh, started})
	if len(ready) != 1 || ready[0].ID != "started" {
		t.Fatalf("ready = %v, want only the started task", ready)
	}
	if len(held) != 1 || held[0].task.ID != "fresh" || !strings.Contains(held[0].reason, "project is parked") {
		t.Fatalf("held = %+v, want the fresh task held with the park as its reason", held)
	}
}

// =============================================================================
// The gate on the agent service
// =============================================================================

// newParkedSpawnerService is newSpawnerService over a project store holding
// one parked project.
func newParkedSpawnerService(t *testing.T, tenantID, projectID string) (*agentServiceImpl, TaskStore) {
	t.Helper()
	impl, tasks := newSpawnerService(t, tenantID)
	projects := NewInMemoryProjectStore()
	if err := projects.Create(ContextWithTenant(context.Background(), tenantID), parkedProject(tenantID, projectID)); err != nil {
		t.Fatalf("create project: %v", err)
	}
	impl.SetProjectStore(projects)
	return impl, tasks
}

// blockingRunner is a runner whose jobs stay open until release closes.
func blockingRunner(name string, release <-chan struct{}) *fakeRunner {
	return &fakeRunner{
		name: name,
		run: func(ctx context.Context, _ *JobRun) error {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return nil
		},
	}
}

func TestAgentService_SpawnTask_ParkedProjectRefusesANewTask(t *testing.T) {
	const tenantID, projectID = "tenant-park-new", "7b0c6a2e-1d4f-4c3a-9e2b-5f8d1a0c3e01"
	impl, tasks := newParkedSpawnerService(t, tenantID, projectID)
	ctx := ContextWithTenant(context.Background(), tenantID)
	release := make(chan struct{})
	defer close(release)
	runner := blockingRunner("park-new-runner", release)
	registerJobRunnerForTest(t, runner)
	defer cancelAllJobs(impl)

	task := &Task{ID: "task-new", TenantID: tenantID, ProjectID: projectID, Subject: "new work",
		Status: TaskStatusPending, AutoSpawn: true, Runner: runner.Name()}
	if err := tasks.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	_, err := impl.SpawnTask(ctx, task)
	if !errors.Is(err, ErrProjectParked) {
		t.Fatalf("err = %v, want the park refusal", err)
	}
	stored, err := tasks.GetTask(ctx, tenantID, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stored.Status != TaskStatusPending || stored.Attempts != 0 || stored.SpawnedJobID != "" {
		t.Errorf("refused task = status %q attempts %d job %q, want it queued exactly as it was",
			stored.Status, stored.Attempts, stored.SpawnedJobID)
	}
	if n := countRunningJobs(impl); n != 0 {
		t.Errorf("%d jobs started for refused work", n)
	}
}

func TestAgentService_SpawnTask_ParkedProjectFinishesAStartedTask(t *testing.T) {
	const tenantID, projectID = "tenant-park-started", "7b0c6a2e-1d4f-4c3a-9e2b-5f8d1a0c3e02"
	impl, tasks := newParkedSpawnerService(t, tenantID, projectID)
	ctx := ContextWithTenant(context.Background(), tenantID)
	release := make(chan struct{})
	defer close(release)
	runner := blockingRunner("park-started-runner", release)
	registerJobRunnerForTest(t, runner)
	defer cancelAllJobs(impl)

	// Requeued after one attempt: work in flight.
	task := &Task{ID: "task-started", TenantID: tenantID, ProjectID: projectID, Subject: "in flight",
		Status: TaskStatusPending, AutoSpawn: true, Runner: runner.Name(), Attempts: 1}
	if err := tasks.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	jobID, err := impl.SpawnTask(ctx, task)
	if err != nil {
		t.Fatalf("SpawnTask: %v; a parked project must finish a started task", err)
	}
	if jobID == "" {
		t.Fatal("no job was named for the started task")
	}
}

func TestAgentService_StartJob_ParkedProjectGatesUnboundJobsByRunner(t *testing.T) {
	const tenantID, projectID = "tenant-park-unbound", "7b0c6a2e-1d4f-4c3a-9e2b-5f8d1a0c3e03"
	impl, _ := newParkedSpawnerService(t, tenantID, projectID)
	ctx := ContextWithTenant(context.Background(), tenantID)
	release := make(chan struct{})
	defer close(release)
	intake := blockingRunner("park-intake-runner", release)
	finisher := finishingRunner{blockingRunner("park-finishing-runner", release)}
	registerJobRunnerForTest(t, intake)
	registerJobRunnerForTest(t, finisher)
	defer cancelAllJobs(impl)

	_, err := impl.StartJob(ctx, &StartJobRequest{TenantID: tenantID, ProjectID: projectID, Runner: intake.Name(), Description: "triage"})
	var parked *ProjectParkedError
	if !errors.As(err, &parked) || parked.ProjectID != projectID {
		t.Fatalf("intake job: err = %v, want a *ProjectParkedError for %s", err, projectID)
	}

	resp, err := impl.StartJob(ctx, &StartJobRequest{TenantID: tenantID, ProjectID: projectID, Runner: finisher.Name(), Description: "review"})
	if err != nil || resp.JobID == "" {
		t.Fatalf("finishing job: resp = %v err = %v, want it started on a parked project", resp, err)
	}
}

func TestAgentService_StartJob_ParkedProjectRefusesABoundNewTask(t *testing.T) {
	const tenantID, projectID = "tenant-park-bound", "7b0c6a2e-1d4f-4c3a-9e2b-5f8d1a0c3e04"
	impl, tasks := newParkedSpawnerService(t, tenantID, projectID)
	ctx := ContextWithTenant(context.Background(), tenantID)
	release := make(chan struct{})
	defer close(release)
	runner := blockingRunner("park-bound-runner", release)
	registerJobRunnerForTest(t, runner)
	defer cancelAllJobs(impl)

	task := &Task{ID: "task-bound", TenantID: tenantID, ProjectID: projectID, Subject: "new",
		Status: TaskStatusPending, Runner: runner.Name()}
	if err := tasks.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	_, err := impl.StartJob(ctx, &StartJobRequest{TenantID: tenantID, ProjectID: projectID, TaskID: task.ID, Runner: runner.Name(), Description: "x"})
	if !errors.Is(err, ErrProjectParked) {
		t.Fatalf("err = %v, want the park refusal", err)
	}
	stored, err := tasks.GetTask(ctx, tenantID, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stored.Attempts != 0 || stored.SpawnedJobID != "" {
		t.Errorf("a refused start charged the task: attempts %d job %q", stored.Attempts, stored.SpawnedJobID)
	}
}

func TestAgentService_StartJob_ParkGateSurfacesAStoreThatCannotAnswer(t *testing.T) {
	const tenantID = "tenant-park-store"
	impl, _ := newSpawnerService(t, tenantID)
	cause := errors.New("the project store is unreachable")
	impl.SetProjectStore(&failingProjects{ProjectStore: NewInMemoryProjectStore(), err: cause})
	intake := &fakeRunner{name: "park-store-runner"}
	registerJobRunnerForTest(t, intake)

	_, err := impl.StartJob(ContextWithTenant(context.Background(), tenantID),
		&StartJobRequest{TenantID: tenantID, ProjectID: "7b0c6a2e-1d4f-4c3a-9e2b-5f8d1a0c3e05", Runner: intake.Name(), Description: "x"})
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v, want the store's own failure: a gate that cannot read the switch must not guess", err)
	}
}

// =============================================================================
// The sweeps
// =============================================================================

func TestTaskDispatcher_ParkedProjectDispatchesOnlyStartedTasks(t *testing.T) {
	t.Parallel()
	started := pendingTask("t-started", 0, 100)
	started.Attempts = 1
	f := newDispatcherFixture(t, 0, pendingTask("t-new", 0, 50), started)
	ctx := ContextWithTenant(context.Background(), dispatchTenant)
	project, err := f.projects.Get(ctx, dispatchTenant, dispatchProject)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	project.Park("ops", parkClock)
	if err := f.projects.Update(ctx, project); err != nil {
		t.Fatalf("park project: %v", err)
	}

	if err := f.dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := f.spawner.dispatched(); len(got) != 1 || got[0] != "t-started" {
		t.Fatalf("dispatched = %v, want only the started task", got)
	}
	if reason := f.tasks.reason("t-new"); !strings.Contains(reason, "project is parked") {
		t.Errorf("the held task's reason = %q, want the park", reason)
	}

	// Resume: the next sweep takes the queued task up.
	project, err = f.projects.Get(ctx, dispatchTenant, dispatchProject)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	project.Resume()
	if err := f.projects.Update(ctx, project); err != nil {
		t.Fatalf("resume project: %v", err)
	}
	if err := f.dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	// The fake spawner writes nothing back, so the started task is offered
	// again too; what matters is that the queued one is now taken up.
	if got := f.spawner.dispatched(); !containsString(got[1:], "t-new") {
		t.Fatalf("dispatched after resume = %v, want the queued task picked up", got)
	}
}

func TestTaskSourcePoller_ParkedProjectAdoptsNothing(t *testing.T) {
	t.Parallel()
	f := newPollerFixture(t)
	src := &fakeTaskSource{name: "tracker", specs: []TaskSourceSpec{{
		Source: TaskSourceRef{Kind: "tracker-park", Ref: "1"}, Subject: "new issue",
	}}}
	f.register(t, "tracker-park", src)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	project, err := f.projects.Get(ctx, "tenant-a", "proj-flow")
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	project.Park("ops", parkClock)
	if err := f.projects.Update(ctx, project); err != nil {
		t.Fatalf("park project: %v", err)
	}

	if err := f.poller.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	err = f.poller.SweepProject(context.Background(), "tenant-a", "proj-flow")
	var parked *ProjectParkedError
	if !errors.As(err, &parked) {
		t.Fatalf("SweepProject err = %v, want a *ProjectParkedError", err)
	}
	src.mu.Lock()
	polled := len(src.polled)
	src.mu.Unlock()
	if polled != 0 {
		t.Errorf("the source was polled %d times for a parked project", polled)
	}
	if tasks := f.tasks(t); len(tasks) != 0 {
		t.Errorf("%d tasks adopted for a parked project", len(tasks))
	}
}

func TestAdmission_ParkedProjectAsksOnlyForStartedWork(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	parked := parkedProject(admissionTenant, "proj-parked")
	parked.Quotas.MaxConcurrency = 4
	rig.projectWith(t, parked)
	rig.queued(admissionTenant, "proj-parked", 3)
	started := rig.queue.tasks["proj-parked-task-0"]
	started.Attempts = 1

	plan, err := rig.controller(t, 4).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got := plan.For(admissionTenant, "proj-parked").ProjectPending; got != 1 {
		t.Errorf("a parked project's pending = %d, want only its one started task", got)
	}
}

func TestAgentService_SpawnChildJob_ParkedProjectAdmitsAChildOfWorkInFlight(t *testing.T) {
	const tenantID, projectID = "tenant-park-child", "7b0c6a2e-1d4f-4c3a-9e2b-5f8d1a0c3e06"
	impl, _ := newParkedSpawnerService(t, tenantID, projectID)
	ctx := ContextWithTenant(context.Background(), tenantID)
	release := make(chan struct{})
	defer close(release)
	finisher := finishingRunner{blockingRunner("park-parent-runner", release)}
	registerJobRunnerForTest(t, finisher)
	defer cancelAllJobs(impl)

	parent, err := impl.StartJob(ctx, &StartJobRequest{TenantID: tenantID, ProjectID: projectID, Runner: finisher.Name(), Description: "parent"})
	if err != nil {
		t.Fatalf("parent StartJob: %v", err)
	}
	_, err = impl.SpawnChildJob(ctx, &SpawnChildJobRequest{ParentJobID: parent.JobID, ProjectID: projectID})
	if errors.Is(err, ErrProjectParked) {
		t.Fatalf("a child of a running job was refused by the park: %v", err)
	}
	if err != nil {
		t.Fatalf("SpawnChildJob: %v", err)
	}
}
