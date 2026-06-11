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

//go:build rest

package rest

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// enforceMTLSBinding checks whether the bearer token contains a cnf/x5t#S256
// claim and, if so, verifies that the request's TLS peer certificate thumbprint
// matches the bound value (RFC 8705 §3.1).
//
// Tokens without a cnf.x5t#S256 claim are unbound (human-user tokens from
// WebAuthn/PIV login) and pass through without a certificate check.
//
// Returns a non-nil error when:
//   - The token carries a cnf.x5t#S256 claim but no peer cert is present, or
//   - The presented peer cert thumbprint does not match the bound claim.
func enforceMTLSBinding(r *http.Request, rawToken string) error {
	claims, err := extractJWTClaims(rawToken)
	if err != nil {
		// Cannot parse the token payload — treat as unbound and let later
		// validation stages handle it.
		return nil
	}

	cnf, ok := claims["cnf"].(map[string]interface{})
	if !ok {
		// No cnf claim — token is not mTLS-bound; skip check.
		return nil
	}

	thumbprint, ok := cnf["x5t#S256"].(string)
	if !ok || thumbprint == "" {
		// cnf present but no x5t#S256 — not an mTLS-bound token; skip check.
		return nil
	}

	// Token IS mTLS-bound: enforce the peer certificate check.
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return iam.ErrMTLSNoCertificate
	}

	return iam.VerifyMTLSBoundToken(claims, r.TLS.PeerCertificates[0])
}

// extractJWTClaims decodes the payload section of a JWT without verifying the
// signature. The token is assumed valid because ValidateToken already verified it;
// this function only needs the claims map for the cnf check.
func extractJWTClaims(rawToken string) (map[string]interface{}, error) {
	parts := strings.SplitN(rawToken, ".", 3)
	if len(parts) != 3 {
		return nil, &InvalidJWTFormatError{}
	}

	// JWT uses Base64URL encoding without padding.
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, &JWTDecodeError{Cause: err}
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, &JWTUnmarshalError{Cause: err}
	}

	return claims, nil
}

// InvalidJWTFormatError is returned when a JWT does not have three dot-separated parts.
type InvalidJWTFormatError struct{}

// Error implements the error interface.
func (e *InvalidJWTFormatError) Error() string {
	return "invalid JWT format: expected three dot-separated parts"
}

// JWTDecodeError is returned when the JWT payload cannot be base64url-decoded.
type JWTDecodeError struct {
	Cause error
}

// Error implements the error interface.
func (e *JWTDecodeError) Error() string {
	return "failed to base64url-decode JWT payload: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *JWTDecodeError) Unwrap() error {
	return e.Cause
}

// JWTUnmarshalError is returned when the JWT payload JSON cannot be unmarshaled.
type JWTUnmarshalError struct {
	Cause error
}

// Error implements the error interface.
func (e *JWTUnmarshalError) Error() string {
	return "failed to unmarshal JWT payload: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *JWTUnmarshalError) Unwrap() error {
	return e.Cause
}
