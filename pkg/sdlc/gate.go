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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cifault"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// =============================================================================
// The review gate — pure functions over a Delivery
// =============================================================================
//
// A pull request may land when the CURRENT head carries the required number
// of independent approvals, no reviewer has requested changes on it, and,
// when the policy asks, every check on it finished green. Every input is a
// record on the Delivery; nothing here talks to a forge or a store, so the
// decision is a function and is tested as one.

// GateBlockReason says why a pull request may not land, or GateClear.
type GateBlockReason string

const (
	// GateClear is a pull request that may land.
	GateClear GateBlockReason = "clear"

	// GateBlockedPolicyInvalid is a policy that would merge unreviewed code.
	GateBlockedPolicyInvalid GateBlockReason = "policy_invalid"

	// GateBlockedPullRequestNotOpen is a merged or closed pull request.
	GateBlockedPullRequestNotOpen GateBlockReason = "pull_request_not_open"

	// GateBlockedPullRequestDraft is an author saying the work is unfinished.
	GateBlockedPullRequestDraft GateBlockReason = "pull_request_draft"

	// GateBlockedHeadUnknown is a delivery with no head to bind a verdict to.
	GateBlockedHeadUnknown GateBlockReason = "head_unknown"

	// GateBlockedReviewErrored is a review that could not be produced.
	GateBlockedReviewErrored GateBlockReason = "review_errored"

	// GateBlockedChangesRequested is a reviewer's refusal of the head.
	GateBlockedChangesRequested GateBlockReason = "changes_requested"

	// GateBlockedAuthorUnknown is an independence rule with no author family.
	GateBlockedAuthorUnknown GateBlockReason = "author_unknown"

	// GateBlockedReviewerNotIndependent is an approval from the author's family.
	GateBlockedReviewerNotIndependent GateBlockReason = "reviewer_not_independent"

	// GateBlockedInsufficientApprovals is too few approvals on the head.
	GateBlockedInsufficientApprovals GateBlockReason = "insufficient_approvals"

	// GateBlockedNoChecks is a required-green policy with no check on the head.
	GateBlockedNoChecks GateBlockReason = "no_checks"

	// GateBlockedChecksIncomplete is a check still running on the head.
	GateBlockedChecksIncomplete GateBlockReason = "checks_incomplete"

	// GateBlockedChecksFailed is a check that finished red on the head.
	GateBlockedChecksFailed GateBlockReason = "checks_failed"
)

// Clear reports whether the reason is GateClear.
func (r GateBlockReason) Clear() bool { return r == GateClear }

// String returns the reason's name.
func (r GateBlockReason) String() string { return string(r) }

// Pending reports whether the reason is one time may resolve on its own — a
// review or a check that has not arrived yet — as opposed to a refusal.
func (r GateBlockReason) Pending() bool {
	switch r {
	case GateBlockedInsufficientApprovals, GateBlockedReviewerNotIndependent,
		GateBlockedNoChecks, GateBlockedChecksIncomplete, GateBlockedReviewErrored,
		GateBlockedPullRequestDraft:
		return true
	default:
		return false
	}
}

// GateDecision is the gate's answer.
type GateDecision struct {
	Reason    GateBlockReason
	HeadSHA   string
	Approvals int
	Detail    string
}

// Mergeable reports whether the pull request may land.
func (d GateDecision) Mergeable() bool { return d.Reason.Clear() }

// GateInput is everything the gate reads.
type GateInput struct {
	Policy            ReviewPolicy
	PullRequestState  PullRequestState
	HeadSHA           string
	Reviews           []ReviewRecord
	Checks            []CheckRecord
	AuthorModelFamily string

	// Draft is the author's own statement that the change is unfinished.
	Draft bool
}

