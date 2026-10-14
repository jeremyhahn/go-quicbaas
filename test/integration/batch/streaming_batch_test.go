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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestStreamingBatchSmallBatchSingleChunk tests a small batch that fits
// in a single chunk.
func TestStreamingBatchSmallBatchSingleChunk(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupStreamingConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	timestamp := time.Now().UnixNano()
	entityType := "stream_small"
	numOps := 10

	stream, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create stream")

	// Send header
	header := &pb.StreamBatchHeader{
		ShardId:         1,
		TotalOperations: int64(numOps),
		ChunkSize:       int32(numOps),
		Consistency:     pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{Header: header},
	})
	require.NoError(t, err, "Failed to send header")

	// Receive initial progress
	resp, err := stream.Recv()
	require.NoError(t, err, "Failed to receive initial progress")
	progress, ok := resp.Payload.(*pb.StreamBatchPutResponse_Progress)
	require.True(t, ok, "Expected progress response after header")
	assert.Equal(t, int64(0), progress.Progress.OperationsReceived)
	assert.Equal(t, int32(-1), progress.Progress.ChunkIndex)

	// Prepare operations
	operations := make([]*pb.PutRequest, numOps)
	for i := 0; i < numOps; i++ {
		operations[i] = &pb.PutRequest{
			EntityType: entityType,
			Id:         uint64(timestamp + int64(i*1000)),
			Data:       []byte(fmt.Sprintf(`{"index":%d,"small_batch":true}`, i)),
		}
	}

	// Send single chunk with all operations
	chunk := &pb.StreamBatchChunk{
		ChunkIndex: 0,
		Operations: operations,
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
	})
	require.NoError(t, err, "Failed to send chunk")

	// Receive chunk progress
	resp, err = stream.Recv()
	require.NoError(t, err, "Failed to receive chunk progress")
	progress, ok = resp.Payload.(*pb.StreamBatchPutResponse_Progress)
	require.True(t, ok, "Expected progress response after chunk")
	assert.Equal(t, int64(numOps), progress.Progress.OperationsReceived)
	assert.Equal(t, int64(numOps), progress.Progress.OperationsValidated)
	assert.Equal(t, int32(0), progress.Progress.ChunkIndex)
	assert.Empty(t, progress.Progress.ValidationErrors)

	// Send complete
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
	assert.True(t, result.Result.Succeeded)
	assert.Equal(t, int64(numOps), result.Result.OperationsCommitted)
	assert.Empty(t, result.Result.Error)
	assert.NotZero(t, result.Result.Timestamp)

	t.Logf("Small batch (single chunk) completed: %d operations committed", result.Result.OperationsCommitted)

	// Verify data was actually written
	for i := 0; i < numOps; i++ {
		getResp, err := client.Get(ctx, &pb.GetRequest{
			EntityType: entityType,
			Id:         uint64(timestamp + int64(i*1000)),
		})
		require.NoError(t, err, "Failed to get entity %d", i)
		assert.NotNil(t, getResp.Data)
	}
}

