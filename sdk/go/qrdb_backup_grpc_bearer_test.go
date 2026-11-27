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
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// backupBearerRecorder records the authorization metadata of every
// BackupList RPC the server receives.
type backupBearerRecorder struct {
	mu    sync.Mutex
	calls int
	got   []string
}

func (r *backupBearerRecorder) intercept(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if strings.HasSuffix(info.FullMethod, "/BackupList") {
		md, _ := metadata.FromIncomingContext(ctx)
		r.mu.Lock()
		r.calls++
		r.got = append(r.got, md.Get("authorization")...)
		r.mu.Unlock()
	}
	return handler(ctx, req)
}

func (r *backupBearerRecorder) values() (int, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls, append([]string(nil), r.got...)
}

// serveQRDBBackupGRPC serves go-qrdb's gRPC service with svc as its backup
// service on lis and records the bearer each BackupList call carries.
func serveQRDBBackupGRPC(t *testing.T, lis net.Listener, svc *stubBackupService) *backupBearerRecorder {
	t.Helper()
	rec := &backupBearerRecorder{}
	srv := grpc.NewServer(grpc.UnaryInterceptor(rec.intercept))
	qrdbsdk.RegisterGRPCService(srv, nil, nil, qrdbapigrpc.WithRegisterBackupService(svc))
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

// newNetworkBackupClient builds a client over the given options against
// addr and closes its qrdb client when the test ends.
func newNetworkBackupClient(t *testing.T, addr string, options ...Option) *client {
	t.Helper()
	opts := defaultOptions()
	for _, apply := range options {
		require.NoError(t, apply(opts))
	}
	c := &client{opts: opts, address: addr}
	t.Cleanup(func() {
		if qc, err := c.qrdbClient(); err == nil {
			if err := qc.Close(); err != nil {
				t.Errorf("close qrdb client: %v", err)
			}
		}
	})
	return c
}

// Over gRPC and Unix, QRDBBackup() carries the WithAuthToken bearer to
// go-qrdb's backup RPCs, and refuses CreateBackup because the node would keep
// the backup in memory only.
func TestStorageQRDBBackup_GRPCAndUnix_SendPlatformBearer(t *testing.T) {
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
			svc := &stubBackupService{}
			rec := serveQRDBBackupGRPC(t, lis, svc)
			c := newNetworkBackupClient(t, lis.Addr().String(), append([]Option{WithAuthToken("t-2509")}, extra...)...)

			bc, err := c.QRDBBackup()
			require.NoError(t, err)
			ctx := context.Background()

			list, err := bc.ListBackups(ctx)
			require.NoError(t, err)
			require.Len(t, list, 1)
			assert.Equal(t, "bk-1", list[0].ID)
			_, got := rec.values()
			assert.Equal(t, []string{"Bearer t-2509"}, got)

			_, err = bc.CreateBackup(ctx, &QRDBBackupRequest{Destination: "/b"})
			require.Error(t, err)
			assert.True(t, sdkerrors.IsUnimplemented(err), "got %v", err)
			assert.Nil(t, svc.created)
		})
	}
}

// Without a token the gRPC backup client sends no authorization, and
// CreateBackup is still refused as not durable.
func TestStorageQRDBBackup_NetworkCreateRefusedWithoutToken(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	svc := &stubBackupService{}
	rec := serveQRDBBackupGRPC(t, lis, svc)
	c := newNetworkBackupClient(t, lis.Addr().String())

	bc, err := c.QRDBBackup()
	require.NoError(t, err)
	ctx := context.Background()

	_, err = bc.ListBackups(ctx)
	require.NoError(t, err)
	calls, got := rec.values()
	assert.Equal(t, 1, calls)
	assert.Empty(t, got)

	_, err = bc.CreateBackup(ctx, &QRDBBackupRequest{Destination: "/b"})
	require.Error(t, err)
	assert.True(t, sdkerrors.IsUnimplemented(err), "got %v", err)
	assert.Nil(t, svc.created)
}

