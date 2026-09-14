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

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// Built-in role reconciliation (go-quicbaas#573).
//
// qrdb owns the reconciliation ITSELF — which verbs a built-in role should
// carry, and the guarantee that a converged tenant performs zero writes. What
// this package owns, and what these tests pin, is the seam: the request
// reaches qrdb's reconcile verb rather than the refused UpdateRole path, the
// report is projected faithfully, qrdb's errors are classified, and the drift
// signal qrdb derives on read is not dropped on the way out.
// =============================================================================

// TestQRDBRoleStorage_ReconcileBuiltInRoles_ProjectsReport proves the qrdb
// reconcile response is projected onto the broker report field for field.
// A dropped Added/Removed list would leave an operator with a report that says
// a role changed but not how, which is the whole content of the log line the
// boot-time hook emits.
func TestQRDBRoleStorage_ReconcileBuiltInRoles_ProjectsReport(t *testing.T) {
	fc := &fakeRoleClient{
		recResp: &qrdbtransport.ReconcileRolesResponse{
			TenantID: "burnin",
			Roles: []qrdbtransport.RoleReconcileResult{
				{
					RoleID:  "role-admin",
					Name:    "admin",
					Added:   []string{"sdlc:bind", "sdlc:unbind"},
					Removed: []string{"sdlc:register"},
				},
				{
					RoleID:  "role-operator",
					Name:    "operator",
					Created: true,
					Added:   []string{"orchestrator:task_block"},
				},
			},
		},
	}
	s := NewQRDBRoleStorage(&fakeRoleResolver{client: fc})

	report, err := s.ReconcileBuiltInRoles(context.Background(), "burnin")
	if err != nil {
		t.Fatalf("ReconcileBuiltInRoles: %v", err)
	}
	if fc.gotRec == nil || fc.gotRec.TenantID != "burnin" {
		t.Fatalf("qrdb request = %+v, want TenantID burnin", fc.gotRec)
	}
	if report.TenantID != "burnin" {
		t.Fatalf("report TenantID = %q, want burnin", report.TenantID)
	}
	if !report.Changed() {
		t.Fatal("Changed() = false for a report carrying two changed roles")
	}
	if len(report.Roles) != 2 {
		t.Fatalf("report.Roles = %d entries, want 2", len(report.Roles))
	}

	admin := report.Roles[0]
	if admin.RoleID != "role-admin" || admin.Name != "admin" || admin.Created {
		t.Fatalf("admin result = %+v, want the existing role-admin", admin)
	}
	if len(admin.Added) != 2 || admin.Added[0] != "sdlc:bind" || admin.Added[1] != "sdlc:unbind" {
		t.Fatalf("admin.Added = %v, want the two verbs the build gained", admin.Added)
	}
	if len(admin.Removed) != 1 || admin.Removed[0] != "sdlc:register" {
		t.Fatalf("admin.Removed = %v, want the retired verb", admin.Removed)
	}

	operator := report.Roles[1]
	if !operator.Created {
		t.Fatal("operator.Created = false; a built-in absent from the tenant is reported as created")
	}
	if len(operator.Added) != 1 || operator.Added[0] != "orchestrator:task_block" {
		t.Fatalf("operator.Added = %v, want the seeded permission set", operator.Added)
	}
}

// TestQRDBRoleStorage_ReconcileBuiltInRoles_ConvergedTenantReportsNoChange
// proves an empty qrdb report projects to a report that answers Changed()
// false. The boot-time hook logs nothing on such a tenant, so a report that
// claimed a change would put one noise line per role per boot in front of
// every operator whose fleet is already correct.
func TestQRDBRoleStorage_ReconcileBuiltInRoles_ConvergedTenantReportsNoChange(t *testing.T) {
	fc := &fakeRoleClient{recResp: &qrdbtransport.ReconcileRolesResponse{TenantID: "burnin"}}
	s := NewQRDBRoleStorage(&fakeRoleResolver{client: fc})

	report, err := s.ReconcileBuiltInRoles(context.Background(), "burnin")
	if err != nil {
		t.Fatalf("ReconcileBuiltInRoles: %v", err)
	}
	if report.Changed() {
		t.Fatalf("Changed() = true for an empty report: %+v", report.Roles)
	}
	if len(report.Roles) != 0 {
		t.Fatalf("report.Roles = %v, want empty", report.Roles)
	}
}

