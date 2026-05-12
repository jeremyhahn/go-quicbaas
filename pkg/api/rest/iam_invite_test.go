//go:build rest && iam

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
)

// setupIAMHandlerWithInvite creates an IAMHandler with a pre-seeded inviteStore and user.
func setupIAMHandlerWithInvite(t *testing.T) (*IAMHandler, string, string, string, func()) {
	t.Helper()

	_, server, cleanup := setupIAMHandler(t)

	// Create the handler via NewIAMHandler so inviteStore is initialized.
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})

	// Seed a pending user directly in the invite store.
	token, err := iam.GenerateInviteToken()
	require.NoError(t, err)

	tenantID := "tenant-invite"
	userID := "user-invite-1"

	expiresAt := time.Now().Add(time.Hour)
	record := &iam.InviteRecord{
		TokenHash: iam.TokenHashExported(token),
		UserID:    userID,
		TenantID:  tenantID,
		ExpiresAt: expiresAt,
	}
	require.NoError(t, handler.inviteStore.Create(context.Background(), record))

	_ = server // unused but kept for cleanup

	return handler, token, tenantID, userID, cleanup
}

// TestGetInvite_Valid verifies that a valid invite token returns 200 with metadata.
func TestGetInvite_Valid(t *testing.T) {
	handler, token, _, userID, cleanup := setupIAMHandlerWithInvite(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
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
	handler, _, _, _, cleanup := setupIAMHandlerWithInvite(t)
	defer cleanup()

	bogusToken := "0000000000000000000000000000000000000000000000000000000000000000"
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", bogusToken)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/invites/"+bogusToken, nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.GetInvite(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestGetInvite_Expired verifies that an expired invite returns 410 Gone.
func TestGetInvite_Expired(t *testing.T) {
	handler, _, _, _, cleanup := setupIAMHandlerWithInvite(t)
	defer cleanup()

	expToken, err := iam.GenerateInviteToken()
	require.NoError(t, err)
	expRecord := &iam.InviteRecord{
		TokenHash: iam.TokenHashExported(expToken),
		UserID:    "user-exp",
		TenantID:  "t1",
		ExpiresAt: time.Now().Add(-time.Minute),
	}
	require.NoError(t, handler.inviteStore.Create(context.Background(), expRecord))

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", expToken)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/invites/"+expToken, nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.GetInvite(w, req)

	assert.Equal(t, http.StatusGone, w.Code)
}

// TestActivateFIDO2Begin_InviteNotFound verifies 404 for unknown invite token.
func TestActivateFIDO2Begin_InviteNotFound(t *testing.T) {
	handler, _, _, _, cleanup := setupIAMHandlerWithInvite(t)
	defer cleanup()

	bogus := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	rctx := chi.NewRouteContext()
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
	handler, token, _, _, cleanup := setupIAMHandlerWithInvite(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
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
	handler, token, _, _, cleanup := setupIAMHandlerWithInvite(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
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
	handler, token, _, _, cleanup := setupIAMHandlerWithInvite(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
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
	handler, token, _, _, cleanup := setupIAMHandlerWithInvite(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
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
	handler, _, _, _, cleanup := setupIAMHandlerWithInvite(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/invites/", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.GetInvite(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
