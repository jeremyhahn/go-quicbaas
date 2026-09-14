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

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
)

// fakeIAMServer is a configurable in-memory IAMService backend. Each method
// returns a populated proto response so the handler conversion paths execute
// fully; when fail is set, every method returns an Internal error so the
// error-propagation paths are exercised.
type fakeIAMServer struct {
	iampb.UnimplementedIAMServiceServer
	fail bool
}

func (f *fakeIAMServer) err() error {
	if f.fail {
		return status.Error(codes.Internal, "boom")
	}
	return nil
}

func iamTime() *timestamppb.Timestamp { return timestamppb.New(time.Unix(1700000000, 0)) }

func sampleUser() *iampb.User {
	return &iampb.User{
		Id: "u1", TenantId: "t1", Email: "a@b.c", Name: "alice",
		Roles: []string{"admin"}, Groups: []string{"g1"},
		Attributes: map[string]string{"k": "v"},
		CreatedAt:  iamTime(), UpdatedAt: iamTime(), LastAuth: iamTime(),
	}
}

func sampleTenant() *iampb.Tenant {
	return &iampb.Tenant{
		Id: "t1", Name: "tenant",
		Settings:  &iampb.TenantSettings{NamedNetworks: map[string]string{"n": "1"}},
		CreatedAt: iamTime(), UpdatedAt: iamTime(),
	}
}

func sampleGroup() *iampb.Group {
	return &iampb.Group{
		Id: "g1", TenantId: "t1", Name: "group", Description: "d",
		Members: []string{"u1", "u2"}, CreatedAt: iamTime(), UpdatedAt: iamTime(),
	}
}

func sampleRole() *iampb.Role {
	return &iampb.Role{
		Id: "r1", TenantId: "t1", Name: "role", Description: "d",
		Permissions: []string{"p1", "p0"}, RetiredPermissions: []string{"p0"},
		CreatedAt: iamTime(), UpdatedAt: iamTime(),
	}
}

func sampleAdminAssignment() *iampb.AdminAssignment {
	return &iampb.AdminAssignment{
		Id: "a1", UserId: "u1", AdminRole: iampb.AdminRole(1),
		TenantIds: []string{"t1"}, Permissions: []string{"p1"}, Active: true,
		GrantedBy: "root", Metadata: map[string]string{"k": "v"},
		ExpiresAt: iamTime(), GrantedAt: iamTime(), LastUsedAt: iamTime(),
	}
}

func sampleImpersonation() *iampb.ImpersonationSession {
	return &iampb.ImpersonationSession{
		Id: "s1", AdminUserId: "admin", TargetUserId: "target", TenantId: "t1",
		Reason: "support", StartedAt: iamTime(), ExpiresAt: iamTime(),
	}
}

// User methods.
func (f *fakeIAMServer) CreateUser(context.Context, *iampb.CreateUserRequest) (*iampb.User, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleUser(), nil
}
func (f *fakeIAMServer) GetUser(context.Context, *iampb.GetUserRequest) (*iampb.User, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleUser(), nil
}
func (f *fakeIAMServer) UpdateUser(context.Context, *iampb.UpdateUserRequest) (*iampb.User, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleUser(), nil
}
func (f *fakeIAMServer) DeleteUser(context.Context, *iampb.DeleteUserRequest) (*iampb.DeleteUserResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.DeleteUserResponse{}, nil
}
func (f *fakeIAMServer) ListUsers(context.Context, *iampb.ListUsersRequest) (*iampb.ListUsersResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.ListUsersResponse{Users: []*iampb.User{sampleUser()}, Total: 1}, nil
}
func (f *fakeIAMServer) AssignRole(context.Context, *iampb.AssignRoleRequest) (*iampb.AssignRoleResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.AssignRoleResponse{}, nil
}
func (f *fakeIAMServer) RevokeRole(context.Context, *iampb.RevokeRoleRequest) (*iampb.RevokeRoleResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.RevokeRoleResponse{}, nil
}
func (f *fakeIAMServer) AddUserToGroup(context.Context, *iampb.AddUserToGroupRequest) (*iampb.AddUserToGroupResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.AddUserToGroupResponse{}, nil
}
func (f *fakeIAMServer) RemoveUserFromGroup(context.Context, *iampb.RemoveUserFromGroupRequest) (*iampb.RemoveUserFromGroupResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.RemoveUserFromGroupResponse{}, nil
}

