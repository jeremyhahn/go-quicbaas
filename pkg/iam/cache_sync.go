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

package iam

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// DeltaLogEntry represents a single entry in the delta log.
type DeltaLogEntry struct {
	Event     *EnhancedPolicyChangeEvent
	Timestamp time.Time
	Version   int64
}

// DeltaLog tracks incremental policy changes for efficient synchronization.
type DeltaLog struct {
	// entries stores the delta log entries in a ring buffer.
	entries []DeltaLogEntry

	// maxEntries is the maximum number of entries to keep.
	maxEntries int

	// writeIndex is the current write position in the ring buffer.
	writeIndex atomic.Int64

	// minVersion is the minimum version available in the log.
	minVersion atomic.Int64

	// maxVersion is the maximum version available in the log.
	maxVersion atomic.Int64

	// mu protects concurrent access to the entries slice.
	mu sync.RWMutex

	// tenantLogs tracks per-tenant delta logs.
	tenantLogs sync.Map // map[string]*tenantDeltaLog
}

// tenantDeltaLog tracks changes for a specific tenant.
type tenantDeltaLog struct {
	entries    []DeltaLogEntry
	mu         sync.RWMutex
	minVersion int64
	maxVersion int64
}

// NewDeltaLog creates a new delta log with the specified maximum entries.
func NewDeltaLog(maxEntries int) *DeltaLog {
	if maxEntries <= 0 {
		maxEntries = 10000 // Default
	}

	return &DeltaLog{
		entries:    make([]DeltaLogEntry, maxEntries),
		maxEntries: maxEntries,
	}
}

