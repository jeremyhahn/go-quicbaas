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
// FIDO2 Sub-Dispatch — OpOrchestrator (0x8F), sub-ops 0x90-0x93
// =============================================================================
//
// Wire format:
//
//	[opcode=OpOrchestrator][tokenLen][token][payloadLen][subOp:1][body...]
//
// payload[0] selects the verb; payload[1:] carries the JSON-encoded go-xkms
// transport request struct with a "tenant_id" field injected alongside it by
// the SDK transport (transport.TenantFromContext), the same enrichment the
// orchestrator verbs use. The ceremony payloads themselves are the xkms
// transport types verbatim — no quicbaas mirror structs.
//
// Every verb delegates to the service-layer FIDO2 composition service
// (Security().Auth().FIDO2() server half), which resolves the tenant's xkms
// relying-party service and forwards through go-xkms's canonical FIDO2
// servicer adapter.
//
// Authorization mirrors the REST canonical mount: registration verbs resolve
// user_authfactors:add (handler_authz_qrdb.go); BeginAuthentication /
// FinishAuthentication are pre-auth (isPublicSubOperation below) because
// WebAuthn IS the authentication — the caller holds no bearer until
// FinishAuthentication mints one.

// isPublicSubOperation reports whether a sub-operation carried on a dispatch
// opcode requires no authentication. It complements isPublicOperation, which
// only sees the top-level opcode: the FIDO2 authentication ceremony rides the
// OpOrchestrator dispatch byte, so the pre-auth decision needs payload[0].
// Only the two FIDO2 login-ceremony verbs are public — exactly the sub-op
// equivalent of OpAuthBeginLogin / OpAuthFinishLogin.
func isPublicSubOperation(opcode byte, payload []byte) bool {
	if opcode != OpOrchestrator || len(payload) < 1 {
		return false
	}
	switch payload[0] {
	case SubOpFIDO2BeginAuthentication, SubOpFIDO2FinishAuthentication:
		return true
	default:
		return false
	}
}

// fido2SubHandler serves one FIDO2 sub-operation. The body excludes the
// sub-op byte.
type fido2SubHandler func(h *Handler, ctx context.Context, body []byte) *Response

// fido2SubHandlers dispatches on the sub-op byte in constant time.
var fido2SubHandlers = map[byte]fido2SubHandler{
	SubOpFIDO2BeginRegistration:    (*Handler).handleFIDO2BeginRegistration,
	SubOpFIDO2FinishRegistration:   (*Handler).handleFIDO2FinishRegistration,
	SubOpFIDO2BeginAuthentication:  (*Handler).handleFIDO2BeginAuthentication,
	SubOpFIDO2FinishAuthentication: (*Handler).handleFIDO2FinishAuthentication,
}

// handleFIDO2Dispatch routes a FIDO2 sub-request, whose sub-op byte has
// already been stripped by handleOrchestratorDispatch, to its handler. An
// unknown sub-op is rejected rather than silently resolved.
func (h *Handler) handleFIDO2Dispatch(ctx context.Context, subOpcode byte, req *Request) *Response {
	handler, ok := fido2SubHandlers[subOpcode]
	if !ok {
		return h.errorResponse(api.NewUnknownSubOpcodeError(
			"quic.handleFIDO2Dispatch", "fido2", subOpcode))
	}
	return handler(h, ctx, req.Payload)
}

// fido2BeginRegistrationWire is the QUIC body for BeginRegistration: the
// xkms transport request plus the tenant scope injected by the SDK.
type fido2BeginRegistrationWire struct {
	TenantID string `json:"tenant_id"`
	xkmstransport.BeginRegistrationRequest
}

// fido2FinishRegistrationWire is the QUIC body for FinishRegistration.
type fido2FinishRegistrationWire struct {
	TenantID string `json:"tenant_id"`
	xkmstransport.FinishRegistrationRequest
}

// fido2BeginAuthenticationWire is the QUIC body for BeginAuthentication.
type fido2BeginAuthenticationWire struct {
	TenantID string `json:"tenant_id"`
	xkmstransport.BeginAuthenticationRequest
}

// fido2FinishAuthenticationWire is the QUIC body for FinishAuthentication.
type fido2FinishAuthenticationWire struct {
	TenantID string `json:"tenant_id"`
	xkmstransport.FinishAuthenticationRequest
}

// handleFIDO2BeginRegistration starts a WebAuthn registration ceremony for
// the tenant's relying party.
func (h *Handler) handleFIDO2BeginRegistration(ctx context.Context, body []byte) *Response {
	fido2Service := h.service.FIDO2()
	if fido2Service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("fido2"))
	}

	var req fido2BeginRegistrationWire
	if err := DecodePayload(body, &req); err != nil {
		return h.errorResponse(err)
	}

	resp, err := fido2Service.BeginRegistration(ctx, req.TenantID, &req.BeginRegistrationRequest)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handleFIDO2FinishRegistration completes a WebAuthn registration ceremony.
func (h *Handler) handleFIDO2FinishRegistration(ctx context.Context, body []byte) *Response {
	fido2Service := h.service.FIDO2()
	if fido2Service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("fido2"))
	}

	var req fido2FinishRegistrationWire
	if err := DecodePayload(body, &req); err != nil {
		return h.errorResponse(err)
	}

	resp, err := fido2Service.FinishRegistration(ctx, req.TenantID, &req.FinishRegistrationRequest)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handleFIDO2BeginAuthentication starts a WebAuthn authentication ceremony.
func (h *Handler) handleFIDO2BeginAuthentication(ctx context.Context, body []byte) *Response {
	fido2Service := h.service.FIDO2()
	if fido2Service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("fido2"))
	}

	var req fido2BeginAuthenticationWire
	if err := DecodePayload(body, &req); err != nil {
		return h.errorResponse(err)
	}

	resp, err := fido2Service.BeginAuthentication(ctx, req.TenantID, &req.BeginAuthenticationRequest)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handleFIDO2FinishAuthentication completes a WebAuthn authentication
// ceremony and returns the relying party's post-authentication token.
func (h *Handler) handleFIDO2FinishAuthentication(ctx context.Context, body []byte) *Response {
	fido2Service := h.service.FIDO2()
	if fido2Service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("fido2"))
	}

	var req fido2FinishAuthenticationWire
	if err := DecodePayload(body, &req); err != nil {
		return h.errorResponse(err)
	}

	resp, err := fido2Service.FinishAuthentication(ctx, req.TenantID, &req.FinishAuthenticationRequest)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}
