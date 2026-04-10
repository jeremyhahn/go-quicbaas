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

// Package service provides the unified service facade for QuicBaaS.
package service

import (
	"context"
)

// =============================================================================
// Crypto Service - Encryption as a Service
// =============================================================================

// CryptoService handles encryption/decryption as a service (crypto encryption).
// This service provides cryptographic operations without exposing key material,
// similar to HashiCorp Vault's crypto secrets engine.
//
// Key features:
// - Symmetric encryption/decryption with automatic key rotation
// - Envelope encryption with data key generation
// - Digital signatures and verification
// - HMAC generation and verification
// - Cryptographic hashing and random data generation
// - Batch operations for high throughput
//
// All operations maintain strong security guarantees:
// - Keys never leave the server
// - Automatic key versioning and rotation
// - Auditable cryptographic operations
// - Context-based access control.
type CryptoService interface {
	// ========================================================================
	// Symmetric Encryption Operations
	// ========================================================================

	// Encrypt encrypts plaintext data using a named encryption key.
	// The operation uses authenticated encryption (AEAD) to ensure both
	// confidentiality and integrity of the data.
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Encrypt request with key name, plaintext, optional context
	// Returns:
	//   - EncryptResponse: Contains ciphertext, key version used
	//   - error: ErrInvalidInput, ErrKeyNotFound, ErrTimeout
	Encrypt(ctx context.Context, req *EncryptRequest) (*EncryptResponse, error)

	// Decrypt decrypts ciphertext that was encrypted with a named key.
	// The operation automatically uses the correct key version based on
	// metadata in the ciphertext.
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Decrypt request with key name, ciphertext, optional context
	// Returns:
	//   - DecryptResponse: Contains plaintext, key version used
	//   - error: ErrInvalidInput, ErrKeyNotFound, ErrDecryptFailed, ErrTimeout
	Decrypt(ctx context.Context, req *DecryptRequest) (*DecryptResponse, error)

	// Rewrap re-encrypts ciphertext with the latest version of the key.
	// This is useful for key rotation without decrypting on the client.
	// The operation is atomic and maintains data integrity.
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Rewrap request with key name, old ciphertext
	// Returns:
	//   - RewrapResponse: Contains new ciphertext with latest key version
	//   - error: ErrInvalidInput, ErrKeyNotFound, ErrTimeout
	Rewrap(ctx context.Context, req *RewrapRequest) (*RewrapResponse, error)

	// ========================================================================
	// Batch Operations
	// ========================================================================

	// EncryptBatch performs multiple encrypt operations in a single request.
	// This provides higher throughput for bulk encryption operations.
	// Operations are processed independently; partial failures are possible.
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Batch encrypt request with multiple encrypt operations
	// Returns:
	//   - EncryptBatchResponse: Contains responses for each operation
	//   - error: ErrInvalidInput, ErrTimeout
	EncryptBatch(ctx context.Context, req *EncryptBatchRequest) (*EncryptBatchResponse, error)

	// DecryptBatch performs multiple decrypt operations in a single request.
	// This provides higher throughput for bulk decryption operations.
	// Operations are processed independently; partial failures are possible.
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Batch decrypt request with multiple decrypt operations
	// Returns:
	//   - DecryptBatchResponse: Contains responses for each operation
	//   - error: ErrInvalidInput, ErrTimeout
	DecryptBatch(ctx context.Context, req *DecryptBatchRequest) (*DecryptBatchResponse, error)

	// ========================================================================
	// Data Key Generation (Envelope Encryption)
	// ========================================================================

	// GenerateDataKey generates a new data encryption key (DEK) for envelope
	// encryption patterns. Returns both plaintext and encrypted versions of
	// the DEK. The plaintext version is used to encrypt data, while the
	// encrypted version is stored alongside the encrypted data.
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Generate data key request with key name, key size, context
	// Returns:
	//   - GenerateDataKeyResponse: Contains plaintext and ciphertext DEK
	//   - error: ErrInvalidInput, ErrKeyNotFound, ErrTimeout
	GenerateDataKey(ctx context.Context, req *GenerateDataKeyRequest) (*GenerateDataKeyResponse, error)

	// GenerateWrappedDataKey generates a new encrypted data encryption key
	// without returning the plaintext version. This is useful when you need
	// to pre-generate encrypted DEKs for later use.
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Generate wrapped data key request with key name, key size
	// Returns:
	//   - GenerateWrappedDataKeyResponse: Contains only ciphertext DEK
	//   - error: ErrInvalidInput, ErrKeyNotFound, ErrTimeout
	GenerateWrappedDataKey(ctx context.Context, req *GenerateWrappedDataKeyRequest) (*GenerateWrappedDataKeyResponse, error)

	// ========================================================================
	// Signing and Verification Operations
	// ========================================================================

	// Sign creates a digital signature of data using a named signing key.
	// Supports various signature algorithms (RSA, ECDSA, Ed25519).
	// The signature proves the data originated from the key holder.
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Sign request with key name, data, algorithm, hash function
	// Returns:
	//   - SignResponse: Contains signature, key version used
	//   - error: ErrInvalidInput, ErrKeyNotFound, ErrTimeout
	Sign(ctx context.Context, req *SignRequest) (*SignResponse, error)

	// Verify verifies a digital signature against data using a named key.
	// Validates that the signature was created by the specified key.
	// Does not require knowing which key version was used.
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Verify request with key name, data, signature
	// Returns:
	//   - VerifyResponse: Contains valid flag, key version used
	//   - error: ErrInvalidInput, ErrKeyNotFound, ErrTimeout
	Verify(ctx context.Context, req *VerifyRequest) (*VerifyResponse, error)

	// ========================================================================
	// HMAC Operations
	// ========================================================================

	// GenerateHMAC generates an HMAC (Hash-based Message Authentication Code)
	// of data using a named key. HMACs provide data integrity and authenticity
	// guarantees using symmetric keys.
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: HMAC request with key name, data, hash algorithm
	// Returns:
	//   - HMACResponse: Contains HMAC value, key version used
	//   - error: ErrInvalidInput, ErrKeyNotFound, ErrTimeout
	GenerateHMAC(ctx context.Context, req *HMACRequest) (*HMACResponse, error)

	// VerifyHMAC verifies an HMAC against data using a named key.
	// Validates that the HMAC was generated with the specified key.
	// Uses constant-time comparison to prevent timing attacks.
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Verify HMAC request with key name, data, HMAC value
	// Returns:
	//   - VerifyHMACResponse: Contains valid flag, key version used
	//   - error: ErrInvalidInput, ErrKeyNotFound, ErrTimeout
	VerifyHMAC(ctx context.Context, req *VerifyHMACRequest) (*VerifyHMACResponse, error)

	// ========================================================================
	// Utility Operations
	// ========================================================================

	// GenerateRandom generates high-quality random bytes.
	// Uses a cryptographically secure random number generator (CSRNG).
	// Suitable for key generation, nonces, and other security-critical uses.
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Generate random request with byte count, format
	// Returns:
	//   - GenerateRandomResponse: Contains random bytes (base64 or hex)
	//   - error: ErrInvalidInput, ErrTimeout
	GenerateRandom(ctx context.Context, req *GenerateRandomRequest) (*GenerateRandomResponse, error)

	// Hash computes a cryptographic hash of data.
	// Supports various hash algorithms (SHA-256, SHA-512, SHA3, etc.).
	// Does not use any key material; purely a hash function.
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Hash request with data, algorithm
	// Returns:
	//   - HashResponse: Contains hash value, algorithm used
	//   - error: ErrInvalidInput, ErrTimeout
	Hash(ctx context.Context, req *HashRequest) (*HashResponse, error)
}
