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
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

const vectorAdminBufSize = 1024 * 1024

// =============================================================================
// Minimal Mock VectorDB Server for Additional Coverage
// =============================================================================

type vectorDBCoverageServer struct {
	pb.UnimplementedVectorDBServer
}

func (m *vectorDBCoverageServer) UpdateMetadata(_ context.Context, _ *pb.UpdateMetadataRequest) (*pb.UpdateMetadataResponse, error) {
	return &pb.UpdateMetadataResponse{
		Success:   true,
		Version:   2,
		Timestamp: timestamppb.Now(),
	}, nil
}

func (m *vectorDBCoverageServer) Health(_ context.Context, _ *pb.VectorHealthRequest) (*pb.VectorHealthResponse, error) {
	return &pb.VectorHealthResponse{
		Status:            pb.VectorHealthStatus_VECTOR_HEALTH_STATUS_HEALTHY,
		NodeId:            1,
		Version:           "1.0.0",
		UptimeSeconds:     3600,
		NamespacesHealthy: 3,
	}, nil
}

func (m *vectorDBCoverageServer) GetVectorStats(_ context.Context, _ *pb.GetVectorStatsRequest) (*pb.GetVectorStatsResponse, error) {
	return &pb.GetVectorStatsResponse{
		TotalVectors:    1000,
		TotalNamespaces: 3,
	}, nil
}

// =============================================================================
// Minimal Mock VectorAdmin Server
// =============================================================================

type vectorAdminCoverageServer struct {
	pb.UnimplementedVectorAdminServer
}

func (m *vectorAdminCoverageServer) CompactNamespace(_ context.Context, _ *pb.CompactNamespaceRequest) (*pb.CompactNamespaceResponse, error) {
	return &pb.CompactNamespaceResponse{
		OperationId: "compact-op-1",
		Started:     timestamppb.Now(),
	}, nil
}

func (m *vectorAdminCoverageServer) GetCompactionProgress(_ context.Context, _ *pb.GetCompactionProgressRequest) (*pb.GetCompactionProgressResponse, error) {
	return &pb.GetCompactionProgressResponse{
		OperationId:     "compact-op-1",
		Namespace:       "test-ns",
		PercentComplete: 50.0,
		Status:          "in-progress",
	}, nil
}

func (m *vectorAdminCoverageServer) GetClusterStats(_ context.Context, _ *pb.GetClusterStatsRequest) (*pb.GetClusterStatsResponse, error) {
	return &pb.GetClusterStatsResponse{}, nil
}

// =============================================================================
// Test Server Setup
// =============================================================================

func setupVectorAdminTestServer(t *testing.T) (*grpc.Server, *bufconn.Listener) {
	t.Helper()
	lis := bufconn.Listen(vectorAdminBufSize)
	server := grpc.NewServer()
	pb.RegisterVectorDBServer(server, &vectorDBCoverageServer{})
	pb.RegisterVectorAdminServer(server, &vectorAdminCoverageServer{})
	go func() {
		if err := server.Serve(lis); err != nil {
			// Server stopped
		}
	}()
	return server, lis
}

func setupConnectedVectorAdminClient(t *testing.T) (*Client, *grpc.Server) {
	t.Helper()
	server, lis := setupVectorAdminTestServer(t)
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
// Vector Client Connected Tests - Additional Coverage
// =============================================================================

func TestClientVectorUpdateMetadata_Connected(t *testing.T) {
	client, server := setupConnectedVectorAdminClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.VectorUpdateMetadata(context.Background(), &pb.UpdateMetadataRequest{
		Namespace: "test-ns",
		Id:        "vec-1",
		Metadata:  map[string]string{"key": "value"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, uint64(2), resp.Version)
}

func TestClientVectorHealth_Connected(t *testing.T) {
	client, server := setupConnectedVectorAdminClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.VectorHealth(context.Background(), &pb.VectorHealthRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pb.VectorHealthStatus_VECTOR_HEALTH_STATUS_HEALTHY, resp.Status)
	assert.Equal(t, "1.0.0", resp.Version)
}

func TestClientVectorGetStats_Connected(t *testing.T) {
	client, server := setupConnectedVectorAdminClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.VectorGetStats(context.Background(), &pb.GetVectorStatsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, int64(1000), resp.TotalVectors)
	assert.Equal(t, int32(3), resp.TotalNamespaces)
}

func TestClientVectorCompact_Connected(t *testing.T) {
	client, server := setupConnectedVectorAdminClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.VectorCompact(context.Background(), &pb.CompactNamespaceRequest{
		Namespace: "test-ns",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "compact-op-1", resp.OperationId)
}

func TestClientVectorGetCompactionProgress_Connected(t *testing.T) {
	client, server := setupConnectedVectorAdminClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.VectorGetCompactionProgress(context.Background(), &pb.GetCompactionProgressRequest{
		OperationId: "compact-op-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "compact-op-1", resp.OperationId)
	assert.Equal(t, 50.0, resp.PercentComplete)
}
