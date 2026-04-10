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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// InitializeClusterDEK creates and distributes the initial cluster DEK.
func (s *Server) InitializeClusterDEK(ctx context.Context, req *pb.InitClusterDEKRequest) (*pb.InitClusterDEKResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Algorithm == "" {
		return nil, status.Error(codes.InvalidArgument, "algorithm is required")
	}

	if req.KeySize <= 0 {
		return nil, status.Error(codes.InvalidArgument, "key_size must be positive")
	}

	// Get cluster DEK service
	clusterDEKService := s.service.ClusterDEK()
	if clusterDEKService == nil {
		return nil, status.Error(codes.Unimplemented, "cluster DEK service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.InitClusterDEKRequest{
		Algorithm: req.Algorithm,
		KeySize:   int(req.KeySize),
		Metadata:  req.Metadata,
	}

	// Convert rotation policy if present
	if req.RotationPolicy != nil {
		serviceReq.RotationPolicy = &service.DEKRotationPolicy{
			Enabled:          req.RotationPolicy.Enabled,
			RotationInterval: req.RotationPolicy.RotationInterval.AsDuration(),
			AutoReencrypt:    req.RotationPolicy.AutoReencrypt,
		}
	}

	// Call service
	resp, err := clusterDEKService.InitializeClusterDEK(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	return &pb.InitClusterDEKResponse{
		Version:     resp.Version,
		CreatedAt:   timestamppb.New(resp.CreatedAt),
		NodesSynced: int32(resp.NodesSynced), //nolint:gosec // G115: count is bounded
		Algorithm:   resp.Algorithm,
		KeySize:     int32(resp.KeySize), //nolint:gosec // G115: key size is bounded
		Timestamp:   timestamppb.New(now),
	}, nil
}

// GetClusterDEKStatus retrieves current cluster DEK status and metadata.
func (s *Server) GetClusterDEKStatus(ctx context.Context, req *pb.GetClusterDEKStatusRequest) (*pb.ClusterDEKStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Get cluster DEK service
	clusterDEKService := s.service.ClusterDEK()
	if clusterDEKService == nil {
		return nil, status.Error(codes.Unimplemented, "cluster DEK service not available")
	}

	// Call service
	resp, err := clusterDEKService.GetClusterDEKStatus(ctx)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	protoResp := &pb.ClusterDEKStatusResponse{
		Initialized:            resp.Initialized,
		CurrentVersion:         resp.CurrentVersion,
		CreatedAt:              timestamppb.New(resp.CreatedAt),
		NodesSynced:            int32(resp.NodesSynced), //nolint:gosec // G115: count is bounded
		TotalNodes:             int32(resp.TotalNodes),  //nolint:gosec // G115: count is bounded
		Algorithm:              resp.Algorithm,
		KeySize:                int32(resp.KeySize), //nolint:gosec // G115: key size is bounded
		ReencryptionInProgress: resp.ReencryptionInProgress,
		ReencryptionProgress:   resp.ReencryptionProgress,
		Timestamp:              timestamppb.New(now),
	}

	// Set optional timestamp fields
	if resp.RotatedAt != nil {
		protoResp.RotatedAt = timestamppb.New(*resp.RotatedAt)
	}
	if resp.NextRotation != nil {
		protoResp.NextRotation = timestamppb.New(*resp.NextRotation)
	}

	// Convert nodes out of sync
	if len(resp.NodesOutOfSync) > 0 {
		protoResp.NodesOutOfSync = make([]*pb.NodeDEKInfo, len(resp.NodesOutOfSync))
		for i, node := range resp.NodesOutOfSync {
			protoResp.NodesOutOfSync[i] = &pb.NodeDEKInfo{
				NodeId:         node.NodeID,
				NodeName:       node.NodeName,
				CurrentVersion: node.CurrentVersion,
				LastSyncedAt:   timestamppb.New(node.LastSyncedAt),
			}
		}
	}

	// Convert previous versions
	if len(resp.PreviousVersions) > 0 {
		protoResp.PreviousVersions = make([]*pb.DEKVersionInfo, len(resp.PreviousVersions))
		for i, v := range resp.PreviousVersions {
			protoVersion := &pb.DEKVersionInfo{
				Version:           v.Version,
				CreatedAt:         timestamppb.New(v.CreatedAt),
				EntitiesEncrypted: v.EntitiesEncrypted,
				CanDelete:         v.CanDelete,
			}
			if v.DeprecatedAt != nil {
				protoVersion.DeprecatedAt = timestamppb.New(*v.DeprecatedAt)
			}
			protoResp.PreviousVersions[i] = protoVersion
		}
	}

	return protoResp, nil
}

// RotateClusterDEK generates a new cluster DEK and initiates re-encryption.
func (s *Server) RotateClusterDEK(ctx context.Context, req *pb.RotateClusterDEKRequest) (*pb.RotateClusterDEKResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Reason == "" {
		return nil, status.Error(codes.InvalidArgument, "reason is required")
	}

	// Get cluster DEK service
	clusterDEKService := s.service.ClusterDEK()
	if clusterDEKService == nil {
		return nil, status.Error(codes.Unimplemented, "cluster DEK service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.RotateClusterDEKRequest{
		Reason:                req.Reason,
		ForceRotation:         req.ForceRotation,
		AutoStartReencryption: req.AutoStartReencryption,
		Metadata:              req.Metadata,
	}

	// Set scheduled time if present
	if req.ScheduledTime != nil && req.ScheduledTime.IsValid() {
		scheduledTime := req.ScheduledTime.AsTime()
		serviceReq.ScheduledTime = &scheduledTime
	}

	// Call service
	resp, err := clusterDEKService.RotateClusterDEK(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	protoResp := &pb.RotateClusterDEKResponse{
		NewVersion:          resp.NewVersion,
		OldVersion:          resp.OldVersion,
		RotatedAt:           timestamppb.New(resp.RotatedAt),
		RotationId:          resp.RotationID,
		NodesSynced:         int32(resp.NodesSynced), //nolint:gosec // G115: count is bounded
		ReencryptionStarted: resp.ReencryptionStarted,
		Timestamp:           timestamppb.New(now),
	}

	if resp.EstimatedReencryptionTime > 0 {
		protoResp.EstimatedReencryptionTime = durationpb.New(resp.EstimatedReencryptionTime)
	}

	return protoResp, nil
}

// SyncNodeDEK synchronizes cluster DEK to a specific node.
func (s *Server) SyncNodeDEK(ctx context.Context, req *pb.SyncNodeDEKRequest) (*pb.SyncNodeDEKResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.NodeId == 0 {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}

	// Get cluster DEK service
	clusterDEKService := s.service.ClusterDEK()
	if clusterDEKService == nil {
		return nil, status.Error(codes.Unimplemented, "cluster DEK service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.SyncNodeDEKRequest{
		NodeID:         req.NodeId,
		CurrentVersion: req.CurrentVersion,
		ForceResync:    req.ForceResync,
	}

	// Call service
	resp, err := clusterDEKService.SyncNodeDEK(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	return &pb.SyncNodeDEKResponse{
		NodeId:         resp.NodeID,
		VersionsSynced: resp.VersionsSynced,
		CurrentVersion: resp.CurrentVersion,
		SyncedAt:       timestamppb.New(resp.SyncedAt),
		Timestamp:      timestamppb.New(now),
	}, nil
}

// GetNodeDEKStatus retrieves DEK sync status for a specific node.
func (s *Server) GetNodeDEKStatus(ctx context.Context, req *pb.GetNodeDEKStatusRequest) (*pb.GetNodeDEKStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.NodeId == 0 {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}

	// Get cluster DEK service
	clusterDEKService := s.service.ClusterDEK()
	if clusterDEKService == nil {
		return nil, status.Error(codes.Unimplemented, "cluster DEK service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.GetNodeDEKStatusRequest{
		NodeID: req.NodeId,
	}

	// Call service
	resp, err := clusterDEKService.GetNodeDEKStatus(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	return &pb.GetNodeDEKStatusResponse{
		NodeId:            resp.NodeID,
		NodeName:          resp.NodeName,
		CurrentVersion:    resp.CurrentVersion,
		LastSyncedAt:      timestamppb.New(resp.LastSyncedAt),
		Status:            nodeDEKSyncStatusToProto(resp.Status),
		Error:             resp.Error,
		VersionsAvailable: resp.VersionsAvailable,
		Timestamp:         timestamppb.New(now),
	}, nil
}

// StartReencryption manually initiates re-encryption of data with new DEK.
func (s *Server) ClusterStartReencryption(ctx context.Context, req *pb.ClusterStartReencryptionRequest) (*pb.ClusterStartReencryptionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Get cluster DEK service
	clusterDEKService := s.service.ClusterDEK()
	if clusterDEKService == nil {
		return nil, status.Error(codes.Unimplemented, "cluster DEK service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.StartReencryptionRequest{
		Path:         req.Path,
		FromVersion:  req.FromVersion,
		ToVersion:    req.ToVersion,
		BatchSize:    int(req.BatchSize),
		Concurrency:  int(req.Concurrency),
		ThrottleRate: int(req.ThrottleRate),
		Force:        req.Force,
	}

	// Call service
	resp, err := clusterDEKService.StartReencryption(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	return &pb.ClusterStartReencryptionResponse{
		OperationId:         resp.OperationID,
		Path:                resp.Path,
		Started:             resp.Started,
		EstimatedItems:      int32(resp.EstimatedItems), //nolint:gosec // G115: count is bounded
		StartedAt:           timestamppb.New(resp.StartedAt),
		TotalEntities:       resp.TotalEntities,
		EstimatedCompletion: timestamppb.New(resp.EstimatedCompletion),
		Timestamp:           timestamppb.New(now),
	}, nil
}

// GetReencryptionStatus retrieves current re-encryption progress.
func (s *Server) GetReencryptionStatus(ctx context.Context, req *pb.GetReencryptionStatusRequest) (*pb.ReencryptionStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Get cluster DEK service
	clusterDEKService := s.service.ClusterDEK()
	if clusterDEKService == nil {
		return nil, status.Error(codes.Unimplemented, "cluster DEK service not available")
	}

	// Call service
	resp, err := clusterDEKService.GetReencryptionStatus(ctx)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	protoResp := &pb.ReencryptionStatusResponse{
		OperationId:       resp.OperationID,
		State:             reencryptionStateToProto(resp.State),
		StartedAt:         timestamppb.New(resp.StartedAt),
		Progress:          resp.Progress,
		TotalEntities:     resp.TotalEntities,
		ProcessedEntities: resp.ProcessedEntities,
		FailedEntities:    resp.FailedEntities,
		ThroughputRate:    resp.ThroughputRate,
		Timestamp:         timestamppb.New(now),
	}

	// Set optional timestamp fields
	if resp.CompletedAt != nil {
		protoResp.CompletedAt = timestamppb.New(*resp.CompletedAt)
	}
	if resp.EstimatedCompletion != nil {
		protoResp.EstimatedCompletion = timestamppb.New(*resp.EstimatedCompletion)
	}

	// Convert node progress
	if len(resp.NodeProgress) > 0 {
		protoResp.NodeProgress = make([]*pb.NodeReencryptionProgress, len(resp.NodeProgress))
		for i, np := range resp.NodeProgress {
			protoResp.NodeProgress[i] = &pb.NodeReencryptionProgress{
				NodeId:            np.NodeID,
				NodeName:          np.NodeName,
				Progress:          np.Progress,
				ProcessedEntities: np.ProcessedEntities,
				TotalEntities:     np.TotalEntities,
				ThroughputRate:    np.ThroughputRate,
				LastUpdatedAt:     timestamppb.New(np.LastUpdatedAt),
			}
		}
	}

	// Convert errors
	if len(resp.Errors) > 0 {
		protoResp.Errors = make([]*pb.ReencryptionError, len(resp.Errors))
		for i, e := range resp.Errors {
			protoResp.Errors[i] = &pb.ReencryptionError{
				Timestamp: timestamppb.New(e.Timestamp),
				NodeId:    e.NodeID,
				EntityId:  e.EntityID,
				Error:     e.Error,
				Retried:   e.Retried,
			}
		}
	}

	return protoResp, nil
}

// CancelReencryption cancels ongoing re-encryption process.
func (s *Server) CancelReencryption(ctx context.Context, req *pb.CancelReencryptionRequest) (*pb.CancelReencryptionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Get cluster DEK service
	clusterDEKService := s.service.ClusterDEK()
	if clusterDEKService == nil {
		return nil, status.Error(codes.Unimplemented, "cluster DEK service not available")
	}

	// Call service
	err := clusterDEKService.CancelReencryption(ctx)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	return &pb.CancelReencryptionResponse{
		Success:     true,
		OperationId: req.OperationId,
		Timestamp:   timestamppb.New(now),
	}, nil
}

// RecoverDEK recovers cluster DEK using recovery keys or backup mechanism.
func (s *Server) RecoverDEK(ctx context.Context, req *pb.RecoverDEKRequest) (*pb.RecoverDEKResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Reason == "" {
		return nil, status.Error(codes.InvalidArgument, "reason is required")
	}

	if req.RecoveryMethod == pb.RecoveryMethod_RECOVERY_METHOD_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "recovery_method is required")
	}

	// Get cluster DEK service
	clusterDEKService := s.service.ClusterDEK()
	if clusterDEKService == nil {
		return nil, status.Error(codes.Unimplemented, "cluster DEK service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.RecoverDEKRequest{
		RecoveryKeys:   req.RecoveryKeys,
		BackupLocation: req.BackupLocation,
		ManualKey:      req.ManualKey,
		RecoveryMethod: protoRecoveryMethodToService(req.RecoveryMethod),
		Reason:         req.Reason,
	}

	// Call service
	resp, err := clusterDEKService.RecoverDEK(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	return &pb.RecoverDEKResponse{
		Success:          resp.Success,
		RecoveredVersion: resp.RecoveredVersion,
		RecoveredAt:      timestamppb.New(resp.RecoveredAt),
		NodesSynced:      int32(resp.NodesSynced), //nolint:gosec // G115: count is bounded
		AuditId:          resp.AuditID,
		Warnings:         resp.Warnings,
		Timestamp:        timestamppb.New(now),
	}, nil
}

// =============================================================================
// Conversion Helpers
// =============================================================================

// nodeDEKSyncStatusToProto converts service NodeDEKSyncStatus to proto NodeDEKSyncStatus.
func nodeDEKSyncStatusToProto(status service.NodeDEKSyncStatus) pb.NodeDEKSyncStatus {
	switch status {
	case service.NodeDEKSynced:
		return pb.NodeDEKSyncStatus_NODE_DEK_SYNC_STATUS_SYNCED
	case service.NodeDEKOutOfDate:
		return pb.NodeDEKSyncStatus_NODE_DEK_SYNC_STATUS_OUT_OF_DATE
	case service.NodeDEKMissing:
		return pb.NodeDEKSyncStatus_NODE_DEK_SYNC_STATUS_MISSING
	case service.NodeDEKSyncFailed:
		return pb.NodeDEKSyncStatus_NODE_DEK_SYNC_STATUS_SYNC_FAILED
	default:
		return pb.NodeDEKSyncStatus_NODE_DEK_SYNC_STATUS_UNSPECIFIED
	}
}

// reencryptionStateToProto converts service ReencryptionState to proto ReencryptionState.
func reencryptionStateToProto(state service.ReencryptionState) pb.ReencryptionState {
	switch state {
	case service.ReencryptionRunning:
		return pb.ReencryptionState_REENCRYPTION_STATE_RUNNING
	case service.ReencryptionPaused:
		return pb.ReencryptionState_REENCRYPTION_STATE_PAUSED
	case service.ReencryptionCompleted:
		return pb.ReencryptionState_REENCRYPTION_STATE_COMPLETED
	case service.ReencryptionFailed:
		return pb.ReencryptionState_REENCRYPTION_STATE_FAILED
	case service.ReencryptionCancelled:
		return pb.ReencryptionState_REENCRYPTION_STATE_CANCELLED
	default:
		return pb.ReencryptionState_REENCRYPTION_STATE_UNSPECIFIED
	}
}

// protoRecoveryMethodToService converts proto RecoveryMethod to service RecoveryMethod.
func protoRecoveryMethodToService(method pb.RecoveryMethod) service.RecoveryMethod {
	switch method {
	case pb.RecoveryMethod_RECOVERY_METHOD_KEYS:
		return service.RecoveryMethodKeys
	case pb.RecoveryMethod_RECOVERY_METHOD_BACKUP:
		return service.RecoveryMethodBackup
	case pb.RecoveryMethod_RECOVERY_METHOD_MANUAL:
		return service.RecoveryMethodManual
	default:
		return service.RecoveryMethodKeys
	}
}
