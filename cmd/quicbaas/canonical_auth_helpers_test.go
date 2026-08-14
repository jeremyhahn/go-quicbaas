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

// Package main: helper-function coverage for canonical_auth.go.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	stderrors "errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// canonicalAuthFactors / canonicalAuthDevices / canonicalAuthFIDO2
// =============================================================================

// withFakeSDKClientForCanonicalAuth installs a sdkClientFactory that
// returns the supplied client and restores the previous factory on
// cleanup.
func withFakeSDKClientForCanonicalAuth(t *testing.T, c quicbaas.Client, err error) {
	t.Helper()
	orig := sdkClientFactory
	sdkClientFactory = func() (quicbaas.Client, error) { return c, err }
	t.Cleanup(func() { sdkClientFactory = orig })
}

// TestCanonicalAuthFactors_FactoryError surfaces the SDK factory error.
func TestCanonicalAuthFactors_FactoryError(t *testing.T) {
	want := stderrors.New("factory boom")
	withFakeSDKClientForCanonicalAuth(t, nil, want)
	_, err := canonicalAuthFactors("test")
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
}

// TestCanonicalAuthDevices_FactoryError surfaces the SDK factory error from
// the client-LOCAL CTAP2 device-helper resolver.
func TestCanonicalAuthDevices_FactoryError(t *testing.T) {
	want := stderrors.New("factory boom")
	withFakeSDKClientForCanonicalAuth(t, nil, want)
	_, err := canonicalAuthDevices("test")
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
}

// TestCanonicalAuthFIDO2_FactoryError surfaces the SDK factory error.
func TestCanonicalAuthFIDO2_FactoryError(t *testing.T) {
	want := stderrors.New("factory boom")
	withFakeSDKClientForCanonicalAuth(t, nil, want)
	_, err := canonicalAuthFIDO2("test")
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
}

// =============================================================================
// buildCanonicalEnrollRequest — early-branch validation
// =============================================================================

// TestBuildCanonicalEnrollRequest_RejectsEmptyType returns
// InvalidFactorTypeError when --type is unset.
func TestBuildCanonicalEnrollRequest_RejectsEmptyType(t *testing.T) {
	_, err := buildCanonicalEnrollRequest(context.Background(), &canonicalAuthFactorAddFlags{})
	require.Error(t, err)
	var typed *InvalidFactorTypeError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "", typed.Type)
}

// TestBuildCanonicalEnrollRequest_RejectsUnknownType returns
// InvalidFactorTypeError for unknown --type values.
func TestBuildCanonicalEnrollRequest_RejectsUnknownType(t *testing.T) {
	_, err := buildCanonicalEnrollRequest(context.Background(), &canonicalAuthFactorAddFlags{
		factorType: "totp",
		label:      "ok",
	})
	require.Error(t, err)
	var typed *InvalidFactorTypeError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "totp", typed.Type)
}

// TestBuildCanonicalEnrollRequest_RequiresLabel returns MissingLabelError
// when --label is unset on a valid factor type.
func TestBuildCanonicalEnrollRequest_RequiresLabel(t *testing.T) {
	_, err := buildCanonicalEnrollRequest(context.Background(), &canonicalAuthFactorAddFlags{
		factorType: "mtls",
	})
	require.Error(t, err)
	var typed *MissingLabelError
	require.ErrorAs(t, err, &typed)
}

// TestBuildCanonicalEnrollRequest_MtlsRequiresCert returns
// MissingCertPathError when --type mtls is selected without --cert.
func TestBuildCanonicalEnrollRequest_MtlsRequiresCert(t *testing.T) {
	_, err := buildCanonicalEnrollRequest(context.Background(), &canonicalAuthFactorAddFlags{
		factorType: "mtls",
		label:      "yubi",
	})
	require.Error(t, err)
	var typed *MissingCertPathError
	require.ErrorAs(t, err, &typed)
}

// TestBuildCanonicalEnrollRequest_MtlsHappy round-trips a real cert
// through buildCanonicalEnrollRequest.
func TestBuildCanonicalEnrollRequest_MtlsHappy(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "user.pem")
	writePEMCertToPath(t, certPath)

	req, err := buildCanonicalEnrollRequest(context.Background(), &canonicalAuthFactorAddFlags{
		factorType: "mtls",
		label:      "yubi",
		user:       "alice",
		certPath:   certPath,
	})
	require.NoError(t, err)
	require.NotNil(t, req)
	assert.Equal(t, "mtls", req.FactorName)
	assert.Equal(t, "alice", req.Subject)
	assert.Contains(t, req.MethodParams, "credential_sha256")
	assert.Contains(t, req.MethodParams, "cert_cn")
}

// =============================================================================
// canonicalLoadCertFingerprint
// =============================================================================

// TestCanonicalLoadCertFingerprint_MissingFile returns CertReadError.
func TestCanonicalLoadCertFingerprint_MissingFile(t *testing.T) {
	_, _, err := canonicalLoadCertFingerprint("/no/such/cert.pem")
	require.Error(t, err)
	var typed *CertReadError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "/no/such/cert.pem", typed.Path)
}

// TestCanonicalLoadCertFingerprint_NotPEM returns CertReadError when
// the file body is not a PEM block.
func TestCanonicalLoadCertFingerprint_NotPEM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notpem.txt")
	require.NoError(t, os.WriteFile(path, []byte("plain text"), 0o600))

	_, _, err := canonicalLoadCertFingerprint(path)
	require.Error(t, err)
	var typed *CertReadError
	require.ErrorAs(t, err, &typed)
}

// TestCanonicalLoadCertFingerprint_WrongBlockType returns CertReadError
// when the PEM block exists but is not "CERTIFICATE".
func TestCanonicalLoadCertFingerprint_WrongBlockType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key.pem")
	body := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("x")})
	require.NoError(t, os.WriteFile(path, body, 0o600))

	_, _, err := canonicalLoadCertFingerprint(path)
	require.Error(t, err)
	var typed *CertReadError
	require.ErrorAs(t, err, &typed)
}

// TestCanonicalLoadCertFingerprint_InvalidCertBytes returns CertReadError
// when the PEM is CERTIFICATE but the body fails to parse as DER.
func TestCanonicalLoadCertFingerprint_InvalidCertBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.pem")
	body := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not a real DER")})
	require.NoError(t, os.WriteFile(path, body, 0o600))

	_, _, err := canonicalLoadCertFingerprint(path)
	require.Error(t, err)
	var typed *CertReadError
	require.ErrorAs(t, err, &typed)
}

// TestCanonicalLoadCertFingerprint_Happy returns the fingerprint + meta
// for a real self-signed cert.
func TestCanonicalLoadCertFingerprint_Happy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.pem")
	writePEMCertToPath(t, path)

	fp, meta, err := canonicalLoadCertFingerprint(path)
	require.NoError(t, err)
	assert.Len(t, fp, 64, "SHA-256 hex fingerprint must be 64 chars")
	assert.Contains(t, meta, "cert_cn")
	assert.Contains(t, meta, "cert_serial")
	assert.Contains(t, meta, "cert_not_after")
	assert.Contains(t, meta, "cert_not_before")
}

// =============================================================================
// helpers
// =============================================================================

// writePEMCertToPath writes a self-signed P-256 ECDSA certificate to path so
// tests can exercise the cert-reading helpers without depending on a
// real CA.
func writePEMCertToPath(t *testing.T, path string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-cert"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NoError(t, os.WriteFile(path, pemBytes, 0o600))
}
