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

package embedded

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	objclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorage"
)

// =============================================================================
// ObjectStorage Handlers
// =============================================================================

func handleObjPutObject(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.PutObjectRequest); ok {
		svcResp, err := objSvc.PutObject(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcPutObjectResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.PutObjectRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "PutObject",
			"*objectstorage.PutObjectRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.PutObjectRequest{
		Key:               sdkReq.Key,
		Value:             sdkReq.Value,
		ContentType:       sdkReq.ContentType,
		TenantID:          sdkReq.TenantID,
		ShardID:           sdkReq.ShardID,
		Tags:              sdkReq.Tags,
		ReplicationPolicy: convertSDKReplicationPolicy(sdkReq.ReplicationPolicy),
		ArchivePolicy:     convertSDKArchivePolicy(sdkReq.ArchivePolicy),
		RetentionPolicy:   convertSDKRetentionPolicy(sdkReq.RetentionPolicy),
		TargetBackend:     sdkReq.TargetBackend,
		Checksum:          sdkReq.Checksum,
	}

	svcResp, err := objSvc.PutObject(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcPutObjectResponse(svcResp), nil
}

func handleObjGetObject(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.GetObjectRequest); ok {
		svcResp, err := objSvc.GetObject(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcGetObjectResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.GetObjectRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "GetObject",
			"*objectstorage.GetObjectRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.GetObjectRequest{
		Key:             sdkReq.Key,
		TenantID:        sdkReq.TenantID,
		IncludeMetadata: sdkReq.IncludeMetadata,
	}

	svcResp, err := objSvc.GetObject(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcGetObjectResponse(svcResp), nil
}

// objStreamChunkSize matches the 64 KiB chunks the gRPC StreamGetObject sends.
const objStreamChunkSize = 64 * 1024

// handleObjStreamGetObject serves StreamGetObject: it reads the object through
// the same service method as the gRPC server and delivers it as
// *objectstorage.ObjectChunk messages. An empty object is one empty chunk
// marked last.
func handleObjStreamGetObject(ctx context.Context, svc service.QuicBaaS, req interface{}, s *stream) error {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return NewServiceNotConfiguredError("ObjectStorage")
	}

	sdkReq, ok := req.(*objclient.GetObjectRequest)
	if !ok || sdkReq == nil {
		return NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "StreamGetObject",
			"*objectstorage.GetObjectRequest",
			typeNameOf(req),
		)
	}

	svcResp, err := objSvc.GetObject(ctx, &service.GetObjectRequest{
		Key:      sdkReq.Key,
		TenantID: sdkReq.TenantID,
	})
	if err != nil {
		return err
	}

	data := svcResp.Value
	totalSize := int64(len(data))
	if totalSize == 0 {
		return s.deliver(&objclient.ObjectChunk{IsLast: true})
	}

	for offset := int64(0); offset < totalSize; offset += objStreamChunkSize {
		end := min(offset+objStreamChunkSize, totalSize)
		chunk := &objclient.ObjectChunk{
			Data:      data[offset:end],
			Offset:    offset,
			IsLast:    end == totalSize,
			TotalSize: totalSize,
		}
		if err := s.deliver(chunk); err != nil {
			return err
		}
	}
	return nil
}

