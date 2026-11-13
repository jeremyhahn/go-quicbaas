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
	"path/filepath"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The forge access seam — one way in, for every runner
// =============================================================================
//
// THE MODULE HAS THREE RUNNERS AND MUST HAVE ONE WAY OF REACHING THE FORGE.
// Delivery, review and triage each spawn a container that clones the project,
// and each used to assemble that spawn for itself: its own workspace spec, its
// own token lookup, its own decision about whether an ssh agent socket went in.
// They drifted, and the drift was not visible in either runner — it was visible
// only in the difference between them.
//
// go-quicbaas#929 is what that cost. The delivery runner mounted an agent
// socket because it needed one to SIGN, and got ssh authentication for its
// clone as a side effect. Triage, which signs nothing, was deliberately handed
// no socket — and therefore no client key — while the project's remote was
// ssh. It could not clone at all. Every sweep, for fifteen-minute intervals
// without end, spawned a container, ran `git clone`, collected "Permission
// denied (publickey)" and concluded. The capability had never once worked.
//
// So this is the seam, and it is the only one. A runner states WHAT it needs —
// the project, the binding's forge, the branch, the workspace key, and whether
// its container also signs commits — and gets back a workspace with the spawn
// request already naming it and carrying its spec. There is no second way to
// build one, which is what makes the divergence above unrepresentable rather
// than merely fixed.
//
// THE SEAM NAMES THE WORKSPACE; IT NEVER PICKS A HOST PATH (go-quicbaas#981).
// A path on this daemon's host means nothing to a fleet node on another one,
// which refused every mount it was handed. The spawn carries the workspace's
// name and its spec files, and whichever host executes the session provisions
// the directory under its own projects root and writes the spec beside it
// (orchestration.SessionWorkspace). The job's later sessions are pinned to that
// host by the job itself (JobRun.Spawn), so no runner has to remember to.
//
// WHAT IT REFUSES, BEFORE ANY CONTAINER STARTS, is a checkout it holds no
// credential for (orchestration.RequireCheckoutCredentials). The transport
// decides the credential and the spec names the transport, so "this spawn
// could not have cloned" is decidable on the daemon — and a job that spends a
// container to discover it is a job spent on nothing.

// forgeAccess is how a runner's container gets what it needs to reach the
// forge: the API token an https remote authenticates with, and the ssh agent
// an ssh remote does.
type forgeAccess struct {
	// tokens resolves the forge API token, as bytes, for the spawn's spec
	// directory.
	tokens ForgeTokenReader

	// signing starts the ephemeral, KMS-backed ssh agent. The keys never
	// leave the KMS and the socket lives as long as it is needed.
	signing SigningAgent

	// guard is the operator's repository allowlist, applied to the project
	// and its context repositories every time a workspace is opened.
	guard RepoHostGuard

	// checkoutRoot is where the container clones, as the CONTAINER sees it.
	checkoutRoot string

	// socketsRoot is the DAEMON's root for per-session agent sockets.
	socketsRoot string

	// sweep is what preparing socketsRoot removed: the sessions a killed
	// process left bound.
	sweep orchestration.SigningSocketSweep
}

// newForgeAccess builds the seam, refusing every collaborator a container
// could not be spawned without.
//
// PREPARING THE SOCKETS ROOT IS PART OF CONSTRUCTION, so a root no socket
// could be bound under refuses the module at startup rather than at the first
// job, and the session directories a killed process left behind are swept
// before anything binds a new one.
func newForgeAccess(tokens ForgeTokenReader, signing SigningAgent, guard RepoHostGuard, checkoutRoot, socketsRoot string) (forgeAccess, error) {
	switch {
	case !guard.configured():
		return forgeAccess{}, &InvalidInputError{
			Field:   "sdlc.repo_hosts",
			Message: "every workspace is checked against the operator's repository allowlist; a runner without one would refuse them all",
		}
	case tokens == nil:
		return forgeAccess{}, &InvalidInputError{
			Field:   "tokens",
			Message: "the container clones with the forge token",
		}
	case signing == nil:
		return forgeAccess{}, &InvalidInputError{
			Field: "signing",
			Message: "the container authenticates an ssh remote against a KMS-backed agent; " +
				"a runner without one cannot clone one",
		}
	}
	sockets := strings.TrimSpace(socketsRoot)
	// A REQUIRED SETTING, NAMED: the runner is configured with the daemon's
	// orchestration.sockets_root, and an unset or relative one is a
	// configuration to fix, not a filesystem fault.
	if sockets == "" || !filepath.IsAbs(filepath.Clean(sockets)) {
		return forgeAccess{}, &InvalidInputError{
			Field:   "orchestration.sockets_root",
			Message: "the daemon binds its per-session agent sockets under an absolute root",
		}
	}
	sweep, err := EnsureSigningSocketRoot(sockets)
	if err != nil {
		return forgeAccess{}, err
	}
	root := strings.TrimSpace(checkoutRoot)
	if root == "" {
		root = orchestration.WorkspaceCheckoutRoot
	}
	return forgeAccess{
		tokens:       tokens,
		signing:      signing,
		guard:        guard,
		checkoutRoot: root,
		socketsRoot:  filepath.Clean(sockets),
		sweep:        sweep,
	}, nil
}

