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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// Source is the in-process liveness authority. It is satisfied structurally by
// *iam.OIDCProvider (whose TokenLiveness has this exact signature), so this
// package needs no dependency on pkg/iam.
type Source interface {
	// TokenLiveness reports whether an already-signature-verified token is still
	// live: its jti has not been revoked and its subject is an enabled user (or a
	// service/non-user subject). It is fail-closed: a store error yields a
	// non-live, error result.
	TokenLiveness(ctx context.Context, tenantID, subject, jti string) (bool, error)
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
// claims.Subject -> subject, claims.ID -> jti. The raw token is unused because
// the in-process authority decides from the already-verified claims; both the
// liveness verdict and any (fail-closed) error pass straight through.
func (c *Checker) CheckLiveness(ctx context.Context, token string, claims *platformtoken.Claims) (bool, error) {
	return c.src.TokenLiveness(ctx, claims.TenantID, claims.Subject, claims.ID)
}
