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
// type, every signing algorithm, the share-claim signer and the
// writeFileSecure permission contract.
package main

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
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
// signShareClaimWithKeyFile: full signature matrix
// =============================================================================

// nonceHex is a fixed hex nonce for matrix tests. Length is 64 hex chars
// (32 bytes), well-formed for hex.DecodeString.
const nonceHex = "deadbeefcafebabedeadbeefcafebabedeadbeefcafebabedeadbeefcafebabe"

// verifyClaimSignature reports whether signature is pub's signature over msg
// in the format the qrdb ceremony verifier accepts: ECDSA (ASN.1 DER) and RSA
// PKCS#1 v1.5 over SHA-256(msg), Ed25519 over msg itself.
func verifyClaimSignature(pub crypto.PublicKey, msg, signature []byte) bool {
	digest := sha256.Sum256(msg)
	switch key := pub.(type) {
	case *ecdsa.PublicKey:
		return ecdsa.VerifyASN1(key, digest[:], signature)
	case *rsa.PublicKey:
		return rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) == nil
	case ed25519.PublicKey:
		return ed25519.Verify(key, msg, signature)
	default:
		return false
	}
}

// assertShareClaimSignature verifies signature against pub over the
// scope-bound share-claim message for label, username and the hex nonce, and
// proves it is bound to that message: it verifies neither over the bare
// nonce nor under the other system-barrier label.
func assertShareClaimSignature(t *testing.T, pub crypto.PublicKey, label, username, hexNonce string, signature []byte) {
	t.Helper()
	nonce, err := hex.DecodeString(hexNonce)
	require.NoError(t, err)

	require.True(t, verifyClaimSignature(pub, qrdbsdk.ShareClaimMessage(label, username, nonce), signature),
		"signature must verify over the %q share-claim message", label)
	assert.False(t, verifyClaimSignature(pub, nonce, signature),
		"a share-claim signature must not verify over the bare nonce")

	other := qrdbsdk.SystemClaimLabel
	if label == qrdbsdk.SystemClaimLabel {
		other = qrdbsdk.RecoveryClaimLabel
	}
	assert.False(t, verifyClaimSignature(pub, qrdbsdk.ShareClaimMessage(other, username, nonce), signature),
		"a %q claim signature must not verify under the %q label", label, other)
}

// TestVerifyClaimSignature_RefusesBareNonceSignature proves the verifier the
// matrix relies on refuses a signature over the bare nonce, which is what the
// CLI sent before it signed the scope-bound message.
func TestVerifyClaimSignature_RefusesBareNonceSignature(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	nonce, err := hex.DecodeString(nonceHex)
	require.NoError(t, err)
	digest := sha256.Sum256(nonce)
	bare, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	require.NoError(t, err)

	msg := qrdbsdk.ShareClaimMessage(qrdbsdk.SystemClaimLabel, "alice", nonce)
	assert.False(t, verifyClaimSignature(&priv.PublicKey, msg, bare))
	assert.False(t, verifyClaimSignature("not a key", msg, bare))
}

// TestSignShareClaimWithKeyFile_ECDSAEC signs with an EC PRIVATE KEY block
// under both system-barrier labels.
func TestSignShareClaimWithKeyFile_ECDSAEC(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ec.key")
	pub := writeECPEM(t, keyPath)

	for _, label := range []string{qrdbsdk.SystemClaimLabel, qrdbsdk.RecoveryClaimLabel} {
		sig, err := signShareClaimWithKeyFile(keyPath, label, "alice", nonceHex)
		require.NoError(t, err)
		assertShareClaimSignature(t, pub, label, "alice", nonceHex, sig)
	}
}

// TestSignShareClaimWithKeyFile_Ed25519 signs with an ed25519 key, which
// signs the message itself rather than its SHA-256 digest.
func TestSignShareClaimWithKeyFile_Ed25519(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ed25519.key")

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	require.NoError(t, os.WriteFile(keyPath, pemBytes, 0o600))

	sig, err := signShareClaimWithKeyFile(keyPath, qrdbsdk.RecoveryClaimLabel, "alice", nonceHex)
	require.NoError(t, err)
	assertShareClaimSignature(t, pub, qrdbsdk.RecoveryClaimLabel, "alice", nonceHex, sig)
}

