//go:build mcp

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

func newBatchExecuteRegistry(t *testing.T, fn func(context.Context, *service.BatchExecuteRequest) (*service.BatchExecuteResponse, error)) *ToolRegistry {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)
	return NewToolRegistry(&mocks.MockQuicBaaS{BatchExecuteFunc: fn}, log)
}

func TestBatchExecuteTool_MixedOperations(t *testing.T) {
	var got *service.BatchExecuteRequest
	registry := newBatchExecuteRegistry(t, func(_ context.Context, req *service.BatchExecuteRequest) (*service.BatchExecuteResponse, error) {
		got = req
		return &service.BatchExecuteResponse{Results: []*service.BatchExecuteResult{
			{Succeeded: true}, {Succeeded: true},
		}}, nil
	})

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_batch_execute", map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{"type": "PUT", "entity_type": "users", "key": "alice", "value": "dmFsdWUx"},
			map[string]interface{}{"type": "DELETE", "entity_type": "users", "key": "bob"},
		},
		"shardID": float64(3),
	})
	require.NoError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, uint64(3), got.ShardID)
	require.Len(t, got.Operations, 2)
	assert.Equal(t, &service.BatchExecuteOperation{
		Type: service.BatchExecutePut, EntityType: "users", Key: "alice", Value: []byte("value1"),
	}, got.Operations[0])
	assert.Equal(t, &service.BatchExecuteOperation{
		Type: service.BatchExecuteDelete, EntityType: "users", Key: "bob",
	}, got.Operations[1])

	assert.Equal(t, map[string]interface{}{
		"success": true,
		"results": []interface{}{
			map[string]interface{}{"success": true},
			map[string]interface{}{"success": true},
		},
	}, result)
}

func TestBatchExecuteTool_InvalidArguments(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{"missing operations", map[string]interface{}{}},
		{"empty operations", map[string]interface{}{"operations": []interface{}{}}},
		{"operation not an object", map[string]interface{}{"operations": []interface{}{"PUT"}}},
		{"bad type", map[string]interface{}{"operations": []interface{}{
			map[string]interface{}{"type": "UPSERT", "entity_type": "users", "key": "k"},
		}}},
		{"lowercase type", map[string]interface{}{"operations": []interface{}{
			map[string]interface{}{"type": "put", "entity_type": "users", "key": "k"},
		}}},
		{"missing entity type", map[string]interface{}{"operations": []interface{}{
			map[string]interface{}{"type": "PUT", "key": "k"},
		}}},
		{"missing key", map[string]interface{}{"operations": []interface{}{
			map[string]interface{}{"type": "DELETE", "entity_type": "users"},
		}}},
		{"bad base64", map[string]interface{}{"operations": []interface{}{
			map[string]interface{}{"type": "PUT", "entity_type": "users", "key": "k", "value": "not base64!"},
		}}},
		{"number value", map[string]interface{}{"operations": []interface{}{
			map[string]interface{}{"type": "PUT", "entity_type": "users", "key": "k", "value": 123},
		}}},
		{"bool value", map[string]interface{}{"operations": []interface{}{
			map[string]interface{}{"type": "PUT", "entity_type": "users", "key": "k", "value": true},
		}}},
		{"object value", map[string]interface{}{"operations": []interface{}{
			map[string]interface{}{"type": "PUT", "entity_type": "users", "key": "k", "value": map[string]interface{}{}},
		}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			registry := newBatchExecuteRegistry(t, func(context.Context, *service.BatchExecuteRequest) (*service.BatchExecuteResponse, error) {
				called = true
				return &service.BatchExecuteResponse{}, nil
			})

			result, err := registry.ExecuteTool(context.Background(), "quicbaas_batch_execute", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.False(t, called, "service must not be reached on invalid arguments")
		})
	}
}

// TestBatchExecuteTool_EmptyOrOmittedValueIsAllowed: an explicitly empty or
// omitted value still reaches the service, as an empty write.
func TestBatchExecuteTool_EmptyOrOmittedValueIsAllowed(t *testing.T) {
	var got *service.BatchExecuteRequest
	registry := newBatchExecuteRegistry(t, func(_ context.Context, req *service.BatchExecuteRequest) (*service.BatchExecuteResponse, error) {
		got = req
		return &service.BatchExecuteResponse{Results: []*service.BatchExecuteResult{
			{Succeeded: true}, {Succeeded: true},
		}}, nil
	})

	_, err := registry.ExecuteTool(context.Background(), "quicbaas_batch_execute", map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{"type": "PUT", "entity_type": "users", "key": "empty", "value": ""},
			map[string]interface{}{"type": "PUT", "entity_type": "users", "key": "omitted"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got.Operations, 2)
	for _, op := range got.Operations {
		assert.Nil(t, op.Value)
	}
}

func TestBatchExecuteTool_ServiceError(t *testing.T) {
	registry := newBatchExecuteRegistry(t, func(context.Context, *service.BatchExecuteRequest) (*service.BatchExecuteResponse, error) {
		return nil, errors.New(errors.ErrInternal, "BatchExecute", "batch commit failed")
	})

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_batch_execute", map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{"type": "DELETE", "entity_type": "users", "key": "bob"},
		},
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "batch commit failed")
}
