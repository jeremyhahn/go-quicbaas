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

// Package interfaces defines core interfaces for QuicBaaS components.
package interfaces

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Algorithm constants for encryption operations.
const (
	// AlgorithmAES256GCM is AES-256 in GCM mode (recommended default).
	AlgorithmAES256GCM = "AES-256-GCM"

	// AlgorithmAES128GCM is AES-128 in GCM mode.
	AlgorithmAES128GCM = "AES-128-GCM"

	// AlgorithmChaCha20Poly1305 is ChaCha20-Poly1305 AEAD cipher.
	AlgorithmChaCha20Poly1305 = "CHACHA20-POLY1305"

	// DefaultAlgorithm is the default encryption algorithm.
	DefaultAlgorithm = AlgorithmAES256GCM
)

// Errors for secrets encryption operations.
var (
	// ErrInvalidKeyIdentifier indicates the key identifier format is invalid.
	ErrInvalidKeyIdentifier = errors.New("invalid key identifier")

	// ErrKeyNotFound indicates the specified key does not exist.
	ErrKeyNotFound = errors.New("key not found")

	// ErrEncryptionFailed indicates encryption operation failed.
	ErrEncryptionFailed = errors.New("encryption failed")

	// ErrDecryptionFailed indicates decryption operation failed.
	ErrDecryptionFailed = errors.New("decryption failed")

	// ErrUnsupportedAlgorithm indicates the algorithm is not supported.
	ErrUnsupportedAlgorithm = errors.New("unsupported algorithm")

	// ErrInvalidBlob indicates the encrypted blob is invalid or corrupted.
	ErrInvalidBlob = errors.New("invalid encrypted blob")
)

// EncryptedBlob represents an encrypted secret with all necessary metadata
// for decryption and audit. The blob is self-describing and includes
// cryptographic binding to the secret path via AAD (Additional Authenticated Data).
type EncryptedBlob struct {
	// KeyIdentifier is either a DEKID (e.g., "dek:global", "dek:tenant:{id}")
	// or a KeyID in go-xkms format for direct KMS key reference.
	KeyIdentifier string

	// KeyVersion tracks the version of the key used for encryption.
	// This is essential for key rotation scenarios where multiple
	// versions of a key may coexist.
	KeyVersion uint64

	// Algorithm specifies the encryption algorithm used.
	// Must be one of the defined Algorithm constants.
	Algorithm string

	// Nonce is the initialization vector or nonce used for encryption.
	// Size depends on the algorithm:
	// - AES-GCM: 12 bytes (96 bits) recommended
	// - ChaCha20-Poly1305: 12 bytes (96 bits) for standard, 24 bytes for XChaCha20
	Nonce []byte

	// Ciphertext is the encrypted data.
	Ciphertext []byte

	// Tag is the authentication tag from AEAD encryption.
	// Typically 16 bytes (128 bits) for GCM and Poly1305.
	Tag []byte

	// AAD is Additional Authenticated Data bound to the ciphertext.
	// For secrets, this should be the secret path to cryptographically
	// bind the encrypted data to its storage location, preventing
	// ciphertext substitution attacks.
	AAD []byte

	// EncryptedAt records when the encryption occurred for audit purposes.
	EncryptedAt time.Time
}

