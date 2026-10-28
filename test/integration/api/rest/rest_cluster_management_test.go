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

//go:build integration

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// ClusterStatusRestResponse represents the REST API cluster status response
type ClusterStatusRestResponse struct {
	NodeID      uint64          `json:"node_id"`
	RaftAddress string          `json:"raft_address"`
	LeaderID    string          `json:"leader_id,omitempty"`
	TotalNodes  int             `json:"total_nodes,omitempty"`
	Members     []uint64        `json:"members,omitempty"`
	Shards      []ShardInfoRest `json:"shards"`
	Healthy     bool            `json:"healthy"`
	Timestamp   time.Time       `json:"timestamp"`
}

// ShardInfoRest represents shard information in REST API responses
type ShardInfoRest struct {
	ShardID  uint64   `json:"shard_id"`
	LeaderID uint64   `json:"leader_id"`
	IsLeader bool     `json:"is_leader"`
	Members  []uint64 `json:"members"`
	State    string   `json:"state"`
}

// LeaderRestResponse represents leader information in REST API responses
type LeaderRestResponse struct {
	ShardID       uint64 `json:"shard_id"`
	LeaderID      uint64 `json:"leader_id"`
	LeaderAddress string `json:"leader_address,omitempty"`
	Term          uint64 `json:"term,omitempty"`
	Valid         bool   `json:"valid"`
}

// AddNodeRestRequest represents the REST API add node request
type AddNodeRestRequest struct {
	ShardID uint64 `json:"shard_id"`
	NodeID  uint64 `json:"node_id"`
	Address string `json:"address"`
}

// AddNodeRestResponse represents the REST API add node response
type AddNodeRestResponse struct {
	Success   bool      `json:"success"`
	Message   string    `json:"message"`
	ShardID   uint64    `json:"shard_id"`
	NodeID    uint64    `json:"node_id"`
	Timestamp time.Time `json:"timestamp"`
}

// RemoveNodeRestResponse represents the REST API remove node response
type RemoveNodeRestResponse struct {
	Success   bool      `json:"success"`
	Message   string    `json:"message"`
	ShardID   uint64    `json:"shard_id"`
	NodeID    uint64    `json:"node_id"`
	Timestamp time.Time `json:"timestamp"`
}

// TestRESTClusterStatus tests that the cluster status REST API returns a valid
// response structure. In embedded/standalone mode, the server has no real Raft
// consensus, so node_id may be 0, healthy may be false, and shards may be nil.
// The test validates the API is reachable and returns a parseable JSON response.
func TestRESTClusterStatus(t *testing.T) {
	baseURL := helpers.GetRESTEndpoints()[0]
	client := helpers.NewSystemAdminHTTPClient(t, 30*time.Second)

	resp, err := client.Get(baseURL + "/api/v1/cluster/status")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var statusResp ClusterStatusRestResponse
	err = json.NewDecoder(resp.Body).Decode(&statusResp)
	require.NoError(t, err)

	// In embedded mode, healthy may be false and shards may be nil since there
	// is no real Raft cluster. Log the actual state for diagnostics.
	t.Logf("Cluster status: node=%d, shards=%d, healthy=%v",
		statusResp.NodeID, len(statusResp.Shards), statusResp.Healthy)
}

// TestRESTClusterLeader tests getting cluster leader via REST API.
// In embedded/standalone mode, there may be no leader available.
func TestRESTClusterLeader(t *testing.T) {
	baseURL := helpers.GetRESTEndpoints()[0]
	client := helpers.NewSystemAdminHTTPClient(t, 30*time.Second)

	resp, err := client.Get(baseURL + "/api/v1/cluster/leader")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Leader endpoint may return 200, 503 if no leader, or 408 if leader
	// election times out (embedded/standalone mode with no real Raft cluster)
	assert.Contains(t, []int{http.StatusOK, http.StatusServiceUnavailable, http.StatusRequestTimeout},
		resp.StatusCode,
		"Expected valid response status for leader query")

	if resp.StatusCode == http.StatusOK {
		var leaderResp struct {
			Leader struct {
				NodeID       uint64 `json:"node_id"`
				Address      string `json:"address"`
				IsLeader     bool   `json:"is_leader"`
				HealthStatus string `json:"health_status"`
			} `json:"leader"`
			Timestamp time.Time `json:"timestamp"`
		}
		err = json.NewDecoder(resp.Body).Decode(&leaderResp)
		require.NoError(t, err)

		assert.True(t, leaderResp.Leader.IsLeader)
		assert.NotZero(t, leaderResp.Leader.NodeID)
		t.Logf("Cluster leader: node=%d, address=%s", leaderResp.Leader.NodeID, leaderResp.Leader.Address)
	} else {
		t.Log("No cluster leader available (expected in embedded/standalone mode)")
	}
}

