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

package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"time"

	"golang.org/x/crypto/chacha20poly1305"

	xkms "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// SecretsDEKProvider defines the interface for managing Data Encryption Keys.
// This allows the encryptor to retrieve DEKs for encryption/decryption.
type SecretsDEKProvider interface {
	// GetKey retrieves a DEK by its identifier.
	// Returns the key material, version, and any error.
	GetKey(ctx context.Context, dekid DEKID) ([]byte, uint64, error)

	// Validate checks if a DEKID is valid and exists.
	Validate(ctx context.Context, dekid DEKID) error
}

// secretsEncryptor implements the interfaces.SecretsEncryptor interface.
// It routes encryption operations based on key identifier prefix:
//   - "dek:*" -> Use SecretsDEK for managed DEK operations
//   - Otherwise -> Use KMSManager for direct KMS encryption
type secretsEncryptor struct {
	dek    SecretsDEKProvider    // For managed DEK encryption
	kms    interfaces.KMSManager // For direct KMS encryption
	logger interfaces.Logger
}

// NewSecretsEncryptor creates a new SecretsEncryptor instance.
func NewSecretsEncryptor(dek SecretsDEKProvider, kms interfaces.KMSManager, logger interfaces.Logger) interfaces.SecretsEncryptor {
	return &secretsEncryptor{
		dek:    dek,
		kms:    kms,
		logger: logger,
	}
}

// Encrypt encrypts plaintext using the specified key identifier.
func (e *secretsEncryptor) Encrypt(ctx context.Context, keyIdentifier string, plaintext []byte, opts *interfaces.EncryptOptions) (*interfaces.EncryptedBlob, error) {
	if keyIdentifier == "" {
		return nil, interfaces.ErrInvalidKeyIdentifier
	}

	// Set default options
	if opts == nil {
		opts = &interfaces.EncryptOptions{}
	}

	algorithm := opts.Algorithm
	if algorithm == "" {
		algorithm = interfaces.DefaultAlgorithm
	}

	// Validate algorithm
	if !isValidAlgorithm(algorithm) {
		return nil, interfaces.ErrUnsupportedAlgorithm
	}

	// Route based on key identifier prefix
	if IsDEKID(keyIdentifier) {
		return e.encryptWithDEK(ctx, keyIdentifier, plaintext, algorithm, opts.AAD)
	}

	return e.encryptWithKMS(ctx, keyIdentifier, plaintext, algorithm, opts.AAD)
}

