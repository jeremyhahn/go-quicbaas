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
// A turn that changed nothing — go-quicbaas#787
// =============================================================================
//
// The defect these cover: three attempts at go-truststrap#26 ran the same
// prompt, each was charged as a failure of the work, the verification
// commands ran on an untouched tree every time, and no job record held a
// single word the worker said.

// verificationsRun is every verification command the container was asked to
// run. It is the assertion behind "the commands must not run on a tree the
// worker did not touch".
func (r *runnerRig) verificationsRun() []string {
	r.container.mu.Lock()
	defer r.container.mu.Unlock()
	var run []string
	for _, exec := range r.container.execs {
		if len(exec.Cmd) > 0 && exec.Cmd[0] == verificationShell {
			run = append(run, strings.Join(exec.Cmd, " "))
		}
	}
	return run
}

// issueComments is every comment the run posted to the issue.
func (r *runnerRig) issueComments(t *testing.T) []string {
	t.Helper()
	comments, err := r.forge.ListIssueComments(context.Background(), testRepoRef, 25)
	if err != nil {
		t.Fatal(err)
	}
	bodies := make([]string, 0, len(comments))
	for _, comment := range comments {
		bodies = append(bodies, comment.Body)
	}
	return bodies
}

// storedDelivery re-reads the module's own record of the attempt.
func (r *runnerRig) storedDelivery(t *testing.T) *Delivery {
	t.Helper()
	delivery, err := r.store.GetDelivery(context.Background(), testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	return delivery
}

// TestRunner_AQuestionParksTheTaskWithTheWorkersOwnWords: the case the whole
// issue is about. The worker declares a question, changes no file, and the
// attempt ends as a task waiting on a person — not as three identical
// attempts charged against the work.
func TestRunner_AQuestionParksTheTaskWithTheWorkersOwnWords(t *testing.T) {
	rig := newRunnerRig(t)
	rig.agents.answers("Both stores could hold the index. Which one does the owner want?\n" +
		"SDLC-STOP: question")

	job := rig.job(t)
	err := rig.runner.Run(context.Background(), job)

	var escalated *EscalatedError
	if !errors.As(err, &escalated) {
		t.Fatalf("Run = %v, want EscalatedError", err)
	}
	parked := job.Parked()
	if parked == nil {
		t.Fatal("the run did not tell the engine the task is parked, so the next attempt would be dispatched")
	}
	if parked.Kind != orchestration.TaskBlockKindNeedsInfo {
		t.Fatalf("block kind = %q, want needs_info", parked.Kind)
	}
	if !strings.Contains(parked.Message, "Which one does the owner want?") {
		t.Fatalf("the park reason does not carry the worker's words: %q", parked.Message)
	}
	if commands := rig.verificationsRun(); len(commands) != 0 {
		t.Fatalf("verification ran on a tree the worker did not touch: %v", commands)
	}
	if prompts := rig.agents.promptTexts(); len(prompts) != 1 {
		t.Fatalf("prompts = %d, want exactly one", len(prompts))
	}

	turns := rig.auditDetails(auditAgentTurn)
	if len(turns) != 1 {
		t.Fatalf("agent turn events = %d, want 1", len(turns))
	}
	if !strings.Contains(turns[0], "Which one does the owner want?") {
		t.Fatalf("the turn event dropped the reply: %q", turns[0])
	}
	if !strings.Contains(turns[0], "attempt=1") || !strings.Contains(turns[0], "elapsed=") {
		t.Fatalf("the turn event carries no accounting: %q", turns[0])
	}
	// THE VERDICT IS ON THE TURN'S OWN EVENT. A reader must not have to
	// find the skipped-verification event to learn what the factory
	// concluded about the turn it is looking at.
	if !strings.Contains(turns[0], "outcome=question") {
		t.Fatalf("the turn event carries no classification: %q", turns[0])
	}
	skipped := rig.auditDetails(auditVerificationSkipped)
	if len(skipped) != 1 || !strings.Contains(skipped[0], "outcome=question") {
		t.Fatalf("verification-skipped events = %v", skipped)
	}

	var found bool
	for _, body := range rig.issueComments(t) {
		if strings.Contains(body, "Which one does the owner want?") &&
			strings.Contains(body, blockedMarker(testTask, 1, OutcomeNeedsInfo)) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the worker's question never reached the issue: %v", rig.issueComments(t))
	}
	if turn := rig.storedDelivery(t).LastTurn; turn == nil || turn.Outcome != TurnOutcomeQuestion {
		t.Fatalf("the no-change turn was not recorded on the delivery: %+v", turn)
	}
}

// TestRunner_ACitedRuleAndABrokenEnvironmentParkDifferently: the two other
// declared stops reach the issue under their own kinds, so the label a person
// sees says which of them it was.
func TestRunner_ACitedRuleAndABrokenEnvironmentParkDifferently(t *testing.T) {
	for name, tc := range map[string]struct {
		reply string
		kind  orchestration.TaskBlockKind
	}{
		"a cited rule": {
			"CLAUDE.md forbids a second identifier; that is the owner's call.\nSDLC-STOP: owner-decision",
			orchestration.TaskBlockKindNeedsInfo,
		},
		"a broken environment": {
			"the module proxy is unreachable from this container.\nSDLC-STOP: environment",
			orchestration.TaskBlockKindBlocked,
		},
		"a code this binary does not know": {
			"stopped for reasons\nSDLC-STOP: vibes",
			orchestration.TaskBlockKindBlocked,
		},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newRunnerRig(t)
			rig.agents.answers(tc.reply)
			job := rig.job(t)
			if err := rig.runner.Run(context.Background(), job); err == nil {
				t.Fatal("a parked attempt reported success")
			}
			parked := job.Parked()
			if parked == nil || parked.Kind != tc.kind {
				t.Fatalf("parked = %+v, want kind %q", parked, tc.kind)
			}
			if len(rig.verificationsRun()) != 0 {
				t.Fatal("verification ran on an untouched tree")
			}
		})
	}
}

