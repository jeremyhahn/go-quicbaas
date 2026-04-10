//go:build rest && iam

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

package rest

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// IAM-specific context keys for middleware values.
const (
	// ContextKeyTenant is the context key for tenant ID.
	ContextKeyTenant contextKey = "tenant_id"

	// ContextKeyUser is the context key for authenticated user.
	ContextKeyUser contextKey = "user"

	// ContextKeyClaims is the context key for JWT claims.
	ContextKeyClaims contextKey = "claims"
)

// JWTMiddleware validates JWT access tokens and adds claims to the request context.
// This middleware should be applied to all protected API endpoints.
func (h *IAMHandler) JWTMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Extract bearer token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			RespondIAMError(w, r, http.StatusUnauthorized,
				NewIAMAPIError(ErrUnauthorized, "authorization header is required"))
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			RespondIAMError(w, r, http.StatusUnauthorized,
				NewIAMAPIError(ErrUnauthorized, "invalid authorization header format"))
			return
		}

		accessToken := parts[1]

		// Get OIDC provider to validate token
		oidcProvider, ok := h.server.service.(interface {
			GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
		})
		if !ok {
			RespondIAMError(w, r, http.StatusInternalServerError,
				NewIAMAPIError(ErrInternalError, "OIDC provider not available"))
			return
		}

		provider, err := oidcProvider.GetOIDCProvider(ctx)
		if err != nil {
			h.server.handleServiceError(w, r, err)
			return
		}

		// Validate token and extract claims
		// Note: This is a simplified version. In production, you'd parse and validate the JWT
		userInfo, err := provider.UserInfo(ctx, accessToken)
		if err != nil {
			RespondIAMError(w, r, http.StatusUnauthorized,
				NewIAMAPIError(ErrUnauthorized, "invalid or expired token"))
			return
		}

		// Add user info to context
		ctx = context.WithValue(ctx, ContextKeyUser, userInfo)

		// Add tenant ID to context if present
		if userInfo != nil && userInfo.TenantID != "" {
			ctx = context.WithValue(ctx, ContextKeyTenant, userInfo.TenantID)
		}

		// Call next handler with enriched context
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// TenantMiddleware extracts tenant ID from URL path and validates tenant existence.
// This middleware should be applied to tenant-scoped endpoints.
func (h *IAMHandler) TenantMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Extract tenant ID from URL path
		tenantID := chi.URLParam(r, "tenant_id")
		if tenantID == "" {
			RespondIAMError(w, r, http.StatusBadRequest,
				NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
			return
		}

		// Get tenant manager
		tenantMgr, ok := h.server.service.(interface {
			GetTenantManager(ctx context.Context) (TenantManager, error)
		})
		if !ok {
			RespondIAMError(w, r, http.StatusInternalServerError,
				NewIAMAPIError(ErrInternalError, "tenant manager not available"))
			return
		}

		mgr, err := tenantMgr.GetTenantManager(ctx)
		if err != nil {
			h.server.handleServiceError(w, r, err)
			return
		}

		// Validate tenant exists
		tenant, err := mgr.GetTenant(ctx, tenantID)
		if err != nil {
			h.server.handleServiceError(w, r, err)
			return
		}

		// Add tenant to context
		ctx = context.WithValue(ctx, ContextKeyTenant, tenant.ID)

		// Call next handler with enriched context
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ABACMiddleware enforces attribute-based access control using policy engine.
// This middleware should be applied to endpoints requiring fine-grained authorization.
func (h *IAMHandler) ABACMiddleware(resource, action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Extract user from context (set by JWT middleware)
			userInfo, ok := ctx.Value(ContextKeyUser).(*iam.UserInfoResponse)
			if !ok || userInfo == nil {
				RespondIAMError(w, r, http.StatusUnauthorized,
					NewIAMAPIError(ErrUnauthorized, "user not authenticated"))
				return
			}

			// Extract tenant ID
			tenantID, _ := ctx.Value(ContextKeyTenant).(string)

			// Build subject from user info
			subject := &iam.Subject{
				ID:            userInfo.Sub,
				Name:          userInfo.Name,
				Email:         userInfo.Email,
				TenantID:      tenantID,
				Authenticated: true,
			}

			// Get policy engine
			policyEngine, ok := h.server.service.(interface {
				GetPolicyEngine(ctx context.Context) (*iam.PolicyEngine, error)
			})
			if !ok {
				RespondIAMError(w, r, http.StatusInternalServerError,
					NewIAMAPIError(ErrInternalError, "policy engine not available"))
				return
			}

			engine, err := policyEngine.GetPolicyEngine(ctx)
			if err != nil {
				h.server.handleServiceError(w, r, err)
				return
			}

			// Perform authorization check
			checkReq := &iam.CheckRequest{
				Subject:  *subject,
				Action:   action,
				Resource: resource,
				Context: map[string]interface{}{
					"ip_address": r.RemoteAddr,
					"user_agent": r.UserAgent(),
					"method":     r.Method,
					"path":       r.URL.Path,
				},
			}

			checkResp, err := engine.Evaluate(ctx, checkReq)
			if err != nil {
				h.server.handleServiceError(w, r, err)
				return
			}

			// Deny access if not allowed
			if !checkResp.Allowed {
				RespondIAMError(w, r, http.StatusForbidden,
					NewIAMAPIError(ErrForbidden, "access denied"))
				return
			}

			// Call next handler
			next.ServeHTTP(w, r)
		})
	}
}

