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
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// A server with no administrator does not start (#57)
// =============================================================================
//
// Reproduced from a real boot: a data directory with no iam.spiffe_trust_domain
// configured logged
//
//	"First-boot bootstrap failed — no admin credential will be created this boot"
//
// at WARN and then CARRIED ON SERVING, answering /health/ready with 200 while no
// admin user, no admin assignment and no certificate existed. Readiness knows
// only about raft, so it had nothing to say about administrability.
//
// The warning was also visible only because that boot ran at --log-level warn.
// At error level it would have been silent, while the sibling branch in
// firstBootBootstrap deliberately writes its banner to stderr "NOT the
// structured logger — so operators see it regardless of log level".
//
// Two halves, two harnesses. The startService tests below stub the IAM seam,
// so they prove that a *NoAdministratorError reaching startService refuses the
// boot. That initializeIAMComponents PRODUCES one is proved against the
// fresh-install fixture (first_boot_fresh_install_test.go), which reaches the
// refusal on a real node where no first-boot has run. Reverting the producer to
// warn-and-carry-on fails
// TestInitializeIAMComponents_RefusesWhenFirstBootFailsOnAFreshInstall.

// serverReadyForIAMInit builds a server whose qrdb node is up, which is what
// startService requires before it reaches IAM initialization at all.
func serverReadyForIAMInit(t *testing.T) *Server {
	t.Helper()
	srv := withTempServer(t)
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	return srv
}

// stubIAMSeamErr makes the IAM seam fail with err.
func stubIAMSeamErr(err error) {
	seamMu.Lock()
	initIAMComponentsFn = func(_ *Server) (service.IAMComponents, error) { return nil, err }
	seamMu.Unlock()
}

// TestStartService_RefusesWhenFirstBootCreatedNoAdministrator is the
// reproduction of the observed sequence.
func TestStartService_RefusesWhenFirstBootCreatedNoAdministrator(t *testing.T) {
	saveAndRestoreLifecycleSeams(t)
	// THE SHAPE PRODUCTION ACTUALLY BUILDS. firstBootBootstrap wraps the
	// sentinel in a *FirstBootBootstrapError before initializeIAMComponents
	// wraps that. A fake that hands the sentinel over directly skips the
	// intermediate wrapper — and it was exactly that shortcut which hid the fact
	// that the cause used to be unreachable through it.
	stubIAMSeamErr(&NoAdministratorError{Cause: &FirstBootBootstrapError{
		Op:    "issue admin certificate",
		Cause: ErrNoSPIFFETrustDomain,
	}})

	err := serverReadyForIAMInit(t).startService()
	require.Error(t, err, "the server started with no administrator; it must refuse")

	var noAdmin *NoAdministratorError
	require.True(t, errors.As(err, &noAdmin),
		"err = %v, want a *NoAdministratorError — asserting merely that startup "+
			"failed would pass on any unrelated failure", err)
	// Both must resolve THROUGH the real wrapper chain: the class of failure and
	// the specific cause.
	assert.ErrorIs(t, err, ErrFirstBootBootstrap,
		"the failure class is unreachable via errors.Is")
	assert.ErrorIs(t, err, ErrNoSPIFFETrustDomain,
		"the specific cause is unreachable via errors.Is — a program cannot tell "+
			"a misconfiguration from any other boot failure, even though the "+
			"message prints it")
}

// TestStartService_RefusesForANonConfigBootstrapFailureToo defends the WIDTH of
// the rule.
//
// The cause does not grade the severity: a missing config line and corrupt
// storage leave the same hole. This exists so a future "surely corrupt storage
// can carry on" change has to argue with a test rather than with a comment.
func TestStartService_RefusesForANonConfigBootstrapFailureToo(t *testing.T) {
	saveAndRestoreLifecycleSeams(t)
	corrupt := errors.New("first-boot: admin assignment store is corrupt")
	stubIAMSeamErr(&NoAdministratorError{Cause: corrupt})

	err := serverReadyForIAMInit(t).startService()

	var noAdmin *NoAdministratorError
	require.True(t, errors.As(err, &noAdmin),
		"a non-config bootstrap failure was tolerated: %v", err)
	assert.ErrorIs(t, err, corrupt, "the cause is unreachable")
}