func handleObjDeleteObject(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.DeleteObjectRequest); ok {
		svcResp, err := objSvc.DeleteObject(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcDeleteObjectResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.DeleteObjectRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "DeleteObject",
			"*objectstorage.DeleteObjectRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.DeleteObjectRequest{
		Key:      sdkReq.Key,
		TenantID: sdkReq.TenantID,
	}

	svcResp, err := objSvc.DeleteObject(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcDeleteObjectResponse(svcResp), nil
}

func handleObjHeadObject(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.HeadObjectRequest); ok {
		svcResp, err := objSvc.HeadObject(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcHeadObjectResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.HeadObjectRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "HeadObject",
			"*objectstorage.HeadObjectRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.HeadObjectRequest{
		Key:      sdkReq.Key,
		TenantID: sdkReq.TenantID,
	}

	svcResp, err := objSvc.HeadObject(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcHeadObjectResponse(svcResp), nil
}

func handleObjListObjects(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.ListObjectsRequest); ok {
		svcResp, err := objSvc.ListObjects(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcListObjectsResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.ListObjectsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "ListObjects",
			"*objectstorage.ListObjectsRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.ListObjectsRequest{
		Prefix:      sdkReq.Prefix,
		TenantID:    sdkReq.TenantID,
		Limit:       sdkReq.Limit,
		Cursor:      sdkReq.Cursor,
		IncludeTags: sdkReq.IncludeTags,
	}

	svcResp, err := objSvc.ListObjects(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcListObjectsResponse(svcResp), nil
}

func handleObjListByTag(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.ListByTagRequest); ok {
		svcResp, err := objSvc.ListByTag(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcListByTagResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.ListByTagRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "ListByTag",
			"*objectstorage.ListByTagRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.ListByTagRequest{
		TagName:  sdkReq.TagName,
		TagValue: sdkReq.TagValue,
		TenantID: sdkReq.TenantID,
		Limit:    sdkReq.Limit,
		Cursor:   sdkReq.Cursor,
	}

	svcResp, err := objSvc.ListByTag(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcListByTagResponse(svcResp), nil
}

func handleObjListByTenant(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.ListByTenantRequest); ok {
		svcResp, err := objSvc.ListByTenant(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcListByTenantResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.ListByTenantRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "ListByTenant",
			"*objectstorage.ListByTenantRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.ListByTenantRequest{
		TenantID:    sdkReq.TenantID,
		Limit:       sdkReq.Limit,
		Cursor:      sdkReq.Cursor,
		IncludeTags: sdkReq.IncludeTags,
	}

	svcResp, err := objSvc.ListByTenant(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcListByTenantResponse(svcResp), nil
}

func handleObjCopyObject(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.CopyObjectRequest); ok {
		svcResp, err := objSvc.CopyObject(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcCopyObjectResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.CopyObjectRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "CopyObject",
			"*objectstorage.CopyObjectRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.CopyObjectRequest{
		SourceKey:         sdkReq.SourceKey,
		DestinationKey:    sdkReq.DestinationKey,
		TenantID:          sdkReq.TenantID,
		OverwriteExisting: sdkReq.OverwriteExisting,
		TargetBackend:     sdkReq.TargetBackend,
	}

	svcResp, err := objSvc.CopyObject(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcCopyObjectResponse(svcResp), nil
}

func handleObjReplicateObject(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.ReplicateObjectRequest); ok {
		svcResp, err := objSvc.ReplicateObject(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcReplicateObjectResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.ReplicateObjectRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "ReplicateObject",
			"*objectstorage.ReplicateObjectRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.ReplicateObjectRequest{
		Key:         sdkReq.Key,
		Targets:     sdkReq.Targets,
		TenantID:    sdkReq.TenantID,
		Priority:    sdkReq.Priority,
		Synchronous: sdkReq.Synchronous,
	}

	svcResp, err := objSvc.ReplicateObject(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcReplicateObjectResponse(svcResp), nil
}

func handleObjMigrateObject(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.MigrateObjectRequest); ok {
		svcResp, err := objSvc.MigrateObject(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcMigrateObjectResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.MigrateObjectRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "MigrateObject",
			"*objectstorage.MigrateObjectRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.MigrateObjectRequest{
		Key:              sdkReq.Key,
		TargetBackend:    sdkReq.TargetBackend,
		TenantID:         sdkReq.TenantID,
		DeleteFromSource: sdkReq.DeleteFromSource,
	}

	svcResp, err := objSvc.MigrateObject(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcMigrateObjectResponse(svcResp), nil
}

func handleObjArchiveObject(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.ArchiveObjectRequest); ok {
		svcResp, err := objSvc.ArchiveObject(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcArchiveObjectResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.ArchiveObjectRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "ArchiveObject",
			"*objectstorage.ArchiveObjectRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.ArchiveObjectRequest{
		Key:               sdkReq.Key,
		TargetStore:       sdkReq.TargetStore,
		TenantID:          sdkReq.TenantID,
		DeleteFromPrimary: sdkReq.DeleteFromPrimary,
	}

	svcResp, err := objSvc.ArchiveObject(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcArchiveObjectResponse(svcResp), nil
}

func handleObjRestoreObject(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.RestoreObjectRequest); ok {
		svcResp, err := objSvc.RestoreObject(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcRestoreObjectResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.RestoreObjectRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "RestoreObject",
			"*objectstorage.RestoreObjectRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.RestoreObjectRequest{
		Key:           sdkReq.Key,
		TargetBackend: sdkReq.TargetBackend,
		TenantID:      sdkReq.TenantID,
	}

	svcResp, err := objSvc.RestoreObject(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcRestoreObjectResponse(svcResp), nil
}

func handleObjVerifyObject(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.VerifyObjectRequest); ok {
		svcResp, err := objSvc.VerifyObject(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcVerifyObjectResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.VerifyObjectRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "VerifyObject",
			"*objectstorage.VerifyObjectRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.VerifyObjectRequest{
		Key:              sdkReq.Key,
		Stores:           sdkReq.Stores,
		TenantID:         sdkReq.TenantID,
		RepairMismatches: sdkReq.RepairMismatches,
	}

	svcResp, err := objSvc.VerifyObject(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcVerifyObjectResponse(svcResp), nil
}

func handleObjSetObjectPolicy(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.SetObjectPolicyRequest); ok {
		svcResp, err := objSvc.SetObjectPolicy(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcSetObjectPolicyResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.SetObjectPolicyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "SetObjectPolicy",
			"*objectstorage.SetObjectPolicyRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.SetObjectPolicyRequest{
		Key:               sdkReq.Key,
		TenantID:          sdkReq.TenantID,
		ReplicationPolicy: convertSDKReplicationPolicy(sdkReq.ReplicationPolicy),
		ArchivePolicy:     convertSDKArchivePolicy(sdkReq.ArchivePolicy),
		RetentionPolicy:   convertSDKRetentionPolicy(sdkReq.RetentionPolicy),
	}

	svcResp, err := objSvc.SetObjectPolicy(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcSetObjectPolicyResponse(svcResp), nil
}

func handleObjGetObjectPolicy(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.GetObjectPolicyRequest); ok {
		svcResp, err := objSvc.GetObjectPolicy(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcGetObjectPolicyResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.GetObjectPolicyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "GetObjectPolicy",
			"*objectstorage.GetObjectPolicyRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.GetObjectPolicyRequest{
		Key:      sdkReq.Key,
		TenantID: sdkReq.TenantID,
	}

	svcResp, err := objSvc.GetObjectPolicy(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcGetObjectPolicyResponse(svcResp), nil
}

func handleObjListBackends(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.ListBackendsRequest); ok {
		svcResp, err := objSvc.ListBackends(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcListBackendsResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.ListBackendsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "ListBackends",
			"*objectstorage.ListBackendsRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.ListBackendsRequest{
		IncludeHealth: sdkReq.IncludeHealth,
		FilterByType:  sdkReq.FilterByType,
	}

	svcResp, err := objSvc.ListBackends(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcListBackendsResponse(svcResp), nil
}

func handleObjGetBackendHealth(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.GetBackendHealthRequest); ok {
		svcResp, err := objSvc.GetBackendHealth(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcGetBackendHealthResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.GetBackendHealthRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "GetBackendHealth",
			"*objectstorage.GetBackendHealthRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.GetBackendHealthRequest{
		Name: sdkReq.Name,
	}

	svcResp, err := objSvc.GetBackendHealth(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcGetBackendHealthResponse(svcResp), nil
}

func handleObjGetTenantUsage(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.GetTenantUsageRequest); ok {
		svcResp, err := objSvc.GetTenantUsage(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcGetTenantUsageResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.GetTenantUsageRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "GetTenantUsage",
			"*objectstorage.GetTenantUsageRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.GetTenantUsageRequest{
		TenantID: sdkReq.TenantID,
	}

	svcResp, err := objSvc.GetTenantUsage(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcGetTenantUsageResponse(svcResp), nil
}

func handleObjGetTenantQuotas(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.GetTenantQuotasRequest); ok {
		svcResp, err := objSvc.GetTenantQuotas(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcGetTenantQuotasResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.GetTenantQuotasRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "GetTenantQuotas",
			"*objectstorage.GetTenantQuotasRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.GetTenantQuotasRequest{
		TenantID: sdkReq.TenantID,
	}

	svcResp, err := objSvc.GetTenantQuotas(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcGetTenantQuotasResponse(svcResp), nil
}

func handleObjSetTenantQuotas(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.SetTenantQuotasRequest); ok {
		svcResp, err := objSvc.SetTenantQuotas(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcSetTenantQuotasResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.SetTenantQuotasRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "SetTenantQuotas",
			"*objectstorage.SetTenantQuotasRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.SetTenantQuotasRequest{
		TenantID:        sdkReq.TenantID,
		MaxStorageBytes: sdkReq.MaxStorageBytes,
		MaxObjectSize:   sdkReq.MaxObjectSize,
		MaxObjectCount:  sdkReq.MaxObjectCount,
	}

	svcResp, err := objSvc.SetTenantQuotas(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcSetTenantQuotasResponse(svcResp), nil
}

func handleObjBatchPutObjects(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.BatchPutObjectsRequest); ok {
		svcResp, err := objSvc.BatchPutObjects(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcBatchPutObjectsResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.BatchPutObjectsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "BatchPutObjects",
			"*objectstorage.BatchPutObjectsRequest",
			typeNameOf(req),
		)
	}

	svcObjects := make([]*service.PutObjectRequest, len(sdkReq.Objects))
	for i, obj := range sdkReq.Objects {
		svcObjects[i] = &service.PutObjectRequest{
			Key:               obj.Key,
			Value:             obj.Value,
			ContentType:       obj.ContentType,
			TenantID:          obj.TenantID,
			ShardID:           obj.ShardID,
			Tags:              obj.Tags,
			ReplicationPolicy: convertSDKReplicationPolicy(obj.ReplicationPolicy),
			ArchivePolicy:     convertSDKArchivePolicy(obj.ArchivePolicy),
			RetentionPolicy:   convertSDKRetentionPolicy(obj.RetentionPolicy),
			TargetBackend:     obj.TargetBackend,
			Checksum:          obj.Checksum,
		}
	}

	svcReq := &service.BatchPutObjectsRequest{
		Objects:         svcObjects,
		ContinueOnError: !sdkReq.Atomic,
	}

	svcResp, err := objSvc.BatchPutObjects(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcBatchPutObjectsResponse(svcResp), nil
}

func handleObjBatchGetObjects(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.BatchGetObjectsRequest); ok {
		svcResp, err := objSvc.BatchGetObjects(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcBatchGetObjectsResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.BatchGetObjectsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "BatchGetObjects",
			"*objectstorage.BatchGetObjectsRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.BatchGetObjectsRequest{
		Keys:            sdkReq.Keys,
		TenantID:        sdkReq.TenantID,
		IncludeMetadata: sdkReq.IncludeMetadata,
	}

	svcResp, err := objSvc.BatchGetObjects(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcBatchGetObjectsResponse(svcResp), nil
}

func handleObjBatchDeleteObjects(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	objSvc := svc.ObjectStorageService()
	if objSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectStorage")
	}

	// Accept service type directly
	if r, ok := req.(*service.BatchDeleteObjectsRequest); ok {
		svcResp, err := objSvc.BatchDeleteObjects(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertSvcBatchDeleteObjectsResponse(svcResp), nil
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*objclient.BatchDeleteObjectsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.ObjectStorage", "BatchDeleteObjects",
			"*objectstorage.BatchDeleteObjectsRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.BatchDeleteObjectsRequest{
		Keys:            sdkReq.Keys,
		TenantID:        sdkReq.TenantID,
		ContinueOnError: !sdkReq.Atomic,
	}

	svcResp, err := objSvc.BatchDeleteObjects(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertSvcBatchDeleteObjectsResponse(svcResp), nil
}

// =============================================================================
// SDK <-> Service Policy Conversion Helpers
// =============================================================================

// convertSDKReplicationPolicy converts an SDK ReplicationPolicy to a catalog ReplicationPolicy.
func convertSDKReplicationPolicy(p *objclient.ReplicationPolicy) *catalog.ReplicationPolicy {
	if p == nil {
		return nil
	}
	return &catalog.ReplicationPolicy{
		Enabled:      p.Enabled,
		MinReplicas:  p.MinReplicas,
		TargetStores: p.TargetStores,
		Mode:         catalog.ReplicationMode(p.Mode),
		Priority:     p.Priority,
	}
}

// convertSDKArchivePolicy converts an SDK ArchivePolicy to a catalog ArchivePolicy.
func convertSDKArchivePolicy(p *objclient.ArchivePolicy) *catalog.ArchivePolicy {
	if p == nil {
		return nil
	}
	return &catalog.ArchivePolicy{
		Enabled:           p.Enabled,
		ArchiveAfterDays:  p.ArchiveAfterDays,
		TargetStore:       p.TargetStore,
		DeleteFromPrimary: p.DeleteFromPrimary,
		CompressionLevel:  p.CompressionLevel,
	}
}

// convertSDKRetentionPolicy converts an SDK RetentionPolicy to a catalog RetentionPolicy.
func convertSDKRetentionPolicy(p *objclient.RetentionPolicy) *catalog.RetentionPolicy {
	if p == nil {
		return nil
	}
	return &catalog.RetentionPolicy{
		Enabled:       p.Enabled,
		RetentionDays: p.RetentionDays,
		MaxVersions:   p.MaxVersions,
		DeleteAfter:   p.DeleteAfter,
	}
}

// convertCatalogReplicationPolicy converts a catalog ReplicationPolicy to an SDK ReplicationPolicy.
func convertCatalogReplicationPolicy(p *catalog.ReplicationPolicy) *objclient.ReplicationPolicy {
	if p == nil {
		return nil
	}
	return &objclient.ReplicationPolicy{
		Enabled:      p.Enabled,
		MinReplicas:  p.MinReplicas,
		TargetStores: p.TargetStores,
		Mode:         objclient.ReplicationMode(p.Mode),
		Priority:     p.Priority,
	}
}

// convertCatalogArchivePolicy converts a catalog ArchivePolicy to an SDK ArchivePolicy.
func convertCatalogArchivePolicy(p *catalog.ArchivePolicy) *objclient.ArchivePolicy {
	if p == nil {
		return nil
	}
	return &objclient.ArchivePolicy{
		Enabled:           p.Enabled,
		ArchiveAfterDays:  p.ArchiveAfterDays,
		TargetStore:       p.TargetStore,
		DeleteFromPrimary: p.DeleteFromPrimary,
		CompressionLevel:  p.CompressionLevel,
	}
}

// convertCatalogRetentionPolicy converts a catalog RetentionPolicy to an SDK RetentionPolicy.
func convertCatalogRetentionPolicy(p *catalog.RetentionPolicy) *objclient.RetentionPolicy {
	if p == nil {
		return nil
	}
	return &objclient.RetentionPolicy{
		Enabled:       p.Enabled,
		RetentionDays: p.RetentionDays,
		MaxVersions:   p.MaxVersions,
		DeleteAfter:   p.DeleteAfter,
	}
}

// =============================================================================
// Service -> SDK Response Conversion Helpers
// =============================================================================

// convertSvcPutObjectResponse converts a service PutObjectResponse to an SDK PutObjectResponse.
func convertSvcPutObjectResponse(r *service.PutObjectResponse) *objclient.PutObjectResponse {
	if r == nil {
		return nil
	}
	return &objclient.PutObjectResponse{
		Key:       r.Key,
		ObjectID:  r.ObjectID,
		Checksum:  r.Checksum,
		Size:      r.Size,
		Backend:   r.Backend,
		Version:   r.Version,
		CreatedAt: r.CreatedAt,
	}
}

// convertSvcGetObjectResponse converts a service GetObjectResponse to an SDK GetObjectResponse.
func convertSvcGetObjectResponse(r *service.GetObjectResponse) *objclient.GetObjectResponse {
	if r == nil {
		return nil
	}
	return &objclient.GetObjectResponse{
		Key:         r.Key,
		Value:       r.Value,
		ContentType: r.ContentType,
		Size:        r.Size,
		Checksum:    r.Checksum,
		Version:     r.Version,
		Tags:        r.Tags,
		TenantID:    r.TenantID,
		CreatedAt:   r.CreatedAt,
		ModifiedAt:  r.ModifiedAt,
		Backend:     r.Backend,
	}
}

// convertSvcDeleteObjectResponse converts a service DeleteObjectResponse to an SDK DeleteObjectResponse.
func convertSvcDeleteObjectResponse(r *service.DeleteObjectResponse) *objclient.DeleteObjectResponse {
	if r == nil {
		return nil
	}
	return &objclient.DeleteObjectResponse{
		Key:       r.Key,
		Success:   r.Success,
		Size:      r.Size,
		Timestamp: r.Timestamp,
	}
}

// convertSvcHeadObjectResponse converts a service HeadObjectResponse to an SDK HeadObjectResponse.
func convertSvcHeadObjectResponse(r *service.HeadObjectResponse) *objclient.HeadObjectResponse {
	if r == nil {
		return nil
	}
	return &objclient.HeadObjectResponse{
		Key:          r.Key,
		Exists:       r.Exists,
		ContentType:  r.ContentType,
		Size:         r.Size,
		Checksum:     r.Checksum,
		Version:      r.Version,
		Tags:         r.Tags,
		TenantID:     r.TenantID,
		Status:       r.Status,
		CreatedAt:    r.CreatedAt,
		ModifiedAt:   r.ModifiedAt,
		LastAccessAt: r.LastAccessAt,
		Backend:      r.Backend,
		ReplicaCount: r.ReplicaCount,
	}
}

// convertSvcObjectSummary converts a service ObjectSummary to an SDK ObjectSummary.
func convertSvcObjectSummary(s *service.ObjectSummary) *objclient.ObjectSummary {
	if s == nil {
		return nil
	}
	return &objclient.ObjectSummary{
		Key:         s.Key,
		Size:        s.Size,
		ContentType: s.ContentType,
		Version:     s.Version,
		Status:      s.Status,
		Tags:        s.Tags,
		CreatedAt:   s.CreatedAt,
		ModifiedAt:  s.ModifiedAt,
	}
}

// convertSvcObjectSummaries converts a slice of service ObjectSummary to SDK ObjectSummary.
func convertSvcObjectSummaries(summaries []*service.ObjectSummary) []*objclient.ObjectSummary {
	if summaries == nil {
		return nil
	}
	result := make([]*objclient.ObjectSummary, len(summaries))
	for i, s := range summaries {
		result[i] = convertSvcObjectSummary(s)
	}
	return result
}

// convertSvcListObjectsResponse converts a service ListObjectsResponse to an SDK ListObjectsResponse.
func convertSvcListObjectsResponse(r *service.ListObjectsResponse) *objclient.ListObjectsResponse {
	if r == nil {
		return nil
	}
	return &objclient.ListObjectsResponse{
		Objects:    convertSvcObjectSummaries(r.Objects),
		NextCursor: r.NextCursor,
		HasMore:    r.HasMore,
		TotalCount: r.TotalCount,
		Timestamp:  r.Timestamp,
	}
}

// convertSvcListByTagResponse converts a service ListByTagResponse to an SDK ListByTagResponse.
func convertSvcListByTagResponse(r *service.ListByTagResponse) *objclient.ListByTagResponse {
	if r == nil {
		return nil
	}
	return &objclient.ListByTagResponse{
		Objects:    convertSvcObjectSummaries(r.Objects),
		NextCursor: r.NextCursor,
		HasMore:    r.HasMore,
		TotalCount: r.TotalCount,
		Timestamp:  r.Timestamp,
	}
}

// convertSvcListByTenantResponse converts a service ListByTenantResponse to an SDK ListByTenantResponse.
func convertSvcListByTenantResponse(r *service.ListByTenantResponse) *objclient.ListByTenantResponse {
	if r == nil {
		return nil
	}
	return &objclient.ListByTenantResponse{
		Objects:    convertSvcObjectSummaries(r.Objects),
		NextCursor: r.NextCursor,
		HasMore:    r.HasMore,
		TotalCount: r.TotalCount,
		Timestamp:  r.Timestamp,
	}
}

// convertSvcCopyObjectResponse converts a service CopyObjectResponse to an SDK CopyObjectResponse.
func convertSvcCopyObjectResponse(r *service.CopyObjectResponse) *objclient.CopyObjectResponse {
	if r == nil {
		return nil
	}
	return &objclient.CopyObjectResponse{
		Key:       r.Key,
		ObjectID:  r.ObjectID,
		Size:      r.Size,
		Checksum:  r.Checksum,
		Version:   r.Version,
		Backend:   r.Backend,
		CreatedAt: r.CreatedAt,
	}
}

// convertSvcReplicateObjectResponse converts a service ReplicateObjectResponse to an SDK ReplicateObjectResponse.
func convertSvcReplicateObjectResponse(r *service.ReplicateObjectResponse) *objclient.ReplicateObjectResponse {
	if r == nil {
		return nil
	}
	var results map[string]*objclient.ReplicationResult
	if r.Results != nil {
		results = make(map[string]*objclient.ReplicationResult, len(r.Results))
		for k, v := range r.Results {
			results[k] = &objclient.ReplicationResult{
				Target:   v.Target,
				Success:  v.Success,
				Error:    v.Error,
				Duration: v.Duration,
			}
		}
	}
	return &objclient.ReplicateObjectResponse{
		Key:       r.Key,
		Status:    r.Status,
		Results:   results,
		Timestamp: r.Timestamp,
	}
}

// convertSvcMigrateObjectResponse converts a service MigrateObjectResponse to an SDK MigrateObjectResponse.
func convertSvcMigrateObjectResponse(r *service.MigrateObjectResponse) *objclient.MigrateObjectResponse {
	if r == nil {
		return nil
	}
	return &objclient.MigrateObjectResponse{
		Key:               r.Key,
		SourceBackend:     r.SourceBackend,
		TargetBackend:     r.TargetBackend,
		Success:           r.Success,
		DeletedFromSource: r.DeletedFromSource,
		Timestamp:         r.Timestamp,
	}
}

// convertSvcArchiveObjectResponse converts a service ArchiveObjectResponse to an SDK ArchiveObjectResponse.
func convertSvcArchiveObjectResponse(r *service.ArchiveObjectResponse) *objclient.ArchiveObjectResponse {
	if r == nil {
		return nil
	}
	return &objclient.ArchiveObjectResponse{
		Key:                r.Key,
		ArchiveStore:       r.ArchiveStore,
		Success:            r.Success,
		DeletedFromPrimary: r.DeletedFromPrimary,
		ArchivedAt:         r.ArchivedAt,
	}
}

// convertSvcRestoreObjectResponse converts a service RestoreObjectResponse to an SDK RestoreObjectResponse.
func convertSvcRestoreObjectResponse(r *service.RestoreObjectResponse) *objclient.RestoreObjectResponse {
	if r == nil {
		return nil
	}
	return &objclient.RestoreObjectResponse{
		Key:        r.Key,
		Backend:    r.Backend,
		Success:    r.Success,
		RestoredAt: r.RestoredAt,
	}
}

// convertSvcVerifyObjectResponse converts a service VerifyObjectResponse to an SDK VerifyObjectResponse.
func convertSvcVerifyObjectResponse(r *service.VerifyObjectResponse) *objclient.VerifyObjectResponse {
	if r == nil {
		return nil
	}
	var results map[string]*objclient.VerificationResult
	if r.Results != nil {
		results = make(map[string]*objclient.VerificationResult, len(r.Results))
		for k, v := range r.Results {
			results[k] = &objclient.VerificationResult{
				Store:         v.Store,
				Valid:         v.Valid,
				ChecksumMatch: v.ChecksumMatch,
				SizeMatch:     v.SizeMatch,
				Error:         v.Error,
			}
		}
	}
	return &objclient.VerifyObjectResponse{
		Key:       r.Key,
		Valid:     r.Valid,
		Results:   results,
		Repaired:  r.Repaired,
		Timestamp: r.Timestamp,
	}
}

// convertSvcSetObjectPolicyResponse converts a service SetObjectPolicyResponse to an SDK SetObjectPolicyResponse.
func convertSvcSetObjectPolicyResponse(r *service.SetObjectPolicyResponse) *objclient.SetObjectPolicyResponse {
	if r == nil {
		return nil
	}
	return &objclient.SetObjectPolicyResponse{
		Key:       r.Key,
		Success:   r.Success,
		Version:   r.Version,
		Timestamp: r.Timestamp,
	}
}

// convertSvcGetObjectPolicyResponse converts a service GetObjectPolicyResponse to an SDK GetObjectPolicyResponse.
func convertSvcGetObjectPolicyResponse(r *service.GetObjectPolicyResponse) *objclient.GetObjectPolicyResponse {
	if r == nil {
		return nil
	}
	return &objclient.GetObjectPolicyResponse{
		Key:               r.Key,
		ReplicationPolicy: convertCatalogReplicationPolicy(r.ReplicationPolicy),
		ArchivePolicy:     convertCatalogArchivePolicy(r.ArchivePolicy),
		RetentionPolicy:   convertCatalogRetentionPolicy(r.RetentionPolicy),
		Timestamp:         r.Timestamp,
	}
}

// convertSvcListBackendsResponse converts a service ListBackendsResponse to an SDK ListBackendsResponse.
func convertSvcListBackendsResponse(r *service.ListBackendsResponse) *objclient.ListBackendsResponse {
	if r == nil {
		return nil
	}
	var backends []*objclient.BackendInfo
	if r.Backends != nil {
		backends = make([]*objclient.BackendInfo, len(r.Backends))
		for i, b := range r.Backends {
			bi := &objclient.BackendInfo{
				Name:        b.Name,
				Type:        b.Type,
				Enabled:     b.Enabled,
				Default:     b.Default,
				Tier:        b.Tier,
				Region:      b.Region,
				ObjectCount: b.ObjectCount,
				TotalSize:   b.TotalSize,
			}
			if b.Health != nil {
				bi.Health = &objclient.BackendHealthInfo{
					Healthy:   b.Health.Healthy,
					Latency:   b.Health.Latency,
					LastCheck: b.Health.LastCheck,
					Error:     b.Health.Error,
				}
			}
			backends[i] = bi
		}
	}
	return &objclient.ListBackendsResponse{
		Backends:  backends,
		Timestamp: r.Timestamp,
	}
}

// convertSvcGetBackendHealthResponse converts a service GetBackendHealthResponse to an SDK GetBackendHealthResponse.
func convertSvcGetBackendHealthResponse(r *service.GetBackendHealthResponse) *objclient.GetBackendHealthResponse {
	if r == nil {
		return nil
	}
	return &objclient.GetBackendHealthResponse{
		Name:      r.Name,
		Healthy:   r.Healthy,
		Latency:   r.Latency,
		LastCheck: r.LastCheck,
		Status:    r.Status,
		Error:     r.Error,
	}
}

// convertSvcGetTenantUsageResponse converts a service GetTenantUsageResponse to an SDK GetTenantUsageResponse.
func convertSvcGetTenantUsageResponse(r *service.GetTenantUsageResponse) *objclient.GetTenantUsageResponse {
	if r == nil {
		return nil
	}
	return &objclient.GetTenantUsageResponse{
		TenantID:      r.TenantID,
		ObjectCount:   r.ObjectCount,
		TotalSize:     r.TotalSize,
		ActiveCount:   r.ActiveCount,
		ArchivedCount: r.ArchivedCount,
		Timestamp:     r.Timestamp,
	}
}

// convertSvcGetTenantQuotasResponse converts a service GetTenantQuotasResponse to an SDK GetTenantQuotasResponse.
func convertSvcGetTenantQuotasResponse(r *service.GetTenantQuotasResponse) *objclient.GetTenantQuotasResponse {
	if r == nil {
		return nil
	}
	return &objclient.GetTenantQuotasResponse{
		TenantID:         r.TenantID,
		MaxStorageBytes:  r.MaxStorageBytes,
		MaxObjectSize:    r.MaxObjectSize,
		MaxObjectCount:   r.MaxObjectCount,
		UsedStorageBytes: r.UsedStorageBytes,
		UsedObjectCount:  r.UsedObjectCount,
		Timestamp:        r.Timestamp,
	}
}

// convertSvcSetTenantQuotasResponse converts a service SetTenantQuotasResponse to an SDK SetTenantQuotasResponse.
func convertSvcSetTenantQuotasResponse(r *service.SetTenantQuotasResponse) *objclient.SetTenantQuotasResponse {
	if r == nil {
		return nil
	}
	return &objclient.SetTenantQuotasResponse{
		TenantID:  r.TenantID,
		UpdatedAt: r.UpdatedAt,
	}
}

// convertSvcBatchPutObjectsResponse converts a service BatchPutObjectsResponse to an SDK BatchPutObjectsResponse.
func convertSvcBatchPutObjectsResponse(r *service.BatchPutObjectsResponse) *objclient.BatchPutObjectsResponse {
	if r == nil {
		return nil
	}
	var results []*objclient.PutObjectResponse
	var errors map[string]string
	if r.Results != nil {
		results = make([]*objclient.PutObjectResponse, 0, len(r.Results))
		for _, res := range r.Results {
			if res.Success {
				results = append(results, &objclient.PutObjectResponse{
					Key:      res.Key,
					ObjectID: res.ObjectID,
					Checksum: res.Checksum,
				})
			} else {
				if errors == nil {
					errors = make(map[string]string)
				}
				errors[res.Key] = res.Error
			}
		}
	}
	return &objclient.BatchPutObjectsResponse{
		Results:   results,
		Succeeded: int32(r.SuccessCount),
		Failed:    int32(r.FailureCount),
		Errors:    errors,
	}
}

// convertSvcBatchGetObjectsResponse converts a service BatchGetObjectsResponse to an SDK BatchGetObjectsResponse.
func convertSvcBatchGetObjectsResponse(r *service.BatchGetObjectsResponse) *objclient.BatchGetObjectsResponse {
	if r == nil {
		return nil
	}
	var objects []*objclient.GetObjectResponse
	var notFound []string
	if r.Results != nil {
		objects = make([]*objclient.GetObjectResponse, 0, len(r.Results))
		for _, res := range r.Results {
			if res.Found {
				objects = append(objects, &objclient.GetObjectResponse{
					Key:         res.Key,
					Value:       res.Value,
					ContentType: res.ContentType,
					Size:        res.Size,
					Checksum:    res.Checksum,
					Version:     res.Version,
					Tags:        res.Tags,
				})
			} else {
				notFound = append(notFound, res.Key)
			}
		}
	}
	return &objclient.BatchGetObjectsResponse{
		Objects:  objects,
		NotFound: notFound,
	}
}

// convertSvcBatchDeleteObjectsResponse converts a service BatchDeleteObjectsResponse to an SDK BatchDeleteObjectsResponse.
func convertSvcBatchDeleteObjectsResponse(r *service.BatchDeleteObjectsResponse) *objclient.BatchDeleteObjectsResponse {
	if r == nil {
		return nil
	}
	var deletedKeys []string
	var failedKeys []string
	var errors map[string]string
	if r.Results != nil {
		for _, res := range r.Results {
			if res.Success {
				deletedKeys = append(deletedKeys, res.Key)
			} else {
				failedKeys = append(failedKeys, res.Key)
				if res.Error != "" {
					if errors == nil {
						errors = make(map[string]string)
					}
					errors[res.Key] = res.Error
				}
			}
		}
	}
	return &objclient.BatchDeleteObjectsResponse{
		DeletedKeys: deletedKeys,
		FailedKeys:  failedKeys,
		Errors:      errors,
	}
}
