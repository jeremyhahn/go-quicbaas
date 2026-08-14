//go:build !pkcs11

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

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	stderrors "errors"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// fakeLocalPIVSession is the seam double used by the enrollment/login
// plumbing tests.
type fakeLocalPIVSession struct {
	cert    *x509.Certificate
	certErr error
	sig     []byte
	sigErr  error
	slot    string
	nonce   []byte
	closed  bool
}

func (f *fakeLocalPIVSession) Certificate(slot string) (*x509.Certificate, error) {
	f.slot = slot
	return f.cert, f.certErr
}

func (f *fakeLocalPIVSession) SignNonce(_ context.Context, slot string, nonce []byte) ([]byte, error) {
	f.slot = slot
	f.nonce = nonce
	return f.sig, f.sigErr
}

func (f *fakeLocalPIVSession) Close() error {
	f.closed = true
	return nil
}

// resetLocalXKMSForTest clears the process-wide xkms singleton and the
// local-PIV ownership flag so a file-backend session test starts from a
// clean slate regardless of which earlier tests (e.g. embedded-transport
// suites) initialized the singleton. Test-only: production code never
// resets a singleton it does not own.
func resetLocalXKMSForTest(t *testing.T) {
	t.Helper()
	xkmssdk.ServiceReset()
	xkmssdk.ServiceResetPIV()
	localPIVOwnsXKMS = false
	t.Cleanup(func() {
		xkmssdk.ServiceReset()
		xkmssdk.ServiceResetPIV()
		localPIVOwnsXKMS = false
	})
}

// withFakeLocalPIVSession installs a seam session and restores the
// production constructor on cleanup.
func withFakeLocalPIVSession(t *testing.T, session localPIVSession, err error) {
	t.Helper()
	orig := newLocalPIVSessionFunc
	newLocalPIVSessionFunc = func(*localPIVConfig) (localPIVSession, error) { return session, err }
	t.Cleanup(func() { newLocalPIVSessionFunc = orig })
}

// selfSignedTestCert builds an in-memory ECDSA P-256 certificate.
func selfSignedTestCert(t *testing.T) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: "piv-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

// TestNewLocalPIVSession_InvalidBackend rejects unknown backends with the
// typed error.
func TestNewLocalPIVSession_InvalidBackend(t *testing.T) {
	_, err := newLocalPIVSession(&localPIVConfig{backend: "opgp"})
	var typed *InvalidPIVBackendError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "opgp", typed.Backend)

	_, err = newLocalPIVSession(nil)
	require.ErrorAs(t, err, &typed)
}

// TestNewLocalPIVSession_TPM2Unsupported surfaces the go-xkms capability gap
// as a typed error.
func TestNewLocalPIVSession_TPM2Unsupported(t *testing.T) {
	_, err := newLocalPIVSession(&localPIVConfig{backend: localPIVBackendTPM2})
	var typed *PIVLocalBackendUnsupportedError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, localPIVBackendTPM2, typed.Backend)
}

// TestNewLocalPIVSession_PKCS11WithoutTag reports the missing pkcs11 build
// tag as a typed error (this test file only builds without the tag).
func TestNewLocalPIVSession_PKCS11WithoutTag(t *testing.T) {
	_, err := newLocalPIVSession(&localPIVConfig{backend: localPIVBackendPKCS11})
	var typed *PIVLocalBackendUnsupportedError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, localPIVBackendPKCS11, typed.Backend)
	assert.Contains(t, typed.Reason, "pkcs11")
}

