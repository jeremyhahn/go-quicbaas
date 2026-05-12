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

import "context"

// =============================================================================
// Seal Service - Database Seal/Unseal Operations
// =============================================================================

// SealService defines database seal/unseal operations for secure initialization,
// access control, and emergency lockdown. This service manages the cryptographic
// sealing of the database to protect data at rest and control access.
//
// Seal/unseal operations follow HashiCorp Vault-style patterns:
// - Database starts sealed (encrypted, inaccessible)
// - Initialization generates master key split into key shares (Shamir's Secret Sharing)
// - Unsealing requires threshold number of key shares to reconstruct master key
// - Sealing immediately locks database and clears master key from memory
// - Rekey operation rotates master key while maintaining data access
// - Recovery keys provide emergency access mechanism
// - Root token generation for initial admin access.
type SealService interface {
	// Status returns the current seal status of the database.
	// This is a non-privileged operation that can be called without authentication.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	// Returns:
	//   - SealStatusResponse: Contains sealed state, initialization state, thresholds
	//   - error: ErrTimeout
	Status(ctx context.Context) (*SealStatusResponse, error)

	// Initialize performs first-time initialization of the database seal.
	// This can only be called once and creates the master key shares.
	// Uses Shamir's Secret Sharing to split master key into N shares where
	// T shares are required to unseal (T <= N).
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Initialize request with secret shares and threshold configuration
	// Returns:
	//   - SealInitResponse: Contains key shares and initial root token
	//   - error: ErrAlreadyInitialized, ErrInvalidInput, ErrTimeout
	Initialize(ctx context.Context, req *SealInitRequest) (*SealInitResponse, error)

	// Unseal provides one key share to unseal the database.
	// Must be called T times with different key shares to fully unseal.
	// Each call returns progress toward unsealing threshold.
	// When threshold is reached, master key is reconstructed and database becomes accessible.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Unseal request with single key share
	// Returns:
	//   - UnsealResponse: Contains progress, sealed state, threshold info
	//   - error: ErrInvalidInput, ErrNotInitialized, ErrTimeout
	Unseal(ctx context.Context, req *UnsealRequest) (*UnsealResponse, error)

	// Seal immediately seals the database, making it inaccessible.
	// This clears the master key from memory and requires unsealing to restore access.
	// Requires authenticated admin access via root token or equivalent.
	// Parameters:
	//   - ctx: Request context with timeout and metadata (includes auth token)
	// Returns:
	//   - error: ErrUnauthorized, ErrTimeout
	Seal(ctx context.Context) error

	// Rekey rotates the master key by generating new key shares.
	// This is a multi-step process similar to unsealing:
	//   1. Initialize rekey with new shares/threshold configuration
	//   2. Provide old key shares to authorize rekey
	//   3. Receive new key shares when threshold is reached
	// Data remains accessible during rekey; only key shares change.
	// Parameters:
	//   - ctx: Request context with timeout and metadata (includes auth token)
	//   - req: Rekey request with configuration or key share
	// Returns:
	//   - RekeyResponse: Contains rekey progress and new key shares when complete
	//   - error: ErrUnauthorized, ErrInvalidInput, ErrNotInitialized, ErrTimeout
	Rekey(ctx context.Context, req *RekeyRequest) (*RekeyResponse, error)

	// GenerateRecoveryKey creates a new recovery key for emergency access.
	// Recovery keys provide an alternative unsealing mechanism if key shares are lost.
	// Requires authenticated admin access and existing key shares for authorization.
	// Parameters:
	//   - ctx: Request context with timeout and metadata (includes auth token)
	//   - req: Generate recovery key request with authorization shares
	// Returns:
	//   - GenerateRecoveryKeyResponse: Contains encrypted recovery key
	//   - error: ErrUnauthorized, ErrInvalidInput, ErrTimeout
	GenerateRecoveryKey(ctx context.Context, req *GenerateRecoveryKeyRequest) (*GenerateRecoveryKeyResponse, error)

	// DeleteRecoveryKey removes a recovery key from the system.
	// Requires authenticated admin access.
	// Parameters:
	//   - ctx: Request context with timeout and metadata (includes auth token)
	//   - req: Delete recovery key request with key ID
	// Returns:
	//   - DeleteRecoveryKeyResponse: Contains deletion status
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	DeleteRecoveryKey(ctx context.Context, req *DeleteRecoveryKeyRequest) (*DeleteRecoveryKeyResponse, error)

	// GenerateRootToken creates a new root token for admin access.
	// This is a multi-step process requiring multiple key shares:
	//   1. Initialize root token generation
	//   2. Provide key shares to authorize generation
	//   3. Receive new root token when threshold is reached
	// Used to recover admin access or create additional root tokens.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Generate root token request with OTP and key share
	// Returns:
	//   - GenerateRootTokenResponse: Contains root token when complete
	//   - error: ErrUnauthorized, ErrInvalidInput, ErrTimeout
	GenerateRootToken(ctx context.Context, req *GenerateRootTokenRequest) (*GenerateRootTokenResponse, error)

	// RotateRootKey rotates the root encryption key used to protect master key shares.
	// This re-encrypts all protected data under a newly generated root key.
	// Requires admin authorization (root token or equivalent).
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Rotate root key request with optional force flag
	// Returns:
	//   - RotateRootKeyResponse: Confirms rotation success
	//   - error: ErrUnauthorized, ErrTimeout, ErrUnavailable
	RotateRootKey(ctx context.Context, req *RotateRootKeyRequest) (*RotateRootKeyResponse, error)
}

// =============================================================================
// Seal Service Request/Response Types
// =============================================================================
// Note: Type definitions are in seal_types.go to keep interface focused.
// See seal_types.go for:
// - SealStatusResponse
// - SealInitRequest/Response
// - UnsealRequest/Response
// - RekeyRequest/Response
// - GenerateRecoveryKeyRequest/Response
// - DeleteRecoveryKeyRequest/Response
// - GenerateRootTokenRequest/Response
// =============================================================================
