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
	"context"
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
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// setupIAMHandlerWithInvite creates an IAMHandler whose service exposes the
// canonical persistent InviteStore (via the mock's InviteStoreOverride) and
// pre-seeds it with one valid invite. The store is the SAME instance the
// handler resolves through getInviteStore — exercising the unified wiring the
// production server uses, not a disjoint per-handler memory store.
func setupIAMHandlerWithInvite(t *testing.T) (*IAMHandler, string, string, uint64, iam.InviteStore, func()) {
	t.Helper()

	handler, server, cleanup := setupIAMHandler(t)

	// Wire the persistent invite store onto the mock service so the handler
	// resolves it via getInviteStore — the production injection seam.
	store := iamtest.NewMemoryInviteStore()
	mockSvc, ok := server.service.(*testutil.MockQuicBaaS)
	require.True(t, ok, "mock service must be *testutil.MockQuicBaaS")
	mockSvc.InviteStoreOverride = store

	tenantID := inviteTenant
	userID := uint64(72)

	// Seed the invite through the store; the token is minted there.
	token, record, err := store.Create(context.Background(), tenantID, userID, time.Hour)
	require.NoError(t, err)
	_ = record
	return handler, token, tenantID, userID, store, cleanup
}

// TestGetInvite_Valid verifies that a valid invite token returns 200 with metadata.
func TestGetInvite_Valid(t *testing.T) {
	handler, token, _, userID, _, cleanup := setupIAMHandlerWithInvite(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", inviteTenant)
	rctx.URLParams.Add("token", token)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/invites/"+token, nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.GetInvite(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp InviteMetadataResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, userID, resp.UserID)
	assert.NotEmpty(t, resp.AuthMethods)
}

// TestGetInvite_NotFound verifies that an unknown token returns 404.
func TestGetInvite_NotFound(t *testing.T) {
	handler, _, _, _, _, cleanup := setupIAMHandlerWithInvite(t)
	defer cleanup()

	bogusToken := "0000000000000000000000000000000000000000000000000000000000000000"
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", inviteTenant)
	rctx.URLParams.Add("token", bogusToken)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/invites/"+bogusToken, nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.GetInvite(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestGetInvite_Expired verifies that an expired invite gets the opaque
// not-found answer rather than a distinguishable "expired" one. The route is
// unauthenticated, so expired, spent and absent are deliberately
// indistinguishable — see respondInviteUnusable.
func TestGetInvite_Expired(t *testing.T) {
	handler, _, _, _, store, cleanup := setupIAMHandlerWithInvite(t)
	defer cleanup()

	// Seed in the SAME tenant the request targets, so the handler reaches its
	// expiry path rather than the tenant-scoping one. Both now answer alike,
	// but the test should still exercise the case it names.
	expToken, _, err := store.Create(context.Background(), inviteTenant, 70, -time.Minute)
	require.NoError(t, err)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", inviteTenant)
	rctx.URLParams.Add("token", expToken)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/invites/"+expToken, nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.GetInvite(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestActivateFIDO2Begin_InviteNotFound verifies 404 for unknown invite token.
func TestActivateFIDO2Begin_InviteNotFound(t *testing.T) {
	handler, _, _, _, _, cleanup := setupIAMHandlerWithInvite(t)
	defer cleanup()

	bogus := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", inviteTenant)
	rctx.URLParams.Add("token", bogus)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/invites/"+bogus+"/activate/fido2/begin", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.ActivateFIDO2Begin(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestActivateFIDO2Begin_ValidInvite_WebAuthnNotConfigured verifies that a valid invite
// but missing WebAuthn manager returns 500 (manager not available).
func TestActivateFIDO2Begin_ValidInvite_WebAuthnNotConfigured(t *testing.T) {
	handler, token, _, _, _, cleanup := setupIAMHandlerWithInvite(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", inviteTenant)
	rctx.URLParams.Add("token", token)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/invites/"+token+"/activate/fido2/begin", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.ActivateFIDO2Begin(w, req)

	// No WebAuthn manager configured in the basic mock, so expect 500.
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "WebAuthn manager not available")
}

// TestActivateFIDO2Finish_InvalidBody verifies 400 when credential is missing.
func TestActivateFIDO2Finish_InvalidBody(t *testing.T) {
	handler, token, _, _, _, cleanup := setupIAMHandlerWithInvite(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", inviteTenant)
	rctx.URLParams.Add("token", token)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/invites/"+token+"/activate/fido2/finish",
		strings.NewReader(`{"credential":null}`))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ActivateFIDO2Finish(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "credential is required")
}

// TestActivatePIV_MissingCert verifies 400 when cert_pem is absent.
func TestActivatePIV_MissingCert(t *testing.T) {
	handler, token, _, _, _, cleanup := setupIAMHandlerWithInvite(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", inviteTenant)
	rctx.URLParams.Add("token", token)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/invites/"+token+"/activate/piv",
		strings.NewReader(`{}`))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ActivatePIV(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "cert_pem is required")
}

// TestActivatePIV_PIVNotConfigured verifies 501 when no PIVActivator is configured.
func TestActivatePIV_PIVNotConfigured(t *testing.T) {
	handler, token, _, _, _, cleanup := setupIAMHandlerWithInvite(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", inviteTenant)
	rctx.URLParams.Add("token", token)

	body := `{"cert_pem":"-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/invites/"+token+"/activate/piv",
		strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ActivatePIV(w, req)

	// Basic mock doesn't implement GetPIVActivator, so expect 501.
	assert.Equal(t, http.StatusNotImplemented, w.Code)
	assert.Contains(t, w.Body.String(), "piv_not_configured")
}

// TestGetInvite_MissingToken verifies 400 when token URL param is missing.
func TestGetInvite_MissingToken(t *testing.T) {
	handler, _, _, _, _, cleanup := setupIAMHandlerWithInvite(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/invites/", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.GetInvite(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