// TestStartService_MustNotReportSuccessWithNoAdministrator is the PROPERTY.
//
// The defect was never "a warning was logged" — it was a live server answering
// /health/ready with 200 while nobody could administer it. Start() builds the
// service facade and only then starts the API servers, so the single thing that
// keeps a listener from ever coming up is startService REFUSING. Success here is
// what would let the boot walk on to startRESTServer.
//
// SCOPE, STATED RATHER THAN IMPLIED: this does NOT assert that nothing capable
// of answering readiness exists. The QuicBaaS facade is wired EARLIER in
// startService than the IAM initialization, so at the moment of refusal it is
// already constructed. The guarantee comes from Start() aborting before
// startRESTServer, not from the facade being absent.
func TestStartService_MustNotReportSuccessWithNoAdministrator(t *testing.T) {
	saveAndRestoreLifecycleSeams(t)
	stubIAMSeamErr(&NoAdministratorError{Cause: ErrNoSPIFFETrustDomain})

	srv := serverReadyForIAMInit(t)

	require.Error(t, srv.startService(),
		"startService reported SUCCESS with no administrator. Start() treats that "+
			"as permission to continue, so the next step is startRESTServer and "+
			"the result is a live /health/ready answering 200 for a server nobody "+
			"can administer — the defect itself")
}

// TestStartService_StartsWhenIAMInitSucceeds is the REGRESSION DIRECTION THAT
// MATTERS MOST: a false refusal bricks a deployment.
//
// The refusal must be reachable only through an IAM failure. With the seam
// reporting success, startService must complete — otherwise every healthy boot
// is now a failed boot.
func TestStartService_StartsWhenIAMInitSucceeds(t *testing.T) {
	saveAndRestoreLifecycleSeams(t)
	seamMu.Lock()
	initIAMComponentsFn = func(_ *Server) (service.IAMComponents, error) { return nil, nil }
	seamMu.Unlock()

	require.NoError(t, serverReadyForIAMInit(t).startService(),
		"a boot whose IAM initialization succeeded was refused")
}

// TestNoAdministratorError_TellsTheOperatorWhatToDo covers the text itself,
// because it is the first thing every misconfigured operator reads.
func TestNoAdministratorError_TellsTheOperatorWhatToDo(t *testing.T) {
	msg := (&NoAdministratorError{Cause: ErrNoSPIFFETrustDomain}).Error()

	for _, want := range []string{
		"refuses to start",        // what happened
		"nobody can administer",   // why it is fatal
		"iam.spiffe_trust_domain", // the cause carries its own remedy
		"will resume",             // true in the fresh AND the partial case
	} {
		assert.Contains(t, msg, want,
			"the message omits %q, leaving the operator without it", want)
	}

	// The remedy belongs to the CAUSE, not to this wrapper: a hardcoded example
	// would be wrong for every other cause.
	generic := (&NoAdministratorError{Cause: errors.New("storage is corrupt")}).Error()
	assert.NotContains(t, generic, "iam.spiffe_trust_domain",
		"the wrapper hardcodes one cause's remedy")
	assert.Contains(t, generic, "storage is corrupt")
}

// TestNoAdministratorError_CauselessStillReads is the invalid-input half: a
// wrapper built with no cause must still produce a usable message and must not
// panic reaching for one.
func TestNoAdministratorError_CauselessStillReads(t *testing.T) {
	err := &NoAdministratorError{}

	msg := err.Error()
	assert.Contains(t, msg, "refuses to start")
	assert.NotContains(t, msg, "cause:", "no cause must not be printed as an empty one")
	assert.NoError(t, errors.Unwrap(err), "a causeless wrapper unwraps to nothing")
}

