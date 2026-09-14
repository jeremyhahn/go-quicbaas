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

package scheduler

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// benchmarkStorage is an optimized mock storage for benchmarks.
type benchmarkStorage struct {
	mu      sync.RWMutex
	jobs    map[uint64]*Job
	nextID  atomic.Uint64
	claimMu sync.Mutex
	claims  map[firingClaimKey]struct{}
}

func newBenchmarkStorage() *benchmarkStorage {
	return &benchmarkStorage{
		jobs:   make(map[uint64]*Job),
		claims: make(map[firingClaimKey]struct{}),
	}
}

// ClaimFiring keeps benchmarkStorage a complete JobStorage. The dispatch path
// takes a claim per firing, so a benchmark measuring dispatch must pay that
// cost too — a mock that granted every claim for free would understate the
// scheduler's real per-firing work.
func (b *benchmarkStorage) ClaimFiring(_ context.Context, claim *FiringClaim) (bool, error) {
	key := firingClaimKey{jobID: claim.JobID, firingNanos: claim.FiringTime.UnixNano()}

	b.claimMu.Lock()
	defer b.claimMu.Unlock()
	if _, held := b.claims[key]; held {
		return false, nil
	}
	b.claims[key] = struct{}{}
	return true, nil
}

func (b *benchmarkStorage) CreateJob(ctx context.Context, job *Job) error {
	id := b.nextID.Add(1)
	job.ID = id

	b.mu.Lock()
	b.jobs[id] = job
	b.mu.Unlock()

	return nil
}

func (b *benchmarkStorage) GetJob(ctx context.Context, id uint64, tenantID string) (*Job, error) {
	b.mu.RLock()
	job := b.jobs[id]
	b.mu.RUnlock()
	return job, nil
}

func (b *benchmarkStorage) UpdateJob(ctx context.Context, job *Job) error {
	b.mu.Lock()
	b.jobs[job.ID] = job
	b.mu.Unlock()
	return nil
}

func (b *benchmarkStorage) DeleteJob(ctx context.Context, id uint64, tenantID string) error {
	b.mu.Lock()
	delete(b.jobs, id)
	b.mu.Unlock()
	return nil
}

func (b *benchmarkStorage) ListJobs(ctx context.Context, query *JobQuery) ([]*Job, string, error) {
	b.mu.RLock()
	result := make([]*Job, 0, len(b.jobs))
	for _, job := range b.jobs {
		result = append(result, job)
	}
	b.mu.RUnlock()
	return result, "", nil
}

func (b *benchmarkStorage) GetJobsDueForExecution(ctx context.Context, before time.Time, limit int) ([]*Job, error) {
	b.mu.RLock()
	result := make([]*Job, 0, limit)
	for _, job := range b.jobs {
		if job.Status == JobStatusPending && (job.NextRun.Before(before) || job.NextRun.Equal(before)) {
			result = append(result, job)
			if len(result) >= limit {
				break
			}
		}
	}
	b.mu.RUnlock()
	return result, nil
}

func (b *benchmarkStorage) SaveExecution(ctx context.Context, exec *JobExecution) error {
	return nil
}

func (b *benchmarkStorage) GetExecutions(ctx context.Context, jobID uint64, tenantID string, limit int) ([]*JobExecution, error) {
	return nil, nil
}

// noopHandler is a minimal handler that does nothing for benchmarking overhead.
func noopHandler(ctx context.Context, job *Job) (*JobResult, error) {
	return &JobResult{Success: true, Message: "ok"}, nil
}

// BenchmarkScheduleJob measures the throughput of scheduling new jobs.
func BenchmarkScheduleJob(b *testing.B) {
	storage := newBenchmarkStorage()
	config := &SchedulerConfig{
		TickInterval:      time.Second,
		MaxConcurrentJobs: 100,
		DefaultTimeout:    time.Minute,
		DefaultRetries:    3,
		RetryBackoff:      time.Second,
	}
	scheduler := New(config, storage)

	ctx := context.Background()
	err := scheduler.Start(ctx)
	if err != nil {
		b.Fatalf("failed to start scheduler: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		scheduler.Stop(stopCtx)
	}()

	err = scheduler.RegisterHandler("benchmark", noopHandler)
	if err != nil {
		b.Fatalf("failed to register handler: %v", err)
	}

	payload := json.RawMessage(`{"test": true}`)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		job := &Job{
			Name:      "benchmark-job",
			Type:      JobTypeOneShot,
			Handler:   "benchmark",
			Payload:   payload,
			ExecuteAt: time.Now().Add(time.Hour), // Future to prevent execution during benchmark
		}
		if err := scheduler.ScheduleJob(ctx, job); err != nil {
			b.Fatalf("failed to schedule job: %v", err)
		}
	}
}

