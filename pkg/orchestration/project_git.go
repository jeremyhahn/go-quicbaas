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

package orchestration

import "strings"

// =============================================================================
// Git workspace identity — author, signing, access key
// =============================================================================
//
// A Project's git workspace is a generic concept: any workload that commits
// from the workspace needs an identity to record and a decision about
// signing. Both are properties of the workspace, so they live on ProjectGit
// and nowhere else. Nothing here names a file: the private half of every key
// stays in the KMS.

// Signing algorithms the KMS serves for commit signing. The values are the
// KMS algorithm names (pkg/kms AlgEd25519 / AlgECDSAP256); they are spelled
// here as literals because this package is reachable from the
// dependency-light standalone agent binary, which deliberately does not
// import pkg/kms. TestSigningAlgorithmsMatchKMS in pkg/service pins the drift.
const (
	// SigningAlgorithmEd25519 is the default commit signing algorithm.
	SigningAlgorithmEd25519 = "ED25519"

	// SigningAlgorithmECDSAP256 is the signing algorithm under FIPS.
	SigningAlgorithmECDSAP256 = "ECDSA-P256"
)

// signingAlgorithms is the set of algorithms a CommitSigning may select.
var signingAlgorithms = map[string]struct{}{
	SigningAlgorithmEd25519:   {},
	SigningAlgorithmECDSAP256: {},
}

// GitAuthor is the author and committer recorded on a commit.
//
// Supplied explicitly rather than read from git's global configuration: a
// worker runs on a shared host where global config belongs to whoever set it
// up last, and a commit attributed to the wrong identity is a provenance
// failure that survives in history.
type GitAuthor struct {
	// Name is the author name.
	Name string `json:"name,omitempty" yaml:"name,omitempty"`

	// Email is the author email.
	Email string `json:"email,omitempty" yaml:"email,omitempty"`
}

// Set reports whether either half of the identity is present.
func (a GitAuthor) Set() bool {
	return strings.TrimSpace(a.Name) != "" || strings.TrimSpace(a.Email) != ""
}

// Validate reports an identity that cannot be recorded. The zero value is
// valid on a project (the workspace simply never commits); an identity that
// is half-set is not, because a commit needs both halves.
func (a GitAuthor) Validate() error {
	if !a.Set() {
		return nil
	}
	if strings.TrimSpace(a.Name) == "" {
		return &ProjectInvalidError{Field: "git.author.name", Message: "a commit must name its author"}
	}
	if strings.TrimSpace(a.Email) == "" {
		return &ProjectInvalidError{Field: "git.author.email", Message: "a commit must carry an author email"}
	}
	return nil
}

// KMSKeyRef names a key held in the KMS: the (name, scope, owner) triple the
// SSH key store resolves. Never material — the private half stays in the KMS
// and reaches git through an ephemeral agent.
type KMSKeyRef struct {
	// Name identifies the key within (Scope, Owner). Required where a ref is.
	Name string `json:"name,omitempty" yaml:"name,omitempty"`

	// Scope is the key store scope ("user", "tenant", "service"). Empty lets
	// the resolver apply its default.
	Scope string `json:"scope,omitempty" yaml:"scope,omitempty"`

	// Owner is the scope-specific owner. Empty lets the resolver apply its
	// default.
	Owner string `json:"owner,omitempty" yaml:"owner,omitempty"`
}

// Set reports whether the ref names a key.
func (r KMSKeyRef) Set() bool { return strings.TrimSpace(r.Name) != "" }

// CommitSigning decides whether and how commits from a workspace are signed.
//
// A POLICY rather than a key path, because the algorithm is an owner ruling
// (ed25519 by default, ECDSA-P256 where FIPS applies) and the key lives in the
// KMS. Nothing here names a file: a signing key on disk is the thing this
// design exists to avoid.
//
// The zero value signs NOTHING, and that is deliberate. An unsigned commit is
// visibly unsigned and can be re-signed; a commit signed by the wrong key is a
// provenance claim that is wrong and permanent. Signing is opted into.
type CommitSigning struct {
	// Enabled turns signing on. Off means commits are explicitly unsigned —
	// explicitly, because the host's own git config must never decide this.
	Enabled bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`

	// Algorithm is the KMS key algorithm: SigningAlgorithmEd25519 by
	// default, SigningAlgorithmECDSAP256 under FIPS. Empty selects ed25519.
	Algorithm string `json:"algorithm,omitempty" yaml:"algorithm,omitempty"`

	// Key identifies the KMS-held signing key. The private half never leaves
	// the KMS and never reaches disk.
	Key KMSKeyRef `json:"key,omitzero" yaml:"key,omitempty"`
}

// SigningAlgorithm returns the effective algorithm.
func (s CommitSigning) SigningAlgorithm() string {
	if s.Algorithm == "" {
		return SigningAlgorithmEd25519
	}
	return s.Algorithm
}

// Validate reports a policy that cannot be honoured.
//
// Fail-closed on an enabled policy with no key: silently falling back to
// unsigned would mean a project that ASKED for signed commits gets unsigned
// ones, and the gap is invisible until someone audits history.
func (s CommitSigning) Validate() error {
	if !s.Enabled {
		return nil
	}
	if !s.Key.Set() {
		return &ProjectInvalidError{
			Field:   "git.signing.key",
			Message: "signing is enabled but names no key; an enabled policy must never degrade to unsigned",
		}
	}
	if _, ok := signingAlgorithms[s.SigningAlgorithm()]; !ok {
		return &ProjectInvalidError{
			Field:   "git.signing.algorithm",
			Message: "unsupported signing algorithm " + s.SigningAlgorithm() + " (ed25519 default, ECDSA-P256 under FIPS)",
		}
	}
	return nil
}

// AccessKey returns the key an ssh:// RepoURL is reached with: the configured
// SSHKey, or the signing key when none is. One key per workspace, registered
// on the remote as both its signing key and its access key, is the common
// shape; a separate access key is the exception this field exists for.
func (g ProjectGit) AccessKey() KMSKeyRef {
	if g.SSHKey.Set() {
		return g.SSHKey
	}
	return g.Signing.Key
}

// Validate reports workspace git metadata that cannot be honoured.
func (g ProjectGit) Validate() error {
	if err := g.Author.Validate(); err != nil {
		return err
	}
	return g.Signing.Validate()
}
