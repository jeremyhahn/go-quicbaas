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

// Package rest: this file is the coherent home for the REST identity
// contract every caller path shares — a qrdb-authenticated request, whether
// qrdb resolved the identity through its own mTLS chain or through the
// module's AuthenticateHTTP fallback (below) for a quicbaas platform-token
// caller qrdb's own authenticators do not understand. It replaces the
// per-request middleware chain (Authorize / AuthorizeViaValidator /
// EnforceTokenBinding / EnforceDefaultDeny) that used to live in
// authz_middleware.go and friends: that machinery only ever gated the
// legacy in-process chi router, which the unified-API-server architecture
// never starts a listener for (see pkg/server/lifecycle_rest.go's
// startRESTServer doc comment), so it was dead code on the live path. What
// actually mattered from it — identity injection and the bearer-token
// contract — lives here; AuthenticateHTTP (below) is now the ONLY place a
// platform token is verified: go-qrdb's own middleware authenticates AND
// authorizes every request before BindRoute (module_support.go) ever runs.
package rest

import (
	"context"
	"net/http"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// Context keys for the identity contract every authenticated request
// observes, sourced from the qrdb identity go-qrdb's unified-server
// middleware places in context (projectQRDBIdentity, module_support.go).
const (
	// UserIDKey is the context key for user ID.
	UserIDKey contextKey = "user_id"
	// UserRolesKey is the context key for user roles.
	UserRolesKey contextKey = "user_roles"
	// usernameKey is the context key for username.
	usernameKey contextKey = "username"
	// authorizedContextKey is the context key under which a verified
	// platformtoken.AuthorizedContext would be stored for downstream
	// handlers. projectQRDBIdentity never populates it (qrdb's own
	// authorization decision already ran); it remains for
	// AuthorizedContextFromRequest's documented nil contract and any
	// future direct injector.
	authorizedContextKey contextKey = "platform_authorized_context"
)

// injectPrincipal returns a child context carrying the validated principal
// and, when present, the verified platform AuthorizedContext. It is the
// single place identity is stamped onto the request.
func injectPrincipal(ctx context.Context, subject interfaces.Subject, authzCtx *platformtoken.AuthorizedContext) context.Context {
	ctx = context.WithValue(ctx, UserIDKey, subject.ID)
	ctx = context.WithValue(ctx, UserRolesKey, subject.Roles)
	ctx = context.WithValue(ctx, usernameKey, subject.Username)

	// Inject the full Subject so service-layer per-key authorization can access
	// the principal without re-parsing the token.
	ctx = auth.SubjectIntoContext(ctx, subject)

	// Inject the verified platform AuthorizedContext (scp -> RBAC, tenant_id ->
	// RLS) when the local platform-token fallback produced one.
	if authzCtx != nil {
		ctx = authorizedContextIntoContext(ctx, authzCtx)
	}

	// Bind the caller's tenant into the context so the service / storage layer
	// resolves row-level isolation from iam.TenantFromContext (the canonical RLS
	// source), matching the gRPC authz interceptor's contextWithAuthorizedSubject.
	// Only set the tenant key when the token/identity carries one; an empty
	// TenantID leaves single-tenant behavior unchanged.
	if subject.TenantID != "" {
		ctx = tenantctx.WithTenant(ctx, subject.TenantID)
	}
	return ctx
}

// GetUserID extracts user ID from context.
func GetUserID(ctx context.Context) string {
	if userID, ok := ctx.Value(UserIDKey).(string); ok {
		return userID
	}
	return ""
}

// GetUserRoles extracts user roles from context.
func GetUserRoles(ctx context.Context) []string {
	if roles, ok := ctx.Value(UserRolesKey).([]string); ok {
		return roles
	}
	return nil
}

// GetUsername extracts username from context.
func GetUsername(ctx context.Context) string {
	if username, ok := ctx.Value(usernameKey).(string); ok {
		return username
	}
	return ""
}

// AuthorizedContextFromRequest returns the verified platform
// AuthorizedContext a direct injector attached to the request. On the
// unified API server projectQRDBIdentity (module_support.go) never
// populates it — qrdb's own authorization decision already ran for every
// caller, platform-token or mTLS alike — so this always returns nil there;
// callers already tolerate that (it is the same nil an upstream
// mTLS-authenticated qrdb identity has always produced) and fall back to
// their other TenantID/Subject source.
func AuthorizedContextFromRequest(ctx context.Context) *platformtoken.AuthorizedContext {
	if ac, ok := ctx.Value(authorizedContextKey).(*platformtoken.AuthorizedContext); ok {
		return ac
	}
	return nil
}

// authorizedContextIntoContext returns a child context carrying the verified
// AuthorizedContext. It is unexported because only injectPrincipal sets it.
func authorizedContextIntoContext(ctx context.Context, ac *platformtoken.AuthorizedContext) context.Context {
	return context.WithValue(ctx, authorizedContextKey, ac)
}

// extractBearerToken extracts the JWT token from the Authorization header.
func extractBearerToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", &MissingAuthHeaderError{}
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", &InvalidAuthHeaderError{}
	}

	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", &EmptyTokenError{}
	}

	return token, nil
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

