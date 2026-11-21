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
	"encoding/json"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// The faceted narrowings, end to end through the paged verbs
//
// These exercise what the transports send: a listing narrowed by a field the
// RECORD DOES NOT CARRY (the agent of a task's latest attempt), counted over
// the set the narrowing removes from, and cut into a page.
// =============================================================================

// pagedServiceWithJobs builds a service whose persisted job records are the
// ones given, so a listing that resolves a job — a task's agent, a session's
// role — has something real to resolve against.
func pagedServiceWithJobs(t *testing.T, tenantID string, jobs ...*JobStatus) *agentServiceImpl {
	t.Helper()

	svc := pagedTestService(t)
	kv := kvstore.NewMemory()
	svc.SetKVStore(kv)

	for _, job := range jobs {
		encoded, err := json.Marshal(job)
		if err != nil {
			t.Fatalf("Marshal(job %s) error = %v", job.JobID, err)
		}
		if err := kv.Put(context.Background(), jobPersistenceKey(tenantID, job.JobID), encoded); err != nil {
			t.Fatalf("Put(job %s) error = %v", job.JobID, err)
		}
	}
	return svc
}

// TestListTasksPage_NarrowsOnTheLatestAttemptsAgent is the owner's ask made
// mechanical: "show me what agent-1 is working on" over records that name no
// agent at all.
func TestListTasksPage_NarrowsOnTheLatestAttemptsAgent(t *testing.T) {
	t.Parallel()

	svc := pagedServiceWithJobs(t, "tenant-1",
		&JobStatus{JobID: "job-1", AgentID: "agent-1", Runner: "sdlc"},
		&JobStatus{JobID: "job-2", AgentID: "agent-2", Runner: "sdlc"},
	)
	svc.SetTaskStore(&mockTaskStore{listTasks: []*Task{
		{ID: "task-1", Subject: "one", Status: TaskStatusInProgress, SpawnedJobID: "job-1", CreatedAt: 1},
		{ID: "task-2", Subject: "two", Status: TaskStatusInProgress, SpawnedJobID: "job-2", CreatedAt: 2},
		{ID: "task-3", Subject: "three", Status: TaskStatusPending, CreatedAt: 3},
	}})

	page, err := svc.ListTasksPage(context.Background(), "tenant-1",
		&TaskFilter{AgentID: "agent-1"}, &Page{})
	if err != nil {
		t.Fatalf("ListTasksPage() error = %v, want nil", err)
	}
	if page.Total != 1 || page.Items[0].ID != "task-1" {
		t.Fatalf("matched %d tasks (%v), want only task-1", page.Total, page.Items)
	}
	// The resolved agent travels on the record, so the UI's column and the
	// filter read the same value.
	if page.Items[0].AgentID != "agent-1" {
		t.Errorf("task.agent_id = %q, want agent-1", page.Items[0].AgentID)
	}
	// The counts see the task the narrowing removed — selecting agent-2
	// instead would yield it — and do not count the task that has no agent.
	if page.Facets["agent_id"]["agent-2"] != 1 {
		t.Errorf("agent facet = %v, want agent-2 counted", page.Facets["agent_id"])
	}
	if _, counted := page.Facets["agent_id"][""]; counted {
		t.Error("the task with no attempt was counted under an empty agent")
	}
}