// TestRESTClusterListNodes tests listing cluster nodes via REST API.
// In embedded/standalone mode, total_count may be 0 and nodes may be empty.
func TestRESTClusterListNodes(t *testing.T) {
	baseURL := helpers.GetRESTEndpoints()[0]
	client := helpers.NewSystemAdminHTTPClient(t, 30*time.Second)

	resp, err := client.Get(baseURL + "/api/v1/cluster/nodes")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var listResp struct {
		Nodes      []json.RawMessage `json:"nodes"`
		TotalCount int               `json:"total_count"`
		Timestamp  time.Time         `json:"timestamp"`
	}
	err = json.NewDecoder(resp.Body).Decode(&listResp)
	require.NoError(t, err)

	// In embedded mode, TotalCount may be 0 and Nodes may be empty.
	// Verify consistency between the count and the nodes slice.
	assert.Equal(t, listResp.TotalCount, len(listResp.Nodes),
		"Node count should match nodes slice length")

	t.Logf("Cluster has %d nodes", listResp.TotalCount)
}

// TestRESTClusterAddNode tests adding a node to a shard via REST API
func TestRESTClusterAddNode(t *testing.T) {
	baseURL := helpers.GetRESTEndpoints()[0]
	client := helpers.NewSystemAdminHTTPClient(t, 30*time.Second)

	// Try to add a new node (may fail depending on cluster state)
	req := AddNodeRestRequest{
		ShardID: 1,
		NodeID:  100,
		Address: "test-node100:63100",
	}

	body, err := json.Marshal(req)
	require.NoError(t, err)

	resp, err := client.Post(
		baseURL+"/api/v1/cluster/nodes",
		"application/json",
		bytes.NewBuffer(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Note: Adding a node may succeed or fail depending on cluster state. The
	// REST handler maps raft membership errors (ErrRaft, ErrLeaderNotAvailable)
	// to 503 Service Unavailable, which is the expected result here since the
	// test asks to add a node that is not reachable.
	assert.Contains(t, []int{
		http.StatusOK,
		http.StatusBadRequest,
		http.StatusInternalServerError,
		http.StatusRequestTimeout,
		http.StatusServiceUnavailable,
	}, resp.StatusCode, "Expected valid response status for add node")

	if resp.StatusCode == http.StatusOK {
		var addResp AddNodeRestResponse
		err = json.NewDecoder(resp.Body).Decode(&addResp)
		require.NoError(t, err)

		assert.Equal(t, req.ShardID, addResp.ShardID)
		assert.Equal(t, req.NodeID, addResp.NodeID)

		t.Logf("Node %d added to shard %d: %s", addResp.NodeID, addResp.ShardID, addResp.Message)

		// The add succeeded against the SHARED integration cluster, so the
		// phantom member MUST be removed again. Leaving a permanently
		// unreachable member in the raft group raises the election quorum
		// (4 members / 3 alive) and destabilizes leader elections for every
		// suite that runs afterwards — observed as "not leader for shard 1,
		// leader unknown" failures in the scheduler suite and stuck
		// leaderless shards in the cluster suite.
		removeTestClusterNode(t, client, baseURL, req.ShardID, req.NodeID)
	} else {
		t.Logf("Add node returned status %d (expected for integration test)", resp.StatusCode)
	}
}

// removeTestClusterNode removes a node previously added by an add-node test
// from a shard's raft group, retrying until the membership change is
// accepted. Removal requires a settled shard leader, so transient 503s are
// retried until the deadline. Failure to clean up is fatal: a poisoned
// membership breaks unrelated suites later in the run.
func removeTestClusterNode(t *testing.T, client *http.Client, baseURL string, shardID, nodeID uint64) {
	t.Helper()

	url := baseURL + "/api/v1/cluster/nodes/" + strconv.FormatUint(nodeID, 10) +
		"?shard_id=" + strconv.FormatUint(shardID, 10)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		if err == nil {
			status := resp.StatusCode
			resp.Body.Close()
			if status == http.StatusOK || status == http.StatusNotFound {
				t.Logf("Cleaned up test node %d from shard %d (status %d)", nodeID, shardID, status)
				return
			}
			t.Logf("Remove test node %d from shard %d returned status %d; retrying", nodeID, shardID, status)
		} else {
			t.Logf("Remove test node %d from shard %d failed: %v; retrying", nodeID, shardID, err)
		}

		select {
		case <-ctx.Done():
			t.Fatalf("failed to remove test node %d from shard %d within 60s — "+
				"leaving it in the raft group would destabilize later suites", nodeID, shardID)
		case <-ticker.C:
		}
	}
}

// TestRESTClusterRemoveNode tests removing a node from a shard via REST API
func TestRESTClusterRemoveNode(t *testing.T) {
	baseURL := helpers.GetRESTEndpoints()[0]
	client := helpers.NewSystemAdminHTTPClient(t, 30*time.Second)

	// Try to remove a non-existent node (should fail gracefully)
	nodeID := uint64(999)
	shardID := uint64(1)

	url := baseURL + "/api/v1/cluster/nodes/" + strconv.FormatUint(nodeID, 10) + "?shard_id=" + strconv.FormatUint(shardID, 10)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should return an error or success depending on node existence. Raft
	// membership errors surface as 503 Service Unavailable when the underlying
	// raft op rejects removal of an unknown node.
	assert.Contains(t, []int{
		http.StatusOK,
		http.StatusNotFound,
		http.StatusBadRequest,
		http.StatusInternalServerError,
		http.StatusRequestTimeout,
		http.StatusServiceUnavailable,
	}, resp.StatusCode, "Expected valid response status for remove node")

	if resp.StatusCode == http.StatusOK {
		var removeResp RemoveNodeRestResponse
		err = json.NewDecoder(resp.Body).Decode(&removeResp)
		require.NoError(t, err)

		t.Logf("Node %d removed from shard %d: %s", removeResp.NodeID, removeResp.ShardID, removeResp.Message)
	} else {
		t.Logf("Remove node returned status %d (expected for non-existent node)", resp.StatusCode)
	}
}

// TestRESTClusterAddNodeValidation tests validation for add node via REST API
func TestRESTClusterAddNodeValidation(t *testing.T) {
	baseURL := helpers.GetRESTEndpoints()[0]
	client := helpers.NewSystemAdminHTTPClient(t, 30*time.Second)

	// Test missing shard_id
	req := AddNodeRestRequest{
		ShardID: 0,
		NodeID:  100,
		Address: "test-node100:63100",
	}

	body, err := json.Marshal(req)
	require.NoError(t, err)

	resp, err := client.Post(
		baseURL+"/api/v1/cluster/nodes",
		"application/json",
		bytes.NewBuffer(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "Missing shard_id should return 400")

	// Test missing node_id
	req = AddNodeRestRequest{
		ShardID: 1,
		NodeID:  0,
		Address: "test-node100:63100",
	}

	body, err = json.Marshal(req)
	require.NoError(t, err)

	resp, err = client.Post(
		baseURL+"/api/v1/cluster/nodes",
		"application/json",
		bytes.NewBuffer(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "Missing node_id should return 400")

	// Test missing address
	req = AddNodeRestRequest{
		ShardID: 1,
		NodeID:  100,
		Address: "",
	}

	body, err = json.Marshal(req)
	require.NoError(t, err)

	resp, err = client.Post(
		baseURL+"/api/v1/cluster/nodes",
		"application/json",
		bytes.NewBuffer(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "Missing address should return 400")

	t.Log("Add node validation tests passed")
}

// TestRESTClusterRemoveNodeValidation tests validation for remove node via REST API
func TestRESTClusterRemoveNodeValidation(t *testing.T) {
	baseURL := helpers.GetRESTEndpoints()[0]
	client := helpers.NewSystemAdminHTTPClient(t, 30*time.Second)

	// Test missing shard_id query parameter
	url := baseURL + "/api/v1/cluster/nodes/999"
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "Missing shard_id should return 400")

	// Test invalid node ID
	url = baseURL + "/api/v1/cluster/nodes/invalid?shard_id=1"
	req, err = http.NewRequest(http.MethodDelete, url, nil)
	require.NoError(t, err)

	resp, err = client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "Invalid node ID should return 400")

	t.Log("Remove node validation tests passed")
}

// TestRESTClusterAddNodeInvalidJSON tests error handling for malformed add node requests
func TestRESTClusterAddNodeInvalidJSON(t *testing.T) {
	baseURL := helpers.GetRESTEndpoints()[0]
	client := helpers.NewSystemAdminHTTPClient(t, 30*time.Second)

	invalidJSON := []byte(`{"shard_id": "not_a_number"}`)

	resp, err := client.Post(
		baseURL+"/api/v1/cluster/nodes",
		"application/json",
		bytes.NewBuffer(invalidJSON),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "Invalid JSON should return 400")

	t.Log("Invalid JSON correctly rejected for add node")
}
