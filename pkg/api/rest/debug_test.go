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
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func TestRegisterDebug(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	t.Run("debug routes not registered when env not set", func(t *testing.T) {
		// Ensure env var is not set
		os.Unsetenv("QUICBAAS_ENABLE_DEBUG_ROUTES")

		mockSvc := testutil.NewMockQuicBaaS()
		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)
		defer server.Close()

		router := chi.NewRouter()
		registerDebug(server, router)

		// Should not register any routes
		req := httptest.NewRequest(http.MethodGet, "/api/v1/debug/route/users/123", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Should be 404 since routes were not registered
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("debug routes registered when env set to 1", func(t *testing.T) {
		// Set env var to enable debug routes
		os.Setenv("QUICBAAS_ENABLE_DEBUG_ROUTES", "1")
		defer os.Unsetenv("QUICBAAS_ENABLE_DEBUG_ROUTES")

		mockSvc := testutil.NewMockQuicBaaS()
		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)
		defer server.Close()

		// Set up a mock shard router
		mockRouter := &mockShardRouter{returnValue: 42}
		server.SetShardRouter(mockRouter)

		router := chi.NewRouter()
		registerDebug(server, router)

		// Now routes should be registered
		req := httptest.NewRequest(http.MethodGet, "/api/v1/debug/route/users/123", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "42", w.Body.String())
	})

	t.Run("debug route handles entity type with numeric id", func(t *testing.T) {
		os.Setenv("QUICBAAS_ENABLE_DEBUG_ROUTES", "1")
		defer os.Unsetenv("QUICBAAS_ENABLE_DEBUG_ROUTES")

		mockSvc := testutil.NewMockQuicBaaS()
		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)
		defer server.Close()

		mockRouter := &mockShardRouter{returnValue: 99}
		server.SetShardRouter(mockRouter)

		router := chi.NewRouter()
		registerDebug(server, router)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/debug/route/orders/456", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "99", w.Body.String())
		assert.Equal(t, "orders", mockRouter.lastEntityType)
		assert.Equal(t, uint64(456), mockRouter.lastID)
	})

	t.Run("debug route handles empty id gracefully", func(t *testing.T) {
		os.Setenv("QUICBAAS_ENABLE_DEBUG_ROUTES", "1")
		defer os.Unsetenv("QUICBAAS_ENABLE_DEBUG_ROUTES")

		mockSvc := testutil.NewMockQuicBaaS()
		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)
		defer server.Close()

		mockRouter := &mockShardRouter{returnValue: 1}
		server.SetShardRouter(mockRouter)

		router := chi.NewRouter()
		registerDebug(server, router)

		// id param missing should default to 0
		req := httptest.NewRequest(http.MethodGet, "/api/v1/debug/route/products/", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Route pattern is /route/{type}/{id} so empty id won't match
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("debug route handles invalid id gracefully", func(t *testing.T) {
		os.Setenv("QUICBAAS_ENABLE_DEBUG_ROUTES", "1")
		defer os.Unsetenv("QUICBAAS_ENABLE_DEBUG_ROUTES")

		mockSvc := testutil.NewMockQuicBaaS()
		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)
		defer server.Close()

		mockRouter := &mockShardRouter{returnValue: 5}
		server.SetShardRouter(mockRouter)

		router := chi.NewRouter()
		registerDebug(server, router)

		// Invalid id (non-numeric) should result in id=0
		req := httptest.NewRequest(http.MethodGet, "/api/v1/debug/route/items/abc", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "5", w.Body.String())
		// When parsing fails, id stays at 0
		assert.Equal(t, uint64(0), mockRouter.lastID)
	})

	t.Run("debug routes not registered when env set to other values", func(t *testing.T) {
		os.Setenv("QUICBAAS_ENABLE_DEBUG_ROUTES", "true") // Not "1"
		defer os.Unsetenv("QUICBAAS_ENABLE_DEBUG_ROUTES")

		mockSvc := testutil.NewMockQuicBaaS()
		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)
		defer server.Close()

		router := chi.NewRouter()
		registerDebug(server, router)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/debug/route/users/123", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Should be 404 since "true" != "1"
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// mockShardRouter is a mock implementation of the shard router interface.
type mockShardRouter struct {
	returnValue    uint64
	lastEntityType string
	lastID         uint64
}

func (m *mockShardRouter) Route(entityType string, id uint64) uint64 {
	m.lastEntityType = entityType
	m.lastID = id
	return m.returnValue
}
