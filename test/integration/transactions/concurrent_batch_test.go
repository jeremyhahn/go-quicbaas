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

package transactions

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// TestConcurrentBatchPutOperations tests multiple batch puts running concurrently
func TestConcurrentBatchPutOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Run 10 concurrent batch operations
	concurrency := 10
	batchSize := 10
	baseTimestamp := time.Now().UnixNano()

	var wg sync.WaitGroup
	var successCount atomic.Int32
	var errorCount atomic.Int32
	var mu sync.Mutex
	var errorDetails []string

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Each worker creates unique entities
			requests := make([]*pb.PutRequest, batchSize)
			for j := 0; j < batchSize; j++ {
				entityID := uint64(baseTimestamp + int64(workerID*1000000) + int64(j*1000))
				requests[j] = &pb.PutRequest{
					EntityType: "concurrent_test",
					Id:         entityID,
					Data:       []byte(fmt.Sprintf(`{"worker":%d,"index":%d}`, workerID, j)),
				}
			}

			batchReq := &pb.BatchPutRequest{Requests: requests}

			opCtx, opCancel := context.WithTimeout(ctx, 10*time.Second)
			defer opCancel()

			resp, err := client.BatchPut(opCtx, batchReq)
			if err != nil {
				errMsg := fmt.Sprintf("Worker %d failed: %v", workerID, err)
				t.Logf("%s", errMsg)
				mu.Lock()
				errorDetails = append(errorDetails, errMsg)
				mu.Unlock()
				errorCount.Add(1)
				return
			}

			if resp != nil && len(resp.Responses) == batchSize {
				successCount.Add(1)
			} else {
				errMsg := fmt.Sprintf("Worker %d incomplete response: got %d responses, expected %d",
					workerID, len(resp.GetResponses()), batchSize)
				t.Logf("%s", errMsg)
				mu.Lock()
				errorDetails = append(errorDetails, errMsg)
				mu.Unlock()
				errorCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	// Verify results
	totalSuccess := successCount.Load()
	totalErrors := errorCount.Load()

	if totalErrors > 0 {
		mu.Lock()
		for _, detail := range errorDetails {
			t.Logf("  Error: %s", detail)
		}
		mu.Unlock()
	}

	assert.Equal(t, int32(concurrency), totalSuccess, "All concurrent batches should succeed")
	assert.Equal(t, int32(0), totalErrors, "No errors should occur")

	t.Logf("Successfully executed %d concurrent batch operations (%d entities each)",
		concurrency, batchSize)
}

// TestConcurrentBatchDeleteOperations tests multiple batch deletes running concurrently
func TestConcurrentBatchDeleteOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	concurrency := 10
	batchSize := 10
	baseTimestamp := time.Now().UnixNano()

	// First create entities to delete
	for i := 0; i < concurrency; i++ {
		requests := make([]*pb.PutRequest, batchSize)
		for j := 0; j < batchSize; j++ {
			entityID := uint64(baseTimestamp + int64(i*1000000) + int64(j*1000))
			requests[j] = &pb.PutRequest{
				EntityType: "concurrent_del_test",
				Id:         entityID,
				Data:       []byte(fmt.Sprintf(`{"worker":%d,"index":%d}`, i, j)),
			}
		}

		setupCtx, setupCancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := client.BatchPut(setupCtx, &pb.BatchPutRequest{Requests: requests})
		setupCancel()
		require.NoError(t, err)
	}

	// Now delete them concurrently
	var wg sync.WaitGroup
	var successCount atomic.Int32
	var errorCount atomic.Int32
	var mu sync.Mutex
	var errorDetails []string

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			requests := make([]*pb.DeleteRequest, batchSize)
			for j := 0; j < batchSize; j++ {
				entityID := uint64(baseTimestamp + int64(workerID*1000000) + int64(j*1000))
				requests[j] = &pb.DeleteRequest{
					EntityType: "concurrent_del_test",
					Id:         entityID,
				}
			}

			batchReq := &pb.BatchDeleteRequest{Requests: requests}

			opCtx, opCancel := context.WithTimeout(ctx, 10*time.Second)
			defer opCancel()

			resp, err := client.BatchDelete(opCtx, batchReq)
			if err != nil {
				errMsg := fmt.Sprintf("Delete worker %d failed: %v", workerID, err)
				t.Logf("%s", errMsg)
				mu.Lock()
				errorDetails = append(errorDetails, errMsg)
				mu.Unlock()
				errorCount.Add(1)
				return
			}

			if resp != nil && len(resp.Responses) == batchSize {
				successCount.Add(1)
			} else {
				errMsg := fmt.Sprintf("Delete worker %d incomplete response: got %d responses, expected %d",
					workerID, len(resp.GetResponses()), batchSize)
				t.Logf("%s", errMsg)
				mu.Lock()
				errorDetails = append(errorDetails, errMsg)
				mu.Unlock()
				errorCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	totalSuccess := successCount.Load()
	totalErrors := errorCount.Load()

	if totalErrors > 0 {
		mu.Lock()
		for _, detail := range errorDetails {
			t.Logf("  Error: %s", detail)
		}
		mu.Unlock()
	}

	assert.Equal(t, int32(concurrency), totalSuccess, "All concurrent batch deletes should succeed")
	assert.Equal(t, int32(0), totalErrors, "No errors should occur")

	t.Logf("Successfully executed %d concurrent batch delete operations", concurrency)
}

