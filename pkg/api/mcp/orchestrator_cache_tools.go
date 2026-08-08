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

// Package mcp implements agent cache tool definitions and handlers for the
// Model Context Protocol. These tools enable storing and retrieving expensive
// analysis results in the KV store for sharing between agent sessions.
package mcp

import (
	"context"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// agentCacheEntityType is the KV entity type used for agent cache entries.
const agentCacheEntityType = "agent_cache"

// registerAgentCacheTools registers all agent cache tools with the registry.
func (r *ToolRegistry) registerAgentCacheTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_cache_put",
		Description: "Store an analysis result in the agent cache (KV store) with optional TTL for sharing between agent sessions",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "Project identifier for scoping cached data",
				},
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Cache key for the analysis result",
				},
				"value": map[string]interface{}{
					"type":        "string",
					"description": "Analysis result value to cache",
				},
				"ttl_seconds": map[string]interface{}{
					"type":        "integer",
					"description": "Time-to-live in seconds (optional, 0 means no expiry)",
				},
			},
			"required": []string{"project_id", "key", "value"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_cache_get",
		Description: "Retrieve a cached analysis result by project and key from the agent cache",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "Project identifier for scoping cached data",
				},
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Cache key to retrieve",
				},
			},
			"required": []string{"project_id", "key"},
		},
	})
}

// =============================================================================
// Agent Cache Tool Dispatch
// =============================================================================

// isAgentCacheTool checks if a tool name is an agent cache tool.
func isAgentCacheTool(name string) bool {
	switch name {
	case "quicbaas_orchestrator_cache_put",
		"quicbaas_orchestrator_cache_get":
		return true
	default:
		return false
	}
}

// executeAgentCacheTool dispatches agent cache tool execution to the appropriate handler.
func (r *ToolRegistry) executeAgentCacheTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "quicbaas_orchestrator_cache_put":
		return r.executeAgentCachePut(ctx, args)
	case "quicbaas_orchestrator_cache_get":
		return r.executeAgentCacheGet(ctx, args)
	default:
		return nil, api.NewUnknownToolError(name)
	}
}

// =============================================================================
// Agent Cache Tool Execution Handlers
// =============================================================================

// executeAgentCachePut stores an analysis result in the KV store using a
// composite key derived from the project ID and cache key.
func (r *ToolRegistry) executeAgentCachePut(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	projectID := getString(args, "project_id")
	if projectID == "" {
		return nil, api.NewRequiredFieldError("project_id")
	}

	key := getString(args, "key")
	if key == "" {
		return nil, api.NewRequiredFieldError("key")
	}

	value := getString(args, "value")
	if value == "" {
		return nil, api.NewRequiredFieldError("value")
	}

	compositeKey := buildAgentCacheKey(projectID, key)

	resp, err := r.service.Put(ctx, &service.PutRequest{
		EntityType: agentCacheEntityType,
		ID:         compositeKeyToID(compositeKey),
		Data:       []byte(value),
	})
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"project_id": projectID,
		"key":        key,
		"stored":     true,
		"version":    resp.Version,
		"timestamp":  time.Now().Format(time.RFC3339Nano),
	})
}

// executeAgentCacheGet retrieves a cached analysis result from the KV store
// using the composite key derived from project ID and cache key.
func (r *ToolRegistry) executeAgentCacheGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	projectID := getString(args, "project_id")
	if projectID == "" {
		return nil, api.NewRequiredFieldError("project_id")
	}

	key := getString(args, "key")
	if key == "" {
		return nil, api.NewRequiredFieldError("key")
	}

	compositeKey := buildAgentCacheKey(projectID, key)

	resp, err := r.service.Get(ctx, &service.GetRequest{
		EntityType: agentCacheEntityType,
		ID:         compositeKeyToID(compositeKey),
	})
	if err != nil {
		return nil, err
	}

	if !resp.Found {
		return marshalResult(map[string]interface{}{
			"project_id": projectID,
			"key":        key,
			"value":      "",
			"found":      false,
			"timestamp":  time.Now().Format(time.RFC3339Nano),
		})
	}

	return marshalResult(map[string]interface{}{
		"project_id": projectID,
		"key":        key,
		"value":      string(resp.Data),
		"found":      true,
		"version":    resp.Version,
		"timestamp":  time.Now().Format(time.RFC3339Nano),
	})
}

// =============================================================================
// Helper Functions
// =============================================================================

// buildAgentCacheKey creates a composite key from project ID and cache key.
func buildAgentCacheKey(projectID, key string) string {
	return projectID + ":" + key
}

// compositeKeyToID converts a composite string key to a uint64 ID using
// FNV-1a hashing for deterministic, uniform distribution.
func compositeKeyToID(key string) uint64 {
	const (
		fnvOffset uint64 = 14695981039346656037
		fnvPrime  uint64 = 1099511628211
	)

	h := fnvOffset
	for i := 0; i < len(key); i++ {
		h ^= uint64(key[i])
		h *= fnvPrime
	}
	return h
}