// TestRunner_AnEmptyReplyIsRetriedOnceThenEscalated: a worker that answers
// with nothing may have lost one answer, so it is asked again — with the
// silence stated, so the prompt is not the identical one — and a second
// silence escalates naming it.
func TestRunner_AnEmptyReplyIsRetriedOnceThenEscalated(t *testing.T) {
	rig := newRunnerRig(t)
	job := rig.job(t)
	err := rig.runner.Run(context.Background(), job)
	if err == nil {
		t.Fatal("a silent attempt reported success")
	}

	prompts := rig.agents.promptTexts()
	if len(prompts) != MaxSilentTurns+1 {
		t.Fatalf("prompts = %d, want %d", len(prompts), MaxSilentTurns+1)
	}
	if prompts[0] == prompts[1] {
		t.Fatal("the retry sent the identical prompt, which is what the loop was")
	}
	if !strings.Contains(prompts[1], "produced no reply and no changes") {
		t.Fatalf("the retry did not say what happened: %q", prompts[1])
	}

	parked := job.Parked()
	if parked == nil || parked.Kind != orchestration.TaskBlockKindEscalated {
		t.Fatalf("parked = %+v, want an escalation", parked)
	}
	if !strings.Contains(parked.Message, "no text on every turn") {
		t.Fatalf("the escalation does not name the empty turns: %q", parked.Message)
	}
	if turns := rig.auditDetails(auditAgentTurn); len(turns) != MaxSilentTurns+1 {
		t.Fatalf("agent turn events = %d, want %d", len(turns), MaxSilentTurns+1)
	}
	if len(rig.verificationsRun()) != 0 {
		t.Fatal("verification ran on an untouched tree")
	}
}

// TestRunner_AReplyClaimingWorkOnACleanTreeIsStillAWorkFailure: the one
// no-change turn that remains a failure of the work, and its reply is on the
// error so the task result carries the account rather than only the fact.
func TestRunner_AReplyClaimingWorkOnACleanTreeIsStillAWorkFailure(t *testing.T) {
	rig := newRunnerRig(t)
	rig.agents.answers("Implemented the parser, added the tests, everything passes.")

	job := rig.job(t)
	err := rig.runner.Run(context.Background(), job)

	var none *NoChangesError
	if !errors.As(err, &none) || !errors.Is(err, ErrNoChanges) {
		t.Fatalf("Run = %v, want NoChangesError", err)
	}
	if !strings.Contains(none.Reply, "Implemented the parser") {
		t.Fatalf("the error dropped the reply: %+v", none)
	}
	if !strings.Contains(err.Error(), "Implemented the parser") {
		t.Fatalf("the task result would not carry the reply: %q", err.Error())
	}
	if job.Parked() != nil {
		t.Fatalf("a work failure parked the task: %+v", job.Parked())
	}
	if len(rig.verificationsRun()) != 0 {
		t.Fatal("verification ran on an untouched tree")
	}
	if turn := rig.storedDelivery(t).LastTurn; turn == nil || turn.Outcome != TurnOutcomeClaimedWork {
		t.Fatalf("the no-change turn was not recorded: %+v", turn)
	}
}

// TestRunner_TheNextAttemptIsToldWhatTheLastOneSaid: rule three. A fresh
// attempt renders the same issue contract, so without the previous turn on
// the prompt the second worker is handed the first one's prompt verbatim.
func TestRunner_TheNextAttemptIsToldWhatTheLastOneSaid(t *testing.T) {
	rig := newRunnerRig(t)
	rig.agents.answers("Implemented it all.")
	if err := rig.runner.Run(context.Background(), rig.job(t)); err == nil {
		t.Fatal("the first attempt reported success")
	}
	first := rig.agents.promptTexts()

	second := rig.job(t)
	second.Config.Attempt = 2
	if err := rig.runner.Run(context.Background(), second); err == nil {
		t.Fatal("the second attempt reported success")
	}
	prompts := rig.agents.promptTexts()
	if len(prompts) != 2 {
		t.Fatalf("prompts = %d, want one per attempt", len(prompts))
	}
	if prompts[1] == first[0] {
		t.Fatal("the second attempt was handed the identical prompt")
	}
	if !strings.Contains(prompts[1], "The previous attempt at this task changed nothing") {
		t.Fatalf("the second prompt does not carry the last turn: %q", prompts[1])
	}
	if !strings.Contains(prompts[1], "Implemented it all.") {
		t.Fatalf("the second prompt does not quote the last turn: %q", prompts[1])
	}
}

// TestRunner_AnIdenticalPromptAfterANoChangeTurnIsRefused: the guard behind
// rule three. If the note that makes the prompt different is ever lost, the
// run parks with a stated reason rather than spending the attempt.
func TestRunner_AnIdenticalPromptAfterANoChangeTurnIsRefused(t *testing.T) {
	rig := newRunnerRig(t)
	run := newRunnerRun(t, rig)
	ctx := context.Background()

	// A previous attempt that answered this exact prompt — the one this run
	// is about to compose, note and all — and changed nothing.
	run.delivery.LastTurn = &AgentTurn{Attempt: 1, Reply: "did it", Outcome: TurnOutcomeClaimedWork}
	prompt, err := run.turnPrompt()
	if err != nil {
		t.Fatal(err)
	}
	run.delivery.LastTurn.PromptDigest = promptDigest(prompt)

	state, err := run.execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if state != orchestration.JobStateEscalating {
		t.Fatalf("state = %s, want escalating", state)
	}
	if len(rig.agents.promptTexts()) != 0 {
		t.Fatal("the identical prompt was dispatched anyway")
	}
	if !strings.Contains(run.job.EscalationReason(), "this exact prompt") {
		t.Fatalf("reason = %q", run.job.EscalationReason())
	}
}