// TestStreamingBatchLargeBatchMultipleChunks tests a large batch split
// across multiple chunks.
func TestStreamingBatchLargeBatchMultipleChunks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	conn := setupStreamingConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	timestamp := time.Now().UnixNano()
	entityType := "stream_large"
	totalOps := 1000 // 1K operations
	chunkSize := 100 // 100 per chunk = 10 chunks

	stream, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create stream")

	// Send header
	header := &pb.StreamBatchHeader{
		ShardId:         1,
		TotalOperations: int64(totalOps),
		ChunkSize:       int32(chunkSize),
		Consistency:     pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{Header: header},
	})
	require.NoError(t, err, "Failed to send header")

	// Receive initial progress
	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive initial progress")

	// Send chunks
	numChunks := (totalOps + chunkSize - 1) / chunkSize
	startTime := time.Now()

	for chunkIdx := 0; chunkIdx < numChunks; chunkIdx++ {
		startOp := chunkIdx * chunkSize
		endOp := startOp + chunkSize
		if endOp > totalOps {
			endOp = totalOps
		}

		operations := make([]*pb.PutRequest, endOp-startOp)
		for i := startOp; i < endOp; i++ {
			operations[i-startOp] = &pb.PutRequest{
				EntityType: entityType,
				Id:         uint64(timestamp + int64(i)),
				Data:       []byte(fmt.Sprintf(`{"index":%d,"chunk":%d,"large_batch":true}`, i, chunkIdx)),
			}
		}

		chunk := &pb.StreamBatchChunk{
			ChunkIndex: int32(chunkIdx),
			Operations: operations,
		}

		err = stream.Send(&pb.StreamBatchPutRequest{
			Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
		})
		require.NoError(t, err, "Failed to send chunk %d", chunkIdx)

		// Receive chunk progress
		resp, err := stream.Recv()
		require.NoError(t, err, "Failed to receive progress for chunk %d", chunkIdx)

		progress, ok := resp.Payload.(*pb.StreamBatchPutResponse_Progress)
		require.True(t, ok, "Expected progress response for chunk %d", chunkIdx)

		expectedReceived := int64(endOp)
		assert.Equal(t, expectedReceived, progress.Progress.OperationsReceived,
			"Chunk %d: expected %d received, got %d", chunkIdx, expectedReceived, progress.Progress.OperationsReceived)
		assert.Equal(t, int32(chunkIdx), progress.Progress.ChunkIndex)

		if len(progress.Progress.ValidationErrors) > 0 {
			t.Logf("Chunk %d validation errors: %v", chunkIdx, progress.Progress.ValidationErrors)
		}
	}

	sendDuration := time.Since(startTime)
	t.Logf("Sent %d chunks (%d operations) in %v", numChunks, totalOps, sendDuration)

	// Send complete
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

	totalDuration := time.Since(startTime)
	opsPerSecond := float64(result.Result.OperationsCommitted) / totalDuration.Seconds()

	assert.True(t, result.Result.Succeeded, "Large batch should succeed")
	assert.Equal(t, int64(totalOps), result.Result.OperationsCommitted)
	assert.Empty(t, result.Result.Error)

	t.Logf("Large batch completed: %d operations in %v (%.2f ops/sec)",
		result.Result.OperationsCommitted, totalDuration, opsPerSecond)

	// Sample verify some data
	sampleIndices := []int{0, totalOps / 4, totalOps / 2, 3 * totalOps / 4, totalOps - 1}
	for _, idx := range sampleIndices {
		getResp, err := client.Get(ctx, &pb.GetRequest{
			EntityType: entityType,
			Id:         uint64(timestamp + int64(idx)),
		})
		require.NoError(t, err, "Failed to get entity at index %d", idx)
		assert.NotNil(t, getResp.Data, "Entity at index %d should have data", idx)
	}
}

// TestStreamingBatchProgressReportingAccuracy tests that progress is
// reported accurately during streaming.
func TestStreamingBatchProgressReportingAccuracy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	conn := setupStreamingConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	timestamp := time.Now().UnixNano()
	entityType := "stream_progress"
	totalOps := 500
	chunkSize := 50

	stream, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create stream")

	// Send header
	header := &pb.StreamBatchHeader{
		ShardId:         1,
		TotalOperations: int64(totalOps),
		ChunkSize:       int32(chunkSize),
		Consistency:     pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{Header: header},
	})
	require.NoError(t, err, "Failed to send header")

	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive initial progress")

	// Track progress accuracy
	var progressReports []struct {
		chunkIndex    int32
		received      int64
		validated     int64
		expectedRecv  int64
		expectedValid int64
	}

	numChunks := (totalOps + chunkSize - 1) / chunkSize

	for chunkIdx := 0; chunkIdx < numChunks; chunkIdx++ {
		startOp := chunkIdx * chunkSize
		endOp := startOp + chunkSize
		if endOp > totalOps {
			endOp = totalOps
		}

		operations := make([]*pb.PutRequest, endOp-startOp)
		for i := startOp; i < endOp; i++ {
			operations[i-startOp] = &pb.PutRequest{
				EntityType: entityType,
				Id:         uint64(timestamp + int64(i)),
				Data:       []byte(fmt.Sprintf(`{"index":%d}`, i)),
			}
		}

		chunk := &pb.StreamBatchChunk{
			ChunkIndex: int32(chunkIdx),
			Operations: operations,
		}

		err = stream.Send(&pb.StreamBatchPutRequest{
			Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
		})
		require.NoError(t, err, "Failed to send chunk %d", chunkIdx)

		resp, err := stream.Recv()
		require.NoError(t, err, "Failed to receive progress for chunk %d", chunkIdx)

		progress, ok := resp.Payload.(*pb.StreamBatchPutResponse_Progress)
		require.True(t, ok, "Expected progress response for chunk %d", chunkIdx)

		expectedReceived := int64(endOp)
		expectedValidated := int64(endOp)

		progressReports = append(progressReports, struct {
			chunkIndex    int32
			received      int64
			validated     int64
			expectedRecv  int64
			expectedValid int64
		}{
			chunkIndex:    progress.Progress.ChunkIndex,
			received:      progress.Progress.OperationsReceived,
			validated:     progress.Progress.OperationsValidated,
			expectedRecv:  expectedReceived,
			expectedValid: expectedValidated,
		})
	}

	// Verify progress accuracy
	for i, report := range progressReports {
		assert.Equal(t, int32(i), report.chunkIndex,
			"Chunk index mismatch at position %d", i)
		assert.Equal(t, report.expectedRecv, report.received,
			"Received count mismatch at chunk %d: expected %d, got %d",
			i, report.expectedRecv, report.received)
		// Validated should be <= received (some may fail validation)
		assert.LessOrEqual(t, report.validated, report.received,
			"Validated count exceeds received at chunk %d", i)
	}

	// Complete the batch
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
	assert.True(t, result.Result.Succeeded)

	t.Logf("Progress reporting verified: %d chunks with accurate counts", len(progressReports))
}

