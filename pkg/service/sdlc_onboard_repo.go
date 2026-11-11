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

package service

import (
	"context"
	"errors"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	quicbaaserrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

const (
	// onboardSigningKeyScope is the key store scope a repository's agent
	// signing key lives in: the key belongs to the agent's service identity,
	// not to a person.
	onboardSigningKeyScope = "service"

	// onboardWebhookSecretLength is the generated webhook HMAC key's length
	// in bytes.
	onboardWebhookSecretLength = 32

	// onboardRerunRemedy is the remedy of a step blocked by an earlier one.
	onboardRerunRemedy = "resolve the findings above, then run the onboarding again"
)

// repoOnboarding is one OnboardRepo run: the request, the template it copies
// and the report it fills in step by step.
type repoOnboarding struct {
	tenantID string
	req      sdlc.RepoOnboardRequest
	template *orchestration.Project
	like     *sdlc.ProjectBinding
	project  *orchestration.Project
	binding  *sdlc.ProjectBinding
	report   *sdlc.RepoOnboardingReport
}

func (o *repoOnboarding) record(step string, outcome sdlc.OnboardOutcome, detail, remedy string) {
	o.report.Steps = append(o.report.Steps, sdlc.RepoOnboardStep{Step: step, Outcome: outcome, Detail: detail, Remedy: remedy})
}

func (o *repoOnboarding) finding(step, detail, remedy string) {
	o.record(step, sdlc.OnboardFinding, detail, remedy)
}

// blocked reports whether any named step ended in a finding.
func (o *repoOnboarding) blocked(steps ...string) bool {
	for _, name := range steps {
		if step, ok := o.report.Step(name); ok && step.Outcome == sdlc.OnboardFinding {
			return true
		}
	}
	return false
}

// OnboardRepo onboards one forge repository like an existing bound project:
// it checks the repository's signing key and forge token, generates its
// webhook secret, creates its project from the template, binds it with the
// template's binding pointed at the new repository, provisions its forge and
// reports its conformance.
//
// EVERY STEP CHECKS BEFORE IT ACTS, so a second run of the same request finds
// everything in place, reports every step unchanged and writes nothing. What
// the module may not do itself — store a forge token, import key material,
// write branch protection, commit to the repository — it reports as a named
// finding with the remedy, and the steps that depend on it as blocked; the
// run is repeated once the operator has put it right.
//
// An unknown template, a template that is not bound, or a request naming the
// template's own repository is refused with a typed error.
func (s *SDLCService) OnboardRepo(ctx context.Context, tenantID string, req sdlc.RepoOnboardRequest) (*sdlc.RepoOnboardingReport, error) {
	if tenantID == "" {
		return nil, &sdlc.InvalidInputError{Field: "tenant_id", Message: "onboarding is tenant-scoped"}
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	template, err := s.project(ctx, tenantID, req.Like)
	if err != nil {
		return nil, err
	}
	like, err := s.store.GetBinding(ctx, tenantID, req.Like)
	if err != nil {
		return nil, err
	}
	if like.Forge.Repo == req.Repo {
		return nil, &sdlc.InvalidInputError{Field: "repo", Message: "the repository is the template's own: " + req.Repo}
	}
	o := &repoOnboarding{
		tenantID: tenantID, req: req, template: template, like: like,
		report: &sdlc.RepoOnboardingReport{TenantID: tenantID, Repo: like.Forge.Owner + "/" + req.Repo},
	}
	steps := []func(context.Context, *repoOnboarding) error{
		s.onboardSigningKey, s.onboardForgeToken, s.onboardWebhookSecret,
		s.onboardProject, s.onboardBinding, s.onboardForge, s.onboardConformance,
		s.onboardPreflight,
	}
	for _, step := range steps {
		if err := step(ctx, o); err != nil {
			return nil, err
		}
	}
	return o.report, nil
}

// onboardSigningKey checks the key the repository's agent signs with exists.
// Onboarding never imports key material.
func (s *SDLCService) onboardSigningKey(ctx context.Context, o *repoOnboarding) error {
	ref := o.signingKey()
	err := requireSSHKeyRef(ctx, s.sshKeys, o.tenantID, "signing_key", ref)
	var notFound *quicbaaserrors.ReferenceNotFoundError
	switch {
	case err == nil:
		o.record(sdlc.OnboardStepSigningKey, sdlc.OnboardUnchanged, "signing key "+ref.Name+" exists", "")
	case errors.As(err, &notFound):
		o.finding(sdlc.OnboardStepSigningKey, "signing key "+notFound.Ref+" does not exist",
			"generate or import the SSH signing key "+ref.Name+" in scope "+onboardSigningKeyScope+" for the repository's agent")
	case s.sshKeys == nil:
		o.finding(sdlc.OnboardStepSigningKey, "this node reads no SSH keys, so signing key "+ref.Name+" cannot be checked",
			"run the onboarding on a node with the key service configured")
	default:
		return err
	}
	return nil
}

// onboardForgeToken checks the repository's forge token is stored. The
// operator stores it: the forge user and token are made outside the module.
func (s *SDLCService) onboardForgeToken(ctx context.Context, o *repoOnboarding) error {
	ref := sdlc.ForgeTokenRef(o.req.Repo)
	token, err := requireSecretRef(ctx, s.secrets, o.tenantID, "forge.token_secret_ref", ref)
	var notFound *quicbaaserrors.ReferenceNotFoundError
	switch {
	case errors.As(err, &notFound), err == nil && len(token) == 0:
		o.finding(sdlc.OnboardStepForgeToken, "forge token secret "+ref+" is missing or empty",
			"create the repository agent's forge token and store it as the tenant secret "+ref)
	case err != nil:
		return err
	default:
		o.record(sdlc.OnboardStepForgeToken, sdlc.OnboardUnchanged, "forge token secret "+ref+" exists", "")
	}
	return nil
}

// onboardWebhookSecret generates the repository's webhook HMAC key when it is
// missing, in the tenant's own keyspace.
//
// A poll-only template names no webhook secret, and neither does the binding
// copied from it, so no secret is generated for it.
func (s *SDLCService) onboardWebhookSecret(ctx context.Context, o *repoOnboarding) error {
	if o.pollOnly() {
		o.record(sdlc.OnboardStepWebhookSecret, sdlc.OnboardUnchanged, "the template is poll-only; no webhook secret is needed", "")
		return nil
	}
	ref := sdlc.WebhookSecretRef(o.req.Repo)
	tenantCtx := tenantctx.WithTenant(ctx, o.tenantID)
	_, err := s.secrets.GetSecret(tenantCtx, &GetSecretRequest{SecretID: ref})
	if err == nil {
		o.record(sdlc.OnboardStepWebhookSecret, sdlc.OnboardUnchanged, "webhook secret "+ref+" exists", "")
		return nil
	}
	if !quicbaaserrors.IsNotFound(err) {
		return err
	}
	if _, err := s.secrets.EnsureSecret(tenantCtx, &EnsureSecretRequest{
		SecretID: ref, Length: onboardWebhookSecretLength, Encoding: "hex", TenantID: o.tenantID,
	}); err != nil {
		return err
	}
	o.record(sdlc.OnboardStepWebhookSecret, sdlc.OnboardCreated, "generated webhook secret "+ref, "")
	return nil
}

// onboardProject finds the repository's project by name, or creates it from
// the template once its signing key and forge token are in place.
func (s *SDLCService) onboardProject(ctx context.Context, o *repoOnboarding) error {
	resp, err := s.projects.GetProjectByName(ctx, &GetProjectByNameRequest{TenantID: o.tenantID, Name: o.req.Repo})
	var notFound *ProjectNotFoundError
	switch {
	case err == nil && resp != nil && resp.Project != nil:
		o.project = resp.Project
		o.report.ProjectID = o.project.ID
		o.record(sdlc.OnboardStepProject, sdlc.OnboardUnchanged, "project "+o.project.Name+" exists", "")
		return nil
	case err != nil && !errors.As(err, &notFound):
		return err
	}
	if o.blocked(sdlc.OnboardStepSigningKey, sdlc.OnboardStepForgeToken) {
		o.finding(sdlc.OnboardStepProject, "blocked: the project is created once its signing key and forge token exist", onboardRerunRemedy)
		return nil
	}
	create, err := o.createProjectRequest()
	if err != nil {
		return err
	}
	created, err := s.projects.CreateProject(ctx, create)
	if err != nil {
		return err
	}
	o.project = created.Project
	o.report.ProjectID = o.project.ID
	o.record(sdlc.OnboardStepProject, sdlc.OnboardCreated, "created project "+o.project.Name+" like "+o.template.Name, "")
	return nil
}

// onboardBinding binds the project with the template's binding pointed at
// the repository, unless it is bound already.
func (s *SDLCService) onboardBinding(ctx context.Context, o *repoOnboarding) error {
	if o.project == nil || o.blocked(sdlc.OnboardStepForgeToken) {
		o.finding(sdlc.OnboardStepBinding, "blocked: the project is bound once it exists and its forge token is stored", onboardRerunRemedy)
		return nil
	}
	existing, err := s.store.GetBinding(ctx, o.tenantID, o.project.ID)
	switch {
	case err == nil:
		o.binding = existing
		o.record(sdlc.OnboardStepBinding, sdlc.OnboardUnchanged, "project "+o.project.Name+" is bound", "")
		return nil
	case !errors.Is(err, sdlc.ErrNotFound):
		return err
	}
	// A preflight with commands to run binds paused: autonomy is then the
	// preflight's to switch on. With none there is nothing to gate on.
	binding := o.bindingFor()
	gated := o.req.Preflight && len(binding.Verification) > 0
	bound, err := s.Bind(ctx, o.tenantID, binding, BindOptions{LeavePaused: o.req.Paused || gated})
	var refused *sdlc.VerificationCommandRefusedError
	if errors.As(err, &refused) {
		o.finding(sdlc.OnboardStepBinding, "not bound: "+refused.Error(),
			"add the command to orchestration.verification_commands or drop it from "+o.template.Name+"'s verification, then run the onboarding again")
		return nil
	}
	if err != nil {
		return err
	}
	o.binding = bound
	detail := "bound like " + o.template.Name + "; autonomy on"
	switch {
	case o.req.Paused:
		detail = "bound like " + o.template.Name + "; paused"
	case gated:
		detail = "bound like " + o.template.Name + "; paused until the preflight passes"
	}
	o.record(sdlc.OnboardStepBinding, sdlc.OnboardCreated, detail, "")
	// Bind provisions the forge as its last step.
	o.record(sdlc.OnboardStepForge, sdlc.OnboardCreated, "labels and webhook provisioned by the bind", "")
	return nil
}

// onboardForge provisions the labels and webhook of a project that was bound
// already, only when conformance says one is missing.
func (s *SDLCService) onboardForge(ctx context.Context, o *repoOnboarding) error {
	if _, ran := o.report.Step(sdlc.OnboardStepForge); ran {
		return nil
	}
	if o.binding == nil {
		o.finding(sdlc.OnboardStepForge, "blocked: the forge is provisioned once the project is bound", onboardRerunRemedy)
		return nil
	}
	check := s.onboarder.Check(ctx, o.tenantID, o.binding)
	if checkSettled(check, sdlc.CheckWebhook) && checkSettled(check, sdlc.CheckLabels) {
		o.record(sdlc.OnboardStepForge, sdlc.OnboardUnchanged, "labels and webhook are in place", "")
		return nil
	}
	onboarded, err := s.Onboard(ctx, o.tenantID, o.binding.ProjectID)
	if err != nil {
		return err
	}
	detail := "webhook ensured"
	if len(onboarded.LabelsCreated) > 0 {
		detail += "; created labels " + strings.Join(onboarded.LabelsCreated, ", ")
	}
	o.record(sdlc.OnboardStepForge, sdlc.OnboardCreated, detail, "")
	return nil
}

// onboardConformance reports what onboarding may not write itself: the
// branch protection the forge port only reads, and workflows only the
// repository's own agent may remove.
func (s *SDLCService) onboardConformance(ctx context.Context, o *repoOnboarding) error {
	if o.binding == nil {
		o.finding(sdlc.OnboardStepProtection, "blocked: protection is checked once the project is bound", onboardRerunRemedy)
		o.finding(sdlc.OnboardStepWorkflows, "blocked: workflows are checked once the project is bound", onboardRerunRemedy)
		return nil
	}
	report, err := s.Conformance(ctx, o.tenantID, o.binding.ProjectID)
	if err != nil {
		return err
	}
	o.report.Conformance = report
	o.conformanceStep(report, sdlc.OnboardStepProtection, sdlc.CheckProtection, "")
	o.conformanceStep(report, sdlc.OnboardStepWorkflows, sdlc.CheckWorkflows,
		"onboarding does not commit to the repository: remove them in a commit signed by "+o.signingKey().Name)
	return nil
}

// onboardPreflight runs the binding's verification commands once, on the
// default branch, in the worker's image, and switches autonomy on only when
// every one passed. It never switches autonomy off, and it gates only the
// switching on: a project already autonomous is left as it is, unrun.
func (s *SDLCService) onboardPreflight(ctx context.Context, o *repoOnboarding) error {
	if !o.req.Preflight {
		return nil
	}
	if o.binding == nil {
		o.finding(sdlc.OnboardStepPreflight, "blocked: the preflight runs once the project is bound", onboardRerunRemedy)
		return nil
	}
	commands := o.binding.Verification
	if len(commands) == 0 {
		o.record(sdlc.OnboardStepPreflight, sdlc.OnboardUnchanged, "no verification commands configured; nothing to preflight", "")
		return nil
	}
	project, err := s.project(ctx, o.tenantID, o.binding.ProjectID)
	if err != nil {
		return err
	}
	if project.Autonomous {
		o.record(sdlc.OnboardStepPreflight, sdlc.OnboardUnchanged, "autonomy is already on; the preflight only gates switching it on", "")
		return nil
	}
	if refused := s.refusedCommands(commands); len(refused) > 0 {
		o.finding(sdlc.OnboardStepPreflight,
			"not run: the operator allowlist refuses "+strings.Join(refused, ", ")+"; autonomy stays off",
			"add the command(s) to orchestration.verification_commands or drop them from the binding, then rerun with --preflight")
		return nil
	}
	held := s.preflight.Load()
	if held == nil {
		o.finding(sdlc.OnboardStepPreflight, "not run: no job engine is wired for the preflight; autonomy stays off", onboardRerunRemedy)
		return nil
	}
	result, err := sdlc.RunPreflight(ctx, held.jobs, o.tenantID, o.binding)
	if err != nil {
		o.finding(sdlc.OnboardStepPreflight, "no verdict: "+err.Error()+"; autonomy stays off", onboardRerunRemedy)
		return nil
	}
	if !result.Passed() {
		o.finding(sdlc.OnboardStepPreflight,
			"preflight job "+result.JobID+" is red; autonomy stays off\n"+result.FailureReport(),
			"fix the default branch or the worker image, then rerun with --preflight")
		return nil
	}
	detail := "preflight job " + result.JobID + " passed: " + strconv.Itoa(len(result.Results)) + " command(s)"
	if o.req.Paused {
		o.record(sdlc.OnboardStepPreflight, sdlc.OnboardUnchanged, detail+"; paused as asked", "")
		return nil
	}
	if err := s.enableAutonomy(ctx, project); err != nil {
		return err
	}
	o.record(sdlc.OnboardStepPreflight, sdlc.OnboardCreated, detail+"; autonomy on", "")
	return nil
}

// refusedCommands is every verification command the operator allowlist
// refuses, so the finding names all of them rather than the first.
func (s *SDLCService) refusedCommands(commands []string) []string {
	var refused []string
	for _, command := range commands {
		trimmed := strings.TrimSpace(command)
		if err := s.verification.CheckCommand(trimmed, trimmed); err != nil {
			refused = append(refused, strconv.Quote(trimmed))
		}
	}
	return refused
}

// conformanceStep records one conformance check as an onboarding step: a
// settled check is unchanged, anything else a finding carrying the check's
// own findings.
func (o *repoOnboarding) conformanceStep(report *sdlc.ConformanceReport, step, check, remedy string) {
	if checkSettled(report, check) {
		o.record(step, sdlc.OnboardUnchanged, check+" conforms", "")
		return
	}
	var details, remedies []string
	for _, f := range report.Findings {
		if f.Check != check {
			continue
		}
		details = append(details, f.Detail)
		if !slices.Contains(remedies, f.Remedy) {
			remedies = append(remedies, f.Remedy)
		}
	}
	if remedy != "" {
		remedies = append(remedies, remedy)
	}
	o.finding(step, strings.Join(details, "; "), strings.Join(remedies, "; "))
}

// checkSettled reports whether the named check passed or did not apply.
func checkSettled(report *sdlc.ConformanceReport, name string) bool {
	for _, check := range report.Checks {
		if check.Name == name {
			return check.Status == sdlc.StatusPass || check.Status == sdlc.StatusNotApplicable
		}
	}
	return false
}

// signingKey is the KMS reference of the repository agent's signing key.
func (o *repoOnboarding) signingKey() orchestration.KMSKeyRef {
	return orchestration.KMSKeyRef{
		Name:  sdlc.SigningKeyName(o.req.Repo),
		Scope: onboardSigningKeyScope,
		Owner: o.template.Git.Signing.Key.Owner,
	}
}

// createProjectRequest is the template's project with the repository
// swapped in: the same workspace root, context repositories, quotas and
// defaults, its own repository URL, author and signing key, and autonomy off
// until the bind turns it on.
func (o *repoOnboarding) createProjectRequest() (*CreateProjectRequest, error) {
	t := o.template
	oldRepo, newRepo := o.like.Forge.Repo, o.req.Repo
	repoURL, ok := swapRepoSegment(t.Git.RepoURL, oldRepo, newRepo)
	if !ok {
		return nil, &sdlc.InvalidInputError{Field: "like", Message: "the template's repository URL does not end in its repository " + oldRepo + ": " + t.Git.RepoURL}
	}
	maxConcurrency := o.req.MaxConcurrency
	if maxConcurrency == 0 {
		maxConcurrency = t.Quotas.MaxConcurrency
	}
	signing := o.signingKey()
	req := &CreateProjectRequest{
		TenantID:             o.tenantID,
		Name:                 newRepo,
		Description:          "Onboarded like " + t.Name,
		WorkspaceFleetNodeID: t.Workspace.FleetNodeID,
		HostAffinity:         t.HostAffinity,
		DefaultLeadAgent:     t.DefaultLeadAgent,
		DefaultWorkflow:      t.DefaultWorkflow,
		GitRepoURL:           repoURL,
		GitDefaultBranch:     t.Git.DefaultBranch,
		FleetNodeRoles:       t.Permissions.FleetNodeRoles,
		ContextRepos:         slices.Clone(t.ContextRepos),
		GitAuthorName:        strings.ReplaceAll(t.Git.Author.Name, oldRepo, newRepo),
		GitAuthorEmail:       swapEmailLocalPart(t.Git.Author.Email, oldRepo, newRepo),
		GitSigningEnabled:    true,
		GitSigningAlgorithm:  t.Git.Signing.Algorithm,
		GitSigningKeyName:    signing.Name,
		GitSigningKeyScope:   signing.Scope,
		GitSigningKeyOwner:   signing.Owner,
		GPUMinutes:           t.Quotas.GPUMinutes,
		MaxConcurrency:       maxConcurrency,
		Spend:                t.Spend,
		WorkspaceManaged:     t.Provisioning.Managed,
		WorkspaceTier:        t.Storage.WorkspaceTier,
		WorkspaceTmpfsMB:     t.Storage.WorkspaceTmpfsMB,
		ContainerMemoryMB:    t.Storage.ContainerMemoryMB,
		SessionRotation:      t.SessionRotation,
	}
	if t.Git.SSHKey.Name != "" {
		req.GitSSHKeyName = strings.ReplaceAll(t.Git.SSHKey.Name, oldRepo, newRepo)
		req.GitSSHKeyScope = t.Git.SSHKey.Scope
		req.GitSSHKeyOwner = t.Git.SSHKey.Owner
	}
	switch {
	case t.Provisioning.Managed:
		// A managed workspace is cloned by the server, over HTTPS with the
		// repository's own forge token.
		if strings.HasPrefix(repoURL, "https://") && req.GitSSHKeyName == "" {
			req.GitCredentialSecretRef = sdlc.ForgeTokenRef(newRepo)
		}
	case t.Workspace.Path != "":
		workspace, ok := swapWorkspacePath(t.Workspace.Path, oldRepo, newRepo)
		if !ok {
			return nil, &sdlc.InvalidInputError{Field: "like", Message: "the template's workspace path does not end in its repository " + oldRepo + ": " + t.Workspace.Path}
		}
		req.WorkspacePath = workspace
	}
	return req, nil
}

// pollOnly reports whether the template binding declares no webhook delivery.
func (o *repoOnboarding) pollOnly() bool {
	return strings.TrimSpace(o.like.Forge.WebhookSecretRef) == ""
}

// bindingFor is the template's binding pointed at the repository: its own
// project, repository and secrets, and the status contexts every onboarded
// repository requires beside the template's.
func (o *repoOnboarding) bindingFor() *sdlc.ProjectBinding {
	binding := *o.like
	binding.ID = 0
	binding.Epoch = ""
	binding.CreatedAt, binding.UpdatedAt = time.Time{}, time.Time{}
	binding.ProjectID = o.project.ID
	binding.Forge.Repo = o.req.Repo
	binding.Forge.TokenSecretRef = sdlc.ForgeTokenRef(o.req.Repo)
	// A poll-only template stays poll-only: an empty webhook reference is the
	// template's declaration, not a gap to fill.
	if !o.pollOnly() {
		binding.Forge.WebhookSecretRef = sdlc.WebhookSecretRef(o.req.Repo)
	}
	contexts := slices.Clone(o.like.Onboarding.StatusContexts())
	for _, required := range sdlc.RepoOnboardStatusContexts {
		if !slices.Contains(contexts, required) {
			contexts = append(contexts, required)
		}
	}
	binding.Onboarding.RequiredStatusContexts = contexts
	return &binding
}

// swapRepoSegment replaces the last path segment of a repository URL, which
// must name oldRepo, with newRepo, keeping a ".git" suffix.
func swapRepoSegment(url, oldRepo, newRepo string) (string, bool) {
	trimmed := strings.TrimSuffix(url, ".git")
	cut := strings.LastIndexAny(trimmed, "/:")
	if cut < 0 || trimmed[cut+1:] != oldRepo {
		return "", false
	}
	return trimmed[:cut+1] + newRepo + url[len(trimmed):], true
}

// swapWorkspacePath replaces oldRepo in the last element of a workspace
// path, so two projects never share a checkout.
func swapWorkspacePath(workspace, oldRepo, newRepo string) (string, bool) {
	dir, base := path.Split(workspace)
	if !strings.Contains(base, oldRepo) {
		return "", false
	}
	return dir + strings.ReplaceAll(base, oldRepo, newRepo), true
}

// swapEmailLocalPart replaces oldRepo in an email's local part only, so a
// short repository name never rewrites the domain.
func swapEmailLocalPart(email, oldRepo, newRepo string) string {
	local, domain, ok := strings.Cut(email, "@")
	if !ok {
		return strings.ReplaceAll(email, oldRepo, newRepo)
	}
	return strings.ReplaceAll(local, oldRepo, newRepo) + "@" + domain
}
