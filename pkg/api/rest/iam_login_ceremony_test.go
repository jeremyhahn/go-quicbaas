//go:build rest && iam

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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ceremonyFakeLoginClient backs a real service.LoginService so the REST
// handler -> facade -> client chain (incl. mTLS model) runs end to end.
type ceremonyFakeLoginClient struct {
	finishReq     qrdbtransport.FinishLoginRequest
	finishResp    *qrdbtransport.FinishLoginResponse
	finishInvoked bool
}

func (f *ceremonyFakeLoginClient) ResolveIdentity(_ context.Context, _ string) (*qrdbtransport.Identity, error) {
	return nil, errors.New("not implemented")
}

func (f *ceremonyFakeLoginClient) BeginLogin(_ context.Context, _ qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error) {
	return &qrdbtransport.BeginLoginResponse{SessionID: "s1", ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (f *ceremonyFakeLoginClient) FinishLogin(_ context.Context, req qrdbtransport.FinishLoginRequest) (*qrdbtransport.FinishLoginResponse, error) {
	f.finishInvoked = true
	f.finishReq = req
	return f.finishResp, nil
}

func (f *ceremonyFakeLoginClient) Logout(_ context.Context, _ string) error { return nil }

func ceremonyTestCert(t *testing.T) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "rest-user"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

// setupCeremonyLoginHandler wires the mock service with a real LoginService
// backed by the supplied fake login client.
func setupCeremonyLoginHandler(t *testing.T, fake *ceremonyFakeLoginClient) (*IAMHandler, func()) {
	handler, _, cleanup := setupIAMHandler(t)
	handler.server.service.SetLoginService(service.NewLoginService(fake))
	return handler, cleanup
}

// TestCeremonyFinishLogin_MTLS_NoPeerCert_FailsClosed asserts the REST handler
// returns 401 for an mtls request with no verified peer cert, and never reaches
// the login client.
func TestCeremonyFinishLogin_MTLS_NoPeerCert_FailsClosed(t *testing.T) {
	fake := &ceremonyFakeLoginClient{finishResp: &qrdbtransport.FinishLoginResponse{Bearer: "x"}}
	handler, cleanup := setupCeremonyLoginHandler(t, fake)
	defer cleanup()

	body, _ := json.Marshal(ceremonyFinishLoginRequest{
		Type:      string(qrdbtransport.FactorTypeMTLS),
		LookupKey: []byte("attacker-key"),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/finish", bytes.NewReader(body))
	// No req.TLS -> no verified peer cert.
	w := httptest.NewRecorder()
	handler.CeremonyFinishLogin(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.False(t, fake.finishInvoked, "login client must not be reached on fail-closed")
}

// TestCeremonyFinishLogin_MTLS_PeerCertWins is the anti-spoofing test: the body
// carries a different key than the verified peer cert; the cert wins.
func TestCeremonyFinishLogin_MTLS_PeerCertWins(t *testing.T) {
	cert := ceremonyTestCert(t)
	wantKey := sha256.Sum256(cert.Raw)

	fake := &ceremonyFakeLoginClient{finishResp: &qrdbtransport.FinishLoginResponse{
		Bearer:    "ok",
		Identity:  &qrdbtransport.Identity{Subject: "rest-user", TenantID: "t1"},
		ExpiresAt: time.Now().Add(time.Minute),
	}}
	handler, cleanup := setupCeremonyLoginHandler(t, fake)
	defer cleanup()

	body, _ := json.Marshal(ceremonyFinishLoginRequest{
		Type:      string(qrdbtransport.FactorTypeMTLS),
		LookupKey: []byte("spoofed-victim-hash"),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/finish", bytes.NewReader(body))
	req.TLS = &tls.ConnectionState{
		VerifiedChains: [][]*x509.Certificate{{cert}},
	}
	w := httptest.NewRecorder()
	handler.CeremonyFinishLogin(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	require.True(t, fake.finishInvoked)
	assert.NotEqual(t, []byte("spoofed-victim-hash"), fake.finishReq.LookupKey, "spoofed key forwarded")
	assert.Equal(t, wantKey[:], fake.finishReq.LookupKey, "peer cert must determine lookup key")
}

// TestCeremonyFinishLogin_PIV_ForwardsUnchanged asserts a PIV request forwards
// the lookup key intact (no peer cert needed).
func TestCeremonyFinishLogin_PIV_ForwardsUnchanged(t *testing.T) {
	fake := &ceremonyFakeLoginClient{finishResp: &qrdbtransport.FinishLoginResponse{Bearer: "ok", ExpiresAt: time.Now().Add(time.Minute)}}
	handler, cleanup := setupCeremonyLoginHandler(t, fake)
	defer cleanup()

	key := []byte("piv-slot-hash")
	body, _ := json.Marshal(ceremonyFinishLoginRequest{
		Type:      string(qrdbtransport.FactorTypePIV),
		LookupKey: key,
		Signature: []byte("sig"),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/finish", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.CeremonyFinishLogin(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Equal(t, key, fake.finishReq.LookupKey)
}

// TestCeremonyBeginLogin_Forwards asserts BeginLogin returns the server
// challenge.
func TestCeremonyBeginLogin_Forwards(t *testing.T) {
	fake := &ceremonyFakeLoginClient{}
	handler, cleanup := setupCeremonyLoginHandler(t, fake)
	defer cleanup()

	body, _ := json.Marshal(ceremonyBeginLoginRequest{Type: string(qrdbtransport.FactorTypeMTLS), Hint: "alice"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/begin", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.CeremonyBeginLogin(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp ceremonyBeginLoginResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "s1", resp.SessionID)
}
