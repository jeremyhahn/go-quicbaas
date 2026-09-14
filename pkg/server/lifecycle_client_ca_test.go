//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

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

// lifecycle_client_ca_test.go covers the TLS client-CA wiring in initializeCA.
// The MCP transport authenticates agents by their mTLS client certificate
// (OU=agents) signed by the platform xkms CA, and go-qrdb resolves that
// principal's roles from the record its Subject CommonName names — see
// mcp_preset_resolver.go and iam.EnrollAgentSessionIdentity.
// For r.TLS.VerifiedChains to be populated the listener's ClientCAs pool must
// trust the platform CA in BOTH TLS postures: the opportunistic-mTLS default
// (TLSClientAuth=false) AND full/operator mTLS (TLSClientAuth=true). These tests
// assert that invariant against the real CA-issued in-memory tls.Config.

package server

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// assertPlatformCATrusted parses the platform xkms CA bundle and asserts every
// CA certificate in it chains against the listener's ClientCAs pool. A missing
// platform CA would fail x509 verification with "certificate signed by unknown
// authority", which is exactly the agent-mTLS regression these tests guard.
func assertPlatformCATrusted(t *testing.T, srv *Server, pool *x509.CertPool) {
	t.Helper()
	require.NotNil(t, pool, "ClientCAs pool must be populated so client certs chain-verify")

	caBundle, err := srv.xkmsCA.CABundle()
	require.NoError(t, err)
	require.NotEmpty(t, caBundle, "platform CA bundle must be non-empty")

	var roots []*x509.Certificate
	rest := caBundle
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, parseErr := x509.ParseCertificate(block.Bytes)
		require.NoError(t, parseErr)
		// The self-signed root anchors the chain; verifying it against the pool
		// proves the platform CA subject is trusted by the listener.
		if cert.CheckSignatureFrom(cert) == nil {
			roots = append(roots, cert)
		}
	}
	require.NotEmpty(t, roots, "platform CA bundle must contain a self-signed root")

	for _, root := range roots {
		_, verifyErr := root.Verify(x509.VerifyOptions{Roots: pool})
		require.NoErrorf(t, verifyErr,
			"platform CA root %q must chain-verify against the listener ClientCAs pool",
			root.Subject.CommonName)
	}
}

// newClientCATestServer builds a server wired for the internal-CA TLS path with
// the requested full-mTLS posture, then runs initializeCA so s.tlsConfig is the
// real OpaqueKey-backed config the REST/gRPC/QUIC/MCP listeners share.
func newClientCATestServer(t *testing.T, tlsClientAuth bool) *Server {
	t.Helper()

	cfg := createFTConfig(t)
	cfg.Security.TLSEnabled = true
	cfg.Security.TLSClientAuth = tlsClientAuth
	cfg.Storage.DataDir = t.TempDir()
	cfg.CA.KeysDir = filepath.Join(t.TempDir(), "ca-keys")
	cfg.Security.TLSCertFile = filepath.Join(t.TempDir(), "server.crt")
	cfg.Security.TLSKeyFile = filepath.Join(t.TempDir(), "server.key")
	cfg.Security.TLSCAFile = filepath.Join(t.TempDir(), "ca-chain.crt")

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	require.NoError(t, srv.initializeCA(), "initializeCA must succeed")
	require.NotNil(t, srv.tlsConfig, "tlsConfig must be populated")
	return srv
}

// TestInitializeCA_ClientCAsTrustPlatform_OptionalMTLS asserts that in the
// default opportunistic-mTLS posture (TLSClientAuth=false) the listener trusts
// the platform CA for client certs and solicits a cert opportunistically.
func TestInitializeCA_ClientCAsTrustPlatform_OptionalMTLS(t *testing.T) {
	t.Parallel()

	srv := newClientCATestServer(t, false)

	assertPlatformCATrusted(t, srv, srv.tlsConfig.ClientCAs)
	require.Equal(t, tls.VerifyClientCertIfGiven, srv.tlsConfig.ClientAuth,
		"optional-mTLS posture must solicit but not require a client cert")
}

// TestInitializeCA_ClientCAsTrustPlatform_FullMTLS asserts that in full/operator
// mTLS (TLSClientAuth=true) the listener STILL trusts the platform CA for client
// certs — the gap this change closes — and requires a CA-signed client cert so
// the agent mTLS auth on the shared MCP/gRPC/QUIC config keeps working.
func TestInitializeCA_ClientCAsTrustPlatform_FullMTLS(t *testing.T) {
	t.Parallel()

	srv := newClientCATestServer(t, true)

	assertPlatformCATrusted(t, srv, srv.tlsConfig.ClientCAs)
	require.Equal(t, tls.RequireAndVerifyClientCert, srv.tlsConfig.ClientAuth,
		"full-mTLS posture must require a CA-signed client cert")
}

// TestAppendPlatformClientCAs_PreservesOperatorPool asserts the helper APPENDS
// the platform CA to an operator-supplied client-CA pool rather than replacing
// it, so operator clients and platform agents both verify.
func TestAppendPlatformClientCAs_PreservesOperatorPool(t *testing.T) {
	t.Parallel()

	srv := newClientCATestServer(t, true)

	// Seed a distinct operator client-CA into a fresh pool, then append.
	operatorCA := buildSelfSignedTLSCert(t)
	operatorLeaf, err := x509.ParseCertificate(operatorCA.Certificate[0])
	require.NoError(t, err)

	pool := x509.NewCertPool()
	pool.AddCert(operatorLeaf)

	tlsConfig := &tls.Config{ClientCAs: pool}
	require.NoError(t, srv.appendPlatformClientCAs(tlsConfig))

	// Operator CA is still trusted...
	_, verifyErr := operatorLeaf.Verify(x509.VerifyOptions{Roots: tlsConfig.ClientCAs})
	require.NoError(t, verifyErr, "operator client CA must be preserved")
	// ...and the platform CA was appended alongside it.
	assertPlatformCATrusted(t, srv, tlsConfig.ClientCAs)
	require.Equal(t, tls.RequireAndVerifyClientCert, tlsConfig.ClientAuth)
}
