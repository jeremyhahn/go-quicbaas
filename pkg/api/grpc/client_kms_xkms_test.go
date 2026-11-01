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
	"crypto/elliptic"
	"crypto/x509"
	stderrors "errors"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

const xkmsKeystoreBufSize = 1024 * 1024

// recordingKeystoreServer is an xkms.v1 KeystoreService that records the
// requests it receives and answers with a fixed result or error.
type recordingKeystoreServer struct {
	xkmssdk.ProtoUnimplementedKeystoreServiceServer

	mu          sync.Mutex
	generateReq *xkmssdk.ProtoGenerateKeyRequest
	listReq     *xkmssdk.ProtoListKeysRequest
	deleteReq   *xkmssdk.ProtoDeleteKeyRequest

	err      error
	listKeys []*xkmssdk.ProtoKeyInfo
	total    int32
}

func (s *recordingKeystoreServer) GenerateKey(_ context.Context, req *xkmssdk.ProtoGenerateKeyRequest) (*xkmssdk.ProtoGenerateKeyResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generateReq = req
	if s.err != nil {
		return nil, s.err
	}
	return &xkmssdk.ProtoGenerateKeyResponse{
		KeyId:     req.GetKeyId(),
		KeyType:   req.GetKeyType(),
		CreatedAt: timestamppb.Now(),
	}, nil
}

func (s *recordingKeystoreServer) ListKeys(_ context.Context, req *xkmssdk.ProtoListKeysRequest) (*xkmssdk.ProtoListKeysResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listReq = req
	if s.err != nil {
		return nil, s.err
	}
	return &xkmssdk.ProtoListKeysResponse{Keys: s.listKeys, Total: s.total}, nil
}

func (s *recordingKeystoreServer) DeleteKey(_ context.Context, req *xkmssdk.ProtoDeleteKeyRequest) (*xkmssdk.ProtoDeleteKeyResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteReq = req
	if s.err != nil {
		return nil, s.err
	}
	return &xkmssdk.ProtoDeleteKeyResponse{Success: true}, nil
}

// newXKMSKeystoreClient serves fake over bufconn with ONLY the xkms.v1
// KeystoreService registered, as the node does, and returns a connected
// Client.
func newXKMSKeystoreClient(t *testing.T, fake *recordingKeystoreServer) *Client {
	t.Helper()
	lis := bufconn.Listen(xkmsKeystoreBufSize)
	server := grpc.NewServer()
	xkmssdk.ProtoRegisterKeystoreServiceServer(server, fake)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(lis) }()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, conn.Close())
		server.Stop()
		require.NoError(t, <-serveDone)
	})
	return &Client{conn: conn, connected: true, logger: log}
}

func TestClientKMSGenerateKey_XKMSKeystore(t *testing.T) {
	tests := []struct {
		name          string
		attrs         *xkmssdk.KeyAttributes
		wantKeyType   string
		wantKeySize   int32
		wantCurve     string
		wantAlgorithm string
		wantServAlg   service.KeyAlgorithm
	}{
		{
			name:          "AES-256-GCM",
			attrs:         &xkmssdk.KeyAttributes{SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM},
			wantKeyType:   "symmetric",
			wantKeySize:   256,
			wantAlgorithm: "aes-256-gcm",
			wantServAlg:   service.KeyAlgAES256GCM,
		},
		{
			name: "RSA-2048",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm:  x509.RSA,
				RSAAttributes: &xkmssdk.RSAAttributes{KeySize: 2048},
			},
			wantKeyType: "rsa",
			wantKeySize: 2048,
			wantServAlg: service.KeyAlgRSA2048,
		},
		{
			name: "ECDSA-P256",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm:  x509.ECDSA,
				ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P256()},
			},
			wantKeyType: "ecdsa",
			wantKeySize: 256,
			wantCurve:   "P-256",
			wantServAlg: service.KeyAlgECDSAP256,
		},
		{
			name:        "Ed25519",
			attrs:       &xkmssdk.KeyAttributes{KeyAlgorithm: x509.Ed25519},
			wantKeyType: "ed25519",
			wantKeySize: 256,
			wantServAlg: service.KeyAlgEd25519,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &recordingKeystoreServer{}
			client := newXKMSKeystoreClient(t, fake)

			resp, err := client.KMSGenerateKey(context.Background(), &service.GenerateKeyRequest{
				Name:          "key-" + tt.name,
				Description:   "xkms keystore key",
				KeyAttributes: tt.attrs,
			})
			require.NoError(t, err)

			fake.mu.Lock()
			got := fake.generateReq
			fake.mu.Unlock()
			require.NotNil(t, got, "the xkms.v1 KeystoreService must receive GenerateKey")
			assert.Equal(t, "key-"+tt.name, got.GetKeyId())
			assert.Equal(t, tt.wantKeyType, got.GetKeyType())
			assert.Equal(t, tt.wantKeySize, got.GetKeySize())
			assert.Equal(t, tt.wantCurve, got.GetCurve())
			assert.Equal(t, tt.wantAlgorithm, got.GetAlgorithm())
			assert.False(t, got.GetExportable())

			assert.Equal(t, "key-"+tt.name, resp.Key.KeyID)
			assert.Equal(t, tt.wantServAlg, resp.Key.Algorithm)
			assert.Equal(t, uint64(1), resp.Key.Version)
			assert.Equal(t, service.KeyStatusEnabled, resp.Key.Status)
			assert.Equal(t, "key-"+tt.name, resp.Metadata.Name)
			assert.Equal(t, "xkms keystore key", resp.Metadata.Description)
			assert.False(t, resp.Metadata.Exportable)
		})
	}
}

