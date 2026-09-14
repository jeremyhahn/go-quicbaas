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

// =============================================================================
// Role Tests
//
// There are no user tests: the QuicBaaS gRPC service no longer implements
// CreateUser/DeleteUser/ListUsers (they answer codes.Unimplemented from the
// embedded pb.UnimplementedQuicBaaSServer). Users are served by
// quicbaas.iam.v1.IAMService. See issue #905.
// =============================================================================

func TestServer_CreateRole(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.CreateRoleRequest
		mockSetup   func(m *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.CreateRoleResponse)
	}{
		{
			name: "successful_create_role",
			request: &pb.CreateRoleRequest{
				RoleName: "editor",
				Permissions: []*pb.Permission{
					{Resource: "documents", Actions: []string{"read", "write"}},
					{Resource: "images", Actions: []string{"read"}},
				},
			},
			mockSetup: func(m *mock.Mock) {
				m.On("CreateAuthRole", mock.Anything, mock.MatchedBy(func(req *service.AuthCreateRoleRequest) bool {
					return req.RoleName == "editor" &&
						len(req.Permissions) == 2 &&
						req.Permissions[0] == "documents:read,write" &&
						req.Permissions[1] == "images:read"
				})).Return(&service.AuthCreateRoleResponse{
					RoleID:    "editor",
					Timestamp: 1700000000,
				}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.CreateRoleResponse) {
				assert.Equal(t, "editor", resp.RoleName)
			},
		},
		{
			name: "create_role_no_permissions",
			request: &pb.CreateRoleRequest{
				RoleName: "viewer",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("CreateAuthRole", mock.Anything, mock.MatchedBy(func(req *service.AuthCreateRoleRequest) bool {
					return req.RoleName == "viewer" && len(req.Permissions) == 0
				})).Return(&service.AuthCreateRoleResponse{
					RoleID:    "viewer",
					Timestamp: 1700000000,
				}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.CreateRoleResponse) {
				assert.Equal(t, "viewer", resp.RoleName)
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
			name: "empty_role_name",
			request: &pb.CreateRoleRequest{
				RoleName: "",
			},
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_error",
			request: &pb.CreateRoleRequest{
				RoleName: "existing-role",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("CreateAuthRole", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.CreateRole(context.Background(), tt.request)
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

// =============================================================================
// DeleteRole Tests
// =============================================================================

func TestServer_DeleteRole(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.DeleteRoleRequest
		mockSetup   func(m *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.DeleteRoleResponse)
	}{
		{
			name: "successful_delete_role",
			request: &pb.DeleteRoleRequest{
				RoleName: "editor",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("DeleteAuthRole", mock.Anything, mock.MatchedBy(func(req *service.AuthDeleteRoleRequest) bool {
					return req.RoleName == "editor"
				})).Return(&service.AuthDeleteRoleResponse{
					Success: true,
					Message: "role deleted",
				}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.DeleteRoleResponse) {
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
			name: "empty_role_name",
			request: &pb.DeleteRoleRequest{
				RoleName: "",
			},
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_error",
			request: &pb.DeleteRoleRequest{
				RoleName: "nonexistent",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("DeleteAuthRole", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.DeleteRole(context.Background(), tt.request)
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

// =============================================================================
// ListRoles Tests
// =============================================================================

func TestServer_ListRoles(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.ListRolesRequest
		mockSetup   func(m *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.ListRolesResponse)
	}{
		{
			name:    "successful_list_roles",
			request: &pb.ListRolesRequest{},
			mockSetup: func(m *mock.Mock) {
				m.On("ListAuthRoles", mock.Anything, mock.Anything).Return(&service.AuthListRolesResponse{
					Roles: []*service.RoleInfo{
						{
							RoleID:      "role-1",
							RoleName:    "admin",
							Permissions: []string{"backup:read,write", "auth:read,write"},
							CreatedAt:   1700000000,
						},
						{
							RoleID:      "role-2",
							RoleName:    "reader",
							Permissions: []string{"documents:read"},
							CreatedAt:   1700000100,
						},
					},
					TotalCount: 2,
				}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ListRolesResponse) {
				require.Len(t, resp.Roles, 2)
				assert.Equal(t, "admin", resp.Roles[0].RoleName)
				require.Len(t, resp.Roles[0].Permissions, 2)
				assert.Equal(t, "backup", resp.Roles[0].Permissions[0].Resource)
				assert.Equal(t, []string{"read", "write"}, resp.Roles[0].Permissions[0].Actions)
				assert.Equal(t, "reader", resp.Roles[1].RoleName)
				require.Len(t, resp.Roles[1].Permissions, 1)
				assert.Equal(t, "documents", resp.Roles[1].Permissions[0].Resource)
				assert.Equal(t, []string{"read"}, resp.Roles[1].Permissions[0].Actions)
			},
		},
		{
			name:    "empty_role_list",
			request: &pb.ListRolesRequest{},
			mockSetup: func(m *mock.Mock) {
				m.On("ListAuthRoles", mock.Anything, mock.Anything).Return(&service.AuthListRolesResponse{
					Roles:      nil,
					TotalCount: 0,
				}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ListRolesResponse) {
				assert.Empty(t, resp.Roles)
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
			request: &pb.ListRolesRequest{},
			mockSetup: func(m *mock.Mock) {
				m.On("ListAuthRoles", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.ListRoles(context.Background(), tt.request)
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

// =============================================================================
// AssignRole Tests
// =============================================================================

func TestServer_AssignRole(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.AssignRoleRequest
		mockSetup   func(m *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.AssignRoleResponse)
	}{
		{
			name: "successful_assign_role",
			request: &pb.AssignRoleRequest{
				UserId:   "user-abc-123",
				RoleName: "editor",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("AssignRole", mock.Anything, mock.MatchedBy(func(req *service.AuthAssignRoleRequest) bool {
					return req.Username == "user-abc-123" && req.RoleName == "editor"
				})).Return(&service.AuthAssignRoleResponse{
					Success: true,
					Message: "role assigned",
				}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.AssignRoleResponse) {
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
			name: "empty_user_id",
			request: &pb.AssignRoleRequest{
				UserId:   "",
				RoleName: "editor",
			},
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "empty_role_name",
			request: &pb.AssignRoleRequest{
				UserId:   "user-abc-123",
				RoleName: "",
			},
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_error",
			request: &pb.AssignRoleRequest{
				UserId:   "user-abc-123",
				RoleName: "nonexistent",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("AssignRole", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.AssignRole(context.Background(), tt.request)
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

// =============================================================================
// RevokeRole Tests
// =============================================================================

func TestServer_RevokeRole(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.RevokeRoleRequest
		mockSetup   func(m *mock.Mock)
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.RevokeRoleResponse)
	}{
		{
			name: "successful_revoke_role",
			request: &pb.RevokeRoleRequest{
				UserId:   "user-abc-123",
				RoleName: "editor",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("RevokeRole", mock.Anything, mock.MatchedBy(func(req *service.AuthRevokeRoleRequest) bool {
					return req.Username == "user-abc-123" && req.RoleName == "editor"
				})).Return(&service.AuthRevokeRoleResponse{
					Success: true,
					Message: "role revoked",
				}, nil).Once()
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.RevokeRoleResponse) {
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
			name: "empty_user_id",
			request: &pb.RevokeRoleRequest{
				UserId:   "",
				RoleName: "editor",
			},
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "empty_role_name",
			request: &pb.RevokeRoleRequest{
				UserId:   "user-abc-123",
				RoleName: "",
			},
			mockSetup:   func(m *mock.Mock) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_error",
			request: &pb.RevokeRoleRequest{
				UserId:   "user-abc-123",
				RoleName: "admin",
			},
			mockSetup: func(m *mock.Mock) {
				m.On("RevokeRole", mock.Anything, mock.Anything).
					Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			tt.mockSetup(&mockSvc.Mock)

			resp, err := srv.RevokeRole(context.Background(), tt.request)
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

// =============================================================================
// Permission Conversion Tests
// =============================================================================

func TestProtoPermissionsToStrings(t *testing.T) {
	tests := []struct {
		name     string
		input    []*pb.Permission
		expected []string
	}{
		{
			name:     "nil_permissions",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty_permissions",
			input:    []*pb.Permission{},
			expected: nil,
		},
		{
			name: "single_permission_with_actions",
			input: []*pb.Permission{
				{Resource: "documents", Actions: []string{"read", "write"}},
			},
			expected: []string{"documents:read,write"},
		},
		{
			name: "multiple_permissions",
			input: []*pb.Permission{
				{Resource: "documents", Actions: []string{"read", "write"}},
				{Resource: "images", Actions: []string{"read"}},
			},
			expected: []string{"documents:read,write", "images:read"},
		},
		{
			name: "resource_only_no_actions",
			input: []*pb.Permission{
				{Resource: "admin", Actions: nil},
			},
			expected: []string{"admin"},
		},
		{
			name: "skip_empty_resource",
			input: []*pb.Permission{
				{Resource: "", Actions: []string{"read"}},
				{Resource: "documents", Actions: []string{"write"}},
			},
			expected: []string{"documents:write"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoPermissionsToStrings(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStringsToProtoPermissions(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []*pb.Permission
	}{
		{
			name:     "nil_strings",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty_strings",
			input:    []string{},
			expected: nil,
		},
		{
			name:  "single_permission_with_actions",
			input: []string{"documents:read,write"},
			expected: []*pb.Permission{
				{Resource: "documents", Actions: []string{"read", "write"}},
			},
		},
		{
			name:  "multiple_permissions",
			input: []string{"documents:read,write", "images:read"},
			expected: []*pb.Permission{
				{Resource: "documents", Actions: []string{"read", "write"}},
				{Resource: "images", Actions: []string{"read"}},
			},
		},
		{
			name:  "resource_only_no_colon",
			input: []string{"admin"},
			expected: []*pb.Permission{
				{Resource: "admin"},
			},
		},
		{
			name:  "resource_with_empty_actions",
			input: []string{"admin:"},
			expected: []*pb.Permission{
				{Resource: "admin"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringsToProtoPermissions(tt.input)
			require.Equal(t, len(tt.expected), len(result))
			for i, expected := range tt.expected {
				assert.Equal(t, expected.Resource, result[i].Resource)
				assert.Equal(t, expected.Actions, result[i].Actions)
			}
		})
	}
}

// TestPermissionRoundTrip verifies that converting permissions from proto to string
// and back yields equivalent results.
func TestPermissionRoundTrip(t *testing.T) {
	original := []*pb.Permission{
		{Resource: "backup", Actions: []string{"read", "write"}},
		{Resource: "auth", Actions: []string{"read"}},
		{Resource: "admin", Actions: []string{"manage", "configure"}},
	}

	strings := protoPermissionsToStrings(original)
	roundTripped := stringsToProtoPermissions(strings)

	require.Len(t, roundTripped, len(original))
	for i, orig := range original {
		assert.Equal(t, orig.Resource, roundTripped[i].Resource)
		assert.Equal(t, orig.Actions, roundTripped[i].Actions)
	}
}

// =============================================================================
// User RPCs answer Unimplemented, and nothing reaches the service layer
// =============================================================================

// TestServer_UserRPCsAreUnimplemented pins the wire answer for the three user
// RPCs the QuicBaaS gRPC service still DECLARES in api/proto/quicbaas.proto but
// no longer implements. They used to route to a service-layer auth manager that
// production never wired, so a caller got an opaque "authentication manager not
// configured" failure; now they get codes.Unimplemented, which is the truthful
// answer for a capability served elsewhere (quicbaas.iam.v1.IAMService).
//
// The test also asserts the service layer is NEVER touched: MockQuicBaaS is left
// with no expectations, so any call into it fails the mock. That is the guard
// against someone re-adding a handler on top of the deleted user model rather
// than on the IAM surface (issue #905).
func TestServer_UserRPCsAreUnimplemented(t *testing.T) {
	tests := []struct {
		name string
		call func(*Server) error
	}{
		{
			name: "CreateUser",
			call: func(s *Server) error {
				_, err := s.CreateUser(context.Background(), &pb.CreateUserRequest{Username: "alice"})
				return err
			},
		},
		{
			name: "DeleteUser",
			call: func(s *Server) error {
				_, err := s.DeleteUser(context.Background(), &pb.DeleteUserRequest{UserId: "user-1"})
				return err
			},
		},
		{
			name: "ListUsers",
			call: func(s *Server) error {
				_, err := s.ListUsers(context.Background(), &pb.ListUsersRequest{})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)

			err := tt.call(srv)
			require.Error(t, err)
			assert.Equal(t, codes.Unimplemented, status.Code(err),
				"%s must answer Unimplemented, not a configuration failure", tt.name)

			// No service-layer call was made: an unexpected call on a mock with
			// no expectations is a failure, and AssertExpectations confirms none
			// were set up and consumed.
			mockSvc.AssertExpectations(t)
		})
	}
}

// TestServer_UserRPCsAreUnimplementedOnNilRequest covers the bad-input side of
// the same three RPCs. A nil request must not change the answer: the capability
// is absent, so argument validation never gets a chance to run and the caller
// still learns Unimplemented rather than InvalidArgument.
func TestServer_UserRPCsAreUnimplementedOnNilRequest(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	_, err := srv.CreateUser(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))

	_, err = srv.DeleteUser(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))

	_, err = srv.ListUsers(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))

	mockSvc.AssertExpectations(t)
}