// TestConcurrentMixedBatchOperations tests concurrent batch puts and deletes
func TestConcurrentMixedBatchOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	baseTimestamp := time.Now().UnixNano()
	operations := 20

	var wg sync.WaitGroup
	var successCount atomic.Int32

	for i := 0; i < operations; i++ {
		wg.Add(1)

		// Alternate between put and delete operations
		if i%2 == 0 {
			// Put operation
			go func(opID int) {
				defer wg.Done()

				entityID := uint64(baseTimestamp + int64(opID*1000))
				req := &pb.BatchPutRequest{
					Requests: []*pb.PutRequest{
						{
							EntityType: "mixed_test",
							Id:         entityID,
							Data:       []byte(fmt.Sprintf(`{"op":%d,"type":"put"}`, opID)),
						},
					},
				}

				_, err := client.BatchPut(ctx, req)
				if err == nil {
					successCount.Add(1)
				}
			}(i)
		} else {
			// Delete operation (delete previously created entity)
			go func(opID int) {
				defer wg.Done()

				// Delete the entity created by previous operation
				entityID := uint64(baseTimestamp + int64((opID-1)*1000))
				req := &pb.BatchDeleteRequest{
					Requests: []*pb.DeleteRequest{
						{
							EntityType: "mixed_test",
							Id:         entityID,
						},
					},
				}

				_, err := client.BatchDelete(ctx, req)
				if err == nil {
					successCount.Add(1)
				}
			}(i)
		}
	}

	wg.Wait()

	totalSuccess := successCount.Load()
	assert.GreaterOrEqual(t, totalSuccess, int32(operations/2),
		"At least half of mixed operations should succeed")

	t.Logf("Successfully executed %d/%d concurrent mixed batch operations",
		totalSuccess, operations)
}

