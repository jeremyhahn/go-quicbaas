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
// A pull request that conflicts with its base is a rebase round
// =============================================================================
//
// Measured on go-truststrap#64 (2026-09-22). The PR was codex-approved, green
// and past the gate, while main had moved five commits ahead into a real
// conflict. Complete retried the merge until the not-ready bound was spent,
// returned the exhaustion as a hook error, and the attempt was charged as a
// failure of the work. That happened twice. Nothing ever asked the worker to
// rebase, which is the only thing that fixes it.

// conflictOnPublish arms a pull request that is approved and green on the head
// it opens with, and that conflicts with its base. The forge answers every
// merge of it "not ready" and keeps it unmergeable, as Gitea does.
func (r *runnerRig) conflictOnPublish() {
	r.forge.onCreatePullRequest = func(pr *ForgePullRequest) {
		r.forge.reviews[pr.Number] = []ForgeReview{{ID: 1, Reviewer: "codex", State: "APPROVED", HeadSHA: pr.HeadSHA}}
		r.forge.checks[pr.HeadSHA] = []ForgeCheckRun{{ID: "1", Name: "ci", HeadSHA: pr.HeadSHA, Status: "completed", Conclusion: "success"}}
		pr.Mergeable = false
		r.forge.mergeNotReadyN = 1 << 20
	}
}

// workerRebased is a worker that rebased onto the base: the head moves to a
// commit that does not descend from the published one, the reviewer approves
// it, and a push without a lease is refused as non-fast-forward.
func (r *runnerRig) workerRebased(head string) {
	r.approveOnANewHead(head)
	r.container.mu.Lock()
	defer r.container.mu.Unlock()
	r.container.rewritten = true
}

// resolveConflict is the base's check settling mergeable once the rebase is
// published, so the forge merges again.
func (r *runnerRig) resolveConflict() {
	r.forge.mu.Lock()
	defer r.forge.mu.Unlock()
	for _, pr := range r.forge.prs {
		pr.Mergeable = true
	}
	r.forge.mergeNotReadyN = 0
}

func TestRunner_AConflictingApprovedPullRequestGetsARebaseRound(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.conflictOnPublish()

	turn := 0
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		turn++
		if turn > 1 {
			// The rebase moves the head, the reviewer approves the new head,
			// and the base no longer conflicts.
			rig.workerRebased("head-rebased")
			rig.resolveConflict()
		}
	}
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run = %v, want the rebased change merged; a conflict is not a failure of the work", err)
	}

	prompts := rig.agents.promptTexts()
	if len(prompts) != 2 {
		t.Fatalf("worker turns = %d, want the first turn and one rebase round", len(prompts))
	}
	for _, want := range []string{"Round 1 findings", "Merge conflict with main", "git rebase origin/main", "head-issue/25-fix-the-thing"} {
		if !strings.Contains(prompts[1], want) {
			t.Errorf("the rebase round does not tell the worker %q:\n%s", want, prompts[1])
		}
	}
	if rig.agents.sessionCount() != WorkerSessionCount {
		t.Fatalf("the rebase round spawned a second session: %d", rig.agents.sessionCount())
	}

	// The approval of the conflicting head did not carry over. The merge that
	// landed is bound to the rebased head, which the gate judged again.
	merges := rig.forge.mergeCalls()
	if len(merges) == 0 || merges[len(merges)-1].head != "head-rebased" {
		t.Fatalf("merges = %+v, want the last one bound to the rebased head", merges)
	}
	if conflicts := rig.auditDetails(auditMergeConflict); len(conflicts) != 1 ||
		!strings.Contains(conflicts[0], "base=main") {
		t.Fatalf("merge-conflict records = %v, want exactly one naming the base", conflicts)
	}
	if parked := rig.auditDetails(auditParked); len(parked) != 0 {
		t.Fatalf("a conflict that one rebase resolved parked the task: %v", parked)
	}
}

