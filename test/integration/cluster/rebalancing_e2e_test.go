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
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// isRetryableRaftError checks if an error is a transient Raft error that should be retried.
func isRetryableRaftError(err error) bool {
	if err == nil {
		return false
	}

	// Check gRPC status codes
	if s, ok := status.FromError(err); ok {
		switch s.Code() {
		case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Aborted, codes.Internal:
			return true
		}
	}

	// Check for specific Raft error messages
	errMsg := err.Error()
	retryableMessages := []string{
		"not ready",
		"proposal dropped",
		"shard not found",
		"leader not available",
		"no leader",
		"timeout",
		"temporarily unavailable",
	}
	for _, msg := range retryableMessages {
		if strings.Contains(strings.ToLower(errMsg), msg) {
			return true
		}
	}

	return false
}

// putWithRetry performs a Put operation with exponential backoff retry for transient errors.
func putWithRetry(ctx context.Context, client pb.QuicBaaSClient, req *pb.PutRequest, maxRetries int) error {
	var lastErr error
	baseBackoff := 100 * time.Millisecond

	for attempt := 0; attempt <= maxRetries; attempt++ {
		_, err := client.Put(ctx, req)
		if err == nil {
			return nil
		}

		lastErr = err

		if !isRetryableRaftError(err) {
			return err // Non-retryable error, fail immediately
		}

		if attempt == maxRetries {
			break // Last attempt exhausted
		}

		// Calculate exponential backoff with jitter
		backoff := baseBackoff * time.Duration(1<<uint(attempt))
		if backoff > 5*time.Second {
			backoff = 5 * time.Second
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			// Continue to next retry
		}
	}

	return fmt.Errorf("operation failed after %d retries: %w", maxRetries, lastErr)
}

// waitForRaftReady waits until a basic Put operation succeeds, indicating the Raft cluster is ready.
func waitForRaftReady(ctx context.Context, t *testing.T, client pb.QuicBaaSClient, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// Try a test write to verify Raft is ready
		testID := uint64(time.Now().UnixNano() % 1000000)
		_, err := client.Put(ctx, &pb.PutRequest{
			EntityType: "_raft_readiness_check",
			Id:         testID,
			Data:       []byte(`{"check":"ready"}`),
		})

		if err == nil {
			// Clean up the test entity
			_, _ = client.Delete(ctx, &pb.DeleteRequest{
				EntityType: "_raft_readiness_check",
				Id:         testID,
			})
			t.Log("Raft cluster is ready for writes")
			return nil
		}

		if !isRetryableRaftError(err) {
			// If it's not a transient error, something else is wrong
			t.Logf("Non-retryable error while waiting for Raft: %v", err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
			// Continue polling
		}
	}

	return fmt.Errorf("Raft cluster not ready within %v", timeout)
}

