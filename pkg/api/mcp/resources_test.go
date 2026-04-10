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

// TestResourceReadCluster tests the cluster resource reading paths.
func TestResourceReadCluster(t *testing.T) {
	tests := []struct {
		name      string
		parts     []string
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name:    "no parts provided",
			parts:   []string{},
			wantErr: true,
			errMsg:  "cluster resource type is required",
		},
		{
			name:  "read cluster status",
			parts: []string{"status"},
			setupMock: func(m *mockService) {
				m.clusterStatusFunc = func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
					return &service.ClusterStatusResponse{
						ClusterID: "test-cluster",
						Nodes: []*service.NodeInfo{
							{NodeID: 1, Address: "localhost:5000"},
						},
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name:  "read cluster nodes",
			parts: []string{"nodes"},
			setupMock: func(m *mockService) {
				m.clusterStatusFunc = func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
					return &service.ClusterStatusResponse{
						Nodes: []*service.NodeInfo{
							{NodeID: 1, Address: "localhost:5000"},
						},
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name:  "read cluster shards",
			parts: []string{"shards"},
			setupMock: func(m *mockService) {
				m.clusterStatusFunc = func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
					return &service.ClusterStatusResponse{
						Shards: []*service.ShardInfo{
							{ShardID: 1, LeaderID: 1},
						},
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name:    "unknown cluster resource",
			parts:   []string{"invalid"},
			wantErr: true,
			errMsg:  "cluster resource not found: invalid",
		},
		{
			name:  "cluster status error",
			parts: []string{"status"},
			setupMock: func(m *mockService) {
				m.clusterStatusFunc = func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
					return nil, &errors.MessageError{Message: "cluster error"}
				}
			},
			wantErr: true,
			errMsg:  "cluster error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewResourceRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.readClusterResource(ctx, tt.parts)

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

// TestResourceReadMetrics tests the metrics resource reading paths.
func TestResourceReadMetrics(t *testing.T) {
	tests := []struct {
		name      string
		parts     []string
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name:    "no parts provided",
			parts:   []string{},
			wantErr: true,
			errMsg:  "metrics resource type is required",
		},
		{
			name:  "read metrics summary",
			parts: []string{"summary"},
			setupMock: func(m *mockService) {
				m.getCacheStatsFunc = func(ctx context.Context, req *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error) {
					return &service.GetCacheStatsResponse{
						Hits:      100,
						Misses:    10,
						Evictions: 5,
					}, nil
				}
				m.healthFunc = func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
					return &service.HealthResponse{
						Status:        service.HealthStatusHealthy,
						UptimeSeconds: 3600,
						Details:       make(map[string]*service.ComponentHealth),
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name:    "unknown metrics resource",
			parts:   []string{"invalid"},
			wantErr: true,
			errMsg:  "metrics resource not found: invalid",
		},
		{
			name:  "cache stats error",
			parts: []string{"summary"},
			setupMock: func(m *mockService) {
				m.getCacheStatsFunc = func(ctx context.Context, req *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error) {
					return nil, &errors.MessageError{Message: "cache error"}
				}
			},
			wantErr: true,
			errMsg:  "cache error",
		},
		{
			name:  "health check error",
			parts: []string{"summary"},
			setupMock: func(m *mockService) {
				m.getCacheStatsFunc = func(ctx context.Context, req *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error) {
					return &service.GetCacheStatsResponse{}, nil
				}
				m.healthFunc = func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
					return nil, &errors.MessageError{Message: "health error"}
				}
			},
			wantErr: true,
			errMsg:  "health error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewResourceRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.readMetricsResource(ctx, tt.parts)

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

// TestResourceReadCache tests the cache resource reading.
func TestResourceReadCache(t *testing.T) {
	tests := []struct {
		name      string
		parts     []string
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name:  "read cache stats",
			parts: []string{"stats"},
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
			name:    "no cache type",
			parts:   []string{},
			wantErr: true,
			errMsg:  "cache resource type is required",
		},
		{
			name:  "cache error",
			parts: []string{"stats"},
			setupMock: func(m *mockService) {
				m.getCacheStatsFunc = func(ctx context.Context, req *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error) {
					return nil, &errors.MessageError{Message: "cache unavailable"}
				}
			},
			wantErr: true,
			errMsg:  "cache unavailable",
		},
		{
			name:    "unknown cache resource",
			parts:   []string{"invalid"},
			wantErr: true,
			errMsg:  "cache resource not found: invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewResourceRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.readCacheResource(ctx, tt.parts)

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

// TestResourceReadHealth tests the health resource reading.
func TestResourceReadHealth(t *testing.T) {
	tests := []struct {
		name      string
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "read health successfully",
			setupMock: func(m *mockService) {
				m.healthFunc = func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
					return &service.HealthResponse{
						Status:        service.HealthStatusHealthy,
						UptimeSeconds: 3600,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "health check fails",
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
			registry := NewResourceRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.readHealthResource(ctx)

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

// TestResourceReadBackups tests backup resource reading.
func TestResourceReadBackups(t *testing.T) {
	tests := []struct {
		name      string
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "list backups successfully",
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
			name: "list backups fails",
			setupMock: func(m *mockService) {
				m.listBackupsFunc = func(ctx context.Context, req *service.ListBackupsRequest) (*service.ListBackupsResponse, error) {
					return nil, &errors.MessageError{Message: "backup service error"}
				}
			},
			wantErr: true,
			errMsg:  "backup service error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewResourceRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.readBackupsResource(ctx)

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

// TestResourceReadUsers tests user resource reading.
func TestResourceReadUsers(t *testing.T) {
	tests := []struct {
		name      string
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "list users successfully",
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
			name: "list users fails",
			setupMock: func(m *mockService) {
				m.listUsersFunc = func(ctx context.Context, req *service.ListUsersRequest) (*service.ListUsersResponse, error) {
					return nil, &errors.MessageError{Message: "auth service error"}
				}
			},
			wantErr: true,
			errMsg:  "auth service error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewResourceRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.readUsersResource(ctx)

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

// TestResourceReadRoles tests role resource reading.
func TestResourceReadRoles(t *testing.T) {
	tests := []struct {
		name      string
		setupMock func(*mockService)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "list roles successfully",
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
			name: "list roles fails",
			setupMock: func(m *mockService) {
				m.listRolesFunc = func(ctx context.Context, req *service.AuthListRolesRequest) (*service.AuthListRolesResponse, error) {
					return nil, &errors.MessageError{Message: "rbac service error"}
				}
			},
			wantErr: true,
			errMsg:  "rbac service error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockService{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewResourceRegistry(mock, log)

			ctx := context.Background()
			result, err := registry.readRolesResource(ctx)

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

// TestMarshalResource tests the marshalResource function.
func TestMarshalResource(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		wantErr bool
	}{
		{
			name:    "marshal simple map",
			input:   map[string]interface{}{"key": "value"},
			wantErr: false,
		},
		{
			name:    "marshal struct",
			input:   struct{ Name string }{Name: "test"},
			wantErr: false,
		},
		{
			name:    "marshal nil",
			input:   nil,
			wantErr: false,
		},
		{
			name:    "marshal unmarshalable",
			input:   make(chan int),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			registry := NewResourceRegistry(&mockService{}, log)

			result, err := registry.marshalResource(tt.input)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}
