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

package mcp

import (
	"context"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// registerRAGTools is a no-op when RAG is not enabled.
func (r *ToolRegistry) registerRAGTools() {
	// RAG not enabled - no tools to register
}

// executeRAGTool returns an error when RAG is not enabled.
func (r *ToolRegistry) executeRAGTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	return nil, api.NewNotImplementedError("RAG not enabled - build with -tags rag")
}

// isRAGTool returns false when RAG is not enabled.
func isRAGTool(name string) bool {
	return false
}