// TestQRDBRoleStorage_ReconcileBuiltInRoles_TenantFallsBackToTheRequest proves
// a response that does not echo the tenant still yields a report naming the
// tenant that was asked for. The report is what the log line and the API
// response identify the tenant by; an empty one would name nothing.
func TestQRDBRoleStorage_ReconcileBuiltInRoles_TenantFallsBackToTheRequest(t *testing.T) {
	fc := &fakeRoleClient{recResp: &qrdbtransport.ReconcileRolesResponse{}}
	s := NewQRDBRoleStorage(&fakeRoleResolver{client: fc})

	report, err := s.ReconcileBuiltInRoles(context.Background(), "burnin")
	if err != nil {
		t.Fatalf("ReconcileBuiltInRoles: %v", err)
	}
	if report.TenantID != "burnin" {
		t.Fatalf("report TenantID = %q, want the requested tenant burnin", report.TenantID)
	}
}

// TestQRDBRoleStorage_ReconcileBuiltInRoles_NilResponseIsNotAPanic proves a
// nil response with a nil error yields an empty report rather than a nil
// dereference in the caller's Changed() check.
func TestQRDBRoleStorage_ReconcileBuiltInRoles_NilResponseIsNotAPanic(t *testing.T) {
	s := NewQRDBRoleStorage(&fakeRoleResolver{client: &fakeRoleClient{}})

	report, err := s.ReconcileBuiltInRoles(context.Background(), "burnin")
	if err != nil {
		t.Fatalf("ReconcileBuiltInRoles: %v", err)
	}
	if report == nil || report.Changed() {
		t.Fatalf("report = %+v, want an empty report for a nil response", report)
	}
}

// TestQRDBRoleStorage_ReconcileBuiltInRoles_ClassifiesQRDBErrors proves qrdb's
// role errors are translated by the same mapper the CRUD paths use, so a
// caller branches on the broker's sentinels regardless of which role
// operation raised them.
func TestQRDBRoleStorage_ReconcileBuiltInRoles_ClassifiesQRDBErrors(t *testing.T) {
	fc := &fakeRoleClient{recErr: errors.New("qrdb unreachable")}
	s := NewQRDBRoleStorage(&fakeRoleResolver{client: fc})

	_, err := s.ReconcileBuiltInRoles(context.Background(), "burnin")
	if err == nil {
		t.Fatal("ReconcileBuiltInRoles: want an error when qrdb fails")
	}
	var qb *dberrors.QBError
	if !errors.As(err, &qb) {
		t.Fatalf("error = %T (%v), want a typed *QBError", err, err)
	}
	if qb.Code != dberrors.ErrInternal {
		t.Fatalf("error code = %v, want ErrInternal for an unclassified qrdb failure", qb.Code)
	}
	if qb.Op != "iam.role.qrdb.reconcile" {
		t.Fatalf("error Op = %q, want iam.role.qrdb.reconcile", qb.Op)
	}
}

// TestQRDBRoleStorage_ReconcileBuiltInRoles_NilResolverFailsClosed proves an
// unwired storage refuses with a typed configuration error instead of
// panicking, matching the CRUD paths' fail-closed behaviour.
func TestQRDBRoleStorage_ReconcileBuiltInRoles_NilResolverFailsClosed(t *testing.T) {
	s := NewQRDBRoleStorage(nil)

	_, err := s.ReconcileBuiltInRoles(context.Background(), "burnin")
	if err == nil {
		t.Fatal("ReconcileBuiltInRoles: want an error with no resolver configured")
	}
	var qb *dberrors.QBError
	if !errors.As(err, &qb) {
		t.Fatalf("error = %T (%v), want a typed *QBError", err, err)
	}
	if qb.Code != dberrors.ErrConfiguration {
		t.Fatalf("error code = %v, want ErrConfiguration", qb.Code)
	}
}

// TestTransportRoleToIAM_CarriesRetiredPermissions is the regression guard for
// the drift column. qrdb derives RetiredPermissions on every ordinary read
// from the live permission registry; this projection is the only place it can
// be lost, and losing it means the roles listing shows a role that looks
// correct while the PDP is answering from a stale grant — the exact shape of
// the defect #573 was filed for.
func TestTransportRoleToIAM_CarriesRetiredPermissions(t *testing.T) {
	got := transportRoleToIAM(&qrdbtransport.Role{
		ID:                 "role-admin",
		Name:               "admin",
		TenantID:           "burnin",
		Permissions:        []string{"sdlc:read", "sdlc:register"},
		RetiredPermissions: []string{"sdlc:register"},
	})
	if got == nil {
		t.Fatal("transportRoleToIAM returned nil for a non-nil role")
	}
	if len(got.RetiredPermissions) != 1 || got.RetiredPermissions[0] != "sdlc:register" {
		t.Fatalf("RetiredPermissions = %v, want [sdlc:register]", got.RetiredPermissions)
	}
	if len(got.Permissions) != 2 {
		t.Fatalf("Permissions = %v, want both stored permissions", got.Permissions)
	}
}

