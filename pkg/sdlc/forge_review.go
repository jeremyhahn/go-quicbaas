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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cifault"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// forge_review — the reviewing-phase validator
// =============================================================================
//
// The engine judges a job's published work through the workflow's
// reviewing-phase criteria. This validator IS the review for an SDLC
// workflow: it reads the forge's approvals and check runs on the pull
// request's CURRENT head, records them on the Delivery with one guarded
// write per round, and answers through the gate. A decision that only time
// can change — approvals not yet given, checks still running — is waited
// for, polling on the workflow's interval, so the job sits in reviewing
// until a person or a pipeline decides; a refusal (changes requested, a
// settled red) fails the criterion with the findings as feedback, and the
// engine re-delegates to the worker.

// ValidatorKindForgeReview is the criterion kind this validator registers.
const ValidatorKindForgeReview = "forge_review"

// DefaultReviewPollInterval paces the wait for a verdict when the workflow
// names no poll_interval.
const DefaultReviewPollInterval = 30 * time.Second

// deliveryWriteAttempts bounds the re-read on a guarded-write conflict.
const deliveryWriteAttempts = 3

// ReviewValidator is the forge_review validator.
type ReviewValidator struct {
	deliveries DeliveryStore
	bindings   BindingReader
	forges     ForgeResolver
	now        func() time.Time

	// logBytes is what a failed check's log is QUOTED at; classifyBytes is
	// what it is READ at. See DefaultChecksClassifyBytes for why they differ.
	logBytes      int
	classifyBytes int

	// rerunBudget bounds the automatic re-runs of one check on one head.
	rerunBudget int
}

// NewReviewValidator builds the validator.
func NewReviewValidator(deliveries DeliveryStore, bindings BindingReader, forges ForgeResolver) (*ReviewValidator, error) {
	switch {
	case deliveries == nil:
		return nil, &InvalidInputError{Field: "deliveries", Message: "the review is recorded on the delivery"}
	case bindings == nil:
		return nil, &InvalidInputError{Field: "bindings", Message: "the review policy lives on the binding"}
	case forges == nil:
		return nil, &InvalidInputError{Field: "forges", Message: "verdicts and checks are read from the forge"}
	}
	return &ReviewValidator{
		deliveries: deliveries, bindings: bindings, forges: forges,
		now:           func() time.Time { return time.Now().UTC() },
		logBytes:      DefaultChecksLogBytes,
		classifyBytes: DefaultChecksClassifyBytes,
		rerunBudget:   DefaultChecksRerunBudget,
	}, nil
}

// Validate implements orchestration.ValidatorFunc.
func (v *ReviewValidator) Validate(ctx context.Context, job *orchestration.JobContext, criterion orchestration.ReviewCriterion) (orchestration.ValidationResult, error) {
	failed := orchestration.ValidationResult{Name: criterion.Name, Passed: false}
	if job == nil || job.TaskID == "" {
		return failed, &InvalidInputError{Field: "job", Message: "forge_review judges a bound task and this job is bound to none"}
	}
	binding, err := v.bindings.GetBinding(ctx, job.TenantID, job.ProjectID)
	if err != nil {
		return failed, err
	}
	forge, err := v.forges.ForgeFor(ctx, job.TenantID, binding.Forge)
	if err != nil {
		return failed, err
	}
	if forge == nil {
		return failed, NewForgeNotRegisteredError(binding.Forge.Type)
	}
	interval := reviewPollInterval(job.Workflow)
	// The independence rule is decided against the model that WROTE the
	// code, and the binding is where that now lives: the canonical sdlc
	// workflow whose worker role once carried it is gone.
	authorFamily := ModelFamily(binding.Worker.Backend, binding.Worker.Model)

	for {
		result, decisive, err := v.round(ctx, job, criterion, binding, forge, authorFamily)
		if err != nil {
			return failed, err
		}
		if decisive {
			return result, nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return failed, ctx.Err()
		case <-timer.C:
		}
	}
}

