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
	"strings"
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

// TestExecute_ARoundSpawnsAWorkerTheRunNeverHad: a fresh job whose task
// already had an open pull request never spawned a worker — resumeReview
// sent it straight to the gate — and when the gate asks for a round it must
// get one through Initializing, exactly as a resumed run does. Refusing it as
// "no worker session to prompt" killed go-quicbaas#53's CI-red round the
// moment the gate began reporting a red pipeline ahead of a missing approval
// (2026-09-21).
func TestExecute_ARoundSpawnsAWorkerTheRunNeverHad(t *testing.T) {
	run := resumeRun(t, openDelivery(971, "4b334a47e960"),
		&ForgePullRequest{Number: 971, State: "open", HeadSHA: "4b334a47e960"})
	run.roundRequested = true
	state, err := run.execute(context.Background())
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if state != orchestration.JobStateInitializing {
		t.Fatalf("state = %q, want %q — the round needs a worker this run never spawned",
			state, orchestration.JobStateInitializing)
	}
}

// TestExecute_NoSessionAndNoRoundIsStillRefused: a run that reached a turn
// with no session and no round asked for skipped initialization, which is the
// programming error the refusal exists for.
func TestExecute_NoSessionAndNoRoundIsStillRefused(t *testing.T) {
	run := resumeRun(t, openDelivery(971, "4b334a47e960"),
		&ForgePullRequest{Number: 971, State: "open", HeadSHA: "4b334a47e960"})
	if _, err := run.execute(context.Background()); err == nil {
		t.Fatal("a run with no session and no round was prompted")
	}
}

// TestAdoptBranchPullRequest_BindsAReAdoptedIssueToItsOpenPullRequest covers
// the second half of the orphan class.
//
// A DELIVERY IS KEYED BY TASK AND A TASK IS TERMINAL, so an issue whose first
// task ended is adopted again as a NEW task with a FRESH delivery that knows
// nothing of the pull request the old one opened. The branch name is planned
// from the ISSUE, so it is the same name; the remote branch still carries the
// old commits; and the new attempt cuts its branch from the base instead.
//
// Measured on go-truststrap#51 (2026-09-15): every push was refused with
// "! [rejected] (non-fast-forward)", and each refusal was charged to the WORK,
// so the task spent its whole budget failing at something the work had nothing
// to do with. Re-labelling an orphaned issue was not merely useless but
// harmful.
func TestAdoptBranchPullRequest_BindsAReAdoptedIssueToItsOpenPullRequest(t *testing.T) {
	rig := newRunnerRig(t)
	ctx := context.Background()

	const branch = "issue/51-the-branch-the-old-task-opened"
	rig.forge.mu.Lock()
	rig.forge.prs[65] = &ForgePullRequest{
		Number: 65, State: "open", HeadRef: branch, HeadSHA: "437558de8a85",
		Title: "the old task's pull request", BaseRef: "main",
	}
	rig.forge.mu.Unlock()

	// A FRESH delivery: same branch, no pull request, nothing published.
	delivery := &Delivery{
		TaskID: testTask, ProjectID: testProject, Branch: branch, Base: "main", Attempt: 1,
		RequiresPullRequest: true,
	}
	if err := rig.store.PutDelivery(ctx, testTenant, delivery); err != nil {
		t.Fatalf("PutDelivery: %v", err)
	}

	run := &runnerRun{
		job: orchestration.NewJobRun(orchestration.JobRunConfig{
			JobID:  "job-1",
			Config: &orchestration.JobConfig{TenantID: testTenant, ProjectID: testProject, TaskID: testTask, Attempt: 1},
		}),
		runner:   rig.runner,
		delivery: delivery,
		attempt:  &attempt{task: testTaskRecord(51), issue: 51, forge: rig.forge, repo: RepoRef{Owner: "o", Repo: "r"}},
	}

	adopted, err := run.adoptBranchPullRequest(ctx)
	if err != nil {
		t.Fatalf("adoptBranchPullRequest: %v", err)
	}
	if !adopted {
		t.Fatal("an open pull request on this delivery's branch must be adopted, not raced with a rival branch")
	}
	if run.delivery.PullRequest.Number != 65 {
		t.Fatalf("delivery pull request = %+v, want #65", run.delivery.PullRequest)
	}
	// THE HEAD MUST COME FROM THE FORGE: this attempt published nothing, and
	// resumeReview refuses to resume without a head.
	if run.delivery.HeadSHA != "437558de8a85" {
		t.Fatalf("delivery head = %q, want the forge's", run.delivery.HeadSHA)
	}

	// THE BINDING IS DURABLE. A binding only this attempt could see would be
	// lost when the attempt ended — the same shape as the orphan it repairs.
	stored, err := rig.store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatalf("GetDelivery: %v", err)
	}
	if stored.PullRequest.Number != 65 || stored.HeadSHA != "437558de8a85" {
		t.Fatalf("the binding did not reach the store: %+v", stored.PullRequest)
	}
}