// TestRunner_AProductiveTurnClearsTheNoChangeRecord: a turn that reached the
// checkout is not something to warn the next worker about, and the run goes
// on to verification as it always did.
func TestRunner_AProductiveTurnClearsTheNoChangeRecord(t *testing.T) {
	rig := newRunnerRig(t)
	run := newRunnerRun(t, rig)
	ctx := context.Background()
	if err := run.runner.hooks.updateDelivery(ctx, run.job.Context(), run.delivery, func(d *Delivery) {
		d.LastTurn = &AgentTurn{Attempt: 1, Outcome: TurnOutcomeClaimedWork, Reply: "did it"}
	}); err != nil {
		t.Fatal(err)
	}
	if err := run.loadDelivery(ctx); err != nil {
		t.Fatal(err)
	}
	rig.workerWrote()
	rig.agents.answers("Added the parser and its tests.")

	state, err := run.execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if state != orchestration.JobStateValidating {
		t.Fatalf("state = %s, want validating", state)
	}
	if turn := rig.storedDelivery(t).LastTurn; turn != nil {
		t.Fatalf("a productive turn left the no-change record in place: %+v", turn)
	}
	turns := rig.auditDetails(auditAgentTurn)
	if len(turns) != 1 || !strings.Contains(turns[0], "Added the parser and its tests.") {
		t.Fatalf("the productive turn was not recorded: %v", turns)
	}
	if skipped := rig.auditDetails(auditVerificationSkipped); len(skipped) != 0 {
		t.Fatalf("a productive turn claimed verification was skipped: %v", skipped)
	}
}

// TestRunner_ATurnThatFailedIsStillRecorded: the prompt itself can fail, and
// the account of the attempt must not go with it.
func TestRunner_ATurnThatFailedIsStillRecorded(t *testing.T) {
	rig := newRunnerRig(t)
	run := newRunnerRun(t, rig)
	rig.agents.promptErr = errors.New("the backend went away")

	if _, err := run.execute(context.Background()); err == nil {
		t.Fatal("a failed prompt reported success")
	}
	turns := rig.auditDetails(auditAgentTurn)
	if len(turns) != 1 {
		t.Fatalf("agent turn events = %d, want the failed turn recorded", len(turns))
	}
	if !strings.Contains(turns[0], "the worker answered with no text") {
		t.Fatalf("turn event = %q", turns[0])
	}
}

// TestRunnerRun_NoChangeTurnRefusesAnActionItCannotPerform: the fail-closed
// half of the dispatch table. An action the runner does not implement stops
// the run rather than sending an unchanged tree to verification.
func TestRunnerRun_NoChangeTurnRefusesAnActionItCannotPerform(t *testing.T) {
	rig := newRunnerRig(t)
	run := newRunnerRun(t, rig)
	original := turnVerdicts[TurnOutcomeClaimedWork]
	turnVerdicts[TurnOutcomeClaimedWork] = TurnVerdict{Outcome: TurnOutcomeClaimedWork, Action: "invent"}
	t.Cleanup(func() { turnVerdicts[TurnOutcomeClaimedWork] = original })

	turn := NewAgentTurn("did it")
	_, err := run.noChangeTurn(context.Background(), turn, ClassifyTurn(turn))
	var unknown *UnknownTurnActionError
	if !errors.As(err, &unknown) || !errors.Is(err, ErrRunner) {
		t.Fatalf("err = %v, want UnknownTurnActionError", err)
	}
	if unknown.Action != "invent" || unknown.Outcome != string(TurnOutcomeClaimedWork) {
		t.Fatalf("error = %+v", unknown)
	}
}

// TestRunnerRun_SplitParkRecordReadsTheKindAndSurvivesAMalformedOne: an
// interrupted park is restored from the audit trail, and a header nothing
// recognises must lose the KIND rather than the reason.
func TestRunnerRun_SplitParkRecordReadsTheKindAndSurvivesAMalformedOne(t *testing.T) {
	judgement, reason := splitParkRecord("needs_info\nWhich store?")
	if judgement.kind != orchestration.TaskBlockKindNeedsInfo || reason != "Which store?" {
		t.Fatalf("judgement = %+v reason = %q", judgement, reason)
	}
	judgement, reason = splitParkRecord("not-a-kind\nWhich store?")
	if judgement.kind != orchestration.TaskBlockKindEscalated || reason != "not-a-kind\nWhich store?" {
		t.Fatalf("judgement = %+v reason = %q", judgement, reason)
	}
	judgement, reason = splitParkRecord("no newline at all")
	if judgement.kind != orchestration.TaskBlockKindEscalated || reason != "no newline at all" {
		t.Fatalf("judgement = %+v reason = %q", judgement, reason)
	}
}

