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

// mockSecretsService implements service.SecretsService for testing.
type mockSecretsService struct {
	storeSecretFunc          func(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error)
	getSecretFunc            func(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error)
	listSecretsFunc          func(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error)
	deleteSecretFunc         func(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error)
	getSecretVersionFunc     func(ctx context.Context, req *service.GetSecretVersionRequest) (*service.GetSecretVersionResponse, error)
	listSecretVersionsFunc   func(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error)
	rollbackSecretFunc       func(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error)
	rotateSecretFunc         func(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error)
	setRotationPolicyFunc    func(ctx context.Context, req *service.SetRotationPolicyRequest) (*service.SetRotationPolicyResponse, error)
	getRotationPolicyFunc    func(ctx context.Context, req *service.GetRotationPolicyRequest) (*service.GetRotationPolicyResponse, error)
	updateSecretStatusFunc   func(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error)
	getSecretsByPrefixFunc   func(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error)
	watchSecretsByPrefixFunc func(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error)
}

func (m *mockSecretsService) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	if m.storeSecretFunc != nil {
		return m.storeSecretFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSecretsService) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	if m.getSecretFunc != nil {
		return m.getSecretFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSecretsService) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	if m.listSecretsFunc != nil {
		return m.listSecretsFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSecretsService) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	if m.deleteSecretFunc != nil {
		return m.deleteSecretFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSecretsService) GetSecretVersion(ctx context.Context, req *service.GetSecretVersionRequest) (*service.GetSecretVersionResponse, error) {
	if m.getSecretVersionFunc != nil {
		return m.getSecretVersionFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSecretsService) ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	if m.listSecretVersionsFunc != nil {
		return m.listSecretVersionsFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSecretsService) RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	if m.rollbackSecretFunc != nil {
		return m.rollbackSecretFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSecretsService) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	if m.rotateSecretFunc != nil {
		return m.rotateSecretFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSecretsService) SetRotationPolicy(ctx context.Context, req *service.SetRotationPolicyRequest) (*service.SetRotationPolicyResponse, error) {
	if m.setRotationPolicyFunc != nil {
		return m.setRotationPolicyFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSecretsService) GetRotationPolicy(ctx context.Context, req *service.GetRotationPolicyRequest) (*service.GetRotationPolicyResponse, error) {
	if m.getRotationPolicyFunc != nil {
		return m.getRotationPolicyFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSecretsService) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	if m.updateSecretStatusFunc != nil {
		return m.updateSecretStatusFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSecretsService) GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	if m.getSecretsByPrefixFunc != nil {
		return m.getSecretsByPrefixFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockSecretsService) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	if m.watchSecretsByPrefixFunc != nil {
		return m.watchSecretsByPrefixFunc(ctx, req)
	}
	return nil, nil
}

func TestStoreSecret(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.StoreSecretRequest
		mockFunc    func(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_store_secret",
			request: &pb.StoreSecretRequest{
				Name:        "database-password",
				Description: "Production database password",
				Data:        []byte("super-secret-password"),
				Metadata:    map[string]string{"env": "production"},
				Tags:        map[string]string{"type": "credential"},
			},
			mockFunc: func(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
				return &service.StoreSecretResponse{
					SecretID:  "secret-12345",
					Version:   1,
					Timestamp: now,
				}, nil
			},
			expectError: false,
		},
		{
			name: "missing_name",
			request: &pb.StoreSecretRequest{
				Data: []byte("secret-data"),
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_data",
			request: &pb.StoreSecretRequest{
				Name: "test-secret",
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_error_already_exists",
			request: &pb.StoreSecretRequest{
				Name: "existing-secret",
				Data: []byte("secret-data"),
			},
			mockFunc: func(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
				return nil, errors.New(errors.ErrAlreadyExists, "StoreSecret", "secret already exists")
			},
			expectError: true,
			errorCode:   codes.AlreadyExists,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockSecretsService{
				storeSecretFunc: tt.mockFunc,
			}
			server := NewSecretsServer(mockSvc, log)

			resp, err := server.StoreSecret(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok, "error should be a gRPC status error")
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.NotEmpty(t, resp.SecretId)
				assert.Equal(t, uint64(1), resp.Version)
			}
		})
	}
}

func TestGetSecret(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()
	storedAt := now.Add(-72 * time.Hour)

	tests := []struct {
		name        string
		request     *pb.GetSecretRequest
		mockFunc    func(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_get_secret",
			request: &pb.GetSecretRequest{
				SecretId: "secret-12345",
				Version:  1,
			},
			mockFunc: func(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
				return &service.GetSecretResponse{
					SecretID:  req.SecretID,
					Version:   req.Version,
					Data:      []byte("secret-data"),
					Metadata:  map[string]string{"env": "production"},
					CreatedAt: storedAt,
					Timestamp: now,
				}, nil
			},
			expectError: false,
		},
		{
			name:        "missing_secret_id",
			request:     &pb.GetSecretRequest{},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "secret_not_found",
			request: &pb.GetSecretRequest{
				SecretId: "non-existent-secret",
			},
			mockFunc: func(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "GetSecret", "secret not found")
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockSecretsService{
				getSecretFunc: tt.mockFunc,
			}
			server := NewSecretsServer(mockSvc, log)

			resp, err := server.GetSecret(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, "secret-12345", resp.SecretId)
				assert.NotEmpty(t, resp.Data)
				// created_at is the service's stored write time, carried
				// through unchanged, not the time of the read (#179).
				assert.Equal(t, storedAt.Unix(), resp.CreatedAt)
			}
		})
	}
}

