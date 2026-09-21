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
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// ProjectBinding — the forge attached to an orchestration Project
// =============================================================================
//
// A binding REFERENCES a Project by id and is the project's AUTONOMOUS
// DEVELOPMENT configuration: which forge repository it delivers to, which
// SDLC it follows, which agent writes the code, what verifies the work, how
// its pull requests are judged, how many review rounds it may take, and how
// its issues rank against other projects'. Everything else a project is — its
// name, its workspace, its repository URL, its commit author and signing
// policy — lives on the orchestration Project, and this record never repeats
// it.

// ForgeType identifies a forge platform.
type ForgeType string

const (
	// ForgeGitea is a Gitea instance.
	ForgeGitea ForgeType = "gitea"

	// ForgeGitHub is GitHub.
	ForgeGitHub ForgeType = "github"

	// ForgeGitLab is a GitLab instance.
	ForgeGitLab ForgeType = "gitlab"
)

// Label keys are the module's own vocabulary; a binding's Labels map each to
// the forge label that displays it. Labels are DISPLAY OUTPUT, never a lock:
// the module reads quicbaas records only and moves labels so a person
// watching the issue sees where its work stands.
const (
	// LabelInProgress marks an issue an attempt is working on.
	LabelInProgress = "in_progress"

	// LabelInReview marks an issue whose pull request is under review.
	LabelInReview = "in_review"

	// LabelMerged marks an issue whose pull request has landed.
	LabelMerged = "merged"

	// LabelBlocked marks an issue whose task is parked for a person.
	LabelBlocked = "blocked"

	// LabelNeedsInfo marks an issue whose task waits on a question.
	LabelNeedsInfo = "needs_info"

	// LabelEscalated marks an issue the engine gave up on.
	LabelEscalated = "escalated"
)

// ForgeConfig names the repository a project delivers to and how the module
// reaches it.
type ForgeConfig struct {
	// Type selects the adapter.
	Type ForgeType `json:"type"`

	// BaseURL is the forge's API root.
	BaseURL string `json:"base_url"`

	// Owner and Repo name the repository on the forge.
	Owner string `json:"owner"`
	Repo  string `json:"repo"`

	// Labels maps the module's label keys (LabelInProgress, ...) to the forge
	// labels that display them.
	Labels map[string]string `json:"labels,omitempty"`

	// TriggerLabel is the label an OPEN issue must carry before the module
	// adopts it as a Task: the explicit per-issue hand-over. Compared
	// exactly, case-sensitively. Required.
	TriggerLabel string `json:"trigger_label"`

	// WebhookSecretRef names the tenant secret holding the webhook HMAC key.
	// Empty means the project is polled only.
	WebhookSecretRef string `json:"webhook_secret_ref,omitempty"`

	// TokenSecretRef names the tenant secret holding the forge API token.
	TokenSecretRef string `json:"token_secret_ref,omitempty"`

	// SSHHostKey pins the forge's SSH host public key: one OpenSSH
	// public-key line, "<type> <base64> [comment]", as `ssh-keyscan` prints
	// after the host field.
	//
	// IT IS REQUIRED FOR AN SSH CHECKOUT AND IGNORED FOR AN HTTPS ONE. The
	// worker clones inside a container that has no known_hosts and never
	// learns one; the daemon writes this key into the container's
	// known_hosts and runs its ssh with StrictHostKeyChecking=yes. Without
	// it the attempt is refused before any container starts, because the
	// alternative — accepting whatever key answers — would hand a worker
	// holding a live signing agent to whatever host held the forge's
	// address at that moment.
	SSHHostKey string `json:"ssh_host_key,omitempty"`
}

// RepoRef returns the repository reference the config names.
func (c ForgeConfig) RepoRef() RepoRef { return RepoRef{Owner: c.Owner, Repo: c.Repo} }

// Label returns the forge label displaying key, or empty when the binding
// names none.
func (c ForgeConfig) Label(key string) string {
	return strings.TrimSpace(c.Labels[key])
}

// OwnedLabels returns every forge label the module writes for this binding
// — the display labels plus the trigger — lowercased for comparison.
func (c ForgeConfig) OwnedLabels() map[string]bool {
	owned := make(map[string]bool, len(c.Labels)+1)
	for _, label := range c.Labels {
		if l := strings.ToLower(strings.TrimSpace(label)); l != "" {
			owned[l] = true
		}
	}
	if trigger := strings.ToLower(strings.TrimSpace(c.TriggerLabel)); trigger != "" {
		owned[trigger] = true
	}
	return owned
}

