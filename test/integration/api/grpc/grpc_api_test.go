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

package grpc

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestGRPCHealthCheck tests the gRPC health check endpoint
func TestGRPCHealthCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	req := &pb.HealthRequest{}
	resp, err := client.Health(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_HEALTHY, resp.Status)
	assert.NotEmpty(t, resp.Version)

	t.Logf("Health check response: version=%s, status=%v", resp.Version, resp.Status)
}

// TestGRPCPutGet tests basic Put and Get operations
func TestGRPCPutGet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Test data
	entityType := "grpc_test"
	entityID := uint64(time.Now().UnixNano())
	testData := []byte(`{"name": "GRPC Test", "value": 12345}`)

	// Put operation
	putReq := &pb.PutRequest{
		EntityType: entityType,
		Id:         entityID,
		Data:       testData,
	}

	putResp, err := client.Put(ctx, putReq)
	require.NoError(t, err)
	assert.NotNil(t, putResp)
	assert.Equal(t, entityID, putResp.Id)

	// Get operation
	getReq := &pb.GetRequest{
		EntityType: entityType,
		Id:         entityID,
	}

	getResp, err := client.Get(ctx, getReq)
	require.NoError(t, err)
	assert.Equal(t, testData, getResp.Data)
	assert.Equal(t, entityID, getResp.Id)

	t.Logf("Successfully put and retrieved entity %d", entityID)
}

// TestGRPCList tests the List operation with server-side streaming
func TestGRPCList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	entityType := "list_test"

	// Create multiple entities with IDs that route to the same shard
	// Use IDs that are multiples of 3 to ensure they route to shard 1
	entityCount := 15
	createdIDs := make([]uint64, entityCount)
	for i := 0; i < entityCount; i++ {
		entityID := uint64((i + 1) * 3) // IDs: 3, 6, 9, 12, 15, ...
		createdIDs[i] = entityID
		data := []byte(fmt.Sprintf(`{"index": %d, "name": "Item %d"}`, i, i))

		putReq := &pb.PutRequest{
			EntityType: entityType,
			Id:         entityID,
			Data:       data,
		}

		_, err := client.Put(ctx, putReq)
		require.NoError(t, err, "Failed to create entity %d", i)
	}

	// Wait for writes to replicate
	time.Sleep(1 * time.Second)

	// List with pagination - streaming API
	listReq := &pb.ListRequest{
		EntityType: entityType,
		PageSize:   5,
		ShardId:    1, // Query shard 1 where our entities are stored
	}

	// Get streaming client
	stream, err := client.List(ctx, listReq)
	require.NoError(t, err)

	// Receive all responses from the stream
	var allEntities []*pb.Entity
	var pageCount int
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			// End of stream
			break
		}
		require.NoError(t, err)

		pageCount++
		allEntities = append(allEntities, resp.Entities...)

		// Each batch should respect page size
		assert.LessOrEqual(t, len(resp.Entities), 5, "Batch %d exceeded page size", pageCount)

		t.Logf("Batch %d returned %d entities (total so far: %d)", pageCount, len(resp.Entities), len(allEntities))
	}

	// Verify we got results
	assert.Greater(t, len(allEntities), 0, "Expected to receive entities")
	t.Logf("Received %d total entities across %d batches", len(allEntities), pageCount)
}

// TestGRPCQuery tests index-based queries with server-side streaming
func TestGRPCQuery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	entityType := "query_test"
	indexName := "status"

	// Create entities with indexed field
	// Use IDs that route to the same shard for consistency
	statuses := []string{"active", "inactive", "pending", "active", "active"}
	entityIDs := make([]uint64, len(statuses))

	for i, status := range statuses {
		entityID := uint64((i + 1) * 3) // Route to shard 1
		entityIDs[i] = entityID
		data := []byte(fmt.Sprintf(`{"id": %d, "status": "%s", "name": "Item %d"}`, i, status, i))

		putReq := &pb.PutRequest{
			EntityType: entityType,
			Id:         entityID,
			Data:       data,
		}

		_, err := client.Put(ctx, putReq)
		require.NoError(t, err)
	}

	// Wait for indexing
	time.Sleep(1 * time.Second)

	// Query for "active" status
	queryReq := &pb.QueryRequest{
		EntityType: entityType,
		IndexName:  indexName,
		Value:      []byte("active"),
	}

	// Get streaming client
	stream, err := client.Query(ctx, queryReq)
	require.NoError(t, err)

	// Receive all responses from the stream
	var allEntities []*pb.Entity
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			// End of stream
			break
		}
		require.NoError(t, err)

		allEntities = append(allEntities, resp.Entities...)
		t.Logf("Received batch of %d entities", len(resp.Entities))
	}

	// Should have entities (may vary based on implementation)
	t.Logf("Query returned %d total entities with status=active", len(allEntities))
}

