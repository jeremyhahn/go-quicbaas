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

package grpc

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Connected server implementing seal + backup RPCs
// =============================================================================

type backupSealServer struct {
	proto.UnimplementedQuicBaaSServer
	fail bool // when true, every RPC returns an error
}

func (s *backupSealServer) CreateBackup(_ context.Context, req *proto.CreateBackupRequest) (*proto.CreateBackupResponse, error) {
	if s.fail {
		return nil, status.Error(codes.Internal, "boom")
	}
	return &proto.CreateBackupResponse{BackupId: "bk-1", Size: 1024, Timestamp: time.Now().UnixNano()}, nil
}

func (s *backupSealServer) ListBackups(_ context.Context, req *proto.ListBackupsRequest) (*proto.ListBackupsResponse, error) {
	if s.fail {
		return nil, status.Error(codes.Internal, "boom")
	}
	return &proto.ListBackupsResponse{
		Backups: []*proto.BackupInfo{
			{BackupId: "bk-1", BackupType: proto.BackupType_BACKUP_TYPE_FULL, Size: 1024, NumShards: 2, Timestamp: time.Now().UnixNano()},
			{BackupId: "bk-2", BackupType: proto.BackupType_BACKUP_TYPE_INCREMENTAL, Size: 512, NumShards: 1, Timestamp: time.Now().UnixNano()},
		},
	}, nil
}

func (s *backupSealServer) RestoreBackup(_ context.Context, req *proto.RestoreBackupRequest) (*proto.RestoreBackupResponse, error) {
	if s.fail {
		return nil, status.Error(codes.Internal, "boom")
	}
	return &proto.RestoreBackupResponse{BackupId: req.BackupId, KeysRestored: 5, Duration: int64(time.Second)}, nil
}

func (s *backupSealServer) DeleteBackup(_ context.Context, req *proto.DeleteBackupRequest) (*proto.DeleteBackupResponse, error) {
	if s.fail {
		return nil, status.Error(codes.Internal, "boom")
	}
	return &proto.DeleteBackupResponse{Success: true}, nil
}

func (s *backupSealServer) VerifyBackup(_ context.Context, req *proto.VerifyBackupRequest) (*proto.VerifyBackupResponse, error) {
	if s.fail {
		return nil, status.Error(codes.Internal, "boom")
	}
	return &proto.VerifyBackupResponse{Valid: true, ChecksumValid: true, Message: "minor warning"}, nil
}

func (s *backupSealServer) SealStatus(_ context.Context, _ *proto.SealStatusRequest) (*proto.SealStatusResponse, error) {
	if s.fail {
		return nil, status.Error(codes.Internal, "boom")
	}
	return &proto.SealStatusResponse{
		Sealed: false, Initialized: true, Threshold: 2, TotalShares: 3, SharesProvided: 2, Version: 1,
	}, nil
}

func (s *backupSealServer) SealInit(_ context.Context, req *proto.SealInitRequest) (*proto.SealInitResponse, error) {
	if s.fail {
		return nil, status.Error(codes.Internal, "boom")
	}
	return &proto.SealInitResponse{Shares: [][]byte{[]byte("s1"), []byte("s2"), []byte("s3")}, RootToken: "root", Version: 1}, nil
}

func (s *backupSealServer) Unseal(_ context.Context, req *proto.UnsealRequest) (*proto.UnsealResponse, error) {
	if s.fail {
		return nil, status.Error(codes.Internal, "boom")
	}
	return &proto.UnsealResponse{Sealed: false, SharesProvided: 2, Threshold: 2}, nil
}

func (s *backupSealServer) Seal(_ context.Context, _ *proto.SealRequest) (*proto.SealResponse, error) {
	if s.fail {
		return nil, status.Error(codes.Internal, "boom")
	}
	return &proto.SealResponse{Sealed: true}, nil
}

func (s *backupSealServer) SealRekey(_ context.Context, req *proto.SealRekeyRequest) (*proto.SealRekeyResponse, error) {
	if s.fail {
		return nil, status.Error(codes.Internal, "boom")
	}
	return &proto.SealRekeyResponse{Shares: [][]byte{[]byte("n1"), []byte("n2")}, Version: 2}, nil
}

// setupConnectedBackupSealClient registers a backupSealServer and returns a
// connected client wired to it.
func setupConnectedBackupSealClient(t *testing.T, fail bool) *Client {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	proto.RegisterQuicBaaSServer(server, &backupSealServer{fail: fail})
	go func() { _ = server.Serve(lis) }()

	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	client := &Client{
		conn:   conn,
		client: proto.NewQuicBaaSClient(conn),
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      0,
			retryBackoff:    time.Millisecond,
			maxRetryBackoff: 10 * time.Millisecond,
		},
		connected: true,
	}
	t.Cleanup(func() {
		_ = conn.Close()
		server.Stop()
	})
	return client
}

// =============================================================================
// Seal client RPCs — happy path
// =============================================================================

func TestSealClient_Connected_HappyPaths(t *testing.T) {
	client := setupConnectedBackupSealClient(t, false)
	ctx := context.Background()

	status, err := client.SealStatus(ctx)
	require.NoError(t, err)
	assert.True(t, status.Initialized)
	assert.Equal(t, 2, status.Threshold)

	initResp, err := client.SealInit(ctx, &service.SealInitRequest{Threshold: 2, TotalShares: 3})
	require.NoError(t, err)
	assert.Len(t, initResp.Shares, 3)
	assert.Equal(t, "root", initResp.RootToken)

	unsealResp, err := client.Unseal(ctx, &service.UnsealRequest{Share: []byte("s1")})
	require.NoError(t, err)
	assert.False(t, unsealResp.Sealed)

	require.NoError(t, client.Seal(ctx))

	rekeyResp, err := client.Rekey(ctx, &service.RekeyRequest{NewThreshold: 2, NewTotalShares: 2})
	require.NoError(t, err)
	assert.Len(t, rekeyResp.Shares, 2)
	assert.Equal(t, uint64(2), rekeyResp.Version)
}

