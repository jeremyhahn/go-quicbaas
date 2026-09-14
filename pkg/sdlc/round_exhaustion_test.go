// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"strconv"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Round exhaustion — the loop has to END
// =============================================================================
//
// MaxReviewRounds is the only thing standing between a project whose review
// never converges and an agent that keeps being paid to author another attempt.
// What it is worth depends entirely on what happens when it is spent, and these
// are the assertions that say: the task is parked, the park reaches a person
// exactly once, the pull request is labelled so nobody waits for a verdict
// nothing will ask for, and the run never re-enters review.
//
// THE TERMINATION IS THE POINT, NOT THE LABELLING. A test that only checked the
// label would pass on an engine that labelled the pull request and then went
// round again.

// exhaustionRig arms a run whose review refuses every round on a fresh head,
// with the bound set to rounds. It is what a project that never converges looks
// like, and it moves the head on every round, which is also the case the
// supersession record exists for.
func exhaustionRig(t *testing.T, rounds int) *runnerRig {
	t.Helper()
	rig := newRunnerRig(t)
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Verification = []string{"true"}
		b.MaxReviewRounds = rounds
		b.Forge.Labels[LabelEscalated] = "factory:escalated"
	}); err != nil {
		t.Fatal(err)
	}
	rig.workerWrote()
	rig.verdictOnPublish("REQUEST_CHANGES", "the test still passes without the fix")

	// Every later turn pushes a new head and is refused on it, so each round
	// is decisive and no round waits on a poll interval.
	turn := 0
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		turn++
		if turn > 1 {
			rig.requestChangesOnANewHead("head-round-"+strconv.Itoa(turn), "still not it")
		}
	}
	return rig
}

// TestRunner_ExhaustingTheReviewRoundsParksOnceAndNeverReviewsAgain is the
// termination guarantee, asserted on the thing that costs money: the number of
// worker turns and the number of review gates the run is allowed.
func TestRunner_ExhaustingTheReviewRoundsParksOnceAndNeverReviewsAgain(t *testing.T) {
	const rounds = 2
	rig := exhaustionRig(t, rounds)

	err := rig.runner.Run(context.Background(), rig.job(t))
	var escalated *EscalatedError
	if !errors.As(err, &escalated) {
		t.Fatalf("Run err = %v, want the escalation a spent budget ends on", err)
	}

	// THE BOUND IS THE BOUND. One first turn plus `rounds` send-backs, and the
	// review gate consulted exactly once per turn. A fourth read would be the
	// run re-entering review after the park.
	if prompts := rig.agents.promptTexts(); len(prompts) != rounds+1 {
		t.Fatalf("worker turns = %d, want the first turn plus %d round(s)", len(prompts), rounds)
	}
	if reads := rig.forge.reviewReads(); reads != rounds+1 {
		t.Fatalf("review gate consulted %d time(s), want %d: the run re-entered review after the park",
			reads, rounds+1)
	}
	if merges := rig.forge.mergeCalls(); len(merges) != 0 {
		t.Fatalf("a change the engine gave up on was merged: %+v", merges)
	}

	// PARKED AND ESCALATED EXACTLY ONCE.
	if parked := rig.auditDetails(auditParked); len(parked) != 1 {
		t.Fatalf("park records = %d, want exactly one:\n%v", len(parked), parked)
	}
	parks := rig.parkComments(t, 1)
	if len(parks) != 1 {
		t.Fatalf("park comments on the issue = %d, want exactly one", len(parks))
	}

	// The reason names the bound that was spent, and states who clears it in
	// the typed form the unblock sweep reads.
	if !strings.Contains(escalated.Reason, "after "+strconv.Itoa(rounds)+" review round(s)") {
		t.Fatalf("the escalation does not name the spent bound: %q", escalated.Reason)
	}
	condition, detail, parseErr := ParseBlockedReason(escalated.Reason)
	if parseErr != nil {
		t.Fatalf("the park carries no typed condition (%v): %q", parseErr, escalated.Reason)
	}
	if condition.Kind != UnblockOwnerRuling {
		t.Fatalf("park condition = %s, want owner_ruling: review that did not converge is the owner's call", condition.Kind)
	}
	if !strings.Contains(detail, "changes requested") {
		t.Fatalf("the detail lost what happened: %q", detail)
	}
}

