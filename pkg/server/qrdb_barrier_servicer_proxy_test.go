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

package server

import (
	"context"
	"errors"
	"net"
	"testing"

	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	qrdbgrpc "github.com/jeremyhahn/go-qrdb/pkg/api/grpc"
	"github.com/jeremyhahn/go-qrdb/pkg/api/grpc/proto/qrdbv1"
	"github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// statusOnlyBarrier is a BarrierServicer double that answers BarrierStatus;
// every other method is the embedded nil interface and is never called.
type statusOnlyBarrier struct {
	qrdbsdk.BarrierServicer
	calls int
}

func (b *statusOnlyBarrier) BarrierStatus(context.Context) (*qrdbsdk.BarrierStatusInfo, error) {
	b.calls++
	return &qrdbsdk.BarrierStatusInfo{Sealed: true, Strategy: "proxy-test"}, nil
}

// TestQRDBBarrierServicerProxy_ServesBarrierWiredAfterRegistration is the
// #2488 regression: the node's gRPC server registers the barrier before any
// barrier servicer is wired. The proxy built then must serve the barrier once
// it is wired, because it resolves the servicer per call.
func TestQRDBBarrierServicerProxy_ServesBarrierWiredAfterRegistration(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	if s.qrdbBarrierServicer() != nil {
		t.Fatal("precondition: no barrier servicer is wired at registration time")
	}
	proxy := newQRDBBarrierServicerProxy(s)

	backing := &statusOnlyBarrier{}
	s.sealedWindowClient = &qrdbsdk.Client{}
	s.ceremonyServices = &CeremonyServices{Barrier: backing}

	info, err := proxy.BarrierStatus(context.Background())
	if err != nil {
		t.Fatalf("BarrierStatus after wiring: %v", err)
	}
	if backing.calls != 1 {
		t.Fatalf("BarrierStatus must delegate to the wired servicer once, got %d calls", backing.calls)
	}
	if info == nil || !info.Sealed || info.Strategy != "proxy-test" {
		t.Fatalf("BarrierStatus must return the wired servicer's answer, got %+v", info)
	}
}

// TestQRDBBarrierServicerProxy_NotConfiguredWhenNothingWired pins the answer
// of a node with no barrier servicer: the typed
// BarrierManagerNotConfiguredError, which go-qrdb's transports classify as
// Unimplemented (a raw gRPC status would be reclassified as Internal).
func TestQRDBBarrierServicerProxy_NotConfiguredWhenNothingWired(t *testing.T) {
	proxy := newQRDBBarrierServicerProxy(&Server{logger: buildTestLogger(t)})
	ctx := context.Background()

	_, statusErr := proxy.BarrierStatus(ctx)
	_, hasErr := proxy.BarrierHasRecoveryKeys(ctx)
	errs := map[string]error{
		"BarrierStatus":          statusErr,
		"BarrierSeal":            proxy.BarrierSeal(ctx),
		"BarrierUnseal":          proxy.BarrierUnseal(ctx, []byte("x")),
		"BarrierHasRecoveryKeys": hasErr,
	}
	for name, err := range errs {
		if !errors.As(err, new(*service.BarrierManagerNotConfiguredError)) {
			t.Fatalf("%s: expected BarrierManagerNotConfiguredError, got %v", name, err)
		}
	}

	if _, err := newQRDBBarrierServicerProxy(nil).BarrierStatus(ctx); !errors.As(err, new(*service.BarrierManagerNotConfiguredError)) {
		t.Fatalf("a proxy without a server must answer BarrierManagerNotConfiguredError, got %v", err)
	}
}

// serveQRDBBarrierOverTCP serves the qrdb.v1 service on a real loopback TCP
// listener with the proxy as its barrier service -- the registration
// qrdbserver.WithBarrierService gives the node's TCP gRPC server -- and
// returns a qrdb.v1 client dialled over TCP.
func serveQRDBBarrierOverTCP(t *testing.T, s *Server) qrdbv1.QRDBServiceClient {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	node := ggrpc.NewServer()
	qrdbgrpc.RegisterService(node, nil, nil, qrdbgrpc.WithRegisterBarrierService(newQRDBBarrierServicerProxy(s)))
	served := make(chan error, 1)
	go func() { served <- node.Serve(listener) }()
	t.Cleanup(func() {
		node.Stop()
		if serveErr := <-served; serveErr != nil {
			t.Errorf("serve: %v", serveErr)
		}
	})
	conn, err := ggrpc.NewClient(listener.Addr().String(),
		ggrpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return qrdbv1.NewQRDBServiceClient(conn)
}

// TestQRDBBarrierStatusOverTCP_ServesWiredBarrier: qrdb.v1 BarrierStatus over
// TCP reaches a barrier servicer wired after registration, instead of the
// "unknown service"/"method not found" Unimplemented of an unregistered one.
func TestQRDBBarrierStatusOverTCP_ServesWiredBarrier(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	client := serveQRDBBarrierOverTCP(t, s)

	backing := &statusOnlyBarrier{}
	s.sealedWindowClient = &qrdbsdk.Client{}
	s.ceremonyServices = &CeremonyServices{Barrier: backing}

	resp, err := client.BarrierStatus(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("BarrierStatus over TCP with a wired barrier: %v (code %s)", err, status.Code(err))
	}
	if backing.calls != 1 || !resp.GetSealed() || resp.GetStrategy() != "proxy-test" {
		t.Fatalf("BarrierStatus over TCP must return the wired servicer's answer, got %+v after %d calls", resp, backing.calls)
	}
}

// TestQRDBBarrierStatusOverTCP_UnimplementedWhenNothingWired: with no barrier
// servicer wired, qrdb.v1 BarrierStatus over TCP answers Unimplemented, not
// Internal.
func TestQRDBBarrierStatusOverTCP_UnimplementedWhenNothingWired(t *testing.T) {
	client := serveQRDBBarrierOverTCP(t, &Server{logger: buildTestLogger(t)})

	_, err := client.BarrierStatus(context.Background(), &emptypb.Empty{})
	if got := status.Code(err); got != codes.Unimplemented {
		t.Fatalf("BarrierStatus over TCP with nothing wired: expected codes.Unimplemented, got %s (%v)", got, err)
	}
}
