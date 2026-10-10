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

// Package kvstore adapts a qrdb storage engine to the quicbaas
// interfaces.KVStore contract.
//
// QuicBaaS does not implement storage engines. qrdb owns them and re-exports
// them through its Go SDK (qrdbsdk.Storage, qrdbsdk.NewMemoryStorage,
// qrdbsdk.NewPebbleStorage, ...). This package is the single, thin bridge
// between that engine interface (byte-slice keys, iterator-based scans) and
// the six-verb, string-keyed interfaces.KVStore that every quicbaas component
// consumes.
//
// The practical consequence is that a component written against
// interfaces.KVStore is backend-agnostic: injecting NewMemory() makes it a
// fast in-process store for unit tests, and injecting the server's
// qrdb-SDK-backed store makes the very same code durable, replicated and
// barrier-encrypted at rest. There is no second implementation to keep in
// sync.
package kvstore

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Store implements interfaces.KVStore over a qrdb storage engine.
type Store struct {
	engine qrdbsdk.Storage

	// closed guards iteration after Close. The engines report a closed store
	// from their point lookups, but an iterator created on a closed engine can
	// still walk its last snapshot — so a Scan would silently return records
	// (or nothing) instead of an error. A store that reports "no records" when
	// it is actually unusable is the worst possible answer for a tenant
	// listing, so the adapter tracks closure itself rather than relying on
	// engine behaviour.
	closed atomic.Bool

	// writeMu serializes every write through this Store, so CompareAndSwap's
	// read and its write are one step against Put and Delete as well as
	// against other swaps. It arbitrates only within this process — which is
	// all an engine owned by one process is ever shared across.
	writeMu sync.Mutex
}

// Compile-time interface satisfaction check.
var _ interfaces.KVStore = (*Store)(nil)

// New wraps an existing qrdb storage engine. The caller retains ownership of
// the engine and is responsible for closing it.
func New(engine qrdbsdk.Storage) *Store {
	return &Store{engine: engine}
}

// NewMemory returns a store backed by qrdb's in-memory storage engine. The
// engine is an ordered B-tree, so Scan and List observe keys in lexical order
// exactly like the durable engines do. This is the backend unit tests should
// use: it needs no disk, no daemon and no barrier.
func NewMemory() *Store {
	return New(qrdbsdk.NewMemoryStorage())
}