func TestListSecrets(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name     string
		request  *pb.ListSecretsRequest
		mockFunc func(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error)
	}{
		{
			name: "successful_list_secrets",
			request: &pb.ListSecretsRequest{
				Limit:  10,
				Cursor: "",
				Filter: map[string]string{"env": "production"},
			},
			mockFunc: func(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
				return &service.ListSecretsResponse{
					Secrets: []*service.Secret{
						{
							SecretID:       "secret-1",
							Name:           "db-password",
							Description:    "Database password",
							CurrentVersion: 3,
							CreatedAt:      now,
							UpdatedAt:      now,
							Tags:           map[string]string{"type": "credential"},
						},
						{
							SecretID:       "secret-2",
							Name:           "api-key",
							Description:    "External API key",
							CurrentVersion: 1,
							CreatedAt:      now,
							UpdatedAt:      now,
							Tags:           map[string]string{"type": "api-key"},
						},
					},
					NextCursor: "cursor-123",
					HasMore:    true,
					TotalCount: 15,
					Timestamp:  now,
				}, nil
			},
		},
		{
			name: "empty_list",
			request: &pb.ListSecretsRequest{
				Limit: 10,
			},
			mockFunc: func(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
				return &service.ListSecretsResponse{
					Secrets:    []*service.Secret{},
					NextCursor: "",
					HasMore:    false,
					TotalCount: 0,
					Timestamp:  now,
				}, nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockSecretsService{
				listSecretsFunc: tt.mockFunc,
			}
			server := NewSecretsServer(mockSvc, log)

			resp, err := server.ListSecrets(context.Background(), tt.request)

			require.NoError(t, err)
			require.NotNil(t, resp)
			if tt.name == "successful_list_secrets" {
				assert.Len(t, resp.Secrets, 2)
				assert.True(t, resp.HasMore)
				assert.Equal(t, int32(15), resp.TotalCount)
			} else {
				assert.Empty(t, resp.Secrets)
				assert.False(t, resp.HasMore)
			}
		})
	}
}

func TestDeleteSecret(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.DeleteSecretRequest
		mockFunc    func(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_delete_secret",
			request: &pb.DeleteSecretRequest{
				SecretId:          "secret-12345",
				PermanentlyDelete: true,
			},
			mockFunc: func(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
				return &service.DeleteSecretResponse{
					SecretID:  req.SecretID,
					Deleted:   true,
					Timestamp: now,
				}, nil
			},
			expectError: false,
		},
		{
			name:        "missing_secret_id",
			request:     &pb.DeleteSecretRequest{},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockSecretsService{
				deleteSecretFunc: tt.mockFunc,
			}
			server := NewSecretsServer(mockSvc, log)

			resp, err := server.DeleteSecret(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.True(t, resp.Deleted)
			}
		})
	}
}

func TestGetSecretVersion(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.GetSecretVersionRequest
		mockFunc    func(ctx context.Context, req *service.GetSecretVersionRequest) (*service.GetSecretVersionResponse, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_get_secret_version",
			request: &pb.GetSecretVersionRequest{
				SecretId: "secret-12345",
				Version:  2,
			},
			mockFunc: func(ctx context.Context, req *service.GetSecretVersionRequest) (*service.GetSecretVersionResponse, error) {
				return &service.GetSecretVersionResponse{
					Version: &service.SecretVersion{
						SecretID:  req.SecretID,
						Version:   req.Version,
						Data:      []byte("version-2-data"),
						Metadata:  map[string]string{"version": "2"},
						CreatedAt: now,
						CreatedBy: "user-123",
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
		},
		{
			name:        "missing_secret_id",
			request:     &pb.GetSecretVersionRequest{Version: 1},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name:        "missing_version",
			request:     &pb.GetSecretVersionRequest{SecretId: "secret-12345"},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockSecretsService{
				getSecretVersionFunc: tt.mockFunc,
			}
			server := NewSecretsServer(mockSvc, log)

			resp, err := server.GetSecretVersion(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, uint64(2), resp.Version.Version)
			}
		})
	}
}

