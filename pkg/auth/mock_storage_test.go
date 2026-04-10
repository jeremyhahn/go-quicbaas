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

// Package auth provides mock storage for testing
package auth

import (
	"context"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// MockStorage implements interfaces.Storage for testing.
type MockStorage struct {
	data   map[string][]byte
	mu     sync.RWMutex
	closed bool
}

// NewMockStorage creates a new mock storage.
func NewMockStorage() *MockStorage {
	return &MockStorage{
		data: make(map[string][]byte),
	}
}

// Get retrieves a value.
func (m *MockStorage) Get(key []byte) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, api.NewStorageClosedAuthError("MockStorage.Get")
	}

	value, exists := m.data[string(key)]
	if !exists {
		return nil, api.NewKeyNotFoundError("MockStorage.Get")
	}
	return value, nil
}

// Set stores a value.
func (m *MockStorage) Set(key, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return api.NewStorageClosedAuthError("MockStorage.Set")
	}

	m.data[string(key)] = value
	return nil
}

// Delete removes a value.
func (m *MockStorage) Delete(key []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return api.NewStorageClosedAuthError("MockStorage.Delete")
	}

	delete(m.data, string(key))
	return nil
}

// GetWithContext retrieves a value with context.
func (m *MockStorage) GetWithContext(ctx context.Context, key []byte) ([]byte, error) {
	return m.Get(key)
}

// SetWithContext stores a value with context.
func (m *MockStorage) SetWithContext(ctx context.Context, key, value []byte) error {
	return m.Set(key, value)
}

// Batch creates a new batch.
func (m *MockStorage) Batch() storage.Batch {
	return &mockBatch{
		storage:    m,
		operations: make([]batchOp, 0),
	}
}

// NewIterator creates a new iterator.
func (m *MockStorage) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	return &mockIterator{
		storage: m,
		opts:    opts,
		index:   -1,
	}
}

// Snapshot creates a snapshot (not implemented for mock).
func (m *MockStorage) Snapshot() (storage.Snapshot, error) {
	return nil, api.NewSnapshotNotSupportedError("MockStorage.Snapshot")
}

// Compact performs compaction (no-op for mock).
func (m *MockStorage) Compact(start, end []byte) error {
	return nil
}

// Flush flushes data (no-op for mock).
func (m *MockStorage) Flush() error {
	return nil
}

// Close closes the storage.
func (m *MockStorage) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// batchOp represents a batch operation.
type batchOp struct {
	isDelete bool
	key      []byte
	value    []byte
}

// mockBatch implements storage.Batch for testing.
type mockBatch struct {
	storage    *MockStorage
	operations []batchOp
	committed  bool
	rolledBack bool
}

// Set adds a set operation to the batch.
func (b *mockBatch) Set(key, value []byte) error {
	if b.committed || b.rolledBack {
		return &dberrors.MessageError{Message: "batch already finalized"}
	}
	b.operations = append(b.operations, batchOp{
		isDelete: false,
		key:      key,
		value:    value,
	})
	return nil
}

// Delete adds a delete operation to the batch.
func (b *mockBatch) Delete(key []byte) error {
	if b.committed || b.rolledBack {
		return &dberrors.MessageError{Message: "batch already finalized"}
	}
	b.operations = append(b.operations, batchOp{
		isDelete: true,
		key:      key,
	})
	return nil
}

// Commit applies all operations.
func (b *mockBatch) Commit() error {
	if b.committed || b.rolledBack {
		return &dberrors.MessageError{Message: "batch already finalized"}
	}

	b.storage.mu.Lock()
	defer b.storage.mu.Unlock()

	for _, op := range b.operations {
		if op.isDelete {
			delete(b.storage.data, string(op.key))
		} else {
			b.storage.data[string(op.key)] = op.value
		}
	}

	b.committed = true
	return nil
}

// Rollback discards all operations.
func (b *mockBatch) Rollback() error {
	if b.committed || b.rolledBack {
		return &dberrors.MessageError{Message: "batch already finalized"}
	}
	b.rolledBack = true
	return nil
}

