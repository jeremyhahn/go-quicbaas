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

package server

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// backupStorageAdapter wraps a storage.Storage to satisfy interfaces.Storage
// by adding the context-aware and maintenance methods required by the backup manager.
type backupStorageAdapter struct {
	inner storage.Storage
}

// Compile-time interface check.
var _ interfaces.Storage = (*backupStorageAdapter)(nil)

// newBackupStorageAdapter creates a new adapter that bridges storage.Storage
// to interfaces.Storage for use by the backup manager.
func newBackupStorageAdapter(s storage.Storage) *backupStorageAdapter {
	return &backupStorageAdapter{inner: s}
}

// Get retrieves the value for the given key.
func (a *backupStorageAdapter) Get(key []byte) ([]byte, error) {
	return a.inner.Get(key)
}

// Set stores a key-value pair.
func (a *backupStorageAdapter) Set(key, value []byte) error {
	return a.inner.Set(key, value)
}

// Delete removes a key-value pair.
func (a *backupStorageAdapter) Delete(key []byte) error {
	return a.inner.Delete(key)
}

// GetWithContext retrieves a value with context support for cancellation.
// Delegates to the underlying Get, checking context before proceeding.
func (a *backupStorageAdapter) GetWithContext(ctx context.Context, key []byte) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return a.inner.Get(key)
	}
}

// SetWithContext stores a value with context support for cancellation.
// Delegates to the underlying Set, checking context before proceeding.
func (a *backupStorageAdapter) SetWithContext(ctx context.Context, key, value []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return a.inner.Set(key, value)
	}
}

// Batch creates a new batch for atomic write operations.
func (a *backupStorageAdapter) Batch() storage.Batch {
	return a.inner.Batch()
}

// NewIterator creates a new iterator with the given options.
func (a *backupStorageAdapter) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	return a.inner.NewIterator(opts)
}

// Snapshot creates a consistent point-in-time view of the storage.
func (a *backupStorageAdapter) Snapshot() (storage.Snapshot, error) {
	return a.inner.Snapshot()
}

// Compact is a no-op for the file-based backup storage adapter.
// File storage does not require compaction.
func (a *backupStorageAdapter) Compact(_, _ []byte) error {
	return nil
}

// Flush is a no-op for the file-based backup storage adapter.
// File storage writes are synced on each operation when SyncWrites is enabled.
func (a *backupStorageAdapter) Flush() error {
	return nil
}

// Close closes the underlying storage and releases all resources.
func (a *backupStorageAdapter) Close() error {
	return a.inner.Close()
}
