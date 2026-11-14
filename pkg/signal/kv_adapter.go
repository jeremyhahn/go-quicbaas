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
	"errors"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	signalstore "github.com/jeremyhahn/go-signal/pkg/store"
)

// ErrNoTransactions is returned by DeleteIfPresent when the adapter was built
// without a go-qrdb TransactionClient. go-signal needs the check-and-delete
// to be atomic (one-time pre-keys are single-use), so the adapter refuses
// rather than fall back to a racy Exists-then-Delete.
var ErrNoTransactions = errors.New("signal: kv adapter has no transaction client; DeleteIfPresent needs an atomic compare-and-delete")

// kvAdapter wraps a go-qrdb KVClient to satisfy go-signal's store.KVClient
// interface. The go-qrdb KVClient uses variadic option parameters on each
// method, while go-signal's interface uses plain parameters. This adapter
// bridges the two by dropping the variadic options and converting Scan
// from a map return to a callback-based iteration.
//
// DeleteIfPresent runs as a go-qrdb transaction (compare version > 0, then
// delete). Transactions address raw keys, so txnPrefix is the key prefix
// the inner client applies (a TenantKVClient's "{tenantID}/").
type kvAdapter struct {
	inner     qrdbtransport.KVClient
	txn       qrdbtransport.TransactionClient
	txnPrefix string
}

// NewKVAdapter creates a new adapter that wraps a go-qrdb KVClient to satisfy
// go-signal's store.KVClient interface. It has no transaction client, so its
// DeleteIfPresent returns ErrNoTransactions; use NewTenantKVAdapter for a
// store that consumes one-time pre-keys.
func NewKVAdapter(inner qrdbtransport.KVClient) signalstore.KVClient {
	return &kvAdapter{inner: inner}
}

// NewTenantKVAdapter creates an adapter over base scoped to tenantID: plain
// operations go through a go-qrdb TenantKVClient and DeleteIfPresent through
// txn, with the same "{tenantID}/" key prefix.
func NewTenantKVAdapter(base qrdbtransport.KVClient, txn qrdbtransport.TransactionClient, tenantID string) (signalstore.KVClient, error) {
	tenantKV, err := qrdbtransport.WithTenant(base, tenantID)
	if err != nil {
		return nil, err
	}
	return &kvAdapter{inner: tenantKV, txn: txn, txnPrefix: tenantID + "/"}, nil
}

// DeleteIfPresent atomically deletes key and reports whether this call
// removed it: a go-qrdb transaction whose only compare is "version > 0" (the
// key exists) and whose success op deletes it. Among racing callers exactly
// one transaction sees the key, so exactly one returns true.
func (a *kvAdapter) DeleteIfPresent(ctx context.Context, key string) (bool, error) {
	if a.txn == nil {
		return false, ErrNoTransactions
	}
	raw := []byte(a.txnPrefix + key)
	resp, err := a.txn.ExecuteTransaction(ctx, &qrdbtransport.TransactionRequest{
		Compares: []qrdbtransport.Compare{{
			Key:     raw,
			Target:  qrdbtransport.CompareVersion,
			Result:  qrdbtransport.CompareGreater,
			Version: 0,
		}},
		SuccessOps: []qrdbtransport.Op{{Type: qrdbtransport.OpDelete, Key: raw}},
	})
	if err != nil {
		var failed *qrdbtransport.TransactionFailedError
		if errors.As(err, &failed) {
			return false, nil
		}
		return false, err
	}
	return resp.Succeeded, nil
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