// TestNoAdministratorError_WithholdsResumeForTheOneUnresumableCause covers the
// single cause the closing reassurance is FALSE for. An undelivered bundle
// cannot be resumed by restarting: the certificate is already bound and qrdb's
// idempotent first-boot will not reissue it, so every restart lands on the same
// refusal. The composed message used to carry both remedies at once, with the
// wrong one last, and an operator who follows the last one restarts forever.
func TestNoAdministratorError_WithholdsResumeForTheOneUnresumableCause(t *testing.T) {
	// The shape the refusal actually reaches the operator in: the bundle failure
	// wrapped by the bootstrap wrapper, wrapped by the refusal.
	err := &NoAdministratorError{Cause: &FirstBootBootstrapError{
		Op:    "deliver admin bundle",
		Cause: &AdminBundleNotDeliveredError{},
	}}

	msg := err.Error()
	assert.NotContains(t, msg, "will resume",
		"the operator is told to restart into a boot that refuses identically, "+
			"contradicting the wipe-and-re-bootstrap remedy in the same sentence")
	assert.Contains(t, msg, "wipe and re-bootstrap",
		"withholding the reassurance must not also lose the real remedy")
	assert.Contains(t, msg, "refuses to start", "the refusal itself must still be stated")

	// The suppression is keyed on the SENTINEL, so it survives the wrapping
	// above rather than depending on a direct-cause comparison.
	assert.ErrorIs(t, err, ErrAdminBundleNotDelivered)
}

// TestNoAdministratorError_KeepsResumeForEveryOtherCause is the other half, and
// the one that matters more: withholding the reassurance too widely sends an
// operator to wipe state that a restart would have recovered.
func TestNoAdministratorError_KeepsResumeForEveryOtherCause(t *testing.T) {
	for name, cause := range map[string]error{
		"a missing config value": ErrNoSPIFFETrustDomain,
		"an unrelated failure":   errors.New("storage is corrupt"),
		"a wrapped failure":      &FirstBootBootstrapError{Op: "list admin assignments", Cause: errors.New("no leader")},
	} {
		t.Run(name, func(t *testing.T) {
			msg := (&NoAdministratorError{Cause: cause}).Error()
			assert.Contains(t, msg, "will resume",
				"a resumable failure must say so, or the operator wipes recoverable state")
		})
	}
}

// =============================================================================
// FirstBootBootstrapError must not swallow its cause
// =============================================================================

// TestFirstBootBootstrapError_ReachesBothSentinelAndCause is the regression for
// the Unwrap fix. It used to return the sentinel ALONE, so the specific cause
// was legible to a human in the message and invisible to errors.Is.
func TestFirstBootBootstrapError_ReachesBothSentinelAndCause(t *testing.T) {
	err := &FirstBootBootstrapError{Op: "issue admin certificate", Cause: ErrNoSPIFFETrustDomain}

	assert.ErrorIs(t, err, ErrFirstBootBootstrap, "the failure class must stay reachable")
	assert.ErrorIs(t, err, ErrNoSPIFFETrustDomain,
		"the cause is unreachable: a program cannot act on a failure it cannot identify")
}

// TestFirstBootBootstrapError_CauselessIsStillTheFailureClass covers the
// invalid-input half: a wrapper built without a cause must still report the
// class and must not panic formatting a nil.
func TestFirstBootBootstrapError_CauselessIsStillTheFailureClass(t *testing.T) {
	err := &FirstBootBootstrapError{Op: "list admin assignments"}

	assert.ErrorIs(t, err, ErrFirstBootBootstrap)
	assert.Equal(t, "first-boot bootstrap: list admin assignments", err.Error())
	assert.False(t, errors.Is(err, ErrNoSPIFFETrustDomain),
		"a causeless wrapper must not match an unrelated cause")
}

// TestAdminBundleNotDeliveredError_SaysWhatToDo covers the message an operator
// sees in the state it describes, and the sentinel a program matches on.
func TestAdminBundleNotDeliveredError_SaysWhatToDo(t *testing.T) {
	err := &AdminBundleNotDeliveredError{}

	for _, want := range []string{
		"no private key",        // what is missing
		"bound certificate",     // why it matters — the cert is live without its key
		"wipe and re-bootstrap", // the ruled remedy while reissue does not exist
	} {
		assert.Contains(t, err.Error(), want, "the message omits %q", want)
	}
	assert.ErrorIs(t, err, ErrAdminBundleNotDelivered, "must unwrap to the sentinel")
}

// =============================================================================
// A count is not an administrator
// =============================================================================

