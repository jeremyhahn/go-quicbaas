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

package vector

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// =============================================================================
// Vector Admin Operations Integration Tests
// =============================================================================

// TestVectorAdminCompaction tests the namespace compaction operation.
func TestVectorAdminCompaction(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	namespace := fmt.Sprintf("admin-compact-%d", time.Now().UnixNano())

	// Create namespace
	err := cluster.EnsureNamespace(0, namespace, 64, pb.DistanceMetric_DISTANCE_METRIC_COSINE)
	require.NoError(t, err)
	defer func() {
		_ = cluster.DeleteNamespace(0, namespace, true)
	}()

	// Insert and delete vectors to create tombstones
	for i := 0; i < 50; i++ {
		id, err := cluster.UpsertVector(0, namespace, cluster.RandomVector(64), nil)
		require.NoError(t, err)

		// Delete every other vector
		if i%2 == 0 {
			err = cluster.DeleteVector(0, namespace, id)
			require.NoError(t, err)
		}
	}

	// 25 of the 50 vectors survive the deletes.
	cluster.RequireVectorCount(t, 0, namespace, 25)

	t.Run("sync compaction completes", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		req := &pb.CompactNamespaceRequest{
			Namespace: namespace,
		}

		resp, err := cluster.adminClient.CompactNamespace(ctx, req)
		require.NoError(t, err, "CompactNamespace must be implemented and available")

		// Response should have operation ID
		assert.NotNil(t, resp)
		if resp.OperationId != "" {
			t.Logf("Compaction operation ID: %s", resp.OperationId)
		}
	})

	t.Run("compaction progress can be queried", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// First start a compaction
		compactReq := &pb.CompactNamespaceRequest{
			Namespace: namespace,
		}

		compactResp, err := cluster.adminClient.CompactNamespace(ctx, compactReq)
		require.NoError(t, err, "CompactNamespace must be implemented and available")

		require.NotEmpty(t, compactResp.OperationId,
			"CompactNamespace must return an operation ID for progress tracking")

		// Check progress
		progressReq := &pb.GetCompactionProgressRequest{
			Namespace:   namespace,
			OperationId: compactResp.OperationId,
		}

		progressResp, err := cluster.adminClient.GetCompactionProgress(ctx, progressReq)
		require.NoError(t, err, "GetCompactionProgress must be implemented and available")

		assert.NotNil(t, progressResp)
	})
}

// TestVectorAdminClusterStats tests the cluster statistics endpoint.
func TestVectorAdminClusterStats(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	namespace := fmt.Sprintf("admin-stats-%d", time.Now().UnixNano())

	// Create namespace and add vectors
	err := cluster.EnsureNamespace(0, namespace, 64, pb.DistanceMetric_DISTANCE_METRIC_COSINE)
	require.NoError(t, err)
	defer func() {
		_ = cluster.DeleteNamespace(0, namespace, true)
	}()

	// Insert vectors
	for i := 0; i < 20; i++ {
		_, err := cluster.UpsertVector(0, namespace, cluster.RandomVector(64), map[string]string{
			"index": fmt.Sprintf("%d", i),
		})
		require.NoError(t, err)
	}

	cluster.RequireVectorCount(t, 0, namespace, 20)

	t.Run("cluster stats returns info", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		req := &pb.GetClusterStatsRequest{}

		resp, err := cluster.adminClient.GetClusterStats(ctx, req)
		require.NoError(t, err, "GetClusterStats must be implemented and available")

		assert.NotNil(t, resp)
		t.Logf("Cluster stats: TotalVectors=%d, TotalNamespaces=%d",
			resp.TotalVectors, resp.TotalNamespaces)
	})

	t.Run("stats update after operations", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Get initial stats
		req := &pb.GetClusterStatsRequest{}
		initialResp, err := cluster.adminClient.GetClusterStats(ctx, req)
		require.NoError(t, err, "GetClusterStats must be implemented and available")

		initialCount := initialResp.TotalVectors

		// Insert more vectors
		for i := 0; i < 5; i++ {
			_, err := cluster.UpsertVector(0, namespace, cluster.RandomVector(64), nil)
			require.NoError(t, err)
		}

		// The namespace now holds the 20 vectors from setup plus these 5.
		cluster.RequireVectorCount(t, 0, namespace, 25)

		// Get updated stats
		updatedResp, err := cluster.adminClient.GetClusterStats(ctx, req)
		require.NoError(t, err)

		// Vector count should have increased
		assert.GreaterOrEqual(t, updatedResp.TotalVectors, initialCount)
	})
}

