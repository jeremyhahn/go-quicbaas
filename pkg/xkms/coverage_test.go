// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package xkms

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupSymmetricTestXKMS initializes xkms with a symmetric-capable backend
// registered as "software". The symmetric backend implements the
// ImportExportBackend interface (GetImportParameters/WrapKey/ImportKey) and the
// SymmetricKeyProvider interface, exercising code paths the PKCS8 backend used
// by setupFullTestXKMS cannot reach.
func setupSymmetricTestXKMS(t *testing.T) {
	t.Helper()

	Reset()
	ResetRNG()

	symProvider, err := xkmssdk.NewSymmetricBackend(&xkmssdk.SymmetricBackendConfig{
		KeyStorage: xkmssdk.NewStorageMemory(),
	})
	require.NoError(t, err)

	certStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)

	ks, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     symProvider,
		CertStorage: certStorage,
	})
	require.NoError(t, err)

	err = Initialize(&Config{
		DefaultBackend: "software",
		Backends: map[string]xkmssdk.ServiceBackend{
			"software": ks,
		},
		Logger: &mockLogger{},
	})
	require.NoError(t, err)
}

func symmetricKeyAttrs(cn string) *xkmssdk.KeyAttributes {
	return &xkmssdk.KeyAttributes{
		CN:                 cn,
		KeyType:            xkmssdk.KeyTypeSecret,
		StoreType:          xkmssdk.StoreSoftware,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}
}

// parseJWTHeader returns the decoded header of a JWT without verifying it.
func parseJWTHeader(t *testing.T, token string) map[string]interface{} {
	t.Helper()
	parser := jwt.NewParser()
	parsed, _, err := parser.ParseUnverified(token, jwt.MapClaims{})
	require.NoError(t, err)
	return parsed.Header
}

// -----------------------------------------------------------------------------
// Symmetric key success paths
// -----------------------------------------------------------------------------

// TestGenerateSymmetricKey_SuccessWithSymmetricBackend exercises the success
// branch of GenerateSymmetricKey, which the PKCS8 backend cannot reach.
func TestGenerateSymmetricKey_SuccessWithSymmetricBackend(t *testing.T) {
	setupSymmetricTestXKMS(t)

	key, err := GenerateSymmetricKey("software", symmetricKeyAttrs("gen-sym-success"))
	require.NoError(t, err)
	assert.NotNil(t, key)
}

// TestGetSymmetricKey_SuccessWithSymmetricBackend generates then retrieves a
// symmetric key to cover the success branch of GetSymmetricKey.
func TestGetSymmetricKey_SuccessWithSymmetricBackend(t *testing.T) {
	setupSymmetricTestXKMS(t)

	attrs := symmetricKeyAttrs("get-sym-success")
	_, err := GenerateSymmetricKey("software", attrs)
	require.NoError(t, err)

	// Reference format is backend:type:algo:keyname.
	key, err := GetSymmetricKey("software:secret:aes256:get-sym-success")
	require.NoError(t, err)
	assert.NotNil(t, key)
}

// TestImportSymmetricKey_SuccessImportExportFlow exercises the full
// ImportExportBackend fallback path (GetImportParameters -> WrapKey ->
// ImportKey) inside ImportSymmetricKey. The symmetric provider does not
// implement RawKeyImporter, so this path is taken.
func TestImportSymmetricKey_SuccessImportExportFlow(t *testing.T) {
	setupSymmetricTestXKMS(t)

	material := make([]byte, 32) // 256-bit key for AES-256-GCM
	_, err := rand.Read(material)
	require.NoError(t, err)

	err = ImportSymmetricKey("software", symmetricKeyAttrs("import-sym-success"), material)
	require.NoError(t, err)
}

// -----------------------------------------------------------------------------
// DefaultBackendName
// -----------------------------------------------------------------------------

// TestDefaultBackendName_NotInitialized verifies the guard clause.
func TestDefaultBackendName_NotInitialized(t *testing.T) {
	Reset()

	_, err := DefaultBackendName()
	assert.ErrorIs(t, err, ErrNotInitialized)
}

// TestDefaultBackendName_SingleBackend covers the unambiguous single-backend
// shortcut in DefaultBackendName.
func TestDefaultBackendName_SingleBackend(t *testing.T) {
	setupFullTestXKMS(t)

	name, err := DefaultBackendName()
	require.NoError(t, err)
	assert.Equal(t, "software", name)
}

// -----------------------------------------------------------------------------
// JWTSignWithSignerKID
// -----------------------------------------------------------------------------

// TestJWTSignWithSignerKID_WithKID signs with a kid and verifies the kid header
// is set on the resulting token.
func TestJWTSignWithSignerKID_WithKID(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	claims := JWTClaims{"sub": "kid-test"}
	token, err := JWTSignWithSignerKID(key, claims, "ES256", "key-2026")
	require.NoError(t, err)
	require.NotEmpty(t, token)

	header := parseJWTHeader(t, token)
	assert.Equal(t, "key-2026", header["kid"])
}

// TestJWTSignWithSignerKID_EmptyKID verifies that an empty kid behaves like
// JWTSignWithSigner and omits the kid header entirely.
func TestJWTSignWithSignerKID_EmptyKID(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	token, err := JWTSignWithSignerKID(key, JWTClaims{"sub": "no-kid"}, "ES256", "")
	require.NoError(t, err)
	require.NotEmpty(t, token)

	header := parseJWTHeader(t, token)
	_, hasKID := header["kid"]
	assert.False(t, hasKID, "kid header must be absent when kid is empty")
}

// TestJWTSignWithSignerKID_UnsupportedAlgorithm covers the unknown-algorithm
// error branch.
func TestJWTSignWithSignerKID_UnsupportedAlgorithm(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	_, err = JWTSignWithSignerKID(key, JWTClaims{"sub": "x"}, "NOPE", "kid-1")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJWTSigningFailed)
}

// TestJWTSignWithSignerKID_SignError covers the SignedString failure branch by
// using a signer that is incompatible with the requested algorithm.
func TestJWTSignWithSignerKID_SignError(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// RS256 requires an RSA key; the ECDSA signer triggers a signing error.
	_, err = JWTSignWithSignerKID(ecKey, JWTClaims{"sub": "x"}, "RS256", "kid-1")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJWTSigningFailed)
}

// -----------------------------------------------------------------------------
// JWTSign / NewJWTSigner error paths
// -----------------------------------------------------------------------------

// TestNewJWTSigner_KeyNotFound exercises the signer key-resolution callback
// failure path through SignWithKeyID.
func TestNewJWTSigner_KeyNotFound(t *testing.T) {
	setupFullTestXKMS(t)

	signer := NewJWTSigner()
	_, err := signer.SignWithKeyID("software:signing:ecdsa-p256:does-not-exist", JWTClaims{"sub": "x"})
	require.Error(t, err)
}
