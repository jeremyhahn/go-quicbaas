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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// =============================================================================
// Node Management Tests
// =============================================================================

func TestHandleAddNode(t *testing.T) {
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
			name: "add node successfully",
			body: AddNodeRequest{
				ShardID: 1,
				NodeID:  2,
				Address: "192.168.1.2:5001",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("AddNode", mock.Anything, mock.Anything).Return(&service.AddNodeResponse{
					Success: true,
					Message: "node added successfully",
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp AddNodeResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.True(t, resp.Success)
				assert.Equal(t, uint64(1), resp.ShardID)
				assert.Equal(t, uint64(2), resp.NodeID)
			},
		},
		{
			name: "missing shard_id",
			body: AddNodeRequest{
				NodeID:  2,
				Address: "192.168.1.2:5001",
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Contains(t, resp.Error, "shard_id")
			},
		},
		{
			name: "identity reaches the service",
			body: AddNodeRequest{
				ShardID:  1,
				NodeID:   3,
				Address:  "192.168.1.3:5001",
				Identity: "spki:0123abcd",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("AddNode", mock.Anything, mock.MatchedBy(func(req *service.AddNodeRequest) bool {
					return req.NodeID == 3 && req.Identity == "spki:0123abcd"
				})).Return(&service.AddNodeResponse{Success: true}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "missing node_id",
			body: AddNodeRequest{
				ShardID: 1,
				Address: "192.168.1.2:5001",
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Contains(t, resp.Error, "node_id")
			},
		},
		{
			name: "missing address",
			body: AddNodeRequest{
				ShardID: 1,
				NodeID:  2,
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Contains(t, resp.Error, "address")
			},
		},
		{
			name: "service error",
			body: AddNodeRequest{
				ShardID: 1,
				NodeID:  2,
				Address: "192.168.1.2:5001",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("AddNode", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "test",
					Err:  &errors.MessageError{Message: "failed to add node"},
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:           "body is not a JSON object",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService.ExpectedCalls = nil
			tt.setupMock(mockService)

			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/nodes", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}

			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleRemoveNode(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		nodeID         string
		shardID        string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:    "remove node successfully",
			nodeID:  "2",
			shardID: "1",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("RemoveNode", mock.Anything, mock.Anything).Return(&service.RemoveNodeResponse{
					Success: true,
					Message: "node removed successfully",
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp RemoveNodeResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.True(t, resp.Success)
				assert.Equal(t, uint64(1), resp.ShardID)
				assert.Equal(t, uint64(2), resp.NodeID)
			},
		},
		{
			name:           "invalid node id",
			nodeID:         "abc",
			shardID:        "1",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "missing shard_id parameter",
			nodeID:         "2",
			shardID:        "",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid shard_id parameter",
			nodeID:         "2",
			shardID:        "invalid",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "service error",
			nodeID:  "2",
			shardID: "1",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("RemoveNode", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "test",
					Err:  &errors.MessageError{Message: "remove failed"},
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService.ExpectedCalls = nil
			tt.setupMock(mockService)

			url := fmt.Sprintf("/api/v1/cluster/nodes/%s", tt.nodeID)
			if tt.shardID != "" {
				url += fmt.Sprintf("?shard_id=%s", tt.shardID)
			}

			req := httptest.NewRequest(http.MethodDelete, url, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}

			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleReplaceNode(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		failedNodeID   string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:         "replace node successfully",
			failedNodeID: "2",
			body: ReplaceNodeRequest{
				ReplacementNodeID:  3,
				ReplacementAddress: "192.168.1.3:5001",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
					Nodes: []*service.NodeInfo{
						{NodeID: 1, Address: "192.168.1.1:5001"},
						{NodeID: 2, Address: "192.168.1.2:5001"},
					},
					Shards: []*service.ShardInfo{
						{ShardID: 1, Replicas: []uint64{1, 2}},
					},
					Timestamp: 12345,
				}, nil).Once()
				m.On("AddNode", mock.Anything, mock.Anything).Return(&service.AddNodeResponse{
					Success: true,
				}, nil).Once()
				m.On("RemoveNode", mock.Anything, mock.Anything).Return(&service.RemoveNodeResponse{
					Success: true,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ReplaceNodeResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.True(t, resp.Success)
				assert.Len(t, resp.AffectedShards, 1)
			},
		},
		{
			name:         "node not found",
			failedNodeID: "99",
			body: ReplaceNodeRequest{
				ReplacementNodeID:  3,
				ReplacementAddress: "192.168.1.3:5001",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
					Nodes: []*service.NodeInfo{
						{NodeID: 1, Address: "192.168.1.1:5001"},
					},
					Shards: []*service.ShardInfo{
						{ShardID: 1, Replicas: []uint64{1}},
					},
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name:         "invalid node id",
			failedNodeID: "invalid",
			body: ReplaceNodeRequest{
				ReplacementNodeID:  3,
				ReplacementAddress: "192.168.1.3:5001",
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:         "missing replacement_node_id",
			failedNodeID: "2",
			body: ReplaceNodeRequest{
				ReplacementAddress: "192.168.1.3:5001",
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:         "missing replacement_address",
			failedNodeID: "2",
			body: ReplaceNodeRequest{
				ReplacementNodeID: 3,
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "body is not a JSON object",
			failedNodeID:   "2",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:         "cluster status error",
			failedNodeID: "2",
			body: ReplaceNodeRequest{
				ReplacementNodeID:  3,
				ReplacementAddress: "192.168.1.3:5001",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ClusterStatus", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "test",
					Err:  &errors.MessageError{Message: "cluster error"},
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService.ExpectedCalls = nil
			tt.setupMock(mockService)

			body, _ := json.Marshal(tt.body)
			url := fmt.Sprintf("/api/v1/cluster/nodes/%s/replace", tt.failedNodeID)
			req := httptest.NewRequest(http.MethodPut, url, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}

			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleListNodes(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "list nodes successfully",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
					Nodes: []*service.NodeInfo{
						{
							NodeID:        1,
							Address:       "192.168.1.1:5001",
							IsLeader:      true,
							HealthStatus:  service.HealthStatusHealthy,
							Version:       "1.0.0",
							UptimeSeconds: 3600,
						},
						{
							NodeID:        2,
							Address:       "192.168.1.2:5001",
							IsLeader:      false,
							HealthStatus:  service.HealthStatusHealthy,
							Version:       "1.0.0",
							UptimeSeconds: 3500,
						},
					},
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ListNodesResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, 2, resp.TotalCount)
				assert.Len(t, resp.Nodes, 2)
				assert.Equal(t, uint64(1), resp.Nodes[0].NodeID)
				assert.True(t, resp.Nodes[0].IsLeader)
			},
		},
		{
			name: "service error",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ClusterStatus", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "test",
					Err:  &errors.MessageError{Message: "cluster status unavailable"},
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "nil status response",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ClusterStatus", mock.Anything, mock.Anything).Return(
					(*service.ClusterStatusResponse)(nil), nil,
				).Once()
			},
			expectedStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "cluster status unavailable")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService.ExpectedCalls = nil
			tt.setupMock(mockService)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/nodes", nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}

			mockService.AssertExpectations(t)
		})
	}
}

// =============================================================================
// Non-Leader Nodes Tests
// =============================================================================

func TestHandleListNonLeaderNodes(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "list non-leader nodes successfully",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
					Nodes: []*service.NodeInfo{
						{
							NodeID:        1,
							Address:       "192.168.1.1:5001",
							IsLeader:      true,
							HealthStatus:  service.HealthStatusHealthy,
							Version:       "1.0.0",
							UptimeSeconds: 3600,
						},
						{
							NodeID:        2,
							Address:       "192.168.1.2:5001",
							IsLeader:      false,
							HealthStatus:  service.HealthStatusHealthy,
							Version:       "1.0.0",
							UptimeSeconds: 3500,
						},
						{
							NodeID:        3,
							Address:       "192.168.1.3:5001",
							IsLeader:      false,
							HealthStatus:  service.HealthStatusHealthy,
							Version:       "1.0.0",
							UptimeSeconds: 3400,
						},
					},
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ListNodesResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				// Should only return non-leader nodes
				assert.Equal(t, 2, resp.TotalCount)
				assert.Len(t, resp.Nodes, 2)
				// Verify none are leaders
				for _, node := range resp.Nodes {
					assert.False(t, node.IsLeader, "expected non-leader node, got leader for node %d", node.NodeID)
				}
				// Verify the non-leader nodes are correct
				assert.Equal(t, uint64(2), resp.Nodes[0].NodeID)
				assert.Equal(t, uint64(3), resp.Nodes[1].NodeID)
			},
		},
		{
			name: "filter unhealthy nodes",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
					Nodes: []*service.NodeInfo{
						{
							NodeID:        1,
							Address:       "192.168.1.1:5001",
							IsLeader:      true,
							HealthStatus:  service.HealthStatusHealthy,
							Version:       "1.0.0",
							UptimeSeconds: 3600,
						},
						{
							NodeID:        2,
							Address:       "192.168.1.2:5001",
							IsLeader:      false,
							HealthStatus:  service.HealthStatusUnhealthy, // Should be filtered out
							Version:       "1.0.0",
							UptimeSeconds: 3500,
						},
						{
							NodeID:        3,
							Address:       "192.168.1.3:5001",
							IsLeader:      false,
							HealthStatus:  service.HealthStatusHealthy,
							Version:       "1.0.0",
							UptimeSeconds: 3400,
						},
					},
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ListNodesResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				// Should only return healthy non-leader nodes
				assert.Equal(t, 1, resp.TotalCount)
				assert.Len(t, resp.Nodes, 1)
				assert.Equal(t, uint64(3), resp.Nodes[0].NodeID)
			},
		},
		{
			name: "no non-leader nodes available",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
					Nodes: []*service.NodeInfo{
						{
							NodeID:        1,
							Address:       "192.168.1.1:5001",
							IsLeader:      true,
							HealthStatus:  service.HealthStatusHealthy,
							Version:       "1.0.0",
							UptimeSeconds: 3600,
						},
					},
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ListNodesResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, 0, resp.TotalCount)
				assert.Len(t, resp.Nodes, 0)
			},
		},
		{
			name: "service error",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ClusterStatus", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "test",
					Err:  &errors.MessageError{Message: "cluster status unavailable"},
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "nil status response",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ClusterStatus", mock.Anything, mock.Anything).Return(
					(*service.ClusterStatusResponse)(nil), nil,
				).Once()
			},
			expectedStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "cluster status unavailable")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService.ExpectedCalls = nil
			tt.setupMock(mockService)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/nodes/non-leader", nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}

			mockService.AssertExpectations(t)
		})
	}
}

