//go:build rest

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
