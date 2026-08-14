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
// Attestation Sub-Dispatch — OpOrchestrator (0x8F), sub-ops 0x70-0x72
// =============================================================================
//
// Wire format:
//
//	[opcode=OpOrchestrator][tokenLen][token][payloadLen][subOp:1][body...]
//
// payload[0] selects the verb; payload[1:] carries the JSON-encoded request
// body. The attestation range is disjoint from the orchestrator, account,
// project, SSH and certificate-extension ranges that share the same dispatch
// byte — the single-byte top-level opcode space is fully allocated (see the
// note above SubOpCertExt* in protocol.go), so a new surface takes a sub-op
// range rather than a new opcode.
//
// Authorization resolves per sub-op in handler_authz_qrdb.go against
// attestation:verify / attestation:attest, so the three verbs authorize with
// exactly the permissions the gRPC, REST and MCP surfaces enforce rather than
// inheriting orchestrator:status from the shared dispatch byte.

// attestationSubHandler serves one attestation sub-operation. The body
// excludes the sub-op byte.
type attestationSubHandler func(h *Handler, ctx context.Context, body []byte) *Response

// attestationSubHandlers dispatches on the sub-op byte in constant time.
var attestationSubHandlers = map[byte]attestationSubHandler{
	SubOpAttestationVerify:          (*Handler).handleAttestationVerify,
	SubOpAttestationVerifyChain:     (*Handler).handleAttestationVerifyChain,
	SubOpAttestationAttestContainer: (*Handler).handleAttestationAttestContainer,
}

// handleAttestationDispatch routes an attestation sub-request, whose sub-op
// byte has already been stripped by handleOrchestratorDispatch, to its
// handler. An unknown sub-op is rejected rather than silently resolved.
func (h *Handler) handleAttestationDispatch(ctx context.Context, subOpcode byte, req *Request) *Response {
	handler, ok := attestationSubHandlers[subOpcode]
	if !ok {
		return h.errorResponse(api.NewUnknownSubOpcodeError(
			"quic.handleAttestationDispatch", "attestation", subOpcode))
	}
	return handler(h, ctx, req.Payload)
}

// handleAttestationVerify validates a complete attestation statement.
func (h *Handler) handleAttestationVerify(ctx context.Context, body []byte) *Response {
	attestationService := h.service.Attestation()
	if attestationService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("attestation"))
	}

	var req service.VerifyAttestationRequest
	if err := DecodePayload(body, &req); err != nil {
		return h.errorResponse(err)
	}

	resp, err := attestationService.Verify(ctx, &req)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.encodeAttestationResponse(resp)
}

// handleAttestationVerifyChain validates only the certificate chain of an
// attestation statement.
func (h *Handler) handleAttestationVerifyChain(ctx context.Context, body []byte) *Response {
	attestationService := h.service.Attestation()
	if attestationService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("attestation"))
	}

	var req service.VerifyAttestationChainRequest
	if err := DecodePayload(body, &req); err != nil {
		return h.errorResponse(err)
	}

	resp, err := attestationService.VerifyChain(ctx, &req)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.encodeAttestationResponse(resp)
}

// handleAttestationAttestContainer crosses the orchestration seam to the
// registered container attestor.
func (h *Handler) handleAttestationAttestContainer(ctx context.Context, body []byte) *Response {
	attestationService := h.service.Attestation()
	if attestationService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("attestation"))
	}

	var req service.AttestContainerRequest
	if err := DecodePayload(body, &req); err != nil {
		return h.errorResponse(err)
	}

	resp, err := attestationService.AttestContainer(ctx, &req)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.encodeAttestationResponse(resp)
}

// encodeAttestationResponse JSON-encodes an attestation response. The
// canonical pkg/service response structs carry the same snake_case JSON tags
// as the leaf SDK types, so the client decodes the bytes directly.
func (h *Handler) encodeAttestationResponse(resp interface{}) *Response {
	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}
