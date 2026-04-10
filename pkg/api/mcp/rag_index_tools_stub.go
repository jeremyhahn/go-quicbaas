//go:build mcp && !rag

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

// Package mcp provides stub implementations for RAG indexing tools when the
// rag build tag is not enabled.
package mcp

import "context"

// registerRAGIndexTools is a no-op when RAG is disabled.
func (r *ToolRegistry) registerRAGIndexTools() {}

// isRAGIndexTool returns false when RAG is disabled.
func isRAGIndexTool(_ string) bool { return false }

// executeRAGIndexTool is unreachable when RAG is disabled.
func (r *ToolRegistry) executeRAGIndexTool(_ context.Context, _ string, _ map[string]interface{}) (interface{}, error) {
	return nil, nil
}
