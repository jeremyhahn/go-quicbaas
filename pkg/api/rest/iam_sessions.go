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

package rest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// SessionManager is a type alias for service.SessionManager to ensure type
// assertion compatibility when the REST layer retrieves the session manager
// from the service. This follows the same pattern used by MFAManager.
type SessionManager = service.SessionManager

// CreateSessionRequest represents the request body for creating a session.
type CreateSessionRequest struct {
	ID          string                 `json:"id,omitempty"`
	UserID      string                 `json:"user_id,omitempty"`
	TenantID    string                 `json:"tenant_id,omitempty"`
	TokenHash   string                 `json:"token_hash"`
	MFAVerified bool                   `json:"mfa_verified"`
	IPAddress   string                 `json:"ip_address,omitempty"`
	UserAgent   string                 `json:"user_agent,omitempty"`
	DeviceInfo  map[string]interface{} `json:"device_info,omitempty"`
	ExpiresAt   string                 `json:"expires_at,omitempty"`
}

// CreateSession handles POST /api/v1/tenants/{tenant_id}/users/{user_id}/sessions - Create a new session.
func (h *IAMHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}
	ctx, tenantID, ok := h.server.urlTenant(w, r, "rest.IAM", "tenant_id")
	if !ok {
		return
	}

	userID, ok := requireUserIDParam(w, r)
	if !ok {
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	var req CreateSessionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid request body"))
		return
	}

	// Validate token hash is provided
	if req.TokenHash == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "token_hash is required"))
		return
	}

	// Get session manager from service
	sessionMgr, ok := h.server.service.(interface {
		GetSessionManager(ctx context.Context) (SessionManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "session manager not available"))
		return
	}

	mgr, err := sessionMgr.GetSessionManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "session manager not initialized"))
		return
	}

	// Build session object
	sessionID := req.ID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	now := time.Now()
	expiresAt := now.Add(24 * time.Hour) // Default 24 hours

	// Parse custom expiration if provided
	if req.ExpiresAt != "" {
		parsedExpiry, parseErr := time.Parse(time.RFC3339, req.ExpiresAt)
		if parseErr == nil {
			expiresAt = parsedExpiry
		}
	}

	// Build device info if provided
	var deviceInfo *iam.DeviceInfo
	if req.DeviceInfo != nil {
		deviceInfo = &iam.DeviceInfo{}
		if id, ok := req.DeviceInfo["id"].(string); ok {
			deviceInfo.ID = id
		}
		if name, ok := req.DeviceInfo["name"].(string); ok {
			deviceInfo.Name = name
		}
		if deviceType, ok := req.DeviceInfo["type"].(string); ok {
			deviceInfo.Type = deviceType
		}
		if fingerprint, ok := req.DeviceInfo["fingerprint"].(string); ok {
			deviceInfo.Fingerprint = fingerprint
		}
		if trusted, ok := req.DeviceInfo["trusted"].(bool); ok {
			deviceInfo.Trusted = trusted
		}
	}

	session := &iam.SessionExt{
		ID:          sessionID,
		UserID:      userID,
		TenantID:    tenantID,
		TokenHash:   req.TokenHash,
		MFAVerified: req.MFAVerified,
		IPAddress:   req.IPAddress,
		UserAgent:   req.UserAgent,
		DeviceInfo:  deviceInfo,
		Created:     now,
		LastActive:  now,
		ExpiresAt:   expiresAt,
	}

	// Create the session
	if err := mgr.Create(ctx, session); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Build response with device info as map
	var deviceInfoMap map[string]interface{}
	if session.DeviceInfo != nil {
		deviceInfoMap = map[string]interface{}{
			"id":          session.DeviceInfo.ID,
			"name":        session.DeviceInfo.Name,
			"type":        session.DeviceInfo.Type,
			"fingerprint": session.DeviceInfo.Fingerprint,
			"trusted":     session.DeviceInfo.Trusted,
		}
	}

	response := map[string]interface{}{
		"id":           session.ID,
		"user_id":      strconv.FormatUint(session.UserID, 10),
		"tenant_id":    session.TenantID,
		"mfa_verified": session.MFAVerified,
		"ip_address":   session.IPAddress,
		"user_agent":   session.UserAgent,
		"device_info":  deviceInfoMap,
		"created":      session.Created.Format(time.RFC3339),
		"last_active":  session.LastActive.Format(time.RFC3339),
		"expires_at":   session.ExpiresAt.Format(time.RFC3339),
	}

	RespondJSON(w, r, http.StatusCreated, response)
}

