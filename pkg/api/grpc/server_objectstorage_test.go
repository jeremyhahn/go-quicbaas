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
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// Mock Object Storage Service
// =============================================================================

type mockObjectStorageService struct {
	putObjectFunc          func(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error)
	getObjectFunc          func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error)
	deleteObjectFunc       func(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error)
	headObjectFunc         func(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error)
	listObjectsFunc        func(ctx context.Context, req *service.ListObjectsRequest) (*service.ListObjectsResponse, error)
	listByTagFunc          func(ctx context.Context, req *service.ListByTagRequest) (*service.ListByTagResponse, error)
	listByTenantFunc       func(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error)
	copyObjectFunc         func(ctx context.Context, req *service.CopyObjectRequest) (*service.CopyObjectResponse, error)
	replicateObjectFunc    func(ctx context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error)
	migrateObjectFunc      func(ctx context.Context, req *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error)
	archiveObjectFunc      func(ctx context.Context, req *service.ArchiveObjectRequest) (*service.ArchiveObjectResponse, error)
	restoreObjectFunc      func(ctx context.Context, req *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error)
	verifyObjectFunc       func(ctx context.Context, req *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error)
	setObjectPolicyFunc    func(ctx context.Context, req *service.SetObjectPolicyRequest) (*service.SetObjectPolicyResponse, error)
	getObjectPolicyFunc    func(ctx context.Context, req *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error)
	listBackendsFunc       func(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error)
	getBackendHealthFunc   func(ctx context.Context, req *service.GetBackendHealthRequest) (*service.GetBackendHealthResponse, error)
	getTenantUsageFunc     func(ctx context.Context, req *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error)
	getTenantQuotasFunc    func(ctx context.Context, req *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error)
	setTenantQuotasFunc    func(ctx context.Context, req *service.SetTenantQuotasRequest) (*service.SetTenantQuotasResponse, error)
	batchPutObjectsFunc    func(ctx context.Context, req *service.BatchPutObjectsRequest) (*service.BatchPutObjectsResponse, error)
	batchGetObjectsFunc    func(ctx context.Context, req *service.BatchGetObjectsRequest) (*service.BatchGetObjectsResponse, error)
	batchDeleteObjectsFunc func(ctx context.Context, req *service.BatchDeleteObjectsRequest) (*service.BatchDeleteObjectsResponse, error)
}

