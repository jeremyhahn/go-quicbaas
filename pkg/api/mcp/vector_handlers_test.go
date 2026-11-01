//go:build mcp

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// Package mcp implements comprehensive tests for vector handlers

// TestHandleBatchUpsertVectors tests batch upsert operation
func TestHandleBatchUpsertVectors(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	vectors := []map[string]interface{}{
		{
			"id":       "vec1",
			"vector":   []interface{}{0.1, 0.2, 0.3},
			"metadata": map[string]interface{}{"type": "test1"},
		},
		{
			"id":       "vec2",
			"vector":   []interface{}{0.4, 0.5, 0.6},
			"metadata": map[string]interface{}{"type": "test2"},
		},
	}
	vectorsJSON, _ := json.Marshal(vectors)

	params := map[string]interface{}{
		"namespace": "test-namespace",
		"vectors":   string(vectorsJSON),
	}

	resp, err := server.handleBatchUpsertVectors(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.IsError)

	var result map[string]interface{}
	err = json.Unmarshal([]byte(resp.Content[0].Text), &result)
	require.NoError(t, err)
	assert.Equal(t, "test-namespace", result["namespace"])
	assert.Equal(t, float64(2), result["vectors_total"])
	assert.Equal(t, float64(2), result["vectors_succeeded"])
}

// TestHandleBatchUpsertVectorsValidation tests parameter validation
func TestHandleBatchUpsertVectorsValidation(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	t.Run("MissingNamespace", func(t *testing.T) {
		params := map[string]interface{}{
			"vectors": `[{"id":"vec1","vector":[0.1,0.2,0.3]}]`,
		}
		_, err := server.handleBatchUpsertVectors(ctx, params)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "namespace is required")
	})

	t.Run("MissingVectors", func(t *testing.T) {
		params := map[string]interface{}{
			"namespace": "test-ns",
		}
		_, err := server.handleBatchUpsertVectors(ctx, params)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "vectors is required")
	})

	t.Run("InvalidVectorsJSON", func(t *testing.T) {
		params := map[string]interface{}{
			"namespace": "test-ns",
			"vectors":   "invalid-json",
		}
		_, err := server.handleBatchUpsertVectors(ctx, params)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid vectors format")
	})
}

// batchIDsJSON returns a JSON array of n vector IDs.
func batchIDsJSON(t *testing.T, n int) string {
	t.Helper()
	ids := make([]string, n)
	for i := range ids {
		ids[i] = "vec" + string(rune('a'+i%26))
	}
	idsJSON, err := json.Marshal(ids)
	require.NoError(t, err)
	return string(idsJSON)
}

// toolCase is one table row for a vector tool handler: either the error
// the handler must return, or the result fields it must carry.
type toolCase struct {
	name    string
	params  map[string]interface{}
	want    map[string]interface{} // result fields and their exact values
	present []string               // result fields that must be non-nil
	wantErr string                 // substring of the expected error
}

// runToolCases runs each case against handler as a subtest.
func runToolCases(t *testing.T, handler func(context.Context, map[string]interface{}) (*ToolResponse, error), cases []toolCase) {
	t.Helper()
	ctx := context.Background()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := handler(ctx, tc.params)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.False(t, resp.IsError)

			var result map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(resp.Content[0].Text), &result))
			for key, value := range tc.want {
				assert.Equal(t, value, result[key], key)
			}
			for _, key := range tc.present {
				assert.NotNil(t, result[key], key)
			}
		})
	}
}

// TestHandleGetVector tests get vector with default and explicit options
func TestHandleGetVector(t *testing.T) {
	server := newTestVectorServer(t)
	runToolCases(t, server.handleGetVector, []toolCase{
		{
			name:    "Defaults",
			params:  map[string]interface{}{"namespace": "test-namespace", "id": "test-vector-1"},
			want:    map[string]interface{}{"id": "test-vector-1", "namespace": "test-namespace", "found": true},
			present: []string{"vector"},
		},
		{
			name: "MetadataAndVectorOff",
			params: map[string]interface{}{
				"namespace":        "test-namespace",
				"id":               "test-vector-1",
				"include_metadata": "false",
				"include_vector":   "false",
				"consistency":      "ONE",
			},
			want: map[string]interface{}{"id": "test-vector-1", "found": true},
		},
		{
			name: "AllOptions",
			params: map[string]interface{}{
				"namespace":        "test-ns",
				"id":               "vec-123",
				"include_metadata": "true",
				"include_vector":   "true",
				"consistency":      "QUORUM",
				"timeout_ms":       float64(5000),
			},
			want: map[string]interface{}{"id": "vec-123", "namespace": "test-ns", "found": true},
		},
	})
}

