//go:build iam

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
)

// Context key type for parameter context.
type parameterContextKey struct{}

// ParameterContext contains parameter-specific context for ABAC evaluation.
// This context is set by parameter store handlers and used by the ABAC provider
// to evaluate attribute-based policies.
type ParameterContext struct {
	// Path is the hierarchical parameter path (e.g., "/app/database/host").
	Path string

	// Type is the parameter value type (string, number, json, binary).
	Type string

	// IsEncrypted indicates if the parameter value is encrypted.
	IsEncrypted bool

	// Tags contains the parameter's tags for tag-based policy evaluation.
	Tags map[string]string

	// Owner is the user ID who owns this parameter (if applicable).
	Owner string

	// TenantID is the tenant that owns this parameter.
	TenantID string

	// Operation is the operation being performed (get, set, delete, list, search).
	Operation string

	// BatchSize is the number of parameters in batch operations.
	BatchSize int

	// AuditEnabled indicates if audit logging is enabled for this parameter.
	AuditEnabled bool

	// Description is the parameter description (if available).
	Description string
}

// ParameterContextFromContext extracts the parameter context from the context.
// Returns nil if not set.
func ParameterContextFromContext(ctx context.Context) *ParameterContext {
	if v := ctx.Value(parameterContextKey{}); v != nil {
		if pc, ok := v.(*ParameterContext); ok {
			return pc
		}
	}
	return nil
}

// ContextWithParameterContext returns a new context with the parameter context set.
func ContextWithParameterContext(ctx context.Context, pc *ParameterContext) context.Context {
	return context.WithValue(ctx, parameterContextKey{}, pc)
}

// Clone creates a deep copy of the parameter context.
func (pc *ParameterContext) Clone() *ParameterContext {
	if pc == nil {
		return nil
	}

	clone := *pc

	// Deep copy Tags
	if pc.Tags != nil {
		clone.Tags = make(map[string]string, len(pc.Tags))
		for k, v := range pc.Tags {
			clone.Tags[k] = v
		}
	}

	return &clone
}

// HasTag returns true if the parameter has the specified tag with the given value.
// If value is empty, returns true if the tag key exists regardless of value.
func (pc *ParameterContext) HasTag(key, value string) bool {
	if pc == nil || pc.Tags == nil {
		return false
	}
	tagValue, exists := pc.Tags[key]
	if !exists {
		return false
	}
	if value == "" {
		return true // Key exists, no value check required
	}
	return tagValue == value
}

// GetTagValue returns the value of a tag, or empty string if not found.
func (pc *ParameterContext) GetTagValue(key string) string {
	if pc == nil || pc.Tags == nil {
		return ""
	}
	return pc.Tags[key]
}

// IsPathPrefix returns true if the parameter path starts with the given prefix.
func (pc *ParameterContext) IsPathPrefix(prefix string) bool {
	if pc == nil || pc.Path == "" {
		return false
	}
	if len(pc.Path) < len(prefix) {
		return false
	}
	return pc.Path[:len(prefix)] == prefix
}

// ParameterOperation constants for ABAC policy evaluation.
const (
	// ParameterOperationGet represents a get operation.
	ParameterOperationGet = "get"

	// ParameterOperationSet represents a set/create/update operation.
	ParameterOperationSet = "set"

	// ParameterOperationDelete represents a delete operation.
	ParameterOperationDelete = "delete"

	// ParameterOperationList represents a list operation.
	ParameterOperationList = "list"

	// ParameterOperationSearch represents a search by tags operation.
	ParameterOperationSearch = "search"

	// ParameterOperationBatchGet represents a batch get operation.
	ParameterOperationBatchGet = "batch_get"

	// ParameterOperationBatchSet represents a batch set operation.
	ParameterOperationBatchSet = "batch_set"

	// ParameterOperationBatchDelete represents a batch delete operation.
	ParameterOperationBatchDelete = "batch_delete"
)