// TestFilePIVSession_EndToEnd proves the full local file-backend flow with
// real go-xkms backends in a temp dir: generate a slot key + self-signed
// cert, read the certificate back, sign a login nonce, and verify the
// signature EXACTLY the way the server-side go-xkms
// PIVHardwareFactorVerifier does (ECDSA ASN.1 over SHA-256(nonce)).
func TestFilePIVSession_EndToEnd(t *testing.T) {
	resetLocalXKMSForTest(t)
	dir := t.TempDir()

	session, err := newLocalPIVSession(&localPIVConfig{
		backend: localPIVBackendFile,
		dir:     dir,
	})
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	// Provision a slot key + self-signed certificate through the same
	// go-xkms surface the session wired up.
	genResp, err := xkmssdk.ServiceGeneratePIVKey(context.Background(), &xkmstransport.GeneratePIVKeyRequest{
		Backend:   localPIVXKMSBackendName,
		Slot:      "9a",
		Algorithm: "ecdsap256",
		Subject:   "login-test",
	})
	require.NoError(t, err)
	require.NotNil(t, genResp)

	cert, err := session.Certificate("9a")
	require.NoError(t, err)
	require.NotNil(t, cert)
	assert.Equal(t, "login-test", cert.Subject.CommonName)

	nonce := []byte("server-issued-login-nonce")
	sig, err := session.SignNonce(context.Background(), "9a", nonce)
	require.NoError(t, err)
	require.NotEmpty(t, sig)

	// Server-side verification contract (PIVHardwareFactorVerifier).
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	require.True(t, ok, "expected ECDSA public key")
	digest := sha256.Sum256(nonce)
	assert.True(t, ecdsa.VerifyASN1(pub, digest[:], sig),
		"signature must verify over SHA-256(nonce) exactly as the server verifier checks")
}

// TestFilePIVSession_CertificateErrors covers slot validation and missing
// certificates.
func TestFilePIVSession_CertificateErrors(t *testing.T) {
	resetLocalXKMSForTest(t)
	dir := t.TempDir()
	session, err := newLocalPIVSession(&localPIVConfig{backend: localPIVBackendFile, dir: dir})
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	var typed *PIVLocalSessionError
	_, err = session.Certificate("zz")
	require.ErrorAs(t, err, &typed)

	_, err = session.Certificate("9c")
	require.ErrorAs(t, err, &typed, "empty slot must fail with a typed error")
}

// TestFilePIVSession_SignNonceMissingKey fails closed when the slot has no
// key material.
func TestFilePIVSession_SignNonceMissingKey(t *testing.T) {
	resetLocalXKMSForTest(t)
	dir := t.TempDir()
	session, err := newLocalPIVSession(&localPIVConfig{backend: localPIVBackendFile, dir: dir})
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	_, err = session.SignNonce(context.Background(), "9d", []byte("nonce"))
	var typed *PIVLocalSessionError
	require.ErrorAs(t, err, &typed)
}

// TestProveCanonicalPIV_Success drives the login proof assembly through the
// seam and checks lookup keys, signature, and metadata.
func TestProveCanonicalPIV_Success(t *testing.T) {
	cert := selfSignedTestCert(t)
	fake := &fakeLocalPIVSession{cert: cert, sig: []byte("sig-bytes")}
	withFakeLocalPIVSession(t, fake, nil)

	opts := &canonicalAuthLoginOptions{pivBackend: localPIVBackendFile, slot: "9c"}
	begin := &qrdbtransport.BeginLoginResponse{Nonce: []byte("nonce-1")}

	lookup, alts, sig, meta, err := proveCanonicalPIV(context.Background(), opts, begin)
	require.NoError(t, err)

	spkiSum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	certSum := sha256.Sum256(cert.Raw)
	assert.Equal(t, spkiSum[:], lookup, "primary lookup key is the SPKI hash")
	require.Len(t, alts, 1)
	assert.Equal(t, certSum[:], alts[0], "alt lookup key is the legacy cert hash")
	assert.Equal(t, []byte("sig-bytes"), sig)
	assert.Equal(t, "9c", meta["slot"])
	assert.Equal(t, localPIVBackendFile, meta["backend"])
	assert.Equal(t, "ecdsa", meta["sign_algo"])
	assert.Equal(t, "piv-test", meta["cert_cn"])
	assert.Equal(t, []byte("nonce-1"), fake.nonce, "the SERVER nonce must be what gets signed")
	assert.True(t, fake.closed, "the session must be released")
}