// EvaluateReviewGate decides whether the current head may land.
func EvaluateReviewGate(in GateInput) GateDecision {
	decision := GateDecision{HeadSHA: in.HeadSHA}

	if in.Policy.RequiredApprovals <= 0 {
		decision.Reason = GateBlockedPolicyInvalid
		decision.Detail = "the review policy requires " + strconv.Itoa(in.Policy.RequiredApprovals) +
			" approvals, which would merge unreviewed code"
		return decision
	}
	if in.PullRequestState != PullRequestOpen {
		decision.Reason = GateBlockedPullRequestNotOpen
		decision.Detail = "pull request is " + in.PullRequestState.String()
		return decision
	}
	// A DRAFT MAY NOT LAND, WHATEVER ITS APPROVALS SAY. The author has
	// stated the change is unfinished, and no reviewer's opinion overrules
	// the one person who knows that. Refused BEFORE the verdicts are read so
	// the answer cannot depend on who happened to review first, and reported
	// as pending rather than as a refusal because marking a pull request
	// ready is a thing that ordinarily happens next.
	if in.Draft {
		decision.Reason = GateBlockedPullRequestDraft
		decision.Detail = "the author marked this pull request a draft; the work is unfinished"
		return decision
	}
	if in.HeadSHA == "" {
		decision.Reason = GateBlockedHeadUnknown
		decision.Detail = "no head commit recorded, so no verdict can bind to it"
		return decision
	}

	verdicts := latestVerdictsForHead(in.Reviews, in.HeadSHA)

	// Fail-closed first: an errored review on the head blocks even beside an
	// approval, because the review that produced the approval is the one
	// erroring.
	for _, review := range verdicts {
		if review.Verdict == VerdictError {
			decision.Reason = GateBlockedReviewErrored
			decision.Detail = "review by " + review.Reviewer + " could not be produced for this head"
			return decision
		}
	}
	for _, review := range verdicts {
		if review.Verdict == VerdictRequestChanges {
			decision.Reason = GateBlockedChangesRequested
			decision.Detail = review.Reviewer + " requested changes"
			return decision
		}
	}

	if in.Policy.RequireIndependentReviewer && in.AuthorModelFamily == "" {
		decision.Reason = GateBlockedAuthorUnknown
		decision.Detail = "independence is required but nothing records what wrote the code"
		return decision
	}

	// A RED PIPELINE IS REPORTED BEFORE THE APPROVAL COUNT. The two are
	// judged by different actors, and only one of them will ever act on a
	// red head: the review sweep dispatches no reviewer until the checks
	// are green, so a pull request with a settled red and no review would
	// wait here as "0 of 1 approvals" for a review that cannot come, while
	// the worker who could fix the red was never told. Measured on
	// go-quicbaas PR #971 (2026-09-21): thirty minutes in reviewing on a
	// failed Security Scan, then ended by the stall detector. Checks that
	// have merely not finished stay behind the approvals, where waiting is
	// the right answer for both.
	if in.Policy.RequireGreenChecks {
		if reason, detail := evaluateChecks(in.Checks, in.HeadSHA); reason == GateBlockedChecksFailed {
			decision.Reason = reason
			decision.Detail = detail
			return decision
		}
	}

	independent, excluded := countIndependentApprovals(verdicts, in.Policy, in.AuthorModelFamily)
	decision.Approvals = independent
	if independent < in.Policy.RequiredApprovals {
		if excluded > 0 {
			decision.Reason = GateBlockedReviewerNotIndependent
			decision.Detail = strconv.Itoa(excluded) +
				" approval(s) did not come from a model family independent of the author's (" +
				in.AuthorModelFamily + ")"
			return decision
		}
		decision.Reason = GateBlockedInsufficientApprovals
		decision.Detail = strconv.Itoa(independent) + " of " + strconv.Itoa(in.Policy.RequiredApprovals) +
			" required approvals"
		return decision
	}

	if in.Policy.RequireGreenChecks {
		if reason, detail := evaluateChecks(in.Checks, in.HeadSHA); !reason.Clear() {
			decision.Reason = reason
			decision.Detail = detail
			return decision
		}
	}

	decision.Reason = GateClear
	return decision
}

