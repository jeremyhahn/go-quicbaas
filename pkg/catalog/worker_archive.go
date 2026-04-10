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
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ArchiveWorkerName is the standard name for the archive worker.
const ArchiveWorkerName = "archive-worker"

// ArchiveWorker moves cold objects to archive storage based on access patterns.
type ArchiveWorker struct {
	*BaseWorker
	catalog  CatalogManager
	backends BackendManager
}

// NewArchiveWorker creates a new archive worker.
func NewArchiveWorker(
	catalog CatalogManager,
	backends BackendManager,
	config *WorkerConfig,
	logger interfaces.Logger,
) (*ArchiveWorker, error) {
	if catalog == nil {
		return nil, ErrCatalogManagerNil
	}
	if backends == nil {
		return nil, ErrBackendManagerNilForWorker
	}

	base, err := NewBaseWorker(ArchiveWorkerName, config, logger)
	if err != nil {
		return nil, err
	}

	return &ArchiveWorker{
		BaseWorker: base,
		catalog:    catalog,
		backends:   backends,
	}, nil
}

// Start begins the archive worker's processing loop.
func (w *ArchiveWorker) Start(ctx context.Context) error {
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

// Stop gracefully stops the archive worker.
func (w *ArchiveWorker) Stop() error {
	return w.StopBase()
}

// processLoop is the main processing loop for a worker goroutine.
func (w *ArchiveWorker) processLoop(workerID int) {
	defer w.wg.Done()

	workerLogger := w.logger
	if workerLogger != nil {
		workerLogger = workerLogger.WithField("worker_id", workerID)
	}

	for w.ShouldContinue() {
		tasks, err := w.catalog.DequeueArchive(w.ctx, w.config.BatchSize)
		if err != nil {
			if workerLogger != nil && !IsCatalogClosed(err) {
				workerLogger.WithError(err).Warn("failed to dequeue archive tasks")
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

// processTask processes a single archive task with retry support.
func (w *ArchiveWorker) processTask(logger interfaces.Logger, task *ArchiveTask) {
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

		err := w.executeArchive(task)
		if err == nil {
			w.RecordTaskComplete(time.Since(start))
			if logger != nil {
				logger.WithFields(map[string]interface{}{
					"key":          task.Key,
					"target_store": task.TargetStore,
					"duration":     time.Since(start),
				}).Debug("archive task completed")
			}
			return
		}

		lastErr = err
		if logger != nil {
			logger.WithFields(map[string]interface{}{
				"key":     task.Key,
				"attempt": attempt + 1,
				"error":   err.Error(),
			}).Warn("archive attempt failed")
		}
	}

	w.RecordTaskFailed()
	if logger != nil {
		logger.WithFields(map[string]interface{}{
			"key":   task.Key,
			"error": lastErr.Error(),
		}).Error("archive task failed after retries")
	}
}

// executeArchive performs the actual archive operation for a task.
func (w *ArchiveWorker) executeArchive(task *ArchiveTask) error {
	ctx, cancel := w.CreateTaskContext()
	defer cancel()

	// Get the catalog entry
	entry, err := w.catalog.Get(ctx, task.Key)
	if err != nil {
		return NewTaskProcessingError("archive", task.Key, err)
	}

	// Check if archiving is needed based on policy
	if entry.ArchivePolicy == nil || !entry.ArchivePolicy.Enabled {
		return nil // No archive policy, skip
	}

	if !entry.ArchivePolicy.ShouldArchive(entry.LastAccessAt) {
		return nil // Not ready to archive yet
	}

	// Already archived
	if entry.ArchiveStore != nil && entry.ArchiveStore.Store == task.TargetStore {
		return nil
	}

	// Get source backend and data
	srcBackend, err := w.backends.GetBackend(entry.PrimaryStore.Store)
	if err != nil {
		return NewTaskProcessingError("archive", task.Key, err)
	}

	reader, err := srcBackend.GetWithContext(ctx, entry.PrimaryStore.ObjectRef)
	if err != nil {
		return NewArchiveError(task.Key, entry.PrimaryStore.Store, err)
	}
	defer reader.Close()

	// Read all data into memory
	data, err := io.ReadAll(reader)
	if err != nil {
		return NewArchiveError(task.Key, entry.PrimaryStore.Store, err)
	}

	// Verify source data integrity
	if len(entry.Checksum) > 0 {
		hash := sha256.Sum256(data)
		if !bytesEqual(hash[:], entry.Checksum) {
			return NewChecksumMismatchError(task.Key, entry.Checksum, hash[:])
		}
	}

	// Apply compression if configured
	archiveData := data
	compressed := false
	if entry.ArchivePolicy.CompressionLevel > 0 {
		compressedData, err := compressData(data, entry.ArchivePolicy.CompressionLevel)
		if err != nil {
			if w.logger != nil {
				w.logger.WithFields(map[string]interface{}{
					"key":   task.Key,
					"error": err.Error(),
				}).Warn("compression failed, archiving uncompressed")
			}
		} else {
			archiveData = compressedData
			compressed = true
		}
	}

	// Get archive backend
	archiveBackend, err := w.backends.GetBackend(task.TargetStore)
	if err != nil {
		return NewArchiveError(task.Key, task.TargetStore, err)
	}

	// Generate archive object reference
	objectRef := generateArchiveRef(entry.Key, task.TargetStore)

	// Write to archive backend
	if err := archiveBackend.PutWithContext(ctx, objectRef, bytes.NewReader(archiveData)); err != nil {
		return NewArchiveError(task.Key, task.TargetStore, err)
	}

	// Calculate checksum for stored data
	hash := sha256.Sum256(archiveData)

	// Create archive location
	archiveLocation := &StoreLocation{
		Store:      task.TargetStore,
		ObjectRef:  objectRef,
		Status:     LocationStatusActive,
		Checksum:   hash[:],
		StoredSize: int64(len(archiveData)),
		Compressed: compressed,
		SyncedAt:   time.Now().UnixNano(),
	}

	// Update catalog with archive location
	err = w.catalog.Update(ctx, entry.Key, func(e *CatalogEntry) error {
		e.ArchiveStore = archiveLocation
		e.Status = StatusArchived
		e.ArchivedAt = time.Now().UnixNano()
		return nil
	})
	if err != nil {
		// Try to clean up the written data
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = archiveBackend.DeleteWithContext(cleanupCtx, objectRef)
		return NewArchiveError(task.Key, task.TargetStore, err)
	}

	// Delete from primary if configured
	if entry.ArchivePolicy.DeleteFromPrimary {
		if err := w.deletePrimaryData(entry); err != nil {
			// Log but don't fail - archive succeeded
			if w.logger != nil {
				w.logger.WithFields(map[string]interface{}{
					"key":   task.Key,
					"error": err.Error(),
				}).Warn("failed to delete from primary after archive")
			}
		}
	}

	return nil
}

// deletePrimaryData deletes data from the primary store after archival.
func (w *ArchiveWorker) deletePrimaryData(entry *CatalogEntry) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	srcBackend, err := w.backends.GetBackend(entry.PrimaryStore.Store)
	if err != nil {
		return err
	}

	if err := srcBackend.DeleteWithContext(ctx, entry.PrimaryStore.ObjectRef); err != nil {
		return err
	}

	// Mark primary location as stale in catalog
	return w.catalog.UpdateLocationStatus(ctx, entry.Key, entry.PrimaryStore.Store, LocationStatusStale)
}

// generateArchiveRef generates a unique archive object reference.
func generateArchiveRef(key, store string) string {
	return fmt.Sprintf("archive/%s/%s/%d", store, key, time.Now().UnixNano())
}

// compressData compresses data using gzip with the specified compression level.
func compressData(data []byte, level int) ([]byte, error) {
	if level < 1 || level > 9 {
		level = gzip.DefaultCompression
	}

	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, level)
	if err != nil {
		return nil, err
	}

	if _, err := writer.Write(data); err != nil {
		writer.Close() //nolint:gosec // G104: best-effort cleanup on error path
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// decompressData decompresses gzip compressed data.
func decompressData(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}
