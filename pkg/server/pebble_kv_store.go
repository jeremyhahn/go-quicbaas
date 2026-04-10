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
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// PebbleKVStore implements interfaces.KVStore using go-qrdb's PebbleDB
// storage engine. Provides high-performance persistent key-value storage
// suitable for production embedded deployments.
//
// Thread safety: PebbleDB handles its own internal synchronization.
// The closed flag uses atomic.Bool for lock-free state checking.
type PebbleKVStore struct {
	store  *xkmssdk.QRDBPebbleStorage
	closed atomic.Bool
}

// PebbleKVStoreError indicates a PebbleDB KV store operation failure.
type PebbleKVStoreError struct {
	Op  string
	Err error
}

// Error implements the error interface.
func (e *PebbleKVStoreError) Error() string {
	return fmt.Sprintf("pebble kv store: %s: %v", e.Op, e.Err)
}

// Unwrap returns the underlying error.
func (e *PebbleKVStoreError) Unwrap() error {
	return e.Err
}

// NewPebbleKVStore creates a new PebbleDB-backed KV store at the given
// data directory. Uses a lightweight PebbleDB configuration suitable for
// embedded usage. The data directory is created if it does not exist.
func NewPebbleKVStore(dataDir string) (*PebbleKVStore, error) {
	if dataDir == "" {
		return nil, &PebbleKVStoreError{
			Op:  "new",
			Err: ErrDataDirRequired,
		}
	}

	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return nil, &PebbleKVStoreError{
			Op:  "new",
			Err: err,
		}
	}

	cfg := xkmssdk.QRDBTestPebbleConfig(dataDir)
	cfg.SyncWrites = true

	store, err := xkmssdk.QRDBNewPebbleStorage(cfg)
	if err != nil {
		return nil, &PebbleKVStoreError{
			Op:  "new",
			Err: err,
		}
	}

	return &PebbleKVStore{store: store}, nil
}

// Put stores a key-value pair in PebbleDB.
func (p *PebbleKVStore) Put(_ context.Context, key string, value []byte) error {
	if p.closed.Load() {
		return &PebbleKVStoreError{Op: "put", Err: ErrServerNotRunning}
	}
	return p.store.Set([]byte(key), value)
}

// Get retrieves a value by key from PebbleDB.
// Returns DragonError with ErrNotFound when the key does not exist.
func (p *PebbleKVStore) Get(_ context.Context, key string) ([]byte, error) {
	if p.closed.Load() {
		return nil, &PebbleKVStoreError{Op: "get", Err: ErrServerNotRunning}
	}
	data, err := p.store.Get([]byte(key))
	if err != nil {
		return nil, &PebbleKVStoreError{Op: "get", Err: err}
	}
	if data == nil {
		return nil, &dberrors.DragonError{Code: dberrors.ErrNotFound, Op: "kv.get"}
	}
	return data, nil
}

// Delete removes a key from PebbleDB.
func (p *PebbleKVStore) Delete(_ context.Context, key string) error {
	if p.closed.Load() {
		return &PebbleKVStoreError{Op: "delete", Err: ErrServerNotRunning}
	}
	return p.store.Delete([]byte(key))
}

// Scan iterates over key-value pairs matching the given prefix in PebbleDB.
// Uses PebbleDB's native prefix iterator for efficient range scanning.
func (p *PebbleKVStore) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	if p.closed.Load() {
		return &PebbleKVStoreError{Op: "scan", Err: ErrServerNotRunning}
	}

	prefixBytes := []byte(prefix)

	// Use PebbleDB's iterator with prefix bounds for efficient scanning.
	keys, err := p.store.List(prefixBytes)
	if err != nil {
		return &PebbleKVStoreError{Op: "scan", Err: err}
	}

	for _, keyBytes := range keys {
		if !bytes.HasPrefix(keyBytes, prefixBytes) {
			continue
		}
		value, getErr := p.store.Get(keyBytes)
		if getErr != nil {
			return &PebbleKVStoreError{Op: "scan", Err: getErr}
		}
		if value == nil {
			continue
		}
		if err := fn(string(keyBytes), value); err != nil {
			return err
		}
	}
	return nil
}

// List returns all keys matching the given prefix from PebbleDB.
func (p *PebbleKVStore) List(_ context.Context, prefix string) ([]string, error) {
	if p.closed.Load() {
		return nil, &PebbleKVStoreError{Op: "list", Err: ErrServerNotRunning}
	}

	keyBytes, err := p.store.List([]byte(prefix))
	if err != nil {
		return nil, &PebbleKVStoreError{Op: "list", Err: err}
	}

	keys := make([]string, 0, len(keyBytes))
	for _, kb := range keyBytes {
		k := string(kb)
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// Exists checks whether a key exists in PebbleDB.
func (p *PebbleKVStore) Exists(_ context.Context, key string) (bool, error) {
	if p.closed.Load() {
		return false, &PebbleKVStoreError{Op: "exists", Err: ErrServerNotRunning}
	}
	return p.store.Exists([]byte(key))
}

// Close releases all PebbleDB resources. Subsequent operations return errors.
// Calling Close on an already-closed store is a no-op.
func (p *PebbleKVStore) Close() error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}
	return p.store.Close()
}

var _ interfaces.KVStore = (*PebbleKVStore)(nil)
