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

//go:build rest

package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// trustTestCA is a CA that issues client certificates for the unified
// listener trust tests.
type trustTestCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newTrustTestCA(t *testing.T, name string) *trustTestCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	return &trustTestCA{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// issueClient returns a client-auth leaf signed by the CA.
func (ca *trustTestCA) issueClient(t *testing.T, name string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}

func writeTrustTestFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// unifiedTrustFixture is a server whose s.tlsConfig trusts only the platform
// CA, configured with an operator CA and a PIV CA issued separately from it.
type unifiedTrustFixture struct {
	srv                      *Server
	platform, operator, piv  *trustTestCA
	serverLeaf               *x509.Certificate
	publishedBefore          *tls.Config
	publishedPoolBeforeTrust *x509.CertPool
}

func newUnifiedTrustFixture(t *testing.T) *unifiedTrustFixture {
	t.Helper()
	f := &unifiedTrustFixture{
		srv:      withTempServer(t),
		platform: newTrustTestCA(t, "platform-ca"),
		operator: newTrustTestCA(t, "operator-ca"),
		piv:      newTrustTestCA(t, "piv-ca"),
	}
	serverCert, _, serverLeaf := newTLSTestCert(t, "server")
	f.serverLeaf = serverLeaf

	pool := x509.NewCertPool()
	pool.AddCert(f.platform.cert)
	f.srv.tlsConfig = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    pool,
		ClientAuth:   tls.VerifyClientCertIfGiven,
		MinVersion:   tls.VersionTLS13,
	}
	f.publishedBefore = f.srv.tlsConfig
	f.publishedPoolBeforeTrust = pool.Clone()

	f.srv.config.IAM.MTLS.TLSClientAuthCAFile = writeTrustTestFile(t, "operator-ca.pem", f.operator.pem)
	f.srv.config.IAM.PIVLogin.Enabled = true
	f.srv.config.IAM.PIVLogin.CAFile = writeTrustTestFile(t, "piv-ca.pem", f.piv.pem)
	return f
}

// servedConfig returns the config the unified listeners hand a client.
func (f *unifiedTrustFixture) servedConfig(t *testing.T) *tls.Config {
	t.Helper()
	cfg, err := f.srv.unifiedServerTLSConfig().GetConfigForClient(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetConfigForClient: %v", err)
	}
	return cfg
}

func verifiesAgainst(pool *x509.CertPool, cert tls.Certificate) bool {
	_, err := cert.Leaf.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	return err == nil
}

func TestTrustIAMClientCAs_UnifiedConfigTrustsOperatorAndPIVCAs(t *testing.T) {
	f := newUnifiedTrustFixture(t)

	if err := f.srv.trustIAMClientCAs(); err != nil {
		t.Fatalf("trustIAMClientCAs: %v", err)
	}
	served := f.servedConfig(t)

	for name, ca := range map[string]*trustTestCA{"platform": f.platform, "operator": f.operator, "piv": f.piv} {
		if !verifiesAgainst(served.ClientCAs, ca.issueClient(t, name+"-client")) {
			t.Errorf("served ClientCAs do not trust a %s-issued client certificate", name)
		}
	}
	if served.ClientAuth != tls.VerifyClientCertIfGiven {
		t.Errorf("served ClientAuth = %v, want VerifyClientCertIfGiven", served.ClientAuth)
	}
	if served == f.publishedBefore {
		t.Fatal("trustIAMClientCAs mutated the published config instead of swapping in a new one")
	}
	if !f.publishedBefore.ClientCAs.Equal(f.publishedPoolBeforeTrust) {
		t.Error("trustIAMClientCAs mutated the pool of the config it replaced")
	}
}

func TestTrustIAMClientCAs_KeepsFullMTLSClientAuth(t *testing.T) {
	f := newUnifiedTrustFixture(t)
	f.srv.tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert

	if err := f.srv.trustIAMClientCAs(); err != nil {
		t.Fatalf("trustIAMClientCAs: %v", err)
	}
	if got := f.servedConfig(t).ClientAuth; got != tls.RequireAndVerifyClientCert {
		t.Errorf("served ClientAuth = %v, want RequireAndVerifyClientCert kept", got)
	}
}

