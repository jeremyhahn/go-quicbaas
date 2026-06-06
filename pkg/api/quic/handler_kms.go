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
// KMS Operations Handlers
// =============================================================================

func (h *Handler) handleKMSGenerateKey(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var genReq service.GenerateKeyRequest
	if err := DecodePayload(req.Payload, &genReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.GenerateKey(ctx, &genReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSImportKey(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var importReq service.ImportKeyRequest
	if err := DecodePayload(req.Payload, &importReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.ImportKey(ctx, &importReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSGetKey(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var getReq service.GetKeyRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.GetKey(ctx, &getReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSListKeys(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var listReq service.ListKeysRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.ListKeys(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSRotateKey(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var rotateReq service.RotateKeyRequest
	if err := DecodePayload(req.Payload, &rotateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.RotateKey(ctx, &rotateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSDeleteKey(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var deleteReq service.DeleteKeyRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.DeleteKey(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSEncrypt(ctx context.Context, req *Request) *Response {
	if h.cryptoService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("crypto"))
	}

	var encReq service.EncryptRequest
	if err := DecodePayload(req.Payload, &encReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.cryptoService.Encrypt(ctx, &encReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSDecrypt(ctx context.Context, req *Request) *Response {
	if h.cryptoService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("crypto"))
	}

	var decReq service.DecryptRequest
	if err := DecodePayload(req.Payload, &decReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.cryptoService.Decrypt(ctx, &decReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// KMS DEK Operations Handlers
// =============================================================================

func (h *Handler) handleKMSDEKCreate(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var createReq service.CreateDEKRequest
	if err := DecodePayload(req.Payload, &createReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.CreateDEK(ctx, &createReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSDEKGet(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var getReq service.GetDEKRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.GetDEK(ctx, &getReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSDEKList(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var listReq service.ListDEKsRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.ListDEKs(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSDEKRotate(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var rotateReq service.RotateDEKRequest
	if err := DecodePayload(req.Payload, &rotateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.RotateDEK(ctx, &rotateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSDEKDelete(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var deleteReq service.DeleteDEKRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.DeleteDEK(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSDEKStatus(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var statusReq service.GetDEKStatusRequest
	if err := DecodePayload(req.Payload, &statusReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.GetDEKStatus(ctx, &statusReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSDEKSetDefault(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var setDefaultReq service.SetDefaultDEKRequest
	if err := DecodePayload(req.Payload, &setDefaultReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.SetDefaultDEK(ctx, &setDefaultReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSDEKSetSafety(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var setSafetyReq service.SetDEKSafetyRequest
	if err := DecodePayload(req.Payload, &setSafetyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.SetDEKSafety(ctx, &setSafetyReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSDEKClearSafety(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var clearSafetyReq service.ClearDEKSafetyRequest
	if err := DecodePayload(req.Payload, &clearSafetyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.ClearDEKSafety(ctx, &clearSafetyReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleKMSDEKStartReencryption handles DEK re-encryption start requests.
func (h *Handler) handleKMSDEKStartReencryption(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var reencryptReq service.StartReencryptionRequest
	if err := DecodePayload(req.Payload, &reencryptReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.StartReencryption(ctx, &reencryptReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleKMSDEKGetReencryptProgress handles DEK re-encryption progress requests.
func (h *Handler) handleKMSDEKGetReencryptProgress(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var progressReq service.GetReencryptionProgressRequest
	if err := DecodePayload(req.Payload, &progressReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.GetReencryptionProgress(ctx, &progressReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// KMS Backend Operations Handlers
// =============================================================================

func (h *Handler) handleKMSBackendStatus(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	resp, err := h.kmsService.BackendStatus(ctx)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSBackendTest(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	resp, err := h.kmsService.BackendTest(ctx)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSKeyVersions(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var listVersionsReq service.ListKeyVersionsRequest
	if err := DecodePayload(req.Payload, &listVersionsReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.ListKeyVersions(ctx, &listVersionsReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSKeyStatus(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var updateStatusReq service.UpdateKeyStatusRequest
	if err := DecodePayload(req.Payload, &updateStatusReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.UpdateKeyStatus(ctx, &updateStatusReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// KMS Policy Operations Handlers
// =============================================================================

func (h *Handler) handleKMSSetKeyPolicy(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var policyReq service.SetKeyPolicyRequest
	if err := DecodePayload(req.Payload, &policyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.SetKeyPolicy(ctx, &policyReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSGetKeyPolicy(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var policyReq service.GetKeyPolicyRequest
	if err := DecodePayload(req.Payload, &policyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.GetKeyPolicy(ctx, &policyReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// KMS Rotation Policy Handlers
// =============================================================================

func (h *Handler) handleKMSSetRotationPolicy(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var svcReq service.SetKeyRotationPolicyRequest
	if err := DecodePayload(req.Payload, &svcReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.SetKeyRotationPolicy(ctx, &svcReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSGetRotationPolicy(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var svcReq service.GetKeyRotationPolicyRequest
	if err := DecodePayload(req.Payload, &svcReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.GetKeyRotationPolicy(ctx, &svcReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSDeleteRotationPolicy(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var svcReq service.DeleteKeyRotationPolicyRequest
	if err := DecodePayload(req.Payload, &svcReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.DeleteKeyRotationPolicy(ctx, &svcReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// KMS Safety Operations Handlers
// =============================================================================

func (h *Handler) handleKMSSetKeySafety(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var safetyReq service.SetKeySafetyRequest
	if err := DecodePayload(req.Payload, &safetyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.SetKeySafety(ctx, &safetyReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSClearKeySafety(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var safetyReq service.ClearKeySafetyRequest
	if err := DecodePayload(req.Payload, &safetyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.ClearKeySafety(ctx, &safetyReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSSafetyCheck(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var checkReq service.SafetyCheckRequest
	if err := DecodePayload(req.Payload, &checkReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.SafetyCheck(ctx, &checkReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSSafetyReport(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var reportReq service.SafetyReportRequest
	if err := DecodePayload(req.Payload, &reportReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.SafetyReport(ctx, &reportReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// KMS Export/Unwrap Operations Handlers
// =============================================================================

func (h *Handler) handleKMSExportKey(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var exportReq service.ExportKeyRequest
	if err := DecodePayload(req.Payload, &exportReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.ExportKey(ctx, &exportReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSUnwrapKey(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var unwrapReq service.UnwrapKeyRequest
	if err := DecodePayload(req.Payload, &unwrapReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.UnwrapKey(ctx, &unwrapReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// KMS Metadata Operations Handlers
// =============================================================================

func (h *Handler) handleKMSGetKeyMetadata(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var metadataReq service.GetKeyMetadataRequest
	if err := DecodePayload(req.Payload, &metadataReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.GetKeyMetadata(ctx, &metadataReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleKMSUpdateKeyMetadata(ctx context.Context, req *Request) *Response {
	if h.kmsService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("KMS"))
	}

	var metadataReq service.UpdateKeyMetadataRequest
	if err := DecodePayload(req.Payload, &metadataReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.kmsService.UpdateKeyMetadata(ctx, &metadataReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
