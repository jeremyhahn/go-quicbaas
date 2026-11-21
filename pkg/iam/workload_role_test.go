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
	"errors"
	"testing"
)

const workloadTestPrincipal = "spiffe://example.org/tenant/t1/svc/web"

func newWorkloadBinderStore(t *testing.T) (*WorkloadRoleBinder, *mockUserStorage) {
	t.Helper()
	users := newMockUserStorage()
	roles := newMockRoleStorage()
	for _, r := range []*Role{
		{ID: "role-viewer", TenantID: "t1", Name: "viewer"},
		{ID: "role-operator", TenantID: "t1", Name: "operator"},
	} {
		if err := roles.CreateRole(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	store := NewIdentityStore(IdentityStoreOptions{UserStorage: users, RoleStorage: roles})
	return &WorkloadRoleBinder{Store: store}, users
}

func TestWorkloadRoleBinder_AssignCreatesMachinePrincipal(t *testing.T) {
	b, users := newWorkloadBinderStore(t)
	ctx := context.Background()
	if err := b.AssignRole(ctx, "t1", workloadTestPrincipal, "viewer"); err != nil {
		t.Fatal(err)
	}
	u, err := users.GetUserByUsername(ctx, "t1", workloadTestPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Roles) != 1 || u.Roles[0] != "role-viewer" {
		t.Fatalf("roles = %v, want [role-viewer]", u.Roles)
	}
	if len(u.AuthMethods) != 1 || u.AuthMethods[0] != AuthMethodMTLS {
		t.Fatalf("auth methods = %v, want [mtls]", u.AuthMethods)
	}
	if u.Email != "" || u.Lifecycle != LifecycleActive {
		t.Fatalf("principal = email %q lifecycle %q", u.Email, u.Lifecycle)
	}
	// A second grant of the same role is a no-op; another role is added.
	if err := b.AssignRole(ctx, "t1", workloadTestPrincipal, "viewer"); err != nil {
		t.Fatal(err)
	}
	if err := b.AssignRole(ctx, "t1", workloadTestPrincipal, "operator"); err != nil {
		t.Fatal(err)
	}
	u, _ = users.GetUserByUsername(ctx, "t1", workloadTestPrincipal)
	if len(u.Roles) != 2 {
		t.Fatalf("roles = %v, want viewer and operator", u.Roles)
	}
}

func TestWorkloadRoleBinder_Revoke(t *testing.T) {
	b, users := newWorkloadBinderStore(t)
	ctx := context.Background()
	// Revoking from a principal that never existed is already done.
	if err := b.RevokeRole(ctx, "t1", workloadTestPrincipal, "viewer"); err != nil {
		t.Fatal(err)
	}
	if err := b.AssignRole(ctx, "t1", workloadTestPrincipal, "viewer"); err != nil {
		t.Fatal(err)
	}
	if err := b.RevokeRole(ctx, "t1", workloadTestPrincipal, "viewer"); err != nil {
		t.Fatal(err)
	}
	u, err := users.GetUserByUsername(ctx, "t1", workloadTestPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Roles) != 0 {
		t.Fatalf("roles = %v, want none", u.Roles)
	}
	if err := b.RevokeRole(ctx, "t1", workloadTestPrincipal, "viewer"); err != nil {
		t.Fatalf("second revoke: %v", err)
	}
}

func TestWorkloadRoleBinder_Refusals(t *testing.T) {
	b, users := newWorkloadBinderStore(t)
	ctx := context.Background()

	var pe *WorkloadPrincipalError
	if err := b.AssignRole(ctx, "t1", "alice@example.org", "viewer"); !errors.As(err, &pe) {
		t.Fatalf("non-SPIFFE principal: err = %v, want WorkloadPrincipalError", err)
	}
	if err := b.AssignRole(ctx, "t1", workloadTestPrincipal, "no-such-role"); err == nil {
		t.Fatal("unknown role: want an error")
	}
	if err := b.AssignRole(ctx, "t2", workloadTestPrincipal, "viewer"); err == nil {
		t.Fatal("role of another tenant: want an error")
	}

	// A human account that shares the name is never widened.
	human := &User{TenantID: "t1", Username: workloadTestPrincipal, AuthMethods: []string{"webauthn"}, Lifecycle: LifecycleActive}
	if err := users.CreateUser(ctx, human); err != nil {
		t.Fatal(err)
	}
	if err := b.AssignRole(ctx, "t1", workloadTestPrincipal, "viewer"); err == nil {
		t.Fatal("human account: want a refusal")
	}
	u, _ := users.GetUserByUsername(ctx, "t1", workloadTestPrincipal)
	if len(u.Roles) != 0 {
		t.Fatalf("human account roles = %v, want none", u.Roles)
	}
}