// TestRunner_APullRequestThatKeepsConflictingStillEscalates keeps the round
// budget honest. A rebase round is spent from the same budget as a review
// round, so a base that conflicts again after every rebase ends with a person
// and not with another worker turn.
func TestRunner_APullRequestThatKeepsConflictingStillEscalates(t *testing.T) {
	const rounds = 1
	rig := newRunnerRig(t)
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Verification = []string{"true"}
		b.MaxReviewRounds = rounds
	}); err != nil {
		t.Fatal(err)
	}
	rig.workerWrote()
	rig.conflictOnPublish()
	turn := 0
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		turn++
		if turn > 1 {
			// Rebased and re-approved, and still in conflict.
			rig.workerRebased("head-rebased")
		}
	}

	err := rig.runner.Run(context.Background(), rig.job(t))
	var escalated *EscalatedError
	if !errors.As(err, &escalated) {
		t.Fatalf("Run = %v, want the escalation a spent round budget ends on", err)
	}
	if !strings.Contains(escalated.Reason, "merge conflict with main after 1 review round(s)") {
		t.Fatalf("the escalation does not say what was not converging: %q", escalated.Reason)
	}
	if prompts := rig.agents.promptTexts(); len(prompts) != rounds+1 {
		t.Fatalf("worker turns = %d, want the first turn plus %d rebase round(s)", len(prompts), rounds)
	}
	if conflicts := rig.auditDetails(auditMergeConflict); len(conflicts) != rounds+1 {
		t.Fatalf("merge-conflict records = %d, want one per merge that conflicted (%d)", len(conflicts), rounds+1)
	}
	if parks := rig.parkComments(t, 1); len(parks) != 1 {
		t.Fatalf("park comments = %d, want exactly one", len(parks))
	}
}

