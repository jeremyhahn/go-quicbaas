// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package quicbaas

import (
	"context"
	"strings"
	"sync"
	"testing"

	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	qrdbembedded "github.com/jeremyhahn/go-qrdb/sdk/go/transport/embedded"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// recordingKVService is a go-qrdb KV servicer that stores what it is given,
// so a test can see a write made through QRDBData().KV() land in the
// embedded go-qrdb node's servicer.
type recordingKVService struct {
	stubKVService
	mu   sync.Mutex
	data map[string][]byte
}

func (s *recordingKVService) put(key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = map[string][]byte{}
	}
	s.data[key] = append([]byte(nil), value...)
	return nil
}

func (s *recordingKVService) get(key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	if !ok {
		return nil, stubErr
	}
	return v, nil
}

func (s *recordingKVService) Put(_ context.Context, key string, value []byte) error {
	return s.put(key, value)
}

func (s *recordingKVService) PutWithMode(_ context.Context, key string, value []byte, _ qrdbsdk.WriteMode) error {
	return s.put(key, value)
}

func (s *recordingKVService) Get(_ context.Context, key string) ([]byte, error) {
	return s.get(key)
}

func (s *recordingKVService) GetWithConsistency(_ context.Context, key string, _ qrdbsdk.ReadOptions) ([]byte, error) {
	return s.get(key)
}

// TestQRDBData_ReturnsGoQRDBClients proves the six data services are
// go-qrdb's own clients: each accessor of QRDBData() returns exactly what
// the go-qrdb SDK client this QuicBaaS client carries returns.
func TestQRDBData_ReturnsGoQRDBClients(t *testing.T) {
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:    &recordingKVService{},
		Admin: stubAdminService{},
	})

	data, err := c.QRDBData()
	if err != nil {
		t.Fatalf("QRDBData(): %v", err)
	}
	qrdb, err := c.qrdbClient()
	if err != nil {
		t.Fatalf("qrdbClient(): %v", err)
	}
	if got, ok := data.(*QRDBClient); !ok || got != qrdb {
		t.Fatalf("QRDBData() = %T, want the go-qrdb SDK client the QuicBaaS client carries", data)
	}

	checks := []struct {
		name string
		got  any
	}{
		{"KV", data.KV()},
		{"Batch", data.Batch()},
		{"Transaction", data.Transaction()},
		{"MVCC", data.MVCC()},
		{"Collections", data.Collections()},
		{"Index", data.Index()},
	}
	for _, ck := range checks {
		if ck.got == nil {
			t.Errorf("QRDBData().%s() is nil", ck.name)
		}
	}
}

// TestQRDBData_KVReachesEmbeddedNode proves a write and a read through
// QRDBData().KV() are served by the embedded go-qrdb node's KV servicer.
func TestQRDBData_KVReachesEmbeddedNode(t *testing.T) {
	kv := &recordingKVService{}
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:    kv,
		Admin: stubAdminService{},
	})

	data, err := c.QRDBData()
	if err != nil {
		t.Fatalf("QRDBData(): %v", err)
	}
	ctx := context.Background()
	if err := data.KV().Put(ctx, "row1652/key", []byte("value")); err != nil {
		t.Fatalf("KV().Put: %v", err)
	}
	if got, err := kv.get("row1652/key"); err != nil || string(got) != "value" {
		t.Fatalf("servicer holds %q (err %v), want %q", got, err, "value")
	}
	got, err := data.KV().Get(ctx, "row1652/key")
	if err != nil {
		t.Fatalf("KV().Get: %v", err)
	}
	if string(got) != "value" {
		t.Fatalf("KV().Get = %q, want %q", got, "value")
	}
}

// TestQRDBData_PropagatesQRDBInitError proves QRDBData reports the go-qrdb
// client's construction error and returns no service: TransportEmbedded
// without WithEmbeddedQRDB cannot build a go-qrdb client.
func TestQRDBData_PropagatesQRDBInitError(t *testing.T) {
	c := &client{opts: defaultOptions(), address: "embedded://local"}
	c.opts.transportType = TransportEmbedded
	c.opts.embeddedQRDB = nil

	data, err := c.QRDBData()
	if err == nil {
		t.Fatal("QRDBData() returned no error for TransportEmbedded without WithEmbeddedQRDB")
	}
	if !sdkerrors.IsInvalidInput(err) || !strings.Contains(err.Error(), "WithEmbeddedQRDB") {
		t.Errorf("QRDBData() error = %v, want the go-qrdb client's WithEmbeddedQRDB construction error", err)
	}
	if data != nil {
		t.Errorf("QRDBData() = %#v alongside error %v, want a nil service", data, err)
	}
}

// The reached* servicers below stand in for the embedded node's data
// services. Each embeds its go-qrdb interface (nil) and overrides only the
// method the probe reaches, so a call landing anywhere else fails loudly
// with a nil-pointer panic.

type reachedBatchService struct {
	qrdbservice.BatchServicer
	reached bool
}

func (s *reachedBatchService) Execute(_ context.Context, ops []qrdbservice.Op) ([]qrdbservice.OpResponse, error) {
	s.reached = true
	return make([]qrdbservice.OpResponse, len(ops)), nil
}

func (s *reachedBatchService) ExecuteWithMode(ctx context.Context, ops []qrdbservice.Op, _ qrdbsdk.WriteMode) ([]qrdbservice.OpResponse, error) {
	return s.Execute(ctx, ops)
}

type reachedTransactionService struct {
	qrdbservice.TransactionServicer
	reached bool
}

