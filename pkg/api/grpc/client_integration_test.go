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

//go:build integration
// +build integration

package grpc

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// mockQuicBaaSServer is a mock implementation of the QuicBaaS gRPC server for testing
type mockQuicBaaSServer struct {
	proto.UnimplementedQuicBaaSServer
	putFunc           func(context.Context, *proto.PutRequest) (*proto.PutResponse, error)
	getFunc           func(context.Context, *proto.GetRequest) (*proto.GetResponse, error)
	deleteFunc        func(context.Context, *proto.DeleteRequest) (*proto.DeleteResponse, error)
	listFunc          func(*proto.ListRequest, grpc.ServerStreamingServer[proto.ListResponse]) error
	queryFunc         func(*proto.QueryRequest, grpc.ServerStreamingServer[proto.QueryResponse]) error
	clusterStatusFunc func(context.Context, *proto.ClusterStatusRequest) (*proto.ClusterStatusResponse, error)
	healthFunc        func(context.Context, *proto.HealthRequest) (*proto.HealthResponse, error)
}

func (m *mockQuicBaaSServer) Put(ctx context.Context, req *proto.PutRequest) (*proto.PutResponse, error) {
	if m.putFunc != nil {
		return m.putFunc(ctx, req)
	}
	return &proto.PutResponse{
		Id:        req.Id,
		Version:   1,
		Timestamp: time.Now().UnixNano(),
		LeaderId:  1,
	}, nil
}

func (m *mockQuicBaaSServer) Get(ctx context.Context, req *proto.GetRequest) (*proto.GetResponse, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, req)
	}
	return &proto.GetResponse{
		Id:        req.Id,
		Data:      []byte("test data"),
		Version:   1,
		Timestamp: time.Now().UnixNano(),
		Found:     true,
	}, nil
}

func (m *mockQuicBaaSServer) Delete(ctx context.Context, req *proto.DeleteRequest) (*proto.DeleteResponse, error) {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, req)
	}
	return &proto.DeleteResponse{
		Deleted:   true,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

func (m *mockQuicBaaSServer) List(req *proto.ListRequest, stream grpc.ServerStreamingServer[proto.ListResponse]) error {
	if m.listFunc != nil {
		return m.listFunc(req, stream)
	}

	// Send a single response with test data
	entities := []*proto.Entity{
		{
			Id:         1,
			EntityType: req.EntityType,
			Data:       []byte("entity 1"),
			Version:    1,
			Timestamp:  time.Now().UnixNano(),
		},
		{
			Id:         2,
			EntityType: req.EntityType,
			Data:       []byte("entity 2"),
			Version:    1,
			Timestamp:  time.Now().UnixNano(),
		},
	}

	return stream.Send(&proto.ListResponse{
		Entities:      entities,
		NextPageToken: "",
		TotalCount:    2,
	})
}

func (m *mockQuicBaaSServer) Query(req *proto.QueryRequest, stream grpc.ServerStreamingServer[proto.QueryResponse]) error {
	if m.queryFunc != nil {
		return m.queryFunc(req, stream)
	}

	entities := []*proto.Entity{
		{
			Id:         1,
			EntityType: req.EntityType,
			Data:       []byte("query result"),
			Version:    1,
			Timestamp:  time.Now().UnixNano(),
		},
	}

	return stream.Send(&proto.QueryResponse{
		Entities:   entities,
		TotalCount: 1,
	})
}

func (m *mockQuicBaaSServer) ClusterStatus(ctx context.Context, req *proto.ClusterStatusRequest) (*proto.ClusterStatusResponse, error) {
	if m.clusterStatusFunc != nil {
		return m.clusterStatusFunc(ctx, req)
	}
	return &proto.ClusterStatusResponse{
		ClusterId: "test-cluster",
		Nodes: []*proto.NodeInfo{
			{
				NodeId:       1,
				Address:      "localhost:50051",
				RaftAddress:  "localhost:9000",
				IsLeader:     true,
				HealthStatus: proto.HealthStatus_HEALTH_STATUS_HEALTHY,
				Version:      "1.0.0",
			},
		},
		Shards: []*proto.ShardInfo{
			{
				ShardId:     1,
				LeaderId:    1,
				Replicas:    []uint64{1},
				EntityTypes: []string{"user"},
				Status:      proto.HealthStatus_HEALTH_STATUS_HEALTHY,
			},
		},
		Timestamp: time.Now().UnixNano(),
	}, nil
}

func (m *mockQuicBaaSServer) Health(ctx context.Context, req *proto.HealthRequest) (*proto.HealthResponse, error) {
	if m.healthFunc != nil {
		return m.healthFunc(ctx, req)
	}
	return &proto.HealthResponse{
		Status:        proto.HealthStatus_HEALTH_STATUS_HEALTHY,
		NodeId:        1,
		Version:       "1.0.0",
		UptimeSeconds: 3600,
	}, nil
}

// setupTestServer creates a test gRPC server
func setupTestServer(t *testing.T, mock *mockQuicBaaSServer) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := grpc.NewServer()
	proto.RegisterQuicBaaSServer(server, mock)

	go func() {
		if err := server.Serve(listener); err != nil {
			t.Logf("server error: %v", err)
		}
	}()

	cleanup := func() {
		server.Stop()
		listener.Close()
	}

	return listener.Addr().String(), cleanup
}

