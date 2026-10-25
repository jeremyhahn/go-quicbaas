//go:build rest

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

package rest

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/iamtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

const (
	activationUserID uint64 = 73
	absentToken             = "0000000000000000000000000000000000000000000000000000000000000000"
)

// errUserStoreUnavailable is the failure the activation write reports when the
// fixture is built to fail it.
var errUserStoreUnavailable = &iam.IAMMessageError{Message: "user store unavailable"}

// stubWebAuthn is a WebAuthnManager whose registration ceremony succeeds, so
// tests can reach the code AFTER FinishRegistration — which is where the
// invite is spent. It counts calls so a test can assert the ceremony was not
// re-run.
type stubWebAuthn struct {
	// beginCalls and finishCalls count the invitation ceremony;
	// publicBeginCalls and publicFinishCalls count the public one, which
	// invite activation must never use.
	beginCalls        int
	publicBeginCalls  int
	finishCalls       int
	publicFinishCalls int

	// finishErr, when set, is what the ceremony refuses with. It is how a test
	// drives the attestation gate's policy refusals through the handlers
	// without standing up a real authenticator.
	finishErr error
}

func (s *stubWebAuthn) BeginRegistration(context.Context, string, string) (map[string]interface{}, error) {
	s.publicBeginCalls++
	return map[string]interface{}{"publicKey": map[string]interface{}{}}, nil
}

// BeginInviteRegistration is the ceremony invite activation must use: the
// account already exists and is pending (go-xkms#1294).
func (s *stubWebAuthn) BeginInviteRegistration(context.Context, string, string) (map[string]interface{}, error) {
	s.beginCalls++
	return map[string]interface{}{"publicKey": map[string]interface{}{}}, nil
}

func (s *stubWebAuthn) FinishRegistration(context.Context, string, string, map[string]interface{}) (*iam.AttestationResult, error) {
	s.publicFinishCalls++
	return nil, s.finishErr
}

func (s *stubWebAuthn) FinishInviteRegistration(context.Context, string, string, map[string]interface{}) error {
	s.finishCalls++
	return s.finishErr
}

func (s *stubWebAuthn) BeginLogin(context.Context, string, string) (map[string]interface{}, error) {
	return nil, nil
}

func (s *stubWebAuthn) FinishLogin(context.Context, string, map[string]interface{}) (*iam.User, error) {
	return nil, nil
}

// failingUserStorage is a UserStorage whose UpdateUser always fails, standing
// in for the activation write losing its race with a storage fault.
type failingUserStorage struct {
	iam.UserStorage
	err error
}

func (f *failingUserStorage) UpdateUser(context.Context, *iam.User) error { return f.err }

// applyThenFailUserStorage commits the write and THEN reports failure — the
// ambiguous outcome a lost response produces. The activation really did land,
// so the handler must reconcile rather than take the error at face value.
type applyThenFailUserStorage struct {
	iam.UserStorage
	err error
}

func (a *applyThenFailUserStorage) UpdateUser(ctx context.Context, u *iam.User) error {
	if err := a.UserStorage.UpdateUser(ctx, u); err != nil {
		return err
	}
	return a.err
}

// activationService is the service seam the invite handlers assert against:
// the invite store, the identity store and the WebAuthn manager together.
type activationService struct {
	*testutil.MockQuicBaaS
	identity *iam.IdentityStore
	webauthn WebAuthnManager
	piv      *iam.PIVActivator
}

func (s *activationService) GetPIVActivator(context.Context) (*iam.PIVActivator, error) {
	return s.piv, nil
}

func (s *activationService) GetIdentityStore(context.Context) (*iam.IdentityStore, error) {
	return s.identity, nil
}

func (s *activationService) GetWebAuthnManager(context.Context) (WebAuthnManager, error) {
	return s.webauthn, nil
}

// activationFixture is a handler wired for a complete FIDO2 finish.
type activationFixture struct {
	handler  *IAMHandler
	tenant   string
	token    string
	invites  iam.InviteStore
	users    iam.UserStorage
	webauthn *stubWebAuthn
}