// Put stores a key-value pair.
func (s *Store) Put(_ context.Context, key string, value []byte) error {
	if key == "" {
		return &EmptyKeyError{Op: "put"}
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.engine.Set([]byte(key), value); err != nil {
		return &OpError{Op: "put", Key: key, Cause: err}
	}
	return nil
}

// CompareAndSwap replaces key's value with next only while it still holds
// expected, and reports whether it did. An absent key is left absent: a swap
// edits a record, it never creates one.
func (s *Store) CompareAndSwap(_ context.Context, key string, expected, next []byte) (bool, error) {
	if key == "" {
		return false, &EmptyKeyError{Op: "compare-and-swap"}
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	held, err := s.engine.Get([]byte(key))
	if err != nil {
		return false, &OpError{Op: "compare-and-swap", Key: key, Cause: err}
	}
	if held == nil || !bytes.Equal(held, expected) {
		return false, nil
	}
	if err := s.engine.Set([]byte(key), next); err != nil {
		return false, &OpError{Op: "compare-and-swap", Key: key, Cause: err}
	}
	return true, nil
}

// Get retrieves a value by key. A missing key yields the canonical quicbaas
// not-found error so callers can keep using errors.IsNotFound regardless of
// which engine backs the store.
func (s *Store) Get(_ context.Context, key string) ([]byte, error) {
	if key == "" {
		return nil, notFound("kv.get")
	}
	value, err := s.engine.Get([]byte(key))
	if err != nil {
		return nil, &OpError{Op: "get", Key: key, Cause: err}
	}
	if value == nil {
		return nil, notFound("kv.get")
	}
	return value, nil
}

// Delete removes a key. Deleting a key that does not exist is a no-op.
func (s *Store) Delete(_ context.Context, key string) error {
	if key == "" {
		return nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.engine.Delete([]byte(key)); err != nil {
		return &OpError{Op: "delete", Key: key, Cause: err}
	}
	return nil
}

// Scan iterates key-value pairs under prefix in lexical order, invoking fn for
// each. Returning a non-nil error from fn stops iteration and propagates that
// error.
//
// The scan is bounded by the prefix range rather than filtering a full-keyspace
// walk, and the iterator is always closed. Keys and values are copied before fn
// sees them because the engine only guarantees its slices until the next
// iterator operation.
func (s *Store) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	if s.closed.Load() {
		return &OpError{Op: "scan", Key: prefix, Cause: ErrClosed}
	}

	iter := s.engine.NewIterator(prefixBounds(prefix))
	defer func() {
		// Close never reports anything the caller can act on for a read-only
		// iterator; the deferred call exists to release engine resources.
		_ = iter.Close()
	}()

	for ok := iter.SeekToFirst(); ok; ok = iter.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		value := iter.Value()
		valueCopy := make([]byte, len(value))
		copy(valueCopy, value)

		if err := fn(string(iter.Key()), valueCopy); err != nil {
			return err
		}
	}
	if err := iter.Error(); err != nil {
		return &OpError{Op: "scan", Key: prefix, Cause: err}
	}
	return nil
}

// List returns every key under prefix in lexical order.
func (s *Store) List(ctx context.Context, prefix string) ([]string, error) {
	if s.closed.Load() {
		return nil, &OpError{Op: "list", Key: prefix, Cause: ErrClosed}
	}

	iter := s.engine.NewIterator(keysOnlyBounds(prefix))
	defer func() {
		_ = iter.Close()
	}()

	var keys []string
	for ok := iter.SeekToFirst(); ok; ok = iter.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		keys = append(keys, string(iter.Key()))
	}
	if err := iter.Error(); err != nil {
		return nil, &OpError{Op: "list", Key: prefix, Cause: err}
	}
	return keys, nil
}

// Exists reports whether key is present.
func (s *Store) Exists(_ context.Context, key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	exists, err := s.engine.Exists([]byte(key))
	if err != nil {
		return false, &OpError{Op: "exists", Key: key, Cause: err}
	}
	return exists, nil
}

// Close releases the underlying engine. Close is idempotent.
func (s *Store) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	if err := s.engine.Close(); err != nil {
		return &OpError{Op: "close", Cause: err}
	}
	return nil
}

// notFound builds the canonical quicbaas not-found error for a missing key.
// Every KVStore implementation in the tree reports missing keys this way, and
// pkg/errors.IsNotFound is the matching predicate.
func notFound(op string) error {
	return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: op}
}

// prefixBounds converts a string prefix into an inclusive/exclusive key range.
// An empty prefix scans the whole keyspace.
func prefixBounds(prefix string) *qrdbsdk.StorageIteratorOptions {
	if prefix == "" {
		return &qrdbsdk.StorageIteratorOptions{}
	}
	lower := []byte(prefix)
	return &qrdbsdk.StorageIteratorOptions{
		LowerBound: lower,
		UpperBound: prefixUpperBound(lower),
	}
}

// keysOnlyBounds is prefixBounds with value reads suppressed.
func keysOnlyBounds(prefix string) *qrdbsdk.StorageIteratorOptions {
	opts := prefixBounds(prefix)
	opts.OnlyKeys = true
	return opts
}

// prefixUpperBound returns the smallest key that sorts after every key with
// the given prefix. It increments the last byte that is not 0xFF and truncates
// the remainder. An all-0xFF prefix has no successor, so nil is returned to
// leave the range open at the top.
func prefixUpperBound(prefix []byte) []byte {
	upper := make([]byte, len(prefix))
	copy(upper, prefix)
	for i := len(upper) - 1; i >= 0; i-- {
		if upper[i] != 0xFF {
			upper[i]++
			return upper[:i+1]
		}
	}
	return nil
}

// HasPrefix reports whether key begins with prefix. It is exported for stores
// that post-filter scan results and want to state the intent explicitly.
func HasPrefix(key, prefix string) bool {
	return bytes.HasPrefix([]byte(key), []byte(prefix))
}
