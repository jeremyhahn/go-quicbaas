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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

const dekBufSize = 1024 * 1024

// minimalDEKServer implements pb.DEKManagementServer for testing
type minimalDEKServer struct {
	pb.UnimplementedDEKManagementServer

	createDEKFunc      func(ctx context.Context, req *pb.CreateDEKRequest) (*pb.CreateDEKResponse, error)
	getDEKFunc         func(ctx context.Context, req *pb.GetDEKRequest) (*pb.GetDEKResponse, error)
	listDEKsFunc       func(ctx context.Context, req *pb.ListDEKsRequest) (*pb.ListDEKsResponse, error)
	rotateDEKFunc      func(ctx context.Context, req *pb.RotateDEKRequest) (*pb.RotateDEKResponse, error)
	deleteDEKFunc      func(ctx context.Context, req *pb.DeleteDEKRequest) (*pb.DeleteDEKResponse, error)
	getDEKStatusFunc   func(ctx context.Context, req *pb.GetDEKStatusRequest) (*pb.GetDEKStatusResponse, error)
	setDefaultDEKFunc  func(ctx context.Context, req *pb.SetDefaultDEKRequest) (*pb.SetDefaultDEKResponse, error)
	setDEKSafetyFunc   func(ctx context.Context, req *pb.SetDEKSafetyRequest) (*pb.SetDEKSafetyResponse, error)
	clearDEKSafetyFunc func(ctx context.Context, req *pb.ClearDEKSafetyRequest) (*pb.ClearDEKSafetyResponse, error)
}

func (s *minimalDEKServer) CreateDEK(ctx context.Context, req *pb.CreateDEKRequest) (*pb.CreateDEKResponse, error) {
	if s.createDEKFunc != nil {
		return s.createDEKFunc(ctx, req)
	}
	return &pb.CreateDEKResponse{
		Dek: &pb.DEKMetadata{
			Path:          req.Path,
			Algorithm:     req.Algorithm,
			Version:       1,
			Status:        "active",
			IsDefault:     true,
			CreatedAt:     time.Now().Unix(),
			MaxUsageCount: req.MaxOperations,
			MaxBytesUsed:  req.MaxBytes,
		},
		Timestamp: time.Now().Unix(),
	}, nil
}

func (s *minimalDEKServer) GetDEK(ctx context.Context, req *pb.GetDEKRequest) (*pb.GetDEKResponse, error) {
	if s.getDEKFunc != nil {
		return s.getDEKFunc(ctx, req)
	}
	return &pb.GetDEKResponse{
		Dek: &pb.DEKMetadata{
			Path:       req.Path,
			Algorithm:  pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
			Version:    1,
			Status:     "active",
			IsDefault:  true,
			CreatedAt:  time.Now().Unix(),
			UsageCount: 5000,
			BytesUsed:  1024 * 1024 * 100,
		},
		Timestamp: time.Now().Unix(),
	}, nil
}

func (s *minimalDEKServer) ListDEKs(ctx context.Context, req *pb.ListDEKsRequest) (*pb.ListDEKsResponse, error) {
	if s.listDEKsFunc != nil {
		return s.listDEKsFunc(ctx, req)
	}
	return &pb.ListDEKsResponse{
		Deks: []*pb.DEKMetadata{
			{
				Path:      "/app/data1",
				Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
				Version:   1,
				Status:    "active",
				CreatedAt: time.Now().Unix(),
			},
			{
				Path:      "/app/data2",
				Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
				Version:   1,
				Status:    "active",
				CreatedAt: time.Now().Unix(),
			},
		},
		NextCursor: "",
		HasMore:    false,
		Timestamp:  time.Now().Unix(),
	}, nil
}

func (s *minimalDEKServer) RotateDEK(ctx context.Context, req *pb.RotateDEKRequest) (*pb.RotateDEKResponse, error) {
	if s.rotateDEKFunc != nil {
		return s.rotateDEKFunc(ctx, req)
	}
	return &pb.RotateDEKResponse{
		Path:       req.Path,
		OldVersion: 1,
		NewVersion: 2,
		Timestamp:  time.Now().Unix(),
	}, nil
}

