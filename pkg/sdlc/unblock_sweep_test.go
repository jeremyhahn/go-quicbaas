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

package sdlc

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Fixture
// =============================================================================

// fakeBlockedTasks is a BlockedTaskService over a slice that records every
// readmission, so a test can assert on what MOVED rather than only on what
// the report said moved.
type fakeBlockedTasks struct {
	mu         sync.Mutex
	tasks      []*orchestration.Task
	readmits   []string
	listErr    error
	readmitErr error
}

func (f *fakeBlockedTasks) ListTasks(_ context.Context, tenantID string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	matched := make([]*orchestration.Task, 0, len(f.tasks))
	for _, task := range f.tasks {
		if task.TenantID != tenantID {
			continue
		}
		if filter != nil && filter.ProjectID != "" && task.ProjectID != filter.ProjectID {
			continue
		}
		if filter != nil && filter.Status != "" && task.Status != filter.Status {
			continue
		}
		// A COPY, because a real store deserializes each listing: a caller
		// holding a snapshot must not see a later transition through it, and
		// a double that shared the record would hide every staleness bug a
		// snapshot can have.
		copied := *task
		matched = append(matched, &copied)
	}
	return matched, nil
}

// adopt adds a task to the store while a sweep is in flight, the way the
// task-source poller's CreateTask does.
func (f *fakeBlockedTasks) adopt(task *orchestration.Task) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks = append(f.tasks, task)
}

// conclude moves a task the store already holds to a resting state, the way
// applyConclusion does: the SAME record transitions, it is not a second task.
func (f *fakeBlockedTasks) conclude(id string, kind orchestration.TaskBlockKind, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, task := range f.tasks {
		if task.ID != id {
			continue
		}
		task.Status = orchestration.TaskStatusBlocked
		task.BlockKind = kind
		task.BlockedReason = reason
		// The engine's park persists the reason with the kind, in the same
		// write. A double that set one and not the other would let a stale
		// reason read as current, which is exactly the staleness these tests
		// exist to catch.
		if conclusion, named := orchestration.ConclusionOfBlockKind(kind); named {
			task.ConclusionReason = conclusion
		}
	}
}

// forget drops a task from the store, the way an operator's delete does.
func (f *fakeBlockedTasks) forget(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.tasks[:0]
	for _, task := range f.tasks {
		if task.ID != id {
			kept = append(kept, task)
		}
	}
	f.tasks = kept
}

// failListing makes every later listing refuse, the way a store that went away
// mid-sweep does.
func (f *fakeBlockedTasks) failListing(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listErr = err
}

func (f *fakeBlockedTasks) ReadmitTask(_ context.Context, tenantID, taskID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readmitErr != nil {
		return f.readmitErr
	}
	f.readmits = append(f.readmits, taskID)
	for _, task := range f.tasks {
		if task.ID == taskID && task.TenantID == tenantID {
			task.Status = orchestration.TaskStatusPending
			task.BlockedReason = ""
			task.BlockKind = ""
		}
	}
	return nil
}

func (f *fakeBlockedTasks) readmitted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.readmits...)
}

// fakeBindings is a BindingReader over one binding.
type fakeBindings struct {
	binding *ProjectBinding
	err     error
}

func (b *fakeBindings) GetBinding(_ context.Context, _, projectID string) (*ProjectBinding, error) {
	if b.err != nil {
		return nil, b.err
	}
	if b.binding == nil {
		return nil, &NotFoundError{Entity: "binding", ID: projectID}
	}
	return b.binding, nil
}

func (b *fakeBindings) ListBindings(context.Context, string) ([]*ProjectBinding, error) {
	if b.binding == nil {
		return nil, b.err
	}
	return []*ProjectBinding{b.binding}, b.err
}

// sweepFixture is one project with a forge and a blocked-task store.
type sweepFixture struct {
	sweeper *UnblockSweeper
	tasks   *fakeBlockedTasks
	forge   *fakeForge
	next    int
}

func newSweepFixture(t *testing.T) *sweepFixture {
	t.Helper()
	f := &sweepFixture{
		tasks: &fakeBlockedTasks{},
		forge: newFakeForge(),
		next:  1,
	}
	sweeper, err := NewUnblockSweeper(UnblockSweeperConfig{
		Tasks:    f.tasks,
		Bindings: &fakeBindings{binding: testBinding()},
		Forges:   &fakeForges{forge: f.forge},
		Now:      func() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewUnblockSweeper: %v", err)
	}
	f.sweeper = sweeper
	return f
}

// blocked seeds one blocked task carrying reason and returns it.
func (f *sweepFixture) blocked(reason string) *orchestration.Task {
	issue := 40 + f.next
	task := testTaskRecord(issue)
	task.ID = "task-" + strconv.Itoa(f.next)
	task.Status = orchestration.TaskStatusBlocked
	task.BlockedReason = reason
	task.BlockKind = orchestration.TaskBlockKindBlocked
	f.next++
	f.forge.putIssue(issue, "issue "+strconv.Itoa(issue))
	f.tasks.mu.Lock()
	f.tasks.tasks = append(f.tasks.tasks, task)
	f.tasks.mu.Unlock()
	return task
}

// sweep runs one pass over the fixture's project.
func (f *sweepFixture) sweep(t *testing.T) UnblockReport {
	t.Helper()
	report, err := f.sweeper.SweepProject(context.Background(), testTenant, testProject)
	if err != nil {
		t.Fatalf("SweepProject: %v", err)
	}
	return report
}

// onlyFinding asserts the pass produced exactly one finding and returns it.
func onlyFinding(t *testing.T, report UnblockReport) UnblockFinding {
	t.Helper()
	if len(report.Findings) != 1 {
		t.Fatalf("pass produced %d findings, want 1: %+v", len(report.Findings), report.Findings)
	}
	return report.Findings[0]
}

// mustCompose composes a reason or fails the test.
func mustCompose(t *testing.T, condition UnblockCondition, detail string) string {
	t.Helper()
	reason, err := ComposeBlockedReason(condition, detail)
	if err != nil {
		t.Fatalf("ComposeBlockedReason(%+v): %v", condition, err)
	}
	return reason
}

// greenCheck is a finished, successful run on head.
func greenCheck(head string) ForgeCheckRun {
	return ForgeCheckRun{ID: "1", Name: "build", HeadSHA: head, Status: "completed", Conclusion: "success"}
}

// =============================================================================
// Construction
// =============================================================================

func TestNewUnblockSweeper_RefusesAMissingCollaborator(t *testing.T) {
	full := UnblockSweeperConfig{
		Tasks:    &fakeBlockedTasks{},
		Bindings: &fakeBindings{binding: testBinding()},
		Forges:   &fakeForges{forge: newFakeForge()},
	}
	cases := map[string]func(*UnblockSweeperConfig){
		"tasks":    func(c *UnblockSweeperConfig) { c.Tasks = nil },
		"bindings": func(c *UnblockSweeperConfig) { c.Bindings = nil },
		"forges":   func(c *UnblockSweeperConfig) { c.Forges = nil },
	}
	for name, strip := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := full
			strip(&cfg)
			sweeper, err := NewUnblockSweeper(cfg)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("NewUnblockSweeper without %s = %v", name, err)
			}
			if sweeper != nil {
				t.Fatal("a refused construction returned a sweeper")
			}
		})
	}
	sweeper, err := NewUnblockSweeper(full)
	if err != nil || sweeper == nil || sweeper.now == nil {
		t.Fatalf("NewUnblockSweeper = %v, %v; a nil clock must take time.Now", sweeper, err)
	}
}

