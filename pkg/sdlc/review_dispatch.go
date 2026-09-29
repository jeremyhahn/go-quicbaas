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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Dispatch — a pull-request delivery becomes a review job
// =============================================================================
//
// THE PAYLOAD DECIDES NOTHING. A verified delivery tells the dispatcher which
// PROJECT and which PULL REQUEST — or which COMMIT — something happened to,
// and that is the whole of its authority: whether a review is needed, on which
// head, by whom, and whether one has already been given are all read from the
// binding and the forge, inside the job, at the moment the job runs.
//
// This is why the runner takes a pull request number and no head, and why
// label routing is absent rather than merely discouraged. Everything the
// shell automation read out of a payload — the reviewer, the trigger, the
// bypass, the head — is read here from a record instead, and every one of
// those substitutions closed an incident.
//
// THE ONE THING THIS LAYER DECIDES IS WHETHER THE PIPELINE HAS FINISHED GREEN,
// and it decides that by reading the forge rather than the payload. See
// ReviewDispatcher.TriggerReview.

// reviewDispatchKinds are the event kinds that warrant asking whether a pull
// request needs a review.
//
// OPENED AND UPDATED, AND DELIBERATELY NOT REVIEW-SUBMITTED. A review
// landing is what this module's own verdict looks like coming back through
// the hook, and dispatching on it would have the reviewer answer itself in a
// loop. Merged and closed need no verdict.
//
// A ready-for-review transition arrives as an update, which is what makes the
// draft skip recoverable: the head is unchanged, no verdict stands on it
// because the draft run posted none, and the next delivery reviews it.
//
// AND CHECK-RUN UPDATES, WHICH ARE THE DELIVERIES A GATED REVIEW ACTUALLY
// STARTS ON (go-quicbaas#927). A pull request opening is a change nothing has
// tested yet: this fleet's pipeline takes about fifty minutes, so a reviewer
// spawned on the open event reads a tree whose verdict is an hour away and —
// worse — reaches for the build and the tests itself to find out. The delivery
// that says "the pipeline finished" was previously discarded here, which left
// only the two events that arrive too early. Acting on it instead is what
// makes a gated review event-driven rather than a wait: the job starts when
// the answer exists, and no session, lease or job is held while CI runs.
var reviewDispatchKinds = map[ForgeEventKind]bool{
	EventPullRequestOpened:  true,
	EventPullRequestUpdated: true,
	EventCheckRunUpdated:    true,
}

// ReviewRequest is what one verified delivery says happened, for the
// dispatcher to resolve into zero or more reviews.
//
// IT CARRIES BOTH IDENTIFIERS BECAUSE THE FORGES DO NOT AGREE ON WHICH ONE A
// PIPELINE EVENT HAS. A pull-request delivery names a pull request; a commit
// status, a check run and a workflow job name a COMMIT and nothing else, on
// every platform this module speaks to. A port that took only a number would
// therefore be a port a green build could not reach, which is exactly how the
// check-run delivery came to be dropped.
type ReviewRequest struct {
	// TenantID and ProjectID name the project the delivery was routed to.
	TenantID  string
	ProjectID string

	// PullRequest is the pull request the delivery named, or zero when it
	// named none.
	PullRequest int

	// HeadSHA is the commit the delivery was about, empty when it named none.
	//
	// IT FINDS THE PULL REQUEST AND NEVER DECIDES ABOUT IT. The head a verdict
	// binds to is read from the forge inside the job, because by then the
	// author may have pushed again; this is a lookup key, not a fact the
	// dispatcher carries forward.
	HeadSHA string
}

// Validate refuses a request the dispatcher cannot act on.
func (r ReviewRequest) Validate() error {
	switch {
	case strings.TrimSpace(r.TenantID) == "":
		return &InvalidInputError{Field: "tenant_id", Message: "a review is started for one tenant's project"}
	case strings.TrimSpace(r.ProjectID) == "":
		return &InvalidInputError{Field: "project_id", Message: "a review is started for one project"}
	case r.PullRequest < 0:
		return &InvalidInputError{Field: ParamPullRequest, Message: "pull requests are numbered from one"}
	case r.PullRequest == 0 && strings.TrimSpace(r.HeadSHA) == "":
		return &InvalidInputError{
			Field:   "head_sha",
			Message: "a delivery naming neither a pull request nor a commit names nothing to review",
		}
	}
	return nil
}

