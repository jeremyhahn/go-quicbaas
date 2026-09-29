//go:build !integration

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

// role_reconcile_test.go covers the boot-time convergence of every tenant's
// built-in roles onto the definitions THIS build carries (go-quicbaas#573).
//
// The behavioural half runs against a real embedded qrdb node, because the
// property under test is precisely that the write lands in qrdb's canonical
// role store through the ONE path qrdb permits for a system role — a fake role
// client would happily accept the write that production refuses. The
// enumeration half uses doubles, because "which tenants are visited" is this
// package's own composition and needs no node to be true.

package server

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// reconcileTestTenant is the data-plane tenant these tests reconcile. It is
// deliberately NOT the system tenant: the system tenant is the control plane,
// and the drift #573 was filed for was measured on an ordinary tenant.
const reconcileTestTenant = "burnin"

// newReconcileFixture wires the production chain — embedded qrdb node ->
// ShardedClient -> platform role client resolver -> QRDBRoleStorage ->
// IdentityStore — and seeds reconcileTestTenant with the built-in roles the
// build defines RIGHT NOW, which is what a tenant created by this build holds.
func newReconcileFixture(t *testing.T) (*Server, *iam.IdentityStore, tenantLister) {
	t.Helper()

	sc := newTestNodeBackedClient(t)
	t.Cleanup(func() { _ = sc.Close() })

	mem := iam.NewMemoryStorage()
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: mem,
		RoleStorage: iam.NewQRDBRoleStorage(newPlatformRoleClientResolver(sc)),
	})

	ctx := context.Background()
	for _, r := range qrdbsdk.BuiltInRoles(reconcileTestTenant) {
		perms := make([]string, 0, len(r.Permissions))
		for _, p := range r.Permissions {
			perms = append(perms, string(p))
		}
		_, err := store.CreateRole(ctx, reconcileTestTenant, iam.CreateRoleOptions{
			ID:          r.ID,
			Name:        r.Name,
			Permissions: perms,
		})
		require.NoErrorf(t, err, "seeding built-in role %q", r.Name)
	}

	return &Server{logger: newReconcileTestLogger(t)}, store,
		&fixedTenantLister{tenants: []*iam.Tenant{{ID: reconcileTestTenant}}}
}

// newReconcileTestLogger builds the quiet logger these tests attach to the
// Server so the one-line-per-changed-role output does not flood the run.
func newReconcileTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	return log
}

