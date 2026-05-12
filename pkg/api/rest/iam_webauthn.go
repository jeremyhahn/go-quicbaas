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
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// BeginRegistration handles POST /api/v1/tenants/{tenant_id}/webauthn/register/begin - Begin WebAuthn registration.
func (h *IAMHandler) BeginRegistration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
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

	// Parse request
	var req struct {
		UserID string `json:"user_id"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	if req.UserID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user_id is required"))
		return
	}

	// Get WebAuthn manager
	webAuthnMgr, ok := h.server.service.(interface {
		GetWebAuthnManager(ctx context.Context) (WebAuthnManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "WebAuthn manager not available"))
		return
	}

	mgr, err := webAuthnMgr.GetWebAuthnManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "WebAuthn manager not initialized"))
		return
	}

	// Begin registration
	options, err := mgr.BeginRegistration(ctx, tenantID, req.UserID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, options)
}

// FinishRegistration handles POST /api/v1/tenants/{tenant_id}/webauthn/register/finish - Finish WebAuthn registration.
func (h *IAMHandler) FinishRegistration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
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

	// Parse request
	var req struct {
		UserID     string                 `json:"user_id"`
		Credential map[string]interface{} `json:"credential"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	if req.UserID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user_id is required"))
		return
	}

	if req.Credential == nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "credential is required"))
		return
	}

	// Get WebAuthn manager
	webAuthnMgr, ok := h.server.service.(interface {
		GetWebAuthnManager(ctx context.Context) (WebAuthnManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "WebAuthn manager not available"))
		return
	}

	mgr, err := webAuthnMgr.GetWebAuthnManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "WebAuthn manager not initialized"))
		return
	}

	// Finish registration - the adapter handles parsing the credential
	if err := mgr.FinishRegistration(ctx, tenantID, req.UserID, req.Credential); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Return success response
	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "WebAuthn credential registered successfully",
	})
}

// BeginLogin handles POST /api/v1/tenants/{tenant_id}/webauthn/login/begin - Begin WebAuthn login.
func (h *IAMHandler) BeginLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
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

	// Parse request
	var req struct {
		Email string `json:"email"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	if req.Email == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "email is required"))
		return
	}

	// Get WebAuthn manager
	webAuthnMgr, ok := h.server.service.(interface {
		GetWebAuthnManager(ctx context.Context) (WebAuthnManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "WebAuthn manager not available"))
		return
	}

	mgr, err := webAuthnMgr.GetWebAuthnManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "WebAuthn manager not initialized"))
		return
	}

	requestID := r.Header.Get("X-Request-ID")

	// Emit begin-login audit event.
	h.logWebAuthnBeginLoginAudit(ctx, tenantID, req.Email, requestID, r)

	// Begin login
	options, err := mgr.BeginLogin(ctx, tenantID, req.Email)
	if err != nil {
		h.logWebAuthnLoginFailureAudit(ctx, tenantID, req.Email, ErrInternalError, err.Error(), requestID, r)
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, options)
}

// FinishLogin handles POST /api/v1/tenants/{tenant_id}/webauthn/login/finish - Finish WebAuthn login.
func (h *IAMHandler) FinishLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
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

	// Parse request
	var req struct {
		Credential map[string]interface{} `json:"credential"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	if req.Credential == nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "credential is required"))
		return
	}

	// Get WebAuthn manager
	webAuthnMgr, ok := h.server.service.(interface {
		GetWebAuthnManager(ctx context.Context) (WebAuthnManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "WebAuthn manager not available"))
		return
	}

	mgr, err := webAuthnMgr.GetWebAuthnManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "WebAuthn manager not initialized"))
		return
	}

	finishRequestID := r.Header.Get("X-Request-ID")

	// Finish login - the adapter handles parsing the credential
	user, err := mgr.FinishLogin(ctx, tenantID, req.Credential)
	if err != nil {
		h.logWebAuthnLoginFailureAudit(ctx, tenantID, "", ErrUnauthorized, err.Error(), finishRequestID, r)
		h.server.handleServiceError(w, r, err)
		return
	}

	// Resolve OIDC provider to issue tokens.
	oidcGetter, ok := h.server.service.(interface {
		GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "OIDC provider not available"))
		return
	}

	oidcProvider, err := oidcGetter.GetOIDCProvider(ctx)
	if err != nil || oidcProvider == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "OIDC provider not configured"))
		return
	}

	// Resolve AAGUID from the credential map for audit and id_token claims.
	aaguidHex := resolveAAGUIDHex(req.Credential)

	tokens, err := oidcProvider.IssueHardwareTokens(ctx, iam.HardwareTokenOptions{
		User:         user,
		AMR:          []string{"webauthn", "hwk"},
		ACR:          iam.ACRHardwareAttested,
		AttestAAGUID: aaguidHex,
		AttestLevel:  "hardware",
	})
	if err != nil {
		h.logWebAuthnLoginFailureAudit(ctx, user.TenantID, user.Email, ErrInternalError, err.Error(), finishRequestID, r)
		h.server.handleServiceError(w, r, err)
		return
	}

	// Emit success audit event for the WebAuthn login.
	h.logWebAuthnLoginAudit(ctx, user.TenantID, user.ID, user.Email, aaguidHex, finishRequestID, r)

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"access_token":  tokens.AccessToken,
		"id_token":      tokens.IDToken,
		"refresh_token": tokens.RefreshToken,
		"token_type":    tokens.TokenType,
		"expires_in":    tokens.ExpiresIn,
	})
}

