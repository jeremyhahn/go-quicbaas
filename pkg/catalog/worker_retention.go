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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// RetentionWorkerName is the standard name for the retention worker.
const RetentionWorkerName = "retention-worker"

// RetentionTask represents a pending retention enforcement operation.
type RetentionTask struct {
	Key       string
	CreatedAt int64
}

// RetentionWorker enforces retention policies by deleting expired objects.
type RetentionWorker struct {
	*BaseWorker
	catalog  CatalogManager
	backends BackendManager

	// scanInterval is how often to scan for expired entries.
	scanInterval time.Duration
}

// RetentionWorkerConfig extends WorkerConfig with retention-specific settings.
type RetentionWorkerConfig struct {
	*WorkerConfig

	// ScanInterval is how often to scan the catalog for expired entries.
	ScanInterval time.Duration
}

// DefaultRetentionWorkerConfig returns default configuration for retention worker.
func DefaultRetentionWorkerConfig() *RetentionWorkerConfig {
	return &RetentionWorkerConfig{
		WorkerConfig: DefaultWorkerConfig(),
		ScanInterval: time.Hour,
	}
}

// NewRetentionWorker creates a new retention enforcement worker.
func NewRetentionWorker(
	catalog CatalogManager,
	backends BackendManager,
	config *RetentionWorkerConfig,
	logger interfaces.Logger,
) (*RetentionWorker, error) {
	if catalog == nil {
		return nil, ErrCatalogManagerNil
	}
	if backends == nil {
		return nil, ErrBackendManagerNilForWorker
	}

	if config == nil {
		config = DefaultRetentionWorkerConfig()
	}

	base, err := NewBaseWorker(RetentionWorkerName, config.WorkerConfig, logger)
	if err != nil {
		return nil, err
	}

	scanInterval := config.ScanInterval
	if scanInterval <= 0 {
		scanInterval = time.Hour
	}

	return &RetentionWorker{
		BaseWorker:   base,
		catalog:      catalog,
		backends:     backends,
		scanInterval: scanInterval,
	}, nil
}

// Start begins the retention worker's processing loop.
func (w *RetentionWorker) Start(ctx context.Context) error {
	if err := w.StartBase(ctx); err != nil {
		return err
	}

	// Start the scanner goroutine
	w.wg.Add(1)
	go w.scanLoop()

	// Start worker goroutines for processing deletion tasks
	for i := 0; i < w.config.Workers; i++ {
		w.wg.Add(1)
		go w.processLoop(i)
	}

	return nil
}

// Stop gracefully stops the retention worker.
func (w *RetentionWorker) Stop() error {
	return w.StopBase()
}

// scanLoop periodically scans the catalog for expired entries.
func (w *RetentionWorker) scanLoop() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.scanInterval)
	defer ticker.Stop()

	// Do an initial scan
	w.scanForExpiredEntries()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.scanForExpiredEntries()
		}
	}
}

// scanForExpiredEntries scans the catalog for entries that should be deleted.
func (w *RetentionWorker) scanForExpiredEntries() {
	ctx, cancel := context.WithTimeout(w.ctx, 5*time.Minute)
	defer cancel()

	// List all entries with active status
	result, err := w.catalog.ListByStatus(ctx, StatusActive, &ListOptions{Limit: 1000})
	if err != nil {
		if w.logger != nil && !IsCatalogClosed(err) {
			w.logger.WithError(err).Warn("failed to list entries for retention scan")
		}
		return
	}

	expiredCount := 0
	for _, key := range result.Keys {
		if !w.ShouldContinue() {
			break
		}

		entry, err := w.catalog.Get(ctx, key)
		if err != nil {
			continue
		}

		if entry.ShouldDelete() {
			// Enqueue directly since we don't have a separate retention queue
			// Process inline using processTask
			task := &RetentionTask{
				Key:       entry.Key,
				CreatedAt: time.Now().UnixNano(),
			}
			w.processTask(w.logger, task)
			expiredCount++
		}
	}

	if w.logger != nil && expiredCount > 0 {
		w.logger.WithField("expired_count", expiredCount).Info("retention scan completed")
	}
}

// processLoop handles tasks that may be enqueued externally.
func (w *RetentionWorker) processLoop(workerID int) {
	defer w.wg.Done()

	// The retention worker primarily uses the scanLoop, but this loop
	// allows for external task enqueueing if needed in the future.
	for w.ShouldContinue() {
		w.Sleep()
	}
}

// processTask processes a single retention task.
func (w *RetentionWorker) processTask(logger interfaces.Logger, task *RetentionTask) {
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

		err := w.executeRetention(task)
		if err == nil {
			w.RecordTaskComplete(time.Since(start))
			if logger != nil {
				logger.WithFields(map[string]interface{}{
					"key":      task.Key,
					"duration": time.Since(start),
				}).Debug("retention task completed")
			}
			return
		}

		lastErr = err
		if logger != nil {
			logger.WithFields(map[string]interface{}{
				"key":     task.Key,
				"attempt": attempt + 1,
				"error":   err.Error(),
			}).Warn("retention attempt failed")
		}
	}

	w.RecordTaskFailed()
	if logger != nil {
		logger.WithFields(map[string]interface{}{
			"key":   task.Key,
			"error": lastErr.Error(),
		}).Error("retention task failed after retries")
	}
}

// executeRetention performs the actual retention enforcement for a task.
func (w *RetentionWorker) executeRetention(task *RetentionTask) error {
	ctx, cancel := w.CreateTaskContext()
	defer cancel()

	// Get the catalog entry
	entry, err := w.catalog.Get(ctx, task.Key)
	if err != nil {
		if IsNotFound(err) {
			return nil // Already deleted
		}
		return NewTaskProcessingError("retention", task.Key, err)
	}

	// Double-check that we should delete
	if !entry.ShouldDelete() {
		return nil
	}

	// Delete from all locations
	var firstErr error
	for _, loc := range entry.GetAllLocations() {
		if err := w.deleteFromLocation(ctx, entry, &loc); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if w.logger != nil {
				w.logger.WithFields(map[string]interface{}{
					"key":   task.Key,
					"store": loc.Store,
					"error": err.Error(),
				}).Warn("failed to delete from location")
			}
		}
	}

	// Remove from catalog
	if err := w.catalog.Delete(ctx, entry.Key); err != nil {
		return NewRetentionError(task.Key, err)
	}

	return firstErr
}

// deleteFromLocation deletes data from a single storage location.
func (w *RetentionWorker) deleteFromLocation(ctx context.Context, entry *CatalogEntry, loc *StoreLocation) error {
	backend, err := w.backends.GetBackend(loc.Store)
	if err != nil {
		// Backend not found - may have been removed
		return nil
	}

	if err := backend.DeleteWithContext(ctx, loc.ObjectRef); err != nil {
		return NewRetentionError(entry.Key, err)
	}

	return nil
}

// ProcessRetentionTask allows external components to trigger retention for a specific key.
func (w *RetentionWorker) ProcessRetentionTask(key string) error {
	if !w.IsRunning() {
		return NewWorkerNotRunningError(w.name)
	}

	task := &RetentionTask{
		Key:       key,
		CreatedAt: time.Now().UnixNano(),
	}

	w.processTask(w.logger, task)
	return nil
}
