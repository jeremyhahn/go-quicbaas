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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// appStoreFailureDetail is internal text a failing store reports; no response
// may carry it.
const appStoreFailureDetail = "write /var/lib/quicbaas/apps.db: input/output error"

// appStoreFailureError is the error a failing application store returns.
type appStoreFailureError struct{}

func (appStoreFailureError) Error() string { return appStoreFailureDetail }

// failingWriteAppStorage is an application store whose reads work and whose
// every write fails.
type failingWriteAppStorage struct {
	iam.AppStorage
}

func (failingWriteAppStorage) CreateApp(context.Context, *iam.Application) error {
	return appStoreFailureError{}
}

func (failingWriteAppStorage) UpdateApp(context.Context, *iam.Application) error {
	return appStoreFailureError{}
}

// newAppErrorHandler returns an IAM handler whose app manager runs over storage.
func newAppErrorHandler(t *testing.T, storage iam.AppStorage) *IAMHandler {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	mockSvc := &appMockService{MockQuicBaaS: testutil.NewMockQuicBaaS(), manager: iam.NewAppManager(storage, nil)}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	t.Cleanup(func() { server.Close() })
	server.service = mockSvc
	return NewIAMHandler(IAMHandlerDependencies{Server: server})
}

// TestRegisterApp_MetadataErrorIsBadRequest proves a register refused for its
// metadata answers 400 with the refusal and stores nothing.
func TestRegisterApp_MetadataErrorIsBadRequest(t *testing.T) {
	storage := iam.NewMemoryStorage()
	handler := newAppErrorHandler(t, storage)

	w := httptest.NewRecorder()
	handler.RegisterApp(w, appRequest(t, http.MethodPost, map[string]interface{}{
		"name":                       "Bad Method",
		"redirect_uris":              []string{"https://app.example/callback"},
		"token_endpoint_auth_method": "invalid_method",
	}, map[string]string{"tenant_id": appsTenant}))

	require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "unsupported token endpoint auth method")
	apps, err := storage.ListApps(context.Background(), appsTenant, iam.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, apps)
}

// TestRegisterApp_StorageFailureIsInternal proves a register whose store fails
// answers 500 without the store's text.
func TestRegisterApp_StorageFailureIsInternal(t *testing.T) {
	handler := newAppErrorHandler(t, failingWriteAppStorage{iam.NewMemoryStorage()})

	w := httptest.NewRecorder()
	handler.RegisterApp(w, appRequest(t, http.MethodPost, map[string]interface{}{
		"name":          "Store Fails",
		"redirect_uris": []string{"https://app.example/callback"},
	}, map[string]string{"tenant_id": appsTenant}))

	require.Equal(t, http.StatusInternalServerError, w.Code, "body: %s", w.Body.String())
	assert.NotContains(t, w.Body.String(), appStoreFailureDetail)
}

// TestUpdateApp_MetadataErrorIsBadRequest proves an update refused for its
// default audience answers 400 and leaves the application unchanged.
func TestUpdateApp_MetadataErrorIsBadRequest(t *testing.T) {
	storage := iam.NewMemoryStorage()
	handler := newAppErrorHandler(t, storage)
	clientID := registerUpdateTarget(t, handler)

	w := httptest.NewRecorder()
	handler.UpdateApp(w, appRequest(t, http.MethodPut, map[string]interface{}{
		"name":             "Renamed",
		"default_audience": "relative/audience",
	}, map[string]string{"tenant_id": appsTenant, "client_id": clientID}))

	require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "invalid default audience")
	app, err := storage.GetAppByTenant(context.Background(), appsTenant, clientID)
	require.NoError(t, err)
	assert.Equal(t, "Update Target", app.Name)
	assert.Empty(t, app.DefaultAudience)
}

// TestUpdateApp_StorageFailureIsInternal proves an update whose store fails
// answers 500 without the store's text.
func TestUpdateApp_StorageFailureIsInternal(t *testing.T) {
	storage := iam.NewMemoryStorage()
	clientID := registerUpdateTarget(t, newAppErrorHandler(t, storage))
	handler := newAppErrorHandler(t, failingWriteAppStorage{storage})

	w := httptest.NewRecorder()
	handler.UpdateApp(w, appRequest(t, http.MethodPut, map[string]interface{}{
		"name": "Renamed",
	}, map[string]string{"tenant_id": appsTenant, "client_id": clientID}))

	require.Equal(t, http.StatusInternalServerError, w.Code, "body: %s", w.Body.String())
	assert.NotContains(t, w.Body.String(), appStoreFailureDetail)
}