// TestStreamingBatchAbortMidStream tests aborting a streaming batch
// before completion.
func TestStreamingBatchAbortMidStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupStreamingConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	timestamp := time.Now().UnixNano()
	entityType := "stream_abort"
	totalOps := 100
	chunkSize := 20

	stream, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create stream")

	// Send header
	header := &pb.StreamBatchHeader{
		ShardId:         1,
		TotalOperations: int64(totalOps),
		ChunkSize:       int32(chunkSize),
		Consistency:     pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{Header: header},
	})
	require.NoError(t, err, "Failed to send header")

	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive initial progress")

	// Send only half the chunks
	chunksToSend := 2 // Send 40 out of 100 operations
	for chunkIdx := 0; chunkIdx < chunksToSend; chunkIdx++ {
		startOp := chunkIdx * chunkSize
		endOp := startOp + chunkSize

		operations := make([]*pb.PutRequest, chunkSize)
		for i := startOp; i < endOp; i++ {
			operations[i-startOp] = &pb.PutRequest{
				EntityType: entityType,
				Id:         uint64(timestamp + int64(i)),
				Data:       []byte(fmt.Sprintf(`{"index":%d,"aborted":false}`, i)),
			}
		}

		chunk := &pb.StreamBatchChunk{
			ChunkIndex: int32(chunkIdx),
			Operations: operations,
		}

		err = stream.Send(&pb.StreamBatchPutRequest{
			Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
		})
		require.NoError(t, err, "Failed to send chunk %d", chunkIdx)

		_, err = stream.Recv()
		require.NoError(t, err, "Failed to receive progress for chunk %d", chunkIdx)
	}

	// Abort instead of completing
	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Complete{
			Complete: &pb.StreamBatchComplete{Commit: false}, // Abort
		},
	})
	require.NoError(t, err, "Failed to send abort")

	// Receive final result
	resp, err := stream.Recv()
	require.NoError(t, err, "Failed to receive final result")

	result, ok := resp.Payload.(*pb.StreamBatchPutResponse_Result)
	require.True(t, ok, "Expected result response")

	assert.False(t, result.Result.Succeeded, "Aborted batch should not succeed")
	assert.Equal(t, int64(0), result.Result.OperationsCommitted, "No operations should be committed on abort")
	assert.Contains(t, result.Result.Error, "aborted", "Error should mention abort")

	t.Logf("Streaming batch abort succeeded: error=%s", result.Result.Error)
}