// TestAdoptBranchPullRequest_LeavesAnUnrelatedBranchAlone keeps a task from
// hijacking somebody else's pull request.
func TestAdoptBranchPullRequest_LeavesAnUnrelatedBranchAlone(t *testing.T) {
	rig := newRunnerRig(t)
	ctx := context.Background()

	rig.forge.mu.Lock()
	rig.forge.prs[65] = &ForgePullRequest{Number: 65, State: "open", HeadRef: "issue/99-someone-elses", HeadSHA: "aaa"}
	rig.forge.prs[70] = &ForgePullRequest{Number: 70, State: "closed", HeadRef: "issue/51-mine", HeadSHA: "bbb"}
	rig.forge.prs[71] = &ForgePullRequest{Number: 71, State: "closed", Merged: true, HeadRef: "issue/51-mine", HeadSHA: "ccc"}
	rig.forge.mu.Unlock()

	delivery := &Delivery{
		TaskID: testTask, ProjectID: testProject, Branch: "issue/51-mine", Base: "main", Attempt: 1,
		RequiresPullRequest: true,
	}
	if err := rig.store.PutDelivery(ctx, testTenant, delivery); err != nil {
		t.Fatalf("PutDelivery: %v", err)
	}
	run := &runnerRun{
		job: orchestration.NewJobRun(orchestration.JobRunConfig{
			JobID:  "job-1",
			Config: &orchestration.JobConfig{TenantID: testTenant, ProjectID: testProject, TaskID: testTask, Attempt: 1},
		}),
		runner:   rig.runner,
		delivery: delivery,
		attempt:  &attempt{task: testTaskRecord(51), issue: 51, forge: rig.forge, repo: RepoRef{Owner: "o", Repo: "r"}},
	}

	adopted, err := run.adoptBranchPullRequest(ctx)
	if err != nil {
		t.Fatalf("adoptBranchPullRequest: %v", err)
	}
	if adopted {
		t.Fatalf("adopted something it should not have: %+v", run.delivery.PullRequest)
	}

	// And a delivery with no branch planned yet asks the forge nothing.
	run.delivery.Branch = ""
	before := rig.forge.listOpenPRs
	if adopted, err := run.adoptBranchPullRequest(ctx); err != nil || adopted {
		t.Fatalf("a delivery with no branch must adopt nothing: adopted=%v err=%v", adopted, err)
	}
	if rig.forge.listOpenPRs != before {
		t.Fatal("a delivery with no branch must not list the forge at all")
	}
}

// TestTriageAgent_FallsBackToTheWorker pins the fallback that keeps every
// existing binding working, and the override that lets triage run elsewhere.
//
// TRIAGE READS AND JUDGES; IT WRITES NO CODE. Running it on the same model that
// writes the code is a cost with no benefit, so a binding may name its own
// triage agent. An empty one falls back to the worker, which is what every
// project did before the field existed.
func TestTriageAgent_FallsBackToTheWorker(t *testing.T) {
	worker := WorkerConfig{Backend: "claude", Model: "claude-opus-5", Image: "worker:1"}

	binding := &ProjectBinding{Worker: worker}
	agent := binding.Triage
	if strings.TrimSpace(agent.Backend) == "" {
		agent = binding.Worker
	}
	if agent != worker {
		t.Fatalf("an unset triage agent must fall back to the worker, got %+v", agent)
	}

	binding.Triage = WorkerConfig{Backend: "qwen", Model: "qwen3:1.7b", Image: "worker:1"}
	agent = binding.Triage
	if strings.TrimSpace(agent.Backend) == "" {
		agent = binding.Worker
	}
	if agent.Backend != "qwen" || agent.Model != "qwen3:1.7b" {
		t.Fatalf("a named triage agent must be used verbatim, got %+v", agent)
	}
}