// evaluateChecks decides the pipeline half of the gate. Its empty case is the
// one that reads wrongly when folded into a loop: "no check failed" is true
// of no checks at all, so the absence of CI is answered first.
func evaluateChecks(checks []CheckRecord, headSHA string) (GateBlockReason, string) {
	covering := make([]CheckRecord, 0, len(checks))
	for _, check := range checks {
		if check.HeadSHA == headSHA {
			covering = append(covering, check)
		}
	}
	for _, check := range covering {
		if !check.Status.Finished() {
			return GateBlockedChecksIncomplete, check.Name + " has not finished"
		}
	}
	// A SKIPPED CHECK IS PASSED OVER, in both directions: it is not red, and
	// it is not green either. A pipeline that skipped every job for this head
	// verified nothing, which is the "no check covers this head" refusal, not
	// a clear gate.
	judged := 0
	for _, check := range covering {
		if check.Status == CheckSkipped {
			continue
		}
		judged++
		if check.Status != CheckSuccess {
			return GateBlockedChecksFailed, check.Name + " finished " + check.Status.String()
		}
	}
	if judged == 0 {
		return GateBlockedNoChecks, "green checks are required and no check covers this head"
	}
	return GateClear, ""
}

// ChecksGreenForReview decides the pipeline question a review DISPATCH asks:
// has everything that runs against this head finished, and finished green.
//
// IT IS THE MERGE GATE'S OWN CHECK HALF, REUSED RATHER THAN RESTATED. The
// dispatcher must not reach a different answer from EvaluateReviewGate about
// the same commit: a second implementation would eventually dispatch a
// reviewer onto a head the gate then refuses to merge, or — the direction that
// costs real money — hold a review back from a head the gate considers
// perfectly landable. So both call evaluateChecks, and there is exactly one
// definition of green in this module.
//
// A POLICY THAT DOES NOT REQUIRE GREEN CHECKS IS CLEAR HERE. That is not a
// bypass: the project has stated that its checks do not decide whether a
// change may land, and a review gate stricter than the merge gate would hold
// changes for a signal the project does not use.
//
// THE SETTLE WINDOW IS DELIBERATELY NOT CONSULTED. It exists to stop a red
// being read as the change's own failure while a re-run is in flight
// (DefaultChecksSettleWindow), and that question only matters when a red is
// about to cost somebody an attempt. Here a red simply means "not yet", the
// next delivery asks again, and a window would only delay the green answer.
func ChecksGreenForReview(policy ReviewPolicy, headSHA string, runs []ForgeCheckRun, now time.Time) (GateBlockReason, string) {
	if !policy.RequireGreenChecks {
		return GateClear, ""
	}
	head := strings.TrimSpace(headSHA)
	if head == "" {
		return GateBlockedHeadUnknown, "no head commit, so no check can be attributed to one"
	}
	return evaluateChecks(checkRecordsFor(runs, head, now), head)
}

// runsExcludingContext drops one check context from a run listing.
//
// THE REVIEWER'S OWN STATUS IS THE OUTPUT OF A REVIEW, NEVER AN INPUT TO
// WHETHER ONE MAY START. Both paths that decide "is this head green enough to
// review" read every status on the head, and the reviewer publishes a status
// on that same head — so leaving it in asks the review to be green before the
// review has run. It is circular in every state the reviewer can publish:
// pending reads as a pipeline still running, and failure — which is what a
// comment and an error publish — reads as a pipeline that went red. Either
// way the head can never be reviewed again.
//
// The MERGE gate is the opposite case and must keep it: there, a review that
// failed is exactly what should hold the change back. So the exclusion lives
// at the two dispatch call sites and not inside ChecksGreenForReview, which
// both gates share.
func runsExcludingContext(runs []ForgeCheckRun, context string) []ForgeCheckRun {
	excluded := strings.TrimSpace(context)
	if excluded == "" {
		return runs
	}
	kept := make([]ForgeCheckRun, 0, len(runs))
	for _, run := range runs {
		if strings.EqualFold(strings.TrimSpace(run.Name), excluded) {
			continue
		}
		kept = append(kept, run)
	}
	return kept
}