// =============================================================================
// Cluster Leader Tests
// =============================================================================

func TestHandleGetClusterLeader(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "get cluster leader successfully",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
					Nodes: []*service.NodeInfo{
						{
							NodeID:        1,
							Address:       "192.168.1.1:5001",
							IsLeader:      true,
							HealthStatus:  service.HealthStatusHealthy,
							Version:       "1.0.0",
							UptimeSeconds: 3600,
						},
						{
							NodeID:        2,
							Address:       "192.168.1.2:5001",
							IsLeader:      false,
							HealthStatus:  service.HealthStatusHealthy,
							Version:       "1.0.0",
							UptimeSeconds: 3500,
						},
					},
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ClusterLeaderResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, uint64(1), resp.Leader.NodeID)
				assert.True(t, resp.Leader.IsLeader)
				assert.Equal(t, "192.168.1.1:5001", resp.Leader.Address)
				assert.Equal(t, "healthy", resp.Leader.HealthStatus)
				assert.Equal(t, "1.0.0", resp.Leader.Version)
				assert.Equal(t, uint64(3600), resp.Leader.UptimeSeconds)
			},
		},
		{
			name: "no leader available",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
					Nodes: []*service.NodeInfo{
						{
							NodeID:        1,
							Address:       "192.168.1.1:5001",
							IsLeader:      false,
							HealthStatus:  service.HealthStatusHealthy,
							Version:       "1.0.0",
							UptimeSeconds: 3600,
						},
						{
							NodeID:        2,
							Address:       "192.168.1.2:5001",
							IsLeader:      false,
							HealthStatus:  service.HealthStatusHealthy,
							Version:       "1.0.0",
							UptimeSeconds: 3500,
						},
					},
					Timestamp: 12345,
				}, nil).Once()
			},
			// Handler emits 503 ("no leader available"); TimeoutMiddleware no
			// longer rewrites status codes, so 503 reaches the wire intact.
			expectedStatus: http.StatusServiceUnavailable,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Contains(t, resp.Error, "no leader")
			},
		},
		{
			name: "service error",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ClusterStatus", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "test",
					Err:  &errors.MessageError{Message: "cluster status unavailable"},
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "nil response from service",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ClusterStatus", mock.Anything, mock.Anything).Return(nil, nil).Once()
			},
			expectedStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Contains(t, resp.Error, "cluster status unavailable")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService.ExpectedCalls = nil
			tt.setupMock(mockService)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/leader", nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}

			mockService.AssertExpectations(t)
		})
	}
}

