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

// CreateAdminAssignment handles POST /api/v1/admin/assignments - Create a new admin assignment.
func (h *IAMHandler) CreateAdminAssignment(w http.ResponseWriter, r *http.Request) {
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
	var opts iam.CreateAdminAssignmentOptions
	if err := json.Unmarshal(body, &opts); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Get admin manager from service
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

	// Create assignment
	assignment, err := manager.CreateAssignment(ctx, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Log audit event
	h.logAuditEvent(ctx, "",
		iam.AuditEventUserManagement,
		iam.AuditCategoryAdministration,
		"create",
		iam.AuditOutcomeSuccess,
		&iam.AuditResource{Type: "admin_assignment", ID: assignment.ID},
		map[string]interface{}{
			"user_id":    assignment.UserID,
			"admin_role": string(assignment.AdminRole),
		},
	)

	RespondJSON(w, r, http.StatusCreated, assignment)
}

// ListAdminAssignments handles GET /api/v1/admin/assignments - List admin assignments.
func (h *IAMHandler) ListAdminAssignments(w http.ResponseWriter, r *http.Request) {
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

	userID := r.URL.Query().Get("user_id")
	tenantID := r.URL.Query().Get("tenant_id")
	adminRoleStr := r.URL.Query().Get("admin_role")

	activeOnly := false
	if activeStr := r.URL.Query().Get("active_only"); activeStr == "true" {
		activeOnly = true
	}

	// Build list options
	opts := iam.ListAdminAssignmentsOptions{
		UserID:     userID,
		TenantID:   tenantID,
		ActiveOnly: activeOnly,
		Limit:      limit,
		Offset:     offset,
	}

	if adminRoleStr != "" {
		opts.AdminRole = iam.AdminRole(adminRoleStr)
	}

	// Get admin manager from service
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

	// List assignments
	assignments, err := manager.ListAssignments(ctx, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Return assignments wrapped in a response object
	resp := map[string]interface{}{
		"assignments": assignments,
		"total":       len(assignments),
	}
	RespondJSON(w, r, http.StatusOK, resp)
}

// GetAdminAssignment handles GET /api/v1/admin/assignments/{assignment_id} - Get a specific admin assignment.
func (h *IAMHandler) GetAdminAssignment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	assignmentID := chi.URLParam(r, "assignment_id")
	if assignmentID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "assignment ID is required"))
		return
	}

	// Get admin manager from service
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

	// Get assignment
	assignment, err := manager.GetAssignment(ctx, assignmentID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, assignment)
}

// UpdateAdminAssignment handles PUT /api/v1/admin/assignments/{assignment_id} - Update an admin assignment.
func (h *IAMHandler) UpdateAdminAssignment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	assignmentID := chi.URLParam(r, "assignment_id")
	if assignmentID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "assignment ID is required"))
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
	var opts iam.UpdateAdminAssignmentOptions
	if err := json.Unmarshal(body, &opts); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Get admin manager from service
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

	// Update assignment
	assignment, err := manager.UpdateAssignment(ctx, assignmentID, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Log audit event
	h.logAuditEvent(ctx, "",
		iam.AuditEventUserManagement,
		iam.AuditCategoryAdministration,
		"update",
		iam.AuditOutcomeSuccess,
		&iam.AuditResource{Type: "admin_assignment", ID: assignmentID},
		map[string]interface{}{
			"user_id": assignment.UserID,
		},
	)

	RespondJSON(w, r, http.StatusOK, assignment)
}

// DeleteAdminAssignment handles DELETE /api/v1/admin/assignments/{assignment_id} - Delete an admin assignment.
func (h *IAMHandler) DeleteAdminAssignment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	assignmentID := chi.URLParam(r, "assignment_id")
	if assignmentID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "assignment ID is required"))
		return
	}

	// Get admin manager from service
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

	// Delete assignment
	if err := manager.DeleteAssignment(ctx, assignmentID); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Log audit event
	h.logAuditEvent(ctx, "",
		iam.AuditEventUserManagement,
		iam.AuditCategoryAdministration,
		"delete",
		iam.AuditOutcomeSuccess,
		&iam.AuditResource{Type: "admin_assignment", ID: assignmentID},
		nil,
	)

	RespondNoContent(w, r)
}

