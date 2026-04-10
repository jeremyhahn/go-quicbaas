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
// Vector Namespace Operations Handlers
// =============================================================================

func (h *Handler) handleVectorCreateNamespace(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var createReq service.CreateNamespaceRequest
	if err := DecodePayload(req.Payload, &createReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.CreateNamespace(ctx, &createReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleVectorDeleteNamespace(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var deleteReq service.DeleteNamespaceRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.DeleteNamespace(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleVectorListNamespaces(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var listReq service.ListNamespacesRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.ListNamespaces(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleVectorGetNamespaceInfo(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var infoReq service.GetNamespaceInfoRequest
	if err := DecodePayload(req.Payload, &infoReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.GetNamespaceInfo(ctx, &infoReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Vector CRUD Operations Handlers
// =============================================================================

func (h *Handler) handleVectorUpsert(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var upsertReq service.UpsertVectorRequest
	if err := DecodePayload(req.Payload, &upsertReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.UpsertVector(ctx, &upsertReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleVectorGet(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var getReq service.GetVectorRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.GetVector(ctx, &getReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleVectorDelete(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var deleteReq service.DeleteVectorRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.DeleteVector(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleVectorBatchUpsert(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var batchReq service.BatchUpsertRequest
	if err := DecodePayload(req.Payload, &batchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.BatchUpsert(ctx, &batchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleVectorBatchDelete(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var batchReq service.BatchDeleteVectorsRequest
	if err := DecodePayload(req.Payload, &batchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.BatchDeleteVectors(ctx, &batchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleVectorListVectors(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var listReq service.ListVectorsRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.ListVectors(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleVectorUpdateMetadata(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var updateReq service.UpdateVectorMetadataRequest
	if err := DecodePayload(req.Payload, &updateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.UpdateMetadata(ctx, &updateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Vector Search Operations Handlers
// =============================================================================

func (h *Handler) handleVectorSearchSimilar(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var searchReq service.SearchSimilarRequest
	if err := DecodePayload(req.Payload, &searchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.SearchSimilar(ctx, &searchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleVectorSearchByMetadata(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var searchReq service.SearchByMetadataRequest
	if err := DecodePayload(req.Payload, &searchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.SearchByMetadata(ctx, &searchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleVectorHybridSearch(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var searchReq service.HybridSearchRequest
	if err := DecodePayload(req.Payload, &searchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.HybridSearch(ctx, &searchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleVectorSearchByID(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var searchReq service.SearchByIDRequest
	if err := DecodePayload(req.Payload, &searchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.SearchByID(ctx, &searchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Vector Index Management Handlers
// =============================================================================

func (h *Handler) handleVectorRebuildIndex(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var rebuildReq service.RebuildIndexRequest
	if err := DecodePayload(req.Payload, &rebuildReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.RebuildIndex(ctx, &rebuildReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleVectorGetRebuildProgress(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var progressReq service.GetRebuildProgressRequest
	if err := DecodePayload(req.Payload, &progressReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.GetRebuildProgress(ctx, &progressReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleVectorGetIndexStats(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var statsReq service.GetIndexStatsRequest
	if err := DecodePayload(req.Payload, &statsReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.GetIndexStats(ctx, &statsReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Vector Compaction Operations Handlers
// =============================================================================

func (h *Handler) handleVectorCompact(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var compactReq service.CompactNamespaceRequest
	if err := DecodePayload(req.Payload, &compactReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.CompactNamespace(ctx, &compactReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleVectorGetCompactionProgress(ctx context.Context, req *Request) *Response {
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("vector"))
	}

	var progressReq service.GetCompactionProgressRequest
	if err := DecodePayload(req.Payload, &progressReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := vectorService.GetCompactionProgress(ctx, &progressReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Vector Extended Operations Dispatch
// =============================================================================

// handleVectorExtendedDispatch routes vector extended operations based on sub-opcode in payload.
// The vector extended operations use OpScheduler (0x00) as the main opcode with sub-operation
// dispatch via the first byte of the payload in the 0x40-0x4F range.
func (h *Handler) handleVectorExtendedDispatch(ctx context.Context, req *Request) *Response {
	// Payload must contain at least sub-opcode byte
	if len(req.Payload) < 1 {
		return h.errorResponse(api.NewMissingSubOpcodeError(
			"quic.handleVectorExtendedDispatch", "vector extended"))
	}

	// Extract sub-opcode
	subOpcode := req.Payload[0]

	// Create sub-request with payload minus sub-opcode byte
	subReq := &Request{
		Opcode:  req.Opcode,
		Token:   req.Token,
		Payload: req.Payload[1:], // Skip sub-opcode byte
	}

	switch subOpcode {
	case SubOpVectorBatchDelete:
		return h.handleVectorBatchDelete(ctx, subReq)
	case SubOpVectorListVectors:
		return h.handleVectorListVectors(ctx, subReq)
	case SubOpVectorUpdateMetadata:
		return h.handleVectorUpdateMetadata(ctx, subReq)
	default:
		return h.errorResponse(api.NewUnknownSubOpcodeError(
			"quic.handleVectorExtendedDispatch", "vector extended", subOpcode))
	}
}
