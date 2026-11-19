//go:build mcp

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func newExtendedRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	svc := &extendedMockService{
		embeddingSvc: &mockEmbeddingService{},
		ollamaSvc:    &mockOllamaService{},
		schedulerSvc: &mockSchedulerService{},
		parameterSvc: &mockParameterStoreService{},
		messagingSvc: &mockMessagingService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(svc, log)
}

// =============================================================================
// Batch, Transaction, and List Tool Execution Tests
// =============================================================================

func TestExtendedBatchTools(t *testing.T) {
	keyOps := []interface{}{
		map[string]interface{}{"entity_type": "users", "id": float64(1)},
		map[string]interface{}{"entity_type": "users", "id": float64(2)},
	}
	tests := []struct {
		name    string
		tool    string
		args    map[string]interface{}
		wantErr bool
	}{
		{
			name: "put succeeds",
			tool: "quicbaas_batch_put",
			args: map[string]interface{}{"operations": []interface{}{
				map[string]interface{}{"entity_type": "users", "id": float64(1), "data": "dmFsdWUx"}, // base64("value1")
				map[string]interface{}{"entity_type": "users", "id": float64(2), "data": "dmFsdWUy"}, // base64("value2")
			}},
		},
		{name: "put without operations", tool: "quicbaas_batch_put", args: map[string]interface{}{}, wantErr: true},
		{
			name:    "put with operations not an array",
			tool:    "quicbaas_batch_put",
			args:    map[string]interface{}{"operations": "not-an-array"},
			wantErr: true,
		},
		{name: "get succeeds", tool: "quicbaas_batch_get", args: map[string]interface{}{"operations": keyOps}},
		{name: "get without operations", tool: "quicbaas_batch_get", args: map[string]interface{}{}, wantErr: true},
		{name: "delete succeeds", tool: "quicbaas_batch_delete", args: map[string]interface{}{"operations": keyOps}},
		{name: "delete without operations", tool: "quicbaas_batch_delete", args: map[string]interface{}{}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := newExtendedRegistry(t)
			// The mock service returns nil, nil for batch operations, so a
			// successful call has no result to assert on.
			result, err := registry.ExecuteTool(context.Background(), tt.tool, tt.args)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestExtendedList(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{"default limit", map[string]interface{}{"entity_type": "users"}},
		{"explicit limit", map[string]interface{}{"entity_type": "users", "limit": float64(50)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := newExtendedRegistry(t)
			result, err := registry.ExecuteTool(context.Background(), "qrdb_list", tt.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

// =============================================================================
// Cache Config Tests (using correct tool names)
// =============================================================================

func TestExtendedGetCacheConfig_Success(t *testing.T) {
	registry := newExtendedRegistry(t)
	ctx := context.Background()

	// Use the correct tool name: quicbaas_cache_config_get
	result, err := registry.ExecuteTool(ctx, "quicbaas_cache_config_get", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExtendedGetCacheConfig_ServiceError(t *testing.T) {
	svc := &extendedMockService{
		embeddingSvc: &mockEmbeddingService{},
		ollamaSvc:    &mockOllamaService{},
		schedulerSvc: &mockSchedulerService{},
		parameterSvc: &mockParameterStoreService{},
		messagingSvc: &mockMessagingService{},
		cacheConfigFunc: func(ctx context.Context, req *service.GetCacheConfigRequest) (*service.GetCacheConfigResponse, error) {
			return nil, errors.New(errors.ErrInternal, "GetCacheConfig", "cache backend unavailable")
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_cache_config_get", map[string]interface{}{})
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestExtendedUpdateCacheConfig_Success(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{"size only", map[string]interface{}{"l1_size": float64(2000)}},
		{"all options", map[string]interface{}{
			"l1_size":          float64(5000),
			"l1_ttl":           "10m",
			"cleanup_interval": "2m",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := newExtendedRegistry(t)
			result, err := registry.ExecuteTool(context.Background(), "quicbaas_cache_config_update", tt.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestExtendedUpdateCacheConfig_ServiceError(t *testing.T) {
	svc := &extendedMockService{
		embeddingSvc: &mockEmbeddingService{},
		ollamaSvc:    &mockOllamaService{},
		schedulerSvc: &mockSchedulerService{},
		parameterSvc: &mockParameterStoreService{},
		messagingSvc: &mockMessagingService{},
		updateCacheFunc: func(ctx context.Context, req *service.UpdateCacheConfigRequest) (*service.UpdateCacheConfigResponse, error) {
			return nil, errors.New(errors.ErrInternal, "UpdateCacheConfig", "cache backend unavailable")
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_cache_config_update", map[string]interface{}{
		"l1_size": float64(2000),
	})
	assert.Error(t, err)
	assert.Nil(t, result)
}

// =============================================================================
// Utility Function Tests
// =============================================================================

func TestExtendedGetString(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		key      string
		expected string
	}{
		{"string value", map[string]interface{}{"k": "hello"}, "k", "hello"},
		{"missing key", map[string]interface{}{}, "k", ""},
		{"non-string type", map[string]interface{}{"k": 42}, "k", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, getString(tc.args, tc.key))
		})
	}
}

func TestExtendedGetStringAlt(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		key1     string
		key2     string
		expected string
	}{
		{"first key exists", map[string]interface{}{"a": "val1", "b": "val2"}, "a", "b", "val1"},
		{"only second key", map[string]interface{}{"b": "val2"}, "a", "b", "val2"},
		{"neither key", map[string]interface{}{}, "a", "b", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, getStringAlt(tc.args, tc.key1, tc.key2))
		})
	}
}

func TestExtendedGetInt(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		key      string
		expected int
	}{
		{"float64", map[string]interface{}{"k": float64(42)}, "k", 42},
		{"int", map[string]interface{}{"k": int(10)}, "k", 10},
		{"missing", map[string]interface{}{}, "k", 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, getInt(tc.args, tc.key))
		})
	}
}

func TestExtendedParseBool(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		defaultValue bool
		expected     bool
	}{
		{"true overrides default", "true", false, true},
		{"false overrides default", "false", true, false},
		{"empty uses default", "", true, true},
		{"unparsable uses default", "bogus", false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, parseBool(tc.input, tc.defaultValue))
		})
	}
}

func TestExtendedParseInt(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		defaultValue int
		expected     int
	}{
		{"number", "42", 0, 42},
		{"empty uses default", "", 100, 100},
		{"unparsable uses default", "abc", 50, 50},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, parseInt(tc.input, tc.defaultValue))
			assert.Equal(t, int64(tc.expected), parseInt64(tc.input, int64(tc.defaultValue)))
		})
	}
}

