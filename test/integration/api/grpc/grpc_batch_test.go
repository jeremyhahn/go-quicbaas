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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// TestGRPCBatchPutSuccess tests successful atomic batch put operations
func TestGRPCBatchPutSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Create batch put request with multiple entities
	timestamp := time.Now().UnixNano()
	requests := []*pb.PutRequest{
		{
			EntityType: "batch_user",
			Id:         uint64(timestamp),
			Data:       []byte(`{"name":"Alice","email":"alice@example.com","status":"active"}`),
		},
		{
			EntityType: "batch_profile",
			Id:         uint64(timestamp),
			Data:       []byte(`{"bio":"Software Engineer","avatar":"alice.png"}`),
		},
		{
			EntityType: "batch_settings",
			Id:         uint64(timestamp),
			Data:       []byte(`{"theme":"dark","notifications":true}`),
		},
	}

	batchReq := &pb.BatchPutRequest{
		Requests: requests,
	}

	// Execute batch put
	batchResp, err := client.BatchPut(ctx, batchReq)
	require.NoError(t, err)
	assert.NotNil(t, batchResp)
	assert.Equal(t, 3, len(batchResp.Responses))

	// Verify all entities were created
	for i, req := range requests {
		resp := batchResp.Responses[i]
		assert.NotZero(t, resp.Id)
		assert.Equal(t, req.Id, resp.Id)
	}

	// Verify data can be retrieved
	for _, req := range requests {
		getResp, err := client.Get(ctx, &pb.GetRequest{
			EntityType: req.EntityType,
			Id:         req.Id,
		})
		require.NoError(t, err)
		assert.Equal(t, req.Data, getResp.Data)
	}

	t.Logf("Successfully created batch of %d entities atomically", len(requests))
}

// TestGRPCBatchDeleteSuccess tests successful atomic batch delete operations
func TestGRPCBatchDeleteSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// First create entities to delete
	timestamp := time.Now().UnixNano()
	putRequests := []*pb.PutRequest{
		{
			EntityType: "del_user",
			Id:         uint64(timestamp),
			Data:       []byte(`{"name":"Bob","status":"active"}`),
		},
		{
			EntityType: "del_profile",
			Id:         uint64(timestamp),
			Data:       []byte(`{"bio":"Test user"}`),
		},
		{
			EntityType: "del_settings",
			Id:         uint64(timestamp),
			Data:       []byte(`{"theme":"light"}`),
		},
	}

	_, err := client.BatchPut(ctx, &pb.BatchPutRequest{Requests: putRequests})
	require.NoError(t, err)

	// Verify entities exist
	for _, req := range putRequests {
		getResp, err := client.Get(ctx, &pb.GetRequest{
			EntityType: req.EntityType,
			Id:         req.Id,
		})
		require.NoError(t, err)
		assert.NotNil(t, getResp.Data)
	}

	// Now delete them atomically
	deleteRequests := []*pb.DeleteRequest{
		{EntityType: "del_user", Id: uint64(timestamp)},
		{EntityType: "del_profile", Id: uint64(timestamp)},
		{EntityType: "del_settings", Id: uint64(timestamp)},
	}

	batchDelResp, err := client.BatchDelete(ctx, &pb.BatchDeleteRequest{
		Requests: deleteRequests,
	})
	require.NoError(t, err)
	assert.NotNil(t, batchDelResp)
	assert.Equal(t, 3, len(batchDelResp.Responses))

	// Verify all entities were deleted
	for _, resp := range batchDelResp.Responses {
		assert.True(t, resp.Deleted)
	}

	// Verify entities no longer exist
	for _, req := range deleteRequests {
		getResp, err := client.Get(ctx, &pb.GetRequest{
			EntityType: req.EntityType,
			Id:         req.Id,
		})
		// Should get error or empty response
		if err == nil {
			assert.Nil(t, getResp.Data)
		}
	}

	t.Logf("Successfully deleted batch of %d entities atomically", len(deleteRequests))
}

