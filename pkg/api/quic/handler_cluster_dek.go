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
// Cluster DEK Operations Handlers
// =============================================================================

// handleClusterDEKInit handles cluster DEK initialization requests.
func (h *Handler) handleClusterDEKInit(ctx context.Context, req *Request) *Response {
	if h.clusterDEKService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("cluster DEK"))
	}

	var initReq service.InitClusterDEKRequest
	if err := DecodePayload(req.Payload, &initReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.clusterDEKService.InitializeClusterDEK(ctx, &initReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleClusterDEKStatus handles cluster DEK status retrieval requests.
func (h *Handler) handleClusterDEKStatus(ctx context.Context, req *Request) *Response {
	if h.clusterDEKService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("cluster DEK"))
	}

	resp, err := h.clusterDEKService.GetClusterDEKStatus(ctx)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleClusterDEKRotate handles cluster DEK rotation requests.
func (h *Handler) handleClusterDEKRotate(ctx context.Context, req *Request) *Response {
	if h.clusterDEKService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("cluster DEK"))
	}

	var rotateReq service.RotateClusterDEKRequest
	if err := DecodePayload(req.Payload, &rotateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.clusterDEKService.RotateClusterDEK(ctx, &rotateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleClusterDEKSyncNode handles node DEK synchronization requests.
func (h *Handler) handleClusterDEKSyncNode(ctx context.Context, req *Request) *Response {
	if h.clusterDEKService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("cluster DEK"))
	}

	var syncReq service.SyncNodeDEKRequest
	if err := DecodePayload(req.Payload, &syncReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.clusterDEKService.SyncNodeDEK(ctx, &syncReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleClusterDEKNodeStatus handles node DEK status retrieval requests.
func (h *Handler) handleClusterDEKNodeStatus(ctx context.Context, req *Request) *Response {
	if h.clusterDEKService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("cluster DEK"))
	}

	var statusReq service.GetNodeDEKStatusRequest
	if err := DecodePayload(req.Payload, &statusReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.clusterDEKService.GetNodeDEKStatus(ctx, &statusReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleClusterDEKStartReencrypt handles cluster DEK re-encryption start requests.
func (h *Handler) handleClusterDEKStartReencrypt(ctx context.Context, req *Request) *Response {
	if h.clusterDEKService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("cluster DEK"))
	}

	var reencryptReq service.StartReencryptionRequest
	if err := DecodePayload(req.Payload, &reencryptReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.clusterDEKService.StartReencryption(ctx, &reencryptReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleClusterDEKReencryptStatus handles cluster DEK re-encryption status requests.
func (h *Handler) handleClusterDEKReencryptStatus(ctx context.Context, req *Request) *Response {
	if h.clusterDEKService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("cluster DEK"))
	}

	resp, err := h.clusterDEKService.GetReencryptionStatus(ctx)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleClusterDEKCancelReencrypt handles cluster DEK re-encryption cancellation requests.
func (h *Handler) handleClusterDEKCancelReencrypt(ctx context.Context, req *Request) *Response {
	if h.clusterDEKService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("cluster DEK"))
	}

	err := h.clusterDEKService.CancelReencryption(ctx)
	if err != nil {
		return h.errorResponse(err)
	}

	// Return success with empty payload for void operations
	return &Response{Status: StatusOK, Payload: nil}
}

// handleClusterDEKRecover handles cluster DEK recovery requests.
func (h *Handler) handleClusterDEKRecover(ctx context.Context, req *Request) *Response {
	if h.clusterDEKService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("cluster DEK"))
	}

	var recoverReq service.RecoverDEKRequest
	if err := DecodePayload(req.Payload, &recoverReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.clusterDEKService.RecoverDEK(ctx, &recoverReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
