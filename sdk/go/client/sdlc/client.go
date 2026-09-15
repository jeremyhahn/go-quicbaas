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

// Package sdlc is the SDK client for the SDLC module of the Software
// Factory: binding a forge repository to an orchestration Project and
// reading what a Task's attempt delivered. Tasks, Jobs and Sessions
// themselves are the orchestrator client's; the module's records only
// reference them.
package sdlc

import (
	"context"
	"fmt"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// serviceName is the transport service the SDLC verbs route under.
const serviceName = "quicbaas.SDLC"

// Client is the SDLC module's SDK surface.
type Client interface {
	// Bind attaches a forge binding to a project, replacing any earlier one.
	Bind(ctx context.Context, req *BindRequest) (*ProjectBinding, error)

	// Unbind detaches the project's binding.
	Unbind(ctx context.Context, tenantID, projectID string) error

	// Binding returns the project's binding.
	Binding(ctx context.Context, tenantID, projectID string) (*ProjectBinding, error)

	// Delivery returns what the task's attempt published.
	Delivery(ctx context.Context, tenantID, taskID string) (*Delivery, error)

	// ListForges returns the forge adapters this build carries, so a caller
	// can show an unavailable platform disabled with the reason rather than
	// hide it. Build capability, not tenant data: no tenant is derived.
	ListForges(ctx context.Context) (*ListForgesResponse, error)

	// Onboard provisions the bound repository's forge configuration — its
	// webhook and its label vocabulary — and returns what it did together
	// with the conformance that follows.
	//
	// IDEMPOTENT, so onboarding a fleet is a loop nobody has to keep notes
	// about: a repository already in the desired state is left alone, and one
	// whose webhook was deleted by hand gets it back. This is also the
	// EXPLICIT REPAIR for a conformance finding — Conformance never repairs.
	Onboard(ctx context.Context, tenantID, projectID string) (*OnboardingReport, error)

	// Conformance reports whether the project is correctly onboarded, and
	// what is wrong if it is not. It changes nothing.
	Conformance(ctx context.Context, tenantID, projectID string) (*ConformanceReport, error)
}

// ForgeConfig names the repository a project delivers to.
type ForgeConfig struct {
	Type             string            `json:"type" yaml:"type"`
	BaseURL          string            `json:"base_url" yaml:"base_url"`
	Owner            string            `json:"owner" yaml:"owner"`
	Repo             string            `json:"repo" yaml:"repo"`
	Labels           map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	TriggerLabel     string            `json:"trigger_label" yaml:"trigger_label"`
	WebhookSecretRef string            `json:"webhook_secret_ref,omitempty" yaml:"webhook_secret_ref,omitempty"`
	TokenSecretRef   string            `json:"token_secret_ref,omitempty" yaml:"token_secret_ref,omitempty"`

	// SSHHostKey pins the forge's SSH host public key: one OpenSSH
	// public-key line, required when the project is cloned over ssh so the
	// worker's checkout verifies the forge instead of learning it.
	SSHHostKey string `json:"ssh_host_key,omitempty" yaml:"ssh_host_key,omitempty"`
}

// ReviewPolicy decides when a pull request may land.
type ReviewPolicy struct {
	RequiredApprovals          int               `json:"required_approvals" yaml:"required_approvals"`
	RequireIndependentReviewer bool              `json:"require_independent_reviewer" yaml:"require_independent_reviewer"`
	RequireGreenChecks         bool              `json:"require_green_checks" yaml:"require_green_checks"`
	ChecksSettleWindow         time.Duration     `json:"checks_settle_window,omitempty" yaml:"checks_settle_window,omitempty"`
	Reviewers                  map[string]string `json:"reviewers,omitempty" yaml:"reviewers,omitempty"`
}

// ReviewerConfig names the agent that reviews this project's pull requests.
//
// AN EMPTY BACKEND MEANS THE PROJECT IS NOT SELF-REVIEWED, which is a
// legitimate configuration rather than a half-finished one: a project reviewed
// by people, or by automation outside this module, binds no reviewer and the
// review runner is never dispatched for it.
//
// IT IS PART OF THE BINDING BECAUSE A BIND REPLACES THE WHOLE RECORD. A client
// that cannot carry this field cannot configure a reviewer, cannot show an
// operator that one is missing, and — worse — erases one that was already set
// the next time the project is bound through it.
type ReviewerConfig struct {
	Backend string `json:"backend,omitempty" yaml:"backend,omitempty"`
	Model   string `json:"model,omitempty" yaml:"model,omitempty"`

	// Image is the container image the reviewer session runs in. It needs
	// git and the project's toolchain: the reviewer reads the change in a
	// real checkout rather than from a summary of it.
	Image string `json:"image,omitempty" yaml:"image,omitempty"`

	// Login is the forge account the reviewer's token authenticates as.
	// Required when a backend is named: it is how the runner recognises its
	// own standing verdicts, and how the independence rule is decided.
	Login string `json:"login,omitempty" yaml:"login,omitempty"`

	// TokenSecretRef names the tenant secret holding the REVIEWER's forge
	// token — deliberately separate from ForgeConfig.TokenSecretRef, which
	// is the account the worker commits as. A verdict posted with the
	// author's own token is a change approving itself.
	TokenSecretRef string `json:"token_secret_ref,omitempty" yaml:"token_secret_ref,omitempty"`

	// StatusContext is the commit status the verdict is published under.
	// Empty takes the module default.
	StatusContext string `json:"status_context,omitempty" yaml:"status_context,omitempty"`
}

// WorkerConfig names the agent that writes the code for this project.
type WorkerConfig struct {
	Backend string `json:"backend" yaml:"backend"`
	Model   string `json:"model" yaml:"model"`

	// Image is the container image the worker session runs in. Naming one
	// requires the sandbox for this project's workers: the image's
	// entrypoint clones the project and its context repositories from the
	// workspace spec the runner mounts.
	Image string `json:"image,omitempty" yaml:"image,omitempty"`
}

// RankingPolicy is a project's contribution to task ordering.
type RankingPolicy struct {
	DefaultPriority    int      `json:"default_priority,omitempty" yaml:"default_priority,omitempty"`
	AnalyzerName       string   `json:"analyzer_name,omitempty" yaml:"analyzer_name,omitempty"`
	DeclaredDependents []string `json:"declared_dependents,omitempty" yaml:"declared_dependents,omitempty"`
}

// ProjectBinding attaches a forge repository and its delivery policy to an
// orchestration Project.
type ProjectBinding struct {
	ProjectID string      `json:"project_id" yaml:"project_id"`
	Forge     ForgeConfig `json:"forge" yaml:"forge"`

	// Strategy names the registered SDLC the project follows.
	Strategy string `json:"strategy" yaml:"strategy"`

	// Worker is the agent that writes the code.
	Worker WorkerConfig `json:"worker" yaml:"worker"`

	// Verification are the commands that must pass before review, in order.
	Verification []string `json:"verification,omitempty" yaml:"verification,omitempty"`

	Review ReviewPolicy `json:"review" yaml:"review"`

	// Reviewer is the agent that reviews this project's pull requests.
	// Empty means the project is not self-reviewed.
	Reviewer ReviewerConfig `json:"reviewer,omitempty" yaml:"reviewer,omitempty"`

	// Discovery selects which discovery paths this project runs:
	// "both" (default), "poll" or "webhook".
	Discovery string `json:"discovery,omitempty" yaml:"discovery,omitempty"`

	// Triage is the agent that triages this project's open issues. Empty
	// falls back to Worker. Triage reads and judges and writes no code, so a
	// project can point it at a cheaper or locally hosted model.
	Triage WorkerConfig `json:"triage,omitempty" yaml:"triage,omitempty"`

	// MaxReviewRounds bounds the review rounds; zero takes the default.
	MaxReviewRounds int `json:"max_review_rounds,omitempty" yaml:"max_review_rounds,omitempty"`

	Ranking RankingPolicy `json:"ranking" yaml:"ranking"`

	// Onboarding declares what provisioning writes to the forge and what
	// conformance checks. Every field defaults, so a binding that says
	// nothing about onboarding still onboards correctly — which is what
	// makes twenty-eight repositories twenty-eight rows rather than
	// twenty-eight decisions.
	Onboarding OnboardingConfig `json:"onboarding,omitempty" yaml:"onboarding,omitempty"`

	CreatedAt time.Time `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
}

// OnboardingConfig declares what provisioning writes to the forge and what
// conformance checks for one bound repository.
type OnboardingConfig struct {
	// WebhookEvents are the forge events the delivery subscribes to. Empty
	// takes the module's defaults.
	WebhookEvents []string `json:"webhook_events,omitempty" yaml:"webhook_events,omitempty"`

	// ProtectedBranch is the branch whose protection conformance reads.
	ProtectedBranch string `json:"protected_branch,omitempty" yaml:"protected_branch,omitempty"`

	// RequiredStatusContexts are the commit-status contexts that branch's
	// protection must require, so a change cannot land past a gate quicbaas
	// is the only writer of.
	RequiredStatusContexts []string `json:"required_status_contexts,omitempty" yaml:"required_status_contexts,omitempty"`

	// ForbiddenPaths are repository paths that must NOT exist — the drift
	// check for the orchestration workflows the cutover deleted.
	ForbiddenPaths []string `json:"forbidden_paths,omitempty" yaml:"forbidden_paths,omitempty"`

	// ForbiddenPathsRef is the ref that check reads; empty reads the
	// default branch.
	ForbiddenPathsRef string `json:"forbidden_paths_ref,omitempty" yaml:"forbidden_paths_ref,omitempty"`

	// LabelColors overrides the fill a label is CREATED with, keyed by the
	// module's label key. An existing label's colour is never changed.
	LabelColors map[string]string `json:"label_colors,omitempty" yaml:"label_colors,omitempty"`
}

// PullRequest is the pull request an attempt opened.
type PullRequest struct {
	Number int    `json:"number"`
	URL    string `json:"url,omitempty"`
	State  string `json:"state"`
	Title  string `json:"title,omitempty"`
	Base   string `json:"base,omitempty"`
}

// ReviewRecord is one reviewer's verdict on one head.
type ReviewRecord struct {
	Reviewer    string    `json:"reviewer"`
	ModelFamily string    `json:"model_family,omitempty"`
	Verdict     string    `json:"verdict"`
	HeadSHA     string    `json:"head_sha"`
	ExternalID  string    `json:"external_id,omitempty"`
	Round       int       `json:"round"`
	Findings    string    `json:"findings,omitempty"`
	SubmittedAt time.Time `json:"submitted_at"`
}

// CheckRecord is the latest state of one check context on one head.
type CheckRecord struct {
	Name       string    `json:"name"`
	HeadSHA    string    `json:"head_sha"`
	Status     string    `json:"status"`
	ForgeRunID string    `json:"forge_run_id,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

// SupersessionRecord is one CI run the delivery's head moved past.
//
// Head pinning already stops the engine ACTING on a displaced result, so a
// superseded run is waste rather than incorrectness. It is still VISIBLE on the
// forge, though, and on a platform that keys a re-run to the workflow rather
// than to the commit — Gitea does — re-running it cancels the run on the real
// head. This record is how a caller tells a live run from an obsolete one
// before touching it.
type SupersessionRecord struct {
	// Name is the check context the run reported under and ForgeRunID the id
	// that addresses it on the forge.
	Name       string `json:"name"`
	ForgeRunID string `json:"forge_run_id,omitempty"`

	// HeadSHA is the head the run was started for, SupersededBy the head that
	// displaced it. The pair is the whole fact: a run on a head is not stale, a
	// run on a head something else replaced is.
	HeadSHA      string `json:"head_sha"`
	SupersededBy string `json:"superseded_by"`

	// Status is what the run was doing when it was displaced.
	Status string `json:"status"`

	// Disposition is "finished" (already complete, holds nothing), "cancelled"
	// (the forge stopped it) or "orphaned" (still in flight and nothing will
	// stop it).
	Disposition string `json:"disposition"`

	// Detail says why the disposition is what it is, for an orphan especially.
	Detail string `json:"detail,omitempty"`

	ObservedAt time.Time `json:"observed_at"`
}

// Delivery records the git side of one attempt at a Task.
type Delivery struct {
	TaskID    string `json:"task_id"`
	ProjectID string `json:"project_id"`
	Attempt   int    `json:"attempt"`
	Branch    string `json:"branch"`
	Base      string `json:"base"`
	Target    string `json:"target"`

	// RequiresPullRequest records the branch plan's review decision.
	RequiresPullRequest bool `json:"requires_pull_request"`

	// DeleteAfterMerge records whether the branch goes once it has landed.
	DeleteAfterMerge bool `json:"delete_after_merge,omitempty"`

	HeadSHA     string         `json:"head_sha,omitempty"`
	WorkDir     string         `json:"work_dir,omitempty"`
	PullRequest PullRequest    `json:"pull_request"`
	Reviews     []ReviewRecord `json:"reviews,omitempty"`
	Checks      []CheckRecord  `json:"checks,omitempty"`

	// SupersededRuns are the CI runs a moved head left behind. Checks holds the
	// latest head only, so this is the only record that a run on an earlier head
	// exists and must not be re-run.
	SupersededRuns []SupersessionRecord `json:"superseded_runs,omitempty"`

	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BindRequest attaches a binding to a project.
type BindRequest struct {
	TenantID  string         `json:"tenant_id,omitempty"`
	ProjectID string         `json:"project_id"`
	Binding   ProjectBinding `json:"binding"`
}

// UnbindRequest detaches a project's binding.
type UnbindRequest struct {
	TenantID  string `json:"tenant_id,omitempty"`
	ProjectID string `json:"project_id"`
}

// UnbindResponse acknowledges an unbind.
type UnbindResponse struct {
	Unbound bool `json:"unbound"`
}

// GetBindingRequest reads a project's binding.
type GetBindingRequest struct {
	TenantID  string `json:"tenant_id,omitempty"`
	ProjectID string `json:"project_id"`
}

// ListForgesRequest reads the build's registered forge adapters. It carries
// no field: the adapters are the same for every caller on a given build.
type ListForgesRequest struct{}

// ListForgesResponse lists the forge platforms this build can talk to.
type ListForgesResponse struct {
	// Forges are the registered platform types, sorted so two reads
	// serialize identically.
	Forges []string `json:"forges"`

	// Count is len(Forges).
	Count int `json:"count"`
}

// GetDeliveryRequest reads a task's delivery.
type GetDeliveryRequest struct {
	TenantID string `json:"tenant_id,omitempty"`
	TaskID   string `json:"task_id"`
}

// OnboardRequest provisions a bound project's repository.
type OnboardRequest struct {
	TenantID  string `json:"tenant_id,omitempty" yaml:"tenant_id,omitempty"`
	ProjectID string `json:"project_id" yaml:"project_id"`
}

// ConformanceRequest reads a project's onboarding conformance.
type ConformanceRequest struct {
	TenantID  string `json:"tenant_id,omitempty" yaml:"tenant_id,omitempty"`
	ProjectID string `json:"project_id" yaml:"project_id"`
}

// OnboardingReport is what a provisioning pass did, plus the conformance the
// repository is in afterwards.
//
// The two together, because "onboarding succeeded" and "the repository is now
// conformant" are different claims: provisioning can succeed while a stale
// workflow it is not allowed to delete remains.
type OnboardingReport struct {
	ProjectID string `json:"project_id" yaml:"project_id"`
	TenantID  string `json:"tenant_id,omitempty" yaml:"tenant_id,omitempty"`
	Repo      string `json:"repo,omitempty" yaml:"repo,omitempty"`

	// WebhookURL is the delivery this node provisioned, empty for a
	// poll-only project.
	WebhookURL string `json:"webhook_url,omitempty" yaml:"webhook_url,omitempty"`

	// WebhookEnsured reports that the delivery was created or brought into
	// line with the spec.
	WebhookEnsured bool `json:"webhook_ensured" yaml:"webhook_ensured"`

	// LabelsCreated names the labels this pass created, empty on a repeat
	// run. It is the idempotency evidence an operator sees.
	LabelsCreated []string `json:"labels_created,omitempty" yaml:"labels_created,omitempty"`

	// Conformance is the state the repository is in after provisioning.
	Conformance *ConformanceReport `json:"conformance,omitempty" yaml:"conformance,omitempty"`
}

// ConformanceFinding is one thing that is wrong, and what to do about it.
//
// EVERY FINDING CARRIES A REMEDY. "Not conformant" is useless, and the
// repairs differ: a missing webhook is an Onboard call, a stale workflow is a
// commit to the repository, a missing protection context is a setting only
// the repository's owner may change.
type ConformanceFinding struct {
	// Class is the failure mode — "webhook_missing", "label_missing",
	// "stale_workflow", "protection_context_missing" and the rest.
	Class string `json:"class" yaml:"class"`

	// Check names the conformance check that produced the finding.
	Check string `json:"check" yaml:"check"`

	// Subject is what the finding is about: the label name, the repository
	// path, the status context, the webhook URL.
	Subject string `json:"subject,omitempty" yaml:"subject,omitempty"`

	// Detail says what was observed.
	Detail string `json:"detail" yaml:"detail"`

	// Remedy says what fixes it.
	Remedy string `json:"remedy" yaml:"remedy"`
}

// ConformanceCheckResult is one check's outcome, so a caller can render
// "webhook ok, labels ok, protection unsupported" rather than only failures.
type ConformanceCheckResult struct {
	Name string `json:"name" yaml:"name"`

	// Status is "pass", "fail", "unsupported", "not_applicable" or "error".
	Status   string `json:"status" yaml:"status"`
	Findings int    `json:"findings" yaml:"findings"`
}

// ConformanceReport answers "is this project correctly onboarded, and if not,
// what is wrong".
type ConformanceReport struct {
	ProjectID string `json:"project_id" yaml:"project_id"`
	TenantID  string `json:"tenant_id,omitempty" yaml:"tenant_id,omitempty"`
	Repo      string `json:"repo,omitempty" yaml:"repo,omitempty"`

	// Conformant is true only when every check that RAN passed. A check that
	// errored makes the report non-conformant; a check the platform does not
	// support, or that the binding does not ask for, does not.
	Conformant bool `json:"conformant" yaml:"conformant"`

	// WebhookURL is where this node expects the repository to deliver.
	WebhookURL string `json:"webhook_url,omitempty" yaml:"webhook_url,omitempty"`

	// Checks is every check and its verdict, in a stable order.
	Checks []ConformanceCheckResult `json:"checks" yaml:"checks"`

	// Findings is everything wrong, in check order.
	Findings []ConformanceFinding `json:"findings,omitempty" yaml:"findings,omitempty"`

	// CheckedAt is when the pass ran.
	CheckedAt time.Time `json:"checked_at" yaml:"checked_at"`
}

// SDLCCapableTransport is declared by a transport that carries the SDLC
// service; every transport package asserts it at compile time.
type SDLCCapableTransport interface {
	SupportsSDLC() bool
}

type client struct {
	transport transport.Transport
}

// NewClient creates an SDLC client over the transport.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{transport: t}
}

// refuseUnwired fails closed on a transport that does not declare the
// service, naming the transport, rather than surfacing a routing miss.
func (c *client) refuseUnwired(op string) error {
	if capable, ok := c.transport.(SDLCCapableTransport); ok && capable.SupportsSDLC() {
		return nil
	}
	return errors.NewSDLCTransportUnsupported(op, fmt.Sprintf("%T", c.transport))
}

// Bind implements Client.
func (c *client) Bind(ctx context.Context, req *BindRequest) (*ProjectBinding, error) {
	const op = "sdlc.Bind"
	if err := c.refuseUnwired(op); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput(op, "context is required")
	}
	if req == nil || req.ProjectID == "" {
		return nil, errors.NewInvalidInput(op, "project_id is required")
	}
	if req.Binding.ProjectID == "" {
		req.Binding.ProjectID = req.ProjectID
	}
	if req.Binding.ProjectID != req.ProjectID {
		return nil, errors.NewInvalidInput(op, "binding.project_id must match project_id")
	}
	resp, err := c.transport.Request(ctx, serviceName, "Bind", req)
	if err != nil {
		return nil, err
	}
	return asBinding(resp, op)
}

// Unbind implements Client.
func (c *client) Unbind(ctx context.Context, tenantID, projectID string) error {
	const op = "sdlc.Unbind"
	if err := c.refuseUnwired(op); err != nil {
		return err
	}
	if ctx == nil {
		return errors.NewInvalidInput(op, "context is required")
	}
	if projectID == "" {
		return errors.NewInvalidInput(op, "project_id is required")
	}
	_, err := c.transport.Request(ctx, serviceName, "Unbind", &UnbindRequest{TenantID: tenantID, ProjectID: projectID})
	return err
}

// Binding implements Client.
func (c *client) Binding(ctx context.Context, tenantID, projectID string) (*ProjectBinding, error) {
	const op = "sdlc.Binding"
	if err := c.refuseUnwired(op); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput(op, "context is required")
	}
	if projectID == "" {
		return nil, errors.NewInvalidInput(op, "project_id is required")
	}
	resp, err := c.transport.Request(ctx, serviceName, "Binding", &GetBindingRequest{TenantID: tenantID, ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	return asBinding(resp, op)
}

// Delivery implements Client.
func (c *client) Delivery(ctx context.Context, tenantID, taskID string) (*Delivery, error) {
	const op = "sdlc.Delivery"
	if err := c.refuseUnwired(op); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput(op, "context is required")
	}
	if taskID == "" {
		return nil, errors.NewInvalidInput(op, "task_id is required")
	}
	resp, err := c.transport.Request(ctx, serviceName, "Delivery", &GetDeliveryRequest{TenantID: tenantID, TaskID: taskID})
	if err != nil {
		return nil, err
	}
	delivery, ok := resp.(*Delivery)
	if !ok || delivery == nil {
		return nil, errors.NewInternal(op, "unexpected response type from server")
	}
	return delivery, nil
}

// ListForges implements Client.
func (c *client) ListForges(ctx context.Context) (*ListForgesResponse, error) {
	const op = "sdlc.ListForges"
	if err := c.refuseUnwired(op); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput(op, "context is required")
	}
	resp, err := c.transport.Request(ctx, serviceName, "ListForges", &ListForgesRequest{})
	if err != nil {
		return nil, err
	}
	forges, ok := resp.(*ListForgesResponse)
	if !ok || forges == nil {
		return nil, errors.NewInternal(op, "unexpected response type from server")
	}
	return forges, nil
}

