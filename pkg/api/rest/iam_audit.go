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
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// ListAuditEvents handles GET /api/v1/iam/audit - Query audit events with filters.
func (h *IAMHandler) ListAuditEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get tenant ID from context or query parameter
	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant_id parameter is required"))
		return
	}

	// Build filter from query parameters
	filter := &iam.AuditFilter{
		TenantID: tenantID,
	}

	// Parse event types
	if eventTypes := r.URL.Query().Get("event_type"); eventTypes != "" {
		filter.EventTypes = []iam.AuditEventType{iam.AuditEventType(eventTypes)}
	}

	// Parse categories
	if category := r.URL.Query().Get("category"); category != "" {
		filter.Categories = []iam.AuditCategory{iam.AuditCategory(category)}
	}

	// Parse outcomes
	if outcome := r.URL.Query().Get("outcome"); outcome != "" {
		filter.Outcomes = []iam.AuditOutcome{iam.AuditOutcome(outcome)}
	}

	// Parse actor ID
	if actorID := r.URL.Query().Get("actor_id"); actorID != "" {
		filter.ActorID = actorID
	}

	// Parse resource ID
	if resourceID := r.URL.Query().Get("resource_id"); resourceID != "" {
		filter.ResourceID = resourceID
	}

	// Parse time range
	if startTime := r.URL.Query().Get("start_time"); startTime != "" {
		t, err := time.Parse(time.RFC3339, startTime)
		if err == nil {
			filter.StartTime = &t
		}
	}

	if endTime := r.URL.Query().Get("end_time"); endTime != "" {
		t, err := time.Parse(time.RFC3339, endTime)
		if err == nil {
			filter.EndTime = &t
		}
	}

	// Parse pagination
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}
	filter.Limit = limit

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}
	filter.Offset = offset

	// Get audit logger from service
	auditLogger, ok := h.server.service.(interface {
		GetAuditLogger(ctx context.Context) (iam.AuditLogger, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "audit logger not available"))
		return
	}

	logger, err := auditLogger.GetAuditLogger(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if logger == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "audit logger not initialized"))
		return
	}

	// Query events
	events, total, err := logger.Query(ctx, filter)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := map[string]interface{}{
		"events": events,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// GetAuditEvent handles GET /api/v1/iam/audit/{id} - Get a specific audit event.
func (h *IAMHandler) GetAuditEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	eventID := chi.URLParam(r, "id")
	if eventID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "event ID is required"))
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant_id parameter is required"))
		return
	}

	// Get audit logger from service
	auditLogger, ok := h.server.service.(interface {
		GetAuditLogger(ctx context.Context) (iam.AuditLogger, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "audit logger not available"))
		return
	}

	logger, err := auditLogger.GetAuditLogger(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if logger == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "audit logger not initialized"))
		return
	}

	// Get event
	event, err := logger.GetEvent(ctx, tenantID, eventID)
	if err != nil {
		if errors.Is(err, iam.ErrAuditEventNotFound) {
			RespondIAMError(w, r, http.StatusNotFound,
				NewIAMAPIError(ErrNotFound, "audit event not found: "+eventID))
			return
		}
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, event)
}

// DeleteOldAuditEvents handles DELETE /api/v1/iam/audit - Delete events older than timestamp (admin only).
func (h *IAMHandler) DeleteOldAuditEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant_id parameter is required"))
		return
	}

	beforeStr := r.URL.Query().Get("before")
	if beforeStr == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "before parameter is required (ISO8601 timestamp)"))
		return
	}

	before, err := time.Parse(time.RFC3339, beforeStr)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid before timestamp format (use ISO8601/RFC3339)"))
		return
	}

	// Authorization check - only admins with audit:delete permission can
	// delete audit events. The principal is the canonical Subject injected
	// by the authz middleware chain (injectPrincipal); the former
	// ContextKeyUser read had no producer on the production middleware
	// chain and failed closed even for properly-granted callers.
	subject, ok := requireSubject(w, r)
	if !ok {
		return
	}

	// Get admin manager to check permissions
	adminMgr, ok := h.server.service.(interface {
		GetAdminManager(ctx context.Context) (*iam.AdminManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "admin manager not available"))
		return
	}

	manager, err := adminMgr.GetAdminManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if manager == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "admin manager not initialized"))
		return
	}

	// Check if user has audit:delete permission
	checkResp, err := manager.CheckPermission(ctx, &iam.AdminCheckRequest{
		UserID:       subject.ID,
		Permission:   iam.PermAuditDelete,
		TenantID:     tenantID,
		ResourceType: "audit",
	})
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	if !checkResp.Allowed {
		RespondIAMError(w, r, http.StatusForbidden,
			NewIAMAPIError(ErrForbidden, "insufficient permissions to delete audit events"))
		return
	}

	// Get audit logger from service
	auditLogger, ok := h.server.service.(interface {
		GetAuditLogger(ctx context.Context) (iam.AuditLogger, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "audit logger not available"))
		return
	}

	logger, err := auditLogger.GetAuditLogger(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if logger == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "audit logger not initialized"))
		return
	}

	// Delete old events
	count, err := logger.DeleteOlderThan(ctx, tenantID, before)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	resp := map[string]interface{}{
		"deleted_count": count,
		"before":        before,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// RegisterAuditRoutes registers audit-related routes with the router.
func RegisterAuditRoutes(r chi.Router, handler *IAMHandler) {
	r.Get("/api/v1/iam/audit", handler.ListAuditEvents)
	r.Get("/api/v1/iam/audit/{id}", handler.GetAuditEvent)
	r.Delete("/api/v1/iam/audit", handler.DeleteOldAuditEvents)
}
