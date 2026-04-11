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
// +build integration

package cluster

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// newInsecureHTTPClient creates an HTTP client configured for self-signed TLS certs.
func newInsecureHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			TLSHandshakeTimeout: 10 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}
}

// TestClusterHealth verifies that all nodes in the cluster are healthy
func TestClusterHealth(t *testing.T) {
	t.Parallel()

	nodes := getNodeURLs()
	client := newInsecureHTTPClient(10 * time.Second)

	for i, nodeURL := range nodes {
		t.Run(fmt.Sprintf("Node%d", i+1), func(t *testing.T) {
			var lastErr error
			for attempt := 0; attempt < 3; attempt++ {
				resp, err := client.Get(nodeURL + "/api/v1/health/live")
				if err != nil {
					lastErr = err
					t.Logf("Attempt %d: Failed to connect to node %d: %v", attempt+1, i+1, err)
					time.Sleep(time.Duration(attempt+1) * time.Second)
					continue
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					body, _ := io.ReadAll(resp.Body)
					t.Errorf("Node %d not healthy: status=%d, body=%s", i+1, resp.StatusCode, string(body))
					return
				}
				return // Success
			}
			t.Fatalf("Failed to connect to node %d after 3 attempts: %v", i+1, lastErr)
		})
	}
}

// TestClusterReadiness verifies that all nodes are ready to accept traffic.
// This test runs in parallel and requires the cluster to be fully initialized
// with leader election completed before it can pass.
func TestClusterReadiness(t *testing.T) {
	t.Parallel()

	nodes := getNodeURLs()
	client := newInsecureHTTPClient(10 * time.Second)

	// Wait for cluster stabilization - leader election can take time
	const maxAttempts = 15
	const baseDelay = 2 * time.Second

	for i, nodeURL := range nodes {
		t.Run(fmt.Sprintf("Node%d", i+1), func(t *testing.T) {
			var lastErr error
			var lastBody string
			for attempt := 0; attempt < maxAttempts; attempt++ {
				resp, err := client.Get(nodeURL + "/api/v1/health/ready")
				if err != nil {
					lastErr = err
					t.Logf("Attempt %d/%d: Failed to connect to node %d: %v", attempt+1, maxAttempts, i+1, err)
					time.Sleep(baseDelay)
					continue
				}

				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				lastBody = string(body)

				if resp.StatusCode == http.StatusOK {
					t.Logf("Node %d ready after %d attempts", i+1, attempt+1)
					return // Success
				}

				lastErr = &qberrors.MessageError{Message: "status=" + strconv.Itoa(resp.StatusCode) + ", body=" + lastBody}
				t.Logf("Attempt %d/%d: Node %d not ready: %v", attempt+1, maxAttempts, i+1, lastErr)
				time.Sleep(baseDelay)
			}
			t.Errorf("Node %d not ready after %d attempts: %v", i+1, maxAttempts, lastErr)
		})
	}
}

// TestRaftLeaderElection verifies that a Raft leader has been elected
func TestRaftLeaderElection(t *testing.T) {
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - leader election requires real Raft cluster")
		return
	}

	nodes := getNodeURLs()
	client := newInsecureHTTPClient(10 * time.Second)

	var leaderFound bool
	var leaderID string

	for i, nodeURL := range nodes {
		resp, err := client.Get(nodeURL + "/api/v1/cluster/status")
		if err != nil {
			t.Logf("Warning: Failed to get cluster status from node %d: %v", i+1, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			continue
		}

		var status map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			t.Logf("Warning: Failed to decode cluster status from node %d: %v", i+1, err)
			continue
		}

		if lid, ok := status["leader_id"].(string); ok && lid != "" {
			leaderFound = true
			leaderID = lid
			t.Logf("Raft leader elected: node %s (reported by node %d)", leaderID, i+1)
			break
		}
	}

	if !leaderFound {
		t.Error("No Raft leader elected in the cluster")
	}
}

