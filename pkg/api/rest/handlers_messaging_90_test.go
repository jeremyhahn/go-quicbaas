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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestHandlersExposeShardHeaders tests the shard header exposure feature
func TestHandlersExposeShardHeaders(t *testing.T) {
	t.Run("PUT with shard header exposed", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		server.config.ExposeShardHeader = true

		mockService.ExpectedCalls = nil
		mockService.On("Put", mock.Anything, mock.Anything).Return(&service.PutResponse{
			ID:        1,
			Version:   1,
			Timestamp: 12345,
		}, nil).Once()

		putReq := PutRequest{
			EntityType: "users",
			ID:         1,
			Data:       json.RawMessage(`{"name":"Test"}`),
		}
		body, _ := json.Marshal(putReq)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/entities/users/1?write_mode=sync", bytes.NewReader(body))
		w := httptest.NewRecorder()

		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NotEmpty(t, w.Header().Get("X-QuicBaaS-Shard"))
	})

	t.Run("GET with shard header exposed", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		server.config.ExposeShardHeader = true

		mockService.ExpectedCalls = nil
		mockService.On("Get", mock.Anything, mock.Anything).Return(&service.GetResponse{
			ID:        2,
			Data:      []byte(`{"test":"data"}`),
			Version:   1,
			Timestamp: 12345,
			Found:     true,
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/entities/users/2", nil)
		w := httptest.NewRecorder()

		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NotEmpty(t, w.Header().Get("X-QuicBaaS-Shard"))
	})

	t.Run("DELETE with shard header exposed", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		server.config.ExposeShardHeader = true

		mockService.ExpectedCalls = nil
		mockService.On("Delete", mock.Anything, mock.Anything).Return(&service.DeleteResponse{
			Deleted:   true,
			Timestamp: 12345,
		}, nil).Once()

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/entities/users/3", nil)
		w := httptest.NewRecorder()

		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NotEmpty(t, w.Header().Get("X-QuicBaaS-Shard"))
	})
}

// TestHandleQueryEdgeCases tests edge cases in HandleQuery
func TestHandleQueryEdgeCases(t *testing.T) {
	t.Run("query with nil stream response", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.ExpectedCalls = nil
		mockService.On("Query", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				stream := args.Get(2).(chan<- *service.QueryResponse)
				close(stream)
			}).Return(nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/query/users/name?value=test", nil)
		w := httptest.NewRecorder()

		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp QueryResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Empty(t, resp.Results)
	})

	t.Run("query with context timeout", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		// Use a channel to block the mock until the context expires,
		// avoiding time.Sleep and ensuring deterministic behavior.
		blockCh := make(chan struct{})

		mockService.ExpectedCalls = nil
		mockService.On("Query", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				defer close(args.Get(2).(chan<- *service.QueryResponse))
				ctx := args.Get(0).(context.Context)
				select {
				case <-ctx.Done():
				case <-blockCh:
				}
			}).Return(nil).Once()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()
		defer close(blockCh)

		// Call the handler directly with a pre-populated chi RouteContext
		// to avoid the race condition in chi's router where RoutePattern()
		// is written during routing and read by middleware concurrently
		// when the context is cancelled mid-flight.
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("type", "users")
		rctx.URLParams.Add("index", "name")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/query/users/name?value=test", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		server.HandleQuery(w, req)

		assert.Equal(t, http.StatusRequestTimeout, w.Code)
	})
}

