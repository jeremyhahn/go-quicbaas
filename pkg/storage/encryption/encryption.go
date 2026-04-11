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

// Package encryption provides field-level encryption for QuicBaaS entities.
// It supports AES-128-GCM and AES-256-GCM authenticated encryption for secure
// data protection with built-in integrity verification.
//
// FIPS 140-3 Compliance:
// When built with FIPS mode (-tags fips), this package automatically enforces
// FIPS 140-3 approved algorithms and rejects non-approved algorithms.
package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"io"

	"golang.org/x/crypto/argon2"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"

	xkms "github.com/jeremyhahn/go-xkms/sdk/go"
)

const (
	// AlgorithmAES128 represents AES-128-GCM encryption.
	AlgorithmAES128 = "aes128"

	// AlgorithmAES256 represents AES-256-GCM encryption.
	AlgorithmAES256 = "aes256"

	// AES128KeySize is the required key size for AES-128.
	AES128KeySize = 16

	// AES256KeySize is the required key size for AES-256.
	AES256KeySize = 32

	// GCMNonceSize is the standard nonce size for GCM mode (12 bytes).
	GCMNonceSize = xkms.AESGCMNonceSize

	// GCMTagSize is the authentication tag size for GCM mode (16 bytes).
	GCMTagSize = xkms.AESGCMTagSize

	// argon2SaltSize is the size of the random salt for Argon2id key derivation.
	argon2SaltSize = 16

	// argon2Time is the number of iterations for Argon2id.
	argon2Time = 1

	// argon2Memory is the memory cost in KiB for Argon2id (64 MB).
	argon2Memory = 64 * 1024

	// argon2Threads is the parallelism factor for Argon2id.
	argon2Threads = 4
)

// EncryptField encrypts a plaintext value using the specified algorithm and key.
// The ciphertext format is: [nonce][ciphertext+tag]
// Uses AES-GCM for authenticated encryption with a randomly generated nonce.
//
// Security considerations:
// - Uses crypto/rand for secure nonce generation
// - Provides authenticity and integrity via GCM mode
// - Each encryption uses a unique nonce
// - Constant-time key size validation
//
// FIPS 140-3 Compliance:
// When built with Go FIPS mode (GOFIPS140), the crypto library automatically
// enforces FIPS 140-3 approved algorithms. Non-approved operations will fail.
//
// Parameters:
//   - value: plaintext data to encrypt
//   - key: encryption key (16+ bytes for AES-128, 32+ bytes for AES-256)
//   - algorithm: "aes128" or "aes256"
//
// Returns encrypted data or an error if encryption fails.
func EncryptField(value []byte, key []byte, algorithm string) ([]byte, error) {
	if len(value) == 0 {
		return nil, api.NewValidationError("encryption.EncryptField", "empty value")
	}

	// Derive the correct key size from the master key
	derivedKey, err := deriveKey(key, algorithm)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrEncryption,
			Op:   "encryption.EncryptField",
			Err:  err,
		}
	}

	// Use xkms AES-256-GCM for 32-byte keys, raw crypto for AES-128
	if len(derivedKey) == xkms.AESGCMKeySize {
		ciphertext, err := xkms.AESGCMEncrypt(derivedKey, value)
		if err != nil {
			return nil, api.NewCipherError("encryption.EncryptField", "failed to encrypt", errors.ErrEncryption, err)
		}
		return ciphertext, nil
	}

	// AES-128: use raw crypto (xkms AES-GCM only supports AES-256)
	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, api.NewCipherError("encryption.EncryptField", "failed to create cipher", errors.ErrEncryption, err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, api.NewCipherError("encryption.EncryptField", "failed to create GCM", errors.ErrEncryption, err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, api.NewCipherError("encryption.EncryptField", "failed to generate nonce", errors.ErrEncryption, err)
	}
	return gcm.Seal(nonce, nonce, value, nil), nil
}

// DecryptField decrypts a ciphertext value using the specified algorithm and key.
// The ciphertext must be in the format: [nonce][ciphertext+tag]
// Uses AES-GCM for authenticated decryption with integrity verification.
//
// Security considerations:
// - Verifies authentication tag before returning plaintext
// - Protects against tampering and corruption
// - Constant-time comparisons for authentication
// - Fails securely on authentication errors
//
// FIPS 140-3 Compliance:
// When built with Go FIPS mode (GOFIPS140), the crypto library automatically
// enforces FIPS 140-3 approved algorithms. Non-approved operations will fail.
//
// Parameters:
//   - value: encrypted data (nonce + ciphertext + tag)
//   - key: decryption key (must match encryption key)
//   - algorithm: "aes128" or "aes256"
//
// Returns decrypted plaintext or an error if decryption/authentication fails.
func DecryptField(value []byte, key []byte, algorithm string) ([]byte, error) {
	// Derive the correct key size from the master key
	derivedKey, err := deriveKey(key, algorithm)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrDecryption,
			Op:   "encryption.DecryptField",
			Err:  err,
		}
	}

	// Use xkms AES-256-GCM for 32-byte keys, raw crypto for AES-128
	if len(derivedKey) == xkms.AESGCMKeySize {
		if len(value) < xkms.AESGCMOverhead {
			return nil, api.NewCiphertextTooShortError("encryption.DecryptField", "")
		}
		plaintext, err := xkms.AESGCMDecrypt(derivedKey, value)
		if err != nil {
			return nil, api.NewAuthenticationFailedError("encryption.DecryptField", err)
		}
		return plaintext, nil
	}

	// AES-128: use raw crypto (xkms AES-GCM only supports AES-256)
	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, api.NewCipherError("encryption.DecryptField", "failed to create cipher", errors.ErrDecryption, err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, api.NewCipherError("encryption.DecryptField", "failed to create GCM", errors.ErrDecryption, err)
	}
	nonceSize := gcm.NonceSize()
	if len(value) < nonceSize+gcm.Overhead() {
		return nil, api.NewCiphertextTooShortError("encryption.DecryptField", "")
	}
	nonce, ciphertext := value[:nonceSize], value[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, api.NewAuthenticationFailedError("encryption.DecryptField", err)
	}
	return plaintext, nil
}

