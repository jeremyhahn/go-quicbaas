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
	"testing"
)

// A project's id is the UUID minted by pkg/service.CreateProject, and
// CreateTask refuses anything else (#613), so these fixtures are real ones.
const (
	testProjectID             = "1f1a1e46-0c1e-4a7e-9a5f-2b0b9c3d4e01"
	testProjectWithWorkflowID = "1f1a1e46-0c1e-4a7e-9a5f-2b0b9c3d4e02"
	testProjectPlainID        = "1f1a1e46-0c1e-4a7e-9a5f-2b0b9c3d4e03"
	testProjectAnyID          = "1f1a1e46-0c1e-4a7e-9a5f-2b0b9c3d4e04"
	testProjectMissingID      = "1f1a1e46-0c1e-4a7e-9a5f-2b0b9c3d4e05"
)

// =============================================================================
// BlockTask / ReadmitTask on the service
// =============================================================================

func newTaskServiceWithStore(t *testing.T, store TaskStore) (AgentService, *agentServiceImpl) {
	t.Helper()
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	if store != nil {
		impl.SetTaskStore(store)
	}
	return svc, impl
}

func TestAgentService_BlockTask_CallsStore(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	svc, _ := newTaskServiceWithStore(t, store)

	err := svc.BlockTask(context.Background(), "tenant-1", "task-1", TaskBlockKindNeedsInfo, "which branch?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.blockCalls) != 1 {
		t.Fatalf("expected 1 BlockTask call, got %d", len(store.blockCalls))
	}
	call := store.blockCalls[0]
	if call.TenantID != "tenant-1" || call.TaskID != "task-1" || call.BlockKind != TaskBlockKindNeedsInfo || call.Reason != "which branch?" {
		t.Fatalf("store received %+v", call)
	}
}

func TestAgentService_BlockTask_Errors(t *testing.T) {
	t.Parallel()

	t.Run("no task store", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTaskServiceWithStore(t, nil)
		err := svc.BlockTask(context.Background(), "tenant-1", "task-1", TaskBlockKindBlocked, "r")
		if !errors.Is(err, ErrTaskStoreRequired) {
			t.Fatalf("expected ErrTaskStoreRequired, got %v", err)
		}
	})
	t.Run("missing tenant", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTaskServiceWithStore(t, &mockTaskStore{})
		err := svc.BlockTask(context.Background(), "", "task-1", TaskBlockKindBlocked, "r")
		if !errors.Is(err, ErrTenantIDRequired) {
			t.Fatalf("expected ErrTenantIDRequired, got %v", err)
		}
	})
	t.Run("missing task id", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTaskServiceWithStore(t, &mockTaskStore{})
		err := svc.BlockTask(context.Background(), "tenant-1", "", TaskBlockKindBlocked, "r")
		if !errors.Is(err, ErrTaskIDRequired) {
			t.Fatalf("expected ErrTaskIDRequired, got %v", err)
		}
	})
	t.Run("store error propagates", func(t *testing.T) {
		t.Parallel()
		store := &mockTaskStore{blockErr: &TaskInvalidStateError{TaskID: "task-1", CurrentState: TaskStatusCompleted, TargetState: TaskStatusBlocked}}
		svc, _ := newTaskServiceWithStore(t, store)
		err := svc.BlockTask(context.Background(), "tenant-1", "task-1", TaskBlockKindBlocked, "r")
		if !errors.Is(err, ErrTaskInvalidState) {
			t.Fatalf("expected ErrTaskInvalidState, got %v", err)
		}
	})
	t.Run("real store validates kind", func(t *testing.T) {
		t.Parallel()
		store := NewInMemoryTaskStore()
		svc, _ := newTaskServiceWithStore(t, store)
		task, err := svc.CreateTask(context.Background(), &CreateTaskRequest{TenantID: "tenant-1", Subject: "s"})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		err = svc.BlockTask(context.Background(), "tenant-1", task.ID, TaskBlockKind("nope"), "r")
		var invalid *InvalidTaskBlockKindError
		if !errors.As(err, &invalid) {
			t.Fatalf("expected InvalidTaskBlockKindError, got %v", err)
		}
	})
}

