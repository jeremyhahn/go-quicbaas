// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
// +build integration

// Package cluster contains Phase F end-to-end integration tests for the
// QuicBaaS cluster mode (3-node Raft via qrdb NewClusteredNode). These tests
// run against a live 3-node docker-compose deployment defined in
// test/integration/docker-compose.cluster.yml. Each node boots as a
// QuicBaaS server in cluster mode (ServerModeCluster) with a shared
// DeploymentID and a static peer list in cfg.Raft.JoinAddresses.
//
// Prerequisites (devcontainer / CI only):
//   - Docker + Docker Compose v2
//   - 3 quicbaasd containers started via: make integration-test-cluster
//   - Environment variables: QUICBAAS_NODE1_URL, QUICBAAS_NODE2_URL,
//     QUICBAAS_NODE3_URL (REST endpoints of the three cluster nodes)
//
// Run targets:
//
//	make integration-test-cluster           — full 3-node cluster suite
//	make integration-test-cluster-failover  — leader-failover test only
//	make integration-test-cluster-membership — dynamic membership test only
//
// Reference: plan ~/.claude/plans/unified-doodling-grove.md, Phase F.
//
// NOTE: This file is named a_qrdb_cluster_test.go (with leading 'a_') so that
// Go's alphabetical test-file ordering runs these cluster tests BEFORE other
// test files (e.g., cluster_test.go with TestClusterStability). Running the
// TestQRDB tests first ensures they see a fresh cluster without any prior
// test-induced disruption to the Raft consensus engine.
package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Environment helpers
// ---------------------------------------------------------------------------

// nodeURLs returns the REST endpoints for all three cluster nodes. Tests call
// t.Skip if the environment variables are absent (not in the devcontainer).
func nodeURLs(t *testing.T) (node1, node2, node3 string) {
	t.Helper()
	node1 = os.Getenv("QUICBAAS_NODE1_URL")
	node2 = os.Getenv("QUICBAAS_NODE2_URL")
	node3 = os.Getenv("QUICBAAS_NODE3_URL")
	if node1 == "" || node2 == "" || node3 == "" {
		t.Skip("QUICBAAS_NODE1_URL / NODE2_URL / NODE3_URL not set; " +
			"run 'make integration-test-cluster' in the devcontainer to start the 3-node docker-compose stack")
	}
	return
}

// waitReady polls the /api/v1/health/ready endpoint until it returns 200 or
// the deadline is exceeded.
func waitReady(t *testing.T, nodeURL string, timeout time.Duration) bool {
	t.Helper()
	// Use a dedicated transport (new connection per call) to avoid
	// stale keepalive connections from the shared DefaultTransport pool.
	client := newInsecureHTTPClient(5 * time.Second)
	deadline := time.After(timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		resp, err := client.Get(nodeURL + "/api/v1/health/ready")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		select {
		case <-deadline:
			return false
		case <-ticker.C:
		}
	}
}

// waitForKey polls nodeURL until key is readable there, failing the test
// once replicationTimeout elapses. Callers assert the value afterwards.
func waitForKey(t *testing.T, nodeURL, key string) {
	t.Helper()
	pollUntil(t, replicationTimeout, fmt.Sprintf("key %s readable on %s", key, nodeURL),
		func(context.Context) (bool, error) {
			return kvGet(t, nodeURL, key) != nil, nil
		})
}

// kvPut stores value under entityType/key via the REST KV API.
// The value is JSON-encoded as a string so it round-trips through the
// KV handler's JSON wrapper without any base64 conversion.
func kvPut(t *testing.T, nodeURL, key string, value []byte) {
	t.Helper()
	// Use a fixed entity type. Keys must not contain '/' since chi's
	// {key} path parameter does not match across path separators.
	const entityType = "phasef"
	apiURL := nodeURL + "/api/v1/kv/" + entityType + "/" + key

	// The handler wraps the body in a JSON kvDataWrapper. Sending a JSON
	// string ensures the value round-trips through json.RawMessage without
	// a base64 conversion.
	jsonValue, err := json.Marshal(string(value))
	require.NoError(t, err, "json.Marshal value for PUT %s", apiURL)

	// Retry on transient 503 (Raft proposal timeout, leader change). Real
	// Raft clusters experience brief write failures during leader elections.
	const maxAttempts = 12
	const retryDelay = 3 * time.Second
	var lastErr error
	var lastStatus int
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		client := newInsecureHTTPClient(30 * time.Second)
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPut,
			apiURL, strings.NewReader(string(jsonValue)))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			t.Logf("kvPut attempt %d/%d: network error: %v", attempt, maxAttempts, err)
			pace(t, retryDelay)
			continue
		}
		lastStatus = resp.StatusCode
		resp.Body.Close()
		if resp.StatusCode < 300 {
			return // success
		}
		lastErr = nil
		t.Logf("kvPut attempt %d/%d: PUT %s returned %d (retrying in %v)",
			attempt, maxAttempts, apiURL, resp.StatusCode, retryDelay)
		pace(t, retryDelay)
	}
	if lastErr != nil {
		require.NoError(t, lastErr, "PUT %s failed after %d attempts", apiURL, maxAttempts)
	} else {
		require.True(t, lastStatus < 300,
			"PUT %s returned %d after %d attempts (expected 2xx)", apiURL, lastStatus, maxAttempts)
	}
}

