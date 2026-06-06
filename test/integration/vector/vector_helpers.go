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
	"io"
	"math"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"

	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// UniqueNamespace generates a unique namespace name using timestamp to avoid conflicts
func UniqueNamespace(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()%1000000000)
}

// VectorCluster represents a 3-node test cluster for vector database testing
type VectorCluster struct {
	t           *testing.T
	clients     [3]pb.VectorDBClient
	conns       [3]*googlegrpc.ClientConn
	grpcPorts   [3]int
	adminClient pb.VectorAdminClient // Admin client for cluster operations

}

// NewVectorCluster creates a vector cluster helper that connects to running QuicBaaS nodes.
// The vector service must be available; connection or service errors cause a fatal test failure.
func NewVectorCluster(t *testing.T) *VectorCluster {
	t.Helper()

	// Get cluster endpoints from environment (same as other integration tests)
	endpoints := helpers.GetClusterEndpoints()

	cluster := &VectorCluster{
		t:         t,
		grpcPorts: [3]int{50051, 50052, 50053},
	}

	tlsCfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		t.Fatalf("load integration TLS config: %v", err)
	}

	// Connect to each node's gRPC endpoint
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		conn, err := googlegrpc.DialContext(
			ctx,
			endpoints[i],
			googlegrpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
			googlegrpc.WithBlock(),
		)
		cancel()

		if err != nil {
			// Clean up any connections made so far
			for j := 0; j < i; j++ {
				if cluster.conns[j] != nil {
					cluster.conns[j].Close()
				}
			}
			t.Fatalf("Vector test failed: could not connect to node %d at %s: %v", i+1, endpoints[i], err)
			return nil
		}
		cluster.conns[i] = conn
		cluster.clients[i] = pb.NewVectorDBClient(conn)
	}

	// Create admin client on node 0
	cluster.adminClient = pb.NewVectorAdminClient(cluster.conns[0])

	// Verify Vector service is available by attempting a simple operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = cluster.clients[0].ListNamespaces(ctx, &pb.ListNamespacesRequest{})
	if err != nil {
		// Clean up connections before failing
		for i := range cluster.conns {
			if cluster.conns[i] != nil {
				cluster.conns[i].Close()
			}
		}
		t.Fatalf("Vector service must be available: ListNamespaces probe failed: %v", err)
		return nil
	}

	// Wait for cluster to be ready
	time.Sleep(2 * time.Second)

	return cluster
}

// RequireServiceAvailable asserts that the vector service is available.
// The service availability is verified during NewVectorCluster; this method
// exists for backward compatibility and always returns true.
func (c *VectorCluster) RequireServiceAvailable(t *testing.T) bool {
	t.Helper()
	return true
}

// LeaderNodeIdx discovers which cluster node is the leader for the vector
// shard by attempting a probe namespace create on each node. Returns 0 if
// no leader is found (callers should use 0 as default).
func (c *VectorCluster) LeaderNodeIdx() int {
	probeNS := fmt.Sprintf("__probe_%d", time.Now().UnixNano())
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := c.clients[i].CreateNamespace(ctx, &pb.CreateNamespaceRequest{
			Config: &pb.NamespaceConfig{
				Namespace:  probeNS,
				Dimensions: 2,
			},
		})
		cancel()
		if err == nil {
			// Clean up probe (best-effort)
			delCtx, delCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = pb.NewVectorDBClient(c.conns[i]).DeleteNamespace(delCtx, &pb.DeleteNamespaceRequest{Namespace: probeNS})
			delCancel()
			c.t.Logf("Vector shard leader is on node %d", i+1)
			return i
		}
	}
	c.t.Log("Warning: no vector shard leader found, defaulting to node 0")
	return 0
}

// Close shuts down the cluster connections
func (c *VectorCluster) Close() {
	if c == nil {
		return
	}
	for i := range c.conns {
		if c.conns[i] != nil {
			c.conns[i].Close()
		}
	}
}

// UpsertVector upserts a vector on the specified node
func (c *VectorCluster) UpsertVector(nodeIdx int, namespace string, vector []float32, metadata map[string]string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Generate a unique ID
	id := fmt.Sprintf("vec-%d-%d", time.Now().UnixNano(), rand.Intn(1000000))

	req := &pb.UpsertVectorRequest{
		Vector: &pb.VectorEntity{
			Id:        id,
			Namespace: namespace,
			Vector:    vector,
			Metadata:  metadata,
		},
		Consistency: pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM,
	}

	resp, err := c.clients[nodeIdx].UpsertVector(ctx, req)
	if err != nil {
		return "", err
	}

	return resp.Id, nil
}