func TestAgentService_ReadmitTask_CallsStore(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	svc, _ := newTaskServiceWithStore(t, store)

	if err := svc.ReadmitTask(context.Background(), "tenant-1", "task-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.readmitCalls) != 1 {
		t.Fatalf("expected 1 ReadmitTask call, got %d", len(store.readmitCalls))
	}
	if call := store.readmitCalls[0]; call.TenantID != "tenant-1" || call.TaskID != "task-1" {
		t.Fatalf("store received %+v", call)
	}
}

func TestAgentService_ReadmitTask_Errors(t *testing.T) {
	t.Parallel()

	t.Run("no task store", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTaskServiceWithStore(t, nil)
		if err := svc.ReadmitTask(context.Background(), "tenant-1", "task-1"); !errors.Is(err, ErrTaskStoreRequired) {
			t.Fatalf("expected ErrTaskStoreRequired, got %v", err)
		}
	})
	t.Run("missing tenant", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTaskServiceWithStore(t, &mockTaskStore{})
		if err := svc.ReadmitTask(context.Background(), "", "task-1"); !errors.Is(err, ErrTenantIDRequired) {
			t.Fatalf("expected ErrTenantIDRequired, got %v", err)
		}
	})
	t.Run("missing task id", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTaskServiceWithStore(t, &mockTaskStore{})
		if err := svc.ReadmitTask(context.Background(), "tenant-1", ""); !errors.Is(err, ErrTaskIDRequired) {
			t.Fatalf("expected ErrTaskIDRequired, got %v", err)
		}
	})
	t.Run("store error propagates", func(t *testing.T) {
		t.Parallel()
		store := &mockTaskStore{readmitErr: &TaskNotFoundError{TenantID: "tenant-1", TaskID: "task-1"}}
		svc, _ := newTaskServiceWithStore(t, store)
		if err := svc.ReadmitTask(context.Background(), "tenant-1", "task-1"); !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("expected ErrTaskNotFound, got %v", err)
		}
	})
}

