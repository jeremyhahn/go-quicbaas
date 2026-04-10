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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

func TestIsEmbeddingService(t *testing.T) {
	tests := []struct {
		name     string
		service  string
		expected bool
	}{
		{"fully qualified service", "quicbaas.Embedding", true},
		{"with method suffix", "quicbaas.Embedding.Generate", true},
		{"bare service name", "Embedding", true},
		{"non-embedding service KV", "quicbaas.KV", false},
		{"non-embedding service Vector", "quicbaas.Vector", false},
		{"empty service", "", false},
		{"partial match", "quicbaas.Embed", false},
		{"prefix only", "quicbaas.", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isEmbeddingService(tt.service)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewEmbeddingHandler(t *testing.T) {
	t.Run("creates handler with transport", func(t *testing.T) {
		tr := &Transport{}
		handler := newEmbeddingHandler(tr)
		assert.NotNil(t, handler)
		assert.Same(t, tr, handler.transport)
	})

	t.Run("creates handler with nil transport", func(t *testing.T) {
		handler := newEmbeddingHandler(nil)
		assert.NotNil(t, handler)
		assert.Nil(t, handler.transport)
	})
}

// newTestEmbeddingHandler creates an embeddingHandler backed by an httptest server.
// The caller must close the returned server when done.
func newTestEmbeddingHandler(t *testing.T, handler http.Handler) (*embeddingHandler, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)

	tr, err := New(
		&transport.Config{
			Address: strings.TrimPrefix(server.URL, "http://"),
			Timeout: 5 * time.Second,
		},
		WithHTTPClient(server.Client()),
		WithBaseURL(server.URL),
	)
	require.NoError(t, err)

	return newEmbeddingHandler(tr), server
}

func TestEmbeddingHandler_HandleGetProviderInfo_ValidProvider(t *testing.T) {
	providerResponse := map[string]interface{}{
		"name":       "voyage",
		"type":       "voyage",
		"model":      "voyage-3",
		"dimensions": float64(1024),
		"max_tokens": float64(32000),
		"status":     "available",
		"is_default": true,
		"timestamp":  time.Now().UTC().Format(time.RFC3339Nano),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/embedding/providers/voyage", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(providerResponse)
	})

	handler, server := newTestEmbeddingHandler(t, mux)
	defer server.Close()

	req := &embedding.GetProviderInfoRequest{
		Provider: embedding.ProviderVoyage,
	}

	resp, err := handler.handleRequest(context.Background(), "quicbaas.Embedding", "GetProviderInfo", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	providerInfo, ok := resp.(*embedding.GetProviderInfoResponse)
	require.True(t, ok)
	assert.Equal(t, "voyage", providerInfo.Info.Name)
	assert.Equal(t, embedding.ProviderVoyage, providerInfo.Info.Type)
	assert.Equal(t, "voyage-3", providerInfo.Info.Model)
	assert.Equal(t, 1024, providerInfo.Info.Dimensions)
	assert.Equal(t, 32000, providerInfo.Info.MaxTokens)
	assert.Equal(t, embedding.StatusAvailable, providerInfo.Info.Status)
	assert.True(t, providerInfo.Info.IsDefault)
}

func TestEmbeddingHandler_HandleGetProviderInfo_EmptyProvider(t *testing.T) {
	handler := newEmbeddingHandler(&Transport{})

	req := &embedding.GetProviderInfoRequest{
		Provider: "",
	}

	resp, err := handler.handleRequest(context.Background(), "quicbaas.Embedding", "GetProviderInfo", req)
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.True(t, errors.IsInvalidInput(err))
	assert.Contains(t, err.Error(), "provider is required")
}

func TestEmbeddingHandler_HandleGetProviderInfo_InvalidRequestType(t *testing.T) {
	handler := newEmbeddingHandler(&Transport{})

	resp, err := handler.handleRequest(context.Background(), "quicbaas.Embedding", "GetProviderInfo", "invalid")
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.True(t, errors.IsInvalidInput(err))
}

func TestEmbeddingHandler_HandleGetProviderInfo_ServerNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/embedding/providers/nonexistent", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "provider not found: nonexistent",
			"code":  "NOT_FOUND",
		})
	})

	handler, server := newTestEmbeddingHandler(t, mux)
	defer server.Close()

	req := &embedding.GetProviderInfoRequest{
		Provider: embedding.Provider("nonexistent"),
	}

	resp, err := handler.handleRequest(context.Background(), "quicbaas.Embedding", "GetProviderInfo", req)
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.True(t, errors.IsNotFound(err))
}

