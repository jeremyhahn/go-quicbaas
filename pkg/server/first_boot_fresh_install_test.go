//go:build !integration

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package server

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// Fresh-install fixture (#60)
// =============================================================================
//
// The first-boot refusal (#57) is a security property. A test proves it only
// if the test reaches the branch where it is enforced. Two things kept every
// earlier harness from getting there:
//
//   - A test that stubbed initIAMComponentsFn proved that the CALLER refuses
//     a *NoAdministratorError, not that initializeIAMComponents ever PRODUCES
//     one. Reverting the producer to warn-and-carry-on left the suite green.
//   - No harness could stop qrdb's own first-boot part-way through, so the
//     re-entry path that resolves already-seeded roles by name was never run
//     from a partial state that first-boot itself had left.
//
// The fixture comes in two shapes, one per seam the tests need to reach:
//
//   - newFreshInstallNode: a real embedded qrdb node with nothing
//     bootstrapped, driven through initializeIAMComponents, which is the
//     function production calls.
//   - newFreshInstall: in-memory IAM components over a store that holds
//     nothing first-boot writes. qrdb's first-boot writes users and roles
//     through a fault injector and a call recorder, so a test can cut the
//     bootstrap at a chosen point inside qrdb and observe how re-entry
//     resolves what the failed boot left behind.
//
// Every test built on this fixture checks that the store really is fresh
// before relying on it, because a stale store once made a first-boot test pass
// without ever running the code under test.

// injectedFirstBootFaultError is the failure the fixture injects. It is a
// distinct type so a test can require that the boot failed on the fault it
// injected, not on some unrelated earlier step.
type injectedFirstBootFaultError struct {
	Op string
}

// Error implements the error interface.
func (e *injectedFirstBootFaultError) Error() string {
	return "injected first-boot fault: " + e.Op
}

// faultingUserStorage fails the next CreateUser once armed and otherwise
// delegates. qrdb's first-boot creates the platform admin user AFTER it has
// written the system tenant and the built-in roles, so failing that one write
// cuts the bootstrap inside qrdb with the roles already persisted.
type faultingUserStorage struct {
	iam.UserStorage
	failNextCreate atomic.Bool
}

// CreateUser fails once when armed, then behaves like the wrapped store.
func (f *faultingUserStorage) CreateUser(ctx context.Context, user *iam.User) error {
	if f.failNextCreate.CompareAndSwap(true, false) {
		return &injectedFirstBootFaultError{Op: "create user"}
	}
	return f.UserStorage.CreateUser(ctx, user)
}

// recordingRoleStorage counts by-name lookups of the system Admin role. The
// only production caller of that lookup during first-boot is
// RoleAdapter.GetRoleByName, which is the resolver first-boot uses to bind a
// grant to an already-persisted role.
type recordingRoleStorage struct {
	iam.RoleStorage
	adminLookups atomic.Int64
}

// GetRoleByName records a system Admin lookup and delegates.
func (r *recordingRoleStorage) GetRoleByName(ctx context.Context, tenantID, name string) (*iam.Role, error) {
	if tenantID == qrdbsdk.SystemTenantID && name == qrdbsdk.RoleNameAdmin {
		r.adminLookups.Add(1)
	}
	return r.RoleStorage.GetRoleByName(ctx, tenantID, name)
}

// freshInstall is a server plus IAM components over a store where no
// first-boot has run.
type freshInstall struct {
	srv   *Server
	c     *iamComponents
	mem   *iam.MemoryStorage
	users *faultingUserStorage
	roles *recordingRoleStorage
}

// newFreshInstall builds the in-memory fixture and checks that it is fresh.
func newFreshInstall(t *testing.T) *freshInstall {
	t.Helper()

	srv := withTempServer(t)
	srv.config.Auth.AllowPasswordAuth = false
	srv.config.Auth.InsecureDevMode = false

	mem := iam.NewMemoryStorage()
	f := &freshInstall{
		srv:   srv,
		mem:   mem,
		users: &faultingUserStorage{UserStorage: mem},
		roles: &recordingRoleStorage{RoleStorage: mem},
	}
	f.c, _ = buildTestIAMComponentsOver(t, mem, f.users, f.roles)

	f.requireFresh(t)
	return f
}

// notFreshError reports which first-boot artifact a supposedly fresh store
// already holds.
type notFreshError struct {
	Artifact string
}

// Error implements the error interface.
func (e *notFreshError) Error() string {
	return "fixture is not fresh: " + e.Artifact + " already exists"
}

