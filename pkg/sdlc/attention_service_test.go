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

package sdlc

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// fakeAttentionTasks is an in-memory AttentionTaskService that records the
// order every verb is called in, so a test can assert "comment posted
// before readmit" rather than merely "both happened".
type fakeAttentionTasks struct {
	mu    sync.Mutex
	tasks map[string]*orchestration.Task
	calls []string

	bumpErr     error
	readmitErr  error
	completeErr error
}

func newFakeAttentionTasks(tasks ...*orchestration.Task) *fakeAttentionTasks {
	f := &fakeAttentionTasks{tasks: map[string]*orchestration.Task{}}
	for _, task := range tasks {
		f.tasks[task.ID] = task
	}
	return f
}

func (f *fakeAttentionTasks) GetTask(_ context.Context, _, taskID string) (*orchestration.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	task, ok := f.tasks[taskID]
	if !ok {
		return nil, &orchestration.TaskNotFoundError{TaskID: taskID}
	}
	return task, nil
}

func (f *fakeAttentionTasks) ListTasksPage(_ context.Context, _ string, filter *orchestration.TaskFilter, _ *orchestration.Page) (*orchestration.PageResult[*orchestration.Task], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []*orchestration.Task
	for _, task := range f.tasks {
		if filter != nil && filter.Status != "" && task.Status != filter.Status {
			continue
		}
		if filter != nil && len(filter.Statuses) > 0 && !slices.Contains(filter.Statuses, task.Status) {
			continue
		}
		items = append(items, task)
	}
	return &orchestration.PageResult[*orchestration.Task]{Items: items, Total: len(items)}, nil
}

func (f *fakeAttentionTasks) ReadmitTask(_ context.Context, _, taskID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "readmit:"+taskID)
	if f.readmitErr != nil {
		return f.readmitErr
	}
	if task, ok := f.tasks[taskID]; ok {
		task.Status = orchestration.TaskStatusPending
	}
	return nil
}

func (f *fakeAttentionTasks) CloseBlockedTask(_ context.Context, _, taskID string, result *orchestration.TaskResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "complete:"+taskID)
	if f.completeErr != nil {
		return f.completeErr
	}
	if task, ok := f.tasks[taskID]; ok {
		// Mirrors the real store: only a blocked task may be closed.
		if task.Status != orchestration.TaskStatusBlocked {
			return &orchestration.TaskInvalidStateError{TaskID: taskID, CurrentState: task.Status, TargetState: orchestration.TaskStatusCompleted}
		}
		task.Status = orchestration.TaskStatusCompleted
		task.Result = result
	}
	return nil
}

func (f *fakeAttentionTasks) BumpTaskReviewRoundBudget(_ context.Context, _, taskID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "bump:"+taskID)
	if f.bumpErr != nil {
		return 0, f.bumpErr
	}
	task := f.tasks[taskID]
	task.ReviewRoundBonus++
	return task.ReviewRoundBonus, nil
}

func (f *fakeAttentionTasks) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// blockedFixtureTask returns a blocked task with a real, parseable
// BlockedReason and a forge-issue source, so Resolve can find its issue.
func blockedFixtureTask(reason string, kind orchestration.TaskBlockKind) *orchestration.Task {
	task := testTaskRecord(77)
	task.Status = orchestration.TaskStatusBlocked
	task.BlockKind = kind
	task.BlockedReason = reason
	task.StatusChangedAt = 1000
	return task
}

// attentionFixture wires an AttentionService with fakes an individual test
// can inspect and mutate.
type attentionFixture struct {
	service  *AttentionService
	tasks    *fakeAttentionTasks
	forge    *fakeForge
	bindings *fakeBindings
	projects *fakeProjects
}

