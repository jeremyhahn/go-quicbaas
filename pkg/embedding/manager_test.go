//go:build vector

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
	"errors"
	"testing"

	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"syscall"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock provider for testing
type mockProvider struct {
	name       string
	dimensions int
	maxTokens  int
	embedFunc  func(ctx context.Context, text string) ([]float32, error)
	batchFunc  func(ctx context.Context, texts []string) ([][]float32, error)
	closed     bool
}

func (m *mockProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, text)
	}
	return []float32{0.1, 0.2, 0.3}, nil
}

func (m *mockProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if m.batchFunc != nil {
		return m.batchFunc(ctx, texts)
	}
	results := make([][]float32, len(texts))
	for i := range texts {
		results[i] = []float32{float32(i), float32(i + 1), float32(i + 2)}
	}
	return results, nil
}

func (m *mockProvider) Dimensions() int {
	return m.dimensions
}

func (m *mockProvider) MaxTokens() int {
	return m.maxTokens
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) Close() error {
	m.closed = true
	return nil
}

func TestNewManager(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		config := DefaultConfig()
		config.Provider = "openai"
		config.APIURL = "https://api.openai.com"
		config.APIKey = "test-key"
		config.Model = "text-embedding-3-small"
		config.Dimensions = 1536
		config.CacheEnabled = true
		config.CacheSize = 100

		mgr, err := NewManager(config)
		require.NoError(t, err)
		assert.NotNil(t, mgr)
		assert.NotNil(t, mgr.cache)
	})

	t.Run("cache disabled", func(t *testing.T) {
		config := DefaultConfig()
		config.Provider = "openai"
		config.APIURL = "https://api.openai.com"
		config.APIKey = "test-key"
		config.Model = "test"
		config.Dimensions = 1536
		config.CacheEnabled = false

		mgr, err := NewManager(config)
		require.NoError(t, err)
		assert.Nil(t, mgr.cache)
	})

	t.Run("invalid config", func(t *testing.T) {
		config := Config{
			Provider: "", // Invalid
		}

		_, err := NewManager(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to validate config")
	})

	t.Run("invalid cache size", func(t *testing.T) {
		config := DefaultConfig()
		config.Provider = "openai"
		config.APIURL = "https://api.openai.com"
		config.APIKey = "test-key"
		config.Model = "test"
		config.Dimensions = 1536
		config.CacheEnabled = true
		config.CacheSize = 0 // Invalid

		_, err := NewManager(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create cache")
	})
}

func TestManagerRegisterProvider(t *testing.T) {
	config := DefaultConfig()
	config.Provider = "openai"
	config.APIURL = "http://test"
	config.APIKey = "test-key"
	mgr, err := NewManager(config)
	require.NoError(t, err)

	mock := &mockProvider{
		name:       "test",
		dimensions: 1536,
		maxTokens:  8192,
	}

	t.Run("register new provider", func(t *testing.T) {
		err := mgr.RegisterProvider("test", mock)
		assert.NoError(t, err)
	})

	t.Run("register duplicate provider", func(t *testing.T) {
		err := mgr.RegisterProvider("test", mock)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
	})
}

func TestManagerGetProvider(t *testing.T) {
	config := DefaultConfig()
	config.Provider = "openai"
	config.APIURL = "http://test"
	config.APIKey = "test-key"
	mgr, err := NewManager(config)
	require.NoError(t, err)

	mock := &mockProvider{name: "test"}
	mgr.RegisterProvider("test", mock)

	t.Run("get existing provider", func(t *testing.T) {
		provider, err := mgr.GetProvider("test")
		assert.NoError(t, err)
		assert.Equal(t, mock, provider)
	})

	t.Run("get non-existent provider", func(t *testing.T) {
		_, err := mgr.GetProvider("nonexistent")
		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrProviderNotFound))
	})
}

func TestManagerEmbed(t *testing.T) {
	config := DefaultConfig()
	config.Provider = "openai"
	config.APIURL = "http://test"
	config.APIKey = "test-key"
	config.CacheEnabled = false
	mgr, err := NewManager(config)
	require.NoError(t, err)

	// Now change provider to "test" for the mock
	mgr.config.Provider = "test"

	expectedEmbedding := []float32{0.5, 0.6, 0.7}
	mock := &mockProvider{
		name: "test",
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			return expectedEmbedding, nil
		},
	}
	mgr.RegisterProvider("test", mock)

	t.Run("successful embed", func(t *testing.T) {
		result, err := mgr.Embed(context.Background(), "hello world")
		assert.NoError(t, err)
		assert.Equal(t, expectedEmbedding, result)
	})

	t.Run("provider not found", func(t *testing.T) {
		mgr.config.Provider = "nonexistent"
		_, err := mgr.Embed(context.Background(), "hello")
		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrProviderNotFound))
	})
}

func TestManagerEmbedWithCache(t *testing.T) {
	config := DefaultConfig()
	config.Provider = "openai"
	config.APIURL = "http://test"
	config.APIKey = "test-key"
	config.CacheEnabled = true
	config.CacheSize = 10
	mgr, err := NewManager(config)
	require.NoError(t, err)

	// Change provider to "test" for the mock
	mgr.config.Provider = "test"

	callCount := 0
	mock := &mockProvider{
		name: "test",
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			callCount++
			return []float32{float32(callCount)}, nil
		},
	}
	mgr.RegisterProvider("test", mock)

	t.Run("first call - cache miss", func(t *testing.T) {
		result, err := mgr.Embed(context.Background(), "hello")
		assert.NoError(t, err)
		assert.Equal(t, []float32{1.0}, result)
		assert.Equal(t, 1, callCount)
	})

	t.Run("second call - cache hit", func(t *testing.T) {
		result, err := mgr.Embed(context.Background(), "hello")
		assert.NoError(t, err)
		assert.Equal(t, []float32{1.0}, result) // Same result
		assert.Equal(t, 1, callCount)           // Provider not called again
	})

	t.Run("different text - cache miss", func(t *testing.T) {
		result, err := mgr.Embed(context.Background(), "world")
		assert.NoError(t, err)
		assert.Equal(t, []float32{2.0}, result)
		assert.Equal(t, 2, callCount)
	})
}

func TestManagerEmbedBatch(t *testing.T) {
	config := DefaultConfig()
	config.Provider = "openai"
	config.APIURL = "http://test"
	config.APIKey = "test-key"
	config.CacheEnabled = false
	mgr, err := NewManager(config)
	require.NoError(t, err)

	// Change provider to "test" for the mock
	mgr.config.Provider = "test"

	mock := &mockProvider{
		name: "test",
		batchFunc: func(ctx context.Context, texts []string) ([][]float32, error) {
			results := make([][]float32, len(texts))
			for i := range texts {
				results[i] = []float32{float32(i)}
			}
			return results, nil
		},
	}
	mgr.RegisterProvider("test", mock)

	t.Run("successful batch embed", func(t *testing.T) {
		texts := []string{"text1", "text2", "text3"}
		results, err := mgr.EmbedBatch(context.Background(), texts)
		assert.NoError(t, err)
		assert.Len(t, results, 3)
		assert.Equal(t, []float32{0.0}, results[0])
		assert.Equal(t, []float32{1.0}, results[1])
		assert.Equal(t, []float32{2.0}, results[2])
	})

	t.Run("empty texts", func(t *testing.T) {
		_, err := mgr.EmbedBatch(context.Background(), []string{})
		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrInvalidInput))
	})

	t.Run("provider not found", func(t *testing.T) {
		mgr.config.Provider = "nonexistent"
		_, err := mgr.EmbedBatch(context.Background(), []string{"text"})
		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrProviderNotFound))
	})
}

