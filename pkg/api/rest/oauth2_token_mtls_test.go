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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// generateMTLSTestCert creates a self-signed ECDSA cert for mTLS tests.
func generateMTLSTestCert(t *testing.T) (*x509.Certificate, []byte) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-service-account"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}

	certPEM := make([]byte, 0, len(der)+30)
	_ = certPEM
	return cert, der
}

// makeMTLSRequest builds a POST /oauth2/token-mtls httptest.Request with an
// optional TLS ConnectionState populated with the given peer certificate.
func makeMTLSRequest(t *testing.T, form url.Values, peerCert *x509.Certificate) *http.Request {
	t.Helper()
	body := strings.NewReader(form.Encode())
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token-mtls", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if peerCert != nil {
		req.TLS = &tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{peerCert},
		}
	}
	return req
}

// TestTokenMTLS_NoCert verifies that a request without a client cert returns
// 401 invalid_client.
func TestTokenMTLS_NoCert(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	form := url.Values{
		"grant_type": {"client_credentials"},
		"client_id":  {"app_test_001"},
	}
	req := makeMTLSRequest(t, form, nil /* no cert */)
	w := httptest.NewRecorder()

	handler.TokenMTLS(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}

	var oauthErr OAuthError
	if err := json.Unmarshal(w.Body.Bytes(), &oauthErr); err == nil {
		if oauthErr.Code != ErrInvalidClient {
			t.Fatalf("expected %q error code, got %q", ErrInvalidClient, oauthErr.Code)
		}
	}
}

// TestTokenMTLS_MissingClientID verifies that a request without client_id
// returns 400.
func TestTokenMTLS_MissingClientID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	cert, _ := generateMTLSTestCert(t)

	form := url.Values{
		"grant_type": {"client_credentials"},
		// no client_id
	}
	req := makeMTLSRequest(t, form, cert)
	w := httptest.NewRecorder()

	handler.TokenMTLS(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestTokenMTLS_MissingGrantType verifies that a request without grant_type
// returns 400.
func TestTokenMTLS_MissingGrantType(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	cert, _ := generateMTLSTestCert(t)

	form := url.Values{
		"client_id": {"app_test_001"},
		// no grant_type
	}
	req := makeMTLSRequest(t, form, cert)
	w := httptest.NewRecorder()

	handler.TokenMTLS(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestTokenMTLS_ThumbprintMismatch verifies that a cert whose thumbprint does
// not match the registered client receives 401 invalid_client.
//
// This test wires a selfSignedAuth that always rejects the cert so it does
// not depend on a live OIDC provider.
func TestTokenMTLS_ThumbprintMismatch(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	cert, _ := generateMTLSTestCert(t)

	// Register a client whose pinned thumbprint is a completely different cert.
	otherCert, otherDER := generateMTLSTestCert(t)
	_ = otherDER

	// Build an app storage that contains the client with the OTHER cert pinned.
	app := &iam.Application{
		ClientID:                "app_mismatch",
		TenantID:                "tenant1",
		GrantTypes:              []string{iam.GrantTypeClientCredentials},
		TokenEndpointAuthMethod: iam.AuthMethodSelfSignedTLSClientAuth,
		MTLSCertThumbprint:      iam.CertThumbprintS256(otherCert),
	}
	storage := newTestMemAppStorage(app)
	handler.selfSignedAuth = iam.NewSelfSignedClientAuth(storage)

	form := url.Values{
		"grant_type": {"client_credentials"},
		"client_id":  {app.ClientID},
	}
	req := makeMTLSRequest(t, form, cert /* wrong cert */)
	w := httptest.NewRecorder()

	handler.TokenMTLS(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for thumbprint mismatch, got %d: %s", w.Code, w.Body.String())
	}
}

// TestSetMTLSAuthenticators verifies that authenticators can be wired after
// construction without panicking.
func TestSetMTLSAuthenticators(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	storage := newTestMemAppStorage()
	pki := iam.NewPKIClientAuth(x509.NewCertPool(), storage)
	self := iam.NewSelfSignedClientAuth(storage)

	handler.SetMTLSAuthenticators(pki, self)

	if handler.pkiAuth == nil {
		t.Fatal("pkiAuth not set")
	}
	if handler.selfSignedAuth == nil {
		t.Fatal("selfSignedAuth not set")
	}
}

// TestSetTPMAttestationResolver verifies that the resolver can be wired.
func TestSetTPMAttestationResolver(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	resolver := iam.NewTPMAttestationResolver(iam.TPMConfig{})
	handler.SetTPMAttestationResolver(resolver)

	if handler.tpmResolver == nil {
		t.Fatal("tpmResolver not set")
	}
}

// ---------------------------------------------------------------------------
// test-local minimal AppStorage
// ---------------------------------------------------------------------------

type testMemAppStorage struct {
	apps map[string]*iam.Application
}

func newTestMemAppStorage(apps ...*iam.Application) iam.AppStorage {
	s := &testMemAppStorage{apps: make(map[string]*iam.Application)}
	for _, a := range apps {
		s.apps[a.ClientID] = a
	}
	return s
}

func (s *testMemAppStorage) CreateApp(_ context.Context, app *iam.Application) error {
	s.apps[app.ClientID] = app
	return nil
}
func (s *testMemAppStorage) GetApp(_ context.Context, clientID string) (*iam.Application, error) {
	app, ok := s.apps[clientID]
	if !ok {
		return nil, iam.ErrMTLSClientNotFound
	}
	return app, nil
}
func (s *testMemAppStorage) GetAppByTenant(_ context.Context, _, clientID string) (*iam.Application, error) {
	return s.GetApp(context.Background(), clientID)
}
func (s *testMemAppStorage) UpdateApp(_ context.Context, app *iam.Application) error {
	s.apps[app.ClientID] = app
	return nil
}
func (s *testMemAppStorage) DeleteApp(_ context.Context, clientID string) error {
	delete(s.apps, clientID)
	return nil
}
func (s *testMemAppStorage) ListApps(_ context.Context, _ string, _ iam.ListOptions) ([]*iam.Application, error) {
	var out []*iam.Application
	for _, a := range s.apps {
		out = append(out, a)
	}
	return out, nil
}
func (s *testMemAppStorage) ValidateClientSecret(_ context.Context, clientID, secret string) (bool, error) {
	app, ok := s.apps[clientID]
	if !ok {
		return false, iam.ErrMTLSClientNotFound
	}
	return app.ClientSecret == secret, nil
}
