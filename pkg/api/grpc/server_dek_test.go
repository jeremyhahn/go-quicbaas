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
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// mockKMSService implements service.KMSService for testing DEK operations.
type mockKMSService struct {
	createDEKFunc      func(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error)
	getDEKFunc         func(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error)
	listDEKsFunc       func(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error)
	rotateDEKFunc      func(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error)
	deleteDEKFunc      func(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error)
	getDEKStatusFunc   func(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error)
	setDefaultDEKFunc  func(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error)
	setDEKSafetyFunc   func(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error)
	clearDEKSafetyFunc func(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error)
}

func (m *mockKMSService) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	if m.createDEKFunc != nil {
		return m.createDEKFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockKMSService) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	if m.getDEKFunc != nil {
		return m.getDEKFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockKMSService) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	if m.listDEKsFunc != nil {
		return m.listDEKsFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockKMSService) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	if m.rotateDEKFunc != nil {
		return m.rotateDEKFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockKMSService) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	if m.deleteDEKFunc != nil {
		return m.deleteDEKFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockKMSService) GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	if m.getDEKStatusFunc != nil {
		return m.getDEKStatusFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockKMSService) SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	if m.setDefaultDEKFunc != nil {
		return m.setDefaultDEKFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockKMSService) SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	if m.setDEKSafetyFunc != nil {
		return m.setDEKSafetyFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockKMSService) ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	if m.clearDEKSafetyFunc != nil {
		return m.clearDEKSafetyFunc(ctx, req)
	}
	return nil, nil
}

// Stub implementations for other KMSService methods
func (m *mockKMSService) GetSupportedAlgorithms(ctx context.Context) ([]string, error) {
	return nil, nil
}
func (m *mockKMSService) GenerateKey(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
	return nil, nil
}
func (m *mockKMSService) ImportKey(ctx context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
	return nil, nil
}
func (m *mockKMSService) GetKey(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
	return nil, nil
}
func (m *mockKMSService) GetPublicKey(ctx context.Context, req *service.GetPublicKeyRequest) (*service.GetPublicKeyResponse, error) {
	return nil, nil
}
func (m *mockKMSService) ListKeys(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error) {
	return nil, nil
}
func (m *mockKMSService) RotateKey(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
	return nil, nil
}
func (m *mockKMSService) RevokeKey(ctx context.Context, req *service.RevokeKeyRequest) (*service.RevokeKeyResponse, error) {
	return nil, nil
}
func (m *mockKMSService) DeleteKey(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
	return nil, nil
}
func (m *mockKMSService) GetKeyMetadata(ctx context.Context, req *service.GetKeyMetadataRequest) (*service.GetKeyMetadataResponse, error) {
	return nil, nil
}
func (m *mockKMSService) UpdateKeyMetadata(ctx context.Context, req *service.UpdateKeyMetadataRequest) (*service.UpdateKeyMetadataResponse, error) {
	return nil, nil
}
func (m *mockKMSService) UpdateKeyStatus(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
	return nil, nil
}
func (m *mockKMSService) ListKeyVersions(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
	return nil, nil
}
func (m *mockKMSService) SetKeyPolicy(ctx context.Context, req *service.SetKeyPolicyRequest) (*service.SetKeyPolicyResponse, error) {
	return nil, nil
}
func (m *mockKMSService) GetKeyPolicy(ctx context.Context, req *service.GetKeyPolicyRequest) (*service.GetKeyPolicyResponse, error) {
	return nil, nil
}
func (m *mockKMSService) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	return nil, nil
}
func (m *mockKMSService) GetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	return nil, nil
}
func (m *mockKMSService) BackendStatus(ctx context.Context) (*service.BackendStatusResponse, error) {
	return nil, nil
}
func (m *mockKMSService) BackendTest(ctx context.Context) (*service.BackendTestResponse, error) {
	return nil, nil
}
func (m *mockKMSService) ExportKey(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
	return nil, nil
}
func (m *mockKMSService) UnwrapKey(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
	return nil, nil
}
func (m *mockKMSService) SetKeySafety(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
	return nil, nil
}
func (m *mockKMSService) ClearKeySafety(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
	return nil, nil
}
func (m *mockKMSService) SafetyCheck(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
	return nil, nil
}
func (m *mockKMSService) SafetyReport(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
	return nil, nil
}

