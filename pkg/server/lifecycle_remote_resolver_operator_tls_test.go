//go:build !integration

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

package server

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// operatorTokenServer serves the token endpoint with an operator-provided,
// self-signed certificate for "localhost", and returns a token URL whose
// hostname matches that certificate along with the certificate file.
func operatorTokenServer(t *testing.T) (tokenURL, certFile string) {
	t.Helper()
	certFile, keyFile := writeOperatorPair(t, t.TempDir())
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"abc.def","token_type":"Bearer","expires_in":3600}`))
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	return "https://localhost:" + u.Port() + "/oauth2/token", certFile
}

// An own endpoint whose listener serves an operator certificate is refused
// by this server's CA chain, and trusted once the operator's CA is used.
func TestCentralOAuth2TokenSource_OperatorCertTrusted(t *testing.T) {
	tokenURL, certFile := operatorTokenServer(t)
	otherCA, _ := writeOperatorPair(t, t.TempDir())
	cred := &OrchestratorDaemonClient{ClientID: "client-id-1", ClientSecret: "client-secret-1"}

	ts := newCentralOAuth2TokenSource(cred, tokenURL, otherCA)
	ts.loopback = false
	ts.ownCA = true
	_, err := ts.Token(context.Background())
	require.Error(t, err, "this server's CA chain must not trust the operator certificate")

	ts.operatorCert = true
	ts.operatorCAFile = certFile
	tok, err := ts.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "abc.def", tok.AccessToken)
}

// With no operator CA file the exchange uses the system roots, which do not
// hold the self-signed certificate; a CA file without PEM is refused.
func TestCentralOAuth2TokenSource_OperatorCertKeepsSystemRoots(t *testing.T) {
	tokenURL, _ := operatorTokenServer(t)
	cred := &OrchestratorDaemonClient{ClientID: "client-id-1", ClientSecret: "client-secret-1"}

	ts := newCentralOAuth2TokenSource(cred, tokenURL, "/nonexistent/ca.crt")
	ts.loopback = false
	ts.ownCA = true
	ts.operatorCert = true
	ts.operatorCAFile = ""
	_, err := ts.Token(context.Background())
	require.Error(t, err, "the system roots must not trust a self-signed operator certificate")

	notPEM := filepath.Join(t.TempDir(), "operator-ca.crt")
	require.NoError(t, os.WriteFile(notPEM, []byte("not pem"), 0o600))
	ts.operatorCAFile = notPEM
	_, err = ts.Token(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no PEM certs in operator CA")
}

// The pool's token trust follows the certificate the listener serves, as
// recorded by initializeCA on each boot.
func TestServer_TokenTrustFollowsListenerCert(t *testing.T) {
	srv, cfg := bootedProvenanceServer(t)
	assert.False(t, srv.operatorListenerCert)

	p := newRemoteBackendPool(nil, &OrchestratorDaemonClient{ClientID: "c", ClientSecret: "s"},
		"https://10.20.30.40:7103/oauth2/token", "/nonexistent/ca.crt", config.SchedulerHostAffinityConfig{}, nil)
	srv.configureTokenTrust(p)
	ts := p.newTokenSource()
	assert.True(t, ts.ownCA)
	assert.False(t, ts.operatorCert)

	certFile, keyFile := writeOperatorPair(t, t.TempDir())
	cfg.Security.TLSCertFile = certFile
	cfg.Security.TLSKeyFile = keyFile
	cfg.Security.TLSCAFile = certFile
	require.NoError(t, srv.initializeCA())
	assert.True(t, srv.operatorListenerCert)
	assert.Equal(t, certFile, srv.operatorListenerCAFile)
	srv.configureTokenTrust(p)
	ts = p.newTokenSource()
	assert.True(t, ts.operatorCert)
	assert.Equal(t, certFile, ts.operatorCAFile)

	require.NoError(t, reboot(srv, certFile, keyFile))
	assert.True(t, srv.operatorListenerCert)
	assert.Empty(t, srv.operatorListenerCAFile, "the defaulted embedded chain is not the operator's CA")
}
