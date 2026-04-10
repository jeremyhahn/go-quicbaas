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
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// ListTenants handles GET /api/v1/tenants - List all tenants.
func (h *IAMHandler) ListTenants(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

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

	// Get tenant manager from service
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
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "tenant manager not initialized"))
		return
	}

	// List tenants
	opts := iam.ListTenantsOptions{
		Limit:  limit,
		Offset: offset,
	}

	tenants, err := mgr.ListTenants(ctx, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := map[string]interface{}{
		"tenants": tenants,
		"limit":   limit,
		"offset":  offset,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// CreateTenant handles POST /api/v1/tenants - Create a new tenant.
func (h *IAMHandler) CreateTenant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

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
		ID       string              `json:"id,omitempty"`
		Name     string              `json:"name"`
		Settings *iam.TenantSettings `json:"settings,omitempty"`
		Quotas   *iam.TenantQuotas   `json:"quotas,omitempty"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Validate required fields
	if req.ID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}
	if req.Name == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant name is required"))
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
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "tenant manager not initialized"))
		return
	}

	// Create tenant
	opts := iam.CreateTenantOptions{
		ID:       req.ID,
		Name:     req.Name,
		Settings: req.Settings,
		Quotas:   req.Quotas,
	}

	tenant, err := mgr.CreateTenant(ctx, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Log audit event
	h.logAuditEvent(ctx, tenant.ID,
		iam.AuditEventTenantEvent,
		iam.AuditCategoryAdministration,
		"create",
		iam.AuditOutcomeSuccess,
		&iam.AuditResource{Type: "tenant", ID: tenant.ID, Name: tenant.Name},
		map[string]interface{}{"name": tenant.Name},
	)

	RespondJSON(w, r, http.StatusCreated, tenant)
}

// GetTenant handles GET /api/v1/tenants/{tenant_id} - Get a specific tenant.
func (h *IAMHandler) GetTenant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

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
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "tenant manager not initialized"))
		return
	}

	// Get tenant
	tenant, err := mgr.GetTenant(ctx, tenantID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, tenant)
}

// UpdateTenant handles PUT /api/v1/tenants/{tenant_id} - Update a tenant.
func (h *IAMHandler) UpdateTenant(w http.ResponseWriter, r *http.Request) {
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
		Name     *string             `json:"name,omitempty"`
		Settings *iam.TenantSettings `json:"settings,omitempty"`
		Quotas   *iam.TenantQuotas   `json:"quotas,omitempty"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
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
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "tenant manager not initialized"))
		return
	}

	// Update tenant
	opts := iam.UpdateTenantOptions{
		Name:     req.Name,
		Settings: req.Settings,
		Quotas:   req.Quotas,
	}

	tenant, err := mgr.UpdateTenant(ctx, tenantID, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, tenant)
}

// DeleteTenant handles DELETE /api/v1/tenants/{tenant_id} - Delete a tenant.
func (h *IAMHandler) DeleteTenant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

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
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "tenant manager not initialized"))
		return
	}

	// Delete tenant
	if err := mgr.DeleteTenant(ctx, tenantID); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondNoContent(w, r)
}
