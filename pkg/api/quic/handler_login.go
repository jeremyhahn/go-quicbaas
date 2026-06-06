//go:build quic

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// =============================================================================
// Unified Login Ceremony QUIC handlers.
//
// These handlers delegate to the QuicBaaS LoginCeremony() facade. The verified
// mTLS peer certificate is threaded onto the request context by the QUIC server
// (handleStream) from the connection's VerifiedChains; the facade derives the
// mtls factor lookup key from it (never from the client-supplied request body).
// =============================================================================

// handleBeginLogin routes the BeginLogin step to the LoginCeremony facade.
func (h *Handler) handleBeginLogin(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}
	login := h.service.LoginCeremony()
	if login == nil {
		return h.errorResponse(api.NewServiceUnavailableError("login ceremony"))
	}

	var beginReq qrdbtransport.BeginLoginRequest
	if err := DecodePayload(req.Payload, &beginReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := login.BeginLogin(ctx, &beginReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handleFinishLogin routes the FinishLogin step to the LoginCeremony facade.
// The ctx already carries the verified peer certificate (set by the server).
func (h *Handler) handleFinishLogin(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}
	login := h.service.LoginCeremony()
	if login == nil {
		return h.errorResponse(api.NewServiceUnavailableError("login ceremony"))
	}

	var finishReq qrdbtransport.FinishLoginRequest
	if err := DecodePayload(req.Payload, &finishReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := login.FinishLogin(ctx, &finishReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}
