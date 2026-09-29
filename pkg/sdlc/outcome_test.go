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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The table
// =============================================================================

// TestConclusionOutcomes_CoverEveryReason is the guard go-quicbaas#788 asks
// for: a conclusion reason the engine can produce and this module cannot
// render would conclude a task in silence, which is the whole defect.
func TestConclusionOutcomes_CoverEveryReason(t *testing.T) {
	reasons := orchestration.TaskConclusionReasons()
	if len(reasons) == 0 {
		t.Fatal("the engine names no conclusion reasons at all")
	}
	for _, reason := range reasons {
		kind, known := OutcomeOfConclusion(reason)
		if !known {
			t.Fatalf("conclusion reason %q has no row in conclusionOutcomes", reason)
		}
		if kind == OutcomeSilent {
			continue
		}
		spec, rendered := lookupOutcomeSpec(kind)
		if !rendered {
			t.Fatalf("conclusion reason %q maps to outcome %q, which has no rendering", reason, kind)
		}
		if spec.headline == "" || spec.meaning == "" {
			t.Fatalf("outcome %q renders nothing a reader can act on: %+v", kind, spec)
		}
	}
	if len(conclusionOutcomes) != len(reasons) {
		t.Fatalf("the table has %d rows for %d reasons; a row for a reason the engine does not name is dead",
			len(conclusionOutcomes), len(reasons))
	}
}

// TestOutcomeOfConclusion_RefusesAReasonWithNoRow proves the lookup answers
// false rather than yielding the zero outcome, which would read as "nothing
// to say" for a reason nobody wrote a row for.
func TestOutcomeOfConclusion_RefusesAReasonWithNoRow(t *testing.T) {
	if kind, known := OutcomeOfConclusion(orchestration.TaskConclusionReason("invented_yesterday")); known {
		t.Fatalf("an unnamed reason must not resolve, got %q", kind)
	}
	if kind, known := OutcomeOfConclusion(orchestration.TaskConcludedBudgetExhausted); !known || kind != OutcomeExhausted {
		t.Fatalf("budget_exhausted must render as exhausted, got %q known=%v", kind, known)
	}
}

// TestOutcomeKind_String proves the kind renders as its wire name, which is
// what the comment marker carries.
func TestOutcomeKind_String(t *testing.T) {
	if OutcomeExhausted.String() != "exhausted" || OutcomeSilent.String() != "" {
		t.Fatalf("kinds render wrong: %q %q", OutcomeExhausted.String(), OutcomeSilent.String())
	}
}

// TestBlockedOutcome_MapsEveryKindAndFallsBack proves the Blocked moment and
// the conclusion seam read the same rows, and that an unknown block kind is
// still rendered as something.
func TestBlockedOutcome_MapsEveryKindAndFallsBack(t *testing.T) {
	for kind, want := range map[orchestration.TaskBlockKind]OutcomeKind{
		orchestration.TaskBlockKindNeedsInfo: OutcomeNeedsInfo,
		orchestration.TaskBlockKindBlocked:   OutcomeBlocked,
		orchestration.TaskBlockKindEscalated: OutcomeEscalated,
	} {
		if got := blockedOutcome(kind); got != want {
			t.Fatalf("block kind %q maps to %q, want %q", kind, got, want)
		}
	}
	if got := blockedOutcome(orchestration.TaskBlockKind("gibberish")); got != OutcomeBlocked {
		t.Fatalf("an unknown block kind must read as blocked, got %q", got)
	}
	if got := blockedLabelKey(orchestration.TaskBlockKind("gibberish")); got != LabelBlocked {
		t.Fatalf("an unknown block kind must label blocked, got %q", got)
	}
}

// =============================================================================
// Rendering
// =============================================================================

func exhaustedTask() *orchestration.Task {
	task := testTaskRecord(25)
	task.Status = orchestration.TaskStatusFailed
	task.Attempts = 3
	task.AttemptBudget = 3
	task.Metadata = map[string]string{"attempt_history": "job-1,job-2,job-3"}
	task.Result = &orchestration.TaskResult{
		Output:   "sdlc: task task-1 produced no changes in /workspace/api",
		Metadata: map[string]string{"reason": "budget_exhausted", "job_status": "failed"},
	}
	return task
}

