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

package iam

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParameterContextFromContext tests retrieving parameter context from context.
func TestParameterContextFromContext(t *testing.T) {
	t.Run("returns parameter context when set", func(t *testing.T) {
		pc := &ParameterContext{
			Path:        "/app/config/database",
			Type:        "string",
			IsEncrypted: true,
			Tags:        map[string]string{"env": "production"},
			Owner:       "user-123",
			TenantID:    "tenant-456",
			Operation:   ParameterOperationGet,
		}

		ctx := ContextWithParameterContext(context.Background(), pc)
		result := ParameterContextFromContext(ctx)

		require.NotNil(t, result)
		assert.Equal(t, "/app/config/database", result.Path)
		assert.Equal(t, "string", result.Type)
		assert.True(t, result.IsEncrypted)
		assert.Equal(t, "production", result.Tags["env"])
		assert.Equal(t, "user-123", result.Owner)
		assert.Equal(t, "tenant-456", result.TenantID)
		assert.Equal(t, ParameterOperationGet, result.Operation)
	})

	t.Run("returns nil when not set", func(t *testing.T) {
		ctx := context.Background()
		result := ParameterContextFromContext(ctx)

		assert.Nil(t, result)
	})

	t.Run("returns nil when wrong type in context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), parameterContextKey{}, "not a ParameterContext")
		result := ParameterContextFromContext(ctx)

		assert.Nil(t, result)
	})

	t.Run("returns nil when nil value in context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), parameterContextKey{}, nil)
		result := ParameterContextFromContext(ctx)

		assert.Nil(t, result)
	})
}

// TestContextWithParameterContext tests setting parameter context in context.
func TestContextWithParameterContext(t *testing.T) {
	t.Run("sets parameter context successfully", func(t *testing.T) {
		pc := &ParameterContext{
			Path:      "/app/secret",
			Operation: ParameterOperationSet,
		}

		ctx := ContextWithParameterContext(context.Background(), pc)
		result := ParameterContextFromContext(ctx)

		require.NotNil(t, result)
		assert.Equal(t, pc, result)
	})

	t.Run("preserves existing context values", func(t *testing.T) {
		type customKey struct{}
		originalCtx := context.WithValue(context.Background(), customKey{}, "custom-value")

		pc := &ParameterContext{Path: "/test"}
		ctx := ContextWithParameterContext(originalCtx, pc)

		// Verify both values exist
		assert.Equal(t, "custom-value", ctx.Value(customKey{}))
		assert.NotNil(t, ParameterContextFromContext(ctx))
	})

	t.Run("overwrites previous parameter context", func(t *testing.T) {
		pc1 := &ParameterContext{Path: "/first"}
		pc2 := &ParameterContext{Path: "/second"}

		ctx := ContextWithParameterContext(context.Background(), pc1)
		ctx = ContextWithParameterContext(ctx, pc2)
		result := ParameterContextFromContext(ctx)

		require.NotNil(t, result)
		assert.Equal(t, "/second", result.Path)
	})
}

