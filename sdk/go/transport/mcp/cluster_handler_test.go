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

package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
)

func TestIsMCPClusterService(t *testing.T) {
	tests := []struct {
		name     string
		service  string
		expected bool
	}{
		{"fully qualified service", "quicbaas.Cluster", true},
		{"bare service name", "Cluster", true},
		{"nested package prefix", "pkg.quicbaas.Cluster", true},
		{"non-cluster service KV", "quicbaas.KV", false},
		{"non-cluster service Vector", "quicbaas.Vector", false},
		{"empty service", "", false},
		{"partial match ClusterAdmin", "quicbaas.ClusterAdmin", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isMCPClusterService(tt.service)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTransformClusterResponse_Rebalance(t *testing.T) {
	t.Run("converts rebalance response with job_id", func(t *testing.T) {
		data := map[string]interface{}{
			"job_id":               "job-123",
			"estimated_completion": float64(1700000000),
		}

		result, err := transformClusterResponse("Rebalance", data)
		require.NoError(t, err)

		resp, ok := result.(*cluster.RebalanceClusterResponse)
		require.True(t, ok, "expected *cluster.RebalanceClusterResponse")
		assert.Equal(t, "job-123", resp.OperationID)
		assert.Equal(t, int64(1700000000), resp.Started)
	})

	t.Run("converts rebalance response with operation_id fallback", func(t *testing.T) {
		data := map[string]interface{}{
			"operation_id": "op-456",
		}

		result, err := transformClusterResponse("Rebalance", data)
		require.NoError(t, err)

		resp, ok := result.(*cluster.RebalanceClusterResponse)
		require.True(t, ok)
		assert.Equal(t, "op-456", resp.OperationID)
	})

	t.Run("converts rebalance response with PascalCase JobID", func(t *testing.T) {
		data := map[string]interface{}{
			"JobID":               "pascal-789",
			"EstimatedCompletion": float64(1700000000),
		}

		result, err := transformClusterResponse("Rebalance", data)
		require.NoError(t, err)

		resp, ok := result.(*cluster.RebalanceClusterResponse)
		require.True(t, ok)
		assert.Equal(t, "pascal-789", resp.OperationID)
		assert.Equal(t, int64(1700000000), resp.Started)
	})
}

func TestTransformClusterResponse_GetRebalanceStatus(t *testing.T) {
	t.Run("converts full progress response", func(t *testing.T) {
		data := map[string]interface{}{
			"operation_id":    "op-123",
			"status":          "in_progress",
			"progress":        float64(50.0),
			"shards_moved":    float64(5),
			"total_shards":    float64(10),
			"elapsed_seconds": float64(30.0),
		}

		result, err := transformClusterResponse("GetRebalanceStatus", data)
		require.NoError(t, err)

		resp, ok := result.(*cluster.GetRebalanceProgressResponse)
		require.True(t, ok, "expected *cluster.GetRebalanceProgressResponse")
		assert.Equal(t, "op-123", resp.OperationID)
		assert.Equal(t, "in_progress", resp.Status)
		assert.Equal(t, float64(50.0), resp.PercentComplete)
		assert.Equal(t, int32(5), resp.ShardsMoved)
		assert.Equal(t, int32(10), resp.TotalShards)
		assert.Equal(t, int64(30000000000), resp.ElapsedTime) // 30s in nanoseconds
	})

	t.Run("converts PascalCase progress response", func(t *testing.T) {
		data := map[string]interface{}{
			"OperationID":    "op-pascal",
			"Status":         "completed",
			"Progress":       float64(100.0),
			"ShardsMoved":    float64(10),
			"TotalShards":    float64(10),
			"ElapsedSeconds": float64(60.0),
		}

		result, err := transformClusterResponse("GetRebalanceStatus", data)
		require.NoError(t, err)

		resp, ok := result.(*cluster.GetRebalanceProgressResponse)
		require.True(t, ok)
		assert.Equal(t, "op-pascal", resp.OperationID)
		assert.Equal(t, "completed", resp.Status)
		assert.Equal(t, float64(100.0), resp.PercentComplete)
		assert.Equal(t, int32(10), resp.ShardsMoved)
		assert.Equal(t, int32(10), resp.TotalShards)
		assert.Equal(t, int64(60000000000), resp.ElapsedTime)
	})

	t.Run("converts response with elapsed_time directly", func(t *testing.T) {
		data := map[string]interface{}{
			"operation_id": "op-direct",
			"elapsed_time": float64(5000000000),
		}

		result, err := transformClusterResponse("GetRebalanceStatus", data)
		require.NoError(t, err)

		resp, ok := result.(*cluster.GetRebalanceProgressResponse)
		require.True(t, ok)
		assert.Equal(t, int64(5000000000), resp.ElapsedTime)
	})
}

func TestTransformClusterResponse_UnknownMethod(t *testing.T) {
	t.Run("unknown method returns raw result", func(t *testing.T) {
		data := map[string]interface{}{
			"cluster_id": "test-cluster",
			"status":     "healthy",
		}

		result, err := transformClusterResponse("Status", data)
		require.NoError(t, err)

		// Should return the original data as-is
		assert.Equal(t, data, result)
	})
}

func TestTransformClusterResponse_NonMapResult(t *testing.T) {
	t.Run("non-map result returns as-is", func(t *testing.T) {
		result, err := transformClusterResponse("Rebalance", "raw string")
		require.NoError(t, err)
		assert.Equal(t, "raw string", result)
	})

	t.Run("nil result returns nil", func(t *testing.T) {
		result, err := transformClusterResponse("Rebalance", nil)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}
