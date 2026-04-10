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

package encryption

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestEncryptDecryptField_AES128(t *testing.T) {
	tests := []struct {
		name      string
		plaintext []byte
		keySize   int
	}{
		{
			name:      "small text",
			plaintext: []byte("Hello, World!"),
			keySize:   AES128KeySize,
		},
		{
			name:      "empty string",
			plaintext: []byte(""),
			keySize:   AES128KeySize,
		},
		{
			name:      "large text",
			plaintext: bytes.Repeat([]byte("A"), 10000),
			keySize:   AES128KeySize,
		},
		{
			name:      "unicode text",
			plaintext: []byte("Hello 世界! 🔐"),
			keySize:   AES128KeySize,
		},
		{
			name:      "binary data",
			plaintext: []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0xfd},
			keySize:   AES128KeySize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate a random key
			key := make([]byte, tt.keySize)
			_, err := rand.Read(key)
			require.NoError(t, err)

			// Skip empty plaintext test for encryption
			if len(tt.plaintext) == 0 {
				_, err := EncryptField(tt.plaintext, key, AlgorithmAES128)
				assert.Error(t, err)
				var dragonErr *errors.DragonError
				assert.ErrorAs(t, err, &dragonErr)
				assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
				return
			}

			// Encrypt
			ciphertext, err := EncryptField(tt.plaintext, key, AlgorithmAES128)
			require.NoError(t, err)
			assert.NotNil(t, ciphertext)

			// Ciphertext should be longer than plaintext (nonce + tag)
			assert.Greater(t, len(ciphertext), len(tt.plaintext))

			// Ciphertext should be different from plaintext
			assert.NotEqual(t, tt.plaintext, ciphertext)

			// Decrypt
			decrypted, err := DecryptField(ciphertext, key, AlgorithmAES128)
			require.NoError(t, err)
			assert.Equal(t, tt.plaintext, decrypted)
		})
	}
}

func TestEncryptDecryptField_AES256(t *testing.T) {
	tests := []struct {
		name      string
		plaintext []byte
		keySize   int
	}{
		{
			name:      "small text",
			plaintext: []byte("Secret message"),
			keySize:   AES256KeySize,
		},
		{
			name:      "sensitive data",
			plaintext: []byte("password123!@#$%^&*()"),
			keySize:   AES256KeySize,
		},
		{
			name:      "json data",
			plaintext: []byte(`{"username":"admin","password":"secret"}`),
			keySize:   AES256KeySize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate a random key
			key := make([]byte, tt.keySize)
			_, err := rand.Read(key)
			require.NoError(t, err)

			// Encrypt
			ciphertext, err := EncryptField(tt.plaintext, key, AlgorithmAES256)
			require.NoError(t, err)
			assert.NotNil(t, ciphertext)

			// Decrypt
			decrypted, err := DecryptField(ciphertext, key, AlgorithmAES256)
			require.NoError(t, err)
			assert.Equal(t, tt.plaintext, decrypted)
		})
	}
}

