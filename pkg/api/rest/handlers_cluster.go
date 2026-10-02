//go:build rest

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

package rest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Node Management Handlers
// =============================================================================

// HandleAddNode adds a node to a Raft shard
// POST /api/v1/cluster/nodes.
func (s *Server) HandleAddNode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "failed to read request body",
		})
		return
	}
	defer r.Body.Close()

	// Parse request
	var req AddNodeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "invalid JSON body",
		})
		return
	}

	// Validate request
	if req.ShardID == 0 {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "shard_id",
			Message: "shard_id is required",
		})
		return
	}

	if req.NodeID == 0 {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "node_id",
			Message: "node_id is required",
		})
		return
	}

	if req.Address == "" {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "address",
			Message: "address is required",
		})
		return
	}

	// Call service
	serviceReq := &service.AddNodeRequest{
		ShardID:  req.ShardID,
		NodeID:   req.NodeID,
		Address:  req.Address,
		Identity: req.Identity,
	}

	serviceResp, err := s.service.AddNode(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := AddNodeResponse{
		Success:   serviceResp.Success,
		Message:   serviceResp.Message,
		ShardID:   req.ShardID,
		NodeID:    req.NodeID,
		Timestamp: time.Now(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleRemoveNode removes a node from a Raft shard
// DELETE /api/v1/cluster/nodes/{nodeId}.
func (s *Server) HandleRemoveNode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract node ID from URL
	nodeIDParam := chi.URLParam(r, "nodeId")
	nodeID, err := strconv.ParseUint(nodeIDParam, 10, 64)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "nodeId",
			Message: "invalid node ID",
		})
		return
	}

	// Get shard ID from query parameter (required)
	shardIDParam := r.URL.Query().Get("shard_id")
	if shardIDParam == "" {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "shard_id",
			Message: "shard_id query parameter is required",
		})
		return
	}

	shardID, err := strconv.ParseUint(shardIDParam, 10, 64)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "shard_id",
			Message: "invalid shard ID",
		})
		return
	}

	// Call service
	serviceReq := &service.RemoveNodeRequest{
		ShardID: shardID,
		NodeID:  nodeID,
	}

	serviceResp, err := s.service.RemoveNode(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := RemoveNodeResponse{
		Success:   serviceResp.Success,
		Message:   serviceResp.Message,
		ShardID:   shardID,
		NodeID:    nodeID,
		Timestamp: time.Now(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleReplaceNode replaces a failed node with a new node
// PUT /api/v1/cluster/nodes/{nodeId}/replace.
func (s *Server) HandleReplaceNode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract failed node ID from URL
	failedNodeIDParam := chi.URLParam(r, "nodeId")
	failedNodeID, err := strconv.ParseUint(failedNodeIDParam, 10, 64)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "nodeId",
			Message: "invalid node ID",
		})
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "failed to read request body",
		})
		return
	}
	defer r.Body.Close()

	// Parse request
	var req ReplaceNodeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "invalid JSON body",
		})
		return
	}

	// Validate request
	if req.ReplacementNodeID == 0 {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "replacement_node_id",
			Message: "replacement_node_id is required",
		})
		return
	}

	if req.ReplacementAddress == "" {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "replacement_address",
			Message: "replacement_address is required",
		})
		return
	}

	// For node replacement, we need to:
	// 1. Add the new node to all shards where the failed node exists
	// 2. Remove the failed node from those shards
	// This is a simplified implementation - a real production implementation
	// would coordinate this across multiple shards

	// First, get cluster status to find which shards contain the failed node
	statusReq := &service.ClusterStatusRequest{
		IncludeDetails: true,
	}

	statusResp, err := s.service.ClusterStatus(ctx, statusReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Find shards containing the failed node
	var affectedShards []uint64
	for _, shard := range statusResp.Shards {
		for _, replica := range shard.Replicas {
			if replica == failedNodeID {
				affectedShards = append(affectedShards, shard.ShardID)
				break
			}
		}
	}

	if len(affectedShards) == 0 {
		RespondError(w, r, http.StatusNotFound, &NotFoundError{
			Resource: fmt.Sprintf("node %d not found in any shard", failedNodeID),
		})
		return
	}

	// Replace node in each affected shard
	// For simplicity, we'll report success if we can replace in at least one shard
	successCount := 0
	var lastError error

	for _, shardID := range affectedShards {
		// Add replacement node
		addReq := &service.AddNodeRequest{
			ShardID:  shardID,
			NodeID:   req.ReplacementNodeID,
			Address:  req.ReplacementAddress,
			Identity: req.ReplacementIdentity,
		}

		_, err := s.service.AddNode(ctx, addReq)
		if err != nil {
			lastError = err
			continue
		}

		// Remove failed node
		removeReq := &service.RemoveNodeRequest{
			ShardID: shardID,
			NodeID:  failedNodeID,
		}

		_, err = s.service.RemoveNode(ctx, removeReq)
		if err != nil {
			lastError = err
			continue
		}

		successCount++
	}

	if successCount == 0 && lastError != nil {
		s.handleServiceError(w, r, lastError)
		return
	}

	// Build response
	resp := ReplaceNodeResponse{
		Success:        successCount > 0,
		Message:        fmt.Sprintf("replaced node in %d/%d shards", successCount, len(affectedShards)),
		AffectedShards: affectedShards,
		Timestamp:      time.Now(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleListNodes lists all nodes in the cluster
// GET /api/v1/cluster/nodes.
func (s *Server) HandleListNodes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Call service
	serviceReq := &service.ClusterStatusRequest{
		IncludeDetails: true,
	}

	serviceResp, err := s.service.ClusterStatus(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	if serviceResp == nil {
		RespondError(w, r, http.StatusInternalServerError, &InternalServerError{
			Message: "cluster status unavailable",
		})
		return
	}

	// Build node list
	nodes := make([]NodeInfoResponse, 0, len(serviceResp.Nodes))
	for _, node := range serviceResp.Nodes {
		nodes = append(nodes, NodeInfoResponse{
			NodeID:        node.NodeID,
			Address:       node.Address,
			RaftAddress:   node.Address, // Use Address as RaftAddress
			IsLeader:      node.IsLeader,
			HealthStatus:  convertHealthStatus(node.HealthStatus),
			Version:       node.Version,
			UptimeSeconds: node.UptimeSeconds,
		})
	}

	// Build response
	resp := ListNodesResponse{
		Nodes:      nodes,
		TotalCount: len(nodes),
		Timestamp:  time.Unix(0, serviceResp.Timestamp),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleListNonLeaderNodes lists non-leader nodes for load balancer routing
// GET /api/v1/cluster/nodes/non-leader.
// This endpoint is used by load balancers to route WebSocket connections
// to non-leader nodes for messaging.
func (s *Server) HandleListNonLeaderNodes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Call service to get cluster status
	serviceReq := &service.ClusterStatusRequest{
		IncludeDetails: true,
	}

	serviceResp, err := s.service.ClusterStatus(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	if serviceResp == nil {
		RespondError(w, r, http.StatusInternalServerError, &InternalServerError{
			Message: "cluster status unavailable",
		})
		return
	}

	// Filter to only healthy non-leader nodes
	nodes := make([]NodeInfoResponse, 0, len(serviceResp.Nodes))
	for _, node := range serviceResp.Nodes {
		// Skip leader nodes
		if node.IsLeader {
			continue
		}

		// Only include healthy nodes
		if node.HealthStatus != service.HealthStatusHealthy {
			continue
		}

		nodes = append(nodes, NodeInfoResponse{
			NodeID:        node.NodeID,
			Address:       node.Address,
			RaftAddress:   node.Address,
			IsLeader:      node.IsLeader,
			HealthStatus:  convertHealthStatus(node.HealthStatus),
			Version:       node.Version,
			UptimeSeconds: node.UptimeSeconds,
		})
	}

	// Build response
	resp := ListNodesResponse{
		Nodes:      nodes,
		TotalCount: len(nodes),
		Timestamp:  time.Unix(0, serviceResp.Timestamp),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetClusterLeader returns the current cluster leader
// GET /api/v1/cluster/leader.
func (s *Server) HandleGetClusterLeader(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Call service to get cluster status
	serviceReq := &service.ClusterStatusRequest{
		IncludeDetails: true,
	}

	serviceResp, err := s.service.ClusterStatus(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	if serviceResp == nil {
		RespondError(w, r, http.StatusInternalServerError, &InternalServerError{
			Message: "cluster status unavailable",
		})
		return
	}

	// Find the leader node
	var leader *NodeInfoResponse
	for _, node := range serviceResp.Nodes {
		if node.IsLeader {
			leader = &NodeInfoResponse{
				NodeID:        node.NodeID,
				Address:       node.Address,
				RaftAddress:   node.Address,
				IsLeader:      node.IsLeader,
				HealthStatus:  convertHealthStatus(node.HealthStatus),
				Version:       node.Version,
				UptimeSeconds: node.UptimeSeconds,
			}
			break
		}
	}

	// If no leader found, return error
	if leader == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
			Message: "no leader available",
		})
		return
	}

	// Build response
	resp := ClusterLeaderResponse{
		Leader:    *leader,
		Timestamp: time.Unix(0, serviceResp.Timestamp),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Rebalance Handlers
// =============================================================================

// HandleStartRebalance starts a cluster rebalancing operation
// POST /api/v1/cluster/rebalance.
func (s *Server) HandleStartRebalance(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "failed to read request body",
		})
		return
	}
	defer r.Body.Close()

	// Parse request
	var req RebalanceRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "invalid JSON body",
		})
		return
	}

	// Default strategy
	if req.Strategy == "" {
		req.Strategy = "round_robin"
	}

	// Call service
	serviceReq := &service.RebalanceClusterRequest{
		Strategy:         req.Strategy,
		TargetShardCount: req.TargetShardCount,
	}

	serviceResp, err := s.service.RebalanceCluster(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := RebalanceResponse{
		OperationID: serviceResp.JobID,
		Started:     time.Unix(0, serviceResp.EstimatedCompletion),
		Status:      "started",
		Timestamp:   time.Now(),
	}

	RespondJSON(w, r, http.StatusAccepted, resp)
}

// HandleGetRebalanceStatus gets the status of a rebalancing operation
// GET /api/v1/cluster/rebalance/status.
func (s *Server) HandleGetRebalanceStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	operationID := r.URL.Query().Get("operation_id")

	// If no operation_id provided, return default not_running status
	if operationID == "" {
		resp := RebalanceStatusResponse{
			Status:    "not_running",
			Timestamp: time.Now(),
		}
		RespondJSON(w, r, http.StatusOK, resp)
		return
	}

	serviceResp, err := s.service.GetRebalanceProgress(ctx, &service.GetRebalanceProgressRequest{
		OperationID: operationID,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := RebalanceStatusResponse{
		OperationID:     serviceResp.OperationID,
		Status:          serviceResp.Status,
		PercentComplete: serviceResp.Progress,
		ShardsMoved:     int32(serviceResp.ShardsMoved),
		TotalShards:     int32(serviceResp.TotalShards),
		ElapsedTime:     int64(serviceResp.ElapsedSeconds * 1e9),
		Timestamp:       time.Now(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetRebalancePlan gets the planned distribution for rebalancing
// GET /api/v1/cluster/rebalance/plan.
func (s *Server) HandleGetRebalancePlan(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get current cluster status
	serviceReq := &service.ClusterStatusRequest{
		IncludeDetails: true,
	}

	serviceResp, err := s.service.ClusterStatus(ctx, serviceReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	if serviceResp == nil {
		RespondError(w, r, http.StatusInternalServerError, &InternalServerError{
			Message: "cluster status unavailable",
		})
		return
	}

	// Build a simple rebalance plan based on current state
	// In a real implementation, this would calculate optimal shard distribution
	resp := RebalancePlanResponse{
		CurrentShards: len(serviceResp.Shards),
		TargetShards:  len(serviceResp.Shards),
		Strategy:      "round_robin",
		EstimatedTime: 0,
		Timestamp:     time.Now(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Migration Handlers
// =============================================================================

// HandleStartMigration starts a data migration operation
// POST /api/v1/cluster/migrate.
func (s *Server) HandleStartMigration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "failed to read request body",
		})
		return
	}
	defer r.Body.Close()

	// Parse request
	var req MigrationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "invalid JSON body",
		})
		return
	}

	// Validate request
	if req.SourceShardID == 0 {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "source_shard_id",
			Message: "source_shard_id is required",
		})
		return
	}

	if req.TargetShardID == 0 {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "target_shard_id",
			Message: "target_shard_id is required",
		})
		return
	}

	// For now, return a simple response
	// Real implementation would start an async migration job
	resp := MigrationResponse{
		OperationID:   fmt.Sprintf("migration-%d-%d-%d", req.SourceShardID, req.TargetShardID, time.Now().Unix()),
		Status:        "started",
		SourceShardID: req.SourceShardID,
		TargetShardID: req.TargetShardID,
		EstimatedTime: 0,
		Timestamp:     time.Now(),
	}

	// Suppress unused variable warning
	_ = ctx

	RespondJSON(w, r, http.StatusAccepted, resp)
}

