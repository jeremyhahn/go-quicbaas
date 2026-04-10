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

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Backend Status & Test Handlers
// =============================================================================

func TestHandleBackendStatus(t *testing.T) {
	t.Run("KMS manager not available", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("KMSManager").Return(nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/backend/status", nil)
		w := httptest.NewRecorder()

		server.HandleBackendStatus(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("KMS manager available and healthy", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockKMS := &MockKMSManager{}
		mockKMS.On("GetBackendType").Return(config.BackendType("local"))
		mockKMS.On("IsAvailable", mock.Anything).Return(true)
		mockService.On("KMSManager").Return(mockKMS)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/backend/status", nil)
		w := httptest.NewRecorder()

		server.HandleBackendStatus(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "healthy")
		mockKMS.AssertExpectations(t)
	})

	t.Run("KMS manager available but unhealthy", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockKMS := &MockKMSManager{}
		mockKMS.On("GetBackendType").Return(config.BackendType("vault"))
		mockKMS.On("IsAvailable", mock.Anything).Return(false)
		mockService.On("KMSManager").Return(mockKMS)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/backend/status", nil)
		w := httptest.NewRecorder()

		server.HandleBackendStatus(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "unavailable")
		mockKMS.AssertExpectations(t)
	})
}

func TestHandleBackendTest(t *testing.T) {
	t.Run("KMS manager not available", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("KMSManager").Return(nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/backend/test", nil)
		w := httptest.NewRecorder()

		server.HandleBackendTest(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("backend test successful", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockKMS := &MockKMSManager{}
		mockKMS.On("IsAvailable", mock.Anything).Return(true)
		mockService.On("KMSManager").Return(mockKMS)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/backend/test", nil)
		w := httptest.NewRecorder()

		server.HandleBackendTest(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "successfully")
		mockKMS.AssertExpectations(t)
	})

	t.Run("backend test failure", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockKMS := &MockKMSManager{}
		mockKMS.On("IsAvailable", mock.Anything).Return(false)
		mockService.On("KMSManager").Return(mockKMS)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/backend/test", nil)
		w := httptest.NewRecorder()

		server.HandleBackendTest(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "not available")
		mockKMS.AssertExpectations(t)
	})
}

// =============================================================================
// Export & Unwrap Key Handlers
// =============================================================================

func TestHandleExportKey(t *testing.T) {
	t.Run("missing key name", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys//export", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleExportKey(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/test-key/export", strings.NewReader("{invalid"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleExportKey(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing wrapping key ID", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")

		body := `{"wrapping_key_id":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/test-key/export", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleExportKey(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service error on export", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("ExportKey", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrNotFound, "kms", "key not found")).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")

		body := `{"wrapping_key_id":"wrap-key"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/test-key/export", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleExportKey(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("successful export key", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("ExportKey", mock.Anything, mock.MatchedBy(func(req *service.ExportKeyRequest) bool {
			return req.KeyID == "test-key" && req.WrappingKeyID == "wrap-key"
		})).Return(&service.ExportKeyResponse{
			KeyID:             "test-key",
			Version:           1,
			WrappedKey:        []byte("wrapped-key-data"),
			WrappingKeyID:     "wrap-key",
			WrappingAlgorithm: "AES-GCM",
			Format:            "raw",
			Timestamp:         time.Now(),
		}, nil).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")

		body := `{"wrapping_key_id":"wrap-key"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/test-key/export", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleExportKey(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "test-key")
		assert.Contains(t, w.Body.String(), "wrap-key")
	})
}

func TestHandleUnwrapKey(t *testing.T) {
	t.Run("invalid JSON body", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/unwrap", strings.NewReader("{invalid"))
		w := httptest.NewRecorder()

		server.HandleUnwrapKey(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing wrapped key", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		body := `{"wrapping_key_id":"wrap-key"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/unwrap", strings.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleUnwrapKey(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing wrapping key ID", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		body := `{"wrapped_key":"AQID","wrapping_key_id":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/unwrap", strings.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleUnwrapKey(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service error on unwrap", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("UnwrapKey", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrInternal, "kms", "unwrap failed")).Once()

		body := `{"wrapped_key":"AQID","wrapping_key_id":"wrap-key"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/unwrap", strings.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleUnwrapKey(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("successful unwrap key without key handle", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("UnwrapKey", mock.Anything, mock.Anything).Return(&service.UnwrapKeyResponse{
			KeyID:     "unwrapped-key",
			Version:   1,
			Algorithm: service.KeyAlgAES256GCM,
			Status:    service.KeyStatusEnabled,
			Key:       nil,
			Timestamp: time.Now(),
		}, nil).Once()

		body := `{"wrapped_key":"AQID","wrapping_key_id":"wrap-key"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/unwrap", strings.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleUnwrapKey(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Contains(t, w.Body.String(), "unwrapped-key")
	})

	t.Run("successful unwrap key with key handle", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("UnwrapKey", mock.Anything, mock.Anything).Return(&service.UnwrapKeyResponse{
			KeyID:     "unwrapped-key",
			Version:   1,
			Algorithm: service.KeyAlgAES256GCM,
			Status:    service.KeyStatusEnabled,
			Key: &service.KeyHandle{
				KeyID:     "unwrapped-key",
				Version:   1,
				Algorithm: service.KeyAlgAES256GCM,
				Status:    service.KeyStatusEnabled,
				CreatedAt: time.Now(),
			},
			Timestamp: time.Now(),
		}, nil).Once()

		body := `{"wrapped_key":"AQID","wrapping_key_id":"wrap-key","key_id":"unwrapped-key"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/unwrap", strings.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleUnwrapKey(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Contains(t, w.Body.String(), "unwrapped-key")
		assert.Contains(t, w.Body.String(), "key")
	})
}