func TestEncryptField_InvalidKey(t *testing.T) {
	tests := []struct {
		name      string
		keySize   int
		algorithm string
		wantErr   bool
	}{
		{
			name:      "AES128 with wrong key size (too small)",
			keySize:   8,
			algorithm: AlgorithmAES128,
			wantErr:   true,
		},
		{
			name:      "AES128 with larger key (uses first 16 bytes)",
			keySize:   32,
			algorithm: AlgorithmAES128,
			wantErr:   false, // Now acceptable, will use first 16 bytes
		},
		{
			name:      "AES256 with wrong key size (too small)",
			keySize:   16,
			algorithm: AlgorithmAES256,
			wantErr:   true,
		},
		{
			name:      "AES256 with larger key (uses first 32 bytes)",
			keySize:   64,
			algorithm: AlgorithmAES256,
			wantErr:   false, // Now acceptable, will use first 32 bytes
		},
		{
			name:      "AES128 with correct key size",
			keySize:   16,
			algorithm: AlgorithmAES128,
			wantErr:   false,
		},
		{
			name:      "AES256 with correct key size",
			keySize:   32,
			algorithm: AlgorithmAES256,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := make([]byte, tt.keySize)
			plaintext := []byte("test data")

			_, err := EncryptField(plaintext, key, tt.algorithm)
			if tt.wantErr {
				assert.Error(t, err)
				var dragonErr *errors.DragonError
				if assert.ErrorAs(t, err, &dragonErr) {
					assert.Equal(t, errors.ErrEncryption, dragonErr.Code)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEncryptField_InvalidAlgorithm(t *testing.T) {
	key := make([]byte, 16)
	plaintext := []byte("test data")

	_, err := EncryptField(plaintext, key, "invalid-algorithm")
	assert.Error(t, err)

	var dragonErr *errors.DragonError
	assert.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, errors.ErrEncryption, dragonErr.Code)
}

func TestDecryptField_TamperedCiphertext(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	plaintext := []byte("important data")
	ciphertext, err := EncryptField(plaintext, key, AlgorithmAES256)
	require.NoError(t, err)

	// Tamper with the ciphertext
	tamperedCiphertext := make([]byte, len(ciphertext))
	copy(tamperedCiphertext, ciphertext)
	tamperedCiphertext[len(tamperedCiphertext)-1] ^= 0xFF

	// Decryption should fail due to authentication tag mismatch
	_, err = DecryptField(tamperedCiphertext, key, AlgorithmAES256)
	assert.Error(t, err)

	var dragonErr *errors.DragonError
	assert.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, errors.ErrDecryption, dragonErr.Code)
}

func TestDecryptField_WrongKey(t *testing.T) {
	key1 := make([]byte, AES256KeySize)
	key2 := make([]byte, AES256KeySize)
	_, err := rand.Read(key1)
	require.NoError(t, err)
	_, err = rand.Read(key2)
	require.NoError(t, err)

	plaintext := []byte("secret message")
	ciphertext, err := EncryptField(plaintext, key1, AlgorithmAES256)
	require.NoError(t, err)

	// Try to decrypt with wrong key
	_, err = DecryptField(ciphertext, key2, AlgorithmAES256)
	assert.Error(t, err)

	var dragonErr *errors.DragonError
	assert.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, errors.ErrDecryption, dragonErr.Code)
}

func TestDecryptField_TruncatedCiphertext(t *testing.T) {
	key := make([]byte, AES128KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	// Create a ciphertext that's too short
	shortCiphertext := []byte{0x01, 0x02, 0x03}

	_, err = DecryptField(shortCiphertext, key, AlgorithmAES128)
	assert.Error(t, err)

	var dragonErr *errors.DragonError
	assert.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, errors.ErrDecryption, dragonErr.Code)
}

func TestEncryptField_NoncesAreUnique(t *testing.T) {
	key := make([]byte, AES128KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	plaintext := []byte("same plaintext")

	// Encrypt the same plaintext multiple times
	ciphertext1, err := EncryptField(plaintext, key, AlgorithmAES128)
	require.NoError(t, err)

	ciphertext2, err := EncryptField(plaintext, key, AlgorithmAES128)
	require.NoError(t, err)

	// Ciphertexts should be different due to random nonces
	assert.NotEqual(t, ciphertext1, ciphertext2)

	// But both should decrypt to the same plaintext
	decrypted1, err := DecryptField(ciphertext1, key, AlgorithmAES128)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted1)

	decrypted2, err := DecryptField(ciphertext2, key, AlgorithmAES128)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted2)
}

func TestGenerateKey(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		wantSize  int
		wantErr   bool
	}{
		{
			name:      "AES128 key generation",
			algorithm: AlgorithmAES128,
			wantSize:  AES128KeySize,
			wantErr:   false,
		},
		{
			name:      "AES256 key generation",
			algorithm: AlgorithmAES256,
			wantSize:  AES256KeySize,
			wantErr:   false,
		},
		{
			name:      "invalid algorithm",
			algorithm: "invalid",
			wantSize:  0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := GenerateKey(tt.algorithm)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, key)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, key)
				assert.Equal(t, tt.wantSize, len(key))

				// Verify key is not all zeros
				allZeros := true
				for _, b := range key {
					if b != 0 {
						allZeros = false
						break
					}
				}
				assert.False(t, allZeros, "generated key should not be all zeros")
			}
		})
	}
}

