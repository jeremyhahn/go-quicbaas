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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// A reply that never read the prompt — go-quicbaas#62
// =============================================================================
//
// The defect these cover, measured on the burn-in fleet at 2026-09-22 01:14:
// Codex requested changes on PR #970, the round prompt was sent to the
// worker's persistent session, and the session answered a background-task
// notification it had queued two turns earlier instead — 451ms, one text
// block, no tool use, nothing changed. The runner took the "claimed work on
// an unchanged tree" path: the job failed, the attempt was charged, and the
// task lost its pull-request adoption state and a review round to a reply
// that never addressed the prompt.
//
// The turn's own measurements are the evidence, and the sdlc:agent_turn event
// already carried both of them.

// missedPrompt is what the session said when it answered its own queued
// notification instead of the round, shortened from the daemon log.
const missedPrompt = "The expired monitor was watching the first-round sweep logs (/tmp/race.log), " +
	"which the earlier pass already consumed."

// nonAnswerOn scripts the worker to answer in 451ms — the measured
// non-answer — on the given turns, and to work for defaultTurnElapsed on
// every other one.
func nonAnswerOn(agents *fakeAgents, turns ...int) {
	fast := make(map[int]bool, len(turns))
	for _, turn := range turns {
		fast[turn] = true
	}
	agents.mu.Lock()
	defer agents.mu.Unlock()
	agents.turnElapsed = func(turn int) time.Duration {
		if fast[turn] {
			return 451 * time.Millisecond
		}
		return defaultTurnElapsed
	}
}

// TestRunner_ANonAnswerAfterAReviewRoundIsRepromptedOnce is the defect
// end to end: the round's prompt is sent again, once, with one line saying
// the last reply did not address it — and the real answer that follows is an
// ordinary productive turn. The round is not spent and no attempt is charged.
func TestRunner_ANonAnswerAfterAReviewRoundIsRepromptedOnce(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("REQUEST_CHANGES", "keep the fix inside pkg/thing")
	nonAnswerOn(rig.agents, 2)
	rig.agents.replyFn = func(turn int) *orchestration.PromptSessionResponse {
		switch turn {
		case 1:
			return workerSaid("Added the parser and its tests.")
		case 2:
			return workerSaid(missedPrompt)
		default:
			// The worker addresses the finding, and the reviewer that asked
			// for it approves the work — so the re-prompted round finishes
			// like any other rather than waiting on a poll.
			rig.workerWrote()
			rig.approveOpenPullRequests()
			return workerSaid("Moved the fix inside pkg/thing as the finding asks.")
		}
	}

	job := rig.job(t)
	if err := rig.runner.Run(context.Background(), job); err != nil {
		t.Fatalf("Run = %v, want the re-prompted round to finish like any other", err)
	}

	prompts := rig.agents.promptTexts()
	if len(prompts) != 3 {
		t.Fatalf("prompts = %d, want the first turn, the round and its one re-prompt", len(prompts))
	}
	// THE SAME PROMPT PLUS ONE LINE. The round's findings are still on it —
	// a re-prompt that dropped them would be a different question — and the
	// only thing added says the last reply missed.
	if !strings.Contains(prompts[2], "keep the fix inside pkg/thing") {
		t.Fatalf("the re-prompt dropped the round's findings: %q", prompts[2])
	}
	if !strings.Contains(prompts[2], "did not address this prompt") {
		t.Fatalf("the re-prompt does not say why it was sent again: %q", prompts[2])
	}
	if !strings.Contains(prompts[2], "451ms") {
		t.Fatalf("the re-prompt does not carry the measurement it was decided on: %q", prompts[2])
	}
	if prompts[1] == prompts[2] {
		t.Fatal("the re-prompt sent the identical bytes, which repeatedPrompt exists to refuse")
	}

	reprompted := rig.auditDetails(auditNonAnswerReprompted)
	if len(reprompted) != 1 {
		t.Fatalf("non-answer events = %v, want exactly one", reprompted)
	}
	if !strings.Contains(reprompted[0], "elapsed=451ms") || !strings.Contains(reprompted[0], "tool_uses=0") {
		t.Fatalf("the audit does not carry the turn's own measurements: %q", reprompted[0])
	}

	// NEITHER A ROUND NOR AN ATTEMPT. The re-prompt's turn belongs to the
	// same round the reply that missed was answering, and to the same
	// attempt — a review round was genuinely spent between turn one and
	// turn two, and none between turn two and turn three.
	turns := rig.auditDetails(auditAgentTurn)
	if len(turns) != 3 {
		t.Fatalf("turn events = %d, want one per prompt", len(turns))
	}
	if !strings.Contains(turns[1], "outcome="+string(TurnOutcomeNonAnswer)) {
		t.Fatalf("the reply that missed was not classified as a non-answer: %q", turns[1])
	}
	if strings.Contains(turns[2], "outcome=") {
		t.Fatalf("the real answer was classified as a no-change turn: %q", turns[2])
	}
	if turnField(t, turns[0], "round") == turnField(t, turns[1], "round") {
		t.Fatalf("the review round was never counted: %q then %q", turns[0], turns[1])
	}
	if got, want := turnField(t, turns[2], "round"), turnField(t, turns[1], "round"); got != want {
		t.Fatalf("the re-prompt spent a round: round=%s, want the missed reply's round=%s", got, want)
	}
	for _, turn := range turns {
		if attempt := turnField(t, turn, "attempt"); attempt != "1" {
			t.Fatalf("the re-prompt spent an attempt: attempt=%s", attempt)
		}
	}

	// The work the re-prompt bought was verified and merged like any other.
	if commands := rig.verificationsRun(); len(commands) != 2 {
		t.Fatalf("verification commands run = %v, want round one's and the answered round's", commands)
	}
	if job.Parked() != nil {
		t.Fatalf("a re-prompted round parked the task: %+v", job.Parked())
	}
	if turn := rig.storedDelivery(t).LastTurn; turn != nil {
		t.Fatalf("a turn that judged nothing was left for the next attempt: %+v", turn)
	}
}