// ImpersonationMiddleware checks for an active impersonation session and injects context.
// This middleware reads the X-Impersonation-Session-ID header and validates the session.
// If valid, it adds the ImpersonationContext to the request context for downstream handlers.
func (h *IAMHandler) ImpersonationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Check for impersonation session header
		sessionID := r.Header.Get("X-Impersonation-Session-ID")
		if sessionID == "" {
			// No impersonation, proceed normally
			next.ServeHTTP(w, r)
			return
		}

		// Get admin manager to validate session
		adminMgr, ok := h.server.service.(interface {
			GetAdminManager(ctx context.Context) (*iam.AdminManager, error)
		})
		if !ok {
			RespondIAMError(w, r, http.StatusInternalServerError,
				NewIAMAPIError(ErrInternalError, "admin manager not available"))
			return
		}

		mgr, err := adminMgr.GetAdminManager(ctx)
		if err != nil {
			h.server.handleServiceError(w, r, err)
			return
		}

		// Get and validate the impersonation session
		session, err := mgr.GetImpersonationSession(ctx, sessionID)
		if err != nil {
			RespondIAMError(w, r, http.StatusUnauthorized,
				NewIAMAPIError(ErrUnauthorized, "invalid impersonation session"))
			return
		}

		// Check if session has ended
		if session.EndedAt != nil {
			RespondIAMError(w, r, http.StatusUnauthorized,
				NewIAMAPIError(ErrUnauthorized, "impersonation session has ended"))
			return
		}

		// Check if session has expired
		// We don't use time.Now() directly, allowing tests to inject time
		if session.ExpiresAt.Before(timeNow()) {
			RespondIAMError(w, r, http.StatusUnauthorized,
				NewIAMAPIError(ErrUnauthorized, "impersonation session has expired"))
			return
		}

		// Create impersonation context from session
		impCtx := iam.ImpersonationContextFromSession(session)

		// Add impersonation context to request context
		ctx = iam.WithImpersonationContext(ctx, impCtx)

		// Call next handler with enriched context
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// timeNow is a variable that can be overridden in tests
var timeNow = func() time.Time { return time.Now().UTC() }

// ImpersonationAuditMiddleware records operations performed during impersonation sessions.
// This middleware should be placed after ImpersonationMiddleware and other auth middleware.
// It logs each request as an operation in the impersonation audit trail.
func (h *IAMHandler) ImpersonationAuditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Check if this is an impersonated request
		impCtx := iam.GetImpersonationContext(ctx)
		if impCtx == nil {
			// Not impersonating, proceed normally
			next.ServeHTTP(w, r)
			return
		}

		// Record the operation before handling
		adminMgr, ok := h.server.service.(interface {
			GetAdminManager(ctx context.Context) (*iam.AdminManager, error)
		})
		if ok {
			mgr, err := adminMgr.GetAdminManager(ctx)
			if err == nil {
				// Build operation details
				operation := r.Method + " " + r.URL.Path
				resource := &iam.AuditResource{
					Type: "http_request",
					ID:   r.URL.Path,
					Name: r.Method + " " + r.URL.Path,
				}
				details := map[string]interface{}{
					"method":     r.Method,
					"path":       r.URL.Path,
					"query":      r.URL.RawQuery,
					"ip_address": r.RemoteAddr,
					"user_agent": r.UserAgent(),
				}

				// Record the operation (this increments operation count)
				_ = mgr.RecordImpersonatedOperation(ctx, impCtx.SessionID, operation, resource, iam.AuditOutcomeSuccess, details)
			}
		}

		// Call next handler
		next.ServeHTTP(w, r)
	})
}

// Helper functions to extract values from map[string]interface{}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getStringSlice(m map[string]interface{}, key string) []string {
	if v, ok := m[key].([]interface{}); ok {
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	if v, ok := m[key].([]string); ok {
		return v
	}
	return nil
}

func getMap(m map[string]interface{}, key string) map[string]interface{} {
	if v, ok := m[key].(map[string]interface{}); ok {
		return v
	}
	return nil
}