// round reads the forge once, records what it saw, and answers the gate.
// decisive is false when only time can change the answer.
func (v *ReviewValidator) round(ctx context.Context, job *orchestration.JobContext, criterion orchestration.ReviewCriterion, binding *ProjectBinding, forge ForgeBackend, authorFamily string) (orchestration.ValidationResult, bool, error) {
	result := orchestration.ValidationResult{Name: criterion.Name}
	delivery, err := v.deliveries.GetDelivery(ctx, job.TenantID, job.TaskID)
	if err != nil {
		return result, false, err
	}
	if !delivery.PullRequest.Opened() {
		return result, false, &InvalidInputError{
			Field:   "delivery.pull_request",
			Message: "no pull request was published for task " + job.TaskID + "; Reviewable did not run",
		}
	}
	repo := binding.Repo()
	pr, err := forge.GetPullRequest(ctx, repo, delivery.PullRequest.Number)
	if err != nil {
		return result, false, err
	}
	if pr == nil {
		return result, false, &NotFoundError{Entity: "pull request", TenantID: job.TenantID, ID: delivery.Subject(repo)}
	}
	reviews, err := forge.ListReviews(ctx, repo, pr.Number)
	if err != nil {
		return result, false, err
	}
	var checks []ForgeCheckRun
	if binding.Review.RequireGreenChecks && pr.HeadSHA != "" {
		checks, err = forge.ListCheckRuns(ctx, repo, pr.HeadSHA)
		if err != nil {
			return result, false, err
		}
	}
	// THE DISPLACEMENT IS OBSERVED HERE, BEFORE THE WRITE THAT ERASES IT.
	// applyForgeState replaces delivery.Checks with the new head's, so the
	// previous head's runs exist in this process and nowhere else for exactly
	// as long as this call takes. Computed here rather than inside
	// applyForgeState because the cancel attempt needs the forge and
	// applyForgeState is deliberately a pure fold.
	superseded := supersedeChecks(ctx, forge, repo, delivery, pr.HeadSHA, v.now())
	delivery, err = v.record(ctx, job, binding, delivery, pr, reviews, checks, superseded)
	if err != nil {
		return result, false, err
	}

	decision := EvaluateReviewGate(GateInput{
		Policy:            binding.Review,
		PullRequestState:  delivery.PullRequest.State,
		HeadSHA:           delivery.HeadSHA,
		Reviews:           delivery.Reviews,
		Checks:            delivery.Checks,
		AuthorModelFamily: authorFamily,
		Draft:             delivery.PullRequest.Draft,
	})
	result.Details = decision.Reason.String()
	switch {
	case decision.Mergeable():
		result.Passed = true
		result.Message = "approved on " + decision.HeadSHA + " by " + itoa(decision.Approvals) + " independent reviewer(s)"
		return result, true, nil
	case decision.Reason == GateBlockedChangesRequested:
		result.Message = findingsFor(delivery.Reviews, delivery.HeadSHA)
		return result, true, nil
	case decision.Reason == GateBlockedChecksFailed:
		return v.checksFailed(ctx, job, binding, forge, repo, delivery, checks, result)
	case decision.Reason == GateBlockedPullRequestNotOpen:
		if delivery.PullRequest.State == PullRequestMerged {
			result.Passed = true
			result.Message = "pull request already merged"
			return result, true, nil
		}
		// CLOSED WITHOUT MERGING ENDS THE DELIVERY; IT IS NOT A REFUSAL.
		// Answered decisive-and-failed, it went back to the worker for
		// another round and then to the owner as a question, for a pull
		// request nobody will merge (go-quicbaas#1054). The runner concludes
		// the attempt `superseded` on this error.
		if delivery.PullRequest.State == PullRequestClosed {
			return result, false, &PullRequestClosedUnmergedError{TaskID: job.TaskID, Number: delivery.PullRequest.Number}
		}
		result.Message = decision.Detail
		return result, true, nil
	case decision.Reason == GateBlockedPolicyInvalid, decision.Reason == GateBlockedAuthorUnknown, decision.Reason == GateBlockedHeadUnknown:
		return result, false, &InvalidInputError{Field: "review", Message: decision.Detail}
	default:
		// Pending: approvals or checks that have not arrived.
		result.Message = decision.Detail
		return result, false, nil
	}
}

// checksFailed decides what a settled red pipeline MEANS, and therefore who
// acts on it. It is the whole point of go-quicbaas#898.
//
// Before this existed, a settled red was returned decisive and the job went to
// re-delegation: a coding agent was paid to author another attempt at a change
// whose tests had never run. The three answers now differ:
//
//   - genuine — the change's own failure. Decisive, with the findings as
//     feedback, exactly as before.
//   - void — not a verdict on the change. The check is re-run and the round is
//     NOT decisive, so the job stays in reviewing and waits for the new
//     result. The agent is never told.
//   - host defect — the machine. A CIHostDefectError, which the SDLC runner
//     parks the task with so a person sees it on the issue.
//
// A failure that has not settled is still waited out: the settle window is not
// replaced by the classifier, it is the thing that stops a red being read
// while a rerun is in flight.
func (v *ReviewValidator) checksFailed(
	ctx context.Context,
	job *orchestration.JobContext,
	binding *ProjectBinding,
	forge ForgeBackend,
	repo RepoRef,
	delivery *Delivery,
	checks []ForgeCheckRun,
	result orchestration.ValidationResult,
) (orchestration.ValidationResult, bool, error) {
	failure, settled := evaluateChecksFailure(delivery.Checks, delivery.HeadSHA, v.now(), binding.Review.SettleWindow())
	if !settled {
		return result, false, nil
	}
	logs := checksFailureLogs(ctx, forge, repo, failure, checks, v.classifyBytes)
	// The project's own forge owner is the private-module owner: the other
	// repositories of the same owner are what this project's go.mod points at
	// and what only a provisioned host can resolve.
	triage := TriageChecksFailure(failure, logs, []string{repo.Owner})

	switch verdict := triage.Aggregate(); verdict {
	case cifault.VerdictGenuine:
		result.Message = failure.Findings(triage, logs, v.logBytes)
		return result, true, nil
	case cifault.VerdictHostDefect:
		return result, false, &CIHostDefectError{
			HeadSHA: delivery.HeadSHA,
			Reason:  triage.Account(delivery.HeadSHA),
		}
	default:
		return v.rerunVoid(ctx, job, forge, repo, delivery, checks, triage, result)
	}
}

