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
	"errors"
	"net/http"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// ValidatorAuthorizer is the narrow seam the REST PEP depends on for the unified
// platform-token authorization decision. It is satisfied by
// *platformtoken.Validator: a SINGLE call validates the token offline
// (signature / iss / aud / exp), enforces DPoP sender-constraint when the token
// carries cnf.jkt, runs the offline scp decision, and applies the per-tenant
// liveness mode. requiredPerm is the qrdb-canonical colon "resource:action".
//
// The middleware stays decoupled from how the validator is constructed (the
// in-process IAM OIDC key source lives in pkg/server) and tests inject a double.
type ValidatorAuthorizer interface {
	Authorize(ctx context.Context, token, requiredPerm, dpopProof, htm, htu string) (*platformtoken.AuthorizedContext, error)
}

// Compile-time check that the SDK validator satisfies the REST seam.
var _ ValidatorAuthorizer = (*platformtoken.Validator)(nil)

// AuthorizeViaValidator is the request-time authorization gate backed by the
// central platform-token Validator. It REPLACES the bespoke token-validate +
// online qrdb role-PDP path: the Validator performs signature + iss + aud + exp,
// DPoP sender-constraint (RFC 9449) for cnf.jkt-bound tokens, the offline scp ->
// permission decision, and the per-tenant liveness/revocation check in one call.
//
// It is default-deny: a non-public request whose path has no qrdb-canonical
// permission mapping is denied (there is no registered permission to admit it),
// and any validation/authorization failure fails closed with the status mapped
// by validatorErrorStatus (401 for token-validation / inactive, 403 for missing
// scope, 500 for a server misconfiguration). On allow it reconstructs the
// principal Subject from the verified AuthorizedContext and injects both for
// downstream handlers (scp -> RBAC, tenant_id -> RLS).
func (m *AuthzMiddleware) AuthorizeViaValidator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Public endpoints bypass authorization entirely (no token required).
		if isAuthzPublicEndpoint(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Sealed-window authentication bypass: while the barrier is sealed
		// the sealed-window allowlist IS the entire control surface (a
		// sealed IAM plane cannot mint or validate tokens), so the
		// allowlisted unseal-flow paths that passed the sealed gate above
		// are admitted without a token (audited, reason "sealed-window
		// allowlist"). Unsealed, this is always false and the barrier verbs
		// keep their granular seal:* / tenant_barrier:* scopes below.
		if m.sealedBypass.BypassesRESTPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		token, err := extractBearerToken(r)
		if err != nil {
			m.logUnauthorized(r, "missing or invalid authorization header")
			respondUnauthorized(w, r, "Missing or invalid authorization header")
			return
		}

		// Map the request to the qrdb-canonical permission the operation demands.
		// Under this posture an unmapped protected path is default-denied: there is
		// no registered permission to admit it.
		resource, action, ok := mapRequestToPermission(r.URL.Path, r.Method)
		if !ok {
			m.logValidatorDenied(r, &UnmappedResourceError{Path: r.URL.Path})
			respondForbidden(w, r, "Authorization denied")
			return
		}
		requiredPerm := resource + ":" + action

		ctx := r.Context()
		authzCtx, err := m.validatorAuthorizer.Authorize(
			ctx,
			token,
			requiredPerm,
			r.Header.Get("DPoP"),
			r.Method,
			requestAbsoluteURI(r),
		)
		if err != nil {
			status := validatorErrorStatus(err)
			m.logValidatorDenied(r, err)
			respondValidatorError(w, r, status, err)
			return
		}

		// Reconstruct the principal from the verified AuthorizedContext so the
		// service-layer per-key authorization (auth.SubjectFromContext) sees an
		// authenticated subject. The AuthorizedContext is the canonical
		// post-validation identity; Username carries the token name claim so
		// principal matching by username (in addition to ID) is preserved.
		subject := interfaces.Subject{
			ID:            authzCtx.Subject,
			Username:      authzCtx.Username,
			TenantID:      authzCtx.TenantID,
			Roles:         authzCtx.Roles,
			Authenticated: true,
			Attributes:    make(map[string]interface{}),
		}
		ctx = injectPrincipal(ctx, subject, authzCtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// validatorErrorStatus maps a platform-token authorization error to an HTTP
// status, fail-closed:
//
//   - *platformtoken.ScopeDeniedError  -> 403 (authenticated but not permitted);
//   - *platformtoken.TokenInactiveError -> 401 (token revoked / subject disabled);
//   - *platformtoken.ConfigError        -> 500 (server misconfiguration: a missing
//     liveness checker or replay cache is an operator error, not the caller's; the
//     request is still DENIED);
//   - every other error (invalid signature / issuer / audience / expiry / DPoP /
//     missing-proof / liveness-backend failure) -> 401.
//
// There is no allow path here: an unrecognised error still denies.
func validatorErrorStatus(err error) int {
	var scopeDenied *platformtoken.ScopeDeniedError
	if errors.As(err, &scopeDenied) {
		return http.StatusForbidden
	}
	var inactive *platformtoken.TokenInactiveError
	if errors.As(err, &inactive) {
		return http.StatusUnauthorized
	}
	var cfg *platformtoken.ConfigError
	if errors.As(err, &cfg) {
		return http.StatusInternalServerError
	}
	return http.StatusUnauthorized
}

// respondValidatorError writes the authorization failure using the same REST
// error-response shape the other gates use, choosing the body type from the
// status so the client sees a consistent 401/403/500 payload.
func respondValidatorError(w http.ResponseWriter, r *http.Request, status int, _ error) {
	switch status {
	case http.StatusForbidden:
		respondForbidden(w, r, "Authorization denied")
	case http.StatusInternalServerError:
		RespondError(w, r, http.StatusInternalServerError, &AuthzMisconfiguredError{})
	default:
		respondUnauthorized(w, r, "Invalid or expired token")
	}
}

// AuthzMisconfiguredError is returned (as a 500) when the platform-token
// validator denies because of a server-side misconfiguration (e.g. a liveness
// checker or replay cache required by the tenant's authz mode is absent). The
// request is denied fail-closed; the operator must fix the wiring.
type AuthzMisconfiguredError struct{}

func (e *AuthzMisconfiguredError) Error() string {
	return "authorization is misconfigured"
}

// UnmappedResourceError is returned by the validator gate when a protected path
// has no qrdb-canonical permission mapping. The gate is default-deny, so an
// unmapped protected surface is denied rather than admitted.
type UnmappedResourceError struct {
	Path string
}

func (e *UnmappedResourceError) Error() string {
	return "no qrdb permission mapping for path: " + e.Path
}

// logValidatorDenied logs a denial from the platform-token validator gate.
func (m *AuthzMiddleware) logValidatorDenied(r *http.Request, err error) {
	if m.logger == nil {
		return
	}
	m.logger.WithError(err).WithFields(map[string]interface{}{
		"path":       r.URL.Path,
		"method":     r.Method,
		"ip_address": getClientIP(r),
		"request_id": GetRequestID(r.Context()),
	}).Info("Authorization denied (platform-token validator)")
}