func TestEmbeddingHandler_HandleGetProviderInfo_ServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/embedding/providers/openai", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "internal server error",
			"code":  "INTERNAL",
		})
	})

	handler, server := newTestEmbeddingHandler(t, mux)
	defer server.Close()

	req := &embedding.GetProviderInfoRequest{
		Provider: embedding.ProviderOpenAI,
	}

	resp, err := handler.handleRequest(context.Background(), "quicbaas.Embedding", "GetProviderInfo", req)
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestEmbeddingHandler_HandleGetProviderInfo_GetModelAlias(t *testing.T) {
	// GetModel should be routed to handleGetProviderInfo
	providerResponse := map[string]interface{}{
		"name":       "openai",
		"type":       "openai",
		"model":      "text-embedding-3-small",
		"dimensions": float64(1536),
		"status":     "available",
		"timestamp":  time.Now().UTC().Format(time.RFC3339Nano),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/embedding/providers/openai", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(providerResponse)
	})

	handler, server := newTestEmbeddingHandler(t, mux)
	defer server.Close()

	req := &embedding.GetProviderInfoRequest{
		Provider: embedding.ProviderOpenAI,
	}

	resp, err := handler.handleRequest(context.Background(), "quicbaas.Embedding", "GetModel", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	providerInfo, ok := resp.(*embedding.GetProviderInfoResponse)
	require.True(t, ok)
	assert.Equal(t, "openai", providerInfo.Info.Name)
}

func TestEmbeddingHandler_DoRequest_Success(t *testing.T) {
	expectedResp := map[string]interface{}{
		"key": "value",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expectedResp)
	})

	handler, server := newTestEmbeddingHandler(t, mux)
	defer server.Close()

	resp, err := handler.doRequest(context.Background(), http.MethodGet, server.URL+"/api/v1/test", nil)
	require.NoError(t, err)
	require.NotNil(t, resp)

	m, ok := resp.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "value", m["key"])
}

func TestEmbeddingHandler_DoRequest_WithBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/test", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
	})

	handler, server := newTestEmbeddingHandler(t, mux)
	defer server.Close()

	body := map[string]string{"input": "test data"}
	resp, err := handler.doRequest(context.Background(), http.MethodPost, server.URL+"/api/v1/test", body)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestEmbeddingHandler_DoRequest_HTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "bad request",
			"code":  "INVALID_INPUT",
		})
	})

	handler, server := newTestEmbeddingHandler(t, mux)
	defer server.Close()

	resp, err := handler.doRequest(context.Background(), http.MethodGet, server.URL+"/api/v1/test", nil)
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.True(t, errors.IsInvalidInput(err))
}

func TestEmbeddingHandler_DoRequest_EmptyResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler, server := newTestEmbeddingHandler(t, mux)
	defer server.Close()

	resp, err := handler.doRequest(context.Background(), http.MethodGet, server.URL+"/api/v1/test", nil)
	require.NoError(t, err)
	assert.Nil(t, resp)
}

func TestEmbeddingHandler_DoRequest_PropagatesHeaders(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/test", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token-123", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	tr, err := New(
		&transport.Config{
			Address: strings.TrimPrefix(server.URL, "http://"),
			Timeout: 5 * time.Second,
		},
		WithHTTPClient(server.Client()),
		WithBaseURL(server.URL),
		WithAuthToken("test-token-123"),
	)
	require.NoError(t, err)

	handler := newEmbeddingHandler(tr)
	resp, err := handler.doRequest(context.Background(), http.MethodGet, server.URL+"/api/v1/test", nil)
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestEmbeddingHandler_DoRequest_UpdatesHealthTimestamp(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	})

	handler, server := newTestEmbeddingHandler(t, mux)
	defer server.Close()

	before := handler.transport.lastHealth.Load()
	_, err := handler.doRequest(context.Background(), http.MethodGet, server.URL+"/api/v1/test", nil)
	require.NoError(t, err)

	after := handler.transport.lastHealth.Load()
	assert.Greater(t, after, before)
}

func TestEmbeddingHandler_ConvertResponse_NilInput(t *testing.T) {
	handler := newEmbeddingHandler(&Transport{})
	resp, err := handler.convertResponse("GetProviderInfo", nil)
	assert.NoError(t, err)
	assert.Nil(t, resp)
}

