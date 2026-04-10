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
// Embedding Operations Handlers
// =============================================================================

// handleEmbeddingGenerate handles embedding generation for a single text.
func (h *Handler) handleEmbeddingGenerate(ctx context.Context, req *Request) *Response {
	embeddingService := h.service.Embedding()
	if embeddingService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("embedding"))
	}

	var genReq service.GenerateEmbeddingRequest
	if err := DecodePayload(req.Payload, &genReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := embeddingService.GenerateEmbedding(ctx, &genReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleEmbeddingBatchGenerate handles batch embedding generation for multiple texts.
func (h *Handler) handleEmbeddingBatchGenerate(ctx context.Context, req *Request) *Response {
	embeddingService := h.service.Embedding()
	if embeddingService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("embedding"))
	}

	var batchReq service.BatchGenerateEmbeddingsRequest
	if err := DecodePayload(req.Payload, &batchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := embeddingService.BatchGenerateEmbeddings(ctx, &batchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleEmbeddingListProviders handles listing all available embedding providers.
func (h *Handler) handleEmbeddingListProviders(ctx context.Context, req *Request) *Response {
	embeddingService := h.service.Embedding()
	if embeddingService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("embedding"))
	}

	var listReq service.ListProvidersRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := embeddingService.ListProviders(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleEmbeddingGetProvider handles retrieving detailed information about a specific provider.
func (h *Handler) handleEmbeddingGetProvider(ctx context.Context, req *Request) *Response {
	embeddingService := h.service.Embedding()
	if embeddingService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("embedding"))
	}

	var infoReq service.GetProviderInfoRequest
	if err := DecodePayload(req.Payload, &infoReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := embeddingService.GetProviderInfo(ctx, &infoReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleEmbeddingSetDefault handles setting the default embedding provider.
func (h *Handler) handleEmbeddingSetDefault(ctx context.Context, req *Request) *Response {
	embeddingService := h.service.Embedding()
	if embeddingService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("embedding"))
	}

	var setReq service.SetDefaultProviderRequest
	if err := DecodePayload(req.Payload, &setReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := embeddingService.SetDefaultProvider(ctx, &setReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleEmbeddingGetCacheStats handles retrieving embedding cache statistics.
func (h *Handler) handleEmbeddingGetCacheStats(ctx context.Context, req *Request) *Response {
	embeddingService := h.service.Embedding()
	if embeddingService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("embedding"))
	}

	var statsReq service.GetEmbeddingCacheStatsRequest
	if err := DecodePayload(req.Payload, &statsReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := embeddingService.GetCacheStats(ctx, &statsReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleEmbeddingClearCache handles clearing the embedding cache.
func (h *Handler) handleEmbeddingClearCache(ctx context.Context, req *Request) *Response {
	embeddingService := h.service.Embedding()
	if embeddingService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("embedding"))
	}

	var clearReq service.ClearEmbeddingCacheRequest
	if err := DecodePayload(req.Payload, &clearReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := embeddingService.ClearCache(ctx, &clearReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