// countIndependentApprovals counts approvals satisfying the independence
// policy and how many it excluded, so the caller names the right refusal.
func countIndependentApprovals(verdicts []ReviewRecord, policy ReviewPolicy, authorFamily string) (independent, excluded int) {
	for _, review := range verdicts {
		if review.Verdict != VerdictApprove {
			continue
		}
		if !policy.RequireIndependentReviewer {
			independent++
			continue
		}
		if review.ModelFamily == "" || review.ModelFamily == authorFamily {
			excluded++
			continue
		}
		independent++
	}
	return independent, excluded
}

// latestVerdictsForHead reduces the reviews bound to one head to each
// reviewer's operative verdict, ordered by reviewer. A reviewer legitimately
// has more than one verdict on one head (an errored review is retried), so
// collapsing is required for the retry to ever clear the gate.
func latestVerdictsForHead(reviews []ReviewRecord, headSHA string) []ReviewRecord {
	newest := map[string]ReviewRecord{}
	for _, review := range reviews {
		if !review.AppliesTo(headSHA) {
			continue
		}
		held, seen := newest[review.Reviewer]
		if !seen || reviewSupersedes(review, held) {
			newest[review.Reviewer] = review
		}
	}
	verdicts := make([]ReviewRecord, 0, len(newest))
	for _, review := range newest {
		verdicts = append(verdicts, review)
	}
	sort.Slice(verdicts, func(i, j int) bool { return verdicts[i].Reviewer < verdicts[j].Reviewer })
	return verdicts
}

// reviewSupersedes reports whether candidate replaces held as a reviewer's
// operative verdict: a later round, then a later submission, and on a tie
// the verdict that blocks harder — the gate fails closed, never on the
// accident of ordering.
func reviewSupersedes(candidate, held ReviewRecord) bool {
	if candidate.Round != held.Round {
		return candidate.Round > held.Round
	}
	if !candidate.SubmittedAt.Equal(held.SubmittedAt) {
		return candidate.SubmittedAt.After(held.SubmittedAt)
	}
	return verdictSeverity(candidate.Verdict) > verdictSeverity(held.Verdict)
}

// verdictSeverities ranks how hard each verdict blocks, for the tie-break in
// reviewSupersedes.
//
// VerdictComment ranks ABOVE an approval and below both refusals, and every
// part of that is deliberate. Above an approval, so a reviewer whose comment
// and approval carry the same timestamp is read as not having approved —
// the gate fails closed on a tie it cannot order. Below a refusal, so a
// comment can never retract a standing REQUEST_CHANGES however the two are
// ordered. That is the same rank an unrecognised verdict takes; it is named
// here anyway, because a rule that holds only because a value is unknown
// stops holding the moment it becomes known.
var verdictSeverities = map[ReviewVerdict]int{
	VerdictApprove:        0,
	VerdictComment:        1,
	VerdictError:          2,
	VerdictRequestChanges: 3,
}

// verdictSeverity ranks a verdict by how hard it blocks; an unknown verdict
// ranks above an approval and below the refusals.
func verdictSeverity(v ReviewVerdict) int {
	if s, ok := verdictSeverities[v]; ok {
		return s
	}
	return 1
}

