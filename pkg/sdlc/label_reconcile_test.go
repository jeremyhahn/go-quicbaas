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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Fixtures
// =============================================================================

// reconcileIssue is the issue every fixture below reconciles. It is one of the
// seven the defect was measured on.
const reconcileIssue = 25

// countingPacer admits every step at once and counts them, so a test asserts
// that the forge calls ARE paced without spending wall-clock time proving it.
type countingPacer struct {
	paces atomic.Int64
	err   error
}

// Pace implements ForgePacer.
func (p *countingPacer) Pace(ctx context.Context) error {
	p.paces.Add(1)
	if p.err != nil {
		return p.err
	}
	return ctx.Err()
}

// fakeTenants is an orchestration.TenantLister over a slice.
type fakeTenants struct {
	ids []string
	err error
}

// ListTenantIDs implements orchestration.TenantLister.
func (f *fakeTenants) ListTenantIDs(context.Context) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.ids, nil
}

// reconcileRig is one bound project with a forge, a task store and the sweep
// that reconciles them.
type reconcileRig struct {
	reconciler *LabelReconciler
	store      *Store
	forge      *fakeForge
	tasks      *fakeBlockedTasks
	records    *fakeTasks
	tenants    *fakeTenants
	pacer      *countingPacer
	binding    *ProjectBinding
}

// reconcileBinding is testBinding with an escalated label, which the shared
// fixture leaves unmapped: the exhausted and abandoned rows both land there,
// and a test against an unmapped key would prove only that nothing was added.
func reconcileBinding() *ProjectBinding {
	binding := testBinding()
	binding.Forge.Labels[LabelEscalated] = "factory:escalated"
	return binding
}

// newReconcileRig builds the sweep over one project whose issue 25 carries
// whatever labels the caller names, paced by a pacer that admits every step.
func newReconcileRig(t *testing.T, issueLabels []string, tasks ...*orchestration.Task) *reconcileRig {
	t.Helper()
	return newReconcileRigPaced(t, &countingPacer{}, issueLabels, tasks...)
}

