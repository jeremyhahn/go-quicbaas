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

package server

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	qrdbgrpc "github.com/jeremyhahn/go-qrdb/pkg/api/grpc"
	"github.com/jeremyhahn/go-qrdb/pkg/api/grpc/proto/qrdbv1"
	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
)

// callAllBackupMethods calls each BackupServicer method once and returns the
// errors in method order.
func callAllBackupMethods(p qrdbservice.BackupServicer) []error {
	ctx := context.Background()
	_, e1 := p.CreateBackup(ctx, qrdbservice.BackupOptions{})
	_, e2 := p.ListBackups(ctx)
	_, e3 := p.GetBackup(ctx, "b")
	e4 := p.DeleteBackup(ctx, "b")
	_, e5 := p.RestoreBackup(ctx, qrdbservice.RestoreOptions{BackupID: "b"})
	_, e6 := p.VerifyBackup(ctx, "b")
	return []error{e1, e2, e3, e4, e5, e6}
}

func TestQRDBBackupServicerProxy_NotConfiguredWithoutNode(t *testing.T) {
	for name, p := range map[string]*qrdbBackupServicerProxy{
		"server without node": newQRDBBackupServicerProxy(&Server{logger: buildTestLogger(t)}),
		"nil server":          newQRDBBackupServicerProxy(nil),
	} {
		t.Run(name, func(t *testing.T) {
			for i, err := range callAllBackupMethods(p) {
				require.Error(t, err, "method %d", i)
				assert.True(t, errors.As(err, new(*qrdbgrpc.ServiceNotConfiguredError)),
					"method %d: want ServiceNotConfiguredError, got %T %v", i, err, err)
			}
		})
	}
}

type stubBackupServicer struct {
	calls   []string
	opts    qrdbservice.BackupOptions
	ids     []string
	restore qrdbservice.RestoreOptions

	info     *qrdbservice.BackupInfo
	list     []qrdbservice.BackupInfo
	result   *qrdbservice.RestoreResult
	verify   *qrdbservice.VerifyResult
	deleteEr error
}

func (s *stubBackupServicer) CreateBackup(_ context.Context, opts qrdbservice.BackupOptions) (*qrdbservice.BackupInfo, error) {
	s.calls = append(s.calls, "CreateBackup")
	s.opts = opts
	return s.info, nil
}

func (s *stubBackupServicer) ListBackups(context.Context) ([]qrdbservice.BackupInfo, error) {
	s.calls = append(s.calls, "ListBackups")
	return s.list, nil
}

func (s *stubBackupServicer) GetBackup(_ context.Context, id string) (*qrdbservice.BackupInfo, error) {
	s.calls = append(s.calls, "GetBackup")
	s.ids = append(s.ids, id)
	return s.info, nil
}

func (s *stubBackupServicer) DeleteBackup(_ context.Context, id string) error {
	s.calls = append(s.calls, "DeleteBackup")
	s.ids = append(s.ids, id)
	return s.deleteEr
}

func (s *stubBackupServicer) RestoreBackup(_ context.Context, opts qrdbservice.RestoreOptions) (*qrdbservice.RestoreResult, error) {
	s.calls = append(s.calls, "RestoreBackup")
	s.restore = opts
	return s.result, nil
}

func (s *stubBackupServicer) VerifyBackup(_ context.Context, id string) (*qrdbservice.VerifyResult, error) {
	s.calls = append(s.calls, "VerifyBackup")
	s.ids = append(s.ids, id)
	return s.verify, nil
}

func TestQRDBBackupServicerProxy_DelegatesToResolvedServicer(t *testing.T) {
	stub := &stubBackupServicer{
		info:     &qrdbservice.BackupInfo{ID: "bk-1"},
		list:     []qrdbservice.BackupInfo{{ID: "bk-1"}, {ID: "bk-2"}},
		result:   &qrdbservice.RestoreResult{BackupID: "bk-1", ShardsRestored: 2},
		verify:   &qrdbservice.VerifyResult{Valid: true, ChecksumMatch: true},
		deleteEr: errors.New("delete refused"),
	}
	p := &qrdbBackupServicerProxy{resolve: func() qrdbservice.BackupServicer { return stub }}
	ctx := context.Background()

	createOpts := qrdbservice.BackupOptions{Destination: "dest", Metadata: map[string]string{"k": "v"}}
	info, err := p.CreateBackup(ctx, createOpts)
	require.NoError(t, err)
	assert.Same(t, stub.info, info)
	assert.Equal(t, createOpts, stub.opts)

	list, err := p.ListBackups(ctx)
	require.NoError(t, err)
	assert.Equal(t, stub.list, list)

	got, err := p.GetBackup(ctx, "get-id")
	require.NoError(t, err)
	assert.Same(t, stub.info, got)

	assert.Same(t, stub.deleteEr, p.DeleteBackup(ctx, "delete-id"))

	restoreOpts := qrdbservice.RestoreOptions{BackupID: "bk-1", TargetShards: []uint64{1, 2}, ValidateFirst: true}
	res, err := p.RestoreBackup(ctx, restoreOpts)
	require.NoError(t, err)
	assert.Same(t, stub.result, res)
	assert.Equal(t, restoreOpts, stub.restore)

	vr, err := p.VerifyBackup(ctx, "verify-id")
	require.NoError(t, err)
	assert.Same(t, stub.verify, vr)

	assert.Equal(t, []string{"CreateBackup", "ListBackups", "GetBackup", "DeleteBackup", "RestoreBackup", "VerifyBackup"}, stub.calls)
	assert.Equal(t, []string{"get-id", "delete-id", "verify-id"}, stub.ids)
}

// serveQuicBaaSQRDBOverTCP registers srv's own qrdb.v1 surface through
// registerQRDBService (the path the Unix socket and sealed-window servers
// take) on a real gRPC server and returns a client for it.
func serveQuicBaaSQRDBOverTCP(t *testing.T, srv *Server) qrdbv1.QRDBServiceClient {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	gs := ggrpc.NewServer()
	srv.registerQRDBService(gs)
	served := make(chan error, 1)
	go func() { served <- gs.Serve(listener) }()
	t.Cleanup(func() {
		gs.Stop()
		if serveErr := <-served; serveErr != nil {
			t.Errorf("serve: %v", serveErr)
		}
	})
	conn, err := ggrpc.NewClient(listener.Addr().String(),
		ggrpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return qrdbv1.NewQRDBServiceClient(conn)
}

func TestQRDBBackupOverQuicBaaSGRPC_ServesNodeBackupService(t *testing.T) {
	srv := withTempServer(t)
	require.NoError(t, srv.initStandaloneQRDBNode())
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	require.NotNil(t, srv.GetQRDBBackupServicer(), "the embedded node must expose its backup service")

	client := serveQuicBaaSQRDBOverTCP(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := client.BackupList(ctx, &emptypb.Empty{})
	require.NoError(t, err, "BackupList must reach the node's backup service")

	_, err = client.BackupGet(ctx, &qrdbv1.BackupGetRequest{Id: "missing-backup"})
	require.Error(t, err)
	assert.NotEqual(t, codes.Unimplemented, status.Code(err), "BackupGet must not be unimplemented: %v", err)
	assert.Contains(t, status.Convert(err).Message(), "missing-backup")
}

func TestQRDBBackupOverQuicBaaSGRPC_UnimplementedWithoutNode(t *testing.T) {
	srv := withTempServer(t)
	client := serveQuicBaaSQRDBOverTCP(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := client.BackupList(ctx, &emptypb.Empty{})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err), "want Unimplemented, not Internal: %v", err)
}
