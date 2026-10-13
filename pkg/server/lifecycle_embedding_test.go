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

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
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

	result, buildErr := srv.buildEmbeddingConfig()
	require.NoError(t, buildErr)
	assert.NotNil(t, result)
	assert.Equal(t, "ollama", result.Provider)
	assert.Equal(t, 120, result.Timeout)
	assert.Equal(t, 3, result.MaxRetries)
	assert.Equal(t, 1000, result.RetryBackoffMS)
}

// TestStopEmbedding_NilManagerIsNoOp covers the nil-manager early-return of
// stopEmbedding: stopping when no manager was ever created must succeed.
func TestStopEmbedding_NilManagerIsNoOp(t *testing.T) {
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	srv.embeddingManager = nil
	require.NoError(t, srv.stopEmbedding(), "stopping a nil embedding manager must be a no-op")
}

// TestBuildEmbeddingConfig_NoRAGUsesConfiguredOllamaBaseURL pins that with no
// RAG embedding configured, the embedding upstream is the operator's
// ollama.base_url, whether or not the Ollama service itself is enabled.
func TestBuildEmbeddingConfig_NoRAGUsesConfiguredOllamaBaseURL(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		cfg := createFTConfig(t)
		cfg.RAG.Enabled = false
		cfg.Ollama.Enabled = enabled
		cfg.Ollama.BaseURL = "http://ollama.example.test:9999"

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		result, err := srv.buildEmbeddingConfig()
		require.NoError(t, err)
		assert.Equal(t, "ollama", result.Provider)
		assert.Equal(t, "http://ollama.example.test:9999", result.APIURL, "ollama.enabled=%v", enabled)
	}
}

// TestBuildEmbeddingConfig_RAGEndpointResolvesFromDefaultsLayer pins that a
// RAG provider with no api_url gets its defaults-layer endpoint, and that an
// operator's api_url wins over it.
func TestBuildEmbeddingConfig_RAGEndpointResolvesFromDefaultsLayer(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.RAG.Enabled = true
	cfg.RAG.Embedding = config.EmbeddingConfig{Provider: "openai", APIKey: "k", Dimensions: 1536}

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	result, err := srv.buildEmbeddingConfig()
	require.NoError(t, err)
	assert.Equal(t, config.DefaultEmbeddingAPIURL("openai"), result.APIURL)

	srv.config.RAG.Embedding.APIURL = "https://openai.proxy.example.test"
	result, err = srv.buildEmbeddingConfig()
	require.NoError(t, err)
	assert.Equal(t, "https://openai.proxy.example.test", result.APIURL)
}

// TestBuildEmbeddingConfig_RAGEndpointUnresolvedIsTypedError pins that a RAG
// provider with neither an api_url nor a defaults-layer endpoint is refused
// with a typed error, and that startEmbedding then leaves embedding off
// rather than dialling a guessed endpoint.
func TestBuildEmbeddingConfig_RAGEndpointUnresolvedIsTypedError(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.RAG.Enabled = true
	cfg.RAG.Embedding = config.EmbeddingConfig{Provider: "custom-provider", Dimensions: 384}

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	_, err = srv.buildEmbeddingConfig()
	require.ErrorIs(t, err, config.ErrUpstreamEndpointMissing)

	require.NoError(t, srv.startEmbedding())
	assert.Nil(t, srv.embeddingManager, "an unresolved upstream must leave embedding disabled")
}
