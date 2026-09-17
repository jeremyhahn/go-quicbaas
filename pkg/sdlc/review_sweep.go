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
// The review floor — a green pull request is reviewed whether or not a
// delivery ever arrived
// =============================================================================
//
// REVIEW DISPATCH WAS WEBHOOK-ONLY, AND ITS OWN COMMENT PROMISED OTHERWISE.
// DispatchReview had exactly one caller, the webhook ingestor; the issue
// poller adopts issues and nothing swept pull requests. So a node whose
// delivery path was broken or never provisioned reviewed nothing, silently,
// and — the part that made it more than a resilience nit — REPAIRING THE HOOK
// DID NOT RECOVER THE BACKLOG: the pipeline event for an already-green head
// fired in the past, a new hook receives no replay, and no operator verb could
// ask for a review. Measured on the burn-in node, 2026-09-13: `go-truststrap`
// had zero webhooks, two finished pull requests sat with every check green and
// `mergeable: true`, and the only way to advance either was to force CI to run
// again purely to manufacture a fresh event (go-quicbaas#933).
//
// THIS IS A SECOND TRIGGER FOR AN EXISTING PORT, NOT NEW REVIEW LOGIC. What a
// review is, when a project's pipeline counts as green, and whether a head has
// already been judged are decided exactly where they were: ReviewTrigger,
// ChecksGreenForReview and the review job itself. This file only finds the
// pull requests nobody told us about.
//
// TWO FILTERS, AND BOTH COME OUT OF ONE FORGE READ. A pull request is a
// candidate when its head carries NO status under the reviewer's own context
// and everything else on that head has finished green. The commit-status
// listing answers both questions at once, so the pass costs one listing of the
// open pull requests per project plus one status read per open pull request,
// and nothing else — see ReviewSweeper.needsReview.
//
// WHY THE ABSENT STATUS IS THE RIGHT FILTER RATHER THAN A RECORD OF ITS OWN:
// the review job publishes under that context on every ending it has — a
// verdict, a re-asserted standing verdict, a draft it declined to approve, a
// head it ran out of rounds for — so the forge already holds the answer to
// "has this head been through review", bound to the head it is about, visible
// to a person, and surviving any restart of this node. A second record here
// would be a weaker copy that can disagree with it.
//
// AND DOUBLE-STARTING IS THE ENGINE'S TO PREVENT, NOT THIS SWEEP'S. A delivery
// and a pass that race name the same pull request, so both start a job with
// the same lease subject (ReviewLeaseSubject) and exactly one of them acquires
// it. A sweeper-side "is one running" flag would be a second, weaker answer
// that disagrees with the lease across a restart or a node boundary — the same
// reasoning TriageScheduler states for the same reason.

// DefaultReviewSweepInterval is how often the review floor visits every bound
// project.
//
// SHORTER THAN THE TRIAGE BACKSTOP'S, AND THE DIFFERENCE IS WHAT EACH PASS
// COSTS. A triage pass starts a JOB per bound project; this one makes forge
// READS and starts work only for a pull request that is genuinely unjudged, so
// a quiet fleet costs one listing plus one status read per open pull request.
// The latency it bounds is a finished change waiting to merge, which is the
// most expensive kind of waiting there is.
const DefaultReviewSweepInterval = 5 * time.Minute

// ReviewSweeperConfig wires the sweeper.
type ReviewSweeperConfig struct {
	// Bindings enumerates the bound projects a pass visits and holds each
	// one's discovery mode, reviewer and repository.
	Bindings BindingReader

	// Forges builds the adapter a project's pull requests are read through.
	Forges ForgeResolver

	// Reviews is the port a candidate is handed to. It is ReviewTrigger and
	// not the dispatcher's concrete type: this sweep decides only WHICH pull
	// requests to ask about, and everything about whether a review is
	// warranted is the trigger's, unchanged from the delivery path.
	Reviews ReviewTrigger

	// Now is the clock the check evaluation reads; nil takes time.Now.
	Now func() time.Time
}

