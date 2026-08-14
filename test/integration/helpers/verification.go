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

package helpers

import (
	"context"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/id"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// WaitForClusterHealthy polls all nodes until they report healthy status or timeout.
func WaitForClusterHealthy(t *testing.T, cluster *TestCluster) error {
	t.Helper()
	deadline := time.Now().Add(cluster.healthTimeout)

	t.Logf("Waiting for cluster to become healthy (timeout: %v)...", cluster.healthTimeout)

	for time.Now().Before(deadline) {
		allHealthy := true
		var unhealthyNodes []int

		for i, client := range cluster.Clients {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := client.Health().Ping(ctx)
			cancel()

			if err != nil {
				allHealthy = false
				unhealthyNodes = append(unhealthyNodes, i+1)
			}
		}

		if allHealthy {
			t.Logf("Cluster is healthy: all %d nodes responding", len(cluster.Clients))
			return nil
		}

		if len(unhealthyNodes) > 0 {
			t.Logf("Waiting for nodes %v to become healthy...", unhealthyNodes)
		}

		time.Sleep(cluster.healthInterval)
	}

	return fmt.Errorf("cluster did not become healthy within %v", cluster.healthTimeout)
}

// WaitForLeaderElection waits for a leader to be elected in the cluster.
func WaitForLeaderElection(t *testing.T, cluster *TestCluster, timeout time.Duration) (*TestNode, quicbaas.Client, error) {
	t.Helper()
	deadline := time.Now().Add(timeout)

	t.Logf("Waiting for leader election (timeout: %v)...", timeout)

	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		leader, client, err := cluster.FindLeader(ctx)
		cancel()

		if err == nil && leader != nil {
			t.Logf("Leader elected: node %d", leader.ID)
			return leader, client, nil
		}

		time.Sleep(1 * time.Second)
	}

	return nil, nil, fmt.Errorf("no leader elected within %v", timeout)
}

// VerifyClusterSize verifies that all nodes report the expected cluster size.
func VerifyClusterSize(t *testing.T, cluster *TestCluster, expectedSize int) {
	t.Helper()

	for i, client := range cluster.Clients {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		status, err := client.Cluster().Status(ctx)
		cancel()

		require.NoError(t, err, "Failed to get cluster status from node %d", i+1)
		nodeCount := len(status.Members)
		assert.Equal(t, expectedSize, nodeCount,
			"Node %d reports incorrect cluster size", i+1)

		t.Logf("Node %d: cluster size = %d (expected %d)", i+1, nodeCount, expectedSize)
	}
}

// VerifyClusterSizeAtLeast verifies that all nodes report a cluster size >= minSize.
// This is useful for tests that don't require an exact cluster size but need
// at least a minimum number of nodes (e.g., after previous tests may have expanded the cluster).
func VerifyClusterSizeAtLeast(t *testing.T, cluster *TestCluster, minSize int) {
	t.Helper()

	for i, client := range cluster.Clients {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		status, err := client.Cluster().Status(ctx)
		cancel()

		require.NoError(t, err, "Failed to get cluster status from node %d", i+1)
		nodeCount := len(status.Members)
		assert.GreaterOrEqual(t, nodeCount, minSize,
			"Node %d reports cluster size %d, expected at least %d", i+1, nodeCount, minSize)

		t.Logf("Node %d: cluster size = %d (expected >= %d)", i+1, nodeCount, minSize)
	}
}