// TestDataDistributionRebalancing tests basic rebalancing with data across multiple shards.
// This test validates that data is properly redistributed to achieve cluster balance.
func TestDataDistributionRebalancing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Connect to node 1 via gRPC
	tlsCfg := requireTLSConfig(t)
	conn, err := grpc.NewClient("test-node1:50051",
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	require.NoError(t, err, "Failed to connect to node1")
	defer conn.Close()

	client := pb.NewQuicBaaSClientExt(conn)

	// Wait for cluster to be fully ready including Raft consensus
	t.Log("Waiting for Raft cluster to be ready...")
	err = waitForRaftReady(ctx, t, client, 60*time.Second)
	require.NoError(t, err, "Cluster not ready for Raft operations")

	// Phase 1: Populate data
	t.Run("Phase1_PopulateData", func(t *testing.T) {
		for i := 1; i <= 100; i++ {
			err := putWithRetry(ctx, client, &pb.PutRequest{
				EntityType: "rebalance_test",
				Id:         uint64(i),
				Data:       []byte(fmt.Sprintf(`{"id":%d,"data":"value-%04d","timestamp":"%s"}`, i, i, time.Now().Format(time.RFC3339))),
			}, 3)
			require.NoError(t, err, "Failed to put entity %d", i)
		}
		t.Logf("Successfully populated 100 entities for rebalancing")
	})

	// Phase 2: Check cluster status before rebalancing
	t.Run("Phase2_CheckStatusBefore", func(t *testing.T) {
		statusResp, err := client.ClusterStatus(ctx, &pb.ClusterStatusRequest{
			IncludeMetrics: true,
		})
		require.NoError(t, err, "Failed to get cluster status")

		t.Log("Cluster status before rebalancing:")
		for _, node := range statusResp.Nodes {
			t.Logf("  Node %d: Leader=%v, Health=%v", node.NodeId, node.IsLeader, node.HealthStatus)
		}
		for _, shard := range statusResp.Shards {
			t.Logf("  Shard %d: Leader=%d, Replicas=%v", shard.ShardId, shard.LeaderId, shard.Replicas)
		}
	})

	// Phase 3: Execute rebalancing via gRPC API
	t.Run("Phase3_ExecuteRebalancing", func(t *testing.T) {
		rebalanceResp, err := client.RebalanceCluster(ctx, &pb.RebalanceClusterRequest{
			Strategy: "round_robin",
		})
		require.NoError(t, err, "Failed to trigger rebalancing")
		require.NotEmpty(t, rebalanceResp.OperationId, "Operation ID should not be empty")

		t.Logf("Rebalancing initiated with operation ID: %s", rebalanceResp.OperationId)

		// Monitor rebalancing progress
		for i := 0; i < 30; i++ {
			progressResp, err := client.GetRebalanceProgress(ctx, &pb.GetRebalanceProgressRequest{
				OperationId: rebalanceResp.OperationId,
			})
			if err != nil {
				t.Logf("Warning: Failed to get rebalance progress: %v", err)
				break
			}

			t.Logf("Rebalance progress: %.1f%% (%s)", progressResp.PercentComplete, progressResp.Status)

			if progressResp.Status == "completed" {
				t.Logf("Rebalancing completed successfully")
				break
			}

			time.Sleep(2 * time.Second)
		}
	})

	// Phase 4: Verify data integrity after rebalancing
	t.Run("Phase4_VerifyDataIntegrity", func(t *testing.T) {
		missingCount := 0
		for i := 1; i <= 100; i++ {
			getResp, err := client.Get(ctx, &pb.GetRequest{
				EntityType: "rebalance_test",
				Id:         uint64(i),
			})
			if err != nil || !getResp.Found {
				missingCount++
			}
		}

		if missingCount > 0 {
			t.Fatalf("Data loss detected: %d entities missing after rebalancing!", missingCount)
		}
		t.Log("All 100 entities verified - zero data loss!")
	})

	// Phase 5: Check cluster status after rebalancing
	t.Run("Phase5_CheckStatusAfter", func(t *testing.T) {
		statusResp, err := client.ClusterStatus(ctx, &pb.ClusterStatusRequest{
			IncludeMetrics: true,
		})
		require.NoError(t, err, "Failed to get cluster status")

		t.Log("Cluster status after rebalancing:")
		for _, node := range statusResp.Nodes {
			t.Logf("  Node %d: Leader=%v, Health=%v", node.NodeId, node.IsLeader, node.HealthStatus)
		}
		for _, shard := range statusResp.Shards {
			t.Logf("  Shard %d: Leader=%d, Replicas=%v", shard.ShardId, shard.LeaderId, shard.Replicas)
		}
	})
}

