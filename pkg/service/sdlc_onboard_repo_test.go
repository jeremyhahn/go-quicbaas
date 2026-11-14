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
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/giteafake"
)

// sdlcEnsuredSecret is one secret the fake secrets service generated: the
// tenant the request named and the tenant its context carried.
type sdlcEnsuredSecret struct {
	secretID      string
	requestTenant string
	contextTenant string
}

func (s *sdlcFakeSecrets) EnsureSecret(ctx context.Context, req *EnsureSecretRequest) (*EnsureSecretResponse, error) {
	contextTenant, err := tenantctx.FromContext(ctx, "secrets.EnsureSecret")
	if err != nil {
		return nil, err
	}
	s.ensured = append(s.ensured, sdlcEnsuredSecret{secretID: req.SecretID, requestTenant: req.TenantID, contextTenant: contextTenant})
	data := []byte(strings.Repeat("ab", req.Length))
	s.secrets[req.SecretID] = data
	return &EnsureSecretResponse{SecretID: req.SecretID, Data: data, Generated: true}, nil
}

func (p *sdlcFakeProjects) GetProjectByName(_ context.Context, req *GetProjectByNameRequest) (*GetProjectResponse, error) {
	for _, project := range p.projects {
		if project.TenantID == req.TenantID && project.Name == req.Name {
			return &GetProjectResponse{Project: project}, nil
		}
	}
	return nil, &ProjectNotFoundError{TenantID: req.TenantID, Name: req.Name}
}

func (p *sdlcFakeProjects) CreateProject(_ context.Context, req *CreateProjectRequest) (*CreateProjectResponse, error) {
	project := &orchestration.Project{
		ID: "proj-" + req.Name, TenantID: req.TenantID, Name: req.Name, Description: req.Description,
		Autonomous: req.Autonomous, ContextRepos: req.ContextRepos,
		Workspace: orchestration.ProjectWorkspace{FleetNodeID: req.WorkspaceFleetNodeID, Path: req.WorkspacePath},
		Git: orchestration.ProjectGit{
			RepoURL: req.GitRepoURL, DefaultBranch: req.GitDefaultBranch,
			Author: orchestration.GitAuthor{Name: req.GitAuthorName, Email: req.GitAuthorEmail},
			Signing: orchestration.CommitSigning{
				Enabled: req.GitSigningEnabled, Algorithm: req.GitSigningAlgorithm,
				Key: orchestration.KMSKeyRef{Name: req.GitSigningKeyName, Scope: req.GitSigningKeyScope, Owner: req.GitSigningKeyOwner},
			},
		},
		Quotas: orchestration.ProjectQuotas{GPUMinutes: req.GPUMinutes, MaxConcurrency: req.MaxConcurrency},
	}
	p.created++
	p.projects[project.ID] = project
	return &CreateProjectResponse{Project: project}, nil
}

const (
	onboardTestRepo       = "web"
	onboardTestSigningKey = "service/" + sdlcTestTenant + "/agent-web-signing"
)

// onboardRig is a service rig whose template project "api" is bound with
// its forge pointed at a fake serving the repository being onboarded, so the
// copied binding reaches it.
type onboardRig struct {
	*sdlcServiceRig
	web *giteafake.Server
}

// newOnboardRig is ready to onboard "web" like the bound "api": the signing
// key and forge token exist and the branch protection conforms.
func newOnboardRig(t *testing.T) *onboardRig {
	t.Helper()
	return newOnboardRigFrom(t, sdlcTestPublicURL, func(*sdlc.ProjectBinding) {})
}

