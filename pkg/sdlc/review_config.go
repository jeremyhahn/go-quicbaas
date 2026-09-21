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

// ReviewerConfig names the agent that reviews this project's pull requests.
//
// It lives beside WorkerConfig on the binding and mirrors its shape, because
// it answers the same question for the other half of the pipeline: which
// agent, on which model, in which image. What it adds is an IDENTITY — a
// reviewer, unlike a worker, has to be someone on the forge.
type ReviewerConfig struct {
	// Backend is the ACP agent backend the reviewer session runs on.
	//
	// EMPTY MEANS THIS PROJECT IS NOT SELF-REVIEWED, which is a legitimate
	// configuration and not a half-finished one: a project reviewed by
	// people, or by automation outside this module, binds no reviewer and
	// the review runner is never dispatched for it. The gate is unaffected
	// — it judges whatever verdicts arrive, from wherever they came.
	Backend string `json:"backend,omitempty"`

	// Model is the model that backend runs. Empty takes the backend's own
	// default.
	Model string `json:"model,omitempty"`

	// Image is the container image the reviewer session runs in. It needs
	// git and the project's toolchain, because the reviewer reads the change
	// in a real checkout rather than from a summary of it.
	Image string `json:"image,omitempty"`

	// Login is the forge account the reviewer's token authenticates as.
	//
	// REQUIRED WHEN A BACKEND IS NAMED, and it is load-bearing twice over.
	// It is how the runner recognises its OWN previous verdicts when it asks
	// whether a head has already been reviewed — without it, the runner
	// cannot tell its own standing rejection from a human's and would review
	// every head again on every delivery. And it is how the gate's
	// independence rule is decided, through ReviewPolicy.Reviewers.
	Login string `json:"login,omitempty"`

	// TokenSecretRef names the tenant secret holding the REVIEWER's forge
	// token.
	//
	// REQUIRED WHEN A BACKEND IS NAMED, and deliberately separate from
	// ForgeConfig.TokenSecretRef, which is the account the worker commits
	// and pushes as. A verdict posted with the author's own token is a
	// change approving itself: the forge would record the author as the
	// reviewer, the independence rule would have nothing to distinguish, and
	// the whole point of an independent review would be lost to a
	// configuration convenience.
	TokenSecretRef string `json:"token_secret_ref,omitempty"`

	// StatusContext is the commit status the verdict is published under.
	// Empty takes DefaultReviewStatusContext.
	StatusContext string `json:"status_context,omitempty"`

	// ReasoningEffort is how hard the reviewer's model thinks per review, in
	// the backend's own vocabulary; empty takes the backend's default. For
	// codex it is `model_reasoning_effort`: minimal, low, medium, high or
	// xhigh. It is a binding setting because it is a cost setting: a fleet
	// generating reviews by the dozen while its pipelines are being brought
	// up runs at medium, and turns it up per project once the reviews are
	// worth it.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

// reviewerEfforts is the vocabulary codex accepts for model_reasoning_effort.
// A value outside it is refused at bind time rather than by every review.
var reviewerEfforts = map[string]bool{
	"minimal": true, "low": true, "medium": true, "high": true, "xhigh": true,
}

// ExtraArgs is what the reviewer's spawn adds to the backend's command line
// for this configuration: for codex, the reasoning-effort override as a
// `-c` config override, which is the one way the CLI takes it. Other
// backends take nothing here; the field is validated for all of them so a
// typo is caught wherever it is written.
func (r ReviewerConfig) ExtraArgs() []string {
	effort := strings.TrimSpace(r.ReasoningEffort)
	if effort == "" || strings.TrimSpace(r.Backend) != "codex" {
		return nil
	}
	return []string{"-c", "model_reasoning_effort=\"" + effort + "\""}
}

// Configured reports whether this project is reviewed by an agent of its own.
func (r ReviewerConfig) Configured() bool { return strings.TrimSpace(r.Backend) != "" }

// Context is the commit status name the verdict is published under.
func (r ReviewerConfig) Context() string {
	if ctx := strings.TrimSpace(r.StatusContext); ctx != "" {
		return ctx
	}
	return DefaultReviewStatusContext
}

// ModelFamily is the family behind the reviewer, for the independence rule.
func (r ReviewerConfig) ModelFamily() string { return ModelFamily(r.Backend, r.Model) }

// Validate checks a reviewer could be spawned and could post as somebody.
//
// AN UNCONFIGURED REVIEWER IS VALID; a partly configured one is not. The
// failure this refuses is a project that looks reviewed and is not: a backend
// with no login posts verdicts nobody can attribute, and a backend with no
// token of its own posts them as the author.
func (r ReviewerConfig) Validate() error {
	if !r.Configured() {
		if strings.TrimSpace(r.Login) != "" || strings.TrimSpace(r.TokenSecretRef) != "" {
			return &InvalidInputError{
				Field:   "reviewer.backend",
				Message: "required; an identity and a token with no agent to use them review nothing",
			}
		}
		return nil
	}
	if strings.TrimSpace(r.Login) == "" {
		return &InvalidInputError{
			Field:   "reviewer.login",
			Message: "required; without it the runner cannot recognise its own standing verdict on a head",
		}
	}
	if strings.TrimSpace(r.TokenSecretRef) == "" {
		return &InvalidInputError{
			Field:   "reviewer.token_secret_ref",
			Message: "required; a reviewer using the author's token is a change approving itself",
		}
	}
	if effort := strings.TrimSpace(r.ReasoningEffort); effort != "" && !reviewerEfforts[effort] {
		return &InvalidInputError{
			Field:   "reviewer.reasoning_effort",
			Message: "must be one of minimal, low, medium, high or xhigh",
		}
	}
	return nil
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
	return map[string]string{ParamPullRequest: strconv.Itoa(s.PullRequest)}
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
	return subject, subject.Validate()
}
