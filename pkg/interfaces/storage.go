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

package interfaces

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// Storage defines the interface for the underlying key-value storage engine.
// It provides ACID guarantees and supports concurrent operations.
type Storage interface {
	// Basic operations

	// Get retrieves the value for the given key.
	// Returns ErrNotFound if the key does not exist.
	Get(key []byte) ([]byte, error)

	// Set stores a key-value pair.
	// Overwrites the value if the key already exists.
	Set(key, value []byte) error

	// Delete removes a key-value pair.
	// Does not return an error if the key does not exist.
	Delete(key []byte) error

	// Context-aware operations

	// GetWithContext retrieves a value with context support for cancellation.
	// The operation can be cancelled via the provided context.
	GetWithContext(ctx context.Context, key []byte) ([]byte, error)

	// SetWithContext stores a value with context support for cancellation.
	// The operation can be cancelled via the provided context.
	SetWithContext(ctx context.Context, key, value []byte) error

	// Batch operations

	// Batch creates a new batch for atomic write operations.
	// The batch must be committed or rolled back explicitly.
	Batch() storage.Batch

	// Iterator operations

	// NewIterator creates a new iterator with the given options.
	// The iterator must be closed when done to release resources.
	NewIterator(opts *storage.IteratorOptions) storage.Iterator

	// Snapshot operations

	// Snapshot creates a consistent point-in-time view of the storage.
	// The snapshot must be closed when done to release resources.
	Snapshot() (storage.Snapshot, error)

	// Maintenance operations

	// Compact manually triggers compaction for a given key range.
	// Pass nil for start/end to compact the entire database.
	Compact(start, end []byte) error

	// Flush manually flushes memtables to disk.
	// Ensures all pending writes are persisted.
	Flush() error

	// Lifecycle

	// Close closes the storage and releases all resources.
	// All pending operations must complete before closing.
	Close() error
}

// StorageWithMetrics extends Storage with metrics reporting capabilities.
// Implementations can expose internal metrics for monitoring.
type StorageWithMetrics interface {
	Storage

	// Metrics returns storage engine-specific metrics.
	// The returned type is implementation-specific (e.g., *pebble.Metrics).
	Metrics() interface{}
}
