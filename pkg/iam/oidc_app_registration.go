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
	"encoding/base64"

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// =============================================================================
// RFC 7592 registration access token
//
// The registration access token is the ONLY credential that authorizes the
// OAuth 2.0 Dynamic Client Registration MANAGEMENT endpoints
// (GET/PUT/DELETE /oauth2/register/{client_id}). Its security properties:
//
//   - It is minted exactly once, at Dynamic Client Registration time, and is
//     returned to the registrant in the registration response only. There is no
//     recovery path: losing it means losing management access to that client.
//   - It is an opaque 256-bit random secret, NOT a JWT. It authorizes one
//     client_id, carries no scope, and is not accepted anywhere else.
//   - It is stored hashed by value (Application.RegistrationTokenHash, the same
//     HashTokenValue SHA-256 derivation the refresh-token store uses), so the
//     database never holds a usable credential and the presented secret is the
//     lookup handle.
//   - Verification is a constant-time comparison of the presented token's hash
//     against the hash stored FOR THAT client_id. A token bound to client A can
//     therefore never authorize client B, and — because a client record is
//     owned by exactly one tenant — never crosses a tenant boundary either.
//   - It fails CLOSED: an absent, malformed, unknown or mismatched token, and a
//     client that has no registration token at all (i.e. was not created
//     through DCR), are all refused with the same opaque error so the caller
//     cannot probe which client ids exist.
// =============================================================================

// registrationAccessTokenEntropyBytes is the size of the random
// registration-access-token secret. 256 bits leaves no practical margin for
// guessing a value that is accepted without any other credential.
const registrationAccessTokenEntropyBytes = 32

// registrationAccessTokenPrefix marks a value as a registration access token in
// logs and operator tooling. It carries no security meaning.
const registrationAccessTokenPrefix = "rat_"

// RegistrationAccessDeniedError reports that an RFC 7592 client-configuration
// request could not be authorized. Reason is for the server-side audit trail
// only — the transport must answer every instance with the same opaque 401 so
// an attacker cannot distinguish "unknown client" from "wrong token".
type RegistrationAccessDeniedError struct {
	// ClientID is the client the request addressed.
	ClientID string
	// Reason describes why authorization failed (audit detail).
	Reason string
}

// Error implements the error interface.
func (e *RegistrationAccessDeniedError) Error() string {
	return "iam: registration access denied for client " + e.ClientID + ": " + e.Reason
}

// generateRegistrationAccessToken generates an RFC 7592 registration access
// token and its storage hash. Returns (plaintext, hash, error); the plaintext
// is the only copy the caller will ever see.
func generateRegistrationAccessToken() (string, string, error) {
	b, err := xkms.Rand(registrationAccessTokenEntropyBytes)
	if err != nil {
		return "", "", err
	}
	plaintext := registrationAccessTokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	return plaintext, HashTokenValue(plaintext), nil
}

// RegisterDCRApp registers an OAuth2 application through RFC 7591 Dynamic
// Client Registration and issues the RFC 7592 registration access token that
// governs the client-configuration endpoints.
//
// It returns the application carrying the plaintext client secret and, as the
// second value, the plaintext registration access token. Both are visible only
// here: the store keeps a bcrypt hash of the secret and a SHA-256 hash of the
// token.
func (m *AppManager) RegisterDCRApp(ctx context.Context, tenantID string, opts RegisterAppOptions) (*Application, string, error) {
	return m.registerApp(ctx, tenantID, opts, true)
}

// AuthorizeRegistrationAccess resolves the application addressed by an RFC 7592
// client-configuration request and authorizes it against the presented
// registration access token. It is the ONLY gate on those endpoints, so it is
// fail-closed by construction: every failure mode returns
// *RegistrationAccessDeniedError and no application.
//
// tenantID, when non-empty, additionally requires the client to live in that
// tenant — defence in depth for callers that already know the tenant the
// request authenticated into. An empty tenantID relies on the token binding
// alone, which is itself client- (and therefore tenant-) specific.
//
// The returned application is redacted: it never carries the client secret or
// the registration token hash.
func (m *AppManager) AuthorizeRegistrationAccess(ctx context.Context, tenantID, clientID, token string) (*Application, error) {
	if clientID == "" {
		return nil, &RegistrationAccessDeniedError{Reason: "empty client id"}
	}
	if token == "" {
		return nil, &RegistrationAccessDeniedError{ClientID: clientID, Reason: "empty registration access token"}
	}

	app, err := m.resolveApp(ctx, tenantID, clientID)
	if err != nil {
		return nil, &RegistrationAccessDeniedError{ClientID: clientID, Reason: "client not found in scope"}
	}

	// A client with no stored registration token was not created through DCR
	// and has no RFC 7592 management credential. Refuse rather than fall back
	// to any weaker check.
	if app.RegistrationTokenHash == "" {
		return nil, &RegistrationAccessDeniedError{ClientID: clientID, Reason: "client has no registration access token"}
	}

	if !constantTimeCompare(app.RegistrationTokenHash, HashTokenValue(token)) {
		return nil, &RegistrationAccessDeniedError{ClientID: clientID, Reason: "registration access token mismatch"}
	}

	return redactAppSecrets(app), nil
}
