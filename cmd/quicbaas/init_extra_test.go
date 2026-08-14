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

// Package main provides extended coverage for init.go: every PEM block
// type, every signing algorithm, the writeFileSecure permission contract,
// and the enroll-external-officer ReadFile error branches.
package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// parsePrivateKeyPEM: every block-type variant + corrupt PEM
// =============================================================================

// TestParsePrivateKeyPEM_PKCS1RSA decodes "RSA PRIVATE KEY" (PKCS1).
func TestParsePrivateKeyPEM_PKCS1RSA(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der := x509.MarshalPKCS1PrivateKey(priv)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})

	got, err := parsePrivateKeyPEM(pemBytes)
	require.NoError(t, err)
	_, ok := got.(*rsa.PrivateKey)
	require.True(t, ok, "expected *rsa.PrivateKey, got %T", got)
}

// TestParsePrivateKeyPEM_ECEC decodes "EC PRIVATE KEY" (SEC1).
func TestParsePrivateKeyPEM_ECEC(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})

	got, err := parsePrivateKeyPEM(pemBytes)
	require.NoError(t, err)
	_, ok := got.(*ecdsa.PrivateKey)
	require.True(t, ok, "expected *ecdsa.PrivateKey, got %T", got)
}

// TestParsePrivateKeyPEM_PKCS8ECDSA decodes "PRIVATE KEY" (PKCS8) holding ECDSA.
func TestParsePrivateKeyPEM_PKCS8ECDSA(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	got, err := parsePrivateKeyPEM(pemBytes)
	require.NoError(t, err)
	_, ok := got.(*ecdsa.PrivateKey)
	require.True(t, ok, "expected *ecdsa.PrivateKey, got %T", got)
}

// TestParsePrivateKeyPEM_PKCS8Ed25519 decodes "PRIVATE KEY" (PKCS8) holding Ed25519.
func TestParsePrivateKeyPEM_PKCS8Ed25519(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	got, err := parsePrivateKeyPEM(pemBytes)
	require.NoError(t, err)
	_, ok := got.(ed25519.PrivateKey)
	require.True(t, ok, "expected ed25519.PrivateKey, got %T", got)
}

// TestParsePrivateKeyPEM_FallbackPKCS8 verifies the default branch tries
// PKCS8 when the block type is unrecognized but the body parses.
func TestParsePrivateKeyPEM_FallbackPKCS8(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)
	// Wrap in an unrecognized header so the default branch is taken.
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "ANY KEY", Bytes: der})

	got, err := parsePrivateKeyPEM(pemBytes)
	require.NoError(t, err)
	_, ok := got.(*ecdsa.PrivateKey)
	require.True(t, ok, "expected *ecdsa.PrivateKey via fallback, got %T", got)
}

// TestParsePrivateKeyPEM_UnsupportedBlockType fails when neither the
// explicit case nor the PKCS8 fallback can decode the body.
func TestParsePrivateKeyPEM_UnsupportedBlockType(t *testing.T) {
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "JUNK BLOCK", Bytes: []byte("not a key")})

	_, err := parsePrivateKeyPEM(pemBytes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported PEM block type JUNK BLOCK")
}