// Ensure mockKMSService implements service.KMSService
var _ service.KMSService = (*mockKMSService)(nil)

// =============================================================================
// DEK Server Tests
// =============================================================================

func TestNewDEKServer(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	mockSvc := &mockKMSService{}

	server := NewDEKServer(mockSvc, log)

	require.NotNil(t, server)
	assert.NotNil(t, server.kmsSvc)
	assert.NotNil(t, server.logger)
}

func TestDEKServer_CreateDEK(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.CreateDEKRequest
		mockFunc    func(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_create_dek",
			request: &pb.CreateDEKRequest{
				Path:           "/app/data",
				Algorithm:      pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
				Description:    "Test DEK",
				MaxOperations:  1000000,
				MaxBytes:       1073741824, // 1GB
				WarningPercent: 80,
			},
			mockFunc: func(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
				return &service.CreateDEKResponse{
					DEK: &service.DEKMetadata{
						Path:           req.Path,
						Algorithm:      service.KeyAlgAES256GCM,
						Version:        1,
						Status:         "active",
						IsDefault:      true,
						CreatedAt:      now,
						MaxUsageCount:  req.MaxOperations,
						MaxBytesUsed:   req.MaxBytes,
						WarningPercent: req.WarningPercent,
						Description:    req.Description,
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
		},
		{
			name: "missing_path",
			request: &pb.CreateDEKRequest{
				Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
			},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_error",
			request: &pb.CreateDEKRequest{
				Path:      "/app/data",
				Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
			},
			mockFunc: func(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
				return nil, errors.New(errors.ErrAlreadyExists, "CreateDEK", "DEK already exists at path")
			},
			expectError: true,
			errorCode:   codes.AlreadyExists,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockKMSService{
				createDEKFunc: tt.mockFunc,
			}
			server := NewDEKServer(mockSvc, log)

			resp, err := server.CreateDEK(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.NotNil(t, resp.Dek)
				assert.Equal(t, tt.request.Path, resp.Dek.Path)
			}
		})
	}
}

func TestDEKServer_GetDEK(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.GetDEKRequest
		mockFunc    func(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_get_dek",
			request: &pb.GetDEKRequest{
				Path: "/app/data",
			},
			mockFunc: func(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
				return &service.GetDEKResponse{
					DEK: &service.DEKMetadata{
						Path:       req.Path,
						Algorithm:  service.KeyAlgAES256GCM,
						Version:    1,
						Status:     "active",
						IsDefault:  true,
						CreatedAt:  now,
						UsageCount: 5000,
						BytesUsed:  1024 * 1024 * 100, // 100MB
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
		},
		{
			name:        "missing_path",
			request:     &pb.GetDEKRequest{},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "dek_not_found",
			request: &pb.GetDEKRequest{
				Path: "/nonexistent",
			},
			mockFunc: func(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "GetDEK", "DEK not found")
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockKMSService{
				getDEKFunc: tt.mockFunc,
			}
			server := NewDEKServer(mockSvc, log)

			resp, err := server.GetDEK(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.NotNil(t, resp.Dek)
				assert.Equal(t, tt.request.Path, resp.Dek.Path)
			}
		})
	}
}

func TestDEKServer_ListDEKs(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.ListDEKsRequest
		mockFunc    func(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error)
		expectCount int
	}{
		{
			name: "successful_list_deks",
			request: &pb.ListDEKsRequest{
				Prefix:    "/app",
				Recursive: true,
				Limit:     10,
			},
			mockFunc: func(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
				return &service.ListDEKsResponse{
					DEKs: []*service.DEKMetadata{
						{
							Path:      "/app/data1",
							Algorithm: service.KeyAlgAES256GCM,
							Version:   1,
							CreatedAt: now,
						},
						{
							Path:      "/app/data2",
							Algorithm: service.KeyAlgAES256GCM,
							Version:   1,
							CreatedAt: now,
						},
					},
					NextCursor: "",
					HasMore:    false,
					Timestamp:  now,
				}, nil
			},
			expectCount: 2,
		},
		{
			name: "empty_list",
			request: &pb.ListDEKsRequest{
				Prefix: "/empty",
			},
			mockFunc: func(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
				return &service.ListDEKsResponse{
					DEKs:      []*service.DEKMetadata{},
					Timestamp: now,
				}, nil
			},
			expectCount: 0,
		},
		{
			name: "paginated_list",
			request: &pb.ListDEKsRequest{
				Prefix: "/app",
				Limit:  1,
			},
			mockFunc: func(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
				return &service.ListDEKsResponse{
					DEKs: []*service.DEKMetadata{
						{
							Path:      "/app/data1",
							Algorithm: service.KeyAlgAES256GCM,
							Version:   1,
							CreatedAt: now,
						},
					},
					NextCursor: "/app/data2",
					HasMore:    true,
					Timestamp:  now,
				}, nil
			},
			expectCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockKMSService{
				listDEKsFunc: tt.mockFunc,
			}
			server := NewDEKServer(mockSvc, log)

			resp, err := server.ListDEKs(context.Background(), tt.request)

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Len(t, resp.Deks, tt.expectCount)
		})
	}
}

