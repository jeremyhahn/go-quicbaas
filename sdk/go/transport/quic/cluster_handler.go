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
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
)

// clusterServicePrefix is the service name prefix for Cluster operations.
const clusterServicePrefix = "quicbaas.Cluster"

// isClusterService checks if the service name is a Cluster service.
func isClusterService(svc string) bool {
	if strings.HasPrefix(svc, clusterServicePrefix) {
		return true
	}
	// Also match bare "Cluster" service name
	if idx := strings.LastIndex(svc, "."); idx >= 0 {
		return svc[idx+1:] == "Cluster"
	}
	return svc == "Cluster"
}

// convertClusterResponse converts a raw map response to the appropriate SDK cluster type.
func convertClusterResponse(method string, data map[string]interface{}) (interface{}, error) {
	if data == nil {
		return nil, nil
	}

	// Map-based dispatch for cluster response converters.
	type converterFunc func(map[string]interface{}) interface{}
	converters := map[string]converterFunc{
		"Rebalance":          func(d map[string]interface{}) interface{} { return convertRebalanceResponse(d) },
		"GetRebalanceStatus": func(d map[string]interface{}) interface{} { return convertGetRebalanceProgressResponse(d) },
	}

	converter, ok := converters[method]
	if !ok {
		// Return raw map for methods that don't need type conversion
		// (Status, Nodes, AddNode, RemoveNode).
		return data, nil
	}

	return converter(data), nil
}

// convertRebalanceResponse converts a raw map response to the SDK RebalanceClusterResponse.
// The QUIC server marshals service.RebalanceClusterResponse which uses JSON tags:
// job_id, progress, estimated_completion.
func convertRebalanceResponse(data map[string]interface{}) *cluster.RebalanceClusterResponse {
	resp := &cluster.RebalanceClusterResponse{}

	// Service type uses "job_id" JSON tag
	if v, ok := data["job_id"].(string); ok {
		resp.OperationID = v
	}
	// PascalCase fallback (Go struct marshaling without JSON tags)
	if resp.OperationID == "" {
		if v, ok := data["JobID"].(string); ok {
			resp.OperationID = v
		}
	}
	// REST-style response fallback
	if resp.OperationID == "" {
		if v, ok := data["operation_id"].(string); ok {
			resp.OperationID = v
		}
	}

	// Service type uses "estimated_completion" JSON tag
	if v, ok := data["estimated_completion"].(float64); ok {
		resp.Started = int64(v)
	}
	// PascalCase fallback
	if resp.Started == 0 {
		if v, ok := data["EstimatedCompletion"].(float64); ok {
			resp.Started = int64(v)
		}
	}

	return resp
}

// convertGetRebalanceProgressResponse converts a raw map response to the SDK GetRebalanceProgressResponse.
// The QUIC server marshals service.GetRebalanceProgressResponse which uses JSON tags:
// operation_id, status, progress, message, shards_moved, total_shards, start_time, elapsed_seconds, error.
func convertGetRebalanceProgressResponse(data map[string]interface{}) *cluster.GetRebalanceProgressResponse {
	resp := &cluster.GetRebalanceProgressResponse{}

	if v, ok := data["operation_id"].(string); ok {
		resp.OperationID = v
	} else if v, ok := data["OperationID"].(string); ok {
		resp.OperationID = v
	}

	if v, ok := data["status"].(string); ok {
		resp.Status = v
	} else if v, ok := data["Status"].(string); ok {
		resp.Status = v
	}

	if v, ok := data["progress"].(float64); ok {
		resp.PercentComplete = v
	} else if v, ok := data["Progress"].(float64); ok {
		resp.PercentComplete = v
	} else if v, ok := data["percent_complete"].(float64); ok {
		resp.PercentComplete = v
	}

	if v, ok := data["shards_moved"].(float64); ok {
		resp.ShardsMoved = int32(v)
	} else if v, ok := data["ShardsMoved"].(float64); ok {
		resp.ShardsMoved = int32(v)
	}

	if v, ok := data["total_shards"].(float64); ok {
		resp.TotalShards = int32(v)
	} else if v, ok := data["TotalShards"].(float64); ok {
		resp.TotalShards = int32(v)
	}

	if v, ok := data["elapsed_seconds"].(float64); ok {
		resp.ElapsedTime = int64(v * 1e9) // Convert seconds to nanoseconds
	} else if v, ok := data["ElapsedSeconds"].(float64); ok {
		resp.ElapsedTime = int64(v * 1e9)
	} else if v, ok := data["elapsed_time"].(float64); ok {
		resp.ElapsedTime = int64(v)
	}

	return resp
}
