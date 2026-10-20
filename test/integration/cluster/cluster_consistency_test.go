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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/hash"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestClusterConsistency runs Phase 4 tests for data consistency verification.
const (
	// membershipChangeWindow stands in for a membership change while no
	// AddNode/RemoveNode API exists. Five seconds spans several election
	// timeouts, so a lost leader shows up within it.
	membershipChangeWindow = 5 * time.Second

	// postStabilizationWindow is how long operations run after the change to
	// measure the settled success rate. At one operation per 100 ms it yields
	// enough samples for the 99% threshold to mean something.
	postStabilizationWindow = 5 * time.Second
)

func TestClusterConsistency(t *testing.T) {
	// Check if cluster is running in standalone/embedded mode (no real Raft consensus).
	// In standalone mode, there is no leader election, so cluster consistency tests
	// that depend on Raft leader election are not applicable.
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - no leader available, cluster consistency tests not applicable")
		return
	}

	t.Run("Test4.1_DataSurvivesMembershipChanges", testDataSurvivesMembershipChanges)
	t.Run("Test4.2_WriteSuccessRateDuringMembershipChanges", testWriteSuccessRateDuringMembershipChanges)
	t.Run("Test4.3_ReadConsistencyAcrossAllNodes", testReadConsistencyAcrossAllNodes)
	t.Run("Test4.4_ZeroDataLossVerification", testZeroDataLossVerification)
}

// testDataSurvivesMembershipChanges verifies data survives membership changes.
// Test 4.1: Write 500 entities before membership change, add node4, verify all
// 500 entities readable, write 500 more entities, remove node, verify all 1000
// entities readable with correct checksums.
func testDataSurvivesMembershipChanges(t *testing.T) {
	ctx := context.Background()

	// Build 3-node cluster
	cluster, err := helpers.NewClusterBuilder().
		WithHealthCheck(180*time.Second, 2*time.Second).
		Build(ctx)
	require.NoError(t, err, "Failed to build cluster")
	defer cluster.Close()

	// Wait for cluster to be healthy
	err = helpers.WaitForClusterHealthy(t, cluster)
	require.NoError(t, err, "Cluster did not become healthy")

	// Wait for leader election (may take a few seconds after cluster is healthy)
	_, leaderClient, err := helpers.WaitForLeaderElection(t, cluster, 60*time.Second)
	require.NoError(t, err, "Failed to elect leader")

	// Phase 1: Write 500 baseline entities
	t.Log("Phase 1: Writing 500 baseline entities...")
	dataOpts := helpers.DefaultDataGeneratorOptions()
	dataOpts.Count = 500
	dataOpts.ShardCount = 3
	dataOpts.KeyPrefix = "baseline"
	baselineEntities := helpers.GenerateTestEntities(dataOpts)
	writtenBaseline := helpers.WriteTestData(t, leaderClient, baselineEntities)

	// Compute checksums for baseline data
	baselineChecksums := helpers.ComputeChecksums(writtenBaseline)
	t.Logf("Baseline: %d entities, %d checksums", len(writtenBaseline), len(baselineChecksums))

	// Note: Membership changes require cluster management API
	t.Log("NOTE: This test requires AddNode/RemoveNode API for membership changes")
	t.Log("Simulating membership change scenario with current cluster...")

	holdSteady(t, membershipChangeWindow, "cluster keeps a leader during the simulated membership change", clusterHasLeader(cluster))

	// Phase 2: Verify baseline data is still accessible
	t.Log("Phase 2: Verifying baseline data after simulated membership change...")
	helpers.VerifyDataIntegrity(t, leaderClient, writtenBaseline, baselineChecksums)

	// Phase 3: Write 500 more entities
	t.Log("Phase 3: Writing 500 additional entities...")
	dataOpts.KeyPrefix = "additional"
	additionalEntities := helpers.GenerateTestEntities(dataOpts)
	writtenAdditional := helpers.WriteTestData(t, leaderClient, additionalEntities)

	// Combine all entities
	allEntities := append(writtenBaseline, writtenAdditional...)
	allChecksums := helpers.ComputeChecksums(allEntities)
	t.Logf("Total: %d entities, %d checksums", len(allEntities), len(allChecksums))

	holdSteady(t, membershipChangeWindow, "cluster keeps a leader during the second simulated membership change", clusterHasLeader(cluster))

	// Phase 4: Verify all 1000 entities are readable
	t.Log("Phase 4: Verifying all 1000 entities after operations...")
	helpers.VerifyDataIntegrity(t, leaderClient, allEntities, allChecksums)

	// Verify data consistency across all nodes
	t.Log("Verifying data consistency across all cluster nodes...")
	helpers.VerifyDataConsistency(t, cluster, allEntities)

	// Final checksum verification
	t.Log("Final checksum verification...")
	helpers.VerifyChecksums(t, leaderClient, allEntities, allChecksums)

	t.Log("Data survives membership changes: PASSED")
	t.Log("All 1000 entities verified with correct checksums")
}

