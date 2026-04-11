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

package lease

import (
	"bytes"
	"sort"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// mockStorage is a simple in-memory storage implementation for testing.
type mockStorage struct {
	data map[string][]byte
	mu   sync.RWMutex
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		data: make(map[string][]byte),
	}
}

func (m *mockStorage) Get(key []byte) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	value, exists := m.data[string(key)]
	if !exists {
		return nil, &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "mockStorage.Get",
		}
	}

	// Return copy
	result := make([]byte, len(value))
	copy(result, value)
	return result, nil
}

func (m *mockStorage) Set(key, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Store copy
	val := make([]byte, len(value))
	copy(val, value)
	m.data[string(key)] = val
	return nil
}

func (m *mockStorage) Delete(key []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.data, string(key))
	return nil
}

func (m *mockStorage) Batch() storage.Batch {
	return &mockBatch{store: m}
}

func (m *mockStorage) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect keys in range
	var keys []string
	for k := range m.data {
		if opts != nil {
			if opts.LowerBound != nil && bytes.Compare([]byte(k), opts.LowerBound) < 0 {
				continue
			}
			if opts.UpperBound != nil && bytes.Compare([]byte(k), opts.UpperBound) >= 0 {
				continue
			}
		}
		keys = append(keys, k)
	}

	// Sort keys
	sort.Strings(keys)

	// Create copies of data for iteration
	items := make([][2][]byte, 0, len(keys))
	for _, k := range keys {
		key := []byte(k)
		value := m.data[k]

		keyCopy := make([]byte, len(key))
		copy(keyCopy, key)

		valCopy := make([]byte, len(value))
		copy(valCopy, value)

		items = append(items, [2][]byte{keyCopy, valCopy})
	}

	return &mockIterator{
		items: items,
		index: -1,
	}
}

func (m *mockStorage) Snapshot() (storage.Snapshot, error) {
	return nil, nil
}

func (m *mockStorage) Close() error {
	return nil
}

func (m *mockStorage) Exists(key []byte) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[string(key)]
	return ok, nil
}

func (m *mockStorage) List(prefix []byte) ([][]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var keys [][]byte
	for k := range m.data {
		if len(prefix) == 0 || bytes.HasPrefix([]byte(k), prefix) {
			keys = append(keys, []byte(k))
		}
	}
	return keys, nil
}

// mockBatch is a simple batch implementation for testing.
type mockBatch struct {
	store *mockStorage
	ops   []func() error
}

func (b *mockBatch) Set(key, value []byte) error {
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)
	valCopy := make([]byte, len(value))
	copy(valCopy, value)

	b.ops = append(b.ops, func() error {
		return b.store.Set(keyCopy, valCopy)
	})
	return nil
}

func (b *mockBatch) Delete(key []byte) error {
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)

	b.ops = append(b.ops, func() error {
		return b.store.Delete(keyCopy)
	})
	return nil
}

func (b *mockBatch) Commit() error {
	for _, op := range b.ops {
		if err := op(); err != nil {
			return err
		}
	}
	b.ops = nil
	return nil
}

func (b *mockBatch) Rollback() error {
	b.ops = nil
	return nil
}

func (b *mockBatch) Close() error {
	return b.Rollback()
}

// mockIterator is a simple iterator implementation for testing.
type mockIterator struct {
	items [][2][]byte
	index int
}

func (it *mockIterator) SeekToFirst() bool {
	it.index = -1
	return it.Next()
}

func (it *mockIterator) SeekToLast() bool {
	it.index = len(it.items)
	return it.Prev()
}

func (it *mockIterator) Seek(target []byte) bool {
	for i, item := range it.items {
		if bytes.Compare(item[0], target) >= 0 {
			it.index = i
			return true
		}
	}
	it.index = len(it.items)
	return false
}

func (it *mockIterator) Next() bool {
	it.index++
	return it.index < len(it.items)
}

func (it *mockIterator) Prev() bool {
	it.index--
	return it.index >= 0
}

func (it *mockIterator) Valid() bool {
	return it.index >= 0 && it.index < len(it.items)
}

func (it *mockIterator) Key() []byte {
	if !it.Valid() {
		return nil
	}
	return it.items[it.index][0]
}

func (it *mockIterator) Value() []byte {
	if !it.Valid() {
		return nil
	}
	return it.items[it.index][1]
}

func (it *mockIterator) Error() error {
	return nil
}

func (it *mockIterator) Close() error {
	it.items = nil
	return nil
}