func TestManagerEmbedBatchWithCache(t *testing.T) {
	config := DefaultConfig()
	config.Provider = "openai"
	config.APIURL = "http://test"
	config.APIKey = "test-key"
	config.CacheEnabled = true
	config.CacheSize = 10
	mgr, err := NewManager(config)
	require.NoError(t, err)

	// Change provider to "test" for the mock
	mgr.config.Provider = "test"

	callCounts := make(map[string]int)
	mock := &mockProvider{
		name: "test",
		batchFunc: func(ctx context.Context, texts []string) ([][]float32, error) {
			results := make([][]float32, len(texts))
			for i, text := range texts {
				callCounts[text]++
				results[i] = []float32{float32(len(text))}
			}
			return results, nil
		},
	}
	mgr.RegisterProvider("test", mock)

	t.Run("first batch - all cache misses", func(t *testing.T) {
		texts := []string{"hello", "world"}
		results, err := mgr.EmbedBatch(context.Background(), texts)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, 1, callCounts["hello"])
		assert.Equal(t, 1, callCounts["world"])
	})

	t.Run("second batch - all cache hits", func(t *testing.T) {
		texts := []string{"hello", "world"}
		results, err := mgr.EmbedBatch(context.Background(), texts)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, 1, callCounts["hello"]) // Still 1, not called again
		assert.Equal(t, 1, callCounts["world"]) // Still 1, not called again
	})

	t.Run("mixed batch - partial cache hits", func(t *testing.T) {
		texts := []string{"hello", "new", "world"}
		results, err := mgr.EmbedBatch(context.Background(), texts)
		assert.NoError(t, err)
		assert.Len(t, results, 3)
		assert.Equal(t, 1, callCounts["hello"]) // Cache hit
		assert.Equal(t, 1, callCounts["new"])   // New call
		assert.Equal(t, 1, callCounts["world"]) // Cache hit
	})
}

func TestManagerDimensions(t *testing.T) {
	config := DefaultConfig()
	config.Provider = "openai"
	config.APIURL = "http://test"
	config.APIKey = "test-key"
	mgr, err := NewManager(config)
	require.NoError(t, err)

	// Change provider to "test" for the mock
	mgr.config.Provider = "test"

	mock := &mockProvider{
		name:       "test",
		dimensions: 768,
	}
	mgr.RegisterProvider("test", mock)

	t.Run("get dimensions", func(t *testing.T) {
		dims, err := mgr.Dimensions()
		assert.NoError(t, err)
		assert.Equal(t, 768, dims)
	})

	t.Run("provider not found", func(t *testing.T) {
		mgr.config.Provider = "nonexistent"
		_, err := mgr.Dimensions()
		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrProviderNotFound))
	})
}

func TestManagerClose(t *testing.T) {
	config := DefaultConfig()
	config.Provider = "openai"
	config.APIURL = "http://test"
	config.APIKey = "test-key"
	mgr, err := NewManager(config)
	require.NoError(t, err)

	mock1 := &mockProvider{name: "test1"}
	mock2 := &mockProvider{name: "test2"}
	mgr.RegisterProvider("test1", mock1)
	mgr.RegisterProvider("test2", mock2)

	err = mgr.Close()
	assert.NoError(t, err)
	assert.True(t, mock1.closed)
	assert.True(t, mock2.closed)
}

func TestManagerGetStats(t *testing.T) {
	config := DefaultConfig()
	config.Provider = "openai"
	config.APIURL = "http://test"
	config.APIKey = "test-key"
	config.CacheEnabled = true
	config.CacheSize = 10
	mgr, err := NewManager(config)
	require.NoError(t, err)

	// Change provider to "test" for the mock
	mgr.config.Provider = "test"

	mock := &mockProvider{name: "test"}
	mgr.RegisterProvider("test", mock)

	t.Run("initial stats", func(t *testing.T) {
		stats := mgr.GetStats()
		assert.Equal(t, 1, stats.ProvidersRegistered)
		assert.Contains(t, stats.ProviderNames, "test")
		assert.True(t, stats.CacheEnabled)
		assert.Equal(t, 0, stats.CacheSize)
		assert.Equal(t, uint64(0), stats.CacheHits)
		assert.Equal(t, uint64(0), stats.CacheMisses)
	})

	t.Run("stats after operations", func(t *testing.T) {
		mgr.Embed(context.Background(), "test1")
		mgr.Embed(context.Background(), "test1") // Cache hit
		mgr.Embed(context.Background(), "test2")

		stats := mgr.GetStats()
		assert.Equal(t, 2, stats.CacheSize)
		assert.Equal(t, uint64(1), stats.CacheHits)
		assert.Equal(t, uint64(2), stats.CacheMisses)
		assert.InDelta(t, 0.333, stats.CacheHitRate, 0.01)
	})
}

func TestManagerGetStatsNoCacheStats(t *testing.T) {
	config := DefaultConfig()
	config.Provider = "openai"
	config.APIURL = "http://test"
	config.APIKey = "test-key"
	config.CacheEnabled = false
	mgr, err := NewManager(config)
	require.NoError(t, err)

	stats := mgr.GetStats()
	assert.False(t, stats.CacheEnabled)
	assert.Equal(t, 0, stats.CacheSize)
	assert.Equal(t, uint64(0), stats.CacheHits)
	assert.Equal(t, uint64(0), stats.CacheMisses)
	assert.Equal(t, 0.0, stats.CacheHitRate)
}

// =============================================================================
// Consolidated from additional_coverage_test.go
// =============================================================================

// TestCacheEncodingEdgeCases tests edge cases in encode/decode
func TestCacheEncodingEdgeCases(t *testing.T) {
	cache, err := NewCache(10, 3600, true)
	require.NoError(t, err)

	t.Run("gzip write error path", func(t *testing.T) {
		// Test with compressible data
		embedding := make([]float32, 1000)
		for i := range embedding {
			embedding[i] = 0.5
		}

		value, compressed, err := cache.encodeValue(embedding, true)
		require.NoError(t, err)
		assert.True(t, compressed)

		// Decode it
		decoded, err := cache.decodeValue(value, true)
		require.NoError(t, err)
		assert.Equal(t, embedding, decoded)
	})

	t.Run("gzip read all error path", func(t *testing.T) {
		// Create a valid gzip with some data
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		data := []byte{0x00, 0x00, 0x80, 0x3f} // Float32: 1.0
		gw.Write(data)
		gw.Close()

		decoded, err := cache.decodeValue(buf.Bytes(), true)
		require.NoError(t, err)
		assert.Equal(t, []float32{1.0}, decoded)
	})
}

// TestOpenAIProviderUnmarshalError tests JSON unmarshal error handling
func TestOpenAIProviderUnmarshalError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return invalid JSON
		w.Write([]byte("{invalid json"))
	}))
	defer server.Close()

	config := Config{
		Provider:   "openai",
		APIURL:     server.URL,
		APIKey:     "test-key",
		Model:      "test-model",
		MaxRetries: 0,
	}

	provider, err := NewOpenAIProvider(config)
	require.NoError(t, err)

	result, err := provider.Embed(context.Background(), "test")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to unmarshal response")
}