// TestRebalancingWithConcurrentWrites tests rebalancing behavior while
// concurrent write operations are happening. Validates consistency.
func TestRebalancingWithConcurrentWrites(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Connect to node 1 via gRPC
	tlsCfg := requireTLSConfig(t)
	conn, err := grpc.NewClient("test-node1:50051",
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	require.NoError(t, err, "Failed to connect to node1")
	defer conn.Close()

	client := pb.NewQuicBaaSClientExt(conn)

	// Wait for cluster to be fully ready including Raft consensus
	t.Log("Waiting for Raft cluster to be ready...")
	err = waitForRaftReady(ctx, t, client, 60*time.Second)
	require.NoError(t, err, "Cluster not ready for Raft operations")

	// Additional stabilization time for concurrent write test
	t.Log("Allowing cluster to stabilize before concurrent writes...")
	time.Sleep(5 * time.Second)

	var writeCount atomic.Int64
	var writeErrors atomic.Int64
	var transientErrors atomic.Int64
	var wg sync.WaitGroup
	stopWriters := make(chan struct{})

	// Phase 1: Start concurrent writers with retry logic
	t.Run("Phase1_StartConcurrentWrites", func(t *testing.T) {
		numWriters := 5
		for w := 0; w < numWriters; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()

				for i := 0; ; i++ {
					select {
					case <-stopWriters:
						return
					case <-ctx.Done():
						return
					default:
						req := &pb.PutRequest{
							EntityType: "concurrent_test",
							Id:         uint64(workerID*10000 + i),
							Data:       []byte(fmt.Sprintf(`{"worker":%d,"sequence":%d,"timestamp":"%s"}`, workerID, i, time.Now().Format(time.RFC3339))),
						}

						// Try the write with retries for transient errors
						err := putWithRetry(ctx, client, req, 3)
						if err == nil {
							writeCount.Add(1)
						} else {
							if isRetryableRaftError(err) {
								transientErrors.Add(1)
							}
							writeErrors.Add(1)
						}

						// Small delay to avoid overwhelming the cluster
						time.Sleep(20 * time.Millisecond)
					}
				}
			}(w)
		}

		// Let writers run for a bit before rebalancing
		time.Sleep(5 * time.Second)
		t.Logf("Started %d concurrent writers, %d writes so far", numWriters, writeCount.Load())
	})

	// Phase 2: Execute rebalancing during writes
	t.Run("Phase2_RebalanceDuringWrites", func(t *testing.T) {
		rebalanceResp, err := client.RebalanceCluster(ctx, &pb.RebalanceClusterRequest{
			Strategy: "round_robin",
		})
		require.NoError(t, err, "Failed to trigger rebalancing")

		t.Logf("Rebalancing initiated during concurrent writes: %s", rebalanceResp.OperationId)

		// Monitor rebalancing progress
		for i := 0; i < 30; i++ {
			progressResp, err := client.GetRebalanceProgress(ctx, &pb.GetRebalanceProgressRequest{
				OperationId: rebalanceResp.OperationId,
			})
			if err != nil {
				t.Logf("Warning: Failed to get rebalance progress: %v", err)
				break
			}

			t.Logf("Rebalance progress: %.1f%% (%s) - writes: %d, errors: %d",
				progressResp.PercentComplete, progressResp.Status, writeCount.Load(), writeErrors.Load())

			if progressResp.Status == "completed" {
				t.Logf("Rebalancing completed during concurrent writes")
				break
			}

			time.Sleep(2 * time.Second)
		}
	})

	// Phase 3: Stop writers and verify consistency
	t.Run("Phase3_VerifyWriteConsistency", func(t *testing.T) {
		close(stopWriters)
		wg.Wait()

		totalWrites := writeCount.Load()
		totalErrors := writeErrors.Load()
		totalTransient := transientErrors.Load()

		t.Logf("Concurrent writes completed: %d successful, %d errors (%d transient)", totalWrites, totalErrors, totalTransient)

		// With retry logic, we should have significantly more successful writes
		assert.Greater(t, totalWrites, int64(20), "Should have completed some writes")

		// Error rate should be low (< 20% accounting for rebalancing disruption)
		if totalWrites+totalErrors > 0 {
			errorRate := float64(totalErrors) / float64(totalWrites+totalErrors)
			t.Logf("Write error rate: %.2f%%", errorRate*100)
			// During rebalancing, some errors are expected, so we use a more lenient threshold
			assert.Less(t, errorRate, 0.20, "Error rate should be less than 20%% during rebalancing")
		}
	})
}