func TestGenerateKey_Uniqueness(t *testing.T) {
	// Generate multiple keys and ensure they're unique
	keys := make(map[string]bool)
	for i := 0; i < 100; i++ {
		key, err := GenerateKey(AlgorithmAES256)
		require.NoError(t, err)

		keyStr := string(key)
		assert.False(t, keys[keyStr], "generated duplicate key")
		keys[keyStr] = true
	}
}

func TestEncryptField_MasterKeyDerivation(t *testing.T) {
	// Test that a larger master key works for both AES128 and AES256
	masterKey := make([]byte, AES256KeySize)
	_, err := rand.Read(masterKey)
	require.NoError(t, err)

	plaintext := []byte("test data")

	// Should work with AES128 (uses first 16 bytes)
	ciphertext128, err := EncryptField(plaintext, masterKey, AlgorithmAES128)
	require.NoError(t, err)
	decrypted128, err := DecryptField(ciphertext128, masterKey, AlgorithmAES128)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted128)

	// Should work with AES256 (uses all 32 bytes)
	ciphertext256, err := EncryptField(plaintext, masterKey, AlgorithmAES256)
	require.NoError(t, err)
	decrypted256, err := DecryptField(ciphertext256, masterKey, AlgorithmAES256)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted256)

	// Ciphertexts should be different (different keys used)
	assert.NotEqual(t, ciphertext128, ciphertext256)
}

// Test validateKeySize function (not exported but can test via error paths).
func TestValidateKeySize(t *testing.T) {
	tests := []struct {
		name      string
		keySize   int
		algorithm string
		wantErr   bool
	}{
		{
			name:      "AES128 correct size",
			keySize:   AES128KeySize,
			algorithm: AlgorithmAES128,
			wantErr:   false,
		},
		{
			name:      "AES256 correct size",
			keySize:   AES256KeySize,
			algorithm: AlgorithmAES256,
			wantErr:   false,
		},
		{
			name:      "AES128 wrong size",
			keySize:   24,
			algorithm: AlgorithmAES128,
			wantErr:   true,
		},
		{
			name:      "AES256 wrong size",
			keySize:   16,
			algorithm: AlgorithmAES256,
			wantErr:   true,
		},
		{
			name:      "invalid algorithm",
			keySize:   16,
			algorithm: "invalid",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := make([]byte, tt.keySize)
			err := validateKeySize(key, tt.algorithm)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Test DeriveKeyFromPassword derives keys of correct size using Argon2id.
func TestDeriveKeyFromPassword(t *testing.T) {
	tests := []struct {
		name        string
		password    string
		algorithm   string
		expectedLen int
	}{
		{
			name:        "AES128",
			password:    "testpassword",
			algorithm:   AlgorithmAES128,
			expectedLen: AES128KeySize,
		},
		{
			name:        "AES256",
			password:    "strongpassword123",
			algorithm:   AlgorithmAES256,
			expectedLen: AES256KeySize,
		},
		{
			name:        "UnknownAlgorithmDefaultsTo32",
			password:    "somepassword",
			algorithm:   "unknown-algo",
			expectedLen: AES256KeySize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := DeriveKeyFromPassword(tt.password, tt.algorithm)
			require.NoError(t, err)
			assert.Len(t, key, tt.expectedLen)
		})
	}

	t.Run("DifferentPasswordsDifferentKeys", func(t *testing.T) {
		key1, err := DeriveKeyFromPassword("password1", AlgorithmAES256)
		require.NoError(t, err)
		key2, err := DeriveKeyFromPassword("password2", AlgorithmAES256)
		require.NoError(t, err)
		assert.False(t, bytes.Equal(key1, key2), "different passwords must produce different keys")
	})

	t.Run("SamePasswordDifferentSaltsProduceDifferentKeys", func(t *testing.T) {
		key1, err := DeriveKeyFromPassword("samepassword", AlgorithmAES256)
		require.NoError(t, err)
		key2, err := DeriveKeyFromPassword("samepassword", AlgorithmAES256)
		require.NoError(t, err)
		// Each call generates a new random salt, so keys must differ
		assert.False(t, bytes.Equal(key1, key2), "same password with different salts must produce different keys")
	})
}

// Test DeriveKeyFromPassword with invalid inputs.
func TestDeriveKeyFromPassword_EmptyPassword(t *testing.T) {
	_, err := DeriveKeyFromPassword("", AlgorithmAES256)
	require.Error(t, err)

	var dragonErr *errors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
}

// Test deriveKey function through error conditions.
func TestDeriveKey(t *testing.T) {
	tests := []struct {
		name      string
		keySize   int
		algorithm string
		wantErr   bool
	}{
		{
			name:      "AES128 exact key",
			keySize:   AES128KeySize,
			algorithm: AlgorithmAES128,
			wantErr:   false,
		},
		{
			name:      "AES256 exact key",
			keySize:   AES256KeySize,
			algorithm: AlgorithmAES256,
			wantErr:   false,
		},
		{
			name:      "AES128 larger key",
			keySize:   AES256KeySize,
			algorithm: AlgorithmAES128,
			wantErr:   false,
		},
		{
			name:      "AES256 larger key",
			keySize:   64,
			algorithm: AlgorithmAES256,
			wantErr:   false,
		},
		{
			name:      "AES128 too small",
			keySize:   8,
			algorithm: AlgorithmAES128,
			wantErr:   true,
		},
		{
			name:      "AES256 too small",
			keySize:   16,
			algorithm: AlgorithmAES256,
			wantErr:   true,
		},
		{
			name:      "invalid algorithm",
			keySize:   32,
			algorithm: "invalid",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := make([]byte, tt.keySize)
			_, err := rand.Read(key)
			require.NoError(t, err)

			derived, err := deriveKey(key, tt.algorithm)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, derived)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, derived)
			}
		})
	}
}

