// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rbac"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// The full verb set is exercised against the real server router in
// pkg/api/rest/handlers_rbac_sdk_wire_test.go; these tests pin the routing,
// the tenant choice and the input checks made before any request is sent.

// refusingServer fails the test if the handler dials it.
func refusingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("handler must reject the request before dialing the server (%s %s)", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestIsRBACService(t *testing.T) {
	if !isRBACService("quicbaas.RBAC") {
		t.Error("quicbaas.RBAC must route to the RBAC handler")
	}
	if isRBACService("quicbaas.IAM") {
		t.Error("quicbaas.IAM must not route to the RBAC handler")
	}
}

func TestRBACRESTHandler_ContextTenantWinsOverDefault(t *testing.T) {
	srv, seen := recordingServer(t, http.MethodPost,
		"/api/v1/tenants/acme/users/42/roles", struct{}{})
	defer srv.Close()
	tr := newTestTransport(t, srv)
	defer tr.Close()
	tr.defaultTenant = "fallback"

	ctx := transport.ContextWithTenant(context.Background(), "acme")
	resp, err := newRBACRESTHandler(tr).handleRequest(ctx, "AssignRole",
		&rbac.AssignRoleRequest{Username: "42", RoleName: "auditor"})
	if err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	if !resp.(*rbac.AssignRoleResponse).Success {
		t.Error("AssignRole must report success")
	}
	if *seen != "POST /api/v1/tenants/acme/users/42/roles" {
		t.Errorf("unexpected request %q", *seen)
	}
}

func TestRBACRESTHandler_DefaultTenantAddressesRole(t *testing.T) {
	srv, seen := recordingServer(t, http.MethodDelete,
		"/api/v1/tenants/fallback/roles/auditor", struct{}{})
	defer srv.Close()
	tr := newTestTransport(t, srv)
	defer tr.Close()
	tr.defaultTenant = "fallback"

	if _, err := newRBACRESTHandler(tr).handleRequest(context.Background(), "DeleteRole",
		&rbac.DeleteRoleRequest{RoleName: "auditor"}); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}
	if *seen != "DELETE /api/v1/tenants/fallback/roles/auditor" {
		t.Errorf("unexpected request %q", *seen)
	}
}

func TestRBACRESTHandler_RequiresTenant(t *testing.T) {
	tr := newTestTransport(t, refusingServer(t))
	defer tr.Close()

	_, err := newRBACRESTHandler(tr).handleRequest(context.Background(), "ListRoles", &rbac.ListRolesRequest{})
	if !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("a request with no tenant must be invalid input, got %v", err)
	}
}

func TestRBACRESTHandler_RejectsBadRequests(t *testing.T) {
	tr := newTestTransport(t, refusingServer(t))
	defer tr.Close()
	tr.defaultTenant = "acme"

	cases := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"nil create", "CreateRole", nil},
		{"unnamed delete", "DeleteRole", &rbac.DeleteRoleRequest{}},
		{"unnamed assign", "AssignRole", &rbac.AssignRoleRequest{Username: "42"}},
		{"unnamed revoke", "RevokeRole", &rbac.RevokeRoleRequest{Username: "42"}},
		{"non-numeric username", "AssignRole", &rbac.AssignRoleRequest{Username: "alice", RoleName: "auditor"}},
		{"negative username", "RevokeRole", &rbac.RevokeRoleRequest{Username: "-1", RoleName: "auditor"}},
	}
	for _, tc := range cases {
		_, err := newRBACRESTHandler(tr).handleRequest(context.Background(), tc.method, tc.req)
		if !sdkerrors.IsInvalidInput(err) {
			t.Errorf("%s must be invalid input, got %v", tc.name, err)
		}
	}
}

func TestRBACRESTHandler_UnknownMethod(t *testing.T) {
	tr := newTestTransport(t, refusingServer(t))
	defer tr.Close()

	_, err := newRBACRESTHandler(tr).handleRequest(context.Background(), "GetUserRoles", nil)
	if !sdkerrors.IsUnimplemented(err) {
		t.Errorf("expected CodeUnimplemented, got %v", err)
	}
}
