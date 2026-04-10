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
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Re-encryption error codes.
const (
	// ErrCodeReencryptFailed indicates re-encryption failed.
	ErrCodeReencryptFailed ErrorCode = "REENCRYPT_FAILED"

	// ErrCodeReencryptTaskInvalid indicates an invalid re-encryption task.
	ErrCodeReencryptTaskInvalid ErrorCode = "REENCRYPT_TASK_INVALID"
)

// Re-encryption errors.
var (
	// ErrEncryptionManagerNil indicates a nil encryption manager was provided.
	ErrEncryptionManagerNil = errors.New("encryption manager cannot be nil")

	// ErrReencryptTaskNil indicates a nil re-encryption task was provided.
	ErrReencryptTaskNil = errors.New("re-encryption task cannot be nil")

	// ErrOldKeyIDEmpty indicates an empty old key ID was provided.
	ErrOldKeyIDEmpty = errors.New("old key ID cannot be empty")

	// ErrNewKeyIDEmpty indicates an empty new key ID was provided.
	ErrNewKeyIDEmpty = errors.New("new key ID cannot be empty")
)

// ReencryptBackendError is returned when a backend operation fails during re-encryption.
type ReencryptBackendError struct {
	Operation string
	Store     string
	Cause     error
}

// Error implements the error interface.
func (e *ReencryptBackendError) Error() string {
	msg := e.Operation + " " + e.Store
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying cause.
func (e *ReencryptBackendError) Unwrap() error {
	return e.Cause
}

// ReencryptDataError is returned when a data operation fails during re-encryption.
type ReencryptDataError struct {
	Operation string
	Cause     error
}

// Error implements the error interface.
func (e *ReencryptDataError) Error() string {
	msg := e.Operation
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying cause.
func (e *ReencryptDataError) Unwrap() error {
	return e.Cause
}

// ReencryptSizeExceededError is returned when object size exceeds the configured limit.
type ReencryptSizeExceededError struct {
	Size  int64
	Limit int64
}

// Error implements the error interface.
func (e *ReencryptSizeExceededError) Error() string {
	return "object size " + strconv.FormatInt(e.Size, 10) + " exceeds max " + strconv.FormatInt(e.Limit, 10)
}

// NewReencryptError creates a new error for re-encryption failure.
func NewReencryptError(key string, cause error) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeReencryptFailed,
		Message: "re-encryption failed",
		Key:     key,
		Cause:   cause,
	}
}

// NewReencryptTaskInvalidError creates a new error for invalid re-encryption task.
func NewReencryptTaskInvalidError(reason string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeReencryptTaskInvalid,
		Message: "invalid re-encryption task: " + reason,
	}
}

// IsReencryptError returns true if the error is a re-encryption error.
func IsReencryptError(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		return ce.Code == ErrCodeReencryptFailed || ce.Code == ErrCodeReencryptTaskInvalid
	}
	return false
}

// ReencryptTask represents a re-encryption operation for key rotation.
type ReencryptTask struct {
	// Key is the catalog entry key to re-encrypt.
	Key string

	// OldKeyID is the current encryption key ID.
	OldKeyID string

	// NewKeyID is the new encryption key ID.
	NewKeyID string

	// Stores is an optional list of specific stores to re-encrypt.
	// If empty, all locations using the old key will be re-encrypted.
	Stores []string

	// Priority is the task priority (higher = more urgent).
	Priority int

	// CreatedAt is when the task was created.
	CreatedAt int64

	// Attempts is the number of processing attempts.
	Attempts int

	// LastError is the error from the last attempt.
	LastError string
}

// Clone creates a deep copy of the re-encryption task.
func (t *ReencryptTask) Clone() *ReencryptTask {
	if t == nil {
		return nil
	}

	clone := &ReencryptTask{
		Key:       t.Key,
		OldKeyID:  t.OldKeyID,
		NewKeyID:  t.NewKeyID,
		Priority:  t.Priority,
		CreatedAt: t.CreatedAt,
		Attempts:  t.Attempts,
		LastError: t.LastError,
	}

	if t.Stores != nil {
		clone.Stores = make([]string, len(t.Stores))
		copy(clone.Stores, t.Stores)
	}

	return clone
}

// Validate validates the re-encryption task.
func (t *ReencryptTask) Validate() error {
	if t == nil {
		return ErrReencryptTaskNil
	}
	if t.Key == "" {
		return NewReencryptTaskInvalidError("key is required")
	}
	if t.OldKeyID == "" {
		return NewReencryptTaskInvalidError("old key ID is required")
	}
	if t.NewKeyID == "" {
		return NewReencryptTaskInvalidError("new key ID is required")
	}
	if t.OldKeyID == t.NewKeyID {
		return NewReencryptTaskInvalidError("old and new key IDs must be different")
	}
	return nil
}

