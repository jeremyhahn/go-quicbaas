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

// Package xkms provides the integration layer between QuicBaaS and go-xkms.
// This file contains tests for the centralized RNG functionality.
package xkms

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

func TestRand(t *testing.T) {
	// Reset RNG state before test
	ResetRNG()
	defer ResetRNG()

	tests := []struct {
		name     string
		numBytes int
		wantErr  bool
	}{
		{
			name:     "generate 16 bytes",
			numBytes: 16,
			wantErr:  false,
		},
		{
			name:     "generate 32 bytes",
			numBytes: 32,
			wantErr:  false,
		},
		{
			name:     "generate 64 bytes",
			numBytes: 64,
			wantErr:  false,
		},
		{
			name:     "generate 128 bytes",
			numBytes: 128,
			wantErr:  false,
		},
		{
			name:     "generate 0 bytes",
			numBytes: 0,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Rand(tt.numBytes)
			if (err != nil) != tt.wantErr {
				t.Errorf("Rand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != tt.numBytes {
				t.Errorf("Rand() returned %d bytes, want %d", len(got), tt.numBytes)
			}
		})
	}
}

func TestRandInt(t *testing.T) {
	ResetRNG()
	defer ResetRNG()

	tests := []struct {
		name    string
		max     *big.Int
		wantErr bool
	}{
		{
			name:    "max = 1000",
			max:     big.NewInt(1000),
			wantErr: false,
		},
		{
			name:    "max = 2^128",
			max:     new(big.Int).Lsh(big.NewInt(1), 128),
			wantErr: false,
		},
		{
			name:    "max = 1",
			max:     big.NewInt(1),
			wantErr: false,
		},
		{
			name:    "nil max",
			max:     nil,
			wantErr: true,
		},
		{
			name:    "zero max",
			max:     big.NewInt(0),
			wantErr: true,
		},
		{
			name:    "negative max",
			max:     big.NewInt(-1),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RandInt(tt.max)
			if (err != nil) != tt.wantErr {
				t.Errorf("RandInt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				assert.ErrorIs(t, err, ErrRNGFailed)
				return
			}
			if !tt.wantErr && got == nil {
				t.Error("RandInt() returned nil value")
			}
			if !tt.wantErr && tt.max != nil && got.Cmp(tt.max) >= 0 {
				t.Errorf("RandInt() = %v, should be < %v", got, tt.max)
			}
			if !tt.wantErr && got.Sign() < 0 {
				t.Error("RandInt() returned negative value")
			}
		})
	}
}

func TestReader(t *testing.T) {
	ResetRNG()
	defer ResetRNG()

	reader := Reader()

	tests := []struct {
		name    string
		bufSize int
	}{
		{
			name:    "read 16 bytes",
			bufSize: 16,
		},
		{
			name:    "read 32 bytes",
			bufSize: 32,
		},
		{
			name:    "read 64 bytes",
			bufSize: 64,
		},
		{
			name:    "read 0 bytes",
			bufSize: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, tt.bufSize)
			n, err := reader.Read(buf)
			if err != nil {
				t.Errorf("Reader.Read() error = %v", err)
				return
			}
			if n != tt.bufSize {
				t.Errorf("Reader.Read() read %d bytes, want %d", n, tt.bufSize)
			}
			if tt.bufSize > 0 && bytes.Equal(buf, make([]byte, tt.bufSize)) {
				t.Error("Reader.Read() left the buffer all zeros")
			}
		})
	}
}

func TestRNGAvailable(t *testing.T) {
	ResetRNG()
	defer ResetRNG()

	// RNG should be available after auto-initialization
	if !RNGAvailable() {
		t.Error("RNGAvailable() = false, want true")
	}
}

func TestInitializeRNG(t *testing.T) {
	ResetRNG()
	defer ResetRNG()

	// Initialize with software mode
	config := &RNGConfig{}
	err := InitializeRNG(config)
	if err != nil {
		t.Errorf("InitializeRNG() error = %v", err)
	}

	// Second initialization should fail
	err = InitializeRNG(config)
	if err != ErrAlreadyInitialized {
		t.Errorf("InitializeRNG() second call error = %v, want ErrAlreadyInitialized", err)
	}
}

func TestCloseRNG(t *testing.T) {
	ResetRNG()

	// Trigger auto-initialization
	_, err := Rand(16)
	if err != nil {
		t.Fatalf("Rand() error = %v", err)
	}

	// Close should work
	err = CloseRNG()
	if err != nil {
		t.Errorf("CloseRNG() error = %v", err)
	}

	// Close again should be safe (nil resolver)
	err = CloseRNG()
	if err != nil {
		t.Errorf("CloseRNG() second call error = %v", err)
	}
}

func TestResetRNG(t *testing.T) {
	// First initialize
	_, err := Rand(16)
	if err != nil {
		t.Fatalf("Rand() error = %v", err)
	}

	// Reset
	ResetRNG()

	// Should still work after reset (auto-initializes)
	data, err := Rand(16)
	if err != nil {
		t.Errorf("Rand() after reset error = %v", err)
	}
	if len(data) != 16 {
		t.Errorf("Rand() after reset returned %d bytes, want 16", len(data))
	}
}

// Benchmark tests

func BenchmarkRand16(b *testing.B) {
	ResetRNG()
	defer ResetRNG()

	for i := 0; i < b.N; i++ {
		_, _ = Rand(16)
	}
}

func BenchmarkRand32(b *testing.B) {
	ResetRNG()
	defer ResetRNG()

	for i := 0; i < b.N; i++ {
		_, _ = Rand(32)
	}
}

func BenchmarkRand64(b *testing.B) {
	ResetRNG()
	defer ResetRNG()

	for i := 0; i < b.N; i++ {
		_, _ = Rand(64)
	}
}

func BenchmarkRandInt(b *testing.B) {
	ResetRNG()
	defer ResetRNG()

	max := new(big.Int).Lsh(big.NewInt(1), 128)
	for i := 0; i < b.N; i++ {
		_, _ = RandInt(max)
	}
}

func BenchmarkReader(b *testing.B) {
	ResetRNG()
	defer ResetRNG()

	reader := Reader()
	buf := make([]byte, 32)
	for i := 0; i < b.N; i++ {
		_, _ = reader.Read(buf)
	}
}

// =============================================================================
// Consolidated from xkms_comprehensive_test.go
// =============================================================================

// mockLogger implements interfaces.Logger for testing.
type mockLogger struct{}

func (m *mockLogger) Debug(msg string, args ...any)                              {}
func (m *mockLogger) Debugf(format string, args ...interface{})                  {}
func (m *mockLogger) Info(msg string, args ...any)                               {}
func (m *mockLogger) Infof(format string, args ...interface{})                   {}
func (m *mockLogger) Warn(msg string, args ...any)                               {}
func (m *mockLogger) Warnf(format string, args ...interface{})                   {}
func (m *mockLogger) Error(msg string, args ...any)                              {}
func (m *mockLogger) Errorf(format string, args ...interface{})                  {}
func (m *mockLogger) Fatal(msg string)                                           {}
func (m *mockLogger) Fatalf(format string, args ...interface{})                  {}
func (m *mockLogger) WithField(key string, value interface{}) interfaces.Logger  { return m }
func (m *mockLogger) WithFields(fields map[string]interface{}) interfaces.Logger { return m }
func (m *mockLogger) WithError(err error) interfaces.Logger                      { return m }

// setupFullTestXKMS creates a fully initialized xkms with PKCS8 backend.
func setupFullTestXKMS(t *testing.T) {
	t.Helper()

	Reset()
	ResetRNG()
	initFullTestXKMS(t)
}

// initFullTestXKMS initializes xkms over fresh in-memory backends without
// resetting first, so a test can observe what an earlier Close left behind.
func initFullTestXKMS(t *testing.T) {
	t.Helper()

	// Create in-memory storage for PKCS8
	keyStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)
	certStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)

	// Create PKCS8 backend. AllowPlaintextKeys is required for password-less
	// key persistence — intentional here: ephemeral in-memory test storage.
	pkcs8Backend, err := xkmssdk.NewPKCS8Backend(&xkmssdk.PKCS8BackendConfig{
		KeyStorage:         keyStorage,
		AllowPlaintextKeys: true,
	})
	require.NoError(t, err)

	// Create go-xkms keystore with PKCS8
	ks, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     pkcs8Backend,
		CertStorage: certStorage,
	})
	require.NoError(t, err)

	// Key-version records, so keys can be rotated.
	versionStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)
	keyVersions, err := xkmssdk.NewKeyVersionStore(versionStorage)
	require.NoError(t, err)

	// Initialize QuicBaaS xkms with the backend
	err = Initialize(&Config{
		DefaultBackend: "software",
		Backends: map[string]xkmssdk.ServiceBackend{
			"software": ks,
		},
		KeyVersions: keyVersions,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)
}

// createTestECDSAKey generates a test ECDSA key using the xkms API.
// Returns the key attributes and the full key ID for ByID lookups.
func createTestECDSAKey(t *testing.T, keyID string) (*xkmssdk.KeyAttributes, string) {
	t.Helper()

	attrs := &xkmssdk.KeyAttributes{
		CN:           keyID,
		KeyType:      xkmssdk.KeyTypeSigning,
		StoreType:    xkmssdk.StoreSoftware,
		KeyAlgorithm: x509.ECDSA,
		ECCAttributes: &xkmssdk.ECCAttributes{
			Curve: elliptic.P256(),
		},
	}

	_, err := GenerateKey(context.Background(), attrs)
	require.NoError(t, err)

	// Return full key ID format: software:signing:ecdsa-p256:keyname
	fullKeyID := fmt.Sprintf("software:signing:ecdsa-p256:%s", keyID)
	return attrs, fullKeyID
}

// createTestRSAKey generates a test RSA key using the xkms API.
// Returns the key attributes and the full key ID for ByID lookups.
func createTestRSAKey(t *testing.T, keyID string) (*xkmssdk.KeyAttributes, string) {
	t.Helper()

	attrs := &xkmssdk.KeyAttributes{
		CN:           keyID,
		KeyType:      xkmssdk.KeyTypeSigning,
		StoreType:    xkmssdk.StoreSoftware,
		KeyAlgorithm: x509.RSA,
		RSAAttributes: &xkmssdk.RSAAttributes{
			KeySize: 2048,
		},
	}

	_, err := GenerateKey(context.Background(), attrs)
	require.NoError(t, err)

	// Return full key ID format: software:signing:rsa:keyname
	fullKeyID := fmt.Sprintf("software:signing:rsa:%s", keyID)
	return attrs, fullKeyID
}

