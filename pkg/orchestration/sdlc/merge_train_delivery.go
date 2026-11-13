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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// =============================================================================
// A delivery rides the train
// =============================================================================
//
// THE DELIVERY'S SIDE OF THE TRAIN IS A WAIT, AND IT IS SPENT IN REVIEWING.
// Once the gate approves a head, a trained project queues the change instead of
// merging it and the job stays where it already was — in reviewing, off the
// project's worker budget, with its worker still there — until the train says
// what happened to it:
//
//	landed    completing: Complete records the landing and closes the change's
//	          own pull request, which the train carried to the target
//	ejected   checks_failed: a round for the worker, with the train's red
//	          conflict: a merge round, exactly as a conflicted merge is
//	          anything else: back to reviewing, where the gate judges the pull
//	          request as it is now
//
// Nothing here merges anything. The entry is the module's own record and the
// wait reads it; the train runner is the only thing that writes a landing.

// Audit events a delivery records about its ride.
const (
	auditTrainQueued  = "sdlc:train_queued"
	auditTrainLanded  = "sdlc:train_landed"
	auditTrainEjected = "sdlc:train_ejected"

	// auditTrainMergedOutside records a waiting change whose pull request was
	// merged by some path other than the train.
	auditTrainMergedOutside = "sdlc:train_merged_outside"

	// auditTrainClosedOutside records a waiting change whose pull request was
	// closed without merging by some path other than the train.
	auditTrainClosedOutside = "sdlc:train_closed_outside"

	// auditTrainOff records why an approved change merges on its own, or why
	// a train job formed nothing: the reason TrainOffReason gives.
	auditTrainOff = "sdlc:train_off"
)

// TrainNudger wakes a project's merge train. *TrainScheduler satisfies it.
type TrainNudger interface {
	NudgeTrain(ctx context.Context, tenantID, projectID string) error
}

// trainEntryWriteAttempts bounds the re-read on a guarded-write conflict.
const trainEntryWriteAttempts = 3

// enqueueForTrain puts the delivery's approved head in its project's queue and
// wakes the train.
//
// IDEMPOTENT ON THE HEAD. An entry already carrying this head — waiting,
// riding or landed — is left as it is: a resumed job re-approving the same head
// must not send it to the back of the queue, and must never pull it out of a
// train already testing it. A new head, or an entry the train ejected,
// replaces the entry and joins the back of the queue.
func (h *Hooks) enqueueForTrain(ctx context.Context, job *orchestration.JobContext, a *attempt, delivery *Delivery) (*TrainEntry, error) {
	if h.trains == nil {
		return nil, &TrainUnavailableError{ProjectID: job.ProjectID}
	}
	for attempt := 1; ; attempt++ {
		entry, err := h.enqueueOnce(ctx, job, a, delivery)
		if err == nil {
			if nudgeErr := h.trainNudger.NudgeTrain(ctx, job.TenantID, job.ProjectID); nudgeErr != nil {
				return nil, nudgeErr
			}
			return entry, nil
		}
		if !errors.Is(err, ErrVersionConflict) || attempt >= trainEntryWriteAttempts {
			return nil, err
		}
	}
}

