// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build qrdb_vector

package quicbaas

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	qrdbapigrpc "github.com/jeremyhahn/go-qrdb/pkg/api/grpc"
	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// fakeRAGServicer is go-qrdb's RAG servicer with fixed answers; it records
// every request it receives.
type fakeRAGServicer struct {
	mu      sync.Mutex
	ingests []*qrdbservice.IngestDocumentRequest
	queries []*qrdbservice.RAGQueryRequest
	deletes []*qrdbservice.DeleteRAGDocumentRequest
	lists   []*qrdbservice.ListRAGDocumentsRequest
}

var _ qrdbservice.RAGServicer = (*fakeRAGServicer)(nil)

func (f *fakeRAGServicer) IngestDocument(_ context.Context, req *qrdbservice.IngestDocumentRequest) (*qrdbservice.IngestDocumentResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ingests = append(f.ingests, req)
	return &qrdbservice.IngestDocumentResponse{DocumentID: req.DocumentID, ChunkCount: 2, Namespace: req.Namespace}, nil
}

func (f *fakeRAGServicer) Query(_ context.Context, req *qrdbservice.RAGQueryRequest) (*qrdbservice.RAGQueryResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, req)
	return &qrdbservice.RAGQueryResponse{Results: []*qrdbservice.RAGQueryResult{
		{ChunkText: "c", Score: 0.5, DocumentID: "d", ChunkIndex: 1},
	}}, nil
}

func (f *fakeRAGServicer) DeleteDocument(_ context.Context, req *qrdbservice.DeleteRAGDocumentRequest) (*qrdbservice.DeleteRAGDocumentResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, req)
	return &qrdbservice.DeleteRAGDocumentResponse{DocumentID: req.DocumentID, ChunksRemoved: 2}, nil
}

func (f *fakeRAGServicer) ListDocuments(_ context.Context, req *qrdbservice.ListRAGDocumentsRequest) (*qrdbservice.ListRAGDocumentsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists = append(f.lists, req)
	return &qrdbservice.ListRAGDocumentsResponse{
		Documents: []*qrdbservice.RAGDocumentInfo{{ID: "d", Namespace: req.Namespace}},
		Total:     1,
	}, nil
}

func (f *fakeRAGServicer) Close() error { return nil }

// ragBearerRecorder records the authorization metadata of every RAG RPC the
// server receives.
type ragBearerRecorder struct {
	mu  sync.Mutex
	got []string
}

func (r *ragBearerRecorder) intercept(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if strings.Contains(info.FullMethod, "/RAG") {
		md, _ := metadata.FromIncomingContext(ctx)
		r.mu.Lock()
		r.got = append(r.got, md.Get("authorization")...)
		r.mu.Unlock()
	}
	return handler(ctx, req)
}

func (r *ragBearerRecorder) values() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.got...)
}

