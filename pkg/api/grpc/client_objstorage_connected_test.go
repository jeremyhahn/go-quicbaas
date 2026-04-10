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

const objStorageBufSize = 1024 * 1024

// =============================================================================
// Minimal Mock ObjectStorageService Server
// =============================================================================

type minimalObjectStorageServer struct {
	pb.UnimplementedObjectStorageServiceServer
}

func (m *minimalObjectStorageServer) PutObject(_ context.Context, req *pb.PutObjectRequest) (*pb.PutObjectResponse, error) {
	return &pb.PutObjectResponse{
		Key:       req.Key,
		ObjectId:  1,
		Checksum:  []byte("sha256-test"),
		Size:      int64(len(req.Value)),
		Backend:   "local",
		Version:   1,
		CreatedAt: timestamppb.Now(),
	}, nil
}

func (m *minimalObjectStorageServer) GetObject(_ context.Context, req *pb.GetObjectRequest) (*pb.GetObjectResponse, error) {
	return &pb.GetObjectResponse{
		Key:         req.Key,
		Value:       []byte("test-value"),
		ContentType: "application/octet-stream",
		Size:        10,
		Checksum:    []byte("sha256-test"),
		Version:     1,
		Tags:        map[string]string{"env": "test"},
		Found:       true,
	}, nil
}

func (m *minimalObjectStorageServer) DeleteObject(_ context.Context, req *pb.DeleteObjectRequest) (*pb.DeleteObjectResponse, error) {
	return &pb.DeleteObjectResponse{
		Key:       req.Key,
		Deleted:   true,
		DeletedAt: timestamppb.Now(),
	}, nil
}

func (m *minimalObjectStorageServer) HeadObject(_ context.Context, req *pb.HeadObjectRequest) (*pb.HeadObjectResponse, error) {
	return &pb.HeadObjectResponse{
		Key:         req.Key,
		Exists:      true,
		ContentType: "text/plain",
		Size:        42,
		Version:     1,
	}, nil
}

func (m *minimalObjectStorageServer) ListObjects(_ context.Context, _ *pb.ListObjectsRequest) (*pb.ListObjectsResponse, error) {
	return &pb.ListObjectsResponse{
		Objects: []*pb.ObjectInfo{},
	}, nil
}

func (m *minimalObjectStorageServer) ListByTag(_ context.Context, _ *pb.ListByTagRequest) (*pb.ListByTagResponse, error) {
	return &pb.ListByTagResponse{
		Keys: []string{},
	}, nil
}

func (m *minimalObjectStorageServer) ListByTenant(_ context.Context, _ *pb.ListByTenantRequest) (*pb.ListByTenantResponse, error) {
	return &pb.ListByTenantResponse{
		Keys: []string{},
	}, nil
}

func (m *minimalObjectStorageServer) CopyObject(_ context.Context, req *pb.CopyObjectRequest) (*pb.CopyObjectResponse, error) {
	return &pb.CopyObjectResponse{
		Key:       req.DestKey,
		Version:   1,
		CreatedAt: timestamppb.Now(),
	}, nil
}

func (m *minimalObjectStorageServer) ReplicateObject(_ context.Context, req *pb.ReplicateObjectRequest) (*pb.ReplicateObjectResponse, error) {
	return &pb.ReplicateObjectResponse{
		Key:     req.Key,
		Success: true,
	}, nil
}

func (m *minimalObjectStorageServer) MigrateObject(_ context.Context, req *pb.MigrateObjectRequest) (*pb.MigrateObjectResponse, error) {
	return &pb.MigrateObjectResponse{
		Key:     req.Key,
		Success: true,
	}, nil
}

func (m *minimalObjectStorageServer) ArchiveObject(_ context.Context, req *pb.ArchiveObjectRequest) (*pb.ArchiveObjectResponse, error) {
	return &pb.ArchiveObjectResponse{
		Key:     req.Key,
		Success: true,
	}, nil
}