// newReconcileRigPaced is newReconcileRig with the caller's pacer, so a test
// that needs to act BETWEEN two of the pass's steps has a seam to act on: Pace
// is called immediately before every forge call the sweep makes, and the first
// of them is after the task snapshot and before the claims are listed.
func newReconcileRigPaced(t *testing.T, pacer ForgePacer, issueLabels []string, tasks ...*orchestration.Task) *reconcileRig {
	t.Helper()
	ctx := context.Background()

	store := newTestStore(t)
	binding := reconcileBinding()
	if err := store.PutBinding(ctx, testTenant, binding); err != nil {
		t.Fatal(err)
	}

	forge := newFakeForge()
	forge.putIssue(reconcileIssue, "Fix the thing", issueLabels...)

	taskList := &fakeBlockedTasks{tasks: tasks}
	records := newFakeTasks(tasks...)
	hooks, err := NewHooks(HooksConfig{
		Projects: &fakeProjects{projects: map[string]*orchestration.Project{testProject: testOrchProject()}},
		Bindings: store, Deliveries: store, Tasks: records, Forges: &fakeForges{forge: forge},
		MergeAttempts: 2, Waiter: &immediateWaiter{},
		Surfacing: SurfacePolicy{RetryAttempts: 2, RetryInterval: time.Millisecond, PostTimeout: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}

	tenants := &fakeTenants{ids: []string{testTenant}}
	reconciler, err := NewLabelReconciler(LabelReconcilerConfig{
		Tenants: tenants, Bindings: store, Tasks: taskList, Hooks: hooks, Pacer: pacer,
	})
	if err != nil {
		t.Fatal(err)
	}
	rig := &reconcileRig{
		reconciler: reconciler, store: store, forge: forge, tasks: taskList,
		records: records, tenants: tenants, binding: binding,
	}
	if counting, ok := pacer.(*countingPacer); ok {
		rig.pacer = counting
	}
	return rig
}

// firstStepPacer runs the caller's action once, at the sweep's FIRST forge
// step, and then paces nothing.
//
// That instant is the race window this file's orphan tests drive: the pass has
// already snapshotted the task store (ReconcileProject lists the tasks before
// it resolves the forge) and has not yet listed the claims, so whatever the
// action does is invisible to the snapshot and visible to the listing —
// exactly the interleaving the engine produces when the task-source poller
// adopts an issue while a sweep is in flight.
type firstStepPacer struct {
	once sync.Once
	act  func()
}

// Pace implements ForgePacer.
func (p *firstStepPacer) Pace(ctx context.Context) error {
	p.once.Do(func() {
		if p.act != nil {
			p.act()
		}
	})
	return ctx.Err()
}

// sweep runs one project pass and fails the test on a pass-level error.
func (r *reconcileRig) sweep(t *testing.T) LabelReconcileReport {
	t.Helper()
	report, err := r.reconciler.ReconcileProject(context.Background(), testTenant, testProject)
	if err != nil {
		t.Fatalf("ReconcileProject: %v", err)
	}
	return report
}

// only returns the single finding a pass produced.
func only(t *testing.T, report LabelReconcileReport) LabelReconcileFinding {
	t.Helper()
	if len(report.Findings) != 1 {
		t.Fatalf("want exactly one finding, got %d: %+v", len(report.Findings), report.Findings)
	}
	return report.Findings[0]
}

// outcomeComments returns the comments on the reconciled issue that carry an
// outcome marker, which is what "one comment" is counted in.
func (r *reconcileRig) outcomeComments() []ForgeComment {
	var marked []ForgeComment
	for _, comment := range r.forge.commentsOn(reconcileIssue) {
		if strings.Contains(comment.Body, surfacingMarker) {
			marked = append(marked, comment)
		}
	}
	return marked
}

// parkedTask is a task the engine blocked and will not retry on its own.
func parkedTask(kind orchestration.TaskBlockKind, reason string) *orchestration.Task {
	task := testTaskRecord(reconcileIssue)
	task.Status = orchestration.TaskStatusBlocked
	task.BlockKind = kind
	task.BlockedReason = reason
	task.Attempts = 2
	// The engine's park persists the reason beside the kind, so a record built
	// here carries what a record read back from the store would.
	if conclusion, named := orchestration.ConclusionOfBlockKind(kind); named {
		task.ConclusionReason = conclusion
	}
	return task
}

// =============================================================================
// The defect: a terminal task whose issue still claims live work
// =============================================================================

// TestLabelReconciler_TerminalTaskWithStaleClaimIsSurfacedOnce is the measured
// defect and its fix in one test: an issue carrying `factory:in-progress` for a
// task that stopped days ago gets the outcome it never got, the label moves,
// and a second sweep over the repaired state writes nothing more.
func TestLabelReconciler_TerminalTaskWithStaleClaimIsSurfacedOnce(t *testing.T) {
	rig := newReconcileRig(t, []string{"factory:in-progress", "bug"},
		parkedTask(orchestration.TaskBlockKindBlocked, "the account pool is empty"))

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileSurfaced {
		t.Fatalf("verdict = %q (%s), want surfaced", finding.Verdict, finding.Evidence)
	}
	if finding.Outcome != OutcomeBlocked || finding.TaskID != testTask || finding.Issue != reconcileIssue {
		t.Fatalf("finding does not name what it settled: %+v", finding)
	}

	comments := rig.outcomeComments()
	if len(comments) != 1 {
		t.Fatalf("want one outcome comment, got %d", len(comments))
	}
	if want := blockedMarker(testTask, 2, OutcomeBlocked); !strings.Contains(comments[0].Body, want) {
		t.Fatalf("the comment carries no %q marker:\n%s", want, comments[0].Body)
	}
	if !strings.Contains(comments[0].Body, "the account pool is empty") {
		t.Fatalf("the comment does not quote why the task stopped:\n%s", comments[0].Body)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:blocked", "bug"}) ||
		hasAll(got, []string{"factory:in-progress"}) {
		t.Fatalf("labels = %v, want the claim swapped for factory:blocked and `bug` left alone", got)
	}

	// THE SECOND SWEEP IS THE IDEMPOTENCE — of one node against itself over
	// time, which is what the marker check actually buys. A repair pass runs
	// every few minutes; one that re-posted would bury the issue it was
	// fixing. The repaired issue no longer claims live work, so it is not even
	// visited. Two nodes sweeping at once is NOT covered here and is not
	// covered by the code: see the note on idempotence in label_reconcile.go
	// and go-quicbaas#840.
	if report := rig.sweep(t); len(report.Findings) != 0 {
		t.Fatalf("the repaired issue was examined again: %+v", report.Findings)
	}
	if got := len(rig.outcomeComments()); got != 1 {
		t.Fatalf("second sweep left %d outcome comments, want the one", got)
	}

	// AND WITH THE CLAIM BACK, which is the case the marker check is really
	// for: the label write raced a re-label, or a person put it back. The
	// sweep must move the label again and post NOTHING, because this task's
	// this attempt's outcome is already on the issue.
	if err := rig.forge.AddIssueLabels(context.Background(), testRepoRef, reconcileIssue,
		[]string{"factory:in-progress"}); err != nil {
		t.Fatal(err)
	}
	third := only(t, rig.sweep(t))
	if third.Verdict != LabelReconcileRelabelled {
		t.Fatalf("third sweep verdict = %q (%s), want relabelled", third.Verdict, third.Evidence)
	}
	if got := len(rig.outcomeComments()); got != 1 {
		t.Fatalf("third sweep left %d outcome comments, want the one", got)
	}
}

// TestLabelReconciler_PostedOutcomeWithStaleLabelIsRelabelledOnly covers the
// half-written conclusion: the comment landed and the label move did not, so
// the sweep must move the label WITHOUT posting a second report of the same
// ending.
func TestLabelReconciler_PostedOutcomeWithStaleLabelIsRelabelledOnly(t *testing.T) {
	task := parkedTask(orchestration.TaskBlockKindBlocked, "waiting on the release")
	rig := newReconcileRig(t, []string{"factory:in-progress"}, task)

	// The comment as TaskConcluded would have written it, with its marker.
	existing := renderOutcome(outcomeReport{
		binding: rig.binding, task: task, attempt: task.Attempts, kind: OutcomeBlocked,
		message: "waiting on the release",
	})
	if _, err := rig.forge.CommentOnIssue(context.Background(), testRepoRef, reconcileIssue, existing); err != nil {
		t.Fatal(err)
	}

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileRelabelled {
		t.Fatalf("verdict = %q (%s), want relabelled", finding.Verdict, finding.Evidence)
	}
	if got := len(rig.outcomeComments()); got != 1 {
		t.Fatalf("want the one comment already there, got %d", got)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:blocked"}) {
		t.Fatalf("labels = %v, want factory:blocked", got)
	}
}

// TestLabelReconciler_SpentBudgetIsAnnouncedAsExhausted proves the sweep reads
// a failed task's record back into the conclusion the engine would have
// announced, and lands it on the escalated label the exhausted row names.
func TestLabelReconciler_SpentBudgetIsAnnouncedAsExhausted(t *testing.T) {
	task := testTaskRecord(reconcileIssue)
	task.Status = orchestration.TaskStatusFailed
	task.Attempts, task.AttemptBudget = 3, 3
	task.ConclusionReason = orchestration.TaskConcludedBudgetExhausted
	rig := newReconcileRig(t, []string{"factory:in-progress"}, task)

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileSurfaced || finding.Outcome != OutcomeExhausted {
		t.Fatalf("finding = %+v, want a surfaced exhausted outcome", finding)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:escalated"}) {
		t.Fatalf("labels = %v, want factory:escalated", got)
	}
	comments := rig.outcomeComments()
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "spent its attempt budget") {
		t.Fatalf("the exhausted row was not rendered: %+v", comments)
	}
}

// TestLabelReconciler_CompletedTaskIsMovedToMergedWithoutAComment covers the
// silent row: the work was accepted and the Complete moment already said so by
// merging, so the repair is the label it did not manage to move — and nothing
// else. A second report of an ending nobody is waiting on is noise.
func TestLabelReconciler_CompletedTaskIsMovedToMergedWithoutAComment(t *testing.T) {
	task := testTaskRecord(reconcileIssue)
	task.Status = orchestration.TaskStatusCompleted
	task.ConclusionReason = orchestration.TaskConcludedCompleted
	rig := newReconcileRig(t, []string{"factory:in-progress"}, task)

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileRelabelled {
		t.Fatalf("verdict = %q (%s), want relabelled", finding.Verdict, finding.Evidence)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:merged"}) {
		t.Fatalf("labels = %v, want factory:merged", got)
	}
	if got := rig.outcomeComments(); len(got) != 0 {
		t.Fatalf("a completed task must announce nothing, got %d comment(s)", len(got))
	}
}

// TestLabelReconciler_MissingTaskIsAbandoned covers the issue whose task record
// is gone: nothing can ever conclude it, so the sweep does, with the table's
// row for an ending whose evidence is missing.
func TestLabelReconciler_MissingTaskIsAbandoned(t *testing.T) {
	rig := newReconcileRig(t, []string{"factory:in-progress"})

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileSurfaced || finding.Outcome != OutcomeAbandoned {
		t.Fatalf("finding = %+v, want a surfaced abandoned outcome", finding)
	}
	if finding.TaskID != "" {
		t.Fatalf("finding names task %q for an issue with no task", finding.TaskID)
	}
	comments := rig.outcomeComments()
	if len(comments) != 1 {
		t.Fatalf("want one outcome comment, got %d", len(comments))
	}
	for _, want := range []string{
		blockedMarker("", 0, OutcomeAbandoned),
		"holds no task record for it",
		"task:ready",
	} {
		if !strings.Contains(comments[0].Body, want) {
			t.Fatalf("the abandoned comment lacks %q:\n%s", want, comments[0].Body)
		}
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:escalated"}) {
		t.Fatalf("labels = %v, want factory:escalated", got)
	}

	// Idempotent for an orphan too: the marker names no task, so it must
	// still identify the comment the previous sweep left when the claim
	// comes back.
	if err := rig.forge.AddIssueLabels(context.Background(), testRepoRef, reconcileIssue,
		[]string{"factory:in-progress"}); err != nil {
		t.Fatal(err)
	}
	if second := only(t, rig.sweep(t)); second.Verdict != LabelReconcileRelabelled {
		t.Fatalf("second sweep verdict = %q (%s), want relabelled", second.Verdict, second.Evidence)
	}
	if got := len(rig.outcomeComments()); got != 1 {
		t.Fatalf("second sweep left %d comments, want the one", got)
	}
}

// =============================================================================
// The orphan is never declared from a stale snapshot
// =============================================================================

// TestLabelReconciler_TaskAdoptedDuringThePassIsNotOrphaned drives the window
// the orphan verdict used to be decided across.
//
// THE PASS SNAPSHOTS THE TASK STORE BEFORE IT READS THE FORGE. An ordinary
// adoption lands in between — the poller persists the task, the run it
// dispatches labels the issue in progress — and the sweep would then see a
// claim it holds no record for and post `abandoned` on a live run, moving its
// issue to `escalated`. That is strictly worse than the stale label the sweep
// exists to correct, because a lie a sweep WROTE is one an operator has no
// reason to doubt.
//
// The pacer is the seam: its first step is after the snapshot and before the
// claims are listed, and the action it runs is the engine's own order — the
// record persisted first, the label applied second.
func TestLabelReconciler_TaskAdoptedDuringThePassIsNotOrphaned(t *testing.T) {
	adopted := testTaskRecord(reconcileIssue)
	adopted.Status = orchestration.TaskStatusInProgress

	pacer := &firstStepPacer{}
	rig := newReconcileRigPaced(t, pacer, []string{"bug"})
	pacer.act = func() {
		rig.tasks.adopt(adopted)
		rig.forge.putIssue(reconcileIssue, "Fix the thing", "bug", "factory:in-progress")
	}

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileConsistent {
		t.Fatalf("verdict = %q (%s), want consistent: a task adopted mid-pass was orphaned", finding.Verdict, finding.Evidence)
	}
	if finding.TaskID != testTask {
		t.Fatalf("the finding names task %q, want the task the re-read found", finding.TaskID)
	}
	if got := rig.outcomeComments(); len(got) != 0 {
		t.Fatalf("a live run was announced as ended: %+v", got)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:in-progress"}) ||
		hasAll(got, []string{"factory:escalated"}) {
		t.Fatalf("labels = %v, want the live claim left exactly as the run set it", got)
	}
	if len(rig.forge.labelRemovals) != 0 {
		t.Fatalf("a live run's labels were moved: -%v", rig.forge.labelRemovals)
	}
}

// TestLabelReconciler_FreshTaskSupersedesASpentOneDuringThePass is the same
// race as the orphan above, arriving through the other destructive answer.
//
// A TERMINAL TASK RELEASES ITS SOURCE RESERVATION, so re-labelling the issue
// adopts a FRESH task while the spent one stays on the record. If that adoption
// lands after the pass snapshotted the store, the pass holds ONLY the spent task
// — and posting its ending would move the issue to a terminal label over a run
// that is live right now. Re-reading only when the snapshot had no task at all
// would have left this half open.
func TestLabelReconciler_FreshTaskSupersedesASpentOneDuringThePass(t *testing.T) {
	spent := parkedTask(orchestration.TaskBlockKindBlocked, "the account pool is empty")
	adopted := testTaskRecord(reconcileIssue)
	adopted.ID = "task-2"
	adopted.Status = orchestration.TaskStatusInProgress
	adopted.CreatedAt = spent.CreatedAt + 1

	pacer := &firstStepPacer{}
	rig := newReconcileRigPaced(t, pacer, []string{"factory:in-progress"}, spent)
	pacer.act = func() { rig.tasks.adopt(adopted) }

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileConsistent {
		t.Fatalf("verdict = %q (%s), want consistent: a spent task's ending was posted over a live run",
			finding.Verdict, finding.Evidence)
	}
	if finding.TaskID != "task-2" {
		t.Fatalf("the finding names task %q, want the freshly adopted one", finding.TaskID)
	}
	if got := rig.outcomeComments(); len(got) != 0 {
		t.Fatalf("a live run was announced as ended: %+v", got)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:in-progress"}) ||
		hasAll(got, []string{"factory:blocked"}) {
		t.Fatalf("labels = %v, want the live claim left alone", got)
	}
	if len(rig.forge.labelRemovals) != 0 {
		t.Fatalf("a live run's labels were moved: -%v", rig.forge.labelRemovals)
	}
}

// TestLabelReconciler_SpentTaskWithNoSuccessorIsStillSurfaced is the negative
// half: with nothing adopted during the pass, the re-read returns the same spent
// task and its ending is posted exactly as before. Without this, the guard above
// could be satisfied by a sweep that simply stopped surfacing terminal tasks.
func TestLabelReconciler_SpentTaskWithNoSuccessorIsStillSurfaced(t *testing.T) {
	rig := newReconcileRig(t, []string{"factory:in-progress"},
		parkedTask(orchestration.TaskBlockKindBlocked, "the account pool is empty"))

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileSurfaced || finding.Outcome != OutcomeBlocked {
		t.Fatalf("finding = %+v, want the spent task's ending surfaced", finding)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:blocked"}) {
		t.Fatalf("labels = %v, want factory:blocked", got)
	}
}

// TestLabelReconciler_UnreadableRecordDefersRatherThanDecides proves the re-read
// is fail-closed (go-quicbaas#848): a store that will not answer, a record it
// cannot decode, or an unreadable record beside a spent task is NOT evidence
// that no record (or no successor) exists, so nothing is written on the
// strength of it. The issue is left as found and the next sweep asks again.
func TestLabelReconciler_UnreadableRecordDefersRatherThanDecides(t *testing.T) {
	spent := func() []*orchestration.Task {
		return []*orchestration.Task{parkedTask(orchestration.TaskBlockKindBlocked, "the account pool is empty")}
	}
	failListingMidPass := func(t *testing.T, rig *reconcileRig, pacer *firstStepPacer) func() {
		pacer.act = func() { rig.tasks.failListing(errors.New("store unreachable")) }
		return func() { rig.tasks.failListing(nil) }
	}
	tests := []struct {
		name  string
		tasks []*orchestration.Task
		// arm makes a record unreadable and returns what heals it, so the
		// retry can prove the deferral is not a standing refusal.
		arm        func(t *testing.T, rig *reconcileRig, pacer *firstStepPacer) func()
		evidence   []string
		wantTaskID string
		forbidden  string
		retrySeen  bool
	}{
		{
			name: "a terminal task's record fails mid-pass", tasks: spent(), arm: failListingMidPass,
			evidence:   []string{ErrLabelReconcileRecord.Error()},
			wantTaskID: testTask, forbidden: "factory:blocked",
		},
		{
			name: "an unreadable record beside a spent task", tasks: spent(),
			arm: func(t *testing.T, rig *reconcileRig, pacer *firstStepPacer) func() {
				rig.tasks.unreadable = []string{"task-successor"}
				return nil
			},
			evidence:  []string{"task-successor"},
			forbidden: "factory:blocked",
		},
		{
			name: "no task and the listing fails mid-pass", arm: failListingMidPass,
			evidence: []string{
				ErrLabelReconcileDeferred.Error(), ErrLabelReconcileRecord.Error(), "store unreachable",
			},
			forbidden: "factory:escalated", retrySeen: true,
		},
		{
			name: "a corrupt task record",
			arm: func(t *testing.T, rig *reconcileRig, pacer *firstStepPacer) func() {
				ctx := context.Background()
				key := "orch/task/" + testTenant + "/" + testTask
				kv := orchestration.NewMemoryTaskKV()
				if err := kv.Put(ctx, key, []byte("{not json")); err != nil {
					t.Fatalf("plant corrupt record: %v", err)
				}
				store := orchestration.NewTaskStore(kv, orchestration.NewProcessClaimArbiter())
				reconciler, err := NewLabelReconciler(LabelReconcilerConfig{
					Tenants: rig.tenants, Bindings: rig.store, Tasks: store, Hooks: rig.reconciler.hooks, Pacer: pacer,
				})
				if err != nil {
					t.Fatal(err)
				}
				rig.reconciler = reconciler
				return func() {
					if err := kv.Delete(ctx, key); err != nil {
						t.Fatalf("remove corrupt record: %v", err)
					}
				}
			},
			evidence: []string{
				ErrLabelReconcileRecord.Error(), orchestration.ErrTaskListingIncomplete.Error(), testTask,
			},
			forbidden: "factory:escalated", retrySeen: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pacer := &firstStepPacer{}
			rig := newReconcileRigPaced(t, pacer, []string{"factory:in-progress"}, tt.tasks...)
			heal := tt.arm(t, rig, pacer)

			finding := only(t, rig.sweep(t))
			if finding.Verdict != LabelReconcileDeferred {
				t.Fatalf("verdict = %q (%s), want deferred", finding.Verdict, finding.Evidence)
			}
			if tt.wantTaskID != "" && finding.TaskID != tt.wantTaskID {
				t.Fatalf("the deferred finding names task %q, want %q", finding.TaskID, tt.wantTaskID)
			}
			for _, want := range tt.evidence {
				if !strings.Contains(finding.Evidence, want) {
					t.Fatalf("the evidence does not carry %q: %s", want, finding.Evidence)
				}
			}
			if got := rig.outcomeComments(); len(got) != 0 {
				t.Fatalf("an unreadable record produced an announcement: %+v", got)
			}
			if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:in-progress"}) ||
				hasAll(got, []string{tt.forbidden}) {
				t.Fatalf("labels = %v, want the issue untouched", got)
			}

			// AND ONCE THE RECORD READS AGAIN the issue is the orphan it then
			// is: the deferral is on the unreadable record, not a standing refusal.
			if !tt.retrySeen {
				return
			}
			heal()
			if retried := only(t, rig.sweep(t)); retried.Verdict != LabelReconcileSurfaced ||
				retried.Outcome != OutcomeAbandoned {
				t.Fatalf("the retry = %+v, want the orphan surfaced", retried)
			}
		})
	}
}

// TestLabelReconciler_TaskConcludedDuringThePassIsSurfacedNotOrphaned is the
// other half of the same re-read: a task that both starts and stops inside the
// window is answered from ITS OWN record, so the issue gets the ending the
// record names rather than `abandoned`, which announces that no record exists.
func TestLabelReconciler_TaskConcludedDuringThePassIsSurfacedNotOrphaned(t *testing.T) {
	pacer := &firstStepPacer{}
	rig := newReconcileRigPaced(t, pacer, []string{"bug"})
	parked := parkedTask(orchestration.TaskBlockKindNeedsInfo, "which database?")
	pacer.act = func() {
		rig.tasks.adopt(parked)
		rig.records.replace(parked)
		rig.forge.putIssue(reconcileIssue, "Fix the thing", "bug", "factory:in-progress")
	}

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileSurfaced || finding.Outcome != OutcomeNeedsInfo {
		t.Fatalf("finding = %+v, want the record's own ending surfaced", finding)
	}
	if finding.TaskID != testTask {
		t.Fatalf("the finding names task %q, want the task the re-read found", finding.TaskID)
	}
	comments := rig.outcomeComments()
	if len(comments) != 1 {
		t.Fatalf("want one outcome comment, got %d", len(comments))
	}
	if strings.Contains(comments[0].Body, "holds no task record for it") {
		t.Fatalf("a task with a record was announced as an orphan:\n%s", comments[0].Body)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:needs-info"}) {
		t.Fatalf("labels = %v, want factory:needs-info", got)
	}
}

// =============================================================================
// A queued task does not make a claim true
// =============================================================================

// TestLabelReconciler_QueuedTaskUnderAClaimIsUndecidable is the statement
// examineHandovers already makes, made the other way round: pending is not a
// handed-over run, so an in-progress label over a pending task is a claim the
// sweep cannot vouch for.
//
// It is reachable and it sticks: a re-labelled issue whose stale claim was
// never cleared is adopted as a fresh pending task, and auto-spawn refuses for
// as long as no agent can take it. Reading that as `consistent` — the verdict
// Drifted() filters out — would certify the exact state this sweep exists to
// end as healthy, in silence, for ever.
func TestLabelReconciler_QueuedTaskUnderAClaimIsUndecidable(t *testing.T) {
	task := testTaskRecord(reconcileIssue)
	task.Status = orchestration.TaskStatusPending
	rig := newReconcileRig(t, []string{"factory:in-progress", "bug"}, task)

	report := rig.sweep(t)
	finding := only(t, report)
	if finding.Verdict != LabelReconcileUndecidable {
		t.Fatalf("verdict = %q (%s), want undecidable", finding.Verdict, finding.Evidence)
	}
	if finding.TaskID != testTask || finding.Outcome != OutcomeSilent {
		t.Fatalf("finding = %+v, want the queued task named and nothing announced", finding)
	}
	if !strings.Contains(finding.Evidence, "queued rather than handed over") {
		t.Fatalf("the evidence does not say why the claim was not vouched for: %s", finding.Evidence)
	}

	// THE OPERATOR SEES IT, which is the whole point of not calling it
	// consistent.
	if len(report.Drifted()) != 1 {
		t.Fatalf("a premature claim was filtered out of the drift report: %+v", report.Drifted())
	}

	// AND NOTHING IS WRITTEN. Moving the issue back onto the trigger label
	// would un-claim a run the engine is dispatching and flap the label on
	// every task a sweep caught mid-dispatch.
	if len(rig.forge.labelAdds)+len(rig.forge.labelRemovals) != 0 {
		t.Fatalf("a queued claim was written to: +%v -%v", rig.forge.labelAdds, rig.forge.labelRemovals)
	}
	if got := len(rig.outcomeComments()); got != 0 {
		t.Fatalf("a queued claim was announced: %d comment(s)", got)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:in-progress", "bug"}) {
		t.Fatalf("labels = %v, want them exactly as found", got)
	}
}

// TestLabelReconciler_ClaimVerdictByLiveStatus locks the classification of
// every status the engine still intends to work, so the two maps cannot be
// read one way here and the other way in examineHandovers.
func TestLabelReconciler_ClaimVerdictByLiveStatus(t *testing.T) {
	for _, tc := range []struct {
		status orchestration.TaskStatus
		want   LabelReconcileVerdict
	}{
		{orchestration.TaskStatusPending, LabelReconcileUndecidable},
		{orchestration.TaskStatusClaimed, LabelReconcileConsistent},
		{orchestration.TaskStatusInProgress, LabelReconcileConsistent},
	} {
		t.Run(string(tc.status), func(t *testing.T) {
			task := testTaskRecord(reconcileIssue)
			task.Status = tc.status
			rig := newReconcileRig(t, []string{"factory:in-progress"}, task)

			finding := only(t, rig.sweep(t))
			if finding.Verdict != tc.want {
				t.Fatalf("verdict = %q (%s), want %q", finding.Verdict, finding.Evidence, tc.want)
			}
			if len(rig.forge.labelAdds)+len(rig.forge.labelRemovals) != 0 {
				t.Fatalf("a live task's issue was written to: +%v -%v", rig.forge.labelAdds, rig.forge.labelRemovals)
			}
			if got := len(rig.outcomeComments()); got != 0 {
				t.Fatalf("a live task was announced as ended: %d comment(s)", got)
			}
		})
	}
}

// TestLabelReconciler_ARunningAutoSpawnedTaskIsConsistent is the other end of
// go-quicbaas#966, measured from this side: the reconciler ran every five
// minutes and reported `LabelReconcileUndecidable` for every executing job —
// three times per delivery — because the engine's own dispatch left the task
// `pending`, and reportQueued's evidence ("queued rather than handed over") was
// simply wrong about the world: the task had been running for ten minutes.
//
// THE RECORD IS BUILT BY THE ENGINE'S OWN CLAIM, not by hand, so this test
// cannot pass by agreeing with a fixture. ClaimTaskSpawn is what auto-spawn and
// the dispatcher both dispatch through; whatever it writes is what the sweep
// reads, and the verdict must be the `consistent` one examineClaim already had
// for a run that has been handed over.
func TestLabelReconciler_ARunningAutoSpawnedTaskIsConsistent(t *testing.T) {
	ctx := context.Background()
	queued := testTaskRecord(reconcileIssue)
	queued.Status = orchestration.TaskStatusPending
	queued.AutoSpawn = true
	queued.Attempts = 0
	queued.SpawnedJobID = ""
	// The source is attached AFTER the claim rather than created with it: the
	// task store validates a source kind against the engine's source registry,
	// which only the daemon populates, and the claim neither reads the source
	// nor writes it.
	source := queued.Source
	queued.Source = nil

	engine := orchestration.NewInMemoryTaskStore()
	if err := engine.CreateTask(ctx, queued); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	dispatched, err := engine.GetTask(ctx, testTenant, queued.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	claim, err := orchestration.ClaimTaskSpawn(ctx, engine, dispatched, "job-running", time.Now().UnixNano())
	if err != nil {
		t.Fatalf("ClaimTaskSpawn: %v", err)
	}
	if !claim.Claimed {
		t.Fatalf("the engine must claim the task it dispatches: %+v", claim)
	}
	if dispatched.Status != orchestration.TaskStatusInProgress {
		t.Fatalf("a dispatched task reads %q; the sweep can only vouch for a hand-over", dispatched.Status)
	}
	dispatched.Source = source

	rig := newReconcileRig(t, []string{"factory:in-progress", "bug"}, dispatched)
	report := rig.sweep(t)
	finding := only(t, report)
	if finding.Verdict != LabelReconcileConsistent {
		t.Fatalf("verdict = %q (%s), want consistent", finding.Verdict, finding.Evidence)
	}
	if strings.Contains(finding.Evidence, "queued rather than handed over") {
		t.Fatalf("a running job was reported as queued: %s", finding.Evidence)
	}

	// AND IT IS SILENT. The operator's drift report is what the wrong verdict
	// polluted three times per delivery, so a healthy run must leave nothing
	// in it and nothing on the issue.
	if len(report.Drifted()) != 0 {
		t.Fatalf("a running delivery appeared in the drift report: %+v", report.Drifted())
	}
	if len(rig.forge.labelAdds)+len(rig.forge.labelRemovals) != 0 {
		t.Fatalf("a true claim was written to: +%v -%v", rig.forge.labelAdds, rig.forge.labelRemovals)
	}
	if got := len(rig.outcomeComments()); got != 0 {
		t.Fatalf("a running delivery was announced as ended: %d comment(s)", got)
	}
}

// TestLabelReconciler_HandedOverIsASubsetOfLive holds the containment the two
// directions both depend on. A status added to the handed-over set but not to
// the live set would be surfaced as an ENDING by examineClaim — an outcome
// posted over a running task — and the drift would be invisible in exactly the
// way the defect this file fixes was.
func TestLabelReconciler_HandedOverIsASubsetOfLive(t *testing.T) {
	for status := range handedOverTaskStatuses {
		if !liveTaskStatuses[status] {
			t.Fatalf("status %q is handed over but not live: examineClaim would announce an ending for it", status)
		}
	}
	if len(handedOverTaskStatuses) >= len(liveTaskStatuses) {
		t.Fatalf("the two sets are the same size: pending must be live and not handed over")
	}
	if handedOverTaskStatuses[orchestration.TaskStatusPending] {
		t.Fatal("pending is handed over; a queued task would be labelled as a run that has not begun")
	}
	if !liveTaskStatuses[orchestration.TaskStatusPending] {
		t.Fatal("pending is not live; a queued task would be announced as an ending")
	}
}

// =============================================================================
// A half-applied move leaves the issue findable
// =============================================================================

// TestLabelReconciler_AFailedRelabelLeavesTheIssueStillClaiming is the
// stranding this sweep could have caused with its own repair.
//
// A move is several forge calls and any of them can fail. Removing the claim
// first and failing to add the outcome's label left the issue carrying NO
// label this module owns — and such an issue is invisible to BOTH directions:
// it is not in the claiming listing, so no claim is examined, and a terminal
// task is not a lost hand-over, so nothing restores it. The display drift
// became permanent at the moment the repair failed.
func TestLabelReconciler_AFailedRelabelLeavesTheIssueStillClaiming(t *testing.T) {
	rig := newReconcileRig(t, []string{"factory:in-progress", "bug"},
		parkedTask(orchestration.TaskBlockKindBlocked, "the account pool is empty"))
	rig.forge.addLabelErr = errors.New("forge down")

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileDeferred {
		t.Fatalf("verdict = %q (%s), want deferred", finding.Verdict, finding.Evidence)
	}

	// THE CLAIM MUST STILL BE THERE. It is the only thing that makes the
	// issue findable by the next sweep.
	labels := rig.forge.issueLabels(reconcileIssue)
	if !hasAll(labels, []string{"factory:in-progress"}) {
		t.Fatalf("labels = %v: the claim was removed before the outcome's label landed, "+
			"so no sweep can ever find this issue again", labels)
	}
	if !hasAll(labels, []string{"bug"}) {
		t.Fatalf("labels = %v, want the unowned label left alone", labels)
	}

	// AND THE NEXT SWEEP CONVERGES IT, which is the property the claim was
	// keeping alive.
	rig.forge.addLabelErr = nil
	retried := only(t, rig.sweep(t))
	if retried.Verdict != LabelReconcileSurfaced && retried.Verdict != LabelReconcileRelabelled {
		t.Fatalf("the retry = %+v, want the issue converged", retried)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:blocked", "bug"}) ||
		hasAll(got, []string{"factory:in-progress"}) {
		t.Fatalf("labels = %v, want factory:blocked with the claim gone", got)
	}
}

// TestLabelReconciler_AFailedCleanupStillConvergesNextSweep is the other order
// of failure: the outcome's label landed and the stale claim could not be
// removed. Over-labelled is recoverable precisely because the issue is still
// in the claiming listing, so the next sweep finds it and finishes the move.
func TestLabelReconciler_AFailedCleanupStillConvergesNextSweep(t *testing.T) {
	rig := newReconcileRig(t, []string{"factory:in-progress"},
		parkedTask(orchestration.TaskBlockKindBlocked, "the account pool is empty"))
	rig.forge.removeLabelErr = errors.New("forge down")

	if finding := only(t, rig.sweep(t)); finding.Verdict != LabelReconcileDeferred {
		t.Fatalf("verdict = %q (%s), want deferred", finding.Verdict, finding.Evidence)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:in-progress", "factory:blocked"}) {
		t.Fatalf("labels = %v, want both: the add landed and the cleanup did not", got)
	}

	// STILL CLAIMING, so still visited -- and the comment is already there,
	// so the second pass moves the label and posts nothing more.
	rig.forge.removeLabelErr = nil
	if retried := only(t, rig.sweep(t)); retried.Verdict != LabelReconcileRelabelled {
		t.Fatalf("the retry = %+v, want relabelled", retried)
	}
	if got := rig.forge.issueLabels(reconcileIssue); hasAll(got, []string{"factory:in-progress"}) {
		t.Fatalf("labels = %v, want the stale claim finally gone", got)
	}
	if got := len(rig.outcomeComments()); got != 1 {
		t.Fatalf("the converging sweep left %d outcome comments, want the one", got)
	}
}

// =============================================================================
// Silence is two endings, not one
// =============================================================================

// runnerParkedTask is the record a park the WORKER surfaced leaves: blocked with
// its kind, and concluded `runner_parked` because the worker already posted the
// comment under the marker the conclusion seam would have used.
func runnerParkedTask(kind orchestration.TaskBlockKind) *orchestration.Task {
	task := parkedTask(kind, "the account pool is empty")
	task.ConclusionReason = orchestration.TaskConcludedRunnerParked
	return task
}

// TestLabelReconciler_RunnerParkedTaskIsNotAnnouncedAsMerged is the worst lie
// this sweep could tell, and reading the silence alone told it.
//
// The outcome table maps BOTH `completed` and `runner_parked` to OutcomeSilent,
// because in both cases the moment that concluded the task already wrote to the
// forge. That is all they have in common: `completed` means the pull request
// merged, `runner_parked` means the worker stopped and the task is BLOCKED
// waiting on a person. Settling silence on `merged` tells the reader of a blocked
// issue that their work landed — worse than the stale claim it replaced, because
// a stale `in_progress` is visibly unfinished and `merged` closes the question.
func TestLabelReconciler_RunnerParkedTaskIsNotAnnouncedAsMerged(t *testing.T) {
	for _, tc := range []struct {
		kind  orchestration.TaskBlockKind
		label string
	}{
		{orchestration.TaskBlockKindBlocked, "factory:blocked"},
		{orchestration.TaskBlockKindNeedsInfo, "factory:needs-info"},
		{orchestration.TaskBlockKindEscalated, "factory:escalated"},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			rig := newReconcileRig(t, []string{"factory:in-progress"}, runnerParkedTask(tc.kind))

			finding := only(t, rig.sweep(t))
			if finding.Verdict != LabelReconcileRelabelled {
				t.Fatalf("verdict = %q (%s), want relabelled", finding.Verdict, finding.Evidence)
			}
			got := rig.forge.issueLabels(reconcileIssue)
			if hasAll(got, []string{"factory:merged"}) {
				t.Fatalf("labels = %v: a BLOCKED task's issue was told its work had landed", got)
			}
			if !hasAll(got, []string{tc.label}) {
				t.Fatalf("labels = %v, want the label the park names (%s)", got, tc.label)
			}

			// NOTHING IS POSTED. The worker's own park comment is already
			// there, under the marker the seam would have used.
			if n := len(rig.outcomeComments()); n != 0 {
				t.Fatalf("the park was announced a second time: %d comment(s)", n)
			}
		})
	}
}

// TestSettleSilent_EveryUnannouncedReasonIsNamed is the DRIFT GATE on the
// silent row, and it is the check whose absence let `runner_parked` be settled
// as `merged`.
//
// OutcomeSilent is shared by more than one conclusion, and the shared row says
// nothing about which label each of them lands on. settleSilent names them
// individually; a reason added to the silent row upstream and not named there
// falls to its default and is reported undecidable — visible, but a reason with a
// correct label that nobody wired is still a repair the sweep cannot make. This
// fails the suite instead, and names the reason to wire.
func TestSettleSilent_EveryUnannouncedReasonIsNamed(t *testing.T) {
	named := map[orchestration.TaskConclusionReason]bool{
		orchestration.TaskConcludedCompleted:    true,
		orchestration.TaskConcludedRunnerParked: true,
	}

	silent := 0
	for _, reason := range orchestration.TaskConclusionReasons() {
		kind, known := OutcomeOfConclusion(reason)
		if !known || kind != OutcomeSilent {
			continue
		}
		silent++
		if !named[reason] {
			t.Errorf("conclusion %q is unannounced but settleSilent does not name it; "+
				"it would fall to the default and never settle a label", reason)
		}
	}
	if silent < 2 {
		t.Fatalf("found %d unannounced reasons; this gate exists because the row is SHARED and would pass vacuously", silent)
	}
	for reason := range named {
		if kind, known := OutcomeOfConclusion(reason); !known || kind != OutcomeSilent {
			t.Errorf("settleSilent names %q, which is no longer unannounced (%q); it belongs on the announced path",
				reason, kind)
		}
	}
}

// TestLabelReconciler_CompletedTaskStillSettlesOnMerged is the other half of the
// same silence: a completed task must keep landing on `merged`. Without this the
// branch above could be satisfied by never settling on `merged` at all.
func TestLabelReconciler_CompletedTaskStillSettlesOnMerged(t *testing.T) {
	task := testTaskRecord(reconcileIssue)
	task.Status = orchestration.TaskStatusCompleted
	task.ConclusionReason = orchestration.TaskConcludedCompleted
	rig := newReconcileRig(t, []string{"factory:in-progress"}, task)

	if got := rig.forge.issueLabels(reconcileIssue); hasAll(got, []string{"factory:merged"}) {
		t.Fatalf("the fixture already carries the label under test: %v", got)
	}
	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileRelabelled {
		t.Fatalf("verdict = %q (%s), want relabelled", finding.Verdict, finding.Evidence)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:merged"}) {
		t.Fatalf("labels = %v, want factory:merged", got)
	}
	if n := len(rig.outcomeComments()); n != 0 {
		t.Fatalf("a completed task announced %d comment(s), want none", n)
	}
}

// TestLabelReconciler_IncoherentRunnerParkIsUndecidable is the fail-closed edge:
// the park label is read off BlockKind, which means something only while the task
// is actually parked. A record naming a runner park while resting elsewhere is
// reported rather than relabelled from.
func TestLabelReconciler_IncoherentRunnerParkIsUndecidable(t *testing.T) {
	task := testTaskRecord(reconcileIssue)
	task.Status = orchestration.TaskStatusFailed
	task.ConclusionReason = orchestration.TaskConcludedRunnerParked
	rig := newReconcileRig(t, []string{"factory:in-progress"}, task)

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileUndecidable {
		t.Fatalf("verdict = %q (%s), want undecidable", finding.Verdict, finding.Evidence)
	}
	if !strings.Contains(finding.Evidence, "rests in status failed") {
		t.Fatalf("the evidence does not name the incoherence: %s", finding.Evidence)
	}
	if len(rig.forge.labelAdds)+len(rig.forge.labelRemovals) != 0 {
		t.Fatalf("an incoherent record was written from: +%v -%v", rig.forge.labelAdds, rig.forge.labelRemovals)
	}
}

// =============================================================================
// A readmission whose label never came off
// =============================================================================

// readmittedTask is the record ReadmitTask leaves: the park cleared, the task
// back in the queue, and `readmitted` persisted as the reason it stopped being
// blocked.
func readmittedTask() *orchestration.Task {
	task := testTaskRecord(reconcileIssue)
	task.Status = orchestration.TaskStatusPending
	task.Attempts = 2
	task.ConclusionReason = orchestration.TaskConcludedReadmitted
	return task
}

// TestLabelReconciler_ReadmittedTaskHasItsParkLabelCleared is the third face of
// this sweep's one defect, and it was invisible to both other directions.
//
// A readmission IS a conclusion: the readmitted row's label key is the trigger,
// so the park label comes off, the issue shows queued work again, and the next
// attempt's Prepare labels it in progress. When that forge write fails, the record says the task is queued again
// and the issue still says `factory:blocked`. The claim direction lists only
// `in_progress` and `in_review`, so it never sees it; the hand-over direction
// skips pending tasks, so it never sees it either.
func TestLabelReconciler_ReadmittedTaskHasItsParkLabelCleared(t *testing.T) {
	rig := newReconcileRig(t, []string{"factory:blocked", "bug"}, readmittedTask())

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileRelabelled {
		t.Fatalf("verdict = %q (%s), want relabelled", finding.Verdict, finding.Evidence)
	}
	if finding.Outcome != OutcomeReadmitted || finding.TaskID != testTask {
		t.Fatalf("finding = %+v, want the readmission named", finding)
	}
	if got := rig.forge.issueLabels(reconcileIssue); hasAll(got, []string{"factory:blocked"}) {
		t.Fatalf("labels = %v, want the park label gone", got)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"bug", "task:ready"}) {
		t.Fatalf("labels = %v, want the unowned label left alone and the trigger showing queued work", got)
	}

	// NOTHING IS ANNOUNCED. The readmission's own surfacing posts the comment;
	// this repairs only the label that surfacing did not manage to move.
	if got := len(rig.outcomeComments()); got != 0 {
		t.Fatalf("the repair announced an ending: %d comment(s)", got)
	}

	// AND IT IS IDEMPOTENT: with the trigger and nothing else owned, the
	// issue agrees.
	second := only(t, rig.sweep(t))
	if second.Verdict != LabelReconcileConsistent {
		t.Fatalf("second sweep verdict = %q (%s), want consistent", second.Verdict, second.Evidence)
	}
}

// TestLabelReconciler_RestoredHandoverIsNotThenStrippedByTheReadmission is the
// interaction the two directions could have had, and the worst outcome either
// could produce: the sweep leaving ACTIVE work unlabelled.
//
// ClaimTask does not clear ConclusionReason, so a task readmitted and then
// claimed still reads `readmitted` — the reason describes the last thing that
// concluded it, which is what a job page wants it for. With the repair keyed on
// the reason alone, this pass restored `factory:in-progress` for the running
// attempt through the hand-over direction and then stripped it straight back off
// through the readmission direction, in the same pass.
func TestLabelReconciler_RestoredHandoverIsNotThenStrippedByTheReadmission(t *testing.T) {
	// Readmitted, then claimed and running: the reason survives into the
	// attempt exactly as the engine leaves it.
	task := readmittedTask()
	task.Status = orchestration.TaskStatusInProgress

	rig := newReconcileRig(t, []string{"bug"}, task)

	report := rig.sweep(t)
	if len(report.Findings) != 1 {
		t.Fatalf("want exactly one finding, got %d: %+v", len(report.Findings), report.Findings)
	}
	if report.Findings[0].Verdict != LabelReconcileRestored {
		t.Fatalf("verdict = %q (%s), want restored", report.Findings[0].Verdict, report.Findings[0].Evidence)
	}

	// THE LABEL MUST SURVIVE THE PASS THAT APPLIED IT.
	got := rig.forge.issueLabels(reconcileIssue)
	if !hasAll(got, []string{"factory:in-progress"}) {
		t.Fatalf("labels = %v: the sweep restored a live attempt's label and then stripped it, "+
			"leaving active work unlabelled", got)
	}
	if !hasAll(got, []string{"bug"}) {
		t.Fatalf("labels = %v, want the unowned label left alone", got)
	}
	if len(rig.forge.labelRemovals) != 0 {
		t.Fatalf("a live attempt's label was removed: -%v", rig.forge.labelRemovals)
	}
}

// TestLabelReconciler_HandoversAndReadmissionsCannotBothFire holds the property
// the fix above rests on: the two directions that write a label select on
// DISJOINT statuses, so no task can be a candidate for both in one pass.
//
// A status added to both sets would put the sweep back to restoring a label and
// then clearing it, and the pass order would decide which one won — the kind of
// bug that looks like flakiness rather than a defect.
func TestLabelReconciler_HandoversAndReadmissionsCannotBothFire(t *testing.T) {
	for status := range handedOverTaskStatuses {
		task := readmittedTask()
		task.Status = status
		if awaitingRedispatch(task) {
			t.Fatalf("status %q is both handed over and awaiting redispatch; one pass would "+
				"restore its label and then strip it", status)
		}
	}

	// And the converse, so the gate is not vacuous: the queued readmission IS
	// a candidate, and is not a hand-over.
	queued := readmittedTask()
	if !awaitingRedispatch(queued) {
		t.Fatal("a pending readmitted task is not awaiting redispatch; the direction would never fire")
	}
	if handedOverTaskStatuses[queued.Status] {
		t.Fatalf("status %q counts as handed over; the two directions overlap", queued.Status)
	}

	// The reason is required as well as the status: a pending task the engine
	// requeued carries no reason and is not a readmission to repair.
	requeued := readmittedTask()
	requeued.ConclusionReason = ""
	if awaitingRedispatch(requeued) {
		t.Fatal("a requeued task with no conclusion reason was read as a readmission")
	}
}

// TestLabelReconciler_ReadmittedTaskWithACleanIssueIsConsistent is the healthy
// state: the readmission's label move landed, so the issue carries the trigger
// and nothing else this module owns, and there is nothing to do.
func TestLabelReconciler_ReadmittedTaskWithACleanIssueIsConsistent(t *testing.T) {
	rig := newReconcileRig(t, []string{"bug", "task:ready"}, readmittedTask())

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileConsistent {
		t.Fatalf("verdict = %q (%s), want consistent", finding.Verdict, finding.Evidence)
	}
	if len(rig.forge.labelAdds)+len(rig.forge.labelRemovals) != 0 {
		t.Fatalf("a clean issue was written to: +%v -%v", rig.forge.labelAdds, rig.forge.labelRemovals)
	}
}

// TestLabelReconciler_ReadmittedTaskAlreadyRestartedIsTheClaimDirections is the
// boundary between two directions: once the next attempt has started and
// labelled the issue in progress, the issue claims live work and the claim
// direction settles it as healthy. The readmission direction must not also fire
// and strip the claim it just read.
func TestLabelReconciler_ReadmittedTaskAlreadyRestartedIsTheClaimDirections(t *testing.T) {
	task := readmittedTask()
	task.Status = orchestration.TaskStatusInProgress
	rig := newReconcileRig(t, []string{"factory:in-progress"}, task)

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileConsistent {
		t.Fatalf("verdict = %q (%s), want consistent", finding.Verdict, finding.Evidence)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:in-progress"}) {
		t.Fatalf("labels = %v, want the live claim left alone", got)
	}
	if len(rig.forge.labelRemovals) != 0 {
		t.Fatalf("the claim of a running attempt was stripped: -%v", rig.forge.labelRemovals)
	}
}

// TestLabelReconciler_ReadmissionReparkedMidPassIsNotCleared is the freshness
// guard on this direction. A task readmitted and then parked AGAIN while the
// pass was in flight has a new park whose label is correct, and clearing it from
// the snapshot would undo a fresh ending.
func TestLabelReconciler_ReadmissionReparkedMidPassIsNotCleared(t *testing.T) {
	pacer := &firstStepPacer{}
	rig := newReconcileRigPaced(t, pacer, []string{"factory:blocked"}, readmittedTask())
	pacer.act = func() {
		rig.tasks.conclude(testTask, orchestration.TaskBlockKindBlocked, "the account pool is empty")
	}

	finding := only(t, rig.sweep(t))
	if finding.Verdict == LabelReconcileRelabelled {
		t.Fatalf("a fresh park's label was cleared from a stale snapshot: %s", finding.Evidence)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:blocked"}) {
		t.Fatalf("labels = %v, want the new park's label left exactly as it was written", got)
	}
	if len(rig.forge.labelRemovals) != 0 {
		t.Fatalf("a fresh park's label was removed: -%v", rig.forge.labelRemovals)
	}
}

// TestLabelReconciler_ReadmissionDefersWhenTheForgeRefuses is the error path:
// both reads are fail-closed and the label write is retried next sweep.
func TestLabelReconciler_ReadmissionDefersWhenTheForgeRefuses(t *testing.T) {
	for name, arm := range map[string]func(*reconcileRig){
		"issue unreadable": func(r *reconcileRig) { r.forge.getIssueErr = errors.New("forge down") },
		"label write":      func(r *reconcileRig) { r.forge.removeLabelErr = errors.New("forge down") },
	} {
		t.Run(name, func(t *testing.T) {
			rig := newReconcileRig(t, []string{"factory:blocked"}, readmittedTask())
			arm(rig)

			finding := only(t, rig.sweep(t))
			if finding.Verdict != LabelReconcileDeferred {
				t.Fatalf("verdict = %q (%s), want deferred", finding.Verdict, finding.Evidence)
			}
			if !strings.Contains(finding.Evidence, "forge down") {
				t.Fatalf("the evidence does not carry the cause: %s", finding.Evidence)
			}
			if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:blocked"}) {
				t.Fatalf("labels = %v, want the issue untouched", got)
			}
		})
	}
}

// TestLabelReconciler_ReadmissionDefersWhenTheRecordCannotBeReRead is the
// fail-closed half of the freshness guard: an unreadable record is not evidence
// that a readmission still stands.
func TestLabelReconciler_ReadmissionDefersWhenTheRecordCannotBeReRead(t *testing.T) {
	pacer := &firstStepPacer{}
	rig := newReconcileRigPaced(t, pacer, []string{"factory:blocked"}, readmittedTask())
	pacer.act = func() { rig.tasks.failListing(errors.New("store unreachable")) }

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileDeferred {
		t.Fatalf("verdict = %q (%s), want deferred", finding.Verdict, finding.Evidence)
	}
	if !strings.Contains(finding.Evidence, ErrLabelReconcileRecord.Error()) {
		t.Fatalf("the evidence does not carry the record sentinel: %s", finding.Evidence)
	}
	if len(rig.forge.labelRemovals) != 0 {
		t.Fatalf("an unreadable record still wrote: -%v", rig.forge.labelRemovals)
	}
}

// =============================================================================
// A death and an escalation rest in the same shape
// =============================================================================

// reapedTask is the record applyDeath leaves: a job the reaper concluded whose
// attempt budget was spent, parked blocked/escalated -- the SAME resting shape
// an explicit escalation leaves -- and concluded `abandoned` rather than with the
// park's own reason. Only the persisted reason tells the two apart.
func reapedTask() *orchestration.Task {
	task := parkedTask(orchestration.TaskBlockKindEscalated,
		"the node stopped heartbeating\n\nThe attempt budget is spent; readmit the task to try again.")
	task.ConclusionReason = orchestration.TaskConcludedAbandoned
	task.Result = &orchestration.TaskResult{
		Output:   "the node stopped heartbeating",
		Metadata: map[string]string{"job_id": "job-1", "job_status": "failed"},
	}
	return task
}

// TestLabelReconciler_ReapedTaskIsAbandonedNotEscalated is the node-died case
// this sweep exists to repair, and the one the block kind alone gets wrong.
//
// applyDeath parks a reaped job whose budget is spent as blocked/escalated —
// the same resting shape an explicit engine escalation leaves — but concludes
// it `abandoned`, because a death carries no diagnosis. Reading the kind alone
// posted the escalation's diagnosis over it.
func TestLabelReconciler_ReapedTaskIsAbandonedNotEscalated(t *testing.T) {
	rig := newReconcileRig(t, []string{"factory:in-progress"}, reapedTask())

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileSurfaced || finding.Outcome != OutcomeAbandoned {
		t.Fatalf("finding = %+v, want the death surfaced as abandoned", finding)
	}
	comments := rig.outcomeComments()
	if len(comments) != 1 {
		t.Fatalf("want one outcome comment, got %d", len(comments))
	}
	if want := blockedMarker(testTask, 2, OutcomeAbandoned); !strings.Contains(comments[0].Body, want) {
		t.Fatalf("the comment carries no %q marker:\n%s", want, comments[0].Body)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:escalated"}) {
		t.Fatalf("labels = %v, want the abandoned row's label", got)
	}
}

// TestLabelReconciler_EscalatedTaskIsStillEscalated is the negative half: an
// explicit engine escalation carries no reaper class, so it keeps its own
// diagnosis. Without this the fix above would simply move the misreading from
// one ending to the other.
func TestLabelReconciler_EscalatedTaskIsStillEscalated(t *testing.T) {
	task := parkedTask(orchestration.TaskBlockKindEscalated, "the pipeline has no reviewer")
	rig := newReconcileRig(t, []string{"factory:in-progress"}, task)

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileSurfaced || finding.Outcome != OutcomeEscalated {
		t.Fatalf("finding = %+v, want the escalation surfaced as escalated", finding)
	}
	if got := rig.outcomeComments(); len(got) != 1 ||
		!strings.Contains(got[0].Body, "the pipeline has no reviewer") {
		t.Fatalf("the escalation's own reason was not announced: %+v", got)
	}
}

// TestLabelReconciler_ParkedTaskWithNoReasonIsUndecidable is the fail-closed
// edge of reading the record: a blocked task whose ConclusionReason is empty is
// what a requeue leaves and what a record written before the field existed
// carries. Neither is an ending, so the sweep reports it and writes nothing
// rather than inventing one from the block kind.
func TestLabelReconciler_ParkedTaskWithNoReasonIsUndecidable(t *testing.T) {
	task := parkedTask(orchestration.TaskBlockKindBlocked, "the account pool is empty")
	task.ConclusionReason = ""
	rig := newReconcileRig(t, []string{"factory:in-progress"}, task)

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileUndecidable {
		t.Fatalf("verdict = %q (%s), want undecidable", finding.Verdict, finding.Evidence)
	}
	if got := len(rig.outcomeComments()); got != 0 {
		t.Fatalf("an ending was invented for a record that names none: %d comment(s)", got)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:in-progress"}) {
		t.Fatalf("labels = %v, want the issue untouched", got)
	}
}

// TestLabelReconciler_BlockedTaskWithNoResultStillReadsItsReason proves the
// reading does not depend on a job result at all: an explicit park leaves no
// result, and the persisted reason is still what names the ending.
func TestLabelReconciler_BlockedTaskWithNoResultStillReadsItsReason(t *testing.T) {
	task := parkedTask(orchestration.TaskBlockKindBlocked, "the account pool is empty")
	task.Result = nil
	rig := newReconcileRig(t, []string{"factory:in-progress"}, task)

	if finding := only(t, rig.sweep(t)); finding.Outcome != OutcomeBlocked {
		t.Fatalf("outcome = %q, want blocked", finding.Outcome)
	}
}

// =============================================================================
// The converse: a live task whose issue says nothing
// =============================================================================

// TestLabelReconciler_LiveTaskWithNoLabelIsRestored covers the lost hand-over:
// the engine is working the task right now and the issue does not say so. The
// label comes back and NOTHING is announced, because nothing ended.
func TestLabelReconciler_LiveTaskWithNoLabelIsRestored(t *testing.T) {
	task := testTaskRecord(reconcileIssue)
	task.Status = orchestration.TaskStatusInProgress
	rig := newReconcileRig(t, []string{"bug"}, task)

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileRestored {
		t.Fatalf("verdict = %q (%s), want restored", finding.Verdict, finding.Evidence)
	}
	if finding.Outcome != OutcomeSilent {
		t.Fatalf("a restoration announced %q; it must announce nothing", finding.Outcome)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:in-progress", "bug"}) {
		t.Fatalf("labels = %v, want the in-progress label back and `bug` left alone", got)
	}
	if got := rig.outcomeComments(); len(got) != 0 {
		t.Fatalf("a lost hand-over must post nothing, got %d comment(s)", len(got))
	}
}

// TestLabelReconciler_HandoverThatConcludedMidPassIsNotRestored is the
// restoration's own version of the snapshot race, and the worst of the three
// because it UNDOES a correct ending.
//
// The pass selects the issue from a snapshot in which the task is handed over
// and the issue carries no lifecycle label. If the task then concludes while
// the pass is in flight, TaskConcluded posts its outcome and moves the issue
// onto a terminal label -- and a restoration decided from the snapshot strips
// that label back off and re-asserts live work for a task that has stopped.
func TestLabelReconciler_HandoverThatConcludedMidPassIsNotRestored(t *testing.T) {
	live := testTaskRecord(reconcileIssue)
	live.Status = orchestration.TaskStatusInProgress

	pacer := &firstStepPacer{}
	rig := newReconcileRigPaced(t, pacer, []string{"bug"}, live)
	pacer.act = func() {
		// The task concludes and its ending reaches the forge, exactly as
		// TaskConcluded would have written it: the same record transitions,
		// and the issue lands on the label its outcome names.
		rig.tasks.conclude(testTask, orchestration.TaskBlockKindBlocked, "the account pool is empty")
		rig.forge.putIssue(reconcileIssue, "Fix the thing", "bug", "factory:blocked")
	}

	finding := only(t, rig.sweep(t))
	if finding.Verdict == LabelReconcileRestored {
		t.Fatalf("a stopped task's issue was re-labelled as live work: %s", finding.Evidence)
	}
	if finding.Verdict != LabelReconcileUndecidable {
		t.Fatalf("verdict = %q (%s), want undecidable", finding.Verdict, finding.Evidence)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:blocked"}) ||
		hasAll(got, []string{"factory:in-progress"}) {
		t.Fatalf("labels = %v, want the ending's own label left exactly as TaskConcluded wrote it", got)
	}
	if len(rig.forge.labelRemovals) != 0 {
		t.Fatalf("a correct ending's label was removed: -%v", rig.forge.labelRemovals)
	}
}

// TestLabelReconciler_HandoverClaimedAgainMidPassIsConsistent is the other
// half of the re-established premise: the ordinary Prepare hook landed while
// the pass was in flight, so the hand-over was never lost and the healthy
// state needs no write.
func TestLabelReconciler_HandoverClaimedAgainMidPassIsConsistent(t *testing.T) {
	live := testTaskRecord(reconcileIssue)
	live.Status = orchestration.TaskStatusInProgress

	pacer := &firstStepPacer{}
	rig := newReconcileRigPaced(t, pacer, []string{"bug"}, live)
	pacer.act = func() {
		rig.forge.putIssue(reconcileIssue, "Fix the thing", "bug", "factory:in-progress")
	}

	// The claim listing runs at that first step too, so the issue is found
	// claiming and settles through the claim direction as the healthy state.
	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileConsistent {
		t.Fatalf("verdict = %q (%s), want consistent", finding.Verdict, finding.Evidence)
	}
	if len(rig.forge.labelAdds)+len(rig.forge.labelRemovals) != 0 {
		t.Fatalf("the healthy state was written to: +%v -%v", rig.forge.labelAdds, rig.forge.labelRemovals)
	}
}

// TestLabelReconciler_RestorationDefers proves each half of the restoration's
// premise is fail-closed: an unreadable record or issue is not evidence, and a
// refused write lands nothing. The finding carries the typed cause and the
// next sweep tries the same issue again.
func TestLabelReconciler_RestorationDefers(t *testing.T) {
	tests := []struct {
		name string
		// arm breaks one half of the restoration and returns what heals it,
		// or nil when the case proves only the deferral.
		arm            func(rig *reconcileRig, pacer *firstStepPacer) func()
		evidence       []string
		writeAttempted bool
	}{
		{
			name: "the record cannot be re-read",
			arm: func(rig *reconcileRig, pacer *firstStepPacer) func() {
				pacer.act = func() { rig.tasks.failListing(errors.New("store unreachable")) }
				return func() { rig.tasks.failListing(nil) }
			},
			evidence: []string{ErrLabelReconcileRecord.Error()},
		},
		{
			name: "the label write fails",
			arm: func(rig *reconcileRig, pacer *firstStepPacer) func() {
				rig.forge.addLabelErr = errors.New("forge down")
				return func() { rig.forge.addLabelErr = nil }
			},
			evidence:       []string{ErrLabelReconcileDeferred.Error(), "forge down"},
			writeAttempted: true,
		},
		{
			name: "the issue cannot be read",
			arm: func(rig *reconcileRig, pacer *firstStepPacer) func() {
				rig.forge.getIssueErr = errors.New("forge down")
				return nil
			},
			evidence: []string{ErrLabelReconcileForge.Error(), "GetIssue", "forge down"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			live := testTaskRecord(reconcileIssue)
			live.Status = orchestration.TaskStatusInProgress
			pacer := &firstStepPacer{}
			rig := newReconcileRigPaced(t, pacer, []string{"bug"}, live)
			heal := tt.arm(rig, pacer)

			finding := only(t, rig.sweep(t))
			if finding.Verdict != LabelReconcileDeferred {
				t.Fatalf("verdict = %q (%s), want deferred", finding.Verdict, finding.Evidence)
			}
			for _, want := range tt.evidence {
				if !strings.Contains(finding.Evidence, want) {
					t.Fatalf("the evidence does not carry %q: %s", want, finding.Evidence)
				}
			}
			if !tt.writeAttempted && len(rig.forge.labelAdds)+len(rig.forge.labelRemovals) != 0 {
				t.Fatalf("a failed premise still wrote: +%v -%v", rig.forge.labelAdds, rig.forge.labelRemovals)
			}
			if got := rig.forge.issueLabels(reconcileIssue); hasAll(got, []string{"factory:in-progress"}) {
				t.Fatalf("labels = %v, want no in-progress label to have landed", got)
			}

			// AND THE NEXT SWEEP RESTORES IT once the failure clears.
			if heal == nil {
				return
			}
			heal()
			if retried := only(t, rig.sweep(t)); retried.Verdict != LabelReconcileRestored {
				t.Fatalf("the retry = %+v, want the hand-over restored", retried)
			}
			if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:in-progress", "bug"}) {
				t.Fatalf("labels = %v, want the in-progress label back", got)
			}
		})
	}
}

// TestLabelReconciler_RestorationOfADeletedRecordRestoresNothing covers the
// record that disappears between the snapshot and the write: there is no task
// to assert live work for, so the label is not applied and the finding says the
// record is gone rather than naming a task that no longer exists.
func TestLabelReconciler_RestorationOfADeletedRecordRestoresNothing(t *testing.T) {
	live := testTaskRecord(reconcileIssue)
	live.Status = orchestration.TaskStatusInProgress

	pacer := &firstStepPacer{}
	rig := newReconcileRigPaced(t, pacer, []string{"bug"}, live)
	pacer.act = func() { rig.tasks.forget(testTask) }

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileUndecidable {
		t.Fatalf("verdict = %q (%s), want undecidable", finding.Verdict, finding.Evidence)
	}
	if finding.TaskID != "" {
		t.Fatalf("the finding names task %q for a record that is gone", finding.TaskID)
	}
	if !strings.Contains(finding.Evidence, "its record is gone") {
		t.Fatalf("the evidence does not say the record went: %s", finding.Evidence)
	}
	if len(rig.forge.labelAdds)+len(rig.forge.labelRemovals) != 0 {
		t.Fatalf("a deleted record still claimed live work: +%v -%v", rig.forge.labelAdds, rig.forge.labelRemovals)
	}
}

// TestLabelReconciler_RestorationRefusesAnIssueTheAdapterWillNotAnswerFor is
// the fail-closed guard on GetIssue's contract: an adapter answering neither an
// issue nor an error must not be dereferenced, and must not be read as an issue
// carrying no claim.
func TestLabelReconciler_RestorationRefusesAnIssueTheAdapterWillNotAnswerFor(t *testing.T) {
	live := testTaskRecord(reconcileIssue)
	live.Status = orchestration.TaskStatusInProgress
	rig := newReconcileRig(t, []string{"bug"}, live)
	rig.forge.getIssueNil = true

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileUndecidable {
		t.Fatalf("verdict = %q (%s), want undecidable", finding.Verdict, finding.Evidence)
	}
	if !strings.Contains(finding.Evidence, "no longer readable on the forge") {
		t.Fatalf("the evidence does not name the unreadable issue: %s", finding.Evidence)
	}
	if len(rig.forge.labelAdds)+len(rig.forge.labelRemovals) != 0 {
		t.Fatalf("an unanswered issue was written to: +%v -%v", rig.forge.labelAdds, rig.forge.labelRemovals)
	}
}

// TestLabelReconciler_PendingTaskIsLeftAlone is the negative of the
// restoration: a queued task has not been handed over, its issue correctly
// still carries the trigger, and labelling it in progress would claim a run
// that has not started.
func TestLabelReconciler_PendingTaskIsLeftAlone(t *testing.T) {
	task := testTaskRecord(reconcileIssue)
	task.Status = orchestration.TaskStatusPending
	rig := newReconcileRig(t, []string{"task:ready"}, task)

	report := rig.sweep(t)
	if len(report.Findings) != 0 {
		t.Fatalf("a pending task produced findings: %+v", report.Findings)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"task:ready"}) ||
		hasAll(got, []string{"factory:in-progress"}) {
		t.Fatalf("labels = %v, want the trigger untouched", got)
	}
}