// activationWriteMode selects what the activation write does, so a test can
// pick the definite failure, the ambiguous one, or neither.
type activationWriteMode int

const (
	// writeSucceeds is the normal path.
	writeSucceeds activationWriteMode = iota
	// writeFails never commits — the invite must survive.
	writeFails
	// writeAppliesThenFails commits and then reports failure, the ambiguous
	// outcome a lost response produces.
	writeAppliesThenFails
)

// setupActivation seeds an invite, a pending user the ceremony can resolve, a
// WebAuthn manager that succeeds, and a PIV activator over the same stores.
// mode selects the activation-write behaviour.
func setupActivation(t *testing.T, mode activationWriteMode) (*activationFixture, func()) {
	t.Helper()

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mem := iam.NewMemoryStorage()
	// The invite lives in the invite store; the user record carries no copy of
	// it, which is why there is no token to seed here.
	require.NoError(t, mem.CreateUser(context.Background(), &iam.User{
		ID:        activationUserID,
		TenantID:  inviteTenant,
		Email:     "alice@acme.com",
		Lifecycle: iam.LifecyclePending,
	}))

	var users iam.UserStorage = mem
	switch mode {
	case writeFails:
		users = &failingUserStorage{UserStorage: mem, err: errUserStoreUnavailable}
	case writeAppliesThenFails:
		users = &applyThenFailUserStorage{UserStorage: mem, err: errUserStoreUnavailable}
	case writeSucceeds:
	}

	identity := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: users,
		RoleStorage: mem,
	})

	invites := iamtest.NewMemoryInviteStore()
	token, _, err := invites.Create(context.Background(), inviteTenant, activationUserID, time.Hour)
	require.NoError(t, err)

	mock := testutil.NewMockQuicBaaS()
	mock.InviteStoreOverride = invites
	webauthn := &stubWebAuthn{}

	// A PIV activator over the SAME invite store, so the PIV route resolves
	// the same tokens. It needs no usable trust anchors here: PIVActivator
	// looks the invite up before it parses the certificate, which is precisely
	// why that route could be used as an enumeration oracle.
	loginFactors, err := iam.NewCertLoginFactorEnroller(testutil.NewMemoryUserAuthFactors())
	require.NoError(t, err)
	claims, err := iamtest.NewMemoryActivationClaims()
	require.NoError(t, err)
	piv, err := iam.NewPIVActivator(iam.PIVActivatorDependencies{
		Roots:            x509.NewCertPool(),
		UserStorage:      users,
		InviteStore:      invites,
		LoginFactors:     loginFactors,
		ActivationClaims: claims,
	})
	require.NoError(t, err)

	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(&activationService{
		MockQuicBaaS: mock,
		identity:     identity,
		webauthn:     webauthn,
		piv:          piv,
	}, cfg, log)
	require.NoError(t, err)

	return &activationFixture{
		handler:  &IAMHandler{server: server},
		tenant:   inviteTenant,
		token:    token,
		invites:  invites,
		users:    mem,
		webauthn: webauthn,
	}, func() {
		server.Close()
	}
}

// finish drives ActivateFIDO2Finish for the fixture's current token.
func (f *activationFixture) finish(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	return f.call(t, http.MethodPost, "/activate/fido2/finish",
		`{"credential":{"id":"abc"}}`, f.handler.ActivateFIDO2Finish)
}

// begin drives ActivateFIDO2Begin for the fixture's current token.
func (f *activationFixture) begin(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	return f.call(t, http.MethodPost, "/activate/fido2/begin", "", f.handler.ActivateFIDO2Begin)
}

// get drives GetInvite for the fixture's current token.
func (f *activationFixture) get(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	return f.call(t, http.MethodGet, "", "", f.handler.GetInvite)
}