// Test DecryptField with invalid algorithm.
func TestDecryptField_InvalidAlgorithm(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	plaintext := []byte("test data")
	ciphertext, err := EncryptField(plaintext, key, AlgorithmAES256)
	require.NoError(t, err)

	// Try to decrypt with invalid algorithm
	_, err = DecryptField(ciphertext, key, "invalid-algorithm")
	assert.Error(t, err)

	var dragonErr *errors.DragonError
	assert.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, errors.ErrDecryption, dragonErr.Code)
}

// Test DecryptField with empty ciphertext.
func TestDecryptField_EmptyCiphertext(t *testing.T) {
	key := make([]byte, AES128KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	_, err = DecryptField([]byte{}, key, AlgorithmAES128)
	assert.Error(t, err)

	var dragonErr *errors.DragonError
	assert.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, errors.ErrDecryption, dragonErr.Code)
}

func BenchmarkEncryptField_AES128(b *testing.B) {
	key := make([]byte, AES128KeySize)
	rand.Read(key)
	plaintext := []byte("benchmark data that needs to be encrypted")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = EncryptField(plaintext, key, AlgorithmAES128)
	}
}

func BenchmarkEncryptField_AES256(b *testing.B) {
	key := make([]byte, AES256KeySize)
	rand.Read(key)
	plaintext := []byte("benchmark data that needs to be encrypted")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = EncryptField(plaintext, key, AlgorithmAES256)
	}
}

func BenchmarkDecryptField_AES128(b *testing.B) {
	key := make([]byte, AES128KeySize)
	rand.Read(key)
	plaintext := []byte("benchmark data that needs to be encrypted")
	ciphertext, _ := EncryptField(plaintext, key, AlgorithmAES128)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DecryptField(ciphertext, key, AlgorithmAES128)
	}
}

