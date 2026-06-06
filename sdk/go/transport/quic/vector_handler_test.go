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
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsVectorService(t *testing.T) {
	assert.True(t, isVectorService("quicbaas.Vector"))
	assert.True(t, isVectorService("Vector"))
	assert.False(t, isVectorService("quicbaas.KV"))
}

// TestVectorTransformRequest_AllMethods drives every request transform branch,
// asserting the resulting server-side flat map carries the expected fields.
func TestVectorTransformRequest_AllMethods(t *testing.T) {
	h := newVectorHandler(nil)

	t.Run("CreateNamespace", func(t *testing.T) {
		out, err := h.transformRequest("CreateNamespace", &vector.CreateNamespaceRequest{
			Config: &vector.NamespaceConfig{
				Namespace: "ns", Dimensions: 8, IndexType: vector.IndexTypeHNSW,
				IndexParams: map[string]string{"m": "16"}, Metadata: map[string]string{"k": "v"},
			},
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "ns", m["namespace"])
		assert.Equal(t, int32(8), m["dimension"])
		assert.Equal(t, "hnsw", m["index_type"])
	})

	t.Run("CreateNamespace wrong type", func(t *testing.T) {
		_, err := h.transformRequest("CreateNamespace", "bad")
		require.Error(t, err)
	})

	t.Run("CreateNamespace nil config", func(t *testing.T) {
		_, err := h.transformRequest("CreateNamespace", &vector.CreateNamespaceRequest{})
		require.Error(t, err)
	})

	t.Run("DeleteNamespace", func(t *testing.T) {
		out, err := h.transformRequest("DeleteNamespace", &vector.DeleteNamespaceRequest{Namespace: "ns"})
		require.NoError(t, err)
		assert.Equal(t, "ns", out.(map[string]interface{})["namespace"])
	})

	t.Run("ListNamespaces", func(t *testing.T) {
		out, err := h.transformRequest("ListNamespaces", &vector.ListNamespacesRequest{
			Prefix: "p", PageSize: 10, PageToken: "tok",
		})
		require.NoError(t, err)
		assert.Equal(t, "p", out.(map[string]interface{})["prefix"])
	})

	t.Run("GetNamespaceInfo", func(t *testing.T) {
		out, err := h.transformRequest("GetNamespaceInfo", &vector.GetNamespaceInfoRequest{Namespace: "ns"})
		require.NoError(t, err)
		assert.Equal(t, "ns", out.(map[string]interface{})["namespace"])
	})

	t.Run("Upsert", func(t *testing.T) {
		out, err := h.transformRequest("Upsert", &vector.UpsertRequest{
			Vector: &vector.VectorEntity{ID: "v1", Namespace: "ns", Vector: []float32{1, 2}},
		})
		require.NoError(t, err)
		assert.Equal(t, "v1", out.(map[string]interface{})["id"])
	})

	t.Run("Upsert nil vector", func(t *testing.T) {
		_, err := h.transformRequest("Upsert", &vector.UpsertRequest{})
		require.Error(t, err)
	})

	t.Run("Get", func(t *testing.T) {
		out, err := h.transformRequest("Get", &vector.GetRequest{Namespace: "ns", ID: "v1", IncludeVector: true})
		require.NoError(t, err)
		assert.Equal(t, true, out.(map[string]interface{})["include_vector"])
	})

	t.Run("Delete", func(t *testing.T) {
		out, err := h.transformRequest("Delete", &vector.DeleteRequest{Namespace: "ns", ID: "v1"})
		require.NoError(t, err)
		assert.Equal(t, "v1", out.(map[string]interface{})["id"])
	})

	t.Run("BatchUpsert", func(t *testing.T) {
		out, err := h.transformRequest("BatchUpsert", &vector.BatchUpsertRequest{
			Vectors: []*vector.VectorEntity{{ID: "a"}, {ID: "b"}},
		})
		require.NoError(t, err)
		assert.Len(t, out.(map[string]interface{})["vectors"], 2)
	})

	t.Run("BatchDelete", func(t *testing.T) {
		out, err := h.transformRequest("BatchDelete", &vector.BatchDeleteRequest{Namespace: "ns", IDs: []string{"a"}})
		require.NoError(t, err)
		assert.Equal(t, []string{"a"}, out.(map[string]interface{})["ids"])
	})

	t.Run("SearchSimilar", func(t *testing.T) {
		out, err := h.transformRequest("SearchSimilar", &vector.SearchSimilarRequest{
			Namespace: "ns", QueryVector: []float32{1}, K: 5,
		})
		require.NoError(t, err)
		assert.Equal(t, int32(5), out.(map[string]interface{})["k"])
	})

	t.Run("SearchByID", func(t *testing.T) {
		out, err := h.transformRequest("SearchByID", &vector.SearchByIDRequest{Namespace: "ns", ID: "v1", K: 3})
		require.NoError(t, err)
		assert.Equal(t, "v1", out.(map[string]interface{})["id"])
	})

	t.Run("HybridSearch", func(t *testing.T) {
		out, err := h.transformRequest("HybridSearch", &vector.HybridSearchRequest{
			Namespace: "ns", QueryVector: []float32{1}, K: 2,
			MetadataFilters: []*vector.MetadataFilter{{Key: "k", Operator: vector.FilterOperatorEquals, Value: "v"}},
		})
		require.NoError(t, err)
		assert.Len(t, out.(map[string]interface{})["metadata_filters"], 1)
	})

	t.Run("SearchByMetadata", func(t *testing.T) {
		out, err := h.transformRequest("SearchByMetadata", &vector.SearchByMetadataRequest{
			Namespace: "ns", Limit: 10,
			MetadataFilters: []*vector.MetadataFilter{{Key: "k", Operator: vector.FilterOperatorIn, Value: "v"}},
		})
		require.NoError(t, err)
		assert.Equal(t, int32(10), out.(map[string]interface{})["limit"])
	})

	t.Run("RebuildIndex", func(t *testing.T) {
		out, err := h.transformRequest("RebuildIndex", &vector.RebuildIndexRequest{Namespace: "ns", Async: true})
		require.NoError(t, err)
		assert.Equal(t, true, out.(map[string]interface{})["async"])
	})

	t.Run("GetIndexProgress", func(t *testing.T) {
		out, err := h.transformRequest("GetIndexProgress", &vector.GetIndexProgressRequest{Namespace: "ns", OperationID: "op"})
		require.NoError(t, err)
		assert.Equal(t, "op", out.(map[string]interface{})["operation_id"])
	})

	t.Run("Compact", func(t *testing.T) {
		out, err := h.transformRequest("Compact", &vector.CompactRequest{Namespace: "ns", Async: false})
		require.NoError(t, err)
		assert.Equal(t, "ns", out.(map[string]interface{})["namespace"])
	})

	t.Run("GetCompactionProgress", func(t *testing.T) {
		out, err := h.transformRequest("GetCompactionProgress", &vector.GetCompactionProgressRequest{Namespace: "ns", OperationID: "op"})
		require.NoError(t, err)
		assert.Equal(t, "op", out.(map[string]interface{})["operation_id"])
	})

	t.Run("unknown method pass-through", func(t *testing.T) {
		in := map[string]interface{}{"x": 1}
		out, err := h.transformRequest("Bogus", in)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})
}

// TestVectorTransformRequest_WrongType verifies the type-assertion error path
// for the non-config transforms.
func TestVectorTransformRequest_WrongType(t *testing.T) {
	h := newVectorHandler(nil)
	methods := []string{
		"DeleteNamespace", "ListNamespaces", "GetNamespaceInfo", "Upsert", "Get",
		"Delete", "BatchUpsert", "BatchDelete", "SearchSimilar", "SearchByID",
		"HybridSearch", "SearchByMetadata", "RebuildIndex", "GetIndexProgress",
		"Compact", "GetCompactionProgress",
	}
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			_, err := h.transformRequest(m, "wrong-type")
			require.Error(t, err)
		})
	}
}