// TestLabelReconciler_BindingWithNoInProgressLabelRestoresNothing is the
// negative half of the restoration. With no claiming label to list, no issue
// is ever found claiming, so every live task would look like a lost hand-over
// on every sweep — one forge round trip each, for ever, moving labels onto
// nothing.
func TestLabelReconciler_BindingWithNoInProgressLabelRestoresNothing(t *testing.T) {
	task := testTaskRecord(reconcileIssue)
	task.Status = orchestration.TaskStatusInProgress
	rig := newReconcileRig(t, []string{"task:ready"}, task)

	unlabelled := reconcileBinding()
	delete(unlabelled.Forge.Labels, LabelInProgress)
	if err := rig.store.PutBinding(context.Background(), testTenant, unlabelled); err != nil {
		t.Fatal(err)
	}

	report := rig.sweep(t)
	if len(report.Findings) != 0 {
		t.Fatalf("a binding with no in-progress label produced findings: %+v", report.Findings)
	}
	if len(rig.forge.labelAdds)+len(rig.forge.labelRemovals) != 0 {
		t.Fatalf("the forge was written to: +%v -%v", rig.forge.labelAdds, rig.forge.labelRemovals)
	}
}

// TestLabelReconciler_LiveTaskWithItsClaimIsConsistent is the healthy state:
// the forge says the factory is working and it is. Nothing is written.
func TestLabelReconciler_LiveTaskWithItsClaimIsConsistent(t *testing.T) {
	task := testTaskRecord(reconcileIssue)
	task.Status = orchestration.TaskStatusInProgress
	rig := newReconcileRig(t, []string{"factory:in-progress"}, task)

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileConsistent {
		t.Fatalf("verdict = %q (%s), want consistent", finding.Verdict, finding.Evidence)
	}
	if len(rig.forge.labelAdds)+len(rig.forge.labelRemovals) != 0 {
		t.Fatalf("a consistent issue was written to: +%v -%v", rig.forge.labelAdds, rig.forge.labelRemovals)
	}
	if got := len(rig.outcomeComments()); got != 0 {
		t.Fatalf("a consistent issue got %d comment(s)", got)
	}
}