func (m *minimalObjectStorageServer) RestoreObject(_ context.Context, req *pb.RestoreObjectRequest) (*pb.RestoreObjectResponse, error) {
	return &pb.RestoreObjectResponse{
		Key:     req.Key,
		Success: true,
	}, nil
}

func (m *minimalObjectStorageServer) VerifyObject(_ context.Context, req *pb.VerifyObjectRequest) (*pb.VerifyObjectResponse, error) {
	return &pb.VerifyObjectResponse{
		Key:   req.Key,
		Valid: true,
	}, nil
}

func (m *minimalObjectStorageServer) SetObjectPolicy(_ context.Context, req *pb.SetObjectPolicyRequest) (*pb.SetObjectPolicyResponse, error) {
	return &pb.SetObjectPolicyResponse{
		Key: req.Key,
	}, nil
}

func (m *minimalObjectStorageServer) GetObjectPolicy(_ context.Context, req *pb.GetObjectPolicyRequest) (*pb.GetObjectPolicyResponse, error) {
	return &pb.GetObjectPolicyResponse{
		Key: req.Key,
	}, nil
}

func (m *minimalObjectStorageServer) ListBackends(_ context.Context, _ *pb.ListBackendsRequest) (*pb.ListBackendsResponse, error) {
	return &pb.ListBackendsResponse{
		Backends: []*pb.BackendInfo{},
	}, nil
}

func (m *minimalObjectStorageServer) GetBackendHealth(_ context.Context, req *pb.GetBackendHealthRequest) (*pb.GetBackendHealthResponse, error) {
	return &pb.GetBackendHealthResponse{
		Name:    req.Name,
		Healthy: true,
	}, nil
}

func (m *minimalObjectStorageServer) GetObjectStorageTenantUsage(_ context.Context, _ *pb.GetObjectStorageTenantUsageRequest) (*pb.GetObjectStorageTenantUsageResponse, error) {
	return &pb.GetObjectStorageTenantUsageResponse{}, nil
}

func (m *minimalObjectStorageServer) GetObjectStorageTenantQuotas(_ context.Context, _ *pb.GetObjectStorageTenantQuotasRequest) (*pb.GetObjectStorageTenantQuotasResponse, error) {
	return &pb.GetObjectStorageTenantQuotasResponse{}, nil
}

// =============================================================================
// Test Server Setup Helpers
// =============================================================================

func setupObjectStorageTestServer(t *testing.T) (*grpc.Server, *bufconn.Listener) {
	t.Helper()
	lis := bufconn.Listen(objStorageBufSize)
	server := grpc.NewServer()
	pb.RegisterObjectStorageServiceServer(server, &minimalObjectStorageServer{})
	go func() {
		if err := server.Serve(lis); err != nil {
			// Server stopped
		}
	}()
	return server, lis
}

func createObjectStorageTestConn(t *testing.T, lis *bufconn.Listener) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	return conn
}