// workspacePlan is what a runner asks the seam for.
type workspacePlan struct {
	// TenantID owns the project and the secret the token is read from.
	TenantID string

	// Project is the repository the container works in.
	Project *orchestration.Project

	// Forge is the binding's forge configuration: the token it names and the
	// pinned ssh host key the container verifies against.
	Forge ForgeConfig

	// Branch is the branch plan the container checks out.
	Branch orchestration.WorkspaceBranchPlan

	// Key names the workspace within the project — an attempt, a review of
	// one head, a triage checkout. The executing host provisions it as
	// <projects_root>/<tenant>/<project>/workspace/<key>, and a later session
	// naming the same key continues the same clone.
	Key string

	// Scope says whether this container also signs commits.
	Scope SigningScope
}

// containerWorkspace is one spawn's workspace: the spec both sides act on,
// the ssh agent the checkout authenticates with, and the releases that undo
// the latter. The spec directory is the executing host's, and goes with the
// session.
type containerWorkspace struct {
	// Spec is what is written — the same bytes the container reads at
	// $QUICBAAS_WORKSPACE_SPEC — so the runner drives the checkout from the
	// description the container has.
	Spec orchestration.WorkspaceSpec

	// Signing is the agent serving this container's keys, nil when it needs
	// none: an https-only checkout by a container that signs nothing.
	Signing *SigningSession

	// stopSigning tears the agent down. A READER stops its agent as soon as
	// the checkout is made, long before its session ends.
	stopSigning func(context.Context)

	// credential is the forge token bound for the container's credential
	// tier, nil when the checkout needs none. Release wipes the daemon's copy.
	credential *forgeCredential
}

// forgeCredential answers the workspace's forge credential, nil for a nil
// workspace or one that needs none.
func (w *containerWorkspace) forgeCredential() *forgeCredential {
	if w == nil {
		return nil
	}
	return w.credential
}

// StopSigning tears down the agent this workspace's checkout authenticated
// with, leaving the container's mounted socket answering nothing.
//
// A READER CALLS IT THE MOMENT ITS CHECKOUT IS MADE. Triage and the reviewer
// read untrusted text — an issue body, a candidate change — and nothing they
// do after the clone needs the forge, so the window in which a compromised
// container could ask the agent for a signature is closed before the model is
// prompted at all. The bind mount survives; the process behind it does not, so
// a connect() to it is refused.
//
// It is idempotent and safe to call from the teardown path as well.
func (w *containerWorkspace) StopSigning(ctx context.Context) {
	if w == nil || w.stopSigning == nil {
		return
	}
	stop := w.stopSigning
	w.stopSigning = nil
	w.Signing = nil
	stop(ctx)
}

// signingSession is the agent this workspace's container signs against, or nil
// when it holds none. It is nil-safe on the workspace itself so a publish path
// reached before the workspace was opened gets an unsigned configuration and a
// named refusal from git, rather than a panic.
func (w *containerWorkspace) signingSession() *SigningSession {
	if w == nil {
		return nil
	}
	return w.Signing
}

// Release undoes what this daemon holds for the workspace: the agent when it
// is still up. The spec directory is the executing host's, released with the
// session. It is idempotent.
func (w *containerWorkspace) Release(ctx context.Context) {
	if w == nil {
		return
	}
	w.StopSigning(ctx)
	w.credential.wipe()
}

