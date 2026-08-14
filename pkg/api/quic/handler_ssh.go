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

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	sshclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
)

// =============================================================================
// SSH QUIC surface.
//
// Wire layout (identical to the account and project sub-dispatches it shares
// an opcode with):
//
//	[opcode=OpOrchestrator][tokenLen][token][payloadLen][subOp:1][json body]
//
// Request and response bodies are the sdk/go/client/ssh wire envelopes, so
// the QUIC bytes match what the REST, gRPC, MCP, and embedded transports
// exchange. Every handler delegates to service.SSHService — no SSH logic
// lives here.
//
// SECURITY INVARIANT: no response body carries private key material. Import
// takes a PEM inbound and Sign takes a digest inbound; neither echoes the
// key back.
// =============================================================================

// sshProvider is the narrow view of the composed service this handler needs.
// It mirrors accountProvider in handler_account.go.
type sshProvider interface {
	SSHClient() xkmsssh.Client
}

// getSSHService builds the SSH service from the handler's composed service.
// Returns nil when the composed service does not expose an SSH client at all
// (a service implementation predating the SSH surface).
func (h *Handler) getSSHService() service.SSHService {
	provider, ok := h.service.(sshProvider)
	if !ok {
		return nil
	}
	return service.NewSSHService(provider.SSHClient())
}

// sshSubHandler handles one SSH sub-operation.
type sshSubHandler func(h *Handler, ctx context.Context, svc service.SSHService, req *Request) *Response

// sshSubHandlers is the O(1) sub-opcode dispatch table for the SSH range
// carried on OpOrchestrator.
var sshSubHandlers = map[byte]sshSubHandler{
	SubOpSSHGenerateKey:         (*Handler).handleSSHGenerateKey,
	SubOpSSHImportKey:           (*Handler).handleSSHImportKey,
	SubOpSSHGetKey:              (*Handler).handleSSHGetKey,
	SubOpSSHListKeys:            (*Handler).handleSSHListKeys,
	SubOpSSHDeleteKey:           (*Handler).handleSSHDeleteKey,
	SubOpSSHExportPublicKey:     (*Handler).handleSSHExportPublicKey,
	SubOpSSHSignDigest:          (*Handler).handleSSHSignDigest,
	SubOpSSHAddKnownHost:        (*Handler).handleSSHAddKnownHost,
	SubOpSSHListKnownHosts:      (*Handler).handleSSHListKnownHosts,
	SubOpSSHRemoveKnownHost:     (*Handler).handleSSHRemoveKnownHost,
	SubOpSSHAddAuthorizedKey:    (*Handler).handleSSHAddAuthorizedKey,
	SubOpSSHListAuthorizedKeys:  (*Handler).handleSSHListAuthorizedKeys,
	SubOpSSHRemoveAuthorizedKey: (*Handler).handleSSHRemoveAuthorizedKey,
	SubOpSSHEmitAuthorizedKey:   (*Handler).handleSSHEmitAuthorizedKey,
}

// handleSSHDispatch routes one SSH sub-operation. Called by
// handleOrchestratorDispatch for sub-opcodes in the SSH range.
func (h *Handler) handleSSHDispatch(ctx context.Context, subOpcode byte, req *Request) *Response {
	handler, ok := sshSubHandlers[subOpcode]
	if !ok {
		return h.errorResponse(api.NewUnknownSubOpcodeError(
			"quic.handleSSHDispatch", "ssh", subOpcode))
	}
	svc := h.getSSHService()
	if svc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("SSH"))
	}
	return handler(h, ctx, svc, req)
}

// =============================================================================
// Keys
// =============================================================================

func (h *Handler) handleSSHGenerateKey(ctx context.Context, svc service.SSHService, req *Request) *Response {
	var body sshclient.GenerateRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	key, err := svc.GenerateKey(ctx, body)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.sshResponse(key)
}

func (h *Handler) handleSSHImportKey(ctx context.Context, svc service.SSHService, req *Request) *Response {
	var body sshclient.ImportKeyRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	key, err := svc.ImportKey(ctx, service.SSHImportKeyRequest{
		Scope:         body.Scope,
		Owner:         body.Owner,
		Name:          body.Name,
		Comment:       body.Comment,
		PEM:           body.PEM,
		PassphraseB64: body.PassphraseB64,
	})
	if err != nil {
		return h.errorResponse(err)
	}
	return h.sshResponse(key)
}

