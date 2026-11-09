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

// ==================== Consent Management ====================

// GrantConsent handles POST /api/v1/tenants/{tenant_id}/users/{user_id}/consents - Grant consent.
func (h *IAMHandler) GrantConsent(w http.ResponseWriter, r *http.Request) {
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

	// Parse request
	var req struct {
		Purpose   iam.ConsentPurpose `json:"purpose"`
		Version   string             `json:"version"`
		ExpiresAt *time.Time         `json:"expires_at,omitempty"` // RFC3339 timestamp
		IPAddress string             `json:"ip_address,omitempty"`
		UserAgent string             `json:"user_agent,omitempty"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Validate required fields
	if req.Purpose == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "purpose is required"))
		return
	}
	if req.Version == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "version is required"))
		return
	}

	// Get GDPR manager
	gdprMgr, ok := h.server.service.(interface {
		GetGDPRManager(ctx context.Context) (*iam.GDPRManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not available"))
		return
	}

	manager, err := gdprMgr.GetGDPRManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if manager == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not initialized"))
		return
	}

	// Check if consent already exists to determine response status code
	existingConsent, _ := manager.GetConsent(ctx, tenantID, userID, req.Purpose)
	isUpdate := existingConsent != nil

	// Grant consent
	expiresAt := req.ExpiresAt

	consent, err := manager.GrantConsent(ctx, tenantID, userID, req.Purpose, req.Version, expiresAt, req.IPAddress, req.UserAgent)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Return 200 OK for updates, 201 Created for new consents
	if isUpdate {
		RespondJSON(w, r, http.StatusOK, consent)
	} else {
		RespondJSON(w, r, http.StatusCreated, consent)
	}
}

// ListUserConsents handles GET /api/v1/tenants/{tenant_id}/users/{user_id}/consents - List user consents.
func (h *IAMHandler) ListUserConsents(w http.ResponseWriter, r *http.Request) {
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

	// Get GDPR manager
	gdprMgr, ok := h.server.service.(interface {
		GetGDPRManager(ctx context.Context) (*iam.GDPRManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not available"))
		return
	}

	manager, err := gdprMgr.GetGDPRManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if manager == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not initialized"))
		return
	}

	// List consents
	consents, err := manager.ListUserConsents(ctx, tenantID, userID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Return array directly for RESTful list endpoints
	RespondJSON(w, r, http.StatusOK, consents)
}

// GetConsent handles GET /api/v1/tenants/{tenant_id}/users/{user_id}/consents/{purpose} - Get specific consent.
func (h *IAMHandler) GetConsent(w http.ResponseWriter, r *http.Request) {
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

	purpose := iam.ConsentPurpose(chi.URLParam(r, "purpose"))
	if purpose == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "purpose is required"))
		return
	}

	// Get GDPR manager
	gdprMgr, ok := h.server.service.(interface {
		GetGDPRManager(ctx context.Context) (*iam.GDPRManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not available"))
		return
	}

	manager, err := gdprMgr.GetGDPRManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if manager == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not initialized"))
		return
	}

	// Get consent
	consent, err := manager.GetConsent(ctx, tenantID, userID, purpose)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, consent)
}

// WithdrawConsent handles PUT /api/v1/tenants/{tenant_id}/users/{user_id}/consents/{purpose}/withdraw - Withdraw consent.
func (h *IAMHandler) WithdrawConsent(w http.ResponseWriter, r *http.Request) {
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

	purpose := iam.ConsentPurpose(chi.URLParam(r, "purpose"))
	if purpose == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "purpose is required"))
		return
	}

	// Read request body (optional IP/UserAgent)
	var req struct {
		IPAddress string `json:"ip_address,omitempty"`
		UserAgent string `json:"user_agent,omitempty"`
	}
	if r.ContentLength > 0 {
		body, err := io.ReadAll(r.Body)
		if err == nil {
			_ = json.Unmarshal(body, &req)
		}
		defer r.Body.Close()
	}

	// Get GDPR manager
	gdprMgr, ok := h.server.service.(interface {
		GetGDPRManager(ctx context.Context) (*iam.GDPRManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not available"))
		return
	}

	manager, err := gdprMgr.GetGDPRManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if manager == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not initialized"))
		return
	}

	// Withdraw consent
	if err := manager.WithdrawConsent(ctx, tenantID, userID, purpose, req.IPAddress, req.UserAgent); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Get the updated consent to return
	consent, err := manager.GetConsent(ctx, tenantID, userID, purpose)
	if err != nil {
		// Consent was withdrawn but we couldn't retrieve it - return minimal response
		RespondJSON(w, r, http.StatusOK, map[string]interface{}{
			"status": "withdrawn",
		})
		return
	}

	RespondJSON(w, r, http.StatusOK, consent)
}

// ==================== Data Export ====================

// RequestDataExport handles POST /api/v1/tenants/{tenant_id}/users/{user_id}/export - Request data export.
func (h *IAMHandler) RequestDataExport(w http.ResponseWriter, r *http.Request) {
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

	// Parse request
	var req struct {
		Format            iam.DataExportFormat `json:"format"`
		RequestedBy       string               `json:"requested_by"`
		IncludeCategories []string             `json:"include_categories,omitempty"`
		ExcludeCategories []string             `json:"exclude_categories,omitempty"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Default format to JSON if not specified
	if req.Format == "" {
		req.Format = iam.DataExportFormatJSON
	}

	// Default requested_by to user if not specified
	requestedBy := userID
	if req.RequestedBy != "" {
		id, err := parseUserIDParam(req.RequestedBy)
		if err != nil {
			RespondIAMError(w, r, http.StatusBadRequest,
				NewIAMAPIError(ErrBadRequest, err.Error()))
			return
		}
		requestedBy = id
	}

	// Get GDPR manager
	gdprMgr, ok := h.server.service.(interface {
		GetGDPRManager(ctx context.Context) (*iam.GDPRManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not available"))
		return
	}

	manager, err := gdprMgr.GetGDPRManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if manager == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not initialized"))
		return
	}

	// Request export
	exportReq, err := manager.RequestExport(ctx, tenantID, userID, requestedBy, req.Format, req.IncludeCategories, req.ExcludeCategories)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusAccepted, exportReq)
}

// GetExportStatus handles GET /api/v1/tenants/{tenant_id}/users/{user_id}/export/{export_id} - Get export status.
func (h *IAMHandler) GetExportStatus(w http.ResponseWriter, r *http.Request) {
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

	exportID := chi.URLParam(r, "export_id")
	if exportID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "export ID is required"))
		return
	}

	// Get GDPR manager
	gdprMgr, ok := h.server.service.(interface {
		GetGDPRManager(ctx context.Context) (*iam.GDPRManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not available"))
		return
	}

	manager, err := gdprMgr.GetGDPRManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if manager == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not initialized"))
		return
	}

	// Get export status
	exportReq, err := manager.GetExportStatus(ctx, exportID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Verify the export belongs to the requested user
	if exportReq.UserID != userID || exportReq.TenantID != tenantID {
		RespondIAMError(w, r, http.StatusNotFound,
			NewIAMAPIError(ErrNotFound, "export not found"))
		return
	}

	// DownloadURL is a relative path - clients should construct the full URL
	// using their known endpoint
	RespondJSON(w, r, http.StatusOK, exportReq)
}

// DownloadExport handles GET /api/v1/tenants/{tenant_id}/users/{user_id}/export/{export_id}/download - Download export.
func (h *IAMHandler) DownloadExport(w http.ResponseWriter, r *http.Request) {
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

	exportID := chi.URLParam(r, "export_id")
	if exportID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "export ID is required"))
		return
	}

	// Get GDPR manager
	gdprMgr, ok := h.server.service.(interface {
		GetGDPRManager(ctx context.Context) (*iam.GDPRManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not available"))
		return
	}

	manager, err := gdprMgr.GetGDPRManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if manager == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not initialized"))
		return
	}

	// Get export status
	exportReq, err := manager.GetExportStatus(ctx, exportID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Verify the export belongs to the requested user
	if exportReq.UserID != userID || exportReq.TenantID != tenantID {
		RespondIAMError(w, r, http.StatusNotFound,
			NewIAMAPIError(ErrNotFound, "export not found"))
		return
	}

	// Check if export is completed
	if exportReq.Status != iam.DataExportStatusCompleted {
		RespondIAMError(w, r, http.StatusConflict,
			NewIAMAPIError(ErrConflict, "export is not ready for download"))
		return
	}

	// Check if export has expired
	if exportReq.Status == iam.DataExportStatusExpired {
		RespondIAMError(w, r, http.StatusGone,
			NewIAMAPIError(ErrNotFound, "export has expired"))
		return
	}

	// Generate the export data
	exportData, err := manager.GenerateExport(ctx, exportID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// The generator writes JSON only, so the download is labelled JSON
	// whatever format the stored request names (#1724).
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="user_data_export.json"`)
	w.WriteHeader(http.StatusOK)

	// Write export data. The header is already written, so a stream failure
	// here has no channel left to report it on.
	_ = json.NewEncoder(w).Encode(exportData)
}

// ==================== Data Deletion ====================

// RequestDeletion handles POST /api/v1/tenants/{tenant_id}/users/{user_id}/deletion - Request data deletion.
func (h *IAMHandler) RequestDeletion(w http.ResponseWriter, r *http.Request) {
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

	// Parse request
	var req struct {
		RequestedBy        string   `json:"requested_by"`
		Reason             string   `json:"reason,omitempty"`
		DeleteCategories   []string `json:"delete_categories,omitempty"`
		PreserveCategories []string `json:"preserve_categories,omitempty"`
		GracePeriodDays    int      `json:"grace_period_days,omitempty"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Default requested_by to user if not specified
	requestedBy := userID
	if req.RequestedBy != "" {
		id, err := parseUserIDParam(req.RequestedBy)
		if err != nil {
			RespondIAMError(w, r, http.StatusBadRequest,
				NewIAMAPIError(ErrBadRequest, err.Error()))
			return
		}
		requestedBy = id
	}

	// Get GDPR manager
	gdprMgr, ok := h.server.service.(interface {
		GetGDPRManager(ctx context.Context) (*iam.GDPRManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not available"))
		return
	}

	manager, err := gdprMgr.GetGDPRManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if manager == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not initialized"))
		return
	}

	// Request deletion
	deletionReq, err := manager.RequestDeletion(ctx, tenantID, userID, requestedBy, req.Reason, req.DeleteCategories, req.PreserveCategories, req.GracePeriodDays)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusAccepted, deletionReq)
}

// GetDeletionStatus handles GET /api/v1/tenants/{tenant_id}/users/{user_id}/deletion/{deletion_id} - Get deletion status.
func (h *IAMHandler) GetDeletionStatus(w http.ResponseWriter, r *http.Request) {
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

	deletionID := chi.URLParam(r, "deletion_id")
	if deletionID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "deletion ID is required"))
		return
	}

	// Get GDPR manager
	gdprMgr, ok := h.server.service.(interface {
		GetGDPRManager(ctx context.Context) (*iam.GDPRManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not available"))
		return
	}

	manager, err := gdprMgr.GetGDPRManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if manager == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not initialized"))
		return
	}

	// Get deletion status
	deletionReq, err := manager.GetDeletionStatus(ctx, deletionID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Verify the deletion belongs to the requested user
	if deletionReq.UserID != userID || deletionReq.TenantID != tenantID {
		RespondIAMError(w, r, http.StatusNotFound,
			NewIAMAPIError(ErrNotFound, "deletion request not found"))
		return
	}

	RespondJSON(w, r, http.StatusOK, deletionReq)
}

// CancelDeletion handles DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/deletion/{deletion_id} - Cancel deletion.
func (h *IAMHandler) CancelDeletion(w http.ResponseWriter, r *http.Request) {
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

	deletionID := chi.URLParam(r, "deletion_id")
	if deletionID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "deletion ID is required"))
		return
	}

	// Get cancelled_by from query parameter or default to user
	cancelledBy := r.URL.Query().Get("cancelled_by")
	if cancelledBy == "" {
		cancelledBy = strconv.FormatUint(userID, 10)
	}

	// Get GDPR manager
	gdprMgr, ok := h.server.service.(interface {
		GetGDPRManager(ctx context.Context) (*iam.GDPRManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not available"))
		return
	}

	manager, err := gdprMgr.GetGDPRManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if manager == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "GDPR manager not initialized"))
		return
	}

	// First verify the deletion belongs to the requested user
	deletionReq, err := manager.GetDeletionStatus(ctx, deletionID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	if deletionReq.UserID != userID || deletionReq.TenantID != tenantID {
		RespondIAMError(w, r, http.StatusNotFound,
			NewIAMAPIError(ErrNotFound, "deletion request not found"))
		return
	}

	// Cancel deletion
	if err := manager.CancelDeletion(ctx, deletionID, cancelledBy); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Get the updated deletion request to return
	updatedReq, err := manager.GetDeletionStatus(ctx, deletionID)
	if err != nil {
		// Deletion was cancelled but we couldn't retrieve it - return minimal response
		RespondJSON(w, r, http.StatusOK, map[string]interface{}{
			"id":     deletionID,
			"status": "cancelled",
		})
		return
	}

	RespondJSON(w, r, http.StatusOK, updatedReq)
}

// RegisterGDPRRoutes registers GDPR-related routes on the provided router.
// This should be called from iam.go RegisterRoutes to add GDPR endpoints.
func RegisterGDPRRoutes(r chi.Router, h *IAMHandler) {
	// Consent Management routes
	r.Post("/consent", h.GrantConsent)
	r.Get("/consents", h.ListUserConsents)
	r.Get("/consent/{purpose}", h.GetConsent)
	r.Put("/consent/{purpose}/withdraw", h.WithdrawConsent)
	r.Delete("/consent/{purpose}", h.WithdrawConsent) // RESTful alias for withdraw

	// Data Export routes
	r.Post("/export", h.RequestDataExport)
	r.Get("/export/{export_id}", h.GetExportStatus)
	r.Get("/export/{export_id}/download", h.DownloadExport)

	// Data Deletion routes
	r.Post("/deletion", h.RequestDeletion)
	r.Get("/deletion/{deletion_id}", h.GetDeletionStatus)
	r.Delete("/deletion/{deletion_id}", h.CancelDeletion)
}
