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
// Certificate Extension Sub-Dispatch — OpOrchestrator (0x8F), sub-ops 0x60-0x62
// =============================================================================
//
// Wire format:
//
//	[opcode=OpOrchestrator][tokenLen][token][payloadLen][subOp:1][body...]
//
// payload[0] selects the verb (SubOpCertExtCrossSign / SubOpCertExtChain /
// SubOpCertExtIssueDeviceIdentity); payload[1:] carries the JSON-encoded
// request body. The certificate extension range is disjoint from the
// orchestrator, account, project and SSH ranges that share the same dispatch
// byte. Authorization resolves per sub-op in handler_authz_qrdb.go so the
// three verbs authorize with the same permissions the REST, gRPC and MCP
// surfaces enforce.

// certExtSubHandler serves one certificate-extension sub-operation. The body
// excludes the sub-op byte.
type certExtSubHandler func(h *Handler, ctx context.Context, body []byte) *Response

// certExtSubHandlers dispatches on the sub-op byte in constant time.
var certExtSubHandlers = map[byte]certExtSubHandler{
	SubOpCertExtCrossSign:           (*Handler).handleCertCrossSign,
	SubOpCertExtChain:               (*Handler).handleCertChain,
	SubOpCertExtIssueDeviceIdentity: (*Handler).handleCertIssueDeviceIdentity,
}

// handleCertExtDispatch routes a certificate-extension sub-request, whose
// sub-op byte has already been stripped by handleOrchestratorDispatch, to its
// handler. An unknown sub-op is rejected rather than silently resolved.
func (h *Handler) handleCertExtDispatch(ctx context.Context, subOpcode byte, req *Request) *Response {
	handler, ok := certExtSubHandlers[subOpcode]
	if !ok {
		return h.errorResponse(api.NewUnknownSubOpcodeError(
			"quic.handleCertExtDispatch", "certificate-extension", subOpcode))
	}
	return handler(h, ctx, req.Payload)
}

// handleCertCrossSign cross-signs an external root certificate.
func (h *Handler) handleCertCrossSign(ctx context.Context, body []byte) *Response {
	certService := h.service.Certificate()
	if certService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("certificate"))
	}

	var req service.CrossSignRequest
	if err := DecodePayload(body, &req); err != nil {
		return h.errorResponse(err)
	}

	resp, err := certService.CrossSign(ctx, &req)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.encodeCertExtResponse(resp)
}

// handleCertChain resolves the issuer chain for a certificate.
func (h *Handler) handleCertChain(ctx context.Context, body []byte) *Response {
	certService := h.service.Certificate()
	if certService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("certificate"))
	}

	var req service.CertificateChainRequest
	if err := DecodePayload(body, &req); err != nil {
		return h.errorResponse(err)
	}

	resp, err := certService.Chain(ctx, &req)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.encodeCertExtResponse(resp)
}

// handleCertIssueDeviceIdentity issues a TCG device identity certificate.
func (h *Handler) handleCertIssueDeviceIdentity(ctx context.Context, body []byte) *Response {
	certService := h.service.Certificate()
	if certService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("certificate"))
	}

	var req service.IssueDeviceIdentityRequest
	if err := DecodePayload(body, &req); err != nil {
		return h.errorResponse(err)
	}

	resp, err := certService.IssueDeviceIdentity(ctx, &req)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.encodeCertExtResponse(resp)
}

// encodeCertExtResponse JSON-encodes a certificate-extension response. The
// canonical pkg/service response structs carry the same snake_case JSON tags
// as the leaf SDK types, so the client decodes the bytes directly.
func (h *Handler) encodeCertExtResponse(resp interface{}) *Response {
	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}