// TestHandleGetClusterLeaderDirect tests the handler directly without middleware.
// This test bypasses the timeout middleware to verify the handler returns 503 directly.
func TestHandleGetClusterLeaderDirect(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
		Nodes: []*service.NodeInfo{
			{
				NodeID:       1,
				Address:      "192.168.1.1:5001",
				IsLeader:     false,
				HealthStatus: service.HealthStatusHealthy,
			},
		},
		Timestamp: 12345,
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/leader", nil)
	w := httptest.NewRecorder()

	// Call handler directly to bypass timeout middleware
	server.HandleGetClusterLeader(w, req)

	// Handler should return 503 when called directly
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// =============================================================================
// Rebalance Tests
// =============================================================================

func TestHandleStartRebalance(t *testing.T) {
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
			name: "start rebalance successfully",
			body: RebalanceRequest{
				Strategy:         "round_robin",
				TargetShardCount: 3,
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("RebalanceCluster", mock.Anything, mock.Anything).Return(&service.RebalanceClusterResponse{
					JobID:               "rebalance-12345",
					Progress:            0,
					EstimatedCompletion: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusAccepted,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp RebalanceResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "rebalance-12345", resp.OperationID)
				assert.Equal(t, "started", resp.Status)
			},
		},
		{
			name: "start rebalance with default strategy",
			body: RebalanceRequest{},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("RebalanceCluster", mock.Anything, mock.Anything).Return(&service.RebalanceClusterResponse{
					JobID:               "rebalance-12346",
					Progress:            0,
					EstimatedCompletion: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusAccepted,
		},
		{
			name:           "body is not a JSON object",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "service error",
			body: RebalanceRequest{Strategy: "auto"},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("RebalanceCluster", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "test",
					Err:  &errors.MessageError{Message: "rebalance failed"},
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService.ExpectedCalls = nil
			tt.setupMock(mockService)

			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/rebalance", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}

			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleGetRebalanceStatus(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/rebalance/status", nil)
	w := httptest.NewRecorder()

	server.GetRouter().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp RebalanceStatusResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "not_running", resp.Status)
}

func TestHandleGetRebalancePlan(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "get rebalance plan successfully",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
					Shards: []*service.ShardInfo{
						{ShardID: 1},
						{ShardID: 2},
					},
					Timestamp: 12345,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp RebalancePlanResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, 2, resp.CurrentShards)
				assert.Equal(t, "round_robin", resp.Strategy)
			},
		},
		{
			name: "service error",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ClusterStatus", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrNotFound,
					Op:   "test",
					Err:  &errors.MessageError{Message: "cluster unavailable"},
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService.ExpectedCalls = nil
			tt.setupMock(mockService)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/rebalance/plan", nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}

			mockService.AssertExpectations(t)
		})
	}
}

