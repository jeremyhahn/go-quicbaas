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

import "strings"

// CIGateStatusContext is the status context the company CI template reports
// for its aggregate gate on a pull request. Onboarding a repository requires
// it on the protected branch beside DefaultReviewStatusContext.
const CIGateStatusContext = "CI / CI Gate (pull_request)"

// RepoOnboardStatusContexts are the status contexts a repository onboarded by
// RepoOnboardRequest must require on its protected branch, whatever its
// template requires.
var RepoOnboardStatusContexts = []string{CIGateStatusContext, DefaultReviewStatusContext}

// The steps a repository onboarding runs, in the order it runs them.
const (
	OnboardStepSigningKey    = "signing_key"
	OnboardStepForgeToken    = "forge_token"
	OnboardStepWebhookSecret = "webhook_secret"
	OnboardStepProject       = "project"
	OnboardStepBinding       = "binding"
	OnboardStepForge         = "forge"
	OnboardStepProtection    = "protection"
	OnboardStepWorkflows     = "workflows"
	OnboardStepPreflight     = "preflight"
)

// OnboardOutcome is what one onboarding step did.
type OnboardOutcome string

const (
	// OnboardCreated means the step wrote what was missing.
	OnboardCreated OnboardOutcome = "created"

	// OnboardUnchanged means the step found it already in place and wrote
	// nothing.
	OnboardUnchanged OnboardOutcome = "unchanged"

	// OnboardFinding means the step could not, or may not, put it right
	// itself: the step's Detail says what is wrong and its Remedy who fixes
	// it and how.
	OnboardFinding OnboardOutcome = "finding"
)

// RepoOnboardRequest asks for one forge repository to be onboarded like an
// existing, bound project: its project created, its secrets and forge
// provisioned and its binding written. Every step checks before it acts, so
// running the same request twice changes nothing the second time.
type RepoOnboardRequest struct {
	// Repo is the repository's name on the forge, one path segment. The
	// forge, owner and everything else about the binding come from Like.
	Repo string `json:"repo"`

	// Like is the ID of the bound project the new one is modelled on.
	Like string `json:"like"`

	// MaxConcurrency bounds the new project's concurrent jobs. Zero takes
	// the template's.
	MaxConcurrency int `json:"max_concurrency,omitempty"`

	// Paused binds without switching the new project's autonomy on.
	Paused bool `json:"paused,omitempty"`

	// Preflight runs the binding's verification commands once, on the
	// default branch, in the worker's image, before autonomy is switched
	// on. A red preflight leaves autonomy off and reports the failing
	// lines; Paused still wins over a green one.
	Preflight bool `json:"preflight,omitempty"`
}

// Validate refuses a request that names no repository, a repository that is
// not one path segment, or no template.
func (r *RepoOnboardRequest) Validate() error {
	repo := strings.TrimSpace(r.Repo)
	switch {
	case repo == "":
		return &InvalidInputError{Field: "repo", Message: "name the repository to onboard"}
	case repo != r.Repo, strings.ContainsAny(repo, "/\\ \t"), repo == ".", repo == "..":
		return &InvalidInputError{Field: "repo", Message: "the repository name must be one path segment: " + r.Repo}
	case strings.TrimSpace(r.Like) == "":
		return &InvalidInputError{Field: "like", Message: "name the bound project the repository is onboarded like"}
	case r.MaxConcurrency < 0:
		return &InvalidInputError{Field: "max_concurrency", Message: "must not be negative"}
	}
	return nil
}

// ForgeTokenRef is the tenant secret a repository's forge token is stored
// under. The operator stores it before onboarding; onboarding only checks it.
func ForgeTokenRef(repo string) string {
	return "sdlc/forge/" + repo + "/token"
}

// WebhookSecretRef is the tenant secret a repository's webhook HMAC key is
// stored under. Onboarding generates it when it is missing.
func WebhookSecretRef(repo string) string {
	return "sdlc/webhook/" + repo
}

// SigningKeyName is the SSH key a repository's agent signs its commits with.
func SigningKeyName(repo string) string {
	return "agent-" + repo + "-signing"
}

// RepoOnboardStep is one step's outcome.
type RepoOnboardStep struct {
	// Step is one of the OnboardStep* names.
	Step string `json:"step"`

	// Outcome is what the step did.
	Outcome OnboardOutcome `json:"outcome"`

	// Detail says what the step created, or what it found wrong.
	Detail string `json:"detail,omitempty"`

	// Remedy says what fixes a finding. Empty unless Outcome is
	// OnboardFinding.
	Remedy string `json:"remedy,omitempty"`
}

// RepoOnboardingReport is every step's outcome and the conformance the
// repository is in afterwards.
type RepoOnboardingReport struct {
	TenantID string `json:"tenant_id"`

	// ProjectID is the onboarded project, empty when the project could not
	// be created.
	ProjectID string `json:"project_id,omitempty"`

	// Repo is the onboarded repository as "owner/repo".
	Repo string `json:"repo"`

	// Steps are the steps in the order they ran.
	Steps []RepoOnboardStep `json:"steps"`

	// Conformance is the repository's state after onboarding, absent when
	// the project was never bound.
	Conformance *ConformanceReport `json:"conformance,omitempty"`
}

// Findings returns the steps that ended in a finding.
func (r *RepoOnboardingReport) Findings() []RepoOnboardStep {
	out := make([]RepoOnboardStep, 0, len(r.Steps))
	for _, step := range r.Steps {
		if step.Outcome == OnboardFinding {
			out = append(out, step)
		}
	}
	return out
}

// Step returns the named step's outcome, and false when it did not run.
func (r *RepoOnboardingReport) Step(name string) (RepoOnboardStep, bool) {
	for _, step := range r.Steps {
		if step.Step == name {
			return step, true
		}
	}
	return RepoOnboardStep{}, false
}
