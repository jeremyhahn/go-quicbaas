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
	"testing"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// qrdbDouble stands in for go-qrdb's KV and transaction clients. It
// implements only what the adapter calls; any other call fails the test.
type qrdbDouble struct {
	t *testing.T

	values  map[string][]byte
	scan    map[string][]byte
	getKeys []string
	scanned []string

	txns      []*transport.TransactionRequest
	succeeded bool
}

func newQRDBDouble(t *testing.T) *qrdbDouble {
	return &qrdbDouble{t: t, values: map[string][]byte{}, succeeded: true}
}

func (d *qrdbDouble) Get(_ context.Context, key string, _ ...transport.ReadOption) ([]byte, error) {
	d.getKeys = append(d.getKeys, key)
	v, ok := d.values[key]
	if !ok {
		return nil, &transport.KeyNotFoundError{Key: key}
	}
	return v, nil
}

func (d *qrdbDouble) Scan(_ context.Context, prefix string, _ ...transport.ReadOption) (map[string][]byte, error) {
	d.scanned = append(d.scanned, prefix)
	return d.scan, nil
}

func (d *qrdbDouble) Put(context.Context, string, []byte, ...transport.WriteOption) error {
	d.t.Fatal("adapter called KVClient.Put; every write must go through one transaction")
	return nil
}

func (d *qrdbDouble) Delete(context.Context, string, ...transport.WriteOption) error {
	d.t.Fatal("adapter called KVClient.Delete; every write must go through one transaction")
	return nil
}

func (d *qrdbDouble) List(context.Context, string, ...transport.ReadOption) ([]string, error) {
	d.t.Fatal("adapter called KVClient.List")
	return nil, nil
}

func (d *qrdbDouble) Exists(context.Context, string, ...transport.ReadOption) (bool, error) {
	d.t.Fatal("adapter called KVClient.Exists")
	return false, nil
}

func (d *qrdbDouble) ExecuteTransaction(_ context.Context, req *transport.TransactionRequest, _ ...transport.WriteOption) (*transport.TransactionResponse, error) {
	d.txns = append(d.txns, req)
	return &transport.TransactionResponse{Succeeded: d.succeeded}, nil
}

func newTestQRDBKV(t *testing.T, d *qrdbDouble) KV {
	t.Helper()
	kv, err := NewQRDBKV(d, d, "__sys:catalog-unit/")
	if err != nil {
		t.Fatalf("NewQRDBKV() error = %v", err)
	}
	return kv
}

func TestQRDBKV_ApplySendsOneOrderedTransaction(t *testing.T) {
	d := newQRDBDouble(t)
	kv := newTestQRDBKV(t, d)

	batch := &KVBatch{}
	batch.Set([]byte("cat:ns:a"), []byte("entry"))
	batch.Set([]byte("idx:store:s3:ns:a"), nil)
	batch.Delete([]byte("idx:store:old:ns:a"))
	if err := kv.Apply(context.Background(), batch); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if len(d.txns) != 1 {
		t.Fatalf("Apply() sent %d transactions, want 1", len(d.txns))
	}
	req := d.txns[0]
	if len(req.Compares) != 0 {
		t.Fatalf("Apply() sent %d compares, want none", len(req.Compares))
	}
	if len(req.FailureOps) != 0 {
		t.Fatalf("Apply() sent %d failure ops, want none", len(req.FailureOps))
	}
	want := []transport.Op{
		{Type: transport.OpPut, Key: []byte("__sys:catalog-unit/cat:ns:a"), Value: []byte("entry")},
		{Type: transport.OpPut, Key: []byte("__sys:catalog-unit/idx:store:s3:ns:a")},
		{Type: transport.OpDelete, Key: []byte("__sys:catalog-unit/idx:store:old:ns:a")},
	}
	if len(req.SuccessOps) != len(want) {
		t.Fatalf("Apply() sent %d ops, want %d", len(req.SuccessOps), len(want))
	}
	for i, op := range req.SuccessOps {
		if op.Type != want[i].Type || string(op.Key) != string(want[i].Key) || string(op.Value) != string(want[i].Value) {
			t.Fatalf("op %d = {%v %q %q}, want {%v %q %q}", i, op.Type, op.Key, op.Value, want[i].Type, want[i].Key, want[i].Value)
		}
	}
}