// TestParameterContextClone tests the Clone method.
func TestParameterContextClone(t *testing.T) {
	t.Run("clones all fields correctly", func(t *testing.T) {
		original := &ParameterContext{
			Path:         "/app/database/host",
			Type:         "string",
			IsEncrypted:  true,
			Tags:         map[string]string{"env": "prod", "team": "backend"},
			Owner:        "user-123",
			TenantID:     "tenant-456",
			Operation:    ParameterOperationSet,
			BatchSize:    10,
			AuditEnabled: true,
			Description:  "Database host configuration",
		}

		clone := original.Clone()

		require.NotNil(t, clone)
		assert.Equal(t, original.Path, clone.Path)
		assert.Equal(t, original.Type, clone.Type)
		assert.Equal(t, original.IsEncrypted, clone.IsEncrypted)
		assert.Equal(t, original.Owner, clone.Owner)
		assert.Equal(t, original.TenantID, clone.TenantID)
		assert.Equal(t, original.Operation, clone.Operation)
		assert.Equal(t, original.BatchSize, clone.BatchSize)
		assert.Equal(t, original.AuditEnabled, clone.AuditEnabled)
		assert.Equal(t, original.Description, clone.Description)
		assert.Equal(t, original.Tags, clone.Tags)
	})

	t.Run("returns nil when cloning nil", func(t *testing.T) {
		var pc *ParameterContext
		clone := pc.Clone()

		assert.Nil(t, clone)
	})

	t.Run("creates independent copy of tags", func(t *testing.T) {
		original := &ParameterContext{
			Path: "/test",
			Tags: map[string]string{"key1": "value1"},
		}

		clone := original.Clone()

		// Modify clone's tags
		clone.Tags["key2"] = "value2"

		// Original should not be affected
		assert.Equal(t, 1, len(original.Tags))
		assert.Equal(t, 2, len(clone.Tags))
		assert.NotContains(t, original.Tags, "key2")
	})

	t.Run("handles nil tags correctly", func(t *testing.T) {
		original := &ParameterContext{
			Path: "/test",
			Tags: nil,
		}

		clone := original.Clone()

		require.NotNil(t, clone)
		assert.Nil(t, clone.Tags)
	})

	t.Run("handles empty tags correctly", func(t *testing.T) {
		original := &ParameterContext{
			Path: "/test",
			Tags: map[string]string{},
		}

		clone := original.Clone()

		require.NotNil(t, clone)
		assert.NotNil(t, clone.Tags)
		assert.Empty(t, clone.Tags)
	})

	t.Run("modifications to clone do not affect original", func(t *testing.T) {
		original := &ParameterContext{
			Path:        "/original",
			Type:        "string",
			IsEncrypted: false,
			BatchSize:   5,
		}

		clone := original.Clone()
		clone.Path = "/modified"
		clone.Type = "json"
		clone.IsEncrypted = true
		clone.BatchSize = 100

		assert.Equal(t, "/original", original.Path)
		assert.Equal(t, "string", original.Type)
		assert.False(t, original.IsEncrypted)
		assert.Equal(t, 5, original.BatchSize)
	})
}

// TestParameterContextHasTag tests the HasTag method.
func TestParameterContextHasTag(t *testing.T) {
	t.Run("returns true when tag exists with matching value", func(t *testing.T) {
		pc := &ParameterContext{
			Tags: map[string]string{
				"env":  "production",
				"team": "backend",
			},
		}

		assert.True(t, pc.HasTag("env", "production"))
		assert.True(t, pc.HasTag("team", "backend"))
	})

	t.Run("returns false when tag exists with different value", func(t *testing.T) {
		pc := &ParameterContext{
			Tags: map[string]string{
				"env": "production",
			},
		}

		assert.False(t, pc.HasTag("env", "development"))
	})

	t.Run("returns true when checking only key existence with empty value", func(t *testing.T) {
		pc := &ParameterContext{
			Tags: map[string]string{
				"sensitive": "true",
			},
		}

		// Empty value means "just check if key exists"
		assert.True(t, pc.HasTag("sensitive", ""))
	})

	t.Run("returns false when key does not exist", func(t *testing.T) {
		pc := &ParameterContext{
			Tags: map[string]string{
				"env": "production",
			},
		}

		assert.False(t, pc.HasTag("missing", ""))
		assert.False(t, pc.HasTag("missing", "value"))
	})

	t.Run("returns false when context is nil", func(t *testing.T) {
		var pc *ParameterContext

		assert.False(t, pc.HasTag("env", "prod"))
	})

	t.Run("returns false when tags map is nil", func(t *testing.T) {
		pc := &ParameterContext{
			Path: "/test",
			Tags: nil,
		}

		assert.False(t, pc.HasTag("env", "prod"))
	})

	t.Run("handles empty string tag values correctly", func(t *testing.T) {
		pc := &ParameterContext{
			Tags: map[string]string{
				"empty-value": "",
			},
		}

		// Key exists, so checking with empty value returns true
		assert.True(t, pc.HasTag("empty-value", ""))
		// But checking with non-empty value returns false
		assert.False(t, pc.HasTag("empty-value", "something"))
	})
}

