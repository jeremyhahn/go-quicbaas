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

package grpc

import (
	"context"
	"testing"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// TestHandleReconcileRoles_Success drives quicbaas.IAM.Role/Reconcile through
// the handler and asserts the values that reach the SDK, not merely that no
// error occurred: a handler that answered with an empty report would look
// healthy to the dispatch-table test while telling every caller that a drifted
// tenant needs no repair.
func TestHandleReconcileRoles_Success(t *testing.T) {
	h := newIAMHandlerWithServer(t, false)

	resp, err := h.handleRequest(context.Background(), "quicbaas.IAM.Role", "Reconcile",
		map[string]interface{}{"tenant_id": "t1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, ok := resp.(*iam.ReconcileRolesResponse)
	if !ok {
		t.Fatalf("expected *iam.ReconcileRolesResponse, got %T", resp)
	}
	if result.TenantID != "t1" {
		t.Errorf("tenant id = %q, want %q", result.TenantID, "t1")
	}
	if !result.Changed {
		t.Error("changed = false, want true when the server reported repaired roles")
	}
	if len(result.Roles) != 1 {
		t.Fatalf("roles = %d, want 1", len(result.Roles))
	}
	role := result.Roles[0]
	if role.RoleID != "r1" || role.Name != "role" || !role.Created {
		t.Errorf("role = %+v, want r1/role/created", role)
	}
	if len(role.Added) != 1 || role.Added[0] != "p1" {
		t.Errorf("added = %v, want [p1]", role.Added)
	}
	if len(role.Removed) != 1 || role.Removed[0] != "p0" {
		t.Errorf("removed = %v, want [p0]", role.Removed)
	}
}

// TestHandleReconcileRoles_ServerError proves the gRPC status is converted
// rather than swallowed, so a refusal from a storage that cannot reach qrdb's
// built-in definitions reaches the caller as an error.
func TestHandleReconcileRoles_ServerError(t *testing.T) {
	h := newIAMHandlerWithServer(t, true)

	if _, err := h.handleRequest(context.Background(), "quicbaas.IAM.Role", "Reconcile",
		map[string]interface{}{"tenant_id": "t1"}); err == nil {
		t.Fatal("expected error from failing server")
	}
}

// TestHandleReconcileRoles_InvalidRequest covers the bad-input path: a request
// the handler cannot read as a map is rejected before any RPC is attempted.
func TestHandleReconcileRoles_InvalidRequest(t *testing.T) {
	h := newIAMHandlerWithServer(t, false)

	if _, err := h.handleRequest(context.Background(), "quicbaas.IAM.Role", "Reconcile", nil); err == nil {
		t.Fatal("expected error for an unreadable request")
	}
}

// TestConvertProtoRoleToSDK_RetiredPermissions pins that the drift a role
// carries survives the hop off the wire. Retired permissions are derived
// server-side on every read, so a converter that dropped them would hide the
// drift from every gRPC caller while the repair verb sat unused.
func TestConvertProtoRoleToSDK_RetiredPermissions(t *testing.T) {
	result := convertProtoRoleToSDK(&iampb.Role{
		Id: "r1", TenantId: "t1", Name: "admin",
		Permissions:        []string{"kv:get", "kv:legacy"},
		RetiredPermissions: []string{"kv:legacy"},
	})

	if result == nil {
		t.Fatal("expected a converted role")
	}
	if len(result.RetiredPermissions) != 1 || result.RetiredPermissions[0] != "kv:legacy" {
		t.Errorf("retired permissions = %v, want [kv:legacy]", result.RetiredPermissions)
	}
}

// TestConvertProtoRoleToSDK_NoRetiredPermissions is the agreeing-with-the-build
// case: a role with no drift carries nothing.
func TestConvertProtoRoleToSDK_NoRetiredPermissions(t *testing.T) {
	result := convertProtoRoleToSDK(&iampb.Role{Id: "r1", TenantId: "t1", Name: "admin"})

	if result == nil {
		t.Fatal("expected a converted role")
	}
	if len(result.RetiredPermissions) != 0 {
		t.Errorf("retired permissions = %v, want none", result.RetiredPermissions)
	}
}

// TestConvertProtoReconcileRolesToSDK_Unchanged pins the idempotent answer: a
// tenant already agreeing with the build converts to Changed false and an
// empty (not nil-bearing) role slice.
func TestConvertProtoReconcileRolesToSDK_Unchanged(t *testing.T) {
	result := convertProtoReconcileRolesToSDK(&iampb.ReconcileRolesResponse{TenantId: "t1"})

	if result == nil {
		t.Fatal("expected a converted response")
	}
	if result.TenantID != "t1" {
		t.Errorf("tenant id = %q, want %q", result.TenantID, "t1")
	}
	if result.Changed {
		t.Error("changed = true, want false when nothing was written")
	}
	if len(result.Roles) != 0 {
		t.Errorf("roles = %v, want none", result.Roles)
	}
}

// TestConvertProtoReconcileRolesToSDK_SkipsNilResults guards the conversion
// against a malformed response: a nil entry must not become a zero-valued
// result claiming a role was reconciled.
func TestConvertProtoReconcileRolesToSDK_SkipsNilResults(t *testing.T) {
	result := convertProtoReconcileRolesToSDK(&iampb.ReconcileRolesResponse{
		TenantId: "t1",
		Roles: []*iampb.RoleReconcileResult{
			nil,
			{RoleId: "r1", Name: "admin", Added: []string{"roles:create"}},
		},
		Changed: true,
	})

	if result == nil {
		t.Fatal("expected a converted response")
	}
	if len(result.Roles) != 1 {
		t.Fatalf("roles = %d, want 1", len(result.Roles))
	}
	if result.Roles[0].RoleID != "r1" {
		t.Errorf("role id = %q, want %q", result.Roles[0].RoleID, "r1")
	}
}

func TestConvertProtoReconcileRolesToSDK_Nil(t *testing.T) {
	if result := convertProtoReconcileRolesToSDK(nil); result != nil {
		t.Errorf("expected nil for a nil response, got %+v", result)
	}
}