// newOnboardRigFrom is newOnboardRig on a node whose public URL is publicURL,
// with the template binding changed by edit before it is stored.
func newOnboardRigFrom(t *testing.T, publicURL string, edit func(*sdlc.ProjectBinding)) *onboardRig {
	t.Helper()
	rig := &onboardRig{sdlcServiceRig: newSDLCServiceRigAt(t, publicURL), web: giteafake.New(t, "acme", onboardTestRepo)}
	template := rig.binding()
	edit(template)
	template.Forge.BaseURL = rig.web.APIURL()
	if err := rig.store.PutBinding(context.Background(), sdlcTestTenant, template); err != nil {
		t.Fatal(err)
	}
	rig.sshKeys.keys[onboardTestSigningKey] = true
	rig.secrets.secrets[sdlc.ForgeTokenRef(onboardTestRepo)] = []byte("web-token")
	rig.web.PutProtection(sdlc.DefaultProtectedBranch, true, sdlc.RepoOnboardStatusContexts...)
	return rig
}

func (r *onboardRig) onboard(t *testing.T, req sdlc.RepoOnboardRequest) *sdlc.RepoOnboardingReport {
	t.Helper()
	report, err := r.svc.OnboardRepo(context.Background(), sdlcTestTenant, req)
	if err != nil {
		t.Fatalf("OnboardRepo: %v", err)
	}
	return report
}

func onboardRequest() sdlc.RepoOnboardRequest {
	return sdlc.RepoOnboardRequest{Repo: onboardTestRepo, Like: sdlcTestProject}
}

func requireStep(t *testing.T, report *sdlc.RepoOnboardingReport, name string, want sdlc.OnboardOutcome) sdlc.RepoOnboardStep {
	t.Helper()
	step, ok := report.Step(name)
	if !ok {
		t.Fatalf("step %s did not run: %+v", name, report.Steps)
	}
	if step.Outcome != want {
		t.Fatalf("step %s = %s (%s), want %s", name, step.Outcome, step.Detail, want)
	}
	return step
}

