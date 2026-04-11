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

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleKVPut(t *testing.T) {
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
			name:   "put with string key",
			method: http.MethodPut,
			path:   "/api/v1/kv/test_entity/test-key-xxx",
			body:   json.RawMessage(`"test-value-data"`),
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Put", mock.Anything, mock.Anything).Return(&service.PutResponse{
					ID:        12345,
					Version:   1,
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp KVPutResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "test_entity", resp.EntityType)
				assert.Equal(t, "test-key-xxx", resp.Key)
				assert.True(t, resp.Success)
			},
		},
		{
			name:   "put with POST method",
			method: http.MethodPost,
			path:   "/api/v1/kv/users/john-doe",
			body:   json.RawMessage(`{"name":"John Doe","email":"john@example.com"}`),
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Put", mock.Anything, mock.Anything).Return(&service.PutResponse{
					ID:        54321,
					Version:   1,
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp KVPutResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "users", resp.EntityType)
				assert.Equal(t, "john-doe", resp.Key)
				assert.True(t, resp.Success)
			},
		},
		{
			name:           "missing key parameter",
			method:         http.MethodPut,
			path:           "/api/v1/kv/users/",
			body:           json.RawMessage(`"value"`),
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusMethodNotAllowed, // chi returns 405 for trailing slash mismatch
		},
		{
			name:           "missing type parameter",
			method:         http.MethodPut,
			path:           "/api/v1/kv/",
			body:           json.RawMessage(`"value"`),
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock(mockService)

			var reqBody []byte
			if tt.body != nil {
				var err error
				reqBody, err = json.Marshal(tt.body)
				require.NoError(t, err)
			}

			req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(context.Background())

			w := httptest.NewRecorder()
			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "unexpected status code: %s", w.Body.String())

			if tt.checkResponse != nil && w.Code == tt.expectedStatus {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleKVGet(t *testing.T) {
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
			name: "get existing key",
			path: "/api/v1/kv/test_entity/my-key",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				// The data is stored as kvDataWrapper
				wrapper := kvDataWrapper{
					Key:   "my-key",
					Value: json.RawMessage(`"hello world"`),
				}
				wrappedData, _ := json.Marshal(wrapper)
				m.On("Get", mock.Anything, mock.Anything).Return(&service.GetResponse{
					ID:        12345,
					Data:      wrappedData,
					Found:     true,
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp KVGetResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "test_entity", resp.EntityType)
				assert.Equal(t, "my-key", resp.Key)
				assert.True(t, resp.Found)
				// Check the value was unwrapped correctly
				var value string
				err = json.Unmarshal(resp.Value, &value)
				require.NoError(t, err)
				assert.Equal(t, "hello world", value)
			},
		},
		{
			name: "get non-existent key",
			path: "/api/v1/kv/test_entity/missing-key",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Get", mock.Anything, mock.Anything).Return(&service.GetResponse{
					Found: false,
				}, nil).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock(mockService)

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req = req.WithContext(context.Background())

			w := httptest.NewRecorder()
			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "unexpected status code: %s", w.Body.String())

			if tt.checkResponse != nil && w.Code == tt.expectedStatus {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleKVDelete(t *testing.T) {
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
			name: "delete existing key",
			path: "/api/v1/kv/test_entity/my-key",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Delete", mock.Anything, mock.Anything).Return(&service.DeleteResponse{
					Deleted:   true,
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp KVDeleteResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "test_entity", resp.EntityType)
				assert.Equal(t, "my-key", resp.Key)
				assert.True(t, resp.Deleted)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock(mockService)

			req := httptest.NewRequest(http.MethodDelete, tt.path, nil)
			req = req.WithContext(context.Background())

			w := httptest.NewRecorder()
			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "unexpected status code: %s", w.Body.String())

			if tt.checkResponse != nil && w.Code == tt.expectedStatus {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleKVList(t *testing.T) {
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
			name: "list keys",
			path: "/api/v1/kv/test_entity",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				// Create wrapped data for two entries
				wrapper1 := kvDataWrapper{Key: "key1", Value: json.RawMessage(`"value1"`)}
				wrapper2 := kvDataWrapper{Key: "key2", Value: json.RawMessage(`"value2"`)}
				data1, _ := json.Marshal(wrapper1)
				data2, _ := json.Marshal(wrapper2)

				m.On("List", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
					// The channel is chan<- *service.ListResponse (send-only)
					stream := args.Get(2).(chan<- *service.ListResponse)
					stream <- &service.ListResponse{
						Entities: []*service.Entity{
							{ID: 1, Data: data1, Timestamp: 12345},
							{ID: 2, Data: data2, Timestamp: 12346},
						},
						TotalCount:    2,
						NextPageToken: "",
					}
					// Note: We cannot close the channel here since it's send-only
					// The handler will handle channel closure
				}).Return(nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp KVListResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "test_entity", resp.EntityType)
				assert.Len(t, resp.Items, 2)
				assert.Equal(t, "key1", resp.Items[0].Key)
				assert.Equal(t, "key2", resp.Items[1].Key)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock(mockService)

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req = req.WithContext(context.Background())

			w := httptest.NewRecorder()
			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "unexpected status code: %s", w.Body.String())

			if tt.checkResponse != nil && w.Code == tt.expectedStatus {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleKVPutErrorHandling(t *testing.T) {
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
			name:   "service error during put",
			method: http.MethodPut,
			path:   "/api/v1/kv/test_entity/error-key",
			body:   json.RawMessage(`"test-value"`),
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Put", mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:   "empty value is allowed",
			method: http.MethodPut,
			path:   "/api/v1/kv/test_entity/empty-key",
			body:   nil,
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Put", mock.Anything, mock.Anything).Return(&service.PutResponse{
					ID:        12345,
					Version:   1,
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock(mockService)

			var reqBody []byte
			if tt.body != nil {
				var err error
				reqBody, err = json.Marshal(tt.body)
				require.NoError(t, err)
			}

			req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(context.Background())

			w := httptest.NewRecorder()
			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "unexpected status code: %s", w.Body.String())

			if tt.checkResponse != nil && w.Code == tt.expectedStatus {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleKVPut_FullCoverage(t *testing.T) {
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
			name:   "put with structured JSON request body",
			method: http.MethodPut,
			path:   "/api/v1/kv/users/john",
			body: KVPutRequest{
				Value:            json.RawMessage(`{"name":"John","age":30}`),
				ConsistencyLevel: "linearizable",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Put", mock.Anything, mock.Anything).Return(&service.PutResponse{
					ID:        12345,
					Version:   1,
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp KVPutResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "users", resp.EntityType)
				assert.Equal(t, "john", resp.Key)
				assert.True(t, resp.Success)
			},
		},
		{
			name:   "put with URL-encoded key",
			method: http.MethodPut,
			path:   "/api/v1/kv/config/app%2Fsettings",
			body:   json.RawMessage(`"value"`),
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Put", mock.Anything, mock.Anything).Return(&service.PutResponse{
					ID:        12345,
					Version:   1,
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp KVPutResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "app/settings", resp.Key)
			},
		},
		{
			name:   "put with invalid consistency level",
			method: http.MethodPut,
			path:   "/api/v1/kv/users/test",
			body: KVPutRequest{
				Value:            json.RawMessage(`"data"`),
				ConsistencyLevel: "invalid-level",
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Contains(t, resp.Error, "consistency")
			},
		},
		{
			name:   "put with service returning not found error",
			method: http.MethodPut,
			path:   "/api/v1/kv/users/missing",
			body:   json.RawMessage(`"value"`),
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Put", mock.Anything, mock.Anything).Return(nil, &errors.QBError{Code: errors.ErrNotFound, Op: "test", Err: &errors.MessageError{Message: "entity not found"}}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name:   "put with shard header exposed",
			method: http.MethodPut,
			path:   "/api/v1/kv/users/user1",
			body:   json.RawMessage(`"value"`),
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Put", mock.Anything, mock.Anything).Return(&service.PutResponse{
					ID:        12345,
					Version:   1,
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			// Enable shard header for some tests
			if tt.name == "put with shard header exposed" {
				server.config.ExposeShardHeader = true
			}

			tt.setupMock(mockService)

			var reqBody []byte
			if tt.body != nil {
				var err error
				reqBody, err = json.Marshal(tt.body)
				require.NoError(t, err)
			}

			req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(context.Background())

			w := httptest.NewRecorder()
			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "unexpected status code: %s", w.Body.String())

			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleKVGet_FullCoverage(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "get with consistency query parameter",
			path: "/api/v1/kv/users/john?consistency=linearizable",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				wrapper := kvDataWrapper{Key: "john", Value: json.RawMessage(`"john-data"`)}
				wrappedData, _ := json.Marshal(wrapper)
				m.On("Get", mock.Anything, mock.Anything).Return(&service.GetResponse{
					ID:        12345,
					Data:      wrappedData,
					Found:     true,
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp KVGetResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "john", resp.Key)
				assert.True(t, resp.Found)
			},
		},
		{
			name: "get with invalid consistency parameter",
			path: "/api/v1/kv/users/john?consistency=invalid",
			setupMock: func(m *testutil.MockQuicBaaS) {
				// No mock needed - validation fails before service call
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "get with URL-encoded key",
			path: "/api/v1/kv/config/app%2Fsettings%2Fdb",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				wrapper := kvDataWrapper{Key: "app/settings/db", Value: json.RawMessage(`"db-config"`)}
				wrappedData, _ := json.Marshal(wrapper)
				m.On("Get", mock.Anything, mock.Anything).Return(&service.GetResponse{
					ID:        12345,
					Data:      wrappedData,
					Found:     true,
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp KVGetResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "app/settings/db", resp.Key)
			},
		},
		{
			name: "get returns raw data when wrapper unmarshal fails",
			path: "/api/v1/kv/users/raw",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				// Return data that is not a valid kvDataWrapper
				m.On("Get", mock.Anything, mock.Anything).Return(&service.GetResponse{
					ID:        12345,
					Data:      []byte(`"raw-string-data"`),
					Found:     true,
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp KVGetResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.True(t, resp.Found)
			},
		},
		{
			name: "get with service error",
			path: "/api/v1/kv/users/error",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Get", mock.Anything, mock.Anything).Return(nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: &errors.MessageError{Message: "database error"}}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "get with shard header exposed",
			path: "/api/v1/kv/users/test",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				wrapper := kvDataWrapper{Key: "test", Value: json.RawMessage(`"test-data"`)}
				wrappedData, _ := json.Marshal(wrapper)
				m.On("Get", mock.Anything, mock.Anything).Return(&service.GetResponse{
					ID:        12345,
					Data:      wrappedData,
					Found:     true,
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			// Enable shard header for some tests
			if tt.name == "get with shard header exposed" {
				server.config.ExposeShardHeader = true
			}

			tt.setupMock(mockService)

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req = req.WithContext(context.Background())

			w := httptest.NewRecorder()
			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "unexpected status code: %s", w.Body.String())

			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleKVDelete_FullCoverage(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "delete with URL-encoded key",
			path: "/api/v1/kv/config/app%2Fsettings",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Delete", mock.Anything, mock.Anything).Return(&service.DeleteResponse{
					Deleted:   true,
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp KVDeleteResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "app/settings", resp.Key)
				assert.True(t, resp.Deleted)
			},
		},
		{
			name: "delete with service error",
			path: "/api/v1/kv/users/error-key",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Delete", mock.Anything, mock.Anything).Return(nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: &errors.MessageError{Message: "delete failed"}}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "delete with shard header exposed",
			path: "/api/v1/kv/users/test",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("Delete", mock.Anything, mock.Anything).Return(&service.DeleteResponse{
					Deleted:   true,
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			// Enable shard header for some tests
			if tt.name == "delete with shard header exposed" {
				server.config.ExposeShardHeader = true
			}

			tt.setupMock(mockService)

			req := httptest.NewRequest(http.MethodDelete, tt.path, nil)
			req = req.WithContext(context.Background())

			w := httptest.NewRecorder()
			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "unexpected status code: %s", w.Body.String())

			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleKVList_FullCoverage(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "list with limit parameter",
			path: "/api/v1/kv/users?limit=50",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				wrapper := kvDataWrapper{Key: "user1", Value: json.RawMessage(`"data1"`)}
				data, _ := json.Marshal(wrapper)

				m.On("List", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
					stream := args.Get(2).(chan<- *service.ListResponse)
					stream <- &service.ListResponse{
						Entities: []*service.Entity{
							{ID: 1, Data: data, Timestamp: 12345},
						},
						TotalCount:    1,
						NextPageToken: "",
					}
				}).Return(nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp KVListResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "users", resp.EntityType)
				assert.Len(t, resp.Items, 1)
			},
		},
		{
			name:           "list with invalid limit - zero",
			path:           "/api/v1/kv/users?limit=0",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "list with invalid limit - too high",
			path:           "/api/v1/kv/users?limit=5000",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "list with invalid limit - not a number",
			path:           "/api/v1/kv/users?limit=abc",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "list with cursor parameter",
			path: "/api/v1/kv/users?cursor=next-page-token",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				wrapper := kvDataWrapper{Key: "user2", Value: json.RawMessage(`"data2"`)}
				data, _ := json.Marshal(wrapper)

				m.On("List", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
					stream := args.Get(2).(chan<- *service.ListResponse)
					stream <- &service.ListResponse{
						Entities: []*service.Entity{
							{ID: 2, Data: data, Timestamp: 12346},
						},
						TotalCount:    1,
						NextPageToken: "another-token",
					}
				}).Return(nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp KVListResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "another-token", resp.NextCursor)
				assert.True(t, resp.HasMore)
			},
		},
		{
			name: "list with consistency parameter",
			path: "/api/v1/kv/users?consistency=strong",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("List", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
					stream := args.Get(2).(chan<- *service.ListResponse)
					stream <- &service.ListResponse{
						Entities:      []*service.Entity{},
						TotalCount:    0,
						NextPageToken: "",
					}
				}).Return(nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "list with invalid consistency parameter",
			path:           "/api/v1/kv/users?consistency=invalid",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "list with service error",
			path: "/api/v1/kv/users",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("List", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
					// Send error through errChan by returning error from List
				}).Return(&errors.QBError{Code: errors.ErrInternal, Op: "test", Err: &errors.MessageError{Message: "list failed"}}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "list with nil response",
			path: "/api/v1/kv/users",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("List", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
					// Don't send anything to stream, let it return nil
				}).Return(nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp KVListResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Empty(t, resp.Items)
			},
		},
		{
			name: "list filters non-KV format entities",
			path: "/api/v1/kv/users",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				wrapper := kvDataWrapper{Key: "user1", Value: json.RawMessage(`"data1"`)}
				kvData, _ := json.Marshal(wrapper)

				m.On("List", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
					stream := args.Get(2).(chan<- *service.ListResponse)
					stream <- &service.ListResponse{
						Entities: []*service.Entity{
							{ID: 1, Data: kvData, Timestamp: 12345},
							{ID: 2, Data: []byte(`"non-kv-data"`), Timestamp: 12346}, // Not KV format
						},
						TotalCount:    2,
						NextPageToken: "",
					}
				}).Return(nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp KVListResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				// Only the KV-formatted entity should be included
				assert.Len(t, resp.Items, 1)
				assert.Equal(t, "user1", resp.Items[0].Key)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			tt.setupMock(mockService)

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req = req.WithContext(context.Background())

			w := httptest.NewRecorder()
			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "unexpected status code: %s", w.Body.String())

			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHashStringKey(t *testing.T) {
	tests := []struct {
		entityType string
		key        string
	}{
		{"users", "john"},
		{"config", "app/settings"},
		{"data", ""},
		{"", "key"},
	}

	for _, tt := range tests {
		t.Run(tt.entityType+"/"+tt.key, func(t *testing.T) {
			hash1 := hashStringKey(tt.entityType, tt.key)
			hash2 := hashStringKey(tt.entityType, tt.key)
			// Same input should produce same hash
			assert.Equal(t, hash1, hash2, "hash should be deterministic")

			// Different inputs should (very likely) produce different hashes
			if tt.key != "" {
				different := hashStringKey(tt.entityType, tt.key+"x")
				assert.NotEqual(t, hash1, different, "different keys should have different hashes")
			}
		})
	}
}