// TestNewUnblockSweeper_RefusesTheEngineTaskStore is go-quicbaas#842 at the
// wiring seam: the store's ReadmitTask makes the same transition in silence,
// so a sweeper built over it would return tasks to the queue leaving every
// issue labelled blocked. The store type-asserts, so the construction can say
// so instead of discovering it in production.
func TestNewUnblockSweeper_RefusesTheEngineTaskStore(t *testing.T) {
	sweeper, err := NewUnblockSweeper(UnblockSweeperConfig{
		Tasks:    orchestration.NewTaskStore(orchestration.NewMemoryTaskKV(), orchestration.NewProcessClaimArbiter()),
		Bindings: &fakeBindings{binding: testBinding()},
		Forges:   &fakeForges{forge: newFakeForge()},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("NewUnblockSweeper over the engine's task store = %v, want an invalid-input refusal", err)
	}
	if sweeper != nil {
		t.Fatal("a refused construction returned a sweeper")
	}
	if !strings.Contains(err.Error(), "AgentService") {
		t.Fatalf("the refusal does not name the seam to wire instead: %v", err)
	}
}

func TestSweepProject_RefusesAnUnscopedPass(t *testing.T) {
	f := newSweepFixture(t)
	for name, call := range map[string][2]string{
		"no tenant":  {"", testProject},
		"no project": {testTenant, ""},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := f.sweeper.SweepProject(context.Background(), call[0], call[1]); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("SweepProject = %v, want an invalid-input refusal", err)
			}
		})
	}
}

// =============================================================================
// The sweep
// =============================================================================

// TestSweepProject_ReleasesASatisfiedIssueDependency is the whole point: a
// condition a check can decide clears without anybody re-labelling.
func TestSweepProject_ReleasesASatisfiedIssueDependency(t *testing.T) {
	f := newSweepFixture(t)
	dependency := f.forge.putIssue(900, "the dependency")
	dependency.State = "closed"

	reason := mustCompose(t, UnblockCondition{
		Kind: UnblockForgeIssueState, Subject: IssueRefString(testRepoRef, 900), Target: "closed",
	}, "needs the SDK route that lands there")
	task := f.blocked(reason)

	report := f.sweep(t)
	finding := onlyFinding(t, report)
	if finding.Verdict != UnblockReleased {
		t.Fatalf("verdict is %s (%s), want released", finding.Verdict, finding.Evidence)
	}
	if finding.Kind != UnblockForgeIssueState {
		t.Fatalf("finding kind is %q", finding.Kind)
	}
	released := report.Released()
	if len(released) != 1 || released[0].TaskID != task.ID {
		t.Fatalf("Released() reported %+v for a pass that released one task", released)
	}
	if got := f.tasks.readmitted(); len(got) != 1 || got[0] != task.ID {
		t.Fatalf("the STORE was asked to readmit %v; the report said released", got)
	}
}

// TestSweepProject_KeepsAnUnsatisfiedDependencyBlocked asserts on the store
// as well as the verdict: a report is not the record.
func TestSweepProject_KeepsAnUnsatisfiedDependencyBlocked(t *testing.T) {
	f := newSweepFixture(t)
	f.forge.putIssue(901, "still open")

	reason := mustCompose(t, UnblockCondition{
		Kind: UnblockForgeIssueState, Subject: IssueRefString(testRepoRef, 901), Target: "closed",
	}, "needs the dependency")
	task := f.blocked(reason)

	finding := onlyFinding(t, f.sweep(t))
	if finding.Verdict != UnblockWaiting {
		t.Fatalf("verdict is %s (%s), want waiting", finding.Verdict, finding.Evidence)
	}
	if len(f.tasks.readmitted()) != 0 {
		t.Fatalf("an unsatisfied condition readmitted %v", f.tasks.readmitted())
	}
	if task.Status != orchestration.TaskStatusBlocked || task.BlockedReason != reason {
		t.Fatalf("the record moved under an unsatisfied condition: %s %q", task.Status, task.BlockedReason)
	}
}

// TestSweepProject_DanglingDependencyIsUndecidableNotWaiting asserts the
// REASON rather than the outcome.
//
// Both verdicts leave the task blocked, so an assertion on the record alone
// passes either way. The difference is what a person is told: waiting means
// the factory is working and the dependency has not landed, undecidable
// means the condition names a record that does not exist and never will.
func TestSweepProject_DanglingDependencyIsUndecidableNotWaiting(t *testing.T) {
	f := newSweepFixture(t)
	reason := mustCompose(t, UnblockCondition{
		Kind: UnblockForgeIssueState, Subject: IssueRefString(testRepoRef, 999999), Target: "closed",
	}, "needs an issue that is not there")
	f.blocked(reason)

	finding := onlyFinding(t, f.sweep(t))
	if finding.Verdict != UnblockUndecidable {
		t.Fatalf("verdict is %s (%s), want undecidable", finding.Verdict, finding.Evidence)
	}
	if len(f.tasks.readmitted()) != 0 {
		t.Fatal("an undecidable condition released a task")
	}
}