// =============================================================================
// Migration Tests
// =============================================================================

func TestHandleStartMigration(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		body           interface{}
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "start migration successfully",
			body: MigrationRequest{
				SourceShardID: 1,
				TargetShardID: 2,
				EntityTypes:   []string{"users"},
			},
			expectedStatus: http.StatusAccepted,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp MigrationResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "started", resp.Status)
				assert.Equal(t, uint64(1), resp.SourceShardID)
				assert.Equal(t, uint64(2), resp.TargetShardID)
			},
		},
		{
			name: "missing source_shard_id",
			body: MigrationRequest{
				TargetShardID: 2,
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "missing target_shard_id",
			body: MigrationRequest{
				SourceShardID: 1,
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "body is not a JSON object",
			body:           "{invalid",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/migrate", bytes.NewReader(body))
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

func TestHandleGetMigrationStatus(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		operationID    string
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:           "get migration status",
			operationID:    "migration-123",
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp MigrationStatusResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "migration-123", resp.OperationID)
				assert.Equal(t, "not_found", resp.Status)
			},
		},
		{
			name:           "missing operation_id",
			operationID:    "",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/api/v1/cluster/migrate/status"
			if tt.operationID != "" {
				url += fmt.Sprintf("?operation_id=%s", tt.operationID)
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleCancelMigration(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		operationID    string
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:           "cancel migration",
			operationID:    "migration-123",
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp CancelMigrationResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "migration-123", resp.OperationID)
				assert.True(t, resp.Cancelled)
			},
		},
		{
			name:           "missing operation_id",
			operationID:    "",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/api/v1/cluster/migrate"
			if tt.operationID != "" {
				url += fmt.Sprintf("?operation_id=%s", tt.operationID)
			}

			req := httptest.NewRequest(http.MethodDelete, url, nil)
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// =============================================================================
// Upgrade Tests
// =============================================================================

func TestHandleStartUpgrade(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		body           interface{}
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "start upgrade successfully",
			body: UpgradeRequest{
				TargetVersion: "2.0.0",
				NodeID:        0,
			},
			expectedStatus: http.StatusAccepted,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp UpgradeResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "started", resp.Status)
				assert.Equal(t, "2.0.0", resp.TargetVersion)
			},
		},
		{
			name:           "missing target_version",
			body:           UpgradeRequest{},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "body is not a JSON object",
			body:           "{invalid",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/upgrade", bytes.NewReader(body))
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

func TestHandleGetUpgradeStatus(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/upgrade/status", nil)
	w := httptest.NewRecorder()

	server.GetRouter().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp UpgradeStatusResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "not_running", resp.Status)
}