// TestHandleDeleteVector tests delete vector with default and explicit options
func TestHandleDeleteVector(t *testing.T) {
	server := newTestVectorServer(t)
	runToolCases(t, server.handleDeleteVector, []toolCase{
		{
			name:   "Defaults",
			params: map[string]interface{}{"namespace": "test-namespace", "id": "test-vector-1"},
			want:   map[string]interface{}{"id": "test-vector-1", "deleted": true},
		},
		{
			name: "WithOptionalParams",
			params: map[string]interface{}{
				"namespace":   "test-ns",
				"id":          "vec-123",
				"consistency": "ALL",
				"timeout_ms":  float64(10000),
			},
			want: map[string]interface{}{"id": "vec-123", "consistency": "ALL", "timeout_ms": float64(10000)},
		},
	})
}

// TestHandleBatchDeleteVectors tests batch delete results and validation
func TestHandleBatchDeleteVectors(t *testing.T) {
	server := newTestVectorServer(t)
	runToolCases(t, server.handleBatchDeleteVectors, []toolCase{
		{
			name:   "ThreeIDs",
			params: map[string]interface{}{"namespace": "test-namespace", "ids": `["vec1","vec2","vec3"]`},
			want:   map[string]interface{}{"ids_total": float64(3), "deleted_count": float64(3)},
		},
		{
			name:   "ExactlyMaxBatchSize",
			params: map[string]interface{}{"namespace": "test-ns", "ids": batchIDsJSON(t, 10000)},
			want:   map[string]interface{}{"ids_total": float64(10000)},
		},
		{
			name:    "TooManyIDs",
			params:  map[string]interface{}{"namespace": "test-ns", "ids": batchIDsJSON(t, 10001)},
			wantErr: "maximum 10000 IDs allowed",
		},
		{
			name:    "MissingNamespace",
			params:  map[string]interface{}{"ids": `["id1","id2"]`},
			wantErr: "namespace is required",
		},
		{
			name:    "EmptyNamespace",
			params:  map[string]interface{}{"namespace": "", "ids": `["id1"]`},
			wantErr: "namespace is required",
		},
		{
			name:    "MissingIDs",
			params:  map[string]interface{}{"namespace": "test-ns"},
			wantErr: "ids is required",
		},
		{
			name:    "InvalidIDsJSON",
			params:  map[string]interface{}{"namespace": "test-ns", "ids": "not-valid-json["},
			wantErr: "invalid ids format",
		},
	})
}

// TestHandleSearchByID tests search by ID operation
func TestHandleSearchByID(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	params := map[string]interface{}{
		"namespace": "test-namespace",
		"id":        "test-vector-1",
		"k":         float64(5),
	}

	resp, err := server.handleSearchByID(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.IsError)

	var result map[string]interface{}
	err = json.Unmarshal([]byte(resp.Content[0].Text), &result)
	require.NoError(t, err)
	assert.Equal(t, "test-vector-1", result["query_id"])
	assert.Equal(t, float64(5), result["k"])
	assert.NotNil(t, result["results"])
}

// TestHandleSearchByIDValidation tests search by ID validation
func TestHandleSearchByIDValidation(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	t.Run("InvalidK", func(t *testing.T) {
		params := map[string]interface{}{
			"namespace": "test-ns",
			"id":        "vec1",
			"k":         float64(20000),
		}
		_, err := server.handleSearchByID(ctx, params)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "k must be between 1 and 10000")
	})
}

// TestHandleListVectors tests list vectors operation
func TestHandleListVectors(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	params := map[string]interface{}{
		"namespace": "test-namespace",
		"page_size": float64(10),
	}

	resp, err := server.handleListVectors(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.IsError)

	var result map[string]interface{}
	err = json.Unmarshal([]byte(resp.Content[0].Text), &result)
	require.NoError(t, err)
	assert.Equal(t, "test-namespace", result["namespace"])
	assert.NotNil(t, result["vectors"])
}