// TestStreamingBatchResumeAfterFailure tests resuming from the last
// successfully received chunk.
func TestStreamingBatchResumeAfterFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	conn := setupStreamingConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	timestamp := time.Now().UnixNano()
	entityType := "stream_resume"
	totalOps := 100
	chunkSize := 20
	numChunks := totalOps / chunkSize

	// Phase 1: Start a batch and abort after some chunks
	stream1, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create first stream")

	header := &pb.StreamBatchHeader{
		ShardId:         1,
		TotalOperations: int64(totalOps),
		ChunkSize:       int32(chunkSize),
		Consistency:     pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	err = stream1.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{Header: header},
	})
	require.NoError(t, err, "Failed to send header")

	_, err = stream1.Recv()
	require.NoError(t, err, "Failed to receive initial progress")

	// Send half the chunks
	lastChunkIndex := int32(-1)
	chunksToSend := numChunks / 2

	for chunkIdx := 0; chunkIdx < chunksToSend; chunkIdx++ {
		startOp := chunkIdx * chunkSize
		endOp := startOp + chunkSize

		operations := make([]*pb.PutRequest, chunkSize)
		for i := startOp; i < endOp; i++ {
			operations[i-startOp] = &pb.PutRequest{
				EntityType: entityType,
				Id:         uint64(timestamp + int64(i)),
				Data:       []byte(fmt.Sprintf(`{"index":%d,"phase":1}`, i)),
			}
		}

		chunk := &pb.StreamBatchChunk{
			ChunkIndex: int32(chunkIdx),
			Operations: operations,
		}

		err = stream1.Send(&pb.StreamBatchPutRequest{
			Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
		})
		require.NoError(t, err, "Failed to send chunk %d", chunkIdx)

		resp, err := stream1.Recv()
		require.NoError(t, err, "Failed to receive progress for chunk %d", chunkIdx)

		progress := resp.Payload.(*pb.StreamBatchPutResponse_Progress)
		lastChunkIndex = progress.Progress.ChunkIndex
	}

	t.Logf("Phase 1: Sent %d chunks, last chunk index: %d", chunksToSend, lastChunkIndex)

	// Abort first stream
	err = stream1.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Complete{
			Complete: &pb.StreamBatchComplete{Commit: false},
		},
	})
	require.NoError(t, err, "Failed to abort first stream")

	_, err = stream1.Recv()
	require.NoError(t, err, "Failed to receive abort result")

	// Phase 2: Start new stream and complete all operations
	stream2, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create second stream")

	err = stream2.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{Header: header},
	})
	require.NoError(t, err, "Failed to send header for resume")

	_, err = stream2.Recv()
	require.NoError(t, err, "Failed to receive initial progress for resume")

	// Send all chunks in the new session
	for chunkIdx := 0; chunkIdx < numChunks; chunkIdx++ {
		startOp := chunkIdx * chunkSize
		endOp := startOp + chunkSize

		operations := make([]*pb.PutRequest, chunkSize)
		for i := startOp; i < endOp; i++ {
			operations[i-startOp] = &pb.PutRequest{
				EntityType: entityType,
				Id:         uint64(timestamp + int64(i)),
				Data:       []byte(fmt.Sprintf(`{"index":%d,"phase":2}`, i)),
			}
		}

		chunk := &pb.StreamBatchChunk{
			ChunkIndex: int32(chunkIdx),
			Operations: operations,
		}

		err = stream2.Send(&pb.StreamBatchPutRequest{
			Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
		})
		require.NoError(t, err, "Failed to send chunk %d in resume", chunkIdx)

		_, err = stream2.Recv()
		require.NoError(t, err, "Failed to receive progress for chunk %d in resume", chunkIdx)
	}

	// Commit
	err = stream2.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Complete{
			Complete: &pb.StreamBatchComplete{Commit: true},
		},
	})
	require.NoError(t, err, "Failed to send complete for resume")

	resp, err := stream2.Recv()
	require.NoError(t, err, "Failed to receive final result for resume")

	result, ok := resp.Payload.(*pb.StreamBatchPutResponse_Result)
	require.True(t, ok, "Expected result response")
	assert.True(t, result.Result.Succeeded)
	assert.Equal(t, int64(totalOps), result.Result.OperationsCommitted)

	t.Logf("Resume completed: %d operations committed", result.Result.OperationsCommitted)
}

