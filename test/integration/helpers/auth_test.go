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
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// pivLoginFiles writes a self-signed client certificate, its key and the
// server's CA certificate into a temporary directory and returns their paths.
func pivLoginFiles(t *testing.T, server *httptest.Server) (certPath, keyPath, caPath string) {
	t.Helper()
	dir := t.TempDir()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "piv-login-test"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	certPath = filepath.Join(dir, "client.crt")
	keyPath = filepath.Join(dir, "client.key")
	caPath = filepath.Join(dir, "ca.crt")
	files := map[string]*pem.Block{
		certPath: {Type: "CERTIFICATE", Bytes: certDER},
		keyPath:  {Type: "EC PRIVATE KEY", Bytes: keyDER},
		caPath:   {Type: "CERTIFICATE", Bytes: server.Certificate().Raw},
	}
	for path, block := range files {
		if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return certPath, keyPath, caPath
}

// newPIVLoginServer starts a TLS server that requests a client certificate,
// records the path of every request and answers with the given status and body.
func newPIVLoginServer(t *testing.T, status int, body string) (*httptest.Server, chan string) {
	t.Helper()
	paths := make(chan string, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	server.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, paths
}

// The platform validator accepts only tokens the quicbaas OIDC provider
// issued, so the helper must log in through the IAM PIV route, not the xkms
// session login.
func TestAuthPIV_LogsInThroughIAMRoute(t *testing.T) {
	server, paths := newPIVLoginServer(t, http.StatusOK,
		`{"access_token":"oidc-token","token_type":"Bearer","expires_in":900}`)
	certPath, keyPath, caPath := pivLoginFiles(t, server)

	bearer, expiry, err := authPIV(context.Background(), server.URL, certPath, keyPath, caPath)
	if err != nil {
		t.Fatalf("authPIV: %v", err)
	}
	if got := <-paths; got != "/api/v1/iam/login/piv" {
		t.Errorf("login path = %q, want /api/v1/iam/login/piv", got)
	}
	if bearer != "oidc-token" {
		t.Errorf("bearer = %q, want oidc-token", bearer)
	}
	if !expiry.After(time.Now()) {
		t.Errorf("expiry %v is not in the future", expiry)
	}
}

func TestAuthPIV_RejectedLoginReturnsBadStatus(t *testing.T) {
	server, paths := newPIVLoginServer(t, http.StatusUnauthorized, `{"error":"unauthorized"}`)
	certPath, keyPath, caPath := pivLoginFiles(t, server)

	_, _, err := authPIV(context.Background(), server.URL, certPath, keyPath, caPath)
	<-paths
	var badStatus *AuthBadStatusError
	if !errors.As(err, &badStatus) {
		t.Fatalf("authPIV error = %v, want *AuthBadStatusError", err)
	}
	if badStatus.Status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", badStatus.Status, http.StatusUnauthorized)
	}
}