// TestLabelReconciler_TerminalLabelWithNoTaskIsNotVisited is the guard against
// the demolition this sweep could so easily have been. An issue resting on
// `escalated` with its task long gone is the DOCUMENTED end state of a task
// that spent its budget and released its reservation; a sweep that listed the
// terminal labels too would post `abandoned` over every issue the factory ever
// finished with.
func TestLabelReconciler_TerminalLabelWithNoTaskIsNotVisited(t *testing.T) {
	rig := newReconcileRig(t, []string{"factory:escalated"})

	report := rig.sweep(t)
	if len(report.Findings) != 0 {
		t.Fatalf("a rested issue was examined: %+v", report.Findings)
	}
	if got := len(rig.outcomeComments()); got != 0 {
		t.Fatalf("a rested issue got %d comment(s)", got)
	}
}

// =============================================================================
// Fail-closed
// =============================================================================

// TestLabelReconciler_ForgeErrorIsTypedDeferredAndRetried proves a refusing
// forge costs the sweep one finding and nothing else: the issue is untouched,
// the pass returns no error, and the next sweep — against a forge that answers
// — repairs it.
func TestLabelReconciler_ForgeErrorIsTypedDeferredAndRetried(t *testing.T) {
	rig := newReconcileRig(t, []string{"factory:in-progress"},
		parkedTask(orchestration.TaskBlockKindNeedsInfo, "which database?"))
	rig.forge.listCommentsErr = errors.New("forge down")

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileDeferred {
		t.Fatalf("verdict = %q (%s), want deferred", finding.Verdict, finding.Evidence)
	}
	for _, want := range []string{ErrLabelReconcileDeferred.Error(), ErrLabelReconcileForge.Error(), "forge down"} {
		if !strings.Contains(finding.Evidence, want) {
			t.Fatalf("the evidence does not carry %q: %s", want, finding.Evidence)
		}
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:in-progress"}) {
		t.Fatalf("a deferred issue was written to: %v", got)
	}

	rig.forge.listCommentsErr = nil
	retried := only(t, rig.sweep(t))
	if retried.Verdict != LabelReconcileSurfaced {
		t.Fatalf("the retry verdict = %q (%s), want surfaced", retried.Verdict, retried.Evidence)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:needs-info"}) {
		t.Fatalf("labels = %v, want factory:needs-info", got)
	}
}