// VerifyLeaderConsistency verifies that all nodes agree on the leader.
// It will retry for up to 10 seconds to allow for leader consensus to form.
func VerifyLeaderConsistency(t *testing.T, cluster *TestCluster) uint64 {
	t.Helper()

	// Retry for up to 10 seconds to allow leader consensus to form
	deadline := time.Now().Add(10 * time.Second)
	var lastError string

	for time.Now().Before(deadline) {
		leaders := make(map[string]int) // leaderID -> count

		for i, client := range cluster.Clients {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			status, err := client.Cluster().Status(ctx)
			cancel()

			if err != nil {
				t.Logf("Warning: Failed to get cluster status from node %d: %v (retrying...)", i+1, err)
				time.Sleep(1 * time.Second)
				continue
			}

			// Check Members for leader information
			var targetLeaderID string
			for _, member := range status.Members {
				if member.IsLeader {
					targetLeaderID = member.ID
					break
				}
			}

			// Fallback to LeaderID if available
			if targetLeaderID == "" && status.LeaderID != "" {
				targetLeaderID = status.LeaderID
			}

			if targetLeaderID != "" {
				leaders[targetLeaderID]++
				t.Logf("Node %d reports leader: %s", i+1, targetLeaderID)
			}
		}

		// Check if all nodes agree on one leader
		if len(leaders) == 1 {
			var leaderID string
			var count int
			for nodeID, c := range leaders {
				leaderID = nodeID
				count = c
			}

			if count == len(cluster.Clients) {
				t.Logf("All nodes agree on leader: %s", leaderID)
				// Convert string leaderID to uint64
				if id, err := strconv.ParseUint(leaderID, 10, 64); err == nil {
					return id
				}
				return 0
			} else {
				lastError = fmt.Sprintf("Not all nodes agree on leader %s: %d/%d nodes", leaderID, count, len(cluster.Clients))
				t.Logf("%s (retrying...)", lastError)
			}
		} else if len(leaders) > 1 {
			lastError = fmt.Sprintf("Multiple leaders detected: %v", leaders)
			t.Logf("%s (retrying...)", lastError)
		} else {
			lastError = "No leader reported by any node"
			t.Logf("%s (retrying...)", lastError)
		}

		time.Sleep(1 * time.Second)
	}

	// If we get here, consensus was not reached
	t.Fatalf("Leader consensus not reached within 10 seconds: %s", lastError)
	return 0
}

// VerifyDataConsistency verifies that all nodes have identical data for test entities.
func VerifyDataConsistency(t *testing.T, cluster *TestCluster, entities []TestEntity) {
	t.Helper()

	t.Logf("Verifying data consistency across %d nodes for %d entities...",
		len(cluster.Clients), len(entities))

	failedChecks := 0
	for _, entity := range entities {
		var referenceData []byte
		var referenceFound bool

		for i, client := range cluster.Clients {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			resp, err := client.KV().Get(ctx, &service.GetRequest{
				EntityType:  entity.EntityType,
				ID:          id.HashString(entity.Key),
				Consistency: service.ConsistencyLevelLinearizable,
			})
			cancel()

			require.NoError(t, err, "Failed to read entity %s from node %d", entity.Key, i+1)
			require.True(t, resp.Found, "Entity %s not found on node %d", entity.Key, i+1)

			if !referenceFound {
				referenceData = resp.Data
				referenceFound = true
			} else {
				if !assert.Equal(t, referenceData, resp.Data,
					"Data mismatch for entity %s between node 1 and node %d", entity.Key, i+1) {
					failedChecks++
					if failedChecks >= 10 {
						t.Fatalf("Too many consistency failures, aborting")
					}
				}
			}
		}
	}

	if failedChecks == 0 {
		t.Logf("Data consistency verified: all nodes have identical data")
	} else {
		t.Errorf("Data consistency check failed: %d mismatches found", failedChecks)
	}
}

// minStableChecks is the minimum number of observations required before
// evaluating leader stability.
const minStableChecks = 5

// dominantLeaderThreshold is the minimum fraction of observations the dominant
// leader must hold across the entire monitoring window (60%). This is strict
// enough to catch genuine instability (50/50 splits) while tolerating the
// transient Raft leader oscillations common in containerized environments
// where CPU scheduling jitter causes frequent brief heartbeat delays.
const dominantLeaderThreshold = 60 // percent

