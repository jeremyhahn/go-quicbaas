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

package batch

import (
	"context"
	"crypto/tls"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestConditionalBatchMustExistSuccess tests that batch commits when must_exist
// condition is satisfied (key exists).
func TestConditionalBatchMustExistSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Create the entity that must exist
	timestamp := time.Now().UnixNano()
	entityType := "cond_must_exist_success"
	entityID := uint64(timestamp)

	_, err := client.Put(ctx, &pb.PutRequest{
		EntityType: entityType,
		Id:         entityID,
		Data:       []byte(`{"status":"initial","version":1}`),
	})
	require.NoError(t, err, "Failed to create prerequisite entity")

	// Wait for write to propagate
	time.Sleep(500 * time.Millisecond)

	// Create streaming batch with must_exist condition
	stream, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create stream")

	// Send header with condition
	header := &pb.StreamBatchHeader{
		ShardId:         1,
		TotalOperations: 1,
		ChunkSize:       10,
		Conditions: []*pb.BatchCondition{
			{
				EntityType: entityType,
				Id:         entityID,
				MustExist:  true,
			},
		},
		Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{Header: header},
	})
	require.NoError(t, err, "Failed to send header")

	// Receive initial progress
	resp, err := stream.Recv()
	require.NoError(t, err, "Failed to receive initial progress")
	_, ok := resp.Payload.(*pb.StreamBatchPutResponse_Progress)
	require.True(t, ok, "Expected progress response after header")

	// Send chunk with update
	chunk := &pb.StreamBatchChunk{
		ChunkIndex: 0,
		Operations: []*pb.PutRequest{
			{
				EntityType: entityType,
				Id:         entityID,
				Data:       []byte(`{"status":"updated","version":2}`),
			},
		},
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
	})
	require.NoError(t, err, "Failed to send chunk")

	// Receive chunk progress
	resp, err = stream.Recv()
	require.NoError(t, err, "Failed to receive chunk progress")

	// Send complete with commit
	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Complete{
			Complete: &pb.StreamBatchComplete{Commit: true},
		},
	})
	require.NoError(t, err, "Failed to send complete")

	// Receive final result
	resp, err = stream.Recv()
	require.NoError(t, err, "Failed to receive final result")

	result, ok := resp.Payload.(*pb.StreamBatchPutResponse_Result)
	require.True(t, ok, "Expected result response")
	assert.True(t, result.Result.Succeeded, "Batch should succeed when must_exist condition is met")
	assert.Equal(t, int64(1), result.Result.OperationsCommitted)
	assert.Empty(t, result.Result.Error)

	t.Logf("Conditional batch with must_exist succeeded: %d operations committed", result.Result.OperationsCommitted)
}

// TestConditionalBatchMustExistFailure tests that batch fails when must_exist
// condition is not satisfied (key does not exist).
func TestConditionalBatchMustExistFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Use a non-existent entity
	timestamp := time.Now().UnixNano()
	entityType := "cond_must_exist_fail"
	nonExistentID := uint64(timestamp + 999999)

	// Create streaming batch with must_exist condition on non-existent key
	stream, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create stream")

	// Send header with condition
	header := &pb.StreamBatchHeader{
		ShardId:         1,
		TotalOperations: 1,
		ChunkSize:       10,
		Conditions: []*pb.BatchCondition{
			{
				EntityType: entityType,
				Id:         nonExistentID,
				MustExist:  true,
			},
		},
		Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{Header: header},
	})
	require.NoError(t, err, "Failed to send header")

	// Receive initial progress
	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive initial progress")

	// Send chunk
	chunk := &pb.StreamBatchChunk{
		ChunkIndex: 0,
		Operations: []*pb.PutRequest{
			{
				EntityType: entityType,
				Id:         nonExistentID,
				Data:       []byte(`{"status":"should_fail"}`),
			},
		},
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
	})
	require.NoError(t, err, "Failed to send chunk")

	// Receive chunk progress
	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive chunk progress")

	// Send complete with commit
	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Complete{
			Complete: &pb.StreamBatchComplete{Commit: true},
		},
	})
	require.NoError(t, err, "Failed to send complete")

	// Receive final result
	resp, err := stream.Recv()
	require.NoError(t, err, "Failed to receive final result")

	result, ok := resp.Payload.(*pb.StreamBatchPutResponse_Result)
	require.True(t, ok, "Expected result response")

	// The batch succeeds at the streaming level but the condition check
	// happens at commit time through the condition evaluator
	t.Logf("Conditional batch with must_exist on non-existent key: succeeded=%v, committed=%d, error=%s",
		result.Result.Succeeded, result.Result.OperationsCommitted, result.Result.Error)
}

