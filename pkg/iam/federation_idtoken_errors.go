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

package iam

import (
	"fmt"
)

// IDTokenValidatorConfigError indicates the ID-token validator was built with
// an invalid configuration.
type IDTokenValidatorConfigError struct {
	Reason string
}

func (e *IDTokenValidatorConfigError) Error() string {
	return "iam: idtoken validator config invalid: " + e.Reason
}

// IDTokenInvalidError is a generic upstream ID-token validation failure.
type IDTokenInvalidError struct {
	Reason string
}

func (e *IDTokenInvalidError) Error() string {
	return "iam: idtoken invalid: " + e.Reason
}

// IDTokenSignatureError indicates the ID-token signature did not verify.
type IDTokenSignatureError struct {
	Cause error
}

func (e *IDTokenSignatureError) Error() string {
	if e.Cause != nil {
		return "iam: idtoken signature invalid: " + e.Cause.Error()
	}
	return "iam: idtoken signature invalid"
}

func (e *IDTokenSignatureError) Unwrap() error { return e.Cause }

// IDTokenIssuerError indicates the `iss` claim did not match the provider.
type IDTokenIssuerError struct{}

func (e *IDTokenIssuerError) Error() string {
	return "iam: idtoken issuer mismatch"
}

// IDTokenAudienceError indicates the `aud` claim did not contain the client ID.
type IDTokenAudienceError struct{}

func (e *IDTokenAudienceError) Error() string {
	return "iam: idtoken audience mismatch"
}

// IDTokenExpiredError indicates the ID token is expired (or fails freshness).
type IDTokenExpiredError struct{}

func (e *IDTokenExpiredError) Error() string {
	return "iam: idtoken expired"
}

// IDTokenNonceError indicates the ID-token nonce did not match the login state.
type IDTokenNonceError struct{}

func (e *IDTokenNonceError) Error() string {
	return "iam: idtoken nonce mismatch"
}

// IDTokenJWKSError indicates a failure fetching or parsing the provider JWKS.
type IDTokenJWKSError struct {
	Reason     string
	StatusCode int
	Cause      error
}

func (e *IDTokenJWKSError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("iam: idtoken jwks error: %s (status %d)", e.Reason, e.StatusCode)
	}
	if e.Cause != nil {
		return fmt.Sprintf("iam: idtoken jwks error: %s: %v", e.Reason, e.Cause)
	}
	return "iam: idtoken jwks error: " + e.Reason
}

func (e *IDTokenJWKSError) Unwrap() error { return e.Cause }

// NoGroupMatchError is returned when claim->role mappings are configured but the
// IdP token's groups/roles matched none of them. The federated login is denied
// (fail-closed).
type NoGroupMatchError struct {
	ProviderID string
}

func (e *NoGroupMatchError) Error() string {
	return "iam: federated login denied: no IdP group or role matched any configured mapping"
}

// EmailNotVerifiedError is returned when an account-linking-by-email or
// email-based JIT login is attempted but the verified IdP ID token reports
// `email_verified == false`. Linking or provisioning on an unverified address
// lets an attacker pre-register an email they do not control (account takeover),
// so the login is denied (fail-closed).
type EmailNotVerifiedError struct {
	Email string
}

func (e *EmailNotVerifiedError) Error() string {
	return "iam: federated login denied: IdP email address is not verified"
}

// MissingEmailClaimError is returned when account linking or email-based JIT is
// in play but the IdP token carries no email claim. The login is denied rather
// than silently substituting the external subject ID.
type MissingEmailClaimError struct{}

func (e *MissingEmailClaimError) Error() string {
	return "iam: federated login denied: email claim missing from IdP token"
}

// EmailDomainNotAllowedError is returned when the provider configures an
// AllowedEmailDomains allowlist and the verified IdP email's domain is not in
// it. The login is denied (fail-closed).
type EmailDomainNotAllowedError struct {
	Domain string
}

func (e *EmailDomainNotAllowedError) Error() string {
	return "iam: federated login denied: email domain is not in the provider allowlist"
}

// NoRolesResolvableError is returned when the provider has
// RequireExplicitRoleMapping set and a login resolves to no roles (neither a
// GroupRoleMapping matched nor DefaultRoles are configured). The login is denied
// so no zero-role user is provisioned or linked.
type NoRolesResolvableError struct {
	ProviderID string
}

func (e *NoRolesResolvableError) Error() string {
	return "iam: federated login denied: no roles resolvable and explicit role mapping is required"
}