// GetVector retrieves a vector from the specified node
func (c *VectorCluster) GetVector(nodeIdx int, namespace, id string) (*pb.VectorEntity, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &pb.GetVectorRequest{
		Namespace:       namespace,
		Id:              id,
		Consistency:     pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM,
		IncludeVector:   true,
		IncludeMetadata: true,
	}

	resp, err := c.clients[nodeIdx].GetVector(ctx, req)
	if err != nil {
		return nil, err
	}

	if !resp.Found {
		return nil, fmt.Errorf("vector not found")
	}

	return resp.Vector, nil
}

// SearchSimilar performs k-NN search on the specified node
func (c *VectorCluster) SearchSimilar(nodeIdx int, namespace string, query []float32, k int) ([]*pb.SimilarityResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &pb.SearchSimilarRequest{
		Namespace:       namespace,
		QueryVector:     query,
		K:               int32(k),
		Consistency:     pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM,
		IncludeVectors:  true,
		IncludeMetadata: true,
	}

	stream, err := c.clients[nodeIdx].SearchSimilar(ctx, req)
	if err != nil {
		return nil, err
	}

	var results []*pb.SimilarityResult
	for {
		resp, err := stream.Recv()
		if err != nil {
			// Check if it's a real error (not EOF)
			if err != io.EOF {
				return nil, err
			}
			break
		}
		results = append(results, resp.Results...)
		if resp.IsFinal {
			break
		}
	}

	return results, nil
}

// DeleteVector deletes a vector from the specified node
func (c *VectorCluster) DeleteVector(nodeIdx int, namespace, id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &pb.DeleteVectorRequest{
		Namespace:   namespace,
		Id:          id,
		Consistency: pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM,
	}

	_, err := c.clients[nodeIdx].DeleteVector(ctx, req)
	return err
}

// BatchUpsertVectors upserts multiple vectors
func (c *VectorCluster) BatchUpsertVectors(nodeIdx int, namespace string, vectors []*pb.VectorEntity) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Set namespace and generate IDs for all vectors
	for i, v := range vectors {
		v.Namespace = namespace
		if v.Id == "" {
			v.Id = fmt.Sprintf("batch-vec-%d-%d", time.Now().UnixNano(), i)
		}
	}

	req := &pb.BatchUpsertVectorsRequest{
		Vectors:     vectors,
		Consistency: pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM,
	}

	stream, err := c.clients[nodeIdx].BatchUpsertVectors(ctx, req)
	if err != nil {
		return err
	}

	// Read all responses
	for {
		resp, err := stream.Recv()
		if err != nil {
			break
		}
		if resp.IsFinal {
			if resp.VectorsFailed > 0 {
				return fmt.Errorf("batch upsert failed: %d vectors failed", resp.VectorsFailed)
			}
			break
		}
	}

	return nil
}

// CreateNamespace creates a new namespace
func (c *VectorCluster) CreateNamespace(nodeIdx int, namespace string, dimension int32, metric pb.DistanceMetric) error {
	req := &pb.CreateNamespaceRequest{
		Config: &pb.NamespaceConfig{
			Namespace:      namespace,
			Dimensions:     dimension,
			DistanceMetric: metric,
			IndexType:      pb.IndexType_INDEX_TYPE_HNSW,
		},
	}

	// Try the requested node first, then fall back to other nodes.
	// The vector shard leader may be on any node.
	nodes := []int{nodeIdx}
	for i := 0; i < 3; i++ {
		if i != nodeIdx {
			nodes = append(nodes, i)
		}
	}
	var lastErr error
	for _, idx := range nodes {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := c.clients[idx].CreateNamespace(ctx, req)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		// If error is not leader/shard related, try next node
		if !strings.Contains(err.Error(), "not leader") && !strings.Contains(err.Error(), "vector shard") && !strings.Contains(err.Error(), "no leader") {
			return err // Non-retryable error
		}
	}
	return lastErr
}

// EnsureNamespace deletes the namespace if it exists, then creates it fresh.
// This ensures tests have a clean namespace state even on re-runs.
func (c *VectorCluster) EnsureNamespace(nodeIdx int, namespace string, dimension int32, metric pb.DistanceMetric) error {
	// Try to delete the namespace first (ignore errors if it doesn't exist)
	_ = c.DeleteNamespace(nodeIdx, namespace, true)
	// Wait a bit for deletion to propagate
	time.Sleep(100 * time.Millisecond)
	// Create the namespace
	return c.CreateNamespace(nodeIdx, namespace, dimension, metric)
}

// DeleteNamespace deletes a namespace
func (c *VectorCluster) DeleteNamespace(nodeIdx int, namespace string, force bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &pb.DeleteNamespaceRequest{
		Namespace: namespace,
		Force:     force,
	}

	_, err := c.clients[nodeIdx].DeleteNamespace(ctx, req)
	return err
}