// TestRenderOutcome_ExhaustedCarriesTheEvidence proves the comment a spent
// budget posts says what happened, what was tried, and that the task is over.
func TestRenderOutcome_ExhaustedCarriesTheEvidence(t *testing.T) {
	binding := testBinding()
	binding.Forge.Labels[LabelEscalated] = "factory:escalated"
	task := exhaustedTask()
	body := renderOutcome(outcomeReport{binding: binding, task: task, attempt: 3, kind: OutcomeExhausted,
		message: conclusionEvidence(task)})

	for _, want := range []string{
		"### This task has spent its attempt budget",
		"**Outcome:** `exhausted`",
		"> sdlc: task task-1 produced no changes in /workspace/api",
		"**Attempt:** 3 of 3",
		"**Attempt history:** `job-1`, `job-2`, `job-3`",
		"**Labels:** `task:ready` → `factory:escalated`",
		"cannot be readmitted",
		blockedMarker(task.ID, 3, OutcomeExhausted),
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the exhausted comment lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "**Review rounds:**") || strings.Contains(body, "**Pull request:**") {
		t.Fatalf("nothing that did not happen may be reported:\n%s", body)
	}
}

// TestRenderOutcome_ReadmittedSaysNothingIsRequired proves the readmission
// comment is not a park: it quotes no reason it does not have and asks the
// reader for nothing.
func TestRenderOutcome_ReadmittedSaysNothingIsRequired(t *testing.T) {
	body := renderOutcome(outcomeReport{binding: testBinding(), task: testTaskRecord(25), attempt: 1, kind: OutcomeReadmitted})
	if !strings.Contains(body, "### This task has been readmitted") || !strings.Contains(body, "Nothing is required of you") {
		t.Fatalf("readmission comment reads wrong:\n%s", body)
	}
	if strings.Contains(body, "recorded no reason") {
		t.Fatalf("a readmission has no park reason to invent:\n%s", body)
	}
	if !strings.Contains(body, "**Labels:** `task:ready` kept until the next attempt starts") {
		t.Fatalf("a readmission keeps the trigger, the display state of queued work:\n%s", body)
	}
}

// TestRenderOutcome_UnknownKindStillRenders proves a kind with no row does not
// produce an empty comment: something parked the task and the reader is told
// so, in the plainest row there is.
func TestRenderOutcome_UnknownKindStillRenders(t *testing.T) {
	body := renderOutcome(outcomeReport{kind: OutcomeKind("who_knows")})
	if !strings.Contains(body, "### This task is blocked") || !strings.Contains(body, "`who_knows`") {
		t.Fatalf("an unknown kind must still render:\n%s", body)
	}
	if !strings.Contains(body, "recorded no reason") {
		t.Fatalf("a terminal outcome with no evidence says so:\n%s", body)
	}
}

// TestOutcomeRemedy_EveryEndingInPrecedenceOrder proves each remedy is
// reachable, says something different, and that the order of the cases is the
// one that matters: a spent budget outranks a machine-checkable condition,
// because no sweep can return a task with no attempts left.
func TestOutcomeRemedy_EveryEndingInPrecedenceOrder(t *testing.T) {
	spent := exhaustedTask()
	fresh := testTaskRecord(25)
	var none UnblockCondition
	green := UnblockCondition{Kind: UnblockPipelineGreen, Subject: "deadbeef"}
	ruling := UnblockCondition{Kind: UnblockOwnerRuling}

	if got := outcomeRemedy(outcomeSpecs[OutcomeReadmitted], fresh, none, false); !strings.Contains(got, "back in the queue") {
		t.Fatalf("readmitted remedy: %q", got)
	}
	if got := outcomeRemedy(outcomeSpecs[OutcomeExhausted], spent, none, false); !strings.Contains(got, "cannot be readmitted") {
		t.Fatalf("exhausted remedy: %q", got)
	}
	if got := outcomeRemedy(outcomeSpecs[OutcomeEscalated], spent, green, true); !strings.Contains(got, "cannot be resumed as it stands") {
		t.Fatalf("a spent budget must outrank a machine-checkable condition: %q", got)
	}
	if got := outcomeRemedy(outcomeSpecs[OutcomeBlocked], fresh, green, true); !strings.Contains(got, "nothing here") {
		t.Fatalf("a machine-checkable condition needs no person: %q", got)
	}
	if got := outcomeRemedy(outcomeSpecs[OutcomeNeedsInfo], fresh, ruling, true); !strings.Contains(got, "**To resume:**") ||
		!strings.Contains(got, fresh.ID) {
		t.Fatalf("an owner ruling needs the owner: %q", got)
	}
	if got := outcomeRemedy(outcomeSpecs[OutcomeNeedsInfo], nil, none, false); strings.Contains(got, " task readmit ") {
		t.Fatalf("a remedy for no task must not name one: %q", got)
	}
}

