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
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// The trusted signing history — retiring a key without orphaning its commits
// =============================================================================
//
// A project's signing key rotates; the pull requests its old key signed do
// not. The factory never rewrites a pushed branch, so a verifier that trusted
// only the current key refused every one of them for ever (go-luks2 PR #210
// and #194, after the 2026-09-24 move to KMS-generated keys). The operator
// retires the old key instead: its PUBLIC half is recorded, with the moment
// it stopped being the project's, and the publisher trusts it for commits made
// before that moment and for nothing else (orchestration.RetiredSigningKey).

// GitSigningRetirement names a KMS-held key to record as retired.
type GitSigningRetirement struct {
	// Key is the retired key. Its owner defaults to the project's tenant, as
	// every project key reference does.
	Key orchestration.KMSKeyRef

	// RetiredAt is when the key stopped being the project's signing key.
	// Zero is now. A time in the future is refused: it would trust the key
	// for commits nobody has made yet.
	RetiredAt time.Time
}

// SigningKeyRetirementError reports a retirement that could not be recorded.
type SigningKeyRetirementError struct {
	// Key names the key the operator asked to retire.
	Key string

	// Reason says why, for the operator.
	Reason string

	// Err is the resolution failure underneath, when there is one.
	Err error
}

// Error implements the error interface.
func (e *SigningKeyRetirementError) Error() string {
	msg := "project: signing key " + e.Key + " could not be retired: " + e.Reason
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap returns the resolution failure.
func (e *SigningKeyRetirementError) Unwrap() error { return e.Err }

// retireSigningKey resolves retirement's key to its public half and records it
// in project's signing history.
//
// THE KEY IS RESOLVED FROM THE KMS, NOT TYPED IN. The operator names a key the
// project's tenant holds, exactly as the current signing key is named, so the
// history can only ever hold a key the tenant actually held; a public key
// pasted into a form could be anybody's.
func (s *projectServiceImpl) retireSigningKey(ctx context.Context, project *orchestration.Project, retirement GitSigningRetirement) error {
	name := strings.TrimSpace(retirement.Key.Name)
	if name == "" {
		return &ProjectInvalidError{Field: "git_signing_retire_key_name", Message: "a retirement names the key it retires"}
	}
	if retirement.Key == project.Git.Signing.Key {
		return &ProjectInvalidError{
			Field:   "git_signing_retire_key_name",
			Message: "the project's current signing key cannot be retired; name its successor as git_signing_key_name first",
		}
	}
	now := time.Now().UTC()
	retiredAt := retirement.RetiredAt
	if retiredAt.IsZero() {
		retiredAt = now
	}
	if retiredAt.After(now) {
		return &ProjectInvalidError{
			Field:   "git_signing_retired_at",
			Message: "a retirement in the future would trust the key for commits nobody has made yet",
		}
	}
	if s.provisioner == nil || s.provisioner.git == nil {
		return retirementRefused(&SigningKeyRetirementError{Key: name, Reason: "this server resolves no project keys"})
	}
	publicKey, err := s.provisioner.git.SigningPublicKey(ctx, project.TenantID, retirement.Key)
	if err != nil {
		return retirementRefused(&SigningKeyRetirementError{Key: name, Reason: "its public half could not be read from the KMS", Err: err})
	}
	if err := project.Git.Signing.Retire(orchestration.RetiredSigningKey{
		PublicKey: publicKey, Name: name, RetiredAt: retiredAt,
	}); err != nil {
		return s.convertStoreError(err)
	}
	return nil
}

// retirementRefused presents a retirement that could not be recorded as the
// invalid request it is — the operator named a key this project cannot
// resolve — so every transport answers it as one, with the cause kept for
// errors.As.
func retirementRefused(err *SigningKeyRetirementError) error {
	return &ProjectInvalidError{Field: "git_signing_retire_key_name", Message: err.Error(), Err: err}
}

// SigningPublicKey answers the public half, in authorized_keys form, of one of
// a tenant's KMS-held keys. The private half is never touched.
func (p *GitProvisioner) SigningPublicKey(ctx context.Context, tenantID string, ref orchestration.KMSKeyRef) (string, error) {
	signer, err := p.projectSigner(ctx, tenantID, ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))), nil
}

// projectSigner resolves one of a project's KMS-held keys, defaulting the
// key's owner to the project's tenant exactly as the clone path does.
//
// The SSH key store partitions keys by the tenant bound to ctx, and the
// SDLC runner reaches here with only the orchestration tenant bound, so the
// lookup runs under the project's tenant explicitly: a key minted in the
// project's tenant is the one the project signs with.
func (p *GitProvisioner) projectSigner(ctx context.Context, tenantID string, ref orchestration.KMSKeyRef) (ssh.Signer, error) {
	if p.SSHSigner == nil {
		return nil, &GitCredentialError{Ref: ref.Name, Reason: "no signer resolver is configured"}
	}
	keyRef := sshKeyRefFrom(ref)
	if keyRef.Owner == "" {
		keyRef.Owner = tenantID
	}
	signer, err := p.SSHSigner.Signer(tenantctx.WithTenant(ctx, tenantID), keyRef)
	if err != nil {
		return nil, err
	}
	if signer == nil {
		return nil, &GitCredentialError{Ref: ref.Name, Reason: "the signer resolver returned no signer for the key"}
	}
	return signer, nil
}

// ParseGitSigningRetirement builds the retirement a transport's flat fields
// name: the key's name, scope and owner, and the retirement time as RFC 3339
// (empty is now). It answers nil when no key is named, and refuses a time
// given with no key or one that does not parse. Every transport calls this, so
// the fields mean one thing everywhere.
func ParseGitSigningRetirement(name, scope, owner, retiredAt string) (*GitSigningRetirement, error) {
	retiredAt = strings.TrimSpace(retiredAt)
	if strings.TrimSpace(name) == "" {
		if retiredAt != "" || strings.TrimSpace(scope) != "" || strings.TrimSpace(owner) != "" {
			return nil, &ProjectInvalidError{Field: "git_signing_retire_key_name", Message: "a retirement names the key it retires"}
		}
		return nil, nil
	}
	retirement := &GitSigningRetirement{Key: orchestration.KMSKeyRef{Name: name, Scope: scope, Owner: owner}}
	if retiredAt != "" {
		at, err := time.Parse(time.RFC3339, retiredAt)
		if err != nil {
			return nil, &ProjectInvalidError{Field: "git_signing_retired_at", Message: "must be an RFC 3339 time: " + err.Error(), Err: err}
		}
		retirement.RetiredAt = at
	}
	return retirement, nil
}