// ListUserSessions handles GET /api/v1/tenants/{tenant_id}/users/{user_id}/sessions - List sessions for a user.
func (h *IAMHandler) ListUserSessions(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}
	ctx, tenantID, ok := h.server.urlTenant(w, r, "rest.IAM", "tenant_id")
	if !ok {
		return
	}

	userID, ok := requireUserIDParam(w, r)
	if !ok {
		return
	}

	// Optional limit query parameter — parity with the parameterized gRPC
	// ListSessions verb, whose request carries a limit. Zero or absent means
	// unlimited; a non-numeric or negative value is a client error.
	limit := 0
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, parseErr := strconv.Atoi(rawLimit)
		if parseErr != nil || parsed < 0 {
			RespondIAMError(w, r, http.StatusBadRequest,
				NewIAMAPIError(ErrBadRequest, "invalid limit parameter"))
			return
		}
		limit = parsed
	}

	// Get session manager from service
	sessionMgr, ok := h.server.service.(interface {
		GetSessionManager(ctx context.Context) (SessionManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "session manager not available"))
		return
	}

	mgr, err := sessionMgr.GetSessionManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "session manager not initialized"))
		return
	}

	// List sessions for the target user named by the URL path.
	sessions, err := mgr.List(ctx, userID, tenantID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if limit > 0 && len(sessions) > limit {
		sessions = sessions[:limit]
	}

	// Sanitize sessions (remove sensitive data)
	sanitizedSessions := make([]map[string]interface{}, len(sessions))
	for i, session := range sessions {
		var deviceInfoMap map[string]interface{}
		if session.DeviceInfo != nil {
			deviceInfoMap = map[string]interface{}{
				"id":          session.DeviceInfo.ID,
				"name":        session.DeviceInfo.Name,
				"type":        session.DeviceInfo.Type,
				"fingerprint": session.DeviceInfo.Fingerprint,
				"trusted":     session.DeviceInfo.Trusted,
			}
		}
		sanitizedSessions[i] = map[string]interface{}{
			"id":           session.ID,
			"created":      session.Created.Format(time.RFC3339),
			"last_active":  session.LastActive.Format(time.RFC3339),
			"expires_at":   session.ExpiresAt.Format(time.RFC3339),
			"mfa_verified": session.MFAVerified,
			"ip_address":   session.IPAddress,
			"user_agent":   session.UserAgent,
			"device_info":  deviceInfoMap,
		}
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"sessions": sanitizedSessions,
	})
}

// RevokeAllUserSessions handles
// DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/sessions - Revoke all
// sessions of the user named by the URL path.
//
// This is the path-scoped admin counterpart of RevokeAllSessions and the REST
// projection of the parameterized SDK Session RevokeAll verb (gRPC:
// SessionService/RevokeAllSessions). The TARGET identity comes from the URL
// path — never from query parameters — while the CALLER identity and
// authorization are enforced by the authz middleware chain on the
// /api/v1/tenants surface (PEP-mapped to users:update).
func (h *IAMHandler) RevokeAllUserSessions(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}
	ctx, tenantID, ok := h.server.urlTenant(w, r, "rest.IAM", "tenant_id")
	if !ok {
		return
	}

	userID, ok := requireUserIDParam(w, r)
	if !ok {
		return
	}

	// Get session manager from service
	sessionMgr, ok := h.server.service.(interface {
		GetSessionManager(ctx context.Context) (SessionManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "session manager not available"))
		return
	}

	mgr, err := sessionMgr.GetSessionManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "session manager not initialized"))
		return
	}

	if err := mgr.RevokeAllForUser(ctx, userID, tenantID); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondNoContent(w, r)
}

