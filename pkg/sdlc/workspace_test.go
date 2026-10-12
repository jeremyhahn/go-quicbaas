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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The forge access seam — the one way in
// =============================================================================
//
// These tests are the guard on go-quicbaas#929. The defect was not that any one
// runner was wrong: it was that the delivery runner and triage assembled their
// spawns separately, so one of them ended up holding an ssh credential and the
// other did not, and nothing in either runner said so. What is asserted here is
// the seam itself — what it refuses, what it mounts, and the scope each caller
// asks for — because that is where the divergence can no longer be written.

// sshAccessProject is a project on an SSH remote, which is the shape the
// burn-in node runs and the shape triage could not clone.
func sshAccessProject() *orchestration.Project {
	project := testOrchProject()
	project.Git.RepoURL = "ssh://git@forge.test:2222/acme/api.git"
	return project
}

// accessRig is a forgeAccess with fakes behind it.
type accessRig struct {
	access  forgeAccess
	signing *fakeSigningAgent
	tokens  *fakeTokens
	forge   ForgeConfig
}

// testRepoGuard is the operator allowlist every test project's forge is on.
func testRepoGuard(t *testing.T) RepoHostGuard {
	t.Helper()
	guard, err := NewRepoHostGuard([]string{"forge.test"}, false)
	if err != nil {
		t.Fatalf("NewRepoHostGuard: %v", err)
	}
	return guard
}

// testWorkspaceKey is the workspace key the rig's plans name.
const testWorkspaceKey = "attempt-1"

// newAccessRig builds the seam over fakes.
func newAccessRig(t *testing.T) *accessRig {
	t.Helper()
	signing := &fakeSigningAgent{}
	tokens := &fakeTokens{token: []byte("t0ken")}
	access, err := newForgeAccess(tokens, signing, testRepoGuard(t), orchestration.WorkspaceCheckoutRoot, testSocketsRoot(t))
	if err != nil {
		t.Fatalf("newForgeAccess: %v", err)
	}
	forge := testBinding().Forge
	forge.SSHHostKey = testSSHHostKey
	return &accessRig{access: access, signing: signing, tokens: tokens, forge: forge}
}

// open runs the seam for one plan, defaulting the project, forge and key.
func (r *accessRig) open(t *testing.T, project *orchestration.Project, scope SigningScope) (*orchestration.SpawnSessionRequest, *containerWorkspace, error) {
	t.Helper()
	req := &orchestration.SpawnSessionRequest{Backend: "claude", ProjectID: project.ID}
	workspace, err := r.access.Open(context.Background(), req, workspacePlan{
		TenantID: project.TenantID,
		Project:  project,
		Forge:    r.forge,
		Branch:   orchestration.WorkspaceBranchPlan{Name: "main"},
		Key:      testWorkspaceKey,
		Scope:    scope,
	})
	if workspace != nil {
		t.Cleanup(func() { workspace.Release(context.Background()) })
	}
	return req, workspace, err
}