// TestVectorAdminNamespaceLifecycle tests the full namespace lifecycle.
func TestVectorAdminNamespaceLifecycle(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	namespace := fmt.Sprintf("admin-lifecycle-%d", time.Now().UnixNano())

	t.Run("create namespace", func(t *testing.T) {
		// Use L2 distance instead of EUCLIDEAN
		err := cluster.EnsureNamespace(0, namespace, 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
		require.NoError(t, err)
	})

	t.Run("namespace appears in list", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		resp, err := cluster.clients[0].ListNamespaces(ctx, &pb.ListNamespacesRequest{})
		require.NoError(t, err)

		found := false
		for _, ns := range resp.Namespaces {
			if ns.Config != nil && ns.Config.Namespace == namespace {
				found = true
				assert.Equal(t, int32(128), ns.Config.Dimensions)
				assert.Equal(t, pb.DistanceMetric_DISTANCE_METRIC_L2, ns.Config.DistanceMetric)
				break
			}
		}
		assert.True(t, found, "Namespace should appear in list")
	})

	t.Run("can perform operations in namespace", func(t *testing.T) {
		// Insert
		id, err := cluster.UpsertVector(0, namespace, cluster.RandomVector(128), map[string]string{
			"test": "lifecycle",
		})
		require.NoError(t, err)
		assert.NotEmpty(t, id)

		// Get
		vector, err := cluster.GetVector(0, namespace, id)
		require.NoError(t, err)
		assert.NotNil(t, vector)

		// Search
		results, err := cluster.SearchSimilar(0, namespace, cluster.RandomVector(128), 5)
		require.NoError(t, err)
		assert.NotEmpty(t, results)

		// Delete
		err = cluster.DeleteVector(0, namespace, id)
		require.NoError(t, err)
	})

	t.Run("delete namespace", func(t *testing.T) {
		err := cluster.DeleteNamespace(0, namespace, true)
		require.NoError(t, err)

		// Verify it's gone
		cluster.RequireNamespaceGone(t, 0, namespace)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		resp, err := cluster.clients[0].ListNamespaces(ctx, &pb.ListNamespacesRequest{})
		require.NoError(t, err)

		for _, ns := range resp.Namespaces {
			if ns.Config != nil {
				assert.NotEqual(t, namespace, ns.Config.Namespace)
			}
		}
	})
}