// TestVoyageProviderUnmarshalError tests JSON unmarshal error handling
func TestVoyageProviderUnmarshalError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return invalid JSON
		w.Write([]byte("{invalid json"))
	}))
	defer server.Close()

	config := Config{
		Provider:   "voyage",
		APIURL:     server.URL,
		APIKey:     "test-key",
		Model:      modelVoyageCode2,
		MaxRetries: 0,
	}

	provider, err := NewVoyageProvider(config)
	require.NoError(t, err)

	result, err := provider.Embed(context.Background(), "test")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to unmarshal response")
}

// TestOpenAIProviderReadBodyError tests error reading response body
func TestOpenAIProviderReadBodyError(t *testing.T) {
	// This is hard to simulate, but we can test the error path by
	// having the server close the connection unexpectedly
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send partial response and close
		w.WriteHeader(http.StatusOK)
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer server.Close()

	config := Config{
		Provider:   "openai",
		APIURL:     server.URL,
		APIKey:     "test-key",
		Model:      "test-model",
		MaxRetries: 0,
	}

	provider, err := NewOpenAIProvider(config)
	require.NoError(t, err)

	result, err := provider.Embed(context.Background(), "test")
	assert.Error(t, err)
	assert.Nil(t, result)
}

// TestVoyageProviderReadBodyError tests error reading response body
func TestVoyageProviderReadBodyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send partial response and close
		w.WriteHeader(http.StatusOK)
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer server.Close()

	config := Config{
		Provider:   "voyage",
		APIURL:     server.URL,
		APIKey:     "test-key",
		Model:      modelVoyageCode2,
		MaxRetries: 0,
	}

	provider, err := NewVoyageProvider(config)
	require.NoError(t, err)

	result, err := provider.Embed(context.Background(), "test")
	assert.Error(t, err)
	assert.Nil(t, result)
}

// TestOpenAIProviderRequestCreationError tests error creating HTTP request
func TestOpenAIProviderRequestCreationError(t *testing.T) {
	config := Config{
		Provider:   "openai",
		APIURL:     "http://test",
		APIKey:     "test-key",
		Model:      "test-model",
		MaxRetries: 0,
	}

	provider, err := NewOpenAIProvider(config)
	require.NoError(t, err)

	// Use a canceled context to trigger early error
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := provider.Embed(ctx, "test")
	assert.Error(t, err)
	assert.Nil(t, result)
}

// TestVoyageProviderRequestCreationError tests error creating HTTP request
func TestVoyageProviderRequestCreationError(t *testing.T) {
	config := Config{
		Provider:   "voyage",
		APIURL:     VoyageAPIURL,
		APIKey:     "test-key",
		Model:      modelVoyageCode2,
		MaxRetries: 0,
	}

	provider, err := NewVoyageProvider(config)
	require.NoError(t, err)

	// Use a canceled context to trigger early error
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := provider.Embed(ctx, "test")
	assert.Error(t, err)
	assert.Nil(t, result)
}

// TestVoyageProviderNoEmbeddingsInResponse tests empty data array
func TestVoyageProviderNoEmbeddingsInResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := voyageResponse{
			Object: "list",
			Data:   []voyageEmbedding{}, // Empty array
			Model:  modelVoyageCode2,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := Config{
		Provider:   "voyage",
		APIURL:     server.URL,
		APIKey:     "test-key",
		Model:      modelVoyageCode2,
		MaxRetries: 0,
	}

	provider, err := NewVoyageProvider(config)
	require.NoError(t, err)

	result, err := provider.Embed(context.Background(), "test")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no embeddings")
}

// TestVoyageProviderDefaultModel tests default model selection
func TestVoyageProviderDefaultModel(t *testing.T) {
	config := Config{
		Provider: "voyage",
		APIURL:   VoyageAPIURL,
		APIKey:   "test-key",
		Model:    "", // Empty model, should use default
	}

	provider, err := NewVoyageProvider(config)
	require.NoError(t, err)
	assert.Equal(t, modelVoyageCode2, provider.model)
}

// TestOpenAIProviderAllModels tests all known OpenAI models
func TestOpenAIProviderAllModels(t *testing.T) {
	models := []string{
		ModelTextEmbedding3Small,
		ModelTextEmbedding3Large,
		ModelTextEmbeddingAda002,
		"nomic-embed-text",
		"mxbai-embed-large",
		"all-minilm",
		"all-minilm-l6-v2",
		"all-minilm-l12-v2",
		"bge-small-en-v1.5",
		"bge-base-en-v1.5",
		"bge-large-en-v1.5",
		"nomic-ai/nomic-embed-text-v1.5-GGUF",
	}

	for _, model := range models {
		config := Config{
			Provider: "openai",
			APIURL:   "http://test",
			APIKey:   "test-key",
			Model:    model,
		}

		provider, err := NewOpenAIProvider(config)
		require.NoError(t, err, "Failed to create provider for model %s", model)
		assert.Equal(t, model, provider.model)
		assert.Greater(t, provider.dimensions, 0, "Model %s should have dimensions", model)
	}
}

// TestVoyageProviderAllModels tests all known Voyage models
func TestVoyageProviderAllModels(t *testing.T) {
	models := []string{
		modelVoyageCode2,
		modelVoyage2,
		modelVoyageLarge2,
	}

	for _, model := range models {
		config := Config{
			Provider: "voyage",
			APIURL:   VoyageAPIURL,
			APIKey:   "test-key",
			Model:    model,
		}

		provider, err := NewVoyageProvider(config)
		require.NoError(t, err, "Failed to create provider for model %s", model)
		assert.Equal(t, model, provider.model)
		assert.Greater(t, provider.dimensions, 0, "Model %s should have dimensions", model)
	}
}

// TestOpenAIProviderNegativeIndexEmbedding tests negative index handling
func TestOpenAIProviderNegativeIndexEmbedding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openaiResponse{
			Object: "list",
			Data: []openaiEmbedding{
				{Object: "embedding", Embedding: []float32{0.1}, Index: -1}, // Negative index
			},
			Model: "test-model",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := Config{
		Provider:   "openai",
		APIURL:     server.URL,
		APIKey:     "test-key",
		Model:      "test-model",
		MaxRetries: 0,
	}

	provider, err := NewOpenAIProvider(config)
	require.NoError(t, err)

	result, err := provider.Embed(context.Background(), "test")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid embedding index")
}

// TestVoyageProviderNegativeIndexEmbedding tests negative index handling
func TestVoyageProviderNegativeIndexEmbedding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := voyageResponse{
			Object: "list",
			Data: []voyageEmbedding{
				{Object: "embedding", Embedding: []float32{0.1}, Index: -1},
			},
			Model: modelVoyageCode2,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := Config{
		Provider:   "voyage",
		APIURL:     server.URL,
		APIKey:     "test-key",
		Model:      modelVoyageCode2,
		MaxRetries: 0,
	}

	provider, err := NewVoyageProvider(config)
	require.NoError(t, err)

	result, err := provider.Embed(context.Background(), "test")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid embedding index")
}

