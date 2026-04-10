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
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// clusterHandler handles Cluster REST requests with proper response type conversion.
type clusterHandler struct {
	transport *Transport
}

// newClusterHandler creates a new cluster handler.
func newClusterHandler(t *Transport) *clusterHandler {
	return &clusterHandler{transport: t}
}

// isClusterService returns true if the service is a Cluster service.
func isClusterService(service string) bool {
	svc := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		svc = service[idx+1:]
	}
	return svc == "Cluster"
}

// clusterMethodHandler defines a handler function for cluster methods.
type clusterMethodHandler func(ctx context.Context, req interface{}) (interface{}, error)

// handleRequest routes cluster requests to appropriate handlers using map-based dispatch.
func (h *clusterHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	handlers := map[string]clusterMethodHandler{
		"Rebalance":          h.handleRebalance,
		"GetRebalanceStatus": h.handleGetRebalanceStatus,
	}

	handler, ok := handlers[method]
	if !ok {
		// Fall through to generic REST request for methods that don't need
		// response type conversion (Status, Nodes, AddNode, RemoveNode).
		return h.transport.requestWithRetry(ctx, "quicbaas.Cluster", method, req)
	}

	return handler(ctx, req)
}

// handleRebalance handles the Rebalance cluster request.
func (h *clusterHandler) handleRebalance(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*cluster.RebalanceClusterRequest)
	if !ok {
		return nil, errors.NewInvalidInput("rest.cluster.handleRebalance",
			"invalid request type, expected *cluster.RebalanceClusterRequest")
	}

	restReq := map[string]interface{}{
		"strategy": sdkReq.Strategy,
	}

	var respMap map[string]interface{}
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/cluster/rebalance", restReq, &respMap); err != nil {
		return nil, err
	}

	return convertRebalanceClusterResponse(respMap), nil
}

// handleGetRebalanceStatus handles the GetRebalanceStatus request.
func (h *clusterHandler) handleGetRebalanceStatus(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*cluster.GetRebalanceProgressRequest)
	if !ok {
		return nil, errors.NewInvalidInput("rest.cluster.handleGetRebalanceStatus",
			"invalid request type, expected *cluster.GetRebalanceProgressRequest")
	}

	endpoint := "/api/v1/cluster/rebalance/status?operation_id=" + url.QueryEscape(sdkReq.OperationID)

	var respMap map[string]interface{}
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &respMap); err != nil {
		return nil, err
	}

	return convertGetRebalanceProgressResponse(respMap), nil
}

// convertRebalanceClusterResponse converts a raw map response to the SDK RebalanceClusterResponse.
func convertRebalanceClusterResponse(data map[string]interface{}) *cluster.RebalanceClusterResponse {
	resp := &cluster.RebalanceClusterResponse{}

	// REST server returns "operation_id", service returns "job_id"
	if v, ok := data["operation_id"].(string); ok {
		resp.OperationID = v
	} else if v, ok := data["job_id"].(string); ok {
		resp.OperationID = v
	}

	// REST server returns "started" as a time string; service returns "estimated_completion"
	if v, ok := data["estimated_completion"].(float64); ok {
		resp.Started = int64(v)
	}

	return resp
}

// convertGetRebalanceProgressResponse converts a raw map response to the SDK GetRebalanceProgressResponse.
func convertGetRebalanceProgressResponse(data map[string]interface{}) *cluster.GetRebalanceProgressResponse {
	resp := &cluster.GetRebalanceProgressResponse{}

	if v, ok := data["operation_id"].(string); ok {
		resp.OperationID = v
	}
	if v, ok := data["status"].(string); ok {
		resp.Status = v
	}
	if v, ok := data["percent_complete"].(float64); ok {
		resp.PercentComplete = v
	} else if v, ok := data["progress"].(float64); ok {
		resp.PercentComplete = v
	}
	if v, ok := data["shards_moved"].(float64); ok {
		resp.ShardsMoved = int32(v)
	}
	if v, ok := data["total_shards"].(float64); ok {
		resp.TotalShards = int32(v)
	}
	if v, ok := data["elapsed_time"].(float64); ok {
		resp.ElapsedTime = int64(v)
	} else if v, ok := data["elapsed_seconds"].(float64); ok {
		resp.ElapsedTime = int64(v * 1e9) // Convert seconds to nanoseconds
	}

	return resp
}