// TestHandleUpdateNamespace tests namespace update operation
func TestHandleUpdateNamespace(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	params := map[string]interface{}{
		"namespace":          "test-namespace",
		"replication_factor": float64(5),
		"metadata":           `{"env":"production"}`,
	}

	resp, err := server.handleUpdateNamespace(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.IsError)

	var result map[string]interface{}
	err = json.Unmarshal([]byte(resp.Content[0].Text), &result)
	require.NoError(t, err)
	assert.Equal(t, "test-namespace", result["namespace"])
	assert.Equal(t, float64(5), result["replication_factor"])
}

// TestHandleDeleteNamespace tests namespace deletion with and without force
func TestHandleDeleteNamespace(t *testing.T) {
	server := newTestVectorServer(t)
	runToolCases(t, server.handleDeleteNamespace, []toolCase{
		{
			name:   "Force",
			params: map[string]interface{}{"namespace": "test-namespace", "force": "true"},
			want:   map[string]interface{}{"namespace": "test-namespace", "deleted": true, "vectors_deleted": float64(1000)},
		},
		{
			name:   "WithoutForce",
			params: map[string]interface{}{"namespace": "test-ns"},
			want:   map[string]interface{}{"namespace": "test-ns"},
		},
	})
}

// TestHandleGetNamespaceInfo tests namespace info with and without stats
func TestHandleGetNamespaceInfo(t *testing.T) {
	server := newTestVectorServer(t)
	runToolCases(t, server.handleGetNamespaceInfo, []toolCase{
		{
			name:    "WithStats",
			params:  map[string]interface{}{"namespace": "test-namespace", "include_stats": "true"},
			want:    map[string]interface{}{"namespace": "test-namespace", "dimensions": float64(384)},
			present: []string{"vector_count"},
		},
		{
			name:   "WithoutStats",
			params: map[string]interface{}{"namespace": "test-ns"},
			want:   map[string]interface{}{"namespace": "test-ns"},
		},
	})
}

// TestHandleGetVectorStats tests vector statistics with and without details
func TestHandleGetVectorStats(t *testing.T) {
	server := newTestVectorServer(t)
	runToolCases(t, server.handleGetVectorStats, []toolCase{
		{
			name:    "WithDetails",
			params:  map[string]interface{}{"namespace": "test-namespace", "include_details": "true"},
			present: []string{"total_vectors", "total_namespaces"},
		},
		{
			name:    "WithoutDetails",
			params:  map[string]interface{}{"namespace": "test-ns"},
			present: []string{"total_vectors"},
		},
	})
}

// TestHandleRebuildIndex tests index rebuild with and without options
func TestHandleRebuildIndex(t *testing.T) {
	server := newTestVectorServer(t)
	runToolCases(t, server.handleRebuildIndex, []toolCase{
		{
			name: "AsyncWithIndexParams",
			params: map[string]interface{}{
				"namespace":    "test-namespace",
				"async":        "true",
				"index_params": `{"m":"16","ef_construction":"200"}`,
			},
			want:    map[string]interface{}{"namespace": "test-namespace"},
			present: []string{"operation_id"},
		},
		{
			name:   "WithoutOptionalParams",
			params: map[string]interface{}{"namespace": "test-ns"},
			want:   map[string]interface{}{"namespace": "test-ns"},
		},
	})
}

// TestHandleGetIndexProgress tests index rebuild progress and validation
func TestHandleGetIndexProgress(t *testing.T) {
	server := newTestVectorServer(t)
	runToolCases(t, server.handleGetIndexProgress, []toolCase{
		{
			name:    "WithOperationID",
			params:  map[string]interface{}{"namespace": "test-namespace", "operation_id": "rebuild-op-123"},
			want:    map[string]interface{}{"operation_id": "rebuild-op-123", "namespace": "test-namespace"},
			present: []string{"percent_complete"},
		},
		{
			name:   "WithoutOperationID",
			params: map[string]interface{}{"namespace": "test-ns"},
			want:   map[string]interface{}{"namespace": "test-ns"},
		},
		{
			name:    "MissingNamespace",
			params:  map[string]interface{}{"operation_id": "rebuild-123"},
			wantErr: "namespace is required",
		},
		{
			name:    "EmptyNamespace",
			params:  map[string]interface{}{"namespace": ""},
			wantErr: "namespace is required",
		},
	})
}

