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
	"net"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"strings"
	"sync"
	"testing"

	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	qrdbembedded "github.com/jeremyhahn/go-qrdb/sdk/go/transport/embedded"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	qbtransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
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

func (s *recordingKVService) PutIfAbsent(_ context.Context, key string, value []byte) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; ok {
		return false, nil
	}
	if s.data == nil {
		s.data = map[string][]byte{}
	}
	s.data[key] = append([]byte(nil), value...)
	return true, nil
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
	if got, ok := data.(*preparedQRDBData); !ok || got.inner != QRDBDataService(qrdb) {
		t.Fatalf("QRDBData() = %T, want the go-qrdb SDK client the QuicBaaS client carries, tenant-guarded", data)
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
	tenant   string
}

func (s *reachedCollectionService) ListCollections(ctx context.Context, req *qrdbservice.ListCollectionsRequest) (*qrdbservice.ListCollectionsResponse, error) {
	s.database = req.Database
	s.tenant, _ = qrdbsdk.TenantFromContext(ctx)
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
		if got, ok := data.(*preparedQRDBData); !ok || got.inner != QRDBDataService(node) {
			t.Fatalf("QRDBData() = %T, want the embedded node's own client, tenant-guarded", data)
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

// TestQRDBData_EmbeddedRefusesTenantBoundCalls proves the embedded data
// services, which serve the node's system keyspace, refuse a call bound to
// another tenant before it reaches the node, serve the system tenant, and
// leave collections (registry-resolved per tenant) to the node.
func TestQRDBData_EmbeddedRefusesTenantBoundCalls(t *testing.T) {
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
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{KV: kv, Admin: stubAdminService{}, Data: node})
	data, err := c.QRDBData()
	if err != nil {
		t.Fatalf("QRDBData(): %v", err)
	}

	if err := data.KV().Put(context.Background(), "row1652/sys", []byte("system")); err != nil {
		t.Fatalf("system KV().Put: %v", err)
	}
	ctxSys := qrdbsdk.ContextWithTenant(context.Background(), qrdbsdk.SystemTenantID)
	if got, err := data.KV().Get(ctxSys, "row1652/sys"); err != nil || string(got) != "system" {
		t.Fatalf("system-bound KV().Get = %q, %v, want %q", got, err, "system")
	}

	ctx := qrdbsdk.ContextWithTenant(context.Background(), "row1652-tenant-b")
	put := qrdbtransport.Op{Type: qrdbtransport.OpPut, Key: []byte("row1652/sys"), Value: []byte("b")}
	refused := func(name string, err error) {
		t.Helper()
		if !sdkerrors.IsForbidden(err) {
			t.Errorf("%s bound to another tenant: err %v, want Forbidden", name, err)
		}
	}
	_, err = data.KV().Get(ctx, "row1652/sys")
	refused("KV().Get", err)
	refused("KV().Put", data.KV().Put(ctx, "row1652/sys", []byte("b")))
	refused("KV().Delete", data.KV().Delete(ctx, "row1652/sys"))
	_, err = data.Batch().ExecuteBatch(ctx, []qrdbtransport.Op{put})
	refused("Batch().ExecuteBatch", err)
	_, err = data.Transaction().ExecuteTransaction(ctx, &qrdbtransport.TransactionRequest{SuccessOps: []qrdbtransport.Op{put}})
	refused("Transaction().ExecuteTransaction", err)
	_, err = data.MVCC().CurrentRevision(ctx)
	refused("MVCC().CurrentRevision", err)
	_, err = data.Index().IndexList(ctx)
	refused("Index().IndexList", err)
	if batch.reached || txn.reached || idx.reached {
		t.Errorf("a refused call reached the node: batch %v, transaction %v, index %v", batch.reached, txn.reached, idx.reached)
	}
	if got, err := kv.get("row1652/sys"); err != nil || string(got) != "system" {
		t.Errorf("system record = %q, %v after refused writes, want %q", got, err, "system")
	}

	if _, err := data.Collections().ListCollections(ctx, "row1652db"); err != nil || coll.database != "row1652db" {
		t.Errorf("tenant-bound Collections().ListCollections: err %v, node saw database %q", err, coll.database)
	}
}

// TestQRDBData_EmbeddedHonoursSDKTenantContext proves the embedded guard
// reads the SDK's own tenant binding (transport.ContextWithTenant) as well as
// go-qrdb's: a tenant-bound call is refused rather than served from the
// system keyspace, collections see the SDK tenant, and conflicting bindings
// are refused.
func TestQRDBData_EmbeddedHonoursSDKTenantContext(t *testing.T) {
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
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{KV: kv, Admin: stubAdminService{}, Data: node})
	data, err := c.QRDBData()
	if err != nil {
		t.Fatalf("QRDBData(): %v", err)
	}

	if err := data.KV().Put(context.Background(), "row1652/sys", []byte("system")); err != nil {
		t.Fatalf("system KV().Put: %v", err)
	}

	ctx := qbtransport.ContextWithTenant(context.Background(), "row1652-tenant-b")
	put := qrdbtransport.Op{Type: qrdbtransport.OpPut, Key: []byte("row1652/sys"), Value: []byte("b")}
	refused := func(name string, err error) {
		t.Helper()
		if !sdkerrors.IsForbidden(err) {
			t.Errorf("%s bound to another tenant by the SDK: err %v, want Forbidden", name, err)
		}
	}
	_, err = data.KV().Get(ctx, "row1652/sys")
	refused("KV().Get", err)
	refused("KV().Put", data.KV().Put(ctx, "row1652/sys", []byte("b")))
	refused("KV().Delete", data.KV().Delete(ctx, "row1652/sys"))
	_, err = data.Batch().ExecuteBatch(ctx, []qrdbtransport.Op{put})
	refused("Batch().ExecuteBatch", err)
	_, err = data.Transaction().ExecuteTransaction(ctx, &qrdbtransport.TransactionRequest{SuccessOps: []qrdbtransport.Op{put}})
	refused("Transaction().ExecuteTransaction", err)
	_, err = data.MVCC().CurrentRevision(ctx)
	refused("MVCC().CurrentRevision", err)
	_, err = data.Index().IndexList(ctx)
	refused("Index().IndexList", err)
	if batch.reached || txn.reached || idx.reached {
		t.Errorf("a refused call reached the node: batch %v, transaction %v, index %v", batch.reached, txn.reached, idx.reached)
	}
	if got, err := kv.get("row1652/sys"); err != nil || string(got) != "system" {
		t.Errorf("system record = %q, %v after refused writes, want %q", got, err, "system")
	}

	if _, err := data.Collections().ListCollections(ctx, "row1652db"); err != nil || coll.tenant != "row1652-tenant-b" {
		t.Errorf("SDK-tenant-bound Collections().ListCollections: err %v, node saw tenant %q, want %q", err, coll.tenant, "row1652-tenant-b")
	}

	ctxSys := qbtransport.ContextWithTenant(context.Background(), qrdbsdk.SystemTenantID)
	if got, err := data.KV().Get(ctxSys, "row1652/sys"); err != nil || string(got) != "system" {
		t.Errorf("SDK system-bound KV().Get = %q, %v, want %q", got, err, "system")
	}

	conflict := qrdbsdk.ContextWithTenant(qbtransport.ContextWithTenant(context.Background(), "row1652-tenant-b"), "row1652-tenant-c")
	_, err = data.KV().Get(conflict, "row1652/sys")
	if !sdkerrors.IsForbidden(err) {
		t.Errorf("KV().Get with conflicting tenant bindings: err %v, want Forbidden", err)
	}
	_, err = data.Collections().ListCollections(conflict, "row1652db")
	if !sdkerrors.IsForbidden(err) {
		t.Errorf("Collections().ListCollections with conflicting tenant bindings: err %v, want Forbidden", err)
	}
}

