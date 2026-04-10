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
// ObjectStorage Operations Handlers
// =============================================================================

// handleOSPutObject handles object storage put operations.
func (h *Handler) handleOSPutObject(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var putReq service.PutObjectRequest
	if err := DecodePayload(req.Payload, &putReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.PutObject(ctx, &putReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSGetObject handles object storage get operations.
func (h *Handler) handleOSGetObject(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var getReq service.GetObjectRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.GetObject(ctx, &getReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSDeleteObject handles object storage delete operations.
func (h *Handler) handleOSDeleteObject(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var deleteReq service.DeleteObjectRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.DeleteObject(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSHeadObject handles object storage head (metadata) operations.
func (h *Handler) handleOSHeadObject(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var headReq service.HeadObjectRequest
	if err := DecodePayload(req.Payload, &headReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.HeadObject(ctx, &headReq)
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
// ObjectStorage Listing Operations Handlers
// =============================================================================

// handleOSListObjects handles object storage list operations.
func (h *Handler) handleOSListObjects(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var listReq service.ListObjectsRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.ListObjects(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSListByTag handles object storage list by tag operations.
func (h *Handler) handleOSListByTag(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var listReq service.ListByTagRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.ListByTag(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSListByTenant handles object storage list by tenant operations.
func (h *Handler) handleOSListByTenant(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var listReq service.ListByTenantRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.ListByTenant(ctx, &listReq)
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
// ObjectStorage Copy Operations Handlers
// =============================================================================

// handleOSCopyObject handles object storage copy operations.
func (h *Handler) handleOSCopyObject(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var copyReq service.CopyObjectRequest
	if err := DecodePayload(req.Payload, &copyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.CopyObject(ctx, &copyReq)
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
// ObjectStorage Replication & Migration Operations Handlers
// =============================================================================

// handleOSReplicateObject handles object storage replication operations.
func (h *Handler) handleOSReplicateObject(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var replicateReq service.ReplicateObjectRequest
	if err := DecodePayload(req.Payload, &replicateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.ReplicateObject(ctx, &replicateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSMigrateObject handles object storage migration operations.
func (h *Handler) handleOSMigrateObject(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var migrateReq service.MigrateObjectRequest
	if err := DecodePayload(req.Payload, &migrateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.MigrateObject(ctx, &migrateReq)
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
// ObjectStorage Archive & Restore Operations Handlers
// =============================================================================

// handleOSArchiveObject handles object storage archive operations.
func (h *Handler) handleOSArchiveObject(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var archiveReq service.ArchiveObjectRequest
	if err := DecodePayload(req.Payload, &archiveReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.ArchiveObject(ctx, &archiveReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSRestoreObject handles object storage restore operations.
func (h *Handler) handleOSRestoreObject(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var restoreReq service.RestoreObjectRequest
	if err := DecodePayload(req.Payload, &restoreReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.RestoreObject(ctx, &restoreReq)
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
// ObjectStorage Verification Operations Handlers
// =============================================================================

// handleOSVerifyObject handles object storage verification operations.
func (h *Handler) handleOSVerifyObject(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var verifyReq service.VerifyObjectRequest
	if err := DecodePayload(req.Payload, &verifyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.VerifyObject(ctx, &verifyReq)
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
// ObjectStorage Metadata Operations Handlers
// =============================================================================

// handleOSGetObjectMetadata handles object storage get metadata operations.
// This uses HeadObject which returns object metadata without the data.
func (h *Handler) handleOSGetObjectMetadata(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var headReq service.HeadObjectRequest
	if err := DecodePayload(req.Payload, &headReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.HeadObject(ctx, &headReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSUpdateObjectMetadata handles object storage update metadata operations.
// This uses SetObjectPolicy to update object policies and metadata.
func (h *Handler) handleOSUpdateObjectMetadata(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var policyReq service.SetObjectPolicyRequest
	if err := DecodePayload(req.Payload, &policyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.SetObjectPolicy(ctx, &policyReq)
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
// ObjectStorage Stats Operations Handlers
// =============================================================================

// handleOSGetStorageStats handles object storage get storage stats operations.
// This uses GetTenantUsage to retrieve storage statistics for a tenant.
func (h *Handler) handleOSGetStorageStats(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var usageReq service.GetTenantUsageRequest
	if err := DecodePayload(req.Payload, &usageReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.GetTenantUsage(ctx, &usageReq)
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
// ObjectStorage Lifecycle Policy Operations Handlers
// =============================================================================

// handleOSSetLifecyclePolicy handles object storage set lifecycle policy operations.
func (h *Handler) handleOSSetLifecyclePolicy(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var policyReq service.SetObjectPolicyRequest
	if err := DecodePayload(req.Payload, &policyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.SetObjectPolicy(ctx, &policyReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSGetLifecyclePolicy handles object storage get lifecycle policy operations.
func (h *Handler) handleOSGetLifecyclePolicy(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var policyReq service.GetObjectPolicyRequest
	if err := DecodePayload(req.Payload, &policyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.GetObjectPolicy(ctx, &policyReq)
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
// ObjectStorage Extended Operations Handlers
// =============================================================================

// handleOSGetObjectPolicy handles object storage get object policy operations.
func (h *Handler) handleOSGetObjectPolicy(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var policyReq service.GetObjectPolicyRequest
	if err := DecodePayload(req.Payload, &policyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.GetObjectPolicy(ctx, &policyReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSListBackends handles object storage list backends operations.
func (h *Handler) handleOSListBackends(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var listReq service.ListBackendsRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.ListBackends(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSGetBackendHealth handles object storage get backend health operations.
func (h *Handler) handleOSGetBackendHealth(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var healthReq service.GetBackendHealthRequest
	if err := DecodePayload(req.Payload, &healthReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.GetBackendHealth(ctx, &healthReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSSetTenantQuotas handles object storage set tenant quotas operations.
func (h *Handler) handleOSSetTenantQuotas(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var quotasReq service.SetTenantQuotasRequest
	if err := DecodePayload(req.Payload, &quotasReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.SetTenantQuotas(ctx, &quotasReq)
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
// ObjectStorage Batch Operations Handlers
// =============================================================================

// handleOSBatchPutObjects handles batch put operations for multiple objects.
func (h *Handler) handleOSBatchPutObjects(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var batchReq service.BatchPutObjectsRequest
	if err := DecodePayload(req.Payload, &batchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.BatchPutObjects(ctx, &batchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSBatchGetObjects handles batch get operations for multiple objects.
func (h *Handler) handleOSBatchGetObjects(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var batchReq service.BatchGetObjectsRequest
	if err := DecodePayload(req.Payload, &batchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.BatchGetObjects(ctx, &batchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleOSBatchDeleteObjects handles batch delete operations for multiple objects.
func (h *Handler) handleOSBatchDeleteObjects(ctx context.Context, req *Request) *Response {
	objectStorageService := h.service.ObjectStorageService()
	if objectStorageService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectStorage"))
	}

	var batchReq service.BatchDeleteObjectsRequest
	if err := DecodePayload(req.Payload, &batchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := objectStorageService.BatchDeleteObjects(ctx, &batchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
