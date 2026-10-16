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

package service

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	quicbaaserrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// SDLCService — the binding service
// =============================================================================
//
// The one write verb the module exposes is Bind: attach a forge repository
// and a review policy to an orchestration Project. Everything the binding
// then relies on is checked HERE, at bind time, so a project that cannot be
// delivered is refused with its reason rather than adopted and stranded:
// the project exists and names a repository on an allowed host, its commits
// are signed, and the secrets the binding names resolve.
//
// BINDING IS THE AUTONOMY SWITCH. A bound project is one the factory works
// on by itself, so Bind turns Project.Autonomous on and persists it; the
// task-source poller and the dispatcher read that flag.
//
// The service is also the module's host ports: it resolves forge adapters
// from tenant secrets, reads webhook secrets, and routes webhook deliveries
// to their tenant.

// SDLCServiceConfig wires the service.
type SDLCServiceConfig struct {
	Store    *sdlc.Store
	Routes   *sdlc.WebhookRouteIndex
	Projects ProjectService
	Secrets  SecretsService
	Guard    sdlc.RepoHostGuard

	// Verification is the operator's command allowlist
	// (orchestration.verification_commands). Bind and Onboard refuse a
	// binding whose verification names a command it does not carry, so the
	// refusal happens where the binding is written rather than after a paid
	// worker turn (X14). Its zero value refuses every command, exactly as the
	// guard does at validation.
	Verification orchestration.ExecutionGuard

	// ProjectWriter persists the project Bind turned autonomous.
	// orchestration.ProjectStore satisfies it.
	ProjectWriter ProjectWriter

	// Tasks is the engine's own task service: the attention inbox pages
	// blocked tasks and drives their resolution through it (readmit,
	// complete, bump the review-round bonus), and it doubles as the
	// agent_offline alert's fleet reader. Required.
	Tasks orchestration.AgentService

	// Accounts is the pooled-provider-account store, read for the attention
	// inbox's account_error alert (a pooled credential whose last refresh
	// failed). Optional: a node with no accounts configured (no
	// secretsManager wired, see lifecycle_orchestration.go) passes nil, and
	// the alert kind is simply never reported rather than the module
	// refusing to start.
	Accounts orchestration.AccountStore

	// Barrier reports the CALLING TENANT's own barrier state, read for the
	// attention inbox's barrier_sealed alert (owner ruling, go-quicbaas#1020:
	// the alert watches each tenant's own barrier, never the system barrier
	// — a sealed system barrier takes the whole API down, so the inbox could
	// not render it anyway). Optional, same as Accounts: nil before the
	// embedded qrdb node's tenant service is reachable simply omits the
	// alert kind.
	Barrier sdlc.AttentionBarrierReader

	// WebhookPublicURL is THIS NODE's externally reachable base URL, the
	// origin a forge delivers to. Required.
	//
	// The service builds the onboarder from it and from ITSELF: the
	// onboarder's forge and secret ports are exactly the ports this service
	// already serves, so wiring an onboarder in from outside would mean
	// constructing the two in a cycle. Passing the one value it cannot
	// derive keeps the construction a straight line.
	WebhookPublicURL string

	// ForgeTransport bounds every forge call the adapters this service builds
	// make: per-call deadlines and the retry of unanswered calls. A zero field
	// takes sdlc.DefaultForgeTransport's.
	ForgeTransport sdlc.ForgeTransport

	// Rulers is the node's configured ruler policy: the forge logins whose
	// comment on a parked issue is an owner ruling, per tenant. The attention
	// inbox's "ruling posted" flag reads it; a malformed policy is refused.
	Rulers sdlc.RulerPolicy

	// Auditor records every issue an operator files. Optional at
	// construction, but FileIssue refuses while none is wired: an operator
	// write to a forge is never made unaudited.
	Auditor sdlc.IssueAuditor

	// Agents is the tenant agent catalog a binding's worker, triage and
	// review seats are checked against when it is bound. Required.
	Agents sdlc.AgentCatalog

	// SSHKeys reads the tenant's SSH keys, so OnboardRepo can check a new
	// repository's signing key exists before it creates a project that
	// signs with it. Optional: without it OnboardRepo reports the key as
	// unverifiable rather than guessing.
	SSHKeys SSHKeyGetter

	// Logger records the bind-time notice that a system-tenant binding runs
	// the SDLC worker at SYSTEM privilege. Optional; a nil logger falls back
	// to slog.Default so the notice is never silently dropped.
	Logger *slog.Logger
}

// ProjectWriter is the one project write the binding makes: the project it
// attaches to is stored back with autonomous development turned on.
type ProjectWriter interface {
	Update(ctx context.Context, project *orchestration.Project) error
}

