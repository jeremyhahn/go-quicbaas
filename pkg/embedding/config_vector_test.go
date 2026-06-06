//go:build vector && qrdb_vector

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

package embedding

import (
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, "voyage", cfg.Provider)
	assert.Equal(t, "voyage-code-2", cfg.Model)
	assert.Equal(t, 1536, cfg.Dimensions)
	assert.True(t, cfg.CacheEnabled)
}

func TestDefaultOllamaConfig(t *testing.T) {
	cfg := DefaultOllamaConfig()
	assert.Equal(t, DefaultOllamaURL, cfg.BaseURL)
	assert.Equal(t, "nomic-embed-text", cfg.Model)
	assert.Equal(t, 3, cfg.MaxRetries)
}

func TestDefaultLMStudioConfig(t *testing.T) {
	cfg := DefaultLMStudioConfig()
	assert.Equal(t, DefaultLMStudioURL, cfg.BaseURL)
	assert.Equal(t, "nomic-ai/nomic-embed-text-v1.5-GGUF", cfg.Model)
	assert.Equal(t, 3, cfg.MaxRetries)
}

func TestStatsFromManager(t *testing.T) {
	in := qrdbsdk.EmbeddingManagerStats{
		ProvidersRegistered: 2,
		ProviderNames:       []string{"a", "b"},
		DefaultProvider:     "a",
		CacheEnabled:        true,
		CacheSize:           5,
		CacheHits:           10,
		CacheMisses:         4,
		CacheHitRate:        0.5,
	}
	out := statsFromManager(in)
	assert.Equal(t, in.ProvidersRegistered, out.ProvidersRegistered)
	assert.Equal(t, in.ProviderNames, out.ProviderNames)
	assert.Equal(t, in.DefaultProvider, out.DefaultProvider)
	assert.Equal(t, in.CacheEnabled, out.CacheEnabled)
	assert.Equal(t, in.CacheSize, out.CacheSize)
	assert.Equal(t, in.CacheHits, out.CacheHits)
	assert.Equal(t, in.CacheMisses, out.CacheMisses)
	assert.Equal(t, in.CacheHitRate, out.CacheHitRate)
}

func TestConstants(t *testing.T) {
	// Re-exported constants must remain stable for downstream callers.
	assert.Equal(t, "https://api.openai.com", DefaultOpenAIURL)
	assert.Equal(t, "text-embedding-3-small", ModelTextEmbedding3Small)
	assert.Equal(t, "text-embedding-3-large", ModelTextEmbedding3Large)
	assert.Equal(t, "text-embedding-ada-002", ModelTextEmbeddingAda002)
	assert.Equal(t, "https://api.voyageai.com/v1/embeddings", VoyageAPIURL)
	assert.NotEmpty(t, DefaultOllamaURL)
	assert.NotEmpty(t, DefaultLMStudioURL)
	assert.Equal(t, 3, DefaultLMStudioMaxRetries)
	assert.Equal(t, 1000, DefaultLMStudioRetryBackoffMS)
	assert.Equal(t, 3, DefaultOllamaMaxRetries)
}
