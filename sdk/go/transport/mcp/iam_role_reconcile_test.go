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

// iam_role_reconcile_test.go covers built-in role reconciliation over MCP:
// the service/method -> tool-name mapping and a full round trip through a real
// in-process MCP server, so a drift between the SDK's tool name or the report
// shape and the server's fails here rather than in production.

package mcp

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// TestRoleReconcileToolMapping pins quicbaas.IAM.Role/Reconcile onto the tool
// the server registers. The name is fixed across every transport.
func TestRoleReconcileToolMapping(t *testing.T) {
	toolName, found := mapIAMToolName("quicbaas.IAM.Role", "Reconcile")
	if !found {
		t.Fatal("Role.Reconcile is not mapped; the MCP transport would fall back to a name the server does not register")
	}
	if toolName != "quicbaas_iam_role_reconcile" {
		t.Errorf("tool = %q, want quicbaas_iam_role_reconcile", toolName)
	}

	if got := ToolName("quicbaas.IAM.Role", "Reconcile"); got != "quicbaas_iam_role_reconcile" {
		t.Errorf("ToolName = %q, want quicbaas_iam_role_reconcile", got)
	}
}

// TestRoleReconcileToolMappingUnknownMethod proves a neighbouring but
// non-existent role verb is reported unmapped instead of resolving to the
// reconcile tool.
func TestRoleReconcileToolMappingUnknownMethod(t *testing.T) {
	if toolName, found := mapIAMToolName("quicbaas.IAM.Role", "ReconcileEverything"); found {
		t.Errorf("Role.ReconcileEverything resolved to %q, want unmapped", toolName)
	}
}

// TestRoleReconcileEndToEnd runs the whole path: the SDK role client sends
// Reconcile, the transport calls the tool by name with a flat tenant_id, and
// the server's {tenant_id, roles[], changed} report parses back into the typed
// response with the added and removed verbs intact.
func TestRoleReconcileEndToEnd(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_iam_role_reconcile",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["tenant_id"] != "acme" {
				t.Errorf("tenant_id = %v, want acme", args["tenant_id"])
			}
			if _, nested := args["request"]; nested {
				t.Error("arguments carry a nested request object; the tool schema is flat")
			}
		},
		result: map[string]interface{}{
			"tenant_id": "acme",
			"changed":   true,
			"roles": []interface{}{
				map[string]interface{}{
					"role_id": "role-admin",
					"name":    "admin",
					"added":   []interface{}{"roles:create"},
					"removed": []interface{}{"roles:reconcile"},
				},
				map[string]interface{}{
					"role_id": "role-viewer",
					"name":    "viewer",
					"created": true,
					"added":   []interface{}{"roles:read"},
				},
			},
		},
	})
	defer cleanup()

	resp, err := iam.NewRoleClient(tr).Reconcile(context.Background(), "acme")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if resp.TenantID != "acme" {
		t.Errorf("TenantID = %q, want acme", resp.TenantID)
	}
	if !resp.Changed {
		t.Error("Changed = false, want true")
	}
	if len(resp.Roles) != 2 {
		t.Fatalf("Roles = %d entries, want 2", len(resp.Roles))
	}

	admin := resp.Roles[0]
	if admin.RoleID != "role-admin" || admin.Name != "admin" {
		t.Errorf("admin entry = %+v", admin)
	}
	if len(admin.Added) != 1 || admin.Added[0] != "roles:create" {
		t.Errorf("admin.Added = %v, want [roles:create]", admin.Added)
	}
	if len(admin.Removed) != 1 || admin.Removed[0] != "roles:reconcile" {
		t.Errorf("admin.Removed = %v, want [roles:reconcile]", admin.Removed)
	}

	if viewer := resp.Roles[1]; !viewer.Created {
		t.Errorf("viewer entry = %+v, want Created", viewer)
	}
}

// TestRoleReconcileEndToEndConverged proves the idempotent case survives the
// round trip: a tenant that already agrees with the build reports no changed
// roles, and that is a success, not an error.
func TestRoleReconcileEndToEndConverged(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_iam_role_reconcile",
		result: map[string]interface{}{
			"tenant_id": "acme",
			"changed":   false,
		},
	})
	defer cleanup()

	resp, err := iam.NewRoleClient(tr).Reconcile(context.Background(), "acme")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if resp.Changed {
		t.Error("Changed = true, want false for a converged tenant")
	}
	if len(resp.Roles) != 0 {
		t.Errorf("Roles = %v, want none", resp.Roles)
	}
}

// TestRoleReconcileToolError proves a server-side failure surfaces as an error
// instead of an empty, apparently-successful report.
func TestRoleReconcileToolError(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:  "quicbaas_iam_role_reconcile",
		toolError: true,
		rawText:   "tenant not found",
	})
	defer cleanup()

	if _, err := iam.NewRoleClient(tr).Reconcile(context.Background(), "missing"); err == nil {
		t.Fatal("expected an error from a tool-level failure")
	}
}

// TestRoleReconcileRejectsEmptyTenant proves the client refuses before it ever
// reaches a transport: reconciliation is always scoped to a named tenant.
func TestRoleReconcileRejectsEmptyTenant(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_iam_role_reconcile",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			t.Errorf("tool was called with %v; an empty tenant must never reach the server", args)
		},
		result: map[string]interface{}{"tenant_id": "", "changed": false},
	})
	defer cleanup()

	if _, err := iam.NewRoleClient(tr).Reconcile(context.Background(), ""); err == nil {
		t.Fatal("expected an error for an empty tenant ID")
	}
}
