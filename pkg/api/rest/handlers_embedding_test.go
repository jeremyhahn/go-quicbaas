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

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

// All embedding handlers check s.service.Embedding() first.
// When EmbeddingService is nil (default), they return 503 Service Unavailable.

func TestHandleEmbeddingGenerate(t *testing.T) {
	t.Run("embedding service not configured", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		body := `{"text":"hello world"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/embedding/generate", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingGenerate(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.Contains(t, w.Body.String(), "embedding service not configured")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/embedding/generate", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingGenerate(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing text field", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		body := `{"provider":"openai"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/embedding/generate", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingGenerate(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "text is required")
	})
}

func TestHandleEmbeddingBatchGenerate(t *testing.T) {
	t.Run("embedding service not configured with valid items", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		body := `{"items":[{"id":"1","text":"hello"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/embedding/batch", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingBatchGenerate(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/embedding/batch", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingBatchGenerate(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("empty items list", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		body := `{"items":[]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/embedding/batch", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingBatchGenerate(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "items is required")
	})

	t.Run("item with empty text returns 503 because nil check before text validation", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		// The handler checks embeddingSvc == nil before validating individual item texts,
		// so with nil embedding service, this returns 503
		body := `{"items":[{"id":"1","text":""}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/embedding/batch", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingBatchGenerate(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

func TestHandleEmbeddingListProviders(t *testing.T) {
	t.Run("embedding service not configured", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/embedding/providers", nil)
		w := httptest.NewRecorder()

		server.HandleEmbeddingListProviders(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

func TestHandleEmbeddingGetProviderInfo(t *testing.T) {
	t.Run("missing provider URL param", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		// When no chi route context, URLParam returns "", which triggers 400
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("provider", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/embedding/providers/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleEmbeddingGetProviderInfo(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "provider is required")
	})

	t.Run("embedding service not configured with valid provider", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("provider", "openai")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/embedding/providers/openai", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleEmbeddingGetProviderInfo(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

func TestHandleEmbeddingSetDefaultProvider(t *testing.T) {
	t.Run("invalid JSON body", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPut, "/api/v1/embedding/providers/default", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingSetDefaultProvider(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing provider", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		body := `{"provider":""}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/embedding/providers/default", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingSetDefaultProvider(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("embedding service not configured", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		body := `{"provider":"openai"}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/embedding/providers/default", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingSetDefaultProvider(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

func TestHandleEmbeddingGetCacheStats(t *testing.T) {
	t.Run("embedding service not configured", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/embedding/cache/stats", nil)
		w := httptest.NewRecorder()

		server.HandleEmbeddingGetCacheStats(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

func TestHandleEmbeddingClearCache(t *testing.T) {
	t.Run("embedding service not configured", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		body := `{}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/embedding/cache/clear", strings.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleEmbeddingClearCache(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("embedding service not configured with invalid body", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		// ClearCache checks embeddingSvc == nil BEFORE parsing body,
		// so invalid JSON still returns 503
		req := httptest.NewRequest(http.MethodPost, "/api/v1/embedding/cache/clear", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleEmbeddingClearCache(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

func TestConvertProviderInfoToResponse(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		result := convertProviderInfoToResponse(nil)
		assert.Nil(t, result)
	})
}
