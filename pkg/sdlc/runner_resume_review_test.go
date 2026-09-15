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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// resumeRun builds a runnerRun whose task already has a delivery, and a forge
// holding one pull request in the given shape.
func resumeRun(t *testing.T, delivery *Delivery, pr *ForgePullRequest) *runnerRun {
	t.Helper()
	forge := newFakeForge()
	if pr != nil {
		forge.prs[pr.Number] = pr
	}
	job := orchestration.NewJobRun(orchestration.JobRunConfig{
		JobID:   "job-1",
		Config:  &orchestration.JobConfig{TenantID: testTenant, ProjectID: testProject, TaskID: testTask, Attempt: 2},
		Project: testOrchProject(),
	})
	return &runnerRun{
		job:      job,
		delivery: delivery,
		attempt:  &attempt{task: testTaskRecord(92), issue: 92, forge: forge, repo: RepoRef{Owner: "o", Repo: "r"}},
	}
}

func openDelivery(number int, head string) *Delivery {
	return &Delivery{
		Branch: "issue/92-x", Base: "main", HeadSHA: head,
		RequiresPullRequest: true,
		PullRequest:         DeliveryPullRequest{Number: number, State: PullRequestOpen},
	}
}

// TestResumeReview_ResumesInsteadOfWorkingAgain is the fix for three pull
// requests stranded on the burn-in fleet.
//
// THE SEQUENCE IT BREAKS. An attempt opened a pull request, then ended for a
// reason that was not the work's fault. The next attempt was handed a
// workspace with that branch checked out, the worker read the tree, found the
// change already present and reported `satisfied` — correctly, by its own
// contract — and the conclusion seam completed the task as `already_satisfied`
// on the premise that there was "nothing to merge". The pull request that
// attempt had opened was still open, and completing the task left it with
// nobody to merge it, ever.
func TestResumeReview_ResumesInsteadOfWorkingAgain(t *testing.T) {
	run := resumeRun(t, openDelivery(197, "e2de05246bd1"),
		&ForgePullRequest{Number: 197, State: "open", HeadSHA: "e2de05246bd1"})

	state, err := run.resumeReview(context.Background())
	if err != nil {
		t.Fatalf("resumeReview: %v", err)
	}
	if state != orchestration.JobStateReviewing {
		t.Fatalf("state = %q, want %q — an attempt whose pull request is open must not re-run the worker",
			state, orchestration.JobStateReviewing)
	}
}

// TestResumeReview_EmptyHeadDoesNotResume guards against a state loop.
//
// publishForReview sends an attempt that reaches Reviewing with no session and
// no HeadSHA BACK to Initializing so the work gets done — and Initializing is
// where resumeReview runs. Firing on an empty head would bounce the job
// between the two states until its budget ran out.
func TestResumeReview_EmptyHeadDoesNotResume(t *testing.T) {
	run := resumeRun(t, openDelivery(197, ""),
		&ForgePullRequest{Number: 197, State: "open"})

	state, err := run.resumeReview(context.Background())
	if err != nil {
		t.Fatalf("resumeReview: %v", err)
	}
	if state != "" {
		t.Fatalf("state = %q, want empty — an empty head must fall through to the worker, not ping-pong", state)
	}
}

// TestResumeReview_AsksTheForgeNotTheRecord proves a stale delivery cannot
// send an attempt into a review of something that already landed.
//
// The delivery says what was true when it was written. A pull request merged
// or closed by a person since then is finished, and the attempt must do its
// own work rather than review a merge. Hooks.Complete re-reads for the same
// reason.
func TestResumeReview_AsksTheForgeNotTheRecord(t *testing.T) {
	cases := []struct {
		name string
		pr   *ForgePullRequest
	}{
		{"merged since the record was written", &ForgePullRequest{Number: 197, State: "closed", Merged: true, HeadSHA: "e2de"}},
		{"closed unmerged — somebody rejected this delivery", &ForgePullRequest{Number: 197, State: "closed", HeadSHA: "e2de"}},
		{"gone from the forge entirely", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// The DELIVERY still claims the pull request is open.
			run := resumeRun(t, openDelivery(197, "e2de"), c.pr)
			if c.pr == nil {
				run.attempt.forge.(*fakeForge).getPRNil = true
			}
			state, err := run.resumeReview(context.Background())
			if err != nil {
				t.Fatalf("resumeReview: %v", err)
			}
			if state != "" {
				t.Fatalf("state = %q, want empty — the forge's answer decides, not the record", state)
			}
		})
	}
}

// TestResumeReview_NothingToResumeFallsThrough covers the ordinary first
// attempt, which must reach the worker exactly as before.
func TestResumeReview_NothingToResumeFallsThrough(t *testing.T) {
	cases := []struct {
		name     string
		delivery *Delivery
	}{
		{"no delivery yet", nil},
		{"delivery with no pull request", &Delivery{Branch: "issue/92-x", RequiresPullRequest: true, HeadSHA: "abc"}},
		{"a carve-out that needs no pull request", &Delivery{
			Branch: "issue/92-x", HeadSHA: "abc", RequiresPullRequest: false,
			PullRequest: DeliveryPullRequest{Number: 197, State: PullRequestOpen},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			run := resumeRun(t, c.delivery, nil)
			state, err := run.resumeReview(context.Background())
			if err != nil {
				t.Fatalf("resumeReview: %v", err)
			}
			if state != "" {
				t.Fatalf("state = %q, want empty — this attempt has work to do", state)
			}
		})
	}
}

// TestResumeReview_ARequestedRoundOutranksTheResume pins the interaction that
// broke TestRunner_AResumedReviewAskedForAnotherRoundSpawnsAWorker when the
// resume was first written.
//
// A ROUND RETURNS THROUGH INITIALIZING. nextRound hands back JobStateExecuting,
// but a round needs a container a resumed run does not hold, so Executing goes
// to Initializing to get one — and Initializing is where resumeReview lives.
// Without the guard it saw the pull request still open, sent the job straight
// back to Reviewing without running the worker, and the gate then rejected the
// same unchanged head until the round budget was spent. The symptom was a
// review that never converged because nobody ever changed anything: "changes
// requested after 3 review round(s)" on a change that had had exactly one.
func TestResumeReview_ARequestedRoundOutranksTheResume(t *testing.T) {
	run := resumeRun(t, openDelivery(197, "e2de05246bd1"),
		&ForgePullRequest{Number: 197, State: "open", HeadSHA: "e2de05246bd1"})

	// Same inputs that resume on a fresh attempt...
	state, err := run.resumeReview(context.Background())
	if err != nil {
		t.Fatalf("resumeReview: %v", err)
	}
	if state != orchestration.JobStateReviewing {
		t.Fatalf("precondition: a fresh attempt must resume, got %q", state)
	}

	// ...must run the worker once a round has asked for new work.
	run.roundRequested = true
	state, err = run.resumeReview(context.Background())
	if err != nil {
		t.Fatalf("resumeReview after a round: %v", err)
	}
	if state != "" {
		t.Fatalf("state = %q, want empty — the gate rejected this head, so reviewing it again judges the identical tree", state)
	}
}
