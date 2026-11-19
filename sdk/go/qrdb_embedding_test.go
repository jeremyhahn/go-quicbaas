// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

package quicbaas

import (
	"context"
	"testing"
	"time"

	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/rest"
)

// The re-exported embedding types are go-qrdb's own types: a value of each
// alias is assignable to the go-qrdb type and back, so SDK consumers never
// import go-qrdb/sdk/go to call its embedding service.
func TestQRDBEmbedding_TypeAliasesAreGoQRDBTypes(t *testing.T) {
	var ec QRDBEmbeddingClient
	var _ qrdbtransport.EmbeddingClient = ec

	req := &QRDBEmbedRequest{Texts: []string{"a"}, Model: "m", Provider: "ollama"}
	var _ *qrdbtransport.EmbedRequest = req
	var _ *qrdbtransport.EmbedResponse = &QRDBEmbedResponse{}
	var _ *qrdbtransport.BatchEmbedRequest = &QRDBBatchEmbedRequest{BatchSize: 8}
	var _ *qrdbtransport.BatchEmbedResponse = &QRDBBatchEmbedResponse{}
	models := &QRDBListEmbeddingModelsResponse{Models: []QRDBEmbeddingModelInfo{{Name: "m", Dimensions: 3}}}
	var _ *qrdbtransport.ListEmbeddingModelsResponse = models
	assert.Equal(t, "m", models.Models[0].Name)
}

// AI().QRDBEmbedding() returns go-qrdb's embedding client over the network
// transports whose qrdb sub-client is built without dialing (REST, gRPC).
func TestQRDBEmbedding_ReturnsGoQRDBClient(t *testing.T) {
	cases := []struct {
		name      string
		transport TransportType
	}{
		{"rest", TransportREST},
		// An unmapped transport type builds the qrdb sub-client on gRPC.
		{"grpc", TransportType(999)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewClient("localhost:8080", WithREST(), WithTimeout(time.Second))
			require.NoError(t, err)
			c.(*client).opts.transportType = tc.transport

			ec, err := c.AI().QRDBEmbedding()
			require.NoError(t, err)
			require.NotNil(t, ec)

			if tc.transport == TransportREST {
				assert.IsType(t, &rest.QRDBEmbeddingClient{}, ec, "over REST the quicbaas transport carries the call, so the bearer is sent")
				return
			}
			qc, err := c.(*client).qrdbClient()
			require.NoError(t, err)
			assert.IsType(t, qc.Embedding(), ec, "the accessor hands back go-qrdb's own embedding client")
		})
	}
}

// QuicBaaS's own embedding service stays reachable beside go-qrdb's: the two
// run in parallel until the owner retires QuicBaaS's.
func TestQRDBEmbedding_ParallelToOwnService(t *testing.T) {
	c, err := NewClient("localhost:8080", WithREST())
	require.NoError(t, err)

	assert.NotNil(t, c.AI().Embedding())
	ec, err := c.AI().QRDBEmbedding()
	require.NoError(t, err)
	assert.NotNil(t, ec)
}

// The two services are independent: a qrdb sub-client that fails to build
// leaves QuicBaaS's own Embedding() working, and QuicBaaS's own client is not
// go-qrdb's (neither wraps the other).
func TestQRDBEmbedding_ParallelServicesAreIndependent(t *testing.T) {
	c, err := NewClient("localhost:8080", WithREST())
	require.NoError(t, err)

	own := c.AI().Embedding()
	require.NotNil(t, own)
	_, ownIsQRDB := any(own).(QRDBEmbeddingClient)
	assert.False(t, ownIsQRDB, "QuicBaaS's own embedding client must not be go-qrdb's")

	cc := c.(*client)
	// An unmapped transport type takes the qrdb sub-client path, as on gRPC.
	cc.opts.transportType = TransportType(999)
	cc.qrdb.once.Do(func() {})
	cc.qrdb.err = assert.AnError

	_, err = c.AI().QRDBEmbedding()
	require.ErrorIs(t, err, assert.AnError)
	assert.Same(t, own, c.AI().Embedding(), "QuicBaaS's own service survives a qrdb build failure")
}

// A qrdb client that fails to build surfaces its error from the accessor.
func TestQRDBEmbedding_PropagatesQRDBClientError(t *testing.T) {
	c, err := NewClient("localhost:8080", WithREST())
	require.NoError(t, err)

	cc := c.(*client)
	// An unmapped transport type takes the qrdb sub-client path, as on gRPC.
	cc.opts.transportType = TransportType(999)
	cc.qrdb.once.Do(func() {})
	cc.qrdb.err = assert.AnError

	ec, err := cc.AI().QRDBEmbedding()
	require.ErrorIs(t, err, assert.AnError)
	assert.Nil(t, ec)
}

// A qrdb transport without an embedding surface yields an Unavailable error,
// never a nil client with a nil error.
func TestQRDBEmbedding_TransportWithoutEmbedding(t *testing.T) {
	c, err := NewClient("localhost:8080", WithREST())
	require.NoError(t, err)

	qc, err := qrdbsdk.NewClient(noEmbeddingTransport{})
	require.NoError(t, err)

	cc := c.(*client)
	// An unmapped transport type takes the qrdb sub-client path, as on gRPC.
	cc.opts.transportType = TransportType(999)
	cc.qrdb.once.Do(func() {})
	cc.qrdb.client = qc

	ec, err := cc.AI().QRDBEmbedding()
	require.Error(t, err)
	assert.Nil(t, ec)
	assert.Contains(t, err.Error(), "embedding")
}

// noEmbeddingTransport is a qrdb transport that implements only the base
// Transport interface, not EmbeddingClient.
type noEmbeddingTransport struct{ qrdbtransport.Transport }

func (noEmbeddingTransport) Connect(context.Context) error { return nil }
func (noEmbeddingTransport) Close() error                  { return nil }

// fakeEmbeddingServicer records the request go-qrdb's embedded transport
// routes to it and answers with a fixed vector.
type fakeEmbeddingServicer struct {
	qrdbservice.EmbeddingServicer
	got *qrdbservice.EmbedRequest
}

func (f *fakeEmbeddingServicer) Embed(_ context.Context, req *qrdbservice.EmbedRequest) (*qrdbservice.EmbedResponse, error) {
	f.got = req
	return &qrdbservice.EmbedResponse{Embeddings: [][]float32{{1, 2}}, Model: "m"}, nil
}

// An embedded client routes AI().QRDBEmbedding() calls to the injected
// go-qrdb embedding servicer.
func TestQRDBEmbedding_EmbeddedRoutesToServicer(t *testing.T) {
	fake := &fakeEmbeddingServicer{}
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{KV: stubKVService{}, Admin: stubAdminService{}, Embedding: fake})

	ec, err := c.AI().QRDBEmbedding()
	require.NoError(t, err)

	resp, err := ec.Embed(context.Background(), &QRDBEmbedRequest{Texts: []string{"hi"}})
	require.NoError(t, err)
	assert.Equal(t, [][]float32{{1, 2}}, resp.Embeddings)
	require.NotNil(t, fake.got)
	assert.Equal(t, []string{"hi"}, fake.got.Texts)
}