// TestLabelReconciler_UndeliverableCommentIsDeferredNotFatal proves a post the
// forge refuses through the whole retry budget leaves a finding rather than
// ending the pass — the exact SurfacingUndeliveredError condition that put the
// seven measured issues into this state to begin with.
func TestLabelReconciler_UndeliverableCommentIsDeferredNotFatal(t *testing.T) {
	rig := newReconcileRig(t, []string{"factory:in-progress"},
		parkedTask(orchestration.TaskBlockKindBlocked, "the runner is offline"))
	rig.forge.postFailures = 10

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileDeferred {
		t.Fatalf("verdict = %q (%s), want deferred", finding.Verdict, finding.Evidence)
	}
	if !strings.Contains(finding.Evidence, "did not reach issue #25") {
		t.Fatalf("the evidence does not name the undelivered post: %s", finding.Evidence)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:in-progress"}) {
		t.Fatalf("the claim was cleared without the outcome reaching the issue: %v", got)
	}
}

// TestLabelReconciler_ListIssuesFailureEndsTheProjectPassTyped proves the one
// forge read the pass cannot proceed without is a typed pass error, not a
// finding: a sweep that could not read the claims must not report the project
// as reconciled.
func TestLabelReconciler_ListIssuesFailureEndsTheProjectPassTyped(t *testing.T) {
	rig := newReconcileRig(t, []string{"factory:in-progress"})
	rig.forge.listIssuesErr = errors.New("forge down")

	_, err := rig.reconciler.ReconcileProject(context.Background(), testTenant, testProject)
	var forgeErr *LabelReconcileForgeError
	if !errors.As(err, &forgeErr) {
		t.Fatalf("err = %v (%T), want *LabelReconcileForgeError", err, err)
	}
	if forgeErr.Operation != "ListIssues" || !errors.Is(err, ErrLabelReconcileForge) {
		t.Fatalf("the error does not name what failed: %+v", forgeErr)
	}
}

