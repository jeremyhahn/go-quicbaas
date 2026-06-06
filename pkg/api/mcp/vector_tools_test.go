//go:build mcp

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

// Package mcp implements Vector Database MCP tools tests
package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestVectorServer creates a test MCPServer with mock gRPC clients
func newTestVectorServer(t *testing.T) *MCPServer {
	log := newTestLogger(t)
	return &MCPServer{
		logger:       log,
		vectorClient: &mockVectorDBClient{},
		adminClient:  &mockVectorAdminClient{},
		handlers:     make(map[string]func(context.Context, map[string]interface{}) (*ToolResponse, error)),
	}
}

// TestVectorToolsCount verifies all 23 vector tools are registered
func TestVectorToolsCount(t *testing.T) {
	vectorTools := getAllVectorTools()
	assert.Equal(t, 23, len(vectorTools), "Expected 23 vector tools")

	// Verify tool names
	toolNames := make([]string, len(vectorTools))
	for i, tool := range vectorTools {
		toolNames[i] = tool.Name
	}

	expectedTools := []string{
		// VectorDB Tools (18)
		"qrdb_vector_upsert",
		"qrdb_vector_batch_upsert",
		"qrdb_vector_get",
		"qrdb_vector_delete",
		"qrdb_vector_update_metadata",
		"qrdb_vector_batch_delete",
		"qrdb_vector_search_similar",
		"qrdb_vector_search_by_id",
		"qrdb_vector_list",
		"qrdb_vector_list_namespaces",
		"qrdb_vector_create_namespace",
		"quicbaas_vector_update_namespace",
		"qrdb_vector_delete_namespace",
		"qrdb_vector_get_namespace_info",
		"quicbaas_vector_get_stats",
		"quicbaas_vector_health",
		"qrdb_vector_rebuild_index",
		"quicbaas_vector_get_index_progress",
		// VectorAdmin Tools (5)
		"quicbaas_vector_admin_get_cluster_stats",
		"quicbaas_vector_admin_compact_namespace",
		"quicbaas_vector_admin_get_compaction_progress",
		"quicbaas_vector_admin_export_vectors",
		"quicbaas_vector_admin_import_vectors",
	}

	assert.ElementsMatch(t, expectedTools, toolNames, "Tool names should match")
}

// TestVectorToolSchemas verifies all tools have proper JSON schemas
func TestVectorToolSchemas(t *testing.T) {
	vectorTools := getAllVectorTools()

	for _, tool := range vectorTools {
		t.Run(tool.Name, func(t *testing.T) {
			assert.NotEmpty(t, tool.Name, "Tool name should not be empty")
			assert.NotEmpty(t, tool.Description, "Tool description should not be empty")
			assert.Equal(t, "object", tool.InputSchema.Type, "Schema type should be object")
			assert.NotNil(t, tool.InputSchema.Properties, "Schema properties should not be nil")
		})
	}
}

// TestHandleUpsertVector tests the upsert vector handler
func TestHandleUpsertVector(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()
	vector := []float32{0.1, 0.2, 0.3}
	vectorJSON, _ := json.Marshal(vector)

	params := map[string]interface{}{
		"namespace": "test-namespace",
		"id":        "test-vector-1",
		"vector":    string(vectorJSON),
		"metadata":  `{"type": "test"}`,
	}

	resp, err := server.handleUpsertVector(ctx, params)
	require.NoError(t, err, "handleUpsertVector should not error")
	require.NotNil(t, resp, "Response should not be nil")
	assert.False(t, resp.IsError, "Response should not be error")
	assert.Len(t, resp.Content, 1, "Response should have 1 content item")
	assert.Equal(t, "text", resp.Content[0].Type, "Content type should be text")

	// Parse response JSON
	var result map[string]interface{}
	err = json.Unmarshal([]byte(resp.Content[0].Text), &result)
	require.NoError(t, err, "Should be able to parse response JSON")
	assert.Equal(t, "test-vector-1", result["id"], "ID should match")
	assert.Equal(t, "test-namespace", result["namespace"], "Namespace should match")
}