// =============================================================================
// Key Safety Handlers
// =============================================================================

func TestHandleSetKeySafety(t *testing.T) {
	t.Run("missing key name", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "")

		req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys//safety", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleSetKeySafety(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")

		req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/test-key/safety", strings.NewReader("{invalid"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleSetKeySafety(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service error on set safety", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("SetKeySafety", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrNotFound, "kms", "key not found")).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")

		body := `{"max_operations":1000}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/test-key/safety", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleSetKeySafety(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("successful set key safety", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("SetKeySafety", mock.Anything, mock.MatchedBy(func(req *service.SetKeySafetyRequest) bool {
			return req.KeyID == "test-key" && req.MaxOperations == 10000
		})).Return(&service.SetKeySafetyResponse{
			KeyID: "test-key",
			Config: service.AEADSafetyConfigDTO{
				MaxOperations:     10000,
				MaxBytes:          1024 * 1024 * 1024,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			},
			Timestamp: time.Now(),
		}, nil).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")

		body := `{"max_operations":10000,"max_bytes":1073741824,"warning_percent":80,"auto_rotate_percent":95}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/test-key/safety", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleSetKeySafety(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "test-key")
		assert.Contains(t, w.Body.String(), "config")
	})
}

func TestHandleClearKeySafety(t *testing.T) {
	t.Run("missing key name", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/keys//safety", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleClearKeySafety(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service error on clear safety", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("ClearKeySafety", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrNotFound, "kms", "key not found")).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/keys/test-key/safety", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleClearKeySafety(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("successful clear key safety", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("ClearKeySafety", mock.Anything, mock.MatchedBy(func(req *service.ClearKeySafetyRequest) bool {
			return req.KeyID == "test-key"
		})).Return(&service.ClearKeySafetyResponse{
			KeyID:     "test-key",
			Timestamp: time.Now(),
		}, nil).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/keys/test-key/safety", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleClearKeySafety(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "test-key")
	})
}