func TestAgentService_BlockThenReadmit_EndToEnd(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	svc, _ := newTaskServiceWithStore(t, store)
	ctx := context.Background()

	task, err := svc.CreateTask(ctx, &CreateTaskRequest{TenantID: "tenant-1", Subject: "s", ProjectID: testProjectID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.BlockTask(ctx, "tenant-1", task.ID, TaskBlockKindEscalated, "needs a decision"); err != nil {
		t.Fatalf("block: %v", err)
	}
	blocked, err := svc.ListTasks(ctx, "tenant-1", &TaskFilter{ProjectID: testProjectID, Statuses: []TaskStatus{TaskStatusBlocked}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(blocked) != 1 || blocked[0].BlockedReason != "needs a decision" {
		t.Fatalf("blocked listing = %+v", blocked)
	}
	if err := svc.ReadmitTask(ctx, "tenant-1", task.ID); err != nil {
		t.Fatalf("readmit: %v", err)
	}
	got, err := svc.GetTask(ctx, "tenant-1", task.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != TaskStatusPending || got.BlockKind != "" {
		t.Fatalf("after readmit: %+v", got)
	}
}

// =============================================================================
// CreateTask carries the new fields; auto-spawn binds the job to the task
// =============================================================================

func TestCreateTask_CopiesSourcePriorityAndBudget(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	svc, _ := newTaskServiceWithStore(t, store)

	source := &TaskSourceRef{Kind: TaskSourceKindOperator, Ref: "42", URL: "https://example.invalid/42"}
	task, err := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:      "tenant-1",
		Subject:       "s",
		Source:        source,
		Priority:      7,
		AttemptBudget: 5,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if task.Source == nil || *task.Source != *source {
		t.Errorf("source not copied: %+v", task.Source)
	}
	if task.Priority != 7 || task.AttemptBudget != 5 {
		t.Errorf("priority/budget = %d/%d", task.Priority, task.AttemptBudget)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.createCalls) != 1 {
		t.Fatalf("expected 1 create, got %d", len(store.createCalls))
	}
	stored := store.createCalls[0].Task
	if stored.Source == nil || stored.Source.Ref != "42" || stored.Priority != 7 || stored.AttemptBudget != 5 {
		t.Errorf("stored task = %+v", stored)
	}
}

func TestCreateTask_InvalidSourceRefusedBeforeStore(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	svc, _ := newTaskServiceWithStore(t, store)

	_, err := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID: "tenant-1",
		Subject:  "s",
		Source:   &TaskSourceRef{Kind: "bogus", Ref: "x"},
	})
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.createCalls) != 0 {
		t.Fatalf("invalid source must be refused before the store is asked, got %d creates", len(store.createCalls))
	}
}

func TestCreateTask_AutoSpawn_BindsJobToTaskAndProject(t *testing.T) {
	t.Parallel()

	taskStore := NewInMemoryTaskStore()
	registry := NewInMemoryAgentRegistry()
	if err := registry.RegisterAgent(context.Background(), &AgentRegistration{
		ID: "agent-idle", TenantID: "tenant-1", Name: "worker", Host: "host", Backend: "ollama", AgentType: "worker", Status: AgentStatusIdle,
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	svc, impl := newTaskServiceWithStore(t, taskStore)
	impl.SetAgentRegistry(registry)
	impl.backends.Store("ollama", &testMinimalBackend{})

	task, err := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:  "tenant-1",
		Subject:   "fix bug",
		ProjectID: testProjectID,
		AutoSpawn: true,
	})
	if err != nil {
		t.Fatalf("create with auto-spawn: %v", err)
	}
	if task.SpawnedJobID == "" {
		t.Fatal("expected a spawned job")
	}
	if task.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1 after the first dispatch", task.Attempts)
	}

	status, err := svc.GetJobStatus(context.Background(), "tenant-1", task.SpawnedJobID)
	if err != nil {
		t.Fatalf("job status: %v", err)
	}
	if status.Config == nil {
		t.Fatal("job status carries no config")
	}
	if status.Config.ProjectID != testProjectID {
		t.Errorf("job ProjectID = %q, want proj-1", status.Config.ProjectID)
	}
	if status.Config.TaskID != task.ID || status.Config.Attempt != 1 {
		t.Errorf("job TaskID/Attempt = %q/%d, want %q/1", status.Config.TaskID, status.Config.Attempt, task.ID)
	}
	if status.TaskID != task.ID || status.Attempt != 1 {
		t.Errorf("status TaskID/Attempt = %q/%d, want %q/1", status.TaskID, status.Attempt, task.ID)
	}

	stored, err := taskStore.GetTask(context.Background(), "tenant-1", task.ID)
	if err != nil {
		t.Fatalf("get stored task: %v", err)
	}
	if stored.Attempts != 1 || stored.SpawnedJobID != task.SpawnedJobID {
		t.Errorf("stored task Attempts/SpawnedJobID = %d/%q", stored.Attempts, stored.SpawnedJobID)
	}
}

func TestStartJob_CopiesTaskBindingIntoConfig(t *testing.T) {
	t.Parallel()

	svc, impl := newTaskServiceWithStore(t, nil)
	impl.backends.Store("ollama", &testMinimalBackend{})

	resp, err := svc.StartJob(context.Background(), &StartJobRequest{
		TenantID:    "tenant-1",
		Description: "attempt three",
		LeadBackend: "ollama",
		TaskID:      "task-77",
		Attempt:     3,
	})
	if err != nil {
		t.Fatalf("start job: %v", err)
	}
	status, err := svc.GetJobStatus(context.Background(), "tenant-1", resp.JobID)
	if err != nil {
		t.Fatalf("job status: %v", err)
	}
	if status.Config == nil || status.Config.TaskID != "task-77" || status.Config.Attempt != 3 {
		t.Fatalf("config = %+v", status.Config)
	}
}

// =============================================================================
// JobStatus session ids and JobEvent type
// =============================================================================

func TestControlLoop_UpdateStatus_PopulatesSessionIDs(t *testing.T) {
	t.Parallel()

	cl := NewControlLoop(&controlLoopMockService{}, &controlLoopMockLogger{})
	cl.job.JobID = "job-1"
	cl.job.Config = &JobConfig{Description: "d", TaskID: "task-1", Attempt: 2}
	cl.leadSessionID = "lead-s"
	cl.reviewerSessionID = "review-s"
	cl.workerSessions["zeta"] = "w-zeta"
	cl.workerSessions["alpha"] = "w-alpha"
	cl.workerSessions["empty"] = ""

	cl.updateStatus(JobStateExecuting)
	status := cl.Status()
	if status == nil {
		t.Fatal("no status")
	}
	if status.LeadSessionID != "lead-s" {
		t.Errorf("LeadSessionID = %q", status.LeadSessionID)
	}
	want := []string{"lead-s", "review-s", "w-alpha", "w-zeta"}
	if len(status.SessionIDs) != len(want) {
		t.Fatalf("SessionIDs = %v, want %v", status.SessionIDs, want)
	}
	for i := range want {
		if status.SessionIDs[i] != want[i] {
			t.Fatalf("SessionIDs = %v, want %v", status.SessionIDs, want)
		}
	}
	if status.TaskID != "task-1" || status.Attempt != 2 {
		t.Errorf("TaskID/Attempt = %q/%d", status.TaskID, status.Attempt)
	}
}

func TestControlLoop_SessionIDs_EmptyWhenNoneSpawned(t *testing.T) {
	t.Parallel()

	cl := NewControlLoop(&controlLoopMockService{}, &controlLoopMockLogger{})
	cl.job.Config = &JobConfig{Description: "d"}
	cl.updateStatus(JobStateInitializing)
	status := cl.Status()
	if status.SessionIDs != nil || status.LeadSessionID != "" {
		t.Fatalf("expected no sessions, got %v / %q", status.SessionIDs, status.LeadSessionID)
	}
}

func TestControlLoop_EmitEvent_TypeIsStateChange(t *testing.T) {
	t.Parallel()

	cl := NewControlLoop(&controlLoopMockService{}, &controlLoopMockLogger{})
	cl.job.JobID = "job-1"
	cl.emitEvent(JobStateExecuting, "assigned")

	select {
	case ev := <-cl.job.events:
		if ev.Type != JobEventTypeStateChange {
			t.Fatalf("Type = %q, want %q", ev.Type, JobEventTypeStateChange)
		}
		if ev.JobID != "job-1" || ev.State != JobStateExecuting || ev.Message != "assigned" {
			t.Fatalf("event = %+v", ev)
		}
	default:
		t.Fatal("no event emitted")
	}
}

// =============================================================================
// SessionRecord.JobID round-trips
// =============================================================================

func TestSessionRecord_JobID_RoundTrip(t *testing.T) {
	t.Parallel()

	record := newDurableSessionRecord(
		&SpawnSessionRequest{Backend: "mock", ProjectID: testProjectID, JobID: "job-9"},
		"tenant-1", "/work", "m", SandboxModeNone, TrustLevelDefault, "", true,
	)
	if record.JobID != "job-9" {
		t.Fatalf("record JobID = %q", record.JobID)
	}
	record.SessionID = "session-1"

	store := NewInMemoryAgentStore()
	if err := store.PutSession(context.Background(), record); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := store.GetSession(context.Background(), "tenant-1", "session-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.JobID != "job-9" {
		t.Fatalf("stored JobID = %q, want job-9", got.JobID)
	}
	if got.Clone().JobID != "job-9" {
		t.Fatal("Clone dropped JobID")
	}

	unbound := newDurableSessionRecord(&SpawnSessionRequest{Backend: "mock"}, "tenant-1", "/work", "m", SandboxModeNone, TrustLevelDefault, "", true)
	if unbound.JobID != "" {
		t.Fatalf("a session spawned outside a job must carry no JobID, got %q", unbound.JobID)
	}
}

// =============================================================================
// autoSpawnTask runs the project's default workflow
// =============================================================================

func autoSpawnFixture(t *testing.T) (AgentService, *agentServiceImpl, ProjectStore) {
	t.Helper()
	taskStore := NewInMemoryTaskStore()
	registry := NewInMemoryAgentRegistry()
	if err := registry.RegisterAgent(context.Background(), &AgentRegistration{
		ID: "agent-idle", TenantID: "tenant-1", Name: "worker", Host: "host", Backend: "ollama", AgentType: "worker", Status: AgentStatusIdle,
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	svc, impl := newTaskServiceWithStore(t, taskStore)
	impl.SetAgentRegistry(registry)
	impl.backends.Store("ollama", &testMinimalBackend{})
	projects := NewInMemoryProjectStore()
	impl.SetProjectStore(projects)
	return svc, impl, projects
}

func TestCreateTask_AutoSpawn_RunsTheProjectDefaultWorkflow(t *testing.T) {
	t.Parallel()

	svc, _, projects := autoSpawnFixture(t)
	if err := projects.Create(context.Background(), &Project{
		ID: testProjectWithWorkflowID, TenantID: "tenant-1", Name: "with-default", DefaultWorkflow: "review-flow",
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	task, err := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID: "tenant-1", Subject: "fix bug", ProjectID: testProjectWithWorkflowID, AutoSpawn: true,
	})
	if err != nil {
		t.Fatalf("create with auto-spawn: %v", err)
	}
	status, err := svc.GetJobStatus(context.Background(), "tenant-1", task.SpawnedJobID)
	if err != nil {
		t.Fatalf("job status: %v", err)
	}
	if status.Config == nil || status.Config.WorkflowName != "review-flow" {
		t.Fatalf("job WorkflowName = %+v, want review-flow from Project.DefaultWorkflow", status.Config)
	}
}

func TestCreateTask_AutoSpawn_NoDefaultWorkflowLeavesTheNameEmpty(t *testing.T) {
	t.Parallel()

	svc, _, projects := autoSpawnFixture(t)
	if err := projects.Create(context.Background(), &Project{ID: testProjectPlainID, TenantID: "tenant-1", Name: "plain"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	for _, projectID := range []string{testProjectPlainID, "", testProjectMissingID} {
		task, err := svc.CreateTask(context.Background(), &CreateTaskRequest{
			TenantID: "tenant-1", Subject: "fix bug " + projectID, ProjectID: projectID, AutoSpawn: true,
		})
		if err != nil {
			t.Fatalf("project %q: create with auto-spawn: %v", projectID, err)
		}
		status, err := svc.GetJobStatus(context.Background(), "tenant-1", task.SpawnedJobID)
		if err != nil {
			t.Fatalf("project %q: job status: %v", projectID, err)
		}
		if status.Config == nil || status.Config.WorkflowName != "" {
			t.Errorf("project %q: WorkflowName = %+v, want empty", projectID, status.Config)
		}
	}
}

func TestCreateTask_AutoSpawn_ProjectStoreOutageRefusesTheSpawn(t *testing.T) {
	t.Parallel()

	svc, impl, _ := autoSpawnFixture(t)
	impl.SetProjectStore(&failingProjectStore{err: errors.New("store down")})

	task, err := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID: "tenant-1", Subject: "fix bug", ProjectID: testProjectAnyID, AutoSpawn: true,
	})
	if err == nil || err.Error() != "store down" {
		t.Fatalf("expected the store outage to refuse the spawn, got %v", err)
	}
	if task == nil || task.SpawnedJobID != "" {
		t.Fatalf("the task must be persisted without a job, got %+v", task)
	}
}

// failingProjectStore answers every read with one error.
type failingProjectStore struct {
	ProjectStore
	err error
}

func (f *failingProjectStore) Get(context.Context, string, string) (*Project, error) {
	return nil, f.err
}

// TestCreateTask_RefusesAProjectIDThatIsNotAnID is the regression test for the
// second half of #613.
//
// CreateTask copied req.ProjectID onto the record unexamined. A project's ID
// is a UUID minted at creation (pkg/service.CreateProject), and its NAME is
// free text, so a caller passing the name got a task record scoped to a
// project that does not exist and a job lease keyed under that string —
// silently, because nothing downstream re-checks it: a project listing filters
// on the id, the project's concurrency quota counts leases under the id's
// subtree, and neither finds the task.
//
// The refusal is at CREATE because that is the last point at which the caller
// is still there to be told.
func TestCreateTask_RefusesAProjectIDThatIsNotAnID(t *testing.T) {
	t.Parallel()

	refused := []struct{ name, projectID string }{
		{"the project's name, the shape #613 was filed for", "go-quicbaas"},
		{"a path", "/workspace/go-quicbaas"},
		{"a uuid with a character changed", "6f9619ff-8b86-d011-b42d-00c04fc964fg"},
		{"a uuid missing a group", "6f9619ff-8b86-d011-00c04fc964ff"},
		{"whitespace around an otherwise valid id", " 6f9619ff-8b86-d011-b42d-00c04fc964ff "},
	}
	for _, tc := range refused {
		t.Run("refused: "+tc.name, func(t *testing.T) {
			t.Parallel()
			store := &mockTaskStore{}
			svc, _ := newTaskServiceWithStore(t, store)

			_, err := svc.CreateTask(context.Background(), &CreateTaskRequest{
				TenantID:  "tenant-1",
				Subject:   "s",
				ProjectID: tc.projectID,
			})
			var invalid *InvalidProjectIDError
			if !errors.As(err, &invalid) || !errors.Is(err, ErrInvalidProjectID) {
				t.Fatalf("got %v, want InvalidProjectIDError", err)
			}
			if invalid.ProjectID != tc.projectID {
				t.Errorf("the refusal must name the value: %q", invalid.ProjectID)
			}
			store.mu.Lock()
			defer store.mu.Unlock()
			if len(store.createCalls) != 0 {
				t.Fatalf("a bad project must be refused before the store is asked, got %d creates", len(store.createCalls))
			}
		})
	}

	// The other direction, so the guard cannot pass by refusing everything.
	accepted := []struct{ name, projectID string }{
		{"unset, an unscoped task", ""},
		{"a canonical lower-case uuid", "6f9619ff-8b86-d011-b42d-00c04fc964ff"},
		{"an upper-case uuid, which is the same id", "6F9619FF-8B86-D011-B42D-00C04FC964FF"},
	}
	for _, tc := range accepted {
		t.Run("accepted: "+tc.name, func(t *testing.T) {
			t.Parallel()
			store := &mockTaskStore{}
			svc, _ := newTaskServiceWithStore(t, store)

			task, err := svc.CreateTask(context.Background(), &CreateTaskRequest{
				TenantID:  "tenant-1",
				Subject:   "s",
				ProjectID: tc.projectID,
			})
			if err != nil {
				t.Fatalf("CreateTask: %v", err)
			}
			if task.ProjectID != tc.projectID {
				t.Fatalf("project = %q, want %q verbatim", task.ProjectID, tc.projectID)
			}
		})
	}
}
