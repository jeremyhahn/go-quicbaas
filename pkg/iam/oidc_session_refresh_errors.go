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

// Session-refresh error types.
//
// The rejection errors deliberately carry a coarse reason: a client learns that
// its credential is no longer usable, not WHY, so a probe cannot distinguish an
// unknown token from a revoked or expired one.

// SessionRefreshUnavailableError reports that the token service backing session
// refresh is not wired.
type SessionRefreshUnavailableError struct{}

func (e *SessionRefreshUnavailableError) Error() string {
	return "iam: session refresh unavailable: token service not configured"
}

// SessionRefreshTokenRequiredError reports a refresh attempt with no credential.
type SessionRefreshTokenRequiredError struct{}

func (e *SessionRefreshTokenRequiredError) Error() string {
	return "iam: refresh token is required"
}

// SessionRefreshSubjectRequiredError reports an issuance attempt for no subject.
type SessionRefreshSubjectRequiredError struct{}

func (e *SessionRefreshSubjectRequiredError) Error() string {
	return "iam: session refresh issuance requires a subject"
}

// SessionRefreshRejectedError reports that a presented refresh credential is
// not usable (unknown, wrong type, revoked, expired, or un-rotatable).
type SessionRefreshRejectedError struct {
	// Reason is the server-side detail. It is logged, not returned to clients.
	Reason string
}

func (e *SessionRefreshRejectedError) Error() string {
	if e.Reason == "" {
		return "iam: refresh token rejected"
	}
	return "iam: refresh token rejected: " + e.Reason
}

// SessionRefreshDPoPRequiredError reports that a sender-constrained session was
// refreshed without a valid RFC 9449 proof.
type SessionRefreshDPoPRequiredError struct{}

func (e *SessionRefreshDPoPRequiredError) Error() string {
	return "iam: refresh of a DPoP-bound session requires a valid DPoP proof"
}

// SessionRefreshDPoPMismatchError reports a proof over the wrong key.
type SessionRefreshDPoPMismatchError struct{}

func (e *SessionRefreshDPoPMismatchError) Error() string {
	return "iam: DPoP proof key does not match the session binding"
}

// SessionRefreshEntropyError reports that the CSPRNG failed while minting a
// refresh credential.
type SessionRefreshEntropyError struct {
	Cause error
}

func (e *SessionRefreshEntropyError) Error() string {
	return "iam: failed to generate refresh token entropy: " + e.Cause.Error()
}

func (e *SessionRefreshEntropyError) Unwrap() error { return e.Cause }

// SessionIDEntropyError reports that the CSPRNG failed while minting the
// per-login session identifier (the sid claim), so no access token was issued.
//
// It is a hard failure rather than a degraded value on purpose: sid's whole job
// is to say whether two credentials belong to the same login, and there is no
// weaker identifier that answers that safely.
type SessionIDEntropyError struct {
	Cause error
}

func (e *SessionIDEntropyError) Error() string {
	return "iam: failed to generate session identifier entropy: " + e.Cause.Error()
}

func (e *SessionIDEntropyError) Unwrap() error { return e.Cause }

// TokenIDEntropyError reports that the CSPRNG failed while minting the
// identifier of an issued token (the jti claim), so no token was issued.
//
// It is a hard failure rather than a degraded value for the same reason
// SessionIDEntropyError is (#372): jti is the revocation handle this provider
// resolves logout and RevokeToken by, and an identifier derived from the clock
// is one an attacker can guess. Issuance that cannot name the token it issues
// does not happen.
type TokenIDEntropyError struct {
	Cause error
}

func (e *TokenIDEntropyError) Error() string {
	return "iam: failed to generate token identifier entropy: " + e.Cause.Error()
}

func (e *TokenIDEntropyError) Unwrap() error { return e.Cause }

// SessionRefreshStorageError reports that a refresh credential could not be
// persisted.
type SessionRefreshStorageError struct {
	Cause error
}

func (e *SessionRefreshStorageError) Error() string {
	return "iam: failed to store refresh token: " + e.Cause.Error()
}

func (e *SessionRefreshStorageError) Unwrap() error { return e.Cause }