func TestDEKServer_RotateDEK(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.RotateDEKRequest
		mockFunc    func(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_rotate_dek",
			request: &pb.RotateDEKRequest{
				Path:       "/app/data",
				Background: false,
			},
			mockFunc: func(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
				return &service.RotateDEKResponse{
					Path:       req.Path,
					OldVersion: 1,
					NewVersion: 2,
					Timestamp:  now,
				}, nil
			},
			expectError: false,
		},
		{
			name:        "missing_path",
			request:     &pb.RotateDEKRequest{},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "dek_not_found",
			request: &pb.RotateDEKRequest{
				Path: "/nonexistent",
			},
			mockFunc: func(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "RotateDEK", "DEK not found")
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockKMSService{
				rotateDEKFunc: tt.mockFunc,
			}
			server := NewDEKServer(mockSvc, log)

			resp, err := server.RotateDEK(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, tt.request.Path, resp.Path)
				assert.Equal(t, uint64(1), resp.OldVersion)
				assert.Equal(t, uint64(2), resp.NewVersion)
			}
		})
	}
}

func TestDEKServer_DeleteDEK(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.DeleteDEKRequest
		mockFunc    func(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_delete_dek_force",
			request: &pb.DeleteDEKRequest{
				Path:  "/app/data",
				Force: true,
			},
			mockFunc: func(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
				return &service.DeleteDEKResponse{
					Path:      req.Path,
					Deleted:   true,
					Timestamp: now,
				}, nil
			},
			expectError: false,
		},
		{
			name: "successful_delete_dek_migrate",
			request: &pb.DeleteDEKRequest{
				Path:      "/app/old-data",
				MigrateTo: "/app/new-data",
			},
			mockFunc: func(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
				return &service.DeleteDEKResponse{
					Path:      req.Path,
					Deleted:   true,
					Timestamp: now,
				}, nil
			},
			expectError: false,
		},
		{
			name:        "missing_path",
			request:     &pb.DeleteDEKRequest{},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "delete_without_force_or_migrate",
			request: &pb.DeleteDEKRequest{
				Path: "/app/data",
			},
			mockFunc: func(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
				return nil, errors.New(errors.ErrInvalidInput, "DeleteDEK", "must specify force=true or migrate_to")
			},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockKMSService{
				deleteDEKFunc: tt.mockFunc,
			}
			server := NewDEKServer(mockSvc, log)

			resp, err := server.DeleteDEK(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, tt.request.Path, resp.Path)
				assert.True(t, resp.Deleted)
			}
		})
	}
}

