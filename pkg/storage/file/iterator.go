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
	"sort"
	"sync/atomic"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// fileIterator implements storage.Iterator for file storage.
// Keys are loaded once at iterator creation and values are loaded on demand.
type fileIterator struct {
	storage *FileStorage
	opts    *storage.IteratorOptions
	keys    []string
	current int
	valid   bool
	err     error
	closed  atomic.Bool
}

// Compile-time interface check.
var _ storage.Iterator = (*fileIterator)(nil)

// newFileIterator creates a new iterator for file storage.
// Keys are loaded at creation time and filtered by bounds.
func newFileIterator(store *FileStorage, opts *storage.IteratorOptions) *fileIterator {
	if opts == nil {
		opts = &storage.IteratorOptions{}
	}

	it := &fileIterator{
		storage: store,
		opts:    opts,
		current: -1,
		valid:   false,
	}

	// Load and filter keys
	it.loadKeys()

	return it
}

// loadKeys loads all keys from storage and applies bounds filtering.
func (it *fileIterator) loadKeys() {
	it.storage.mu.RLock()
	defer it.storage.mu.RUnlock()

	if it.storage.closed {
		it.err = errors.New(errors.ErrStorage, "fileIterator.loadKeys", "storage is closed")
		return
	}

	allKeys, err := it.storage.listKeys()
	if err != nil {
		it.err = err
		return
	}

	// Filter keys by bounds
	filteredKeys := make([]string, 0, len(allKeys))
	for _, keyStr := range allKeys {
		key := []byte(keyStr)

		// Apply lower bound (inclusive)
		if it.opts.LowerBound != nil && bytes.Compare(key, it.opts.LowerBound) < 0 {
			continue
		}

		// Apply upper bound (exclusive)
		if it.opts.UpperBound != nil && bytes.Compare(key, it.opts.UpperBound) >= 0 {
			continue
		}

		filteredKeys = append(filteredKeys, keyStr)
	}

	// Reverse if needed
	if it.opts.Reverse {
		for i, j := 0, len(filteredKeys)-1; i < j; i, j = i+1, j-1 {
			filteredKeys[i], filteredKeys[j] = filteredKeys[j], filteredKeys[i]
		}
	}

	it.keys = filteredKeys
}

// SeekToFirst positions the iterator at the first key.
// Returns false if there are no keys.
func (it *fileIterator) SeekToFirst() bool {
	if it.closed.Load() || len(it.keys) == 0 {
		it.valid = false
		return false
	}

	it.current = 0
	it.valid = true
	return true
}

// SeekToLast positions the iterator at the last key.
// Returns false if there are no keys.
func (it *fileIterator) SeekToLast() bool {
	if it.closed.Load() || len(it.keys) == 0 {
		it.valid = false
		return false
	}

	it.current = len(it.keys) - 1
	it.valid = true
	return true
}

// Seek positions the iterator at the first key >= target.
// Returns false if no such key exists.
func (it *fileIterator) Seek(target []byte) bool {
	if it.closed.Load() || len(it.keys) == 0 {
		it.valid = false
		return false
	}

	targetStr := string(target)

	if it.opts.Reverse {
		// In reverse mode, find the last key <= target
		idx := sort.Search(len(it.keys), func(i int) bool {
			// Keys are in reverse order, so compare in reverse
			return it.keys[i] < targetStr
		})
		// idx points to first key < target, so idx-1 is the key >= target (in reverse order)
		if idx > 0 {
			it.current = idx - 1
			it.valid = true
			return true
		}
		// Check if the last element matches
		if len(it.keys) > 0 && it.keys[0] == targetStr {
			it.current = 0
			it.valid = true
			return true
		}
		it.valid = false
		return false
	}

	// Normal forward mode: find first key >= target
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
// Returns false if there are no more keys.
func (it *fileIterator) Next() bool {
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
// Returns false if there are no more keys.
func (it *fileIterator) Prev() bool {
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
func (it *fileIterator) Valid() bool {
	if it.closed.Load() {
		return false
	}
	return it.valid && it.current >= 0 && it.current < len(it.keys)
}

// Key returns the current key.
// Only valid if Valid() returns true.
// Returns a copy of the key.
func (it *fileIterator) Key() []byte {
	if !it.Valid() {
		return nil
	}

	key := it.keys[it.current]
	result := make([]byte, len(key))
	copy(result, key)
	return result
}

// Value returns the current value.
// Only valid if Valid() returns true.
// Returns nil if OnlyKeys option is set.
// Values are loaded from disk on each call.
func (it *fileIterator) Value() []byte {
	if !it.Valid() {
		return nil
	}

	if it.opts.OnlyKeys {
		return nil
	}

	key := it.keys[it.current]
	value, err := it.storage.Get([]byte(key))
	if err != nil {
		it.err = err
		return nil
	}

	return value
}

// Error returns any error that occurred during iteration.
func (it *fileIterator) Error() error {
	return it.err
}

// Close releases resources held by the iterator.
func (it *fileIterator) Close() error {
	it.closed.Store(true)
	it.valid = false
	it.keys = nil
	return nil
}
