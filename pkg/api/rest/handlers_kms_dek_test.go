//go:build rest

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/iotest"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestHandleCreateDEK_NilKMS tests HandleCreateDEK returns 500
// when the KMS service is nil.
func TestHandleCreateDEK_NilKMS(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true

	body, _ := json.Marshal(map[string]interface{}{
		"path":      "tenants/acme/data",
		"algorithm": "AES-256-GCM",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.HandleCreateDEK(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "KMS service not available")
}

// TestHandleCreateDEK_ReadBodyError tests HandleCreateDEK returns 400
// when reading the request body fails.
func TestHandleCreateDEK_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks", iotest.ErrReader(assert.AnError))
	w := httptest.NewRecorder()

	server.HandleCreateDEK(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleGetDEK_NilKMS tests HandleGetDEK returns 500
// when the KMS service is nil.
func TestHandleGetDEK_NilKMS(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "tenants/acme/data")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/deks/tenants/acme/data", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleGetDEK(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestHandleListDEKs_NilKMS tests HandleListDEKs returns 500
// when the KMS service is nil.
func TestHandleListDEKs_NilKMS(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/deks", nil)
	w := httptest.NewRecorder()

	server.HandleListDEKs(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestHandleRotateDEK_NilKMS tests HandleRotateDEK returns 500
// when the KMS service is nil.
func TestHandleRotateDEK_NilKMS(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "tenants/acme/data/rotate")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks/tenants/acme/data/rotate", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleRotateDEK(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestHandleRotateDEK_EmptyPathAfterStrip tests HandleRotateDEK returns 400
// when path is empty after stripping /rotate suffix.
func TestHandleRotateDEK_EmptyPathAfterStrip(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "/rotate")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks/rotate", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleRotateDEK(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleRotateDEK_ReadBodyError tests HandleRotateDEK returns 400
// when the request body read fails.
func TestHandleRotateDEK_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "tenants/acme/data/rotate")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks/tenants/acme/data/rotate",
		iotest.ErrReader(assert.AnError))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleRotateDEK(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleDeleteDEK_NilKMS tests HandleDeleteDEK returns 500
// when the KMS service is nil.
func TestHandleDeleteDEK_NilKMS(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "tenants/acme/data")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/deks/tenants/acme/data", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleDeleteDEK(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestHandleDeleteDEK_ReadBodyError tests HandleDeleteDEK returns 400
// when the request body read fails.
func TestHandleDeleteDEK_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "tenants/acme/data")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/deks/tenants/acme/data",
		iotest.ErrReader(assert.AnError))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleDeleteDEK(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleGetDEKStatus_NilKMS tests HandleGetDEKStatus returns 500
// when the KMS service is nil.
func TestHandleGetDEKStatus_NilKMS(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "tenants/acme/data/status")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/deks/tenants/acme/data/status", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleGetDEKStatus(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestHandleGetDEKStatus_EmptyPathAfterStrip tests HandleGetDEKStatus returns 400
// when path is empty after stripping /status suffix.
func TestHandleGetDEKStatus_EmptyPathAfterStrip(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "/status")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/deks/status", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleGetDEKStatus(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleSetDefaultDEK_NilKMS tests HandleSetDefaultDEK returns 500
// when the KMS service is nil.
func TestHandleSetDefaultDEK_NilKMS(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "tenants/acme/data/set-default")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks/tenants/acme/data/set-default", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleSetDefaultDEK(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestHandleSetDefaultDEK_EmptyPathAfterStrip tests HandleSetDefaultDEK returns 400
// when path is empty after stripping /set-default suffix.
func TestHandleSetDefaultDEK_EmptyPathAfterStrip(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "/set-default")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks/set-default", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleSetDefaultDEK(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleSetDefaultDEKFromBody_NilKMS tests HandleSetDefaultDEKFromBody returns 500
// when the KMS service is nil.
func TestHandleSetDefaultDEKFromBody_NilKMS(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true

	body, _ := json.Marshal(map[string]interface{}{
		"path": "tenants/acme/data",
	})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/deks/default", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.HandleSetDefaultDEKFromBody(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestHandleSetDefaultDEKFromBody_ReadBodyError tests HandleSetDefaultDEKFromBody returns 400
// when reading the request body fails.
func TestHandleSetDefaultDEKFromBody_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/deks/default",
		iotest.ErrReader(assert.AnError))
	w := httptest.NewRecorder()

	server.HandleSetDefaultDEKFromBody(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleSetDefaultDEKFromBody_EmptyPath tests HandleSetDefaultDEKFromBody returns 400
// when path is empty in the request body.
func TestHandleSetDefaultDEKFromBody_EmptyPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]interface{}{
		"path": "",
	})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/deks/default", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.HandleSetDefaultDEKFromBody(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleSetDefaultDEKFromBody_ServiceError tests HandleSetDefaultDEKFromBody returns
// an error when the service returns an error.
func TestHandleSetDefaultDEKFromBody_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("SetDefaultDEK", mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()

	body, _ := json.Marshal(map[string]interface{}{
		"path": "tenants/acme/data",
	})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/deks/default", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.HandleSetDefaultDEKFromBody(w, req)

	assert.True(t, w.Code >= 400)
}

// TestHandleSetDEKSafety_NilKMS tests HandleSetDEKSafety returns 500
// when the KMS service is nil.
func TestHandleSetDEKSafety_NilKMS(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "tenants/acme/data/safety")

	body, _ := json.Marshal(map[string]interface{}{
		"max_operations": 100000,
	})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/deks/tenants/acme/data/safety",
		bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleSetDEKSafety(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestHandleSetDEKSafety_EmptyPathAfterStrip tests HandleSetDEKSafety returns 400
// when path is empty after stripping /safety suffix.
func TestHandleSetDEKSafety_EmptyPathAfterStrip(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "/safety")

	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/deks/safety", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleSetDEKSafety(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleSetDEKSafety_ReadBodyError tests HandleSetDEKSafety returns 400
// when the request body read fails.
func TestHandleSetDEKSafety_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "tenants/acme/data/safety")

	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/deks/tenants/acme/data/safety",
		iotest.ErrReader(assert.AnError))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleSetDEKSafety(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleClearDEKSafety_NilKMS tests HandleClearDEKSafety returns 500
// when the KMS service is nil.
func TestHandleClearDEKSafety_NilKMS(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "tenants/acme/data/safety")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/deks/tenants/acme/data/safety", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleClearDEKSafety(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestHandleClearDEKSafety_EmptyPathAfterStrip tests HandleClearDEKSafety returns 400
// when path is empty after stripping /safety suffix.
func TestHandleClearDEKSafety_EmptyPathAfterStrip(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "/safety")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/deks/safety", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleClearDEKSafety(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleStartReencryption_NilKMS tests HandleStartReencryption returns 500
// when the KMS service is nil.
func TestHandleStartReencryption_NilKMS(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "tenants/acme/data/reencrypt")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks/tenants/acme/data/reencrypt", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleStartReencryption(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestHandleStartReencryption_EmptyPathAfterStrip tests HandleStartReencryption returns 400
// when path is empty after stripping /reencrypt suffix.
func TestHandleStartReencryption_EmptyPathAfterStrip(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "/reencrypt")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks/reencrypt", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleStartReencryption(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleStartReencryption_ReadBodyError tests HandleStartReencryption returns 400
// when the request body read fails.
func TestHandleStartReencryption_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "tenants/acme/data/reencrypt")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks/tenants/acme/data/reencrypt",
		iotest.ErrReader(assert.AnError))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleStartReencryption(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleGetReencryptionProgress_NilKMS tests HandleGetReencryptionProgress returns 500
// when the KMS service is nil.
func TestHandleGetReencryptionProgress_NilKMS(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "tenants/acme/data/reencrypt")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/deks/tenants/acme/data/reencrypt", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleGetReencryptionProgress(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestHandleGetReencryptionProgress_EmptyPathAfterStrip tests HandleGetReencryptionProgress
// returns 400 when path is empty after stripping /reencrypt suffix.
func TestHandleGetReencryptionProgress_EmptyPathAfterStrip(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "/reencrypt")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/deks/reencrypt", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleGetReencryptionProgress(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleGetReencryptionProgress_Success tests HandleGetReencryptionProgress
// with verbose query parameter.
func TestHandleGetReencryptionProgress_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetReencryptionProgress", mock.Anything, mock.Anything).Return(
		&service.GetReencryptionProgressResponse{
			Path:        "tenants/acme/data",
			OperationID: "op-1",
		}, nil).Once()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "tenants/acme/data/reencrypt")

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/kms/deks/tenants/acme/data/reencrypt?verbose=true&operation_id=op-1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleGetReencryptionProgress(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