// TestForgeAccess_AnSSHCheckoutIsGivenAnAgentTheContainerCanReach is the fix
// for go-quicbaas#929 at the seam: a reader on an ssh remote gets an agent, the
// socket is mounted, and SSH_AUTH_SOCK and GIT_SSH_COMMAND name paths INSIDE
// the container. Without this the container's git offers no client key and the
// forge answers "Permission denied (publickey)".
func TestForgeAccess_AnSSHCheckoutIsGivenAnAgentTheContainerCanReach(t *testing.T) {
	rig := newAccessRig(t)
	req, workspace, err := rig.open(t, sshAccessProject(), SigningScopeCheckout)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if workspace.Signing == nil {
		t.Fatal("a reader on an ssh remote was given no agent; its clone cannot authenticate")
	}
	if req.Env[orchestration.WorkspaceSigningAgentEnv] != orchestration.WorkspaceSigningAgentSocket {
		t.Errorf("%s = %q, want the container's socket path",
			orchestration.WorkspaceSigningAgentEnv, req.Env[orchestration.WorkspaceSigningAgentEnv])
	}
	mounted := false
	for _, mount := range req.ExtraMounts {
		if mount.Target == orchestration.WorkspaceSigningAgentSocket {
			mounted = true
			if mount.Source != workspace.Signing.SocketPath {
				t.Errorf("the mount source is %q, want the agent's socket %q", mount.Source, workspace.Signing.SocketPath)
			}
			if !mount.ReadOnly {
				t.Error("the agent socket was mounted writable")
			}
		}
	}
	if !mounted {
		t.Fatal("the agent's socket was not mounted into the container")
	}
	// The ssh invocation names the mounted known_hosts, so the container
	// verifies the forge strictly against the binding's pinned key.
	command := req.Env[orchestration.WorkspaceSSHCommandEnv]
	if !strings.Contains(command, orchestration.WorkspaceSpecDir) || !strings.Contains(command, "StrictHostKeyChecking=yes") {
		t.Errorf("%s = %q", orchestration.WorkspaceSSHCommandEnv, command)
	}
}

// TestForgeAccess_AReaderAsksForTheCheckoutScopeAndADelivererForDelivery is the
// property that keeps the two apart: one agent implementation, one call site,
// and the ONLY difference between a reader's container and a deliverer's is the
// scope. A reader's session carries no commit signing key at all.
func TestForgeAccess_AReaderAsksForTheCheckoutScopeAndADelivererForDelivery(t *testing.T) {
	rig := newAccessRig(t)
	_, reader, err := rig.open(t, sshAccessProject(), SigningScopeCheckout)
	if err != nil {
		t.Fatalf("Open (reader): %v", err)
	}
	if reader.Signing.PublicKey != "" {
		t.Errorf("a checkout scope served a commit signing key: %q", reader.Signing.PublicKey)
	}
	_, deliverer, err := rig.open(t, sshAccessProject(), SigningScopeDelivery)
	if err != nil {
		t.Fatalf("Open (delivery): %v", err)
	}
	if deliverer.Signing.PublicKey == "" {
		t.Error("a delivery scope served no commit signing key; the worker could not sign")
	}
	want := []SigningScope{SigningScopeCheckout, SigningScopeDelivery}
	got := rig.signing.startedScopes()
	if len(got) != len(want) {
		t.Fatalf("scopes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("scopes = %v, want %v", got, want)
		}
	}
	// Both sockets were bound under the DAEMON's sockets root, each in its own
	// fresh directory: the sockets root is shared by every tenant, so two
	// sessions must never name one directory.
	dirs := rig.signing.startedDirs()
	if len(dirs) != 2 || dirs[0] == dirs[1] {
		t.Fatalf("socket directories = %v, want two distinct ones", dirs)
	}
	for _, dir := range dirs {
		if filepath.Dir(dir) != rig.access.socketsRoot {
			t.Errorf("socket bound under %q, want the sockets root %q", filepath.Dir(dir), rig.access.socketsRoot)
		}
	}
}

// TestForgeAccess_AnHTTPSReaderIsGivenNoAgentAtAll: the strongest posture is
// still available and still taken. An https remote authenticates with the
// token, so a container that signs nothing holds no socket — there is nothing
// in it that could produce a signature of any kind.
func TestForgeAccess_AnHTTPSReaderIsGivenNoAgentAtAll(t *testing.T) {
	rig := newAccessRig(t)
	req, workspace, err := rig.open(t, testOrchProject(), SigningScopeCheckout)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if workspace.Signing != nil {
		t.Fatal("an https reader was handed an agent it has no use for")
	}
	if len(rig.signing.startedDirs()) != 0 {
		t.Fatalf("agents started = %v, want none", rig.signing.startedDirs())
	}
	if _, ok := req.Env[orchestration.WorkspaceSigningAgentEnv]; ok {
		t.Error("SSH_AUTH_SOCK was set for a container with no agent")
	}
	// The token still travels, as a sibling file the container reads through.
	if workspace.Spec.ForgeTokenFile != orchestration.WorkspaceForgeTokenFile {
		t.Errorf("forge token file = %q, want the token written beside the spec", workspace.Spec.ForgeTokenFile)
	}
}