// TestRunnerRun_CompleteLeavesEveryOtherMergeRefusalAHookFailure keeps the new
// round narrow. Only a conflict is sent back to the worker. A forge that is
// genuinely not ready still fails the stage, as it did before.
func TestRunnerRun_CompleteLeavesEveryOtherMergeRefusalAHookFailure(t *testing.T) {
	rig := newRunnerRig(t)
	ctx := context.Background()
	if _, err := rig.hooks.Prepare(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	rig.publishHead(t, ctx, testPublishedHead)
	if err := rig.hooks.Reviewable(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	rig.approveOpenPullRequests()
	rig.forge.mergeNotReadyN = 1 << 20 // mergeable, yet never merged

	job := rig.job(t)
	job.StartState = orchestration.JobStateCompleting
	err := rig.runner.Run(ctx, job)
	if !errors.Is(err, ErrMergeNotReadyExhausted) {
		t.Fatalf("Run = %v, want the not-ready exhaustion as the stage's failure", err)
	}
	if conflicts := rig.auditDetails(auditMergeConflict); len(conflicts) != 0 {
		t.Fatalf("a forge that was not ready was sent to the worker as a conflict: %v", conflicts)
	}
	if rig.agents.sessionCount() != 0 {
		t.Fatal("a forge that was not ready spawned a worker")
	}
}

// TestRunner_AConflictFoundWithNoWorkerSpawnsOneForTheRebase is the production
// shape of the defect. The re-dispatched job found its approved PR already
// open, went straight to the gate without spawning a worker, and met the
// conflict at the merge. A rebase needs a checkout that the run does not have,
// so it goes back through initializing to get one. It does not fail on a
// session that was never there.
func TestRunner_AConflictFoundWithNoWorkerSpawnsOneForTheRebase(t *testing.T) {
	rig := newRunnerRig(t)
	ctx := context.Background()
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	if _, err := rig.hooks.Prepare(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	rig.publishHead(t, ctx, testPublishedHead)
	if err := rig.hooks.Reviewable(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	rig.approveOpenPullRequests()
	rig.forge.mu.Lock()
	for _, pr := range rig.forge.prs {
		pr.Mergeable = false
	}
	rig.forge.mergeNotReadyN = 1 << 20
	rig.forge.mu.Unlock()
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		rig.workerRebased("head-rebased")
		rig.resolveConflict()
	}

	job := rig.job(t)
	job.StartState = orchestration.JobStateCompleting
	if err := rig.runner.Run(ctx, job); err != nil {
		t.Fatalf("Run = %v, want the rebased change merged", err)
	}
	if rig.agents.sessionCount() != WorkerSessionCount {
		t.Fatalf("sessions = %d, want the one worker the rebase runs in", rig.agents.sessionCount())
	}
	prompts := rig.agents.promptTexts()
	if len(prompts) != 1 || !strings.Contains(prompts[0], "Merge conflict with main") {
		t.Fatalf("prompts = %d, want the one rebase round:\n%s", len(prompts), strings.Join(prompts, "\n---\n"))
	}
	merges := rig.forge.mergeCalls()
	if merges[len(merges)-1].head != "head-rebased" {
		t.Fatalf("merges = %+v, want the last bound to the rebased head", merges)
	}
}

// TestRunner_ARebaseIsPublishedUnderALeaseOnEveryWorkspaceKind is the Codex
// finding on go-quicbaas#1009. A rebase round rewrites the published branch,
// and a persistent workspace publishes nothing mid-turn. So the review push was
// the only push, and as a plain push it was refused as non-fast-forward. The
// attempt then failed instead of reaching re-review. The container here refuses
// exactly that push. The publication has to be a force leased on the head the
// factory published, on the persistent tier and on the ephemeral one.
func TestRunner_ARebaseIsPublishedUnderALeaseOnEveryWorkspaceKind(t *testing.T) {
	tiers := []struct {
		name    string
		storage func(*runnerRig)
	}{
		{name: "persistent", storage: func(*runnerRig) {}},
		{name: "ephemeral", storage: func(r *runnerRig) { r.storage = ephemeralWorkspaceTier() }},
	}
	for _, tier := range tiers {
		t.Run(tier.name, func(t *testing.T) {
			rig := newRunnerRig(t)
			tier.storage(rig)
			rig.rebindVerification(t, "true")
			rig.workerWrote()
			rig.conflictOnPublish()
			turn := 0
			rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
				turn++
				if turn > 1 {
					rig.workerRebased("head-rebased")
					rig.resolveConflict()
				}
			}

			if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
				t.Fatalf("Run = %v, want the rebased branch published and merged", err)
			}
			// The first publication is of the approved head; every one after
			// the conflict is the rewrite, and each must be leased on the head
			// the remote held when it was made.
			rig.container.mu.Lock()
			pushes := append([]pushedRef(nil), rig.container.pushes...)
			rig.container.mu.Unlock()
			approved := rig.forge.mergeCalls()[0].head
			var rewrite *pushedRef
			for i := range pushes {
				if pushes[i].Lease != "" {
					rewrite = &pushes[i]
					break
				}
			}
			if rewrite == nil || !rewrite.Force || rewrite.Lease != approved {
				t.Fatalf("pushes = %+v, want the rebase forced under a lease on the approved head %s", pushes, approved)
			}
			if rewrite.RemoteBranch != "" {
				t.Fatalf("the rebase was published to %q, want the attempt's own branch", rewrite.RemoteBranch)
			}
			merges := rig.forge.mergeCalls()
			if merges[len(merges)-1].head != "head-rebased" {
				t.Fatalf("merges = %+v, want the last bound to the rebased head", merges)
			}
		})
	}
}

// TestRunner_ARoundThatRewritesNothingIsNeverForced keeps the lease on the
// rebase round alone. A review round without a conflict publishes as it always
// has, with no force.
func TestRunner_ARoundThatRewritesNothingIsNeverForced(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("REQUEST_CHANGES", "keep the fix inside pkg/thing")
	turn := 0
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		turn++
		if turn > 1 {
			rig.approveOnANewHead("head-round-2")
		}
	}
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if forced := rig.forcedPushes(); len(forced) != 0 {
		t.Fatalf("a round with no rewrite was force-published: %+v", forced)
	}
}