func TestClientKMSGenerateKey_XKMSKeystoreAlreadyExists(t *testing.T) {
	fake := &recordingKeystoreServer{err: status.Error(codes.AlreadyExists, "key exists")}
	client := newXKMSKeystoreClient(t, fake)

	resp, err := client.KMSGenerateKey(context.Background(), &service.GenerateKeyRequest{
		Name:          "dup",
		KeyAttributes: &xkmssdk.KeyAttributes{SymmetricAlgorithm: xkmssdk.SymmetricAES128GCM},
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.True(t, stderrors.As(err, &qbErr), "error must be a typed QBError, got %T", err)
	assert.Equal(t, errors.ErrAlreadyExists, qbErr.Code)
}

func TestClientKMSListKeys_XKMSKeystore(t *testing.T) {
	fake := &recordingKeystoreServer{
		listKeys: []*xkmssdk.ProtoKeyInfo{
			{KeyId: "k1", KeyType: "symmetric", Algorithm: "aes-256-gcm", KeySize: 256, KeyVersion: 2},
			{KeyId: "k2", KeyType: "ecdsa", KeySize: 384, Curve: "P-384", KeyVersion: 1},
		},
		total: 5,
	}
	client := newXKMSKeystoreClient(t, fake)

	resp, err := client.KMSListKeys(context.Background(), &service.ListKeysRequest{Limit: 2})
	require.NoError(t, err)

	fake.mu.Lock()
	got := fake.listReq
	fake.mu.Unlock()
	require.NotNil(t, got)
	assert.Equal(t, int32(2), got.GetLimit())
	assert.Equal(t, int32(0), got.GetOffset())

	require.Len(t, resp.Keys, 2)
	assert.Equal(t, "k1", resp.Keys[0].KeyID)
	assert.Equal(t, uint64(2), resp.Keys[0].Version)
	assert.Equal(t, service.KeyAlgAES256GCM, resp.Keys[0].Algorithm)
	assert.Equal(t, service.KeyAlgECDSAP384, resp.Keys[1].Algorithm)
	assert.Equal(t, 5, resp.TotalCount)
	assert.True(t, resp.HasMore)
	assert.Equal(t, "2", resp.NextCursor)

	// The cursor is the xkms.v1 offset of the next page.
	_, err = client.KMSListKeys(context.Background(), &service.ListKeysRequest{Limit: 2, Cursor: resp.NextCursor})
	require.NoError(t, err)
	fake.mu.Lock()
	assert.Equal(t, int32(2), fake.listReq.GetOffset())
	fake.mu.Unlock()
}

func TestClientKMSListKeys_XKMSKeystoreErrors(t *testing.T) {
	t.Run("bad cursor", func(t *testing.T) {
		client := newXKMSKeystoreClient(t, &recordingKeystoreServer{})
		resp, err := client.KMSListKeys(context.Background(), &service.ListKeysRequest{Cursor: "not-a-number"})
		require.Error(t, err)
		assert.Nil(t, resp)
		var qbErr *errors.QBError
		require.True(t, stderrors.As(err, &qbErr), "error must be a typed QBError, got %T", err)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("server error", func(t *testing.T) {
		client := newXKMSKeystoreClient(t, &recordingKeystoreServer{err: status.Error(codes.PermissionDenied, "denied")})
		resp, err := client.KMSListKeys(context.Background(), nil)
		require.Error(t, err)
		assert.Nil(t, resp)
		var qbErr *errors.QBError
		require.True(t, stderrors.As(err, &qbErr), "error must be a typed QBError, got %T", err)
		assert.Equal(t, errors.ErrForbidden, qbErr.Code)
	})
}

func TestClientKMSDeleteKey_XKMSKeystore(t *testing.T) {
	fake := &recordingKeystoreServer{}
	client := newXKMSKeystoreClient(t, fake)

	resp, err := client.KMSDeleteKey(context.Background(), &service.DeleteKeyRequest{KeyID: "k1", Force: true})
	require.NoError(t, err)
	assert.Equal(t, "k1", resp.KeyID)
	assert.Equal(t, service.KeyStatusDeleted, resp.Status)

	fake.mu.Lock()
	defer fake.mu.Unlock()
	require.NotNil(t, fake.deleteReq)
	assert.Equal(t, "k1", fake.deleteReq.GetKeyId())
}

func TestClientKMSDeleteKey_XKMSKeystoreNotFound(t *testing.T) {
	client := newXKMSKeystoreClient(t, &recordingKeystoreServer{err: status.Error(codes.NotFound, "no such key")})

	resp, err := client.KMSDeleteKey(context.Background(), &service.DeleteKeyRequest{KeyID: "missing"})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.True(t, stderrors.As(err, &qbErr), "error must be a typed QBError, got %T", err)
	assert.Equal(t, errors.ErrNotFound, qbErr.Code)
}