// TestRunnerRun_AResumedParkKeepsItsKind: the run that FINISHES an
// interrupted park is a different one, and a kind lost on the way would post
// an escalation over the worker's question.
func TestRunnerRun_AResumedParkKeepsItsKind(t *testing.T) {
	rig := newRunnerRig(t)
	interrupted := rig.interruptedStatus(orchestration.JobStateEscalating,
		orchestration.JobAuditEvent{Type: auditParked, Details: "needs_info\nWhich store should hold the index?"})
	job := rig.resumedJob(t, interrupted)

	if err := rig.runner.Run(context.Background(), job); err == nil {
		t.Fatal("a resumed escalation reported success")
	}
	parked := job.Parked()
	if parked == nil || parked.Kind != orchestration.TaskBlockKindNeedsInfo {
		t.Fatalf("parked = %+v, want needs_info", parked)
	}
	if !strings.Contains(parked.Message, "Which store should hold the index?") {
		t.Fatalf("the resumed park lost its reason: %q", parked.Message)
	}
	var found bool
	for _, body := range rig.issueComments(t) {
		if strings.Contains(body, blockedMarker(testTask, 1, OutcomeNeedsInfo)) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the resumed park posted under the wrong kind: %v", rig.issueComments(t))
	}
}

// TestRunnerRun_AResumedEscalationStillRestoresTheOldRecord: a job record
// written before workers could state their own reason carries only
// auditEscalated, and it must still restore.
func TestRunnerRun_AResumedEscalationStillRestoresTheOldRecord(t *testing.T) {
	rig := newRunnerRig(t)
	interrupted := rig.interruptedStatus(orchestration.JobStateEscalating,
		orchestration.JobAuditEvent{Type: auditEscalated, Details: "review did not converge"})
	job := rig.resumedJob(t, interrupted)

	if err := rig.runner.Run(context.Background(), job); err == nil {
		t.Fatal("a resumed escalation reported success")
	}
	parked := job.Parked()
	if parked == nil || parked.Kind != orchestration.TaskBlockKindEscalated {
		t.Fatalf("parked = %+v, want an escalation", parked)
	}
	if parked.Message != "review did not converge" {
		t.Fatalf("reason = %q", parked.Message)
	}
}

// TestConclusionOutcome_ARunnerParkIsAlreadyOnTheIssue: the conclusion seam
// must write nothing for a park the run decided, because the run's Blocked
// moment already wrote it — under the marker renderOutcome would use, so a
// second write would be two comments claiming to be one.
func TestConclusionOutcome_ARunnerParkIsAlreadyOnTheIssue(t *testing.T) {
	kind, known := OutcomeOfConclusion(orchestration.TaskConcludedRunnerParked)
	if !known {
		t.Fatal("runner_parked has no row in the outcome table")
	}
	if kind != OutcomeSilent {
		t.Fatalf("runner_parked renders %q; a park the run already posted must be silent", kind)
	}
	// The operator's own park is NOT silent: nothing has announced it.
	for _, reason := range []orchestration.TaskConclusionReason{
		orchestration.TaskConcludedParkedNeedsInfo,
		orchestration.TaskConcludedParkedBlocked,
		orchestration.TaskConcludedParkedEscalated,
	} {
		if outcome, ok := OutcomeOfConclusion(reason); !ok || outcome == OutcomeSilent {
			t.Fatalf("%q renders %q; an explicit BlockTask has no other way to reach the issue", reason, outcome)
		}
	}
}

// TestRunner_AParkPostsExactlyOneComment: the end-to-end half. A classified
// park posts once, and the conclusion the engine is handed for it is the one
// that adds nothing on top.
func TestRunner_AParkPostsExactlyOneComment(t *testing.T) {
	rig := newRunnerRig(t)
	rig.agents.answers("Which store should hold the index?\nSDLC-STOP: question")
	job := rig.job(t)
	if err := rig.runner.Run(context.Background(), job); err == nil {
		t.Fatal("a parked attempt reported success")
	}

	marker := blockedMarker(testTask, 1, OutcomeNeedsInfo)
	posted := 0
	for _, body := range rig.issueComments(t) {
		if strings.Contains(body, marker) {
			posted++
		}
	}
	if posted != 1 {
		t.Fatalf("the park posted %d comments carrying %q, want exactly one", posted, marker)
	}
	if kind, _ := OutcomeOfConclusion(orchestration.TaskConcludedRunnerParked); kind != OutcomeSilent {
		t.Fatalf("the conclusion seam would post a second comment as %q", kind)
	}
}

// TestRunner_AParkedRunAlsoFailsTheJob is half of the reachability proof the
// park branch needs, and it is not implied by the applyConclusion tests:
// those hand the conclusion a `JobStatus{State: JobStateFailed}`, while the
// engine DERIVES that state from what the run returned.
//
// applyConclusion answers `completed` and returns before the park branch for
// any state but failed, so a run that parked and then returned nil would have
// its judgement silently dropped and the task requeued — go-quicbaas#787's
// defect reintroduced one layer down. This end asserts the two things the
// envelope needs: a non-nil terminal error, and the park beside it. The other
// end — that a non-nil runner error becomes JobStateFailed — is
// TestJobRun_ARunnerErrorEndsTheJobFailed, in the package that owns it.
func TestRunner_AParkedRunAlsoFailsTheJob(t *testing.T) {
	rig := newRunnerRig(t)
	rig.agents.answers("Which store should hold the index?\nSDLC-STOP: question")
	job := rig.job(t)

	runErr := rig.runner.Run(context.Background(), job)
	if runErr == nil {
		t.Fatal("a parked run returned nil; the envelope would conclude the task completed and drop the park")
	}
	var escalated *EscalatedError
	if !errors.As(runErr, &escalated) {
		t.Fatalf("err = %v, want EscalatedError", runErr)
	}
	parked := job.Parked()
	if parked == nil || parked.Kind != orchestration.TaskBlockKindNeedsInfo {
		t.Fatalf("parked = %+v, want needs_info alongside the terminal error", parked)
	}
}

