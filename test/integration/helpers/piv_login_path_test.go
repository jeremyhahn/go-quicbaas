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

//go:build integration

package helpers

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newFakeComposedNode stands in for a composed node: the xkms module's PIV
// login mints a token quicbaas routes refuse, quicbaas's own PIV login mints
// one they accept.
func newFakeComposedNode(t *testing.T) *httptest.Server {
	t.Helper()
	writeJSON := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login/piv", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"access_token": "xkms-module-token", "expires_in": 900})
	})
	mux.HandleFunc("/api/v1/iam/login/piv", func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "client certificate required"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"access_token": "iam-token", "expires_in": 900})
	})
	mux.HandleFunc("/api/v1/tenants/acme/provision", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer iam-token" {
			writeJSON(w, http.StatusOK, map[string]any{"tenant_id": "acme"})
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": `platformtoken: signing key "xkms-jwt-signing" not found in issuer JWKS`,
		})
	})
	srv := httptest.NewUnstartedServer(mux)
	srv.TLS = &tls.Config{ClientAuth: tls.RequestClientCert, MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

// writePIVFixture writes a self-signed client certificate, its key and the
// server's CA to a temporary directory.
func writePIVFixture(t *testing.T, srv *httptest.Server) (certPath, keyPath, caPath string, certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "piv-path-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})

	dir := t.TempDir()
	certPath = filepath.Join(dir, "client.crt")
	keyPath = filepath.Join(dir, "client.key")
	caPath = filepath.Join(dir, "ca.crt")
	require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyPath, keyPEM, 0o600))
	require.NoError(t, os.WriteFile(caPath, caPEM, 0o600))
	return certPath, keyPath, caPath, certPEM, keyPEM
}

// provisionStatus posts a tenant provision with bearer and returns the status.
func provisionStatus(t *testing.T, srv *httptest.Server, bearer string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/api/v1/tenants/acme/provision", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestAuthPIV_BearerIsAcceptedOnTenantProvision(t *testing.T) {
	srv := newFakeComposedNode(t)
	certPath, keyPath, caPath, _, _ := writePIVFixture(t, srv)

	bearer, _, err := authPIV(context.Background(), srv.URL, certPath, keyPath, caPath)
	require.NoError(t, err)
	require.Equal(t, "iam-token", bearer, "the system admin logs in at quicbaas's PIV handler")
	require.Equal(t, http.StatusOK, provisionStatus(t, srv, bearer))
}

func TestLoginWithCert_BearerIsAcceptedOnTenantProvision(t *testing.T) {
	srv := newFakeComposedNode(t)
	_, _, caPath, certPEM, keyPEM := writePIVFixture(t, srv)

	bearer, expiresIn, err := postLoginWithCert(context.Background(), srv.URL, certPEM, keyPEM, caPath)
	require.NoError(t, err)
	require.Equal(t, "iam-token", bearer, "a tenant admin logs in at quicbaas's PIV handler")
	require.Equal(t, 900, expiresIn)
	require.Equal(t, http.StatusOK, provisionStatus(t, srv, bearer))
}