func (m *mockObjectStorageService) PutObject(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
	if m.putObjectFunc != nil {
		return m.putObjectFunc(ctx, req)
	}
	return &service.PutObjectResponse{
		Key:       req.Key,
		ObjectID:  12345,
		Checksum:  []byte("checksum"),
		Size:      int64(len(req.Value)),
		Backend:   "local",
		Version:   1,
		CreatedAt: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) GetObject(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
	if m.getObjectFunc != nil {
		return m.getObjectFunc(ctx, req)
	}
	return &service.GetObjectResponse{
		Key:         req.Key,
		Value:       []byte("test data"),
		ContentType: "application/octet-stream",
		Size:        9,
		Checksum:    []byte("checksum"),
		Version:     1,
		Tags:        map[string]string{"env": "test"},
		TenantID:    req.TenantID,
		CreatedAt:   time.Now().UnixNano(),
		ModifiedAt:  time.Now().UnixNano(),
		Backend:     "local",
	}, nil
}

func (m *mockObjectStorageService) DeleteObject(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error) {
	if m.deleteObjectFunc != nil {
		return m.deleteObjectFunc(ctx, req)
	}
	return &service.DeleteObjectResponse{
		Key:       req.Key,
		Success:   true,
		Size:      100,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) HeadObject(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error) {
	if m.headObjectFunc != nil {
		return m.headObjectFunc(ctx, req)
	}
	return &service.HeadObjectResponse{
		Key:          req.Key,
		Exists:       true,
		ContentType:  "application/octet-stream",
		Size:         100,
		Checksum:     []byte("checksum"),
		Version:      1,
		Tags:         map[string]string{"env": "test"},
		TenantID:     req.TenantID,
		Status:       "active",
		CreatedAt:    time.Now().UnixNano(),
		ModifiedAt:   time.Now().UnixNano(),
		LastAccessAt: time.Now().UnixNano(),
		Backend:      "local",
		ReplicaCount: 2,
	}, nil
}

func (m *mockObjectStorageService) ListObjects(ctx context.Context, req *service.ListObjectsRequest) (*service.ListObjectsResponse, error) {
	if m.listObjectsFunc != nil {
		return m.listObjectsFunc(ctx, req)
	}
	return &service.ListObjectsResponse{
		Objects: []*service.ObjectSummary{
			{Key: "test/obj1", Size: 100, ContentType: "text/plain", Version: 1, Status: "active", CreatedAt: time.Now().UnixNano()},
			{Key: "test/obj2", Size: 200, ContentType: "text/plain", Version: 1, Status: "active", CreatedAt: time.Now().UnixNano()},
		},
		NextCursor: "cursor123",
		HasMore:    true,
		TotalCount: 10,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) ListByTag(ctx context.Context, req *service.ListByTagRequest) (*service.ListByTagResponse, error) {
	if m.listByTagFunc != nil {
		return m.listByTagFunc(ctx, req)
	}
	return &service.ListByTagResponse{
		Objects: []*service.ObjectSummary{
			{Key: "tagged/obj1", Size: 100, Status: "active"},
			{Key: "tagged/obj2", Size: 200, Status: "active"},
		},
		NextCursor: "cursor456",
		HasMore:    false,
		TotalCount: 2,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) ListByTenant(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error) {
	if m.listByTenantFunc != nil {
		return m.listByTenantFunc(ctx, req)
	}
	return &service.ListByTenantResponse{
		Objects: []*service.ObjectSummary{
			{Key: "tenant/obj1", Size: 100, Status: "active"},
			{Key: "tenant/obj2", Size: 200, Status: "active"},
		},
		NextCursor: "cursor789",
		HasMore:    false,
		TotalCount: 2,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) CopyObject(ctx context.Context, req *service.CopyObjectRequest) (*service.CopyObjectResponse, error) {
	if m.copyObjectFunc != nil {
		return m.copyObjectFunc(ctx, req)
	}
	return &service.CopyObjectResponse{
		Key:       req.DestinationKey,
		ObjectID:  12346,
		Size:      100,
		Checksum:  []byte("checksum"),
		Version:   1,
		Backend:   "local",
		CreatedAt: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) ReplicateObject(ctx context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error) {
	if m.replicateObjectFunc != nil {
		return m.replicateObjectFunc(ctx, req)
	}
	results := make(map[string]*service.ReplicationResult)
	for _, target := range req.Targets {
		results[target] = &service.ReplicationResult{
			Target:   target,
			Success:  true,
			Duration: 100,
		}
	}
	return &service.ReplicateObjectResponse{
		Key:       req.Key,
		Status:    "success",
		Results:   results,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) MigrateObject(ctx context.Context, req *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error) {
	if m.migrateObjectFunc != nil {
		return m.migrateObjectFunc(ctx, req)
	}
	return &service.MigrateObjectResponse{
		Key:               req.Key,
		SourceBackend:     "local",
		TargetBackend:     req.TargetBackend,
		Success:           true,
		DeletedFromSource: req.DeleteFromSource,
		Timestamp:         time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) ArchiveObject(ctx context.Context, req *service.ArchiveObjectRequest) (*service.ArchiveObjectResponse, error) {
	if m.archiveObjectFunc != nil {
		return m.archiveObjectFunc(ctx, req)
	}
	return &service.ArchiveObjectResponse{
		Key:                req.Key,
		ArchiveStore:       req.TargetStore,
		Success:            true,
		DeletedFromPrimary: req.DeleteFromPrimary,
		ArchivedAt:         time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) RestoreObject(ctx context.Context, req *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error) {
	if m.restoreObjectFunc != nil {
		return m.restoreObjectFunc(ctx, req)
	}
	return &service.RestoreObjectResponse{
		Key:        req.Key,
		Backend:    req.TargetBackend,
		Success:    true,
		RestoredAt: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) VerifyObject(ctx context.Context, req *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error) {
	if m.verifyObjectFunc != nil {
		return m.verifyObjectFunc(ctx, req)
	}
	return &service.VerifyObjectResponse{
		Key:   req.Key,
		Valid: true,
		Results: map[string]*service.VerificationResult{
			"local": {Store: "local", Valid: true, ChecksumMatch: true, SizeMatch: true},
			"s3":    {Store: "s3", Valid: true, ChecksumMatch: true, SizeMatch: true},
		},
		Repaired:  false,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) SetObjectPolicy(ctx context.Context, req *service.SetObjectPolicyRequest) (*service.SetObjectPolicyResponse, error) {
	if m.setObjectPolicyFunc != nil {
		return m.setObjectPolicyFunc(ctx, req)
	}
	return &service.SetObjectPolicyResponse{
		Key:       req.Key,
		Success:   true,
		Version:   2,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) GetObjectPolicy(ctx context.Context, req *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error) {
	if m.getObjectPolicyFunc != nil {
		return m.getObjectPolicyFunc(ctx, req)
	}
	return &service.GetObjectPolicyResponse{
		Key: req.Key,
		ReplicationPolicy: &catalog.ReplicationPolicy{
			Enabled:      true,
			MinReplicas:  2,
			TargetStores: []string{"s3", "gcs"},
			Mode:         catalog.ReplicationModeAsync,
			Priority:     5,
		},
		ArchivePolicy: &catalog.ArchivePolicy{
			Enabled:           true,
			ArchiveAfterDays:  90,
			TargetStore:       "glacier",
			DeleteFromPrimary: false,
			CompressionLevel:  6,
		},
		RetentionPolicy: &catalog.RetentionPolicy{
			Enabled:       true,
			RetentionDays: 365,
			MaxVersions:   10,
		},
		Timestamp: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) ListBackends(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error) {
	if m.listBackendsFunc != nil {
		return m.listBackendsFunc(ctx, req)
	}
	return &service.ListBackendsResponse{
		Backends: []*service.BackendInfo{
			{Name: "local", Type: "local", Enabled: true, Tier: "hot", ObjectCount: 1000, TotalSize: 1000000},
			{Name: "s3", Type: "s3", Enabled: true, Tier: "warm", Region: "us-east-1", ObjectCount: 5000, TotalSize: 5000000},
		},
		Timestamp: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) GetBackendHealth(ctx context.Context, req *service.GetBackendHealthRequest) (*service.GetBackendHealthResponse, error) {
	if m.getBackendHealthFunc != nil {
		return m.getBackendHealthFunc(ctx, req)
	}
	return &service.GetBackendHealthResponse{
		Name:      req.Name,
		Healthy:   true,
		Latency:   15,
		LastCheck: time.Now().UnixNano(),
		Status:    "operational",
		Error:     "",
	}, nil
}

func (m *mockObjectStorageService) GetTenantUsage(ctx context.Context, req *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error) {
	if m.getTenantUsageFunc != nil {
		return m.getTenantUsageFunc(ctx, req)
	}
	return &service.GetTenantUsageResponse{
		TenantID:      req.TenantID,
		ObjectCount:   1000,
		TotalSize:     10000000,
		ActiveCount:   900,
		ArchivedCount: 100,
		Timestamp:     time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) GetTenantQuotas(ctx context.Context, req *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error) {
	if m.getTenantQuotasFunc != nil {
		return m.getTenantQuotasFunc(ctx, req)
	}
	return &service.GetTenantQuotasResponse{
		TenantID:         req.TenantID,
		MaxStorageBytes:  1000000000,
		MaxObjectSize:    100000000,
		MaxObjectCount:   10000,
		UsedStorageBytes: 10000000,
		UsedObjectCount:  1000,
		Timestamp:        time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) SetTenantQuotas(ctx context.Context, req *service.SetTenantQuotasRequest) (*service.SetTenantQuotasResponse, error) {
	if m.setTenantQuotasFunc != nil {
		return m.setTenantQuotasFunc(ctx, req)
	}
	return &service.SetTenantQuotasResponse{
		TenantID:  req.TenantID,
		UpdatedAt: time.Now().UnixNano(),
	}, nil
}

func (m *mockObjectStorageService) BatchPutObjects(ctx context.Context, req *service.BatchPutObjectsRequest) (*service.BatchPutObjectsResponse, error) {
	if m.batchPutObjectsFunc != nil {
		return m.batchPutObjectsFunc(ctx, req)
	}
	return &service.BatchPutObjectsResponse{
		Results:      make([]*service.BatchPutResult, 0),
		SuccessCount: 0,
		FailureCount: 0,
	}, nil
}

func (m *mockObjectStorageService) BatchGetObjects(ctx context.Context, req *service.BatchGetObjectsRequest) (*service.BatchGetObjectsResponse, error) {
	if m.batchGetObjectsFunc != nil {
		return m.batchGetObjectsFunc(ctx, req)
	}
	return &service.BatchGetObjectsResponse{
		Results:       make([]*service.BatchGetResult, 0),
		FoundCount:    0,
		NotFoundCount: 0,
	}, nil
}

func (m *mockObjectStorageService) BatchDeleteObjects(ctx context.Context, req *service.BatchDeleteObjectsRequest) (*service.BatchDeleteObjectsResponse, error) {
	if m.batchDeleteObjectsFunc != nil {
		return m.batchDeleteObjectsFunc(ctx, req)
	}
	return &service.BatchDeleteObjectsResponse{
		Results:      make([]*service.BatchDeleteResult, 0),
		DeletedCount: 0,
		FailureCount: 0,
	}, nil
}

// =============================================================================
// Mock Stream for StreamGetObject
// =============================================================================

type mockStreamGetObjectServer struct {
	ctx       context.Context
	chunks    []*pb.ObjectChunk
	sendError error
}

func (m *mockStreamGetObjectServer) Send(chunk *pb.ObjectChunk) error {
	if m.sendError != nil {
		return m.sendError
	}
	m.chunks = append(m.chunks, chunk)
	return nil
}

func (m *mockStreamGetObjectServer) SetHeader(md metadata.MD) error  { return nil }
func (m *mockStreamGetObjectServer) SendHeader(md metadata.MD) error { return nil }
func (m *mockStreamGetObjectServer) SetTrailer(md metadata.MD)       {}
func (m *mockStreamGetObjectServer) Context() context.Context        { return m.ctx }
func (m *mockStreamGetObjectServer) SendMsg(msg interface{}) error   { return nil }
func (m *mockStreamGetObjectServer) RecvMsg(msg interface{}) error   { return nil }

// =============================================================================
// Mock Stream for StreamPutObject
// =============================================================================

type mockStreamPutObjectServer struct {
	ctx          context.Context
	chunks       []*pb.PutObjectChunk
	chunkIndex   int
	response     *pb.PutObjectResponse
	recvError    error
	sendCloseErr error
}

func (m *mockStreamPutObjectServer) Recv() (*pb.PutObjectChunk, error) {
	if m.recvError != nil {
		return nil, m.recvError
	}
	if m.chunkIndex >= len(m.chunks) {
		return nil, io.EOF
	}
	chunk := m.chunks[m.chunkIndex]
	m.chunkIndex++
	return chunk, nil
}

func (m *mockStreamPutObjectServer) SendAndClose(resp *pb.PutObjectResponse) error {
	if m.sendCloseErr != nil {
		return m.sendCloseErr
	}
	m.response = resp
	return nil
}

func (m *mockStreamPutObjectServer) SetHeader(md metadata.MD) error  { return nil }
func (m *mockStreamPutObjectServer) SendHeader(md metadata.MD) error { return nil }
func (m *mockStreamPutObjectServer) SetTrailer(md metadata.MD)       {}
func (m *mockStreamPutObjectServer) Context() context.Context        { return m.ctx }
func (m *mockStreamPutObjectServer) SendMsg(msg interface{}) error   { return nil }
func (m *mockStreamPutObjectServer) RecvMsg(msg interface{}) error   { return nil }

// =============================================================================
// Mock gRPC Server for RegisterObjectStorageServer
// =============================================================================

// =============================================================================
// Test Cases - Server Creation and Registration
// =============================================================================

func TestNewObjectStorageServer(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	server := NewObjectStorageServer(mockSvc)
	assert.NotNil(t, server)
}

func TestNewObjectStorageServer_NilService(t *testing.T) {
	server := NewObjectStorageServer(nil)
	assert.NotNil(t, server)
}

// =============================================================================
// Test Cases - PutObject
// =============================================================================

func TestPutObject_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	server := NewObjectStorageServer(mockSvc).(*objectStorageServer)

	req := &pb.PutObjectRequest{
		Key:         "test/object1",
		Value:       []byte("test data"),
		ContentType: "text/plain",
		TenantId:    "tenant1",
		Tags:        map[string]string{"env": "test"},
	}

	resp, err := server.PutObject(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.Equal(t, req.Key, resp.Key)
	assert.NotZero(t, resp.ObjectId)
	assert.NotEmpty(t, resp.Checksum)
}

// =============================================================================
// Test Cases - GetObject
// =============================================================================

func TestGetObject_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	server := NewObjectStorageServer(mockSvc).(*objectStorageServer)

	req := &pb.GetObjectRequest{
		Key:      "test/object1",
		TenantId: "tenant1",
	}

	resp, err := server.GetObject(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.Equal(t, req.Key, resp.Key)
	assert.NotEmpty(t, resp.Value)
}

// =============================================================================
// Test Cases - DeleteObject
// =============================================================================

func TestDeleteObject_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	server := NewObjectStorageServer(mockSvc).(*objectStorageServer)

	req := &pb.DeleteObjectRequest{
		Key:      "test/object1",
		TenantId: "tenant1",
	}

	resp, err := server.DeleteObject(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.Equal(t, req.Key, resp.Key)
	assert.True(t, resp.Deleted)
}

// =============================================================================
// Test Cases - HeadObject
// =============================================================================

func TestHeadObject_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	server := NewObjectStorageServer(mockSvc).(*objectStorageServer)

	req := &pb.HeadObjectRequest{
		Key:      "test/object1",
		TenantId: "tenant1",
	}

	resp, err := server.HeadObject(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.Equal(t, req.Key, resp.Key)
	assert.True(t, resp.Exists)
}

// =============================================================================
// Test Cases - ListObjects
// =============================================================================

func TestListObjects_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	server := NewObjectStorageServer(mockSvc).(*objectStorageServer)

	req := &pb.ListObjectsRequest{
		Prefix: "test/",
		Limit:  10,
	}

	resp, err := server.ListObjects(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.NotNil(t, resp.Objects)
}

// =============================================================================
// Test Cases - BatchPutObjects
// =============================================================================

// =============================================================================
// Test Cases - BatchGetObjects
// =============================================================================

// =============================================================================
// Test Cases - BatchDeleteObjects
// =============================================================================

// Silence unused imports
var (
	_ = fmt.Sprint
	_ = catalog.ReplicationModeAsync
)

// --- Merged from remaining_coverage_91_test.go ---

// =============================================================================
// Server ObjectStorage - Nil Request and Nil Service Tests
// =============================================================================

func TestObjectStorageServer_NilRequest(t *testing.T) {
	srv := &objectStorageServer{service: &mockObjectStorageService{}}
	ctx := tenantctx.WithSystem(context.Background())

	tests := []struct {
		name string
		call func() error
	}{
		{"PutObject", func() error { _, err := srv.PutObject(ctx, nil); return err }},
		{"GetObject", func() error { _, err := srv.GetObject(ctx, nil); return err }},
		{"DeleteObject", func() error { _, err := srv.DeleteObject(ctx, nil); return err }},
		{"HeadObject", func() error { _, err := srv.HeadObject(ctx, nil); return err }},
		{"ListObjects", func() error { _, err := srv.ListObjects(ctx, nil); return err }},
		{"ListByTag", func() error { _, err := srv.ListByTag(ctx, nil); return err }},
		{"ListByTenant", func() error { _, err := srv.ListByTenant(ctx, nil); return err }},
		{"CopyObject", func() error { _, err := srv.CopyObject(ctx, nil); return err }},
		{"ReplicateObject", func() error { _, err := srv.ReplicateObject(ctx, nil); return err }},
		{"MigrateObject", func() error { _, err := srv.MigrateObject(ctx, nil); return err }},
		{"ArchiveObject", func() error { _, err := srv.ArchiveObject(ctx, nil); return err }},
		{"RestoreObject", func() error { _, err := srv.RestoreObject(ctx, nil); return err }},
		{"VerifyObject", func() error { _, err := srv.VerifyObject(ctx, nil); return err }},
		{"SetObjectPolicy", func() error { _, err := srv.SetObjectPolicy(ctx, nil); return err }},
		{"GetObjectPolicy", func() error { _, err := srv.GetObjectPolicy(ctx, nil); return err }},
		{"ListBackends", func() error { _, err := srv.ListBackends(ctx, nil); return err }},
		{"GetBackendHealth", func() error { _, err := srv.GetBackendHealth(ctx, nil); return err }},
		{"GetTenantUsage", func() error { _, err := srv.GetObjectStorageTenantUsage(ctx, nil); return err }},
		{"GetTenantQuotas", func() error { _, err := srv.GetObjectStorageTenantQuotas(ctx, nil); return err }},
		{"SetTenantQuotas", func() error { _, err := srv.SetObjectStorageTenantQuotas(ctx, nil); return err }},
		{"BatchPutObjects", func() error { _, err := srv.BatchPutObjects(ctx, nil); return err }},
		{"BatchDeleteObjects", func() error { _, err := srv.BatchDeleteObjects(ctx, nil); return err }},
		{"BatchGetObjects", func() error { _, err := srv.BatchGetObjects(ctx, nil); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func TestObjectStorageServer_NilService(t *testing.T) {
	srv := &objectStorageServer{service: nil}
	ctx := tenantctx.WithSystem(context.Background())

	tests := []struct {
		name string
		call func() error
	}{
		{"PutObject", func() error { _, err := srv.PutObject(ctx, &pb.PutObjectRequest{Key: "k"}); return err }},
		{"GetObject", func() error { _, err := srv.GetObject(ctx, &pb.GetObjectRequest{Key: "k"}); return err }},
		{"DeleteObject", func() error { _, err := srv.DeleteObject(ctx, &pb.DeleteObjectRequest{Key: "k"}); return err }},
		{"HeadObject", func() error { _, err := srv.HeadObject(ctx, &pb.HeadObjectRequest{Key: "k"}); return err }},
		{"ListObjects", func() error { _, err := srv.ListObjects(ctx, &pb.ListObjectsRequest{}); return err }},
		{"ListByTag", func() error { _, err := srv.ListByTag(ctx, &pb.ListByTagRequest{Tag: "t"}); return err }},
		{"ListByTenant", func() error { _, err := srv.ListByTenant(ctx, &pb.ListByTenantRequest{TenantId: "t1"}); return err }},
		{"CopyObject", func() error {
			_, err := srv.CopyObject(ctx, &pb.CopyObjectRequest{SourceKey: "a", DestKey: "b"})
			return err
		}},
		{"ReplicateObject", func() error {
			_, err := srv.ReplicateObject(ctx, &pb.ReplicateObjectRequest{Key: "k", TargetStores: []string{"s1"}})
			return err
		}},
		{"MigrateObject", func() error {
			_, err := srv.MigrateObject(ctx, &pb.MigrateObjectRequest{Key: "k", TargetBackend: "b"})
			return err
		}},
		{"ArchiveObject", func() error { _, err := srv.ArchiveObject(ctx, &pb.ArchiveObjectRequest{Key: "k"}); return err }},
		{"RestoreObject", func() error { _, err := srv.RestoreObject(ctx, &pb.RestoreObjectRequest{Key: "k"}); return err }},
		{"VerifyObject", func() error { _, err := srv.VerifyObject(ctx, &pb.VerifyObjectRequest{Key: "k"}); return err }},
		{"SetObjectPolicy", func() error { _, err := srv.SetObjectPolicy(ctx, &pb.SetObjectPolicyRequest{Key: "k"}); return err }},
		{"GetObjectPolicy", func() error { _, err := srv.GetObjectPolicy(ctx, &pb.GetObjectPolicyRequest{Key: "k"}); return err }},
		{"ListBackends", func() error { _, err := srv.ListBackends(ctx, &pb.ListBackendsRequest{}); return err }},
		{"GetBackendHealth", func() error {
			_, err := srv.GetBackendHealth(ctx, &pb.GetBackendHealthRequest{Name: "n"})
			return err
		}},
		{"GetTenantUsage", func() error {
			_, err := srv.GetObjectStorageTenantUsage(ctx, &pb.GetObjectStorageTenantUsageRequest{TenantId: "t1"})
			return err
		}},
		{"GetTenantQuotas", func() error {
			_, err := srv.GetObjectStorageTenantQuotas(ctx, &pb.GetObjectStorageTenantQuotasRequest{TenantId: "t1"})
			return err
		}},
		{"SetTenantQuotas", func() error {
			_, err := srv.SetObjectStorageTenantQuotas(ctx, &pb.SetObjectStorageTenantQuotasRequest{
				Quotas: &pb.ObjectStorageTenantQuotas{TenantId: "t1"},
			})
			return err
		}},
		{"BatchPutObjects", func() error {
			_, err := srv.BatchPutObjects(ctx, &pb.BatchPutObjectsRequest{Objects: []*pb.PutObjectRequest{{Key: "k"}}})
			return err
		}},
		{"BatchDeleteObjects", func() error {
			_, err := srv.BatchDeleteObjects(ctx, &pb.BatchDeleteObjectsRequest{Keys: []string{"k"}})
			return err
		}},
		{"BatchGetObjects", func() error {
			_, err := srv.BatchGetObjects(ctx, &pb.BatchGetObjectsRequest{Keys: []string{"k"}})
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			require.Error(t, err)
			assert.Equal(t, codes.Unavailable, status.Code(err))
		})
	}
}

func TestObjectStorageServer_ValidationErrors(t *testing.T) {
	srv := &objectStorageServer{service: &mockObjectStorageService{}}
	ctx := tenantctx.WithSystem(context.Background())

	tests := []struct {
		name string
		call func() error
	}{
		{"PutObject_EmptyKey", func() error { _, err := srv.PutObject(ctx, &pb.PutObjectRequest{Key: ""}); return err }},
		{"GetObject_EmptyKey", func() error { _, err := srv.GetObject(ctx, &pb.GetObjectRequest{Key: ""}); return err }},
		{"DeleteObject_EmptyKey", func() error { _, err := srv.DeleteObject(ctx, &pb.DeleteObjectRequest{Key: ""}); return err }},
		{"HeadObject_EmptyKey", func() error { _, err := srv.HeadObject(ctx, &pb.HeadObjectRequest{Key: ""}); return err }},
		{"ListByTag_EmptyTag", func() error { _, err := srv.ListByTag(ctx, &pb.ListByTagRequest{Tag: ""}); return err }},
		{"ListByTenant_EmptyTenantID", func() error {
			_, err := srv.ListByTenant(ctx, &pb.ListByTenantRequest{TenantId: ""})
			return err
		}},
		{"CopyObject_EmptySourceKey", func() error {
			_, err := srv.CopyObject(ctx, &pb.CopyObjectRequest{SourceKey: "", DestKey: "b"})
			return err
		}},
		{"CopyObject_EmptyDestKey", func() error {
			_, err := srv.CopyObject(ctx, &pb.CopyObjectRequest{SourceKey: "a", DestKey: ""})
			return err
		}},
		{"ReplicateObject_EmptyKey", func() error {
			_, err := srv.ReplicateObject(ctx, &pb.ReplicateObjectRequest{Key: "", TargetStores: []string{"s1"}})
			return err
		}},
		{"ReplicateObject_NoTargets", func() error {
			_, err := srv.ReplicateObject(ctx, &pb.ReplicateObjectRequest{Key: "k", TargetStores: nil})
			return err
		}},
		{"MigrateObject_EmptyKey", func() error {
			_, err := srv.MigrateObject(ctx, &pb.MigrateObjectRequest{Key: "", TargetBackend: "b"})
			return err
		}},
		{"MigrateObject_EmptyTargetBackend", func() error {
			_, err := srv.MigrateObject(ctx, &pb.MigrateObjectRequest{Key: "k", TargetBackend: ""})
			return err
		}},
		{"ArchiveObject_EmptyKey", func() error { _, err := srv.ArchiveObject(ctx, &pb.ArchiveObjectRequest{Key: ""}); return err }},
		{"RestoreObject_EmptyKey", func() error { _, err := srv.RestoreObject(ctx, &pb.RestoreObjectRequest{Key: ""}); return err }},
		{"VerifyObject_EmptyKey", func() error { _, err := srv.VerifyObject(ctx, &pb.VerifyObjectRequest{Key: ""}); return err }},
		{"SetObjectPolicy_EmptyKey", func() error { _, err := srv.SetObjectPolicy(ctx, &pb.SetObjectPolicyRequest{Key: ""}); return err }},
		{"GetObjectPolicy_EmptyKey", func() error { _, err := srv.GetObjectPolicy(ctx, &pb.GetObjectPolicyRequest{Key: ""}); return err }},
		{"GetBackendHealth_EmptyName", func() error {
			_, err := srv.GetBackendHealth(ctx, &pb.GetBackendHealthRequest{Name: ""})
			return err
		}},
		{"GetTenantUsage_EmptyTenantID", func() error {
			_, err := srv.GetObjectStorageTenantUsage(ctx, &pb.GetObjectStorageTenantUsageRequest{TenantId: ""})
			return err
		}},
		{"GetTenantQuotas_EmptyTenantID", func() error {
			_, err := srv.GetObjectStorageTenantQuotas(ctx, &pb.GetObjectStorageTenantQuotasRequest{TenantId: ""})
			return err
		}},
		{"SetTenantQuotas_NilQuotas", func() error {
			_, err := srv.SetObjectStorageTenantQuotas(ctx, &pb.SetObjectStorageTenantQuotasRequest{Quotas: nil})
			return err
		}},
		{"SetTenantQuotas_EmptyTenantID", func() error {
			_, err := srv.SetObjectStorageTenantQuotas(ctx, &pb.SetObjectStorageTenantQuotasRequest{
				Quotas: &pb.ObjectStorageTenantQuotas{TenantId: ""},
			})
			return err
		}},
		{"BatchPutObjects_Empty", func() error {
			_, err := srv.BatchPutObjects(ctx, &pb.BatchPutObjectsRequest{Objects: nil})
			return err
		}},
		{"BatchDeleteObjects_Empty", func() error {
			_, err := srv.BatchDeleteObjects(ctx, &pb.BatchDeleteObjectsRequest{Keys: nil})
			return err
		}},
		{"BatchGetObjects_Empty", func() error {
			_, err := srv.BatchGetObjects(ctx, &pb.BatchGetObjectsRequest{Keys: nil})
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func TestObjectStorageServer_ServiceErrors(t *testing.T) {
	svcErr := &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test", Err: dberrors.New(dberrors.ErrNotFound, "ObjectStorage", "not found")}

	mockSvc := &mockObjectStorageService{
		putObjectFunc: func(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
			return nil, svcErr
		},
		getObjectFunc: func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return nil, svcErr
		},
		deleteObjectFunc: func(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error) {
			return nil, svcErr
		},
		headObjectFunc: func(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error) {
			return nil, svcErr
		},
		listObjectsFunc: func(ctx context.Context, req *service.ListObjectsRequest) (*service.ListObjectsResponse, error) {
			return nil, svcErr
		},
		listByTagFunc: func(ctx context.Context, req *service.ListByTagRequest) (*service.ListByTagResponse, error) {
			return nil, svcErr
		},
		listByTenantFunc: func(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error) {
			return nil, svcErr
		},
		copyObjectFunc: func(ctx context.Context, req *service.CopyObjectRequest) (*service.CopyObjectResponse, error) {
			return nil, svcErr
		},
		replicateObjectFunc: func(ctx context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error) {
			return nil, svcErr
		},
		migrateObjectFunc: func(ctx context.Context, req *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error) {
			return nil, svcErr
		},
		archiveObjectFunc: func(ctx context.Context, req *service.ArchiveObjectRequest) (*service.ArchiveObjectResponse, error) {
			return nil, svcErr
		},
		restoreObjectFunc: func(ctx context.Context, req *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error) {
			return nil, svcErr
		},
		verifyObjectFunc: func(ctx context.Context, req *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error) {
			return nil, svcErr
		},
		setObjectPolicyFunc: func(ctx context.Context, req *service.SetObjectPolicyRequest) (*service.SetObjectPolicyResponse, error) {
			return nil, svcErr
		},
		getObjectPolicyFunc: func(ctx context.Context, req *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error) {
			return nil, svcErr
		},
		listBackendsFunc: func(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error) {
			return nil, svcErr
		},
		getBackendHealthFunc: func(ctx context.Context, req *service.GetBackendHealthRequest) (*service.GetBackendHealthResponse, error) {
			return nil, svcErr
		},
		getTenantUsageFunc: func(ctx context.Context, req *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error) {
			return nil, svcErr
		},
		getTenantQuotasFunc: func(ctx context.Context, req *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error) {
			return nil, svcErr
		},
	}

	srv := &objectStorageServer{service: mockSvc}
	ctx := tenantctx.WithSystem(context.Background())

	tests := []struct {
		name string
		call func() error
	}{
		{"PutObject", func() error { _, err := srv.PutObject(ctx, &pb.PutObjectRequest{Key: "k"}); return err }},
		{"GetObject", func() error { _, err := srv.GetObject(ctx, &pb.GetObjectRequest{Key: "k"}); return err }},
		{"DeleteObject", func() error { _, err := srv.DeleteObject(ctx, &pb.DeleteObjectRequest{Key: "k"}); return err }},
		{"HeadObject", func() error { _, err := srv.HeadObject(ctx, &pb.HeadObjectRequest{Key: "k"}); return err }},
		{"ListObjects", func() error { _, err := srv.ListObjects(ctx, &pb.ListObjectsRequest{}); return err }},
		{"ListByTag", func() error { _, err := srv.ListByTag(ctx, &pb.ListByTagRequest{Tag: "t"}); return err }},
		{"ListByTenant", func() error {
			_, err := srv.ListByTenant(ctx, &pb.ListByTenantRequest{TenantId: "t1"})
			return err
		}},
		{"CopyObject", func() error {
			_, err := srv.CopyObject(ctx, &pb.CopyObjectRequest{SourceKey: "a", DestKey: "b"})
			return err
		}},
		{"ReplicateObject", func() error {
			_, err := srv.ReplicateObject(ctx, &pb.ReplicateObjectRequest{Key: "k", TargetStores: []string{"s1"}})
			return err
		}},
		{"MigrateObject", func() error {
			_, err := srv.MigrateObject(ctx, &pb.MigrateObjectRequest{Key: "k", TargetBackend: "b"})
			return err
		}},
		{"ArchiveObject", func() error { _, err := srv.ArchiveObject(ctx, &pb.ArchiveObjectRequest{Key: "k"}); return err }},
		{"RestoreObject", func() error { _, err := srv.RestoreObject(ctx, &pb.RestoreObjectRequest{Key: "k"}); return err }},
		{"VerifyObject", func() error { _, err := srv.VerifyObject(ctx, &pb.VerifyObjectRequest{Key: "k"}); return err }},
		{"SetObjectPolicy", func() error { _, err := srv.SetObjectPolicy(ctx, &pb.SetObjectPolicyRequest{Key: "k"}); return err }},
		{"GetObjectPolicy", func() error { _, err := srv.GetObjectPolicy(ctx, &pb.GetObjectPolicyRequest{Key: "k"}); return err }},
		{"ListBackends", func() error { _, err := srv.ListBackends(ctx, &pb.ListBackendsRequest{}); return err }},
		{"GetBackendHealth", func() error {
			_, err := srv.GetBackendHealth(ctx, &pb.GetBackendHealthRequest{Name: "n"})
			return err
		}},
		{"GetTenantUsage", func() error {
			_, err := srv.GetObjectStorageTenantUsage(ctx, &pb.GetObjectStorageTenantUsageRequest{TenantId: "t1"})
			return err
		}},
		{"GetTenantQuotas", func() error {
			_, err := srv.GetObjectStorageTenantQuotas(ctx, &pb.GetObjectStorageTenantQuotasRequest{TenantId: "t1"})
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			require.Error(t, err)
			assert.Equal(t, codes.NotFound, status.Code(err))
		})
	}
}

// =============================================================================
// Server ObjectStorage - Successful operations with detailed assertions
// =============================================================================

func TestGRPCRemaining91_ObjectStorageServer_ArchiveObject_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		archiveObjectFunc: func(ctx context.Context, req *service.ArchiveObjectRequest) (*service.ArchiveObjectResponse, error) {
			return &service.ArchiveObjectResponse{
				Key:          req.Key,
				ArchiveStore: "cold-store",
				Success:      true,
				ArchivedAt:   time.Now().UnixNano(),
			}, nil
		},
	}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.ArchiveObject(tenantctx.WithSystem(context.Background()), &pb.ArchiveObjectRequest{
		Key:               "test-key",
		TargetStore:       "cold-store",
		DeleteFromPrimary: true,
	})

	require.NoError(t, err)
	assert.Equal(t, "test-key", resp.Key)
	assert.Equal(t, "cold-store", resp.ArchiveStore)
	assert.True(t, resp.Success)
}

func TestGRPCRemaining91_ObjectStorageServer_RestoreObject_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		restoreObjectFunc: func(ctx context.Context, req *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error) {
			return &service.RestoreObjectResponse{
				Key:     req.Key,
				Backend: "hot-store",
				Success: true,
			}, nil
		},
	}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.RestoreObject(tenantctx.WithSystem(context.Background()), &pb.RestoreObjectRequest{
		Key:           "test-key",
		TargetBackend: "hot-store",
	})

	require.NoError(t, err)
	assert.Equal(t, "test-key", resp.Key)
	assert.True(t, resp.Success)
}

func TestGRPCRemaining91_ObjectStorageServer_VerifyObject_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		verifyObjectFunc: func(ctx context.Context, req *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error) {
			return &service.VerifyObjectResponse{
				Key:      req.Key,
				Valid:    true,
				Repaired: false,
				Results: map[string]*service.VerificationResult{
					"local": {Store: "local", Valid: true, ChecksumMatch: true},
					"s3":    {Store: "s3", Valid: true, ChecksumMatch: true},
				},
			}, nil
		},
	}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.VerifyObject(tenantctx.WithSystem(context.Background()), &pb.VerifyObjectRequest{
		Key:    "test-key",
		Repair: false,
	})

	require.NoError(t, err)
	assert.Equal(t, "test-key", resp.Key)
	assert.True(t, resp.Valid)
	assert.Len(t, resp.Locations, 2)
}

func TestGRPCRemaining91_ObjectStorageServer_SetObjectPolicy_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		setObjectPolicyFunc: func(ctx context.Context, req *service.SetObjectPolicyRequest) (*service.SetObjectPolicyResponse, error) {
			return &service.SetObjectPolicyResponse{
				Key:       req.Key,
				Version:   2,
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
	}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.SetObjectPolicy(tenantctx.WithSystem(context.Background()), &pb.SetObjectPolicyRequest{
		Key: "test-key",
		ReplicationPolicy: &pb.ReplicationPolicy{
			Enabled:     true,
			MinReplicas: 3,
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "test-key", resp.Key)
	assert.Equal(t, uint64(2), resp.Version)
}

func TestGRPCRemaining91_ObjectStorageServer_GetObjectPolicy_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		getObjectPolicyFunc: func(ctx context.Context, req *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error) {
			return &service.GetObjectPolicyResponse{
				Key:       req.Key,
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
	}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.GetObjectPolicy(tenantctx.WithSystem(context.Background()), &pb.GetObjectPolicyRequest{Key: "test-key"})

	require.NoError(t, err)
	assert.Equal(t, "test-key", resp.Key)
}

func TestGRPCRemaining91_ObjectStorageServer_ListBackends_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		listBackendsFunc: func(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error) {
			return &service.ListBackendsResponse{
				Backends: []*service.BackendInfo{
					{Name: "local", Type: "local", Enabled: true, Tier: "hot"},
					{Name: "s3-us-east", Type: "s3", Enabled: true, Tier: "warm"},
				},
			}, nil
		},
	}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.ListBackends(tenantctx.WithSystem(context.Background()), &pb.ListBackendsRequest{IncludeHealth: true})

	require.NoError(t, err)
	assert.Len(t, resp.Backends, 2)
	assert.Equal(t, int32(2), resp.Total)
}

func TestGRPCRemaining91_ObjectStorageServer_GetBackendHealth_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		getBackendHealthFunc: func(ctx context.Context, req *service.GetBackendHealthRequest) (*service.GetBackendHealthResponse, error) {
			return &service.GetBackendHealthResponse{
				Name:      req.Name,
				Healthy:   true,
				Latency:   5,
				LastCheck: time.Now().UnixNano(),
			}, nil
		},
	}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.GetBackendHealth(tenantctx.WithSystem(context.Background()), &pb.GetBackendHealthRequest{Name: "local"})

	require.NoError(t, err)
	assert.Equal(t, "local", resp.Name)
	assert.True(t, resp.Healthy)
}

func TestGRPCRemaining91_ObjectStorageServer_GetTenantUsage_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		getTenantUsageFunc: func(ctx context.Context, req *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error) {
			return &service.GetTenantUsageResponse{
				TenantID:    req.TenantID,
				TotalSize:   1024 * 1024,
				ObjectCount: 42,
				Timestamp:   time.Now().UnixNano(),
			}, nil
		},
	}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.GetObjectStorageTenantUsage(tenantctx.WithSystem(context.Background()), &pb.GetObjectStorageTenantUsageRequest{TenantId: "t1"})

	require.NoError(t, err)
	assert.Equal(t, "t1", resp.Usage.TenantId)
	assert.Equal(t, int64(42), resp.Usage.ObjectCount)
}

func TestGRPCRemaining91_ObjectStorageServer_GetTenantQuotas_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		getTenantQuotasFunc: func(ctx context.Context, req *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error) {
			return &service.GetTenantQuotasResponse{
				TenantID:         req.TenantID,
				MaxStorageBytes:  1024 * 1024 * 1024,
				MaxObjectSize:    100 * 1024 * 1024,
				MaxObjectCount:   10000,
				UsedStorageBytes: 1024,
				UsedObjectCount:  5,
			}, nil
		},
	}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.GetObjectStorageTenantQuotas(tenantctx.WithSystem(context.Background()), &pb.GetObjectStorageTenantQuotasRequest{TenantId: "t1"})

	require.NoError(t, err)
	assert.Equal(t, "t1", resp.Quotas.TenantId)
	assert.Equal(t, int64(10000), resp.Quotas.MaxObjectCount)
}

func TestGRPCRemaining91_ObjectStorageServer_SetTenantQuotas_Success(t *testing.T) {
	srv := &objectStorageServer{service: &mockObjectStorageService{}}

	resp, err := srv.SetObjectStorageTenantQuotas(tenantctx.WithSystem(context.Background()), &pb.SetObjectStorageTenantQuotasRequest{
		Quotas: &pb.ObjectStorageTenantQuotas{TenantId: "t1", MaxStorageBytes: 1024},
	})

	require.NoError(t, err)
	assert.Equal(t, "t1", resp.TenantId)
	assert.NotNil(t, resp.UpdatedAt)
}

func TestGRPCRemaining91_ObjectStorageServer_BatchPutObjects_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		batchPutObjectsFunc: func(ctx context.Context, req *service.BatchPutObjectsRequest) (*service.BatchPutObjectsResponse, error) {
			results := make([]*service.BatchPutResult, len(req.Objects))
			for i, obj := range req.Objects {
				results[i] = &service.BatchPutResult{
					Key:      obj.Key,
					ObjectID: uint64(i + 1),
				}
			}
			return &service.BatchPutObjectsResponse{
				Results:      results,
				SuccessCount: len(req.Objects),
				FailureCount: 0,
			}, nil
		},
	}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.BatchPutObjects(tenantctx.WithSystem(context.Background()), &pb.BatchPutObjectsRequest{
		Objects: []*pb.PutObjectRequest{
			{Key: "key1", Value: []byte("data1")},
			{Key: "key2", Value: []byte("data2")},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, int32(2), resp.Succeeded)
	assert.Equal(t, int32(0), resp.Failed)
}

func TestGRPCRemaining91_ObjectStorageServer_BatchPutObjects_PartialFailure(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		batchPutObjectsFunc: func(ctx context.Context, req *service.BatchPutObjectsRequest) (*service.BatchPutObjectsResponse, error) {
			return &service.BatchPutObjectsResponse{
				Results: []*service.BatchPutResult{
					{Key: "key2", ObjectID: 1},
				},
				SuccessCount: 1,
				FailureCount: 1,
			}, nil
		},
	}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.BatchPutObjects(tenantctx.WithSystem(context.Background()), &pb.BatchPutObjectsRequest{
		Objects: []*pb.PutObjectRequest{
			{Key: "", Value: []byte("data1")},
			{Key: "key2", Value: []byte("data2")},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, int32(1), resp.Succeeded)
	assert.Equal(t, int32(1), resp.Failed)
}

func TestGRPCRemaining91_ObjectStorageServer_BatchDeleteObjects_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		batchDeleteObjectsFunc: func(ctx context.Context, req *service.BatchDeleteObjectsRequest) (*service.BatchDeleteObjectsResponse, error) {
			results := make([]*service.BatchDeleteResult, len(req.Keys))
			for i, key := range req.Keys {
				results[i] = &service.BatchDeleteResult{
					Key:     key,
					Success: true,
				}
			}
			return &service.BatchDeleteObjectsResponse{
				Results:      results,
				DeletedCount: len(req.Keys),
				FailureCount: 0,
			}, nil
		},
	}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.BatchDeleteObjects(tenantctx.WithSystem(context.Background()), &pb.BatchDeleteObjectsRequest{
		Keys:     []string{"key1", "key2"},
		TenantId: "t1",
	})

	require.NoError(t, err)
	assert.Len(t, resp.DeletedKeys, 2)
}

func TestGRPCRemaining91_ObjectStorageServer_BatchGetObjects_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		batchGetObjectsFunc: func(ctx context.Context, req *service.BatchGetObjectsRequest) (*service.BatchGetObjectsResponse, error) {
			results := make([]*service.BatchGetResult, len(req.Keys))
			for i, key := range req.Keys {
				results[i] = &service.BatchGetResult{
					Key:         key,
					Value:       []byte("data-" + key),
					ContentType: "application/octet-stream",
					Size:        int64(len("data-" + key)),
					Found:       true,
				}
			}
			return &service.BatchGetObjectsResponse{
				Results:       results,
				FoundCount:    len(req.Keys),
				NotFoundCount: 0,
			}, nil
		},
	}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.BatchGetObjects(tenantctx.WithSystem(context.Background()), &pb.BatchGetObjectsRequest{
		Keys:     []string{"key1", "key2"},
		TenantId: "t1",
	})

	require.NoError(t, err)
	assert.Len(t, resp.Objects, 2)
}

func TestGRPCRemaining91_ObjectStorageServer_BatchGetObjects_NotFound(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		batchGetObjectsFunc: func(ctx context.Context, req *service.BatchGetObjectsRequest) (*service.BatchGetObjectsResponse, error) {
			results := make([]*service.BatchGetResult, len(req.Keys))
			for i, key := range req.Keys {
				results[i] = &service.BatchGetResult{
					Key:   key,
					Found: false,
				}
			}
			return &service.BatchGetObjectsResponse{
				Results:       results,
				FoundCount:    0,
				NotFoundCount: len(req.Keys),
			}, nil
		},
	}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.BatchGetObjects(tenantctx.WithSystem(context.Background()), &pb.BatchGetObjectsRequest{
		Keys: []string{"missing1", "missing2"},
	})

	require.NoError(t, err)
	assert.Len(t, resp.NotFound, 2)
	assert.Len(t, resp.Objects, 0)
}

func TestGRPCRemaining91_ObjectStorageServer_BatchDeleteObjects_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		batchDeleteObjectsFunc: func(ctx context.Context, req *service.BatchDeleteObjectsRequest) (*service.BatchDeleteObjectsResponse, error) {
			results := make([]*service.BatchDeleteResult, len(req.Keys))
			for i, key := range req.Keys {
				results[i] = &service.BatchDeleteResult{
					Key:     key,
					Success: false,
					Error:   "internal error",
				}
			}
			return &service.BatchDeleteObjectsResponse{
				Results:      results,
				DeletedCount: 0,
				FailureCount: len(req.Keys),
			}, nil
		},
	}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.BatchDeleteObjects(tenantctx.WithSystem(context.Background()), &pb.BatchDeleteObjectsRequest{
		Keys: []string{"key1"},
	})

	require.NoError(t, err)
	assert.Len(t, resp.FailedKeys, 1)
}

// =============================================================================
// Server ObjectStorage - ReplicateObject with mixed results
// =============================================================================

func TestGRPCRemaining91_ObjectStorageServer_ReplicateObject_MixedResults(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		replicateObjectFunc: func(ctx context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error) {
			return &service.ReplicateObjectResponse{
				Key:    req.Key,
				Status: "partial",
				Results: map[string]*service.ReplicationResult{
					"s3": {Success: true},
					"gcs": {
						Success: false,
						Error:   "connection refused",
					},
				},
			}, nil
		},
	}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.ReplicateObject(tenantctx.WithSystem(context.Background()), &pb.ReplicateObjectRequest{
		Key:          "test-key",
		TargetStores: []string{"s3", "gcs"},
	})

	require.NoError(t, err)
	assert.Equal(t, "test-key", resp.Key)
	assert.False(t, resp.Success)
}

// =============================================================================
// Server ObjectStorage - MigrateObject Success
// =============================================================================

func TestGRPCRemaining91_ObjectStorageServer_MigrateObject_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		migrateObjectFunc: func(ctx context.Context, req *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error) {
			return &service.MigrateObjectResponse{
				Key:               req.Key,
				SourceBackend:     "local",
				TargetBackend:     req.TargetBackend,
				Success:           true,
				DeletedFromSource: req.DeleteFromSource,
			}, nil
		},
	}
	srv := &objectStorageServer{service: mockSvc}

	resp, err := srv.MigrateObject(tenantctx.WithSystem(context.Background()), &pb.MigrateObjectRequest{
		Key:           "test-key",
		TargetBackend: "s3",
		DeleteSource:  true,
	})

	require.NoError(t, err)
	assert.Equal(t, "test-key", resp.Key)
	assert.True(t, resp.Success)
	assert.True(t, resp.SourceDeleted)
}

// =============================================================================
// Server ObjectStorage - Enum conversion tests
// =============================================================================

func TestGRPCRemaining91_ObjectStorageServer_EnumConversions(t *testing.T) {
	t.Run("stringToObjectStatus", func(t *testing.T) {
		tests := []struct {
			input    string
			expected pb.ObjectStatus
		}{
			{"active", pb.ObjectStatus_OBJECT_STATUS_ACTIVE},
			{"archived", pb.ObjectStatus_OBJECT_STATUS_ARCHIVED},
			{"restoring", pb.ObjectStatus_OBJECT_STATUS_RESTORING},
			{"pending_delete", pb.ObjectStatus_OBJECT_STATUS_PENDING_DELETE},
			{"deleted", pb.ObjectStatus_OBJECT_STATUS_DELETED},
			{"corrupted", pb.ObjectStatus_OBJECT_STATUS_CORRUPTED},
			{"unknown", pb.ObjectStatus_OBJECT_STATUS_UNSPECIFIED},
		}
		for _, tt := range tests {
			assert.Equal(t, tt.expected, stringToObjectStatus(tt.input), "status: %s", tt.input)
		}
	})

	t.Run("stringToBackendType", func(t *testing.T) {
		tests := []struct {
			input    string
			expected pb.BackendType
		}{
			{"local", pb.BackendType_BACKEND_TYPE_LOCAL},
			{"s3", pb.BackendType_BACKEND_TYPE_S3},
			{"gcs", pb.BackendType_BACKEND_TYPE_GCS},
			{"azure", pb.BackendType_BACKEND_TYPE_AZURE},
			{"minio", pb.BackendType_BACKEND_TYPE_MINIO},
			{"pebble", pb.BackendType_BACKEND_TYPE_PEBBLE},
			{"raft", pb.BackendType_BACKEND_TYPE_RAFT},
			{"unknown", pb.BackendType_BACKEND_TYPE_UNSPECIFIED},
		}
		for _, tt := range tests {
			assert.Equal(t, tt.expected, stringToBackendType(tt.input), "type: %s", tt.input)
		}
	})

	t.Run("stringToStorageTier", func(t *testing.T) {
		tests := []struct {
			input    string
			expected pb.StorageTier
		}{
			{"hot", pb.StorageTier_STORAGE_TIER_HOT},
			{"warm", pb.StorageTier_STORAGE_TIER_WARM},
			{"cold", pb.StorageTier_STORAGE_TIER_COLD},
			{"archive", pb.StorageTier_STORAGE_TIER_ARCHIVE},
			{"unknown", pb.StorageTier_STORAGE_TIER_UNSPECIFIED},
		}
		for _, tt := range tests {
			assert.Equal(t, tt.expected, stringToStorageTier(tt.input), "tier: %s", tt.input)
		}
	})

	t.Run("stringToReplicationMode", func(t *testing.T) {
		tests := []struct {
			input    string
			expected pb.ReplicationMode
		}{
			{"sync", pb.ReplicationMode_REPLICATION_MODE_SYNC},
			{"async", pb.ReplicationMode_REPLICATION_MODE_ASYNC},
			{"quorum", pb.ReplicationMode_REPLICATION_MODE_QUORUM},
			{"unknown", pb.ReplicationMode_REPLICATION_MODE_UNSPECIFIED},
		}
		for _, tt := range tests {
			assert.Equal(t, tt.expected, stringToReplicationMode(tt.input), "mode: %s", tt.input)
		}
	})
}

// =============================================================================
// Server ObjectStorage - Policy conversion tests (nil paths)
// =============================================================================

func TestGRPCRemaining91_ObjectStorageServer_NilPolicyConversions(t *testing.T) {
	t.Run("protoToReplicationPolicy_nil", func(t *testing.T) {
		assert.Nil(t, protoToReplicationPolicy(nil))
	})

	t.Run("protoToArchivePolicy_nil", func(t *testing.T) {
		assert.Nil(t, protoToArchivePolicy(nil))
	})

	t.Run("protoToRetentionPolicy_nil", func(t *testing.T) {
		assert.Nil(t, protoToRetentionPolicy(nil))
	})

	t.Run("replicationPolicyToProto_nil", func(t *testing.T) {
		assert.Nil(t, replicationPolicyToProto(nil))
	})

	t.Run("archivePolicyToProto_nil", func(t *testing.T) {
		assert.Nil(t, archivePolicyToProto(nil))
	})

	t.Run("retentionPolicyToProto_nil", func(t *testing.T) {
		assert.Nil(t, retentionPolicyToProto(nil))
	})

	t.Run("objectSummaryToProto_nil", func(t *testing.T) {
		assert.Nil(t, objectSummaryToProto(nil))
	})

	t.Run("backendInfoToProto_nil", func(t *testing.T) {
		assert.Nil(t, backendInfoToProto(nil))
	})

	t.Run("backendInfoToProto_with_health", func(t *testing.T) {
		info := backendInfoToProto(&service.BackendInfo{
			Name:    "local",
			Type:    "local",
			Enabled: true,
			Health:  &service.BackendHealthInfo{Healthy: true, LastCheck: time.Now().UnixNano()},
		})
		assert.NotNil(t, info)
		assert.True(t, info.Healthy)
	})
}

// =============================================================================
// Client - Closed client tests for objectstorage
// =============================================================================

func TestGRPCRemaining91_Client_ObjectStorage_Closed(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: false,
		closed:    true,
	}
	ctx := tenantctx.WithSystem(context.Background())

	tests := []struct {
		name string
		call func() error
	}{
		{"PutObject", func() error { _, err := client.ObjectStoragePutObject(ctx, &pb.PutObjectRequest{}); return err }},
		{"GetObject", func() error { _, err := client.ObjectStorageGetObject(ctx, &pb.GetObjectRequest{}); return err }},
		{"DeleteObject", func() error { _, err := client.ObjectStorageDeleteObject(ctx, &pb.DeleteObjectRequest{}); return err }},
		{"HeadObject", func() error { _, err := client.ObjectStorageHeadObject(ctx, &pb.HeadObjectRequest{}); return err }},
		{"ListObjects", func() error { _, err := client.ObjectStorageListObjects(ctx, &pb.ListObjectsRequest{}); return err }},
		{"ListByTag", func() error { _, err := client.ObjectStorageListByTag(ctx, &pb.ListByTagRequest{}); return err }},
		{"ListByTenant", func() error { _, err := client.ObjectStorageListByTenant(ctx, &pb.ListByTenantRequest{}); return err }},
		{"CopyObject", func() error { _, err := client.ObjectStorageCopyObject(ctx, &pb.CopyObjectRequest{}); return err }},
		{"ReplicateObject", func() error {
			_, err := client.ObjectStorageReplicateObject(ctx, &pb.ReplicateObjectRequest{})
			return err
		}},
		{"MigrateObject", func() error { _, err := client.ObjectStorageMigrateObject(ctx, &pb.MigrateObjectRequest{}); return err }},
		{"ArchiveObject", func() error { _, err := client.ObjectStorageArchiveObject(ctx, &pb.ArchiveObjectRequest{}); return err }},
		{"RestoreObject", func() error { _, err := client.ObjectStorageRestoreObject(ctx, &pb.RestoreObjectRequest{}); return err }},
		{"VerifyObject", func() error { _, err := client.ObjectStorageVerifyObject(ctx, &pb.VerifyObjectRequest{}); return err }},
		{"SetObjectPolicy", func() error {
			_, err := client.ObjectStorageSetObjectPolicy(ctx, &pb.SetObjectPolicyRequest{})
			return err
		}},
		{"GetObjectPolicy", func() error {
			_, err := client.ObjectStorageGetObjectPolicy(ctx, &pb.GetObjectPolicyRequest{})
			return err
		}},
		{"ListBackends", func() error { _, err := client.ObjectStorageListBackends(ctx, &pb.ListBackendsRequest{}); return err }},
		{"GetBackendHealth", func() error {
			_, err := client.ObjectStorageGetBackendHealth(ctx, &pb.GetBackendHealthRequest{})
			return err
		}},
		{"GetTenantUsage", func() error {
			_, err := client.ObjectStorageGetTenantUsage(ctx, &pb.GetObjectStorageTenantUsageRequest{})
			return err
		}},
		{"GetTenantQuotas", func() error {
			_, err := client.ObjectStorageGetTenantQuotas(ctx, &pb.GetObjectStorageTenantQuotasRequest{})
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "closed")
		})
	}
}

