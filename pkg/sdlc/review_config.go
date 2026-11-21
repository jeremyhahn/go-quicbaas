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
	"net/url"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Who reviews this project, and which pull request one review job is about
// =============================================================================
//
// THE REVIEWER IS PROJECT CONFIGURATION. It is decided once, on the binding,
// by whoever configured the project — not derived per pull request from a
// label, a title prefix, a comment or anything else that travels on a webhook
// payload.
//
// That is the single biggest lesson of the shell review automation this
// replaces. Routing by label made every question about the reviewer a
// question about the state of a mutable, forgeable, eventually-consistent
// display field: who may add the label, what happens when two are present,
// what happens when it is removed mid-review, what happens when the payload
// renders an empty set as null rather than as an empty array. None of those
// questions has a good answer, and each of them cost an incident. A binding
// field has none of them, because the configuration is read from a record
// this module owns and a payload cannot reach.

// DefaultReviewStatusContext is the commit status the review runner publishes
// under when the reviewer names none.
//
// It is the name branch protection requires by, so it is stable and boring on
// purpose: a context that varied with the reviewer's model or backend would
// have to be re-required on the forge every time either changed.
const DefaultReviewStatusContext = "quicbaas/review"

// ReviewerConfig is one REVIEW SEAT: the tenant Agent that reviews this
// project's pull requests, and the forge identity it reviews as.
//
// The Agent answers which runtime, which model and which image; the seat adds
// what an Agent cannot carry, because it is the project's and not the
// Agent's — an IDENTITY on the forge.
type ReviewerConfig struct {
	// Name identifies this reviewer as a PROFILE: what a per-pull-request
	// override names, what an advisory status context is derived from, and
	// what an operator reads in a listing.
	//
	// Optional on the binding's default reviewer, where empty reads as
	// DefaultReviewerProfileName; required on every alternate in
	// ProjectBinding.Reviewers. Names are unique within a binding. See
	// reviewer_profiles.go.
	Name string `json:"name,omitempty"`

	// CountsTowardMerge says whether this profile's verdicts decide whether a
	// change may land.
	//
	// A POINTER BECAUSE THE DEFAULT DEPENDS ON THE ROLE. Unset on the
	// binding's default reviewer means true, which is exactly what every
	// binding written before profiles existed meant; unset on an alternate
	// means false, so a reviewer added to try a new model is advisory until
	// someone decides its approvals should merge code. An advisory profile's
	// verdicts are posted and shown, and the gate neither approves nor blocks
	// on them.
	CountsTowardMerge *bool `json:"counts_toward_merge,omitempty"`

	// Agent is the ID of the tenant Agent this seat reviews as. It supplies
	// the backend, the model, the image and the reasoning effort; the seat
	// keeps only the forge identity. The Agent must declare the review role.
	//
	// EMPTY MEANS THIS PROJECT IS NOT SELF-REVIEWED, which is a legitimate
	// configuration and not a half-finished one: a project reviewed by
	// people, or by automation outside this module, binds no reviewer and
	// the review runner is never dispatched for it. The gate is unaffected
	// — it judges whatever verdicts arrive, from wherever they came.
	//
	// A SEAT NAMES AN AGENT, NOT A POOL. The seat is a forge identity, and
	// its failover chain is the binding's seat order (NextReviewerSeat), so a
	// switch of reviewer is always a switch of the login that posts.
	Agent string `json:"agent_id,omitempty"`

	// Login is the forge account the reviewer's token authenticates as.
	//
	// REQUIRED WHEN AN AGENT IS NAMED, and it is load-bearing twice over.
	// It is how the runner recognises its OWN previous verdicts when it asks
	// whether a head has already been reviewed — without it, the runner
	// cannot tell its own standing rejection from a human's and would review
	// every head again on every delivery. And it is how the gate's
	// independence rule is decided, through ReviewPolicy.Reviewers.
	Login string `json:"login,omitempty"`

	// TokenSecretRef names the tenant secret holding the REVIEWER's forge
	// token.
	//
	// REQUIRED WHEN AN AGENT IS NAMED, and deliberately separate from
	// ForgeConfig.TokenSecretRef, which is the account the worker commits
	// and pushes as. A verdict posted with the author's own token is a
	// change approving itself: the forge would record the author as the
	// reviewer, the independence rule would have nothing to distinguish, and
	// the whole point of an independent review would be lost to a
	// configuration convenience.
	TokenSecretRef string `json:"token_secret_ref,omitempty"`

	// StatusContext is the commit status the verdict is published under.
	//
	// On the default reviewer, empty takes DefaultReviewStatusContext, and
	// whatever it resolves to is the project's REQUIRED review context: every
	// profile that counts toward merge publishes there, so branch protection
	// requires one name however many reviewers the project has. On an
	// advisory profile, empty takes DefaultReviewStatusContext + "/" + Name,
	// so its status can never satisfy or block the required one. See
	// ProjectBinding.ReviewerProfile.
	StatusContext string `json:"status_context,omitempty"`

	// Endpoint is the root of the model server an opencode reviewer on a
	// local model is served from — the ollama URL as the reviewer's CONTAINER
	// reaches it, e.g. "http://gpu-host.internal:11434".
	//
	// REQUIRED WHEN THE SEAT'S AGENT IS AN OPENCODE REVIEWER ON A LOCAL MODEL
	// (model "ollama/<name>"), REFUSED OTHERWISE (CheckAgents). It is carried
	// per seat rather than taken from whatever the fleet node configures,
	// because the model a verdict is attributed to must be the model that
	// produced it: the runner hands opencode this endpoint as its only
	// provider, with the Agent's model pinned (opencodeProviderConfig).
	Endpoint string `json:"endpoint,omitempty"`

	// HostAffinity pins the reviewer session to one fleet node, by name or
	// host. Empty leaves placement to the fleet: any online node serving the
	// Agent's backend. When set it is STRICT — a node that is offline refuses
	// the review rather than letting it run somewhere that may not reach
	// Endpoint.
	HostAffinity string `json:"host_affinity,omitempty"`

	// PromptAppend is run-time prompt material appended after the reviewer
	// charter for every review this seat runs: what this project's reviews
	// must also check. At most orchestration.MaxPromptAppendBytes.
	PromptAppend string `json:"prompt_append,omitempty"`
}