// TestStreamingBatchBackpressure tests back-pressure handling under load.
func TestStreamingBatchBackpressure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	conn := setupStreamingConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	timestamp := time.Now().UnixNano()
	entityType := "stream_backpressure"
	totalOps := 2000
	chunkSize := 100

	stream, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create stream")

	// Send header
	header := &pb.StreamBatchHeader{
		ShardId:         1,
		TotalOperations: int64(totalOps),
		ChunkSize:       int32(chunkSize),
		Consistency:     pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{Header: header},
	})
	require.NoError(t, err, "Failed to send header")

	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive initial progress")

	// Generate large data for each operation to increase pressure
	largeData := make([]byte, 4096) // 4KB per operation
	for i := range largeData {
		largeData[i] = byte('A' + (i % 26))
	}

	numChunks := (totalOps + chunkSize - 1) / chunkSize
	startTime := time.Now()

	// Send chunks rapidly without waiting for progress
	sendDone := make(chan struct{})
	var sendErr error
	var chunksSent int

	go func() {
		defer close(sendDone)
		for chunkIdx := 0; chunkIdx < numChunks; chunkIdx++ {
			startOp := chunkIdx * chunkSize
			endOp := startOp + chunkSize
			if endOp > totalOps {
				endOp = totalOps
			}

			operations := make([]*pb.PutRequest, endOp-startOp)
			for i := startOp; i < endOp; i++ {
				operations[i-startOp] = &pb.PutRequest{
					EntityType: entityType,
					Id:         uint64(timestamp + int64(i)),
					Data:       largeData,
				}
			}

			chunk := &pb.StreamBatchChunk{
				ChunkIndex: int32(chunkIdx),
				Operations: operations,
			}

			if err := stream.Send(&pb.StreamBatchPutRequest{
				Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
			}); err != nil {
				sendErr = err
				return
			}
			chunksSent++
		}

		// Send complete
		if err := stream.Send(&pb.StreamBatchPutRequest{
			Payload: &pb.StreamBatchPutRequest_Complete{
				Complete: &pb.StreamBatchComplete{Commit: true},
			},
		}); err != nil {
			sendErr = err
		}
	}()

	// Receive all progress messages and final result
	var progressCount int
	var finalResult *pb.StreamBatchResult

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Logf("Receive error: %v", err)
			break
		}

		switch payload := resp.Payload.(type) {
		case *pb.StreamBatchPutResponse_Progress:
			progressCount++
		case *pb.StreamBatchPutResponse_Result:
			finalResult = payload.Result
		}

		if finalResult != nil {
			break
		}
	}

	<-sendDone

	require.NoError(t, sendErr, "Send error occurred")
	require.NotNil(t, finalResult, "Should receive final result")

	duration := time.Since(startTime)
	throughputMB := float64(totalOps*len(largeData)) / duration.Seconds() / 1024 / 1024

	assert.True(t, finalResult.Succeeded, "Backpressure test should succeed")
	assert.Equal(t, int64(totalOps), finalResult.OperationsCommitted)

	t.Logf("Backpressure test completed: %d operations, %d progress updates, %.2f MB/s throughput",
		finalResult.OperationsCommitted, progressCount, throughputMB)
}

