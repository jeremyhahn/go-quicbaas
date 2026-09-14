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

package iam

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestRoleClient_Reconcile(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if service != roleServiceName {
					t.Errorf("expected service %s, got %s", roleServiceName, service)
				}
				if method != "Reconcile" {
					t.Errorf("expected method Reconcile, got %s", method)
				}
				reconcileReq, ok := req.(*reconcileRolesRequest)
				if !ok {
					t.Fatal("expected reconcileRolesRequest")
				}
				if reconcileReq.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", reconcileReq.TenantID)
				}
				return &ReconcileRolesResponse{
					TenantID: reconcileReq.TenantID,
					Roles: []RoleReconcileResult{
						{
							RoleID:  "role-1",
							Name:    "admin",
							Added:   []string{"sdlc:write"},
							Removed: []string{"legacy:read"},
						},
					},
					Changed: true,
				}, nil
			},
		}

		client := NewRoleClient(mt)
		resp, err := client.Reconcile(context.Background(), "tenant-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.TenantID != "tenant-123" {
			t.Errorf("expected tenant ID tenant-123, got %s", resp.TenantID)
		}
		if !resp.Changed {
			t.Error("expected changed to be true")
		}
		if len(resp.Roles) != 1 {
			t.Fatalf("expected 1 reconciled role, got %d", len(resp.Roles))
		}
		if resp.Roles[0].Name != "admin" {
			t.Errorf("expected role admin, got %s", resp.Roles[0].Name)
		}
		if len(resp.Roles[0].Added) != 1 || resp.Roles[0].Added[0] != "sdlc:write" {
			t.Errorf("expected added [sdlc:write], got %v", resp.Roles[0].Added)
		}
		if len(resp.Roles[0].Removed) != 1 || resp.Roles[0].Removed[0] != "legacy:read" {
			t.Errorf("expected removed [legacy:read], got %v", resp.Roles[0].Removed)
		}
	})

	t.Run("success with map response from REST", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return map[string]interface{}{
					"tenant_id": "tenant-123",
					"changed":   true,
					"roles": []interface{}{
						map[string]interface{}{
							"role_id": "role-1",
							"name":    "operator",
							"created": true,
							"added":   []interface{}{"kv:read", "kv:write"},
						},
					},
				}, nil
			},
		}

		client := NewRoleClient(mt)
		resp, err := client.Reconcile(context.Background(), "tenant-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.TenantID != "tenant-123" || !resp.Changed {
			t.Errorf("expected reconciled tenant-123 with changes, got %+v", resp)
		}
		if len(resp.Roles) != 1 {
			t.Fatalf("expected 1 reconciled role, got %d", len(resp.Roles))
		}
		if !resp.Roles[0].Created {
			t.Error("expected created to be true")
		}
		if len(resp.Roles[0].Added) != 2 {
			t.Errorf("expected 2 added permissions, got %v", resp.Roles[0].Added)
		}
		if len(resp.Roles[0].Removed) != 0 {
			t.Errorf("expected no removed permissions, got %v", resp.Roles[0].Removed)
		}
	})

	t.Run("idempotent no-change report", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return map[string]interface{}{
					"tenant_id": "tenant-123",
					"changed":   false,
				}, nil
			},
		}

		client := NewRoleClient(mt)
		resp, err := client.Reconcile(context.Background(), "tenant-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Changed {
			t.Error("expected changed to be false")
		}
		if len(resp.Roles) != 0 {
			t.Errorf("expected no reconciled roles, got %v", resp.Roles)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewRoleClient(&mockTransport{})
		_, err := client.Reconcile(context.Background(), "")
		if err == nil {
			t.Fatal("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewRoleClient(&mockTransport{})
		_, err := client.Reconcile(nil, "tenant-123")
		if err == nil {
			t.Fatal("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewNotFound("iam.role.Reconcile", "tenant not found")
			},
		}

		client := NewRoleClient(mt)
		_, err := client.Reconcile(context.Background(), "tenant-123")
		if err == nil {
			t.Fatal("expected transport error")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewRoleClient(mt)
		_, err := client.Reconcile(context.Background(), "tenant-123")
		if err == nil {
			t.Fatal("expected error for nil response")
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "not a reconcile report", nil
			},
		}

		client := NewRoleClient(mt)
		_, err := client.Reconcile(context.Background(), "tenant-123")
		if err == nil {
			t.Fatal("expected error for invalid response type")
		}
	})
}