// ReviewSweeper finds the open pull requests that are green and unjudged, and
// asks the review trigger for each.
type ReviewSweeper struct {
	bindings BindingReader
	forges   ForgeResolver
	reviews  ReviewTrigger
	now      func() time.Time

	// failures are the per-tenant and per-project failures a pass deferred,
	// drained by SweepFailures so the caller logs them where it logs
	// everything else. Only the sweep goroutine touches them, and there is
	// exactly one.
	failures []error
}

// NewReviewSweeper builds the sweeper; every collaborator is required.
func NewReviewSweeper(cfg ReviewSweeperConfig) (*ReviewSweeper, error) {
	switch {
	case cfg.Bindings == nil:
		return nil, &InvalidInputError{
			Field:   "bindings",
			Message: "the pass visits bound projects, and the binding names the reviewer and the repository",
		}
	case cfg.Forges == nil:
		return nil, &InvalidInputError{
			Field:   "forges",
			Message: "which pull requests are open, and what has run against each head, are read from the forge",
		}
	case cfg.Reviews == nil:
		return nil, &InvalidInputError{
			Field:   "reviews",
			Message: "a pass that triggers no review is a forge listing nobody acts on",
		}
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &ReviewSweeper{bindings: cfg.Bindings, forges: cfg.Forges, reviews: cfg.Reviews, now: now}, nil
}

// Sweep visits every bound project of every tenant and reports how many
// reviews it started.
//
// THE ONE FAILURE THAT STOPS IT is not being able to enumerate the tenants:
// the pass then does not know what it was meant to visit, and reporting a
// clean pass would be a lie. Everything narrower — a tenant whose bindings
// cannot be listed, a project whose forge is unreachable — costs that one
// tenant or project a pass and nothing else, because every other project's
// only floor is this same pass.
func (s *ReviewSweeper) Sweep(ctx context.Context, tenants orchestration.TenantLister) (int, error) {
	if tenants == nil {
		return 0, &InvalidInputError{
			Field:   "tenants",
			Message: "a pass that enumerates no tenant visits nothing while claiming a clean pass",
		}
	}
	tenantIDs, err := tenants.ListTenantIDs(ctx)
	if err != nil {
		return 0, backendError("ListTenantIDs", err)
	}
	started := 0
	for _, tenantID := range tenantIDs {
		if err := ctx.Err(); err != nil {
			return started, err
		}
		// THE TENANT TRAVELS ON THE CONTEXT, NOT ONLY IN THE ARGUMENT. This
		// pass runs on a background context of its own, and the engine guards
		// StartJob on the tenant bound to the CONTEXT — so a sweep passing
		// only the argument starts nothing while reporting itself healthy.
		// The triage backstop was that bug (go-quicbaas#918) and the
		// task-source poller stamps it for the same reason.
		tenantCtx := orchestration.ContextWithTenant(ctx, tenantID)
		bindings, listErr := s.bindings.ListBindings(tenantCtx, tenantID)
		if listErr != nil {
			s.failures = append(s.failures, listErr)
			continue
		}
		for _, binding := range bindings {
			if binding == nil {
				continue
			}
			if err := ctx.Err(); err != nil {
				return started, err
			}
			swept, sweepErr := s.sweepBinding(tenantCtx, tenantID, binding)
			started += swept
			if sweepErr != nil {
				s.failures = append(s.failures, sweepErr)
			}
		}
	}
	return started, nil
}

// SweepProject runs one project's pass now and reports how many reviews it
// started.
//
// Exported because "why has this pull request no verdict" is a question an
// operator asks about one project, and answering it should not mean waiting
// for the fleet-wide pass. An UNBOUND project is a quiet no-op, exactly as it
// is for triage: most projects on a node have no forge binding.
func (s *ReviewSweeper) SweepProject(ctx context.Context, tenantID, projectID string) (int, error) {
	if strings.TrimSpace(tenantID) == "" {
		return 0, &InvalidInputError{Field: "tenant_id", Message: "a review pass is scoped to a tenant"}
	}
	if strings.TrimSpace(projectID) == "" {
		return 0, &InvalidInputError{Field: "project_id", Message: "a review pass is per project"}
	}
	binding, err := s.bindings.GetBinding(ctx, tenantID, projectID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	if binding == nil {
		return 0, nil
	}
	return s.sweepBinding(ctx, tenantID, binding)
}

// SweepFailures returns and clears the failures the last passes deferred, so
// the caller reports each of them once rather than on every later pass.
func (s *ReviewSweeper) SweepFailures() []error {
	failures := s.failures
	s.failures = nil
	return failures
}

// sweepBinding runs one bound project's pass.
//
// THE TWO CHEAP REFUSALS COME FIRST, BEFORE ANY FORGE CALL. A project whose
// discovery mode excludes the sweep, and a project that binds no reviewer,
// have nothing this pass could do — and reading the forge to find that out
// would spend a call per project per pass on projects this module was told not
// to look at.
func (s *ReviewSweeper) sweepBinding(ctx context.Context, tenantID string, binding *ProjectBinding) (int, error) {
	if !binding.Discovery.Admits(DiscoveryPathSweep) || !binding.Reviewer.Configured() {
		return 0, nil
	}
	forge, err := s.forges.ForgeFor(ctx, tenantID, binding.Forge)
	if err != nil {
		return 0, s.sweepError(tenantID, binding, "ForgeFor", err)
	}
	if forge == nil {
		return 0, s.sweepError(tenantID, binding, "ForgeFor", NewForgeNotRegisteredError(binding.Forge.Type))
	}
	open, err := forge.ListOpenPullRequests(ctx, binding.Repo())
	if err != nil {
		return 0, s.sweepError(tenantID, binding, "ListOpenPullRequests", err)
	}

	started := 0
	for _, pull := range open {
		if err := ctx.Err(); err != nil {
			return started, err
		}
		needed, needErr := s.needsReview(ctx, forge, binding, pull)
		if needErr != nil {
			return started, s.sweepError(tenantID, binding, "ListCheckRuns", needErr)
		}
		if !needed {
			continue
		}
		// THE TRIGGER DECIDES, AND IT READS THE FORGE AGAIN. The head this
		// pass listed may already have moved, and the pull request may already
		// have been closed; re-reading is what makes the verdict bind to the
		// tree that is actually there. For a gated project that is two further
		// calls, paid only for a pull request this pass is about to spend a
		// whole model run on.
		ok, triggerErr := s.reviews.TriggerReview(ctx, ReviewRequest{
			TenantID:    tenantID,
			ProjectID:   binding.ProjectID,
			PullRequest: pull.Number,
		})
		if triggerErr != nil {
			return started, s.sweepError(tenantID, binding, "TriggerReview", triggerErr)
		}
		if ok {
			started++
		}
	}
	return started, nil
}

// needsReview reports whether one open pull request is a candidate: no status
// under the reviewer's context, and every other check on the head finished
// green.
//
// ONE FORGE READ ANSWERS BOTH, and the order is deliberate. The absent-status
// question is decided from that one listing before the check evaluation runs,
// so a pull request that has already been judged costs nothing further — which
// is most of them, most of the time.
//
// GREEN IS ChecksGreenForReview AND NOTHING ELSE, the same function the merge
// gate and the delivery path call. A second definition here would eventually
// sweep a head the dispatcher refuses, or hold one back that it would have
// taken, and the two would drift with nobody watching.
func (s *ReviewSweeper) needsReview(
	ctx context.Context,
	forge ForgeBackend,
	binding *ProjectBinding,
	pull ForgePullRequest,
) (bool, error) {
	head := strings.TrimSpace(pull.HeadSHA)
	if head == "" {
		return false, nil
	}
	runs, err := forge.ListCheckRuns(ctx, binding.Repo(), head)
	if err != nil {
		return false, err
	}
	now := s.now()
	reviewerContext := binding.Reviewer.Context()
	if checkRunPresent(runs, reviewerContext, now) {
		return false, nil
	}
	// "EVERY OTHER CHECK", which is what this function has always promised:
	// the reviewer's own status is excluded, or a head that carries one can
	// never be green enough to review again. See runsExcludingContext.
	reason, _ := ChecksGreenForReview(binding.Review, head, runsExcludingContext(runs, reviewerContext), now)
	return reason.Clear(), nil
}

// sweepError names the project a failure belongs to.
func (s *ReviewSweeper) sweepError(tenantID string, binding *ProjectBinding, op string, cause error) error {
	return &ReviewSweepError{TenantID: tenantID, ProjectID: binding.ProjectID, Op: op, Cause: cause}
}

// StaleReviewStatusAfter is how long a PENDING reviewer status may stand
// before this sweep stops believing a review is still running behind it.
//
// IT MATCHES THE JOB STALL BOUND ON PURPOSE. That bound is the engine's own
// answer to "how long may a review take before the agent is presumed gone",
// and a floor that gave up sooner would dispatch a second reviewer onto a head
// the first is still working — the duplicate this sweep is careful to avoid.
// Deriving it from a second, independent number would let the two disagree.
const StaleReviewStatusAfter = 30 * time.Minute

// checkRunPresent reports whether a review has already been started for this
// head and may still be running behind the status it left.
//
// PRESENCE AND NOT STATE, ALMOST. A finished verdict, a standing one
// re-asserted and a declined draft all mean a review has been given, and
// reading their state would turn a judged head into "start another one".
// Pending is the one that is ambiguous: it is the shape of a review in flight
// AND the shape of one that finished without deciding.
//
// A PENDING STATUS THAT OUTLIVED THE STALL BOUND IS ABANDONED, NOT IN FLIGHT.
// Treating it as in flight made this sweep useless in exactly the case it
// exists for. The floor's whole purpose is that a green pull request is
// reviewed even when the delivery path broke — and a review that left pending
// behind blocked the gate AND this backstop, so nothing could ever re-review
// that head. Measured on go-luks2 #202: the round-2 commit answered the
// round-1 finding, CI went green, and it then waited indefinitely on a review
// no code path could dispatch. The reviewer's own verdict mapping no longer
// publishes pending for a finished review, but a reviewer killed mid-run still
// leaves one, and that is the case this bound recovers.
func checkRunPresent(runs []ForgeCheckRun, statusContext string, now time.Time) bool {
	wanted := strings.TrimSpace(statusContext)
	if wanted == "" {
		return false
	}
	for _, run := range runs {
		if !strings.EqualFold(strings.TrimSpace(run.Name), wanted) {
			continue
		}
		if !reviewStatusStale(run, now) {
			return true
		}
	}
	return false
}

// reviewStatusStale reports whether one reviewer status has stood pending long
// enough that the review behind it is presumed gone.
//
// ONLY PENDING CAN GO STALE. Every other state is a finished review, and a
// finished review does not become undone by age.
//
// AN UNDATED PENDING STATUS IS NOT STALE. Some forges report no timestamp, and
// "no date" must not read as "infinitely old" — that would re-dispatch a
// reviewer onto every head on every pass.
func reviewStatusStale(run ForgeCheckRun, now time.Time) bool {
	if CheckStatus(strings.TrimSpace(run.Status)) != CheckPending {
		return false
	}
	started := run.Started
	if started.IsZero() {
		return false
	}
	return now.Sub(started) > StaleReviewStatusAfter
}

// ReviewSweepError reports a project the review floor could not sweep.
//
// TYPED AND NAMING THE PROJECT because a pass visits many, defers what it
// could not do, and reports them together: "the forge refused" without a
// project is a line an operator cannot act on.
type ReviewSweepError struct {
	// TenantID and ProjectID name the project the pass was on.
	TenantID  string
	ProjectID string

	// Op is the call that failed.
	Op string

	// Cause is the failure underneath, kept reachable by errors.Is and
	// errors.As so a caller can still recognise a forge outage.
	Cause error
}

// Error implements the error interface.
func (e *ReviewSweepError) Error() string {
	return "sdlc review floor: project " + strconv.Quote(e.ProjectID) +
		" of tenant " + strconv.Quote(e.TenantID) + " was not swept: " +
		e.Op + ": " + e.Cause.Error()
}

// Unwrap returns the failure underneath.
func (e *ReviewSweepError) Unwrap() error { return e.Cause }
