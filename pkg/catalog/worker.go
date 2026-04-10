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

package catalog

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Worker is the interface for background workers that process async tasks.
type Worker interface {
	// Start begins the worker's processing loop.
	Start(ctx context.Context) error

	// Stop gracefully stops the worker and waits for current tasks to complete.
	Stop() error

	// Name returns the worker's unique identifier.
	Name() string

	// IsRunning returns true if the worker is currently running.
	IsRunning() bool

	// Metrics returns the worker's current statistics.
	Metrics() *WorkerMetrics
}

// WorkerMetrics tracks worker statistics using atomic operations for thread safety.
type WorkerMetrics struct {
	// TasksProcessed is the total number of successfully processed tasks.
	TasksProcessed atomic.Int64

	// TasksFailed is the total number of failed task processing attempts.
	TasksFailed atomic.Int64

	// TasksRetried is the total number of retried task processing attempts.
	TasksRetried atomic.Int64

	// LastTaskTime is the Unix nanosecond timestamp of the last processed task.
	LastTaskTime atomic.Int64

	// CurrentTasks is the number of tasks currently being processed.
	CurrentTasks atomic.Int64

	// TotalProcessingTime is the cumulative processing time in nanoseconds.
	TotalProcessingTime atomic.Int64
}

// Snapshot returns a point-in-time copy of the metrics.
func (m *WorkerMetrics) Snapshot() WorkerMetricsSnapshot {
	return WorkerMetricsSnapshot{
		TasksProcessed:      m.TasksProcessed.Load(),
		TasksFailed:         m.TasksFailed.Load(),
		TasksRetried:        m.TasksRetried.Load(),
		LastTaskTime:        m.LastTaskTime.Load(),
		CurrentTasks:        m.CurrentTasks.Load(),
		TotalProcessingTime: m.TotalProcessingTime.Load(),
	}
}

// AverageProcessingTime returns the average processing time per task.
func (m *WorkerMetrics) AverageProcessingTime() time.Duration {
	processed := m.TasksProcessed.Load()
	if processed == 0 {
		return 0
	}
	return time.Duration(m.TotalProcessingTime.Load() / processed)
}

// WorkerMetricsSnapshot is a point-in-time copy of WorkerMetrics.
type WorkerMetricsSnapshot struct {
	TasksProcessed      int64
	TasksFailed         int64
	TasksRetried        int64
	LastTaskTime        int64
	CurrentTasks        int64
	TotalProcessingTime int64
}

// WorkerConfig configures a background worker.
type WorkerConfig struct {
	// Workers is the number of concurrent worker goroutines.
	Workers int

	// BatchSize is the maximum number of tasks to dequeue per poll.
	BatchSize int

	// PollInterval is the time between queue polling attempts.
	PollInterval time.Duration

	// MaxRetries is the maximum number of retry attempts for failed tasks.
	MaxRetries int

	// RetryDelay is the initial delay between retry attempts.
	RetryDelay time.Duration

	// RetryBackoffMultiplier multiplies the retry delay on each attempt.
	RetryBackoffMultiplier float64

	// TaskTimeout is the maximum time allowed for a single task.
	TaskTimeout time.Duration
}

// DefaultWorkerConfig returns sensible default configuration for workers.
func DefaultWorkerConfig() *WorkerConfig {
	return &WorkerConfig{
		Workers:                2,
		BatchSize:              10,
		PollInterval:           time.Second,
		MaxRetries:             3,
		RetryDelay:             100 * time.Millisecond,
		RetryBackoffMultiplier: 2.0,
		TaskTimeout:            30 * time.Second,
	}
}

// Validate validates the worker configuration.
func (c *WorkerConfig) Validate() error {
	if c == nil {
		return ErrWorkerConfigNil
	}
	if c.Workers <= 0 {
		return ErrInvalidWorkerCount
	}
	if c.BatchSize <= 0 {
		return ErrInvalidBatchSize
	}
	if c.PollInterval <= 0 {
		return ErrInvalidPollInterval
	}
	if c.MaxRetries < 0 {
		return ErrInvalidMaxRetries
	}
	if c.RetryDelay < 0 {
		return ErrInvalidRetryDelay
	}
	if c.RetryBackoffMultiplier < 1.0 {
		return ErrInvalidBackoffMultiplier
	}
	if c.TaskTimeout <= 0 {
		return ErrInvalidTaskTimeout
	}
	return nil
}

// Clone creates a deep copy of the worker configuration.
func (c *WorkerConfig) Clone() *WorkerConfig {
	if c == nil {
		return nil
	}
	return &WorkerConfig{
		Workers:                c.Workers,
		BatchSize:              c.BatchSize,
		PollInterval:           c.PollInterval,
		MaxRetries:             c.MaxRetries,
		RetryDelay:             c.RetryDelay,
		RetryBackoffMultiplier: c.RetryBackoffMultiplier,
		TaskTimeout:            c.TaskTimeout,
	}
}

