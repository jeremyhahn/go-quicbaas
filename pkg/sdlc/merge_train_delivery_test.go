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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// A delivery rides the train
// =============================================================================
//
// The delivery runner is driven end to end, exactly as the rest of its suite
// drives it; the train itself is stood in for by a nudger that answers the way
// a train would — landing the change, ejecting it, or finding the train turned
// off — because the train's own decisions are proven in
// merge_train_runner_test.go.

// scriptedTrain answers each nudge with the next scripted outcome for the
// queued change.
type scriptedTrain struct {
	mu       sync.Mutex
	store    *Store
	outcomes []func(*TrainEntry)
	nudges   int
}

func (s *scriptedTrain) NudgeTrain(ctx context.Context, tenantID, projectID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nudges++
	if len(s.outcomes) == 0 {
		return nil
	}
	outcome := s.outcomes[0]
	s.outcomes = s.outcomes[1:]
	entry, err := s.store.GetTrainEntry(ctx, tenantID, projectID, testTask)
	if err != nil {
		return err
	}
	outcome(entry)
	return s.store.SaveTrainEntry(ctx, tenantID, entry)
}

// landIt is a train landing the change.
func landIt(entry *TrainEntry) {
	entry.State = TrainEntryLanded
	entry.Train = 1
	entry.MergeSHA = "train-merge-sha"
	entry.TrainPullRequest = 900
}

// ejectIt is a train ejecting the change for a red pipeline.
func ejectIt(entry *TrainEntry) {
	entry.State = TrainEntryEjected
	entry.Train = 1
	entry.Ejection = &TrainEjection{Reason: TrainEjectChecksFailed, Train: 1,
		Findings: "## CI failed\n\n`ci` failed: widget_test.go:12: got 1, want 2"}
}

// refuseIt is a train that carried the change alone and whose reviewer
// requested changes on it.
func refuseIt(entry *TrainEntry) {
	entry.State = TrainEntryEjected
	entry.Train = 1
	entry.Ejection = &TrainEjection{Reason: TrainEjectReviewRefused, Train: 1,
		Detail: "The review is on the train's pull request, #900.", Findings: "The retry loop never backs off on main."}
}

// trainedRunnerRig is the delivery rig on a project that trains, with the
// train stood in for by script.
func trainedRunnerRig(t *testing.T, outcomes ...func(*TrainEntry)) (*runnerRig, *scriptedTrain) {
	t.Helper()
	rig := newRunnerRig(t)
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Verification = []string{"true"}
		b.Reviewer = trainTestReviewer()
	}); err != nil {
		t.Fatal(err)
	}
	train := &scriptedTrain{store: rig.store, outcomes: outcomes}
	rig.runner.hooks.trains = rig.store
	rig.runner.hooks.trainNudger = train
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")
	return rig, train
}

// TestTrainedDelivery_AnApprovedHeadIsQueuedAndClosedWhereItLanded is the
// delivery's whole ride: approved, queued instead of merged, and — once the
// train lands it — recorded as merged with its own pull request closed naming
// where the change went.
func TestTrainedDelivery_AnApprovedHeadIsQueuedAndClosedWhereItLanded(t *testing.T) {
	rig, train := trainedRunnerRig(t, landIt)
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if merges := rig.forge.mergeCalls(); len(merges) != 0 {
		t.Fatalf("a trained change was merged on its own: %+v", merges)
	}
	if train.nudges == 0 {
		t.Fatal("the queued change never woke the train")
	}
	delivery, err := rig.store.GetDelivery(context.Background(), testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.PullRequest.State != PullRequestMerged {
		t.Fatalf("delivery pull request state = %s, want merged", delivery.PullRequest.State)
	}
	pr := rig.forge.pullRequest(delivery.PullRequest.Number)
	if pr == nil || pr.State != "closed" || pr.Merged {
		t.Fatalf("the change's own pull request = %+v, want closed and not merged a second time", pr)
	}
	comments := rig.forge.commentsOn(delivery.PullRequest.Number)
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "merge train #1") ||
		!strings.Contains(comments[0].Body, shortSHA("train-merge-sha")) {
		t.Fatalf("pull request comments = %+v, want one naming the landing", comments)
	}
	if _, err := rig.store.GetTrainEntry(context.Background(), testTenant, testProject, testTask); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the landed entry outlived its delivery: %v", err)
	}
	if got := rig.forge.issueLabels(25); !containsLabel(got, "factory:merged") {
		t.Fatalf("issue labels = %v, want merged", got)
	}
	if len(rig.auditDetails(auditTrainQueued)) != 1 || len(rig.auditDetails(auditTrainLanded)) != 1 {
		t.Fatalf("audits: queued=%v landed=%v", rig.auditDetails(auditTrainQueued), rig.auditDetails(auditTrainLanded))
	}
}

