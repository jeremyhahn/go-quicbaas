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

package quic

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorage"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDoRequest_RemainingServiceDispatch drives doRequest end-to-end through the
// TimeSeries, Certificate, Messaging and ObjectStorage decode branches using the
// mock server, proving the wire round-trip and the service-specific decode each
// SDK client relies on.
func TestDoRequest_RemainingServiceDispatch(t *testing.T) {
	server, err := newMockQUICServer(t)
	require.NoError(t, err)

	// TimeSeries Query (request transform + typed response decode).
	tsQuery, _ := EncodePayload(&service.TSQueryResponse{
		Metric: "cpu",
		Points: []*service.TSDataPoint{{Value: 1.5, Timestamp: time.Unix(1, 0).UTC()}},
	})
	server.RegisterHandler(OpTSQuery, okResponse(tsQuery))

	// TimeSeries Write (request transform + void/nil decode).
	tsWrite, _ := EncodePayload(&service.WritePointResponse{Success: true})
	server.RegisterHandler(OpTSWritePoint, okResponse(tsWrite))

	// Certificate IssueCertificate (typed decode).
	certIssue, _ := EncodePayload(&service.IssueCertificateResponse{SerialNumber: "0a", CertificatePEM: "pem"})
	server.RegisterHandler(OpCertIssue, okResponse(certIssue))

	// Messaging GetStats. Shares OpMsg (0x00) with the scheduler dispatch byte;
	// the mock keys purely on the opcode, so this handler answers the messaging
	// GetStats call below.
	msgStats, _ := EncodePayload(&msgStatsRespWire{TotalChannels: 4, MessagesPublished: 100})
	server.RegisterHandler(OpMsg, okResponse(msgStats))

	// ObjectStorage PutObject (typed decode).
	osPut, _ := EncodePayload(&objectstorage.PutObjectResponse{Key: "ns:doc", ObjectID: 7, Size: 1024})
	server.RegisterHandler(OpOSPutObject, okResponse(osPut))

	trans := newDispatchTestTransport(t, server)
	ctx := context.Background()

	t.Run("TimeSeriesQuery", func(t *testing.T) {
		got, err := trans.Request(ctx, "quicbaas.TimeSeries", "Query", &timeseries.QueryRequest{
			Metric: "cpu",
			Start:  time.Unix(0, 0),
			End:    time.Unix(10, 0),
		})
		require.NoError(t, err)
		resp := got.(*timeseries.QueryResponse)
		require.Len(t, resp.Series, 1)
		assert.Equal(t, 1.5, resp.Series[0].Value)
	})

	t.Run("TimeSeriesWrite", func(t *testing.T) {
		got, err := trans.Request(ctx, "quicbaas.TimeSeries", "Write", &timeseries.WriteRequest{
			Metric:    "cpu",
			Value:     2.0,
			Timestamp: time.Unix(1, 0),
		})
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("Certificate", func(t *testing.T) {
		got, err := trans.Request(ctx, "quicbaas.Certificate", "IssueCertificate",
			&certificate.IssueCertificateRequest{CommonName: "example.com"})
		require.NoError(t, err)
		assert.Equal(t, "0a", got.(*certificate.IssueCertificateResponse).SerialNumber)
	})

	t.Run("Messaging", func(t *testing.T) {
		got, err := trans.Request(ctx, "quicbaas.Messaging", "GetStats", &struct{}{})
		require.NoError(t, err)
		assert.Equal(t, int64(4), got.(*messaging.Stats).TotalChannels)
	})

	t.Run("ObjectStorage", func(t *testing.T) {
		got, err := trans.Request(ctx, "quicbaas.ObjectStorage", "PutObject",
			&objectstorage.PutObjectRequest{Key: "ns:doc", Value: []byte("data")})
		require.NoError(t, err)
		resp := got.(*objectstorage.PutObjectResponse)
		assert.Equal(t, "ns:doc", resp.Key)
		assert.Equal(t, uint64(7), resp.ObjectID)
	})
}

// TestDoRequest_RemainingTransformErrors covers the request-transform error
// branches for TimeSeries and Messaging (bad request types) reached through
// doRequest before any network I/O.
func TestDoRequest_RemainingTransformErrors(t *testing.T) {
	server, err := newMockQUICServer(t)
	require.NoError(t, err)
	trans := newDispatchTestTransport(t, server)
	ctx := context.Background()

	t.Run("timeseries transform rejects bad type", func(t *testing.T) {
		_, err := trans.Request(ctx, "quicbaas.TimeSeries", "Write", "not-a-request")
		require.Error(t, err)
	})

	t.Run("messaging transform rejects bad type", func(t *testing.T) {
		_, err := trans.Request(ctx, "quicbaas.Messaging", "CreateChannel", "not-a-request")
		require.Error(t, err)
	})

	t.Run("certificate unknown method", func(t *testing.T) {
		// CAInfo has no QUIC opcode; must fail at opcode resolution.
		_, err := trans.Request(ctx, "quicbaas.Certificate", "CAInfo", &certificate.CAInfoRequest{})
		require.Error(t, err)
	})
}