func setupConnectedObjectStorageClient(t *testing.T) (*Client, *grpc.Server) {
	t.Helper()
	server, lis := setupObjectStorageTestServer(t)
	conn := createObjectStorageTestConn(t, lis)

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
// ObjectStorage Client Connected Tests
// =============================================================================

func TestClientObjectStoragePutObject_Connected(t *testing.T) {
	client, server := setupConnectedObjectStorageClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.ObjectStoragePutObject(context.Background(), &pb.PutObjectRequest{
		Key:   "test-key",
		Value: []byte("test-value"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "test-key", resp.Key)
	assert.Equal(t, uint64(1), resp.ObjectId)
}

func TestClientObjectStorageGetObject_Connected(t *testing.T) {
	client, server := setupConnectedObjectStorageClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.ObjectStorageGetObject(context.Background(), &pb.GetObjectRequest{Key: "test-key"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "test-key", resp.Key)
	assert.Equal(t, []byte("test-value"), resp.Value)
}

func TestClientObjectStorageDeleteObject_Connected(t *testing.T) {
	client, server := setupConnectedObjectStorageClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.ObjectStorageDeleteObject(context.Background(), &pb.DeleteObjectRequest{Key: "test-key"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Deleted)
}

func TestClientObjectStorageHeadObject_Connected(t *testing.T) {
	client, server := setupConnectedObjectStorageClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.ObjectStorageHeadObject(context.Background(), &pb.HeadObjectRequest{Key: "test-key"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Exists)
	assert.Equal(t, int64(42), resp.Size)
}

func TestClientObjectStorageListObjects_Connected(t *testing.T) {
	client, server := setupConnectedObjectStorageClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.ObjectStorageListObjects(context.Background(), &pb.ListObjectsRequest{Prefix: "test/"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestClientObjectStorageListByTag_Connected(t *testing.T) {
	client, server := setupConnectedObjectStorageClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.ObjectStorageListByTag(context.Background(), &pb.ListByTagRequest{Tag: "env:test"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestClientObjectStorageListByTenant_Connected(t *testing.T) {
	client, server := setupConnectedObjectStorageClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.ObjectStorageListByTenant(context.Background(), &pb.ListByTenantRequest{TenantId: "tenant-1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestClientObjectStorageCopyObject_Connected(t *testing.T) {
	client, server := setupConnectedObjectStorageClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.ObjectStorageCopyObject(context.Background(), &pb.CopyObjectRequest{SourceKey: "src", DestKey: "dst"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "dst", resp.Key)
}

func TestClientObjectStorageReplicateObject_Connected(t *testing.T) {
	client, server := setupConnectedObjectStorageClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.ObjectStorageReplicateObject(context.Background(), &pb.ReplicateObjectRequest{Key: "test-key"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestClientObjectStorageMigrateObject_Connected(t *testing.T) {
	client, server := setupConnectedObjectStorageClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.ObjectStorageMigrateObject(context.Background(), &pb.MigrateObjectRequest{Key: "test-key"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestClientObjectStorageArchiveObject_Connected(t *testing.T) {
	client, server := setupConnectedObjectStorageClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.ObjectStorageArchiveObject(context.Background(), &pb.ArchiveObjectRequest{Key: "test-key"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestClientObjectStorageRestoreObject_Connected(t *testing.T) {
	client, server := setupConnectedObjectStorageClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.ObjectStorageRestoreObject(context.Background(), &pb.RestoreObjectRequest{Key: "test-key"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestClientObjectStorageVerifyObject_Connected(t *testing.T) {
	client, server := setupConnectedObjectStorageClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.ObjectStorageVerifyObject(context.Background(), &pb.VerifyObjectRequest{Key: "test-key"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Valid)
}

func TestClientObjectStorageSetObjectPolicy_Connected(t *testing.T) {
	client, server := setupConnectedObjectStorageClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.ObjectStorageSetObjectPolicy(context.Background(), &pb.SetObjectPolicyRequest{Key: "test-key"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestClientObjectStorageGetObjectPolicy_Connected(t *testing.T) {
	client, server := setupConnectedObjectStorageClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.ObjectStorageGetObjectPolicy(context.Background(), &pb.GetObjectPolicyRequest{Key: "test-key"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestClientObjectStorageListBackends_Connected(t *testing.T) {
	client, server := setupConnectedObjectStorageClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.ObjectStorageListBackends(context.Background(), &pb.ListBackendsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestClientObjectStorageGetBackendHealth_Connected(t *testing.T) {
	client, server := setupConnectedObjectStorageClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.ObjectStorageGetBackendHealth(context.Background(), &pb.GetBackendHealthRequest{Name: "primary"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Healthy)
}

func TestClientObjectStorageGetTenantUsage_Connected(t *testing.T) {
	client, server := setupConnectedObjectStorageClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.ObjectStorageGetTenantUsage(context.Background(), &pb.GetObjectStorageTenantUsageRequest{TenantId: "t1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestClientObjectStorageGetTenantQuotas_Connected(t *testing.T) {
	client, server := setupConnectedObjectStorageClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.ObjectStorageGetTenantQuotas(context.Background(), &pb.GetObjectStorageTenantQuotasRequest{TenantId: "t1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}