// SDLCService binds projects and serves the module's ports.
type SDLCService struct {
	store         *sdlc.Store
	routes        *sdlc.WebhookRouteIndex
	projects      ProjectService
	projectWriter ProjectWriter
	secrets       SecretsService
	guard         sdlc.RepoHostGuard
	verification  orchestration.ExecutionGuard
	onboarder     *sdlc.Onboarder
	transport     sdlc.ForgeTransport
	logger        *slog.Logger
	agents        sdlc.AgentCatalog
	sshKeys       SSHKeyGetter
	nudger        atomic.Pointer[nudgerHolder]
	reviews       atomic.Pointer[sdlc.ReviewDispatcher]
	workerImage   atomic.Pointer[workerImageHolder]
	preflight     atomic.Pointer[preflightJobsHolder]
	attention     *sdlc.AttentionService
	auditor       sdlc.IssueAuditor
	adopter       *sdlc.IssueAdopter
}

type nudgerHolder struct {
	nudger sdlc.ProjectNudger
}

type preflightJobsHolder struct {
	jobs sdlc.PreflightJobs
}

var (
	_ sdlc.ForgeResolver          = (*SDLCService)(nil)
	_ sdlc.WebhookSecretResolver  = (*SDLCService)(nil)
	_ sdlc.BindingReader          = (*SDLCService)(nil)
	_ sdlc.ProjectNudger          = (*SDLCService)(nil)
	_ sdlc.ReviewerOverrideReader = (*SDLCService)(nil)
	_ sdlc.ProjectReader          = (*SDLCService)(nil)

	_ orchestration.AgentUsageReporter = (*SDLCService)(nil)
)