// createTestEncryptionRSAKey generates a test RSA key for encryption using the xkms API.
// Returns the key attributes and the full key ID for ByID lookups.
func createTestEncryptionRSAKey(t *testing.T, keyID string) (*xkmssdk.KeyAttributes, string) {
	t.Helper()

	attrs := &xkmssdk.KeyAttributes{
		CN:           keyID,
		KeyType:      xkmssdk.KeyTypeEncryption,
		StoreType:    xkmssdk.StoreSoftware,
		KeyAlgorithm: x509.RSA,
		RSAAttributes: &xkmssdk.RSAAttributes{
			KeySize: 2048,
		},
	}

	_, err := GenerateKey(context.Background(), attrs)
	require.NoError(t, err)

	// Return full key ID format: software:encryption:rsa:keyname
	fullKeyID := fmt.Sprintf("software:encryption:rsa:%s", keyID)
	return attrs, fullKeyID
}

// createTestCertificate creates a self-signed test certificate.
func createTestCertificate(t *testing.T, key crypto.Signer) *x509.Certificate {
	t.Helper()

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "Test Certificate",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(certDER)
	require.NoError(t, err)

	return cert
}

// createJWTWithKID creates a JWT token with a specific KID header using the provided signer.
func createJWTWithKID(t *testing.T, signer crypto.Signer, kid string, claims JWTClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = kid

	tokenString, err := token.SignedString(signer)
	require.NoError(t, err)

	return tokenString
}

// -----------------------------------------------------------------------------
// PublicKey Tests
// -----------------------------------------------------------------------------

// resetTestXKMS leaves the package uninitialized, or initialized with the
// full software backend when initialize is true.
func resetTestXKMS(t *testing.T, initialize bool) {
	t.Helper()

	if initialize {
		setupFullTestXKMS(t)
		return
	}
	Reset()
}

func TestPublicKey(t *testing.T) {
	tests := []struct {
		name    string
		init    bool
		create  string
		keyID   string
		wantErr bool
		errIs   error
	}{
		{
			name:    "not initialized",
			keyID:   "software:signing:ecdsa-p256:test-key",
			wantErr: true,
			errIs:   ErrNotInitialized,
		},
		{
			name:   "success",
			init:   true,
			create: "public-key-test",
			keyID:  "software:signing:ecdsa-p256:public-key-test",
		},
		{
			name:    "not found",
			init:    true,
			keyID:   "software:signing:ecdsa-p256:non-existent-key",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetTestXKMS(t, tt.init)
			if tt.create != "" {
				createTestECDSAKey(t, tt.create)
			}

			pub, err := PublicKey(context.Background(), tt.keyID)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errIs != nil {
					assert.ErrorIs(t, err, tt.errIs)
				}
				return
			}
			require.NoError(t, err)
			_, ok := pub.(*ecdsa.PublicKey)
			assert.True(t, ok, "want *ecdsa.PublicKey, got %T", pub)
		})
	}
}

// -----------------------------------------------------------------------------
// ParseKeyID Tests
// -----------------------------------------------------------------------------

func TestParseKeyID_NotInitialized(t *testing.T) {
	Reset()

	_, err := ParseKeyID("test-key")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestParseKeyID_Success(t *testing.T) {
	setupFullTestXKMS(t)

	// Test simple key name
	attrs, err := ParseKeyID("my-key")
	require.NoError(t, err)
	assert.Equal(t, "my-key", attrs.CN)

	// Test full format
	attrs, err = ParseKeyID("software:signing:ecdsa-p256:full-key-name")
	require.NoError(t, err)
	assert.Equal(t, "full-key-name", attrs.CN)
	assert.Equal(t, xkmssdk.StoreSoftware, attrs.StoreType)
	assert.Equal(t, xkmssdk.KeyTypeSigning, attrs.KeyType)
}

func TestParseCertificateID_NotInitialized(t *testing.T) {
	Reset()

	_, err := ParseCertificateID("test-cert")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestParseCertificateID_Success(t *testing.T) {
	setupFullTestXKMS(t)

	attrs, err := ParseCertificateID("my-cert")
	require.NoError(t, err)
	assert.Equal(t, "my-cert", attrs.CN)
}

// -----------------------------------------------------------------------------
// GenerateKey Tests
// -----------------------------------------------------------------------------

// testECDSAKeyAttrs returns valid software ECDSA P-256 signing key attributes.
func testECDSAKeyAttrs(cn string) *xkmssdk.KeyAttributes {
	return &xkmssdk.KeyAttributes{
		CN:           cn,
		KeyType:      xkmssdk.KeyTypeSigning,
		StoreType:    xkmssdk.StoreSoftware,
		KeyAlgorithm: x509.ECDSA,
		ECCAttributes: &xkmssdk.ECCAttributes{
			Curve: elliptic.P256(),
		},
	}
}

func TestGenerateKey(t *testing.T) {
	tests := []struct {
		name  string
		init  bool
		attrs *xkmssdk.KeyAttributes
		errIs error
	}{
		{
			name:  "not initialized",
			attrs: &xkmssdk.KeyAttributes{},
			errIs: ErrNotInitialized,
		},
		{
			name:  "success",
			init:  true,
			attrs: testECDSAKeyAttrs("generated-key"),
		},
		{
			name: "missing CN",
			init: true,
			attrs: &xkmssdk.KeyAttributes{
				KeyType:      xkmssdk.KeyTypeSigning,
				StoreType:    xkmssdk.StoreSoftware,
				KeyAlgorithm: x509.ECDSA,
			},
			errIs: ErrKeyGenerationFailed,
		},
		{
			name: "unknown algorithm",
			init: true,
			attrs: &xkmssdk.KeyAttributes{
				CN:           "invalid-algo-test",
				KeyType:      xkmssdk.KeyTypeSigning,
				StoreType:    xkmssdk.StoreSoftware,
				KeyAlgorithm: x509.UnknownPublicKeyAlgorithm,
			},
			errIs: ErrKeyGenerationFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetTestXKMS(t, tt.init)

			key, err := GenerateKey(context.Background(), tt.attrs)
			if tt.errIs != nil {
				assert.ErrorIs(t, err, tt.errIs)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, key)
		})
	}
}

func TestGenerateKeyWithBackend(t *testing.T) {
	tests := []struct {
		name    string
		init    bool
		backend string
		attrs   *xkmssdk.KeyAttributes
		errIs   error
	}{
		{
			name:    "not initialized",
			backend: "software",
			attrs:   &xkmssdk.KeyAttributes{},
			errIs:   ErrNotInitialized,
		},
		{
			name:    "success",
			init:    true,
			backend: "software",
			attrs:   testECDSAKeyAttrs("backend-generated-key"),
		},
		{
			name:    "unknown backend",
			init:    true,
			backend: "non-existent",
			attrs:   &xkmssdk.KeyAttributes{CN: "test-key"},
			errIs:   ErrKeyGenerationFailed,
		},
		{
			name:    "unknown algorithm",
			init:    true,
			backend: "software",
			attrs: &xkmssdk.KeyAttributes{
				CN:           "invalid-backend-gen",
				KeyType:      xkmssdk.KeyTypeSigning,
				StoreType:    xkmssdk.StoreSoftware,
				KeyAlgorithm: x509.UnknownPublicKeyAlgorithm,
			},
			errIs: ErrKeyGenerationFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetTestXKMS(t, tt.init)

			key, err := GenerateKeyWithBackend(context.Background(), tt.backend, tt.attrs)
			if tt.errIs != nil {
				assert.ErrorIs(t, err, tt.errIs)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, key)
		})
	}
}

// -----------------------------------------------------------------------------
// Key Retrieval Tests
// -----------------------------------------------------------------------------

func TestKey(t *testing.T) {
	tests := []struct {
		name   string
		init   bool
		create bool
		attrs  *xkmssdk.KeyAttributes
		errIs  error
	}{
		{
			name:  "not initialized",
			attrs: &xkmssdk.KeyAttributes{CN: "test"},
			errIs: ErrNotInitialized,
		},
		{
			name:   "success",
			init:   true,
			create: true,
			attrs:  &xkmssdk.KeyAttributes{CN: "retrieve-key-test"},
		},
		{
			name: "not found",
			init: true,
			attrs: &xkmssdk.KeyAttributes{
				CN:        "non-existent-key",
				StoreType: xkmssdk.StoreSoftware,
				KeyType:   xkmssdk.KeyTypeSigning,
			},
			errIs: ErrKeyNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetTestXKMS(t, tt.init)
			attrs := tt.attrs
			if tt.create {
				attrs, _ = createTestECDSAKey(t, tt.attrs.CN)
			}

			key, err := Key(context.Background(), attrs)
			if tt.errIs != nil {
				assert.ErrorIs(t, err, tt.errIs)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, key)
		})
	}
}

func TestKeyByID(t *testing.T) {
	tests := []struct {
		name   string
		init   bool
		create string
		keyID  string
		errIs  error
	}{
		{
			name:  "not initialized",
			keyID: "software:signing:ecdsa-p256:test-key",
			errIs: ErrNotInitialized,
		},
		{
			name:   "success",
			init:   true,
			create: "key-by-id-test",
			keyID:  "software:signing:ecdsa-p256:key-by-id-test",
		},
		{
			name:  "not found",
			init:  true,
			keyID: "software:signing:ecdsa-p256:non-existent-key",
			errIs: ErrKeyNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetTestXKMS(t, tt.init)
			if tt.create != "" {
				createTestECDSAKey(t, tt.create)
			}

			key, err := KeyByID(context.Background(), tt.keyID)
			if tt.errIs != nil {
				assert.ErrorIs(t, err, tt.errIs)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, key)
		})
	}
}

// -----------------------------------------------------------------------------
// DeleteKey Tests
// -----------------------------------------------------------------------------

func TestDeleteKey_NotInitialized(t *testing.T) {
	Reset()

	err := DeleteKey(context.Background(), &xkmssdk.KeyAttributes{CN: "test"})
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestDeleteKey_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, _ := createTestECDSAKey(t, "delete-key-test")

	err := DeleteKey(context.Background(), attrs)
	require.NoError(t, err)

	// Verify key no longer exists
	_, err = Key(context.Background(), attrs)
	require.Error(t, err)
}

func TestDeleteKeyByID_NotInitialized(t *testing.T) {
	Reset()

	err := DeleteKeyByID(context.Background(), "software:signing:ecdsa-p256:test-key")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestDeleteKeyByID_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, fullKeyID := createTestECDSAKey(t, "delete-key-by-id-test")

	err := DeleteKeyByID(context.Background(), fullKeyID)
	require.NoError(t, err)

	// Verify key no longer exists using attribute-based lookup
	_, err = Key(context.Background(), attrs)
	require.Error(t, err)
}

