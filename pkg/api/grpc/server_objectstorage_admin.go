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
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// objectStorageAdminServer implements the gRPC ObjectStorageAdminServer interface.
type objectStorageAdminServer struct {
	pb.UnimplementedObjectStorageAdminServer
	service service.ObjectStorageAdminService
}

// NewObjectStorageAdminServer creates a new object storage admin gRPC server.
func NewObjectStorageAdminServer(svc service.ObjectStorageAdminService) pb.ObjectStorageAdminServer {
	return &objectStorageAdminServer{service: svc}
}

// RegisterObjectStorageAdminServer registers the object storage admin service with a gRPC server.
func RegisterObjectStorageAdminServer(server *grpc.Server, svc service.ObjectStorageAdminService) {
	pb.RegisterObjectStorageAdminServer(server, NewObjectStorageAdminServer(svc))
}

// =============================================================================
// Backend Management Operations
// =============================================================================

// CreateBackend registers a new storage backend.
func (s *objectStorageAdminServer) CreateBackend(ctx context.Context, req *pb.CreateBackendRequest) (*pb.CreateBackendResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage admin service not available")
	}

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	// Convert proto request to service request
	svcReq := &service.CreateBackendRequest{
		Name:    req.Name,
		Type:    protoBackendTypeToService(req.Type),
		Tier:    protoStorageTierToService(req.Tier),
		Config:  req.Config,
		Enabled: req.Enabled,
	}

	resp, err := s.service.CreateBackend(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.CreateBackendResponse{
		Name:      resp.Name,
		CreatedAt: timestamppb.New(time.Unix(0, resp.CreatedAt)),
	}, nil
}

// UpdateBackend updates backend configuration.
func (s *objectStorageAdminServer) UpdateBackend(ctx context.Context, req *pb.UpdateBackendRequest) (*pb.UpdateBackendResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage admin service not available")
	}

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	// Convert proto request to service request
	svcReq := &service.UpdateBackendRequest{
		Name:   req.Name,
		Config: req.Config,
		Tier:   protoStorageTierToService(req.Tier),
	}

	resp, err := s.service.UpdateBackend(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.UpdateBackendResponse{
		Name:      resp.Name,
		UpdatedAt: timestamppb.New(time.Unix(0, resp.UpdatedAt)),
	}, nil
}

// DeleteBackend removes a storage backend (requires migration of objects).
func (s *objectStorageAdminServer) DeleteBackend(ctx context.Context, req *pb.DeleteBackendRequest) (*pb.DeleteBackendResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage admin service not available")
	}

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	// Convert proto request to service request
	svcReq := &service.DeleteBackendRequest{
		Name:      req.Name,
		MigrateTo: req.MigrateTo,
		Force:     req.Force,
	}

	resp, err := s.service.DeleteBackend(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.DeleteBackendResponse{
		Name:            resp.Name,
		Deleted:         resp.Deleted,
		MigratedObjects: resp.MigratedObjects,
	}, nil
}

// SetBackendStatus enables or disables a backend.
func (s *objectStorageAdminServer) SetBackendStatus(ctx context.Context, req *pb.SetBackendStatusRequest) (*pb.SetBackendStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage admin service not available")
	}

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	// Convert proto request to service request
	svcReq := &service.SetBackendStatusRequest{
		Name:    req.Name,
		Enabled: req.Enabled,
	}

	resp, err := s.service.SetBackendStatus(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.SetBackendStatusResponse{
		Name:      resp.Name,
		Enabled:   resp.Enabled,
		UpdatedAt: timestamppb.New(time.Unix(0, resp.UpdatedAt)),
	}, nil
}

// =============================================================================
// Statistics Operations
// =============================================================================

// GetStorageStats returns cluster-wide storage statistics.
func (s *objectStorageAdminServer) GetStorageStats(ctx context.Context, req *pb.GetStorageStatsRequest) (*pb.GetStorageStatsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage admin service not available")
	}

	// Convert proto request to service request
	svcReq := &service.GetStorageStatsRequest{
		IncludeBackends: req.IncludeBackends,
		IncludeTenants:  req.IncludeTenants,
	}

	resp, err := s.service.GetStorageStats(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	// Convert backend stats
	var backendStats []*pb.BackendInfo
	if resp.BackendStats != nil {
		backendStats = make([]*pb.BackendInfo, len(resp.BackendStats))
		for i, b := range resp.BackendStats {
			backendStats[i] = serviceBackendInfoToProto(b)
		}
	}

	// Convert tenant stats
	var tenantStats []*pb.ObjectStorageTenantUsage
	if resp.TenantStats != nil {
		tenantStats = make([]*pb.ObjectStorageTenantUsage, len(resp.TenantStats))
		for i, t := range resp.TenantStats {
			tenantStats[i] = serviceTenantStatsToProto(t)
		}
	}

	return &pb.GetStorageStatsResponse{
		TotalBytes:      resp.TotalBytes,
		TotalObjects:    resp.TotalObjects,
		TotalBackends:   resp.TotalBackends,
		HealthyBackends: resp.HealthyBackends,
		BackendStats:    backendStats,
		TenantStats:     tenantStats,
		CalculatedAt:    timestamppb.New(time.Unix(0, resp.CalculatedAt)),
	}, nil
}

// =============================================================================
// Garbage Collection Operations
// =============================================================================

// RunGarbageCollection initiates garbage collection on specified backends.
func (s *objectStorageAdminServer) RunGarbageCollection(ctx context.Context, req *pb.RunGarbageCollectionRequest) (*pb.RunGarbageCollectionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage admin service not available")
	}

	// Convert proto request to service request
	svcReq := &service.RunGarbageCollectionRequest{
		Backends:   req.Backends,
		DryRun:     req.DryRun,
		Aggressive: req.Aggressive,
	}

	resp, err := s.service.RunGarbageCollection(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.RunGarbageCollectionResponse{
		OperationId: resp.OperationID,
		StartedAt:   timestamppb.New(time.Unix(0, resp.StartedAt)),
	}, nil
}

