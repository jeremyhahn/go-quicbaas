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

// Package liveness adapts the in-process liveness authority (the iam OIDC
// provider's TokenLiveness) to the SDK platformtoken.LivenessChecker interface,
// so the same authority backs both RFC 7662 broker introspection and the
// transports' request-time authorization. It depends only on the SDK
// platformtoken contract: the local Source interface decouples it from pkg/iam,
// which avoids an import cycle (platformtoken imports only pkg/iam/dpop).
package liveness

import (
	"context"
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// Source is the in-process liveness authority. It is satisfied structurally by
// *iam.OIDCProvider (whose TokenLiveness has this exact signature), so this
// package needs no dependency on pkg/iam.
type Source interface {
	// TokenLiveness reports whether an already-signature-verified token is still
	// live: its jti has not been revoked and the principal it names may still
	// hold a session. userID is the issuer's immutable uint64 handle on a user
	// principal and principalType is the positive marker for a non-user one;
	// they are zero and empty on tokens minted before those claims existed,
	// which the authority resolves from the subject instead. It is
	// fail-closed: a store error yields a non-live, error result.
	//
	// The parameters are primitives rather than a shared struct so this package
	// keeps needing no dependency on pkg/iam.
	TokenLiveness(ctx context.Context, tenantID, subject string, userID uint64, principalType, jti string) (bool, error)
}

// Checker adapts an in-process Source to the SDK platformtoken.LivenessChecker
// interface so the same liveness authority backs both broker introspection and
// the transports' request-time authorization.
type Checker struct {
	src Source
}

// Compile-time assertion that Checker satisfies the SDK contract.
var _ platformtoken.LivenessChecker = (*Checker)(nil)

// NewChecker returns a Checker that routes liveness decisions to src.
func NewChecker(src Source) *Checker {
	return &Checker{src: src}
}

// CheckLiveness satisfies platformtoken.LivenessChecker. It maps the validated
// token's claims onto the in-process Source: claims.TenantID -> tenant,
// claims.Subject -> subject, claims.UserID -> the immutable user handle,
// claims.PrincipalType -> the positive service marker, claims.ID -> jti. The raw
// token is unused because the in-process authority decides from the
// already-verified claims; both the liveness verdict and any (fail-closed)
// error pass straight through. The uid claim is the decimal form of the uint64
// user id; an absent one is zero, and one that does not parse is refused with
// a UserIDClaimError rather than read as "no user".
func (c *Checker) CheckLiveness(ctx context.Context, token string, claims *platformtoken.Claims) (bool, error) {
	var userID uint64
	if claims.UserID != "" {
		parsed, err := strconv.ParseUint(claims.UserID, 10, 64)
		if err != nil {
			return false, &UserIDClaimError{Value: claims.UserID, Err: err}
		}
		userID = parsed
	}
	return c.src.TokenLiveness(ctx, claims.TenantID, claims.Subject, userID, claims.PrincipalType, claims.ID)
}

// UserIDClaimError reports a token uid claim that is not a decimal uint64.
type UserIDClaimError struct {
	Value string
	Err   error
}

func (e *UserIDClaimError) Error() string {
	return "liveness: uid claim " + strconv.Quote(e.Value) + " is not a decimal uint64: " + e.Err.Error()
}

func (e *UserIDClaimError) Unwrap() error { return e.Err }