// ListSessions handles GET /api/v1/iam/sessions - List the authenticated
// caller's active sessions.
func (h *IAMHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Identity comes exclusively from the authenticated Subject injected by
	// the authz middleware chain (injectPrincipal). The legacy
	// query-parameter identity fallback was an authentication bypass and has
	// been removed; explicit user_id / tenant_id parameters from direct HTTP
	// callers are validated against the principal instead. Cross-user
	// listing is the admin surface: ListUserSessions. The Go SDK REST
	// transport routes its parameterized Session/List verb there and emits
	// no identity parameters here.
	subject, callerID, ok := h.server.requireTenantUserSubject(w, r)
	if !ok {
		return
	}
	if !queryIdentityMatches(w, r, subject, resolvedCallerAccount(callerID)) {
		return
	}

	// Get session manager from service
	sessionMgr, ok := h.server.service.(interface {
		GetSessionManager(ctx context.Context) (SessionManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "session manager not available"))
		return
	}

	mgr, err := sessionMgr.GetSessionManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "session manager not initialized"))
		return
	}

	// List sessions scoped to the authenticated caller.
	sessions, err := mgr.List(ctx, callerID, subject.TenantID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Sanitize sessions (remove sensitive data)
	sanitizedSessions := make([]map[string]interface{}, len(sessions))
	for i, session := range sessions {
		var deviceInfoMap map[string]interface{}
		if session.DeviceInfo != nil {
			deviceInfoMap = map[string]interface{}{
				"id":          session.DeviceInfo.ID,
				"name":        session.DeviceInfo.Name,
				"type":        session.DeviceInfo.Type,
				"fingerprint": session.DeviceInfo.Fingerprint,
				"trusted":     session.DeviceInfo.Trusted,
			}
		}
		sanitizedSessions[i] = map[string]interface{}{
			"id":           session.ID,
			"created":      session.Created.Format(time.RFC3339),
			"last_active":  session.LastActive.Format(time.RFC3339),
			"expires_at":   session.ExpiresAt.Format(time.RFC3339),
			"mfa_verified": session.MFAVerified,
			"ip_address":   session.IPAddress,
			"user_agent":   session.UserAgent,
			"device_info":  deviceInfoMap,
		}
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"sessions": sanitizedSessions,
	})
}

// GetSession handles GET /api/v1/iam/sessions/{id} - Get session details.
func (h *IAMHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "session ID is required"))
		return
	}

	// Identity comes exclusively from the authenticated Subject injected by
	// the authz middleware chain.
	subject, callerID, ok := h.server.requireTenantUserSubject(w, r)
	if !ok {
		return
	}

	// Get session manager
	sessionMgr, ok := h.server.service.(interface {
		GetSessionManager(ctx context.Context) (SessionManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "session manager not available"))
		return
	}

	mgr, err := sessionMgr.GetSessionManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "session manager not initialized"))
		return
	}

	// Get session
	session, err := mgr.Get(ctx, sessionID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Verify the session belongs to the authenticated caller.
	if session.UserID != callerID || session.TenantID != subject.TenantID {
		RespondIAMError(w, r, http.StatusForbidden,
			NewIAMAPIError(ErrForbidden, "access denied"))
		return
	}

	// Build device info map
	var deviceInfoMap map[string]interface{}
	if session.DeviceInfo != nil {
		deviceInfoMap = map[string]interface{}{
			"id":          session.DeviceInfo.ID,
			"name":        session.DeviceInfo.Name,
			"type":        session.DeviceInfo.Type,
			"fingerprint": session.DeviceInfo.Fingerprint,
			"trusted":     session.DeviceInfo.Trusted,
		}
	}

	// Sanitize session
	sanitizedSession := map[string]interface{}{
		"id":           session.ID,
		"created":      session.Created.Format(time.RFC3339),
		"last_active":  session.LastActive.Format(time.RFC3339),
		"expires_at":   session.ExpiresAt.Format(time.RFC3339),
		"mfa_verified": session.MFAVerified,
		"ip_address":   session.IPAddress,
		"user_agent":   session.UserAgent,
		"device_info":  deviceInfoMap,
	}

	RespondJSON(w, r, http.StatusOK, sanitizedSession)
}

// RevokeSession handles DELETE /api/v1/iam/sessions/{id} - Revoke specific session.
func (h *IAMHandler) RevokeSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "session ID is required"))
		return
	}

	// Identity comes exclusively from the authenticated Subject injected by
	// the authz middleware chain.
	subject, callerID, ok := h.server.requireTenantUserSubject(w, r)
	if !ok {
		return
	}

	// Get session manager
	sessionMgr, ok := h.server.service.(interface {
		GetSessionManager(ctx context.Context) (SessionManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "session manager not available"))
		return
	}

	mgr, err := sessionMgr.GetSessionManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "session manager not initialized"))
		return
	}

	// Verify the session belongs to the authenticated caller.
	session, err := mgr.Get(ctx, sessionID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	if session.UserID != callerID || session.TenantID != subject.TenantID {
		RespondIAMError(w, r, http.StatusForbidden,
			NewIAMAPIError(ErrForbidden, "access denied"))
		return
	}

	// Revoke session
	if err := mgr.Revoke(ctx, sessionID); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondNoContent(w, r)
}

