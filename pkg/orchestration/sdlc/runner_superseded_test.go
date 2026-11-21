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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestRunnerRun_AClosedUnmergedPullRequestSupersedesTheAttempt is the review
// half of go-quicbaas#1054: a pull request closed without merging while the
// gate waits on it ends the attempt as superseded. It gets no worker round,
// no park and so no owner question.
//
// Before, the gate answered a closed pull request as a failed verdict. The
// runner sent it back to the worker, and once the rounds ran out it parked
// with a question for the owner. Task 3a58725f did exactly that against its
// closed PR #1052.
func TestRunnerRun_AClosedUnmergedPullRequestSupersedesTheAttempt(t *testing.T) {
	rig := newRunnerRig(t)
	rig.workerWrote()
	rig.forge.onCreatePullRequest = func(pr *ForgePullRequest) {
		pr.State = "closed"
	}

	run := newRunnerRun(t, rig)
	iteration := run.job.Iteration()
	state, err := run.review(context.Background())
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if state != orchestration.JobStateCompleted {
		t.Fatalf("state = %q, want completed: a closed pull request gets no further round", state)
	}
	if !run.job.Superseded() {
		t.Fatal("the run did not record the superseded judgement, so the task would conclude `completed` in silence")
	}
	if run.job.Satisfied() {
		t.Error("a closed pull request was recorded as already satisfied")
	}
	if run.parked.kind != "" || run.job.EscalationReason() != "" {
		t.Errorf("the run parked (kind %q, reason %q): a closed pull request is not an owner question",
			run.parked.kind, run.job.EscalationReason())
	}
	if run.feedback != "" {
		t.Errorf("the closed pull request was turned into worker feedback: %q", run.feedback)
	}
	if run.job.Iteration() != iteration {
		t.Errorf("iteration = %d, want %d: no review round may be spent", run.job.Iteration(), iteration)
	}
}

// TestResumeReview_AClosedUnmergedPullRequestSupersedesTheAttempt is the
// resume half: an attempt that starts on a delivery whose pull request has
// since been closed unmerged concludes instead of initializing a worker.
func TestResumeReview_AClosedUnmergedPullRequestSupersedesTheAttempt(t *testing.T) {
	run := resumeRun(t, openDelivery(197, "e2de"), &ForgePullRequest{Number: 197, State: "closed", HeadSHA: "e2de"})
	state, err := run.resumeReview(context.Background())
	if err != nil {
		t.Fatalf("resumeReview: %v", err)
	}
	if state != orchestration.JobStateCompleted {
		t.Fatalf("state = %q, want completed: no worker round for a pull request nobody will merge", state)
	}
	if !run.job.Superseded() {
		t.Fatal("the run did not record the superseded judgement")
	}
	if run.parked.kind != "" || run.job.EscalationReason() != "" {
		t.Errorf("the run parked (kind %q): a closed pull request is not an owner question", run.parked.kind)
	}
}

// TestResumeReview_AClosedPullRequestOutranksEveryPathToTheWorker: a requested
// round and a recorded refusal both send the attempt to the worker. A pull
// request closed after either must still end the attempt. Before, the closure
// check sat behind both exits, so a closed pull request with a prior refusal
// went on to prepare a workspace.
func TestResumeReview_AClosedPullRequestOutranksEveryPathToTheWorker(t *testing.T) {
	cases := map[string]func(*runnerRun){
		"after a requested round": func(run *runnerRun) { run.roundRequested = true },
		"after a recorded refusal": func(run *runnerRun) {
			run.delivery.Reviews = []ReviewRecord{{
				Reviewer: "codex", Verdict: VerdictRequestChanges, HeadSHA: "e2de",
			}}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			closed := &ForgePullRequest{Number: 197, State: "closed", HeadSHA: "e2de"}
			run := resumeRun(t, openDelivery(197, "e2de"), closed)
			setup(run)

			// The precondition: with the pull request still open, this path
			// goes to the worker.
			closed.State = "open"
			state, err := run.resumeReview(context.Background())
			if err != nil || state != "" {
				t.Fatalf("precondition: state=%q err=%v, want the worker path", state, err)
			}

			closed.State = "closed"
			state, err = run.resumeReview(context.Background())
			if err != nil {
				t.Fatalf("resumeReview: %v", err)
			}
			if state != orchestration.JobStateCompleted || !run.job.Superseded() {
				t.Fatalf("state = %q, superseded = %v: a closed pull request must not reach the worker",
					state, run.job.Superseded())
			}
			if run.parked.kind != "" || run.job.EscalationReason() != "" {
				t.Errorf("the run parked (kind %q): a closed pull request is not an owner question", run.parked.kind)
			}
		})
	}
}

// TestReviewValidator_AClosedUnmergedPullRequestIsATypedEnd pins the gate's
// half. A closed, unmerged pull request comes back as the typed error the
// runner concludes on, never as a failed verdict that would redelegate.
func TestReviewValidator_AClosedUnmergedPullRequestIsATypedEnd(t *testing.T) {
	rig := newReviewRig(t)
	rig.forge.prs[100].State = "closed"
	result, err := rig.validator.Validate(context.Background(), testJob(1), rig.criterion)
	var closed *PullRequestClosedUnmergedError
	if !errors.As(err, &closed) || !errors.Is(err, ErrPullRequestClosedUnmerged) {
		t.Fatalf("err = %v, want PullRequestClosedUnmergedError", err)
	}
	if closed.Number != 100 {
		t.Errorf("number = %d, want 100", closed.Number)
	}
	if result.Passed {
		t.Error("a closed pull request passed the gate")
	}
	delivery, _ := rig.store.GetDelivery(context.Background(), testTenant, testTask)
	if delivery.PullRequest.State != PullRequestClosed {
		t.Errorf("state not recorded: %+v", delivery.PullRequest)
	}
}

// TestOutcomeOfConclusion_SupersededPostsItsOwnComment: the conclusion is
// announced on the issue and moves no label that reads as a problem.
func TestOutcomeOfConclusion_SupersededPostsItsOwnComment(t *testing.T) {
	kind, ok := OutcomeOfConclusion(orchestration.TaskConcludedSuperseded)
	if !ok || kind != OutcomeSuperseded {
		t.Fatalf("kind = %q, %v; want superseded", kind, ok)
	}
	spec, ok := lookupOutcomeSpec(kind)
	if !ok {
		t.Fatal("no rendering for superseded")
	}
	// Not resumable: the task completes, and completed is terminal in the
	// store's transition table, so a readmission would be refused.
	if spec.labelKey != "" || !spec.terminal || spec.resumable {
		t.Errorf("spec = %+v, want terminal, not resumable, no label", spec)
	}
}