// =============================================================================
// Integration Tests
// =============================================================================

func TestClusterOperationsIntegration(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	// Test complete workflow: add node -> list nodes -> remove node
	t.Run("complete node lifecycle", func(t *testing.T) {
		// Step 1: Add node
		mockService.On("AddNode", mock.Anything, mock.Anything).Return(&service.AddNodeResponse{
			Success: true,
			Message: "node added",
		}, nil).Once()

		addBody, _ := json.Marshal(AddNodeRequest{
			ShardID: 1,
			NodeID:  3,
			Address: "192.168.1.3:5001",
		})

		req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/nodes", bytes.NewReader(addBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.GetRouter().ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Step 2: List nodes
		mockService.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
			Nodes: []*service.NodeInfo{
				{NodeID: 1, Address: "192.168.1.1:5001"},
				{NodeID: 2, Address: "192.168.1.2:5001"},
				{NodeID: 3, Address: "192.168.1.3:5001"},
			},
			Timestamp: 12345,
		}, nil).Once()

		req = httptest.NewRequest(http.MethodGet, "/api/v1/cluster/nodes", nil)
		w = httptest.NewRecorder()
		server.GetRouter().ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var listResp ListNodesResponse
		json.NewDecoder(w.Body).Decode(&listResp)
		assert.Equal(t, 3, listResp.TotalCount)

		// Step 3: Remove node
		mockService.On("RemoveNode", mock.Anything, mock.Anything).Return(&service.RemoveNodeResponse{
			Success: true,
			Message: "node removed",
		}, nil).Once()

		req = httptest.NewRequest(http.MethodDelete, "/api/v1/cluster/nodes/3?shard_id=1", nil)
		w = httptest.NewRecorder()
		server.GetRouter().ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		mockService.AssertExpectations(t)
	})
}

func TestHandleCryptoDataKeyMissingKeyNameTarget(t *testing.T) {
	t.Parallel()
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{"context": {"purpose": "encryption"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/datakey", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCryptoDataKey(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "key name is required")
}

// TestHandleStartMigrationWithInvalidJSONTarget tests start migration with invalid JSON.
// TestHandleStartUpgradeWithInvalidJSONTarget tests start upgrade with invalid JSON.
// TestHandleListNonLeaderNodesNilResponseTarget tests nil response from ClusterStatus.
// TestHandleListNonLeaderNodesSuccessTarget tests successful non-leader nodes listing.
// TestHandleReplaceNodeEmptyNodeIDTarget tests node replacement with empty node ID.
// TestHandleListNodesNilResponseTarget tests nil response from ClusterStatus.
// TestHandleListNodesSuccessTarget tests successful nodes listing.
func TestHandleClusterStatus_FullCoverage(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "cluster status success",
			path: "/api/v1/cluster/status",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
					ClusterID: "cluster-1",
					Shards: []*service.ShardInfo{
						{ShardID: 1, LeaderID: 1, Replicas: []uint64{1, 2, 3}},
						{ShardID: 2, LeaderID: 2, Replicas: []uint64{2, 3, 4}},
					},
					Timestamp: time.Now().Unix(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "cluster status service error",
			path: "/api/v1/cluster/status",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.ExpectedCalls = nil
				m.On("ClusterStatus", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "test",
					Err:  &errors.MessageError{Message: "cluster error"},
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
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

			assert.Equal(t, tt.expectedStatus, w.Code, "unexpected status: %s", w.Body.String())
		})
	}
}