// ReviewTrigger starts the reviews one delivery calls for.
//
// It is a port rather than a direct call on the engine so the webhook path
// holds no orchestration client, and so a node that ingests deliveries
// without reviewing anything simply has none.
type ReviewTrigger interface {
	// TriggerReview starts a review job for every pull request the request
	// resolves to whose pipeline has finished green, or reports why it started
	// none. Starting a review for a pull request that turns out not to need
	// one is normal and cheap: the job's own first step decides that against
	// the forge.
	TriggerReview(ctx context.Context, req ReviewRequest) (started bool, err error)
}

// JobStarter starts an orchestration job. orchestration.AgentService
// satisfies it.
type JobStarter interface {
	StartJob(ctx context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error)
}

// ReviewDispatcher turns "something happened to this pull request" into a
// review job.
type ReviewDispatcher struct {
	bindings  BindingReader
	overrides ReviewerOverrideReader
	jobs      JobStarter
	forges    ForgeResolver

	// accounts answers whether the selected seat's Agent can draw a usable
	// pooled account. Optional; see SetAccountAvailability.
	accounts ReviewerAccountAvailability

	// heldReasons remembers the reason last posted on each held head, keyed
	// by heldKey, so the status is written only when the reason changes. It is
	// process memory, not storage: after a restart the reason is posted once
	// more, which is harmless.
	heldReasons sync.Map
}

// ReviewerAccountAvailability answers whether an Agent can draw a usable
// pooled account; orchestration.AgentService satisfies it.
type ReviewerAccountAvailability interface {
	AgentAccountAvailability(ctx context.Context, tenantID, projectID string, ref orchestration.AgentRef, role orchestration.AgentRole) (orchestration.AccountAvailability, error)
}

// SetAccountAvailability installs the account check a review is held on.
//
// A REVIEW WITH NO USABLE ACCOUNT IS NOT STARTED. When every account the
// seat's Agent could draw on is errored, revoked or over budget, the job's
// only possible outcome is a failed spawn, and the sweep would start one every
// pass for as long as nobody fixes the account (go-codec watcher, 2026-09-28).
// The dispatch is held instead; the account's own inbox item says what to fix,
// and the next sweep dispatches by itself once an account is usable again.
// Without a checker every review is dispatched, as it always was.
func (d *ReviewDispatcher) SetAccountAvailability(accounts ReviewerAccountAvailability) {
	d.accounts = accounts
}

var _ ReviewTrigger = (*ReviewDispatcher)(nil)

// NewReviewDispatcher wires the dispatcher; every collaborator is required.
func NewReviewDispatcher(bindings BindingReader, overrides ReviewerOverrideReader, jobs JobStarter, forges ForgeResolver) (*ReviewDispatcher, error) {
	switch {
	case bindings == nil:
		return nil, &InvalidInputError{Field: "bindings", Message: "whether a project is self-reviewed is on its binding"}
	case overrides == nil:
		return nil, &InvalidInputError{
			Field:   "overrides",
			Message: "which reviewer profile reviews a pull request is the operator's override, read on every dispatch",
		}
	case jobs == nil:
		return nil, &InvalidInputError{Field: "jobs", Message: "a review runs as a job on the engine"}
	case forges == nil:
		return nil, &InvalidInputError{
			Field:   "forges",
			Message: "whether the head is green, and which pull request a commit heads, are read from the forge",
		}
	}
	return &ReviewDispatcher{bindings: bindings, overrides: overrides, jobs: jobs, forges: forges}, nil
}

// RedispatchReview asks for a review of one pull request by whichever seat is
// selected for it now, and reports whether one started.
//
// IT IS WHAT AN OPERATOR'S OVERRIDE MEANS, AND IT NEVER CANCELS. Owner ruling
// 2026-09-25: switching reviewer lets every review already running finish
// normally; the switch decides who reviews from here on. So this is an
// ordinary dispatch, read against the override just recorded.
//
// A REVIEW STILL RUNNING HOLDS THE PULL REQUEST'S LEASE (ReviewLeaseSubject),
// so the engine refuses the new job as a duplicate and this answers false:
// the running review finishes and publishes, and every later dispatch — the
// next push, delivery or sweep — is by the newly selected seat. The lease is
// deliberately not widened to let both run at once: every counting seat
// publishes under the one required status context, and two concurrent
// reviewers there would let whichever finished last decide the gate.
func (d *ReviewDispatcher) RedispatchReview(ctx context.Context, tenantID, projectID string, pullRequest int) (bool, error) {
	started, err := d.TriggerReview(ctx, ReviewRequest{TenantID: tenantID, ProjectID: projectID, PullRequest: pullRequest})
	if err != nil && !jobAlreadyRunning(err) {
		return false, err
	}
	return started, nil
}

