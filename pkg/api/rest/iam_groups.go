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

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// ListGroups handles GET /api/v1/tenants/{tenant_id}/groups - List groups in a tenant.
func (h *IAMHandler) ListGroups(w http.ResponseWriter, r *http.Request) {
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

	// List groups
	opts := iam.ListGroupsOptions{
		Limit:  limit,
		Offset: offset,
	}

	groups, err := store.ListGroups(ctx, tenantID, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := map[string]interface{}{
		"groups": groups,
		"limit":  limit,
		"offset": offset,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// CreateGroup handles POST /api/v1/tenants/{tenant_id}/groups - Create a new group.
func (h *IAMHandler) CreateGroup(w http.ResponseWriter, r *http.Request) {
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
		ID          string                 `json:"id,omitempty"`
		Name        string                 `json:"name"`
		Description string                 `json:"description,omitempty"`
		Attributes  map[string]interface{} `json:"attributes,omitempty"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Validate required fields
	if req.Name == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "group name is required"))
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

	// Create group
	opts := iam.CreateGroupOptions{
		ID:          req.ID,
		Name:        req.Name,
		Description: req.Description,
		Attributes:  req.Attributes,
	}

	group, err := store.CreateGroup(ctx, tenantID, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Log audit event
	h.logAuditEvent(ctx, tenantID,
		iam.AuditEventGroupManagement,
		iam.AuditCategoryAdministration,
		"create",
		iam.AuditOutcomeSuccess,
		&iam.AuditResource{Type: "group", ID: group.ID, Name: group.Name},
		map[string]interface{}{"name": group.Name},
	)

	RespondJSON(w, r, http.StatusCreated, group)
}

// GetGroup handles GET /api/v1/tenants/{tenant_id}/groups/{group_id} - Get a specific group.
func (h *IAMHandler) GetGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	groupID := chi.URLParam(r, "group_id")
	if groupID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "group ID is required"))
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

	// Get group
	group, err := store.GetGroup(ctx, tenantID, groupID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, group)
}

// UpdateGroup handles PUT /api/v1/tenants/{tenant_id}/groups/{group_id} - Update a group.
func (h *IAMHandler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	groupID := chi.URLParam(r, "group_id")
	if groupID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "group ID is required"))
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
		Name        *string                 `json:"name,omitempty"`
		Description *string                 `json:"description,omitempty"`
		Attributes  *map[string]interface{} `json:"attributes,omitempty"`
		Members     *[]string               `json:"members,omitempty"`
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

	// Update group
	opts := iam.UpdateGroupOptions{
		Name:        req.Name,
		Description: req.Description,
		Attributes:  req.Attributes,
		Members:     req.Members,
	}

	group, err := store.UpdateGroup(ctx, tenantID, groupID, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, group)
}

// DeleteGroup handles DELETE /api/v1/tenants/{tenant_id}/groups/{group_id} - Delete a group.
func (h *IAMHandler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	groupID := chi.URLParam(r, "group_id")
	if groupID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "group ID is required"))
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

	// Delete group
	if err := store.DeleteGroup(ctx, tenantID, groupID); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondNoContent(w, r)
}

// AddMember handles POST /api/v1/tenants/{tenant_id}/groups/{group_id}/members - Add a user to a group.
func (h *IAMHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	groupID := chi.URLParam(r, "group_id")
	if groupID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "group ID is required"))
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

	// Add member
	if err := store.AddUserToGroup(ctx, tenantID, groupID, req.UserID); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondNoContent(w, r)
}

// RemoveMember handles DELETE /api/v1/tenants/{tenant_id}/groups/{group_id}/members/{user_id} - Remove a user from a group.
func (h *IAMHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	groupID := chi.URLParam(r, "group_id")
	if groupID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "group ID is required"))
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

	// Remove member
	if err := store.RemoveUserFromGroup(ctx, tenantID, groupID, userID); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondNoContent(w, r)
}

// ListMembers handles GET /api/v1/tenants/{tenant_id}/groups/{group_id}/members - List members of a group.
func (h *IAMHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	groupID := chi.URLParam(r, "group_id")
	if groupID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "group ID is required"))
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

	// Get group to retrieve its members
	group, err := store.GetGroup(ctx, tenantID, groupID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Build response with members list
	members := group.Members
	if members == nil {
		members = []string{}
	}

	resp := map[string]interface{}{
		"members": members,
		"total":   len(members),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}
