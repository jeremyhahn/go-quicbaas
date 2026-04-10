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

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/hash"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

type Stats struct {
	totalRequests   atomic.Uint64
	successRequests atomic.Uint64
	failedRequests  atomic.Uint64
	totalDuration   atomic.Int64 // nanoseconds
}

func (s *Stats) recordSuccess(duration time.Duration) {
	s.totalRequests.Add(1)
	s.successRequests.Add(1)
	s.totalDuration.Add(int64(duration))
}

func (s *Stats) recordFailure() {
	s.totalRequests.Add(1)
	s.failedRequests.Add(1)
}

func (s *Stats) snapshot() (total, success, failed uint64, avgDuration time.Duration) {
	total = s.totalRequests.Load()
	success = s.successRequests.Load()
	failed = s.failedRequests.Load()
	if success > 0 {
		avgDuration = time.Duration(s.totalDuration.Load() / int64(success))
	}
	return
}

func main() {
	// Command-line flags
	var (
		address        = flag.String("address", "localhost:50051", "QuicBaaS gRPC server address")
		duration       = flag.Duration("duration", 30*time.Second, "Test duration")
		pipelineSize   = flag.Int("pipeline", 100, "Number of requests per pipeline batch")
		workers        = flag.Int("workers", 10, "Number of concurrent worker goroutines")
		valueSize      = flag.Int("value-size", 100, "Size of value in bytes")
		reportInterval = flag.Duration("report-interval", 1*time.Second, "Stats reporting interval")
	)
	flag.Parse()

	fmt.Printf("Pipelined Load Test\n")
	fmt.Printf("===============================================================\n")
	fmt.Printf("Server:            %s\n", *address)
	fmt.Printf("Duration:          %v\n", *duration)
	fmt.Printf("Pipeline Size:     %d requests/batch\n", *pipelineSize)
	fmt.Printf("Workers:           %d\n", *workers)
	fmt.Printf("Value Size:        %d bytes\n", *valueSize)
	fmt.Printf("===============================================================\n\n")

	// Create SDK client
	client, err := quicbaas.NewClient(*address, quicbaas.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create client: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	// Test connectivity with a health check
	ctx := context.Background()
	healthResp, err := client.Health().Readiness(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Server not reachable: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Connected to server (status: %s)\n", healthResp.Status)

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Stats tracking
	stats := &Stats{}

	// Context with timeout
	testCtx, cancel := context.WithTimeout(ctx, *duration)
	defer cancel()

	// WaitGroup for workers
	var wg sync.WaitGroup

	// Start workers
	fmt.Printf("Starting %d workers with pipeline size %d...\n\n", *workers, *pipelineSize)

	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			runPipelinedWorker(testCtx, client, workerID, *pipelineSize, *valueSize, stats)
		}(i)
	}

	// Stats reporter
	ticker := time.NewTicker(*reportInterval)
	defer ticker.Stop()

	go func() {
		var lastTotal uint64
		lastTime := time.Now()

		for {
			select {
			case <-ticker.C:
				now := time.Now()
				elapsed := now.Sub(lastTime).Seconds()

				total, success, failed, avgDuration := stats.snapshot()

				// Calculate rates
				requestRate := float64(total-lastTotal) / elapsed

				fmt.Printf("[%6.1fs] Requests: %7d | Success: %7d | Failed: %4d | Rate: %7.1f req/s | Avg Latency: %6.2f ms\n",
					time.Since(lastTime).Seconds(),
					total,
					success,
					failed,
					requestRate,
					float64(avgDuration.Microseconds())/1000.0,
				)

				lastTotal = total
				lastTime = now

			case <-testCtx.Done():
				return
			}
		}
	}()

	// Wait for completion or signal
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		fmt.Println("\nTest completed")
	case <-sigChan:
		fmt.Println("\nInterrupted, shutting down...")
		cancel()
		wg.Wait()
	}

	// Final stats
	total, success, failed, avgDuration := stats.snapshot()
	actualDuration := *duration
	if testCtx.Err() != nil {
		actualDuration = time.Since(time.Now().Add(-*duration))
	}

	fmt.Printf("\n===============================================================\n")
	fmt.Printf("Final Results\n")
	fmt.Printf("===============================================================\n")
	fmt.Printf("Total Requests:    %d\n", total)
	fmt.Printf("Successful:        %d (%.2f%%)\n", success, 100.0*float64(success)/float64(total))
	fmt.Printf("Failed:            %d (%.2f%%)\n", failed, 100.0*float64(failed)/float64(total))
	fmt.Printf("Avg Latency:       %.2f ms\n", float64(avgDuration.Microseconds())/1000.0)
	fmt.Printf("Throughput:        %.2f req/s\n", float64(total)/actualDuration.Seconds())
	fmt.Printf("===============================================================\n")
}

func runPipelinedWorker(ctx context.Context, client quicbaas.Client, workerID, pipelineSize, valueSize int, stats *Stats) {
	// Pre-allocate value buffer
	value := make([]byte, valueSize)
	for i := range value {
		value[i] = byte('a' + (i % 26))
	}

	// Channel for pipeline results
	type result struct {
		success  bool
		duration time.Duration
		err      error
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Execute pipelined batch using concurrent requests
			resultsChan := make(chan result, pipelineSize)
			var pipelineWg sync.WaitGroup

			for i := 0; i < pipelineSize; i++ {
				pipelineWg.Add(1)
				go func(reqID int) {
					defer pipelineWg.Done()

					key := fmt.Sprintf("worker%d-key%d-%d", workerID, reqID, rand.Int63())
					start := time.Now()

					_, err := client.KV().Put(ctx, &service.PutRequest{
						EntityType:  "kv",
						ID:          hash.FNV1a(key),
						Data:        value,
						Consistency: service.ConsistencyLevelLinearizable,
					})

					resultsChan <- result{
						success:  err == nil,
						duration: time.Since(start),
						err:      err,
					}
				}(i)
			}

			// Wait for all pipeline requests to complete
			go func() {
				pipelineWg.Wait()
				close(resultsChan)
			}()

			// Collect results
			for r := range resultsChan {
				if r.success {
					stats.recordSuccess(r.duration)
				} else {
					stats.recordFailure()
					// Log first few errors for debugging
					if stats.failedRequests.Load() < 5 && r.err != nil {
						fmt.Fprintf(os.Stderr, "Error: %v\n", r.err)
					}
				}
			}
		}
	}
}
