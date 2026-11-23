// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed under AGPL-3.0 and a Commercial License.
// See LICENSE file for details.

package grpc

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	qrdbgrpc "github.com/jeremyhahn/go-qrdb/pkg/api/grpc"
	qrdbv1 "github.com/jeremyhahn/go-qrdb/pkg/api/grpc/proto/qrdbv1"
	qrdbtimeseries "github.com/jeremyhahn/go-qrdb/pkg/timeseries"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// tsRecorder is the go-qrdb time-series servicer behind the qrdb.v1 TS RPCs.
// Only the methods the test drives are overridden; the embedded nil
// interface panics on anything else, which would fail the test loudly.
type tsRecorder struct {
	qrdbtimeseries.TimeSeriesServicer

	mu    sync.Mutex
	calls []string
}

func (r *tsRecorder) record(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name)
}

func (r *tsRecorder) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func (r *tsRecorder) Write(_ context.Context, metric string, _ float64, _ time.Time, _ map[string]string) error {
	r.record("Write:" + metric)
	return nil
}

func (r *tsRecorder) SetRetentionPolicy(_ context.Context, metric string, _ *qrdbtimeseries.RetentionPolicy) error {
	r.record("SetRetentionPolicy:" + metric)
	return nil
}

func (r *tsRecorder) GetRetentionPolicy(metric string) (*qrdbtimeseries.RetentionPolicy, error) {
	r.record("GetRetentionPolicy:" + metric)
	return &qrdbtimeseries.RetentionPolicy{Duration: time.Hour}, nil
}

// startQRDBTimeSeriesUnixServer serves the qrdb.v1 service, with the
// time-series servicer attached, behind the broker's authorization
// interceptor on a Unix socket, the transport the broker exposes it on.
func startQRDBTimeSeriesUnixServer(t *testing.T, authorizer *fakeValidatorAuthorizer) (*tsRecorder, qrdbv1.QRDBServiceClient) {
	t.Helper()
	recorder := &tsRecorder{}
	gs := grpc.NewServer(grpc.ChainUnaryInterceptor(UnaryAuthorizationInterceptor(authorizer)))
	qrdbgrpc.RegisterService(gs, nil, nil, qrdbgrpc.WithRegisterTimeSeries(recorder))

	// A short directory keeps the socket path inside the sun_path limit.
	dir, err := os.MkdirTemp("", "qbts")
	require.NoError(t, err)
	socket := filepath.Join(dir, "s.sock")
	lis, err := net.Listen("unix", socket)
	require.NoError(t, err)
	go func() { _ = gs.Serve(lis) }()

	conn, err := grpc.NewClient("unix://"+socket, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = conn.Close()
		gs.Stop()
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove socket dir: %v", err)
		}
	})
	return recorder, qrdbv1.NewQRDBServiceClient(conn)
}

// TestQRDBTimeSeries_UnixAuthorized drives the qrdb.v1 TS RPCs over the
// broker's Unix transport with RequireAuthz semantics: a token granted
// timeseries:write writes, and the same token cannot read a retention
// policy, which needs timeseries:get_retention.
func TestQRDBTimeSeries_UnixAuthorized(t *testing.T) {
	authorizer := &fakeValidatorAuthorizer{
		token:       "good",
		grantedPerm: "timeseries:write",
		authzCtx: &platformtoken.AuthorizedContext{
			Subject:  "u1",
			Username: "writer",
			TenantID: "system",
			Scopes:   []string{"timeseries:write"},
		},
	}
	recorder, client := startQRDBTimeSeriesUnixServer(t, authorizer)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer good")

	_, err := client.TSWrite(ctx, &qrdbv1.TSWriteRequest{Point: &qrdbv1.TSDataPoint{
		Metric:      "cpu",
		Value:       1.5,
		TimestampNs: time.Now().UnixNano(),
	}})
	require.NoError(t, err, "TSWrite with timeseries:write must be admitted")
	assert.Equal(t, "timeseries:write", authorizer.lastPerm)
	assert.Equal(t, []string{"Write:cpu"}, recorder.seen())

	_, err = client.TSGetRetention(ctx, &qrdbv1.TSGetRetentionRequest{Metric: "cpu"})
	require.Error(t, err, "TSGetRetention needs timeseries:get_retention")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Equal(t, "timeseries:get_retention", authorizer.lastPerm)
	assert.Equal(t, []string{"Write:cpu"}, recorder.seen(), "a denied RPC must not reach the servicer")
}