func TestListSecretVersions(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.ListSecretVersionsRequest
		mockFunc    func(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_list_secret_versions",
			request: &pb.ListSecretVersionsRequest{
				SecretId: "secret-12345",
				Limit:    10,
			},
			mockFunc: func(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
				return &service.ListSecretVersionsResponse{
					Versions: []*service.SecretVersion{
						{
							SecretID:  req.SecretID,
							Version:   3,
							CreatedAt: now,
							CreatedBy: "user-123",
						},
						{
							SecretID:  req.SecretID,
							Version:   2,
							CreatedAt: now.Add(-24 * time.Hour),
							CreatedBy: "user-123",
						},
						{
							SecretID:  req.SecretID,
							Version:   1,
							CreatedAt: now.Add(-48 * time.Hour),
							CreatedBy: "user-123",
						},
					},
					NextCursor: "",
					HasMore:    false,
					Timestamp:  now,
				}, nil
			},
			expectError: false,
		},
		{
			name:        "missing_secret_id",
			request:     &pb.ListSecretVersionsRequest{},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockSecretsService{
				listSecretVersionsFunc: tt.mockFunc,
			}
			server := NewSecretsServer(mockSvc, log)

			resp, err := server.ListSecretVersions(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Len(t, resp.Versions, 3)
			}
		})
	}
}

func TestRollbackSecret(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.RollbackSecretRequest
		mockFunc    func(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_rollback_secret",
			request: &pb.RollbackSecretRequest{
				SecretId:      "secret-12345",
				TargetVersion: 2,
			},
			mockFunc: func(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
				return &service.RollbackSecretResponse{
					SecretID:   req.SecretID,
					NewVersion: 4,
					Timestamp:  now,
				}, nil
			},
			expectError: false,
		},
		{
			name:        "missing_secret_id",
			request:     &pb.RollbackSecretRequest{TargetVersion: 1},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name:        "missing_target_version",
			request:     &pb.RollbackSecretRequest{SecretId: "secret-12345"},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockSecretsService{
				rollbackSecretFunc: tt.mockFunc,
			}
			server := NewSecretsServer(mockSvc, log)

			resp, err := server.RollbackSecret(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, uint64(4), resp.NewVersion)
			}
		})
	}
}

func TestRotateSecret(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.RotateSecretRequest
		mockFunc    func(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_rotate_secret",
			request: &pb.RotateSecretRequest{
				SecretId: "secret-12345",
				NewData:  []byte("new-secret-value"),
				Metadata: map[string]string{"rotated": "true"},
			},
			mockFunc: func(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
				return &service.RotateSecretResponse{
					SecretID:   req.SecretID,
					OldVersion: 2,
					NewVersion: 3,
					Timestamp:  now,
				}, nil
			},
			expectError: false,
		},
		{
			name:        "missing_secret_id",
			request:     &pb.RotateSecretRequest{NewData: []byte("data")},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name:        "missing_new_data",
			request:     &pb.RotateSecretRequest{SecretId: "secret-12345"},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockSecretsService{
				rotateSecretFunc: tt.mockFunc,
			}
			server := NewSecretsServer(mockSvc, log)

			resp, err := server.RotateSecret(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, uint64(2), resp.OldVersion)
				assert.Equal(t, uint64(3), resp.NewVersion)
			}
		})
	}
}

func TestSetRotationPolicy(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()
	nextRotation := now.Add(30 * 24 * time.Hour)

	tests := []struct {
		name        string
		request     *pb.SetRotationPolicyRequest
		mockFunc    func(ctx context.Context, req *service.SetRotationPolicyRequest) (*service.SetRotationPolicyResponse, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_set_rotation_policy",
			request: &pb.SetRotationPolicyRequest{
				SecretId: "secret-12345",
				Policy: &pb.SecretRotationPolicy{
					Enabled:         true,
					IntervalSeconds: 30 * 24 * 60 * 60,
					NextRotation:    nextRotation.Unix(),
					RetainVersions:  5,
					RotationLambda:  "arn:aws:lambda:us-east-1:123456789012:function:rotate-secret",
				},
			},
			mockFunc: func(ctx context.Context, req *service.SetRotationPolicyRequest) (*service.SetRotationPolicyResponse, error) {
				return &service.SetRotationPolicyResponse{
					SecretID:  req.SecretID,
					Policy:    req.Policy,
					Timestamp: now,
				}, nil
			},
			expectError: false,
		},
		{
			name:        "missing_secret_id",
			request:     &pb.SetRotationPolicyRequest{Policy: &pb.SecretRotationPolicy{}},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name:        "missing_policy",
			request:     &pb.SetRotationPolicyRequest{SecretId: "secret-12345"},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockSecretsService{
				setRotationPolicyFunc: tt.mockFunc,
			}
			server := NewSecretsServer(mockSvc, log)

			resp, err := server.SetRotationPolicy(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.True(t, resp.Policy.Enabled)
				assert.Equal(t, int32(5), resp.Policy.RetainVersions)
			}
		})
	}
}