// Open decides one spawn's workspace and puts it on the spawn request: the
// workspace's name, its spec files for the executing host to write, the
// environment variables naming their paths, the signing socket's mount, and
// nothing else.
//
// THE ORDER IS THE POINT. The spec is built, the credentials are resolved, and
// then the credentials are CHECKED AGAINST THE SPEC — long before req is
// spawned. A runner that holds no credential for the remote it named never
// reaches a container.
func (a forgeAccess) Open(
	ctx context.Context,
	req *orchestration.SpawnSessionRequest,
	plan workspacePlan,
) (*containerWorkspace, error) {
	if req == nil {
		return nil, &InvalidInputError{Field: "request", Message: "the workspace is applied to a spawn request"}
	}
	if plan.Project == nil {
		return nil, &InvalidInputError{Field: "project", Message: "a workspace belongs to a project"}
	}
	if strings.TrimSpace(plan.Key) == "" {
		return nil, &InvalidInputError{Field: "workspace.key", Message: "a spawn names the workspace it works in"}
	}
	// The repositories are checked HERE, not only at bind: this is the one
	// seam every runner opens a workspace through, so a context repository
	// edited onto the project after it was bound, or a host the operator has
	// since dropped from the allowlist, is refused before a container exists
	// rather than failing its clone three attempts later (go-quicbaas#770).
	if err := a.guard.CheckProject(plan.Project); err != nil {
		return nil, &WorkspaceRepoRefusedError{ProjectID: plan.Project.ID, Cause: err}
	}
	spec, err := orchestration.NewWorkspaceSpec(plan.Project, plan.Branch)
	if err != nil {
		return nil, err
	}
	spec.CheckoutRoot = a.checkoutRoot
	// The forge's pinned host key travels with the spec, so the known_hosts
	// the container verifies against is written from the binding rather than
	// learned from whatever answers at the forge's address.
	spec.SSHHostKey = plan.Forge.SSHHostKey

	token, err := a.tokens.ForgeToken(ctx, plan.TenantID, plan.Forge)
	if err != nil {
		return nil, err
	}
	workspace := &containerWorkspace{}
	if err := a.startSigning(ctx, workspace, spec, plan); err != nil {
		return nil, err
	}
	creds := orchestration.WorkspaceCredentials{ForgeToken: len(token) > 0}
	if workspace.Signing != nil {
		creds.SSHAgentSocket = workspace.Signing.SocketPath
	}
	if workspace.Signing != nil && plan.Scope.SignsCommits() {
		// The key's public half travels with the spec, so the checkout can
		// make the WORKER'S own commits signed ones and the publisher can
		// refuse any outgoing commit that is not (go-quicbaas#1048).
		spec.SigningKey = workspace.Signing.PublicKey
		// The project's trusted signing history travels with it, so a branch
		// carrying commits the key signed before its rotation still verifies.
		spec.RetiredSigningKeys = plan.Project.Git.Signing.Retired
	}
	if err := orchestration.RequireCheckoutCredentials(spec, creds); err != nil {
		workspace.Release(context.WithoutCancel(ctx))
		// WRAPPED AS ErrWorkspaceUnfit, because that is the one question a
		// runner asks of a workspace and this is one more way of being unfit:
		// provable on the daemon, before a container existed. The delivery
		// runner parks it for an operator rather than burning the task's
		// attempt budget on a configuration that will refuse identically every
		// time.
		return nil, &WorkspaceCredentialError{ProjectID: plan.Project.ID, Cause: err}
	}
	// The spec is DECIDED here and WRITTEN by the host that executes the
	// session, beside the workspace it provisions there: this daemon holds the
	// token and the binding, that host holds the directory its container
	// mounts.
	files, err := orchestration.NewWorkspaceSpecFiles(spec, len(token) > 0)
	if err != nil {
		workspace.Release(context.WithoutCancel(ctx))
		return nil, err
	}
	// The token itself stays HERE, on the daemon, until the container exists:
	// the checkout delivers it to the container's tmpfs credential tier, so
	// no host writes it to disk and no spawn surface carries it
	// (go-quicbaas#899).
	workspace.credential = newForgeCredential(token)
	req.Workspace = &orchestration.SessionWorkspace{
		TenantID:  plan.TenantID,
		ProjectID: plan.Project.ID,
		Key:       plan.Key,
	}
	workspace.Spec = files.Spec
	orchestration.ApplyWorkspaceSpec(req, files, creds.SSHAgentSocket)
	return workspace, nil
}