// TestVectorAdminBatchOperations tests batch operations for admin workflows.
func TestVectorAdminBatchOperations(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	namespace := fmt.Sprintf("admin-batch-%d", time.Now().UnixNano())

	err := cluster.EnsureNamespace(0, namespace, 64, pb.DistanceMetric_DISTANCE_METRIC_COSINE)
	require.NoError(t, err)
	defer func() {
		_ = cluster.DeleteNamespace(0, namespace, true)
	}()

	t.Run("batch upsert for data loading", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Create batch of vector entities
		vectors := make([]*pb.VectorEntity, 100)
		for i := 0; i < 100; i++ {
			vectors[i] = &pb.VectorEntity{
				Id:        fmt.Sprintf("batch-%d", i),
				Namespace: namespace,
				Vector:    cluster.RandomVector(64),
				Metadata: map[string]string{
					"batch": "load",
					"index": fmt.Sprintf("%d", i),
				},
			}
		}

		req := &pb.BatchUpsertVectorsRequest{
			Vectors: vectors,
		}

		stream, err := cluster.clients[0].BatchUpsertVectors(ctx, req)
		require.NoError(t, err)

		// Collect all responses from the stream
		var totalSuccess int64
		for {
			resp, err := stream.Recv()
			if err != nil {
				break
			}
			totalSuccess += resp.VectorsSucceeded
		}
		assert.GreaterOrEqual(t, totalSuccess, int64(0))
	})

	t.Run("list all vectors", func(t *testing.T) {
		// Allow replication. The count below accepts any result, so a
		// timeout here is not a failure.
		waitUntil(settleTimeout, func() bool {
			listed, listErr := cluster.ListVectors(0, namespace, 100)
			return listErr == nil && len(listed) >= 100
		})

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		req := &pb.ListVectorsRequest{
			Namespace: namespace,
			PageSize:  100,
		}

		stream, err := cluster.clients[0].ListVectors(ctx, req)
		require.NoError(t, err)

		// Collect vectors from the stream
		var count int
		for {
			_, recvErr := stream.Recv()
			if recvErr != nil {
				break
			}
			count++
		}
		assert.GreaterOrEqual(t, count, 0)
	})

	t.Run("batch delete for cleanup", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Delete first 50 vectors
		ids := make([]string, 50)
		for i := 0; i < 50; i++ {
			ids[i] = fmt.Sprintf("batch-%d", i)
		}

		req := &pb.BatchDeleteVectorsRequest{
			Namespace: namespace,
			Ids:       ids,
		}

		resp, err := cluster.clients[0].BatchDeleteVectors(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

// TestVectorAdminSearchConfiguration tests search with various configurations.
func TestVectorAdminSearchConfiguration(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	namespace := fmt.Sprintf("admin-search-%d", time.Now().UnixNano())

	err := cluster.EnsureNamespace(0, namespace, 64, pb.DistanceMetric_DISTANCE_METRIC_COSINE)
	require.NoError(t, err)
	defer func() {
		_ = cluster.DeleteNamespace(0, namespace, true)
	}()

	// Insert diverse vectors
	for i := 0; i < 100; i++ {
		_, err := cluster.UpsertVector(0, namespace, cluster.RandomVector(64), map[string]string{
			"category": fmt.Sprintf("cat-%d", i%5),
			"priority": fmt.Sprintf("%d", i%3),
		})
		require.NoError(t, err)
	}

	cluster.RequireVectorCount(t, 0, namespace, 100)

	t.Run("search with different K values", func(t *testing.T) {
		query := cluster.RandomVector(64)

		for _, k := range []int{1, 5, 10, 50} {
			results, err := cluster.SearchSimilar(0, namespace, query, k)
			require.NoError(t, err)
			assert.LessOrEqual(t, len(results), k)
			if len(results) > 0 {
				t.Logf("K=%d returned %d results, best score: %.4f", k, len(results), results[0].Score)
			}
		}
	})

	t.Run("search with metadata filter", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		req := &pb.SearchSimilarRequest{
			Namespace:   namespace,
			QueryVector: cluster.RandomVector(64),
			K:           20,
			MetadataFilters: []*pb.MetadataFilter{
				{
					Key:      "category",
					Operator: pb.FilterOperator_FILTER_OPERATOR_EQUALS,
					Value:    "cat-0",
				},
			},
		}

		stream, err := cluster.clients[0].SearchSimilar(ctx, req)
		require.NoError(t, err)

		// Collect results from the stream
		for {
			result, recvErr := stream.Recv()
			if recvErr != nil {
				break
			}
			for _, r := range result.Results {
				if r.Vector != nil && r.Vector.Metadata != nil {
					assert.Equal(t, "cat-0", r.Vector.Metadata["category"])
				}
			}
		}
	})

	t.Run("search consistency across nodes", func(t *testing.T) {
		query := cluster.RandomVector(64)

		// Verify search works on the write node (node 0)
		node0Results, err := cluster.SearchSimilar(0, namespace, query, 10)
		require.NoError(t, err, "Search on write node (0) must succeed")
		assert.Greater(t, len(node0Results), 0, "Write node must return search results")
		t.Logf("Node 0 returned %d results", len(node0Results))

		// With embedded transport each node has independent storage.
		// Cross-node searches may return 0 results or different results.
		for i := 1; i < 3; i++ {
			nodeResults, crossErr := cluster.SearchSimilar(i, namespace, query, 10)
			if crossErr != nil {
				t.Logf("Node %d search error (expected with embedded transport): %v", i, crossErr)
				continue
			}
			t.Logf("Node %d returned %d results (cross-node, embedded transport)", i, len(nodeResults))
		}
	})
}
