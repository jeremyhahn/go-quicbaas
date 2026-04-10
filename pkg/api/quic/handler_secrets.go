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
// Secrets Operations Handlers
// =============================================================================

func (h *Handler) handleSecretsStore(ctx context.Context, req *Request) *Response {
	if h.secretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("secrets"))
	}

	var storeReq service.StoreSecretRequest
	if err := DecodePayload(req.Payload, &storeReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.secretsService.StoreSecret(ctx, &storeReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleSecretsGet(ctx context.Context, req *Request) *Response {
	if h.secretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("secrets"))
	}

	var getReq service.GetSecretRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.secretsService.GetSecret(ctx, &getReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleSecretsList(ctx context.Context, req *Request) *Response {
	if h.secretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("secrets"))
	}

	var listReq service.ListSecretsRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.secretsService.ListSecrets(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleSecretsDelete(ctx context.Context, req *Request) *Response {
	if h.secretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("secrets"))
	}

	var deleteReq service.DeleteSecretRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.secretsService.DeleteSecret(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleSecretsListVersions(ctx context.Context, req *Request) *Response {
	if h.secretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("secrets"))
	}

	var listReq service.ListSecretVersionsRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.secretsService.ListSecretVersions(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleSecretsRollback(ctx context.Context, req *Request) *Response {
	if h.secretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("secrets"))
	}

	var rollbackReq service.RollbackSecretRequest
	if err := DecodePayload(req.Payload, &rollbackReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.secretsService.RollbackSecret(ctx, &rollbackReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleSecretsRotate(ctx context.Context, req *Request) *Response {
	if h.secretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("secrets"))
	}

	var rotateReq service.RotateSecretRequest
	if err := DecodePayload(req.Payload, &rotateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.secretsService.RotateSecret(ctx, &rotateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleSecretsGetVersion retrieves a specific version of a secret.
func (h *Handler) handleSecretsGetVersion(ctx context.Context, req *Request) *Response {
	if h.secretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("secrets"))
	}

	var getReq service.GetSecretVersionRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.secretsService.GetSecretVersion(ctx, &getReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleSecretsSetRotation sets the automatic rotation policy for a secret.
func (h *Handler) handleSecretsSetRotation(ctx context.Context, req *Request) *Response {
	if h.secretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("secrets"))
	}

	var setReq service.SetRotationPolicyRequest
	if err := DecodePayload(req.Payload, &setReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.secretsService.SetRotationPolicy(ctx, &setReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleSecretsGetRotation retrieves the current rotation policy for a secret.
func (h *Handler) handleSecretsGetRotation(ctx context.Context, req *Request) *Response {
	if h.secretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("secrets"))
	}

	var getReq service.GetRotationPolicyRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.secretsService.GetRotationPolicy(ctx, &getReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleSecretsUpdateStatus enables or disables a secret.
func (h *Handler) handleSecretsUpdateStatus(ctx context.Context, req *Request) *Response {
	if h.secretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("secrets"))
	}

	var updateReq service.UpdateSecretStatusRequest
	if err := DecodePayload(req.Payload, &updateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.secretsService.UpdateSecretStatus(ctx, &updateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleSecretsGetByPrefix retrieves all secrets matching a path prefix.
func (h *Handler) handleSecretsGetByPrefix(ctx context.Context, req *Request) *Response {
	if h.secretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("secrets"))
	}

	var getReq service.GetSecretsByPrefixRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.secretsService.GetSecretsByPrefix(ctx, &getReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleSecretsWatch handles streaming watch requests for secrets.
// Note: QUIC streaming is handled differently - this collects changes into a batch response.
// For true streaming, clients should use the gRPC API or establish persistent QUIC streams.
func (h *Handler) handleSecretsWatch(ctx context.Context, req *Request) *Response {
	if h.secretsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("secrets"))
	}

	var watchReq service.WatchSecretsRequest
	if err := DecodePayload(req.Payload, &watchReq); err != nil {
		return h.errorResponse(err)
	}

	changes, err := h.secretsService.WatchSecretsByPrefix(ctx, &watchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	// Collect changes into a batch response
	// The watch will continue until context is cancelled or channel is closed
	var collected []*service.SecretChange
	for change := range changes {
		collected = append(collected, change)
		// For QUIC, we return after receiving one change for simplicity
		// Clients wanting continuous updates should reconnect or use gRPC streaming
		break
	}

	payload, err := EncodePayload(collected)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