// Over REST, QUIC and MCP go-qrdb's backup client sends no credentials, so a
// client configured with credentials gets Unimplemented, not a client that
// would fail authentication.
func TestStorageQRDBBackup_RESTQUICMCPWithCredentialsIsUnimplemented(t *testing.T) {
	cases := []struct {
		name string
		opts []Option
	}{
		{"rest token", []Option{WithREST(), WithAuthToken("t")}},
		{"quic token", []Option{WithQUIC(), WithAuthToken("t")}},
		{"mcp token", []Option{WithMCP(), WithMCPAuthToken("t")}},
		{"mcp credentials", []Option{WithMCP(), WithMCPCredentials("u", "p")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := defaultOptions()
			for _, apply := range tc.opts {
				require.NoError(t, apply(opts))
			}
			c := &client{opts: opts, address: "127.0.0.1:1"}
			bc, err := c.QRDBBackup()
			require.Error(t, err)
			assert.Nil(t, bc)
			assert.True(t, sdkerrors.IsUnimplemented(err), "got %v", err)
		})
	}
}

// fakeBearerBackupClient records the outgoing authorization metadata of each
// verb.
type fakeBearerBackupClient struct {
	seen map[string][]string
}

func (f *fakeBearerBackupClient) record(ctx context.Context, verb string) {
	md, _ := metadata.FromOutgoingContext(ctx)
	f.seen[verb] = md.Get("authorization")
}

func (f *fakeBearerBackupClient) CreateBackup(ctx context.Context, _ *QRDBBackupRequest) (*QRDBBackupInfo, error) {
	f.record(ctx, "CreateBackup")
	return &QRDBBackupInfo{ID: "created"}, nil
}

func (f *fakeBearerBackupClient) ListBackups(ctx context.Context) ([]QRDBBackupInfo, error) {
	f.record(ctx, "ListBackups")
	return []QRDBBackupInfo{{ID: "listed"}}, nil
}

func (f *fakeBearerBackupClient) GetBackup(ctx context.Context, id string) (*QRDBBackupInfo, error) {
	f.record(ctx, "GetBackup")
	return &QRDBBackupInfo{ID: id}, nil
}

func (f *fakeBearerBackupClient) DeleteBackup(ctx context.Context, _ string) error {
	f.record(ctx, "DeleteBackup")
	return nil
}

func (f *fakeBearerBackupClient) RestoreBackup(ctx context.Context, req *qrdbtransport.RestoreRequest) (*qrdbtransport.RestoreResult, error) {
	f.record(ctx, "RestoreBackup")
	return &qrdbtransport.RestoreResult{BackupID: req.BackupID}, nil
}

func (f *fakeBearerBackupClient) VerifyBackup(ctx context.Context, _ string) (*qrdbtransport.VerifyResult, error) {
	f.record(ctx, "VerifyBackup")
	return &qrdbtransport.VerifyResult{Valid: true}, nil
}

// The bearer wrapper adds the bearer to every verb and returns the inner
// client's results unchanged.
func TestQRDBBearerBackupClient_ForwardsEveryVerb(t *testing.T) {
	fake := &fakeBearerBackupClient{seen: map[string][]string{}}
	b := &qrdbBearerBackupClient{inner: fake, bearer: "Bearer x"}
	ctx := context.Background()

	ci, err := b.CreateBackup(ctx, &QRDBBackupRequest{})
	require.NoError(t, err)
	assert.Equal(t, "created", ci.ID)

	list, err := b.ListBackups(ctx)
	require.NoError(t, err)
	assert.Equal(t, "listed", list[0].ID)

	gi, err := b.GetBackup(ctx, "g")
	require.NoError(t, err)
	assert.Equal(t, "g", gi.ID)

	require.NoError(t, b.DeleteBackup(ctx, "d"))

	rr, err := b.RestoreBackup(ctx, &qrdbtransport.RestoreRequest{BackupID: "r"})
	require.NoError(t, err)
	assert.Equal(t, "r", rr.BackupID)

	vr, err := b.VerifyBackup(ctx, "v")
	require.NoError(t, err)
	assert.True(t, vr.Valid)

	for _, verb := range []string{"CreateBackup", "ListBackups", "GetBackup", "DeleteBackup", "RestoreBackup", "VerifyBackup"} {
		assert.Equal(t, []string{"Bearer x"}, fake.seen[verb], verb)
	}
}
