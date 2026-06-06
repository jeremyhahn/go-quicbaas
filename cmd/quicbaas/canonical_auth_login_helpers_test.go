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

// Package main: helper-function coverage for canonical_auth_login.go.
package main

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// stringifyCanonicalMetadata
// =============================================================================

func TestStringifyCanonicalMetadata_Strings(t *testing.T) {
	in := map[string]any{
		"a": "alpha",
		"b": "beta",
	}
	got := stringifyCanonicalMetadata(in)
	assert.Equal(t, "alpha", got["a"])
	assert.Equal(t, "beta", got["b"])
}

func TestStringifyCanonicalMetadata_BytesAndOther(t *testing.T) {
	in := map[string]any{
		"bytes": []byte("hi"),
		"int":   42,
		"bool":  true,
	}
	got := stringifyCanonicalMetadata(in)
	assert.Equal(t, "hi", got["bytes"])
	assert.Equal(t, "42", got["int"])
	assert.Equal(t, "true", got["bool"])
}

func TestStringifyCanonicalMetadata_Empty(t *testing.T) {
	got := stringifyCanonicalMetadata(nil)
	assert.Empty(t, got)
}

// =============================================================================
// pivAlgoFromCanonicalCert
// =============================================================================

func TestPIVAlgoFromCanonicalCert_NilCert(t *testing.T) {
	assert.Equal(t, "ecdsa", pivAlgoFromCanonicalCert(nil))
}

func TestPIVAlgoFromCanonicalCert_RSA(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	cert := mintCertFromPub(t, &priv.PublicKey, priv)
	assert.Equal(t, "rsa-pkcs1", pivAlgoFromCanonicalCert(cert))
}

func TestPIVAlgoFromCanonicalCert_ECDSA(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	cert := mintCertFromPub(t, &priv.PublicKey, priv)
	assert.Equal(t, "ecdsa", pivAlgoFromCanonicalCert(cert))
}

func TestPIVAlgoFromCanonicalCert_Ed25519(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	cert := mintCertFromPub(t, pub, priv)
	assert.Equal(t, "ed25519", pivAlgoFromCanonicalCert(cert))
}

// =============================================================================
// expandCanonicalHome
// =============================================================================

func TestExpandCanonicalHome_Empty(t *testing.T) {
	assert.Equal(t, "", expandCanonicalHome(""))
}

func TestExpandCanonicalHome_NoTilde(t *testing.T) {
	assert.Equal(t, "/etc/passwd", expandCanonicalHome("/etc/passwd"))
}

func TestExpandCanonicalHome_TildeOnly(t *testing.T) {
	t.Setenv("HOME", "/home/alice")
	assert.Equal(t, "/home/alice", expandCanonicalHome("~"))
}

func TestExpandCanonicalHome_TildeSlash(t *testing.T) {
	t.Setenv("HOME", "/home/alice")
	assert.Equal(t, "/home/alice/.quicbaas", expandCanonicalHome("~/.quicbaas"))
}

func TestExpandCanonicalHome_TildeWithoutSlash(t *testing.T) {
	t.Setenv("HOME", "/home/alice")
	// "~foo" is returned unchanged (not expanded).
	assert.Equal(t, "~foo", expandCanonicalHome("~foo"))
}

// =============================================================================
// resolveCanonicalDefaultClientCert
// =============================================================================

func TestResolveCanonicalDefaultClientCert_ExplicitPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cert.pem")
	require.NoError(t, os.WriteFile(p, []byte("x"), 0o600))
	got := resolveCanonicalDefaultClientCert(p)
	assert.Equal(t, p, got)
}

func TestResolveCanonicalDefaultClientCert_ExplicitMissingFile(t *testing.T) {
	got := resolveCanonicalDefaultClientCert("/no/such/cert.pem")
	assert.Equal(t, "", got)
}

func TestResolveCanonicalDefaultClientCert_FromEnv(t *testing.T) {
	// Clear all env vars that might interfere, then set a known one.
	for _, env := range defaultCanonicalClientCertEnvs {
		t.Setenv(env, "")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	p := filepath.Join(dir, "cert.pem")
	require.NoError(t, os.WriteFile(p, []byte("x"), 0o600))
	if len(defaultCanonicalClientCertEnvs) > 0 {
		t.Setenv(defaultCanonicalClientCertEnvs[0], p)
		got := resolveCanonicalDefaultClientCert("")
		assert.Equal(t, p, got)
	}
}

func TestResolveCanonicalDefaultClientCert_NoneFound(t *testing.T) {
	for _, env := range defaultCanonicalClientCertEnvs {
		t.Setenv(env, "")
	}
	// Point HOME at an empty temp directory so the default paths miss.
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	got := resolveCanonicalDefaultClientCert("")
	assert.Equal(t, "", got)
}

// =============================================================================
// canonicalLoginLoadCertFingerprint
// =============================================================================

func TestCanonicalLoginLoadCertFingerprint_MissingFile(t *testing.T) {
	_, _, err := canonicalLoginLoadCertFingerprint("/no/such/cert.pem")
	require.Error(t, err)
	var typed *CanonicalCertReadError
	require.ErrorAs(t, err, &typed)
}

func TestCanonicalLoginLoadCertFingerprint_Happy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.pem")
	writePEMCertToPath(t, path) // helper from canonical_auth_helpers_test.go
	fp, meta, err := canonicalLoginLoadCertFingerprint(path)
	require.NoError(t, err)
	assert.Len(t, fp, 64)
	assert.Contains(t, meta, "cert_cn")
}

func TestCanonicalLoginLoadCertFingerprint_NotPEMV2(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notpem.txt")
	require.NoError(t, os.WriteFile(path, []byte("plain"), 0o600))
	_, _, err := canonicalLoginLoadCertFingerprint(path)
	require.Error(t, err)
}

// =============================================================================
// writeCanonicalSessionToken
// =============================================================================

func TestWriteCanonicalSessionToken_ExplicitPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tok")
	got, err := writeCanonicalSessionToken(p, "jwt.payload.sig")
	require.NoError(t, err)
	assert.Equal(t, p, got)
	body, rerr := os.ReadFile(p)
	require.NoError(t, rerr)
	assert.True(t, strings.HasPrefix(string(body), "jwt.payload.sig"))
}

func TestWriteCanonicalSessionToken_DefaultPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	got, err := writeCanonicalSessionToken("", "tok")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(got, dir), "want token under HOME (%s); got %s", dir, got)
	assert.Contains(t, got, ".quicbaas")
	assert.Contains(t, got, "session.token")
}

func TestWriteCanonicalSessionToken_MkdirFails(t *testing.T) {
	// A path inside a regular file (file as parent) cannot be MkdirAll'd.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "block")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	bad := filepath.Join(blocker, "session.token")
	_, err := writeCanonicalSessionToken(bad, "tok")
	require.Error(t, err)
	var typed *TokenWriteError
	require.ErrorAs(t, err, &typed)
}

// =============================================================================
// helpers
// =============================================================================

// mintCertFromPub creates a self-signed certificate from the given public
// key and signer, so the resulting cert.PublicKeyAlgorithm matches the
// caller-selected algorithm.
func mintCertFromPub(t *testing.T, pub any, signer any) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, signer)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}