// Tenant methods.
func (f *fakeIAMServer) CreateTenant(context.Context, *iampb.CreateTenantRequest) (*iampb.Tenant, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleTenant(), nil
}
func (f *fakeIAMServer) GetTenant(context.Context, *iampb.GetTenantRequest) (*iampb.Tenant, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleTenant(), nil
}
func (f *fakeIAMServer) UpdateTenant(context.Context, *iampb.UpdateTenantRequest) (*iampb.Tenant, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleTenant(), nil
}
func (f *fakeIAMServer) DeleteTenant(context.Context, *iampb.DeleteTenantRequest) (*iampb.DeleteTenantResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.DeleteTenantResponse{}, nil
}
func (f *fakeIAMServer) ListTenants(context.Context, *iampb.ListTenantsRequest) (*iampb.ListTenantsResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.ListTenantsResponse{Tenants: []*iampb.Tenant{sampleTenant()}, Total: 1}, nil
}

// Group methods.
func (f *fakeIAMServer) CreateGroup(context.Context, *iampb.CreateGroupRequest) (*iampb.Group, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleGroup(), nil
}
func (f *fakeIAMServer) GetGroup(context.Context, *iampb.GetGroupRequest) (*iampb.Group, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleGroup(), nil
}
func (f *fakeIAMServer) UpdateGroup(context.Context, *iampb.UpdateGroupRequest) (*iampb.Group, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleGroup(), nil
}
func (f *fakeIAMServer) DeleteGroup(context.Context, *iampb.DeleteGroupRequest) (*iampb.DeleteGroupResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.DeleteGroupResponse{}, nil
}
func (f *fakeIAMServer) ListGroups(context.Context, *iampb.ListGroupsRequest) (*iampb.ListGroupsResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.ListGroupsResponse{Groups: []*iampb.Group{sampleGroup()}, Total: 1}, nil
}

// Role methods.
func (f *fakeIAMServer) CreateRole(context.Context, *iampb.CreateRoleRequest) (*iampb.Role, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleRole(), nil
}
func (f *fakeIAMServer) GetRole(context.Context, *iampb.GetRoleRequest) (*iampb.Role, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleRole(), nil
}
func (f *fakeIAMServer) UpdateRole(context.Context, *iampb.UpdateRoleRequest) (*iampb.Role, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleRole(), nil
}
func (f *fakeIAMServer) DeleteRole(context.Context, *iampb.DeleteRoleRequest) (*iampb.DeleteRoleResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.DeleteRoleResponse{}, nil
}
func (f *fakeIAMServer) ListRoles(context.Context, *iampb.ListRolesRequest) (*iampb.ListRolesResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.ListRolesResponse{Roles: []*iampb.Role{sampleRole()}, Total: 1}, nil
}

func (f *fakeIAMServer) ReconcileRoles(_ context.Context, req *iampb.ReconcileRolesRequest) (*iampb.ReconcileRolesResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.ReconcileRolesResponse{
		TenantId: req.TenantId,
		Roles: []*iampb.RoleReconcileResult{
			{RoleId: "r1", Name: "role", Created: true, Added: []string{"p1"}, Removed: []string{"p0"}},
		},
		Changed: true,
	}, nil
}

// Admin methods.
func (f *fakeIAMServer) CreateAdminAssignment(context.Context, *iampb.CreateAdminAssignmentRequest) (*iampb.AdminAssignment, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleAdminAssignment(), nil
}
func (f *fakeIAMServer) GetAdminAssignment(context.Context, *iampb.GetAdminAssignmentRequest) (*iampb.AdminAssignment, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleAdminAssignment(), nil
}
func (f *fakeIAMServer) UpdateAdminAssignment(context.Context, *iampb.UpdateAdminAssignmentRequest) (*iampb.AdminAssignment, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleAdminAssignment(), nil
}
func (f *fakeIAMServer) DeleteAdminAssignment(context.Context, *iampb.DeleteAdminAssignmentRequest) (*iampb.DeleteAdminAssignmentResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.DeleteAdminAssignmentResponse{}, nil
}
func (f *fakeIAMServer) ListAdminAssignments(context.Context, *iampb.ListAdminAssignmentsRequest) (*iampb.AdminAssignmentList, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.AdminAssignmentList{Assignments: []*iampb.AdminAssignment{sampleAdminAssignment()}, Total: 5}, nil
}
func (f *fakeIAMServer) CheckAdminPermission(context.Context, *iampb.CheckAdminPermissionRequest) (*iampb.CheckAdminPermissionResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.CheckAdminPermissionResponse{Allowed: true, AssignmentId: "a1", AdminRole: iampb.AdminRole(2), Reason: "ok"}, nil
}
func (f *fakeIAMServer) StartImpersonation(context.Context, *iampb.StartImpersonationRequest) (*iampb.ImpersonationSession, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleImpersonation(), nil
}
func (f *fakeIAMServer) EndImpersonation(context.Context, *iampb.EndImpersonationRequest) (*iampb.EndImpersonationResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.EndImpersonationResponse{}, nil
}
func (f *fakeIAMServer) GetActiveImpersonations(context.Context, *iampb.GetActiveImpersonationsRequest) (*iampb.ImpersonationSessionList, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.ImpersonationSessionList{Sessions: []*iampb.ImpersonationSession{sampleImpersonation()}}, nil
}

