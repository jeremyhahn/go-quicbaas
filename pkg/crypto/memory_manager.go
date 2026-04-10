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

package crypto

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	stderrors "errors"
	"hash"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// MemoryCryptoManager implements interfaces.CryptoManager using an in-memory KMS backend.
// It provides a simple implementation for development and testing.
type MemoryCryptoManager struct {
	kmsManager interfaces.KMSManager
}

// NewMemoryCryptoManager creates a new in-memory crypto manager.
func NewMemoryCryptoManager(kmsManager interfaces.KMSManager) *MemoryCryptoManager {
	return &MemoryCryptoManager{
		kmsManager: kmsManager,
	}
}

// Encrypt encrypts plaintext using the named key.
func (m *MemoryCryptoManager) Encrypt(ctx context.Context, keyID string, plaintext, encContext []byte) ([]byte, error) {
	if keyID == "" {
		return nil, errors.InvalidInput("MemoryCryptoManager.Encrypt", "keyID is required")
	}

	return m.kmsManager.Encrypt(ctx, keyID, plaintext, encContext)
}

// Decrypt decrypts ciphertext using the named key.
func (m *MemoryCryptoManager) Decrypt(ctx context.Context, keyID string, ciphertext, encContext []byte) ([]byte, error) {
	if keyID == "" {
		return nil, errors.InvalidInput("MemoryCryptoManager.Decrypt", "keyID is required")
	}

	return m.kmsManager.Decrypt(ctx, keyID, ciphertext, encContext)
}

// Rewrap re-encrypts ciphertext with the latest key version.
func (m *MemoryCryptoManager) Rewrap(ctx context.Context, keyID string, ciphertext, encContext []byte) ([]byte, error) {
	// Decrypt with current version
	plaintext, err := m.kmsManager.Decrypt(ctx, keyID, ciphertext, encContext)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrInternal, "MemoryCryptoManager.Rewrap", "failed to decrypt")
	}

	// Re-encrypt with latest version
	newCiphertext, err := m.kmsManager.Encrypt(ctx, keyID, plaintext, encContext)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrInternal, "MemoryCryptoManager.Rewrap", "failed to re-encrypt")
	}

	return newCiphertext, nil
}

// EncryptBatch encrypts multiple plaintexts in a single call.
func (m *MemoryCryptoManager) EncryptBatch(ctx context.Context, keyID string, items [][]byte, encContext []byte) ([][]byte, error) {
	if keyID == "" {
		return nil, errors.InvalidInput("MemoryCryptoManager.EncryptBatch", "keyID is required")
	}

	results := make([][]byte, len(items))
	for i, plaintext := range items {
		ciphertext, err := m.kmsManager.Encrypt(ctx, keyID, plaintext, encContext)
		if err != nil {
			return nil, errors.Wrapf(err, errors.ErrInternal, "MemoryCryptoManager.EncryptBatch", "failed to encrypt item %d", i)
		}
		results[i] = ciphertext
	}

	return results, nil
}

// DecryptBatch decrypts multiple ciphertexts in a single call.
func (m *MemoryCryptoManager) DecryptBatch(ctx context.Context, keyID string, items [][]byte, encContext []byte) ([][]byte, error) {
	if keyID == "" {
		return nil, errors.InvalidInput("MemoryCryptoManager.DecryptBatch", "keyID is required")
	}

	results := make([][]byte, len(items))
	for i, ciphertext := range items {
		plaintext, err := m.kmsManager.Decrypt(ctx, keyID, ciphertext, encContext)
		if err != nil {
			return nil, errors.Wrapf(err, errors.ErrInternal, "MemoryCryptoManager.DecryptBatch", "failed to decrypt item %d", i)
		}
		results[i] = plaintext
	}

	return results, nil
}

// Sign creates a signature of the input data.
// When preHashed is true, input is already a hash digest and should not be hashed again.
func (m *MemoryCryptoManager) Sign(ctx context.Context, keyID string, input []byte, hashAlgorithm string, preHashed bool) ([]byte, error) {
	if keyID == "" {
		return nil, errors.InvalidInput("MemoryCryptoManager.Sign", "keyID is required")
	}

	var digest []byte
	if preHashed {
		// Input is already a hash digest
		digest = input
	} else {
		// Hash the input first
		var err error
		digest, err = m.Hash(ctx, input, hashAlgorithm)
		if err != nil {
			return nil, err
		}
	}

	return m.kmsManager.Sign(ctx, keyID, digest, hashAlgorithm)
}

// Verify validates a signature.
func (m *MemoryCryptoManager) Verify(ctx context.Context, keyID string, input, signature []byte, hashAlgorithm string) (bool, error) {
	if keyID == "" {
		return false, errors.InvalidInput("MemoryCryptoManager.Verify", "keyID is required")
	}

	// Hash the input first
	digest, err := m.Hash(ctx, input, hashAlgorithm)
	if err != nil {
		return false, err
	}

	return m.kmsManager.Verify(ctx, keyID, digest, signature, hashAlgorithm)
}