// BaseWorker provides common functionality for all background workers.
type BaseWorker struct {
	name    string
	config  *WorkerConfig
	metrics *WorkerMetrics
	logger  interfaces.Logger

	running atomic.Bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewBaseWorker creates a new base worker with the given configuration.
func NewBaseWorker(name string, config *WorkerConfig, logger interfaces.Logger) (*BaseWorker, error) {
	if name == "" {
		return nil, ErrWorkerNameEmpty
	}
	if config == nil {
		config = DefaultWorkerConfig()
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &BaseWorker{
		name:    name,
		config:  config.Clone(),
		metrics: &WorkerMetrics{},
		logger:  logger,
	}, nil
}

// Name returns the worker's unique identifier.
func (w *BaseWorker) Name() string {
	return w.name
}

// IsRunning returns true if the worker is currently running.
func (w *BaseWorker) IsRunning() bool {
	return w.running.Load()
}

// Metrics returns the worker's current statistics.
func (w *BaseWorker) Metrics() *WorkerMetrics {
	return w.metrics
}

// Config returns the worker's configuration.
func (w *BaseWorker) Config() *WorkerConfig {
	return w.config
}

// Logger returns the worker's logger.
func (w *BaseWorker) Logger() interfaces.Logger {
	return w.logger
}

// Context returns the worker's context.
func (w *BaseWorker) Context() context.Context {
	return w.ctx
}

// WaitGroup returns the worker's wait group for coordination.
func (w *BaseWorker) WaitGroup() *sync.WaitGroup {
	return &w.wg
}

// StartBase initializes the worker's context and marks it as running.
// Returns an error if the worker is already running.
func (w *BaseWorker) StartBase(ctx context.Context) error {
	if !w.running.CompareAndSwap(false, true) {
		return NewWorkerAlreadyRunningError(w.name)
	}

	w.ctx, w.cancel = context.WithCancel(ctx)

	if w.logger != nil {
		w.logger.WithField("worker", w.name).Info("worker started")
	}

	return nil
}

// StopBase signals the worker to stop and waits for goroutines to finish.
func (w *BaseWorker) StopBase() error {
	if !w.running.CompareAndSwap(true, false) {
		return nil // Already stopped
	}

	if w.cancel != nil {
		w.cancel()
	}

	w.wg.Wait()

	if w.logger != nil {
		w.logger.WithField("worker", w.name).Info("worker stopped")
	}

	return nil
}

// RecordTaskStart records the start of a task processing.
func (w *BaseWorker) RecordTaskStart() {
	w.metrics.CurrentTasks.Add(1)
}

// RecordTaskComplete records successful task completion with processing time.
func (w *BaseWorker) RecordTaskComplete(processingTime time.Duration) {
	w.metrics.CurrentTasks.Add(-1)
	w.metrics.TasksProcessed.Add(1)
	w.metrics.LastTaskTime.Store(time.Now().UnixNano())
	w.metrics.TotalProcessingTime.Add(int64(processingTime))
}

// RecordTaskFailed records a failed task processing.
func (w *BaseWorker) RecordTaskFailed() {
	w.metrics.CurrentTasks.Add(-1)
	w.metrics.TasksFailed.Add(1)
	w.metrics.LastTaskTime.Store(time.Now().UnixNano())
}

// RecordTaskRetry records a task retry.
func (w *BaseWorker) RecordTaskRetry() {
	w.metrics.TasksRetried.Add(1)
}

// ShouldContinue returns true if the worker should continue processing.
func (w *BaseWorker) ShouldContinue() bool {
	select {
	case <-w.ctx.Done():
		return false
	default:
		return w.running.Load()
	}
}

// Sleep sleeps for the poll interval, returning early if context is cancelled.
func (w *BaseWorker) Sleep() {
	select {
	case <-w.ctx.Done():
		return
	case <-time.After(w.config.PollInterval):
		return
	}
}

// CalculateRetryDelay calculates the retry delay for the given attempt number.
func (w *BaseWorker) CalculateRetryDelay(attempt int) time.Duration {
	delay := w.config.RetryDelay
	for i := 0; i < attempt; i++ {
		delay = time.Duration(float64(delay) * w.config.RetryBackoffMultiplier)
	}
	return delay
}

// CreateTaskContext creates a context with the configured task timeout.
func (w *BaseWorker) CreateTaskContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(w.ctx, w.config.TaskTimeout) // #nosec G118 -- cancel returned to caller who defers it
}
