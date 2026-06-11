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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ceremonyErrLoginClient backs a real LoginService but returns a configurable
// error from FinishLogin/BeginLogin so the REST error mapping is exercised.
type ceremonyErrLoginClient struct {
	beginErr  error
	finishErr error
}

func (c *ceremonyErrLoginClient) ResolveIdentity(_ context.Context, _ string) (*qrdbtransport.Identity, error) {
	return nil, errors.New("not implemented")
}

func (c *ceremonyErrLoginClient) BeginLogin(_ context.Context, _ qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error) {
	if c.beginErr != nil {
		return nil, c.beginErr
	}
	return &qrdbtransport.BeginLoginResponse{SessionID: "s1", ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (c *ceremonyErrLoginClient) FinishLogin(_ context.Context, _ qrdbtransport.FinishLoginRequest) (*qrdbtransport.FinishLoginResponse, error) {
	return nil, c.finishErr
}

func (c *ceremonyErrLoginClient) Logout(_ context.Context, _ string) error { return nil }

func setupCeremonyErrHandler(t *testing.T, c *ceremonyErrLoginClient) (*IAMHandler, func()) {
	handler, _, cleanup := setupIAMHandler(t)
	handler.server.service.SetLoginService(service.NewLoginService(c))
	return handler, cleanup
}

// When no LoginService is wired, the ceremony handlers return 503.
func TestCeremonyBeginLogin_NotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	body, _ := json.Marshal(ceremonyBeginLoginRequest{Type: "mtls"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/begin", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.CeremonyBeginLogin(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestCeremonyFinishLogin_NotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	body, _ := json.Marshal(ceremonyFinishLoginRequest{Type: "piv", LookupKey: []byte("k")})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/finish", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.CeremonyFinishLogin(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestCeremonyBeginLogin_InvalidJSON(t *testing.T) {
	handler, cleanup := setupCeremonyErrHandler(t, &ceremonyErrLoginClient{})
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/begin", bytes.NewReader([]byte("{not-json")))
	w := httptest.NewRecorder()
	handler.CeremonyBeginLogin(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCeremonyFinishLogin_InvalidJSON(t *testing.T) {
	handler, cleanup := setupCeremonyErrHandler(t, &ceremonyErrLoginClient{})
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/finish", bytes.NewReader([]byte("{not-json")))
	w := httptest.NewRecorder()
	handler.CeremonyFinishLogin(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCeremonyBeginLogin_BackendError(t *testing.T) {
	handler, cleanup := setupCeremonyErrHandler(t, &ceremonyErrLoginClient{beginErr: errors.New("boom")})
	defer cleanup()

	body, _ := json.Marshal(ceremonyBeginLoginRequest{Type: "piv", Hint: "alice"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/begin", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.CeremonyBeginLogin(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// respondLoginCeremonyError maps each typed facade error onto its HTTP status.
func TestRespondLoginCeremonyError_TypedMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"unavailable", &service.LoginCeremonyUnavailableError{}, http.StatusServiceUnavailable},
		{"peer_required", &service.MTLSPeerCertRequiredError{}, http.StatusUnauthorized},
		{"nil_request", &service.LoginRequestNilError{}, http.StatusBadRequest},
		{"generic", errors.New("invalid credentials"), http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/x", nil)
			respondLoginCeremonyError(w, r, tt.err)
			assert.Equal(t, tt.want, w.Code)
		})
	}
}

// A DPoP proof on login-finish rides the context into the issuer; the request
// still completes (PIV path, no peer cert required).
func TestCeremonyFinishLogin_WithDPoP(t *testing.T) {
	fake := &ceremonyFakeLoginClient{finishResp: &qrdbtransport.FinishLoginResponse{
		Bearer:    "ok",
		Identity:  &qrdbtransport.Identity{Subject: "u", TenantID: "t1"},
		ExpiresAt: time.Now().Add(time.Minute),
	}}
	handler, cleanup := setupCeremonyLoginHandler(t, fake)
	defer cleanup()

	body, _ := json.Marshal(ceremonyFinishLoginRequest{
		Type:      string(qrdbtransport.FactorTypePIV),
		LookupKey: []byte("piv-hash"),
		Signature: []byte("sig"),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/finish", bytes.NewReader(body))
	req.Header.Set("DPoP", "eyJ.proof.value")
	w := httptest.NewRecorder()
	handler.CeremonyFinishLogin(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.True(t, fake.finishInvoked)
}

// verifiedPeerCertFromRequest returns nil for a request with no TLS or no
// verified chains, and the leaf when a verified chain is present.
func TestVerifiedPeerCertFromRequest(t *testing.T) {
	if got := verifiedPeerCertFromRequest(nil); got != nil {
		t.Fatal("nil request must yield nil cert")
	}
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	if got := verifiedPeerCertFromRequest(r); got != nil {
		t.Fatal("no TLS must yield nil cert")
	}
}
