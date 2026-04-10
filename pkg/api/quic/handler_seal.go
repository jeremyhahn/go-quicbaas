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
// Seal Operations Handlers
// =============================================================================

func (h *Handler) handleSealStatus(ctx context.Context, req *Request) *Response {
	if h.sealService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("seal"))
	}

	resp, err := h.sealService.Status(ctx)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleSealInit(ctx context.Context, req *Request) *Response {
	if h.sealService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("seal"))
	}

	var initReq service.SealInitRequest
	if err := DecodePayload(req.Payload, &initReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.sealService.Initialize(ctx, &initReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleUnseal(ctx context.Context, req *Request) *Response {
	if h.sealService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("seal"))
	}

	var unsealReq service.UnsealRequest
	if err := DecodePayload(req.Payload, &unsealReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.sealService.Unseal(ctx, &unsealReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleSeal(ctx context.Context, req *Request) *Response {
	if h.sealService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("seal"))
	}

	err := h.sealService.Seal(ctx)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: nil}
}

func (h *Handler) handleSealRekey(ctx context.Context, req *Request) *Response {
	if h.sealService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("seal"))
	}

	var rekeyReq service.RekeyRequest
	if err := DecodePayload(req.Payload, &rekeyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.sealService.Rekey(ctx, &rekeyReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
