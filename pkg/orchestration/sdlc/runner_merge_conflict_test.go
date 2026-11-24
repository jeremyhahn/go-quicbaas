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
// A pull request that conflicts with its base is a merge round
// =============================================================================
//
// Measured on go-truststrap#64 (2026-09-22). The PR was codex-approved, green
// and past the gate, while main had moved five commits ahead into a real
// conflict. Complete retried the merge until the not-ready bound was spent,
// returned the exhaustion as a hook error, and the attempt was charged as a
// failure of the work. That happened twice. Nothing ever asked the worker to
// merge the base into the branch, which is the only thing that fixes it.
//
// The branch is never rewritten (owner ruling 2026-09-04, handbook
// version-control §5b). The fake container refuses every force push outright,
// so each test here also proves no round asked for one.

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

// workerMergedBase is a worker that merged the base into its branch: the head
// moves to a merge commit that descends from the published one, so the plain
// push fast-forwards, and the reviewer approves the new head.
func (r *runnerRig) workerMergedBase(head string) {
	r.approveOnANewHead(head)
}

// resolveConflict is the base's check settling mergeable once the merge is
// published, so the forge merges again.
func (r *runnerRig) resolveConflict() {
	r.forge.mu.Lock()
	defer r.forge.mu.Unlock()
	for _, pr := range r.forge.prs {
		pr.Mergeable = true
	}
	r.forge.mergeNotReadyN = 0
}

func TestRunner_AConflictingApprovedPullRequestGetsAMergeRound(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.conflictOnPublish()

	turn := 0
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		turn++
		if turn > 1 {
			// The merge moves the head, the reviewer approves the new head,
			// and the base no longer conflicts.
			rig.workerMergedBase("head-merged")
			rig.resolveConflict()
		}
	}
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run = %v, want the merged change landed; a conflict is not a failure of the work", err)
	}

	prompts := rig.agents.promptTexts()
	if len(prompts) != 2 {
		t.Fatalf("worker turns = %d, want the first turn and one merge round", len(prompts))
	}
	for _, want := range []string{"Round 1 findings", "Merge conflict with main", "git fetch origin main",
		"git merge origin/main", "head-issue/25-fix-the-thing"} {
		if !strings.Contains(prompts[1], want) {
			t.Errorf("the merge round does not tell the worker %q:\n%s", want, prompts[1])
		}
	}
	if strings.Contains(strings.ToLower(prompts[1]), "rebase") {
		t.Errorf("the merge round mentions a rebase; a published branch is never rewritten:\n%s", prompts[1])
	}
	// The worker was released while the approval was judged, so the merge
	// round runs in a fresh container on the same branch.
	if got := rig.agents.sessionCount(); got != 2 {
		t.Fatalf("spawns = %d, want the first worker and the one respawned for the merge round", got)
	}

	// The approval of the conflicting head did not carry over. The merge that
	// landed is bound to the merged head, which the gate judged again.
	merges := rig.forge.mergeCalls()
	if len(merges) == 0 || merges[len(merges)-1].head != "head-merged" {
		t.Fatalf("merges = %+v, want the last one bound to the merged head", merges)
	}
	if conflicts := rig.auditDetails(auditMergeConflict); len(conflicts) != 1 ||
		!strings.Contains(conflicts[0], "base=main") {
		t.Fatalf("merge-conflict records = %v, want exactly one naming the base", conflicts)
	}
	if parked := rig.auditDetails(auditParked); len(parked) != 0 {
		t.Fatalf("a conflict that one merge resolved parked the task: %v", parked)
	}
}

// TestRunner_APullRequestThatKeepsConflictingStillEscalates keeps the round
// budget honest. A merge round is spent from the same budget as a review
// round, so a base that conflicts again after every merge ends with a person
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
			// Merged and re-approved, and still in conflict.
			rig.workerMergedBase("head-merged")
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
		t.Fatalf("worker turns = %d, want the first turn plus %d merge round(s)", len(prompts), rounds)
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