// Configured reports whether this project is reviewed by an agent of its own.
func (r ReviewerConfig) Configured() bool { return strings.TrimSpace(r.Agent) != "" }

// Context is the commit status name the verdict is published under.
func (r ReviewerConfig) Context() string {
	if ctx := strings.TrimSpace(r.StatusContext); ctx != "" {
		return ctx
	}
	return DefaultReviewStatusContext
}

// Validate checks a reviewer could be spawned and could post as somebody.
//
// AN UNCONFIGURED REVIEWER IS VALID; a partly configured one is not. The
// failure this refuses is a project that looks reviewed and is not: a backend
// with no login posts verdicts nobody can attribute, and a backend with no
// token of its own posts them as the author.
func (r ReviewerConfig) Validate() error { return r.validateAs("reviewer") }

// validateAs is Validate with the field path the refusal names, so an
// alternate profile's error says which entry of reviewers[] it is about.
func (r ReviewerConfig) validateAs(field string) error {
	if err := orchestration.ValidatePromptAppend(field+".prompt_append", r.PromptAppend); err != nil {
		return err
	}
	if !r.Configured() {
		if strings.TrimSpace(r.Login) != "" || strings.TrimSpace(r.TokenSecretRef) != "" {
			return &InvalidInputError{
				Field:   field + ".agent_id",
				Message: "required; an identity and a token with no agent to use them review nothing",
			}
		}
		return nil
	}
	if strings.TrimSpace(r.Login) == "" {
		return &InvalidInputError{
			Field:   field + ".login",
			Message: "required; without it the runner cannot recognise its own standing verdict on a head",
		}
	}
	if strings.TrimSpace(r.TokenSecretRef) == "" {
		return &InvalidInputError{
			Field:   field + ".token_secret_ref",
			Message: "required; a reviewer using the author's token is a change approving itself",
		}
	}
	if !orchestration.ValidCatalogID(strings.TrimSpace(r.Agent)) {
		return &InvalidInputError{
			Field:   field + ".agent_id",
			Message: "must name an agent: 1-64 lowercase letters, digits, '.', '_' or '-'",
		}
	}
	if err := r.validateEndpoint(field); err != nil {
		return err
	}
	if name := strings.TrimSpace(r.Name); name != "" && !validReviewerProfileName(name) {
		return &InvalidInputError{
			Field:   field + ".name",
			Message: "must be 1-" + strconv.Itoa(maxReviewerProfileName) + " lowercase letters, digits, '.', '_' or '-', starting with a letter or digit",
		}
	}
	return nil
}

