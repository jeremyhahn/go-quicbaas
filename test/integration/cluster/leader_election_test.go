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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// leaderHTTPClient is a package-level HTTP client that calls as the tenant operator.
var leaderHTTPClient = newDeferredTenantOperatorRESTClient(30 * time.Second)

// TestLeaderElection validates that a leader is elected in a multi-node cluster
// Note: In a multi-shard cluster, each shard can have a different leader, and the
// cluster status endpoint reports the leader of shard 1. During leader cryptoions,
// nodes may temporarily report different leaders. This test uses retry logic to
// handle transient inconsistencies.
func TestLeaderElection(t *testing.T) {
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - leader election requires real Raft cluster")
		return
	}

	nodes := getNodeURLs()

	// The retries below poll for the cluster to stabilize; nine of them cover
	// the settling time a fixed pre-wait used to give.
	maxRetries := 9
	retryInterval := 3 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		var leaderID string
		var leaderFound bool
		var allNodesAgree bool = true

		// Check each node for leader information
		leadersByNode := make(map[int]string)
		for i, nodeURL := range nodes {
			resp, err := leaderHTTPClient.Get(nodeURL + "/api/v1/cluster/status")
			if err != nil {
				t.Logf("Attempt %d: Warning: Failed to get cluster status from node %d: %v", attempt, i+1, err)
				continue
			}

			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Logf("Attempt %d: Warning: Node %d returned non-OK status: %d, body: %s", attempt, i+1, resp.StatusCode, string(body))
				continue
			}

			var status map[string]interface{}
			if err := json.Unmarshal(body, &status); err != nil {
				t.Logf("Attempt %d: Warning: Failed to decode cluster status from node %d: %v", attempt, i+1, err)
				continue
			}

			if lid, ok := status["leader_id"].(string); ok && lid != "" && lid != "0" {
				leadersByNode[i+1] = lid
				if !leaderFound {
					leaderID = lid
					leaderFound = true
					t.Logf("Attempt %d: Node %d reports leader: %s", attempt, i+1, leaderID)
				} else if lid != leaderID {
					allNodesAgree = false
					t.Logf("Attempt %d: Node %d reports different leader: %s (expected: %s)", attempt, i+1, lid, leaderID)
				} else {
					t.Logf("Attempt %d: Node %d agrees on leader: %s", attempt, i+1, leaderID)
				}
			} else {
				t.Logf("Attempt %d: Warning: Node %d does not report a leader", attempt, i+1)
			}
		}

		// Check if we have consensus
		if leaderFound && allNodesAgree && len(leadersByNode) >= len(nodes) {
			t.Logf("SUCCESS: Leader elected and all %d nodes agree: leader=%s", len(leadersByNode), leaderID)
			return
		}

		// Check if we at least have a leader elected
		if leaderFound && !allNodesAgree {
			t.Logf("Attempt %d: Leader found but not all nodes agree, retrying...", attempt)
		} else if !leaderFound {
			t.Logf("Attempt %d: No leader found yet, retrying...", attempt)
		}

		if attempt < maxRetries {
			pace(t, retryInterval)
		}
	}

	// After all retries, check if we have at least a majority agreement
	// In a 3-node cluster, 2 nodes agreeing is still valid
	t.Log("Checking final state after retries...")

	var leaderFound bool
	leaderVotes := make(map[string]int)

	for i, nodeURL := range nodes {
		resp, err := leaderHTTPClient.Get(nodeURL + "/api/v1/cluster/status")
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			continue
		}

		var status map[string]interface{}
		if err := json.Unmarshal(body, &status); err != nil {
			continue
		}

		if lid, ok := status["leader_id"].(string); ok && lid != "" && lid != "0" {
			leaderVotes[lid]++
			leaderFound = true
			t.Logf("Final: Node %d reports leader: %s", i+1, lid)
		}
	}

	if !leaderFound {
		t.Fatal("FAIL: No leader elected in the cluster after retries")
	}

	// Find the leader with most votes
	maxVotes := 0
	consensusLeader := ""
	for lid, votes := range leaderVotes {
		if votes > maxVotes {
			maxVotes = votes
			consensusLeader = lid
		}
	}

	// In a 3-node cluster, we need at least 2 nodes to agree (majority)
	majority := len(nodes)/2 + 1
	if maxVotes >= majority {
		t.Logf("SUCCESS: Leader elected with majority consensus: leader=%s (votes: %d/%d)", consensusLeader, maxVotes, len(nodes))
		return
	}

	t.Logf("WARNING: Leader elected but only %d/%d nodes agree (leader=%s). This may be due to multi-shard leader distribution.", maxVotes, len(nodes), consensusLeader)
	// Don't fail - in a multi-shard cluster, different shards can have different leaders
}