// findingsFor returns the findings of the reviewers who requested changes on
// the head, for the worker's next round.
func findingsFor(reviews []ReviewRecord, headSHA string) string {
	var b strings.Builder
	for _, review := range latestVerdictsForHead(reviews, headSHA) {
		if review.Verdict != VerdictRequestChanges {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		body := strings.TrimSpace(review.Findings)
		if body == "" {
			body = sdlcPrompts.MustRender("changes_requested_no_findings", nil)
		}
		b.WriteString(sdlcPrompts.MustRender("changes_requested", prompts.Vars{
			"reviewer": review.Reviewer,
			"head_sha": headSHA,
			"findings": body,
		}))
	}
	return b.String()
}

// =============================================================================
// Forge records → Delivery records
// =============================================================================

// forgeReviewVerdicts maps a forge review state to a verdict; states not
// listed (COMMENT, PENDING) carry no verdict.
var forgeReviewVerdicts = map[string]ReviewVerdict{
	"APPROVED":        VerdictApprove,
	"REQUEST_CHANGES": VerdictRequestChanges,
}

// forgeCheckStatuses maps a forge check's status/conclusion pair to a status.
var forgeCheckStatuses = map[[2]string]CheckStatus{
	{"completed", "success"}:   CheckSuccess,
	{"completed", "failure"}:   CheckFailure,
	{"completed", "cancelled"}: CheckCancelled,
	{"completed", "skipped"}:   CheckSkipped,
}

// reviewRecordFor converts one forge review, when it carries a verdict.
func reviewRecordFor(review ForgeReview, policy ReviewPolicy, round int) (ReviewRecord, bool) {
	verdict, acted := forgeReviewVerdicts[strings.ToUpper(strings.TrimSpace(review.State))]
	if !acted || review.Reviewer == "" || review.HeadSHA == "" {
		return ReviewRecord{}, false
	}
	record := ReviewRecord{
		Reviewer:    review.Reviewer,
		ModelFamily: policy.ReviewerModelFamily(review.Reviewer),
		Verdict:     verdict,
		HeadSHA:     review.HeadSHA,
		Round:       round,
		Findings:    review.Body,
		SubmittedAt: review.Submitted.UTC(),
	}
	if review.ID != 0 {
		record.ExternalID = strconv.FormatInt(review.ID, 10)
	}
	return record, true
}

// checkStatusFor converts a forge check's status/conclusion.
func checkStatusFor(check ForgeCheckRun) CheckStatus {
	key := [2]string{strings.ToLower(check.Status), strings.ToLower(check.Conclusion)}
	if status, ok := forgeCheckStatuses[key]; ok {
		return status
	}
	return CheckPending
}

// checkRecordsFor converts the latest run per check context on one head.
func checkRecordsFor(checks []ForgeCheckRun, headSHA string, now time.Time) []CheckRecord {
	latest := latestCheckPerContext(checks)
	records := make([]CheckRecord, 0, len(latest))
	for _, check := range latest {
		record := CheckRecord{Name: check.Name, HeadSHA: headSHA, Status: checkStatusFor(check), ForgeRunID: check.ID}
		if record.Status.Finished() {
			record.FinishedAt = check.Completed.UTC()
			if check.Completed.IsZero() {
				record.FinishedAt = now
			}
		}
		records = append(records, record)
	}
	return records
}

// latestCheckPerContext keeps the newest run per check name.
func latestCheckPerContext(checks []ForgeCheckRun) []ForgeCheckRun {
	newest := map[string]ForgeCheckRun{}
	for _, check := range checks {
		if check.Name == "" {
			continue
		}
		held, seen := newest[check.Name]
		if !seen || checkSupersedes(check, held) {
			newest[check.Name] = check
		}
	}
	out := make([]ForgeCheckRun, 0, len(newest))
	for _, check := range newest {
		out = append(out, check)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// checkSupersedes orders two runs of one context: by numeric id when both
// have one, then by completion, then by start.
func checkSupersedes(candidate, held ForgeCheckRun) bool {
	if cmp, ok := compareCheckIDs(candidate.ID, held.ID); ok && cmp != 0 {
		return cmp > 0
	}
	if !candidate.Completed.Equal(held.Completed) {
		return candidate.Completed.After(held.Completed)
	}
	return candidate.Started.After(held.Started)
}

func compareCheckIDs(a, b string) (int, bool) {
	ai, errA := strconv.ParseInt(a, 10, 64)
	bi, errB := strconv.ParseInt(b, 10, 64)
	if errA != nil || errB != nil {
		return 0, false
	}
	switch {
	case ai < bi:
		return -1, true
	case ai > bi:
		return 1, true
	default:
		return 0, true
	}
}

// =============================================================================
// Settled check failures — the change's own red, not a runner fault
// =============================================================================

// DefaultChecksSettleWindow is how long a red check must stay red before it
// is read as the change's own failure.
const DefaultChecksSettleWindow = 5 * time.Minute

// DefaultChecksLogBytes bounds the log excerpt quoted per failed check.
const DefaultChecksLogBytes = 4 * 1024

// DefaultChecksClassifyBytes bounds the log READ per failed check, which is
// what pkg/cifault is handed.
//
// SIXTEEN TIMES THE QUOTE BUDGET, because the two answer different questions.
// A quote is for a person and stops being useful long before it stops being
// long; a classification has to reach the decisive line, and on this fleet
// that line sits behind the post-checkout teardown the runner appends to
// every job — roughly forty lines of git-config noise. Measured on the
// 2026-09-12 corpus: "unknown revision" is 42 lines from the end of job
// 77532's log and a bare "FAIL" is 40 lines from the end of the race
// detector's. A 4 KiB tail reaches neither, and a classifier that cannot see
// the decisive line answers "genuine" — safe, but the whole feature off.
const DefaultChecksClassifyBytes = 64 * 1024

const (
	// maxChecksLogContexts bounds how many failed checks are QUOTED to the
	// worker. Three is what a person reads.
	maxChecksLogContexts = 3

	// maxChecksClassifyContexts bounds how many failed checks are READ for
	// classification. Higher than the quote bound because a broken host fails
	// every job of a pipeline at once, and a check whose log was never read
	// classifies as genuine — one unread check among six would send the whole
	// fleet's outage to a coding agent. Bounded all the same, so a repository
	// with fifty contexts cannot turn one round into fifty forge calls.
	maxChecksClassifyContexts = 12
)

// ChecksFailure is a settled red on one head.
type ChecksFailure struct {
	HeadSHA   string
	Failed    []CheckRecord
	SettledAt time.Time
}

// evaluateChecksFailure reports a settled failure: every check on the head
// finished, at least one did not succeed, and the newest finish is older than
// window.
//
// A CANCELLED CHECK COUNTS, and it did not before. evaluateChecks already
// blocks the gate on one — it refuses every status that is not success — but
// this only ever collected CheckFailure, so a head whose only red was a
// cancellation never settled and the job waited in reviewing until the stall
// detector noticed. Collecting it is what lets the classifier answer it, and
// its answer is void: a cancellation is an act from outside the tests and
// never their verdict on the change (go-quicbaas#898).
func evaluateChecksFailure(checks []CheckRecord, headSHA string, now time.Time, window time.Duration) (ChecksFailure, bool) {
	if headSHA == "" {
		return ChecksFailure{}, false
	}
	failure := ChecksFailure{HeadSHA: headSHA}
	for _, check := range checks {
		if check.HeadSHA != headSHA {
			continue
		}
		if !check.Status.Finished() || check.FinishedAt.IsZero() {
			return ChecksFailure{}, false
		}
		if check.FinishedAt.After(failure.SettledAt) {
			failure.SettledAt = check.FinishedAt
		}
		// A skipped check is not a failure to triage, and cifault would refuse
		// the conclusion; it is passed over here as the gate passes over it.
		if check.Status != CheckSuccess && check.Status != CheckSkipped {
			failure.Failed = append(failure.Failed, check)
		}
	}
	if len(failure.Failed) == 0 || failure.SettledAt.IsZero() {
		return ChecksFailure{}, false
	}
	if now.Sub(failure.SettledAt) < window {
		return ChecksFailure{}, false
	}
	sort.SliceStable(failure.Failed, func(i, j int) bool { return failure.Failed[i].Name < failure.Failed[j].Name })
	return failure, true
}

// Findings renders the failure for the worker: what failed, what this module
// decided each red meant, and the tail of the logs it read.
//
// THE TRIAGE IS IN THE PROMPT, not only in the engine's own decision. A worker
// handed six red contexts on a day the fleet was unwell spends its round
// guessing which of them is its problem — that is what happened on 2026-09-12.
// Naming the infrastructure faults explicitly, and telling the worker not to
// chase them, is the cheapest part of go-quicbaas#898.
//
// ONLY THE CHANGE'S OWN REDS ARE NAMED AND QUOTED. A void or host-defect check
// is being handled without the worker, so quoting its log would hand it a
// failure to fix that no change of its could.
func (f ChecksFailure) Findings(triage ChecksTriage, logs map[string]string, quoteBytes int) string {
	var checks strings.Builder
	quoted := 0
	for _, check := range f.Failed {
		if !triage.Verdict(check.Name).ReachesAgent() {
			continue
		}
		finished := ""
		if !check.FinishedAt.IsZero() {
			finished = " (finished " + check.FinishedAt.UTC().Format(time.RFC3339) + ")"
		}
		checks.WriteString("\n")
		checks.WriteString(sdlcPrompts.MustRender("ci_red_check", prompts.Vars{
			"check_name": check.Name,
			"finished":   finished,
		}))
		checks.WriteString("\n")
		if quoted >= maxChecksLogContexts {
			continue
		}
		// THE LOG IS REDACTED HERE, NEVER IN THE PROMPT. A fenced excerpt of
		// somebody's CI output is the one part of this message that can
		// carry a secret, and a scrub a prompt could be edited to skip is
		// not a scrub. Bounded here too, and for the same reason: the log
		// handed in was read at the CLASSIFIER's budget, which is sixteen
		// times what a worker should be made to read.
		excerpt, _ := boundedTail(strings.TrimSpace(redactSecrets(logs[check.Name])), quoteBytes)
		if excerpt == "" {
			continue
		}
		quoted++
		checks.WriteString(sdlcPrompts.MustRender("ci_red_check_log", prompts.Vars{"excerpt": excerpt}))
		checks.WriteString("\n")
	}
	infrastructure := ""
	if faults := triage.NotTheChanges(); len(faults) > 0 {
		infrastructure = "\n" + sdlcPrompts.MustRender("ci_red_infrastructure", prompts.Vars{
			"checks": strings.Join(faults, ", "),
		}) + "\n"
	}
	return sdlcPrompts.MustRender("ci_red", prompts.Vars{
		"head_sha":       f.HeadSHA,
		"failed_count":   strconv.Itoa(len(f.Failed)),
		"check_noun":     checkNouns[len(f.Failed) == 1],
		"infrastructure": infrastructure,
		"checks":         checks.String(),
	})
}

// checkNouns makes the count agree. Plural agreement is grammar the engine
// owes the reader, not a choice a prompt should be able to get wrong.
var checkNouns = map[bool]string{true: "required check", false: "required checks"}

// checksFailureLogs reads the tail of each failed check's log, when the forge
// can serve one, bounded per check and in contexts.
//
// What comes back is REDACTED AND TIMESTAMP-STRIPPED but not otherwise cut at
// the quote budget: it is what the classifier reads AND what the findings
// quote a tail of, and reading it twice at two sizes would cost the forge call
// twice.
func checksFailureLogs(ctx context.Context, forge ForgeBackend, repo RepoRef, failure ChecksFailure, checks []ForgeCheckRun, maxBytes int) map[string]string {
	if forge == nil || !forge.Capabilities().CheckRunLogs || maxBytes <= 0 {
		return nil
	}
	byName := make(map[string]ForgeCheckRun, len(checks))
	for _, check := range checks {
		byName[check.Name] = check
	}
	logs := make(map[string]string)
	for i, failed := range failure.Failed {
		if i >= maxChecksClassifyContexts {
			break
		}
		check, ok := byName[failed.Name]
		if !ok {
			continue
		}
		raw, err := forge.CheckRunLogs(ctx, repo, check, maxBytes)
		if err != nil {
			continue
		}
		if excerpt := checksLogExcerpt(raw, maxBytes); excerpt != "" {
			logs[failed.Name] = excerpt
		}
	}
	return logs
}

// checksLogExcerpt returns the last limit bytes of a log, on a line boundary
// and never inside a rune.
//
// The bound is boundedTail's (public_reason.go), which every consumer of a
// bounded tail shares; what is particular to a check's log is the runner
// timestamp prefix stripped from it first — by cifault.StripTimestamps, which
// the classifier applies to the same text, so the two cannot disagree about
// where a line begins.
func checksLogExcerpt(raw string, limit int) string {
	excerpt, _ := boundedTail(redactSecrets(cifault.StripTimestamps(raw)), limit)
	return excerpt
}