func TestDEKServer_GetDEKStatus(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.GetDEKStatusRequest
		mockFunc    func(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_get_dek_status",
			request: &pb.GetDEKStatusRequest{
				Path: "/app/data",
			},
			mockFunc: func(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
				return &service.GetDEKStatusResponse{
					DEK: &service.DEKMetadata{
						Path:          req.Path,
						Algorithm:     service.KeyAlgAES256GCM,
						Version:       1,
						Status:        "active",
						UsageCount:    500000,
						BytesUsed:     1073741824, // 1GB
						MaxUsageCount: 1000000,
						MaxBytesUsed:  2147483648, // 2GB
					},
					ReencryptionProgress: nil,
					Timestamp:            now,
				}, nil
			},
			expectError: false,
		},
		{
			name: "status_with_reencryption_progress",
			request: &pb.GetDEKStatusRequest{
				Path: "/app/data",
			},
			mockFunc: func(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
				startedAt := now.Add(-5 * time.Minute)
				estimatedCompletion := now.Add(10 * time.Minute)
				return &service.GetDEKStatusResponse{
					DEK: &service.DEKMetadata{
						Path:      req.Path,
						Algorithm: service.KeyAlgAES256GCM,
						Version:   2,
						Status:    "rotating",
					},
					ReencryptionProgress: &service.ReencryptionProgress{
						InProgress:          true,
						TotalKeys:           1000,
						ProcessedKeys:       500,
						FailedKeys:          5,
						StartedAt:           &startedAt,
						EstimatedCompletion: &estimatedCompletion,
						PercentComplete:     50.0,
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
		},
		{
			name:        "missing_path",
			request:     &pb.GetDEKStatusRequest{},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockKMSService{
				getDEKStatusFunc: tt.mockFunc,
			}
			server := NewDEKServer(mockSvc, log)

			resp, err := server.GetDEKStatus(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.NotNil(t, resp.Dek)
				assert.Equal(t, tt.request.Path, resp.Dek.Path)
			}
		})
	}
}

func TestDEKServer_SetDefaultDEK(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.SetDefaultDEKRequest
		mockFunc    func(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_set_default_dek",
			request: &pb.SetDefaultDEKRequest{
				Path: "/app/data",
			},
			mockFunc: func(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
				return &service.SetDefaultDEKResponse{
					Path:      req.Path,
					Timestamp: now,
				}, nil
			},
			expectError: false,
		},
		{
			name:        "missing_path",
			request:     &pb.SetDefaultDEKRequest{},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "dek_not_found",
			request: &pb.SetDefaultDEKRequest{
				Path: "/nonexistent",
			},
			mockFunc: func(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "SetDefaultDEK", "DEK not found")
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockKMSService{
				setDefaultDEKFunc: tt.mockFunc,
			}
			server := NewDEKServer(mockSvc, log)

			resp, err := server.SetDefaultDEK(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, tt.request.Path, resp.Path)
			}
		})
	}
}

func TestDEKServer_SetDEKSafety(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.SetDEKSafetyRequest
		mockFunc    func(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_set_dek_safety",
			request: &pb.SetDEKSafetyRequest{
				Path:           "/app/data",
				MaxOperations:  500000,
				MaxBytes:       1073741824, // 1GB
				WarningPercent: 75,
			},
			mockFunc: func(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
				return &service.SetDEKSafetyResponse{
					Path: req.Path,
					Config: &service.DEKSafetyConfig{
						MaxOperations:  req.MaxOperations,
						MaxBytes:       req.MaxBytes,
						WarningPercent: req.WarningPercent,
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
		},
		{
			name: "missing_path",
			request: &pb.SetDEKSafetyRequest{
				MaxOperations: 500000,
			},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockKMSService{
				setDEKSafetyFunc: tt.mockFunc,
			}
			server := NewDEKServer(mockSvc, log)

			resp, err := server.SetDEKSafety(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, tt.request.Path, resp.Path)
			}
		})
	}
}

