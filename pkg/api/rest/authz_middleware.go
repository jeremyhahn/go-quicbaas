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
}

// AuthzMiddleware provides unified authorization middleware for REST API.
type AuthzMiddleware struct {
	logger         *logger.Logger
	tokenValidator TokenValidator
	authzService   interfaces.AuthorizationService
}

// NewAuthzMiddleware creates a new authorization middleware with the given configuration.
func NewAuthzMiddleware(cfg *AuthzMiddlewareConfig) *AuthzMiddleware {
	return &AuthzMiddleware{
		logger:         cfg.Logger,
		tokenValidator: cfg.TokenValidator,
		authzService:   cfg.AuthzService,
	}
}

// Authorize is the middleware function that enforces authorization on requests.
// It extracts the JWT token, validates it, extracts resource and action from the request,
// and calls the AuthorizationService to make an access control decision.
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

		// Validate the token
		ctx := r.Context()
		result, err := m.tokenValidator.ValidateToken(ctx, token)
		if err != nil || result == nil || !result.Valid {
			m.logUnauthorized(r, "invalid or expired token")
			respondUnauthorized(w, r, "Invalid or expired token")
			return
		}

		// Check if token is revoked
		if result.IsRevoked {
			m.logUnauthorized(r, "token has been revoked")
			respondUnauthorized(w, r, "Token has been revoked")
			return
		}

		// Build subject from validated token
		subject := interfaces.Subject{
			ID:            result.UserID,
			Username:      result.Username,
			Roles:         result.Roles,
			Authenticated: true,
			Attributes:    make(map[string]interface{}),
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

		// Inject user info into context for downstream handlers
		ctx = context.WithValue(ctx, UserIDKey, result.UserID)
		ctx = context.WithValue(ctx, UserRolesKey, result.Roles)
		ctx = context.WithValue(ctx, usernameKey, result.Username)

		// Continue with the request
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
// Authentication endpoints must be public to allow login.
var publicEndpoints = []string{
	// Authentication endpoints (password-based login)
	"/api/v1/auth/login",
	"/api/v1/auth/refresh",
	// Health and metrics endpoints
	"/api/v1/health",
	"/api/v1/health/liveness",
	"/api/v1/health/readiness",
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
