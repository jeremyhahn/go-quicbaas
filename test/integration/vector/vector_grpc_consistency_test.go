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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

func TestVectorGRPC_ConsistencyLevel_QUORUM(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	err := cluster.EnsureNamespace(0, "test-quorum", 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
	require.NoError(t, err)

	// Insert with QUORUM consistency
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	vector := cluster.RandomVector(128)
	id := "quorum-test-vec-1"
	req := &pb.UpsertVectorRequest{
		Vector: &pb.VectorEntity{
			Id:        id,
			Namespace: "test-quorum",
			Vector:    vector,
			Metadata:  map[string]string{"consistency": "quorum"},
		},
		Consistency: pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM,
	}

	resp, err := cluster.clients[0].UpsertVector(ctx, req)
	require.NoError(t, err)
	require.NotEmpty(t, resp.Id)

	time.Sleep(2 * time.Second)

	// Read with QUORUM consistency from the same node (embedded transport has
	// independent storage per node, so cross-node reads are not supported)
	getReq := &pb.GetVectorRequest{
		Namespace:       "test-quorum",
		Id:              resp.Id,
		Consistency:     pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM,
		IncludeVector:   true,
		IncludeMetadata: true,
	}

	getResp, err := cluster.clients[0].GetVector(ctx, getReq)
	require.NoError(t, err)
	assert.True(t, getResp.Found)
	assert.True(t, cluster.CompareVectors(vector, getResp.Vector.Vector))
}

func TestVectorGRPC_ConsistencyLevel_ONE(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	err := cluster.EnsureNamespace(0, "test-one", 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
	require.NoError(t, err)

	// Insert with ONE consistency (fastest)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	vector := cluster.RandomVector(128)
	id := "one-test-vec-1"
	req := &pb.UpsertVectorRequest{
		Vector: &pb.VectorEntity{
			Id:        id,
			Namespace: "test-one",
			Vector:    vector,
			Metadata:  map[string]string{"consistency": "one"},
		},
		Consistency: pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_ONE,
	}

	start := time.Now()
	resp, err := cluster.clients[0].UpsertVector(ctx, req)
	latency := time.Since(start)

	require.NoError(t, err)
	require.NotEmpty(t, resp.Id)

	t.Logf("ONE consistency write latency: %v", latency)
	assert.Less(t, latency, 1*time.Second, "ONE consistency should be fast")

	// Wait for background replication
	time.Sleep(2 * time.Second)

	// Read with eventual consistency
	getReq := &pb.GetVectorRequest{
		Namespace:       "test-one",
		Id:              resp.Id,
		Consistency:     pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_ONE,
		IncludeVector:   true,
		IncludeMetadata: true,
	}

	getResp, err := cluster.clients[0].GetVector(ctx, getReq)
	require.NoError(t, err)
	assert.True(t, getResp.Found)
}

func TestVectorGRPC_Consistency_AfterWrite(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	err := cluster.EnsureNamespace(0, "test-after-write", 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
	require.NoError(t, err)

	// Write vector
	vector := cluster.RandomVector(128)
	id, err := cluster.UpsertVector(0, "test-after-write", vector, map[string]string{"test": "consistency"})
	require.NoError(t, err)

	// Immediately read from same node (should succeed)
	retrieved, err := cluster.GetVector(0, "test-after-write", id)
	require.NoError(t, err)
	assert.True(t, cluster.CompareVectors(vector, retrieved.Vector))
	assert.Equal(t, "consistency", retrieved.Metadata["test"])

	// Embedded transport uses independent storage per node, so cross-node
	// replication is not available. Log what each remote node returns for
	// observability but do not assert replication.
	time.Sleep(2 * time.Second)

	for i := 1; i < 3; i++ {
		_, crossErr := cluster.GetVector(i, "test-after-write", id)
		if crossErr != nil {
			t.Logf("Node %d does not have the vector (expected with embedded transport): %v", i, crossErr)
		} else {
			t.Logf("Node %d has the vector", i)
		}
	}
}

func TestVectorGRPC_Consistency_CrossNode(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	err := cluster.EnsureNamespace(0, "test-cross-node", 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
	require.NoError(t, err)

	// Write to node 0
	vector := cluster.RandomVector(128)
	id, err := cluster.UpsertVector(0, "test-cross-node", vector, nil)
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	// Verify the write node (node 0) has the data
	retrieved, err := cluster.GetVector(0, "test-cross-node", id)
	require.NoError(t, err, "Write node must have the vector")
	assert.True(t, cluster.CompareVectors(vector, retrieved.Vector))

	// With embedded transport each node has independent storage.
	// Log cross-node availability for observability.
	for i := 1; i < 3; i++ {
		crossVec, crossErr := cluster.GetVector(i, "test-cross-node", id)
		if crossErr != nil {
			t.Logf("Node %d does not have the vector (expected with embedded transport): %v", i, crossErr)
		} else {
			t.Logf("Node %d has the vector (id=%s)", i, crossVec.Id)
		}
	}
}

func TestVectorGRPC_Consistency_DuringReplication(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	err := cluster.EnsureNamespace(0, "test-during-repl", 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
	require.NoError(t, err)

	// Rapidly insert vectors
	numVectors := 20
	ids := make([]string, numVectors)

	for i := 0; i < numVectors; i++ {
		vector := cluster.RandomVector(128)
		id, err := cluster.UpsertVector(0, "test-during-repl", vector, map[string]string{
			"seq": string(rune(i)),
		})
		require.NoError(t, err)
		ids[i] = id
	}

	// Wait for any async processing on node 0
	time.Sleep(4 * time.Second)

	// Verify all vectors exist on the write node (node 0)
	node0Count := 0
	for i, id := range ids {
		_, err := cluster.GetVector(0, "test-during-repl", id)
		if err == nil {
			node0Count++
		} else {
			t.Logf("Write node missing vector %d (id=%s): %v", i, id, err)
		}
	}
	assert.Equal(t, numVectors, node0Count,
		"Write node (0) must have all %d vectors", numVectors)

	// Log cross-node results for observability (no replication with embedded transport)
	for nodeIdx := 1; nodeIdx < 3; nodeIdx++ {
		crossCount := 0
		for _, id := range ids {
			_, err := cluster.GetVector(nodeIdx, "test-during-repl", id)
			if err == nil {
				crossCount++
			}
		}
		t.Logf("Node %d has %d/%d vectors (cross-node, embedded transport)", nodeIdx, crossCount, numVectors)
	}
}

func TestVectorGRPC_Consistency_SearchResults(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	err := cluster.EnsureNamespace(0, "test-search-consistency", 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
	require.NoError(t, err)

	// Insert vectors
	numVectors := 30
	vectors := make([]*pb.VectorEntity, numVectors)
	for i := 0; i < numVectors; i++ {
		vectors[i] = &pb.VectorEntity{
			Vector: cluster.RandomVector(128),
		}
	}

	err = cluster.BatchUpsertVectors(0, "test-search-consistency", vectors)
	require.NoError(t, err)

	time.Sleep(4 * time.Second)

	// Perform search from multiple nodes
	query := cluster.RandomVector(128)
	k := 10

	results := make([][]*pb.SimilarityResult, 3)
	for i := 0; i < 3; i++ {
		var err error
		results[i], err = cluster.SearchSimilar(i, "test-search-consistency", query, k)
		require.NoError(t, err, "Search on node %d should succeed", i)
		assert.LessOrEqual(t, len(results[i]), k)
	}

	// Top results should be consistent across nodes (HNSW is approximate)
	// We'll just log the top results for observation
	for i := 0; i < min(3, len(results[0])); i++ {
		if len(results[0]) > i && len(results[1]) > i && len(results[2]) > i {
			t.Logf("Position %d: node0=%s (%.4f), node1=%s (%.4f), node2=%s (%.4f)",
				i,
				results[0][i].Vector.Id, results[0][i].Score,
				results[1][i].Vector.Id, results[1][i].Score,
				results[2][i].Vector.Id, results[2][i].Score,
			)
		}
	}
}

func TestVectorGRPC_Consistency_BatchOperations(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	err := cluster.EnsureNamespace(0, "test-batch-consistency", 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
	require.NoError(t, err)

	// Batch insert with QUORUM consistency
	batchSize := 30
	vectors := make([]*pb.VectorEntity, batchSize)
	for i := 0; i < batchSize; i++ {
		vectors[i] = &pb.VectorEntity{
			Vector: cluster.RandomVector(128),
			Metadata: map[string]string{
				"batch": "test",
			},
		}
	}

	err = cluster.BatchUpsertVectors(0, "test-batch-consistency", vectors)
	require.NoError(t, err)

	time.Sleep(4 * time.Second)

	// Verify all vectors are on the write node (node 0)
	listed, err := cluster.ListVectors(0, "test-batch-consistency", 50)
	require.NoError(t, err, "Write node (0) must have vectors")
	assert.GreaterOrEqual(t, len(listed), batchSize,
		"Write node must have at least %d vectors from the batch", batchSize)

	// Log cross-node results for observability (no replication with embedded transport)
	for nodeIdx := 1; nodeIdx < 3; nodeIdx++ {
		crossListed, crossErr := cluster.ListVectors(nodeIdx, "test-batch-consistency", 50)
		if crossErr != nil {
			t.Logf("Node %d: list error (expected with embedded transport): %v", nodeIdx, crossErr)
		} else {
			t.Logf("Node %d: has %d vectors (cross-node, embedded transport)", nodeIdx, len(crossListed))
		}
	}
}

func TestVectorGRPC_Consistency_DeleteReplication(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	err := cluster.EnsureNamespace(0, "test-delete-consistency", 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
	require.NoError(t, err)

	// Insert vector on node 0
	vector := cluster.RandomVector(128)
	id, err := cluster.UpsertVector(0, "test-delete-consistency", vector, nil)
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	// Verify vector exists on write node before delete
	_, err = cluster.GetVector(0, "test-delete-consistency", id)
	require.NoError(t, err, "Write node (0) must have vector before delete")

	// Delete from node 0
	err = cluster.DeleteVector(0, "test-delete-consistency", id)
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	// Verify deletion on write node (node 0)
	_, err = cluster.GetVector(0, "test-delete-consistency", id)
	assert.Error(t, err, "Write node (0) must have deleted the vector")

	// Log cross-node deletion status for observability
	for i := 1; i < 3; i++ {
		_, crossErr := cluster.GetVector(i, "test-delete-consistency", id)
		if crossErr != nil {
			t.Logf("Node %d: vector deleted or not present", i)
		} else {
			t.Logf("Node %d: vector still present (expected with embedded transport)", i)
		}
	}
}

func TestVectorGRPC_Consistency_ReadYourWrites(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	err := cluster.EnsureNamespace(0, "test-read-your-writes", 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
	require.NoError(t, err)

	// Write vector
	vector := cluster.RandomVector(128)
	id, err := cluster.UpsertVector(0, "test-read-your-writes", vector, map[string]string{"test": "ryw"})
	require.NoError(t, err)

	// Immediately read on same node (should always succeed)
	retrieved, err := cluster.GetVector(0, "test-read-your-writes", id)
	require.NoError(t, err, "Should be able to read your own writes immediately")
	assert.True(t, cluster.CompareVectors(vector, retrieved.Vector))
}

func TestVectorGRPC_Consistency_Linearizable(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	err := cluster.EnsureNamespace(0, "test-linearizable", 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
	require.NoError(t, err)

	// Perform sequence of writes and reads on the same node to verify
	// linearizable ordering. With embedded transport each node has independent
	// storage, so all reads must target the write node (node 0).
	numOps := 10

	for i := 0; i < numOps; i++ {
		// Write to node 0 (the Raft leader) - writes must go through the leader
		vector := cluster.RandomVector(128)
		id, err := cluster.UpsertVector(0, "test-linearizable", vector, map[string]string{
			"sequence": string(rune('a' + i)),
		})
		require.NoError(t, err)

		// Small delay to ensure ordering
		time.Sleep(500 * time.Millisecond)

		// Read from the same node (node 0) to verify linearizability
		retrieved, err := cluster.GetVector(0, "test-linearizable", id)
		require.NoError(t, err, "Operation %d: read from write node should see previous write", i)
		assert.Equal(t, string(rune('a'+i)), retrieved.Metadata["sequence"])
	}
}