func TestOnboardRepo_FirstRunCreatesWhatIsMissing(t *testing.T) {
	rig := newOnboardRig(t)

	report := rig.onboard(t, onboardRequest())

	requireStep(t, report, sdlc.OnboardStepSigningKey, sdlc.OnboardUnchanged)
	requireStep(t, report, sdlc.OnboardStepForgeToken, sdlc.OnboardUnchanged)
	requireStep(t, report, sdlc.OnboardStepWebhookSecret, sdlc.OnboardCreated)
	requireStep(t, report, sdlc.OnboardStepProject, sdlc.OnboardCreated)
	requireStep(t, report, sdlc.OnboardStepBinding, sdlc.OnboardCreated)
	requireStep(t, report, sdlc.OnboardStepForge, sdlc.OnboardCreated)
	requireStep(t, report, sdlc.OnboardStepProtection, sdlc.OnboardUnchanged)
	requireStep(t, report, sdlc.OnboardStepWorkflows, sdlc.OnboardUnchanged)
	if report.Conformance == nil || !report.Conformance.Conformant {
		t.Fatalf("conformance = %+v, want conformant", report.Conformance)
	}
	if report.Repo != "acme/web" {
		t.Fatalf("repo = %q, want acme/web", report.Repo)
	}

	project := rig.projects.projects[report.ProjectID]
	if project == nil {
		t.Fatalf("project %q was not created", report.ProjectID)
	}
	if project.Name != onboardTestRepo || project.Git.RepoURL != "https://forge.test/acme/web.git" {
		t.Fatalf("project = %s %s, want web at https://forge.test/acme/web.git", project.Name, project.Git.RepoURL)
	}
	if project.Git.Author.Name != "agent-web" || project.Git.Author.Email != "agent-web@example.test" {
		t.Fatalf("author = %+v, want agent-web", project.Git.Author)
	}
	wantKey := orchestration.KMSKeyRef{Name: "agent-web-signing", Scope: "service"}
	if !project.Git.Signing.Enabled || project.Git.Signing.Key != wantKey {
		t.Fatalf("signing = %+v, want enabled with %+v", project.Git.Signing, wantKey)
	}
	if !project.Autonomous {
		t.Fatal("an unpaused onboarding left the project paused")
	}

	binding, err := rig.store.GetBinding(context.Background(), sdlcTestTenant, report.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Forge.Repo != onboardTestRepo ||
		binding.Forge.TokenSecretRef != "sdlc/forge/web/token" ||
		binding.Forge.WebhookSecretRef != "sdlc/webhook/web" {
		t.Fatalf("binding forge = %+v, want web with its own secrets", binding.Forge)
	}
	for _, required := range sdlc.RepoOnboardStatusContexts {
		if !strings.Contains(strings.Join(binding.Onboarding.StatusContexts(), "\n"), required) {
			t.Fatalf("binding requires %v, missing %q", binding.Onboarding.StatusContexts(), required)
		}
	}
	if hooks := rig.web.HookURLs(); len(hooks) != 1 {
		t.Fatalf("hooks = %v, want the one the bind provisioned", hooks)
	}
}

// TestOnboardRepo_FreshRepoIsConformant is #1041's first verification line:
// one run on a fresh repository leaves a project whose own conformance pass
// is clean, every check passing, as the hand-driven onboarding did.
func TestOnboardRepo_FreshRepoIsConformant(t *testing.T) {
	rig := newOnboardRig(t)
	onboarded := rig.onboard(t, onboardRequest())

	report, err := rig.svc.Conformance(context.Background(), sdlcTestTenant, onboarded.ProjectID)
	if err != nil {
		t.Fatal(err)
	}

	if !report.Conformant || len(report.Findings) != 0 {
		t.Fatalf("conformant = %v, findings = %+v, want conformant with none", report.Conformant, report.Findings)
	}
	// The conformance pass runs the binding check, then its forge checks in
	// this order; the order is part of the report's contract.
	want := []string{sdlc.CheckBinding, sdlc.CheckWebhook, sdlc.CheckLabels, sdlc.CheckWorkflows, sdlc.CheckProtection}
	if len(report.Checks) != len(want) {
		t.Fatalf("checks = %+v, want exactly %v", report.Checks, want)
	}
	for i, check := range report.Checks {
		if check.Name != want[i] || check.Status != sdlc.StatusPass {
			t.Fatalf("check %d = %s %s, want %s pass", i, check.Name, check.Status, want[i])
		}
	}
}

func TestOnboardRepo_SecondRunChangesNothing(t *testing.T) {
	rig := newOnboardRig(t)
	first := rig.onboard(t, onboardRequest())
	before, err := rig.store.GetBinding(context.Background(), sdlcTestTenant, first.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	labels, hooks := rig.web.Calls("POST /labels"), rig.web.Calls("POST /hooks")
	updates := rig.projects.updates

	second := rig.onboard(t, onboardRequest())

	for _, step := range second.Steps {
		if step.Outcome != sdlc.OnboardUnchanged {
			t.Fatalf("second run step %s = %s (%s), want unchanged", step.Step, step.Outcome, step.Detail)
		}
	}
	if len(second.Steps) != 8 {
		t.Fatalf("second run ran %d steps, want 8", len(second.Steps))
	}
	if rig.projects.created != 1 || rig.projects.updates != updates || len(rig.secrets.ensured) != 1 {
		t.Fatalf("second run wrote: projects created %d updated %d, secrets generated %d",
			rig.projects.created, rig.projects.updates-updates, len(rig.secrets.ensured))
	}
	if rig.web.Calls("POST /labels") != labels || rig.web.Calls("POST /hooks") != hooks {
		t.Fatal("second run wrote to the forge")
	}
	after, err := rig.store.GetBinding(context.Background(), sdlcTestTenant, second.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Epoch != before.Epoch || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatal("second run rewrote the binding")
	}
}

func TestOnboardRepo_ProvisionsTheForgeOfABoundProjectMissingIt(t *testing.T) {
	rig := newOnboardRig(t)
	rig.onboard(t, onboardRequest())
	rig.web.DropHooks()

	report := rig.onboard(t, onboardRequest())

	requireStep(t, report, sdlc.OnboardStepBinding, sdlc.OnboardUnchanged)
	requireStep(t, report, sdlc.OnboardStepForge, sdlc.OnboardCreated)
	if hooks := rig.web.HookURLs(); len(hooks) != 1 {
		t.Fatalf("hooks = %v, want the webhook re-provisioned", hooks)
	}
}

func TestOnboardRepo_WebhookSecretLandsInTheTenantKeyspace(t *testing.T) {
	rig := newOnboardRig(t)

	rig.onboard(t, onboardRequest())

	if len(rig.secrets.ensured) != 1 {
		t.Fatalf("generated %d secrets, want 1", len(rig.secrets.ensured))
	}
	got := rig.secrets.ensured[0]
	if got.secretID != "sdlc/webhook/web" || got.requestTenant != sdlcTestTenant || got.contextTenant != sdlcTestTenant {
		t.Fatalf("generated %+v, want sdlc/webhook/web in tenant %s", got, sdlcTestTenant)
	}
}

func TestOnboardRepo_PausedLeavesAutonomyOff(t *testing.T) {
	rig := newOnboardRig(t)
	req := onboardRequest()
	req.Paused = true

	report := rig.onboard(t, req)

	step := requireStep(t, report, sdlc.OnboardStepBinding, sdlc.OnboardCreated)
	if !strings.Contains(step.Detail, "paused") {
		t.Fatalf("binding detail = %q, want it to say paused", step.Detail)
	}
	if rig.projects.projects[report.ProjectID].Autonomous {
		t.Fatal("a paused onboarding turned autonomy on")
	}
}

func TestOnboardRepo_MaxConcurrencyOverridesTheTemplate(t *testing.T) {
	rig := newOnboardRig(t)
	req := onboardRequest()
	req.MaxConcurrency = 3

	report := rig.onboard(t, req)

	if got := rig.projects.projects[report.ProjectID].Quotas.MaxConcurrency; got != 3 {
		t.Fatalf("max concurrency = %d, want 3", got)
	}
}

func TestOnboardRepo_MissingTokenIsAFindingAndBlocksTheProject(t *testing.T) {
	rig := newOnboardRig(t)
	delete(rig.secrets.secrets, sdlc.ForgeTokenRef(onboardTestRepo))

	report := rig.onboard(t, onboardRequest())

	step := requireStep(t, report, sdlc.OnboardStepForgeToken, sdlc.OnboardFinding)
	if !strings.Contains(step.Remedy, "sdlc/forge/web/token") {
		t.Fatalf("remedy = %q, want it to name the secret", step.Remedy)
	}
	for _, blocked := range []string{sdlc.OnboardStepProject, sdlc.OnboardStepBinding, sdlc.OnboardStepForge} {
		requireStep(t, report, blocked, sdlc.OnboardFinding)
	}
	if rig.projects.created != 0 {
		t.Fatal("a project was created without its forge token")
	}
	if report.Conformance != nil {
		t.Fatal("an unbound repository reported conformance")
	}
}

func TestOnboardRepo_NodeTokenCommandNeedsNoForgeTokenSecret(t *testing.T) {
	rig := newOnboardRig(t)
	delete(rig.secrets.secrets, sdlc.ForgeTokenRef(onboardTestRepo))
	rig.svc.forgeTokens = map[sdlc.ForgeType]sdlc.ForgeTokenSource{
		rig.binding().Forge.Type: &sdlcFakeTokenSource{token: "web-token"},
	}

	report := rig.onboard(t, onboardRequest())

	step := requireStep(t, report, sdlc.OnboardStepForgeToken, sdlc.OnboardUnchanged)
	if !strings.Contains(step.Detail, "node's token command") {
		t.Fatalf("detail = %q, want it to name the node's token command", step.Detail)
	}
	if rig.projects.created != 1 {
		t.Fatalf("projects created = %d, want 1", rig.projects.created)
	}
}

func TestOnboardRepo_NodeTokenCommandStillNeedsTheSecretForAManagedHTTPSClone(t *testing.T) {
	// The server clones a managed workspace with the forge token secret, not
	// the node's command, so onboarding without it would fail late.
	rig := newOnboardRig(t)
	rig.projects.projects[sdlcTestProject].Provisioning.Managed = true
	delete(rig.secrets.secrets, sdlc.ForgeTokenRef(onboardTestRepo))
	rig.svc.forgeTokens = map[sdlc.ForgeType]sdlc.ForgeTokenSource{
		rig.binding().Forge.Type: &sdlcFakeTokenSource{token: "web-token"},
	}

	report := rig.onboard(t, onboardRequest())

	requireStep(t, report, sdlc.OnboardStepForgeToken, sdlc.OnboardFinding)
	if rig.projects.created != 0 {
		t.Fatal("a managed HTTPS project was created without its forge token secret")
	}
}

func TestOnboardRepo_MissingSigningKeyIsAFinding(t *testing.T) {
	rig := newOnboardRig(t)
	delete(rig.sshKeys.keys, onboardTestSigningKey)

	report := rig.onboard(t, onboardRequest())

	step := requireStep(t, report, sdlc.OnboardStepSigningKey, sdlc.OnboardFinding)
	if !strings.Contains(step.Detail, "agent-web-signing") {
		t.Fatalf("detail = %q, want it to name the key", step.Detail)
	}
	requireStep(t, report, sdlc.OnboardStepProject, sdlc.OnboardFinding)
	if rig.projects.created != 0 {
		t.Fatal("a project was created without its signing key")
	}
}

func TestOnboardRepo_KeyServiceFailureIsReturned(t *testing.T) {
	rig := newOnboardRig(t)
	rig.sshKeys.err = errors.New("key service down")

	if _, err := rig.svc.OnboardRepo(context.Background(), sdlcTestTenant, onboardRequest()); err == nil {
		t.Fatal("a key service failure was reported as a finding")
	}
}

func TestOnboardRepo_MissingProtectionIsAFinding(t *testing.T) {
	rig := newOnboardRig(t)
	rig.web.PutProtection(sdlc.DefaultProtectedBranch, true, sdlc.DefaultReviewStatusContext)

	report := rig.onboard(t, onboardRequest())

	step := requireStep(t, report, sdlc.OnboardStepProtection, sdlc.OnboardFinding)
	if !strings.Contains(step.Detail, sdlc.CIGateStatusContext) {
		t.Fatalf("detail = %q, want it to name the missing context", step.Detail)
	}
	if step.Remedy == "" {
		t.Fatal("a protection finding carries no remedy")
	}
}

func TestOnboardRepo_StaleWorkflowIsAFindingForTheRepoAgent(t *testing.T) {
	rig := newOnboardRig(t)
	rig.web.PutDir(".github/workflows", "automerge.yml")

	report := rig.onboard(t, onboardRequest())

	step := requireStep(t, report, sdlc.OnboardStepWorkflows, sdlc.OnboardFinding)
	if !strings.Contains(step.Detail, "automerge.yml") || !strings.Contains(step.Remedy, "agent-web-signing") {
		t.Fatalf("workflows step = %+v, want automerge.yml removed by agent-web-signing", step)
	}
}

func TestOnboardRepo_RefusesAnUnknownTemplate(t *testing.T) {
	rig := newOnboardRig(t)
	req := onboardRequest()
	req.Like = "nope"
	projects, secrets := len(rig.projects.projects), len(rig.secrets.secrets)

	_, err := rig.svc.OnboardRepo(context.Background(), sdlcTestTenant, req)

	var notFound *ProjectNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("err = %v, want ProjectNotFoundError", err)
	}
	if len(rig.projects.projects) != projects || len(rig.secrets.secrets) != secrets {
		t.Fatalf("a refused onboarding wrote: projects %d -> %d, secrets %d -> %d",
			projects, len(rig.projects.projects), secrets, len(rig.secrets.secrets))
	}
	bindings, err := rig.store.ListBindings(context.Background(), sdlcTestTenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 {
		t.Fatalf("bindings = %d, want only the template's", len(bindings))
	}
	if hooks := rig.web.HookURLs(); len(hooks) != 0 {
		t.Fatalf("hooks = %v, want none provisioned", hooks)
	}
}

func TestOnboardRepo_RefusesAnUnboundTemplate(t *testing.T) {
	rig := newSDLCServiceRig(t)

	_, err := rig.svc.OnboardRepo(context.Background(), sdlcTestTenant, onboardRequest())

	if !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("err = %v, want not found", err)
	}
}

func TestOnboardRepo_RefusesInvalidRequests(t *testing.T) {
	rig := newOnboardRig(t)
	cases := map[string]struct {
		tenant string
		req    sdlc.RepoOnboardRequest
	}{
		"no tenant":         {"", onboardRequest()},
		"no repo":           {sdlcTestTenant, sdlc.RepoOnboardRequest{Like: sdlcTestProject}},
		"the template repo": {sdlcTestTenant, sdlc.RepoOnboardRequest{Repo: "api", Like: sdlcTestProject}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := rig.svc.OnboardRepo(context.Background(), tc.tenant, tc.req)
			var invalid *sdlc.InvalidInputError
			if !errors.As(err, &invalid) {
				t.Fatalf("err = %v, want InvalidInputError", err)
			}
		})
	}
}

