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

// Package main: coverage for agent_deploy_sdk_issuer.go's helpers. The
// full IssueAgentCertificate path is driven via an httptest.Server so we
// can exercise the marshal/POST/decode pipeline hermetically.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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

	pkgservice "github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// resolveServerBaseURL
// =============================================================================

// withAddress installs an --address value and restores the previous one
// on cleanup.
func withAddress(t *testing.T, value string) {
	t.Helper()
	orig := address
	address = value
	t.Cleanup(func() { address = orig })
}

func TestResolveServerBaseURL_RejectsEmpty(t *testing.T) {
	withAddress(t, "")
	_, err := resolveServerBaseURL()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--address is required")
}

func TestResolveServerBaseURL_RejectsUnixSocket(t *testing.T) {
	withAddress(t, "/var/run/quicbaas.sock")
	_, err := resolveServerBaseURL()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Unix socket")
}

func TestResolveServerBaseURL_Happy(t *testing.T) {
	withAddress(t, "https://example.com:8443")
	got, err := resolveServerBaseURL()
	require.NoError(t, err)
	assert.Equal(t, "https://example.com:8443", got)
}

// =============================================================================
// buildIssuerTLSConfig
// =============================================================================

// withTLSGlobals installs a clean set of TLS-related globals and restores
// the originals on cleanup.
func withTLSGlobals(t *testing.T) {
	t.Helper()
	origDataDir := dataDir
	origTLS := tlsEnabled
	origCA := tlsCA
	origCert := tlsCert
	origKey := tlsKey
	t.Cleanup(func() {
		dataDir = origDataDir
		tlsEnabled = origTLS
		tlsCA = origCA
		tlsCert = origCert
		tlsKey = origKey
	})
	dataDir = ""
	tlsEnabled = false
	tlsCA = ""
	tlsCert = ""
	tlsKey = ""
}

func TestBuildIssuerTLSConfig_NoFlagsReturnsNil(t *testing.T) {
	withTLSGlobals(t)
	cfg, err := buildIssuerTLSConfig()
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestBuildIssuerTLSConfig_AutoDiscoversCA(t *testing.T) {
	withTLSGlobals(t)
	dir := t.TempDir()
	dataDir = dir
	caDir := filepath.Join(dir, "certs")
	require.NoError(t, os.MkdirAll(caDir, 0o700))
	caPath := filepath.Join(caDir, "ca-chain.crt")
	caPEM := writeSelfSignedCertPEM(t, caPath)
	_ = caPEM

	cfg, err := buildIssuerTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.NotNil(t, cfg.RootCAs)
}

func TestBuildIssuerTLSConfig_ExplicitCA(t *testing.T) {
	withTLSGlobals(t)
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	writeSelfSignedCertPEM(t, caPath)
	tlsCA = caPath

	cfg, err := buildIssuerTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.NotNil(t, cfg.RootCAs)
}

func TestBuildIssuerTLSConfig_BadCAFile(t *testing.T) {
	withTLSGlobals(t)
	tlsCA = "/no/such/ca.pem"
	_, err := buildIssuerTLSConfig()
	require.Error(t, err)
}

func TestBuildIssuerTLSConfig_BadCABundle(t *testing.T) {
	withTLSGlobals(t)
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caPath, []byte("not a PEM"), 0o600))
	tlsCA = caPath
	_, err := buildIssuerTLSConfig()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidCABundle)
}

func TestBuildIssuerTLSConfig_LoadClientCert(t *testing.T) {
	withTLSGlobals(t)
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cli.crt")
	keyPath := filepath.Join(dir, "cli.key")
	writeSelfSignedKeyPair(t, certPath, keyPath)
	tlsCert = certPath
	tlsKey = keyPath

	cfg, err := buildIssuerTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Len(t, cfg.Certificates, 1)
}

func TestBuildIssuerTLSConfig_BadClientCert(t *testing.T) {
	withTLSGlobals(t)
	dir := t.TempDir()
	tlsCert = filepath.Join(dir, "missing.crt")
	tlsKey = filepath.Join(dir, "missing.key")
	_, err := buildIssuerTLSConfig()
	require.Error(t, err)
}

// =============================================================================
// decodeFirstPEMBlock
// =============================================================================

func TestDecodeFirstPEMBlock_Empty(t *testing.T) {
	assert.Nil(t, decodeFirstPEMBlock(nil))
	assert.Nil(t, decodeFirstPEMBlock([]byte("not a PEM")))
}

func TestDecodeFirstPEMBlock_DecodesFirstBlock(t *testing.T) {
	body := pem.EncodeToMemory(&pem.Block{Type: "TEST", Bytes: []byte{0x01, 0x02}})
	got := decodeFirstPEMBlock(body)
	assert.Equal(t, []byte{0x01, 0x02}, got)
}

// =============================================================================
// signCSR / fetchCABundle / IssueAgentCertificate via httptest
// =============================================================================