// turnField reads one of a turn event's counts — the runner writes them as
// `name=value` — so a test asserts on what the event says rather than on a
// number it computed the same way twice.
func turnField(t *testing.T, event, name string) string {
	t.Helper()
	for _, field := range strings.Fields(event) {
		if value, ok := strings.CutPrefix(field, name+"="); ok {
			return value
		}
	}
	t.Fatalf("turn event carries no %s: %q", name, event)
	return ""
}

// TestRunner_TwoConsecutiveNonAnswersAreStillAWorkFailure: the bound. One
// re-prompt is a session flushing something it had queued; a second reply
// that misses is a session not reading prompts at all, and on an untouched
// tree that is the ordinary claimed-work failure.
func TestRunner_TwoConsecutiveNonAnswersAreStillAWorkFailure(t *testing.T) {
	rig := newRunnerRig(t)
	rig.agents.answersIn(451*time.Millisecond, missedPrompt)

	job := rig.job(t)
	err := rig.runner.Run(context.Background(), job)

	var none *NoChangesError
	if !errors.As(err, &none) || !errors.Is(err, ErrNoChanges) {
		t.Fatalf("Run = %v, want NoChangesError", err)
	}
	if !strings.Contains(none.Reply, "The expired monitor") {
		t.Fatalf("the error dropped the reply: %+v", none)
	}
	if prompts := rig.agents.promptTexts(); len(prompts) != MaxNonAnswerTurns+1 {
		t.Fatalf("prompts = %d, want %d", len(prompts), MaxNonAnswerTurns+1)
	}
	if reprompted := rig.auditDetails(auditNonAnswerReprompted); len(reprompted) != MaxNonAnswerTurns {
		t.Fatalf("non-answer events = %v, want %d", reprompted, MaxNonAnswerTurns)
	}
	if job.Parked() != nil {
		t.Fatalf("a work failure parked the task: %+v", job.Parked())
	}
	if turn := rig.storedDelivery(t).LastTurn; turn == nil || turn.Outcome != TurnOutcomeNonAnswer {
		t.Fatalf("the charged turn was not recorded on the delivery: %+v", turn)
	}
}