// TestTransportRoleToIAM_CleanRoleCarriesNoDrift proves a role the build still
// agrees with reports no retired permissions, so the drift column is a signal
// rather than decoration.
func TestTransportRoleToIAM_CleanRoleCarriesNoDrift(t *testing.T) {
	got := transportRoleToIAM(&qrdbtransport.Role{
		ID:          "role-admin",
		Name:        "admin",
		TenantID:    "burnin",
		Permissions: []string{"sdlc:read"},
	})
	if len(got.RetiredPermissions) != 0 {
		t.Fatalf("RetiredPermissions = %v, want empty for a role that agrees with the build", got.RetiredPermissions)
	}
}

// nonReconcilingRoleStorage is a RoleStorage that does NOT implement
// BuiltInRoleReconciler, so the identity store's capability probe has
// something to refuse.
type nonReconcilingRoleStorage struct{ RoleStorage }

// TestIdentityStore_ReconcileBuiltInRoles_RefusesAStorageThatCannotReconcile
// proves the service layer refuses with a typed error rather than reconciling
// a tenant against a built-in set this package invented. Built-in definitions
// are qrdb's; a storage that cannot reach them has nothing correct to say.
func TestIdentityStore_ReconcileBuiltInRoles_RefusesAStorageThatCannotReconcile(t *testing.T) {
	mem := NewMemoryStorage()
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  mem,
		GroupStorage: mem,
		RoleStorage:  &nonReconcilingRoleStorage{RoleStorage: mem},
	})

	_, err := store.ReconcileBuiltInRoles(context.Background(), "burnin")
	if !errors.Is(err, ErrRoleReconcileUnsupported) {
		t.Fatalf("error = %v, want ErrRoleReconcileUnsupported", err)
	}
}

// TestIdentityStore_ReconcileBuiltInRoles_RejectsAnInvalidTenant proves the
// tenant identifier is validated before any store call, matching every other
// tenant-scoped method on the identity store.
func TestIdentityStore_ReconcileBuiltInRoles_RejectsAnInvalidTenant(t *testing.T) {
	fc := &fakeRoleClient{recResp: &qrdbtransport.ReconcileRolesResponse{}}
	store := NewIdentityStore(IdentityStoreOptions{
		RoleStorage: NewQRDBRoleStorage(&fakeRoleResolver{client: fc}),
	})

	if _, err := store.ReconcileBuiltInRoles(context.Background(), ""); err == nil {
		t.Fatal("ReconcileBuiltInRoles: want an error for an empty tenant ID")
	}
	if fc.gotRec != nil {
		t.Fatalf("qrdb was called with %+v; validation must precede the store call", fc.gotRec)
	}
}

// TestIdentityStore_ReconcileBuiltInRoles_DelegatesToTheRoleStorage proves the
// happy path reaches the qrdb reconcile verb and returns its report unchanged.
func TestIdentityStore_ReconcileBuiltInRoles_DelegatesToTheRoleStorage(t *testing.T) {
	fc := &fakeRoleClient{
		recResp: &qrdbtransport.ReconcileRolesResponse{
			TenantID: "burnin",
			Roles:    []qrdbtransport.RoleReconcileResult{{RoleID: "role-admin", Name: "admin", Added: []string{"sdlc:bind"}}},
		},
	}
	store := NewIdentityStore(IdentityStoreOptions{
		RoleStorage: NewQRDBRoleStorage(&fakeRoleResolver{client: fc}),
	})

	report, err := store.ReconcileBuiltInRoles(context.Background(), "burnin")
	if err != nil {
		t.Fatalf("ReconcileBuiltInRoles: %v", err)
	}
	if !report.Changed() || len(report.Roles) != 1 || report.Roles[0].Name != "admin" {
		t.Fatalf("report = %+v, want the admin role reported as changed", report)
	}
}

// TestIdentityStore_ReconcileBuiltInRoles_SurfacesStorageFailures proves a
// storage failure is wrapped, not swallowed: the boot-time hook logs a warning
// on it, and an empty report would instead read as "every tenant is fine".
func TestIdentityStore_ReconcileBuiltInRoles_SurfacesStorageFailures(t *testing.T) {
	fc := &fakeRoleClient{recErr: errors.New("qrdb unreachable")}
	store := NewIdentityStore(IdentityStoreOptions{
		RoleStorage: NewQRDBRoleStorage(&fakeRoleResolver{client: fc}),
	})

	if _, err := store.ReconcileBuiltInRoles(context.Background(), "burnin"); err == nil {
		t.Fatal("ReconcileBuiltInRoles: want the storage failure surfaced")
	}
}