func newIAMHandlerWithServer(t *testing.T, fail bool) *iamHandler {
	srv := newHandlerServer(t, func(s *grpc.Server) {
		iampb.RegisterIAMServiceServer(s, &fakeIAMServer{fail: fail})
	})
	return newIAMHandler(srv.dial(t))
}

// iamCase drives one IAM service/method through the handler.
type iamCase struct {
	name    string
	service string
	method  string
	req     interface{}
}

var iamServiceCases = []iamCase{
	// User service.
	{"CreateUser", "quicbaas.IAM.User", "Create", map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"email": "a@b.c", "username": "alice", "roles": []string{"admin"}}}},
	{"GetUser", "quicbaas.IAM.User", "Get", map[string]interface{}{"tenant_id": "t1", "user_id": "u1"}},
	{"UpdateUser", "quicbaas.IAM.User", "Update", map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "request": map[string]interface{}{"email": "a@b.c"}}},
	{"DeleteUser", "quicbaas.IAM.User", "Delete", map[string]interface{}{"tenant_id": "t1", "user_id": "u1"}},
	{"ListUsers", "quicbaas.IAM.User", "List", map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"page_size": 10}}},
	{"AssignRole", "quicbaas.IAM.User", "AssignRole", map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "role_id": "r1"}},
	{"RevokeRole", "quicbaas.IAM.User", "RevokeRole", map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "role_id": "r1"}},
	{"AddToGroup", "quicbaas.IAM.User", "AddToGroup", map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "group_id": "g1"}},
	{"RemoveFromGroup", "quicbaas.IAM.User", "RemoveFromGroup", map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "group_id": "g1"}},
	// Tenant service.
	{"CreateTenant", "quicbaas.IAM.Tenant", "Create", map[string]interface{}{"id": "t1", "name": "tenant", "settings": map[string]string{"n": "1"}}},
	{"GetTenant", "quicbaas.IAM.Tenant", "Get", map[string]interface{}{"tenant_id": "t1"}},
	{"UpdateTenant", "quicbaas.IAM.Tenant", "Update", map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"display_name": "n", "settings": map[string]string{"a": "b"}}}},
	{"DeleteTenant", "quicbaas.IAM.Tenant", "Delete", map[string]interface{}{"tenant_id": "t1"}},
	{"ListTenants", "quicbaas.IAM.Tenant", "List", map[string]interface{}{"page_size": 10}},
	// Group service.
	{"CreateGroup", "quicbaas.IAM.Group", "Create", map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"name": "group"}}},
	{"GetGroup", "quicbaas.IAM.Group", "Get", map[string]interface{}{"tenant_id": "t1", "group_id": "g1"}},
	{"UpdateGroup", "quicbaas.IAM.Group", "Update", map[string]interface{}{"tenant_id": "t1", "group_id": "g1", "request": map[string]interface{}{"display_name": "g"}}},
	{"DeleteGroup", "quicbaas.IAM.Group", "Delete", map[string]interface{}{"tenant_id": "t1", "group_id": "g1"}},
	{"ListGroups", "quicbaas.IAM.Group", "List", map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"page_size": 10}}},
	{"AddGroupMember", "quicbaas.IAM.Group", "AddMember", map[string]interface{}{"tenant_id": "t1", "group_id": "g1", "user_id": "u1"}},
	{"RemoveGroupMember", "quicbaas.IAM.Group", "RemoveMember", map[string]interface{}{"tenant_id": "t1", "group_id": "g1", "user_id": "u1"}},
	{"ListGroupMembers", "quicbaas.IAM.Group", "ListMembers", map[string]interface{}{"tenant_id": "t1", "group_id": "g1"}},
	// Role service.
	{"CreateRole", "quicbaas.IAM.Role", "Create", map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"name": "role", "permissions": []string{"p1"}}}},
	{"GetRole", "quicbaas.IAM.Role", "Get", map[string]interface{}{"tenant_id": "t1", "role_id": "r1"}},
	{"UpdateRole", "quicbaas.IAM.Role", "Update", map[string]interface{}{"tenant_id": "t1", "role_id": "r1", "request": map[string]interface{}{"name": "role"}}},
	{"DeleteRole", "quicbaas.IAM.Role", "Delete", map[string]interface{}{"tenant_id": "t1", "role_id": "r1"}},
	{"ListRoles", "quicbaas.IAM.Role", "List", map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"page_size": 10}}},
	{"ReconcileRoles", "quicbaas.IAM.Role", "Reconcile", map[string]interface{}{"tenant_id": "t1"}},
	// Admin service.
	{"CreateAssignment", "quicbaas.IAM.Admin", "CreateAssignment", map[string]interface{}{"user_id": "u1", "admin_role": 1, "tenant_ids": []string{"t1"}}},
	{"GetAssignment", "quicbaas.IAM.Admin", "GetAssignment", map[string]interface{}{"id": "a1"}},
	{"UpdateAssignment", "quicbaas.IAM.Admin", "UpdateAssignment", map[string]interface{}{"id": "a1", "admin_role": 1, "active": true}},
	{"DeleteAssignment", "quicbaas.IAM.Admin", "DeleteAssignment", map[string]interface{}{"id": "a1"}},
	{"ListAssignments", "quicbaas.IAM.Admin", "ListAssignments", map[string]interface{}{"user_id": "u1", "active_only": true}},
	{"CheckPermission", "quicbaas.IAM.Admin", "CheckPermission", map[string]interface{}{"user_id": "u1", "permission": "p1"}},
	{"StartImpersonation", "quicbaas.IAM.Admin", "StartImpersonation", map[string]interface{}{"admin_user_id": "admin", "target_user_id": "target", "duration_seconds": 60}},
	{"EndImpersonation", "quicbaas.IAM.Admin", "EndImpersonation", map[string]interface{}{"session_id": "s1"}},
	{"ListActiveImpersonations", "quicbaas.IAM.Admin", "ListActiveImpersonations", map[string]interface{}{"admin_user_id": "admin"}},
}