// TestGRPCBatchGetMultiple tests batch get (non-atomic read convenience)
func TestGRPCBatchGetMultiple(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Create test entities
	entityType := "batch_get_test"
	baseTimestamp := time.Now().UnixNano()

	putRequests := make([]*pb.PutRequest, 5)
	for i := 0; i < 5; i++ {
		putRequests[i] = &pb.PutRequest{
			EntityType: entityType,
			Id:         uint64(baseTimestamp + int64(i*1000)),
			Data:       []byte(fmt.Sprintf(`{"index":%d,"name":"Item %d"}`, i, i)),
		}
	}

	_, err := client.BatchPut(ctx, &pb.BatchPutRequest{Requests: putRequests})
	require.NoError(t, err)

	// Batch get them
	getRequests := make([]*pb.GetRequest, 5)
	for i := 0; i < 5; i++ {
		getRequests[i] = &pb.GetRequest{
			EntityType: entityType,
			Id:         uint64(baseTimestamp + int64(i*1000)),
		}
	}

	batchGetResp, err := client.BatchGet(ctx, &pb.BatchGetRequest{
		Requests: getRequests,
	})
	require.NoError(t, err)
	assert.Equal(t, 5, len(batchGetResp.Responses))

	// Verify all data matches
	for i, resp := range batchGetResp.Responses {
		assert.Equal(t, putRequests[i].Data, resp.Data)
		assert.Equal(t, putRequests[i].Id, resp.Id)
	}

	t.Logf("Successfully retrieved batch of %d entities", len(getRequests))
}

// TestGRPCBatchPutLargeSet tests batch put with larger number of operations
func TestGRPCBatchPutLargeSet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Create batch with 50 entities
	batchSize := 50
	baseTimestamp := time.Now().UnixNano()
	requests := make([]*pb.PutRequest, batchSize)

	for i := 0; i < batchSize; i++ {
		requests[i] = &pb.PutRequest{
			EntityType: "large_batch",
			Id:         uint64(baseTimestamp + int64(i*1000)),
			Data:       []byte(fmt.Sprintf(`{"index":%d,"value":"data_%d"}`, i, i)),
		}
	}

	// Execute batch put
	startTime := time.Now()
	batchResp, err := client.BatchPut(ctx, &pb.BatchPutRequest{Requests: requests})
	duration := time.Since(startTime)

	require.NoError(t, err)
	assert.NotNil(t, batchResp)
	assert.Equal(t, batchSize, len(batchResp.Responses))

	// Verify all succeeded
	for _, resp := range batchResp.Responses {
		assert.NotZero(t, resp.Id)
	}

	t.Logf("Successfully created %d entities in batch (took %v)", batchSize, duration)

	// Sample verify a few
	for i := 0; i < 5; i++ {
		idx := i * 10 // Check every 10th item
		getResp, err := client.Get(ctx, &pb.GetRequest{
			EntityType: "large_batch",
			Id:         uint64(baseTimestamp + int64(idx*1000)),
		})
		require.NoError(t, err)
		assert.Equal(t, requests[idx].Data, getResp.Data)
	}
}

// TestGRPCBatchPutUnrelatedEntities tests batch operations on unrelated entity types
func TestGRPCBatchPutUnrelatedEntities(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Create batch with completely unrelated entity types
	timestamp := time.Now().UnixNano()
	requests := []*pb.PutRequest{
		{
			EntityType: "product",
			Id:         uint64(timestamp),
			Data:       []byte(`{"name":"Laptop","price":999.99,"sku":"PROD-001"}`),
		},
		{
			EntityType: "customer",
			Id:         uint64(timestamp + 1000),
			Data:       []byte(`{"name":"John Doe","email":"john@example.com"}`),
		},
		{
			EntityType: "invoice",
			Id:         uint64(timestamp + 2000),
			Data:       []byte(`{"total":1234.56,"status":"paid"}`),
		},
		{
			EntityType: "log_entry",
			Id:         uint64(timestamp + 3000),
			Data:       []byte(`{"level":"info","message":"System started"}`),
		},
	}

	batchResp, err := client.BatchPut(ctx, &pb.BatchPutRequest{Requests: requests})
	require.NoError(t, err)
	assert.Equal(t, 4, len(batchResp.Responses))

	// Verify all different entity types were created atomically
	for i, req := range requests {
		assert.NotZero(t, batchResp.Responses[i].Id)

		// Verify retrieval
		getResp, err := client.Get(ctx, &pb.GetRequest{
			EntityType: req.EntityType,
			Id:         req.Id,
		})
		require.NoError(t, err)
		assert.Equal(t, req.Data, getResp.Data)
	}

	t.Logf("Successfully created batch of %d unrelated entity types atomically", len(requests))
}