// Append adds an event to the delta log.
func (d *DeltaLog) Append(event *EnhancedPolicyChangeEvent) error {
	if event == nil {
		return &NilEventError{}
	}

	entry := DeltaLogEntry{
		Event:     event,
		Timestamp: time.Now(),
		Version:   event.Version,
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Get the current write index
	idx := int(d.writeIndex.Load() % int64(d.maxEntries))

	// Store the entry
	d.entries[idx] = entry

	// Increment write index
	d.writeIndex.Add(1)

	// Update min/max versions
	if d.minVersion.Load() == 0 || entry.Version < d.minVersion.Load() {
		d.minVersion.Store(entry.Version)
	}
	if entry.Version > d.maxVersion.Load() {
		d.maxVersion.Store(entry.Version)
	}

	// Append to tenant-specific log
	d.appendToTenantLog(event.TenantID, entry)

	return nil
}

// appendToTenantLog appends an entry to a tenant-specific log.
// appendToTenantLog appends an entry to a tenant-specific log.
func (d *DeltaLog) appendToTenantLog(tenantID string, entry DeltaLogEntry) {
	value, _ := d.tenantLogs.LoadOrStore(tenantID, &tenantDeltaLog{
		entries:    make([]DeltaLogEntry, 0, 100),
		minVersion: entry.Version,
		maxVersion: entry.Version,
	})

	tlog := value.(*tenantDeltaLog)
	tlog.mu.Lock()
	defer tlog.mu.Unlock()

	tlog.entries = append(tlog.entries, entry)

	// Update min/max versions
	if len(tlog.entries) == 1 {
		tlog.minVersion = entry.Version
		tlog.maxVersion = entry.Version
	} else {
		if entry.Version < tlog.minVersion {
			tlog.minVersion = entry.Version
		}
		if entry.Version > tlog.maxVersion {
			tlog.maxVersion = entry.Version
		}
	}

	// Trim if too large - use parent's maxEntries or 1000, whichever is smaller
	maxTenantEntries := d.maxEntries
	if maxTenantEntries > 1000 {
		maxTenantEntries = 1000
	}

	if len(tlog.entries) > maxTenantEntries {
		tlog.entries = tlog.entries[len(tlog.entries)-maxTenantEntries:]
		if len(tlog.entries) > 0 {
			tlog.minVersion = tlog.entries[0].Version
		}
	}
}

// GetSince retrieves all changes since a specific version for a tenant.
func (d *DeltaLog) GetSince(tenantID string, sinceVersion int64) ([]*EnhancedPolicyChangeEvent, error) {
	value, ok := d.tenantLogs.Load(tenantID)
	if !ok {
		return nil, nil // No changes for this tenant
	}

	tlog := value.(*tenantDeltaLog)
	tlog.mu.RLock()
	defer tlog.mu.RUnlock()

	// Check if we can fulfill this request
	if sinceVersion < tlog.minVersion {
		return nil, &DeltaLogVersionError{RequestedVersion: sinceVersion, MinimumVersion: tlog.minVersion}
	}

	var events []*EnhancedPolicyChangeEvent
	for i := range tlog.entries {
		if tlog.entries[i].Version > sinceVersion {
			events = append(events, tlog.entries[i].Event)
		}
	}

	return events, nil
}

// Trim removes old entries from the delta log.
func (d *DeltaLog) Trim() {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Reset the log by clearing old entries
	totalEntries := int(d.writeIndex.Load())
	if totalEntries > d.maxEntries {
		// Keep only the most recent maxEntries
		newMinVersion := d.maxVersion.Load() - int64(d.maxEntries)
		if newMinVersion > 0 {
			d.minVersion.Store(newMinVersion)
		}
	}
}

// GetMinVersion returns the minimum version available in the log.
func (d *DeltaLog) GetMinVersion() int64 {
	return d.minVersion.Load()
}

// GetMaxVersion returns the maximum version available in the log.
func (d *DeltaLog) GetMaxVersion() int64 {
	return d.maxVersion.Load()
}

// Size returns the current number of entries in the log.
func (d *DeltaLog) Size() int {
	writeIndex := d.writeIndex.Load()
	if writeIndex > int64(d.maxEntries) {
		return d.maxEntries
	}
	return int(writeIndex)
}

// BatchProcessor batches policy change events for efficient propagation.
type BatchProcessor struct {
	// config is the sync configuration.
	config *EnhancedCacheSyncConfig

	// syncer is the underlying cache syncer.
	syncer PolicyCacheSyncer

	// batch stores the current batch of events.
	batch []*EnhancedPolicyChangeEvent

	// batchMu protects the batch slice.
	batchMu sync.Mutex

	// flushTimer triggers periodic batch flushing.
	flushTimer *time.Timer

	// closed indicates if the processor is closed.
	closed atomic.Bool

	// stopCh signals the processor to stop.
	stopCh chan struct{}

	// wg tracks running goroutines.
	wg sync.WaitGroup

	// metrics tracks batch processing metrics.
	metrics *EnhancedCacheSyncMetrics

	// compressor handles compression if enabled.
	compressor *gzip.Writer

	// compressionBuffer is the buffer for compression.
	compressionBuffer *bytes.Buffer
}

// NewBatchProcessor creates a new batch processor.
func NewBatchProcessor(config *EnhancedCacheSyncConfig, syncer PolicyCacheSyncer) (*BatchProcessor, error) {
	if config == nil {
		return nil, &InvalidSyncConfigError{Reason: "config is nil"}
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if syncer == nil {
		return nil, &InvalidSyncConfigError{Reason: "syncer is nil"}
	}

	bp := &BatchProcessor{
		config:            config,
		syncer:            syncer,
		batch:             make([]*EnhancedPolicyChangeEvent, 0, config.BatchSize),
		stopCh:            make(chan struct{}),
		metrics:           NewEnhancedCacheSyncMetrics(),
		compressionBuffer: new(bytes.Buffer),
	}

	// Initialize compressor if compression is enabled
	if config.EnableCompression {
		var err error
		bp.compressor, err = gzip.NewWriterLevel(bp.compressionBuffer, config.CompressionLevel)
		if err != nil {
			return nil, &CompressorCreationError{Cause: err}
		}
	}

	// Start the flush timer
	bp.flushTimer = time.NewTimer(config.BatchInterval)

	// Start the flush goroutine
	bp.wg.Add(1)
	go bp.flushLoop()

	return bp, nil
}

// Add adds an event to the current batch.
func (bp *BatchProcessor) Add(event *EnhancedPolicyChangeEvent) error {
	if bp.closed.Load() {
		return ErrSyncerClosed
	}

	if event == nil {
		return &NilEventError{}
	}

	bp.batchMu.Lock()
	defer bp.batchMu.Unlock()

	// Add to batch
	bp.batch = append(bp.batch, event)

	// Flush if batch is full
	if len(bp.batch) >= bp.config.BatchSize {
		return bp.flushLocked()
	}

	return nil
}

// Flush sends the current batch immediately.
func (bp *BatchProcessor) Flush() error {
	if bp.closed.Load() {
		return ErrSyncerClosed
	}

	bp.batchMu.Lock()
	defer bp.batchMu.Unlock()

	return bp.flushLocked()
}

// flushLocked flushes the current batch (caller must hold batchMu).
func (bp *BatchProcessor) flushLocked() error {
	if len(bp.batch) == 0 {
		return nil
	}

	// Create a batch event
	batchEvent := &EnhancedPolicyChangeEvent{
		EventID:   uuid.New().String(),
		Type:      PolicyUpdateBatch,
		TenantID:  bp.config.TenantID,
		NodeID:    bp.config.NodeID,
		Timestamp: time.Now(),
		Metadata:  map[string]string{"batch_size": fmt.Sprintf("%d", len(bp.batch))},
	}

	// Serialize the batch
	data, err := json.Marshal(bp.batch)
	if err != nil {
		return &BatchMarshalError{Cause: err}
	}

	// Compress if enabled
	if bp.config.EnableCompression {
		originalSize := len(data)
		compressed, err := bp.compress(data)
		if err != nil {
			return &BatchCompressError{Cause: err}
		}

		// Record compression ratio
		ratio := float64(originalSize) / float64(len(compressed))
		bp.metrics.RecordCompressionRatio(ratio)

		data = compressed
		batchEvent.Metadata["compressed"] = "true"
		batchEvent.Metadata["compression_ratio"] = fmt.Sprintf("%.2f", ratio)
	}

	// Convert to base PolicyChangeEvent for syncer
	baseEvent := PolicyChangeEvent{
		Type:      PolicyChangeSnapshot, // Use snapshot for batch
		PolicyID:  "batch",
		TenantID:  bp.config.TenantID,
		Version:   time.Now().UnixNano(),
		Timestamp: time.Now(),
		Data:      data,
		NodeID:    bp.config.NodeID,
	}

	// Notify via syncer
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := bp.syncer.NotifyPolicyChange(ctx, baseEvent); err != nil {
		return &BatchNotifyError{Cause: err}
	}

	// Clear the batch
	bp.batch = bp.batch[:0]

	// Record metrics
	bp.metrics.RecordBatchSent()

	// Reset the flush timer
	if !bp.flushTimer.Stop() {
		select {
		case <-bp.flushTimer.C:
		default:
		}
	}
	bp.flushTimer.Reset(bp.config.BatchInterval)

	return nil
}

// compress compresses data using gzip.
func (bp *BatchProcessor) compress(data []byte) ([]byte, error) {
	bp.compressionBuffer.Reset()

	w, err := gzip.NewWriterLevel(bp.compressionBuffer, bp.config.CompressionLevel)
	if err != nil {
		return nil, err
	}

	if _, err := w.Write(data); err != nil {
		w.Close()
		return nil, err
	}

	if err := w.Close(); err != nil {
		return nil, err
	}

	return bp.compressionBuffer.Bytes(), nil
}

// decompress decompresses gzip data.
func decompress(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

// flushLoop periodically flushes the batch.
func (bp *BatchProcessor) flushLoop() {
	defer bp.wg.Done()

	for {
		select {
		case <-bp.stopCh:
			// Final flush before stopping
			bp.batchMu.Lock()
			_ = bp.flushLocked()
			bp.batchMu.Unlock()
			return

		case <-bp.flushTimer.C:
			bp.batchMu.Lock()
			_ = bp.flushLocked()
			bp.batchMu.Unlock()
		}
	}
}

// Close stops the batch processor and flushes pending events.
func (bp *BatchProcessor) Close() error {
	if !bp.closed.CompareAndSwap(false, true) {
		return nil // Already closed
	}

	// Stop the flush loop
	close(bp.stopCh)

	// Stop the timer
	if !bp.flushTimer.Stop() {
		select {
		case <-bp.flushTimer.C:
		default:
		}
	}

	// Wait for goroutines to finish
	bp.wg.Wait()

	// Close compressor if present
	if bp.compressor != nil {
		bp.compressor.Close()
	}

	return nil
}

// GetMetrics returns the current batch processing metrics.
func (bp *BatchProcessor) GetMetrics() *EnhancedSyncStats {
	return bp.metrics.GetStats()
}