// handshakeUnified runs one TLS handshake over net.Pipe through the unified
// config and returns the server-side result.
func handshakeUnified(t *testing.T, f *unifiedTrustFixture, clientCert tls.Certificate) error {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	roots := x509.NewCertPool()
	roots.AddCert(f.serverLeaf)
	client := tls.Client(clientConn, &tls.Config{
		ServerName: "127.0.0.1",
		RootCAs:    roots,
		// Always present the certificate: with Certificates, a Go client
		// sends none when it matches no CA the server advertises.
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return &clientCert, nil
		},
		MinVersion: tls.VersionTLS13,
	})
	server := tls.Server(serverConn, f.srv.unifiedServerTLSConfig())

	clientDone := make(chan error, 1)
	go func() {
		err := client.Handshake()
		if err == nil {
			// TLS 1.3 clients finish before the server verifies them; read
			// until the server's verdict closes the pipe.
			buf := make([]byte, 1)
			_, err = client.Read(buf)
		}
		clientDone <- err
	}()
	serverErr := server.Handshake()
	if closeErr := server.Close(); closeErr != nil && serverErr == nil {
		t.Errorf("close server conn: %v", closeErr)
	}
	clientErr := <-clientDone
	if closeErr := client.Close(); closeErr != nil && clientErr == nil {
		t.Errorf("close client conn: %v", closeErr)
	}
	return serverErr
}

func TestTrustIAMClientCAs_HandshakeWithOperatorIssuedClientCert(t *testing.T) {
	f := newUnifiedTrustFixture(t)
	if err := f.srv.trustIAMClientCAs(); err != nil {
		t.Fatalf("trustIAMClientCAs: %v", err)
	}

	if err := handshakeUnified(t, f, f.operator.issueClient(t, "operator-client")); err != nil {
		t.Errorf("handshake with an operator-issued client certificate failed: %v", err)
	}
	unknown := newTrustTestCA(t, "unknown-ca")
	err := handshakeUnified(t, f, unknown.issueClient(t, "stranger"))
	var unknownAuthority x509.UnknownAuthorityError
	if !errors.As(err, &unknownAuthority) {
		t.Errorf("handshake with an unknown-CA client certificate = %v, want x509.UnknownAuthorityError", err)
	}
}

func TestTrustIAMClientCAs_BuildRESTServerTwiceAddsNoRootsAndMutatesNoServedPool(t *testing.T) {
	f := newUnifiedTrustFixture(t)
	if err := f.srv.trustIAMClientCAs(); err != nil {
		t.Fatalf("trustIAMClientCAs: %v", err)
	}
	served := f.servedConfig(t)
	servedPool := served.ClientCAs.Clone()

	// Capture the TLS config each build hands the REST server, then stop the
	// build there: the clone is all this test is about.
	errStop := errors.New("stop after the TLS config is captured")
	var captured []*tls.Config
	orig := newREST
	newREST = func(_ service.QuicBaaS, cfg *restapi.Config, _ *logger.Logger) (*restapi.Server, error) {
		captured = append(captured, cfg.TLSConfig)
		return nil, errStop
	}
	t.Cleanup(func() { newREST = orig })

	for i := 0; i < 2; i++ {
		if _, err := f.srv.buildRESTServer(); !errors.Is(err, errStop) {
			t.Fatalf("buildRESTServer #%d = %v, want the captured stop", i+1, err)
		}
	}
	if len(captured) != 2 {
		t.Fatalf("captured %d TLS configs, want 2", len(captured))
	}
	if captured[0].ClientCAs == captured[1].ClientCAs || captured[0].ClientCAs == served.ClientCAs {
		t.Error("REST builds share a ClientCAs pool instead of each having its own")
	}
	for i, cfg := range captured {
		if !cfg.ClientCAs.Equal(servedPool) {
			t.Errorf("build #%d ClientCAs differ from the served pool: roots were added", i+1)
		}
	}
	if !served.ClientCAs.Equal(servedPool) {
		t.Error("building the REST server mutated the pool the unified listeners serve")
	}
	if f.servedConfig(t) != served {
		t.Error("building the REST server replaced the served config")
	}
}

func TestTrustIAMClientCAs_MalformedOperatorCAFileIsRefused(t *testing.T) {
	f := newUnifiedTrustFixture(t)
	f.srv.config.IAM.MTLS.TLSClientAuthCAFile = writeTrustTestFile(t, "bad.pem", []byte("not a certificate"))

	err := f.srv.trustIAMClientCAs()
	var caErr *MTLSClientCAsError
	if !errors.As(err, &caErr) {
		t.Fatalf("trustIAMClientCAs = %v, want *MTLSClientCAsError", err)
	}
	var emptyErr *CertPoolEmptyError
	if !errors.As(err, &emptyErr) {
		t.Errorf("trustIAMClientCAs = %v, want a *CertPoolEmptyError cause", err)
	}
	if f.servedConfig(t) != f.publishedBefore {
		t.Error("a refused operator CA file still replaced the served config")
	}
}

func TestTrustIAMClientCAs_NoServerTLSConfigIsNoOp(t *testing.T) {
	srv := withTempServer(t)
	srv.tlsConfig = nil
	if err := srv.trustIAMClientCAs(); err != nil {
		t.Fatalf("trustIAMClientCAs without a TLS config = %v, want nil", err)
	}
	if srv.serverTLSConfigRef() != nil {
		t.Error("trustIAMClientCAs published a config where there was none")
	}
}
