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
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// GetKVStore returns a KVStore for the given shard backed by the qrdb SDK.
// The SDK is the sole storage path for all server modes. All operations
// are routed through the ShardedClient which handles embedded or network
// transport selection internally.
//
// Panics if the qrdb SDK client has not been initialized. This is a
// programming error: the SDK must be set up during server startup before
// any component calls GetKVStore.
func (s *Server) GetKVStore(shardID uint64) interfaces.KVStore {
	if s.qrdbClient == nil {
		panic(&QRDBClientNotInitializedError{})
	}
	client, err := s.qrdbClient.ForShard(shardID)
	if err != nil {
		panic(&ShardClientError{ShardID: shardID, Cause: err})
	}
	return newSDKKVStore(client, shardID)
}

// sdkKVStore implements interfaces.KVStore by delegating through the qrdb SDK.
// It provides a clean abstraction layer between quicbaas internal components
// and the underlying storage, routing operations through the SDK client
// rather than making direct storage calls.
type sdkKVStore struct {
	client  *qrdbsdk.Client
	shardID uint64
}

func newSDKKVStore(client *qrdbsdk.Client, shardID uint64) interfaces.KVStore {
	return &sdkKVStore{
		client:  client,
		shardID: shardID,
	}
}

// Put stores a key-value pair through the SDK client.
func (s *sdkKVStore) Put(ctx context.Context, key string, value []byte) error {
	return s.client.KV().Put(ctx, key, value)
}

// Get retrieves a value by key through the SDK client. The SDK returns
// KeyNotFoundError when the key does not exist, which is translated to
// the quicbaas QBError with ErrNotFound code.
func (s *sdkKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := s.client.KV().Get(ctx, key)
	if err != nil {
		var knf *transport.KeyNotFoundError
		if errors.As(err, &knf) {
			return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "kv.get"}
		}
		return nil, err
	}
	return data, nil
}

// Delete removes a key through the SDK client.
func (s *sdkKVStore) Delete(ctx context.Context, key string) error {
	return s.client.KV().Delete(ctx, key)
}

// Scan iterates over key-value pairs matching the given prefix through the
// SDK client. The SDK returns pairs as a map, which is sorted by key for
// deterministic iteration order before invoking the callback.
func (s *sdkKVStore) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	pairs, err := s.client.KV().Scan(ctx, prefix)
	if err != nil {
		return err
	}
	if len(pairs) == 0 {
		return nil
	}

	// Sort keys for deterministic iteration order.
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if err := fn(k, pairs[k]); err != nil {
			return err
		}
	}
	return nil
}

// List returns all keys matching the given prefix through the SDK client.
func (s *sdkKVStore) List(ctx context.Context, prefix string) ([]string, error) {
	return s.client.KV().List(ctx, prefix)
}

// Exists checks whether a key exists through the SDK client.
func (s *sdkKVStore) Exists(ctx context.Context, key string) (bool, error) {
	return s.client.KV().Exists(ctx, key)
}

var _ interfaces.KVStore = (*sdkKVStore)(nil)

// MemoryKVStore is an in-memory implementation of interfaces.KVStore.
// Used by qrdb's memory state machine for in-process storage.
// Thread-safe using sync.Map for concurrent access.
//
// NOTE: MemoryKVStore is NOT returned by GetKVStore. GetKVStore always
// returns an sdkKVStore backed by the qrdb SDK client.
type MemoryKVStore struct {
	data sync.Map
}

// NewMemoryKVStore creates a new in-memory KV store.
func NewMemoryKVStore() *MemoryKVStore {
	return &MemoryKVStore{}
}

// Put stores a key-value pair in memory.
func (m *MemoryKVStore) Put(_ context.Context, key string, value []byte) error {
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	m.data.Store(key, valueCopy)
	return nil
}

// Get retrieves a value by key from memory.
func (m *MemoryKVStore) Get(_ context.Context, key string) ([]byte, error) {
	value, ok := m.data.Load(key)
	if !ok {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "kv.get"}
	}
	storedValue := value.([]byte) //nolint:errcheck // type assertion guaranteed by map contract
	valueCopy := make([]byte, len(storedValue))
	copy(valueCopy, storedValue)
	return valueCopy, nil
}

// Delete removes a key from memory.
func (m *MemoryKVStore) Delete(_ context.Context, key string) error {
	m.data.Delete(key)
	return nil
}

// Scan iterates over key-value pairs matching the given prefix in memory.
func (m *MemoryKVStore) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	var scanErr error
	m.data.Range(func(k, v any) bool {
		key := k.(string) //nolint:errcheck // type assertion guaranteed by map contract
		if strings.HasPrefix(key, prefix) {
			value := v.([]byte) //nolint:errcheck // type assertion guaranteed by map contract
			if err := fn(key, value); err != nil {
				scanErr = err
				return false
			}
		}
		return true
	})
	return scanErr
}

// List returns all keys matching the given prefix from memory.
func (m *MemoryKVStore) List(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	m.data.Range(func(k, v any) bool {
		key := k.(string) //nolint:errcheck // type assertion guaranteed by map contract
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
		return true
	})
	return keys, nil
}

// Exists checks whether a key exists in memory.
func (m *MemoryKVStore) Exists(_ context.Context, key string) (bool, error) {
	_, ok := m.data.Load(key)
	return ok, nil
}

var _ interfaces.KVStore = (*MemoryKVStore)(nil)
