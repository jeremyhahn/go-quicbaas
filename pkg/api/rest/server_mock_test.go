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
// KV Handler Tests
// =============================================================================

// TestHandleKV_WithFuncMock drives the KV put, get and delete handlers with
// one row per verb and case: the request, the mock's return and the status.
func TestHandleKV_WithFuncMock(t *testing.T) {
	putOK := func(t *testing.T, m *mocks.MockQuicBaaS) {
		m.PutFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
			return &service.PutResponse{ID: 12345, Version: 1, Timestamp: time.Now().UnixNano()}, nil
		}
	}
	putKey := func(wantKey string) func(t *testing.T, rec *httptest.ResponseRecorder) {
		return func(t *testing.T, rec *httptest.ResponseRecorder) {
			var resp KVPutResponse
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
			assert.Equal(t, "users", resp.EntityType)
			assert.Equal(t, wantKey, resp.Key)
			assert.True(t, resp.Success)
		}
	}

	tests := []struct {
		name       string
		method     string
		entityType string
		key        string
		query      string
		body       string
		setup      func(t *testing.T, m *mocks.MockQuicBaaS)
		wantStatus int
		check      func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name:   "put success",
			method: http.MethodPut, entityType: "users", key: "alice",
			body:       `{"value": "test-value"}`,
			setup:      putOK,
			wantStatus: http.StatusOK,
			check:      putKey("alice"),
		},
		{
			name:   "put URL-encoded key is decoded",
			method: http.MethodPut, entityType: "users", key: "alice%40example.com",
			body:       `{"value": "test-value"}`,
			setup:      putOK,
			wantStatus: http.StatusOK,
			check:      putKey("alice@example.com"),
		},
		{
			name:   "put raw JSON body without value field",
			method: http.MethodPut, entityType: "users", key: "alice",
			body:       `{"name":"Alice","email":"alice@example.com"}`,
			setup:      putOK,
			wantStatus: http.StatusOK,
		},
		{
			name:   "put service error",
			method: http.MethodPut, entityType: "users", key: "alice",
			body: `{"value": "test-value"}`,
			setup: func(t *testing.T, m *mocks.MockQuicBaaS) {
				m.PutFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
					return nil, &errors.QBError{Code: errors.ErrInternal, Op: "Put", Err: stderrors.New("internal service error")}
				}
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:   "put service timeout maps to 408",
			method: http.MethodPut, entityType: "users", key: "alice",
			body: `{"value": "test-value"}`,
			setup: func(t *testing.T, m *mocks.MockQuicBaaS) {
				m.PutFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
					return nil, &errors.QBError{Code: errors.ErrTimeout, Op: "Put", Err: stderrors.New("timeout")}
				}
			},
			wantStatus: http.StatusRequestTimeout,
		},
		{
			name:   "put missing entity type",
			method: http.MethodPut, entityType: "", key: "alice",
			body:       `{"value": "test-value"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:   "put missing key",
			method: http.MethodPut, entityType: "users", key: "",
			body:       `{"value": "test-value"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:   "get success",
			method: http.MethodGet, entityType: "users", key: "alice",
			setup: func(t *testing.T, m *mocks.MockQuicBaaS) {
				data, err := json.Marshal(kvDataWrapper{
					Key:   "alice",
					Value: json.RawMessage(`{"name":"Alice","email":"alice@example.com"}`),
				})
				require.NoError(t, err)
				m.GetFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
					return &service.GetResponse{Data: data, Version: 1, Timestamp: time.Now().UnixNano(), Found: true}, nil
				}
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp KVGetResponse
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
				assert.Equal(t, "users", resp.EntityType)
				assert.Equal(t, "alice", resp.Key)
				assert.True(t, resp.Found)
			},
		},
		{
			name:   "get not found",
			method: http.MethodGet, entityType: "users", key: "nonexistent",
			setup: func(t *testing.T, m *mocks.MockQuicBaaS) {
				m.GetFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
					return &service.GetResponse{Found: false}, nil
				}
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:   "get service not found error",
			method: http.MethodGet, entityType: "users", key: "alice",
			setup: func(t *testing.T, m *mocks.MockQuicBaaS) {
				m.GetFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
					return nil, &errors.QBError{Code: errors.ErrNotFound, Op: "Get", Err: stderrors.New("not found")}
				}
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:   "get invalid consistency level",
			method: http.MethodGet, entityType: "users", key: "alice",
			query:      "?consistency=invalid",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:   "delete success",
			method: http.MethodDelete, entityType: "users", key: "alice",
			setup: func(t *testing.T, m *mocks.MockQuicBaaS) {
				m.DeleteFunc = func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
					return &service.DeleteResponse{Timestamp: time.Now().UnixNano()}, nil
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:   "delete service not found error",
			method: http.MethodDelete, entityType: "users", key: "alice",
			setup: func(t *testing.T, m *mocks.MockQuicBaaS) {
				m.DeleteFunc = func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
					return nil, &errors.QBError{Code: errors.ErrNotFound, Op: "Delete", Err: stderrors.New("not found")}
				}
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServerWithFuncMock(t)
			defer cleanup()
			if tt.setup != nil {
				tt.setup(t, mockService)
			}
			handlers := map[string]http.HandlerFunc{
				http.MethodPut:    server.HandleKVPut,
				http.MethodGet:    server.HandleKVGet,
				http.MethodDelete: server.HandleKVDelete,
			}

			target := "/api/v1/kv/" + tt.entityType + "/" + tt.key + tt.query
			req := httptest.NewRequest(tt.method, target, strings.NewReader(tt.body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("type", tt.entityType)
			rctx.URLParams.Add("key", tt.key)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			rec := httptest.NewRecorder()
			handlers[tt.method](rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.check != nil {
				tt.check(t, rec)
			}
		})
	}
}

// =============================================================================
// Batch Operation Handler Tests
// =============================================================================

// TestHandleBatch_WithFuncMock drives the batch put, get and delete handlers
// with one row per verb and case. A string body is sent as-is; any other body
// is marshalled to JSON.
func TestHandleBatch_WithFuncMock(t *testing.T) {
	type handlerFor func(s *Server) http.HandlerFunc
	batchPut := func(s *Server) http.HandlerFunc { return s.HandleBatchPut }
	batchGet := func(s *Server) http.HandlerFunc { return s.HandleBatchGet }
	batchDelete := func(s *Server) http.HandlerFunc { return s.HandleBatchDelete }

	tests := []struct {
		name       string
		handler    handlerFor
		body       any
		setup      func(m *mocks.MockQuicBaaS)
		wantStatus int
		check      func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name:    "put success",
			handler: batchPut,
			body: BatchPutRequest{Operations: []BatchPutOperation{
				{EntityType: "users", ID: 1, Data: json.RawMessage(`{"name":"Alice"}`)},
				{EntityType: "users", ID: 2, Data: json.RawMessage(`{"name":"Bob"}`)},
			}},
			setup: func(m *mocks.MockQuicBaaS) {
				m.BatchPutFunc = func(ctx context.Context, req *service.BatchPutRequest) (*service.BatchPutResponse, error) {
					responses := make([]*service.PutResponse, len(req.Requests))
					for i := range req.Requests {
						responses[i] = &service.PutResponse{ID: uint64(i + 1), Version: 1, Timestamp: time.Now().UnixNano()}
					}
					return &service.BatchPutResponse{Responses: responses, Timestamp: time.Now().UnixNano()}, nil
				}
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp BatchPutResponse
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
				assert.True(t, resp.Success)
				assert.Len(t, resp.Responses, 2)
			},
		},
		{
			name:       "put empty operations returns an error body",
			handler:    batchPut,
			body:       BatchPutRequest{Operations: []BatchPutOperation{}},
			wantStatus: http.StatusBadRequest,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
				assert.NotEmpty(t, errResp.Error)
			},
		},
		{
			name:       "put invalid JSON",
			handler:    batchPut,
			body:       "not-json",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "put missing entity type",
			handler: batchPut,
			body: BatchPutRequest{Operations: []BatchPutOperation{
				{EntityType: "", ID: 1, Data: json.RawMessage(`{"name":"Alice"}`)},
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "put missing ID",
			handler: batchPut,
			body: BatchPutRequest{Operations: []BatchPutOperation{
				{EntityType: "users", ID: 0, Data: json.RawMessage(`{"name":"Alice"}`)},
			}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "get success",
			handler: batchGet,
			body: BatchGetRequest{Operations: []BatchGetOperation{
				{EntityType: "users", ID: 1},
				{EntityType: "users", ID: 2},
			}},
			setup: func(m *mocks.MockQuicBaaS) {
				m.BatchGetFunc = func(ctx context.Context, req *service.BatchGetRequest) (*service.BatchGetResponse, error) {
					responses := make([]*service.GetResponse, len(req.Requests))
					for i := range req.Requests {
						responses[i] = &service.GetResponse{Data: []byte(`{"name":"Test"}`), Version: 1, Timestamp: time.Now().UnixNano(), Found: true}
					}
					return &service.BatchGetResponse{Responses: responses}, nil
				}
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp BatchGetResponse
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
				assert.True(t, resp.Success)
				assert.Len(t, resp.Responses, 2)
			},
		},
		{
			name:       "get empty operations",
			handler:    batchGet,
			body:       BatchGetRequest{Operations: []BatchGetOperation{}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "get invalid consistency level",
			handler: batchGet,
			body: BatchGetRequest{
				Operations:       []BatchGetOperation{{EntityType: "users", ID: 1}},
				ConsistencyLevel: "invalid",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "delete success",
			handler: batchDelete,
			body: BatchDeleteRequest{Operations: []BatchDeleteOperation{
				{EntityType: "users", ID: 1},
				{EntityType: "users", ID: 2},
			}},
			setup: func(m *mocks.MockQuicBaaS) {
				m.BatchDeleteFunc = func(ctx context.Context, req *service.BatchDeleteRequest) (*service.BatchDeleteResponse, error) {
					responses := make([]*service.DeleteResponse, len(req.Requests))
					for i := range req.Requests {
						responses[i] = &service.DeleteResponse{Timestamp: time.Now().UnixNano()}
					}
					return &service.BatchDeleteResponse{Responses: responses, Timestamp: time.Now().UnixNano()}, nil
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "delete empty operations",
			handler:    batchDelete,
			body:       BatchDeleteRequest{Operations: []BatchDeleteOperation{}},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServerWithFuncMock(t)
			defer cleanup()
			if tt.setup != nil {
				tt.setup(mockService)
			}

			bodyBytes, isRaw := tt.body.(string)
			if !isRaw {
				encoded, err := json.Marshal(tt.body)
				require.NoError(t, err)
				bodyBytes = string(encoded)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/batch", strings.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			rec := httptest.NewRecorder()
			tt.handler(server)(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.check != nil {
				tt.check(t, rec)
			}
		})
	}
}

// =============================================================================
// Query Handler Tests
// =============================================================================

// TestHandleQuery_WithFuncMock drives the index query handler with one row
// per case: the URL params, the mock's stream and the status.
func TestHandleQuery_WithFuncMock(t *testing.T) {
	tests := []struct {
		name       string
		entityType string
		query      string
		setup      func(m *mocks.MockQuicBaaS)
		wantStatus int
	}{
		{
			name:       "success",
			entityType: "users",
			query:      "?value=Alice",
			setup: func(m *mocks.MockQuicBaaS) {
				m.QueryFunc = func(ctx context.Context, req *service.QueryRequest, stream chan<- *service.QueryResponse) error {
					stream <- &service.QueryResponse{
						Entities:   []*service.Entity{{ID: 1, EntityType: "users", Data: []byte(`{"name":"Alice"}`)}},
						TotalCount: 1,
					}
					close(stream)
					return nil
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing value query param",
			entityType: "users",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing entity type",
			entityType: "",
			query:      "?value=Alice",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServerWithFuncMock(t)
			defer cleanup()
			if tt.setup != nil {
				tt.setup(mockService)
			}

			// HandleQuery reads the value from the query string, not a JSON body.
			req := httptest.NewRequest(http.MethodGet, "/api/v1/entities/"+tt.entityType+"/query/name"+tt.query, nil)
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("type", tt.entityType)
			rctx.URLParams.Add("index", "name")
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			rec := httptest.NewRecorder()
			server.HandleQuery(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

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

// TestHandleSecrets_WithFuncMock drives the store, get and delete secret
// handlers with one row per verb and case. The mock has no secrets service,
// so a well-formed request reaches the service-not-available path.
func TestHandleSecrets_WithFuncMock(t *testing.T) {
	storeSecret := func(s *Server) http.HandlerFunc { return s.HandleStoreSecret }
	getSecret := func(s *Server) http.HandlerFunc { return s.HandleGetSecret }
	deleteSecret := func(s *Server) http.HandlerFunc { return s.HandleDeleteSecret }

	tests := []struct {
		name       string
		handler    func(s *Server) http.HandlerFunc
		method     string
		path       string
		query      string
		body       string
		wantStatus int
	}{
		{
			name:    "store service not available",
			handler: storeSecret, method: http.MethodPost, path: "myapp/db",
			body:       `{"data": {"password": "secret123"}}`,
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:    "store missing path",
			handler: storeSecret, method: http.MethodPost, path: "",
			body:       `{"data": {"password": "secret123"}}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "store missing data",
			handler: storeSecret, method: http.MethodPost, path: "myapp/db",
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "store invalid algorithm",
			handler: storeSecret, method: http.MethodPost, path: "myapp/db",
			body:       `{"data": {"password": "secret123"}, "algorithm": "INVALID-ALGO"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "store invalid JSON",
			handler: storeSecret, method: http.MethodPost, path: "myapp/db",
			body:       "not-json",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "get service not available",
			handler: getSecret, method: http.MethodGet, path: "myapp/db",
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:    "get missing path",
			handler: getSecret, method: http.MethodGet, path: "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "get invalid version",
			handler: getSecret, method: http.MethodGet, path: "myapp/db",
			query:      "?version=invalid",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "delete service not available",
			handler: deleteSecret, method: http.MethodDelete, path: "myapp/db",
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:    "delete missing path",
			handler: deleteSecret, method: http.MethodDelete, path: "",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _, cleanup := setupMockServerWithFuncMock(t)
			defer cleanup()

			req := httptest.NewRequest(tt.method, "/api/v1/secrets/"+tt.path+tt.query, strings.NewReader(tt.body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.path)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			rec := httptest.NewRecorder()
			tt.handler(server)(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

// =============================================================================
// Health, Cluster and Cache Handler Tests
// =============================================================================

// TestHandleOperational_WithFuncMock drives the health, cluster and cache
// handlers with one row per handler and case: the request, the mock's return
// and the status.
func TestHandleOperational_WithFuncMock(t *testing.T) {
	tests := []struct {
		name       string
		handler    func(s *Server) http.HandlerFunc
		method     string
		target     string
		shardID    string
		setup      func(m *mocks.MockQuicBaaS)
		wantStatus int
	}{
		{
			name:    "health healthy",
			handler: func(s *Server) http.HandlerFunc { return s.HandleHealth },
			method:  http.MethodGet, target: "/api/v1/health",
			setup: func(m *mocks.MockQuicBaaS) {
				m.HealthFunc = func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
					return &service.HealthResponse{Status: service.HealthStatusHealthy}, nil
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:    "liveness alive",
			handler: func(s *Server) http.HandlerFunc { return s.HandleLiveness },
			method:  http.MethodGet, target: "/api/v1/health/liveness",
			setup: func(m *mocks.MockQuicBaaS) {
				m.LivenessFunc = func(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error) {
					return &service.LivenessResponse{Alive: true}, nil
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:    "readiness ready",
			handler: func(s *Server) http.HandlerFunc { return s.HandleReadiness },
			method:  http.MethodGet, target: "/api/v1/health/readiness",
			setup: func(m *mocks.MockQuicBaaS) {
				m.ReadinessFunc = func(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error) {
					return &service.ReadinessResponse{Ready: true}, nil
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:    "readiness not ready",
			handler: func(s *Server) http.HandlerFunc { return s.HandleReadiness },
			method:  http.MethodGet, target: "/api/v1/health/readiness",
			setup: func(m *mocks.MockQuicBaaS) {
				m.ReadinessFunc = func(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error) {
					return &service.ReadinessResponse{Ready: false, Reason: "Database not connected"}, nil
				}
			},
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:    "cluster status",
			handler: func(s *Server) http.HandlerFunc { return s.HandleClusterStatus },
			method:  http.MethodGet, target: "/api/v1/cluster/status",
			setup: func(m *mocks.MockQuicBaaS) {
				m.ClusterStatusFunc = func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
					return &service.ClusterStatusResponse{
						ClusterID: "test-cluster",
						Nodes:     []*service.NodeInfo{{NodeID: 1, Address: "127.0.0.1:9000"}},
						Shards:    []*service.ShardInfo{{ShardID: 1, LeaderID: 1}},
					}, nil
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:    "get leader",
			handler: func(s *Server) http.HandlerFunc { return s.HandleGetLeader },
			method:  http.MethodGet, target: "/api/v1/cluster/shards/1/leader", shardID: "1",
			setup: func(m *mocks.MockQuicBaaS) {
				m.GetLeaderFunc = func(ctx context.Context, req *service.GetLeaderRequest) (*service.GetLeaderResponse, error) {
					return &service.GetLeaderResponse{ShardID: req.ShardID, LeaderID: 1}, nil
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:    "get leader invalid shard ID",
			handler: func(s *Server) http.HandlerFunc { return s.HandleGetLeader },
			method:  http.MethodGet, target: "/api/v1/cluster/shards/invalid/leader", shardID: "invalid",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "cache stats",
			handler: func(s *Server) http.HandlerFunc { return s.HandleGetCacheStats },
			method:  http.MethodGet, target: "/api/v1/cache/stats",
			setup: func(m *mocks.MockQuicBaaS) {
				m.GetCacheStatsFunc = func(ctx context.Context, req *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error) {
					return &service.GetCacheStatsResponse{Hits: 100, Misses: 20, Size: 50, HitRate: 0.83}, nil
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:    "clear cache",
			handler: func(s *Server) http.HandlerFunc { return s.HandleClearCache },
			method:  http.MethodPost, target: "/api/v1/cache/clear",
			setup: func(m *mocks.MockQuicBaaS) {
				m.ClearCacheFunc = func(ctx context.Context, req *service.ClearCacheRequest) (*service.ClearCacheResponse, error) {
					return &service.ClearCacheResponse{EntriesCleared: 50}, nil
				}
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServerWithFuncMock(t)
			defer cleanup()
			if tt.setup != nil {
				tt.setup(mockService)
			}

			req := httptest.NewRequest(tt.method, tt.target, nil)
			if tt.shardID != "" {
				rctx := chi.NewRouteContext()
				rctx.URLParams.Add("shardID", tt.shardID)
				req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
			}

			rec := httptest.NewRecorder()
			tt.handler(server)(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

// =============================================================================
// Backup Handler Tests
// =============================================================================

// TestHandleBackup_WithFuncMock drives the create, list, restore and delete
// backup handlers with one row per verb and case.
func TestHandleBackup_WithFuncMock(t *testing.T) {
	tests := []struct {
		name       string
		handler    func(s *Server) http.HandlerFunc
		method     string
		target     string
		backupID   string
		body       string
		setup      func(m *mocks.MockQuicBaaS)
		wantStatus int
	}{
		{
			name:    "create returns 201",
			handler: func(s *Server) http.HandlerFunc { return s.HandleCreateBackup },
			method:  http.MethodPost, target: "/api/v1/backups",
			body: `{"metadata": {"label": "daily"}}`,
			setup: func(m *mocks.MockQuicBaaS) {
				m.CreateBackupFunc = func(ctx context.Context, req *service.CreateBackupRequest) (*service.CreateBackupResponse, error) {
					return &service.CreateBackupResponse{BackupID: "backup-123", Size: 1024 * 1024}, nil
				}
			},
			wantStatus: http.StatusCreated,
		},
		{
			name:    "list",
			handler: func(s *Server) http.HandlerFunc { return s.HandleListBackups },
			method:  http.MethodGet, target: "/api/v1/backups",
			setup: func(m *mocks.MockQuicBaaS) {
				m.ListBackupsFunc = func(ctx context.Context, req *service.ListBackupsRequest) (*service.ListBackupsResponse, error) {
					return &service.ListBackupsResponse{Backups: []*service.BackupMetadata{
						{BackupID: "backup-1", Size: 1024},
						{BackupID: "backup-2", Size: 2048},
					}}, nil
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:    "restore",
			handler: func(s *Server) http.HandlerFunc { return s.HandleRestoreBackup },
			method:  http.MethodPost, target: "/api/v1/backups/backup-123/restore", backupID: "backup-123",
			body: `{"target_shards": []}`,
			setup: func(m *mocks.MockQuicBaaS) {
				m.RestoreBackupFunc = func(ctx context.Context, req *service.RestoreBackupRequest) (*service.RestoreBackupResponse, error) {
					return &service.RestoreBackupResponse{JobID: "restore-job-123", Status: "completed", Progress: 100}, nil
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:    "restore missing backup ID",
			handler: func(s *Server) http.HandlerFunc { return s.HandleRestoreBackup },
			method:  http.MethodPost, target: "/api/v1/backups//restore",
			body:       `{"target_shards": []}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "delete",
			handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteBackup },
			method:  http.MethodDelete, target: "/api/v1/backups/backup-123", backupID: "backup-123",
			setup: func(m *mocks.MockQuicBaaS) {
				m.DeleteBackupFunc = func(ctx context.Context, req *service.DeleteBackupRequest) (*service.DeleteBackupResponse, error) {
					return &service.DeleteBackupResponse{Success: true, Message: "backup deleted successfully"}, nil
				}
			},
			wantStatus: http.StatusOK,
		},
		{
			name:    "delete missing backup ID",
			handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteBackup },
			method:  http.MethodDelete, target: "/api/v1/backups/",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServerWithFuncMock(t)
			defer cleanup()
			if tt.setup != nil {
				tt.setup(mockService)
			}

			req := httptest.NewRequest(tt.method, tt.target, strings.NewReader(tt.body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tt.backupID)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			rec := httptest.NewRecorder()
			tt.handler(server)(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
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