// TestParsePrivateKeyPEM_NoPEMBlock fails when the input has no PEM block.
func TestParsePrivateKeyPEM_NoPEMBlock(t *testing.T) {
	_, err := parsePrivateKeyPEM([]byte("not a PEM at all"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no PEM block found")
}

// =============================================================================
// signNonceWithKeyFile: full signature matrix
// =============================================================================

// nonceHex is a fixed hex nonce for matrix tests. Length is 64 hex chars
// (32 bytes), well-formed for hex.DecodeString.
const nonceHex = "deadbeefcafebabedeadbeefcafebabedeadbeefcafebabedeadbeefcafebabe"

// TestSignNonceWithKeyFile_ECDSAEC signs with an EC PRIVATE KEY block.
func TestSignNonceWithKeyFile_ECDSAEC(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ec.key")
	writeECPEM(t, keyPath)

	sig, err := signNonceWithKeyFile(keyPath, nonceHex)
	require.NoError(t, err)
	require.NotEmpty(t, sig)
}

// TestSignNonceWithKeyFile_Ed25519 signs with an ed25519 key. Ed25519
// takes the message itself (no SHA256 pre-hash) — this exercises the
// special-case branch in signNonceWithKeyFile.
func TestSignNonceWithKeyFile_Ed25519(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ed25519.key")

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	require.NoError(t, os.WriteFile(keyPath, pemBytes, 0o600))

	sig, err := signNonceWithKeyFile(keyPath, nonceHex)
	require.NoError(t, err)
	require.NotEmpty(t, sig)
}

// TestSignNonceWithKeyFile_RSAPKCS1 signs with an RSA PKCS1 key.
func TestSignNonceWithKeyFile_RSAPKCS1(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "rsa.key")

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der := x509.MarshalPKCS1PrivateKey(priv)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})
	require.NoError(t, os.WriteFile(keyPath, pemBytes, 0o600))

	sig, err := signNonceWithKeyFile(keyPath, nonceHex)
	require.NoError(t, err)
	require.NotEmpty(t, sig)
}

// TestSignNonceWithKeyFile_RSAPKCS8 signs with an RSA PKCS8 key.
func TestSignNonceWithKeyFile_RSAPKCS8(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "rsa-pkcs8.key")

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	require.NoError(t, os.WriteFile(keyPath, pemBytes, 0o600))

	sig, err := signNonceWithKeyFile(keyPath, nonceHex)
	require.NoError(t, err)
	require.NotEmpty(t, sig)
}

// TestSignNonceWithKeyFile_CorruptPEM rejects malformed PEM input with
// the typed InitCSRKeySignError.
func TestSignNonceWithKeyFile_CorruptPEM(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "broken.key")
	require.NoError(t, os.WriteFile(keyPath, []byte("this is not a PEM"), 0o600))

	_, err := signNonceWithKeyFile(keyPath, nonceHex)
	require.Error(t, err)
	var typed *InitCSRKeySignError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, keyPath, typed.Path)
}

// TestSignNonceWithKeyFile_BadHexNonce rejects a malformed nonce.
func TestSignNonceWithKeyFile_BadHexNonce(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ec.key")
	writeECPEM(t, keyPath)

	_, err := signNonceWithKeyFile(keyPath, "zzznothex")
	require.Error(t, err)
	var typed *InitCSRKeySignError
	require.ErrorAs(t, err, &typed)
}

// TestSignNonceWithKeyFile_MissingFile surfaces the OS-level read error.
func TestSignNonceWithKeyFile_MissingFile(t *testing.T) {
	_, err := signNonceWithKeyFile("/no/such/key.pem", nonceHex)
	require.Error(t, err)
	var typed *InitCSRKeySignError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "/no/such/key.pem", typed.Path)
}

// x25519PKCS8DER is a minimal valid PKCS8 wrapper around an X25519
// private key. ParsePKCS8PrivateKey returns *ecdh.PrivateKey for this
// algorithm, which does NOT implement crypto.Signer. We use it to
// exercise the "key does not implement crypto.Signer" branch in
// signNonceWithKeyFile.
var x25519PKCS8DER = []byte{
	0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x6e,
	0x04, 0x22, 0x04, 0x20,
	// 32 bytes of private key material
	0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
	0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
	0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
	0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
}

// TestSignNonceWithKeyFile_NonSignerKey verifies the typed error when
// the parsed key does not satisfy crypto.Signer (X25519 -> *ecdh.PrivateKey).
func TestSignNonceWithKeyFile_NonSignerKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "x25519.key")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: x25519PKCS8DER})
	require.NoError(t, os.WriteFile(keyPath, pemBytes, 0o600))

	_, err := signNonceWithKeyFile(keyPath, nonceHex)
	require.Error(t, err)
	var typed *InitCSRKeySignError
	require.ErrorAs(t, err, &typed)
}

// =============================================================================
// writeFileSecure: 0600 permission contract
// =============================================================================

