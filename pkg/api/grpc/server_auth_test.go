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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestServer_Login(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.LoginRequest
		mockSetup   func(mockSvc *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.LoginResponse)
	}{
		{
			name: "successful_login",
			request: &pb.LoginRequest{
				Username: "admin",
				Password: "secret123",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("Login", mock.Anything, mock.MatchedBy(func(req *service.LoginRequest) bool {
					return req.Username == "admin" && req.Password == "secret123"
				})).Return(&service.LoginResponse{
					AccessToken:  "access-token-123",
					RefreshToken: "refresh-token-456",
					ExpiresAt:    1700000000,
					UserID:       "user-1",
				}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.LoginResponse) {
				assert.Equal(t, "access-token-123", resp.AccessToken)
				assert.Equal(t, "refresh-token-456", resp.RefreshToken)
				assert.Equal(t, int64(1700000000), resp.ExpiresAt)
				assert.Equal(t, "Bearer", resp.TokenType)
				assert.Equal(t, "user-1", resp.UserId)
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
			request: &pb.LoginRequest{
				Username: "admin",
				Password: "wrong",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("Login", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.Login(context.Background(), tt.request)
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

func TestServer_Logout(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.LogoutRequest
		mockSetup   func(mockSvc *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.LogoutResponse)
	}{
		{
			name: "successful_logout",
			request: &pb.LogoutRequest{
				AccessToken: "access-token-123",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("Logout", mock.Anything, mock.MatchedBy(func(req *service.LogoutRequest) bool {
					return req.AccessToken == "access-token-123"
				})).Return(&service.LogoutResponse{
					Success: true,
				}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.LogoutResponse) {
				assert.True(t, resp.Success)
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
			request: &pb.LogoutRequest{
				AccessToken: "invalid-token",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("Logout", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.Logout(context.Background(), tt.request)
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

func TestServer_RefreshToken(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.RefreshTokenRequest
		mockSetup   func(mockSvc *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.RefreshTokenResponse)
	}{
		{
			name: "successful_refresh",
			request: &pb.RefreshTokenRequest{
				RefreshToken: "refresh-token-456",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("RefreshToken", mock.Anything, mock.MatchedBy(func(req *service.RefreshTokenRequest) bool {
					return req.RefreshToken == "refresh-token-456"
				})).Return(&service.RefreshTokenResponse{
					AccessToken: "new-access-token-789",
					ExpiresAt:   1700003600,
				}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.RefreshTokenResponse) {
				assert.Equal(t, "new-access-token-789", resp.AccessToken)
				assert.Equal(t, int64(1700003600), resp.ExpiresAt)
				assert.Equal(t, "Bearer", resp.TokenType)
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
			request: &pb.RefreshTokenRequest{
				RefreshToken: "expired-token",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("RefreshToken", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.RefreshToken(context.Background(), tt.request)
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

func TestServer_ValidateToken(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.ValidateTokenRequest
		mockSetup   func(mockSvc *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.ValidateTokenResponse)
	}{
		{
			name: "valid_token",
			request: &pb.ValidateTokenRequest{
				AccessToken: "valid-token",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("ValidateToken", mock.Anything, mock.MatchedBy(func(req *service.ValidateTokenRequest) bool {
					return req.AccessToken == "valid-token"
				})).Return(&service.ValidateTokenResponse{
					Valid:     true,
					UserID:    "user-1",
					Username:  "admin",
					Roles:     []string{"admin", "user"},
					ExpiresAt: 1700003600,
				}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ValidateTokenResponse) {
				assert.True(t, resp.Valid)
				assert.Equal(t, "user-1", resp.UserId)
				assert.Equal(t, "admin", resp.Username)
				assert.Equal(t, []string{"admin", "user"}, resp.Roles)
				assert.Equal(t, int64(1700003600), resp.ExpiresAt)
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
			request: &pb.ValidateTokenRequest{
				AccessToken: "bad-token",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("ValidateToken", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.ValidateToken(context.Background(), tt.request)
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
