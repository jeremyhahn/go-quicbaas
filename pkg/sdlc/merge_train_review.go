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
)

// =============================================================================
// validating, second half — the train's pull request is reviewed
// =============================================================================
//
// A GREEN PIPELINE IS NOT A REVIEW. Every change in the train was approved at
// its own head, but the head that lands is their union on the current target,
// which no reviewer has read. The target's protection asks for an APPROVE on
// the head that lands (handbook sdlc.md §5), so the train asks for one — from
// the project's counting reviewer profile, through the same review job every
// other pull request is judged by — and waits for it.
//
// THE FORGE IS THE RECORD OF THE VERDICT. The train keeps only the moment it
// asked (TrainProbe.ReviewRequestedAt), so a train resumed while it waits
// does not ask twice; the verdict itself is read from the forge on every
// pass, bound to the prefix's head, exactly as the review runner itself
// decides whether a head has been judged.
//
// WHAT A REFUSAL MEANS. The reviewer judged the combination, not any one
// change, and every change in it still carries its own approval. So a refused
// train is abandoned, every change it carried is told why on its own pull
// request, and each goes back to the queue in its place — bounded to a train
// half the size (TrainEntry.MaxTrain), so the combination is judged again in
// smaller parts. A train carrying ONE change is the exception, because there
// the findings can only be about that change: it is ejected to its worker with
// them, as a red pipeline ejects its culprit.
//
// A REVIEW THAT ENDS WITH NO VERDICT IS ASKED AGAIN, AND NOT FOR EVER. A
// review job can end without deciding — a reviewer that never declared, a
// session that died past its retries — and a train waiting on it would wait
// for ever. So each pass reads the recorded job's state as well as the
// verdict; a job that has ended with no verdict on the head is asked again,
// up to MaxTrainReviewAsks jobs per head, and past that the train is
// abandoned without ejecting anyone: nothing was found wrong with any change.

// TrainReviewJobs starts the review job that judges a train's pull request
// and reads whether it has ended. orchestration.AgentService satisfies it.
type TrainReviewJobs interface {
	JobStarter
	GetJobStatus(ctx context.Context, tenantID, jobID string) (*orchestration.JobStatus, error)
}

