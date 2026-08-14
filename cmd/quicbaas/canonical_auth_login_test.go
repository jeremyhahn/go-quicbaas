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

package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
)

// TestExpandCanonicalHome covers the home-directory expansion helper.
func TestExpandCanonicalHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"/etc/foo", "/etc/foo"},
		{"~", home},
		{"~/x", filepath.Join(home, "x")},
	}
	for _, tc := range cases {
		got := expandCanonicalHome(tc.in)
		if got != tc.want {
			t.Errorf("expandCanonicalHome(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestResolveCanonicalDefaultClientCert_ExplicitPresent returns the
// supplied path when it exists.
func TestResolveCanonicalDefaultClientCert_ExplicitPresent(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "admin.pem")
	if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := resolveCanonicalDefaultClientCert(path); got != path {
		t.Fatalf("expected %q, got %q", path, got)
	}
}

// TestResolveCanonicalDefaultClientCert_ExplicitMissing returns "" for
// missing path.
func TestResolveCanonicalDefaultClientCert_ExplicitMissing(t *testing.T) {
	if got := resolveCanonicalDefaultClientCert("/no/such/file.pem"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

// TestPIVAlgoFromCanonicalCert maps key algorithms to PIV signing
// algorithm names.
func TestPIVAlgoFromCanonicalCert(t *testing.T) {
	cases := []struct {
		algo x509.PublicKeyAlgorithm
		want string
	}{
		{x509.RSA, "rsa-pkcs1"},
		{x509.Ed25519, "ed25519"},
		{x509.ECDSA, "ecdsa"},
		{x509.UnknownPublicKeyAlgorithm, "ecdsa"},
	}
	for _, tc := range cases {
		got := pivAlgoFromCanonicalCert(&x509.Certificate{PublicKeyAlgorithm: tc.algo})
		if got != tc.want {
			t.Errorf("pivAlgoFromCanonicalCert(%v) = %q, want %q", tc.algo, got, tc.want)
		}
	}
	if got := pivAlgoFromCanonicalCert(nil); got != "ecdsa" {
		t.Errorf("nil cert default = %q, want ecdsa", got)
	}
}

// TestStringifyCanonicalMetadata flattens any-typed maps.
func TestStringifyCanonicalMetadata(t *testing.T) {
	got := stringifyCanonicalMetadata(map[string]any{"a": "v", "b": 7})
	if got["a"] != "v" {
		t.Errorf("string passthrough: %q", got["a"])
	}
	if got["b"] != "7" {
		t.Errorf("int formatting: %q", got["b"])
	}
}

// TestProveCanonicalMTLS_MissingCert returns the typed missing-cert error.
func TestProveCanonicalMTLS_MissingCert(t *testing.T) {
	t.Setenv("QUICBAAS_CLIENT_CERT", "")
	t.Setenv("PLATFORMCTL_CLIENT_CERT", "")
	t.Setenv("XKMSCTL_CLIENT_CERT", "")
	t.Setenv("QRDB_CLIENT_CERT", "")
	t.Setenv("HOME", t.TempDir())
	_, _, _, err := proveCanonicalMTLS(t.Context(), &canonicalAuthLoginOptions{}, nil)
	if err == nil {
		t.Fatal("expected missing-cert error")
	}
	var mc *MissingClientCertError
	if !errors.As(err, &mc) {
		t.Errorf("expected *MissingClientCertError, got %T: %v", err, err)
	}
}

// TestProveCanonicalMTLS_RoundTrip generates a self-signed cert and asserts the
// lookup key + metadata population. The mTLS proof has no in-band signature
// (handshake-established); the lookup key is the certificate SHA-256.
func TestProveCanonicalMTLS_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	certPath := filepath.Join(tmp, "admin.pem")
	writeCanonicalTestCert(t, certPath)

	opts := &canonicalAuthLoginOptions{
		clientCertPath: certPath,
		username:       "alice",
		factorID:       "f1",
	}
	lookupKey, signature, meta, err := proveCanonicalMTLS(t.Context(), opts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lookupKey) == 0 {
		t.Error("expected non-empty lookup key (cert sha256)")
	}
	if signature != nil {
		t.Error("expected nil signature for mTLS (handshake-established possession)")
	}
	if meta["credential_sha256"] == "" {
		t.Error("expected credential_sha256 in metadata")
	}
	if meta["cert_path"] != certPath {
		t.Errorf("expected cert_path metadata; got %q", meta["cert_path"])
	}
}

// TestCanonicalLoginLoadCertFingerprint_BadPath returns a typed error.
func TestCanonicalLoginLoadCertFingerprint_BadPath(t *testing.T) {
	if _, _, err := canonicalLoginLoadCertFingerprint("/no/such/file"); err == nil {
		t.Fatal("expected error")
	} else {
		var cre *CanonicalCertReadError
		if !errors.As(err, &cre) {
			t.Errorf("expected *CanonicalCertReadError, got %T", err)
		}
	}
}

// TestCanonicalLoginLoadCertFingerprint_NotPEM rejects junk input.
func TestCanonicalLoginLoadCertFingerprint_NotPEM(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "junk.pem")
	if err := os.WriteFile(path, []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := canonicalLoginLoadCertFingerprint(path); err == nil {
		t.Fatal("expected error")
	} else {
		var cre *CanonicalCertReadError
		if !errors.As(err, &cre) {
			t.Errorf("expected *CanonicalCertReadError, got %T", err)
		}
	}
}

// TestWriteCanonicalSessionToken persists to a temp path.
func TestWriteCanonicalSessionToken(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "session.token")
	gotPath, err := writeCanonicalSessionToken(path, "JWT.HEAD.SIG", "refresh-abc", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != path {
		t.Errorf("expected %q, got %q", path, gotPath)
	}
	stored, rerr := cli.LoadToken(path)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if stored.AccessToken != "JWT.HEAD.SIG" {
		t.Errorf("access token = %q, want JWT.HEAD.SIG", stored.AccessToken)
	}
	if stored.RefreshToken != "refresh-abc" {
		t.Errorf("refresh token = %q, want refresh-abc", stored.RefreshToken)
	}
}

// TestRunCanonicalAuthLoginV2_InvalidMethod surfaces the typed error.
func TestRunCanonicalAuthLoginV2_InvalidMethod(t *testing.T) {
	t.Setenv("QUICBAAS_CLIENT_CERT", "")
	t.Setenv("HOME", t.TempDir())
	err := runCanonicalAuthLoginV2(t.Context(), &canonicalAuthLoginOptions{method: "totp"})
	if err == nil {
		t.Fatal("expected invalid method error")
	}
	var im *InvalidAuthMethodError
	if !errors.As(err, &im) {
		t.Errorf("expected *InvalidAuthMethodError, got %T: %v", err, err)
	}
}

// writeCanonicalTestCert writes a self-signed test cert at path.
func writeCanonicalTestCert(t *testing.T, path string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-cn"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if err := pem.Encode(out, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatal(err)
	}
}
