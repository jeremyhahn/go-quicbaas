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
	"errors"
	"strconv"
	"testing"
)

// =============================================================================
// go-quicbaas#976: the jobs page must name the issue behind every row
// =============================================================================
//
// The jobs view is server-paged and holds ONE page of tasks. It named a job's
// forge issue by finding that job's task among the tasks it happened to be
// holding, so the chip rendered for the jobs whose task shared the first page
// of tasks and blanked for every other row — on page 2 of jobs, for nearly all
// of them. The listing carries the source now, resolved per page.

// sourcedTaskStore answers GetTask from a map and counts the reads, which is
// how the memoisation is observed rather than asserted.
type sourcedTaskStore struct {
	*mockTaskStore

	tasks map[string]*Task
	err   error
	reads []string
}

func newSourcedTaskStore(tasks ...*Task) *sourcedTaskStore {
	byID := make(map[string]*Task, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}
	return &sourcedTaskStore{mockTaskStore: &mockTaskStore{}, tasks: byID}
}

func (s *sourcedTaskStore) GetTask(_ context.Context, _, taskID string) (*Task, error) {
	s.reads = append(s.reads, taskID)
	if s.err != nil {
		return nil, s.err
	}
	task, ok := s.tasks[taskID]
	if !ok {
		return nil, &TaskNotFoundError{TaskID: taskID}
	}
	return task, nil
}

// forgeIssueKind is pkg/orchestration/sdlc's source kind, spelled out here because this
// package cannot import the module that declares it: the kind is registered
// INTO the engine, not by it.
const forgeIssueKind TaskSourceKind = "forge_issue"

// forgeTask is a task adopted from a forge issue.
func forgeTask(id, ref string) *Task {
	return &Task{
		ID:      id,
		Subject: "subject " + id,
		Source: &TaskSourceRef{
			Kind: forgeIssueKind,
			Ref:  ref,
			URL:  "http://forge.example/" + ref,
		},
	}
}

// sourcedJobService is a service holding n concluded attempts, each at its own
// task, with the task store those tasks live in.
func sourcedJobService(t *testing.T, tenantID string, n int) (*agentServiceImpl, *sourcedTaskStore) {
	t.Helper()

	tasks := make([]*Task, 0, n)
	statuses := make([]*JobStatus, 0, n)
	for i := range n {
		id := strconv.Itoa(i)
		tasks = append(tasks, forgeTask("task-"+id, "jeremyhahn/go-quicbaas#"+id))
		statuses = append(statuses, concludedJob("job-"+id, tenantID, "task-"+id, 1, int64(i)))
	}

	svc, _ := concludedJobService(t, tenantID, statuses...)
	store := newSourcedTaskStore(tasks...)
	svc.SetTaskStore(store)
	return svc, store
}

