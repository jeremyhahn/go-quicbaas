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

package grpc

import (
	"context"
	"errors"
	"io"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// objectStorageServer implements the gRPC ObjectStorageServiceServer interface.
type objectStorageServer struct {
	pb.UnimplementedObjectStorageServiceServer
	service service.ObjectStorageService
}

// NewObjectStorageServer creates a new object storage gRPC server.
func NewObjectStorageServer(svc service.ObjectStorageService) pb.ObjectStorageServiceServer {
	return &objectStorageServer{service: svc}
}

// RegisterObjectStorageServer registers the object storage service with a gRPC server.
func RegisterObjectStorageServer(server *grpc.Server, svc service.ObjectStorageService) {
	pb.RegisterObjectStorageServiceServer(server, NewObjectStorageServer(svc))
}

// =============================================================================
// Object CRUD Operations
// =============================================================================

// PutObject stores an object.
func (s *objectStorageServer) PutObject(ctx context.Context, req *pb.PutObjectRequest) (*pb.PutObjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	svcReq := &service.PutObjectRequest{
		Key:               req.Key,
		Value:             req.Value,
		ContentType:       req.ContentType,
		TenantID:          req.TenantId,
		ShardID:           req.ShardId,
		Tags:              req.Tags,
		ReplicationPolicy: protoToReplicationPolicy(req.ReplicationPolicy),
		ArchivePolicy:     protoToArchivePolicy(req.ArchivePolicy),
		RetentionPolicy:   protoToRetentionPolicy(req.RetentionPolicy),
		TargetBackend:     req.TargetBackend,
		Checksum:          req.Checksum,
	}

	resp, err := s.service.PutObject(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.PutObjectResponse{
		Key:       resp.Key,
		ObjectId:  resp.ObjectID,
		Checksum:  resp.Checksum,
		Size:      resp.Size,
		Backend:   resp.Backend,
		Version:   resp.Version,
		CreatedAt: timestamppb.New(time.Unix(0, resp.CreatedAt)),
	}, nil
}

// GetObject retrieves an object by key.
func (s *objectStorageServer) GetObject(ctx context.Context, req *pb.GetObjectRequest) (*pb.GetObjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	svcReq := &service.GetObjectRequest{
		Key:      req.Key,
		TenantID: req.TenantId,
	}

	resp, err := s.service.GetObject(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.GetObjectResponse{
		Key:         resp.Key,
		Value:       resp.Value,
		ContentType: resp.ContentType,
		Checksum:    resp.Checksum,
		Size:        resp.Size,
		Version:     resp.Version,
		Tags:        resp.Tags,
		Found:       true,
	}, nil
}

// DeleteObject removes an object from storage.
func (s *objectStorageServer) DeleteObject(ctx context.Context, req *pb.DeleteObjectRequest) (*pb.DeleteObjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	svcReq := &service.DeleteObjectRequest{
		Key:      req.Key,
		TenantID: req.TenantId,
	}

	resp, err := s.service.DeleteObject(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.DeleteObjectResponse{
		Key:       resp.Key,
		Deleted:   resp.Success,
		DeletedAt: timestamppb.New(time.Unix(0, resp.Timestamp)),
	}, nil
}

// HeadObject retrieves object metadata without the data.
func (s *objectStorageServer) HeadObject(ctx context.Context, req *pb.HeadObjectRequest) (*pb.HeadObjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	svcReq := &service.HeadObjectRequest{
		Key:      req.Key,
		TenantID: req.TenantId,
	}

	resp, err := s.service.HeadObject(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.HeadObjectResponse{
		Key:         resp.Key,
		Exists:      resp.Exists,
		Size:        resp.Size,
		Checksum:    resp.Checksum,
		ContentType: resp.ContentType,
		Version:     resp.Version,
		ModifiedAt:  timestamppb.New(time.Unix(0, resp.ModifiedAt)),
		Tags:        resp.Tags,
		Status:      stringToObjectStatus(resp.Status),
	}, nil
}

// =============================================================================
// Listing Operations
// =============================================================================

// ListObjects lists objects matching the specified criteria.
func (s *objectStorageServer) ListObjects(ctx context.Context, req *pb.ListObjectsRequest) (*pb.ListObjectsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	svcReq := &service.ListObjectsRequest{
		Prefix:   req.Prefix,
		TenantID: req.TenantId,
		Limit:    int(req.Limit),
		Cursor:   req.Cursor,
	}

	resp, err := s.service.ListObjects(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	objects := make([]*pb.ObjectInfo, len(resp.Objects))
	for i, obj := range resp.Objects {
		objects[i] = objectSummaryToProto(obj)
	}

	return &pb.ListObjectsResponse{
		Objects:    objects,
		NextCursor: resp.NextCursor,
		HasMore:    resp.HasMore,
		Total:      resp.TotalCount,
	}, nil
}

// ListByTag lists objects matching a specific tag.
func (s *objectStorageServer) ListByTag(ctx context.Context, req *pb.ListByTagRequest) (*pb.ListByTagResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if req.Tag == "" {
		return nil, status.Error(codes.InvalidArgument, "tag is required")
	}

	svcReq := &service.ListByTagRequest{
		TagName:  req.Tag,
		TagValue: req.Value,
		TenantID: req.TenantId,
		Limit:    int(req.Limit),
		Cursor:   req.Cursor,
	}

	resp, err := s.service.ListByTag(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	keys := make([]string, len(resp.Objects))
	for i, obj := range resp.Objects {
		keys[i] = obj.Key
	}

	return &pb.ListByTagResponse{
		Keys:       keys,
		NextCursor: resp.NextCursor,
		HasMore:    resp.HasMore,
		Total:      resp.TotalCount,
	}, nil
}

// ListByTenant lists objects belonging to a specific tenant.
func (s *objectStorageServer) ListByTenant(ctx context.Context, req *pb.ListByTenantRequest) (*pb.ListByTenantResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	svcReq := &service.ListByTenantRequest{
		TenantID: req.TenantId,
		Limit:    int(req.Limit),
		Cursor:   req.Cursor,
	}

	resp, err := s.service.ListByTenant(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	keys := make([]string, len(resp.Objects))
	for i, obj := range resp.Objects {
		keys[i] = obj.Key
	}

	return &pb.ListByTenantResponse{
		Keys:       keys,
		NextCursor: resp.NextCursor,
		HasMore:    resp.HasMore,
		Total:      resp.TotalCount,
	}, nil
}

// =============================================================================
// Advanced Object Operations
// =============================================================================

// CopyObject copies an object to a new key.
func (s *objectStorageServer) CopyObject(ctx context.Context, req *pb.CopyObjectRequest) (*pb.CopyObjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if req.SourceKey == "" {
		return nil, status.Error(codes.InvalidArgument, "source_key is required")
	}

	if req.DestKey == "" {
		return nil, status.Error(codes.InvalidArgument, "dest_key is required")
	}

	svcReq := &service.CopyObjectRequest{
		SourceKey:      req.SourceKey,
		DestinationKey: req.DestKey,
		TenantID:       req.TenantId,
	}

	resp, err := s.service.CopyObject(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.CopyObjectResponse{
		Key:       resp.Key,
		Version:   resp.Version,
		CreatedAt: timestamppb.New(time.Unix(0, resp.CreatedAt)),
		Size:      resp.Size,
	}, nil
}

// ReplicateObject replicates an object to target backends.
func (s *objectStorageServer) ReplicateObject(ctx context.Context, req *pb.ReplicateObjectRequest) (*pb.ReplicateObjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	if len(req.TargetStores) == 0 {
		return nil, status.Error(codes.InvalidArgument, "target_stores is required")
	}

	svcReq := &service.ReplicateObjectRequest{
		Key:         req.Key,
		Targets:     req.TargetStores,
		Synchronous: req.Sync,
	}

	resp, err := s.service.ReplicateObject(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	var replicatedTo []string
	var failedStores []string
	allSuccess := true

	for target, result := range resp.Results {
		if result.Success {
			replicatedTo = append(replicatedTo, target)
		} else {
			failedStores = append(failedStores, target)
			allSuccess = false
		}
	}

	return &pb.ReplicateObjectResponse{
		Key:          resp.Key,
		ReplicatedTo: replicatedTo,
		Success:      allSuccess,
		FailedStores: failedStores,
	}, nil
}

// MigrateObject migrates an object to a different backend.
func (s *objectStorageServer) MigrateObject(ctx context.Context, req *pb.MigrateObjectRequest) (*pb.MigrateObjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	if req.TargetBackend == "" {
		return nil, status.Error(codes.InvalidArgument, "target_backend is required")
	}

	svcReq := &service.MigrateObjectRequest{
		Key:              req.Key,
		TargetBackend:    req.TargetBackend,
		DeleteFromSource: req.DeleteSource,
	}

	resp, err := s.service.MigrateObject(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.MigrateObjectResponse{
		Key:           resp.Key,
		OldBackend:    resp.SourceBackend,
		NewBackend:    resp.TargetBackend,
		Success:       resp.Success,
		SourceDeleted: resp.DeletedFromSource,
	}, nil
}

// ArchiveObject archives an object to a cold storage backend.
func (s *objectStorageServer) ArchiveObject(ctx context.Context, req *pb.ArchiveObjectRequest) (*pb.ArchiveObjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	svcReq := &service.ArchiveObjectRequest{
		Key:               req.Key,
		TargetStore:       req.TargetStore,
		DeleteFromPrimary: req.DeleteFromPrimary,
	}

	resp, err := s.service.ArchiveObject(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.ArchiveObjectResponse{
		Key:          resp.Key,
		ArchiveStore: resp.ArchiveStore,
		Success:      resp.Success,
		ArchivedAt:   timestamppb.New(time.Unix(0, resp.ArchivedAt)),
	}, nil
}

// RestoreObject restores an archived object.
func (s *objectStorageServer) RestoreObject(ctx context.Context, req *pb.RestoreObjectRequest) (*pb.RestoreObjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	svcReq := &service.RestoreObjectRequest{
		Key:           req.Key,
		TargetBackend: req.TargetBackend,
	}

	resp, err := s.service.RestoreObject(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.RestoreObjectResponse{
		Key:        resp.Key,
		RestoredTo: resp.Backend,
		Success:    resp.Success,
	}, nil
}

// VerifyObject verifies object integrity across storage locations.
func (s *objectStorageServer) VerifyObject(ctx context.Context, req *pb.VerifyObjectRequest) (*pb.VerifyObjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	svcReq := &service.VerifyObjectRequest{
		Key:              req.Key,
		RepairMismatches: req.Repair,
	}

	resp, err := s.service.VerifyObject(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	locations := make([]*pb.LocationVerification, 0, len(resp.Results))
	for _, result := range resp.Results {
		locations = append(locations, &pb.LocationVerification{
			Store: result.Store,
			Valid: result.Valid,
			Error: result.Error,
		})
	}

	return &pb.VerifyObjectResponse{
		Key:       resp.Key,
		Valid:     resp.Valid,
		Locations: locations,
		Repaired:  resp.Repaired,
	}, nil
}

// =============================================================================
// Policy Operations
// =============================================================================

// SetObjectPolicy sets policies for an object.
func (s *objectStorageServer) SetObjectPolicy(ctx context.Context, req *pb.SetObjectPolicyRequest) (*pb.SetObjectPolicyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	svcReq := &service.SetObjectPolicyRequest{
		Key:               req.Key,
		ReplicationPolicy: protoToReplicationPolicy(req.ReplicationPolicy),
		ArchivePolicy:     protoToArchivePolicy(req.ArchivePolicy),
		RetentionPolicy:   protoToRetentionPolicy(req.RetentionPolicy),
	}

	resp, err := s.service.SetObjectPolicy(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.SetObjectPolicyResponse{
		Key:       resp.Key,
		Version:   resp.Version,
		UpdatedAt: timestamppb.New(time.Unix(0, resp.Timestamp)),
	}, nil
}

// GetObjectPolicy retrieves policies for an object.
func (s *objectStorageServer) GetObjectPolicy(ctx context.Context, req *pb.GetObjectPolicyRequest) (*pb.GetObjectPolicyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	svcReq := &service.GetObjectPolicyRequest{
		Key: req.Key,
	}

	resp, err := s.service.GetObjectPolicy(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.GetObjectPolicyResponse{
		Key: resp.Key,
		Policy: &pb.ObjectPolicy{
			ReplicationPolicy: replicationPolicyToProto(resp.ReplicationPolicy),
			ArchivePolicy:     archivePolicyToProto(resp.ArchivePolicy),
			RetentionPolicy:   retentionPolicyToProto(resp.RetentionPolicy),
		},
	}, nil
}

// =============================================================================
// Backend Operations
// =============================================================================

// ListBackends lists available storage backends.
func (s *objectStorageServer) ListBackends(ctx context.Context, req *pb.ListBackendsRequest) (*pb.ListBackendsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	svcReq := &service.ListBackendsRequest{
		IncludeHealth: req.IncludeHealth,
	}

	resp, err := s.service.ListBackends(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	backends := make([]*pb.BackendInfo, len(resp.Backends))
	for i, b := range resp.Backends {
		backends[i] = backendInfoToProto(b)
	}

	return &pb.ListBackendsResponse{
		Backends: backends,
		Total:    int32(len(backends)),
	}, nil
}

// GetBackendHealth retrieves health status for a backend.
func (s *objectStorageServer) GetBackendHealth(ctx context.Context, req *pb.GetBackendHealthRequest) (*pb.GetBackendHealthResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	svcReq := &service.GetBackendHealthRequest{
		Name: req.Name,
	}

	resp, err := s.service.GetBackendHealth(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.GetBackendHealthResponse{
		Name:      resp.Name,
		Healthy:   resp.Healthy,
		LatencyMs: resp.Latency,
		LastCheck: timestamppb.New(time.Unix(0, resp.LastCheck)),
		Error:     resp.Error,
	}, nil
}

// =============================================================================
// Tenant Operations
// =============================================================================

// GetObjectStorageTenantUsage retrieves storage usage for a tenant.
func (s *objectStorageServer) GetObjectStorageTenantUsage(ctx context.Context, req *pb.GetObjectStorageTenantUsageRequest) (*pb.GetObjectStorageTenantUsageResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	svcReq := &service.GetTenantUsageRequest{
		TenantID: req.TenantId,
	}

	resp, err := s.service.GetTenantUsage(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.GetObjectStorageTenantUsageResponse{
		Usage: &pb.ObjectStorageTenantUsage{
			TenantId:    resp.TenantID,
			TotalBytes:  resp.TotalSize,
			ObjectCount: resp.ObjectCount,
		},
	}, nil
}

// GetObjectStorageTenantQuotas retrieves quota settings for a tenant.
func (s *objectStorageServer) GetObjectStorageTenantQuotas(ctx context.Context, req *pb.GetObjectStorageTenantQuotasRequest) (*pb.GetObjectStorageTenantQuotasResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	svcReq := &service.GetTenantQuotasRequest{
		TenantID: req.TenantId,
	}

	resp, err := s.service.GetTenantQuotas(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.GetObjectStorageTenantQuotasResponse{
		Quotas: &pb.ObjectStorageTenantQuotas{
			TenantId:        resp.TenantID,
			MaxStorageBytes: resp.MaxStorageBytes,
			MaxObjectSize:   resp.MaxObjectSize,
			MaxObjectCount:  resp.MaxObjectCount,
		},
		Usage: &pb.ObjectStorageTenantUsage{
			TenantId:    resp.TenantID,
			TotalBytes:  resp.UsedStorageBytes,
			ObjectCount: resp.UsedObjectCount,
		},
	}, nil
}

// SetObjectStorageTenantQuotas sets or updates quotas for a tenant.
func (s *objectStorageServer) SetObjectStorageTenantQuotas(ctx context.Context, req *pb.SetObjectStorageTenantQuotasRequest) (*pb.SetObjectStorageTenantQuotasResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if req.Quotas == nil {
		return nil, status.Error(codes.InvalidArgument, "quotas is required")
	}

	if req.Quotas.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	svcReq := &service.SetTenantQuotasRequest{
		TenantID:        req.Quotas.TenantId,
		MaxStorageBytes: req.Quotas.MaxStorageBytes,
		MaxObjectSize:   req.Quotas.MaxObjectSize,
		MaxObjectCount:  req.Quotas.MaxObjectCount,
	}

	resp, err := s.service.SetTenantQuotas(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.SetObjectStorageTenantQuotasResponse{
		TenantId:  resp.TenantID,
		UpdatedAt: timestamppb.New(time.Unix(0, resp.UpdatedAt)),
	}, nil
}

// =============================================================================
// Streaming Operations
// =============================================================================

// StreamGetObject streams large objects in chunks for memory-efficient retrieval.
func (s *objectStorageServer) StreamGetObject(req *pb.GetObjectRequest, stream grpc.ServerStreamingServer[pb.ObjectChunk]) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return status.Error(codes.Unavailable, "object storage service not available")
	}

	if req.Key == "" {
		return status.Error(codes.InvalidArgument, "key is required")
	}

	ctx := stream.Context()

	svcReq := &service.GetObjectRequest{
		Key:      req.Key,
		TenantID: req.TenantId,
	}

	resp, err := s.service.GetObject(ctx, svcReq)
	if err != nil {
		return toProtoError(err)
	}

	const chunkSize = 64 * 1024 // 64KB chunks
	data := resp.Value
	totalSize := int64(len(data))

	// Send empty chunk if data was empty
	if totalSize == 0 {
		chunk := &pb.ObjectChunk{
			Data:      nil,
			Offset:    0,
			IsLast:    true,
			TotalSize: 0,
		}
		return stream.Send(chunk)
	}

	for offset := int64(0); offset < totalSize; offset += int64(chunkSize) {
		end := offset + int64(chunkSize)
		if end > totalSize {
			end = totalSize
		}

		chunk := &pb.ObjectChunk{
			Data:      data[offset:end],
			Offset:    offset,
			IsLast:    end == totalSize,
			TotalSize: totalSize,
		}

		if err := stream.Send(chunk); err != nil {
			return err
		}
	}

	return nil
}

// StreamPutObject accepts object data as a stream of chunks for large uploads.
func (s *objectStorageServer) StreamPutObject(stream grpc.ClientStreamingServer[pb.PutObjectChunk, pb.PutObjectResponse]) error {
	if s.service == nil {
		return status.Error(codes.Unavailable, "object storage service not available")
	}

	ctx := stream.Context()

	var key string
	var contentType string
	var tenantID string
	var tags map[string]string
	var data []byte

	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}

		// Extract metadata from first chunk via the oneof Metadata payload
		if metadata := chunk.GetMetadata(); metadata != nil && key == "" {
			key = metadata.Key
			contentType = metadata.ContentType
			tenantID = metadata.TenantId
			tags = metadata.Tags
		}

		// Accumulate data from data payloads
		if chunkData := chunk.GetData(); len(chunkData) > 0 {
			data = append(data, chunkData...)
		}
	}

	if key == "" {
		return status.Error(codes.InvalidArgument, "key is required")
	}

	svcReq := &service.PutObjectRequest{
		Key:         key,
		Value:       data,
		ContentType: contentType,
		TenantID:    tenantID,
		Tags:        tags,
	}

	resp, err := s.service.PutObject(ctx, svcReq)
	if err != nil {
		return toProtoError(err)
	}

	return stream.SendAndClose(&pb.PutObjectResponse{
		Key:       resp.Key,
		ObjectId:  resp.ObjectID,
		Checksum:  resp.Checksum,
		Size:      resp.Size,
		Backend:   resp.Backend,
		Version:   resp.Version,
		CreatedAt: timestamppb.New(time.Unix(0, resp.CreatedAt)),
	})
}

// =============================================================================
// Batch Operations
// =============================================================================

// BatchPutObjects stores multiple objects.
func (s *objectStorageServer) BatchPutObjects(ctx context.Context, req *pb.BatchPutObjectsRequest) (*pb.BatchPutObjectsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if len(req.Objects) == 0 {
		return nil, status.Error(codes.InvalidArgument, "objects is required")
	}

	objects := make([]*service.PutObjectRequest, len(req.Objects))
	for i, obj := range req.Objects {
		objects[i] = &service.PutObjectRequest{
			Key:         obj.Key,
			Value:       obj.Value,
			ContentType: obj.ContentType,
			TenantID:    obj.TenantId,
			Tags:        obj.Tags,
		}
	}

	svcReq := &service.BatchPutObjectsRequest{
		Objects: objects,
	}

	resp, err := s.service.BatchPutObjects(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	results := make([]*pb.PutObjectResponse, len(resp.Results))
	for i, result := range resp.Results {
		results[i] = &pb.PutObjectResponse{
			Key:      result.Key,
			ObjectId: result.ObjectID,
			Checksum: result.Checksum,
		}
	}

	return &pb.BatchPutObjectsResponse{
		Results:   results,
		Succeeded: int32(resp.SuccessCount),
		Failed:    int32(resp.FailureCount),
	}, nil
}

// BatchGetObjects retrieves multiple objects.
func (s *objectStorageServer) BatchGetObjects(ctx context.Context, req *pb.BatchGetObjectsRequest) (*pb.BatchGetObjectsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if len(req.Keys) == 0 {
		return nil, status.Error(codes.InvalidArgument, "keys is required")
	}

	svcReq := &service.BatchGetObjectsRequest{
		Keys:     req.Keys,
		TenantID: req.TenantId,
	}

	resp, err := s.service.BatchGetObjects(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	var objects []*pb.GetObjectResponse
	var notFound []string

	for _, result := range resp.Results {
		if result.Found {
			objects = append(objects, &pb.GetObjectResponse{
				Key:         result.Key,
				Value:       result.Value,
				ContentType: result.ContentType,
				Checksum:    result.Checksum,
				Size:        result.Size,
				Version:     result.Version,
				Tags:        result.Tags,
				Found:       true,
			})
		} else {
			notFound = append(notFound, result.Key)
		}
	}

	return &pb.BatchGetObjectsResponse{
		Objects:  objects,
		NotFound: notFound,
	}, nil
}

// BatchDeleteObjects deletes multiple objects.
func (s *objectStorageServer) BatchDeleteObjects(ctx context.Context, req *pb.BatchDeleteObjectsRequest) (*pb.BatchDeleteObjectsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage service not available")
	}

	if len(req.Keys) == 0 {
		return nil, status.Error(codes.InvalidArgument, "keys is required")
	}

	svcReq := &service.BatchDeleteObjectsRequest{
		Keys:     req.Keys,
		TenantID: req.TenantId,
	}

	resp, err := s.service.BatchDeleteObjects(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	var deletedKeys []string
	var failedKeys []string
	errors := make(map[string]string)

	for _, result := range resp.Results {
		if result.Success {
			deletedKeys = append(deletedKeys, result.Key)
		} else {
			failedKeys = append(failedKeys, result.Key)
			if result.Error != "" {
				errors[result.Key] = result.Error
			}
		}
	}

	return &pb.BatchDeleteObjectsResponse{
		DeletedKeys: deletedKeys,
		FailedKeys:  failedKeys,
		Errors:      errors,
	}, nil
}

// =============================================================================
// Helper Functions - String to Proto Enum Conversions
// =============================================================================

// stringToObjectStatus converts a string status to a proto ObjectStatus.
func stringToObjectStatus(s string) pb.ObjectStatus {
	switch s {
	case "active":
		return pb.ObjectStatus_OBJECT_STATUS_ACTIVE
	case "archived":
		return pb.ObjectStatus_OBJECT_STATUS_ARCHIVED
	case "restoring":
		return pb.ObjectStatus_OBJECT_STATUS_RESTORING
	case "pending_delete":
		return pb.ObjectStatus_OBJECT_STATUS_PENDING_DELETE
	case "deleted":
		return pb.ObjectStatus_OBJECT_STATUS_DELETED
	case "corrupted":
		return pb.ObjectStatus_OBJECT_STATUS_CORRUPTED
	default:
		return pb.ObjectStatus_OBJECT_STATUS_UNSPECIFIED
	}
}

// stringToBackendType converts a string backend type to a proto BackendType.
func stringToBackendType(s string) pb.BackendType {
	switch s {
	case "local":
		return pb.BackendType_BACKEND_TYPE_LOCAL
	case "s3":
		return pb.BackendType_BACKEND_TYPE_S3
	case "gcs":
		return pb.BackendType_BACKEND_TYPE_GCS
	case "azure":
		return pb.BackendType_BACKEND_TYPE_AZURE
	case "minio":
		return pb.BackendType_BACKEND_TYPE_MINIO
	case "pebble":
		return pb.BackendType_BACKEND_TYPE_PEBBLE
	case "raft":
		return pb.BackendType_BACKEND_TYPE_RAFT
	default:
		return pb.BackendType_BACKEND_TYPE_UNSPECIFIED
	}
}

// stringToStorageTier converts a string storage tier to a proto StorageTier.
func stringToStorageTier(s string) pb.StorageTier {
	switch s {
	case "hot":
		return pb.StorageTier_STORAGE_TIER_HOT
	case "warm":
		return pb.StorageTier_STORAGE_TIER_WARM
	case "cold":
		return pb.StorageTier_STORAGE_TIER_COLD
	case "archive":
		return pb.StorageTier_STORAGE_TIER_ARCHIVE
	default:
		return pb.StorageTier_STORAGE_TIER_UNSPECIFIED
	}
}

// stringToReplicationMode converts a string replication mode to a proto ReplicationMode.
func stringToReplicationMode(s string) pb.ReplicationMode {
	switch s {
	case "sync":
		return pb.ReplicationMode_REPLICATION_MODE_SYNC
	case "async":
		return pb.ReplicationMode_REPLICATION_MODE_ASYNC
	case "quorum":
		return pb.ReplicationMode_REPLICATION_MODE_QUORUM
	default:
		return pb.ReplicationMode_REPLICATION_MODE_UNSPECIFIED
	}
}

// =============================================================================
// Helper Functions - Proto to Service Conversions
// =============================================================================

// protoReplicationModeToString converts a proto ReplicationMode to a catalog ReplicationMode.
func protoReplicationModeToString(mode pb.ReplicationMode) catalog.ReplicationMode {
	switch mode {
	case pb.ReplicationMode_REPLICATION_MODE_SYNC:
		return catalog.ReplicationModeSync
	case pb.ReplicationMode_REPLICATION_MODE_ASYNC:
		return catalog.ReplicationModeAsync
	case pb.ReplicationMode_REPLICATION_MODE_QUORUM:
		return catalog.ReplicationModeQuorum
	default:
		return catalog.ReplicationModeAsync
	}
}

// protoToReplicationPolicy converts a proto ReplicationPolicy to a catalog ReplicationPolicy.
func protoToReplicationPolicy(p *pb.ReplicationPolicy) *catalog.ReplicationPolicy {
	if p == nil {
		return nil
	}
	return &catalog.ReplicationPolicy{
		Enabled:      p.Enabled,
		MinReplicas:  int(p.MinReplicas),
		TargetStores: p.TargetStores,
		Mode:         protoReplicationModeToString(p.Mode),
		Priority:     int(p.Priority),
	}
}

// protoToArchivePolicy converts a proto ArchivePolicy to a catalog ArchivePolicy.
func protoToArchivePolicy(p *pb.ArchivePolicy) *catalog.ArchivePolicy {
	if p == nil {
		return nil
	}
	return &catalog.ArchivePolicy{
		Enabled:           p.Enabled,
		ArchiveAfterDays:  int(p.ArchiveAfterDays),
		TargetStore:       p.TargetStore,
		DeleteFromPrimary: p.DeleteFromPrimary,
		CompressionLevel:  int(p.CompressionLevel),
	}
}

// protoToRetentionPolicy converts a proto RetentionPolicy to a catalog RetentionPolicy.
func protoToRetentionPolicy(p *pb.RetentionPolicy) *catalog.RetentionPolicy {
	if p == nil {
		return nil
	}
	return &catalog.RetentionPolicy{
		Enabled:       p.Enabled,
		RetentionDays: int(p.RetentionDays),
		MaxVersions:   int(p.MaxVersions),
	}
}

// =============================================================================
// Helper Functions - Service to Proto Conversions
// =============================================================================

// replicationPolicyToProto converts a catalog ReplicationPolicy to a proto ReplicationPolicy.
func replicationPolicyToProto(p *catalog.ReplicationPolicy) *pb.ReplicationPolicy {
	if p == nil {
		return nil
	}
	return &pb.ReplicationPolicy{
		Enabled:      p.Enabled,
		MinReplicas:  int32(p.MinReplicas),
		TargetStores: p.TargetStores,
		Mode:         stringToReplicationMode(string(p.Mode)),
		Priority:     int32(p.Priority),
	}
}

// archivePolicyToProto converts a catalog ArchivePolicy to a proto ArchivePolicy.
func archivePolicyToProto(p *catalog.ArchivePolicy) *pb.ArchivePolicy {
	if p == nil {
		return nil
	}
	return &pb.ArchivePolicy{
		Enabled:           p.Enabled,
		ArchiveAfterDays:  int32(p.ArchiveAfterDays),
		TargetStore:       p.TargetStore,
		DeleteFromPrimary: p.DeleteFromPrimary,
		CompressionLevel:  int32(p.CompressionLevel),
	}
}

// retentionPolicyToProto converts a catalog RetentionPolicy to a proto RetentionPolicy.
func retentionPolicyToProto(p *catalog.RetentionPolicy) *pb.RetentionPolicy {
	if p == nil {
		return nil
	}
	return &pb.RetentionPolicy{
		Enabled:       p.Enabled,
		RetentionDays: int32(p.RetentionDays),
		MaxVersions:   int32(p.MaxVersions),
	}
}

// objectSummaryToProto converts a service ObjectSummary to a proto ObjectInfo.
func objectSummaryToProto(s *service.ObjectSummary) *pb.ObjectInfo {
	if s == nil {
		return nil
	}
	return &pb.ObjectInfo{
		Key:         s.Key,
		Size:        s.Size,
		ContentType: s.ContentType,
		Version:     s.Version,
		CreatedAt:   timestamppb.New(time.Unix(0, s.CreatedAt)),
		ModifiedAt:  timestamppb.New(time.Unix(0, s.ModifiedAt)),
		Tags:        s.Tags,
		Status:      stringToObjectStatus(s.Status),
	}
}

// backendInfoToProto converts a service BackendInfo to a proto BackendInfo.
func backendInfoToProto(b *service.BackendInfo) *pb.BackendInfo {
	if b == nil {
		return nil
	}

	info := &pb.BackendInfo{
		Name:        b.Name,
		Type:        stringToBackendType(b.Type),
		Enabled:     b.Enabled,
		Tier:        stringToStorageTier(b.Tier),
		Region:      b.Region,
		ObjectCount: b.ObjectCount,
		UsedBytes:   b.TotalSize,
	}

	if b.Health != nil {
		info.Healthy = b.Health.Healthy
		info.LastHealthCheck = timestamppb.New(time.Unix(0, b.Health.LastCheck))
	}

	return info
}
