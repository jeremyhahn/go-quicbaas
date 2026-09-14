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
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// RBAC User Operations
//
// There are none. The QuicBaaS gRPC service still DECLARES CreateUser,
// DeleteUser and ListUsers (api/proto/quicbaas.proto), but Server embeds
// pb.UnimplementedQuicBaaSServer, so they now answer codes.Unimplemented
// instead of routing to a service-layer auth manager production never wired —
// which failed every call with "authentication manager not configured". Users
// are qrdb's: administer them over quicbaas.iam.v1.IAMService (the same surface
// REST serves at /api/v1/iam and the SDK exposes as Identity().IAM().User()).
// See issue #905.
// =============================================================================

// =============================================================================
// RBAC Role Operations
// =============================================================================

// CreateRole creates a new role with permissions.
func (s *Server) CreateRole(ctx context.Context, req *pb.CreateRoleRequest) (*pb.CreateRoleResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if req.GetRoleName() == "" {
		return nil, status.Error(codes.InvalidArgument, "role_name is required")
	}

	serviceReq := &service.AuthCreateRoleRequest{
		RoleName:    req.GetRoleName(),
		Permissions: protoPermissionsToStrings(req.GetPermissions()),
	}

	serviceResp, err := s.service.CreateAuthRole(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.CreateRoleResponse{
		RoleName: serviceResp.RoleID,
	}, nil
}

// DeleteRole removes a role from the system.
func (s *Server) DeleteRole(ctx context.Context, req *pb.DeleteRoleRequest) (*pb.DeleteRoleResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if req.GetRoleName() == "" {
		return nil, status.Error(codes.InvalidArgument, "role_name is required")
	}

	serviceReq := &service.AuthDeleteRoleRequest{
		RoleName: req.GetRoleName(),
	}

	serviceResp, err := s.service.DeleteAuthRole(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.DeleteRoleResponse{
		Success: serviceResp.Success,
	}, nil
}

// ListRoles returns all roles in the system.
func (s *Server) ListRoles(ctx context.Context, req *pb.ListRolesRequest) (*pb.ListRolesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	serviceReq := &service.AuthListRolesRequest{}

	serviceResp, err := s.service.ListAuthRoles(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	pbRoles := make([]*pb.RoleInfo, 0, len(serviceResp.Roles))
	for _, r := range serviceResp.Roles {
		pbRoles = append(pbRoles, &pb.RoleInfo{
			RoleName:    r.RoleName,
			Permissions: stringsToProtoPermissions(r.Permissions),
		})
	}

	return &pb.ListRolesResponse{
		Roles: pbRoles,
	}, nil
}

// =============================================================================
// RBAC Role Assignment Operations
// =============================================================================

// AssignRole assigns a role to a user.
func (s *Server) AssignRole(ctx context.Context, req *pb.AssignRoleRequest) (*pb.AssignRoleResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if req.GetUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.GetRoleName() == "" {
		return nil, status.Error(codes.InvalidArgument, "role_name is required")
	}

	serviceReq := &service.AuthAssignRoleRequest{
		Username: req.GetUserId(),
		RoleName: req.GetRoleName(),
	}

	serviceResp, err := s.service.AssignRole(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.AssignRoleResponse{
		Success: serviceResp.Success,
	}, nil
}

// RevokeRole removes a role from a user.
func (s *Server) RevokeRole(ctx context.Context, req *pb.RevokeRoleRequest) (*pb.RevokeRoleResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if req.GetUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.GetRoleName() == "" {
		return nil, status.Error(codes.InvalidArgument, "role_name is required")
	}

	serviceReq := &service.AuthRevokeRoleRequest{
		Username: req.GetUserId(),
		RoleName: req.GetRoleName(),
	}

	serviceResp, err := s.service.RevokeRole(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.RevokeRoleResponse{
		Success: serviceResp.Success,
	}, nil
}

// =============================================================================
// Permission Conversion Helpers
// =============================================================================

// protoPermissionsToStrings converts proto Permission objects to string representations.
// Each permission is encoded as "resource:action1,action2,...".
func protoPermissionsToStrings(perms []*pb.Permission) []string {
	if len(perms) == 0 {
		return nil
	}
	result := make([]string, 0, len(perms))
	for _, p := range perms {
		if p.GetResource() == "" {
			continue
		}
		actions := strings.Join(p.GetActions(), ",")
		if actions == "" {
			result = append(result, p.GetResource())
		} else {
			result = append(result, p.GetResource()+":"+actions)
		}
	}
	return result
}

// stringsToProtoPermissions converts string permission representations back to proto Permission objects.
// Expects strings in the format "resource:action1,action2,..." or "resource" for resource-only permissions.
func stringsToProtoPermissions(perms []string) []*pb.Permission {
	if len(perms) == 0 {
		return nil
	}
	result := make([]*pb.Permission, 0, len(perms))
	for _, p := range perms {
		parts := strings.SplitN(p, ":", 2)
		perm := &pb.Permission{
			Resource: parts[0],
		}
		if len(parts) == 2 && parts[1] != "" {
			perm.Actions = strings.Split(parts[1], ",")
		}
		result = append(result, perm)
	}
	return result
}