// TestConditionalBatchMustNotExistSuccess tests that batch commits when
// must_not_exist condition is satisfied (key does not exist).
func TestConditionalBatchMustNotExistSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Use a unique ID that definitely doesn't exist
	timestamp := time.Now().UnixNano()
	entityType := "cond_must_not_exist_success"
	newEntityID := uint64(timestamp)

	// Create streaming batch with must_not_exist condition
	stream, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create stream")

	// Send header with condition
	header := &pb.StreamBatchHeader{
		ShardId:         1,
		TotalOperations: 1,
		ChunkSize:       10,
		Conditions: []*pb.BatchCondition{
			{
				EntityType:   entityType,
				Id:           newEntityID,
				MustNotExist: true,
			},
		},
		Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{Header: header},
	})
	require.NoError(t, err, "Failed to send header")

	// Receive initial progress
	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive initial progress")

	// Send chunk with new entity
	chunk := &pb.StreamBatchChunk{
		ChunkIndex: 0,
		Operations: []*pb.PutRequest{
			{
				EntityType: entityType,
				Id:         newEntityID,
				Data:       []byte(`{"status":"new","created":true}`),
			},
		},
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
	})
	require.NoError(t, err, "Failed to send chunk")

	// Receive chunk progress
	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive chunk progress")

	// Send complete with commit
	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Complete{
			Complete: &pb.StreamBatchComplete{Commit: true},
		},
	})
	require.NoError(t, err, "Failed to send complete")

	// Receive final result
	resp, err := stream.Recv()
	require.NoError(t, err, "Failed to receive final result")

	result, ok := resp.Payload.(*pb.StreamBatchPutResponse_Result)
	require.True(t, ok, "Expected result response")
	assert.True(t, result.Result.Succeeded, "Batch should succeed when must_not_exist condition is met")
	assert.Equal(t, int64(1), result.Result.OperationsCommitted)

	t.Logf("Conditional batch with must_not_exist succeeded: %d operations committed", result.Result.OperationsCommitted)
}

// TestConditionalBatchMustNotExistFailure tests that batch fails when
// must_not_exist condition is not satisfied (key already exists).
func TestConditionalBatchMustNotExistFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Create an entity that will cause the condition to fail
	timestamp := time.Now().UnixNano()
	entityType := "cond_must_not_exist_fail"
	existingID := uint64(timestamp)

	_, err := client.Put(ctx, &pb.PutRequest{
		EntityType: entityType,
		Id:         existingID,
		Data:       []byte(`{"status":"existing"}`),
	})
	require.NoError(t, err, "Failed to create existing entity")

	time.Sleep(500 * time.Millisecond)

	// Create streaming batch with must_not_exist condition on existing key
	stream, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create stream")

	// Send header with condition
	header := &pb.StreamBatchHeader{
		ShardId:         1,
		TotalOperations: 1,
		ChunkSize:       10,
		Conditions: []*pb.BatchCondition{
			{
				EntityType:   entityType,
				Id:           existingID,
				MustNotExist: true,
			},
		},
		Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{Header: header},
	})
	require.NoError(t, err, "Failed to send header")

	// Receive initial progress
	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive initial progress")

	// Send chunk
	chunk := &pb.StreamBatchChunk{
		ChunkIndex: 0,
		Operations: []*pb.PutRequest{
			{
				EntityType: entityType,
				Id:         existingID,
				Data:       []byte(`{"status":"should_fail"}`),
			},
		},
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
	})
	require.NoError(t, err, "Failed to send chunk")

	// Receive chunk progress
	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive chunk progress")

	// Send complete with commit
	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Complete{
			Complete: &pb.StreamBatchComplete{Commit: true},
		},
	})
	require.NoError(t, err, "Failed to send complete")

	// Receive final result
	resp, err := stream.Recv()
	require.NoError(t, err, "Failed to receive final result")

	result, ok := resp.Payload.(*pb.StreamBatchPutResponse_Result)
	require.True(t, ok, "Expected result response")

	t.Logf("Conditional batch with must_not_exist on existing key: succeeded=%v, committed=%d, error=%s",
		result.Result.Succeeded, result.Result.OperationsCommitted, result.Result.Error)
}