// TestWriteFileSecure_PermsAre0600 verifies the file is created with mode
// 0600 (owner read/write only) so credential material does not leak. The
// runtime is Unix in CI; on the off chance umask trims a bit, mask the
// FileMode to permission bits before comparing.
func TestWriteFileSecure_PermsAre0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.bin")
	require.NoError(t, writeFileSecure(path, []byte("topsecret")))

	st, err := os.Stat(path)
	require.NoError(t, err)
	// Mask off the file-type bits; only the permission bits matter.
	mode := st.Mode().Perm()
	assert.Equal(t, os.FileMode(0o600), mode,
		"writeFileSecure must create mode-0600 files, got %v", mode)

	// And confirm the contents round-trip.
	got, rerr := os.ReadFile(path)
	require.NoError(t, rerr)
	assert.Equal(t, "topsecret", string(got))
}

// TestWriteFileSecure_WrapsWriteError wraps an os.WriteFile failure in
// the typed InitWriteFileError when the parent directory does not exist.
func TestWriteFileSecure_WrapsWriteError(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "missing", "file.bin")
	err := writeFileSecure(bad, []byte("x"))
	require.Error(t, err)
	var typed *InitWriteFileError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, bad, typed.Path)
}

// =============================================================================
// runInitEnrollExternalOfficerCommand: XOR + ReadFile error paths
// =============================================================================

// TestEnrollExternalOfficer_CertReadError surfaces an OS-level read error
// from --cert as a Wrapf-wrapped error chain.
func TestEnrollExternalOfficer_CertReadError(t *testing.T) {
	defer resetInitFlags()

	cmd := newInitEnrollExternalOfficerCommand()
	cmd.SetArgs([]string{
		"--username", "alice",
		"--cert", "/no/such/cert.pem",
		"--share-index", "0",
		"--auth-factor", "mtls",
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/no/such/cert.pem")
}

// TestEnrollExternalOfficer_PubKeyReadError surfaces an OS-level read
// error from --pubkey.
func TestEnrollExternalOfficer_PubKeyReadError(t *testing.T) {
	defer resetInitFlags()

	cmd := newInitEnrollExternalOfficerCommand()
	cmd.SetArgs([]string{
		"--username", "alice",
		"--pubkey", "/no/such/pub.pem",
		"--share-index", "0",
		"--auth-factor", "webauthn",
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/no/such/pub.pem")
}

// TestEnrollExternalOfficer_EmptyAuthFactor rejects an unset auth factor
// (covers the "empty" branch of the map lookup).
func TestEnrollExternalOfficer_EmptyAuthFactor(t *testing.T) {
	defer resetInitFlags()
	dir := t.TempDir()
	certPath := filepath.Join(dir, "c.pem")
	require.NoError(t, os.WriteFile(certPath, []byte("x"), 0o600))

	cmd := newInitEnrollExternalOfficerCommand()
	cmd.SetArgs([]string{
		"--username", "alice",
		"--cert", certPath,
		"--share-index", "1",
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	var typed *InitInvalidAuthFactorError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "", typed.Got)
}

// TestEnrollExternalOfficer_FactoryError surfaces a factory failure once
// flag validation has passed (CertPath is read successfully).
func TestEnrollExternalOfficer_FactoryError(t *testing.T) {
	defer resetInitFlags()
	dir := t.TempDir()
	certPath := filepath.Join(dir, "c.pem")
	require.NoError(t, os.WriteFile(certPath, []byte("pem-bytes"), 0o600))

	orig := ceremonyClientFactory
	defer func() { ceremonyClientFactory = orig }()
	wantErr := assertEnrollError("factory boom")
	ceremonyClientFactory = func() (CeremonyClient, error) { return nil, wantErr }

	cmd := newInitEnrollExternalOfficerCommand()
	cmd.SetArgs([]string{
		"--username", "alice",
		"--cert", certPath,
		"--share-index", "0",
		"--auth-factor", "mtls",
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

// assertEnrollError is a tiny sentinel-error helper so the typed-error
// check in TestEnrollExternalOfficer_FactoryError stays direct.
type assertEnrollError string

func (e assertEnrollError) Error() string { return string(e) }
