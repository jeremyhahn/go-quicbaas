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
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// mapTxn is a go-qrdb TransactionClient over a test KV client's map. It
// evaluates the compares the adapter sends (version > 0, version == 0) and
// applies OpDelete / OpPut, all under one mutex, so it is atomic like the
// real Raft-applied transaction.
type mapTxn struct {
	mu    sync.Mutex
	data  map[string][]byte
	calls []*qrdbtransport.TransactionRequest
	err   error
}

func newMapTxn(data map[string][]byte) *mapTxn { return &mapTxn{data: data} }

func (m *mapTxn) ExecuteTransaction(_ context.Context, req *qrdbtransport.TransactionRequest, _ ...qrdbtransport.WriteOption) (*qrdbtransport.TransactionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	if m.err != nil {
		return nil, m.err
	}
	ok := true
	for _, c := range req.Compares {
		if c.Target != qrdbtransport.CompareVersion {
			return nil, fmt.Errorf("mapTxn: unsupported compare target %d", c.Target)
		}
		var version int64
		if _, present := m.data[string(c.Key)]; present {
			version = 1
		}
		switch c.Result {
		case qrdbtransport.CompareGreater:
			ok = ok && version > c.Version
		case qrdbtransport.CompareEqual:
			ok = ok && version == c.Version
		default:
			return nil, fmt.Errorf("mapTxn: unsupported compare result %d", c.Result)
		}
	}
	ops := req.FailureOps
	if ok {
		ops = req.SuccessOps
	}
	for _, op := range ops {
		switch op.Type {
		case qrdbtransport.OpDelete:
			delete(m.data, string(op.Key))
		case qrdbtransport.OpPut:
			m.data[string(op.Key)] = op.Value
		default:
			return nil, fmt.Errorf("mapTxn: unsupported op %d", op.Type)
		}
	}
	return &qrdbtransport.TransactionResponse{Succeeded: ok}, nil
}

func TestKVAdapter_DeleteIfPresent_NoTransactions(t *testing.T) {
	adapter := NewKVAdapter(newTestQRDBKVClient())
	_, err := adapter.DeleteIfPresent(context.Background(), "k")
	if !errors.Is(err, ErrNoTransactions) {
		t.Fatalf("want ErrNoTransactions, got %v", err)
	}
}

func TestTenantKVAdapter_DeleteIfPresent(t *testing.T) {
	ctx := context.Background()
	base := newTestQRDBKVClient()
	txn := newMapTxn(base.data)
	adapter, err := NewTenantKVAdapter(base, txn, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Put(ctx, "signal/keys/a", []byte("v")); err != nil {
		t.Fatal(err)
	}
	if _, ok := base.data["t1/signal/keys/a"]; !ok {
		t.Fatalf("put did not land under the tenant prefix: %v", base.data)
	}

	removed, err := adapter.DeleteIfPresent(ctx, "signal/keys/a")
	if err != nil || !removed {
		t.Fatalf("first delete: removed=%v err=%v", removed, err)
	}
	if _, ok := base.data["t1/signal/keys/a"]; ok {
		t.Fatal("key still present after DeleteIfPresent")
	}
	removed, err = adapter.DeleteIfPresent(ctx, "signal/keys/a")
	if err != nil || removed {
		t.Fatalf("second delete: removed=%v err=%v", removed, err)
	}

	req := txn.calls[0]
	if len(req.Compares) != 1 || string(req.Compares[0].Key) != "t1/signal/keys/a" ||
		req.Compares[0].Target != qrdbtransport.CompareVersion ||
		req.Compares[0].Result != qrdbtransport.CompareGreater || req.Compares[0].Version != 0 {
		t.Fatalf("unexpected compares: %+v", req.Compares)
	}
	if len(req.SuccessOps) != 1 || req.SuccessOps[0].Type != qrdbtransport.OpDelete ||
		string(req.SuccessOps[0].Key) != "t1/signal/keys/a" || len(req.FailureOps) != 0 {
		t.Fatalf("unexpected ops: success=%+v failure=%+v", req.SuccessOps, req.FailureOps)
	}
}

func TestTenantKVAdapter_DeleteIfPresent_ExactlyOneWinner(t *testing.T) {
	base := newTestQRDBKVClient()
	base.data["t1/k"] = []byte("v")
	adapter, err := NewTenantKVAdapter(base, newMapTxn(base.data), "t1")
	if err != nil {
		t.Fatal(err)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			removed, err := adapter.DeleteIfPresent(context.Background(), "k")
			if err != nil {
				t.Error(err)
			}
			if removed {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := wins.Load(); got != 1 {
		t.Fatalf("want exactly one winner, got %d", got)
	}
}

func TestTenantKVAdapter_DeleteIfPresent_Errors(t *testing.T) {
	base := newTestQRDBKVClient()
	txn := newMapTxn(base.data)
	adapter, err := NewTenantKVAdapter(base, txn, "t1")
	if err != nil {
		t.Fatal(err)
	}

	txn.err = &qrdbtransport.TransactionFailedError{Revision: 7}
	removed, err := adapter.DeleteIfPresent(context.Background(), "k")
	if err != nil || removed {
		t.Fatalf("failed precondition: removed=%v err=%v", removed, err)
	}

	boom := errors.New("raft unavailable")
	txn.err = boom
	if _, err := adapter.DeleteIfPresent(context.Background(), "k"); !errors.Is(err, boom) {
		t.Fatalf("want backend error, got %v", err)
	}

	if _, err := NewTenantKVAdapter(base, txn, ""); err == nil {
		t.Fatal("empty tenant ID must be refused")
	}
}