// =============================================================================
// Seal client RPCs — server error path
// =============================================================================

func TestSealClient_Connected_ServerErrors(t *testing.T) {
	client := setupConnectedBackupSealClient(t, true)
	ctx := context.Background()

	_, err := client.SealStatus(ctx)
	require.Error(t, err)

	_, err = client.SealInit(ctx, &service.SealInitRequest{})
	require.Error(t, err)

	_, err = client.Unseal(ctx, &service.UnsealRequest{})
	require.Error(t, err)

	require.Error(t, client.Seal(ctx))

	_, err = client.Rekey(ctx, &service.RekeyRequest{})
	require.Error(t, err)
}

// =============================================================================
// Backup client RPCs — happy path
// =============================================================================

func TestBackupClient_Connected_HappyPaths(t *testing.T) {
	client := setupConnectedBackupSealClient(t, false)
	ctx := context.Background()

	createResp, err := client.CreateBackup(ctx, &proto.CreateBackupRequest{})
	require.NoError(t, err)
	assert.Equal(t, "bk-1", createResp.BackupId)

	listResp, err := client.ListBackups(ctx, &proto.ListBackupsRequest{})
	require.NoError(t, err)
	assert.Len(t, listResp.Backups, 2)

	restoreResp, err := client.RestoreBackup(ctx, &proto.RestoreBackupRequest{BackupId: "bk-1"})
	require.NoError(t, err)
	assert.Equal(t, int64(5), restoreResp.KeysRestored)

	delResp, err := client.DeleteBackup(ctx, &proto.DeleteBackupRequest{BackupId: "bk-1"})
	require.NoError(t, err)
	assert.True(t, delResp.Success)

	verifyResp, err := client.VerifyBackup(ctx, &proto.VerifyBackupRequest{BackupId: "bk-1"})
	require.NoError(t, err)
	assert.True(t, verifyResp.Valid)
}

func TestBackupClient_Connected_ServerErrors(t *testing.T) {
	client := setupConnectedBackupSealClient(t, true)
	ctx := context.Background()

	_, err := client.CreateBackup(ctx, &proto.CreateBackupRequest{})
	require.Error(t, err)
	_, err = client.ListBackups(ctx, &proto.ListBackupsRequest{})
	require.Error(t, err)
	_, err = client.RestoreBackup(ctx, &proto.RestoreBackupRequest{})
	require.Error(t, err)
	_, err = client.DeleteBackup(ctx, &proto.DeleteBackupRequest{})
	require.Error(t, err)
	_, err = client.VerifyBackup(ctx, &proto.VerifyBackupRequest{})
	require.Error(t, err)
}

// =============================================================================
// Backup adapter (FromRequest) — happy path + error path
// =============================================================================

func TestBackupAdapter_Connected_HappyPaths(t *testing.T) {
	client := setupConnectedBackupSealClient(t, false)
	adapter := NewBackupClientAdapter(client)
	ctx := context.Background()

	createResp, err := adapter.CreateBackupFromRequest(ctx, &mockBackupCreateRequest{
		backupType: "incremental", compression: "zstd", encryption: "aes256gcm",
		tags: map[string]string{"env": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, "bk-1", createResp.BackupID)
	assert.True(t, createResp.Success)

	listResp, err := adapter.ListBackupsFromRequest(ctx, &mockBackupListRequest{limit: 10})
	require.NoError(t, err)
	assert.Equal(t, 2, listResp.TotalCount)

	restoreResp, err := adapter.RestoreBackupFromRequest(ctx, &mockBackupRestoreRequest{
		backupID: "bk-1", verifyChecksum: true, skipExisting: true,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(5), restoreResp.KeysRestored)

	delResp, err := adapter.DeleteBackupFromRequest(ctx, &mockBackupDeleteRequest{backupID: "bk-1"})
	require.NoError(t, err)
	assert.True(t, delResp.Deleted)

	verifyResp, err := adapter.VerifyBackupFromRequest(ctx, &mockBackupVerifyRequest{backupID: "bk-1"})
	require.NoError(t, err)
	assert.True(t, verifyResp.Valid)
	assert.Equal(t, []string{"minor warning"}, verifyResp.Issues)
}

func TestBackupAdapter_Connected_ServerErrors(t *testing.T) {
	client := setupConnectedBackupSealClient(t, true)
	adapter := NewBackupClientAdapter(client)
	ctx := context.Background()

	_, err := adapter.CreateBackupFromRequest(ctx, &mockBackupCreateRequest{backupType: "full", compression: "gzip", encryption: "none"})
	require.Error(t, err)
	_, err = adapter.ListBackupsFromRequest(ctx, &mockBackupListRequest{})
	require.Error(t, err)
	_, err = adapter.RestoreBackupFromRequest(ctx, &mockBackupRestoreRequest{backupID: "bk-1"})
	require.Error(t, err)
	_, err = adapter.DeleteBackupFromRequest(ctx, &mockBackupDeleteRequest{backupID: "bk-1"})
	require.Error(t, err)
	_, err = adapter.VerifyBackupFromRequest(ctx, &mockBackupVerifyRequest{backupID: "bk-1"})
	require.Error(t, err)
}
