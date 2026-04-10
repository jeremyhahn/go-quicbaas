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
	"strconv"
	"strings"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// Cluster service prefix for routing.
const clusterServicePrefix = "quicbaas.Cluster"

// isClusterService checks if the service name is a Cluster service.
func isClusterService(svc string) bool {
	return strings.HasPrefix(svc, clusterServicePrefix)
}

// clusterHandler handles Cluster service requests by converting between SDK types and protobuf types.
type clusterHandler struct {
	conn *grpc.ClientConn
}

// newClusterHandler creates a new cluster handler.
func newClusterHandler(conn *grpc.ClientConn) *clusterHandler {
	return &clusterHandler{conn: conn}
}

// clusterHandlerMap maps method names to handler functions for O(1) dispatch.
var clusterHandlerMap = map[string]func(h *clusterHandler, ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error){
	"Status":             (*clusterHandler).handleStatus,
	"Nodes":              (*clusterHandler).handleNodes,
	"AddNode":            (*clusterHandler).handleAddNode,
	"RemoveNode":         (*clusterHandler).handleRemoveNode,
	"Rebalance":          (*clusterHandler).handleRebalance,
	"GetRebalanceStatus": (*clusterHandler).handleGetRebalanceStatus,
}

// handleRequest routes Cluster requests to the appropriate handler based on method name.
func (h *clusterHandler) handleRequest(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	client := pb.NewQuicBaaSClient(h.conn)

	handler, ok := clusterHandlerMap[method]
	if !ok {
		return nil, errors.NewUnimplemented("clusterHandler.handleRequest",
			"unsupported method: "+method)
	}

	return handler(h, ctx, client, req)
}

// handleStatus handles Status requests by calling ClusterStatus.
func (h *clusterHandler) handleStatus(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	// Build protobuf request with metrics.
	pbReq := &pb.ClusterStatusRequest{
		IncludeMetrics: true,
	}

	// Call gRPC.
	pbResp, err := client.ClusterStatus(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "clusterHandler.handleStatus")
	}

	// Convert protobuf response to SDK response.
	members := make([]cluster.ClusterMember, 0, len(pbResp.Nodes))
	var leaderID string

	for _, node := range pbResp.Nodes {
		member := cluster.ClusterMember{
			ID:       strconv.FormatUint(node.NodeId, 10),
			Name:     node.Address, // Use address as name if name not provided
			Address:  node.Address,
			IsLeader: node.IsLeader,
			Healthy:  node.HealthStatus == pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		}
		members = append(members, member)

		if node.IsLeader {
			leaderID = member.ID
		}
	}

	// Determine overall cluster health.
	healthy := len(members) > 0
	for _, m := range members {
		if !m.Healthy {
			healthy = false
			break
		}
	}

	// Convert shard information for per-shard leader tracking.
	shards := make([]cluster.ClusterShard, 0, len(pbResp.Shards))
	for _, s := range pbResp.Shards {
		shards = append(shards, cluster.ClusterShard{
			ShardID:  s.ShardId,
			LeaderID: s.LeaderId,
			Replicas: s.Replicas,
		})
	}

	return &cluster.ClusterStatus{
		ClusterID: pbResp.ClusterId,
		LeaderID:  leaderID,
		Members:   members,
		Healthy:   healthy,
		Shards:    shards,
	}, nil
}

// handleNodes handles Nodes requests by calling ClusterStatus.
func (h *clusterHandler) handleNodes(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	// Build protobuf request.
	pbReq := &pb.ClusterStatusRequest{
		IncludeMetrics: false,
	}

	// Call gRPC.
	pbResp, err := client.ClusterStatus(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "clusterHandler.handleNodes")
	}

	// Convert protobuf response to SDK response.
	nodes := make([]cluster.NodeInfo, 0, len(pbResp.Nodes))
	for _, node := range pbResp.Nodes {
		nodes = append(nodes, cluster.NodeInfo{
			ID:      strconv.FormatUint(node.NodeId, 10),
			Name:    node.Address, // Use address as name if name not provided
			Address: node.Address,
			State:   convertHealthStatusToNodeState(node.HealthStatus),
		})
	}

	return &cluster.NodesResponse{
		Nodes: nodes,
	}, nil
}

