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

func TestEncryptDecryptField(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		keySize   int
		plaintext []byte
	}{
		{"AES128 small text", AlgorithmAES128, AES128KeySize, []byte("Hello, World!")},
		{"AES128 large text", AlgorithmAES128, AES128KeySize, bytes.Repeat([]byte("A"), 10000)},
		{"AES128 unicode text", AlgorithmAES128, AES128KeySize, []byte("Hello 世界! 🔐")},
		{"AES128 binary data", AlgorithmAES128, AES128KeySize, []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0xfd}},
		{"AES128 single byte", AlgorithmAES128, AES128KeySize, []byte("x")},
		{"AES256 small text", AlgorithmAES256, AES256KeySize, []byte("Secret message")},
		{"AES256 json data", AlgorithmAES256, AES256KeySize, []byte(`{"username":"admin","password":"secret"}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := make([]byte, tt.keySize)
			_, err := rand.Read(key)
			require.NoError(t, err)

			ciphertext, err := EncryptField(tt.plaintext, key, tt.algorithm)
			require.NoError(t, err)

			// Ciphertext carries the nonce and tag, and never the plaintext.
			assert.Greater(t, len(ciphertext), len(tt.plaintext))
			assert.NotEqual(t, tt.plaintext, ciphertext)

			decrypted, err := DecryptField(ciphertext, key, tt.algorithm)
			require.NoError(t, err)
			assert.Equal(t, tt.plaintext, decrypted)
		})
	}
}

func TestEncryptField_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		value     []byte
		keySize   int
		algorithm string
		wantErr   bool
		wantCode  errors.ErrorCode
	}{
		{"empty value", []byte{}, AES128KeySize, AlgorithmAES128, true, errors.ErrInvalidInput},
		{"nil key", []byte("test"), 0, AlgorithmAES128, true, errors.ErrEncryption},
		{"AES128 key one byte too small", []byte("test"), AES128KeySize - 1, AlgorithmAES128, true, errors.ErrEncryption},
		{"AES256 key one byte too small", []byte("test"), AES256KeySize - 1, AlgorithmAES256, true, errors.ErrEncryption},
		{"invalid algorithm", []byte("test"), AES128KeySize, "invalid-algorithm", true, errors.ErrEncryption},
		{"AES128 exact key", []byte("test"), AES128KeySize, AlgorithmAES128, false, 0},
		{"AES256 exact key", []byte("test"), AES256KeySize, AlgorithmAES256, false, 0},
		{"AES256 larger key uses first 32 bytes", []byte("test"), 64, AlgorithmAES256, false, 0},
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
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			var qbErr *errors.QBError
			require.ErrorAs(t, err, &qbErr)
			assert.Equal(t, tt.wantCode, qbErr.Code)
		})
	}
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
		{"AES128 generates 16 bytes", AlgorithmAES128, AES128KeySize, false},
		{"AES256 generates 32 bytes", AlgorithmAES256, AES256KeySize, false},
		{"empty algorithm fails", "", 0, true},
		{"unsupported algorithm fails", "aes512", 0, true},
		{"algorithm is case sensitive", "AES128", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := GenerateKey(tt.algorithm)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, key)
				return
			}
			require.NoError(t, err)
			assert.Len(t, key, tt.wantSize)

			// Each call draws a fresh key: a second key must differ.
			other, err := GenerateKey(tt.algorithm)
			require.NoError(t, err)
			assert.NotEqual(t, key, other, "generated duplicate key")
		})
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

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
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

// TestRecordMetrics drives every record function down its success and error
// branch with metrics initialized, and down its early return without.
func TestRecordMetrics(t *testing.T) {
	InitMetrics()
	failure := fmt.Errorf("operation failed")

	record := map[string]func(err error){
		"encryption": func(err error) { recordEncryption(AlgorithmAES128, 100, time.Millisecond, err) },
		"decryption": func(err error) { recordDecryption(AlgorithmAES256, 128, time.Millisecond, err) },
		"marshal":    func(err error) { recordMarshal(5, time.Millisecond, err) },
		"unmarshal":  func(err error) { recordUnmarshal(5, time.Millisecond, err) },
	}

	for name, fn := range record {
		t.Run(name, func(t *testing.T) {
			assert.NotPanics(t, func() { fn(nil) })
			assert.NotPanics(t, func() { fn(failure) })
		})
	}

	t.Run("uninitialized", func(t *testing.T) {
		originalState := metricsInitialized
		defer func() { metricsInitialized = originalState }()
		metricsInitialized = false

		for _, fn := range record {
			assert.NotPanics(t, func() { fn(nil) })
		}
	})
}

// TestDecryptField_EdgeCases tests additional edge cases for DecryptField.
func TestDecryptField_EdgeCases(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	otherKey := make([]byte, AES256KeySize)
	_, err = rand.Read(otherKey)
	require.NoError(t, err)
	key128 := key[:AES128KeySize]

	validCiphertext, err := EncryptField([]byte("test data"), key, AlgorithmAES256)
	require.NoError(t, err)

	// Every failure is reported as a QBError with the ErrDecryption code.
	tests := []struct {
		name       string
		ciphertext []byte
		key        []byte
		algorithm  string
	}{
		{"AES256 nil ciphertext", nil, key, AlgorithmAES256},
		{"AES256 ciphertext with only nonce", make([]byte, GCMNonceSize), key, AlgorithmAES256},
		{"AES256 corrupted nonce", corruptData(validCiphertext, 0, 5), key, AlgorithmAES256},
		{"AES256 corrupted tag", corruptData(validCiphertext, len(validCiphertext)-1, 1), key, AlgorithmAES256},
		{"AES256 wrong key", validCiphertext, otherKey, AlgorithmAES256},
		{"AES128 empty ciphertext", []byte{}, key128, AlgorithmAES128},
		{"AES128 truncated ciphertext", []byte{0x01, 0x02, 0x03}, key128, AlgorithmAES128},
		{"AES128 malformed ciphertext", make([]byte, 32), key128, AlgorithmAES128},
		{"wrong algorithm for ciphertext", validCiphertext, key, AlgorithmAES128},
		{"nil key", validCiphertext, nil, AlgorithmAES256},
		{"invalid algorithm", validCiphertext, key, "invalid-algorithm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecryptField(tt.ciphertext, tt.key, tt.algorithm)
			var qbErr *errors.QBError
			require.ErrorAs(t, err, &qbErr)
			assert.Equal(t, errors.ErrDecryption, qbErr.Code)
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
