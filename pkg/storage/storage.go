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

// Package storage provides the storage abstraction layer for QuicBaaS.
// It defines the core interfaces for key-value storage engines, batch
// operations, iterators, and point-in-time snapshots.
package storage

// Storage is the unified key-value storage interface used by all backends.
// Implementations must be safe for concurrent use by multiple goroutines.
type Storage interface {
	// Get retrieves the value associated with the given key.
	// Returns an error if the key does not exist or the read fails.
	Get(key []byte) ([]byte, error)

	// Set stores the given key-value pair, overwriting any existing value.
	Set(key, value []byte) error

	// Delete removes the given key and its associated value.
	Delete(key []byte) error

	// List returns all keys that share the given prefix.
	List(prefix []byte) ([][]byte, error)

	// Exists reports whether the given key exists in storage.
	Exists(key []byte) (bool, error)

	// Batch creates a new atomic batch operation.
	Batch() Batch

	// NewIterator creates an iterator over key-value pairs scoped by opts.
	NewIterator(opts *IteratorOptions) Iterator

	// Snapshot creates a consistent point-in-time view of the storage.
	Snapshot() (Snapshot, error)

	// Close releases all resources held by the storage engine.
	Close() error
}

// Batch groups multiple write operations into a single atomic unit.
// All mutations are applied together on Commit or discarded on Rollback.
type Batch interface {
	// Set stages a key-value write within the batch.
	Set(key, value []byte) error

	// Delete stages a key deletion within the batch.
	Delete(key []byte) error

	// Commit atomically applies all staged mutations.
	Commit() error

	// Rollback discards all staged mutations without applying them.
	Rollback() error

	// Close releases all resources held by the batch.
	Close() error
}

// Iterator scans key-value pairs within a range defined by IteratorOptions.
// Callers must call Close when the iterator is no longer needed.
type Iterator interface {
	// SeekToFirst positions the iterator at the first key in the range.
	// Returns true if a valid entry exists.
	SeekToFirst() bool

	// SeekToLast positions the iterator at the last key in the range.
	// Returns true if a valid entry exists.
	SeekToLast() bool

	// Seek positions the iterator at the first key >= target.
	// Returns true if a valid entry exists at or after the target.
	Seek(target []byte) bool

	// Next advances the iterator to the next key-value pair.
	// Returns true if the iterator is positioned at a valid entry.
	Next() bool

	// Prev moves the iterator to the previous key-value pair.
	// Returns true if the iterator is positioned at a valid entry.
	Prev() bool

	// Valid reports whether the iterator is positioned at a valid entry.
	Valid() bool

	// Key returns the key at the current iterator position.
	Key() []byte

	// Value returns the value at the current iterator position.
	Value() []byte

	// Error returns any accumulated error encountered during iteration.
	Error() error

	// Close releases all resources held by the iterator.
	Close() error
}

// Snapshot provides a consistent point-in-time read-only view of the storage.
// Callers must call Close when the snapshot is no longer needed.
type Snapshot interface {
	// Get retrieves the value associated with the given key at the snapshot point.
	Get(key []byte) ([]byte, error)

	// NewIterator creates an iterator over the snapshot scoped by opts.
	NewIterator(opts *IteratorOptions) Iterator

	// Close releases all resources held by the snapshot.
	Close() error
}

// IteratorOptions configures the behavior of a storage iterator.
type IteratorOptions struct {
	// LowerBound sets the inclusive lower bound for the iteration range.
	LowerBound []byte

	// UpperBound sets the exclusive upper bound for the iteration range.
	UpperBound []byte

	// Reverse iterates in reverse key order when true.
	Reverse bool

	// OnlyKeys skips loading values when true, improving scan performance
	// for key-only operations.
	OnlyKeys bool
}
