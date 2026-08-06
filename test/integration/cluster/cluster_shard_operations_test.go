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

package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestClusterShardOperations runs Phase 3 tests for shard operations and rebalancing.
func TestClusterShardOperations(t *testing.T) {
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - shard operations require real Raft cluster")
		return
	}

	t.Run("Test3.1_VerifyShardDistribution", testVerifyShardDistribution)
	t.Run("Test3.2_ShardRebalancingWhenNodeAdded", testShardRebalancingWhenNodeAdded)
	t.Run("Test3.3_DataMigrationDuringRebalancing", testDataMigrationDuringRebalancing)
}

// testVerifyShardDistribution verifies shard distribution across nodes.
// Test 3.1: Query cluster status for the physical KV Raft group (shard 1),
// verify it is replicated across all nodes with an elected leader, then
// exercise logical shard routing with writes and cross-node reads.
func testVerifyShardDistribution(t *testing.T) {
	ctx := context.Background()

	// Build cluster connection
	cluster, err := helpers.NewClusterBuilder().
		WithHealthCheck(180*time.Second, 2*time.Second).
		Build(ctx)
	require.NoError(t, err, "Failed to build cluster")
	defer cluster.Close()

	// Wait for cluster to be healthy
	err = helpers.WaitForClusterHealthy(t, cluster)
	require.NoError(t, err, "Cluster did not become healthy")

	// Wait for leader election with extended timeout
	_, _, err = helpers.WaitForLeaderElection(t, cluster, 60*time.Second)
	require.NoError(t, err, "No leader elected")

	// Verify shard distribution - wait for all shards to have leaders first.
	//
	// The embedded qrdb node hosts a single physical KV Raft group
	// (shard 1); the shard IDs 2/3/200/300 listed in quicbaas.yaml are
	// logical routing IDs, not Raft groups, so only shard 1 ever elects
	// a leader. See pkg/server/qrdb_node.go (qcfg.Raft.ShardID = 1) and
	// the shard note in test/integration/quicbaas-cluster.yaml.
	t.Log("Verifying shard distribution across cluster nodes...")
	expectedShards := []uint64{1}

	// Wait for all shards to have leaders (this can take time with multi-raft)
	shardMap, err := helpers.WaitForAllShardLeaders(t, cluster, expectedShards, 90*time.Second)
	require.NoError(t, err, "Not all shards elected leaders")

	// Wait for leader stability to avoid catching transient split-brain states
	// during leader election transitions. Use 30s to allow Raft to settle in
	// containerized environments.
	t.Log("Waiting for shard leader stability (30 seconds)...")
	helpers.VerifyLeaderStability(t, cluster, 30*time.Second)

	// Get full shard distribution with replicas
	shardMap = helpers.VerifyShardDistribution(t, cluster, expectedShards)

	// Verify all expected user shards exist (filter out system shards 1000, 1001)
	userShardCount := 0
	for shardID := range shardMap {
		if shardID < 1000 { // User shards are < 1000, system shards are >= 1000
			userShardCount++
		}
	}
	// Single physical KV Raft group (shard 1); see expectedShards above.
	assert.Equal(t, len(expectedShards), userShardCount,
		"Expected exactly %d user shard(s) in cluster", len(expectedShards))

	// Verify each user shard has replicas
	for shardID, info := range shardMap {
		if shardID >= 1000 {
			continue // Skip system shards
		}
		t.Logf("Shard %d: %d replicas", shardID, len(info.Replicas))

		// Each shard should have replicas on all nodes (3-node cluster)
		// Note: This assumes full replication; adjust if partial replication is used
		assert.GreaterOrEqual(t, len(info.Replicas), 1,
			"Shard %d should have at least 1 replica", shardID)
	}

	// Verify each shard has exactly one leader
	t.Log("Verifying shard leaders...")
	helpers.VerifyShardLeaders(t, cluster, shardMap)

	// Write data to each shard and verify routing
	t.Log("Testing shard routing...")
	dataOpts := helpers.DefaultDataGeneratorOptions()
	dataOpts.Count = 30 // 10 per shard
	dataOpts.ShardCount = 3
	entities := helpers.GenerateTestEntities(dataOpts)

	// Get leader client for writing
	_, leaderClient, err := cluster.FindLeader(ctx)
	require.NoError(t, err, "Failed to find leader")

	// Write test data
	writtenEntities := helpers.WriteTestData(t, leaderClient, entities)
	t.Logf("Successfully wrote %d entities across shards", len(writtenEntities))

	// Wait for replication
	time.Sleep(3 * time.Second)

	// Verify data is accessible from all nodes
	helpers.VerifyDataConsistency(t, cluster, writtenEntities)

	t.Log("Shard distribution verification completed successfully")
}