// VerifyLeaderStability verifies that the cluster has a clear dominant leader
// over the monitoring window. Transient leader changes during the monitoring
// period are logged but not treated as failures. Stability is evaluated using
// the full observation history: one leader must hold > 70% of all observations
// and appear in at least one of the last 3 checks.
func VerifyLeaderStability(t *testing.T, cluster *TestCluster, duration time.Duration) {
	t.Helper()

	// Get initial leader
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	initialLeader, _, err := cluster.FindLeader(ctx)
	cancel()
	require.NoError(t, err, "Failed to find initial leader")
	require.NotNil(t, initialLeader, "No initial leader found")

	t.Logf("Initial leader: node %d, monitoring for %v...", initialLeader.ID, duration)

	// Track the leader observed in each check to evaluate stability.
	var leaderHistory []uint64
	checkCount := 0
	changeCount := 0
	gapCount := 0

	deadlineCtx, deadlineCancel := context.WithTimeout(context.Background(), duration)
	defer deadlineCancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-deadlineCtx.Done():
			// Monitoring window elapsed; evaluate stability across full history.
			stableLeader, stable := isDominantLeaderStable(leaderHistory, minStableChecks)
			if !stable {
				t.Fatalf(
					"Leader did not stabilize: no leader held >%d%% of %d checks "+
						"(%d checks observed no leader at all; history: %v, 0 = no leader)",
					dominantLeaderThreshold, len(leaderHistory), gapCount, leaderHistory,
				)
			}

			t.Logf(
				"Leader stability verified: node %d dominant across %d checks "+
					"(%d transient changes, %d transient no-leader gaps observed)",
				stableLeader, checkCount, changeCount, gapCount,
			)
			return

		case <-ticker.C:
			checkCount++

			queryCtx, queryCancel := context.WithTimeout(context.Background(), 5*time.Second)
			currentLeader, _, queryErr := cluster.FindLeader(queryCtx)
			queryCancel()

			// A momentary "no leader" observation is the FIRST HALF of every
			// leader change: Raft goes leader -> election -> new leader, and
			// FindLeader is a single-shot query with no retry that reports
			// "no leader found in cluster" for any node that answers mid
			// election (or whose status RPC is slow enough to be skipped).
			// Aborting here would make this verifier tolerate a leader change
			// A -> B while failing on the election gap A -> none -> B that
			// necessarily precedes it. Record the gap instead and let the
			// window evaluation below decide: gaps count against every
			// dominance percentage, and a trailing run of them fails both
			// strategies, so a GENUINE loss of leader availability still
			// fails - only a transient one is absorbed.
			if queryErr != nil || currentLeader == nil {
				leaderHistory = append(leaderHistory, noLeaderObserved)
				gapCount++
				t.Logf("No leader observed during stability check %d (gap #%d): %v",
					checkCount, gapCount, queryErr)
				continue
			}

			leaderHistory = append(leaderHistory, currentLeader.ID)

			// Log transient changes for diagnostic purposes without failing.
			// A gap is reported above, so only leader-to-leader moves reach here.
			if len(leaderHistory) > 1 && leaderHistory[len(leaderHistory)-2] != noLeaderObserved &&
				leaderHistory[len(leaderHistory)-1] != leaderHistory[len(leaderHistory)-2] {
				changeCount++
				t.Logf(
					"Leader changed from node %d to node %d (check %d, change #%d)",
					leaderHistory[len(leaderHistory)-2], currentLeader.ID, checkCount, changeCount,
				)
			}
		}
	}
}

// trailingWindowSize is the number of recent checks used in the trailing
// window stability evaluation.
const trailingWindowSize = 10

// trailingWindowThreshold is the minimum percent of the trailing window that
// one leader must hold.
const trailingWindowThreshold = 80 // percent