func (h *Handler) handleSSHGetKey(ctx context.Context, svc service.SSHService, req *Request) *Response {
	var body sshclient.GetKeyRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	key, err := svc.GetKey(ctx, body)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.sshResponse(key)
}

func (h *Handler) handleSSHListKeys(ctx context.Context, svc service.SSHService, req *Request) *Response {
	var body sshclient.ListKeysRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	items, err := svc.ListKeys(ctx, body)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.sshResponse(&sshclient.ListKeysResponse{Items: items})
}

func (h *Handler) handleSSHDeleteKey(ctx context.Context, svc service.SSHService, req *Request) *Response {
	var body sshclient.DeleteKeyRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if err := svc.DeleteKey(ctx, body); err != nil {
		return h.errorResponse(err)
	}
	return h.sshResponse(&sshclient.AckResponse{Success: true})
}

func (h *Handler) handleSSHExportPublicKey(ctx context.Context, svc service.SSHService, req *Request) *Response {
	var body sshclient.ExportRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	pub, err := svc.ExportPublicKey(ctx, body)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.sshResponse(pub)
}

func (h *Handler) handleSSHSignDigest(ctx context.Context, svc service.SSHService, req *Request) *Response {
	var body sshclient.SignRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	sig, err := svc.SignDigest(ctx, service.SSHSignRequest{
		Scope: body.Scope, Owner: body.Owner, Name: body.Name, Digest: body.Digest,
	})
	if err != nil {
		return h.errorResponse(err)
	}
	return h.sshResponse(&sshclient.SignResponse{
		Format: sig.Format, Blob: sig.Blob, Rest: sig.Rest,
	})
}

// =============================================================================
// Known hosts
// =============================================================================

func (h *Handler) handleSSHAddKnownHost(ctx context.Context, svc service.SSHService, req *Request) *Response {
	var body sshclient.AddKnownHostRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if err := svc.AddKnownHost(ctx, body); err != nil {
		return h.errorResponse(err)
	}
	return h.sshResponse(&sshclient.AckResponse{Success: true})
}

func (h *Handler) handleSSHListKnownHosts(ctx context.Context, svc service.SSHService, req *Request) *Response {
	var body sshclient.ListKnownHostsRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	items, err := svc.ListKnownHosts(ctx, body)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.sshResponse(&sshclient.ListKnownHostsResponse{Items: items})
}

func (h *Handler) handleSSHRemoveKnownHost(ctx context.Context, svc service.SSHService, req *Request) *Response {
	var body sshclient.RemoveKnownHostRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if err := svc.RemoveKnownHost(ctx, body); err != nil {
		return h.errorResponse(err)
	}
	return h.sshResponse(&sshclient.AckResponse{Success: true})
}

// =============================================================================
// Authorized keys
// =============================================================================

func (h *Handler) handleSSHAddAuthorizedKey(ctx context.Context, svc service.SSHService, req *Request) *Response {
	var body sshclient.AddAuthorizedKeyRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if err := svc.AddAuthorizedKey(ctx, body); err != nil {
		return h.errorResponse(err)
	}
	return h.sshResponse(&sshclient.AckResponse{Success: true})
}

func (h *Handler) handleSSHListAuthorizedKeys(ctx context.Context, svc service.SSHService, req *Request) *Response {
	var body sshclient.ListAuthorizedKeysRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	items, err := svc.ListAuthorizedKeys(ctx, body)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.sshResponse(&sshclient.ListAuthorizedKeysResponse{Items: items})
}

func (h *Handler) handleSSHRemoveAuthorizedKey(ctx context.Context, svc service.SSHService, req *Request) *Response {
	var body sshclient.RemoveAuthorizedKeyRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if err := svc.RemoveAuthorizedKey(ctx, service.SSHAuthorizedKeyRef{
		Scope: body.Scope, Owner: body.Owner, Name: body.Name,
	}); err != nil {
		return h.errorResponse(err)
	}
	return h.sshResponse(&sshclient.AckResponse{Success: true})
}

func (h *Handler) handleSSHEmitAuthorizedKey(ctx context.Context, svc service.SSHService, req *Request) *Response {
	var body sshclient.EmitRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	line, err := svc.EmitAuthorizedKey(ctx, body)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.sshResponse(&sshclient.EmitResponse{Line: line})
}

// sshResponse encodes a value into a wire response.
func (h *Handler) sshResponse(v interface{}) *Response {
	payload, err := EncodePayload(v)
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}