// TestRunner_AnExhaustedReviewLabelsThePullRequestAndTheIssue is the half item
// 9 of the control-loop design names and the engine did not do: the park was
// visible on the issue only.
//
// The branch is published on every non-completed exit, so an exhausted review
// leaves an OPEN pull request behind. Unlabelled, it reads exactly like a change
// waiting for review — and the next reader either waits for a verdict nobody
// will ask for, or reviews and merges a change the engine already refused.
func TestRunner_AnExhaustedReviewLabelsThePullRequestAndTheIssue(t *testing.T) {
	rig := exhaustionRig(t, 1)

	if err := rig.runner.Run(context.Background(), rig.job(t)); err == nil {
		t.Fatal("a spent review budget completed the job")
	}

	if labels := rig.forge.issueLabels(25); !containsLabel(labels, "factory:escalated") {
		t.Fatalf("issue labels = %v, want the park shown", labels)
	}
	if labels := rig.forge.issueLabels(25); containsLabel(labels, "task:ready") {
		t.Fatalf("issue labels = %v, want the trigger removed: the issue is no longer claimable", labels)
	}

	prs := rig.forge.pullRequests()
	if len(prs) != 1 {
		t.Fatalf("pull requests = %d, want the one the attempt opened", len(prs))
	}
	labels := rig.forge.pullRequestLabels(prs[0].Number)
	if !containsLabel(labels, "factory:escalated") {
		t.Fatalf("pull request labels = %v, want the park shown on the change itself", labels)
	}
	// ADDED, NOT MOVED. A pull request carries labels a person may have put
	// there and never carried the module's trigger, so there is nothing of the
	// module's to remove and a removal would be destroying somebody's
	// annotation.
	if len(labels) != 1 {
		t.Fatalf("pull request labels = %v, want exactly the park label added", labels)
	}
}

