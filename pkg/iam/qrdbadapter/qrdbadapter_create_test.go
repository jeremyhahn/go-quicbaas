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

	qrdbfirstboot "github.com/jeremyhahn/go-qrdb/pkg/firstboot"
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
		UserStorage: mem,
		RoleStorage: mem,
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
	store := newStore()
	// Register the role first, mirroring first-boot ordering (built-in roles
	// are created before the admin user). IdentityStore.CreateUser validates
	// role references before writing the user record.
	if _, err := store.CreateRole(context.Background(), "system", iam.CreateRoleOptions{
		ID: "role-admin", Name: "admin",
	}); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	a := WrapUser(store)
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
	if got.Lifecycle != qrdbsdk.LifecycleActive {
		t.Fatalf("bootstrap user Lifecycle = %q, want active", got.Lifecycle)
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
// re-run path triggers. This exercises isAlreadyExists end-to-end.
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

// TestIsAlreadyExistsCoversTheDuplicateEmailAnswer covers the helper's
// true/false/nil branches, and pins the reason CreateUser needs only this one
// predicate: iam.ErrEmailAlreadyExists -- the duplicate-synthetic-email answer
// -- carries the ErrAlreadyExists CODE, so the code check already recognises
// it. CreateUser used to call a second, narrower-reading predicate alongside
// this one; it was errors.Is against a QBError, which compares codes, so it
// matched exactly this set and could never add a case (go-quicbaas#434).
func TestIsAlreadyExistsCoversTheDuplicateEmailAnswer(t *testing.T) {
	if !isAlreadyExists(iam.ErrEmailAlreadyExists) {
		t.Fatal("isAlreadyExists(ErrEmailAlreadyExists) = false, want true")
	}
	if !isAlreadyExists(iam.ErrUserAlreadyExists) {
		t.Fatal("isAlreadyExists(ErrUserAlreadyExists) = false, want true")
	}
	if isAlreadyExists(errors.New("other")) {
		t.Fatal("isAlreadyExists(other) = true, want false")
	}
	if isAlreadyExists(nil) {
		t.Fatal("isAlreadyExists(nil) = true, want false")
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

// TestRoleAdapter_GetRoleByName_ResolvesPersistedID proves the adapter answers
// first-boot's name lookup with the identifier the role is actually stored
// under, marked System for a built-in name — the binding a grant must use on
// a re-run against an already-seeded store.
func TestRoleAdapter_GetRoleByName_ResolvesPersistedID(t *testing.T) {
	a := WrapRole(newStore())
	ctx := context.Background()
	if _, err := a.CreateRole(ctx, &qrdbsdk.CreateRoleRequest{
		ID: "legacy-admin-id", Name: "admin", TenantID: "system",
		Permissions: []qrdbsdk.Permission{qrdbsdk.Permission("read:users")},
	}); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}

	got, err := a.GetRoleByName(ctx, "system", "admin")
	if err != nil {
		t.Fatalf("GetRoleByName: %v", err)
	}
	if got.ID != "legacy-admin-id" || got.TenantID != "system" || got.Name != "admin" {
		t.Fatalf("GetRoleByName = %+v, want the persisted identifier", got)
	}
	if !got.System {
		t.Fatal("a built-in role name must resolve as System")
	}
	if len(got.Permissions) != 1 || string(got.Permissions[0]) != "read:users" {
		t.Fatalf("Permissions = %v, want the stored permission round-tripped", got.Permissions)
	}
}

// TestRoleAdapter_GetRoleByName_NilStore proves a nil store is refused before
// any lookup.
func TestRoleAdapter_GetRoleByName_NilStore(t *testing.T) {
	a := &RoleAdapter{}
	if _, err := a.GetRoleByName(context.Background(), "system", "admin"); err == nil {
		t.Fatal("expected NilDependencyError")
	}
}

// TestUserAdapter_GetUserByUsername_ReadsBackTheAdmin proves the adapter is the
// first-boot UserResolver go-qrdb needs to verify an existing admin on re-run:
// it returns the record CreateUser wrote, with its role grant and lifecycle,
// and an empty tenant means the system tenant.
func TestUserAdapter_GetUserByUsername_ReadsBackTheAdmin(t *testing.T) {
	store := newStore()
	if _, err := store.CreateRole(context.Background(), "system", iam.CreateRoleOptions{ID: "role-admin", Name: "admin"}); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	a := WrapUser(store)
	if _, err := a.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{Username: "admin", TenantID: "system", RoleIDs: []string{"role-admin"}}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	var _ qrdbfirstboot.UserResolver = a

	got, err := a.GetUserByUsername(context.Background(), "", "admin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if got.Username != "admin" || got.TenantID != "system" || len(got.RoleIDs) != 1 || got.RoleIDs[0] != "role-admin" {
		t.Fatalf("read back %+v", got)
	}
	if !got.Lifecycle.Valid() || got.Lifecycle.Terminal() {
		t.Fatalf("Lifecycle %q must be live for first boot to accept the admin", got.Lifecycle)
	}
}

func TestUserAdapter_GetUserByUsername_Errors(t *testing.T) {
	if _, err := WrapUser(newStore()).GetUserByUsername(context.Background(), "system", "nobody"); err == nil {
		t.Fatal("an unknown user must be an error, not an empty record")
	}
	var nilAdapter *UserAdapter
	var dep *NilDependencyError
	if _, err := nilAdapter.GetUserByUsername(context.Background(), "system", "admin"); !errors.As(err, &dep) {
		t.Fatalf("nil adapter = %v, want NilDependencyError", err)
	}
}

// TestUserAdapter_CreateUser_PendingAdminSatisfiesDeferredProvisioning pins
// the record qrdb's deferred tenant provisioning validates before it hands
// the admin back as an invitation target: a persisted id, the exact tenant,
// username and Admin role, lifecycle pending, and no cert binding. An adapter
// that ignored Pending or dropped the id made every deferred run fail.
func TestUserAdapter_CreateUser_PendingAdminSatisfiesDeferredProvisioning(t *testing.T) {
	ctx := context.Background()
	store := newStore()
	if _, err := store.CreateRole(ctx, "acme", iam.CreateRoleOptions{ID: "role-admin", Name: "Admin"}); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	a := WrapUser(store)
	got, err := a.CreateUser(ctx, &qrdbsdk.CreateUserRequest{
		Username: "owner",
		TenantID: "acme",
		RoleIDs:  []string{"role-admin"},
		Pending:  true,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if got.ID == 0 {
		t.Fatal("a created user must carry its persisted id")
	}
	if got.Lifecycle != qrdbsdk.LifecyclePending {
		t.Fatalf("Lifecycle = %q, want pending", got.Lifecycle)
	}
	if got.TenantID != "acme" || got.Username != "owner" || len(got.RoleIDs) != 1 || got.RoleIDs[0] != "role-admin" {
		t.Fatalf("created %+v, want owner in acme holding role-admin", got)
	}
	if len(got.CertBindings) != 0 {
		t.Fatalf("CertBindings = %v, want none", got.CertBindings)
	}
	stored, err := store.GetUserByUsername(ctx, "acme", "owner")
	if err != nil {
		t.Fatalf("store read: %v", err)
	}
	if stored.ID != iam.FormatUserID(got.ID) {
		t.Fatalf("returned id %d does not name stored user %q", got.ID, stored.ID)
	}
}

// TestUserAdapter_GetUserByUsername_ProjectsIDAndBindings pins the lookup a
// deferred re-run resolves the still-pending admin through, and the cert
// bindings qrdb checks to refuse an admin that is already enrolled.
func TestUserAdapter_GetUserByUsername_ProjectsIDAndBindings(t *testing.T) {
	ctx := context.Background()
	store := newStore()
	if _, err := store.CreateRole(ctx, "system", iam.CreateRoleOptions{ID: "role-admin", Name: "admin"}); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	a := WrapUser(store)
	created, err := a.CreateUser(ctx, &qrdbsdk.CreateUserRequest{Username: "admin", TenantID: "system", RoleIDs: []string{"role-admin"}})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := a.BindAdminCert(ctx, "system", created.ID, "ab12"); err != nil {
		t.Fatalf("BindAdminCert: %v", err)
	}

	got, err := a.GetUserByUsername(ctx, "system", "admin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("ID = %d, want %d", got.ID, created.ID)
	}
	if len(got.CertBindings) == 0 {
		t.Fatal("a bound admin must project its cert binding")
	}
}

func TestStoredUser_RefusesAMissingOrNonCanonicalID(t *testing.T) {
	for _, u := range []*iam.User{nil, {ID: ""}, {ID: "007"}, {ID: "not-a-number"}} {
		if _, err := storedUser(u); !errors.Is(err, ErrInvalidUserID) {
			t.Fatalf("storedUser(%+v) = %v, want ErrInvalidUserID", u, err)
		}
	}
}