// NewSDLCService wires the service; every collaborator is required.
func NewSDLCService(cfg SDLCServiceConfig) (*SDLCService, error) {
	switch {
	case cfg.Store == nil:
		return nil, &sdlc.InvalidInputError{Field: "store", Message: "the service reads and writes bindings through the store"}
	case cfg.Routes == nil:
		return nil, &sdlc.InvalidInputError{Field: "routes", Message: "a bound project is routable by its webhook or it is not bound"}
	case cfg.Projects == nil:
		return nil, &sdlc.InvalidInputError{Field: "projects", Message: "a binding references a project that must exist"}
	case cfg.Secrets == nil:
		return nil, &sdlc.InvalidInputError{Field: "secrets", Message: "forge tokens and webhook secrets live in the tenant's secrets"}
	case cfg.ProjectWriter == nil:
		return nil, &sdlc.InvalidInputError{Field: "project_writer", Message: "binding turns the project autonomous, which has to be persisted"}
	case cfg.Tasks == nil:
		return nil, &sdlc.InvalidInputError{Field: "tasks", Message: "the attention inbox reads and resolves blocked tasks through the engine's task service"}
	case cfg.Agents == nil:
		return nil, &sdlc.InvalidInputError{Field: "agents", Message: "a binding's agents are checked against the tenant agent catalog"}
	}
	// Refused here rather than at the first forge call, which would fail
	// every project's first sweep on a knob the operator set once.
	if err := cfg.ForgeTransport.Validate(); err != nil {
		return nil, err
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	svc := &SDLCService{
		store: cfg.Store, routes: cfg.Routes, projects: cfg.Projects, projectWriter: cfg.ProjectWriter,
		secrets: cfg.Secrets, guard: cfg.Guard, verification: cfg.Verification, transport: cfg.ForgeTransport, logger: logger,
		auditor: cfg.Auditor, agents: cfg.Agents, sshKeys: cfg.SSHKeys,
	}
	onboarder, err := sdlc.NewOnboarder(svc, svc, cfg.WebhookPublicURL)
	if err != nil {
		return nil, err
	}
	// Accounts and Barrier are wired straight through from cfg — nil is a
	// valid, honest "not configured on this node" rather than a refusal to
	// start.
	attention, err := sdlc.NewAttentionService(sdlc.AttentionServiceConfig{
		Tasks: cfg.Tasks, Bindings: svc, Forges: svc, Projects: svc, Fleet: cfg.Tasks,
		Accounts: cfg.Accounts, Barrier: cfg.Barrier, Rulers: cfg.Rulers,
	})
	if err != nil {
		return nil, err
	}
	adopter, err := sdlc.NewIssueAdopter(cfg.Tasks, svc, svc)
	if err != nil {
		return nil, err
	}
	svc.attention = attention
	svc.adopter = adopter
	svc.onboarder = onboarder
	return svc, nil
}

// Store returns the module's record store.
func (s *SDLCService) Store() *sdlc.Store { return s.store }

// Attention returns the attention inbox service.
func (s *SDLCService) Attention() *sdlc.AttentionService { return s.attention }

// AdoptIssue hands one issue back to the factory on an operator's
// instruction: the task is created for it — on the named pull request's own
// branch when one is named — the issue's holds come off and the ruling is
// posted. See sdlc.IssueAdopter.
//
// THE TENANT IS STAMPED ON THE CONTEXT when the caller's carries none, so the
// engine's tenant guard admits the task in the tenant the request was
// admitted to, and never in some other one: a context that already names a
// tenant is left exactly as it is.
func (s *SDLCService) AdoptIssue(ctx context.Context, tenantID string, req sdlc.AdoptIssueRequest) (*sdlc.Adoption, error) {
	return s.adopter.Adopt(projectStoreContext(ctx, tenantID), tenantID, req)
}

// Get implements sdlc.ProjectReader: the orchestration Project a binding or
// a task's ProjectID names. It is the same read Bind and Conformance already
// make (see project below), exposed so the attention inbox can resolve a
// parked task's project display name through the one port every other SDLC
// verb already trusts.
func (s *SDLCService) Get(ctx context.Context, tenantID, projectID string) (*orchestration.Project, error) {
	return s.project(ctx, tenantID, projectID)
}

// Routes returns the webhook route index.
func (s *SDLCService) Routes() *sdlc.WebhookRouteIndex { return s.routes }

// SetProjectNudger attaches the engine's poller nudge. Until one is set,
// a verified webhook delivery is refused rather than dropped.
func (s *SDLCService) SetProjectNudger(nudger sdlc.ProjectNudger) {
	if nudger == nil {
		s.nudger.Store(nil)
		return
	}
	s.nudger.Store(&nudgerHolder{nudger: nudger})
}

// SetPreflightJobs attaches the job engine an onboarding's preflight runs on.
// Until one is set, a requested preflight is a finding and autonomy stays off.
func (s *SDLCService) SetPreflightJobs(jobs sdlc.PreflightJobs) {
	if jobs == nil {
		s.preflight.Store(nil)
		return
	}
	s.preflight.Store(&preflightJobsHolder{jobs: jobs})
}

// NudgeProject implements sdlc.ProjectNudger by delegating to the attached
// poller nudge.
func (s *SDLCService) NudgeProject(ctx context.Context, tenantID, projectID string) error {
	held := s.nudger.Load()
	if held == nil {
		return sdlc.ErrUnavailable
	}
	return held.nudger.NudgeProject(ctx, tenantID, projectID)
}

// =============================================================================
// Bind / Unbind / Binding / Delivery
// =============================================================================

// BindOptions carries the intent of one Bind request. It is not stored on
// the binding: it describes the request, not the binding.
type BindOptions struct {
	// LeavePaused binds without switching the project's autonomy on, so the
	// project can be staged before the poller adopts its issues. It never
	// turns autonomy off: an already-autonomous project stays autonomous,
	// the same rule Unbind follows.
	LeavePaused bool
}

// Bind attaches the forge binding to the project, replacing any earlier
// binding, after checking everything the delivery pipeline will rely on.
// It switches the project's autonomy on unless opts.LeavePaused is set.
func (s *SDLCService) Bind(ctx context.Context, tenantID string, binding *sdlc.ProjectBinding, opts BindOptions) (*sdlc.ProjectBinding, error) {
	if tenantID == "" {
		return nil, &sdlc.InvalidInputError{Field: "tenant_id", Message: "bindings are tenant-scoped"}
	}
	if binding != nil {
		binding.Forge.ApplyDefaults()
	}
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	// EVERY AGENT REFERENCE IS CHECKED WHERE THE OPERATOR IS LOOKING. A
	// worker naming no Agent, a triage pool with a coder in it, or a review
	// seat whose Agent cannot count would otherwise surface as a failed
	// spawn minutes later, charged to the work.
	catalogue, err := s.agents.Catalogue(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if err := binding.CheckAgents(catalogue); err != nil {
		return nil, err
	}
	if err := binding.CheckVerification(s.verification); err != nil {
		return nil, err
	}
	if !slices.Contains(sdlc.RegisteredForgeTypes(), binding.Forge.Type) {
		return nil, sdlc.NewForgeNotRegisteredError(binding.Forge.Type)
	}
	project, err := s.project(ctx, tenantID, binding.ProjectID)
	if err != nil {
		return nil, err
	}
	if err := s.checkProject(project); err != nil {
		return nil, err
	}
	if err := s.checkSecrets(ctx, tenantID, binding); err != nil {
		return nil, err
	}
	if err := s.checkRulers(ctx, tenantID, binding); err != nil {
		return nil, err
	}
	if err := s.retirePriorDelivery(ctx, tenantID, binding); err != nil {
		return nil, err
	}
	if !opts.LeavePaused {
		if err := s.enableAutonomy(ctx, project); err != nil {
			return nil, err
		}
	}
	if err := s.routes.Record(ctx, binding.ProjectID, tenantID); err != nil {
		return nil, err
	}
	if err := s.store.PutBinding(ctx, tenantID, binding); err != nil {
		return nil, err
	}
	s.warnSystemTenantBind(tenantID, binding.ProjectID)
	// PROVISIONED LAST, AND THE ORDER IS DELIBERATE. The route index and the
	// binding are what make an inbound delivery routable, so they exist
	// before the forge is told to deliver; a webhook created first would
	// aim at an ingress that answers 401 until the write behind it lands.
	//
	// A provisioning failure therefore leaves a project BOUND but not
	// driven, and that is the recoverable half of the two: Conformance says
	// exactly what is missing and Onboard puts it right, where the other
	// order would leave a repository delivering to a node that has no record
	// of it.
	if _, err := s.onboarder.Onboard(ctx, tenantID, binding); err != nil {
		return nil, err
	}
	return binding, nil
}

// checkReviewerSecrets refuses a binding that names a reviewer profile whose
// forge token cannot be resolved — the default AND every alternate.
//
// IT IS CHECKED AT BIND TIME BECAUSE THE ALTERNATIVE FAILS AT REVIEW TIME, and
// review time is the worst possible moment to discover it: the pull request is
// already finished and green, a review job starts, spends a model run reading
// the change, and only then cannot publish a verdict — and the attempt is
// charged to the work. The forge token is checked here for exactly this
// reason; a reviewer token is no different, and was once simply missed. An
// alternate's token is no different either: an override is an operator asking
// for that reviewer on a pull request that is already waiting.
//
// AN UNCONFIGURED REVIEWER IS STILL VALID and checks nothing: a project
// reviewed by people binds no reviewer, and ReviewerConfig.Validate already
// refuses the half-configured case.
func (s *SDLCService) checkReviewerSecrets(ctx context.Context, tenantID string, binding *sdlc.ProjectBinding) error {
	for i, profile := range binding.ReviewerProfiles() {
		field := "reviewer.token_secret_ref"
		if !profile.Default {
			field = "reviewers[" + strconv.Itoa(i-1) + "].token_secret_ref"
		}
		if err := s.checkReviewerSecret(ctx, tenantID, field, profile); err != nil {
			return err
		}
	}
	return nil
}

// checkReviewerSecret refuses one profile whose token does not resolve.
func (s *SDLCService) checkReviewerSecret(ctx context.Context, tenantID, field string, profile sdlc.ReviewerProfile) error {
	ref := profile.TokenSecretRef
	if ref == "" {
		return &sdlc.InvalidInputError{
			Field:   field,
			Message: "reviewer profile " + profile.Name + " names no token secret; a reviewer with no token cannot publish a verdict",
		}
	}
	token, err := requireSecretRef(ctx, s.secrets, tenantID, field, ref)
	if err != nil {
		return err
	}
	if len(token) == 0 {
		return &sdlc.InvalidInputError{
			Field:   field,
			Message: "the token secret " + ref + " of reviewer profile " + profile.Name + " is empty",
		}
	}
	return nil
}

// warnSystemTenantBind emits a one-line notice when a project is bound in the
// system tenant. The system tenant has full parity with regular tenants,
// including the right to run an SDLC project (a single-tenant deployment that
// uses the system account for everything is a legitimate mode), so this is an
// informed-operator choice rather than an error. The notice records that the
// SDLC worker the binding turns on will run at SYSTEM privilege — the highest
// scope on the platform — so the operator who bound it can see the exposure.
func (s *SDLCService) warnSystemTenantBind(tenantID, projectID string) {
	if !qrdbsdk.IsSystemTenant(tenantID) {
		return
	}
	s.logger.Warn("sdlc: binding a project in the system tenant; the SDLC worker will run at SYSTEM privilege (the highest) — informed-operator choice, not an error",
		"tenant", qrdbsdk.SystemTenantID, "project", projectID)
}

// enableAutonomy turns autonomous development on for the bound project and
// persists it. Already-autonomous is not written again.
func (s *SDLCService) enableAutonomy(ctx context.Context, project *orchestration.Project) error {
	if project.Autonomous {
		return nil
	}
	project.Autonomous = true
	return s.projectWriter.Update(ctx, project)
}

// Unbind detaches the project's binding. Unbinding an unbound project is
// not an error.
//
// AUTONOMY IS NOT TURNED BACK OFF. Bind turns it on because binding is the
// operator asking for autonomous development; turning it off is a separate,
// deliberate act on the project, so unbinding a forge never silently stops
// work the operator did not ask to stop.
func (s *SDLCService) Unbind(ctx context.Context, tenantID, projectID string) error {
	if tenantID == "" {
		return &sdlc.InvalidInputError{Field: "tenant_id", Message: "bindings are tenant-scoped"}
	}
	if err := s.offboard(ctx, tenantID, projectID); err != nil {
		return err
	}
	if err := s.store.DeleteBinding(ctx, tenantID, projectID); err != nil {
		return err
	}
	// An override names a profile of THIS binding. A later binding may carry
	// different profiles, and must not inherit choices made about these.
	if err := s.store.DeleteReviewerOverrides(ctx, tenantID, projectID); err != nil {
		return err
	}
	return s.routes.Forget(ctx, projectID, tenantID)
}

// offboard removes the forge webhook the binding provisioned, BEFORE the
// binding itself goes.
//
// A FAILURE REFUSES THE UNBIND, which is the same fail-closed rule Bind
// already follows when a secret does not resolve. Deleting the binding first
// would leave a live delivery on the forge that nothing can any longer
// identify the owner of: the route index has forgotten the project, so every
// delivery answers 401 for ever and no later unbind can find the hook to
// remove. Refusing leaves a state the operator can retry.
//
// Unbinding a project that was never bound is not an error, so an unbind can
// be repeated.
// offboard removes what Bind provisioned on the forge.
//
// A FORGE CLEANUP THAT CAN NEVER SUCCEED MUST NOT STRAND THE LOCAL RECORD.
// Offboard needs the binding's forge token to delete the webhook, so a binding
// whose token secret has been deleted cannot be offboarded — and because
// Unbind refused on that error, the binding could never be deleted either. The
// local record is what drives work on this node: the triage backstop starts a
// job for every bound project, so a binding left behind by a deleted project
// failed a job on every pass, for ever, with no operator verb able to stop it.
// Measured on the burn-in node (2026-09-14): `DELETE .../binding` answered 500
// "secret has been deleted", retryable=false, on every attempt.
//
// ONLY THE PERMANENT CASE IS WAVED THROUGH, and the distinction is what makes
// this safe rather than sloppy — see sdlc.ForgeRefusalIsPermanent for where the
// line is drawn and why. A transient failure still refuses, because retrying
// WILL work and silently dropping the local record would leave a live webhook
// delivering to a node that no longer routes it. A forge that ANSWERED — the
// secret is gone, the account is not an admin — is the opposite: no retry can
// change it, so refusing buys nothing and costs the operator their only way
// out. What is left on the forge is inert and `conformance` names it.
func (s *SDLCService) offboard(ctx context.Context, tenantID, projectID string) error {
	binding, err := s.store.GetBinding(ctx, tenantID, projectID)
	if err != nil {
		if errors.Is(err, sdlc.ErrNotFound) {
			return nil
		}
		return err
	}
	return s.offboardBinding(ctx, tenantID, binding)
}

// offboardBinding removes the delivery a stored binding provisioned, under the
// rule offboard documents: a transient failure refuses, a permanent forge
// refusal is logged and waved through.
func (s *SDLCService) offboardBinding(ctx context.Context, tenantID string, binding *sdlc.ProjectBinding) error {
	if err := s.onboarder.Offboard(ctx, tenantID, binding); err != nil {
		var missingRef *quicbaaserrors.ReferenceNotFoundError
		if !quicbaaserrors.IsNotFound(err) && !errors.As(err, &missingRef) && !sdlc.ForgeRefusalIsPermanent(err) {
			return err
		}
		s.logger.Warn("sdlc: the forge refused this offboarding and repeating it cannot help; the local record is removed and the forge is left as it stands",
			"tenant", tenantID, "project", binding.ProjectID, "reason", err.Error())
	}
	return nil
}

// retirePriorDelivery offboards the binding a rebind is about to replace, when
// the replacement would leave its webhook behind.
//
// A REBIND TO DIFFERENT FORGE COORDINATES ORPHANS THE OLD WEBHOOK otherwise.
// Once PutBinding returns, the old repository and forge are recorded nowhere:
// Conformance and Unbind resolve only the new binding, the route index maps
// the project to it, and the old repository keeps delivering to this node and
// is answered 401 for ever. That is the state offboard exists to prevent on
// the Unbind path, and a rebind reaches it by another road. Switching a
// webhook-backed binding to poll-only has the same shape: onboarding a binding
// with no webhook secret provisions nothing, so it removes nothing either.
//
// IT RUNS BEFORE ANYTHING IS WRITTEN, and a failure refuses the rebind. The
// old binding is then still stored and still names the hook, so the operator
// can retry; the other order would leave a half-applied rebind and a hook
// nothing can find.
//
// A rebind that keeps the same repository on the same forge offboards
// nothing: onboarding the replacement reconciles the existing webhook in
// place.
func (s *SDLCService) retirePriorDelivery(ctx context.Context, tenantID string, next *sdlc.ProjectBinding) error {
	prior, err := s.store.GetBinding(ctx, tenantID, next.ProjectID)
	if err != nil {
		if errors.Is(err, sdlc.ErrNotFound) {
			return nil
		}
		return err
	}
	if !orphansDelivery(prior, next) {
		return nil
	}
	return s.offboardBinding(ctx, tenantID, prior)
}

// orphansDelivery reports whether replacing prior with next would leave
// prior's webhook on a forge that nothing records any longer: the repository,
// the forge or its API root changed, or the delivery is being switched off.
func orphansDelivery(prior, next *sdlc.ProjectBinding) bool {
	if prior.Forge.WebhookSecretRef == "" {
		return false
	}
	switch {
	case next.Forge.WebhookSecretRef == "":
		return true
	case prior.Forge.Type != next.Forge.Type:
		return true
	case strings.TrimRight(prior.Forge.BaseURL, "/") != strings.TrimRight(next.Forge.BaseURL, "/"):
		return true
	default:
		return prior.Repo() != next.Repo()
	}
}

// Onboard re-runs provisioning against the project's stored binding and
// returns what it did together with the conformance that follows.
//
// THE EXPLICIT REPAIR VERB. Conformance reports and never repairs, so an
// operator who reads a report and wants it fixed calls this — a separate act
// on a separate route, rather than a read that quietly rewrote a repository's
// configuration.
func (s *SDLCService) Onboard(ctx context.Context, tenantID, projectID string) (*sdlc.OnboardingReport, error) {
	if tenantID == "" {
		return nil, &sdlc.InvalidInputError{Field: "tenant_id", Message: "onboarding is tenant-scoped"}
	}
	binding, err := s.store.GetBinding(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	// A binding written before the allowlist narrowed is refused here too:
	// onboarding it would drive work every attempt of which parks.
	if err := binding.CheckVerification(s.verification); err != nil {
		return nil, err
	}
	return s.onboarder.Onboard(ctx, tenantID, binding)
}

// Conformance reports whether the project is correctly onboarded. It changes
// nothing.
//
// AN UNBOUND PROJECT IS A REPORT, NOT AN ERROR: "this project has no binding"
// is precisely the answer a fleet dashboard asking about twenty-eight
// projects needs rendered as a row. A project that does not exist at all is
// still a NotFoundError, because that is a question about the wrong subject
// rather than an answer about this one.
func (s *SDLCService) Conformance(ctx context.Context, tenantID, projectID string) (*sdlc.ConformanceReport, error) {
	if tenantID == "" {
		return nil, &sdlc.InvalidInputError{Field: "tenant_id", Message: "conformance is tenant-scoped"}
	}
	if _, err := s.project(ctx, tenantID, projectID); err != nil {
		return nil, err
	}
	binding, err := s.store.GetBinding(ctx, tenantID, projectID)
	if err != nil && !errors.Is(err, sdlc.ErrNotFound) {
		return nil, err
	}
	report := s.onboarder.Check(ctx, tenantID, binding)
	report.ProjectID = projectID
	return report, nil
}

// OpenWork reads a bound project's open work from its forge: the open pull
// requests the project's agent opened, each with its CI and review state, and
// the open issues carrying a label the module writes. It changes nothing.
//
// Unlike Conformance, an UNBOUND project is an error: it has no forge to read.
func (s *SDLCService) OpenWork(ctx context.Context, tenantID, projectID string) (*sdlc.OpenWork, error) {
	if tenantID == "" {
		return nil, &sdlc.InvalidInputError{Field: "tenant_id", Message: "open work is tenant-scoped"}
	}
	if _, err := s.project(ctx, tenantID, projectID); err != nil {
		return nil, err
	}
	binding, err := s.store.GetBinding(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	forge, err := s.ForgeFor(ctx, tenantID, binding.Forge)
	if err != nil {
		return nil, err
	}
	return sdlc.ReadOpenWork(ctx, forge, binding, time.Now())
}

// Binding returns the project's binding.
func (s *SDLCService) Binding(ctx context.Context, tenantID, projectID string) (*sdlc.ProjectBinding, error) {
	return s.store.GetBinding(ctx, tenantID, projectID)
}

// GetBinding implements sdlc.BindingReader.
func (s *SDLCService) GetBinding(ctx context.Context, tenantID, projectID string) (*sdlc.ProjectBinding, error) {
	return s.store.GetBinding(ctx, tenantID, projectID)
}

// ListBindings implements sdlc.BindingReader.
func (s *SDLCService) ListBindings(ctx context.Context, tenantID string) ([]*sdlc.ProjectBinding, error) {
	return s.store.ListBindings(ctx, tenantID)
}

// =============================================================================
// The per-pull-request reviewer override
// =============================================================================
//
// OPERATOR VERBS, GATED LIKE THE BINDING THEY REFINE. Choosing which reviewer
// judges a change is as much a decision about the project's review as binding
// the reviewer was, so setting and clearing an override carry sdlc:bind on
// every transport and reading one carries sdlc:read. Nothing else writes an
// override: not a label, not a comment, not a webhook.

// ReviewerSelection reports which reviewer profile reviews one pull request:
// the operator's override when one is recorded, the binding's default
// otherwise.
func (s *SDLCService) ReviewerSelection(ctx context.Context, tenantID, projectID string, pullRequest int) (*sdlc.ReviewerSelection, error) {
	binding, err := s.selfReviewedBinding(ctx, tenantID, projectID, pullRequest)
	if err != nil {
		return nil, err
	}
	_, selection, err := sdlc.SelectReviewer(ctx, s.store, tenantID, binding, pullRequest)
	if err != nil {
		return nil, err
	}
	return &selection, nil
}

// SetReviewerOverride records that one pull request is reviewed by the named
// profile, and returns the selection that now holds.
//
// THE PROFILE MUST BE ONE THE BINDING CARRIES NOW. A name nobody bound is
// refused here, where the operator is looking, rather than at dispatch, where
// it would surface as a sweep failure minutes later.
func (s *SDLCService) SetReviewerOverride(ctx context.Context, tenantID, projectID string, pullRequest int, profile string) (*sdlc.ReviewerSelection, error) {
	binding, err := s.selfReviewedBinding(ctx, tenantID, projectID, pullRequest)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(profile) == "" {
		return nil, &sdlc.InvalidInputError{Field: "profile", Message: "required; clear the override to review with the default"}
	}
	resolved, err := binding.ReviewerProfile(profile)
	if err != nil {
		return nil, err
	}
	override := &sdlc.ReviewerOverride{
		ProjectID: projectID, PullRequest: pullRequest, Profile: resolved.Name, BindingEpoch: binding.Epoch,
	}
	if err := s.store.PutReviewerOverride(ctx, tenantID, override); err != nil {
		return nil, err
	}
	return s.redispatchReview(ctx, tenantID, projectID, pullRequest)
}

// SetReviewDispatcher attaches the dispatcher a reviewer override asks for a
// review by the newly selected seat through. Until one is set, an override is
// recorded and takes effect on the next dispatch.
func (s *SDLCService) SetReviewDispatcher(dispatcher *sdlc.ReviewDispatcher) {
	s.reviews.Store(dispatcher)
}

// redispatchReview asks for a review of the pull request by the now-selected
// seat, then reports the selection with whether one started.
//
// OWNER RULING 2026-09-25: switching reviewer never cancels anything. A review
// already running finishes normally; the switch decides who reviews from here
// on. The selection is recorded before this runs, so a failure here leaves the
// override in force for the next dispatch rather than lost.
func (s *SDLCService) redispatchReview(ctx context.Context, tenantID, projectID string, pullRequest int) (*sdlc.ReviewerSelection, error) {
	selection, err := s.ReviewerSelection(ctx, tenantID, projectID, pullRequest)
	if err != nil {
		return nil, err
	}
	dispatcher := s.reviews.Load()
	if dispatcher == nil {
		return selection, nil
	}
	started, err := dispatcher.RedispatchReview(ctx, tenantID, projectID, pullRequest)
	if err != nil {
		return nil, err
	}
	selection.Redispatched = started
	return selection, nil
}

// AgentReferences implements orchestration.AgentUsageReporter: every
// reference tenantID's bindings hold into its agent catalog.
func (s *SDLCService) AgentReferences(ctx context.Context, tenantID string) ([]orchestration.AgentReference, error) {
	bindings, err := s.store.ListBindings(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	var references []orchestration.AgentReference
	for _, binding := range bindings {
		references = append(references, binding.AgentReferences()...)
	}
	return references, nil
}

// ClearReviewerOverride removes one pull request's override, so the binding's
// default reviews it again, and returns the selection that now holds.
// Clearing an absent override is not an error.
func (s *SDLCService) ClearReviewerOverride(ctx context.Context, tenantID, projectID string, pullRequest int) (*sdlc.ReviewerSelection, error) {
	if _, err := s.selfReviewedBinding(ctx, tenantID, projectID, pullRequest); err != nil {
		return nil, err
	}
	if err := s.store.DeleteReviewerOverride(ctx, tenantID, projectID, pullRequest); err != nil {
		return nil, err
	}
	return s.ReviewerSelection(ctx, tenantID, projectID, pullRequest)
}

// GetReviewerOverride implements sdlc.ReviewerOverrideReader.
func (s *SDLCService) GetReviewerOverride(ctx context.Context, tenantID, projectID string, pullRequest int) (*sdlc.ReviewerOverride, error) {
	return s.store.GetReviewerOverride(ctx, tenantID, projectID, pullRequest)
}

// selfReviewedBinding loads the binding an override verb acts on, refusing a
// request that names no tenant or pull request and a project that reviews
// nothing of its own.
func (s *SDLCService) selfReviewedBinding(ctx context.Context, tenantID, projectID string, pullRequest int) (*sdlc.ProjectBinding, error) {
	if tenantID == "" {
		return nil, &sdlc.InvalidInputError{Field: "tenant_id", Message: "reviewer overrides are tenant-scoped"}
	}
	if pullRequest <= 0 {
		return nil, &sdlc.InvalidInputError{Field: "pull_request", Message: "pull requests are numbered from one"}
	}
	binding, err := s.store.GetBinding(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	if !binding.Reviewer.Configured() {
		return nil, &sdlc.InvalidInputError{
			Field:   "reviewer",
			Message: "project " + projectID + " binds no reviewer, so there is no reviewer to choose between",
		}
	}
	return binding, nil
}

// Delivery returns what the task's attempt published.
func (s *SDLCService) Delivery(ctx context.Context, tenantID, taskID string) (*sdlc.Delivery, error) {
	return s.store.GetDelivery(ctx, tenantID, taskID)
}

// project loads the orchestration project a binding references.
func (s *SDLCService) project(ctx context.Context, tenantID, projectID string) (*orchestration.Project, error) {
	resp, err := s.projects.GetProject(ctx, &GetProjectRequest{TenantID: tenantID, ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Project == nil {
		return nil, &sdlc.NotFoundError{Entity: "project", TenantID: tenantID, ID: projectID}
	}
	return resp.Project, nil
}

// checkProject refuses a project the pipeline could not deliver.
func (s *SDLCService) checkProject(project *orchestration.Project) error {
	if err := s.guard.CheckProject(project); err != nil {
		return err
	}
	if !project.Git.Signing.Enabled {
		return &sdlc.InvalidInputError{
			Field:   "project.git.signing",
			Message: "the project's commits are unsigned; the Software Factory signs every commit it pushes and refuses a project whose policy would not",
		}
	}
	if err := project.Git.Signing.Validate(); err != nil {
		return err
	}
	return nil
}

// checkSecrets refuses a binding whose secrets do not resolve. A secret that
// does not exist is a ReferenceNotFoundError; a secrets store that cannot
// answer (a sealed barrier) is a ReferenceUnavailableError, never a false
// "not found".
func (s *SDLCService) checkSecrets(ctx context.Context, tenantID string, binding *sdlc.ProjectBinding) error {
	if _, err := s.forgeToken(ctx, tenantID, binding.Forge); err != nil {
		return err
	}
	if err := s.checkReviewerSecrets(ctx, tenantID, binding); err != nil {
		return err
	}
	if binding.Forge.WebhookSecretRef == "" {
		return nil
	}
	secret, err := requireSecretRef(ctx, s.secrets, tenantID, "forge.webhook_secret_ref", binding.Forge.WebhookSecretRef)
	if err != nil {
		return err
	}
	if len(secret) == 0 {
		return &sdlc.WebhookSecretMissingError{
			ProjectID: binding.ProjectID,
			Reason:    "the secret named by " + binding.Forge.WebhookSecretRef + " resolved empty",
		}
	}
	return nil
}

// =============================================================================
// Ports
// =============================================================================

// ForgeFor implements sdlc.ForgeResolver: the adapter for a binding's
// forge, authenticated with the token its TokenSecretRef names.
func (s *SDLCService) ForgeFor(ctx context.Context, tenantID string, cfg sdlc.ForgeConfig) (sdlc.ForgeBackend, error) {
	token, err := s.forgeToken(ctx, tenantID, cfg)
	if err != nil {
		return nil, err
	}
	return sdlc.NewForgeBackend(cfg, sdlc.ForgeCredentials{Token: token}, s.transport)
}

// ForgeToken implements sdlc.ForgeTokenReader: the forge token value the
// binding names, for the worker container's own clone and push.
//
// The value leaves this process only in the spawn's named workspace spec,
// written by the host that executes the session into the per-spawn directory
// its container mounts read-only; see orchestration.NewWorkspaceSpecFiles and
// WorkspaceManager.ProvisionSession.
func (s *SDLCService) ForgeToken(ctx context.Context, tenantID string, cfg sdlc.ForgeConfig) ([]byte, error) {
	token, err := s.forgeToken(ctx, tenantID, cfg)
	if err != nil {
		return nil, err
	}
	return []byte(token), nil
}

// forgeToken reads the forge token a binding names.
func (s *SDLCService) forgeToken(ctx context.Context, tenantID string, cfg sdlc.ForgeConfig) (string, error) {
	ref := cfg.TokenSecretRef
	if ref == "" {
		return "", &sdlc.InvalidInputError{
			Field:   "forge.token_secret_ref",
			Message: "the binding names no forge token secret; an adapter with no token cannot read the forge",
		}
	}
	token, err := requireSecretRef(ctx, s.secrets, tenantID, "forge.token_secret_ref", ref)
	if err != nil {
		return "", err
	}
	if len(token) == 0 {
		return "", &sdlc.InvalidInputError{
			Field:   "forge.token_secret_ref",
			Message: "the forge token secret " + ref + " is empty",
		}
	}
	return string(token), nil
}

// WebhookSecret implements sdlc.WebhookSecretResolver, returning a copy the
// ingestor may zeroize.
func (s *SDLCService) WebhookSecret(ctx context.Context, tenantID, secretRef string) ([]byte, error) {
	if secretRef == "" {
		return nil, nil
	}
	resp, err := s.secrets.GetSecret(tenantctx.WithTenant(ctx, tenantID), &GetSecretRequest{SecretID: secretRef})
	if err != nil {
		return nil, err
	}
	if resp == nil || len(resp.Data) == 0 {
		return nil, nil
	}
	out := make([]byte, len(resp.Data))
	copy(out, resp.Data)
	return out, nil
}
