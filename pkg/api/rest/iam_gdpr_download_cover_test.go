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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

type gdprDownloadService struct {
	*testutil.MockQuicBaaS
	manager *iam.GDPRManager
}

func (m *gdprDownloadService) GetGDPRManager(_ context.Context) (*iam.GDPRManager, error) {
	return m.manager, nil
}

// setupGDPRDownloadHandler retains the export storage so a test can mark an
// export completed and exercise the download path.
func setupGDPRDownloadHandler(t *testing.T) (*IAMHandler, *iam.MemoryDataExportStorage, func()) {
	t.Helper()
	memStorage := iam.NewMemoryStorage()
	exportStore := iam.NewMemoryDataExportStorage()
	manager, err := iam.NewGDPRManager(iam.GDPRManagerDependencies{
		ConsentStorage:    iam.NewMemoryConsentStorage(),
		DataExportStorage: exportStore,
		DeletionStorage:   iam.NewMemoryDeletionStorage(),
		UserStorage:       memStorage,
	})
	require.NoError(t, err)

	require.NoError(t, memStorage.CreateUser(context.Background(), &iam.User{
		ID: 71, TenantID: gdprTenant, Email: "gdpr@example.com",
	}))

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mockSvc := &gdprDownloadService{MockQuicBaaS: testutil.NewMockQuicBaaS(), manager: manager}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})
	return handler, exportStore, func() { server.Close() }
}

func TestDownloadExport_Validation(t *testing.T) {
	handler, _, cleanup := setupGDPRDownloadHandler(t)
	defer cleanup()

	// Missing export_id -> 400
	w := httptest.NewRecorder()
	handler.DownloadExport(w, withChiParams(httptest.NewRequest(http.MethodGet, "/x", nil),
		map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser, "export_id": ""}))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDownloadExport_NotReady(t *testing.T) {
	handler, _, cleanup := setupGDPRDownloadHandler(t)
	defer cleanup()
	userParams := map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser}

	// Create an export (status pending/processing).
	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]interface{}{"format": "json"})
	handler.RequestDataExport(w, withChiParams(
		httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(body)), userParams))
	require.Equal(t, http.StatusAccepted, w.Code)
	var exp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&exp))
	exportID := exp["id"].(string)

	// Download before completion -> 409 (or other non-200).
	w = httptest.NewRecorder()
	handler.DownloadExport(w, withChiParams(httptest.NewRequest(http.MethodGet, "/x", nil),
		map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser, "export_id": exportID}))
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestDownloadExport_Completed(t *testing.T) {
	handler, exportStore, cleanup := setupGDPRDownloadHandler(t)
	defer cleanup()
	ctx := context.Background()
	userParams := map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser}

	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]interface{}{"format": "json"})
	handler.RequestDataExport(w, withChiParams(
		httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(body)), userParams))
	require.Equal(t, http.StatusAccepted, w.Code)
	var exp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&exp))
	exportID := exp["id"].(string)

	// Force the export to Completed so the download path runs GenerateExport.
	rec, err := exportStore.GetExportRequest(ctx, exportID)
	require.NoError(t, err)
	rec.Status = iam.DataExportStatusCompleted
	require.NoError(t, exportStore.UpdateExportRequest(ctx, rec))

	w = httptest.NewRecorder()
	handler.DownloadExport(w, withChiParams(httptest.NewRequest(http.MethodGet, "/x", nil),
		map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser, "export_id": exportID}))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}

func TestDownloadExport_WrongUser(t *testing.T) {
	handler, _, cleanup := setupGDPRDownloadHandler(t)
	defer cleanup()
	userParams := map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser}

	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]interface{}{"format": "json"})
	handler.RequestDataExport(w, withChiParams(
		httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(body)), userParams))
	require.Equal(t, http.StatusAccepted, w.Code)
	var exp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&exp))
	exportID := exp["id"].(string)

	// Download as a different user -> 404.
	w = httptest.NewRecorder()
	handler.DownloadExport(w, withChiParams(httptest.NewRequest(http.MethodGet, "/x", nil),
		map[string]string{"tenant_id": gdprTenant, "user_id": "40002", "export_id": exportID}))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// DownloadExport sets a CSV content type for a CSV-format completed export.
func TestDownloadExport_CSVFormat(t *testing.T) {
	handler, exportStore, cleanup := setupGDPRDownloadHandler(t)
	defer cleanup()
	ctx := context.Background()
	userParams := map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser}

	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]interface{}{"format": "csv"})
	handler.RequestDataExport(w, withChiParams(
		httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(body)), userParams))
	require.Equal(t, http.StatusAccepted, w.Code)
	var exp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&exp))
	exportID := exp["id"].(string)

	rec, err := exportStore.GetExportRequest(ctx, exportID)
	require.NoError(t, err)
	rec.Status = iam.DataExportStatusCompleted
	require.NoError(t, exportStore.UpdateExportRequest(ctx, rec))

	w = httptest.NewRecorder()
	handler.DownloadExport(w, withChiParams(httptest.NewRequest(http.MethodGet, "/x", nil),
		map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser, "export_id": exportID}))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/csv", w.Header().Get("Content-Type"))
}

// GetExportStatus / GetDeletionStatus missing IDs -> 400.
func TestGDPRStatus_MissingIDs(t *testing.T) {
	handler, _, cleanup := setupGDPRDownloadHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.GetExportStatus(w, withChiParams(httptest.NewRequest(http.MethodGet, "/x", nil),
		map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser, "export_id": ""}))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = httptest.NewRecorder()
	handler.GetDeletionStatus(w, withChiParams(httptest.NewRequest(http.MethodGet, "/x", nil),
		map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser, "deletion_id": ""}))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// CancelDeletion for an unknown deletion request returns a non-200.
func TestCancelDeletion_NotFound(t *testing.T) {
	handler, _, cleanup := setupGDPRDownloadHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.CancelDeletion(w, withChiParams(httptest.NewRequest(http.MethodDelete, "/x", nil),
		map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser, "deletion_id": "missing"}))
	assert.NotEqual(t, http.StatusOK, w.Code)
}

// A processing export (not completed) yields a 409 conflict on download.
func TestDownloadExport_Conflict(t *testing.T) {
	handler, exportStore, cleanup := setupGDPRDownloadHandler(t)
	defer cleanup()
	ctx := context.Background()
	userParams := map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser}

	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]interface{}{"format": "json"})
	handler.RequestDataExport(w, withChiParams(
		httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(body)), userParams))
	require.Equal(t, http.StatusAccepted, w.Code)
	var exp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&exp))
	exportID := exp["id"].(string)

	rec, err := exportStore.GetExportRequest(ctx, exportID)
	require.NoError(t, err)
	rec.Status = iam.DataExportStatusProcessing
	require.NoError(t, exportStore.UpdateExportRequest(ctx, rec))

	w = httptest.NewRecorder()
	handler.DownloadExport(w, withChiParams(httptest.NewRequest(http.MethodGet, "/x", nil),
		map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser, "export_id": exportID}))
	assert.Equal(t, http.StatusConflict, w.Code)
}
