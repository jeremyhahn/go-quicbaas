//go:build rest

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/qrdbadapter"
)

// ceremonyRefusedError is the double's stand-in for go-xkms's
// ErrRegistrationRefused, which the xkms SDK does not re-export.
type ceremonyRefusedError struct{ reason string }

func (e *ceremonyRefusedError) Error() string { return "webauthn: registration refused: " + e.reason }

// xkmsCeremony is a WebAuthn manager that keeps go-xkms's registration
// contract (pkg/webauthn Service at the pinned version) against the same user
// storage the broker writes, so the ORDER of the broker's and the ceremony's
// writes is exercised rather than assumed:
//
//   - BeginRegistration refuses an email that already has an account, and for
//     one that has none CREATES the account when public registration is
//     allowed — with the email as its username, written straight to storage
//     the way go-xkms writes straight to qrdb, past validateUsername.
//   - BeginInviteRegistration enrols only an existing, pending account that
//     holds no credential; it never creates one.
//   - FinishRegistration refuses a pending account; FinishInviteRegistration
//     binds the credential to a pending account and leaves it pending.
type xkmsCeremony struct {
	store       *iam.IdentityStore
	raw         *iam.MemoryStorage
	allowPublic bool
	failFinish  bool

	// gate, when set, is the real attestation gate the ceremony runs at
	// finish against an authenticator of attestationType, as go-xkms's
	// credential write-through does.
	gate            *iam.WebAuthnAttestationGate
	attestationType string

	mu      sync.Mutex
	invited map[string]bool // tenant|email -> ceremony opened by the invite path
	bound   map[string]bool // tenant|email -> credential bound
}

func newXKMSCeremony(svc *selfRegService, allowPublic bool) *xkmsCeremony {
	c := &xkmsCeremony{
		store:       svc.store,
		raw:         svc.tenants.storage,
		allowPublic: allowPublic,
		invited:     map[string]bool{},
		bound:       map[string]bool{},
	}
	svc.mgr = c
	return c
}

func (c *xkmsCeremony) BeginRegistration(ctx context.Context, tenantID, email string) (map[string]interface{}, error) {
	if _, err := c.store.GetUserByEmail(ctx, tenantID, email); err == nil {
		return nil, &ceremonyRefusedError{reason: "account exists"}
	}
	if !c.allowPublic {
		return nil, &ceremonyRefusedError{reason: "public registration disabled"}
	}
	if err := c.raw.CreateUser(ctx, &iam.User{
		TenantID:  tenantID,
		Email:     email,
		Username:  email,
		Lifecycle: iam.LifecycleActive,
	}); err != nil {
		return nil, err
	}
	return map[string]interface{}{"challenge": "public"}, nil
}

