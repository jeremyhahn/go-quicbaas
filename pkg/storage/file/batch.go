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

package file

import (
	"sync"
	"sync/atomic"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// batchOperation represents a single operation in a batch.
type batchOperation struct {
	key      []byte
	value    []byte
	isDelete bool
}

// fileBatch implements storage.Batch for file storage.
// Operations are buffered and applied atomically on commit.
// Note: File-based batch operations are not truly atomic across
// all files, but each individual file write is atomic.
type fileBatch struct {
	storage    *FileStorage
	operations []batchOperation
	mu         sync.Mutex
	closed     atomic.Bool
	committed  atomic.Bool
}

// Compile-time interface check.
var _ storage.Batch = (*fileBatch)(nil)

// newFileBatch creates a new batch for the given file storage.
func newFileBatch(store *FileStorage) *fileBatch {
	return &fileBatch{
		storage:    store,
		operations: make([]batchOperation, 0, 16),
	}
}

// Set adds a key-value pair to the batch.
// The key and value are copied to ensure batch integrity.
func (b *fileBatch) Set(key, value []byte) error {
	if b.closed.Load() {
		return errors.New(errors.ErrStorage, "fileBatch.Set", "batch is closed")
	}

	if b.committed.Load() {
		return errors.New(errors.ErrStorage, "fileBatch.Set", "batch is already committed")
	}

	if len(key) == 0 {
		return errors.New(errors.ErrInvalidInput, "fileBatch.Set", "key cannot be empty")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Copy key and value to ensure batch integrity
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)

	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	b.operations = append(b.operations, batchOperation{
		key:      keyCopy,
		value:    valueCopy,
		isDelete: false,
	})

	return nil
}

// Delete adds a key deletion to the batch.
// The key is copied to ensure batch integrity.
func (b *fileBatch) Delete(key []byte) error {
	if b.closed.Load() {
		return errors.New(errors.ErrStorage, "fileBatch.Delete", "batch is closed")
	}

	if b.committed.Load() {
		return errors.New(errors.ErrStorage, "fileBatch.Delete", "batch is already committed")
	}

	if len(key) == 0 {
		return errors.New(errors.ErrInvalidInput, "fileBatch.Delete", "key cannot be empty")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Copy key to ensure batch integrity
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)

	b.operations = append(b.operations, batchOperation{
		key:      keyCopy,
		isDelete: true,
	})

	return nil
}

// Commit applies all operations in the batch to the storage.
// The batch cannot be used after commit.
// Note: Operations are applied sequentially with the storage lock held,
// ensuring consistency but not true atomicity across all files.
func (b *fileBatch) Commit() error {
	if b.closed.Load() {
		return errors.New(errors.ErrStorage, "fileBatch.Commit", "batch is closed")
	}

	if !b.committed.CompareAndSwap(false, true) {
		return errors.New(errors.ErrStorage, "fileBatch.Commit", "batch is already committed")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Acquire storage lock for the entire batch
	b.storage.mu.Lock()
	defer b.storage.mu.Unlock()

	// Apply all operations
	for _, op := range b.operations {
		var err error
		if op.isDelete {
			err = b.storage.deleteUnlocked(op.key)
		} else {
			err = b.storage.setUnlocked(op.key, op.value)
		}
		if err != nil {
			return errors.Wrap(err, errors.ErrStorage, "fileBatch.Commit")
		}
	}

	return nil
}

// Rollback discards all operations in the batch.
// The batch cannot be used after rollback.
func (b *fileBatch) Rollback() error {
	if b.closed.Load() {
		return errors.New(errors.ErrStorage, "fileBatch.Rollback", "batch is closed")
	}

	if b.committed.Load() {
		return errors.New(errors.ErrStorage, "fileBatch.Rollback", "batch is already committed")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.operations = nil
	b.closed.Store(true)

	return nil
}

// Close releases resources held by the batch.
// If the batch was not committed or rolled back, it will be rolled back.
func (b *fileBatch) Close() error {
	if b.closed.Load() {
		return nil
	}

	if !b.committed.Load() {
		return b.Rollback()
	}

	b.closed.Store(true)
	return nil
}

// Len returns the number of operations in the batch.
func (b *fileBatch) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.operations)
}