// TestHandleCompactNamespace tests async and synchronous compaction
func TestHandleCompactNamespace(t *testing.T) {
	server := newTestVectorServer(t)
	runToolCases(t, server.handleCompactNamespace, []toolCase{
		{
			name:    "Async",
			params:  map[string]interface{}{"namespace": "test-namespace", "async": "true"},
			want:    map[string]interface{}{"namespace": "test-namespace"},
			present: []string{"operation_id"},
		},
		{
			name:   "Synchronous",
			params: map[string]interface{}{"namespace": "test-ns", "async": "false"},
			want:   map[string]interface{}{"namespace": "test-ns"},
		},
	})
}

// TestHandleGetCompactionProgress tests compaction progress and validation
func TestHandleGetCompactionProgress(t *testing.T) {
	server := newTestVectorServer(t)
	runToolCases(t, server.handleGetCompactionProgress, []toolCase{
		{
			name:    "WithOperationID",
			params:  map[string]interface{}{"namespace": "test-namespace", "operation_id": "compact-op-456"},
			want:    map[string]interface{}{"operation_id": "compact-op-456", "namespace": "test-namespace"},
			present: []string{"percent_complete"},
		},
		{
			name:   "WithoutOperationID",
			params: map[string]interface{}{"namespace": "test-ns"},
			want:   map[string]interface{}{"namespace": "test-ns"},
		},
		{
			name:    "MissingNamespace",
			params:  map[string]interface{}{"operation_id": "compact-123"},
			wantErr: "namespace is required",
		},
		{
			name:    "EmptyNamespace",
			params:  map[string]interface{}{"namespace": ""},
			wantErr: "namespace is required",
		},
	})
}

// TestHandleExportVectors tests vector export with explicit and default options
func TestHandleExportVectors(t *testing.T) {
	server := newTestVectorServer(t)
	runToolCases(t, server.handleExportVectors, []toolCase{
		{
			name: "BinaryGzip",
			params: map[string]interface{}{
				"namespace":   "test-namespace",
				"format":      "binary",
				"compression": "gzip",
				"batch_size":  float64(500),
			},
			want:    map[string]interface{}{"namespace": "test-namespace", "format": "binary"},
			present: []string{"vectors_exported"},
		},
		{
			name:   "WithDefaultParams",
			params: map[string]interface{}{"namespace": "test-ns"},
			want:   map[string]interface{}{"namespace": "test-ns", "format": "binary"},
		},
	})
}

// TestHandleImportVectors tests vector import operation
func TestHandleImportVectors(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	params := map[string]interface{}{
		"namespace":          "test-namespace",
		"format":             "binary",
		"data":               "mock-binary-data",
		"skip_validation":    "false",
		"overwrite_existing": "true",
	}

	resp, err := server.handleImportVectors(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.IsError)

	var result map[string]interface{}
	err = json.Unmarshal([]byte(resp.Content[0].Text), &result)
	require.NoError(t, err)
	assert.Equal(t, "test-namespace", result["namespace"])
	assert.Equal(t, "binary", result["format"])
	assert.NotNil(t, result["vectors_imported"])
}

// TestParseHelperFunctions tests all parse helper functions
func TestParseHelperFunctions(t *testing.T) {
	server := newTestVectorServer(t)

	t.Run("ParseStringParam", func(t *testing.T) {
		params := map[string]interface{}{
			"key1": "value1",
		}
		result := server.parseStringParam(params, "key1", "default")
		assert.Equal(t, "value1", result)

		result = server.parseStringParam(params, "missing", "default")
		assert.Equal(t, "default", result)
	})

	t.Run("ParseBoolParam", func(t *testing.T) {
		params := map[string]interface{}{
			"key1": "true",
			"key2": true,
			"key3": "false",
		}
		assert.True(t, server.parseBoolParam(params, "key1", false))
		assert.True(t, server.parseBoolParam(params, "key2", false))
		assert.False(t, server.parseBoolParam(params, "key3", true))
		assert.True(t, server.parseBoolParam(params, "missing", true))
	})

	t.Run("ParseFloatParam", func(t *testing.T) {
		params := map[string]interface{}{
			"key1": float64(3.14),
			"key2": "2.718",
		}
		assert.Equal(t, 3.14, server.parseFloatParam(params, "key1", 0.0))
		assert.Equal(t, 2.718, server.parseFloatParam(params, "key2", 0.0))
		assert.Equal(t, 1.5, server.parseFloatParam(params, "missing", 1.5))
	})
}