// TestVectorTransformResponse_AllMethods drives the response transforms with a
// representative server map and asserts the typed SDK value is produced.
func TestVectorTransformResponse_AllMethods(t *testing.T) {
	h := newVectorHandler(nil)
	ts := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)

	t.Run("CreateNamespace", func(t *testing.T) {
		out, err := h.transformResponse("CreateNamespace", map[string]interface{}{"namespace": "ns", "timestamp": ts})
		require.NoError(t, err)
		assert.Equal(t, "ns", out.(*vector.CreateNamespaceResponse).Namespace)
	})

	t.Run("DeleteNamespace", func(t *testing.T) {
		out, err := h.transformResponse("DeleteNamespace", map[string]interface{}{"deleted": true})
		require.NoError(t, err)
		assert.True(t, out.(*vector.DeleteNamespaceResponse).Deleted)
	})

	t.Run("ListNamespaces", func(t *testing.T) {
		out, err := h.transformResponse("ListNamespaces", map[string]interface{}{
			"namespaces": []interface{}{
				map[string]interface{}{"namespace": "n1", "dimension": float64(4)},
			},
			"total_count": float64(1),
		})
		require.NoError(t, err)
		resp := out.(*vector.ListNamespacesResponse)
		require.Len(t, resp.Namespaces, 1)
		assert.Equal(t, "n1", resp.Namespaces[0].Config.Namespace)
	})

	t.Run("ListNamespaces nil resp", func(t *testing.T) {
		out, err := h.transformResponse("ListNamespaces", nil)
		require.NoError(t, err)
		assert.Empty(t, out.(*vector.ListNamespacesResponse).Namespaces)
	})

	t.Run("GetNamespaceInfo", func(t *testing.T) {
		out, err := h.transformResponse("GetNamespaceInfo", map[string]interface{}{"namespace": "ns", "vector_count": float64(7)})
		require.NoError(t, err)
		assert.Equal(t, int64(7), out.(*vector.GetNamespaceInfoResponse).Info.VectorCount)
	})

	t.Run("Upsert", func(t *testing.T) {
		out, err := h.transformResponse("Upsert", map[string]interface{}{"id": "v1", "version": float64(2)})
		require.NoError(t, err)
		assert.Equal(t, "v1", out.(*vector.UpsertResponse).ID)
	})

	t.Run("Get", func(t *testing.T) {
		out, err := h.transformResponse("Get", map[string]interface{}{
			"id": "v1", "vector": []interface{}{float64(1), float64(2)},
		})
		require.NoError(t, err)
		resp := out.(*vector.GetResponse)
		assert.True(t, resp.Found)
		assert.Equal(t, []float32{1, 2}, resp.Vector.Vector)
	})

	t.Run("Get nil resp", func(t *testing.T) {
		out, err := h.transformResponse("Get", nil)
		require.NoError(t, err)
		assert.False(t, out.(*vector.GetResponse).Found)
	})

	t.Run("Delete", func(t *testing.T) {
		out, err := h.transformResponse("Delete", map[string]interface{}{"deleted": true})
		require.NoError(t, err)
		assert.True(t, out.(*vector.DeleteResponse).Deleted)
	})

	t.Run("BatchUpsert", func(t *testing.T) {
		out, err := h.transformResponse("BatchUpsert", map[string]interface{}{"vectors_succeeded": float64(3)})
		require.NoError(t, err)
		assert.Equal(t, int64(3), out.(*vector.BatchUpsertResponse).VectorsSucceeded)
	})

	t.Run("BatchDelete", func(t *testing.T) {
		out, err := h.transformResponse("BatchDelete", map[string]interface{}{"deleted_count": float64(2)})
		require.NoError(t, err)
		assert.Equal(t, int64(2), out.(*vector.BatchDeleteResponse).DeletedCount)
	})

	t.Run("SearchSimilar", func(t *testing.T) {
		out, err := h.transformResponse("SearchSimilar", map[string]interface{}{
			"results": []interface{}{
				map[string]interface{}{"score": float64(0.9), "rank": float64(1),
					"vector": map[string]interface{}{"id": "v1"}},
			},
		})
		require.NoError(t, err)
		resp := out.(*vector.SearchSimilarResponse)
		require.Len(t, resp.Results, 1)
		assert.Equal(t, float32(0.9), resp.Results[0].Score)
		assert.Equal(t, "v1", resp.Results[0].Vector.ID)
	})

	t.Run("SearchSimilar nil resp", func(t *testing.T) {
		out, err := h.transformResponse("SearchSimilar", nil)
		require.NoError(t, err)
		assert.Empty(t, out.(*vector.SearchSimilarResponse).Results)
	})

	t.Run("SearchByID", func(t *testing.T) {
		out, err := h.transformResponse("SearchByID", map[string]interface{}{"results": []interface{}{}})
		require.NoError(t, err)
		assert.NotNil(t, out.(*vector.SearchByIDResponse))
	})

	t.Run("SearchByID nil resp", func(t *testing.T) {
		out, err := h.transformResponse("SearchByID", nil)
		require.NoError(t, err)
		assert.Empty(t, out.(*vector.SearchByIDResponse).Results)
	})

	t.Run("HybridSearch", func(t *testing.T) {
		out, err := h.transformResponse("HybridSearch", map[string]interface{}{"reranking_applied": true})
		require.NoError(t, err)
		assert.True(t, out.(*vector.HybridSearchResponse).RerankingApplied)
	})

	t.Run("HybridSearch nil resp", func(t *testing.T) {
		out, err := h.transformResponse("HybridSearch", nil)
		require.NoError(t, err)
		assert.Empty(t, out.(*vector.HybridSearchResponse).Results)
	})

	t.Run("SearchByMetadata", func(t *testing.T) {
		out, err := h.transformResponse("SearchByMetadata", map[string]interface{}{
			"results":     []interface{}{map[string]interface{}{"id": "v1"}},
			"total_count": float64(1),
		})
		require.NoError(t, err)
		resp := out.(*vector.SearchByMetadataResponse)
		require.Len(t, resp.Results, 1)
		assert.Equal(t, "v1", resp.Results[0].ID)
	})

	t.Run("SearchByMetadata nil resp", func(t *testing.T) {
		out, err := h.transformResponse("SearchByMetadata", nil)
		require.NoError(t, err)
		assert.Empty(t, out.(*vector.SearchByMetadataResponse).Results)
	})

	t.Run("RebuildIndex", func(t *testing.T) {
		out, err := h.transformResponse("RebuildIndex", map[string]interface{}{"operation_id": "op", "status": "running"})
		require.NoError(t, err)
		assert.Equal(t, "op", out.(*vector.RebuildIndexResponse).OperationID)
	})

	t.Run("GetIndexProgress", func(t *testing.T) {
		out, err := h.transformResponse("GetIndexProgress", map[string]interface{}{
			"operation_id": "op", "percent_complete": float64(50),
		})
		require.NoError(t, err)
		assert.Equal(t, float32(50), out.(*vector.GetIndexProgressResponse).PercentComplete)
	})

	t.Run("Compact", func(t *testing.T) {
		out, err := h.transformResponse("Compact", map[string]interface{}{"operation_id": "op"})
		require.NoError(t, err)
		assert.Equal(t, "op", out.(*vector.CompactResponse).OperationID)
	})

	t.Run("GetCompactionProgress", func(t *testing.T) {
		out, err := h.transformResponse("GetCompactionProgress", map[string]interface{}{
			"operation_id": "op", "bytes_compacted": float64(1024),
		})
		require.NoError(t, err)
		assert.Equal(t, int64(1024), out.(*vector.GetCompactionProgressResponse).BytesCompacted)
	})

	t.Run("unknown method pass-through", func(t *testing.T) {
		in := map[string]interface{}{"x": 1}
		out, err := h.transformResponse("Bogus", in)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})
}

