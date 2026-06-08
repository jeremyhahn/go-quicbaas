//go:build rest

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

package rest

import (
	"context"
	"net/http"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// PlatformTokenResult is the richer validation result a DPoPTokenValidator
// returns. It carries the platform access-token claims the global authz
// middleware needs to enforce sender-constrained binding (cnf.jkt) and to build
// the scp -> RBAC / tenant_id -> RLS AuthorizedContext, none of which fit on the
// transport-agnostic auth.TokenValidationResult.
//
// It deliberately mirrors the subset of platformtoken.Claims the broker enforces
// inbound; the validator that produces it (the IAM OIDC provider adapter) has
// already verified the signature, issuer/audience, revocation, and — when the
// token is bound — the DPoP proof.
type PlatformTokenResult struct {
	// Subject is the token sub (stable user/service id).
	Subject string
	// Username is the human-facing name claim, when present.
	Username string
	// TenantID is the tenant the request operates within (-> RLS scope).
	TenantID string
	// Roles is the supplemental RBAC roles claim.
	Roles []string
	// Scopes is the authoritative granted scope set (scp; falls back to scope).
	Scopes []string
	// JKT is the cnf.jkt DPoP binding, empty for an unbound (plain bearer) token.
	JKT string
}

// DPoPTokenValidator is the sender-constraint-aware extension of TokenValidator.
// A validator that implements it performs DPoP proof-of-possession enforcement
// (RFC 9449) for cnf.jkt-bound tokens in addition to the signature/revocation
// checks ValidateToken performs, and returns the platform claims needed to build
// the AuthorizedContext.
//
// The middleware type-asserts its configured TokenValidator to this interface:
// when satisfied (the IAM OIDC adapter), inbound DPoP enforcement and scp/tenant
// authorization are active; when not (e.g. a non-iam build), the middleware
// falls back to plain bearer validation, preserving prior behavior.
type DPoPTokenValidator interface {
	// ValidateTokenWithDPoP validates the access token and, when it carries a
	// cnf.jkt binding, enforces the presented DPoP proof against it. htm is the
	// request method; htu is the absolute request URI. A bound token presented
	// without a valid proof MUST return a non-nil error so the middleware fails
	// closed with 401.
	ValidateTokenWithDPoP(ctx context.Context, token, dpopProof, htm, htu string) (*PlatformTokenResult, error)
}

// authorizedContextKey is the context key under which the verified
// platformtoken.AuthorizedContext is stored for downstream handlers to consume
// for scp -> RBAC (RequireScope/HasRole) and tenant_id -> RLS enforcement.
const authorizedContextKey contextKey = "platform_authorized_context"

// AuthorizedContextFromRequest returns the verified platform AuthorizedContext
// the authz middleware attached to the request, or nil when the request was not
// authorized through the DPoP-aware path (e.g. a non-iam build or a public
// endpoint). Protected handlers call this and then RequireScope / HasRole.
func AuthorizedContextFromRequest(ctx context.Context) *platformtoken.AuthorizedContext {
	if ac, ok := ctx.Value(authorizedContextKey).(*platformtoken.AuthorizedContext); ok {
		return ac
	}
	return nil
}

// authorizedContextIntoContext returns a child context carrying the verified
// AuthorizedContext. It is unexported because only the authz middleware sets it.
func authorizedContextIntoContext(ctx context.Context, ac *platformtoken.AuthorizedContext) context.Context {
	return context.WithValue(ctx, authorizedContextKey, ac)
}

// toAuthorizedContext projects a verified PlatformTokenResult onto the canonical
// platformtoken.AuthorizedContext. It reuses the SDK authz type rather than
// inventing a parallel one, so handlers enforce scopes/roles with the same
// helpers downstream services use.
func (r *PlatformTokenResult) toAuthorizedContext() *platformtoken.AuthorizedContext {
	ac := &platformtoken.AuthorizedContext{
		Subject:  r.Subject,
		TenantID: r.TenantID,
		Scopes:   r.Scopes,
		Roles:    r.Roles,
	}
	if r.JKT != "" {
		ac.Confirmation = &platformtoken.Confirmation{Jkt: r.JKT}
	}
	return ac
}

// requestAbsoluteURI reconstructs the externally-visible absolute request URI
// (scheme://host/path) used as the RFC 9449 DPoP htu. It honors the reverse-proxy
// forwarding headers so the htu matches the URL the client dialed when a
// TLS-terminating proxy fronts the service. Query and fragment are not included;
// the DPoP verifier canonicalizes both sides to scheme+host+path regardless.
func requestAbsoluteURI(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	if fwd := r.Header.Get("X-Forwarded-Proto"); fwd != "" {
		scheme = fwd
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = fwd
	}
	return scheme + "://" + host + r.URL.Path
}