func newAttentionFixture(t *testing.T, tasks ...*orchestration.Task) *attentionFixture {
	t.Helper()
	f := &attentionFixture{
		tasks:    newFakeAttentionTasks(tasks...),
		forge:    newFakeForge(),
		bindings: &fakeBindings{binding: testBinding()},
		projects: &fakeProjects{projects: map[string]*orchestration.Project{testProject: testOrchProject()}},
	}
	f.forge.putIssue(77, "the parked issue")
	svc, err := NewAttentionService(AttentionServiceConfig{
		Tasks: f.tasks, Bindings: f.bindings, Forges: &fakeForges{forge: f.forge},
		Projects: f.projects,
	})
	if err != nil {
		t.Fatalf("NewAttentionService: %v", err)
	}
	f.service = svc
	return f
}

// =============================================================================
// Construction
// =============================================================================

func TestNewAttentionService_RefusesEachMissingCollaborator(t *testing.T) {
	full := AttentionServiceConfig{
		Tasks: newFakeAttentionTasks(), Bindings: &fakeBindings{}, Forges: &fakeForges{},
		Projects: &fakeProjects{projects: map[string]*orchestration.Project{}},
	}
	cases := []struct {
		name   string
		mutate func(*AttentionServiceConfig)
	}{
		{"tasks", func(c *AttentionServiceConfig) { c.Tasks = nil }},
		{"bindings", func(c *AttentionServiceConfig) { c.Bindings = nil }},
		{"forges", func(c *AttentionServiceConfig) { c.Forges = nil }},
		{"projects", func(c *AttentionServiceConfig) { c.Projects = nil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := full
			c.mutate(&cfg)
			if _, err := NewAttentionService(cfg); err == nil {
				t.Fatalf("NewAttentionService with %s=nil: want an error, got none", c.name)
			}
		})
	}
}

func TestNewAttentionService_ValidConfigSucceeds(t *testing.T) {
	svc, err := NewAttentionService(AttentionServiceConfig{
		Tasks: newFakeAttentionTasks(), Bindings: &fakeBindings{}, Forges: &fakeForges{},
		Projects: &fakeProjects{projects: map[string]*orchestration.Project{}},
	})
	if err != nil {
		t.Fatalf("NewAttentionService: %v", err)
	}
	if svc == nil {
		t.Fatal("NewAttentionService returned a nil service with no error")
	}
}

// =============================================================================
// List
// =============================================================================

func TestAttentionService_List_RendersParkedTasksWithFacets(t *testing.T) {
	task := blockedFixtureTask(fixtureP256ProtoOwnerRuling, orchestration.TaskBlockKindNeedsInfo)
	f := newAttentionFixture(t, task)

	page, err := f.service.List(context.Background(), testTenant, AttentionFilter{}, &orchestration.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(page.Items))
	}
	item := page.Items[0]
	if item.Kind != AttentionParkedTask {
		t.Errorf("Kind = %q, want %q", item.Kind, AttentionParkedTask)
	}
	if item.ProjectName != "api" {
		t.Errorf("ProjectName = %q, want the resolved project name", item.ProjectName)
	}
	if item.IssueRef == "" || item.IssueURL == "" {
		t.Errorf("IssueRef/IssueURL are empty: %+v", item)
	}
	if item.Condition != UnblockOwnerRuling {
		t.Errorf("Condition = %q, want %q", item.Condition, UnblockOwnerRuling)
	}
	if item.Recommendation == "" {
		t.Error("Recommendation is empty; ExtractAsk should have found the fallback paragraph")
	}
	if got, want := page.Facets["kind"][string(AttentionParkedTask)], 1; got != want {
		t.Errorf("facets[kind][parked_task] = %d, want %d", got, want)
	}
	if got, want := page.Facets["project_id"][testProject], 1; got != want {
		t.Errorf("facets[project_id][%s] = %d, want %d", testProject, got, want)
	}
}

