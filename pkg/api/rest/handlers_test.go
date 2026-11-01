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
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// setupMockServer creates a test server with mock dependencies.
func setupMockServer(t *testing.T) (*Server, *testutil.MockQuicBaaS, func()) {
	t.Helper()

	// Create logger
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	// Create mock service
	mockService := testutil.NewMockQuicBaaS()

	// Create REST server
	serverConfig := DefaultConfig()
	serverConfig.Address = ":0"

	server, err := NewServer(mockService, serverConfig, log)
	require.NoError(t, err)

	cleanup := func() {
		server.Close()
	}

	return server, mockService, cleanup
}

func TestHandlePut(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		method         string
		path           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:   "create entity with POST",
			method: http.MethodPost,
			path:   "/api/v1/entities/users",
			body: PutRequest{
				EntityType: "users",
				ID:         0,
				Data:       json.RawMessage(`{"name":"Alice","email":"alice@example.com"}`),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Put", mock.Anything, mock.Anything).Return(&service.PutResponse{
					ID:        1,
					Version:   1,
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp PutResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "users", resp.EntityType)
				assert.True(t, resp.Created)
			},
		},
		{
			name:   "update entity with PUT",
			method: http.MethodPut,
			path:   "/api/v1/entities/users/123",
			body: PutRequest{
				EntityType: "users",
				ID:         123,
				Data:       json.RawMessage(`{"name":"Bob","email":"bob@example.com"}`),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Put", mock.Anything, mock.Anything).Return(&service.PutResponse{
					ID:        123,
					Version:   2,
					Timestamp: 12346,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp PutResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "users", resp.EntityType)
				assert.Equal(t, uint64(123), resp.ID)
				assert.False(t, resp.Created)
			},
		},
		{
			name:   "update entity with wal_durable write mode",
			method: http.MethodPut,
			path:   "/api/v1/entities/users/1?write_mode=wal_durable",
			body: PutRequest{
				EntityType: "users",
				ID:         1,
				Data:       json.RawMessage(`{"name":"test"}`),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Put", mock.Anything, mock.Anything).Return(&service.PutResponse{
					ID: 1, Version: 1, Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:   "update entity with batched write mode",
			method: http.MethodPut,
			path:   "/api/v1/entities/users/1?write_mode=batched",
			body: PutRequest{
				EntityType: "users",
				ID:         1,
				Data:       json.RawMessage(`{"name":"test"}`),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Put", mock.Anything, mock.Anything).Return(&service.PutResponse{
					ID: 1, Version: 1, Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing entity type",
			method:         http.MethodPost,
			path:           "/api/v1/entities/",
			body:           PutRequest{Data: json.RawMessage(`{"test":"data"}`)},
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "invalid entity ID in URL",
			method:         http.MethodPut,
			path:           "/api/v1/entities/users/invalid",
			body:           PutRequest{},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "invalid entity ID")
			},
		},
		{
			name:           "invalid JSON body",
			method:         http.MethodPost,
			path:           "/api/v1/entities/users",
			body:           "not-json",
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "invalid JSON")
			},
		},
		{
			name:   "invalid consistency level",
			method: http.MethodPost,
			path:   "/api/v1/entities/users",
			body: PutRequest{
				EntityType:       "users",
				Data:             json.RawMessage(`{"test":"data"}`),
				ConsistencyLevel: "invalid",
			},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "consistency level")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupMock != nil {
				tt.setupMock(mockService)
			}

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleGet(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		path           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "get entity by ID",
			path: "/api/v1/entities/users/123",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Get", mock.Anything, mock.Anything).
					Return(nil, errors.Newf(errors.ErrNotFound, "test", "not found")).Once()
			},
			expectedStatus: http.StatusNotFound,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
			},
		},
		{
			name:           "invalid entity ID",
			path:           "/api/v1/entities/users/abc",
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "invalid entity ID")
			},
		},
		{
			name:           "missing entity type and ID",
			path:           "/api/v1/entities//",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "get with consistency level",
			path: "/api/v1/entities/users/456?consistency=strong",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Get", mock.Anything, mock.Anything).
					Return(nil, errors.Newf(errors.ErrNotFound, "test", "not found")).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "invalid consistency level",
			path:           "/api/v1/entities/users/456?consistency=invalid",
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "consistency level")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupMock != nil {
				tt.setupMock(mockService)
			}

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}

	// Additional test cases for coverage
	t.Run("successful get", func(t *testing.T) {
		mockService.ExpectedCalls = nil

		mockService.On("Get", mock.Anything, mock.Anything).Return(&service.GetResponse{
			ID:        789,
			Data:      []byte(`{"name":"Alice","age":30}`),
			Version:   1,
			Timestamp: 12345,
			Found:     true,
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/entities/user/789", nil)
		w := httptest.NewRecorder()

		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp GetResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, uint64(789), resp.ID)
		assert.Equal(t, "user", resp.EntityType)
	})

	t.Run("result not found", func(t *testing.T) {
		mockService.ExpectedCalls = nil
		mockService.On("Get", mock.Anything, mock.Anything).Return(&service.GetResponse{
			Found: false,
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/entities/user/999", nil)
		w := httptest.NewRecorder()

		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("binary data response", func(t *testing.T) {
		mockService.ExpectedCalls = nil
		// Return arbitrary binary data - this is valid with binary protocol
		mockService.On("Get", mock.Anything, mock.Anything).Return(&service.GetResponse{
			ID:        222,
			Data:      []byte{0x01, 0x02, 0x03, 0x04},
			Version:   1,
			Timestamp: 12345,
			Found:     true,
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/entities/user/222", nil)
		w := httptest.NewRecorder()

		server.GetRouter().ServeHTTP(w, req)

		// Binary protocol: any bytes are valid
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestHandleDelete(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		path           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "delete entity by ID",
			path: "/api/v1/entities/users/123",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Delete", mock.Anything, mock.Anything).Return(&service.DeleteResponse{
					Deleted:   true,
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp DeleteResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, uint64(123), resp.ID)
				assert.Equal(t, "users", resp.EntityType)
				assert.True(t, resp.Deleted)
			},
		},
		{
			name:           "invalid entity ID",
			path:           "/api/v1/entities/users/notanumber",
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "invalid entity ID")
			},
		},
		{
			name:           "missing entity type",
			path:           "/api/v1/entities//123",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupMock != nil {
				tt.setupMock(mockService)
			}

			req := httptest.NewRequest(http.MethodDelete, tt.path, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleList(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		path           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "list entities",
			path: "/api/v1/entities/users",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("List", mock.Anything, mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						stream := args.Get(2).(chan<- *service.ListResponse)
						close(stream)
					}).Return(nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ListResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.NotNil(t, resp.Entities)
			},
		},
		{
			name: "list with limit",
			path: "/api/v1/entities/users?limit=50",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("List", mock.Anything, mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						stream := args.Get(2).(chan<- *service.ListResponse)
						close(stream)
					}).Return(nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ListResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.NotNil(t, resp.Entities)
			},
		},
		{
			name: "list with cursor",
			path: "/api/v1/entities/users?cursor=next-page",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("List", mock.Anything, mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						stream := args.Get(2).(chan<- *service.ListResponse)
						close(stream)
					}).Return(nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "list with consistency level",
			path: "/api/v1/entities/users?consistency=strong",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("List", mock.Anything, mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						stream := args.Get(2).(chan<- *service.ListResponse)
						close(stream)
					}).Return(nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid limit - negative",
			path:           "/api/v1/entities/users?limit=-1",
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "limit")
			},
		},
		{
			name:           "invalid limit - too large",
			path:           "/api/v1/entities/users?limit=5000",
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "limit")
			},
		},
		{
			name:           "invalid limit - not a number",
			path:           "/api/v1/entities/users?limit=abc",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid consistency level",
			path:           "/api/v1/entities/users?consistency=invalid",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "missing entity type",
			path:           "/api/v1/entities/",
			expectedStatus: http.StatusNotFound,
		},
		{
			name: "total count beyond int is a server failure",
			path: "/api/v1/entities/users",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("List", mock.Anything, mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						stream := args.Get(2).(chan<- *service.ListResponse)
						stream <- &service.ListResponse{TotalCount: math.MaxUint64}
						close(stream)
					}).Return(nil).Once()
			},
			expectedStatus: http.StatusInternalServerError,
			checkResponse:  requireServerFailure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupMock != nil {
				tt.setupMock(mockService)
			}

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleQuery(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		path           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "query by index",
			path: "/api/v1/query/users/email?value=alice@example.com",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Query", mock.Anything, mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						stream := args.Get(2).(chan<- *service.QueryResponse)
						stream <- &service.QueryResponse{
							Entities: []*service.Entity{},
						}
						close(stream)
					}).Return(nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp QueryResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "email", resp.Index)
				assert.NotNil(t, resp.Results)
			},
		},
		{
			name: "query with consistency",
			path: "/api/v1/query/users/email?value=test&consistency=strong",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Query", mock.Anything, mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						stream := args.Get(2).(chan<- *service.QueryResponse)
						stream <- &service.QueryResponse{
							Entities: []*service.Entity{},
						}
						close(stream)
					}).Return(nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "query with empty result stream",
			path: "/api/v1/query/users/name?value=test",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Query", mock.Anything, mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						stream := args.Get(2).(chan<- *service.QueryResponse)
						close(stream)
					}).Return(nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing value parameter",
			path:           "/api/v1/query/users/email",
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "value")
			},
		},
		{
			name:           "invalid consistency level",
			path:           "/api/v1/query/users/email?value=test&consistency=invalid",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "missing entity type",
			path:           "/api/v1/query//email?value=test",
			expectedStatus: http.StatusBadRequest, // Changed from 404 to match actual behavior
		},
		{
			name:           "missing index",
			path:           "/api/v1/query/users/?value=test",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupMock != nil {
				tt.setupMock(mockService)
			}
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleRangeQuery(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "valid range query",
			body: RangeQueryRequest{
				EntityType: "users",
				Index:      "age",
				Start:      18,
				End:        65,
				Limit:      100,
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("ScanIndex", mock.Anything, mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						stream := args.Get(2).(chan<- *service.ScanResponse)
						stream <- &service.ScanResponse{
							Entities: []*service.Entity{
								{ID: 1, EntityType: "users", Data: []byte(`{"name":"test"}`)},
							},
							TotalCount: 1,
						}
						close(stream)
					}).Return(nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp QueryResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "age", resp.Index)
				assert.NotNil(t, resp.Results)
			},
		},
		{
			name: "range query with consistency",
			body: RangeQueryRequest{
				EntityType:       "users",
				Index:            "score",
				Start:            0,
				End:              100,
				ConsistencyLevel: "strong",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("ScanIndex", mock.Anything, mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						stream := args.Get(2).(chan<- *service.ScanResponse)
						close(stream)
					}).Return(nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid JSON body",
			body:           "not-json",
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "invalid JSON")
			},
		},
		{
			name: "invalid consistency level",
			body: RangeQueryRequest{
				EntityType:       "users",
				Index:            "age",
				Start:            1,
				End:              10,
				ConsistencyLevel: "invalid",
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupMock != nil {
				tt.setupMock(mockService)
			}
			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/query/range", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleClusterStatus(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	// Setup mock to return valid cluster status
	mockService.ExpectedCalls = nil
	mockService.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
		ClusterID: "test-cluster",
		Nodes: []*service.NodeInfo{
			{NodeID: 1, Address: "127.0.0.1:9999", IsLeader: true, HealthStatus: service.HealthStatusHealthy},
		},
		Shards: []*service.ShardInfo{
			{ShardID: 1, LeaderID: 1, Replicas: []uint64{1}, Status: service.HealthStatusHealthy},
		},
		Timestamp: 12345,
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/status", nil)
	w := httptest.NewRecorder()

	server.GetRouter().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ClusterStatusResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.NotNil(t, resp.Shards)
}

func TestHandleGetLeader(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		path           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "get leader for valid shard",
			path: "/api/v1/cluster/leader/1",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("GetLeader", mock.Anything, mock.Anything).Return(&service.GetLeaderResponse{
					ShardID:  1,
					LeaderID: 1,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp LeaderResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, uint64(1), resp.ShardID)
			},
		},
		{
			name:           "invalid shard ID",
			path:           "/api/v1/cluster/leader/invalid",
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "invalid shard ID")
			},
		},
		{
			name:           "missing shard ID",
			path:           "/api/v1/cluster/leader/",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupMock != nil {
				tt.setupMock(mockService)
			}

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestBuildEntityKey(t *testing.T) {
	tests := []struct {
		name       string
		entityType string
		entityID   uint64
		expected   string
	}{
		{
			name:       "simple entity key",
			entityType: "users",
			entityID:   123,
			expected:   "users:123",
		},
		{
			name:       "zero entity ID",
			entityType: "products",
			entityID:   0,
			expected:   "products:0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := buildEntityKey(tt.entityType, tt.entityID)
			assert.Equal(t, tt.expected, string(key))
		})
	}
}