// seedActiveUser creates a user who may authenticate, which is what
// hasSystemAdministrator requires of the principal a grant names. It returns the
// record so callers use the id the STORE assigned: the identity store owns the
// identifier space (qrdb's Snowflake in production), so a grant built from an id
// the caller picked would name nobody.
func seedActiveUser(t *testing.T, c *iamComponents, username, email string) *iam.User {
	t.Helper()
	user, err := c.identityStore.CreateUser(context.Background(), qrdbsdk.SystemTenantID,
		iam.CreateUserOptions{
			Email:     email,
			Username:  username,
			Lifecycle: iam.LifecycleActive,
		})
	require.NoError(t, err, "seeding the admin user")
	require.True(t, user.CanAuthenticate(), "the seeded user must be able to authenticate")
	return user
}

// TestHasSystemAdministrator_ACountIsNotAnAdministrator is the core regression.
//
// The check this replaced received `len(assignments)` and asked whether it was
// positive. ListAssignments accepts ANY active admin assignment, so an orphaned
// grant whose user was deleted — or a tenant-scoped administrator with no system
// authority — satisfied it, and bootstrap was skipped for a platform nobody
// could administer. That is the same fail-open #57 exists to remove, reached by
// a longer route.
func TestHasSystemAdministrator_ACountIsNotAnAdministrator(t *testing.T) {
	c := buildTestIAMComponents(t)
	ctx := context.Background()

	adminID := seedActiveUser(t, c, "admin", bootstrapAdminEmail).ID

	systemGrant := &iam.AdminAssignment{UserID: strconv.FormatUint(adminID, 10), AdminRole: iam.AdminRoleSuperAdmin}

	cases := map[string]struct {
		assignments []*iam.AdminAssignment
		want        bool
		why         string
	}{
		"a real system administrator": {
			[]*iam.AdminAssignment{systemGrant}, true,
			"a super-admin grant on an existing authenticating user IS an administrator",
		},
		"orphaned grant — the user was deleted": {
			[]*iam.AdminAssignment{{UserID: "deleted-user", AdminRole: iam.AdminRoleSuperAdmin}}, false,
			"an assignment that outlived its user confers nothing, but it is still ACTIVE and still counted by len()",
		},
		"tenant administrator, not a system one": {
			[]*iam.AdminAssignment{{UserID: strconv.FormatUint(adminID, 10), AdminRole: iam.AdminRoleSuperAdmin, TenantIDs: []string{"acme"}}}, false,
			"a tenant-scoped grant cannot administer the platform, and first-boot creates the system grant with NO tenant scoping",
		},
		"lower-privilege role": {
			[]*iam.AdminAssignment{{UserID: strconv.FormatUint(adminID, 10), AdminRole: iam.AdminRoleTenantAdmin}}, false,
			"a non-super-admin role is not the bootstrap administrator",
		},
		"a nil entry in the listing": {
			[]*iam.AdminAssignment{nil}, false,
			"a nil assignment is not an administrator, and must not panic on the way to that answer",
		},
		"nothing at all": {nil, false, "a fresh install has no administrator"},
		"an orphan ahead of a real administrator": {
			[]*iam.AdminAssignment{
				{UserID: "deleted-user", AdminRole: iam.AdminRoleSuperAdmin},
				systemGrant,
			}, true,
			"the scan must not stop at the first grant that confers nothing — that is " +
				"how a truncated listing would send an administered node back through first-boot",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := hasSystemAdministrator(ctx, c, tc.assignments)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got, tc.why)
		})
	}
}

// TestHasSystemAdministrator_ASuspendedAdminIsNotAnAdministrator covers the
// lifecycle half of the rule.
//
// The account exists and the grant is the right shape, so every structural check
// passes — but the store will not let it log in, so it administers nothing. The
// question is asked of iam.User.CanAuthenticate rather than compared here, so
// this stays true for any non-authenticating lifecycle.
func TestHasSystemAdministrator_ASuspendedAdminIsNotAnAdministrator(t *testing.T) {
	c := buildTestIAMComponents(t)
	ctx := context.Background()

	adminID := seedActiveUser(t, c, "suspended", "suspended@"+qrdbsdk.SystemTenantID+".qrdb.local").ID
	suspended := iam.LifecycleSuspended
	_, err := c.identityStore.UpdateUser(ctx, qrdbsdk.SystemTenantID, adminID,
		iam.UpdateUserOptions{Lifecycle: &suspended})
	require.NoError(t, err, "suspending the admin")

	got, err := hasSystemAdministrator(ctx, c,
		[]*iam.AdminAssignment{{UserID: strconv.FormatUint(adminID, 10), AdminRole: iam.AdminRoleSuperAdmin}})
	require.NoError(t, err)
	assert.False(t, got,
		"a grant on an account that cannot authenticate was read as an administrator")
}