// TestTrainedDelivery_ALandedCarsBranchIsDeleted proves a change a train
// landed loses its source branch, as a change merged on its own does: the
// car's pull request is closed rather than merged, so the forge's
// delete-on-merge never fires, and a branch left behind collides with the
// next task cut for the same issue (go-luks2#92, 2026-09-28: a fresh attempt
// cut issue/92-… from main and its push was refused non-fast-forward by the
// stale branch of the landed PR #216).
func TestTrainedDelivery_ALandedCarsBranchIsDeleted(t *testing.T) {
	rig, _ := trainedRunnerRig(t, landIt)
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	delivery, err := rig.store.GetDelivery(context.Background(), testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	if !delivery.DeleteAfterMerge || delivery.Branch == "" {
		t.Fatalf("fixture delivery = branch %q delete_after_merge %v, want a branch that goes once landed", delivery.Branch, delivery.DeleteAfterMerge)
	}
	if got := rig.forge.deleted(); len(got) != 1 || got[0] != delivery.Branch {
		t.Fatalf("deleted branches = %v, want exactly the car's branch %q", got, delivery.Branch)
	}
}

// TestTrainedDelivery_AFailedBranchDeleteFailsCompletionAndARetryFinishesIt
// proves a branch the forge would not delete is not silently left behind:
// Complete reports it, and the retried Complete — which finds the pull
// request already closed — still deletes the branch.
func TestTrainedDelivery_AFailedBranchDeleteFailsCompletionAndARetryFinishesIt(t *testing.T) {
	rig, _ := trainedRunnerRig(t, landIt)
	refused := &BackendError{Op: "DeleteBranch", Cause: context.DeadlineExceeded}
	rig.forge.deleteBranchErr = refused
	if err := rig.runner.Run(context.Background(), rig.job(t)); !errors.Is(err, refused) {
		t.Fatalf("Run error = %v, want the refused branch delete", err)
	}
	delivery, err := rig.store.GetDelivery(context.Background(), testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	if pr := rig.forge.pullRequest(delivery.PullRequest.Number); pr == nil || pr.State != "closed" {
		t.Fatalf("the car's pull request = %+v, want closed before the branch delete", pr)
	}

	rig.forge.deleteBranchErr = nil
	if err := rig.runner.hooks.Complete(context.Background(), rig.job(t).Context()); err != nil {
		t.Fatalf("retried Complete: %v", err)
	}
	if got := rig.forge.deleted(); len(got) != 1 || got[0] != delivery.Branch {
		t.Fatalf("deleted branches after the retry = %v, want %q", got, delivery.Branch)
	}
}

// TestTrainedDelivery_AnEjectionIsARoundWithTheTrainsFailure proves an ejected
// change goes back to its worker with what the train saw, and rides again once
// it is approved again.
func TestTrainedDelivery_AnEjectionIsARoundWithTheTrainsFailure(t *testing.T) {
	rig, _ := trainedRunnerRig(t, ejectIt, landIt)
	// The worker fixes the failure on the round the ejection asks for, and
	// the fix is approved on its new head.
	round := 0
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		round++
		if round > 1 {
			rig.workerWrote()
			rig.approveOnANewHead("head-round-2")
		}
	}
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prompts := rig.agents.promptTexts()
	if len(prompts) < 2 {
		t.Fatalf("prompts = %d, want a second round after the ejection", len(prompts))
	}
	second := prompts[len(prompts)-1]
	for _, want := range []string{"Ejected from merge train #1", "got 1, want 2"} {
		if !strings.Contains(second, want) {
			t.Fatalf("the round after the ejection lacks %q:\n%s", want, second)
		}
	}
	if ejections := rig.auditDetails(auditTrainEjected); len(ejections) != 1 || !strings.Contains(ejections[0], "checks_failed") {
		t.Fatalf("ejection audits = %v", ejections)
	}
	if landed := rig.auditDetails(auditTrainLanded); len(landed) != 1 {
		t.Fatalf("the change did not ride again and land: %v", landed)
	}
}

// TestTrainedDelivery_ARefusedTrainIsARoundWithTheReview proves a change a
// train's reviewer refused goes back to its worker with the review itself, and
// rides again once it is approved again.
func TestTrainedDelivery_ARefusedTrainIsARoundWithTheReview(t *testing.T) {
	rig, _ := trainedRunnerRig(t, refuseIt, landIt)
	round := 0
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		round++
		if round > 1 {
			rig.workerWrote()
			rig.approveOnANewHead("head-round-2")
		}
	}
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prompts := rig.agents.promptTexts()
	if len(prompts) < 2 {
		t.Fatalf("prompts = %d, want a second round after the refusal", len(prompts))
	}
	second := prompts[len(prompts)-1]
	for _, want := range []string{"Merge train #1 was refused by its reviewer", "The retry loop never backs off on main."} {
		if !strings.Contains(second, want) {
			t.Fatalf("the round after the refusal lacks %q:\n%s", want, second)
		}
	}
	if ejections := rig.auditDetails(auditTrainEjected); len(ejections) != 1 || !strings.Contains(ejections[0], "review_refused") {
		t.Fatalf("ejection audits = %v", ejections)
	}
	if landed := rig.auditDetails(auditTrainLanded); len(landed) != 1 {
		t.Fatalf("the change did not ride again and land: %v", landed)
	}
}