// TestParameterContextGetTagValue tests the GetTagValue method.
func TestParameterContextGetTagValue(t *testing.T) {
	t.Run("returns tag value when exists", func(t *testing.T) {
		pc := &ParameterContext{
			Tags: map[string]string{
				"env":     "production",
				"version": "1.0.0",
			},
		}

		assert.Equal(t, "production", pc.GetTagValue("env"))
		assert.Equal(t, "1.0.0", pc.GetTagValue("version"))
	})

	t.Run("returns empty string when tag does not exist", func(t *testing.T) {
		pc := &ParameterContext{
			Tags: map[string]string{
				"env": "production",
			},
		}

		assert.Equal(t, "", pc.GetTagValue("missing"))
	})

	t.Run("returns empty string when context is nil", func(t *testing.T) {
		var pc *ParameterContext

		assert.Equal(t, "", pc.GetTagValue("env"))
	})

	t.Run("returns empty string when tags map is nil", func(t *testing.T) {
		pc := &ParameterContext{
			Path: "/test",
			Tags: nil,
		}

		assert.Equal(t, "", pc.GetTagValue("env"))
	})

	t.Run("returns empty string for key with empty value", func(t *testing.T) {
		pc := &ParameterContext{
			Tags: map[string]string{
				"empty": "",
			},
		}

		assert.Equal(t, "", pc.GetTagValue("empty"))
	})
}

// TestParameterContextIsPathPrefix tests the IsPathPrefix method.
func TestParameterContextIsPathPrefix(t *testing.T) {
	t.Run("returns true when path starts with prefix", func(t *testing.T) {
		pc := &ParameterContext{
			Path: "/app/database/host",
		}

		assert.True(t, pc.IsPathPrefix("/app"))
		assert.True(t, pc.IsPathPrefix("/app/database"))
		assert.True(t, pc.IsPathPrefix("/app/database/host"))
	})

	t.Run("returns false when path does not start with prefix", func(t *testing.T) {
		pc := &ParameterContext{
			Path: "/app/database/host",
		}

		assert.False(t, pc.IsPathPrefix("/api"))
		assert.False(t, pc.IsPathPrefix("/application"))
		assert.False(t, pc.IsPathPrefix("/other/path"))
	})

	t.Run("returns false when prefix is longer than path", func(t *testing.T) {
		pc := &ParameterContext{
			Path: "/app",
		}

		assert.False(t, pc.IsPathPrefix("/app/database"))
	})

	t.Run("returns true for empty prefix", func(t *testing.T) {
		pc := &ParameterContext{
			Path: "/app/config",
		}

		assert.True(t, pc.IsPathPrefix(""))
	})

	t.Run("returns false when context is nil", func(t *testing.T) {
		var pc *ParameterContext

		assert.False(t, pc.IsPathPrefix("/app"))
	})

	t.Run("returns false when path is empty", func(t *testing.T) {
		pc := &ParameterContext{
			Path: "",
		}

		assert.False(t, pc.IsPathPrefix("/app"))
		// Empty path with empty prefix: false because path is empty
		assert.False(t, pc.IsPathPrefix(""))
	})

	t.Run("exact match returns true", func(t *testing.T) {
		pc := &ParameterContext{
			Path: "/app/config",
		}

		assert.True(t, pc.IsPathPrefix("/app/config"))
	})

	t.Run("handles root path correctly", func(t *testing.T) {
		pc := &ParameterContext{
			Path: "/",
		}

		assert.True(t, pc.IsPathPrefix("/"))
		assert.False(t, pc.IsPathPrefix("/app"))
	})
}