// handleAddNode handles AddNode requests.
func (h *clusterHandler) handleAddNode(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*cluster.AddNodeRequest)
	if !ok {
		return nil, errors.NewInvalidInput("clusterHandler.handleAddNode",
			"invalid request type, expected *cluster.AddNodeRequest")
	}

	// The SDK AddNodeRequest uses a simple string address.
	// The protobuf AddNodeRequest requires shard_id, node_id, and address.
	// For SDK compatibility, we use shard_id=0 (default shard) and auto-assign node_id.
	pbReq := &pb.AddNodeRequest{
		ShardId: 0, // Default shard
		NodeId:  0, // Auto-assign
		Address: sdkReq.Address,
	}

	// Call gRPC.
	pbResp, err := client.AddNode(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "clusterHandler.handleAddNode")
	}

	// Check if the operation succeeded.
	if !pbResp.Success {
		return nil, errors.NewInternal("clusterHandler.handleAddNode", pbResp.Message)
	}

	// Return nil to indicate success (SDK expects nil response on success).
	return nil, nil
}

// handleRemoveNode handles RemoveNode requests.
func (h *clusterHandler) handleRemoveNode(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*cluster.RemoveNodeRequest)
	if !ok {
		return nil, errors.NewInvalidInput("clusterHandler.handleRemoveNode",
			"invalid request type, expected *cluster.RemoveNodeRequest")
	}

	// Parse the node ID from string to uint64.
	nodeID, err := strconv.ParseUint(sdkReq.NodeID, 10, 64)
	if err != nil {
		return nil, errors.NewInvalidInput("clusterHandler.handleRemoveNode",
			"invalid node ID format: "+sdkReq.NodeID)
	}

	// The SDK RemoveNodeRequest uses NodeID string.
	// The protobuf RemoveNodeRequest requires shard_id and node_id.
	pbReq := &pb.RemoveNodeRequest{
		ShardId: 0, // Default shard
		NodeId:  nodeID,
	}

	// Call gRPC.
	pbResp, err := client.RemoveNode(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "clusterHandler.handleRemoveNode")
	}

	// Check if the operation succeeded.
	if !pbResp.Success {
		return nil, errors.NewInternal("clusterHandler.handleRemoveNode", pbResp.Message)
	}

	// Return nil to indicate success (SDK expects nil response on success).
	return nil, nil
}

// handleRebalance handles Rebalance requests by calling RebalanceCluster.
func (h *clusterHandler) handleRebalance(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*cluster.RebalanceClusterRequest)
	if !ok {
		return nil, errors.NewInvalidInput("clusterHandler.handleRebalance",
			"invalid request type, expected *cluster.RebalanceClusterRequest")
	}

	pbReq := &pb.RebalanceClusterRequest{
		Strategy: sdkReq.Strategy,
	}

	pbResp, err := client.RebalanceCluster(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "clusterHandler.handleRebalance")
	}

	return &cluster.RebalanceClusterResponse{
		OperationID: pbResp.OperationId,
		Started:     pbResp.Started,
	}, nil
}

// handleGetRebalanceStatus handles GetRebalanceStatus requests by calling GetRebalanceProgress.
func (h *clusterHandler) handleGetRebalanceStatus(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*cluster.GetRebalanceProgressRequest)
	if !ok {
		return nil, errors.NewInvalidInput("clusterHandler.handleGetRebalanceStatus",
			"invalid request type, expected *cluster.GetRebalanceProgressRequest")
	}

	pbReq := &pb.GetRebalanceProgressRequest{
		OperationId: sdkReq.OperationID,
	}

	pbResp, err := client.GetRebalanceProgress(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "clusterHandler.handleGetRebalanceStatus")
	}

	return &cluster.GetRebalanceProgressResponse{
		OperationID:     pbResp.OperationId,
		Status:          pbResp.Status,
		PercentComplete: pbResp.PercentComplete,
		ShardsMoved:     pbResp.ShardsMoved,
		TotalShards:     pbResp.TotalShards,
		ElapsedTime:     pbResp.ElapsedTime,
	}, nil
}

// convertHealthStatusToNodeState converts a protobuf HealthStatus to an SDK NodeState.
func convertHealthStatusToNodeState(status pb.HealthStatus) cluster.NodeState {
	switch status {
	case pb.HealthStatus_HEALTH_STATUS_HEALTHY:
		return cluster.NodeStateHealthy
	case pb.HealthStatus_HEALTH_STATUS_DEGRADED, pb.HealthStatus_HEALTH_STATUS_UNHEALTHY:
		return cluster.NodeStateUnhealthy
	default:
		return cluster.NodeStateUnknown
	}
}