// TestLabelReconciler_OperatorFailedTaskIsUndecidable is the fail-closed case.
// applyConclusion fails a task for one reason only — the budget is spent — so a
// failed task with attempts left was retired by some other hand, and announcing
// "this task has spent its attempt budget" on its issue would be false.
func TestLabelReconciler_OperatorFailedTaskIsUndecidable(t *testing.T) {
	task := testTaskRecord(reconcileIssue)
	task.Status = orchestration.TaskStatusFailed
	task.Attempts, task.AttemptBudget = 1, 5
	rig := newReconcileRig(t, []string{"factory:in-progress"}, task)

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileUndecidable {
		t.Fatalf("verdict = %q (%s), want undecidable", finding.Verdict, finding.Evidence)
	}
	if got := len(rig.outcomeComments()); got != 0 {
		t.Fatalf("an undecidable task was announced anyway: %d comment(s)", got)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:in-progress"}) {
		t.Fatalf("an undecidable task's label was moved: %v", got)
	}
}

// TestLabelReconciler_UnnamedBlockKindIsUndecidable covers a parked task whose
// block kind the conclusion vocabulary does not name. The sweep reports it
// rather than guessing.
func TestLabelReconciler_UnnamedBlockKindIsUndecidable(t *testing.T) {
	task := parkedTask(orchestration.TaskBlockKind("invented_yesterday"), "who knows")
	rig := newReconcileRig(t, []string{"factory:in-progress"}, task)

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileUndecidable {
		t.Fatalf("verdict = %q (%s), want undecidable", finding.Verdict, finding.Evidence)
	}
}