func TestEmbeddingHandler_ConvertResponse_InvalidType(t *testing.T) {
	handler := newEmbeddingHandler(&Transport{})
	resp, err := handler.convertResponse("GetProviderInfo", "not a map")
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestEmbeddingHandler_ConvertResponse_UnknownMethod(t *testing.T) {
	handler := newEmbeddingHandler(&Transport{})
	data := map[string]interface{}{"key": "value"}
	resp, err := handler.convertResponse("UnknownMethod", data)
	assert.NoError(t, err)
	// Unknown methods pass through the raw data
	assert.Equal(t, data, resp)
}

func TestConvertGetProviderInfoResponse(t *testing.T) {
	t.Run("full response with all fields", func(t *testing.T) {
		now := time.Now().UTC()
		errTime := now.Add(-1 * time.Hour)

		data := map[string]interface{}{
			"name":                  "voyage",
			"type":                  "voyage",
			"model":                 "voyage-3",
			"dimensions":            float64(1024),
			"max_tokens":            float64(32000),
			"max_batch_size":        float64(128),
			"status":                "available",
			"api_url":               "https://api.voyageai.com/v1/embeddings",
			"rate_limit_per_minute": float64(300),
			"timeout":               "30s",
			"is_default":            true,
			"last_error":            "rate limited",
			"last_error_time":       errTime.Format(time.RFC3339Nano),
			"success_count":         float64(1000),
			"error_count":           float64(5),
			"avg_latency":           "150ms",
			"timestamp":             now.Format(time.RFC3339Nano),
		}

		resp, err := convertGetProviderInfoResponse(data)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "voyage", resp.Info.Name)
		assert.Equal(t, embedding.Provider("voyage"), resp.Info.Type)
		assert.Equal(t, "voyage-3", resp.Info.Model)
		assert.Equal(t, 1024, resp.Info.Dimensions)
		assert.Equal(t, 32000, resp.Info.MaxTokens)
		assert.Equal(t, 128, resp.Info.MaxBatchSize)
		assert.Equal(t, embedding.Status("available"), resp.Info.Status)
		assert.Equal(t, "https://api.voyageai.com/v1/embeddings", resp.Info.APIURL)
		assert.Equal(t, 300, resp.Info.RateLimitPerMinute)
		assert.Equal(t, 30*time.Second, resp.Info.Timeout)
		assert.True(t, resp.Info.IsDefault)
		assert.Equal(t, "rate limited", resp.Info.LastError)
		assert.NotNil(t, resp.Info.LastErrorTime)
		assert.Equal(t, uint64(1000), resp.Info.SuccessCount)
		assert.Equal(t, uint64(5), resp.Info.ErrorCount)
		assert.Equal(t, 150*time.Millisecond, resp.Info.AvgLatency)
	})

	t.Run("minimal response with empty data", func(t *testing.T) {
		data := map[string]interface{}{}
		resp, err := convertGetProviderInfoResponse(data)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotNil(t, resp.Info)
		assert.Equal(t, "", resp.Info.Name)
		assert.Equal(t, embedding.Provider(""), resp.Info.Type)
	})
}

func TestConvertGenerateEmbeddingResponse(t *testing.T) {
	t.Run("full response", func(t *testing.T) {
		now := time.Now().UTC()
		data := map[string]interface{}{
			"embedding":       []interface{}{float64(0.1), float64(0.2), float64(0.3)},
			"model":           "voyage-3",
			"provider":        "voyage",
			"dimensions":      float64(3),
			"tokens":          float64(10),
			"cache_hit":       true,
			"processing_time": "5ms",
			"timestamp":       now.Format(time.RFC3339Nano),
		}

		resp, err := convertGenerateEmbeddingResponse(data)
		require.NoError(t, err)
		assert.Equal(t, []float32{0.1, 0.2, 0.3}, resp.Embedding)
		assert.Equal(t, "voyage-3", resp.Metadata.Model)
		assert.Equal(t, embedding.Provider("voyage"), resp.Metadata.Provider)
		assert.Equal(t, 3, resp.Metadata.Dimensions)
		assert.Equal(t, 10, resp.Metadata.Tokens)
		assert.True(t, resp.Metadata.CacheHit)
		assert.Equal(t, 5*time.Millisecond, resp.Metadata.ProcessingTime)
	})

	t.Run("empty response", func(t *testing.T) {
		data := map[string]interface{}{}
		resp, err := convertGenerateEmbeddingResponse(data)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Nil(t, resp.Embedding)
	})
}

