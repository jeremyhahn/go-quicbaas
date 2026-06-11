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
// GDPR Service Interface
// =============================================================================

// gdprServiceInterface defines GDPR compliance operations for type assertion.
type gdprServiceInterface interface {
	// Consent operations
	GrantConsent(ctx context.Context, tenantID, userID string, purpose iam.ConsentPurpose, version string, expiresAt *time.Time, ipAddress, userAgent string) (*iam.Consent, error)
	WithdrawConsent(ctx context.Context, tenantID, userID string, purpose iam.ConsentPurpose, ipAddress, userAgent string) error
	GetConsent(ctx context.Context, tenantID, userID string, purpose iam.ConsentPurpose) (*iam.Consent, error)
	ListUserConsents(ctx context.Context, tenantID, userID string) ([]*iam.Consent, error)

	// Data export operations
	RequestExport(ctx context.Context, tenantID, userID, requestedBy string, format iam.DataExportFormat, includeCategories, excludeCategories []string) (*iam.DataExportRequest, error)
	GetExportStatus(ctx context.Context, exportID string) (*iam.DataExportRequest, error)
	GenerateExport(ctx context.Context, exportID string) (*iam.UserDataExport, error)

	// Data deletion operations
	RequestDeletion(ctx context.Context, tenantID, userID, requestedBy, reason string, deleteCategories, preserveCategories []string, gracePeriodDays int) (*iam.DataDeletionRequest, error)
	CancelDeletion(ctx context.Context, deletionID, cancelledBy string) error
	ProcessDeletion(ctx context.Context, deletionID string) (*iam.DeletionSummary, error)
	GetDeletionStatus(ctx context.Context, deletionID string) (*iam.DataDeletionRequest, error)
}

// =============================================================================
// GDPR Request/Response Types
// =============================================================================

// GrantConsentRequest represents a request to grant consent.
type GrantConsentRequest struct {
	TenantID  string             `json:"tenant_id"`
	UserID    string             `json:"user_id"`
	Purpose   iam.ConsentPurpose `json:"purpose"`
	Version   string             `json:"version"`
	ExpiresAt *time.Time         `json:"expires_at,omitempty"`
	IPAddress string             `json:"ip_address,omitempty"`
	UserAgent string             `json:"user_agent,omitempty"`
}

// WithdrawConsentRequest represents a request to withdraw consent.
type WithdrawConsentRequest struct {
	TenantID  string             `json:"tenant_id"`
	UserID    string             `json:"user_id"`
	Purpose   iam.ConsentPurpose `json:"purpose"`
	IPAddress string             `json:"ip_address,omitempty"`
	UserAgent string             `json:"user_agent,omitempty"`
}

// GetConsentRequest represents a request to get a specific consent.
type GetConsentRequest struct {
	TenantID string             `json:"tenant_id"`
	UserID   string             `json:"user_id"`
	Purpose  iam.ConsentPurpose `json:"purpose"`
}

// ListUserConsentsRequest represents a request to list user consents.
type ListUserConsentsRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
}

// ListUserConsentsResponse represents a response containing user consents.
type ListUserConsentsResponse struct {
	Consents []*iam.Consent `json:"consents"`
}

// RequestExportRequest represents a request to create a data export.
type RequestExportRequest struct {
	TenantID          string               `json:"tenant_id"`
	UserID            string               `json:"user_id"`
	RequestedBy       string               `json:"requested_by"`
	Format            iam.DataExportFormat `json:"format"`
	IncludeCategories []string             `json:"include_categories,omitempty"`
	ExcludeCategories []string             `json:"exclude_categories,omitempty"`
}

// GetExportStatusRequest represents a request to get export status.
type GetExportStatusRequest struct {
	ExportID string `json:"export_id"`
}

// GenerateExportRequest represents a request to generate an export.
type GenerateExportRequest struct {
	ExportID string `json:"export_id"`
}

// RequestDeletionRequest represents a request to create a data deletion request.
type RequestDeletionRequest struct {
	TenantID           string   `json:"tenant_id"`
	UserID             string   `json:"user_id"`
	RequestedBy        string   `json:"requested_by"`
	Reason             string   `json:"reason,omitempty"`
	DeleteCategories   []string `json:"delete_categories,omitempty"`
	PreserveCategories []string `json:"preserve_categories,omitempty"`
	GracePeriodDays    int      `json:"grace_period_days,omitempty"`
}

// CancelDeletionRequest represents a request to cancel a deletion.
type CancelDeletionRequest struct {
	DeletionID  string `json:"deletion_id"`
	CancelledBy string `json:"cancelled_by"`
}

// ProcessDeletionRequest represents a request to process a deletion.
type ProcessDeletionRequest struct {
	DeletionID string `json:"deletion_id"`
}

// GetDeletionStatusRequest represents a request to get deletion status.
type GetDeletionStatusRequest struct {
	DeletionID string `json:"deletion_id"`
}

// =============================================================================
// Consent Operations Handlers
// =============================================================================

