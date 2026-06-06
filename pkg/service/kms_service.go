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
// KMS Service - Cryptographic Key Management
// =============================================================================

// KMSService handles cryptographic key lifecycle management.
// This service provides operations for generating, importing, rotating, and
// managing cryptographic keys with access control and audit logging.
type KMSService interface {
	// GetSupportedAlgorithms returns the list of cryptographic algorithms
	// supported by the current backend. Use this to check if specific algorithms
	// like ChaCha20-Poly1305 are available before attempting operations.
	GetSupportedAlgorithms(ctx context.Context) ([]string, error)

	// GenerateKey creates a new cryptographic key with specified parameters.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Generate key request with algorithm, size, and metadata
	// Returns:
	//   - GenerateKeyResponse: Contains key ID, version, and creation timestamp
	//   - error: ErrInvalidInput, ErrUnauthorized, ErrTimeout
	GenerateKey(ctx context.Context, req *GenerateKeyRequest) (*GenerateKeyResponse, error)

	// ImportKey imports an externally generated key into the KMS.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Import key request with key material and metadata
	// Returns:
	//   - ImportKeyResponse: Contains key ID, version, and import timestamp
	//   - error: ErrInvalidInput, ErrUnauthorized, ErrTimeout
	ImportKey(ctx context.Context, req *ImportKeyRequest) (*ImportKeyResponse, error)

	// GetKey retrieves key material or public key by key ID.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get key request with key ID and version (optional)
	// Returns:
	//   - GetKeyResponse: Contains key material, metadata, and status
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	GetKey(ctx context.Context, req *GetKeyRequest) (*GetKeyResponse, error)

	// GetPublicKey retrieves the public key component of an asymmetric key.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get public key request with key ID and version (optional)
	// Returns:
	//   - GetPublicKeyResponse: Contains DER-encoded public key and algorithm
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout, ErrInvalidInput (if symmetric key)
	GetPublicKey(ctx context.Context, req *GetPublicKeyRequest) (*GetPublicKeyResponse, error)

	// ListKeys returns all keys accessible to the requester with pagination.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List keys request with optional filters and pagination
	// Returns:
	//   - ListKeysResponse: Contains key metadata list and pagination token
	//   - error: ErrUnauthorized, ErrTimeout
	ListKeys(ctx context.Context, req *ListKeysRequest) (*ListKeysResponse, error)

	// RotateKey creates a new version of an existing key.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Rotate key request with key ID and rotation policy
	// Returns:
	//   - RotateKeyResponse: Contains new version and rotation timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	RotateKey(ctx context.Context, req *RotateKeyRequest) (*RotateKeyResponse, error)

	// RevokeKey marks a key version as revoked and unusable.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Revoke key request with key ID, version, and reason
	// Returns:
	//   - RevokeKeyResponse: Contains revocation status and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	RevokeKey(ctx context.Context, req *RevokeKeyRequest) (*RevokeKeyResponse, error)

	// DeleteKey permanently removes a key and all its versions.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Delete key request with key ID
	// Returns:
	//   - DeleteKeyResponse: Contains deletion status and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	DeleteKey(ctx context.Context, req *DeleteKeyRequest) (*DeleteKeyResponse, error)

	// GetKeyMetadata retrieves metadata for a key without exposing key material.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get key metadata request with key ID
	// Returns:
	//   - GetKeyMetadataResponse: Contains algorithm, status, timestamps, and tags
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	GetKeyMetadata(ctx context.Context, req *GetKeyMetadataRequest) (*GetKeyMetadataResponse, error)

	// UpdateKeyMetadata updates mutable metadata fields for a key.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Update key metadata request with key ID and new metadata
	// Returns:
	//   - UpdateKeyMetadataResponse: Contains updated metadata and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	UpdateKeyMetadata(ctx context.Context, req *UpdateKeyMetadataRequest) (*UpdateKeyMetadataResponse, error)

	// UpdateKeyStatus enables or disables a key or key version.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Update key status request with key ID, version, and new status
	// Returns:
	//   - UpdateKeyStatusResponse: Contains updated status and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrInvalidInput, ErrTimeout
	UpdateKeyStatus(ctx context.Context, req *UpdateKeyStatusRequest) (*UpdateKeyStatusResponse, error)

	// ListKeyVersions returns all versions of a key.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List key versions request with key ID and optional pagination
	// Returns:
	//   - ListKeyVersionsResponse: Contains list of key versions and pagination info
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	ListKeyVersions(ctx context.Context, req *ListKeyVersionsRequest) (*ListKeyVersionsResponse, error)

	// SetKeyPolicy sets the access control policy for a key.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Set key policy request with key ID and policy document
	// Returns:
	//   - SetKeyPolicyResponse: Contains updated policy and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrInvalidInput, ErrTimeout
	SetKeyPolicy(ctx context.Context, req *SetKeyPolicyRequest) (*SetKeyPolicyResponse, error)

	// GetKeyPolicy retrieves the access control policy for a key.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get key policy request with key ID
	// Returns:
	//   - GetKeyPolicyResponse: Contains policy document and version
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	GetKeyPolicy(ctx context.Context, req *GetKeyPolicyRequest) (*GetKeyPolicyResponse, error)

	// DeleteKeyPolicy removes the access control policy for a key, restoring the
	// default-allow-all behaviour for authenticated principals.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Delete key policy request with key ID
	// Returns:
	//   - DeleteKeyPolicyResponse: Confirms deletion with timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	DeleteKeyPolicy(ctx context.Context, req *DeleteKeyPolicyRequest) (*DeleteKeyPolicyResponse, error)

	// SetKeyRotationPolicy sets or replaces the automatic rotation policy for a key.
	// When AutoRotate is true, Interval must be positive (>0). NextRotation is
	// computed from Interval relative to now when not explicitly supplied.
	// This operation is gated by per-key authorization (key-management level).
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Set rotation policy request with key ID and policy
	// Returns:
	//   - SetKeyRotationPolicyResponse: Contains the stored policy with computed NextRotation
	//   - error: ErrNotFound, ErrInvalidInput (AutoRotate with zero Interval), ErrUnauthorized
	SetKeyRotationPolicy(ctx context.Context, req *SetKeyRotationPolicyRequest) (*SetKeyRotationPolicyResponse, error)

	// GetKeyRotationPolicy retrieves the current automatic rotation policy for a key.
	// Returns (policy=nil) when no rotation policy has been configured.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get rotation policy request with key ID
	// Returns:
	//   - GetKeyRotationPolicyResponse: Contains the current policy or nil
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	GetKeyRotationPolicy(ctx context.Context, req *GetKeyRotationPolicyRequest) (*GetKeyRotationPolicyResponse, error)

	// DeleteKeyRotationPolicy removes the rotation policy for a key, disabling
	// automatic rotation. The scheduler will no longer rotate the key.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Delete rotation policy request with key ID
	// Returns:
	//   - DeleteKeyRotationPolicyResponse: Confirms removal with timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	DeleteKeyRotationPolicy(ctx context.Context, req *DeleteKeyRotationPolicyRequest) (*DeleteKeyRotationPolicyResponse, error)

	// ==========================================================================
	// DEK (Data Encryption Key) Operations
	// ==========================================================================

	// CreateDEK creates a new data encryption key.
	CreateDEK(ctx context.Context, req *CreateDEKRequest) (*CreateDEKResponse, error)

	// GetDEK retrieves a data encryption key.
	GetDEK(ctx context.Context, req *GetDEKRequest) (*GetDEKResponse, error)

	// ListDEKs lists all data encryption keys.
	ListDEKs(ctx context.Context, req *ListDEKsRequest) (*ListDEKsResponse, error)

	// RotateDEK rotates a data encryption key.
	RotateDEK(ctx context.Context, req *RotateDEKRequest) (*RotateDEKResponse, error)

	// DeleteDEK deletes a data encryption key.
	DeleteDEK(ctx context.Context, req *DeleteDEKRequest) (*DeleteDEKResponse, error)

	// GetDEKStatus gets the status of a data encryption key.
	GetDEKStatus(ctx context.Context, req *GetDEKStatusRequest) (*GetDEKStatusResponse, error)

	// SetDefaultDEK sets the default data encryption key.
	SetDefaultDEK(ctx context.Context, req *SetDefaultDEKRequest) (*SetDefaultDEKResponse, error)

	// SetDEKSafety sets AEAD safety limits for a DEK.
	SetDEKSafety(ctx context.Context, req *SetDEKSafetyRequest) (*SetDEKSafetyResponse, error)

	// ClearDEKSafety clears AEAD safety limits for a DEK.
	ClearDEKSafety(ctx context.Context, req *ClearDEKSafetyRequest) (*ClearDEKSafetyResponse, error)

	// StartReencryption starts background re-encryption.
	StartReencryption(ctx context.Context, req *StartReencryptionRequest) (*StartReencryptionResponse, error)

	// GetReencryptionProgress gets the progress of re-encryption.
	GetReencryptionProgress(ctx context.Context, req *GetReencryptionProgressRequest) (*GetReencryptionProgressResponse, error)

	// ==========================================================================
	// Backend Operations
	// ==========================================================================

	// BackendStatus returns health and capability information about the KMS backend.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	// Returns:
	//   - BackendStatusResponse: Contains backend type, status, and capabilities
	//   - error: ErrTimeout if the backend is unreachable
	BackendStatus(ctx context.Context) (*BackendStatusResponse, error)

	// BackendTest performs a seal/unseal test cycle to verify backend functionality.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	// Returns:
	//   - BackendTestResponse: Contains success status, duration, and any errors
	//   - error: ErrTimeout if the test times out
	BackendTest(ctx context.Context) (*BackendTestResponse, error)

	// ==========================================================================
	// Key Export/Import Operations
	// ==========================================================================

	// ExportKey exports a key wrapped with a wrapping key.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Export key request with key ID and wrapping parameters
	// Returns:
	//   - ExportKeyResponse: Contains wrapped key material
	//   - error: ErrNotFound, ErrUnauthorized, ErrInvalidInput
	ExportKey(ctx context.Context, req *ExportKeyRequest) (*ExportKeyResponse, error)

	// UnwrapKey imports a wrapped key into the KMS.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Unwrap key request with wrapped material and unwrap parameters
	// Returns:
	//   - UnwrapKeyResponse: Contains new key handle and metadata
	//   - error: ErrInvalidInput, ErrUnauthorized
	UnwrapKey(ctx context.Context, req *UnwrapKeyRequest) (*UnwrapKeyResponse, error)

	// ==========================================================================
	// AEAD Safety Operations
	// ==========================================================================

	// SetKeySafety configures AEAD safety limits for a key.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Set key safety request with limits
	// Returns:
	//   - SetKeySafetyResponse: Confirmation of safety configuration
	//   - error: ErrNotFound, ErrInvalidInput
	SetKeySafety(ctx context.Context, req *SetKeySafetyRequest) (*SetKeySafetyResponse, error)

	// ClearKeySafety removes custom AEAD safety limits from a key.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Clear key safety request with key ID
	// Returns:
	//   - ClearKeySafetyResponse: Confirmation of safety removal
	//   - error: ErrNotFound
	ClearKeySafety(ctx context.Context, req *ClearKeySafetyRequest) (*ClearKeySafetyResponse, error)

	// SafetyCheck returns warnings for keys/DEKs approaching safety limits.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Safety check request with optional threshold
	// Returns:
	//   - SafetyCheckResponse: List of items exceeding threshold
	//   - error: ErrTimeout
	SafetyCheck(ctx context.Context, req *SafetyCheckRequest) (*SafetyCheckResponse, error)

	// SafetyReport returns a comprehensive safety report for all keys/DEKs.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Safety report request
	// Returns:
	//   - SafetyReportResponse: Complete safety report
	//   - error: ErrTimeout
	SafetyReport(ctx context.Context, req *SafetyReportRequest) (*SafetyReportResponse, error)

	// SetDefaultKey marks the named key as the system default.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Set default key request with key name
	// Returns:
	//   - SetDefaultKeyResponse: Contains name and is_default flag
	//   - error: ErrNotFound if key does not exist, ErrInvalidInput for empty name
	SetDefaultKey(ctx context.Context, req *SetDefaultKeyRequest) (*SetDefaultKeyResponse, error)
}