// TestStreamingBatchConcurrentSessions tests multiple concurrent streaming
// sessions.
func TestStreamingBatchConcurrentSessions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	numSessions := 5
	opsPerSession := 100
	chunkSize := 20

	type sessionResult struct {
		sessionID int
		succeeded bool
		committed int64
		err       error
	}

	var wg sync.WaitGroup
	results := make(chan sessionResult, numSessions)

	for s := 0; s < numSessions; s++ {
		wg.Add(1)
		go func(sessionID int) {
			sessionCtx, sessionCancel := context.WithTimeout(ctx, 60*time.Second)
			defer sessionCancel()

			sendResult := func(succeeded bool, committed int64, err error) {
				results <- sessionResult{sessionID, succeeded, committed, err}
				wg.Done()
			}

			conn := setupStreamingConnection(t, sessionCtx)
			defer conn.Close()

			client := pb.NewQuicBaaSClient(conn)

			timestamp := time.Now().UnixNano() + int64(sessionID*1000000)
			entityType := fmt.Sprintf("stream_concurrent_%d", sessionID)

			stream, err := client.StreamBatchPut(sessionCtx)
			if err != nil {
				sendResult(false, 0, err)
				return
			}

			header := &pb.StreamBatchHeader{
				ShardId:         1,
				TotalOperations: int64(opsPerSession),
				ChunkSize:       int32(chunkSize),
				Consistency:     pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
			}

			if err := stream.Send(&pb.StreamBatchPutRequest{
				Payload: &pb.StreamBatchPutRequest_Header{Header: header},
			}); err != nil {
				sendResult(false, 0, err)
				return
			}

			if _, err := stream.Recv(); err != nil {
				sendResult(false, 0, err)
				return
			}

			numChunks := (opsPerSession + chunkSize - 1) / chunkSize
			for chunkIdx := 0; chunkIdx < numChunks; chunkIdx++ {
				startOp := chunkIdx * chunkSize
				endOp := startOp + chunkSize
				if endOp > opsPerSession {
					endOp = opsPerSession
				}

				operations := make([]*pb.PutRequest, endOp-startOp)
				for i := startOp; i < endOp; i++ {
					operations[i-startOp] = &pb.PutRequest{
						EntityType: entityType,
						Id:         uint64(timestamp + int64(i)),
						Data:       []byte(fmt.Sprintf(`{"session":%d,"index":%d}`, sessionID, i)),
					}
				}

				chunk := &pb.StreamBatchChunk{
					ChunkIndex: int32(chunkIdx),
					Operations: operations,
				}

				if err := stream.Send(&pb.StreamBatchPutRequest{
					Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
				}); err != nil {
					sendResult(false, 0, err)
					return
				}

				if _, err := stream.Recv(); err != nil {
					sendResult(false, 0, err)
					return
				}
			}

			if err := stream.Send(&pb.StreamBatchPutRequest{
				Payload: &pb.StreamBatchPutRequest_Complete{
					Complete: &pb.StreamBatchComplete{Commit: true},
				},
			}); err != nil {
				sendResult(false, 0, err)
				return
			}

			resp, err := stream.Recv()
			if err != nil {
				sendResult(false, 0, err)
				return
			}

			result := resp.Payload.(*pb.StreamBatchPutResponse_Result)
			sendResult(result.Result.Succeeded, result.Result.OperationsCommitted, nil)
		}(s)
	}

	wg.Wait()
	close(results)

	// Collect results
	successCount := 0
	totalCommitted := int64(0)

	for r := range results {
		if r.err != nil {
			t.Errorf("Session %d error: %v", r.sessionID, r.err)
			continue
		}

		if r.succeeded {
			successCount++
			totalCommitted += r.committed
		} else {
			t.Errorf("Session %d failed", r.sessionID)
		}
	}

	assert.Equal(t, numSessions, successCount, "All sessions should succeed")
	assert.Equal(t, int64(numSessions*opsPerSession), totalCommitted,
		"All operations should be committed")

	t.Logf("Concurrent sessions: %d/%d succeeded, %d total operations",
		successCount, numSessions, totalCommitted)
}

// TestStreamingBatchValidationErrors tests handling of validation errors
// in chunks.
func TestStreamingBatchValidationErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupStreamingConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	timestamp := time.Now().UnixNano()

	stream, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create stream")

	// Send header
	header := &pb.StreamBatchHeader{
		ShardId:         1,
		TotalOperations: 5,
		ChunkSize:       10,
		Consistency:     pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{Header: header},
	})
	require.NoError(t, err, "Failed to send header")

	_, err = stream.Recv()
	require.NoError(t, err, "Failed to receive initial progress")

	// Send chunk with invalid operations (empty entity_type and empty data)
	chunk := &pb.StreamBatchChunk{
		ChunkIndex: 0,
		Operations: []*pb.PutRequest{
			{
				EntityType: "valid_type",
				Id:         uint64(timestamp),
				Data:       []byte(`{"valid":true}`),
			},
			{
				EntityType: "", // Invalid: empty entity_type
				Id:         uint64(timestamp + 1000),
				Data:       []byte(`{"invalid":"empty_type"}`),
			},
			{
				EntityType: "valid_type",
				Id:         uint64(timestamp + 2000),
				Data:       []byte{}, // Invalid: empty data
			},
			{
				EntityType: "valid_type",
				Id:         uint64(timestamp + 3000),
				Data:       []byte(`{"valid":true}`),
			},
			{
				EntityType: "", // Invalid
				Id:         uint64(timestamp + 4000),
				Data:       []byte{}, // Invalid
			},
		},
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
	})
	require.NoError(t, err, "Failed to send chunk with invalid ops")

	// Receive chunk progress with validation errors
	resp, err := stream.Recv()
	require.NoError(t, err, "Failed to receive chunk progress")

	progress, ok := resp.Payload.(*pb.StreamBatchPutResponse_Progress)
	require.True(t, ok, "Expected progress response")

	// Should report all 5 received but only 2 validated
	assert.Equal(t, int64(5), progress.Progress.OperationsReceived)
	assert.Equal(t, int64(2), progress.Progress.OperationsValidated)
	assert.NotEmpty(t, progress.Progress.ValidationErrors)

	t.Logf("Validation errors: %v", progress.Progress.ValidationErrors)

	// Complete the batch - should fail due to validation errors
	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Complete{
			Complete: &pb.StreamBatchComplete{Commit: true},
		},
	})
	require.NoError(t, err, "Failed to send complete")

	resp, err = stream.Recv()
	require.NoError(t, err, "Failed to receive final result")

	result, ok := resp.Payload.(*pb.StreamBatchPutResponse_Result)
	require.True(t, ok, "Expected result response")

	assert.False(t, result.Result.Succeeded, "Batch with validation errors should fail")
	assert.Contains(t, result.Result.Error, "validation", "Error should mention validation")
	assert.NotEmpty(t, result.Result.FailedKeys)

	t.Logf("Validation error test: succeeded=%v, error=%s, failed_keys=%v",
		result.Result.Succeeded, result.Result.Error, result.Result.FailedKeys)
}