// TestSignShareClaimWithKeyFile_RSAPKCS1 signs with an RSA PKCS1 key.
func TestSignShareClaimWithKeyFile_RSAPKCS1(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "rsa.key")

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der := x509.MarshalPKCS1PrivateKey(priv)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})
	require.NoError(t, os.WriteFile(keyPath, pemBytes, 0o600))

	sig, err := signShareClaimWithKeyFile(keyPath, qrdbsdk.SystemClaimLabel, "alice", nonceHex)
	require.NoError(t, err)
	assertShareClaimSignature(t, &priv.PublicKey, qrdbsdk.SystemClaimLabel, "alice", nonceHex, sig)
}

// TestSignShareClaimWithKeyFile_RSAPKCS8 signs with an RSA PKCS8 key.
func TestSignShareClaimWithKeyFile_RSAPKCS8(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "rsa-pkcs8.key")

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	require.NoError(t, os.WriteFile(keyPath, pemBytes, 0o600))

	sig, err := signShareClaimWithKeyFile(keyPath, qrdbsdk.SystemClaimLabel, "alice", nonceHex)
	require.NoError(t, err)
	assertShareClaimSignature(t, &priv.PublicKey, qrdbsdk.SystemClaimLabel, "alice", nonceHex, sig)
}

// TestSignShareClaimWithKeyFile_EmptyNonce surfaces the SDK's refusal to sign
// a claim over an empty nonce as the typed InitCSRKeySignError.
func TestSignShareClaimWithKeyFile_EmptyNonce(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ec.key")
	writeECPEM(t, keyPath)

	_, err := signShareClaimWithKeyFile(keyPath, qrdbsdk.SystemClaimLabel, "alice", "")
	require.Error(t, err)
	var typed *InitCSRKeySignError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, keyPath, typed.Path)
}

// TestSignShareClaimWithKeyFile_CorruptPEM rejects malformed PEM input with
// the typed InitCSRKeySignError.
func TestSignShareClaimWithKeyFile_CorruptPEM(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "broken.key")
	require.NoError(t, os.WriteFile(keyPath, []byte("this is not a PEM"), 0o600))

	_, err := signShareClaimWithKeyFile(keyPath, qrdbsdk.SystemClaimLabel, "alice", nonceHex)
	require.Error(t, err)
	var typed *InitCSRKeySignError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, keyPath, typed.Path)
}

// TestSignShareClaimWithKeyFile_BadHexNonce rejects a malformed nonce.
func TestSignShareClaimWithKeyFile_BadHexNonce(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ec.key")
	writeECPEM(t, keyPath)

	_, err := signShareClaimWithKeyFile(keyPath, qrdbsdk.SystemClaimLabel, "alice", "zzznothex")
	require.Error(t, err)
	var typed *InitCSRKeySignError
	require.ErrorAs(t, err, &typed)
}

// TestSignShareClaimWithKeyFile_MissingFile surfaces the OS-level read error.
func TestSignShareClaimWithKeyFile_MissingFile(t *testing.T) {
	_, err := signShareClaimWithKeyFile("/no/such/key.pem", qrdbsdk.SystemClaimLabel, "alice", nonceHex)
	require.Error(t, err)
	var typed *InitCSRKeySignError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "/no/such/key.pem", typed.Path)
}

// x25519PKCS8DER is a minimal valid PKCS8 wrapper around an X25519
// private key. ParsePKCS8PrivateKey returns *ecdh.PrivateKey for this
// algorithm, which does NOT implement crypto.Signer. We use it to
// exercise the "key does not implement crypto.Signer" branch in
// signShareClaimWithKeyFile.
var x25519PKCS8DER = []byte{
	0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x6e,
	0x04, 0x22, 0x04, 0x20,
	// 32 bytes of private key material
	0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
	0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
	0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
	0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
}

// TestSignShareClaimWithKeyFile_NonSignerKey verifies the typed error when
// the parsed key does not satisfy crypto.Signer (X25519 -> *ecdh.PrivateKey).
func TestSignShareClaimWithKeyFile_NonSignerKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "x25519.key")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: x25519PKCS8DER})
	require.NoError(t, os.WriteFile(keyPath, pemBytes, 0o600))

	_, err := signShareClaimWithKeyFile(keyPath, qrdbsdk.SystemClaimLabel, "alice", nonceHex)
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