// TestHandleSearchSimilar tests the search similar handler
func TestHandleSearchSimilar(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()
	queryVector := []float32{0.1, 0.2, 0.3}
	queryVectorJSON, _ := json.Marshal(queryVector)

	params := map[string]interface{}{
		"namespace":    "test-namespace",
		"query_vector": string(queryVectorJSON),
		"k":            float64(10),
	}

	resp, err := server.handleSearchSimilar(ctx, params)
	require.NoError(t, err, "handleSearchSimilar should not error")
	require.NotNil(t, resp, "Response should not be nil")
	assert.False(t, resp.IsError, "Response should not be error")

	// Parse response JSON
	var result map[string]interface{}
	err = json.Unmarshal([]byte(resp.Content[0].Text), &result)
	require.NoError(t, err, "Should be able to parse response JSON")
	assert.Equal(t, "test-namespace", result["namespace"], "Namespace should match")
	assert.Equal(t, float64(10), result["k"], "K should match")
	assert.NotNil(t, result["results"], "Results should be present")
}

// TestHandleCreateNamespace tests the create namespace handler
func TestHandleCreateNamespace(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()
	params := map[string]interface{}{
		"namespace":       "test-namespace",
		"dimensions":      float64(384),
		"distance_metric": "COSINE",
	}

	resp, err := server.handleCreateNamespace(ctx, params)
	require.NoError(t, err, "handleCreateNamespace should not error")
	require.NotNil(t, resp, "Response should not be nil")
	assert.False(t, resp.IsError, "Response should not be error")

	// Parse response JSON
	var result map[string]interface{}
	err = json.Unmarshal([]byte(resp.Content[0].Text), &result)
	require.NoError(t, err, "Should be able to parse response JSON")
	assert.Equal(t, "test-namespace", result["namespace"], "Namespace should match")
	assert.Equal(t, float64(384), result["dimensions"], "Dimensions should match")
	assert.Equal(t, "COSINE", result["distance_metric"], "Distance metric should match")
}

// TestHandleListNamespaces tests the list namespaces handler
func TestHandleListNamespaces(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()
	params := map[string]interface{}{}

	resp, err := server.handleListNamespaces(ctx, params)
	require.NoError(t, err, "handleListNamespaces should not error")
	require.NotNil(t, resp, "Response should not be nil")
	assert.False(t, resp.IsError, "Response should not be error")

	// Parse response JSON
	var result map[string]interface{}
	err = json.Unmarshal([]byte(resp.Content[0].Text), &result)
	require.NoError(t, err, "Should be able to parse response JSON")
	assert.NotNil(t, result["namespaces"], "Namespaces should be present")
}

// TestHandleVectorHealth tests the vector health handler
func TestHandleVectorHealth(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()
	params := map[string]interface{}{
		"include_details": "true",
	}

	resp, err := server.handleVectorHealth(ctx, params)
	require.NoError(t, err, "handleVectorHealth should not error")
	require.NotNil(t, resp, "Response should not be nil")
	assert.False(t, resp.IsError, "Response should not be error")

	// Parse response JSON
	var result map[string]interface{}
	err = json.Unmarshal([]byte(resp.Content[0].Text), &result)
	require.NoError(t, err, "Should be able to parse response JSON")
	assert.Equal(t, "healthy", result["status"], "Status should be healthy")
	assert.NotNil(t, result["details"], "Details should be present")
}

// TestHandleGetVectorClusterStats tests the cluster stats handler
func TestHandleGetVectorClusterStats(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()
	params := map[string]interface{}{
		"include_namespaces": "true",
		"include_nodes":      "true",
	}

	resp, err := server.handleGetVectorClusterStats(ctx, params)
	require.NoError(t, err, "handleGetVectorClusterStats should not error")
	require.NotNil(t, resp, "Response should not be nil")
	assert.False(t, resp.IsError, "Response should not be error")

	// Parse response JSON
	var result map[string]interface{}
	err = json.Unmarshal([]byte(resp.Content[0].Text), &result)
	require.NoError(t, err, "Should be able to parse response JSON")
	assert.NotNil(t, result["cluster_id"], "Cluster ID should be present")
	assert.NotNil(t, result["namespace_stats"], "Namespace stats should be present")
	assert.NotNil(t, result["node_stats"], "Node stats should be present")
}

