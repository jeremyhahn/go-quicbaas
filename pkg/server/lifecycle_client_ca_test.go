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
	"os"
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
func newClientCATestServer(t *testing.T, tlsClientAuth bool, operatorCAFile string) *Server {
	t.Helper()

	cfg := createFTConfig(t)
	cfg.Security.TLSEnabled = true
	cfg.Security.TLSClientAuth = tlsClientAuth
	cfg.Storage.DataDir = t.TempDir()
	cfg.CA.KeysDir = filepath.Join(t.TempDir(), "ca-keys")
	// An empty operatorCAFile leaves security.tls_ca_file to default to the
	// server's own chain under the data dir.
	cfg.Security.TLSCAFile = operatorCAFile

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

	srv := newClientCATestServer(t, false, "")

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

	srv := newClientCATestServer(t, true, "")

	assertPlatformCATrusted(t, srv, srv.tlsConfig.ClientCAs)
	require.Equal(t, tls.RequireAndVerifyClientCert, srv.tlsConfig.ClientAuth,
		"full-mTLS posture must require a CA-signed client cert")
}

// TestAppendPlatformClientCAs_PreservesOperatorPool asserts the helper APPENDS
// the platform CA to an operator-supplied client-CA pool rather than replacing
// it, so operator clients and platform agents both verify.
func TestAppendPlatformClientCAs_PreservesOperatorPool(t *testing.T) {
	t.Parallel()

	operator := newTestAnchorCA(t, "operator CA")
	operatorCAFile := operator.writePEM(t)
	before, err := os.ReadFile(operatorCAFile)
	require.NoError(t, err)

	srv := newClientCATestServer(t, true, operatorCAFile)

	// Operator CA is still trusted...
	_, verifyErr := operator.cert.Verify(x509.VerifyOptions{Roots: srv.tlsConfig.ClientCAs})
	require.NoError(t, verifyErr, "operator client CA must be preserved")
	// ...and the platform CA was appended alongside it.
	assertPlatformCATrusted(t, srv, srv.tlsConfig.ClientCAs)
	require.Equal(t, tls.RequireAndVerifyClientCert, srv.tlsConfig.ClientAuth)

	after, err := os.ReadFile(operatorCAFile)
	require.NoError(t, err)
	require.Equal(t, before, after, "the self-issued boot must never overwrite an operator tls_ca_file")
}

// TestInitializeCA_OperatorTLSCAFileUnchanged_OptionalMTLS asserts the
// opportunistic-mTLS self-issued boot leaves an operator tls_ca_file
// byte-identical while still trusting it in the handshake.
func TestInitializeCA_OperatorTLSCAFileUnchanged_OptionalMTLS(t *testing.T) {
	t.Parallel()

	operator := newTestAnchorCA(t, "operator CA")
	operatorCAFile := operator.writePEM(t)
	before, err := os.ReadFile(operatorCAFile)
	require.NoError(t, err)

	srv := newClientCATestServer(t, false, operatorCAFile)

	after, err := os.ReadFile(operatorCAFile)
	require.NoError(t, err)
	require.Equal(t, before, after, "the self-issued boot must never overwrite an operator tls_ca_file")
	_, verifyErr := operator.cert.Verify(x509.VerifyOptions{Roots: srv.tlsConfig.ClientCAs})
	require.NoError(t, verifyErr)
	assertPlatformCATrusted(t, srv, srv.tlsConfig.ClientCAs)
}

// serverHandshake runs one TLS handshake against the unified listener config
// and returns the number of client chains the server verified, with the
// server side's handshake error. A nil clientCert sends no certificate.
func serverHandshake(t *testing.T, srv *Server, clientCert *tls.Certificate) (int, error) {
	t.Helper()

	listener, err := tls.Listen("tcp", "127.0.0.1:0", srv.unifiedServerTLSConfig())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })

	type handshakeResult struct {
		verifiedChains int
		err            error
	}
	result := make(chan handshakeResult, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			result <- handshakeResult{err: acceptErr}
			return
		}
		defer conn.Close()
		tlsConn := conn.(*tls.Conn)
		handshakeErr := tlsConn.Handshake()
		result <- handshakeResult{
			verifiedChains: len(tlsConn.ConnectionState().VerifiedChains),
			err:            handshakeErr,
		}
	}()

	caBundle, err := srv.xkmsCA.CABundle()
	require.NoError(t, err)
	serverRoots := x509.NewCertPool()
	require.True(t, serverRoots.AppendCertsFromPEM(caBundle), "platform CA bundle must parse")

	clientConfig := &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    serverRoots,
		// Present the certificate even when its issuer is not among the
		// server's acceptable CAs, so the server's verification is exercised.
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			if clientCert == nil {
				return &tls.Certificate{}, nil
			}
			return clientCert, nil
		},
	}
	conn, err := tls.Dial("tcp", listener.Addr().String(), clientConfig)
	if err == nil {
		defer conn.Close()
	}
	got := <-result
	return got.verifiedChains, got.err
}

func TestUnifiedHandshake_AcceptsTLSCAFileClientCert(t *testing.T) {
	t.Parallel()

	operator := newTestAnchorCA(t, "operator CA")
	srv := newClientCATestServer(t, false, operator.writePEM(t))
	client := operator.issueClient(t, "agent")

	verified, err := serverHandshake(t, srv, &client)
	require.NoError(t, err, "a client cert from security.tls_ca_file must pass the handshake")
	require.Positive(t, verified, "the server must verify the client chain")
}

func TestUnifiedHandshake_RefusesUnlistedCA(t *testing.T) {
	t.Parallel()

	operator := newTestAnchorCA(t, "operator CA")
	unlisted := newTestAnchorCA(t, "unlisted CA")
	srv := newClientCATestServer(t, false, operator.writePEM(t))
	client := unlisted.issueClient(t, "agent")

	_, err := serverHandshake(t, srv, &client)
	require.Error(t, err, "a client cert from an unlisted CA must fail the handshake")
}

func TestUnifiedHandshake_RequireMTLSRefusesNoCert(t *testing.T) {
	t.Parallel()

	operator := newTestAnchorCA(t, "operator CA")
	cfg := createFTConfig(t)
	cfg.Security.TLSEnabled = true
	cfg.Security.RequireMTLS = true
	cfg.Storage.DataDir = t.TempDir()
	cfg.CA.KeysDir = filepath.Join(t.TempDir(), "ca-keys")
	cfg.Security.TLSCAFile = operator.writePEM(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, srv.initializeCA())

	_, err = serverHandshake(t, srv, nil)
	require.Error(t, err, "RequireMTLS must refuse a client that presents no certificate")
	client := operator.issueClient(t, "agent")
	verified, err := serverHandshake(t, srv, &client)
	require.NoError(t, err)
	require.Positive(t, verified)
}
