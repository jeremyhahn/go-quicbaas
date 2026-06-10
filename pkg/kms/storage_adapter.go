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

// Package kms provides key management services for QuicBaaS.
package kms

import (
	"context"
	"strings"
	"sync"
	"time"

	xkms "github.com/jeremyhahn/go-xkms/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// kvStoreTimeout is the default timeout for KVStore operations.
// QuicRaft's SyncRead requires a context with a deadline.
const kvStoreTimeout = 30 * time.Second

// KeyPrefix is the prefix used for storing KMS keys in the underlying storage.
const KeyPrefix = "kms/keys/"

// TenantKeyPrefix is the prefix template for tenant-scoped KMS keys.
// Format: kms/tenant/{tenantID}/keys/.
const TenantKeyPrefix = "kms/tenant/"

// KVStoreStorageAdapter adapts QuicBaaS's unified KVStore interface to go-xkms's storage.Backend.
// This is the preferred adapter for production use as it works with:
//   - Cluster mode (Raft-replicated storage)
//   - Local mode (direct PebbleDB)
//   - Memory mode (testing/bootstrap)
//
// When tenantID is set, keys are stored under a tenant-scoped prefix for isolation.
// When tenantID is empty, the adapter uses the flat "kms/keys/" prefix for backward
// compatibility with single-tenant deployments.
type KVStoreStorageAdapter struct {
	kvstore  interfaces.KVStore
	tenantID string // empty = single-tenant (backward compatible)

	// putIfAbsentMu serializes the Exists+Put performed by PutIfAbsent within
	// this process. The underlying interfaces.KVStore exposes no compare-and-swap
	// primitive, so this guarantees uniqueness for single-process access; true
	// cross-node atomicity would require a CAS method on interfaces.KVStore.
	putIfAbsentMu sync.Mutex
}

// NewKVStoreStorageAdapter creates a new adapter wrapping QuicBaaS's KVStore interface.
// This constructor creates a single-tenant adapter (backward compatible).
func NewKVStoreStorageAdapter(kvstore interfaces.KVStore) (xkms.StorageBackend, error) {
	if kvstore == nil {
		return nil, ErrNilStorage
	}
	return &KVStoreStorageAdapter{kvstore: kvstore}, nil
}

// NewKVStoreStorageAdapterWithTenant creates a new adapter with tenant-scoped storage.
// When tenantID is empty, the adapter operates in single-tenant mode (same as NewKVStoreStorageAdapter).
// When tenantID is set, keys are stored under "kms/tenant/{tenantID}/keys/" for isolation.
func NewKVStoreStorageAdapterWithTenant(kvstore interfaces.KVStore, tenantID string) (xkms.StorageBackend, error) {
	if kvstore == nil {
		return nil, ErrNilStorage
	}
	return &KVStoreStorageAdapter{kvstore: kvstore, tenantID: tenantID}, nil
}

// buildKey constructs the full storage key with the appropriate prefix.
// Single-tenant mode: "kms/keys/{key}".
// Multi-tenant mode:  "kms/tenant/{tenantID}/keys/{key}".
func (k *KVStoreStorageAdapter) buildKey(key string) string {
	if k.tenantID == "" {
		return KeyPrefix + key
	}
	return TenantKeyPrefix + k.tenantID + "/keys/" + key
}

// buildListPrefix constructs the prefix for list operations.
// Single-tenant mode: "kms/keys/{prefix}".
// Multi-tenant mode:  "kms/tenant/{tenantID}/keys/{prefix}".
func (k *KVStoreStorageAdapter) buildListPrefix(prefix string) string {
	if k.tenantID == "" {
		return KeyPrefix + prefix
	}
	return TenantKeyPrefix + k.tenantID + "/keys/" + prefix
}

// storagePrefix returns the base storage prefix for this adapter.
// Used internally for stripping prefixes from listed keys.
func (k *KVStoreStorageAdapter) storagePrefix() string {
	if k.tenantID == "" {
		return KeyPrefix
	}
	return TenantKeyPrefix + k.tenantID + "/keys/"
}

// Get retrieves the value for the given key from KVStore.
func (k *KVStoreStorageAdapter) Get(ctx context.Context, key string) ([]byte, error) {
	kvCtx, cancel := context.WithTimeout(ctx, kvStoreTimeout)
	defer cancel()
	data, err := k.kvstore.Get(kvCtx, k.buildKey(key))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, xkms.ErrStorageNotFound
		}
		return nil, err
	}
	return data, nil
}