// TestConvertToRole_RetiredPermissions guards the drift column across the
// REST/gRPC map path: a converter that decodes permissions but not
// retired_permissions silently drops the only signal that a tenant's built-in
// roles disagree with the server build.
func TestConvertToRole_RetiredPermissions(t *testing.T) {
	t.Run("decodes retired_permissions from REST map", func(t *testing.T) {
		role, ok := convertToRole(map[string]interface{}{
			"id":                  "role-1",
			"tenant_id":           "tenant-123",
			"name":                "admin",
			"permissions":         []interface{}{"kv:read", "legacy:read"},
			"retired_permissions": []interface{}{"legacy:read"},
		})
		if !ok {
			t.Fatal("expected conversion to succeed")
		}
		if len(role.Permissions) != 2 {
			t.Errorf("expected 2 permissions, got %v", role.Permissions)
		}
		if len(role.RetiredPermissions) != 1 || role.RetiredPermissions[0] != "legacy:read" {
			t.Errorf("expected retired permissions [legacy:read], got %v", role.RetiredPermissions)
		}
	})

	t.Run("decodes retired_permissions from nested role map", func(t *testing.T) {
		role, ok := convertToRole(map[string]interface{}{
			"role": map[string]interface{}{
				"id":                  "role-1",
				"name":                "operator",
				"retired_permissions": []string{"legacy:write"},
			},
		})
		if !ok {
			t.Fatal("expected conversion to succeed")
		}
		if len(role.RetiredPermissions) != 1 || role.RetiredPermissions[0] != "legacy:write" {
			t.Errorf("expected retired permissions [legacy:write], got %v", role.RetiredPermissions)
		}
	})

	t.Run("absent retired_permissions leaves the field empty", func(t *testing.T) {
		role, ok := convertToRole(map[string]interface{}{
			"id":          "role-1",
			"name":        "admin",
			"permissions": []interface{}{"kv:read"},
		})
		if !ok {
			t.Fatal("expected conversion to succeed")
		}
		if len(role.RetiredPermissions) != 0 {
			t.Errorf("expected no retired permissions, got %v", role.RetiredPermissions)
		}
	})

	t.Run("wrong element types are skipped", func(t *testing.T) {
		role, ok := convertToRole(map[string]interface{}{
			"id":                  "role-1",
			"retired_permissions": []interface{}{"legacy:read", 42},
		})
		if !ok {
			t.Fatal("expected conversion to succeed")
		}
		if len(role.RetiredPermissions) != 1 || role.RetiredPermissions[0] != "legacy:read" {
			t.Errorf("expected only string entries, got %v", role.RetiredPermissions)
		}
	})
}

func TestConvertToReconcileResponse(t *testing.T) {
	t.Run("typed pointer passes through", func(t *testing.T) {
		want := &ReconcileRolesResponse{TenantID: "tenant-123", Changed: true}
		got, ok := convertToReconcileResponse(want)
		if !ok {
			t.Fatal("expected conversion to succeed")
		}
		if got != want {
			t.Error("expected the same response pointer back")
		}
	})

	t.Run("typed role slices are accepted", func(t *testing.T) {
		got, ok := convertToReconcileResponse(map[string]interface{}{
			"tenant_id": "tenant-123",
			"roles":     []RoleReconcileResult{{RoleID: "role-1", Name: "admin"}},
		})
		if !ok {
			t.Fatal("expected conversion to succeed")
		}
		if len(got.Roles) != 1 || got.Roles[0].RoleID != "role-1" {
			t.Fatalf("expected role-1, got %+v", got.Roles)
		}
		// A report that lists changed roles is changed, even when the
		// transport omitted the boolean.
		if !got.Changed {
			t.Error("expected changed to be inferred from the role list")
		}
	})

	t.Run("role pointer slices are accepted", func(t *testing.T) {
		got, ok := convertToReconcileResponse(map[string]interface{}{
			"roles": []*RoleReconcileResult{{RoleID: "role-1"}, nil},
		})
		if !ok {
			t.Fatal("expected conversion to succeed")
		}
		if len(got.Roles) != 1 {
			t.Errorf("expected nil entries to be skipped, got %+v", got.Roles)
		}
	})

	t.Run("nil response is rejected", func(t *testing.T) {
		if _, ok := convertToReconcileResponse(nil); ok {
			t.Error("expected nil to be rejected")
		}
	})

	t.Run("unrelated type is rejected", func(t *testing.T) {
		if _, ok := convertToReconcileResponse(42); ok {
			t.Error("expected an unrelated type to be rejected")
		}
	})
}

func TestConvertToReconcileResult(t *testing.T) {
	t.Run("decodes a REST map entry", func(t *testing.T) {
		item, ok := convertToReconcileResult(map[string]interface{}{
			"role_id": "role-1",
			"name":    "admin",
			"created": true,
			"added":   []string{"kv:read"},
			"removed": []interface{}{"legacy:read"},
		})
		if !ok {
			t.Fatal("expected conversion to succeed")
		}
		if item.RoleID != "role-1" || item.Name != "admin" || !item.Created {
			t.Errorf("unexpected result: %+v", item)
		}
		if len(item.Added) != 1 || item.Added[0] != "kv:read" {
			t.Errorf("expected added [kv:read], got %v", item.Added)
		}
		if len(item.Removed) != 1 || item.Removed[0] != "legacy:read" {
			t.Errorf("expected removed [legacy:read], got %v", item.Removed)
		}
	})

	t.Run("value and pointer entries pass through", func(t *testing.T) {
		item, ok := convertToReconcileResult(RoleReconcileResult{RoleID: "role-1"})
		if !ok || item.RoleID != "role-1" {
			t.Fatalf("expected value entry to convert, got %+v ok=%v", item, ok)
		}
		item, ok = convertToReconcileResult(&RoleReconcileResult{RoleID: "role-2"})
		if !ok || item.RoleID != "role-2" {
			t.Fatalf("expected pointer entry to convert, got %+v ok=%v", item, ok)
		}
	})

	t.Run("nil and unrelated types are rejected", func(t *testing.T) {
		if _, ok := convertToReconcileResult(nil); ok {
			t.Error("expected nil to be rejected")
		}
		if _, ok := convertToReconcileResult("role-1"); ok {
			t.Error("expected an unrelated type to be rejected")
		}
	})
}