// TestRunner_TheParkIsDurableBeforeEscalatingIsPersisted: the earlier crash
// window, from the runner's side. JobRun.SetState persists the state before
// the handler for it runs, so the park has to be on the record by the time
// `escalating` is written — not recorded inside the escalating handler,
// where a crash in between left the reaper a record it reads as a death.
func TestRunner_TheParkIsDurableBeforeEscalatingIsPersisted(t *testing.T) {
	rig := newRunnerRig(t)
	rig.agents.answers("Which store should hold the index?\nSDLC-STOP: question")
	job := rig.job(t)

	if err := rig.runner.Run(context.Background(), job); err == nil {
		t.Fatal("a parked attempt reported success")
	}

	var escalating *orchestration.JobStatus
	for _, status := range rig.snapshotsIn(orchestration.JobStateEscalating) {
		snapshot := status
		escalating = &snapshot
		break
	}
	if escalating == nil {
		t.Fatal("the run never persisted an escalating status")
	}
	if escalating.Park == nil {
		t.Fatal("the durable escalating status carries no park; a crash in this window requeues a parked task")
	}
	if escalating.Park.Kind != orchestration.TaskBlockKindNeedsInfo {
		t.Fatalf("persisted park = %+v", escalating.Park)
	}
	if escalating.Park.Surfaced {
		t.Fatal("the park was marked announced before anything had posted it")
	}
}

// TestRunner_ASurfacedParkPostsOnceAndConcludesSilent: the success half. The
// run posts, and the conclusion adds nothing on top.
func TestRunner_ASurfacedParkPostsOnceAndConcludesSilent(t *testing.T) {
	rig := newRunnerRig(t)
	rig.agents.answers("Which store should hold the index?\nSDLC-STOP: question")
	job := rig.job(t)

	if err := rig.runner.Run(context.Background(), job); err == nil {
		t.Fatal("a parked attempt reported success")
	}
	parked := job.Parked()
	if parked == nil || !parked.Surfaced {
		t.Fatalf("parked = %+v, want a park recorded as announced", parked)
	}

	marker := blockedMarker(testTask, 1, OutcomeNeedsInfo)
	posted := 0
	for _, body := range rig.issueComments(t) {
		if strings.Contains(body, marker) {
			posted++
		}
	}
	if posted != 1 {
		t.Fatalf("the run posted %d comments, want exactly one", posted)
	}
	if kind, _ := OutcomeOfConclusion(orchestration.TaskConcludedRunnerParked); kind != OutcomeSilent {
		t.Fatalf("the conclusion would post a second comment as %q", kind)
	}
}

// TestRunner_AParkThatCouldNotBePostedIsNotSilent is the hole this closes: a
// task blocked with NOTHING on its issue, in the one case where the runner's
// own surfacing is what broke.
//
// The run parks, the forge refuses every retry, the run ends on that error —
// and because it never announced the park, the conclusion must be one the
// outcome table POSTS rather than the silent one.
func TestRunner_AParkThatCouldNotBePostedIsNotSilent(t *testing.T) {
	rig := newRunnerRig(t)
	rig.agents.answers("Which store should hold the index?\nSDLC-STOP: question")
	rig.forge.commentErr = errors.New("the forge refused the comment")
	rig.forge.postFailures = 99
	job := rig.job(t)

	err := rig.runner.Run(context.Background(), job)
	if err == nil {
		t.Fatal("a park nobody could read reported success")
	}
	parked := job.Parked()
	if parked == nil {
		t.Fatal("the run lost its park when the surfacing failed")
	}
	if parked.Surfaced {
		t.Fatal("the park claims it was announced, but every post was refused")
	}

	// Nothing reached the issue, which is exactly why the conclusion must
	// not be the silent one.
	for _, body := range rig.issueComments(t) {
		if strings.Contains(body, blockedMarker(testTask, 1, OutcomeNeedsInfo)) {
			t.Fatal("a comment landed despite the forge refusing every post")
		}
	}
	kind, known := OutcomeOfConclusion(orchestration.TaskConcludedParkedNeedsInfo)
	if !known || kind == OutcomeSilent {
		t.Fatalf("an unsurfaced park renders %q; the issue would say nothing at all", kind)
	}
	if kind != OutcomeNeedsInfo {
		t.Fatalf("outcome = %q, want needs_info", kind)
	}
}

// TestHooks_TaskConcludedPostsAnUnsurfacedPark drives the other end: given
// the conclusion an unsurfaced park produces, the observer actually writes
// the comment the run could not.
func TestHooks_TaskConcludedPostsAnUnsurfacedPark(t *testing.T) {
	rig := newRunnerRig(t)
	task := rig.tasks.tasks[testTask]
	task.Status = orchestration.TaskStatusBlocked
	task.BlockKind = orchestration.TaskBlockKindNeedsInfo
	task.BlockedReason = "Which store should hold the index?"
	task.Attempts = 1

	err := rig.hooks.TaskConcluded(context.Background(), orchestration.TaskConclusion{
		TenantID: testTenant,
		Reason:   orchestration.TaskConcludedParkedNeedsInfo,
		Task:     task,
		JobID:    "job-1",
	})
	if err != nil {
		t.Fatalf("TaskConcluded: %v", err)
	}

	var found bool
	for _, body := range rig.issueComments(t) {
		if strings.Contains(body, "Which store should hold the index?") &&
			strings.Contains(body, blockedMarker(testTask, 1, OutcomeNeedsInfo)) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the conclusion wrote nothing for a park the run never posted: %v", rig.issueComments(t))
	}
}

