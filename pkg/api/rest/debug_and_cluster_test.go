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