// ReencryptProgress tracks the progress of a re-encryption operation.
type ReencryptProgress struct {
	// TotalTasks is the total number of tasks.
	TotalTasks atomic.Int64

	// CompletedTasks is the number of completed tasks.
	CompletedTasks atomic.Int64

	// FailedTasks is the number of failed tasks.
	FailedTasks atomic.Int64

	// BytesProcessed is the total bytes re-encrypted.
	BytesProcessed atomic.Int64

	// StartTime is when the operation started.
	StartTime int64

	// LastUpdateTime is when the progress was last updated.
	LastUpdateTime atomic.Int64
}

// Snapshot returns a point-in-time snapshot of the progress.
func (p *ReencryptProgress) Snapshot() ReencryptProgressSnapshot {
	return ReencryptProgressSnapshot{
		TotalTasks:     p.TotalTasks.Load(),
		CompletedTasks: p.CompletedTasks.Load(),
		FailedTasks:    p.FailedTasks.Load(),
		BytesProcessed: p.BytesProcessed.Load(),
		StartTime:      p.StartTime,
		LastUpdateTime: p.LastUpdateTime.Load(),
	}
}

// PercentComplete returns the percentage of tasks completed.
func (p *ReencryptProgress) PercentComplete() float64 {
	total := p.TotalTasks.Load()
	if total == 0 {
		return 0
	}
	completed := p.CompletedTasks.Load()
	return float64(completed) / float64(total) * 100
}

// ReencryptProgressSnapshot is a point-in-time snapshot of ReencryptProgress.
type ReencryptProgressSnapshot struct {
	TotalTasks     int64
	CompletedTasks int64
	FailedTasks    int64
	BytesProcessed int64
	StartTime      int64
	LastUpdateTime int64
}

// ReencryptWorkerConfig extends WorkerConfig with re-encryption specific options.
type ReencryptWorkerConfig struct {
	*WorkerConfig

	// MaxBytesPerTask is the maximum bytes to process per task.
	MaxBytesPerTask int64

	// VerifyAfterReencrypt enables verification after re-encryption.
	VerifyAfterReencrypt bool
}

// DefaultReencryptWorkerConfig returns sensible defaults for the re-encryption worker.
func DefaultReencryptWorkerConfig() *ReencryptWorkerConfig {
	return &ReencryptWorkerConfig{
		WorkerConfig:         DefaultWorkerConfig(),
		MaxBytesPerTask:      100 * 1024 * 1024, // 100MB
		VerifyAfterReencrypt: true,
	}
}

// Validate validates the re-encryption worker configuration.
func (c *ReencryptWorkerConfig) Validate() error {
	if c == nil {
		return ErrWorkerConfigNil
	}
	if c.WorkerConfig == nil {
		c.WorkerConfig = DefaultWorkerConfig()
	}
	if err := c.WorkerConfig.Validate(); err != nil {
		return err
	}
	if c.MaxBytesPerTask < 0 {
		return NewReencryptTaskInvalidError("max bytes per task cannot be negative")
	}
	return nil
}

// ReencryptWorker handles re-encryption when keys are rotated.
type ReencryptWorker struct {
	*BaseWorker
	catalog    CatalogManager
	backends   BackendManager
	encryption EncryptionManager
	config     *ReencryptWorkerConfig
	taskQueue  chan *ReencryptTask
	progress   *ReencryptProgress
}