func TestAttentionService_List_FiltersByProjectAndKind(t *testing.T) {
	a := blockedFixtureTask(fixtureOwnerRulingIssueBody, orchestration.TaskBlockKindBlocked)
	a.ID = "task-a"
	b := blockedFixtureTask(fixtureForgeIssueState, orchestration.TaskBlockKindBlocked)
	b.ID = "task-b"
	b.ProjectID = "other-project"
	f := newAttentionFixture(t, a, b)

	page, err := f.service.List(context.Background(), testTenant, AttentionFilter{ProjectID: testProject}, &orchestration.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "task-a" {
		t.Fatalf("List filtered by project = %v, want only task-a", page.Items)
	}
	if page.Total != 1 {
		t.Errorf("Total = %d, want 1 (the narrowed count, not the whole tenant)", page.Total)
	}
}

func TestAttentionService_List_RefusesUnknownKind(t *testing.T) {
	f := newAttentionFixture(t)
	if _, err := f.service.List(context.Background(), testTenant, AttentionFilter{Kind: "not-a-kind"}, nil); err == nil {
		t.Fatal("List with an unknown kind: want an error, got none")
	}
}

// =============================================================================
// Resolve — ordering and the five actions
// =============================================================================

func TestAttentionService_Resolve_ApproveRecommendationCommentsThenReadmits(t *testing.T) {
	task := blockedFixtureTask(fixtureP256ProtoOwnerRuling, orchestration.TaskBlockKindNeedsInfo)
	f := newAttentionFixture(t, task)

	item, err := f.service.Resolve(context.Background(), testTenant, task.ID, AttentionResolution{Action: AttentionApproveRecommendation, ActorID: "op-1"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if item.Recommendation == "" {
		t.Fatal("returned item carries no recommendation to assert against")
	}

	comments := f.forge.commentsOn(77)
	if len(comments) != 1 {
		t.Fatalf("comments on issue 77 = %d, want 1", len(comments))
	}
	if !strings.Contains(comments[0].Body, attentionRulingActor) {
		t.Errorf("comment body = %q, want it to carry the bot identity %q", comments[0].Body, attentionRulingActor)
	}
	if calls := f.tasks.callLog(); len(calls) != 1 || calls[0] != "readmit:"+task.ID {
		t.Errorf("call log = %v, want exactly one readmit", calls)
	}
	if task.Status != orchestration.TaskStatusPending {
		t.Errorf("task status = %q, want pending", task.Status)
	}
}

func TestAttentionService_Resolve_CloseAsDoneClosesIssueAndCompletesWithoutReadmitting(t *testing.T) {
	task := blockedFixtureTask(fixtureOwnerRulingIssueBody, orchestration.TaskBlockKindBlocked)
	f := newAttentionFixture(t, task)

	if _, err := f.service.Resolve(context.Background(), testTenant, task.ID, AttentionResolution{Action: AttentionCloseAsDone}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if len(f.forge.closed) != 1 || f.forge.closed[0] != 77 {
		t.Errorf("closed issues = %v, want [77]", f.forge.closed)
	}
	calls := f.tasks.callLog()
	if len(calls) != 1 || calls[0] != "complete:"+task.ID {
		t.Fatalf("call log = %v, want exactly one complete and NO readmit", calls)
	}
	if task.Status != orchestration.TaskStatusCompleted {
		t.Errorf("task status = %q, want completed", task.Status)
	}
}

// TestAttentionService_Resolve_CloseAsDoneStripsEveryParkLabel pins X19: a
// closed, completed issue must not go on saying a person owns it. On the
// burn-in node go-truststrap#23 and #27 were closed as done and kept
// factory:escalated and task:blocked. Labels that are not park labels are
// left alone, and no trigger label is added, because nothing is queued.
func TestAttentionService_Resolve_CloseAsDoneStripsEveryParkLabel(t *testing.T) {
	task := blockedFixtureTask(fixtureOwnerRulingIssueBody, orchestration.TaskBlockKindEscalated)
	f := newAttentionFixture(t, task)
	f.forge.putIssue(77, "the parked issue", "factory:needs-info", "factory:blocked", HoldLabel, "kind:bug")

	if _, err := f.service.Resolve(context.Background(), testTenant, task.ID, AttentionResolution{Action: AttentionCloseAsDone}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	issue, err := f.forge.GetIssue(context.Background(), RepoRef{}, 77)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if !equalStrings(issue.Labels, []string{"kind:bug"}) {
		t.Errorf("labels on the closed issue = %v, want only [kind:bug]", issue.Labels)
	}
	if issue.State != "closed" {
		t.Errorf("issue state = %q, want closed", issue.State)
	}
	if task.Status != orchestration.TaskStatusCompleted {
		t.Errorf("task status = %q, want completed", task.Status)
	}
}

// TestAttentionService_Resolve_CloseAsDoneKeepsTheTaskParkedWhenALabelWontComeOff
// pins the ordering: a park label that cannot be removed leaves the issue
// open and the task blocked, so the operator's retry converges instead of
// leaving a closed issue that still reads "blocked".
func TestAttentionService_Resolve_CloseAsDoneKeepsTheTaskParkedWhenALabelWontComeOff(t *testing.T) {
	task := blockedFixtureTask(fixtureOwnerRulingIssueBody, orchestration.TaskBlockKindBlocked)
	f := newAttentionFixture(t, task)
	f.forge.putIssue(77, "the parked issue", HoldLabel)
	f.forge.removeLabelErr = errors.New("forge refused the label removal")

	if _, err := f.service.Resolve(context.Background(), testTenant, task.ID, AttentionResolution{Action: AttentionCloseAsDone}); err == nil {
		t.Fatal("Resolve with a failing label removal: want an error, got none")
	}
	if len(f.forge.closed) != 0 {
		t.Errorf("closed issues = %v, want none: the labels come off before the issue closes", f.forge.closed)
	}
	if calls := f.tasks.callLog(); len(calls) != 0 {
		t.Errorf("call log = %v, want the task untouched", calls)
	}
	if task.Status != orchestration.TaskStatusBlocked {
		t.Errorf("task status = %q, want it to stay blocked", task.Status)
	}
}

// TestAttentionService_Resolve_CloseAsDonePostsTheOperatorsContext pins that
// the evidence an operator gives for "done" reaches the issue, not only the
// task record: the forge comment is the ruling people read.
func TestAttentionService_Resolve_CloseAsDonePostsTheOperatorsContext(t *testing.T) {
	tests := []struct {
		name     string
		answer   string
		wantText string
	}{
		{name: "context is posted", answer: "  Landed with #19 through bfe8cf8.  ", wantText: "Landed with #19 through bfe8cf8."},
		{name: "blank context posts the stock sentence", answer: "   ", wantText: "No further work is needed."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := blockedFixtureTask(fixtureOwnerRulingIssueBody, orchestration.TaskBlockKindEscalated)
			f := newAttentionFixture(t, task)

			if _, err := f.service.Resolve(context.Background(), testTenant, task.ID, AttentionResolution{Action: AttentionCloseAsDone, Answer: tt.answer}); err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			comments := f.forge.commentsOn(77)
			if len(comments) != 1 {
				t.Fatalf("comments on issue 77 = %d, want 1", len(comments))
			}
			if !strings.Contains(comments[0].Body, tt.wantText) {
				t.Errorf("comment body = %q, want it to contain %q", comments[0].Body, tt.wantText)
			}
			if !strings.Contains(comments[0].Body, "The issue is closed and the task is marked done.") {
				t.Errorf("comment body = %q, want it to state the outcome", comments[0].Body)
			}
		})
	}
}

// TestAttentionService_Resolve_CloseAsDoneOnTheRealEngine drives close_as_done
// through the real AgentService and task store rather than a fake, because a
// fake is exactly what hid that CompleteTask refuses a blocked task: the
// forge issue was closed and the task stayed parked.
func TestAttentionService_Resolve_CloseAsDoneOnTheRealEngine(t *testing.T) {
	rig := newEngineRig(t)
	task := rig.park(t, "the owner must decide whether this is still wanted")

	svc, err := NewAttentionService(AttentionServiceConfig{
		Tasks: rig.svc, Bindings: rig.hooks.store, Forges: &fakeForges{forge: rig.hooks.forge},
		Projects: &fakeProjects{projects: map[string]*orchestration.Project{testProject: testOrchProject()}},
	})
	if err != nil {
		t.Fatalf("NewAttentionService: %v", err)
	}
	if _, err := svc.Resolve(rig.ctx, testTenant, task.ID, AttentionResolution{Action: AttentionCloseAsDone}); err != nil {
		t.Fatalf("Resolve close_as_done: %v", err)
	}

	got, err := rig.tasks.GetTask(rig.ctx, testTenant, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != orchestration.TaskStatusCompleted {
		t.Fatalf("task status = %q, want completed", got.Status)
	}
	if got.ConclusionReason == orchestration.TaskConcludedReadmitted {
		t.Error("close_as_done readmitted the task")
	}
}

func TestAttentionService_Resolve_AllowAnotherRoundBumpsBeforeReadmitting(t *testing.T) {
	task := blockedFixtureTask(fixtureReviewRoundsExhausted, orchestration.TaskBlockKindEscalated)
	f := newAttentionFixture(t, task)

	if _, err := f.service.Resolve(context.Background(), testTenant, task.ID, AttentionResolution{Action: AttentionAllowAnotherRound}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	wantOrder := []string{"bump:" + task.ID, "readmit:" + task.ID}
	if calls := f.tasks.callLog(); !equalStrings(calls, wantOrder) {
		t.Fatalf("call log = %v, want %v (bump BEFORE readmit)", calls, wantOrder)
	}
	if task.ReviewRoundBonus != 1 {
		t.Errorf("ReviewRoundBonus = %d, want 1", task.ReviewRoundBonus)
	}
}

func TestAttentionService_Resolve_AllowAnotherRoundPostsTheOperatorsScopeInOneComment(t *testing.T) {
	task := blockedFixtureTask(fixtureReviewRoundsExhausted, orchestration.TaskBlockKindEscalated)
	f := newAttentionFixture(t, task)

	scope := "Fix only the ext2 comment at filesystem.go:21; scope is frozen."
	if _, err := f.service.Resolve(context.Background(), testTenant, task.ID, AttentionResolution{Action: AttentionAllowAnotherRound, Answer: scope}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	comments := f.forge.commentsOn(77)
	if len(comments) != 1 {
		t.Fatalf("comments on issue 77 = %d, want exactly 1 (scope and grant in one ruling)", len(comments))
	}
	body := comments[0].Body
	if !strings.Contains(body, scope) {
		t.Errorf("comment body = %q, want the operator's scope %q", body, scope)
	}
	if !strings.Contains(body, "One more review round is granted") {
		t.Errorf("comment body = %q, want the round grant", body)
	}
	if strings.Index(body, scope) > strings.Index(body, "One more review round is granted") {
		t.Errorf("comment body = %q, want the scope ahead of the grant", body)
	}
	if task.ReviewRoundBonus != 1 {
		t.Errorf("ReviewRoundBonus = %d, want 1", task.ReviewRoundBonus)
	}
}

func TestAttentionService_Resolve_AllowAnotherRoundWithBlankTextPostsOnlyTheGrant(t *testing.T) {
	task := blockedFixtureTask(fixtureReviewRoundsExhausted, orchestration.TaskBlockKindEscalated)
	f := newAttentionFixture(t, task)

	if _, err := f.service.Resolve(context.Background(), testTenant, task.ID, AttentionResolution{Action: AttentionAllowAnotherRound, Answer: "  \n\t "}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	comments := f.forge.commentsOn(77)
	if len(comments) != 1 {
		t.Fatalf("comments on issue 77 = %d, want 1", len(comments))
	}
	want := "**" + attentionRulingActor + "**\n\nOne more review round is granted"
	if !strings.Contains(comments[0].Body, want) {
		t.Errorf("comment body = %q, want the bare grant directly under the actor (%q)", comments[0].Body, want)
	}
}

func TestAttentionService_Resolve_AnswerPostsFreeTextThenReadmits(t *testing.T) {
	task := blockedFixtureTask(fixtureOwnerRulingIssueBody, orchestration.TaskBlockKindBlocked)
	f := newAttentionFixture(t, task)

	if _, err := f.service.Resolve(context.Background(), testTenant, task.ID, AttentionResolution{Action: AttentionAnswer, Answer: "Use the daemon-host mount for now; #981 tracks the real fix."}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	comments := f.forge.commentsOn(77)
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "daemon-host mount") {
		t.Fatalf("comments = %v, want the operator's free text", comments)
	}
	if task.Status != orchestration.TaskStatusPending {
		t.Errorf("task status = %q, want pending", task.Status)
	}
}

func TestAttentionService_Resolve_PlainReadmitPostsAMinimalComment(t *testing.T) {
	task := blockedFixtureTask(fixtureForgeIssueState, orchestration.TaskBlockKindBlocked)
	f := newAttentionFixture(t, task)

	if _, err := f.service.Resolve(context.Background(), testTenant, task.ID, AttentionResolution{Action: AttentionReadmit}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(f.forge.commentsOn(77)) != 1 {
		t.Fatalf("expected a comment recording the ruling even for a bare readmit")
	}
	if task.Status != orchestration.TaskStatusPending {
		t.Errorf("task status = %q, want pending", task.Status)
	}
}

// =============================================================================
// Resolve — refusals
// =============================================================================

func TestAttentionService_Resolve_RefusesUnknownAction(t *testing.T) {
	task := blockedFixtureTask(fixtureOwnerRulingIssueBody, orchestration.TaskBlockKindBlocked)
	f := newAttentionFixture(t, task)
	if _, err := f.service.Resolve(context.Background(), testTenant, task.ID, AttentionResolution{Action: "not-a-real-action"}); err == nil {
		t.Fatal("Resolve with an unknown action: want an error, got none")
	}
}

func TestAttentionService_Resolve_RefusesEmptyAnswer(t *testing.T) {
	task := blockedFixtureTask(fixtureOwnerRulingIssueBody, orchestration.TaskBlockKindBlocked)
	f := newAttentionFixture(t, task)
	if _, err := f.service.Resolve(context.Background(), testTenant, task.ID, AttentionResolution{Action: AttentionAnswer, Answer: "   "}); err == nil {
		t.Fatal("Resolve(answer, blank text): want an error, got none")
	}
}

func TestAttentionService_Resolve_RefusesATaskThatIsNotBlocked(t *testing.T) {
	task := testTaskRecord(77)
	task.Status = orchestration.TaskStatusPending
	f := newAttentionFixture(t, task)
	_, err := f.service.Resolve(context.Background(), testTenant, task.ID, AttentionResolution{Action: AttentionReadmit})
	var invalidState *orchestration.TaskInvalidStateError
	if !errors.As(err, &invalidState) {
		t.Fatalf("Resolve on a pending task: got %v, want a TaskInvalidStateError", err)
	}
}

func TestAttentionService_Resolve_DoesNotReadmitWhenTheForgeCommentFails(t *testing.T) {
	task := blockedFixtureTask(fixtureOwnerRulingIssueBody, orchestration.TaskBlockKindBlocked)
	f := newAttentionFixture(t, task)
	f.forge.commentErr = errors.New("forge unreachable")

	if _, err := f.service.Resolve(context.Background(), testTenant, task.ID, AttentionResolution{Action: AttentionReadmit}); err == nil {
		t.Fatal("Resolve with a failing forge comment: want an error, got none")
	}
	if calls := f.tasks.callLog(); len(calls) != 0 {
		t.Fatalf("call log = %v, want no readmit: the comment must land before the task moves", calls)
	}
	if task.Status != orchestration.TaskStatusBlocked {
		t.Errorf("task status = %q, want it to stay blocked", task.Status)
	}
}

// =============================================================================
// Environmental alerts
// =============================================================================

// fakeAttentionFleet is an AttentionFleetReader over a fixed list.
type fakeAttentionFleet struct {
	agents []*orchestration.FleetNode
}

func (f *fakeAttentionFleet) ListFleetNodesPage(_ context.Context, _ string, filter *orchestration.FleetNodeFilter, _ *orchestration.Page) (*orchestration.PageResult[*orchestration.FleetNode], error) {
	var items []*orchestration.FleetNode
	for _, agent := range f.agents {
		if filter != nil && filter.Status != "" && agent.Status != filter.Status {
			continue
		}
		items = append(items, agent)
	}
	return &orchestration.PageResult[*orchestration.FleetNode]{Items: items, Total: len(items)}, nil
}

// fakeAttentionAccounts is an AttentionAccountReader over a fixed list.
type fakeAttentionAccounts struct{ accounts []*orchestration.Account }

func (f *fakeAttentionAccounts) ListByTenant(_ context.Context, _ string, _ orchestration.AccountProvider, _ string) ([]*orchestration.Account, error) {
	return f.accounts, nil
}

// fakeAttentionBarrier is an AttentionBarrierReader returning a fixed status,
// and records the tenant id it was asked about so a test can prove the
// inbox reads the CALLING tenant's own barrier, not some fixed scope.
type fakeAttentionBarrier struct {
	status AttentionBarrierStatus
	err    error

	gotTenantID string
}

func (f *fakeAttentionBarrier) BarrierStatus(_ context.Context, tenantID string) (AttentionBarrierStatus, error) {
	f.gotTenantID = tenantID
	return f.status, f.err
}

func TestAttentionService_List_ReportsAgentOfflineAlert(t *testing.T) {
	f := newAttentionFixture(t)
	f.service.fleet = &fakeAttentionFleet{agents: []*orchestration.FleetNode{
		{ID: "agent-1", Name: "agent-04", Host: "agent-04.local", Status: orchestration.FleetNodeStatusOffline},
		{ID: "agent-2", Name: "gpu-host", Host: "gpu-host.local", Status: orchestration.FleetNodeStatusIdle},
	}}

	page, err := f.service.List(context.Background(), testTenant, AttentionFilter{}, &orchestration.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1 (only the offline agent, not the idle one)", len(page.Items))
	}
	item := page.Items[0]
	if item.Kind != AttentionFleetNodeOffline || item.ID != "agent_offline:agent-1" {
		t.Errorf("item = %+v, want the agent_offline alert for agent-1", item)
	}
	if len(item.Actions) != 0 {
		t.Errorf("Actions = %v, want none: an environmental alert is not resolved through the task lifecycle", item.Actions)
	}
}

func TestAttentionService_List_ReportsAccountErrorAlert(t *testing.T) {
	f := newAttentionFixture(t)
	f.service.accounts = &fakeAttentionAccounts{accounts: []*orchestration.Account{
		{ID: "acct-1", TenantID: testTenant, Provider: orchestration.AccountProviderClaude, DisplayName: "team-max", Status: orchestration.AccountStatusError, LastError: "refresh: 401"},
		{ID: "acct-2", TenantID: testTenant, Provider: orchestration.AccountProviderClaude, DisplayName: "team-3", Status: orchestration.AccountStatusActive},
	}}

	page, err := f.service.List(context.Background(), testTenant, AttentionFilter{}, &orchestration.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1 (only the erroring account)", len(page.Items))
	}
	if !strings.Contains(page.Items[0].Message, "refresh: 401") {
		t.Errorf("Message = %q, want it to carry the account's last error", page.Items[0].Message)
	}
}

// TestAttentionService_List_ARejectedLoginAsksForARelogin: an account the
// provider refused for its login is fixed by a person logging it in again, and
// the inbox item says so rather than asking for a refresh.
func TestAttentionService_List_ARejectedLoginAsksForARelogin(t *testing.T) {
	f := newAttentionFixture(t)
	f.service.accounts = &fakeAttentionAccounts{accounts: []*orchestration.Account{
		{ID: "acct-1", TenantID: testTenant, Provider: orchestration.AccountProviderCodex, DisplayName: "reviewer",
			Status: orchestration.AccountStatusError, LastError: orchestration.AccountReloginNeededPrefix + "401 Unauthorized"},
	}}

	page, err := f.service.List(context.Background(), testTenant, AttentionFilter{}, &orchestration.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(page.Items))
	}
	item := page.Items[0]
	if !strings.Contains(item.Subject, "re-login") || !strings.Contains(item.Message, "logs it in again") ||
		!strings.Contains(item.Message, "401 Unauthorized") {
		t.Errorf("item = %q / %q, want a re-login subject and the provider's refusal", item.Subject, item.Message)
	}
}

func TestAttentionService_List_ReportsBarrierSealedAlertOnlyWhenSealed(t *testing.T) {
	f := newAttentionFixture(t)
	unsealed := &fakeAttentionBarrier{status: AttentionBarrierStatus{Scope: testTenant, Sealed: false}}
	f.service.barrier = unsealed

	page, err := f.service.List(context.Background(), testTenant, AttentionFilter{}, &orchestration.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("Items = %v, want none: the barrier is not sealed", page.Items)
	}
	if unsealed.gotTenantID != testTenant {
		t.Errorf("BarrierStatus asked about tenant %q, want the calling tenant %q", unsealed.gotTenantID, testTenant)
	}

	sealed := &fakeAttentionBarrier{status: AttentionBarrierStatus{Scope: testTenant, Initialized: true, Sealed: true}}
	f.service.barrier = sealed
	page, err = f.service.List(context.Background(), testTenant, AttentionFilter{}, &orchestration.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Kind != AttentionBarrierSealed {
		t.Fatalf("Items = %v, want one barrier_sealed alert", page.Items)
	}
	if sealed.gotTenantID != testTenant {
		t.Errorf("BarrierStatus asked about tenant %q, want the calling tenant %q", sealed.gotTenantID, testTenant)
	}
}

// TestAttentionService_List_ATenantWithNoBarrierOfItsOwnIsNotSealed is X3:
// qrdb answers Sealed=true, Initialized=false for a tenant that never had a
// per-tenant barrier, and that tenant's data runs under the system barrier —
// so no alert.
func TestAttentionService_List_ATenantWithNoBarrierOfItsOwnIsNotSealed(t *testing.T) {
	f := newAttentionFixture(t)
	f.service.barrier = &fakeAttentionBarrier{status: AttentionBarrierStatus{Scope: testTenant, Initialized: false, Sealed: true}}

	page, err := f.service.List(context.Background(), testTenant, AttentionFilter{Kind: AttentionBarrierSealed}, &orchestration.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("Items = %+v, want no barrier_sealed alert for a tenant with no barrier of its own", page.Items)
	}
}

func TestAttentionService_List_BarrierReaderErrorIsSurfacedNotSwallowed(t *testing.T) {
	// An error from the barrier reader (e.g. the embedded qrdb node
	// unreachable) must fail the whole List call, exactly like a fleet or
	// account reader error — swallowing it would render an inbox that looks
	// empty when it is actually broken.
	f := newAttentionFixture(t)
	wantErr := errors.New("qrdb: tenant client unavailable")
	f.service.barrier = &fakeAttentionBarrier{err: wantErr}

	_, err := f.service.List(context.Background(), testTenant, AttentionFilter{}, &orchestration.Page{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("List error = %v, want it to wrap %v", err, wantErr)
	}
}

func TestAttentionService_List_UnwiredAlertSourcesAreSilentlyOmitted(t *testing.T) {
	// Fleet, Accounts and Barrier are all nil in newAttentionFixture: the
	// listing must not error just because an operator has not wired the
	// optional alert sources yet.
	f := newAttentionFixture(t)
	page, err := f.service.List(context.Background(), testTenant, AttentionFilter{}, &orchestration.Page{})
	if err != nil {
		t.Fatalf("List with no alert sources wired: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("Items = %v, want none", page.Items)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