func (s *minimalDEKServer) DeleteDEK(ctx context.Context, req *pb.DeleteDEKRequest) (*pb.DeleteDEKResponse, error) {
	if s.deleteDEKFunc != nil {
		return s.deleteDEKFunc(ctx, req)
	}
	return &pb.DeleteDEKResponse{
		Path:      req.Path,
		Deleted:   true,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (s *minimalDEKServer) GetDEKStatus(ctx context.Context, req *pb.GetDEKStatusRequest) (*pb.GetDEKStatusResponse, error) {
	if s.getDEKStatusFunc != nil {
		return s.getDEKStatusFunc(ctx, req)
	}
	return &pb.GetDEKStatusResponse{
		Dek: &pb.DEKMetadata{
			Path:          req.Path,
			Algorithm:     pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
			Version:       1,
			Status:        "active",
			UsageCount:    50000,
			MaxUsageCount: 100000,
			BytesUsed:     1073741824,
			MaxBytesUsed:  2147483648,
		},
		ReencryptionProgress: nil,
		Timestamp:            time.Now().Unix(),
	}, nil
}

func (s *minimalDEKServer) SetDefaultDEK(ctx context.Context, req *pb.SetDefaultDEKRequest) (*pb.SetDefaultDEKResponse, error) {
	if s.setDefaultDEKFunc != nil {
		return s.setDefaultDEKFunc(ctx, req)
	}
	return &pb.SetDefaultDEKResponse{
		Path:      req.Path,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (s *minimalDEKServer) SetDEKSafety(ctx context.Context, req *pb.SetDEKSafetyRequest) (*pb.SetDEKSafetyResponse, error) {
	if s.setDEKSafetyFunc != nil {
		return s.setDEKSafetyFunc(ctx, req)
	}
	return &pb.SetDEKSafetyResponse{
		Path:      req.Path,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (s *minimalDEKServer) ClearDEKSafety(ctx context.Context, req *pb.ClearDEKSafetyRequest) (*pb.ClearDEKSafetyResponse, error) {
	if s.clearDEKSafetyFunc != nil {
		return s.clearDEKSafetyFunc(ctx, req)
	}
	return &pb.ClearDEKSafetyResponse{
		Path:      req.Path,
		Timestamp: time.Now().Unix(),
	}, nil
}

// minimalSafetyServer implements safety-related methods for pb.KeyManagementServer
type minimalSafetyServer struct {
	pb.UnimplementedKeyManagementServer

	safetyCheckFunc  func(ctx context.Context, req *pb.SafetyCheckRequest) (*pb.SafetyCheckResponse, error)
	safetyReportFunc func(ctx context.Context, req *pb.SafetyReportRequest) (*pb.SafetyReportResponse, error)
}

func (s *minimalSafetyServer) SafetyCheck(ctx context.Context, req *pb.SafetyCheckRequest) (*pb.SafetyCheckResponse, error) {
	if s.safetyCheckFunc != nil {
		return s.safetyCheckFunc(ctx, req)
	}
	return &pb.SafetyCheckResponse{
		Warnings: []*pb.SafetyWarning{
			{
				Type:           "key",
				Path:           "key-1",
				UsagePercent:   85.0,
				Status:         "warning",
				OperationsUsed: 850000,
				BytesUsed:      900000000,
				MaxOperations:  1000000,
				MaxBytes:       1073741824,
			},
		},
		Timestamp: time.Now().Unix(),
	}, nil
}

func (s *minimalSafetyServer) SafetyReport(ctx context.Context, req *pb.SafetyReportRequest) (*pb.SafetyReportResponse, error) {
	if s.safetyReportFunc != nil {
		return s.safetyReportFunc(ctx, req)
	}
	return &pb.SafetyReportResponse{
		AllItems: []*pb.SafetyWarning{
			{
				Type:           "key",
				Path:           "key-1",
				UsagePercent:   85.0,
				Status:         "warning",
				OperationsUsed: 850000,
				MaxOperations:  1000000,
			},
			{
				Type:         "dek",
				Path:         "/app/data",
				UsagePercent: 50.0,
				Status:       "ok",
				BytesUsed:    500000000,
				MaxBytes:     1000000000,
			},
		},
		TotalKeys:      5,
		TotalDeks:      3,
		KeysAtWarning:  1,
		KeysAtCritical: 0,
		DeksAtWarning:  0,
		DeksAtCritical: 0,
		Timestamp:      time.Now().Unix(),
	}, nil
}

func setupDEKTestServer(dekServer *minimalDEKServer, safetyServer *minimalSafetyServer) (*grpc.Server, *bufconn.Listener) {
	lis := bufconn.Listen(dekBufSize)
	server := grpc.NewServer()
	pb.RegisterDEKManagementServer(server, dekServer)
	if safetyServer != nil {
		pb.RegisterKeyManagementServer(server, safetyServer)
	}
	go func() {
		if err := server.Serve(lis); err != nil {
			// Server stopped
		}
	}()
	return server, lis
}

func createDEKTestClient(lis *bufconn.Listener) (*grpc.ClientConn, error) {
	return grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}

func setupConnectedDEKClient(t *testing.T, dekServer *minimalDEKServer, safetyServer *minimalSafetyServer) (*Client, *grpc.Server) {
	server, lis := setupDEKTestServer(dekServer, safetyServer)
	conn, err := createDEKTestClient(lis)
	require.NoError(t, err)

	client := &Client{
		conn:      conn,
		connected: true,
	}
	return client, server
}

// =============================================================================
// KMS Client DEK Method Tests
// =============================================================================

func TestClientKMSCreateDEK_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSCreateDEK(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSCreateDEK_Success(t *testing.T) {
	dekServer := &minimalDEKServer{}
	client, server := setupConnectedDEKClient(t, dekServer, nil)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.CreateDEKRequest{
		Path:           "/app/data",
		Algorithm:      service.KeyAlgAES256GCM,
		Description:    "Test DEK",
		MaxOperations:  1000000,
		MaxBytes:       1073741824,
		WarningPercent: 80,
	}

	resp, err := client.KMSCreateDEK(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotNil(t, resp.DEK)
	assert.Equal(t, "/app/data", resp.DEK.Path)
}

func TestClientKMSCreateDEK_Error(t *testing.T) {
	dekServer := &minimalDEKServer{
		createDEKFunc: func(ctx context.Context, req *pb.CreateDEKRequest) (*pb.CreateDEKResponse, error) {
			return nil, status.Error(codes.AlreadyExists, "DEK already exists")
		},
	}
	client, server := setupConnectedDEKClient(t, dekServer, nil)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.CreateDEKRequest{
		Path:      "/app/data",
		Algorithm: service.KeyAlgAES256GCM,
	}

	resp, err := client.KMSCreateDEK(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSGetDEK_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSGetDEK(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSGetDEK_Success(t *testing.T) {
	dekServer := &minimalDEKServer{}
	client, server := setupConnectedDEKClient(t, dekServer, nil)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.GetDEKRequest{
		Path: "/app/data",
	}

	resp, err := client.KMSGetDEK(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotNil(t, resp.DEK)
	assert.Equal(t, "/app/data", resp.DEK.Path)
}

func TestClientKMSGetDEK_Error(t *testing.T) {
	dekServer := &minimalDEKServer{
		getDEKFunc: func(ctx context.Context, req *pb.GetDEKRequest) (*pb.GetDEKResponse, error) {
			return nil, status.Error(codes.NotFound, "DEK not found")
		},
	}
	client, server := setupConnectedDEKClient(t, dekServer, nil)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.GetDEKRequest{
		Path: "/nonexistent",
	}

	resp, err := client.KMSGetDEK(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSListDEKs_NilRequest(t *testing.T) {
	dekServer := &minimalDEKServer{}
	client, server := setupConnectedDEKClient(t, dekServer, nil)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	// nil request should work and use defaults
	resp, err := client.KMSListDEKs(ctx, nil)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.DEKs, 2)
}

func TestClientKMSListDEKs_Success(t *testing.T) {
	dekServer := &minimalDEKServer{}
	client, server := setupConnectedDEKClient(t, dekServer, nil)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.ListDEKsRequest{
		Prefix:    "/app",
		Recursive: true,
		Limit:     10,
	}

	resp, err := client.KMSListDEKs(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.DEKs, 2)
}

func TestClientKMSListDEKs_Error(t *testing.T) {
	dekServer := &minimalDEKServer{
		listDEKsFunc: func(ctx context.Context, req *pb.ListDEKsRequest) (*pb.ListDEKsResponse, error) {
			return nil, status.Error(codes.Internal, "internal error")
		},
	}
	client, server := setupConnectedDEKClient(t, dekServer, nil)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.ListDEKsRequest{
		Prefix: "/app",
	}

	resp, err := client.KMSListDEKs(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSRotateDEK_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSRotateDEK(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSRotateDEK_Success(t *testing.T) {
	dekServer := &minimalDEKServer{}
	client, server := setupConnectedDEKClient(t, dekServer, nil)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.RotateDEKRequest{
		Path:       "/app/data",
		Background: false,
	}

	resp, err := client.KMSRotateDEK(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "/app/data", resp.Path)
	assert.Equal(t, uint64(1), resp.OldVersion)
	assert.Equal(t, uint64(2), resp.NewVersion)
}

func TestClientKMSRotateDEK_Error(t *testing.T) {
	dekServer := &minimalDEKServer{
		rotateDEKFunc: func(ctx context.Context, req *pb.RotateDEKRequest) (*pb.RotateDEKResponse, error) {
			return nil, status.Error(codes.NotFound, "DEK not found")
		},
	}
	client, server := setupConnectedDEKClient(t, dekServer, nil)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.RotateDEKRequest{
		Path: "/nonexistent",
	}

	resp, err := client.KMSRotateDEK(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSDeleteDEK_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSDeleteDEK(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSDeleteDEK_Success(t *testing.T) {
	dekServer := &minimalDEKServer{}
	client, server := setupConnectedDEKClient(t, dekServer, nil)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.DeleteDEKRequest{
		Path:  "/app/data",
		Force: true,
	}

	resp, err := client.KMSDeleteDEK(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "/app/data", resp.Path)
	assert.True(t, resp.Deleted)
}

func TestClientKMSDeleteDEK_Error(t *testing.T) {
	dekServer := &minimalDEKServer{
		deleteDEKFunc: func(ctx context.Context, req *pb.DeleteDEKRequest) (*pb.DeleteDEKResponse, error) {
			return nil, status.Error(codes.FailedPrecondition, "DEK is in use")
		},
	}
	client, server := setupConnectedDEKClient(t, dekServer, nil)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.DeleteDEKRequest{
		Path: "/app/data",
	}

	resp, err := client.KMSDeleteDEK(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSGetDEKStatus_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSGetDEKStatus(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSGetDEKStatus_Success(t *testing.T) {
	dekServer := &minimalDEKServer{}
	client, server := setupConnectedDEKClient(t, dekServer, nil)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.GetDEKStatusRequest{
		Path: "/app/data",
	}

	resp, err := client.KMSGetDEKStatus(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotNil(t, resp.DEK)
	assert.Equal(t, "/app/data", resp.DEK.Path)
}

func TestClientKMSGetDEKStatus_WithReencryptionProgress(t *testing.T) {
	now := time.Now()
	dekServer := &minimalDEKServer{
		getDEKStatusFunc: func(ctx context.Context, req *pb.GetDEKStatusRequest) (*pb.GetDEKStatusResponse, error) {
			return &pb.GetDEKStatusResponse{
				Dek: &pb.DEKMetadata{
					Path:    req.Path,
					Version: 2,
					Status:  "rotating",
				},
				ReencryptionProgress: &pb.ReencryptionProgress{
					InProgress:          true,
					TotalKeys:           1000,
					ProcessedKeys:       500,
					FailedKeys:          5,
					StartedAt:           now.Add(-5 * time.Minute).Unix(),
					EstimatedCompletion: now.Add(10 * time.Minute).Unix(),
				},
				Timestamp: now.Unix(),
			}, nil
		},
	}
	client, server := setupConnectedDEKClient(t, dekServer, nil)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.GetDEKStatusRequest{
		Path: "/app/data",
	}

	resp, err := client.KMSGetDEKStatus(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotNil(t, resp.ReencryptionProgress)
	assert.True(t, resp.ReencryptionProgress.InProgress)
	assert.Equal(t, uint64(1000), resp.ReencryptionProgress.TotalKeys)
	assert.Equal(t, uint64(500), resp.ReencryptionProgress.ProcessedKeys)
}

func TestClientKMSSetDefaultDEK_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSSetDefaultDEK(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSSetDefaultDEK_Success(t *testing.T) {
	dekServer := &minimalDEKServer{}
	client, server := setupConnectedDEKClient(t, dekServer, nil)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.SetDefaultDEKRequest{
		Path: "/app/data",
	}

	resp, err := client.KMSSetDefaultDEK(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "/app/data", resp.Path)
}

func TestClientKMSSetDEKSafety_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSSetDEKSafety(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSSetDEKSafety_Success(t *testing.T) {
	dekServer := &minimalDEKServer{}
	client, server := setupConnectedDEKClient(t, dekServer, nil)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.SetDEKSafetyRequest{
		Path:           "/app/data",
		MaxOperations:  500000,
		MaxBytes:       1073741824,
		WarningPercent: 75,
	}

	resp, err := client.KMSSetDEKSafety(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "/app/data", resp.Path)
}

func TestClientKMSClearDEKSafety_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSClearDEKSafety(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSClearDEKSafety_Success(t *testing.T) {
	dekServer := &minimalDEKServer{}
	client, server := setupConnectedDEKClient(t, dekServer, nil)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.ClearDEKSafetyRequest{
		Path: "/app/data",
	}

	resp, err := client.KMSClearDEKSafety(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "/app/data", resp.Path)
	assert.True(t, resp.Cleared)
}

// =============================================================================
// Safety Check/Report Tests
// =============================================================================

func TestClientKMSSafetyCheck_NilRequest(t *testing.T) {
	safetyServer := &minimalSafetyServer{}
	client, server := setupConnectedDEKClient(t, &minimalDEKServer{}, safetyServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	// nil request should work and use defaults
	resp, err := client.KMSSafetyCheck(ctx, nil)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Items, 1)
}

func TestClientKMSSafetyCheck_Success(t *testing.T) {
	safetyServer := &minimalSafetyServer{}
	client, server := setupConnectedDEKClient(t, &minimalDEKServer{}, safetyServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.SafetyCheckRequest{
		ThresholdPercent: 80.0,
	}

	resp, err := client.KMSSafetyCheck(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Items, 1)
	assert.Equal(t, "key", resp.Items[0].Type)
	assert.Equal(t, 85.0, resp.Items[0].UsagePercent)
}

func TestClientKMSSafetyCheck_Error(t *testing.T) {
	safetyServer := &minimalSafetyServer{
		safetyCheckFunc: func(ctx context.Context, req *pb.SafetyCheckRequest) (*pb.SafetyCheckResponse, error) {
			return nil, status.Error(codes.Internal, "failed to check safety")
		},
	}
	client, server := setupConnectedDEKClient(t, &minimalDEKServer{}, safetyServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.SafetyCheckRequest{}

	resp, err := client.KMSSafetyCheck(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSSafetyReport_NilRequest(t *testing.T) {
	safetyServer := &minimalSafetyServer{}
	client, server := setupConnectedDEKClient(t, &minimalDEKServer{}, safetyServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	// nil request should work and use defaults
	resp, err := client.KMSSafetyReport(ctx, nil)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Items, 2)
}

func TestClientKMSSafetyReport_Success(t *testing.T) {
	safetyServer := &minimalSafetyServer{}
	client, server := setupConnectedDEKClient(t, &minimalDEKServer{}, safetyServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.SafetyReportRequest{
		IncludeDetails: true,
	}

	resp, err := client.KMSSafetyReport(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Items, 2)
	assert.Equal(t, uint64(850000+0), resp.TotalOperations)        // only key has operations
	assert.Equal(t, uint64(0+500000000), resp.TotalBytesProcessed) // only dek has bytes
}

func TestClientKMSSafetyReport_Error(t *testing.T) {
	safetyServer := &minimalSafetyServer{
		safetyReportFunc: func(ctx context.Context, req *pb.SafetyReportRequest) (*pb.SafetyReportResponse, error) {
			return nil, status.Error(codes.Internal, "failed to generate report")
		},
	}
	client, server := setupConnectedDEKClient(t, &minimalDEKServer{}, safetyServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.SafetyReportRequest{}

	resp, err := client.KMSSafetyReport(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Conversion Helper Tests
// =============================================================================

func TestConvertProtoDEKMetadata(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name  string
		input *pb.DEKMetadata
	}{
		{
			name:  "nil_input",
			input: nil,
		},
		{
			name: "full_metadata",
			input: &pb.DEKMetadata{
				Path:           "/app/data",
				Algorithm:      pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
				Version:        2,
				Status:         "active",
				IsDefault:      true,
				CreatedAt:      now.Unix(),
				RotatedAt:      now.Add(-1 * time.Hour).Unix(),
				UsageCount:     5000,
				BytesUsed:      1073741824,
				MaxUsageCount:  1000000,
				MaxBytesUsed:   2147483648,
				WarningPercent: 80,
				Description:    "Test DEK",
				BackendType:    "software",
			},
		},
		{
			name: "minimal_metadata",
			input: &pb.DEKMetadata{
				Path:      "/app/data",
				Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
				Version:   1,
				CreatedAt: now.Unix(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoDEKMetadata(tt.input)
			if tt.input == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.input.Path, result.Path)
				assert.Equal(t, tt.input.Version, result.Version)
			}
		})
	}
}

func TestConvertProtoReencryptionProgress(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name  string
		input *pb.ReencryptionProgress
	}{
		{
			name:  "nil_input",
			input: nil,
		},
		{
			name: "in_progress",
			input: &pb.ReencryptionProgress{
				InProgress:          true,
				TotalKeys:           1000,
				ProcessedKeys:       500,
				FailedKeys:          5,
				StartedAt:           now.Add(-5 * time.Minute).Unix(),
				EstimatedCompletion: now.Add(10 * time.Minute).Unix(),
			},
		},
		{
			name: "completed",
			input: &pb.ReencryptionProgress{
				InProgress:    false,
				TotalKeys:     1000,
				ProcessedKeys: 1000,
				FailedKeys:    0,
				StartedAt:     now.Add(-10 * time.Minute).Unix(),
			},
		},
		{
			name: "no_timestamps",
			input: &pb.ReencryptionProgress{
				InProgress:    true,
				TotalKeys:     500,
				ProcessedKeys: 100,
			},
		},
		{
			name: "zero_total_keys",
			input: &pb.ReencryptionProgress{
				InProgress:    false,
				TotalKeys:     0,
				ProcessedKeys: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := convertProtoReencryptionProgress(tt.input)
			require.NoError(t, err)
			if tt.input == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.input.InProgress, result.InProgress)
				assert.Equal(t, uint64(tt.input.TotalKeys), result.TotalKeys)
				assert.Equal(t, uint64(tt.input.ProcessedKeys), result.ProcessedKeys)
			}
		})
	}
}

func TestConvertServiceKeyAlgorithmToProto(t *testing.T) {
	tests := []struct {
		input    service.KeyAlgorithm
		expected pb.KeyAlgorithm
	}{
		{service.KeyAlgAES128GCM, pb.KeyAlgorithm_KEY_ALG_AES_128_GCM},
		{service.KeyAlgAES192GCM, pb.KeyAlgorithm_KEY_ALG_AES_192_GCM},
		{service.KeyAlgAES256GCM, pb.KeyAlgorithm_KEY_ALG_AES_256_GCM},
		{service.KeyAlgChaCha20Poly1305, pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305},
		{service.KeyAlgXChaCha20Poly1305, pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305},
		{service.KeyAlgRSA2048, pb.KeyAlgorithm_KEY_ALG_RSA_2048},
		{service.KeyAlgRSA3072, pb.KeyAlgorithm_KEY_ALG_RSA_3072},
		{service.KeyAlgRSA4096, pb.KeyAlgorithm_KEY_ALG_RSA_4096},
		{service.KeyAlgECDSAP256, pb.KeyAlgorithm_KEY_ALG_ECDSA_P256},
		{service.KeyAlgECDSAP384, pb.KeyAlgorithm_KEY_ALG_ECDSA_P384},
		{service.KeyAlgECDSAP521, pb.KeyAlgorithm_KEY_ALG_ECDSA_P521},
		{service.KeyAlgEd25519, pb.KeyAlgorithm_KEY_ALG_ED25519},
		{service.KeyAlgEd448, pb.KeyAlgorithm_KEY_ALG_ED448},
		{service.KeyAlgX25519, pb.KeyAlgorithm_KEY_ALG_X25519},
		{service.KeyAlgX448, pb.KeyAlgorithm_KEY_ALG_X448},
		{service.KeyAlgHMACSHA256, pb.KeyAlgorithm_KEY_ALG_HMAC_SHA256},
		{service.KeyAlgHMACSHA384, pb.KeyAlgorithm_KEY_ALG_HMAC_SHA384},
		{service.KeyAlgHMACSHA512, pb.KeyAlgorithm_KEY_ALG_HMAC_SHA512},
		{service.KeyAlgorithm("UNKNOWN"), pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			result := convertServiceKeyAlgorithmToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
