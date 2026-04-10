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
	"crypto/sha256"
	"fmt"
	"io"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ReplicationWorkerName is the standard name for the replication worker.
const ReplicationWorkerName = "replication-worker"

// ReplicationWorker handles asynchronous replication of objects to target stores.
type ReplicationWorker struct {
	*BaseWorker
	catalog  CatalogManager
	backends BackendManager
}

// NewReplicationWorker creates a new replication worker.
func NewReplicationWorker(
	catalog CatalogManager,
	backends BackendManager,
	config *WorkerConfig,
	logger interfaces.Logger,
) (*ReplicationWorker, error) {
	if catalog == nil {
		return nil, ErrCatalogManagerNil
	}
	if backends == nil {
		return nil, ErrBackendManagerNilForWorker
	}

	base, err := NewBaseWorker(ReplicationWorkerName, config, logger)
	if err != nil {
		return nil, err
	}

	return &ReplicationWorker{
		BaseWorker: base,
		catalog:    catalog,
		backends:   backends,
	}, nil
}

// Start begins the replication worker's processing loop.
func (w *ReplicationWorker) Start(ctx context.Context) error {
	if err := w.StartBase(ctx); err != nil {
		return err
	}

	// Start worker goroutines
	for i := 0; i < w.config.Workers; i++ {
		w.wg.Add(1)
		go w.processLoop(i)
	}

	return nil
}

// Stop gracefully stops the replication worker.
func (w *ReplicationWorker) Stop() error {
	return w.StopBase()
}

// processLoop is the main processing loop for a worker goroutine.
func (w *ReplicationWorker) processLoop(workerID int) {
	defer w.wg.Done()

	workerLogger := w.logger
	if workerLogger != nil {
		workerLogger = workerLogger.WithField("worker_id", workerID)
	}

	for w.ShouldContinue() {
		tasks, err := w.catalog.DequeueReplication(w.ctx, w.config.BatchSize)
		if err != nil {
			if workerLogger != nil && !IsCatalogClosed(err) {
				workerLogger.WithError(err).Warn("failed to dequeue replication tasks")
			}
			w.Sleep()
			continue
		}

		if len(tasks) == 0 {
			w.Sleep()
			continue
		}

		for _, task := range tasks {
			if !w.ShouldContinue() {
				break
			}
			w.processTask(workerLogger, task)
		}
	}
}

// processTask processes a single replication task with retry support.
func (w *ReplicationWorker) processTask(logger interfaces.Logger, task *ReplicationTask) {
	w.RecordTaskStart()
	start := time.Now()

	var lastErr error
	for attempt := 0; attempt <= w.config.MaxRetries; attempt++ {
		if attempt > 0 {
			w.RecordTaskRetry()
			delay := w.CalculateRetryDelay(attempt - 1)
			select {
			case <-w.ctx.Done():
				w.RecordTaskFailed()
				return
			case <-time.After(delay):
			}
		}

		err := w.executeReplication(task)
		if err == nil {
			w.RecordTaskComplete(time.Since(start))
			if logger != nil {
				logger.WithFields(map[string]interface{}{
					"key":      task.Key,
					"targets":  task.Targets,
					"duration": time.Since(start),
				}).Debug("replication task completed")
			}
			return
		}

		lastErr = err
		if logger != nil {
			logger.WithFields(map[string]interface{}{
				"key":     task.Key,
				"attempt": attempt + 1,
				"error":   err.Error(),
			}).Warn("replication attempt failed")
		}
	}

	w.RecordTaskFailed()
	if logger != nil {
		logger.WithFields(map[string]interface{}{
			"key":   task.Key,
			"error": lastErr.Error(),
		}).Error("replication task failed after retries")
	}
}

// executeReplication performs the actual replication for a task.
func (w *ReplicationWorker) executeReplication(task *ReplicationTask) error {
	ctx, cancel := w.CreateTaskContext()
	defer cancel()

	// Get the catalog entry
	entry, err := w.catalog.Get(ctx, task.Key)
	if err != nil {
		return NewTaskProcessingError("replication", task.Key, err)
	}

	// Get source backend and data
	srcBackend, err := w.backends.GetBackend(entry.PrimaryStore.Store)
	if err != nil {
		return NewTaskProcessingError("replication", task.Key, err)
	}

	reader, err := srcBackend.GetWithContext(ctx, entry.PrimaryStore.ObjectRef)
	if err != nil {
		return NewReplicationError(task.Key, entry.PrimaryStore.Store, err)
	}
	defer reader.Close()

	// Read all data into memory for replication
	data, err := io.ReadAll(reader)
	if err != nil {
		return NewReplicationError(task.Key, entry.PrimaryStore.Store, err)
	}

	// Verify source data integrity if checksum exists
	if len(entry.Checksum) > 0 {
		hash := sha256.Sum256(data)
		if !bytesEqual(hash[:], entry.Checksum) {
			return NewChecksumMismatchError(task.Key, entry.Checksum, hash[:])
		}
	}

	// Replicate to each target store
	for _, target := range task.Targets {
		// Skip if already replicated to this target
		if entry.HasLocation(target) {
			continue
		}

		if err := w.replicateToTarget(ctx, entry, data, target); err != nil {
			// Log error but continue with other targets
			if w.logger != nil {
				w.logger.WithFields(map[string]interface{}{
					"key":    task.Key,
					"target": target,
					"error":  err.Error(),
				}).Warn("failed to replicate to target")
			}
			continue
		}
	}

	return nil
}

// replicateToTarget replicates data to a single target store.
func (w *ReplicationWorker) replicateToTarget(
	ctx context.Context,
	entry *CatalogEntry,
	data []byte,
	target string,
) error {
	// Get target backend
	dstBackend, err := w.backends.GetBackend(target)
	if err != nil {
		return NewReplicationError(entry.Key, target, err)
	}

	// Generate object reference for target
	objectRef := generateObjectRef(entry.Key, target)

	// Write to target backend
	if err := dstBackend.PutWithContext(ctx, objectRef, bytes.NewReader(data)); err != nil {
		return NewReplicationError(entry.Key, target, err)
	}

	// Calculate checksum for the stored data
	hash := sha256.Sum256(data)

	// Create new location
	newLocation := &StoreLocation{
		Store:      target,
		ObjectRef:  objectRef,
		Status:     LocationStatusActive,
		Checksum:   hash[:],
		StoredSize: int64(len(data)),
		SyncedAt:   time.Now().UnixNano(),
	}

	// Update catalog with new location
	if err := w.catalog.AddLocation(ctx, entry.Key, newLocation); err != nil {
		// Try to clean up the written data
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = dstBackend.DeleteWithContext(cleanupCtx, objectRef)
		return NewReplicationError(entry.Key, target, err)
	}

	return nil
}

// generateObjectRef generates a unique object reference for a target store.
func generateObjectRef(key, store string) string {
	// Use a consistent format: store/key with timestamp for uniqueness
	return fmt.Sprintf("%s/%s/%d", store, key, time.Now().UnixNano())
}

// bytesEqual compares two byte slices for equality.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