func (c *xkmsCeremony) BeginInviteRegistration(ctx context.Context, tenantID, email string) (map[string]interface{}, error) {
	user, err := c.store.GetUserByEmail(ctx, tenantID, email)
	if err != nil {
		return nil, &ceremonyRefusedError{reason: "no account"}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if user.Lifecycle != iam.LifecyclePending || c.bound[tenantID+"|"+email] {
		return nil, &ceremonyRefusedError{reason: "account not pending"}
	}
	c.invited[tenantID+"|"+email] = true
	return map[string]interface{}{"challenge": "invite"}, nil
}

func (c *xkmsCeremony) FinishRegistration(ctx context.Context, tenantID, email string, _ map[string]interface{}) (*iam.AttestationResult, error) {
	user, err := c.store.GetUserByEmail(ctx, tenantID, email)
	if err != nil || user.Lifecycle == iam.LifecyclePending {
		return nil, &ceremonyRefusedError{reason: "no active account"}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bound[tenantID+"|"+email] = true
	return nil, nil
}

func (c *xkmsCeremony) FinishInviteRegistration(ctx context.Context, tenantID, email string, _ map[string]interface{}) (*iam.AttestationResult, error) {
	if c.failFinish {
		return nil, &ceremonyRefusedError{reason: "attestation rejected"}
	}
	if c.gate != nil {
		if _, err := c.gate.Evaluate(ctx, tenantID, email, c.attestationType, nil); err != nil {
			return nil, err
		}
	}
	user, err := c.store.GetUserByEmail(ctx, tenantID, email)
	if err != nil || user.Lifecycle != iam.LifecyclePending {
		return nil, &ceremonyRefusedError{reason: "account not pending"}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.invited[tenantID+"|"+email] {
		return nil, &ceremonyRefusedError{reason: "no ceremony"}
	}
	c.bound[tenantID+"|"+email] = true
	return nil, nil
}

// postSelfRegBegin posts a raw begin body and returns the response.
func postSelfRegBegin(handler *IAMHandler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register/begin", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.RegisterSelfBegin(w, req)
	return w
}

func (c *xkmsCeremony) BeginLogin(context.Context, string, string) (map[string]interface{}, error) {
	return nil, &ceremonyRefusedError{reason: "login is not under test"}
}

func (c *xkmsCeremony) FinishLogin(context.Context, string, map[string]interface{}) (*iam.User, error) {
	return nil, &ceremonyRefusedError{reason: "login is not under test"}
}

func brokerSelfRegConfig() config.IAMSelfServiceRegistrationConfig {
	return config.IAMSelfServiceRegistrationConfig{
		Enabled:               true,
		AllowedIsolationModes: []string{config.TenantIsolationKeyspace},
		DefaultIsolationMode:  config.TenantIsolationKeyspace,
	}
}

// tenantIDs lists the tenants the handler's tenant manager holds.
func tenantIDs(t *testing.T, handler *IAMHandler) []string {
	t.Helper()
	tenants, err := handler.getTenantManager(context.Background())
	require.NoError(t, err)
	list, err := tenants.ListTenants(context.Background(), iam.ListOptions{})
	require.NoError(t, err)
	ids := make([]string, 0, len(list))
	for _, tenant := range list {
		ids = append(ids, tenant.ID)
	}
	return ids
}

// usersIn lists the user rows the tenant holds.
func usersIn(t *testing.T, handler *IAMHandler, tenantID string) []*iam.User {
	t.Helper()
	store, err := handler.getIdentityStore(context.Background())
	require.NoError(t, err)
	users, err := store.ListUsers(context.Background(), tenantID, iam.ListOptions{})
	require.NoError(t, err)
	return users
}

// TestRegisterSelf_BrokerCreatesTheOnlyAccount is the #482 sequence: begin
// then finish must leave exactly one account, the broker's, with a legal
// handle, in exactly one new tenant, and the finish must succeed. The double
// allows public registration, as quicbaas configured go-xkms whenever
// self-service registration was on, so a ceremony asked to create the account
// does — and the broker's own create then collides on the email.
func TestRegisterSelf_BrokerCreatesTheOnlyAccount(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	newXKMSCeremony(handler.server.service.(*selfRegService), true)

	sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")
	w := selfRegFinish(t, handler, sessionID)
	require.Equal(t, http.StatusOK, w.Code, "finish body: %s", w.Body.String())

	assert.Equal(t, []string{"acme-inc"}, tenantIDs(t, handler),
		"one registration creates exactly one tenant")
	users := usersIn(t, handler, "acme-inc")
	require.Len(t, users, 1, "one registration creates exactly one account")
	assert.Equal(t, "jane@acme.example", users[0].Email)
	assert.Equal(t, deriveHandle("jane@acme.example"), users[0].Username,
		"the account carries the broker's derived handle, not the email address")
	assert.Equal(t, iam.LifecycleActive, users[0].Lifecycle)
}

// TestRegisterSelf_FailedFinishLeavesNothing: a ceremony the registrant does
// not complete — the authenticator's response is refused — leaves no tenant
// and no account behind, and the session cannot be replayed.
func TestRegisterSelf_FailedFinishLeavesNothing(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	ceremony := newXKMSCeremony(handler.server.service.(*selfRegService), true)
	ceremony.failFinish = true

	sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")
	w := selfRegFinish(t, handler, sessionID)
	require.NotEqual(t, http.StatusOK, w.Code, "a refused ceremony must not register")

	assert.Empty(t, tenantIDs(t, handler), "a failed finish leaves no orphan tenant")
	assert.Empty(t, usersIn(t, handler, "acme-inc"), "a failed finish leaves no user row")

	ceremony.failFinish = false
	w = selfRegFinish(t, handler, sessionID)
	assert.Equal(t, http.StatusBadRequest, w.Code, "a failed session is spent: %s", w.Body.String())
}

// TestRegisterSelf_AbandonedBeginLeavesNothing: a begin whose session expires
// without a finish leaves no tenant and no account behind.
func TestRegisterSelf_AbandonedBeginLeavesNothing(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	newXKMSCeremony(handler.server.service.(*selfRegService), true)

	sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")
	raw, ok := globalRegSessionStore.sessions.Load(sessionID)
	require.True(t, ok)
	raw.(*registrationSession).expiresAt = time.Now().Add(-time.Second)

	w := selfRegFinish(t, handler, sessionID)
	require.Equal(t, http.StatusBadRequest, w.Code, "finish body: %s", w.Body.String())

	assert.Empty(t, tenantIDs(t, handler), "an abandoned begin leaves no orphan tenant")
	assert.Empty(t, usersIn(t, handler, "acme-inc"), "an abandoned begin leaves no user row")
}

// TestRegisterSelf_ReaperDiscardsAnExpiredBegin: the expiry timer's work —
// run here directly rather than waited for — removes the tenant and account
// of a begin nobody finished, and spends the session.
func TestRegisterSelf_ReaperDiscardsAnExpiredBegin(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	newXKMSCeremony(handler.server.service.(*selfRegService), false)

	sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")
	require.Equal(t, []string{"acme-inc"}, tenantIDs(t, handler))
	require.Len(t, usersIn(t, handler, "acme-inc"), 1)

	handler.reapRegistration(context.Background(), sessionID)

	assert.Empty(t, tenantIDs(t, handler), "the reaper removes the tenant")
	assert.Empty(t, usersIn(t, handler, "acme-inc"), "the reaper removes the account")
	w := selfRegFinish(t, handler, sessionID)
	assert.Equal(t, http.StatusBadRequest, w.Code, "a reaped session is spent: %s", w.Body.String())
}

// TestRegisterSelf_ReaperLeavesAFinishedRegistration: a reaper that fires
// after the finish took the session does nothing.
func TestRegisterSelf_ReaperLeavesAFinishedRegistration(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	newXKMSCeremony(handler.server.service.(*selfRegService), false)

	sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")
	w := selfRegFinish(t, handler, sessionID)
	require.Equal(t, http.StatusOK, w.Code, "finish body: %s", w.Body.String())

	handler.reapRegistration(context.Background(), sessionID)

	assert.Equal(t, []string{"acme-inc"}, tenantIDs(t, handler))
	assert.Len(t, usersIn(t, handler, "acme-inc"), 1)
}

// TestRegisterSelf_RefusedAccountLeavesNoTenant: when the broker refuses the
// account at begin, no tenant is left behind and no ceremony is opened.
func TestRegisterSelf_RefusedAccountLeavesNoTenant(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	newXKMSCeremony(handler.server.service.(*selfRegService), true)

	body := `{"tenant_name":"Acme Inc","email":"not-an-address","display_name":"Jane"}`
	w := postSelfRegBegin(handler, body)
	require.NotEqual(t, http.StatusOK, w.Code, "begin body: %s", w.Body.String())

	assert.Empty(t, tenantIDs(t, handler), "a refused account leaves no tenant")
	assert.Empty(t, usersIn(t, handler, "acme-inc"), "a refused account leaves no user row")
}

// TestRegisterSelf_CeremonyNeverCreatesTheAccount: with public registration
// off, as quicbaas now configures go-xkms, the registration still completes,
// because the ceremony enrols the account the broker created.
func TestRegisterSelf_CeremonyNeverCreatesTheAccount(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	newXKMSCeremony(handler.server.service.(*selfRegService), false)

	sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")
	w := selfRegFinish(t, handler, sessionID)
	require.Equal(t, http.StatusOK, w.Code, "finish body: %s", w.Body.String())
	require.Len(t, usersIn(t, handler, "acme-inc"), 1)
}

// adminResolver is the resolver go-xkms's credential write-through asks
// whether an account is administrative, over the broker's own store.
func adminResolver(t *testing.T, handler *IAMHandler) iam.WebAuthnAdminResolver {
	t.Helper()
	store, err := handler.getIdentityStore(context.Background())
	require.NoError(t, err)
	return iam.NewWebAuthnAdminResolver(store, qrdbadapter.WrapRole(store))
}

func holdsAdmin(t *testing.T, handler *IAMHandler, tenantID, email string) bool {
	t.Helper()
	isAdmin, err := adminResolver(t, handler).IsAdministrativeAccount(context.Background(), tenantID, email)
	require.NoError(t, err)
	return isAdmin
}

// The registrant is made Admin of the new tenant, so the ceremony must hold
// them to the administrator hardware bar even where the tenant accepts any
// authenticator. The gate asks the account's roles; they were granted after
// the ceremony, so a software authenticator enrolled and then became Admin.
func TestRegisterSelf_SoftwareAuthenticatorRefusedForAdmin(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	ceremony := newXKMSCeremony(handler.server.service.(*selfRegService), true)
	ceremony.gate = iam.NewWebAuthnAttestationGate(iam.WebAuthnAssuranceAny, nil, adminResolver(t, handler))
	ceremony.attestationType = "none"

	sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")
	require.True(t, holdsAdmin(t, handler, "acme-inc", "jane@acme.example"),
		"the account holds Admin before the ceremony opens")

	w := selfRegFinish(t, handler, sessionID)
	require.NotEqual(t, http.StatusOK, w.Code, "a software authenticator must not enrol an administrator: %s", w.Body.String())
	assert.Empty(t, tenantIDs(t, handler), "a refused administrator leaves no tenant")
	assert.Empty(t, usersIn(t, handler, "acme-inc"), "a refused administrator leaves no account")
}

// A hardware authenticator passes the gate (the double stands in for a
// vouched attestation chain by not refusing) and the registrant holds Admin.
func TestRegisterSelf_HardwareAuthenticatorCompletesAsAdmin(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	newXKMSCeremony(handler.server.service.(*selfRegService), true)

	sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")
	w := selfRegFinish(t, handler, sessionID)
	require.Equal(t, http.StatusOK, w.Code, "finish body: %s", w.Body.String())

	assert.True(t, holdsAdmin(t, handler, "acme-inc", "jane@acme.example"))
	users := usersIn(t, handler, "acme-inc")
	require.Len(t, users, 1)
	assert.Equal(t, iam.LifecycleActive, users[0].Lifecycle)
}

// afterExpiry is a sweep time past the expiry of a begin made now.
func afterExpiry() time.Time { return time.Now().Add(selfRegistrationSessionTTL + time.Minute) }

// fakeTimer is a timer the test fires instead of waiting for.
type fakeTimer struct {
	wait    time.Duration
	fire    func()
	stopped atomic.Bool
}

// Stop reports whether this call stopped the timer, as time.Timer does.
func (t *fakeTimer) Stop() bool { return !t.stopped.Swap(true) }

// fakeTimers records the timers a sweeper arms.
type fakeTimers struct {
	mu    sync.Mutex
	armed []*fakeTimer
}

func (f *fakeTimers) afterFunc(wait time.Duration, fire func()) registrationTimer {
	f.mu.Lock()
	defer f.mu.Unlock()
	timer := &fakeTimer{wait: wait, fire: fire}
	f.armed = append(f.armed, timer)
	return timer
}

func (f *fakeTimers) all() []*fakeTimer {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*fakeTimer(nil), f.armed...)
}

// testClock is the sweeper's clock in a test; the test moves it.
type testClock struct{ at time.Time }

func (c *testClock) now() time.Time { return c.at }

// newTestSweeper builds the sweeper a restarted server builds, over the
// handler's stores, on a clock and timers the test drives.
func newTestSweeper(handler *IAMHandler, clock *testClock) (*registrationSweeper, *fakeTimers) {
	svc := handler.server.service.(*selfRegService)
	sweeper := newRegistrationSweeper(svc.tenants, svc.store, handler.server.logger)
	timers := &fakeTimers{}
	sweeper.now = clock.now
	sweeper.afterFunc = timers.afterFunc
	return sweeper, timers
}

// restart drops the begun session and its reaper, as a process restart does;
// only the broker's record on the tenant survives it.
func restart(t *testing.T, sessionID string) {
	t.Helper()
	_, ok := globalRegSessionStore.take(sessionID)
	require.True(t, ok, "the begun session is held until the restart")
}

func TestRegistrationSweeper_DiscardsAnExpiredRegistration(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	newXKMSCeremony(handler.server.service.(*selfRegService), true)
	restart(t, selfRegBegin(t, handler, "Acme Inc", "jane@acme.example"))
	sweeper, timers := newTestSweeper(handler, &testClock{at: afterExpiry()})

	require.NoError(t, sweeper.sweep(context.Background()))

	assert.Empty(t, tenantIDs(t, handler), "the sweep removes the tenant")
	assert.Empty(t, usersIn(t, handler, "acme-inc"), "the sweep removes the account")
	assert.Empty(t, timers.all(), "an expired registration needs no timer")
}

// A restart inside a registration's expiry: the sweep keeps it, and the timer
// it arms discards it once it expires, with no second restart.
func TestRegistrationSweeper_DiscardsARegistrationPendingAcrossARestart(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	newXKMSCeremony(handler.server.service.(*selfRegService), true)
	restart(t, selfRegBegin(t, handler, "Acme Inc", "jane@acme.example"))
	clock := &testClock{at: time.Now()}
	sweeper, timers := newTestSweeper(handler, clock)

	require.NoError(t, sweeper.sweep(context.Background()))

	assert.Equal(t, []string{"acme-inc"}, tenantIDs(t, handler), "an unexpired registration is kept")
	armed := timers.all()
	require.Len(t, armed, 1, "the sweep arms a timer for the unexpired registration")
	assert.Positive(t, armed[0].wait)
	assert.LessOrEqual(t, armed[0].wait, selfRegistrationSessionTTL, "the timer waits out the remaining expiry")

	clock.at = afterExpiry()
	armed[0].fire()

	assert.Empty(t, tenantIDs(t, handler), "the timer removes the tenant at expiry")
	assert.Empty(t, usersIn(t, handler, "acme-inc"), "the timer removes the account at expiry")
	assert.Len(t, timers.all(), 1, "a discarded registration arms no further timer")
}

// A timer that fires before the expiry it waited for re-arms for the rest.
func TestRegistrationSweeper_RearmsATimerThatFiresEarly(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	newXKMSCeremony(handler.server.service.(*selfRegService), true)
	restart(t, selfRegBegin(t, handler, "Acme Inc", "jane@acme.example"))
	sweeper, timers := newTestSweeper(handler, &testClock{at: time.Now()})
	require.NoError(t, sweeper.sweep(context.Background()))
	require.Len(t, timers.all(), 1)

	timers.all()[0].fire()

	assert.Equal(t, []string{"acme-inc"}, tenantIDs(t, handler), "an unexpired registration is kept")
	assert.Len(t, timers.all(), 2, "the early timer re-arms")
}

// A registration this process began is its session's reaper's: the sweep
// neither discards it nor arms a timer for it, and it still finishes.
func TestRegistrationSweeper_LeavesASessionOfThisProcess(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	newXKMSCeremony(handler.server.service.(*selfRegService), true)
	sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")
	sweeper, timers := newTestSweeper(handler, &testClock{at: afterExpiry()})

	require.NoError(t, sweeper.sweep(context.Background()))

	assert.Equal(t, []string{"acme-inc"}, tenantIDs(t, handler))
	assert.Empty(t, timers.all())
	w := selfRegFinish(t, handler, sessionID)
	assert.Equal(t, http.StatusOK, w.Code, "the session still finishes: %s", w.Body.String())
}

// An account held for admin approval finished its ceremony, and the finish
// cleared the tenant's record: it is enrolled, not abandoned, however long the
// approval takes.
func TestRegistrationSweeper_KeepsAnEnrolledPendingAccount(t *testing.T) {
	cfg := brokerSelfRegConfig()
	cfg.RequireAdminApproval = true
	handler := setupSelfRegHandler(t, cfg)
	svc := handler.server.service.(*selfRegService)
	newXKMSCeremony(svc, true)
	sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")
	w := selfRegFinish(t, handler, sessionID)
	require.Equal(t, http.StatusOK, w.Code, "finish body: %s", w.Body.String())
	sweeper, timers := newTestSweeper(handler, &testClock{at: afterExpiry()})

	require.NoError(t, sweeper.sweep(context.Background()))

	users := usersIn(t, handler, "acme-inc")
	require.Len(t, users, 1, "an enrolled account awaiting approval is kept")
	assert.Equal(t, iam.LifecyclePending, users[0].Lifecycle)
	tenant, err := svc.tenants.GetTenant(context.Background(), "acme-inc")
	require.NoError(t, err)
	assert.Nil(t, tenant.SelfRegistration, "the finish clears the tenant's registration record")
	assert.Empty(t, timers.all())
}

// The attributes the former sweep trusted can be written by any caller who may
// update a user. Setting them, at create and through the update path, does not
// make a tenant the sweep's to delete.
func TestRegistrationSweeper_IgnoresRegistrationAttributesOnAnAccount(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	svc := handler.server.service.(*selfRegService)
	ctx := context.Background()
	forged := map[string]string{
		"self_registered":      "true",
		"registration_state":   "ceremony",
		"registration_expires": time.Now().Add(-time.Hour).Format(time.RFC3339),
	}
	_, err := svc.tenants.CreateTenant(ctx, iam.CreateTenantOptions{ID: "victim-co", Name: "Victim Co"})
	require.NoError(t, err)
	_, err = svc.store.CreateUser(ctx, "victim-co", iam.CreateUserOptions{
		Email: "bob@victim.example", Username: "bob", Name: "Bob",
		Lifecycle: iam.LifecyclePending, Attributes: forged,
	})
	require.NoError(t, err)
	carol, err := svc.store.CreateUser(ctx, "victim-co", iam.CreateUserOptions{
		Email: "carol@victim.example", Username: "carol", Name: "Carol",
	})
	require.NoError(t, err)
	_, err = svc.store.UpdateUser(ctx, "victim-co", carol.ID, iam.UpdateUserOptions{Attributes: &forged})
	require.NoError(t, err)
	sweeper, timers := newTestSweeper(handler, &testClock{at: afterExpiry()})

	require.NoError(t, sweeper.sweep(ctx))

	assert.Equal(t, []string{"victim-co"}, tenantIDs(t, handler), "account attributes do not condemn a tenant")
	assert.Len(t, usersIn(t, handler, "victim-co"), 2)
	assert.Empty(t, timers.all())
}

// A registration's tenant that holds more than the registrant's pending
// account is reported and kept.
func TestRegistrationSweeper_KeepsATenantInUse(t *testing.T) {
	tests := []struct {
		name  string
		after func(t *testing.T, svc *selfRegService, registrant *iam.User)
		users int
	}{
		{
			name: "a second account",
			after: func(t *testing.T, svc *selfRegService, _ *iam.User) {
				_, err := svc.store.CreateUser(context.Background(), "acme-inc", iam.CreateUserOptions{
					Email: "bob@acme.example", Username: "bob", Name: "Bob",
				})
				require.NoError(t, err)
			},
			users: 2,
		},
		{
			name: "the registrant activated",
			after: func(t *testing.T, svc *selfRegService, registrant *iam.User) {
				active := iam.LifecycleActive
				_, err := svc.store.UpdateUser(context.Background(), "acme-inc", registrant.ID,
					iam.UpdateUserOptions{Lifecycle: &active})
				require.NoError(t, err)
			},
			users: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := setupSelfRegHandler(t, brokerSelfRegConfig())
			svc := handler.server.service.(*selfRegService)
			newXKMSCeremony(svc, true)
			restart(t, selfRegBegin(t, handler, "Acme Inc", "jane@acme.example"))
			users := usersIn(t, handler, "acme-inc")
			require.Len(t, users, 1)
			tt.after(t, svc, users[0])
			sweeper, _ := newTestSweeper(handler, &testClock{at: afterExpiry()})

			err := sweeper.sweep(context.Background())

			var inUse *SelfRegistrationTenantInUseError
			require.ErrorAs(t, err, &inUse)
			assert.Equal(t, "acme-inc", inUse.TenantID)
			assert.Equal(t, []string{"acme-inc"}, tenantIDs(t, handler))
			assert.Len(t, usersIn(t, handler, "acme-inc"), tt.users)
		})
	}
}

// stop cancels the armed timers; one that fires as stop runs does nothing,
// and nothing is armed after.
func TestRegistrationSweeper_StopCancelsArmedTimers(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	newXKMSCeremony(handler.server.service.(*selfRegService), true)
	restart(t, selfRegBegin(t, handler, "Acme Inc", "jane@acme.example"))
	clock := &testClock{at: time.Now()}
	sweeper, timers := newTestSweeper(handler, clock)
	require.NoError(t, sweeper.sweep(context.Background()))
	require.Len(t, timers.all(), 1)

	sweeper.stop()

	armed := timers.all()[0]
	assert.True(t, armed.stopped.Load(), "stop cancels the armed timer")
	clock.at = afterExpiry()
	armed.fire()
	assert.Equal(t, []string{"acme-inc"}, tenantIDs(t, handler), "a timer firing after stop discards nothing")
	sweeper.arm(context.Background(), "acme-inc", time.Minute)
	assert.Len(t, timers.all(), 1, "nothing is armed after stop")
}

// A tenant whose users cannot be listed is reported, and the sweep still
// discards the expired registrations of the other tenants.
func TestRegistrationSweeper_ReturnsAUserListFailure(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	svc := handler.server.service.(*selfRegService)
	newXKMSCeremony(svc, true)
	restart(t, selfRegBegin(t, handler, "Acme Inc", "jane@acme.example"))
	ctx := context.Background()
	// A tenant record with no id: the store refuses to list its users.
	require.NoError(t, svc.tenants.storage.CreateTenant(ctx, &iam.Tenant{
		Name:             "Broken",
		SelfRegistration: &iam.TenantSelfRegistration{Expires: time.Now()},
	}))
	sweeper, _ := newTestSweeper(handler, &testClock{at: afterExpiry()})

	err := sweeper.sweep(ctx)

	require.Error(t, err, "a tenant whose users cannot be listed is reported")
	assert.Empty(t, usersIn(t, handler, "acme-inc"), "the other tenant's expired registration is still discarded")
}

// unavailableTenants is a tenant manager whose tenants cannot be read.
type unavailableTenants struct{ *testTenantManager }

func (unavailableTenants) ListTenants(context.Context, iam.ListOptions) ([]*iam.Tenant, error) {
	return nil, &ceremonyRefusedError{reason: "tenant list unavailable"}
}

func (unavailableTenants) GetTenant(context.Context, string) (*iam.Tenant, error) {
	return nil, &ceremonyRefusedError{reason: "tenant unavailable"}
}

func TestRegistrationSweeper_ReturnsATenantReadFailure(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	svc := handler.server.service.(*selfRegService)
	sweeper, _ := newTestSweeper(handler, &testClock{at: afterExpiry()})
	sweeper.tenants = unavailableTenants{svc.tenants}

	var readErr *ceremonyRefusedError
	assert.ErrorAs(t, sweeper.sweep(context.Background()), &readErr, "the tenant list failure is returned")
	assert.ErrorAs(t, sweeper.expire(context.Background(), "acme-inc"), &readErr, "the tenant read failure is returned")
}

// A timer whose tenant is gone, or finished its registration, does nothing.
func TestRegistrationSweeper_ExpireLeavesASettledTenant(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	svc := handler.server.service.(*selfRegService)
	ctx := context.Background()
	_, err := svc.tenants.CreateTenant(ctx, iam.CreateTenantOptions{ID: "plain-co", Name: "Plain Co"})
	require.NoError(t, err)
	sweeper, timers := newTestSweeper(handler, &testClock{at: afterExpiry()})

	require.NoError(t, sweeper.expire(ctx, "gone-co"), "a deleted tenant is not an error")
	require.NoError(t, sweeper.expire(ctx, "plain-co"))

	assert.Equal(t, []string{"plain-co"}, tenantIDs(t, handler))
	assert.Empty(t, timers.all())
}

// A server without the stores provisioned no registration to sweep.
func TestRegistrationSweeper_WithoutStoresSweepsNothing(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	sweeper := newRegistrationSweeper(nil, nil, handler.server.logger)

	assert.NoError(t, sweeper.sweep(context.Background()))
}

// Arming a tenant again replaces its timer: the first is stopped, so one
// registration never holds two timers.
func TestRegistrationSweeper_ArmReplacesATenantsTimer(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	sweeper, timers := newTestSweeper(handler, &testClock{at: time.Now()})

	sweeper.arm(context.Background(), "acme-inc", time.Minute)
	sweeper.arm(context.Background(), "acme-inc", time.Second)

	armed := timers.all()
	require.Len(t, armed, 2)
	assert.True(t, armed[0].stopped.Load(), "the replaced timer is stopped")
	assert.False(t, armed[1].stopped.Load(), "the new timer stays armed")
}

// A timer whose discard fails logs the failure and keeps the tenant; the
// failure has no caller to return to.
func TestRegistrationSweeper_TimerKeepsATenantItCannotRead(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	svc := handler.server.service.(*selfRegService)
	newXKMSCeremony(svc, true)
	restart(t, selfRegBegin(t, handler, "Acme Inc", "jane@acme.example"))
	sweeper, timers := newTestSweeper(handler, &testClock{at: time.Now()})
	require.NoError(t, sweeper.sweep(context.Background()))
	require.Len(t, timers.all(), 1)

	sweeper.tenants = unavailableTenants{svc.tenants}
	timers.all()[0].fire()

	assert.Equal(t, []string{"acme-inc"}, tenantIDs(t, handler))
	assert.Len(t, timers.all(), 1, "a failed read arms no timer")
}

// The production sweeper arms real timers, which fire their callback.
func TestRegistrationSweeper_DefaultTimerFires(t *testing.T) {
	sweeper := newRegistrationSweeper(nil, nil, nil)
	fired := make(chan struct{})

	sweeper.afterFunc(0, func() { close(fired) })

	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		t.Fatal("the timer never fired")
	}
}

