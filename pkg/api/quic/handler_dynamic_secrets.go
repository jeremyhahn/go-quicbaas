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
// Dynamic Secrets Operations Handlers
// =============================================================================

// handleDynamicSecretsCreateRole handles role creation for dynamic credentials.
func (h *Handler) handleDynamicSecretsCreateRole(ctx context.Context, req *Request) *Response {
	if h.dynamicSecretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("dynamic secrets"))
	}

	var createReq service.CreateRoleRequest
	if err := DecodePayload(req.Payload, &createReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.dynamicSecretsService.CreateRole(ctx, &createReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleDynamicSecretsGetRole handles retrieval of a credential generation role.
func (h *Handler) handleDynamicSecretsGetRole(ctx context.Context, req *Request) *Response {
	if h.dynamicSecretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("dynamic secrets"))
	}

	var getReq service.GetRoleRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.dynamicSecretsService.GetRole(ctx, &getReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleDynamicSecretsListRoles handles listing all credential generation roles.
func (h *Handler) handleDynamicSecretsListRoles(ctx context.Context, req *Request) *Response {
	if h.dynamicSecretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("dynamic secrets"))
	}

	var listReq service.ListRolesRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.dynamicSecretsService.ListRoles(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleDynamicSecretsDeleteRole handles deletion of a credential generation role.
func (h *Handler) handleDynamicSecretsDeleteRole(ctx context.Context, req *Request) *Response {
	if h.dynamicSecretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("dynamic secrets"))
	}

	var deleteReq service.DeleteRoleRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.dynamicSecretsService.DeleteRole(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleDynamicSecretsGenerate handles dynamic credential generation.
func (h *Handler) handleDynamicSecretsGenerate(ctx context.Context, req *Request) *Response {
	if h.dynamicSecretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("dynamic secrets"))
	}

	var genReq service.GenerateCredentialRequest
	if err := DecodePayload(req.Payload, &genReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.dynamicSecretsService.GenerateCredential(ctx, &genReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleDynamicSecretsRenewLease handles lease renewal for dynamic credentials.
func (h *Handler) handleDynamicSecretsRenewLease(ctx context.Context, req *Request) *Response {
	if h.dynamicSecretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("dynamic secrets"))
	}

	var renewReq service.RenewLeaseRequest
	if err := DecodePayload(req.Payload, &renewReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.dynamicSecretsService.RenewLease(ctx, &renewReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleDynamicSecretsRevokeLease handles lease revocation for dynamic credentials.
func (h *Handler) handleDynamicSecretsRevokeLease(ctx context.Context, req *Request) *Response {
	if h.dynamicSecretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("dynamic secrets"))
	}

	var revokeReq service.RevokeLeaseRequest
	if err := DecodePayload(req.Payload, &revokeReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.dynamicSecretsService.RevokeLease(ctx, &revokeReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleDynamicSecretsListLeases handles listing all active leases.
func (h *Handler) handleDynamicSecretsListLeases(ctx context.Context, req *Request) *Response {
	if h.dynamicSecretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("dynamic secrets"))
	}

	var listReq service.ListLeasesRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.dynamicSecretsService.ListLeases(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