// TestSweepProject_HumanConditionsAreNeverReleased covers both person-decided
// kinds and insists the finding names WHO.
func TestSweepProject_HumanConditionsAreNeverReleased(t *testing.T) {
	for _, tc := range []struct {
		kind     UnblockKind
		awaiting string
	}{
		{UnblockOwnerRuling, "the owner"},
		{UnblockOperatorRepair, "an operator"},
	} {
		t.Run(tc.kind.String(), func(t *testing.T) {
			f := newSweepFixture(t)
			f.blocked(mustCompose(t, UnblockCondition{Kind: tc.kind}, "somebody must decide"))

			finding := onlyFinding(t, f.sweep(t))
			if finding.Verdict != UnblockAwaitingHuman {
				t.Fatalf("verdict is %s (%s), want awaiting_human", finding.Verdict, finding.Evidence)
			}
			if !strings.Contains(finding.Evidence, tc.awaiting) {
				t.Fatalf("the evidence does not name who is being waited on: %q", finding.Evidence)
			}
			if len(f.tasks.readmitted()) != 0 {
				t.Fatal("a condition only a person can clear was cleared by the sweep")
			}
		})
	}
}

// TestSweepProject_ProseReasonAwaitsAHumanDiagnosis uses the engine's own
// park text, because that is what the sweep meets today.
//
// It is NOT reported as a defect. The engine blocks a reaped death because
// nothing diagnosed it, so there is no condition to state; attaching a kind
// would fabricate a diagnosis nobody made. What the task awaits is a person
// reading the evidence it carries, and that is what the verdict says.
func TestSweepProject_ProseReasonAwaitsAHumanDiagnosis(t *testing.T) {
	f := newSweepFixture(t)
	const prose = "the worker died and nothing diagnosed it\n\nThe attempt budget is spent; readmit the task to try again."
	f.blocked(prose)

	finding := onlyFinding(t, f.sweep(t))
	if finding.Verdict != UnblockAwaitingDiagnosis {
		t.Fatalf("verdict is %s (%s), want awaiting_diagnosis", finding.Verdict, finding.Evidence)
	}
	if finding.Kind != "" {
		t.Fatalf("a finding with no condition reported kind %q", finding.Kind)
	}
	if !strings.Contains(finding.Evidence, "the worker died") {
		t.Fatalf("the evidence a person must diagnose was not quoted: %q", finding.Evidence)
	}
	if len(f.tasks.readmitted()) != 0 {
		t.Fatal("a task with no condition was released")
	}
}

func TestSweepProject_MalformedConditionIsReportedAsUnreadable(t *testing.T) {
	f := newSweepFixture(t)
	f.blocked("blocked " + unblockMarkerOpen + "kind=forge_issue_state;subject=acme/api#7" + unblockMarkerClose)

	finding := onlyFinding(t, f.sweep(t))
	if finding.Verdict != UnblockUnreadable {
		t.Fatalf("verdict is %s (%s), want unreadable", finding.Verdict, finding.Evidence)
	}
	if len(f.tasks.readmitted()) != 0 {
		t.Fatal("an unreadable condition released a task")
	}
}

// TestSweepProject_SatisfiedButBudgetExhaustedStaysBlocked keeps the two
// reasons a task can fail to be readmitted apart.
func TestSweepProject_SatisfiedButBudgetExhaustedStaysBlocked(t *testing.T) {
	f := newSweepFixture(t)
	dependency := f.forge.putIssue(902, "landed")
	dependency.State = "closed"

	task := f.blocked(mustCompose(t, UnblockCondition{
		Kind: UnblockForgeIssueState, Subject: IssueRefString(testRepoRef, 902), Target: "closed",
	}, "needs the dependency"))
	task.Attempts = task.AttemptBudget

	finding := onlyFinding(t, f.sweep(t))
	if finding.Verdict != UnblockBudgetExhausted {
		t.Fatalf("verdict is %s (%s), want budget_exhausted", finding.Verdict, finding.Evidence)
	}
	if !strings.Contains(finding.Evidence, "satisfied") {
		t.Fatalf("the evidence hides that the condition cleared: %q", finding.Evidence)
	}
	if len(f.tasks.readmitted()) != 0 {
		t.Fatal("a task with no attempts left re-entered a queue that will refuse it")
	}
}

// TestSweepProject_PipelineConditionNeedsAGreenRun uses fixtures the naive
// check gets wrong.
//
// A red run for the head is PRESENT in the unsatisfied case, so a check
// written as "are there runs for this commit" — or as "is there no failure
// yet" — answers the same as the correct one on an empty fixture and
// differently here.
func TestSweepProject_PipelineConditionNeedsAGreenRun(t *testing.T) {
	const head = "c0ffee1234"
	cases := []struct {
		name    string
		runs    []ForgeCheckRun
		verdict UnblockVerdict
	}{
		{"no run at all does not clear it", nil, UnblockWaiting},
		{"a failed run does not clear it",
			[]ForgeCheckRun{{ID: "1", Name: "build", HeadSHA: head, Status: "completed", Conclusion: "failure"}},
			UnblockWaiting},
		{"an unfinished run does not clear it",
			[]ForgeCheckRun{{ID: "1", Name: "build", HeadSHA: head, Status: "in_progress"}},
			UnblockWaiting},
		{"one green among a red does not clear it",
			[]ForgeCheckRun{greenCheck(head), {ID: "2", Name: "lint", HeadSHA: head, Status: "completed", Conclusion: "failure"}},
			UnblockWaiting},
		{"every check green clears it", []ForgeCheckRun{greenCheck(head)}, UnblockReleased},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSweepFixture(t)
			f.forge.checks[head] = tc.runs
			f.blocked(mustCompose(t, UnblockCondition{Kind: UnblockPipelineGreen, Subject: head}, "needs green CI"))

			finding := onlyFinding(t, f.sweep(t))
			if finding.Verdict != tc.verdict {
				t.Fatalf("verdict is %s (%s), want %s", finding.Verdict, finding.Evidence, tc.verdict)
			}
			if released := len(f.tasks.readmitted()) == 1; released != tc.verdict.Released() {
				t.Fatalf("readmitted=%v with verdict %s", released, tc.verdict)
			}
		})
	}
}