// TestVectorTransformResponse_InvalidType covers the non-map response error
// branch on a representative set of response transforms.
func TestVectorTransformResponse_InvalidType(t *testing.T) {
	h := newVectorHandler(nil)
	methods := []string{
		"CreateNamespace", "DeleteNamespace", "ListNamespaces", "GetNamespaceInfo",
		"Upsert", "Get", "Delete", "BatchUpsert", "BatchDelete", "SearchSimilar",
		"SearchByID", "HybridSearch", "SearchByMetadata", "RebuildIndex",
		"GetIndexProgress", "Compact", "GetCompactionProgress",
	}
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			_, err := h.transformResponse(m, "not-a-map")
			require.Error(t, err)
		})
	}
}

// TestVectorHelpers exercises the small field-accessor helpers including their
// type-mismatch fall-throughs.
func TestVectorHelpers(t *testing.T) {
	m := map[string]interface{}{
		"s": "str", "b": true, "i": float64(42), "f": float64(1.5),
		"t":   "2021-01-02T03:04:05Z",
		"tn":  "2021-01-02T03:04:05.123456789Z",
		"bad": 123,
	}

	assert.Equal(t, "str", vectorGetString(m, "s"))
	assert.Equal(t, "", vectorGetString(m, "bad"))
	assert.True(t, vectorGetBoolField(m, "b"))
	assert.False(t, vectorGetBoolField(m, "bad"))
	assert.Equal(t, int64(42), vectorGetInt64Field(m, "i"))
	assert.Equal(t, int64(0), vectorGetInt64Field(m, "s"))
	assert.Equal(t, float32(1.5), vectorGetFloat32Field(m, "f"))
	assert.Equal(t, float32(0), vectorGetFloat32Field(m, "s"))

	assert.False(t, vectorGetTimeField(m, "t").IsZero())
	assert.False(t, vectorGetTimeField(m, "tn").IsZero())
	assert.True(t, vectorGetTimeField(m, "bad").IsZero())
	assert.True(t, vectorGetTimeField(m, "s").IsZero()) // unparseable string

	assert.Nil(t, stringMapToInterfaceMap(nil))
	assert.Equal(t, map[string]interface{}{"a": "b"}, stringMapToInterfaceMap(map[string]string{"a": "b"}))

	assert.Equal(t, map[string]string{"a": "b"}, interfaceMapToStringMap(map[string]interface{}{"a": "b", "n": 1}))
	assert.Equal(t, map[string]string{}, interfaceMapToStringMap("not-a-map"))
}

// TestVectorTransformResponse_NilResp covers the `if resp == nil` early-return
// branch for the response transforms that return a nil (not an empty struct).
func TestVectorTransformResponse_NilResp(t *testing.T) {
	h := newVectorHandler(nil)
	nilReturning := []string{
		"CreateNamespace", "DeleteNamespace", "GetNamespaceInfo", "Upsert",
		"Delete", "BatchUpsert", "BatchDelete", "RebuildIndex", "GetIndexProgress",
		"Compact", "GetCompactionProgress",
	}
	for _, m := range nilReturning {
		t.Run(m, func(t *testing.T) {
			out, err := h.transformResponse(m, nil)
			require.NoError(t, err)
			assert.Nil(t, out)
		})
	}
}
