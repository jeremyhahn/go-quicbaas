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

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// ListUsers handles GET /api/v1/tenants/{tenant_id}/users - List users in a tenant.
func (h *IAMHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	// Parse query parameters
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	// Get identity store from service
	identityStore, ok := h.server.service.(interface {
		GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}

	store, err := identityStore.GetIdentityStore(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if store == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not initialized"))
		return
	}

	// Validate tenant exists
	_, err = store.GetTenant(ctx, tenantID)
	if err != nil {
		RespondIAMError(w, r, http.StatusNotFound,
			NewIAMAPIError(ErrNotFound, "tenant not found"))
		return
	}

	// List users
	opts := iam.ListUsersOptions{
		Limit:  limit,
		Offset: offset,
	}

	users, err := store.ListUsers(ctx, tenantID, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := map[string]interface{}{
		"users":  users,
		"limit":  limit,
		"offset": offset,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// CreateUser handles POST /api/v1/tenants/{tenant_id}/users - Create a new user.
func (h *IAMHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
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
		ID                string                 `json:"id,omitempty"`
		Email             string                 `json:"email"`
		Username          string                 `json:"username,omitempty"`
		Name              string                 `json:"name,omitempty"`
		Attributes        map[string]interface{} `json:"attributes,omitempty"`
		Enabled           bool                   `json:"enabled"`
		AuthMethods       []string               `json:"auth_methods,omitempty"`
		InviteExpiryHours int                    `json:"invite_expiry_hours,omitempty"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Validate required fields
	if req.Email == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "email is required"))
		return
	}

	// Default and cap invite expiry hours.
	if req.InviteExpiryHours <= 0 {
		req.InviteExpiryHours = 24
	}
	if req.InviteExpiryHours > 168 {
		req.InviteExpiryHours = 168
	}

	// Default auth methods.
	if len(req.AuthMethods) == 0 {
		req.AuthMethods = []string{"webauthn"}
	}

	// Get identity store
	identityStore, ok := h.server.service.(interface {
		GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}

	store, err := identityStore.GetIdentityStore(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if store == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not initialized"))
		return
	}

	// User quota (max users per tenant) is enforced by qrdb's TenantServicer.
	// qrdb will return a quota-exceeded error if the limit is reached; the
	// service layer propagates that as a 403 Forbidden through the normal error
	// path below. No BaaS-local count is maintained here.

	// Create user
	inviteExpiry := time.Duration(req.InviteExpiryHours) * time.Hour
	opts := iam.CreateUserOptions{
		ID:           req.ID,
		Email:        req.Email,
		Username:     req.Username,
		Name:         req.Name,
		Attributes:   req.Attributes,
		Enabled:      req.Enabled,
		AuthMethods:  req.AuthMethods,
		InviteExpiry: inviteExpiry,
	}

	user, err := store.CreateUser(ctx, tenantID, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Generate a one-time invite token.
	inviteToken, err := iam.GenerateInviteToken()
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	expiresAt := time.Now().UTC().Add(inviteExpiry)

	// Store the invite record so that the activation handlers can look it up.
	inviteRecord := &iam.InviteRecord{
		TokenHash: iam.TokenHashExported(inviteToken),
		UserID:    user.ID,
		TenantID:  tenantID,
		ExpiresAt: expiresAt,
	}
	if err := h.inviteStore.Create(ctx, inviteRecord); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Attach invite state to the user record (cleared on activation).
	pendingState := iam.UserStatePending
	updateOpts := iam.UpdateUserOptions{
		State:           &pendingState,
		AuthMethods:     &req.AuthMethods,
		InviteToken:     &inviteToken,
		InviteExpiresAt: func() **time.Time { p := &expiresAt; return &p }(),
	}

	updatedUser, updateErr := store.UpdateUser(ctx, tenantID, user.ID, updateOpts)
	if updateErr != nil {
		// Non-fatal: user was created; just cannot issue invite. Log and continue.
		h.logAuditEvent(ctx, tenantID,
			iam.AuditEventUserManagement,
			iam.AuditCategoryAdministration,
			"create",
			iam.AuditOutcomeFailure,
			&iam.AuditResource{Type: "user", ID: user.ID, Name: user.Email},
			map[string]interface{}{"email": user.Email, "error": updateErr.Error()},
		)
	} else if updatedUser != nil {
		user = updatedUser
	}

	// Derive invite URL. Use X-Forwarded-Host or Host header.
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = fwd
	}
	inviteURL := scheme + "://" + host + "/#/invite/" + inviteToken

	// Log audit event
	h.logAuditEvent(ctx, tenantID,
		iam.AuditEventUserManagement,
		iam.AuditCategoryAdministration,
		"create",
		iam.AuditOutcomeSuccess,
		&iam.AuditResource{Type: "user", ID: user.ID, Name: user.Email},
		map[string]interface{}{"email": user.Email, "auth_methods": req.AuthMethods},
	)

	// Return the canonical user object (matches the shape returned by GetUser/
	// UpdateUser/ListUsers) with invite_url appended. invite_token and
	// invite_expires_at are carried on the user record itself. This is the
	// only place the cleartext invite token is exposed; it is cleared from
	// the user record at activation time.
	RespondJSON(w, r, http.StatusCreated, struct {
		*iam.User
		InviteURL string `json:"invite_url,omitempty"`
	}{
		User:      user,
		InviteURL: inviteURL,
	})
}

// GetUser handles GET /api/v1/tenants/{tenant_id}/users/{user_id} - Get a specific user.
func (h *IAMHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user ID is required"))
		return
	}

	// Get identity store
	identityStore, ok := h.server.service.(interface {
		GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}

	store, err := identityStore.GetIdentityStore(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if store == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not initialized"))
		return
	}

	// Get user
	user, err := store.GetUser(ctx, tenantID, userID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, user)
}

// UpdateUser handles PUT /api/v1/tenants/{tenant_id}/users/{user_id} - Update a user.
func (h *IAMHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user ID is required"))
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
		Email      *string                 `json:"email,omitempty"`
		Username   *string                 `json:"username,omitempty"`
		Name       *string                 `json:"name,omitempty"`
		Attributes *map[string]interface{} `json:"attributes,omitempty"`
		Enabled    *bool                   `json:"enabled,omitempty"`
		Roles      *[]string               `json:"roles,omitempty"`
		Groups     *[]string               `json:"groups,omitempty"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Get identity store
	identityStore, ok := h.server.service.(interface {
		GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}

	store, err := identityStore.GetIdentityStore(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if store == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not initialized"))
		return
	}

	// Update user
	opts := iam.UpdateUserOptions{
		Email:      req.Email,
		Username:   req.Username,
		Name:       req.Name,
		Attributes: req.Attributes,
		Enabled:    req.Enabled,
		Roles:      req.Roles,
		Groups:     req.Groups,
	}

	user, err := store.UpdateUser(ctx, tenantID, userID, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, user)
}

// DeleteUser handles DELETE /api/v1/tenants/{tenant_id}/users/{user_id} - Delete a user.
func (h *IAMHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user ID is required"))
		return
	}

	// Get identity store
	identityStore, ok := h.server.service.(interface {
		GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}

	store, err := identityStore.GetIdentityStore(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if store == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not initialized"))
		return
	}

	// Delete user
	if err := store.DeleteUser(ctx, tenantID, userID); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondNoContent(w, r)
}

// AssignRole handles POST /api/v1/tenants/{tenant_id}/users/{user_id}/roles - Assign a role to a user.
func (h *IAMHandler) AssignRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user ID is required"))
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
		RoleID string `json:"role_id"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	if req.RoleID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "role_id is required"))
		return
	}

	// Get identity store
	identityStore, ok := h.server.service.(interface {
		GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}

	store, err := identityStore.GetIdentityStore(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if store == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not initialized"))
		return
	}

	// Assign role
	if err := store.AssignRole(ctx, tenantID, userID, req.RoleID); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondNoContent(w, r)
}

// RevokeRole handles DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/roles/{role} - Revoke a role from a user.
func (h *IAMHandler) RevokeRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user ID is required"))
		return
	}

	roleID := chi.URLParam(r, "role")
	if roleID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "role ID is required"))
		return
	}

	// Get identity store
	identityStore, ok := h.server.service.(interface {
		GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}

	store, err := identityStore.GetIdentityStore(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if store == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not initialized"))
		return
	}

	// Revoke role
	if err := store.RevokeRole(ctx, tenantID, userID, roleID); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondNoContent(w, r)
}
