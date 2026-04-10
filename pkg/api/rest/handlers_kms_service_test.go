//go:build rest

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestHandleGenerateKey_ServicePath tests HandleGenerateKey through the
// KMS service path (the primary code path) rather than the KMS manager fallback.
func TestHandleGenerateKey_ServicePath(t *testing.T) {
	now := time.Now()

	t.Run("success via service path", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("GenerateKey", mock.Anything, mock.Anything).Return(&service.GenerateKeyResponse{
			Key: &service.KeyHandle{
				KeyID:     "key-123",
				Algorithm: "AES-256-GCM",
				Status:    "enabled",
				CreatedAt: now,
			},
			Metadata: &service.KeyMetadata{
				KeyID:     "key-123",
				Algorithm: "AES-256-GCM",
				Status:    "enabled",
			},
			Timestamp: now,
		}, nil).Once()

		body, _ := json.Marshal(map[string]interface{}{
			"name":        "my-key",
			"description": "Test key",
			"key_attributes": map[string]interface{}{
				"algorithm": "AES-256-GCM",
				"key_usage": []string{"encrypt", "decrypt"},
			},
		})

		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys", bytes.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleGenerateKey(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("service error via service path", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("GenerateKey", mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()

		body, _ := json.Marshal(map[string]interface{}{
			"name": "my-key",
			"key_attributes": map[string]interface{}{
				"algorithm": "AES-256-GCM",
				"key_usage": []string{"encrypt"},
			},
		})

		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys", bytes.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleGenerateKey(w, req)

		assert.True(t, w.Code >= 400)
	})
}

