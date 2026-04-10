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
	"crypto/sha256"
	"io"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// VerifyWorkerName is the standard name for the verification worker.
const VerifyWorkerName = "verify-worker"

// VerifyWorker verifies object integrity across all storage locations.
type VerifyWorker struct {
	*BaseWorker
	catalog  CatalogManager
	backends BackendManager
}

// NewVerifyWorker creates a new verification worker.
func NewVerifyWorker(
	catalog CatalogManager,
	backends BackendManager,
	config *WorkerConfig,
	logger interfaces.Logger,
) (*VerifyWorker, error) {
	if catalog == nil {
		return nil, ErrCatalogManagerNil
	}
	if backends == nil {
		return nil, ErrBackendManagerNilForWorker
	}

	base, err := NewBaseWorker(VerifyWorkerName, config, logger)
	if err != nil {
		return nil, err
	}

	return &VerifyWorker{
		BaseWorker: base,
		catalog:    catalog,
		backends:   backends,
	}, nil
}

// Start begins the verification worker's processing loop.
func (w *VerifyWorker) Start(ctx context.Context) error {
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

// Stop gracefully stops the verification worker.
func (w *VerifyWorker) Stop() error {
	return w.StopBase()
}

// processLoop is the main processing loop for a worker goroutine.
func (w *VerifyWorker) processLoop(workerID int) {
	defer w.wg.Done()

	workerLogger := w.logger
	if workerLogger != nil {
		workerLogger = workerLogger.WithField("worker_id", workerID)
	}

	for w.ShouldContinue() {
		tasks, err := w.catalog.DequeueVerify(w.ctx, w.config.BatchSize)
		if err != nil {
			if workerLogger != nil && !IsCatalogClosed(err) {
				workerLogger.WithError(err).Warn("failed to dequeue verify tasks")
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

// processTask processes a single verification task.
func (w *VerifyWorker) processTask(logger interfaces.Logger, task *VerifyTask) {
	w.RecordTaskStart()
	start := time.Now()

	err := w.executeVerify(task)
	if err == nil {
		w.RecordTaskComplete(time.Since(start))
		if logger != nil {
			logger.WithFields(map[string]interface{}{
				"key":      task.Key,
				"stores":   task.Stores,
				"duration": time.Since(start),
			}).Debug("verify task completed")
		}
		return
	}

	w.RecordTaskFailed()
	if logger != nil {
		logger.WithFields(map[string]interface{}{
			"key":   task.Key,
			"error": err.Error(),
		}).Warn("verify task failed")
	}
}

// executeVerify performs the actual verification for a task.
func (w *VerifyWorker) executeVerify(task *VerifyTask) error {
	ctx, cancel := w.CreateTaskContext()
	defer cancel()

	// Get the catalog entry
	entry, err := w.catalog.Get(ctx, task.Key)
	if err != nil {
		return NewTaskProcessingError("verify", task.Key, err)
	}

	// No checksum to verify against
	if len(entry.Checksum) == 0 {
		if w.logger != nil {
			w.logger.WithField("key", task.Key).Debug("no checksum, skipping verification")
		}
		return nil
	}

	// Determine which stores to verify
	storesToVerify := task.Stores
	if len(storesToVerify) == 0 {
		// Verify all locations
		for _, loc := range entry.GetAllLocations() {
			storesToVerify = append(storesToVerify, loc.Store)
		}
	}

	// Track verification results
	var failedStores []string
	healthyCount := 0

	for _, store := range storesToVerify {
		loc := entry.GetLocation(store)
		if loc == nil {
			continue
		}

		if loc.Status != LocationStatusActive {
			continue
		}

		err := w.verifyLocation(ctx, entry, loc)
		if err != nil {
			failedStores = append(failedStores, store)
			w.markLocationFailed(ctx, entry.Key, store, err)
		} else {
			healthyCount++
		}
	}

	// Check if we need to trigger replication
	if len(failedStores) > 0 && entry.NeedsMoreReplicas() {
		w.triggerRepairReplication(ctx, entry, failedStores)
	}

	// Return error if all locations failed
	if healthyCount == 0 && len(storesToVerify) > 0 {
		return NewVerificationError(task.Key, "all stores", nil)
	}

	return nil
}

// verifyLocation verifies a single storage location.
func (w *VerifyWorker) verifyLocation(ctx context.Context, entry *CatalogEntry, loc *StoreLocation) error {
	backend, err := w.backends.GetBackend(loc.Store)
	if err != nil {
		return NewVerificationError(entry.Key, loc.Store, err)
	}

	reader, err := backend.GetWithContext(ctx, loc.ObjectRef)
	if err != nil {
		return NewVerificationError(entry.Key, loc.Store, err)
	}
	defer reader.Close()

	// Read all data into memory
	data, err := io.ReadAll(reader)
	if err != nil {
		return NewVerificationError(entry.Key, loc.Store, err)
	}

	// Handle compressed data
	if loc.Compressed {
		decompressed, err := decompressData(data)
		if err != nil {
			return NewVerificationError(entry.Key, loc.Store, err)
		}
		data = decompressed
	}

	// Verify checksum
	hash := sha256.Sum256(data)
	if !bytesEqual(hash[:], entry.Checksum) {
		return NewChecksumMismatchError(entry.Key, entry.Checksum, hash[:])
	}

	// Optionally verify location-specific checksum
	if len(loc.Checksum) > 0 && loc.Compressed {
		// For compressed data, verify against the compressed checksum
		// Re-read raw data from storage
		rawReader, err := backend.GetWithContext(ctx, loc.ObjectRef)
		if err != nil {
			return NewVerificationError(entry.Key, loc.Store, err)
		}
		defer rawReader.Close()

		rawData, err := io.ReadAll(rawReader)
		if err != nil {
			return NewVerificationError(entry.Key, loc.Store, err)
		}

		compressedHash := sha256.Sum256(rawData)
		if !bytesEqual(compressedHash[:], loc.Checksum) {
			return NewChecksumMismatchError(entry.Key, loc.Checksum, compressedHash[:])
		}
	}

	return nil
}

// markLocationFailed marks a location as failed in the catalog.
func (w *VerifyWorker) markLocationFailed(ctx context.Context, key, store string, err error) {
	if updateErr := w.catalog.UpdateLocationStatus(ctx, key, store, LocationStatusFailed); updateErr != nil {
		if w.logger != nil {
			w.logger.WithFields(map[string]interface{}{
				"key":   key,
				"store": store,
				"error": updateErr.Error(),
			}).Warn("failed to mark location as failed")
		}
	}

	if w.logger != nil {
		w.logger.WithFields(map[string]interface{}{
			"key":          key,
			"store":        store,
			"verify_error": err.Error(),
		}).Warn("verification failed for location")
	}
}

// triggerRepairReplication enqueues replication to repair missing/failed replicas.
func (w *VerifyWorker) triggerRepairReplication(ctx context.Context, entry *CatalogEntry, failedStores []string) {
	if entry.ReplicationPolicy == nil || !entry.ReplicationPolicy.Enabled {
		return
	}

	// Determine targets for repair replication
	targets := make([]string, 0)
	for _, store := range entry.ReplicationPolicy.TargetStores {
		// Include stores that failed or don't have the data
		needsReplication := false
		for _, failed := range failedStores {
			if store == failed {
				needsReplication = true
				break
			}
		}
		if !needsReplication && !entry.HasLocation(store) {
			needsReplication = true
		}
		if needsReplication {
			targets = append(targets, store)
		}
	}

	if len(targets) > 0 {
		// Use high priority for repair replication
		if err := w.catalog.EnqueueReplication(ctx, entry.Key, targets, MaxPriority); err != nil {
			if w.logger != nil {
				w.logger.WithFields(map[string]interface{}{
					"key":     entry.Key,
					"targets": targets,
					"error":   err.Error(),
				}).Warn("failed to enqueue repair replication")
			}
		} else if w.logger != nil {
			w.logger.WithFields(map[string]interface{}{
				"key":     entry.Key,
				"targets": targets,
			}).Info("enqueued repair replication")
		}
	}
}
