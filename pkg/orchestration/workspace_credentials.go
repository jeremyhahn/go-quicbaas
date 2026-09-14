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

package orchestration

import (
	"errors"
	"strings"
)

// =============================================================================
// The checkout's credentials — decided by the transport, refused before the
// container
// =============================================================================
//
// A CLONE NEEDS A CREDENTIAL, AND WHICH ONE IS NOT A CHOICE. git offers a
// credential helper's answer to an http(s) remote and an ssh agent's signature
// to an ssh one, and never the other way round: a spawn holding a forge token
// and no agent can clone every https remote in its spec and not one ssh
// remote. That is a property of the URL, so it is computed from the URL here
// rather than assumed by whichever runner built the spec.
//
// IT IS CHECKED BEFORE THE CONTAINER STARTS. go-quicbaas#929: the triage
// runner's spawn carried a forge token and no ssh agent socket while the
// project's remote was ssh, so every sweep started a container, cloned,
// collected "Permission denied (publickey)" and concluded — one wasted job
// every fifteen minutes, for a fact that was decidable from the spec before
// anything was spawned. A workspace fault must cost zero containers and zero
// agent tokens (see preflight.go, go-quicbaas#914), and a credential the spawn
// does not hold is the earliest-decidable workspace fault there is.

// CheckoutCredential names what a container authenticates one repository's
// clone with.
type CheckoutCredential uint8

const (
	// CheckoutCredentialNone is an unauthenticated clone: a public remote, or
	// a transport this daemon has no credential shape for. It is what an
	// http:// remote gets, deliberately — a bearer token is never put on a
	// cleartext transport.
	CheckoutCredentialNone CheckoutCredential = iota

	// CheckoutCredentialForgeToken is the forge API token, read out of the
	// mounted token file by git's credential helper at the moment git asks
	// for it. It authenticates an https remote.
	CheckoutCredentialForgeToken

	// CheckoutCredentialSSHAgent is a signature from the ssh agent whose
	// socket the spawn mounts. It authenticates an ssh remote, and it is the
	// ONLY thing that does: no token, no known_hosts and no environment
	// variable can stand in for the client key.
	CheckoutCredentialSSHAgent
)

// checkoutCredentialNames render a credential for an operator reading an
// error. A map rather than a switch: one entry per constant, looked up.
var checkoutCredentialNames = map[CheckoutCredential]string{
	CheckoutCredentialNone:       "none",
	CheckoutCredentialForgeToken: "forge token",
	CheckoutCredentialSSHAgent:   "ssh agent",
}

// String implements fmt.Stringer.
func (c CheckoutCredential) String() string {
	if name, ok := checkoutCredentialNames[c]; ok {
		return name
	}
	return "unknown"
}

// CheckoutCredentialFor reports which credential a clone of repoURL
// authenticates with.
//
// An http:// remote answers CheckoutCredentialNone rather than "the token":
// the credential helper is configured for https only, because putting a forge
// token on a cleartext transport would leak it to anything on the path. A
// private http remote is therefore a configuration the operator has to fix at
// the forge, not one this smuggles a credential into.
func CheckoutCredentialFor(repoURL string) CheckoutCredential {
	if SSHRepoHost(repoURL) != "" {
		return CheckoutCredentialSSHAgent
	}
	if strings.HasPrefix(strings.TrimSpace(repoURL), "https://") {
		return CheckoutCredentialForgeToken
	}
	return CheckoutCredentialNone
}

// WorkspaceCredentials is what one spawn holds to authenticate its checkout:
// the facts, not the values. The token's bytes and the agent's key stay where
// they are — this says only whether each exists.
type WorkspaceCredentials struct {
	// ForgeToken is true when a forge API token was resolved for this spawn
	// and will be written beside the spec.
	ForgeToken bool

	// SSHAgentSocket is the agent socket the spawn will mount, empty when it
	// holds none.
	SSHAgentSocket string
}

// holds reports whether these credentials satisfy one requirement.
func (w WorkspaceCredentials) holds(need CheckoutCredential) bool {
	switch need {
	case CheckoutCredentialForgeToken:
		return w.ForgeToken
	case CheckoutCredentialSSHAgent:
		return strings.TrimSpace(w.SSHAgentSocket) != ""
	default:
		return true
	}
}

// NeedsSSHAgent reports whether any repository in the spec is cloned over ssh,
// and therefore whether the spawn has to mount an agent socket at all.
//
// The CONTEXT REPOSITORIES count. They are cloned by the same container with
// the same credentials, and a context repository on an ssh remote fails in
// exactly the way the project's would.
func (s WorkspaceSpec) NeedsSSHAgent() bool {
	if CheckoutCredentialFor(s.Project.RepoURL) == CheckoutCredentialSSHAgent {
		return true
	}
	for _, repo := range s.ContextRepos {
		if CheckoutCredentialFor(repo.RepoURL) == CheckoutCredentialSSHAgent {
			return true
		}
	}
	return false
}

// RequireCheckoutCredentials refuses a spec whose repositories need a
// credential this spawn does not hold, naming the repository and what is
// missing.
//
// IT IS THE PRE-SPAWN REFUSAL. Called before the container is started, it
// turns "a job that spent a container to discover it had no key" into a typed
// error an operator can act on — which is the difference between a defect that
// recurs every sweep and one that is reported once.
func RequireCheckoutCredentials(spec WorkspaceSpec, creds WorkspaceCredentials) error {
	repos := make([]WorkspaceRepoSpec, 0, len(spec.ContextRepos)+1)
	repos = append(repos, spec.Project)
	repos = append(repos, spec.ContextRepos...)
	for _, repo := range repos {
		need := CheckoutCredentialFor(repo.RepoURL)
		if creds.holds(need) {
			continue
		}
		return &CheckoutCredentialMissingError{Repo: repo.Name, RepoURL: repo.RepoURL, Need: need}
	}
	return nil
}

// ErrCheckoutCredentialMissing indicates a checkout no container could
// authenticate.
var ErrCheckoutCredentialMissing = errors.New("workspace checkout: no credential")

// CheckoutCredentialMissingError reports a repository the spawn holds no
// credential for.
type CheckoutCredentialMissingError struct {
	// Repo is the directory the repository would have been cloned into.
	Repo string

	// RepoURL is the remote that could not have been authenticated.
	RepoURL string

	// Need is the credential the remote's transport requires.
	Need CheckoutCredential
}

// Error implements the error interface.
func (e *CheckoutCredentialMissingError) Error() string {
	return ErrCheckoutCredentialMissing.Error() + ": " + e.Repo + " (" + e.RepoURL +
		") is cloned with the " + e.Need.String() + ", and this spawn holds none"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *CheckoutCredentialMissingError) Unwrap() error { return ErrCheckoutCredentialMissing }
