//go:build quic

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

package quic

import (
	"context"
	"errors"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// ValidatorAuthorizer is the narrow seam the QUIC PEP depends on for the unified
// platform-token authorization decision. It is satisfied by
// *platformtoken.Validator: a SINGLE call validates the token offline
// (signature / iss / aud / exp), enforces DPoP sender-constraint when the token
// carries cnf.jkt, runs the offline scp decision, and applies the per-tenant
// liveness mode. requiredPerm is the qrdb-canonical colon "resource:action".
//
// QUIC carries NO DPoP proof, so the handler always passes empty dpopProof / htm
// / htu. This is intended and secure: bearer / service tokens pass, while a
// cnf.jkt-bound token cannot prove possession over QUIC and is therefore
// rejected by the validator's sender-constraint check. The check is NOT skipped.
//
// The handler stays decoupled from how the validator is constructed (the
// in-process IAM OIDC key source lives in pkg/server) and tests inject a double.
type ValidatorAuthorizer interface {
	Authorize(ctx context.Context, token, requiredPerm, dpopProof, htm, htu string) (*platformtoken.AuthorizedContext, error)
}

// Compile-time check that the SDK validator satisfies the QUIC seam.
var _ ValidatorAuthorizer = (*platformtoken.Validator)(nil)

// validatorErrorStatus maps a platform-token authorization error to a QUIC
// transport status byte, fail-closed:
//
//   - *platformtoken.ScopeDeniedError   -> StatusPermissionDenied (authenticated
//     but the granted scopes do not admit the required permission);
//   - *platformtoken.TokenInactiveError -> StatusUnauthorized (token revoked /
//     subject disabled);
//   - *platformtoken.ConfigError        -> StatusInternalError (server
//     misconfiguration: a missing liveness checker or replay cache is an operator
//     error, not the caller's; the request is still DENIED);
//   - every other error (invalid signature / issuer / audience / expiry / DPoP /
//     missing-proof / liveness-backend failure) -> StatusUnauthorized.
//
// There is no allow path here: an unrecognised error still denies.
func validatorErrorStatus(err error) byte {
	var scopeDenied *platformtoken.ScopeDeniedError
	if errors.As(err, &scopeDenied) {
		return StatusPermissionDenied
	}
	var inactive *platformtoken.TokenInactiveError
	if errors.As(err, &inactive) {
		return StatusUnauthorized
	}
	var cfg *platformtoken.ConfigError
	if errors.As(err, &cfg) {
		return StatusInternalError
	}
	return StatusUnauthorized
}