// Validate checks that the EncryptedBlob has all required fields populated
// and that the values are within expected ranges.
func (b *EncryptedBlob) Validate() error {
	if b == nil {
		return ErrInvalidBlob
	}

	if b.KeyIdentifier == "" {
		return ErrInvalidKeyIdentifier
	}

	if b.Algorithm == "" {
		return ErrUnsupportedAlgorithm
	}

	// Validate algorithm is supported
	switch b.Algorithm {
	case AlgorithmAES256GCM, AlgorithmAES128GCM, AlgorithmChaCha20Poly1305:
		// Valid algorithm
	default:
		return ErrUnsupportedAlgorithm
	}

	// Check nonce is present and has reasonable size
	if len(b.Nonce) == 0 {
		return ErrInvalidBlob
	}

	// Validate nonce size based on algorithm
	switch b.Algorithm {
	case AlgorithmAES256GCM, AlgorithmAES128GCM:
		if len(b.Nonce) != 12 {
			return ErrInvalidBlob
		}
	case AlgorithmChaCha20Poly1305:
		if len(b.Nonce) != 12 && len(b.Nonce) != 24 {
			return ErrInvalidBlob
		}
	}

	// Ciphertext must be present
	if len(b.Ciphertext) == 0 {
		return ErrInvalidBlob
	}

	// Tag must be present and correct size (16 bytes for GCM and Poly1305)
	if len(b.Tag) != 16 {
		return ErrInvalidBlob
	}

	// Timestamp should not be zero
	if b.EncryptedAt.IsZero() {
		return ErrInvalidBlob
	}

	return nil
}

// IsDEKEncrypted returns true if the blob was encrypted using a managed
// Data Encryption Key (DEK) rather than a direct KMS key.
// DEK identifiers follow the pattern "dek:*".
func (b *EncryptedBlob) IsDEKEncrypted() bool {
	return strings.HasPrefix(b.KeyIdentifier, "dek:")
}

// EncryptOptions provides configuration options for encryption operations.
type EncryptOptions struct {
	// Algorithm specifies the encryption algorithm to use.
	// If empty, defaults to DefaultAlgorithm (AES-256-GCM).
	Algorithm string

	// AAD is optional Additional Authenticated Data to bind to the ciphertext.
	// For secrets storage, this should be the secret path to prevent
	// ciphertext substitution attacks.
	AAD []byte
}

// SecretsEncryptor defines the interface for encrypting and decrypting secrets.
// This interface abstracts the encryption layer from the storage layer,
// allowing secrets to be encrypted with either managed DEKs or direct KMS keys.
//
// The encryptor ensures:
//  1. Strong encryption using AEAD ciphers (AES-GCM, ChaCha20-Poly1305)
//  2. Cryptographic binding of ciphertext to secret path via AAD
//  3. Support for key rotation via version tracking
//  4. Audit trail via timestamp metadata
//
// Key Identifier Formats:
//
//   - DEK: "dek:global" - Global cluster DEK
//   - DEK: "dek:tenant:{tenantID}" - Tenant-specific DEK
//   - KeyID: go-xkms format - Direct KMS key reference
type SecretsEncryptor interface {
	// Encrypt encrypts plaintext using the specified key identifier.
	//
	// The keyIdentifier can be:
	//   - A DEKID (e.g., "dek:global", "dek:tenant:acme-corp") for envelope encryption
	//   - A KeyID in go-xkms format for direct KMS encryption
	//
	// If opts.Algorithm is empty, DefaultAlgorithm is used.
	// If opts.AAD is provided, it will be included in authentication but not encrypted.
	//
	// Returns an EncryptedBlob containing all necessary information for decryption,
	// or an error if encryption fails.
	Encrypt(ctx context.Context, keyIdentifier string, plaintext []byte, opts *EncryptOptions) (*EncryptedBlob, error)

	// Decrypt decrypts an EncryptedBlob and returns the plaintext.
	//
	// The method validates the blob structure, retrieves the appropriate key,
	// verifies the authentication tag, and returns the decrypted data.
	//
	// Returns the plaintext bytes or an error if:
	//   - The blob is invalid or corrupted
	//   - The key cannot be found
	//   - Authentication fails (indicating tampering)
	//   - Decryption fails
	Decrypt(ctx context.Context, blob *EncryptedBlob) ([]byte, error)

	// ValidateKeyIdentifier validates that the key identifier has correct format
	// and that the referenced key exists and is accessible.
	//
	// This method can be used to verify key availability before attempting
	// encryption operations, allowing for early error detection.
	//
	// Returns nil if the key identifier is valid and the key exists,
	// or an appropriate error otherwise.
	ValidateKeyIdentifier(ctx context.Context, keyIdentifier string) error
}