// ModuleTokenAuthorizer is the seam AuthenticateHTTP (below) depends on for
// authenticating a platform token: the same central seam the REST/gRPC/QUIC/
// MCP transports already share (pkg/server buildPlatformValidator). Validate
// performs offline signature / iss / aud / exp verification, DPoP
// sender-constraint (RFC 9449) for cnf.jkt-bound tokens, and the per-tenant
// liveness/revocation check, WITHOUT a scope check — the scope decision is
// qrdb's own RBAC authorizer's, via the ScopePermissions it unions from the
// returned AuthorizedContext.Scopes (auth.Identity.ScopePermissions,
// go-qrdb v0.5.6-alpha+). Authorize (the scope-checking sibling) is used
// nowhere on the unified-server REST path anymore; it stays on the
// interface because platformtoken.Validator's other callers (gRPC/QUIC/MCP
// transports) still call it directly.
type ModuleTokenAuthorizer interface {
	// Authorize verifies token and enforces requiredPerm ("resource:action").
	Authorize(ctx context.Context, token, requiredPerm, dpopProof, htm, htu string) (*platformtoken.AuthorizedContext, error)
	// Validate verifies token WITHOUT a scope check.
	Validate(ctx context.Context, token, dpopProof, htm, htu string) (*platformtoken.AuthorizedContext, error)
}

// Compile-time check that the SDK validator satisfies the module authorizer seam.
var _ ModuleTokenAuthorizer = (*platformtoken.Validator)(nil)

// ModuleAuthorizerRequiredError is returned by AuthenticateHTTP when no
// ModuleTokenAuthorizer is wired (SetModuleTokenAuthorizer was never called
// — RequireAuthz is off, or the OIDC plane is unavailable). Authentication
// fails closed rather than silently admitting every platform-token caller.
type ModuleAuthorizerRequiredError struct{}

// Error implements the error interface.
func (e *ModuleAuthorizerRequiredError) Error() string {
	return "module platform-token authorizer is not configured"
}