// GetNamespaceInfo retrieves namespace information
func (c *VectorCluster) GetNamespaceInfo(nodeIdx int, namespace string) (*pb.NamespaceInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &pb.GetNamespaceInfoRequest{
		Namespace:    namespace,
		IncludeStats: true,
	}

	resp, err := c.clients[nodeIdx].GetNamespaceInfo(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp.Info, nil
}

// WaitForReplication waits for a vector to be replicated across the cluster
func (c *VectorCluster) WaitForReplication(namespace, id string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		allFound := true
		for i := 0; i < 3; i++ {
			_, err := c.GetVector(i, namespace, id)
			if err != nil {
				allFound = false
				break
			}
		}

		if allFound {
			return nil
		}

		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for replication")
}

// ListVectors lists all vectors in a namespace
func (c *VectorCluster) ListVectors(nodeIdx int, namespace string, pageSize int32) ([]*pb.VectorEntity, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &pb.ListVectorsRequest{
		Namespace:       namespace,
		PageSize:        pageSize,
		Consistency:     pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM,
		IncludeVectors:  true,
		IncludeMetadata: true,
	}

	stream, err := c.clients[nodeIdx].ListVectors(ctx, req)
	if err != nil {
		return nil, err
	}

	var vectors []*pb.VectorEntity
	for {
		resp, err := stream.Recv()
		if err != nil {
			// Check if it's a real error vs end of stream
			if err != io.EOF {
				return nil, err
			}
			break
		}
		vectors = append(vectors, resp.Vectors...)
		if resp.IsFinal {
			break
		}
	}

	return vectors, nil
}

// RandomVector generates a random vector of the specified dimension
func (c *VectorCluster) RandomVector(dimension int) []float32 {
	vec := make([]float32, dimension)
	for i := range vec {
		vec[i] = rand.Float32()
	}
	return vec
}

// NormalizedRandomVector generates a normalized random vector
func (c *VectorCluster) NormalizedRandomVector(dimension int) []float32 {
	vec := c.RandomVector(dimension)
	return normalizeVector(vec)
}

// CompareVectors compares two vectors for equality within epsilon
func (c *VectorCluster) CompareVectors(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}

	const epsilon = 1e-6
	for i := range a {
		if math.Abs(float64(a[i]-b[i])) > epsilon {
			return false
		}
	}
	return true
}

// Helper functions

// normalizeVector normalizes a vector to unit length
func normalizeVector(vec []float32) []float32 {
	var sum float32
	for _, v := range vec {
		sum += v * v
	}

	norm := float32(math.Sqrt(float64(sum)))
	if norm < 1e-10 {
		return vec
	}

	normalized := make([]float32, len(vec))
	for i, v := range vec {
		normalized[i] = v / norm
	}
	return normalized
}

// euclideanDistance calculates Euclidean distance between two vectors
func euclideanDistance(a, b []float32) float32 {
	var sum float32
	for i := range a {
		diff := a[i] - b[i]
		sum += diff * diff
	}
	return float32(math.Sqrt(float64(sum)))
}

// cosineSimilarity calculates cosine similarity between two vectors
func cosineSimilarity(a, b []float32) float32 {
	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	normA = float32(math.Sqrt(float64(normA)))
	normB = float32(math.Sqrt(float64(normB)))

	if normA < 1e-10 || normB < 1e-10 {
		return 0
	}

	return dotProduct / (normA * normB)
}

// bruteForceKNN performs exact k-NN search using brute force
func bruteForceKNN(query []float32, vectors [][]float32, k int) []int {
	type distanceID struct {
		id       int
		distance float32
	}

	distances := make([]distanceID, len(vectors))
	for i, vec := range vectors {
		distances[i] = distanceID{
			id:       i,
			distance: euclideanDistance(query, vec),
		}
	}

	// Simple bubble sort for small k
	for i := 0; i < k && i < len(distances); i++ {
		for j := i + 1; j < len(distances); j++ {
			if distances[j].distance < distances[i].distance {
				distances[i], distances[j] = distances[j], distances[i]
			}
		}
	}

	result := make([]int, min(k, len(distances)))
	for i := range result {
		result[i] = distances[i].id
	}

	return result
}

// calculateRecall calculates the recall between expected and actual results
func calculateRecall(expected, actual []int) float64 {
	if len(expected) == 0 {
		return 0
	}

	expectedSet := make(map[int]bool)
	for _, id := range expected {
		expectedSet[id] = true
	}

	matches := 0
	for _, id := range actual {
		if expectedSet[id] {
			matches++
		}
	}

	return float64(matches) / float64(len(expected))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
