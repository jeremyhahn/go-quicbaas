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
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
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
	// IsolationType, EngineQuotas and EngineEntitlements are engine-side
	// provisioning knobs that can only be chosen while the tenant is being
	// created — the engine binds isolation at creation, and applying quotas
	// in the same call avoids a window where a new tenant runs on defaults.
	// All three are optional; omitting them defers to the engine's defaults.
	//
	// The two engine objects are held as raw JSON and decoded strictly below.
	// Their ABSENCE means "keep the engine's configured defaults", so a
	// mistyped key would otherwise decode to an empty override and quietly
	// replace those defaults with an all-defaults, all-denied set.
	var req struct {
		ID                 string                      `json:"id,omitempty"`
		Name               string                      `json:"name"`
		Settings           *iam.TenantSettings         `json:"settings,omitempty"`
		Quotas             *iam.TenantQuotas           `json:"quotas,omitempty"`
		IsolationType      qrdbsdk.TenantIsolationType `json:"isolation_type,omitempty"`
		EngineQuotas       json.RawMessage             `json:"engine_quotas,omitempty"`
		EngineEntitlements json.RawMessage             `json:"engine_entitlements,omitempty"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Decoded into the SDK's limits-only view, so the engine-owned current_*
	// counters are rejected as unknown fields: the gRPC create request models
	// limits only, and accepting them here would give the same request a
	// different meaning per transport.
	var engineQuotas *qrdbsdk.Quotas
	if jsonObjectPresent(req.EngineQuotas) {
		limits := &iamsdk.EngineQuotas{}
		if err := decodeStrictJSON(req.EngineQuotas, limits); err != nil {
			RespondIAMError(w, r, http.StatusBadRequest,
				NewIAMAPIError(ErrBadRequest, "invalid engine_quotas: "+err.Error()))
			return
		}
		engineQuotas = limits.Quotas()
	}

	var engineEntitlements *qrdbsdk.Entitlements
	if jsonObjectPresent(req.EngineEntitlements) {
		engineEntitlements = &qrdbsdk.Entitlements{}
		if err := decodeStrictJSON(req.EngineEntitlements, engineEntitlements); err != nil {
			RespondIAMError(w, r, http.StatusBadRequest,
				NewIAMAPIError(ErrBadRequest, "invalid engine_entitlements: "+err.Error()))
			return
		}
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
		ID:                 req.ID,
		Name:               req.Name,
		Settings:           req.Settings,
		Quotas:             req.Quotas,
		IsolationType:      req.IsolationType,
		EngineQuotas:       engineQuotas,
		EngineEntitlements: engineEntitlements,
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

// UpdateTenantEngineQuotas handles PATCH /api/v1/tenants/{tenant_id}/quotas
// - Update the engine-level resource quotas for a tenant.
func (h *IAMHandler) UpdateTenantEngineQuotas(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	var quotas qrdbsdk.Quotas
	if err := json.Unmarshal(body, &quotas); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	mgr, ok := h.requireTenantManager(w, r, ctx)
	if !ok {
		return
	}

	if err := mgr.UpdateEngineQuotas(ctx, tenantID, quotas); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondNoContent(w, r)
}

// UpdateTenantEngineEntitlements handles PATCH /api/v1/tenants/{tenant_id}/entitlements
// - Update the engine-level feature entitlements for a tenant.
func (h *IAMHandler) UpdateTenantEngineEntitlements(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	var entitlements qrdbsdk.Entitlements
	if err := json.Unmarshal(body, &entitlements); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	mgr, ok := h.requireTenantManager(w, r, ctx)
	if !ok {
		return
	}

	if err := mgr.UpdateEngineEntitlements(ctx, tenantID, entitlements); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondNoContent(w, r)
}

// requireTenantManager is a shared helper that resolves the TenantManager from
// the service and writes the appropriate error response on failure. Returns the
// manager and true on success, or (nil, false) when a response has already been
// written.
func (h *IAMHandler) requireTenantManager(w http.ResponseWriter, r *http.Request, ctx context.Context) (TenantManager, bool) {
	tenantMgr, ok := h.server.service.(interface {
		GetTenantManager(ctx context.Context) (TenantManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "tenant manager not available"))
		return nil, false
	}
	mgr, err := tenantMgr.GetTenantManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return nil, false
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "tenant manager not initialized"))
		return nil, false
	}
	return mgr, true
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

// jsonObjectPresent reports whether raw carries an actual object. A literal
// JSON null is the caller spelling "not supplied": decoding it would produce a
// non-nil zero override and cross the nil-vs-empty boundary, turning "keep the
// engine's configured defaults" into "apply an all-defaults, all-denied
// override".
func jsonObjectPresent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && string(trimmed) != "null"
}

// decodeStrictJSON decodes raw into dst rejecting unknown fields. It is used
// for the engine provisioning objects, where an unrecognized key must be an
// error rather than a silently empty override — the distinction between "no
// object" (keep engine defaults) and "empty object" (apply an all-defaults,
// all-denied override) is load-bearing, so a typo must not cross it.
func decodeStrictJSON(raw json.RawMessage, dst interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}