// GetUserAssignments handles GET /api/v1/admin/users/{user_id}/assignments - Get admin assignments for a user.
func (h *IAMHandler) GetUserAssignments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user ID is required"))
		return
	}

	// Get admin manager from service
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

	// List assignments with user_id filter
	opts := iam.ListAdminAssignmentsOptions{
		UserID: userID,
	}

	assignments, err := manager.ListAssignments(ctx, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := map[string]interface{}{
		"user_id":     userID,
		"assignments": assignments,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// CheckAdminPermission handles POST /api/v1/admin/check - Check if an admin has a specific permission.
func (h *IAMHandler) CheckAdminPermission(w http.ResponseWriter, r *http.Request) {
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
	var checkReq iam.AdminCheckRequest
	if err := json.Unmarshal(body, &checkReq); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Get admin manager from service
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

	// Check permission
	checkResp, err := manager.CheckPermission(ctx, &checkReq)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, checkResp)
}

// startImpersonationRequest is the REST request for starting impersonation.
type startImpersonationRequest struct {
	AdminUserID  string `json:"admin_user_id"`
	TargetUserID string `json:"target_user_id"`
	TenantID     string `json:"tenant_id"`
	Reason       string `json:"reason"`
	Duration     string `json:"duration,omitempty"`
}

// StartImpersonation handles POST /api/v1/admin/impersonate - Start an impersonation session.
func (h *IAMHandler) StartImpersonation(w http.ResponseWriter, r *http.Request) {
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
	var req startImpersonationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Parse duration string
	var duration time.Duration
	if req.Duration != "" {
		duration, err = time.ParseDuration(req.Duration)
		if err != nil {
			RespondIAMError(w, r, http.StatusBadRequest,
				NewIAMAPIError(ErrBadRequest, "invalid duration format"))
			return
		}
	}

	// Convert to IAM options
	opts := iam.StartImpersonationOptions{
		AdminUserID:  req.AdminUserID,
		TargetUserID: req.TargetUserID,
		TenantID:     req.TenantID,
		Reason:       req.Reason,
		Duration:     duration,
	}

	// Get admin manager from service
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

	// Start impersonation
	session, err := manager.StartImpersonation(ctx, &opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Log audit event
	h.logAuditEvent(ctx, opts.TenantID,
		iam.AuditEventSessionEvent,
		iam.AuditCategorySecurity,
		"start_impersonation",
		iam.AuditOutcomeSuccess,
		&iam.AuditResource{Type: "impersonation_session", ID: session.ID},
		map[string]interface{}{
			"admin_user_id":  opts.AdminUserID,
			"target_user_id": opts.TargetUserID,
			"reason":         opts.Reason,
		},
	)

	RespondJSON(w, r, http.StatusOK, session)
}

// EndImpersonation handles DELETE /api/v1/admin/impersonate/{session_id} - End an impersonation session.
func (h *IAMHandler) EndImpersonation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "session ID is required"))
		return
	}

	// Get admin manager from service
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

	// End impersonation
	if err := manager.EndImpersonation(ctx, sessionID); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Get the updated session to return
	session, err := manager.GetImpersonationSession(ctx, sessionID)
	if err != nil {
		// Session was ended but we couldn't retrieve it - return minimal response
		RespondJSON(w, r, http.StatusOK, map[string]string{"status": "impersonation_ended"})
		return
	}

	// Log audit event
	h.logAuditEvent(ctx, "",
		iam.AuditEventSessionEvent,
		iam.AuditCategorySecurity,
		"end_impersonation",
		iam.AuditOutcomeSuccess,
		&iam.AuditResource{Type: "impersonation_session", ID: sessionID},
		nil,
	)

	RespondJSON(w, r, http.StatusOK, session)
}

// ListActiveImpersonations handles GET /api/v1/admin/users/{user_id}/impersonations - List active impersonation sessions for a user.
func (h *IAMHandler) ListActiveImpersonations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user ID is required"))
		return
	}

	// Get admin manager from service
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

	// Get active impersonations
	sessions, err := manager.GetActiveImpersonations(ctx, userID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := map[string]interface{}{
		"user_id":  userID,
		"sessions": sessions,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// ListImpersonations handles GET /api/v1/admin/impersonation - List all impersonation sessions.
func (h *IAMHandler) ListImpersonations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query params
	activeOnly := r.URL.Query().Get("active") == "true"
	adminUserID := r.URL.Query().Get("admin_user_id")

	// Get admin manager from service
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

	// List impersonations
	sessions, err := manager.ListImpersonations(ctx, activeOnly, adminUserID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := map[string]interface{}{
		"sessions": sessions,
		"total":    len(sessions),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// RegisterAdminRoutes registers delegated admin routes on the provided router.
func RegisterAdminRoutes(router chi.Router, handler *IAMHandler) {
	// Admin assignment management
	router.Route("/api/v1/admin", func(r chi.Router) {
		r.Route("/assignments", func(r chi.Router) {
			r.Post("/", handler.CreateAdminAssignment)
			r.Get("/", handler.ListAdminAssignments)
			r.Get("/{assignment_id}", handler.GetAdminAssignment)
			r.Put("/{assignment_id}", handler.UpdateAdminAssignment)
			r.Delete("/{assignment_id}", handler.DeleteAdminAssignment)
		})

		// Permission checking
		r.Post("/check-permission", handler.CheckAdminPermission)

		// Impersonation
		r.Route("/impersonation", func(r chi.Router) {
			r.Get("/", handler.ListImpersonations)
			r.Post("/start", handler.StartImpersonation)
			r.Post("/{session_id}/end", handler.EndImpersonation)
		})

		// User-specific routes
		r.Route("/users/{user_id}", func(r chi.Router) {
			r.Get("/assignments", handler.GetUserAssignments)
			r.Get("/impersonations", handler.ListActiveImpersonations)
		})
	})
}
