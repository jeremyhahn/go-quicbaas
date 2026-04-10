//go:build mcp

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func newParameterRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	svc := &extendedMockService{
		parameterSvc: &mockParameterStoreService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(svc, log)
}

func TestParameterGet_Success(t *testing.T) {
	registry := newParameterRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_get", map[string]interface{}{
		"path": "/app/config/db_host",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestParameterGet_MissingPath(t *testing.T) {
	registry := newParameterRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_get", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "path is required")
}

func TestParameterGet_ServiceError(t *testing.T) {
	svc := &extendedMockService{
		parameterSvc: &mockParameterStoreService{
			getFunc: func(ctx context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error) {
				return nil, &errors.MessageError{Message: "parameter not found"}
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_get", map[string]interface{}{
		"path": "/missing/path",
	})
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestParameterSet_Success(t *testing.T) {
	registry := newParameterRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "basic set",
			args: map[string]interface{}{"path": "/app/config", "value": "test-value"},
		},
		{
			name: "with all options",
			args: map[string]interface{}{
				"path": "/app/secure", "value": "secret",
				"type": "string", "description": "test param",
				"kms_key_id": "key-1", "tags": `{"env":"prod"}`,
				"ttl_seconds": "3600", "audit_enabled": "true",
				"overwrite": "true",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_set", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestParameterSet_Errors(t *testing.T) {
	registry := newParameterRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{"missing path", map[string]interface{}{"value": "v"}, "path is required"},
		{"missing value", map[string]interface{}{"path": "/app"}, "value is required"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_set", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestParameterDelete_Success(t *testing.T) {
	registry := newParameterRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_delete", map[string]interface{}{
		"path": "/app/old_param",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestParameterDelete_MissingPath(t *testing.T) {
	registry := newParameterRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_delete", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestParameterList_Success(t *testing.T) {
	registry := newParameterRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{"basic list", map[string]interface{}{"prefix": "/app/"}},
		{"with options", map[string]interface{}{
			"prefix": "/app/", "recursive": "true",
			"with_decryption": "false", "with_values": "true",
			"limit": "50", "cursor": "abc123",
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_list", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestParameterList_MissingPrefix(t *testing.T) {
	registry := newParameterRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_list", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestParameterBatchGet_Success(t *testing.T) {
	registry := newParameterRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_batch_get", map[string]interface{}{
		"paths": `["/app/a","/app/b"]`,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestParameterBatchGet_Errors(t *testing.T) {
	registry := newParameterRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{"missing paths", map[string]interface{}{}, "paths is required"},
		{"invalid JSON", map[string]interface{}{"paths": "bad"}, "invalid paths"},
		{"empty array", map[string]interface{}{"paths": "[]"}, "cannot be empty"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_batch_get", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestParameterBatchSet_Success(t *testing.T) {
	registry := newParameterRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_batch_set", map[string]interface{}{
		"parameters": `[{"path":"/app/a","value":"v1"},{"path":"/app/b","value":"v2","type":"string"}]`,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestParameterBatchSet_Errors(t *testing.T) {
	registry := newParameterRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{"missing parameters", map[string]interface{}{}, "parameters is required"},
		{"invalid JSON", map[string]interface{}{"parameters": "bad"}, "invalid parameters"},
		{"empty array", map[string]interface{}{"parameters": "[]"}, "cannot be empty"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_batch_set", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestParameterBatchDelete_Success(t *testing.T) {
	registry := newParameterRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_batch_delete", map[string]interface{}{
		"paths": `["/app/a","/app/b"]`,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestParameterBatchDelete_Errors(t *testing.T) {
	registry := newParameterRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{"missing paths", map[string]interface{}{}, "paths is required"},
		{"invalid JSON", map[string]interface{}{"paths": "bad"}, "invalid paths"},
		{"empty array", map[string]interface{}{"paths": "[]"}, "cannot be empty"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_batch_delete", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestParameterSearch_Success(t *testing.T) {
	registry := newParameterRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_search", map[string]interface{}{
		"tags": `{"env":"prod"}`,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestParameterSearch_Errors(t *testing.T) {
	registry := newParameterRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{"missing tags", map[string]interface{}{}, "tags is required"},
		{"invalid JSON", map[string]interface{}{"tags": "bad"}, "invalid tags"},
		{"empty tags", map[string]interface{}{"tags": "{}"}, "cannot be empty"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_search", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestParameterServiceNotAvailable(t *testing.T) {
	svc := &extendedMockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_parameter_get", map[string]interface{}{
		"path": "/app/test",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not configured")
}

func TestIsParameterTool(t *testing.T) {
	paramTools := []string{
		"quicbaas_parameter_get", "quicbaas_parameter_set",
		"quicbaas_parameter_delete", "quicbaas_parameter_list",
		"quicbaas_parameter_batch_get", "quicbaas_parameter_batch_set",
		"quicbaas_parameter_batch_delete", "quicbaas_parameter_search",
	}
	for _, name := range paramTools {
		assert.True(t, isParameterTool(name))
	}
	assert.False(t, isParameterTool("quicbaas_put"))
	assert.False(t, isParameterTool(""))
}
