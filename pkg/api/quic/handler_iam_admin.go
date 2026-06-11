//go:build quic

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

package quic

import (
	"context"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// Admin Service Interface
// =============================================================================

// adminServiceInterface defines delegated administration operations for type assertion.
type adminServiceInterface interface {
	// Assignment operations
	CreateAdminAssignment(ctx context.Context, opts iam.CreateAdminAssignmentOptions) (*iam.AdminAssignment, error)
	GetAdminAssignment(ctx context.Context, id string) (*iam.AdminAssignment, error)
	UpdateAdminAssignment(ctx context.Context, id string, opts iam.UpdateAdminAssignmentOptions) (*iam.AdminAssignment, error)
	DeleteAdminAssignment(ctx context.Context, id string) error
	ListAdminAssignments(ctx context.Context, opts iam.ListAdminAssignmentsOptions) ([]*iam.AdminAssignment, error)
	CheckAdminPermission(ctx context.Context, req *iam.AdminCheckRequest) (*iam.AdminCheckResponse, error)

	// Impersonation operations
	StartImpersonation(ctx context.Context, opts *iam.StartImpersonationOptions) (*iam.ImpersonationSession, error)
	EndImpersonation(ctx context.Context, sessionID string) error
	GetActiveImpersonations(ctx context.Context, adminUserID string) ([]*iam.ImpersonationSession, error)
}

// =============================================================================
// Admin Request/Response Types
// =============================================================================

// CreateAdminAssignmentRequest represents a request to create an admin assignment.
type CreateAdminAssignmentRequest struct {
	UserID            string                `json:"user_id"`
	AdminRole         iam.AdminRole         `json:"admin_role"`
	TenantIDs         []string              `json:"tenant_ids,omitempty"`
	Permissions       []iam.AdminPermission `json:"permissions,omitempty"`
	DeniedPermissions []iam.AdminPermission `json:"denied_permissions,omitempty"`
	ResourceScopes    []iam.ResourceScope   `json:"resource_scopes,omitempty"`
	ExpiresAt         *time.Time            `json:"expires_at,omitempty"`
	GrantedBy         string                `json:"granted_by"`
	Metadata          map[string]string     `json:"metadata,omitempty"`
}

// GetAdminAssignmentRequest represents a request to get an admin assignment.
type GetAdminAssignmentRequest struct {
	ID string `json:"id"`
}

// UpdateAdminAssignmentRequest represents a request to update an admin assignment.
type UpdateAdminAssignmentRequest struct {
	ID                string                `json:"id"`
	AdminRole         *iam.AdminRole        `json:"admin_role,omitempty"`
	TenantIDs         []string              `json:"tenant_ids,omitempty"`
	Permissions       []iam.AdminPermission `json:"permissions,omitempty"`
	DeniedPermissions []iam.AdminPermission `json:"denied_permissions,omitempty"`
	ResourceScopes    []iam.ResourceScope   `json:"resource_scopes,omitempty"`
	ExpiresAt         *time.Time            `json:"expires_at,omitempty"`
	Active            *bool                 `json:"active,omitempty"`
	Metadata          map[string]string     `json:"metadata,omitempty"`
}

// DeleteAdminAssignmentRequest represents a request to delete an admin assignment.
type DeleteAdminAssignmentRequest struct {
	ID string `json:"id"`
}

// ListAdminAssignmentsRequest represents a request to list admin assignments.
type ListAdminAssignmentsRequest struct {
	UserID     string        `json:"user_id,omitempty"`
	TenantID   string        `json:"tenant_id,omitempty"`
	AdminRole  iam.AdminRole `json:"admin_role,omitempty"`
	ActiveOnly bool          `json:"active_only,omitempty"`
	Limit      int           `json:"limit,omitempty"`
	Offset     int           `json:"offset,omitempty"`
}

// ListAdminAssignmentsResponse represents a response containing admin assignments.
type ListAdminAssignmentsResponse struct {
	Assignments []*iam.AdminAssignment `json:"assignments"`
}

// CheckAdminPermissionRequest represents a request to check admin permission.
type CheckAdminPermissionRequest struct {
	UserID       string              `json:"user_id"`
	Permission   iam.AdminPermission `json:"permission"`
	TenantID     string              `json:"tenant_id,omitempty"`
	ResourceType string              `json:"resource_type,omitempty"`
	ResourceID   string              `json:"resource_id,omitempty"`
}

// StartImpersonationRequest represents a request to start impersonation.
type StartImpersonationRequest struct {
	AdminUserID  string        `json:"admin_user_id"`
	TargetUserID string        `json:"target_user_id"`
	TenantID     string        `json:"tenant_id"`
	Reason       string        `json:"reason"`
	Duration     time.Duration `json:"duration,omitempty"`
}

// EndImpersonationRequest represents a request to end impersonation.
type EndImpersonationRequest struct {
	SessionID string `json:"session_id"`
}

// GetActiveImpersonationsRequest represents a request to get active impersonations.
type GetActiveImpersonationsRequest struct {
	AdminUserID string `json:"admin_user_id"`
}

// GetActiveImpersonationsResponse represents a response containing active impersonation sessions.
type GetActiveImpersonationsResponse struct {
	Sessions []*iam.ImpersonationSession `json:"sessions"`
}

// =============================================================================
// Admin Operations Handlers
// =============================================================================

func (h *Handler) handleIAMCreateAdminAssignment(ctx context.Context, req *Request) *Response {
	adminSvc, ok := h.service.(adminServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("admin"))
	}

	var createReq CreateAdminAssignmentRequest
	if err := DecodePayload(req.Payload, &createReq); err != nil {
		return h.errorResponse(err)
	}

	opts := iam.CreateAdminAssignmentOptions{
		UserID:            createReq.UserID,
		AdminRole:         createReq.AdminRole,
		TenantIDs:         createReq.TenantIDs,
		Permissions:       createReq.Permissions,
		DeniedPermissions: createReq.DeniedPermissions,
		ResourceScopes:    createReq.ResourceScopes,
		ExpiresAt:         createReq.ExpiresAt,
		GrantedBy:         createReq.GrantedBy,
		Metadata:          createReq.Metadata,
	}

	assignment, err := adminSvc.CreateAdminAssignment(ctx, opts)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(assignment)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMGetAdminAssignment(ctx context.Context, req *Request) *Response {
	adminSvc, ok := h.service.(adminServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("admin"))
	}

	var getReq GetAdminAssignmentRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	assignment, err := adminSvc.GetAdminAssignment(ctx, getReq.ID)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(assignment)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMUpdateAdminAssignment(ctx context.Context, req *Request) *Response {
	adminSvc, ok := h.service.(adminServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("admin"))
	}

	var updateReq UpdateAdminAssignmentRequest
	if err := DecodePayload(req.Payload, &updateReq); err != nil {
		return h.errorResponse(err)
	}

	opts := iam.UpdateAdminAssignmentOptions{
		AdminRole:         updateReq.AdminRole,
		TenantIDs:         updateReq.TenantIDs,
		Permissions:       updateReq.Permissions,
		DeniedPermissions: updateReq.DeniedPermissions,
		ResourceScopes:    updateReq.ResourceScopes,
		ExpiresAt:         updateReq.ExpiresAt,
		Active:            updateReq.Active,
		Metadata:          updateReq.Metadata,
	}

	assignment, err := adminSvc.UpdateAdminAssignment(ctx, updateReq.ID, opts)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(assignment)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMDeleteAdminAssignment(ctx context.Context, req *Request) *Response {
	adminSvc, ok := h.service.(adminServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("admin"))
	}

	var deleteReq DeleteAdminAssignmentRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	err := adminSvc.DeleteAdminAssignment(ctx, deleteReq.ID)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: nil}
}