// TestHasSystemAdministrator_AnUnreadableStoreFailsClosed is the error-handling
// half: "I could not check" must never be reported as "there is nobody".
//
// Reading it as absence would send an already-bootstrapped node back through
// first-boot on a transient storage fault, and bootstrap would then fail on the
// state it finds — a storage blip escalating into a refused boot.
// errUserRead is unreadableUserStorage's read failure.
var errUserRead = errors.New("user store cannot be read")

// unreadableUserStorage is a user store whose reads fail.
type unreadableUserStorage struct {
	*iam.MemoryStorage
}

func (unreadableUserStorage) GetUser(context.Context, string, uint64) (*iam.User, error) {
	return nil, errUserRead
}

func TestHasSystemAdministrator_AnUnreadableStoreFailsClosed(t *testing.T) {
	c := buildTestIAMComponents(t)
	// A store whose reads fail answers neither "found" nor "not found", which
	// is exactly the "unreadable" class this must fail closed on.
	c.identityStore = iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: unreadableUserStorage{MemoryStorage: iam.NewMemoryStorage()},
		RoleStorage: iam.NewMemoryStorage(),
	})

	got, err := hasSystemAdministrator(context.Background(), c,
		[]*iam.AdminAssignment{{UserID: "40001", AdminRole: iam.AdminRoleSuperAdmin}})

	assert.False(t, got, "no administrator may be reported when the check could not run")
	require.Error(t, err, "an unresolved lookup must be surfaced, not swallowed as absence")
	assert.ErrorIs(t, err, errUserRead, "the store's own failure must stay inspectable")
	assert.ErrorIs(t, err, ErrFirstBootBootstrap,
		"the failure must arrive as a first-boot bootstrap failure so the boot refuses")
	var fb *FirstBootBootstrapError
	require.True(t, errors.As(err, &fb))
	assert.Equal(t, "verify bootstrap administrator", fb.Op,
		"the operation must name the check that could not answer")
}

func TestHasSystemAdministrator_AnUnparseableAssignmentConfersNothing(t *testing.T) {
	c := buildTestIAMComponents(t)

	for name, userID := range map[string]string{
		"empty":       "",
		"non-numeric": "admin",
		"overflowing": "18446744073709551616",
		"zero":        "0",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := hasSystemAdministrator(context.Background(), c,
				[]*iam.AdminAssignment{{UserID: userID, AdminRole: iam.AdminRoleSuperAdmin}})

			require.NoError(t, err, "an assignment naming no user is an answer, not a failure")
			assert.False(t, got, "an assignment naming no user makes nobody an administrator")
		})
	}
}

// TestHasSystemAdministrator_AnUnreadableGrantDoesNotHideARealOne keeps the
// fail-closed rule from over-firing.
//
// An unresolved lookup is remembered, not returned immediately: if another
// assignment confirms an administrator outright, the answer is known regardless.
// Returning the error first would refuse a boot that has a perfectly good
// administrator.
func TestHasSystemAdministrator_AnUnreadableGrantDoesNotHideARealOne(t *testing.T) {
	c := buildTestIAMComponents(t)

	adminID := seedActiveUser(t, c, "admin", bootstrapAdminEmail).ID

	got, err := hasSystemAdministrator(context.Background(), c, []*iam.AdminAssignment{
		{UserID: "", AdminRole: iam.AdminRoleSuperAdmin}, // unreadable
		{UserID: strconv.FormatUint(adminID, 10), AdminRole: iam.AdminRoleSuperAdmin},
	})

	require.NoError(t, err, "a confirmed administrator settles the question")
	assert.True(t, got)
}

// =============================================================================
// The re-entry that the ordering makes safe
// =============================================================================