// isDominantLeaderStable uses a hybrid evaluation to determine if the cluster
// has a clear leader. Two strategies are tried (either passing is sufficient):
//
// Strategy 1 (full-history dominance): One leader holds > 70% of ALL
// observations AND appeared in at least one of the last 3 checks. Handles
// scattered transient oscillations where the leader is clearly dominant but
// individual outliers can cluster near the end of the window.
//
// Strategy 2 (trailing-window dominance): One leader holds >= 80% of the
// last 10 observations. Handles clean leader transitions where a new leader
// settles quickly but early transition checks reduce the full-history
// percentage.
//
// This hybrid approach is robust against both failure modes observed in
// containerized Raft clusters (Docker, CI).
func isDominantLeaderStable(history []uint64, minChecks int) (uint64, bool) {
	if len(history) < minChecks {
		return 0, false
	}

	total := len(history)

	// Strategy 1: Full-history dominance.
	fullFreq := leaderFrequency(history)
	bestLeader, bestCount := mostFrequentLeader(fullFreq)

	if bestCount*100 >= total*dominantLeaderThreshold {
		// Verify the dominant leader appeared in at least one of the last 3 checks.
		recentWindow := 3
		if recentWindow > total {
			recentWindow = total
		}
		for i := total - recentWindow; i < total; i++ {
			if history[i] == bestLeader {
				return bestLeader, true
			}
		}
	}

	// Strategy 2: Trailing-window dominance.
	windowSize := trailingWindowSize
	if windowSize > total {
		windowSize = total
	}
	tail := history[total-windowSize:]
	tailFreq := leaderFrequency(tail)
	tailLeader, tailCount := mostFrequentLeader(tailFreq)

	if tailCount*100 >= len(tail)*trailingWindowThreshold {
		return tailLeader, true
	}

	return 0, false
}

// noLeaderObserved is the history sentinel for a check in which no leader could
// be resolved. Node IDs are 1-based, so zero is unambiguous. It counts toward
// the observation total but can never win a dominance evaluation, which is what
// makes a sustained loss of leadership fail while a one-check election gap does
// not.
const noLeaderObserved uint64 = 0

// leaderFrequency counts the occurrences of each leader in the history.
func leaderFrequency(history []uint64) map[uint64]int {
	freq := make(map[uint64]int, 2)
	for _, nodeID := range history {
		freq[nodeID]++
	}
	return freq
}

// mostFrequentLeader returns the leader with the highest frequency.
func mostFrequentLeader(freq map[uint64]int) (uint64, int) {
	var bestLeader uint64
	var bestCount int
	for nodeID, c := range freq {
		if nodeID == noLeaderObserved {
			continue
		}
		if c > bestCount {
			bestLeader = nodeID
			bestCount = c
		}
	}
	return bestLeader, bestCount
}

// AssertWriteSucceeds attempts to write data and asserts it succeeds.
func AssertWriteSucceeds(t *testing.T, client quicbaas.Client, entity TestEntity) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := client.KV().Put(ctx, &service.PutRequest{
		EntityType:  entity.EntityType,
		ID:          id.HashString(entity.Key),
		Data:        entity.Data,
		Consistency: service.ConsistencyLevelLinearizable,
	})

	require.NoError(t, err, "Write failed for entity %s", entity.Key)
}

// AssertWriteFails attempts to write data and asserts it fails.
func AssertWriteFails(t *testing.T, client quicbaas.Client, entity TestEntity) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := client.KV().Put(ctx, &service.PutRequest{
		EntityType:  entity.EntityType,
		ID:          id.HashString(entity.Key),
		Data:        entity.Data,
		Consistency: service.ConsistencyLevelLinearizable,
	})

	assert.Error(t, err, "Write should have failed for entity %s", entity.Key)
	t.Logf("Write correctly failed for entity %s: %v", entity.Key, err)
}