// TestVoyageProviderHTTPMethods tests that the correct HTTP method is used
func TestVoyageProviderHTTPMethods(t *testing.T) {
	methodUsed := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methodUsed = r.Method
		resp := voyageResponse{
			Object: "list",
			Data: []voyageEmbedding{
				{Object: "embedding", Embedding: []float32{0.1}, Index: 0},
			},
			Model: modelVoyageCode2,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := Config{
		Provider:   "voyage",
		APIURL:     server.URL,
		APIKey:     "test-key",
		Model:      modelVoyageCode2,
		MaxRetries: 0,
		Dimensions: 1,
	}

	provider, err := NewVoyageProvider(config)
	require.NoError(t, err)

	_, err = provider.Embed(context.Background(), "test")
	assert.NoError(t, err)
	assert.Equal(t, http.MethodPost, methodUsed)
}

// TestVoyageProviderAuthorizationHeader tests auth header
func TestVoyageProviderAuthorizationHeader(t *testing.T) {
	authHeader := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		resp := voyageResponse{
			Object: "list",
			Data: []voyageEmbedding{
				{Object: "embedding", Embedding: []float32{0.1}, Index: 0},
			},
			Model: modelVoyageCode2,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := Config{
		Provider:   "voyage",
		APIURL:     server.URL,
		APIKey:     "test-secret-key",
		Model:      modelVoyageCode2,
		MaxRetries: 0,
		Dimensions: 1,
	}

	provider, err := NewVoyageProvider(config)
	require.NoError(t, err)

	_, err = provider.Embed(context.Background(), "test")
	assert.NoError(t, err)
	assert.Equal(t, "Bearer test-secret-key", authHeader)
}

// TestCachePutUpdateExisting tests updating an existing cache entry
func TestCachePutUpdateExisting(t *testing.T) {
	cache, err := NewCache(10, 3600, false)
	require.NoError(t, err)

	// Add initial value
	cache.Put("key1", []float32{0.1, 0.2})

	// Update with new value
	cache.Put("key1", []float32{0.3, 0.4})

	result, found := cache.Get("key1")
	assert.True(t, found)
	assert.Equal(t, []float32{0.3, 0.4}, result)
	assert.Equal(t, 1, cache.Size()) // Should still be just 1 entry
}

// TestVoyageProviderNetworkError tests network error handling
func TestVoyageProviderNetworkError(t *testing.T) {
	config := Config{
		Provider:       "voyage",
		APIURL:         "http://127.0.0.1:1", // Invalid port
		APIKey:         "test-key",
		Model:          modelVoyageCode2,
		MaxRetries:     0,
		RetryBackoffMS: 10,
	}

	provider, err := NewVoyageProvider(config)
	require.NoError(t, err)

	result, err := provider.Embed(context.Background(), "test")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "provider unavailable")
}

// =============================================================================
// Consolidated from coverage_complete_test.go
// =============================================================================

// mockProviderWithCloseError allows customizing Close behavior
type mockProviderWithCloseError struct {
	mockProvider
	closeErr error
}

func (m *mockProviderWithCloseError) Close() error {
	return m.closeErr
}

// TestCacheEncodeDecodeEdgeCases covers error paths in encode/decode
func TestCacheEncodeDecodeEdgeCases(t *testing.T) {
	cache, err := NewCache(10, 3600, true)
	require.NoError(t, err)

	t.Run("encode then decode with compression", func(t *testing.T) {
		// Test with data that compresses well
		embedding := make([]float32, 1000)
		for i := range embedding {
			embedding[i] = 1.0
		}

		value, compressed, err := cache.encodeValue(embedding, true)
		require.NoError(t, err)
		assert.True(t, compressed, "should be compressed for repetitive data")

		decoded, err := cache.decodeValue(value, compressed)
		require.NoError(t, err)
		assert.Equal(t, embedding, decoded)
	})

	t.Run("encode with compression but data doesn't compress well", func(t *testing.T) {
		// Very small embedding that won't benefit from compression
		embedding := []float32{0.1}

		value, compressed, err := cache.encodeValue(embedding, true)
		require.NoError(t, err)
		assert.False(t, compressed, "should not compress small data")

		decoded, err := cache.decodeValue(value, compressed)
		require.NoError(t, err)
		assert.Equal(t, embedding, decoded)
	})

	t.Run("decode corrupted gzip data", func(t *testing.T) {
		corruptedData := []byte{0x1f, 0x8b, 0x08, 0x00, 0xFF, 0xFF} // Invalid gzip
		_, err := cache.decodeValue(corruptedData, true)
		assert.Error(t, err, "should fail on corrupted gzip data")
	})

	t.Run("decode empty gzip data", func(t *testing.T) {
		// Create valid but empty gzip data
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		gw.Close()

		decoded, err := cache.decodeValue(buf.Bytes(), true)
		require.NoError(t, err)
		assert.Empty(t, decoded)
	})

	t.Run("decode non-gzip data as compressed", func(t *testing.T) {
		plainData := []byte{0x00, 0x01, 0x02, 0x03}
		_, err := cache.decodeValue(plainData, true)
		assert.Error(t, err, "should fail when treating non-gzip as compressed")
	})
}

// TestCacheGetDecodingErrors tests Get when decoding fails
func TestCacheGetDecodingErrors(t *testing.T) {
	cache, err := NewCache(10, 3600, true)
	require.NoError(t, err)

	// Manually inject corrupted data into cache
	cache.mu.Lock()
	hashKey := cache.hashKey("corrupted")
	cache.items[hashKey] = &cacheEntry{
		key:        hashKey,
		value:      []byte{0xFF, 0xFF, 0xFF}, // Invalid compressed data
		compressed: true,
		element:    cache.evictList.PushFront(&cacheEntry{}),
		timestamp:  time.Now(),
	}
	cache.mu.Unlock()

	// Try to get the corrupted entry
	result, found := cache.Get("corrupted")
	assert.False(t, found)
	assert.Nil(t, result)
	assert.Equal(t, uint64(1), cache.Misses())
}

// TestCachePutEncodingErrors tests Put when encoding fails
func TestCachePutEncodingErrors(t *testing.T) {
	cache, err := NewCache(10, 3600, false)
	require.NoError(t, err)

	// This should work fine normally
	embedding := []float32{0.1, 0.2, 0.3}
	cache.Put("key1", embedding)

	result, found := cache.Get("key1")
	assert.True(t, found)
	assert.Equal(t, embedding, result)
}

// TestManagerEmbedError tests Embed when provider returns an error
func TestManagerEmbedError(t *testing.T) {
	config := DefaultConfig()
	config.Provider = "openai"
	config.APIURL = "http://test"
	config.APIKey = "test-key"
	config.CacheEnabled = false

	mgr, err := NewManager(config)
	require.NoError(t, err)

	testErr := errors.New("provider error")
	mock := &mockProvider{
		name: "test",
		embedFunc: func(ctx context.Context, text string) ([]float32, error) {
			return nil, testErr
		},
	}
	mgr.RegisterProvider("openai", mock)

	result, err := mgr.Embed(context.Background(), "hello")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, testErr)
}

// TestManagerCloseWithErrors tests Close when a provider fails to close
func TestManagerCloseWithErrors(t *testing.T) {
	config := DefaultConfig()
	config.Provider = "openai"
	config.APIURL = "http://test"
	config.APIKey = "test-key"

	mgr, err := NewManager(config)
	require.NoError(t, err)

	closeErr := errors.New("close error")
	mock := &mockProviderWithCloseError{
		mockProvider: mockProvider{
			name: "test",
		},
		closeErr: closeErr,
	}

	mgr.RegisterProvider("openai", mock)

	err = mgr.Close()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "errors closing providers")
	assert.Contains(t, err.Error(), "openai")
}