// TestQRDBData_EmbeddedKVKeepsGuardedWrites proves QRDBData().KV() keeps the
// embedded client's guarded-write capability (PutIfAbsent, CompareAndDelete,
// CompareAndSwap), still behind the tenant guard.
func TestQRDBData_EmbeddedKVKeepsGuardedWrites(t *testing.T) {
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
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{KV: kv, Admin: stubAdminService{}, Data: node})
	data, err := c.QRDBData()
	if err != nil {
		t.Fatalf("QRDBData(): %v", err)
	}

	g, ok := data.KV().(qrdbtransport.GuardedKVClient)
	if !ok {
		t.Fatalf("QRDBData().KV() = %T, want a qrdbtransport.GuardedKVClient", data.KV())
	}
	if put, err := g.PutIfAbsent(context.Background(), "row1652/once", []byte("a")); err != nil || !put {
		t.Fatalf("first PutIfAbsent = %v, %v, want true, nil", put, err)
	}
	if put, err := g.PutIfAbsent(context.Background(), "row1652/once", []byte("b")); err != nil || put {
		t.Fatalf("second PutIfAbsent = %v, %v, want false, nil", put, err)
	}
	if got, err := kv.get("row1652/once"); err != nil || string(got) != "a" {
		t.Fatalf("servicer holds %q (err %v), want %q", got, err, "a")
	}
	ctx := qrdbsdk.ContextWithTenant(context.Background(), "row1652-tenant-b")
	if _, err := g.PutIfAbsent(ctx, "row1652/tenant", []byte("b")); !sdkerrors.IsForbidden(err) {
		t.Errorf("tenant-bound PutIfAbsent: err %v, want Forbidden", err)
	}
}