func TestIAMService_AllMethods_Success(t *testing.T) {
	h := newIAMHandlerWithServer(t, false)
	for _, tc := range iamServiceCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.handleRequest(context.Background(), tc.service, tc.method, tc.req)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
		})
	}
}

func TestIAMService_AllMethods_ServerError(t *testing.T) {
	h := newIAMHandlerWithServer(t, true)
	for _, tc := range iamServiceCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.handleRequest(context.Background(), tc.service, tc.method, tc.req)
			if err == nil {
				t.Fatalf("%s: expected error from failing server", tc.name)
			}
		})
	}
}

func TestIAMService_UnsupportedService(t *testing.T) {
	h := newIAMHandler(nil)
	_, err := h.handleRequest(context.Background(), "quicbaas.IAM.Bogus", "X", nil)
	if err == nil {
		t.Fatal("expected error for unsupported IAM service")
	}
}

func TestIAMService_UnsupportedMethods(t *testing.T) {
	h := newIAMHandler(nil)
	services := []string{
		"quicbaas.IAM.User", "quicbaas.IAM.Tenant", "quicbaas.IAM.Group",
		"quicbaas.IAM.Role", "quicbaas.IAM.Policy", "quicbaas.IAM.App",
		"quicbaas.IAM.Session", "quicbaas.IAM.Audit", "quicbaas.IAM.Admin",
		"quicbaas.IAM.GDPR", "quicbaas.IAM.PEP", "quicbaas.IAM.MFA",
	}
	for _, svc := range services {
		_, err := h.handleRequest(context.Background(), svc, "NopeMethod", map[string]interface{}{})
		if err == nil {
			t.Errorf("%s: expected unsupported-method error", svc)
		}
	}
}

// guard against helper drift
var _ = durationpb.New