func TestHandleSafetyCheck(t *testing.T) {
	t.Run("service error on safety check", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("SafetyCheck", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrInternal, "kms", "check failed")).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/safety/check", nil)
		w := httptest.NewRecorder()

		server.HandleSafetyCheck(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with valid threshold parameter", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("SafetyCheck", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrInternal, "kms", "check failed")).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/safety/check?threshold_percent=80.5", nil)
		w := httptest.NewRecorder()

		server.HandleSafetyCheck(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("successful safety check", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("SafetyCheck", mock.Anything, mock.Anything).Return(&service.SafetyCheckResponse{
			Items:            []service.SafetyCheckItem{},
			ThresholdPercent: 80.0,
			TotalKeysChecked: 10,
			TotalDEKsChecked: 5,
			Timestamp:        time.Now(),
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/safety/check", nil)
		w := httptest.NewRecorder()

		server.HandleSafetyCheck(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "threshold_percent")
	})

	t.Run("safety check with items exceeding threshold", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("SafetyCheck", mock.Anything, mock.Anything).Return(&service.SafetyCheckResponse{
			Items: []service.SafetyCheckItem{
				{
					ID:             "key-1",
					Type:           "key",
					OperationCount: 8500,
					BytesProcessed: 0,
					UsagePercent:   85.0,
					MaxOperations:  10000,
					MaxBytes:       0,
				},
			},
			ThresholdPercent: 80.0,
			TotalKeysChecked: 10,
			TotalDEKsChecked: 5,
			Timestamp:        time.Now(),
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/safety/check?threshold_percent=80", nil)
		w := httptest.NewRecorder()

		server.HandleSafetyCheck(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "key-1")
	})
}

func TestHandleSafetyReport(t *testing.T) {
	t.Run("service error on safety report", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("SafetyReport", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrInternal, "kms", "report failed")).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/safety/report", nil)
		w := httptest.NewRecorder()

		server.HandleSafetyReport(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with include_details parameter", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("SafetyReport", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrInternal, "kms", "report failed")).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/safety/report?include_details=true", nil)
		w := httptest.NewRecorder()

		server.HandleSafetyReport(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("successful safety report", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("SafetyReport", mock.Anything, mock.Anything).Return(&service.SafetyReportResponse{
			Items:               []service.SafetyCheckItem{},
			TotalOperations:     50000,
			TotalBytesProcessed: 1024 * 1024 * 100,
			ServerConfig: service.AEADServerConfig{
				Enforce:           true,
				MaxOperations:     1000000,
				MaxBytes:          1024 * 1024 * 1024,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			},
			Timestamp: time.Now(),
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/safety/report", nil)
		w := httptest.NewRecorder()

		server.HandleSafetyReport(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "total_operations")
	})

	t.Run("safety report with items", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("SafetyReport", mock.Anything, mock.MatchedBy(func(req *service.SafetyReportRequest) bool {
			return req.IncludeDetails
		})).Return(&service.SafetyReportResponse{
			Items: []service.SafetyCheckItem{
				{
					ID:             "kek-master",
					Type:           "key",
					OperationCount: 5000,
					BytesProcessed: 1024 * 1024,
					UsagePercent:   50.0,
					MaxOperations:  10000,
					MaxBytes:       1024 * 1024 * 1024,
				},
				{
					ID:             "dek-data-1",
					Type:           "dek",
					OperationCount: 9000,
					BytesProcessed: 900 * 1024 * 1024,
					UsagePercent:   90.0,
					MaxOperations:  10000,
					MaxBytes:       1024 * 1024 * 1024,
				},
			},
			TotalOperations:     14000,
			TotalBytesProcessed: 901 * 1024 * 1024,
			ServerConfig: service.AEADServerConfig{
				Enforce:           true,
				MaxOperations:     1000000,
				MaxBytes:          10 * 1024 * 1024 * 1024,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			},
			Timestamp: time.Now(),
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/safety/report?include_details=true", nil)
		w := httptest.NewRecorder()

		server.HandleSafetyReport(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "kek-master")
		assert.Contains(t, w.Body.String(), "dek-data-1")
	})
}

// =============================================================================
// Key Policy Handlers
// =============================================================================

func TestHandleSetKeyPolicy(t *testing.T) {
	t.Run("missing key name", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "")

		req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys//policy", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleSetKeyPolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")

		req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/test-key/policy", strings.NewReader("{invalid"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleSetKeyPolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service error on set policy", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("SetKeyPolicy", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrNotFound, "kms", "key not found")).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")

		body := `{"allowed_operations":["encrypt","decrypt"]}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/test-key/policy", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleSetKeyPolicy(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("successful set policy", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("SetKeyPolicy", mock.Anything, mock.MatchedBy(func(req *service.SetKeyPolicyRequest) bool {
			return req.KeyID == "test-key"
		})).Return(&service.SetKeyPolicyResponse{
			KeyID:         "test-key",
			PolicyVersion: 1,
			Timestamp:     time.Now(),
		}, nil).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")

		body := `{"allowed_operations":["encrypt","decrypt"],"rotation_required":true}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/test-key/policy", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleSetKeyPolicy(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "test-key")
	})
}

func TestHandleGetKeyPolicy(t *testing.T) {
	t.Run("missing key name", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys//policy", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleGetKeyPolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service error on get policy", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("GetKeyPolicy", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrNotFound, "kms", "key not found")).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys/test-key/policy", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleGetKeyPolicy(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("successful get policy with nil policy", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("GetKeyPolicy", mock.Anything, mock.Anything).Return(&service.GetKeyPolicyResponse{
			Policy:    nil,
			Timestamp: time.Now(),
		}, nil).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys/test-key/policy", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleGetKeyPolicy(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "timestamp")
	})

	t.Run("successful get policy with full policy", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		expiresAt := time.Now().Add(24 * time.Hour)
		maxUsage := uint64(10000)

		mockService.On("GetKeyPolicy", mock.Anything, mock.MatchedBy(func(req *service.GetKeyPolicyRequest) bool {
			return req.KeyID == "test-key"
		})).Return(&service.GetKeyPolicyResponse{
			Policy: &service.KeyPolicy{
				KeyID:             "test-key",
				Version:           2,
				AllowedOperations: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
				AllowedPrincipals: []string{"admin", "app-service"},
				DeniedPrincipals:  []string{"blocked-user"},
				ExpiresAt:         &expiresAt,
				RotationRequired:  true,
				MaxUsageCount:     &maxUsage,
				RequireMFA:        true,
				UpdatedAt:         time.Now(),
			},
			Timestamp: time.Now(),
		}, nil).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys/test-key/policy", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleGetKeyPolicy(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "test-key")
		assert.Contains(t, w.Body.String(), "allowed_operations")
		assert.Contains(t, w.Body.String(), "rotation_required")
	})

	t.Run("successful get policy without optional fields", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("GetKeyPolicy", mock.Anything, mock.Anything).Return(&service.GetKeyPolicyResponse{
			Policy: &service.KeyPolicy{
				KeyID:             "minimal-key",
				Version:           1,
				AllowedOperations: []service.KeyUsage{service.KeyUsageSign},
				ExpiresAt:         nil,
				MaxUsageCount:     nil,
				UpdatedAt:         time.Now(),
			},
			Timestamp: time.Now(),
		}, nil).Once()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "minimal-key")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys/minimal-key/policy", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleGetKeyPolicy(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "minimal-key")
	})
}