// TestListTasksPage_NarrowsOnTheLatestAttemptsJob proves the reverse binding —
// "which task is this job an attempt at" — resolves through the OPEN attempt
// rather than through the whole history.
func TestListTasksPage_NarrowsOnTheLatestAttemptsJob(t *testing.T) {
	t.Parallel()

	// THE REAL STORE, because this narrowing is the STORE's: `job_id` reads a
	// value derived from the record (LatestAttemptJobID), so it is applied
	// where the record is, and a double that returned every task regardless
	// would prove nothing about it.
	svc := pagedTestService(t)
	store := NewTaskStore(NewMemoryTaskKV(), NewProcessClaimArbiter())
	svc.SetTaskStore(store)
	ctx := ContextWithTenant(context.Background(), "tenant-1")
	seed := []*Task{
		{ID: "task-1", TenantID: "tenant-1", Subject: "one", Status: TaskStatusInProgress, SpawnedJobID: "job-2", CreatedAt: 1,
			Metadata: map[string]string{attemptHistoryKey: "job-1"}},
		{ID: "task-2", TenantID: "tenant-1", Subject: "two", Status: TaskStatusInProgress, SpawnedJobID: "job-3", CreatedAt: 2},
	}
	for _, task := range seed {
		if err := store.CreateTask(ctx, task); err != nil {
			t.Fatalf("CreateTask(%s) error = %v", task.ID, err)
		}
	}

	current, err := svc.ListTasksPage(ctx, "tenant-1", &TaskFilter{JobID: "job-2"}, &Page{})
	if err != nil {
		t.Fatalf("ListTasksPage() error = %v, want nil", err)
	}
	if current.Total != 1 || current.Items[0].ID != "task-1" {
		t.Errorf("job-2 matched %v, want task-1", current.Items)
	}

	// job-1 is task-1's CONCLUDED attempt, and the binding is the latest one:
	// answering with task-1 here would tell a caller the task is running as a
	// job it has already finished with.
	concluded, err := svc.ListTasksPage(ctx, "tenant-1", &TaskFilter{JobID: "job-1"}, &Page{})
	if err != nil {
		t.Fatalf("ListTasksPage() error = %v, want nil", err)
	}
	if concluded.Total != 0 {
		t.Errorf("job-1 matched %v, want nothing: it is not any task's latest attempt", concluded.Items)
	}
}

// TestListTasksPage_FiltersComposeWithSearchAndPaging proves the three
// narrowings stack rather than replace one another, and that Total counts the
// intersection rather than the page.
func TestListTasksPage_FiltersComposeWithSearchAndPaging(t *testing.T) {
	t.Parallel()

	tasks := []*Task{
		{ID: "task-1", Subject: "wire the filter bar", Status: TaskStatusBlocked, ProjectID: "proj-a", CreatedAt: 1},
		{ID: "task-2", Subject: "wire the filter bar", Status: TaskStatusBlocked, ProjectID: "proj-a", CreatedAt: 2},
		{ID: "task-3", Subject: "wire the filter bar", Status: TaskStatusBlocked, ProjectID: "proj-b", CreatedAt: 3},
		{ID: "task-4", Subject: "something else", Status: TaskStatusBlocked, ProjectID: "proj-a", CreatedAt: 4},
		{ID: "task-5", Subject: "wire the filter bar", Status: TaskStatusPending, ProjectID: "proj-a", CreatedAt: 5},
	}
	svc := pagedTestService(t)
	svc.SetTaskStore(&mockTaskStore{listTasks: tasks})

	page, err := svc.ListTasksPage(context.Background(), "tenant-1",
		&TaskFilter{Statuses: []TaskStatus{TaskStatusBlocked}, ProjectID: "proj-a"},
		&Page{Query: "filter bar", Limit: 1})
	if err != nil {
		t.Fatalf("ListTasksPage() error = %v, want nil", err)
	}
	// Two records satisfy all three narrowings; one page holds one of them.
	if page.Total != 2 {
		t.Errorf("Total = %d, want 2 — the intersection, not the page", page.Total)
	}
	if len(page.Items) != 1 || !page.HasNext || page.NextOffset != 1 {
		t.Errorf("page = %d records (next %d, more %v), want 1 with a second page", len(page.Items), page.NextOffset, page.HasNext)
	}
	// The status facet counts only what the SEARCH left, and counts the
	// pending record the status narrowing removed.
	if page.Facets["status"]["pending"] != 1 || page.Facets["status"]["blocked"] != 3 {
		t.Errorf("status facet = %v, want blocked 3 and pending 1 over the searched set", page.Facets["status"])
	}
}

