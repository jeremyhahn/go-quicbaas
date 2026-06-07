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

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Trust Operations Handlers
// =============================================================================
//
// These delegate to the service-layer TrustService, which composes the
// xkms TrustedCA cross-sign / AIA verbs. The wire request/response shapes
// are the canonical pkg/service.*Request/Response structs, whose JSON tags
// match the leaf client/trust types the SDK decodes. PublishDANE is wired
// and returns a typed not-configured error until a DANE publisher is
// registered on the server.

// handleTrustCrossSignedRootPool returns the active CA's cross-sign-aware
// root set in PEM form.
func (h *Handler) handleTrustCrossSignedRootPool(ctx context.Context, req *Request) *Response {
	trustService := h.service.Trust()
	if trustService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("trust"))
	}

	resp, err := trustService.CrossSignedRootPool(ctx, &service.CrossSignedRootPoolRequest{})
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleTrustAutoImportAIA walks the AIA chain of the supplied DER-encoded
// certificates and imports the referenced issuing CAs.
func (h *Handler) handleTrustAutoImportAIA(ctx context.Context, req *Request) *Response {
	trustService := h.service.Trust()
	if trustService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("trust"))
	}

	var aiaReq service.AutoImportAIARequest
	if err := DecodePayload(req.Payload, &aiaReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := trustService.AutoImportAIA(ctx, &aiaReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleTrustPublishDANE derives the DANE TLSA records for the supplied
// certificate and publishes them via the registered DANE publisher.
func (h *Handler) handleTrustPublishDANE(ctx context.Context, req *Request) *Response {
	trustService := h.service.Trust()
	if trustService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("trust"))
	}

	var daneReq service.PublishDANERequest
	if err := DecodePayload(req.Payload, &daneReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := trustService.PublishDANE(ctx, &daneReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