func (f *activationFixture) call(
	t *testing.T,
	method, suffix, body string,
	h http.HandlerFunc,
) *httptest.ResponseRecorder {
	t.Helper()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", f.tenant)
	rctx.URLParams.Add("token", f.token)

	req := httptest.NewRequest(method,
		"/api/v1/iam/"+f.tenant+"/invites/"+f.token+suffix, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	h(w, req)
	return w
}

// TestActivateFIDO2Finish_ActivationFailureLeavesInviteUnspent is the #272
// regression. The activation write used to have its error discarded, and the
// invite was consumed regardless — spending a one-time token on an account
// still in `pending`, with no reissue surface and no recovery short of
// deleting and recreating the user.
//
// The invite must survive a failed activation, because it is the only thing
// that keeps the account activatable at all.
func TestActivateFIDO2Finish_ActivationFailureLeavesInviteUnspent(t *testing.T) {
	f, cleanup := setupActivation(t, writeFails)
	defer cleanup()

	w := f.finish(t)
	assert.NotEqual(t, http.StatusOK, w.Code, "a failed activation reported success")

	rec, err := f.invites.Redeem(context.Background(), inviteTenant, f.token)
	require.NoError(t, err, "the invite was consumed despite the activation failing")
	assert.Equal(t, activationUserID, rec.UserID)
}

// TestActivateFIDO2Finish_SuccessActivatesAndConsumes proves a successful
// ceremony both activates the user AND spends the invite — the fail-closed
// change must not leave a redeemed invite live, nor skip the activation write.
func TestActivateFIDO2Finish_SuccessActivatesAndConsumes(t *testing.T) {
	f, cleanup := setupActivation(t, writeSucceeds)
	defer cleanup()

	w := f.finish(t)
	require.Equal(t, http.StatusOK, w.Code, "activation failed: %s", w.Body.String())

	user, err := f.users.GetUser(context.Background(), inviteTenant, activationUserID)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecycleActive, user.Lifecycle, "the user was not activated")

	_, err = f.invites.Redeem(context.Background(), inviteTenant, f.token)
	assert.ErrorIs(t, err, iam.ErrInviteUsed,
		"the invite was still redeemable after a successful activation")
}

// TestActivateFIDO2Finish_ReplayIsRefused proves a finished invite cannot
// activate a second time, and that the replay is refused BEFORE the WebAuthn
// ceremony runs again. Single-use is the whole security property of an
// activation token: a replayable one lets whoever observes the link register
// their own credential against the account afterwards.
func TestActivateFIDO2Finish_ReplayIsRefused(t *testing.T) {
	f, cleanup := setupActivation(t, writeSucceeds)
	defer cleanup()

	first := f.finish(t)
	require.Equal(t, http.StatusOK, first.Code, "first activation failed: %s", first.Body.String())
	require.Equal(t, 1, f.webauthn.finishCalls)

	second := f.finish(t)
	assert.Equal(t, http.StatusNotFound, second.Code,
		"a spent invite activated twice (got %d: %s)", second.Code, second.Body.String())
	assert.Equal(t, 1, f.webauthn.finishCalls,
		"the replay reached the WebAuthn ceremony instead of being refused at the invite")
}

// TestActivateFIDO2Begin_AbandonedCeremonyLeavesInviteUnspent pins the
// property #272 assumed was broken. A ceremony that begins and never finishes
// — the invitee cancels the passkey dialog, the browser errors — must leave
// the invite redeemable, because begin only reads it.
//
// It is worth a test even though it already held: the issue was filed against
// this exact behaviour, so the belief that begin consumes the invite will
// recur unless something states otherwise.
func TestActivateFIDO2Begin_AbandonedCeremonyLeavesInviteUnspent(t *testing.T) {
	f, cleanup := setupActivation(t, writeSucceeds)
	defer cleanup()

	w := f.begin(t)
	require.Equal(t, http.StatusOK, w.Code, "begin failed: %s", w.Body.String())

	// The ceremony stops here — finish is never called.
	_, err := f.invites.Redeem(context.Background(), inviteTenant, f.token)
	assert.NoError(t, err, "begin consumed the invite")
}

