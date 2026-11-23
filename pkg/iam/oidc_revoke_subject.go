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

package iam

import (
	"context"
	"errors"
	"fmt"
)

// CredentialRevocationInputError reports a RevokeSubjectCredentials call that
// did not name the tenant or the subject whose credentials it should end.
type CredentialRevocationInputError struct {
	// Field is the missing input: "tenant_id" or "subject".
	Field string
}

func (e *CredentialRevocationInputError) Error() string {
	return fmt.Sprintf("iam: credential revocation requires %s", e.Field)
}

// CredentialRevocationError reports a stored credential that could not be
// listed or revoked while ending a subject's login credentials.
type CredentialRevocationError struct {
	// Op is the step that failed: "list" or "revoke".
	Op string
	// JTI names the token a failed "revoke" was for.
	JTI string
	// Cause is the storage error.
	Cause error
}

func (e *CredentialRevocationError) Error() string {
	if e.JTI != "" {
		return fmt.Sprintf("iam: credential revocation %s of token %s: %v", e.Op, e.JTI, e.Cause)
	}
	return fmt.Sprintf("iam: credential revocation %s: %v", e.Op, e.Cause)
}

func (e *CredentialRevocationError) Unwrap() error {
	return e.Cause
}

// RevokeSubjectCredentials revokes every stored access and refresh credential
// that subject holds in tenantID, which is what logging out everywhere has to
// end: a login keeps no session record, so deleting session records alone
// leaves its bearer and refresh credential usable. Each token is revoked under
// its own tenant. It returns how many tokens it revoked, and every failure
// joined: a logout that could not revoke a credential must not report success.
func (p *OIDCProvider) RevokeSubjectCredentials(ctx context.Context, tenantID, subject string) (int, error) {
	if tenantID == "" {
		return 0, &CredentialRevocationInputError{Field: "tenant_id"}
	}
	if subject == "" {
		return 0, &CredentialRevocationInputError{Field: "subject"}
	}
	if p.tokenService == nil || p.tokenService.storage == nil {
		return 0, &SessionRefreshUnavailableError{}
	}
	storage := p.tokenService.storage

	tokens, err := storage.ListTokensByUser(withTokenTenant(ctx, tenantID), subject)
	if err != nil {
		return 0, &CredentialRevocationError{Op: "list", Cause: err}
	}

	count := 0
	var errs []error
	for _, token := range tokens {
		if token == nil || token.TenantID != tenantID {
			// The same sub in another tenant is another account.
			continue
		}
		if token.Revoked {
			continue
		}
		if err := storage.RevokeToken(withTokenTenant(ctx, token.TenantID), token.JTI); err != nil {
			errs = append(errs, &CredentialRevocationError{Op: "revoke", JTI: token.JTI, Cause: err})
			continue
		}
		count++
	}
	return count, errors.Join(errs...)
}
