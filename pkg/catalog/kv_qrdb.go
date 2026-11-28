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
	"sort"
	"strings"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// DefaultQRDBNamespace is the key namespace the system catalog lives under in
// go-qrdb, alongside the other internal records kept under "__sys:".
const DefaultQRDBNamespace = "__sys:catalog/"

// qrdbKV is a KV over a go-qrdb client. Every catalog key is stored under
// namespace.
//
// Catalog keys are text: key.go builds every entry key and index key from
// strings, and checksums are hex encoded, so converting a key to a string
// for the SDK and back loses nothing.
type qrdbKV struct {
	kv        transport.KVClient
	txn       transport.TransactionClient
	namespace string
}

// NewQRDBKV returns a KV over go-qrdb's KV and transaction clients, storing
// every key under namespace, which must be non-empty and end in "/". The
// caller owns the clients: Close does not close them.
func NewQRDBKV(kv transport.KVClient, txn transport.TransactionClient, namespace string) (KV, error) {
	if kv == nil {
		return nil, NewInvalidEntryError("kv", "go-qrdb KV client is required")
	}
	if txn == nil {
		return nil, NewInvalidEntryError("txn", "go-qrdb transaction client is required")
	}
	if namespace == "" || !strings.HasSuffix(namespace, "/") {
		return nil, NewInvalidEntryError("namespace", `namespace must be non-empty and end in "/"`)
	}
	return &qrdbKV{kv: kv, txn: txn, namespace: namespace}, nil
}

func (q *qrdbKV) Get(ctx context.Context, key []byte) ([]byte, error) {
	data, err := q.kv.Get(ctx, q.namespace+string(key))
	if err != nil {
		var notFound *transport.KeyNotFoundError
		if errors.As(err, &notFound) {
			return nil, &KVKeyNotFoundError{Key: string(key)}
		}
		return nil, NewStorageError("get", err)
	}
	return data, nil
}

// Scan reads the whole prefix and sorts it, since the SDK returns the pairs
// unordered.
func (q *qrdbKV) Scan(ctx context.Context, prefix []byte, fn func(key, value []byte) bool) error {
	pairs, err := q.kv.Scan(ctx, q.namespace+string(prefix))
	if err != nil {
		return NewStorageError("scan", err)
	}
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		if strings.HasPrefix(k, q.namespace) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !fn([]byte(strings.TrimPrefix(k, q.namespace)), pairs[k]) {
			break
		}
	}
	return nil
}

// Apply sends the whole batch as one go-qrdb transaction, so an entry and
// its index keys commit together. A batch is never split.
func (q *qrdbKV) Apply(ctx context.Context, batch *KVBatch) error {
	if batch == nil || batch.Len() == 0 {
		return nil
	}
	ops := make([]transport.Op, 0, batch.Len())
	for _, op := range batch.Ops() {
		key := []byte(q.namespace + string(op.Key))
		if op.Delete {
			ops = append(ops, transport.Op{Type: transport.OpDelete, Key: key})
			continue
		}
		ops = append(ops, transport.Op{Type: transport.OpPut, Key: key, Value: op.Value})
	}
	resp, err := q.txn.ExecuteTransaction(ctx, &transport.TransactionRequest{SuccessOps: ops})
	if err != nil {
		return NewStorageError("apply", err)
	}
	if resp == nil || !resp.Succeeded {
		return NewStorageError("apply", errors.New("go-qrdb transaction did not succeed"))
	}
	return nil
}

// Close does nothing: the server owns the go-qrdb client.
func (q *qrdbKV) Close() error { return nil }