// enqueueOnce is one read-then-guarded-write of the entry.
func (h *Hooks) enqueueOnce(ctx context.Context, job *orchestration.JobContext, a *attempt, delivery *Delivery) (*TrainEntry, error) {
	held, err := h.trains.GetTrainEntry(ctx, job.TenantID, job.ProjectID, job.TaskID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if held != nil && held.HeadSHA == delivery.HeadSHA && held.PullRequest == delivery.PullRequest.Number &&
		held.State != TrainEntryEjected {
		return held, nil
	}
	title := strings.TrimSpace(delivery.PullRequest.Title)
	if title == "" {
		title = strings.TrimSpace(a.task.Subject)
	}
	entry := &TrainEntry{
		ProjectID:   job.ProjectID,
		TaskID:      job.TaskID,
		PullRequest: delivery.PullRequest.Number,
		Title:       title,
		Branch:      delivery.Branch,
		Target:      delivery.Target,
		HeadSHA:     delivery.HeadSHA,
		Closes:      a.closes(),
		State:       TrainEntryQueued,
		EnqueuedAt:  h.now(),
	}
	if held != nil {
		entry.Version = held.Version
		entry.CreatedAt = held.CreatedAt
	}
	if err := h.trains.SaveTrainEntry(ctx, job.TenantID, entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// trainLanding returns the entry recording that THIS delivery's approved head
// landed inside a train, or nil when it did not.
//
// ALL THREE MUST MATCH — landed, the same pull request, the same head — or the
// answer is nil and Complete merges as it always has: a landing recorded for a
// head the delivery has since moved past is not a landing of this change.
func (h *Hooks) trainLanding(ctx context.Context, job *orchestration.JobContext, delivery *Delivery) (*TrainEntry, error) {
	// NOT GATED ON THE BINDING: a change a train landed has landed whether or
	// not the project trains today, and merging it again would be the forge
	// asked to land the same change twice.
	if h.trains == nil {
		return nil, nil
	}
	entry, err := h.trains.GetTrainEntry(ctx, job.TenantID, job.ProjectID, job.TaskID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if entry.State != TrainEntryLanded || entry.HeadSHA != delivery.HeadSHA ||
		entry.PullRequest != delivery.PullRequest.Number {
		return nil, nil
	}
	return entry, nil
}

// closeLanded closes the change's own pull request, saying where its change
// landed. The comment goes first and is posted once: a retried Complete finds
// its own marker and does not post it again.
func (h *Hooks) closeLanded(ctx context.Context, a *attempt, pr *ForgePullRequest, entry *TrainEntry) error {
	if pullRequestStateFor(pr) != PullRequestOpen {
		return nil
	}
	comments, err := a.forge.ListPullRequestComments(ctx, a.repo, pr.Number)
	if err != nil {
		return err
	}
	marker := trainLandedMarker + " train=" + strconv.Itoa(entry.Train)
	posted := false
	for _, comment := range comments {
		if strings.Contains(comment.Body, marker) {
			posted = true
			break
		}
	}
	if !posted {
		if _, err := a.forge.CommentOnPullRequest(ctx, a.repo, pr.Number, trainLandedComment(entry)); err != nil {
			return err
		}
	}
	return a.forge.ClosePullRequest(ctx, a.repo, pr.Number)
}

// deleteLandedBranch removes the source branch of a change a train landed,
// when the branch plan said it goes once landed.
//
// THE FORGE WILL NOT DO IT HERE. A change merged on its own asks the forge to
// delete its branch as part of the merge; a train car's own pull request is
// CLOSED, not merged, so that never fires and the branch outlives the change.
// A branch left behind is not only clutter: the next task cut for the same
// issue plans the same name from main, and its push is refused as
// non-fast-forward after a whole paid worker turn (go-luks2#92, 2026-09-28,
// the stale branch of the landed PR #216).
//
// It runs whether or not closeLanded found the pull request still open, so a
// retried Complete finishes a delete the first attempt could not; a branch
// already gone is the goal, not an error.
func (h *Hooks) deleteLandedBranch(ctx context.Context, a *attempt, delivery *Delivery) error {
	branch := strings.TrimSpace(delivery.Branch)
	if !delivery.DeleteAfterMerge || branch == "" {
		return nil
	}
	if err := a.forge.DeleteBranch(ctx, a.repo, branch); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}

// rideTrain queues the approved head and waits in reviewing for the train's
// answer.
//
// THE WAIT POLLS THE ENTRY, NOT THE FORGE. The entry is the module's record of
// the train's decision, written by the only thing that decides it; a forge
// read here could only be a hint of what the train already recorded.
func (s *runnerRun) rideTrain(ctx context.Context) (orchestration.JobState, error) {
	if err := s.loadDelivery(ctx); err != nil {
		return "", err
	}
	// A PULL REQUEST THE GATE FOUND ALREADY MERGED IS NEVER QUEUED. The gate
	// passes a merged pull request, and queueing it anyway woke a train only
	// for that train to eject the change as closed and send it back here:
	// go-luks2 PR #210, merged by hand, rode trains #79 to #152 at one every
	// thirty seconds (X15, 2026-09-25).
	if s.delivery.PullRequest.State == PullRequestMerged {
		return s.landedOutsideTheTrain(ctx, s.delivery.PullRequest.Number, "approved")
	}
	queued, err := s.runner.hooks.enqueueForTrain(ctx, s.job.Context(), s.attempt, s.delivery)
	if err != nil {
		return "", err
	}
	s.job.Audit(auditTrainQueued, "pull_request="+strconv.Itoa(queued.PullRequest)+" head="+queued.HeadSHA+
		" target="+queued.Target+" state="+string(queued.State))
	s.job.Emit(orchestration.JobStateReviewing, "approved; queued for the merge train onto "+queued.Target)

	interval := reviewPollInterval(s.job.Context().Workflow)
	seen, seenTrain := queued.State, queued.Train
	for {
		// READ BEFORE WAITING: the nudge that queued the change may already
		// have run a train to its end.
		entry, err := s.trainEntry(ctx)
		if err != nil {
			return "", err
		}
		if entry.State == TrainEntryQueued {
			settled, err := s.settledOutsideTheTrain(ctx, entry)
			if settled != "" || err != nil {
				return settled, err
			}
		}
		switch entry.State {
		case TrainEntryLanded:
			s.job.Audit(auditTrainLanded, "train="+strconv.Itoa(entry.Train)+" merge="+entry.MergeSHA+
				" train_pull_request="+strconv.Itoa(entry.TrainPullRequest))
			s.job.Emit(orchestration.JobStateCompleting, "landed on "+entry.Target+" by merge train #"+strconv.Itoa(entry.Train))
			return orchestration.JobStateCompleting, nil
		case TrainEntryEjected:
			return s.trainEjected(ctx, entry)
		}
		withdrawn, err := s.trainWithdrawn(ctx, entry)
		if err != nil {
			return "", err
		}
		if withdrawn {
			s.job.Emit(orchestration.JobStateCompleting, "the project's merge train was turned off; the change merges on its own")
			return orchestration.JobStateCompleting, nil
		}
		if entry.State != seen || entry.Train != seenTrain {
			seen, seenTrain = entry.State, entry.Train
			s.job.Emit(orchestration.JobStateReviewing, "merge train: "+string(entry.State)+trainSuffix(entry.Train))
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
		// A CHANGE STILL WAITING WAKES ITS TRAIN AGAIN. The scheduler starts at
		// most one train per project per formation interval, so the nudge that
		// queued this change may have started nothing; this one, a poll later,
		// is the retry, and it costs nothing while a train is running.
		if entry.State == TrainEntryQueued {
			if err := s.runner.hooks.trainNudger.NudgeTrain(ctx, s.job.TenantID, s.job.Config.ProjectID); err != nil {
				return "", err
			}
		}
	}
}

// settledOutsideTheTrain answers a change whose pull request was merged or
// closed by some other path — a person, the forge's own button — while its
// entry waits, and answers the empty state while the pull request is open.
//
// THE ONE FORGE READ IN THE WAIT, and only for an entry no train is carrying.
// The entry is still the record of the TRAIN's decision; what it cannot record
// is a decision made outside the train, and a delivery that waited for the
// train to answer for a change that had already landed waited for ever
// (go-luks2 PR #210, 2026-09-25: merged by hand, the job still `reviewing` and
// the issue still `in-review`). A riding entry is left to its train, which
// reads every car before it lands and ejects a pull request that is no longer
// open; the ejection is then answered by trainEjected, the same way.
func (s *runnerRun) settledOutsideTheTrain(ctx context.Context, entry *TrainEntry) (orchestration.JobState, error) {
	pr, err := s.attempt.forge.GetPullRequest(ctx, s.attempt.repo, entry.PullRequest)
	if err != nil {
		return "", err
	}
	if pr == nil {
		return "", nil
	}
	switch pullRequestStateFor(pr) {
	case PullRequestMerged:
		return s.landedOutsideTheTrain(ctx, pr.Number, string(entry.State)+" head="+pr.HeadSHA)
	case PullRequestClosed:
		return s.closedOutsideTheTrain(ctx, pr.Number, entry)
	default:
		return "", nil
	}
}

// landedOutsideTheTrain takes the change out of the queue for good and sends
// the job to completing, where Complete records the landing without merging
// again.
func (s *runnerRun) landedOutsideTheTrain(ctx context.Context, number int, detail string) (orchestration.JobState, error) {
	if err := s.dequeue(ctx); err != nil {
		return "", err
	}
	s.job.Audit(auditTrainMergedOutside, "pull_request="+strconv.Itoa(number)+" state="+detail)
	s.job.Emit(orchestration.JobStateCompleting,
		"pull request #"+strconv.Itoa(number)+" was merged outside the merge train; the change has landed")
	return orchestration.JobStateCompleting, nil
}

// closedOutsideTheTrain takes the change out of the queue for good and ends
// the attempt as superseded: somebody closed the pull request on purpose, so
// no train and no worker round can change it (go-quicbaas#1054).
func (s *runnerRun) closedOutsideTheTrain(ctx context.Context, number int, entry *TrainEntry) (orchestration.JobState, error) {
	if err := s.dequeue(ctx); err != nil {
		return "", err
	}
	s.job.Audit(auditTrainClosedOutside, "pull_request="+strconv.Itoa(number)+" state="+string(entry.State)+
		" train="+strconv.Itoa(entry.Train)+" head="+entry.HeadSHA)
	return s.supersede(number), nil
}

// dequeue removes this delivery's entry from its project's queue.
func (s *runnerRun) dequeue(ctx context.Context) error {
	return s.runner.hooks.trains.DeleteTrainEntry(ctx, s.job.TenantID, s.job.Config.ProjectID, s.taskID())
}

// trainEntry reads this delivery's queue entry as the train last wrote it.
//
// AN ENTRY THAT IS GONE while the change still waits was cleared by an
// operator. The approval still stands, so the change is queued again rather
// than left waiting on nothing. An entry for another head means this job no
// longer owns what it is waiting on.
func (s *runnerRun) trainEntry(ctx context.Context) (*TrainEntry, error) {
	hooks := s.runner.hooks
	entry, err := hooks.trains.GetTrainEntry(ctx, s.job.TenantID, s.job.Config.ProjectID, s.taskID())
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if entry, err = hooks.enqueueForTrain(ctx, s.job.Context(), s.attempt, s.delivery); err != nil {
			return nil, err
		}
	}
	if entry.HeadSHA != s.delivery.HeadSHA {
		return nil, &HeadMovedError{
			Subject: s.delivery.Subject(s.attempt.repo), Reviewed: s.delivery.HeadSHA, Current: entry.HeadSHA,
		}
	}
	return entry, nil
}

// trainWithdrawn takes a still-queued change back out of the queue when the
// project's train has been turned off since it was queued, and reports whether
// it did.
//
// A CHANGE THAT IS ALREADY RIDING IS LEFT TO ITS TRAIN, which finishes what it
// started. A queued one would otherwise wait for ever: no train will be formed
// for a project whose train is off.
func (s *runnerRun) trainWithdrawn(ctx context.Context, entry *TrainEntry) (bool, error) {
	if entry.State != TrainEntryQueued {
		return false, nil
	}
	binding, err := s.runner.hooks.bindings.GetBinding(ctx, s.job.TenantID, s.job.Config.ProjectID)
	if err != nil {
		return false, err
	}
	if binding.TrainEnabled() {
		return false, nil
	}
	s.attempt.binding = binding
	if err := s.runner.hooks.trains.DeleteTrainEntry(ctx, s.job.TenantID, s.job.Config.ProjectID, s.taskID()); err != nil {
		return false, err
	}
	s.job.Audit(auditTrainQueued, "withdrawn=true reason=the project's merge train is off")
	return true, nil
}

// trainSuffix renders " in train #n" for a riding entry.
func trainSuffix(train int) string {
	if train <= 0 {
		return ""
	}
	return " in train #" + strconv.Itoa(train)
}

// trainEjected answers an ejection: a round for the worker when the change is
// what failed or what its train's reviewer refused, a merge round when it no
// longer merges, and a fresh look by the gate when the pull request itself
// changed under the train.
func (s *runnerRun) trainEjected(ctx context.Context, entry *TrainEntry) (orchestration.JobState, error) {
	ejection := entry.Ejection
	if ejection == nil {
		ejection = &TrainEjection{Reason: TrainEjectClosed, Train: entry.Train}
	}
	s.job.Audit(auditTrainEjected, "train="+strconv.Itoa(ejection.Train)+" reason="+string(ejection.Reason)+
		" head="+entry.HeadSHA+" detail="+ejection.Detail)
	// A PULL REQUEST NO LONGER OPEN IS ANSWERED HERE, ONCE, whatever the train
	// ejected it for, and the entry leaves the queue for good. Sent back to the
	// gate instead, a merged pull request passed it, the approval queued it
	// again, and the next train ejected it again: go-luks2 PR #210 rode trains
	// #79 to #152 that way (X15, 2026-09-25).
	settled, err := s.settledOutsideTheTrain(ctx, entry)
	if settled != "" || err != nil {
		return settled, err
	}
	set := jobPrompts(s.job)
	switch ejection.Reason {
	case TrainEjectClosed:
		// Only a pull request reopened since the train read it reaches this.
		s.job.Emit(orchestration.JobStateReviewing,
			"ejected from merge train #"+strconv.Itoa(ejection.Train)+" (closed), but the pull request is open again; the gate judges it again")
		return orchestration.JobStateReviewing, nil
	case TrainEjectChecksFailed:
		s.feedback = set.MustRender("train_ejected", prompts.Vars{
			"train":    strconv.Itoa(ejection.Train),
			"target":   entry.Target,
			"findings": strings.TrimSpace(ejection.Findings),
		})
		s.job.Memory().AddFeedback(s.feedback)
		return s.nextRound(ctx, "ejected from merge train #"+strconv.Itoa(ejection.Train))
	case TrainEjectReviewRefused:
		s.feedback = set.MustRender("train_review_refused", prompts.Vars{
			"train":    strconv.Itoa(ejection.Train),
			"target":   entry.Target,
			"findings": strings.TrimSpace(ejection.Findings),
		})
		s.job.Memory().AddFeedback(s.feedback)
		return s.nextRound(ctx, "merge train #"+strconv.Itoa(ejection.Train)+" was refused by its reviewer")
	case TrainEjectUnsigned:
		// NOT BACK TO THE GATE: the gate would approve the same head and the
		// next train would eject it again. Only a person can change the
		// answer, by trusting the key or by replacing the commit.
		return s.park(orchestration.TaskBlockKindEscalated,
			"Merge train #"+strconv.Itoa(ejection.Train)+" could not carry this pull request: "+ejection.Detail+
				" The factory never rewrites a pushed branch. Record the signing key in the project's trusted signing "+
				"history if it was the project's, or replace the commit, then readmit the task.",
			orchestration.FaultUntrustedSigner), nil
	case TrainEjectConflict:
		return s.mergeRound(ctx, &MergeConflictError{
			Subject:  s.delivery.Subject(s.attempt.repo),
			Base:     entry.Target,
			Head:     entry.HeadSHA,
			Attempts: 1,
		})
	default:
		s.job.Emit(orchestration.JobStateReviewing,
			"ejected from merge train #"+strconv.Itoa(ejection.Train)+" ("+string(ejection.Reason)+"); the gate judges the pull request again")
		return orchestration.JobStateReviewing, nil
	}
}
