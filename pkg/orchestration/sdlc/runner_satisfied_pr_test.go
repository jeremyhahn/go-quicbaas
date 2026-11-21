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
// A satisfied turn on the task's own pull request (X5)
// =============================================================================
//
// Measured on go-luks2#209 (2026-09-22): attempt 1 opened PR #210, the
// integration run died, and the worker's next turn said "I made no changes
// this round ... satisfied". The task concluded already_satisfied and the
// issue was told nothing would happen; the pull request was approved with CI
// green an hour later and sat unmerged for three days.

func TestSatisfiedOnOwnPullRequest_AnOpenPullRequestGoesToReview(t *testing.T) {
	run := resumeRun(t, openDelivery(210, "df8ae49c"),
		&ForgePullRequest{Number: 210, State: "open", HeadSHA: "df8ae49c"})
	state, err := run.satisfiedOnOwnPullRequest(context.Background())
	if err != nil || state != orchestration.JobStateReviewing {
		t.Fatalf("state = %q, err = %v; want reviewing", state, err)
	}
	if run.job.Satisfied() {
		t.Fatal("the task was concluded as satisfied with its own pull request open")
	}
}

func TestSatisfiedOnOwnPullRequest_AMergedPullRequestCompletes(t *testing.T) {
	run := resumeRun(t, openDelivery(210, "df8ae49c"),
		&ForgePullRequest{Number: 210, State: "closed", Merged: true, HeadSHA: "df8ae49c"})
	state, err := run.satisfiedOnOwnPullRequest(context.Background())
	if err != nil || state != orchestration.JobStateCompleting {
		t.Fatalf("state = %q, err = %v; want completing", state, err)
	}
}

func TestSatisfiedOnOwnPullRequest_AClosedPullRequestSupersedes(t *testing.T) {
	run := resumeRun(t, openDelivery(210, "df8ae49c"),
		&ForgePullRequest{Number: 210, State: "closed", HeadSHA: "df8ae49c"})
	state, err := run.satisfiedOnOwnPullRequest(context.Background())
	if err != nil || state != orchestration.JobStateCompleted || !run.job.Superseded() {
		t.Fatalf("state = %q, err = %v, superseded = %v; want the attempt superseded", state, err, run.job.Superseded())
	}
}

// TestSatisfiedOnOwnPullRequest_NoPullRequestIsTheStaleIssue: a task that
// opened nothing is exactly the stale issue the verdict concludes.
func TestSatisfiedOnOwnPullRequest_NoPullRequestIsTheStaleIssue(t *testing.T) {
	for name, delivery := range map[string]*Delivery{
		"no delivery":     nil,
		"no pull request": {Branch: "issue/92-x", Base: "main", RequiresPullRequest: true},
		"carve-out":       {Branch: "issue/92-x", Base: "main"},
	} {
		t.Run(name, func(t *testing.T) {
			run := resumeRun(t, delivery, nil)
			state, err := run.satisfiedOnOwnPullRequest(context.Background())
			if state != "" || err != nil {
				t.Fatalf("state = %q, err = %v; want nothing to route", state, err)
			}
		})
	}
}

// TestSatisfiedOnOwnPullRequest_AForgeThatCannotAnswerIsAnError: the forge
// not answering is not "no pull request".
func TestSatisfiedOnOwnPullRequest_AForgeThatCannotAnswerIsAnError(t *testing.T) {
	run := resumeRun(t, openDelivery(210, "df8ae49c"), nil) // the forge holds no #210
	if _, err := run.satisfiedOnOwnPullRequest(context.Background()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v; a forge read that failed was treated as an answer", err)
	}
}

// TestRunner_ASatisfiedRoundOnAnOpenPullRequestIsReviewedNotConcluded drives
// the whole X5 sequence: round one opens the pull request and is refused; the
// worker's next turn changes nothing and says satisfied — the work is on the
// pull request — and the reviewer then approves that same head. The pull
// request is reviewed and merged, and the task is not concluded as satisfied.
func TestRunner_ASatisfiedRoundOnAnOpenPullRequestIsReviewedNotConcluded(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("REQUEST_CHANGES", "explain why the retry is safe")
	rig.agents.mu.Lock()
	rig.agents.replyFn = func(turn int) *orchestration.PromptSessionResponse {
		if turn == 1 {
			return workerSaid("Implemented the change.")
		}
		return workerSaid("I made no changes this round; the pull request already carries the work.\n\n" +
			TurnStopMarker + " " + TurnStopSatisfied)
	}
	round := 0
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		round++
		if round > 1 {
			rig.approveOpenPullRequests()
		}
	}
	rig.agents.mu.Unlock()

	job := rig.job(t)
	if err := rig.runner.Run(context.Background(), job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if job.Satisfied() {
		t.Fatal("the task was concluded as satisfied with its own pull request open")
	}
	if merges := rig.forge.mergeCalls(); len(merges) != 1 {
		t.Fatalf("merges = %+v, want the pull request merged", merges)
	}
	if audits := rig.auditDetails(auditSatisfiedOnPullRequest); len(audits) != 1 {
		t.Fatalf("satisfied_on_pull_request audits = %v, want one", audits)
	}
}

// TestRefusedPush_OnlyAnUntrustedSignerParks: any other publication failure
// is returned as it was, and the park reason names the commit and the way out.
func TestRefusedPush_OnlyAnUntrustedSignerParks(t *testing.T) {
	run := resumeRun(t, nil, nil)
	other := errors.New("the remote refused")
	if state, err := run.refusedPush(other); state != "" || !errors.Is(err, other) {
		t.Fatalf("state = %q, err = %v; want the failure returned", state, err)
	}
	if run.job.Parked() != nil {
		t.Fatal("a push that failed for another reason parked the task")
	}
	unsigned := &UnsignedCommitError{TaskID: "t", Commit: "0badc0ffee", Status: "U", Signer: "SHA256:stranger"}
	state, err := run.refusedPush(&orchestration.TurnPushError{JobID: "job-1", Cause: unsigned})
	if err != nil || state != orchestration.JobStateEscalating {
		t.Fatalf("state = %q, err = %v; want the task parked", state, err)
	}
	parked := run.job.Parked()
	if parked == nil || parked.FaultClass != orchestration.FaultUntrustedSigner {
		t.Fatalf("parked = %+v", parked)
	}
	for _, want := range []string{"0badc0f", "SHA256:stranger", "trusted signing history"} {
		if !strings.Contains(parked.Message, want) {
			t.Fatalf("reason %q does not carry %q", parked.Message, want)
		}
	}
}