// Validate checks the config for what the module needs to reach the forge.
func (c ForgeConfig) Validate() error {
	switch {
	case c.Type == "":
		return &InvalidInputError{Field: "forge.type", Message: "must name a forge"}
	case strings.TrimSpace(c.BaseURL) == "":
		return &InvalidInputError{Field: "forge.base_url", Message: "must name the forge API root"}
	case strings.TrimSpace(c.Owner) == "" || strings.TrimSpace(c.Repo) == "":
		return &InvalidInputError{Field: "forge.owner/repo", Message: "must name the repository"}
	case strings.TrimSpace(c.TriggerLabel) == "":
		return &InvalidInputError{
			Field:   "forge.trigger_label",
			Message: "required; without it no issue is ever handed to the module",
		}
	case strings.TrimSpace(c.TokenSecretRef) == "":
		return &InvalidInputError{
			Field:   "forge.token_secret_ref",
			Message: "required; an adapter with no token cannot read the forge",
		}
	}
	if err := c.validateSSHHostKey(); err != nil {
		return err
	}
	return c.validateTriggerLabel()
}

// validateSSHHostKey refuses a pinned host key that is not one OpenSSH
// public-key line. It is checked ON BIND rather than at clone time so a
// mistyped key is an error the operator sees while binding, not a container
// that dies at 3am with an ssh message.
//
// An empty key is accepted here: whether one is REQUIRED depends on the
// project's repository URL, which lives on the orchestration Project and not
// on this record, so that half is decided where the checkout is prepared.
func (c ForgeConfig) validateSSHHostKey() error {
	if strings.TrimSpace(c.SSHHostKey) == "" {
		return nil
	}
	if err := orchestration.ValidateSSHHostKey(c.SSHHostKey); err != nil {
		var hostKey *orchestration.SSHHostKeyError
		if errors.As(err, &hostKey) {
			return &InvalidInputError{Field: hostKey.Field(), Message: hostKey.Message}
		}
		return err
	}
	return nil
}

// validateTriggerLabel refuses a trigger that is also a display label.
func (c ForgeConfig) validateTriggerLabel() error {
	trigger := strings.ToLower(strings.TrimSpace(c.TriggerLabel))
	for key, label := range c.Labels {
		if strings.ToLower(strings.TrimSpace(label)) == trigger {
			return &TriggerLabelCollisionError{Key: key, Label: label}
		}
	}
	return nil
}

// ReviewPolicy decides when a pull request may land.
type ReviewPolicy struct {
	// RequiredApprovals is how many independent approvals the current head
	// needs. Must be at least one.
	RequiredApprovals int `json:"required_approvals"`

	// RequireIndependentReviewer refuses approvals from the model family
	// that wrote the code.
	RequireIndependentReviewer bool `json:"require_independent_reviewer"`

	// RequireGreenChecks refuses a merge until every check on the head
	// finished and succeeded.
	RequireGreenChecks bool `json:"require_green_checks"`

	// ChecksSettleWindow is how long a red check must stay red before the
	// module reads it as the change's own failure rather than a rerun in
	// flight. Zero takes DefaultChecksSettleWindow.
	ChecksSettleWindow time.Duration `json:"checks_settle_window,omitempty"`

	// Reviewers maps a forge login to the model family behind it, so the
	// independence rule can be decided for automated reviewers.
	Reviewers map[string]string `json:"reviewers,omitempty"`
}

// ReviewerModelFamily returns the model family recorded for a reviewer login.
func (p ReviewPolicy) ReviewerModelFamily(login string) string {
	return p.Reviewers[login]
}

// SettleWindow returns the configured settle window or the default.
func (p ReviewPolicy) SettleWindow() time.Duration {
	if p.ChecksSettleWindow <= 0 {
		return DefaultChecksSettleWindow
	}
	return p.ChecksSettleWindow
}

