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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
)

func TestVectorHandler_TransformCreateNamespaceRequest(t *testing.T) {
	t.Run("valid request", func(t *testing.T) {
		vh := &vectorHandler{}

		req := &vector.CreateNamespaceRequest{
			Config: &vector.NamespaceConfig{
				Namespace:  "test-ns",
				Dimensions: 128,
				IndexType:  vector.IndexTypeHNSW,
				IndexParams: map[string]string{
					"m":               "16",
					"ef_construction": "200",
				},
				Metadata: map[string]string{
					"env": "test",
				},
			},
		}

		args, err := vh.transformCreateNamespace(req)
		require.NoError(t, err)
		assert.Equal(t, "test-ns", args["namespace"])
		assert.Equal(t, int32(128), args["dimensions"])
		assert.Equal(t, "hnsw", args["index_type"])
		assert.Equal(t, "cosine", args["distance_metric"])
		assert.NotNil(t, args["index_params"])
		assert.NotNil(t, args["metadata"])
	})

	t.Run("nil config returns error", func(t *testing.T) {
		vh := &vectorHandler{}

		req := &vector.CreateNamespaceRequest{
			Config: nil,
		}

		_, err := vh.transformCreateNamespace(req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "config is required")
	})

	t.Run("invalid request type returns error", func(t *testing.T) {
		vh := &vectorHandler{}

		_, err := vh.transformCreateNamespace("invalid")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid request type")
	})
}

func TestVectorHandler_TransformUpsertRequest(t *testing.T) {
	t.Run("valid request", func(t *testing.T) {
		vh := &vectorHandler{}

		req := &vector.UpsertRequest{
			Vector: &vector.VectorEntity{
				ID:        "vec-1",
				Namespace: "test-ns",
				Vector:    []float32{0.1, 0.2, 0.3},
				Metadata: map[string]string{
					"label": "test",
				},
			},
		}

		args, err := vh.transformUpsert(req)
		require.NoError(t, err)
		assert.Equal(t, "vec-1", args["id"])
		assert.Equal(t, "test-ns", args["namespace"])
		assert.Equal(t, []float32{0.1, 0.2, 0.3}, args["vector"])
		assert.NotNil(t, args["metadata"])
	})

	t.Run("nil vector returns error", func(t *testing.T) {
		vh := &vectorHandler{}

		req := &vector.UpsertRequest{
			Vector: nil,
		}

		_, err := vh.transformUpsert(req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "vector is required")
	})
}

func TestVectorHandler_TransformSearchSimilarRequest(t *testing.T) {
	t.Run("valid request", func(t *testing.T) {
		vh := &vectorHandler{}

		req := &vector.SearchSimilarRequest{
			Namespace:       "test-ns",
			QueryVector:     []float32{0.1, 0.2, 0.3},
			K:               10,
			EfSearch:        50,
			MinScore:        0.8,
			IncludeVectors:  true,
			IncludeMetadata: true,
		}

		args, err := vh.transformSearchSimilar(req)
		require.NoError(t, err)
		assert.Equal(t, "test-ns", args["namespace"])
		assert.Equal(t, []float32{0.1, 0.2, 0.3}, args["query_vector"])
		assert.Equal(t, int32(10), args["k"])
		assert.Equal(t, int32(50), args["ef_search"])
		assert.Equal(t, float32(0.8), args["min_score"])
		assert.Equal(t, true, args["include_vectors"])
		assert.Equal(t, true, args["include_metadata"])
	})
}

