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

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// fakeTenantManager records CreateTenant calls and can be primed to
// return iam.ErrTenantAlreadyExists.
type fakeTenantManager struct {
	created      []iam.CreateTenantOptions
	returnExists bool
	returnErr    error
}

func (f *fakeTenantManager) CreateTenant(_ context.Context, opts iam.CreateTenantOptions) (*iam.Tenant, error) {
	f.created = append(f.created, opts)
	if f.returnExists {
		return nil, iam.ErrTenantAlreadyExists
	}
	if f.returnErr != nil {
		return nil, f.returnErr
	}
	return &iam.Tenant{ID: opts.ID, Name: opts.Name}, nil
}

func (f *fakeTenantManager) GetTenant(_ context.Context, _ string) (*iam.Tenant, error) {
	return nil, errors.New("not implemented in fake")
}
func (f *fakeTenantManager) UpdateTenant(_ context.Context, _ string, _ iam.UpdateTenantOptions) (*iam.Tenant, error) {
	return nil, errors.New("not implemented in fake")
}
func (f *fakeTenantManager) DeleteTenant(_ context.Context, _ string) error {
	return errors.New("not implemented in fake")
}
func (f *fakeTenantManager) ListTenants(_ context.Context, _ iam.ListOptions) ([]*iam.Tenant, error) {
	return nil, errors.New("not implemented in fake")
}
func (f *fakeTenantManager) CheckQuota(_ context.Context, _, _ string) error { return nil }
func (f *fakeTenantManager) UpdateEngineQuotas(_ context.Context, _ string, _ qrdbsdk.Quotas) error {
	return nil
}
func (f *fakeTenantManager) UpdateEngineEntitlements(_ context.Context, _ string, _ qrdbsdk.Entitlements) error {
	return nil
}

func TestTenantAdapter_CreateTenant_HappyPath(t *testing.T) {
	mgr := &fakeTenantManager{}
	a := WrapTenant(mgr)
	got, err := a.CreateTenant(context.Background(), "system", "System", qrdbsdk.TenantIsolationBarrier)
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	if got == nil || got.ID != "system" {
		t.Fatalf("returned TenantInfo = %+v, want ID=system", got)
	}
	if len(mgr.created) != 1 || mgr.created[0].ID != "system" {
		t.Fatalf("manager recorded %+v, want one CreateTenant(system)", mgr.created)
	}
}

func TestTenantAdapter_CreateTenant_AlreadyExists(t *testing.T) {
	mgr := &fakeTenantManager{returnExists: true}
	a := WrapTenant(mgr)
	_, err := a.CreateTenant(context.Background(), "system", "System", qrdbsdk.TenantIsolationBarrier)
	if !errors.Is(err, qrdbsdk.ErrTenantExists) {
		t.Fatalf("CreateTenant duplicate err = %v, want qrdbsdk.ErrTenantExists", err)
	}
}

func TestTenantAdapter_CreateTenant_OtherError(t *testing.T) {
	other := errors.New("backend boom")
	mgr := &fakeTenantManager{returnErr: other}
	a := WrapTenant(mgr)
	_, err := a.CreateTenant(context.Background(), "system", "System", qrdbsdk.TenantIsolationBarrier)
	if !errors.Is(err, other) {
		t.Fatalf("CreateTenant other-error = %v, want wrapping %v", err, other)
	}
}

// TestTenantAdapter_CreateTenant_BarrierIsolationThreaded proves that a
// barrier-isolation request is not discarded by the adapter but is forwarded
// to the TenantManager via CreateTenantOptions.IsolationType, so qrdb's
// engine-side CreateTenant creates a barrier-isolated tenant (own DEK +
// "{tenantID}/" namespace) rather than silently degrading to prefix.
func TestTenantAdapter_CreateTenant_BarrierIsolationThreaded(t *testing.T) {
	mgr := &fakeTenantManager{}
	a := WrapTenant(mgr)
	if _, err := a.CreateTenant(context.Background(), "acme", "Acme", qrdbsdk.TenantIsolationBarrier); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	if len(mgr.created) != 1 {
		t.Fatalf("recorded %d CreateTenant calls, want 1", len(mgr.created))
	}
	if got := mgr.created[0].IsolationType; got != qrdbsdk.TenantIsolationBarrier {
		t.Fatalf("IsolationType = %q, want %q (barrier request must reach the manager)", got, qrdbsdk.TenantIsolationBarrier)
	}
}

// TestTenantAdapter_CreateTenant_EmptyIsolationStaysEmpty proves an
// unspecified isolation type is forwarded as empty, letting qrdb apply its
// default (prefix) rather than the adapter forcing a value.
func TestTenantAdapter_CreateTenant_EmptyIsolationStaysEmpty(t *testing.T) {
	mgr := &fakeTenantManager{}
	a := WrapTenant(mgr)
	if _, err := a.CreateTenant(context.Background(), "acme", "Acme", ""); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	if got := mgr.created[0].IsolationType; got != "" {
		t.Fatalf("IsolationType = %q, want empty (qrdb defaults empty to prefix)", got)
	}
}