// TestLabelReconciler_CancelledSweepWritesNothingFurther proves a cancelled
// context ends the pass without a partial write and without a hot loop.
func TestLabelReconciler_CancelledSweepWritesNothingFurther(t *testing.T) {
	rig := newReconcileRig(t, []string{"factory:in-progress"},
		parkedTask(orchestration.TaskBlockKindBlocked, "stop here"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := rig.reconciler.ReconcileProject(ctx, testTenant, testProject)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := len(rig.outcomeComments()); got != 0 {
		t.Fatalf("a cancelled sweep posted %d comment(s)", got)
	}
}

// =============================================================================
// The fleet pass
// =============================================================================

// TestLabelReconciler_ReconcileWalksEveryBoundProject proves the boot and
// periodic entry point reaches the project through the tenant listing, which is
// the only way the sweep runs without an operator asking it to.
func TestLabelReconciler_ReconcileWalksEveryBoundProject(t *testing.T) {
	rig := newReconcileRig(t, []string{"factory:in-progress"},
		parkedTask(orchestration.TaskBlockKindEscalated, "review did not converge"))

	report, err := rig.reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := report.Count(LabelReconcileSurfaced); got != 1 {
		t.Fatalf("surfaced = %d, want 1: %+v", got, report.Findings)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"factory:escalated"}) {
		t.Fatalf("labels = %v, want factory:escalated", got)
	}
	if rig.pacer.paces.Load() == 0 {
		t.Fatal("the sweep made forge calls without pacing any of them")
	}
}

// TestLabelReconciler_TenantListingFailureIsTypedAndTotal proves the one fault
// that ends the whole pass is reported as such: a sweep that cannot enumerate
// its tenants has established nothing and must not look like a clean run.
func TestLabelReconciler_TenantListingFailureIsTypedAndTotal(t *testing.T) {
	rig := newReconcileRig(t, []string{"factory:in-progress"})
	rig.tenants.err = errors.New("broker down")

	report, err := rig.reconciler.Reconcile(context.Background())
	var unswept *LabelReconcileUnsweptError
	if !errors.As(err, &unswept) || !errors.Is(err, ErrLabelReconcileUnswept) {
		t.Fatalf("err = %v (%T), want *LabelReconcileUnsweptError", err, err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("a sweep that never started reported findings: %+v", report.Findings)
	}
}

// TestLabelReconciler_UnreconcilableProjectDefersRatherThanStops proves one
// project's fault does not abandon the fleet: it becomes a deferred finding
// naming the project, and the pass returns no error.
func TestLabelReconciler_UnreconcilableProjectDefersRatherThanStops(t *testing.T) {
	rig := newReconcileRig(t, []string{"factory:in-progress"})
	rig.forge.listIssuesErr = errors.New("forge down")

	report, err := rig.reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("one bad project ended the fleet pass: %v", err)
	}
	finding := only(t, report)
	if finding.Verdict != LabelReconcileDeferred || finding.ProjectID != testProject || finding.Issue != 0 {
		t.Fatalf("finding = %+v, want a project-scoped deferral", finding)
	}
	if !strings.Contains(finding.Evidence, "retried on the next sweep") {
		t.Fatalf("the evidence does not say what happens next: %s", finding.Evidence)
	}
}

// TestLabelReconciler_TenantWithNoListableBindingsDefers proves a tenant whose
// bindings cannot be read is reported rather than silently skipped.
func TestLabelReconciler_TenantWithNoListableBindingsDefers(t *testing.T) {
	rig := newReconcileRig(t, []string{"factory:in-progress"})
	failing, err := NewLabelReconciler(LabelReconcilerConfig{
		Tenants:  rig.tenants,
		Bindings: &fakeBindings{err: errors.New("store down")},
		Tasks:    rig.tasks,
		Hooks:    rig.reconciler.hooks,
		Pacer:    rig.pacer,
	})
	if err != nil {
		t.Fatal(err)
	}

	report, err := failing.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("an unreadable tenant ended the fleet pass: %v", err)
	}
	finding := only(t, report)
	if finding.Verdict != LabelReconcileDeferred || finding.TenantID != testTenant || finding.ProjectID != "" {
		t.Fatalf("finding = %+v, want a tenant-scoped deferral", finding)
	}
}

// =============================================================================
// Construction and scope
// =============================================================================

// TestNewLabelReconciler_AcceptsACompleteConfig is the positive half.
func TestNewLabelReconciler_AcceptsACompleteConfig(t *testing.T) {
	rig := newReconcileRig(t, nil)
	reconciler, err := NewLabelReconciler(LabelReconcilerConfig{
		Tenants: rig.tenants, Bindings: rig.store, Tasks: rig.tasks, Hooks: rig.reconciler.hooks,
	})
	if err != nil {
		t.Fatalf("NewLabelReconciler: %v", err)
	}
	if reconciler.pacer == nil {
		t.Fatal("a config naming no pacer must take the default rather than leaving the forge unpaced")
	}
}

// TestNewLabelReconciler_RefusesAnIncompleteConfig proves every collaborator is
// required and named: a sweep missing one of them would report a fleet as
// reconciled while visiting none of it.
func TestNewLabelReconciler_RefusesAnIncompleteConfig(t *testing.T) {
	rig := newReconcileRig(t, nil)
	complete := func() LabelReconcilerConfig {
		return LabelReconcilerConfig{
			Tenants: rig.tenants, Bindings: rig.store, Tasks: rig.tasks, Hooks: rig.reconciler.hooks,
		}
	}
	for field, strip := range map[string]func(*LabelReconcilerConfig){
		"tenants":  func(c *LabelReconcilerConfig) { c.Tenants = nil },
		"bindings": func(c *LabelReconcilerConfig) { c.Bindings = nil },
		"tasks":    func(c *LabelReconcilerConfig) { c.Tasks = nil },
		"hooks":    func(c *LabelReconcilerConfig) { c.Hooks = nil },
	} {
		cfg := complete()
		strip(&cfg)
		_, err := NewLabelReconciler(cfg)
		var invalid *InvalidInputError
		if !errors.As(err, &invalid) || invalid.Field != field {
			t.Fatalf("a config missing %s: err = %v, want an InvalidInputError naming it", field, err)
		}
	}
}

// TestLabelReconciler_ReconcileProject_RefusesAnUnscopedCall proves the pass
// will not run against an unnamed tenant or project, which on a shared store
// would be a sweep over somebody else's issues.
func TestLabelReconciler_ReconcileProject_RefusesAnUnscopedCall(t *testing.T) {
	rig := newReconcileRig(t, nil)
	for _, scope := range [][2]string{{"", testProject}, {testTenant, ""}, {"", ""}} {
		_, err := rig.reconciler.ReconcileProject(context.Background(), scope[0], scope[1])
		var invalid *InvalidInputError
		if !errors.As(err, &invalid) || invalid.Field != "project" {
			t.Fatalf("scope %v: err = %v, want an InvalidInputError", scope, err)
		}
	}
}