// Close closes the batch.
func (b *mockBatch) Close() error {
	if !b.committed && !b.rolledBack {
		return b.Rollback()
	}
	return nil
}

// mockIterator implements storage.Iterator for testing.
type mockIterator struct {
	storage *MockStorage
	opts    *storage.IteratorOptions
	keys    []string
	index   int
	err     error
	mu      sync.RWMutex
}

// buildKeys populates the keys list based on storage and options.
func (it *mockIterator) buildKeys() {
	it.mu.Lock()
	defer it.mu.Unlock()

	if it.keys != nil {
		return
	}

	it.storage.mu.RLock()
	defer it.storage.mu.RUnlock()

	it.keys = make([]string, 0, len(it.storage.data))
	for k := range it.storage.data {
		// Apply bounds filtering
		if it.opts != nil {
			if it.opts.LowerBound != nil && k < string(it.opts.LowerBound) {
				continue
			}
			if it.opts.UpperBound != nil && k >= string(it.opts.UpperBound) {
				continue
			}
		}
		it.keys = append(it.keys, k)
	}

	// Sort keys (simple string sort for mock)
	// In a real implementation, this would be lexicographic byte sorting
	for i := 0; i < len(it.keys)-1; i++ {
		for j := i + 1; j < len(it.keys); j++ {
			if it.keys[i] > it.keys[j] {
				it.keys[i], it.keys[j] = it.keys[j], it.keys[i]
			}
		}
	}

	if it.opts != nil && it.opts.Reverse {
		// Reverse the order
		for i, j := 0, len(it.keys)-1; i < j; i, j = i+1, j-1 {
			it.keys[i], it.keys[j] = it.keys[j], it.keys[i]
		}
	}
}

// SeekToFirst positions at the first key.
func (it *mockIterator) SeekToFirst() bool {
	it.buildKeys()
	it.mu.Lock()
	defer it.mu.Unlock()
	if len(it.keys) == 0 {
		it.index = -1
		return false
	}
	it.index = 0
	return true
}

// SeekToLast positions at the last key.
func (it *mockIterator) SeekToLast() bool {
	it.buildKeys()
	it.mu.Lock()
	defer it.mu.Unlock()
	if len(it.keys) == 0 {
		it.index = -1
		return false
	}
	it.index = len(it.keys) - 1
	return true
}

// Seek positions at the first key >= target.
func (it *mockIterator) Seek(target []byte) bool {
	it.buildKeys()
	it.mu.Lock()
	defer it.mu.Unlock()
	targetStr := string(target)
	for i, k := range it.keys {
		if k >= targetStr {
			it.index = i
			return true
		}
	}
	it.index = -1
	return false
}

// Next advances to the next key.
func (it *mockIterator) Next() bool {
	if it.keys == nil {
		it.buildKeys()
	}
	it.mu.Lock()
	defer it.mu.Unlock()
	it.index++
	return it.index < len(it.keys)
}

// Prev moves to the previous key.
func (it *mockIterator) Prev() bool {
	if it.keys == nil {
		it.buildKeys()
	}
	it.mu.Lock()
	defer it.mu.Unlock()
	it.index--
	return it.index >= 0
}

// Valid returns true if positioned at a valid key.
func (it *mockIterator) Valid() bool {
	it.mu.RLock()
	defer it.mu.RUnlock()
	return it.index >= 0 && it.index < len(it.keys)
}

// Key returns the current key.
func (it *mockIterator) Key() []byte {
	it.mu.RLock()
	defer it.mu.RUnlock()
	if it.index < 0 || it.index >= len(it.keys) {
		return nil
	}
	return []byte(it.keys[it.index])
}

// Value returns the current value.
func (it *mockIterator) Value() []byte {
	it.mu.RLock()
	if it.index < 0 || it.index >= len(it.keys) {
		it.mu.RUnlock()
		return nil
	}
	key := it.keys[it.index]
	it.mu.RUnlock()

	it.storage.mu.RLock()
	defer it.storage.mu.RUnlock()

	return it.storage.data[key]
}

// Error returns any error.
func (it *mockIterator) Error() error {
	return it.err
}

// Close closes the iterator.
func (it *mockIterator) Close() error {
	return nil
}
