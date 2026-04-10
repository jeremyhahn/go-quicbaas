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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPhase4HandlerCreateBackup tests the Phase4Handler.HandleCreateBackup handler.
func TestPhase4HandlerCreateBackup(t *testing.T) {
	t.Run("wrong method returns 405", func(t *testing.T) {
		handler := &Phase4Handler{}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backups", nil)
		w := httptest.NewRecorder()

		handler.HandleCreateBackup(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		handler := &Phase4Handler{}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.HandleCreateBackup(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestPhase4HandlerListBackups tests the Phase4Handler.HandleListBackups handler.
func TestPhase4HandlerListBackups(t *testing.T) {
	t.Run("wrong method returns 405", func(t *testing.T) {
		handler := &Phase4Handler{}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups", nil)
		w := httptest.NewRecorder()

		handler.HandleListBackups(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

// TestPhase4HandlerRestoreBackup tests the Phase4Handler.HandleRestoreBackup handler.
func TestPhase4HandlerRestoreBackup(t *testing.T) {
	t.Run("wrong method returns 405", func(t *testing.T) {
		handler := &Phase4Handler{}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backups/backup-123/restore", nil)
		w := httptest.NewRecorder()

		handler.HandleRestoreBackup(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		handler := &Phase4Handler{}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups/backup-123/restore", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.HandleRestoreBackup(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestPhase4HandlerDeleteBackup tests the Phase4Handler.HandleDeleteBackup handler.
func TestPhase4HandlerDeleteBackup(t *testing.T) {
	t.Run("wrong method returns 405", func(t *testing.T) {
		handler := &Phase4Handler{}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups/backup-123", nil)
		w := httptest.NewRecorder()

		handler.HandleDeleteBackup(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

// TestPhase4HandlerLogin tests the Phase4Handler.HandleLogin handler.
func TestPhase4HandlerLogin(t *testing.T) {
	t.Run("wrong method returns 405", func(t *testing.T) {
		handler := &Phase4Handler{}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil)
		w := httptest.NewRecorder()

		handler.HandleLogin(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		handler := &Phase4Handler{}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.HandleLogin(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestPhase4HandlerLogout tests the Phase4Handler.HandleLogout handler.
func TestPhase4HandlerLogout(t *testing.T) {
	t.Run("wrong method returns 405", func(t *testing.T) {
		handler := &Phase4Handler{}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/logout", nil)
		w := httptest.NewRecorder()

		handler.HandleLogout(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		handler := &Phase4Handler{}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.HandleLogout(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestPhase4HandlerRefreshToken tests the Phase4Handler.HandleRefreshToken handler.
func TestPhase4HandlerRefreshToken(t *testing.T) {
	t.Run("wrong method returns 405", func(t *testing.T) {
		handler := &Phase4Handler{}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/refresh", nil)
		w := httptest.NewRecorder()

		handler.HandleRefreshToken(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		handler := &Phase4Handler{}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.HandleRefreshToken(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestPhase4HandlerCreateUser tests the Phase4Handler.HandleCreateUser handler.
func TestPhase4HandlerCreateUser(t *testing.T) {
	t.Run("wrong method returns 405", func(t *testing.T) {
		handler := &Phase4Handler{}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
		w := httptest.NewRecorder()

		handler.HandleCreateUser(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		handler := &Phase4Handler{}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.HandleCreateUser(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestPhase4HandlerListUsers tests the Phase4Handler.HandleListUsers handler.
func TestPhase4HandlerListUsers(t *testing.T) {
	t.Run("wrong method returns 405", func(t *testing.T) {
		handler := &Phase4Handler{}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/users", nil)
		w := httptest.NewRecorder()

		handler.HandleListUsers(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

// TestPhase4HandlerGetCacheStats tests the Phase4Handler.HandleGetCacheStats handler.
func TestPhase4HandlerGetCacheStats(t *testing.T) {
	t.Run("wrong method returns 405", func(t *testing.T) {
		handler := &Phase4Handler{}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/cache/stats", nil)
		w := httptest.NewRecorder()

		handler.HandleGetCacheStats(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("nil cache manager returns 503", func(t *testing.T) {
		handler := &Phase4Handler{
			cacheMgr: nil,
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/cache/stats", nil)
		w := httptest.NewRecorder()

		handler.HandleGetCacheStats(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.Contains(t, w.Body.String(), "Cache manager not initialized")
	})
}

// TestPhase4HandlerClearCache tests the Phase4Handler.HandleClearCache handler.
func TestPhase4HandlerClearCache(t *testing.T) {
	t.Run("wrong method returns 405", func(t *testing.T) {
		handler := &Phase4Handler{}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/cache/clear", nil)
		w := httptest.NewRecorder()

		handler.HandleClearCache(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("nil cache manager returns 503", func(t *testing.T) {
		handler := &Phase4Handler{
			cacheMgr: nil,
		}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/cache/clear", nil)
		w := httptest.NewRecorder()

		handler.HandleClearCache(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.Contains(t, w.Body.String(), "Cache manager not initialized")
	})
}

// TestExtractPathParam tests the extractPathParam helper function.
func TestExtractPathParam(t *testing.T) {
	t.Run("extract backup ID from restore path", func(t *testing.T) {
		path := "/api/v1/backups/backup-123/restore"
		result := extractPathParam(path, "/api/v1/backups/", "/restore")
		assert.Equal(t, "backup-123", result)
	})

	t.Run("extract backup ID from delete path", func(t *testing.T) {
		path := "/api/v1/backups/backup-456"
		result := extractPathParam(path, "/api/v1/backups/", "")
		assert.Equal(t, "backup-456", result)
	})

	t.Run("empty prefix", func(t *testing.T) {
		path := "backup-789/restore"
		result := extractPathParam(path, "", "/restore")
		assert.Equal(t, "backup-789", result)
	})

	t.Run("empty suffix", func(t *testing.T) {
		path := "/api/v1/backups/backup-101"
		result := extractPathParam(path, "/api/v1/backups/", "")
		assert.Equal(t, "backup-101", result)
	})
}

// TestNewPhase4Handler tests the Phase4Handler constructor.
func TestNewPhase4Handler(t *testing.T) {
	t.Run("creates handler with nil managers", func(t *testing.T) {
		handler := NewPhase4Handler(nil, nil, nil)
		assert.NotNil(t, handler)
		assert.Nil(t, handler.backupMgr)
		assert.Nil(t, handler.authMgr)
		assert.Nil(t, handler.cacheMgr)
	})
}
