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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
)

func TestIsClusterService(t *testing.T) {
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
		// HasPrefix matches ClusterAdmin -- consistent with gRPC handler pattern
		{"prefix match with HasPrefix", "quicbaas.ClusterAdmin", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isClusterService(tt.service)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertClusterResponse_Rebalance(t *testing.T) {
	t.Run("converts rebalance response with job_id", func(t *testing.T) {
		data := map[string]interface{}{
			"job_id":               "job-123",
			"estimated_completion": float64(1700000000),
		}

		result, err := convertClusterResponse("Rebalance", data)
		require.NoError(t, err)

		resp, ok := result.(*cluster.RebalanceClusterResponse)
		require.True(t, ok, "expected *cluster.RebalanceClusterResponse")
		assert.Equal(t, "job-123", resp.OperationID)
		assert.Equal(t, int64(1700000000), resp.Started)
	})

	t.Run("converts rebalance response with PascalCase JobID", func(t *testing.T) {
		data := map[string]interface{}{
			"JobID":               "pascal-456",
			"EstimatedCompletion": float64(1700000000),
		}

		result, err := convertClusterResponse("Rebalance", data)
		require.NoError(t, err)

		resp, ok := result.(*cluster.RebalanceClusterResponse)
		require.True(t, ok)
		assert.Equal(t, "pascal-456", resp.OperationID)
		assert.Equal(t, int64(1700000000), resp.Started)
	})

	t.Run("converts rebalance response with operation_id fallback", func(t *testing.T) {
		data := map[string]interface{}{
			"operation_id": "op-789",
		}

		result, err := convertClusterResponse("Rebalance", data)
		require.NoError(t, err)

		resp, ok := result.(*cluster.RebalanceClusterResponse)
		require.True(t, ok)
		assert.Equal(t, "op-789", resp.OperationID)
	})

	t.Run("empty data returns zero-value response", func(t *testing.T) {
		data := map[string]interface{}{}

		result, err := convertClusterResponse("Rebalance", data)
		require.NoError(t, err)

		resp, ok := result.(*cluster.RebalanceClusterResponse)
		require.True(t, ok)
		assert.Equal(t, "", resp.OperationID)
		assert.Equal(t, int64(0), resp.Started)
	})
}

func TestConvertClusterResponse_GetRebalanceStatus(t *testing.T) {
	t.Run("converts full progress response with snake_case", func(t *testing.T) {
		data := map[string]interface{}{
			"operation_id":    "op-123",
			"status":          "in_progress",
			"progress":        float64(50.0),
			"shards_moved":    float64(5),
			"total_shards":    float64(10),
			"elapsed_seconds": float64(30.0),
		}

		result, err := convertClusterResponse("GetRebalanceStatus", data)
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

		result, err := convertClusterResponse("GetRebalanceStatus", data)
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

		result, err := convertClusterResponse("GetRebalanceStatus", data)
		require.NoError(t, err)

		resp, ok := result.(*cluster.GetRebalanceProgressResponse)
		require.True(t, ok)
		assert.Equal(t, int64(5000000000), resp.ElapsedTime)
	})

	t.Run("converts response with percent_complete field", func(t *testing.T) {
		data := map[string]interface{}{
			"operation_id":     "op-pct",
			"percent_complete": float64(75.0),
		}

		result, err := convertClusterResponse("GetRebalanceStatus", data)
		require.NoError(t, err)

		resp, ok := result.(*cluster.GetRebalanceProgressResponse)
		require.True(t, ok)
		assert.Equal(t, float64(75.0), resp.PercentComplete)
	})

	t.Run("empty data returns zero-value response", func(t *testing.T) {
		data := map[string]interface{}{}

		result, err := convertClusterResponse("GetRebalanceStatus", data)
		require.NoError(t, err)

		resp, ok := result.(*cluster.GetRebalanceProgressResponse)
		require.True(t, ok)
		assert.Equal(t, "", resp.OperationID)
		assert.Equal(t, "", resp.Status)
		assert.Equal(t, float64(0), resp.PercentComplete)
		assert.Equal(t, int32(0), resp.ShardsMoved)
		assert.Equal(t, int32(0), resp.TotalShards)
		assert.Equal(t, int64(0), resp.ElapsedTime)
	})
}

func TestConvertClusterResponse_UnknownMethod(t *testing.T) {
	t.Run("unknown method returns raw map", func(t *testing.T) {
		data := map[string]interface{}{
			"cluster_id": "test-cluster",
			"status":     "healthy",
		}

		result, err := convertClusterResponse("Status", data)
		require.NoError(t, err)

		// Should return the original data as-is
		mapResult, ok := result.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "test-cluster", mapResult["cluster_id"])
	})
}

func TestConvertClusterResponse_NilData(t *testing.T) {
	t.Run("nil data returns nil", func(t *testing.T) {
		result, err := convertClusterResponse("Rebalance", nil)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}
