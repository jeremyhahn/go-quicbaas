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
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
)

func TestIsMCPEmbeddingService(t *testing.T) {
	tests := []struct {
		name     string
		service  string
		expected bool
	}{
		{"fully qualified service", "quicbaas.Embedding", true},
		{"bare service name", "Embedding", true},
		{"nested package prefix", "pkg.quicbaas.Embedding", true},
		{"non-embedding service KV", "quicbaas.KV", false},
		{"non-embedding service Vector", "quicbaas.Vector", false},
		{"empty service", "", false},
		{"partial match EmbeddingService", "quicbaas.EmbeddingService", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isMCPEmbeddingService(tt.service)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- transformRequest tests ---

func TestEmbeddingHandler_TransformGenerateEmbeddingRequest(t *testing.T) {
	h := newEmbeddingHandler()

	t.Run("converts full request with all fields", func(t *testing.T) {
		req := &embedding.GenerateEmbeddingRequest{
			Text:        "hello world",
			Provider:    embedding.ProviderVoyage,
			InputType:   embedding.InputTypeCode,
			Source:      "main.go",
			Language:    "go",
			CacheBypass: true,
		}

		args, err := h.transformRequest("GenerateEmbedding", req)
		require.NoError(t, err)
		assert.Equal(t, "hello world", args["text"])
		assert.Equal(t, "voyage", args["provider"])
		assert.Equal(t, "code", args["input_type"])
		assert.Equal(t, "main.go", args["source"])
		assert.Equal(t, "go", args["language"])
		assert.Equal(t, "true", args["cache_bypass"])
	})

	t.Run("converts minimal request with only required text", func(t *testing.T) {
		req := &embedding.GenerateEmbeddingRequest{
			Text: "embed this",
		}

		args, err := h.transformRequest("GenerateEmbedding", req)
		require.NoError(t, err)
		assert.Equal(t, "embed this", args["text"])
		assert.NotContains(t, args, "provider")
		assert.NotContains(t, args, "input_type")
		assert.NotContains(t, args, "source")
		assert.NotContains(t, args, "language")
		assert.NotContains(t, args, "cache_bypass")
	})

	t.Run("falls back to generic transform for non-SDK type", func(t *testing.T) {
		req := map[string]interface{}{
			"Text": "generic text",
		}

		args, err := h.transformRequest("GenerateEmbedding", req)
		require.NoError(t, err)
		// Generic transform converts keys to snake_case
		assert.Equal(t, "generic text", args["text"])
	})
}

func TestEmbeddingHandler_TransformBatchGenerateEmbeddingsRequest(t *testing.T) {
	h := newEmbeddingHandler()

	t.Run("converts batch request with items as JSON string", func(t *testing.T) {
		req := &embedding.BatchGenerateEmbeddingsRequest{
			Items: []*embedding.BatchEmbeddingItem{
				{
					ID:        "item-1",
					Text:      "first text",
					InputType: embedding.InputTypeDocument,
					Source:    "doc.txt",
					Language:  "en",
				},
				{
					ID:   "item-2",
					Text: "second text",
				},
			},
			Provider:    embedding.ProviderOpenAI,
			CacheBypass: true,
		}

		args, err := h.transformRequest("BatchGenerateEmbeddings", req)
		require.NoError(t, err)

		// items must be a JSON string, not an array
		itemsStr, ok := args["items"].(string)
		require.True(t, ok, "items must be a string, got %T", args["items"])

		// Parse the JSON string to verify structure
		var items []map[string]interface{}
		err = json.Unmarshal([]byte(itemsStr), &items)
		require.NoError(t, err)
		require.Len(t, items, 2)

		assert.Equal(t, "item-1", items[0]["id"])
		assert.Equal(t, "first text", items[0]["text"])
		assert.Equal(t, "document", items[0]["input_type"])
		assert.Equal(t, "doc.txt", items[0]["source"])
		assert.Equal(t, "en", items[0]["language"])

		assert.Equal(t, "item-2", items[1]["id"])
		assert.Equal(t, "second text", items[1]["text"])
		assert.NotContains(t, items[1], "input_type")
		assert.NotContains(t, items[1], "source")
		assert.NotContains(t, items[1], "language")

		assert.Equal(t, "openai", args["provider"])
		assert.Equal(t, "true", args["cache_bypass"])
	})

	t.Run("skips nil items in batch request", func(t *testing.T) {
		req := &embedding.BatchGenerateEmbeddingsRequest{
			Items: []*embedding.BatchEmbeddingItem{
				{ID: "item-1", Text: "valid"},
				nil,
				{ID: "item-3", Text: "also valid"},
			},
		}

		args, err := h.transformRequest("BatchGenerateEmbeddings", req)
		require.NoError(t, err)

		itemsStr := args["items"].(string)
		var items []map[string]interface{}
		err = json.Unmarshal([]byte(itemsStr), &items)
		require.NoError(t, err)
		require.Len(t, items, 2)
		assert.Equal(t, "item-1", items[0]["id"])
		assert.Equal(t, "item-3", items[1]["id"])
	})

	t.Run("converts batch request without optional fields", func(t *testing.T) {
		req := &embedding.BatchGenerateEmbeddingsRequest{
			Items: []*embedding.BatchEmbeddingItem{
				{ID: "only-one", Text: "minimal"},
			},
		}

		args, err := h.transformRequest("BatchGenerateEmbeddings", req)
		require.NoError(t, err)

		assert.Contains(t, args, "items")
		assert.NotContains(t, args, "provider")
		assert.NotContains(t, args, "cache_bypass")
	})

	t.Run("falls back to generic transform for non-SDK type", func(t *testing.T) {
		req := map[string]interface{}{
			"Items": []interface{}{"a", "b"},
		}

		args, err := h.transformRequest("BatchGenerateEmbeddings", req)
		require.NoError(t, err)
		// Generic transform converts keys to snake_case
		assert.Contains(t, args, "items")
	})
}

func TestEmbeddingHandler_TransformListProvidersRequest(t *testing.T) {
	h := newEmbeddingHandler()

	t.Run("converts request with include unavailable", func(t *testing.T) {
		req := &embedding.ListProvidersRequest{
			IncludeUnavailable: true,
		}

		args, err := h.transformRequest("ListProviders", req)
		require.NoError(t, err)
		assert.Equal(t, "true", args["include_unavailable"])
	})

	t.Run("converts request without include unavailable", func(t *testing.T) {
		req := &embedding.ListProvidersRequest{}

		args, err := h.transformRequest("ListProviders", req)
		require.NoError(t, err)
		assert.NotContains(t, args, "include_unavailable")
	})
}

func TestEmbeddingHandler_TransformGetProviderInfoRequest(t *testing.T) {
	h := newEmbeddingHandler()

	t.Run("converts request with provider", func(t *testing.T) {
		req := &embedding.GetProviderInfoRequest{
			Provider: embedding.ProviderOllama,
		}

		args, err := h.transformRequest("GetProviderInfo", req)
		require.NoError(t, err)
		assert.Equal(t, "ollama", args["provider"])
	})

	t.Run("falls back to generic transform for non-SDK type", func(t *testing.T) {
		req := map[string]interface{}{
			"Provider": "voyage",
		}

		args, err := h.transformRequest("GetProviderInfo", req)
		require.NoError(t, err)
		assert.Equal(t, "voyage", args["provider"])
	})
}

func TestEmbeddingHandler_TransformSetDefaultProviderRequest(t *testing.T) {
	h := newEmbeddingHandler()

	t.Run("converts request with provider", func(t *testing.T) {
		req := &embedding.SetDefaultProviderRequest{
			Provider: embedding.ProviderOpenAI,
		}

		args, err := h.transformRequest("SetDefaultProvider", req)
		require.NoError(t, err)
		assert.Equal(t, "openai", args["provider"])
	})

	t.Run("falls back to generic transform for non-SDK type", func(t *testing.T) {
		req := map[string]interface{}{
			"Provider": "lmstudio",
		}

		args, err := h.transformRequest("SetDefaultProvider", req)
		require.NoError(t, err)
		assert.Equal(t, "lmstudio", args["provider"])
	})
}

func TestEmbeddingHandler_TransformGetCacheStatsRequest(t *testing.T) {
	h := newEmbeddingHandler()

	t.Run("returns empty map for cache stats", func(t *testing.T) {
		req := &embedding.GetEmbeddingCacheStatsRequest{}

		args, err := h.transformRequest("GetCacheStats", req)
		require.NoError(t, err)
		assert.Empty(t, args)
	})

	t.Run("returns empty map even for nil request", func(t *testing.T) {
		args, err := h.transformRequest("GetCacheStats", nil)
		require.NoError(t, err)
		assert.Empty(t, args)
	})
}

func TestEmbeddingHandler_TransformClearCacheRequest(t *testing.T) {
	h := newEmbeddingHandler()

	t.Run("converts request with provider and all flag", func(t *testing.T) {
		req := &embedding.ClearEmbeddingCacheRequest{
			Provider: embedding.ProviderVoyage,
			All:      true,
		}

		args, err := h.transformRequest("ClearCache", req)
		require.NoError(t, err)
		assert.Equal(t, "voyage", args["provider"])
		assert.Equal(t, "true", args["all"])
	})

	t.Run("converts request with only provider", func(t *testing.T) {
		req := &embedding.ClearEmbeddingCacheRequest{
			Provider: embedding.ProviderOpenAI,
		}

		args, err := h.transformRequest("ClearCache", req)
		require.NoError(t, err)
		assert.Equal(t, "openai", args["provider"])
		assert.NotContains(t, args, "all")
	})

	t.Run("converts empty request", func(t *testing.T) {
		req := &embedding.ClearEmbeddingCacheRequest{}

		args, err := h.transformRequest("ClearCache", req)
		require.NoError(t, err)
		assert.NotContains(t, args, "provider")
		assert.NotContains(t, args, "all")
	})

	t.Run("falls back to generic transform for non-SDK type", func(t *testing.T) {
		req := map[string]interface{}{
			"All": true,
		}

		args, err := h.transformRequest("ClearCache", req)
		require.NoError(t, err)
		assert.Contains(t, args, "all")
	})
}

func TestEmbeddingHandler_TransformUnknownMethodRequest(t *testing.T) {
	h := newEmbeddingHandler()

	t.Run("unknown method uses generic transform", func(t *testing.T) {
		req := map[string]interface{}{
			"SomeField": "value",
		}

		args, err := h.transformRequest("FutureMethod", req)
		require.NoError(t, err)
		assert.Equal(t, "value", args["some_field"])
	})

	t.Run("unknown method with nil returns empty map", func(t *testing.T) {
		args, err := h.transformRequest("FutureMethod", nil)
		require.NoError(t, err)
		assert.Empty(t, args)
	})
}

// --- transformResponse tests ---

func TestEmbeddingHandler_TransformGenerateEmbeddingResponse(t *testing.T) {
	h := newEmbeddingHandler()

	t.Run("converts valid response with embedding and metadata", func(t *testing.T) {
		data := map[string]interface{}{
			"embedding": []interface{}{float64(0.1), float64(0.2), float64(0.3)},
			"metadata": map[string]interface{}{
				"model":           "voyage-3",
				"provider":        "voyage",
				"dimensions":      float64(1024),
				"tokens":          float64(42),
				"source":          "test.go",
				"language":        "go",
				"input_type":      "code",
				"cache_hit":       false,
				"processing_time": float64(5000000),
				"tags": map[string]interface{}{
					"env": "test",
				},
			},
			"timestamp": "2025-01-15T10:30:00Z",
		}

		result, err := h.transformResponse("GenerateEmbedding", data)
		require.NoError(t, err)

		resp, ok := result.(*embedding.GenerateEmbeddingResponse)
		require.True(t, ok, "expected *embedding.GenerateEmbeddingResponse")
		assert.Equal(t, []float32{0.1, 0.2, 0.3}, resp.Embedding)
		require.NotNil(t, resp.Metadata)
		assert.Equal(t, "voyage-3", resp.Metadata.Model)
		assert.Equal(t, embedding.ProviderVoyage, resp.Metadata.Provider)
		assert.Equal(t, 1024, resp.Metadata.Dimensions)
		assert.Equal(t, 42, resp.Metadata.Tokens)
		assert.Equal(t, "test.go", resp.Metadata.Source)
		assert.Equal(t, "go", resp.Metadata.Language)
		assert.Equal(t, embedding.InputTypeCode, resp.Metadata.InputType)
		assert.False(t, resp.Metadata.CacheHit)
		assert.Equal(t, time.Duration(5000000), resp.Metadata.ProcessingTime)
		assert.Equal(t, map[string]string{"env": "test"}, resp.Metadata.Tags)
		assert.False(t, resp.Timestamp.IsZero())
	})

	t.Run("returns error for nil response", func(t *testing.T) {
		_, err := h.transformResponse("GenerateEmbedding", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nil response from server")
	})

	t.Run("handles response without metadata", func(t *testing.T) {
		data := map[string]interface{}{
			"embedding": []interface{}{float64(0.5), float64(0.6)},
		}

		result, err := h.transformResponse("GenerateEmbedding", data)
		require.NoError(t, err)

		resp, ok := result.(*embedding.GenerateEmbeddingResponse)
		require.True(t, ok)
		assert.Equal(t, []float32{0.5, 0.6}, resp.Embedding)
		assert.Nil(t, resp.Metadata)
	})
}

func TestEmbeddingHandler_TransformBatchGenerateEmbeddingsResponse(t *testing.T) {
	h := newEmbeddingHandler()

	t.Run("converts valid batch response", func(t *testing.T) {
		data := map[string]interface{}{
			"results": []interface{}{
				map[string]interface{}{
					"id":        "item-1",
					"embedding": []interface{}{float64(0.1), float64(0.2)},
					"metadata": map[string]interface{}{
						"model":    "voyage-3",
						"provider": "voyage",
					},
				},
				map[string]interface{}{
					"id":        "item-2",
					"embedding": []interface{}{float64(0.3), float64(0.4)},
					"error":     "",
				},
			},
			"success_count":   float64(2),
			"error_count":     float64(0),
			"total_tokens":    float64(100),
			"processing_time": float64(10000000),
			"timestamp":       "2025-01-15T10:30:00Z",
		}

		result, err := h.transformResponse("BatchGenerateEmbeddings", data)
		require.NoError(t, err)

		resp, ok := result.(*embedding.BatchGenerateEmbeddingsResponse)
		require.True(t, ok, "expected *embedding.BatchGenerateEmbeddingsResponse")
		assert.Equal(t, 2, resp.SuccessCount)
		assert.Equal(t, 0, resp.ErrorCount)
		assert.Equal(t, 100, resp.TotalTokens)
		assert.Equal(t, time.Duration(10000000), resp.ProcessingTime)
		require.Len(t, resp.Results, 2)
		assert.Equal(t, "item-1", resp.Results[0].ID)
		assert.Equal(t, []float32{0.1, 0.2}, resp.Results[0].Embedding)
		assert.Equal(t, "item-2", resp.Results[1].ID)
		assert.Equal(t, []float32{0.3, 0.4}, resp.Results[1].Embedding)
	})

	t.Run("returns error for nil response", func(t *testing.T) {
		_, err := h.transformResponse("BatchGenerateEmbeddings", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nil response from server")
	})

	t.Run("handles batch with errors", func(t *testing.T) {
		data := map[string]interface{}{
			"results": []interface{}{
				map[string]interface{}{
					"id":    "fail-1",
					"error": "provider timeout",
				},
			},
			"success_count": float64(0),
			"error_count":   float64(1),
		}

		result, err := h.transformResponse("BatchGenerateEmbeddings", data)
		require.NoError(t, err)

		resp, ok := result.(*embedding.BatchGenerateEmbeddingsResponse)
		require.True(t, ok)
		assert.Equal(t, 1, resp.ErrorCount)
		require.Len(t, resp.Results, 1)
		assert.Equal(t, "fail-1", resp.Results[0].ID)
		assert.Equal(t, "provider timeout", resp.Results[0].Error)
		assert.Nil(t, resp.Results[0].Embedding)
	})
}

func TestEmbeddingHandler_TransformListProvidersResponse(t *testing.T) {
	h := newEmbeddingHandler()

	t.Run("converts valid providers list", func(t *testing.T) {
		data := map[string]interface{}{
			"providers": []interface{}{
				map[string]interface{}{
					"name":                  "voyage",
					"type":                  "voyage",
					"model":                 "voyage-3",
					"dimensions":            float64(1024),
					"max_tokens":            float64(32000),
					"max_batch_size":        float64(128),
					"status":                "available",
					"api_url":               "https://api.voyageai.com",
					"rate_limit_per_minute": float64(100),
					"timeout":               float64(30000000000),
					"is_default":            true,
					"success_count":         float64(500),
					"error_count":           float64(2),
					"avg_latency":           float64(50000000),
				},
			},
			"default_provider": "voyage",
			"timestamp":        "2025-01-15T10:30:00Z",
		}

		result, err := h.transformResponse("ListProviders", data)
		require.NoError(t, err)

		resp, ok := result.(*embedding.ListProvidersResponse)
		require.True(t, ok, "expected *embedding.ListProvidersResponse")
		assert.Equal(t, "voyage", resp.DefaultProvider)
		require.Len(t, resp.Providers, 1)

		p := resp.Providers[0]
		assert.Equal(t, "voyage", p.Name)
		assert.Equal(t, embedding.ProviderVoyage, p.Type)
		assert.Equal(t, "voyage-3", p.Model)
		assert.Equal(t, 1024, p.Dimensions)
		assert.Equal(t, 32000, p.MaxTokens)
		assert.Equal(t, 128, p.MaxBatchSize)
		assert.Equal(t, embedding.StatusAvailable, p.Status)
		assert.Equal(t, "https://api.voyageai.com", p.APIURL)
		assert.Equal(t, 100, p.RateLimitPerMinute)
		assert.True(t, p.IsDefault)
		assert.Equal(t, uint64(500), p.SuccessCount)
		assert.Equal(t, uint64(2), p.ErrorCount)
	})

	t.Run("returns empty list for nil response", func(t *testing.T) {
		result, err := h.transformResponse("ListProviders", nil)
		require.NoError(t, err)

		resp, ok := result.(*embedding.ListProvidersResponse)
		require.True(t, ok)
		assert.Empty(t, resp.Providers)
	})

	t.Run("handles response with no providers array", func(t *testing.T) {
		data := map[string]interface{}{
			"default_provider": "openai",
		}

		result, err := h.transformResponse("ListProviders", data)
		require.NoError(t, err)

		resp, ok := result.(*embedding.ListProvidersResponse)
		require.True(t, ok)
		assert.Equal(t, "openai", resp.DefaultProvider)
		assert.Empty(t, resp.Providers)
	})
}

func TestEmbeddingHandler_TransformGetProviderInfoResponse(t *testing.T) {
	h := newEmbeddingHandler()

	t.Run("converts response with nested info", func(t *testing.T) {
		data := map[string]interface{}{
			"info": map[string]interface{}{
				"name":            "openai",
				"type":            "openai",
				"model":           "text-embedding-3-small",
				"dimensions":      float64(1536),
				"status":          "available",
				"is_default":      false,
				"last_error":      "rate limit exceeded",
				"last_error_time": "2025-01-15T09:00:00Z",
			},
			"timestamp": "2025-01-15T10:30:00Z",
		}

		result, err := h.transformResponse("GetProviderInfo", data)
		require.NoError(t, err)

		resp, ok := result.(*embedding.GetProviderInfoResponse)
		require.True(t, ok, "expected *embedding.GetProviderInfoResponse")
		require.NotNil(t, resp.Info)
		assert.Equal(t, "openai", resp.Info.Name)
		assert.Equal(t, embedding.ProviderOpenAI, resp.Info.Type)
		assert.Equal(t, "text-embedding-3-small", resp.Info.Model)
		assert.Equal(t, 1536, resp.Info.Dimensions)
		assert.Equal(t, embedding.StatusAvailable, resp.Info.Status)
		assert.False(t, resp.Info.IsDefault)
		assert.Equal(t, "rate limit exceeded", resp.Info.LastError)
		require.NotNil(t, resp.Info.LastErrorTime)
	})

	t.Run("converts response with flat fields", func(t *testing.T) {
		data := map[string]interface{}{
			"name":       "ollama",
			"type":       "ollama",
			"model":      "nomic-embed-text",
			"dimensions": float64(768),
			"status":     "available",
			"timestamp":  "2025-01-15T10:30:00Z",
		}

		result, err := h.transformResponse("GetProviderInfo", data)
		require.NoError(t, err)

		resp, ok := result.(*embedding.GetProviderInfoResponse)
		require.True(t, ok)
		require.NotNil(t, resp.Info)
		assert.Equal(t, "ollama", resp.Info.Name)
		assert.Equal(t, embedding.ProviderOllama, resp.Info.Type)
		assert.Equal(t, 768, resp.Info.Dimensions)
	})

	t.Run("returns error for nil response", func(t *testing.T) {
		_, err := h.transformResponse("GetProviderInfo", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nil response from server")
	})
}

func TestEmbeddingHandler_TransformSetDefaultProviderResponse(t *testing.T) {
	h := newEmbeddingHandler()

	t.Run("converts valid response", func(t *testing.T) {
		data := map[string]interface{}{
			"provider":          "openai",
			"previous_provider": "voyage",
			"timestamp":         "2025-01-15T10:30:00Z",
		}

		result, err := h.transformResponse("SetDefaultProvider", data)
		require.NoError(t, err)

		resp, ok := result.(*embedding.SetDefaultProviderResponse)
		require.True(t, ok, "expected *embedding.SetDefaultProviderResponse")
		assert.Equal(t, embedding.ProviderOpenAI, resp.Provider)
		assert.Equal(t, embedding.ProviderVoyage, resp.PreviousProvider)
		assert.False(t, resp.Timestamp.IsZero())
	})

	t.Run("returns error for nil response", func(t *testing.T) {
		_, err := h.transformResponse("SetDefaultProvider", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nil response from server")
	})
}

func TestEmbeddingHandler_TransformGetCacheStatsResponse(t *testing.T) {
	h := newEmbeddingHandler()

	t.Run("converts valid cache stats", func(t *testing.T) {
		data := map[string]interface{}{
			"enabled":             true,
			"size":                float64(150),
			"capacity":            float64(10000),
			"hits":                float64(850),
			"misses":              float64(150),
			"hit_rate":            float64(0.85),
			"evictions":           float64(10),
			"memory_bytes":        float64(1048576),
			"compression_enabled": true,
			"compression_ratio":   float64(0.7),
			"timestamp":           "2025-01-15T10:30:00Z",
		}

		result, err := h.transformResponse("GetCacheStats", data)
		require.NoError(t, err)

		resp, ok := result.(*embedding.GetEmbeddingCacheStatsResponse)
		require.True(t, ok, "expected *embedding.GetEmbeddingCacheStatsResponse")
		assert.True(t, resp.Enabled)
		assert.Equal(t, 150, resp.Size)
		assert.Equal(t, 10000, resp.Capacity)
		assert.Equal(t, uint64(850), resp.Hits)
		assert.Equal(t, uint64(150), resp.Misses)
		assert.Equal(t, 0.85, resp.HitRate)
		assert.Equal(t, uint64(10), resp.Evictions)
		assert.Equal(t, uint64(1048576), resp.MemoryBytes)
		assert.True(t, resp.CompressionEnabled)
		assert.Equal(t, 0.7, resp.CompressionRatio)
		assert.False(t, resp.Timestamp.IsZero())
	})

	t.Run("returns error for nil response", func(t *testing.T) {
		_, err := h.transformResponse("GetCacheStats", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nil response from server")
	})
}

func TestEmbeddingHandler_TransformClearCacheResponse(t *testing.T) {
	h := newEmbeddingHandler()

	t.Run("converts valid clear cache response", func(t *testing.T) {
		data := map[string]interface{}{
			"entries_cleared": float64(42),
			"provider":        "voyage",
			"timestamp":       "2025-01-15T10:30:00Z",
		}

		result, err := h.transformResponse("ClearCache", data)
		require.NoError(t, err)

		resp, ok := result.(*embedding.ClearEmbeddingCacheResponse)
		require.True(t, ok, "expected *embedding.ClearEmbeddingCacheResponse")
		assert.Equal(t, 42, resp.EntriesCleared)
		assert.Equal(t, embedding.ProviderVoyage, resp.Provider)
		assert.False(t, resp.Timestamp.IsZero())
	})

	t.Run("returns error for nil response", func(t *testing.T) {
		_, err := h.transformResponse("ClearCache", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nil response from server")
	})

	t.Run("handles response with empty provider", func(t *testing.T) {
		data := map[string]interface{}{
			"entries_cleared": float64(100),
			"timestamp":       "2025-01-15T10:30:00Z",
		}

		result, err := h.transformResponse("ClearCache", data)
		require.NoError(t, err)

		resp, ok := result.(*embedding.ClearEmbeddingCacheResponse)
		require.True(t, ok)
		assert.Equal(t, 100, resp.EntriesCleared)
		assert.Equal(t, embedding.Provider(""), resp.Provider)
	})
}

func TestEmbeddingHandler_TransformUnknownMethod(t *testing.T) {
	h := newEmbeddingHandler()

	t.Run("unknown method returns raw result", func(t *testing.T) {
		data := map[string]interface{}{
			"status": "ok",
		}

		result, err := h.transformResponse("UnknownMethod", data)
		require.NoError(t, err)
		assert.Equal(t, data, result)
	})

	t.Run("unknown method with nil returns nil", func(t *testing.T) {
		result, err := h.transformResponse("UnknownMethod", nil)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}

func TestResultFloat32Slice(t *testing.T) {
	t.Run("extracts from interface slice", func(t *testing.T) {
		m := map[string]interface{}{
			"vec": []interface{}{float64(1.0), float64(2.0), float64(3.0)},
		}
		result := resultFloat32Slice(m, "vec")
		assert.Equal(t, []float32{1.0, 2.0, 3.0}, result)
	})

	t.Run("returns nil for missing key", func(t *testing.T) {
		m := map[string]interface{}{}
		result := resultFloat32Slice(m, "vec")
		assert.Nil(t, result)
	})

	t.Run("returns nil for non-slice value", func(t *testing.T) {
		m := map[string]interface{}{
			"vec": "not a slice",
		}
		result := resultFloat32Slice(m, "vec")
		assert.Nil(t, result)
	})

	t.Run("handles float64 slice", func(t *testing.T) {
		m := map[string]interface{}{
			"vec": []float64{1.5, 2.5},
		}
		result := resultFloat32Slice(m, "vec")
		assert.Equal(t, []float32{1.5, 2.5}, result)
	})

	t.Run("handles float32 slice", func(t *testing.T) {
		m := map[string]interface{}{
			"vec": []float32{0.1, 0.2},
		}
		result := resultFloat32Slice(m, "vec")
		assert.Equal(t, []float32{0.1, 0.2}, result)
	})
}

func TestEmbeddingHandler_ParseMetadata(t *testing.T) {
	h := newEmbeddingHandler()

	t.Run("parses complete metadata", func(t *testing.T) {
		raw := map[string]interface{}{
			"model":           "voyage-3",
			"provider":        "voyage",
			"dimensions":      float64(1024),
			"tokens":          float64(100),
			"source":          "doc.txt",
			"language":        "en",
			"input_type":      "document",
			"cache_hit":       true,
			"processing_time": float64(1000000),
			"tags": map[string]interface{}{
				"category": "science",
			},
		}

		meta := h.parseMetadata(raw)
		require.NotNil(t, meta)
		assert.Equal(t, "voyage-3", meta.Model)
		assert.Equal(t, embedding.ProviderVoyage, meta.Provider)
		assert.Equal(t, 1024, meta.Dimensions)
		assert.Equal(t, 100, meta.Tokens)
		assert.Equal(t, "doc.txt", meta.Source)
		assert.Equal(t, "en", meta.Language)
		assert.Equal(t, embedding.InputTypeDocument, meta.InputType)
		assert.True(t, meta.CacheHit)
		assert.Equal(t, time.Duration(1000000), meta.ProcessingTime)
		assert.Equal(t, "science", meta.Tags["category"])
	})

	t.Run("returns nil for non-map input", func(t *testing.T) {
		meta := h.parseMetadata("not a map")
		assert.Nil(t, meta)
	})
}

func TestEmbeddingHandler_ParseProviderInfo(t *testing.T) {
	h := newEmbeddingHandler()

	t.Run("parses provider with last error time", func(t *testing.T) {
		raw := map[string]interface{}{
			"name":            "openai",
			"type":            "openai",
			"model":           "text-embedding-3-small",
			"dimensions":      float64(1536),
			"status":          "error",
			"last_error":      "api key invalid",
			"last_error_time": "2025-01-15T08:00:00Z",
		}

		info := h.parseProviderInfo(raw)
		require.NotNil(t, info)
		assert.Equal(t, "openai", info.Name)
		assert.Equal(t, embedding.StatusError, info.Status)
		assert.Equal(t, "api key invalid", info.LastError)
		require.NotNil(t, info.LastErrorTime)
	})

	t.Run("parses provider without last error time", func(t *testing.T) {
		raw := map[string]interface{}{
			"name":   "voyage",
			"type":   "voyage",
			"status": "available",
		}

		info := h.parseProviderInfo(raw)
		require.NotNil(t, info)
		assert.Equal(t, "voyage", info.Name)
		assert.Nil(t, info.LastErrorTime)
	})

	t.Run("returns nil for non-map input", func(t *testing.T) {
		info := h.parseProviderInfo("invalid")
		assert.Nil(t, info)
	})
}