// review waits on the reviewer's verdict on the verified prefix's head, asking
// for the review once, and answers where the train goes next.
func (s *trainRun) review(ctx context.Context, t *MergeTrain) (orchestration.JobState, error) {
	probe := t.ProbeOf(t.Land)
	if probe == nil || probe.PullRequest == 0 {
		return "", &TrainLandingError{ProjectID: s.binding.ProjectID, Train: t.Number, Reason: "the verified prefix has no pull request to review"}
	}
	reviewer, ok := s.binding.TrainReviewer()
	if !ok {
		if err := s.abandon(ctx, s.binding.TrainOffReason()); err != nil {
			return "", err
		}
		return orchestration.JobStateCleanup, nil
	}
	// THE CULPRIT GOES BACK NOW, NOT AFTER THE REVIEW: CI has already decided
	// about it, and its worker should not wait on a verdict about changes it
	// is not part of. Ejecting is idempotent, so the landing's own ejection
	// of it finds this one done.
	if t.Eject > 0 {
		if err := s.ejectCulprit(ctx, t); err != nil {
			return "", err
		}
	}
	for {
		// THE JOB IS READ BEFORE THE VERDICT. A review posts its verdict
		// before its job ends, so a job seen ended here whose verdict is
		// still absent below ended without one — never a verdict in flight.
		ended, endedAs, err := s.reviewEnded(ctx, probe)
		if err != nil {
			return "", err
		}
		review, verdict, decided, err := s.trainVerdict(ctx, probe, reviewer)
		if err != nil {
			return "", err
		}
		if decided {
			if verdict == VerdictApprove {
				return s.approved(ctx, t, probe, reviewer)
			}
			return s.refused(ctx, t, probe, review)
		}
		switch {
		case probe.ReviewJobID == "":
			if err := s.requestReview(ctx, t, probe, reviewer); err != nil {
				return "", err
			}
		case ended && probe.ReviewAsks >= MaxTrainReviewAsks:
			return s.unreviewed(ctx, t, probe)
		case ended:
			previous := probe.ReviewJobID
			if err := s.requestReview(ctx, t, probe, reviewer); err != nil {
				return "", err
			}
			if probe.ReviewJobID != previous {
				s.job.Audit(auditTrainReasked, "train="+strconv.Itoa(t.Number)+" pull_request="+strconv.Itoa(probe.PullRequest)+
					" head="+probe.HeadSHA+" ask="+strconv.Itoa(probe.ReviewAsks)+" of="+strconv.Itoa(MaxTrainReviewAsks)+
					" previous_job="+previous+" previous_state="+endedAs)
			}
		}
		timer := time.NewTimer(s.runner.poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
}

// trainVerdict reads the train reviewer's standing decision on the prefix's
// head, through standingVerdict — the review runner's own definition of a
// head already judged, so the train and the reviewer cannot disagree about
// whether a verdict exists.
func (s *trainRun) trainVerdict(ctx context.Context, probe *TrainProbe, reviewer ReviewerProfile) (ForgeReview, ReviewVerdict, bool, error) {
	reviews, err := s.forge.ListReviews(ctx, s.repo, probe.PullRequest)
	if err != nil {
		return ForgeReview{}, "", false, err
	}
	review, verdict, found := standingVerdict(reviews, reviewer.Login, probe.HeadSHA)
	return review, verdict, found, nil
}

// reviewEnded reports whether the review job the train last started has
// ended, and in which state. No recorded job has not ended. A job the engine
// no longer knows has ended: nothing will ever post its verdict.
func (s *trainRun) reviewEnded(ctx context.Context, probe *TrainProbe) (bool, string, error) {
	if probe.ReviewJobID == "" {
		return false, "", nil
	}
	status, err := s.runner.jobs.GetJobStatus(orchestration.ContextWithTenant(ctx, s.job.TenantID), s.job.TenantID, probe.ReviewJobID)
	if err != nil {
		if errors.Is(err, orchestration.ErrJobNotFound) {
			return true, "not_found", nil
		}
		return false, "", backendError("GetJobStatus", err)
	}
	if status == nil {
		return true, "not_found", nil
	}
	return orchestration.IsTerminalJobState(status.State), string(status.State), nil
}

// requestReview starts the review job for the prefix's pull request and
// records that it did.
//
// A REVIEW ALREADY RUNNING IS NOT A FAILURE AND IS NOT RECORDED. The delivery
// that reported the train's pipeline green may have started one first, and
// the per-pull-request lease then refuses this start; the train asks again on
// its next pass, which finds either that review's verdict or the lease free.
// Only a start the engine accepted is recorded, so a restart never asks for a
// review the train already has in flight.
func (s *trainRun) requestReview(ctx context.Context, t *MergeTrain, probe *TrainProbe, reviewer ReviewerProfile) error {
	// THE SAME ACCOUNT CHECK A DISPATCHED REVIEW ASKS. A reviewer with no
	// usable account would only fail at spawn; the train holds instead, asks
	// again on its next pass, and ejects nobody: an account nobody can use is
	// the environment's fault, not any change's.
	if accounts, ok := s.runner.jobs.(ReviewerAccountAvailability); ok {
		reason, err := reviewerAccountHold(ctx, accounts, s.job.TenantID, s.binding, reviewer)
		if err != nil {
			return err
		}
		if reason != "" {
			return s.holdReview(ctx, t, probe, reviewer, reason)
		}
	}
	s.heldReason = ""
	jobID, err := startReviewJob(orchestration.ContextWithTenant(ctx, s.job.TenantID), s.runner.jobs,
		s.job.TenantID, s.binding, probe.PullRequest, reviewer)
	if err != nil {
		if jobAlreadyRunning(err) {
			return nil
		}
		return err
	}
	if jobID == "" {
		return nil
	}
	probe.ReviewRequestedAt = s.runner.now()
	probe.ReviewJobID = jobID
	probe.ReviewAsks++
	if err := s.saveLedger(ctx); err != nil {
		return err
	}
	s.job.Audit(auditTrainReview, "train="+strconv.Itoa(t.Number)+" pull_request="+strconv.Itoa(probe.PullRequest)+
		" head="+probe.HeadSHA+" profile="+reviewer.Name+" job="+jobID+" ask="+strconv.Itoa(probe.ReviewAsks))
	s.job.Emit(orchestration.JobStateValidating, "merge train #"+strconv.Itoa(t.Number)+": review of #"+
		strconv.Itoa(probe.PullRequest)+" at "+shortSHA(probe.HeadSHA)+" requested from "+reviewer.Name)
	return nil
}

// holdReview records that the train's review is held for reason: audited,
// emitted and posted on the pull request's head once per reason, with no job
// started and no ask counted.
func (s *trainRun) holdReview(ctx context.Context, t *MergeTrain, probe *TrainProbe, reviewer ReviewerProfile, reason string) error {
	if s.heldReason == reason {
		return nil
	}
	if err := postReviewHold(ctx, s.forge, s.repo, probe.HeadSHA, reviewer.Context(), reason); err != nil {
		return err
	}
	s.heldReason = reason
	s.job.Audit(auditTrainReviewHeld, "train="+strconv.Itoa(t.Number)+" pull_request="+strconv.Itoa(probe.PullRequest)+
		" head="+probe.HeadSHA+" profile="+reviewer.Name+" reason="+reason)
	s.job.Emit(orchestration.JobStateValidating, "merge train #"+strconv.Itoa(t.Number)+": review of #"+
		strconv.Itoa(probe.PullRequest)+" held: "+reason)
	return nil
}

// approved moves a train its reviewer approved on to landing.
func (s *trainRun) approved(ctx context.Context, t *MergeTrain, probe *TrainProbe, reviewer ReviewerProfile) (orchestration.JobState, error) {
	t.Phase = TrainLanding
	if err := s.saveLedger(ctx); err != nil {
		return "", err
	}
	s.job.Emit(orchestration.JobStateCompleting, "merge train #"+strconv.Itoa(t.Number)+": approved by "+reviewer.Name+
		" at "+shortSHA(probe.HeadSHA)+"; landing "+strconv.Itoa(t.Land)+" change(s)")
	return orchestration.JobStateCompleting, nil
}

// refused ends a train its reviewer requested changes on.
//
// IDEMPOTENT, because a train resumed while it waits re-reads the same
// refusal and runs this again: every comment is posted once by its marker,
// and every entry is written only while it still rides this train.
func (s *trainRun) refused(ctx context.Context, t *MergeTrain, probe *TrainProbe, review ForgeReview) (orchestration.JobState, error) {
	trainPR, err := s.forge.GetPullRequest(ctx, s.repo, probe.PullRequest)
	if err != nil {
		return "", err
	}
	if trainPR == nil {
		return "", &NotFoundError{Entity: "pull request", ID: s.repo.String() + "#" + strconv.Itoa(probe.PullRequest)}
	}
	findings := strings.TrimSpace(review.Body)
	reviewed := t.Cars[:t.Land]
	if len(reviewed) == 1 {
		car := reviewed[0]
		if err := s.eject(ctx, t, car, TrainEjection{
			Reason:   TrainEjectReviewRefused,
			Train:    t.Number,
			Detail:   "The review is on the train's pull request, " + trainLink(trainPR) + ", at `" + shortSHA(probe.HeadSHA) + "`.",
			Findings: findings,
		}); err != nil {
			return "", err
		}
	} else {
		bound := refusedTrainSize(len(reviewed))
		for _, car := range reviewed {
			if err := s.noteRefusal(ctx, t, trainPR, car, findings, bound); err != nil {
				return "", err
			}
			if err := s.updateEntry(ctx, t, car, func(entry *TrainEntry) {
				entry.State = TrainEntryQueued
				entry.Train = 0
				entry.MaxTrain = bound
			}); err != nil {
				return "", err
			}
		}
	}
	for _, car := range t.Cars[max(t.Land, t.Eject):] {
		if err := s.requeue(ctx, t, car); err != nil {
			return "", err
		}
	}
	s.job.Audit(auditTrainRefused, "train="+strconv.Itoa(t.Number)+" pull_request="+strconv.Itoa(probe.PullRequest)+
		" head="+probe.HeadSHA+" reviewer="+review.Reviewer+" cars="+carList(reviewed))
	if err := s.finish(ctx, "refused by its reviewer at "+shortSHA(probe.HeadSHA)); err != nil {
		return "", err
	}
	return orchestration.JobStateCleanup, nil
}

// unreviewed ends a train whose review produced no verdict after every ask.
//
// NOBODY IS EJECTED AND NOTHING IS BOUNDED: no change was found wrong, alone
// or in combination. Each change is told why its train ended and goes back to
// the queue in its place with its approval standing. Idempotent as refused
// is: each comment is posted once by its marker, and each entry written only
// while it still rides this train.
func (s *trainRun) unreviewed(ctx context.Context, t *MergeTrain, probe *TrainProbe) (orchestration.JobState, error) {
	trainPR, err := s.forge.GetPullRequest(ctx, s.repo, probe.PullRequest)
	if err != nil {
		return "", err
	}
	if trainPR == nil {
		return "", &NotFoundError{Entity: "pull request", ID: s.repo.String() + "#" + strconv.Itoa(probe.PullRequest)}
	}
	marker := trainUnreviewedMarker + " train=" + strconv.Itoa(t.Number)
	for _, car := range t.Cars[:t.Land] {
		body := trainUnreviewedComment(t, trainPR, car, probe.ReviewAsks)
		if err := s.commentOnce(ctx, car.PullRequest, marker, body); err != nil {
			return "", err
		}
		if err := s.requeue(ctx, t, car); err != nil {
			return "", err
		}
	}
	for _, car := range t.Cars[max(t.Land, t.Eject):] {
		if err := s.requeue(ctx, t, car); err != nil {
			return "", err
		}
	}
	s.job.Audit(auditTrainNoVerdict, "train="+strconv.Itoa(t.Number)+" pull_request="+strconv.Itoa(probe.PullRequest)+
		" head="+probe.HeadSHA+" asks="+strconv.Itoa(probe.ReviewAsks)+" last_job="+probe.ReviewJobID+" cars="+carList(t.Cars[:t.Land]))
	if err := s.finish(ctx, "abandoned: its review produced no verdict after "+strconv.Itoa(probe.ReviewAsks)+" review job(s)"); err != nil {
		return "", err
	}
	return orchestration.JobStateCleanup, nil
}

// noteRefusal posts the refused train's findings on one change's own pull
// request, once.
func (s *trainRun) noteRefusal(ctx context.Context, t *MergeTrain, trainPR *ForgePullRequest, car TrainCar, findings string, bound int) error {
	return s.commentOnce(ctx, car.PullRequest, trainRefusedMarker+" train="+strconv.Itoa(t.Number),
		trainRefusedComment(t, trainPR, car, findings, bound))
}

// commentOnce posts body on a pull request unless a comment carrying marker
// is already there.
func (s *trainRun) commentOnce(ctx context.Context, number int, marker, body string) error {
	comments, err := s.forge.ListIssueComments(ctx, s.repo, number)
	if err != nil {
		return err
	}
	for _, comment := range comments {
		if strings.Contains(comment.Body, marker) {
			return nil
		}
	}
	_, err = s.forge.CommentOnIssue(ctx, s.repo, number, body)
	return err
}

// ejectCulprit ejects the car the pipeline found turned the train red.
func (s *trainRun) ejectCulprit(ctx context.Context, t *MergeTrain) error {
	return s.eject(ctx, t, t.Cars[t.Eject-1], TrainEjection{
		Reason: TrainEjectChecksFailed,
		Train:  t.Number,
		Detail: "The train was green with the " + strconv.Itoa(t.Good) + " change(s) queued before this one and red with it added." +
			environmentalDetail(t, t.ProbeOf(t.Bad)),
		Findings: t.BadFindings,
	})
}
