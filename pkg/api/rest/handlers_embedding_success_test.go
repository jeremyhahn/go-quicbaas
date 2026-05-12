//go:build rest

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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// MockEmbeddingService implements service.EmbeddingService for testing.
type MockEmbeddingService struct {
	mock.Mock
}

func (m *MockEmbeddingService) GenerateEmbedding(ctx context.Context, req *service.GenerateEmbeddingRequest) (*service.GenerateEmbeddingResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GenerateEmbeddingResponse), args.Error(1)
}

func (m *MockEmbeddingService) BatchGenerateEmbeddings(ctx context.Context, req *service.BatchGenerateEmbeddingsRequest) (*service.BatchGenerateEmbeddingsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.BatchGenerateEmbeddingsResponse), args.Error(1)
}

func (m *MockEmbeddingService) ListProviders(ctx context.Context, req *service.ListProvidersRequest) (*service.ListProvidersResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ListProvidersResponse), args.Error(1)
}

func (m *MockEmbeddingService) GetProviderInfo(ctx context.Context, req *service.GetProviderInfoRequest) (*service.GetProviderInfoResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetProviderInfoResponse), args.Error(1)
}

func (m *MockEmbeddingService) SetDefaultProvider(ctx context.Context, req *service.SetDefaultProviderRequest) (*service.SetDefaultProviderResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.SetDefaultProviderResponse), args.Error(1)
}

func (m *MockEmbeddingService) GetCacheStats(ctx context.Context, req *service.GetEmbeddingCacheStatsRequest) (*service.GetEmbeddingCacheStatsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetEmbeddingCacheStatsResponse), args.Error(1)
}

func (m *MockEmbeddingService) ClearCache(ctx context.Context, req *service.ClearEmbeddingCacheRequest) (*service.ClearEmbeddingCacheResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ClearEmbeddingCacheResponse), args.Error(1)
}

func (m *MockEmbeddingService) SetProviderStatus(ctx context.Context, req *service.SetProviderStatusRequest) (*service.SetProviderStatusResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.SetProviderStatusResponse), args.Error(1)
}

// setupMockServerWithEmbedding creates a test server with mock embedding service.
func setupMockServerWithEmbedding(t *testing.T) (*Server, *testutil.MockQuicBaaS, *MockEmbeddingService, func()) {
	t.Helper()

	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockService := testutil.NewMockQuicBaaS()
	mockEmbedding := &MockEmbeddingService{}
	mockService.EmbeddingService = mockEmbedding

	serverConfig := DefaultConfig()
	serverConfig.Address = ":0"

	server, err := NewServer(mockService, serverConfig, log)
	require.NoError(t, err)

	cleanup := func() {
		server.Close()
	}

	return server, mockService, mockEmbedding, cleanup
}