func TestSwapRepoSegment(t *testing.T) {
	cases := map[string]struct {
		url, want string
		ok        bool
	}{
		"https with .git": {"https://forge.test/acme/api.git", "https://forge.test/acme/web.git", true},
		"https bare":      {"https://forge.test/acme/api", "https://forge.test/acme/web", true},
		"scp style":       {"git@forge.test:api.git", "git@forge.test:web.git", true},
		"other repo":      {"https://forge.test/acme/apis.git", "", false},
		"empty":           {"", "", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := swapRepoSegment(tc.url, "api", "web")
			if got != tc.want || ok != tc.ok {
				t.Fatalf("swapRepoSegment(%q) = %q, %v; want %q, %v", tc.url, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestSwapWorkspacePath(t *testing.T) {
	if got, ok := swapWorkspacePath("/srv/work/api", "api", "web"); !ok || got != "/srv/work/web" {
		t.Fatalf("swapWorkspacePath = %q, %v; want /srv/work/web", got, ok)
	}
	if _, ok := swapWorkspacePath("/srv/api/checkout", "api", "web"); ok {
		t.Fatal("a path whose last element does not name the repository was swapped")
	}
}

func TestSwapEmailLocalPart(t *testing.T) {
	if got := swapEmailLocalPart("agent-a@a.test", "a", "b"); got != "bgent-b@a.test" {
		t.Fatalf("swapEmailLocalPart = %q, want the domain untouched", got)
	}
	if got := swapEmailLocalPart("agent-api", "api", "web"); got != "agent-web" {
		t.Fatalf("swapEmailLocalPart = %q, want agent-web", got)
	}
}

// TestOnboardRepo_APollOnlyTemplateOnboardsOnANodeWithNoPublicURL: the copied
// binding keeps the template's empty webhook reference, so no webhook secret
// is generated, no delivery is provisioned, and onboarding completes on a node
// that takes no deliveries.
func TestOnboardRepo_APollOnlyTemplateOnboardsOnANodeWithNoPublicURL(t *testing.T) {
	rig := newOnboardRigFrom(t, "", func(b *sdlc.ProjectBinding) { b.Forge.WebhookSecretRef = "" })

	report := rig.onboard(t, onboardRequest())

	requireStep(t, report, sdlc.OnboardStepWebhookSecret, sdlc.OnboardUnchanged)
	requireStep(t, report, sdlc.OnboardStepBinding, sdlc.OnboardCreated)
	if len(rig.secrets.ensured) != 0 {
		t.Fatalf("generated %+v, want no webhook secret for a poll-only template", rig.secrets.ensured)
	}
	binding, err := rig.store.GetBinding(context.Background(), sdlcTestTenant, report.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Forge.WebhookSecretRef != "" {
		t.Fatalf("webhook secret ref = %q, want the template's empty one", binding.Forge.WebhookSecretRef)
	}
	if hooks := rig.web.HookURLs(); len(hooks) != 0 {
		t.Fatalf("hooks = %v, want none for a poll-only repository", hooks)
	}
}