func (h *Handler) handleIAMListAdminAssignments(ctx context.Context, req *Request) *Response {
	adminSvc, ok := h.service.(adminServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("admin"))
	}

	var listReq ListAdminAssignmentsRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	opts := iam.ListAdminAssignmentsOptions{
		UserID:     listReq.UserID,
		TenantID:   listReq.TenantID,
		AdminRole:  listReq.AdminRole,
		ActiveOnly: listReq.ActiveOnly,
		Limit:      listReq.Limit,
		Offset:     listReq.Offset,
	}

	assignments, err := adminSvc.ListAdminAssignments(ctx, opts)
	if err != nil {
		return h.errorResponse(err)
	}

	resp := &ListAdminAssignmentsResponse{
		Assignments: assignments,
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMCheckAdminPermission(ctx context.Context, req *Request) *Response {
	adminSvc, ok := h.service.(adminServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("admin"))
	}

	var checkReq CheckAdminPermissionRequest
	if err := DecodePayload(req.Payload, &checkReq); err != nil {
		return h.errorResponse(err)
	}

	iamReq := &iam.AdminCheckRequest{
		UserID:       checkReq.UserID,
		Permission:   checkReq.Permission,
		TenantID:     checkReq.TenantID,
		ResourceType: checkReq.ResourceType,
		ResourceID:   checkReq.ResourceID,
	}

	resp, err := adminSvc.CheckAdminPermission(ctx, iamReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMStartImpersonation(ctx context.Context, req *Request) *Response {
	adminSvc, ok := h.service.(adminServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("admin"))
	}

	var startReq StartImpersonationRequest
	if err := DecodePayload(req.Payload, &startReq); err != nil {
		return h.errorResponse(err)
	}

	opts := &iam.StartImpersonationOptions{
		AdminUserID:  startReq.AdminUserID,
		TargetUserID: startReq.TargetUserID,
		TenantID:     startReq.TenantID,
		Reason:       startReq.Reason,
		Duration:     startReq.Duration,
	}

	session, err := adminSvc.StartImpersonation(ctx, opts)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(session)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMEndImpersonation(ctx context.Context, req *Request) *Response {
	adminSvc, ok := h.service.(adminServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("admin"))
	}

	var endReq EndImpersonationRequest
	if err := DecodePayload(req.Payload, &endReq); err != nil {
		return h.errorResponse(err)
	}

	err := adminSvc.EndImpersonation(ctx, endReq.SessionID)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: nil}
}

func (h *Handler) handleIAMGetActiveImpersonations(ctx context.Context, req *Request) *Response {
	adminSvc, ok := h.service.(adminServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("admin"))
	}

	var getReq GetActiveImpersonationsRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	sessions, err := adminSvc.GetActiveImpersonations(ctx, getReq.AdminUserID)
	if err != nil {
		return h.errorResponse(err)
	}

	resp := &GetActiveImpersonationsResponse{
		Sessions: sessions,
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