// GetGarbageCollectionProgress returns GC progress.
func (s *objectStorageAdminServer) GetGarbageCollectionProgress(ctx context.Context, req *pb.GetGarbageCollectionProgressRequest) (*pb.GetGarbageCollectionProgressResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage admin service not available")
	}

	if req.OperationId == "" {
		return nil, status.Error(codes.InvalidArgument, "operation_id is required")
	}

	// Convert proto request to service request
	svcReq := &service.GetGarbageCollectionProgressRequest{
		OperationID: req.OperationId,
	}

	resp, err := s.service.GetGarbageCollectionProgress(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	var completedAt *timestamppb.Timestamp
	if resp.CompletedAt > 0 {
		completedAt = timestamppb.New(time.Unix(0, resp.CompletedAt))
	}

	return &pb.GetGarbageCollectionProgressResponse{
		OperationId:     resp.OperationID,
		Status:          string(resp.Status),
		ObjectsScanned:  resp.ObjectsScanned,
		ObjectsDeleted:  resp.ObjectsDeleted,
		BytesReclaimed:  resp.BytesReclaimed,
		PercentComplete: resp.PercentComplete,
		Error:           resp.Error,
		CompletedAt:     completedAt,
	}, nil
}

// =============================================================================
// Repair Operations
// =============================================================================

// RepairObject attempts to repair an object with inconsistent replicas.
func (s *objectStorageAdminServer) RepairObject(ctx context.Context, req *pb.RepairObjectRequest) (*pb.RepairObjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage admin service not available")
	}

	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	// Convert proto request to service request
	svcReq := &service.RepairObjectRequest{
		Key:            req.Key,
		SourceLocation: req.SourceLocation,
		TimeoutMs:      req.TimeoutMs,
	}

	resp, err := s.service.RepairObject(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.RepairObjectResponse{
		Key:               resp.Key,
		Success:           resp.Success,
		RepairedLocations: resp.RepairedLocations,
		Error:             resp.Error,
	}, nil
}

// =============================================================================
// Compaction Operations
// =============================================================================

// CompactBackend triggers compaction on a backend.
func (s *objectStorageAdminServer) CompactBackend(ctx context.Context, req *pb.CompactBackendRequest) (*pb.CompactBackendResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if s.service == nil {
		return nil, status.Error(codes.Unavailable, "object storage admin service not available")
	}

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	// Convert proto request to service request
	svcReq := &service.CompactBackendRequest{
		Name:       req.Name,
		Aggressive: req.Aggressive,
	}

	resp, err := s.service.CompactBackend(ctx, svcReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.CompactBackendResponse{
		Name:        resp.Name,
		OperationId: resp.OperationID,
		StartedAt:   timestamppb.New(time.Unix(0, resp.StartedAt)),
	}, nil
}

// =============================================================================
// Helper Functions - Proto to Service Conversions
// =============================================================================

// protoBackendTypeToService converts a proto BackendType to a service BackendType.
func protoBackendTypeToService(t pb.BackendType) service.BackendType {
	switch t {
	case pb.BackendType_BACKEND_TYPE_LOCAL:
		return service.BackendTypeLocal
	case pb.BackendType_BACKEND_TYPE_S3:
		return service.BackendTypeS3
	case pb.BackendType_BACKEND_TYPE_GCS:
		return service.BackendTypeGCS
	case pb.BackendType_BACKEND_TYPE_AZURE:
		return service.BackendTypeAzure
	case pb.BackendType_BACKEND_TYPE_MINIO:
		return service.BackendTypeS3 // MinIO uses S3 compatibility
	case pb.BackendType_BACKEND_TYPE_PEBBLE:
		return service.BackendTypeLocal // Pebble is a local backend
	case pb.BackendType_BACKEND_TYPE_RAFT:
		return service.BackendTypeLocal // Raft-replicated is local+distributed
	default:
		return service.BackendTypeUnspecified
	}
}

// protoStorageTierToService converts a proto StorageTier to a service StorageTier.
func protoStorageTierToService(t pb.StorageTier) service.StorageTier {
	switch t {
	case pb.StorageTier_STORAGE_TIER_HOT:
		return service.StorageTierHot
	case pb.StorageTier_STORAGE_TIER_WARM:
		return service.StorageTierWarm
	case pb.StorageTier_STORAGE_TIER_COLD:
		return service.StorageTierCold
	case pb.StorageTier_STORAGE_TIER_ARCHIVE:
		return service.StorageTierCold // Map archive to cold
	default:
		return service.StorageTierUnspecified
	}
}

// =============================================================================
// Helper Functions - Service to Proto Conversions
// =============================================================================

// serviceBackendInfoToProto converts a service BackendInfo to a proto BackendInfo.
func serviceBackendInfoToProto(b *service.BackendInfo) *pb.BackendInfo {
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

// serviceTenantStatsToProto converts a service TenantStorageStats to a proto ObjectStorageTenantUsage.
func serviceTenantStatsToProto(t *service.TenantStorageStats) *pb.ObjectStorageTenantUsage {
	if t == nil {
		return nil
	}

	return &pb.ObjectStorageTenantUsage{
		TenantId:     t.TenantID,
		TotalBytes:   t.TotalBytes,
		ObjectCount:  t.ObjectCount,
		HotBytes:     t.HotBytes,
		WarmBytes:    t.WarmBytes,
		ColdBytes:    t.ColdBytes,
		ArchiveBytes: t.ArchiveBytes,
	}
}
