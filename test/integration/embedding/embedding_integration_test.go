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

//go:build integration

package embedding

import (
	"context"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
)

const (
	// Ollama configuration
	defaultOllamaURL   = "http://localhost:11434"
	defaultOllamaModel = "all-minilm"
	defaultDimensions  = 384 // all-minilm produces 384-dimensional embeddings

	// Test timeouts
	ollamaStartupTimeout = 120 * time.Second
	testTimeout          = 30 * time.Second
	batchTimeout         = 120 * time.Second // Increased for Docker/Ollama performance
)

// TestEmbeddingServiceIntegration is the main integration test suite
func TestEmbeddingServiceIntegration(t *testing.T) {
	// Get Ollama URL from environment
	ollamaURL := os.Getenv("OLLAMA_URL")
	if ollamaURL == "" {
		ollamaURL = defaultOllamaURL
	}

	// Wait for Ollama to be ready
	t.Log("Waiting for Ollama service to be ready...")
	err := waitForOllama(ollamaURL, ollamaStartupTimeout)
	require.NoError(t, err, "Ollama service must be available at %s for embedding integration tests", ollamaURL)
	t.Log("Ollama service is ready")

	// Run test suite
	t.Run("Provider", testEmbeddingProvider)
	t.Run("Manager", testEmbeddingManager)
	t.Run("Cache", testEmbeddingCache)
	t.Run("Batch", testEmbeddingBatch)
	t.Run("Models", testMultipleModels)
	t.Run("ErrorHandling", testErrorHandling)
	t.Run("Performance", testPerformance)
	t.Run("SemanticSimilarity", testSemanticSimilarity)
}

// testEmbeddingProvider tests the OpenAI provider with Ollama
func testEmbeddingProvider(t *testing.T) {
	ollamaURL := getOllamaURL()
	config := embedding.Config{
		Provider:     "openai",
		APIURL:       ollamaURL,
		APIKey:       "", // No key needed for Ollama
		Model:        defaultOllamaModel,
		Dimensions:   defaultDimensions,
		MaxBatchSize: 32,
		Timeout:      int(testTimeout.Seconds()),
		CacheEnabled: false,
	}

	provider, err := embedding.NewOpenAIProvider(config)
	require.NoError(t, err, "Failed to create OpenAI provider")
	defer provider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	t.Run("SingleEmbedding", func(t *testing.T) {
		text := "hello world"
		emb, err := provider.Embed(ctx, text)

		require.NoError(t, err)
		assert.NotNil(t, emb)
		assert.Equal(t, defaultDimensions, len(emb), "Expected %d dimensions", defaultDimensions)

		// Check that embedding values are in reasonable range
		for i, val := range emb {
			assert.True(t, !math.IsNaN(float64(val)), "Embedding value at %d is NaN", i)
			assert.True(t, !math.IsInf(float64(val), 0), "Embedding value at %d is Inf", i)
		}
	})

	t.Run("EmptyTextError", func(t *testing.T) {
		_, err := provider.Embed(ctx, "")
		assert.Error(t, err)
		assert.ErrorIs(t, err, embedding.ErrInvalidInput)
	})

	t.Run("ProviderMetadata", func(t *testing.T) {
		assert.Equal(t, "openai", provider.Name())
		assert.Equal(t, defaultDimensions, provider.Dimensions())
		assert.Greater(t, provider.MaxTokens(), 0)
	})

	t.Run("DifferentTexts", func(t *testing.T) {
		emb1, err := provider.Embed(ctx, "hello world")
		require.NoError(t, err)

		emb2, err := provider.Embed(ctx, "goodbye world")
		require.NoError(t, err)

		// Different texts should produce different embeddings
		assert.NotEqual(t, emb1, emb2)
	})

	t.Run("SameTextConsistency", func(t *testing.T) {
		text := "consistency test"
		emb1, err := provider.Embed(ctx, text)
		require.NoError(t, err)

		emb2, err := provider.Embed(ctx, text)
		require.NoError(t, err)

		// Same text should produce identical embeddings
		assert.Equal(t, len(emb1), len(emb2))
		for i := range emb1 {
			assert.InDelta(t, emb1[i], emb2[i], 1e-6, "Embeddings differ at index %d", i)
		}
	})
}