// TestFirstBootBootstrap_ReEntersAfterAPostGrantFailureCannotHappen is the
// ordering regression.
//
// Three steps used to run AFTER the super-admin grant was persisted — PIV
// binding, hardware-factor enrollment and bundle delivery — so a failure in any
// of them left the grant behind, and the next boot read it as "already
// bootstrapped" and served with an administrator who had no usable credential.
// The grant is now the LAST step, which is what makes a failed bootstrap
// resumable: this drives a failure at PIV binding and requires that NO grant was
// created, then restarts and requires the retry to complete.
func TestFirstBootBootstrap_ReEntersAfterAPostGrantFailureCannotHappen(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Auth.AllowPasswordAuth = false
	srv.config.Auth.InsecureDevMode = false
	c := buildTestIAMComponents(t)
	ctx := context.Background()

	var bannerCalled bool
	origPrint := writeFirstBootBannerFn
	writeFirstBootBannerFn = func(_ interface{ WriteString(string) (int, error) }, _ adminEnrollmentBanner) {
		bannerCalled = true
	}
	defer func() { writeFirstBootBannerFn = origPrint }()

	// Boot 1: fails at the PIV binding step.
	srv.config.Auth.BootstrapAdminPIVCertPath = "/no/such/file.pem"
	firstErr := srv.firstBootBootstrap(ctx, c)
	require.Error(t, firstErr, "a missing PIV cert must fail the boot")
	var fb *FirstBootBootstrapError
	require.True(t, errors.As(firstErr, &fb))
	require.Equal(t, "bind bootstrap admin PIV cert", fb.Op,
		"the test must fail at the intended step, not an earlier one")

	assignments, err := c.adminManager.ListAssignments(ctx, iam.ListAdminAssignmentsOptions{})
	require.NoError(t, err)
	assert.Empty(t, assignments,
		"a bootstrap that failed before it finished left a super-admin grant behind. "+
			"The next boot reads that as completion and serves with an administrator "+
			"who never received a credential")

	// Boot 2: the operator fixes the cause and restarts.
	srv.config.Auth.BootstrapAdminPIVCertPath = ""
	require.NoError(t, srv.firstBootBootstrap(ctx, c),
		"re-entry must complete rather than fail on the state boot 1 left behind")
	assert.True(t, bannerCalled, "the restart did not resume bootstrap")

	assignments, err = c.adminManager.ListAssignments(ctx, iam.ListAdminAssignmentsOptions{ActiveOnly: true})
	require.NoError(t, err)
	require.Len(t, assignments, 1, "the completed retry must leave exactly one grant")
	assert.Equal(t, iam.AdminRoleSuperAdmin, assignments[0].AdminRole)
}

// TestFirstBootBootstrap_ReEntryWithoutAReissuedCertRefuses covers the state
// where re-entry cannot complete honestly.
//
// qrdb's first-boot is idempotent: once the admin user exists it returns no
// certificate. With a CA configured that means the operator receives no private
// key for a certificate that is already bound to them, so the retry must refuse
// rather than grant the assignment and let the next boot report a healthy server
// with an administrator who cannot log in.
func TestFirstBootBootstrap_ReEntryWithoutAReissuedCertRefuses(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Auth.AllowPasswordAuth = false
	srv.config.Auth.InsecureDevMode = false
	srv.xkmsCA = newFakeTrustedCA(t)
	srv.config.IAM.SPIFFETrustDomain = testTrustDomain
	c := buildTestIAMComponents(t)
	ctx := context.Background()

	var banner adminEnrollmentBanner
	restore := captureFirstBootBanner(t, &banner)
	defer restore()

	// Boot 1 completes, which is what makes the admin user exist.
	require.NoError(t, srv.firstBootBootstrap(ctx, c))
	require.NotEmpty(t, banner.CertPath, "boot 1 must have delivered a bundle")

	// Now destroy the grant, exactly as an operator deleting the assignment (or a
	// boot that failed before writing it) leaves the store. The user survives, so
	// qrdb's first-boot has nothing to reissue.
	assignments, err := c.adminManager.ListAssignments(ctx, iam.ListAdminAssignmentsOptions{})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.NoError(t, c.adminManager.DeleteAssignment(ctx, assignments[0].ID))

	err = srv.firstBootBootstrap(ctx, c)
	require.Error(t, err, "a re-entry that can deliver no credential must refuse")
	assert.ErrorIs(t, err, ErrAdminBundleNotDelivered,
		"the refusal must name the undelivered bundle so the operator knows the remedy")
	assert.ErrorIs(t, err, ErrFirstBootBootstrap)

	after, err := c.adminManager.ListAssignments(ctx, iam.ListAdminAssignmentsOptions{ActiveOnly: true})
	require.NoError(t, err)
	assert.Empty(t, after,
		"the refused re-entry granted an assignment anyway, which would let the "+
			"NEXT boot start a server whose administrator has no private key")
}

