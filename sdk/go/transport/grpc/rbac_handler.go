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
	case "CreateUser":
		return h.handleCreateUser(ctx, client, req)
	case "DeleteUser":
		return h.handleDeleteUser(ctx, client, req)
	case "ListUsers":
		return h.handleListUsers(ctx, client, req)
	case "GetUser":
		return h.handleGetUser(ctx, client, req)
	case "UpdateUser":
		return h.handleUpdateUser(ctx, client, req)
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

// handleCreateUser converts SDK CreateUserRequest to protobuf and calls gRPC.
func (h *rbacHandler) handleCreateUser(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("rbac.handleCreateUser", "invalid request type")
	}

	username, _ := getString(reqMap, "Username")
	password, _ := getString(reqMap, "Password")
	roles, _ := getStringSlice(reqMap, "Roles")

	pbReq := &pb.CreateUserRequest{
		Username: username,
		Password: password,
		Roles:    roles,
	}

	resp, err := client.CreateUser(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "rbac.CreateUser")
	}

	return &rbac.CreateUserResponse{
		UserID:    resp.GetUserId(),
		Timestamp: resp.GetCreated(),
	}, nil
}

// handleDeleteUser converts SDK DeleteUserRequest to protobuf and calls gRPC.
func (h *rbacHandler) handleDeleteUser(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("rbac.handleDeleteUser", "invalid request type")
	}

	username, _ := getString(reqMap, "Username")

	pbReq := &pb.DeleteUserRequest{
		UserId: username,
	}

	resp, err := client.DeleteUser(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "rbac.DeleteUser")
	}

	return &rbac.DeleteUserResponse{
		Success: resp.GetSuccess(),
		Message: "User deleted successfully",
	}, nil
}

// handleListUsers converts SDK ListUsersRequest to protobuf and calls gRPC.
func (h *rbacHandler) handleListUsers(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	pbReq := &pb.ListUsersRequest{}

	resp, err := client.ListUsers(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "rbac.ListUsers")
	}

	protoUsers := resp.GetUsers()
	users := make([]*rbac.UserInfo, 0, len(protoUsers))
	for _, u := range protoUsers {
		users = append(users, &rbac.UserInfo{
			UserID:      u.GetUserId(),
			Username:    u.GetUsername(),
			Roles:       u.GetRoles(),
			CreatedAt:   u.GetCreated(),
			LastLoginAt: u.GetLastAuth(),
		})
	}

	return &rbac.ListUsersResponse{
		Users:      users,
		TotalCount: uint64(len(users)),
	}, nil
}

// handleGetUser retrieves a single user by listing all users and filtering
// by username. The proto service does not expose a dedicated GetUser RPC.
func (h *rbacHandler) handleGetUser(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("rbac.handleGetUser", "invalid request type")
	}

	username, _ := getString(reqMap, "Username")

	resp, err := client.ListUsers(ctx, &pb.ListUsersRequest{})
	if err != nil {
		return nil, convertGRPCError(err, "rbac.GetUser")
	}

	for _, u := range resp.GetUsers() {
		if u.GetUsername() == username {
			return &rbac.UserInfo{
				UserID:      u.GetUserId(),
				Username:    u.GetUsername(),
				Roles:       u.GetRoles(),
				CreatedAt:   u.GetCreated(),
				LastLoginAt: u.GetLastAuth(),
			}, nil
		}
	}

	return nil, errors.NewNotFound("rbac.GetUser", "user not found: "+username)
}

// handleUpdateUser updates a user's role assignments by computing the diff
// between current and requested roles, then issuing RevokeRole and AssignRole
// calls as needed. The proto service does not expose a dedicated UpdateUser RPC.
func (h *rbacHandler) handleUpdateUser(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("rbac.handleUpdateUser", "invalid request type")
	}

	username, _ := getString(reqMap, "Username")
	requestedRoles, _ := getStringSlice(reqMap, "Roles")

	resp, err := client.ListUsers(ctx, &pb.ListUsersRequest{})
	if err != nil {
		return nil, convertGRPCError(err, "rbac.UpdateUser")
	}

	var currentRoles []string
	found := false
	for _, u := range resp.GetUsers() {
		if u.GetUsername() == username {
			currentRoles = u.GetRoles()
			found = true
			break
		}
	}

	if !found {
		return nil, errors.NewNotFound("rbac.UpdateUser", "user not found: "+username)
	}

	// Build lookup sets for O(1) membership checks.
	currentSet := make(map[string]struct{}, len(currentRoles))
	for _, role := range currentRoles {
		currentSet[role] = struct{}{}
	}

	requestedSet := make(map[string]struct{}, len(requestedRoles))
	for _, role := range requestedRoles {
		requestedSet[role] = struct{}{}
	}

	// Revoke roles that are in current but not in requested.
	for _, role := range currentRoles {
		if _, exists := requestedSet[role]; !exists {
			_, err := client.RevokeRole(ctx, &pb.RevokeRoleRequest{
				UserId:   username,
				RoleName: role,
			})
			if err != nil {
				return nil, convertGRPCError(err, "rbac.UpdateUser")
			}
		}
	}

	// Assign roles that are in requested but not in current.
	for _, role := range requestedRoles {
		if _, exists := currentSet[role]; !exists {
			_, err := client.AssignRole(ctx, &pb.AssignRoleRequest{
				UserId:   username,
				RoleName: role,
			})
			if err != nil {
				return nil, convertGRPCError(err, "rbac.UpdateUser")
			}
		}
	}

	return &rbac.AssignRoleResponse{
		Success: true,
		Message: "User updated successfully",
	}, nil
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