// serveQRDBRAGGRPC serves go-qrdb's gRPC service with fake as its RAG
// servicer on lis and records the bearer each RAG call carries.
func serveQRDBRAGGRPC(t *testing.T, lis net.Listener, fake *fakeRAGServicer) *ragBearerRecorder {
	t.Helper()
	rec := &ragBearerRecorder{}
	srv := grpc.NewServer(grpc.UnaryInterceptor(rec.intercept))
	qrdbsdk.RegisterGRPCService(srv, nil, nil, qrdbapigrpc.WithRegisterRAGService(fake))
	served := make(chan error, 1)
	go func() { served <- srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	return rec
}

// Over gRPC and Unix, QRDBRAG carries the WithAuthToken bearer to every
// go-qrdb RAG RPC as authorization metadata.
func TestQRDBRAG_GRPCAndUnix_SendPlatformBearer(t *testing.T) {
	cases := []struct {
		name   string
		listen func(t *testing.T) (net.Listener, []Option)
	}{
		{"grpc", func(t *testing.T) (net.Listener, []Option) {
			lis, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			return lis, nil
		}},
		{"unix", func(t *testing.T) (net.Listener, []Option) {
			socket := filepath.Join(shortSocketDir(t), "qrdb.sock")
			lis, err := net.Listen("unix", socket)
			require.NoError(t, err)
			return lis, []Option{WithUnix(socket)}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lis, extra := tc.listen(t)
			fake := &fakeRAGServicer{}
			rec := serveQRDBRAGGRPC(t, lis, fake)

			opts := defaultOptions()
			for _, apply := range append([]Option{WithAuthToken("t-1672")}, extra...) {
				require.NoError(t, apply(opts))
			}
			c := &client{opts: opts, address: lis.Addr().String()}
			t.Cleanup(func() {
				if qc, err := c.qrdbClient(); err == nil {
					if err := qc.Close(); err != nil {
						t.Errorf("close qrdb client: %v", err)
					}
				}
			})

			rc, err := c.qrdbRAG()
			require.NoError(t, err)
			ctx := context.Background()

			ir, err := rc.RAGIngestDocument(ctx, &QRDBRAGIngestRequest{DocumentID: "d", Text: "hello", Namespace: "ns"})
			require.NoError(t, err)
			assert.Equal(t, 2, ir.ChunkCount)

			qr, err := rc.RAGQuery(ctx, &QRDBRAGQueryRequest{Query: "q", Namespace: "ns", TopK: 3})
			require.NoError(t, err)
			require.Len(t, qr.Results, 1)
			assert.Equal(t, "c", qr.Results[0].ChunkText)
			assert.InDelta(t, 0.5, qr.Results[0].Score, 1e-6)
			assert.Equal(t, "d", qr.Results[0].DocumentID)
			assert.Equal(t, 1, qr.Results[0].ChunkIndex)

			dr, err := rc.RAGDeleteDocument(ctx, &QRDBRAGDeleteDocumentRequest{DocumentID: "d", Namespace: "ns"})
			require.NoError(t, err)
			assert.Equal(t, 2, dr.ChunksRemoved)

			lr, err := rc.RAGListDocuments(ctx, &QRDBRAGListDocumentsRequest{Namespace: "ns"})
			require.NoError(t, err)
			assert.Equal(t, 1, lr.Total)
			require.Len(t, lr.Documents, 1)
			assert.Equal(t, "d", lr.Documents[0].ID)

			fake.mu.Lock()
			require.Len(t, fake.ingests, 1)
			assert.Equal(t, "d", fake.ingests[0].DocumentID)
			assert.Equal(t, "ns", fake.ingests[0].Namespace)
			require.Len(t, fake.queries, 1)
			assert.Equal(t, "q", fake.queries[0].Query)
			assert.Equal(t, 3, fake.queries[0].TopK)
			require.Len(t, fake.deletes, 1)
			assert.Equal(t, "d", fake.deletes[0].DocumentID)
			require.Len(t, fake.lists, 1)
			assert.Equal(t, "ns", fake.lists[0].Namespace)
			fake.mu.Unlock()

			assert.Equal(t, []string{"Bearer t-1672", "Bearer t-1672", "Bearer t-1672", "Bearer t-1672"}, rec.values())
		})
	}
}

// fakeBearerRAGClient records the outgoing authorization metadata of each verb.
type fakeBearerRAGClient struct {
	seen map[string][]string
}

func (f *fakeBearerRAGClient) record(ctx context.Context, verb string) {
	md, _ := metadata.FromOutgoingContext(ctx)
	f.seen[verb] = md.Get("authorization")
}

func (f *fakeBearerRAGClient) RAGIngestDocument(ctx context.Context, _ *QRDBRAGIngestRequest) (*QRDBRAGIngestResponse, error) {
	f.record(ctx, "RAGIngestDocument")
	return &QRDBRAGIngestResponse{DocumentID: "ingest"}, nil
}

func (f *fakeBearerRAGClient) RAGQuery(ctx context.Context, _ *QRDBRAGQueryRequest) (*QRDBRAGQueryResponse, error) {
	f.record(ctx, "RAGQuery")
	return &QRDBRAGQueryResponse{Results: []*QRDBRAGQueryResult{{DocumentID: "query"}}}, nil
}

func (f *fakeBearerRAGClient) RAGDeleteDocument(ctx context.Context, _ *QRDBRAGDeleteDocumentRequest) (*QRDBRAGDeleteDocumentResponse, error) {
	f.record(ctx, "RAGDeleteDocument")
	return &QRDBRAGDeleteDocumentResponse{DocumentID: "delete"}, nil
}

func (f *fakeBearerRAGClient) RAGListDocuments(ctx context.Context, _ *QRDBRAGListDocumentsRequest) (*QRDBRAGListDocumentsResponse, error) {
	f.record(ctx, "RAGListDocuments")
	return &QRDBRAGListDocumentsResponse{Total: 7}, nil
}

// The bearer wrapper adds the bearer to every verb and returns the inner
// client's response unchanged.
func TestQRDBRAG_BearerClientForwardsEveryVerb(t *testing.T) {
	fake := &fakeBearerRAGClient{seen: map[string][]string{}}
	b := &qrdbBearerRAGClient{inner: fake, bearer: "Bearer x"}
	ctx := context.Background()

	ir, err := b.RAGIngestDocument(ctx, &QRDBRAGIngestRequest{})
	require.NoError(t, err)
	assert.Equal(t, "ingest", ir.DocumentID)

	qr, err := b.RAGQuery(ctx, &QRDBRAGQueryRequest{})
	require.NoError(t, err)
	assert.Equal(t, "query", qr.Results[0].DocumentID)

	dr, err := b.RAGDeleteDocument(ctx, &QRDBRAGDeleteDocumentRequest{})
	require.NoError(t, err)
	assert.Equal(t, "delete", dr.DocumentID)

	lr, err := b.RAGListDocuments(ctx, &QRDBRAGListDocumentsRequest{})
	require.NoError(t, err)
	assert.Equal(t, 7, lr.Total)

	for _, verb := range []string{"RAGIngestDocument", "RAGQuery", "RAGDeleteDocument", "RAGListDocuments"} {
		assert.Equal(t, []string{"Bearer x"}, fake.seen[verb], verb)
	}
}

// On the embedded transport, QRDBRAG reaches the in-process RAG servicer.
func TestQRDBRAG_EmbeddedRoutesToServicer(t *testing.T) {
	fake := &fakeRAGServicer{}
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{KV: stubKVService{}, Admin: stubAdminService{}, RAG: fake})
	t.Cleanup(func() { _ = c.Close() })

	rc, err := QRDBRAG(c)
	require.NoError(t, err)
	ctx := context.Background()

	ir, err := rc.RAGIngestDocument(ctx, &QRDBRAGIngestRequest{DocumentID: "d", Text: "hello", Namespace: "ns"})
	require.NoError(t, err)
	assert.Equal(t, 2, ir.ChunkCount)

	qr, err := rc.RAGQuery(ctx, &QRDBRAGQueryRequest{Query: "q", Namespace: "ns", TopK: 1})
	require.NoError(t, err)
	require.Len(t, qr.Results, 1)
	assert.Equal(t, "c", qr.Results[0].ChunkText)

	fake.mu.Lock()
	defer fake.mu.Unlock()
	require.Len(t, fake.ingests, 1)
	assert.Equal(t, "d", fake.ingests[0].DocumentID)
	require.Len(t, fake.queries, 1)
	assert.Equal(t, "q", fake.queries[0].Query)
}