// TestRunner_ARepeatedParkOnThePullRequestAddsNothing: the escalation is the
// state an interrupted run RESUMES at, so its forge writes are re-run. The
// comment is guarded by the marker; the labels are guarded by being idempotent,
// and this is the assertion that they are.
func TestRunner_ARepeatedParkOnThePullRequestAddsNothing(t *testing.T) {
	rig := newRunnerRig(t)
	ctx := context.Background()
	if err := rig.rebind(ctx, func(b *ProjectBinding) {
		b.Forge.Labels[LabelEscalated] = "factory:escalated"
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.hooks.Prepare(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	rig.publishHead(t, ctx, testPublishedHead)
	if err := rig.hooks.Reviewable(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	const reason = "changes requested after 3 review round(s); this change needs a person"
	interrupted := rig.interruptedStatus(orchestration.JobStateEscalating,
		orchestration.JobAuditEvent{Type: auditEscalated, Details: reason})

	for resume := 1; resume <= 2; resume++ {
		if err := rig.runner.Run(ctx, rig.resumedJob(t, interrupted)); err == nil {
			t.Fatalf("resume %d completed the job", resume)
		}
	}

	if parks := rig.parkComments(t, 1); len(parks) != 1 {
		t.Fatalf("park comments = %d after two resumes, want exactly one", len(parks))
	}
	prs := rig.forge.pullRequests()
	if len(prs) != 1 {
		t.Fatalf("pull requests = %d", len(prs))
	}
	labels := rig.forge.pullRequestLabels(prs[0].Number)
	if len(labels) != 1 || labels[0] != "factory:escalated" {
		t.Fatalf("pull request labels = %v after two resumes, want the park label once", labels)
	}
}

// TestHooks_TheParkLabelsOnlyAnOpenPullRequest is the bad-input half of the
// pull-request label. The label says "the engine stopped and a person owns this
// now", which is a statement about an open change: stamping it on a landed one
// describes the wrong object, and a binding that names no label must not start
// failing its parks.
func TestHooks_TheParkLabelsOnlyAnOpenPullRequest(t *testing.T) {
	cases := map[string]struct {
		delivery  *Delivery
		label     string
		wantAdded bool
	}{
		"an open pull request is labelled": {
			delivery:  &Delivery{PullRequest: DeliveryPullRequest{Number: 100, State: PullRequestOpen}},
			label:     "factory:escalated",
			wantAdded: true,
		},
		"a merged pull request is left alone": {
			delivery: &Delivery{PullRequest: DeliveryPullRequest{Number: 100, State: PullRequestMerged}},
			label:    "factory:escalated",
		},
		"a closed pull request is left alone": {
			delivery: &Delivery{PullRequest: DeliveryPullRequest{Number: 100, State: PullRequestClosed}},
			label:    "factory:escalated",
		},
		"a carve-out change has no pull request to label": {
			delivery: &Delivery{},
			label:    "factory:escalated",
		},
		"a task parked before its branch was cut has no delivery": {
			delivery: nil,
			label:    "factory:escalated",
		},
		"a binding naming no label writes nothing": {
			delivery: &Delivery{PullRequest: DeliveryPullRequest{Number: 100, State: PullRequestOpen}},
			label:    "",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rig := newHooksRig(t)
			ctx := context.Background()
			if err := rig.rebind(ctx, func(b *ProjectBinding) {
				if tc.label == "" {
					delete(b.Forge.Labels, LabelEscalated)
					return
				}
				b.Forge.Labels[LabelEscalated] = tc.label
			}); err != nil {
				t.Fatal(err)
			}
			rig.forge.prs[100] = &ForgePullRequest{Number: 100, State: "open"}
			a, err := rig.hooks.resolve(ctx, testJob(1))
			if err != nil {
				t.Fatal(err)
			}
			if err := rig.hooks.labelPullRequest(ctx, a, tc.delivery, LabelEscalated); err != nil {
				t.Fatalf("labelPullRequest: %v", err)
			}
			labels := rig.forge.pullRequestLabels(100)
			if tc.wantAdded && (len(labels) != 1 || labels[0] != tc.label) {
				t.Fatalf("pull request labels = %v, want %q", labels, tc.label)
			}
			if !tc.wantAdded && len(labels) != 0 {
				t.Fatalf("pull request labels = %v, want none", labels)
			}
		})
	}
}

// TestHooks_AParkThatCannotLabelThePullRequestIsReported: a forge write that
// failed is not a park that succeeded. The label is display output, but a
// silent failure here is how a parked change goes on looking reviewable.
func TestHooks_AParkThatCannotLabelThePullRequestIsReported(t *testing.T) {
	rig := newHooksRig(t)
	ctx := context.Background()
	if err := rig.rebind(ctx, func(b *ProjectBinding) {
		b.Forge.Labels[LabelEscalated] = "factory:escalated"
	}); err != nil {
		t.Fatal(err)
	}
	rig.forge.prs[100] = &ForgePullRequest{Number: 100, State: "open"}
	rig.forge.addLabelErr = errors.New("the forge refused the label")

	a, err := rig.hooks.resolve(ctx, testJob(1))
	if err != nil {
		t.Fatal(err)
	}
	delivery := &Delivery{PullRequest: DeliveryPullRequest{Number: 100, State: PullRequestOpen}}
	if err := rig.hooks.labelPullRequest(ctx, a, delivery, LabelEscalated); err == nil {
		t.Fatal("a refused label was reported as applied")
	}
}

// unreadableDeliveries is a store whose delivery read fails for a reason that
// is not "there is none".
type unreadableDeliveries struct {
	*Store
	err error
}

func (u *unreadableDeliveries) GetDelivery(context.Context, string, string) (*Delivery, error) {
	return nil, u.err
}

// TestHooks_BlockedReportsADeliveryItCouldNotRead is the bad-input half of the
// read the park's rendering and its pull-request label both depend on.
//
// "There is no delivery" is a legitimate park — a task stopped before its
// branch was cut — and answers nil. A store that FAILED is not that, and
// swallowing it would post a park comment with no pull request in it and leave
// the change unlabelled, silently, on the one path where the display matters
// most.
func TestHooks_BlockedReportsADeliveryItCouldNotRead(t *testing.T) {
	rig := newHooksRig(t)
	ctx := context.Background()
	if _, err := rig.hooks.Prepare(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	broken := &unreadableDeliveries{Store: rig.store, err: errors.New("the store is unreachable")}
	hooks, err := NewHooks(HooksConfig{
		Projects: &fakeProjects{projects: map[string]*orchestration.Project{testProject: testOrchProject()}},
		Bindings: rig.store, Deliveries: broken, Tasks: rig.tasks, Forges: &fakeForges{forge: rig.forge},
		MergeAttempts: 3, Waiter: rig.waiter,
		Surfacing: SurfacePolicy{RetryAttempts: 2, RetryInterval: 1, PostTimeout: 1e9},
	})
	if err != nil {
		t.Fatal(err)
	}
	blockErr := hooks.Blocked(ctx, testJob(1), orchestration.BlockReason{
		Kind: orchestration.TaskBlockKindEscalated, Message: "review did not converge",
	})
	if blockErr == nil {
		t.Fatal("an unreadable delivery was reported as a clean park")
	}
	if !strings.Contains(blockErr.Error(), "the store is unreachable") {
		t.Fatalf("err = %v, want the store's own failure", blockErr)
	}
	if comments := rig.forge.commentsOn(25); len(comments) != 0 {
		t.Fatalf("a park was posted from a delivery that could not be read: %+v", comments)
	}
}

// TestRunnerRun_NextRoundIsMonotoneOnceTheBudgetIsSpent is the invariant
// underneath the whole guarantee, asserted directly rather than inferred from a
// run: once the bound is crossed, EVERY answer is escalating. A guard that let
// one more round through on any later call would be an unbounded loop wearing a
// bound.
func TestRunnerRun_NextRoundIsMonotoneOnceTheBudgetIsSpent(t *testing.T) {
	const rounds = 2
	rig := newRunnerRig(t)
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) { b.MaxReviewRounds = rounds }); err != nil {
		t.Fatal(err)
	}
	run := newRunnerRun(t, rig)
	ctx := context.Background()

	for round := 1; round <= rounds; round++ {
		next, err := run.nextRound(ctx, "changes requested")
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		if next != orchestration.JobStateExecuting {
			t.Fatalf("round %d answered %q, want another round while the budget holds", round, next)
		}
	}
	for spent := 1; spent <= 3; spent++ {
		next, err := run.nextRound(ctx, "changes requested")
		if err != nil {
			t.Fatalf("call %d past the bound: %v", spent, err)
		}
		if next != orchestration.JobStateEscalating {
			t.Fatalf("call %d past the bound answered %q, want escalating every time", spent, next)
		}
	}
	if run.blockKind() != orchestration.TaskBlockKindEscalated {
		t.Fatalf("block kind = %q, want escalated", run.blockKind())
	}
}

// TestRunnerRun_NextRoundResolvesTheBindingItWasNotGiven is the bad-input half
// of nextRound: it is reached from validate and from redelegate, and a resumed
// run enters redelegating with nothing resolved yet. Reading the bound off a nil
// attempt would panic on exactly the path a crash recovery takes.
func TestRunnerRun_NextRoundResolvesTheBindingItWasNotGiven(t *testing.T) {
	rig := newRunnerRig(t)
	run := &runnerRun{runner: rig.runner, job: rig.job(t)}
	if run.attempt != nil {
		t.Fatal("the run was pre-resolved; the case under test is the one that is not")
	}
	next, err := run.nextRound(context.Background(), "changes requested")
	if err != nil {
		t.Fatalf("nextRound on an unresolved run: %v", err)
	}
	if next != orchestration.JobStateExecuting {
		t.Fatalf("next = %q, want the first round", next)
	}
	if run.attempt == nil {
		t.Fatal("nextRound did not resolve the binding it needed")
	}
}

// TestRunner_TheExhaustedRunRecordsTheRunsItsOwnRoundsDisplaced ties the two
// halves of this change together. Every round pushes a new head, so an attempt
// that spends its rounds is also the attempt that leaves the most displaced CI
// runs behind — and those runs are what a person reading the park comment would
// otherwise find on the forge and re-run.
func TestRunner_TheExhaustedRunRecordsTheRunsItsOwnRoundsDisplaced(t *testing.T) {
	const rounds = 2
	rig := exhaustionRig(t, rounds)
	ctx := context.Background()

	if err := rig.runner.Run(ctx, rig.job(t)); err == nil {
		t.Fatal("a spent review budget completed the job")
	}

	delivery, err := rig.store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	if len(delivery.SupersededRuns) != rounds {
		t.Fatalf("superseded runs = %+v, want one per head the rounds pushed (%d)", delivery.SupersededRuns, rounds)
	}
	for _, record := range delivery.SupersededRuns {
		if record.SupersededBy == "" || record.HeadSHA == record.SupersededBy {
			t.Fatalf("record names no displacement: %+v", record)
		}
		if record.HeadSHA == delivery.HeadSHA {
			t.Fatalf("the CURRENT head was recorded as displaced: %+v", record)
		}
	}
}