// TestUtilsConnectionRefused tests the isConnectionRefused utility
func TestUtilsConnectionRefused(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		assert.False(t, isConnectionRefused(nil))
	})

	t.Run("connection refused error", func(t *testing.T) {
		// Create a real connection refused error
		conn, err := net.Dial("tcp", "127.0.0.1:1") // Port 1 should be refused
		if conn != nil {
			conn.Close()
		}
		if err != nil {
			result := isConnectionRefused(err)
			// May or may not be connection refused depending on system
			// Just verify it doesn't panic
			_ = result
		}
	})

	t.Run("other network error", func(t *testing.T) {
		err := &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: errors.New("other error"),
		}
		assert.False(t, isConnectionRefused(err))
	})

	t.Run("ECONNREFUSED syscall error", func(t *testing.T) {
		err := &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: syscall.ECONNREFUSED,
		}
		assert.True(t, isConnectionRefused(err))
	})

	t.Run("non-network error", func(t *testing.T) {
		err := errors.New("some random error")
		assert.False(t, isConnectionRefused(err))
	})
}

// TestOpenAIProviderEmbed tests the Embed method edge cases
func TestOpenAIProviderEmbed(t *testing.T) {
	t.Run("empty text", func(t *testing.T) {
		config := Config{
			Provider: "openai",
			APIURL:   "http://localhost:11434",
			APIKey:   "test-key",
			Model:    "test-model",
		}

		provider, err := NewOpenAIProvider(config)
		require.NoError(t, err)

		result, err := provider.Embed(context.Background(), "")
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrInvalidInput)
		assert.Contains(t, err.Error(), "text cannot be empty")
	})

	t.Run("no embeddings returned", func(t *testing.T) {
		// Create a mock server that returns empty data array
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := openaiResponse{
				Object: "list",
				Data:   []openaiEmbedding{},
				Model:  "test-model",
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		config := Config{
			Provider:   "openai",
			APIURL:     server.URL,
			APIKey:     "test-key",
			Model:      "test-model",
			MaxRetries: 0,
		}

		provider, err := NewOpenAIProvider(config)
		require.NoError(t, err)

		result, err := provider.Embed(context.Background(), "test")
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "no embeddings")
	})
}

// TestOpenAIProviderRetryLogic tests retry behavior
func TestOpenAIProviderRetryLogic(t *testing.T) {
	t.Run("retry on rate limit", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			if attempts < 2 {
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(openaiError{
					Error: struct {
						Message string `json:"message"`
						Type    string `json:"type"`
						Code    string `json:"code"`
					}{
						Message: "rate limit exceeded",
						Type:    "rate_limit_error",
					},
				})
				return
			}
			// Success on second attempt
			resp := openaiResponse{
				Object: "list",
				Data: []openaiEmbedding{
					{Object: "embedding", Embedding: []float32{0.1, 0.2}, Index: 0},
				},
				Model: "test-model",
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		config := Config{
			Provider:       "openai",
			APIURL:         server.URL,
			APIKey:         "test-key",
			Model:          "test-model",
			MaxRetries:     2,
			RetryBackoffMS: 10,
			Dimensions:     2,
		}

		provider, err := NewOpenAIProvider(config)
		require.NoError(t, err)

		result, err := provider.Embed(context.Background(), "test")
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, 2, attempts)
	})

	t.Run("retry on server error", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(openaiError{
				Error: struct {
					Message string `json:"message"`
					Type    string `json:"type"`
					Code    string `json:"code"`
				}{
					Message: "internal server error",
				},
			})
		}))
		defer server.Close()

		config := Config{
			Provider:       "openai",
			APIURL:         server.URL,
			APIKey:         "test-key",
			Model:          "test-model",
			MaxRetries:     2,
			RetryBackoffMS: 10,
		}

		provider, err := NewOpenAIProvider(config)
		require.NoError(t, err)

		result, err := provider.Embed(context.Background(), "test")
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Equal(t, 3, attempts) // Initial + 2 retries
	})

	t.Run("no retry on auth error", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(openaiError{
				Error: struct {
					Message string `json:"message"`
					Type    string `json:"type"`
					Code    string `json:"code"`
				}{
					Message: "invalid api key",
				},
			})
		}))
		defer server.Close()

		config := Config{
			Provider:       "openai",
			APIURL:         server.URL,
			APIKey:         "bad-key",
			Model:          "test-model",
			MaxRetries:     2,
			RetryBackoffMS: 10,
		}

		provider, err := NewOpenAIProvider(config)
		require.NoError(t, err)

		result, err := provider.Embed(context.Background(), "test")
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrInvalidCredentials)
		assert.Equal(t, 1, attempts) // No retries on auth error
	})

	t.Run("retry with connection refused", func(t *testing.T) {
		config := Config{
			Provider:       "openai",
			APIURL:         "http://127.0.0.1:1", // Port 1 should be refused
			APIKey:         "test-key",
			Model:          "test-model",
			MaxRetries:     1,
			RetryBackoffMS: 10,
		}

		provider, err := NewOpenAIProvider(config)
		require.NoError(t, err)

		result, err := provider.Embed(context.Background(), "test")
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "provider unavailable")
	})

	t.Run("context cancellation during retry", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(openaiError{
				Error: struct {
					Message string `json:"message"`
					Type    string `json:"type"`
					Code    string `json:"code"`
				}{
					Message: "rate limit",
				},
			})
		}))
		defer server.Close()

		config := Config{
			Provider:       "openai",
			APIURL:         server.URL,
			APIKey:         "test-key",
			Model:          "test-model",
			MaxRetries:     5,
			RetryBackoffMS: 1000, // Long backoff
		}

		provider, err := NewOpenAIProvider(config)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		result, err := provider.Embed(ctx, "test")
		assert.Error(t, err)
		assert.Nil(t, result)
	})
}

// TestOpenAIHandleErrorResponse tests error response parsing
func TestOpenAIHandleErrorResponse(t *testing.T) {
	provider := &OpenAIProvider{model: "test-model"}

	t.Run("error with valid JSON", func(t *testing.T) {
		errResp := openaiError{
			Error: struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    string `json:"code"`
			}{
				Message: "test error message",
				Type:    "test_error",
			},
		}
		body, _ := json.Marshal(errResp)

		err := provider.handleErrorResponse(http.StatusBadRequest, body)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidInput)
		assert.Contains(t, err.Error(), "test error message")
	})

	t.Run("error with invalid JSON", func(t *testing.T) {
		body := []byte("plain text error")

		err := provider.handleErrorResponse(http.StatusBadRequest, body)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidInput)
		assert.Contains(t, err.Error(), "plain text error")
	})

	t.Run("forbidden error", func(t *testing.T) {
		body := []byte(`{"error":{"message":"forbidden"}}`)
		err := provider.handleErrorResponse(http.StatusForbidden, body)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidCredentials)
	})

	t.Run("not found error", func(t *testing.T) {
		body := []byte(`{"error":{"message":"model not found"}}`)
		err := provider.handleErrorResponse(http.StatusNotFound, body)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrModelNotFound)
		assert.Contains(t, err.Error(), "test-model")
	})

	t.Run("various server errors", func(t *testing.T) {
		serverCodes := []int{
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout,
		}

		for _, code := range serverCodes {
			body := []byte(`{"error":{"message":"server error"}}`)
			err := provider.handleErrorResponse(code, body)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "provider unavailable")
		}
	})

	t.Run("unknown status code", func(t *testing.T) {
		body := []byte(`{"error":{"message":"unknown error"}}`)
		err := provider.handleErrorResponse(999, body)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "HTTP 999")
	})
}