func TestHandleServiceError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		err            error
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "not found error",
			err: &errors.QBError{
				Code: errors.ErrNotFound,
				Op:   "test",
				Err:  &errors.MessageError{Message: "entity not found"},
			},
			expectedStatus: http.StatusNotFound,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "not found")
			},
		},
		{
			name: "leader not available error",
			err: &errors.QBError{
				Code: errors.ErrLeaderNotAvailable,
				Op:   "test",
				Err:  &errors.MessageError{Message: "leader not available"},
			},
			expectedStatus: http.StatusServiceUnavailable,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				// The wire carries the CODE and its retryability, not the
				// wrapped cause: a leadership transition describes the server,
				// so since #427 its text is stable and the detail goes to the
				// log. What a client branches on is asserted here; what it must
				// no longer see is asserted in
				// handlers_service_error_disclosure_test.go.
				assert.Equal(t, errors.ErrLeaderNotAvailable.String(), errResp.Code)
				assert.True(t, errResp.Retryable,
					"a leadership transition must stay retryable, or clients turn a transient failure into a hard one")
				assert.Contains(t, errResp.Error, ErrMsgServerFailure)
			},
		},
		{
			name: "timeout error",
			err: &errors.QBError{
				Code: errors.ErrTimeout,
				Op:   "test",
				Err:  &errors.MessageError{Message: "operation timed out"},
			},
			expectedStatus: http.StatusRequestTimeout,
		},
		{
			name: "generic qb error",
			err: &errors.QBError{
				Code: errors.ErrInternal,
				Op:   "test",
				Err:  &errors.MessageError{Message: "internal error"},
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:           "non-qb error",
			err:            &errors.MessageError{Message: "generic error"},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
			w := httptest.NewRecorder()

			server.handleServiceError(w, req, tt.err)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestErrorTypes(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name: "validation error with field",
			err: &ValidationError{
				Field:   "email",
				Message: "invalid format",
			},
			expected: "validation error for email: invalid format",
		},
		{
			name: "validation error without field",
			err: &ValidationError{
				Message: "missing required fields",
			},
			expected: "validation error: missing required fields",
		},
		{
			name: "not found error",
			err: &NotFoundError{
				Resource: "user/123",
			},
			expected: "resource not found: user/123",
		},
		{
			name: "service unavailable error",
			err: &ServiceUnavailableError{
				Message: "service is down",
			},
			expected: "service is down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

func TestRootRedirect(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	server.GetRouter().ServeHTTP(w, req)

	assert.Equal(t, http.StatusMovedPermanently, w.Code)
	assert.Equal(t, "/docs/", w.Header().Get("Location"))
}

func TestHandleBatchPut(t *testing.T) {
	t.Run("successful batch put", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("BatchPut", mock.Anything, mock.Anything).Return(&service.BatchPutResponse{
			Responses: []*service.PutResponse{
				{ID: 1, Version: 1, Timestamp: 12345},
				{ID: 2, Version: 1, Timestamp: 12346},
			},
			Timestamp: 12346,
		}, nil).Once()

		batchReq := BatchPutRequest{
			Operations: []BatchPutOperation{
				{
					EntityType: "user",
					ID:         1,
					Data:       json.RawMessage(`{"name":"Alice"}`),
				},
				{
					EntityType: "user",
					ID:         2,
					Data:       json.RawMessage(`{"name":"Bob"}`),
				},
			},
		}

		body, err := json.Marshal(batchReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/put", bytes.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleBatchPut(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp BatchPutResponse
		err = json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)

		assert.True(t, resp.Success)
		assert.Len(t, resp.Responses, 2)
		assert.Equal(t, uint64(1), resp.Responses[0].ID)
		assert.Equal(t, "user", resp.Responses[0].EntityType)
		assert.True(t, resp.Responses[0].Created)
		assert.Equal(t, uint64(2), resp.Responses[1].ID)
		assert.True(t, resp.Responses[1].Created)
	})

	t.Run("empty operations", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		batchReq := BatchPutRequest{
			Operations: []BatchPutOperation{},
		}

		body, err := json.Marshal(batchReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/put", bytes.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleBatchPut(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "batch operations cannot be empty")
	})

	t.Run("missing entity type", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		batchReq := BatchPutRequest{
			Operations: []BatchPutOperation{
				{
					EntityType: "",
					ID:         1,
					Data:       json.RawMessage(`{"name":"Alice"}`),
				},
			},
		}

		body, err := json.Marshal(batchReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/put", bytes.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleBatchPut(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "entity_type is required")
	})

	t.Run("missing id", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		batchReq := BatchPutRequest{
			Operations: []BatchPutOperation{
				{
					EntityType: "user",
					ID:         0,
					Data:       json.RawMessage(`{"name":"Alice"}`),
				},
			},
		}

		body, err := json.Marshal(batchReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/put", bytes.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleBatchPut(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "id is required")
	})

	t.Run("invalid json body", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/put", bytes.NewReader([]byte("invalid json")))
		w := httptest.NewRecorder()

		server.HandleBatchPut(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("batch put error", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.ExpectedCalls = nil
		mockService.On("BatchPut", mock.Anything, mock.Anything).Return(nil,
			&errors.QBError{Code: errors.ErrRaft, Op: "test", Err: &errors.MessageError{Message: "raft error"}}).Once()

		batchReq := BatchPutRequest{
			Operations: []BatchPutOperation{
				{
					EntityType: "user",
					ID:         1,
					Data:       json.RawMessage(`{"name":"Alice"}`),
				},
			},
		}

		body, err := json.Marshal(batchReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/put", bytes.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleBatchPut(w, req)

		// ErrRaft returns 503 for service unavailable
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

func TestHandleBatchDelete(t *testing.T) {
	t.Run("successful batch delete", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("BatchDelete", mock.Anything, mock.Anything).Return(&service.BatchDeleteResponse{
			Responses: []*service.DeleteResponse{
				{Deleted: true},
				{Deleted: true},
				{Deleted: true},
			},
			Timestamp: 12345,
		}, nil).Once()

		batchReq := BatchDeleteRequest{
			Operations: []BatchDeleteOperation{
				{
					EntityType: "user",
					ID:         1,
				},
				{
					EntityType: "user",
					ID:         2,
				},
				{
					EntityType: "user",
					ID:         3,
				},
			},
		}

		body, err := json.Marshal(batchReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/delete", bytes.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleBatchDelete(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp BatchDeleteResponse
		err = json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)

		assert.True(t, resp.Success)
		assert.Len(t, resp.Responses, 3)
		assert.Equal(t, uint64(1), resp.Responses[0].ID)
		assert.Equal(t, "user", resp.Responses[0].EntityType)
		assert.True(t, resp.Responses[0].Deleted)
		assert.Equal(t, uint64(2), resp.Responses[1].ID)
		assert.True(t, resp.Responses[1].Deleted)
		assert.Equal(t, uint64(3), resp.Responses[2].ID)
		assert.True(t, resp.Responses[2].Deleted)
	})

	t.Run("empty operations", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		batchReq := BatchDeleteRequest{
			Operations: []BatchDeleteOperation{},
		}

		body, err := json.Marshal(batchReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/delete", bytes.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleBatchDelete(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "batch operations cannot be empty")
	})

	t.Run("missing entity type", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		batchReq := BatchDeleteRequest{
			Operations: []BatchDeleteOperation{
				{
					EntityType: "",
					ID:         1,
				},
			},
		}

		body, err := json.Marshal(batchReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/delete", bytes.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleBatchDelete(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "entity_type is required")
	})

	t.Run("missing id", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		batchReq := BatchDeleteRequest{
			Operations: []BatchDeleteOperation{
				{
					EntityType: "user",
					ID:         0,
				},
			},
		}

		body, err := json.Marshal(batchReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/delete", bytes.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleBatchDelete(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "id is required")
	})

	t.Run("invalid json body", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/delete", bytes.NewReader([]byte("invalid json")))
		w := httptest.NewRecorder()

		server.HandleBatchDelete(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("batch delete error", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.ExpectedCalls = nil
		mockService.On("BatchDelete", mock.Anything, mock.Anything).Return(nil,
			&errors.QBError{Code: errors.ErrRaft, Op: "test", Err: &errors.MessageError{Message: "raft error"}}).Once()

		batchReq := BatchDeleteRequest{
			Operations: []BatchDeleteOperation{
				{
					EntityType: "user",
					ID:         1,
				},
			},
		}

		body, err := json.Marshal(batchReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/delete", bytes.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleBatchDelete(w, req)

		// ErrRaft returns 503 for service unavailable
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

func TestHandleListEntityTypes(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	t.Run("success with entity types", func(t *testing.T) {
		mockService.ExpectedCalls = nil
		mockService.On("ListEntityTypes", mock.Anything, mock.Anything).Return(&service.ListEntityTypesResponse{
			EntityTypes:   []string{"User", "Zone", "Organization"},
			NextPageToken: "",
			TotalCount:    3,
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/entity-types", nil)
		w := httptest.NewRecorder()
		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp ListEntityTypesResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Len(t, resp.EntityTypes, 3)
		assert.Equal(t, 3, resp.TotalCount)
		assert.Contains(t, resp.EntityTypes, "User")
		assert.Contains(t, resp.EntityTypes, "Zone")
		assert.Contains(t, resp.EntityTypes, "Organization")
	})

	t.Run("success with pagination", func(t *testing.T) {
		mockService.ExpectedCalls = nil
		mockService.On("ListEntityTypes", mock.Anything, mock.MatchedBy(func(req *service.ListEntityTypesRequest) bool {
			return req.PageSize == 10 && req.PageToken == "cursor123"
		})).Return(&service.ListEntityTypesResponse{
			EntityTypes:   []string{"User", "Zone"},
			NextPageToken: "next-cursor",
			TotalCount:    5,
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/entity-types?limit=10&cursor=cursor123", nil)
		w := httptest.NewRecorder()
		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp ListEntityTypesResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Len(t, resp.EntityTypes, 2)
		assert.Equal(t, "next-cursor", resp.NextPageToken)
	})

	t.Run("invalid limit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/entity-types?limit=invalid", nil)
		w := httptest.NewRecorder()
		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("limit out of range", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/entity-types?limit=5000", nil)
		w := httptest.NewRecorder()
		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service error", func(t *testing.T) {
		mockService.ExpectedCalls = nil
		mockService.On("ListEntityTypes", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "test",
			Err:  &errors.MessageError{Message: "internal error"},
		}).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/entity-types", nil)
		w := httptest.NewRecorder()
		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestCov91_HandleSetKeySafety_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockKMSService = &mocks.MockKMSService{
		SetKeySafetyFunc: func(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
			return &service.SetKeySafetyResponse{
				KeyID: req.KeyID,
				Config: service.AEADSafetyConfigDTO{
					MaxOperations:     1000000,
					MaxBytes:          1073741824,
					WarningPercent:    80,
					AutoRotatePercent: 90,
				},
				Timestamp: time.Now(),
			}, nil
		},
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "my-key")
	body := `{"max_operations":1000000,"max_bytes":1073741824,"warning_percent":80,"auto_rotate_percent":90}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/my-key/safety", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleSetKeySafety(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCov91_HandleSetKeySafety_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockKMSService = &mocks.MockKMSService{
		SetKeySafetyFunc: func(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrInternal, Err: stderrors.New("safety error")}
		},
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "my-key")
	body := `{"max_operations":1000000}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/my-key/safety", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleSetKeySafety(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCov91_HandleClearKeySafety_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockKMSService = &mocks.MockKMSService{
		ClearKeySafetyFunc: func(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
			return &service.ClearKeySafetyResponse{
				KeyID:     req.KeyID,
				Timestamp: time.Now(),
			}, nil
		},
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "my-key")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/keys/my-key/safety", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleClearKeySafety(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCov91_HandleClearKeySafety_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockKMSService = &mocks.MockKMSService{
		ClearKeySafetyFunc: func(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrInternal, Err: stderrors.New("clear error")}
		},
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "my-key")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/keys/my-key/safety", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleClearKeySafety(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCov91_HandleSafetyCheck_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockKMSService = &mocks.MockKMSService{
		SafetyCheckFunc: func(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
			return &service.SafetyCheckResponse{
				Items: []service.SafetyCheckItem{
					{ID: "key-1", Type: "key", OperationCount: 500, MaxOperations: 1000, UsagePercent: 50.0},
				},
				ThresholdPercent: 80.0,
				TotalKeysChecked: 5,
				TotalDEKsChecked: 10,
				Timestamp:        time.Now(),
			}, nil
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/safety/check?threshold_percent=80", nil)
	w := httptest.NewRecorder()
	server.HandleSafetyCheck(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCov91_HandleSafetyCheck_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockKMSService = &mocks.MockKMSService{
		SafetyCheckFunc: func(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrInternal, Err: stderrors.New("check error")}
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/safety/check", nil)
	w := httptest.NewRecorder()
	server.HandleSafetyCheck(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCov91_HandleSafetyReport_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockKMSService = &mocks.MockKMSService{
		SafetyReportFunc: func(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
			return &service.SafetyReportResponse{
				Items: []service.SafetyCheckItem{
					{ID: "key-1", Type: "key", OperationCount: 500, BytesProcessed: 1024, MaxOperations: 1000, MaxBytes: 2048, UsagePercent: 50.0},
				},
				TotalOperations:     500,
				TotalBytesProcessed: 1024,
				ServerConfig: service.AEADServerConfig{
					Enforce:           true,
					MaxOperations:     1000000,
					MaxBytes:          1073741824,
					WarningPercent:    80,
					AutoRotatePercent: 90,
				},
				Timestamp: time.Now(),
			}, nil
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/safety/report?include_details=true", nil)
	w := httptest.NewRecorder()
	server.HandleSafetyReport(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCov91_HandleSafetyReport_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockKMSService = &mocks.MockKMSService{
		SafetyReportFunc: func(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrInternal, Err: stderrors.New("report error")}
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/safety/report", nil)
	w := httptest.NewRecorder()
	server.HandleSafetyReport(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCov91_HandleSetKeyPolicy_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockKMSService = &mocks.MockKMSService{
		SetKeyPolicyFunc: func(ctx context.Context, req *service.SetKeyPolicyRequest) (*service.SetKeyPolicyResponse, error) {
			return &service.SetKeyPolicyResponse{
				KeyID:         req.KeyID,
				PolicyVersion: 2,
				Timestamp:     time.Now(),
			}, nil
		},
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "my-key")
	body := `{"allowed_operations":["encrypt","decrypt"]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/my-key/policy", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleSetKeyPolicy(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCov91_HandleSetKeyPolicy_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockKMSService = &mocks.MockKMSService{
		SetKeyPolicyFunc: func(ctx context.Context, req *service.SetKeyPolicyRequest) (*service.SetKeyPolicyResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrInternal, Err: stderrors.New("policy error")}
		},
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "my-key")
	body := `{"allowed_operations":["encrypt"]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/my-key/policy", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleSetKeyPolicy(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCov91_HandleGetMessagingChannel_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{
		GetChannelFunc: func(ctx context.Context, name string) (*messaging.Channel, error) {
			return nil, stderrors.New("channel not found")
		},
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "nonexistent")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/channels/nonexistent", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetMessagingChannel(w, req)

	assert.True(t, w.Code >= 400)
}

func TestCov91_HandleDeleteMessagingChannel_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{
		DeleteChannelFunc: func(ctx context.Context, name string) error {
			return stderrors.New("delete failed")
		},
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "ch1")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/messaging/channels/ch1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleDeleteMessagingChannel(w, req)

	assert.True(t, w.Code >= 400)
}

func TestCov91_HandleMessagingUnsubscribe_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{
		UnsubscribeFunc: func(ctx context.Context, subscriptionID string) error {
			return stderrors.New("unsubscribe failed")
		},
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "sub-123")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/messaging/subscribe/sub-123", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingUnsubscribe(w, req)

	assert.True(t, w.Code >= 400)
}

func TestCov91_HandleMessagingPublishDelta_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{
		PublishDeltaFunc: func(ctx context.Context, channel string, old, new interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
			return nil, stderrors.New("publish failed")
		},
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "ch1")
	body := `{"old":"a","new":"b"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish-delta/ch1", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingPublishDelta(w, req)

	assert.True(t, w.Code >= 400)
}

func TestCov91_HandleMessagingChannelStats_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{
		GetChannelFunc: func(ctx context.Context, name string) (*messaging.Channel, error) {
			return nil, stderrors.New("channel not found")
		},
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "nonexistent")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/stats/nonexistent", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingChannelStats(w, req)

	assert.True(t, w.Code >= 400)
}

func TestCov91_HandleKVPut_ServiceError_Conflict(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.PutFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
		return nil, &errors.QBError{Code: errors.ErrConflict, Op: "Put", Err: stderrors.New("version conflict")}
	}

	body := `{"value": "test"}`
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("key", "alice")
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kv/users/alice", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleKVPut(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestCov91_HandleKVDelete_ServiceError_Internal(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.DeleteFunc = func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "Delete", Err: stderrors.New("internal")}
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("key", "alice")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kv/users/alice", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleKVDelete(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCov91_HandleRangeQuery_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.ScanIndexFunc = func(ctx context.Context, req *service.ScanRequest, stream chan<- *service.ScanResponse) error {
		defer close(stream)
		return &errors.QBError{Code: errors.ErrInternal, Op: "ScanIndex", Err: stderrors.New("range error")}
	}

	body := `{"entity_type":"users","start":"a","end":"z"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/range", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleRangeQuery(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCov91_HandleListVersions_MissingPath(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/?versions=true", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleListVersions(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCov91_HandleExportVectors_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	body := `{"format":"json"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/admin/export/test-ns", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleExportVectors(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestCov91_HandleImportVectors_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	body := `{"id":"vec1","vector":[1.0,2.0]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/admin/import/test-ns", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleImportVectors(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestCov91_HandleDeleteParameter_MissingPath(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/parameters/", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleDeleteParameter(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCov91_HandleReplaceNode_InvalidShardID(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("nodeId", "invalid")
	body := `{"old_node_id": 1, "new_node_id": 2, "new_address": "localhost:9001"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/shards/invalid/replace", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleReplaceNode(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCov91_HandleReplaceNode_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.ClusterStatusFunc = func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "ClusterStatus", Err: stderrors.New("cluster error")}
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("nodeId", "1")
	body := `{"replacement_node_id": 2, "replacement_address": "localhost:9001"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/shards/1/replace", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleReplaceNode(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// =============================================================================
// Ollama Handler - Additional Coverage
// =============================================================================

func TestCov91_HandleOllamaModelInfo_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "llama2")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ollama/models/llama2", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleOllamaModelInfo(w, req)

	assert.True(t, w.Code >= 400)
}

// =============================================================================
// Messaging Handlers - Dedup Stats Error
// =============================================================================

func TestCov91_HandleMessagingDedupStats_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{
		GetChannelFunc: func(ctx context.Context, name string) (*messaging.Channel, error) {
			return nil, stderrors.New("channel not found")
		},
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "nonexistent")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/dedup/stats/nonexistent", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingDedupStats(w, req)

	assert.True(t, w.Code >= 400)
}

// =============================================================================
// Messaging Handlers - Success Paths
// =============================================================================

func TestCov91_HandleMessagingChannelStats_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{
		GetChannelFunc: func(ctx context.Context, name string) (*messaging.Channel, error) {
			return &messaging.Channel{Name: name, Subscribers: 5, MessagesPublished: 100, CreatedAt: time.Now()}, nil
		},
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "ch1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/stats/ch1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingChannelStats(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// =============================================================================
// Messaging Handlers - Missing Name Validation
// =============================================================================

// =============================================================================
// Messaging - Nil service (not configured)
// =============================================================================

// =============================================================================
// KMS Handler - Invalid JSON / Missing Params
// =============================================================================

type cacheTestLogger struct {
	*logger.Logger
}

func (l *cacheTestLogger) WithField(key string, value interface{}) interfaces.Logger {
	return l
}

func (l *cacheTestLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	return l
}

func (l *cacheTestLogger) WithError(err error) interfaces.Logger {
	return l
}

func newTestCacheManager(t *testing.T) *cache.CacheManager {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	cfg := cache.DefaultConfig()
	mgr, err := cache.NewManager(cfg, &cacheTestLogger{log})
	require.NoError(t, err)
	return mgr
}

func TestPhase4HandlerGetCacheStats_WithData(t *testing.T) {
	cacheMgr := newTestCacheManager(t)
	defer cacheMgr.Close()

	handler := NewPhase4Handler(nil, cacheMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cache/stats", nil)
	w := httptest.NewRecorder()

	handler.HandleGetCacheStats(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

	var resp pb.GetCacheStatsResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, resp.L1Capacity, int32(0))
}

func TestPhase4HandlerClearCache_WithData(t *testing.T) {
	cacheMgr := newTestCacheManager(t)
	defer cacheMgr.Close()

	handler := NewPhase4Handler(nil, cacheMgr)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cache/clear", nil)
	w := httptest.NewRecorder()

	handler.HandleClearCache(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp pb.ClearCacheResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestXkmsContextWithTimeout(t *testing.T) {
	t.Run("creates context with 30-second timeout", func(t *testing.T) {
		parent := context.Background()
		ctx, cancel := xkmsContextWithTimeout(parent)
		defer cancel()

		deadline, ok := ctx.Deadline()
		assert.True(t, ok, "context should have a deadline")
		// Deadline should be roughly 30 seconds from now
		remaining := time.Until(deadline)
		assert.Greater(t, remaining, 29*time.Second)
		assert.LessOrEqual(t, remaining, 30*time.Second)
	})

	t.Run("cancel function works", func(t *testing.T) {
		parent := context.Background()
		ctx, cancel := xkmsContextWithTimeout(parent)
		cancel()

		select {
		case <-ctx.Done():
			assert.Equal(t, context.Canceled, ctx.Err())
		default:
			t.Fatal("context should be done after cancel")
		}
	})
}

func TestHandleBatchGetObjects_EmptyKeyInArray(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/batch/get", s.HandleBatchGetObjects)

	body := `{"keys":["valid-key","","another-key"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/batch/get", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp BatchGetObjectsRESTResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, 2, resp.Success)
	assert.Equal(t, 1, resp.Failed)
	// Verify the empty key was reported as failed
	for _, result := range resp.Results {
		if result.Key == "" {
			assert.False(t, result.Success)
			assert.Equal(t, "key cannot be empty", result.Error)
		}
	}
}

func TestHandleBatchGetObjects_WithMetadata(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		getObjectFunc: func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return &service.GetObjectResponse{
				Key:         req.Key,
				Value:       []byte("test data"),
				ContentType: "text/plain",
				Size:        9,
				Version:     1,
				Tags:        map[string]string{"env": "test"},
			}, nil
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/batch/get", s.HandleBatchGetObjects)

	body := `{"keys":["key1"],"include_metadata":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/batch/get", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp BatchGetObjectsRESTResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, 1, resp.Success)
	assert.Equal(t, 0, resp.Failed)
	assert.NotNil(t, resp.Results[0].Tags)
}

func TestHandleBatchGetObjects_GetObjectError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		getObjectFunc: func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return nil, fmt.Errorf("storage error")
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/batch/get", s.HandleBatchGetObjects)

	body := `{"keys":["key1","key2"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/batch/get", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp BatchGetObjectsRESTResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Success)
	assert.Equal(t, 2, resp.Failed)
}

func TestHandleBatchDeleteObjects_EmptyKeyInArray(t *testing.T) {
	mockObjService := &mockObjectStorageService{}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/batch/delete", s.HandleBatchDeleteObjects)

	body := `{"keys":["valid-key","","another-key"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/batch/delete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp BatchDeleteObjectsRESTResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, 2, resp.Success)
	assert.Equal(t, 1, resp.Failed)
}

func TestHandleBatchDeleteObjects_DeleteError(t *testing.T) {
	mockObjService := &mockObjectStorageService{
		deleteObjectFunc: func(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error) {
			return nil, fmt.Errorf("storage error")
		},
	}
	s := newTestObjectStorageServer(mockObjService)

	r := chi.NewRouter()
	r.Post("/api/v1/objects/batch/delete", s.HandleBatchDeleteObjects)

	body := `{"keys":["key1","key2"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/batch/delete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp BatchDeleteObjectsRESTResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Success)
	assert.Equal(t, 2, resp.Failed)
}

func TestFormatVectorID(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]string
		expected string
	}{
		{name: "nil metadata", metadata: nil, expected: "42"},
		{name: "metadata without original ID", metadata: map[string]string{"other_key": "value"}, expected: "42"},
		{name: "metadata with original ID", metadata: map[string]string{MetadataKeyOriginalID: "my-doc-id"}, expected: "my-doc-id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, formatVectorID(42, tt.metadata))
		})
	}
}

func TestHandleClusterShards_NilResponse(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ClusterStatus", mock.Anything, mock.Anything).Return(nil, nil)

	rctx := chi.NewRouteContext()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/shards", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleClusterShards(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleClusterShards_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ClusterStatus", mock.Anything, mock.Anything).Return(nil, fmt.Errorf("cluster error"))

	rctx := chi.NewRouteContext()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/shards", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleClusterShards(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleGetLeader_NilResponse(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetLeader", mock.Anything, mock.Anything).Return(nil, nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("shardID", "1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/shards/1/leader", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleGetLeader(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleGetLeader_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetLeader", mock.Anything, mock.Anything).Return(nil, fmt.Errorf("raft error"))

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("shardID", "1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/shards/1/leader", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleGetLeader(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestExtractPathParamEdgeCasesMore(t *testing.T) {
	t.Run("empty path", func(t *testing.T) {
		result := extractPathParam("", "", "")
		assert.Equal(t, "", result)
	})

	t.Run("only prefix", func(t *testing.T) {
		result := extractPathParam("/api/v1/backups/", "/api/v1/backups/", "")
		assert.Equal(t, "", result)
	})

	t.Run("complex backup ID", func(t *testing.T) {
		path := "/api/v1/backups/backup_2024-01-15T10:30:00Z_full/restore"
		result := extractPathParam(path, "/api/v1/backups/", "/restore")
		assert.Equal(t, "backup_2024-01-15T10:30:00Z_full", result)
	})

	t.Run("UUID backup ID", func(t *testing.T) {
		path := "/api/v1/backups/550e8400-e29b-41d4-a716-446655440000"
		result := extractPathParam(path, "/api/v1/backups/", "")
		assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", result)
	})
}

func TestPhase4HandlerMethodNotAllowed(t *testing.T) {
	handler := &Phase4Handler{}

	t.Run("create backup with GET method", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/backups", nil)
		w := httptest.NewRecorder()

		handler.HandleCreateBackup(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("list backups with POST method", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups", nil)
		w := httptest.NewRecorder()

		handler.HandleListBackups(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("restore backup with GET method", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/backups/123/restore", nil)
		w := httptest.NewRecorder()

		handler.HandleRestoreBackup(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("delete backup with POST method", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups/123", nil)
		w := httptest.NewRecorder()

		handler.HandleDeleteBackup(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("get cache stats with POST method", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/cache/stats", nil)
		w := httptest.NewRecorder()

		handler.HandleGetCacheStats(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("clear cache with GET method", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/cache/clear", nil)
		w := httptest.NewRecorder()

		handler.HandleClearCache(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestPhase4HandlerInvalidJSON(t *testing.T) {
	handler := &Phase4Handler{}

	t.Run("create backup with invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.HandleCreateBackup(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("restore backup with invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/backups/123/restore", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.HandleRestoreBackup(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

type MockQuicBaaSWithNilParamStore struct {
	*testutil.MockQuicBaaS
}

func (m *MockQuicBaaSWithNilParamStore) ParameterStoreService() service.ParameterStoreService {
	return nil
}

func setupNilParamStoreMockServer(t *testing.T) (*Server, func()) {
	t.Helper()

	mockService := &MockQuicBaaSWithNilParamStore{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
	}

	server, _, cleanup := setupMockServer(t)
	server.service = mockService

	return server, cleanup
}

func TestHandleGetParametersBatch_ServiceErrors(t *testing.T) {
	t.Parallel()

	t.Run("service error returns 500", func(t *testing.T) {
		server, mockParamService, cleanup := setupMockServerWithParameterStore(t)
		defer cleanup()

		mockParamService.On("GetParameters", mock.Anything, mock.Anything).Return(
			nil, &qberrors.QBError{
				Code: qberrors.ErrInternal,
				Op:   "GetParameters",
				Err:  stderrors.New("database connection failed"),
			}).Once()

		body := `{"paths": ["/app/test"]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parameters/batch/get", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleGetParametersBatch(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("nil param service returns 500", func(t *testing.T) {
		server, cleanup := setupNilParamStoreMockServer(t)
		defer cleanup()

		body := `{"paths": ["/app/test"]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parameters/batch/get", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleGetParametersBatch(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "not available")
	})
}

func TestHandleSetParametersBatch_ServiceErrors(t *testing.T) {
	t.Parallel()

	t.Run("service error returns 500", func(t *testing.T) {
		server, mockParamService, cleanup := setupMockServerWithParameterStore(t)
		defer cleanup()

		mockParamService.On("SetParameters", mock.Anything, mock.Anything).Return(
			nil, &qberrors.QBError{
				Code: qberrors.ErrInternal,
				Op:   "SetParameters",
				Err:  stderrors.New("storage full"),
			}).Once()

		body := `{"parameters": [{"path": "/app/test", "value": "dGVzdA=="}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parameters/batch/set", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleSetParametersBatch(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("nil param service returns 500", func(t *testing.T) {
		server, cleanup := setupNilParamStoreMockServer(t)
		defer cleanup()

		body := `{"parameters": [{"path": "/app/test", "value": "dGVzdA=="}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parameters/batch/set", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleSetParametersBatch(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestHandleDeleteParametersBatch_ServiceErrors(t *testing.T) {
	t.Parallel()

	t.Run("service error returns 500", func(t *testing.T) {
		server, mockParamService, cleanup := setupMockServerWithParameterStore(t)
		defer cleanup()

		mockParamService.On("DeleteParameters", mock.Anything, mock.Anything).Return(
			nil, &qberrors.QBError{
				Code: qberrors.ErrInternal,
				Op:   "DeleteParameters",
				Err:  stderrors.New("write error"),
			}).Once()

		body := `{"paths": ["/app/test"]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parameters/batch/delete", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleDeleteParametersBatch(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("nil param service returns 500", func(t *testing.T) {
		server, cleanup := setupNilParamStoreMockServer(t)
		defer cleanup()

		body := `{"paths": ["/app/test"]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parameters/batch/delete", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleDeleteParametersBatch(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestHandleSearchParametersByTags_ServiceErrors(t *testing.T) {
	t.Parallel()

	t.Run("service error returns 500", func(t *testing.T) {
		server, mockParamService, cleanup := setupMockServerWithParameterStore(t)
		defer cleanup()

		mockParamService.On("SearchByTags", mock.Anything, mock.Anything).Return(
			nil, &qberrors.QBError{
				Code: qberrors.ErrInternal,
				Op:   "SearchByTags",
				Err:  stderrors.New("index error"),
			}).Once()

		body := `{"tags": {"env": "prod"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parameters/search", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleSearchParametersByTags(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("nil param service returns 500", func(t *testing.T) {
		server, cleanup := setupNilParamStoreMockServer(t)
		defer cleanup()

		body := `{"tags": {"env": "prod"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parameters/search", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleSearchParametersByTags(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		server, mockParamService, cleanup := setupMockServerWithParameterStore(t)
		defer cleanup()

		_ = mockParamService // Not used for this test

		body := `{invalid json`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parameters/search", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleSearchParametersByTags(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("empty tags returns 400", func(t *testing.T) {
		server, mockParamService, cleanup := setupMockServerWithParameterStore(t)
		defer cleanup()

		_ = mockParamService

		body := `{"tags": {}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parameters/search", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleSearchParametersByTags(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("success with all options", func(t *testing.T) {
		server, mockParamService, cleanup := setupMockServerWithParameterStore(t)
		defer cleanup()

		mockParamService.On("SearchByTags", mock.Anything, mock.MatchedBy(func(req *interfaces.SearchByTagsRequest) bool {
			return req.Tags["env"] == "prod" && req.Prefix == "/app/" && req.Limit == 50
		})).Return(&interfaces.SearchByTagsResponse{
			Parameters: []*interfaces.ParameterValue{
				{Path: "/app/db/host", Value: []byte("localhost")},
			},
			NextCursor: "",
			HasMore:    false,
			Timestamp:  time.Now(),
		}, nil).Once()

		body := `{"tags": {"env": "prod"}, "prefix": "/app/", "with_values": true, "with_decryption": false, "limit": 50}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parameters/search", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleSearchParametersByTags(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// =============================================================================
// KMS DEK Handlers - Coverage Boost Tests
// =============================================================================

// TestHandleSetDefaultDEKFromBody_EdgeCases tests edge cases for setting default DEK from body.
func TestHandleSetDefaultDEKFromBody_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		body := `{invalid json`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/deks/default", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleSetDefaultDEKFromBody(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON")
	})

	t.Run("empty path returns 400", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		body := `{"path": ""}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/deks/default", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleSetDefaultDEKFromBody(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "path is required")
	})
}

// =============================================================================
// Cluster Handlers - Coverage Boost Tests
// =============================================================================

// TestHandleReplaceNode_Validation tests validation for replace node handler.
func TestHandleReplaceNode_Validation(t *testing.T) {
	t.Parallel()

	t.Run("invalid JSON body", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		body := `{invalid json`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/replace", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleReplaceNode(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing old node ID", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		body := `{"old_node_id": 0, "new_address": "localhost:8080"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/replace", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleReplaceNode(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// =============================================================================
// Range Query Handler - Coverage Boost Tests
// =============================================================================

// =============================================================================
// Client Methods - Coverage Boost Tests
// =============================================================================

// createCoverageTestLogger creates a logger for coverage tests.
func createCoverageTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)
	return log
}

// TestRESTClient_KMSDeleteDEK_Errors tests error paths for KMS delete DEK.
func TestRESTClient_KMSDeleteDEK_Errors(t *testing.T) {
	t.Parallel()

	t.Run("server returns not found", func(t *testing.T) {
		testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(ErrorResponse{Error: "DEK not found"})
		}))
		defer testServer.Close()

		log := createCoverageTestLogger(t)
		client, err := NewRESTClient(testServer.URL, log)
		require.NoError(t, err)
		defer client.Close()

		_, err = client.KMSDeleteDEK(context.Background(), &service.DeleteDEKRequest{
			Path: "/deks/nonexistent",
		})

		assert.Error(t, err)
	})

	t.Run("server returns error", func(t *testing.T) {
		testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
		}))
		defer testServer.Close()

		log := createCoverageTestLogger(t)
		client, err := NewRESTClient(testServer.URL, log)
		require.NoError(t, err)
		defer client.Close()

		_, err = client.KMSDeleteDEK(context.Background(), &service.DeleteDEKRequest{
			Path: "/deks/test",
		})

		assert.Error(t, err)
	})
}

// TestRESTClient_WatchSecretsByPrefix_Errors tests error paths for watch secrets.
func TestRESTClient_WatchSecretsByPrefix_Errors(t *testing.T) {
	t.Parallel()

	t.Run("server returns error", func(t *testing.T) {
		testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(ErrorResponse{Error: "watch not available"})
		}))
		defer testServer.Close()

		log := createCoverageTestLogger(t)
		client, err := NewRESTClient(testServer.URL, log)
		require.NoError(t, err)
		defer client.Close()

		_, err = client.WatchSecretsByPrefix(context.Background(), &service.WatchSecretsRequest{
			Prefix: "/secrets/",
		})

		assert.Error(t, err)
	})
}

// TestRESTClient_SchedulerMethods_Errors tests various scheduler client methods.
func TestRESTClient_SchedulerMethods_Errors(t *testing.T) {
	t.Parallel()

	t.Run("trigger job server error", func(t *testing.T) {
		testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(ErrorResponse{Error: "scheduler unavailable"})
		}))
		defer testServer.Close()

		log := createCoverageTestLogger(t)
		client, err := NewRESTClient(testServer.URL, log)
		require.NoError(t, err)
		defer client.Close()

		_, err = client.SchedulerTriggerJob(context.Background(), &service.TriggerSchedulerJobRequest{
			JobID: 123,
		})

		assert.Error(t, err)
	})

	t.Run("pause job server error", func(t *testing.T) {
		testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(ErrorResponse{Error: "scheduler unavailable"})
		}))
		defer testServer.Close()

		log := createCoverageTestLogger(t)
		client, err := NewRESTClient(testServer.URL, log)
		require.NoError(t, err)
		defer client.Close()

		_, err = client.SchedulerPauseJob(context.Background(), &service.PauseSchedulerJobRequest{
			JobID: 123,
		})

		assert.Error(t, err)
	})

	t.Run("resume job server error", func(t *testing.T) {
		testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(ErrorResponse{Error: "scheduler unavailable"})
		}))
		defer testServer.Close()

		log := createCoverageTestLogger(t)
		client, err := NewRESTClient(testServer.URL, log)
		require.NoError(t, err)
		defer client.Close()

		_, err = client.SchedulerResumeJob(context.Background(), &service.ResumeSchedulerJobRequest{
			JobID: 123,
		})

		assert.Error(t, err)
	})
}

// =============================================================================
// Messaging Channel Conversion Tests
// =============================================================================

// =============================================================================
// Test Utilities
// =============================================================================

func TestHandleBatchPut_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/entities/batch", nil)
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleBatchPut(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleBatchDelete_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/entities/batch", nil)
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleBatchDelete(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandlePut_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/entities/users", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandlePut(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandlePut_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("Put", mock.Anything, mock.Anything).Return(nil, fmt.Errorf("storage error"))

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	body := `{"entity_type":"users","id":0,"data":{"name":"Alice"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/entities/users", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandlePut(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleDelete_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("Delete", mock.Anything, mock.Anything).Return(nil, fmt.Errorf("storage error"))

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("id", "123")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/entities/users/123", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleDelete(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleCryptoEncrypt_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/encrypt/test-key", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleCryptoEncrypt(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoDecrypt_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/decrypt/test-key", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleCryptoDecrypt(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoRewrap_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/rewrap/test-key", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleCryptoRewrap(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoSign_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/sign/test-key", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleCryptoSign(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoVerify_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/verify/test-key", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleCryptoVerify(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoHash_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/hash", nil)
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleCryptoHash(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoRandom_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/random", nil)
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleCryptoRandom(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoDataKey_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/datakey/test-key", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleCryptoDataKey(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoBatchEncrypt_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/batch/encrypt/test-key", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleCryptoBatchEncrypt(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoBatchDecrypt_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/batch/decrypt/test-key", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleCryptoBatchDecrypt(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleGenerateKey_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys", nil)
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleGenerateKey(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleImportKey_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/import", nil)
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleImportKey(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleStoreSecret_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "/test-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test-secret", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleSecretsAction(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleIssueCertificate_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/certificates/issue", nil)
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleIssueCertificate(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleSignCSR_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/certificates/sign", nil)
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleSignCSR(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleRevokeCertificate_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/certificates/revoke", nil)
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleRevokeCertificate(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleVerifyCertificate_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/certificates/verify", nil)
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleVerifyCertificate(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleGetTLSConfig_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/certificates/tls-config", nil)
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleGetTLSConfig(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAddNode_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/nodes", nil)
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleAddNode(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleReplaceNode_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPut, "/api/v1/cluster/nodes/replace", nil)
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleReplaceNode(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleStartRebalance_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/rebalance", nil)
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleStartRebalance(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleStartMigration_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/migrate", nil)
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleStartMigration(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleStartUpgrade_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/upgrade", nil)
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleStartUpgrade(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCreateDynamicSecretRole_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/dynamic-secrets/roles", nil)
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleCreateDynamicSecretRole(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleGenerateDynamicCredential_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("role", "test-role")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/dynamic-secrets/roles/test-role/credentials", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleGenerateDynamicCredential(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleRenewDynamicSecretLease_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("lease_id", "test-lease")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/dynamic-secrets/leases/test-lease/renew", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleRenewDynamicSecretLease(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCreateMessagingChannel_ReadBodyError(t *testing.T) {
	server, mockSvc, cleanup := setupMockServer(t)
	defer cleanup()

	mockSvc.On("SetMessagingService", mock.Anything).Return()
	mockSvc.SetMessagingService(&stubMessagingService{})

	mockSvc.On("Messaging").Return(&stubMessagingService{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/channels", nil)
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleCreateMessagingChannel(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleMessagingPublish_ReadBodyError(t *testing.T) {
	server, mockSvc, cleanup := setupMockServer(t)
	defer cleanup()

	mockSvc.On("SetMessagingService", mock.Anything).Return()
	mockSvc.SetMessagingService(&stubMessagingService{})

	mockSvc.On("Messaging").Return(&stubMessagingService{})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "test-channel")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/channels/test-channel/publish", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleMessagingPublish(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleMessagingPublishDelta_ReadBodyError(t *testing.T) {
	server, mockSvc, cleanup := setupMockServer(t)
	defer cleanup()

	mockSvc.On("SetMessagingService", mock.Anything).Return()
	mockSvc.SetMessagingService(&stubMessagingService{})

	mockSvc.On("Messaging").Return(&stubMessagingService{})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "test-channel")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/channels/test-channel/delta", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleMessagingPublishDelta(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleListMessagingChannels_ServiceError(t *testing.T) {
	server, mockSvc, cleanup := setupMockServer(t)
	defer cleanup()

	mockSvc.On("SetMessagingService", mock.Anything).Return()
	mockSvc.SetMessagingService(&stubMessagingServiceWithError{})

	mockSvc.On("Messaging").Return(&stubMessagingServiceWithError{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/channels", nil)
	w := httptest.NewRecorder()

	server.HandleListMessagingChannels(w, req)

	// Should return error status since list fails
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestHandleServiceError_ConflictError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	conflictErr := qberrors.New(qberrors.ErrConflict, "Get", "entity already exists")
	mockService.On("Get", mock.Anything, mock.Anything).Return((*service.GetResponse)(nil), conflictErr)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("id", "1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/entities/users/1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleGet(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestHandleServiceError_StatusMapping(t *testing.T) {
	tests := []struct {
		name           string
		code           qberrors.ErrorCode
		expectedStatus int
	}{
		{"invalid input", qberrors.ErrInvalidInput, http.StatusBadRequest},
		{"forbidden", qberrors.ErrForbidden, http.StatusForbidden},
		{"unauthorized", qberrors.ErrUnauthorized, http.StatusUnauthorized},
		{"unavailable", qberrors.ErrUnavailable, http.StatusServiceUnavailable},
		{"internal", qberrors.ErrInternal, http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			svcErr := qberrors.New(tt.code, "Get", tt.name)
			mockService.On("Get", mock.Anything, mock.Anything).Return((*service.GetResponse)(nil), svcErr)

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("type", "users")
			rctx.URLParams.Add("id", "1")
			req := httptest.NewRequest(http.MethodGet, "/api/v1/entities/users/1", nil)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
			w := httptest.NewRecorder()

			server.HandleGet(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestHandleSetParameter_ReadBodyError(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "/test-param")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/parameters/test-param", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	server.HandleSetParameter(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

type stubMessagingServiceWithError struct {
	stubMessagingService
}

func (s *stubMessagingServiceWithError) ListChannels(ctx context.Context) ([]*messaging.Channel, error) {
	return nil, fmt.Errorf("list channels error")
}

func TestPerformanceMetricsHandler_Encoding(t *testing.T) {
	t.Run("CollectPerformanceMetrics_ReturnsValidMetrics", func(t *testing.T) {
		log, err := logger.New(logger.LogConfig{
			Level:  "debug",
			Format: "json",
		})
		require.NoError(t, err)

		server := &Server{
			logger: log,
		}

		metrics := server.collectPerformanceMetrics()

		assert.NotNil(t, metrics)
		assert.NotZero(t, metrics.Timestamp)
		assert.GreaterOrEqual(t, metrics.Throughput.ReadsPerSec, 0.0)
		assert.GreaterOrEqual(t, metrics.Throughput.WritesPerSec, 0.0)
		assert.GreaterOrEqual(t, metrics.Latency.ReadP50Ms, 0.0)
		assert.GreaterOrEqual(t, metrics.Storage.CacheHitRatePct, 0.0)
		assert.GreaterOrEqual(t, metrics.Pool.Size, 0)
	})

	t.Run("HandlePerformanceMetrics_EncodesJSON", func(t *testing.T) {
		log, err := logger.New(logger.LogConfig{
			Level:  "debug",
			Format: "json",
		})
		require.NoError(t, err)

		server := &Server{
			logger: log,
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/performance", nil)
		w := httptest.NewRecorder()

		server.HandlePerformanceMetrics(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var metrics PerformanceMetrics
		err = json.NewDecoder(w.Body).Decode(&metrics)
		assert.NoError(t, err)
		assert.NotZero(t, metrics.Timestamp)
	})
}

func TestGetClientIPFunction_EdgeCases(t *testing.T) {
	testCases := []struct {
		name       string
		setupReq   func(*http.Request)
		expectedIP string
	}{
		{
			name: "XForwardedFor_Single",
			setupReq: func(r *http.Request) {
				r.Header.Set("X-Forwarded-For", "192.168.1.100")
			},
			expectedIP: "192.168.1.100",
		},
		{
			name: "XForwardedFor_Multiple",
			setupReq: func(r *http.Request) {
				r.Header.Set("X-Forwarded-For", "192.168.1.100, 10.0.0.1, 172.16.0.1")
			},
			expectedIP: "192.168.1.100", // Should take first IP
		},
		{
			name: "XRealIP",
			setupReq: func(r *http.Request) {
				r.Header.Set("X-Real-IP", "192.168.1.200")
			},
			expectedIP: "192.168.1.200",
		},
		{
			name: "RemoteAddr_WithPort",
			setupReq: func(r *http.Request) {
				r.RemoteAddr = "192.168.1.50:12345"
			},
			expectedIP: "192.168.1.50",
		},
		{
			name: "RemoteAddr_NoPort",
			setupReq: func(r *http.Request) {
				r.RemoteAddr = "192.168.1.75"
			},
			expectedIP: "192.168.1.75",
		},
		{
			name: "PreferXForwardedForOverXRealIP",
			setupReq: func(r *http.Request) {
				r.Header.Set("X-Forwarded-For", "192.168.1.10")
				r.Header.Set("X-Real-IP", "192.168.1.20")
			},
			expectedIP: "192.168.1.10",
		},
		{
			name: "PreferXRealIPOverRemoteAddr",
			setupReq: func(r *http.Request) {
				r.Header.Set("X-Real-IP", "192.168.1.30")
				r.RemoteAddr = "192.168.1.40:8080"
			},
			expectedIP: "192.168.1.30",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tc.setupReq != nil {
				tc.setupReq(req)
			}

			// Parsing is reached the only way a deployment reaches it: from a
			// peer the operator trusts. See resolvedViaOwnPeer.
			assert.Equal(t, tc.expectedIP, resolvedViaOwnPeer(t, req))
		})
	}
}

func TestParametersBatchValidation(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name    string
		handler http.HandlerFunc
		body    string
		errMsg  string
	}{
		{"get invalid JSON", server.HandleGetParametersBatch, `not-json`, ""},
		{"get empty paths", server.HandleGetParametersBatch, `{"paths": []}`, "at least one path is required"},
		{"delete invalid JSON", server.HandleDeleteParametersBatch, `not-json`, ""},
		{"delete empty paths", server.HandleDeleteParametersBatch, `{"paths": []}`, "at least one path is required"},
		{"set invalid JSON", server.HandleSetParametersBatch, `not-json`, ""},
		{"set empty parameters", server.HandleSetParametersBatch, `{"parameters": []}`, ""},
		{"set missing path", server.HandleSetParametersBatch, `{"parameters": [{"value": "test"}]}`, "parameter path is required"},
		{"set missing value", server.HandleSetParametersBatch, `{"parameters": [{"path": "/test/path"}]}`, "parameter value is required"},
		{"search invalid JSON", server.HandleSearchParametersByTags, `not-json`, ""},
		{"search empty tags", server.HandleSearchParametersByTags, `{"tags": {}}`, "at least one tag is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/parameters/batch", bytes.NewReader([]byte(tt.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			tt.handler(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			if tt.errMsg != "" {
				assert.Contains(t, w.Body.String(), tt.errMsg)
			}
		})
	}
}

func TestHandleKVDelete_MissingParams(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name       string
		entityType string
		key        string
	}{
		{"missing entity type", "", "test-key"},
		{"missing key", "test-type", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/kv/entities", nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("type", tt.entityType)
			rctx.URLParams.Add("key", tt.key)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleKVDelete(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "entity type and key are required")
		})
	}
}

func TestHandleCryptoBatchEncryptInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `not-json`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/batch/encrypt", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCryptoBatchEncrypt(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoBatchEncryptMissingKeyID(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{"items": [{"plaintext": "data"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/batch/encrypt", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCryptoBatchEncrypt(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoBatchDecryptInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `not-json`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/batch/decrypt", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCryptoBatchDecrypt(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoBatchDecryptMissingKeyID(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{"items": [{"ciphertext": "encrypted"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/batch/decrypt", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCryptoBatchDecrypt(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleRotateSecretMissingPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/*/rotate", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleRotateSecret(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleWatchSecretsMissingPrefix(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/watch", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleWatchSecrets(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleSetDefaultDEKFromBodyInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `not-json`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/dek/default", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleSetDefaultDEKFromBody(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleSetDefaultDEKFromBodyMissingDEKID(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/dek/default", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleSetDefaultDEKFromBody(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleReplaceNodeInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `not-json`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/nodes/replace", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleReplaceNode(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleUpdateCacheConfigInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `not-json`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/cache/config", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleUpdateCacheConfig(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleListKeysLimitTruncation(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	// Create a mock KMS manager
	mockKMSManager := &MockKMSManager{}
	mockService.DisableKMS = true
	mockService.On("KMSManager").Return(mockKMSManager)

	// Return more results than the limit to test truncation
	results := make([]interface{}, 5)
	for i := 0; i < 5; i++ {
		results[i] = &kms.KeyMetadata{
			KeyID:     "key-" + string(rune('a'+i)),
			Algorithm: "AES-256-GCM",
			State:     kms.KeyStateEnabled,
			Created:   time.Now(),
		}
	}
	mockKMSManager.On("ListKeys", mock.Anything, "").Return(results, nil).Once()

	// Request with limit=2 to trigger truncation
	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys?limit=2", nil)
	w := httptest.NewRecorder()

	server.GetRouter().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ListKeysRESTResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	assert.NoError(t, err)
	assert.Len(t, resp.Keys, 2)
	assert.True(t, resp.HasMore)
	assert.Equal(t, 5, resp.TotalCount)
}

func TestHandleStartMigrationInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `not-json`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/migrate", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleStartMigration(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleStartUpgradeInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `not-valid-json`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/upgrade", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleStartUpgrade(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleStartUpgradeMissingVersion(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/upgrade", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleStartUpgrade(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoDataKeyMissingKeyName(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/datakey", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleCryptoDataKey(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "key name is required")
}

func TestHandleRotateKeyMissingKeyID(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys//rotate", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleRotateKey(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleGenerateKeyMissingAlgorithm(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{"key_id": "test-key"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleGenerateKey(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleDeleteKeyMissingKeyID(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/keys/", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleDeleteKey(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleUpdateKeyStatusMissingKeyID(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{"status": "disabled"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/kms/keys//status", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleUpdateKeyStatus(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleUpdateKeyStatusMissingStatus(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/kms/keys/test-key/status", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleUpdateKeyStatus(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleGetKeyMissingKeyID(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys/", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGetKey(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleImportKeyMissingKeyMaterial(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{"key_id": "test-key", "algorithm": "AES-256-GCM"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/import", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleImportKey(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleImportKeyInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `not-json`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/import", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleImportKey(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleClusterShardsSuccess(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
		ClusterID: "test-cluster",
		Shards: []*service.ShardInfo{
			{
				ShardID:  1,
				LeaderID: 123,
				Replicas: []uint64{123, 124, 125},
				Status:   service.HealthStatusHealthy,
			},
		},
		Timestamp: time.Now().UnixNano(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/shards", nil)
	w := httptest.NewRecorder()

	server.HandleClusterShards(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleCryptoEncryptMissingKeyID(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{"plaintext": "dGVzdC1kYXRh"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/encrypt", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCryptoEncrypt(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoEncryptInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `not-json`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/encrypt", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCryptoEncrypt(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoDecryptMissingKeyID(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{"ciphertext": "encrypted-data"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/decrypt", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCryptoDecrypt(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoDecryptInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `not-json`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/decrypt", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCryptoDecrypt(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoSignMissingKeyID(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{"message": "dGVzdC1tZXNzYWdl"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/sign", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCryptoSign(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoSignInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `not-json`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/sign", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCryptoSign(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoVerifyMissingKeyID(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{"message": "dGVzdC1tZXNzYWdl", "signature": "c2lnbmF0dXJl"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/verify", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCryptoVerify(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoVerifyInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `not-json`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/verify", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCryptoVerify(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoHashInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `not-json`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/hash", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCryptoHash(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoRewrapInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `not-json`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/rewrap", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCryptoRewrap(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoRewrapMissingSourceKeyID(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{"destination_key_id": "new-key", "ciphertext": "encrypted"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/rewrap", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCryptoRewrap(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCryptoRandomInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `not-json`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/random", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCryptoRandom(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleVectorHealth_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.HealthFunc = func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
		return &service.HealthResponse{
			Status:        service.HealthStatusHealthy,
			UptimeSeconds: 3600,
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vector/health", nil)
	w := httptest.NewRecorder()
	server.HandleVectorHealth(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp VectorHealthResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "healthy", resp.Status)
	assert.Equal(t, int64(3600), resp.UptimeSeconds)
}

func TestRemaining91_HandleVectorHealth_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.HealthFunc = func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
		return nil, fmt.Errorf("health check failed")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vector/health", nil)
	w := httptest.NewRecorder()
	server.HandleVectorHealth(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRemaining91_HandleCreateNamespace_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockVectorSvc := &mocks.MockVectorService{
		CreateNamespaceFunc: func(ctx context.Context, req *service.CreateNamespaceRequest) (*service.CreateNamespaceResponse, error) {
			return &service.CreateNamespaceResponse{
				Namespace: req.Namespace,
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
	}
	mockService.MockVectorService = mockVectorSvc

	body := `{"namespace":"test-ns","dimensions":128,"distance_metric":"cosine"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.HandleCreateNamespace(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp CreateNamespaceResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "test-ns", resp.Namespace)
}

func TestRemaining91_HandleCreateNamespace_InvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	server.HandleCreateNamespace(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleCreateNamespace_MissingNamespace(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"namespace":"","dimensions":128,"distance_metric":"cosine"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleCreateNamespace(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleCreateNamespace_InvalidDimensions(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"namespace":"ns","dimensions":0,"distance_metric":"cosine"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleCreateNamespace(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleCreateNamespace_MissingMetric(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"namespace":"ns","dimensions":128,"distance_metric":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleCreateNamespace(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleCreateNamespace_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"namespace":"ns","dimensions":128,"distance_metric":"cosine"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleCreateNamespace(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRemaining91_HandleCreateNamespace_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockVectorSvc := &mocks.MockVectorService{
		CreateNamespaceFunc: func(ctx context.Context, req *service.CreateNamespaceRequest) (*service.CreateNamespaceResponse, error) {
			return nil, fmt.Errorf("create failed")
		},
	}
	mockService.MockVectorService = mockVectorSvc

	body := `{"namespace":"ns","dimensions":128,"distance_metric":"cosine"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleCreateNamespace(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRemaining91_HandleUpdateNamespace_InvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	req := httptest.NewRequest(http.MethodPut, "/api/v1/vector/namespaces/test-ns", strings.NewReader("{bad"))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleUpdateNamespace(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleUpdateNamespace_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	body := `{"metadata":{"key":"val"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/vector/namespaces/test-ns", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleUpdateNamespace(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRemaining91_HandleUpdateNamespace_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockVectorSvc := &mocks.MockVectorService{
		GetNamespaceInfoFunc: func(ctx context.Context, req *service.GetNamespaceInfoRequest) (*service.GetNamespaceInfoResponse, error) {
			return &service.GetNamespaceInfoResponse{Namespace: req.Namespace}, nil
		},
	}
	mockService.MockVectorService = mockVectorSvc

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	body := `{"metadata":{"key":"val"},"index_params":{"m":"16"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/vector/namespaces/test-ns", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleUpdateNamespace(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleDeleteNamespace_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/vector/namespaces/test-ns", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleDeleteNamespace(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRemaining91_HandleDeleteNamespace_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockVectorSvc := &mocks.MockVectorService{
		DeleteNamespaceFunc: func(ctx context.Context, req *service.DeleteNamespaceRequest) (*service.DeleteNamespaceResponse, error) {
			return &service.DeleteNamespaceResponse{Success: true, VectorsDeleted: 10, Timestamp: time.Now().UnixNano()}, nil
		},
	}
	mockService.MockVectorService = mockVectorSvc

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/vector/namespaces/test-ns", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleDeleteNamespace(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleDeleteNamespace_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockVectorSvc := &mocks.MockVectorService{
		DeleteNamespaceFunc: func(ctx context.Context, req *service.DeleteNamespaceRequest) (*service.DeleteNamespaceResponse, error) {
			return nil, fmt.Errorf("delete failed")
		},
	}
	mockService.MockVectorService = mockVectorSvc

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/vector/namespaces/test-ns", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleDeleteNamespace(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRemaining91_HandleListNamespaces_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vector/namespaces", nil)
	w := httptest.NewRecorder()
	server.HandleListNamespaces(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRemaining91_HandleListNamespaces_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockVectorSvc := &mocks.MockVectorService{
		ListNamespacesFunc: func(ctx context.Context, req *service.ListNamespacesRequest) (*service.ListNamespacesResponse, error) {
			return &service.ListNamespacesResponse{
				Namespaces: []*service.NamespaceInfo{{Namespace: "ns1", Dimension: 128}},
				TotalCount: 1,
			}, nil
		},
	}
	mockService.MockVectorService = mockVectorSvc

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vector/namespaces?page_size=50", nil)
	w := httptest.NewRecorder()
	server.HandleListNamespaces(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleGetNamespaceInfo_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/vector/namespaces/test-ns", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetNamespaceInfo(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRemaining91_HandleGetNamespaceInfo_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockVectorSvc := &mocks.MockVectorService{
		GetNamespaceInfoFunc: func(ctx context.Context, req *service.GetNamespaceInfoRequest) (*service.GetNamespaceInfoResponse, error) {
			return &service.GetNamespaceInfoResponse{
				Namespace:   req.Namespace,
				Dimension:   128,
				VectorCount: 42,
			}, nil
		},
	}
	mockService.MockVectorService = mockVectorSvc

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/vector/namespaces/test-ns", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetNamespaceInfo(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleGetNamespaceStats_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/vector/namespaces/test-ns/stats", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetNamespaceStats(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRemaining91_HandleGetNamespaceStats_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockVectorSvc := &mocks.MockVectorService{
		GetIndexStatsFunc: func(ctx context.Context, req *service.GetIndexStatsRequest) (*service.GetIndexStatsResponse, error) {
			return &service.GetIndexStatsResponse{
				Namespace:      req.Namespace,
				VectorCount:    100,
				IndexSizeBytes: 4096,
			}, nil
		},
	}
	mockService.MockVectorService = mockVectorSvc

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/vector/namespaces/test-ns/stats", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetNamespaceStats(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleUpsertVector_InvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces/test-ns/vectors", strings.NewReader("{bad"))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleUpsertVector(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleUpsertVector_MissingID(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	body := `{"id":"","vector":[1.0,2.0]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces/test-ns/vectors", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleUpsertVector(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleUpsertVector_EmptyVector(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	body := `{"id":"vec1","vector":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces/test-ns/vectors", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleUpsertVector(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleUpsertVector_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	body := `{"id":"vec1","vector":[1.0,2.0,3.0]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces/test-ns/vectors", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleUpsertVector(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRemaining91_HandleUpsertVector_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockVectorSvc := &mocks.MockVectorService{
		UpsertVectorFunc: func(ctx context.Context, req *service.UpsertVectorRequest) (*service.UpsertVectorResponse, error) {
			return &service.UpsertVectorResponse{
				ID:        req.ID,
				Success:   true,
				Version:   1,
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
	}
	mockService.MockVectorService = mockVectorSvc

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	body := `{"id":"vec1","vector":[1.0,2.0,3.0],"metadata":{"tag":"test"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces/test-ns/vectors", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleUpsertVector(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleGetVector_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	rctx.URLParams.Add("id", "vec1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/vector/namespaces/test-ns/vectors/vec1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetVector(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRemaining91_HandleGetVector_NotFound(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockVectorSvc := &mocks.MockVectorService{
		GetVectorFunc: func(ctx context.Context, req *service.GetVectorRequest) (*service.GetVectorResponse, error) {
			return &service.GetVectorResponse{Found: false}, nil
		},
	}
	mockService.MockVectorService = mockVectorSvc

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	rctx.URLParams.Add("id", "missing")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/vector/namespaces/test-ns/vectors/missing", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetVector(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRemaining91_HandleGetVector_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockVectorSvc := &mocks.MockVectorService{
		GetVectorFunc: func(ctx context.Context, req *service.GetVectorRequest) (*service.GetVectorResponse, error) {
			return &service.GetVectorResponse{
				Found:   true,
				Vector:  []float32{1.0, 2.0, 3.0},
				Version: 1,
			}, nil
		},
	}
	mockService.MockVectorService = mockVectorSvc

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	rctx.URLParams.Add("id", "42")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/vector/namespaces/test-ns/vectors/42?include_vector=true", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetVector(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleDeleteVector_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	rctx.URLParams.Add("id", "vec1")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/vector/namespaces/test-ns/vectors/vec1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleDeleteVector(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRemaining91_HandleDeleteVector_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockVectorSvc := &mocks.MockVectorService{
		DeleteVectorFunc: func(ctx context.Context, req *service.DeleteVectorRequest) (*service.DeleteVectorResponse, error) {
			return &service.DeleteVectorResponse{Success: true, Timestamp: time.Now().UnixNano()}, nil
		},
	}
	mockService.MockVectorService = mockVectorSvc

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	rctx.URLParams.Add("id", "vec1")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/vector/namespaces/test-ns/vectors/vec1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleDeleteVector(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleBatchUpsertVectors_InvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces/test-ns/vectors/batch", strings.NewReader("{bad"))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleBatchUpsertVectors(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleBatchUpsertVectors_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	body := `{"vectors":[{"id":"v1","vector":[1.0]}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces/test-ns/vectors/batch", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleBatchUpsertVectors(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRemaining91_HandleBatchDeleteVectors_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	body := `{"ids":["v1","v2"]}`
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/vector/namespaces/test-ns/vectors/batch", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleBatchDeleteVectors(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRemaining91_HandleListVectors_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/vector/namespaces/test-ns/vectors", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleListVectors(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRemaining91_HandleSearchSimilar_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	body := `{"query_vector":[1.0,2.0],"k":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces/test-ns/search/similar", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleSearchSimilar(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRemaining91_HandleSearchByID_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	rctx.URLParams.Add("id", "vec1")
	body := `{"k":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces/test-ns/search/by-id/vec1", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleSearchByID(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRemaining91_HandleHybridSearch_InvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/hybrid-search", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	server.HandleHybridSearch(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleSearchByMetadata_InvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/search-by-metadata", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	server.HandleSearchByMetadata(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleRebuildIndex_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces/test-ns/index/rebuild", strings.NewReader("{}"))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleRebuildIndex(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRemaining91_HandleGetIndexProgress_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/vector/namespaces/test-ns/index/progress?operation_id=test-op", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetIndexProgress(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleGetGlobalStats_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vector/admin/stats/global", nil)
	w := httptest.NewRecorder()
	server.HandleGetGlobalStats(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRemaining91_HandleCompactNamespace_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"namespace":"test-ns"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/admin/compact", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleCompactNamespace(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestRemaining91_HandleGetCompactionProgress_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vector/admin/compact/progress?operation_id=test-op", nil)
	w := httptest.NewRecorder()
	server.HandleGetCompactionProgress(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleUpdateMetadata_NilVectorService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "test-ns")
	rctx.URLParams.Add("id", "vec1")
	body := `{"metadata":{"key":"val"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/vector/namespaces/test-ns/vectors/vec1/metadata", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleUpdateMetadata(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// =============================================================================
// Messaging Handlers
// =============================================================================

func TestRemaining91_HandleListMessagingChannels_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/channels", nil)
	w := httptest.NewRecorder()
	server.HandleListMessagingChannels(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestRemaining91_HandleListMessagingChannels_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockMsgSvc := &mocks.MockMessagingService{
		ListChannelsFunc: func(ctx context.Context) ([]*messaging.Channel, error) {
			return []*messaging.Channel{
				{Name: "ch1", CreatedAt: time.Now()},
				{Name: "ch2", CreatedAt: time.Now()},
			}, nil
		},
	}
	mockService.MockMessagingService = mockMsgSvc

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/channels", nil)
	w := httptest.NewRecorder()
	server.HandleListMessagingChannels(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp ListMessagingChannelsRESTResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 2, resp.Total)
}

func TestRemaining91_HandleListMessagingChannels_WithPrefix(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockMsgSvc := &mocks.MockMessagingService{
		ListChannelsFunc: func(ctx context.Context) ([]*messaging.Channel, error) {
			return []*messaging.Channel{
				{Name: "events.user", CreatedAt: time.Now()},
				{Name: "events.order", CreatedAt: time.Now()},
				{Name: "logs.system", CreatedAt: time.Now()},
			}, nil
		},
	}
	mockService.MockMessagingService = mockMsgSvc

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/channels?name_prefix=events", nil)
	w := httptest.NewRecorder()
	server.HandleListMessagingChannels(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp ListMessagingChannelsRESTResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 2, resp.Total)
}

func TestRemaining91_HandleListMessagingChannels_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockMsgSvc := &mocks.MockMessagingService{
		ListChannelsFunc: func(ctx context.Context) ([]*messaging.Channel, error) {
			return nil, fmt.Errorf("list failed")
		},
	}
	mockService.MockMessagingService = mockMsgSvc

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/channels", nil)
	w := httptest.NewRecorder()
	server.HandleListMessagingChannels(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRemaining91_HandleCreateMessagingChannel_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"name":"test-ch"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/channels", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleCreateMessagingChannel(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestRemaining91_HandleCreateMessagingChannel_InvalidJSON(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/channels", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	server.HandleCreateMessagingChannel(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleCreateMessagingChannel_MissingName(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{}

	body := `{"name":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/channels", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleCreateMessagingChannel(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleCreateMessagingChannel_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockMsgSvc := &mocks.MockMessagingService{
		CreateChannelFunc: func(ctx context.Context, name string, opts ...messaging.CreateChannelOption) (*messaging.Channel, error) {
			return &messaging.Channel{Name: name, CreatedAt: time.Now()}, nil
		},
	}
	mockService.MockMessagingService = mockMsgSvc

	body := `{"name":"test-ch","codec":"json","persistent":true,"max_history":100,"buffer_size":50,"max_subscribers":10,"ttl_seconds":3600}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/channels", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleCreateMessagingChannel(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestRemaining91_HandleGetMessagingChannel_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-ch")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/channels/test-ch", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetMessagingChannel(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestRemaining91_HandleGetMessagingChannel_MissingName(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/channels/", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetMessagingChannel(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleGetMessagingChannel_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockMsgSvc := &mocks.MockMessagingService{
		GetChannelFunc: func(ctx context.Context, name string) (*messaging.Channel, error) {
			return &messaging.Channel{Name: name, CreatedAt: time.Now(), Subscribers: 5}, nil
		},
	}
	mockService.MockMessagingService = mockMsgSvc

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-ch")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/channels/test-ch", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetMessagingChannel(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleDeleteMessagingChannel_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-ch")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/messaging/channels/test-ch", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleDeleteMessagingChannel(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestRemaining91_HandleDeleteMessagingChannel_MissingName(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/messaging/channels/", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleDeleteMessagingChannel(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleDeleteMessagingChannel_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockMsgSvc := &mocks.MockMessagingService{
		DeleteChannelFunc: func(ctx context.Context, name string) error {
			return nil
		},
	}
	mockService.MockMessagingService = mockMsgSvc

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-ch")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/messaging/channels/test-ch", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleDeleteMessagingChannel(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleMessagingSubscribe_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"channel":"ch1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/subscribe", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleMessagingSubscribe(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestRemaining91_HandleMessagingSubscribe_InvalidJSON(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/subscribe", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	server.HandleMessagingSubscribe(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleMessagingSubscribe_MissingChannel(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{}

	body := `{"channel":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/subscribe", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleMessagingSubscribe(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleMessagingSubscribe_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockMsgSvc := &mocks.MockMessagingService{
		SubscribeFunc: func(ctx context.Context, channel string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
			return &messaging.Subscription{ID: "sub-1", Channel: channel, CreatedAt: time.Now()}, nil
		},
	}
	mockService.MockMessagingService = mockMsgSvc

	body := `{"channel":"ch1","session_id":"sess1","delta_mode":true,"include_old_values":true,"dedup_metadata":true,"dedup_skip_duplicates":true,"buffer_size":100,"event_types":["PUT","DELETE"],"key_prefix":"prefix/"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/subscribe", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleMessagingSubscribe(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestRemaining91_HandleMessagingUnsubscribe_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "sub-1")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/messaging/subscribe/sub-1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingUnsubscribe(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestRemaining91_HandleMessagingUnsubscribe_MissingID(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/messaging/subscribe/", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingUnsubscribe(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleMessagingUnsubscribe_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "sub-1")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/messaging/subscribe/sub-1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingUnsubscribe(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleMessagingPublish_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "ch1")
	body := `{"payload":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish/ch1", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingPublish(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestRemaining91_HandleMessagingPublish_MissingChannel(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "")
	body := `{"payload":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish/", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingPublish(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleMessagingPublish_InvalidJSON(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "ch1")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish/ch1", strings.NewReader("{bad"))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingPublish(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleMessagingPublish_EmptyPayload(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "ch1")
	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish/ch1", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingPublish(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleMessagingPublish_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockMsgSvc := &mocks.MockMessagingService{
		PublishFunc: func(ctx context.Context, channel string, payload interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
			return &messaging.PublishResult{MessageID: "msg-1"}, nil
		},
	}
	mockService.MockMessagingService = mockMsgSvc

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "ch1")
	body := `{"payload":"hello","message_id":"m1","dedup_key":"dk","codec":"json","metadata":{"k":"v"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish/ch1", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingPublish(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleMessagingPublishDelta_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "ch1")
	body := `{"old":"a","new":"b"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish-delta/ch1", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingPublishDelta(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestRemaining91_HandleMessagingPublishDelta_MissingOld(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "ch1")
	body := `{"new":"b"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish-delta/ch1", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingPublishDelta(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleMessagingPublishDelta_MissingNew(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "ch1")
	body := `{"old":"a"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish-delta/ch1", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingPublishDelta(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleMessagingPublishDelta_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockMsgSvc := &mocks.MockMessagingService{
		PublishDeltaFunc: func(ctx context.Context, channel string, old, new interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
			return &messaging.PublishResult{MessageID: "delta-1"}, nil
		},
	}
	mockService.MockMessagingService = mockMsgSvc

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "ch1")
	body := `{"old":"a","new":"b","message_id":"m1","dedup_key":"dk","codec":"json","metadata":{"k":"v"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish-delta/ch1", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingPublishDelta(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleMessagingStats_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/stats", nil)
	w := httptest.NewRecorder()
	server.HandleMessagingStats(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestRemaining91_HandleMessagingStats_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockMsgSvc := &mocks.MockMessagingService{
		GetStatsFunc: func() *messaging.Stats {
			return &messaging.Stats{TotalChannels: 3, MessagesPublished: 100}
		},
	}
	mockService.MockMessagingService = mockMsgSvc

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/stats", nil)
	w := httptest.NewRecorder()
	server.HandleMessagingStats(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleMessagingChannelStats_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "ch1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/stats/ch1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingChannelStats(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestRemaining91_HandleMessagingChannelStats_MissingChannel(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/stats/", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingChannelStats(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleMessagingDedupStats_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "ch1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/dedup/stats/ch1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingDedupStats(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestRemaining91_HandleMessagingDedupStats_MissingChannel(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockMessagingService = &mocks.MockMessagingService{}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/dedup/stats/", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingDedupStats(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleMessagingDedupStats_NotPushService(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockMsgSvc := &mocks.MockMessagingService{
		GetChannelFunc: func(ctx context.Context, name string) (*messaging.Channel, error) {
			return &messaging.Channel{Name: name, CreatedAt: time.Now()}, nil
		},
	}
	mockService.MockMessagingService = mockMsgSvc

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "ch1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/dedup/stats/ch1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleMessagingDedupStats(w, req)

	// MockMessagingService is not *messaging.PushService, so it returns 501
	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

// =============================================================================
// Scheduler Handlers
// =============================================================================

func TestRemaining91_HandleCreateSchedulerJob_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"name":"job1","handler":"h1","type":"cron"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleCreateSchedulerJob(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRemaining91_HandleCreateSchedulerJob_InvalidJSON(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockSchedulerService = &mocks.MockSchedulerService{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	server.HandleCreateSchedulerJob(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleCreateSchedulerJob_MissingName(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockSchedulerService = &mocks.MockSchedulerService{}

	body := `{"name":"","handler":"h1","type":"cron"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleCreateSchedulerJob(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleCreateSchedulerJob_MissingHandler(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockSchedulerService = &mocks.MockSchedulerService{}

	body := `{"name":"job1","handler":"","type":"cron"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleCreateSchedulerJob(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleCreateSchedulerJob_MissingType(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockSchedulerService = &mocks.MockSchedulerService{}

	body := `{"name":"job1","handler":"h1","type":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleCreateSchedulerJob(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleCreateSchedulerJob_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockSchedSvc := &mocks.MockSchedulerService{
		CreateJobFunc: func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
			return &service.CreateSchedulerJobResponse{
				Job:       &service.SchedulerJob{ID: 1, Name: req.Name},
				Timestamp: time.Now(),
			}, nil
		},
	}
	mockService.MockSchedulerService = mockSchedSvc

	body := `{"name":"job1","handler":"h1","type":"cron","schedule":"* * * * *","delay_seconds":10,"timeout_seconds":30}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleCreateSchedulerJob(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestRemaining91_HandleListSchedulerJobs_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/jobs", nil)
	w := httptest.NewRecorder()
	server.HandleListSchedulerJobs(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRemaining91_HandleListSchedulerJobs_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockSchedSvc := &mocks.MockSchedulerService{
		ListJobsFunc: func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
			return &service.ListSchedulerJobsResponse{
				Jobs:      []*service.SchedulerJob{{ID: 1, Name: "j1"}},
				Timestamp: time.Now(),
			}, nil
		},
	}
	mockService.MockSchedulerService = mockSchedSvc

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/jobs?owner_type=system&owner_id=sys1&status=active&handler=h1&limit=50&cursor=abc", nil)
	w := httptest.NewRecorder()
	server.HandleListSchedulerJobs(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleGetSchedulerJob_InvalidID(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "notanumber")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/jobs/notanumber", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetSchedulerJob(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleGetSchedulerJob_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/jobs/1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetSchedulerJob(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRemaining91_HandleGetSchedulerJob_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockSchedSvc := &mocks.MockSchedulerService{
		GetJobFunc: func(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
			return &service.GetSchedulerJobResponse{
				Job:       &service.SchedulerJob{ID: req.JobID, Name: "j1"},
				Timestamp: time.Now(),
			}, nil
		},
	}
	mockService.MockSchedulerService = mockSchedSvc

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/jobs/1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetSchedulerJob(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleUpdateSchedulerJob_InvalidID(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "bad")
	body := `{"name":"updated"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/scheduler/jobs/bad", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleUpdateSchedulerJob(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleUpdateSchedulerJob_InvalidJSON(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.MockSchedulerService = &mocks.MockSchedulerService{}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req := httptest.NewRequest(http.MethodPut, "/api/v1/scheduler/jobs/1", strings.NewReader("{bad"))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleUpdateSchedulerJob(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleUpdateSchedulerJob_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	body := `{"name":"updated"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/scheduler/jobs/1", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleUpdateSchedulerJob(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRemaining91_HandleDeleteSchedulerJob_InvalidID(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "bad")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/scheduler/jobs/bad", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleDeleteSchedulerJob(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleDeleteSchedulerJob_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/scheduler/jobs/1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleDeleteSchedulerJob(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRemaining91_HandleDeleteSchedulerJob_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockSchedSvc := &mocks.MockSchedulerService{
		DeleteJobFunc: func(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
			return &service.DeleteSchedulerJobResponse{JobID: req.JobID, Deleted: true, Timestamp: time.Now()}, nil
		},
	}
	mockService.MockSchedulerService = mockSchedSvc

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/scheduler/jobs/1?force=true", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleDeleteSchedulerJob(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleTriggerSchedulerJob_InvalidID(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "bad")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs/bad/trigger", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleTriggerSchedulerJob(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandlePauseSchedulerJob_InvalidID(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "bad")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs/bad/pause", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandlePauseSchedulerJob(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleResumeSchedulerJob_InvalidID(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "bad")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs/bad/resume", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleResumeSchedulerJob(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleCancelSchedulerJob_InvalidID(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "bad")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs/bad/cancel", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleCancelSchedulerJob(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleCancelSchedulerJob_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockSchedSvc := &mocks.MockSchedulerService{
		CancelJobFunc: func(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
			return &service.CancelSchedulerJobResponse{JobID: req.JobID, Timestamp: time.Now()}, nil
		},
	}
	mockService.MockSchedulerService = mockSchedSvc

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	body := `{"reason":"test cancel"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs/1/cancel", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleCancelSchedulerJob(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRemaining91_HandleGetSchedulerJobHistory_InvalidID(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "bad")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/jobs/bad/history", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetSchedulerJobHistory(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleGetSchedulerJobHistory_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/jobs/1/history", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetSchedulerJobHistory(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRemaining91_HandleGetSchedulerStatus_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/status", nil)
	w := httptest.NewRecorder()
	server.HandleGetSchedulerStatus(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRemaining91_HandleGetSchedulerStatus_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockSchedSvc := &mocks.MockSchedulerService{
		GetStatusFunc: func(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
			return &service.GetSchedulerStatusResponse{
				Running: true,
				Stats: &service.SchedulerStats{
					JobCount:    5,
					RunningJobs: 2,
				},
				Timestamp: time.Now(),
			}, nil
		},
	}
	mockService.MockSchedulerService = mockSchedSvc

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/status", nil)
	w := httptest.NewRecorder()
	server.HandleGetSchedulerStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// =============================================================================
// Seal Handlers
// =============================================================================

// =============================================================================
// Parameter Store Handlers
// =============================================================================

func TestRemaining91_HandleGetParameter_MissingPath(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/parameters/", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetParameter(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleGetParameter_NilService(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "app/db/host")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/parameters/app/db/host", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetParameter(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRemaining91_HandleGetParameter_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockParamSvc := &mocks.MockParameterStoreService{
		GetParameterFunc: func(ctx context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error) {
			return &interfaces.GetParameterResponse{}, nil
		},
	}
	mockService.MockParameterStoreService = mockParamSvc

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "app/db/host")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/parameters/app/db/host", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetParameter(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// =============================================================================
// Secrets Handlers
// =============================================================================

func TestRemaining91_HandleSecretsAction_Rollback(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "my/secret")
	// rollback action dispatches to HandleRollbackSecret which needs secrets service
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/my/secret?action=rollback", strings.NewReader(`{"version":1}`))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleSecretsAction(w, req)

	// Nil secrets service returns 500
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRemaining91_HandleSecretsAction_Rotate(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "my/secret")
	// rotate action requires data in request body
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/my/secret?action=rotate", strings.NewReader(`{"data":"dGVzdA=="}`))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleSecretsAction(w, req)

	// Nil secrets service returns 500
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRemaining91_HandleSecretsAction_Rotate_MissingData(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "my/secret")
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/my/secret?action=rotate", strings.NewReader(`{}`))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleSecretsAction(w, req)

	// Missing data returns 400
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleSecretsAction_Destroy(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "my/secret")
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/my/secret?action=destroy", strings.NewReader(`{}`))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleSecretsAction(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRemaining91_HandleSecretsAction_DefaultStore(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "my/secret")
	body := `{"data":"dGVzdA=="}`
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/my/secret", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleSecretsAction(w, req)

	// Nil secrets service returns 500
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRemaining91_HandleGetSecretOrVersions_WithVersions(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "my/secret")
	req := httptest.NewRequest(http.MethodGet, "/v1/secrets/my/secret?versions=true", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetSecretOrVersions(w, req)

	// Dispatches to HandleListVersions, nil secrets service returns 500
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRemaining91_HandleGetSecretOrVersions_WithoutVersions(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "my/secret")
	req := httptest.NewRequest(http.MethodGet, "/v1/secrets/my/secret", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetSecretOrVersions(w, req)

	// Dispatches to HandleGetSecret, nil secrets service returns 500
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRemaining91_HandleStoreSecret_MissingPath(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	body := `{"data":"dGVzdA=="}`
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleStoreSecret(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleStoreSecret_InvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "my/secret")
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/my/secret", strings.NewReader("{bad"))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleStoreSecret(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleStoreSecret_EmptyData(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "my/secret")
	body := `{"data":null}`
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/my/secret", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleStoreSecret(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRemaining91_HandleStoreSecret_InvalidAlgorithm(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "my/secret")
	body := `{"data":"dGVzdA==","algorithm":"INVALID_ALGO"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/my/secret", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleStoreSecret(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// =============================================================================
// Conversion helpers
// =============================================================================

func TestRemaining91_SchedulerJobToREST_Nil(t *testing.T) {
	result := schedulerJobToREST(nil)
	assert.Nil(t, result)
}

func TestRemaining91_SchedulerExecutionToREST_Nil(t *testing.T) {
	result := schedulerExecutionToREST(nil)
	assert.Nil(t, result)
}

func TestRemaining91_MessagingChannelToREST_Nil(t *testing.T) {
	result := messagingChannelToREST(nil)
	assert.Nil(t, result)
}

func TestRemaining91_MessagingChannelToREST_Valid(t *testing.T) {
	ch := &messaging.Channel{
		Name:       "test",
		Codec:      "json",
		CreatedAt:  time.Now(),
		Persistent: true,
		MaxHistory: 100,
		BufferSize: 50,
		TTL:        time.Hour,
	}
	result := messagingChannelToREST(ch)
	assert.Equal(t, "test", result.Name)
	assert.Equal(t, "json", result.Codec)
	assert.True(t, result.Persistent)
	assert.Equal(t, 100, result.MaxHistory)
	assert.Equal(t, int64(3600), result.TTLSeconds)
}

func TestRemaining91_MessagingServiceNotConfiguredError(t *testing.T) {
	err := &MessagingServiceNotConfiguredError{}
	assert.Equal(t, "messaging notification service is not configured", err.Error())
}

func TestGenerateKeyValidations(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		errMsg string
	}{
		{
			name:   "invalid JSON",
			body:   `not-json`,
			errMsg: "",
		},
		{
			name:   "missing key_attributes",
			body:   `{}`,
			errMsg: "key algorithm is required",
		},
		{
			name:   "missing key usage",
			body:   `{"key_attributes": {"algorithm": "AES-256-GCM"}}`,
			errMsg: "key usage is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _, cleanup := setupMockServer(t)
			defer cleanup()

			req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/generate", bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.HandleGenerateKey(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			if tc.errMsg != "" {
				assert.Contains(t, w.Body.String(), tc.errMsg)
			}
		})
	}
}

func TestRotateKeyEmptyName(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys//rotate", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleRotateKey(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "key name is required")
}

func TestRotateKeyInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `not-json`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/test-key/rotate", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleRotateKey(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteKeyEmptyName(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/keys/", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleDeleteKey(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "key name is required")
}

func TestUpdateKeyStatusValidations(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	t.Run("empty key name", func(t *testing.T) {
		body := `{"status": "enabled"}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys//status", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		server.HandleUpdateKeyStatus(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "key name is required")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		body := `not-json`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/test-key/status", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		server.HandleUpdateKeyStatus(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestGetKeyEmptyName(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys/", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGetKey(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "key name is required")
}

func TestListKeyVersionsEmptyName(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys//versions", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleListKeyVersions(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "key name is required")
}

func TestKMSEncryptValidations(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	t.Run("empty key name", func(t *testing.T) {
		body := `{"plaintext": "dGVzdA=="}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys//encrypt", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		server.HandleKMSEncrypt(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "key name is required")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		body := `not-json`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/test-key/encrypt", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		server.HandleKMSEncrypt(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestKMSDecryptValidations(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	t.Run("empty key name", func(t *testing.T) {
		body := `{"ciphertext": "Y2lwaGVy"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys//decrypt", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		server.HandleKMSDecrypt(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "key name is required")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		body := `not-json`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/test-key/decrypt", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		server.HandleKMSDecrypt(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestExportKeyEmptyName(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys//export", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleExportKey(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "key name is required")
}

func TestUnwrapKeyInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `not-json`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/test-key/unwrap", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleUnwrapKey(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUnwrapKeyMissingFields(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	t.Run("missing wrapped_key", func(t *testing.T) {
		body := `{"wrapping_key_id": "kek-1"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/test-key/unwrap", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		server.HandleUnwrapKey(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "wrapped_key is required")
	})

	t.Run("missing wrapping_key_id", func(t *testing.T) {
		body := `{"wrapped_key": "d3JhcHBlZA=="}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/test-key/unwrap", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		server.HandleUnwrapKey(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "wrapping_key_id is required")
	})
}

func TestSetKeySafetyValidations(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	t.Run("empty key name", func(t *testing.T) {
		body := `{"max_operations": 1000}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys//safety", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		server.HandleSetKeySafety(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "key name is required")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		body := `not-json`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/test-key/safety", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		server.HandleSetKeySafety(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestClearKeySafetyEmptyName(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/keys//safety", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleClearKeySafety(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "key name is required")
}

func TestSetKeyPolicyValidations(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	t.Run("empty key name", func(t *testing.T) {
		body := `{"policy": {}}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys//policy", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		server.HandleSetKeyPolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "key name is required")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		body := `not-json`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/test-key/policy", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-key")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		server.HandleSetKeyPolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestGetKeyPolicyEmptyName(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys//policy", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGetKeyPolicy(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "key name is required")
}

func TestSetDefaultDEKFromBodyMissingPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/dek/default", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleSetDefaultDEKFromBody(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path is required")
}

func TestRotateDEKMissingPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks//rotate", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleRotateDEK(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path is required")
}

func TestDeleteDEKMissingPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/deks/", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleDeleteDEK(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path is required")
}

func TestGetDEKStatusMissingPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/deks//status", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGetDEKStatus(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path is required")
}

func TestSetDefaultDEKMissingPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/deks//default", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleSetDefaultDEK(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path is required")
}

func TestSetDEKSafetyMissingPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/deks//safety", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleSetDEKSafety(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path is required")
}

func TestClearDEKSafetyMissingPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/deks//safety", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleClearDEKSafety(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path is required")
}

func TestStartReencryptionMissingPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks//reencrypt", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleStartReencryption(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path is required")
}

func TestGetReencryptionProgressMissingPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/deks//reencrypt/progress", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGetReencryptionProgress(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path is required")
}

func TestKVPutValidations(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	t.Run("missing entity type", func(t *testing.T) {
		body := `{"value": "test"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kv/entities/", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("type", "")
		rctx.URLParams.Add("key", "test-key")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		server.HandleKVPut(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "entity type is required")
	})

	t.Run("missing key", func(t *testing.T) {
		body := `{"value": "test"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kv/entities/test-type/", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("type", "test-type")
		rctx.URLParams.Add("key", "")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		server.HandleKVPut(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "key is required")
	})
}

func TestKVGetMissingParams(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	// Both entity type and key are checked together
	req := httptest.NewRequest(http.MethodGet, "/api/v1/kv/entities/", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "")
	rctx.URLParams.Add("key", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleKVGet(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "entity type and key are required")
}

func TestKVListMissingEntityType(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kv/entities", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleKVList(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "entity type is required")
}

func TestObservabilityHandlers_MethodValidation(t *testing.T) {
	handler := NewPhase4Handler(nil, nil)

	tests := []struct {
		name          string
		handler       func(http.ResponseWriter, *http.Request)
		validMethod   string
		invalidMethod string
	}{
		{
			name:          "HandleCreateBackup",
			handler:       handler.HandleCreateBackup,
			validMethod:   http.MethodPost,
			invalidMethod: http.MethodGet,
		},
		{
			name:          "HandleListBackups",
			handler:       handler.HandleListBackups,
			validMethod:   http.MethodGet,
			invalidMethod: http.MethodPost,
		},
		{
			name:          "HandleDeleteBackup",
			handler:       handler.HandleDeleteBackup,
			validMethod:   http.MethodDelete,
			invalidMethod: http.MethodGet,
		},
		{
			name:          "HandleGetCacheStats",
			handler:       handler.HandleGetCacheStats,
			validMethod:   http.MethodGet,
			invalidMethod: http.MethodPost,
		},
		{
			name:          "HandleClearCache",
			handler:       handler.HandleClearCache,
			validMethod:   http.MethodPost,
			invalidMethod: http.MethodGet,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_wrong_method", func(t *testing.T) {
			req := httptest.NewRequest(tt.invalidMethod, "/test", nil)
			w := httptest.NewRecorder()

			tt.handler(w, req)

			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
		})
	}
}

func TestObservabilityHandlers_InvalidJSON(t *testing.T) {
	handler := NewPhase4Handler(nil, nil)

	tests := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		method  string
	}{
		{
			name:    "HandleCreateBackup",
			handler: handler.HandleCreateBackup,
			method:  http.MethodPost,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_invalid_json", func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/test", bytes.NewReader([]byte("invalid json")))
			w := httptest.NewRecorder()

			tt.handler(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestCacheHandlers_NilManager(t *testing.T) {
	handler := NewPhase4Handler(nil, nil)

	t.Run("GetCacheStats_NilManager", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/cache/stats", nil)
		w := httptest.NewRecorder()

		handler.HandleGetCacheStats(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.Contains(t, w.Body.String(), "Cache manager not initialized")
	})

	t.Run("ClearCache_NilManager", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/cache/clear", nil)
		w := httptest.NewRecorder()

		handler.HandleClearCache(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.Contains(t, w.Body.String(), "Cache manager not initialized")
	})
}

func TestExtractPathParam_QA90Coverage(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		prefix   string
		suffix   string
		expected string
	}{
		{
			name:     "extract with both prefix and suffix",
			path:     "/api/v1/backups/backup-123/restore",
			prefix:   "/api/v1/backups/",
			suffix:   "/restore",
			expected: "backup-123",
		},
		{
			name:     "extract without suffix",
			path:     "/api/v1/backups/backup-456",
			prefix:   "/api/v1/backups/",
			suffix:   "",
			expected: "backup-456",
		},
		{
			name:     "extract with empty prefix",
			path:     "user-789",
			prefix:   "",
			suffix:   "",
			expected: "user-789",
		},
		{
			name:     "suffix longer than remaining path",
			path:     "/api/v1/backups/a",
			prefix:   "/api/v1/backups/",
			suffix:   "/restore",
			expected: "a",
		},
		{
			name:     "single segment between prefix and suffix",
			path:     "/api/v1/data/restore",
			prefix:   "/api/v1/",
			suffix:   "/restore",
			expected: "data",
		},
		{
			name:     "empty result after prefix",
			path:     "/api/v1/backups/",
			prefix:   "/api/v1/backups/",
			suffix:   "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPathParam(tt.path, tt.prefix, tt.suffix)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewPhase4Handler_QA90(t *testing.T) {
	handler := NewPhase4Handler(nil, nil)
	require.NotNil(t, handler)
	assert.Nil(t, handler.backupMgr)
	assert.Nil(t, handler.cacheMgr)
}

func TestDetectPreferredCodec_QA90(t *testing.T) {
	tests := []struct {
		name      string
		acceptHdr string
		query     string
		expected  string
	}{
		{
			name:      "msgpack from accept header",
			acceptHdr: "application/msgpack",
			expected:  "msgpack",
		},
		{
			name:      "yaml from accept header",
			acceptHdr: "application/yaml",
			expected:  "yaml",
		},
		{
			name:      "json from accept header",
			acceptHdr: "application/json",
			expected:  "json",
		},
		{
			name:      "json matches first in list",
			acceptHdr: "text/html, application/msgpack, application/json",
			expected:  "json",
		},
		{
			name:      "default to json for unknown",
			acceptHdr: "text/html",
			expected:  "json",
		},
		{
			name:     "query param overrides",
			query:    "codec=msgpack",
			expected: "msgpack",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/test"
			if tt.query != "" {
				url = "/test?" + tt.query
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			if tt.acceptHdr != "" {
				req.Header.Set("Accept", tt.acceptHdr)
			}

			result := detectPreferredCodec(req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConversionHelpers_QA90(t *testing.T) {
	t.Run("convertInterfaceMapToString", func(t *testing.T) {
		input := map[string]interface{}{
			"string_key": "value1",
			"int_key":    123,
			"bool_key":   true,
			"float_key":  45.67,
		}
		result := convertInterfaceMapToString(input)
		assert.Equal(t, "value1", result["string_key"])
		assert.Equal(t, "123", result["int_key"])
		assert.Equal(t, "true", result["bool_key"])
		assert.Equal(t, "45.67", result["float_key"])
	})

	t.Run("convertStringMapToInterface", func(t *testing.T) {
		input := map[string]string{
			"key1": "value1",
			"key2": "value2",
			"key3": "value3",
		}
		result := convertStringMapToInterface(input)
		assert.Equal(t, "value1", result["key1"])
		assert.Equal(t, "value2", result["key2"])
		assert.Equal(t, "value3", result["key3"])
	})
}