func TestSelfRegistrationTenantInUseError_NamesTheTenant(t *testing.T) {
	err := &SelfRegistrationTenantInUseError{TenantID: "acme-inc"}

	assert.Contains(t, err.Error(), "acme-inc")
}

// TestRegisterSelf_ReaperKeepsATenantInUse pins the session reaper to the
// sweep's in-use check: a tenant with a second account, or whose registrant
// is no longer pending, is kept.
func TestRegisterSelf_ReaperKeepsATenantInUse(t *testing.T) {
	tests := []struct {
		name  string
		after func(t *testing.T, svc *selfRegService, registrant *iam.User)
		users int
	}{
		{
			name: "a second account",
			after: func(t *testing.T, svc *selfRegService, _ *iam.User) {
				_, err := svc.store.CreateUser(context.Background(), "acme-inc", iam.CreateUserOptions{
					Email: "bob@acme.example", Username: "bob", Name: "Bob",
				})
				require.NoError(t, err)
			},
			users: 2,
		},
		{
			name: "the registrant activated",
			after: func(t *testing.T, svc *selfRegService, registrant *iam.User) {
				active := iam.LifecycleActive
				_, err := svc.store.UpdateUser(context.Background(), "acme-inc", registrant.ID,
					iam.UpdateUserOptions{Lifecycle: &active})
				require.NoError(t, err)
			},
			users: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := setupSelfRegHandler(t, brokerSelfRegConfig())
			svc := handler.server.service.(*selfRegService)
			newXKMSCeremony(svc, true)
			sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")
			users := usersIn(t, handler, "acme-inc")
			require.Len(t, users, 1)
			tt.after(t, svc, users[0])

			handler.reapRegistration(context.Background(), sessionID)

			assert.Equal(t, []string{"acme-inc"}, tenantIDs(t, handler))
			assert.Len(t, usersIn(t, handler, "acme-inc"), tt.users)
		})
	}
}