// TestSweepProject_UndecidedKindIsUnreadableNotUndecidable keeps a gap in
// THIS package apart from a fault in the world.
//
// Both leave the task blocked. They differ in who has to act: an undecidable
// condition waits on an outage clearing, an unhandled kind waits on somebody
// finishing the kind — and reporting the second as the first sends an
// operator looking for an outage that does not exist.
func TestSweepProject_UndecidedKindIsUnreadableNotUndecidable(t *testing.T) {
	const undecided UnblockKind = "test_sweep_undecided"
	describe := func(UnblockCondition) string { return "a kind nobody finished" }
	// Composed while the kind is still whole, so the STORED text is what a
	// record written before its decider was lost looks like.
	unblockKinds[undecided] = unblockKindSpec{awaiting: "nobody", describe: describe}
	reason, err := ComposeBlockedReason(UnblockCondition{Kind: undecided}, "written when it was whole")
	if err != nil {
		t.Fatalf("ComposeBlockedReason: %v", err)
	}
	unblockKinds[undecided] = unblockKindSpec{describe: describe}
	t.Cleanup(func() { delete(unblockKinds, undecided) })

	f := newSweepFixture(t)
	f.blocked(reason)

	finding := onlyFinding(t, f.sweep(t))
	if finding.Verdict != UnblockUnreadable {
		t.Fatalf("verdict is %s (%s), want unreadable", finding.Verdict, finding.Evidence)
	}
}

// TestSweepProject_ReportsEveryBlockedTaskOrderedById covers the pass shape:
// every blocked task appears, nothing else does, and the order is stable.
func TestSweepProject_ReportsEveryBlockedTaskOrderedById(t *testing.T) {
	f := newSweepFixture(t)
	for i := 0; i < 3; i++ {
		f.blocked("blocked with no condition, task " + strconv.Itoa(i))
	}
	// A task in another state must not appear: the sweep is about blocked work.
	running := testTaskRecord(77)
	running.ID = "task-running"
	f.tasks.tasks = append(f.tasks.tasks, running)
	// Nor must a blocked task in another project.
	other := testTaskRecord(78)
	other.ID = "task-other-project"
	other.ProjectID = "another-project"
	other.Status = orchestration.TaskStatusBlocked
	f.tasks.tasks = append(f.tasks.tasks, other)

	report := f.sweep(t)
	if len(report.Findings) != 3 {
		t.Fatalf("pass produced %d findings, want 3: %+v", len(report.Findings), report.Findings)
	}
	if report.Count(UnblockAwaitingDiagnosis) != 3 {
		t.Fatalf("awaiting-diagnosis count is %d, want 3", report.Count(UnblockAwaitingDiagnosis))
	}
	if len(report.Released()) != 0 {
		t.Fatalf("a pass that released nothing reported %d releases", len(report.Released()))
	}
	for i := 1; i < len(report.Findings); i++ {
		if report.Findings[i-1].TaskID >= report.Findings[i].TaskID {
			t.Fatalf("findings are not ordered by task id: %+v", report.Findings)
		}
	}
}

// TestSweepProject_AFaultThatEndsThePassIsReturnedAsAnError separates the
// two error classes: a store that cannot be read or written ends the pass,
// while a condition that cannot be decided is a finding.
func TestSweepProject_AFaultThatEndsThePassIsReturnedAsAnError(t *testing.T) {
	listing := newSweepFixture(t)
	listing.tasks.listErr = &NotFoundError{Entity: "project", ID: testProject}
	if _, err := listing.sweeper.SweepProject(context.Background(), testTenant, testProject); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a store that cannot list produced %v", err)
	}

	writing := newSweepFixture(t)
	dependency := writing.forge.putIssue(903, "landed")
	dependency.State = "closed"
	writing.blocked(mustCompose(t, UnblockCondition{
		Kind: UnblockForgeIssueState, Subject: IssueRefString(testRepoRef, 903), Target: "closed",
	}, "needs the dependency"))
	writing.tasks.readmitErr = &NotFoundError{Entity: "task", ID: "task-1"}
	if _, err := writing.sweeper.SweepProject(context.Background(), testTenant, testProject); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a readmission that could not be written produced %v", err)
	}
}

// TestSweepProject_TheForgeIsResolvedOnlyWhenAConditionNeedsIt is a property
// about availability: a project whose blocked work all awaits a person must
// not have its sweep fail because the forge is down.
func TestSweepProject_TheForgeIsResolvedOnlyWhenAConditionNeedsIt(t *testing.T) {
	sweeper, err := NewUnblockSweeper(UnblockSweeperConfig{
		Tasks:    &fakeBlockedTasks{},
		Bindings: &fakeBindings{err: &NotFoundError{Entity: "binding", ID: testProject}},
		Forges:   &fakeForges{forge: newFakeForge()},
	})
	if err != nil {
		t.Fatalf("NewUnblockSweeper: %v", err)
	}
	store := sweeper.tasks.(*fakeBlockedTasks)
	human := testTaskRecord(50)
	human.ID = "task-human"
	human.Status = orchestration.TaskStatusBlocked
	human.BlockedReason = mustCompose(t, UnblockCondition{Kind: UnblockOwnerRuling}, "the owner must choose")
	store.tasks = append(store.tasks, human)

	report, err := sweeper.SweepProject(context.Background(), testTenant, testProject)
	if err != nil {
		t.Fatalf("a pass over person-decided blocks failed on an unreachable binding: %v", err)
	}
	if finding := onlyFinding(t, report); finding.Verdict != UnblockAwaitingHuman {
		t.Fatalf("verdict is %s (%s)", finding.Verdict, finding.Evidence)
	}

	// A checkable condition in the same project DOES need the binding, and
	// reports the failure as undecidable rather than releasing the task.
	checkable := testTaskRecord(51)
	checkable.ID = "task-checkable"
	checkable.Status = orchestration.TaskStatusBlocked
	checkable.BlockedReason = mustCompose(t, UnblockCondition{Kind: UnblockPipelineGreen, Subject: "cafe"}, "needs CI")
	store.tasks = append(store.tasks, checkable)

	report, err = sweeper.SweepProject(context.Background(), testTenant, testProject)
	if err != nil {
		t.Fatalf("SweepProject: %v", err)
	}
	if report.Count(UnblockUndecidable) != 1 {
		t.Fatalf("an unreachable binding did not make the checkable condition undecidable: %+v", report.Findings)
	}
}