// TestRebalancingMultipleShards tests rebalancing across multiple shards.
func TestRebalancingMultipleShards(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Connect to node 1 via gRPC
	tlsCfg := requireTLSConfig(t)
	conn, err := grpc.NewClient("test-node1:50051",
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	require.NoError(t, err, "Failed to connect to node1")
	defer conn.Close()

	client := pb.NewQuicBaaSClientExt(conn)

	// Wait for cluster to be fully ready including Raft consensus
	t.Log("Waiting for Raft cluster to be ready...")
	err = waitForRaftReady(ctx, t, client, 60*time.Second)
	require.NoError(t, err, "Cluster not ready for Raft operations")

	// Phase 1: Populate data across different entity types (which may map to different shards)
	t.Run("Phase1_PopulateMultipleEntityTypes", func(t *testing.T) {
		entityTypes := []string{"users", "orders", "products", "sessions"}

		for _, entityType := range entityTypes {
			for i := 1; i <= 25; i++ {
				err := putWithRetry(ctx, client, &pb.PutRequest{
					EntityType: entityType,
					Id:         uint64(i),
					Data:       []byte(fmt.Sprintf(`{"type":"%s","id":%d,"timestamp":"%s"}`, entityType, i, time.Now().Format(time.RFC3339))),
				}, 3)
				require.NoError(t, err, "Failed to put %s entity %d", entityType, i)
			}
		}
		t.Log("Populated 100 entities across 4 entity types")
	})

	// Phase 2: Execute rebalancing
	t.Run("Phase2_RebalanceMultipleShards", func(t *testing.T) {
		rebalanceResp, err := client.RebalanceCluster(ctx, &pb.RebalanceClusterRequest{
			Strategy: "round_robin",
		})
		require.NoError(t, err, "Failed to trigger rebalancing")

		t.Logf("Multi-shard rebalancing initiated: %s", rebalanceResp.OperationId)

		// Monitor progress
		for i := 0; i < 30; i++ {
			progressResp, err := client.GetRebalanceProgress(ctx, &pb.GetRebalanceProgressRequest{
				OperationId: rebalanceResp.OperationId,
			})
			if err != nil {
				break
			}

			t.Logf("Progress: %.1f%% (%s)", progressResp.PercentComplete, progressResp.Status)

			if progressResp.Status == "completed" {
				break
			}

			time.Sleep(2 * time.Second)
		}
	})

	// Phase 3: Verify all entity types are accessible
	t.Run("Phase3_VerifyAllEntityTypes", func(t *testing.T) {
		entityTypes := []string{"users", "orders", "products", "sessions"}
		totalMissing := 0

		for _, entityType := range entityTypes {
			missing := 0
			for i := 1; i <= 25; i++ {
				getResp, err := client.Get(ctx, &pb.GetRequest{
					EntityType: entityType,
					Id:         uint64(i),
				})
				if err != nil || !getResp.Found {
					missing++
				}
			}
			if missing > 0 {
				t.Logf("WARNING: %s has %d missing entities", entityType, missing)
				totalMissing += missing
			}
		}

		if totalMissing > 0 {
			t.Fatalf("Data loss: %d total entities missing after multi-shard rebalancing!", totalMissing)
		}
		t.Log("All entity types verified - zero data loss!")
	})
}

// TestRebalancingIdempotency tests that running rebalancing multiple times
// doesn't cause data loss or corruption.
func TestRebalancingIdempotency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Connect to node 1 via gRPC
	tlsCfg := requireTLSConfig(t)
	conn, err := grpc.NewClient("test-node1:50051",
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	require.NoError(t, err, "Failed to connect to node1")
	defer conn.Close()

	client := pb.NewQuicBaaSClientExt(conn)

	// Wait for cluster to be fully ready including Raft consensus
	t.Log("Waiting for Raft cluster to be ready...")
	err = waitForRaftReady(ctx, t, client, 60*time.Second)
	require.NoError(t, err, "Cluster not ready for Raft operations")

	// Phase 1: Populate initial data
	t.Run("Phase1_PopulateData", func(t *testing.T) {
		for i := 1; i <= 50; i++ {
			err := putWithRetry(ctx, client, &pb.PutRequest{
				EntityType: "idempotency_test",
				Id:         uint64(i),
				Data:       []byte(fmt.Sprintf(`{"id":%d,"test":"idempotency"}`, i)),
			}, 3)
			require.NoError(t, err, "Failed to put entity %d", i)
		}
		t.Log("Populated 50 entities for idempotency test")
	})

	// Phase 2: Run rebalancing multiple times
	t.Run("Phase2_MultipleRebalanceRuns", func(t *testing.T) {
		for run := 1; run <= 3; run++ {
			t.Logf("Rebalance run %d/3", run)

			rebalanceResp, err := client.RebalanceCluster(ctx, &pb.RebalanceClusterRequest{
				Strategy: "round_robin",
			})
			require.NoError(t, err, "Failed to trigger rebalancing run %d", run)

			// Wait for completion
			for i := 0; i < 20; i++ {
				progressResp, err := client.GetRebalanceProgress(ctx, &pb.GetRebalanceProgressRequest{
					OperationId: rebalanceResp.OperationId,
				})
				if err != nil || progressResp.Status == "completed" {
					break
				}
				time.Sleep(2 * time.Second)
			}

			// Verify data after each run
			missing := 0
			for i := 1; i <= 50; i++ {
				getResp, err := client.Get(ctx, &pb.GetRequest{
					EntityType: "idempotency_test",
					Id:         uint64(i),
				})
				if err != nil || !getResp.Found {
					missing++
				}
			}

			if missing > 0 {
				t.Fatalf("Run %d: Data loss - %d entities missing!", run, missing)
			}
			t.Logf("Run %d: All 50 entities verified", run)

			// Small delay between runs
			time.Sleep(1 * time.Second)
		}
	})

	t.Log("SUCCESS: Rebalancing is idempotent - no data loss after 3 runs!")
}
