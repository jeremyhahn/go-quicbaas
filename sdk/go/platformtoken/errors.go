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

package platformtoken

import "fmt"

// ConfigError indicates the validator was constructed with invalid options
// (e.g. no issuer, or neither a JWKS nor a JWKS URL).
type ConfigError struct {
	// Detail describes the misconfiguration.
	Detail string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("platformtoken: invalid configuration: %s", e.Detail)
}

// InvalidTokenError indicates the access token could not be parsed or its
// signature did not verify against the issuer JWKS.
type InvalidTokenError struct {
	// Detail describes the defect.
	Detail string
	// Cause is the wrapped underlying error, if any.
	Cause error
}

func (e *InvalidTokenError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("platformtoken: invalid token: %s: %v", e.Detail, e.Cause)
	}
	return fmt.Sprintf("platformtoken: invalid token: %s", e.Detail)
}

func (e *InvalidTokenError) Unwrap() error { return e.Cause }

// ExpiredTokenError indicates the token exp has passed.
type ExpiredTokenError struct{}

func (e *ExpiredTokenError) Error() string { return "platformtoken: token has expired" }

// InvalidIssuerError indicates the token iss does not equal the trusted issuer.
type InvalidIssuerError struct {
	// Expected is the configured trusted issuer.
	Expected string
	// Actual is the iss claim found in the token.
	Actual string
}

func (e *InvalidIssuerError) Error() string {
	return fmt.Sprintf("platformtoken: untrusted issuer: expected %q, token carried %q", e.Expected, e.Actual)
}

// InvalidAudienceError indicates the token aud does not include the service
// identifier this validator requires.
type InvalidAudienceError struct {
	// Expected is the required audience (this service's id).
	Expected string
}

func (e *InvalidAudienceError) Error() string {
	return fmt.Sprintf("platformtoken: token audience does not include %q", e.Expected)
}

// UnknownKeyIDError indicates the token kid was not found in the issuer JWKS
// (even after a refresh).
type UnknownKeyIDError struct {
	// KID is the unresolved key identifier.
	KID string
}

func (e *UnknownKeyIDError) Error() string {
	return fmt.Sprintf("platformtoken: signing key %q not found in issuer JWKS", e.KID)
}

// JWKSFetchError indicates the issuer JWKS could not be retrieved or decoded.
type JWKSFetchError struct {
	// Detail describes the failure.
	Detail string
	// Cause is the wrapped underlying error, if any.
	Cause error
}

func (e *JWKSFetchError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("platformtoken: JWKS fetch failed: %s: %v", e.Detail, e.Cause)
	}
	return fmt.Sprintf("platformtoken: JWKS fetch failed: %s", e.Detail)
}

func (e *JWKSFetchError) Unwrap() error { return e.Cause }

// NotSenderConstrainedError indicates DPoP verification was requested for a
// token that carries no cnf.jkt binding. A bearer token must not be accepted on
// a DPoP-protected path: the caller would otherwise gain proof-of-possession
// semantics it never proved.
type NotSenderConstrainedError struct{}

func (e *NotSenderConstrainedError) Error() string {
	return "platformtoken: token is not sender-constrained (missing cnf.jkt) but a DPoP proof was required"
}

// InvalidTokenTypeError indicates the access token's JOSE "typ" header is not the
// RFC 9068 access-token media type ("at+jwt" / "application/at+jwt"). It guards
// against token-type confusion (e.g. an OIDC ID token, which carries typ "JWT",
// being replayed as a bearer access token). Callers map it to the same rejection
// class as InvalidTokenError (401 / Unauthenticated).
type InvalidTokenTypeError struct {
	// Got is the typ header value found on the token (empty when absent).
	Got string
}

func (e *InvalidTokenTypeError) Error() string {
	if e.Got == "" {
		return "platformtoken: invalid token type: missing \"typ\" header (require \"at+jwt\" per RFC 9068)"
	}
	return fmt.Sprintf("platformtoken: invalid token type %q: require \"at+jwt\" per RFC 9068", e.Got)
}

// ScopeDeniedError indicates the token's scp set lacks a required scope.
type ScopeDeniedError struct {
	// Required is the scope that was demanded.
	Required string
}

func (e *ScopeDeniedError) Error() string {
	return fmt.Sprintf("platformtoken: required scope %q not granted", e.Required)
}