// TriggerReview implements ReviewTrigger.
//
// IT REFUSES ON CONFIGURATION AND ON THE PIPELINE, and on nothing else.
//
//   - CONFIGURATION. A project that binds no reviewer will never produce a
//     verdict however many jobs are started for it, so starting one would be
//     a job that exists to fail.
//   - THE PIPELINE. A review of a head whose checks are pending or red is work
//     spent on a tree CI may still reject, and it invites the reviewer to run
//     the suite itself to find out — measured at fourteen minutes of wall
//     clock and a whole attempt in go-quicbaas#927. The gate is the project's
//     own RequireGreenChecks policy, evaluated through the SAME function the
//     merge gate uses, so "green enough to review" and "green enough to
//     merge" cannot drift apart.
//
// Everything that is a matter of the pull request's current state — draft,
// closed, already reviewed — is still left to the job, because it is only
// knowable from the forge and only true as of the moment it is read.
//
// A PROJECT THAT REQUIRES GREEN CHECKS AND PUBLISHES NONE IS AN OPERATOR'S
// MISCONFIGURATION, AND IT IS LEFT VISIBLE. It has no head this will ever
// dispatch on — exactly as it has no head the merge gate will ever clear. The
// pull request waits, with the same stated reason, rather than being reviewed
// on a pipeline nobody ran. What an ungated project does instead is at the
// short circuit below.
func (d *ReviewDispatcher) TriggerReview(ctx context.Context, req ReviewRequest) (bool, error) {
	if err := req.Validate(); err != nil {
		return false, err
	}
	binding, err := d.bindings.GetBinding(ctx, req.TenantID, req.ProjectID)
	if err != nil {
		return false, err
	}
	if !binding.Reviewer.Configured() {
		return false, nil
	}
	// AN UNGATED PROJECT IS DISPATCHED EXACTLY AS IT ALWAYS WAS, and reads
	// nothing from the forge to establish that: its policy says the checks do
	// not decide whether a change may land, so they do not decide whether it
	// may be reviewed either.
	//
	// WHICH ALSO MEANS A PIPELINE DELIVERY IS NOTHING TO IT. The check-run
	// delivery exists for the gate; without one it carries no fact this project
	// acts on, and dispatching on it would turn every job of every run into a
	// review job — dozens per head, each of which would read the forge, start,
	// and decide against a standing verdict it did not need to ask about.
	if !binding.Review.RequireGreenChecks {
		if req.PullRequest <= 0 {
			return false, nil
		}
		return d.start(ctx, req.TenantID, binding, req.PullRequest)
	}

	forge, err := d.forges.ForgeFor(ctx, req.TenantID, binding.Forge)
	if err != nil {
		return false, err
	}
	if forge == nil {
		return false, NewForgeNotRegisteredError(binding.Forge.Type)
	}
	candidates, err := d.candidates(ctx, forge, binding, req)
	if err != nil {
		return false, err
	}

	// FROM HERE THE PROJECT IS GATED, so every candidate's pipeline is read.
	// A head that is not green is passed over rather than refused: a pipeline
	// still running, or one that has just gone red, is the ordinary state of a
	// change somebody is working on, and the next delivery asks again.
	started := false
	for _, pull := range candidates {
		green, greenErr := d.pipelineGreen(ctx, forge, binding, pull)
		if greenErr != nil {
			return started, greenErr
		}
		if !green {
			continue
		}
		ok, startErr := d.start(ctx, req.TenantID, binding, pull.Number)
		if startErr != nil {
			return started, startErr
		}
		started = started || ok
	}
	return started, nil
}

// candidates are the open pull requests one delivery is about.
//
// A delivery that names a pull request is about that one and no other, and it
// is READ rather than trusted: the head the check gate is evaluated against
// has to come from the forge, because the payload's is already stale by the
// time it is acted on.
//
// A delivery that names only a commit is about every open pull request that
// commit currently heads — normally exactly one, often none (a push to a
// branch with no pull request open on it is most pushes), and more than one
// only when one commit heads two branches, in which case both changes are
// equally reviewable and both are dispatched.
func (d *ReviewDispatcher) candidates(
	ctx context.Context,
	forge ForgeBackend,
	binding *ProjectBinding,
	req ReviewRequest,
) ([]ForgePullRequest, error) {
	repo := binding.Repo()
	if req.PullRequest > 0 {
		pull, err := forge.GetPullRequest(ctx, repo, req.PullRequest)
		if err != nil {
			return nil, err
		}
		if pull == nil {
			return nil, &NotFoundError{
				Entity:   "pull request",
				TenantID: req.TenantID,
				ID:       repo.String() + "#" + strconv.Itoa(req.PullRequest),
			}
		}
		return []ForgePullRequest{*pull}, nil
	}
	open, err := forge.ListOpenPullRequests(ctx, repo)
	if err != nil {
		return nil, err
	}
	head := strings.TrimSpace(req.HeadSHA)
	matching := make([]ForgePullRequest, 0, 1)
	for _, pull := range open {
		if strings.EqualFold(strings.TrimSpace(pull.HeadSHA), head) {
			matching = append(matching, pull)
		}
	}
	return matching, nil
}

