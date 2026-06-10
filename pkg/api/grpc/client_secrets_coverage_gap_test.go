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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// SecretsManagement client coverage: happy-path proto mapping + RPC-error
// mapping (convertError) + nil-request fail-closed guards. These methods were
// previously exercised only on the nil-request branch, leaving the proto build
// + response mapping + error path uncovered.
// =============================================================================

const secretsBufSize = 1024 * 1024

// secretsMockServer returns deterministic responses, or — when failAll is set —
// a codes.NotFound error from every RPC so the client error path (convertError)
// is exercised.
type secretsMockServer struct {
	pb.UnimplementedSecretsManagementServer
	failAll bool
}

func (m *secretsMockServer) fail() error {
	return status.Error(codes.NotFound, "secret not found")
}

func (m *secretsMockServer) StoreSecret(_ context.Context, req *pb.StoreSecretRequest) (*pb.StoreSecretResponse, error) {
	if m.failAll {
		return nil, m.fail()
	}
	return &pb.StoreSecretResponse{SecretId: req.SecretId, Version: 1, Timestamp: time.Now().Unix()}, nil
}

func (m *secretsMockServer) GetSecret(_ context.Context, req *pb.GetSecretRequest) (*pb.GetSecretResponse, error) {
	if m.failAll {
		return nil, m.fail()
	}
	return &pb.GetSecretResponse{
		SecretId: req.SecretId, Version: 2, Data: []byte("v"),
		Metadata: map[string]string{"k": "v"}, CreatedAt: time.Now().Unix(), Timestamp: time.Now().Unix(),
	}, nil
}

func (m *secretsMockServer) ListSecrets(_ context.Context, _ *pb.ListSecretsRequest) (*pb.ListSecretsResponse, error) {
	if m.failAll {
		return nil, m.fail()
	}
	return &pb.ListSecretsResponse{NextCursor: "c", HasMore: false, TotalCount: 0, Timestamp: time.Now().Unix()}, nil
}

func (m *secretsMockServer) DeleteSecret(_ context.Context, req *pb.DeleteSecretRequest) (*pb.DeleteSecretResponse, error) {
	if m.failAll {
		return nil, m.fail()
	}
	return &pb.DeleteSecretResponse{SecretId: req.SecretId, Deleted: true, Timestamp: time.Now().Unix()}, nil
}

func (m *secretsMockServer) ListSecretVersions(_ context.Context, _ *pb.ListSecretVersionsRequest) (*pb.ListSecretVersionsResponse, error) {
	if m.failAll {
		return nil, m.fail()
	}
	return &pb.ListSecretVersionsResponse{NextCursor: "", HasMore: false, Timestamp: time.Now().Unix()}, nil
}

func (m *secretsMockServer) RollbackSecret(_ context.Context, req *pb.RollbackSecretRequest) (*pb.RollbackSecretResponse, error) {
	if m.failAll {
		return nil, m.fail()
	}
	return &pb.RollbackSecretResponse{SecretId: req.SecretId, NewVersion: 3, Timestamp: time.Now().Unix()}, nil
}

func (m *secretsMockServer) RotateSecret(_ context.Context, req *pb.RotateSecretRequest) (*pb.RotateSecretResponse, error) {
	if m.failAll {
		return nil, m.fail()
	}
	return &pb.RotateSecretResponse{SecretId: req.SecretId, OldVersion: 1, NewVersion: 2, Timestamp: time.Now().Unix()}, nil
}

func (m *secretsMockServer) UpdateSecretStatus(_ context.Context, req *pb.UpdateSecretStatusRequest) (*pb.UpdateSecretStatusResponse, error) {
	if m.failAll {
		return nil, m.fail()
	}
	return &pb.UpdateSecretStatusResponse{SecretId: req.SecretId, Status: req.Status, Timestamp: time.Now().Unix()}, nil
}

func (m *secretsMockServer) GetSecretsByPrefix(_ context.Context, _ *pb.GetSecretsByPrefixRequest) (*pb.GetSecretsByPrefixResponse, error) {
	if m.failAll {
		return nil, m.fail()
	}
	return &pb.GetSecretsByPrefixResponse{
		Secrets: map[string]*pb.SecretData{
			"app/db": {Data: map[string][]byte{"password": []byte("s")}, Metadata: map[string]string{"k": "v"}, Version: 1},
		},
		NextCursor: "", HasMore: false, TotalCount: 1, Timestamp: time.Now().Unix(),
	}, nil
}

func setupConnectedSecretsClient(t *testing.T, failAll bool) (*Client, *grpc.Server) {
	t.Helper()
	lis := bufconn.Listen(secretsBufSize)
	srv := grpc.NewServer()
	pb.RegisterSecretsManagementServer(srv, &secretsMockServer{failAll: failAll})
	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	client := &Client{
		conn: conn,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      1,
			retryBackoff:    10 * time.Millisecond,
			maxRetryBackoff: 100 * time.Millisecond,
		},
		connected: true,
	}
	return client, srv
}