// TestListJobsPage_CarriesTheIssueOnEveryPage is the regression for
// go-quicbaas#976: the ref must be on the rows of page 2, where a client
// holding one page of tasks could never find it.
func TestListJobsPage_CarriesTheIssueOnEveryPage(t *testing.T) {
	const tenantID = "tenant-sources"
	svc, _ := sourcedJobService(t, tenantID, 6)
	ctx := ContextWithTenant(context.Background(), tenantID)

	// Newest first, so page 2 of 6 at a limit of 2 is job-3 then job-2.
	page, err := svc.ListJobsPage(ctx, tenantID, nil, &Page{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListJobsPage() error = %v, want nil", err)
	}
	if page.Total != 6 || len(page.Items) != 2 {
		t.Fatalf("page = %d of %d, want 2 of 6", len(page.Items), page.Total)
	}
	for _, job := range page.Items {
		if job.Source == nil {
			t.Fatalf("job %s carries no source; the chip blanks for this row", job.JobID)
		}
		want := "jeremyhahn/go-quicbaas#" + job.TaskID[len("task-"):]
		if job.Source.Ref != want {
			t.Errorf("job %s ref = %q, want %q", job.JobID, job.Source.Ref, want)
		}
		if job.Source.Kind != forgeIssueKind {
			t.Errorf("job %s kind = %q, want %q", job.JobID, job.Source.Kind, forgeIssueKind)
		}
		if job.Source.URL == "" {
			t.Errorf("job %s carries no URL; the chip renders as plain text", job.JobID)
		}
	}
}

// TestListJobsPage_ReadsOnlyThePagesTasks proves the projection is paid for
// the page and not for the collection: the tenant owns six jobs, the page
// holds two, and exactly two task reads happen.
func TestListJobsPage_ReadsOnlyThePagesTasks(t *testing.T) {
	const tenantID = "tenant-page-cost"
	svc, store := sourcedJobService(t, tenantID, 6)
	ctx := ContextWithTenant(context.Background(), tenantID)

	if _, err := svc.ListJobsPage(ctx, tenantID, nil, &Page{Limit: 2}); err != nil {
		t.Fatalf("ListJobsPage() error = %v, want nil", err)
	}
	if len(store.reads) != 2 {
		t.Errorf("read %d tasks to answer a page of 2: %v", len(store.reads), store.reads)
	}
}

// TestListJobsPage_ResolvesEachTaskOnce proves the read is memoised: a task's
// whole attempt history is one page of jobs and must cost one task read, not
// one per attempt.
func TestListJobsPage_ResolvesEachTaskOnce(t *testing.T) {
	const tenantID = "tenant-memo"
	attempts := []*JobStatus{
		concludedJob("job-1", tenantID, "task-a", 1, 10),
		concludedJob("job-2", tenantID, "task-a", 2, 20),
		concludedJob("job-3", tenantID, "task-a", 3, 30),
	}
	svc, _ := concludedJobService(t, tenantID, attempts...)
	store := newSourcedTaskStore(forgeTask("task-a", "jeremyhahn/go-quicbaas#62"))
	svc.SetTaskStore(store)

	page, err := svc.ListJobsPage(ContextWithTenant(context.Background(), tenantID),
		tenantID, &JobFilter{TaskID: "task-a"}, &Page{Limit: 25})
	if err != nil {
		t.Fatalf("ListJobsPage() error = %v, want nil", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("listed %d attempts, want 3", len(page.Items))
	}
	for _, job := range page.Items {
		if job.Source == nil || job.Source.Ref != "jeremyhahn/go-quicbaas#62" {
			t.Errorf("attempt %s = %+v, want every attempt to name the issue", job.JobID, job.Source)
		}
	}
	if len(store.reads) != 1 {
		t.Errorf("read the task %d times for 3 attempts at it: %v", len(store.reads), store.reads)
	}
}

// TestListJobsPage_AnUnreadableTaskIsNoSourceNotAnError proves the page
// survives a task the store cannot answer for: the job's own record is
// complete without it, and refusing the page would take away the rows that
// ARE readable over the one that is not.
func TestListJobsPage_AnUnreadableTaskIsNoSourceNotAnError(t *testing.T) {
	const tenantID = "tenant-unreadable"
	svc, store := sourcedJobService(t, tenantID, 2)
	store.err = errors.New("store unavailable")

	page, err := svc.ListJobsPage(ContextWithTenant(context.Background(), tenantID),
		tenantID, nil, &Page{Limit: 25})
	if err != nil {
		t.Fatalf("ListJobsPage() error = %v, want the page anyway", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("listed %d jobs, want 2", len(page.Items))
	}
	for _, job := range page.Items {
		if job.Source != nil {
			t.Errorf("job %s invented a source from an unreadable task: %+v", job.JobID, job.Source)
		}
	}
}

// TestListJobsPage_AJobThatIsNotAnAttemptHasNoSource proves a lead-driven job
// — one bound to no task — is left alone, and costs no task read.
func TestListJobsPage_AJobThatIsNotAnAttemptHasNoSource(t *testing.T) {
	const tenantID = "tenant-lead"
	lead := concludedJob("job-lead", tenantID, "", 0, 10)
	svc, _ := concludedJobService(t, tenantID, lead)
	store := newSourcedTaskStore()
	svc.SetTaskStore(store)

	page, err := svc.ListJobsPage(ContextWithTenant(context.Background(), tenantID),
		tenantID, nil, &Page{Limit: 25})
	if err != nil {
		t.Fatalf("ListJobsPage() error = %v, want nil", err)
	}
	if len(page.Items) != 1 || page.Items[0].Source != nil {
		t.Errorf("lead job = %+v, want no source", page.Items[0])
	}
	if len(store.reads) != 0 {
		t.Errorf("read %v for a job bound to no task", store.reads)
	}
}

// TestListJobsPage_LeavesThePublishedStatusAlone is the concurrency rule
// written as a test. A running job's status is published by its own goroutine
// and handed to every reader as the same pointer; writing the source into it
// would be a data race with those readers. The page gets a COPY.
func TestListJobsPage_LeavesThePublishedStatusAlone(t *testing.T) {
	const tenantID = "tenant-copy"
	svc, _ := concludedJobService(t, tenantID)
	svc.SetTaskStore(newSourcedTaskStore(forgeTask("task-a", "jeremyhahn/go-quicbaas#62")))

	live := newJobRun(JobRunConfig{
		JobID:  "job-live",
		Runner: JobRunnerWorkflow,
		Config: &JobConfig{TenantID: tenantID, TaskID: "task-a", Attempt: 1},
		Logger: &controlLoopMockLogger{},
	})
	live.snapshot(JobStateExecuting)
	svc.jobs.Store(tenantID+"/job-live", &runningJob{run: live, cancel: func() {}})

	page, err := svc.ListJobsPage(ContextWithTenant(context.Background(), tenantID),
		tenantID, nil, &Page{Limit: 25})
	if err != nil {
		t.Fatalf("ListJobsPage() error = %v, want nil", err)
	}
	if len(page.Items) != 1 || page.Items[0].Source == nil {
		t.Fatalf("page = %+v, want the running job with its source", page.Items)
	}
	if live.Status().Source != nil {
		t.Error("the projection wrote into the status the run published; readers of that pointer race with it")
	}
}

// TestListJobsPage_WithoutATaskStoreStillPages proves the projection is
// optional wiring, not a precondition: a service with no task store answers
// the same page with no sources on it.
func TestListJobsPage_WithoutATaskStoreStillPages(t *testing.T) {
	const tenantID = "tenant-no-store"
	svc, _ := concludedJobService(t, tenantID, concludedJob("job-1", tenantID, "task-a", 1, 10))

	page, err := svc.ListJobsPage(ContextWithTenant(context.Background(), tenantID),
		tenantID, nil, &Page{Limit: 25})
	if err != nil {
		t.Fatalf("ListJobsPage() error = %v, want nil", err)
	}
	if len(page.Items) != 1 || page.Items[0].Source != nil {
		t.Errorf("page = %+v, want one job and no source", page.Items)
	}
}

// TestGetJobStatus_CarriesTheIssueToo proves the detail read agrees with the
// listing: one shape for "the issue behind this job", whichever verb answered.
func TestGetJobStatus_CarriesTheIssueToo(t *testing.T) {
	const tenantID = "tenant-detail"
	svc, _ := concludedJobService(t, tenantID, concludedJob("job-1", tenantID, "task-a", 1, 10))
	svc.SetTaskStore(newSourcedTaskStore(forgeTask("task-a", "jeremyhahn/go-quicbaas#62")))

	status, err := svc.GetJobStatus(ContextWithTenant(context.Background(), tenantID), tenantID, "job-1")
	if err != nil {
		t.Fatalf("GetJobStatus() error = %v, want nil", err)
	}
	if status.Source == nil || status.Source.Ref != "jeremyhahn/go-quicbaas#62" {
		t.Errorf("source = %+v, want the task's forge issue", status.Source)
	}
}

// TestListJobsPage_ResolvesATaskBoundOnlyThroughTheConfig covers the persisted
// record shape jobStatusTaskID exists for: a job written before TaskID was
// promoted onto JobStatus names its task only in its launch config. The
// listing already pages such records in; the projection must name their issue
// too, or the chip blanks for exactly the rows whose task is off the page.
func TestListJobsPage_ResolvesATaskBoundOnlyThroughTheConfig(t *testing.T) {
	const tenantID = "tenant-config-binding"
	var statuses []*JobStatus
	for i := range 4 {
		id := strconv.Itoa(i)
		legacy := concludedJob("job-"+id, tenantID, "", 1, int64(i))
		legacy.Config = &JobConfig{TenantID: tenantID, TaskID: "task-" + id, Attempt: 1}
		statuses = append(statuses, legacy)
	}
	// A second config-only attempt at task-3 proves the memoisation keys on
	// the resolved binding, not on the empty top-level field.
	second := concludedJob("job-3b", tenantID, "", 2, 3)
	second.Config = &JobConfig{TenantID: tenantID, TaskID: "task-3", Attempt: 2}
	statuses = append(statuses, second)

	svc, _ := concludedJobService(t, tenantID, statuses...)
	store := newSourcedTaskStore(
		forgeTask("task-0", "jeremyhahn/go-quicbaas#0"),
		forgeTask("task-1", "jeremyhahn/go-quicbaas#1"),
		forgeTask("task-2", "jeremyhahn/go-quicbaas#2"),
		forgeTask("task-3", "jeremyhahn/go-quicbaas#3"),
	)
	svc.SetTaskStore(store)

	page, err := svc.ListJobsPage(ContextWithTenant(context.Background(), tenantID),
		tenantID, nil, &Page{Limit: 3, Offset: 2})
	if err != nil {
		t.Fatalf("ListJobsPage() error = %v, want nil", err)
	}
	if page.Total != 5 || len(page.Items) != 3 {
		t.Fatalf("page = %d of %d, want 3 of 5", len(page.Items), page.Total)
	}
	for _, job := range page.Items {
		want := "jeremyhahn/go-quicbaas#" + job.Config.TaskID[len("task-"):]
		if job.Source == nil || job.Source.Ref != want {
			t.Errorf("config-bound job %s source = %+v, want ref %q", job.JobID, job.Source, want)
		}
	}
	if len(store.reads) != len(page.Items) {
		t.Errorf("read %d tasks for a page of %d distinct tasks: %v", len(store.reads), len(page.Items), store.reads)
	}

	// The whole listing, where task-3 has two config-only attempts, costs one
	// read per distinct task.
	store.reads = nil
	if _, err := svc.ListJobsPage(ContextWithTenant(context.Background(), tenantID),
		tenantID, nil, &Page{Limit: 25}); err != nil {
		t.Fatalf("ListJobsPage() error = %v, want nil", err)
	}
	if len(store.reads) != 4 {
		t.Errorf("read %d tasks for 5 attempts at 4 tasks: %v", len(store.reads), store.reads)
	}
}

// TestGetJobStatus_AgreesWithTheListingOnAConfigOnlyBinding proves the detail
// read resolves the task the same way the listing does: a record naming its
// task only in its launch config must carry the same source on both verbs, or
// the chip on the jobs table names an issue the job's own page cannot.
func TestGetJobStatus_AgreesWithTheListingOnAConfigOnlyBinding(t *testing.T) {
	const tenantID = "tenant-config-detail"
	legacy := concludedJob("job-legacy", tenantID, "", 1, 10)
	legacy.Config = &JobConfig{TenantID: tenantID, TaskID: "task-a", Attempt: 1}
	svc, _ := concludedJobService(t, tenantID, legacy)
	svc.SetTaskStore(newSourcedTaskStore(forgeTask("task-a", "jeremyhahn/go-quicbaas#62")))
	ctx := ContextWithTenant(context.Background(), tenantID)

	page, err := svc.ListJobsPage(ctx, tenantID, nil, &Page{Limit: 25})
	if err != nil {
		t.Fatalf("ListJobsPage() error = %v, want nil", err)
	}
	if len(page.Items) != 1 || page.Items[0].Source == nil {
		t.Fatalf("page = %+v, want the config-bound job with its source", page.Items)
	}
	status, err := svc.GetJobStatus(ctx, tenantID, "job-legacy")
	if err != nil {
		t.Fatalf("GetJobStatus() error = %v, want nil", err)
	}
	if status.Source == nil {
		t.Fatal("detail read carries no source for a job the listing names an issue for")
	}
	if *status.Source != *page.Items[0].Source {
		t.Errorf("detail source = %+v, listing source = %+v, want identical", status.Source, page.Items[0].Source)
	}
}