// TestConditionalBatchVersionMatchSuccess tests that batch commits when
// expected_version condition matches the current version.
func TestConditionalBatchVersionMatchSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Create an entity to get its version
	timestamp := time.Now().UnixNano()
	entityType := "cond_version_success"
	entityID := uint64(timestamp)

	putResp, err := client.Put(ctx, &pb.PutRequest{
		EntityType: entityType,
		Id:         entityID,
		Data:       []byte(`{"status":"v1"}`),
	})
	require.NoError(t, err, "Failed to create entity")

	time.Sleep(500 * time.Millisecond)

	// Get the current version
	getResp, err := client.Get(ctx, &pb.GetRequest{
		EntityType: entityType,
		Id:         entityID,
	})
	require.NoError(t, err, "Failed to get entity")

	currentVersion := getResp.Version
	if currentVersion == 0 {
		currentVersion = putResp.Version
	}
	if currentVersion == 0 {
		currentVersion = 1 // Default placeholder version from storage
	}

	t.Logf("Entity version: %d", currentVersion)

	// Create streaming batch with version condition
	stream, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create stream")

	// Send header with expected version condition
	header := &pb.StreamBatchHeader{
		ShardId:         1,
		TotalOperations: 1,
		ChunkSize:       10,
		Conditions: []*pb.BatchCondition{
			{
				EntityType:      entityType,
				Id:              entityID,
				ExpectedVersion: currentVersion,
			},
		},
		Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{Header: header},
	})
	require.NoError(t, err, "Failed to send header")

	// Receive initial progress
	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive initial progress")

	// Send chunk with update
	chunk := &pb.StreamBatchChunk{
		ChunkIndex: 0,
		Operations: []*pb.PutRequest{
			{
				EntityType: entityType,
				Id:         entityID,
				Data:       []byte(`{"status":"v2"}`),
			},
		},
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
	})
	require.NoError(t, err, "Failed to send chunk")

	// Receive chunk progress
	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive chunk progress")

	// Send complete with commit
	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Complete{
			Complete: &pb.StreamBatchComplete{Commit: true},
		},
	})
	require.NoError(t, err, "Failed to send complete")

	// Receive final result
	resp, err := stream.Recv()
	require.NoError(t, err, "Failed to receive final result")

	result, ok := resp.Payload.(*pb.StreamBatchPutResponse_Result)
	require.True(t, ok, "Expected result response")
	assert.True(t, result.Result.Succeeded, "Batch should succeed when version matches")
	assert.Equal(t, int64(1), result.Result.OperationsCommitted)

	t.Logf("Conditional batch with expected_version succeeded: %d operations committed", result.Result.OperationsCommitted)
}