// Validate checks the policy would ever let a reviewed change land.
func (p ReviewPolicy) Validate() error {
	if p.RequiredApprovals <= 0 {
		return &InvalidInputError{
			Field:   "review.required_approvals",
			Message: "must be at least one; zero would merge unreviewed code",
		}
	}
	if p.ChecksSettleWindow < 0 {
		return &InvalidInputError{Field: "review.checks_settle_window", Message: "must not be negative"}
	}
	for login, family := range p.Reviewers {
		if strings.TrimSpace(login) == "" || strings.TrimSpace(family) == "" {
			return &InvalidInputError{
				Field:   "review.reviewers",
				Message: "every entry maps a non-empty login to a non-empty model family",
			}
		}
	}
	return nil
}

// WorkerConfig names the agent that writes the code for this project.
//
// It lives on the binding rather than the Project because it is an SDLC
// decision: the same repository could be worked by a different model under a
// different SDLC, and the review policy's independence rule is decided against
// this backend's model family.
type WorkerConfig struct {
	// Backend is the ACP agent backend the worker session runs on, e.g.
	// "claude". Required: a binding with no backend cannot spawn a worker.
	Backend string `json:"backend"`

	// Model is the model that backend runs, e.g. "claude-opus-5". Empty
	// takes the backend's own default.
	Model string `json:"model"`

	// Image is the container image the worker session runs in. Naming one
	// requires the sandbox for this project's workers, and the image must
	// carry what the runner's checkout and the binding's verification
	// commands need — git, make and the project's toolchain
	// (deployments/agents/software-factory-worker). Empty leaves the sandbox
	// decision to the operator's own policy, which is deny-by-default on a
	// node that serves tenants.
	Image string `json:"image,omitempty"`
}

// Validate checks a worker could be spawned from this configuration.
func (w WorkerConfig) Validate() error {
	if strings.TrimSpace(w.Backend) == "" {
		return &InvalidInputError{
			Field:   "worker.backend",
			Message: "required; autonomous development has no worker to spawn without one",
		}
	}
	return nil
}

// RankingPolicy is a project's contribution to task ordering.
type RankingPolicy struct {
	// DefaultPriority shifts every task in this project. Zero is no shift.
	DefaultPriority int `json:"default_priority,omitempty"`

	// AnalyzerName pins the dependency analyzer; empty lets the registered
	// analyzers decide by precedence.
	AnalyzerName string `json:"analyzer_name,omitempty"`

	// DeclaredDependents lists projects that depend on this one, for the
	// declared analyzer.
	DeclaredDependents []string `json:"declared_dependents,omitempty"`
}

// Validate checks the ranking names an analyzer that exists.
func (p RankingPolicy) Validate() error {
	if p.AnalyzerName == "" {
		return nil
	}
	_, err := LookupDependencyAnalyzer(p.AnalyzerName)
	return err
}

// DefaultMaxReviewRounds bounds how many times review may send the work back
// before the task is escalated to a person. Three is the company's own bar
// (handbook conventions/sdlc.md): past it the change needs a human, not
// another round.
const DefaultMaxReviewRounds = 3