// TestTrainedDelivery_AnUntrainedProjectSaysWhyItMergesAlone proves the
// delivery records why an approved change was not queued: here the project's
// only reviewer is advisory, so nothing could review a train.
func TestTrainedDelivery_AnUntrainedProjectSaysWhyItMergesAlone(t *testing.T) {
	rig, train := trainedRunnerRig(t)
	no := false
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Verification = []string{"true"}
		b.Reviewer = trainTestReviewer()
		b.Reviewer.CountsTowardMerge = &no
	}); err != nil {
		t.Fatal(err)
	}
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if merges := rig.forge.mergeCalls(); len(merges) != 1 || train.nudges != 0 {
		t.Fatalf("merges = %+v, nudges = %d; want the change merged on its own", merges, train.nudges)
	}
	if off := rig.auditDetails(auditTrainOff); len(off) != 1 || !strings.Contains(off[0], "counts toward merge") {
		t.Fatalf("the delivery did not record why it merged alone: %v", off)
	}
}

// TestTrainedDelivery_ATrainTurnedOffReleasesAQueuedChange proves a change
// queued before an operator turned the train off is not stranded: it merges on
// its own.
func TestTrainedDelivery_ATrainTurnedOffReleasesAQueuedChange(t *testing.T) {
	rig, _ := trainedRunnerRig(t, func(*TrainEntry) {})
	rig.runner.hooks.trainNudger = nudgeFunc(func(ctx context.Context, _, _ string) error {
		return rig.rebind(ctx, func(b *ProjectBinding) {
			b.Verification = []string{"true"}
			b.Reviewer = trainTestReviewer()
			b.MergeTrain.Disabled = true
		})
	})
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if merges := rig.forge.mergeCalls(); len(merges) != 1 {
		t.Fatalf("merges = %+v, want the change merged on its own", merges)
	}
	if _, err := rig.store.GetTrainEntry(context.Background(), testTenant, testProject, testTask); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the withdrawn change is still queued: %v", err)
	}
}

// TestTrainedDelivery_ANodeWithNoTrainRefusesRatherThanMerging proves the
// operator's configuration is not overruled by whichever node ran the job.
func TestTrainedDelivery_ANodeWithNoTrainRefusesRatherThanMerging(t *testing.T) {
	rig, _ := trainedRunnerRig(t)
	rig.runner.hooks.trains = nil
	err := rig.runner.Run(context.Background(), rig.job(t))
	if !errors.Is(err, ErrTrainUnavailable) {
		t.Fatalf("Run = %v, want the train refused as unavailable", err)
	}
	if merges := rig.forge.mergeCalls(); len(merges) != 0 {
		t.Fatalf("a trained change merged on a node with no train: %+v", merges)
	}
}

// TestNewHooks_RefusesATrainWithNothingToWakeIt proves the wiring rule.
func TestNewHooks_RefusesATrainWithNothingToWakeIt(t *testing.T) {
	rig := newHooksRig(t)
	_, err := NewHooks(HooksConfig{
		Projects: &fakeProjects{}, Bindings: rig.store, Deliveries: rig.store, Tasks: rig.tasks,
		Forges: &fakeForges{forge: rig.forge}, Trains: rig.store,
	})
	var invalid *InvalidInputError
	if !errors.As(err, &invalid) || invalid.Field != "train_nudger" {
		t.Fatalf("NewHooks = %v, want train_nudger refused", err)
	}
}

// nudgeFunc adapts a function to TrainNudger.
type nudgeFunc func(ctx context.Context, tenantID, projectID string) error