// ReviewerBackendOpencode is the backend a reviewer on a local model runs on:
// opencode, routed to the seat's Endpoint.
const ReviewerBackendOpencode = "opencode"

// ReviewsOnLocalModel reports whether agent runs opencode on a model a server
// the fleet operates serves, which is what a seat's Endpoint is for.
func ReviewsOnLocalModel(agent orchestration.Agent) bool {
	return strings.TrimSpace(agent.Backend) == ReviewerBackendOpencode &&
		orchestration.ModelProviderOf(agent.Model).Local()
}

// validateEndpoint checks that an endpoint, when one is written, is an http(s)
// server root. Whether the seat may carry one at all depends on its Agent, so
// that half is checked against the catalog (checkEndpointFor).
func (r ReviewerConfig) validateEndpoint(field string) error {
	endpoint := strings.TrimSpace(r.Endpoint)
	if endpoint == "" {
		return nil
	}
	if !validEndpointRoot(endpoint) {
		return &InvalidInputError{
			Field:   field + ".endpoint",
			Message: "must be the http(s) root of the model server the reviewer's container reaches, e.g. http://gpu-host.internal:11434",
		}
	}
	return nil
}

// checkEndpointFor checks the endpoint is present exactly when the seat's
// Agent runs opencode on a local model.
func (r ReviewerConfig) checkEndpointFor(field string, agent orchestration.Agent) error {
	endpoint := strings.TrimSpace(r.Endpoint)
	if !ReviewsOnLocalModel(agent) {
		if endpoint != "" {
			return &InvalidInputError{
				Field: field + ".endpoint",
				Message: "only a seat whose agent runs opencode on a local model (ollama/<name>) is handed an endpoint; " +
					"agent " + strconv.Quote(agent.ID) + " reaches its provider through its own configuration",
			}
		}
		return nil
	}
	if !validEndpointRoot(endpoint) {
		return &InvalidInputError{
			Field: field + ".endpoint",
			Message: "required for agent " + strconv.Quote(agent.ID) + " on " + strconv.Quote(agent.Model) +
				": the http(s) root of the model server its container reaches, e.g. http://gpu-host.internal:11434",
		}
	}
	return nil
}

