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
	"strings"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
	"golang.org/x/crypto/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
)

// =============================================================================
// SDLCSigningAgent — the container's signature, the daemon's key custody
// =============================================================================
//
// The worker commits inside its container, and the commit is signed with the
// project's KMS-held key. THE KEY DOES NOT TRAVEL: this starts an ephemeral
// agent on the daemon, holding a signer whose private half never leaves the
// KMS, and hands back the socket the spawn mounts into the container.
//
// It is the same machinery the daemon's own commits use (GitProvisioner
// prepareSigning) with one difference the container forces: the socket is
// bound where the caller says, because a bind-mount source has to be
// somewhere the operator's deny-by-default mount policy admits, and the OS
// temporary directory is not.

// SDLCSigningAgent implements sdlc.SigningAgent over a GitProvisioner.
type SDLCSigningAgent struct {
	provisioner *GitProvisioner
}

var _ sdlc.SigningAgent = (*SDLCSigningAgent)(nil)

// NewSDLCSigningAgent builds the signing agent over the provisioner, which is
// where every project credential is already resolved from.
func NewSDLCSigningAgent(provisioner *GitProvisioner) (*SDLCSigningAgent, error) {
	if provisioner == nil {
		return nil, &sdlc.InvalidInputError{
			Field:   "provisioner",
			Message: "the module's key custody is the project provisioner",
		}
	}
	return &SDLCSigningAgent{provisioner: provisioner}, nil
}

// StartSigningAgent implements sdlc.SigningAgent.
//
// WHICH KEYS IT SERVES IS THE SCOPE'S ANSWER, NOT THIS FUNCTION'S. A project's
// git identity is two keys — the ACCESS key an ssh remote authenticates with,
// and the COMMIT SIGNING key — and they are frequently the same one
// (ProjectGit.AccessKey). What differs per caller is which of the two its
// container is entitled to:
//
//   - SigningScopeDelivery serves both. The worker commits, signs and pushes;
//     an agent serving only one of the two would produce a signed commit
//     nothing could push.
//   - SigningScopeCheckout serves the ACCESS key alone. A reader — triage, the
//     reviewer — clones over ssh and signs nothing, and before this scope
//     existed it was given no agent at all and therefore could not clone
//     (go-quicbaas#929).
//
// An https remote authenticates with the forge token instead, so the access
// key is not served for one: there is nothing for ssh to do.
func (a *SDLCSigningAgent) StartSigningAgent(
	ctx context.Context,
	project *orchestration.Project,
	socketDir string,
	scope sdlc.SigningScope,
) (*sdlc.SigningSession, error) {
	if project == nil {
		return nil, &sdlc.InvalidInputError{Field: "project", Message: "a signing agent serves a project's identity"}
	}
	if strings.TrimSpace(socketDir) == "" {
		return nil, &sdlc.InvalidInputError{
			Field:   "socket_dir",
			Message: "the socket is bound where the mount policy admits, so the caller names the directory",
		}
	}
	policy := project.Git.Signing
	if scope.SignsCommits() && !policy.Enabled {
		return nil, &sdlc.InvalidInputError{
			Field:   "project.git.signing",
			Message: "signing is disabled for this project; there is no identity to start an agent for",
		}
	}
	if a.provisioner.SSHSigner == nil {
		return nil, &GitCredentialError{
			Ref:    policy.Key.Name,
			Reason: "no signer resolver is configured; a container cannot be handed an agent that serves nothing",
		}
	}

	var signers []ssh.Signer
	publicKey := ""
	if scope.SignsCommits() {
		signer, err := a.signerFor(ctx, project, policy.Key)
		if err != nil {
			return nil, err
		}
		if err := requireSigningAlgorithm(signer, policy); err != nil {
			return nil, err
		}
		signers = append(signers, signer)
		publicKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	}
	if access := project.Git.AccessKey(); access.Set() && !isHTTPRepoURL(project.Git.RepoURL) {
		// The same key twice is one signer: a delivery scope whose access key
		// IS its signing key has already served it.
		if !scope.SignsCommits() || access != policy.Key {
			accessSigner, accessErr := a.signerFor(ctx, project, access)
			if accessErr != nil {
				return nil, accessErr
			}
			signers = append(signers, accessSigner)
		}
	}
	if len(signers) == 0 {
		// An agent serving nothing is worse than no agent: git would find a
		// socket, offer no key and fail on the remote's refusal, which names
		// neither the project nor the field an operator has to fill in.
		return nil, &GitCredentialError{
			Ref: project.Git.AccessKey().Name,
			Reason: "the project names no key this container could use: an ssh remote is authenticated with " +
				"git.ssh_key, or with git.signing.key standing in for it",
		}
	}

	session, err := xkmsssh.StartAgent(ctx, xkmsssh.AgentOptions{Signers: signers, SocketDir: socketDir})
	if err != nil {
		return nil, err
	}

	stopped := false
	return &sdlc.SigningSession{
		SocketPath: session.SockPath,
		PublicKey:  publicKey,
		Stop: func(stopCtx context.Context) {
			if stopped {
				return
			}
			stopped = true
			shutdownCtx, cancel := a.provisioner.shutdownContext(stopCtx)
			defer cancel()
			a.provisioner.reportAgentShutdown("sdlc worker signing agent", session.Stop(shutdownCtx))
		},
	}, nil
}

// signerFor resolves one of the project's KMS-held keys.
func (a *SDLCSigningAgent) signerFor(
	ctx context.Context,
	project *orchestration.Project,
	ref orchestration.KMSKeyRef,
) (ssh.Signer, error) {
	return a.provisioner.projectSigner(ctx, project.TenantID, ref)
}

// isHTTPRepoURL reports whether a remote authenticates with a bearer token
// rather than an ssh key.
func isHTTPRepoURL(url string) bool {
	return strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://")
}