// TestRenderOutcome_CarriesTheTypedCondition proves the conclusion seam
// composes with go-quicbaas#784: a park reason carrying a typed condition is
// split the same way whichever path renders it, and the machine marker never
// reaches a reader.
func TestRenderOutcome_CarriesTheTypedCondition(t *testing.T) {
	reason, err := ComposeBlockedReason(UnblockCondition{Kind: UnblockPipelineGreen, Subject: "deadbeef"}, "the release build is red")
	if err != nil {
		t.Fatalf("ComposeBlockedReason: %v", err)
	}
	task := testTaskRecord(25)
	task.Status = orchestration.TaskStatusBlocked
	task.BlockKind = orchestration.TaskBlockKindBlocked
	task.BlockedReason = reason

	body := renderOutcome(outcomeReport{binding: testBinding(), task: task, attempt: 1,
		kind: OutcomeBlocked, message: conclusionEvidence(task)})
	for _, want := range []string{"> the release build is red", "**Unblocks when:** ", "**Cleared by:** ", "nothing here"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the comment lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, unblockMarkerOpen) {
		t.Fatalf("the machine marker must not reach a reader:\n%s", body)
	}
}

// TestConclusionEvidence_PrefersThePark proves the evidence is read off the
// concluded record: the park reason when there is one, and otherwise the last
// attempt's result — which is where the worker's final word lands.
func TestConclusionEvidence_PrefersThePark(t *testing.T) {
	task := exhaustedTask()
	if got := conclusionEvidence(task); got != "sdlc: task task-1 produced no changes in /workspace/api" {
		t.Fatalf("failed task evidence: %q", got)
	}
	task.BlockedReason = "  the pipeline is red  "
	if got := conclusionEvidence(task); got != "the pipeline is red" {
		t.Fatalf("parked task evidence: %q", got)
	}
	bare := testTaskRecord(25)
	if got := conclusionEvidence(bare); got != "" {
		t.Fatalf("a task with nothing recorded has nothing to quote: %q", got)
	}
}

// =============================================================================
// The seam
// =============================================================================

// concludedRig is a hooks rig whose issue already carries the in-progress
// label, so a label swap is observable.
func concludedRig(t *testing.T) *hooksRig {
	t.Helper()
	rig := newHooksRig(t)
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Forge.Labels[LabelEscalated] = "factory:escalated"
	}); err != nil {
		t.Fatalf("rebind: %v", err)
	}
	rig.forge.putIssue(25, "Fix the thing", "factory:in-progress", "bug")
	return rig
}

