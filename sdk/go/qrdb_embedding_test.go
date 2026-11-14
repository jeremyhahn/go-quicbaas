// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

package quicbaas

import (
	"context"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// A qrdb client that fails to build surfaces its error from the accessor.
func TestQRDBEmbedding_PropagatesQRDBClientError(t *testing.T) {
	c, err := NewClient("localhost:8080", WithREST())
	require.NoError(t, err)

	cc := c.(*client)
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
