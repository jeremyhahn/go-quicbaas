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
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Parameter Store Operations Handlers
// =============================================================================

// handleParameterGet retrieves a single parameter by path.
func (h *Handler) handleParameterGet(ctx context.Context, req *Request) *Response {
	paramService := h.service.ParameterStoreService()
	if paramService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("parameter store"))
	}

	var getReq interfaces.GetParameterRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := paramService.GetParameter(ctx, &getReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleParameterSet creates or updates a parameter.
func (h *Handler) handleParameterSet(ctx context.Context, req *Request) *Response {
	paramService := h.service.ParameterStoreService()
	if paramService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("parameter store"))
	}

	var setReq interfaces.SetParameterRequest
	if err := DecodePayload(req.Payload, &setReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := paramService.SetParameter(ctx, &setReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleParameterDelete removes a parameter by path.
func (h *Handler) handleParameterDelete(ctx context.Context, req *Request) *Response {
	paramService := h.service.ParameterStoreService()
	if paramService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("parameter store"))
	}

	var deleteReq interfaces.DeleteParameterRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := paramService.DeleteParameter(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleParameterList returns parameters matching a path prefix.
func (h *Handler) handleParameterList(ctx context.Context, req *Request) *Response {
	paramService := h.service.ParameterStoreService()
	if paramService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("parameter store"))
	}

	var listReq interfaces.ListParametersRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := paramService.ListParameters(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleParameterBatchGet retrieves multiple parameters by paths.
func (h *Handler) handleParameterBatchGet(ctx context.Context, req *Request) *Response {
	paramService := h.service.ParameterStoreService()
	if paramService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("parameter store"))
	}

	var batchReq interfaces.GetParametersRequest
	if err := DecodePayload(req.Payload, &batchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := paramService.GetParameters(ctx, &batchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleParameterBatchSet creates or updates multiple parameters atomically.
func (h *Handler) handleParameterBatchSet(ctx context.Context, req *Request) *Response {
	paramService := h.service.ParameterStoreService()
	if paramService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("parameter store"))
	}

	var batchReq interfaces.SetParametersRequest
	if err := DecodePayload(req.Payload, &batchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := paramService.SetParameters(ctx, &batchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleParameterBatchDelete removes multiple parameters by paths.
func (h *Handler) handleParameterBatchDelete(ctx context.Context, req *Request) *Response {
	paramService := h.service.ParameterStoreService()
	if paramService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("parameter store"))
	}

	var batchReq interfaces.DeleteParametersRequest
	if err := DecodePayload(req.Payload, &batchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := paramService.DeleteParameters(ctx, &batchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleParameterSearch finds parameters matching tag key-value pairs.
func (h *Handler) handleParameterSearch(ctx context.Context, req *Request) *Response {
	paramService := h.service.ParameterStoreService()
	if paramService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("parameter store"))
	}

	var searchReq interfaces.SearchByTagsRequest
	if err := DecodePayload(req.Payload, &searchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := paramService.SearchByTags(ctx, &searchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