// TestQRDBData_UnixCarriesBearerToken proves a client configured with
// WithAuthToken sends the token on QRDBData() calls over the Unix socket
// transport: a server that refuses calls without
// "authorization: Bearer <token>" serves them.
func TestQRDBData_UnixCarriesBearerToken(t *testing.T) {
	const token = "row1652-token"
	var mu sync.Mutex
	var seen []string
	srv := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		md, _ := metadata.FromIncomingContext(stream.Context())
		mu.Lock()
		seen = append(seen, strings.Join(md.Get("authorization"), ","))
		mu.Unlock()
		if got := md.Get("authorization"); len(got) != 1 || got[0] != "Bearer "+token {
			return status.Error(codes.Unauthenticated, "bearer token required")
		}
		var req emptypb.Empty
		if err := stream.RecvMsg(&req); err != nil {
			return err
		}
		return stream.SendMsg(&emptypb.Empty{})
	}))
	dir, err := os.MkdirTemp("", "qb1652")
	if err != nil {
		t.Fatalf("socket dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "q.sock")
	lis, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	newData := func(t *testing.T, opts ...Option) QRDBDataService {
		t.Helper()
		c := &client{opts: defaultOptions(), address: sock}
		c.opts.transportType = TransportUnix
		c.opts.tlsEnabled = false
		for _, o := range opts {
			o(c.opts)
		}
		t.Cleanup(func() { _ = c.Close() })
		data, err := c.QRDBData()
		if err != nil {
			t.Fatalf("QRDBData(): %v", err)
		}
		return data
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := newData(t).KV().Put(ctx, "row1652/k", []byte("v")); err == nil {
		t.Fatal("anonymous KV().Put succeeded against a server that requires a bearer token")
	}
	data := newData(t, WithAuthToken(token))
	if err := data.KV().Put(ctx, "row1652/k", []byte("v")); err != nil {
		t.Fatalf("bearer-authenticated KV().Put: %v (server saw %q)", err, seen)
	}
	if _, err := data.Collections().ListCollections(ctx, "row1652db"); err != nil {
		t.Fatalf("bearer-authenticated Collections().ListCollections: %v (server saw %q)", err, seen)
	}
}

// TestQRDBData_GRPCCarriesBearerToken proves a client configured with
// WithAuthToken sends the token on QRDBData() calls over gRPC: a server that
// refuses calls without "authorization: Bearer <token>" serves them.
func TestQRDBData_GRPCCarriesBearerToken(t *testing.T) {
	const token = "row1652-token"
	var mu sync.Mutex
	var seen []string
	srv := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		md, _ := metadata.FromIncomingContext(stream.Context())
		mu.Lock()
		seen = append(seen, strings.Join(md.Get("authorization"), ","))
		mu.Unlock()
		if got := md.Get("authorization"); len(got) != 1 || got[0] != "Bearer "+token {
			return status.Error(codes.Unauthenticated, "bearer token required")
		}
		var req emptypb.Empty
		if err := stream.RecvMsg(&req); err != nil {
			return err
		}
		return stream.SendMsg(&emptypb.Empty{})
	}))
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	newData := func(t *testing.T, opts ...Option) QRDBDataService {
		t.Helper()
		c := &client{opts: defaultOptions(), address: lis.Addr().String()}
		for _, o := range opts {
			o(c.opts)
		}
		t.Cleanup(func() { _ = c.Close() })
		data, err := c.QRDBData()
		if err != nil {
			t.Fatalf("QRDBData(): %v", err)
		}
		return data
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := newData(t).KV().Put(ctx, "row1652/k", []byte("v")); err == nil {
		t.Fatal("anonymous KV().Put succeeded against a server that requires a bearer token")
	}
	data := newData(t, WithAuthToken(token))
	if err := data.KV().Put(ctx, "row1652/k", []byte("v")); err != nil {
		t.Fatalf("bearer-authenticated KV().Put: %v (server saw %q)", err, seen)
	}
	if _, err := data.Collections().ListCollections(ctx, "row1652db"); err != nil {
		t.Fatalf("bearer-authenticated Collections().ListCollections: %v (server saw %q)", err, seen)
	}
}