// TestFirstBootBootstrap_CALessReEntryStillCompletes is the counterpart that
// keeps the delivery check from over-firing.
//
// A deployment with no CA never had a certificate to deliver; the banner tells
// the operator how to finish enrollment, and that IS its completed state.
// Refusing there would brick every CA-less deployment on its second pass.
func TestFirstBootBootstrap_CALessReEntryStillCompletes(t *testing.T) {
	srv := withTempServer(t)
	require.Nil(t, srv.xkmsCA, "this test is about the CA-less path")
	c := buildTestIAMComponents(t)
	ctx := context.Background()

	var banner adminEnrollmentBanner
	restore := captureFirstBootBanner(t, &banner)
	defer restore()

	require.NoError(t, srv.firstBootBootstrap(ctx, c))
	assert.Empty(t, banner.CertPath, "no CA means no certificate is written")

	assignments, err := c.adminManager.ListAssignments(ctx, iam.ListAdminAssignmentsOptions{})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.NoError(t, c.adminManager.DeleteAssignment(ctx, assignments[0].ID))

	require.NoError(t, srv.firstBootBootstrap(ctx, c),
		"a CA-less re-entry must complete; there was never a bundle to deliver")

	after, err := c.adminManager.ListAssignments(ctx, iam.ListAdminAssignmentsOptions{ActiveOnly: true})
	require.NoError(t, err)
	assert.Len(t, after, 1, "the re-entry must restore the administrator")
}

// TestGrantFirstBootSuperAdmin_SystemScopedOnly pins the SHAPE of the grant the
// rest of this change reads back.
//
// hasSystemAdministrator only accepts a super-admin grant with NO tenant
// scoping, so a first-boot that wrote any other shape would be invisible to the
// next boot's completion check and bootstrap would run forever.
func TestGrantFirstBootSuperAdmin_SystemScopedOnly(t *testing.T) {
	srv := withTempServer(t)
	c := buildTestIAMComponents(t)
	ctx := context.Background()

	adminID := seedActiveUser(t, c, "admin", bootstrapAdminEmail).ID

	require.NoError(t, srv.grantFirstBootSuperAdmin(ctx, c, adminID))

	assignments, err := c.adminManager.ListAssignments(ctx, iam.ListAdminAssignmentsOptions{ActiveOnly: true})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	assert.Equal(t, iam.AdminRoleSuperAdmin, assignments[0].AdminRole)
	assert.Empty(t, assignments[0].TenantIDs, "the first-boot grant is system-scoped (#357)")
	assert.Equal(t, "system:first-boot", assignments[0].GrantedBy)

	// The shape must satisfy the very check the next boot applies to it.
	administered, err := hasSystemAdministrator(ctx, c, assignments)
	require.NoError(t, err)
	assert.True(t, administered,
		"first-boot wrote a grant its own completion check does not recognise, so "+
			"every subsequent boot would re-run bootstrap")
}

// TestGrantFirstBootSuperAdmin_RejectsAnEmptyUser is the invalid-input half. The
// failure must arrive wrapped so the boot refuses with the first-boot class
// rather than an unclassified storage error.
func TestGrantFirstBootSuperAdmin_RejectsAnEmptyUser(t *testing.T) {
	srv := withTempServer(t)
	c := buildTestIAMComponents(t)

	err := srv.grantFirstBootSuperAdmin(context.Background(), c, 0)
	require.Error(t, err, "a grant naming no user must not be written")
	assert.ErrorIs(t, err, ErrFirstBootBootstrap)
	var fb *FirstBootBootstrapError
	require.True(t, errors.As(err, &fb))
	assert.Equal(t, "create admin assignment", fb.Op)
	assert.True(t, strings.Contains(err.Error(), "user_id"),
		"the message must name the field that was missing: %v", err)
}