// testEmbeddingManager tests the embedding manager
func testEmbeddingManager(t *testing.T) {
	ollamaURL := getOllamaURL()
	config := embedding.Config{
		Provider:       "openai",
		APIURL:         ollamaURL,
		APIKey:         "",
		Model:          defaultOllamaModel,
		Dimensions:     defaultDimensions,
		MaxBatchSize:   32,
		Timeout:        int(testTimeout.Seconds()),
		CacheEnabled:   false,
		MaxRetries:     3,
		RetryBackoffMS: 1000,
	}

	mgr, err := embedding.NewManager(config)
	require.NoError(t, err)
	defer mgr.Close()

	provider, err := embedding.NewOpenAIProvider(config)
	require.NoError(t, err)

	err = mgr.RegisterProvider("openai", provider)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("ManagerEmbed", func(t *testing.T) {
		text := "manager test"
		emb, err := mgr.Embed(ctx, text)
		require.NoError(t, err)
		assert.Equal(t, defaultDimensions, len(emb))
	})

	t.Run("ManagerBatch", func(t *testing.T) {
		texts := []string{
			"first text",
			"second text",
			"third text",
		}

		embeddings, err := mgr.EmbedBatch(ctx, texts)
		require.NoError(t, err)
		assert.Equal(t, len(texts), len(embeddings))

		for i, emb := range embeddings {
			assert.Equal(t, defaultDimensions, len(emb), "Embedding %d has wrong dimensions", i)
		}
	})

	t.Run("ProviderLookup", func(t *testing.T) {
		p, err := mgr.GetProvider("openai")
		require.NoError(t, err)
		assert.NotNil(t, p)
		assert.Equal(t, "openai", p.Name())
	})

	t.Run("ProviderNotFound", func(t *testing.T) {
		_, err := mgr.GetProvider("nonexistent")
		assert.Error(t, err)
		assert.ErrorIs(t, err, embedding.ErrProviderNotFound)
	})

	t.Run("DuplicateProvider", func(t *testing.T) {
		err := mgr.RegisterProvider("openai", provider)
		assert.Error(t, err)
	})

	t.Run("GetDimensions", func(t *testing.T) {
		dims, err := mgr.Dimensions()
		require.NoError(t, err)
		assert.Equal(t, defaultDimensions, dims)
	})

	t.Run("GetStats", func(t *testing.T) {
		stats := mgr.GetStats()
		assert.Greater(t, stats.ProvidersRegistered, 0)
		assert.Contains(t, stats.ProviderNames, "openai")
	})
}

// testEmbeddingCache tests the caching functionality
func testEmbeddingCache(t *testing.T) {
	ollamaURL := getOllamaURL()
	config := embedding.Config{
		Provider:      "openai",
		APIURL:        ollamaURL,
		APIKey:        "",
		Model:         defaultOllamaModel,
		Dimensions:    defaultDimensions,
		MaxBatchSize:  32,
		Timeout:       int(testTimeout.Seconds()),
		CacheEnabled:  true,
		CacheSize:     100,
		CacheTTL:      3600,
		CacheCompress: true,
	}

	mgr, err := embedding.NewManager(config)
	require.NoError(t, err)
	defer mgr.Close()

	provider, err := embedding.NewOpenAIProvider(config)
	require.NoError(t, err)

	err = mgr.RegisterProvider("openai", provider)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("CacheHitPerformance", func(t *testing.T) {
		text := "cache performance test"

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

		// Cache hit should be significantly faster (at least 5x)
		assert.Less(t, cachedDuration, firstDuration/5,
			"Cache hit (%v) should be at least 5x faster than cache miss (%v)",
			cachedDuration, firstDuration)

		t.Logf("Cache miss: %v, Cache hit: %v (%.1fx faster)",
			firstDuration, cachedDuration, float64(firstDuration)/float64(cachedDuration))
	})

	t.Run("CacheStats", func(t *testing.T) {
		// Clear previous stats by creating new manager
		mgr2, err := embedding.NewManager(config)
		require.NoError(t, err)
		defer mgr2.Close()

		provider2, err := embedding.NewOpenAIProvider(config)
		require.NoError(t, err)
		err = mgr2.RegisterProvider("openai", provider2)
		require.NoError(t, err)

		// Generate some cache activity
		text1 := "stats test 1"
		text2 := "stats test 2"

		_, err = mgr2.Embed(ctx, text1) // Miss
		require.NoError(t, err)
		_, err = mgr2.Embed(ctx, text1) // Hit
		require.NoError(t, err)
		_, err = mgr2.Embed(ctx, text2) // Miss
		require.NoError(t, err)

		stats := mgr2.GetStats()
		assert.True(t, stats.CacheEnabled)
		assert.Equal(t, uint64(1), stats.CacheHits, "Expected 1 cache hit")
		assert.Equal(t, uint64(2), stats.CacheMisses, "Expected 2 cache misses")
		assert.InDelta(t, 0.333, stats.CacheHitRate, 0.01, "Cache hit rate should be ~33%")
	})

	t.Run("BatchCaching", func(t *testing.T) {
		texts := []string{"batch1", "batch2", "batch3"}

		// First batch - all misses
		emb1, err := mgr.EmbedBatch(ctx, texts)
		require.NoError(t, err)

		// Second batch - all hits
		emb2, err := mgr.EmbedBatch(ctx, texts)
		require.NoError(t, err)

		// Verify results are identical
		assert.Equal(t, len(emb1), len(emb2))
		for i := range emb1 {
			assert.Equal(t, emb1[i], emb2[i])
		}
	})

	t.Run("CacheCompression", func(t *testing.T) {
		// Create cache with compression
		cache1, err := embedding.NewCache(100, 3600, true)
		require.NoError(t, err)

		// Create cache without compression
		cache2, err := embedding.NewCache(100, 3600, false)
		require.NoError(t, err)

		// Store same embedding in both
		testEmb := make([]float32, defaultDimensions)
		for i := range testEmb {
			testEmb[i] = float32(i) / float32(defaultDimensions)
		}

		cache1.Put("test", testEmb)
		cache2.Put("test", testEmb)

		// Retrieve and verify
		emb1, ok1 := cache1.Get("test")
		require.True(t, ok1)
		emb2, ok2 := cache2.Get("test")
		require.True(t, ok2)

		assert.Equal(t, testEmb, emb1)
		assert.Equal(t, testEmb, emb2)
	})
}