func TestQRDBKV_ApplyFailedTransactionIsError(t *testing.T) {
	d := newQRDBDouble(t)
	d.succeeded = false
	kv := newTestQRDBKV(t, d)

	batch := &KVBatch{}
	batch.Set([]byte("cat:ns:a"), []byte("entry"))
	err := kv.Apply(context.Background(), batch)
	if err == nil {
		t.Fatal("Apply() of a transaction that did not succeed returned nil")
	}
	if !IsStorageError(err) {
		t.Fatalf("Apply() error = %v, want a storage error", err)
	}
}

func TestQRDBKV_ApplyEmptyBatchSendsNothing(t *testing.T) {
	d := newQRDBDouble(t)
	kv := newTestQRDBKV(t, d)

	if err := kv.Apply(context.Background(), &KVBatch{}); err != nil {
		t.Fatalf("Apply(empty) error = %v", err)
	}
	if err := kv.Apply(context.Background(), nil); err != nil {
		t.Fatalf("Apply(nil) error = %v", err)
	}
	if len(d.txns) != 0 {
		t.Fatalf("empty batches sent %d transactions, want 0", len(d.txns))
	}
}

func TestQRDBKV_GetMapsKeyNotFound(t *testing.T) {
	d := newQRDBDouble(t)
	d.values["__sys:catalog-unit/cat:ns:a"] = []byte("entry")
	kv := newTestQRDBKV(t, d)

	got, err := kv.Get(context.Background(), []byte("cat:ns:a"))
	if err != nil || string(got) != "entry" {
		t.Fatalf("Get() = %q, %v, want %q", got, err, "entry")
	}
	_, err = kv.Get(context.Background(), []byte("cat:ns:missing"))
	var notFound *KVKeyNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("Get(missing) error = %v, want *KVKeyNotFoundError", err)
	}
	if notFound.Key != "cat:ns:missing" {
		t.Fatalf("KVKeyNotFoundError.Key = %q, want the key without namespace", notFound.Key)
	}
	if d.getKeys[0] != "__sys:catalog-unit/cat:ns:a" {
		t.Fatalf("Get() asked for %q, want the namespaced key", d.getKeys[0])
	}
}

func TestQRDBKV_ScanStripsNamespaceSortsAndStops(t *testing.T) {
	d := newQRDBDouble(t)
	d.scan = map[string][]byte{
		"__sys:catalog-unit/idx:c": []byte("3"),
		"__sys:catalog-unit/idx:a": []byte("1"),
		"__sys:catalog-unit/idx:b": []byte("2"),
	}
	kv := newTestQRDBKV(t, d)

	var keys, values []string
	if err := kv.Scan(context.Background(), []byte("idx:"), func(k, v []byte) bool {
		keys = append(keys, string(k))
		values = append(values, string(v))
		return true
	}); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	assertKeys(t, "Scan keys", keys, []string{"idx:a", "idx:b", "idx:c"})
	assertKeys(t, "Scan values", values, []string{"1", "2", "3"})
	if d.scanned[0] != "__sys:catalog-unit/idx:" {
		t.Fatalf("Scan() asked for prefix %q, want the namespaced prefix", d.scanned[0])
	}

	var first []string
	if err := kv.Scan(context.Background(), []byte("idx:"), func(k, _ []byte) bool {
		first = append(first, string(k))
		return len(first) < 2
	}); err != nil {
		t.Fatalf("Scan() stop error = %v", err)
	}
	assertKeys(t, "Scan stop", first, []string{"idx:a", "idx:b"})
}

func TestQRDBKV_ConstructorValidates(t *testing.T) {
	d := newQRDBDouble(t)
	cases := []struct {
		name      string
		kv        transport.KVClient
		txn       transport.TransactionClient
		namespace string
	}{
		{"nil kv client", nil, d, DefaultQRDBNamespace},
		{"nil transaction client", d, nil, DefaultQRDBNamespace},
		{"empty namespace", d, d, ""},
		{"namespace without trailing slash", d, d, "__sys:catalog"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kv, err := NewQRDBKV(tc.kv, tc.txn, tc.namespace)
			if err == nil || kv != nil {
				t.Fatalf("NewQRDBKV() = %v, %v, want an error", kv, err)
			}
		})
	}
	if _, err := NewQRDBKV(d, d, DefaultQRDBNamespace); err != nil {
		t.Fatalf("NewQRDBKV(default namespace) error = %v", err)
	}
}
