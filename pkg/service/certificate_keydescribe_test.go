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

package service

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// certWithPubKey builds a minimal self-signed certificate carrying the given
// key pair so describePublicKey can be exercised for every key algorithm.
func certWithPubKey(t *testing.T, pub, priv any) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "key-describe"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

func TestDescribePublicKey(t *testing.T) {
	t.Run("RSA", func(t *testing.T) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		cert := certWithPubKey(t, &key.PublicKey, key)
		algo, bits := describePublicKey(cert)
		assert.Equal(t, "RSA", algo)
		assert.Equal(t, 2048, bits)
	})

	t.Run("ECDSA", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		cert := certWithPubKey(t, &key.PublicKey, key)
		algo, bits := describePublicKey(cert)
		assert.Equal(t, "ECDSA", algo)
		assert.Equal(t, 256, bits)
	})

	t.Run("ECDSA nil curve", func(t *testing.T) {
		// Directly exercise the nil-curve guard via a synthetic cert.
		cert := &x509.Certificate{PublicKey: &ecdsa.PublicKey{}}
		algo, bits := describePublicKey(cert)
		assert.Equal(t, "ECDSA", algo)
		assert.Equal(t, 0, bits)
	})

	t.Run("Ed25519", func(t *testing.T) {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		cert := certWithPubKey(t, pub, priv)
		algo, bits := describePublicKey(cert)
		assert.Equal(t, "Ed25519", algo)
		assert.Equal(t, 256, bits)
	})

	t.Run("Unknown", func(t *testing.T) {
		cert := &x509.Certificate{PublicKey: "not-a-key"}
		algo, bits := describePublicKey(cert)
		assert.Equal(t, "Unknown", algo)
		assert.Equal(t, 0, bits)
	})
}

func TestDecodeFirstCertPEM(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	cert := certWithPubKey(t, &key.PublicKey, key)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})

	t.Run("skips non-certificate blocks", func(t *testing.T) {
		// A leading PRIVATE KEY block must be skipped before the CERTIFICATE.
		var buf []byte
		buf = append(buf, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("xx")})...)
		buf = append(buf, certPEM...)
		got, err := decodeFirstCertPEM(buf)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "key-describe", got.Subject.CommonName)
	})

	t.Run("invalid PEM", func(t *testing.T) {
		got, err := decodeFirstCertPEM([]byte("not pem at all"))
		require.Error(t, err)
		assert.Nil(t, got)
	})

	t.Run("malformed certificate bytes", func(t *testing.T) {
		bad := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("garbage")})
		got, err := decodeFirstCertPEM(bad)
		require.Error(t, err)
		assert.Nil(t, got)
	})
}