// ragAdapterTransport is a quicbaas transport with its own go-qrdb RAG adapter.
type ragAdapterTransport struct {
	Transport
	rc qrdbtransport.RAGClient
}

func (t ragAdapterTransport) QRDBRAG() qrdbtransport.RAGClient { return t.rc }

// ragNoAdapterTransport is a quicbaas transport without a go-qrdb RAG adapter.
type ragNoAdapterTransport struct{ Transport }

// On REST, MCP and QUIC, QRDBRAG returns the transport's own RAG adapter.
func TestQRDBRAG_DispatchesToTransportAdapter(t *testing.T) {
	for _, tt := range []TransportType{TransportREST, TransportMCP, TransportQUIC} {
		c, err := NewClient("localhost:8080", WithREST())
		require.NoError(t, err)
		cc := c.(*client)
		fake := &fakeBearerRAGClient{seen: map[string][]string{}}
		cc.transport = ragAdapterTransport{rc: fake}
		cc.opts.transportType = tt

		rc, err := QRDBRAG(c)
		require.NoError(t, err)
		assert.Same(t, fake, rc)
	}
}

// A REST client whose transport has no RAG adapter is refused; it never
// falls back to go-qrdb's own REST sub-client, which sends no platform bearer.
func TestQRDBRAG_RESTWithoutAdapterRefused(t *testing.T) {
	c, err := NewClient("localhost:8080", WithREST())
	require.NoError(t, err)
	c.(*client).transport = ragNoAdapterTransport{}

	rc, err := QRDBRAG(c)
	require.Error(t, err)
	assert.True(t, sdkerrors.IsInvalidInput(err), err)
	assert.Nil(t, rc)
}