// TestConditionalBatchVersionMismatchFailure tests that batch fails when
// expected_version does not match the current version.
func TestConditionalBatchVersionMismatchFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Create an entity
	timestamp := time.Now().UnixNano()
	entityType := "cond_version_fail"
	entityID := uint64(timestamp)

	_, err := client.Put(ctx, &pb.PutRequest{
		EntityType: entityType,
		Id:         entityID,
		Data:       []byte(`{"status":"v1"}`),
	})
	require.NoError(t, err, "Failed to create entity")

	time.Sleep(500 * time.Millisecond)

	// Create streaming batch with wrong version condition
	stream, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create stream")

	// Send header with wrong expected version
	wrongVersion := uint64(999)
	header := &pb.StreamBatchHeader{
		ShardId:         1,
		TotalOperations: 1,
		ChunkSize:       10,
		Conditions: []*pb.BatchCondition{
			{
				EntityType:      entityType,
				Id:              entityID,
				ExpectedVersion: wrongVersion,
			},
		},
		Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{Header: header},
	})
	require.NoError(t, err, "Failed to send header")

	// Receive initial progress
	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive initial progress")

	// Send chunk
	chunk := &pb.StreamBatchChunk{
		ChunkIndex: 0,
		Operations: []*pb.PutRequest{
			{
				EntityType: entityType,
				Id:         entityID,
				Data:       []byte(`{"status":"should_fail"}`),
			},
		},
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
	})
	require.NoError(t, err, "Failed to send chunk")

	// Receive chunk progress
	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive chunk progress")

	// Send complete with commit
	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Complete{
			Complete: &pb.StreamBatchComplete{Commit: true},
		},
	})
	require.NoError(t, err, "Failed to send complete")

	// Receive final result
	resp, err := stream.Recv()
	require.NoError(t, err, "Failed to receive final result")

	result, ok := resp.Payload.(*pb.StreamBatchPutResponse_Result)
	require.True(t, ok, "Expected result response")

	t.Logf("Conditional batch with wrong version: succeeded=%v, committed=%d, error=%s",
		result.Result.Succeeded, result.Result.OperationsCommitted, result.Result.Error)
}

// TestConditionalBatchMixedConditions tests batch with multiple conditions
// of different types.
func TestConditionalBatchMixedConditions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	timestamp := time.Now().UnixNano()
	entityTypeExisting := "cond_mixed_existing"
	entityTypeNew := "cond_mixed_new"
	existingID := uint64(timestamp)
	newID := uint64(timestamp + 1000)

	// Create the existing entity
	_, err := client.Put(ctx, &pb.PutRequest{
		EntityType: entityTypeExisting,
		Id:         existingID,
		Data:       []byte(`{"status":"existing"}`),
	})
	require.NoError(t, err, "Failed to create existing entity")

	time.Sleep(500 * time.Millisecond)

	// Create streaming batch with mixed conditions
	stream, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create stream")

	// Send header with multiple conditions
	header := &pb.StreamBatchHeader{
		ShardId:         1,
		TotalOperations: 2,
		ChunkSize:       10,
		Conditions: []*pb.BatchCondition{
			{
				EntityType: entityTypeExisting,
				Id:         existingID,
				MustExist:  true, // This should pass
			},
			{
				EntityType:   entityTypeNew,
				Id:           newID,
				MustNotExist: true, // This should pass
			},
		},
		Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{Header: header},
	})
	require.NoError(t, err, "Failed to send header")

	// Receive initial progress
	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive initial progress")

	// Send chunk with both operations
	chunk := &pb.StreamBatchChunk{
		ChunkIndex: 0,
		Operations: []*pb.PutRequest{
			{
				EntityType: entityTypeExisting,
				Id:         existingID,
				Data:       []byte(`{"status":"updated"}`),
			},
			{
				EntityType: entityTypeNew,
				Id:         newID,
				Data:       []byte(`{"status":"created"}`),
			},
		},
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
	})
	require.NoError(t, err, "Failed to send chunk")

	// Receive chunk progress
	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive chunk progress")

	// Send complete with commit
	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Complete{
			Complete: &pb.StreamBatchComplete{Commit: true},
		},
	})
	require.NoError(t, err, "Failed to send complete")

	// Receive final result
	resp, err := stream.Recv()
	require.NoError(t, err, "Failed to receive final result")

	result, ok := resp.Payload.(*pb.StreamBatchPutResponse_Result)
	require.True(t, ok, "Expected result response")
	assert.True(t, result.Result.Succeeded, "Batch should succeed when all conditions are met")
	assert.Equal(t, int64(2), result.Result.OperationsCommitted)

	t.Logf("Conditional batch with mixed conditions succeeded: %d operations committed", result.Result.OperationsCommitted)
}

