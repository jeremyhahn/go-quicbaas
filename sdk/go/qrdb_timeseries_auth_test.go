// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package quicbaas

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type qtsRecordedCall struct {
	method string
	auth   []string
}

// qtsRecorder is a gRPC server that answers every method: health probes
// with PermissionDenied (go-qrdb's Connect treats that as reachable) and
// everything else with Unimplemented, recording each non-health call with
// its incoming authorization metadata.
type qtsRecorder struct {
	mu    sync.Mutex
	calls []qtsRecordedCall
}

func (r *qtsRecorder) handle(_ interface{}, stream grpc.ServerStream) error {
	method, _ := grpc.MethodFromServerStream(stream)
	if strings.Contains(method, "Health") {
		return status.Error(codes.PermissionDenied, "health not exported")
	}
	md, _ := metadata.FromIncomingContext(stream.Context())
	r.mu.Lock()
	r.calls = append(r.calls, qtsRecordedCall{method: method, auth: md.Get("authorization")})
	r.mu.Unlock()
	return status.Error(codes.Unimplemented, "recorded")
}

func (r *qtsRecorder) snapshot() []qtsRecordedCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]qtsRecordedCall(nil), r.calls...)
}

func startQTSRecorder(t *testing.T) (string, *qtsRecorder) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	rec := &qtsRecorder{}
	srv := grpc.NewServer(grpc.UnknownServiceHandler(rec.handle))
	served := make(chan error, 1)
	go func() { served <- srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		<-served
	})
	return lis.Addr().String(), rec
}

// newQTSGRPCClient builds a gRPC SDK client for address the way the other
// qrdb sub-client tests do (no quicbaas transport; the qrdb sub-client dials
// address in plaintext).
func newQTSGRPCClient(t *testing.T, address string, options ...Option) *client {
	t.Helper()
	opts := defaultOptions()
	for _, apply := range options {
		if err := apply(opts); err != nil {
			t.Fatalf("option: %v", err)
		}
	}
	c := &client{opts: opts, address: address}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestSearchQRDBTimeSeries_GRPC_SendsPlatformBearer(t *testing.T) {
	t.Run("with token", func(t *testing.T) {
		addr, rec := startQTSRecorder(t)
		c := newQTSGRPCClient(t, addr, WithAuthToken("t-2500"))
		ts, err := c.Search().QRDBTimeSeries()
		if err != nil {
			t.Fatalf("QRDBTimeSeries: %v", err)
		}
		if _, err := ts.TSQuery(context.Background(), &qrdbtransport.TSQueryRequest{Metric: "cpu", StartNs: 1, EndNs: 2}); err == nil {
			t.Fatal("TSQuery against the recorder succeeded, want Unimplemented")
		}
		calls := rec.snapshot()
		if len(calls) == 0 {
			t.Fatal("no time-series RPC reached the server")
		}
		for _, call := range calls {
			if len(call.auth) != 1 || call.auth[0] != "Bearer t-2500" {
				t.Errorf("%s authorization = %v, want [Bearer t-2500]", call.method, call.auth)
			}
		}
	})

	t.Run("without token", func(t *testing.T) {
		addr, rec := startQTSRecorder(t)
		c := newQTSGRPCClient(t, addr)
		ts, err := c.Search().QRDBTimeSeries()
		if err != nil {
			t.Fatalf("QRDBTimeSeries: %v", err)
		}
		_, _ = ts.TSQuery(context.Background(), &qrdbtransport.TSQueryRequest{Metric: "cpu", StartNs: 1, EndNs: 2})
		calls := rec.snapshot()
		if len(calls) == 0 {
			t.Fatal("no time-series RPC reached the server")
		}
		for _, call := range calls {
			if len(call.auth) != 0 {
				t.Errorf("%s authorization = %v, want none", call.method, call.auth)
			}
		}
	})
}

func TestSearchQRDBTimeSeries_GRPC_CloseClosesSubClient(t *testing.T) {
	t.Run("initialized sub-client", func(t *testing.T) {
		addr, rec := startQTSRecorder(t)
		c := newQTSGRPCClient(t, addr, WithAuthToken("t-2500"))
		ts, err := c.Search().QRDBTimeSeries()
		if err != nil {
			t.Fatalf("QRDBTimeSeries: %v", err)
		}
		if err := c.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		before := len(rec.snapshot())
		_, err = ts.TSQuery(context.Background(), &qrdbtransport.TSQueryRequest{Metric: "cpu", StartNs: 1, EndNs: 2})
		if err == nil {
			t.Fatal("TSQuery after Close succeeded")
		}
		if msg := strings.ToLower(err.Error()); !strings.Contains(msg, "close") && !strings.Contains(msg, "connection") {
			t.Errorf("TSQuery after Close error = %v, want a closed or connection error", err)
		}
		if after := len(rec.snapshot()); after != before {
			t.Errorf("server calls after Close = %d, want %d", after, before)
		}
	})

	t.Run("never initialized", func(t *testing.T) {
		addr, rec := startQTSRecorder(t)
		c := newQTSGRPCClient(t, addr)
		if err := c.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if qc, err := c.qrdbClient(); err == nil || qc != nil {
			t.Fatalf("qrdbClient after Close = (%v, %v), want an error", qc, err)
		}
		if _, err := c.Search().QRDBTimeSeries(); err == nil {
			t.Error("QRDBTimeSeries after Close succeeded")
		}
		if n := len(rec.snapshot()); n != 0 {
			t.Errorf("server calls = %d, want 0", n)
		}
	})
}