// rerunVoid asks the forge to run the void checks again, bounded, and reports
// the round as not decisive so the job waits for the answer.
//
// IT ESCALATES RATHER THAN LOOPING, in all three ways the ask can fail to be
// worth making again:
//
//   - the budget is spent, so this host has already been asked and answered
//     the same thing;
//   - the forge cannot re-run at all, so waiting would be waiting for
//     something nobody will ever trigger;
//   - the re-run call itself was refused, which still spends budget so a
//     broken route reaches an operator instead of being retried forever.
//
// A check whose re-run is already PENDING — asked for since this result was
// recorded — is not asked again; the forge has been told and the round simply
// waits.
func (v *ReviewValidator) rerunVoid(
	ctx context.Context,
	job *orchestration.JobContext,
	forge ForgeBackend,
	repo RepoRef,
	delivery *Delivery,
	checks []ForgeCheckRun,
	triage ChecksTriage,
	result orchestration.ValidationResult,
) (orchestration.ValidationResult, bool, error) {
	void := triage.Of(cifault.VerdictVoid)
	if !forge.Capabilities().RerunCheckRuns {
		return result, false, &CIHostDefectError{
			HeadSHA: delivery.HeadSHA,
			Reason: rerunUnsupported(void, delivery.HeadSHA,
				string(forge.Type())+" cannot re-run a finished check, so the engine cannot answer an "+
					"infrastructure fault by itself"),
		}
	}
	byName := make(map[string]ForgeCheckRun, len(checks))
	for _, check := range checks {
		byName[check.Name] = check
	}

	asked := 0
	for _, entry := range void {
		if delivery.RerunPending(entry.Check) {
			continue
		}
		if attempts := delivery.RerunAttempts(entry.Check.HeadSHA, entry.Check.Name); attempts >= v.rerunBudget {
			return result, false, &CIHostDefectError{
				HeadSHA: delivery.HeadSHA,
				Reason:  rerunBudgetSpent(void, delivery.HeadSHA, attempts),
			}
		}
		// A context the forge no longer lists yields the zero run, whose empty
		// target URL every adapter refuses. That is the right outcome and it
		// needs no special case here: the refusal is recorded below, spends
		// budget like any other, and escalates once the budget is gone.
		record := CIRerunRecord{
			HeadSHA:    entry.Check.HeadSHA,
			Check:      entry.Check.Name,
			ForgeRunID: entry.Check.ForgeRunID,
			Signature:  entry.Finding.Signature.String(),
			At:         v.now(),
		}
		if err := forge.RerunCheckRun(ctx, repo, byName[entry.Check.Name]); err != nil {
			record.Failure = publicOutput(err.Error())
		}
		delivery.RecordRerun(record)
		asked++
	}
	if asked > 0 {
		if err := v.persistReruns(ctx, job, delivery); err != nil {
			return result, false, err
		}
	}

	result.Message = "continuous integration failed for reasons that are not this change's; " +
		strconv.Itoa(len(void)) + " check(s) are being re-run automatically"
	return result, false, nil
}

// persistReruns writes the re-run records with one guarded write, re-reading
// and re-applying on a conflict.
//
// THE BUDGET IS ONLY A BOUND WHILE IT IS DURABLE. A record kept in the
// validator's memory is lost when the daemon restarts mid-wait, and the next
// round would start the count again — which is an unbounded loop with extra
// steps.
func (v *ReviewValidator) persistReruns(ctx context.Context, job *orchestration.JobContext, delivery *Delivery) error {
	pending := append([]CIRerunRecord(nil), delivery.CIReruns...)
	for attempt := 1; ; attempt++ {
		err := v.deliveries.UpdateDelivery(ctx, job.TenantID, delivery)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrVersionConflict) || attempt >= deliveryWriteAttempts {
			return err
		}
		fresh, readErr := v.deliveries.GetDelivery(ctx, job.TenantID, job.TaskID)
		if readErr != nil {
			return readErr
		}
		fresh.CIReruns = mergeRerunRecords(fresh.CIReruns, pending)
		delivery = fresh
	}
}