// =============================================================================
// The second boot of a real node
// =============================================================================

// TestInitializeIAMComponents_SecondBootDoesNotReBootstrap runs the whole IAM
// initialization TWICE against the embedded qrdb node — the store production
// actually uses — and requires the second pass to skip bootstrap.
//
// This is the counterweight to every refusal above, and it needs the real store
// rather than the in-memory one: the grant first-boot writes names the user by
// qrdb's Snowflake id, and hasSystemAdministrator resolves that id back to the
// user record. If that round trip did not hold, an administered node would
// re-enter first-boot on every restart — and with a CA configured it would then
// refuse to start at all, bricking a healthy deployment on a change that was
// supposed to protect it.
func TestInitializeIAMComponents_SecondBootDoesNotReBootstrap(t *testing.T) {
	srv := newFreshInstallNode(t)
	banners := countFirstBootBanners(t)

	first, err := srv.initializeIAMComponents()
	require.NoError(t, err, "the first boot must bootstrap an administrator")
	require.NotNil(t, first)
	require.Equal(t, 1, *banners, "first boot must announce the enrollment")

	ctx := context.Background()
	assignments, err := first.GetAdminManager().ListAssignments(ctx, iam.ListAdminAssignmentsOptions{
		ActiveOnly: true,
		AdminRole:  iam.AdminRoleSuperAdmin,
	})
	require.NoError(t, err)
	require.Len(t, assignments, 1, "first boot must leave exactly one system grant")

	// The grant must be recognisable as an administrator through the REAL store,
	// which is the round trip this test exists for.
	administered, err := hasSystemAdministrator(ctx, first.(*iamComponents), assignments)
	require.NoError(t, err)
	require.True(t, administered,
		"the grant first-boot wrote is not readable back as an administrator, so "+
			"every restart would re-run first-boot")

	second, err := srv.initializeIAMComponents()
	require.NoError(t, err, "the second boot of an administered node must not be refused")
	require.NotNil(t, second)
	assert.Equal(t, 1, *banners,
		"the second boot re-ran first-boot; on a CA-backed deployment that path "+
			"ends in a refused boot for a node that has a perfectly good administrator")

	assignments, err = second.GetAdminManager().ListAssignments(ctx, iam.ListAdminAssignmentsOptions{
		ActiveOnly: true,
		AdminRole:  iam.AdminRoleSuperAdmin,
	})
	require.NoError(t, err)
	assert.Len(t, assignments, 1, "no second grant may be created on the second boot")
}

// TestReportUnusableAdministrators_NamesTheOrphanedGrant is the report an
// operator gets when a running node re-enters first-boot: it must name the
// grant and the user it points at, or the refusal that follows is a dead end.
func TestReportUnusableAdministrators_NamesTheOrphanedGrant(t *testing.T) {
	c := buildTestIAMComponents(t)
	log := &warnCapture{}

	reportUnusableAdministrators(context.Background(), c, []*iam.AdminAssignment{
		{ID: "adm_orphan", UserID: "9fe07c04-gone", AdminRole: iam.AdminRoleSuperAdmin},
	}, log)

	got := log.String()
	assert.Contains(t, got, "adm_orphan")
	assert.Contains(t, got, "9fe07c04-gone")
	assert.Contains(t, got, "re-entering first-boot")
}

// TestReportUnusableAdministrators_IgnoresTenantScopedGrants keeps the report
// to the grants the decision was made on: a tenant admin never administers the
// system, so naming one would send the operator after the wrong account.
func TestReportUnusableAdministrators_IgnoresTenantScopedGrants(t *testing.T) {
	c := buildTestIAMComponents(t)
	log := &warnCapture{}

	reportUnusableAdministrators(context.Background(), c, []*iam.AdminAssignment{
		{ID: "adm_tenant", UserID: "someone", AdminRole: iam.AdminRoleSuperAdmin, TenantIDs: []string{"acme"}},
	}, log)

	assert.NotContains(t, log.String(), "adm_tenant")
}