// TestForgeAccess_RefusesAnHTTPSCheckoutWithNoToken is the pre-spawn refusal on
// the token side: a spawn that holds no credential for the remote it named
// never reaches a container. Asserted through the seam rather than only on
// RequireCheckoutCredentials, because the ORDER is the property — the check
// runs before the spec directory is written.
func TestForgeAccess_RefusesAnHTTPSCheckoutWithNoToken(t *testing.T) {
	rig := newAccessRig(t)
	rig.tokens.token = nil
	req, workspace, err := rig.open(t, testOrchProject(), SigningScopeCheckout)
	if !errors.Is(err, orchestration.ErrCheckoutCredentialMissing) {
		t.Fatalf("err = %v, want ErrCheckoutCredentialMissing", err)
	}
	if workspace != nil {
		t.Fatal("a refused open answered with a workspace")
	}
	if len(req.ExtraMounts) != 0 || len(req.Env) != 0 {
		t.Errorf("a refused open left the spawn request carrying mounts=%v env=%v", req.ExtraMounts, req.Env)
	}
}

// TestForgeAccess_RefusesAnAgentThatCameBackUnusable: an agent that answered
// with no session, no socket, or — for a delivery scope — no signing key is
// refused with the scope named, and nothing is left running. A socket path
// that reached a spawn empty would mount nothing and fail inside the container,
// which is the shape of the defect this whole change removes.
func TestForgeAccess_RefusesAnAgentThatCameBackUnusable(t *testing.T) {
	for name, tc := range map[string]struct {
		agent SigningAgent
		scope SigningScope
	}{
		"no session": {agent: &nilSessionAgent{}, scope: SigningScopeCheckout},
		"no socket":  {agent: &socketlessAgent{}, scope: SigningScopeCheckout},
		"a delivery scope with no signing key": {
			agent: &unsignedDeliveryAgent{}, scope: SigningScopeDelivery,
		},
	} {
		t.Run(name, func(t *testing.T) {
			access, err := newForgeAccess(&fakeTokens{token: []byte("t")}, tc.agent, testRepoGuard(t),
				orchestration.WorkspaceCheckoutRoot, testSocketsRoot(t))
			if err != nil {
				t.Fatalf("newForgeAccess: %v", err)
			}
			forge := testBinding().Forge
			forge.SSHHostKey = testSSHHostKey
			req := &orchestration.SpawnSessionRequest{Backend: "claude"}
			_, err = access.Open(context.Background(), req, workspacePlan{
				TenantID: testTenant,
				Project:  sshAccessProject(),
				Forge:    forge,
				Branch:   orchestration.WorkspaceBranchPlan{Name: "main"},
				Key:      testWorkspaceKey,
				Scope:    tc.scope,
			})
			var unusable *SigningSessionError
			if !errors.As(err, &unusable) {
				t.Fatalf("err = %v (%T), want *SigningSessionError", err, err)
			}
			if unusable.Scope != tc.scope {
				t.Errorf("scope = %v, want %v", unusable.Scope, tc.scope)
			}
			if !errors.Is(err, ErrRunner) {
				t.Errorf("the sentinel is not reachable: %v", err)
			}
		})
	}
}

// TestForgeAccess_RefusesADeliveryScopeForAProjectThatSignsNothing: autonomous
// development publishes under an identity, and it is refused here — before a
// container — rather than at publish time, after an attempt's turns have been
// spent on work that could never be published.
func TestForgeAccess_RefusesADeliveryScopeForAProjectThatSignsNothing(t *testing.T) {
	rig := newAccessRig(t)
	project := sshAccessProject()
	project.Git.Signing.Enabled = false
	_, _, err := rig.open(t, project, SigningScopeDelivery)
	var invalid *InvalidInputError
	if !errors.As(err, &invalid) || invalid.Field != "project.git.signing" {
		t.Fatalf("err = %v, want project.git.signing refused", err)
	}
	if len(rig.signing.startedDirs()) != 0 {
		t.Error("an agent was started for a project with no publishing identity")
	}
}