// TestQRDBData_RefusesBearerOnTransportsWithoutSeam proves a client with an
// auth token on a transport whose go-qrdb client cannot carry it gets an
// error, not a data service that would call anonymously.
func TestQRDBData_RefusesBearerOnTransportsWithoutSeam(t *testing.T) {
	for _, tt := range []TransportType{TransportREST, TransportQUIC, TransportMCP} {
		c := &client{opts: defaultOptions(), address: "127.0.0.1:1"}
		c.opts.transportType = tt
		WithAuthToken("row1652-token")(c.opts)
		data, err := c.QRDBData()
		if !sdkerrors.IsUnimplemented(err) || data != nil {
			t.Errorf("transport %v: QRDBData() = %v, %v, want a nil service and Unimplemented", tt, data, err)
		}
		_ = c.Close()
	}
}

// TestQRDBData_RefusesMCPCredentials pins that an MCP token or MCP
// username/password, which go-qrdb's MCP client cannot carry, gets an
// Unimplemented error rather than anonymous data calls.
func TestQRDBData_RefusesMCPCredentials(t *testing.T) {
	for name, opt := range map[string]Option{
		"mcp token":       WithMCPAuthToken("row1652-mcp"),
		"mcp credentials": WithMCPCredentials("u1652", "p1652"),
	} {
		c := &client{opts: defaultOptions(), address: "127.0.0.1:1"}
		c.opts.transportType = TransportMCP
		opt(c.opts)
		if c.opts.authToken != "" {
			t.Fatalf("%s: authToken must stay empty", name)
		}
		data, err := c.QRDBData()
		if !sdkerrors.IsUnimplemented(err) || data != nil {
			t.Errorf("%s: QRDBData() = %v, %v, want a nil service and Unimplemented", name, data, err)
		}
		_ = c.Close()
	}
}
