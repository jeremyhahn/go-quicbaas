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
	"encoding/base64"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Ceremony QUIC handlers — OpCeremony (0xC1) sub-op dispatcher.
//
// Mirrors the /api/v1/init REST surface exposed by
// pkg/api/rest/handlers_ceremony.go (8 methods) and the gRPC ceremony
// service. Every handler delegates to the CeremonyServicer facade wired
// into the QUIC server at lifecycle time. Until the facade is installed,
// every sub-handler short-circuits with a typed ServiceUnavailableError
// ("ceremony") which maps to wire status StatusUnavailable on the protocol.
//
// Wire format for OpCeremony requests:
//
//	[opcode=OpCeremony][tokenLen][token][payloadLen][subOp:1][body...]
//
// The first byte of the QUIC payload identifies the ceremony sub-operation
// (see SubOpCeremony* constants in protocol.go); the remaining bytes carry
// the JSON-encoded request body matching the wire request types declared in
// this file. Responses use the same JSON encoding as REST, less the HTTP
// envelope.
// =============================================================================

// ---------------------------------------------------------------------------
// Wire request types (local to this file; JSON shape only).
//
// These intentionally mirror the unexported REST request types
// (pkg/api/rest/handlers_ceremony.go) so REST and QUIC clients share an
// identical body schema and round-trip the same JSON bytes.
// ---------------------------------------------------------------------------

// ceremonyOfficerWire describes a single officer participating in the
// initialization ceremony.
type ceremonyOfficerWire struct {
	Username string `json:"username"`
	CSRPEM   string `json:"csr_pem"`
}

// ceremonyInitializeWireRequest is the JSON payload for
// SubOpCeremonyInitialize.
type ceremonyInitializeWireRequest struct {
	SOPin                  string                `json:"so_pin"`
	UserPin                string                `json:"user_pin"`
	CredentialSealStrategy string                `json:"credential_seal_strategy,omitempty"`
	Threshold              int                   `json:"threshold"`
	Officers               []ceremonyOfficerWire `json:"officers,omitempty"`
	Hostname               string                `json:"hostname,omitempty"`
	SANs                   []string              `json:"sans,omitempty"`
}

// ceremonyClaimCertBeginWireRequest is the JSON payload for
// SubOpCeremonyClaimCertBegin.
type ceremonyClaimCertBeginWireRequest struct {
	Username string `json:"username"`
}

// ceremonyClaimCertCompleteWireRequest is the JSON payload for
// SubOpCeremonyClaimCertComplete.
type ceremonyClaimCertCompleteWireRequest struct {
	Username  string `json:"username"`
	NonceHex  string `json:"nonce_hex"`
	Signature string `json:"signature"` // base64-encoded
}

// ceremonyBeginClaimShareWireRequest is the JSON payload for
// SubOpCeremonyBeginClaimShare.
type ceremonyBeginClaimShareWireRequest struct {
	Username string `json:"username"`
}

// ceremonyCompleteClaimShareWireRequest is the JSON payload for
// SubOpCeremonyCompleteClaimShare.
type ceremonyCompleteClaimShareWireRequest struct {
	Username  string `json:"username"`
	NonceHex  string `json:"nonce_hex"`
	Signature string `json:"signature"` // base64-encoded
}

// ceremonySignCSRWireRequest is the JSON payload for SubOpCeremonySignCSR.
type ceremonySignCSRWireRequest struct {
	Username string `json:"username"`
	SOPin    string `json:"so_pin"`
	CSRPEM   string `json:"csr_pem"`
	Role     string `json:"role"`
}

// ceremonyEnrollExternalOfficerWireRequest is the JSON payload for
// SubOpCeremonyEnrollExternalOfficer.
type ceremonyEnrollExternalOfficerWireRequest struct {
	Username   string `json:"username"`
	CertPEM    string `json:"cert_pem,omitempty"`
	PubKeyPEM  string `json:"pubkey_pem,omitempty"`
	ShareIndex int    `json:"share_index"`
	AuthFactor string `json:"auth_factor"`
	ShareBytes []byte `json:"share_bytes,omitempty"`
}

// ---------------------------------------------------------------------------
// Wire response types
// ---------------------------------------------------------------------------

// ceremonyInitWireResponse mirrors ceremonyInitToResponse from REST.
// Certificate/key PEM blobs are emitted as strings to match REST exactly.
type ceremonyInitWireResponse struct {
	SPKIPin    string `json:"spki_pin"`
	CACertPEM  string `json:"ca_cert_pem"`
	TLSCertPEM string `json:"tls_cert_pem"`
	State      string `json:"state"`
	ShareCount int    `json:"share_count"`
}