// TestProveCanonicalPIV_Errors covers session construction, certificate,
// and signing failures.
func TestProveCanonicalPIV_Errors(t *testing.T) {
	begin := &qrdbtransport.BeginLoginResponse{Nonce: []byte("nonce")}

	t.Run("session error", func(t *testing.T) {
		want := &PIVLocalBackendUnsupportedError{Backend: "tpm2", Reason: "gap"}
		withFakeLocalPIVSession(t, nil, want)
		_, _, _, _, err := proveCanonicalPIV(context.Background(), &canonicalAuthLoginOptions{}, begin)
		var typed *PIVLocalBackendUnsupportedError
		require.ErrorAs(t, err, &typed)
	})

	t.Run("certificate error", func(t *testing.T) {
		want := stderrors.New("no cert")
		withFakeLocalPIVSession(t, &fakeLocalPIVSession{certErr: want}, nil)
		_, _, _, _, err := proveCanonicalPIV(context.Background(), &canonicalAuthLoginOptions{}, begin)
		var typed *PIVOperationError
		require.ErrorAs(t, err, &typed)
		assert.ErrorIs(t, err, want)
	})

	t.Run("sign error", func(t *testing.T) {
		want := stderrors.New("no key")
		withFakeLocalPIVSession(t, &fakeLocalPIVSession{cert: selfSignedTestCert(t), sigErr: want}, nil)
		_, _, _, _, err := proveCanonicalPIV(context.Background(), &canonicalAuthLoginOptions{}, begin)
		var typed *PIVOperationError
		require.ErrorAs(t, err, &typed)
		assert.ErrorIs(t, err, want)
	})
}

// TestBuildCanonicalEnrollRequest_PIV verifies the enrollment request built
// from a local session: credential + SPKI lookup hashes, the cert_pem the
// server-side verifier requires, and the backend/slot metadata.
func TestBuildCanonicalEnrollRequest_PIV(t *testing.T) {
	cert := selfSignedTestCert(t)
	fake := &fakeLocalPIVSession{cert: cert}
	withFakeLocalPIVSession(t, fake, nil)

	req, err := buildCanonicalEnrollRequest(context.Background(), &canonicalAuthFactorAddFlags{
		factorType: "piv",
		label:      "badge",
		user:       "alice",
		tenant:     "acme",
		pivSlot:    "9a",
	})
	require.NoError(t, err)
	require.NotNil(t, req)
	assert.Equal(t, "piv", req.FactorName)
	assert.Equal(t, "alice", req.Subject)
	assert.Equal(t, "acme", req.TenantID)

	certSum := sha256.Sum256(cert.Raw)
	spkiSum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	assert.Equal(t, hex.EncodeToString(certSum[:]), req.MethodParams["credential_sha256"])
	assert.Equal(t, hex.EncodeToString(spkiSum[:]), req.MethodParams["spki_sha256"])
	assert.Equal(t, localPIVBackendFile, req.MethodParams["backend"])
	assert.Equal(t, "9a", req.MethodParams["slot"])
	assert.Equal(t, "piv-test", req.MethodParams["cert_cn"])
	certPEM, _ := req.MethodParams["cert_pem"].(string)
	assert.Contains(t, certPEM, "BEGIN CERTIFICATE",
		"cert_pem is required by the server-side PIV verifier")
	assert.True(t, fake.closed)
}

// TestBuildCanonicalEnrollRequest_PIVSessionError fails closed when the
// local session cannot be built.
func TestBuildCanonicalEnrollRequest_PIVSessionError(t *testing.T) {
	want := &PIVLocalSessionError{Op: "init keystore", Cause: stderrors.New("boom")}
	withFakeLocalPIVSession(t, nil, want)

	_, err := buildCanonicalEnrollRequest(context.Background(), &canonicalAuthFactorAddFlags{
		factorType: "piv",
		label:      "badge",
		user:       "alice",
	})
	var typed *PIVLocalSessionError
	require.ErrorAs(t, err, &typed)
}