func BenchmarkDecryptField_AES256(b *testing.B) {
	key := make([]byte, AES256KeySize)
	rand.Read(key)
	plaintext := []byte("benchmark data that needs to be encrypted")
	ciphertext, _ := EncryptField(plaintext, key, AlgorithmAES256)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DecryptField(ciphertext, key, AlgorithmAES256)
	}
}

func BenchmarkEncryptField_LargeData(b *testing.B) {
	key := make([]byte, AES256KeySize)
	rand.Read(key)
	plaintext := bytes.Repeat([]byte("A"), 1024*1024) // 1MB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = EncryptField(plaintext, key, AlgorithmAES256)
	}
}

// =============================================================================
// Consolidated from encryption_additional_test.go
// =============================================================================

// TestRecordEncryption tests the recordEncryption metrics function.
func TestRecordEncryption(t *testing.T) {
	// Initialize metrics
	if !metricsInitialized {
		InitMetrics()
	}

	tests := []struct {
		name          string
		algorithm     string
		plaintextSize int
		duration      time.Duration
		err           error
	}{
		{
			name:          "successful encryption with AES128",
			algorithm:     AlgorithmAES128,
			plaintextSize: 100,
			duration:      time.Millisecond,
			err:           nil,
		},
		{
			name:          "successful encryption with AES256",
			algorithm:     AlgorithmAES256,
			plaintextSize: 500,
			duration:      time.Millisecond * 2,
			err:           nil,
		},
		{
			name:          "failed encryption with AES128",
			algorithm:     AlgorithmAES128,
			plaintextSize: 100,
			duration:      time.Millisecond,
			err:           fmt.Errorf("encryption failed"),
		},
		{
			name:          "large data encryption",
			algorithm:     AlgorithmAES256,
			plaintextSize: 1024 * 1024,
			duration:      time.Millisecond * 10,
			err:           nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			assert.NotPanics(t, func() {
				recordEncryption(tt.algorithm, tt.plaintextSize, tt.duration, tt.err)
			})
		})
	}
}

// TestRecordDecryption tests the recordDecryption metrics function.
func TestRecordDecryption(t *testing.T) {
	// Initialize metrics
	if !metricsInitialized {
		InitMetrics()
	}

	tests := []struct {
		name           string
		algorithm      string
		ciphertextSize int
		duration       time.Duration
		err            error
	}{
		{
			name:           "successful decryption with AES128",
			algorithm:      AlgorithmAES128,
			ciphertextSize: 128,
			duration:       time.Millisecond,
			err:            nil,
		},
		{
			name:           "successful decryption with AES256",
			algorithm:      AlgorithmAES256,
			ciphertextSize: 256,
			duration:       time.Millisecond * 2,
			err:            nil,
		},
		{
			name:           "failed decryption",
			algorithm:      AlgorithmAES128,
			ciphertextSize: 128,
			duration:       time.Millisecond,
			err:            fmt.Errorf("decryption failed"),
		},
		{
			name:           "authentication failed",
			algorithm:      AlgorithmAES256,
			ciphertextSize: 256,
			duration:       time.Millisecond,
			err:            fmt.Errorf("authentication failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			assert.NotPanics(t, func() {
				recordDecryption(tt.algorithm, tt.ciphertextSize, tt.duration, tt.err)
			})
		})
	}
}

// TestRecordMarshal tests the recordMarshal metrics function.
func TestRecordMarshal(t *testing.T) {
	// Initialize metrics
	if !metricsInitialized {
		InitMetrics()
	}

	tests := []struct {
		name       string
		fieldCount int
		duration   time.Duration
		err        error
	}{
		{
			name:       "successful marshal with 5 fields",
			fieldCount: 5,
			duration:   time.Millisecond,
			err:        nil,
		},
		{
			name:       "successful marshal with 20 fields",
			fieldCount: 20,
			duration:   time.Millisecond * 3,
			err:        nil,
		},
		{
			name:       "failed marshal",
			fieldCount: 10,
			duration:   time.Millisecond,
			err:        fmt.Errorf("marshal failed"),
		},
		{
			name:       "empty marshal",
			fieldCount: 0,
			duration:   time.Microsecond * 100,
			err:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			assert.NotPanics(t, func() {
				recordMarshal(tt.fieldCount, tt.duration, tt.err)
			})
		})
	}
}

