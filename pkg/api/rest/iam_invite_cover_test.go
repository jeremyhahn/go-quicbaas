//go:build rest

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

package rest

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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

type inviteActivationService struct {
	*testutil.MockQuicBaaS
	mgr   WebAuthnManager
	store *iam.IdentityStore
}

func (s *inviteActivationService) GetWebAuthnManager(_ context.Context) (WebAuthnManager, error) {
	return s.mgr, nil
}
func (s *inviteActivationService) GetIdentityStore(_ context.Context) (*iam.IdentityStore, error) {
	return s.store, nil
}

const (
	inviteTenant        = "tenant-invite"
	inviteUserID uint64 = 72
)

// setupInviteActivationHandler wires a WebAuthnManager and an identity store
// with a pending user whose invite is seeded into the canonical persistent
// invite store (exposed to the handler via the service's GetInviteStore seam).
func setupInviteActivationHandler(t *testing.T) (*IAMHandler, string, iam.InviteStore, func()) {
	t.Helper()
	memStorage := iam.NewMemoryStorage()
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: memStorage,
		RoleStorage: memStorage,
		Tenants:     &testTenantManager{storage: memStorage},
	})
	require.NoError(t, memStorage.CreateUser(context.Background(), &iam.User{
		ID: inviteUserID, TenantID: inviteTenant, Email: "invitee@example.com",
	}))

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	inviteStore := iamtest.NewMemoryInviteStore()
	mockSvc := &inviteActivationService{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		mgr:          &fakeWebAuthnManager{user: &iam.User{ID: inviteUserID, TenantID: inviteTenant, Email: "invitee@example.com"}},
		store:        store,
	}
	// Expose the persistent invite store through the service's GetInviteStore
	// seam (promoted from the embedded MockQuicBaaS).
	mockSvc.InviteStoreOverride = inviteStore
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc

	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	token, _, err := inviteStore.Create(context.Background(), inviteTenant, inviteUserID, time.Hour)
	require.NoError(t, err)

	return handler, token, inviteStore, func() { server.Close() }
}