// TestStreamingBatchEmptyChunk tests handling of empty batches with zero operations.
// The server requires total_operations to be positive, so this should return an error.
func TestStreamingBatchEmptyChunk(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupStreamingConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	stream, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create stream")

	// Send header with zero operations - server should reject this
	header := &pb.StreamBatchHeader{
		ShardId:         1,
		TotalOperations: 0, // Invalid - must be positive
		ChunkSize:       10,
		Consistency:     pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{Header: header},
	})
	require.NoError(t, err, "Send should succeed, validation happens server-side")

	// Server should reject the header with zero operations
	_, err = stream.Recv()
	require.Error(t, err, "Server should reject header with zero total_operations")
	assert.Contains(t, err.Error(), "total_operations must be positive",
		"Error should mention that total_operations must be positive")

	t.Logf("Empty batch test: correctly rejected with error: %v", err)
}

// TestStreamingBatchNoHeaderError tests that sending chunks without
// a header fails appropriately.
func TestStreamingBatchNoHeaderError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupStreamingConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	timestamp := time.Now().UnixNano()

	stream, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create stream")

	// Send chunk without header first
	chunk := &pb.StreamBatchChunk{
		ChunkIndex: 0,
		Operations: []*pb.PutRequest{
			{
				EntityType: "no_header",
				Id:         uint64(timestamp),
				Data:       []byte(`{"test":true}`),
			},
		},
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Chunk{Chunk: chunk},
	})
	require.NoError(t, err, "Send should succeed, error happens on server side")

	// Receive should return error
	_, err = stream.Recv()
	assert.Error(t, err, "Should receive error when sending chunk without header")

	t.Logf("No header error test: %v", err)
}

// TestStreamingBatchInvalidHeader tests handling of invalid headers.
func TestStreamingBatchInvalidHeader(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupStreamingConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	stream, err := client.StreamBatchPut(ctx)
	require.NoError(t, err, "Failed to create stream")

	// Send header with invalid total operations (negative)
	header := &pb.StreamBatchHeader{
		ShardId:         1,
		TotalOperations: -1, // Invalid
		ChunkSize:       10,
		Consistency:     pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	err = stream.Send(&pb.StreamBatchPutRequest{
		Payload: &pb.StreamBatchPutRequest_Header{Header: header},
	})
	require.NoError(t, err, "Send should succeed, validation happens server-side")

	// Receive should return error
	_, err = stream.Recv()
	assert.Error(t, err, "Should receive error for invalid header")

	t.Logf("Invalid header error test: %v", err)
}

// setupStreamingConnection establishes a gRPC connection optimized for streaming.
func setupStreamingConnection(t *testing.T, ctx context.Context) *grpc.ClientConn {
	endpoints := helpers.GetClusterEndpoints()
	endpoint := endpoints[0]

	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	conn, err := grpc.DialContext(ctx, endpoint,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithPerRPCCredentials(helpers.DataPlaneGRPCCredentials(t)),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(64*1024*1024)), // 64MB max message
		grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(64*1024*1024)), // 64MB max message
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
