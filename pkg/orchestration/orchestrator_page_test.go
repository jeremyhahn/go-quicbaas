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
	"strconv"
	"testing"
)

// pagedTestService builds a service with an in-memory agent store, which is
// everything the paged listings need beyond the store each one is given.
func pagedTestService(t *testing.T) *agentServiceImpl {
	t.Helper()

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService() error = %v", err)
	}
	return svc.(*agentServiceImpl)
}

// TestListTasksPage_CutsAPageOfTheFilteredSet proves the paged verb narrows
// with the filter, counts what the filter left, and returns only one page of
// it.
func TestListTasksPage_CutsAPageOfTheFilteredSet(t *testing.T) {
	t.Parallel()

	tasks := make([]*Task, 0, 7)
	for i := range 7 {
		tasks = append(tasks, &Task{
			ID:        "task-" + strconv.Itoa(i),
			Subject:   "subject " + strconv.Itoa(i),
			Status:    TaskStatusPending,
			CreatedAt: int64(i),
		})
	}

	store := &mockTaskStore{listTasks: tasks}
	svc := pagedTestService(t)
	svc.SetTaskStore(store)

	page, err := svc.ListTasksPage(context.Background(), "tenant-1",
		&TaskFilter{Status: TaskStatusPending}, &Page{Limit: 3})
	if err != nil {
		t.Fatalf("ListTasksPage() error = %v, want nil", err)
	}
	if len(page.Items) != 3 {
		t.Errorf("page size = %d, want 3", len(page.Items))
	}
	if page.Total != 7 {
		t.Errorf("Total = %d, want 7", page.Total)
	}
	if !page.HasNext || page.NextOffset != 3 {
		t.Errorf("next offset = %d (hasNext %v), want 3 (true)", page.NextOffset, page.HasNext)
	}
	if page.Items[0].ID != "task-6" {
		t.Errorf("first task = %q, want the newest (task-6)", page.Items[0].ID)
	}
	if len(store.listCalls) != 1 || store.listCalls[0].Filter.Status != TaskStatusPending {
		t.Error("the filter did not reach the store; paging must not replace narrowing")
	}
}

// TestListTasksPage_SearchComposesWithTheFilter proves `q` narrows what the
// filter already left rather than replacing it.
func TestListTasksPage_SearchComposesWithTheFilter(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{listTasks: []*Task{
		{ID: "task-1", Subject: "page the job list", ProjectID: "proj-a"},
		{ID: "task-2", Subject: "page the task list", ProjectID: "proj-a"},
		{ID: "task-3", Subject: "unrelated", ProjectID: "proj-a"},
	}}
	svc := pagedTestService(t)
	svc.SetTaskStore(store)

	page, err := svc.ListTasksPage(context.Background(), "tenant-1",
		&TaskFilter{ProjectID: "proj-a"}, &Page{Query: "job list"})
	if err != nil {
		t.Fatalf("ListTasksPage() error = %v, want nil", err)
	}
	if page.Total != 1 || page.Items[0].ID != "task-1" {
		t.Errorf("matched %d tasks, want only task-1", page.Total)
	}
	if len(store.listCalls) != 1 || store.listCalls[0].Filter.ProjectID != "proj-a" {
		t.Error("the project filter did not reach the store")
	}
}

// TestListTasksPage_PropagatesTheStoreRefusal proves a paged listing fails
// the way its unpaged twin does: a missing store is a typed refusal, not an
// empty page that reads as "there are no tasks".
func TestListTasksPage_PropagatesTheStoreRefusal(t *testing.T) {
	t.Parallel()

	svc := pagedTestService(t)

	page, err := svc.ListTasksPage(context.Background(), "tenant-1", nil, nil)
	if page != nil {
		t.Error("ListTasksPage() returned a page with no task store configured")
	}
	if !errors.Is(err, ErrTaskStoreRequired) {
		t.Errorf("error = %v, want ErrTaskStoreRequired", err)
	}
}

// TestListTasksPage_RefusesAnUnknownSortField proves the allow-list is
// enforced in the service, so every transport refuses the same field.
func TestListTasksPage_RefusesAnUnknownSortField(t *testing.T) {
	t.Parallel()

	svc := pagedTestService(t)
	svc.SetTaskStore(&mockTaskStore{listTasks: []*Task{{ID: "task-1"}}})

	page, err := svc.ListTasksPage(context.Background(), "tenant-1", nil, &Page{Sort: "runner"})
	if page != nil {
		t.Error("ListTasksPage() returned a page for a sort it cannot serve")
	}
	var unknown *UnknownSortFieldError
	if !errors.As(err, &unknown) || unknown.Field != "runner" {
		t.Errorf("error = %v, want *UnknownSortFieldError naming runner", err)
	}
}