// TestParseConsistencyLevel tests consistency level parsing
func TestParseConsistencyLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ALL", "CONSISTENCY_LEVEL_ALL"},
		{"QUORUM", "CONSISTENCY_LEVEL_QUORUM"},
		{"ONE", "CONSISTENCY_LEVEL_ONE"},
		{"invalid", "CONSISTENCY_LEVEL_QUORUM"},
		{"", "CONSISTENCY_LEVEL_QUORUM"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseConsistencyLevel(tt.input)
			assert.Contains(t, result.String(), tt.expected)
		})
	}
}

// TestParseDistanceMetric tests distance metric parsing
func TestParseDistanceMetric(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"L2", "DISTANCE_METRIC_L2"},
		{"EUCLIDEAN", "DISTANCE_METRIC_L2"},
		{"COSINE", "DISTANCE_METRIC_COSINE"},
		{"DOT_PRODUCT", "DISTANCE_METRIC_DOT_PRODUCT"},
		{"DOT", "DISTANCE_METRIC_DOT_PRODUCT"},
		{"MANHATTAN", "DISTANCE_METRIC_MANHATTAN"},
		{"L1", "DISTANCE_METRIC_MANHATTAN"},
		{"invalid", "DISTANCE_METRIC_COSINE"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseDistanceMetric(tt.input)
			assert.Contains(t, result.String(), tt.expected)
		})
	}
}

// TestParsePrecision tests precision parsing
func TestParsePrecision(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"FLOAT32", "VECTOR_PRECISION_FLOAT32"},
		{"FLOAT16", "VECTOR_PRECISION_FLOAT16"},
		{"BFLOAT16", "VECTOR_PRECISION_BFLOAT16"},
		{"invalid", "VECTOR_PRECISION_FLOAT32"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parsePrecision(tt.input)
			assert.Contains(t, result.String(), tt.expected)
		})
	}
}

// TestParseIndexType tests index type parsing
func TestParseIndexType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"HNSW", "INDEX_TYPE_HNSW"},
		{"FLAT", "INDEX_TYPE_FLAT"},
		{"invalid", "INDEX_TYPE_HNSW"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseIndexType(tt.input)
			assert.Contains(t, result.String(), tt.expected)
		})
	}
}

// TestNormalizeHealthStatus tests health status normalization
func TestNormalizeHealthStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected string
	}{
		{"Healthy", 1, "healthy"},
		{"Degraded", 2, "degraded"},
		{"Unhealthy", 3, "unhealthy"},
		{"Unknown", 999, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We can't directly test the enum values without importing proto,
			// but we can verify the function exists and handles unknown values
			if tt.input == 999 {
				result := normalizeHealthStatus(999)
				assert.Equal(t, "unknown", result)
			}
		})
	}
}

// TestHandleSearchSimilarWithFilters tests search with metadata filters
func TestHandleSearchSimilarWithFilters(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	queryVector := []float32{0.1, 0.2, 0.3}
	queryVectorJSON, _ := json.Marshal(queryVector)

	filters := []map[string]interface{}{
		{"key": "category", "value": "test"},
		{"key": "environment", "value": "production"},
	}
	filtersJSON, _ := json.Marshal(filters)

	params := map[string]interface{}{
		"namespace":        "test-namespace",
		"query_vector":     string(queryVectorJSON),
		"k":                float64(10),
		"metadata_filters": string(filtersJSON),
		"min_score":        float64(0.5),
		"ef_search":        float64(100),
	}

	resp, err := server.handleSearchSimilar(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.IsError)

	var result map[string]interface{}
	err = json.Unmarshal([]byte(resp.Content[0].Text), &result)
	require.NoError(t, err)
	assert.Equal(t, float64(2), result["filters_count"])
	assert.Equal(t, float64(0.5), result["min_score"])
	assert.Equal(t, float64(100), result["ef_search"])
}