// TestHooks_TaskConcluded_ExhaustedPostsOnceAndSwapsTheLabel is the defect:
// a task that spends its budget must reach the issue exactly as an escalation
// does.
func TestHooks_TaskConcluded_ExhaustedPostsOnceAndSwapsTheLabel(t *testing.T) {
	rig := concludedRig(t)
	task := exhaustedTask()
	rig.tasks.replace(task)

	err := rig.hooks.TaskConcluded(context.Background(), orchestration.TaskConclusion{
		TenantID: testTenant, Reason: orchestration.TaskConcludedBudgetExhausted, Task: task, JobID: "job-3",
	})
	if err != nil {
		t.Fatalf("TaskConcluded: %v", err)
	}

	comments := rig.forge.commentsOn(25)
	if len(comments) != 1 {
		t.Fatalf("expected exactly one comment, got %d", len(comments))
	}
	for _, want := range []string{"### This task has spent its attempt budget", "**Outcome:** `exhausted`",
		"produced no changes", "`job-1`, `job-2`, `job-3`", blockedMarker(task.ID, 3, OutcomeExhausted)} {
		if !strings.Contains(comments[0].Body, want) {
			t.Fatalf("comment lacks %q:\n%s", want, comments[0].Body)
		}
	}

	labels := rig.forge.issueLabels(25)
	if !containsLabel(labels, "factory:escalated") || containsLabel(labels, "factory:in-progress") {
		t.Fatalf("the label was not swapped: %v", labels)
	}

	artifacts := rig.tasks.submitted(task.ID)
	if len(artifacts) != 1 || artifacts[0].Name != conclusionArtifactName {
		t.Fatalf("the forge write was not recorded on the task: %+v", artifacts)
	}
	if artifacts[0].Metadata["outcome"] != "exhausted" || artifacts[0].Ref.Path != task.Source.URL {
		t.Fatalf("the record does not name the write: %+v", artifacts[0])
	}
}

// TestHooks_TaskConcluded_ReadmitClearsTheTerminalLabel proves the undoing of
// a park is announced too, and moves the issue from its park label to the
// trigger — the display state of queued work — until the next attempt's
// Prepare labels it in progress.
func TestHooks_TaskConcluded_ReadmitClearsTheTerminalLabel(t *testing.T) {
	rig := concludedRig(t)
	rig.forge.putIssue(25, "Fix the thing", "factory:escalated", "bug")
	task := exhaustedTask()
	task.Status = orchestration.TaskStatusPending
	rig.tasks.replace(task)

	err := rig.hooks.TaskConcluded(context.Background(), orchestration.TaskConclusion{
		TenantID: testTenant, Reason: orchestration.TaskConcludedReadmitted, Task: task,
	})
	if err != nil {
		t.Fatalf("TaskConcluded: %v", err)
	}
	comments := rig.forge.commentsOn(25)
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "### This task has been readmitted") {
		t.Fatalf("the readmission was not announced: %+v", comments)
	}
	labels := rig.forge.issueLabels(25)
	if containsLabel(labels, "factory:escalated") {
		t.Fatalf("the terminal label was not cleared: %v", labels)
	}
	if !containsLabel(labels, "bug") {
		t.Fatalf("a label the module does not own must be left alone: %v", labels)
	}
	if !containsLabel(labels, "task:ready") {
		t.Fatalf("a queued task's issue must show the trigger: %v", labels)
	}
}

// TestHooks_TaskConcluded_ReadmitKeepsTheTriggerAHandBackApplied is X11
// (go-luks2#211 and five more, 2026-09-25): a Needs-you ruling handed the
// issue back — trigger on, park label off — and the readmission comment
// posted the same second said "`task:ready` removed" and removed it, leaving
// a pending task's issue with no state label at all. The readmission must
// leave the trigger exactly where the hand-back put it.
func TestHooks_TaskConcluded_ReadmitKeepsTheTriggerAHandBackApplied(t *testing.T) {
	rig := concludedRig(t)
	rig.forge.putIssue(25, "Fix the thing", "task:ready", "bug")
	task := exhaustedTask()
	task.Status = orchestration.TaskStatusPending
	rig.tasks.replace(task)

	if err := rig.hooks.TaskConcluded(context.Background(), orchestration.TaskConclusion{
		TenantID: testTenant, Reason: orchestration.TaskConcludedReadmitted, Task: task,
	}); err != nil {
		t.Fatalf("TaskConcluded: %v", err)
	}
	if labels := rig.forge.issueLabels(25); !containsLabel(labels, "task:ready") {
		t.Fatalf("the readmission stripped the trigger the hand-back applied: %v", labels)
	}
	if removed := rig.forge.labelRemovals; len(removed) != 0 {
		t.Fatalf("labels removed = %v, want none: nothing but the trigger was owned", removed)
	}
	comments := rig.forge.commentsOn(25)
	if len(comments) != 1 || strings.Contains(comments[0].Body, "removed") {
		t.Fatalf("the readmission comment still announces a removal: %+v", comments)
	}
}