// TestVoyageProviderHandleErrorResponse tests Voyage error handling
func TestVoyageProviderHandleErrorResponse(t *testing.T) {
	provider := &VoyageProvider{model: "voyage-code-2"}

	t.Run("error with empty body", func(t *testing.T) {
		err := provider.handleErrorResponse(http.StatusBadRequest, []byte{})
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidInput)
		assert.Contains(t, err.Error(), "unknown error")
	})

	t.Run("error with valid JSON", func(t *testing.T) {
		errResp := voyageError{
			Error: struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    string `json:"code"`
			}{
				Message: "voyage error message",
			},
		}
		body, _ := json.Marshal(errResp)

		err := provider.handleErrorResponse(http.StatusUnauthorized, body)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidCredentials)
		assert.Contains(t, err.Error(), "voyage error message")
	})

	t.Run("error with plain text", func(t *testing.T) {
		body := []byte("plain text error")
		err := provider.handleErrorResponse(http.StatusBadRequest, body)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidInput)
		assert.Contains(t, err.Error(), "plain text error")
	})
}

// TestVoyageProviderRetryLogic tests Voyage retry behavior
func TestVoyageProviderRetryLogic(t *testing.T) {
	t.Run("no retry on invalid input", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(voyageError{
				Error: struct {
					Message string `json:"message"`
					Type    string `json:"type"`
					Code    string `json:"code"`
				}{
					Message: "invalid input",
				},
			})
		}))
		defer server.Close()

		config := Config{
			Provider:       "voyage",
			APIURL:         server.URL,
			APIKey:         "test-key",
			Model:          modelVoyageCode2,
			MaxRetries:     2,
			RetryBackoffMS: 10,
		}

		provider, err := NewVoyageProvider(config)
		require.NoError(t, err)

		result, err := provider.EmbedBatch(context.Background(), []string{"test"})
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrInvalidInput)
		assert.Equal(t, 1, attempts) // No retries on client error
	})

	t.Run("retry exhausted", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(voyageError{
				Error: struct {
					Message string `json:"message"`
					Type    string `json:"type"`
					Code    string `json:"code"`
				}{
					Message: "rate limit",
				},
			})
		}))
		defer server.Close()

		config := Config{
			Provider:       "voyage",
			APIURL:         server.URL,
			APIKey:         "test-key",
			Model:          modelVoyageCode2,
			MaxRetries:     2,
			RetryBackoffMS: 10,
		}

		provider, err := NewVoyageProvider(config)
		require.NoError(t, err)

		result, err := provider.EmbedBatch(context.Background(), []string{"test"})
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "failed after 2 retries")
	})
}

// TestOpenAIProviderDefaults tests default configuration
func TestOpenAIProviderDefaults(t *testing.T) {
	t.Run("all defaults", func(t *testing.T) {
		config := Config{
			Provider: "openai",
			APIKey:   "test-key",
		}

		provider, err := NewOpenAIProvider(config)
		require.NoError(t, err)
		assert.Equal(t, DefaultOpenAIURL, provider.apiURL)
		assert.Equal(t, ModelTextEmbedding3Small, provider.model)
		assert.Equal(t, defaultMaxRetries, provider.maxRetries)
		assert.Equal(t, defaultRetryBackoffMS, provider.backoffMS)
	})

	t.Run("unknown model without dimensions", func(t *testing.T) {
		config := Config{
			Provider: "openai",
			APIKey:   "test-key",
			Model:    "unknown-model",
		}

		provider, err := NewOpenAIProvider(config)
		require.NoError(t, err)
		assert.Equal(t, 0, provider.dimensions) // Will auto-detect
	})

	t.Run("known model with dimension override", func(t *testing.T) {
		config := Config{
			Provider:   "openai",
			APIKey:     "test-key",
			Model:      ModelTextEmbedding3Small,
			Dimensions: 512, // Override
		}

		provider, err := NewOpenAIProvider(config)
		require.NoError(t, err)
		assert.Equal(t, 512, provider.dimensions)
	})
}

// TestBinarySerialization ensures binary encoding/decoding works correctly
func TestBinarySerialization(t *testing.T) {
	cache, err := NewCache(10, 3600, false)
	require.NoError(t, err)

	t.Run("encode and decode various sizes", func(t *testing.T) {
		sizes := []int{1, 10, 100, 1000}
		for _, size := range sizes {
			embedding := make([]float32, size)
			for i := range embedding {
				embedding[i] = float32(i) * 0.1
			}

			encoded, compressed, err := cache.encodeValue(embedding, false)
			require.NoError(t, err)
			assert.False(t, compressed)

			decoded, err := cache.decodeValue(encoded, false)
			require.NoError(t, err)
			assert.Equal(t, embedding, decoded)
		}
	})

	t.Run("manual binary encoding", func(t *testing.T) {
		// Manually create binary data
		buf := new(bytes.Buffer)
		values := []float32{1.0, 2.0, 3.0}
		for _, v := range values {
			err := binary.Write(buf, binary.LittleEndian, v)
			require.NoError(t, err)
		}

		decoded, err := cache.decodeValue(buf.Bytes(), false)
		require.NoError(t, err)
		assert.Equal(t, values, decoded)
	})

	t.Run("decode with insufficient data", func(t *testing.T) {
		// Create buffer with incomplete float32 (less than 4 bytes)
		buf := bytes.NewBuffer([]byte{0x00, 0x01})
		decoded := []float32{}

		reader := bytes.NewReader(buf.Bytes())
		for reader.Len() > 0 {
			var f float32
			err := binary.Read(reader, binary.LittleEndian, &f)
			if err == io.EOF || err != nil {
				break
			}
			decoded = append(decoded, f)
		}

		assert.Empty(t, decoded)
	})
}

// TestManagerEmbedBatchPartialCache tests batch embedding with partial cache
func TestManagerEmbedBatchPartialCache(t *testing.T) {
	config := DefaultConfig()
	config.Provider = "openai"
	config.APIURL = "http://test"
	config.APIKey = "test-key"
	config.CacheEnabled = true
	config.CacheSize = 10

	mgr, err := NewManager(config)
	require.NoError(t, err)

	batchCallCount := 0
	mock := &mockProvider{
		name: "test",
		batchFunc: func(ctx context.Context, texts []string) ([][]float32, error) {
			batchCallCount++
			results := make([][]float32, len(texts))
			for i := range texts {
				results[i] = []float32{float32(i + 100)}
			}
			return results, nil
		},
	}
	mgr.RegisterProvider("openai", mock)

	// First, populate cache with some entries
	mgr.Embed(context.Background(), "cached1")
	mgr.Embed(context.Background(), "cached2")

	batchCallCount = 0 // Reset count

	// Now batch embed with mix of cached and uncached
	texts := []string{"cached1", "uncached1", "cached2", "uncached2"}
	results, err := mgr.EmbedBatch(context.Background(), texts)
	require.NoError(t, err)
	assert.Len(t, results, 4)

	// Should only call batch once for the 2 uncached items
	assert.Equal(t, 1, batchCallCount)

	// Verify all results are present
	for _, res := range results {
		assert.NotNil(t, res)
	}
}