func TestDEKServer_ClearDEKSafety(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.ClearDEKSafetyRequest
		mockFunc    func(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_clear_dek_safety",
			request: &pb.ClearDEKSafetyRequest{
				Path: "/app/data",
			},
			mockFunc: func(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
				return &service.ClearDEKSafetyResponse{
					Path:      req.Path,
					Cleared:   true,
					Timestamp: now,
				}, nil
			},
			expectError: false,
		},
		{
			name:        "missing_path",
			request:     &pb.ClearDEKSafetyRequest{},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockKMSService{
				clearDEKSafetyFunc: tt.mockFunc,
			}
			server := NewDEKServer(mockSvc, log)

			resp, err := server.ClearDEKSafety(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, tt.request.Path, resp.Path)
			}
		})
	}
}

// =============================================================================
// Conversion Helper Tests
// =============================================================================

func TestConvertDEKMetadata(t *testing.T) {
	now := time.Now()
	rotatedAt := now.Add(-1 * time.Hour)

	tests := []struct {
		name  string
		input *service.DEKMetadata
	}{
		{
			name:  "nil_input",
			input: nil,
		},
		{
			name: "valid_conversion",
			input: &service.DEKMetadata{
				Path:           "/app/data",
				Algorithm:      service.KeyAlgAES256GCM,
				Version:        2,
				Status:         "active",
				IsDefault:      true,
				CreatedAt:      now,
				RotatedAt:      &rotatedAt,
				UsageCount:     5000,
				BytesUsed:      1024 * 1024 * 100,
				MaxUsageCount:  1000000,
				MaxBytesUsed:   1073741824,
				WarningPercent: 80,
				Description:    "Test DEK",
				BackendType:    "software",
			},
		},
		{
			name: "conversion_without_rotated_at",
			input: &service.DEKMetadata{
				Path:      "/app/data",
				Algorithm: service.KeyAlgAES256GCM,
				Version:   1,
				Status:    "active",
				CreatedAt: now,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertDEKMetadata(tt.input)
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

func TestConvertReencryptionProgress(t *testing.T) {
	now := time.Now()
	startedAt := now.Add(-5 * time.Minute)
	estimatedCompletion := now.Add(10 * time.Minute)

	tests := []struct {
		name  string
		input *service.ReencryptionProgress
	}{
		{
			name:  "nil_input",
			input: nil,
		},
		{
			name: "in_progress",
			input: &service.ReencryptionProgress{
				InProgress:          true,
				TotalKeys:           1000,
				ProcessedKeys:       500,
				FailedKeys:          5,
				StartedAt:           &startedAt,
				EstimatedCompletion: &estimatedCompletion,
				PercentComplete:     50.0,
			},
		},
		{
			name: "completed",
			input: &service.ReencryptionProgress{
				InProgress:      false,
				TotalKeys:       1000,
				ProcessedKeys:   1000,
				FailedKeys:      0,
				StartedAt:       &startedAt,
				PercentComplete: 100.0,
			},
		},
		{
			name: "without_timestamps",
			input: &service.ReencryptionProgress{
				InProgress:    true,
				TotalKeys:     500,
				ProcessedKeys: 100,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertReencryptionProgress(tt.input)
			if tt.input == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.input.InProgress, result.InProgress)
				assert.Equal(t, int32(tt.input.TotalKeys), result.TotalKeys)
				assert.Equal(t, int32(tt.input.ProcessedKeys), result.ProcessedKeys)
			}
		})
	}
}

// --- Merged from server_dek_additional_test.go ---

// =============================================================================
// GetDEKStatus Additional Tests
// =============================================================================

func TestDEKServer_GetDEKStatus_ServiceError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})

	mockSvc := &mockKMSService{
		getDEKStatusFunc: func(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
			return nil, &errors.DragonError{
				Code: errors.ErrNotFound,
				Op:   "GetDEKStatus",
				Err:  assert.AnError,
			}
		},
	}
	server := NewDEKServer(mockSvc, log)

	resp, err := server.GetDEKStatus(context.Background(), &pb.GetDEKStatusRequest{
		Path: "/app/data",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

// =============================================================================
// SetDEKSafety Additional Tests
// =============================================================================

func TestDEKServer_SetDEKSafety_ServiceError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})

	mockSvc := &mockKMSService{
		setDEKSafetyFunc: func(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
			return nil, &errors.DragonError{
				Code: errors.ErrInternal,
				Op:   "SetDEKSafety",
				Err:  assert.AnError,
			}
		},
	}
	server := NewDEKServer(mockSvc, log)

	resp, err := server.SetDEKSafety(context.Background(), &pb.SetDEKSafetyRequest{
		Path:           "/app/data",
		MaxOperations:  1000000,
		MaxBytes:       2147483648,
		WarningPercent: 80,
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

// =============================================================================
// ClearDEKSafety Additional Tests
// =============================================================================

func TestDEKServer_ClearDEKSafety_ServiceError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})

	mockSvc := &mockKMSService{
		clearDEKSafetyFunc: func(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
			return nil, &errors.DragonError{
				Code: errors.ErrUnavailable,
				Op:   "ClearDEKSafety",
				Err:  assert.AnError,
			}
		},
	}
	server := NewDEKServer(mockSvc, log)

	resp, err := server.ClearDEKSafety(context.Background(), &pb.ClearDEKSafetyRequest{
		Path: "/app/data",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unavailable, st.Code())
}

