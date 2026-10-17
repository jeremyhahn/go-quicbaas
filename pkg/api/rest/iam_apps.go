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
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ListApps handles GET /api/v1/tenants/{tenant_id}/apps - List OAuth applications in a tenant.
func (h *IAMHandler) ListApps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}
	tenantID, ok := h.server.urlTenant(w, r, "rest.IAM", "tenant_id")
	if !ok {
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

	// Get app manager from service
	appMgr, ok := h.server.service.(interface {
		GetAppManager(ctx context.Context) (*iam.AppManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "app manager not available"))
		return
	}

	mgr, err := appMgr.GetAppManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "app manager not initialized"))
		return
	}

	// List apps
	opts := iam.ListAppsOptions{
		Limit:  limit,
		Offset: offset,
	}

	apps, err := mgr.ListApps(ctx, tenantID, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := map[string]interface{}{
		"applications": apps,
		"apps":         apps,
		"limit":        limit,
		"offset":       offset,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// RegisterApp handles POST /api/v1/tenants/{tenant_id}/apps - Register a new OAuth application.
func (h *IAMHandler) RegisterApp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}
	tenantID, ok := h.server.urlTenant(w, r, "rest.IAM", "tenant_id")
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

	// Parse request
	var req struct {
		Name           string   `json:"name"`
		RedirectURIs   []string `json:"redirect_uris"`
		ResourcePrefix string   `json:"resource_prefix,omitempty"`
		GrantTypes     []string `json:"grant_types,omitempty"`
		ResponseTypes  []string `json:"response_types,omitempty"`
		PKCERequired   bool     `json:"pkce_required"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Validate required fields
	if req.Name == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "application name is required"))
		return
	}

	if len(req.RedirectURIs) == 0 {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "at least one redirect URI is required"))
		return
	}

	// Get app manager
	appMgr, ok := h.server.service.(interface {
		GetAppManager(ctx context.Context) (*iam.AppManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "app manager not available"))
		return
	}

	mgr, err := appMgr.GetAppManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "app manager not initialized"))
		return
	}

	// Register app
	opts := iam.RegisterAppOptions{
		Name:           req.Name,
		RedirectURIs:   req.RedirectURIs,
		ResourcePrefix: req.ResourcePrefix,
		GrantTypes:     req.GrantTypes,
		ResponseTypes:  req.ResponseTypes,
		PKCERequired:   req.PKCERequired,
	}

	app, err := mgr.RegisterApp(ctx, tenantID, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusCreated, app)
}

// GetApp handles GET /api/v1/tenants/{tenant_id}/apps/{client_id} - Get a specific OAuth application.
func (h *IAMHandler) GetApp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}
	callerTenant, ok := h.server.urlTenant(w, r, "rest.IAM", "tenant_id")
	if !ok {
		return
	}

	clientID := chi.URLParam(r, "client_id")
	if clientID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "client ID is required"))
		return
	}

	// Get app manager
	appMgr, ok := h.server.service.(interface {
		GetAppManager(ctx context.Context) (*iam.AppManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "app manager not available"))
		return
	}

	mgr, err := appMgr.GetAppManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "app manager not initialized"))
		return
	}

	// Get app
	app, err := mgr.GetAppInTenant(ctx, callerTenant, clientID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, app)
}

// UpdateApp handles PUT /api/v1/tenants/{tenant_id}/apps/{client_id} - Update an OAuth application.
func (h *IAMHandler) UpdateApp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}
	callerTenant, ok := h.server.urlTenant(w, r, "rest.IAM", "tenant_id")
	if !ok {
		return
	}

	clientID := chi.URLParam(r, "client_id")
	if clientID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "client ID is required"))
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

	// Parse request: every field the SDK sends is carried to the app manager,
	// which applies it or refuses it; none is dropped here.
	var req service.UpdateAppRequest

	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Get app manager
	appMgr, ok := h.server.service.(interface {
		GetAppManager(ctx context.Context) (*iam.AppManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "app manager not available"))
		return
	}

	mgr, err := appMgr.GetAppManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "app manager not initialized"))
		return
	}

	// Update app
	app, err := mgr.UpdateAppInTenant(ctx, callerTenant, clientID, req.Options())
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, app)
}

// DeleteApp handles DELETE /api/v1/tenants/{tenant_id}/apps/{client_id} - Delete an OAuth application.
func (h *IAMHandler) DeleteApp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}
	callerTenant, ok := h.server.urlTenant(w, r, "rest.IAM", "tenant_id")
	if !ok {
		return
	}

	clientID := chi.URLParam(r, "client_id")
	if clientID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "client ID is required"))
		return
	}

	// Get app manager
	appMgr, ok := h.server.service.(interface {
		GetAppManager(ctx context.Context) (*iam.AppManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "app manager not available"))
		return
	}

	mgr, err := appMgr.GetAppManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "app manager not initialized"))
		return
	}

	// Delete app
	if err := mgr.DeleteAppInTenant(ctx, callerTenant, clientID); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondNoContent(w, r)
}

// RotateSecret handles POST /api/v1/tenants/{tenant_id}/apps/{client_id}/rotate-secret - Rotate OAuth application client secret.
func (h *IAMHandler) RotateSecret(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}
	callerTenant, ok := h.server.urlTenant(w, r, "rest.IAM", "tenant_id")
	if !ok {
		return
	}

	clientID := chi.URLParam(r, "client_id")
	if clientID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "client ID is required"))
		return
	}

	// Get app manager
	appMgr, ok := h.server.service.(interface {
		GetAppManager(ctx context.Context) (*iam.AppManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "app manager not available"))
		return
	}

	mgr, err := appMgr.GetAppManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "app manager not initialized"))
		return
	}

	// Rotate secret
	app, err := mgr.RotateSecretInTenant(ctx, callerTenant, clientID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, app)
}

// ValidateClient handles POST /api/v1/iam/validate-client - Validate OAuth2 client credentials.
func (h *IAMHandler) ValidateClient(w http.ResponseWriter, r *http.Request) {
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
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	if req.ClientID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "client_id is required"))
		return
	}

	if req.ClientSecret == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "client_secret is required"))
		return
	}

	// Get app manager
	appMgr, ok := h.server.service.(interface {
		GetAppManager(ctx context.Context) (*iam.AppManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "app manager not available"))
		return
	}

	mgr, err := appMgr.GetAppManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "app manager not initialized"))
		return
	}

	// Validate client credentials
	valid, err := mgr.ValidateClientCredentials(ctx, req.ClientID, req.ClientSecret)
	if err != nil || !valid {
		// Return 401 for invalid credentials (don't reveal why it failed for security)
		RespondIAMError(w, r, http.StatusUnauthorized,
			NewIAMAPIError(ErrUnauthorized, "invalid client credentials"))
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]bool{"valid": true})
}