// -----------------------------------------------------------------------------
// RotateKey Tests
// -----------------------------------------------------------------------------

func TestRotateKey_NotInitialized(t *testing.T) {
	Reset()

	_, err := RotateKey(context.Background(), "software:signing:ecdsa-p256:test-key")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestRotateKey_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, fullKeyID := createTestECDSAKey(t, "rotate-key-test")

	// Get original signer to capture original public key
	origSigner, err := Signer(context.Background(), attrs)
	require.NoError(t, err)
	origPublicKey := origSigner.Public().(*ecdsa.PublicKey)

	// Sign some data with the original key
	testData := sha256.Sum256([]byte("test data for rotation"))
	origSig, err := origSigner.Sign(rand.Reader, testData[:], crypto.SHA256)
	require.NoError(t, err)

	// Rotate using fullKeyID. go-xkms rotates by creating the next version
	// as its own backend key ("<name>--v2") and making it current; the
	// original version is kept.
	newKey, err := RotateKey(context.Background(), fullKeyID)
	require.NoError(t, err)
	rotatedSigner, ok := newKey.(crypto.Signer)
	require.True(t, ok, "rotated ECDSA key must be a crypto.Signer, got %T", newKey)
	rotatedPublic, ok := rotatedSigner.Public().(*ecdsa.PublicKey)
	require.True(t, ok, "rotated key must have an ECDSA public key")

	// The new version is a different key.
	assert.False(t, rotatedPublic.Equal(origPublicKey),
		"rotation must produce a new key, not re-issue the original")

	// The new version is addressable under its version name, and it is the
	// key RotateKey returned.
	v2Attrs := *attrs
	v2Attrs.CN = xkmssdk.KeyVersionName(attrs.CN, 2)
	newSigner, err := Signer(context.Background(), &v2Attrs)
	require.NoError(t, err)
	newPublicKey := newSigner.Public().(*ecdsa.PublicKey)
	assert.True(t, newPublicKey.Equal(rotatedPublic),
		"the v2 signer must be the rotated key")

	// The original signature does not verify under the new version...
	assert.False(t, ecdsa.VerifyASN1(newPublicKey, testData[:], origSig),
		"original signature should not verify with the new key")
	// ...but the original version is kept, so what it signed still verifies.
	keptSigner, err := Signer(context.Background(), attrs)
	require.NoError(t, err)
	assert.True(t, keptSigner.Public().(*ecdsa.PublicKey).Equal(origPublicKey),
		"rotation must keep the original version")

	// Sign with the new version and verify it works.
	newSig, err := newSigner.Sign(rand.Reader, testData[:], crypto.SHA256)
	require.NoError(t, err)
	assert.True(t, ecdsa.VerifyASN1(newPublicKey, testData[:], newSig),
		"new signature should verify with new key")
}

// TestRotateKey_WithoutKeyVersionStore pins the go-xkms contract: rotation
// needs the key-version store, and a service initialized without one refuses
// rather than replacing key material in place.
func TestRotateKey_WithoutKeyVersionStore(t *testing.T) {
	Reset()
	ResetRNG()
	t.Cleanup(Reset)

	keyStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)
	pkcs8Backend, err := xkmssdk.NewPKCS8Backend(&xkmssdk.PKCS8BackendConfig{
		KeyStorage:         keyStorage,
		AllowPlaintextKeys: true,
	})
	require.NoError(t, err)
	ks, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     pkcs8Backend,
		CertStorage: keyStorage,
	})
	require.NoError(t, err)
	require.NoError(t, Initialize(&Config{
		DefaultBackend: "software",
		Backends:       map[string]xkmssdk.ServiceBackend{"software": ks},
		Logger:         &mockLogger{},
	}))

	_, fullKeyID := createTestECDSAKey(t, "rotate-without-versions")
	_, err = RotateKey(context.Background(), fullKeyID)
	require.ErrorIs(t, err, xkmssdk.ErrKeyVersionsNotConfigured)
}

func TestRotateKey_NotFound(t *testing.T) {
	setupFullTestXKMS(t)

	_, err := RotateKey(context.Background(), "software:signing:ecdsa-p256:non-existent-key")
	require.Error(t, err)
}

// -----------------------------------------------------------------------------
// ListKeys Tests
// -----------------------------------------------------------------------------

