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
	"path/filepath"
	"strings"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbembedded "github.com/jeremyhahn/go-qrdb/sdk/go/transport/embedded"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// adminBearerRecorder records the authorization metadata of every Admin RPC
// the cluster and membership accessors reach, keyed by method name. Health
// is left out: the main qrdb client's Connect calls it, without a bearer.
type adminBearerRecorder struct {
	mu  sync.Mutex
	got map[string][][]string
}

func (r *adminBearerRecorder) intercept(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	method := info.FullMethod[strings.LastIndex(info.FullMethod, "/")+1:]
	switch method {
	case "AddMember", "RemoveMember", "Peers", "GetShardState":
		md, _ := metadata.FromIncomingContext(ctx)
		r.mu.Lock()
		r.got[method] = append(r.got[method], md.Get("authorization"))
		r.mu.Unlock()
	}
	return handler(ctx, req)
}

func (r *adminBearerRecorder) calls() map[string][][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string][][]string, len(r.got))
	for k, v := range r.got {
		out[k] = append([][]string(nil), v...)
	}
	return out
}

// serveQRDBAdminGRPC serves go-qrdb's gRPC service with KV and Admin
// servicers on lis and records the bearer each Admin call carries.
func serveQRDBAdminGRPC(t *testing.T, lis net.Listener) *adminBearerRecorder {
	t.Helper()
	rec := &adminBearerRecorder{got: map[string][][]string{}}
	srv := grpc.NewServer(grpc.UnaryInterceptor(rec.intercept))
	qrdbsdk.RegisterGRPCService(srv, stubKVService{}, &recordingAdminService{})
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

// bearerClusterListeners are the tcp and unix listeners the gRPC bearer
// tests run over, with the options that select each transport.
var bearerClusterListeners = []struct {
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

// bearerClusterClient builds a quicbaas client with the bearer and the
// listener's transport, and closes its qrdb sub-client at cleanup.
func bearerClusterClient(t *testing.T, lis net.Listener, extra []Option) *client {
	t.Helper()
	opts := defaultOptions()
	for _, apply := range append([]Option{WithAuthToken("t-1649")}, extra...) {
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
	return c
}

// Over gRPC and Unix, QRDBCluster carries the WithAuthToken bearer to every
// Admin RPC go-qrdb's ClusterClient makes.
func TestQRDBCluster_GRPCAndUnix_SendPlatformBearer(t *testing.T) {
	for _, tc := range bearerClusterListeners {
		t.Run(tc.name, func(t *testing.T) {
			lis, extra := tc.listen(t)
			rec := serveQRDBAdminGRPC(t, lis)
			c := bearerClusterClient(t, lis, extra)
			ctx := context.Background()

			cc, err := c.QRDBCluster()
			require.NoError(t, err)
			require.NotNil(t, cc)

			members, err := cc.Members(ctx, 7)
			require.NoError(t, err)
			assert.Len(t, members, 2)
			require.NoError(t, cc.AddReplica(ctx, 7, 3, "raft-3:7000", "node-3"))

			calls := rec.calls()
			assert.NotEmpty(t, calls["Peers"], "Members reached no Peers RPC")
			assert.NotEmpty(t, calls["AddMember"], "AddReplica reached no AddMember RPC")
			for method, got := range calls {
				for _, values := range got {
					assert.Equal(t, []string{"Bearer t-1649"}, values, method)
				}
			}
		})
	}
}

// Over gRPC, a membership reconfigurer built from QRDBMembership carries the
// bearer to the Admin RPC it makes.
func TestQRDBMembership_GRPC_SendsPlatformBearer(t *testing.T) {
	lis, extra := bearerClusterListeners[0].listen(t)
	rec := serveQRDBAdminGRPC(t, lis)
	c := bearerClusterClient(t, lis, extra)

	mc, err := c.QRDBMembership()
	require.NoError(t, err)
	require.NotNil(t, mc)
	require.NoError(t, mc.NewMembershipReconfigurer().AddMember(context.Background(), 7, 4, "raft-4:7000"))

	calls := rec.calls()
	require.Len(t, calls["AddMember"], 1)
	assert.Equal(t, []string{"Bearer t-1649"}, calls["AddMember"][0])
}

// A token set on a transport whose qrdb sub-client cannot carry it (REST,
// QUIC, MCP) is refused by every cluster accessor, never silently dropped.
// Without a token, the same transports serve the accessors.
func TestQRDBClusterAccessors_TokenOverRESTQUICMCPIsRefused(t *testing.T) {
	base, err := qrdbembedded.New(stubKVService{}, stubAdminService{})
	require.NoError(t, err)
	qc, err := qrdbsdk.NewClient(base)
	require.NoError(t, err)

	newClient := func(tt TransportType, token string) *client {
		opts := defaultOptions()
		if token != "" {
			require.NoError(t, WithAuthToken(token)(opts))
		}
		opts.transportType = tt
		c := &client{opts: opts, address: "127.0.0.1:1"}
		c.qrdb.once.Do(func() { c.qrdb.client = qc })
		return c
	}

	for _, tt := range []TransportType{TransportREST, TransportQUIC, TransportMCP} {
		c := newClient(tt, "t-1649")

		cc, err := c.QRDBCluster()
		assert.True(t, sdkerrors.IsInvalidInput(err), "%v: QRDBCluster err = %v, want InvalidInput", tt, err)
		assert.Nil(t, cc, "%v: QRDBCluster client", tt)

		mc, err := c.QRDBMembership()
		assert.True(t, sdkerrors.IsInvalidInput(err), "%v: QRDBMembership err = %v, want InvalidInput", tt, err)
		assert.Nil(t, mc, "%v: QRDBMembership client", tt)

		rc, err := c.QRDBRebalance()
		assert.True(t, sdkerrors.IsInvalidInput(err), "%v: QRDBRebalance err = %v, want InvalidInput", tt, err)
		assert.Nil(t, rc, "%v: QRDBRebalance client", tt)

		c = newClient(tt, "")
		cc, err = c.QRDBCluster()
		assert.NoError(t, err, "%v without a token: QRDBCluster", tt)
		assert.NotNil(t, cc, "%v without a token: QRDBCluster client", tt)
		mc, err = c.QRDBMembership()
		assert.NoError(t, err, "%v without a token: QRDBMembership", tt)
		assert.NotNil(t, mc, "%v without a token: QRDBMembership client", tt)
		rc, err = c.QRDBRebalance()
		assert.NoError(t, err, "%v without a token: QRDBRebalance", tt)
		assert.NotNil(t, rc, "%v without a token: QRDBRebalance client", tt)
	}
}

// closeCountingTransport is an inner transport that counts Close calls.
type closeCountingTransport struct {
	closes int
}

func (f *closeCountingTransport) Connect(context.Context) error                   { return nil }
func (f *closeCountingTransport) Close() error                                    { f.closes++; return nil }
func (f *closeCountingTransport) Healthy(context.Context) bool                    { return true }
func (f *closeCountingTransport) Request(context.Context, string, any, any) error { return nil }

// The bearer decorator never closes the transport it wraps: the main qrdb
// client owns it, and that client's Close still closes it.
func TestQRDBBearerTransport_CloseLeavesInnerOpen(t *testing.T) {
	inner := &closeCountingTransport{}

	bearerClient, err := qrdbsdk.NewClient(&qrdbBearerTransport{inner: inner, bearer: "Bearer x"})
	require.NoError(t, err)
	require.NoError(t, bearerClient.Close())
	assert.Equal(t, 0, inner.closes, "the bearer client closed the inner transport")

	mainClient, err := qrdbsdk.NewClient(inner)
	require.NoError(t, err)
	require.NoError(t, mainClient.Close())
	assert.Equal(t, 1, inner.closes, "the main client did not close the inner transport")
}