// TestRunnerRun_TheExhaustedReviewParksDurably is the ending that actually
// happens in production: three review rounds, no convergence, hand it to a
// person. It used to write its own audit event and Escalate reason and then
// return the state — so the durable `escalating` record carried no park, and
// a crash before the escalating handler ran let the reaper account an
// exhausted review as an undiagnosed death and requeue it.
//
// The park must be on the run BEFORE the state is returned, because
// SetState persists the state before the handler for it runs.
func TestRunnerRun_TheExhaustedReviewParksDurably(t *testing.T) {
	rig := newRunnerRig(t)
	run := newRunnerRun(t, rig)
	ctx := context.Background()

	max := run.attempt.binding.MaxReviewRounds
	if max <= 0 {
		max = DefaultMaxReviewRounds
	}
	var state orchestration.JobState
	for i := 0; i <= max; i++ {
		next, err := run.nextRound(ctx, "changes requested")
		if err != nil {
			t.Fatalf("nextRound: %v", err)
		}
		state = next
	}
	if state != orchestration.JobStateEscalating {
		t.Fatalf("state = %s, want escalating after %d rounds", state, max)
	}

	parked := run.job.Parked()
	if parked == nil {
		t.Fatal("the exhausted review returned escalating with no park recorded; a crash here requeues it")
	}
	if parked.Kind != orchestration.TaskBlockKindEscalated {
		t.Fatalf("kind = %q, want escalated", parked.Kind)
	}
	if parked.Surfaced {
		t.Fatal("the park is marked announced before anything posted it")
	}
	if !strings.Contains(parked.Message, "review round(s); this change needs a person") {
		t.Fatalf("reason = %q", parked.Message)
	}
	// It restores as an escalation across an interruption, through the one
	// park vocabulary rather than the legacy event.
	details := rig.auditDetails(auditParked)
	if len(details) == 0 {
		t.Fatal("the exhausted review recorded no durable park event")
	}
	judgement, reason := splitParkRecord(details[len(details)-1])
	if judgement.kind != orchestration.TaskBlockKindEscalated || !strings.Contains(reason, "needs a person") {
		t.Fatalf("audit record = %+v / %q", judgement, reason)
	}
	// AND IT IS CHARGED. Exhausting the review rounds is a judgement about the
	// work, so the park carries no operator attribution.
	if judgement.faultClass != "" {
		t.Fatalf("an exhausted review was attributed to the factory: %q", judgement.faultClass)
	}
}

// TestRunner_AnExhaustedReviewPersistsItsParkOnEscalating reads the DURABLE
// record for that ending — what the reaper would actually find.
//
// SetState is what persists a state, and it runs BEFORE the handler for it,
// so this is the exact record a crash in that window leaves behind.
func TestRunner_AnExhaustedReviewPersistsItsParkOnEscalating(t *testing.T) {
	rig := newRunnerRig(t)
	run := newRunnerRun(t, rig)
	ctx := context.Background()

	max := run.attempt.binding.MaxReviewRounds
	if max <= 0 {
		max = DefaultMaxReviewRounds
	}
	var state orchestration.JobState
	for i := 0; i <= max; i++ {
		next, err := run.nextRound(ctx, "changes requested")
		if err != nil {
			t.Fatalf("nextRound: %v", err)
		}
		state = next
	}
	if err := run.job.SetState(ctx, state); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	persisted := rig.snapshotsIn(orchestration.JobStateEscalating)
	if len(persisted) == 0 {
		t.Fatal("no escalating status was persisted")
	}
	for _, status := range persisted {
		if status.Park == nil {
			t.Fatal("a durable escalating status carries no park; the reaper reads this as a death")
		}
		if status.Park.Kind != orchestration.TaskBlockKindEscalated {
			t.Fatalf("persisted park kind = %q, want escalated", status.Park.Kind)
		}
		if status.Park.Surfaced {
			t.Fatal("the persisted park claims it was announced before anything posted it")
		}
	}
}

// TestRunner_TheTurnEventCarriesItsOwnOutcome: every classification a
// no-change turn can get must appear on the turn's OWN event, because that
// event is the operator's account of the turn and a reader should not have
// to correlate two events to learn what the factory decided.
func TestRunner_TheTurnEventCarriesItsOwnOutcome(t *testing.T) {
	for name, tc := range map[string]struct {
		reply string
		want  TurnOutcome
	}{
		"a question":      {"Which store?\nSDLC-STOP: question", TurnOutcomeQuestion},
		"an owner ruling": {"CLAUDE.md forbids it.\nSDLC-STOP: owner-decision", TurnOutcomeOwnerRuling},
		"the environment": {"no module proxy\nSDLC-STOP: environment", TurnOutcomeEnvironment},
		"an unknown code": {"stopped\nSDLC-STOP: vibes", TurnOutcomeUnreadable},
		"work never done": {"Implemented it all.", TurnOutcomeClaimedWork},
		"nothing at all":  {"", TurnOutcomeSilent},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newRunnerRig(t)
			if tc.reply != "" {
				rig.agents.answers(tc.reply)
			}
			_ = rig.runner.Run(context.Background(), rig.job(t))

			turns := rig.auditDetails(auditAgentTurn)
			if len(turns) == 0 {
				t.Fatal("no turn event was recorded")
			}
			if !strings.Contains(turns[0], "outcome="+string(tc.want)) {
				t.Fatalf("turn event = %q, want outcome=%s", turns[0], tc.want)
			}
		})
	}
}

