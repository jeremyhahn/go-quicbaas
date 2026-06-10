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

// Package security provides encryption and security features for QuicBaaS.
package security

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/jeremyhahn/go-objstore/pkg/common"
	xkms "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// ObjStoreAdapter adapts go-objstore's common.Storage to go-xkms's storage.Backend.
// This allows QuicBaaS to use any go-objstore backend (S3, GCS, Azure, local, etc.)
// as storage for go-xkms encryption keys.
type ObjStoreAdapter struct {
	objstore common.Storage

	// putIfAbsentMu serializes the Exists+Put performed by PutIfAbsent within
	// this process. go-objstore exposes no conditional-put primitive, so this
	// guarantees uniqueness for single-process access; true cross-node atomicity
	// is not available through the object-store abstraction.
	putIfAbsentMu sync.Mutex
}

// NewObjStoreAdapter creates a new adapter that wraps a go-objstore Storage backend.
func NewObjStoreAdapter(objstore common.Storage) xkms.StorageBackend {
	return &ObjStoreAdapter{objstore: objstore}
}

// Get retrieves the value for the given key.
func (a *ObjStoreAdapter) Get(_ context.Context, key string) ([]byte, error) {
	reader, err := a.objstore.Get(key)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, api.NewOperationFailedError("read data", err)
	}
	return data, nil
}

// Put stores the value for the given key.
func (a *ObjStoreAdapter) Put(_ context.Context, key string, value []byte) error {
	reader := bytes.NewReader(value)
	return a.objstore.Put(key, reader)
}

// Delete removes the key and its value from storage.
func (a *ObjStoreAdapter) Delete(_ context.Context, key string) error {
	return a.objstore.Delete(key)
}

// PutIfAbsent atomically stores value at key only if key does not already
// exist, returning stored=true when written and stored=false when the key was
// already present. Atomicity holds for single-process access via putIfAbsentMu;
// the object-store abstraction offers no cross-node compare-and-swap.
func (a *ObjStoreAdapter) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	a.putIfAbsentMu.Lock()
	defer a.putIfAbsentMu.Unlock()

	exists, err := a.Exists(ctx, key)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	if err := a.Put(ctx, key, value); err != nil {
		return false, err
	}
	return true, nil
}

// List returns all keys with the given prefix.
func (a *ObjStoreAdapter) List(_ context.Context, prefix string) ([]string, error) {
	return a.objstore.List(prefix)
}

// Scan iterates over all key-value pairs matching the given prefix,
// invoking fn for each entry. Iteration stops early if fn returns an error.
func (a *ObjStoreAdapter) Scan(ctx context.Context, prefix string, fn func(string, []byte) error) error {
	keys, err := a.objstore.List(prefix)
	if err != nil {
		return err
	}
	for _, key := range keys {
		data, err := a.Get(ctx, key)
		if err != nil {
			return err
		}
		if err := fn(key, data); err != nil {
			return err
		}
	}
	return nil
}

// Exists checks if a key exists in storage.
func (a *ObjStoreAdapter) Exists(ctx context.Context, key string) (bool, error) {
	return a.objstore.Exists(ctx, key)
}

// Close releases any resources held by the backend.
// Note: go-objstore's common.Storage doesn't have a Close method, so we check
// if the underlying storage implements io.Closer.
func (a *ObjStoreAdapter) Close() error {
	if closer, ok := a.objstore.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}