// TestForgeAccess_NamesTheWorkspaceAndPicksNoHostPath is go-quicbaas#981 at
// the seam: the spawn names the workspace — tenant, project, key — and carries
// the decided spec for whichever host runs it, and the seam neither writes a
// file nor names a path on this daemon. The executing host provisions both.
func TestForgeAccess_NamesTheWorkspaceAndPicksNoHostPath(t *testing.T) {
	rig := newAccessRig(t)
	project := sshAccessProject()
	req, workspace, err := rig.open(t, project, SigningScopeCheckout)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ws := req.Workspace
	if ws == nil || ws.TenantID != project.TenantID || ws.ProjectID != project.ID || ws.Key != testWorkspaceKey {
		t.Fatalf("workspace = %+v, want the plan's tenant, project and key", ws)
	}
	if req.WorkDir != "" {
		t.Fatalf("the seam named a host path: %q", req.WorkDir)
	}
	for _, mount := range req.ExtraMounts {
		if mount.Target == orchestration.WorkspaceSpecDir {
			t.Fatalf("the spec directory is the executing host's to write and mount: %+v", mount)
		}
	}
	if ws.Spec == nil || string(ws.Spec.Files[orchestration.WorkspaceForgeTokenFile]) != "t0ken" {
		t.Fatal("the forge token must travel with the spec to the host that writes it")
	}
	if ws.Spec.Spec.KnownHostsFile != orchestration.WorkspaceKnownHostsFile ||
		workspace.Spec.KnownHostsFile != orchestration.WorkspaceKnownHostsFile {
		t.Fatal("the runner must drive the checkout from the spec the container will read")
	}
	if err := ws.Validate(); err != nil {
		t.Fatalf("the named workspace is not one a host could provision: %v", err)
	}
}

// TestForgeAccess_ReleaseStopsTheAgent: the one thing a workspace holds on
// this daemon is the ssh agent, and it goes. A leaked agent is a live signing
// socket on the daemon after its container is gone.
func TestForgeAccess_ReleaseStopsTheAgent(t *testing.T) {
	rig := newAccessRig(t)
	_, workspace, err := rig.open(t, sshAccessProject(), SigningScopeCheckout)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	workspace.Release(context.Background())
	if rig.signing.stopCount() != 1 {
		t.Errorf("agents stopped = %d, want the one this workspace started", rig.signing.stopCount())
	}
	// Idempotent: every unplanned exit arrives here behind a path that may
	// already have released.
	workspace.Release(context.Background())
	if rig.signing.stopCount() != 1 {
		t.Errorf("a second release stopped the agent again: %d", rig.signing.stopCount())
	}
}

// TestForgeAccess_StopSigningLeavesTheSpecAndIsIdempotent: a reader stops its
// agent the moment the checkout is made and then goes on using the spec for the
// rest of its session, so the two lifetimes are separate.
func TestForgeAccess_StopSigningLeavesTheSpecAndIsIdempotent(t *testing.T) {
	rig := newAccessRig(t)
	req, workspace, err := rig.open(t, sshAccessProject(), SigningScopeCheckout)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	workspace.StopSigning(context.Background())
	workspace.StopSigning(context.Background())
	if rig.signing.stopCount() != 1 {
		t.Errorf("agents stopped = %d, want exactly one stop for two calls", rig.signing.stopCount())
	}
	if workspace.Signing != nil || workspace.signingSession() != nil {
		t.Error("the stopped agent is still reachable from the workspace")
	}
	if req.Workspace.Spec == nil {
		t.Error("stopping the agent dropped the spec the container reads")
	}
	// And a nil workspace answers the same questions without panicking: the
	// teardown paths reach here behind a failed open.
	var absent *containerWorkspace
	absent.StopSigning(context.Background())
	absent.Release(context.Background())
	if absent.signingSession() != nil {
		t.Error("a nil workspace produced a signing session")
	}
}

