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

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rbac"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// rbacHandler handles RBAC-specific gRPC requests.
type rbacHandler struct {
	conn *grpc.ClientConn
}

// newRBACHandler creates a new RBAC handler.
func newRBACHandler(conn *grpc.ClientConn) *rbacHandler {
	return &rbacHandler{conn: conn}
}

// isRBACService returns true if the service name is an RBAC service.
func isRBACService(service string) bool {
	return service == "quicbaas.RBAC"
}

// handleRequest routes RBAC requests to the appropriate handler.
func (h *rbacHandler) handleRequest(ctx context.Context, _, method string, req interface{}) (interface{}, error) {
	client := pb.NewQuicBaaSClient(h.conn)

	switch method {
	case "CreateRole":
		return h.handleCreateRole(ctx, client, req)
	case "DeleteRole":
		return h.handleDeleteRole(ctx, client, req)
	case "ListRoles":
		return h.handleListRoles(ctx, client, req)
	case "AssignRole":
		return h.handleAssignRole(ctx, client, req)
	case "RevokeRole":
		return h.handleRevokeRole(ctx, client, req)
	default:
		return nil, errors.NewNotFound("rbac.handleRequest", "unknown method: "+method)
	}
}

// handleCreateRole converts SDK CreateRoleRequest to protobuf and calls gRPC.
func (h *rbacHandler) handleCreateRole(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("rbac.handleCreateRole", "invalid request type")
	}

	roleName, _ := getString(reqMap, "RoleName")
	permissions, _ := getStringSlice(reqMap, "Permissions")

	pbPermissions := make([]*pb.Permission, 0, len(permissions))
	for _, p := range permissions {
		pbPermissions = append(pbPermissions, &pb.Permission{
			Resource: p,
			Actions:  []string{"*"},
		})
	}

	pbReq := &pb.CreateRoleRequest{
		RoleName:    roleName,
		Permissions: pbPermissions,
	}

	resp, err := client.CreateRole(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "rbac.CreateRole")
	}

	return &rbac.CreateRoleResponse{
		RoleID:    resp.GetRoleName(),
		Timestamp: 0,
	}, nil
}

// handleDeleteRole converts SDK DeleteRoleRequest to protobuf and calls gRPC.
func (h *rbacHandler) handleDeleteRole(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("rbac.handleDeleteRole", "invalid request type")
	}

	roleName, _ := getString(reqMap, "RoleName")

	pbReq := &pb.DeleteRoleRequest{
		RoleName: roleName,
	}

	resp, err := client.DeleteRole(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "rbac.DeleteRole")
	}

	return &rbac.DeleteRoleResponse{
		Success: resp.GetSuccess(),
		Message: "Role deleted successfully",
	}, nil
}

// handleListRoles converts SDK ListRolesRequest to protobuf and calls gRPC.
func (h *rbacHandler) handleListRoles(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	pbReq := &pb.ListRolesRequest{}

	resp, err := client.ListRoles(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "rbac.ListRoles")
	}

	protoRoles := resp.GetRoles()
	roles := make([]*rbac.RoleInfo, 0, len(protoRoles))
	for _, r := range protoRoles {
		roles = append(roles, &rbac.RoleInfo{
			RoleName:    r.GetRoleName(),
			Permissions: convertPermissionsFromProto(r.GetPermissions()),
		})
	}

	return &rbac.ListRolesResponse{
		Roles:      roles,
		TotalCount: uint64(len(roles)),
	}, nil
}

// convertPermissionsFromProto converts protobuf Permission messages to SDK
// permission strings by extracting the Resource field from each Permission.
// This is the inverse of the conversion in handleCreateRole, which maps each
// permission string to a pb.Permission with Resource set to the string value.
func convertPermissionsFromProto(perms []*pb.Permission) []string {
	result := make([]string, 0, len(perms))
	for _, p := range perms {
		result = append(result, p.GetResource())
	}
	return result
}

// handleAssignRole converts SDK AssignRoleRequest to protobuf and calls gRPC.
func (h *rbacHandler) handleAssignRole(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("rbac.handleAssignRole", "invalid request type")
	}

	username, _ := getString(reqMap, "Username")
	roleName, _ := getString(reqMap, "RoleName")

	pbReq := &pb.AssignRoleRequest{
		UserId:   username,
		RoleName: roleName,
	}

	resp, err := client.AssignRole(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "rbac.AssignRole")
	}

	return &rbac.AssignRoleResponse{
		Success: resp.GetSuccess(),
		Message: "Role assigned successfully",
	}, nil
}

// handleRevokeRole converts SDK RevokeRoleRequest to protobuf and calls gRPC.
func (h *rbacHandler) handleRevokeRole(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("rbac.handleRevokeRole", "invalid request type")
	}

	username, _ := getString(reqMap, "Username")
	roleName, _ := getString(reqMap, "RoleName")

	pbReq := &pb.RevokeRoleRequest{
		UserId:   username,
		RoleName: roleName,
	}

	resp, err := client.RevokeRole(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "rbac.RevokeRole")
	}

	return &rbac.RevokeRoleResponse{
		Success: resp.GetSuccess(),
		Message: "Role revoked successfully",
	}, nil
}