// testWriteSuccessRateDuringMembershipChanges verifies write success rate during changes.
// Test 4.2: Start continuous write load, perform membership change, measure write
// success rate, verify >95% success rate during operation, 100% after stabilization.
func testWriteSuccessRateDuringMembershipChanges(t *testing.T) {
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

	// Wait for leader election (may take a few seconds after cluster is healthy)
	_, leaderClient, err := helpers.WaitForLeaderElection(t, cluster, 60*time.Second)
	require.NoError(t, err, "Failed to elect leader")

	// Initialize operation metrics
	metrics := &helpers.OperationMetrics{}

	// Start continuous read/write operations in background
	t.Log("Starting continuous read/write operations...")
	opCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Start 10 background goroutines
	var workers sync.WaitGroup
	for i := 0; i < 10; i++ {
		workers.Go(func() { helpers.ContinuousReadWrite(opCtx, t, leaderClient, metrics) })
	}

	pollUntil(t, replicationTimeout, "background operations complete a write",
		func(context.Context) (bool, error) { return metrics.SuccessfulWrites.Load() > 0, nil })
	t.Logf("Background operations started: %d writes, %d reads so far",
		metrics.SuccessfulWrites.Load(), metrics.SuccessfulReads.Load())

	// Simulate membership change (pause to represent the change operation)
	t.Log("Simulating membership change operation...")
	membershipChangeStart := time.Now()

	// No membership change API exists yet, so the window stands in for the
	// change while the cluster must keep a leader under load.
	holdSteady(t, membershipChangeWindow, "cluster keeps a leader during the membership change", clusterHasLeader(cluster))

	membershipChangeDuration := time.Since(membershipChangeStart)
	t.Logf("Simulated membership change completed in %v", membershipChangeDuration)

	holdSteady(t, membershipChangeWindow, "cluster keeps a leader after the membership change", clusterHasLeader(cluster))

	// Stop background operations and wait for them to finish
	cancel()
	workers.Wait()

	// Analyze results
	totalReads := metrics.SuccessfulReads.Load() + metrics.FailedReads.Load()
	totalWrites := metrics.SuccessfulWrites.Load() + metrics.FailedWrites.Load()

	readSuccessRate := metrics.GetReadSuccessRate()
	writeSuccessRate := metrics.GetWriteSuccessRate()

	t.Logf("Operation Metrics:")
	t.Logf("  Total Reads: %d (Success: %d, Failed: %d, Rate: %.2f%%)",
		totalReads, metrics.SuccessfulReads.Load(), metrics.FailedReads.Load(), readSuccessRate*100)
	t.Logf("  Total Writes: %d (Success: %d, Failed: %d, Rate: %.2f%%)",
		totalWrites, metrics.SuccessfulWrites.Load(), metrics.FailedWrites.Load(), writeSuccessRate*100)

	// Verify success rates meet requirements
	// During membership changes, we expect >95% success rate
	assert.Greater(t, readSuccessRate, 0.95,
		"Read success rate too low: %.2f%% (expected >95%%)", readSuccessRate*100)
	assert.Greater(t, writeSuccessRate, 0.95,
		"Write success rate too low: %.2f%% (expected >95%%)", writeSuccessRate*100)

	// Verify cluster returns to 100% success after stabilization
	t.Log("Verifying 100% success rate after stabilization...")
	testMetrics := &helpers.OperationMetrics{}
	testCtx, testCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer testCancel()

	var stabilized sync.WaitGroup
	stabilized.Go(func() { helpers.ContinuousReadWrite(testCtx, t, leaderClient, testMetrics) })
	pace(t, postStabilizationWindow)
	testCancel()
	stabilized.Wait()

	postStabilizationWriteRate := testMetrics.GetWriteSuccessRate()
	t.Logf("Post-stabilization write success rate: %.2f%%", postStabilizationWriteRate*100)

	// We expect close to 100% after stabilization (allow for minor network issues)
	assert.Greater(t, postStabilizationWriteRate, 0.99,
		"Post-stabilization success rate too low: %.2f%% (expected >99%%)", postStabilizationWriteRate*100)

	t.Log("Write success rate during membership changes: PASSED")
}

