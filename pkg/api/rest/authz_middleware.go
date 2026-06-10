//go:build rest

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

// Package rest provides authorization middleware for REST API using unified authorization.
package rest

import (
	"context"
	"net/http"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// TokenValidator defines the interface for JWT token validation.
// This allows for dependency injection and easier testing.
type TokenValidator interface {
	ValidateToken(ctx context.Context, token string) (*auth.TokenValidationResult, error)
}

// AuthzMiddlewareConfig holds configuration for authorization middleware.
type AuthzMiddlewareConfig struct {
	// Logger for logging authorization decisions.
	Logger *logger.Logger
	// TokenValidator validates JWT tokens.
	TokenValidator TokenValidator
	// AuthzService performs authorization checks.
	AuthzService interfaces.AuthorizationService
	// ValidatorAuthorizer, when set, routes the AuthorizeViaValidator gate
	// through the central platform-token Validator: one call does offline
	// signature/iss/aud/exp + DPoP + scp + per-tenant liveness. It is installed
	// only under RequireAuthz=true and is the reference (REST) enforcement path.
	ValidatorAuthorizer ValidatorAuthorizer
}

// AuthzMiddleware provides unified authorization middleware for REST API.
type AuthzMiddleware struct {
	logger              *logger.Logger
	tokenValidator      TokenValidator
	authzService        interfaces.AuthorizationService
	validatorAuthorizer ValidatorAuthorizer
}

// NewAuthzMiddleware creates a new authorization middleware with the given configuration.
func NewAuthzMiddleware(cfg *AuthzMiddlewareConfig) *AuthzMiddleware {
	return &AuthzMiddleware{
		logger:              cfg.Logger,
		tokenValidator:      cfg.TokenValidator,
		authzService:        cfg.AuthzService,
		validatorAuthorizer: cfg.ValidatorAuthorizer,
	}
}

// validate verifies the bearer token and returns the authorization Subject plus,
// when the configured validator is DPoP-aware, the verified platform
// AuthorizedContext (scp -> RBAC, tenant_id -> RLS).
//
// DPoP-aware path (IAM OIDC adapter): a cnf.jkt-bound token requires a valid
// DPoP proof over the request method + absolute URI (RFC 9449); a bearer
// presentation of a bound token fails closed here. The returned AuthorizedContext
// is non-nil so handlers can enforce scopes/roles.
//
// Plain path (validator without DPoP support, e.g. non-iam builds): the token is
// validated for signature/expiry/revocation only and the AuthorizedContext is
// nil, preserving prior behavior. The RFC 8705 mTLS binding check still runs on
// this path for cnf.x5t#S256-bound tokens.
func (m *AuthzMiddleware) validate(ctx context.Context, r *http.Request, token string) (interfaces.Subject, *platformtoken.AuthorizedContext, error) {
	if dpopValidator, ok := m.tokenValidator.(DPoPTokenValidator); ok {
		result, err := dpopValidator.ValidateTokenWithDPoP(ctx, token, r.Header.Get("DPoP"), r.Method, requestAbsoluteURI(r))
		if err != nil {
			return interfaces.Subject{}, nil, err
		}
		authzCtx := result.toAuthorizedContext()
		subject := interfaces.Subject{
			ID:            result.Subject,
			Username:      result.Username,
			TenantID:      result.TenantID,
			Roles:         result.Roles,
			Authenticated: true,
			Attributes:    make(map[string]interface{}),
		}
		return subject, authzCtx, nil
	}

	result, err := m.tokenValidator.ValidateToken(ctx, token)
	if err != nil {
		return interfaces.Subject{}, nil, err
	}
	if result == nil || !result.Valid {
		return interfaces.Subject{}, nil, &InvalidTokenResultError{}
	}
	if result.IsRevoked {
		return interfaces.Subject{}, nil, &RevokedTokenError{}
	}

	// Enforce RFC 8705 §3.1 mTLS proof-of-possession for cnf-bound tokens.
	// enforceMTLSBinding is a no-op when the iam build tag is absent.
	if err := enforceMTLSBinding(r, token); err != nil {
		return interfaces.Subject{}, nil, err
	}

	subject := interfaces.Subject{
		ID:            result.UserID,
		Username:      result.Username,
		TenantID:      result.TenantID,
		Roles:         result.Roles,
		Authenticated: true,
		Attributes:    make(map[string]interface{}),
	}
	return subject, nil, nil
}

// InvalidTokenResultError indicates the validator returned a nil-error result
// that was nil or not valid (a contract violation surfaced as a 401).
type InvalidTokenResultError struct{}

func (e *InvalidTokenResultError) Error() string { return "token is invalid or expired" }

// RevokedTokenError indicates the presented token has been revoked.
type RevokedTokenError struct{}

func (e *RevokedTokenError) Error() string { return "token has been revoked" }

// Authorize is the middleware function that enforces authorization on requests.
// It extracts the JWT token, validates it, extracts resource and action from the request,
// and calls the AuthzService to make an access control decision.
func (m *AuthzMiddleware) Authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip authorization for public endpoints
		if isAuthzPublicEndpoint(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Extract token from Authorization header
		token, err := extractBearerToken(r)
		if err != nil {
			m.logUnauthorized(r, "missing or invalid authorization header")
			respondUnauthorized(w, r, "Missing or invalid authorization header")
			return
		}

		// Validate the token. When the validator is DPoP-aware (the IAM OIDC
		// adapter), route through the sender-constraint-enforcing path: a
		// cnf.jkt-bound token requires a valid DPoP proof (RFC 9449) over the
		// request method + absolute URI, verified against the provider's replay
		// cache. A plain bearer (no cnf.jkt) validates exactly as before. When
		// the validator is not DPoP-aware (non-iam builds), fall back to plain
		// bearer validation, preserving prior behavior.
		ctx := r.Context()
		subject, authzCtx, err := m.validate(ctx, r, token)
		if err != nil {
			m.logUnauthorized(r, "token validation failed: "+err.Error())
			respondUnauthorized(w, r, "Invalid or expired token")
			return
		}

		// Extract resource from URL path
		resource := extractAuthzResource(r.URL.Path)

		// Map HTTP method to action
		action := mapHTTPMethodToAction(r.Method)

		// Build authorization request with context
		authzReq := &interfaces.AuthorizationRequest{
			Subject:  subject,
			Resource: resource,
			Action:   action,
			Context:  buildAuthzContext(r),
		}

		// Perform authorization check
		decision, err := m.authzService.Authorize(ctx, authzReq)
		if err != nil {
			m.logAuthzError(r, authzReq, err)
			respondForbidden(w, r, "Authorization check failed")
			return
		}

		if !decision.Allowed {
			m.logDenied(r, authzReq, decision)
			respondForbidden(w, r, decision.Reason)
			return
		}

		// Log successful authorization
		m.logAllowed(r, authzReq, decision)

		// Inject the verified principal + platform AuthorizedContext for
		// downstream handlers (user info, RLS tenant, scp -> RBAC).
		ctx = injectPrincipal(ctx, subject, authzCtx)

		// Continue with the request
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// EnforceTokenBinding is the proof-of-possession middleware: it validates the
// bearer token and, when the token is DPoP sender-constrained (cnf.jkt), enforces
// the presented DPoP proof (RFC 9449) fail-closed before the request reaches a
// handler. A bearer presentation of a bound token is rejected with 401; a plain
// bearer (no cnf) passes. It then injects the verified principal + the platform
// AuthorizedContext (scp -> RBAC, tenant_id -> RLS) so protected handlers can
// enforce RequireScope / HasRole and set the RLS tenant.
//
// Unlike Authorize, it does NOT run the RBAC authorization decision: it gates on
// token binding and identity only, leaving per-resource RBAC enforcement to the
// handler layer (via the AuthorizedContext) until cluster-wide role propagation
// is validated. This is the additive inbound enforcement the broker installs
// today; it keeps unbound-bearer callers working while closing the DPoP replay
// gap on sender-constrained tokens.
func (m *AuthzMiddleware) EnforceTokenBinding(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAuthzPublicEndpoint(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		token, err := extractBearerToken(r)
		if err != nil {
			// No bearer token presented. EnforceTokenBinding enforces
			// sender-constraint BINDING on tokens that ARE presented (a
			// DPoP cnf-bound token cannot be stripped of its proof and
			// replayed as a plain bearer); it is NOT an authentication gate.
			// Token-less requests pass through unchanged — the data plane's
			// authentication posture (anonymous vs required) is decided by the
			// separate Authorize gate, not here. This preserves the
			// pre-existing behavior where the data plane accepted
			// unauthenticated requests.
			next.ServeHTTP(w, r)
			return
		}

		ctx := r.Context()
		subject, authzCtx, err := m.validate(ctx, r, token)
		if err != nil {
			m.logUnauthorized(r, "token binding check failed: "+err.Error())
			respondUnauthorized(w, r, "invalid_token")
			return
		}

		ctx = injectPrincipal(ctx, subject, authzCtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// injectPrincipal returns a child context carrying the validated principal and,
// when present, the verified platform AuthorizedContext. It is the single place
// the authz middlewares stamp identity onto the request so Authorize and
// EnforceTokenBinding stay consistent.
func injectPrincipal(ctx context.Context, subject interfaces.Subject, authzCtx *platformtoken.AuthorizedContext) context.Context {
	ctx = context.WithValue(ctx, UserIDKey, subject.ID)
	ctx = context.WithValue(ctx, UserRolesKey, subject.Roles)
	ctx = context.WithValue(ctx, usernameKey, subject.Username)

	// Inject the full Subject so service-layer per-key authorization can access
	// the principal without re-parsing the token.
	ctx = auth.SubjectIntoContext(ctx, subject)

	// Inject the verified platform AuthorizedContext (scp -> RBAC, tenant_id ->
	// RLS) when the DPoP-aware path produced one.
	if authzCtx != nil {
		ctx = authorizedContextIntoContext(ctx, authzCtx)
	}
	return ctx
}

// Context keys for authorization middleware.
const (
	// UserIDKey is the context key for user ID.
	UserIDKey contextKey = "user_id"
	// UserRolesKey is the context key for user roles.
	UserRolesKey contextKey = "user_roles"
	// usernameKey is the context key for username.
	usernameKey contextKey = "username"
)

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

// publicEndpoints defines endpoints that bypass authorization.
var publicEndpoints = []string{
	// Token management endpoints
	"/api/v1/auth/refresh",
	// Hardware-attested login endpoints (WebAuthn + PIV) — tokens are not yet available here.
	"/api/v1/auth/webauthn",
	"/api/v1/auth/login/piv",
	// Unified login ceremony (mtls / fido2 / piv) — the ceremony itself
	// authenticates; no bearer is available yet.
	"/api/v1/auth/login/begin",
	"/api/v1/auth/login/finish",
	// Public tenant listing for the login UI tenant selector — no token available yet.
	"/api/v1/iam/tenants/public",
	// Health and metrics endpoints
	"/api/v1/health",
	"/api/v1/health/liveness",
	"/api/v1/health/readiness",
	// Version (read-only build info; consumed by the GUI dashboard).
	"/api/v1/version",
	"/metrics",
}

// isAuthzPublicEndpoint checks if the endpoint doesn't require authorization.
func isAuthzPublicEndpoint(path string) bool {
	for _, endpoint := range publicEndpoints {
		if path == endpoint || strings.HasPrefix(path, endpoint+"/") {
			return true
		}
	}
	return false
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

// resourcePathMapping maps URL path prefixes to resource names.
var resourcePathMapping = map[string]string{
	"/api/v1/keys":         interfaces.ResourceKeys,
	"/api/v1/secrets":      interfaces.ResourceSecrets,
	"/api/v1/kms":          interfaces.ResourceKMS,
	"/api/v1/crypto":       interfaces.ResourceCrypto,
	"/api/v1/seal":         interfaces.ResourceSeal,
	"/api/v1/cluster":      interfaces.ResourceCluster,
	"/api/v1/users":        interfaces.ResourceUsers,
	"/api/v1/roles":        interfaces.ResourceRoles,
	"/api/v1/timeseries":   interfaces.ResourceTimeseries,
	"/api/v1/vector":       interfaces.ResourceVector,
	"/api/v1/watch":        interfaces.ResourceWatch,
	"/api/v1/transactions": interfaces.ResourceTransactions,
	"/api/v1/backups":      interfaces.ResourceBackups,
	"/api/v1/parameters":   interfaces.ResourceParameters,
}

// extractAuthzResource extracts the resource name from the URL path.
func extractAuthzResource(path string) string {
	// Normalize path
	path = strings.TrimSuffix(path, "/")

	// Check for exact match or prefix match
	for prefix, resource := range resourcePathMapping {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return resource
		}
	}

	// Fallback: extract from path segments
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 3 && parts[0] == "api" && parts[1] == "v1" {
		return parts[2]
	}

	return "unknown"
}

// mapHTTPMethodToAction maps HTTP methods to authorization actions.
func mapHTTPMethodToAction(method string) string {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return interfaces.ActionRead
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return interfaces.ActionWrite
	case http.MethodDelete:
		return interfaces.ActionDelete
	default:
		return interfaces.ActionRead
	}
}

// buildAuthzContext builds the authorization context from the HTTP request.
func buildAuthzContext(r *http.Request) map[string]interface{} {
	ctx := make(map[string]interface{})

	// Add client IP
	ctx["ip_address"] = getClientIP(r)

	// Add user agent
	if ua := r.Header.Get("User-Agent"); ua != "" {
		ctx["user_agent"] = ua
	}

	// Add HTTP method
	ctx["http_method"] = r.Method

	// Add request path
	ctx["request_path"] = r.URL.Path

	// Add request ID if available
	if reqID := GetRequestID(r.Context()); reqID != "" {
		ctx["request_id"] = reqID
	}

	return ctx
}

// Error types for authorization middleware

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

// AuthorizationDeniedError is returned when authorization is denied.
type AuthorizationDeniedError struct {
	Reason string
}

func (e *AuthorizationDeniedError) Error() string {
	if e.Reason != "" {
		return "authorization denied: " + e.Reason
	}
	return "authorization denied"
}

// Response helpers

func respondUnauthorized(w http.ResponseWriter, r *http.Request, message string) {
	RespondError(w, r, http.StatusUnauthorized, &UnauthorizedError{Message: message})
}

func respondForbidden(w http.ResponseWriter, r *http.Request, reason string) {
	RespondError(w, r, http.StatusForbidden, &ForbiddenError{Reason: reason})
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

// Logging helpers

func (m *AuthzMiddleware) logUnauthorized(r *http.Request, reason string) {
	if m.logger == nil {
		return
	}
	m.logger.WithFields(map[string]interface{}{
		"path":       r.URL.Path,
		"method":     r.Method,
		"ip_address": getClientIP(r),
		"reason":     reason,
		"request_id": GetRequestID(r.Context()),
	}).Warn("Authorization failed: unauthorized")
}

func (m *AuthzMiddleware) logAuthzError(r *http.Request, req *interfaces.AuthorizationRequest, err error) {
	if m.logger == nil {
		return
	}
	m.logger.WithError(err).WithFields(map[string]interface{}{
		"path":       r.URL.Path,
		"method":     r.Method,
		"ip_address": getClientIP(r),
		"subject_id": req.Subject.ID,
		"resource":   req.Resource,
		"action":     req.Action,
		"request_id": GetRequestID(r.Context()),
	}).Error("Authorization check error")
}

func (m *AuthzMiddleware) logDenied(r *http.Request, req *interfaces.AuthorizationRequest, decision *interfaces.AuthorizationDecision) {
	if m.logger == nil {
		return
	}
	m.logger.WithFields(map[string]interface{}{
		"path":           r.URL.Path,
		"method":         r.Method,
		"ip_address":     getClientIP(r),
		"subject_id":     req.Subject.ID,
		"username":       req.Subject.Username,
		"resource":       req.Resource,
		"action":         req.Action,
		"reason":         decision.Reason,
		"provider":       decision.Provider,
		"matched_policy": decision.MatchedPolicy,
		"decision_id":    decision.DecisionID,
		"request_id":     GetRequestID(r.Context()),
	}).Info("Authorization denied")
}

func (m *AuthzMiddleware) logAllowed(r *http.Request, req *interfaces.AuthorizationRequest, decision *interfaces.AuthorizationDecision) {
	if m.logger == nil {
		return
	}
	m.logger.WithFields(map[string]interface{}{
		"path":           r.URL.Path,
		"method":         r.Method,
		"subject_id":     req.Subject.ID,
		"username":       req.Subject.Username,
		"resource":       req.Resource,
		"action":         req.Action,
		"provider":       decision.Provider,
		"matched_policy": decision.MatchedPolicy,
		"decision_id":    decision.DecisionID,
		"evaluation_ns":  decision.EvaluationTimeNs,
	}).Debug("Authorization allowed")
}