// Onboard implements Client.
func (c *client) Onboard(ctx context.Context, tenantID, projectID string) (*OnboardingReport, error) {
	const op = "sdlc.Onboard"
	if err := c.refuseUnwired(op); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput(op, "context is required")
	}
	if projectID == "" {
		return nil, errors.NewInvalidInput(op, "project_id is required")
	}
	resp, err := c.transport.Request(ctx, serviceName, "Onboard", &OnboardRequest{TenantID: tenantID, ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	report, ok := resp.(*OnboardingReport)
	if !ok || report == nil {
		return nil, errors.NewInternal(op, "unexpected response type from server")
	}
	return report, nil
}

// Conformance implements Client.
func (c *client) Conformance(ctx context.Context, tenantID, projectID string) (*ConformanceReport, error) {
	const op = "sdlc.Conformance"
	if err := c.refuseUnwired(op); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput(op, "context is required")
	}
	if projectID == "" {
		return nil, errors.NewInvalidInput(op, "project_id is required")
	}
	resp, err := c.transport.Request(ctx, serviceName, "Conformance", &ConformanceRequest{TenantID: tenantID, ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	report, ok := resp.(*ConformanceReport)
	if !ok || report == nil {
		return nil, errors.NewInternal(op, "unexpected response type from server")
	}
	return report, nil
}

func asBinding(resp interface{}, op string) (*ProjectBinding, error) {
	binding, ok := resp.(*ProjectBinding)
	if !ok || binding == nil {
		return nil, errors.NewInternal(op, "unexpected response type from server")
	}
	return binding, nil
}
