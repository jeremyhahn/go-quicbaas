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
// Secrets Sub-Dispatch Handler — OpSecretsSubDispatch (0x6F).
//
// Wire format for OpSecretsSubDispatch requests:
//
//	[opcode=OpSecretsSubDispatch][tokenLen][token][payloadLen][subOp:1][body...]
//
// payload[0] identifies the secrets sub-operation (see OpSecretsSubEnsure
// and other SubOpSecrets* constants in protocol.go); payload[1:] carries
// the JSON-encoded request body matching the service layer request type.
// Responses use the same JSON encoding as REST, less the HTTP envelope.
//
// Mirrors the barrier / ceremony / orchestrator sub-dispatch pattern exactly.
// =============================================================================

// secretsSubOpHandler is the dispatch function signature for secrets
// sub-operations. Using a typed function avoids any reflection overhead.
type secretsSubOpHandler func(h *Handler, ctx context.Context, req *Request) *Response

// secretsSubOpHandlers maps secrets sub-opcodes to their dispatch handlers.
// O(1) constant-time routing, identical to the barrier and orchestrator tables.
var secretsSubOpHandlers = map[byte]secretsSubOpHandler{
	OpSecretsSubEnsure: (*Handler).handleSecretsEnsure,
}

// handleSecretsSubDispatch routes secrets sub-dispatch operations based on the
// sub-opcode in payload[0]. Unknown sub-opcodes are rejected with
// StatusInvalidArgument so clients receive a clear, typed error.
func (h *Handler) handleSecretsSubDispatch(ctx context.Context, req *Request) *Response {
	if len(req.Payload) < 1 {
		return h.errorResponse(api.NewMissingSubOpcodeError(
			"quic.handleSecretsSubDispatch", "secrets"))
	}

	subOpcode := req.Payload[0]
	subReq := &Request{
		Opcode:  req.Opcode,
		Token:   req.Token,
		Payload: req.Payload[1:],
	}

	handler, ok := secretsSubOpHandlers[subOpcode]
	if !ok {
		return h.errorResponse(api.NewUnknownSubOpcodeError(
			"quic.handleSecretsSubDispatch", "secrets", subOpcode))
	}
	return handler(h, ctx, subReq)
}

// handleSecretsEnsure implements the Vault-style generate-on-first-access
// primitive over QUIC (OpSecretsSubDispatch / OpSecretsSubEnsure).
//
// It decodes a service.EnsureSecretRequest from the payload, validates that
// secret_id is present, delegates to the secrets service, and encodes the
// service.EnsureSecretResponse as the JSON payload of the QUIC response.
func (h *Handler) handleSecretsEnsure(ctx context.Context, req *Request) *Response {
	if h.secretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("secrets"))
	}

	var ensureReq service.EnsureSecretRequest
	if err := DecodePayload(req.Payload, &ensureReq); err != nil {
		return h.errorResponse(err)
	}

	if ensureReq.SecretID == "" {
		return h.errorResponse(api.NewInvalidInputError(
			"handleSecretsEnsure: secret_id is required"))
	}

	resp, err := h.secretsService.EnsureSecret(ctx, &ensureReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
