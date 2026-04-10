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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestHandleCreateBackup(t *testing.T) {
	t.Run("successful backup creation with defaults", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("CreateBackup", mock.Anything, mock.Anything).Return(&service.CreateBackupResponse{
			BackupID:  "backup-001",
			Timestamp: 1700000000,
			Size:      1024000,
			Status:    "completed",
		}, nil).Once()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleCreateBackup(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var resp CreateBackupResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, "backup-001", resp.BackupID)
		assert.Equal(t, int64(1700000000), resp.Timestamp)
		assert.Equal(t, uint64(1024000), resp.Size)
		assert.Equal(t, "completed", resp.Status)
	})

	t.Run("successful backup creation with options", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("CreateBackup", mock.Anything, mock.MatchedBy(func(req *service.CreateBackupRequest) bool {
			return req.Name == "daily-backup" && req.Compression == "gzip"
		})).Return(&service.CreateBackupResponse{
			BackupID:  "backup-002",
			Timestamp: 1700000001,
			Size:      512000,
			Status:    "completed",
		}, nil).Once()

		body := `{"name":"daily-backup","description":"Daily backup","compression":"gzip","encryption":"aes256","include_shards":[1,2]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleCreateBackup(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("service error on backup creation", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("CreateBackup", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrInternal, "backup", "disk full")).Once()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleCreateBackup(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("empty body uses defaults", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("CreateBackup", mock.Anything, mock.Anything).Return(&service.CreateBackupResponse{
			BackupID:  "backup-003",
			Timestamp: 1700000002,
			Size:      0,
			Status:    "completed",
		}, nil).Once()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups", strings.NewReader(""))
		w := httptest.NewRecorder()

		server.HandleCreateBackup(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})
}

func TestHandleListBackups(t *testing.T) {
	t.Run("successful list with defaults", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("ListBackups", mock.Anything, mock.MatchedBy(func(req *service.ListBackupsRequest) bool {
			return req.Limit == 100 && req.Offset == 0
		})).Return(&service.ListBackupsResponse{
			Backups: []*service.BackupMetadata{
				{
					BackupID:    "backup-001",
					Name:        "test-backup",
					Description: "Test backup",
					Timestamp:   1700000000,
					Size:        1024000,
					Status:      "completed",
					Shards:      []uint64{1, 2},
				},
			},
			TotalCount: 1,
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backups", nil)
		w := httptest.NewRecorder()

		server.HandleListBackups(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ListBackupsResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, uint64(1), resp.TotalCount)
		require.Len(t, resp.Backups, 1)
		assert.Equal(t, "backup-001", resp.Backups[0].BackupID)
		assert.Equal(t, "test-backup", resp.Backups[0].Name)
		assert.Equal(t, []uint64{1, 2}, resp.Backups[0].Shards)
	})

	t.Run("list with custom limit and offset", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("ListBackups", mock.Anything, mock.MatchedBy(func(req *service.ListBackupsRequest) bool {
			return req.Limit == 10 && req.Offset == 20
		})).Return(&service.ListBackupsResponse{
			Backups:    []*service.BackupMetadata{},
			TotalCount: 0,
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backups?limit=10&offset=20", nil)
		w := httptest.NewRecorder()

		server.HandleListBackups(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("list with invalid limit uses default", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("ListBackups", mock.Anything, mock.MatchedBy(func(req *service.ListBackupsRequest) bool {
			return req.Limit == 100 // default
		})).Return(&service.ListBackupsResponse{
			Backups:    []*service.BackupMetadata{},
			TotalCount: 0,
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backups?limit=invalid", nil)
		w := httptest.NewRecorder()

		server.HandleListBackups(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("service error on list", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("ListBackups", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrInternal, "backup", "storage error")).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backups", nil)
		w := httptest.NewRecorder()

		server.HandleListBackups(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestHandleRestoreBackup(t *testing.T) {
	t.Run("successful restore", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("RestoreBackup", mock.Anything, mock.MatchedBy(func(req *service.RestoreBackupRequest) bool {
			return req.BackupID == "backup-001"
		})).Return(&service.RestoreBackupResponse{
			JobID:    "restore-job-001",
			Status:   "in_progress",
			Progress: 0.0,
		}, nil).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "backup-001")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups/backup-001/restore", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleRestoreBackup(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp RestoreBackupResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, "restore-job-001", resp.JobID)
		assert.Equal(t, "in_progress", resp.Status)
	})

	t.Run("missing backup ID", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups//restore", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleRestoreBackup(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service error on restore", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("RestoreBackup", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrInternal, "backup", "restore failed")).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "backup-001")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups/backup-001/restore", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleRestoreBackup(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("restore with target shards", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("RestoreBackup", mock.Anything, mock.MatchedBy(func(req *service.RestoreBackupRequest) bool {
			return len(req.TargetShards) == 2
		})).Return(&service.RestoreBackupResponse{
			JobID:  "restore-job-002",
			Status: "in_progress",
		}, nil).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "backup-001")

		body := `{"target_shards":[1,2]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups/backup-001/restore", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleRestoreBackup(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestHandleDeleteBackup(t *testing.T) {
	t.Run("successful delete", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("DeleteBackup", mock.Anything, mock.MatchedBy(func(req *service.DeleteBackupRequest) bool {
			return req.BackupID == "backup-001"
		})).Return(&service.DeleteBackupResponse{
			Success: true,
		}, nil).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "backup-001")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/backups/backup-001", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleDeleteBackup(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp DeleteBackupResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.True(t, resp.Success)
	})

	t.Run("missing backup ID", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/backups/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleDeleteBackup(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service error on delete", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("DeleteBackup", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrInternal, "backup", "delete failed")).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "backup-001")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/backups/backup-001", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleDeleteBackup(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestHandleVerifyBackup(t *testing.T) {
	t.Run("successful verification with valid backup", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("VerifyBackup", mock.Anything, mock.MatchedBy(func(req *service.VerifyBackupRequest) bool {
			return req.BackupID == "backup-001"
		})).Return(&service.VerifyBackupResponse{
			Valid:         true,
			ChecksumMatch: true,
			Issues:        nil,
		}, nil).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "backup-001")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups/backup-001/verify", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleVerifyBackup(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var resp VerifyBackupResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.True(t, resp.Valid)
		assert.True(t, resp.ChecksumMatch)
		assert.Empty(t, resp.Issues)
	})

	t.Run("successful verification with issues", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("VerifyBackup", mock.Anything, mock.MatchedBy(func(req *service.VerifyBackupRequest) bool {
			return req.BackupID == "backup-002"
		})).Return(&service.VerifyBackupResponse{
			Valid:         false,
			ChecksumMatch: false,
			Issues:        []string{"checksum mismatch on shard 3", "missing metadata file"},
		}, nil).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "backup-002")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups/backup-002/verify", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleVerifyBackup(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp VerifyBackupResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.False(t, resp.Valid)
		assert.False(t, resp.ChecksumMatch)
		require.Len(t, resp.Issues, 2)
		assert.Equal(t, "checksum mismatch on shard 3", resp.Issues[0])
		assert.Equal(t, "missing metadata file", resp.Issues[1])
	})

	t.Run("missing backup ID", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups//verify", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleVerifyBackup(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service error on verify", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("VerifyBackup", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrInternal, "backup", "verification failed")).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "backup-001")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups/backup-001/verify", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleVerifyBackup(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestHandleBackupStatus(t *testing.T) {
	t.Run("successful status for valid backup", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("VerifyBackup", mock.Anything, mock.MatchedBy(func(req *service.VerifyBackupRequest) bool {
			return req.BackupID == "backup-001"
		})).Return(&service.VerifyBackupResponse{
			Valid:         true,
			ChecksumMatch: true,
			Issues:        nil,
		}, nil).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "backup-001")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backups/backup-001/status", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleBackupStatus(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var resp BackupStatusResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, "backup-001", resp.BackupID)
		assert.True(t, resp.Valid)
		assert.True(t, resp.ChecksumMatch)
		assert.Empty(t, resp.Issues)
	})

	t.Run("status for backup with issues", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("VerifyBackup", mock.Anything, mock.MatchedBy(func(req *service.VerifyBackupRequest) bool {
			return req.BackupID == "backup-003"
		})).Return(&service.VerifyBackupResponse{
			Valid:         false,
			ChecksumMatch: false,
			Issues:        []string{"corrupt data block"},
		}, nil).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "backup-003")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backups/backup-003/status", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleBackupStatus(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp BackupStatusResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, "backup-003", resp.BackupID)
		assert.False(t, resp.Valid)
		assert.False(t, resp.ChecksumMatch)
		require.Len(t, resp.Issues, 1)
		assert.Equal(t, "corrupt data block", resp.Issues[0])
	})

	t.Run("missing backup ID", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backups//status", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleBackupStatus(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service error on status", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("VerifyBackup", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrInternal, "backup", "status check failed")).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "backup-001")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/backups/backup-001/status", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleBackupStatus(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
