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
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// ListAuditEvents handles GET /api/v1/iam/audit - Query audit events with filters.
func (h *IAMHandler) ListAuditEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Tenant scoping: the caller reads its own authenticated tenant's
	// chain, and ONLY that chain. An empty tenant_id query parameter
	// defaults to it; any different tenant_id — from a system-tenant
	// principal included — is rejected before ever reaching storage, so a
	// rejected query leaks no events (iam.ResolveRequestTenant states the
	// full rule).
	tenantID, err := iam.ResolveRequestTenant(ctx, r.URL.Query().Get("tenant_id"))
	if err != nil {
		RespondIAMError(w, r, http.StatusForbidden,
			NewIAMAPIError(ErrForbidden, err.Error()))
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

	// Tenant scoping mirrors ListAuditEvents (iam.ResolveRequestTenant):
	// the caller's own tenant only — no caller identity lifts it.
	tenantID, err := iam.ResolveRequestTenant(ctx, r.URL.Query().Get("tenant_id"))
	if err != nil {
		RespondIAMError(w, r, http.StatusForbidden,
			NewIAMAPIError(ErrForbidden, err.Error()))
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

	// Tenant scoping mirrors ListAuditEvents/GetAuditEvent
	// (iam.ResolveRequestTenant): the caller's own tenant only — no
	// caller identity lifts it. This runs BEFORE the audit:delete
	// permission check below so a caller targeting another tenant never
	// reaches storage, exactly like the read path.
	tenantID, err := iam.ResolveRequestTenant(ctx, r.URL.Query().Get("tenant_id"))
	if err != nil {
		RespondIAMError(w, r, http.StatusForbidden,
			NewIAMAPIError(ErrForbidden, err.Error()))
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
	r.Post("/api/v1/iam/audit", handler.RecordAuditEvent)
	r.Delete("/api/v1/iam/audit", handler.DeleteOldAuditEvents)
}

// recordAuditEventRequest is the POST /api/v1/iam/audit body: tenant_id is
// optional (defaults to the caller's own authenticated tenant), event is
// the iam.AuditEvent to record (TP-specific extra fields ride in
// event.details — no schema addition beyond this endpoint), and async
// selects Log (false, synchronous, event_id populated in the response) vs
// LogAsync (true, fire-and-forget, event_id empty).
type recordAuditEventRequest struct {
	TenantID string          `json:"tenant_id,omitempty"`
	Event    *iam.AuditEvent `json:"event"`
	Async    bool            `json:"async,omitempty"`
}

// RecordAuditEvent handles POST /api/v1/iam/audit - record (emit) a new
// audit event onto the platform audit chain. This is the remote-write
// counterpart to ListAuditEvents/GetAuditEvent.
func (h *IAMHandler) RecordAuditEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req recordAuditEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid request body: "+err.Error()))
		return
	}
	if req.Event == nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "event is required"))
		return
	}

	tenantID, err := iam.ResolveRequestTenant(ctx, req.TenantID)
	if err != nil {
		RespondIAMError(w, r, http.StatusForbidden,
			NewIAMAPIError(ErrForbidden, err.Error()))
		return
	}
	req.Event.TenantID = tenantID

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

	if req.Async {
		logger.LogAsync(req.Event)
		RespondJSON(w, r, http.StatusAccepted, map[string]interface{}{"event_id": ""})
		return
	}
	if err := logger.Log(ctx, req.Event); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusCreated, map[string]interface{}{"event_id": req.Event.ID})
}