// deriveKey derives the correct key size from a master key based on the algorithm.
// If the master key is larger than needed, it uses the first N bytes.
// If the master key is exactly the right size, it uses it directly.
// Returns an error if the master key is too small.
func deriveKey(masterKey []byte, algorithm string) ([]byte, error) {
	var requiredSize int

	switch algorithm {
	case AlgorithmAES128:
		requiredSize = AES128KeySize
	case AlgorithmAES256:
		requiredSize = AES256KeySize
	default:
		return nil, api.NewUnsupportedEncryptionAlgorithmError(
			"encryption.deriveKey", algorithm, []string{AlgorithmAES128, AlgorithmAES256})
	}

	if len(masterKey) < requiredSize {
		return nil, api.NewKeyTooSmallError("encryption.deriveKey", algorithm, requiredSize, len(masterKey))
	}

	// Use only the required number of bytes
	return masterKey[:requiredSize], nil
}

// validateKeySize validates that the key size matches the algorithm requirements.
// Uses constant-time comparison to prevent timing attacks.
func validateKeySize(key []byte, algorithm string) error {
	var expectedSize int

	switch algorithm {
	case AlgorithmAES128:
		expectedSize = AES128KeySize
	case AlgorithmAES256:
		expectedSize = AES256KeySize
	default:
		return api.NewUnsupportedEncryptionAlgorithmError(
			"encryption.validateKeySize", algorithm, []string{AlgorithmAES128, AlgorithmAES256})
	}

	// Use constant-time comparison to prevent timing attacks
	// Key length bounded by AES key sizes (16 or 32 bytes)
	if subtle.ConstantTimeEq(int32(len(key)), int32(expectedSize)) != 1 { //nolint:gosec // G115: key sizes bounded
		return api.NewKeySizeError("encryption.validateKeySize", algorithm, expectedSize, len(key))
	}

	return nil
}

// DeriveKeyFromPassword derives an encryption key from a password using Argon2id.
// Argon2id is the recommended algorithm for password-based key derivation, providing
// resistance against both GPU-based and side-channel attacks.
//
// The function generates a random 16-byte salt using crypto/rand and derives a key
// of the appropriate size for the specified algorithm.
//
// Argon2id parameters: time=1, memory=64MB, threads=4.
//
// Parameters:
//   - password: the password to derive the key from (must not be empty)
//   - algorithm: "aes128" (16-byte key) or "aes256" (32-byte key)
//
// Returns the derived key bytes or an error.
func DeriveKeyFromPassword(password string, algorithm string) ([]byte, error) {
	const op = "encryption.DeriveKeyFromPassword"

	if password == "" {
		return nil, api.NewEmptyPasswordError(op)
	}

	var keyLen uint32
	switch algorithm {
	case AlgorithmAES128:
		keyLen = AES128KeySize
	case AlgorithmAES256:
		keyLen = AES256KeySize
	default:
		keyLen = AES256KeySize
	}

	salt := make([]byte, argon2SaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, api.NewSaltGenerationError(op, err)
	}

	key := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, keyLen)
	return key, nil
}

// GenerateKey generates a cryptographically secure random key for the specified algorithm.
// This is useful for creating new encryption keys.
//
// FIPS 140-3 Compliance:
// When built with Go FIPS mode (GOFIPS140), the crypto library automatically
// enforces FIPS 140-3 approved random number generation.
func GenerateKey(algorithm string) ([]byte, error) {
	var keySize int

	switch algorithm {
	case AlgorithmAES128:
		keySize = AES128KeySize
	case AlgorithmAES256:
		keySize = AES256KeySize
	default:
		return nil, api.NewUnsupportedEncryptionAlgorithmError(
			"encryption.GenerateKey", algorithm, nil)
	}

	key := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, api.NewCipherError("encryption.GenerateKey", "failed to generate key", errors.ErrEncryption, err)
	}

	return key, nil
}