func TestVectorHandler_TransformHybridSearchRequest(t *testing.T) {
	t.Run("valid request with filters", func(t *testing.T) {
		vh := &vectorHandler{}

		req := &vector.HybridSearchRequest{
			Namespace:   "test-ns",
			QueryVector: []float32{0.1, 0.2, 0.3},
			K:           10,
			MetadataFilters: []*vector.MetadataFilter{
				{
					Key:      "label",
					Operator: vector.FilterOperatorEquals,
					Value:    "test",
				},
			},
			IncludeVectors:  true,
			IncludeMetadata: true,
		}

		args, err := vh.transformHybridSearch(req)
		require.NoError(t, err)
		assert.Equal(t, "test-ns", args["namespace"])
		assert.Equal(t, []float32{0.1, 0.2, 0.3}, args["query_vector"])
		assert.Equal(t, int32(10), args["k"])

		filters, ok := args["metadata_filters"].([]map[string]interface{})
		require.True(t, ok)
		require.Len(t, filters, 1)
		assert.Equal(t, "label", filters[0]["key"])
		assert.Equal(t, "eq", filters[0]["operator"])
		assert.Equal(t, "test", filters[0]["value"])
	})
}

func TestVectorHandler_TransformCreateNamespaceResponse(t *testing.T) {
	t.Run("valid response", func(t *testing.T) {
		vh := &vectorHandler{}

		resp := map[string]interface{}{
			"namespace": "test-ns",
			"timestamp": "2025-01-28T12:00:00Z",
		}

		result, err := vh.transformCreateNamespaceResponse(resp)
		require.NoError(t, err)

		createResp, ok := result.(*vector.CreateNamespaceResponse)
		require.True(t, ok)
		assert.Equal(t, "test-ns", createResp.Namespace)
		assert.True(t, createResp.Success)
	})

	t.Run("nil response returns nil", func(t *testing.T) {
		vh := &vectorHandler{}

		result, err := vh.transformCreateNamespaceResponse(nil)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}

func TestVectorHandler_TransformUpsertResponse(t *testing.T) {
	t.Run("valid response", func(t *testing.T) {
		vh := &vectorHandler{}

		resp := map[string]interface{}{
			"id":        "vec-1",
			"version":   float64(1),
			"timestamp": "2025-01-28T12:00:00Z",
		}

		result, err := vh.transformUpsertResponse(resp)
		require.NoError(t, err)

		upsertResp, ok := result.(*vector.UpsertResponse)
		require.True(t, ok)
		assert.Equal(t, "vec-1", upsertResp.ID)
		assert.Equal(t, int64(1), upsertResp.Version)
		assert.True(t, upsertResp.Success)
	})
}

func TestVectorHandler_TransformGetResponse(t *testing.T) {
	t.Run("valid response", func(t *testing.T) {
		vh := &vectorHandler{}

		resp := map[string]interface{}{
			"id":        "vec-1",
			"namespace": "test-ns",
			"vector":    []interface{}{0.1, 0.2, 0.3},
			"metadata": map[string]interface{}{
				"label": "test",
			},
			"version":   float64(1),
			"timestamp": "2025-01-28T12:00:00Z",
		}

		result, err := vh.transformGetResponse(resp)
		require.NoError(t, err)

		getResp, ok := result.(*vector.GetResponse)
		require.True(t, ok)
		assert.True(t, getResp.Found)
		require.NotNil(t, getResp.Vector)
		assert.Equal(t, "vec-1", getResp.Vector.ID)
		assert.Equal(t, "test-ns", getResp.Vector.Namespace)
		assert.Len(t, getResp.Vector.Vector, 3)
	})

	t.Run("nil response returns not found", func(t *testing.T) {
		vh := &vectorHandler{}

		result, err := vh.transformGetResponse(nil)
		require.NoError(t, err)

		getResp, ok := result.(*vector.GetResponse)
		require.True(t, ok)
		assert.False(t, getResp.Found)
	})
}

func TestVectorHandler_TransformSearchSimilarResponse(t *testing.T) {
	t.Run("valid response", func(t *testing.T) {
		vh := &vectorHandler{}

		resp := map[string]interface{}{
			"results": []interface{}{
				map[string]interface{}{
					"score":    float64(0.95),
					"distance": float64(0.05),
					"rank":     float64(1),
					"vector": map[string]interface{}{
						"id":        "vec-1",
						"namespace": "test-ns",
					},
				},
			},
			"total_candidates": float64(100),
			"search_time_ms":   float64(5),
			"timestamp":        "2025-01-28T12:00:00Z",
		}

		result, err := vh.transformSearchSimilarResponse(resp)
		require.NoError(t, err)

		searchResp, ok := result.(*vector.SearchSimilarResponse)
		require.True(t, ok)
		require.Len(t, searchResp.Results, 1)
		assert.Equal(t, float32(0.95), searchResp.Results[0].Score)
		assert.Equal(t, float32(0.05), searchResp.Results[0].Distance)
		assert.Equal(t, int32(1), searchResp.Results[0].Rank)
		assert.Equal(t, int64(100), searchResp.TotalCandidates)
		assert.Equal(t, int64(5), searchResp.SearchTimeMs)
	})

	t.Run("nil response returns empty results", func(t *testing.T) {
		vh := &vectorHandler{}

		result, err := vh.transformSearchSimilarResponse(nil)
		require.NoError(t, err)

		searchResp, ok := result.(*vector.SearchSimilarResponse)
		require.True(t, ok)
		assert.Empty(t, searchResp.Results)
	})
}

func TestVectorHandler_TransformListNamespacesResponse(t *testing.T) {
	t.Run("valid response", func(t *testing.T) {
		vh := &vectorHandler{}

		resp := map[string]interface{}{
			"namespaces": []interface{}{
				map[string]interface{}{
					"namespace":    "ns-1",
					"dimensions":   float64(128),
					"index_type":   "hnsw",
					"vector_count": float64(1000),
				},
				map[string]interface{}{
					"namespace":    "ns-2",
					"dimensions":   float64(256),
					"index_type":   "flat",
					"vector_count": float64(500),
				},
			},
			"total_count":     float64(2),
			"next_page_token": "token123",
			"timestamp":       "2025-01-28T12:00:00Z",
		}

		result, err := vh.transformListNamespacesResponse(resp)
		require.NoError(t, err)

		listResp, ok := result.(*vector.ListNamespacesResponse)
		require.True(t, ok)
		require.Len(t, listResp.Namespaces, 2)
		assert.Equal(t, "ns-1", listResp.Namespaces[0].Config.Namespace)
		assert.Equal(t, int32(128), listResp.Namespaces[0].Config.Dimensions)
		assert.Equal(t, "ns-2", listResp.Namespaces[1].Config.Namespace)
		assert.Equal(t, int64(2), listResp.TotalCount)
		assert.Equal(t, "token123", listResp.NextPageToken)
	})
}

func TestVectorHandler_HelperFunctions(t *testing.T) {
	t.Run("mcpGetString", func(t *testing.T) {
		m := map[string]interface{}{
			"key": "value",
		}
		assert.Equal(t, "value", mcpGetString(m, "key"))
		assert.Equal(t, "", mcpGetString(m, "missing"))
	})

	t.Run("mcpGetBool", func(t *testing.T) {
		m := map[string]interface{}{
			"key": true,
		}
		assert.True(t, mcpGetBool(m, "key"))
		assert.False(t, mcpGetBool(m, "missing"))
	})

	t.Run("mcpGetInt64", func(t *testing.T) {
		m := map[string]interface{}{
			"key": float64(42),
		}
		assert.Equal(t, int64(42), mcpGetInt64(m, "key"))
		assert.Equal(t, int64(0), mcpGetInt64(m, "missing"))
	})

	t.Run("mcpGetFloat32", func(t *testing.T) {
		m := map[string]interface{}{
			"key": float64(3.14),
		}
		assert.InDelta(t, float32(3.14), mcpGetFloat32(m, "key"), 0.001)
		assert.Equal(t, float32(0), mcpGetFloat32(m, "missing"))
	})

	t.Run("mcpGetTime", func(t *testing.T) {
		m := map[string]interface{}{
			"rfc3339":     "2025-01-28T12:00:00Z",
			"rfc3339nano": "2025-01-28T12:00:00.123456789Z",
		}
		expected := time.Date(2025, 1, 28, 12, 0, 0, 0, time.UTC)
		assert.Equal(t, expected, mcpGetTime(m, "rfc3339"))
		assert.False(t, mcpGetTime(m, "rfc3339nano").IsZero())
		assert.True(t, mcpGetTime(m, "missing").IsZero())
	})

	t.Run("stringMapToInterface", func(t *testing.T) {
		m := map[string]string{
			"key1": "value1",
			"key2": "value2",
		}
		result := stringMapToInterface(m)
		assert.Equal(t, "value1", result["key1"])
		assert.Equal(t, "value2", result["key2"])
		assert.Nil(t, stringMapToInterface(nil))
	})

	t.Run("interfaceToStringMap", func(t *testing.T) {
		m := map[string]interface{}{
			"key1": "value1",
			"key2": 123, // Non-string value should be ignored
		}
		result := interfaceToStringMap(m)
		assert.Equal(t, "value1", result["key1"])
		assert.Equal(t, "", result["key2"])
	})
}

func TestIsMCPVectorService(t *testing.T) {
	t.Run("quicbaas.Vector service", func(t *testing.T) {
		assert.True(t, isMCPVectorService("quicbaas.Vector"))
	})

	t.Run("plain Vector service", func(t *testing.T) {
		assert.True(t, isMCPVectorService("Vector"))
	})

	t.Run("non-Vector service", func(t *testing.T) {
		assert.False(t, isMCPVectorService("quicbaas.KV"))
		assert.False(t, isMCPVectorService("quicbaas.Secrets"))
	})
}

func TestVectorHandler_TransformRequest(t *testing.T) {
	t.Run("routes to correct handler", func(t *testing.T) {
		vh := &vectorHandler{}

		methods := []string{
			"CreateNamespace",
			"DeleteNamespace",
			"ListNamespaces",
			"GetNamespaceInfo",
			"Upsert",
			"Get",
			"Delete",
			"BatchUpsert",
			"BatchDelete",
			"SearchSimilar",
			"SearchByID",
			"HybridSearch",
			"SearchByMetadata",
			"RebuildIndex",
			"GetIndexProgress",
			"Compact",
			"GetCompactionProgress",
		}

		for _, method := range methods {
			t.Run(method, func(t *testing.T) {
				// Should return error for wrong type, not panic
				_, err := vh.transformRequest(method, "invalid")
				assert.Error(t, err)
			})
		}
	})

	t.Run("unknown method falls back to generic", func(t *testing.T) {
		vh := &vectorHandler{transport: &Transport{}}

		req := map[string]interface{}{
			"key": "value",
		}

		args, err := vh.transformRequest("UnknownMethod", req)
		require.NoError(t, err)
		assert.Equal(t, "value", args["key"])
	})
}

func TestVectorHandler_TransformResponse(t *testing.T) {
	t.Run("routes to correct handler", func(t *testing.T) {
		vh := &vectorHandler{}

		methods := []string{
			"CreateNamespace",
			"DeleteNamespace",
			"ListNamespaces",
			"GetNamespaceInfo",
			"Upsert",
			"Get",
			"Delete",
			"BatchUpsert",
			"BatchDelete",
			"SearchSimilar",
			"SearchByID",
			"HybridSearch",
			"SearchByMetadata",
			"RebuildIndex",
			"GetIndexProgress",
			"Compact",
			"GetCompactionProgress",
		}

		for _, method := range methods {
			t.Run(method, func(t *testing.T) {
				// Should handle nil response gracefully
				_, err := vh.transformResponse(method, nil)
				assert.NoError(t, err)
			})
		}
	})

	t.Run("unknown method returns response as-is", func(t *testing.T) {
		vh := &vectorHandler{}

		resp := map[string]interface{}{
			"key": "value",
		}

		result, err := vh.transformResponse("UnknownMethod", resp)
		require.NoError(t, err)
		assert.Equal(t, resp, result)
	})
}
