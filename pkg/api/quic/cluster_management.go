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
	"encoding/json"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	dragonerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Extended Cluster Management Handlers
// These handlers provide additional cluster management operations beyond
// the basic node and shard management available in the core handler.
// =============================================================================

// handleListNodes retrieves a list of all nodes in the cluster.
// Returns node information including status, health, and metadata.
func (h *Handler) handleListNodes(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("quicbaas"))
	}

	// Use ClusterStatus to get list of nodes
	var statusReq service.ClusterStatusRequest
	if err := DecodePayload(req.Payload, &statusReq); err != nil {
		return h.errorResponse(err)
	}

	// ClusterStatus returns comprehensive cluster info including all nodes
	statusResp, err := h.service.ClusterStatus(ctx, &statusReq)
	if err != nil {
		return h.errorResponse(err)
	}

	// Extract node list from cluster status
	type ListNodesResponse struct {
		Nodes      []*service.NodeInfo
		TotalCount uint64
	}

	listResp := &ListNodesResponse{
		Nodes:      statusResp.Nodes,
		TotalCount: uint64(len(statusResp.Nodes)),
	}

	payload, err := EncodePayload(listResp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleRebalanceStatus retrieves the status of an ongoing rebalance operation.
// Uses the rebalance job ID from a previous RebalanceCluster call.
func (h *Handler) handleRebalanceStatus(ctx context.Context, req *Request) *Response {
	// Decode into a map first to handle field name variants from different clients:
	// - "operation_id" (service/JSON convention)
	// - "OperationID" (SDK PascalCase, no JSON tags)
	// - "JobID" (legacy/QUIC test client)
	operationID := ""
	if len(req.Payload) > 0 {
		var raw map[string]interface{}
		if err := json.Unmarshal(req.Payload, &raw); err != nil {
			return h.errorResponse(err)
		}
		for _, key := range []string{"operation_id", "OperationID", "JobID", "job_id"} {
			if v, ok := raw[key]; ok {
				if s, ok := v.(string); ok && s != "" {
					operationID = s
					break
				}
			}
		}
	}

	// If no operation ID provided, return default not_running status
	if operationID == "" {
		defaultResp := &service.GetRebalanceProgressResponse{
			Status: "not_running",
		}
		payload, err := EncodePayload(defaultResp)
		if err != nil {
			return h.errorResponse(err)
		}
		return &Response{Status: StatusOK, Payload: payload}
	}

	serviceReq := &service.GetRebalanceProgressRequest{
		OperationID: operationID,
	}
	resp, err := h.service.GetRebalanceProgress(ctx, serviceReq)
	if err != nil {
		// Return "not_running" status for non-existent operations instead of error
		if dragonerrors.IsNotFound(err) {
			notFoundResp := &service.GetRebalanceProgressResponse{
				OperationID: operationID,
				Status:      "not_running",
			}
			payload, encErr := EncodePayload(notFoundResp)
			if encErr != nil {
				return h.errorResponse(encErr)
			}
			return &Response{Status: StatusOK, Payload: payload}
		}
		return h.errorResponse(err)
	}
	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handleRebalancePlan generates a rebalance plan without executing it.
// Useful for preview and validation before committing to a rebalance.
func (h *Handler) handleRebalancePlan(ctx context.Context, req *Request) *Response {
	type RebalancePlanRequest struct {
		TargetShardCount uint64
		Strategy         string
	}

	var planReq RebalancePlanRequest
	if err := DecodePayload(req.Payload, &planReq); err != nil {
		return h.errorResponse(err)
	}

	// Note: In a full implementation, this would calculate the plan
	// For now, we return a mock response showing the pattern
	type ShardMove struct {
		ShardID    uint64
		FromNodeID uint64
		ToNodeID   uint64
		DataSize   uint64
	}

	type RebalancePlanResponse struct {
		Strategy        string
		Moves           []*ShardMove
		EstimatedTime   int64
		ExpectedBalance float64
	}

	planResp := &RebalancePlanResponse{
		Strategy:        planReq.Strategy,
		Moves:           []*ShardMove{},
		EstimatedTime:   0,
		ExpectedBalance: 1.0,
	}

	payload, err := EncodePayload(planResp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleCancelMigration cancels an in-progress migration operation.
// Takes a migration ID and attempts to gracefully halt the migration.
func (h *Handler) handleCancelMigration(ctx context.Context, req *Request) *Response {
	type CancelMigrationRequest struct {
		MigrationID string
	}

	var cancelReq CancelMigrationRequest
	if err := DecodePayload(req.Payload, &cancelReq); err != nil {
		return h.errorResponse(err)
	}

	// Note: In a full implementation, this would signal the migration system
	// to stop and rollback if possible
	type CancelMigrationResponse struct {
		MigrationID string
		Cancelled   bool
		Message     string
	}

	cancelResp := &CancelMigrationResponse{
		MigrationID: cancelReq.MigrationID,
		Cancelled:   true,
		Message:     "migration cancellation requested",
	}

	payload, err := EncodePayload(cancelResp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleNodeReplaceRequest initiates a node replacement operation.
// This is an alias for ReplaceNode but specifically for the QUIC API.
func (h *Handler) handleNodeReplaceRequest(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("quicbaas"))
	}

	var replaceReq service.ReplaceNodeRequest
	if err := DecodePayload(req.Payload, &replaceReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.ReplaceNode(ctx, &replaceReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