// AuthenticateHTTP authenticates a request with quicbaas's platform-token
// (OIDC JWT/DPoP) validator. It backs the module's qrdb ModuleAuthFallback
// (pkg/apimodule/module_http.go): go-qrdb's REST middleware runs its own
// mTLS chain first and consults this method only when it produces no
// identity for a request on one of the module's non-public routes — the
// returned identity then flows through qrdb's store-resolved role
// enrichment (finalizeIdentity) and the ONE authorizer, exactly like an
// mTLS identity.
//
// USER tokens (a subject that resolves to a qrdb user record) get correct
// RBAC treatment from the store-resolved roles qrdb's finalizeIdentity
// enriches. APP tokens (client-credential grants with no qrdb user record)
// resolve to empty store roles; ScopePermissions carries the token's
// scp-derived "resource:action" set (platformtoken.AuthorizedContext.Scopes
// — already qrdb-canonical and wildcard-aware, the subject's qrdb-resolved
// effective permissions verbatim) so qrdb's RBAC authorizer unions it in
// additively (auth.Identity.ScopePermissions) — exactly what the token was
// issued for. DPoP verification happens inside Validate: the presented
// DPoP proof and the request's method/absolute-URI are threaded through so
// a cnf.jkt-bound token is sender-constraint checked. Fails closed on any
// authentication error: no authorizer wired, a missing/malformed bearer, or
// a validator error (bad signature, expired, revoked, DPoP mismatch).
func (s *Server) AuthenticateHTTP(r *http.Request) (*qrdbsdk.Identity, error) {
	if s.moduleAuthorizer == nil {
		// Fails closed exactly like a refused bearer below, and MUST be
		// logged exactly like one: from the browser's side this is
		// indistinguishable from any other 401, but its cause is a
		// deployment-configuration gap (RequireAuthz=false, or the gate
		// never finished construction) rather than anything about the
		// token presented. Before this line the two were impossible to
		// tell apart from quicbaasd.out: every AuthenticateHTTP rejection
		// on this branch was silent, so a session that died on every
		// protected route (this one included) left no trace pointing back
		// here (go-quicbaas UI-login-refused investigation, 2026-09-25).
		err := &ModuleAuthorizerRequiredError{}
		s.logBearerRefusal(r, err)
		return nil, err
	}
	token, err := extractBearerToken(r)
	if err != nil {
		// A missing/malformed Authorization header is refused before the
		// validator ever runs, and is just as worth tracing as a validator
		// refusal: logging only the branch below made "no header" and "bad
		// signature" equally silent from the log's point of view.
		s.logBearerRefusal(r, err)
		return nil, err
	}
	dpopProof := r.Header.Get("DPoP")
	htu := requestAbsoluteURI(r)
	ac, err := s.moduleAuthorizer.Validate(r.Context(), token, dpopProof, r.Method, htu)
	if err != nil {
		// A PRESENTED bearer that is refused is logged with its route and
		// reason. The browser only sees "401"; without this line a session
		// that dies on one endpoint cannot be traced to that endpoint.
		s.logBearerRefusal(r, err)
		return nil, err
	}
	if ac.TenantID == "" {
		err := &EmptyTenantTokenError{}
		s.logBearerRefusal(r, err)
		return nil, err
	}
	// sub is the login name; the account is the verified uid claim.
	return auth.MarkTokenSubject(&qrdbsdk.Identity{
		Subject:          ac.Subject,
		TenantID:         ac.TenantID,
		ScopePermissions: ac.Scopes,
	}, ac.UserID), nil
}

// MissingAuthHeaderError is returned when Authorization header is missing.
type MissingAuthHeaderError struct{}

func (e *MissingAuthHeaderError) Error() string {
	return "authorization header is missing"
}

// InvalidAuthHeaderError is returned when Authorization header format is invalid.
type InvalidAuthHeaderError struct{}

func (e *InvalidAuthHeaderError) Error() string {
	return "authorization header format must be 'Bearer <token>'"
}

// EmptyTokenError is returned when token is empty.
type EmptyTokenError struct{}

func (e *EmptyTokenError) Error() string {
	return "token cannot be empty"
}

// EmptyTenantTokenError is returned when a validated platform token has no tenant.
type EmptyTenantTokenError struct{}

func (e *EmptyTenantTokenError) Error() string {
	return "platform token tenant_id is required"
}

// UnauthorizedError represents a 401 error.
type UnauthorizedError struct {
	Message string
}

func (e *UnauthorizedError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "unauthorized"
}

// ForbiddenError represents a 403 error.
type ForbiddenError struct {
	Reason string
}

func (e *ForbiddenError) Error() string {
	if e.Reason != "" {
		return "access denied: " + e.Reason
	}
	return "access denied"
}

// logBearerRefusal records why a presented bearer token was refused. The token
// itself is never logged.
func (s *Server) logBearerRefusal(r *http.Request, err error) {
	if s.logger == nil {
		return
	}
	s.logger.Warn("rest: bearer token refused",
		"method", r.Method,
		"path", r.URL.Path,
		"request_id", GetRequestID(r.Context()),
		"error", err.Error())
}