// TestRegisterSelf_ReaperKeepsATenantWithoutARegistrationRecord pins the
// ownership check: a tenant whose registration record is gone is not the
// registration's own, so the reaper keeps it.
func TestRegisterSelf_ReaperKeepsATenantWithoutARegistrationRecord(t *testing.T) {
	ctx := context.Background()
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	svc := handler.server.service.(*selfRegService)
	newXKMSCeremony(svc, true)
	sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")
	_, err := svc.tenants.UpdateTenant(ctx, "acme-inc", iam.UpdateTenantOptions{FinishSelfRegistration: true})
	require.NoError(t, err)

	handler.reapRegistration(ctx, sessionID)

	assert.Equal(t, []string{"acme-inc"}, tenantIDs(t, handler))
	assert.Len(t, usersIn(t, handler, "acme-inc"), 1)
}

// TestRegisterSelfFinish_ClearsTheRecordAfterActivation pins the order of the
// finish's writes: the ungated finish activates the account before it clears
// the registration record; a gated finish clears it with the account pending.
func TestRegisterSelfFinish_ClearsTheRecordAfterActivation(t *testing.T) {
	tests := []struct {
		name      string
		gated     bool
		lifecycle iam.Lifecycle
	}{
		{name: "ungated", lifecycle: iam.LifecycleActive},
		{name: "admin approval", gated: true, lifecycle: iam.LifecyclePending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := brokerSelfRegConfig()
			cfg.RequireAdminApproval = tt.gated
			handler := setupSelfRegHandler(t, cfg)
			svc := handler.server.service.(*selfRegService)
			newXKMSCeremony(svc, true)
			sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")
			var recorded []iam.Lifecycle
			svc.tenants.onUpdate = func(_ string, opts iam.UpdateTenantOptions) {
				if opts.FinishSelfRegistration {
					recorded = append(recorded, usersIn(t, handler, "acme-inc")[0].Lifecycle)
				}
			}

			w := selfRegFinish(t, handler, sessionID)

			require.Equal(t, http.StatusOK, w.Code, "finish body: %s", w.Body.String())
			assert.Equal(t, []iam.Lifecycle{tt.lifecycle}, recorded)
			tenant, err := svc.tenants.GetTenant(context.Background(), "acme-inc")
			require.NoError(t, err)
			assert.Nil(t, tenant.SelfRegistration)
		})
	}
}