// ProjectBinding attaches a forge repository and its delivery policy to an
// orchestration Project.
type ProjectBinding struct {
	// ID is the DAO entity id, derived from ProjectID. Never set by callers.
	ID uint64 `json:"-"`

	// ProjectID references the orchestration Project.
	ProjectID string `json:"project_id"`

	// Forge names the repository and how the module reaches it.
	Forge ForgeConfig `json:"forge"`

	// Strategy names the registered SDLC the project follows: how its
	// branches are cut, whether its changes are reviewed, how they land and
	// what follows. Required, and resolved fail-closed.
	Strategy string `json:"strategy"`

	// Worker is the agent that writes the code.
	Worker WorkerConfig `json:"worker"`

	// Verification are the shell commands that must pass before the work is
	// offered for review, run in the worker's checkout in order. Empty means
	// the forge's own checks are the only gate.
	Verification []string `json:"verification,omitempty"`

	// Review decides when a pull request may land.
	Review ReviewPolicy `json:"review"`

	// Reviewer names the agent that reviews this project's pull requests.
	// A zero value means the project is not self-reviewed; see
	// ReviewerConfig.
	Reviewer ReviewerConfig `json:"reviewer,omitempty"`

	// Modules says how the worker reaches this project's private Go modules.
	// Empty for a project with none. See ModulePolicy.
	Modules ModulePolicy `json:"modules,omitempty"`

	// MaxReviewRounds bounds how many times review may send the work back
	// before the task is escalated to a person. Zero takes
	// DefaultMaxReviewRounds.
	MaxReviewRounds int `json:"max_review_rounds,omitempty"`

	// Ranking is this project's contribution to task ordering.
	Ranking RankingPolicy `json:"ranking"`

	// Triage names the agent that triages this project's open issues.
	//
	// EMPTY FALLS BACK TO Worker, which is what every project did before this
	// existed. Triage is a READING task — it decides whether an issue is
	// already satisfied, duplicated or moot — and nothing it does writes code,
	// so a project can point it at a cheaper or locally hosted model without
	// changing who writes the code. Owner direction 2026-09-15.
	Triage WorkerConfig `json:"triage,omitempty"`

	// TriageMayClose lets triage CLOSE an issue it found is already done or
	// otherwise not work, rather than commenting and leaving it.
	//
	// OFF BY DEFAULT, AND THE DEFAULT IS THE CAREFUL ONE. A label is
	// reversible and carries no social weight; closing somebody's issue does,
	// which is why the charter forbade proposing it. Turning this on is a
	// project saying "my stale reports should be closed, and I accept an agent
	// deciding which those are" — so it is per project rather than fleet-wide,
	// and the charter tells the agent which world it is in rather than letting
	// it guess.
	//
	// It reaches ONLY the already-done case. A duplicate keeps its comment and
	// its reference, needs-info keeps its label, and an issue needing a
	// person's decision is still left for that person: those are not stale
	// reports and closing them would lose the thing that makes them useful.
	TriageMayClose bool `json:"triage_may_close,omitempty"`

	// Discovery selects how this project learns that something happened on
	// its forge: `poll`, `webhook` or `both`. Empty takes DiscoveryBoth —
	// see DiscoveryMode for why that is the default.
	Discovery DiscoveryMode `json:"discovery,omitempty"`

	// Readiness is what an issue must satisfy before it becomes a Task. See
	// ReadinessPolicy; the zero value is the shipped default.
	Readiness ReadinessPolicy `json:"readiness,omitempty"`

	// Onboarding declares what provisioning writes to the forge and what
	// conformance checks. Every field defaults, so a binding that says
	// nothing about onboarding still onboards correctly.
	Onboarding OnboardingConfig `json:"onboarding,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EntityID implements the DAO entity contract.
func (b *ProjectBinding) EntityID() uint64 { return b.ID }

// SetEntityID implements the DAO entity contract.
func (b *ProjectBinding) SetEntityID(id uint64) { b.ID = id }

// Repo returns the bound repository.
func (b *ProjectBinding) Repo() RepoRef { return b.Forge.RepoRef() }

// Validate checks the binding is complete enough to deliver with.
func (b *ProjectBinding) Validate() error {
	if b == nil {
		return &InvalidInputError{Field: "binding", Message: "must not be nil"}
	}
	if err := validateKeySegment("project_id", b.ProjectID); err != nil {
		return err
	}
	if err := b.Forge.Validate(); err != nil {
		return err
	}
	if _, err := LookupStrategy(b.Strategy); err != nil {
		return err
	}
	if err := b.Worker.Validate(); err != nil {
		return err
	}
	if err := b.validateVerification(); err != nil {
		return err
	}
	if err := b.Review.Validate(); err != nil {
		return err
	}
	if err := b.Reviewer.Validate(); err != nil {
		return err
	}
	if b.MaxReviewRounds < 0 {
		return &InvalidInputError{Field: "max_review_rounds", Message: "must not be negative"}
	}
	if err := b.Modules.Validate(); err != nil {
		return err
	}
	if err := b.Discovery.Validate(); err != nil {
		return err
	}
	if err := b.Readiness.Validate(); err != nil {
		return err
	}
	if err := b.Onboarding.Validate(); err != nil {
		return err
	}
	return b.Ranking.Validate()
}

// validateVerification refuses a blank command: a step that runs nothing
// would report success it never earned.
func (b *ProjectBinding) validateVerification() error {
	for i, command := range b.Verification {
		if strings.TrimSpace(command) == "" {
			return &InvalidInputError{
				Field:   "verification[" + strconv.Itoa(i) + "]",
				Message: "must be a command; a blank step would pass without running anything",
			}
		}
	}
	return nil
}

// ReviewRounds returns the configured bound or the default.
func (b *ProjectBinding) ReviewRounds() int {
	if b.MaxReviewRounds <= 0 {
		return DefaultMaxReviewRounds
	}
	return b.MaxReviewRounds
}