// TestRecordUnmarshal tests the recordUnmarshal metrics function.
func TestRecordUnmarshal(t *testing.T) {
	// Initialize metrics
	if !metricsInitialized {
		InitMetrics()
	}

	tests := []struct {
		name       string
		fieldCount int
		duration   time.Duration
		err        error
	}{
		{
			name:       "successful unmarshal with 5 fields",
			fieldCount: 5,
			duration:   time.Millisecond,
			err:        nil,
		},
		{
			name:       "successful unmarshal with 30 fields",
			fieldCount: 30,
			duration:   time.Millisecond * 5,
			err:        nil,
		},
		{
			name:       "failed unmarshal",
			fieldCount: 10,
			duration:   time.Millisecond,
			err:        fmt.Errorf("unmarshal failed"),
		},
		{
			name:       "corrupted data unmarshal",
			fieldCount: 0,
			duration:   time.Microsecond * 50,
			err:        fmt.Errorf("corrupted data"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			assert.NotPanics(t, func() {
				recordUnmarshal(tt.fieldCount, tt.duration, tt.err)
			})
		})
	}
}

// TestRecordMetrics_Uninitialized tests that record functions handle uninitialized metrics gracefully.
func TestRecordMetrics_Uninitialized(t *testing.T) {
	// Save original state
	originalState := metricsInitialized
	defer func() {
		metricsInitialized = originalState
	}()

	// Temporarily mark as uninitialized
	metricsInitialized = false

	// All record functions should handle this gracefully without panicking
	assert.NotPanics(t, func() {
		recordEncryption(AlgorithmAES128, 100, time.Millisecond, nil)
	})

	assert.NotPanics(t, func() {
		recordDecryption(AlgorithmAES256, 100, time.Millisecond, nil)
	})

	assert.NotPanics(t, func() {
		recordMarshal(5, time.Millisecond, nil)
	})

	assert.NotPanics(t, func() {
		recordUnmarshal(5, time.Millisecond, nil)
	})
}