// TestRegistrationSweeper_KeepsARegistrantActivatedBeforeACrash covers a crash
// between the ungated finish's activation and its record write: the record is
// still there, but the active registrant keeps the tenant.
func TestRegistrationSweeper_KeepsARegistrantActivatedBeforeACrash(t *testing.T) {
	handler := setupSelfRegHandler(t, brokerSelfRegConfig())
	svc := handler.server.service.(*selfRegService)
	newXKMSCeremony(svc, true)
	sessionID := selfRegBegin(t, handler, "Acme Inc", "jane@acme.example")
	users := usersIn(t, handler, "acme-inc")
	require.Len(t, users, 1)
	active := iam.LifecycleActive
	_, err := svc.store.UpdateUser(context.Background(), "acme-inc", users[0].ID,
		iam.UpdateUserOptions{Lifecycle: &active})
	require.NoError(t, err)
	restart(t, sessionID)
	sweeper, _ := newTestSweeper(handler, &testClock{at: afterExpiry()})

	err = sweeper.sweep(context.Background())

	var inUse *SelfRegistrationTenantInUseError
	require.ErrorAs(t, err, &inUse)
	assert.Equal(t, "acme-inc", inUse.TenantID)
	assert.Equal(t, []string{"acme-inc"}, tenantIDs(t, handler))
	users = usersIn(t, handler, "acme-inc")
	require.Len(t, users, 1)
	assert.Equal(t, iam.LifecycleActive, users[0].Lifecycle)
}