// GenerateHMAC generates an HMAC of the input.
func (m *MemoryCryptoManager) GenerateHMAC(ctx context.Context, keyID string, input []byte, algorithm string) ([]byte, error) {
	if keyID == "" {
		return nil, errors.InvalidInput("MemoryCryptoManager.GenerateHMAC", "keyID is required")
	}

	// Get key from KMS for HMAC
	// For the memory backend, we generate a deterministic HMAC key from the key ID
	kekAttrs := &xkmssdk.KeyAttributes{CN: keyID}
	plaintext, _, err := m.kmsManager.GenerateDataKey(ctx, kekAttrs, 32)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrInternal, "MemoryCryptoManager.GenerateHMAC", "failed to get key material")
	}

	var h func() hash.Hash
	switch algorithm {
	case "sha256", "SHA256", "":
		h = sha256.New
	case "sha512", "SHA512":
		h = sha512.New
	default:
		return nil, errors.InvalidInputf("MemoryCryptoManager.GenerateHMAC", "unsupported algorithm: %s", algorithm)
	}

	mac := hmac.New(h, plaintext)
	mac.Write(input)
	return mac.Sum(nil), nil
}

// VerifyHMAC validates an HMAC.
func (m *MemoryCryptoManager) VerifyHMAC(ctx context.Context, keyID string, input, hmacValue []byte, algorithm string) (bool, error) {
	expectedHMAC, err := m.GenerateHMAC(ctx, keyID, input, algorithm)
	if err != nil {
		return false, err
	}

	return hmac.Equal(expectedHMAC, hmacValue), nil
}

// GenerateRandom generates cryptographically secure random bytes.
func (m *MemoryCryptoManager) GenerateRandom(ctx context.Context, numBytes int) ([]byte, error) {
	if numBytes <= 0 {
		return nil, errors.InvalidInput("MemoryCryptoManager.GenerateRandom", "numBytes must be positive")
	}

	if numBytes > 1024*1024 { // 1MB limit
		return nil, errors.InvalidInput("MemoryCryptoManager.GenerateRandom", "numBytes exceeds maximum allowed (1MB)")
	}

	randomBytes, err := dbxkms.Rand(numBytes)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrInternal, "MemoryCryptoManager.GenerateRandom", "failed to generate random bytes")
	}

	return randomBytes, nil
}

// Hash computes a cryptographic hash of the input.
func (m *MemoryCryptoManager) Hash(ctx context.Context, input []byte, algorithm string) ([]byte, error) {
	var h hash.Hash

	switch algorithm {
	case "sha256", "SHA256", "":
		h = sha256.New()
	case "sha512", "SHA512":
		h = sha512.New()
	default:
		return nil, errors.InvalidInputf("MemoryCryptoManager.Hash", "unsupported hash algorithm: %s", algorithm)
	}

	h.Write(input)
	return h.Sum(nil), nil
}

// Close releases any resources held by the crypto manager.
func (m *MemoryCryptoManager) Close() error {
	return nil
}

// Ensure MemoryCryptoManager implements interfaces.CryptoManager.
var _ interfaces.CryptoManager = (*MemoryCryptoManager)(nil)

// CreateDefaultManagers creates default KMS, Secrets, and Crypto managers for development/testing.
// This is a convenience function for server initialization when no external backends are configured.
func CreateDefaultManagers(log interfaces.Logger) (interfaces.KMSManager, interfaces.SecretsManager, interfaces.CryptoManager, error) {
	// Create in-memory storage backend for go-xkms
	memStorage, err := xkmssdk.NewMemoryBackend()
	if err != nil {
		return nil, nil, nil, errors.Wrapf(err, errors.ErrInternal, "crypto.CreateDefaultManagers", "failed to create memory storage")
	}

	// Create software backend with in-memory storage
	softwareBackend, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{
		KeyStorage: memStorage,
	})
	if err != nil {
		return nil, nil, nil, errors.Wrapf(err, errors.ErrInternal, "crypto.CreateDefaultManagers", "failed to create software backend")
	}

	// Create KeyStore by wrapping backend with cert storage
	keyStore, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     softwareBackend,
		CertStorage: memStorage,
	})
	if err != nil {
		return nil, nil, nil, errors.Wrapf(err, errors.ErrInternal, "crypto.CreateDefaultManagers", "failed to create keystore")
	}

	// Initialize XKMS with the software backend
	if err := dbxkms.Initialize(&dbxkms.Config{
		DefaultBackend: "software",
		Backends: map[string]xkmssdk.ServiceBackend{
			"software": keyStore,
		},
		Logger: log,
	}); err != nil {
		// If already initialized (e.g., in tests), continue
		if !stderrors.Is(err, dbxkms.ErrAlreadyInitialized) {
			return nil, nil, nil, errors.Wrapf(err, errors.ErrInternal, "crypto.CreateDefaultManagers", "failed to initialize XKMS")
		}
	}

	// Create KMS manager (uses XKMS internally)
	kmsManager, err := kms.NewKMSService(&kms.KMSServiceOptions{
		Logger: log,
	})
	if err != nil {
		return nil, nil, nil, errors.Wrapf(err, errors.ErrInternal, "crypto.CreateDefaultManagers", "failed to create KMS manager")
	}

	// Create crypto manager using the KMS manager
	cryptoManager := NewMemoryCryptoManager(kmsManager)

	// Note: SecretsManager should be created separately using secrets.NewMemoryBackend()
	// We return nil for SecretsManager here - the caller should create it if needed
	return kmsManager, nil, cryptoManager, nil
}
