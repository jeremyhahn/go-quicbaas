package embedded

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rbac"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// The rbac client sends its own request types; these tests prove each one
// reaches the service over the embedded transport, fields intact, and that
// the service's answer comes back as the client's response type (#1544).

func TestCreateRole_RBACRequestReachesTheService(t *testing.T) {
	var got *service.AuthCreateRoleRequest
	tr, err := New(&mockQuicBaaS{createAuthRoleFunc: func(_ context.Context, req *service.AuthCreateRoleRequest) (*service.AuthCreateRoleResponse, error) {
		got = req
		return &service.AuthCreateRoleResponse{RoleID: "role-7", Timestamp: 1700}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := rbac.NewClient(tr).CreateRole(context.Background(), &rbac.CreateRoleRequest{
		RoleName:    "auditor",
		Permissions: []string{"kv:read", "audit:read"},
	})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if got == nil || got.RoleName != "auditor" || !reflect.DeepEqual(got.Permissions, []string{"kv:read", "audit:read"}) {
		t.Fatalf("service received %+v", got)
	}
	if resp.RoleID != "role-7" || resp.Timestamp != 1700 {
		t.Fatalf("CreateRole response = %+v", resp)
	}
}

func TestDeleteRole_RBACRequestReachesTheService(t *testing.T) {
	var got *service.AuthDeleteRoleRequest
	tr, err := New(&mockQuicBaaS{deleteAuthRoleFunc: func(_ context.Context, req *service.AuthDeleteRoleRequest) (*service.AuthDeleteRoleResponse, error) {
		got = req
		return &service.AuthDeleteRoleResponse{Success: true, Message: "deleted"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := rbac.NewClient(tr).DeleteRole(context.Background(), &rbac.DeleteRoleRequest{RoleName: "auditor"})
	if err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}
	if got == nil || got.RoleName != "auditor" {
		t.Fatalf("service received %+v", got)
	}
	if !resp.Success || resp.Message != "deleted" {
		t.Fatalf("DeleteRole response = %+v", resp)
	}
}

func TestListRoles_RBACRequestReachesTheService(t *testing.T) {
	var got *service.AuthListRolesRequest
	tr, err := New(&mockQuicBaaS{listAuthRolesFunc: func(_ context.Context, req *service.AuthListRolesRequest) (*service.AuthListRolesResponse, error) {
		got = req
		return &service.AuthListRolesResponse{
			Roles:      []*service.RoleInfo{{RoleID: "role-7", RoleName: "auditor", Permissions: []string{"kv:read"}, CreatedAt: 1700}},
			TotalCount: 3,
		}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := rbac.NewClient(tr).ListRoles(context.Background(), &rbac.ListRolesRequest{Limit: 10, Offset: 2})
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if got == nil || got.Limit != 10 || got.Offset != 2 {
		t.Fatalf("service received %+v", got)
	}
	want := []*rbac.RoleInfo{{RoleID: "role-7", RoleName: "auditor", Permissions: []string{"kv:read"}, CreatedAt: 1700}}
	if resp.TotalCount != 3 || !reflect.DeepEqual(resp.Roles, want) {
		t.Fatalf("ListRoles response = %+v", resp)
	}
}

func TestAssignRole_RBACRequestReachesTheService(t *testing.T) {
	var got *service.AuthAssignRoleRequest
	tr, err := New(&mockQuicBaaS{assignRoleFunc: func(_ context.Context, req *service.AuthAssignRoleRequest) (*service.AuthAssignRoleResponse, error) {
		got = req
		return &service.AuthAssignRoleResponse{Success: true, Message: "assigned"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := rbac.NewClient(tr).AssignRole(context.Background(), &rbac.AssignRoleRequest{Username: "42", RoleName: "auditor"})
	if err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	if got == nil || got.Username != "42" || got.RoleName != "auditor" {
		t.Fatalf("service received %+v", got)
	}
	if !resp.Success || resp.Message != "assigned" {
		t.Fatalf("AssignRole response = %+v", resp)
	}
}

func TestRevokeRole_RBACRequestReachesTheService(t *testing.T) {
	var got *service.AuthRevokeRoleRequest
	tr, err := New(&mockQuicBaaS{revokeRoleFunc: func(_ context.Context, req *service.AuthRevokeRoleRequest) (*service.AuthRevokeRoleResponse, error) {
		got = req
		return &service.AuthRevokeRoleResponse{Success: true, Message: "revoked"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := rbac.NewClient(tr).RevokeRole(context.Background(), &rbac.RevokeRoleRequest{Username: "42", RoleName: "auditor"})
	if err != nil {
		t.Fatalf("RevokeRole: %v", err)
	}
	if got == nil || got.Username != "42" || got.RoleName != "auditor" {
		t.Fatalf("service received %+v", got)
	}
	if !resp.Success || resp.Message != "revoked" {
		t.Fatalf("RevokeRole response = %+v", resp)
	}
}

func TestRoleHandlers_ServiceErrorReachesTheClient(t *testing.T) {
	denied := sdkerrors.NewUnavailable("rbac.CreateRole", "role store unavailable")
	tr, err := New(&mockQuicBaaS{createAuthRoleFunc: func(context.Context, *service.AuthCreateRoleRequest) (*service.AuthCreateRoleResponse, error) {
		return nil, denied
	}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = rbac.NewClient(tr).CreateRole(context.Background(), &rbac.CreateRoleRequest{RoleName: "auditor"})
	if !errors.Is(err, denied) {
		t.Fatalf("CreateRole error = %v, want %v", err, denied)
	}
}

func TestRoleHandlers_WrongRequestTypeIsRefused(t *testing.T) {
	tr, err := New(&mockQuicBaaS{})
	if err != nil {
		t.Fatal(err)
	}

	for _, method := range []string{"CreateRole", "DeleteRole", "ListRoles", "AssignRole", "RevokeRole"} {
		t.Run(method, func(t *testing.T) {
			_, err := tr.Request(context.Background(), "quicbaas.RBAC", method, "auditor")
			var typed *InvalidRequestTypeError
			if !errors.As(err, &typed) {
				t.Fatalf("%s error = %v, want *InvalidRequestTypeError", method, err)
			}
			if typed.Method != method || typed.ActualType != "string" {
				t.Fatalf("%s error = %+v", method, typed)
			}
		})
	}
}