// TestListJobsPage_RequiresATenant proves the paged jobs listing keeps the
// tenant check its unpaged twin has: a tenant-less scan must not be reachable
// through the new verb.
func TestListJobsPage_RequiresATenant(t *testing.T) {
	t.Parallel()

	svc := pagedTestService(t)

	page, err := svc.ListJobsPage(context.Background(), "", nil, nil)
	if page != nil {
		t.Error("ListJobsPage() returned a page for a tenant-less request")
	}
	if !errors.Is(err, ErrTenantIDRequired) {
		t.Errorf("error = %v, want ErrTenantIDRequired", err)
	}
}

// TestListJobsPage_EmptyTenantListsNothing proves an empty listing is an
// empty page with a zero total, not a nil slice a transport would render as
// null.
func TestListJobsPage_EmptyTenantListsNothing(t *testing.T) {
	t.Parallel()

	svc := pagedTestService(t)

	page, err := svc.ListJobsPage(context.Background(), "tenant-1", nil, &Page{Limit: 10})
	if err != nil {
		t.Fatalf("ListJobsPage() error = %v, want nil", err)
	}
	if page.Total != 0 || len(page.Items) != 0 || page.Items == nil {
		t.Errorf("page = %+v, want an empty, non-nil page", page)
	}
	if page.Limit != 10 {
		t.Errorf("Limit = %d, want the applied 10", page.Limit)
	}
}

// TestListAgentsPage_PropagatesTheRegistryRefusal proves the fleet listing
// fails loudly when no registry is configured.
func TestListAgentsPage_PropagatesTheRegistryRefusal(t *testing.T) {
	t.Parallel()

	svc := pagedTestService(t)

	page, err := svc.ListAgentsPage(context.Background(), "tenant-1", nil, nil)
	if page != nil {
		t.Error("ListAgentsPage() returned a page with no registry configured")
	}
	if !errors.Is(err, ErrAgentRegistryRequired) {
		t.Errorf("error = %v, want ErrAgentRegistryRequired", err)
	}
}

// TestListWorkflowsPage_PagesTheRegisteredDefinitions proves the workflow
// listing pages in its by-name order and counts the whole set.
func TestListWorkflowsPage_PagesTheRegisteredDefinitions(t *testing.T) {
	t.Parallel()

	svc := pagedTestService(t)
	ctx := ContextWithTenant(context.Background(), "tenant-1")

	for _, name := range []string{"zeta", "alpha", "mid"} {
		if err := svc.RegisterWorkflow(ctx, &WorkflowDefinition{
			Name:   name,
			Mode:   WorkflowModeAutonomous,
			Agents: []AgentRoleSpec{{Role: "worker", Backend: "claude"}},
		}); err != nil {
			t.Fatalf("RegisterWorkflow(%q) error = %v", name, err)
		}
	}

	page, err := svc.ListWorkflowsPage(ctx, &Page{Limit: 2})
	if err != nil {
		t.Fatalf("ListWorkflowsPage() error = %v, want nil", err)
	}
	if page.Total != 3 {
		t.Errorf("Total = %d, want 3", page.Total)
	}
	if len(page.Items) != 2 || page.Items[0].Name != "alpha" {
		t.Errorf("page = %v, want [alpha mid]", page.Items)
	}
	if !page.HasNext || page.NextOffset != 2 {
		t.Errorf("next offset = %d (hasNext %v), want 2 (true)", page.NextOffset, page.HasNext)
	}
}

// TestListWorkflowsPage_RefusesAnUnknownSortField proves the workflow
// listing's narrower allow-list refuses a field another endpoint has.
func TestListWorkflowsPage_RefusesAnUnknownSortField(t *testing.T) {
	t.Parallel()

	svc := pagedTestService(t)

	page, err := svc.ListWorkflowsPage(context.Background(), &Page{Sort: "created_at"})
	if page != nil {
		t.Error("ListWorkflowsPage() returned a page for a sort it cannot serve")
	}
	var unknown *UnknownSortFieldError
	if !errors.As(err, &unknown) || unknown.Field != "created_at" {
		t.Errorf("error = %v, want *UnknownSortFieldError naming created_at", err)
	}
}