// TestRunner_AFailedTurnEventCarriesNoOutcome is the other half: a turn that
// never got as far as being classified must not claim a verdict. The account
// still has to exist — that is why it is audited immediately — but an
// outcome on it would be an invention.
func TestRunner_AFailedTurnEventCarriesNoOutcome(t *testing.T) {
	rig := newRunnerRig(t)
	run := newRunnerRun(t, rig)
	rig.agents.promptErr = errors.New("the backend went away")

	if _, err := run.execute(context.Background()); err == nil {
		t.Fatal("a failed prompt reported success")
	}
	turns := rig.auditDetails(auditAgentTurn)
	if len(turns) != 1 {
		t.Fatalf("turn events = %d, want the failed turn recorded", len(turns))
	}
	if strings.Contains(turns[0], "outcome=") {
		t.Fatalf("a turn that was never classified claims one: %q", turns[0])
	}
}

// TestRunner_ADeclarationPastTheCapStillParks is the end-to-end of the same
// defect: a long, careful reply that ends with a stop declaration must park
// the task, not be charged as a worker claiming work it never did.
func TestRunner_ADeclarationPastTheCapStillParks(t *testing.T) {
	rig := newRunnerRig(t)
	long := strings.Repeat("I examined the store options and their trade-offs. ", 200)
	if len(long) <= TurnReplyLimit {
		t.Fatalf("the fixture must exceed the cap, got %d bytes", len(long))
	}
	rig.agents.answers(long + "\nWhich store should hold the index?\nSDLC-STOP: question")

	job := rig.job(t)
	err := rig.runner.Run(context.Background(), job)

	var none *NoChangesError
	if errors.As(err, &none) {
		t.Fatal("a declared stop past the cap was charged as a work failure")
	}
	parked := job.Parked()
	if parked == nil || parked.Kind != orchestration.TaskBlockKindNeedsInfo {
		t.Fatalf("parked = %+v, want needs_info", parked)
	}

	turns := rig.auditDetails(auditAgentTurn)
	if len(turns) != 1 || !strings.Contains(turns[0], "outcome=question") {
		t.Fatalf("turn event = %v", turns)
	}
	// And what was written down is still bounded.
	if len(turns[0]) > TurnReplyLimit+len(turnReplyEllipsis)+512 {
		t.Fatalf("the event carried %d bytes; the cap was not applied on write", len(turns[0]))
	}
	stored := rig.storedDelivery(t).LastTurn
	if stored == nil || len(stored.Reply) > TurnReplyLimit+len(turnReplyEllipsis) {
		t.Fatalf("the stored turn is not capped: %+v", stored)
	}
	if !stored.Truncated() || stored.Outcome != TurnOutcomeQuestion {
		t.Fatalf("the stored turn lost its shape: %+v", stored)
	}
}

// TestRunner_AResumedParkWhoseRelabelFailsPostsNoDuplicate: the late-failure
// window. A resumed escalation finds its comment already on the issue, so
// nothing needs posting — but the label move can still fail, and the run
// then ends on that error before it can mark the park surfaced.
//
// If re-arming the restored park dropped the Surfaced it came back with, the
// terminal record would say the park was never announced and the conclusion
// would post it a second time.
func TestRunner_AResumedParkWhoseRelabelFailsPostsNoDuplicate(t *testing.T) {
	rig := newRunnerRig(t)
	ctx := context.Background()

	// The comment this attempt's park already put on the issue.
	marker := blockedMarker(testTask, 1, OutcomeNeedsInfo)
	if _, err := rig.forge.CommentOnIssue(ctx, testRepoRef, 25,
		"### The agent has a question\n\n> Which store?\n\n<!-- "+marker+" -->\n"); err != nil {
		t.Fatal(err)
	}
	// The label move is what fails this time.
	rig.forge.addLabelErr = errors.New("the forge refused the label")

	interrupted := rig.interruptedStatus(orchestration.JobStateEscalating,
		orchestration.JobAuditEvent{Type: auditParked, Details: "needs_info\nWhich store?"})
	interrupted.Park = &orchestration.BlockReason{
		Kind: orchestration.TaskBlockKindNeedsInfo, Message: "Which store?", Surfaced: true,
	}
	job := rig.resumedJob(t, interrupted)

	_ = rig.runner.Run(ctx, job)

	parked := job.Parked()
	if parked == nil {
		t.Fatal("the resumed run lost its park")
	}
	if !parked.Surfaced {
		t.Fatal("re-arming the restored park dropped the fact that it was already announced")
	}
	posted := 0
	for _, body := range rig.issueComments(t) {
		if strings.Contains(body, marker) {
			posted++
		}
	}
	if posted != 1 {
		t.Fatalf("the issue carries %d copies of the park, want the one already there", posted)
	}
}

// =============================================================================
// The turn that is not the first one
// =============================================================================
//
// Round one commits and pushes, so from round two onward the checkout is
// PERMANENTLY ahead of its base. A classification that read the branch's
// position instead of the turn's own would call every later turn productive,
// and the defect above would come straight back for the commonest shape of
// all: a task whose pull request already exists and a follow-up worker that
// cannot proceed.