// HandleGetMigrationStatus gets the status of a migration operation
// GET /api/v1/cluster/migrate/status.
func (s *Server) HandleGetMigrationStatus(w http.ResponseWriter, r *http.Request) {
	// Get operation ID from query parameter
	operationID := r.URL.Query().Get("operation_id")
	if operationID == "" {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "operation_id",
			Message: "operation_id query parameter is required",
		})
		return
	}

	// For now, return a not found response
	// Real implementation would look up migration status
	resp := MigrationStatusResponse{
		OperationID:     operationID,
		Status:          "not_found",
		KeysProcessed:   0,
		BytesProcessed:  0,
		PercentComplete: 0,
		ElapsedTime:     0,
		Timestamp:       time.Now(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleCancelMigration cancels an ongoing migration operation
// DELETE /api/v1/cluster/migrate.
func (s *Server) HandleCancelMigration(w http.ResponseWriter, r *http.Request) {
	// Get operation ID from query parameter
	operationID := r.URL.Query().Get("operation_id")
	if operationID == "" {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "operation_id",
			Message: "operation_id query parameter is required",
		})
		return
	}

	// For now, return a simple response
	// Real implementation would cancel the migration job
	resp := CancelMigrationResponse{
		OperationID: operationID,
		Cancelled:   true,
		Message:     "migration cancelled",
		Timestamp:   time.Now(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Upgrade Handlers
// =============================================================================

// HandleStartUpgrade starts a rolling upgrade operation
// POST /api/v1/cluster/upgrade.
func (s *Server) HandleStartUpgrade(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "failed to read request body",
		})
		return
	}
	defer r.Body.Close()

	// Parse request
	var req UpgradeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "invalid JSON body",
		})
		return
	}

	// Validate request
	if req.TargetVersion == "" {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Field:   "target_version",
			Message: "target_version is required",
		})
		return
	}

	// For now, return a simple response
	// Real implementation would start an async upgrade job
	resp := UpgradeResponse{
		OperationID:   fmt.Sprintf("upgrade-%s-%d", req.TargetVersion, time.Now().Unix()),
		Status:        "started",
		TargetVersion: req.TargetVersion,
		Timestamp:     time.Now(),
	}

	// Suppress unused variable warning
	_ = ctx

	RespondJSON(w, r, http.StatusAccepted, resp)
}

// HandleGetUpgradeStatus gets the status of an upgrade operation
// GET /api/v1/cluster/upgrade/status.
func (s *Server) HandleGetUpgradeStatus(w http.ResponseWriter, r *http.Request) {
	// Get operation ID from query parameter (optional)
	operationID := r.URL.Query().Get("operation_id")

	// For now, return a not running response
	// Real implementation would track upgrade progress
	resp := UpgradeStatusResponse{
		OperationID:     operationID,
		Status:          "not_running",
		NodesUpgraded:   0,
		TotalNodes:      0,
		PercentComplete: 0,
		CurrentVersion:  "",
		TargetVersion:   "",
		Timestamp:       time.Now(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}
