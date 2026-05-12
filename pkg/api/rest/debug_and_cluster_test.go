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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func newMockServer(t *testing.T) *Server {
	t.Helper()
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := testutil.NewMockQuicBaaS()

	// Setup default health mock for routes that may need it
	mockSvc.On("Health", mock.Anything, mock.Anything).Return(&service.HealthResponse{
		Status: service.HealthStatusHealthy,
		NodeID: 1,
	}, nil).Maybe()

	srv, err := NewServer(mockSvc, DefaultConfig(), log)
	require.NoError(t, err)
	return srv
}

func TestRegisterDebugRoutes_RouteEndpoint(t *testing.T) {
	// Note: Debug routes are enabled via environment variable in integration tests
	// This test just verifies server starts without the debug routes in normal mode
	srv := newMockServer(t)
	defer srv.Close()

	// Verify server was created successfully
	assert.NotNil(t, srv)
	assert.NotNil(t, srv.GetRouter())
}

func TestHandleClusterShards_WithNodeHostInfo(t *testing.T) {
	t.Helper()
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := testutil.NewMockQuicBaaS()

	// Setup cluster status mock with shard information
	mockSvc.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
		ClusterID: "test-cluster",
		Nodes: []*service.NodeInfo{
			{NodeID: 1, Address: "127.0.0.1:9999", IsLeader: true, HealthStatus: service.HealthStatusHealthy},
			{NodeID: 2, Address: "127.0.0.1:9998", IsLeader: false, HealthStatus: service.HealthStatusHealthy},
		},
		Shards: []*service.ShardInfo{
			{ShardID: 10, LeaderID: 1, Replicas: []uint64{1, 2}, Status: service.HealthStatusHealthy},
			{ShardID: 11, LeaderID: 2, Replicas: []uint64{1, 2}, Status: service.HealthStatusHealthy},
		},
	}, nil).Maybe()

	srv, err := NewServer(mockSvc, DefaultConfig(), log)
	require.NoError(t, err)
	defer srv.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/shards", nil)
	w := httptest.NewRecorder()
	srv.GetRouter().ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	// Verify response structure
	assert.Contains(t, resp, "shards")
}

// ---------------------------------------------------------------------------
// Regression tests: HandleClusterStatus must include node_id (Issue 4 fix)
// ---------------------------------------------------------------------------

// TestHandleClusterStatus_NodeIDFromServiceNodes verifies that when the service
// returns nodes the first node's ID is used as node_id in the response.
func TestHandleClusterStatus_NodeIDFromServiceNodes(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := testutil.NewMockQuicBaaS()

	mockSvc.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
		ClusterID: "quicbaas",
		Nodes: []*service.NodeInfo{
			{NodeID: 42, Address: "127.0.0.1:9000", IsLeader: true, HealthStatus: service.HealthStatusHealthy},
		},
		Shards: []*service.ShardInfo{
			{ShardID: 1, LeaderID: 42, Replicas: []uint64{42}, Status: service.HealthStatusHealthy},
		},
	}, nil).Once()

	cfg := DefaultConfig()
	cfg.NodeID = 7 // daemon config; should be overridden by service response
	srv, err := NewServer(mockSvc, cfg, log)
	require.NoError(t, err)
	defer srv.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/status", nil)
	w := httptest.NewRecorder()
	srv.GetRouter().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp ClusterStatusResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, uint64(42), resp.NodeID, "node_id must come from service response nodes list")
}

// TestHandleClusterStatus_NodeIDFallbackFromConfig verifies that when the
// service returns an empty Nodes slice (standalone mode), the node_id in the
// response falls back to the daemon's own Raft node ID from Config.NodeID.
func TestHandleClusterStatus_NodeIDFallbackFromConfig(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := testutil.NewMockQuicBaaS()

	// Simulate standalone mode: service returns no nodes (topology unavailable).
	mockSvc.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
		ClusterID: "quicbaas",
		Nodes:     []*service.NodeInfo{},
		Shards:    []*service.ShardInfo{},
	}, nil).Once()

	cfg := DefaultConfig()
	cfg.NodeID = 1 // the daemon's own Raft node ID
	srv, err := NewServer(mockSvc, cfg, log)
	require.NoError(t, err)
	defer srv.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/status", nil)
	w := httptest.NewRecorder()
	srv.GetRouter().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp ClusterStatusResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, uint64(1), resp.NodeID,
		"node_id must fall back to config NodeID when service returns no nodes")
}