// RevokeAllSessions handles DELETE /api/v1/iam/sessions - Revoke all of the
// authenticated caller's sessions (logout everywhere).
func (h *IAMHandler) RevokeAllSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Identity comes exclusively from the authenticated Subject injected by
	// the authz middleware chain (injectPrincipal). The legacy
	// query-parameter identity fallback was an authentication bypass and has
	// been removed; explicit user_id / tenant_id parameters from direct HTTP
	// callers are validated against the principal instead. Cross-user
	// revocation is the admin surface: RevokeAllUserSessions. The Go SDK
	// REST transport routes its parameterized Session/RevokeAll verb there
	// and emits no identity parameters here.
	subject, callerID, ok := h.server.requireTenantUserSubject(w, r)
	if !ok {
		return
	}
	if !queryIdentityMatches(w, r, subject, resolvedCallerAccount(callerID)) {
		return
	}

	// Check query parameter for "except" to keep current session
	exceptSessionID := r.URL.Query().Get("except")

	// Get session manager
	sessionMgr, ok := h.server.service.(interface {
		GetSessionManager(ctx context.Context) (SessionManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "session manager not available"))
		return
	}

	mgr, err := sessionMgr.GetSessionManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "session manager not initialized"))
		return
	}

	// Revoke sessions
	if exceptSessionID != "" {
		// Verify the exception session belongs to the authenticated caller.
		session, err := mgr.Get(ctx, exceptSessionID)
		if err != nil {
			h.server.handleServiceError(w, r, err)
			return
		}
		if session.UserID != callerID || session.TenantID != subject.TenantID {
			RespondIAMError(w, r, http.StatusForbidden,
				NewIAMAPIError(ErrForbidden, "access denied"))
			return
		}

		err = mgr.RevokeAllExcept(ctx, callerID, subject.TenantID, exceptSessionID)
		if err != nil {
			h.server.handleServiceError(w, r, err)
			return
		}
	} else {
		err = mgr.RevokeAllForUser(ctx, callerID, subject.TenantID)
		if err != nil {
			h.server.handleServiceError(w, r, err)
			return
		}
	}

	RespondNoContent(w, r)
}

// RevokeOtherSessions handles POST /api/v1/iam/sessions/revoke-others -
// Revoke all of the authenticated caller's sessions except the one named in
// the request body.
func (h *IAMHandler) RevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Identity comes exclusively from the authenticated Subject injected by
	// the authz middleware chain.
	subject, callerID, ok := h.server.requireTenantUserSubject(w, r)
	if !ok {
		return
	}

	// The session to keep comes from the request body. The former untyped
	// "session_id" context read had no producer anywhere in the transport
	// stack and was removed.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	var currentSessionID string
	if len(body) > 0 {
		var req struct {
			KeepSessionID string `json:"keep_session_id,omitempty"`
		}

		if err := json.Unmarshal(body, &req); err == nil {
			currentSessionID = req.KeepSessionID
		}
	}
	if currentSessionID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "current session ID is required"))
		return
	}

	// Get session manager
	sessionMgr, ok := h.server.service.(interface {
		GetSessionManager(ctx context.Context) (SessionManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "session manager not available"))
		return
	}

	mgr, err := sessionMgr.GetSessionManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "session manager not initialized"))
		return
	}

	// Verify the session to keep belongs to the authenticated caller.
	session, err := mgr.Get(ctx, currentSessionID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if session.UserID != callerID || session.TenantID != subject.TenantID {
		RespondIAMError(w, r, http.StatusForbidden,
			NewIAMAPIError(ErrForbidden, "access denied"))
		return
	}

	// Revoke all other sessions
	err = mgr.RevokeAllExcept(ctx, callerID, subject.TenantID, currentSessionID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"message":         "other sessions revoked successfully",
		"kept_session_id": currentSessionID,
	})
}
