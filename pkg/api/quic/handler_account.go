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
// Pooled provider account QUIC surface.
//
// Wire layout (identical to the orchestrator sub-dispatch it shares an opcode
// with):
//
//	[opcode=OpOrchestrator][tokenLen][token][payloadLen][subOp:1][json body]
//
// SECURITY INVARIANT: every response payload here is built from a
// service-layer response type, none of which carries an
// orchestration.AccountCredential. Credential material travels inbound only,
// on the create and rotate sub-ops.
// =============================================================================

// accountProvider is the narrow view of the composed service this handler
// needs. Mirrors orchestratorProvider in handler_orchestrator.go.
type accountProvider interface {
	Account() service.AccountService
}

// getAccountService retrieves the AccountService from the handler's service.
// Returns nil when the service does not expose one.
func (h *Handler) getAccountService() service.AccountService {
	provider, ok := h.service.(accountProvider)
	if !ok {
		return nil
	}
	return provider.Account()
}

// accountSubHandler handles one account sub-operation.
type accountSubHandler func(h *Handler, ctx context.Context, req *Request) *Response

// accountSubHandlers is the O(1) sub-opcode dispatch table for the account
// range carried on OpOrchestrator.
var accountSubHandlers = map[byte]accountSubHandler{
	SubOpAccountCreate:     (*Handler).handleAccountCreate,
	SubOpAccountGet:        (*Handler).handleAccountGet,
	SubOpAccountList:       (*Handler).handleAccountList,
	SubOpAccountUpdate:     (*Handler).handleAccountUpdate,
	SubOpAccountDelete:     (*Handler).handleAccountDelete,
	SubOpAccountRotate:     (*Handler).handleAccountRotate,
	SubOpAccountRevoke:     (*Handler).handleAccountRevoke,
	SubOpAccountSetDefault: (*Handler).handleAccountSetDefault,
	SubOpAccountUsage:      (*Handler).handleAccountUsage,
}

// handleAccountDispatch routes one account sub-operation. Called by
// handleOrchestratorDispatch for sub-opcodes in the account range.
func (h *Handler) handleAccountDispatch(ctx context.Context, subOpcode byte, req *Request) *Response {
	handler, ok := accountSubHandlers[subOpcode]
	if !ok {
		return h.errorResponse(api.NewUnknownSubOpcodeError(
			"quic.handleAccountDispatch", "account", subOpcode))
	}
	return handler(h, ctx, req)
}

// =============================================================================
// Sub-operation handlers
// =============================================================================

func (h *Handler) handleAccountCreate(ctx context.Context, req *Request) *Response {
	accountSvc := h.getAccountService()
	if accountSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Account"))
	}
	var svcReq service.CreateAccountRequest
	if err := DecodePayload(req.Payload, &svcReq); err != nil {
		return h.errorResponse(err)
	}
	resp, err := accountSvc.CreateAccount(ctx, &svcReq)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.accountResponse(resp)
}

func (h *Handler) handleAccountGet(ctx context.Context, req *Request) *Response {
	accountSvc := h.getAccountService()
	if accountSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Account"))
	}
	var svcReq service.GetAccountRequest
	if err := DecodePayload(req.Payload, &svcReq); err != nil {
		return h.errorResponse(err)
	}
	resp, err := accountSvc.GetAccount(ctx, &svcReq)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.accountResponse(resp)
}

func (h *Handler) handleAccountList(ctx context.Context, req *Request) *Response {
	accountSvc := h.getAccountService()
	if accountSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Account"))
	}
	var svcReq service.ListAccountsRequest
	if err := DecodePayload(req.Payload, &svcReq); err != nil {
		return h.errorResponse(err)
	}
	resp, err := accountSvc.ListAccounts(ctx, &svcReq)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.accountResponse(resp)
}

func (h *Handler) handleAccountUpdate(ctx context.Context, req *Request) *Response {
	accountSvc := h.getAccountService()
	if accountSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Account"))
	}
	var svcReq service.UpdateAccountRequest
	if err := DecodePayload(req.Payload, &svcReq); err != nil {
		return h.errorResponse(err)
	}
	resp, err := accountSvc.UpdateAccount(ctx, &svcReq)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.accountResponse(resp)
}

func (h *Handler) handleAccountDelete(ctx context.Context, req *Request) *Response {
	accountSvc := h.getAccountService()
	if accountSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Account"))
	}
	var svcReq service.DeleteAccountRequest
	if err := DecodePayload(req.Payload, &svcReq); err != nil {
		return h.errorResponse(err)
	}
	resp, err := accountSvc.DeleteAccount(ctx, &svcReq)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.accountResponse(resp)
}

func (h *Handler) handleAccountRotate(ctx context.Context, req *Request) *Response {
	accountSvc := h.getAccountService()
	if accountSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Account"))
	}
	var svcReq service.RotateAccountCredentialRequest
	if err := DecodePayload(req.Payload, &svcReq); err != nil {
		return h.errorResponse(err)
	}
	resp, err := accountSvc.RotateAccountCredential(ctx, &svcReq)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.accountResponse(resp)
}

func (h *Handler) handleAccountRevoke(ctx context.Context, req *Request) *Response {
	accountSvc := h.getAccountService()
	if accountSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Account"))
	}
	var svcReq service.RevokeAccountRequest
	if err := DecodePayload(req.Payload, &svcReq); err != nil {
		return h.errorResponse(err)
	}
	resp, err := accountSvc.RevokeAccount(ctx, &svcReq)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.accountResponse(resp)
}

func (h *Handler) handleAccountSetDefault(ctx context.Context, req *Request) *Response {
	accountSvc := h.getAccountService()
	if accountSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Account"))
	}
	var svcReq service.SetDefaultAccountRequest
	if err := DecodePayload(req.Payload, &svcReq); err != nil {
		return h.errorResponse(err)
	}
	resp, err := accountSvc.SetDefaultAccount(ctx, &svcReq)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.accountResponse(resp)
}

func (h *Handler) handleAccountUsage(ctx context.Context, req *Request) *Response {
	accountSvc := h.getAccountService()
	if accountSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Account"))
	}
	var svcReq service.GetAccountUsageRequest
	if err := DecodePayload(req.Payload, &svcReq); err != nil {
		return h.errorResponse(err)
	}
	resp, err := accountSvc.GetAccountUsage(ctx, &svcReq)
	if err != nil {
		return h.errorResponse(err)
	}
	return h.accountResponse(resp)
}

// accountResponse encodes a service-layer response into a wire response.
func (h *Handler) accountResponse(resp interface{}) *Response {
	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}