// BenchmarkScheduleJobParallel measures parallel job scheduling throughput.
func BenchmarkScheduleJobParallel(b *testing.B) {
	storage := newBenchmarkStorage()
	config := &SchedulerConfig{
		TickInterval:      time.Second,
		MaxConcurrentJobs: 100,
		DefaultTimeout:    time.Minute,
		DefaultRetries:    3,
		RetryBackoff:      time.Second,
	}
	scheduler := New(config, storage)

	ctx := context.Background()
	err := scheduler.Start(ctx)
	if err != nil {
		b.Fatalf("failed to start scheduler: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		scheduler.Stop(stopCtx)
	}()

	err = scheduler.RegisterHandler("benchmark", noopHandler)
	if err != nil {
		b.Fatalf("failed to register handler: %v", err)
	}

	payload := json.RawMessage(`{"test": true}`)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			job := &Job{
				Name:      "benchmark-job",
				Type:      JobTypeOneShot,
				Handler:   "benchmark",
				Payload:   payload,
				ExecuteAt: time.Now().Add(time.Hour),
			}
			if err := scheduler.ScheduleJob(ctx, job); err != nil {
				b.Errorf("failed to schedule job: %v", err)
			}
		}
	})
}

// BenchmarkGetJob measures job retrieval performance.
func BenchmarkGetJob(b *testing.B) {
	storage := newBenchmarkStorage()
	config := &SchedulerConfig{
		TickInterval:      time.Second,
		MaxConcurrentJobs: 100,
		DefaultTimeout:    time.Minute,
		DefaultRetries:    3,
		RetryBackoff:      time.Second,
	}
	scheduler := New(config, storage)

	ctx := context.Background()
	err := scheduler.Start(ctx)
	if err != nil {
		b.Fatalf("failed to start scheduler: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		scheduler.Stop(stopCtx)
	}()

	err = scheduler.RegisterHandler("benchmark", noopHandler)
	if err != nil {
		b.Fatalf("failed to register handler: %v", err)
	}

	// Create a job to retrieve
	job := &Job{
		Name:      "benchmark-job",
		Type:      JobTypeOneShot,
		Handler:   "benchmark",
		Payload:   json.RawMessage(`{"test": true}`),
		ExecuteAt: time.Now().Add(time.Hour),
	}
	if err := scheduler.ScheduleJob(ctx, job); err != nil {
		b.Fatalf("failed to schedule job: %v", err)
	}

	jobID := job.ID

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := scheduler.GetJob(ctx, jobID)
		if err != nil {
			b.Fatalf("failed to get job: %v", err)
		}
	}
}

// BenchmarkRegisterHandler measures handler registration performance.
func BenchmarkRegisterHandler(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		storage := newBenchmarkStorage()
		config := DefaultConfig()
		scheduler := New(config, storage)
		b.StartTimer()

		for j := 0; j < 100; j++ {
			name := "handler-" + string(rune('a'+j))
			_ = scheduler.RegisterHandler(name, noopHandler)
		}
	}
}

// BenchmarkHandlerLookup measures handler dispatch performance.
func BenchmarkHandlerLookup(b *testing.B) {
	storage := newBenchmarkStorage()
	config := DefaultConfig()
	scheduler := New(config, storage)

	// Register many handlers to test map performance
	for i := 0; i < 100; i++ {
		name := "handler-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		_ = scheduler.RegisterHandler(name, noopHandler)
	}

	// Register the handler we'll look up
	_ = scheduler.RegisterHandler("target-handler", noopHandler)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		handler, ok := scheduler.getHandler("target-handler")
		if !ok || handler == nil {
			b.Fatal("handler not found")
		}
	}
}

// BenchmarkHandlerLookupParallel measures concurrent handler dispatch.
func BenchmarkHandlerLookupParallel(b *testing.B) {
	storage := newBenchmarkStorage()
	config := DefaultConfig()
	scheduler := New(config, storage)

	// Register many handlers
	for i := 0; i < 100; i++ {
		name := "handler-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		_ = scheduler.RegisterHandler(name, noopHandler)
	}

	_ = scheduler.RegisterHandler("target-handler", noopHandler)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			handler, ok := scheduler.getHandler("target-handler")
			if !ok || handler == nil {
				b.Error("handler not found")
			}
		}
	})
}

// BenchmarkStats measures statistics retrieval performance.
func BenchmarkStats(b *testing.B) {
	storage := newBenchmarkStorage()
	config := DefaultConfig()
	scheduler := New(config, storage)

	ctx := context.Background()
	_ = scheduler.Start(ctx)
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		scheduler.Stop(stopCtx)
	}()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = scheduler.Stats()
	}
}

// BenchmarkStatsParallel measures concurrent statistics retrieval.
func BenchmarkStatsParallel(b *testing.B) {
	storage := newBenchmarkStorage()
	config := DefaultConfig()
	scheduler := New(config, storage)

	ctx := context.Background()
	_ = scheduler.Start(ctx)
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		scheduler.Stop(stopCtx)
	}()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = scheduler.Stats()
		}
	})
}