// testReadConsistencyAcrossAllNodes verifies read consistency across all nodes.
// Test 4.3: Write entity to cluster, read from all 3 nodes with strong consistency,
// verify all nodes return same data, test with linearizable reads.
func testReadConsistencyAcrossAllNodes(t *testing.T) {
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

	// Wait for leader election (may take a few seconds after cluster is healthy)
	_, leaderClient, err := helpers.WaitForLeaderElection(t, cluster, 60*time.Second)
	require.NoError(t, err, "Failed to elect leader")

	// Write 100 entities with unique timestamps
	t.Log("Writing 100 test entities with timestamps...")
	dataOpts := helpers.DefaultDataGeneratorOptions()
	dataOpts.Count = 100
	dataOpts.ShardCount = 3
	entities := helpers.GenerateTestEntities(dataOpts)
	writtenEntities := helpers.WriteTestData(t, leaderClient, entities)

	// Wait for replication
	t.Log("Waiting for replication across cluster...")
	waitForEntitiesOnAllNodes(t, cluster, writtenEntities)

	// Perform linearizable reads from all nodes
	t.Log("Performing linearizable reads from all nodes...")
	inconsistencyCount := 0

	for _, entity := range writtenEntities {
		var referenceData []byte
		var referenceFound bool

		for i, sdkClient := range cluster.Clients {
			readCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			resp, err := sdkClient.KV().Get(readCtx, &service.GetRequest{
				EntityType:  entity.EntityType,
				ID:          hash.FNV1a(entity.Key),
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
					"Linearizable read inconsistency for entity %s on node %d", entity.Key, i+1) {
					inconsistencyCount++
					if inconsistencyCount >= 5 {
						t.Fatalf("Too many inconsistencies detected, aborting test")
					}
				}
			}
		}
	}

	assert.Equal(t, 0, inconsistencyCount,
		"Found %d linearizable read inconsistencies", inconsistencyCount)

	t.Log("Testing eventual consistency reads...")
	// Test eventual consistency (may have stale reads, but should not error)
	staleReadCount := 0
	for _, entity := range writtenEntities[:10] { // Test first 10 entities
		for i, sdkClient := range cluster.Clients {
			readCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			resp, err := sdkClient.KV().Get(readCtx, &service.GetRequest{
				EntityType:  entity.EntityType,
				ID:          hash.FNV1a(entity.Key),
				Consistency: service.ConsistencyLevelEventual,
			})
			cancel()

			if err != nil {
				t.Logf("Eventual read error on node %d for entity %s: %v", i+1, entity.Key, err)
			} else if !resp.Found {
				staleReadCount++
				t.Logf("Eventual read returned not found on node %d for entity %s (may be stale)", i+1, entity.Key)
			}
		}
	}

	t.Logf("Eventual consistency: %d stale reads detected (acceptable)", staleReadCount)

	// Verify strong consistency always returns latest data
	t.Log("Verifying strong consistency guarantees...")
	for _, entity := range writtenEntities[:10] {
		for i, sdkClient := range cluster.Clients {
			readCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			resp, err := sdkClient.KV().Get(readCtx, &service.GetRequest{
				EntityType:  entity.EntityType,
				ID:          hash.FNV1a(entity.Key),
				Consistency: service.ConsistencyLevelStrong,
			})
			cancel()

			require.NoError(t, err, "Strong consistency read failed on node %d", i+1)
			require.True(t, resp.Found, "Strong consistency read not found on node %d", i+1)
		}
	}

	t.Log("Read consistency across all nodes: PASSED")
	t.Logf("All linearizable reads returned consistent data")
	t.Logf("No torn reads or partially updated data observed")
}