// resolveAAGUIDHex extracts the AAGUID from a WebAuthn credential map and
// returns it as a lowercase hex string. Returns empty string if unavailable.
func resolveAAGUIDHex(credential map[string]interface{}) string {
	resp, ok := credential["response"].(map[string]interface{})
	if !ok {
		return ""
	}
	aaguid, ok := resp["aaguid"]
	if !ok {
		return ""
	}
	switch v := aaguid.(type) {
	case []byte:
		return hex.EncodeToString(v)
	case string:
		return v
	}
	return ""
}

// logWebAuthnBeginLoginAudit emits an info-level audit event when a WebAuthn login ceremony begins.
// Emits event_type "auth.login.fido2.begin".
func (h *IAMHandler) logWebAuthnBeginLoginAudit(ctx context.Context, tenantID, email, requestID string, r *http.Request) {
	auditGetter, ok := h.server.service.(interface {
		GetAuditLogger(ctx context.Context) (iam.AuditLogger, error)
	})
	if !ok {
		return
	}
	logger, err := auditGetter.GetAuditLogger(ctx)
	if err != nil || logger == nil {
		return
	}
	logger.LogAsync(&iam.AuditEvent{
		TenantID:  tenantID,
		Timestamp: time.Now().UTC(),
		EventType: iam.AuditEventAuthentication,
		Category:  iam.AuditCategoryAuthentication,
		Actor:     &iam.AuditActor{Email: email},
		Action:    "auth.login.fido2.begin",
		Outcome:   iam.AuditOutcomeSuccess,
		Details: map[string]interface{}{
			"tenant_id":  tenantID,
			"user_email": email,
			"request_id": requestID,
		},
		IPAddress: r.RemoteAddr,
		UserAgent: r.Header.Get("User-Agent"),
	})
}

// logWebAuthnLoginAudit emits a success audit event for a completed WebAuthn login.
// Emits event_type "auth.login.fido2.finish.success".
func (h *IAMHandler) logWebAuthnLoginAudit(ctx context.Context, tenantID, userID, email, aaguid, requestID string, r *http.Request) {
	auditGetter, ok := h.server.service.(interface {
		GetAuditLogger(ctx context.Context) (iam.AuditLogger, error)
	})
	if !ok {
		return
	}
	logger, err := auditGetter.GetAuditLogger(ctx)
	if err != nil || logger == nil {
		return
	}
	logger.LogAsync(&iam.AuditEvent{
		TenantID:  tenantID,
		Timestamp: time.Now().UTC(),
		EventType: iam.AuditEventAuthentication,
		Category:  iam.AuditCategoryAuthentication,
		Actor: &iam.AuditActor{
			UserID: userID,
			Email:  email,
		},
		Action:  "auth.login.fido2.finish.success",
		Outcome: iam.AuditOutcomeSuccess,
		Details: map[string]interface{}{
			"tenant_id":    tenantID,
			"user_id":      userID,
			"user_email":   email,
			"aaguid":       aaguid,
			"attest_level": "hardware",
			"acr":          iam.ACRHardwareAttested,
			"amr":          []string{"webauthn", "hwk"},
			"request_id":   requestID,
		},
		IPAddress: r.RemoteAddr,
		UserAgent: r.Header.Get("User-Agent"),
	})
}

// logWebAuthnLoginFailureAudit emits a warning-level audit event for a failed WebAuthn login.
// Emits event_type "auth.login.fido2.finish.failure".
func (h *IAMHandler) logWebAuthnLoginFailureAudit(ctx context.Context, tenantID, email, errorCode, errorMessage, requestID string, r *http.Request) {
	auditGetter, ok := h.server.service.(interface {
		GetAuditLogger(ctx context.Context) (iam.AuditLogger, error)
	})
	if !ok {
		return
	}
	logger, err := auditGetter.GetAuditLogger(ctx)
	if err != nil || logger == nil {
		return
	}
	logger.LogAsync(&iam.AuditEvent{
		TenantID:  tenantID,
		Timestamp: time.Now().UTC(),
		EventType: iam.AuditEventAuthentication,
		Category:  iam.AuditCategoryAuthentication,
		Actor:     &iam.AuditActor{Email: email},
		Action:    "auth.login.fido2.finish.failure",
		Outcome:   iam.AuditOutcomeFailure,
		Details: map[string]interface{}{
			"tenant_id":     tenantID,
			"user_email":    email,
			"error_code":    errorCode,
			"error_message": errorMessage,
			"request_id":    requestID,
		},
		IPAddress: r.RemoteAddr,
		UserAgent: r.Header.Get("User-Agent"),
	})
}