// TestEncryptField_EdgeCases tests additional edge cases for EncryptField.
func TestEncryptField_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		value     []byte
		keySize   int
		algorithm string
		wantErr   bool
	}{
		{
			name:      "nil key with AES128",
			value:     []byte("test"),
			keySize:   0,
			algorithm: AlgorithmAES128,
			wantErr:   true,
		},
		{
			name:      "nil key with AES256",
			value:     []byte("test"),
			keySize:   0,
			algorithm: AlgorithmAES256,
			wantErr:   true,
		},
		{
			name:      "single byte value",
			value:     []byte("x"),
			keySize:   AES128KeySize,
			algorithm: AlgorithmAES128,
			wantErr:   false,
		},
		{
			name:      "maximum practical size",
			value:     make([]byte, 10*1024*1024), // 10MB
			keySize:   AES256KeySize,
			algorithm: AlgorithmAES256,
			wantErr:   false,
		},
		{
			name:      "key size 1 byte too small for AES128",
			value:     []byte("test"),
			keySize:   AES128KeySize - 1,
			algorithm: AlgorithmAES128,
			wantErr:   true,
		},
		{
			name:      "key size 1 byte too small for AES256",
			value:     []byte("test"),
			keySize:   AES256KeySize - 1,
			algorithm: AlgorithmAES256,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var key []byte
			if tt.keySize > 0 {
				key = make([]byte, tt.keySize)
				_, err := rand.Read(key)
				require.NoError(t, err)
			}

			_, err := EncryptField(tt.value, key, tt.algorithm)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestDecryptField_EdgeCases tests additional edge cases for DecryptField.
func TestDecryptField_EdgeCases(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	// Create a valid ciphertext for some tests
	plaintext := []byte("test data")
	validCiphertext, err := EncryptField(plaintext, key, AlgorithmAES256)
	require.NoError(t, err)

	tests := []struct {
		name       string
		ciphertext []byte
		key        []byte
		algorithm  string
		wantErr    bool
	}{
		{
			name:       "nil ciphertext",
			ciphertext: nil,
			key:        key,
			algorithm:  AlgorithmAES256,
			wantErr:    true,
		},
		{
			name:       "ciphertext with only nonce (no data)",
			ciphertext: make([]byte, GCMNonceSize),
			key:        key,
			algorithm:  AlgorithmAES256,
			wantErr:    true,
		},
		{
			name:       "ciphertext 1 byte too short",
			ciphertext: validCiphertext[:GCMNonceSize-1],
			key:        key,
			algorithm:  AlgorithmAES256,
			wantErr:    true,
		},
		{
			name:       "corrupted nonce",
			ciphertext: corruptData(validCiphertext, 0, 5),
			key:        key,
			algorithm:  AlgorithmAES256,
			wantErr:    true,
		},
		{
			name:       "corrupted tag",
			ciphertext: corruptData(validCiphertext, len(validCiphertext)-1, 1),
			key:        key,
			algorithm:  AlgorithmAES256,
			wantErr:    true,
		},
		{
			name:       "nil key",
			ciphertext: validCiphertext,
			key:        nil,
			algorithm:  AlgorithmAES256,
			wantErr:    true,
		},
		{
			name:       "wrong algorithm for ciphertext",
			ciphertext: validCiphertext,
			key:        key,
			algorithm:  AlgorithmAES128,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecryptField(tt.ciphertext, tt.key, tt.algorithm)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestGenerateKey_Scenarios tests various key generation scenarios.
func TestGenerateKey_Scenarios(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		wantSize  int
		wantErr   bool
	}{
		{
			name:      "AES128 generates 16 bytes",
			algorithm: AlgorithmAES128,
			wantSize:  AES128KeySize,
			wantErr:   false,
		},
		{
			name:      "AES256 generates 32 bytes",
			algorithm: AlgorithmAES256,
			wantSize:  AES256KeySize,
			wantErr:   false,
		},
		{
			name:      "empty algorithm fails",
			algorithm: "",
			wantSize:  0,
			wantErr:   true,
		},
		{
			name:      "unsupported algorithm fails",
			algorithm: "aes512",
			wantSize:  0,
			wantErr:   true,
		},
		{
			name:      "case sensitive algorithm",
			algorithm: "AES128",
			wantSize:  0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := GenerateKey(tt.algorithm)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, key)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, key)
				assert.Equal(t, tt.wantSize, len(key))

				// Verify key contains non-zero bytes
				hasNonZero := false
				for _, b := range key {
					if b != 0 {
						hasNonZero = true
						break
					}
				}
				assert.True(t, hasNonZero, "key should contain non-zero bytes")
			}
		})
	}
}

// TestGenerateKey_Statistical tests key generation for statistical properties.
func TestGenerateKey_Statistical(t *testing.T) {
	const iterations = 1000
	keysAES128 := make([][]byte, iterations)
	keysAES256 := make([][]byte, iterations)

	// Generate many keys
	for i := 0; i < iterations; i++ {
		key128, err := GenerateKey(AlgorithmAES128)
		require.NoError(t, err)
		keysAES128[i] = key128

		key256, err := GenerateKey(AlgorithmAES256)
		require.NoError(t, err)
		keysAES256[i] = key256
	}

	// Verify uniqueness for AES128
	uniqueAES128 := make(map[string]bool)
	for _, key := range keysAES128 {
		uniqueAES128[string(key)] = true
	}
	assert.Equal(t, iterations, len(uniqueAES128), "all AES128 keys should be unique")

	// Verify uniqueness for AES256
	uniqueAES256 := make(map[string]bool)
	for _, key := range keysAES256 {
		uniqueAES256[string(key)] = true
	}
	assert.Equal(t, iterations, len(uniqueAES256), "all AES256 keys should be unique")

	// Check for reasonable distribution (basic test)
	// For a random key, expect roughly 50% of bits to be 1
	for _, key := range keysAES256 {
		onesCount := 0
		for _, b := range key {
			for bit := 0; bit < 8; bit++ {
				if (b & (1 << bit)) != 0 {
					onesCount++
				}
			}
		}
		totalBits := len(key) * 8
		ratio := float64(onesCount) / float64(totalBits)
		// Allow 30-70% range for randomness (loose bounds)
		assert.True(t, ratio >= 0.3 && ratio <= 0.7,
			"key should have reasonable bit distribution, got %.2f", ratio)
	}
}