// testEmbeddingBatch tests batch embedding operations
func testEmbeddingBatch(t *testing.T) {
	ollamaURL := getOllamaURL()
	config := embedding.Config{
		Provider:     "openai",
		APIURL:       ollamaURL,
		APIKey:       "",
		Model:        defaultOllamaModel,
		Dimensions:   defaultDimensions,
		MaxBatchSize: 32,
		Timeout:      int(batchTimeout.Seconds()),
		CacheEnabled: false,
	}

	provider, err := embedding.NewOpenAIProvider(config)
	require.NoError(t, err)
	defer provider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), batchTimeout)
	defer cancel()

	t.Run("SmallBatch", func(t *testing.T) {
		texts := []string{
			"machine learning",
			"artificial intelligence",
			"deep learning",
			"neural networks",
			"natural language processing",
		}

		embeddings, err := provider.EmbedBatch(ctx, texts)
		require.NoError(t, err)
		assert.Equal(t, len(texts), len(embeddings))

		for i, emb := range embeddings {
			assert.Equal(t, defaultDimensions, len(emb), "Embedding %d has wrong dimensions", i)
		}

		// Verify embeddings are different
		for i := 0; i < len(embeddings); i++ {
			for j := i + 1; j < len(embeddings); j++ {
				assert.NotEqual(t, embeddings[i], embeddings[j],
					"Embeddings %d and %d should be different", i, j)
			}
		}
	})

	t.Run("LargeBatch", func(t *testing.T) {
		// Use fresh context for large batch - Ollama in Docker is slow (~2-3s per embedding)
		largeBatchCtx, largeBatchCancel := context.WithTimeout(context.Background(), 3*batchTimeout)
		defer largeBatchCancel()

		// Create 20 unique texts (reduced from 50 for Docker/Ollama performance)
		texts := make([]string, 20)
		for i := range texts {
			texts[i] = fmt.Sprintf("This is test document number %d with unique content", i)
		}

		embeddings, err := provider.EmbedBatch(largeBatchCtx, texts)
		require.NoError(t, err)
		assert.Equal(t, len(texts), len(embeddings))

		for i, emb := range embeddings {
			assert.Equal(t, defaultDimensions, len(emb), "Embedding %d has wrong dimensions", i)
		}
	})

	t.Run("EmptyBatch", func(t *testing.T) {
		_, err := provider.EmbedBatch(ctx, []string{})
		assert.Error(t, err)
		assert.ErrorIs(t, err, embedding.ErrInvalidInput)
	})

	t.Run("BatchWithEmptyString", func(t *testing.T) {
		texts := []string{"valid", "", "also valid"}
		_, err := provider.EmbedBatch(ctx, texts)
		assert.Error(t, err)
		assert.ErrorIs(t, err, embedding.ErrInvalidInput)
	})

	t.Run("SingleItemBatch", func(t *testing.T) {
		// Use fresh context for single item batch
		singleCtx, singleCancel := context.WithTimeout(context.Background(), testTimeout)
		defer singleCancel()

		texts := []string{"single item"}
		embeddings, err := provider.EmbedBatch(singleCtx, texts)
		require.NoError(t, err)
		assert.Equal(t, 1, len(embeddings))
		assert.Equal(t, defaultDimensions, len(embeddings[0]))
	})
}