func (s *reachedTransactionService) Execute(_ context.Context, _ []qrdbservice.Compare, _, _ []qrdbservice.Op) (*qrdbservice.TransactionResponse, error) {
	s.reached = true
	return &qrdbservice.TransactionResponse{Succeeded: true}, nil
}

func (s *reachedTransactionService) ExecuteWithMode(ctx context.Context, compares []qrdbservice.Compare, successOps, failureOps []qrdbservice.Op, _ qrdbsdk.WriteMode) (*qrdbservice.TransactionResponse, error) {
	return s.Execute(ctx, compares, successOps, failureOps)
}

type reachedMVCCService struct {
	qrdbservice.MVCCKVServicer
}

func (reachedMVCCService) CurrentRevision(context.Context) (int64, error) {
	return 1652, nil
}

type reachedCollectionService struct {
	qrdbservice.CollectionServicer
	database string
}

func (s *reachedCollectionService) ListCollections(_ context.Context, req *qrdbservice.ListCollectionsRequest) (*qrdbservice.ListCollectionsResponse, error) {
	s.database = req.Database
	return &qrdbservice.ListCollectionsResponse{}, nil
}

type reachedIndexService struct {
	qrdbservice.IndexServicer
	reached bool
}

func (s *reachedIndexService) ListIndexes(context.Context) ([]string, error) {
	s.reached = true
	return []string{"row1652_idx"}, nil
}

// TestQRDBData_EveryAccessorReachesEmbeddedNode proves that, when the client
// carries the embedded node's own go-qrdb client in
// QRDBEmbeddedServicers.Data, QRDBData() returns it on any transport
// selection, and each of the six data services reaches the node.
func TestQRDBData_EveryAccessorReachesEmbeddedNode(t *testing.T) {
	kv := &recordingKVService{}
	batch := &reachedBatchService{}
	txn := &reachedTransactionService{}
	coll := &reachedCollectionService{}
	idx := &reachedIndexService{}
	tr, err := qrdbembedded.New(kv, stubAdminService{},
		qrdbembedded.WithBatchService(batch),
		qrdbembedded.WithTransactionService(txn),
		qrdbembedded.WithMVCCService(reachedMVCCService{}),
		qrdbembedded.WithCollectionService(coll),
		qrdbembedded.WithIndexService(idx),
	)
	if err != nil {
		t.Fatalf("embedded.New: %v", err)
	}
	node, err := qrdbsdk.NewClient(tr)
	if err != nil {
		t.Fatalf("qrdbsdk.NewClient: %v", err)
	}
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:    kv,
		Admin: stubAdminService{},
		Data:  node,
	})

	probe := func(t *testing.T) {
		batch.reached, txn.reached, idx.reached, coll.database = false, false, false, ""
		ctx := context.Background()

		data, err := c.QRDBData()
		if err != nil {
			t.Fatalf("QRDBData(): %v", err)
		}
		if got, ok := data.(*qrdbsdk.Client); !ok || got != node {
			t.Fatalf("QRDBData() = %T, want the embedded node's own client", data)
		}

		put := qrdbtransport.Op{Type: qrdbtransport.OpPut, Key: []byte("row1652/k"), Value: []byte("v")}
		if err := data.KV().Put(ctx, "row1652/k", []byte("v")); err != nil {
			t.Errorf("KV().Put: %v", err)
		}
		if _, err := data.Batch().ExecuteBatch(ctx, []qrdbtransport.Op{put}); err != nil || !batch.reached {
			t.Errorf("Batch().ExecuteBatch: err %v, reached node %v", err, batch.reached)
		}
		if _, err := data.Transaction().ExecuteTransaction(ctx, &qrdbtransport.TransactionRequest{SuccessOps: []qrdbtransport.Op{put}}); err != nil || !txn.reached {
			t.Errorf("Transaction().ExecuteTransaction: err %v, reached node %v", err, txn.reached)
		}
		if rev, err := data.MVCC().CurrentRevision(ctx); err != nil || rev != 1652 {
			t.Errorf("MVCC().CurrentRevision = %d, %v, want 1652", rev, err)
		}
		if _, err := data.Collections().ListCollections(ctx, "row1652db"); err != nil || coll.database != "row1652db" {
			t.Errorf("Collections().ListCollections: err %v, node saw database %q", err, coll.database)
		}
		if _, err := data.Index().IndexList(ctx); err != nil || !idx.reached {
			t.Errorf("Index().IndexList: err %v, reached node %v", err, idx.reached)
		}
	}

	t.Run("DefaultTransport", probe)
	t.Run("EmbeddedTransport", func(t *testing.T) {
		c.opts.transportType = TransportEmbedded
		probe(t)
	})
}

// TestQRDBData_CollectionsFailClosedWithoutNodeClient proves that, without
// the node's own client, QRDBData() still returns this client's go-qrdb
// client, whose embedded transport serves KV only: a collection call fails
// instead of reaching an unwired service.
func TestQRDBData_CollectionsFailClosedWithoutNodeClient(t *testing.T) {
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:    &recordingKVService{},
		Admin: stubAdminService{},
	})
	c.opts.transportType = TransportEmbedded

	data, err := c.QRDBData()
	if err != nil {
		t.Fatalf("QRDBData(): %v", err)
	}
	if _, err := data.Collections().ListCollections(context.Background(), "row1652db"); err == nil {
		t.Fatal("Collections().ListCollections succeeded without the node's client, want an error")
	}
}