// pipelineGreen reports whether the head's checks have finished green.
//
// A refusal is not an error: a pipeline still running, or one that has just
// gone red, is the ordinary state of a change somebody is working on. The next
// delivery asks again, which is what makes this event-driven — nothing is held
// open across the fifty minutes the answer takes.
func (d *ReviewDispatcher) pipelineGreen(
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
	// Every reviewer's own status is excluded: it is the result of a review,
	// not a precondition for one. See runsExcludingContexts.
	reason, _ := ChecksGreenForReview(
		binding.Review, head,
		runsExcludingContexts(runs, binding.ReviewerStatusContexts()),
		time.Now().UTC(),
	)
	return reason.Clear(), nil
}

// start starts one review job and reports whether it did.
//
// THE PROFILE IS CHOSEN HERE, FROM THE OPERATOR'S RECORD. The override for
// this pull request, when there is one, names the profile; otherwise the
// binding's default reviews it. The choice travels on the job as a parameter
// and the backend and model it runs on are declared on the start, so the
// lease that bounds a shared model server bounds the reviewer that actually
// runs.
func (d *ReviewDispatcher) start(ctx context.Context, tenantID string, binding *ProjectBinding, pullRequest int) (bool, error) {
	if pullRequest <= 0 {
		return false, ReviewJobSubject{PullRequest: pullRequest}.Validate()
	}
	profile, _, err := SelectReviewer(ctx, d.overrides, tenantID, binding, pullRequest)
	if err != nil {
		return false, err
	}
	reason, err := reviewerAccountHold(ctx, d.accounts, tenantID, binding, profile)
	if err != nil {
		return false, err
	}
	if reason != "" {
		return false, d.postHold(ctx, tenantID, binding, profile, pullRequest, reason)
	}
	jobID, err := startReviewJob(ctx, d.jobs, tenantID, binding, pullRequest, profile)
	return jobID != "", err
}

// reviewerAccountHold answers why profile's reviewer cannot run now for lack of
// a usable pooled account, or "" when it can (or when no checker is wired). It
// is the one check both a dispatched review and a merge train's review ask.
func reviewerAccountHold(ctx context.Context, accounts ReviewerAccountAvailability, tenantID string, binding *ProjectBinding, profile ReviewerProfile) (string, error) {
	if accounts == nil {
		return "", nil
	}
	availability, err := accounts.AgentAccountAvailability(ctx, tenantID, binding.ProjectID,
		orchestration.AgentRefTo(profile.Agent), orchestration.AgentRoleReview)
	if err != nil || availability.Usable {
		return "", err
	}
	if strings.TrimSpace(availability.Reason) == "" {
		return "no usable reviewer account", nil
	}
	return availability.Reason, nil
}

// postHold says on the pull request's head why its review is held: a pending
// status under the seat's review context, whose description is the reason.
// It is written only when the reason for that head changes.
func (d *ReviewDispatcher) postHold(ctx context.Context, tenantID string, binding *ProjectBinding, profile ReviewerProfile, pullRequest int, reason string) error {
	forge, err := d.forges.ForgeFor(ctx, tenantID, binding.Forge)
	if err != nil {
		return err
	}
	if forge == nil {
		return NewForgeNotRegisteredError(binding.Forge.Type)
	}
	pull, err := forge.GetPullRequest(ctx, binding.Repo(), pullRequest)
	if err != nil {
		return err
	}
	if pull == nil || strings.TrimSpace(pull.HeadSHA) == "" {
		return nil
	}
	key := heldKey(tenantID, binding.ProjectID, pullRequest, pull.HeadSHA, profile.Context())
	if last, seen := d.heldReasons.Load(key); seen && last == reason {
		return nil
	}
	if err := postReviewHold(ctx, forge, binding.Repo(), pull.HeadSHA, profile.Context(), reason); err != nil {
		return err
	}
	d.heldReasons.Store(key, reason)
	return nil
}