// TestListJobsPage_NarrowsOnTheDispatchedAgent proves the jobs listing can
// answer "what has this daemon run", and that the counts beside the other
// daemons survive the choice.
func TestListJobsPage_NarrowsOnTheDispatchedAgent(t *testing.T) {
	t.Parallel()

	svc := pagedServiceWithJobs(t, "tenant-1",
		&JobStatus{JobID: "job-1", State: JobStateCompleted, AgentID: "agent-1", Runner: "sdlc", StartedAt: 1},
		&JobStatus{JobID: "job-2", State: JobStateFailed, AgentID: "agent-2", Runner: "sdlc", StartedAt: 2},
		&JobStatus{JobID: "job-3", State: JobStateExecuting, Runner: "review", StartedAt: 3},
	)

	page, err := svc.ListJobsPage(context.Background(), "tenant-1", &JobFilter{AgentID: "agent-1"}, &Page{})
	if err != nil {
		t.Fatalf("ListJobsPage() error = %v, want nil", err)
	}
	if page.Total != 1 || page.Items[0].JobID != "job-1" {
		t.Fatalf("matched %d jobs (%v), want only job-1", page.Total, page.Items)
	}
	if page.Facets["agent_id"]["agent-2"] != 1 {
		t.Errorf("agent facet = %v, want agent-2 counted despite agent-1 being selected", page.Facets["agent_id"])
	}
	if page.Facets["state"]["failed"] != 1 || page.Facets["state"]["executing"] != 1 {
		t.Errorf("state facet = %v, want every state counted over the unfiltered set", page.Facets["state"])
	}
	if page.Facets["runner"]["review"] != 1 {
		t.Errorf("runner facet = %v, want the review job counted", page.Facets["runner"])
	}
}

// TestListJobsPage_AgentNarrowingComposesWithTheTaskBinding proves a base
// narrowing and a faceted one stack, and that the counts are taken over the
// base-narrowed set rather than over the tenant.
func TestListJobsPage_AgentNarrowingComposesWithTheTaskBinding(t *testing.T) {
	t.Parallel()

	svc := pagedServiceWithJobs(t, "tenant-1",
		&JobStatus{JobID: "job-1", TaskID: "task-1", AgentID: "agent-1", State: JobStateFailed, StartedAt: 1},
		&JobStatus{JobID: "job-2", TaskID: "task-1", AgentID: "agent-2", State: JobStateCompleted, StartedAt: 2},
		&JobStatus{JobID: "job-3", TaskID: "task-2", AgentID: "agent-1", State: JobStateCompleted, StartedAt: 3},
	)

	page, err := svc.ListJobsPage(context.Background(), "tenant-1",
		&JobFilter{TaskID: "task-1", AgentID: "agent-1"}, &Page{})
	if err != nil {
		t.Fatalf("ListJobsPage() error = %v, want nil", err)
	}
	if page.Total != 1 || page.Items[0].JobID != "job-1" {
		t.Fatalf("matched %d jobs (%v), want only job-1", page.Total, page.Items)
	}
	// task-2's job is outside the base narrowing, so it is not counted: the
	// filter bar on a task's attempt history is about that task.
	if page.Facets["agent_id"]["agent-1"] != 1 || page.Facets["agent_id"]["agent-2"] != 1 {
		t.Errorf("agent facet = %v, want one attempt per agent within task-1", page.Facets["agent_id"])
	}
	if len(page.Facets["agent_id"]) != 2 {
		t.Errorf("agent facet = %v, want no agent from outside the task binding", page.Facets["agent_id"])
	}
}