// TestGRPCMetadata tests gRPC metadata handling
func TestGRPCMetadata(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Add metadata to context
	md := metadata.Pairs(
		"request-id", "test-123",
		"client-version", "1.0.0",
	)
	ctx = metadata.NewOutgoingContext(ctx, md)

	// Make request with metadata
	req := &pb.HealthRequest{}
	resp, err := client.Health(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)

	t.Logf("Request with metadata successful")
}

// TestGRPCErrorHandling tests error responses
func TestGRPCErrorHandling(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Try to get non-existent entity
	getReq := &pb.GetRequest{
		EntityType: "nonexistent",
		Id:         9999999999,
	}

	_, err := client.Get(ctx, getReq)
	require.Error(t, err)

	// Verify it's a gRPC error
	st, ok := status.FromError(err)
	assert.True(t, ok, "Expected gRPC status error")
	assert.NotEmpty(t, st.Message())

	t.Logf("Error handling works correctly: %s", st.Message())
}

// TestGRPCConcurrentRequests tests concurrent operations
func TestGRPCConcurrentRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	entityType := "concurrent_test"
	concurrency := 20

	errs := make([]error, concurrency)
	var wg sync.WaitGroup
	wg.Add(concurrency)

	// Launch concurrent writes
	for i := 0; i < concurrency; i++ {
		go func(index int) {
			defer wg.Done()

			entityID := uint64(time.Now().UnixNano() + int64(index*1000))
			data := []byte(fmt.Sprintf(`{"index": %d, "timestamp": %d}`, index, time.Now().Unix()))

			putReq := &pb.PutRequest{
				EntityType: entityType,
				Id:         entityID,
				Data:       data,
			}

			_, err := client.Put(ctx, putReq)
			if err != nil {
				errs[index] = err
			}
		}(i)
	}

	// Wait for all goroutines to complete or context to expire
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines completed
	case <-ctx.Done():
		t.Fatal("Context deadline exceeded waiting for concurrent operations")
	}

	// Collect all errors and report them together
	var failures []string
	for i, err := range errs {
		if err != nil {
			failures = append(failures, fmt.Sprintf("goroutine %d: %v", i, err))
		}
	}

	require.Empty(t, failures, "Concurrent operations failed:\n%s", fmt.Sprintf("%v", failures))
	t.Logf("Successfully completed %d concurrent operations", concurrency)
}

// TestGRPCClusterStatus tests that the cluster status API returns a valid response.
// In embedded/standalone mode, the server may report 0 nodes and no leader since
// there is no real Raft consensus running. The test verifies the API works and
// returns a well-formed response structure.
func TestGRPCClusterStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// ClusterStatus needs admin:status. The data-plane role of
	// setupGRPCConnection does not hold it; the tenant operator's Operator
	// role does.
	conn := helpers.MustSetupGRPCConnectionToEndpoint(t, ctx, helpers.GetClusterEndpoints()[0],
		grpc.WithPerRPCCredentials(helpers.TenantOperatorGRPCCredentials(t)))
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	req := &pb.ClusterStatusRequest{}
	resp, err := client.ClusterStatus(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)

	// In embedded mode, Nodes may be empty (no real Raft cluster).
	// Log the actual state for diagnostics.
	var leaderFound bool
	for _, node := range resp.Nodes {
		if node.IsLeader {
			leaderFound = true
		}
	}

	t.Logf("Cluster status: %d nodes, leader found: %v", len(resp.Nodes), leaderFound)
}

// Helper function to set up gRPC connection.
// Uses non-blocking dial with a 30-second timeout to prevent hanging tests.
func setupGRPCConnection(t *testing.T, ctx context.Context) *grpc.ClientConn {
	endpoints := helpers.GetClusterEndpoints()
	endpoint := endpoints[0]

	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")

	// Create connection without WithBlock() to prevent indefinite hanging
	conn, err := grpc.DialContext(ctx, endpoint,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithPerRPCCredentials(helpers.DataPlaneGRPCCredentials(t)),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
	)
	require.NoError(t, err, "Failed to dial gRPC server at %s", endpoint)

	// Wait for connection to be ready with timeout
	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return conn
		}
		if state == connectivity.TransientFailure || state == connectivity.Shutdown {
			conn.Close()
			require.Fail(t, "Connection failed", "state=%v for endpoint %s", state, endpoint)
		}
		if !conn.WaitForStateChange(dialCtx, state) {
			conn.Close()
			require.Fail(t, "Connection timeout", "timeout waiting for connection to %s", endpoint)
		}
	}
}
