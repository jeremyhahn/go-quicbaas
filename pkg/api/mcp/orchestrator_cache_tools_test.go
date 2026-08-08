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

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func newAgentCacheRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	svc := &extendedMockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(svc, log)
}

func TestRegisterAgentCacheTools(t *testing.T) {
	registry := newAgentCacheRegistry(t)
	tools := registry.ListTools()

	found := 0
	expected := map[string]bool{
		"quicbaas_orchestrator_cache_put": false,
		"quicbaas_orchestrator_cache_get": false,
	}

	for _, tool := range tools {
		if _, ok := expected[tool.Name]; ok {
			expected[tool.Name] = true
			found++
		}
	}

	assert.Equal(t, 2, found, "expected 2 agent cache tools to be registered")
	for name, registered := range expected {
		assert.True(t, registered, "tool %s should be registered", name)
	}
}

func TestIsAgentCacheTool(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		expected bool
	}{
		{
			name:     "cache put tool",
			toolName: "quicbaas_orchestrator_cache_put",
			expected: true,
		},
		{
			name:     "cache get tool",
			toolName: "quicbaas_orchestrator_cache_get",
			expected: true,
		},
		{
			name:     "unknown tool",
			toolName: "quicbaas_unknown_tool",
			expected: false,
		},
		{
			name:     "empty string",
			toolName: "",
			expected: false,
		},
		{
			name:     "scheduler tool is not cache tool",
			toolName: "quicbaas_scheduler_create_job",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isAgentCacheTool(tc.toolName)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestExecuteAgentCacheTool_UnknownTool(t *testing.T) {
	registry := newAgentCacheRegistry(t)
	ctx := context.Background()

	result, err := registry.executeAgentCacheTool(ctx, "quicbaas_nonexistent", map[string]interface{}{})
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quicbaas_nonexistent")
}

func TestExecuteAgentCachePut_MissingProjectID(t *testing.T) {
	registry := newAgentCacheRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_orchestrator_cache_put", map[string]interface{}{
		"key":   "analysis-key",
		"value": "some result",
	})
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project_id")
}

func TestExecuteAgentCachePut_MissingKey(t *testing.T) {
	registry := newAgentCacheRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_orchestrator_cache_put", map[string]interface{}{
		"project_id": "proj-123",
		"value":      "some result",
	})
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key")
}

func TestExecuteAgentCachePut_MissingValue(t *testing.T) {
	registry := newAgentCacheRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_orchestrator_cache_put", map[string]interface{}{
		"project_id": "proj-123",
		"key":        "analysis-key",
	})
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "value")
}

func TestExecuteAgentCacheGet_MissingProjectID(t *testing.T) {
	registry := newAgentCacheRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_orchestrator_cache_get", map[string]interface{}{
		"key": "analysis-key",
	})
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project_id")
}

func TestExecuteAgentCacheGet_MissingKey(t *testing.T) {
	registry := newAgentCacheRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_orchestrator_cache_get", map[string]interface{}{
		"project_id": "proj-123",
	})
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key")
}

func TestBuildAgentCacheKey(t *testing.T) {
	key := buildAgentCacheKey("proj-123", "arch-analysis")
	assert.Equal(t, "proj-123:arch-analysis", key)
}

func TestCompositeKeyToID(t *testing.T) {
	// Deterministic: same inputs produce same output
	id1 := compositeKeyToID("proj-123:analysis")
	id2 := compositeKeyToID("proj-123:analysis")
	assert.Equal(t, id1, id2, "same keys should produce same ID")

	// Different keys produce different IDs
	id3 := compositeKeyToID("proj-456:analysis")
	assert.NotEqual(t, id1, id3, "different keys should produce different IDs")

	// Non-zero for non-empty input
	assert.NotZero(t, id1, "ID should be non-zero for non-empty input")
}