// TestNewForgeAccess_RefusesAWiringNoContainerCouldCloneWith: every
// collaborator is required and named. The signing agent is the one
// go-quicbaas#929 added, because a runner built without it cannot authenticate
// an ssh clone and used to find that out inside a container.
func TestNewForgeAccess_RefusesAWiringNoContainerCouldCloneWith(t *testing.T) {
	sockets := testSocketsRoot(t)
	tokens := &fakeTokens{token: []byte("t")}
	signing := &fakeSigningAgent{}

	guard := testRepoGuard(t)

	if _, err := newForgeAccess(nil, signing, guard, "", sockets); !fieldRefused(err, "tokens") {
		t.Fatalf("a nil token reader: %v", err)
	}
	if _, err := newForgeAccess(tokens, nil, guard, "", sockets); !fieldRefused(err, "signing") {
		t.Fatalf("a nil signing agent: %v", err)
	}
	// The zero guard admits no host, so a runner holding one would refuse
	// every workspace it opened; it is refused here, at startup, instead.
	if _, err := newForgeAccess(tokens, signing, RepoHostGuard{}, "", sockets); !fieldRefused(err, "sdlc.repo_hosts") {
		t.Fatalf("an unconfigured repository guard: %v", err)
	}
	if _, err := newForgeAccess(tokens, signing, guard, "", "relative/sockets"); err == nil {
		t.Fatal("a relative sockets root was accepted")
	}
	// An empty checkout root takes the engine's default rather than producing
	// a git command with an empty operand.
	access, err := newForgeAccess(tokens, signing, guard, "  ", sockets)
	if err != nil {
		t.Fatalf("newForgeAccess: %v", err)
	}
	if access.checkoutRoot != orchestration.WorkspaceCheckoutRoot {
		t.Errorf("checkout root = %q, want the engine's default", access.checkoutRoot)
	}
}

// TestForgeAccess_RefusesAPlanNothingCouldBeBuiltFrom covers the seam's own
// input validation: a request, a project and a workspace key are each
// required, and each refusal names what is missing.
func TestForgeAccess_RefusesAPlanNothingCouldBeBuiltFrom(t *testing.T) {
	rig := newAccessRig(t)
	plan := workspacePlan{
		TenantID: testTenant,
		Project:  testOrchProject(),
		Forge:    rig.forge,
		Branch:   orchestration.WorkspaceBranchPlan{Name: "main"},
		Key:      testWorkspaceKey,
		Scope:    SigningScopeCheckout,
	}
	if _, err := rig.access.Open(context.Background(), nil, plan); !fieldRefused(err, "request") {
		t.Fatalf("a nil spawn request: %v", err)
	}
	noProject := plan
	noProject.Project = nil
	if _, err := rig.access.Open(context.Background(), &orchestration.SpawnSessionRequest{}, noProject); !fieldRefused(err, "project") {
		t.Fatalf("a nil project: %v", err)
	}
	noKey := plan
	noKey.Key = "   "
	if _, err := rig.access.Open(context.Background(), &orchestration.SpawnSessionRequest{}, noKey); !fieldRefused(err, "workspace.key") {
		t.Fatalf("a plan naming no workspace: %v", err)
	}
	// A token the provisioner could not resolve is the caller's error, not a
	// missing credential: the difference matters because one is retried and
	// the other is a configuration.
	rig.tokens.err = errors.New("the forge credential is unavailable")
	if _, err := rig.access.Open(context.Background(), &orchestration.SpawnSessionRequest{}, plan); err == nil {
		t.Fatal("a token failure was swallowed")
	}
}