func TestConvertListProvidersResponse(t *testing.T) {
	t.Run("response with providers", func(t *testing.T) {
		now := time.Now().UTC()
		data := map[string]interface{}{
			"default_provider": "voyage",
			"timestamp":        now.Format(time.RFC3339Nano),
			"providers": []interface{}{
				map[string]interface{}{
					"name":   "voyage",
					"type":   "voyage",
					"status": "available",
				},
				map[string]interface{}{
					"name":   "openai",
					"type":   "openai",
					"status": "available",
				},
			},
		}

		resp, err := convertListProvidersResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "voyage", resp.DefaultProvider)
		assert.Len(t, resp.Providers, 2)
		assert.Equal(t, "voyage", resp.Providers[0].Name)
		assert.Equal(t, "openai", resp.Providers[1].Name)
	})

	t.Run("empty providers list", func(t *testing.T) {
		data := map[string]interface{}{
			"default_provider": "",
			"providers":        []interface{}{},
		}

		resp, err := convertListProvidersResponse(data)
		require.NoError(t, err)
		assert.Empty(t, resp.Providers)
	})
}

func TestConvertSetDefaultProviderResponse(t *testing.T) {
	t.Run("successful provider change", func(t *testing.T) {
		now := time.Now().UTC()
		data := map[string]interface{}{
			"provider":          "openai",
			"previous_provider": "voyage",
			"timestamp":         now.Format(time.RFC3339Nano),
		}

		resp, err := convertSetDefaultProviderResponse(data)
		require.NoError(t, err)
		assert.Equal(t, embedding.Provider("openai"), resp.Provider)
		assert.Equal(t, embedding.Provider("voyage"), resp.PreviousProvider)
	})

	t.Run("empty data", func(t *testing.T) {
		data := map[string]interface{}{}
		resp, err := convertSetDefaultProviderResponse(data)
		require.NoError(t, err)
		assert.Equal(t, embedding.Provider(""), resp.Provider)
	})
}

func TestConvertGetCacheStatsResponse(t *testing.T) {
	t.Run("full response", func(t *testing.T) {
		now := time.Now().UTC()
		data := map[string]interface{}{
			"enabled":             true,
			"size":                float64(500),
			"capacity":            float64(10000),
			"hits":                float64(850),
			"misses":              float64(150),
			"hit_rate":            float64(0.85),
			"evictions":           float64(10),
			"memory_bytes":        float64(1048576),
			"compression_enabled": true,
			"compression_ratio":   float64(0.65),
			"timestamp":           now.Format(time.RFC3339Nano),
		}

		resp, err := convertGetCacheStatsResponse(data)
		require.NoError(t, err)
		assert.True(t, resp.Enabled)
		assert.Equal(t, 500, resp.Size)
		assert.Equal(t, 10000, resp.Capacity)
		assert.Equal(t, uint64(850), resp.Hits)
		assert.Equal(t, uint64(150), resp.Misses)
		assert.Equal(t, 0.85, resp.HitRate)
		assert.Equal(t, uint64(10), resp.Evictions)
		assert.Equal(t, uint64(1048576), resp.MemoryBytes)
		assert.True(t, resp.CompressionEnabled)
		assert.Equal(t, 0.65, resp.CompressionRatio)
	})

	t.Run("empty data", func(t *testing.T) {
		data := map[string]interface{}{}
		resp, err := convertGetCacheStatsResponse(data)
		require.NoError(t, err)
		assert.False(t, resp.Enabled)
		assert.Equal(t, 0, resp.Size)
	})
}

func TestConvertClearCacheResponse(t *testing.T) {
	t.Run("full response", func(t *testing.T) {
		now := time.Now().UTC()
		data := map[string]interface{}{
			"entries_cleared": float64(42),
			"provider":        "voyage",
			"timestamp":       now.Format(time.RFC3339Nano),
		}

		resp, err := convertClearCacheResponse(data)
		require.NoError(t, err)
		assert.Equal(t, 42, resp.EntriesCleared)
		assert.Equal(t, embedding.Provider("voyage"), resp.Provider)
	})

	t.Run("empty data", func(t *testing.T) {
		data := map[string]interface{}{}
		resp, err := convertClearCacheResponse(data)
		require.NoError(t, err)
		assert.Equal(t, 0, resp.EntriesCleared)
	})
}