// TestActivateFIDO2Begin_UsesTheInvitationCeremony pins the go-xkms#1294
// contract: the invitee's account already exists and is pending, and go-xkms
// refuses the public registration ceremony for any existing account, so
// invite activation must open the invitation ceremony and never the public
// one.
func TestActivateFIDO2Begin_UsesTheInvitationCeremony(t *testing.T) {
	f, cleanup := setupActivation(t, writeSucceeds)
	defer cleanup()

	w := f.begin(t)
	require.Equal(t, http.StatusOK, w.Code, "begin failed: %s", w.Body.String())
	assert.Equal(t, 1, f.webauthn.beginCalls, "invite activation must open the invitation ceremony")
	assert.Zero(t, f.webauthn.publicBeginCalls, "invite activation must never open the public ceremony")
}

// TestActivateFIDO2Finish_UsesTheInvitationCeremony is the finish half of the
// same contract: go-xkms's public finish refuses the still-pending invitee,
// so only the invitation finish can close the ceremony.
func TestActivateFIDO2Finish_UsesTheInvitationCeremony(t *testing.T) {
	f, cleanup := setupActivation(t, writeSucceeds)
	defer cleanup()

	w := f.finish(t)
	require.Equal(t, http.StatusOK, w.Code, "finish failed: %s", w.Body.String())
	assert.Equal(t, 1, f.webauthn.finishCalls, "invite activation must close the invitation ceremony")
	assert.Zero(t, f.webauthn.publicFinishCalls, "invite activation must never close the public ceremony")
}

// TestInviteRoutes_SpentTokenIsOpaque proves a spent token gets the SAME
// answer as one that never existed, on every public invite route.
//
// Two things were wrong before. The spent case reached the UI as
// "access denied: invite has already been used" — a 403, which says the caller
// lacks authority and sends the invitee looking for a permissions problem they
// do not have. And distinguishing spent from absent is an enumeration oracle
// on an unauthenticated route: qrdb requires the cases be collapsed, because
// confirming that a token exists but is spent confirms a real user was
// invited.
func TestInviteRoutes_SpentTokenIsOpaque(t *testing.T) {
	f, cleanup := setupActivation(t, writeSucceeds)
	defer cleanup()

	_, err := f.invites.Redeem(context.Background(), inviteTenant, f.token)
	require.NoError(t, err)

	spentGet := f.get(t)
	spentBegin := f.begin(t)

	// The same requests against a token that never existed.
	f.token = absentToken
	absentGet := f.get(t)
	absentBegin := f.begin(t)

	assert.Equal(t, absentGet.Code, spentGet.Code, "GetInvite distinguishes spent from absent")
	assert.Equal(t, absentGet.Body.String(), spentGet.Body.String(),
		"GetInvite body distinguishes spent from absent")
	assert.Equal(t, absentBegin.Code, spentBegin.Code, "begin distinguishes spent from absent")
	assert.Equal(t, absentBegin.Body.String(), spentBegin.Body.String(),
		"begin body distinguishes spent from absent")

	assert.Equal(t, http.StatusNotFound, spentGet.Code)
	assert.NotContains(t, spentGet.Body.String(), "access denied")
	assert.NotContains(t, spentGet.Body.String(), "already been used")
}

// TestInviteRoutes_ExpiredTokenIsOpaque proves expiry is collapsed into the
// same opaque answer, for the same reason.
func TestInviteRoutes_ExpiredTokenIsOpaque(t *testing.T) {
	f, cleanup := setupActivation(t, writeSucceeds)
	defer cleanup()

	expired, _, err := f.invites.Create(context.Background(), inviteTenant, activationUserID, -time.Minute)
	require.NoError(t, err)

	f.token = expired
	expiredGet := f.get(t)

	f.token = absentToken
	absentGet := f.get(t)

	assert.Equal(t, absentGet.Code, expiredGet.Code, "GetInvite distinguishes expired from absent")
	assert.Equal(t, absentGet.Body.String(), expiredGet.Body.String(),
		"GetInvite body distinguishes expired from absent")
}

// piv drives ActivatePIV for the fixture's current token with an arbitrary,
// syntactically valid-looking body. The certificate is never parsed on these
// paths: PIVActivator resolves the invite first.
func (f *activationFixture) piv(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	return f.call(t, http.MethodPost, "/activate/piv",
		`{"cert_pem":"-----BEGIN CERTIFICATE-----\nnot-a-real-cert\n-----END CERTIFICATE-----"}`,
		f.handler.ActivatePIV)
}