// BenchmarkListJobs measures job listing performance with varying job counts.
func BenchmarkListJobs(b *testing.B) {
	jobCounts := []int{10, 100, 1000}

	for _, count := range jobCounts {
		b.Run(
			"jobs="+string(rune('0'+count/1000%10))+string(rune('0'+count/100%10))+string(rune('0'+count/10%10))+string(rune('0'+count%10)),
			func(b *testing.B) {
				storage := newBenchmarkStorage()
				config := DefaultConfig()
				scheduler := New(config, storage)

				ctx := context.Background()
				_ = scheduler.Start(ctx)
				defer func() {
					stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					scheduler.Stop(stopCtx)
				}()

				_ = scheduler.RegisterHandler("benchmark", noopHandler)

				// Pre-populate jobs
				for i := 0; i < count; i++ {
					job := &Job{
						Name:      "benchmark-job",
						Type:      JobTypeOneShot,
						Handler:   "benchmark",
						Payload:   json.RawMessage(`{}`),
						ExecuteAt: time.Now().Add(time.Hour),
					}
					_ = scheduler.ScheduleJob(ctx, job)
				}

				query := &JobQuery{}

				b.ResetTimer()
				b.ReportAllocs()

				for i := 0; i < b.N; i++ {
					_, err := scheduler.ListJobs(ctx, query)
					if err != nil {
						b.Fatalf("failed to list jobs: %v", err)
					}
				}
			},
		)
	}
}

// BenchmarkCronParsing measures cron schedule parsing performance.
func BenchmarkCronParsing(b *testing.B) {
	storage := newBenchmarkStorage()
	config := DefaultConfig()
	scheduler := New(config, storage)

	schedules := []string{
		"*/5 * * * *",     // Every 5 minutes
		"0 */2 * * *",     // Every 2 hours
		"0 0 * * *",       // Daily at midnight
		"0 0 * * 0",       // Weekly on Sunday
		"0 0 1 * *",       // Monthly on the 1st
		"@hourly",         // Descriptor
		"@daily",          // Descriptor
		"30 4 1,15 * 1-5", // Complex expression
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		schedule := schedules[i%len(schedules)]
		_, err := scheduler.cronParser.Parse(schedule)
		if err != nil {
			b.Fatalf("failed to parse cron: %v", err)
		}
	}
}

// BenchmarkJobValidation measures job validation performance.
func BenchmarkJobValidation(b *testing.B) {
	storage := newBenchmarkStorage()
	config := DefaultConfig()
	scheduler := New(config, storage)

	_ = scheduler.RegisterHandler("benchmark", noopHandler)

	job := &Job{
		Name:     "benchmark-job",
		Type:     JobTypeRecurring,
		Handler:  "benchmark",
		Payload:  json.RawMessage(`{"data": "test"}`),
		Schedule: "0 0 * * *",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = scheduler.validateJob(job)
	}
}

// BenchmarkAtomicCounters measures atomic counter operations.
func BenchmarkAtomicCounters(b *testing.B) {
	storage := newBenchmarkStorage()
	config := DefaultConfig()
	scheduler := New(config, storage)

	ctx := context.Background()
	_ = scheduler.Start(ctx)
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		scheduler.Stop(stopCtx)
	}()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			scheduler.jobCount.Add(1)
			scheduler.successCount.Add(1)
			scheduler.failureCount.Add(1)
			_ = scheduler.jobCount.Load()
			_ = scheduler.successCount.Load()
			_ = scheduler.failureCount.Load()
		}
	})
}

// BenchmarkSemaphoreAcquisition measures semaphore performance.
func BenchmarkSemaphoreAcquisition(b *testing.B) {
	semaphore := make(chan struct{}, 100)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Acquire
		semaphore <- struct{}{}
		// Release
		<-semaphore
	}
}

// BenchmarkSemaphoreAcquisitionParallel measures concurrent semaphore performance.
func BenchmarkSemaphoreAcquisitionParallel(b *testing.B) {
	semaphore := make(chan struct{}, 100)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			semaphore <- struct{}{}
			<-semaphore
		}
	})
}

// BenchmarkJobCreationAllocation measures memory allocations for job creation.
func BenchmarkJobCreationAllocation(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		job := &Job{
			Name:        "benchmark-job",
			Description: "A benchmark job for testing",
			Type:        JobTypeOneShot,
			Handler:     "benchmark",
			Payload:     json.RawMessage(`{"key": "value", "number": 42}`),
			Labels: map[string]string{
				"env":      "benchmark",
				"priority": "high",
			},
			Priority:   1,
			Timeout:    time.Minute,
			MaxRetries: 3,
		}
		_ = job
	}
}

// BenchmarkContextOperations measures context creation and cancellation.
func BenchmarkContextOperations(b *testing.B) {
	parentCtx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(parentCtx, time.Minute)
		cancel()
		_ = ctx
	}
}