// An unmapped transport type is refused as invalid input; a gRPC qrdb
// sub-client whose go-qrdb transport has no RAG client is unavailable.
func TestQRDBRAG_TransportWithoutRAG(t *testing.T) {
	c, err := NewClient("localhost:8080", WithREST())
	require.NoError(t, err)
	cc := c.(*client)
	cc.transport = ragNoAdapterTransport{}
	cc.opts.transportType = TransportType(999)

	rc, err := QRDBRAG(c)
	require.Error(t, err)
	assert.True(t, sdkerrors.IsInvalidInput(err), err)
	assert.Nil(t, rc)

	qc, err := qrdbsdk.NewClient(noEmbeddingTransport{})
	require.NoError(t, err)
	cc.opts.transportType = TransportGRPC
	cc.qrdb.once.Do(func() {})
	cc.qrdb.client = qc

	rc, err = QRDBRAG(c)
	require.Error(t, err)
	assert.True(t, sdkerrors.IsUnavailable(err), err)
	assert.Nil(t, rc)
	assert.Contains(t, err.Error(), "RAG")
}

// A qrdb sub-client build error is returned as is.
func TestQRDBRAG_PropagatesQRDBClientError(t *testing.T) {
	c, err := NewClient("localhost:8080", WithREST())
	require.NoError(t, err)
	cc := c.(*client)
	cc.transport = ragNoAdapterTransport{}
	cc.opts.transportType = TransportGRPC
	cc.qrdb.once.Do(func() {})
	cc.qrdb.err = assert.AnError

	rc, err := QRDBRAG(c)
	require.ErrorIs(t, err, assert.AnError)
	assert.Nil(t, rc)
}

// After Close, QRDBRAG is refused with the closed-client error.
func TestQRDBRAG_ClosedClientRefused(t *testing.T) {
	fake := &fakeRAGServicer{}
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{KV: stubKVService{}, Admin: stubAdminService{}, RAG: fake})
	require.NoError(t, c.Close())

	rc, err := QRDBRAG(c)
	require.Error(t, err)
	assert.Nil(t, rc)
	assert.Equal(t, errQRDBClientClosed().Error(), err.Error())
	assert.Equal(t, sdkerrors.Code(errQRDBClientClosed()), sdkerrors.Code(err))
}

// ragForeignClient implements Client without being the SDK's client.
type ragForeignClient struct{ Client }

// A Client that is not the SDK's own is refused as invalid input.
func TestQRDBRAG_NotSDKClient(t *testing.T) {
	rc, err := QRDBRAG(ragForeignClient{})
	require.Error(t, err)
	assert.True(t, sdkerrors.IsInvalidInput(err), err)
	assert.Nil(t, rc)
}