// setupTestClient creates a test client with default options
func setupTestClient(t *testing.T, address string, opts ...ClientOption) (*Client, func()) {
	t.Helper()

	// Create test logger
	logConfig := logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	// Add logger to options
	allOpts := append([]ClientOption{
		WithLogger(log),
		WithTimeout(5 * time.Second),
	}, opts...)

	client, err := NewClient(address, allOpts...)
	require.NoError(t, err)

	cleanup := func() {
		client.Close()
	}

	return client, cleanup
}

func TestNewClient(t *testing.T) {
	mock := &mockQuicBaaSServer{}
	addr, cleanup := setupTestServer(t, mock)
	defer cleanup()

	client, clientCleanup := setupTestClient(t, addr)
	defer clientCleanup()

	assert.NotNil(t, client)
	assert.Equal(t, addr, client.address)
	assert.True(t, client.isConnected())
}

func TestClientConnectionManagement(t *testing.T) {
	t.Run("Disconnect", func(t *testing.T) {
		mock := &mockQuicBaaSServer{}
		addr, cleanup := setupTestServer(t, mock)
		defer cleanup()

		client, _ := setupTestClient(t, addr)

		assert.True(t, client.isConnected())

		err := client.Disconnect()
		assert.NoError(t, err)
		assert.False(t, client.isConnected())
	})

	t.Run("Close", func(t *testing.T) {
		mock := &mockQuicBaaSServer{}
		addr, cleanup := setupTestServer(t, mock)
		defer cleanup()

		client, _ := setupTestClient(t, addr)

		err := client.Close()
		assert.NoError(t, err)
		assert.False(t, client.isConnected())

		// Closing again should be safe
		err = client.Close()
		assert.NoError(t, err)
	})
}

func TestNewClient_ConnectionFailure(t *testing.T) {
	// Try to connect to a non-existent server
	client, err := NewClient("localhost:99999",
		WithTimeout(100*time.Millisecond),
		WithRetry(1, 10*time.Millisecond),
	)

	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "failed to connect")
}

func TestClient_ConnectWhenClosed(t *testing.T) {
	mock := &mockQuicBaaSServer{}
	addr, cleanup := setupTestServer(t, mock)
	defer cleanup()

	client, clientCleanup := setupTestClient(t, addr)
	clientCleanup() // This closes the client

	// Try to connect when closed
	err := client.Connect(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_EnsureConnectedReconnects(t *testing.T) {
	mock := &mockQuicBaaSServer{}
	addr, cleanup := setupTestServer(t, mock)
	defer cleanup()

	client, clientCleanup := setupTestClient(t, addr)
	defer clientCleanup()

	// Disconnect the client
	err := client.Disconnect()
	assert.NoError(t, err)
	assert.False(t, client.isConnected())

	// ensureConnected should reconnect
	err = client.ensureConnected(context.Background())
	assert.NoError(t, err)
	assert.True(t, client.isConnected())
}