// testMultipleModels tests different embedding models
func testMultipleModels(t *testing.T) {
	ollamaURL := getOllamaURL()

	// Test with all-minilm model
	t.Run("AllMiniLM", func(t *testing.T) {
		config := embedding.Config{
			Provider:   "openai",
			APIURL:     ollamaURL,
			Model:      "all-minilm",
			Dimensions: 384,
			Timeout:    int(testTimeout.Seconds()),
		}

		provider, err := embedding.NewOpenAIProvider(config)
		require.NoError(t, err)
		defer provider.Close()

		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		emb, err := provider.Embed(ctx, "test")
		require.NoError(t, err)
		assert.Equal(t, 384, len(emb))
	})

	// Note: Add more models if they are available in the test environment
}

// testErrorHandling tests error handling scenarios
func testErrorHandling(t *testing.T) {
	t.Run("InvalidURL", func(t *testing.T) {
		config := embedding.Config{
			Provider:   "openai",
			APIURL:     "http://invalid-url-that-does-not-exist:99999",
			Model:      defaultOllamaModel,
			Dimensions: defaultDimensions,
			Timeout:    2,
			MaxRetries: 1,
		}

		provider, err := embedding.NewOpenAIProvider(config)
		require.NoError(t, err)
		defer provider.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err = provider.Embed(ctx, "test")
		assert.Error(t, err)
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		config := embedding.Config{
			Provider:   "openai",
			APIURL:     getOllamaURL(),
			Model:      defaultOllamaModel,
			Dimensions: defaultDimensions,
			Timeout:    int(testTimeout.Seconds()),
		}

		provider, err := embedding.NewOpenAIProvider(config)
		require.NoError(t, err)
		defer provider.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err = provider.Embed(ctx, "test")
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("ContextTimeout", func(t *testing.T) {
		config := embedding.Config{
			Provider:   "openai",
			APIURL:     getOllamaURL(),
			Model:      defaultOllamaModel,
			Dimensions: defaultDimensions,
			Timeout:    1, // Very short timeout
		}

		provider, err := embedding.NewOpenAIProvider(config)
		require.NoError(t, err)
		defer provider.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer cancel()

		_, err = provider.Embed(ctx, "test with very long timeout that should fail")
		assert.Error(t, err)
	})

	t.Run("InvalidConfig", func(t *testing.T) {
		config := embedding.Config{
			Provider: "",
			APIURL:   "",
		}

		_, err := embedding.NewManager(config)
		assert.Error(t, err)
	})
}

// testPerformance tests performance characteristics
func testPerformance(t *testing.T) {
	ollamaURL := getOllamaURL()
	config := embedding.Config{
		Provider:     "openai",
		APIURL:       ollamaURL,
		Model:        defaultOllamaModel,
		Dimensions:   defaultDimensions,
		MaxBatchSize: 32,
		Timeout:      int(batchTimeout.Seconds()),
		CacheEnabled: false,
	}

	provider, err := embedding.NewOpenAIProvider(config)
	require.NoError(t, err)
	defer provider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), batchTimeout)
	defer cancel()

	t.Run("BatchVsIndividual", func(t *testing.T) {
		texts := []string{
			"performance test 1",
			"performance test 2",
			"performance test 3",
			"performance test 4",
			"performance test 5",
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

		t.Logf("Individual: %v, Batch: %v, Ratio: %.2fx", individualDuration, batchDuration, float64(batchDuration)/float64(individualDuration))

		// Note: Ollama doesn't optimize batch operations, so batch timing can vary
		// significantly based on system load, model warmup, and resource contention.
		// We only verify that batch returns correct results, not performance.
	})

	t.Run("Throughput", func(t *testing.T) {
		// Use fresh context with extended timeout for Docker/Ollama which is slow (~3-5s per embedding)
		// 10 embeddings * 5s = 50s, allow 3 minutes to be safe
		throughputCtx, throughputCancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer throughputCancel()

		// Reduced to 10 embeddings for Docker performance
		numEmbeddings := 10
		texts := make([]string, numEmbeddings)
		for i := range texts {
			texts[i] = fmt.Sprintf("throughput test document %d", i)
		}

		start := time.Now()
		embeddings, err := provider.EmbedBatch(throughputCtx, texts)
		duration := time.Since(start)

		require.NoError(t, err)
		assert.Equal(t, numEmbeddings, len(embeddings))

		throughput := float64(numEmbeddings) / duration.Seconds()
		t.Logf("Throughput: %.2f embeddings/second", throughput)

		// Should achieve reasonable throughput (at least 0.1 embedding per second)
		// Ollama in Docker environments is slow (~3-5s per embedding)
		assert.Greater(t, throughput, 0.1, "Throughput too low")
	})
}

// testSemanticSimilarity tests that embeddings capture semantic meaning
func testSemanticSimilarity(t *testing.T) {
	ollamaURL := getOllamaURL()
	config := embedding.Config{
		Provider:   "openai",
		APIURL:     ollamaURL,
		Model:      defaultOllamaModel,
		Dimensions: defaultDimensions,
		Timeout:    int(testTimeout.Seconds()),
	}

	provider, err := embedding.NewOpenAIProvider(config)
	require.NoError(t, err)
	defer provider.Close()

	t.Run("SimilarWords", func(t *testing.T) {
		// Use fresh context for each subtest to avoid timeout accumulation
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		// Similar words
		emb1, err := provider.Embed(ctx, "dog")
		require.NoError(t, err)

		emb2, err := provider.Embed(ctx, "puppy")
		require.NoError(t, err)

		// Dissimilar word
		emb3, err := provider.Embed(ctx, "computer")
		require.NoError(t, err)

		// Calculate cosine similarities
		sim12 := cosineSimilarity(emb1, emb2)
		sim13 := cosineSimilarity(emb1, emb3)

		// dog and puppy should be more similar than dog and computer
		assert.Greater(t, sim12, sim13,
			"dog/puppy (%.3f) should be more similar than dog/computer (%.3f)",
			sim12, sim13)

		t.Logf("Similarity dog/puppy: %.3f, dog/computer: %.3f", sim12, sim13)
	})

	t.Run("SimilarPhrases", func(t *testing.T) {
		// Use fresh context for each subtest to avoid timeout accumulation
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		emb1, err := provider.Embed(ctx, "machine learning is a subset of artificial intelligence")
		require.NoError(t, err)

		emb2, err := provider.Embed(ctx, "AI includes machine learning as a subfield")
		require.NoError(t, err)

		emb3, err := provider.Embed(ctx, "cooking recipes for dinner")
		require.NoError(t, err)

		sim12 := cosineSimilarity(emb1, emb2)
		sim13 := cosineSimilarity(emb1, emb3)

		assert.Greater(t, sim12, sim13,
			"Similar phrases should have higher similarity")
		assert.Greater(t, sim12, 0.5, "Similar phrases should have similarity > 0.5")

		t.Logf("Similar phrases: %.3f, Dissimilar: %.3f", sim12, sim13)
	})

	t.Run("IdenticalText", func(t *testing.T) {
		// Use fresh context for each subtest to avoid timeout accumulation
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		text := "identical text test"
		emb1, err := provider.Embed(ctx, text)
		require.NoError(t, err)

		emb2, err := provider.Embed(ctx, text)
		require.NoError(t, err)

		sim := cosineSimilarity(emb1, emb2)
		assert.InDelta(t, 1.0, sim, 0.001, "Identical texts should have similarity ~1.0")
	})
}

// Helper functions

// getOllamaURL returns the Ollama URL from environment or default
func getOllamaURL() string {
	url := os.Getenv("OLLAMA_URL")
	if url == "" {
		url = defaultOllamaURL
	}
	return url
}

// waitForOllama waits for the Ollama service to become available.
func waitForOllama(url string, timeout time.Duration) error {
	config := embedding.Config{
		Provider:   "openai",
		APIURL:     url,
		Model:      defaultOllamaModel,
		Dimensions: defaultDimensions,
		Timeout:    5,
		MaxRetries: 1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Try immediately first
	if err := tryOllama(config); err == nil {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for Ollama at %s", url)
		case <-ticker.C:
			if err := tryOllama(config); err == nil {
				return nil
			}
		}
	}
}

// tryOllama attempts a single embedding request to verify Ollama is ready.
func tryOllama(config embedding.Config) error {
	provider, err := embedding.NewOpenAIProvider(config)
	if err != nil {
		return err
	}
	defer provider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = provider.Embed(ctx, "test")
	return err
}

// cosineSimilarity is defined in vector_helpers.go