// TestJobFilter_MatchesTheNewNarrowings covers the runner, project and state
// filters the jobs listing gained, including the record shape that carries
// them only on the launch config.
func TestJobFilter_MatchesTheNewNarrowings(t *testing.T) {
	t.Parallel()

	sdlcRunning := &JobStatus{
		JobID:  "job-1",
		State:  JobStateExecuting,
		Runner: "sdlc",
		Config: &JobConfig{ProjectID: "proj-a"},
	}
	triageDone := &JobStatus{
		JobID:  "job-2",
		State:  JobStateCompleted,
		Config: &JobConfig{Runner: "triage", ProjectID: "proj-b"},
	}

	tests := []struct {
		name   string
		filter *JobFilter
		status *JobStatus
		want   bool
	}{
		{"runner matches on status", &JobFilter{Runner: "sdlc"}, sdlcRunning, true},
		{"runner matches on config", &JobFilter{Runner: "triage"}, triageDone, true},
		{"other runner does not match", &JobFilter{Runner: "review"}, sdlcRunning, false},
		{"project matches", &JobFilter{ProjectID: "proj-a"}, sdlcRunning, true},
		{"other project does not match", &JobFilter{ProjectID: "proj-b"}, sdlcRunning, false},
		{"state in the list matches", &JobFilter{Statuses: []JobState{JobStateExecuting, JobStateFailed}}, sdlcRunning, true},
		{"state outside the list does not", &JobFilter{Statuses: []JobState{JobStateFailed}}, sdlcRunning, false},
		{"narrowings compose", &JobFilter{Runner: "sdlc", ProjectID: "proj-a", Statuses: []JobState{JobStateExecuting}}, sdlcRunning, true},
		{"one failing narrowing refuses the record", &JobFilter{Runner: "sdlc", ProjectID: "proj-b"}, sdlcRunning, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.filter.Matches(tc.status); got != tc.want {
				t.Errorf("Matches() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestJobFilter_EmptyCountsTheNewNarrowings proves a filter that narrows only
// by runner, project or state is not mistaken for an unrestricted one.
func TestJobFilter_EmptyCountsTheNewNarrowings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		filter *JobFilter
		want   bool
	}{
		{"no fields set", &JobFilter{}, true},
		{"runner set", &JobFilter{Runner: "sdlc"}, false},
		{"project set", &JobFilter{ProjectID: "proj-a"}, false},
		{"statuses set", &JobFilter{Statuses: []JobState{JobStateFailed}}, false},
		{"empty statuses narrow nothing", &JobFilter{Statuses: []JobState{}}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.filter.Empty(); got != tc.want {
				t.Errorf("Empty() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestJobFilter_NarrowsToNamedJobs proves the job_ids narrowing a page of
// sessions or tasks uses to resolve the jobs its rows name in ONE call.
func TestJobFilter_NarrowsToNamedJobs(t *testing.T) {
	t.Parallel()

	worker := &JobStatus{JobID: "job-1", State: JobStateExecuting, Runner: "sdlc"}
	reviewer := &JobStatus{JobID: "job-2", State: JobStateCompleted, Runner: "review"}

	tests := []struct {
		name   string
		filter *JobFilter
		status *JobStatus
		want   bool
	}{
		{"named job matches", &JobFilter{JobIDs: []string{"job-1", "job-2"}}, worker, true},
		{"unnamed job does not", &JobFilter{JobIDs: []string{"job-2"}}, worker, false},
		{"an empty list narrows nothing", &JobFilter{JobIDs: []string{}}, worker, true},
		{"composes with the runner", &JobFilter{JobIDs: []string{"job-2"}, Runner: "review"}, reviewer, true},
		{"one failing narrowing refuses the record", &JobFilter{JobIDs: []string{"job-2"}, Runner: "sdlc"}, reviewer, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.filter.Matches(tc.status); got != tc.want {
				t.Errorf("Matches() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestJobFilter_EmptyCountsJobIDs proves a filter that narrows only to named
// jobs is not mistaken for an unrestricted one — which would list the whole
// tenant for a caller that asked about two rows.
func TestJobFilter_EmptyCountsJobIDs(t *testing.T) {
	t.Parallel()

	if (&JobFilter{JobIDs: []string{"job-1"}}).Empty() {
		t.Error("Empty() = true for a filter narrowed to one job")
	}
	if !(&JobFilter{JobIDs: []string{}}).Empty() {
		t.Error("Empty() = false for an empty job id list, which narrows nothing")
	}
}

// TestListJobsPage_NarrowsToNamedJobs proves the narrowing reaches the paged
// listing, and that Total counts the jobs the caller asked about rather than
// every job the tenant owns.
func TestListJobsPage_NarrowsToNamedJobs(t *testing.T) {
	t.Parallel()

	jobs := []*JobStatus{
		{JobID: "job-1", State: JobStateExecuting, StartedAt: 30},
		{JobID: "job-2", State: JobStateCompleted, StartedAt: 20},
		{JobID: "job-3", State: JobStateFailed, StartedAt: 10},
	}
	matched := make([]*JobStatus, 0, len(jobs))
	filter := &JobFilter{JobIDs: []string{"job-1", "job-3"}}
	for _, job := range jobs {
		if filter.Matches(job) {
			matched = append(matched, job)
		}
	}

	page, err := ApplyPage(matched, &Page{Limit: 25}, jobPageSpec)
	if err != nil {
		t.Fatalf("ApplyPage() error = %v", err)
	}
	if page.Total != 2 {
		t.Errorf("Total = %d, want 2", page.Total)
	}
	if len(page.Items) != 2 || page.Items[0].JobID != "job-1" || page.Items[1].JobID != "job-3" {
		t.Errorf("Items = %+v, want job-1 then job-3", page.Items)
	}
}