// TestSweepProject_SubjectThatNamesNoRecordIsUndecidable covers a reference
// that is well formed as text and names nothing.
//
// The charset guard cannot catch this: "41" contains no reserved character,
// so it encodes and parses cleanly and only the check can find it.
func TestSweepProject_SubjectThatNamesNoRecordIsUndecidable(t *testing.T) {
	f := newSweepFixture(t)
	f.blocked(mustCompose(t, UnblockCondition{
		Kind: UnblockForgeIssueState, Subject: "41", Target: "closed",
	}, "names something that is not an issue reference"))

	finding := onlyFinding(t, f.sweep(t))
	if finding.Verdict != UnblockUndecidable {
		t.Fatalf("verdict is %s (%s), want undecidable", finding.Verdict, finding.Evidence)
	}
	if len(f.tasks.readmitted()) != 0 {
		t.Fatal("a subject naming nothing released a task")
	}
}

// TestSweepProject_AForgeOutageIsUndecidableNotWaiting: a forge that cannot
// be read has not said the condition is unmet, and reporting it as waiting
// would hide an outage behind ordinary patience.
func TestSweepProject_AForgeOutageIsUndecidableNotWaiting(t *testing.T) {
	f := newSweepFixture(t)
	f.forge.listChecksErr = &NotFoundError{Entity: "commit", ID: "cafe"}
	f.blocked(mustCompose(t, UnblockCondition{Kind: UnblockPipelineGreen, Subject: "cafe"}, "needs green CI"))

	finding := onlyFinding(t, f.sweep(t))
	if finding.Verdict != UnblockUndecidable {
		t.Fatalf("verdict is %s (%s), want undecidable", finding.Verdict, finding.Evidence)
	}
	if !strings.Contains(finding.Evidence, "stays blocked") {
		t.Fatalf("the evidence does not say the task did not move: %q", finding.Evidence)
	}
}

// TestSweepProject_AnUnregisteredForgeIsUndecidable covers the resolver that
// answers with neither an adapter nor an error.
func TestSweepProject_AnUnregisteredForgeIsUndecidable(t *testing.T) {
	sweeper, err := NewUnblockSweeper(UnblockSweeperConfig{
		Tasks:    &fakeBlockedTasks{},
		Bindings: &fakeBindings{binding: testBinding()},
		Forges:   &fakeForges{},
	})
	if err != nil {
		t.Fatalf("NewUnblockSweeper: %v", err)
	}
	store := sweeper.tasks.(*fakeBlockedTasks)
	task := testTaskRecord(52)
	task.ID = "task-unregistered"
	task.Status = orchestration.TaskStatusBlocked
	task.BlockedReason = mustCompose(t, UnblockCondition{Kind: UnblockPipelineGreen, Subject: "cafe"}, "needs CI")
	store.tasks = append(store.tasks, task)

	report, err := sweeper.SweepProject(context.Background(), testTenant, testProject)
	if err != nil {
		t.Fatalf("SweepProject: %v", err)
	}
	finding := onlyFinding(t, report)
	if finding.Verdict != UnblockUndecidable || !strings.Contains(finding.Evidence, "forge") {
		t.Fatalf("verdict is %s (%s), want undecidable naming the forge", finding.Verdict, finding.Evidence)
	}

	// A resolver that fails outright is the same class of answer: the
	// condition was not decided, so the task does not move.
	failing, err := NewUnblockSweeper(UnblockSweeperConfig{
		Tasks:    &fakeBlockedTasks{tasks: []*orchestration.Task{task}},
		Bindings: &fakeBindings{binding: testBinding()},
		Forges:   &fakeForges{err: &NotFoundError{Entity: "forge credential", ID: "tok"}},
	})
	if err != nil {
		t.Fatalf("NewUnblockSweeper: %v", err)
	}
	report, err = failing.SweepProject(context.Background(), testTenant, testProject)
	if err != nil {
		t.Fatalf("SweepProject: %v", err)
	}
	if finding := onlyFinding(t, report); finding.Verdict != UnblockUndecidable {
		t.Fatalf("verdict is %s (%s), want undecidable", finding.Verdict, finding.Evidence)
	}
}

// TestSweepProject_ANilTaskEndsThePass: the store handing back a nil record
// is a fault in the store, not a task to skip quietly.
func TestSweepProject_ANilTaskEndsThePass(t *testing.T) {
	f := newSweepFixture(t)
	f.sweeper.tasks = nilListingTasks{}
	if _, err := f.sweeper.SweepProject(context.Background(), testTenant, testProject); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a nil task in the listing produced %v", err)
	}
}

// nilListingTasks lists one nil task, which no real store should do and
// which the sweep must therefore refuse rather than dereference.
type nilListingTasks struct{}

func (nilListingTasks) ListTasks(context.Context, string, *orchestration.TaskFilter) ([]*orchestration.Task, error) {
	return []*orchestration.Task{nil}, nil
}

func (nilListingTasks) ReadmitTask(context.Context, string, string) error { return nil }

// TestVerdictForCheckError_KeepsTheThreeClassesApart tests the
// classification directly, because two of its branches are defensive: the
// sweep's own parse catches an unusable kind before a check ever sees it,
// and a classifier that folded the classes together would only be noticed
// once something else started reaching it.
func TestVerdictForCheckError_KeepsTheThreeClassesApart(t *testing.T) {
	condition := UnblockCondition{Kind: UnblockOwnerRuling}
	cases := map[string]struct {
		err     error
		verdict UnblockVerdict
		names   string
	}{
		"a person decides it": {&UnblockNotCheckableError{Kind: UnblockOwnerRuling, Awaiting: "the owner"}, UnblockAwaitingHuman, "the owner"},
		"an unknown kind":     {&UnblockKindError{Kind: "nope", Sentinel: ErrUnblockKindUnknown}, UnblockUnreadable, "nope"},
		"an unfinished kind":  {&UnblockKindError{Kind: "half", Sentinel: ErrUnblockKindUnhandled}, UnblockUnreadable, "half"},
		"the world failed":    {&NotFoundError{Entity: "commit", ID: "cafe"}, UnblockUndecidable, "stays blocked"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			verdict, evidence := verdictForCheckError(condition, tc.err)
			if verdict != tc.verdict {
				t.Fatalf("verdict = %s, want %s", verdict, tc.verdict)
			}
			if !strings.Contains(evidence, tc.names) {
				t.Fatalf("evidence %q does not carry %q", evidence, tc.names)
			}
		})
	}
}

// =============================================================================
// CheckCondition
// =============================================================================