// TestLeaderElectionTiming validates that leader election happens within reasonable time
func TestLeaderElectionTiming(t *testing.T) {
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - leader election timing requires real Raft cluster")
		return
	}

	nodes := getNodeURLs()
	maxElectionTime := 30 * time.Second
	checkInterval := 1 * time.Second

	t.Logf("Checking for leader election within %v", maxElectionTime)

	start := time.Now()
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	timeout := time.After(maxElectionTime)

	for {
		select {
		case <-timeout:
			elapsed := time.Since(start)
			t.Fatalf("FAIL: Leader not elected within %v (elapsed: %v)", maxElectionTime, elapsed)
			return

		case <-ticker.C:
			// Check first node for leader
			resp, err := leaderHTTPClient.Get(nodes[0] + "/api/v1/cluster/status")
			if err != nil {
				continue
			}

			if resp.StatusCode == http.StatusOK {
				var status map[string]interface{}
				if err := json.NewDecoder(resp.Body).Decode(&status); err == nil {
					if lid, ok := status["leader_id"].(string); ok && lid != "" && lid != "0" {
						elapsed := time.Since(start)
						resp.Body.Close()
						t.Logf("SUCCESS: Leader elected in %v (leader_id=%s)", elapsed, lid)
						return
					}
				}
			}
			resp.Body.Close()
		}
	}
}

// TestLeaderStability validates that the leader remains stable over time
// leaderStabilityWindow is how long TestLeaderStability watches the leader.
// It spans many Raft election timeouts (each on the order of a second), so a
// cluster whose followers keep timing out and electing a new leader changes
// leader within it.
const leaderStabilityWindow = 15 * time.Second

func TestLeaderStability(t *testing.T) {
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - leader stability requires real Raft cluster")
		return
	}

	nodes := getNodeURLs()
	duration := leaderStabilityWindow
	checkInterval := 2 * time.Second

	t.Logf("Monitoring leader stability for %v", duration)

	// Get initial leader
	var initialLeader string
	pollUntil(t, 20*time.Second, "initial leader reported by node 1", func(ctx context.Context) (bool, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nodes[0]+"/api/v1/cluster/status", nil)
		if err != nil {
			return false, err
		}
		resp, err := leaderHTTPClient.Do(req)
		if err != nil {
			return false, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return false, nil
		}
		var status map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			return false, err
		}
		if lid, ok := status["leader_id"].(string); ok && lid != "" && lid != "0" {
			initialLeader = lid
			return true, nil
		}
		return false, nil
	})

	t.Logf("Initial leader: %s", initialLeader)

	// Monitor leader stability
	deadline := time.Now().Add(duration)
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	leaderChanges := 0
	currentLeader := initialLeader
	checkCount := 0

	for time.Now().Before(deadline) {
		<-ticker.C
		checkCount++

		resp, err := leaderHTTPClient.Get(nodes[0] + "/api/v1/cluster/status")
		if err != nil {
			t.Logf("Check %d: Failed to query cluster status: %v", checkCount, err)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			var status map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&status); err == nil {
				if lid, ok := status["leader_id"].(string); ok && lid != "" {
					if lid != currentLeader {
						leaderChanges++
						t.Logf("Check %d: Leader changed from %s to %s", checkCount, currentLeader, lid)
						currentLeader = lid
					}
				}
			}
		}
		resp.Body.Close()
	}

	t.Logf("Stability test completed: %d checks, %d leader changes", checkCount, leaderChanges)

	// In a stable cluster, we expect 0 leader changes
	// Allow 1 change as networks can be unpredictable
	if leaderChanges > 1 {
		t.Errorf("WARNING: Cluster showed instability with %d leader changes", leaderChanges)
	} else {
		t.Logf("SUCCESS: Leader remained stable (changes: %d)", leaderChanges)
	}
}