// Put stores the value for the given key in KVStore.
func (k *KVStoreStorageAdapter) Put(ctx context.Context, key string, value []byte) error {
	kvCtx, cancel := context.WithTimeout(ctx, kvStoreTimeout)
	defer cancel()
	return k.kvstore.Put(kvCtx, k.buildKey(key), value)
}

// PutIfAbsent atomically stores value at key only if key does not already
// exist, returning stored=true when written and stored=false when the key was
// already present. Atomicity holds for single-process access via putIfAbsentMu;
// the underlying KVStore offers no cross-node compare-and-swap.
func (k *KVStoreStorageAdapter) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	k.putIfAbsentMu.Lock()
	defer k.putIfAbsentMu.Unlock()

	exists, err := k.Exists(ctx, key)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	if err := k.Put(ctx, key, value); err != nil {
		return false, err
	}
	return true, nil
}

// Delete removes the key and its value from KVStore.
func (k *KVStoreStorageAdapter) Delete(ctx context.Context, key string) error {
	kvCtx, cancel := context.WithTimeout(ctx, kvStoreTimeout)
	defer cancel()
	return k.kvstore.Delete(kvCtx, k.buildKey(key))
}

// List returns all keys with the given prefix from KVStore.
func (k *KVStoreStorageAdapter) List(ctx context.Context, prefix string) ([]string, error) {
	kvCtx, cancel := context.WithTimeout(ctx, kvStoreTimeout)
	defer cancel()
	fullPrefix := k.buildListPrefix(prefix)
	basePrefix := k.storagePrefix()
	var keys []string

	err := k.kvstore.Scan(kvCtx, fullPrefix, func(key string, _ []byte) error {
		if strings.HasPrefix(key, fullPrefix) {
			keys = append(keys, strings.TrimPrefix(key, basePrefix))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return keys, nil
}

// Scan iterates over all key-value pairs matching the given prefix from KVStore,
// invoking fn for each entry. Iteration stops early if fn returns an error.
func (k *KVStoreStorageAdapter) Scan(ctx context.Context, prefix string, fn func(string, []byte) error) error {
	kvCtx, cancel := context.WithTimeout(ctx, kvStoreTimeout)
	defer cancel()
	fullPrefix := k.buildListPrefix(prefix)
	basePrefix := k.storagePrefix()

	return k.kvstore.Scan(kvCtx, fullPrefix, func(key string, value []byte) error {
		if strings.HasPrefix(key, fullPrefix) {
			trimmedKey := strings.TrimPrefix(key, basePrefix)
			if err := fn(trimmedKey, value); err != nil {
				return err
			}
		}
		return nil
	})
}

// Exists checks if a key exists in KVStore. It delegates to the KVStore's
// own Exists so transient errors (timeouts, leadership changes) propagate
// instead of being conflated with "key absent" — PutIfAbsent depends on
// that distinction to avoid overwriting existing keys.
func (k *KVStoreStorageAdapter) Exists(ctx context.Context, key string) (bool, error) {
	kvCtx, cancel := context.WithTimeout(ctx, kvStoreTimeout)
	defer cancel()
	return k.kvstore.Exists(kvCtx, k.buildKey(key))
}

// Close is a no-op since KVStore lifecycle is managed by QuicBaaS.
func (k *KVStoreStorageAdapter) Close() error {
	return nil
}

var _ xkms.StorageBackend = (*KVStoreStorageAdapter)(nil)