func (h *Handler) handleIAMGrantConsent(ctx context.Context, req *Request) *Response {
	gdprSvc, ok := h.service.(gdprServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("GDPR"))
	}

	var grantReq GrantConsentRequest
	if err := DecodePayload(req.Payload, &grantReq); err != nil {
		return h.errorResponse(err)
	}

	consent, err := gdprSvc.GrantConsent(
		ctx,
		grantReq.TenantID,
		grantReq.UserID,
		grantReq.Purpose,
		grantReq.Version,
		grantReq.ExpiresAt,
		grantReq.IPAddress,
		grantReq.UserAgent,
	)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(consent)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMWithdrawConsent(ctx context.Context, req *Request) *Response {
	gdprSvc, ok := h.service.(gdprServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("GDPR"))
	}

	var withdrawReq WithdrawConsentRequest
	if err := DecodePayload(req.Payload, &withdrawReq); err != nil {
		return h.errorResponse(err)
	}

	err := gdprSvc.WithdrawConsent(
		ctx,
		withdrawReq.TenantID,
		withdrawReq.UserID,
		withdrawReq.Purpose,
		withdrawReq.IPAddress,
		withdrawReq.UserAgent,
	)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: nil}
}

func (h *Handler) handleIAMGetConsent(ctx context.Context, req *Request) *Response {
	gdprSvc, ok := h.service.(gdprServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("GDPR"))
	}

	var getReq GetConsentRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	consent, err := gdprSvc.GetConsent(ctx, getReq.TenantID, getReq.UserID, getReq.Purpose)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(consent)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMListUserConsents(ctx context.Context, req *Request) *Response {
	gdprSvc, ok := h.service.(gdprServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("GDPR"))
	}

	var listReq ListUserConsentsRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	consents, err := gdprSvc.ListUserConsents(ctx, listReq.TenantID, listReq.UserID)
	if err != nil {
		return h.errorResponse(err)
	}

	resp := &ListUserConsentsResponse{
		Consents: consents,
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Data Export Operations Handlers
// =============================================================================

func (h *Handler) handleIAMRequestExport(ctx context.Context, req *Request) *Response {
	gdprSvc, ok := h.service.(gdprServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("GDPR"))
	}

	var exportReq RequestExportRequest
	if err := DecodePayload(req.Payload, &exportReq); err != nil {
		return h.errorResponse(err)
	}

	exportRequest, err := gdprSvc.RequestExport(
		ctx,
		exportReq.TenantID,
		exportReq.UserID,
		exportReq.RequestedBy,
		exportReq.Format,
		exportReq.IncludeCategories,
		exportReq.ExcludeCategories,
	)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(exportRequest)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMGetExportStatus(ctx context.Context, req *Request) *Response {
	gdprSvc, ok := h.service.(gdprServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("GDPR"))
	}

	var getReq GetExportStatusRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	exportRequest, err := gdprSvc.GetExportStatus(ctx, getReq.ExportID)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(exportRequest)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMGenerateExport(ctx context.Context, req *Request) *Response {
	gdprSvc, ok := h.service.(gdprServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("GDPR"))
	}

	var generateReq GenerateExportRequest
	if err := DecodePayload(req.Payload, &generateReq); err != nil {
		return h.errorResponse(err)
	}

	userDataExport, err := gdprSvc.GenerateExport(ctx, generateReq.ExportID)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(userDataExport)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Data Deletion Operations Handlers
// =============================================================================

func (h *Handler) handleIAMRequestDeletion(ctx context.Context, req *Request) *Response {
	gdprSvc, ok := h.service.(gdprServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("GDPR"))
	}

	var deleteReq RequestDeletionRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	deletionRequest, err := gdprSvc.RequestDeletion(
		ctx,
		deleteReq.TenantID,
		deleteReq.UserID,
		deleteReq.RequestedBy,
		deleteReq.Reason,
		deleteReq.DeleteCategories,
		deleteReq.PreserveCategories,
		deleteReq.GracePeriodDays,
	)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(deletionRequest)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMCancelDeletion(ctx context.Context, req *Request) *Response {
	gdprSvc, ok := h.service.(gdprServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("GDPR"))
	}

	var cancelReq CancelDeletionRequest
	if err := DecodePayload(req.Payload, &cancelReq); err != nil {
		return h.errorResponse(err)
	}

	err := gdprSvc.CancelDeletion(ctx, cancelReq.DeletionID, cancelReq.CancelledBy)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: nil}
}

func (h *Handler) handleIAMProcessDeletion(ctx context.Context, req *Request) *Response {
	gdprSvc, ok := h.service.(gdprServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("GDPR"))
	}

	var processReq ProcessDeletionRequest
	if err := DecodePayload(req.Payload, &processReq); err != nil {
		return h.errorResponse(err)
	}

	summary, err := gdprSvc.ProcessDeletion(ctx, processReq.DeletionID)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(summary)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMGetDeletionStatus(ctx context.Context, req *Request) *Response {
	gdprSvc, ok := h.service.(gdprServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("GDPR"))
	}

	var getReq GetDeletionStatusRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	deletionRequest, err := gdprSvc.GetDeletionStatus(ctx, getReq.DeletionID)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(deletionRequest)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