// roleNamed returns the tenant's role of the given name.
func roleNamed(t *testing.T, store *iam.IdentityStore, tenantID, name string) *iam.Role {
	t.Helper()
	roles, err := store.ListRoles(context.Background(), tenantID, iam.ListOptions{Limit: 200})
	require.NoError(t, err)
	for _, r := range roles {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("tenant %q has no role named %q", tenantID, name)
	return nil
}

// hasPermission reports whether the role carries the exact permission string.
func hasPermission(r *iam.Role, perm string) bool {
	for _, p := range r.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}

// TestReconcileTenantBuiltInRoles_AddsAVerbTheBuildGained is the issue's
// headline case AND its ordering case in one, because they are the same fact.
//
// A permission registered into the process-global registry — which is exactly
// what a module contributes at startup — must be present in the tenant's
// built-in roles after reconciliation. Before #573 it never was: the tenant's
// roles were copied at creation and every later write to a built-in was
// refused, so a build that added a verb stranded every existing tenant with no
// remedy at any privilege level.
//
// The registration happens BEFORE the reconcile call here for the same reason
// the production hook runs at the tail of IAM initialization: qrdb resolves the
// built-in definitions through the registry at CALL time, so reconciling first
// would converge the tenant onto a set missing this verb and report success.
func TestReconcileTenantBuiltInRoles_AddsAVerbTheBuildGained(t *testing.T) {
	srv, store, tenants := newReconcileFixture(t)
	ctx := context.Background()

	const gained = qrdbsdk.Permission("quicbaas573module:reconcile")
	require.False(t, hasPermission(roleNamed(t, store, reconcileTestTenant, qrdbsdk.RoleNameAdmin), string(gained)),
		"fixture is only meaningful if the tenant was seeded WITHOUT the verb")

	// What a module does at startup: contribute the permission, then place it
	// in the roles that should hold it.
	qrdbsdk.RegisterPermissions(gained)
	qrdbsdk.RegisterPermissionRoleGrant(qrdbsdk.PermissionRoleAdmin, gained)

	require.NoError(t, srv.reconcileTenantBuiltInRoles(ctx, store, tenants))

	admin := roleNamed(t, store, reconcileTestTenant, qrdbsdk.RoleNameAdmin)
	require.Truef(t, hasPermission(admin, string(gained)),
		"admin did not gain %q after reconciliation; the tenant is still on the grant it was seeded with", gained)
}

// TestReconcileTenantBuiltInRoles_LeavesCustomRolesAlone proves reconciliation
// touches only the built-in names. A tenant's own roles are its operators'
// deliberate work; converging them onto a definition qrdb did not author would
// silently revoke grants nobody asked to lose.
func TestReconcileTenantBuiltInRoles_LeavesCustomRolesAlone(t *testing.T) {
	srv, store, tenants := newReconcileFixture(t)
	ctx := context.Background()

	// The custom role the coordinator created by hand on the burn-in node as
	// the interim for #573, carrying a permission the build defines.
	customPerm := qrdbsdk.Permission("roles:read")
	require.Truef(t, qrdbsdk.IsPermissionRegistered(customPerm),
		"fixture permission %q must be in the registry; qrdb refuses a role carrying an unregistered permission", customPerm)
	customPerms := []string{string(customPerm)}
	custom, err := store.CreateRole(ctx, reconcileTestTenant, iam.CreateRoleOptions{
		Name:        "sdlc-operator",
		Permissions: customPerms,
	})
	require.NoError(t, err)

	require.NoError(t, srv.reconcileTenantBuiltInRoles(ctx, store, tenants))

	got, err := store.GetRole(ctx, reconcileTestTenant, custom.ID)
	require.NoError(t, err, "the custom role must still exist after reconciliation")
	require.Equal(t, "sdlc-operator", got.Name)
	require.Equal(t, customPerms, got.Permissions,
		"reconciliation rewrote a custom role's permissions")
}

// TestReconcileTenantBuiltInRoles_SecondRunIsANoOp proves idempotency at the
// seam an operator actually observes: a converged tenant reports no changed
// roles, so the boot-time hook logs nothing and writes nothing. Without this
// the hook would rewrite every built-in role of every tenant on every daemon
// start.
func TestReconcileTenantBuiltInRoles_SecondRunIsANoOp(t *testing.T) {
	_, store, _ := newReconcileFixture(t)
	ctx := context.Background()

	first, err := store.ReconcileBuiltInRoles(ctx, reconcileTestTenant)
	require.NoError(t, err)

	second, err := store.ReconcileBuiltInRoles(ctx, reconcileTestTenant)
	require.NoError(t, err)
	require.Falsef(t, second.Changed(),
		"second run reported %d changed roles after the first run converged the tenant (first run: %d)",
		len(second.Roles), len(first.Roles))
}

// TestReconcileTenantBuiltInRoles_SeedsABuiltInTheTenantIsMissing proves a
// built-in absent from the tenant is created rather than skipped. A tenant
// whose `operator` role was never seeded — or was deleted before qrdb made
// built-ins immutable — is exactly as unusable as one holding a stale grant.
func TestReconcileTenantBuiltInRoles_SeedsABuiltInTheTenantIsMissing(t *testing.T) {
	sc := newTestNodeBackedClient(t)
	t.Cleanup(func() { _ = sc.Close() })

	mem := iam.NewMemoryStorage()
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: mem,
		RoleStorage: iam.NewQRDBRoleStorage(newPlatformRoleClientResolver(sc)),
	})
	srv := &Server{logger: newReconcileTestLogger(t)}
	ctx := context.Background()

	// A tenant holding no roles at all: nothing was seeded for it.
	roles, err := store.ListRoles(ctx, "unseeded", iam.ListOptions{Limit: 200})
	require.NoError(t, err)
	require.Empty(t, roles, "fixture requires a tenant with no roles")

	require.NoError(t, srv.reconcileTenantBuiltInRoles(ctx, store,
		&fixedTenantLister{tenants: []*iam.Tenant{{ID: "unseeded"}}}))

	admin := roleNamed(t, store, "unseeded", qrdbsdk.RoleNameAdmin)
	require.NotEmpty(t, admin.Permissions, "the seeded admin role carries this build's permission set")
}

// -----------------------------------------------------------------------------
// Tenant enumeration and failure classification.
// -----------------------------------------------------------------------------

// recordingReconciler records the tenants it was asked to reconcile and can be
// told to fail on a chosen one.
type recordingReconciler struct {
	seen     []string
	failOn   string
	failWith error
}

func (r *recordingReconciler) ReconcileBuiltInRoles(_ context.Context, tenantID string) (*iam.RoleReconcileReport, error) {
	r.seen = append(r.seen, tenantID)
	if r.failWith != nil && tenantID == r.failOn {
		return nil, r.failWith
	}
	return &iam.RoleReconcileReport{TenantID: tenantID}, nil
}