func TestSDKAgentCertIssuer_SignCSR_Happy(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/xkms/ca/sign", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&xkmsSignCSRResponse{
			CertificatePEM: []byte("LEAF-PEM"),
			ChainPEM:       []byte("CHAIN-PEM"),
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	i := &sdkAgentCertIssuer{baseURL: srv.URL, httpClient: srv.Client()}
	cert, chain, err := i.signCSR(context.Background(), &xkmsSignCSRRequest{})
	require.NoError(t, err)
	assert.Equal(t, []byte("LEAF-PEM"), cert)
	assert.Equal(t, []byte("CHAIN-PEM"), chain)
}

func TestSDKAgentCertIssuer_SignCSR_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer srv.Close()

	i := &sdkAgentCertIssuer{baseURL: srv.URL, httpClient: srv.Client()}
	_, _, err := i.signCSR(context.Background(), &xkmsSignCSRRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 403")
}

func TestSDKAgentCertIssuer_SignCSR_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	}))
	defer srv.Close()

	i := &sdkAgentCertIssuer{baseURL: srv.URL, httpClient: srv.Client()}
	_, _, err := i.signCSR(context.Background(), &xkmsSignCSRRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode sign-csr response")
}

func TestSDKAgentCertIssuer_FetchCABundle_Happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&xkmsCABundleResponse{BundlePEM: []byte("BUNDLE-PEM")})
	}))
	defer srv.Close()

	i := &sdkAgentCertIssuer{baseURL: srv.URL, httpClient: srv.Client()}
	got, err := i.fetchCABundle(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []byte("BUNDLE-PEM"), got)
}

func TestSDKAgentCertIssuer_FetchCABundle_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	i := &sdkAgentCertIssuer{baseURL: srv.URL, httpClient: srv.Client()}
	_, err := i.fetchCABundle(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 503")
}

func TestSDKAgentCertIssuer_FetchCABundle_EmptyBundle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&xkmsCABundleResponse{})
	}))
	defer srv.Close()

	i := &sdkAgentCertIssuer{baseURL: srv.URL, httpClient: srv.Client()}
	_, err := i.fetchCABundle(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyCABundle)
}

func TestSDKAgentCertIssuer_IssueAgentCertificate_NilRequest(t *testing.T) {
	i := &sdkAgentCertIssuer{}
	_, err := i.IssueAgentCertificate(context.Background(), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, pkgservice.ErrAgentCertRequestNil)
}

func TestSDKAgentCertIssuer_IssueAgentCertificate_MissingTenant(t *testing.T) {
	i := &sdkAgentCertIssuer{}
	_, err := i.IssueAgentCertificate(context.Background(), &pkgservice.AgentCertificateRequest{Hostname: "h"})
	require.Error(t, err)
	var typed *pkgservice.AgentCertRequestFieldError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "tenant_id", typed.Field)
}

func TestSDKAgentCertIssuer_IssueAgentCertificate_MissingHostname(t *testing.T) {
	i := &sdkAgentCertIssuer{}
	_, err := i.IssueAgentCertificate(context.Background(), &pkgservice.AgentCertificateRequest{TenantID: "t"})
	require.Error(t, err)
	var typed *pkgservice.AgentCertRequestFieldError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "hostname", typed.Field)
}

func TestSDKAgentCertIssuer_IssueAgentCertificate_HappyPathWithChain(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/xkms/ca/sign", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&xkmsSignCSRResponse{
			CertificatePEM: leafPEMForTest(t),
			ChainPEM:       chainPEMForTest(t),
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	i := &sdkAgentCertIssuer{baseURL: srv.URL, httpClient: srv.Client()}
	out, err := i.IssueAgentCertificate(context.Background(), &pkgservice.AgentCertificateRequest{
		TenantID:      "tenant",
		Hostname:      "agent01.example.com",
		ExtraDNSNames: []string{"alt.example.com"},
		ValidityDays:  30,
	})
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.NotEmpty(t, out.CertPEM)
	assert.NotNil(t, out.PrivateKey)
}

// =============================================================================
// helpers
// =============================================================================

// writeSelfSignedCertPEM writes a self-signed certificate to path and
// returns the PEM bytes for callers that need them.
func writeSelfSignedCertPEM(t *testing.T, path string) []byte {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NoError(t, os.WriteFile(path, pemBytes, 0o600))
	return pemBytes
}

// writeSelfSignedKeyPair writes a cert + matching ECDSA private key for
// use as --tls-cert/--tls-key inputs.
func writeSelfSignedKeyPair(t *testing.T, certPath, keyPath string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))

	privDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER})
	require.NoError(t, os.WriteFile(keyPath, keyPEM, 0o600))
}

// leafPEMForTest returns a minimal CERTIFICATE PEM payload for the
// sign-csr happy-path test. We don't need to actually parse it — the
// IssueAgentCertificate code path treats it as opaque bytes.
func leafPEMForTest(t *testing.T) []byte {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func chainPEMForTest(t *testing.T) []byte {
	return leafPEMForTest(t)
}
