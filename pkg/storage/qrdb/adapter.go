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

package qrdb

import (
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	dragonStorage "github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// StorageAdapter wraps a quicbaas storage.Storage and translates
// not-found errors to (nil, nil) per the qrdb SDK storage contract.
type StorageAdapter struct {
	inner dragonStorage.Storage
}

// NewStorageAdapter creates a StorageAdapter that delegates all
// operations to the provided quicbaas Storage.
func NewStorageAdapter(s dragonStorage.Storage) *StorageAdapter {
	return &StorageAdapter{inner: s}
}

// Get retrieves a value by key. quicbaas's PebbleDB returns a typed
// DragonError with ErrNotFound for missing keys, but the SDK storage
// contract expects (nil, nil). The adapter translates accordingly.
func (a *StorageAdapter) Get(key []byte) ([]byte, error) {
	val, err := a.inner.Get(key)
	if err != nil && dberrors.IsNotFound(err) {
		return nil, nil
	}
	return val, err
}

// Set stores a key-value pair.
func (a *StorageAdapter) Set(key, value []byte) error {
	return a.inner.Set(key, value)
}

// Delete removes a key.
func (a *StorageAdapter) Delete(key []byte) error {
	return a.inner.Delete(key)
}

// Exists checks if a key exists.
func (a *StorageAdapter) Exists(key []byte) (bool, error) {
	return a.inner.Exists(key)
}

// List returns all keys matching the given prefix.
func (a *StorageAdapter) List(prefix []byte) ([][]byte, error) {
	return a.inner.List(prefix)
}

// Batch creates a new batch operation.
func (a *StorageAdapter) Batch() dragonStorage.Batch {
	return a.inner.Batch()
}

// NewIterator creates a new iterator with the given options.
func (a *StorageAdapter) NewIterator(opts *dragonStorage.IteratorOptions) dragonStorage.Iterator {
	return a.inner.NewIterator(opts)
}

// Snapshot creates a read-only snapshot of the storage.
func (a *StorageAdapter) Snapshot() (dragonStorage.Snapshot, error) {
	return a.inner.Snapshot()
}

// Close closes the underlying storage.
func (a *StorageAdapter) Close() error {
	return a.inner.Close()
}

// Compile-time assertion.
var _ dragonStorage.Storage = (*StorageAdapter)(nil)