func TestHandleEmbeddingGenerateSuccess(t *testing.T) {
	t.Run("successful embedding generation", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("GenerateEmbedding", mock.Anything, mock.Anything).Return(&service.GenerateEmbeddingResponse{
			Embedding: []float32{0.1, 0.2, 0.3},
			Metadata: &service.EmbeddingMetadata{
				Model:          "text-embedding-ada-002",
				Provider:       service.EmbeddingProviderOpenAI,
				Dimensions:     3,
				Tokens:         5,
				CacheHit:       false,
				ProcessingTime: 100 * time.Millisecond,
			},
			Timestamp: time.Now(),
		}, nil).Once()

		body := `{"text":"hello world"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/embedding/generate", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingGenerate(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "embedding")
		mockEmbedding.AssertExpectations(t)
	})

	t.Run("embedding generation with all options", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("GenerateEmbedding", mock.Anything, mock.MatchedBy(func(req *service.GenerateEmbeddingRequest) bool {
			return req.Text == "hello world" && req.CacheBypass == true
		})).Return(&service.GenerateEmbeddingResponse{
			Embedding: []float32{0.1, 0.2, 0.3},
			Metadata: &service.EmbeddingMetadata{
				Model:          "text-embedding-ada-002",
				Provider:       service.EmbeddingProviderOpenAI,
				Dimensions:     3,
				Tokens:         5,
				CacheHit:       false,
				ProcessingTime: 100 * time.Millisecond,
			},
			Timestamp: time.Now(),
		}, nil).Once()

		body := `{"text":"hello world","provider":"openai","cache_bypass":true}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/embedding/generate", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingGenerate(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockEmbedding.AssertExpectations(t)
	})

	t.Run("embedding generation service error", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("GenerateEmbedding", mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()

		body := `{"text":"hello world"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/embedding/generate", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingGenerate(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		mockEmbedding.AssertExpectations(t)
	})
}

func TestHandleEmbeddingBatchGenerateSuccess(t *testing.T) {
	t.Run("successful batch embedding generation", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("BatchGenerateEmbeddings", mock.Anything, mock.Anything).Return(&service.BatchGenerateEmbeddingsResponse{
			Results: []*service.BatchEmbeddingResult{
				{
					ID:        "1",
					Embedding: []float32{0.1, 0.2, 0.3},
					Metadata: &service.EmbeddingMetadata{
						Model:          "text-embedding-ada-002",
						Provider:       service.EmbeddingProviderOpenAI,
						Dimensions:     3,
						Tokens:         3,
						CacheHit:       false,
						ProcessingTime: 50 * time.Millisecond,
					},
				},
				{
					ID:        "2",
					Embedding: []float32{0.4, 0.5, 0.6},
					Metadata: &service.EmbeddingMetadata{
						Model:          "text-embedding-ada-002",
						Provider:       service.EmbeddingProviderOpenAI,
						Dimensions:     3,
						Tokens:         3,
						CacheHit:       false,
						ProcessingTime: 50 * time.Millisecond,
					},
				},
			},
			SuccessCount:   2,
			ErrorCount:     0,
			TotalTokens:    6,
			ProcessingTime: 100 * time.Millisecond,
			Timestamp:      time.Now(),
		}, nil).Once()

		body := `{"items":[{"id":"1","text":"hello"},{"id":"2","text":"world"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/embedding/batch", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingBatchGenerate(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "results")
		assert.Contains(t, w.Body.String(), "success_count")
		mockEmbedding.AssertExpectations(t)
	})

	t.Run("batch embedding with nil metadata in result", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("BatchGenerateEmbeddings", mock.Anything, mock.Anything).Return(&service.BatchGenerateEmbeddingsResponse{
			Results: []*service.BatchEmbeddingResult{
				{
					ID:        "1",
					Embedding: []float32{0.1, 0.2, 0.3},
					Metadata:  nil, // nil metadata to test nil handling
				},
			},
			SuccessCount:   1,
			ErrorCount:     0,
			TotalTokens:    3,
			ProcessingTime: 50 * time.Millisecond,
			Timestamp:      time.Now(),
		}, nil).Once()

		body := `{"items":[{"id":"1","text":"hello"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/embedding/batch", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingBatchGenerate(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockEmbedding.AssertExpectations(t)
	})

	t.Run("batch embedding service error", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("BatchGenerateEmbeddings", mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()

		body := `{"items":[{"id":"1","text":"hello"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/embedding/batch", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingBatchGenerate(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		mockEmbedding.AssertExpectations(t)
	})

	t.Run("batch embedding item with empty text after service check", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		// When embedding service is configured, empty text in items should return 400
		body := `{"items":[{"id":"1","text":""}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/embedding/batch", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingBatchGenerate(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "text is required")
		mockEmbedding.AssertNotCalled(t, "BatchGenerateEmbeddings")
	})
}

func TestHandleEmbeddingListProvidersSuccess(t *testing.T) {
	t.Run("successful list providers", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("ListProviders", mock.Anything, mock.Anything).Return(&service.ListProvidersResponse{
			Providers: []*service.ProviderInfo{
				{
					Name:         "openai",
					Type:         service.EmbeddingProviderOpenAI,
					Model:        "text-embedding-ada-002",
					Dimensions:   1536,
					MaxTokens:    8191,
					MaxBatchSize: 2048,
					Status:       service.EmbeddingStatusAvailable,
					IsDefault:    true,
					Timeout:      30 * time.Second,
				},
			},
			DefaultProvider: "openai",
			Timestamp:       time.Now(),
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/embedding/providers", nil)
		w := httptest.NewRecorder()

		server.HandleEmbeddingListProviders(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "providers")
		assert.Contains(t, w.Body.String(), "default_provider")
		mockEmbedding.AssertExpectations(t)
	})

	t.Run("list providers with include_unavailable query param", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("ListProviders", mock.Anything, mock.MatchedBy(func(req *service.ListProvidersRequest) bool {
			return req.IncludeUnavailable == true
		})).Return(&service.ListProvidersResponse{
			Providers:       []*service.ProviderInfo{},
			DefaultProvider: "",
			Timestamp:       time.Now(),
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/embedding/providers?include_unavailable=true", nil)
		w := httptest.NewRecorder()

		server.HandleEmbeddingListProviders(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockEmbedding.AssertExpectations(t)
	})

	t.Run("list providers service error", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("ListProviders", mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/embedding/providers", nil)
		w := httptest.NewRecorder()

		server.HandleEmbeddingListProviders(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		mockEmbedding.AssertExpectations(t)
	})
}

func TestHandleEmbeddingGetProviderInfoSuccess(t *testing.T) {
	t.Run("successful get provider info", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("GetProviderInfo", mock.Anything, mock.MatchedBy(func(req *service.GetProviderInfoRequest) bool {
			return req.Provider == "openai"
		})).Return(&service.GetProviderInfoResponse{
			Info: &service.ProviderInfo{
				Name:         "openai",
				Type:         service.EmbeddingProviderOpenAI,
				Model:        "text-embedding-ada-002",
				Dimensions:   1536,
				MaxTokens:    8191,
				MaxBatchSize: 2048,
				Status:       service.EmbeddingStatusAvailable,
				IsDefault:    true,
				Timeout:      30 * time.Second,
				AvgLatency:   100 * time.Millisecond,
			},
			Timestamp: time.Now(),
		}, nil).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("provider", "openai")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/embedding/providers/openai", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleEmbeddingGetProviderInfo(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "openai")
		mockEmbedding.AssertExpectations(t)
	})

	t.Run("get provider info service error", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("GetProviderInfo", mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("provider", "unknown")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/embedding/providers/unknown", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleEmbeddingGetProviderInfo(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		mockEmbedding.AssertExpectations(t)
	})
}

func TestHandleEmbeddingSetDefaultProviderSuccess(t *testing.T) {
	t.Run("successful set default provider", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("SetDefaultProvider", mock.Anything, mock.MatchedBy(func(req *service.SetDefaultProviderRequest) bool {
			return req.Provider == "voyage"
		})).Return(&service.SetDefaultProviderResponse{
			Provider:         "voyage",
			PreviousProvider: "openai",
			Timestamp:        time.Now(),
		}, nil).Once()

		body := `{"provider":"voyage"}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/embedding/providers/default", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingSetDefaultProvider(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "voyage")
		assert.Contains(t, w.Body.String(), "previous_provider")
		mockEmbedding.AssertExpectations(t)
	})

	t.Run("set default provider service error", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("SetDefaultProvider", mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()

		body := `{"provider":"unknown"}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/embedding/providers/default", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingSetDefaultProvider(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		mockEmbedding.AssertExpectations(t)
	})
}

func TestHandleEmbeddingGetCacheStatsSuccess(t *testing.T) {
	t.Run("successful get cache stats", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("GetCacheStats", mock.Anything, mock.Anything).Return(&service.GetEmbeddingCacheStatsResponse{
			Enabled:            true,
			Size:               100,
			Capacity:           1000,
			Hits:               500,
			Misses:             100,
			HitRate:            0.833,
			Evictions:          10,
			MemoryBytes:        1024000,
			CompressionEnabled: true,
			CompressionRatio:   0.7,
			Timestamp:          time.Now(),
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/embedding/cache/stats", nil)
		w := httptest.NewRecorder()

		server.HandleEmbeddingGetCacheStats(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "enabled")
		assert.Contains(t, w.Body.String(), "hit_rate")
		mockEmbedding.AssertExpectations(t)
	})

	t.Run("get cache stats service error", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("GetCacheStats", mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/embedding/cache/stats", nil)
		w := httptest.NewRecorder()

		server.HandleEmbeddingGetCacheStats(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		mockEmbedding.AssertExpectations(t)
	})
}

func TestHandleEmbeddingClearCacheSuccess(t *testing.T) {
	t.Run("successful clear cache", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("ClearCache", mock.Anything, mock.Anything).Return(&service.ClearEmbeddingCacheResponse{
			EntriesCleared: 50,
			Provider:       "",
			Timestamp:      time.Now(),
		}, nil).Once()

		body := `{}`
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/embedding/cache", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingClearCache(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "entries_cleared")
		mockEmbedding.AssertExpectations(t)
	})

	t.Run("clear cache with provider filter", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("ClearCache", mock.Anything, mock.MatchedBy(func(req *service.ClearEmbeddingCacheRequest) bool {
			return req.Provider == "openai"
		})).Return(&service.ClearEmbeddingCacheResponse{
			EntriesCleared: 25,
			Provider:       "openai",
			Timestamp:      time.Now(),
		}, nil).Once()

		body := `{"provider":"openai"}`
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/embedding/cache", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingClearCache(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "openai")
		mockEmbedding.AssertExpectations(t)
	})

	t.Run("clear cache with all flag", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("ClearCache", mock.Anything, mock.MatchedBy(func(req *service.ClearEmbeddingCacheRequest) bool {
			return req.All == true
		})).Return(&service.ClearEmbeddingCacheResponse{
			EntriesCleared: 100,
			Provider:       "",
			Timestamp:      time.Now(),
		}, nil).Once()

		body := `{"all":true}`
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/embedding/cache", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingClearCache(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockEmbedding.AssertExpectations(t)
	})

	t.Run("clear cache service error", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("ClearCache", mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()

		body := `{}`
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/embedding/cache", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingClearCache(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		mockEmbedding.AssertExpectations(t)
	})

	t.Run("clear cache invalid JSON body returns 400 when service configured", func(t *testing.T) {
		server, _, _, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		// When embedding service is configured and body has content but is invalid JSON
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/embedding/cache", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = 8 // Set content length to trigger body parsing
		w := httptest.NewRecorder()

		server.HandleEmbeddingClearCache(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("clear cache with empty body succeeds", func(t *testing.T) {
		server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
		defer cleanup()

		mockEmbedding.On("ClearCache", mock.Anything, mock.Anything).Return(&service.ClearEmbeddingCacheResponse{
			EntriesCleared: 10,
			Timestamp:      time.Now(),
		}, nil).Once()

		// Empty body should succeed
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/embedding/cache", nil)
		w := httptest.NewRecorder()

		server.HandleEmbeddingClearCache(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockEmbedding.AssertExpectations(t)
	})
}

func TestConvertProviderInfoToResponseWithValues(t *testing.T) {
	t.Run("convert non-nil provider info", func(t *testing.T) {
		now := time.Now()
		info := &service.ProviderInfo{
			Name:               "openai",
			Type:               service.EmbeddingProviderOpenAI,
			Model:              "text-embedding-ada-002",
			Dimensions:         1536,
			MaxTokens:          8191,
			MaxBatchSize:       2048,
			Status:             service.EmbeddingStatusAvailable,
			APIURL:             "https://api.openai.com",
			RateLimitPerMinute: 60,
			Timeout:            30 * time.Second,
			IsDefault:          true,
			LastError:          "some error",
			LastErrorTime:      &now,
			SuccessCount:       100,
			ErrorCount:         5,
			AvgLatency:         150 * time.Millisecond,
		}

		result := convertProviderInfoToResponse(info)

		assert.NotNil(t, result)
		assert.Equal(t, "openai", result.Name)
		assert.Equal(t, service.EmbeddingProviderOpenAI, result.Type)
		assert.Equal(t, "text-embedding-ada-002", result.Model)
		assert.Equal(t, 1536, result.Dimensions)
		assert.Equal(t, 8191, result.MaxTokens)
		assert.Equal(t, 2048, result.MaxBatchSize)
		assert.Equal(t, service.EmbeddingStatusAvailable, result.Status)
		assert.Equal(t, "https://api.openai.com", result.APIURL)
		assert.Equal(t, 60, result.RateLimitPerMinute)
		assert.Equal(t, "30s", result.Timeout)
		assert.True(t, result.IsDefault)
		assert.Equal(t, "some error", result.LastError)
		assert.NotNil(t, result.LastErrorTime)
		assert.Equal(t, uint64(100), result.SuccessCount)
		assert.Equal(t, uint64(5), result.ErrorCount)
		assert.Equal(t, "150ms", result.AvgLatency)
	})
}

func TestHandleEmbeddingSetProviderStatus(t *testing.T) {
	tests := []struct {
		name           string
		providerName   string
		body           string
		setupMock      func(*MockEmbeddingService)
		expectedStatus int
		checkEnabled   *bool
	}{
		{
			name:         "enable provider successfully",
			providerName: "openai",
			body:         `{"enabled":true}`,
			setupMock: func(m *MockEmbeddingService) {
				enabled := true
				m.On("SetProviderStatus", mock.Anything, &service.SetProviderStatusRequest{
					Name:    "openai",
					Enabled: true,
				}).Return(&service.SetProviderStatusResponse{
					Name:      "openai",
					Enabled:   true,
					Timestamp: time.Now(),
				}, nil).Once()
				_ = enabled
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:         "disable provider successfully",
			providerName: "voyage",
			body:         `{"enabled":false}`,
			setupMock: func(m *MockEmbeddingService) {
				m.On("SetProviderStatus", mock.Anything, &service.SetProviderStatusRequest{
					Name:    "voyage",
					Enabled: false,
				}).Return(&service.SetProviderStatusResponse{
					Name:      "voyage",
					Enabled:   false,
					Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing provider name returns 400",
			providerName:   "",
			body:           `{"enabled":true}`,
			setupMock:      func(m *MockEmbeddingService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:         "invalid JSON returns 400",
			providerName: "openai",
			body:         `not-json`,
			setupMock:    func(m *MockEmbeddingService) {},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockQuicBaaS, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
			defer cleanup()
			tt.setupMock(mockEmbedding)
			mockQuicBaaS.EmbeddingService = mockEmbedding

			req := httptest.NewRequest(http.MethodPut, "/api/v1/embedding/providers/"+tt.providerName+"/status",
				strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tt.providerName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleEmbeddingSetProviderStatus(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockEmbedding.AssertExpectations(t)
		})
	}
}

// ---------------------------------------------------------------------------
// Regression tests: HandleEmbeddingListProviders must return provider type
// (Issue 3 fix — ListProviders path omitted Type field)
// ---------------------------------------------------------------------------

// TestHandleEmbeddingListProviders_ProviderTypePresent asserts that each entry
// in the providers list includes a non-empty "type" field matching the provider
// name. This gates the regression where the embeddingService.ListProviders
// method built ProviderInfo without setting the Type field.
func TestHandleEmbeddingListProviders_ProviderTypePresent(t *testing.T) {
	server, _, mockEmbedding, cleanup := setupMockServerWithEmbedding(t)
	defer cleanup()

	mockEmbedding.On("ListProviders", mock.Anything, mock.Anything).Return(&service.ListProvidersResponse{
		Providers: []*service.ProviderInfo{
			{
				Name:   "ollama",
				Type:   service.EmbeddingProviderOllama,
				Model:  "nomic-embed-text",
				Status: service.EmbeddingStatusAvailable,
			},
		},
		DefaultProvider: "ollama",
		Timestamp:       time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/embedding/providers", nil)
	w := httptest.NewRecorder()

	server.HandleEmbeddingListProviders(w, req)

	require.Equal(t, http.StatusOK, w.Code, "unexpected HTTP status: %s", w.Body.String())

	// Use the wire-format body to assert the field is present and non-empty.
	// This avoids import dependency on encoding/json in the assertion itself.
	respBody := w.Body.String()
	assert.Contains(t, respBody, `"type":"ollama"`,
		"provider list response must include type field with value 'ollama'")
	mockEmbedding.AssertExpectations(t)
}