// workspaceSpawnFault classifies a spawn the named workspace refused as the
// workspace fault it is, and leaves every other spawn failure alone.
//
// A REFUSED WORKSPACE IS AN OPERATOR'S CONDITION, NOT THE WORK'S. The job's
// checkout lives on one host and the job is pinned there; when that host is
// gone, or holds the checkout no longer, no retry on another host could
// continue it and nothing the worker does would change that. So it arrives as
// ErrWorkspaceUnfit — parked for a person with no attempt charged — naming the
// host and the workspace, rather than as an attempt spent on a spawn that was
// never going to start.
//
// A SIGNING SPAWN WITH NOWHERE TO MOUNT THE SIGNING SOCKET IS THE SAME FAULT.
// Only an agent co-located with the daemon can mount it (go-quicbaas#1013), and
// when none serves the backend the condition is an operator's to repair.
func workspaceSpawnFault(projectID string, err error) error {
	if errors.Is(err, orchestration.ErrSessionWorkspace) || errors.Is(err, orchestration.ErrSigningAgentUnavailable) {
		return &WorkspaceUnavailableError{ProjectID: projectID, Cause: err}
	}
	return err
}

// startSigning binds the container's ssh agent when it needs one, and leaves
// the workspace without one when it does not.
//
// WHO NEEDS ONE: a container that signs commits always does, and a container
// whose spec names an ssh remote does because nothing else can authenticate
// one. A reader cloning https only gets no agent at all — which is the
// strongest posture available and the one the reviewer was written for.
func (a forgeAccess) startSigning(
	ctx context.Context,
	workspace *containerWorkspace,
	spec orchestration.WorkspaceSpec,
	plan workspacePlan,
) error {
	if !plan.Scope.SignsCommits() && !spec.NeedsSSHAgent() {
		return nil
	}
	if plan.Scope.SignsCommits() && !plan.Project.Git.Signing.Enabled {
		// REFUSED HERE, BEFORE THE SPAWN, because autonomous development
		// publishes under an identity and a project whose signing is off has
		// none. Answering with an unsigned commit would be a provenance claim
		// that is wrong and permanent, and discovering it at publish time
		// would cost a whole attempt's turns first.
		return &InvalidInputError{
			Field:   "project.git.signing",
			Message: "autonomous development commits are signed; a project whose signing is off has no identity to publish under",
		}
	}
	dir, err := a.socketDir()
	if err != nil {
		return err
	}
	session, err := a.signing.StartSigningAgent(ctx, plan.Project, dir, plan.Scope)
	if err != nil {
		return err
	}
	if session == nil {
		return &SigningSessionError{Scope: plan.Scope, Message: "the signing agent returned no session"}
	}
	if strings.TrimSpace(session.SocketPath) == "" {
		session.Stop(context.WithoutCancel(ctx))
		return &SigningSessionError{Scope: plan.Scope, Message: "the signing agent returned no socket to mount"}
	}
	if plan.Scope.SignsCommits() && strings.TrimSpace(session.PublicKey) == "" {
		// A delivery scope that answers with no public key would configure
		// git with an empty user.signingkey, and every commit would fail with
		// a message about the key rather than about the agent that did not
		// serve one.
		session.Stop(context.WithoutCancel(ctx))
		return &SigningSessionError{Scope: plan.Scope, Message: "the signing agent served no commit signing key"}
	}
	workspace.Signing = session
	workspace.stopSigning = session.Stop
	return nil
}

// socketDir names a fresh directory under the sockets root for one session's
// agent, refusing a path the kernel could not bind a socket at.
//
// THE SOCKET LIVES UNDER THE DAEMON'S SOCKETS ROOT, never under the project's
// workspace and never in the OS temporary directory. The workspace is
// <projects_root>/<tenant_id>/<project_id>/workspace, which spends 88 bytes of
// the kernel's 107-byte sun_path budget before a socket name is appended; the
// temporary directory is world-writable, so nothing can vouch for the pathname a
// socket is bound at inside it. The sockets root is short, private, prepared
// when this seam is built, and admitted by the mount policy by construction.
// The directory under it is fresh per session — the go-xkms session creates it
// 0700 itself and refuses one that already exists — and it goes away with the
// agent.
func (a forgeAccess) socketDir() (string, error) {
	name, err := orchestration.NewSigningSocketDirName()
	if err != nil {
		return "", &SigningSessionError{Message: err.Error()}
	}
	dir := filepath.Join(a.socketsRoot, name)
	// The root was measured at construction and every session directory is
	// the same width, so this cannot fail here — it is asserted because the
	// consequence of being wrong is an unbindable socket whose listen error
	// names nothing.
	if socket := filepath.Join(dir, SigningSocketFile); len(socket) > orchestration.MaxSigningSocketPath {
		return "", &SigningSocketPathError{Path: socket, Limit: orchestration.MaxSigningSocketPath}
	}
	return dir, nil
}
