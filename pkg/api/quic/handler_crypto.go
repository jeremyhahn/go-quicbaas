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
// Crypto Operations Handlers
// =============================================================================

func (h *Handler) handleCryptoEncrypt(ctx context.Context, req *Request) *Response {
	if h.cryptoService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("crypto"))
	}

	var encReq service.EncryptRequest
	if err := DecodePayload(req.Payload, &encReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.cryptoService.Encrypt(ctx, &encReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleCryptoDecrypt(ctx context.Context, req *Request) *Response {
	if h.cryptoService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("crypto"))
	}

	var decReq service.DecryptRequest
	if err := DecodePayload(req.Payload, &decReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.cryptoService.Decrypt(ctx, &decReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleCryptoRewrap(ctx context.Context, req *Request) *Response {
	if h.cryptoService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("crypto"))
	}

	var rewrapReq service.RewrapRequest
	if err := DecodePayload(req.Payload, &rewrapReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.cryptoService.Rewrap(ctx, &rewrapReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleCryptoSign(ctx context.Context, req *Request) *Response {
	if h.cryptoService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("crypto"))
	}

	var signReq service.SignRequest
	if err := DecodePayload(req.Payload, &signReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.cryptoService.Sign(ctx, &signReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleCryptoVerify(ctx context.Context, req *Request) *Response {
	if h.cryptoService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("crypto"))
	}

	var verifyReq service.VerifyRequest
	if err := DecodePayload(req.Payload, &verifyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.cryptoService.Verify(ctx, &verifyReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleCryptoHash(ctx context.Context, req *Request) *Response {
	if h.cryptoService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("crypto"))
	}

	var hashReq service.HashRequest
	if err := DecodePayload(req.Payload, &hashReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.cryptoService.Hash(ctx, &hashReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleCryptoRandom(ctx context.Context, req *Request) *Response {
	if h.cryptoService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("crypto"))
	}

	var randomReq service.GenerateRandomRequest
	if err := DecodePayload(req.Payload, &randomReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.cryptoService.GenerateRandom(ctx, &randomReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleCryptoDataKey(ctx context.Context, req *Request) *Response {
	if h.cryptoService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("crypto"))
	}

	var dataKeyReq service.GenerateDataKeyRequest
	if err := DecodePayload(req.Payload, &dataKeyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.cryptoService.GenerateDataKey(ctx, &dataKeyReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleCryptoGenerateHMAC generates an HMAC of data using a named key.
func (h *Handler) handleCryptoGenerateHMAC(ctx context.Context, req *Request) *Response {
	if h.cryptoService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("crypto"))
	}

	var hmacReq service.HMACRequest
	if err := DecodePayload(req.Payload, &hmacReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.cryptoService.GenerateHMAC(ctx, &hmacReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleCryptoVerifyHMAC verifies an HMAC against data using a named key.
func (h *Handler) handleCryptoVerifyHMAC(ctx context.Context, req *Request) *Response {
	if h.cryptoService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("crypto"))
	}

	var verifyReq service.VerifyHMACRequest
	if err := DecodePayload(req.Payload, &verifyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.cryptoService.VerifyHMAC(ctx, &verifyReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