// TestRunner_TheNonAnswerNoteIsNotCarriedIntoALaterRound: the note is an
// account of what just happened, and it is consumed by the prompt it was
// written for. A later round that still carried it would tell a worker its
// last reply missed when it did not.
func TestRunner_TheNonAnswerNoteIsNotCarriedIntoALaterRound(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.verdictOnPublish("REQUEST_CHANGES", "keep the fix inside pkg/thing")
	nonAnswerOn(rig.agents, 1)
	rig.agents.replyFn = func(turn int) *orchestration.PromptSessionResponse {
		if turn == 1 {
			return workerSaid(missedPrompt)
		}
		rig.workerWrote()
		return workerSaid("Added the parser and its tests.")
	}

	if err := rig.runner.Run(context.Background(), rig.job(t)); err == nil {
		t.Fatal("a review that never converged reported success")
	}

	prompts := rig.agents.promptTexts()
	if len(prompts) < 3 {
		t.Fatalf("prompts = %d, want the first turn, its re-prompt and at least one round", len(prompts))
	}
	if !strings.Contains(prompts[1], "did not address this prompt") {
		t.Fatalf("the re-prompt does not carry the note it was sent with: %q", prompts[1])
	}
	for i, prompt := range prompts[2:] {
		if strings.Contains(prompt, "did not address this prompt") {
			t.Fatalf("round prompt %d still carries the re-prompt's note: %q", i+1, prompt)
		}
	}
	if !strings.Contains(prompts[2], "keep the fix inside pkg/thing") {
		t.Fatalf("the round prompt lost its findings: %q", prompts[2])
	}
}

// TestRunner_AFastReplyThatChangedTheTreeIsNotANonAnswer: speed alone says
// nothing. A worker that edited a file in under a second did the work, and
// the turn goes to verification like any other.
func TestRunner_AFastReplyThatChangedTheTreeIsNotANonAnswer(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.agents.answersIn(451*time.Millisecond, "One line; the fix was a typo in the route table.")
	rig.verdictOnPublish("APPROVED", "")

	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run = %v, want the fast productive turn to proceed", err)
	}
	if reprompted := rig.auditDetails(auditNonAnswerReprompted); len(reprompted) != 0 {
		t.Fatalf("a turn that changed a file was re-prompted: %v", reprompted)
	}
	if prompts := rig.agents.promptTexts(); len(prompts) != 1 {
		t.Fatalf("prompts = %d, want the one turn that did the work", len(prompts))
	}
	if commands := rig.verificationsRun(); len(commands) != 1 {
		t.Fatalf("verification commands run = %v, want the turn's work verified", commands)
	}
}

// TestRunner_ASlowNoChangeReplyIsChargedWithoutAReprompt: the other side of
// the bound. A worker that spent a real turn and still reports work on an
// untouched tree is the claimed-work failure it has always been — it is not
// re-prompted, and the attempt is charged on the first turn.
func TestRunner_ASlowNoChangeReplyIsChargedWithoutAReprompt(t *testing.T) {
	rig := newRunnerRig(t)
	rig.agents.answersIn(NonAnswerTurnBound+time.Second,
		"Implemented the parser, added the tests, everything passes.")

	err := rig.runner.Run(context.Background(), rig.job(t))

	var none *NoChangesError
	if !errors.As(err, &none) || !errors.Is(err, ErrNoChanges) {
		t.Fatalf("Run = %v, want NoChangesError on the first turn", err)
	}
	if prompts := rig.agents.promptTexts(); len(prompts) != 1 {
		t.Fatalf("prompts = %d, want the one turn that was judged", len(prompts))
	}
	if reprompted := rig.auditDetails(auditNonAnswerReprompted); len(reprompted) != 0 {
		t.Fatalf("a turn that took real time was re-prompted: %v", reprompted)
	}
	turns := rig.auditDetails(auditAgentTurn)
	if len(turns) != 1 || !strings.Contains(turns[0], "outcome="+string(TurnOutcomeClaimedWork)) {
		t.Fatalf("turn events = %v, want one claimed-work turn", turns)
	}
}