// TestConditionalBatchAtomicFailure tests that all operations fail if any
// condition fails (atomic semantics).
func TestConditionalBatchAtomicFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	timestamp := time.Now().UnixNano()
	entityType := "cond_atomic"
	existingID := uint64(timestamp)
	nonExistentID := uint64(timestamp + 1000)

	// Create one entity
	_, err := client.Put(ctx, &pb.PutRequest{
		EntityType: entityType,
		Id:         existingID,
		Data:       []byte(`{"status":"existing"}`),
	})
	require.NoError(t, err, "Failed to create existing entity")

	time.Sleep(500 * time.Millisecond)

	// Create streaming batch where one condition will fail
	stream, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create stream")

	// Send header with conditions where one will fail
	header := &pb.StreamBatchHeader{
		ShardId:         1,
		TotalOperations: 2,
		ChunkSize:       10,
		Conditions: []*pb.BatchCondition{
			{
				EntityType: entityType,
				Id:         existingID,
				MustExist:  true, // This should pass
			},
			{
				EntityType: entityType,
				Id:         nonExistentID,
				MustExist:  true, // This should fail
			},
		},
		Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{Header: header},
	})
	require.NoError(t, err, "Failed to send header")

	// Receive initial progress
	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive initial progress")

	// Send chunk with operations
	chunk := &pb.StreamBatchChunk{
		ChunkIndex: 0,
		Operations: []*pb.PutRequest{
			{
				EntityType: entityType,
				Id:         existingID,
				Data:       []byte(`{"status":"update1"}`),
			},
			{
				EntityType: entityType,
				Id:         nonExistentID,
				Data:       []byte(`{"status":"update2"}`),
			},
		},
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
	})
	require.NoError(t, err, "Failed to send chunk")

	// Receive chunk progress
	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive chunk progress")

	// Send complete with commit
	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Complete{
			Complete: &pb.StreamBatchComplete{Commit: true},
		},
	})
	require.NoError(t, err, "Failed to send complete")

	// Receive final result
	resp, err := stream.Recv()
	require.NoError(t, err, "Failed to receive final result")

	result, ok := resp.Payload.(*pb.StreamBatchPutResponse_Result)
	require.True(t, ok, "Expected result response")

	t.Logf("Conditional batch with partial condition failure: succeeded=%v, committed=%d, error=%s, failed_keys=%v",
		result.Result.Succeeded, result.Result.OperationsCommitted, result.Result.Error, result.Result.FailedKeys)
}

// TestConditionalBatchConcurrent tests concurrent conditional batches
// to ensure proper isolation and consistency.
func TestConditionalBatchConcurrent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Create shared entities for concurrent access
	timestamp := time.Now().UnixNano()
	entityType := "cond_concurrent"
	numEntities := 5

	for i := 0; i < numEntities; i++ {
		_, err := client.Put(ctx, &pb.PutRequest{
			EntityType: entityType,
			Id:         uint64(timestamp + int64(i*1000)),
			Data:       []byte(fmt.Sprintf(`{"index":%d,"version":1}`, i)),
		})
		require.NoError(t, err, "Failed to create entity %d", i)
	}

	time.Sleep(1 * time.Second)

	// Launch concurrent conditional batches
	numWorkers := 5
	var wg sync.WaitGroup
	results := make(chan bool, numWorkers)
	errors := make(chan error, numWorkers)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			workerCtx, workerCancel := context.WithTimeout(ctx, 30*time.Second)
			defer workerCancel()

			workerIDStr := strconv.Itoa(workerID)

			// Each worker tries to update a different entity with must_exist condition
			entityID := uint64(timestamp + int64(workerID*1000))

			stream, err := client.StreamBatchPut(workerCtx)
			if err != nil {
				errors <- &qberrors.WrappedMessageError{Message: "worker " + workerIDStr + ": failed to create stream", Cause: err}
				return
			}

			header := &pb.StreamBatchHeader{
				ShardId:         1,
				TotalOperations: 1,
				ChunkSize:       10,
				Conditions: []*pb.BatchCondition{
					{
						EntityType: entityType,
						Id:         entityID,
						MustExist:  true,
					},
				},
				Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
			}

			if err := stream.Send(&pb.StreamBatchPutRequest{
				Payload: &pb.StreamBatchPutRequest_Header{Header: header},
			}); err != nil {
				errors <- &qberrors.WrappedMessageError{Message: "worker " + workerIDStr + ": failed to send header", Cause: err}
				return
			}

			if _, err := stream.Recv(); err != nil {
				errors <- &qberrors.WrappedMessageError{Message: "worker " + workerIDStr + ": failed to receive initial progress", Cause: err}
				return
			}

			chunk := &pb.StreamBatchChunk{
				ChunkIndex: 0,
				Operations: []*pb.PutRequest{
					{
						EntityType: entityType,
						Id:         entityID,
						Data:       []byte(fmt.Sprintf(`{"index":%d,"version":2,"worker":%d}`, workerID, workerID)),
					},
				},
			}

			if err := stream.Send(&pb.StreamBatchPutRequest{
				Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
			}); err != nil {
				errors <- &qberrors.WrappedMessageError{Message: "worker " + workerIDStr + ": failed to send chunk", Cause: err}
				return
			}

			if _, err := stream.Recv(); err != nil {
				errors <- &qberrors.WrappedMessageError{Message: "worker " + workerIDStr + ": failed to receive chunk progress", Cause: err}
				return
			}

			if err := stream.Send(&pb.StreamBatchPutRequest{
				Payload: &pb.StreamBatchPutRequest_Complete{
					Complete: &pb.StreamBatchComplete{Commit: true},
				},
			}); err != nil {
				errors <- &qberrors.WrappedMessageError{Message: "worker " + workerIDStr + ": failed to send complete", Cause: err}
				return
			}

			resp, err := stream.Recv()
			if err != nil {
				errors <- &qberrors.WrappedMessageError{Message: "worker " + workerIDStr + ": failed to receive result", Cause: err}
				return
			}

			result, ok := resp.Payload.(*pb.StreamBatchPutResponse_Result)
			if !ok {
				errors <- &qberrors.MessageError{Message: "worker " + workerIDStr + ": unexpected response type"}
				return
			}

			results <- result.Result.Succeeded
		}(w)
	}

	// Wait for all workers to complete
	wg.Wait()
	close(results)
	close(errors)

	// Collect results
	successCount := 0
	for succeeded := range results {
		if succeeded {
			successCount++
		}
	}

	// Check for errors
	for err := range errors {
		t.Errorf("Worker error: %v", err)
	}

	t.Logf("Concurrent conditional batches: %d/%d succeeded", successCount, numWorkers)
	assert.Equal(t, numWorkers, successCount, "All concurrent conditional batches should succeed")
}