// TestSigningScope_DescribesItself: the scope reaches an operator inside an
// error, and SignsCommits is the question the publisher's git configuration
// turns on.
func TestSigningScope_DescribesItself(t *testing.T) {
	for scope, want := range map[SigningScope]string{
		SigningScopeCheckout: "checkout",
		SigningScopeDelivery: "delivery",
		SigningScope(9):      "unknown",
	} {
		if got := scope.String(); got != want {
			t.Errorf("SigningScope(%d).String() = %q, want %q", scope, got, want)
		}
	}
	if SigningScopeCheckout.SignsCommits() {
		t.Error("the checkout scope claims to sign commits")
	}
	if !SigningScopeDelivery.SignsCommits() {
		t.Error("the delivery scope does not sign commits")
	}
}

// fieldRefused reports whether err is an InvalidInputError naming field.
func fieldRefused(err error, field string) bool {
	var invalid *InvalidInputError
	return errors.As(err, &invalid) && invalid.Field == field
}

// nilSessionAgent answers with neither a session nor an error, which a badly
// written implementation of the port can do and which would otherwise be a nil
// dereference at the mount.
type nilSessionAgent struct{}

func (*nilSessionAgent) StartSigningAgent(
	context.Context, *orchestration.Project, string, SigningScope,
) (*SigningSession, error) {
	return nil, nil
}

// socketlessAgent answers with a session naming no socket, which would mount
// nothing and fail inside the container.
type socketlessAgent struct{}

func (*socketlessAgent) StartSigningAgent(
	context.Context, *orchestration.Project, string, SigningScope,
) (*SigningSession, error) {
	return &SigningSession{Stop: func(context.Context) {}}, nil
}

// unsignedDeliveryAgent answers a DELIVERY scope with no commit signing key,
// which would configure git with an empty user.signingkey.
type unsignedDeliveryAgent struct{}

func (*unsignedDeliveryAgent) StartSigningAgent(
	_ context.Context, _ *orchestration.Project, socketDir string, _ SigningScope,
) (*SigningSession, error) {
	return &SigningSession{
		SocketPath: filepath.Join(socketDir, SigningSocketFile),
		Stop:       func(context.Context) {},
	}, nil
}

