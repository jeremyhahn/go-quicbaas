//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

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

package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInitializeCA_AdvertisesGRPCALPN asserts that the server's TLS
// config advertises HTTP/2 (h2) and HTTP/1.1 ALPN tokens after CA
// initialization. Without h2 in NextProtos, gRPC clients (e.g. the
// agent event forwarder's PushEvents stream) hit
// "tls: no application protocol" alerts on TLS handshake.
func TestInitializeCA_AdvertisesGRPCALPN(t *testing.T) {
	t.Parallel()

	cfg := createFTConfig(t)
	cfg.Security.TLSEnabled = true
	cfg.Storage.DataDir = t.TempDir()
	cfg.CA.KeysDir = filepath.Join(t.TempDir(), "ca-keys")
	cfg.Security.TLSCertFile = filepath.Join(t.TempDir(), "server.crt")
	cfg.Security.TLSKeyFile = filepath.Join(t.TempDir(), "server.key")
	cfg.Security.TLSCAFile = filepath.Join(t.TempDir(), "ca-chain.crt")

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	require.NoError(t, srv.initializeCA(), "initializeCA must succeed")
	require.NotNil(t, srv.tlsConfig, "tlsConfig must be populated")

	assertALPNAdvertised(t, srv.tlsConfig)
}

// TestIssueTLSCertFromExternalCA_PreIssuedAdvertisesALPN asserts that
// the pre-issued TLS certificate path also installs h2 / http/1.1 ALPN
// on the server's tls.Config. Operators wiring an external CA via
// WithTLSCertificate would otherwise inherit the same ALPN bug fixed
// for the internal-CA path.
func TestIssueTLSCertFromExternalCA_PreIssuedAdvertisesALPN(t *testing.T) {
	t.Parallel()

	cfg := createFTConfig(t)
	cfg.Security.TLSEnabled = true
	cfg.Storage.DataDir = t.TempDir()

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Construct a self-signed cert in-memory and inject it via the
	// pre-issued external-CA path.
	cert := newSelfSignedTLSCert(t)
	srv.opts.tlsCertificate = &cert

	require.NoError(t, srv.issueTLSCertFromExternalCA())
	require.NotNil(t, srv.tlsConfig)

	assertALPNAdvertised(t, srv.tlsConfig)
}

// assertALPNAdvertised checks that the TLS config advertises gRPC's h2
// ALPN token. http/1.1 should also be advertised so connections that
// don't speak HTTP/2 still negotiate cleanly.
func assertALPNAdvertised(t *testing.T, cfg *tls.Config) {
	t.Helper()
	if !alpnContains(cfg.NextProtos, "h2") {
		t.Fatalf("TLS config must advertise h2 ALPN; got %v", cfg.NextProtos)
	}
	if !alpnContains(cfg.NextProtos, "http/1.1") {
		t.Fatalf("TLS config should advertise http/1.1 ALPN; got %v", cfg.NextProtos)
	}
	assert.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion,
		"TLS 1.3 minimum must be retained alongside ALPN advertisement")
}

func alpnContains(protos []string, want string) bool {
	for _, p := range protos {
		if p == want {
			return true
		}
	}
	return false
}

// newSelfSignedTLSCert returns a minimal in-memory self-signed certificate
// suitable for exercising TLS plumbing in unit tests. It is NOT a valid
// certificate for production use and exists solely so we can assert
// behaviour of code paths that consume *tls.Certificate without
// roundtripping through the embedded CA.
func newSelfSignedTLSCert(t *testing.T) tls.Certificate {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "alpn-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	return cert
}