// TestEncryptDecrypt_Interoperability tests that different key sizes work correctly.
func TestEncryptDecrypt_Interoperability(t *testing.T) {
	plaintext := []byte("interoperability test data")

	// Test with exact key sizes
	key128 := make([]byte, AES128KeySize)
	key256 := make([]byte, AES256KeySize)
	_, err := rand.Read(key128)
	require.NoError(t, err)
	_, err = rand.Read(key256)
	require.NoError(t, err)

	// Encrypt with AES128
	ciphertext128, err := EncryptField(plaintext, key128, AlgorithmAES128)
	require.NoError(t, err)

	// Encrypt with AES256
	ciphertext256, err := EncryptField(plaintext, key256, AlgorithmAES256)
	require.NoError(t, err)

	// Decrypt with AES128
	decrypted128, err := DecryptField(ciphertext128, key128, AlgorithmAES128)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted128)

	// Decrypt with AES256
	decrypted256, err := DecryptField(ciphertext256, key256, AlgorithmAES256)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted256)

	// Cross-algorithm decryption should fail
	_, err = DecryptField(ciphertext128, key256, AlgorithmAES256)
	assert.Error(t, err, "AES128 ciphertext should not decrypt with AES256 key")

	_, err = DecryptField(ciphertext256, key128, AlgorithmAES128)
	assert.Error(t, err, "AES256 ciphertext should not decrypt with AES128 key")
}

// TestDecryptField_MalformedData tests decryption with various malformed inputs.
func TestDecryptField_MalformedData(t *testing.T) {
	key := make([]byte, AES128KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	tests := []struct {
		name       string
		ciphertext []byte
	}{
		{
			name:       "all zeros",
			ciphertext: make([]byte, 32),
		},
		{
			name:       "all ones",
			ciphertext: makeFilledBytes(32, 0xFF),
		},
		{
			name:       "random data",
			ciphertext: makeRandomBytes(32),
		},
		{
			name:       "partial nonce",
			ciphertext: makeRandomBytes(GCMNonceSize / 2),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecryptField(tt.ciphertext, key, AlgorithmAES128)
			assert.Error(t, err, "malformed data should fail decryption")
		})
	}
}

// TestEncryptField_ConcurrentOperations tests thread safety of encryption.
func TestEncryptField_ConcurrentOperations(t *testing.T) {
	const goroutines = 100
	const iterations = 10

	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	done := make(chan bool, goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer func() { done <- true }()

			for i := 0; i < iterations; i++ {
				plaintext := []byte("concurrent test data")
				ciphertext, err := EncryptField(plaintext, key, AlgorithmAES256)
				assert.NoError(t, err)
				assert.NotNil(t, ciphertext)

				decrypted, err := DecryptField(ciphertext, key, AlgorithmAES256)
				assert.NoError(t, err)
				assert.Equal(t, plaintext, decrypted)
			}
		}(g)
	}

	// Wait for all goroutines
	for i := 0; i < goroutines; i++ {
		<-done
	}
}

// Helper functions

// corruptData corrupts a byte slice at the specified offset.
func corruptData(data []byte, offset, length int) []byte {
	corrupted := make([]byte, len(data))
	copy(corrupted, data)
	for i := offset; i < offset+length && i < len(corrupted); i++ {
		corrupted[i] ^= 0xFF
	}
	return corrupted
}

// makeFilledBytes creates a byte slice filled with the specified value.
func makeFilledBytes(size int, value byte) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = value
	}
	return data
}

// makeRandomBytes creates a byte slice filled with random data.
func makeRandomBytes(size int) []byte {
	data := make([]byte, size)
	rand.Read(data)
	return data
}