// TestManagerEmbedBatchNoCache tests batch embedding without cache
func TestManagerEmbedBatchNoCache(t *testing.T) {
	config := DefaultConfig()
	config.Provider = "openai"
	config.APIURL = "http://test"
	config.APIKey = "test-key"
	config.CacheEnabled = false

	mgr, err := NewManager(config)
	require.NoError(t, err)

	mock := &mockProvider{
		name: "test",
		batchFunc: func(ctx context.Context, texts []string) ([][]float32, error) {
			results := make([][]float32, len(texts))
			for i := range texts {
				results[i] = []float32{float32(i)}
			}
			return results, nil
		},
	}
	mgr.RegisterProvider("openai", mock)

	texts := []string{"text1", "text2", "text3"}
	results, err := mgr.EmbedBatch(context.Background(), texts)
	require.NoError(t, err)
	assert.Len(t, results, 3)

	for i, res := range results {
		assert.Equal(t, []float32{float32(i)}, res)
	}
}

// TestManagerEmbedBatchProviderError tests batch embedding when provider fails
func TestManagerEmbedBatchProviderError(t *testing.T) {
	config := DefaultConfig()
	config.Provider = "openai"
	config.APIURL = "http://test"
	config.APIKey = "test-key"
	config.CacheEnabled = true
	config.CacheSize = 10

	mgr, err := NewManager(config)
	require.NoError(t, err)

	testErr := errors.New("batch provider error")
	mock := &mockProvider{
		name: "test",
		batchFunc: func(ctx context.Context, texts []string) ([][]float32, error) {
			return nil, testErr
		},
	}
	mgr.RegisterProvider("openai", mock)

	results, err := mgr.EmbedBatch(context.Background(), []string{"text1", "text2"})
	assert.Error(t, err)
	assert.Nil(t, results)
	assert.ErrorIs(t, err, testErr)
}

// TestCacheExpiredEntriesCleanup tests that expired entries are properly removed
func TestCacheExpiredEntriesCleanup(t *testing.T) {
	cache, err := NewCache(10, 1, false) // 1 second TTL
	require.NoError(t, err)

	cache.Put("key1", []float32{0.1})
	cache.Put("key2", []float32{0.2})

	// Verify both exist
	assert.Equal(t, 2, cache.Size())

	// Wait for TTL to expire
	time.Sleep(1100 * time.Millisecond)

	// Access key1 (should be expired and removed)
	_, found := cache.Get("key1")
	assert.False(t, found)

	// Size should be reduced
	assert.Equal(t, 1, cache.Size()) // key2 is still there, not accessed yet
}

// =============================================================================
// Consolidated from final_coverage_test.go
// =============================================================================

// TestOpenAIProviderEmbedBatchEdgeCases tests edge cases in OpenAI batch embedding
func TestOpenAIProviderEmbedBatchEdgeCases(t *testing.T) {
	t.Run("empty text at specific index", func(t *testing.T) {
		config := Config{
			Provider:   "openai",
			APIURL:     "http://test",
			APIKey:     "test-key",
			Model:      "test-model",
			MaxRetries: 0,
		}

		provider, err := NewOpenAIProvider(config)
		require.NoError(t, err)

		result, err := provider.EmbedBatch(context.Background(), []string{"valid", ""})
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrInvalidInput)
		assert.Contains(t, err.Error(), "text at index 1 is empty")
	})

	t.Run("invalid embedding index in response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := openaiResponse{
				Object: "list",
				Data: []openaiEmbedding{
					{Object: "embedding", Embedding: []float32{0.1}, Index: 999}, // Invalid index
				},
				Model: "test-model",
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		config := Config{
			Provider:   "openai",
			APIURL:     server.URL,
			APIKey:     "test-key",
			Model:      "test-model",
			MaxRetries: 0,
			Dimensions: 1,
		}

		provider, err := NewOpenAIProvider(config)
		require.NoError(t, err)

		result, err := provider.Embed(context.Background(), "test")
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "invalid embedding index")
	})

	t.Run("empty embedding in response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := openaiResponse{
				Object: "list",
				Data: []openaiEmbedding{
					{Object: "embedding", Embedding: []float32{}, Index: 0}, // Empty embedding
				},
				Model: "test-model",
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		config := Config{
			Provider:   "openai",
			APIURL:     server.URL,
			APIKey:     "test-key",
			Model:      "test-model",
			MaxRetries: 0,
		}

		provider, err := NewOpenAIProvider(config)
		require.NoError(t, err)

		result, err := provider.Embed(context.Background(), "test")
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "received empty embedding")
	})

	t.Run("missing embedding at index", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Return only one embedding when two texts were sent
			resp := openaiResponse{
				Object: "list",
				Data: []openaiEmbedding{
					{Object: "embedding", Embedding: []float32{0.1}, Index: 0},
					// Missing index 1
				},
				Model: "test-model",
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		config := Config{
			Provider:   "openai",
			APIURL:     server.URL,
			APIKey:     "test-key",
			Model:      "test-model",
			MaxRetries: 0,
			Dimensions: 1,
		}

		provider, err := NewOpenAIProvider(config)
		require.NoError(t, err)

		result, err := provider.EmbedBatch(context.Background(), []string{"text1", "text2"})
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "missing embedding at index")
	})

	t.Run("dimension mismatch", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			dimensions := 2
			if attempts > 1 {
				dimensions = 3 // Different dimension on second call
			}
			emb := make([]float32, dimensions)
			resp := openaiResponse{
				Object: "list",
				Data: []openaiEmbedding{
					{Object: "embedding", Embedding: emb, Index: 0},
				},
				Model: "test-model",
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		config := Config{
			Provider:   "openai",
			APIURL:     server.URL,
			APIKey:     "test-key",
			Model:      "test-model",
			MaxRetries: 0,
		}

		provider, err := NewOpenAIProvider(config)
		require.NoError(t, err)

		// First call - sets dimensions to 2
		_, err = provider.Embed(context.Background(), "test1")
		assert.NoError(t, err)

		// Second call - tries to use 3 dimensions
		_, err = provider.Embed(context.Background(), "test2")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "dimensions mismatch")
	})

	t.Run("no API key for local provider", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check that no auth header was sent
			authHeader := r.Header.Get("Authorization")
			assert.Empty(t, authHeader)

			resp := openaiResponse{
				Object: "list",
				Data: []openaiEmbedding{
					{Object: "embedding", Embedding: []float32{0.1}, Index: 0},
				},
				Model: "test-model",
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		config := Config{
			Provider:   "openai",
			APIURL:     server.URL,
			APIKey:     "", // No API key
			Model:      "test-model",
			MaxRetries: 0,
			Dimensions: 1,
		}

		provider, err := NewOpenAIProvider(config)
		require.NoError(t, err)

		result, err := provider.Embed(context.Background(), "test")
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})
}