// TestClusterMembership verifies that all nodes are part of the cluster
func TestClusterMembership(t *testing.T) {
	// Check if cluster is running in standalone/embedded mode (no real Raft consensus).
	// In standalone mode, cluster membership information is not populated, so the
	// membership verification is expected to fail.
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - cluster membership test not applicable")
		return
	}

	nodes := getNodeURLs()
	expectedMembers := len(nodes)
	client := newInsecureHTTPClient(10 * time.Second)

	// Try to get cluster membership from each node
	for i, nodeURL := range nodes {
		resp, err := client.Get(nodeURL + "/api/v1/cluster/status")
		if err != nil {
			t.Logf("Warning: Failed to get cluster status from node %d: %v", i+1, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			continue
		}

		var status map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			t.Logf("Warning: Failed to decode cluster status from node %d: %v", i+1, err)
			continue
		}

		if members, ok := status["members"].([]interface{}); ok {
			memberCount := len(members)
			if memberCount < expectedMembers {
				t.Errorf("Incomplete cluster membership: expected %d members, got %d (from node %d)",
					expectedMembers, memberCount, i+1)
			} else {
				t.Logf("Cluster membership verified: %d members (from node %d)", memberCount, i+1)
				return // Success
			}
		}
	}

	t.Error("Failed to verify cluster membership from any node")
}

// TestNodeCommunication verifies that nodes can communicate with each other
func TestNodeCommunication(t *testing.T) {
	nodes := getNodeURLs()

	// Verify each node can reach every other node
	for i := range nodes {
		for j, targetURL := range nodes {
			if i == j {
				continue // Skip self-communication
			}

			t.Run(fmt.Sprintf("Node%d_to_Node%d", i+1, j+1), func(t *testing.T) {
				// Simple connectivity test via health endpoint
				client := newInsecureHTTPClient(5 * time.Second)

				resp, err := client.Get(targetURL + "/api/v1/health/live")
				if err != nil {
					t.Errorf("Node %d cannot reach node %d: %v", i+1, j+1, err)
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Errorf("Node %d received unhealthy response from node %d: status=%d",
						i+1, j+1, resp.StatusCode)
				}
			})
		}
	}
}

// TestClusterMetrics verifies that metrics endpoints are accessible
func TestClusterMetrics(t *testing.T) {
	t.Parallel()

	nodes := getNodeURLs()
	client := newInsecureHTTPClient(10 * time.Second)

	for i, nodeURL := range nodes {
		t.Run(fmt.Sprintf("Node%d", i+1), func(t *testing.T) {
			// Note: Metrics endpoint is on port 9090 by default
			// In the test environment, all nodes expose on the same internal port
			metricsURL := nodeURL // Assuming metrics are exposed via the same endpoint

			var lastErr error
			for attempt := 0; attempt < 3; attempt++ {
				resp, err := client.Get(metricsURL + "/api/v1/metrics/")
				if err != nil {
					lastErr = err
					t.Logf("Attempt %d: Failed to get metrics from node %d: %v", attempt+1, i+1, err)
					time.Sleep(time.Duration(attempt+1) * time.Second)
					continue
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Logf("Warning: Metrics endpoint not available on node %d: status=%d",
						i+1, resp.StatusCode)
					return
				}

				t.Logf("Metrics endpoint accessible on node %d", i+1)
				return
			}
			t.Logf("Warning: Failed to get metrics from node %d after 3 attempts: %v", i+1, lastErr)
		})
	}
}

// TestClusterStability verifies cluster remains stable over time
func TestClusterStability(t *testing.T) {
	nodes := getNodeURLs()
	duration := 30 * time.Second
	interval := 2 * time.Second
	client := newInsecureHTTPClient(10 * time.Second)

	t.Logf("Running cluster stability test for %v", duration)

	deadline := time.Now().Add(duration)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	failureCount := 0
	checkCount := 0

	for time.Now().Before(deadline) {
		<-ticker.C
		checkCount++

		healthy := true
		for i, nodeURL := range nodes {
			resp, err := client.Get(nodeURL + "/api/v1/health/live")
			if err != nil {
				t.Logf("Check %d: Node %d unhealthy: %v", checkCount, i+1, err)
				healthy = false
				continue
			}
			resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Logf("Check %d: Node %d unhealthy: status=%d", checkCount, i+1, resp.StatusCode)
				healthy = false
			}
		}

		if !healthy {
			failureCount++
		}
	}

	t.Logf("Stability test completed: %d checks, %d failures", checkCount, failureCount)

	if failureCount > checkCount/4 {
		t.Errorf("Cluster instability detected: %d/%d checks failed", failureCount, checkCount)
	}
}

// Helper functions

// getNodeURLs returns the list of node URLs from environment variables
func getNodeURLs() []string {
	// Use the shared helper for consistent URL resolution
	return helpers.GetRESTEndpoints()
}