// postReviewHold writes a held review's reason on head as a pending status
// under context — unless a verdict already stands there.
//
// A VERDICT ALREADY ON THE HEAD STANDS. The hold says a review cannot run
// yet; it must never turn an approval or a rejection back into pending.
func postReviewHold(ctx context.Context, forge ForgeBackend, repo RepoRef, head, context, reason string) error {
	runs, err := forge.ListCheckRuns(ctx, repo, head)
	if err != nil {
		return err
	}
	for _, run := range latestCheckPerContext(runs) {
		if run.Name == context && checkStatusFor(run).Finished() {
			return nil
		}
	}
	return forge.SetCommitStatus(ctx, repo, head, CommitStatus{Context: context, Status: CheckPending, Description: reason})
}

// heldKey names one held review: a head of a pull request under one context.
func heldKey(tenantID, projectID string, pullRequest int, head, context string) string {
	return tenantID + "|" + projectID + "|" + strconv.Itoa(pullRequest) + "|" + head + "|" + context
}

// startReviewJob starts one review job of pullRequest by profile and answers
// the job's id, empty when the engine took none. It is the one way a review job is started — the
// dispatcher's, for a pull request somebody opened, and the merge train's, for
// its own — so the two cannot describe a review differently.
func startReviewJob(
	ctx context.Context,
	jobs JobStarter,
	tenantID string,
	binding *ProjectBinding,
	pullRequest int,
	profile ReviewerProfile,
) (string, error) {
	subject := ReviewJobSubject{PullRequest: pullRequest, Profile: profile.Name}
	if err := subject.Validate(); err != nil {
		return "", err
	}
	resp, err := jobs.StartJob(ctx, &orchestration.StartJobRequest{
		TenantID:  tenantID,
		ProjectID: binding.ProjectID,
		Runner:    ReviewJobRunnerName,
		// THE REVIEWER'S AGENT IS DECLARED SO IT CAN BE BOUNDED. Its backend
		// reaches the job lease (JobLease.Backend) and lets a deployment cap
		// how many reviews a shared model server runs at once. Without it, a
		// project's every green pull request is reviewed in the same pass —
		// reviews are off the project budget by construction — and a locally
		// hosted model serves them all at once, which serves none of them.
		Lead: orchestration.AgentRefTo(profile.Agent),
		// The description is for a person reading the job list. Nothing
		// parses it: the subject travels in Params, typed.
		Description: "Review " + binding.Repo().String() + "#" + strconv.Itoa(pullRequest) + " as " + profile.Name,
		Params:      subject.Params(),
	})
	if err != nil {
		return "", backendError("StartJob", err)
	}
	if resp == nil {
		return "", nil
	}
	return strings.TrimSpace(resp.JobID), nil
}

// DispatchReview asks the trigger for a review when the event warrants one.
//
// It is the ingestor's half, kept here beside the dispatch table so the two
// cannot drift: the ingestor knows nothing about which events mean a review.
//
// IT TAKES THE BINDING RATHER THAN A PROJECT ID because the delivery half's
// whole decision belongs in one place, and one part of it is the project's
// DISCOVERY MODE: a project that has said its reviews are found by polling
// ignores a delivery here rather than somewhere further in, where "we chose
// not to" would be indistinguishable from "nothing warranted one". The
// ingestor already holds the binding it verified the delivery against, so
// this costs no read.
func DispatchReview(ctx context.Context, trigger ReviewTrigger, tenantID string, binding *ProjectBinding, event *ForgeEvent) (bool, error) {
	if trigger == nil || binding == nil || event == nil || !reviewDispatchKinds[event.Kind] {
		return false, nil
	}
	if !binding.Discovery.Admits(DiscoveryPathDelivery) {
		return false, nil
	}
	request := ReviewRequest{
		TenantID:    tenantID,
		ProjectID:   binding.ProjectID,
		PullRequest: event.PullRequestNumber,
		HeadSHA:     event.HeadSHA,
	}
	// A delivery naming neither a usable pull request nor a commit is one this
	// build cannot act on. It is not an error — the forge sent something valid
	// — and the head it was about is reached again by ReviewSweeper, which is
	// the floor under this whole path: every open pull request of every bound
	// project, green and carrying no verdict, is reviewed from the sweep
	// whether or not any delivery for it ever arrives.
	if request.PullRequest < 0 {
		return false, nil
	}
	if request.PullRequest == 0 && strings.TrimSpace(request.HeadSHA) == "" {
		return false, nil
	}
	return trigger.TriggerReview(ctx, request)
}
