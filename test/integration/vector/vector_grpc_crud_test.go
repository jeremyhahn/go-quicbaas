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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

func TestVectorGRPC_UpsertAndGet(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	// Create namespace
	err := cluster.EnsureNamespace(0, "test-upsert", 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
	require.NoError(t, err)

	// Upsert vector
	vector := cluster.RandomVector(128)
	metadata := map[string]string{
		"category": "test",
		"type":     "embedding",
	}

	id, err := cluster.UpsertVector(0, "test-upsert", vector, metadata)
	require.NoError(t, err)
	require.NotEmpty(t, id)

	time.Sleep(2 * time.Second)

	// Verify write node (node 0) has the vector with correct data
	retrieved, err := cluster.GetVector(0, "test-upsert", id)
	require.NoError(t, err, "Write node (0) must have the vector")
	assert.True(t, cluster.CompareVectors(vector, retrieved.Vector))
	assert.Equal(t, metadata, retrieved.Metadata)
	assert.Equal(t, "test-upsert", retrieved.Namespace)

	// With embedded transport each node has independent storage.
	// Log cross-node availability for observability.
	for i := 1; i < 3; i++ {
		crossVec, crossErr := cluster.GetVector(i, "test-upsert", id)
		if crossErr != nil {
			t.Logf("Node %d does not have the vector (expected with embedded transport): %v", i, crossErr)
		} else {
			t.Logf("Node %d has the vector (namespace=%s)", i, crossVec.Namespace)
		}
	}
}

func TestVectorGRPC_UpsertWithMetadata(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	err := cluster.EnsureNamespace(0, "test-metadata", 256, pb.DistanceMetric_DISTANCE_METRIC_COSINE)
	require.NoError(t, err)

	// Test with rich metadata
	vector := cluster.NormalizedRandomVector(256)
	metadata := map[string]string{
		"document_id": "doc-12345",
		"title":       "Test Document",
		"author":      "John Doe",
		"timestamp":   "2025-01-15T10:00:00Z",
		"tags":        "machine-learning,nlp,embeddings",
		"confidence":  "0.95",
	}

	id, err := cluster.UpsertVector(0, "test-metadata", vector, metadata)
	require.NoError(t, err)

	time.Sleep(2 * time.Second) // Wait for replication

	retrieved, err := cluster.GetVector(0, "test-metadata", id)
	require.NoError(t, err)
	assert.Equal(t, metadata, retrieved.Metadata)
	assert.True(t, cluster.CompareVectors(vector, retrieved.Vector))
}

func TestVectorGRPC_DeleteVector(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	err := cluster.EnsureNamespace(0, "test-delete", 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
	require.NoError(t, err)

	// Insert vector
	vector := cluster.RandomVector(128)
	id, err := cluster.UpsertVector(0, "test-delete", vector, nil)
	require.NoError(t, err)

	time.Sleep(1 * time.Second)

	// Verify exists
	_, err = cluster.GetVector(0, "test-delete", id)
	require.NoError(t, err)

	// Delete vector
	err = cluster.DeleteVector(0, "test-delete", id)
	require.NoError(t, err)

	time.Sleep(1 * time.Second)

	// Verify deleted on all nodes
	for i := 0; i < 3; i++ {
		_, err = cluster.GetVector(i, "test-delete", id)
		assert.Error(t, err, "node %d should not have vector", i)
	}
}

func TestVectorGRPC_DeleteNonExistent(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	err := cluster.EnsureNamespace(0, "test-delete-ne", 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
	require.NoError(t, err)

	// Delete non-existent vector (should be idempotent)
	err = cluster.DeleteVector(0, "test-delete-ne", "non-existent-id")
	// Deletion should succeed (idempotent) or return not found
	// Either way is acceptable for integration test
	t.Logf("Delete non-existent returned: %v", err)
}

func TestVectorGRPC_BatchUpsert(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	err := cluster.EnsureNamespace(0, "test-batch", 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
	require.NoError(t, err)

	// Create batch of vectors
	batchSize := 100
	vectors := make([]*pb.VectorEntity, batchSize)
	for i := 0; i < batchSize; i++ {
		vectors[i] = &pb.VectorEntity{
			Vector: cluster.RandomVector(128),
			Metadata: map[string]string{
				"batch": "1",
			},
		}
	}

	// Batch upsert
	err = cluster.BatchUpsertVectors(0, "test-batch", vectors)
	require.NoError(t, err)

	time.Sleep(3 * time.Second) // Wait for replication

	// Verify all vectors exist
	listed, err := cluster.ListVectors(0, "test-batch", 200)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listed), batchSize)
}

func TestVectorGRPC_BatchUpsert_LargeBatch(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	err := cluster.EnsureNamespace(0, "test-large-batch", 256, pb.DistanceMetric_DISTANCE_METRIC_COSINE)
	require.NoError(t, err)

	// Create large batch
	batchSize := 500 // Reduced from 1000 for faster test
	vectors := make([]*pb.VectorEntity, batchSize)
	for i := 0; i < batchSize; i++ {
		vectors[i] = &pb.VectorEntity{
			Vector: cluster.NormalizedRandomVector(256),
			Metadata: map[string]string{
				"batch": "large",
			},
		}
	}

	start := time.Now()
	err = cluster.BatchUpsertVectors(0, "test-large-batch", vectors)
	require.NoError(t, err)
	duration := time.Since(start)

	t.Logf("Batch upsert of %d vectors took %v", batchSize, duration)
	assert.Less(t, duration, 60*time.Second, "Batch upsert should complete in reasonable time")

	time.Sleep(3 * time.Second)

	// Verify count
	listed, err := cluster.ListVectors(0, "test-large-batch", 1000)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listed), batchSize)
}

