// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

package quicbaas

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	qrdbapigrpc "github.com/jeremyhahn/go-qrdb/pkg/api/grpc"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// bearerRecorder records the authorization metadata of every Embed RPC the
// server receives.
type bearerRecorder struct {
	mu  sync.Mutex
	got []string
}

func (r *bearerRecorder) intercept(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if strings.HasSuffix(info.FullMethod, "/Embed") {
		md, _ := metadata.FromIncomingContext(ctx)
		r.mu.Lock()
		r.got = append(r.got, md.Get("authorization")...)
		r.mu.Unlock()
	}
	return handler(ctx, req)
}

func (r *bearerRecorder) values() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.got...)
}

// serveQRDBEmbeddingGRPC serves go-qrdb's gRPC service with an embedding
// servicer on lis and records the bearer each Embed call carries.
func serveQRDBEmbeddingGRPC(t *testing.T, lis net.Listener) *bearerRecorder {
	t.Helper()
	rec := &bearerRecorder{}
	srv := grpc.NewServer(grpc.UnaryInterceptor(rec.intercept))
	fake := &fakeEmbeddingServicer{}
	qrdbsdk.RegisterGRPCService(srv, nil, nil, qrdbapigrpc.WithRegisterEmbedding(fake))
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

// Over gRPC and Unix, AI().QRDBEmbedding() carries the WithAuthToken bearer
// to go-qrdb's embedding RPCs as authorization metadata.
func TestQRDBEmbedding_GRPCAndUnix_SendPlatformBearer(t *testing.T) {
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
			rec := serveQRDBEmbeddingGRPC(t, lis)

			opts := defaultOptions()
			for _, apply := range append([]Option{WithAuthToken("t-2506")}, extra...) {
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

			ec, err := c.qrdbEmbedding()
			require.NoError(t, err)
			resp, err := ec.Embed(context.Background(), &QRDBEmbedRequest{Texts: []string{"hi"}})
			require.NoError(t, err)
			assert.Equal(t, [][]float32{{1, 2}}, resp.Embeddings)
			assert.Equal(t, []string{"Bearer t-2506"}, rec.values())
		})
	}
}

// fakeBearerEmbeddingClient records the outgoing authorization metadata of
// each verb.
type fakeBearerEmbeddingClient struct {
	seen map[string][]string
}

func (f *fakeBearerEmbeddingClient) record(ctx context.Context, verb string) {
	md, _ := metadata.FromOutgoingContext(ctx)
	f.seen[verb] = md.Get("authorization")
}

func (f *fakeBearerEmbeddingClient) Embed(ctx context.Context, _ *QRDBEmbedRequest) (*QRDBEmbedResponse, error) {
	f.record(ctx, "Embed")
	return &QRDBEmbedResponse{Model: "embed"}, nil
}

func (f *fakeBearerEmbeddingClient) BatchEmbed(ctx context.Context, _ *QRDBBatchEmbedRequest) (*QRDBBatchEmbedResponse, error) {
	f.record(ctx, "BatchEmbed")
	return &QRDBBatchEmbedResponse{Model: "batch"}, nil
}

func (f *fakeBearerEmbeddingClient) ListEmbeddingModels(ctx context.Context, _ string) (*QRDBListEmbeddingModelsResponse, error) {
	f.record(ctx, "ListEmbeddingModels")
	return &QRDBListEmbeddingModelsResponse{Models: []QRDBEmbeddingModelInfo{{Name: "list"}}}, nil
}

// The bearer wrapper adds the bearer to every verb and returns the inner
// client's response unchanged.
func TestQRDBEmbedding_BearerClientForwardsEveryVerb(t *testing.T) {
	fake := &fakeBearerEmbeddingClient{seen: map[string][]string{}}
	b := &qrdbBearerEmbeddingClient{inner: fake, bearer: "Bearer x"}
	ctx := context.Background()

	er, err := b.Embed(ctx, &QRDBEmbedRequest{Texts: []string{"a"}})
	require.NoError(t, err)
	assert.Equal(t, "embed", er.Model)

	br, err := b.BatchEmbed(ctx, &QRDBBatchEmbedRequest{Texts: []string{"a"}})
	require.NoError(t, err)
	assert.Equal(t, "batch", br.Model)

	lr, err := b.ListEmbeddingModels(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, "list", lr.Models[0].Name)

	for _, verb := range []string{"Embed", "BatchEmbed", "ListEmbeddingModels"} {
		assert.Equal(t, []string{"Bearer x"}, fake.seen[verb], verb)
	}
}

// Only the transports whose qrdb sub-client is built on gRPC take the
// metadata bearer.
func TestQRDBSubClientSpeaksGRPC(t *testing.T) {
	cases := []struct {
		tt   TransportType
		want bool
	}{
		{TransportGRPC, true},
		{TransportUnix, true},
		{TransportType(999), true},
		{TransportREST, false},
		{TransportQUIC, false},
		{TransportMCP, false},
		{TransportEmbedded, false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, qrdbSubClientSpeaksGRPC(tc.tt), "transport %v", tc.tt)
	}
}