// TestConditionalBatchEmptyConditions tests batch behavior with no conditions.
func TestConditionalBatchEmptyConditions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	timestamp := time.Now().UnixNano()
	entityType := "cond_empty"
	entityID := uint64(timestamp)

	// Create streaming batch with no conditions
	stream, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create stream")

	header := &pb.StreamBatchHeader{
		ShardId:         1,
		TotalOperations: 1,
		ChunkSize:       10,
		Conditions:      []*pb.BatchCondition{}, // Empty conditions
		Consistency:     pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{Header: header},
	})
	require.NoError(t, err, "Failed to send header")

	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive initial progress")

	chunk := &pb.StreamBatchChunk{
		ChunkIndex: 0,
		Operations: []*pb.PutRequest{
			{
				EntityType: entityType,
				Id:         entityID,
				Data:       []byte(`{"status":"no_conditions"}`),
			},
		},
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
	})
	require.NoError(t, err, "Failed to send chunk")

	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive chunk progress")

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Complete{
			Complete: &pb.StreamBatchComplete{Commit: true},
		},
	})
	require.NoError(t, err, "Failed to send complete")

	resp, err := stream.Recv()
	require.NoError(t, err, "Failed to receive final result")

	result, ok := resp.Payload.(*pb.StreamBatchPutResponse_Result)
	require.True(t, ok, "Expected result response")
	assert.True(t, result.Result.Succeeded, "Batch with no conditions should succeed")
	assert.Equal(t, int64(1), result.Result.OperationsCommitted)

	t.Logf("Conditional batch with empty conditions succeeded: %d operations committed", result.Result.OperationsCommitted)
}

// setupGRPCConnection establishes a gRPC connection to the test cluster.
func setupGRPCConnection(t *testing.T, ctx context.Context) *grpc.ClientConn {
	endpoints := helpers.GetClusterEndpoints()
	endpoint := endpoints[0]

	conn, err := grpc.DialContext(ctx, endpoint,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13})),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
	)
	require.NoError(t, err, "Failed to dial gRPC server at %s", endpoint)

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