// ceremonyStatusWireResponse mirrors ceremonyStatusToResponse.
type ceremonyStatusWireResponse struct {
	State   string `json:"state"`
	SPKIPin string `json:"spki_pin,omitempty"`
}

// ceremonyChallengeWireResponse mirrors ceremonyChallengeToResponse.
// ExpiresAt is RFC3339 UTC; empty when zero.
type ceremonyChallengeWireResponse struct {
	NonceHex  string `json:"nonce_hex"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// ceremonyClaimCertWireResponse mirrors ceremonyClaimCertToResponse.
type ceremonyClaimCertWireResponse struct {
	CertPEM   string `json:"cert_pem"`
	CACertPEM string `json:"ca_cert_pem"`
}

// ceremonyCompleteClaimShareWireResponse mirrors the REST JSON shape
// returned by HandleCeremonyCompleteClaimShare (and the qrdb officer-share
// wire contract: share_index + share_json).
type ceremonyCompleteClaimShareWireResponse struct {
	ShareIndex int    `json:"share_index"`
	ShareJSON  string `json:"share_json"`
}

// ceremonySignCSRWireResponse mirrors ceremonySignCSRToResponse.
type ceremonySignCSRWireResponse struct {
	CertPEM string `json:"cert_pem"`
}

// ceremonyListOfficersWireResponse mirrors the JSON envelope returned by
// HandleCeremonyListOfficers. Officers is intentionally nilable so an
// empty registry serializes as `{"officers":[]}` rather than
// `{"officers":null}`.
type ceremonyListOfficersWireResponse struct {
	Officers []interface{} `json:"officers"`
}

// ceremonyEnrollExternalOfficerWireResponse mirrors
// enrollExternalOfficerResponse from REST.
type ceremonyEnrollExternalOfficerWireResponse struct {
	SealedShare string `json:"sealed_share,omitempty"`
}

// ---------------------------------------------------------------------------
// Dispatcher
// ---------------------------------------------------------------------------

// handleCeremonyDispatch routes ceremony operations based on the sub-opcode
// in the first byte of the QUIC request payload. Wire opcode is OpCeremony
// (0xC1); payload[0] selects one of the SubOpCeremony* operations declared
// in protocol.go.
//
// On dispatch failure the request is rejected with StatusInvalidArgument
// (missing or unknown sub-opcode).
func (h *Handler) handleCeremonyDispatch(ctx context.Context, req *Request) *Response {
	if len(req.Payload) < 1 {
		return h.errorResponse(api.NewMissingSubOpcodeError(
			"quic.handleCeremonyDispatch", "ceremony"))
	}

	subOpcode := req.Payload[0]
	subReq := &Request{
		Opcode:  req.Opcode,
		Token:   req.Token,
		Payload: req.Payload[1:],
	}

	handler, ok := ceremonySubOpHandlers[subOpcode]
	if !ok {
		return h.errorResponse(api.NewUnknownSubOpcodeError(
			"quic.handleCeremonyDispatch", "ceremony", subOpcode))
	}
	return handler(h, ctx, subReq)
}

// ceremonySubOpHandler is the dispatch function signature for ceremony
// sub-operations.
type ceremonySubOpHandler func(h *Handler, ctx context.Context, req *Request) *Response

// ceremonySubOpHandlers maps ceremony sub-opcodes to their dispatch handlers
// for O(1) constant-time routing.
var ceremonySubOpHandlers = map[byte]ceremonySubOpHandler{
	SubOpCeremonyInitialize:            (*Handler).handleCeremonyInitialize,
	SubOpCeremonyStatus:                (*Handler).handleCeremonyStatus,
	SubOpCeremonyClaimCertBegin:        (*Handler).handleCeremonyClaimCertBegin,
	SubOpCeremonyClaimCertComplete:     (*Handler).handleCeremonyClaimCertComplete,
	SubOpCeremonyBeginClaimShare:       (*Handler).handleCeremonyBeginClaimShare,
	SubOpCeremonyCompleteClaimShare:    (*Handler).handleCeremonyCompleteClaimShare,
	SubOpCeremonySignCSR:               (*Handler).handleCeremonySignCSR,
	SubOpCeremonyListOfficers:          (*Handler).handleCeremonyListOfficers,
	SubOpCeremonyEnrollExternalOfficer: (*Handler).handleCeremonyEnrollExternalOfficer,
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// requireCeremonyService returns the wired CeremonyServicer or an error
// response carrying StatusUnavailable when the facade has not been installed.
func (h *Handler) requireCeremonyService() (service.CeremonyServicer, *Response) {
	if h.ceremonyService == nil {
		return nil, h.errorResponse(api.NewServiceUnavailableError("ceremony"))
	}
	return h.ceremonyService, nil
}

// ---------------------------------------------------------------------------
// Handlers (8 — REST parity)
// ---------------------------------------------------------------------------

// handleCeremonyInitialize starts the initialization ceremony.
// REST equivalent: POST /api/v1/init/initialize.
func (h *Handler) handleCeremonyInitialize(ctx context.Context, req *Request) *Response {
	svc, errResp := h.requireCeremonyService()
	if errResp != nil {
		return errResp
	}

	var body ceremonyInitializeWireRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}

	officers := make([]service.CeremonyOfficer, 0, len(body.Officers))
	for _, o := range body.Officers {
		officers = append(officers, service.CeremonyOfficer{
			Username: o.Username,
			CSRPEM:   o.CSRPEM,
		})
	}

	info, err := svc.CeremonyInitialize(ctx, &service.CeremonyInitRequest{
		SOPin:                  body.SOPin,
		UserPin:                body.UserPin,
		CredentialSealStrategy: body.CredentialSealStrategy,
		Threshold:              body.Threshold,
		Officers:               officers,
		Hostname:               body.Hostname,
		SANs:                   body.SANs,
	})
	if err != nil {
		return h.errorResponse(err)
	}

	resp := &ceremonyInitWireResponse{
		SPKIPin:    info.SPKIPin,
		CACertPEM:  string(info.CACertPEM),
		TLSCertPEM: string(info.TLSCertPEM),
		State:      info.State,
		ShareCount: info.ShareCount,
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handleCeremonyStatus returns the current ceremony state.
// REST equivalent: GET /api/v1/init/status.
func (h *Handler) handleCeremonyStatus(ctx context.Context, _ *Request) *Response {
	svc, errResp := h.requireCeremonyService()
	if errResp != nil {
		return errResp
	}

	info, err := svc.CeremonyStatus(ctx)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(&ceremonyStatusWireResponse{
		State:   info.State,
		SPKIPin: info.SPKIPin,
	})
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handleCeremonyClaimCertBegin starts a certificate claim challenge.
// REST equivalent: POST /api/v1/init/claim-cert/begin.
func (h *Handler) handleCeremonyClaimCertBegin(ctx context.Context, req *Request) *Response {
	svc, errResp := h.requireCeremonyService()
	if errResp != nil {
		return errResp
	}

	var body ceremonyClaimCertBeginWireRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if body.Username == "" {
		return h.errorResponse(api.NewRequiredFieldError("username"))
	}

	challenge, err := svc.CeremonyBeginClaimCert(ctx, body.Username)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(&ceremonyChallengeWireResponse{
		NonceHex:  challenge.NonceHex,
		ExpiresAt: formatRFC3339(challenge.ExpiresAt),
	})
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handleCeremonyClaimCertComplete completes a certificate claim with a
// signed nonce.
// REST equivalent: POST /api/v1/init/claim-cert/complete.
func (h *Handler) handleCeremonyClaimCertComplete(ctx context.Context, req *Request) *Response {
	svc, errResp := h.requireCeremonyService()
	if errResp != nil {
		return errResp
	}

	var body ceremonyClaimCertCompleteWireRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if body.Username == "" {
		return h.errorResponse(api.NewRequiredFieldError("username"))
	}
	if body.NonceHex == "" {
		return h.errorResponse(api.NewRequiredFieldError("nonce_hex"))
	}
	if body.Signature == "" {
		return h.errorResponse(api.NewRequiredFieldError("signature"))
	}

	result, err := svc.CeremonyCompleteClaimCert(ctx, body.Username, body.NonceHex, body.Signature)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(&ceremonyClaimCertWireResponse{
		CertPEM:   string(result.CertPEM),
		CACertPEM: string(result.CACertPEM),
	})
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handleCeremonyBeginClaimShare starts the Shamir share claim handshake
// for an officer, returning the challenge nonce to sign.
// REST equivalent: POST /api/v1/init/officer-share/begin.
func (h *Handler) handleCeremonyBeginClaimShare(ctx context.Context, req *Request) *Response {
	svc, errResp := h.requireCeremonyService()
	if errResp != nil {
		return errResp
	}

	var body ceremonyBeginClaimShareWireRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if body.Username == "" {
		return h.errorResponse(api.NewRequiredFieldError("username"))
	}

	challenge, err := svc.CeremonyBeginClaimShare(ctx, body.Username)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(&ceremonyChallengeWireResponse{
		NonceHex:  challenge.NonceHex,
		ExpiresAt: formatRFC3339(challenge.ExpiresAt),
	})
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handleCeremonyCompleteClaimShare completes the Shamir share claim
// handshake with the signed challenge nonce; the share is released exactly
// once.
// REST equivalent: POST /api/v1/init/officer-share/complete.
func (h *Handler) handleCeremonyCompleteClaimShare(ctx context.Context, req *Request) *Response {
	svc, errResp := h.requireCeremonyService()
	if errResp != nil {
		return errResp
	}

	var body ceremonyCompleteClaimShareWireRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if body.Username == "" {
		return h.errorResponse(api.NewRequiredFieldError("username"))
	}
	if body.NonceHex == "" {
		return h.errorResponse(api.NewRequiredFieldError("nonce_hex"))
	}
	if body.Signature == "" {
		return h.errorResponse(api.NewRequiredFieldError("signature"))
	}
	sig, decErr := base64.StdEncoding.DecodeString(body.Signature)
	if decErr != nil {
		return h.errorResponse(api.NewInvalidFormatMessageError(
			"signature", "signature must be base64-encoded"))
	}

	result, err := svc.CeremonyCompleteClaimShare(ctx, body.Username, body.NonceHex, sig)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(&ceremonyCompleteClaimShareWireResponse{
		ShareIndex: result.ShareIndex,
		ShareJSON:  result.ShareJSON,
	})
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handleCeremonySignCSR signs a CSR during the ceremony.
// REST equivalent: POST /api/v1/init/sign-csr.
func (h *Handler) handleCeremonySignCSR(ctx context.Context, req *Request) *Response {
	svc, errResp := h.requireCeremonyService()
	if errResp != nil {
		return errResp
	}

	var body ceremonySignCSRWireRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if body.Username == "" {
		return h.errorResponse(api.NewRequiredFieldError("username"))
	}
	if body.CSRPEM == "" {
		return h.errorResponse(api.NewRequiredFieldError("csr_pem"))
	}

	result, err := svc.CeremonySignCSR(ctx, body.Username, body.SOPin, body.CSRPEM, body.Role)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(&ceremonySignCSRWireResponse{CertPEM: string(result.CertPEM)})
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handleCeremonyListOfficers returns the persistent ceremony officer
// registry. While the registry is empty (no officers enrolled yet) the
// array is returned empty rather than not-found, matching REST.
// REST equivalent: GET /api/v1/init/officers.
//
// The persistent ceremony-officer roster is exposed by the ceremony
// servicer in a forthcoming milestone; until then the QUIC handler
// returns an empty array to match the REST contract exactly.
func (h *Handler) handleCeremonyListOfficers(_ context.Context, _ *Request) *Response {
	if _, errResp := h.requireCeremonyService(); errResp != nil {
		return errResp
	}

	payload, err := EncodePayload(&ceremonyListOfficersWireResponse{
		Officers: []interface{}{},
	})
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handleCeremonyEnrollExternalOfficer enrols an officer whose key material
// lives outside the daemon.
// REST equivalent: POST /api/v1/init/enroll-external-officer.
func (h *Handler) handleCeremonyEnrollExternalOfficer(ctx context.Context, req *Request) *Response {
	svc, errResp := h.requireCeremonyService()
	if errResp != nil {
		return errResp
	}

	var body ceremonyEnrollExternalOfficerWireRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if body.Username == "" {
		return h.errorResponse(api.NewRequiredFieldError("username"))
	}
	if body.CertPEM == "" && body.PubKeyPEM == "" {
		return h.errorResponse(api.NewInvalidFormatMessageError(
			"cert_pem|pubkey_pem", "exactly one of cert_pem or pubkey_pem is required"))
	}
	if body.CertPEM != "" && body.PubKeyPEM != "" {
		return h.errorResponse(api.NewInvalidFormatMessageError(
			"cert_pem|pubkey_pem", "cert_pem and pubkey_pem are mutually exclusive"))
	}
	if body.AuthFactor == "" {
		return h.errorResponse(api.NewRequiredFieldError("auth_factor"))
	}

	resp, err := svc.CeremonyEnrollExternalOfficer(ctx, &service.CeremonyEnrollExternalOfficerRequest{
		Username:   body.Username,
		CertPEM:    body.CertPEM,
		PubKeyPEM:  body.PubKeyPEM,
		ShareIndex: body.ShareIndex,
		AuthFactor: body.AuthFactor,
		ShareBytes: body.ShareBytes,
	})
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(&ceremonyEnrollExternalOfficerWireResponse{
		SealedShare: resp.SealedShare,
	})
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}