// TestLabelReconciler_ReconcileProject_RefusesAnUnboundProject proves a project
// with no binding is an error rather than a silent no-op: the sweep has no
// forge to reconcile against and must say so.
func TestLabelReconciler_ReconcileProject_RefusesAnUnboundProject(t *testing.T) {
	rig := newReconcileRig(t, nil)
	if _, err := rig.reconciler.ReconcileProject(context.Background(), testTenant, "unbound"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// =============================================================================
// Reading the engine's record
// =============================================================================

// TestRestingConclusion_ReadsTheReasonTheRecordCarries is the positive half:
// whatever the engine persisted is what the sweep announces, across the whole
// vocabulary, with no re-derivation in between.
func TestRestingConclusion_ReadsTheReasonTheRecordCarries(t *testing.T) {
	for _, reason := range orchestration.TaskConclusionReasons() {
		task := testTaskRecord(reconcileIssue)
		task.ConclusionReason = reason

		got, named := restingConclusion(task)
		if !named || got != reason {
			t.Fatalf("%s: restingConclusion = %q named=%v, want the reason on the record", reason, got, named)
		}
	}

	// AND THE TWO ENDINGS THAT SHARE A SHAPE COME APART, which re-deriving from
	// the status could not do: both rest blocked/escalated.
	death, escalation := reapedTask(), parkedTask(orchestration.TaskBlockKindEscalated, "review did not converge")
	if death.Status != escalation.Status || death.BlockKind != escalation.BlockKind {
		t.Fatalf("the fixtures no longer share a resting shape, so this proves nothing: %s/%s vs %s/%s",
			death.Status, death.BlockKind, escalation.Status, escalation.BlockKind)
	}
	gotDeath, _ := restingConclusion(death)
	gotEsc, _ := restingConclusion(escalation)
	if gotDeath != orchestration.TaskConcludedAbandoned || gotEsc != orchestration.TaskConcludedParkedEscalated {
		t.Fatalf("death = %q, escalation = %q; the shared shape was not told apart", gotDeath, gotEsc)
	}
}

// TestRestingConclusion_RefusesWhatItCannotRead is the negative half: a live
// task, a nil task, an unreachable status and a failed task whose budget is not
// spent all answer false rather than yielding an ending nobody concluded.
func TestRestingConclusion_RefusesWhatItCannotRead(t *testing.T) {
	live := testTaskRecord(reconcileIssue)
	live.Status = orchestration.TaskStatusInProgress

	cancelled := testTaskRecord(reconcileIssue)
	cancelled.Status = orchestration.TaskStatusCancelled

	retired := testTaskRecord(reconcileIssue)
	retired.Status = orchestration.TaskStatusFailed
	retired.Attempts, retired.AttemptBudget = 1, 9

	for name, task := range map[string]*orchestration.Task{
		"nil": nil, "live": live, "cancelled": cancelled, "retired-by-hand": retired,
	} {
		if reason, named := restingConclusion(task); named {
			t.Fatalf("%s: restingConclusion named %q; it must refuse", name, reason)
		}
	}
}

// TestIndexTasksByIssue_TheLiveTaskAnswers proves the index picks the task the
// issue's label is actually about: a terminal task releases its reservation, so
// an issue can carry a spent task and a fresh live one at once.
func TestIndexTasksByIssue_TheLiveTaskAnswers(t *testing.T) {
	spent := parkedTask(orchestration.TaskBlockKindEscalated, "gave up")
	spent.ID, spent.CreatedAt = "task-old", 100

	fresh := testTaskRecord(reconcileIssue)
	fresh.ID, fresh.CreatedAt, fresh.Status = "task-new", 50, orchestration.TaskStatusInProgress

	byIssue := indexTasksByIssue(testRepoRef, []*orchestration.Task{spent, fresh})
	if got := byIssue[reconcileIssue]; got == nil || got.ID != "task-new" {
		t.Fatalf("index chose %+v, want the live task even though it is older", got)
	}

	newer := parkedTask(orchestration.TaskBlockKindBlocked, "later")
	newer.ID, newer.CreatedAt = "task-newest", 900
	byIssue = indexTasksByIssue(testRepoRef, []*orchestration.Task{spent, newer})
	if got := byIssue[reconcileIssue]; got == nil || got.ID != "task-newest" {
		t.Fatalf("index chose %+v, want the newer of two terminal tasks", got)
	}
}

// TestIndexTasksByIssue_SkipsWhatIsNotThisRepositorysIssue is the negative
// half: a task from another source, another repository, or no source at all is
// not this sweep's to reconcile.
func TestIndexTasksByIssue_SkipsWhatIsNotThisRepositorysIssue(t *testing.T) {
	sourceless := testTaskRecord(reconcileIssue)
	sourceless.ID, sourceless.Source = "task-sourceless", nil

	elsewhere := testTaskRecord(reconcileIssue)
	elsewhere.ID = "task-elsewhere"
	elsewhere.Source = &orchestration.TaskSourceRef{
		Kind: TaskSourceKindForgeIssue, Ref: IssueRefString(RepoRef{Owner: "other", Repo: "repo"}, reconcileIssue),
	}

	operator := testTaskRecord(reconcileIssue)
	operator.ID = "task-operator"
	operator.Source = &orchestration.TaskSourceRef{Kind: orchestration.TaskSourceKindOperator, Ref: "by hand"}

	byIssue := indexTasksByIssue(testRepoRef, []*orchestration.Task{nil, sourceless, elsewhere, operator})
	if len(byIssue) != 0 {
		t.Fatalf("index adopted work that is not this repository's: %+v", byIssue)
	}
}

// TestLabelsAgree_AnswersBothWays proves the comparison that decides whether
// anything has to be written: exactly the desired label and no other of the
// module's own.
func TestLabelsAgree_AnswersBothWays(t *testing.T) {
	binding := reconcileBinding()
	for name, tc := range map[string]struct {
		labels []string
		key    string
		want   bool
	}{
		"exactly the desired one":      {[]string{"factory:blocked", "bug"}, LabelBlocked, true},
		"desired plus another of ours": {[]string{"factory:blocked", "factory:in-progress"}, LabelBlocked, false},
		"the wrong one":                {[]string{"factory:in-progress"}, LabelBlocked, false},
		"none of ours, none wanted":    {[]string{"bug"}, "", true},
		"one of ours, none wanted":     {[]string{"factory:merged"}, "", false},
		"the trigger is ours too":      {[]string{"factory:blocked", "task:ready"}, LabelBlocked, false},
	} {
		got := labelsAgree(binding, ForgeIssue{Number: reconcileIssue, Labels: tc.labels}, tc.key)
		if got != tc.want {
			t.Fatalf("%s: labelsAgree = %v, want %v", name, got, tc.want)
		}
	}
}

// TestClaimingLabels_AreTheLiveOnesDeduplicated proves the sweep lists only the
// labels that assert live work, once each.
func TestClaimingLabels_AreTheLiveOnesDeduplicated(t *testing.T) {
	binding := reconcileBinding()
	got := claimingLabels(binding)
	if len(got) != 2 || got[0] != "factory:in-progress" || got[1] != "factory:in-review" {
		t.Fatalf("claimingLabels = %v, want the two claiming labels in key order", got)
	}

	binding.Forge.Labels[LabelInReview] = "factory:in-progress"
	if got := claimingLabels(binding); len(got) != 1 {
		t.Fatalf("claimingLabels = %v, want one listing for two keys naming one label", got)
	}

	binding.Forge.Labels = nil
	if got := claimingLabels(binding); len(got) != 0 {
		t.Fatalf("claimingLabels = %v, want none for a binding that maps no labels", got)
	}
}

// =============================================================================
// The pacer
// =============================================================================

// TestIntervalPacer_AdmitsTheFirstStepAndSpacesTheNext proves the rate limit
// holds without the test waiting for it: the clock is driven, not slept on.
func TestIntervalPacer_AdmitsTheFirstStepAndSpacesTheNext(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	pacer := &intervalPacer{interval: time.Second, now: func() time.Time { return clock }}
	ctx := context.Background()

	if err := pacer.Pace(ctx); err != nil {
		t.Fatalf("the first step must be admitted at once: %v", err)
	}
	// A second step at the same instant must wait, so the clock is advanced
	// past the interval before asking — proving the bound is read from the
	// clock rather than ignored.
	clock = clock.Add(2 * time.Second)
	if err := pacer.Pace(ctx); err != nil {
		t.Fatalf("a step past the interval must be admitted: %v", err)
	}
	if got := pacer.last.Load(); got != clock.UnixNano() {
		t.Fatalf("the pacer recorded %d, want the instant it admitted (%d)", got, clock.UnixNano())
	}
}

// TestNewForgePacer_TakesTheDefaultForANonPositiveInterval proves the exported
// constructor cannot produce an unpaced sweep: zero in a configuration file
// means "take the default" everywhere else in this module.
func TestNewForgePacer_TakesTheDefaultForANonPositiveInterval(t *testing.T) {
	for name, interval := range map[string]time.Duration{"zero": 0, "negative": -time.Second} {
		pacer, ok := NewForgePacer(interval).(*intervalPacer)
		if !ok {
			t.Fatalf("%s: NewForgePacer returned something else entirely", name)
		}
		if pacer.interval != DefaultForgeCallInterval {
			t.Fatalf("%s: interval = %s, want the default %s", name, pacer.interval, DefaultForgeCallInterval)
		}
	}
	pacer, ok := NewForgePacer(3 * time.Second).(*intervalPacer)
	if !ok || pacer.interval != 3*time.Second {
		t.Fatalf("an explicit interval was not honoured: %+v", pacer)
	}
	if err := pacer.Pace(context.Background()); err != nil {
		t.Fatalf("the first step must be admitted at once: %v", err)
	}
}

// TestIntervalPacer_RefusesACancelledSweep is the negative half: a pacer that
// ignored cancellation would hold a shutdown open for its whole interval, and
// one with no interval at all would be a hot loop.
func TestIntervalPacer_RefusesACancelledSweep(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	pacer := &intervalPacer{interval: time.Hour, now: func() time.Time { return clock }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := pacer.Pace(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	unpaced := &intervalPacer{interval: 0, now: func() time.Time { return clock }}
	if err := unpaced.Pace(context.Background()); err != nil {
		t.Fatalf("a pacer with no interval must admit rather than spin: %v", err)
	}
}

// =============================================================================
// Report and errors
// =============================================================================

// TestLabelReconcileReport_CountsAndFiltersItsFindings proves the shapes the
// host logs from.
func TestLabelReconcileReport_CountsAndFiltersItsFindings(t *testing.T) {
	report := LabelReconcileReport{Findings: []LabelReconcileFinding{
		{TenantID: "b", ProjectID: "p", Issue: 2, Verdict: LabelReconcileConsistent},
		{TenantID: "a", ProjectID: "p", Issue: 9, Verdict: LabelReconcileSurfaced},
		{TenantID: "a", ProjectID: "p", Issue: 1, Verdict: LabelReconcileDeferred},
	}}
	report.sort()
	if report.Findings[0].Issue != 1 || report.Findings[2].TenantID != "b" {
		t.Fatalf("findings are not in a comparable order: %+v", report.Findings)
	}
	if got := report.Count(LabelReconcileSurfaced); got != 1 {
		t.Fatalf("Count(surfaced) = %d, want 1", got)
	}
	if got := report.Drifted(); len(got) != 2 {
		t.Fatalf("Drifted() = %+v, want the two non-consistent findings", got)
	}
	if got := (LabelReconcileReport{}).Count(LabelReconcileSurfaced); got != 0 {
		t.Fatalf("an empty report counted %d", got)
	}
	if got := (LabelReconcileReport{}).Drifted(); len(got) != 0 {
		t.Fatalf("an empty report drifted %+v", got)
	}
}

// TestLabelReconcileVerdict_NamesWhatItWrote proves the predicate the host logs
// on, both ways.
func TestLabelReconcileVerdict_NamesWhatItWrote(t *testing.T) {
	for _, v := range []LabelReconcileVerdict{LabelReconcileSurfaced, LabelReconcileRelabelled, LabelReconcileRestored} {
		if !v.Wrote() || v.String() != string(v) {
			t.Fatalf("%q must report as a write", v)
		}
	}
	for _, v := range []LabelReconcileVerdict{LabelReconcileConsistent, LabelReconcileDeferred, LabelReconcileUndecidable} {
		if v.Wrote() {
			t.Fatalf("%q must not report as a write", v)
		}
	}
}

// TestLabelReconcileFinding_Subject proves the log subject names the issue when
// there is one and the project when there is not.
func TestLabelReconcileFinding_Subject(t *testing.T) {
	if got := (LabelReconcileFinding{TenantID: "t", ProjectID: "p", Issue: 7}).Subject(); got != "t/p#7" {
		t.Fatalf("Subject = %q, want t/p#7", got)
	}
	if got := (LabelReconcileFinding{TenantID: "t", ProjectID: "p"}).Subject(); got != "t/p" {
		t.Fatalf("Subject = %q, want t/p", got)
	}
}

// TestLabelReconcileErrors_CarryTheirCauseAndSentinel proves each typed error
// names what failed and unwraps to both its sentinel and its cause.
func TestLabelReconcileErrors_CarryTheirCauseAndSentinel(t *testing.T) {
	cause := errors.New("forge down")
	for name, tc := range map[string]struct {
		err      error
		sentinel error
		contains string
	}{
		"unswept":  {&LabelReconcileUnsweptError{Cause: cause}, ErrLabelReconcileUnswept, "could not be enumerated"},
		"forge":    {&LabelReconcileForgeError{Repo: "acme/api", Operation: "ListIssues", Cause: cause}, ErrLabelReconcileForge, "ListIssues on acme/api"},
		"deferred": {&LabelReconcileDeferredError{Repo: "acme/api", Issue: 25, TaskID: "t-1", Cause: cause}, ErrLabelReconcileDeferred, "acme/api#25 task=t-1"},
		"record": {&LabelReconcileRecordError{TenantID: "tenant-a", ProjectID: "proj-1", Issue: 25, Cause: cause},
			ErrLabelReconcileRecord, "tenant-a/proj-1#25 was not orphaned"},
	} {
		if !errors.Is(tc.err, tc.sentinel) {
			t.Fatalf("%s does not unwrap to its sentinel", name)
		}
		if !errors.Is(tc.err, cause) {
			t.Fatalf("%s does not unwrap to its cause", name)
		}
		if !strings.Contains(tc.err.Error(), tc.contains) {
			t.Fatalf("%s does not name what failed: %s", name, tc.err.Error())
		}
	}
}

// TestLabelReconcileErrors_WithoutACauseStillReadAsThemselves is the negative
// half: an error built with no cause must still carry its sentinel rather than
// unwrapping to a nil that errors.Is reads as a match for anything.
func TestLabelReconcileErrors_WithoutACauseStillReadAsThemselves(t *testing.T) {
	for name, tc := range map[string]struct {
		err      error
		sentinel error
	}{
		"unswept":  {&LabelReconcileUnsweptError{}, ErrLabelReconcileUnswept},
		"forge":    {&LabelReconcileForgeError{Repo: "acme/api", Operation: "ListIssues"}, ErrLabelReconcileForge},
		"deferred": {&LabelReconcileDeferredError{Repo: "acme/api", Issue: 25}, ErrLabelReconcileDeferred},
		"record":   {&LabelReconcileRecordError{TenantID: "tenant-a", ProjectID: "proj-1", Issue: 25}, ErrLabelReconcileRecord},
	} {
		if !errors.Is(tc.err, tc.sentinel) {
			t.Fatalf("%s does not unwrap to its sentinel", name)
		}
		if errors.Is(tc.err, errors.New("anything")) {
			t.Fatalf("%s matched an unrelated error", name)
		}
		if tc.err.Error() == "" {
			t.Fatalf("%s renders nothing", name)
		}
	}
}

// TestLabelReconciler_AQueuedReadmissionWithNoStateLabelGetsTheTrigger is X11
// (go-luks2#211, 2026-09-25) as the sweep sees it: a readmitted task queued
// behind a stripped trigger carried no factory label at all. The sweep puts
// the trigger back, the display state of queued work.
func TestLabelReconciler_AQueuedReadmissionWithNoStateLabelGetsTheTrigger(t *testing.T) {
	rig := newReconcileRig(t, []string{"bug"}, readmittedTask())

	finding := only(t, rig.sweep(t))
	if finding.Verdict != LabelReconcileRelabelled {
		t.Fatalf("verdict = %q (%s), want relabelled", finding.Verdict, finding.Evidence)
	}
	if got := rig.forge.issueLabels(reconcileIssue); !hasAll(got, []string{"bug", "task:ready"}) {
		t.Fatalf("labels = %v, want the trigger back beside the unowned label", got)
	}
	if len(rig.forge.labelRemovals) != 0 {
		t.Fatalf("nothing owned was on the issue, yet labels were removed: -%v", rig.forge.labelRemovals)
	}
}