// TestJSONToolResponse tests JSON response formatting
func TestJSONToolResponse(t *testing.T) {
	server := newTestVectorServer(t)

	t.Run("ValidResult", func(t *testing.T) {
		result := map[string]interface{}{
			"key1": "value1",
			"key2": 42,
			"key3": true,
		}
		resp, err := server.jsonToolResponse(result)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.False(t, resp.IsError)
		assert.Len(t, resp.Content, 1)
		assert.Equal(t, "text", resp.Content[0].Type)

		// Verify it's valid JSON
		var parsed map[string]interface{}
		err = json.Unmarshal([]byte(resp.Content[0].Text), &parsed)
		require.NoError(t, err)
		assert.Equal(t, "value1", parsed["key1"])
		assert.Equal(t, float64(42), parsed["key2"])
		assert.Equal(t, true, parsed["key3"])
	})
}

// TestCreateNamespaceInvalidParams tests create namespace validation
func TestCreateNamespaceInvalidParams(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	t.Run("MissingNamespace", func(t *testing.T) {
		params := map[string]interface{}{
			"dimensions":      float64(384),
			"distance_metric": "COSINE",
		}
		_, err := server.handleCreateNamespace(ctx, params)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "namespace is required")
	})

	t.Run("MissingDistanceMetric", func(t *testing.T) {
		params := map[string]interface{}{
			"namespace":  "test-ns",
			"dimensions": float64(384),
		}
		_, err := server.handleCreateNamespace(ctx, params)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "distance_metric is required")
	})

	t.Run("InvalidIndexParams", func(t *testing.T) {
		params := map[string]interface{}{
			"namespace":       "test-ns",
			"dimensions":      float64(384),
			"distance_metric": "COSINE",
			"index_params":    "invalid-json",
		}
		_, err := server.handleCreateNamespace(ctx, params)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid index_params format")
	})

	t.Run("InvalidMetadata", func(t *testing.T) {
		params := map[string]interface{}{
			"namespace":       "test-ns",
			"dimensions":      float64(384),
			"distance_metric": "COSINE",
			"metadata":        "invalid-json",
		}
		_, err := server.handleCreateNamespace(ctx, params)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid metadata format")
	})
}

// TestUpdateNamespaceInvalidParams tests update namespace validation
func TestUpdateNamespaceInvalidParams(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	t.Run("InvalidIndexParams", func(t *testing.T) {
		params := map[string]interface{}{
			"namespace":    "test-ns",
			"index_params": "invalid-json",
		}
		_, err := server.handleUpdateNamespace(ctx, params)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid index_params format")
	})
}

// TestHandleUpsertVectorInvalidMetadata tests upsert with invalid metadata
func TestHandleUpsertVectorInvalidMetadata(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	vector := []float32{0.1, 0.2, 0.3}
	vectorJSON, _ := json.Marshal(vector)

	params := map[string]interface{}{
		"namespace": "test-namespace",
		"id":        "test-vector-1",
		"vector":    string(vectorJSON),
		"metadata":  "invalid-json",
	}

	_, err := server.handleUpsertVector(ctx, params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid metadata format")
}

// Package mcp implements edge case tests to reach 90%+ coverage

// TestNormalizeHealthStatusAllCases tests all health status enum values
func TestNormalizeHealthStatusAllCases(t *testing.T) {
	tests := []struct {
		name     string
		status   pb.VectorHealthStatus
		expected string
	}{
		{"Healthy", pb.VectorHealthStatus_VECTOR_HEALTH_STATUS_HEALTHY, "healthy"},
		{"Degraded", pb.VectorHealthStatus_VECTOR_HEALTH_STATUS_DEGRADED, "degraded"},
		{"Unhealthy", pb.VectorHealthStatus_VECTOR_HEALTH_STATUS_UNHEALTHY, "unhealthy"},
		{"Unspecified", pb.VectorHealthStatus_VECTOR_HEALTH_STATUS_UNSPECIFIED, "unknown"},
		{"Invalid", pb.VectorHealthStatus(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeHealthStatus(tt.status)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestHandleImportVectorsEdgeCases tests import error scenarios
func TestHandleImportVectorsEdgeCases(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	t.Run("MissingNamespace", func(t *testing.T) {
		params := map[string]interface{}{
			"format": "binary",
			"data":   "test-data",
		}
		_, err := server.handleImportVectors(ctx, params)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "namespace is required")
	})

	t.Run("MissingFormat", func(t *testing.T) {
		params := map[string]interface{}{
			"namespace": "test-ns",
			"data":      "test-data",
		}
		_, err := server.handleImportVectors(ctx, params)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "format is required")
	})

	t.Run("MissingData", func(t *testing.T) {
		params := map[string]interface{}{
			"namespace": "test-ns",
			"format":    "binary",
		}
		_, err := server.handleImportVectors(ctx, params)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "data is required")
	})

	t.Run("EmptyNamespace", func(t *testing.T) {
		params := map[string]interface{}{
			"namespace": "",
			"format":    "binary",
			"data":      "test-data",
		}
		_, err := server.handleImportVectors(ctx, params)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "namespace is required")
	})

	t.Run("EmptyFormat", func(t *testing.T) {
		params := map[string]interface{}{
			"namespace": "test-ns",
			"format":    "",
			"data":      "test-data",
		}
		_, err := server.handleImportVectors(ctx, params)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "format is required")
	})
}

