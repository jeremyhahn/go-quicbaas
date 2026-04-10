//go:build mcp && rag

package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestGetAllRAGTools(t *testing.T) {
	tools := getAllRAGTools()
	if len(tools) != 2 {
		t.Errorf("expected 2 rag tools, got %d", len(tools))
	}

	expectedTools := map[string]bool{
		"quicbaas_rag_query":         false,
		"quicbaas_rag_build_context": false,
	}

	for _, tool := range tools {
		if _, ok := expectedTools[tool.Name]; !ok {
			t.Errorf("unexpected tool name: %s", tool.Name)
		}
		expectedTools[tool.Name] = true
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("missing expected tool: %s", name)
		}
	}
}

func TestIsRAGTool(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		expected bool
	}{
		{"query tool", "quicbaas_rag_query", true},
		{"build context tool", "quicbaas_rag_build_context", true},
		{"unknown tool", "quicbaas_unknown", false},
		{"ollama tool", "quicbaas_ollama_embed", false},
		{"vector tool", "quicbaas_vector_search_similar", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRAGTool(tt.toolName)
			if result != tt.expected {
				t.Errorf("isRAGTool(%s) = %v, want %v", tt.toolName, result, tt.expected)
			}
		})
	}
}

func TestRAGQueryTool_Schema(t *testing.T) {
	tool := RAGQueryTool

	if tool.Name != "quicbaas_rag_query" {
		t.Errorf("expected name 'quicbaas_rag_query', got %s", tool.Name)
	}

	if tool.InputSchema.Type != "object" {
		t.Errorf("expected type 'object', got %s", tool.InputSchema.Type)
	}

	// Check required fields
	expectedRequired := map[string]bool{"query": false, "namespace": false}
	for _, req := range tool.InputSchema.Required {
		if _, ok := expectedRequired[req]; !ok {
			t.Errorf("unexpected required field: %s", req)
		}
		expectedRequired[req] = true
	}
	for name, found := range expectedRequired {
		if !found {
			t.Errorf("missing required field: %s", name)
		}
	}

	// Check properties exist
	props := tool.InputSchema.Properties
	expectedProps := []string{"query", "namespace", "top_k", "max_tokens", "min_score", "template", "include_citations", "model"}
	for _, propName := range expectedProps {
		if _, ok := props[propName]; !ok {
			t.Errorf("missing '%s' property", propName)
		}
	}

	// Check template has enum
	templateProp := props["template"]
	if len(templateProp.Enum) == 0 {
		t.Error("expected template property to have enum values")
	}
}

func TestRAGBuildContextTool_Schema(t *testing.T) {
	tool := RAGBuildContextTool

	if tool.Name != "quicbaas_rag_build_context" {
		t.Errorf("expected name 'quicbaas_rag_build_context', got %s", tool.Name)
	}

	// Check required fields
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "results" {
		t.Errorf("expected required field 'results', got %v", tool.InputSchema.Required)
	}

	// Check properties exist
	props := tool.InputSchema.Properties
	expectedProps := []string{"results", "max_tokens", "template", "include_citations", "truncation_strategy"}
	for _, propName := range expectedProps {
		if _, ok := props[propName]; !ok {
			t.Errorf("missing '%s' property", propName)
		}
	}

	// Check truncation_strategy has enum
	truncProp := props["truncation_strategy"]
	if len(truncProp.Enum) == 0 {
		t.Error("expected truncation_strategy property to have enum values")
	}
}