func TestGetRotationPolicy(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()
	nextRotation := now.Add(30 * 24 * time.Hour)

	tests := []struct {
		name        string
		request     *pb.GetRotationPolicyRequest
		mockFunc    func(ctx context.Context, req *service.GetRotationPolicyRequest) (*service.GetRotationPolicyResponse, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_get_rotation_policy",
			request: &pb.GetRotationPolicyRequest{
				SecretId: "secret-12345",
			},
			mockFunc: func(ctx context.Context, req *service.GetRotationPolicyRequest) (*service.GetRotationPolicyResponse, error) {
				return &service.GetRotationPolicyResponse{
					SecretID: req.SecretID,
					Policy: &service.SecretRotationPolicy{
						Enabled:        true,
						Interval:       30 * 24 * time.Hour,
						NextRotation:   nextRotation,
						RetainVersions: 5,
						RotationLambda: "arn:aws:lambda:us-east-1:123456789012:function:rotate-secret",
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
		},
		{
			name:        "missing_secret_id",
			request:     &pb.GetRotationPolicyRequest{},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "policy_not_found",
			request: &pb.GetRotationPolicyRequest{
				SecretId: "secret-no-policy",
			},
			mockFunc: func(ctx context.Context, req *service.GetRotationPolicyRequest) (*service.GetRotationPolicyResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "GetRotationPolicy", "rotation policy not found")
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockSecretsService{
				getRotationPolicyFunc: tt.mockFunc,
			}
			server := NewSecretsServer(mockSvc, log)

			resp, err := server.GetRotationPolicy(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.True(t, resp.Policy.Enabled)
				assert.Equal(t, int32(5), resp.Policy.RetainVersions)
			}
		})
	}
}

func TestUpdateSecretStatus(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name           string
		request        *pb.UpdateSecretStatusRequest
		mockFunc       func(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error)
		expectError    bool
		errorCode      codes.Code
		expectedStatus pb.SecretStatus
	}{
		{
			name: "successful_enable_secret",
			request: &pb.UpdateSecretStatusRequest{
				SecretId: "secret-12345",
				Status:   pb.SecretStatus_SECRET_STATUS_ENABLED,
			},
			mockFunc: func(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
				return &service.UpdateSecretStatusResponse{
					SecretID:  req.SecretID,
					NewStatus: service.SecretStatusEnabled,
					Timestamp: now,
				}, nil
			},
			expectError:    false,
			expectedStatus: pb.SecretStatus_SECRET_STATUS_ENABLED,
		},
		{
			name: "successful_disable_secret",
			request: &pb.UpdateSecretStatusRequest{
				SecretId: "secret-12345",
				Status:   pb.SecretStatus_SECRET_STATUS_DISABLED,
			},
			mockFunc: func(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
				return &service.UpdateSecretStatusResponse{
					SecretID:  req.SecretID,
					NewStatus: service.SecretStatusDisabled,
					Timestamp: now,
				}, nil
			},
			expectError:    false,
			expectedStatus: pb.SecretStatus_SECRET_STATUS_DISABLED,
		},
		{
			name: "missing_secret_id",
			request: &pb.UpdateSecretStatusRequest{
				Status: pb.SecretStatus_SECRET_STATUS_ENABLED,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "invalid_status_unspecified",
			request: &pb.UpdateSecretStatusRequest{
				SecretId: "secret-12345",
				Status:   pb.SecretStatus_SECRET_STATUS_UNSPECIFIED,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "secret_not_found",
			request: &pb.UpdateSecretStatusRequest{
				SecretId: "non-existent-secret",
				Status:   pb.SecretStatus_SECRET_STATUS_ENABLED,
			},
			mockFunc: func(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "UpdateSecretStatus", "secret not found")
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
		{
			name: "service_internal_error",
			request: &pb.UpdateSecretStatusRequest{
				SecretId: "secret-12345",
				Status:   pb.SecretStatus_SECRET_STATUS_ENABLED,
			},
			mockFunc: func(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
				return nil, errors.New(errors.ErrInternal, "UpdateSecretStatus", "internal error occurred")
			},
			expectError: true,
			errorCode:   codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockSecretsService{
				updateSecretStatusFunc: tt.mockFunc,
			}
			server := NewSecretsServer(mockSvc, log)

			resp, err := server.UpdateSecretStatus(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok, "error should be a gRPC status error")
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, tt.request.SecretId, resp.SecretId)
				assert.Equal(t, tt.expectedStatus, resp.Status)
				assert.NotZero(t, resp.Timestamp)
			}
		})
	}
}
