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

	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// TestIAMRoleReconcileRoundTrip drives the SDK RoleClient over the REST
// transport against the server's documented reconcile response. It covers the
// whole chain — request path, HTTP method, and JSON decoding — because a
// converter that reaches the wire but drops fields looks like a clean success
// to the caller.
func TestIAMRoleReconcileRoundTrip(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tenant_id": "t1",
			"changed": true,
			"roles": [
				{"role_id": "r-admin", "name": "admin", "added": ["sdlc:write"], "removed": ["legacy:read"]},
				{"role_id": "r-operator", "name": "operator", "created": true, "added": ["kv:read"]}
			]
		}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	client := iam.NewRoleClient(tr)

	resp, err := client.Reconcile(context.Background(), "t1")
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/api/v1/tenants/t1/roles/reconcile" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if resp.TenantID != "t1" || !resp.Changed {
		t.Errorf("unexpected response: %+v", resp)
	}
	if len(resp.Roles) != 2 {
		t.Fatalf("expected 2 reconciled roles, got %d", len(resp.Roles))
	}
	if resp.Roles[0].RoleID != "r-admin" || len(resp.Roles[0].Added) != 1 || len(resp.Roles[0].Removed) != 1 {
		t.Errorf("unexpected admin result: %+v", resp.Roles[0])
	}
	if !resp.Roles[1].Created {
		t.Errorf("expected the operator role to report created: %+v", resp.Roles[1])
	}
}

// TestIAMRoleReconcileRoundTrip_ServerError proves a server refusal surfaces
// as an error rather than an empty, apparently-clean report.
func TestIAMRoleReconcileRoundTrip_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error": "insufficient permissions"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	client := iam.NewRoleClient(tr)

	if _, err := client.Reconcile(context.Background(), "t1"); err == nil {
		t.Fatal("expected the server refusal to surface as an error")
	}
}

// TestIAMRoleListRoundTrip_RetiredPermissions proves an ordinary listing
// carries the drift column across REST, where the response is a decoded map
// rather than a typed value.
func TestIAMRoleListRoundTrip_RetiredPermissions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"roles": [
				{"id": "r-admin", "name": "admin", "permissions": ["kv:read", "legacy:read"],
				 "retired_permissions": ["legacy:read"]}
			],
			"total": 1
		}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	client := iam.NewRoleClient(tr)

	resp, err := client.List(context.Background(), "t1", nil)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(resp.Roles) != 1 {
		t.Fatalf("expected 1 role, got %d", len(resp.Roles))
	}
	if len(resp.Roles[0].RetiredPermissions) != 1 || resp.Roles[0].RetiredPermissions[0] != "legacy:read" {
		t.Errorf("expected retired [legacy:read], got %v", resp.Roles[0].RetiredPermissions)
	}
}