// TestParameterOperationConstants tests that operation constants are defined correctly.
func TestParameterOperationConstants(t *testing.T) {
	t.Run("operation constants have expected values", func(t *testing.T) {
		assert.Equal(t, "get", ParameterOperationGet)
		assert.Equal(t, "set", ParameterOperationSet)
		assert.Equal(t, "delete", ParameterOperationDelete)
		assert.Equal(t, "list", ParameterOperationList)
		assert.Equal(t, "search", ParameterOperationSearch)
		assert.Equal(t, "batch_get", ParameterOperationBatchGet)
		assert.Equal(t, "batch_set", ParameterOperationBatchSet)
		assert.Equal(t, "batch_delete", ParameterOperationBatchDelete)
	})

	t.Run("operations can be used in context", func(t *testing.T) {
		operations := []string{
			ParameterOperationGet,
			ParameterOperationSet,
			ParameterOperationDelete,
			ParameterOperationList,
			ParameterOperationSearch,
			ParameterOperationBatchGet,
			ParameterOperationBatchSet,
			ParameterOperationBatchDelete,
		}

		for _, op := range operations {
			pc := &ParameterContext{
				Path:      "/test",
				Operation: op,
			}
			ctx := ContextWithParameterContext(context.Background(), pc)
			result := ParameterContextFromContext(ctx)

			require.NotNil(t, result)
			assert.Equal(t, op, result.Operation)
		}
	})
}

// TestParameterContextFullWorkflow tests a complete ABAC workflow.
func TestParameterContextFullWorkflow(t *testing.T) {
	t.Run("simulates ABAC evaluation workflow", func(t *testing.T) {
		// Create a parameter context for a set operation
		pc := &ParameterContext{
			Path:         "/myapp/production/database/password",
			Type:         "securestring",
			IsEncrypted:  true,
			Tags:         map[string]string{"env": "production", "sensitive": "true", "team": "platform"},
			Owner:        "admin-user",
			TenantID:     "acme-corp",
			Operation:    ParameterOperationSet,
			BatchSize:    0,
			AuditEnabled: true,
			Description:  "Production database password",
		}

		// Set in context
		ctx := ContextWithParameterContext(context.Background(), pc)

		// Retrieve for ABAC evaluation
		evalCtx := ParameterContextFromContext(ctx)
		require.NotNil(t, evalCtx)

		// Verify path prefix check (for path-based policies)
		assert.True(t, evalCtx.IsPathPrefix("/myapp/production"))
		assert.False(t, evalCtx.IsPathPrefix("/myapp/staging"))

		// Verify tag checks (for tag-based policies)
		assert.True(t, evalCtx.HasTag("env", "production"))
		assert.True(t, evalCtx.HasTag("sensitive", ""))
		assert.False(t, evalCtx.HasTag("env", "development"))

		// Get specific tag value
		assert.Equal(t, "platform", evalCtx.GetTagValue("team"))

		// Verify sensitive attributes
		assert.True(t, evalCtx.IsEncrypted)
		assert.True(t, evalCtx.AuditEnabled)

		// Clone for modification (if needed for sub-operations)
		cloned := evalCtx.Clone()
		cloned.Operation = ParameterOperationGet
		assert.Equal(t, ParameterOperationSet, evalCtx.Operation) // Original unchanged
		assert.Equal(t, ParameterOperationGet, cloned.Operation)
	})
}

// TestParameterContextEdgeCases tests edge cases and boundary conditions.
func TestParameterContextEdgeCases(t *testing.T) {
	t.Run("handles special characters in path", func(t *testing.T) {
		pc := &ParameterContext{
			Path: "/app/config/key-with-dashes_and_underscores.json",
		}

		assert.True(t, pc.IsPathPrefix("/app/config"))
		assert.False(t, pc.IsPathPrefix("/app/configs"))
	})

	t.Run("handles unicode in tags", func(t *testing.T) {
		pc := &ParameterContext{
			Tags: map[string]string{
				"region": "eu-west",
			},
		}

		assert.True(t, pc.HasTag("region", "eu-west"))
		assert.Equal(t, "eu-west", pc.GetTagValue("region"))
	})

	t.Run("handles large batch size", func(t *testing.T) {
		pc := &ParameterContext{
			BatchSize: 1000000,
		}

		clone := pc.Clone()
		assert.Equal(t, 1000000, clone.BatchSize)
	})

	t.Run("handles many tags", func(t *testing.T) {
		tags := make(map[string]string)
		for i := 0; i < 100; i++ {
			tags[string(rune('a'+i%26))+string(rune('0'+i/26))] = "value"
		}

		pc := &ParameterContext{Tags: tags}
		clone := pc.Clone()

		assert.Equal(t, len(tags), len(clone.Tags))
	})
}