// TestClientSecrets_HappyPath exercises the proto-build + response-mapping path
// of every secrets RPC against a mock server returning success.
func TestClientSecrets_HappyPath(t *testing.T) {
	client, srv := setupConnectedSecretsClient(t, false)
	defer srv.Stop()
	defer client.conn.Close()
	ctx := context.Background()

	storeResp, err := client.StoreSecret(ctx, &service.StoreSecretRequest{
		SecretID: "s1", Name: "db", Data: []byte("v"),
		Metadata: map[string]string{"k": "v"}, ExpiresAt: ptrTime(time.Now().Add(time.Hour)),
	})
	require.NoError(t, err)
	assert.Equal(t, "s1", storeResp.SecretID)
	assert.Equal(t, uint64(1), storeResp.Version)

	getResp, err := client.GetSecret(ctx, &service.GetSecretRequest{SecretID: "s1"})
	require.NoError(t, err)
	assert.Equal(t, []byte("v"), getResp.Data)

	listResp, err := client.ListSecrets(ctx, &service.ListSecretsRequest{Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, "c", listResp.NextCursor)

	delResp, err := client.DeleteSecret(ctx, &service.DeleteSecretRequest{SecretID: "s1", PermanentlyDelete: true})
	require.NoError(t, err)
	assert.True(t, delResp.Deleted)

	verResp, err := client.ListSecretVersions(ctx, &service.ListSecretVersionsRequest{SecretID: "s1", Limit: 5})
	require.NoError(t, err)
	assert.False(t, verResp.HasMore)

	rbResp, err := client.RollbackSecret(ctx, &service.RollbackSecretRequest{SecretID: "s1", TargetVersion: 1})
	require.NoError(t, err)
	assert.Equal(t, uint64(3), rbResp.NewVersion)

	rotResp, err := client.RotateSecret(ctx, &service.RotateSecretRequest{SecretID: "s1", NewData: []byte("n")})
	require.NoError(t, err)
	assert.Equal(t, uint64(2), rotResp.NewVersion)

	statResp, err := client.UpdateSecretStatus(ctx, &service.UpdateSecretStatusRequest{SecretID: "s1", Status: service.SecretStatusEnabled})
	require.NoError(t, err)
	assert.Equal(t, "s1", statResp.SecretID)

	prefixResp, err := client.GetSecretsByPrefix(ctx, &service.GetSecretsByPrefixRequest{Prefix: "app/", IncludeData: true, Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, 1, prefixResp.TotalCount)
	assert.Contains(t, prefixResp.Secrets, "app/db")
}

// TestClientSecrets_RPCErrorMapped exercises the convertError branch of every
// secrets RPC: a server-side error must propagate as a non-nil error rather than
// being silently dropped.
func TestClientSecrets_RPCErrorMapped(t *testing.T) {
	client, srv := setupConnectedSecretsClient(t, true)
	defer srv.Stop()
	defer client.conn.Close()
	ctx := context.Background()

	_, err := client.StoreSecret(ctx, &service.StoreSecretRequest{SecretID: "s1", Data: []byte("v")})
	require.Error(t, err)
	_, err = client.GetSecret(ctx, &service.GetSecretRequest{SecretID: "s1"})
	require.Error(t, err)
	_, err = client.ListSecrets(ctx, &service.ListSecretsRequest{})
	require.Error(t, err)
	_, err = client.DeleteSecret(ctx, &service.DeleteSecretRequest{SecretID: "s1"})
	require.Error(t, err)
	_, err = client.ListSecretVersions(ctx, &service.ListSecretVersionsRequest{SecretID: "s1"})
	require.Error(t, err)
	_, err = client.RollbackSecret(ctx, &service.RollbackSecretRequest{SecretID: "s1", TargetVersion: 1})
	require.Error(t, err)
	_, err = client.RotateSecret(ctx, &service.RotateSecretRequest{SecretID: "s1", NewData: []byte("n")})
	require.Error(t, err)
	_, err = client.UpdateSecretStatus(ctx, &service.UpdateSecretStatusRequest{SecretID: "s1", Status: service.SecretStatusEnabled})
	require.Error(t, err)
	_, err = client.GetSecretsByPrefix(ctx, &service.GetSecretsByPrefixRequest{Prefix: "app/"})
	require.Error(t, err)
}

// TestClientSecrets_NilRequestFailClosed asserts every secrets RPC rejects a nil
// request with ErrInvalidInput before touching the wire.
func TestClientSecrets_NilRequestFailClosed(t *testing.T) {
	client, srv := setupConnectedSecretsClient(t, false)
	defer srv.Stop()
	defer client.conn.Close()
	ctx := context.Background()

	assertInvalidInput := func(err error) {
		t.Helper()
		require.Error(t, err)
		var qbErr *qberrors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, qberrors.ErrInvalidInput, qbErr.Code)
	}

	_, err := client.StoreSecret(ctx, nil)
	assertInvalidInput(err)
	_, err = client.GetSecret(ctx, nil)
	assertInvalidInput(err)
	_, err = client.ListSecrets(ctx, nil)
	assertInvalidInput(err)
	_, err = client.DeleteSecret(ctx, nil)
	assertInvalidInput(err)
	_, err = client.ListSecretVersions(ctx, nil)
	assertInvalidInput(err)
	_, err = client.RollbackSecret(ctx, nil)
	assertInvalidInput(err)
	_, err = client.RotateSecret(ctx, nil)
	assertInvalidInput(err)
	_, err = client.UpdateSecretStatus(ctx, nil)
	assertInvalidInput(err)
	_, err = client.GetSecretsByPrefix(ctx, nil)
	assertInvalidInput(err)
	_, err = client.WatchSecretsByPrefix(ctx, nil)
	assertInvalidInput(err)
}

func ptrTime(t time.Time) *time.Time { return &t }