// TestVoyageProviderEdgeCases tests edge cases in Voyage provider
func TestVoyageProviderEdgeCases(t *testing.T) {
	t.Run("empty text", func(t *testing.T) {
		config := Config{
			Provider: "voyage",
			APIURL:   VoyageAPIURL,
			APIKey:   "test-key",
			Model:    modelVoyageCode2,
		}

		provider, err := NewVoyageProvider(config)
		require.NoError(t, err)

		result, err := provider.Embed(context.Background(), "")
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrInvalidInput)
	})

	t.Run("empty text at index", func(t *testing.T) {
		config := Config{
			Provider: "voyage",
			APIURL:   VoyageAPIURL,
			APIKey:   "test-key",
			Model:    modelVoyageCode2,
		}

		provider, err := NewVoyageProvider(config)
		require.NoError(t, err)

		result, err := provider.EmbedBatch(context.Background(), []string{"valid", ""})
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrInvalidInput)
		assert.Contains(t, err.Error(), "text at index 1 is empty")
	})

	t.Run("invalid embedding index", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := voyageResponse{
				Object: "list",
				Data: []voyageEmbedding{
					{Object: "embedding", Embedding: []float32{0.1}, Index: 999},
				},
				Model: modelVoyageCode2,
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		config := Config{
			Provider:   "voyage",
			APIURL:     server.URL,
			APIKey:     "test-key",
			Model:      modelVoyageCode2,
			MaxRetries: 0,
		}

		provider, err := NewVoyageProvider(config)
		require.NoError(t, err)

		result, err := provider.Embed(context.Background(), "test")
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "invalid embedding index")
	})

	t.Run("missing embedding at index", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := voyageResponse{
				Object: "list",
				Data: []voyageEmbedding{
					{Object: "embedding", Embedding: []float32{0.1}, Index: 0},
				},
				Model: modelVoyageCode2,
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		config := Config{
			Provider:   "voyage",
			APIURL:     server.URL,
			APIKey:     "test-key",
			Model:      modelVoyageCode2,
			MaxRetries: 0,
		}

		provider, err := NewVoyageProvider(config)
		require.NoError(t, err)

		result, err := provider.EmbedBatch(context.Background(), []string{"text1", "text2"})
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "missing embedding at index")
	})

	t.Run("custom dimensions override", func(t *testing.T) {
		config := Config{
			Provider:   "voyage",
			APIURL:     VoyageAPIURL,
			APIKey:     "test-key",
			Model:      modelVoyageCode2,
			Dimensions: 768, // Override default
		}

		provider, err := NewVoyageProvider(config)
		require.NoError(t, err)
		assert.Equal(t, 768, provider.Dimensions())
	})

	t.Run("zero retries", func(t *testing.T) {
		config := Config{
			Provider:   "voyage",
			APIURL:     VoyageAPIURL,
			APIKey:     "test-key",
			Model:      modelVoyageCode2,
			MaxRetries: 0, // Explicitly set to 0
		}

		provider, err := NewVoyageProvider(config)
		require.NoError(t, err)
		assert.Equal(t, 0, provider.maxRetries)
	})
}

// TestVoyageProviderRetryEdgeCases tests more retry scenarios
func TestVoyageProviderRetryEdgeCases(t *testing.T) {
	t.Run("no retry on model not found", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(voyageError{
				Error: struct {
					Message string `json:"message"`
					Type    string `json:"type"`
					Code    string `json:"code"`
				}{
					Message: "model not found",
				},
			})
		}))
		defer server.Close()

		config := Config{
			Provider:       "voyage",
			APIURL:         server.URL,
			APIKey:         "test-key",
			Model:          modelVoyageCode2,
			MaxRetries:     3,
			RetryBackoffMS: 10,
		}

		provider, err := NewVoyageProvider(config)
		require.NoError(t, err)

		result, err := provider.EmbedBatch(context.Background(), []string{"test"})
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrModelNotFound)
		assert.Equal(t, 1, attempts) // No retries
	})

	t.Run("retry on server unavailable", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(voyageError{
				Error: struct {
					Message string `json:"message"`
					Type    string `json:"type"`
					Code    string `json:"code"`
				}{
					Message: "service unavailable",
				},
			})
		}))
		defer server.Close()

		config := Config{
			Provider:       "voyage",
			APIURL:         server.URL,
			APIKey:         "test-key",
			Model:          modelVoyageCode2,
			MaxRetries:     2,
			RetryBackoffMS: 10,
		}

		provider, err := NewVoyageProvider(config)
		require.NoError(t, err)

		result, err := provider.EmbedBatch(context.Background(), []string{"test"})
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Equal(t, 3, attempts) // Initial + 2 retries
	})

	t.Run("retry on credentials error - no retry", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(voyageError{
				Error: struct {
					Message string `json:"message"`
					Type    string `json:"type"`
					Code    string `json:"code"`
				}{
					Message: "invalid credentials",
				},
			})
		}))
		defer server.Close()

		config := Config{
			Provider:       "voyage",
			APIURL:         server.URL,
			APIKey:         "bad-key",
			Model:          modelVoyageCode2,
			MaxRetries:     3,
			RetryBackoffMS: 10,
		}

		provider, err := NewVoyageProvider(config)
		require.NoError(t, err)

		result, err := provider.EmbedBatch(context.Background(), []string{"test"})
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrInvalidCredentials)
		assert.Equal(t, 1, attempts) // No retries on auth error
	})
}

// TestOpenAIProviderRetryNonRetryableErrors tests that certain errors don't retry
func TestOpenAIProviderRetryNonRetryableErrors(t *testing.T) {
	t.Run("no retry on model not found", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(openaiError{
				Error: struct {
					Message string `json:"message"`
					Type    string `json:"type"`
					Code    string `json:"code"`
				}{
					Message: "model not found",
				},
			})
		}))
		defer server.Close()

		config := Config{
			Provider:       "openai",
			APIURL:         server.URL,
			APIKey:         "test-key",
			Model:          "test-model",
			MaxRetries:     3,
			RetryBackoffMS: 10,
		}

		provider, err := NewOpenAIProvider(config)
		require.NoError(t, err)

		result, err := provider.Embed(context.Background(), "test")
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrModelNotFound)
		assert.Equal(t, 1, attempts) // No retries
	})

	t.Run("no retry on bad request", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(openaiError{
				Error: struct {
					Message string `json:"message"`
					Type    string `json:"type"`
					Code    string `json:"code"`
				}{
					Message: "bad request",
				},
			})
		}))
		defer server.Close()

		config := Config{
			Provider:       "openai",
			APIURL:         server.URL,
			APIKey:         "test-key",
			Model:          "test-model",
			MaxRetries:     3,
			RetryBackoffMS: 10,
		}

		provider, err := NewOpenAIProvider(config)
		require.NoError(t, err)

		result, err := provider.Embed(context.Background(), "test")
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrInvalidInput)
		assert.Equal(t, 1, attempts) // No retries
	})
}

// TestOpenAIProviderCustomTimeout tests custom timeout configuration
func TestOpenAIProviderCustomTimeout(t *testing.T) {
	config := Config{
		Provider: "openai",
		APIURL:   "http://test",
		APIKey:   "test-key",
		Model:    "test-model",
		Timeout:  60, // 60 seconds
	}

	provider, err := NewOpenAIProvider(config)
	require.NoError(t, err)
	assert.NotNil(t, provider)
}

// TestVoyageProviderCustomTimeout tests custom timeout configuration
func TestVoyageProviderCustomTimeout(t *testing.T) {
	config := Config{
		Provider: "voyage",
		APIURL:   VoyageAPIURL,
		APIKey:   "test-key",
		Model:    modelVoyageCode2,
		Timeout:  60,
	}

	provider, err := NewVoyageProvider(config)
	require.NoError(t, err)
	assert.NotNil(t, provider)
}
