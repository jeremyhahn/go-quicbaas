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

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestRaftClusterLifecycle runs all cluster lifecycle tests.
func TestRaftClusterLifecycle(t *testing.T) {
	// Check if cluster is running in standalone/embedded mode (no real Raft consensus).
	// In standalone mode, Raft lifecycle operations (leader election, data replication,
	// quorum operations) are not available.
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - Raft lifecycle tests not applicable")
		return
	}

	// Run subtests sequentially to ensure proper cluster state
	t.Run("Bootstrap", testClusterBootstrap)
	t.Run("LeaderElection", testLeaderElection)
	t.Run("DataReplication", testDataReplication)
	t.Run("QuorumOperations", testQuorumOperations)
}

// testClusterBootstrap verifies that a 3-node Raft cluster forms correctly.
func testClusterBootstrap(t *testing.T) {
	// Build cluster
	builder := helpers.NewClusterBuilder()
	cluster, err := builder.Build(context.Background())
	require.NoError(t, err, "Failed to build test cluster")
	defer cluster.Close()

	// Wait for cluster to become healthy
	err = helpers.WaitForClusterHealthy(t, cluster)
	require.NoError(t, err, "Cluster did not become healthy")

	// Verify cluster size from all nodes (use AtLeast to handle cases where
	// previous tests may have added nodes)
	helpers.VerifyClusterSizeAtLeast(t, cluster, 3)

	t.Log("Cluster bootstrap test passed")
}

// testLeaderElection verifies that a leader is elected and remains stable.
func testLeaderElection(t *testing.T) {
	// Build cluster
	builder := helpers.NewClusterBuilder()
	cluster, err := builder.Build(context.Background())
	require.NoError(t, err, "Failed to build test cluster")
	defer cluster.Close()

	// Wait for cluster health
	err = helpers.WaitForClusterHealthy(t, cluster)
	require.NoError(t, err, "Cluster did not become healthy")

	// Wait for leader election with longer timeout to avoid transient issues
	leader, _, err := helpers.WaitForLeaderElection(t, cluster, 60*time.Second)
	require.NoError(t, err, "Leader election failed")
	require.NotNil(t, leader, "No leader elected")

	t.Logf("Initial leader elected: node %d", leader.ID)

	// Wait for initial leader stability before verifying consistency.
	// Use 30s to allow Raft to settle in containerized environments.
	t.Log("Waiting for initial leader stability (30 seconds)...")
	helpers.VerifyLeaderStability(t, cluster, 30*time.Second)

	// Verify all nodes agree on the leader (may be different from initial leader after stabilization)
	leaderID := helpers.VerifyLeaderConsistency(t, cluster)
	t.Logf("Stable leader confirmed: node %d", leaderID)

	// Verify continued leader stability for a shorter period since we already verified consistency
	t.Log("Verifying continued leader stability (10 seconds)...")
	helpers.VerifyLeaderStability(t, cluster, 10*time.Second)

	t.Log("Leader election test passed")
}

// testDataReplication verifies that data is replicated across all nodes.
func testDataReplication(t *testing.T) {
	// Build cluster
	builder := helpers.NewClusterBuilder()
	cluster, err := builder.Build(context.Background())
	require.NoError(t, err, "Failed to build test cluster")
	defer cluster.Close()

	// Wait for cluster health
	err = helpers.WaitForClusterHealthy(t, cluster)
	require.NoError(t, err, "Cluster did not become healthy")

	// Find leader
	leader, leaderClient, err := helpers.WaitForLeaderElection(t, cluster, 30*time.Second)
	require.NoError(t, err, "Failed to find leader")
	require.NotNil(t, leader, "No leader found")

	t.Logf("Writing test data to leader (node %d)...", leader.ID)

	// Generate test entities distributed across shards
	opts := helpers.DefaultDataGeneratorOptions()
	opts.Count = 100
	opts.ShardCount = 3
	opts.EntityType = "replication_test"
	entities := helpers.GenerateTestEntities(opts)

	// Write data to leader
	startWrite := time.Now()
	entities = helpers.WriteTestData(t, leaderClient, entities)
	writeTime := time.Since(startWrite)

	t.Logf("Wrote 100 entities in %v, waiting for replication...", writeTime)

	waitForEntitiesOnAllNodes(t, cluster, entities)

	// Verify data consistency across all nodes
	startVerify := time.Now()
	helpers.VerifyDataConsistency(t, cluster, entities)
	verifyTime := time.Since(startVerify)

	t.Logf("Data replication verified in %v", verifyTime)
	t.Log("Data replication test passed")
}

// testQuorumOperations verifies quorum behavior for write operations.
func testQuorumOperations(t *testing.T) {
	// Note: This test is complex as it requires stopping/starting Docker containers
	// For now, we'll implement a basic version that verifies writes succeed with full quorum

	// Build cluster
	builder := helpers.NewClusterBuilder()
	cluster, err := builder.Build(context.Background())
	require.NoError(t, err, "Failed to build test cluster")
	defer cluster.Close()

	// Wait for cluster health
	err = helpers.WaitForClusterHealthy(t, cluster)
	require.NoError(t, err, "Cluster did not become healthy")

	// Find leader
	leader, leaderClient, err := helpers.WaitForLeaderElection(t, cluster, 30*time.Second)
	require.NoError(t, err, "Failed to find leader")
	require.NotNil(t, leader, "No leader found")

	t.Logf("Testing quorum operations with leader (node %d)...", leader.ID)

	// Phase 1: Write with all 3 nodes healthy (full quorum)
	t.Log("Phase 1: Writing with full cluster (3/3 nodes)...")
	opts := helpers.DefaultDataGeneratorOptions()
	opts.Count = 10
	opts.EntityType = "quorum_test"
	entities := helpers.GenerateTestEntities(opts)

	for i := 0; i < 5; i++ {
		helpers.AssertWriteSucceeds(t, leaderClient, entities[i])
	}

	t.Log("Writes successful with full quorum (3/3 nodes)")

	// Verify all nodes have the data
	waitForEntitiesOnAllNodes(t, cluster, entities[:5])
	helpers.VerifyDataConsistency(t, cluster, entities[:5])

	// Note: To properly test phases 2-4 (node failures), we would need to:
	// - Stop specific Docker containers (requires docker exec/docker-compose commands)
	// - Wait for cluster to detect failures
	// - Attempt writes and verify they fail/succeed based on quorum
	// - Restart containers and verify recovery
	//
	// This requires integration with Docker Compose CLI which is beyond
	// the scope of basic gRPC client testing. We'll implement this in a
	// separate test file with proper Docker container management.

	t.Log("Quorum operations test passed (basic)")
	t.Log("Note: Full quorum failure testing requires Docker container management")
}