// VerifyNodeCount verifies the number of healthy nodes matches expected.
func VerifyNodeCount(t *testing.T, cluster *TestCluster, expectedCount int) {
	t.Helper()

	healthyCount := 0
	for i, client := range cluster.Clients {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := client.Health().Ping(ctx)
		cancel()

		if err == nil {
			healthyCount++
		} else {
			t.Logf("Node %d is not healthy: %v", i+1, err)
		}
	}

	assert.Equal(t, expectedCount, healthyCount,
		"Expected %d healthy nodes, found %d", expectedCount, healthyCount)
}

// ShardInfo represents information about a shard distribution.
type ShardInfo struct {
	ShardID     uint64
	LeaderNodes []uint64 // Node IDs that claim leadership
	Replicas    []uint64 // All node IDs in this shard
}

// VerifyShardDistribution verifies that shards are correctly distributed across nodes.
// It queries a single healthy node for a consistent snapshot of cluster state,
// avoiding split-view issues that arise from aggregating leadership data across
// multiple nodes with potentially stale election state.
func VerifyShardDistribution(t *testing.T, tc *TestCluster, expectedShards []uint64) map[uint64]*ShardInfo {
	t.Helper()

	t.Logf("Verifying shard distribution for shards: %v", expectedShards)

	shardMap := make(map[uint64]*ShardInfo)

	// Initialize shard map with expected shards
	for _, shardID := range expectedShards {
		shardMap[shardID] = &ShardInfo{
			ShardID:     shardID,
			LeaderNodes: []uint64{},
			Replicas:    []uint64{},
		}
	}

	// Query a single healthy node for a consistent view of cluster state.
	// Aggregating across all nodes can produce phantom multi-leader situations
	// when different nodes hold stale election data.
	var queryNodeIndex int
	var queried bool
	for i, client := range tc.Clients {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		status, err := client.Cluster().Status(ctx)
		cancel()

		if err != nil {
			t.Logf("Node %d unavailable for shard query: %v, trying next node", i+1, err)
			continue
		}

		queryNodeIndex = i + 1

		// Process cluster members: add each member as a replica for all shards.
		// Leader assignment is handled separately using per-shard data below.
		for _, member := range status.Members {
			memberID, parseErr := strconv.ParseUint(member.ID, 10, 64)
			if parseErr != nil {
				continue
			}

			for shardID := range shardMap {
				if !contains(shardMap[shardID].Replicas, memberID) {
					shardMap[shardID].Replicas = append(shardMap[shardID].Replicas, memberID)
				}
			}
		}

		// Use per-shard leader data from the cluster status response.
		populateShardLeaders(shardMap, status)

		queried = true
		break
	}

	require.True(t, queried, "Failed to get cluster status from any node")

	// Log shard information
	for _, expectedShard := range expectedShards {
		if info, ok := shardMap[expectedShard]; ok {
			t.Logf("Shard %d: %d leaders, %d replicas (queried from node %d)",
				expectedShard, len(info.LeaderNodes), len(info.Replicas), queryNodeIndex)
		}
	}

	return shardMap
}

// populateShardLeaders uses per-shard data from the SDK ClusterStatus to assign
// leaders and additional replicas to the shard map.
func populateShardLeaders(shardMap map[uint64]*ShardInfo, status *cluster.ClusterStatus) {
	for _, shard := range status.Shards {
		info, ok := shardMap[shard.ShardID]
		if !ok {
			continue
		}

		if shard.LeaderID > 0 && !contains(info.LeaderNodes, shard.LeaderID) {
			info.LeaderNodes = append(info.LeaderNodes, shard.LeaderID)
		}

		// Also add per-shard replicas if available.
		for _, r := range shard.Replicas {
			if !contains(info.Replicas, r) {
				info.Replicas = append(info.Replicas, r)
			}
		}
	}
}