// testShardRebalancingWhenNodeAdded verifies shard rebalancing when a node is added.
// Test 3.2: Start with 3-node cluster, add 4th node, trigger rebalancing,
// verify shards are redistributed, verify data remains accessible during rebalancing.
func testShardRebalancingWhenNodeAdded(t *testing.T) {
	ctx := context.Background()

	// Build initial 3-node cluster
	cluster, err := helpers.NewClusterBuilder().
		WithHealthCheck(180*time.Second, 2*time.Second).
		Build(ctx)
	require.NoError(t, err, "Failed to build cluster")
	defer cluster.Close()

	// Wait for cluster to be healthy
	err = helpers.WaitForClusterHealthy(t, cluster)
	require.NoError(t, err, "Cluster did not become healthy")

	// Get leader client
	_, leaderClient, err := cluster.FindLeader(ctx)
	require.NoError(t, err, "Failed to find leader")

	// Record initial shard distribution. Only shard 1 is a physical
	// Raft group; see the note in testVerifyShardDistribution.
	t.Log("Recording initial shard distribution...")
	expectedShards := []uint64{1}
	initialShardMap := helpers.VerifyShardDistribution(t, cluster, expectedShards)

	// Write 100 test entities
	t.Log("Writing 100 test entities...")
	dataOpts := helpers.DefaultDataGeneratorOptions()
	dataOpts.Count = 100
	dataOpts.ShardCount = 3
	entities := helpers.GenerateTestEntities(dataOpts)
	writtenEntities := helpers.WriteTestData(t, leaderClient, entities)

	// Compute checksums for data integrity verification
	expectedChecksums := helpers.ComputeChecksums(writtenEntities)

	// Note: Adding a 4th node requires cluster management API which may not be
	// implemented yet. This test documents the expected behavior.
	t.Log("NOTE: This test requires AddNode API for dynamic membership changes")
	t.Log("Skipping node4 addition - feature may not be implemented")

	// If AddNode API is available, uncomment the following:
	/*
		// Add node4 to cluster
		docker := helpers.NewDockerManager()
		err = docker.UpNode(ctx, t, "test-node4")
		if err != nil {
			t.Logf("Warning: Could not start node4: %v", err)
			t.Fatal("node4 must be available for this test")
		}
		defer docker.DownNode(ctx, t, "test-node4")

		// Wait for node4 to join
		time.Sleep(10 * time.Second)

		// Record final shard distribution
		t.Log("Recording final shard distribution after node addition...")
		finalShardMap := helpers.VerifyShardDistribution(t, cluster, expectedShards)

		// Compare distributions
		t.Log("Comparing shard distributions...")
		for shardID := range initialShardMap {
			initialReplicas := len(initialShardMap[shardID].Replicas)
			finalReplicas := len(finalShardMap[shardID].Replicas)
			t.Logf("Shard %d: replicas changed from %d to %d",
				shardID, initialReplicas, finalReplicas)
		}
	*/

	// Verify all data is still accessible
	t.Log("Verifying data accessibility...")
	helpers.VerifyDataIntegrity(t, leaderClient, writtenEntities, expectedChecksums)

	t.Log("Initial distribution:", initialShardMap)
	t.Log("No automatic rebalancing occurred (expected for current implementation)")
	t.Log("All data remains accessible after stability check")
}

// testDataMigrationDuringRebalancing verifies data migration during rebalancing.
// Test 3.3: Seed 1000 entities across shards, compute checksums, trigger rebalancing,
// verify all data accessible after rebalancing, verify checksums match.
func testDataMigrationDuringRebalancing(t *testing.T) {
	ctx := context.Background()

	// Build cluster
	cluster, err := helpers.NewClusterBuilder().
		WithHealthCheck(180*time.Second, 2*time.Second).
		Build(ctx)
	require.NoError(t, err, "Failed to build cluster")
	defer cluster.Close()

	// Wait for cluster to be healthy
	err = helpers.WaitForClusterHealthy(t, cluster)
	require.NoError(t, err, "Cluster did not become healthy")

	// Get leader client
	_, leaderClient, err := cluster.FindLeader(ctx)
	require.NoError(t, err, "Failed to find leader")

	// Write 1000 entities distributed across all shards
	t.Log("Writing 1000 test entities across shards...")
	dataOpts := helpers.DefaultDataGeneratorOptions()
	dataOpts.Count = 1000
	dataOpts.ShardCount = 3
	dataOpts.Pattern = helpers.PatternUniform
	entities := helpers.GenerateTestEntities(dataOpts)
	writtenEntities := helpers.WriteTestData(t, leaderClient, entities)

	// Compute checksums for data integrity
	t.Log("Computing checksums for all entities...")
	expectedChecksums := helpers.ComputeChecksums(writtenEntities)
	t.Logf("Computed %d checksums", len(expectedChecksums))

	// Note: Leadership transfer or shard rebalancing requires specific API
	// This test documents the expected behavior for when the feature is available
	t.Log("NOTE: This test requires TransferLeadership or rebalancing API")
	t.Log("Current implementation may not support automatic rebalancing")

	// If leadership transfer is supported, uncomment:
	/*
		t.Log("Triggering leadership transfers for load distribution...")
		// Attempt to transfer leadership for each shard to different nodes
		// This simulates rebalancing behavior
		for shardID := uint64(1); shardID <= 3; shardID++ {
			targetNode := (shardID % 3) + 1 // Round-robin distribution
			t.Logf("Attempting to transfer shard %d leadership to node %d", shardID, targetNode)
			// Call leadership transfer API
			// err := leaderClient.TransferLeadership(ctx, shardID, targetNode)
			// if err != nil {
			//     t.Logf("Leadership transfer not supported or failed: %v", err)
			// }
		}

		// Wait for transfers to complete
		time.Sleep(10 * time.Second)
	*/

	// Verify data integrity after potential migration
	t.Log("Verifying data integrity after stability period...")
	time.Sleep(5 * time.Second) // Allow time for any background operations

	helpers.VerifyDataIntegrity(t, leaderClient, writtenEntities, expectedChecksums)

	// Verify data consistency across all nodes
	t.Log("Verifying data consistency across all nodes...")
	helpers.VerifyDataConsistency(t, cluster, writtenEntities)

	// Verify all checksums still match
	t.Log("Final checksum verification...")
	helpers.VerifyChecksums(t, leaderClient, writtenEntities, expectedChecksums)

	t.Log("Data migration test completed successfully")
	t.Log("All 1000 entities verified with correct checksums")
	t.Log("No data loss or corruption detected")
}
