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
	"testing"

	"github.com/stretchr/testify/assert"

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
		{"non-cluster service KV", "quicbaas.KV", false},
		{"non-cluster service Vector", "quicbaas.Vector", false},
		{"empty service", "", false},
		{"partial match", "quicbaas.ClusterAdmin", false},
		{"prefix only", "quicbaas.", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isClusterService(tt.service)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertRebalanceClusterResponse(t *testing.T) {
	t.Run("REST style response with operation_id", func(t *testing.T) {
		data := map[string]interface{}{
			"operation_id":         "op-123",
			"estimated_completion": float64(1700000000),
		}

		resp := convertRebalanceClusterResponse(data)
		assert.Equal(t, "op-123", resp.OperationID)
		assert.Equal(t, int64(1700000000), resp.Started)
	})

	t.Run("service style response with job_id", func(t *testing.T) {
		data := map[string]interface{}{
			"job_id":               "job-456",
			"estimated_completion": float64(1700000000),
		}

		resp := convertRebalanceClusterResponse(data)
		assert.Equal(t, "job-456", resp.OperationID)
		assert.Equal(t, int64(1700000000), resp.Started)
	})

	t.Run("empty data returns zero values", func(t *testing.T) {
		data := map[string]interface{}{}

		resp := convertRebalanceClusterResponse(data)
		assert.Equal(t, "", resp.OperationID)
		assert.Equal(t, int64(0), resp.Started)
	})

	t.Run("returns correct type", func(t *testing.T) {
		data := map[string]interface{}{
			"operation_id": "op-789",
		}

		resp := convertRebalanceClusterResponse(data)
		assert.IsType(t, &cluster.RebalanceClusterResponse{}, resp)
	})
}

func TestConvertGetRebalanceProgressResponse(t *testing.T) {
	t.Run("full response with all fields", func(t *testing.T) {
		data := map[string]interface{}{
			"operation_id":     "op-123",
			"status":           "in_progress",
			"percent_complete": float64(45.5),
			"shards_moved":     float64(10),
			"total_shards":     float64(20),
			"elapsed_time":     float64(5000000000),
		}

		resp := convertGetRebalanceProgressResponse(data)
		assert.Equal(t, "op-123", resp.OperationID)
		assert.Equal(t, "in_progress", resp.Status)
		assert.Equal(t, float64(45.5), resp.PercentComplete)
		assert.Equal(t, int32(10), resp.ShardsMoved)
		assert.Equal(t, int32(20), resp.TotalShards)
		assert.Equal(t, int64(5000000000), resp.ElapsedTime)
	})

	t.Run("response with progress field fallback", func(t *testing.T) {
		data := map[string]interface{}{
			"operation_id": "op-456",
			"status":       "completed",
			"progress":     float64(100.0),
		}

		resp := convertGetRebalanceProgressResponse(data)
		assert.Equal(t, "op-456", resp.OperationID)
		assert.Equal(t, float64(100.0), resp.PercentComplete)
	})

	t.Run("response with elapsed_seconds conversion", func(t *testing.T) {
		data := map[string]interface{}{
			"operation_id":    "op-789",
			"elapsed_seconds": float64(5.0),
		}

		resp := convertGetRebalanceProgressResponse(data)
		assert.Equal(t, int64(5000000000), resp.ElapsedTime) // 5 seconds in nanoseconds
	})

	t.Run("empty data returns zero values", func(t *testing.T) {
		data := map[string]interface{}{}

		resp := convertGetRebalanceProgressResponse(data)
		assert.Equal(t, "", resp.OperationID)
		assert.Equal(t, "", resp.Status)
		assert.Equal(t, float64(0), resp.PercentComplete)
		assert.Equal(t, int32(0), resp.ShardsMoved)
		assert.Equal(t, int32(0), resp.TotalShards)
		assert.Equal(t, int64(0), resp.ElapsedTime)
	})

	t.Run("returns correct type", func(t *testing.T) {
		data := map[string]interface{}{
			"operation_id": "op-test",
		}

		resp := convertGetRebalanceProgressResponse(data)
		assert.IsType(t, &cluster.GetRebalanceProgressResponse{}, resp)
	})
}
