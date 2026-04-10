//go:build integration && vector

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
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOllamaIntegration tests the OpenAI provider against a real Ollama instance
// Run with: go test -tags=integration,vector -run TestOllama
func TestOllamaIntegration(t *testing.T) {
	// Check if Ollama is available
	ollamaURL := os.Getenv("OLLAMA_URL")
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}

	config := Config{
		Provider:     "openai",
		APIURL:       ollamaURL,
		APIKey:       "", // No key needed for Ollama
		Model:        "all-minilm",
		Dimensions:   384, // all-minilm uses 384 dimensions
		MaxBatchSize: 32,
		Timeout:      30,
		CacheEnabled: false, // Disable cache for integration tests
	}

	provider, err := NewOpenAIProvider(config)
	require.NoError(t, err, "Failed to create Ollama provider")
	defer provider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("single embedding", func(t *testing.T) {
		text := "hello world"
		embedding, err := provider.Embed(ctx, text)

		require.NoError(t, err)
		assert.NotNil(t, embedding)
		assert.Equal(t, 384, len(embedding), "all-minilm should produce 384-dimensional embeddings")

		// Check that embedding values are in reasonable range
		for _, val := range embedding {
			assert.True(t, val >= -1.0 && val <= 1.0, "Embedding values should be normalized")
		}
	})

	t.Run("batch embeddings", func(t *testing.T) {
		texts := []string{
			"hello world",
			"goodbye world",
			"machine learning",
		}

		embeddings, err := provider.EmbedBatch(ctx, texts)

		require.NoError(t, err)
		assert.Equal(t, 3, len(embeddings))

		for i, emb := range embeddings {
			assert.Equal(t, 384, len(emb), "Embedding %d should have 384 dimensions", i)
		}

		// Verify embeddings are different
		assert.NotEqual(t, embeddings[0], embeddings[1], "Different texts should have different embeddings")
	})

	t.Run("semantic similarity", func(t *testing.T) {
		// Similar texts should have similar embeddings
		emb1, err := provider.Embed(ctx, "dog")
		require.NoError(t, err)

		emb2, err := provider.Embed(ctx, "puppy")
		require.NoError(t, err)

		emb3, err := provider.Embed(ctx, "car")
		require.NoError(t, err)

		// Calculate cosine similarity
		sim12 := cosineSimilarity(emb1, emb2)
		sim13 := cosineSimilarity(emb1, emb3)

		// dog and puppy should be more similar than dog and car
		assert.Greater(t, sim12, sim13, "dog/puppy should be more similar than dog/car")
		assert.Greater(t, sim12, 0.5, "dog/puppy similarity should be > 0.5")
	})

	t.Run("provider metadata", func(t *testing.T) {
		assert.Equal(t, "openai", provider.Name())
		assert.Equal(t, 384, provider.Dimensions())
		assert.Greater(t, provider.MaxTokens(), 0)
	})

	t.Run("empty text error", func(t *testing.T) {
		_, err := provider.Embed(ctx, "")
		assert.Error(t, err)
	})
}

// TestOllamaIntegrationWithCache tests Ollama with caching enabled
func TestOllamaIntegrationWithCache(t *testing.T) {
	ollamaURL := os.Getenv("OLLAMA_URL")
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}

	config := Config{
		Provider:     "openai",
		APIURL:       ollamaURL,
		APIKey:       "",
		Model:        "all-minilm",
		Dimensions:   384,
		MaxBatchSize: 32,
		Timeout:      30,
		CacheEnabled: true,
		CacheSize:    100,
		CacheTTL:     3600,
	}

	mgr, err := NewManager(config)
	require.NoError(t, err)
	defer mgr.Close()

	provider, err := NewOpenAIProvider(config)
	require.NoError(t, err)

	err = mgr.RegisterProvider("openai", provider)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("cache hit performance", func(t *testing.T) {
		text := "test cache performance"

		// First call - cache miss
		start := time.Now()
		emb1, err := mgr.Embed(ctx, text)
		firstDuration := time.Since(start)
		require.NoError(t, err)

		// Second call - cache hit
		start = time.Now()
		emb2, err := mgr.Embed(ctx, text)
		cachedDuration := time.Since(start)
		require.NoError(t, err)

		// Verify embeddings are the same
		assert.Equal(t, emb1, emb2)

		// Cache hit should be much faster (at least 10x)
		assert.Less(t, cachedDuration.Milliseconds(), firstDuration.Milliseconds()/10,
			"Cache hit (%dms) should be at least 10x faster than cache miss (%dms)",
			cachedDuration.Milliseconds(), firstDuration.Milliseconds())

		// Verify cache stats
		stats := mgr.GetStats()
		assert.Equal(t, uint64(1), stats.CacheHits, "Should have 1 cache hit")
		assert.Equal(t, uint64(1), stats.CacheMisses, "Should have 1 cache miss")
		assert.InDelta(t, 0.5, stats.CacheHitRate, 0.01, "Hit rate should be 50%")
	})
}

// TestOllamaIntegrationBatchPerformance tests batch operation performance
func TestOllamaIntegrationBatchPerformance(t *testing.T) {
	ollamaURL := os.Getenv("OLLAMA_URL")
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}

	config := Config{
		Provider:     "openai",
		APIURL:       ollamaURL,
		APIKey:       "",
		Model:        "all-minilm",
		Dimensions:   384,
		MaxBatchSize: 32,
		Timeout:      60,
		CacheEnabled: false,
	}

	provider, err := NewOpenAIProvider(config)
	require.NoError(t, err)
	defer provider.Close()

	ctx := context.Background()

	t.Run("batch vs individual", func(t *testing.T) {
		texts := []string{
			"machine learning",
			"artificial intelligence",
			"deep learning",
			"neural networks",
			"natural language processing",
		}

		// Individual embeddings
		start := time.Now()
		for _, text := range texts {
			_, err := provider.Embed(ctx, text)
			require.NoError(t, err)
		}
		individualDuration := time.Since(start)

		// Batch embedding
		start = time.Now()
		embeddings, err := provider.EmbedBatch(ctx, texts)
		batchDuration := time.Since(start)
		require.NoError(t, err)
		assert.Equal(t, len(texts), len(embeddings))

		// Batch should be faster (but Ollama doesn't have true batch API,
		// so it may not be much faster)
		t.Logf("Individual: %v, Batch: %v", individualDuration, batchDuration)
	})
}

// cosineSimilarity calculates cosine similarity between two vectors
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := 0; i < len(a); i++ {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (normA * normB)
}
