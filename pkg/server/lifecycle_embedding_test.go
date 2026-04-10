//go:build raft && rag && vector && !integration

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

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// buildEmbeddingConfig: OllamaConfig with zero-value fields that trigger defaults
// Covers lifecycle_embedding.go:117-149 (Ollama fallback with default timeout, maxRetries, retryBackoff)
// =============================================================================

func TestFTFinal_BuildEmbeddingConfig_OllamaZeroTimeoutDefaults(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.Ollama.Enabled = true
	cfg.Ollama.BaseURL = "http://localhost:11434"
	// Leave Timeout, MaxRetries, RetryBackoff at zero to trigger defaults

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	result := srv.buildEmbeddingConfig()
	assert.NotNil(t, result)
	assert.Equal(t, "ollama", result.Provider)
	assert.Equal(t, 120, result.Timeout)
	assert.Equal(t, 3, result.MaxRetries)
	assert.Equal(t, 1000, result.RetryBackoffMS)
}