func (f nudgeFunc) NudgeTrain(ctx context.Context, tenantID, projectID string) error {
	return f(ctx, tenantID, projectID)
}

// mergeByHand is a person merging the change's pull request on the forge
// while its entry waits in the queue: the train records nothing.
func mergeByHand(rig *runnerRig) func(*TrainEntry) {
	return func(entry *TrainEntry) {
		rig.forge.mu.Lock()
		defer rig.forge.mu.Unlock()
		if pr := rig.forge.prs[entry.PullRequest]; pr != nil {
			pr.Merged, pr.State = true, "closed"
		}
	}
}

// TestTrainedDelivery_APullRequestMergedOutsideTheTrainCompletes is X9
// (go-luks2 PR #210, 2026-09-25): a change waiting on its train was merged by
// hand, and the delivery waited in `reviewing` for a train answer that would
// never come. It must see the merge, take its entry out of the queue and
// complete as landed, without merging a second time.
func TestTrainedDelivery_APullRequestMergedOutsideTheTrainCompletes(t *testing.T) {
	rig, train := trainedRunnerRig(t)
	train.outcomes = []func(*TrainEntry){mergeByHand(rig)}
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if merges := rig.forge.mergeCalls(); len(merges) != 0 {
		t.Fatalf("a change merged by hand was merged again: %+v", merges)
	}
	delivery, err := rig.store.GetDelivery(context.Background(), testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.PullRequest.State != PullRequestMerged {
		t.Fatalf("delivery pull request state = %s, want merged", delivery.PullRequest.State)
	}
	if _, err := rig.store.GetTrainEntry(context.Background(), testTenant, testProject, testTask); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the entry outlived a change that landed outside the train: %v", err)
	}
	if got := rig.forge.issueLabels(25); !containsLabel(got, "factory:merged") || containsLabel(got, "factory:in-review") {
		t.Fatalf("issue labels = %v, want merged and no longer in review", got)
	}
	if audits := rig.auditDetails(auditTrainMergedOutside); len(audits) != 1 {
		t.Fatalf("train_merged_outside audits = %v, want one", audits)
	}
}

// TestTrainedDelivery_AnOpenPullRequestKeepsWaiting: the forge read changes
// nothing for a pull request that is still open — the train's answer decides.
func TestTrainedDelivery_AnOpenPullRequestKeepsWaiting(t *testing.T) {
	rig, _ := trainedRunnerRig(t, landIt)
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if audits := rig.auditDetails(auditTrainMergedOutside); len(audits) != 0 {
		t.Fatalf("an open pull request was read as merged: %v", audits)
	}
	if landed := rig.auditDetails(auditTrainLanded); len(landed) != 1 {
		t.Fatalf("the train's landing was not the answer: %v", landed)
	}
}

// closeByHand is a person closing the change's pull request without merging
// it while its entry waits.
func closeByHand(rig *runnerRig) func(*TrainEntry) {
	return func(entry *TrainEntry) {
		rig.forge.mu.Lock()
		defer rig.forge.mu.Unlock()
		if pr := rig.forge.prs[entry.PullRequest]; pr != nil {
			pr.State = "closed"
		}
	}
}

// ejectClosed is a train reading a car whose pull request is no longer open.
func ejectClosed(entry *TrainEntry) {
	entry.State = TrainEntryEjected
	entry.Train = 1
	entry.Ejection = &TrainEjection{Reason: TrainEjectClosed, Train: 1, Detail: "#" + strconv.Itoa(entry.PullRequest) + " is closed."}
}

// both applies two outcomes to one nudge.
func both(first, second func(*TrainEntry)) func(*TrainEntry) {
	return func(entry *TrainEntry) {
		first(entry)
		second(entry)
	}
}

// TestTrainedDelivery_APullRequestClosedOutsideTheTrainLeavesTheQueue is X15's
// closed half: a train ejected a car whose pull request somebody closed without
// merging. The delivery must end the attempt as superseded and take its entry
// out of the queue for good, WITHOUT another pass through the gate: an entry
// left behind is one the next approval re-queues.
func TestTrainedDelivery_APullRequestClosedOutsideTheTrainLeavesTheQueue(t *testing.T) {
	rig, train := trainedRunnerRig(t)
	train.outcomes = []func(*TrainEntry){both(closeByHand(rig), ejectClosed)}
	job := rig.job(t)
	if err := rig.runner.Run(context.Background(), job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !job.Superseded() {
		t.Fatal("a pull request closed without merging did not supersede the attempt")
	}
	if _, err := rig.store.GetTrainEntry(context.Background(), testTenant, testProject, testTask); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the entry of a closed pull request stayed in the queue: %v", err)
	}
	if audits := rig.auditDetails(auditTrainClosedOutside); len(audits) != 1 {
		t.Fatalf("train_closed_outside audits = %v, want one", audits)
	}
	if merges := rig.forge.mergeCalls(); len(merges) != 0 {
		t.Fatalf("a closed pull request was merged: %+v", merges)
	}
	if train.nudges != 1 {
		t.Fatalf("nudges = %d, want the one train that ejected it", train.nudges)
	}
}