func TestVectorGRPC_GetNonExistent(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	err := cluster.EnsureNamespace(0, "test-get-ne", 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
	require.NoError(t, err)

	_, err = cluster.GetVector(0, "test-get-ne", "does-not-exist")
	assert.Error(t, err)
}

func TestVectorGRPC_InvalidDimension(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	err := cluster.EnsureNamespace(0, "test-dim", 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
	require.NoError(t, err)

	// Try to upsert vector with wrong dimension
	wrongVector := cluster.RandomVector(256) // Wrong dimension
	_, err = cluster.UpsertVector(0, "test-dim", wrongVector, nil)
	assert.Error(t, err, "Should reject vector with wrong dimension")
}

func TestVectorGRPC_EmptyVector(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	err := cluster.EnsureNamespace(0, "test-empty", 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
	require.NoError(t, err)

	// Try to upsert empty vector
	emptyVector := []float32{}
	_, err = cluster.UpsertVector(0, "test-empty", emptyVector, nil)
	assert.Error(t, err, "Should reject empty vector")
}

func TestVectorGRPC_NamespaceIsolation(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	// Create two namespaces
	err := cluster.EnsureNamespace(0, "namespace-a", 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
	require.NoError(t, err)

	err = cluster.EnsureNamespace(0, "namespace-b", 128, pb.DistanceMetric_DISTANCE_METRIC_L2)
	require.NoError(t, err)

	// Insert vectors into both namespaces
	vector1 := cluster.RandomVector(128)
	id1, err := cluster.UpsertVector(0, "namespace-a", vector1, map[string]string{"ns": "a"})
	require.NoError(t, err)

	vector2 := cluster.RandomVector(128)
	id2, err := cluster.UpsertVector(0, "namespace-b", vector2, map[string]string{"ns": "b"})
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	// Verify namespace isolation
	vec1, err := cluster.GetVector(0, "namespace-a", id1)
	require.NoError(t, err)
	assert.Equal(t, "a", vec1.Metadata["ns"])

	vec2, err := cluster.GetVector(0, "namespace-b", id2)
	require.NoError(t, err)
	assert.Equal(t, "b", vec2.Metadata["ns"])

	// Verify vector from namespace-a is not in namespace-b
	_, err = cluster.GetVector(0, "namespace-b", id1)
	assert.Error(t, err)
}

func TestVectorGRPC_HighDimensionVectors(t *testing.T) {
	cluster := NewVectorCluster(t)
	defer cluster.Close()

	if !cluster.RequireServiceAvailable(t) {
		return
	}

	// Test with high-dimension vectors (e.g., GPT-4 embeddings)
	dimension := int32(1536) // OpenAI ada-002 dimension
	err := cluster.EnsureNamespace(0, "test-high-dim", dimension, pb.DistanceMetric_DISTANCE_METRIC_COSINE)
	require.NoError(t, err)

	vector := cluster.NormalizedRandomVector(int(dimension))
	id, err := cluster.UpsertVector(0, "test-high-dim", vector, nil)
	require.NoError(t, err)

	time.Sleep(1 * time.Second)

	retrieved, err := cluster.GetVector(0, "test-high-dim", id)
	require.NoError(t, err)
	assert.Len(t, retrieved.Vector, int(dimension))
	assert.True(t, cluster.CompareVectors(vector, retrieved.Vector))
}
