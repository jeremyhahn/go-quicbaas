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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	unixtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport/unix"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestServer_QRDBServiceOnUnixSocket_ServesNodeData proves the qrdb.v1
// service registered on the Unix socket's gRPC server serves the embedded
// node's data services: a go-qrdb client over the socket writes and reads a
// key instead of getting Unimplemented.
func TestServer_QRDBServiceOnUnixSocket_ServesNodeData(t *testing.T) {
	ctx, cl := qrdbUnixDataClient(t, true)

	require.NoError(t, cl.KV().Put(ctx, "row1652/unix", []byte("v")))
	got, err := cl.KV().Get(ctx, "row1652/unix")
	require.NoError(t, err)
	require.Equal(t, "v", string(got))
}

// TestServer_QRDBServiceOnUnixSocket_HonoursDatabaseScope proves the Unix
// chain carries the client's x-qrdb-database selection to the data
// services: a client scoped to another database must not read the
// system database.
func TestServer_QRDBServiceOnUnixSocket_HonoursDatabaseScope(t *testing.T) {
	ctx, cl := qrdbUnixDataClient(t, true)

	require.NoError(t, cl.KV().Put(ctx, "row1652/scope", []byte("v")))
	scoped, err := cl.DatabaseScoped("nope1652")
	require.NoError(t, err)
	got, err := scoped.KV().Get(ctx, "row1652/scope")
	require.Error(t, err, "a client scoped to a missing database read %q from the system database", got)
}

// qrdbUnixDataClient serves the node's qrdb service on a Unix socket
// through the server's own gRPC interceptor chain and returns a qrdb SDK
// client connected to it.
func qrdbUnixDataClient(t *testing.T, bindTenant bool) (context.Context, *qrdbsdk.Client) {
	t.Helper()
	srv := withTempServer(t)
	require.NoError(t, srv.initStandaloneQRDBNode(), "initStandaloneQRDBNode")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	srv.dataPlaneBarrierPending.Store(false)

	dir, err := os.MkdirTemp("", "qb1652")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "q.sock")
	lis, err := net.Listen("unix", sock)
	require.NoError(t, err)
	u, st, err := srv.grpcServerInterceptors()
	require.NoError(t, err)
	if bindTenant {
		u = append([]ggrpc.UnaryServerInterceptor{
			func(ctx context.Context, req interface{}, _ *ggrpc.UnaryServerInfo, handler ggrpc.UnaryHandler) (interface{}, error) {
				return handler(tenantctx.WithSystem(ctx), req)
			},
		}, u...)
		st = append([]ggrpc.StreamServerInterceptor{
			func(srv interface{}, ss ggrpc.ServerStream, _ *ggrpc.StreamServerInfo, handler ggrpc.StreamHandler) error {
				return handler(srv, &qrdbTenantBoundStream{ServerStream: ss, ctx: tenantctx.WithSystem(ss.Context())})
			},
		}, st...)
	}
	gs := ggrpc.NewServer(ggrpc.ChainUnaryInterceptor(u...), ggrpc.ChainStreamInterceptor(st...))
	srv.registerQRDBService(gs)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	tr, err := unixtransport.New(qrdbtransport.WithAddress(sock))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	if err := tr.Connect(ctx); err != nil && !strings.Contains(err.Error(), "health check failed") {
		t.Fatal(err)
	}
	cl, err := qrdbsdk.NewClient(tr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	return ctx, cl
}

// qrdbTenantBoundStream carries a context with a bound tenant into a stream
// handler.
type qrdbTenantBoundStream struct {
	ggrpc.ServerStream
	ctx context.Context
}

// Context returns the tenant-bound context.
func (s *qrdbTenantBoundStream) Context() context.Context { return s.ctx }

// TestServer_QRDBServiceOnUnixSocket_RefusesUnboundTenant pins that a
// certless caller with no tenant bound is refused before qrdb's handlers run:
// they would read the absent tenant as the system tenant.
func TestServer_QRDBServiceOnUnixSocket_RefusesUnboundTenant(t *testing.T) {
	ctx, cl := qrdbUnixDataClient(t, false)
	err := cl.KV().Put(ctx, "row1652/unbound", []byte("v"))
	require.Error(t, err)
	require.Equal(t, codes.PermissionDenied, status.Code(err), "%v", err)
}

// TestQRDBDataRegistryProxy_RefusesOutOfRowServices pins that the registry
// proxy refuses the watch, lease and lock services: they are outside the
// data-services surface, so the Unix socket never serves them, whether or not
// authorization is on.
func TestQRDBDataRegistryProxy_RefusesOutOfRowServices(t *testing.T) {
	p := newQRDBDataRegistryProxy(&Server{})
	var unsupported *qrdbservice.CapabilityUnsupportedError

	watch, err := p.WatchService("system", "system")
	require.Nil(t, watch)
	require.True(t, errors.As(err, &unsupported), "watch: %v", err)

	lease, err := p.LeaseService("system", "system")
	require.Nil(t, lease)
	require.True(t, errors.As(err, &unsupported), "lease: %v", err)

	lock, err := p.LockService("system", "system")
	require.Nil(t, lock)
	require.True(t, errors.As(err, &unsupported), "lock: %v", err)
}
