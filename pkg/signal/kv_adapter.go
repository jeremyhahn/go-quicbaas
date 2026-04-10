//go:build quicbaas_signal

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

package signal

import (
	"context"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	signalstore "github.com/jeremyhahn/go-signal/pkg/store"
)

// kvAdapter wraps a go-qrdb KVClient to satisfy go-signal's store.KVClient
// interface. The go-qrdb KVClient uses variadic option parameters on each
// method, while go-signal's interface uses plain parameters. This adapter
// bridges the two by dropping the variadic options and converting Scan
// from a map return to a callback-based iteration.
type kvAdapter struct {
	inner qrdbtransport.KVClient
}

// NewKVAdapter creates a new adapter that wraps a go-qrdb KVClient to satisfy
// go-signal's store.KVClient interface.
func NewKVAdapter(inner qrdbtransport.KVClient) signalstore.KVClient {
	return &kvAdapter{inner: inner}
}

// Put stores a value at the given key, delegating to the inner go-qrdb client
// without any write options.
func (a *kvAdapter) Put(ctx context.Context, key string, value []byte) error {
	return a.inner.Put(ctx, key, value)
}

// Get retrieves the value stored at the given key, delegating to the inner
// go-qrdb client without any read options.
func (a *kvAdapter) Get(ctx context.Context, key string) ([]byte, error) {
	return a.inner.Get(ctx, key)
}

// Delete removes the value stored at the given key, delegating to the inner
// go-qrdb client without any write options.
func (a *kvAdapter) Delete(ctx context.Context, key string) error {
	return a.inner.Delete(ctx, key)
}

// List returns all keys matching the given prefix, delegating to the inner
// go-qrdb client without any read options.
func (a *kvAdapter) List(ctx context.Context, prefix string) ([]string, error) {
	return a.inner.List(ctx, prefix)
}

// Scan iterates over all key-value pairs whose keys share the given prefix.
// The inner go-qrdb client returns a map, which is iterated to invoke the
// callback function for each pair. Iteration stops early if fn returns an error.
func (a *kvAdapter) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	pairs, err := a.inner.Scan(ctx, prefix)
	if err != nil {
		return err
	}
	for k, v := range pairs {
		if err := fn(k, v); err != nil {
			return err
		}
	}
	return nil
}

// Exists reports whether a value is stored at the given key, delegating to
// the inner go-qrdb client without any read options.
func (a *kvAdapter) Exists(ctx context.Context, key string) (bool, error) {
	return a.inner.Exists(ctx, key)
}

// Compile-time check: kvAdapter implements signalstore.KVClient.
var _ signalstore.KVClient = (*kvAdapter)(nil)