// TestTenantAdapter_CreateTenant_SystemForcedToPrefix proves the reserved
// system tenant never requests barrier isolation, even when the caller asks
// for it, because qrdb rejects a barrier system tenant with
// ErrSystemTenantMustBePrefix.
func TestTenantAdapter_CreateTenant_SystemForcedToPrefix(t *testing.T) {
	mgr := &fakeTenantManager{}
	a := WrapTenant(mgr)
	if _, err := a.CreateTenant(context.Background(), qrdbsdk.SystemTenantID, "System", qrdbsdk.TenantIsolationBarrier); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	if got := mgr.created[0].IsolationType; got != "" {
		t.Fatalf("system tenant IsolationType = %q, want empty (prefix)", got)
	}
}

func TestIsAlreadyExists_QBError(t *testing.T) {
	qb := &dberrors.QBError{Code: dberrors.ErrAlreadyExists}
	if !isAlreadyExists(qb) {
		t.Fatal("isAlreadyExists(QBError{ErrAlreadyExists}) = false, want true")
	}
}

func TestIsAlreadyExists_NotQBError(t *testing.T) {
	if isAlreadyExists(errors.New("plain")) {
		t.Fatal("isAlreadyExists(plain) = true, want false")
	}
}

func TestIsAlreadyExists_Nil(t *testing.T) {
	if isAlreadyExists(nil) {
		t.Fatal("isAlreadyExists(nil) = true, want false")
	}
}

func TestSyntheticEmail_FormatStable(t *testing.T) {
	want := "admin@system.qrdb.local"
	if got := syntheticEmail("admin", "system"); got != want {
		t.Fatalf("syntheticEmail = %q, want %q", got, want)
	}
}

// TestUserAdapter_EmailFor_ConfiguredAdminAddress (#1827): a configured
// address applies to the system tenant only; elsewhere, and when unset, the
// synthetic address stays.
func TestUserAdapter_EmailFor_ConfiguredAdminAddress(t *testing.T) {
	a := WrapUser(nil).WithAdminEmail(" ops@acme.example ")
	if got := a.EmailFor("admin", qrdbsdk.SystemTenantID); got != "ops@acme.example" {
		t.Fatalf("EmailFor(system) = %q, want the configured address", got)
	}
	if got := a.EmailFor("admin", "acme"); got != "admin@acme.qrdb.local" {
		t.Fatalf("EmailFor(acme) = %q, want the synthetic address", got)
	}
	if got := WrapUser(nil).EmailFor("admin", qrdbsdk.SystemTenantID); got != "admin@system.qrdb.local" {
		t.Fatalf("EmailFor unset = %q, want the default admin@system.qrdb.local", got)
	}
}

func TestConvertPermissions_Roundtrip(t *testing.T) {
	in := []qrdbsdk.Permission{qrdbsdk.Permission("read:users"), qrdbsdk.Permission("write:roles")}
	got := convertPermissions(in)
	if len(got) != len(in) {
		t.Fatalf("convertPermissions len = %d, want %d", len(got), len(in))
	}
	if got[0] != "read:users" || got[1] != "write:roles" {
		t.Fatalf("convertPermissions = %v, want [read:users write:roles]", got)
	}
}

// assertNilDependency asserts err is a *NilDependencyError naming want.
func assertNilDependency(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected NilDependencyError(%q), got nil", want)
	}
	var nde *NilDependencyError
	if !errors.As(err, &nde) {
		t.Fatalf("expected *NilDependencyError, got %T: %v", err, err)
	}
	if nde.Name != want {
		t.Fatalf("NilDependencyError.Name = %q, want %q", nde.Name, want)
	}
}

func TestNilDependencyError_Error(t *testing.T) {
	err := &NilDependencyError{Name: "TenantManager"}
	if got := err.Error(); got != "qrdbadapter: nil TenantManager" {
		t.Fatalf("Error() = %q, want %q", got, "qrdbadapter: nil TenantManager")
	}
}

func TestTenantAdapter_CreateTenant_NilManagerErrors(t *testing.T) {
	a := WrapTenant(nil)
	_, err := a.CreateTenant(context.Background(), "t1", "Tenant 1", "")
	assertNilDependency(t, err, "TenantManager")
}

func TestRoleAdapter_CreateRole_NilStoreErrors(t *testing.T) {
	a := WrapRole(nil)
	_, err := a.CreateRole(context.Background(), &qrdbsdk.CreateRoleRequest{Name: "admin"})
	assertNilDependency(t, err, "IdentityStore")
}

func TestRoleAdapter_CreateRole_NilRequestErrors(t *testing.T) {
	a := WrapRole(&iam.IdentityStore{})
	_, err := a.CreateRole(context.Background(), nil)
	assertNilDependency(t, err, "CreateRoleRequest")
}

func TestUserAdapter_CreateUser_NilStoreErrors(t *testing.T) {
	a := WrapUser(nil)
	_, err := a.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{Username: "admin"})
	assertNilDependency(t, err, "IdentityStore")
}

func TestUserAdapter_CreateUser_NilRequestErrors(t *testing.T) {
	a := WrapUser(&iam.IdentityStore{})
	_, err := a.CreateUser(context.Background(), nil)
	assertNilDependency(t, err, "CreateUserRequest")
}