// fixedTenantLister returns a fixed tenant listing, or an error.
type fixedTenantLister struct {
	tenants []*iam.Tenant
	err     error
}

func (l *fixedTenantLister) ListTenants(context.Context, iam.ListOptions) ([]*iam.Tenant, error) {
	return l.tenants, l.err
}

// TestReconcileTenantBuiltInRoles_VisitsEveryTenantIncludingTheControlPlane
// proves the sweep covers every tenant the broker knows about plus the system
// tenant, exactly once each.
//
// The system tenant is named explicitly because the broker's listing does not
// carry it — the same composition abac_sync.go performs for the policy sweep.
// Its built-in roles drift for the same reason a data-plane tenant's do, and
// leaving the control plane out would strand the system administrator on the
// stale grant while repairing everyone else.
func TestReconcileTenantBuiltInRoles_VisitsEveryTenantIncludingTheControlPlane(t *testing.T) {
	srv := &Server{logger: newReconcileTestLogger(t)}
	rec := &recordingReconciler{}
	lister := &fixedTenantLister{tenants: []*iam.Tenant{
		{ID: "burnin"},
		{ID: "acme"},
		// Duplicates of the control plane must not produce a second visit.
		{ID: qrdbsdk.SystemTenantID},
		// Malformed entries must not produce an empty-tenant call.
		{ID: ""},
		nil,
	}}

	require.NoError(t, srv.reconcileTenantBuiltInRoles(context.Background(), rec, lister))

	got := append([]string(nil), rec.seen...)
	sort.Strings(got)
	want := []string{"acme", "burnin", qrdbsdk.SystemTenantID}
	sort.Strings(want)
	require.Equal(t, want, got)
}

// TestReconcileTenantBuiltInRoles_WithoutATenantListerStillCoversTheControlPlane
// proves a process with no tenant manager wired still reconciles the system
// tenant rather than doing nothing.
func TestReconcileTenantBuiltInRoles_WithoutATenantListerStillCoversTheControlPlane(t *testing.T) {
	srv := &Server{logger: newReconcileTestLogger(t)}
	rec := &recordingReconciler{}

	require.NoError(t, srv.reconcileTenantBuiltInRoles(context.Background(), rec, nil))
	require.Equal(t, []string{qrdbsdk.SystemTenantID}, rec.seen)
}

// TestReconcileTenantBuiltInRoles_NilReconcilerIsANoOp proves a process
// without a role reconciler wired neither panics nor reports a failure.
func TestReconcileTenantBuiltInRoles_NilReconcilerIsANoOp(t *testing.T) {
	srv := &Server{logger: newReconcileTestLogger(t)}
	require.NoError(t, srv.reconcileTenantBuiltInRoles(context.Background(), nil, &fixedTenantLister{err: errors.New("never reached")}))
}

// TestReconcileTenantBuiltInRoles_TenantListingFailureIsTyped proves a failure
// to enumerate tenants is reported as a typed RoleReconcileError with no
// tenant named, so the boot warning distinguishes "could not find out which
// tenants exist" from "one tenant failed".
func TestReconcileTenantBuiltInRoles_TenantListingFailureIsTyped(t *testing.T) {
	srv := &Server{logger: newReconcileTestLogger(t)}
	rec := &recordingReconciler{}

	err := srv.reconcileTenantBuiltInRoles(context.Background(), rec, &fixedTenantLister{err: errors.New("store sealed")})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRoleReconcile)

	var typed *RoleReconcileError
	require.ErrorAs(t, err, &typed)
	require.Empty(t, typed.TenantID, "an enumeration failure names no tenant")
	require.Empty(t, rec.seen, "no tenant may be reconciled when the listing failed")
}

// TestReconcileTenantBuiltInRoles_PerTenantFailureNamesTheTenant proves a
// failing tenant is identified in the typed error. A sweep that reported only
// "reconciliation failed" would leave an operator to find the affected tenant
// themselves.
func TestReconcileTenantBuiltInRoles_PerTenantFailureNamesTheTenant(t *testing.T) {
	srv := &Server{logger: newReconcileTestLogger(t)}
	rec := &recordingReconciler{failOn: "burnin", failWith: errors.New("qrdb unreachable")}

	err := srv.reconcileTenantBuiltInRoles(context.Background(), rec, &fixedTenantLister{
		tenants: []*iam.Tenant{{ID: "burnin"}},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRoleReconcile)

	var typed *RoleReconcileError
	require.ErrorAs(t, err, &typed)
	require.Equal(t, "burnin", typed.TenantID)
	require.Contains(t, typed.Error(), "burnin")
}
