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

package catalog

import (
	"context"
	"errors"
)

// KV is the ordered key-value store the catalog's storage and indexes run on.
// Implementations must give Apply all-or-nothing semantics: a catalog entry
// and its index keys are written in one batch and must land together.
type KV interface {
	// Get returns the value stored under key. A missing key returns a
	// *KVKeyNotFoundError.
	Get(ctx context.Context, key []byte) ([]byte, error)

	// Scan calls fn for every key that starts with prefix, in ascending key
	// order, and stops as soon as fn returns false. The slices passed to fn
	// may be reused after fn returns; fn copies what it keeps.
	Scan(ctx context.Context, prefix []byte, fn func(key, value []byte) bool) error

	// Apply commits every operation in batch, in order, or none of them.
	Apply(ctx context.Context, batch *KVBatch) error

	// Close releases the store.
	Close() error
}

// KVOp is one operation of a KVBatch. Exported for KV adapters.
type KVOp struct {
	Key    []byte
	Value  []byte
	Delete bool
}

// KVBatch is an ordered list of puts and deletes applied atomically by
// KV.Apply. The zero value is an empty batch ready for use.
type KVBatch struct {
	ops []KVOp
}

// Set records a put of value under key. Both slices are copied.
func (b *KVBatch) Set(key, value []byte) {
	b.ops = append(b.ops, KVOp{Key: cloneBytes(key), Value: cloneBytes(value)})
}

// Delete records a delete of key. The slice is copied.
func (b *KVBatch) Delete(key []byte) {
	b.ops = append(b.ops, KVOp{Key: cloneBytes(key), Delete: true})
}

// Len returns the number of operations in the batch.
func (b *KVBatch) Len() int { return len(b.ops) }

// Ops returns the batch's operations in the order they were recorded.
func (b *KVBatch) Ops() []KVOp { return b.ops }

// KVKeyNotFoundError is returned by KV.Get when no value is stored under Key.
type KVKeyNotFoundError struct {
	Key string
}

// Error implements the error interface.
func (e *KVKeyNotFoundError) Error() string {
	return "catalog kv: key not found: " + e.Key
}

// isKVKeyNotFound reports whether err is a missing-key error from a KV.
func isKVKeyNotFound(err error) bool {
	var notFound *KVKeyNotFoundError
	return errors.As(err, &notFound)
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