// =============================================================================
// ListDEKs Additional Tests
// =============================================================================

func TestDEKServer_ListDEKs_ServiceError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})

	mockSvc := &mockKMSService{
		listDEKsFunc: func(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
			return nil, &errors.DragonError{
				Code: errors.ErrInternal,
				Op:   "ListDEKs",
				Err:  assert.AnError,
			}
		},
	}
	server := NewDEKServer(mockSvc, log)

	resp, err := server.ListDEKs(context.Background(), &pb.ListDEKsRequest{
		Prefix: "/app",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

// =============================================================================
// convertReencryptionProgress Additional Tests - Bounds Checking
// =============================================================================

func TestConvertReencryptionProgress_LargeValues(t *testing.T) {
	t.Run("values_exceeding_max_int32", func(t *testing.T) {
		// Values that exceed MaxInt32 should be clamped
		largeValue := uint64(math.MaxInt32) + 1000

		input := &service.ReencryptionProgress{
			InProgress:    true,
			TotalKeys:     largeValue,
			ProcessedKeys: largeValue,
			FailedKeys:    largeValue,
		}

		result := convertReencryptionProgress(input)

		require.NotNil(t, result)
		// Values should be clamped to MaxInt32
		assert.Equal(t, int32(math.MaxInt32), result.TotalKeys)
		assert.Equal(t, int32(math.MaxInt32), result.ProcessedKeys)
		assert.Equal(t, int32(math.MaxInt32), result.FailedKeys)
	})

	t.Run("values_at_max_int32", func(t *testing.T) {
		// Values exactly at MaxInt32 should not be changed
		input := &service.ReencryptionProgress{
			InProgress:    true,
			TotalKeys:     uint64(math.MaxInt32),
			ProcessedKeys: uint64(math.MaxInt32),
			FailedKeys:    uint64(math.MaxInt32),
		}

		result := convertReencryptionProgress(input)

		require.NotNil(t, result)
		assert.Equal(t, int32(math.MaxInt32), result.TotalKeys)
		assert.Equal(t, int32(math.MaxInt32), result.ProcessedKeys)
		assert.Equal(t, int32(math.MaxInt32), result.FailedKeys)
	})

	t.Run("values_below_max_int32", func(t *testing.T) {
		// Normal values should be converted directly
		input := &service.ReencryptionProgress{
			InProgress:    false,
			TotalKeys:     1000,
			ProcessedKeys: 999,
			FailedKeys:    1,
		}

		result := convertReencryptionProgress(input)

		require.NotNil(t, result)
		assert.Equal(t, int32(1000), result.TotalKeys)
		assert.Equal(t, int32(999), result.ProcessedKeys)
		assert.Equal(t, int32(1), result.FailedKeys)
	})
}