// =============================================================================
// Client - Closed client tests for KMS (KMSListKeys has ensureConnected)
// =============================================================================

func TestGRPCRemaining91_Client_KMS_ListKeys_Closed(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: false,
		closed:    true,
	}
	ctx := tenantctx.WithSystem(context.Background())

	_, err := client.KMSListKeys(ctx, &service.ListKeysRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

// =============================================================================
// Client - Closed client tests for vector (uses pb types)
// =============================================================================

func TestGRPCRemaining91_Client_Vector_Closed(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: false,
		closed:    true,
	}
	ctx := tenantctx.WithSystem(context.Background())

	tests := []struct {
		name string
		call func() error
	}{
		{"CreateNamespace", func() error { _, err := client.VectorCreateNamespace(ctx, &pb.CreateNamespaceRequest{}); return err }},
		{"DeleteNamespace", func() error { _, err := client.VectorDeleteNamespace(ctx, &pb.DeleteNamespaceRequest{}); return err }},
		{"ListNamespaces", func() error { _, err := client.VectorListNamespaces(ctx, &pb.ListNamespacesRequest{}); return err }},
		{"GetNamespaceInfo", func() error { _, err := client.VectorGetNamespaceInfo(ctx, &pb.GetNamespaceInfoRequest{}); return err }},
		{"Upsert", func() error { _, err := client.VectorUpsert(ctx, &pb.UpsertVectorRequest{}); return err }},
		{"Get", func() error { _, err := client.VectorGet(ctx, &pb.GetVectorRequest{}); return err }},
		{"Delete", func() error { _, err := client.VectorDelete(ctx, &pb.DeleteVectorRequest{}); return err }},
		{"BatchUpsert", func() error { _, err := client.VectorBatchUpsert(ctx, &pb.BatchUpsertVectorsRequest{}); return err }},
		{"BatchDelete", func() error { _, err := client.VectorBatchDelete(ctx, &pb.BatchDeleteVectorsRequest{}); return err }},
		{"UpdateMetadata", func() error { _, err := client.VectorUpdateMetadata(ctx, &pb.UpdateMetadataRequest{}); return err }},
		{"Health", func() error { _, err := client.VectorHealth(ctx, &pb.VectorHealthRequest{}); return err }},
		{"GetStats", func() error { _, err := client.VectorGetStats(ctx, &pb.GetVectorStatsRequest{}); return err }},
		{"SearchSimilar", func() error { _, err := client.VectorSearchSimilar(ctx, &pb.SearchSimilarRequest{}); return err }},
		{"SearchByID", func() error { _, err := client.VectorSearchByID(ctx, &pb.SearchByIDRequest{}); return err }},
		{"HybridSearch", func() error { _, err := client.VectorHybridSearch(ctx, &pb.HybridSearchRequest{}); return err }},
		{"SearchByMetadata", func() error { _, err := client.VectorSearchByMetadata(ctx, &pb.SearchByMetadataRequest{}); return err }},
		{"List", func() error { _, err := client.VectorList(ctx, &pb.ListVectorsRequest{}); return err }},
		{"RebuildIndex", func() error { _, err := client.VectorRebuildIndex(ctx, &pb.RebuildIndexRequest{}); return err }},
		{"GetIndexProgress", func() error { _, err := client.VectorGetIndexProgress(ctx, &pb.GetIndexProgressRequest{}); return err }},
		{"Export", func() error { _, err := client.VectorExport(ctx, &pb.ExportVectorsRequest{}); return err }},
		{"Import", func() error { _, err := client.VectorImport(ctx, []*pb.ImportVectorsRequest{}); return err }},
		{"Compact", func() error { _, err := client.VectorCompact(ctx, &pb.CompactNamespaceRequest{}); return err }},
		{"GetCompactionProgress", func() error {
			_, err := client.VectorGetCompactionProgress(ctx, &pb.GetCompactionProgressRequest{})
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "closed")
		})
	}
}