// TestRunner_AConflictFoundWithNoWorkerSpawnsOneForTheMerge is the production
// shape of the defect. The re-dispatched job found its approved PR already
// open, went straight to the gate without spawning a worker, and met the
// conflict at the merge. Merging the base needs a checkout that the run does not have,
// so it goes back through initializing to get one. It does not fail on a
// session that was never there.
func TestRunner_AConflictFoundWithNoWorkerSpawnsOneForTheMerge(t *testing.T) {
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
		rig.workerMergedBase("head-merged")
		rig.resolveConflict()
	}

	job := rig.job(t)
	job.StartState = orchestration.JobStateCompleting
	if err := rig.runner.Run(ctx, job); err != nil {
		t.Fatalf("Run = %v, want the merged change landed", err)
	}
	if rig.agents.sessionCount() != WorkerSessionCount {
		t.Fatalf("sessions = %d, want the one worker the merge runs in", rig.agents.sessionCount())
	}
	prompts := rig.agents.promptTexts()
	if len(prompts) != 1 || !strings.Contains(prompts[0], "Merge conflict with main") {
		t.Fatalf("prompts = %d, want the one merge round:\n%s", len(prompts), strings.Join(prompts, "\n---\n"))
	}
	merges := rig.forge.mergeCalls()
	if merges[len(merges)-1].head != "head-merged" {
		t.Fatalf("merges = %+v, want the last bound to the merged head", merges)
	}
}

// TestRunner_AMergeRoundPublishesWithAPlainPushOnEveryWorkspaceKind is the
// owner's ruling held end to end. The merge round's head descends from the
// head the factory published, so the publication after it is the same plain
// fast-forward push every round makes, on the persistent tier and on the
// ephemeral one, and no push of the attempt carries a force of any kind.
func TestRunner_AMergeRoundPublishesWithAPlainPushOnEveryWorkspaceKind(t *testing.T) {
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
					rig.workerMergedBase("head-merged")
					rig.resolveConflict()
				}
			}

			if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
				t.Fatalf("Run = %v, want the merged branch published and landed", err)
			}
			pushes := rig.pushes()
			if len(pushes) < 2 {
				t.Fatalf("pushes = %+v, want the approved head and the merge published", pushes)
			}
			for _, push := range pushes {
				for _, arg := range push.Args {
					if arg == "-f" || strings.Contains(arg, "force") {
						t.Fatalf("a publication carried %q; the factory never forces: %+v", arg, push)
					}
				}
				if push.RemoteBranch != "" {
					t.Fatalf("the merge was published to %q, want the attempt's own branch", push.RemoteBranch)
				}
			}
			merges := rig.forge.mergeCalls()
			if merges[len(merges)-1].head != "head-merged" {
				t.Fatalf("merges = %+v, want the last bound to the merged head", merges)
			}
		})
	}
}

// TestRunner_ARewrittenBranchIsRefusedNotForced is the other half. A worker
// that rewrote its published branch anyway leaves a head the remote cannot
// fast-forward to. The push is refused, the refusal is the error, and nothing
// forces past it: forcing would discard the commit the review approved.
func TestRunner_ARewrittenBranchIsRefusedNotForced(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.conflictOnPublish()
	turn := 0
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		turn++
		if turn > 1 {
			rig.workerMergedBase("head-rewritten")
			rig.container.mu.Lock()
			rig.container.diverged = true
			rig.container.mu.Unlock()
		}
	}

	err := rig.runner.Run(context.Background(), rig.job(t))
	var refused *PublishCommandError
	if !errors.As(err, &refused) {
		t.Fatalf("Run = %T %v, want the non-fast-forward refusal as a PublishCommandError", err, err)
	}
	if !strings.Contains(refused.Output, "non-fast-forward") {
		t.Fatalf("the refusal says %q, want git's non-fast-forward rejection", refused.Output)
	}
	for _, merge := range rig.forge.mergeCalls() {
		if merge.head == "head-rewritten" {
			t.Fatalf("a rewritten head that was never published was merged: %+v", rig.forge.mergeCalls())
		}
	}
}