// TestHandleImportKey_ServicePath tests HandleImportKey with custom name,
// algorithm attributes, and nil KMS manager.
func TestHandleImportKey_ServicePath(t *testing.T) {
	t.Run("with custom name and algorithm", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.DisableKMS = true
		mockKMS := &MockKMSManager{}
		mockKMS.On("ImportKey", mock.Anything, "my-import-key", mock.Anything, "RSA-4096").Return(
			&kms.KeyMetadata{
				KeyID:     "my-import-key",
				Algorithm: "RSA-4096",
				Version:   1,
				State:     kms.KeyStateEnabled,
				Created:   time.Now(),
				Updated:   time.Now(),
			}, nil).Once()
		mockService.On("KMSManager").Return(mockKMS)

		body, _ := json.Marshal(map[string]interface{}{
			"name":            "my-import-key",
			"key_material":    []byte("encrypted-key"),
			"wrapping_key_id": "wrapping-123",
			"key_attributes": map[string]interface{}{
				"algorithm": "RSA-4096",
			},
		})

		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/import", bytes.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleImportKey(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("nil KMS manager", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.DisableKMS = true
		mockService.On("KMSManager").Return(nil)

		body, _ := json.Marshal(map[string]interface{}{
			"key_material":    []byte("encrypted-key"),
			"wrapping_key_id": "wrapping-123",
		})

		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/import", bytes.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleImportKey(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// TestHandleExportKey_Success tests HandleExportKey success path.
func TestHandleExportKey_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ExportKey", mock.Anything, mock.Anything).Return(&service.ExportKeyResponse{
		KeyID:             "key-123",
		Version:           1,
		WrappedKey:        []byte("wrapped"),
		WrappingKeyID:     "wrapping-123",
		WrappingAlgorithm: "RSA-OAEP",
		Format:            "raw",
		Timestamp:         time.Now(),
	}, nil).Once()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "key-123")

	body, _ := json.Marshal(map[string]interface{}{
		"wrapping_key_id": "wrapping-123",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/key-123/export", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleExportKey(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestHandleExportKey_NilKMS tests HandleExportKey when KMS service is nil.
func TestHandleExportKey_NilKMS(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "key-123")

	body, _ := json.Marshal(map[string]interface{}{
		"wrapping_key_id": "wrapping-123",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/key-123/export", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleExportKey(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestHandleUnwrapKey_Success tests HandleUnwrapKey success path.
func TestHandleUnwrapKey_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	now := time.Now()
	mockService.On("UnwrapKey", mock.Anything, mock.Anything).Return(&service.UnwrapKeyResponse{
		KeyID:     "unwrapped-key",
		Version:   1,
		Algorithm: "AES-256-GCM",
		Status:    "enabled",
		Key: &service.KeyHandle{
			KeyID:     "unwrapped-key",
			Version:   1,
			Algorithm: "AES-256-GCM",
			Status:    "enabled",
			CreatedAt: now,
		},
		Timestamp: now,
	}, nil).Once()

	body, _ := json.Marshal(map[string]interface{}{
		"wrapped_key":     []byte("wrapped-data"),
		"wrapping_key_id": "wrapping-123",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/unwrap", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.HandleUnwrapKey(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), "unwrapped-key")
}

// TestHandleUnwrapKey_NilKMS tests HandleUnwrapKey when KMS service is nil.
func TestHandleUnwrapKey_NilKMS(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true

	body, _ := json.Marshal(map[string]interface{}{
		"wrapped_key":     []byte("wrapped-data"),
		"wrapping_key_id": "wrapping-123",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/unwrap", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.HandleUnwrapKey(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestHandleSetKeySafety_Success tests HandleSetKeySafety success path.
func TestHandleSetKeySafety_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("SetKeySafety", mock.Anything, mock.Anything).Return(&service.SetKeySafetyResponse{
		KeyID: "key-123",
		Config: service.AEADSafetyConfigDTO{
			MaxOperations:     100000,
			MaxBytes:          1073741824,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		},
		Timestamp: time.Now(),
	}, nil).Once()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "key-123")

	body, _ := json.Marshal(map[string]interface{}{
		"max_operations":      100000,
		"max_bytes":           1073741824,
		"warning_percent":     80,
		"auto_rotate_percent": 95,
	})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/key-123/safety", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleSetKeySafety(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestHandleClearKeySafety_Success tests HandleClearKeySafety success path.
func TestHandleClearKeySafety_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ClearKeySafety", mock.Anything, mock.Anything).Return(&service.ClearKeySafetyResponse{
		KeyID:     "key-123",
		Timestamp: time.Now(),
	}, nil).Once()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "key-123")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/keys/key-123/safety", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleClearKeySafety(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestHandleSafetyCheck_Success tests HandleSafetyCheck success path
// with threshold_percent query parameter.
func TestHandleSafetyCheck_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("SafetyCheck", mock.Anything, mock.Anything).Return(&service.SafetyCheckResponse{
		Items: []service.SafetyCheckItem{
			{
				ID:             "key-1",
				Type:           "key",
				OperationCount: 500,
				BytesProcessed: 1024,
				UsagePercent:   50.0,
				MaxOperations:  1000,
				MaxBytes:       2048,
			},
		},
		ThresholdPercent: 80,
		TotalKeysChecked: 5,
		TotalDEKsChecked: 3,
		Timestamp:        time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/safety/check?threshold_percent=80", nil)
	w := httptest.NewRecorder()

	server.HandleSafetyCheck(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "key-1")
}

// TestHandleSafetyReport_Success tests HandleSafetyReport success path
// with include_details query parameter.
func TestHandleSafetyReport_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("SafetyReport", mock.Anything, mock.Anything).Return(&service.SafetyReportResponse{
		Items:               []service.SafetyCheckItem{},
		TotalOperations:     1000,
		TotalBytesProcessed: 4096,
		ServerConfig: service.AEADServerConfig{
			Enforce:           true,
			MaxOperations:     100000,
			MaxBytes:          1073741824,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		},
		Timestamp: time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/safety/report?include_details=true", nil)
	w := httptest.NewRecorder()

	server.HandleSafetyReport(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestHandleSetKeyPolicy_Success tests HandleSetKeyPolicy success path.
func TestHandleSetKeyPolicy_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("SetKeyPolicy", mock.Anything, mock.Anything).Return(&service.SetKeyPolicyResponse{
		KeyID:         "key-123",
		PolicyVersion: 2,
		Timestamp:     time.Now(),
	}, nil).Once()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "key-123")

	body, _ := json.Marshal(map[string]interface{}{
		"allowed_operations": []string{"encrypt", "decrypt"},
	})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/key-123/policy", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleSetKeyPolicy(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestHandleGetKeyPolicy_NilKMS tests HandleGetKeyPolicy when KMS service is nil.
func TestHandleGetKeyPolicy_NilKMS(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "key-123")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys/key-123/policy", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleGetKeyPolicy(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestHandleKMSEncrypt_ServicePath tests HandleKMSEncrypt success via the service path.
func TestHandleKMSEncrypt_ServicePath(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	require.NotNil(t, server)

	mockService.On("Encrypt", mock.Anything, mock.Anything).Return(&service.EncryptResponse{
		KeyID:      "key-123",
		Ciphertext: []byte("encrypted-data"),
		KeyVersion: 1,
	}, nil).Once()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "key-123")

	body, _ := json.Marshal(map[string]interface{}{
		"plaintext": []byte("secret data"),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/encrypt/key-123", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleKMSEncrypt(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestHandleKMSDecrypt_ServicePath tests HandleKMSDecrypt success via the service path.
func TestHandleKMSDecrypt_ServicePath(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("Decrypt", mock.Anything, mock.Anything).Return(&service.DecryptResponse{
		KeyID:     "key-123",
		Plaintext: []byte("decrypted-data"),
	}, nil).Once()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "key-123")

	body, _ := json.Marshal(map[string]interface{}{
		"ciphertext": []byte("encrypted-data"),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/decrypt/key-123", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleKMSDecrypt(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestHandleUpdateKeyStatus_DisableKey tests HandleUpdateKeyStatus with a disable
// request, exercising the KMSManager().DisableKey() code path.
func TestHandleUpdateKeyStatus_DisableKey(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockKMS := &MockKMSManager{}
	mockKMS.On("DisableKey", mock.Anything, "key-123").Return(nil).Once()
	mockService.On("KMSManager").Return(mockKMS)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "key-123")

	body, _ := json.Marshal(map[string]interface{}{
		"status": "disabled",
	})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/key-123/status", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleUpdateKeyStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "disabled")
	mockKMS.AssertExpectations(t)
}

// TestHandleUpdateKeyStatus_EnableKey tests HandleUpdateKeyStatus with an enable
// request, exercising the KMSManager().EnableKey() code path.
func TestHandleUpdateKeyStatus_EnableKey(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockKMS := &MockKMSManager{}
	mockKMS.On("EnableKey", mock.Anything, "key-456").Return(nil).Once()
	mockService.On("KMSManager").Return(mockKMS)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "key-456")

	body, _ := json.Marshal(map[string]interface{}{
		"status": "enabled",
	})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/key-456/status", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleUpdateKeyStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "enabled")
	mockKMS.AssertExpectations(t)
}

// TestHandleUpdateKeyStatus_InvalidStatus tests HandleUpdateKeyStatus with an
// invalid status value, verifying proper validation.
func TestHandleUpdateKeyStatus_InvalidStatus(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "key-123")

	body, _ := json.Marshal(map[string]interface{}{
		"status": "suspended",
	})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/key-123/status", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleUpdateKeyStatus(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "unsupported status")
}