// =============================================================================
// Client - Closed client tests for ollama
// =============================================================================

func TestGRPCRemaining91_Client_Ollama_Closed(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: false,
		closed:    true,
	}
	ctx := tenantctx.WithSystem(context.Background())

	tests := []struct {
		name string
		call func() error
	}{
		{"Health", func() error { _, err := client.OllamaHealth(ctx, &pb.OllamaHealthRequest{}); return err }},
		{"ListModels", func() error { _, err := client.OllamaListModels(ctx, &pb.OllamaListModelsRequest{}); return err }},
		{"GetModelInfo", func() error { _, err := client.OllamaGetModelInfo(ctx, &pb.OllamaGetModelInfoRequest{}); return err }},
		{"Embed", func() error { _, err := client.OllamaEmbed(ctx, &pb.OllamaEmbedRequest{}); return err }},
		{"EmbedWithModel", func() error {
			_, err := client.OllamaEmbedWithModel(ctx, &pb.OllamaEmbedWithModelRequest{})
			return err
		}},
		{"EmbedBatch", func() error { _, err := client.OllamaEmbedBatch(ctx, &pb.OllamaEmbedBatchRequest{}); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "closed")
		})
	}
}

// =============================================================================
// Client - UpgradeNode and GetUpgradeStatus with closed client
// =============================================================================

