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

package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// GRPCValidatorAuthorizer is the narrow seam the gRPC PEP depends on for the
// unified platform-token authorization decision. It is satisfied by
// *platformtoken.Validator: a SINGLE call validates the token offline
// (signature / iss / aud / exp), enforces DPoP sender-constraint when the token
// carries cnf.jkt, runs the offline scp decision, and applies the per-tenant
// liveness mode. requiredPerm is the qrdb-canonical colon "resource:action".
//
// This mirrors the REST ValidatorAuthorizer seam so REST, gRPC, and the
// Unix-over-gRPC transport share one authorization contract. The gRPC metadata
// path carries NO DPoP proof, so the interceptor always passes empty dpopProof /
// htm / htu: a bearer/service token (no cnf.jkt) passes, and a (rare) cnf.jkt
// sender-constrained token is correctly rejected because possession cannot be
// proven over gRPC (DPoP is a REST/human-session concern).
type GRPCValidatorAuthorizer interface {
	Authorize(ctx context.Context, token, requiredPerm, dpopProof, htm, htu string) (*platformtoken.AuthorizedContext, error)
}

// Compile-time check that the SDK validator satisfies the gRPC seam.
var _ GRPCValidatorAuthorizer = (*platformtoken.Validator)(nil)

// GRPCValidatorAuthenticator is the OPTIONAL scope-free authenticated path of
// the gRPC PEP seam, satisfied by *platformtoken.Validator via its Validate
// method: a SINGLE call validates the token offline (signature / iss / aud /
// exp), enforces DPoP sender-constraint when the token carries cnf.jkt, and
// applies the per-tenant liveness mode — WITHOUT requiring any scp
// permission. It exists for the qrdb IdentityGated verbs (the barrier
// share-claim methods): qrdb's own transport maps gate those with the
// permission.IdentityGated sentinel — any AUTHENTICATED identity may call,
// and the HANDLER enforces the real credential (registered-officer lookup,
// mTLS SPKI thumbprint match, nonce signature verification). Admitting them
// through Authorize would demand a registered scope qrdb deliberately does
// not define; admitting them public would drop the authenticated-identity
// requirement. Validate is the exact parity path.
//
// The seam stays fail-closed: an authorizer that does not implement this
// interface denies the IdentityGated verbs outright.
type GRPCValidatorAuthenticator interface {
	Validate(ctx context.Context, token, dpopProof, htm, htu string) (*platformtoken.AuthorizedContext, error)
}

// Compile-time check that the SDK validator satisfies the authenticated seam.
var _ GRPCValidatorAuthenticator = (*platformtoken.Validator)(nil)

// validatorGRPCStatus maps a platform-token authorization error to a gRPC status
// error, fail-closed:
//
//   - *platformtoken.ScopeDeniedError   -> PermissionDenied (authenticated but
//     not permitted: the token's scp set lacks the required permission);
//   - *platformtoken.TokenInactiveError -> Unauthenticated (token revoked /
//     subject disabled);
//   - *platformtoken.ConfigError        -> Internal (server misconfiguration: a
//     missing liveness checker or replay cache is an operator error, not the
//     caller's; the request is still DENIED);
//   - every other error (invalid signature / issuer / audience / expiry / DPoP /
//     missing-proof / liveness-backend failure) -> Unauthenticated.
//
// There is no allow path here: an unrecognised error still denies. The status
// message is deliberately coarse so a caller cannot probe internal policy state.
func validatorGRPCStatus(err error) error {
	var scopeDenied *platformtoken.ScopeDeniedError
	if errors.As(err, &scopeDenied) {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	var inactive *platformtoken.TokenInactiveError
	if errors.As(err, &inactive) {
		return status.Error(codes.Unauthenticated, "token inactive")
	}
	var cfg *platformtoken.ConfigError
	if errors.As(err, &cfg) {
		return status.Error(codes.Internal, "authorization is misconfigured")
	}
	return status.Error(codes.Unauthenticated, "invalid or expired token")
}
