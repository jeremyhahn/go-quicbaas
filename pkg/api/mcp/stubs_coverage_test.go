//go:build mcp && !iam && !rag

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

func newStubRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(&mockService{}, log)
}

// IAM disabled: classifier returns false and execute is a no-op.
func TestIAMStub_Disabled(t *testing.T) {
	reg := newStubRegistry(t)
	assert.False(t, isIAMTool("quicbaas_iam_user_create"))

	// registerIAMTools is a no-op; must not panic.
	reg.registerIAMTools()

	result, err := reg.executeIAMTool(context.Background(), "quicbaas_iam_user_create", map[string]interface{}{})
	require.NoError(t, err)
	assert.Nil(t, result)
}

// RAG index tools disabled.
func TestRAGIndexStub_Disabled(t *testing.T) {
	reg := newStubRegistry(t)
	assert.False(t, isRAGIndexTool("quicbaas_rag_index_create"))

	reg.registerRAGIndexTools()

	result, err := reg.executeRAGIndexTool(context.Background(), "quicbaas_rag_index_create", map[string]interface{}{})
	require.NoError(t, err)
	assert.Nil(t, result)
}

// RAG tools disabled: classifier false, register no-op, execute errors.
func TestRAGStub_Disabled(t *testing.T) {
	reg := newStubRegistry(t)
	assert.False(t, isRAGTool("quicbaas_rag_query"))

	reg.registerRAGTools()

	_, err := reg.executeRAGTool(context.Background(), "quicbaas_rag_query", map[string]interface{}{})
	require.Error(t, err)
}