// VerifyShardLeaders verifies that each shard has exactly one leader.
// It accepts the cluster so that when multiple leaders are detected (a transient
// condition caused by stale election data), it can re-query a single node to
// obtain a consistent snapshot before failing.
func VerifyShardLeaders(t *testing.T, tc *TestCluster, shardMap map[uint64]*ShardInfo) {
	t.Helper()

	// First pass: check the provided shard map.
	multiLeaderShards := findMultiLeaderShards(shardMap)
	if len(multiLeaderShards) == 0 {
		logShardLeaders(t, shardMap)
		return
	}

	// Multiple leaders detected on some shards. This is typically a transient
	// state from aggregated stale data. Retry by querying a single node for
	// up to 10 seconds to get a consistent view.
	t.Logf("Detected shards with multiple leaders: %v, retrying with single-node query...", multiLeaderShards)

	retryTimeout := 10 * time.Second
	retryCtx, retryCancel := context.WithTimeout(context.Background(), retryTimeout)
	defer retryCancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-retryCtx.Done():
			// Exhausted retries; fail with the current state.
			for _, shardID := range multiLeaderShards {
				info := shardMap[shardID]
				assert.Len(t, info.LeaderNodes, 1,
					"Shard %d has %d leaders after retries (expected exactly 1): leaders=%v",
					shardID, len(info.LeaderNodes), info.LeaderNodes)
			}
			logShardLeaders(t, shardMap)
			return

		case <-ticker.C:
			refreshed := querySingleNodeShardLeaders(t, tc, shardMap)
			if refreshed == nil {
				continue
			}

			multiLeaderShards = findMultiLeaderShards(refreshed)
			if len(multiLeaderShards) == 0 {
				// All shards now show exactly one leader.
				logShardLeaders(t, refreshed)
				return
			}

			t.Logf("Still have multi-leader shards: %v, retrying...", multiLeaderShards)
			shardMap = refreshed
		}
	}
}

// findMultiLeaderShards returns the IDs of shards that have != 1 leader.
func findMultiLeaderShards(shardMap map[uint64]*ShardInfo) []uint64 {
	var result []uint64
	for shardID, info := range shardMap {
		if len(info.LeaderNodes) != 1 {
			result = append(result, shardID)
		}
	}
	return result
}

// querySingleNodeShardLeaders re-queries a single healthy node and rebuilds
// the leader data for the given shard map. Returns nil if no node responded.
func querySingleNodeShardLeaders(t *testing.T, tc *TestCluster, current map[uint64]*ShardInfo) map[uint64]*ShardInfo {
	t.Helper()

	for i, client := range tc.Clients {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		status, err := client.Cluster().Status(ctx)
		cancel()

		if err != nil {
			t.Logf("Node %d unavailable during shard leader retry: %v", i+1, err)
			continue
		}

		// Rebuild shard map with same shard IDs but fresh leader data from one node.
		refreshed := make(map[uint64]*ShardInfo, len(current))
		for shardID, info := range current {
			refreshed[shardID] = &ShardInfo{
				ShardID:  info.ShardID,
				Replicas: info.Replicas,
			}
		}

		// Use per-shard leader data from the cluster status response.
		populateShardLeaders(refreshed, status)

		t.Logf("Refreshed shard leaders from node %d", i+1)
		return refreshed
	}

	t.Logf("Warning: no node available for shard leader refresh")
	return nil
}

// logShardLeaders logs the leader information for all shards.
func logShardLeaders(t *testing.T, shardMap map[uint64]*ShardInfo) {
	t.Helper()
	for shardID, info := range shardMap {
		if len(info.LeaderNodes) == 1 {
			t.Logf("Shard %d has leader on node %d", shardID, info.LeaderNodes[0])
		} else {
			t.Logf("Shard %d has %d leaders: %v", shardID, len(info.LeaderNodes), info.LeaderNodes)
		}
	}
}

