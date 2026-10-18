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
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// setupMockServerWithFuncMock creates a test server with function-based mock.
func setupMockServerWithFuncMock(t *testing.T) (*Server, *mocks.MockQuicBaaS, func()) {
	t.Helper()

	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockService := mocks.NewMockQuicBaaS()

	serverConfig := DefaultConfig()
	serverConfig.Address = ":0"

	server, err := NewServer(mockService, serverConfig, log)
	require.NoError(t, err)

	cleanup := func() {
		server.Close()
	}

	return server, mockService, cleanup
}

// =============================================================================
// PUT Handler Tests
// =============================================================================

func TestHandleKVPut_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.PutFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
		return &service.PutResponse{
			ID:        12345,
			Version:   1,
			Timestamp: time.Now().UnixNano(),
		}, nil
	}

	body := `{"value": "test-value"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kv/users/alice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("key", "alice")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleKVPut(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp KVPutResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "users", resp.EntityType)
	assert.Equal(t, "alice", resp.Key)
	assert.True(t, resp.Success)
}

func TestHandleKVPut_ServiceError_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.PutFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Put",
			Err:  stderrors.New("internal service error"),
		}
	}

	body := `{"value": "test-value"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kv/users/alice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("key", "alice")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleKVPut(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandleKVPut_MissingEntityType_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"value": "test-value"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kv//alice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "")
	rctx.URLParams.Add("key", "alice")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleKVPut(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleKVPut_MissingKey_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"value": "test-value"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kv/users/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("key", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleKVPut(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// =============================================================================
// GET Handler Tests
// =============================================================================

func TestHandleKVGet_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.GetFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
		wrapper := kvDataWrapper{
			Key:   "alice",
			Value: json.RawMessage(`{"name":"Alice","email":"alice@example.com"}`),
		}
		data, _ := json.Marshal(wrapper)
		return &service.GetResponse{
			Data:      data,
			Version:   1,
			Timestamp: time.Now().UnixNano(),
			Found:     true,
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kv/users/alice", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("key", "alice")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleKVGet(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp KVGetResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "users", resp.EntityType)
	assert.Equal(t, "alice", resp.Key)
	assert.True(t, resp.Found)
}

func TestHandleKVGet_NotFound_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.GetFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
		return &service.GetResponse{
			Found: false,
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kv/users/nonexistent", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("key", "nonexistent")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleKVGet(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleKVGet_InvalidConsistencyLevel_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kv/users/alice?consistency=invalid", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("key", "alice")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleKVGet(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// =============================================================================
// DELETE Handler Tests
// =============================================================================

func TestHandleKVDelete_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.DeleteFunc = func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
		return &service.DeleteResponse{
			Timestamp: time.Now().UnixNano(),
		}, nil
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kv/users/alice", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("key", "alice")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleKVDelete(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleKVDelete_ServiceError_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.DeleteFunc = func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
		return nil, &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "Delete",
			Err:  stderrors.New("not found"),
		}
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kv/users/alice", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("key", "alice")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleKVDelete(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// =============================================================================
// Batch Operation Handler Tests
// =============================================================================

func TestHandleBatchPut_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.BatchPutFunc = func(ctx context.Context, req *service.BatchPutRequest) (*service.BatchPutResponse, error) {
		responses := make([]*service.PutResponse, len(req.Requests))
		for i := range req.Requests {
			responses[i] = &service.PutResponse{
				ID:        uint64(i + 1),
				Version:   1,
				Timestamp: time.Now().UnixNano(),
			}
		}
		return &service.BatchPutResponse{
			Responses: responses,
			Timestamp: time.Now().UnixNano(),
		}, nil
	}

	body := BatchPutRequest{
		Operations: []BatchPutOperation{
			{EntityType: "users", ID: 1, Data: json.RawMessage(`{"name":"Alice"}`)},
			{EntityType: "users", ID: 2, Data: json.RawMessage(`{"name":"Bob"}`)},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/put", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleBatchPut(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp BatchPutResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Len(t, resp.Responses, 2)
}

func TestHandleBatchPut_EmptyOperations_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := BatchPutRequest{
		Operations: []BatchPutOperation{},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/put", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleBatchPut(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleBatchPut_InvalidJSON_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/put", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleBatchPut(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleBatchPut_MissingEntityType_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := BatchPutRequest{
		Operations: []BatchPutOperation{
			{EntityType: "", ID: 1, Data: json.RawMessage(`{"name":"Alice"}`)},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/put", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleBatchPut(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleBatchPut_MissingID_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := BatchPutRequest{
		Operations: []BatchPutOperation{
			{EntityType: "users", ID: 0, Data: json.RawMessage(`{"name":"Alice"}`)},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/put", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleBatchPut(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleBatchGet_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.BatchGetFunc = func(ctx context.Context, req *service.BatchGetRequest) (*service.BatchGetResponse, error) {
		responses := make([]*service.GetResponse, len(req.Requests))
		for i := range req.Requests {
			responses[i] = &service.GetResponse{
				Data:      []byte(`{"name":"Test"}`),
				Version:   1,
				Timestamp: time.Now().UnixNano(),
				Found:     true,
			}
		}
		return &service.BatchGetResponse{
			Responses: responses,
		}, nil
	}

	body := BatchGetRequest{
		Operations: []BatchGetOperation{
			{EntityType: "users", ID: 1},
			{EntityType: "users", ID: 2},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/get", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleBatchGet(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp BatchGetResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Len(t, resp.Responses, 2)
}

func TestHandleBatchGet_EmptyOperations_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := BatchGetRequest{
		Operations: []BatchGetOperation{},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/get", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleBatchGet(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleBatchGet_InvalidConsistency_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := BatchGetRequest{
		Operations:       []BatchGetOperation{{EntityType: "users", ID: 1}},
		ConsistencyLevel: "invalid",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/get", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleBatchGet(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleBatchDelete_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.BatchDeleteFunc = func(ctx context.Context, req *service.BatchDeleteRequest) (*service.BatchDeleteResponse, error) {
		responses := make([]*service.DeleteResponse, len(req.Requests))
		for i := range req.Requests {
			responses[i] = &service.DeleteResponse{
				Timestamp: time.Now().UnixNano(),
			}
		}
		return &service.BatchDeleteResponse{
			Responses: responses,
			Timestamp: time.Now().UnixNano(),
		}, nil
	}

	body := BatchDeleteRequest{
		Operations: []BatchDeleteOperation{
			{EntityType: "users", ID: 1},
			{EntityType: "users", ID: 2},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/delete", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleBatchDelete(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleBatchDelete_EmptyOperations_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := BatchDeleteRequest{
		Operations: []BatchDeleteOperation{},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/delete", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleBatchDelete(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// =============================================================================
// Query Handler Tests
// =============================================================================

func TestHandleQuery_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.QueryFunc = func(ctx context.Context, req *service.QueryRequest, stream chan<- *service.QueryResponse) error {
		stream <- &service.QueryResponse{
			Entities: []*service.Entity{
				{ID: 1, EntityType: "users", Data: []byte(`{"name":"Alice"}`)},
			},
			TotalCount: 1,
		}
		close(stream)
		return nil
	}

	// HandleQuery expects URL params, not JSON body
	req := httptest.NewRequest(http.MethodGet, "/api/v1/entities/users/query/name?value=Alice", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("index", "name")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleQuery(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleQuery_MissingValue_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	// HandleQuery requires a value query param
	req := httptest.NewRequest(http.MethodGet, "/api/v1/entities/users/query/name", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("index", "name")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleQuery(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleQuery_MissingEntityType_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/entities//query/name?value=Alice", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "")
	rctx.URLParams.Add("index", "name")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleQuery(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// =============================================================================
// Seal/Unseal Handler Tests
// =============================================================================

// =============================================================================
// KMS Handler Tests
// =============================================================================

func TestHandleKMSEncrypt_ServiceNotAvailable_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"plaintext": "dGVzdCBkYXRh"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/encrypt/test-key", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleKMSEncrypt(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandleKMSDecrypt_ServiceNotAvailable_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"ciphertext": "dGVzdCBkYXRh"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/decrypt/test-key", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleKMSDecrypt(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// =============================================================================
// Secrets Handler Tests
// =============================================================================

func TestHandleStoreSecret_ServiceNotAvailable_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"data": {"password": "secret123"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/myapp/db", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "myapp/db")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleStoreSecret(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandleStoreSecret_MissingPath_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"data": {"password": "secret123"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleStoreSecret(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleStoreSecret_MissingData_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/myapp/db", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "myapp/db")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleStoreSecret(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleStoreSecret_InvalidAlgorithm_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"data": {"password": "secret123"}, "algorithm": "INVALID-ALGO"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/myapp/db", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "myapp/db")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleStoreSecret(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleStoreSecret_InvalidJSON_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/myapp/db", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "myapp/db")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleStoreSecret(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleGetSecret_ServiceNotAvailable_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/myapp/db", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "myapp/db")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleGetSecret(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandleGetSecret_MissingPath_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleGetSecret(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleGetSecret_InvalidVersion_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/myapp/db?version=invalid", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "myapp/db")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleGetSecret(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleDeleteSecret_ServiceNotAvailable_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/secrets/myapp/db", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "myapp/db")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleDeleteSecret(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandleDeleteSecret_MissingPath_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/secrets/", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleDeleteSecret(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// =============================================================================
// Health Handler Tests
// =============================================================================

func TestHandleHealth_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.HealthFunc = func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
		return &service.HealthResponse{
			Status: service.HealthStatusHealthy,
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	server.HandleHealth(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleLiveness_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.LivenessFunc = func(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error) {
		return &service.LivenessResponse{
			Alive: true,
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/liveness", nil)
	rec := httptest.NewRecorder()
	server.HandleLiveness(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleReadiness_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.ReadinessFunc = func(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error) {
		return &service.ReadinessResponse{
			Ready: true,
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/readiness", nil)
	rec := httptest.NewRecorder()
	server.HandleReadiness(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleReadiness_NotReady_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.ReadinessFunc = func(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error) {
		return &service.ReadinessResponse{
			Ready:  false,
			Reason: "Database not connected",
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/readiness", nil)
	rec := httptest.NewRecorder()
	server.HandleReadiness(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// =============================================================================
// Cluster Handler Tests
// =============================================================================

func TestHandleClusterStatus_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.ClusterStatusFunc = func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
		return &service.ClusterStatusResponse{
			ClusterID: "test-cluster",
			Nodes: []*service.NodeInfo{
				{NodeID: 1, Address: "127.0.0.1:9000"},
			},
			Shards: []*service.ShardInfo{
				{ShardID: 1, LeaderID: 1},
			},
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/status", nil)
	rec := httptest.NewRecorder()
	server.HandleClusterStatus(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleGetLeader_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.GetLeaderFunc = func(ctx context.Context, req *service.GetLeaderRequest) (*service.GetLeaderResponse, error) {
		return &service.GetLeaderResponse{
			ShardID:  req.ShardID,
			LeaderID: 1,
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/shards/1/leader", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("shardID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleGetLeader(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleGetLeader_InvalidShardID_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/shards/invalid/leader", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("shardID", "invalid")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleGetLeader(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// =============================================================================
// Cache Handler Tests
// =============================================================================

func TestHandleGetCacheStats_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.GetCacheStatsFunc = func(ctx context.Context, req *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error) {
		return &service.GetCacheStatsResponse{
			Hits:    100,
			Misses:  20,
			Size:    50,
			HitRate: 0.83,
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cache/stats", nil)
	rec := httptest.NewRecorder()
	server.HandleGetCacheStats(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleClearCache_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.ClearCacheFunc = func(ctx context.Context, req *service.ClearCacheRequest) (*service.ClearCacheResponse, error) {
		return &service.ClearCacheResponse{
			EntriesCleared: 50,
		}, nil
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cache/clear", nil)
	rec := httptest.NewRecorder()
	server.HandleClearCache(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// =============================================================================
// Backup Handler Tests
// =============================================================================

func TestHandleCreateBackup_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.CreateBackupFunc = func(ctx context.Context, req *service.CreateBackupRequest) (*service.CreateBackupResponse, error) {
		return &service.CreateBackupResponse{
			BackupID: "backup-123",
			Size:     1024 * 1024,
		}, nil
	}

	body := `{"metadata": {"label": "daily"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backups", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleCreateBackup(rec, req)

	// CreateBackup returns 201 Created
	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestHandleListBackups_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.ListBackupsFunc = func(ctx context.Context, req *service.ListBackupsRequest) (*service.ListBackupsResponse, error) {
		return &service.ListBackupsResponse{
			Backups: []*service.BackupMetadata{
				{BackupID: "backup-1", Size: 1024},
				{BackupID: "backup-2", Size: 2048},
			},
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/backups", nil)
	rec := httptest.NewRecorder()
	server.HandleListBackups(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// =============================================================================
// Transaction Handler Tests
// =============================================================================

func TestHandleTransaction_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.TransactionFunc = func(ctx context.Context, req *service.TransactionRequest) (*service.TransactionResponse, error) {
		return &service.TransactionResponse{
			Succeeded: true,
		}, nil
	}

	// Use the correct transaction request format with entity_type, id, type, operator
	body := `{
		"compare": [{"entity_type": "users", "id": 1, "target": "version", "version": 1, "operator": "equal"}],
		"success": [{"type": "put", "entity_type": "users", "id": 1, "data": {"name": "alice"}}],
		"failure": []
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleTransaction(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleTransaction_InvalidJSON_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleTransaction(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// =============================================================================
// Service Error Mapping Tests
// =============================================================================

func TestHandleServiceError_NotFound_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.GetFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
		return nil, &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "Get",
			Err:  stderrors.New("not found"),
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kv/users/alice", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("key", "alice")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleKVGet(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleServiceError_Timeout_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.PutFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
		return nil, &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "Put",
			Err:  stderrors.New("timeout"),
		}
	}

	body := `{"value": "test-value"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kv/users/alice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("key", "alice")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleKVPut(rec, req)

	// Timeout maps to 408 Request Timeout
	assert.Equal(t, http.StatusRequestTimeout, rec.Code)
}

// =============================================================================
// List Entity Types Tests
// =============================================================================

func TestHandleListEntityTypes_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.ListEntityTypesFunc = func(ctx context.Context, req *service.ListEntityTypesRequest) (*service.ListEntityTypesResponse, error) {
		return &service.ListEntityTypesResponse{
			EntityTypes: []string{"users", "orders", "products"},
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/entities", nil)
	rec := httptest.NewRecorder()
	server.HandleListEntityTypes(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ListEntityTypesResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, []string{"users", "orders", "products"}, resp.EntityTypes)
}

// =============================================================================
// Error Response Format Tests
// =============================================================================

func TestErrorResponse_Format_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"operations": []}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/put", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleBatchPut(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var errResp ErrorResponse
	err := json.NewDecoder(rec.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.NotEmpty(t, errResp.Error)
}

// =============================================================================
// Secrets Action Routing Tests
// =============================================================================

func TestHandleSecretsAction_Rollback_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"version": 1}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/myapp/db?action=rollback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "myapp/db")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleSecretsAction(rec, req)

	// Service not available, but action routing should work
	assert.True(t, rec.Code >= 400)
}

func TestHandleSecretsAction_Rotate_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/myapp/db?action=rotate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "myapp/db")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleSecretsAction(rec, req)

	// Service not available, but action routing should work
	assert.True(t, rec.Code >= 400)
}

// =============================================================================
// Secrets GetOrVersions Routing Tests
// =============================================================================

func TestHandleGetSecretOrVersions_ListVersions_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/myapp/db?versions=true", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "myapp/db")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleGetSecretOrVersions(rec, req)

	// Service not available, but routing should work
	assert.True(t, rec.Code >= 400)
}

// =============================================================================
// Concurrent Request Tests
// =============================================================================

func TestConcurrentRequests_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.GetFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
		wrapper := kvDataWrapper{
			Key:   "test",
			Value: json.RawMessage(`{"data":"test"}`),
		}
		data, _ := json.Marshal(wrapper)
		return &service.GetResponse{
			Data:      data,
			Found:     true,
			Timestamp: time.Now().UnixNano(),
		}, nil
	}

	done := make(chan struct{})
	numRequests := 10

	for i := 0; i < numRequests; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/kv/users/test", nil)

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("type", "users")
			rctx.URLParams.Add("key", "test")
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			rec := httptest.NewRecorder()
			server.HandleKVGet(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			done <- struct{}{}
		}()
	}

	for i := 0; i < numRequests; i++ {
		<-done
	}
}

// =============================================================================
// URL Encoding Tests
// =============================================================================

func TestHandleKVPut_URLEncodedKey_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.PutFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
		return &service.PutResponse{
			ID:        12345,
			Version:   1,
			Timestamp: time.Now().UnixNano(),
		}, nil
	}

	body := `{"value": "test-value"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kv/users/alice%40example.com", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("key", "alice%40example.com")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleKVPut(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp KVPutResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", resp.Key)
}

// =============================================================================
// Raw Body Value Tests
// =============================================================================

func TestHandleKVPut_RawBodyValue_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.PutFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
		return &service.PutResponse{
			ID:        12345,
			Version:   1,
			Timestamp: time.Now().UnixNano(),
		}, nil
	}

	// Send raw JSON value without wrapping in value field
	body := `{"name":"Alice","email":"alice@example.com"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kv/users/alice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("key", "alice")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleKVPut(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// =============================================================================
// Restore and Delete Backup Handler Tests
// =============================================================================

func TestHandleRestoreBackup_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.RestoreBackupFunc = func(ctx context.Context, req *service.RestoreBackupRequest) (*service.RestoreBackupResponse, error) {
		return &service.RestoreBackupResponse{
			JobID:    "restore-job-123",
			Status:   "completed",
			Progress: 100,
		}, nil
	}

	body := `{"target_shards": []}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backups/backup-123/restore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Add chi.RouteContext with "id" URL param
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "backup-123")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleRestoreBackup(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleRestoreBackup_MissingBackupID_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"target_shards": []}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backups//restore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Add chi.RouteContext with empty "id" URL param
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleRestoreBackup(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleDeleteBackup_Success_WithFuncMock(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.DeleteBackupFunc = func(ctx context.Context, req *service.DeleteBackupRequest) (*service.DeleteBackupResponse, error) {
		return &service.DeleteBackupResponse{
			Success: true,
			Message: "backup deleted successfully",
		}, nil
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/backups/backup-123", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "backup-123")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleDeleteBackup(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleDeleteBackup_MissingBackupID_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/backups/", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleDeleteBackup(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// =============================================================================
// Range Query Handler Tests
// =============================================================================

func TestHandleRangeQuery_InvalidJSON_WithFuncMock(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/range", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleRangeQuery(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
