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
// Ollama Operations Handlers
// =============================================================================

// handleOllamaEmbed handles embedding generation for a single text using the default model.
func (h *Handler) handleOllamaEmbed(ctx context.Context, req *Request) *Response {
	ollamaService := h.service.Ollama()
	if ollamaService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Ollama"))
	}

	var embedReq service.OllamaEmbedRequest
	if err := DecodePayload(req.Payload, &embedReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := ollamaService.Embed(ctx, &embedReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOllamaEmbedWithModel handles embedding generation using a specific model.
func (h *Handler) handleOllamaEmbedWithModel(ctx context.Context, req *Request) *Response {
	ollamaService := h.service.Ollama()
	if ollamaService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Ollama"))
	}

	var embedReq service.OllamaEmbedWithModelRequest
	if err := DecodePayload(req.Payload, &embedReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := ollamaService.EmbedWithModel(ctx, &embedReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOllamaEmbedBatch handles batch embedding generation for multiple texts.
func (h *Handler) handleOllamaEmbedBatch(ctx context.Context, req *Request) *Response {
	ollamaService := h.service.Ollama()
	if ollamaService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Ollama"))
	}

	var batchReq service.OllamaEmbedBatchRequest
	if err := DecodePayload(req.Payload, &batchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := ollamaService.EmbedBatch(ctx, &batchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOllamaListModels handles listing all available Ollama models.
func (h *Handler) handleOllamaListModels(ctx context.Context, req *Request) *Response {
	ollamaService := h.service.Ollama()
	if ollamaService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Ollama"))
	}

	var listReq service.OllamaListModelsRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := ollamaService.ListModels(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOllamaGetModelInfo handles retrieving detailed information about a specific model.
func (h *Handler) handleOllamaGetModelInfo(ctx context.Context, req *Request) *Response {
	ollamaService := h.service.Ollama()
	if ollamaService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Ollama"))
	}

	var infoReq service.OllamaGetModelInfoRequest
	if err := DecodePayload(req.Payload, &infoReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := ollamaService.GetModelInfo(ctx, &infoReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOllamaHealth handles Ollama health check requests.
func (h *Handler) handleOllamaHealth(ctx context.Context, req *Request) *Response {
	ollamaService := h.service.Ollama()
	if ollamaService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Ollama"))
	}

	var healthReq service.OllamaHealthRequest
	if err := DecodePayload(req.Payload, &healthReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := ollamaService.Health(ctx, &healthReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