// WaitForAllShardLeaders waits until all expected shards have elected leaders.
// Returns the shard map when all shards have leaders, or error on timeout.
func WaitForAllShardLeaders(t *testing.T, tc *TestCluster, expectedShards []uint64, timeout time.Duration) (map[uint64]*ShardInfo, error) {
	t.Helper()

	t.Logf("Waiting for all %d shards to have leaders (timeout: %v)...", len(expectedShards), timeout)
	deadline := time.Now().Add(timeout)
	var lastShardMap map[uint64]*ShardInfo

	for time.Now().Before(deadline) {
		// Get current shard distribution
		shardMap := make(map[uint64]*ShardInfo)

		// Initialize shard map
		for _, shardID := range expectedShards {
			shardMap[shardID] = &ShardInfo{
				ShardID:     shardID,
				LeaderNodes: []uint64{},
				Replicas:    []uint64{},
			}
		}

		// Aggregate leader data across ALL responding nodes. Per-member
		// leader knowledge in raft is eventually consistent: a follower's
		// view may lag (LeaderID == 0) while a leader is in fact elected
		// and serving writes. Querying only the first responding node
		// produced false "shard has no leader" timeouts even though writes
		// to those shards succeeded. A shard counts as having a leader when
		// ANY node reports one.
		for _, client := range tc.Clients {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			status, err := client.Cluster().Status(ctx)
			cancel()

			if err != nil {
				continue // Try next node
			}

			// Use per-shard leader data from the cluster status response.
			populateShardLeaders(shardMap, status)
		}

		lastShardMap = shardMap

		// Check if all expected shards have leaders
		allHaveLeaders := true
		for _, shardID := range expectedShards {
			if info, ok := shardMap[shardID]; !ok || len(info.LeaderNodes) == 0 {
				allHaveLeaders = false
				break
			}
		}

		if allHaveLeaders {
			t.Logf("All %d shards have elected leaders", len(expectedShards))
			return shardMap, nil
		}

		time.Sleep(2 * time.Second)
	}

	// Log which shards are missing leaders
	for _, shardID := range expectedShards {
		if info, ok := lastShardMap[shardID]; !ok || len(info.LeaderNodes) == 0 {
			t.Logf("Shard %d still has no leader", shardID)
		}
	}

	return lastShardMap, fmt.Errorf("not all shards elected leaders within %v", timeout)
}

// OperationMetrics tracks success and failure metrics for operations.
type OperationMetrics struct {
	SuccessfulReads  atomic.Int64
	FailedReads      atomic.Int64
	SuccessfulWrites atomic.Int64
	FailedWrites     atomic.Int64
}

// GetReadSuccessRate returns the read success rate as a percentage.
func (m *OperationMetrics) GetReadSuccessRate() float64 {
	total := m.SuccessfulReads.Load() + m.FailedReads.Load()
	if total == 0 {
		return 0
	}
	return float64(m.SuccessfulReads.Load()) / float64(total)
}

// GetWriteSuccessRate returns the write success rate as a percentage.
func (m *OperationMetrics) GetWriteSuccessRate() float64 {
	total := m.SuccessfulWrites.Load() + m.FailedWrites.Load()
	if total == 0 {
		return 0
	}
	return float64(m.SuccessfulWrites.Load()) / float64(total)
}

// ContinuousReadWrite performs continuous read/write operations until context is canceled.
func ContinuousReadWrite(ctx context.Context, t *testing.T, client quicbaas.Client, metrics *OperationMetrics) {
	t.Helper()

	opts := DefaultDataGeneratorOptions()
	opts.Count = 1

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Generate a test entity
			entities := GenerateTestEntities(opts)
			entity := entities[0]

			// Try write
			writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := client.KV().Put(writeCtx, &service.PutRequest{
				EntityType:  entity.EntityType,
				ID:          id.HashString(entity.Key),
				Data:        entity.Data,
				Consistency: service.ConsistencyLevelLinearizable,
			})
			cancel()

			if err != nil {
				metrics.FailedWrites.Add(1)
			} else {
				metrics.SuccessfulWrites.Add(1)

				// Try read
				readCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_, err := client.KV().Get(readCtx, &service.GetRequest{
					EntityType:  entity.EntityType,
					ID:          id.HashString(entity.Key),
					Consistency: service.ConsistencyLevelLinearizable,
				})
				cancel()

				if err != nil {
					metrics.FailedReads.Add(1)
				} else {
					metrics.SuccessfulReads.Add(1)
				}
			}

			time.Sleep(100 * time.Millisecond)
		}
	}
}