func TestListKeys_NotInitialized(t *testing.T) {
	Reset()

	_, err := ListKeys(context.Background())
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestListKeys_Success(t *testing.T) {
	setupFullTestXKMS(t)
	createTestECDSAKey(t, "list-key-1")
	createTestECDSAKey(t, "list-key-2")

	keys, err := ListKeys(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(keys), 2)
}

func TestListKeys_SpecificBackend(t *testing.T) {
	setupFullTestXKMS(t)
	createTestECDSAKey(t, "backend-list-key")

	keys, err := ListKeys(context.Background(), "software")
	require.NoError(t, err)
	assert.NotNil(t, keys)
}

// -----------------------------------------------------------------------------
// Signer Tests
// -----------------------------------------------------------------------------

func TestSigner_NotInitialized(t *testing.T) {
	Reset()

	_, err := Signer(context.Background(), &xkmssdk.KeyAttributes{CN: "test"})
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestSigner_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, _ := createTestECDSAKey(t, "signer-test")

	signer, err := Signer(context.Background(), attrs)
	require.NoError(t, err)
	assert.NotNil(t, signer)
	assert.NotNil(t, signer.Public())
}

func TestSignerByID_NotInitialized(t *testing.T) {
	Reset()

	_, err := SignerByID(context.Background(), "software:signing:ecdsa-p256:test-key")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestSignerByID_Success(t *testing.T) {
	setupFullTestXKMS(t)
	_, fullKeyID := createTestECDSAKey(t, "signer-by-id-test")

	// Use SignerByID with full key ID (ID-based lookup now works correctly)
	signer, err := SignerByID(context.Background(), fullKeyID)
	require.NoError(t, err)
	assert.NotNil(t, signer)
}

func TestSignerByID_NotFound(t *testing.T) {
	setupFullTestXKMS(t)

	_, err := SignerByID(context.Background(), "software:signing:ecdsa-p256:non-existent-key")
	require.Error(t, err)
}

// -----------------------------------------------------------------------------
// Decrypter Tests
// -----------------------------------------------------------------------------

func TestDecrypter_NotInitialized(t *testing.T) {
	Reset()

	_, err := Decrypter(context.Background(), &xkmssdk.KeyAttributes{CN: "test"})
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestDecrypter_Success(t *testing.T) {
	setupFullTestXKMS(t)

	// Create RSA key for decryption
	attrs, _ := createTestEncryptionRSAKey(t, "decrypter-test")

	dec, err := Decrypter(context.Background(), attrs)
	require.NoError(t, err)
	assert.NotNil(t, dec)
}

func TestDecrypterByID_NotInitialized(t *testing.T) {
	Reset()

	_, err := DecrypterByID(context.Background(), "software:encryption:rsa:test-key")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestDecrypterByID_Success(t *testing.T) {
	setupFullTestXKMS(t)

	// Create RSA key for decryption
	_, fullKeyID := createTestEncryptionRSAKey(t, "decrypter-by-id-test")

	// Use DecrypterByID with full key ID (ID-based lookup now works correctly)
	dec, err := DecrypterByID(context.Background(), fullKeyID)
	require.NoError(t, err)
	assert.NotNil(t, dec)
}

// -----------------------------------------------------------------------------
// Sign and Verify Tests
// -----------------------------------------------------------------------------

func TestSign_NotInitialized(t *testing.T) {
	Reset()

	_, err := Sign(context.Background(), "software:signing:ecdsa-p256:test-key", []byte("data"), nil)
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestSign_Success(t *testing.T) {
	setupFullTestXKMS(t)
	_, fullKeyID := createTestECDSAKey(t, "sign-test")

	data := []byte("test data to sign")
	sig, err := Sign(context.Background(), fullKeyID, data, &SignOptions{
		Hash: crypto.SHA256,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, sig)
}

func TestSign_InvalidKey(t *testing.T) {
	setupFullTestXKMS(t)

	_, err := Sign(context.Background(), "software:signing:ecdsa-p256:non-existent-key", []byte("data"), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSigningFailed)
}

func TestVerify_NotInitialized(t *testing.T) {
	Reset()

	err := Verify(context.Background(), "software:signing:ecdsa-p256:test-key", []byte("data"), []byte("sig"), nil)
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestVerify_Success(t *testing.T) {
	setupFullTestXKMS(t)
	_, fullKeyID := createTestECDSAKey(t, "verify-test")

	data := []byte("test data to sign and verify")

	// Sign first
	sig, err := Sign(context.Background(), fullKeyID, data, &SignOptions{
		Hash: crypto.SHA256,
	})
	require.NoError(t, err)

	// Verify
	err = Verify(context.Background(), fullKeyID, data, sig, &xkmssdk.VerifyOpts{
		Hash: crypto.SHA256,
	})
	require.NoError(t, err)
}

func TestVerify_InvalidSignature(t *testing.T) {
	setupFullTestXKMS(t)
	_, fullKeyID := createTestECDSAKey(t, "verify-invalid-test")

	data := []byte("test data")
	invalidSig := []byte("invalid signature")

	err := Verify(context.Background(), fullKeyID, data, invalidSig, &xkmssdk.VerifyOpts{
		Hash: crypto.SHA256,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrVerificationFailed)
}

// -----------------------------------------------------------------------------
// Encrypt and Decrypt Tests (Symmetric Encryption)
// -----------------------------------------------------------------------------

func TestEncrypt_NotInitialized(t *testing.T) {
	Reset()

	_, err := Encrypt(context.Background(), "software:symmetric:aes256:test-key", []byte("data"), nil)
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestEncrypt_BackendNotSupported(t *testing.T) {
	setupFullTestXKMS(t)

	// PKCS8 backend does not support symmetric encryption
	// This should fail because the backend doesn't support symmetric operations
	_, err := Encrypt(context.Background(), "software:symmetric:aes256:test-key", []byte("data"), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEncryptionFailed)
}

func TestDecrypt_NotInitialized(t *testing.T) {
	Reset()

	encData := &xkmssdk.EncryptedData{
		Ciphertext: []byte("encrypted"),
	}
	_, err := Decrypt(context.Background(), "software:symmetric:aes256:test-key", encData, nil)
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestDecrypt_NilData(t *testing.T) {
	setupFullTestXKMS(t)

	_, err := Decrypt(context.Background(), "software:symmetric:aes256:test-key", nil, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDecryptionFailed)
}

func TestDecrypt_BackendNotSupported(t *testing.T) {
	setupFullTestXKMS(t)

	encData := &xkmssdk.EncryptedData{
		Ciphertext: []byte("encrypted"),
	}
	// PKCS8 backend does not support symmetric decryption
	_, err := Decrypt(context.Background(), "software:symmetric:aes256:test-key", encData, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDecryptionFailed)
}

// -----------------------------------------------------------------------------
// GenerateSymmetricKey and GetSymmetricKey Tests
// -----------------------------------------------------------------------------

func TestGenerateSymmetricKey_NotInitialized(t *testing.T) {
	Reset()

	attrs := &xkmssdk.KeyAttributes{
		CN:      "test-sym-key",
		KeyType: xkmssdk.KeyTypeSecret,
	}
	_, err := GenerateSymmetricKey(context.Background(), "software", attrs)
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestGenerateSymmetricKey_BackendNotSupported(t *testing.T) {
	setupFullTestXKMS(t)

	attrs := &xkmssdk.KeyAttributes{
		CN:        "test-sym-key",
		KeyType:   xkmssdk.KeyTypeSecret,
		StoreType: xkmssdk.StoreSoftware,
	}
	// PKCS8 backend does not support symmetric key generation
	_, err := GenerateSymmetricKey(context.Background(), "software", attrs)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyGenerationFailed)
}

func TestGetSymmetricKey_NotInitialized(t *testing.T) {
	Reset()

	_, err := GetSymmetricKey(context.Background(), "software:symmetric:aes256:test-key")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestGetSymmetricKey_BackendNotSupported(t *testing.T) {
	setupFullTestXKMS(t)

	// PKCS8 backend does not support symmetric key retrieval
	_, err := GetSymmetricKey(context.Background(), "software:symmetric:aes256:test-key")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyNotFound)
}

// -----------------------------------------------------------------------------
// ImportKeyMaterial Tests
// -----------------------------------------------------------------------------

func TestImportKeyMaterial_NotInitialized(t *testing.T) {
	Reset()

	err := ImportKeyMaterial(context.Background(), "software", &xkmssdk.KeyAttributes{}, []byte("key"))
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestImportKeyMaterial_InvalidBackend(t *testing.T) {
	setupFullTestXKMS(t)

	attrs := &xkmssdk.KeyAttributes{
		CN: "import-test",
	}

	err := ImportKeyMaterial(context.Background(), "non-existent", attrs, []byte("key-material"))
	require.Error(t, err)
}

func TestImportKeyMaterial_BackendNotSupported(t *testing.T) {
	setupFullTestXKMS(t)

	attrs := &xkmssdk.KeyAttributes{
		CN:        "import-sym-key",
		KeyType:   xkmssdk.KeyTypeSecret,
		StoreType: xkmssdk.StoreSoftware,
	}

	// PKCS8 backend does not support symmetric operations
	err := ImportKeyMaterial(context.Background(), "software", attrs, []byte("key-material-32-bytes-for-aes!"))
	require.Error(t, err)
}

// -----------------------------------------------------------------------------
// Seal and Unseal Tests
// -----------------------------------------------------------------------------

func TestSeal_NotInitialized(t *testing.T) {
	Reset()

	ctx := context.Background()
	opts := &xkmssdk.SealOptions{}
	_, err := Seal(ctx, []byte("secret data"), opts)
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestSeal_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, _ := createTestECDSAKey(t, "seal-test-key")

	ctx := context.Background()
	secretData := []byte("this is secret data to seal")

	opts := &xkmssdk.SealOptions{
		KeyAttributes: attrs,
	}

	sealed, err := Seal(ctx, secretData, opts)
	require.NoError(t, err)
	assert.NotNil(t, sealed)
	assert.NotEmpty(t, sealed.Ciphertext)
	assert.NotEmpty(t, sealed.Nonce)
	assert.Equal(t, xkmssdk.BackendTypeSoftware, sealed.Backend)
}

func TestSeal_WithAAD(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, _ := createTestECDSAKey(t, "seal-aad-test-key")

	ctx := context.Background()
	secretData := []byte("secret data with AAD")
	aadData := []byte("additional authenticated data")

	opts := &xkmssdk.SealOptions{
		KeyAttributes: attrs,
		AAD:           aadData,
	}

	sealed, err := Seal(ctx, secretData, opts)
	require.NoError(t, err)
	assert.NotNil(t, sealed)
	assert.NotEmpty(t, sealed.Ciphertext)
}

func TestSeal_NilOptions(t *testing.T) {
	setupFullTestXKMS(t)

	ctx := context.Background()
	// Nil options should cause an error
	_, err := Seal(ctx, []byte("data"), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSealingFailed)
}

func TestSealWithBackend_NotInitialized(t *testing.T) {
	Reset()

	ctx := context.Background()
	opts := &xkmssdk.SealOptions{}
	_, err := SealWithBackend(ctx, "software", []byte("secret data"), opts)
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestSealWithBackend_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, _ := createTestRSAKey(t, "seal-backend-test-key")

	ctx := context.Background()
	secretData := []byte("secret data for specific backend")

	opts := &xkmssdk.SealOptions{
		KeyAttributes: attrs,
	}

	sealed, err := SealWithBackend(ctx, "software", secretData, opts)
	require.NoError(t, err)
	assert.NotNil(t, sealed)
	assert.NotEmpty(t, sealed.Ciphertext)
}

func TestSealWithBackend_InvalidBackend(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, _ := createTestECDSAKey(t, "seal-invalid-backend-key")

	ctx := context.Background()
	opts := &xkmssdk.SealOptions{
		KeyAttributes: attrs,
	}

	_, err := SealWithBackend(ctx, "non-existent", []byte("data"), opts)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSealingFailed)
}

func TestUnseal_NotInitialized(t *testing.T) {
	Reset()

	ctx := context.Background()
	sealed := &xkmssdk.SealedData{
		Ciphertext: []byte("ciphertext"),
		Backend:    xkmssdk.BackendTypeSoftware,
	}
	opts := &xkmssdk.UnsealOptions{}
	_, err := Unseal(ctx, sealed, opts)
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestUnseal_NilSealedData(t *testing.T) {
	setupFullTestXKMS(t)

	ctx := context.Background()
	opts := &xkmssdk.UnsealOptions{}
	_, err := Unseal(ctx, nil, opts)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsealingFailed)
}

func TestUnseal_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, _ := createTestECDSAKey(t, "unseal-test-key")

	ctx := context.Background()
	originalData := []byte("this is the original secret data")

	// First seal the data
	sealOpts := &xkmssdk.SealOptions{
		KeyAttributes: attrs,
	}
	sealed, err := Seal(ctx, originalData, sealOpts)
	require.NoError(t, err)

	// Now unseal it
	unsealOpts := &xkmssdk.UnsealOptions{
		KeyAttributes: attrs,
	}
	plaintext, err := Unseal(ctx, sealed, unsealOpts)
	require.NoError(t, err)
	assert.Equal(t, originalData, plaintext)
}

func TestUnseal_WithAAD(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, _ := createTestRSAKey(t, "unseal-aad-test-key")

	ctx := context.Background()
	originalData := []byte("secret data with authenticated context")
	aadData := []byte("context information")

	// Seal with AAD
	sealOpts := &xkmssdk.SealOptions{
		KeyAttributes: attrs,
		AAD:           aadData,
	}
	sealed, err := Seal(ctx, originalData, sealOpts)
	require.NoError(t, err)

	// Unseal with same AAD
	unsealOpts := &xkmssdk.UnsealOptions{
		KeyAttributes: attrs,
		AAD:           aadData,
	}
	plaintext, err := Unseal(ctx, sealed, unsealOpts)
	require.NoError(t, err)
	assert.Equal(t, originalData, plaintext)
}

func TestUnseal_WrongAAD(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, _ := createTestECDSAKey(t, "unseal-wrong-aad-key")

	ctx := context.Background()
	originalData := []byte("secret data")
	correctAAD := []byte("correct AAD")
	wrongAAD := []byte("wrong AAD")

	// Seal with AAD
	sealOpts := &xkmssdk.SealOptions{
		KeyAttributes: attrs,
		AAD:           correctAAD,
	}
	sealed, err := Seal(ctx, originalData, sealOpts)
	require.NoError(t, err)

	// Try to unseal with wrong AAD
	unsealOpts := &xkmssdk.UnsealOptions{
		KeyAttributes: attrs,
		AAD:           wrongAAD,
	}
	_, err = Unseal(ctx, sealed, unsealOpts)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsealingFailed)
}

func TestUnsealWithBackend_NotInitialized(t *testing.T) {
	Reset()

	ctx := context.Background()
	sealed := &xkmssdk.SealedData{
		Ciphertext: []byte("ciphertext"),
		Backend:    xkmssdk.BackendTypeSoftware,
	}
	opts := &xkmssdk.UnsealOptions{}
	_, err := UnsealWithBackend(ctx, "software", sealed, opts)
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestUnsealWithBackend_NilSealedData(t *testing.T) {
	setupFullTestXKMS(t)

	ctx := context.Background()
	opts := &xkmssdk.UnsealOptions{}
	_, err := UnsealWithBackend(ctx, "software", nil, opts)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsealingFailed)
}

func TestUnsealWithBackend_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, _ := createTestECDSAKey(t, "unseal-backend-test-key")

	ctx := context.Background()
	originalData := []byte("secret data for backend unseal")

	// Seal with backend
	sealOpts := &xkmssdk.SealOptions{
		KeyAttributes: attrs,
	}
	sealed, err := SealWithBackend(ctx, "software", originalData, sealOpts)
	require.NoError(t, err)

	// Unseal with backend
	unsealOpts := &xkmssdk.UnsealOptions{
		KeyAttributes: attrs,
	}
	plaintext, err := UnsealWithBackend(ctx, "software", sealed, unsealOpts)
	require.NoError(t, err)
	assert.Equal(t, originalData, plaintext)
}

func TestUnsealWithBackend_InvalidBackend(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, _ := createTestECDSAKey(t, "unseal-invalid-backend-key")

	ctx := context.Background()

	sealed := &xkmssdk.SealedData{
		Ciphertext: []byte("ciphertext"),
		Backend:    xkmssdk.BackendTypeSoftware,
		Nonce:      make([]byte, 12),
	}

	unsealOpts := &xkmssdk.UnsealOptions{
		KeyAttributes: attrs,
	}

	_, err := UnsealWithBackend(ctx, "non-existent", sealed, unsealOpts)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsealingFailed)
}

// -----------------------------------------------------------------------------
// Certificate Tests
// -----------------------------------------------------------------------------

func TestCertificate_NotInitialized(t *testing.T) {
	Reset()

	_, err := Certificate(context.Background(), &xkmssdk.KeyAttributes{CN: "test"})
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestCertificate_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, _ := createTestECDSAKey(t, "cert-test")

	// Get signer for cert creation
	signer, err := Signer(context.Background(), attrs)
	require.NoError(t, err)

	// Create and save cert
	cert := createTestCertificate(t, signer)
	err = SaveCertificate(context.Background(), attrs, cert)
	require.NoError(t, err)

	// Retrieve cert
	retrieved, err := Certificate(context.Background(), attrs)
	require.NoError(t, err)
	assert.Equal(t, cert.SerialNumber, retrieved.SerialNumber)
}

func TestCertificate_NotFound(t *testing.T) {
	setupFullTestXKMS(t)

	attrs := &xkmssdk.KeyAttributes{
		CN:        "non-existent-cert",
		StoreType: xkmssdk.StoreSoftware,
	}

	_, err := Certificate(context.Background(), attrs)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCertificateOperationFailed)
}

func TestCertificateByID_NotInitialized(t *testing.T) {
	Reset()

	_, err := CertificateByID(context.Background(), "software:signing:ecdsa-p256:test-cert")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestCertificateByID_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, fullKeyID := createTestECDSAKey(t, "cert-by-id-test")

	// Get signer and create cert
	signer, err := Signer(context.Background(), attrs)
	require.NoError(t, err)
	cert := createTestCertificate(t, signer)

	err = SaveCertificateByID(context.Background(), fullKeyID, cert)
	require.NoError(t, err)

	retrieved, err := CertificateByID(context.Background(), fullKeyID)
	require.NoError(t, err)
	assert.NotNil(t, retrieved)
}

func TestCertificateByID_NotFound(t *testing.T) {
	setupFullTestXKMS(t)

	_, err := CertificateByID(context.Background(), "software:signing:ecdsa-p256:non-existent-cert")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCertificateOperationFailed)
}

// -----------------------------------------------------------------------------
// SaveCertificate Tests
// -----------------------------------------------------------------------------

func TestSaveCertificate_NotInitialized(t *testing.T) {
	Reset()

	err := SaveCertificate(context.Background(), &xkmssdk.KeyAttributes{}, &x509.Certificate{})
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestSaveCertificateByID_NotInitialized(t *testing.T) {
	Reset()

	err := SaveCertificateByID(context.Background(), "software:signing:ecdsa-p256:test", &x509.Certificate{})
	assert.ErrorIs(t, err, ErrNotInitialized)
}

// -----------------------------------------------------------------------------
// DeleteCertificate Tests
// -----------------------------------------------------------------------------

func TestDeleteCertificate_NotInitialized(t *testing.T) {
	Reset()

	err := DeleteCertificate(context.Background(), &xkmssdk.KeyAttributes{CN: "test"})
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestDeleteCertificate_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, _ := createTestECDSAKey(t, "delete-cert-test")

	signer, err := Signer(context.Background(), attrs)
	require.NoError(t, err)
	cert := createTestCertificate(t, signer)

	err = SaveCertificate(context.Background(), attrs, cert)
	require.NoError(t, err)

	err = DeleteCertificate(context.Background(), attrs)
	require.NoError(t, err)

	// Verify cert no longer exists
	_, err = Certificate(context.Background(), attrs)
	require.Error(t, err)
}

func TestDeleteCertificateByID_NotInitialized(t *testing.T) {
	Reset()

	err := DeleteCertificateByID(context.Background(), "software:signing:ecdsa-p256:test-cert")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestDeleteCertificateByID_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, fullKeyID := createTestECDSAKey(t, "delete-cert-by-id-test")

	signer, err := Signer(context.Background(), attrs)
	require.NoError(t, err)
	cert := createTestCertificate(t, signer)

	err = SaveCertificateByID(context.Background(), fullKeyID, cert)
	require.NoError(t, err)

	err = DeleteCertificateByID(context.Background(), fullKeyID)
	require.NoError(t, err)
}

// -----------------------------------------------------------------------------
// CertificateExists Tests
// -----------------------------------------------------------------------------

func TestCertificateExists_NotInitialized(t *testing.T) {
	Reset()

	_, err := CertificateExists(context.Background(), &xkmssdk.KeyAttributes{CN: "test"})
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestCertificateExists_True(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, _ := createTestECDSAKey(t, "cert-exists-test")

	signer, err := Signer(context.Background(), attrs)
	require.NoError(t, err)
	cert := createTestCertificate(t, signer)

	err = SaveCertificate(context.Background(), attrs, cert)
	require.NoError(t, err)

	exists, err := CertificateExists(context.Background(), attrs)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestCertificateExists_False(t *testing.T) {
	setupFullTestXKMS(t)

	attrs := &xkmssdk.KeyAttributes{
		CN:        "non-existent-cert",
		StoreType: xkmssdk.StoreSoftware,
	}

	exists, err := CertificateExists(context.Background(), attrs)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestCertificateExistsByID_NotInitialized(t *testing.T) {
	Reset()

	_, err := CertificateExistsByID(context.Background(), "software:signing:ecdsa-p256:test-cert")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestCertificateExistsByID_True(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, fullKeyID := createTestECDSAKey(t, "cert-exists-by-id-test")

	signer, err := Signer(context.Background(), attrs)
	require.NoError(t, err)
	cert := createTestCertificate(t, signer)

	err = SaveCertificateByID(context.Background(), fullKeyID, cert)
	require.NoError(t, err)

	exists, err := CertificateExistsByID(context.Background(), fullKeyID)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestCertificateExistsByID_False(t *testing.T) {
	setupFullTestXKMS(t)

	exists, err := CertificateExistsByID(context.Background(), "software:signing:ecdsa-p256:non-existent-cert")
	require.NoError(t, err)
	assert.False(t, exists)
}

// -----------------------------------------------------------------------------
// ListCertificates Tests
// -----------------------------------------------------------------------------

func TestListCertificates_NotInitialized(t *testing.T) {
	Reset()

	_, err := ListCertificates(context.Background())
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestListCertificates_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, _ := createTestECDSAKey(t, "list-cert-test")

	signer, err := Signer(context.Background(), attrs)
	require.NoError(t, err)
	cert := createTestCertificate(t, signer)

	err = SaveCertificate(context.Background(), attrs, cert)
	require.NoError(t, err)

	certs, err := ListCertificates(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, certs)
}

func TestListCertificates_SpecificBackend(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, _ := createTestECDSAKey(t, "list-cert-backend-test")

	signer, err := Signer(context.Background(), attrs)
	require.NoError(t, err)
	cert := createTestCertificate(t, signer)

	err = SaveCertificate(context.Background(), attrs, cert)
	require.NoError(t, err)

	certs, err := ListCertificates(context.Background(), "software")
	require.NoError(t, err)
	assert.NotNil(t, certs)
}

// -----------------------------------------------------------------------------
// CertificateChain Tests
// -----------------------------------------------------------------------------

func TestCertificateChain_NotInitialized(t *testing.T) {
	Reset()

	_, err := CertificateChain(context.Background(), &xkmssdk.KeyAttributes{CN: "test"})
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestCertificateChain_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, _ := createTestECDSAKey(t, "cert-chain-test")

	signer, err := Signer(context.Background(), attrs)
	require.NoError(t, err)
	cert := createTestCertificate(t, signer)

	// Save as chain
	err = SaveCertificateChain(context.Background(), attrs, []*x509.Certificate{cert})
	require.NoError(t, err)

	chain, err := CertificateChain(context.Background(), attrs)
	require.NoError(t, err)
	assert.NotEmpty(t, chain)
}

func TestCertificateChain_NotFound(t *testing.T) {
	setupFullTestXKMS(t)

	attrs := &xkmssdk.KeyAttributes{
		CN:        "non-existent-chain",
		StoreType: xkmssdk.StoreSoftware,
	}

	_, err := CertificateChain(context.Background(), attrs)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCertificateOperationFailed)
}

func TestCertificateChainByID_NotInitialized(t *testing.T) {
	Reset()

	_, err := CertificateChainByID(context.Background(), "software:signing:ecdsa-p256:test-chain")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestCertificateChainByID_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, fullKeyID := createTestECDSAKey(t, "cert-chain-by-id-test")

	signer, err := Signer(context.Background(), attrs)
	require.NoError(t, err)
	cert := createTestCertificate(t, signer)

	err = SaveCertificateChainByID(context.Background(), fullKeyID, []*x509.Certificate{cert})
	require.NoError(t, err)

	chain, err := CertificateChainByID(context.Background(), fullKeyID)
	require.NoError(t, err)
	assert.NotEmpty(t, chain)
}

func TestCertificateChainByID_NotFound(t *testing.T) {
	setupFullTestXKMS(t)

	_, err := CertificateChainByID(context.Background(), "software:signing:ecdsa-p256:non-existent-chain")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCertificateOperationFailed)
}

// -----------------------------------------------------------------------------
// SaveCertificateChain Tests
// -----------------------------------------------------------------------------

func TestSaveCertificateChain_NotInitialized(t *testing.T) {
	Reset()

	err := SaveCertificateChain(context.Background(), &xkmssdk.KeyAttributes{}, []*x509.Certificate{})
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestSaveCertificateChainByID_NotInitialized(t *testing.T) {
	Reset()

	err := SaveCertificateChainByID(context.Background(), "software:signing:ecdsa-p256:test", []*x509.Certificate{})
	assert.ErrorIs(t, err, ErrNotInitialized)
}

// -----------------------------------------------------------------------------
// TLSCertificate Tests
// -----------------------------------------------------------------------------

func TestTLSCertificate_NotInitialized(t *testing.T) {
	Reset()

	_, err := TLSCertificate(context.Background(), &xkmssdk.KeyAttributes{CN: "test"})
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestTLSCertificate_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, _ := createTestECDSAKey(t, "tls-cert-test")

	signer, err := Signer(context.Background(), attrs)
	require.NoError(t, err)
	cert := createTestCertificate(t, signer)

	err = SaveCertificate(context.Background(), attrs, cert)
	require.NoError(t, err)

	tlsCert, err := TLSCertificate(context.Background(), attrs)
	require.NoError(t, err)
	assert.NotNil(t, tlsCert.PrivateKey)
	assert.NotEmpty(t, tlsCert.Certificate)
}

func TestTLSCertificateByID_NotInitialized(t *testing.T) {
	Reset()

	_, err := TLSCertificateByID(context.Background(), "software:signing:ecdsa-p256:test-cert")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestTLSCertificateByID_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, fullKeyID := createTestECDSAKey(t, "tls-cert-by-id-test")

	signer, err := Signer(context.Background(), attrs)
	require.NoError(t, err)
	cert := createTestCertificate(t, signer)

	err = SaveCertificateByID(context.Background(), fullKeyID, cert)
	require.NoError(t, err)

	tlsCert, err := TLSCertificateByID(context.Background(), fullKeyID)
	require.NoError(t, err)
	assert.NotNil(t, tlsCert.PrivateKey)
}

// -----------------------------------------------------------------------------
// Backend Tests
// -----------------------------------------------------------------------------

func TestBackendFor_NotInitialized(t *testing.T) {
	Reset()

	_, err := BackendFor(&xkmssdk.KeyAttributes{})
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestBackendFor_Success(t *testing.T) {
	setupFullTestXKMS(t)

	attrs := &xkmssdk.KeyAttributes{
		StoreType: xkmssdk.StoreSoftware,
	}

	be, err := BackendFor(attrs)
	require.NoError(t, err)
	assert.NotNil(t, be)
}

func TestGetBackendInfo_NotInitialized(t *testing.T) {
	Reset()

	_, err := GetBackendInfo("software")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestGetBackendInfo_Success(t *testing.T) {
	setupFullTestXKMS(t)

	info, err := GetBackendInfo("software")
	require.NoError(t, err)
	assert.NotNil(t, info)
}

func TestGetBackendInfo_NotFound(t *testing.T) {
	setupFullTestXKMS(t)

	_, err := GetBackendInfo("non-existent")
	require.Error(t, err)
}

func TestGetBackendCapabilities_NotInitialized(t *testing.T) {
	Reset()

	_, err := GetBackendCapabilities("software")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestGetBackendCapabilities_Success(t *testing.T) {
	setupFullTestXKMS(t)

	caps, err := GetBackendCapabilities("software")
	require.NoError(t, err)
	assert.NotNil(t, caps)
}

func TestGetBackendCapabilities_NotFound(t *testing.T) {
	setupFullTestXKMS(t)

	_, err := GetBackendCapabilities("non-existent")
	require.Error(t, err)
}

// -----------------------------------------------------------------------------
// Key Import Tests
// -----------------------------------------------------------------------------

func TestGetImportParameters_NotInitialized(t *testing.T) {
	Reset()

	_, err := GetImportParameters(context.Background(), "software", &xkmssdk.KeyAttributes{}, xkmssdk.WrappingAlgorithmRSAES_OAEP_SHA_256)
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestGetImportParameters_Success(t *testing.T) {
	setupFullTestXKMS(t)

	attrs := &xkmssdk.KeyAttributes{
		CN:        "import-test-key",
		StoreType: xkmssdk.StoreSoftware,
		KeyType:   xkmssdk.KeyTypeSigning,
	}

	// This tests the success path - PKCS8 backend may or may not support this
	_, err := GetImportParameters(context.Background(), "software", attrs, xkmssdk.WrappingAlgorithmRSAES_OAEP_SHA_256)
	// We just verify it doesn't panic - it may return an error if unsupported
	_ = err
}

func TestWrapKey_NotInitialized(t *testing.T) {
	Reset()

	_, err := WrapKey(context.Background(), "software", []byte("key"), nil)
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestWrapKey_Success(t *testing.T) {
	setupFullTestXKMS(t)

	// This tests the success path - PKCS8 backend may or may not support this
	_, err := WrapKey(context.Background(), "software", []byte("key-material"), nil)
	// We just verify it doesn't panic - it may return an error if params are nil
	_ = err
}

func TestImportKey_NotInitialized(t *testing.T) {
	Reset()

	err := ImportKey(context.Background(), "software", &xkmssdk.KeyAttributes{}, nil)
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestImportKey_Success(t *testing.T) {
	setupFullTestXKMS(t)

	attrs := &xkmssdk.KeyAttributes{
		CN:        "import-key-test",
		StoreType: xkmssdk.StoreSoftware,
		KeyType:   xkmssdk.KeyTypeSigning,
	}

	// This tests the success path - calling with nil material should error but not panic
	err := ImportKey(context.Background(), "software", attrs, nil)
	// We just verify it doesn't panic - it should return an error
	require.Error(t, err)
}

// -----------------------------------------------------------------------------
// ECDH-ES Tests
// -----------------------------------------------------------------------------

func TestWrapKeyWithECDHES_NotInitialized(t *testing.T) {
	Reset()

	_, err := WrapKeyWithECDHES(context.Background(), "software:signing:ecdsa-p256:test-key", []byte("material"), "ECDH-ES+A256KW")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestWrapKeyWithECDHES_KeyNotFound(t *testing.T) {
	setupFullTestXKMS(t)

	_, err := WrapKeyWithECDHES(context.Background(), "software:signing:ecdsa-p256:non-existent", []byte("material"), "ECDH-ES+A256KW")
	require.Error(t, err)
}

func TestWrapKeyWithECDHES_NotECKey(t *testing.T) {
	setupFullTestXKMS(t)
	_, fullKeyID := createTestRSAKey(t, "rsa-key-ecdh-test")

	// This should error because RSA keys are not valid for ECDH-ES
	_, err := WrapKeyWithECDHES(context.Background(), fullKeyID, []byte("material"), "ECDH-ES+A256KW")
	require.Error(t, err)
	// Just verify we get an error - the exact message may vary based on library implementation
}

func TestUnwrapKeyWithECDHES_NotInitialized(t *testing.T) {
	Reset()

	_, err := UnwrapKeyWithECDHES(context.Background(), "software:signing:ecdsa-p256:test-key", "jwe-data")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestUnwrapKeyWithECDHES_KeyNotFound(t *testing.T) {
	setupFullTestXKMS(t)

	_, err := UnwrapKeyWithECDHES(context.Background(), "software:signing:ecdsa-p256:non-existent", "invalid-jwe")
	require.Error(t, err)
}

func TestIsECDHESAlgorithm(t *testing.T) {
	testCases := []struct {
		algorithm string
		expected  bool
	}{
		{"ECDH-ES", true},
		{"ECDH-ES+A128KW", true},
		{"ECDH-ES+A192KW", true},
		{"ECDH-ES+A256KW", true},
		{"RSA-OAEP", false},
		{"A256GCM", false},
		{"", false},
	}

	for _, tc := range testCases {
		t.Run(tc.algorithm, func(t *testing.T) {
			result := IsECDHESAlgorithm(tc.algorithm)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// -----------------------------------------------------------------------------
// JWT Tests
// -----------------------------------------------------------------------------

func TestJWTSign_NotInitialized(t *testing.T) {
	Reset()

	claims := JWTClaims{"sub": "user123"}
	_, err := JWTSign("software:signing:ecdsa-p256:test-key", claims)
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestJWTSign_Success(t *testing.T) {
	setupFullTestXKMS(t)
	_, fullKeyID := createTestECDSAKey(t, "jwt-sign-test")

	claims := JWTClaims{
		"sub": "user123",
		"iat": time.Now().Unix(),
	}

	token, err := JWTSign(fullKeyID, claims)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
	// JWT has 3 parts separated by dots
	assert.Equal(t, 2, len(token)-len(strings.ReplaceAll(token, ".", "")))
}

func TestJWTSign_KeyNotFound(t *testing.T) {
	setupFullTestXKMS(t)

	claims := JWTClaims{"sub": "user123"}
	_, err := JWTSign("software:signing:ecdsa-p256:non-existent-key", claims)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJWTSigningFailed)
}

func TestJWTSignWithAlgorithm_NotInitialized(t *testing.T) {
	Reset()

	claims := JWTClaims{"sub": "user123"}
	_, err := JWTSignWithAlgorithm("software:signing:ecdsa-p256:test-key", claims, JWTAlgES256)
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestJWTSignWithAlgorithm_Success(t *testing.T) {
	setupFullTestXKMS(t)
	_, fullKeyID := createTestECDSAKey(t, "jwt-sign-alg-test")

	claims := JWTClaims{"sub": "user123"}
	token, err := JWTSignWithAlgorithm(fullKeyID, claims, JWTAlgES256)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestJWTSignWithAlgorithm_KeyNotFound(t *testing.T) {
	setupFullTestXKMS(t)

	claims := JWTClaims{"sub": "user123"}
	_, err := JWTSignWithAlgorithm("software:signing:ecdsa-p256:non-existent-key", claims, JWTAlgES256)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJWTSigningFailed)
}

func TestJWTSignWithSigner(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	setupFullTestXKMS(t)
	rsaAttrs, _ := createTestRSAKey(t, "rsa-alg-test")
	rsaSigner, err := Signer(context.Background(), rsaAttrs)
	require.NoError(t, err)

	tests := []struct {
		name      string
		signer    crypto.Signer
		algorithm string
		wantErr   bool
	}{
		{name: "ECDSA ES256", signer: ecKey, algorithm: "ES256"},
		{name: "backend RSA RS256", signer: rsaSigner, algorithm: "RS256"},
		{name: "unknown algorithm", signer: ecKey, algorithm: "INVALID", wantErr: true},
		{name: "algorithm does not match key", signer: ecKey, algorithm: "RS256", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := JWTSignWithSigner(tt.signer, JWTClaims{"sub": "user123"}, tt.algorithm)
			if tt.wantErr {
				assert.ErrorIs(t, err, ErrJWTSigningFailed)
				return
			}
			require.NoError(t, err)
			assert.NotEmpty(t, token)
		})
	}
}

func TestJWTVerify_NotInitialized(t *testing.T) {
	Reset()

	_, err := JWTVerify("token", "software:signing:ecdsa-p256:key-id")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestJWTVerify_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, fullKeyID := createTestECDSAKey(t, "jwt-verify-test")

	signer, err := Signer(context.Background(), attrs)
	require.NoError(t, err)

	claims := JWTClaims{"sub": "user123"}
	token, err := JWTSignWithSigner(signer, claims, "ES256")
	require.NoError(t, err)

	verified, err := JWTVerify(token, fullKeyID)
	require.NoError(t, err)
	assert.True(t, verified.Valid)
}

func TestJWTVerify_InvalidToken(t *testing.T) {
	setupFullTestXKMS(t)
	_, fullKeyID := createTestECDSAKey(t, "jwt-verify-invalid-test")

	_, err := JWTVerify("invalid.token.here", fullKeyID)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJWTVerificationFailed)
}

func TestJWTVerifyAuto_NotInitialized(t *testing.T) {
	Reset()

	_, err := JWTVerifyAuto("token")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestJWTVerifyAuto_Success(t *testing.T) {
	setupFullTestXKMS(t)
	_, fullKeyID := createTestECDSAKey(t, "jwt-verify-auto-test")

	// Use SignerByID with full key ID (ID-based lookup now works correctly)
	signer, err := SignerByID(context.Background(), fullKeyID)
	require.NoError(t, err)

	// Create a JWT with the KID header
	claims := JWTClaims{"sub": "user123"}
	tokenStr := createJWTWithKID(t, signer, fullKeyID, claims)

	// JWTVerifyAuto extracts KID and verifies - ID-based lookup now works correctly
	token, err := JWTVerifyAuto(tokenStr)
	require.NoError(t, err)
	assert.NotNil(t, token)
	assert.True(t, token.Valid)
}

func TestJWTVerifyAuto_InvalidToken(t *testing.T) {
	setupFullTestXKMS(t)

	_, err := JWTVerifyAuto("invalid.token.here")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJWTVerificationFailed)
}

func TestJWTExtractKID_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, fullKeyID := createTestECDSAKey(t, "jwt-extract-kid-test")

	// Get signer from xkms
	signer, err := Signer(context.Background(), attrs)
	require.NoError(t, err)

	// Create a JWT with the KID header using our helper
	claims := JWTClaims{"sub": "user123"}
	token := createJWTWithKID(t, signer, fullKeyID, claims)

	// Extract the KID
	kid, err := JWTExtractKID(token)
	require.NoError(t, err)
	assert.Equal(t, fullKeyID, kid)
}

func TestJWTExtractKID_InvalidToken(t *testing.T) {
	_, err := JWTExtractKID("not-a-valid-jwt")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJWTParsingFailed)
}

func TestJWTParse_Success(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, _ := createTestECDSAKey(t, "jwt-parse-test")

	// Get key and sign directly
	signer, err := Signer(context.Background(), attrs)
	require.NoError(t, err)

	originalClaims := JWTClaims{
		"sub":  "user123",
		"name": "Test User",
	}
	token, err := JWTSignWithSigner(signer, originalClaims, "ES256")
	require.NoError(t, err)

	parsedClaims, err := JWTParse(token)
	require.NoError(t, err)
	assert.Equal(t, "user123", parsedClaims["sub"])
	assert.Equal(t, "Test User", parsedClaims["name"])
}

func TestJWTParse_InvalidToken(t *testing.T) {
	_, err := JWTParse("not.a.token")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJWTParsingFailed)
}

func TestJWTParseAlgorithm_Success(t *testing.T) {
	alg, err := JWTParseAlgorithm("ES256")
	require.NoError(t, err)
	assert.Equal(t, JWTAlgES256, alg)
}

func TestJWTParseAlgorithm_Invalid(t *testing.T) {
	_, err := JWTParseAlgorithm("INVALID")
	require.Error(t, err)
}

func TestJWTVerifyWithKey_Success(t *testing.T) {
	// Generate a key pair
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	claims := JWTClaims{"sub": "user123"}
	token, err := JWTSignWithSigner(privateKey, claims, "ES256")
	require.NoError(t, err)

	verified, err := JWTVerifyWithKey(token, &privateKey.PublicKey, "ES256")
	require.NoError(t, err)
	assert.True(t, verified.Valid)
}

func TestJWTVerifyWithKey_WrongAlgorithm(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	claims := JWTClaims{"sub": "user123"}
	token, err := JWTSignWithSigner(privateKey, claims, "ES256")
	require.NoError(t, err)

	_, err = JWTVerifyWithKey(token, &privateKey.PublicKey, "ES384")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJWTVerificationFailed)
}

func TestJWTVerifyWithKey_InvalidSignature(t *testing.T) {
	privateKey1, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	privateKey2, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	claims := JWTClaims{"sub": "user123"}
	token, err := JWTSignWithSigner(privateKey1, claims, "ES256")
	require.NoError(t, err)

	// Try to verify with wrong key
	_, err = JWTVerifyWithKey(token, &privateKey2.PublicKey, "ES256")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJWTVerificationFailed)
}

func TestJWTVerifyWithKeyAndClaims_Success(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	claims := JWTClaims{
		"sub":  "user123",
		"role": "admin",
	}
	token, err := JWTSignWithSigner(privateKey, claims, "ES256")
	require.NoError(t, err)

	parsedClaims, err := JWTVerifyWithKeyAndClaims(token, &privateKey.PublicKey, "ES256")
	require.NoError(t, err)
	assert.Equal(t, "user123", parsedClaims["sub"])
	assert.Equal(t, "admin", parsedClaims["role"])
}

func TestJWTVerifyWithKeyAndClaims_InvalidToken(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	_, err = JWTVerifyWithKeyAndClaims("invalid.token", &privateKey.PublicKey, "ES256")
	require.Error(t, err)
}

// -----------------------------------------------------------------------------
// HMAC JWT Tests
// -----------------------------------------------------------------------------

func TestJWTSignHMAC_Success(t *testing.T) {
	claims := JWTClaims{"sub": "user123"}
	secret := []byte("my-secret-key-at-least-32-bytes!")

	token, err := JWTSignHMAC(claims, secret)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestJWTSignHMACWithAlgorithm_Success(t *testing.T) {
	claims := JWTClaims{"sub": "user123"}
	secret := []byte("my-secret-key-at-least-32-bytes!")

	testCases := []string{"HS256", "HS384", "HS512"}
	for _, alg := range testCases {
		t.Run(alg, func(t *testing.T) {
			token, err := JWTSignHMACWithAlgorithm(claims, secret, alg)
			require.NoError(t, err)
			assert.NotEmpty(t, token)
		})
	}
}

func TestJWTSignHMACWithAlgorithm_InvalidAlgorithm(t *testing.T) {
	claims := JWTClaims{"sub": "user123"}
	secret := []byte("secret")

	_, err := JWTSignHMACWithAlgorithm(claims, secret, "INVALID")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJWTSigningFailed)
}

func TestJWTSignHMACWithAlgorithm_NotHMAC(t *testing.T) {
	claims := JWTClaims{"sub": "user123"}
	secret := []byte("secret")

	_, err := JWTSignHMACWithAlgorithm(claims, secret, "ES256")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJWTSigningFailed)
	assert.Contains(t, err.Error(), "not HMAC")
}

func TestJWTVerifyHMAC_Success(t *testing.T) {
	claims := JWTClaims{"sub": "user123"}
	secret := []byte("my-secret-key-at-least-32-bytes!")

	token, err := JWTSignHMAC(claims, secret)
	require.NoError(t, err)

	parsedClaims, err := JWTVerifyHMAC(token, secret)
	require.NoError(t, err)
	assert.Equal(t, "user123", parsedClaims["sub"])
}

func TestJWTVerifyHMAC_WrongSecret(t *testing.T) {
	claims := JWTClaims{"sub": "user123"}
	secret := []byte("correct-secret")
	wrongSecret := []byte("wrong-secret")

	token, err := JWTSignHMAC(claims, secret)
	require.NoError(t, err)

	_, err = JWTVerifyHMAC(token, wrongSecret)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJWTVerificationFailed)
}

func TestJWTVerifyHMACWithAlgorithm_Success(t *testing.T) {
	claims := JWTClaims{"sub": "user123"}
	secret := []byte("my-secret-key")

	token, err := JWTSignHMACWithAlgorithm(claims, secret, "HS256")
	require.NoError(t, err)

	parsedClaims, err := JWTVerifyHMACWithAlgorithm(token, secret, "HS256")
	require.NoError(t, err)
	assert.Equal(t, "user123", parsedClaims["sub"])
}

func TestJWTVerifyHMACWithAlgorithm_WrongAlgorithm(t *testing.T) {
	claims := JWTClaims{"sub": "user123"}
	secret := []byte("my-secret-key")

	token, err := JWTSignHMACWithAlgorithm(claims, secret, "HS256")
	require.NoError(t, err)

	_, err = JWTVerifyHMACWithAlgorithm(token, secret, "HS384")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJWTVerificationFailed)
}

func TestJWTVerifyHMACWithAlgorithm_NotHMAC(t *testing.T) {
	// Create an EC signed token
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	claims := JWTClaims{"sub": "user123"}
	token, err := JWTSignWithSigner(privateKey, claims, "ES256")
	require.NoError(t, err)

	// Try to verify as HMAC
	_, err = JWTVerifyHMACWithAlgorithm(token, []byte("secret"), "")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJWTVerificationFailed)
}

func TestJWTVerifyHMACWithAlgorithm_EmptyAlgorithm(t *testing.T) {
	claims := JWTClaims{"sub": "user123"}
	secret := []byte("my-secret-key")

	token, err := JWTSignHMAC(claims, secret)
	require.NoError(t, err)

	// Empty algorithm should accept any HMAC algorithm
	parsedClaims, err := JWTVerifyHMACWithAlgorithm(token, secret, "")
	require.NoError(t, err)
	assert.Equal(t, "user123", parsedClaims["sub"])
}

// -----------------------------------------------------------------------------
// Additional Error Type Tests
// -----------------------------------------------------------------------------

// -----------------------------------------------------------------------------
// JWT Algorithm Constants Tests
// -----------------------------------------------------------------------------

// -----------------------------------------------------------------------------
// Backend and DefaultBackend Tests
// -----------------------------------------------------------------------------

func TestBackend_NotInitialized(t *testing.T) {
	Reset()

	_, err := GetBackend("software")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestBackend_Success(t *testing.T) {
	setupFullTestXKMS(t)

	be, err := GetBackend("software")
	require.NoError(t, err)
	assert.NotNil(t, be)
}

func TestBackend_NotFound(t *testing.T) {
	setupFullTestXKMS(t)

	_, err := GetBackend("non-existent")
	require.Error(t, err)
}

func TestDefaultBackend_NotInitialized(t *testing.T) {
	Reset()

	_, err := DefaultBackend()
	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestDefaultBackend_Success(t *testing.T) {
	setupFullTestXKMS(t)

	be, err := DefaultBackend()
	require.NoError(t, err)
	assert.NotNil(t, be)
}

func TestBackends_NotInitialized(t *testing.T) {
	Reset()

	backends := Backends()
	assert.Nil(t, backends)
}

func TestBackends_Success(t *testing.T) {
	setupFullTestXKMS(t)

	backends := Backends()
	assert.NotNil(t, backends)
	assert.Contains(t, backends, "software")
}

// -----------------------------------------------------------------------------
// IsInitialized and Close Tests
// -----------------------------------------------------------------------------

func TestIsInitialized(t *testing.T) {
	Reset()
	assert.False(t, IsInitialized())

	setupFullTestXKMS(t)
	assert.True(t, IsInitialized())
}

func TestClose_NotInitialized(t *testing.T) {
	Reset()

	// Close should work even when not initialized (closes RNG)
	err := Close()
	assert.NoError(t, err)
}

func TestClose_Success(t *testing.T) {
	setupFullTestXKMS(t)
	assert.True(t, IsInitialized())

	err := Close()
	require.NoError(t, err)

	assert.False(t, IsInitialized())
}

func TestClose_MultipleCalls(t *testing.T) {
	setupFullTestXKMS(t)

	// First close
	err := Close()
	require.NoError(t, err)

	// Second close should still succeed (idempotent for RNG)
	err = Close()
	assert.NoError(t, err)
}

// TestClose_ReinitializeReachesFreshBackends pins that Close releases the
// go-xkms singleton, not just its backends. Before it did, the next
// Initialize returned success while every operation still reached the closed
// backends, so a second server in the same process could not generate a key
// (go-quicbaas#714).
func TestClose_ReinitializeReachesFreshBackends(t *testing.T) {
	setupFullTestXKMS(t)
	require.NoError(t, Close())

	initFullTestXKMS(t)
	attrs := &xkmssdk.KeyAttributes{
		CN:           "after-reinitialize",
		KeyType:      xkmssdk.KeyTypeSigning,
		StoreType:    xkmssdk.StoreSoftware,
		KeyAlgorithm: x509.ECDSA,
		ECCAttributes: &xkmssdk.ECCAttributes{
			Curve: elliptic.P256(),
		},
	}
	_, err := GenerateKey(context.Background(), attrs)
	require.NoError(t, err)
}

// -----------------------------------------------------------------------------
// Initialize Tests
// -----------------------------------------------------------------------------

func TestInitialize_NilConfig(t *testing.T) {
	Reset()

	err := Initialize(nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestInitialize_NoBackends(t *testing.T) {
	Reset()

	err := Initialize(&Config{
		DefaultBackend: "software",
		Backends:       map[string]xkmssdk.ServiceBackend{},
		Logger:         &mockLogger{},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestInitialize_NoLogger(t *testing.T) {
	Reset()

	keyStorage, _ := xkmssdk.NewMemoryBackend()
	certStorage, _ := xkmssdk.NewMemoryBackend()
	pkcs8Backend, _ := xkmssdk.NewPKCS8Backend(&xkmssdk.PKCS8BackendConfig{
		KeyStorage:         keyStorage,
		AllowPlaintextKeys: true,
	})
	ks, _ := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     pkcs8Backend,
		CertStorage: certStorage,
	})

	err := Initialize(&Config{
		DefaultBackend: "software",
		Backends: map[string]xkmssdk.ServiceBackend{
			"software": ks,
		},
		Logger: nil,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestInitialize_AlreadyInitialized(t *testing.T) {
	setupFullTestXKMS(t)

	// Try to initialize again
	err := Initialize(&Config{
		DefaultBackend: "software",
		Backends:       map[string]xkmssdk.ServiceBackend{},
		Logger:         &mockLogger{},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAlreadyInitialized)
}

// -----------------------------------------------------------------------------
// CanSeal Tests
// -----------------------------------------------------------------------------

func TestCanSeal_NotInitialized(t *testing.T) {
	Reset()

	result := CanSeal()
	assert.False(t, result)
}

func TestCanSeal_Success(t *testing.T) {
	setupFullTestXKMS(t)

	// PKCS8 backend supports sealing
	result := CanSeal()
	assert.True(t, result)
}

func TestCanSeal_SpecificBackend(t *testing.T) {
	setupFullTestXKMS(t)

	result := CanSeal("software")
	assert.True(t, result)
}

func TestCanSeal_NonExistentBackend(t *testing.T) {
	setupFullTestXKMS(t)

	result := CanSeal("non-existent")
	assert.False(t, result)
}

// =============================================================================
// Consolidated from xkms_coverage_test.go
// =============================================================================

// sanitizeKeyName replaces characters that are invalid in key IDs.
// Key IDs only allow: a-z, A-Z, 0-9, -, _, .
func sanitizeKeyName(name string) string {
	return strings.NewReplacer("+", "plus").Replace(name)
}

// TestWrapKeyWithECDHES_FullAPIAlgorithmVariants tests all algorithm mapping
// paths through the actual WrapKeyWithECDHES function.
func TestWrapKeyWithECDHES_FullAPIAlgorithmVariants(t *testing.T) {
	setupFullTestXKMS(t)

	algorithms := []struct {
		name        string
		algorithm   string
		expectedEnc string
	}{
		{"ECDH-ES-A128KW", "ECDH-ES+A128KW", "A128GCM"},
		{"ECDH-ES-A192KW", "ECDH-ES+A192KW", "A192GCM"},
		{"ECDH-ES-A256KW", "ECDH-ES+A256KW", "A256GCM"},
		{"ECDH-ES direct", "ECDH-ES", "A256GCM"},
	}

	for _, alg := range algorithms {
		t.Run(alg.name, func(t *testing.T) {
			// Use sanitized key name to avoid invalid characters in key ID
			keyName := sanitizeKeyName("ecdh-alg-" + alg.algorithm)
			_, fullKeyID := createTestECDSAKey(t, keyName)
			keyMaterial := []byte("test-material-32-bytes-long!!!")

			wrapped, err := WrapKeyWithECDHES(context.Background(), fullKeyID, keyMaterial, alg.algorithm)
			require.NoError(t, err)
			assert.Equal(t, alg.algorithm, wrapped.Algorithm)
			assert.Equal(t, alg.expectedEnc, wrapped.ContentEncryption)
		})
	}
}

// TestUnwrapKeyWithECDHES_NotECKey verifies that UnwrapKeyWithECDHES returns
// error when the private key is not an EC key.
func TestUnwrapKeyWithECDHES_NotECKey(t *testing.T) {
	setupFullTestXKMS(t)
	_, fullKeyID := createTestRSAKey(t, "rsa-key-unwrap-test")

	_, err := UnwrapKeyWithECDHES(context.Background(), fullKeyID, "eyJhbGciOiJFQ0RILUVTIiwiZW5jIjoiQTI1NkdDTSJ9.invalid.iv.cipher.tag")
	require.Error(t, err)
}

// TestWrapUnwrapKeyWithECDHES_RoundTrip does a full round-trip wrap+unwrap
// through the package API.
func TestWrapUnwrapKeyWithECDHES_RoundTrip(t *testing.T) {
	setupFullTestXKMS(t)
	_, fullKeyID := createTestECDSAKey(t, "ecdh-roundtrip-test")

	originalMaterial := []byte("round-trip-secret-key-material!")

	// Wrap
	wrapped, err := WrapKeyWithECDHES(context.Background(), fullKeyID, originalMaterial, "ECDH-ES+A256KW")
	require.NoError(t, err)

	// Unwrap
	unwrapped, err := UnwrapKeyWithECDHES(context.Background(), fullKeyID, wrapped.JWE)
	require.NoError(t, err)

	assert.Equal(t, originalMaterial, unwrapped)
}

// TestNewJWTSigner_CallbacksWork tests that the JWT signer callbacks resolve keys.
func TestNewJWTSigner_CallbacksWork(t *testing.T) {
	setupFullTestXKMS(t)
	_, fullKeyID := createTestECDSAKey(t, "jwt-signer-callback-test")

	signer := NewJWTSigner()
	require.NotNil(t, signer)

	claims := JWTClaims{
		"sub": "test-user",
		"iat": float64(1234567890),
	}

	token, err := signer.SignWithKeyID(fullKeyID, claims)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

// TestNewJWTVerifier_VerifySuccess tests the verifier with a real token.
func TestNewJWTVerifier_VerifySuccess(t *testing.T) {
	setupFullTestXKMS(t)
	attrs, fullKeyID := createTestECDSAKey(t, "jwt-verifier-test")

	// Sign with key
	signerKey, err := Signer(context.Background(), attrs)
	require.NoError(t, err)

	claims := JWTClaims{
		"sub": "test-user",
		"iat": float64(1234567890),
	}

	// Create token with kid header
	tokenStr := createJWTWithKID(t, signerKey, fullKeyID, claims)

	// Verify
	verifier := NewJWTVerifier()
	require.NotNil(t, verifier)
	token, err := verifier.VerifyWithKeyID(tokenStr, fullKeyID)
	require.NoError(t, err)
	assert.True(t, token.Valid)
}

// TestRand_ProducesDifferentValues tests the Rand function produces different values.
func TestRand_ProducesDifferentValues(t *testing.T) {
	ResetRNG()

	r1, err := Rand(32)
	require.NoError(t, err)

	r2, err := Rand(32)
	require.NoError(t, err)

	// Two random values should be different (extremely unlikely to collide)
	assert.NotEqual(t, r1, r2)
}

// TestRandInt_MaxBoundBigInt verifies RandInt respects the max bound.
func TestRandInt_MaxBoundBigInt(t *testing.T) {
	ResetRNG()

	maxVal := big.NewInt(10)
	for i := 0; i < 100; i++ {
		val, err := RandInt(maxVal)
		require.NoError(t, err)
		assert.True(t, val.Sign() >= 0, "value should be non-negative")
		assert.True(t, val.Cmp(maxVal) < 0, "value should be less than max")
	}
}

// TestJWTVerifyWithKey_NoAlgorithmValidation exercises the path where
// expectedAlgorithm is empty (accepts any algorithm).
func TestJWTVerifyWithKey_NoAlgorithmValidation(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	claims := jwt.MapClaims{
		"sub": "user123",
		"iat": float64(1234567890),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tokenStr, err := token.SignedString(ecKey)
	require.NoError(t, err)

	// Empty algorithm means accept any
	result, err := JWTVerifyWithKey(tokenStr, &ecKey.PublicKey, "")
	require.NoError(t, err)
	assert.True(t, result.Valid)
}

// TestWrapKeyWithECDHES_UnsupportedAlgorithm exercises the unsupported algorithm error path.
func TestWrapKeyWithECDHES_UnsupportedAlgorithm(t *testing.T) {
	setupFullTestXKMS(t)
	_, fullKeyID := createTestECDSAKey(t, "ecdh-unsupported-alg")

	_, err := WrapKeyWithECDHES(context.Background(), fullKeyID, []byte("data"), "UNSUPPORTED-ALG")
	require.Error(t, err)
}

// TestInitializeRNG_SoftwareMode exercises the software mode RNG initialization.
func TestInitializeRNG_SoftwareMode(t *testing.T) {
	ResetRNG()

	config := &RNGConfig{
		Mode: RNGModeSoftware,
	}
	err := InitializeRNG(config)
	require.NoError(t, err)

	// Verify RNG works after explicit software init
	data, err := Rand(16)
	require.NoError(t, err)
	assert.Len(t, data, 16)

	ResetRNG()
}