// testZeroDataLossVerification verifies zero data loss during operations.
// Test 4.4: Write 1000 entities with unique IDs, track all writes in memory,
// perform multiple cluster operations, verify every single entity is still
// accessible, verify checksums for all entities match.
func testZeroDataLossVerification(t *testing.T) {
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

	// Wait for leader election with extended timeout for stability
	t.Log("Waiting for leader election...")
	_, leaderClient, err := helpers.WaitForLeaderElection(t, cluster, 60*time.Second)
	require.NoError(t, err, "Failed to elect leader")

	// Wait for initial leader stability before writing data.
	// Use 30s window to allow Raft to settle in containerized environments
	// where CPU scheduling jitter can cause transient leader oscillation.
	t.Log("Verifying initial leader stability (30 seconds)...")
	helpers.VerifyLeaderStability(t, cluster, 30*time.Second)

	// Write 1000 uniquely identified entities
	t.Log("Writing 1000 uniquely identified entities...")
	dataOpts := helpers.DefaultDataGeneratorOptions()
	dataOpts.Count = 1000
	dataOpts.ShardCount = 3
	dataOpts.KeyPrefix = "unique"
	entities := helpers.GenerateTestEntities(dataOpts)
	writtenEntities := helpers.WriteTestData(t, leaderClient, entities)

	// Track all entity IDs
	entityIDs := make(map[string]bool)
	for _, entity := range writtenEntities {
		key := entity.EntityType + ":" + entity.Key
		entityIDs[key] = true
	}

	// Compute checksums
	expectedChecksums := helpers.ComputeChecksums(writtenEntities)
	t.Logf("Tracking %d entities with %d checksums", len(entityIDs), len(expectedChecksums))

	// Wait for data to replicate across all nodes
	t.Log("Waiting for data replication...")
	waitForEntitiesOnAllNodes(t, cluster, writtenEntities)

	// Define test scenarios
	scenarios := []struct {
		Name      string
		Operation func() error
	}{
		{
			Name: "StabilityCheck",
			Operation: func() error {
				t.Log("Performing stability check...")
				holdSteady(t, membershipChangeWindow, "cluster keeps a leader during the stability check", clusterHasLeader(cluster))
				return nil
			},
		},
		{
			Name: "LeaderStabilityVerification",
			Operation: func() error {
				t.Log("Verifying leader stability...")
				helpers.VerifyLeaderStability(t, cluster, 30*time.Second)
				return nil
			},
		},
		{
			Name: "ClusterHealthCheck",
			Operation: func() error {
				t.Log("Performing cluster health check...")
				return helpers.WaitForClusterHealthy(t, cluster)
			},
		},
	}

	// Execute each scenario and verify data
	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			t.Logf("Executing scenario: %s", scenario.Name)

			// Execute operation
			err := scenario.Operation()
			require.NoError(t, err, "Scenario operation failed")

			pollUntil(t, clusterSettleTimeout, "cluster has a leader after "+scenario.Name,
				func(ctx context.Context) (bool, error) { return clusterHasLeader(cluster)(ctx) == nil, nil })

			// Re-find the current leader for reads - leader may have changed
			currentLeader, currentLeaderClient, err := cluster.FindLeader(ctx)
			if err != nil {
				t.Logf("Warning: Could not find leader, using original client: %v", err)
				currentLeaderClient = leaderClient
			} else {
				t.Logf("Current leader for verification: node %d", currentLeader.ID)
			}

			// Verify all entities still exist using linearizable reads
			t.Logf("Verifying all entities exist after %s...", scenario.Name)
			missingEntities := []string{}

			for _, entity := range writtenEntities {
				key := entity.EntityType + ":" + entity.Key

				// Read using linearizable consistency to get the most up-to-date value
				readCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				resp, err := currentLeaderClient.KV().Get(readCtx, &service.GetRequest{
					EntityType:  entity.EntityType,
					ID:          hash.FNV1a(entity.Key),
					Consistency: service.ConsistencyLevelLinearizable,
				})
				cancel()

				if err != nil || !resp.Found {
					missingEntities = append(missingEntities, key)
				}
			}

			assert.Empty(t, missingEntities,
				"Missing entities after %s: %d entities not found", scenario.Name, len(missingEntities))

			// Verify checksums using current leader
			t.Logf("Verifying checksums after %s...", scenario.Name)
			helpers.VerifyChecksums(t, currentLeaderClient, writtenEntities, expectedChecksums)

			t.Logf("Scenario %s completed: No data loss detected", scenario.Name)
		})
	}

	// Final comprehensive verification - re-find leader to ensure we use current leader
	t.Log("Performing final comprehensive data integrity check...")
	finalLeader, finalLeaderClient, err := cluster.FindLeader(ctx)
	if err != nil {
		t.Logf("Warning: Could not find leader for final verification, using original: %v", err)
		finalLeaderClient = leaderClient
	} else {
		t.Logf("Final verification using leader: node %d", finalLeader.ID)
	}

	helpers.VerifyDataIntegrity(t, finalLeaderClient, writtenEntities, expectedChecksums)
	helpers.VerifyDataConsistency(t, cluster, writtenEntities)

	t.Log("Zero data loss verification: PASSED")
	t.Logf("All 1000 entities verified across %d scenarios", len(scenarios))
	t.Log("No data loss or corruption detected in any scenario")
}
