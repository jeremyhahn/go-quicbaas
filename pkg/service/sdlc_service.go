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
	"sync/atomic"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	quicbaaserrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
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

	// ProjectWriter persists the project Bind turned autonomous.
	// orchestration.ProjectStore satisfies it.
	ProjectWriter ProjectWriter

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
	onboarder     *sdlc.Onboarder
	transport     sdlc.ForgeTransport
	logger        *slog.Logger
	nudger        atomic.Pointer[nudgerHolder]
}

type nudgerHolder struct {
	nudger sdlc.ProjectNudger
}

var (
	_ sdlc.ForgeResolver         = (*SDLCService)(nil)
	_ sdlc.WebhookSecretResolver = (*SDLCService)(nil)
	_ sdlc.BindingReader         = (*SDLCService)(nil)
	_ sdlc.ProjectNudger         = (*SDLCService)(nil)
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
		secrets: cfg.Secrets, guard: cfg.Guard, transport: cfg.ForgeTransport, logger: logger,
	}
	onboarder, err := sdlc.NewOnboarder(svc, svc, cfg.WebhookPublicURL)
	if err != nil {
		return nil, err
	}
	svc.onboarder = onboarder
	return svc, nil
}

// Store returns the module's record store.
func (s *SDLCService) Store() *sdlc.Store { return s.store }

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

// Bind attaches the forge binding to the project, replacing any earlier
// binding, after checking everything the delivery pipeline will rely on.
func (s *SDLCService) Bind(ctx context.Context, tenantID string, binding *sdlc.ProjectBinding) (*sdlc.ProjectBinding, error) {
	if tenantID == "" {
		return nil, &sdlc.InvalidInputError{Field: "tenant_id", Message: "bindings are tenant-scoped"}
	}
	if err := binding.Validate(); err != nil {
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
	if err := s.enableAutonomy(ctx, project); err != nil {
		return nil, err
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

// warnSystemTenantBind emits a one-line notice when a project is bound in the
// system tenant. The system tenant has full parity with regular tenants,
// including the right to run an SDLC project (a single-tenant deployment that
// uses the system account for everything is a legitimate mode), so this is an
// informed-operator choice rather than an error. The notice records that the
// SDLC worker the binding turns on will run at SYSTEM privilege — the highest
// scope on the platform — so the operator who bound it can see the exposure.
// checkReviewerSecret refuses a binding that names a reviewer whose forge token
// cannot be resolved.
//
// IT IS CHECKED AT BIND TIME BECAUSE THE ALTERNATIVE FAILS AT REVIEW TIME, and
// review time is the worst possible moment to discover it: the pull request is
// already finished and green, a review job starts, spends a model run reading
// the change, and only then cannot publish a verdict — and the attempt is
// charged to the work. The forge token is checked here for exactly this
// reason; a reviewer token is no different, and was simply missed.
//
// AN UNCONFIGURED REVIEWER IS STILL VALID and checks nothing: a project
// reviewed by people binds no reviewer, and ReviewerConfig.Validate already
// refuses the half-configured case.
func (s *SDLCService) checkReviewerSecret(ctx context.Context, tenantID string, binding *sdlc.ProjectBinding) error {
	if !binding.Reviewer.Configured() {
		return nil
	}
	ref := binding.Reviewer.TokenSecretRef
	if ref == "" {
		return &sdlc.InvalidInputError{
			Field:   "reviewer.token_secret_ref",
			Message: "the binding names a reviewer but no reviewer token secret; a reviewer with no token cannot publish a verdict",
		}
	}
	resp, err := s.secrets.GetSecret(iam.ContextWithTenant(ctx, tenantID), &GetSecretRequest{SecretID: ref})
	if err != nil {
		return err
	}
	if resp == nil || len(resp.Data) == 0 {
		return &sdlc.InvalidInputError{
			Field:   "reviewer.token_secret_ref",
			Message: "the reviewer token secret " + ref + " is absent or empty",
		}
	}
	return nil
}

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
	if err := s.onboarder.Offboard(ctx, tenantID, binding); err != nil {
		if !quicbaaserrors.IsNotFound(err) && !sdlc.ForgeRefusalIsPermanent(err) {
			return err
		}
		s.logger.Warn("sdlc: the forge refused this offboarding and repeating it cannot help; the local record is removed and the forge is left as it stands",
			"tenant", tenantID, "project", projectID, "reason", err.Error())
	}
	return nil
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
	if err := s.guard.CheckRepoURL(project.Git.RepoURL); err != nil {
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

// checkSecrets refuses a binding whose secrets do not resolve.
func (s *SDLCService) checkSecrets(ctx context.Context, tenantID string, binding *sdlc.ProjectBinding) error {
	if _, err := s.forgeToken(ctx, tenantID, binding.Forge); err != nil {
		return err
	}
	if err := s.checkReviewerSecret(ctx, tenantID, binding); err != nil {
		return err
	}
	if binding.Forge.WebhookSecretRef == "" {
		return nil
	}
	secret, err := s.WebhookSecret(ctx, tenantID, binding.Forge.WebhookSecretRef)
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
	resp, err := s.secrets.GetSecret(iam.ContextWithTenant(ctx, tenantID), &GetSecretRequest{SecretID: ref})
	if err != nil {
		return "", err
	}
	if resp == nil || len(resp.Data) == 0 {
		return "", &sdlc.InvalidInputError{
			Field:   "forge.token_secret_ref",
			Message: "the forge token secret " + ref + " is absent or empty",
		}
	}
	return string(resp.Data), nil
}

// WebhookSecret implements sdlc.WebhookSecretResolver, returning a copy the
// ingestor may zeroize.
func (s *SDLCService) WebhookSecret(ctx context.Context, tenantID, secretRef string) ([]byte, error) {
	if secretRef == "" {
		return nil, nil
	}
	resp, err := s.secrets.GetSecret(iam.ContextWithTenant(ctx, tenantID), &GetSecretRequest{SecretID: secretRef})
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
