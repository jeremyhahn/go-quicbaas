//go:build rest && iam

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

const (
	gdprTenant = "tenant-gdpr"
	gdprUser   = "user-gdpr"
)

// gdprMockService exposes a real GDPRManager to the IAM handlers.
type gdprMockService struct {
	*testutil.MockQuicBaaS
	manager *iam.GDPRManager
}

func (m *gdprMockService) GetGDPRManager(_ context.Context) (*iam.GDPRManager, error) {
	return m.manager, nil
}

func setupGDPRHandler(t *testing.T) (*IAMHandler, func()) {
	t.Helper()
	memStorage := iam.NewMemoryStorage()
	manager, err := iam.NewGDPRManager(iam.GDPRManagerDependencies{
		ConsentStorage:    iam.NewMemoryConsentStorage(),
		DataExportStorage: iam.NewMemoryDataExportStorage(),
		DeletionStorage:   iam.NewMemoryDeletionStorage(),
		UserStorage:       memStorage,
	})
	require.NoError(t, err)

	// Register the subject user so export/deletion can read it.
	require.NoError(t, memStorage.CreateUser(context.Background(), &iam.User{
		ID: gdprUser, TenantID: gdprTenant, Email: "gdpr@example.com",
	}))

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mockSvc := &gdprMockService{MockQuicBaaS: testutil.NewMockQuicBaaS(), manager: manager}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc

	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})
	return handler, func() { server.Close() }
}

func gdprRequest(t *testing.T, method string, body interface{}, params map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, "/x", &buf)
	if body != nil {
		req.ContentLength = int64(buf.Len())
	}
	return withChiParams(req, params)
}

// Full consent lifecycle: grant (201), grant again (200 update), get, list,
// withdraw.
func TestGDPRConsentLifecycle(t *testing.T) {
	handler, cleanup := setupGDPRHandler(t)
	defer cleanup()

	userParams := map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser}
	grantBody := map[string]interface{}{"purpose": "marketing", "version": "1.0"}

	// Grant (new) -> 201
	w := httptest.NewRecorder()
	handler.GrantConsent(w, gdprRequest(t, http.MethodPost, grantBody, userParams))
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	// Grant again (update) -> 200
	w = httptest.NewRecorder()
	handler.GrantConsent(w, gdprRequest(t, http.MethodPost, grantBody, userParams))
	require.Equal(t, http.StatusOK, w.Code)

	// Get
	w = httptest.NewRecorder()
	handler.GetConsent(w, gdprRequest(t, http.MethodGet, nil,
		map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser, "purpose": "marketing"}))
	require.Equal(t, http.StatusOK, w.Code)

	// List
	w = httptest.NewRecorder()
	handler.ListUserConsents(w, gdprRequest(t, http.MethodGet, nil, userParams))
	require.Equal(t, http.StatusOK, w.Code)

	// Withdraw
	w = httptest.NewRecorder()
	handler.WithdrawConsent(w, gdprRequest(t, http.MethodPut, map[string]string{"ip_address": "1.2.3.4"},
		map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser, "purpose": "marketing"}))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

func TestGDPRConsent_Validation(t *testing.T) {
	handler, cleanup := setupGDPRHandler(t)
	defer cleanup()
	userParams := map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser}

	// Missing purpose -> 400
	w := httptest.NewRecorder()
	handler.GrantConsent(w, gdprRequest(t, http.MethodPost, map[string]string{"version": "1.0"}, userParams))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Missing version -> 400
	w = httptest.NewRecorder()
	handler.GrantConsent(w, gdprRequest(t, http.MethodPost, map[string]string{"purpose": "marketing"}, userParams))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Invalid JSON -> 400
	req := withChiParams(httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader([]byte("{bad"))), userParams)
	req.ContentLength = 4
	w = httptest.NewRecorder()
	handler.GrantConsent(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// Data export request -> status -> (not-ready) download conflict.
func TestGDPRExportLifecycle(t *testing.T) {
	handler, cleanup := setupGDPRHandler(t)
	defer cleanup()
	userParams := map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser}

	// Request export -> 202
	w := httptest.NewRecorder()
	handler.RequestDataExport(w, gdprRequest(t, http.MethodPost,
		map[string]interface{}{"format": "json"}, userParams))
	require.Equal(t, http.StatusAccepted, w.Code, "body: %s", w.Body.String())
	var exp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&exp))
	exportID, _ := exp["id"].(string)
	require.NotEmpty(t, exportID)

	statusParams := map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser, "export_id": exportID}

	// Status -> 200
	w = httptest.NewRecorder()
	handler.GetExportStatus(w, gdprRequest(t, http.MethodGet, nil, statusParams))
	require.Equal(t, http.StatusOK, w.Code)

	// Wrong export ID -> 404 from manager error path / not found mapping.
	w = httptest.NewRecorder()
	handler.GetExportStatus(w, gdprRequest(t, http.MethodGet, nil,
		map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser, "export_id": "does-not-exist"}))
	assert.NotEqual(t, http.StatusOK, w.Code)
}

// Data deletion request -> status -> cancel.
func TestGDPRDeletionLifecycle(t *testing.T) {
	handler, cleanup := setupGDPRHandler(t)
	defer cleanup()
	userParams := map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser}

	// Request deletion -> 202
	w := httptest.NewRecorder()
	handler.RequestDeletion(w, gdprRequest(t, http.MethodPost,
		map[string]interface{}{"reason": "user request", "grace_period_days": 30}, userParams))
	require.Equal(t, http.StatusAccepted, w.Code, "body: %s", w.Body.String())
	var del map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&del))
	deletionID, _ := del["id"].(string)
	require.NotEmpty(t, deletionID)

	delParams := map[string]string{"tenant_id": gdprTenant, "user_id": gdprUser, "deletion_id": deletionID}

	// Status -> 200
	w = httptest.NewRecorder()
	handler.GetDeletionStatus(w, gdprRequest(t, http.MethodGet, nil, delParams))
	require.Equal(t, http.StatusOK, w.Code)

	// Cancel -> 200
	w = httptest.NewRecorder()
	handler.CancelDeletion(w, gdprRequest(t, http.MethodDelete, nil, delParams))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

// Missing path params return 400 across the GDPR surface.
func TestGDPR_MissingParams(t *testing.T) {
	handler, cleanup := setupGDPRHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.GrantConsent(w, gdprRequest(t, http.MethodPost, map[string]string{"purpose": "marketing", "version": "1"},
		map[string]string{"tenant_id": ""}))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = httptest.NewRecorder()
	handler.GetConsent(w, gdprRequest(t, http.MethodGet, nil,
		map[string]string{"tenant_id": gdprTenant, "user_id": ""}))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
