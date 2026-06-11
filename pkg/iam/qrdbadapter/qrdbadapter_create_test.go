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

package qrdbadapter

import (
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// newStore builds a real IdentityStore backed by in-memory storage. The
// adapter wraps the concrete *iam.IdentityStore, so we exercise the
// happy/error paths end-to-end through the real store rather than a
// hand-rolled fake. Tenants is intentionally nil: CreateRole/CreateUser
// guard the quota check with `if s.tenants != nil`, so nil disables the
// quota path without a tenant manager stub.
func newStore() *iam.IdentityStore {
	mem := iam.NewMemoryStorage()
	return iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  mem,
		GroupStorage: mem,
		RoleStorage:  mem,
	})
}

// TestRoleAdapter_CreateRole_HappyPath proves the adapter forwards the
// request to the store, converts the strongly-typed qrdb permissions to
// quicbaas's string slice, and returns a qrdbsdk.Role carrying the
// request identifiers with System=true.
func TestRoleAdapter_CreateRole_HappyPath(t *testing.T) {
	a := WrapRole(newStore())
	req := &qrdbsdk.CreateRoleRequest{
		ID:          "role-admin",
		Name:        "admin",
		TenantID:    "system",
		Permissions: []qrdbsdk.Permission{qrdbsdk.Permission("read:users"), qrdbsdk.Permission("write:roles")},
	}
	got, err := a.CreateRole(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if got.ID != "role-admin" || got.Name != "admin" || got.TenantID != "system" {
		t.Fatalf("returned Role = %+v, want ID/Name/Tenant from request", got)
	}
	if !got.System {
		t.Fatal("bootstrap role must be marked System=true")
	}
	if len(got.Permissions) != 2 {
		t.Fatalf("Permissions = %v, want 2 entries round-tripped", got.Permissions)
	}
}

// TestRoleAdapter_CreateRole_NilRequest proves a nil request is rejected
// before touching the store.
func TestRoleAdapter_CreateRole_NilRequest(t *testing.T) {
	a := WrapRole(newStore())
	_, err := a.CreateRole(context.Background(), nil)
	if err == nil {
		t.Fatal("CreateRole(nil req) = nil err, want non-nil")
	}
}

// TestRoleAdapter_CreateRole_ValidationErrorPassthrough proves a
// non-duplicate store error (here: empty role name failing the
// IdentityStore validator) is returned verbatim, not masked as a
// RoleNameExistsError.
func TestRoleAdapter_CreateRole_ValidationErrorPassthrough(t *testing.T) {
	a := WrapRole(newStore())
	_, err := a.CreateRole(context.Background(), &qrdbsdk.CreateRoleRequest{
		ID:       "role-x",
		Name:     "", // invalid: IdentityStore rejects empty name
		TenantID: "system",
	})
	if err == nil {
		t.Fatal("CreateRole(empty name) = nil err, want validation error")
	}
	var exists *qrdbsdk.RoleNameExistsError
	if errors.As(err, &exists) {
		t.Fatalf("validation error misclassified as RoleNameExistsError: %v", err)
	}
}

// TestUserAdapter_CreateUser_HappyPath proves the adapter synthesizes the
// stable email, forwards roles, and returns a qrdbsdk.User echoing the
// stored username/tenant/roles with Enabled=true.
func TestUserAdapter_CreateUser_HappyPath(t *testing.T) {
	a := WrapUser(newStore())
	req := &qrdbsdk.CreateUserRequest{
		Username: "admin",
		TenantID: "system",
		RoleIDs:  []string{"role-admin"},
	}
	got, err := a.CreateUser(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if got.Username != "admin" || got.TenantID != "system" {
		t.Fatalf("returned User = %+v, want admin/system", got)
	}
	if !got.Enabled {
		t.Fatal("bootstrap user must be Enabled=true")
	}
	if len(got.RoleIDs) != 1 || got.RoleIDs[0] != "role-admin" {
		t.Fatalf("RoleIDs = %v, want [role-admin]", got.RoleIDs)
	}
}

// TestUserAdapter_CreateUser_EmptyTenantDefaultsToSystem proves an empty
// TenantID is defaulted to the reserved system tenant before the user is
// created (the email is synthesized against the system tenant).
func TestUserAdapter_CreateUser_EmptyTenantDefaultsToSystem(t *testing.T) {
	a := WrapUser(newStore())
	got, err := a.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{Username: "admin"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if got.TenantID != qrdbsdk.SystemTenantID {
		t.Fatalf("TenantID = %q, want %q (empty must default to system)", got.TenantID, qrdbsdk.SystemTenantID)
	}
}

// TestUserAdapter_CreateUser_DuplicateEmailMapped proves the second
// create of the same admin (which collides on the synthesized email)
// surfaces as qrdbsdk.UsernameExistsError so qrdbfirstboot's idempotent
// re-run path triggers. This exercises isEmailDuplicate end-to-end.
func TestUserAdapter_CreateUser_DuplicateEmailMapped(t *testing.T) {
	a := WrapUser(newStore())
	req := &qrdbsdk.CreateUserRequest{Username: "admin", TenantID: "system"}
	if _, err := a.CreateUser(context.Background(), req); err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}
	_, err := a.CreateUser(context.Background(), req)
	var exists *qrdbsdk.UsernameExistsError
	if !errors.As(err, &exists) {
		t.Fatalf("duplicate CreateUser err = %T (%v), want *UsernameExistsError", err, err)
	}
	if exists.Username != "admin" || exists.TenantID != "system" {
		t.Fatalf("UsernameExistsError = %+v, want admin/system", exists)
	}
}

// TestUserAdapter_CreateUser_ValidationErrorPassthrough proves a
// non-duplicate store error (here: an invalid username failing the
// IdentityStore validator) is returned verbatim, not masked as a
// UsernameExistsError.
func TestUserAdapter_CreateUser_ValidationErrorPassthrough(t *testing.T) {
	a := WrapUser(newStore())
	_, err := a.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{
		Username: "ab", // too short: IdentityStore requires 3-64 chars
		TenantID: "system",
	})
	if err == nil {
		t.Fatal("CreateUser(short username) = nil err, want validation error")
	}
	var exists *qrdbsdk.UsernameExistsError
	if errors.As(err, &exists) {
		t.Fatalf("validation error misclassified as UsernameExistsError: %v", err)
	}
}

// TestIsEmailDuplicate covers the helper's true/false/nil branches.
func TestIsEmailDuplicate(t *testing.T) {
	if !isEmailDuplicate(iam.ErrEmailAlreadyExists) {
		t.Fatal("isEmailDuplicate(ErrEmailAlreadyExists) = false, want true")
	}
	if isEmailDuplicate(errors.New("other")) {
		t.Fatal("isEmailDuplicate(other) = true, want false")
	}
	if isEmailDuplicate(nil) {
		t.Fatal("isEmailDuplicate(nil) = true, want false")
	}
}

// TestRoleAdapter_CreateRole_NilAdapter proves a nil *RoleAdapter
// receiver is handled gracefully rather than panicking.
func TestRoleAdapter_CreateRole_NilAdapter(t *testing.T) {
	var a *RoleAdapter
	_, err := a.CreateRole(context.Background(), &qrdbsdk.CreateRoleRequest{Name: "x"})
	if err == nil {
		t.Fatal("CreateRole(nil adapter) = nil err, want non-nil")
	}
}

// TestUserAdapter_CreateUser_NilAdapter proves a nil *UserAdapter
// receiver is handled gracefully rather than panicking.
func TestUserAdapter_CreateUser_NilAdapter(t *testing.T) {
	var a *UserAdapter
	_, err := a.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{Username: "admin"})
	if err == nil {
		t.Fatal("CreateUser(nil adapter) = nil err, want non-nil")
	}
}

// TestTenantAdapter_CreateTenant_NilReceiverAndManager proves the nil
// guards on the tenant adapter return an error instead of panicking.
func TestTenantAdapter_CreateTenant_NilReceiverAndManager(t *testing.T) {
	var nilAdapter *TenantAdapter
	if _, err := nilAdapter.CreateTenant(context.Background(), "system", "System", ""); err == nil {
		t.Fatal("CreateTenant(nil adapter) = nil err, want non-nil")
	}
	emptyAdapter := &TenantAdapter{}
	if _, err := emptyAdapter.CreateTenant(context.Background(), "system", "System", ""); err == nil {
		t.Fatal("CreateTenant(nil manager) = nil err, want non-nil")
	}
}

// TestConvertPermissions_Empty proves an empty/nil permission slice
// converts to nil (no allocation), matching the qrdb contract.
func TestConvertPermissions_Empty(t *testing.T) {
	if got := convertPermissions(nil); got != nil {
		t.Fatalf("convertPermissions(nil) = %v, want nil", got)
	}
	if got := convertPermissions([]qrdbsdk.Permission{}); got != nil {
		t.Fatalf("convertPermissions(empty) = %v, want nil", got)
	}
}
