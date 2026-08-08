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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// REST session-renewal wiring.
//
// These tests pin the two contracts the SPA and the CLI depend on:
//
//   1. the login-finish response carries the renewal credential, and
//   2. the renewal endpoint returns the ROTATED credential (not an empty
//      string, which is what the handler used to hard-code).
// =============================================================================

// fakeSessionRefreshIssuer is a test double for service.SessionRefreshIssuer.
type fakeSessionRefreshIssuer struct {
	value     string
	expiresAt time.Time
}

func (f *fakeSessionRefreshIssuer) IssueRefreshForSessionBearer(_ context.Context, _ string) (string, time.Time, error) {
	return f.value, f.expiresAt, nil
}

// TestCeremonyFinishLogin_ReturnsRefreshCredential asserts the login-finish
// body carries refresh_token. Without it a CLI or SPA holds a bearer it can
// never renew, and the session dies at the 15-minute boundary.
func TestCeremonyFinishLogin_ReturnsRefreshCredential(t *testing.T) {
	deadline := time.Now().Add(7 * 24 * time.Hour).UTC()
	fake := &ceremonyFakeLoginClient{
		finishResp: &qrdbtransport.FinishLoginResponse{
			Bearer:    "session-bearer",
			ExpiresAt: time.Now().Add(15 * time.Minute),
			Identity:  &qrdbtransport.Identity{Subject: "alice", TenantID: "acme"},
			FactorID:  "factor-1",
		},
	}
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()
	login := service.NewLoginService(fake)
	login.SetSessionRefreshIssuer(&fakeSessionRefreshIssuer{value: "refresh-1", expiresAt: deadline})
	handler.server.service.SetLoginService(login)

	body, err := json.Marshal(ceremonyFinishLoginRequest{
		SessionID: "s1",
		Type:      string(qrdbtransport.FactorTypeFIDO2),
		Signature: []byte("proof"),
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/finish", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.CeremonyFinishLogin(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var out ceremonyFinishLoginResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, "session-bearer", out.Bearer)
	assert.Equal(t, "refresh-1", out.RefreshToken)
	assert.NotEmpty(t, out.RefreshExpiresAt, "the session's absolute deadline must be reported")

	parsed, err := time.Parse(time.RFC3339Nano, out.RefreshExpiresAt)
	require.NoError(t, err, "refresh_expires_at must be RFC3339")
	assert.WithinDuration(t, deadline, parsed, time.Second)
}

// TestCeremonyFinishLogin_OmitsRefreshWhenIssuerAbsent asserts the field is
// simply absent (not an error) before the OIDC provider is bound.
func TestCeremonyFinishLogin_OmitsRefreshWhenIssuerAbsent(t *testing.T) {
	fake := &ceremonyFakeLoginClient{
		finishResp: &qrdbtransport.FinishLoginResponse{
			Bearer:    "session-bearer",
			ExpiresAt: time.Now().Add(15 * time.Minute),
		},
	}
	handler, cleanup := setupCeremonyLoginHandler(t, fake)
	defer cleanup()

	body, err := json.Marshal(ceremonyFinishLoginRequest{
		SessionID: "s1",
		Type:      string(qrdbtransport.FactorTypePIV),
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/finish", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.CeremonyFinishLogin(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	assert.Equal(t, "session-bearer", raw["bearer"])
	_, present := raw["refresh_token"]
	assert.False(t, present, "refresh_token must be omitted, not empty-valued")
}

// stubRefreshService is a QuicBaaS service double that answers RefreshToken
// with a canned rotation result.
type stubRefreshService struct {
	service.QuicBaaS
	resp *service.RefreshTokenResponse
	err  error
	got  *service.RefreshTokenRequest
}

func (s *stubRefreshService) RefreshToken(_ context.Context, req *service.RefreshTokenRequest) (*service.RefreshTokenResponse, error) {
	s.got = req
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

// TestRefreshTokenHandler_ReturnsRotatedCredential is the regression guard for
// the handler that used to hard-code `RefreshToken: ""` in its response: a
// client that stored that empty value could never renew again.
func TestRefreshTokenHandler_ReturnsRotatedCredential(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	stub := &stubRefreshService{
		QuicBaaS: handler.server.service,
		resp: &service.RefreshTokenResponse{
			AccessToken:      "new-access",
			ExpiresAt:        time.Now().Add(15 * time.Minute).Unix(),
			RefreshToken:     "refresh-2",
			RefreshExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix(),
			TokenType:        "Bearer",
		},
	}
	handler.server.service = stub

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh",
		bytes.NewReader([]byte(`{"refresh_token":"refresh-1"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.RefreshToken(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var out LoginResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, "new-access", out.AccessToken)
	assert.Equal(t, "refresh-2", out.RefreshToken, "the rotated credential must reach the client")
	assert.Equal(t, "Bearer", out.TokenType)
	// expires_in is derived from a Unix SECONDS timestamp; a nanosecond value
	// would produce an absurd number here.
	assert.Greater(t, out.ExpiresIn, int64(0))
	assert.LessOrEqual(t, out.ExpiresIn, int64(3600))

	require.NotNil(t, stub.got)
	assert.Equal(t, "refresh-1", stub.got.RefreshToken)
	assert.Equal(t, http.MethodPost, stub.got.DPoPMethod)
}

// TestRefreshTokenHandler_ForwardsDPoPProof asserts a presented RFC 9449 proof
// reaches the service layer, so a sender-constrained session can be renewed
// only by the holder of the bound key.
func TestRefreshTokenHandler_ForwardsDPoPProof(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	stub := &stubRefreshService{
		QuicBaaS: handler.server.service,
		resp: &service.RefreshTokenResponse{
			AccessToken: "new-access",
			ExpiresAt:   time.Now().Add(time.Minute).Unix(),
			TokenType:   "DPoP",
		},
	}
	handler.server.service = stub

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh",
		bytes.NewReader([]byte(`{"refresh_token":"refresh-1"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DPoP", "proof.jwt.value")
	w := httptest.NewRecorder()
	handler.RefreshToken(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotNil(t, stub.got)
	assert.Equal(t, "proof.jwt.value", stub.got.DPoPProof)
	assert.NotEmpty(t, stub.got.DPoPURI)

	var out LoginResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, "DPoP", out.TokenType, "a sender-constrained renewal keeps its token type")
}

// TestRefreshTokenHandler_RejectsMissingCredential covers the validation path.
func TestRefreshTokenHandler_RejectsMissingCredential(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh",
		bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.RefreshToken(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