// TestHandleRangeQueryEdgeCases tests edge cases in HandleRangeQuery
func TestHandleRangeQueryEdgeCases(t *testing.T) {
	t.Run("range query with nil in stream", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.ExpectedCalls = nil
		mockService.On("ScanIndex", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				stream := args.Get(2).(chan<- *service.ScanResponse)
				stream <- nil
				stream <- &service.ScanResponse{
					Entities: []*service.Entity{
						{ID: 1, EntityType: "users", Data: []byte(`{"name":"test"}`)},
					},
				}
				close(stream)
			}).Return(nil).Once()

		rangeReq := RangeQueryRequest{
			EntityType: "users",
			Index:      "age",
			Start:      1,
			End:        10,
		}
		body, _ := json.Marshal(rangeReq)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/query/range", bytes.NewReader(body))
		w := httptest.NewRecorder()

		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("range query with timeout", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		// Use a channel to block the mock until the context expires,
		// avoiding time.Sleep and ensuring deterministic behavior.
		blockCh := make(chan struct{})

		mockService.ExpectedCalls = nil
		mockService.On("ScanIndex", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				defer close(args.Get(2).(chan<- *service.ScanResponse))
				ctx := args.Get(0).(context.Context)
				select {
				case <-ctx.Done():
				case <-blockCh:
				}
			}).Return(nil).Once()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()
		defer close(blockCh)

		rangeReq := RangeQueryRequest{
			EntityType: "users",
			Index:      "age",
			Start:      1,
			End:        10,
		}
		body, _ := json.Marshal(rangeReq)

		// Call the handler directly with a pre-populated chi RouteContext
		// to avoid the race condition in chi's router where RoutePattern()
		// is written during routing and read by middleware concurrently
		// when the context is cancelled mid-flight.
		rctx := chi.NewRouteContext()
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/query/range", bytes.NewReader(body))
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		server.HandleRangeQuery(w, req)

		assert.Equal(t, http.StatusRequestTimeout, w.Code)
	})
}

// TestHandleClusterShardsEdgeCases tests edge cases in HandleClusterShards
func TestHandleClusterShardsEdgeCases(t *testing.T) {
	t.Run("cluster shards with nil response", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.ExpectedCalls = nil
		mockService.On("ClusterStatus", mock.Anything, mock.Anything).Return(nil, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/shards", nil)
		w := httptest.NewRecorder()

		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "cluster status unavailable")
	})
}

// TestHandleGetLeaderEdgeCases tests edge cases in HandleGetLeader
func TestHandleGetLeaderEdgeCases(t *testing.T) {
	t.Run("get leader with nil response", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.ExpectedCalls = nil
		mockService.On("GetLeader", mock.Anything, mock.Anything).Return(nil, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/leader/1", nil)
		w := httptest.NewRecorder()

		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "leader information unavailable")
	})

	t.Run("get leader with zero ID", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.ExpectedCalls = nil
		mockService.On("GetLeader", mock.Anything, mock.Anything).Return(&service.GetLeaderResponse{
			ShardID:  1,
			LeaderID: 0,
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/leader/1", nil)
		w := httptest.NewRecorder()

		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp LeaderResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.False(t, resp.Valid)
	})
}

// TestHandleClusterStatusEdgeCases tests edge cases in HandleClusterStatus
func TestHandleClusterStatusEdgeCases(t *testing.T) {
	t.Run("cluster status with nil response", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.ExpectedCalls = nil
		mockService.On("ClusterStatus", mock.Anything, mock.Anything).Return(nil, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/status", nil)
		w := httptest.NewRecorder()

		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "cluster status unavailable")
	})

	t.Run("cluster status with empty arrays", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.ExpectedCalls = nil
		mockService.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
			ClusterID: "test",
			Nodes:     []*service.NodeInfo{},
			Shards:    []*service.ShardInfo{},
			Timestamp: 12345,
		}, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/status", nil)
		w := httptest.NewRecorder()

		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// TestHandleListStreamingEdgeCases tests edge cases in HandleList streaming
func TestHandleListStreamingEdgeCases(t *testing.T) {
	t.Run("list with error in stream", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.ExpectedCalls = nil
		mockService.On("List", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				close(args.Get(2).(chan<- *service.ListResponse))
			}).
			Return(errors.Newf(errors.ErrInternal, "test", "stream error")).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/entities/users", nil)
		w := httptest.NewRecorder()

		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