// checkFresh returns nil only when the store holds nothing first-boot writes:
// no admin assignment, no system Admin role and no platform admin user. It
// reads the role store directly rather than through the recorder, so the check
// does not count as a resolver lookup.
func (f *freshInstall) checkFresh(ctx context.Context) error {
	assignments, err := f.c.adminManager.ListAssignments(ctx, iam.ListAdminAssignmentsOptions{})
	if err != nil {
		return err
	}
	if len(assignments) > 0 {
		return &notFreshError{Artifact: "an admin assignment"}
	}

	_, err = f.mem.GetRoleByName(ctx, qrdbsdk.SystemTenantID, qrdbsdk.RoleNameAdmin)
	if err == nil {
		return &notFreshError{Artifact: "the system Admin role"}
	}
	if !iam.IsRoleNotFound(err) {
		return err
	}

	if _, err := f.c.identityStore.GetUserByEmail(ctx, qrdbsdk.SystemTenantID, bootstrapAdminEmail); err == nil {
		return &notFreshError{Artifact: "the platform admin user"}
	}
	return nil
}

// requireFresh fails the test unless checkFresh passes.
func (f *freshInstall) requireFresh(t *testing.T) {
	t.Helper()
	require.NoError(t, f.checkFresh(context.Background()))
}

// newFreshInstallNode returns a server whose embedded qrdb node is up and
// whose client is wired, with nothing bootstrapped. It is the harness for
// initializeIAMComponents itself. That function builds its own IAM components,
// so freshness is not checked here: each caller proves it by asserting which
// first-boot branch ran.
func newFreshInstallNode(t *testing.T) *Server {
	t.Helper()
	srv := withTempServer(t)
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init")
	t.Cleanup(func() { assert.NoError(t, srv.stopQRDBNode(), "stopping the qrdb node") })
	srv.qrdbClient = newNodeBackedShardedClient(srv.qrdbNode)
	return srv
}

// countFirstBootBanners replaces the banner seam with a counter.
func countFirstBootBanners(t *testing.T) *int {
	t.Helper()
	var n int
	orig := writeFirstBootBannerFn
	writeFirstBootBannerFn = func(_ interface{ WriteString(string) (int, error) }, _ adminEnrollmentBanner) {
		n++
	}
	t.Cleanup(func() { writeFirstBootBannerFn = orig })
	return &n
}

// =============================================================================
// Acceptance case 1: initializeIAMComponents produces the refusal
// =============================================================================

// TestInitializeIAMComponents_RefusesWhenFirstBootFailsOnAFreshInstall
// reproduces the live defect at its source. A CA-backed node with no
// iam.spiffe_trust_domain cannot issue a usable admin certificate. Before #57
// this logged a WARN and served with no administrator.
//
// This test fails if initializeIAMComponents is reverted to
// warn-and-carry-on. The startService tests do not catch that revert, because
// they stub this function.
func TestInitializeIAMComponents_RefusesWhenFirstBootFailsOnAFreshInstall(t *testing.T) {
	srv := newFreshInstallNode(t)
	srv.setCA(newFakeTrustedCA(t))
	srv.config.IAM.SPIFFETrustDomain = ""
	banners := countFirstBootBanners(t)

	components, err := srv.initializeIAMComponents()

	require.Error(t, err, "a fresh install whose first-boot failed must refuse, not serve")
	assert.Nil(t, components, "a refused boot must hand back no IAM components to serve with")

	var noAdmin *NoAdministratorError
	require.True(t, errors.As(err, &noAdmin), "err = %v, want *NoAdministratorError", err)
	assert.ErrorIs(t, err, ErrNoSPIFFETrustDomain, "the specific cause must be reachable")

	// Proof that the fresh-install branch ran. "issue admin certificate" is
	// raised only after hasSystemAdministrator found nobody. A store that
	// already had an administrator would have returned before reaching it.
	var fb *FirstBootBootstrapError
	require.True(t, errors.As(err, &fb))
	assert.Equal(t, "issue admin certificate", fb.Op,
		"the refusal came from a different step, so this test did not exercise the fresh-install path")
	assert.Zero(t, *banners, "no enrollment may be announced for a boot that created no administrator")

	// The operator fixes the cause and restarts on the same data. The boot
	// must resume and complete, as the refusal's message promises.
	srv.config.IAM.SPIFFETrustDomain = testTrustDomain
	components, err = srv.initializeIAMComponents()
	require.NoError(t, err, "after the cause is fixed the restart must complete first-boot")
	require.NotNil(t, components)
	assert.Equal(t, 1, *banners, "the restart did not run first-boot")

	assignments, err := components.GetAdminManager().ListAssignments(context.Background(),
		iam.ListAdminAssignmentsOptions{ActiveOnly: true, AdminRole: iam.AdminRoleSuperAdmin})
	require.NoError(t, err)
	assert.Len(t, assignments, 1, "the resumed boot must leave exactly one system grant")
}