// TestListAgentsPage_NarrowsOnServedBackendAndCountsThemAll proves the fleet
// filter and its counts agree about a daemon serving several backends.
func TestListAgentsPage_NarrowsOnServedBackendAndCountsThemAll(t *testing.T) {
	t.Parallel()

	svc := pagedTestService(t)
	registry := NewFleetRegistry(NewMemoryTaskKV())
	svc.SetFleetRegistry(registry)
	ctx := ContextWithTenant(context.Background(), "tenant-1")
	agents := []*FleetNode{
		{ID: "a1", TenantID: "tenant-1", Name: "one", Backend: "acp", Capabilities: []string{"claude", "codex"}, Status: FleetNodeStatusRunning},
		{ID: "a2", TenantID: "tenant-1", Name: "two", Backend: "codex", Status: FleetNodeStatusOffline},
	}
	for _, agent := range agents {
		if err := registry.RegisterFleetNode(ctx, agent); err != nil {
			t.Fatalf("RegisterFleetNode(%s) error = %v", agent.ID, err)
		}
	}

	page, err := svc.ListFleetNodesPage(ctx, "tenant-1", &FleetNodeFilter{Backend: "codex"}, &Page{})
	if err != nil {
		t.Fatalf("ListFleetNodesPage() error = %v, want nil", err)
	}
	if page.Total != 2 {
		t.Errorf("matched %d agents, want both — one by capability, one by primary", page.Total)
	}
	if page.Facets["backend"]["claude"] != 1 || page.Facets["backend"]["codex"] != 2 {
		t.Errorf("backend facet = %v, want claude 1 and codex 2", page.Facets["backend"])
	}
	if page.Facets["status"]["offline"] != 1 || page.Facets["status"]["running"] != 1 {
		t.Errorf("status facet = %v, want one of each", page.Facets["status"])
	}

	offline, err := svc.ListFleetNodesPage(ctx, "tenant-1",
		&FleetNodeFilter{Backend: "codex", Status: FleetNodeStatusOffline}, &Page{})
	if err != nil {
		t.Fatalf("ListFleetNodesPage() error = %v, want nil", err)
	}
	if offline.Total != 1 || offline.Items[0].ID != "a2" {
		t.Errorf("matched %v, want only the offline daemon", offline.Items)
	}
}

// TestListWorkflowsPage_NarrowsOnProjectAndMode proves the workflows listing
// answers the two questions a filter bar can ask of a definition.
func TestListWorkflowsPage_NarrowsOnProjectAndMode(t *testing.T) {
	t.Parallel()

	svc := pagedTestService(t)
	ctx := ContextWithTenant(context.Background(), "tenant-1")
	definitions := []*WorkflowDefinition{
		{Name: "alpha", Mode: WorkflowModeConstrained, ProjectID: "proj-a", Agents: []AgentRoleSpec{{Role: "lead", Backend: "claude"}}},
		{Name: "beta", Mode: WorkflowModeAutonomous, ProjectID: "proj-a", Agents: []AgentRoleSpec{{Role: "worker", Backend: "claude"}}},
		{Name: "gamma", Mode: WorkflowModeConstrained, ProjectID: "proj-b", Agents: []AgentRoleSpec{{Role: "lead", Backend: "claude"}}},
	}
	for _, definition := range definitions {
		if err := svc.RegisterWorkflow(ctx, definition); err != nil {
			t.Fatalf("RegisterWorkflow(%s) error = %v", definition.Name, err)
		}
	}

	page, err := svc.ListWorkflowsPage(ctx, &WorkflowFilter{ProjectID: "proj-a", Mode: WorkflowModeConstrained}, &Page{})
	if err != nil {
		t.Fatalf("ListWorkflowsPage() error = %v, want nil", err)
	}
	if page.Total != 1 || page.Items[0].Name != "alpha" {
		t.Fatalf("matched %d definitions (%v), want only alpha", page.Total, page.Items)
	}
	if page.Facets["project_id"]["proj-b"] != 1 || page.Facets["mode"]["autonomous"] != 1 {
		t.Errorf("facets = %v, want the unselected choices counted", page.Facets)
	}
}

// TestListTasksPage_SortsOnTheAttemptCount proves the sort field the Tasks
// view added — "which tasks have burned the most attempts" is the question an
// operator asks of a stuck factory.
func TestListTasksPage_SortsOnTheAttemptCount(t *testing.T) {
	t.Parallel()

	svc := pagedTestService(t)
	svc.SetTaskStore(&mockTaskStore{listTasks: []*Task{
		{ID: "task-1", Attempts: 1, CreatedAt: 1},
		{ID: "task-2", Attempts: 4, CreatedAt: 2},
		{ID: "task-3", Attempts: 2, CreatedAt: 3},
	}})

	page, err := svc.ListTasksPage(context.Background(), "tenant-1", nil, &Page{Sort: "attempts:desc"})
	if err != nil {
		t.Fatalf("ListTasksPage() error = %v, want nil", err)
	}
	if page.Items[0].ID != "task-2" || page.Items[2].ID != "task-1" {
		t.Errorf("order = %v, want the most-attempted task first", page.Items)
	}
}