func TestCheckCondition_ReportsAHumanConditionAsUncheckable(t *testing.T) {
	f := newSweepFixture(t)
	task := f.blocked("anything")

	satisfied, err := f.sweeper.CheckCondition(context.Background(), testTenant, task, UnblockCondition{Kind: UnblockOwnerRuling})
	if satisfied {
		t.Fatal("an owner ruling reported itself satisfied")
	}
	var human *UnblockNotCheckableError
	if !errors.As(err, &human) {
		t.Fatalf("got %v, want an UnblockNotCheckableError: a caller receiving this as an "+
			"ordinary unsatisfied condition would wait on it forever", err)
	}
	if human.Awaiting != "the owner" {
		t.Fatalf("the error names %q as the party being waited on", human.Awaiting)
	}
	if !errors.Is(err, ErrUnblockNotCheckable) || !strings.Contains(err.Error(), "the owner") {
		t.Fatalf("the error does not match its sentinel or name the party: %v", err)
	}
}

func TestCheckCondition_Refusals(t *testing.T) {
	f := newSweepFixture(t)
	if _, err := f.sweeper.CheckCondition(context.Background(), testTenant, nil,
		UnblockCondition{Kind: UnblockPipelineGreen, Subject: "cafe"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CheckCondition accepted a nil task: %v", err)
	}
	task := f.blocked("anything")
	if _, err := f.sweeper.CheckCondition(context.Background(), testTenant, task,
		UnblockCondition{Kind: "the_stars_align"}); !errors.Is(err, ErrUnblockKindUnknown) {
		t.Fatalf("CheckCondition accepted an unknown kind: %v", err)
	}
	// A task that was not adopted from a forge issue has no repository for a
	// check to run against, and that is a refusal rather than a release.
	task.Source = nil
	if _, err := f.sweeper.CheckCondition(context.Background(), testTenant, task,
		UnblockCondition{Kind: UnblockPipelineGreen, Subject: "cafe"}); !errors.Is(err, ErrTaskNotForgeIssue) {
		t.Fatalf("a sourceless task was checked anyway: %v", err)
	}
}

func TestCheckCondition_SatisfiedAndUnsatisfiedAgreeWithTheSweep(t *testing.T) {
	f := newSweepFixture(t)
	task := f.blocked("anything")
	const head = "beefcafe"

	satisfied, err := f.sweeper.CheckCondition(context.Background(), testTenant, task,
		UnblockCondition{Kind: UnblockPipelineGreen, Subject: head})
	if err != nil || satisfied {
		t.Fatalf("a head with no runs reported satisfied=%v, %v", satisfied, err)
	}
	f.forge.checks[head] = []ForgeCheckRun{greenCheck(head)}
	satisfied, err = f.sweeper.CheckCondition(context.Background(), testTenant, task,
		UnblockCondition{Kind: UnblockPipelineGreen, Subject: head})
	if err != nil || !satisfied {
		t.Fatalf("a green head reported satisfied=%v, %v", satisfied, err)
	}
}

// =============================================================================
// Verdict surface
// =============================================================================

func TestUnblockVerdict_ReleasedNamesOnlyTheReleasingVerdict(t *testing.T) {
	all := []UnblockVerdict{
		UnblockReleased, UnblockWaiting, UnblockAwaitingHuman, UnblockUndecidable,
		UnblockAwaitingDiagnosis, UnblockUnreadable, UnblockBudgetExhausted,
	}
	for _, v := range all {
		if want := v == UnblockReleased; v.Released() != want {
			t.Errorf("%s.Released() = %v, want %v", v, v.Released(), want)
		}
		if v.String() != string(v) {
			t.Errorf("%s renders as %q", string(v), v.String())
		}
	}
	empty := UnblockReport{}
	if len(empty.Released()) != 0 || empty.Count(UnblockReleased) != 0 {
		t.Error("an empty report reported findings")
	}
}

// =============================================================================
// The whole chain — sweep, engine verb, conclusion, issue
// =============================================================================
//
// These run the sweep against the REAL engine rather than the fixture above:
// an in-memory task store under a real AgentService, with this module's own
// Hooks registered as the engine's conclusion observer. Nothing here stands in
// for the readmission, which is the point — a double that announced because
// the test told it to would say nothing about what the engine does.

// conclusionRouter is the one observer this package registers in the engine's
// process-wide conclusion registry. Registration there is append-only, so the
// router dispatches by tenant to whichever test is watching, and a test that
// has finished is simply no longer in the map.
type conclusionRouter struct{}

var (
	conclusionRouterOnce sync.Once
	errConclusionRouter  error

	// conclusionWatchers maps a tenant to the observer watching it.
	conclusionWatchers sync.Map
)

func (conclusionRouter) TaskConcluded(ctx context.Context, conclusion orchestration.TaskConclusion) error {
	watching, ok := conclusionWatchers.Load(conclusion.TenantID)
	if !ok {
		return nil
	}
	return watching.(orchestration.TaskConclusionObserver).TaskConcluded(ctx, conclusion)
}

// watchConclusions points the engine's conclusion announcements for one
// tenant at observer, for the length of one test.
func watchConclusions(t *testing.T, tenantID string, observer orchestration.TaskConclusionObserver) {
	t.Helper()
	conclusionRouterOnce.Do(func() {
		errConclusionRouter = orchestration.RegisterTaskConclusionObserver("sdlc-test-router", conclusionRouter{})
	})
	if errConclusionRouter != nil {
		t.Fatalf("RegisterTaskConclusionObserver: %v", errConclusionRouter)
	}
	if _, watched := conclusionWatchers.LoadOrStore(tenantID, observer); watched {
		t.Fatalf("tenant %s is already watched; two tests cannot share one tenant's conclusions", tenantID)
	}
	t.Cleanup(func() { conclusionWatchers.Delete(tenantID) })
}

// engineRig is the sweeper wired the way a host must wire it: to the engine's
// service, over a real task store, with the module observing conclusions.
type engineRig struct {
	hooks   *hooksRig
	svc     orchestration.AgentService
	tasks   orchestration.TaskStore
	sweeper *UnblockSweeper
	ctx     context.Context
}

// forgeIssueKindOnce registers the module's own source once per process.
//
// Kind registration is what makes `forge_issue` a provenance the engine's
// task store accepts, and it is process-wide however local the registry it
// was written to — so this is the same registration pkg/server makes at boot,
// done once here rather than per rig.
var (
	forgeIssueKindOnce sync.Once
	errForgeIssueKind  error
)

func registerForgeIssueKind(t *testing.T, bindings BindingReader, forges ForgeResolver) {
	t.Helper()
	forgeIssueKindOnce.Do(func() {
		source, err := NewIssueSource(bindings, forges, 10)
		if err != nil {
			errForgeIssueKind = err
			return
		}
		errForgeIssueKind = orchestration.NewTaskSourceRegistry().Register(TaskSourceKindForgeIssue, source)
	})
	if errForgeIssueKind != nil {
		t.Fatalf("registering the forge_issue source kind: %v", errForgeIssueKind)
	}
}

func newEngineRig(t *testing.T) *engineRig {
	t.Helper()
	hooks := newHooksRig(t)
	watchConclusions(t, testTenant, hooks.hooks)
	registerForgeIssueKind(t, hooks.store, &fakeForges{forge: hooks.forge})

	base, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout", Format: "console"})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	svc, err := orchestration.NewAgentService(orchestration.NewInMemoryAgentStore(), nil, logger.NewAdapter(base))
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	wirer, wirable := svc.(interface {
		SetTaskStore(store orchestration.TaskStore)
	})
	if !wirable {
		t.Fatal("the engine's service no longer accepts a task store")
	}
	tasks := orchestration.NewTaskStore(orchestration.NewMemoryTaskKV(), orchestration.NewProcessClaimArbiter())
	wirer.SetTaskStore(tasks)

	sweeper, err := NewUnblockSweeper(UnblockSweeperConfig{
		Tasks:    svc,
		Bindings: hooks.store,
		Forges:   &fakeForges{forge: hooks.forge},
		Now:      func() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewUnblockSweeper: %v", err)
	}
	return &engineRig{
		hooks:   hooks,
		svc:     svc,
		tasks:   tasks,
		sweeper: sweeper,
		ctx:     orchestration.ContextWithTenant(context.Background(), testTenant),
	}
}

// park blocks the rig's task on condition, through the engine's verb, and
// returns the task record the store now holds.
func (r *engineRig) park(t *testing.T, reason string) *orchestration.Task {
	t.Helper()
	task := testTaskRecord(25)
	if err := r.tasks.CreateTask(r.ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := r.svc.BlockTask(r.ctx, testTenant, task.ID, orchestration.TaskBlockKindBlocked, reason); err != nil {
		t.Fatalf("BlockTask: %v", err)
	}
	return task
}

// TestSweepProject_ReadmissionClearsTheBlockedLabel is go-quicbaas#842: a
// machine-checkable unblock must leave the issue where every other
// readmission leaves it, with the block label gone. A sweep wired to the task
// store underneath the service readmits in silence and strands the label,
// and nothing downstream repairs it.
func TestSweepProject_ReadmissionClearsTheBlockedLabel(t *testing.T) {
	rig := newEngineRig(t)
	dependency := rig.hooks.forge.putIssue(900, "the dependency")
	dependency.State = "closed"

	condition := UnblockCondition{
		Kind: UnblockForgeIssueState, Subject: IssueRefString(testRepoRef, 900), Target: "closed",
	}
	task := rig.park(t, mustCompose(t, condition, "needs the SDK route that lands there"))

	if labels := rig.hooks.forge.issueLabels(25); !containsLabel(labels, "factory:blocked") {
		t.Fatalf("the park left no block label, so this test could not prove the sweep clears one: %v", labels)
	}

	report, err := rig.sweeper.SweepProject(rig.ctx, testTenant, testProject)
	if err != nil {
		t.Fatalf("SweepProject: %v", err)
	}
	finding := onlyFinding(t, report)
	if finding.Verdict != UnblockReleased {
		t.Fatalf("verdict is %s (%s), want released", finding.Verdict, finding.Evidence)
	}

	readmitted, err := rig.tasks.GetTask(rig.ctx, testTenant, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if readmitted.Status != orchestration.TaskStatusPending {
		t.Fatalf("the released task is %s, want pending", readmitted.Status)
	}

	labels := rig.hooks.forge.issueLabels(25)
	if containsLabel(labels, "factory:blocked") {
		t.Fatalf("the task is back in the queue while the issue still says it is blocked: %v", labels)
	}
	if !containsLabel(labels, "bug") {
		t.Fatalf("a label the module does not own must be left alone: %v", labels)
	}
	comments := rig.hooks.forge.commentsOn(25)
	if len(comments) != 2 || !strings.Contains(comments[1].Body, "### This task has been readmitted") {
		t.Fatalf("the readmission was not announced on the issue: %+v", comments)
	}
}

// TestSweepProject_AnUnreleasedTaskLeavesTheIssueBlocked is the other half:
// the sweep announces nothing for a task it did not move, so an unsatisfied
// condition leaves both the task and its label exactly where the park put
// them.
func TestSweepProject_AnUnreleasedTaskLeavesTheIssueBlocked(t *testing.T) {
	rig := newEngineRig(t)
	dependency := rig.hooks.forge.putIssue(900, "the dependency")
	dependency.State = "open"

	condition := UnblockCondition{
		Kind: UnblockForgeIssueState, Subject: IssueRefString(testRepoRef, 900), Target: "closed",
	}
	task := rig.park(t, mustCompose(t, condition, "needs the SDK route that lands there"))

	report, err := rig.sweeper.SweepProject(rig.ctx, testTenant, testProject)
	if err != nil {
		t.Fatalf("SweepProject: %v", err)
	}
	if finding := onlyFinding(t, report); finding.Verdict != UnblockWaiting {
		t.Fatalf("verdict is %s (%s), want waiting", finding.Verdict, finding.Evidence)
	}

	held, err := rig.tasks.GetTask(rig.ctx, testTenant, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if held.Status != orchestration.TaskStatusBlocked {
		t.Fatalf("an unsatisfied condition moved the task to %s", held.Status)
	}
	if labels := rig.hooks.forge.issueLabels(25); !containsLabel(labels, "factory:blocked") {
		t.Fatalf("the block label was cleared by a sweep that released nothing: %v", labels)
	}
}

// =============================================================================
// The release-tag kind — merged is not released
// =============================================================================

// TestSweepProject_ReleasesAPublishedTag: the condition a dependency-parked task
// carries when its consumer pins a version, cleared by the tag existing.
func TestSweepProject_ReleasesAPublishedTag(t *testing.T) {
	f := newSweepFixture(t)
	f.forge.putTag(testRepoRef, "v0.9.5-alpha", "deadbeefcafe")

	reason := mustCompose(t, UnblockCondition{
		Kind: UnblockReleaseTag, Subject: testRepoRef.String(), Target: "v0.9.5-alpha",
	}, "the consumer pins this version")
	task := f.blocked(reason)

	finding := onlyFinding(t, f.sweep(t))
	if finding.Verdict != UnblockReleased {
		t.Fatalf("verdict is %s (%s), want released", finding.Verdict, finding.Evidence)
	}
	if finding.Kind != UnblockReleaseTag {
		t.Fatalf("finding kind is %q", finding.Kind)
	}
	if got := f.tasks.readmitted(); len(got) != 1 || got[0] != task.ID {
		t.Fatalf("the store was asked to readmit %v", got)
	}
}

// TestSweepProject_AnAbsentTagIsWaitingNotUndecidable is the distinction this
// kind exists to make. "There is no v0.9.5-alpha yet" is the ANSWER to the
// question, not a failure to answer it — so it waits, on every pass, and clears
// itself when the release happens. Reporting it as undecidable would summon an
// operator to look at a dependency that is simply not tagged yet.
func TestSweepProject_AnAbsentTagIsWaitingNotUndecidable(t *testing.T) {
	f := newSweepFixture(t)
	reason := mustCompose(t, UnblockCondition{
		Kind: UnblockReleaseTag, Subject: testRepoRef.String(), Target: "v0.9.5-alpha",
	}, "the consumer pins this version")
	f.blocked(reason)

	finding := onlyFinding(t, f.sweep(t))
	if finding.Verdict != UnblockWaiting {
		t.Fatalf("verdict is %s (%s), want waiting: an untagged version is an answer, not an outage",
			finding.Verdict, finding.Evidence)
	}
	if len(f.tasks.readmitted()) != 0 {
		t.Fatalf("an unpublished tag released %v", f.tasks.readmitted())
	}
}

// TestSweepProject_ADifferentTagDoesNotSatisfyThePinnedOne: a repository that has
// released v1.0.0 has not released v0.9.5, and a check matching any tag would
// unblock a consumer against a version that does not exist.
func TestSweepProject_ADifferentTagDoesNotSatisfyThePinnedOne(t *testing.T) {
	f := newSweepFixture(t)
	f.forge.putTag(testRepoRef, "v1.0.0", "cafe")

	reason := mustCompose(t, UnblockCondition{
		Kind: UnblockReleaseTag, Subject: testRepoRef.String(), Target: "v0.9.5-alpha",
	}, "the consumer pins this version")
	f.blocked(reason)

	if got := onlyFinding(t, f.sweep(t)).Verdict; got != UnblockWaiting {
		t.Fatalf("verdict is %s, want waiting", got)
	}
}

// TestSweepProject_AnUnreadableTagIsUndecidable: a forge that would not answer is
// an outage, which is a different fact from "not released" and leaves the task
// blocked for a different reason.
func TestSweepProject_AnUnreadableTagIsUndecidable(t *testing.T) {
	f := newSweepFixture(t)
	f.forge.getTagErr = errors.New("gateway timeout")

	reason := mustCompose(t, UnblockCondition{
		Kind: UnblockReleaseTag, Subject: testRepoRef.String(), Target: "v0.9.5-alpha",
	}, "the consumer pins this version")
	f.blocked(reason)

	if got := onlyFinding(t, f.sweep(t)).Verdict; got != UnblockUndecidable {
		t.Fatalf("verdict is %s, want undecidable", got)
	}
}

// TestSweepProject_AReleaseSubjectThatNamesNoRepositoryIsUndecidable: the subject
// is a full owner/repo, and one that is not is reported on every pass rather than
// refused once at encode time and never mentioned again.
func TestSweepProject_AReleaseSubjectThatNamesNoRepositoryIsUndecidable(t *testing.T) {
	f := newSweepFixture(t)
	// Composed by hand: Encode would accept "api" as a charset-legal subject,
	// which is exactly the case the check has to report.
	f.blocked("repository api must have released tag v1.0.0 " +
		"unblock[kind=release_tag;subject=api;target=v1.0.0]")

	finding := onlyFinding(t, f.sweep(t))
	if finding.Verdict != UnblockUndecidable {
		t.Fatalf("verdict is %s (%s), want undecidable", finding.Verdict, finding.Evidence)
	}
}

// TestUnblockReleaseTag_ValidationRequiresBothFields: a release condition without
// a repository or without a tag names nothing a check could decide.
func TestUnblockReleaseTag_ValidationRequiresBothFields(t *testing.T) {
	t.Parallel()
	good := UnblockCondition{Kind: UnblockReleaseTag, Subject: "acme/api", Target: "v1.0.0"}
	if err := good.Validate(); err != nil {
		t.Fatalf("a complete release condition was refused: %v", err)
	}
	if !good.Kind.MachineCheckable() {
		t.Fatal("a release condition is decided by a check, not by a person")
	}
	if good.Kind.Awaiting() != "" {
		t.Fatal("a release condition waits on no named person")
	}
	for name, condition := range map[string]UnblockCondition{
		"no subject": {Kind: UnblockReleaseTag, Target: "v1.0.0"},
		"no target":  {Kind: UnblockReleaseTag, Subject: "acme/api"},
		"neither":    {Kind: UnblockReleaseTag},
		"a reserved character in the tag": {
			Kind: UnblockReleaseTag, Subject: "acme/api", Target: "v1.0.0;x",
		},
		"a space in the subject": {
			Kind: UnblockReleaseTag, Subject: "acme/ api", Target: "v1.0.0",
		},
	} {
		if err := condition.Validate(); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s: want ErrInvalidInput, got %v", name, err)
		}
	}
}

// TestUnblockSweeper_SweepVisitsEveryBoundProject proves the sweep has a
// tenant-wide entry point at all.
//
// It had none, which is the mechanical reason it could never be mounted beside
// the other backstops — so a task that parked on a checkable condition stayed
// parked however completely that condition cleared, and the only thing that
// ever returned one to the queue was a person.
func TestUnblockSweeper_SweepVisitsEveryBoundProject(t *testing.T) {
	var sweeper *UnblockSweeper
	var _ interface {
		Sweep(ctx context.Context, tenants orchestration.TenantLister) (int, error)
		SweepFailures() []error
	} = sweeper

	if _, err := (&UnblockSweeper{}).Sweep(context.Background(), nil); err == nil {
		t.Fatal("a pass that enumerates no tenant must refuse rather than report a clean sweep")
	}
}