func inviteTokenReq(method, token string) *http.Request {
	r := httptest.NewRequest(method, "/api/v1/iam/invites/"+token+"/activate/fido2/begin", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", inviteTenant)
	rctx.URLParams.Add("token", token)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// ActivateFIDO2Begin resolves the invite, looks up the email via the identity
// store, and returns the WebAuthn registration options.
func TestActivateFIDO2Begin_Success(t *testing.T) {
	handler, token, _, cleanup := setupInviteActivationHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.ActivateFIDO2Begin(w, inviteTokenReq(http.MethodPost, token))

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "challenge")
}

func inviteFinishReq(t *testing.T, token string) *http.Request {
	t.Helper()
	body := []byte(`{"credential":{"id":"abc","response":{}}}`)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/iam/invites/"+token+"/activate/fido2/finish", nil)
	r.Body = io.NopCloser(bytes.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", inviteTenant)
	rctx.URLParams.Add("token", token)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// ActivateFIDO2Finish verifies the credential, activates the user, and consumes
// the invite.
func TestActivateFIDO2Finish_Success(t *testing.T) {
	handler, token, _, cleanup := setupInviteActivationHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.ActivateFIDO2Finish(w, inviteFinishReq(t, token))

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "activation successful")
}

// ActivateFIDO2Finish rejects a request with no credential.
func TestActivateFIDO2Finish_MissingCredential(t *testing.T) {
	handler, token, _, cleanup := setupInviteActivationHandler(t)
	defer cleanup()

	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	r.Body = io.NopCloser(bytes.NewReader([]byte(`{}`)))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", inviteTenant)
	rctx.URLParams.Add("token", token)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	handler.ActivateFIDO2Finish(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ActivateFIDO2Begin/Finish surface a WebAuthn manager error.
func TestActivateFIDO2_ManagerError(t *testing.T) {
	handler, token, _, cleanup := setupInviteActivationHandler(t)
	defer cleanup()
	handler.server.service.(*inviteActivationService).mgr = errWebAuthnManager{}

	w := httptest.NewRecorder()
	handler.ActivateFIDO2Begin(w, inviteTokenReq(http.MethodPost, token))
	assert.GreaterOrEqual(t, w.Code, 400)

	w = httptest.NewRecorder()
	handler.ActivateFIDO2Finish(w, inviteFinishReq(t, token))
	assert.GreaterOrEqual(t, w.Code, 400)
}

// ActivateFIDO2Begin/Finish reject an unknown invite token (404).
func TestActivateFIDO2_UnknownToken(t *testing.T) {
	handler, _, _, cleanup := setupInviteActivationHandler(t)
	defer cleanup()

	bogus := "0000000000000000000000000000000000000000000000000000000000000000"
	w := httptest.NewRecorder()
	handler.ActivateFIDO2Begin(w, inviteTokenReq(http.MethodPost, bogus))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ActivatePIV validation and not-configured branches.
func TestActivatePIV_Branches(t *testing.T) {
	handler, token, _, cleanup := setupInviteActivationHandler(t)
	defer cleanup()

	pivReq := func(tok, body string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/iam/invites/"+tok+"/activate/piv", nil)
		r.Body = io.NopCloser(bytes.NewReader([]byte(body)))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", inviteTenant)
		rctx.URLParams.Add("tenant_id", inviteTenant)
		rctx.URLParams.Add("token", tok)
		return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	}

	// Missing token
	w := httptest.NewRecorder()
	handler.ActivatePIV(w, pivReq("", `{"cert_pem":"x"}`))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Invalid JSON
	w = httptest.NewRecorder()
	handler.ActivatePIV(w, pivReq(token, "{bad"))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Missing cert_pem
	w = httptest.NewRecorder()
	handler.ActivatePIV(w, pivReq(token, `{}`))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// PIV activator not configured (service does not expose GetPIVActivator) -> 501
	w = httptest.NewRecorder()
	handler.ActivatePIV(w, pivReq(token, `{"cert_pem":"-----BEGIN CERTIFICATE-----\nx\n-----END CERTIFICATE-----"}`))
	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

// GetInvite returns 410 for a used invite and 410 for an expired one.
func TestGetInvite_UsedAndExpired(t *testing.T) {
	handler, token, store, cleanup := setupInviteActivationHandler(t)
	defer cleanup()

	getReq := func(tok string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/iam/invites/"+tok, nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", inviteTenant)
		rctx.URLParams.Add("tenant_id", inviteTenant)
		rctx.URLParams.Add("token", tok)
		return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	}

	// Mark the seeded invite used. Spent, expired and absent all get the same
	// opaque answer on this unauthenticated route — see respondInviteUnusable.
	_, redeemErr := store.Redeem(context.Background(), inviteTenant, token)
	require.NoError(t, redeemErr)
	w := httptest.NewRecorder()
	handler.GetInvite(w, getReq(token))
	assert.Equal(t, http.StatusNotFound, w.Code)

	// Seed an expired invite.
	expiredToken, _, err := store.Create(context.Background(), inviteTenant, inviteUserID, -time.Hour)
	require.NoError(t, err)
	w = httptest.NewRecorder()
	handler.GetInvite(w, getReq(expiredToken))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ActivateFIDO2Begin refuses a used invite with the opaque not-found answer.
// These routes are unauthenticated, so spent and absent must be
// indistinguishable — see respondInviteUnusable.
func TestActivateFIDO2Begin_UsedInvite(t *testing.T) {
	handler, token, store, cleanup := setupInviteActivationHandler(t)
	defer cleanup()

	_, redeemErrtoken := store.Redeem(context.Background(), inviteTenant, token)
	require.NoError(t, redeemErrtoken)
	w := httptest.NewRecorder()
	handler.ActivateFIDO2Begin(w, inviteTokenReq(http.MethodPost, token))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ActivateFIDO2Finish refuses used and expired invites with the same opaque
// not-found answer an absent token gets.
func TestActivateFIDO2Finish_UsedAndExpired(t *testing.T) {
	handler, token, store, cleanup := setupInviteActivationHandler(t)
	defer cleanup()

	// Used invite.
	_, redeemErrtoken := store.Redeem(context.Background(), inviteTenant, token)
	require.NoError(t, redeemErrtoken)
	w := httptest.NewRecorder()
	handler.ActivateFIDO2Finish(w, inviteFinishReq(t, token))
	assert.Equal(t, http.StatusNotFound, w.Code)

	// Expired invite.
	expiredToken, _, err := store.Create(context.Background(), inviteTenant, inviteUserID, -time.Hour)
	require.NoError(t, err)
	w = httptest.NewRecorder()
	handler.ActivateFIDO2Finish(w, inviteFinishReq(t, expiredToken))
	assert.Equal(t, http.StatusNotFound, w.Code)
}
