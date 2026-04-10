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

func newOllamaRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	svc := &extendedMockService{
		ollamaSvc: &mockOllamaService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(svc, log)
}

func TestOllamaEmbed_Success(t *testing.T) {
	registry := newOllamaRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "default model",
			args: map[string]interface{}{"text": "hello world"},
		},
		{
			name: "specific model",
			args: map[string]interface{}{"text": "test text", "model": "mxbai-embed-large"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_ollama_embed", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, resultMap, "embedding")
			assert.Contains(t, resultMap, "model")
			assert.Contains(t, resultMap, "dimensions")
		})
	}
}

func TestOllamaEmbed_Errors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		setupFunc   func() *ToolRegistry
		errContains string
	}{
		{
			name: "missing text",
			args: map[string]interface{}{},
			setupFunc: func() *ToolRegistry {
				return newOllamaRegistry(t)
			},
			errContains: "text is required",
		},
		{
			name: "embed service error",
			args: map[string]interface{}{"text": "test"},
			setupFunc: func() *ToolRegistry {
				svc := &extendedMockService{
					ollamaSvc: &mockOllamaService{
						embedFunc: func(ctx context.Context, req *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
							return nil, &errors.MessageError{Message: "connection refused"}
						},
					},
				}
				log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
				return NewToolRegistry(svc, log)
			},
			errContains: "connection refused",
		},
		{
			name: "service not configured",
			args: map[string]interface{}{"text": "test"},
			setupFunc: func() *ToolRegistry {
				svc := &extendedMockService{}
				log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
				return NewToolRegistry(svc, log)
			},
			errContains: "not configured",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			registry := tc.setupFunc()
			result, err := registry.ExecuteTool(ctx, "quicbaas_ollama_embed", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestOllamaEmbedBatch_Success(t *testing.T) {
	registry := newOllamaRegistry(t)
	ctx := context.Background()

	texts := []string{"hello", "world", "test"}
	textsJSON, _ := json.Marshal(texts)

	result, err := registry.ExecuteTool(ctx, "quicbaas_ollama_embed_batch", map[string]interface{}{
		"texts": string(textsJSON),
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "embeddings")
	assert.Contains(t, resultMap, "count")
}

func TestOllamaEmbedBatch_Errors(t *testing.T) {
	registry := newOllamaRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{"missing texts", map[string]interface{}{}, "texts is required"},
		{"invalid JSON", map[string]interface{}{"texts": "not-json"}, "invalid"},
		{"empty array", map[string]interface{}{"texts": "[]"}, "cannot be empty"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_ollama_embed_batch", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestOllamaModelList_Success(t *testing.T) {
	registry := newOllamaRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_ollama_model_list", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "models")
	assert.Contains(t, resultMap, "count")
}

func TestOllamaModelList_ServiceError(t *testing.T) {
	svc := &extendedMockService{
		ollamaSvc: &mockOllamaService{
			listModelsFunc: func(ctx context.Context, req *service.OllamaListModelsRequest) (*service.OllamaListModelsResponse, error) {
				return nil, &errors.MessageError{Message: "connection timeout"}
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_ollama_model_list", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestOllamaModelInfo_Success(t *testing.T) {
	registry := newOllamaRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_ollama_model_info", map[string]interface{}{
		"model": "nomic-embed-text",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "model")
	assert.Contains(t, resultMap, "details")
}

func TestOllamaModelInfo_MissingModel(t *testing.T) {
	registry := newOllamaRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_ollama_model_info", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "model is required")
}

func TestOllamaHealth_Success(t *testing.T) {
	registry := newOllamaRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_ollama_health", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "available")
	assert.Contains(t, resultMap, "version")
}

func TestOllamaHealth_ServiceError(t *testing.T) {
	// The health handler is designed to never return an error - it returns
	// available: false with an error message in the response instead.
	svc := &extendedMockService{
		ollamaSvc: &mockOllamaService{
			healthFunc: func(ctx context.Context, req *service.OllamaHealthRequest) (*service.OllamaHealthResponse, error) {
				return nil, &errors.MessageError{Message: "health check failed"}
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_ollama_health", map[string]interface{}{})
	// Health tool never returns an error - it returns available: false with error message
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, false, resultMap["available"])
	assert.Contains(t, resultMap["error"], "health check failed")
}

func TestOllamaEmbedBatch_WithModel(t *testing.T) {
	registry := newOllamaRegistry(t)
	ctx := context.Background()

	texts := []string{"hello", "world"}
	textsJSON, _ := json.Marshal(texts)

	result, err := registry.ExecuteTool(ctx, "quicbaas_ollama_embed_batch", map[string]interface{}{
		"texts": string(textsJSON),
		"model": "custom-model",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}