// TestGRPCBatchPutUpdateExisting tests batch put that updates existing entities
func TestGRPCBatchPutUpdateExisting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	timestamp := time.Now().UnixNano()
	entityType := "update_test"

	// Create initial entities
	initialRequests := []*pb.PutRequest{
		{
			EntityType: entityType,
			Id:         uint64(timestamp),
			Data:       []byte(`{"version":1,"status":"draft"}`),
		},
		{
			EntityType: entityType,
			Id:         uint64(timestamp + 1000),
			Data:       []byte(`{"version":1,"status":"draft"}`),
		},
	}

	_, err := client.BatchPut(ctx, &pb.BatchPutRequest{Requests: initialRequests})
	require.NoError(t, err)

	// Update them atomically with new data
	updateRequests := []*pb.PutRequest{
		{
			EntityType: entityType,
			Id:         uint64(timestamp),
			Data:       []byte(`{"version":2,"status":"published"}`),
		},
		{
			EntityType: entityType,
			Id:         uint64(timestamp + 1000),
			Data:       []byte(`{"version":2,"status":"published"}`),
		},
	}

	updateResp, err := client.BatchPut(ctx, &pb.BatchPutRequest{Requests: updateRequests})
	require.NoError(t, err)
	assert.Equal(t, 2, len(updateResp.Responses))

	// Verify updates
	for _, req := range updateRequests {
		getResp, err := client.Get(ctx, &pb.GetRequest{
			EntityType: req.EntityType,
			Id:         req.Id,
		})
		require.NoError(t, err)
		assert.Equal(t, req.Data, getResp.Data)
	}

	t.Logf("Successfully updated %d existing entities atomically", len(updateRequests))
}

// TestGRPCBatchEmptyRequest tests error handling for empty batch
func TestGRPCBatchEmptyRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Try empty batch put
	_, err := client.BatchPut(ctx, &pb.BatchPutRequest{
		Requests: []*pb.PutRequest{},
	})
	assert.Error(t, err)

	// Try empty batch delete
	_, err = client.BatchDelete(ctx, &pb.BatchDeleteRequest{
		Requests: []*pb.DeleteRequest{},
	})
	assert.Error(t, err)

	t.Log("Empty batch requests correctly rejected")
}

// TestGRPCBatchMixedOperationsSequence tests sequential batch operations
func TestGRPCBatchMixedOperationsSequence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	timestamp := time.Now().UnixNano()

	// Step 1: Create initial entities
	createRequests := []*pb.PutRequest{
		{
			EntityType: "seq_test",
			Id:         uint64(timestamp),
			Data:       []byte(`{"step":1}`),
		},
		{
			EntityType: "seq_test",
			Id:         uint64(timestamp + 1000),
			Data:       []byte(`{"step":1}`),
		},
		{
			EntityType: "seq_test",
			Id:         uint64(timestamp + 2000),
			Data:       []byte(`{"step":1}`),
		},
	}

	_, err := client.BatchPut(ctx, &pb.BatchPutRequest{Requests: createRequests})
	require.NoError(t, err)

	// Step 2: Update some, delete others
	updateRequests := []*pb.PutRequest{
		{
			EntityType: "seq_test",
			Id:         uint64(timestamp),
			Data:       []byte(`{"step":2,"updated":true}`),
		},
		{
			EntityType: "seq_test",
			Id:         uint64(timestamp + 2000),
			Data:       []byte(`{"step":2,"updated":true}`),
		},
	}

	_, err = client.BatchPut(ctx, &pb.BatchPutRequest{Requests: updateRequests})
	require.NoError(t, err)

	deleteRequests := []*pb.DeleteRequest{
		{EntityType: "seq_test", Id: uint64(timestamp + 1000)},
	}

	_, err = client.BatchDelete(ctx, &pb.BatchDeleteRequest{Requests: deleteRequests})
	require.NoError(t, err)

	// Step 3: Verify final state
	// First should exist with updated data
	getResp1, err := client.Get(ctx, &pb.GetRequest{
		EntityType: "seq_test",
		Id:         uint64(timestamp),
	})
	require.NoError(t, err)
	assert.Contains(t, string(getResp1.Data), "\"step\":2")

	// Second should be deleted
	getResp2, err := client.Get(ctx, &pb.GetRequest{
		EntityType: "seq_test",
		Id:         uint64(timestamp + 1000),
	})
	if err == nil {
		assert.Nil(t, getResp2.Data)
	}

	// Third should exist with updated data
	getResp3, err := client.Get(ctx, &pb.GetRequest{
		EntityType: "seq_test",
		Id:         uint64(timestamp + 2000),
	})
	require.NoError(t, err)
	assert.Contains(t, string(getResp3.Data), "\"step\":2")

	t.Log("Successfully executed sequence of batch operations")
}