// =============================================================================
// Acceptance case 2: a failure inside qrdb's own first-boot resumes
// =============================================================================

// TestFirstBootBootstrap_ResumesAfterAFailureInsideQRDBFirstBoot cuts the
// bootstrap INSIDE runBootstrapSystem. qrdb's first-boot has written the
// system tenant and the built-in roles but not the admin user. The test then
// requires the next boot to resume through the role-name resolver.
//
// This is the cut point that deciding completion from qrdb's own artifacts
// would get wrong: the roles are present, so the store looks partly
// bootstrapped. Resuming from here needs first-boot to bind the admin's
// grant to the role identifier the store actually holds, and only
// RoleAdapter.GetRoleByName can answer that. Without it, first-boot refuses
// with UnresolvableSystemRoleError. With a resolver that never resolves, it
// binds to a derived identifier without looking.
func TestFirstBootBootstrap_ResumesAfterAFailureInsideQRDBFirstBoot(t *testing.T) {
	f := newFreshInstall(t)
	ctx := context.Background()
	banners := countFirstBootBanners(t)

	// Boot 1: qrdb's first-boot fails creating the platform admin user.
	f.users.failNextCreate.Store(true)
	err := f.srv.firstBootBootstrap(ctx, f.c)
	require.Error(t, err)
	var injected *injectedFirstBootFaultError
	require.True(t, errors.As(err, &injected),
		"boot 1 failed on something other than the injected fault: %v", err)
	var fb *FirstBootBootstrapError
	require.True(t, errors.As(err, &fb))
	require.Equal(t, "firstboot.Initialize", fb.Op,
		"the fault must surface from inside qrdb's first-boot (runBootstrapSystem)")
	assert.ErrorIs(t, err, ErrFirstBootBootstrap)

	// The partial state boot 1 left behind: roles present, no admin, no grant.
	adminRole, err := f.mem.GetRoleByName(ctx, qrdbsdk.SystemTenantID, qrdbsdk.RoleNameAdmin)
	require.NoError(t, err, "the cut must fall AFTER qrdb seeded the built-in roles")
	_, err = f.c.identityStore.GetUserByEmail(ctx, qrdbsdk.SystemTenantID, bootstrapAdminEmail)
	require.Error(t, err, "the cut must fall BEFORE the admin user was created")
	assignments, err := f.c.adminManager.ListAssignments(ctx, iam.ListAdminAssignmentsOptions{})
	require.NoError(t, err)
	require.Empty(t, assignments, "a boot that failed inside qrdb must leave no grant behind")
	assert.Zero(t, *banners)

	// Boot 2: the fault is gone. The roles already exist, so qrdb must learn
	// their identifiers from the resolver.
	lookupsBefore := f.roles.adminLookups.Load()
	require.NoError(t, f.srv.firstBootBootstrap(ctx, f.c),
		"re-entry over the state a failed qrdb first-boot left behind must complete")
	assert.Greater(t, f.roles.adminLookups.Load(), lookupsBefore,
		"re-entry did not resolve the pre-existing Admin role through RoleAdapter.GetRoleByName")

	admin, err := f.c.identityStore.GetUserByEmail(ctx, qrdbsdk.SystemTenantID, bootstrapAdminEmail)
	require.NoError(t, err, "the resumed boot must create the platform admin")
	assert.Contains(t, admin.Roles, adminRole.ID,
		"the admin must hold the role identifier the store persisted, not a derived guess")
	assert.Equal(t, 1, *banners, "the resumed boot must announce the enrollment")

	assignments, err = f.c.adminManager.ListAssignments(ctx, iam.ListAdminAssignmentsOptions{ActiveOnly: true})
	require.NoError(t, err)
	require.Len(t, assignments, 1, "the resumed boot must leave exactly one grant")
	administered, err := hasSystemAdministrator(ctx, f.c, assignments)
	require.NoError(t, err)
	assert.True(t, administered, "the resumed boot's grant must be read as a usable administrator")
}

// TestFreshInstall_RejectsAStoreThatIsNotFresh is the fixture's own error
// path. A fixture that reports "fresh" over a bootstrapped store is the defect
// that once let a first-boot test pass without reaching the code under test.
func TestFreshInstall_RejectsAStoreThatIsNotFresh(t *testing.T) {
	f := newFreshInstall(t)
	countFirstBootBanners(t)
	require.NoError(t, f.srv.firstBootBootstrap(context.Background(), f.c))

	err := f.checkFresh(context.Background())
	var notFresh *notFreshError
	require.True(t, errors.As(err, &notFresh),
		"checkFresh accepted a store on which first-boot has already run: %v", err)
}
