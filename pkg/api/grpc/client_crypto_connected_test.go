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
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

const cryptoBufSize = 1024 * 1024

// =============================================================================
// Minimal Mock Crypto Server for Connected Tests
// =============================================================================

type connectedCryptoServer struct {
	pb.UnimplementedCryptoServer
}

func (m *connectedCryptoServer) Sign(_ context.Context, req *pb.SignRequest) (*pb.SignResponse, error) {
	return &pb.SignResponse{
		KeyId:         req.KeyId,
		KeyVersion:    1,
		Signature:     []byte("mock-signature"),
		HashAlgorithm: req.HashAlgorithm,
		Timestamp:     time.Now().Unix(),
	}, nil
}

func (m *connectedCryptoServer) Verify(_ context.Context, req *pb.VerifyRequest) (*pb.VerifyResponse, error) {
	return &pb.VerifyResponse{
		KeyId:     req.KeyId,
		Valid:     true,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *connectedCryptoServer) Hash(_ context.Context, _ *pb.HashRequest) (*pb.HashResponse, error) {
	return &pb.HashResponse{
		Hash:      []byte("mock-hash"),
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *connectedCryptoServer) GenerateHMAC(_ context.Context, req *pb.HMACRequest) (*pb.HMACResponse, error) {
	return &pb.HMACResponse{
		KeyId:     req.KeyId,
		Hmac:      []byte("mock-hmac"),
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *connectedCryptoServer) VerifyHMAC(_ context.Context, req *pb.VerifyHMACRequest) (*pb.VerifyHMACResponse, error) {
	return &pb.VerifyHMACResponse{
		KeyId:     req.KeyId,
		Valid:     true,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *connectedCryptoServer) Rewrap(_ context.Context, _ *pb.RewrapRequest) (*pb.RewrapResponse, error) {
	return &pb.RewrapResponse{
		Ciphertext: []byte("rewrapped-data"),
		Timestamp:  time.Now().Unix(),
	}, nil
}

func (m *connectedCryptoServer) GenerateRandom(_ context.Context, req *pb.GenerateRandomRequest) (*pb.GenerateRandomResponse, error) {
	data := make([]byte, req.NumBytes)
	return &pb.GenerateRandomResponse{
		RandomBytes: data,
		Timestamp:   time.Now().Unix(),
	}, nil
}

// =============================================================================
// Test Server Setup
// =============================================================================

func setupCryptoTestServer(t *testing.T) (*grpc.Server, *bufconn.Listener) {
	t.Helper()
	lis := bufconn.Listen(cryptoBufSize)
	server := grpc.NewServer()
	pb.RegisterCryptoServer(server, &connectedCryptoServer{})
	go func() {
		if err := server.Serve(lis); err != nil {
			// Server stopped
		}
	}()
	return server, lis
}

func setupConnectedCryptoClient(t *testing.T) (*Client, *grpc.Server) {
	t.Helper()
	server, lis := setupCryptoTestServer(t)
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
		conn: conn,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      1,
			retryBackoff:    10 * time.Millisecond,
			maxRetryBackoff: 100 * time.Millisecond,
		},
		connected: true,
	}

	return client, server
}

// =============================================================================
// Crypto Client Connected Tests
// =============================================================================

func TestClientKMSSign_Connected(t *testing.T) {
	client, server := setupConnectedCryptoClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSSign(context.Background(), &service.SignRequest{
		KeyID:   "key-1",
		Message: []byte("data-to-sign"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "key-1", resp.KeyID)
	assert.Equal(t, []byte("mock-signature"), resp.Signature)
}

func TestClientKMSVerify_Connected(t *testing.T) {
	client, server := setupConnectedCryptoClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSVerify(context.Background(), &service.VerifyRequest{
		KeyID:     "key-1",
		Message:   []byte("data-to-verify"),
		Signature: []byte("mock-signature"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Valid)
}

func TestClientKMSHash_Connected(t *testing.T) {
	client, server := setupConnectedCryptoClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSHash(context.Background(), &service.HashRequest{
		Data: []byte("data-to-hash"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, []byte("mock-hash"), resp.Hash)
}

func TestClientKMSGenerateHMAC_Connected(t *testing.T) {
	client, server := setupConnectedCryptoClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSGenerateHMAC(context.Background(), &service.HMACRequest{
		KeyID:   "hmac-key-1",
		Message: []byte("data-for-hmac"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, []byte("mock-hmac"), resp.HMAC)
}

func TestClientKMSVerifyHMAC_Connected(t *testing.T) {
	client, server := setupConnectedCryptoClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSVerifyHMAC(context.Background(), &service.VerifyHMACRequest{
		KeyID:   "hmac-key-1",
		Message: []byte("data-for-hmac"),
		HMAC:    []byte("mock-hmac"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Valid)
}

func TestClientKMSRewrap_Connected(t *testing.T) {
	client, server := setupConnectedCryptoClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSRewrap(context.Background(), &service.RewrapRequest{
		Ciphertext: []byte("wrapped-data"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, []byte("rewrapped-data"), resp.Ciphertext)
}

func TestClientKMSGenerateRandom_Connected(t *testing.T) {
	client, server := setupConnectedCryptoClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSGenerateRandom(context.Background(), &service.GenerateRandomRequest{
		NumBytes: 32,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.RandomBytes, 32)
}