// TestForgeAccess_IsTheOnlyPlaceTheModuleAcquiresForgeCredentials is the
// anti-drift assertion, made against the SOURCE because that is where the
// defect lived.
//
// go-quicbaas#929 was three runners each assembling their own spawn: one
// happened to hold an ssh credential and the others did not, and no test could
// see it because every runner was internally consistent. So the property
// asserted is the one that makes the divergence unwritable — every call that
// acquires or mounts a forge credential appears exactly once in the module, in
// this file. A fourth runner, or a fifth, gets the seam or gets nothing.
func TestForgeAccess_IsTheOnlyPlaceTheModuleAcquiresForgeCredentials(t *testing.T) {
	sources, err := filepath.Glob(filepath.Join(".", "*.go"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	// Each call, and the one file it is allowed to appear in.
	owners := map[string]string{
		".StartSigningAgent(":                     "workspace.go",
		"orchestration.NewWorkspaceSpecFiles(":    "workspace.go",
		"orchestration.ApplyWorkspaceSpec(":       "workspace.go",
		"orchestration.RequireCheckoutCredential": "workspace.go",
		"orchestration.NewWorkspaceSpec(":         "workspace.go",
	}
	found := make(map[string]int, len(owners))
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		body, readErr := os.ReadFile(source)
		if readErr != nil {
			t.Fatalf("read %s: %v", source, readErr)
		}
		for call, owner := range owners {
			count := strings.Count(string(body), call)
			if count == 0 {
				continue
			}
			found[call] += count
			if filepath.Base(source) != owner {
				t.Errorf("%s calls %s; forge credentials are acquired in %s alone", source, call, owner)
			}
		}
	}
	for call := range owners {
		if found[call] == 0 {
			t.Errorf("%s appears nowhere in the module; this test is asserting nothing", call)
		}
	}
}

// TestForgeCredentials_EveryRunnerAuthenticatesThroughTheOneSeam is the
// requirement stated as a behaviour rather than as a shape: a triage run and a
// delivery run, against the same project on the same ssh remote, both obtain
// their forge credential from the same port, and the ONLY difference between
// them is the scope.
//
// Before go-quicbaas#929 this comparison was unmakeable — the delivery runner
// held a signing agent and triage held nothing — and the difference was invisible
// in either runner read on its own.
func TestForgeCredentials_EveryRunnerAuthenticatesThroughTheOneSeam(t *testing.T) {
	delivery := newRunnerRig(t)
	delivery.repoURL = sshForgeRepoURL
	if err := delivery.rebind(context.Background(), func(b *ProjectBinding) {
		b.Verification = []string{"true"}
		b.Forge.SSHHostKey = testSSHHostKey
	}); err != nil {
		t.Fatal(err)
	}
	delivery.workerWrote()
	delivery.verdictOnPublish("APPROVED", "lgtm")
	if err := delivery.runner.Run(context.Background(), delivery.job(t)); err != nil {
		t.Fatalf("delivery: %v", err)
	}

	triage := newTriageRig(t)
	triage.onSSH()
	triage.forge.putIssue(101, "Parse returns an untyped error")
	triage.answers("SDLC-TRIAGE: ready")
	if err := triage.run(); err != nil {
		t.Fatalf("triage: %v", err)
	}

	// Both cloned the same remote, and both cloned it successfully — through a
	// container that refuses a clone it could not authenticate.
	for name, clones := range map[string][]clonedRepo{
		"delivery": delivery.container.clonesMade(),
		"triage":   triage.container.clonesMade(),
	} {
		if len(clones) == 0 {
			t.Fatalf("%s cloned nothing", name)
		}
		if clones[0].URL != sshForgeRepoURL {
			t.Errorf("%s cloned %q, want %q", name, clones[0].URL, sshForgeRepoURL)
		}
	}
	// And each asked the ONE agent port for exactly the scope its container is
	// entitled to.
	if scopes := delivery.signing.startedScopes(); len(scopes) != 1 || scopes[0] != SigningScopeDelivery {
		t.Errorf("delivery scopes = %v, want [delivery]", scopes)
	}
	if scopes := triage.signing.startedScopes(); len(scopes) != 1 || scopes[0] != SigningScopeCheckout {
		t.Errorf("triage scopes = %v, want [checkout]", scopes)
	}
}

// TestForgeAccess_AMissingCredentialIsAWorkspaceFault: the refusal answers the
// one question a runner asks of a workspace, so the delivery runner parks it for
// an operator instead of spending the task's attempt budget on a configuration
// that will refuse identically every time — while the orchestration cause stays
// readable for whoever has to fix it.
func TestForgeAccess_AMissingCredentialIsAWorkspaceFault(t *testing.T) {
	rig := newAccessRig(t)
	rig.tokens.token = nil
	_, _, err := rig.open(t, testOrchProject(), SigningScopeCheckout)
	for _, sentinel := range []error{ErrRunner, ErrWorkspaceUnfit, orchestration.ErrCheckoutCredentialMissing} {
		if !errors.Is(err, sentinel) {
			t.Errorf("errors.Is(err, %v) = false: %v", sentinel, err)
		}
	}
	var missing *orchestration.CheckoutCredentialMissingError
	if !errors.As(err, &missing) || missing.Need != orchestration.CheckoutCredentialForgeToken {
		t.Fatalf("the cause does not name what was missing: %v", err)
	}
	var fault *WorkspaceCredentialError
	if !errors.As(err, &fault) || fault.ProjectID != testProject {
		t.Fatalf("the fault does not name the project: %v", err)
	}
	// And the park reason a runner composes from it reads as one.
	reason, composeErr := WorkspaceUnfitParkReason(sdlcPrompts, err)
	if composeErr != nil {
		t.Fatalf("WorkspaceUnfitParkReason: %v", composeErr)
	}
	if !strings.Contains(reason, "no credential") {
		t.Errorf("the park reason does not say what was wrong: %q", reason)
	}
}

// TestForgeAccess_RefusesARepositoryEditedOnAfterBind is go-quicbaas#770 at
// the seam every runner opens a workspace through. Bind checked the project's
// repositories once; a context repository added to the project record
// afterwards, or a remote moved off the allowlist, reaches here instead. Each
// is refused before an agent is started or a spawn names a workspace, as the
// workspace fault a runner parks for an operator, naming the repository.
func TestForgeAccess_RefusesARepositoryEditedOnAfterBind(t *testing.T) {
	for name, tc := range map[string]struct {
		edit        func(p *orchestration.Project)
		contextRepo string
	}{
		"an http context repository": {
			edit: func(p *orchestration.Project) {
				p.ContextRepos = append(p.ContextRepos, orchestration.ContextRepo{Name: "handbook", RepoURL: "http://forge.test/acme/handbook.git", Ref: "main"})
			},
			contextRepo: "handbook",
		},
		"a context repository on a foreign ssh host": {
			edit: func(p *orchestration.Project) {
				p.ContextRepos = append(p.ContextRepos, orchestration.ContextRepo{Name: "specs", RepoURL: "ssh://git@elsewhere.test/acme/specs.git", Ref: "main"})
			},
			contextRepo: "specs",
		},
		"a context repository off the allowlist": {
			edit: func(p *orchestration.Project) {
				p.ContextRepos = append(p.ContextRepos, orchestration.ContextRepo{Name: "vendor", RepoURL: "https://elsewhere.test/acme/vendor.git", Ref: "main"})
			},
			contextRepo: "vendor",
		},
		"a project remote moved off the allowlist": {
			edit: func(p *orchestration.Project) { p.Git.RepoURL = "https://elsewhere.test/acme/api.git" },
		},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newAccessRig(t)
			project := sshAccessProject()
			tc.edit(project)

			req, workspace, err := rig.open(t, project, SigningScopeDelivery)

			if workspace != nil {
				t.Fatal("a workspace was opened over a refused repository")
			}
			var refused *WorkspaceRepoRefusedError
			if !errors.As(err, &refused) || refused.ProjectID != project.ID {
				t.Fatalf("err = %v, want a *WorkspaceRepoRefusedError for the project", err)
			}
			if !errors.Is(err, ErrWorkspaceUnfit) || !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrWorkspaceUnfit (parked, not charged) wrapping the guard's ErrInvalidInput", err)
			}
			var contextErr *ContextRepoRefusedError
			if tc.contextRepo == "" {
				var urlErr *RepoURLRefusedError
				if !errors.As(err, &urlErr) || errors.As(err, &contextErr) {
					t.Fatalf("err = %v, want the project's own remote refused", err)
				}
			} else if !errors.As(err, &contextErr) || contextErr.Name != tc.contextRepo {
				t.Fatalf("err = %v, want context repository %q named", err, tc.contextRepo)
			}
			if len(rig.signing.scopes) != 0 {
				t.Fatal("a signing agent was started for a workspace that was refused")
			}
			if req.Workspace != nil {
				t.Fatal("the spawn request names a workspace that was refused")
			}
		})
	}
}

// TestForgeAccess_OpensAContextRepositoryTheGuardAdmits is the other half: a
// context repository on the project's own forge, over https beside an ssh
// project, passes the same check and reaches the spec.
func TestForgeAccess_OpensAContextRepositoryTheGuardAdmits(t *testing.T) {
	rig := newAccessRig(t)
	project := sshAccessProject()
	project.ContextRepos = []orchestration.ContextRepo{{Name: "handbook", RepoURL: "https://forge.test/acme/handbook.git", Ref: "main"}}

	_, workspace, err := rig.open(t, project, SigningScopeCheckout)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(workspace.Spec.ContextRepos) != 1 || workspace.Spec.ContextRepos[0].Name != "handbook" {
		t.Fatalf("spec context repositories = %+v, want the handbook", workspace.Spec.ContextRepos)
	}
}