// TestTrainedDelivery_AMergedEjectionIsNotQueuedAgain is the X15 storm itself
// (go-luks2 PR #210, trains #79 to #152): a train ejects a car as closed
// because its pull request was merged by hand. The delivery completes as
// landed and never queues the change for another train.
func TestTrainedDelivery_AMergedEjectionIsNotQueuedAgain(t *testing.T) {
	rig, train := trainedRunnerRig(t)
	train.outcomes = []func(*TrainEntry){both(mergeByHand(rig), ejectClosed)}
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if train.nudges != 1 {
		t.Fatalf("nudges = %d, want one: a merged change woke another train", train.nudges)
	}
	if queued := rig.auditDetails(auditTrainQueued); len(queued) != 1 {
		t.Fatalf("train_queued audits = %v, want the one approval", queued)
	}
	if _, err := rig.store.GetTrainEntry(context.Background(), testTenant, testProject, testTask); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the entry of a merged pull request stayed in the queue: %v", err)
	}
	if merges := rig.forge.mergeCalls(); len(merges) != 0 {
		t.Fatalf("a change merged by hand was merged again: %+v", merges)
	}
}

// TestTrainedDelivery_AnApprovalOfAMergedPullRequestWakesNoTrain: the gate
// passes a pull request that is already merged, and queueing it anyway woke a
// train whose only work was to eject it. The delivery completes without
// touching the queue.
func TestTrainedDelivery_AnApprovalOfAMergedPullRequestWakesNoTrain(t *testing.T) {
	rig, train := trainedRunnerRig(t)
	armed := rig.forge.onCreatePullRequest
	rig.forge.onCreatePullRequest = func(pr *ForgePullRequest) {
		armed(pr)
		pr.Merged, pr.State = true, "closed"
	}
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if train.nudges != 0 {
		t.Fatalf("nudges = %d, want none for a pull request already merged", train.nudges)
	}
	if queued := rig.auditDetails(auditTrainQueued); len(queued) != 0 {
		t.Fatalf("a merged pull request was queued: %v", queued)
	}
	if audits := rig.auditDetails(auditTrainMergedOutside); len(audits) != 1 {
		t.Fatalf("train_merged_outside audits = %v, want one", audits)
	}
}

// TestTrainedDelivery_AReopenedPullRequestGoesBackToTheGate: a car ejected as
// closed whose pull request is open again by the time the delivery reads it is
// neither landed nor superseded; the gate judges it again, and the train lands
// it on the second ride.
func TestTrainedDelivery_AReopenedPullRequestGoesBackToTheGate(t *testing.T) {
	rig, train := trainedRunnerRig(t)
	train.outcomes = []func(*TrainEntry){ejectClosed, landIt}
	job := rig.job(t)
	if err := rig.runner.Run(context.Background(), job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if job.Superseded() {
		t.Fatal("an open pull request was superseded")
	}
	if landed := rig.auditDetails(auditTrainLanded); len(landed) != 1 {
		t.Fatalf("train_landed audits = %v, want the second ride's landing", landed)
	}
	if train.nudges != 2 {
		t.Fatalf("nudges = %d, want two rides", train.nudges)
	}
}

// TestTrainedDelivery_AWaitingChangeWakesItsTrainAgain: the scheduler starts at
// most one train per project per formation interval, so the nudge that queued
// a change may start nothing. The delivery waiting on it nudges again on its
// next poll, and that nudge is the one the train answers.
func TestTrainedDelivery_AWaitingChangeWakesItsTrainAgain(t *testing.T) {
	rig, train := trainedRunnerRig(t)
	rig.workflow = &orchestration.WorkflowDefinition{Settings: orchestration.WorkflowSettings{PollInterval: "1ms"}}
	rateLimited := func(*TrainEntry) {}
	train.outcomes = []func(*TrainEntry){rateLimited, landIt}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := rig.runner.Run(ctx, rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if landed := rig.auditDetails(auditTrainLanded); len(landed) != 1 {
		t.Fatalf("train_landed audits = %v, want the landing the second nudge woke", landed)
	}
}