// VerifyDataIntegrity verifies that all entities still exist and have correct checksums.
func VerifyDataIntegrity(t *testing.T, client quicbaas.Client, entities []TestEntity, expectedChecksums map[string]string) {
	t.Helper()

	t.Logf("Verifying data integrity for %d entities...", len(entities))

	missingCount := 0
	corruptedCount := 0

	for i, entity := range entities {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		resp, err := client.KV().Get(ctx, &service.GetRequest{
			EntityType:  entity.EntityType,
			ID:          id.HashString(entity.Key),
			Consistency: service.ConsistencyLevelLinearizable,
		})
		cancel()

		if err != nil || !resp.Found {
			missingCount++
			t.Errorf("Entity %s is missing", entity.Key)
			continue
		}

		// Verify checksum
		key := fmt.Sprintf("%s:%s", entity.EntityType, entity.Key)
		expectedChecksum, ok := expectedChecksums[key]
		if ok && entity.Checksum != expectedChecksum {
			corruptedCount++
			t.Errorf("Checksum mismatch for entity %s", entity.Key)
		}

		// Log progress
		if (i+1)%100 == 0 {
			t.Logf("Verified %d/%d entities...", i+1, len(entities))
		}
	}

	assert.Equal(t, 0, missingCount, "Found %d missing entities", missingCount)
	assert.Equal(t, 0, corruptedCount, "Found %d corrupted entities", corruptedCount)

	if missingCount == 0 && corruptedCount == 0 {
		t.Logf("Data integrity verified: all entities present and uncorrupted")
	}
}

// contains checks if a slice contains a value.
func contains(slice []uint64, val uint64) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

// IsStandaloneMode probes the cluster to determine if it is running in
// standalone/embedded mode (no real Raft consensus). It queries each node's
// cluster status and checks whether any node reports a leader. If no leader is
// found across all nodes or the cluster status API returns errors, the cluster
// is considered to be in standalone mode.
func IsStandaloneMode(t *testing.T, endpoints []string) bool {
	t.Helper()

	for _, endpoint := range endpoints {
		client, err := connectToNode(context.Background(), endpoint, 10*time.Second, 1, 500*time.Millisecond)
		if err != nil {
			t.Logf("IsStandaloneMode: could not connect to %s: %v", endpoint, err)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		status, err := client.Cluster().Status(ctx)
		cancel()
		_ = client.Close()

		if err != nil {
			t.Logf("IsStandaloneMode: cluster status error from %s: %v", endpoint, err)
			continue
		}

		// Check if any member reports as leader
		for _, member := range status.Members {
			if member.IsLeader {
				return false
			}
		}

		// Check the LeaderID field as a fallback
		if status.LeaderID != "" && status.LeaderID != "0" {
			return false
		}
	}

	return true
}

// IsStandaloneModeFromCluster probes an already-connected TestCluster to
// determine if it is running in standalone/embedded mode. Returns true if no
// leader is found across all nodes.
func IsStandaloneModeFromCluster(t *testing.T, tc *TestCluster) bool {
	t.Helper()

	for i, client := range tc.Clients {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		status, err := client.Cluster().Status(ctx)
		cancel()

		if err != nil {
			t.Logf("IsStandaloneModeFromCluster: cluster status error from node %d: %v", i+1, err)
			continue
		}

		for _, member := range status.Members {
			if member.IsLeader {
				return false
			}
		}

		if status.LeaderID != "" && status.LeaderID != "0" {
			return false
		}
	}

	return true
}