func TestExtendedGetStringDefault(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		expected string
	}{
		{"value present", map[string]interface{}{"k": "value"}, "value"},
		{"empty uses default", map[string]interface{}{"k": ""}, "default"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, getStringDefault(tc.args, "k", "default"))
		})
	}
}

func TestExtendedFormatParameterValue(t *testing.T) {
	t.Run("full parameter", func(t *testing.T) {
		exp := time.Now().Add(time.Hour)
		p := &interfaces.ParameterValue{
			Path:         "/app/config",
			Value:        []byte("test-value"),
			Type:         "string",
			Description:  "test param",
			Tags:         map[string]string{"env": "prod"},
			IsEncrypted:  true,
			KMSKeyID:     "key-1",
			ExpiresAt:    &exp,
			AuditEnabled: true,
			CreatedBy:    "admin",
			UpdatedBy:    "admin",
		}
		result := formatParameterValue(p)
		assert.Equal(t, "/app/config", result["path"])
		assert.Equal(t, "test-value", result["value"])
		assert.Equal(t, "test param", result["description"])
		assert.Contains(t, result, "tags")
		assert.Contains(t, result, "kms_key_id")
		assert.Contains(t, result, "expires_at")
		assert.Contains(t, result, "created_by")
		assert.Contains(t, result, "updated_by")
	})

	t.Run("minimal parameter", func(t *testing.T) {
		p := &interfaces.ParameterValue{
			Path: "/app/min",
			Type: "string",
		}
		result := formatParameterValue(p)
		assert.Equal(t, "/app/min", result["path"])
		_, hasValue := result["value"]
		assert.False(t, hasValue)
		_, hasTags := result["tags"]
		assert.False(t, hasTags)
	})
}

func TestExtendedFormatParameterResponse(t *testing.T) {
	p := &interfaces.ParameterValue{
		Path:  "/app/test",
		Value: []byte("test"),
		Type:  "string",
	}
	result := formatParameterResponse(p, time.Now().Format(time.RFC3339))
	// Result contains "parameter" key with nested data
	assert.Contains(t, result, "parameter")
	if param, ok := result["parameter"].(map[string]interface{}); ok {
		assert.Contains(t, param, "path")
		assert.Contains(t, param, "type")
	}
}

// =============================================================================
// Unknown Tool Error Test
// =============================================================================

func TestExtendedUnknownTool(t *testing.T) {
	registry := newExtendedRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "nonexistent_tool_xyz", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unknown tool")
}
