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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestServer_GetCacheStats(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.GetCacheStatsRequest
		mockSetup   func(m *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.GetCacheStatsResponse)
	}{
		{
			name:    "successful_get_stats",
			request: &pb.GetCacheStatsRequest{},
			mockSetup: func(m *mock.Mock) {
				m.On("GetCacheStats", mock.Anything, mock.Anything).
					Return(&service.GetCacheStatsResponse{
						Size:      100,
						MaxSize:   1000,
						Hits:      500,
						Misses:    50,
						HitRate:   0.91,
						Evictions: 10,
					}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.GetCacheStatsResponse) {
				assert.True(t, resp.Enabled)
				assert.Equal(t, int32(100), resp.L1Size)
				assert.Equal(t, int32(1000), resp.L1Capacity)
				assert.Equal(t, int64(500), resp.L1Hits)
				assert.Equal(t, int64(50), resp.L1Misses)
				assert.InDelta(t, 0.91, resp.L1HitRate, 0.001)
				assert.Equal(t, int64(10), resp.L1Evictions)
			},
		},
		{
			name:        "nil_request",
			request:     nil,
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name:    "service_error",
			request: &pb.GetCacheStatsRequest{},
			mockSetup: func(m *mock.Mock) {
				m.On("GetCacheStats", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.GetCacheStats(context.Background(), tt.request)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorCode != 0 {
					assert.Equal(t, tt.errorCode, status.Code(err))
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
			mockSvc.AssertExpectations(t)
		})
	}
}

func TestServer_ClearCache(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.ClearCacheRequest
		mockSetup   func(m *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.ClearCacheResponse)
	}{
		{
			name:    "successful_clear",
			request: &pb.ClearCacheRequest{},
			mockSetup: func(m *mock.Mock) {
				m.On("ClearCache", mock.Anything, mock.MatchedBy(func(req *service.ClearCacheRequest) bool {
					return req.All == true
				})).Return(&service.ClearCacheResponse{
					EntriesCleared: 150,
				}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ClearCacheResponse) {
				assert.True(t, resp.Success)
				assert.Equal(t, int32(150), resp.EntriesCleared)
			},
		},
		{
			name:        "nil_request",
			request:     nil,
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name:    "service_error",
			request: &pb.ClearCacheRequest{},
			mockSetup: func(m *mock.Mock) {
				m.On("ClearCache", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.ClearCache(context.Background(), tt.request)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorCode != 0 {
					assert.Equal(t, tt.errorCode, status.Code(err))
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
			mockSvc.AssertExpectations(t)
		})
	}
}

func TestServer_GetCacheConfig(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.GetCacheConfigRequest
		mockSetup   func(m *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.GetCacheConfigResponse)
	}{
		{
			name:    "successful_get_config",
			request: &pb.GetCacheConfigRequest{},
			mockSetup: func(m *mock.Mock) {
				m.On("GetCacheConfig", mock.Anything, mock.Anything).
					Return(&service.GetCacheConfigResponse{
						Enabled:         true,
						L1Size:          1000,
						L1TTL:           5 * time.Minute,
						CleanupInterval: 1 * time.Minute,
					}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.GetCacheConfigResponse) {
				assert.True(t, resp.Enabled)
				assert.Equal(t, int32(1000), resp.L1Size)
				assert.Equal(t, int64(300), resp.L1TtlSeconds)
				assert.Equal(t, int64(60), resp.CleanupIntervalSeconds)
			},
		},
		{
			name:        "nil_request",
			request:     nil,
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name:    "service_error",
			request: &pb.GetCacheConfigRequest{},
			mockSetup: func(m *mock.Mock) {
				m.On("GetCacheConfig", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.GetCacheConfig(context.Background(), tt.request)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorCode != 0 {
					assert.Equal(t, tt.errorCode, status.Code(err))
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
			mockSvc.AssertExpectations(t)
		})
	}
}

func TestServer_UpdateCacheConfig(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.UpdateCacheConfigRequest
		mockSetup   func(m *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.UpdateCacheConfigResponse)
	}{
		{
			name: "successful_update_with_values",
			request: &pb.UpdateCacheConfigRequest{
				L1Size:       2000,
				L1TtlSeconds: 600,
			},
			mockSetup: func(m *mock.Mock) {
				m.On("UpdateCacheConfig", mock.Anything, mock.MatchedBy(func(req *service.UpdateCacheConfigRequest) bool {
					return req.L1Size != nil && *req.L1Size == 2000 &&
						req.L1TTL != nil && *req.L1TTL == 600*time.Second
				})).Return(&service.UpdateCacheConfigResponse{
					Updated: true,
				}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.UpdateCacheConfigResponse) {
				assert.True(t, resp.Success)
				assert.Equal(t, "Cache configuration updated successfully", resp.Message)
			},
		},
		{
			name:    "update_with_zero_values_unchanged",
			request: &pb.UpdateCacheConfigRequest{},
			mockSetup: func(m *mock.Mock) {
				m.On("UpdateCacheConfig", mock.Anything, mock.MatchedBy(func(req *service.UpdateCacheConfigRequest) bool {
					return req.L1Size == nil && req.L1TTL == nil
				})).Return(&service.UpdateCacheConfigResponse{
					Updated: false,
				}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.UpdateCacheConfigResponse) {
				assert.False(t, resp.Success)
				assert.Equal(t, "Cache configuration unchanged", resp.Message)
			},
		},
		{
			name:        "nil_request",
			request:     nil,
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_error",
			request: &pb.UpdateCacheConfigRequest{
				L1Size: 500,
			},
			mockSetup: func(m *mock.Mock) {
				m.On("UpdateCacheConfig", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.UpdateCacheConfig(context.Background(), tt.request)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorCode != 0 {
					assert.Equal(t, tt.errorCode, status.Code(err))
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
			mockSvc.AssertExpectations(t)
		})
	}
}
