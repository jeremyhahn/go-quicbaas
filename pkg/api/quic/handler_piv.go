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

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// =============================================================================
// PIV Sub-Dispatch — OpOrchestrator (0x8F), sub-ops 0x80-0x87
// =============================================================================
//
// Wire format:
//
//	[opcode=OpOrchestrator][tokenLen][token][payloadLen][subOp:1][body...]
//
// payload[0] selects the verb; payload[1:] carries the JSON-encoded go-xkms
// transport request struct (go-xkms/sdk/go/transport). The PIV surface is the
// xkms PIVService contract verbatim, so the wire request and response shapes
// are the xkms transport types themselves — no quicbaas mirror structs.
//
// Every verb delegates to the service-layer PIV composition service
// (Security().Auth().PIV() server half), which forwards to the composed
// go-xkms service singleton. Authorization resolves per sub-op in
// handler_authz_qrdb.go against the piv:* family.

// pivSubHandler serves one PIV sub-operation. The body excludes the sub-op
// byte.
type pivSubHandler func(h *Handler, ctx context.Context, body []byte) *Response

// pivSubHandlers dispatches on the sub-op byte in constant time.
var pivSubHandlers = map[byte]pivSubHandler{
	SubOpPIVListSlots:   (*Handler).handlePIVListSlots,
	SubOpPIVGetCert:     (*Handler).handlePIVGetCert,
	SubOpPIVStoreCert:   (*Handler).handlePIVStoreCert,
	SubOpPIVDeleteCert:  (*Handler).handlePIVDeleteCert,
	SubOpPIVGenerateKey: (*Handler).handlePIVGenerateKey,
	SubOpPIVImportCert:  (*Handler).handlePIVImportCert,
	SubOpPIVExportCert:  (*Handler).handlePIVExportCert,
	SubOpPIVGenerateCSR: (*Handler).handlePIVGenerateCSR,
}

// handlePIVDispatch routes a PIV sub-request, whose sub-op byte has already
// been stripped by handleOrchestratorDispatch, to its handler. An unknown
// sub-op is rejected rather than silently resolved.
func (h *Handler) handlePIVDispatch(ctx context.Context, subOpcode byte, req *Request) *Response {
	handler, ok := pivSubHandlers[subOpcode]
	if !ok {
		return h.errorResponse(api.NewUnknownSubOpcodeError(
			"quic.handlePIVDispatch", "piv", subOpcode))
	}
	return handler(h, ctx, req.Payload)
}

// handlePIVListSlots returns the status of all PIV slots in a backend.
func (h *Handler) handlePIVListSlots(ctx context.Context, body []byte) *Response {
	pivService := h.service.PIV()
	if pivService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("piv"))
	}

	var req xkmstransport.ListPIVSlotsRequest
	if err := DecodePayload(body, &req); err != nil {
		return h.errorResponse(err)
	}

	resp, err := pivService.ListPIVSlots(ctx, &req)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handlePIVGetCert retrieves the certificate stored in a PIV slot.
func (h *Handler) handlePIVGetCert(ctx context.Context, body []byte) *Response {
	pivService := h.service.PIV()
	if pivService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("piv"))
	}

	var req xkmstransport.GetPIVCertificateRequest
	if err := DecodePayload(body, &req); err != nil {
		return h.errorResponse(err)
	}

	resp, err := pivService.GetPIVCertificate(ctx, &req)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handlePIVStoreCert stores a certificate in a PIV slot.
func (h *Handler) handlePIVStoreCert(ctx context.Context, body []byte) *Response {
	pivService := h.service.PIV()
	if pivService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("piv"))
	}

	var req xkmstransport.StorePIVCertificateRequest
	if err := DecodePayload(body, &req); err != nil {
		return h.errorResponse(err)
	}

	if err := pivService.StorePIVCertificate(ctx, &req); err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK}
}

// handlePIVDeleteCert removes the certificate from a PIV slot.
func (h *Handler) handlePIVDeleteCert(ctx context.Context, body []byte) *Response {
	pivService := h.service.PIV()
	if pivService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("piv"))
	}

	var req xkmstransport.DeletePIVCertificateRequest
	if err := DecodePayload(body, &req); err != nil {
		return h.errorResponse(err)
	}

	if err := pivService.DeletePIVCertificate(ctx, &req); err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK}
}

// handlePIVGenerateKey generates a new key pair in a PIV slot with a
// self-signed certificate.
func (h *Handler) handlePIVGenerateKey(ctx context.Context, body []byte) *Response {
	pivService := h.service.PIV()
	if pivService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("piv"))
	}

	var req xkmstransport.GeneratePIVKeyRequest
	if err := DecodePayload(body, &req); err != nil {
		return h.errorResponse(err)
	}

	resp, err := pivService.GeneratePIVKey(ctx, &req)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handlePIVImportCert imports a certificate into a PIV slot.
func (h *Handler) handlePIVImportCert(ctx context.Context, body []byte) *Response {
	pivService := h.service.PIV()
	if pivService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("piv"))
	}

	var req xkmstransport.StorePIVCertificateRequest
	if err := DecodePayload(body, &req); err != nil {
		return h.errorResponse(err)
	}

	if err := pivService.ImportPIVCertificate(ctx, &req); err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK}
}

// handlePIVExportCert exports the certificate from a PIV slot.
func (h *Handler) handlePIVExportCert(ctx context.Context, body []byte) *Response {
	pivService := h.service.PIV()
	if pivService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("piv"))
	}

	var req xkmstransport.GetPIVCertificateRequest
	if err := DecodePayload(body, &req); err != nil {
		return h.errorResponse(err)
	}

	resp, err := pivService.ExportPIVCertificate(ctx, &req)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handlePIVGenerateCSR generates a certificate signing request for a PIV
// slot key.
func (h *Handler) handlePIVGenerateCSR(ctx context.Context, body []byte) *Response {
	pivService := h.service.PIV()
	if pivService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("piv"))
	}

	var req xkmstransport.GeneratePIVCSRRequest
	if err := DecodePayload(body, &req); err != nil {
		return h.errorResponse(err)
	}

	resp, err := pivService.GeneratePIVCSR(ctx, &req)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}