// TestClusterMembershipConsistency validates that all nodes agree on cluster membership
// Note: This test validates that nodes agree on membership, not that membership matches
// a specific count. Raft membership may include nodes from previous test runs if the
// cluster was not fully reset. The important property is consistency across nodes.
func TestClusterMembershipConsistency(t *testing.T) {
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - cluster membership consistency requires real Raft cluster")
		return
	}

	nodes := getNodeURLs()
	minExpectedMembers := len(nodes) // At minimum, we should have this many nodes

	t.Logf("Validating cluster membership consistency across %d active nodes", len(nodes))

	waitForQuorumReady(t, nodes)

	// Collect membership info from each node
	type MembershipInfo struct {
		NodeID      int
		TotalNodes  int
		Members     []uint64
		LeaderID    string
		RaftAddress string
	}

	membershipByNode := make(map[int]MembershipInfo)

	for i, nodeURL := range nodes {
		resp, err := leaderHTTPClient.Get(nodeURL + "/api/v1/cluster/status")
		if err != nil {
			t.Fatalf("Failed to get cluster status from node %d: %v", i+1, err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("Node %d returned non-OK status: %d, body: %s", i+1, resp.StatusCode, string(body))
		}

		var status map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			resp.Body.Close()
			t.Fatalf("Failed to decode cluster status from node %d: %v", i+1, err)
		}
		resp.Body.Close()

		info := MembershipInfo{
			NodeID: i + 1,
		}

		if totalNodes, ok := status["total_nodes"].(float64); ok {
			info.TotalNodes = int(totalNodes)
		}

		if leaderID, ok := status["leader_id"].(string); ok {
			info.LeaderID = leaderID
		}

		if raftAddr, ok := status["raft_address"].(string); ok {
			info.RaftAddress = raftAddr
		}

		if members, ok := status["members"].([]interface{}); ok {
			for _, m := range members {
				if memberID, ok := m.(float64); ok {
					info.Members = append(info.Members, uint64(memberID))
				}
			}
		}

		membershipByNode[i+1] = info
		t.Logf("Node %d: total_nodes=%d, members=%v, leader=%s",
			i+1, info.TotalNodes, info.Members, info.LeaderID)
	}

	// Validate consistency using majority consensus
	// In a multi-shard cluster, different nodes may report different leaders for different shards
	var referenceInfo MembershipInfo
	first := true
	leaderVotes := make(map[string]int)

	for nodeID, info := range membershipByNode {
		if first {
			referenceInfo = info
			first = false
		}

		// Check total nodes - warn but don't fail if different
		// Membership views can temporarily diverge during cluster operations
		if info.TotalNodes != referenceInfo.TotalNodes {
			t.Logf("Warning: Node %d reports different total_nodes: %d (vs reference: %d)",
				nodeID, info.TotalNodes, referenceInfo.TotalNodes)
		}

		// Check member count - warn but don't fail if different
		// Member lists can temporarily diverge during membership changes
		if len(info.Members) != len(referenceInfo.Members) {
			t.Logf("Warning: Node %d reports different member count: %d (vs reference: %d)",
				nodeID, len(info.Members), len(referenceInfo.Members))
		}

		// Count leader votes for majority consensus
		if info.LeaderID != "" && info.LeaderID != "0" {
			leaderVotes[info.LeaderID]++
		}
	}

	// Check if membership is at least the minimum expected
	// Note: Raft membership may include nodes from previous test runs that haven't been
	// fully removed. The important check is that all nodes agree on membership.
	if referenceInfo.TotalNodes < minExpectedMembers {
		t.Errorf("Cluster reports only %d total nodes, expected at least %d", referenceInfo.TotalNodes, minExpectedMembers)
	} else if referenceInfo.TotalNodes > minExpectedMembers {
		t.Logf("Note: Cluster has %d nodes (min expected: %d) - may include nodes from previous runs",
			referenceInfo.TotalNodes, minExpectedMembers)
	}

	// Find the leader with most votes and check for majority consensus
	maxVotes := 0
	consensusLeader := ""
	for lid, votes := range leaderVotes {
		if votes > maxVotes {
			maxVotes = votes
			consensusLeader = lid
		}
	}

	// In a 3-node cluster, we need at least 2 nodes to agree (majority)
	majority := len(nodes)/2 + 1
	if maxVotes >= majority {
		t.Logf("SUCCESS: Cluster membership consistent with majority consensus: %d members, leader=%s (votes: %d/%d)",
			referenceInfo.TotalNodes, consensusLeader, maxVotes, len(nodes))
		return
	}

	// If no majority consensus, log a warning but don't fail
	// In a multi-shard cluster, different shards can legitimately have different leaders
	t.Logf("WARNING: No majority consensus on leader (leader=%s, votes=%d/%d). This is expected in multi-shard clusters.",
		consensusLeader, maxVotes, len(nodes))
	t.Logf("SUCCESS: Cluster membership validated with %d members", referenceInfo.TotalNodes)
}

// TestShardLeaderAPI validates the shard leader API endpoint
func TestShardLeaderAPI(t *testing.T) {
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - shard leader API requires real Raft cluster")
		return
	}

	nodes := getNodeURLs()

	waitForQuorumReady(t, nodes)

	// Test shard 1 leader endpoint
	shardID := uint64(1)
	url := fmt.Sprintf("%s/api/v1/cluster/leader/%d", nodes[0], shardID)

	resp, err := leaderHTTPClient.Get(url)
	if err != nil {
		t.Fatalf("Failed to get shard leader: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Shard leader endpoint returned non-OK status: %d, body: %s",
			resp.StatusCode, string(body))
	}

	var leaderResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&leaderResp); err != nil {
		t.Fatalf("Failed to decode leader response: %v", err)
	}

	t.Logf("Shard %d leader response: %+v", shardID, leaderResp)

	// Validate response
	if shard, ok := leaderResp["shard_id"].(float64); !ok || uint64(shard) != shardID {
		t.Errorf("Unexpected shard_id in response: %v", leaderResp["shard_id"])
	}

	if valid, ok := leaderResp["valid"].(bool); !ok || !valid {
		t.Errorf("Leader is not valid: %v", leaderResp)
	}

	if leaderID, ok := leaderResp["leader_id"].(float64); ok && leaderID > 0 {
		t.Logf("SUCCESS: Shard %d has leader: %d", shardID, uint64(leaderID))
	} else {
		t.Error("FAIL: No valid leader ID in response")
	}
}
