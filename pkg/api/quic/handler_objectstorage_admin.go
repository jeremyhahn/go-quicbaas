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

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// ObjectStorage Admin Operations Handlers
// =============================================================================

// handleOSAdminDispatch routes ObjectStorage admin operations based on sub-opcode in payload.
// The OS admin uses a single opcode (OpOSAdmin = 0xCF) with sub-operation
// dispatch via the first byte of the payload.
func (h *Handler) handleOSAdminDispatch(ctx context.Context, req *Request) *Response {
	// Payload must contain at least sub-opcode byte
	if len(req.Payload) < 1 {
		return h.errorResponse(api.NewMissingSubOpcodeError(
			"quic.handleOSAdminDispatch", "OS admin"))
	}

	// Extract sub-opcode and create sub-request with remaining payload
	subOpcode := req.Payload[0]
	subReq := &Request{
		Opcode:  req.Opcode,
		Token:   req.Token,
		Payload: req.Payload[1:], // Skip sub-opcode byte
	}

	switch subOpcode {
	case SubOpOSAdminCreate:
		return h.handleOSAdminCreateBackend(ctx, subReq)
	case SubOpOSAdminUpdate:
		return h.handleOSAdminUpdateBackend(ctx, subReq)
	case SubOpOSAdminDelete:
		return h.handleOSAdminDeleteBackend(ctx, subReq)
	case SubOpOSAdminSetStatus:
		return h.handleOSAdminSetBackendStatus(ctx, subReq)
	case SubOpOSAdminGetStats:
		return h.handleOSAdminGetStorageStats(ctx, subReq)
	case SubOpOSAdminRunGC:
		return h.handleOSAdminRunGC(ctx, subReq)
	case SubOpOSAdminGetGC:
		return h.handleOSAdminGetGCProgress(ctx, subReq)
	case SubOpOSAdminRepair:
		return h.handleOSAdminRepairObject(ctx, subReq)
	case SubOpOSAdminCompact:
		return h.handleOSAdminCompactBackend(ctx, subReq)
	default:
		return h.errorResponse(api.NewUnknownSubOpcodeError(
			"quic.handleOSAdminDispatch", "OS admin", subOpcode))
	}
}

// handleOSAdminCreateBackend handles admin backend creation operations.
func (h *Handler) handleOSAdminCreateBackend(ctx context.Context, req *Request) *Response {
	adminService := h.service.ObjectStorageAdminService()
	if adminService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage admin"))
	}

	var createReq service.CreateBackendRequest
	if err := DecodePayload(req.Payload, &createReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := adminService.CreateBackend(ctx, &createReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSAdminUpdateBackend handles admin backend update operations.
func (h *Handler) handleOSAdminUpdateBackend(ctx context.Context, req *Request) *Response {
	adminService := h.service.ObjectStorageAdminService()
	if adminService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage admin"))
	}

	var updateReq service.UpdateBackendRequest
	if err := DecodePayload(req.Payload, &updateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := adminService.UpdateBackend(ctx, &updateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSAdminDeleteBackend handles admin backend deletion operations.
func (h *Handler) handleOSAdminDeleteBackend(ctx context.Context, req *Request) *Response {
	adminService := h.service.ObjectStorageAdminService()
	if adminService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage admin"))
	}

	var deleteReq service.DeleteBackendRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := adminService.DeleteBackend(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSAdminSetBackendStatus handles admin backend status operations.
func (h *Handler) handleOSAdminSetBackendStatus(ctx context.Context, req *Request) *Response {
	adminService := h.service.ObjectStorageAdminService()
	if adminService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage admin"))
	}

	var statusReq service.SetBackendStatusRequest
	if err := DecodePayload(req.Payload, &statusReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := adminService.SetBackendStatus(ctx, &statusReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSAdminGetStorageStats handles admin storage statistics operations.
func (h *Handler) handleOSAdminGetStorageStats(ctx context.Context, req *Request) *Response {
	adminService := h.service.ObjectStorageAdminService()
	if adminService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage admin"))
	}

	var statsReq service.GetStorageStatsRequest
	if err := DecodePayload(req.Payload, &statsReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := adminService.GetStorageStats(ctx, &statsReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSAdminRunGC handles admin garbage collection operations.
func (h *Handler) handleOSAdminRunGC(ctx context.Context, req *Request) *Response {
	adminService := h.service.ObjectStorageAdminService()
	if adminService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage admin"))
	}

	var gcReq service.RunGarbageCollectionRequest
	if err := DecodePayload(req.Payload, &gcReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := adminService.RunGarbageCollection(ctx, &gcReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSAdminGetGCProgress handles admin garbage collection progress operations.
func (h *Handler) handleOSAdminGetGCProgress(ctx context.Context, req *Request) *Response {
	adminService := h.service.ObjectStorageAdminService()
	if adminService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage admin"))
	}

	var progressReq service.GetGarbageCollectionProgressRequest
	if err := DecodePayload(req.Payload, &progressReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := adminService.GetGarbageCollectionProgress(ctx, &progressReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSAdminRepairObject handles admin object repair operations.
func (h *Handler) handleOSAdminRepairObject(ctx context.Context, req *Request) *Response {
	adminService := h.service.ObjectStorageAdminService()
	if adminService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage admin"))
	}

	var repairReq service.RepairObjectRequest
	if err := DecodePayload(req.Payload, &repairReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := adminService.RepairObject(ctx, &repairReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSAdminCompactBackend handles admin backend compaction operations.
func (h *Handler) handleOSAdminCompactBackend(ctx context.Context, req *Request) *Response {
	adminService := h.service.ObjectStorageAdminService()
	if adminService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage admin"))
	}

	var compactReq service.CompactBackendRequest
	if err := DecodePayload(req.Payload, &compactReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := adminService.CompactBackend(ctx, &compactReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
