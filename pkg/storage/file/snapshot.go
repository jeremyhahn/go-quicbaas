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
	"bytes"
	"os"
	"sort"
	"sync/atomic"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// fileSnapshot implements storage.Snapshot for file storage.
// It captures a copy of all data at snapshot creation time,
// providing a consistent point-in-time view.
type fileSnapshot struct {
	data   map[string][]byte
	keys   []string
	closed atomic.Bool
}

// Compile-time interface check.
var _ storage.Snapshot = (*fileSnapshot)(nil)

// newFileSnapshot creates a snapshot of the current storage state.
// The snapshot captures all key-value pairs at the time of creation.
// Must be called with storage read lock held.
func newFileSnapshot(store *FileStorage) (*fileSnapshot, error) {
	// Get all keys
	keys, err := store.listKeys()
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrStorage, "fileSnapshot.new")
	}

	// Read all values
	data := make(map[string][]byte, len(keys))
	for _, keyStr := range keys {
		path := store.keyToPath([]byte(keyStr))
		value, readErr := os.ReadFile(path)
		if readErr != nil {
			// Skip keys that can't be read (might have been deleted)
			if os.IsNotExist(readErr) {
				continue
			}
			return nil, errors.Wrap(readErr, errors.ErrStorage, "fileSnapshot.new")
		}

		// Deep copy value
		valueCopy := make([]byte, len(value))
		copy(valueCopy, value)
		data[keyStr] = valueCopy
	}

	return &fileSnapshot{
		data: data,
		keys: keys,
	}, nil
}

// Get retrieves the value for the given key from the snapshot.
// Returns ErrNotFound if the key does not exist.
func (s *fileSnapshot) Get(key []byte) ([]byte, error) {
	if s.closed.Load() {
		return nil, errors.New(errors.ErrStorage, "fileSnapshot.Get", "snapshot is closed")
	}

	if len(key) == 0 {
		return nil, errors.New(errors.ErrInvalidInput, "fileSnapshot.Get", "key cannot be empty")
	}

	value, exists := s.data[string(key)]
	if !exists {
		return nil, errors.New(errors.ErrNotFound, "fileSnapshot.Get", "key not found")
	}

	// Return a copy
	result := make([]byte, len(value))
	copy(result, value)
	return result, nil
}

// NewIterator creates an iterator over the snapshot.
func (s *fileSnapshot) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	if s.closed.Load() {
		closedIter := &snapshotIterator{}
		closedIter.closed.Store(true)
		return closedIter
	}

	return newSnapshotIterator(s, opts)
}

// Close releases resources held by the snapshot.
func (s *fileSnapshot) Close() error {
	s.closed.Store(true)
	s.data = nil
	s.keys = nil
	return nil
}

// snapshotIterator implements storage.Iterator for snapshot data.
type snapshotIterator struct {
	snapshot *fileSnapshot
	opts     *storage.IteratorOptions
	keys     []string
	current  int
	valid    bool
	closed   atomic.Bool
}

// Compile-time interface check.
var _ storage.Iterator = (*snapshotIterator)(nil)

// newSnapshotIterator creates a new iterator for a snapshot.
func newSnapshotIterator(snap *fileSnapshot, opts *storage.IteratorOptions) *snapshotIterator {
	if opts == nil {
		opts = &storage.IteratorOptions{}
	}

	// Filter and sort keys
	filteredKeys := make([]string, 0, len(snap.keys))
	for _, keyStr := range snap.keys {
		key := []byte(keyStr)

		// Apply lower bound (inclusive)
		if opts.LowerBound != nil && bytes.Compare(key, opts.LowerBound) < 0 {
			continue
		}

		// Apply upper bound (exclusive)
		if opts.UpperBound != nil && bytes.Compare(key, opts.UpperBound) >= 0 {
			continue
		}

		filteredKeys = append(filteredKeys, keyStr)
	}

	// Sort keys
	sort.Strings(filteredKeys)

	// Reverse if needed
	if opts.Reverse {
		for i, j := 0, len(filteredKeys)-1; i < j; i, j = i+1, j-1 {
			filteredKeys[i], filteredKeys[j] = filteredKeys[j], filteredKeys[i]
		}
	}

	return &snapshotIterator{
		snapshot: snap,
		opts:     opts,
		keys:     filteredKeys,
		current:  -1,
		valid:    false,
	}
}

// SeekToFirst positions the iterator at the first key.
func (it *snapshotIterator) SeekToFirst() bool {
	if it.closed.Load() || len(it.keys) == 0 {
		it.valid = false
		return false
	}

	it.current = 0
	it.valid = true
	return true
}

// SeekToLast positions the iterator at the last key.
func (it *snapshotIterator) SeekToLast() bool {
	if it.closed.Load() || len(it.keys) == 0 {
		it.valid = false
		return false
	}

	it.current = len(it.keys) - 1
	it.valid = true
	return true
}

// Seek positions the iterator at the first key >= target.
func (it *snapshotIterator) Seek(target []byte) bool {
	if it.closed.Load() || len(it.keys) == 0 {
		it.valid = false
		return false
	}

	targetStr := string(target)

	if it.opts.Reverse {
		// In reverse mode, find the last key <= target
		idx := sort.Search(len(it.keys), func(i int) bool {
			return it.keys[i] < targetStr
		})
		if idx > 0 {
			it.current = idx - 1
			it.valid = true
			return true
		}
		if len(it.keys) > 0 && it.keys[0] == targetStr {
			it.current = 0
			it.valid = true
			return true
		}
		it.valid = false
		return false
	}

	// Normal forward mode
	idx := sort.SearchStrings(it.keys, targetStr)
	if idx >= len(it.keys) {
		it.valid = false
		return false
	}

	it.current = idx
	it.valid = true
	return true
}

// Next advances the iterator to the next key.
func (it *snapshotIterator) Next() bool {
	if it.closed.Load() || !it.valid {
		it.valid = false
		return false
	}

	it.current++
	if it.current >= len(it.keys) {
		it.valid = false
		return false
	}

	return true
}

// Prev moves the iterator to the previous key.
func (it *snapshotIterator) Prev() bool {
	if it.closed.Load() || !it.valid {
		it.valid = false
		return false
	}

	it.current--
	if it.current < 0 {
		it.valid = false
		return false
	}

	return true
}

// Valid returns true if the iterator is positioned at a valid key.
func (it *snapshotIterator) Valid() bool {
	if it.closed.Load() {
		return false
	}
	return it.valid && it.current >= 0 && it.current < len(it.keys)
}

// Key returns the current key.
func (it *snapshotIterator) Key() []byte {
	if !it.Valid() {
		return nil
	}

	key := it.keys[it.current]
	result := make([]byte, len(key))
	copy(result, key)
	return result
}

// Value returns the current value.
func (it *snapshotIterator) Value() []byte {
	if !it.Valid() {
		return nil
	}

	if it.opts.OnlyKeys {
		return nil
	}

	value, exists := it.snapshot.data[it.keys[it.current]]
	if !exists {
		return nil
	}

	// Return a copy
	result := make([]byte, len(value))
	copy(result, value)
	return result
}

// Error returns any error that occurred during iteration.
func (it *snapshotIterator) Error() error {
	return nil
}

// Close releases resources held by the iterator.
func (it *snapshotIterator) Close() error {
	it.closed.Store(true)
	it.valid = false
	return nil
}
