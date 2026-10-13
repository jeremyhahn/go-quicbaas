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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// MigrateData initiates data migration between storage backends.
func (s *Server) MigrateData(ctx context.Context, req *pb.MigrateDataRequest) (*pb.MigrateDataResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.SourceBackend == "" {
		return nil, status.Error(codes.InvalidArgument, "source_backend is required")
	}

	if req.DestinationBackend == "" {
		return nil, status.Error(codes.InvalidArgument, "destination_backend is required")
	}

	// Call service layer
	serviceReq := &service.MigrateDataRequest{
		SourceShard: 0,          // Map from source_backend string to shard ID
		TargetShard: 1,          // Map from destination_backend string to shard ID
		EntityTypes: []string{}, // Can be populated from request if needed
		Concurrency: int(req.Concurrency),
	}

	if serviceReq.Concurrency == 0 {
		serviceReq.Concurrency = 4 // Default concurrency
	}

	serviceResp, err := s.service.MigrateData(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.MigrateDataResponse{
		OperationId: serviceResp.MigrationID,
		Started:     0, // Service doesn't return timestamp currently
	}, nil
}

// GetMigrationProgress returns the current migration progress.
func (s *Server) GetMigrationProgress(ctx context.Context, req *pb.GetMigrationProgressRequest) (*pb.GetMigrationProgressResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.OperationId == "" {
		return nil, status.Error(codes.InvalidArgument, "operation_id is required")
	}

	// Call service layer
	serviceReq := &service.GetMigrationProgressRequest{
		MigrationID: req.OperationId,
	}

	serviceResp, err := s.service.GetMigrationProgress(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.GetMigrationProgressResponse{
		OperationId:        serviceResp.MigrationID,
		KeysProcessed:      serviceResp.ItemsMigrated,
		BytesProcessed:     0, // Service doesn't track bytes currently
		EstimatedTotal:     serviceResp.TotalItems,
		PercentComplete:    float64(serviceResp.Progress),
		ElapsedTime:        0, // Service doesn't return elapsed time currently
		EstimatedRemaining: serviceResp.ETA,
		CurrentRate:        0, // Service doesn't return rate currently
		Status:             serviceResp.Status,
	}, nil
}

// GetRebalanceProgress returns the current rebalance progress.
func (s *Server) GetRebalanceProgress(ctx context.Context, req *pb.GetRebalanceProgressRequest) (*pb.GetRebalanceProgressResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.OperationId == "" {
		return nil, status.Error(codes.InvalidArgument, "operation_id is required")
	}

	// Query service layer for rebalance progress
	serviceReq := &service.GetRebalanceProgressRequest{
		OperationID: req.OperationId,
	}

	serviceResp, err := s.service.GetRebalanceProgress(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	var narrow responseNarrower
	out := &pb.GetRebalanceProgressResponse{
		OperationId:     serviceResp.OperationID,
		ShardsMoved:     narrow.int32("shards_moved", serviceResp.ShardsMoved),
		TotalShards:     narrow.int32("total_shards", serviceResp.TotalShards),
		PercentComplete: serviceResp.Progress,
		ElapsedTime:     int64(serviceResp.ElapsedSeconds),
		Status:          serviceResp.Status,
	}
	if err := narrow.err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ReplaceNode replaces a failed node in the cluster.
func (s *Server) ReplaceNode(ctx context.Context, req *pb.ReplaceNodeRequest) (*pb.ReplaceNodeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.FailedNodeId == 0 {
		return nil, status.Error(codes.InvalidArgument, "failed_node_id is required")
	}

	if req.ReplacementNodeId == 0 {
		return nil, status.Error(codes.InvalidArgument, "replacement_node_id is required")
	}

	if req.ReplacementAddress == "" {
		return nil, status.Error(codes.InvalidArgument, "replacement_address is required")
	}

	// Call service layer
	serviceReq := &service.ReplaceNodeRequest{
		ShardID:    shards.PlatformShardID, // Default shard - should be determined from req
		OldNodeID:  req.FailedNodeId,
		NewNodeID:  req.ReplacementNodeId,
		NewAddress: req.ReplacementAddress,
	}

	serviceResp, err := s.service.ReplaceNode(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.ReplaceNodeResponse{
		Success: serviceResp.JobID != "",
		Message: serviceResp.Status,
	}, nil
}

// UpgradeNode initiates a rolling upgrade for a node.
// If NodeId is 0, initiates a rolling upgrade for all nodes.
func (s *Server) UpgradeNode(ctx context.Context, req *pb.UpgradeNodeRequest) (*pb.UpgradeNodeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// NodeId 0 means upgrade all nodes - this is a valid request

	if req.TargetVersion == "" {
		return nil, status.Error(codes.InvalidArgument, "target_version is required")
	}

	// Call service layer
	serviceReq := &service.UpgradeNodeRequest{
		NodeID:        req.NodeId,
		TargetVersion: req.TargetVersion,
	}

	serviceResp, err := s.service.UpgradeNode(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.UpgradeNodeResponse{
		OperationId: serviceResp.JobID,
		Started:     0, // Service doesn't return timestamp currently
	}, nil
}

// GetUpgradeStatus returns the current upgrade status.
func (s *Server) GetUpgradeStatus(ctx context.Context, req *pb.GetUpgradeStatusRequest) (*pb.GetUpgradeStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.OperationId == "" {
		return nil, status.Error(codes.InvalidArgument, "operation_id is required")
	}

	// Call service layer
	serviceReq := &service.GetUpgradeStatusRequest{
		JobID: req.OperationId,
	}

	serviceResp, err := s.service.GetUpgradeStatus(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.GetUpgradeStatusResponse{
		OperationId:     serviceResp.JobID,
		NodesUpgraded:   0, // Service doesn't return this currently
		TotalNodes:      0, // Service doesn't return this currently
		PercentComplete: float64(serviceResp.Progress),
		CurrentVersion:  serviceResp.CurrentVersion,
		TargetVersion:   serviceResp.TargetVersion,
		Status:          serviceResp.Status,
	}, nil
}