// TestParameterValidation tests parameter validation for vector tools
func TestParameterValidation(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	// Test missing required namespace parameter
	t.Run("MissingNamespace", func(t *testing.T) {
		params := map[string]interface{}{
			"id":     "test-id",
			"vector": `[0.1, 0.2, 0.3]`,
		}
		_, err := server.handleUpsertVector(ctx, params)
		assert.Error(t, err, "Should error on missing namespace")
	})

	// Test missing required ID parameter
	t.Run("MissingID", func(t *testing.T) {
		params := map[string]interface{}{
			"namespace": "test-ns",
			"vector":    `[0.1, 0.2, 0.3]`,
		}
		_, err := server.handleUpsertVector(ctx, params)
		assert.Error(t, err, "Should error on missing ID")
	})

	// Test invalid vector JSON
	t.Run("InvalidVectorJSON", func(t *testing.T) {
		params := map[string]interface{}{
			"namespace": "test-ns",
			"id":        "test-id",
			"vector":    `invalid-json`,
		}
		_, err := server.handleUpsertVector(ctx, params)
		assert.Error(t, err, "Should error on invalid vector JSON")
	})

	// Test invalid k value
	t.Run("InvalidKValue", func(t *testing.T) {
		params := map[string]interface{}{
			"namespace":    "test-ns",
			"query_vector": `[0.1, 0.2, 0.3]`,
			"k":            float64(20000), // exceeds max
		}
		_, err := server.handleSearchSimilar(ctx, params)
		assert.Error(t, err, "Should error on invalid k value")
	})

	// Test invalid dimensions
	t.Run("InvalidDimensions", func(t *testing.T) {
		params := map[string]interface{}{
			"namespace":       "test-ns",
			"dimensions":      float64(5000), // exceeds max
			"distance_metric": "COSINE",
		}
		_, err := server.handleCreateNamespace(ctx, params)
		assert.Error(t, err, "Should error on invalid dimensions")
	})
}

// TestAllToolsRegistered verifies all vector tools are registered in the MCPServer
func TestAllToolsRegistered(t *testing.T) {
	log := newTestLogger(t)
	mcpServer := NewMCPServer(nil, "localhost:9091", log)

	// MCPServer only registers vector tools: 23 tools total
	assert.Equal(t, 23, len(mcpServer.handlers), "Expected 23 vector tool handlers")
	assert.Equal(t, 23, len(mcpServer.tools), "Expected 23 vector tools in tools list")

	// Verify all vector tools are registered
	vectorToolNames := []string{
		"qrdb_vector_upsert",
		"qrdb_vector_batch_upsert",
		"qrdb_vector_get",
		"qrdb_vector_delete",
		"qrdb_vector_update_metadata",
		"qrdb_vector_batch_delete",
		"qrdb_vector_search_similar",
		"qrdb_vector_search_by_id",
		"qrdb_vector_list",
		"qrdb_vector_list_namespaces",
		"qrdb_vector_create_namespace",
		"quicbaas_vector_update_namespace",
		"qrdb_vector_delete_namespace",
		"qrdb_vector_get_namespace_info",
		"quicbaas_vector_get_stats",
		"quicbaas_vector_health",
		"qrdb_vector_rebuild_index",
		"quicbaas_vector_get_index_progress",
		"quicbaas_vector_admin_get_cluster_stats",
		"quicbaas_vector_admin_compact_namespace",
		"quicbaas_vector_admin_get_compaction_progress",
		"quicbaas_vector_admin_export_vectors",
		"quicbaas_vector_admin_import_vectors",
	}

	for _, toolName := range vectorToolNames {
		t.Run(toolName, func(t *testing.T) {
			handler, exists := mcpServer.handlers[toolName]
			assert.True(t, exists, "Tool handler should be registered: %s", toolName)
			assert.NotNil(t, handler, "Tool handler should not be nil: %s", toolName)
		})
	}
}