// TestRunner_AFollowUpRoundThatChangedNothingParksOnTheWorkersStop: the
// worker on the SECOND round declares a question and touches nothing. The
// attempt must park on that declaration — not drop it, re-verify the stale
// head it never touched, and send the same commits back to the reviewer.
func TestRunner_AFollowUpRoundThatChangedNothingParksOnTheWorkersStop(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("REQUEST_CHANGES", "keep the fix inside pkg/thing")
	rig.agents.replyFn = func(turn int) *orchestration.PromptSessionResponse {
		if turn == 1 {
			return workerSaid("Added the parser and its tests.")
		}
		return workerSaid("The finding asks for the opposite of what the issue asks for. " +
			"Which does the owner want?\nSDLC-STOP: question")
	}

	job := rig.job(t)
	err := rig.runner.Run(context.Background(), job)

	var escalated *EscalatedError
	if !errors.As(err, &escalated) {
		t.Fatalf("Run = %v, want EscalatedError", err)
	}
	parked := job.Parked()
	if parked == nil {
		t.Fatal("the second round's stop was dropped; the engine was never told the task is parked")
	}
	if parked.Kind != orchestration.TaskBlockKindNeedsInfo {
		t.Fatalf("block kind = %q, want needs_info — the park is the worker's question, not a spent review budget", parked.Kind)
	}
	if !strings.Contains(parked.Message, "Which does the owner want?") {
		t.Fatalf("the park reason does not carry the second round's words: %q", parked.Message)
	}
	if prompts := rig.agents.promptTexts(); len(prompts) != 2 {
		t.Fatalf("prompts = %d, want the first turn and one review round", len(prompts))
	}
	// The stale head was not re-verified. Round one's commands ran on the
	// work it actually did; round two changed nothing, so there was nothing
	// to build, test or review.
	if commands := rig.verificationsRun(); len(commands) != 1 {
		t.Fatalf("verification commands run = %v, want only round one's", commands)
	}
	skipped := rig.auditDetails(auditVerificationSkipped)
	if len(skipped) != 1 || !strings.Contains(skipped[0], "outcome=question") {
		t.Fatalf("verification-skipped events = %v, want one naming the round's classification", skipped)
	}
	if turn := rig.storedDelivery(t).LastTurn; turn == nil || turn.Outcome != TurnOutcomeQuestion {
		t.Fatalf("the second round's no-change turn was not recorded on the delivery: %+v", turn)
	}
}

// TestRunnerRun_ABranchAheadOfItsBaseIsNotEvidenceThisTurnTouchedAnything
// pins the property directly, in all three of its states: the branch's
// position answers nothing about one turn, a head that moved says the turn
// committed, and uncommitted work says it wrote.
func TestRunnerRun_ABranchAheadOfItsBaseIsNotEvidenceThisTurnTouchedAnything(t *testing.T) {
	rig := newRunnerRig(t)
	run := newRunnerRun(t, rig)
	ctx := context.Background()
	// What a second turn starts on: commits the base does not have, a clean
	// tree, and a head that has not moved since the last one published.
	rig.container.ahead = true
	rig.container.head = "head-1"

	touched, err := run.workerTouchedTree(ctx, "head-1")
	if err != nil {
		t.Fatalf("workerTouchedTree: %v", err)
	}
	if touched {
		t.Fatal("a branch ahead of its base was read as proof that THIS turn changed something")
	}

	rig.container.head = "head-2"
	if touched, err = run.workerTouchedTree(ctx, "head-1"); err != nil || !touched {
		t.Fatalf("a turn that committed its own work read as untouched: touched = %v, err = %v", touched, err)
	}

	rig.container.head = "head-1"
	rig.workerWrote()
	if touched, err = run.workerTouchedTree(ctx, "head-1"); err != nil || !touched {
		t.Fatalf("uncommitted work read as untouched: touched = %v, err = %v", touched, err)
	}
}

// TestRunnerRun_ATurnWhoseBaselineCannotBeReadIsRefusedBeforeThePrompt: there
// is no honest classification without a baseline, so a checkout whose HEAD
// the runner cannot read stops the turn BEFORE it is dispatched — the worker
// is never prompted and nothing is charged for a turn that did not happen.
func TestRunnerRun_ATurnWhoseBaselineCannotBeReadIsRefusedBeforeThePrompt(t *testing.T) {
	rig := newRunnerRig(t)
	run := newRunnerRun(t, rig)
	rig.container.headErr = errors.New("fatal: not a git repository")

	_, err := run.execute(context.Background())
	var refused *PublishCommandError
	if !errors.As(err, &refused) || !errors.Is(err, ErrRunner) {
		t.Fatalf("err = %v, want PublishCommandError", err)
	}
	if prompts := rig.agents.promptTexts(); len(prompts) != 0 {
		t.Fatalf("the worker was prompted on a checkout the runner could not read: %v", prompts)
	}
	if turns := rig.auditDetails(auditAgentTurn); len(turns) != 0 {
		t.Fatalf("a turn that never happened was recorded: %v", turns)
	}
}

// TestRunnerRun_AHeadUnreadableAfterTheTurnStillRecordsWhatTheWorkerSaid is
// the other half: the baseline was read, the worker answered, and the
// checkout then could not be read. The turn is unclassifiable — but what the
// worker said is the only account of it and must survive.
func TestRunnerRun_AHeadUnreadableAfterTheTurnStillRecordsWhatTheWorkerSaid(t *testing.T) {
	rig := newRunnerRig(t)
	run := newRunnerRun(t, rig)
	rig.agents.answers("I read the issue and looked at the tree.")
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		rig.container.mu.Lock()
		defer rig.container.mu.Unlock()
		rig.container.headErr = errors.New("fatal: bad object HEAD")
	}

	_, err := run.execute(context.Background())
	var refused *PublishCommandError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want PublishCommandError", err)
	}
	turns := rig.auditDetails(auditAgentTurn)
	if len(turns) != 1 || !strings.Contains(turns[0], "I read the issue and looked at the tree.") {
		t.Fatalf("the turn was lost with the tree that could not be read: %v", turns)
	}
	if strings.Contains(turns[0], "outcome=") {
		t.Fatalf("a turn nothing could classify carries an outcome: %q", turns[0])
	}
}