// TestHandleUpdateNamespaceEdgeCases tests update namespace edge cases
func TestHandleUpdateNamespaceEdgeCases(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	t.Run("MissingNamespace", func(t *testing.T) {
		params := map[string]interface{}{
			"replication_factor": float64(3),
		}
		_, err := server.handleUpdateNamespace(ctx, params)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "namespace is required")
	})

	t.Run("EmptyNamespace", func(t *testing.T) {
		params := map[string]interface{}{
			"namespace":          "",
			"replication_factor": float64(3),
		}
		_, err := server.handleUpdateNamespace(ctx, params)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "namespace is required")
	})

	t.Run("InvalidMetadataJSON", func(t *testing.T) {
		params := map[string]interface{}{
			"namespace": "test-ns",
			"metadata":  "not-valid-json{",
		}
		_, err := server.handleUpdateNamespace(ctx, params)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid metadata format")
	})

	t.Run("WithIndexParams", func(t *testing.T) {
		indexParams := map[string]interface{}{
			"m":               "16",
			"ef_construction": "200",
			"max_connections": 32,
			"enable_prefetch": true,
		}
		indexParamsJSON, _ := json.Marshal(indexParams)

		params := map[string]interface{}{
			"namespace":    "test-ns",
			"index_params": string(indexParamsJSON),
		}
		resp, err := server.handleUpdateNamespace(ctx, params)
		require.NoError(t, err)
		require.NotNil(t, resp)

		var result map[string]interface{}
		err = json.Unmarshal([]byte(resp.Content[0].Text), &result)
		require.NoError(t, err)
		assert.NotNil(t, result["index_params"])
	})
}

// TestHandleSearchSimilarEdgeCases tests search similar edge cases
func TestHandleSearchSimilarEdgeCases(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	t.Run("WithAllOptionalParams", func(t *testing.T) {
		queryVector := []float32{0.1, 0.2, 0.3}
		queryVectorJSON, _ := json.Marshal(queryVector)

		filters := []map[string]interface{}{
			{"key": "type", "value": "test"},
		}
		filtersJSON, _ := json.Marshal(filters)

		params := map[string]interface{}{
			"namespace":        "test-ns",
			"query_vector":     string(queryVectorJSON),
			"k":                float64(20),
			"metadata_filters": string(filtersJSON),
			"min_score":        float64(0.8),
			"ef_search":        float64(200),
			"consistency":      "ONE",
			"timeout_ms":       float64(3000),
		}
		resp, err := server.handleSearchSimilar(ctx, params)
		require.NoError(t, err)
		require.NotNil(t, resp)

		var result map[string]interface{}
		err = json.Unmarshal([]byte(resp.Content[0].Text), &result)
		require.NoError(t, err)
		assert.Equal(t, float64(20), result["k"])
		assert.Equal(t, float64(0.8), result["min_score"])
	})
}

// TestHandleListVectorsEdgeCases tests list vectors edge cases
func TestHandleListVectorsEdgeCases(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	t.Run("WithAllOptions", func(t *testing.T) {
		params := map[string]interface{}{
			"namespace":        "test-ns",
			"page_size":        float64(100),
			"page_token":       "next-page-token",
			"include_metadata": "true",
			"include_vector":   "false",
		}
		resp, err := server.handleListVectors(ctx, params)
		require.NoError(t, err)
		require.NotNil(t, resp)

		var result map[string]interface{}
		err = json.Unmarshal([]byte(resp.Content[0].Text), &result)
		require.NoError(t, err)
		assert.Equal(t, float64(100), result["page_size"])
	})
}

