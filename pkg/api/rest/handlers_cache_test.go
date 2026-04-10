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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestHandleGetCacheStats(t *testing.T) {
	t.Run("successful cache stats retrieval", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("GetCacheStats", mock.Anything, mock.Anything).Return(&service.GetCacheStatsResponse{
			Size:      500,
			MaxSize:   1000,
			Hits:      800,
			Misses:    200,
			HitRate:   0.8,
			Evictions: 50,
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/cache/stats", nil)
		w := httptest.NewRecorder()

		server.HandleGetCacheStats(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp CacheStatsResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.True(t, resp.Enabled)
		assert.Equal(t, int64(500), resp.L1Size)
		assert.Equal(t, int64(1000), resp.L1Capacity)
		assert.Equal(t, int64(800), resp.L1Hits)
		assert.Equal(t, int64(200), resp.L1Misses)
		assert.InDelta(t, 0.8, resp.L1HitRate, 0.01)
		assert.Equal(t, int64(50), resp.L1Evictions)
	})

	t.Run("service error on cache stats", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("GetCacheStats", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrInternal, "cache", "stats unavailable")).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/cache/stats", nil)
		w := httptest.NewRecorder()

		server.HandleGetCacheStats(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("wrong method returns method not allowed", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/cache/stats", nil)
		w := httptest.NewRecorder()

		server.HandleGetCacheStats(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestHandleClearCache(t *testing.T) {
	t.Run("successful cache clear", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("ClearCache", mock.Anything, mock.MatchedBy(func(req *service.ClearCacheRequest) bool {
			return req.All == true
		})).Return(&service.ClearCacheResponse{
			EntriesCleared: 250,
		}, nil).Once()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/cache/clear", nil)
		w := httptest.NewRecorder()

		server.HandleClearCache(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ClearCacheResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.Equal(t, int32(250), resp.EntriesCleared)
	})

	t.Run("service error on cache clear", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("ClearCache", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrInternal, "cache", "clear failed")).Once()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/cache/clear", nil)
		w := httptest.NewRecorder()

		server.HandleClearCache(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("wrong method returns method not allowed", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/cache/clear", nil)
		w := httptest.NewRecorder()

		server.HandleClearCache(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestHandleGetCacheConfig(t *testing.T) {
	t.Run("successful cache config retrieval", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("GetCacheConfig", mock.Anything, mock.Anything).Return(&service.GetCacheConfigResponse{
			Enabled:         true,
			L1Size:          1000,
			L1TTL:           300 * time.Second,
			CleanupInterval: 60 * time.Second,
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/cache/config", nil)
		w := httptest.NewRecorder()

		server.HandleGetCacheConfig(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp CacheConfigResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.True(t, resp.Enabled)
		assert.Equal(t, int32(1000), resp.L1Size)
		assert.Equal(t, int64(300), resp.L1TTLSeconds)
		assert.Equal(t, int64(60), resp.CleanupIntervalSeconds)
	})

	t.Run("service error on cache config", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("GetCacheConfig", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrInternal, "cache", "config unavailable")).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/cache/config", nil)
		w := httptest.NewRecorder()

		server.HandleGetCacheConfig(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("wrong method returns method not allowed", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/cache/config", nil)
		w := httptest.NewRecorder()

		server.HandleGetCacheConfig(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestHandleUpdateCacheConfig(t *testing.T) {
	t.Run("successful config update with all fields", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("UpdateCacheConfig", mock.Anything, mock.Anything).Return(&service.UpdateCacheConfigResponse{
			Updated: true,
		}, nil).Once()

		body := `{"l1_size":2000,"l1_ttl_seconds":600}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/cache/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleUpdateCacheConfig(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp UpdateCacheConfigResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.Equal(t, "Cache configuration updated successfully", resp.Message)
	})

	t.Run("update with no changes", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("UpdateCacheConfig", mock.Anything, mock.Anything).Return(&service.UpdateCacheConfigResponse{
			Updated: false,
		}, nil).Once()

		body := `{}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/cache/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleUpdateCacheConfig(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp UpdateCacheConfigResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.False(t, resp.Success)
		assert.Equal(t, "Cache configuration unchanged", resp.Message)
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPut, "/api/v1/cache/config", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleUpdateCacheConfig(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service error on update", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("UpdateCacheConfig", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrInternal, "cache", "update failed")).Once()

		body := `{"l1_size":2000}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/cache/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleUpdateCacheConfig(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("wrong method returns method not allowed", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/cache/config-update", nil)
		w := httptest.NewRecorder()

		server.HandleUpdateCacheConfig(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}
