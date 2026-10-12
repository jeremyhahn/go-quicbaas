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
	"errors"
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
	// There is deliberately no "id": the identity is the Snowflake the store
	// mints, so an id supplied here could only name a record in some other
	// system. A caller that sent one used to get it back and then find nothing
	// under it, because the store minted its own anyway.
	var req struct {
		Email             string            `json:"email"`
		Username          string            `json:"username,omitempty"`
		Name              string            `json:"name,omitempty"`
		Attributes        map[string]string `json:"attributes,omitempty"`
		Enabled           *bool             `json:"enabled,omitempty"`
		AuthMethods       []string          `json:"auth_methods,omitempty"`
		Roles             []string          `json:"roles,omitempty"`
		Lifecycle         iam.Lifecycle     `json:"lifecycle,omitempty"`
		InviteExpiryHours int               `json:"invite_expiry_hours,omitempty"`
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

	// User quota (max users per tenant) is enforced by qrdb's TenantServicer.
	// qrdb will return a quota-exceeded error if the limit is reached; the
	// service layer propagates that as a 403 Forbidden through the normal error
	// path below. No BaaS-local count is maintained here.
	opts := iam.CreateUserOptions{
		Email:      req.Email,
		Username:   req.Username,
		Name:       req.Name,
		Attributes: req.Attributes,
		// A pointer so an omitted "enabled" means "unspecified", not false.
		// As a plain bool it meant every create without the field explicitly
		// set would have suspended the user the moment CreateUser started
		// honouring it.
		Enabled: req.Enabled,
		// Roles are validated against the tenant's role store by
		// IdentityStore.CreateUser BEFORE the user record is written
		// (validate-before-create), matching the gRPC/Unix adapter contract.
		Roles:       req.Roles,
		AuthMethods: req.AuthMethods,
		Lifecycle:   req.Lifecycle,
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

	// A pending account is activated through an invite, so the invite store is
	// resolved before anything is written: failing here leaves nothing behind.
	var inviteStore iam.InviteStore
	if opts.EffectiveLifecycle() == iam.LifecyclePending {
		inviteStore, err = h.getInviteStore(ctx)
		if err != nil {
			h.server.handleServiceError(w, r, err)
			return
		}
	}

	// The user, its invite and the rollback of a create that cannot complete
	// all live in the service layer, shared with every other transport. The
	// token is minted by go-qrdb and returned exactly once, here.
	inviteExpiry := time.Duration(req.InviteExpiryHours) * time.Hour
	user, inviteToken, invite, err := store.CreateInvitedUser(ctx, tenantID, opts, inviteStore, inviteExpiry)
	if err != nil {
		var rollbackErr *iam.UserRollbackError
		if errors.As(err, &rollbackErr) {
			h.logAuditEvent(ctx, tenantID, iam.AuditEventUserManagement,
				iam.AuditCategorySecurity, "create.rollback", iam.AuditOutcomeFailure,
				&iam.AuditResource{Type: "user", ID: rollbackErr.UserID},
				map[string]interface{}{"error": rollbackErr.Rollback.Error()})
		}
		h.server.handleServiceError(w, r, err)
		return
	}

	// An account that is not pending has nothing to activate, so it gets no
	// invite and the response carries no token.
	if inviteToken == "" {
		h.logAuditEvent(ctx, tenantID,
			iam.AuditEventUserManagement,
			iam.AuditCategoryAdministration,
			"create",
			iam.AuditOutcomeSuccess,
			&iam.AuditResource{Type: "user", ID: user.ID, Name: user.Email},
			map[string]interface{}{"email": user.Email, "lifecycle": string(user.Lifecycle)},
		)
		RespondJSON(w, r, http.StatusCreated, user)
		return
	}

	// HOST COMES FROM THE REQUEST, so an admin must create invites via the host
	// the invitee will use. WebAuthn refuses a ceremony whose origin does not
	// match the configured RP ID, so an invite minted through 127.0.0.1 yields a
	// link that loads and then fails at the key. The REST layer cannot see the
	// IAM WebAuthn config to override this; until it can, the constraint is
	// stated here rather than left to be discovered at the authenticator.
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = fwd
	}
	inviteURL := iam.InviteURL(scheme+"://"+host, tenantID, inviteToken)

	// Log audit event
	h.logAuditEvent(ctx, tenantID,
		iam.AuditEventUserManagement,
		iam.AuditCategoryAdministration,
		"create",
		iam.AuditOutcomeSuccess,
		&iam.AuditResource{Type: "user", ID: user.ID, Name: user.Email},
		// The STORED methods, not the requested ones: an audit line that
		// reports the request cannot be used to answer what the account may
		// actually present.
		map[string]interface{}{"email": user.Email, "auth_methods": user.AuthMethods},
	)

	// Return the canonical user object (matches the shape returned by GetUser/
	// UpdateUser/ListUsers) with the invite appended. The invite fields come
	// from the invite store, which owns them; the user record carries neither.
	RespondJSON(w, r, http.StatusCreated, struct {
		*iam.User
		InviteToken     string    `json:"invite_token,omitempty"`
		InviteExpiresAt time.Time `json:"invite_expires_at,omitempty"`
		InviteURL       string    `json:"invite_url,omitempty"`
	}{
		User:            user,
		InviteToken:     inviteToken,
		InviteExpiresAt: invite.ExpiresAt,
		InviteURL:       inviteURL,
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
		Email       *string            `json:"email,omitempty"`
		Username    *string            `json:"username,omitempty"`
		Name        *string            `json:"name,omitempty"`
		Attributes  *map[string]string `json:"attributes,omitempty"`
		Enabled     *bool              `json:"enabled,omitempty"`
		Roles       *[]string          `json:"roles,omitempty"`
		AuthMethods *[]string          `json:"auth_methods,omitempty"`
		Lifecycle   *iam.Lifecycle     `json:"lifecycle,omitempty"`
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
		// A pointer so an omitted "enabled" means "unspecified", not false.
		// As a plain bool it meant every create without the field explicitly
		// set would have suspended the user the moment CreateUser started
		// honouring it.
		Enabled: req.Enabled,
		// Forwarded, so a caller that sends them observes the change. Accepting
		// an account-state field and answering 200 with nothing changed is the
		// fail-open shape: the caller believes the account was disabled.
		AuthMethods: req.AuthMethods,
		Lifecycle:   req.Lifecycle,
		Roles:       req.Roles,
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