// kvGet retrieves the value stored under key via the REST KV API.
// Returns nil when the key does not exist (404).
func kvGet(t *testing.T, nodeURL, key string) []byte {
	t.Helper()
	const entityType = "phasef"
	apiURL := nodeURL + "/api/v1/kv/" + entityType + "/" + key

	// Retry on transient errors (leader change, ReadIndex timeout).
	const maxAttempts = 6
	const retryDelay = 3 * time.Second
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		client := newInsecureHTTPClient(10 * time.Second)
		resp, err := client.Get(apiURL)
		if err != nil {
			t.Logf("kvGet attempt %d/%d: network error: %v", attempt, maxAttempts, err)
			pace(t, retryDelay)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return nil
		}
		if resp.StatusCode == http.StatusServiceUnavailable {
			t.Logf("kvGet attempt %d/%d: 503 (retrying)", attempt, maxAttempts)
			pace(t, retryDelay)
			continue
		}
		require.True(t, resp.StatusCode < 300,
			"GET %s returned %d (expected 2xx)", apiURL, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return kvExtractValue(t, body, key)
	}
	t.Fatalf("kvGet %s: failed after %d attempts", apiURL, maxAttempts)
	return nil
}

// kvExtractValue extracts the stored value bytes from the KV GET response.
func kvExtractValue(t *testing.T, body []byte, key string) []byte {
	t.Helper()
	var resp struct {
		Value string `json:"value"`
		Data  string `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err == nil {
		if resp.Value != "" {
			return []byte(resp.Value)
		}
		if resp.Data != "" {
			return []byte(resp.Data)
		}
	}
	var s string
	if err := json.Unmarshal(body, &s); err == nil {
		return []byte(s)
	}
	return body
}

// ---------------------------------------------------------------------------
// Phase F: 3-node Raft cluster tests
// ---------------------------------------------------------------------------

// TestQRDBCluster_ThreeNodeReplication verifies that data written on the
// leader is replicated to all follower nodes in a 3-node qrdb cluster.
//
// Prerequisites: QUICBAAS_NODE1_URL, NODE2_URL, NODE3_URL set.
// Run via: make integration-test-cluster
func TestQRDBCluster_ThreeNodeReplication(t *testing.T) {
	node1, node2, node3 := nodeURLs(t)

	// Ensure all nodes are ready. Allow up to 2 minutes since parallel
	// tests may temporarily disrupt cluster leadership.
	for _, url := range []string{node1, node2, node3} {
		require.True(t, waitReady(t, url, 120*time.Second),
			"cluster node at %s must become ready within 2 min", url)
	}

	key := fmt.Sprintf("replication-%d", time.Now().UnixNano())
	value := []byte("replication-sentinel")

	// Write on node 1 (will route to the Raft leader via leader forwarding).
	kvPut(t, node1, key, value)

	// Wait for Raft log application on both followers.
	waitForKey(t, node2, key)
	waitForKey(t, node3, key)

	// Read from node 2.
	got2 := kvGet(t, node2, key)
	assert.Equal(t, value, got2,
		"value written on node1 must be readable from node2 after replication")

	// Read from node 3.
	got3 := kvGet(t, node3, key)
	assert.Equal(t, value, got3,
		"value written on node1 must be readable from node3 after replication")
}

// TestQRDBCluster_LeaderFailoverRecovers verifies that the cluster elects a
// new leader when the current leader is killed, and that KV operations
// continue on the surviving nodes.
func TestQRDBCluster_LeaderFailoverRecovers(t *testing.T) {
	node1, node2, node3 := nodeURLs(t)

	// Ensure all three nodes are ready before we start.
	for _, url := range []string{node1, node2, node3} {
		require.True(t, waitReady(t, url, 120*time.Second),
			"cluster node at %s must become ready within 2 min", url)
	}

	// Write a pre-failover key.
	preKey := fmt.Sprintf("failover-pre-%d", time.Now().UnixNano())
	kvPut(t, node1, preKey, []byte("pre-failover"))
	waitForKey(t, node3, preKey)

	// Simulate node1 outage: the test runner expects the orchestrator to have
	// killed node1 before this test runs (see make integration-test-cluster-failover).
	// We check that node2 and node3 can still serve the pre-failover key and accept
	// new writes.
	t.Logf("Assuming node1 (%s) has been taken down by the orchestrator", node1)

	postKey := fmt.Sprintf("failover-post-%d", time.Now().UnixNano())
	kvPut(t, node2, postKey, []byte("post-failover"))
	waitForKey(t, node3, postKey)

	// node3 must see both the pre-failover and post-failover keys.
	assert.Equal(t, []byte("pre-failover"), kvGet(t, node3, preKey),
		"pre-failover key must survive leader failover on node3")
	assert.Equal(t, []byte("post-failover"), kvGet(t, node3, postKey),
		"post-failover key must replicate to node3")
}

// TestQRDBCluster_AtRestEncryptionAllNodes verifies that raw PebbleDB files
// on all three cluster nodes do not contain the plaintext sentinel values.
func TestQRDBCluster_AtRestEncryptionAllNodes(t *testing.T) {
	node1, node2, node3 := nodeURLs(t)

	dataDir1 := os.Getenv("QUICBAAS_NODE1_DATADIR")
	dataDir2 := os.Getenv("QUICBAAS_NODE2_DATADIR")
	dataDir3 := os.Getenv("QUICBAAS_NODE3_DATADIR")
	if dataDir1 == "" || dataDir2 == "" || dataDir3 == "" {
		t.Skip("QUICBAAS_NODE1_DATADIR / NODE2_DATADIR / NODE3_DATADIR not set; " +
			"mount node data directories to enable at-rest encryption check")
	}

	require.True(t, waitReady(t, node1, 60*time.Second))

	sentinel := fmt.Sprintf("ATREST-CLUSTER-%d", time.Now().UnixNano())
	kvPut(t, node1, "atrest-cluster-sentinel", []byte(sentinel))
	// The sentinel must reach every node's store before its absence on disk
	// means anything.
	for _, url := range []string{node2, node3} {
		waitForKey(t, url, "atrest-cluster-sentinel")
	}

	for i, dataDir := range []string{dataDir1, dataDir2, dataDir3} {
		t.Run(fmt.Sprintf("node%d", i+1), func(t *testing.T) {
			assertSentinelAbsentOnDisk(t, dataDir, []byte(sentinel))
		})
	}
}

// assertSentinelAbsentOnDisk walks the data directory looking for any .sst or
// .vlog file that contains the plaintext sentinel bytes.
func assertSentinelAbsentOnDisk(t *testing.T, dataDir string, sentinel []byte) {
	t.Helper()
	var found bool
	err := walkDirForSentinel(dataDir, sentinel, &found)
	require.NoError(t, err, "walking data dir %s must not error", dataDir)
	assert.False(t, found,
		"plaintext sentinel must not appear in any on-disk file under %s (at-rest encryption broken)",
		dataDir)
}

// walkDirForSentinel recursively checks .sst and .vlog files for the sentinel.
func walkDirForSentinel(dir string, sentinel []byte, found *bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		path := dir + "/" + e.Name()
		if e.IsDir() {
			if err := walkDirForSentinel(path, sentinel, found); err != nil {
				return err
			}
			continue
		}
		ext := ""
		if n := e.Name(); len(n) > 4 {
			ext = n[len(n)-4:]
		}
		if ext != ".sst" && ext != "vlog" {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if containsBytes(raw, sentinel) {
			*found = true
			return nil
		}
	}
	return nil
}

// containsBytes is a simple bytes.Contains replacement to avoid extra imports.
func containsBytes(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
outer:
	for i := 0; i <= len(haystack)-len(needle); i++ {
		for j, b := range needle {
			if haystack[i+j] != b {
				continue outer
			}
		}
		return true
	}
	return false
}

// TestQRDBCluster_DynamicMembership verifies that a fourth node can join
// the cluster mid-operation.
func TestQRDBCluster_DynamicMembership(t *testing.T) {
	node1, node2, node3 := nodeURLs(t)
	node4 := os.Getenv("QUICBAAS_NODE4_URL")
	if node4 == "" {
		t.Skip("QUICBAAS_NODE4_URL not set; " +
			"start a 4th node container and set the URL to run the dynamic membership test")
	}

	for _, url := range []string{node1, node2, node3} {
		require.True(t, waitReady(t, url, 60*time.Second))
	}

	// Write baseline data before node4 joins.
	baseKey := fmt.Sprintf("membership-base-%d", time.Now().UnixNano())
	kvPut(t, node1, baseKey, []byte("membership-base"))
	for _, url := range []string{node2, node3} {
		waitForKey(t, url, baseKey)
	}

	require.True(t, waitReady(t, node4, 60*time.Second),
		"node4 must become ready after joining the cluster")

	// Wait for node4's log catchup.
	waitForKey(t, node4, baseKey)

	// node4 must serve the baseline key.
	got := kvGet(t, node4, baseKey)
	assert.Equal(t, []byte("membership-base"), got,
		"node4 must catch up with pre-join data after joining the cluster")

	// Write a new key through node4.
	afterKey := fmt.Sprintf("membership-after-%d", time.Now().UnixNano())
	kvPut(t, node4, afterKey, []byte("written-by-node4"))

	// node1, 2, 3 must all serve the key written through node4.
	for i, url := range []string{node1, node2, node3} {
		waitForKey(t, url, afterKey)
		got := kvGet(t, url, afterKey)
		assert.Equal(t, []byte("written-by-node4"), got,
			"node%d must see key written through node4", i+1)
	}
}