func TestGRPCRemaining91_Client_UpgradeNode_Closed(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: false,
		closed:    true,
	}

	resp, err := client.UpgradeNode(tenantctx.WithSystem(context.Background()), &pb.UpgradeNodeRequest{})
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestGRPCRemaining91_Client_GetUpgradeStatus_Closed(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: false,
		closed:    true,
	}

	resp, err := client.GetUpgradeStatus(tenantctx.WithSystem(context.Background()), &pb.GetUpgradeStatusRequest{})
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

// =============================================================================
// Client - RebalanceCluster and GetRebalanceProgress with closed client
// =============================================================================

func TestGRPCRemaining91_Client_RebalanceCluster_Closed(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: false,
		closed:    true,
	}

	resp, err := client.RebalanceCluster(tenantctx.WithSystem(context.Background()), &pb.RebalanceClusterRequest{})
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestGRPCRemaining91_Client_GetRebalanceProgress_Closed(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: false,
		closed:    true,
	}

	resp, err := client.GetRebalanceProgress(tenantctx.WithSystem(context.Background()), &pb.GetRebalanceProgressRequest{})
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

// =============================================================================
// Server Vector - Enum conversion coverage (low coverage areas: 33-50%)
// =============================================================================

func TestGRPCRemaining91_ServerVector_ProtoDistanceMetricToString(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.DistanceMetric
		expected string
	}{
		{"L2", pb.DistanceMetric_DISTANCE_METRIC_L2, "L2"},
		{"Cosine", pb.DistanceMetric_DISTANCE_METRIC_COSINE, "COSINE"},
		{"DotProduct", pb.DistanceMetric_DISTANCE_METRIC_DOT_PRODUCT, "DOT_PRODUCT"},
		{"Manhattan", pb.DistanceMetric_DISTANCE_METRIC_MANHATTAN, "MANHATTAN"},
		{"Unspecified", pb.DistanceMetric_DISTANCE_METRIC_UNSPECIFIED, "L2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoDistanceMetricToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGRPCRemaining91_ServerVector_StringToProtoDistanceMetric(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected pb.DistanceMetric
	}{
		{"L2", "L2", pb.DistanceMetric_DISTANCE_METRIC_L2},
		{"COSINE", "COSINE", pb.DistanceMetric_DISTANCE_METRIC_COSINE},
		{"DOT_PRODUCT", "DOT_PRODUCT", pb.DistanceMetric_DISTANCE_METRIC_DOT_PRODUCT},
		{"MANHATTAN", "MANHATTAN", pb.DistanceMetric_DISTANCE_METRIC_MANHATTAN},
		{"unknown", "unknown", pb.DistanceMetric_DISTANCE_METRIC_L2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringToProtoDistanceMetric(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGRPCRemaining91_ServerVector_ProtoIndexTypeToString(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.IndexType
		expected string
	}{
		{"HNSW", pb.IndexType_INDEX_TYPE_HNSW, "HNSW"},
		{"Flat", pb.IndexType_INDEX_TYPE_FLAT, "FLAT"},
		{"IVF_FLAT", pb.IndexType_INDEX_TYPE_IVF_FLAT, "IVF_FLAT"},
		{"IVF_PQ", pb.IndexType_INDEX_TYPE_IVF_PQ, "IVF_PQ"},
		{"Unspecified", pb.IndexType_INDEX_TYPE_UNSPECIFIED, "HNSW"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoIndexTypeToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGRPCRemaining91_ServerVector_StringToProtoIndexType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected pb.IndexType
	}{
		{"HNSW", "HNSW", pb.IndexType_INDEX_TYPE_HNSW},
		{"FLAT", "FLAT", pb.IndexType_INDEX_TYPE_FLAT},
		{"IVF_FLAT", "IVF_FLAT", pb.IndexType_INDEX_TYPE_IVF_FLAT},
		{"IVF_PQ", "IVF_PQ", pb.IndexType_INDEX_TYPE_IVF_PQ},
		{"unknown", "unknown", pb.IndexType_INDEX_TYPE_HNSW},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringToProtoIndexType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// convertGRPCError tests
// =============================================================================

func TestGRPCRemaining91_ConvertGRPCError(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		err := convertGRPCError(nil, "TestOp")
		assert.Nil(t, err)
	})

	t.Run("QBError passthrough", func(t *testing.T) {
		qbErr := &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test", Err: dberrors.New(dberrors.ErrNotFound, "test", "gone")}
		err := convertGRPCError(qbErr, "TestOp")
		assert.Error(t, err)
		assert.Equal(t, qbErr, err)
	})

	t.Run("gRPC NotFound error", func(t *testing.T) {
		grpcErr := status.Error(codes.NotFound, "not found")
		err := convertGRPCError(grpcErr, "TestOp")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "gRPC operation failed")
	})

	t.Run("generic error", func(t *testing.T) {
		err := convertGRPCError(assert.AnError, "TestOp")
		assert.Error(t, err)
	})
}

// =============================================================================
// toProtoError tests
// =============================================================================

func TestGRPCRemaining91_ToProtoError(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		err := toProtoError(nil)
		assert.Nil(t, err)
	})

	t.Run("QBError NotFound", func(t *testing.T) {
		qbErr := &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test", Err: dberrors.New(dberrors.ErrNotFound, "test", "not found")}
		err := toProtoError(qbErr)
		assert.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("QBError AlreadyExists", func(t *testing.T) {
		qbErr := &dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "test", Err: dberrors.New(dberrors.ErrAlreadyExists, "test", "exists")}
		err := toProtoError(qbErr)
		assert.Error(t, err)
		assert.Equal(t, codes.AlreadyExists, status.Code(err))
	})

	t.Run("generic error becomes Internal", func(t *testing.T) {
		err := toProtoError(assert.AnError)
		assert.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})
}