// TestForgeConfig_LabelNamesTheTriggerByItsKey: the trigger is reachable by
// its label key like every display label, and a binding without one answers
// empty.
func TestForgeConfig_LabelNamesTheTriggerByItsKey(t *testing.T) {
	if got := (ForgeConfig{TriggerLabel: " task:ready "}).Label(TriggerLabelKey); got != "task:ready" {
		t.Fatalf("Label(trigger) = %q", got)
	}
	if got := (ForgeConfig{Labels: map[string]string{LabelBlocked: "factory:blocked"}}).Label(TriggerLabelKey); got != "" {
		t.Fatalf("Label(trigger) = %q, want empty for a binding with no trigger", got)
	}
}

// TestHooks_TaskConcluded_CompletedWritesNothing proves the silent row: the
// Complete moment has already merged and relabelled, and a second comment
// saying so would be the module talking to itself.
func TestHooks_TaskConcluded_CompletedWritesNothing(t *testing.T) {
	rig := concludedRig(t)
	task := testTaskRecord(25)
	task.Status = orchestration.TaskStatusCompleted
	rig.tasks.replace(task)

	err := rig.hooks.TaskConcluded(context.Background(), orchestration.TaskConclusion{
		TenantID: testTenant, Reason: orchestration.TaskConcludedCompleted, Task: task, JobID: "job-1",
	})
	if err != nil {
		t.Fatalf("TaskConcluded: %v", err)
	}
	if comments := rig.forge.commentsOn(25); len(comments) != 0 {
		t.Fatalf("a completed task posts nothing here, got %+v", comments)
	}
	if len(rig.forge.labelAdds) != 0 || len(rig.forge.labelRemovals) != 0 {
		t.Fatalf("a completed task moves no label here: adds=%v removals=%v", rig.forge.labelAdds, rig.forge.labelRemovals)
	}
}

// TestHooks_TaskConcluded_RefusesWhatItCannotRender covers the bad inputs: a
// conclusion that names no task, a reason with no row, and a task this module
// never adopted.
func TestHooks_TaskConcluded_RefusesWhatItCannotRender(t *testing.T) {
	rig := concludedRig(t)
	ctx := context.Background()

	err := rig.hooks.TaskConcluded(ctx, orchestration.TaskConclusion{
		TenantID: testTenant, Reason: orchestration.TaskConcludedBudgetExhausted,
	})
	if !errors.Is(err, orchestration.ErrTaskConclusionInvalid) {
		t.Fatalf("a conclusion with no task must be refused, got %v", err)
	}

	err = rig.hooks.TaskConcluded(ctx, orchestration.TaskConclusion{
		TenantID: testTenant, Reason: orchestration.TaskConclusionReason("invented"), Task: exhaustedTask(),
	})
	if !errors.Is(err, orchestration.ErrTaskConclusionInvalid) {
		t.Fatalf("a reason outside the vocabulary must be refused, got %v", err)
	}

	foreign := exhaustedTask()
	foreign.Source = &orchestration.TaskSourceRef{Kind: orchestration.TaskSourceKind("cron"), Ref: "nightly"}
	if err := rig.hooks.TaskConcluded(ctx, orchestration.TaskConclusion{
		TenantID: testTenant, Reason: orchestration.TaskConcludedBudgetExhausted, Task: foreign,
	}); err != nil {
		t.Fatalf("a task the module never adopted is not its business: %v", err)
	}
	if comments := rig.forge.commentsOn(25); len(comments) != 0 {
		t.Fatalf("a foreign task must reach no issue, got %+v", comments)
	}
}

