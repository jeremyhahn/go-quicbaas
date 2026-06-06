//go:build mcp

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func newEmbeddingRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	svc := &extendedMockService{
		embeddingSvc: &mockEmbeddingService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(svc, log)
}

func TestEmbeddingGenerate_Success(t *testing.T) {
	registry := newEmbeddingRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "with text only",
			args: map[string]interface{}{
				"text": "hello world",
			},
		},
		{
			name: "with provider and input type",
			args: map[string]interface{}{
				"text":       "test document",
				"provider":   "ollama",
				"input_type": "document",
			},
		},
		{
			name: "with source and language",
			args: map[string]interface{}{
				"text":     "func main() {}",
				"source":   "main.go",
				"language": "go",
			},
		},
		{
			name: "with cache bypass",
			args: map[string]interface{}{
				"text":         "fresh embedding",
				"cache_bypass": "true",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_generate", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, resultMap, "embedding")
			assert.Contains(t, resultMap, "dimensions")
			// Check that metadata contains model info
			if metadata, ok := resultMap["metadata"].(map[string]interface{}); ok {
				assert.Contains(t, metadata, "model")
			}
		})
	}
}

func TestEmbeddingGenerate_Errors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		setupSvc    func() *ToolRegistry
		errContains string
	}{
		{
			name: "missing text",
			args: map[string]interface{}{},
			setupSvc: func() *ToolRegistry {
				return newEmbeddingRegistry(t)
			},
			errContains: "text is required",
		},
		{
			name: "empty text",
			args: map[string]interface{}{
				"text": "",
			},
			setupSvc: func() *ToolRegistry {
				return newEmbeddingRegistry(t)
			},
			errContains: "text is required",
		},
		{
			name: "service error",
			args: map[string]interface{}{
				"text": "test",
			},
			setupSvc: func() *ToolRegistry {
				svc := &extendedMockService{
					embeddingSvc: &mockEmbeddingService{
						generateFunc: func(ctx context.Context, req *service.GenerateEmbeddingRequest) (*service.GenerateEmbeddingResponse, error) {
							return nil, &errors.MessageError{Message: "provider unavailable"}
						},
					},
				}
				log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
				return NewToolRegistry(svc, log)
			},
			errContains: "provider unavailable",
		},
		{
			name: "service not configured",
			args: map[string]interface{}{
				"text": "test",
			},
			setupSvc: func() *ToolRegistry {
				svc := &extendedMockService{}
				log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
				return NewToolRegistry(svc, log)
			},
			errContains: "not configured",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			registry := tc.setupSvc()
			result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_generate", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestEmbeddingBatch_Success(t *testing.T) {
	registry := newEmbeddingRegistry(t)
	ctx := context.Background()

	items := []map[string]interface{}{
		{"id": "1", "text": "hello"},
		{"id": "2", "text": "world"},
	}
	itemsJSON, _ := json.Marshal(items)

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_batch", map[string]interface{}{
		"items": string(itemsJSON),
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "results")
	assert.Contains(t, resultMap, "success_count")
	assert.Contains(t, resultMap, "processing_time")
}

func TestEmbeddingBatch_Errors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{
			name:        "missing items",
			args:        map[string]interface{}{},
			errContains: "items is required",
		},
		{
			name: "invalid JSON",
			args: map[string]interface{}{
				"items": "not-json",
			},
			errContains: "invalid items JSON",
		},
		{
			name: "empty array",
			args: map[string]interface{}{
				"items": "[]",
			},
			errContains: "cannot be empty",
		},
		{
			name: "item with empty text",
			args: map[string]interface{}{
				"items": `[{"id":"1","text":""}]`,
			},
			errContains: "empty text",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			registry := newEmbeddingRegistry(t)
			result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_batch", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestEmbeddingBatch_WithOptions(t *testing.T) {
	registry := newEmbeddingRegistry(t)
	ctx := context.Background()

	items := []map[string]interface{}{
		{"text": "test text", "input_type": "document", "source": "test.txt"},
	}
	itemsJSON, _ := json.Marshal(items)

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_batch", map[string]interface{}{
		"items":        string(itemsJSON),
		"provider":     "voyage",
		"cache_bypass": "true",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestEmbeddingListProviders_Success(t *testing.T) {
	registry := newEmbeddingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_list_providers", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "providers")
	assert.Contains(t, resultMap, "default_provider")
	assert.Contains(t, resultMap, "count")
}

func TestEmbeddingListProviders_WithIncludeUnavailable(t *testing.T) {
	registry := newEmbeddingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_list_providers", map[string]interface{}{
		"include_unavailable": "true",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestEmbeddingListProviders_ServiceError(t *testing.T) {
	svc := &extendedMockService{
		embeddingSvc: &mockEmbeddingService{
			listProvidersFunc: func(ctx context.Context, req *service.ListProvidersRequest) (*service.ListProvidersResponse, error) {
				return nil, &errors.MessageError{Message: "internal error"}
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_list_providers", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "internal error")
}

func TestEmbeddingGetProvider_Success(t *testing.T) {
	registry := newEmbeddingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_get_provider", map[string]interface{}{
		"provider": "ollama",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "ollama", resultMap["name"])
}

func TestEmbeddingGetProvider_MissingProvider(t *testing.T) {
	registry := newEmbeddingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_get_provider", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "provider is required")
}

func TestEmbeddingSetDefault_Success(t *testing.T) {
	registry := newEmbeddingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_set_default", map[string]interface{}{
		"provider": "openai",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "provider")
	assert.Contains(t, resultMap, "previous_provider")
}

func TestEmbeddingSetDefault_MissingProvider(t *testing.T) {
	registry := newEmbeddingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_set_default", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "provider is required")
}

func TestEmbeddingCacheStats_Success(t *testing.T) {
	registry := newEmbeddingRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_cache_stats", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "enabled")
	assert.Contains(t, resultMap, "size")
	assert.Contains(t, resultMap, "hit_rate")
}

func TestEmbeddingCacheStats_ServiceError(t *testing.T) {
	svc := &extendedMockService{
		embeddingSvc: &mockEmbeddingService{
			cacheStatsFunc: func(ctx context.Context, req *service.GetEmbeddingCacheStatsRequest) (*service.GetEmbeddingCacheStatsResponse, error) {
				return nil, &errors.MessageError{Message: "cache error"}
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_cache_stats", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestEmbeddingCacheClear_Success(t *testing.T) {
	registry := newEmbeddingRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "clear all",
			args: map[string]interface{}{"all": "true"},
		},
		{
			name: "clear by provider",
			args: map[string]interface{}{"provider": "ollama"},
		},
		{
			name: "clear default",
			args: map[string]interface{}{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_embedding_cache_clear", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, resultMap, "entries_cleared")
		})
	}
}

func TestIsEmbeddingTool(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"quicbaas_embedding_generate", true},
		{"quicbaas_embedding_batch", true},
		{"quicbaas_embedding_list_providers", true},
		{"quicbaas_embedding_get_provider", true},
		{"quicbaas_embedding_set_default", true},
		{"quicbaas_embedding_cache_stats", true},
		{"quicbaas_embedding_cache_clear", true},
		{"qrdb_put", false},
		{"unknown_tool", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, isEmbeddingTool(tc.name))
		})
	}
}