// copyKey creates a defensive copy of key material so the caller can
// safely zero it without corrupting the provider's internal state.
func copyKey(src []byte) []byte {
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

// encryptWithDEK performs local AEAD encryption using a DEK.
func (e *secretsEncryptor) encryptWithDEK(ctx context.Context, keyIdentifier string, plaintext []byte, algorithm string, aad []byte) (*interfaces.EncryptedBlob, error) {
	// Parse and validate DEKID
	dekid, err := ParseDEKID(keyIdentifier)
	if err != nil {
		e.logger.Errorf("Invalid DEKID format: %v", err)
		return nil, api.NewSentinelWrapError("secrets.encryptor.encryptWithDEK", dberrors.ErrInvalidInput, interfaces.ErrInvalidKeyIdentifier, err)
	}

	// Get DEK material
	rawKey, version, err := e.dek.GetKey(ctx, dekid)
	if err != nil {
		e.logger.Errorf("Failed to get DEK: %v", err)
		return nil, api.NewDEKRetrievalError("secrets.encryptor.encryptWithDEK", err)
	}

	// Copy key material so zeroing does not corrupt the provider's internal state
	key := copyKey(rawKey)
	defer xkms.SecureMemZero(key)

	// Validate key size matches algorithm
	expectedSize := getKeySizeForAlgorithm(algorithm)
	if len(key) != expectedSize {
		e.logger.Errorf("DEK size mismatch: got %d, expected %d for %s", len(key), expectedSize, algorithm)
		return nil, api.NewDEKSizeMismatchError("secrets.encryptor.encryptWithDEK", expectedSize, algorithm)
	}

	// Perform AEAD encryption
	nonce, ciphertext, tag, err := encryptAEAD(key, plaintext, aad, algorithm)
	if err != nil {
		e.logger.Errorf("AEAD encryption failed: %v", err)
		return nil, api.NewSentinelWrapError("secrets.encryptor.encryptWithDEK", dberrors.ErrEncryption, interfaces.ErrEncryptionFailed, err)
	}

	// Build encrypted blob
	blob := &interfaces.EncryptedBlob{
		KeyIdentifier: keyIdentifier,
		KeyVersion:    version,
		Algorithm:     algorithm,
		Nonce:         nonce,
		Ciphertext:    ciphertext,
		Tag:           tag,
		AAD:           aad,
		EncryptedAt:   time.Now().UTC(),
	}

	return blob, nil
}

// encryptWithKMS delegates encryption to the KMS manager.
func (e *secretsEncryptor) encryptWithKMS(ctx context.Context, keyIdentifier string, plaintext []byte, algorithm string, aad []byte) (*interfaces.EncryptedBlob, error) {
	// Use KMS to encrypt
	ciphertext, err := e.kms.Encrypt(ctx, keyIdentifier, plaintext, aad)
	if err != nil {
		e.logger.Errorf("KMS encryption failed: %v", err)
		return nil, api.NewSentinelWrapError("secrets.encryptor.encryptWithKMS", dberrors.ErrEncryption, interfaces.ErrEncryptionFailed, err)
	}

	// For KMS encryption, the ciphertext is opaque
	// We need to create dummy nonce/tag to satisfy Validate() requirements
	// The actual KMS encryption handles these internally
	blob := &interfaces.EncryptedBlob{
		KeyIdentifier: keyIdentifier,
		KeyVersion:    0, // KMS manages versioning internally
		Algorithm:     algorithm,
		Nonce:         make([]byte, 12), // Dummy nonce for validation
		Ciphertext:    ciphertext,
		Tag:           make([]byte, 16), // Dummy tag for validation
		AAD:           aad,
		EncryptedAt:   time.Now().UTC(),
	}

	return blob, nil
}

// Decrypt decrypts an EncryptedBlob and returns the plaintext.
func (e *secretsEncryptor) Decrypt(ctx context.Context, blob *interfaces.EncryptedBlob) ([]byte, error) {
	// Validate blob
	if err := blob.Validate(); err != nil {
		return nil, api.NewInvalidBlobError("secrets.encryptor.Decrypt", err)
	}

	// Route based on key identifier
	if blob.IsDEKEncrypted() {
		return e.decryptWithDEK(ctx, blob)
	}

	return e.decryptWithKMS(ctx, blob)
}

// decryptWithDEK performs local AEAD decryption using a DEK.
func (e *secretsEncryptor) decryptWithDEK(ctx context.Context, blob *interfaces.EncryptedBlob) ([]byte, error) {
	// Parse DEKID
	dekid, err := ParseDEKID(blob.KeyIdentifier)
	if err != nil {
		e.logger.Errorf("Invalid DEKID format: %v", err)
		return nil, api.NewSentinelWrapError("secrets.encryptor.decryptWithDEK", dberrors.ErrInvalidInput, interfaces.ErrInvalidKeyIdentifier, err)
	}

	// Get DEK material
	rawKey, _, err := e.dek.GetKey(ctx, dekid)
	if err != nil {
		e.logger.Errorf("Failed to get DEK: %v", err)
		return nil, api.NewDEKRetrievalError("secrets.encryptor.decryptWithDEK", err)
	}

	// Copy key material so zeroing does not corrupt the provider's internal state
	key := copyKey(rawKey)
	defer xkms.SecureMemZero(key)

	// Perform AEAD decryption
	plaintext, err := decryptAEAD(key, blob.Nonce, blob.Ciphertext, blob.Tag, blob.AAD, blob.Algorithm)
	if err != nil {
		e.logger.Errorf("AEAD decryption failed: %v", err)
		return nil, api.NewSentinelWrapError("secrets.encryptor.decryptWithDEK", dberrors.ErrDecryption, interfaces.ErrDecryptionFailed, err)
	}

	return plaintext, nil
}

// decryptWithKMS delegates decryption to the KMS manager.
func (e *secretsEncryptor) decryptWithKMS(ctx context.Context, blob *interfaces.EncryptedBlob) ([]byte, error) {
	plaintext, err := e.kms.Decrypt(ctx, blob.KeyIdentifier, blob.Ciphertext, blob.AAD)
	if err != nil {
		e.logger.Errorf("KMS decryption failed: %v", err)
		return nil, api.NewSentinelWrapError("secrets.encryptor.decryptWithKMS", dberrors.ErrDecryption, interfaces.ErrDecryptionFailed, err)
	}

	return plaintext, nil
}

// ValidateKeyIdentifier validates that the key identifier has correct format
// and that the referenced key exists and is accessible.
func (e *secretsEncryptor) ValidateKeyIdentifier(ctx context.Context, keyIdentifier string) error {
	if keyIdentifier == "" {
		return interfaces.ErrInvalidKeyIdentifier
	}

	// Route based on identifier type
	if IsDEKID(keyIdentifier) {
		dekid, err := ParseDEKID(keyIdentifier)
		if err != nil {
			return api.NewSentinelWrapError("secrets.encryptor.ValidateKeyIdentifier", dberrors.ErrInvalidInput, interfaces.ErrInvalidKeyIdentifier, err)
		}

		return e.dek.Validate(ctx, dekid)
	}

	// For KMS keys, check if key exists
	_, err := e.kms.GetKey(ctx, keyIdentifier)
	if err != nil {
		return err
	}

	return nil
}

// encryptAEAD performs AEAD encryption with the given key.
func encryptAEAD(key []byte, plaintext, aad []byte, algorithm string) (nonce, ciphertext, tag []byte, err error) {
	switch algorithm {
	case interfaces.AlgorithmAES256GCM:
		// Use xkms aesgcm package for AES-256-GCM
		sealed, encErr := xkms.AESGCMEncryptWithAAD(key, plaintext, aad)
		if encErr != nil {
			return nil, nil, nil, api.NewCryptoOperationError("secrets.encryptor.encryptAEAD", "AES-256-GCM encryption failed", encErr)
		}

		// Split xkms output [nonce:12][ciphertext+tag:16] into separate components
		nonce = sealed[:xkms.AESGCMNonceSize]
		sealedBody := sealed[xkms.AESGCMNonceSize:]

		// Split ciphertext and tag (tag is last 16 bytes)
		if len(sealedBody) < xkms.AESGCMTagSize {
			ciphertext = []byte{}
			tag = sealedBody
		} else {
			ciphertext = sealedBody[:len(sealedBody)-xkms.AESGCMTagSize]
			tag = sealedBody[len(sealedBody)-xkms.AESGCMTagSize:]
		}

		return nonce, ciphertext, tag, nil

	case interfaces.AlgorithmAES128GCM:
		// AES-128-GCM uses raw crypto (xkms aesgcm only supports AES-256)
		return encryptAES128GCMAEAD(key, plaintext, aad)

	case interfaces.AlgorithmChaCha20Poly1305:
		// Create ChaCha20-Poly1305 cipher
		aead, chachaErr := chacha20poly1305.New(key)
		if chachaErr != nil {
			return nil, nil, nil, api.NewCryptoOperationError("secrets.encryptor.encryptAEAD", "failed to create ChaCha20-Poly1305", chachaErr)
		}

		// Generate random nonce
		nonceSize := aead.NonceSize()
		nonce = make([]byte, nonceSize)
		if _, randErr := rand.Read(nonce); randErr != nil {
			return nil, nil, nil, api.NewCryptoOperationError("secrets.encryptor.encryptAEAD", "failed to generate nonce", randErr)
		}

		// Encrypt and authenticate
		sealedData := aead.Seal(nil, nonce, plaintext, aad)

		// Split ciphertext and tag (tag is last 16 bytes)
		tagSize := 16
		if len(sealedData) < tagSize {
			ciphertext = []byte{}
			tag = sealedData
		} else {
			ciphertext = sealedData[:len(sealedData)-tagSize]
			tag = sealedData[len(sealedData)-tagSize:]
		}

		return nonce, ciphertext, tag, nil

	default:
		return nil, nil, nil, interfaces.ErrUnsupportedAlgorithm
	}
}

// encryptAES128GCMAEAD performs AES-128-GCM encryption using raw crypto primitives.
// The xkms aesgcm package only supports AES-256, so AES-128 uses standard library.
func encryptAES128GCMAEAD(key []byte, plaintext, aad []byte) (nonce, ciphertext, tag []byte, err error) {
	block, blockErr := aes.NewCipher(key)
	if blockErr != nil {
		return nil, nil, nil, api.NewCryptoOperationError("secrets.encryptor.encryptAEAD", "failed to create AES cipher", blockErr)
	}

	gcm, gcmErr := cipher.NewGCM(block)
	if gcmErr != nil {
		return nil, nil, nil, api.NewCryptoOperationError("secrets.encryptor.encryptAEAD", "failed to create GCM", gcmErr)
	}

	// Generate random nonce
	nonceSize := gcm.NonceSize()
	nonce = make([]byte, nonceSize)
	if _, randErr := rand.Read(nonce); randErr != nil {
		return nil, nil, nil, api.NewCryptoOperationError("secrets.encryptor.encryptAEAD", "failed to generate nonce", randErr)
	}

	// Encrypt and authenticate
	sealed := gcm.Seal(nil, nonce, plaintext, aad)

	// Split ciphertext and tag (tag is last 16 bytes)
	tagSize := 16
	if len(sealed) < tagSize {
		ciphertext = []byte{}
		tag = sealed
	} else {
		ciphertext = sealed[:len(sealed)-tagSize]
		tag = sealed[len(sealed)-tagSize:]
	}

	return nonce, ciphertext, tag, nil
}

// decryptAEAD performs AEAD decryption with the given key.
func decryptAEAD(key []byte, nonce, ciphertext, tag, aad []byte, algorithm string) ([]byte, error) {
	switch algorithm {
	case interfaces.AlgorithmAES256GCM:
		// Reconstruct xkms wire format: [nonce:12][ciphertext+tag]
		sealed := make([]byte, len(nonce)+len(ciphertext)+len(tag))
		copy(sealed, nonce)
		copy(sealed[len(nonce):], ciphertext)
		copy(sealed[len(nonce)+len(ciphertext):], tag)

		plaintext, decErr := xkms.AESGCMDecryptWithAAD(key, sealed, aad)
		if decErr != nil {
			return nil, api.NewAuthenticationFailedError("secrets.encryptor.decryptAEAD", decErr)
		}

		return plaintext, nil

	case interfaces.AlgorithmAES128GCM:
		// AES-128-GCM uses raw crypto (xkms aesgcm only supports AES-256)
		return decryptAES128GCMAEAD(key, nonce, ciphertext, tag, aad)

	case interfaces.AlgorithmChaCha20Poly1305:
		// Create ChaCha20-Poly1305 cipher
		aead, chachaErr := chacha20poly1305.New(key)
		if chachaErr != nil {
			return nil, api.NewCryptoOperationError("secrets.encryptor.decryptAEAD", "failed to create ChaCha20-Poly1305", chachaErr)
		}

		// Reconstruct sealed data (ciphertext || tag)
		sealedData := make([]byte, len(ciphertext)+len(tag))
		copy(sealedData, ciphertext)
		copy(sealedData[len(ciphertext):], tag)

		// Decrypt and verify
		plaintext, openErr := aead.Open(nil, nonce, sealedData, aad)
		if openErr != nil {
			return nil, api.NewAuthenticationFailedError("secrets.encryptor.decryptAEAD", openErr)
		}

		return plaintext, nil

	default:
		return nil, interfaces.ErrUnsupportedAlgorithm
	}
}

// decryptAES128GCMAEAD performs AES-128-GCM decryption using raw crypto primitives.
// The xkms aesgcm package only supports AES-256, so AES-128 uses standard library.
func decryptAES128GCMAEAD(key []byte, nonce, ciphertext, tag, aad []byte) ([]byte, error) {
	block, blockErr := aes.NewCipher(key)
	if blockErr != nil {
		return nil, api.NewCryptoOperationError("secrets.encryptor.decryptAEAD", "failed to create AES cipher", blockErr)
	}

	gcm, gcmErr := cipher.NewGCM(block)
	if gcmErr != nil {
		return nil, api.NewCryptoOperationError("secrets.encryptor.decryptAEAD", "failed to create GCM", gcmErr)
	}

	// Reconstruct sealed data (ciphertext || tag)
	sealed := make([]byte, len(ciphertext)+len(tag))
	copy(sealed, ciphertext)
	copy(sealed[len(ciphertext):], tag)

	// Decrypt and verify
	plaintext, openErr := gcm.Open(nil, nonce, sealed, aad)
	if openErr != nil {
		return nil, api.NewAuthenticationFailedError("secrets.encryptor.decryptAEAD", openErr)
	}

	return plaintext, nil
}

// getKeySizeForAlgorithm returns the required key size in bytes for an algorithm.
func getKeySizeForAlgorithm(algorithm string) int {
	switch algorithm {
	case interfaces.AlgorithmAES256GCM:
		return 32 // 256 bits
	case interfaces.AlgorithmAES128GCM:
		return 16 // 128 bits
	case interfaces.AlgorithmChaCha20Poly1305:
		return 32 // 256 bits
	default:
		return 0
	}
}

// getNonceSizeForAlgorithm returns the required nonce size in bytes for an algorithm.
func getNonceSizeForAlgorithm(algorithm string) int {
	switch algorithm {
	case interfaces.AlgorithmAES256GCM, interfaces.AlgorithmAES128GCM:
		return 12 // 96 bits (standard for GCM)
	case interfaces.AlgorithmChaCha20Poly1305:
		return 12 // 96 bits (standard ChaCha20-Poly1305)
	default:
		return 0
	}
}
