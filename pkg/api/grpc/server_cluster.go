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
)

// AddNode adds a node to a Raft shard.
func (s *Server) AddNode(ctx context.Context, req *pb.AddNodeRequest) (*pb.AddNodeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.ShardId == 0 {
		return nil, status.Error(codes.InvalidArgument, "shard_id is required")
	}

	if req.NodeId == 0 {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}

	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address is required")
	}

	// Call service
	serviceReq := &service.AddNodeRequest{
		ShardID: req.ShardId,
		NodeID:  req.NodeId,
		Address: req.Address,
	}

	serviceResp, err := s.service.AddNode(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.AddNodeResponse{
		Success: serviceResp.Success,
		Message: serviceResp.Message,
		ShardId: req.ShardId,
		NodeId:  req.NodeId,
	}, nil
}

// RemoveNode removes a node from a Raft shard.
func (s *Server) RemoveNode(ctx context.Context, req *pb.RemoveNodeRequest) (*pb.RemoveNodeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.ShardId == 0 {
		return nil, status.Error(codes.InvalidArgument, "shard_id is required")
	}

	if req.NodeId == 0 {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}

	// Call service
	serviceReq := &service.RemoveNodeRequest{
		ShardID: req.ShardId,
		NodeID:  req.NodeId,
	}

	serviceResp, err := s.service.RemoveNode(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.RemoveNodeResponse{
		Success: serviceResp.Success,
		Message: serviceResp.Message,
		ShardId: req.ShardId,
		NodeId:  req.NodeId,
	}, nil
}

// CreateShard creates a new Raft shard with initial members.
func (s *Server) CreateShard(ctx context.Context, req *pb.CreateShardRequest) (*pb.CreateShardResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.ShardId == 0 {
		return nil, status.Error(codes.InvalidArgument, "shard_id is required")
	}

	if len(req.InitialMembers) == 0 {
		return nil, status.Error(codes.InvalidArgument, "initial_members is required")
	}

	// Call service
	serviceReq := &service.CreateShardRequest{
		ShardID:        req.ShardId,
		InitialMembers: req.InitialMembers,
	}

	serviceResp, err := s.service.CreateShard(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.CreateShardResponse{
		Success:  serviceResp.Status == "created",
		Message:  serviceResp.Status,
		ShardId:  serviceResp.ShardID,
		LeaderId: serviceResp.LeaderID,
	}, nil
}

// RebalanceCluster redistributes data across shards for better balance.
func (s *Server) RebalanceCluster(ctx context.Context, req *pb.RebalanceClusterRequest) (*pb.RebalanceClusterResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Call service
	serviceReq := &service.RebalanceClusterRequest{
		Strategy: req.Strategy,
	}

	serviceResp, err := s.service.RebalanceCluster(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.RebalanceClusterResponse{
		OperationId: serviceResp.JobID,
		Started:     serviceResp.EstimatedCompletion,
	}, nil
}