// validEndpointRoot reports whether endpoint is an absolute http(s) URL.
func validEndpointRoot(endpoint string) bool {
	parsed, err := url.Parse(endpoint)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// ForgeConfigFor returns the forge configuration the REVIEWER acts through:
// the project's repository, reached with the reviewer's own token.
//
// It is the whole of the identity switch, and it is a value transformation
// rather than a second port: ForgeResolver already resolves whichever token a
// ForgeConfig names, so handing it this config gives an adapter authenticated
// as the reviewer without anything else in the module learning that two
// identities exist.
func (r ReviewerConfig) ForgeConfigFor(forge ForgeConfig) ForgeConfig {
	forge.TokenSecretRef = r.TokenSecretRef
	forge.TokenFromSecret = true
	return forge
}

// =============================================================================
// The review job's subject
// =============================================================================

// ReviewJobRunnerName is the name a review job selects its runner by.
const ReviewJobRunnerName = "review"

// ParamPullRequest is the JobConfig parameter naming the pull request a
// review job is about. Namespaced by its runner, per JobConfig.Params.
const ParamPullRequest = "review.pull_request"

// ParamReviewerProfile is the JobConfig parameter naming the reviewer profile
// a review job runs as. Absent or empty is the binding's default.
//
// IT IS WRITTEN BY THE DISPATCHER, FROM THE OPERATOR'S OVERRIDE RECORD, and
// nothing else sets it: the job's parameters are the engine's own record, not
// a payload's, and the runner resolves the name against the binding again so
// a profile removed after dispatch is refused rather than impersonated.
const ParamReviewerProfile = "review.profile"

// ReviewJobSubject is what one review job is about.
//
// THE PULL REQUEST NUMBER AND NOTHING ELSE. The head is deliberately absent,
// though the webhook that prompted the job knew one: a delivery is a hint,
// and by the time the job runs the author may have pushed again. A job
// carrying a head would review the tree that was current when the hook fired
// and post a verdict the gate then discards as stale — work spent to produce
// nothing. Reading the head from the forge at the moment the review starts
// costs one call and is always the head the verdict should bind to.
type ReviewJobSubject struct {
	PullRequest int

	// Profile names the reviewer profile the job runs as; empty is the
	// binding's default. See ParamReviewerProfile.
	Profile string
}

// Validate refuses a subject naming no pull request.
func (s ReviewJobSubject) Validate() error {
	if s.PullRequest <= 0 {
		return &InvalidInputError{
			Field:   ParamPullRequest,
			Message: "a review job is about one pull request; pull requests are numbered from one",
		}
	}
	return nil
}

// Params renders the subject as the job parameters that carry it.
func (s ReviewJobSubject) Params() map[string]string {
	params := map[string]string{ParamPullRequest: strconv.Itoa(s.PullRequest)}
	if profile := strings.TrimSpace(s.Profile); profile != "" {
		params[ParamReviewerProfile] = profile
	}
	return params
}

// ReviewLeaseSubject is what every review job of one pull request holds its
// lease on, so at most one of them runs at a time.
//
// ONE PULL REQUEST, ONE REVIEWER, AND THE LEASE IS WHAT MAKES IT ONE. A
// forge redelivers, and an opened pull request that is immediately labelled
// or retargeted produces several deliveries within a second; each of them
// starts a job, and without a shared subject each job gets a lease named
// after its own job id and contends with nothing. Two reviewers then read
// the same change, spend two model runs, and publish two commit statuses
// under one login — where the later write wins, so a green status can
// supersede a rejection that was filed while it was being produced. That is
// the exact failure the standing-verdict check cannot catch, because neither
// run has published anything yet when the other one looks.
//
// IT IS THE PULL REQUEST AND NOT THE HEAD, for two reasons. The head is not
// knowable when the job is started — it is read from the forge inside the
// job, which is the whole point of ReviewJobSubject carrying no head — and
// serializing on the pull request is strictly stronger anyway: it excludes
// two reviewers of one head, which is the reported failure, and also two
// reviewers of two heads of one pull request, which burns a model run to
// produce a verdict the next push invalidates. A head that moves while the
// holder is reviewing is not starved by this: the holder sees the move when
// it re-reads the pull request to publish, and reviews the new head itself.
// See reviewRun.publish.
//
// The tenant and the project are already segments of the lease key; they are
// repeated in the subject so a person reading a lease listing can see what
// the record is about without reconstructing it from the key.
func ReviewLeaseSubject(projectID string, pullRequest int) string {
	return "review:" + projectID + ":#" + strconv.Itoa(pullRequest)
}

// ReviewJobSubjectFrom reads the subject off a job configuration.
func ReviewJobSubjectFrom(cfg *orchestration.JobConfig) (ReviewJobSubject, error) {
	raw, ok := cfg.Param(ParamPullRequest)
	if !ok {
		return ReviewJobSubject{}, &InvalidInputError{
			Field:   ParamPullRequest,
			Message: "required; a review job with no pull request has nothing to review",
		}
	}
	number, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return ReviewJobSubject{}, &InvalidInputError{
			Field:   ParamPullRequest,
			Message: "must be a pull request number, got " + strconv.Quote(raw),
		}
	}
	subject := ReviewJobSubject{PullRequest: number}
	if profile, named := cfg.Param(ParamReviewerProfile); named {
		subject.Profile = strings.TrimSpace(profile)
	}
	return subject, subject.Validate()
}
