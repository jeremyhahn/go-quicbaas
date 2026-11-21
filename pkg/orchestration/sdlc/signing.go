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

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The signing agent — a socket in, a key never
// =============================================================================
//
// The worker's commit is signed with the project's KMS-held key, and the
// worker runs in a container. THE KEY DOES NOT TRAVEL. What travels is a unix
// socket to an agent the daemon holds, bound for the lifetime of one attempt
// and removed with it, so a compromised worker can ask for signatures while
// its container lives and holds nothing at all afterwards.
//
// The socket lives under the DAEMON'S OWN SOCKETS ROOT
// (orchestration.sockets_root), not under the project's workspace, and not in
// the OS temporary directory. A unix socket address is a fixed-size kernel
// field, and a managed workspace is
// <projects_root>/<tenant_id>/<project_id>/workspace — 88 bytes of tenant and
// project before a socket name — so a socket placed there does not fit on any
// realistic projects root. The sockets root is short, dedicated, created 0700
// at startup, and admitted by the mount policy by construction.

// SigningSocketFile is the socket's name inside the directory the agent
// creates. It is fixed by the go-xkms SDK, which is why a caller can compute
// the mount source before the session exists, and why the mount policy can
// recognise one by its shape.
const SigningSocketFile = orchestration.WorkspaceSigningAgentSocketFile

// EnsureSigningSocketRoot prepares the daemon's sockets root — refusing, AT
// STARTUP, every root a session could not bind a socket in, and sweeping the
// session directories a killed process left (orchestration.EnsureSigningSocketRoot,
// shared with the fleet node's relay root) — and then stamps it with this
// daemon's signing identifier.
//
// It is called before the runner exists precisely so a root no socket fits
// under is a refusal naming the root and the limit, not one failed job per
// delivery on a deployment that looks configured.
//
// THE ROOT CARRIES THIS DAEMON'S SIGNING IDENTIFIER, which is how a fleet
// agent proves it can mount the sockets bound here and how the dispatcher
// places a signing spawn on such an agent without a relay (go-quicbaas#981).
// The marker is the daemon's alone: a fleet node's relay root never carries
// one, because nothing another host binds there is this daemon's socket.
func EnsureSigningSocketRoot(root string) (orchestration.SigningSocketSweep, error) {
	sweep, err := orchestration.EnsureSigningSocketRoot(root)
	if err != nil {
		return sweep, err
	}
	if _, err := orchestration.EnsureSigningRootID(sweep.Root); err != nil {
		return sweep, &orchestration.SigningSocketRootUnusableError{
			Root: sweep.Root, Reason: "could not carry its signing-root marker", Cause: err,
		}
	}
	return sweep, nil
}

// SigningScope says which of the project's keys an agent serves, and it is
// the ONE place the difference between a reader's container and a deliverer's
// is stated.
//
// THERE ARE TWO KEYS, NOT ONE. A project's git identity is an ACCESS key —
// what ssh authenticates a clone, a fetch and a push with — and a COMMIT
// SIGNING key. ProjectGit.AccessKey has always known that; what did not was
// the agent, which served both or neither, so a runner that must not sign
// could not clone either. That is go-quicbaas#929: triage was handed no agent
// on purpose, and therefore no way to authenticate an ssh remote, and burned
// one job per sweep discovering it inside a container.
type SigningScope uint8

const (
	// SigningScopeCheckout serves the project's ACCESS KEY ONLY: enough for
	// git to authenticate a clone or a fetch over ssh, and no commit-signing
	// key, so nothing in the container can produce a commit signature under
	// the project's provenance identity.
	//
	// It is what a READER asks for — triage and the reviewer — and its agent
	// is stopped the moment the checkout is made, before anything is
	// prompted: the socket the container holds from then on answers nothing
	// at all. A project whose access key IS its signing key (the common
	// shape, see ProjectGit.AccessKey) narrows that window rather than
	// removing it; an operator who wants it removed configures a distinct
	// git.ssh_key, which is the field that exists for exactly this.
	SigningScopeCheckout SigningScope = iota

	// SigningScopeDelivery serves the access key AND the commit signing key:
	// the delivery runner's container clones, signs its commit and pushes it,
	// and an agent serving only one of the two would produce a signed commit
	// nothing could push.
	SigningScopeDelivery
)

// signingScopeNames render a scope for a person reading a log or an error.
var signingScopeNames = map[SigningScope]string{
	SigningScopeCheckout: "checkout",
	SigningScopeDelivery: "delivery",
}

// String implements fmt.Stringer.
func (s SigningScope) String() string {
	if name, ok := signingScopeNames[s]; ok {
		return name
	}
	return "unknown"
}

// SignsCommits reports whether an agent of this scope serves a commit-signing
// key. It is the question the publisher's git configuration turns on, asked of
// the scope rather than inferred from a session's fields.
func (s SigningScope) SignsCommits() bool { return s == SigningScopeDelivery }

// SigningSession is one running signing agent.
type SigningSession struct {
	// SocketPath is the agent's socket on the DAEMON's filesystem. It is
	// what the spawn bind-mounts into the container.
	SocketPath string

	// PublicKey is the signing key's public half in authorized_keys form.
	// git is given it by value ("user.signingkey=key::<line>"), so nothing
	// has to write a key file into the container and delete it again.
	PublicKey string

	// Helper, when set, signs through quicbaas-git-sign over the session's
	// MCP connection instead of through the socket. The socket is still
	// started and mounted; Helper only changes which one git uses.
	Helper *SigningHelper

	// Stop tears the agent down and removes the socket and its directory.
	// It is always non-nil and always safe to call more than once.
	Stop func(context.Context)
}

// SigningHelper is git's gpg.ssh.program for a session that signs over MCP.
// The helper asks quicbaas to sign with the session's key under the session's
// own principal, so every signature is authorized and audited per call and no
// key material, socket or agent is in the container.
type SigningHelper struct {
	// Program is the helper's path inside the container.
	Program string

	// Env is the helper's environment for each signing git command: the MCP
	// endpoint, the session's certificate paths and the key reference, under
	// the gitsign.Env* and orchestration.MCP*EnvVar names.
	Env map[string]string
}

// SigningAgent starts an ephemeral, KMS-backed SSH agent for one session.
//
// The module holds no keys and no KMS client: the host supplies this over the
// same git provisioner that resolves every other project credential.
type SigningAgent interface {
	// StartSigningAgent binds an agent serving the keys scope names, on a
	// socket inside socketDir.
	//
	// socketDir MUST NOT EXIST — the session creates it 0700 itself, which is
	// what makes a 0600 socket inside it safe — and its parent must be a
	// directory private to the daemon's own user. Both refusals are typed and
	// neither is worked around.
	//
	// A delivery scope answers with the commit signing key's public half on
	// the session; a checkout scope answers with none, because it serves
	// none.
	StartSigningAgent(
		ctx context.Context,
		project *orchestration.Project,
		socketDir string,
		scope SigningScope,
	) (*SigningSession, error)
}
