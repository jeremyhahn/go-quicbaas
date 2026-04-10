//go:build mcp

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

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestExecutePut tests the executePut tool handler.
func TestExecutePut(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful put",
			args: map[string]interface{}{
				"entityType": "user",
				"id":         float64(123),
				"data":       `{"name":"test"}`,
				"shardID":    float64(1),
			},
			setupMock: func(m *mockService) {
				m.putFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
					return &service.PutResponse{
						ID:      123,
						Version: 1,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "put service error",
			args: map[string]interface{}{
				"entityType": "user",
				"id":         float64(123),
				"data":       `{"name":"test"}`,
				"shardID":    float64(1),
			},
			setupMock: func(m *mockService) {
				m.putFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
					return nil, &errors.MessageError{Message: "storage error"}
				}
			},
			wantErr: true,
			errMsg:  "storage error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executePut(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

// TestExecuteGet tests the executeGet tool handler.
func TestExecuteGet(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful get",
			args: map[string]interface{}{
				"entityType": "user",
				"id":         float64(123),
				"shardID":    float64(1),
			},
			setupMock: func(m *mockService) {
				m.getFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
					return &service.GetResponse{
						ID:    123,
						Data:  []byte(`{"name":"test"}`),
						Found: true,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "get service error",
			args: map[string]interface{}{
				"entityType": "user",
				"id":         float64(123),
				"shardID":    float64(1),
			},
			setupMock: func(m *mockService) {
				m.getFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
					return nil, &errors.MessageError{Message: "not found"}
				}
			},
			wantErr: true,
			errMsg:  "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeGet(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

// TestExecuteDelete tests the executeDelete tool handler.
func TestExecuteDelete(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful delete",
			args: map[string]interface{}{
				"entityType": "user",
				"id":         float64(123),
				"shardID":    float64(1),
			},
			setupMock: func(m *mockService) {
				m.deleteFunc = func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
					return &service.DeleteResponse{
						Deleted: true,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "delete service error",
			args: map[string]interface{}{
				"entityType": "user",
				"id":         float64(123),
				"shardID":    float64(1),
			},
			setupMock: func(m *mockService) {
				m.deleteFunc = func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
					return nil, &errors.MessageError{Message: "delete failed"}
				}
			},
			wantErr: true,
			errMsg:  "delete failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeDelete(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

// TestExecuteClusterOperations tests cluster-related tool handlers.
func TestExecuteClusterStatus(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful cluster status",
			args: map[string]interface{}{
				"includeDetails": true,
			},
			setupMock: func(m *mockService) {
				m.clusterStatusFunc = func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
					return &service.ClusterStatusResponse{}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "cluster status error",
			args: map[string]interface{}{},
			setupMock: func(m *mockService) {
				m.clusterStatusFunc = func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
					return nil, &errors.MessageError{Message: "cluster unavailable"}
				}
			},
			wantErr: true,
			errMsg:  "cluster unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeClusterStatus(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestExecuteGetLeader(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful get leader",
			args: map[string]interface{}{
				"shardID": float64(1),
			},
			setupMock: func(m *mockService) {
				m.getLeaderFunc = func(ctx context.Context, req *service.GetLeaderRequest) (*service.GetLeaderResponse, error) {
					return &service.GetLeaderResponse{
						LeaderID: 1,
						ShardID:  1,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "get leader error",
			args: map[string]interface{}{
				"shardID": float64(1),
			},
			setupMock: func(m *mockService) {
				m.getLeaderFunc = func(ctx context.Context, req *service.GetLeaderRequest) (*service.GetLeaderResponse, error) {
					return nil, &errors.MessageError{Message: "no leader"}
				}
			},
			wantErr: true,
			errMsg:  "no leader",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeGetLeader(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestExecuteAddNode(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful add node",
			args: map[string]interface{}{
				"nodeID":  float64(2),
				"address": "localhost:5001",
				"shardID": float64(1),
			},
			setupMock: func(m *mockService) {
				m.addNodeFunc = func(ctx context.Context, req *service.AddNodeRequest) (*service.AddNodeResponse, error) {
					return &service.AddNodeResponse{
						Success: true,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "add node error",
			args: map[string]interface{}{
				"nodeID":  float64(2),
				"address": "localhost:5001",
				"shardID": float64(1),
			},
			setupMock: func(m *mockService) {
				m.addNodeFunc = func(ctx context.Context, req *service.AddNodeRequest) (*service.AddNodeResponse, error) {
					return nil, &errors.MessageError{Message: "node exists"}
				}
			},
			wantErr: true,
			errMsg:  "node exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeAddNode(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestExecuteRemoveNode(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful remove node",
			args: map[string]interface{}{
				"nodeID":  float64(2),
				"shardID": float64(1),
			},
			setupMock: func(m *mockService) {
				m.removeNodeFunc = func(ctx context.Context, req *service.RemoveNodeRequest) (*service.RemoveNodeResponse, error) {
					return &service.RemoveNodeResponse{
						Success: true,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "remove node error",
			args: map[string]interface{}{
				"nodeID":  float64(2),
				"shardID": float64(1),
			},
			setupMock: func(m *mockService) {
				m.removeNodeFunc = func(ctx context.Context, req *service.RemoveNodeRequest) (*service.RemoveNodeResponse, error) {
					return nil, &errors.MessageError{Message: "node not found"}
				}
			},
			wantErr: true,
			errMsg:  "node not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeRemoveNode(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

// TestExecuteBackupOperations tests backup-related tool handlers.
func TestExecuteBackupList(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful list backups",
			args: map[string]interface{}{},
			setupMock: func(m *mockService) {
				m.listBackupsFunc = func(ctx context.Context, req *service.ListBackupsRequest) (*service.ListBackupsResponse, error) {
					return &service.ListBackupsResponse{
						Backups: []*service.BackupMetadata{{BackupID: "backup1", Name: "backup1"}, {BackupID: "backup2", Name: "backup2"}},
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "list backups error",
			args: map[string]interface{}{},
			setupMock: func(m *mockService) {
				m.listBackupsFunc = func(ctx context.Context, req *service.ListBackupsRequest) (*service.ListBackupsResponse, error) {
					return nil, &errors.MessageError{Message: "backup service down"}
				}
			},
			wantErr: true,
			errMsg:  "backup service down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeBackupList(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestExecuteBackupDelete(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful delete backup",
			args: map[string]interface{}{
				"backupID": "backup1",
			},
			setupMock: func(m *mockService) {
				m.deleteBackupFunc = func(ctx context.Context, req *service.DeleteBackupRequest) (*service.DeleteBackupResponse, error) {
					return &service.DeleteBackupResponse{
						Success: true,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "delete backup error",
			args: map[string]interface{}{
				"backupID": "backup1",
			},
			setupMock: func(m *mockService) {
				m.deleteBackupFunc = func(ctx context.Context, req *service.DeleteBackupRequest) (*service.DeleteBackupResponse, error) {
					return nil, &errors.MessageError{Message: "backup not found"}
				}
			},
			wantErr: true,
			errMsg:  "backup not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeBackupDelete(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestExecuteBackupVerify(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful verify backup",
			args: map[string]interface{}{
				"backupID": "backup1",
			},
			setupMock: func(m *mockService) {
				m.verifyBackupFunc = func(ctx context.Context, req *service.VerifyBackupRequest) (*service.VerifyBackupResponse, error) {
					return &service.VerifyBackupResponse{
						Valid: true,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "verify backup error",
			args: map[string]interface{}{
				"backupID": "backup1",
			},
			setupMock: func(m *mockService) {
				m.verifyBackupFunc = func(ctx context.Context, req *service.VerifyBackupRequest) (*service.VerifyBackupResponse, error) {
					return nil, &errors.MessageError{Message: "backup corrupt"}
				}
			},
			wantErr: true,
			errMsg:  "backup corrupt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeBackupVerify(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

// TestExecuteAuthOperations tests authentication-related tool handlers.
func TestExecuteLogin(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful login",
			args: map[string]interface{}{
				"username": "admin",
				"password": "secret",
			},
			setupMock: func(m *mockService) {
				m.loginFunc = func(ctx context.Context, req *service.LoginRequest) (*service.LoginResponse, error) {
					return &service.LoginResponse{
						AccessToken: "token123", UserID: "user1",
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "login error",
			args: map[string]interface{}{
				"username": "admin",
				"password": "wrong",
			},
			setupMock: func(m *mockService) {
				m.loginFunc = func(ctx context.Context, req *service.LoginRequest) (*service.LoginResponse, error) {
					return nil, &errors.MessageError{Message: "invalid credentials"}
				}
			},
			wantErr: true,
			errMsg:  "invalid credentials",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeLogin(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestExecuteLogout(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful logout",
			args: map[string]interface{}{
				"token": "token123",
			},
			setupMock: func(m *mockService) {
				m.logoutFunc = func(ctx context.Context, req *service.LogoutRequest) (*service.LogoutResponse, error) {
					return &service.LogoutResponse{
						Success: true,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "logout error",
			args: map[string]interface{}{
				"token": "invalid",
			},
			setupMock: func(m *mockService) {
				m.logoutFunc = func(ctx context.Context, req *service.LogoutRequest) (*service.LogoutResponse, error) {
					return nil, &errors.MessageError{Message: "invalid token"}
				}
			},
			wantErr: true,
			errMsg:  "invalid token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeLogout(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

// TestExecuteRBACOperations tests RBAC-related tool handlers.
func TestExecuteUserDelete(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful user delete",
			args: map[string]interface{}{
				"username": "user1",
			},
			setupMock: func(m *mockService) {
				m.deleteUserFunc = func(ctx context.Context, req *service.DeleteUserRequest) (*service.DeleteUserResponse, error) {
					return &service.DeleteUserResponse{
						Success: true,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "user delete error",
			args: map[string]interface{}{
				"username": "user1",
			},
			setupMock: func(m *mockService) {
				m.deleteUserFunc = func(ctx context.Context, req *service.DeleteUserRequest) (*service.DeleteUserResponse, error) {
					return nil, &errors.MessageError{Message: "user not found"}
				}
			},
			wantErr: true,
			errMsg:  "user not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeUserDelete(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestExecuteUserList(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful user list",
			args: map[string]interface{}{},
			setupMock: func(m *mockService) {
				m.listUsersFunc = func(ctx context.Context, req *service.ListUsersRequest) (*service.ListUsersResponse, error) {
					return &service.ListUsersResponse{
						Users: []*service.UserInfo{{UserID: "user1", Username: "user1"}, {UserID: "user2", Username: "user2"}},
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "user list error",
			args: map[string]interface{}{},
			setupMock: func(m *mockService) {
				m.listUsersFunc = func(ctx context.Context, req *service.ListUsersRequest) (*service.ListUsersResponse, error) {
					return nil, &errors.MessageError{Message: "auth service down"}
				}
			},
			wantErr: true,
			errMsg:  "auth service down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeUserList(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestExecuteRoleDelete(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful role delete",
			args: map[string]interface{}{
				"role": "admin",
			},
			setupMock: func(m *mockService) {
				m.deleteRoleFunc = func(ctx context.Context, req *service.AuthDeleteRoleRequest) (*service.AuthDeleteRoleResponse, error) {
					return &service.AuthDeleteRoleResponse{
						Success: true,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "role delete error",
			args: map[string]interface{}{
				"role": "admin",
			},
			setupMock: func(m *mockService) {
				m.deleteRoleFunc = func(ctx context.Context, req *service.AuthDeleteRoleRequest) (*service.AuthDeleteRoleResponse, error) {
					return nil, &errors.MessageError{Message: "role not found"}
				}
			},
			wantErr: true,
			errMsg:  "role not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeRoleDelete(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestExecuteRoleList(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful role list",
			args: map[string]interface{}{},
			setupMock: func(m *mockService) {
				m.listRolesFunc = func(ctx context.Context, req *service.AuthListRolesRequest) (*service.AuthListRolesResponse, error) {
					return &service.AuthListRolesResponse{
						Roles: []*service.RoleInfo{{RoleID: "1", RoleName: "admin"}, {RoleID: "2", RoleName: "user"}},
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "role list error",
			args: map[string]interface{}{},
			setupMock: func(m *mockService) {
				m.listRolesFunc = func(ctx context.Context, req *service.AuthListRolesRequest) (*service.AuthListRolesResponse, error) {
					return nil, &errors.MessageError{Message: "auth service down"}
				}
			},
			wantErr: true,
			errMsg:  "auth service down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeRoleList(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestExecuteRoleAssign(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful role assign",
			args: map[string]interface{}{
				"username": "user1",
				"role":     "admin",
			},
			setupMock: func(m *mockService) {
				m.assignRoleFunc = func(ctx context.Context, req *service.AssignRoleRequest) (*service.AssignRoleResponse, error) {
					return &service.AssignRoleResponse{
						Success: true,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "role assign error",
			args: map[string]interface{}{
				"username": "user1",
				"role":     "admin",
			},
			setupMock: func(m *mockService) {
				m.assignRoleFunc = func(ctx context.Context, req *service.AssignRoleRequest) (*service.AssignRoleResponse, error) {
					return nil, &errors.MessageError{Message: "user not found"}
				}
			},
			wantErr: true,
			errMsg:  "user not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeRoleAssign(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestExecuteRoleRevoke(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful role revoke",
			args: map[string]interface{}{
				"username": "user1",
				"role":     "admin",
			},
			setupMock: func(m *mockService) {
				m.revokeRoleFunc = func(ctx context.Context, req *service.RevokeRoleRequest) (*service.RevokeRoleResponse, error) {
					return &service.RevokeRoleResponse{
						Success: true,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "role revoke error",
			args: map[string]interface{}{
				"username": "user1",
				"role":     "admin",
			},
			setupMock: func(m *mockService) {
				m.revokeRoleFunc = func(ctx context.Context, req *service.RevokeRoleRequest) (*service.RevokeRoleResponse, error) {
					return nil, &errors.MessageError{Message: "user not found"}
				}
			},
			wantErr: true,
			errMsg:  "user not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeRoleRevoke(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

// TestExecuteCacheOperations tests cache-related tool handlers.
func TestExecuteCacheStats(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful cache stats",
			args: map[string]interface{}{},
			setupMock: func(m *mockService) {
				m.getCacheStatsFunc = func(ctx context.Context, req *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error) {
					return &service.GetCacheStatsResponse{
						Hits:   100,
						Misses: 10,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "cache stats error",
			args: map[string]interface{}{},
			setupMock: func(m *mockService) {
				m.getCacheStatsFunc = func(ctx context.Context, req *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error) {
					return nil, &errors.MessageError{Message: "cache unavailable"}
				}
			},
			wantErr: true,
			errMsg:  "cache unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeCacheStats(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestExecuteCacheClear(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful cache clear",
			args: map[string]interface{}{},
			setupMock: func(m *mockService) {
				m.clearCacheFunc = func(ctx context.Context, req *service.ClearCacheRequest) (*service.ClearCacheResponse, error) {
					return &service.ClearCacheResponse{
						EntriesCleared: 100,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "cache clear error",
			args: map[string]interface{}{},
			setupMock: func(m *mockService) {
				m.clearCacheFunc = func(ctx context.Context, req *service.ClearCacheRequest) (*service.ClearCacheResponse, error) {
					return nil, &errors.MessageError{Message: "cache unavailable"}
				}
			},
			wantErr: true,
			errMsg:  "cache unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeCacheClear(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

// TestExecuteHealthOperations tests health-related tool handlers.
func TestExecuteHealth(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful health check",
			args: map[string]interface{}{},
			setupMock: func(m *mockService) {
				m.healthFunc = func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
					return &service.HealthResponse{
						Status: service.HealthStatusHealthy,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "health check error",
			args: map[string]interface{}{},
			setupMock: func(m *mockService) {
				m.healthFunc = func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
					return nil, &errors.MessageError{Message: "service down"}
				}
			},
			wantErr: true,
			errMsg:  "service down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeHealth(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestExecuteLiveness(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful liveness check",
			args: map[string]interface{}{},
			setupMock: func(m *mockService) {
				m.livenessFunc = func(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error) {
					return &service.LivenessResponse{
						Alive: true,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "liveness check error",
			args: map[string]interface{}{},
			setupMock: func(m *mockService) {
				m.livenessFunc = func(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error) {
					return nil, &errors.MessageError{Message: "service down"}
				}
			},
			wantErr: true,
			errMsg:  "service down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeLiveness(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestExecuteReadiness(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful readiness check",
			args: map[string]interface{}{},
			setupMock: func(m *mockService) {
				m.readinessFunc = func(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error) {
					return &service.ReadinessResponse{
						Ready: true,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "readiness check error",
			args: map[string]interface{}{},
			setupMock: func(m *mockService) {
				m.readinessFunc = func(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error) {
					return nil, &errors.MessageError{Message: "not ready"}
				}
			},
			wantErr: true,
			errMsg:  "not ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeReadiness(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

// TestExecuteMigrationProgress tests migration progress tool handler.
func TestExecuteMigrationProgress(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful migration progress",
			args: map[string]interface{}{
				"migrationID": "migration123",
			},
			setupMock: func(m *mockService) {
				m.getMigrationProgressFunc = func(ctx context.Context, req *service.GetMigrationProgressRequest) (*service.GetMigrationProgressResponse, error) {
					return &service.GetMigrationProgressResponse{
						MigrationID:   "migration123",
						Progress:      0.5,
						Status:        "in_progress",
						ItemsMigrated: 500,
						TotalItems:    1000,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "migration progress error",
			args: map[string]interface{}{
				"migrationID": "migration123",
			},
			setupMock: func(m *mockService) {
				m.getMigrationProgressFunc = func(ctx context.Context, req *service.GetMigrationProgressRequest) (*service.GetMigrationProgressResponse, error) {
					return nil, &errors.MessageError{Message: "migration not found"}
				}
			},
			wantErr: true,
			errMsg:  "migration not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewToolRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.executeMigrationProgress(ctx, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

// TestMarshalResult tests the marshalResult helper function.
func TestMarshalResult(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		wantErr bool
	}{
		{
			name:    "marshal simple value",
			input:   "test",
			wantErr: false,
		},
		{
			name:    "marshal map",
			input:   map[string]interface{}{"key": "value"},
			wantErr: false,
		},
		{
			name:    "marshal struct",
			input:   struct{ Name string }{Name: "test"},
			wantErr: false,
		},
		{
			name:    "marshal unmarshalable type",
			input:   make(chan int),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := marshalResult(tt.input)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}
