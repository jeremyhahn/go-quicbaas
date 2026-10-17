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

package rbactest

import (
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

func TestRoleRBAC_ListPermissionsFollowsRoles(t *testing.T) {
	t.Parallel()
	r := New(map[string][]string{"reader": {"x:read"}, "writer": {"x:write"}})

	got, err := r.RBACClient().ListPermissions(context.Background(), &qrdbtransport.Identity{Roles: []string{"reader"}})
	if err != nil {
		t.Fatalf("ListPermissions: %v", err)
	}
	if len(got) != 1 || got[0].Name != "x:read" {
		t.Fatalf("permissions = %+v, want only x:read", got)
	}
	none, err := r.ListPermissions(context.Background(), &qrdbtransport.Identity{Roles: []string{"unknown"}})
	if err != nil || len(none) != 0 {
		t.Fatalf("unknown role: permissions = %+v, err = %v, want none", none, err)
	}
}

func TestRoleRBAC_Authorize(t *testing.T) {
	t.Parallel()
	r := New(map[string][]string{"reader": {"x:read"}})
	identity := &qrdbtransport.Identity{Subject: "alice", Roles: []string{"reader"}}

	if err := r.Authorize(context.Background(), identity, "x", "read"); err != nil {
		t.Fatalf("Authorize x:read: %v", err)
	}
	var denied *NotAuthorizedError
	if err := r.Authorize(context.Background(), identity, "x", "write"); !errors.As(err, &denied) || denied.Permission != "x:write" {
		t.Fatalf("Authorize x:write: err = %v, want *NotAuthorizedError for x:write", err)
	}
}

func TestCallerContext_BindsIdentity(t *testing.T) {
	t.Parallel()
	ctx := CallerContext(context.Background(), "alice", "tenant-a", "reader")

	identity := qrdbsdk.IdentityFromContext(ctx)
	if identity == nil || identity.Subject != "alice" || identity.TenantID != "tenant-a" || len(identity.Roles) != 1 {
		t.Fatalf("identity = %+v, want alice in tenant-a with one role", identity)
	}
	if qrdbsdk.IdentityFromContext(context.Background()) != nil {
		t.Fatal("a bare context carries an identity")
	}
}
