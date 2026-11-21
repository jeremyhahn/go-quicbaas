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
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// tenantCertLoginPath is the route that serves a tenant-scoped cert login on
// a composed node: quicbaas's OIDC-session PIVLogin (pkg/api/rest/routes_iam.go
// iamManagedRouteSpecs). /api/v1/auth/login/piv belongs to xkms's module,
// which does not resolve tenant-scoped users.
const tenantCertLoginPath = "/api/v1/iam/login/piv"

// newClientCertPair mints a self-signed ECDSA client certificate and key, PEM
// encoded, for the mTLS handshake postLoginWithCert performs.
func newClientCertPair(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "tenant-admin"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

// newLoginServer starts a TLS server that requires a client certificate,
// serves handler, and returns the server plus a CA file trusting it.
func newLoginServer(t *testing.T, handler http.Handler) (*httptest.Server, string) {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert, MinVersion: tls.VersionTLS13}
	server.StartTLS()
	t.Cleanup(server.Close)

	caPath := filepath.Join(t.TempDir(), "ca.pem")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	require.NoError(t, os.WriteFile(caPath, caPEM, 0o600))
	return server, caPath
}

func TestPostLoginWithCert_UsesTenantCertLoginRoute(t *testing.T) {
	var gotMethod, gotPath string
	server, caPath := newLoginServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if r.URL.Path != tenantCertLoginPath {
			http.Error(w, `{"error":"unauthorized","message":"authentication failed","code":401}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"access_token":"tenant-bearer","token_type":"Bearer","expires_in":900}`))
		require.NoError(t, err)
	}))
	certPEM, keyPEM := newClientCertPair(t)

	bearer, expiresIn, err := postLoginWithCert(context.Background(), server.URL, certPEM, keyPEM, caPath)

	require.NoError(t, err)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, tenantCertLoginPath, gotPath, "a tenant-scoped cert login must use quicbaas's PIVLogin route")
	require.Equal(t, "tenant-bearer", bearer)
	require.Equal(t, 900, expiresIn)
}

func TestPostLoginWithCert_RejectedLoginIsTyped(t *testing.T) {
	server, caPath := newLoginServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}))
	certPEM, keyPEM := newClientCertPair(t)

	bearer, _, err := postLoginWithCert(context.Background(), server.URL, certPEM, keyPEM, caPath)

	require.Empty(t, bearer)
	var statusErr *AuthBadStatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusUnauthorized, statusErr.Status)
	require.Equal(t, server.URL+tenantCertLoginPath, statusErr.URL)
}