// NewReencryptWorker creates a new re-encryption worker.
func NewReencryptWorker(
	catalog CatalogManager,
	backends BackendManager,
	encryption EncryptionManager,
	cfg *ReencryptWorkerConfig,
	logger interfaces.Logger,
) (*ReencryptWorker, error) {
	if catalog == nil {
		return nil, ErrCatalogManagerNil
	}
	if backends == nil {
		return nil, ErrBackendManagerNilForWorker
	}
	if encryption == nil {
		return nil, ErrEncryptionManagerNil
	}
	if cfg == nil {
		cfg = DefaultReencryptWorkerConfig()
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	base, err := NewBaseWorker("reencrypt", cfg.WorkerConfig, logger)
	if err != nil {
		return nil, err
	}

	return &ReencryptWorker{
		BaseWorker: base,
		catalog:    catalog,
		backends:   backends,
		encryption: encryption,
		config:     cfg,
		taskQueue:  make(chan *ReencryptTask, cfg.BatchSize*cfg.Workers),
		progress:   &ReencryptProgress{},
	}, nil
}

// Start begins the worker's processing loop.
func (w *ReencryptWorker) Start(ctx context.Context) error {
	if err := w.StartBase(ctx); err != nil {
		return err
	}

	w.progress.StartTime = time.Now().UnixNano()

	// Start worker goroutines
	for i := 0; i < w.Config().Workers; i++ {
		w.WaitGroup().Add(1)
		go w.processLoop(i)
	}

	return nil
}

// Stop gracefully stops the worker.
func (w *ReencryptWorker) Stop() error {
	return w.StopBase()
}

// Enqueue adds a re-encryption task to the queue.
func (w *ReencryptWorker) Enqueue(task *ReencryptTask) error {
	if !w.IsRunning() {
		return NewWorkerNotRunningError(w.Name())
	}

	if err := task.Validate(); err != nil {
		return err
	}

	// Set creation time if not set
	if task.CreatedAt == 0 {
		task.CreatedAt = time.Now().UnixNano()
	}

	w.progress.TotalTasks.Add(1)

	select {
	case w.taskQueue <- task:
		return nil
	default:
		w.progress.TotalTasks.Add(-1)
		return NewQueueFullError("reencrypt")
	}
}

// EnqueueBulk adds multiple re-encryption tasks for a key rotation.
func (w *ReencryptWorker) EnqueueBulk(oldKeyID, newKeyID string, keys []string, priority int) error {
	if !w.IsRunning() {
		return NewWorkerNotRunningError(w.Name())
	}

	if oldKeyID == "" {
		return ErrOldKeyIDEmpty
	}
	if newKeyID == "" {
		return ErrNewKeyIDEmpty
	}

	for _, key := range keys {
		task := &ReencryptTask{
			Key:       key,
			OldKeyID:  oldKeyID,
			NewKeyID:  newKeyID,
			Priority:  priority,
			CreatedAt: time.Now().UnixNano(),
		}

		if err := w.Enqueue(task); err != nil {
			// Continue with other keys even if queue is full
			if IsQueueFull(err) {
				if w.Logger() != nil {
					w.Logger().WithField("key", key).Warn("failed to enqueue re-encryption task: queue full")
				}
				continue
			}
			return err
		}
	}

	return nil
}

// Progress returns the current re-encryption progress.
func (w *ReencryptWorker) Progress() *ReencryptProgress {
	return w.progress
}

// processLoop is the main processing loop for a worker goroutine.
func (w *ReencryptWorker) processLoop(workerID int) {
	defer w.WaitGroup().Done()

	logger := w.Logger()
	if logger != nil {
		logger = logger.WithField("worker_id", workerID)
	}

	for {
		select {
		case <-w.Context().Done():
			return
		case task := <-w.taskQueue:
			if task == nil {
				continue
			}

			w.RecordTaskStart()
			start := time.Now()

			err := w.processTask(task)
			processingTime := time.Since(start)

			if err != nil {
				w.RecordTaskFailed()
				w.progress.FailedTasks.Add(1)
				task.Attempts++
				task.LastError = err.Error()

				// Retry if attempts remaining
				if task.Attempts < w.Config().MaxRetries {
					w.RecordTaskRetry()
					// Re-enqueue with backoff
					go func(t *ReencryptTask) {
						delay := w.CalculateRetryDelay(t.Attempts)
						select {
						case <-time.After(delay):
						case <-w.Context().Done():
							return
						}
						if w.IsRunning() {
							select {
							case w.taskQueue <- t:
							default:
								// Queue full, task lost
							}
						}
					}(task)
				} else if logger != nil {
					logger.WithField("key", task.Key).
						WithField("error", err.Error()).
						Error("re-encryption task failed permanently")
				}
			} else {
				w.RecordTaskComplete(processingTime)
				w.progress.CompletedTasks.Add(1)
				w.progress.LastUpdateTime.Store(time.Now().UnixNano())

				if logger != nil {
					logger.WithField("key", task.Key).
						WithField("duration", processingTime).
						Debug("re-encryption task completed")
				}
			}
		}
	}
}

// processTask re-encrypts an object with a new key.
func (w *ReencryptWorker) processTask(task *ReencryptTask) error {
	ctx, cancel := w.CreateTaskContext()
	defer cancel()

	// Get the catalog entry
	entry, err := w.catalog.Get(ctx, task.Key)
	if err != nil {
		if IsNotFound(err) {
			// Entry no longer exists, skip
			return nil
		}
		return NewReencryptError(task.Key, err)
	}

	// Process each location that uses the old key
	locations := entry.GetAllLocations()
	processedBytes := int64(0)

	for _, loc := range locations {
		// Skip if location doesn't use the old key
		if loc.Encryption == nil || loc.Encryption.KeyID != task.OldKeyID {
			continue
		}

		// Check if we should process this store
		if len(task.Stores) > 0 && !containsStore(task.Stores, loc.Store) {
			continue
		}

		// Process this location
		bytesProcessed, err := w.reencryptLocation(ctx, task, entry, &loc)
		if err != nil {
			return err
		}
		processedBytes += bytesProcessed
	}

	w.progress.BytesProcessed.Add(processedBytes)
	return nil
}

// reencryptLocation re-encrypts data at a specific location.
func (w *ReencryptWorker) reencryptLocation(ctx context.Context, task *ReencryptTask, entry *CatalogEntry, loc *StoreLocation) (int64, error) {
	// Get the backend
	backend, err := w.backends.GetBackend(loc.Store)
	if err != nil {
		return 0, NewReencryptError(task.Key, &ReencryptBackendError{Operation: "get backend", Store: loc.Store, Cause: err})
	}

	// Read encrypted data from backend
	reader, err := backend.GetWithContext(ctx, loc.ObjectRef)
	if err != nil {
		return 0, NewReencryptError(task.Key, &ReencryptBackendError{Operation: "read from", Store: loc.Store, Cause: err})
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return 0, NewReencryptError(task.Key, &ReencryptDataError{Operation: "read data", Cause: err})
	}

	// Check size limits
	if w.config.MaxBytesPerTask > 0 && int64(len(data)) > w.config.MaxBytesPerTask {
		return 0, NewReencryptError(task.Key, &ReencryptSizeExceededError{Size: int64(len(data)), Limit: w.config.MaxBytesPerTask})
	}

	// Re-encrypt with new key
	result, err := w.encryption.Reencrypt(ctx, data, loc.Encryption, task.NewKeyID)
	if err != nil {
		return 0, NewReencryptError(task.Key, &ReencryptDataError{Operation: "reencrypt", Cause: err})
	}

	// Write re-encrypted data back
	err = backend.PutWithContext(ctx, loc.ObjectRef, bytes.NewReader(result.Ciphertext))
	if err != nil {
		return 0, NewReencryptError(task.Key, &ReencryptBackendError{Operation: "write to", Store: loc.Store, Cause: err})
	}

	// Verify if configured
	if w.config.VerifyAfterReencrypt {
		if err := w.verifyReencryption(ctx, backend, loc.ObjectRef, result.Ciphertext); err != nil {
			return 0, NewReencryptError(task.Key, &ReencryptDataError{Operation: "verify", Cause: err})
		}
	}

	// Update catalog entry with new encryption info
	err = w.catalog.Update(ctx, task.Key, func(e *CatalogEntry) error {
		return updateLocationEncryption(e, loc.Store, result.Encryption)
	})
	if err != nil {
		return 0, NewReencryptError(task.Key, &ReencryptDataError{Operation: "update catalog", Cause: err})
	}

	return int64(len(data)), nil
}

// verifyReencryption verifies that re-encrypted data was written correctly.
func (w *ReencryptWorker) verifyReencryption(ctx context.Context, backend interface {
	GetWithContext(ctx context.Context, key string) (io.ReadCloser, error)
}, objectRef string, expected []byte) error {
	reader, err := backend.GetWithContext(ctx, objectRef)
	if err != nil {
		return err
	}
	defer reader.Close()

	actual, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	if !bytes.Equal(expected, actual) {
		return ErrVerificationDataMismatch
	}

	return nil
}

// updateLocationEncryption updates the encryption info for a specific location.
func updateLocationEncryption(entry *CatalogEntry, store string, enc *LocationEncryption) error {
	if entry == nil {
		return ErrEntryNil
	}

	// Update primary store
	if entry.PrimaryStore.Store == store {
		entry.PrimaryStore.Encryption = enc.Clone()
		entry.ModifiedAt = time.Now().UnixNano()
		entry.Version++
		return nil
	}

	// Update replicas
	for i := range entry.Replicas {
		if entry.Replicas[i].Store == store {
			entry.Replicas[i].Encryption = enc.Clone()
			entry.ModifiedAt = time.Now().UnixNano()
			entry.Version++
			return nil
		}
	}

	// Update archive store
	if entry.ArchiveStore != nil && entry.ArchiveStore.Store == store {
		entry.ArchiveStore.Encryption = enc.Clone()
		entry.ModifiedAt = time.Now().UnixNano()
		entry.Version++
		return nil
	}

	return NewLocationNotFoundError(entry.Key, store)
}

// containsStore checks if a store is in the list.
func containsStore(stores []string, store string) bool {
	for _, s := range stores {
		if s == store {
			return true
		}
	}
	return false
}

// IsQueueFull returns true if the error indicates a full queue.
func IsQueueFull(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		return ce.Code == ErrCodeQueueFull
	}
	return false
}
