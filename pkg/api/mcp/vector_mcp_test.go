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

func TestVectorMCPServerCreation(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)

	t.Run("with logger", func(t *testing.T) {
		server := NewMCPServer(nil, "localhost:8080", log)
		assert.NotNil(t, server)
		assert.NotNil(t, server.handlers)
		assert.NotNil(t, server.tools)
	})

	t.Run("without logger", func(t *testing.T) {
		server := NewMCPServer(nil, "localhost:8080", nil)
		assert.NotNil(t, server)
		assert.NotNil(t, server.logger)
	})
}

func TestVectorMCPServerExecuteTool(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)

	server := NewMCPServer(nil, "localhost:8080", log)
	ctx := context.Background()

	t.Run("unknown tool", func(t *testing.T) {
		resp, err := server.ExecuteTool(ctx, "unknown_tool", map[string]interface{}{})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, resp.IsError)
		assert.Contains(t, resp.Content[0].Text, "Tool not found")
	})
}

func TestVectorMCPServerGetTools(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)

	server := NewMCPServer(nil, "localhost:8080", log)

	tools := server.GetTools()
	assert.NotNil(t, tools)
	assert.Greater(t, len(tools), 0)
}
