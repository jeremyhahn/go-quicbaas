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
)

const kmsBackendBufSize = 1024 * 1024

// =============================================================================
// Minimal Mock KeyManagement Server for Backend Tests
// =============================================================================

type kmsBackendMockServer struct {
	pb.UnimplementedKeyManagementServer
}

func (m *kmsBackendMockServer) BackendStatus(_ context.Context, _ *pb.BackendStatusRequest) (*pb.BackendStatusResponse, error) {
	return &pb.BackendStatusResponse{
		BackendType:  "software",
		Status:       "healthy",
		Device:       "",
		Region:       "",
		KeyId:        "master-key-1",
		PcrBanks:     []string{},
		PcrRegisters: []int32{},
		Capabilities: map[string]bool{
			"encrypt": true,
			"decrypt": true,
			"sign":    true,
		},
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *kmsBackendMockServer) BackendTest(_ context.Context, _ *pb.BackendTestRequest) (*pb.BackendTestResponse, error) {
	return &pb.BackendTestResponse{
		Success:    true,
		DurationMs: 42,
		Message:    "all tests passed",
		Timestamp:  time.Now().Unix(),
	}, nil
}

// =============================================================================
// Test Server Setup
// =============================================================================

func setupKMSBackendTestServer(t *testing.T) (*grpc.Server, *bufconn.Listener) {
	t.Helper()
	lis := bufconn.Listen(kmsBackendBufSize)
	server := grpc.NewServer()
	pb.RegisterKeyManagementServer(server, &kmsBackendMockServer{})
	go func() {
		if err := server.Serve(lis); err != nil {
			// Server stopped
		}
	}()
	return server, lis
}

func setupConnectedKMSBackendClient(t *testing.T) (*Client, *grpc.Server) {
	t.Helper()
	server, lis := setupKMSBackendTestServer(t)
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
// KMS Backend Client Connected Tests
// =============================================================================

func TestClientKMSBackendStatus_Connected(t *testing.T) {
	client, server := setupConnectedKMSBackendClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSBackendStatus(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "software", resp.BackendType)
	assert.Equal(t, "healthy", resp.Status)
	assert.Equal(t, "master-key-1", resp.KeyID)
	assert.True(t, resp.Capabilities["encrypt"])
}

func TestClientKMSBackendTest_Connected(t *testing.T) {
	client, server := setupConnectedKMSBackendClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSBackendTest(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "all tests passed", resp.Message)
	assert.Equal(t, 42*time.Millisecond, resp.Duration)
}
