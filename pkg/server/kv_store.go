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
//
// An in-memory KVStore for tests and embedded use is NOT defined here: qrdb
// owns storage engines and re-exports them through its SDK, so pkg/kvstore
// adapts qrdbsdk.NewMemoryStorage() into the same interfaces.KVStore contract.
// Use kvstore.NewMemory() rather than adding a second implementation.
type sdkKVStore struct {
	client  *qrdbsdk.Client
	shardID uint64
}

func newSDKKVStore(client *qrdbsdk.Client, shardID uint64) interfaces.KVStore {
	base := sdkKVStore{
		client:  client,
		shardID: shardID,
	}
	if g, ok := client.KV().(transport.GuardedKVClient); ok {
		return &guardedSDKKVStore{sdkKVStore: base, guarded: g}
	}
	return &base
}

// Put stores a key-value pair through the SDK client.
func (s *sdkKVStore) Put(ctx context.Context, key string, value []byte) error {
	return s.client.KV().Put(ctx, key, value)
}

// guardedSDKKVStore carries the guarded verbs as EXPLICIT FORWARDS
// (go-quicbaas#120): qrdb evaluates their condition inside the storage
// engine — across every process sharing the store — which is exactly the
// arbitration capability consumers (the secrets backend among them)
// discover by type assertion. The split at construction is deliberate: a
// wrapper must never PROMOTE a capability its transport does not truly
// carry, and must never emulate one over the base verbs — so the methods
// exist only on the type returned when the transport really has them.
//
// The capability is qrdb's own transport.GuardedKVClient, which carries all
// three verbs. A narrower local interface once matched only the create and
// delete guards, so CompareAndSwap was never forwarded and every guarded edit
// against this store had nothing to call (go-quicbaas#837).
type guardedSDKKVStore struct {
	sdkKVStore
	guarded transport.GuardedKVClient
}

// The guarded store is what KMS key claims and every other guarded-write
// consumer assert for (round 34 Integration on go-quicbaas#1993).
var _ interfaces.GuardedKVStore = (*guardedSDKKVStore)(nil)

// CompareAndSwap replaces key's value with next only while it still holds
// expected, arbitrated in the store. An absent key is left absent.
func (s *guardedSDKKVStore) CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error) {
	return s.guarded.CompareAndSwap(ctx, key, expected, next)
}

// PutIfAbsent stores value only when key is absent, arbitrated in the store.
func (s *guardedSDKKVStore) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return s.guarded.PutIfAbsent(ctx, key, value)
}

// CompareAndDelete removes key only while it still holds expected,
// arbitrated in the store.
func (s *guardedSDKKVStore) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	return s.guarded.CompareAndDelete(ctx, key, expected)
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