// TestActivatePIV_UnusableInvitesAreOpaque proves the PIV route answers absent,
// spent and expired tokens identically.
//
// It is the more exposed of the two activation routes, not the less: PIVActivator
// looks the invite up BEFORE it parses or verifies the certificate, so an
// unauthenticated caller posting arbitrary cert_pem could otherwise read the
// distinct statuses as an existence oracle — confirming which tokens name a real
// pending user. Same policy as the FIDO2 routes, for the same reason.
func TestActivatePIV_UnusableInvitesAreOpaque(t *testing.T) {
	f, cleanup := setupActivation(t, writeSucceeds)
	defer cleanup()

	spentTok := f.token
	_, err := f.invites.Redeem(context.Background(), inviteTenant, spentTok)
	require.NoError(t, err)

	expiredTok, _, err := f.invites.Create(context.Background(), inviteTenant, activationUserID, -time.Minute)
	require.NoError(t, err)

	f.token = spentTok
	spent := f.piv(t)

	f.token = expiredTok
	expired := f.piv(t)

	f.token = absentToken
	absent := f.piv(t)

	assert.Equal(t, absent.Code, spent.Code, "PIV distinguishes spent from absent")
	assert.Equal(t, absent.Body.String(), spent.Body.String(),
		"PIV body distinguishes spent from absent")
	assert.Equal(t, absent.Code, expired.Code, "PIV distinguishes expired from absent")
	assert.Equal(t, absent.Body.String(), expired.Body.String(),
		"PIV body distinguishes expired from absent")

	assert.Equal(t, http.StatusNotFound, spent.Code)
	assert.NotContains(t, spent.Body.String(), "invite_used")
	assert.NotContains(t, expired.Body.String(), "invite_expired")
}

// TestActivateFIDO2Finish_AmbiguousWriteReconcilesAndConsumes covers the
// outcome an unconditional fail-closed return gets wrong: the activation write
// COMMITS and then reports an error, as a lost response does.
//
// Taking that error at face value would leave a live one-time token on an
// account that is already active — redeemable by whoever else holds the link.
// The handler re-reads the user, finds the activation persisted, and finishes
// the job: the invite is consumed and the irregularity is surfaced as a
// warning rather than a refusal.
func TestActivateFIDO2Finish_AmbiguousWriteReconcilesAndConsumes(t *testing.T) {
	f, cleanup := setupActivation(t, writeAppliesThenFails)
	defer cleanup()

	w := f.finish(t)
	require.Equal(t, http.StatusOK, w.Code,
		"an activation that actually landed was refused: %s", w.Body.String())

	var resp ActivationResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.Warning, "the ambiguous write was not surfaced to the operator")

	user, err := f.users.GetUser(context.Background(), inviteTenant, activationUserID)
	require.NoError(t, err)
	require.Equal(t, iam.LifecycleActive, user.Lifecycle, "the fixture did not actually persist the activation")

	_, err = f.invites.Redeem(context.Background(), inviteTenant, f.token)
	assert.ErrorIs(t, err, iam.ErrInviteUsed,
		"a live invite was left on an activated account")
}

// TestActivateFIDO2Finish_DefiniteWriteFailureStillFailsClosed pins the other
// half of the reconciliation: when the write did NOT land, the refusal stands
// and the invite survives. Reconciling must not soften the fail-closed path.
func TestActivateFIDO2Finish_DefiniteWriteFailureStillFailsClosed(t *testing.T) {
	f, cleanup := setupActivation(t, writeFails)
	defer cleanup()

	w := f.finish(t)
	require.NotEqual(t, http.StatusOK, w.Code, "a failed activation reported success")

	user, err := f.users.GetUser(context.Background(), inviteTenant, activationUserID)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecyclePending, user.Lifecycle, "the user was activated despite the write failing")

	_, err = f.invites.Redeem(context.Background(), inviteTenant, f.token)
	assert.NoError(t, err, "the invite was consumed despite the activation failing")
}