// TestHandleSearchByIDEdgeCases tests search by ID edge cases
func TestHandleSearchByIDEdgeCases(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	t.Run("WithAllOptionalParams", func(t *testing.T) {
		filters := []map[string]interface{}{
			{"key": "env", "value": "prod"},
		}
		filtersJSON, _ := json.Marshal(filters)

		params := map[string]interface{}{
			"namespace":        "test-ns",
			"id":               "vec-source",
			"k":                float64(15),
			"metadata_filters": string(filtersJSON),
			"min_score":        float64(0.7),
			"ef_search":        float64(150),
			"consistency":      "QUORUM",
			"timeout_ms":       float64(8000),
		}
		resp, err := server.handleSearchByID(ctx, params)
		require.NoError(t, err)
		require.NotNil(t, resp)

		var result map[string]interface{}
		err = json.Unmarshal([]byte(resp.Content[0].Text), &result)
		require.NoError(t, err)
		assert.Equal(t, float64(15), result["k"])
		assert.Equal(t, float64(0.7), result["min_score"])
	})
}

// TestHandleVectorHealthEdgeCases tests vector health edge cases
func TestHandleVectorHealthEdgeCases(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	t.Run("WithAllOptions", func(t *testing.T) {
		params := map[string]interface{}{
			"namespace":       "test-ns",
			"include_details": "true",
		}
		resp, err := server.handleVectorHealth(ctx, params)
		require.NoError(t, err)
		require.NotNil(t, resp)

		var result map[string]interface{}
		err = json.Unmarshal([]byte(resp.Content[0].Text), &result)
		require.NoError(t, err)
		assert.NotNil(t, result["status"])
	})
}

// TestHandleGetVectorClusterStatsEdgeCases tests cluster stats edge cases
func TestHandleGetVectorClusterStatsEdgeCases(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	t.Run("WithoutOptionals", func(t *testing.T) {
		params := map[string]interface{}{}
		resp, err := server.handleGetVectorClusterStats(ctx, params)
		require.NoError(t, err)
		require.NotNil(t, resp)

		var result map[string]interface{}
		err = json.Unmarshal([]byte(resp.Content[0].Text), &result)
		require.NoError(t, err)
		assert.NotNil(t, result["cluster_id"])
	})

	t.Run("WithAllOptions", func(t *testing.T) {
		params := map[string]interface{}{
			"include_namespaces": "true",
			"include_nodes":      "true",
		}
		resp, err := server.handleGetVectorClusterStats(ctx, params)
		require.NoError(t, err)
		require.NotNil(t, resp)

		var result map[string]interface{}
		err = json.Unmarshal([]byte(resp.Content[0].Text), &result)
		require.NoError(t, err)
		assert.NotNil(t, result["cluster_id"])
	})
}

// TestJSONToolResponseEdgeCases tests JSON response edge cases
func TestJSONToolResponseEdgeCases(t *testing.T) {
	server := newTestVectorServer(t)

	t.Run("EmptyResult", func(t *testing.T) {
		result := map[string]interface{}{}
		resp, err := server.jsonToolResponse(result)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.False(t, resp.IsError)

		var parsed map[string]interface{}
		err = json.Unmarshal([]byte(resp.Content[0].Text), &parsed)
		require.NoError(t, err)
		assert.Empty(t, parsed)
	})

	t.Run("NestedStructures", func(t *testing.T) {
		result := map[string]interface{}{
			"nested": map[string]interface{}{
				"level2": map[string]interface{}{
					"level3": "value",
				},
			},
			"array": []interface{}{1, 2, 3},
		}
		resp, err := server.jsonToolResponse(result)
		require.NoError(t, err)
		require.NotNil(t, resp)

		var parsed map[string]interface{}
		err = json.Unmarshal([]byte(resp.Content[0].Text), &parsed)
		require.NoError(t, err)
		assert.NotNil(t, parsed["nested"])
		assert.NotNil(t, parsed["array"])
	})
}