// TestConcurrentBatchPutSameEntity tests race condition on same entity updates
func TestConcurrentBatchPutSameEntity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	entityID := uint64(time.Now().UnixNano())
	concurrency := 10

	var wg sync.WaitGroup
	var successCount atomic.Int32

	// Multiple workers updating the same entity concurrently
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(version int) {
			defer wg.Done()

			req := &pb.BatchPutRequest{
				Requests: []*pb.PutRequest{
					{
						EntityType: "race_test",
						Id:         entityID,
						Data:       []byte(fmt.Sprintf(`{"version":%d,"timestamp":%d}`, version, time.Now().UnixNano())),
					},
				},
			}

			_, err := client.BatchPut(ctx, req)
			if err == nil {
				successCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	// All should succeed (last write wins)
	totalSuccess := successCount.Load()
	assert.Equal(t, int32(concurrency), totalSuccess,
		"All concurrent updates to same entity should succeed")

	// Verify entity exists and has some version
	getResp, err := client.Get(ctx, &pb.GetRequest{
		EntityType: "race_test",
		Id:         entityID,
	})
	require.NoError(t, err)
	assert.NotNil(t, getResp.Data)

	t.Logf("Successfully handled %d concurrent updates to same entity", concurrency)
}

// TestConcurrentBatchStress tests high-volume concurrent batch operations
func TestConcurrentBatchStress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Stress test: 50 concurrent workers, 20 entities each
	concurrency := 50
	batchSize := 20
	baseTimestamp := time.Now().UnixNano()

	startTime := time.Now()

	var wg sync.WaitGroup
	var successCount atomic.Int32
	var errorCount atomic.Int32
	var totalEntities atomic.Int32

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			requests := make([]*pb.PutRequest, batchSize)
			for j := 0; j < batchSize; j++ {
				entityID := uint64(baseTimestamp + int64(workerID*1000000) + int64(j*1000))
				requests[j] = &pb.PutRequest{
					EntityType: "stress_test",
					Id:         entityID,
					Data:       []byte(fmt.Sprintf(`{"worker":%d,"index":%d,"ts":%d}`, workerID, j, time.Now().UnixNano())),
				}
			}

			batchReq := &pb.BatchPutRequest{Requests: requests}
			resp, err := client.BatchPut(ctx, batchReq)
			if err != nil {
				errorCount.Add(1)
				return
			}

			if resp != nil && len(resp.Responses) == batchSize {
				successCount.Add(1)
				totalEntities.Add(int32(batchSize))
			} else {
				errorCount.Add(1)
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(startTime)

	totalSuccess := successCount.Load()
	totalErrors := errorCount.Load()
	entitiesCreated := totalEntities.Load()

	assert.GreaterOrEqual(t, totalSuccess, int32(concurrency*80/100),
		"At least 80% of stress test batches should succeed")

	throughput := float64(entitiesCreated) / duration.Seconds()

	t.Logf("Stress test results:")
	t.Logf("  Workers: %d", concurrency)
	t.Logf("  Batch size: %d", batchSize)
	t.Logf("  Successful batches: %d/%d", totalSuccess, concurrency)
	t.Logf("  Failed batches: %d", totalErrors)
	t.Logf("  Total entities: %d", entitiesCreated)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Throughput: %.2f entities/sec", throughput)
}

// TestConcurrentBatchWithRetries tests batch operations with retry logic
func TestConcurrentBatchWithRetries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	concurrency := 10
	batchSize := 5
	maxRetries := 3
	baseTimestamp := time.Now().UnixNano()

	var wg sync.WaitGroup
	var successCount atomic.Int32
	var retryCount atomic.Int32

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			requests := make([]*pb.PutRequest, batchSize)
			for j := 0; j < batchSize; j++ {
				entityID := uint64(baseTimestamp + int64(workerID*1000000) + int64(j*1000))
				requests[j] = &pb.PutRequest{
					EntityType: "retry_test",
					Id:         entityID,
					Data:       []byte(fmt.Sprintf(`{"worker":%d,"index":%d}`, workerID, j)),
				}
			}

			batchReq := &pb.BatchPutRequest{Requests: requests}

			// Retry logic
			var lastErr error
			for retry := 0; retry < maxRetries; retry++ {
				if retry > 0 {
					retryCount.Add(1)
					delay := time.Millisecond * 100 * time.Duration(retry)
					select {
					case <-time.After(delay):
					case <-ctx.Done():
						t.Logf("Worker %d context cancelled during retry backoff: %v", workerID, ctx.Err())
						return
					}
				}

				attemptCtx, attemptCancel := context.WithTimeout(ctx, 10*time.Second)
				resp, err := client.BatchPut(attemptCtx, batchReq)
				attemptCancel()

				if err == nil && resp != nil && len(resp.Responses) == batchSize {
					successCount.Add(1)
					return
				}

				lastErr = err
			}

			t.Logf("Worker %d failed after %d retries: %v", workerID, maxRetries, lastErr)
		}(i)
	}

	wg.Wait()

	totalSuccess := successCount.Load()
	totalRetries := retryCount.Load()

	assert.Equal(t, int32(concurrency), totalSuccess,
		"All concurrent batches should eventually succeed with retries")

	t.Logf("Successfully executed %d batch operations with %d total retries",
		concurrency, totalRetries)
}

// TestConcurrentBatchDeadlock tests for potential deadlock scenarios
func TestConcurrentBatchDeadlock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Create interlocking operations that could deadlock
	baseTimestamp := time.Now().UnixNano()
	entityA := uint64(baseTimestamp)
	entityB := uint64(baseTimestamp + 1000)

	var wg sync.WaitGroup
	doneChan := make(chan bool, 2)

	// Worker 1: A -> B
	wg.Add(1)
	go func() {
		defer wg.Done()

		req := &pb.BatchPutRequest{
			Requests: []*pb.PutRequest{
				{EntityType: "deadlock_test", Id: entityA, Data: []byte(`{"order":"A-first"}`)},
				{EntityType: "deadlock_test", Id: entityB, Data: []byte(`{"order":"B-second"}`)},
			},
		}

		_, err := client.BatchPut(ctx, req)
		if err == nil {
			doneChan <- true
		}
	}()

	// Worker 2: B -> A
	wg.Add(1)
	go func() {
		defer wg.Done()

		time.Sleep(time.Millisecond * 50) // Slight delay to create interleaving

		req := &pb.BatchPutRequest{
			Requests: []*pb.PutRequest{
				{EntityType: "deadlock_test", Id: entityB, Data: []byte(`{"order":"B-first"}`)},
				{EntityType: "deadlock_test", Id: entityA, Data: []byte(`{"order":"A-second"}`)},
			},
		}

		_, err := client.BatchPut(ctx, req)
		if err == nil {
			doneChan <- true
		}
	}()

	// Wait with timeout to detect deadlock
	go func() {
		wg.Wait()
		close(doneChan)
	}()

	completedCount := 0
	timeoutTimer := time.After(30 * time.Second)

	for {
		select {
		case <-doneChan:
			completedCount++
			if completedCount == 2 {
				t.Log("No deadlock detected - both operations completed successfully")
				return
			}
		case <-timeoutTimer:
			t.Fatal("Possible deadlock detected - operations did not complete in time")
		}
	}
}