// mergeRerunRecords folds this round's records into whatever the store now
// holds, keeping every attempt exactly once.
//
// APPEND-ONLY AND DE-DUPLICATED, because the budget is a COUNT: a conflict
// that re-applied a record already written would spend the allowance twice and
// escalate a fleet that is fine, while one that dropped a record would never
// spend it at all.
func mergeRerunRecords(held, pending []CIRerunRecord) []CIRerunRecord {
	seen := make(map[CIRerunRecord]struct{}, len(held))
	for _, record := range held {
		seen[record] = struct{}{}
	}
	for _, record := range pending {
		if _, already := seen[record]; already {
			continue
		}
		seen[record] = struct{}{}
		held = append(held, record)
	}
	return held
}

// record merges what the forge reported into the delivery with one guarded
// write. A conflict re-reads and merges again, bounded; a review already
// recorded is never recorded twice, so a lost race records nothing extra.
func (v *ReviewValidator) record(ctx context.Context, job *orchestration.JobContext, binding *ProjectBinding, delivery *Delivery, pr *ForgePullRequest, reviews []ForgeReview, checks []ForgeCheckRun, superseded []SupersessionRecord) (*Delivery, error) {
	for attempt := 1; ; attempt++ {
		changed := applyForgeState(delivery, binding, pr, reviews, checks, superseded, job.Iteration, v.now())
		if !changed {
			return delivery, nil
		}
		err := v.deliveries.UpdateDelivery(ctx, job.TenantID, delivery)
		if err == nil {
			return delivery, nil
		}
		if !errors.Is(err, ErrVersionConflict) || attempt >= deliveryWriteAttempts {
			return nil, err
		}
		delivery, err = v.deliveries.GetDelivery(ctx, job.TenantID, job.TaskID)
		if err != nil {
			return nil, err
		}
	}
}

// applyForgeState folds the forge's view into the delivery and reports
// whether anything changed.
func applyForgeState(delivery *Delivery, binding *ProjectBinding, pr *ForgePullRequest, reviews []ForgeReview, checks []ForgeCheckRun, superseded []SupersessionRecord, round int, now time.Time) bool {
	changed := false
	if state := pullRequestStateFor(pr); state != delivery.PullRequest.State {
		delivery.PullRequest.State = state
		changed = true
	}
	if pr.Draft != delivery.PullRequest.Draft {
		delivery.PullRequest.Draft = pr.Draft
		changed = true
	}
	if pr.URL != "" && pr.URL != delivery.PullRequest.URL {
		delivery.PullRequest.URL = pr.URL
		changed = true
	}
	if pr.HeadSHA != "" && pr.HeadSHA != delivery.HeadSHA {
		// RECORDED BEFORE THE HEAD MOVES, so a re-read after a version
		// conflict re-enters this branch against the same stored head and
		// RecordSupersession deduplicates rather than doubling. Its answer is
		// not consulted: the head move on the next line is itself the change
		// this write exists for.
		delivery.RecordSupersession(superseded)
		delivery.HeadSHA = pr.HeadSHA
		changed = true
	}
	for _, review := range reviews {
		record, acted := reviewRecordFor(review, binding.Review, round)
		if !acted || delivery.ReviewRecorded(record) {
			continue
		}
		delivery.Reviews = append(delivery.Reviews, record)
		changed = true
	}
	if len(checks) > 0 {
		next := checkRecordsFor(checks, delivery.HeadSHA, now)
		if !sameChecks(delivery.Checks, next) {
			delivery.Checks = next
			changed = true
		}
	}
	return changed
}

// pullRequestStateFor maps the forge's view of a pull request to a state.
func pullRequestStateFor(pr *ForgePullRequest) PullRequestState {
	switch {
	case pr.Merged:
		return PullRequestMerged
	case pr.State == "closed":
		return PullRequestClosed
	default:
		return PullRequestOpen
	}
}

func sameChecks(a, b []CheckRecord) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].HeadSHA != b[i].HeadSHA || a[i].Status != b[i].Status ||
			a[i].ForgeRunID != b[i].ForgeRunID || !a[i].FinishedAt.Equal(b[i].FinishedAt) {
			return false
		}
	}
	return true
}

// reviewPollInterval is the workflow's poll_interval or the default.
func reviewPollInterval(workflow *orchestration.WorkflowDefinition) time.Duration {
	if workflow == nil || workflow.Settings.PollInterval == "" {
		return DefaultReviewPollInterval
	}
	d, err := time.ParseDuration(workflow.Settings.PollInterval)
	if err != nil || d <= 0 {
		return DefaultReviewPollInterval
	}
	return d
}

func itoa(n int) string { return strconv.Itoa(n) }