func TestGetStringFromMetadata(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]string
		key      string
		expected string
	}{
		{"nil metadata", nil, "key", ""},
		{"empty metadata", map[string]string{}, "key", ""},
		{"key not found", map[string]string{"other": "value"}, "key", ""},
		{"key found", map[string]string{"key": "value"}, "key", "value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getStringFromMetadata(tt.metadata, tt.key)
			if result != tt.expected {
				t.Errorf("getStringFromMetadata() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetIntFromMetadata(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]string
		key      string
		expected int
	}{
		{"nil metadata", nil, "key", 0},
		{"empty metadata", map[string]string{}, "key", 0},
		{"key not found", map[string]string{"other": "123"}, "key", 0},
		{"key found with valid int", map[string]string{"key": "42"}, "key", 42},
		{"key found with invalid int", map[string]string{"key": "invalid"}, "key", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getIntFromMetadata(tt.metadata, tt.key)
			if result != tt.expected {
				t.Errorf("getIntFromMetadata() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetIntFromStringMap(t *testing.T) {
	tests := []struct {
		name     string
		m        map[string]string
		key      string
		expected int
	}{
		{"nil map", nil, "key", 0},
		{"empty map", map[string]string{}, "key", 0},
		{"key not found", map[string]string{"other": "123"}, "key", 0},
		{"key found with valid int", map[string]string{"key": "42"}, "key", 42},
		{"key found with invalid int", map[string]string{"key": "invalid"}, "key", 0},
		{"negative int", map[string]string{"key": "-10"}, "key", -10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getIntFromStringMap(tt.m, tt.key)
			if result != tt.expected {
				t.Errorf("getIntFromStringMap() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetStringFromInterfaceMetadata(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]interface{}
		key      string
		expected string
	}{
		{"nil metadata", nil, "key", ""},
		{"empty metadata", map[string]interface{}{}, "key", ""},
		{"key not found", map[string]interface{}{"other": "value"}, "key", ""},
		{"key found with string", map[string]interface{}{"key": "value"}, "key", "value"},
		{"key found with non-string", map[string]interface{}{"key": 123}, "key", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getStringFromInterfaceMetadata(tt.metadata, tt.key)
			if result != tt.expected {
				t.Errorf("getStringFromInterfaceMetadata() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetIntFromInterfaceMetadata(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]interface{}
		key      string
		expected int
	}{
		{"nil metadata", nil, "key", 0},
		{"empty metadata", map[string]interface{}{}, "key", 0},
		{"key not found", map[string]interface{}{"other": 123}, "key", 0},
		{"key found with int", map[string]interface{}{"key": 42}, "key", 42},
		{"key found with int64", map[string]interface{}{"key": int64(100)}, "key", 100},
		{"key found with float64", map[string]interface{}{"key": float64(75.0)}, "key", 75},
		{"key found with string int", map[string]interface{}{"key": "50"}, "key", 50},
		{"key found with invalid string", map[string]interface{}{"key": "invalid"}, "key", 0},
		{"key found with unsupported type", map[string]interface{}{"key": []int{1, 2, 3}}, "key", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getIntFromInterfaceMetadata(tt.metadata, tt.key)
			if result != tt.expected {
				t.Errorf("getIntFromInterfaceMetadata() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// =============================================================================
// RAG Tool Error Path Tests
// =============================================================================

func TestHandleRAGQuery_MissingQueryParam(t *testing.T) {
	svc := &extendedMockService{
		ollamaSvc: &mockOllamaService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_rag_query", map[string]interface{}{
		"namespace": "test",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "query is required")
}

func TestHandleRAGQuery_MissingNamespaceParam(t *testing.T) {
	svc := &extendedMockService{
		ollamaSvc: &mockOllamaService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_rag_query", map[string]interface{}{
		"query": "test query",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "namespace is required")
}

func TestHandleRAGQuery_OllamaServiceNotConfigured(t *testing.T) {
	svc := &extendedMockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_rag_query", map[string]interface{}{
		"query":     "test query",
		"namespace": "test",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "ollama service not configured")
}

func TestExecuteRAGTool_UnknownRAGTool(t *testing.T) {
	svc := &extendedMockService{
		ollamaSvc: &mockOllamaService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.executeRAGTool(ctx, "quicbaas_rag_unknown", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unknown tool: quicbaas_rag_unknown")
}

// =============================================================================
// Helper Function Additional Tests
// =============================================================================

func TestGetUint64_AllBranches(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		key      string
		expected uint64
	}{
		{"float64 value", map[string]interface{}{"key": float64(100)}, "key", 100},
		{"missing key", map[string]interface{}{}, "key", 0},
		{"zero value", map[string]interface{}{"key": float64(0)}, "key", 0},
		{"large value", map[string]interface{}{"key": float64(999999)}, "key", 999999},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := getUint64(tc.args, tc.key)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGetBool_AllBranches(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		key      string
		expected bool
	}{
		{"true value", map[string]interface{}{"key": true}, "key", true},
		{"false value", map[string]interface{}{"key": false}, "key", false},
		{"missing key", map[string]interface{}{}, "key", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := getBool(tc.args, tc.key)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGetString_AllBranches(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		key      string
		expected string
	}{
		{"string value", map[string]interface{}{"key": "value"}, "key", "value"},
		{"missing key", map[string]interface{}{}, "key", ""},
		{"empty string", map[string]interface{}{"key": ""}, "key", ""},
		{"non-string", map[string]interface{}{"key": 123}, "key", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := getString(tc.args, tc.key)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// Scheduler Tool Error Path Tests
// =============================================================================

func TestSchedulerServiceNotAvailable(t *testing.T) {
	svc := &extendedMockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_scheduler_list_jobs", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "scheduler service not available")
}

func TestExecuteSchedulerTool_UnknownToolName(t *testing.T) {
	svc := &extendedMockService{
		schedulerSvc: &mockSchedulerService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.executeSchedulerTool(ctx, "quicbaas_scheduler_unknown", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unknown tool: quicbaas_scheduler_unknown")
}

// =============================================================================
// Is-Tool Function Coverage Tests (using correct tool names)
// =============================================================================

func TestIsRAGTool_AllBranches(t *testing.T) {
	ragTools := []string{
		"quicbaas_rag_query",
		"quicbaas_rag_build_context",
	}

	for _, name := range ragTools {
		assert.True(t, isRAGTool(name), "expected %s to be RAG tool", name)
	}

	assert.False(t, isRAGTool("quicbaas_embedding_generate"))
	assert.False(t, isRAGTool("quicbaas_put"))
	assert.False(t, isRAGTool(""))
}

func TestIsSchedulerTool_AllBranches(t *testing.T) {
	schedulerTools := []string{
		"quicbaas_scheduler_create_job",
		"quicbaas_scheduler_get_job",
		"quicbaas_scheduler_list_jobs",
		"quicbaas_scheduler_delete_job",
		"quicbaas_scheduler_pause_job",
		"quicbaas_scheduler_resume_job",
		"quicbaas_scheduler_trigger_job",
	}

	for _, name := range schedulerTools {
		assert.True(t, isSchedulerTool(name), "expected %s to be scheduler tool", name)
	}

	assert.False(t, isSchedulerTool("quicbaas_embedding_generate"))
	assert.False(t, isSchedulerTool("quicbaas_put"))
	assert.False(t, isSchedulerTool(""))
}

func TestIsObjectTransferTool_AllBranches(t *testing.T) {
	transferTools := []string{
		"quicbaas_object_transfer_fetch",
		"quicbaas_object_transfer_stream_fetch",
	}

	for _, name := range transferTools {
		assert.True(t, isObjectTransferTool(name), "expected %s to be object transfer tool", name)
	}

	assert.False(t, isObjectTransferTool("quicbaas_object_put"))
	assert.False(t, isObjectTransferTool("quicbaas_put"))
	assert.False(t, isObjectTransferTool(""))
}

func TestIsDynamicSecretsTool_AllBranches(t *testing.T) {
	dynamicTools := []string{
		"quicbaas_dynamic_secrets_create_role",
		"quicbaas_dynamic_secrets_get_role",
		"quicbaas_dynamic_secrets_list_roles",
		"quicbaas_dynamic_secrets_delete_role",
		"quicbaas_dynamic_secrets_generate",
		"quicbaas_dynamic_secrets_renew_lease",
		"quicbaas_dynamic_secrets_revoke_lease",
		"quicbaas_dynamic_secrets_list_leases",
	}

	for _, name := range dynamicTools {
		assert.True(t, isDynamicSecretsTool(name), "expected %s to be dynamic secrets tool", name)
	}

	assert.False(t, isDynamicSecretsTool("quicbaas_kms_encrypt"))
	assert.False(t, isDynamicSecretsTool("quicbaas_put"))
	assert.False(t, isDynamicSecretsTool(""))
}

func TestIsClusterDEKTool_AllBranches(t *testing.T) {
	clusterTools := []string{
		"quicbaas_cluster_dek_init",
		"quicbaas_cluster_dek_status",
		"quicbaas_cluster_dek_rotate",
		"quicbaas_cluster_dek_sync_node",
		"quicbaas_cluster_dek_node_status",
		"quicbaas_cluster_dek_start_reencryption",
		"quicbaas_cluster_dek_reencryption_status",
		"quicbaas_cluster_dek_cancel_reencryption",
		"quicbaas_cluster_dek_recover",
	}

	for _, name := range clusterTools {
		assert.True(t, isClusterDEKTool(name), "expected %s to be cluster DEK tool", name)
	}

	assert.False(t, isClusterDEKTool("quicbaas_kms_encrypt"))
	assert.False(t, isClusterDEKTool("quicbaas_put"))
	assert.False(t, isClusterDEKTool(""))
}

func TestIsIAMTool_AllBranches(t *testing.T) {
	iamTools := []string{
		"quicbaas_iam_whoami",
		"quicbaas_iam_create_token",
		"quicbaas_iam_revoke_token",
	}

	for _, name := range iamTools {
		assert.True(t, isIAMTool(name), "expected %s to be IAM tool", name)
	}

	assert.False(t, isIAMTool("quicbaas_kms_encrypt"))
	assert.False(t, isIAMTool("quicbaas_put"))
	assert.False(t, isIAMTool(""))
}

func TestIsVectorTool_AllBranches(t *testing.T) {
	vectorTools := []string{
		"quicbaas_vector_put",
		"quicbaas_vector_get",
		"quicbaas_vector_delete",
		"quicbaas_vector_search",
		"quicbaas_vector_batch_put",
		"quicbaas_vector_batch_get",
		"quicbaas_vector_batch_delete",
	}

	for _, name := range vectorTools {
		assert.True(t, isVectorTool(name), "expected %s to be vector tool", name)
	}

	assert.False(t, isVectorTool("quicbaas_kms_encrypt"))
	assert.False(t, isVectorTool("quicbaas_put"))
	assert.False(t, isVectorTool(""))
}

func TestIsCertificateTool_AllBranches(t *testing.T) {
	certTools := []string{
		"quicbaas_certificate_issue",
		"quicbaas_certificate_sign_csr",
		"quicbaas_certificate_revoke",
		"quicbaas_certificate_verify",
		"quicbaas_certificate_get_ca",
		"quicbaas_certificate_get_tls_config",
	}

	for _, name := range certTools {
		assert.True(t, isCertificateTool(name), "expected %s to be certificate tool", name)
	}

	assert.False(t, isCertificateTool("quicbaas_kms_encrypt"))
	assert.False(t, isCertificateTool("quicbaas_put"))
	assert.False(t, isCertificateTool(""))
}

func TestIsMsgTool_AllBranches(t *testing.T) {
	msgTools := []string{
		"quicbaas_msg_create_channel",
		"quicbaas_msg_delete_channel",
		"quicbaas_msg_get_channel",
		"quicbaas_msg_list_channels",
		"quicbaas_msg_publish",
		"quicbaas_msg_subscribe",
		"quicbaas_msg_unsubscribe",
		"quicbaas_msg_stats",
		"quicbaas_msg_publish_delta",
		"quicbaas_msg_dedup_stats",
	}

	for _, name := range msgTools {
		assert.True(t, isMsgTool(name), "expected %s to be messaging tool", name)
	}

	assert.False(t, isMsgTool("quicbaas_kms_encrypt"))
	assert.False(t, isMsgTool("quicbaas_put"))
	assert.False(t, isMsgTool(""))
}

// =============================================================================
// RAG Build Context Tests (handleRAGBuildContext at 0% coverage)
// =============================================================================

func TestExecuteRAGBuildContext_Success(t *testing.T) {
	mockDB := &MockQuicBaaS{}
	log := newTestLogger(t)
	registry := NewToolRegistry(mockDB, log)
	ctx := context.Background()

	results := []struct {
		Content  string            `json:"content"`
		Score    float32           `json:"score"`
		Metadata map[string]string `json:"metadata"`
	}{
		{
			Content: "This is test content for RAG context building.",
			Score:   0.95,
			Metadata: map[string]string{
				"file_path":  "/src/main.go",
				"file_name":  "main.go",
				"language":   "go",
				"start_line": "1",
				"end_line":   "10",
			},
		},
		{
			Content: "Another piece of content for context.",
			Score:   0.85,
			Metadata: map[string]string{
				"file_path": "/src/utils.go",
				"file_name": "utils.go",
				"language":  "go",
			},
		},
	}

	resultsJSON, err := json.Marshal(results)
	require.NoError(t, err)

	result, err := registry.ExecuteTool(ctx, "quicbaas_rag_build_context", map[string]interface{}{
		"results": string(resultsJSON),
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "context")
	assert.Contains(t, resultMap, "tokens")
	assert.Contains(t, resultMap, "result_count")
}

func TestExecuteRAGBuildContext_WithAllOptions(t *testing.T) {
	mockDB := &MockQuicBaaS{}
	log := newTestLogger(t)
	registry := NewToolRegistry(mockDB, log)
	ctx := context.Background()

	results := []struct {
		Content  string            `json:"content"`
		Score    float32           `json:"score"`
		Metadata map[string]string `json:"metadata"`
	}{
		{
			Content:  "Test content.",
			Score:    0.9,
			Metadata: map[string]string{},
		},
	}

	resultsJSON, err := json.Marshal(results)
	require.NoError(t, err)

	tests := []struct {
		name     string
		template string
		strategy string
	}{
		{"json template", "json", "hard"},
		{"xml template", "xml", "none"},
		{"plain template", "plain", "smart"},
		{"default template", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]interface{}{
				"results":             string(resultsJSON),
				"max_tokens":          float64(2000),
				"include_citations":   "false",
				"template":            tc.template,
				"truncation_strategy": tc.strategy,
			}

			result, err := registry.ExecuteTool(ctx, "quicbaas_rag_build_context", args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestExecuteRAGBuildContext_MissingResults(t *testing.T) {
	mockDB := &MockQuicBaaS{}
	log := newTestLogger(t)
	registry := NewToolRegistry(mockDB, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_rag_build_context", map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "results is required")
}

func TestExecuteRAGBuildContext_InvalidJSON(t *testing.T) {
	mockDB := &MockQuicBaaS{}
	log := newTestLogger(t)
	registry := NewToolRegistry(mockDB, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_rag_build_context", map[string]interface{}{
		"results": "not-valid-json",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid results JSON")
}

func TestExecuteRAGBuildContext_EmptyResults(t *testing.T) {
	mockDB := &MockQuicBaaS{}
	log := newTestLogger(t)
	registry := NewToolRegistry(mockDB, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_rag_build_context", map[string]interface{}{
		"results": "[]",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "results array cannot be empty")
}

func TestExecuteRAGTool_UnknownTool(t *testing.T) {
	mockDB := &MockQuicBaaS{}
	log := newTestLogger(t)
	registry := NewToolRegistry(mockDB, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_rag_nonexistent", map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown")
}

// =============================================================================
// RAG Query Tests (handleRAGQuery at 0% coverage)
// =============================================================================

func TestExecuteRAGQuery_OllamaNotConfigured(t *testing.T) {
	mockDB := &MockQuicBaaS{}
	log := newTestLogger(t)
	registry := NewToolRegistry(mockDB, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_rag_query", map[string]interface{}{
		"query":     "test query",
		"namespace": "test-ns",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ollama service not configured")
}

func TestExecuteRAGQuery_MissingQuery(t *testing.T) {
	mockDB := &MockQuicBaaS{}
	log := newTestLogger(t)
	registry := NewToolRegistry(mockDB, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_rag_query", map[string]interface{}{
		"namespace": "test-ns",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query is required")
}

func TestExecuteRAGQuery_MissingNamespace(t *testing.T) {
	mockDB := &MockQuicBaaS{}
	log := newTestLogger(t)
	registry := NewToolRegistry(mockDB, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_rag_query", map[string]interface{}{
		"query": "test query",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace is required")
}

// =============================================================================
// Object Storage Admin - GetStorageStats with BackendStats and TenantStats
// =============================================================================

func TestObjectAdminGetStorageStats_WithBackendAndTenantStats(t *testing.T) {
	adminSvc := &mockObjectStorageAdminService{
		getStorageStatsFunc: func(_ context.Context, _ *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error) {
			return &service.GetStorageStatsResponse{
				TotalObjects:    5000,
				TotalBytes:      1024 * 1024 * 1024 * 10,
				TotalBackends:   3,
				HealthyBackends: 2,
				CalculatedAt:    time.Now().Unix(),
				BackendStats: []*service.BackendInfo{
					{
						Name:        "primary",
						Type:        "local",
						Enabled:     true,
						Tier:        "hot",
						ObjectCount: 3000,
						TotalSize:   1024 * 1024 * 1024 * 6,
					},
					{
						Name:        "archive",
						Type:        "s3",
						Enabled:     true,
						Tier:        "cold",
						ObjectCount: 2000,
						TotalSize:   1024 * 1024 * 1024 * 4,
					},
				},
				TenantStats: []*service.TenantStorageStats{
					{
						TenantID:     "tenant-1",
						TotalBytes:   1024 * 1024 * 500,
						ObjectCount:  1000,
						HotBytes:     1024 * 1024 * 300,
						WarmBytes:    1024 * 1024 * 100,
						ColdBytes:    1024 * 1024 * 80,
						ArchiveBytes: 1024 * 1024 * 20,
					},
					{
						TenantID:    "tenant-2",
						TotalBytes:  1024 * 1024 * 200,
						ObjectCount: 500,
					},
				},
			}, nil
		},
	}

	svc := &objStorageAdminMockService{
		objAdminSvc: adminSvc,
	}

	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_get_storage_stats", map[string]interface{}{
		"include_backends": true,
		"include_tenants":  true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "total_objects")
	assert.Contains(t, resultMap, "backend_stats")
	assert.Contains(t, resultMap, "tenant_stats")

	// Verify backend_stats populated
	backendStats, ok := resultMap["backend_stats"].([]interface{})
	require.True(t, ok)
	assert.Len(t, backendStats, 2)

	// Verify tenant_stats populated
	tenantStats, ok := resultMap["tenant_stats"].([]interface{})
	require.True(t, ok)
	assert.Len(t, tenantStats, 2)
}

// =============================================================================
// Object Storage Admin - RepairObject with error response
// =============================================================================

func TestObjectAdminRepairObject_WithErrorResponse(t *testing.T) {
	adminSvc := &mockObjectStorageAdminService{
		repairObjectFunc: func(_ context.Context, req *service.RepairObjectRequest) (*service.RepairObjectResponse, error) {
			return &service.RepairObjectResponse{
				Key:               req.Key,
				Success:           false,
				RepairedLocations: []string{},
				Error:             "object corrupted beyond repair",
			}, nil
		},
	}

	svc := &objStorageAdminMockService{
		objAdminSvc: adminSvc,
	}

	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_object_admin_repair_object", map[string]interface{}{
		"key":             "damaged-object",
		"source_location": "backup-1",
		"timeout_ms":      float64(10000),
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "error")
	assert.Equal(t, "object corrupted beyond repair", resultMap["error"])
}