func TestConvertBatchGenerateEmbeddingsResponse(t *testing.T) {
	t.Run("full response with results", func(t *testing.T) {
		now := time.Now().UTC()
		data := map[string]interface{}{
			"success_count":   float64(2),
			"error_count":     float64(1),
			"total_tokens":    float64(50),
			"processing_time": "10ms",
			"timestamp":       now.Format(time.RFC3339Nano),
			"results": []interface{}{
				map[string]interface{}{
					"id":        "item-1",
					"embedding": []interface{}{float64(0.1), float64(0.2)},
					"model":     "voyage-3",
				},
				map[string]interface{}{
					"id":    "item-2",
					"error": "text too long",
				},
			},
		}

		resp, err := convertBatchGenerateEmbeddingsResponse(data)
		require.NoError(t, err)
		assert.Equal(t, 2, resp.SuccessCount)
		assert.Equal(t, 1, resp.ErrorCount)
		assert.Equal(t, 50, resp.TotalTokens)
		assert.Equal(t, 10*time.Millisecond, resp.ProcessingTime)
		assert.Len(t, resp.Results, 2)
		assert.Equal(t, "item-1", resp.Results[0].ID)
		assert.Equal(t, []float32{0.1, 0.2}, resp.Results[0].Embedding)
		assert.Equal(t, "item-2", resp.Results[1].ID)
		assert.Equal(t, "text too long", resp.Results[1].Error)
	})

	t.Run("empty results", func(t *testing.T) {
		data := map[string]interface{}{}
		resp, err := convertBatchGenerateEmbeddingsResponse(data)
		require.NoError(t, err)
		assert.Equal(t, 0, resp.SuccessCount)
		assert.Nil(t, resp.Results)
	})
}

func TestConvertProviderInfoFromMap(t *testing.T) {
	t.Run("complete provider info", func(t *testing.T) {
		data := map[string]interface{}{
			"name":                  "voyage",
			"type":                  "voyage",
			"model":                 "voyage-3",
			"dimensions":            float64(1024),
			"max_tokens":            float64(32000),
			"max_batch_size":        float64(128),
			"status":                "available",
			"api_url":               "https://api.voyageai.com",
			"rate_limit_per_minute": float64(300),
			"timeout":               "30s",
			"is_default":            true,
			"success_count":         float64(100),
			"error_count":           float64(2),
			"avg_latency":           "200ms",
		}

		info := convertProviderInfoFromMap(data)
		assert.Equal(t, "voyage", info.Name)
		assert.Equal(t, embedding.Provider("voyage"), info.Type)
		assert.Equal(t, "voyage-3", info.Model)
		assert.Equal(t, 1024, info.Dimensions)
		assert.Equal(t, 32000, info.MaxTokens)
		assert.Equal(t, 128, info.MaxBatchSize)
		assert.Equal(t, embedding.Status("available"), info.Status)
		assert.Equal(t, "https://api.voyageai.com", info.APIURL)
		assert.Equal(t, 300, info.RateLimitPerMinute)
		assert.Equal(t, 30*time.Second, info.Timeout)
		assert.True(t, info.IsDefault)
		assert.Equal(t, uint64(100), info.SuccessCount)
		assert.Equal(t, uint64(2), info.ErrorCount)
		assert.Equal(t, 200*time.Millisecond, info.AvgLatency)
	})

	t.Run("empty map returns zero value info", func(t *testing.T) {
		info := convertProviderInfoFromMap(map[string]interface{}{})
		assert.Equal(t, "", info.Name)
		assert.Equal(t, embedding.Provider(""), info.Type)
		assert.Equal(t, 0, info.Dimensions)
		assert.False(t, info.IsDefault)
		assert.Nil(t, info.LastErrorTime)
	})
}

func TestToFloat32Slice(t *testing.T) {
	t.Run("float64 array", func(t *testing.T) {
		input := []interface{}{float64(1.0), float64(2.5), float64(3.7)}
		result := toFloat32Slice(input)
		assert.Equal(t, []float32{1.0, 2.5, 3.7}, result)
	})

	t.Run("nil input", func(t *testing.T) {
		result := toFloat32Slice(nil)
		assert.Nil(t, result)
	})

	t.Run("non-array input", func(t *testing.T) {
		result := toFloat32Slice("not an array")
		assert.Nil(t, result)
	})

	t.Run("empty array", func(t *testing.T) {
		result := toFloat32Slice([]interface{}{})
		assert.Empty(t, result)
	})

	t.Run("mixed types skips invalid elements", func(t *testing.T) {
		input := []interface{}{float64(1.0), "invalid", float64(3.0)}
		result := toFloat32Slice(input)
		assert.Equal(t, []float32{1.0, 3.0}, result)
	})
}