// TestHooks_TaskConcluded_UnrenderableReasonIsAnError proves that a reason the
// ENGINE names and this table does not is reported rather than ignored — the
// silence go-quicbaas#788 measured, refused at the source.
//
// The row is removed for the duration of the test and put back, because that
// is exactly the state the guard exists for: an engine that grows a reason and
// a module that has not grown its row.
func TestHooks_TaskConcluded_UnrenderableReasonIsAnError(t *testing.T) {
	rig := concludedRig(t)
	task := exhaustedTask()
	rig.tasks.replace(task)

	const orphaned = orchestration.TaskConcludedFaultHeld
	restore := conclusionOutcomes[orphaned]
	delete(conclusionOutcomes, orphaned)
	t.Cleanup(func() { conclusionOutcomes[orphaned] = restore })

	err := rig.hooks.TaskConcluded(context.Background(), orchestration.TaskConclusion{
		TenantID: testTenant, Reason: orphaned, Task: task, JobID: "job-3",
	})
	var unknown *UnknownConclusionError
	if !errors.As(err, &unknown) || !errors.Is(err, ErrUnknownConclusion) {
		t.Fatalf("expected UnknownConclusionError, got %v", err)
	}
	if unknown.Reason != string(orphaned) || unknown.TaskID != task.ID {
		t.Fatalf("the refusal must name what it could not render: %+v", unknown)
	}
	if got := unknown.Error(); !strings.Contains(got, string(orphaned)) {
		t.Fatalf("error text: %q", got)
	}
	if comments := rig.forge.commentsOn(25); len(comments) != 0 {
		t.Fatalf("a conclusion the module cannot render posts nothing, got %+v", comments)
	}
}

// TestHooks_TaskConcluded_APostThatNeverLandsIsReported proves the conclusion
// is not silently swallowed when the forge will not take the comment.
func TestHooks_TaskConcluded_APostThatNeverLandsIsReported(t *testing.T) {
	rig := concludedRig(t)
	task := exhaustedTask()
	rig.tasks.replace(task)
	rig.forge.postFailures = 5

	err := rig.hooks.TaskConcluded(context.Background(), orchestration.TaskConclusion{
		TenantID: testTenant, Reason: orchestration.TaskConcludedBudgetExhausted, Task: task, JobID: "job-3",
	})
	var undelivered *SurfacingUndeliveredError
	if !errors.As(err, &undelivered) {
		t.Fatalf("expected SurfacingUndeliveredError, got %v", err)
	}
	if undelivered.IssueNumber != 25 {
		t.Fatalf("the refusal must name the issue: %+v", undelivered)
	}
	if got := rig.tasks.submitted(task.ID); len(got) != 0 {
		t.Fatalf("a write that did not happen must not be recorded: %+v", got)
	}
}

// TestHooks_RecordOutcome_SkipsATaskWithNoIssueURL proves an artifact that
// would point nowhere is not written, the same rule Hooks.publish follows for
// a carve-out change with no pull request.
func TestHooks_RecordOutcome_SkipsATaskWithNoIssueURL(t *testing.T) {
	rig := concludedRig(t)
	task := exhaustedTask()
	task.Source.URL = ""
	rig.tasks.replace(task)

	if err := rig.hooks.recordOutcome(context.Background(), testTenant, task, OutcomeExhausted, nil); err != nil {
		t.Fatalf("recordOutcome: %v", err)
	}
	if got := rig.tasks.submitted(task.ID); len(got) != 0 {
		t.Fatalf("an artifact pointing nowhere must not be written: %+v", got)
	}

	task.Source.URL = "http://forge.test/acme/api/issues/25"
	if err := rig.hooks.recordOutcome(context.Background(), testTenant, task, OutcomeExhausted, &ForgeComment{ID: 42}); err != nil {
		t.Fatalf("recordOutcome: %v", err)
	}
	got := rig.tasks.submitted(task.ID)
	if len(got) != 1 || got[0].Metadata["comment_id"] != "42" {
		t.Fatalf("the comment must be recorded: %+v", got)
	}
}
