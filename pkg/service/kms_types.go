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

// Package service provides high-level service types for KMS, Secrets, Crypto, and Seal operations.
// These types define the request/response contracts and enums used across the KMS service layer.
package service

import (
	"time"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"
)

// =============================================================================
// Key Algorithm Constants
// =============================================================================

// KeyAlgorithm specifies the cryptographic algorithm for a key.
type KeyAlgorithm string

const (
	// Symmetric Encryption Algorithms.
	KeyAlgAES128GCM         KeyAlgorithm = "AES-128-GCM"
	KeyAlgAES192GCM         KeyAlgorithm = "AES-192-GCM"
	KeyAlgAES256GCM         KeyAlgorithm = "AES-256-GCM"
	KeyAlgChaCha20Poly1305  KeyAlgorithm = "CHACHA20-POLY1305"
	KeyAlgXChaCha20Poly1305 KeyAlgorithm = "XCHACHA20-POLY1305"

	// RSA Algorithms.
	KeyAlgRSA2048 KeyAlgorithm = "RSA-2048"
	KeyAlgRSA3072 KeyAlgorithm = "RSA-3072"
	KeyAlgRSA4096 KeyAlgorithm = "RSA-4096"

	// ECDSA Algorithms.
	KeyAlgECDSAP256 KeyAlgorithm = "ECDSA-P256"
	KeyAlgECDSAP384 KeyAlgorithm = "ECDSA-P384"
	KeyAlgECDSAP521 KeyAlgorithm = "ECDSA-P521"

	// EdDSA Algorithms.
	KeyAlgEd25519 KeyAlgorithm = "ED25519"
	KeyAlgEd448   KeyAlgorithm = "ED448"

	// Key Exchange Algorithms.
	KeyAlgX25519 KeyAlgorithm = "X25519"
	KeyAlgX448   KeyAlgorithm = "X448"

	// HMAC Algorithms.
	KeyAlgHMACSHA256 KeyAlgorithm = "HMAC-SHA256"
	KeyAlgHMACSHA384 KeyAlgorithm = "HMAC-SHA384"
	KeyAlgHMACSHA512 KeyAlgorithm = "HMAC-SHA512"
)

// =============================================================================
// Key Usage Flags
// =============================================================================

// KeyUsage specifies permitted operations for a key.
type KeyUsage string

const (
	// KeyUsageEncrypt permits encryption operations.
	KeyUsageEncrypt KeyUsage = "encrypt"

	// KeyUsageDecrypt permits decryption operations.
	KeyUsageDecrypt KeyUsage = "decrypt"

	// KeyUsageSign permits signing operations.
	KeyUsageSign KeyUsage = "sign"

	// KeyUsageVerify permits signature verification.
	KeyUsageVerify KeyUsage = "verify"

	// KeyUsageWrapKey permits key wrapping/unwrapping.
	KeyUsageWrapKey KeyUsage = "wrap_key"

	// KeyUsageDeriveKey permits key derivation.
	KeyUsageDeriveKey KeyUsage = "derive_key"

	// KeyUsageGenerateMAC permits MAC generation.
	KeyUsageGenerateMAC KeyUsage = "generate_mac"

	// KeyUsageVerifyMAC permits MAC verification.
	KeyUsageVerifyMAC KeyUsage = "verify_mac"
)

// =============================================================================
// Key Status
// =============================================================================

// KeyStatus represents the lifecycle state of a key.
type KeyStatus string

const (
	// KeyStatusEnabled indicates the key is active and can be used.
	KeyStatusEnabled KeyStatus = "enabled"

	// KeyStatusDisabled indicates the key is temporarily disabled.
	KeyStatusDisabled KeyStatus = "disabled"

	// KeyStatusPendingDeletion indicates the key is scheduled for deletion.
	KeyStatusPendingDeletion KeyStatus = "pending_deletion"

	// KeyStatusDeleted indicates the key has been permanently deleted.
	KeyStatusDeleted KeyStatus = "deleted"

	// KeyStatusRotating indicates the key is undergoing rotation.
	KeyStatusRotating KeyStatus = "rotating"

	// KeyStatusCompromised indicates the key has been marked as compromised.
	KeyStatusCompromised KeyStatus = "compromised"
)

// =============================================================================
// Hash Algorithm
// =============================================================================

// HashAlgorithm specifies a cryptographic hash function.
type HashAlgorithm string

const (
	// HashAlgSHA256 is the SHA-256 hash algorithm.
	HashAlgSHA256 HashAlgorithm = "SHA256"

	// HashAlgSHA384 is the SHA-384 hash algorithm.
	HashAlgSHA384 HashAlgorithm = "SHA384"

	// HashAlgSHA512 is the SHA-512 hash algorithm.
	HashAlgSHA512 HashAlgorithm = "SHA512"

	// HashAlgSHA3_256 is the SHA3-256 hash algorithm.
	HashAlgSHA3_256 HashAlgorithm = "SHA3-256"

	// HashAlgSHA3_384 is the SHA3-384 hash algorithm.
	HashAlgSHA3_384 HashAlgorithm = "SHA3-384"

	// HashAlgSHA3_512 is the SHA3-512 hash algorithm.
	HashAlgSHA3_512 HashAlgorithm = "SHA3-512"

	// HashAlgBLAKE2b256 is the BLAKE2b-256 hash algorithm.
	HashAlgBLAKE2b256 HashAlgorithm = "BLAKE2B-256"

	// HashAlgBLAKE2b512 is the BLAKE2b-512 hash algorithm.
	HashAlgBLAKE2b512 HashAlgorithm = "BLAKE2B-512"
)

// =============================================================================
// Common Types
// =============================================================================

// KeyHandle is an opaque reference to a key that never exposes raw key material.
type KeyHandle struct {
	// KeyID is the unique identifier for the key
	KeyID string `json:"key_id"`

	// Version is the key version (for versioned keys)
	Version uint64 `json:"version,omitempty"`

	// Algorithm is the key algorithm
	Algorithm KeyAlgorithm `json:"algorithm"`

	// Status is the current key status
	Status KeyStatus `json:"status"`

	// CreatedAt is when the key was created
	CreatedAt time.Time `json:"created_at"`
}

// KeyMetadata contains non-sensitive metadata about a key.
type KeyMetadata struct {
	// KeyID is the unique identifier
	KeyID string `json:"key_id"`

	// Name is the human-readable key name
	Name string `json:"name,omitempty"`

	// Description provides context about the key
	Description string `json:"description,omitempty"`

	// Algorithm is the cryptographic algorithm
	Algorithm KeyAlgorithm `json:"algorithm"`

	// Status is the current key status
	Status KeyStatus `json:"status"`

	// Usage specifies permitted operations
	Usage []KeyUsage `json:"usage"`

	// Version is the current key version
	Version uint64 `json:"version"`

	// Exportable indicates if the key can be exported
	Exportable bool `json:"exportable"`

	// DeletionProtection prevents accidental deletion
	DeletionProtection bool `json:"deletion_protection"`

	// CreatedAt is when the key was created
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the key was last modified
	UpdatedAt time.Time `json:"updated_at"`

	// LastUsedAt is when the key was last used for crypto operations
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`

	// RotationPolicy is the automatic rotation policy
	RotationPolicy *RotationPolicy `json:"rotation_policy,omitempty"`

	// Tags are user-defined key-value pairs
	Tags map[string]string `json:"tags,omitempty"`

	// Owner is the owning user or service
	Owner string `json:"owner,omitempty"`
}

// KeyPolicy defines access control and usage policies for a key.
type KeyPolicy struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`

	// Version is the policy version
	Version uint64 `json:"version"`

	// AllowedOperations restricts permitted operations
	AllowedOperations []KeyUsage `json:"allowed_operations"`

	// AllowedPrincipals lists authorized users/services
	AllowedPrincipals []string `json:"allowed_principals,omitempty"`

	// DeniedPrincipals lists explicitly denied users/services
	DeniedPrincipals []string `json:"denied_principals,omitempty"`

	// ExpiresAt is when the policy expires
	ExpiresAt *time.Time `json:"expires_at,omitempty"`

	// RotationRequired indicates if key rotation is mandatory
	RotationRequired bool `json:"rotation_required"`

	// MaxUsageCount limits the number of operations
	MaxUsageCount *uint64 `json:"max_usage_count,omitempty"`

	// RequireMFA requires multi-factor authentication
	RequireMFA bool `json:"require_mfa"`

	// UpdatedAt is when the policy was last modified
	UpdatedAt time.Time `json:"updated_at"`
}

// RotationPolicy defines automatic key rotation behavior.
type RotationPolicy struct {
	// Enabled indicates if automatic rotation is active
	Enabled bool `json:"enabled"`

	// Interval is the rotation frequency
	Interval time.Duration `json:"interval"`

	// NextRotation is when the next rotation will occur
	NextRotation time.Time `json:"next_rotation"`

	// RetainVersions is how many old versions to keep
	RetainVersions int `json:"retain_versions"`

	// AutoRotate enables automatic rotation without manual intervention
	AutoRotate bool `json:"auto_rotate"`
}

// Secret represents a versioned secret with metadata.
type Secret struct {
	// SecretID is the unique identifier
	SecretID string `json:"secret_id"`

	// Name is the human-readable secret name
	Name string `json:"name"`

	// Description provides context about the secret
	Description string `json:"description,omitempty"`

	// CurrentVersion is the active version number
	CurrentVersion uint64 `json:"current_version"`

	// CreatedAt is when the secret was first created
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the secret was last modified
	UpdatedAt time.Time `json:"updated_at"`

	// RotationPolicy is the automatic rotation policy
	RotationPolicy *SecretRotationPolicy `json:"rotation_policy,omitempty"`

	// Tags are user-defined key-value pairs
	Tags map[string]string `json:"tags,omitempty"`

	// Owner is the owning user or service
	Owner string `json:"owner,omitempty"`
}

// SecretVersion represents a specific version of a secret.
type SecretVersion struct {
	// SecretID is the parent secret identifier
	SecretID string `json:"secret_id"`

	// Version is the version number
	Version uint64 `json:"version"`

	// Data is the encrypted secret data (never returned in plaintext)
	Data []byte `json:"data,omitempty"`

	// Metadata contains version-specific metadata
	Metadata map[string]string `json:"metadata,omitempty"`

	// CreatedAt is when this version was created
	CreatedAt time.Time `json:"created_at"`

	// CreatedBy is who created this version
	CreatedBy string `json:"created_by,omitempty"`

	// DeletedAt indicates if this version was soft-deleted
	DeletedAt *time.Time `json:"deleted_at,omitempty"`

	// ExpiresAt is when this version expires
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// SecretRotationPolicy defines automatic secret rotation behavior.
type SecretRotationPolicy struct {
	// Enabled indicates if automatic rotation is active
	Enabled bool `json:"enabled"`

	// Interval is the rotation frequency
	Interval time.Duration `json:"interval"`

	// NextRotation is when the next rotation will occur
	NextRotation time.Time `json:"next_rotation"`

	// RetainVersions is how many old versions to keep
	RetainVersions int `json:"retain_versions"`

	// RotationLambda is an optional function ARN to call during rotation
	RotationLambda string `json:"rotation_lambda,omitempty"`
}

// =============================================================================
// KMS Request/Response Types
// =============================================================================

// GenerateKeyRequest requests generation of a new cryptographic key.
type GenerateKeyRequest struct {
	// KeyAttributes defines the key attributes from go-xkms
	KeyAttributes *kctypes.KeyAttributes `json:"key_attributes"`

	// Name is the human-readable key name
	Name string `json:"name,omitempty"`

	// Description provides context about the key
	Description string `json:"description,omitempty"`

	// Usage specifies permitted operations for the key
	Usage []KeyUsage `json:"usage,omitempty"`

	// Exportable indicates if the key can be exported
	Exportable bool `json:"exportable,omitempty"`

	// DeletionProtection prevents accidental deletion
	DeletionProtection bool `json:"deletion_protection,omitempty"`

	// Tags are user-defined key-value pairs
	Tags map[string]string `json:"tags,omitempty"`

	// Owner is the owning user or service
	Owner string `json:"owner,omitempty"`
}

// GenerateKeyResponse contains the newly generated key.
type GenerateKeyResponse struct {
	// Key is the key handle (does not contain key material)
	Key *KeyHandle `json:"key"`

	// Metadata contains key metadata
	Metadata *KeyMetadata `json:"metadata"`

	// Timestamp is when the key was generated
	Timestamp time.Time `json:"timestamp"`
}

// ImportKeyRequest requests import of an external key.
type ImportKeyRequest struct {
	// KeyMaterial is the encrypted key material
	KeyMaterial []byte `json:"key_material"`

	// WrappingKeyID is the key used to wrap the imported key
	WrappingKeyID string `json:"wrapping_key_id"`

	// KeyAttributes defines the key attributes from go-xkms
	KeyAttributes *kctypes.KeyAttributes `json:"key_attributes"`

	// Name is the human-readable key name
	Name string `json:"name,omitempty"`

	// Description provides context about the key
	Description string `json:"description,omitempty"`

	// Usage specifies permitted operations for the key
	Usage []KeyUsage `json:"usage,omitempty"`

	// DeletionProtection prevents accidental deletion
	DeletionProtection bool `json:"deletion_protection,omitempty"`

	// Tags are user-defined key-value pairs
	Tags map[string]string `json:"tags,omitempty"`
}

// ImportKeyResponse confirms key import.
type ImportKeyResponse struct {
	// Key is the imported key handle
	Key *KeyHandle `json:"key"`

	// Metadata contains key metadata
	Metadata *KeyMetadata `json:"metadata"`

	// Timestamp is when the key was imported
	Timestamp time.Time `json:"timestamp"`
}

// GetKeyRequest requests a key by ID.
type GetKeyRequest struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`

	// Version optionally specifies a specific key version
	Version uint64 `json:"version,omitempty"`
}

// GetKeyResponse returns a key handle.
type GetKeyResponse struct {
	// Key is the key handle
	Key *KeyHandle `json:"key"`

	// Metadata contains key metadata
	Metadata *KeyMetadata `json:"metadata"`

	// Timestamp is when the request was processed
	Timestamp time.Time `json:"timestamp"`
}

// GetPublicKeyRequest requests the public key for an asymmetric key.
type GetPublicKeyRequest struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`

	// Version optionally specifies a specific key version
	Version uint64 `json:"version,omitempty"`
}

// GetPublicKeyResponse returns the public key.
type GetPublicKeyResponse struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`

	// PublicKey is the DER-encoded public key
	PublicKey []byte `json:"public_key"`

	// Algorithm is the key algorithm
	Algorithm KeyAlgorithm `json:"algorithm"`

	// Version is the key version
	Version uint64 `json:"version"`

	// Timestamp is when the request was processed
	Timestamp time.Time `json:"timestamp"`
}

// ListKeysRequest requests a list of keys.
type ListKeysRequest struct {
	// Limit is the maximum number of results
	Limit int `json:"limit,omitempty"`

	// Cursor is the pagination cursor
	Cursor string `json:"cursor,omitempty"`

	// Filter optionally filters by status, algorithm, etc.
	Filter map[string]string `json:"filter,omitempty"`
}

// ListKeysResponse returns a list of keys.
type ListKeysResponse struct {
	// Keys is the list of key handles
	Keys []*KeyHandle `json:"keys"`

	// NextCursor is the cursor for the next page
	NextCursor string `json:"next_cursor,omitempty"`

	// HasMore indicates if there are more results
	HasMore bool `json:"has_more"`

	// TotalCount is the total number of keys (if available)
	TotalCount int `json:"total_count,omitempty"`

	// Timestamp is when the list was generated
	Timestamp time.Time `json:"timestamp"`
}

// RotateKeyRequest requests key rotation.
type RotateKeyRequest struct {
	// KeyID is the key to rotate
	KeyID string `json:"key_id"`

	// RetainOldVersion indicates if the old version should be retained
	RetainOldVersion bool `json:"retain_old_version"`

	// GracePeriodSeconds is the duration in seconds after which the old version
	// will be automatically disabled. If 0, no grace period is set.
	GracePeriodSeconds int64 `json:"grace_period_seconds,omitempty"`
}

// RotateKeyResponse confirms key rotation.
type RotateKeyResponse struct {
	// KeyID is the rotated key identifier
	KeyID string `json:"key_id"`

	// OldVersion is the previous version number
	OldVersion uint64 `json:"old_version"`

	// NewVersion is the new version number
	NewVersion uint64 `json:"new_version"`

	// Timestamp is when rotation occurred
	Timestamp time.Time `json:"timestamp"`

	// GracePeriodDisableAt is the Unix timestamp when the old version will be disabled.
	// Zero if no grace period was set.
	GracePeriodDisableAt int64 `json:"grace_period_disable_at,omitempty"`

	// GracePeriodJobID is the scheduler job ID for the grace period disable task.
	// Empty if no grace period was set.
	GracePeriodJobID string `json:"grace_period_job_id,omitempty"`
}

// ListKeyVersionsRequest requests list of key versions.
type ListKeyVersionsRequest struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`

	// Limit is the maximum number of results
	Limit int `json:"limit,omitempty"`

	// Cursor is the pagination cursor
	Cursor string `json:"cursor,omitempty"`
}

// ListKeyVersionsResponse returns a list of key versions.
type ListKeyVersionsResponse struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`

	// Versions is the list of key versions
	Versions []*KeyVersionInfo `json:"versions"`

	// NextCursor is the cursor for the next page
	NextCursor string `json:"next_cursor,omitempty"`

	// HasMore indicates if there are more results
	HasMore bool `json:"has_more"`

	// Timestamp is when the list was generated
	Timestamp time.Time `json:"timestamp"`
}

// UpdateKeyStatusRequest requests key status update.
type UpdateKeyStatusRequest struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`

	// Version optionally specifies a specific version
	Version uint64 `json:"version,omitempty"`

	// Status is the new key status
	Status KeyStatus `json:"status"`

	// Reason optionally provides a reason for the status change
	Reason string `json:"reason,omitempty"`
}

// UpdateKeyStatusResponse confirms key status update.
type UpdateKeyStatusResponse struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`

	// Version is the key version affected
	Version uint64 `json:"version"`

	// OldStatus is the previous status
	OldStatus KeyStatus `json:"old_status"`

	// NewStatus is the new status
	NewStatus KeyStatus `json:"new_status"`

	// Status is an alias for NewStatus for convenience
	Status KeyStatus `json:"status"`

	// Timestamp is when the status was updated
	Timestamp time.Time `json:"timestamp"`
}

// RevokeKeyRequest requests key revocation.
type RevokeKeyRequest struct {
	// KeyID is the key to revoke
	KeyID string `json:"key_id"`

	// Reason is the revocation reason
	Reason string `json:"reason,omitempty"`
}

// RevokeKeyResponse confirms key revocation.
type RevokeKeyResponse struct {
	// KeyID is the revoked key identifier
	KeyID string `json:"key_id"`

	// Status is the new key status
	Status KeyStatus `json:"status"`

	// Timestamp is when revocation occurred
	Timestamp time.Time `json:"timestamp"`
}

// DeleteKeyRequest requests key deletion.
type DeleteKeyRequest struct {
	// KeyID is the key to delete
	KeyID string `json:"key_id"`

	// PendingWindowDays is days before permanent deletion (7-30)
	PendingWindowDays int `json:"pending_window_days,omitempty"`

	// Force bypasses deletion protection (requires elevated privileges)
	Force bool `json:"force"`
}

// DeleteKeyResponse confirms key deletion scheduling.
type DeleteKeyResponse struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`

	// Status is the new key status
	Status KeyStatus `json:"status"`

	// DeletionDate is when permanent deletion will occur
	DeletionDate time.Time `json:"deletion_date"`

	// Timestamp is when deletion was scheduled
	Timestamp time.Time `json:"timestamp"`
}

// GetKeyMetadataRequest requests key metadata.
type GetKeyMetadataRequest struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`
}

// GetKeyMetadataResponse returns key metadata.
type GetKeyMetadataResponse struct {
	// Metadata contains key metadata
	Metadata *KeyMetadata `json:"metadata"`

	// Timestamp is when metadata was retrieved
	Timestamp time.Time `json:"timestamp"`
}

// UpdateKeyMetadataRequest updates key metadata.
type UpdateKeyMetadataRequest struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`

	// Name optionally updates the key name
	Name *string `json:"name,omitempty"`

	// Description optionally updates the description
	Description *string `json:"description,omitempty"`

	// Tags optionally updates tags (merged with existing)
	Tags map[string]string `json:"tags,omitempty"`

	// RemoveTags optionally removes specific tags
	RemoveTags []string `json:"remove_tags,omitempty"`
}

// UpdateKeyMetadataResponse confirms metadata update.
type UpdateKeyMetadataResponse struct {
	// Metadata contains updated key metadata
	Metadata *KeyMetadata `json:"metadata"`

	// Timestamp is when update occurred
	Timestamp time.Time `json:"timestamp"`
}

// SetKeyPolicyRequest sets a key's access policy.
type SetKeyPolicyRequest struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`

	// Policy is the new policy
	Policy *KeyPolicy `json:"policy"`
}

// SetKeyPolicyResponse confirms policy update.
type SetKeyPolicyResponse struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`

	// PolicyVersion is the new policy version
	PolicyVersion uint64 `json:"policy_version"`

	// Timestamp is when policy was updated
	Timestamp time.Time `json:"timestamp"`
}

// GetKeyPolicyRequest requests a key's access policy.
type GetKeyPolicyRequest struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`
}

// GetKeyPolicyResponse returns the key policy.
type GetKeyPolicyResponse struct {
	// Policy is the current policy
	Policy *KeyPolicy `json:"policy"`

	// Timestamp is when policy was retrieved
	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// Secrets Request/Response Types
// =============================================================================

// StoreSecretRequest stores a new secret or version.
type StoreSecretRequest struct {
	// SecretID is the secret identifier (optional for new secrets)
	SecretID string `json:"secret_id,omitempty"`

	// Name is the human-readable secret name
	Name string `json:"name"`

	// Description provides context about the secret
	Description string `json:"description,omitempty"`

	// Data is the secret data (will be encrypted)
	Data []byte `json:"data"`

	// Metadata contains version-specific metadata
	Metadata map[string]string `json:"metadata,omitempty"`

	// Tags are user-defined key-value pairs
	Tags map[string]string `json:"tags,omitempty"`

	// ExpiresAt optionally sets version expiration
	ExpiresAt *time.Time `json:"expires_at,omitempty"`

	// TenantID specifies a tenant-specific DEK to use for encryption
	TenantID string `json:"tenant_id,omitempty"`

	// KeyID specifies a custom key identifier to use for encryption
	KeyID string `json:"key_id,omitempty"`

	// Algorithm specifies the encryption algorithm to use
	Algorithm string `json:"algorithm,omitempty"`
}

// StoreSecretResponse confirms secret storage.
type StoreSecretResponse struct {
	// SecretID is the secret identifier
	SecretID string `json:"secret_id"`

	// Version is the version number
	Version uint64 `json:"version"`

	// Timestamp is when the secret was stored
	Timestamp time.Time `json:"timestamp"`
}

// EnsureSecretRequest is the Vault-style generate-on-first-access
// request. The caller supplies the secret path and the desired random
// shape. On the first call for a given path, the server generates a
// fresh value per Spec and stores it; on every subsequent call it
// returns the same stable bytes. Concurrent callers racing on the
// same path are serialized via a distributed lock so only one
// generate-and-store actually executes.
//
// Spec mismatch between the request and a previously stored value is
// a hard error. EnsureSecret never silently rotates.
//
// See pkg/secrets/spec.go for the underlying SecretSpec semantics.
type EnsureSecretRequest struct {
	// SecretID is the path to the secret. This is the stable
	// identity used by both the generate path and the read path.
	SecretID string `json:"secret_id"`

	// Length is the spec length (bytes for base64/hex/raw, digits
	// for numeric). Must be positive.
	Length int `json:"length"`

	// Encoding is the output encoding. One of:
	// "base64", "hex", "numeric", "raw". Required.
	Encoding string `json:"encoding"`

	// TenantID optionally scopes the secret to a tenant for
	// tenant-keyed DEK selection. Defaults to the context tenant
	// when empty.
	TenantID string `json:"tenant_id,omitempty"`
}

// EnsureSecretResponse carries either the existing stable bytes or
// the bytes generated on this call's first-ever access.
type EnsureSecretResponse struct {
	// SecretID echoes the request path.
	SecretID string `json:"secret_id"`

	// Data is the secret value as the caller should consume it.
	// For base64/hex/numeric encodings this is printable ASCII;
	// for raw it is opaque bytes.
	Data []byte `json:"data"`

	// Generated is true when this call produced a fresh value
	// (first access). Subsequent reads return Generated=false.
	Generated bool `json:"generated"`

	// Version is the current version of the underlying secret.
	Version uint64 `json:"version"`

	// Timestamp is when the secret was first generated or, on a
	// cache hit, when it was last stored.
	Timestamp time.Time `json:"timestamp"`
}

// GetSecretRequest retrieves a secret.
type GetSecretRequest struct {
	// SecretID is the secret identifier
	SecretID string `json:"secret_id"`

	// Version optionally specifies a specific version (defaults to latest)
	Version uint64 `json:"version,omitempty"`
}

// GetSecretResponse returns the secret data.
type GetSecretResponse struct {
	// SecretID is the secret identifier
	SecretID string `json:"secret_id"`

	// Version is the version number
	Version uint64 `json:"version"`

	// Data is the decrypted secret data
	Data []byte `json:"data"`

	// Metadata contains version-specific metadata
	Metadata map[string]string `json:"metadata,omitempty"`

	// CreatedAt is when this version was created
	CreatedAt time.Time `json:"created_at"`

	// Timestamp is when the request was processed
	Timestamp time.Time `json:"timestamp"`

	// Algorithm is the encryption algorithm used (if encrypted)
	Algorithm string `json:"algorithm,omitempty"`

	// KeyIdentifier is the key used for encryption (if encrypted)
	KeyIdentifier string `json:"key_identifier,omitempty"`
}

// ListSecretsRequest lists secrets.
type ListSecretsRequest struct {
	// Prefix filters secrets by path prefix (e.g., "/apps/myapp/")
	Prefix string `json:"prefix,omitempty"`

	// Limit is the maximum number of results
	Limit int `json:"limit,omitempty"`

	// Cursor is the pagination cursor
	Cursor string `json:"cursor,omitempty"`

	// Filter optionally filters by tags, owner, etc.
	Filter map[string]string `json:"filter,omitempty"`
}

// ListSecretsResponse returns a list of secrets.
type ListSecretsResponse struct {
	// Secrets is the list of secrets (without data)
	Secrets []*Secret `json:"secrets"`

	// NextCursor is the cursor for the next page
	NextCursor string `json:"next_cursor,omitempty"`

	// HasMore indicates if there are more results
	HasMore bool `json:"has_more"`

	// TotalCount is the total number of secrets
	TotalCount int `json:"total_count,omitempty"`

	// Timestamp is when the list was generated
	Timestamp time.Time `json:"timestamp"`
}

// DeleteSecretRequest deletes a secret.
type DeleteSecretRequest struct {
	// SecretID is the secret to delete
	SecretID string `json:"secret_id"`

	// PermanentlyDelete bypasses soft-delete
	PermanentlyDelete bool `json:"permanently_delete"`

	// AllVersions deletes all versions of the secret
	AllVersions bool `json:"all_versions"`
}

// DeleteSecretResponse confirms secret deletion.
type DeleteSecretResponse struct {
	// SecretID is the deleted secret identifier
	SecretID string `json:"secret_id"`

	// Deleted indicates if deletion succeeded
	Deleted bool `json:"deleted"`

	// Timestamp is when deletion occurred
	Timestamp time.Time `json:"timestamp"`
}

// GetSecretVersionRequest gets metadata for a specific version.
type GetSecretVersionRequest struct {
	// SecretID is the secret identifier
	SecretID string `json:"secret_id"`

	// Version is the version number
	Version uint64 `json:"version"`
}

// GetSecretVersionResponse returns version metadata.
type GetSecretVersionResponse struct {
	// Version contains the version details
	Version *SecretVersion `json:"version"`

	// Timestamp is when the request was processed
	Timestamp time.Time `json:"timestamp"`
}

// ListSecretVersionsRequest lists versions of a secret.
type ListSecretVersionsRequest struct {
	// SecretID is the secret identifier
	SecretID string `json:"secret_id"`

	// Limit is the maximum number of results
	Limit int `json:"limit,omitempty"`

	// Cursor is the pagination cursor
	Cursor string `json:"cursor,omitempty"`
}

// ListSecretVersionsResponse returns a list of versions.
type ListSecretVersionsResponse struct {
	// Versions is the list of secret versions
	Versions []*SecretVersion `json:"versions"`

	// NextCursor is the cursor for the next page
	NextCursor string `json:"next_cursor,omitempty"`

	// HasMore indicates if there are more results
	HasMore bool `json:"has_more"`

	// Timestamp is when the list was generated
	Timestamp time.Time `json:"timestamp"`
}

// RollbackSecretRequest rolls back to a previous version.
type RollbackSecretRequest struct {
	// SecretID is the secret identifier
	SecretID string `json:"secret_id"`

	// TargetVersion is the version to roll back to
	TargetVersion uint64 `json:"target_version"`
}

// RollbackSecretResponse confirms rollback.
type RollbackSecretResponse struct {
	// SecretID is the secret identifier
	SecretID string `json:"secret_id"`

	// NewVersion is the new current version
	NewVersion uint64 `json:"new_version"`

	// Timestamp is when rollback occurred
	Timestamp time.Time `json:"timestamp"`
}

// RotateSecretRequest rotates a secret.
type RotateSecretRequest struct {
	// SecretID is the secret to rotate
	SecretID string `json:"secret_id"`

	// NewData is the new secret data
	NewData []byte `json:"new_data"`

	// Metadata contains version-specific metadata
	Metadata map[string]string `json:"metadata,omitempty"`
}

// RotateSecretResponse confirms secret rotation.
type RotateSecretResponse struct {
	// SecretID is the secret identifier
	SecretID string `json:"secret_id"`

	// OldVersion is the previous version
	OldVersion uint64 `json:"old_version"`

	// NewVersion is the new version
	NewVersion uint64 `json:"new_version"`

	// Timestamp is when rotation occurred
	Timestamp time.Time `json:"timestamp"`
}

// SetRotationPolicyRequest sets automatic rotation policy.
type SetRotationPolicyRequest struct {
	// SecretID is the secret identifier
	SecretID string `json:"secret_id"`

	// Policy is the rotation policy
	Policy *SecretRotationPolicy `json:"policy"`
}

// SetRotationPolicyResponse confirms policy update.
type SetRotationPolicyResponse struct {
	// SecretID is the secret identifier
	SecretID string `json:"secret_id"`

	// Policy is the updated policy
	Policy *SecretRotationPolicy `json:"policy"`

	// Timestamp is when policy was updated
	Timestamp time.Time `json:"timestamp"`
}

// GetRotationPolicyRequest gets rotation policy.
type GetRotationPolicyRequest struct {
	// SecretID is the secret identifier
	SecretID string `json:"secret_id"`
}

// GetRotationPolicyResponse returns the rotation policy.
type GetRotationPolicyResponse struct {
	// SecretID is the secret identifier
	SecretID string `json:"secret_id"`

	// Policy is the current policy
	Policy *SecretRotationPolicy `json:"policy"`

	// Timestamp is when policy was retrieved
	Timestamp time.Time `json:"timestamp"`
}

// SecretStatus represents the status of a secret.
type SecretStatus string

const (
	// SecretStatusEnabled indicates the secret is enabled and accessible.
	SecretStatusEnabled SecretStatus = "enabled"

	// SecretStatusDisabled indicates the secret is disabled.
	SecretStatusDisabled SecretStatus = "disabled"

	// SecretStatusDeleted indicates the secret is marked for deletion.
	SecretStatusDeleted SecretStatus = "deleted"
)

// UpdateSecretStatusRequest changes a secret's status.
type UpdateSecretStatusRequest struct {
	// SecretID is the secret identifier
	SecretID string `json:"secret_id"`

	// Status is the new status
	Status SecretStatus `json:"status"`
}

// UpdateSecretStatusResponse confirms status update.
type UpdateSecretStatusResponse struct {
	// SecretID is the secret identifier
	SecretID string `json:"secret_id"`

	// OldStatus is the previous status
	OldStatus SecretStatus `json:"old_status"`

	// NewStatus is the new status
	NewStatus SecretStatus `json:"new_status"`

	// Timestamp is when status was updated
	Timestamp time.Time `json:"timestamp"`
}

// ReencryptionProgress tracks the progress of a re-encryption operation.
type ReencryptionProgress struct {
	// Status is the current status (e.g., "running", "completed", "failed")
	Status string `json:"status"`

	// InProgress indicates if re-encryption is currently running
	InProgress bool `json:"in_progress"`

	// TotalKeys is the total number of keys to re-encrypt
	TotalKeys uint64 `json:"total_keys"`

	// ProcessedKeys is the number of keys processed so far
	ProcessedKeys uint64 `json:"processed_keys"`

	// FailedKeys is the number of keys that failed re-encryption
	FailedKeys uint64 `json:"failed_keys"`

	// PercentComplete is the percentage of completion
	PercentComplete float64 `json:"percent_complete"`

	// StartedAt is when re-encryption started
	StartedAt *time.Time `json:"started_at,omitempty"`

	// EstimatedCompletion is the estimated completion time
	EstimatedCompletion *time.Time `json:"estimated_completion,omitempty"`

	// ErrorMessage contains any error message if failed
	ErrorMessage string `json:"error_message,omitempty"`
}

// =============================================================================
// Crypto Request/Response Types
// =============================================================================

// EncryptRequest requests data encryption.
type EncryptRequest struct {
	// KeyID is the encryption key identifier
	KeyID string `json:"key_id"`

	// Plaintext is the data to encrypt
	Plaintext []byte `json:"plaintext"`

	// Context is optional additional authenticated data
	Context []byte `json:"context,omitempty"`

	// Nonce optionally specifies a nonce (algorithm-dependent)
	Nonce []byte `json:"nonce,omitempty"`
}

// EncryptResponse returns encrypted data.
type EncryptResponse struct {
	// KeyID is the encryption key identifier
	KeyID string `json:"key_id"`

	// KeyVersion is the key version used
	KeyVersion uint64 `json:"key_version"`

	// Ciphertext is the encrypted data
	Ciphertext []byte `json:"ciphertext"`

	// Nonce is the nonce used (if applicable)
	Nonce []byte `json:"nonce,omitempty"`

	// Timestamp is when encryption occurred
	Timestamp time.Time `json:"timestamp"`
}

// DecryptRequest requests data decryption.
type DecryptRequest struct {
	// KeyID is the decryption key identifier
	KeyID string `json:"key_id"`

	// Ciphertext is the data to decrypt
	Ciphertext []byte `json:"ciphertext"`

	// Context is optional additional authenticated data
	Context []byte `json:"context,omitempty"`

	// Nonce optionally specifies a nonce (algorithm-dependent)
	Nonce []byte `json:"nonce,omitempty"`

	// KeyVersion optionally specifies a specific key version
	KeyVersion uint64 `json:"key_version,omitempty"`
}

// DecryptResponse returns decrypted data.
type DecryptResponse struct {
	// KeyID is the decryption key identifier
	KeyID string `json:"key_id"`

	// KeyVersion is the key version used
	KeyVersion uint64 `json:"key_version"`

	// Plaintext is the decrypted data
	Plaintext []byte `json:"plaintext"`

	// Timestamp is when decryption occurred
	Timestamp time.Time `json:"timestamp"`
}

// RewrapRequest requests re-encryption with a new key version.
type RewrapRequest struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`

	// Ciphertext is the data to rewrap
	Ciphertext []byte `json:"ciphertext"`

	// Context is optional additional authenticated data
	Context []byte `json:"context,omitempty"`

	// Nonce optionally specifies a nonce
	Nonce []byte `json:"nonce,omitempty"`
}

// RewrapResponse returns re-encrypted data.
type RewrapResponse struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`

	// OldKeyVersion is the previous key version
	OldKeyVersion uint64 `json:"old_key_version"`

	// NewKeyVersion is the new key version
	NewKeyVersion uint64 `json:"new_key_version"`

	// Ciphertext is the re-encrypted data
	Ciphertext []byte `json:"ciphertext"`

	// Nonce is the new nonce (if applicable)
	Nonce []byte `json:"nonce,omitempty"`

	// Timestamp is when rewrap occurred
	Timestamp time.Time `json:"timestamp"`
}

// BatchEncryptItem represents a single item in a batch encrypt request.
type BatchEncryptItem struct {
	// Plaintext is the data to encrypt
	Plaintext []byte `json:"plaintext"`

	// Context is optional additional authenticated data
	Context []byte `json:"context,omitempty"`
}

// EncryptBatchRequest requests batch encryption.
type EncryptBatchRequest struct {
	// KeyID is the encryption key identifier
	KeyID string `json:"key_id"`

	// Items is the list of items to encrypt
	Items []BatchEncryptItem `json:"items"`
}

// BatchEncryptResult represents the result of encrypting one item.
type BatchEncryptResult struct {
	// Ciphertext is the encrypted data
	Ciphertext []byte `json:"ciphertext"`

	// Nonce is the nonce used (if applicable)
	Nonce []byte `json:"nonce,omitempty"`

	// Error contains any error message
	Error string `json:"error,omitempty"`
}

// EncryptBatchResponse returns batch encryption results.
type EncryptBatchResponse struct {
	// KeyID is the encryption key identifier
	KeyID string `json:"key_id"`

	// KeyVersion is the key version used
	KeyVersion uint64 `json:"key_version"`

	// Results contains per-item results
	Results []BatchEncryptResult `json:"results"`

	// Timestamp is when batch encryption occurred
	Timestamp time.Time `json:"timestamp"`
}

// BatchDecryptItem represents a single item in a batch decrypt request.
type BatchDecryptItem struct {
	// Ciphertext is the data to decrypt
	Ciphertext []byte `json:"ciphertext"`

	// Context is optional additional authenticated data
	Context []byte `json:"context,omitempty"`

	// Nonce optionally specifies a nonce
	Nonce []byte `json:"nonce,omitempty"`
}

// DecryptBatchRequest requests batch decryption.
type DecryptBatchRequest struct {
	// KeyID is the decryption key identifier
	KeyID string `json:"key_id"`

	// Items is the list of items to decrypt
	Items []BatchDecryptItem `json:"items"`
}

// BatchDecryptResult represents the result of decrypting one item.
type BatchDecryptResult struct {
	// Plaintext is the decrypted data
	Plaintext []byte `json:"plaintext"`

	// Error contains any error message
	Error string `json:"error,omitempty"`
}

// DecryptBatchResponse returns batch decryption results.
type DecryptBatchResponse struct {
	// KeyID is the decryption key identifier
	KeyID string `json:"key_id"`

	// Results contains per-item results
	Results []BatchDecryptResult `json:"results"`

	// Timestamp is when batch decryption occurred
	Timestamp time.Time `json:"timestamp"`
}

// GenerateDataKeyRequest generates a data encryption key.
type GenerateDataKeyRequest struct {
	// KeyID is the master key for wrapping
	KeyID string `json:"key_id"`

	// KeyAttributes specifies the data key attributes from go-xkms
	KeyAttributes *kctypes.KeyAttributes `json:"key_attributes"`

	// Context is optional additional authenticated data
	Context []byte `json:"context,omitempty"`
}

// GenerateDataKeyResponse returns a data key.
type GenerateDataKeyResponse struct {
	// KeyID is the master key identifier
	KeyID string `json:"key_id"`

	// Plaintext is the plaintext data key (handle with care!)
	Plaintext []byte `json:"plaintext"`

	// CiphertextBlob is the encrypted data key
	CiphertextBlob []byte `json:"ciphertext_blob"`

	// Timestamp is when the data key was generated
	Timestamp time.Time `json:"timestamp"`
}

// GenerateWrappedDataKeyRequest generates a wrapped data key.
type GenerateWrappedDataKeyRequest struct {
	// KeyID is the master key for wrapping
	KeyID string `json:"key_id"`

	// KeyAttributes specifies the data key attributes from go-xkms
	KeyAttributes *kctypes.KeyAttributes `json:"key_attributes"`

	// Context is optional additional authenticated data
	Context []byte `json:"context,omitempty"`
}

// GenerateWrappedDataKeyResponse returns only the wrapped data key.
type GenerateWrappedDataKeyResponse struct {
	// KeyID is the master key identifier
	KeyID string `json:"key_id"`

	// CiphertextBlob is the encrypted data key (plaintext never returned)
	CiphertextBlob []byte `json:"ciphertext_blob"`

	// Timestamp is when the data key was generated
	Timestamp time.Time `json:"timestamp"`
}

// SignRequest requests data signing.
type SignRequest struct {
	// KeyID is the signing key identifier
	KeyID string `json:"key_id"`

	// Message is the data to sign
	Message []byte `json:"message"`

	// HashAlgorithm specifies the hash algorithm
	HashAlgorithm HashAlgorithm `json:"hash_algorithm,omitempty"`

	// PreHashed indicates the Message is already a hash digest (e.g., from crypto.Signer).
	// When true, the service will not hash the message again.
	PreHashed bool `json:"pre_hashed,omitempty"`
}

// SignResponse returns the signature.
type SignResponse struct {
	// KeyID is the signing key identifier
	KeyID string `json:"key_id"`

	// KeyVersion is the key version used
	KeyVersion uint64 `json:"key_version"`

	// Signature is the cryptographic signature
	Signature []byte `json:"signature"`

	// HashAlgorithm is the hash algorithm used
	HashAlgorithm HashAlgorithm `json:"hash_algorithm"`

	// Timestamp is when signing occurred
	Timestamp time.Time `json:"timestamp"`
}

// VerifyRequest requests signature verification.
type VerifyRequest struct {
	// KeyID is the verification key identifier
	KeyID string `json:"key_id"`

	// Message is the signed data
	Message []byte `json:"message"`

	// Signature is the signature to verify
	Signature []byte `json:"signature"`

	// HashAlgorithm specifies the hash algorithm
	HashAlgorithm HashAlgorithm `json:"hash_algorithm,omitempty"`

	// KeyVersion optionally specifies a specific key version
	KeyVersion uint64 `json:"key_version,omitempty"`
}

// VerifyResponse returns verification result.
type VerifyResponse struct {
	// KeyID is the verification key identifier
	KeyID string `json:"key_id"`

	// KeyVersion is the key version used
	KeyVersion uint64 `json:"key_version"`

	// Valid indicates if the signature is valid
	Valid bool `json:"valid"`

	// Timestamp is when verification occurred
	Timestamp time.Time `json:"timestamp"`
}

// HMACRequest requests HMAC generation.
type HMACRequest struct {
	// KeyID is the HMAC key identifier
	KeyID string `json:"key_id"`

	// Message is the data to MAC
	Message []byte `json:"message"`

	// HashAlgorithm specifies the hash algorithm
	HashAlgorithm HashAlgorithm `json:"hash_algorithm,omitempty"`
}

// HMACResponse returns the HMAC.
type HMACResponse struct {
	// KeyID is the HMAC key identifier
	KeyID string `json:"key_id"`

	// KeyVersion is the key version used
	KeyVersion uint64 `json:"key_version"`

	// HMAC is the message authentication code
	HMAC []byte `json:"hmac"`

	// HashAlgorithm is the hash algorithm used
	HashAlgorithm HashAlgorithm `json:"hash_algorithm"`

	// Timestamp is when HMAC was generated
	Timestamp time.Time `json:"timestamp"`
}

// VerifyHMACRequest requests HMAC verification.
type VerifyHMACRequest struct {
	// KeyID is the HMAC key identifier
	KeyID string `json:"key_id"`

	// Message is the data to verify
	Message []byte `json:"message"`

	// HMAC is the HMAC to verify
	HMAC []byte `json:"hmac"`

	// HashAlgorithm specifies the hash algorithm
	HashAlgorithm HashAlgorithm `json:"hash_algorithm,omitempty"`

	// KeyVersion optionally specifies a specific key version
	KeyVersion uint64 `json:"key_version,omitempty"`
}

// VerifyHMACResponse returns HMAC verification result.
type VerifyHMACResponse struct {
	// KeyID is the HMAC key identifier
	KeyID string `json:"key_id"`

	// KeyVersion is the key version used
	KeyVersion uint64 `json:"key_version"`

	// Valid indicates if the HMAC is valid
	Valid bool `json:"valid"`

	// Timestamp is when verification occurred
	Timestamp time.Time `json:"timestamp"`
}

// GenerateRandomRequest requests random bytes.
type GenerateRandomRequest struct {
	// NumBytes is the number of random bytes to generate
	NumBytes int `json:"num_bytes"`
}

// GenerateRandomResponse returns random bytes.
type GenerateRandomResponse struct {
	// RandomBytes is the cryptographically secure random data
	RandomBytes []byte `json:"random_bytes"`

	// Timestamp is when random bytes were generated
	Timestamp time.Time `json:"timestamp"`
}

// HashRequest requests hashing.
type HashRequest struct {
	// Data is the data to hash
	Data []byte `json:"data"`

	// Algorithm specifies the hash algorithm
	Algorithm HashAlgorithm `json:"algorithm"`
}

// HashResponse returns the hash.
type HashResponse struct {
	// Algorithm is the hash algorithm used
	Algorithm HashAlgorithm `json:"algorithm"`

	// Hash is the computed hash
	Hash []byte `json:"hash"`

	// Timestamp is when hashing occurred
	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// Seal Request/Response Types
// =============================================================================

// SealStatusResponse returns the current seal status.
type SealStatusResponse struct {
	// Sealed indicates if the system is sealed
	Sealed bool `json:"sealed"`

	// Initialized indicates if seal has been bootstrapped
	Initialized bool `json:"initialized"`

	// Mode is the seal mode (manual/auto)
	Mode string `json:"mode"`

	// Threshold is shares required to unseal
	Threshold int `json:"threshold"`

	// TotalShares is total shares generated
	TotalShares int `json:"total_shares"`

	// SharesProvided is shares provided so far
	SharesProvided int `json:"shares_provided"`

	// TPMAvailable indicates if TPM is available
	TPMAvailable bool `json:"tpm_available"`

	// ClusterNodes is the number of cluster nodes
	ClusterNodes int `json:"cluster_nodes"`

	// SharesReceived is shares received from other nodes
	SharesReceived int `json:"shares_received"`

	// Version is the seal configuration version
	Version uint64 `json:"version"`

	// Timestamp is when status was retrieved
	Timestamp time.Time `json:"timestamp"`
}

// SealInitRequest initializes the seal system.
type SealInitRequest struct {
	// Threshold is minimum shares needed
	Threshold int `json:"threshold"`

	// TotalShares is total shares to generate
	TotalShares int `json:"total_shares"`

	// RecoveryKeysEnabled enables recovery key generation
	RecoveryKeysEnabled bool `json:"recovery_keys_enabled"`

	// RecoveryThreshold is minimum recovery keys needed
	RecoveryThreshold int `json:"recovery_threshold"`

	// RecoveryTotalKeys is total recovery keys to generate
	RecoveryTotalKeys int `json:"recovery_total_keys"`
}

// SealInitResponse returns initialization result.
type SealInitResponse struct {
	// Shares are the Shamir shares (STORE SECURELY!)
	Shares [][]byte `json:"shares"`

	// RecoveryKeys are recovery keys (STORE SECURELY!)
	RecoveryKeys [][]byte `json:"recovery_keys,omitempty"`

	// RootToken is the initial root token
	RootToken string `json:"root_token"`

	// Version is the seal configuration version
	Version uint64 `json:"version"`

	// Timestamp is when initialization occurred
	Timestamp time.Time `json:"timestamp"`
}

// UnsealRequest provides an unseal share.
type UnsealRequest struct {
	// Share is the Shamir share
	Share []byte `json:"share"`

	// NodeID is the node providing the share (auto mode)
	NodeID uint64 `json:"node_id,omitempty"`

	// Signature authenticates the share (auto mode)
	Signature []byte `json:"signature,omitempty"`
}

// UnsealResponse returns unseal progress.
type UnsealResponse struct {
	// Sealed indicates if still sealed
	Sealed bool `json:"sealed"`

	// SharesProvided is shares provided so far
	SharesProvided int `json:"shares_provided"`

	// Threshold is shares still needed
	Threshold int `json:"threshold"`

	// Error contains any error message
	Error string `json:"error,omitempty"`

	// Timestamp is when unseal was attempted
	Timestamp time.Time `json:"timestamp"`
}

// RekeyRequest rotates seal shares.
type RekeyRequest struct {
	// NewThreshold is the new threshold
	NewThreshold int `json:"new_threshold"`

	// NewTotalShares is the new total shares
	NewTotalShares int `json:"new_total_shares"`
}

// RekeyResponse returns new shares.
type RekeyResponse struct {
	// Shares are the new Shamir shares (STORE SECURELY!)
	Shares [][]byte `json:"shares"`

	// Version is the new seal configuration version
	Version uint64 `json:"version"`

	// Timestamp is when rekey occurred
	Timestamp time.Time `json:"timestamp"`
}

// GenerateRecoveryKeyRequest generates new recovery keys.
type GenerateRecoveryKeyRequest struct {
	// RecoveryThreshold is minimum recovery keys needed
	RecoveryThreshold int `json:"recovery_threshold"`

	// RecoveryTotalKeys is total recovery keys to generate
	RecoveryTotalKeys int `json:"recovery_total_keys"`
}

// GenerateRecoveryKeyResponse returns recovery keys.
type GenerateRecoveryKeyResponse struct {
	// RecoveryKeys are the recovery keys (STORE SECURELY!)
	RecoveryKeys [][]byte `json:"recovery_keys"`

	// Timestamp is when recovery keys were generated
	Timestamp time.Time `json:"timestamp"`
}

// DeleteRecoveryKeyRequest deletes recovery keys.
type DeleteRecoveryKeyRequest struct {
	// Confirm must be true to proceed
	Confirm bool `json:"confirm"`
}

// DeleteRecoveryKeyResponse confirms deletion.
type DeleteRecoveryKeyResponse struct {
	// Deleted indicates if recovery keys were deleted
	Deleted bool `json:"deleted"`

	// Timestamp is when deletion occurred
	Timestamp time.Time `json:"timestamp"`
}

// GenerateRootTokenRequest generates a new root token.
type GenerateRootTokenRequest struct {
	// Strategy is the generation strategy (recovery_keys, shamir_shares)
	Strategy string `json:"strategy"`

	// Shares are the shares to use (strategy-dependent)
	Shares [][]byte `json:"shares,omitempty"`
}

// GenerateRootTokenResponse returns a new root token.
type GenerateRootTokenResponse struct {
	// RootToken is the generated root token (STORE SECURELY!)
	RootToken string `json:"root_token"`

	// ExpiresAt is when the token expires (if applicable)
	ExpiresAt *time.Time `json:"expires_at,omitempty"`

	// Timestamp is when token was generated
	Timestamp time.Time `json:"timestamp"`
}

// RotateRootKeyRequest requests rotation of the root encryption key.
type RotateRootKeyRequest struct {
	// Force bypasses safety checks if true
	Force bool `json:"force,omitempty"`
}

// RotateRootKeyResponse confirms root key rotation.
type RotateRootKeyResponse struct {
	// Success indicates the rotation completed successfully
	Success bool `json:"success"`

	// Message provides a human-readable status message
	Message string `json:"message"`

	// Timestamp is when the rotation occurred
	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// DEK (Data Encryption Key) Request/Response Types
// =============================================================================

// DEKStatus represents the status of a DEK.
type DEKStatus string

const (
	// DEKStatusActive indicates the DEK is active and can be used.
	DEKStatusActive DEKStatus = "active"

	// DEKStatusDisabled indicates the DEK is disabled.
	DEKStatusDisabled DEKStatus = "disabled"

	// DEKStatusDeleted indicates the DEK has been deleted.
	DEKStatusDeleted DEKStatus = "deleted"

	// DEKStatusRotating indicates the DEK is being rotated.
	DEKStatusRotating DEKStatus = "rotating"
)

// DEKMetadata contains metadata about a Data Encryption Key.
type DEKMetadata struct {
	// Path is the DEK path/identifier
	Path string `json:"path"`

	// Algorithm is the encryption algorithm
	Algorithm KeyAlgorithm `json:"algorithm"`

	// Version is the DEK version
	Version uint64 `json:"version"`

	// Status is the DEK status
	Status string `json:"status"`

	// IsDefault indicates if this is the default DEK
	IsDefault bool `json:"is_default"`

	// CreatedAt is when the DEK was created
	CreatedAt time.Time `json:"created_at"`

	// RotatedAt is when the DEK was last rotated
	RotatedAt *time.Time `json:"rotated_at,omitempty"`

	// UsageCount is the number of operations using this DEK
	UsageCount uint64 `json:"usage_count"`

	// BytesUsed is the total bytes encrypted with this DEK
	BytesUsed uint64 `json:"bytes_used"`

	// MaxUsageCount is the maximum allowed operations
	MaxUsageCount uint64 `json:"max_usage_count,omitempty"`

	// MaxBytesUsed is the maximum bytes allowed
	MaxBytesUsed uint64 `json:"max_bytes_used,omitempty"`

	// WarningPercent is the percentage threshold for warnings
	WarningPercent int `json:"warning_percent,omitempty"`

	// Description provides context about the DEK
	Description string `json:"description,omitempty"`

	// BackendType is the backend type for this DEK
	BackendType string `json:"backend_type,omitempty"`
}

// CreateDEKRequest requests creation of a new data encryption key.
type CreateDEKRequest struct {
	// Path is the DEK path/identifier
	Path string `json:"path"`

	// Algorithm is the encryption algorithm
	Algorithm KeyAlgorithm `json:"algorithm,omitempty"`

	// Description provides context about the DEK
	Description string `json:"description,omitempty"`

	// MaxOperations is the maximum number of operations
	MaxOperations uint64 `json:"max_operations,omitempty"`

	// MaxBytes is the maximum bytes to encrypt
	MaxBytes uint64 `json:"max_bytes,omitempty"`

	// WarningPercent is the percentage threshold for warnings
	WarningPercent int `json:"warning_percent,omitempty"`

	// Tags are user-defined key-value pairs
	Tags map[string]string `json:"tags,omitempty"`
}

// CreateDEKResponse confirms DEK creation.
type CreateDEKResponse struct {
	// DEK is the created DEK metadata
	DEK *DEKMetadata `json:"dek"`

	// Timestamp is when the DEK was created
	Timestamp time.Time `json:"timestamp"`
}

// GetDEKRequest requests a DEK.
type GetDEKRequest struct {
	// Path is the DEK path/identifier
	Path string `json:"path"`

	// Version optionally specifies a specific version
	Version uint64 `json:"version,omitempty"`
}

// GetDEKResponse returns a DEK handle.
type GetDEKResponse struct {
	// DEK is the DEK metadata
	DEK *DEKMetadata `json:"dek"`

	// Timestamp is when the request was processed
	Timestamp time.Time `json:"timestamp"`
}

// ListDEKsRequest requests a list of DEKs.
type ListDEKsRequest struct {
	// Prefix filters DEKs by path prefix
	Prefix string `json:"prefix,omitempty"`

	// Recursive indicates whether to list recursively
	Recursive bool `json:"recursive,omitempty"`

	// Limit is the maximum number of results
	Limit int `json:"limit,omitempty"`

	// Cursor is the pagination cursor
	Cursor string `json:"cursor,omitempty"`

	// Filter optionally filters by tags
	Filter map[string]string `json:"filter,omitempty"`
}

// ListDEKsResponse returns a list of DEKs.
type ListDEKsResponse struct {
	// DEKs is the list of DEK metadata
	DEKs []*DEKMetadata `json:"deks"`

	// NextCursor is the cursor for the next page
	NextCursor string `json:"next_cursor,omitempty"`

	// HasMore indicates if there are more results
	HasMore bool `json:"has_more"`

	// TotalCount is the total number of DEKs
	TotalCount int `json:"total_count,omitempty"`

	// Timestamp is when the list was generated
	Timestamp time.Time `json:"timestamp"`
}

// RotateDEKRequest requests DEK rotation.
type RotateDEKRequest struct {
	// Path is the DEK path to rotate
	Path string `json:"path"`

	// Background indicates whether to run rotation in background
	Background bool `json:"background,omitempty"`

	// RetainOldVersion indicates if the old version should be retained
	RetainOldVersion bool `json:"retain_old_version"`
}

// RotateDEKResponse confirms DEK rotation.
type RotateDEKResponse struct {
	// Path is the DEK path
	Path string `json:"path"`

	// OldVersion is the previous version number
	OldVersion uint64 `json:"old_version"`

	// NewVersion is the new version number
	NewVersion uint64 `json:"new_version"`

	// Timestamp is when rotation occurred
	Timestamp time.Time `json:"timestamp"`
}

// DeleteDEKRequest requests DEK deletion.
type DeleteDEKRequest struct {
	// Path is the DEK path to delete
	Path string `json:"path"`

	// MigrateTo optionally specifies a path to migrate data to
	MigrateTo string `json:"migrate_to,omitempty"`

	// Force bypasses protection
	Force bool `json:"force"`
}

// DeleteDEKResponse confirms DEK deletion.
type DeleteDEKResponse struct {
	// Path is the DEK path
	Path string `json:"path"`

	// Deleted indicates if deletion succeeded
	Deleted bool `json:"deleted"`

	// MigratedCount is the number of items migrated
	MigratedCount int64 `json:"migrated_count,omitempty"`

	// Timestamp is when deletion occurred
	Timestamp time.Time `json:"timestamp"`
}

// GetDEKStatusRequest requests DEK status.
type GetDEKStatusRequest struct {
	// Path is the DEK path
	Path string `json:"path"`
}

// GetDEKStatusResponse returns DEK status.
type GetDEKStatusResponse struct {
	// DEK is the DEK metadata
	DEK *DEKMetadata `json:"dek"`

	// ReencryptionProgress contains progress if re-encryption is in progress
	ReencryptionProgress *ReencryptionProgress `json:"reencryption_progress,omitempty"`

	// Timestamp is when the request was processed
	Timestamp time.Time `json:"timestamp"`
}

// SetDefaultDEKRequest requests setting a default DEK.
type SetDefaultDEKRequest struct {
	// Path is the DEK path to set as default
	Path string `json:"path"`
}

// SetDefaultDEKResponse confirms default DEK was set.
type SetDefaultDEKResponse struct {
	// Path is the DEK path
	Path string `json:"path"`

	// PreviousDefault is the previous default DEK path (if any)
	PreviousDefault string `json:"previous_default,omitempty"`

	// Timestamp is when the default was set
	Timestamp time.Time `json:"timestamp"`
}

// SetDEKSafetyRequest requests setting DEK safety limits.
type SetDEKSafetyRequest struct {
	// Path is the DEK path
	Path string `json:"path"`

	// MaxOperations is the maximum number of operations
	MaxOperations uint64 `json:"max_operations,omitempty"`

	// MaxBytes is the maximum bytes to encrypt
	MaxBytes uint64 `json:"max_bytes,omitempty"`

	// WarningPercent is the percentage threshold for warnings
	WarningPercent int `json:"warning_percent,omitempty"`
}

// SetDEKSafetyResponse confirms DEK safety was set.
type SetDEKSafetyResponse struct {
	// Path is the DEK path
	Path string `json:"path"`

	// Config is the updated safety config
	Config *DEKSafetyConfig `json:"config"`

	// Timestamp is when the safety was set
	Timestamp time.Time `json:"timestamp"`
}

// DEKSafetyConfig contains AEAD safety configuration for a DEK.
type DEKSafetyConfig struct {
	// MaxOperations is the maximum number of operations
	MaxOperations uint64 `json:"max_operations"`

	// MaxBytes is the maximum bytes to encrypt
	MaxBytes uint64 `json:"max_bytes"`

	// WarningPercent is the percentage threshold for warnings
	WarningPercent int `json:"warning_percent"`
}

// ClearDEKSafetyRequest requests clearing DEK safety.
type ClearDEKSafetyRequest struct {
	// Path is the DEK path
	Path string `json:"path"`
}

// ClearDEKSafetyResponse confirms DEK safety was cleared.
type ClearDEKSafetyResponse struct {
	// Path is the DEK path
	Path string `json:"path"`

	// Cleared indicates if safety was cleared
	Cleared bool `json:"cleared"`

	// Timestamp is when the safety was cleared
	Timestamp time.Time `json:"timestamp"`
}

// SafetyCheckItem represents a single item in a safety check.
type SafetyCheckItem struct {
	// ID is the key or DEK identifier
	ID string `json:"id"`

	// Type is "key" or "dek"
	Type string `json:"type"`

	// OperationCount is the number of operations performed
	OperationCount uint64 `json:"operation_count"`

	// BytesProcessed is the total bytes processed
	BytesProcessed uint64 `json:"bytes_processed"`

	// UsagePercent is the percentage of limit used
	UsagePercent float64 `json:"usage_percent"`

	// MaxOperations is the maximum allowed operations
	MaxOperations uint64 `json:"max_operations"`

	// MaxBytes is the maximum bytes allowed
	MaxBytes uint64 `json:"max_bytes"`
}

// SafetyCheckRequest requests a safety check.
type SafetyCheckRequest struct {
	// ThresholdPercent is the usage threshold percentage to check against
	ThresholdPercent float64 `json:"threshold_percent,omitempty"`
}

// SafetyCheckResponse returns safety check result.
type SafetyCheckResponse struct {
	// Items are the keys/DEKs that exceed the threshold
	Items []SafetyCheckItem `json:"items"`

	// ThresholdPercent is the threshold used
	ThresholdPercent float64 `json:"threshold_percent"`

	// TotalKeysChecked is the number of keys checked
	TotalKeysChecked int `json:"total_keys_checked"`

	// TotalDEKsChecked is the number of DEKs checked
	TotalDEKsChecked int `json:"total_deks_checked"`

	// Timestamp is when the check was performed
	Timestamp time.Time `json:"timestamp"`
}

// SafetyReportRequest requests a safety report.
type SafetyReportRequest struct {
	// IncludeDetails includes detailed info
	IncludeDetails bool `json:"include_details"`
}

// AEADServerConfig contains server-wide AEAD safety configuration.
type AEADServerConfig struct {
	// Enforce indicates if safety limits are enforced
	Enforce bool `json:"enforce"`

	// MaxOperations is the default maximum operations
	MaxOperations uint64 `json:"max_operations"`

	// MaxBytes is the default maximum bytes
	MaxBytes uint64 `json:"max_bytes"`

	// WarningPercent is the warning threshold percentage
	WarningPercent int `json:"warning_percent"`

	// AutoRotatePercent is the auto-rotation threshold percentage
	AutoRotatePercent int `json:"auto_rotate_percent"`
}

// SafetyReportResponse returns safety report.
type SafetyReportResponse struct {
	// Items are all keys/DEKs with their usage
	Items []SafetyCheckItem `json:"items"`

	// TotalOperations is the total operations across all items
	TotalOperations uint64 `json:"total_operations"`

	// TotalBytesProcessed is the total bytes processed
	TotalBytesProcessed uint64 `json:"total_bytes_processed"`

	// ServerConfig is the server-wide AEAD configuration
	ServerConfig AEADServerConfig `json:"server_config"`

	// Timestamp is when the report was generated
	Timestamp time.Time `json:"timestamp"`
}

// GetReencryptionProgressRequest requests re-encryption progress.
type GetReencryptionProgressRequest struct {
	// Path is the DEK path to check (optional)
	Path string `json:"path,omitempty"`

	// OperationID is a specific operation ID to check (optional)
	OperationID string `json:"operation_id,omitempty"`

	// Verbose includes additional details
	Verbose bool `json:"verbose,omitempty"`
}

// GetReencryptionProgressResponse returns re-encryption progress.
type GetReencryptionProgressResponse struct {
	// Path is the DEK path being re-encrypted
	Path string `json:"path,omitempty"`

	// OperationID is the operation identifier
	OperationID string `json:"operation_id,omitempty"`

	// Progress contains the re-encryption progress
	Progress *ReencryptionProgress `json:"progress"`

	// Status is the current operation status (running, completed, failed, not_started)
	Status string `json:"status"`

	// Error contains error message if failed
	Error string `json:"error,omitempty"`

	// Timestamp is when the progress was retrieved
	Timestamp time.Time `json:"timestamp"`
}

// ExportKeyRequest requests key export.
type ExportKeyRequest struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`

	// Version optionally specifies a specific version to export
	Version uint64 `json:"version,omitempty"`

	// WrappingKeyID is the key used to wrap the exported key
	WrappingKeyID string `json:"wrapping_key_id"`

	// WrappingAlgorithm specifies the wrapping algorithm
	WrappingAlgorithm string `json:"wrapping_algorithm,omitempty"`

	// Format specifies the export format
	Format string `json:"format,omitempty"`
}

// ExportKeyResponse returns the exported key.
type ExportKeyResponse struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`

	// Version is the key version that was exported
	Version uint64 `json:"version,omitempty"`

	// WrappedKey is the wrapped key material
	WrappedKey []byte `json:"wrapped_key"`

	// WrappingKeyID is the key used to wrap
	WrappingKeyID string `json:"wrapping_key_id"`

	// WrappingAlgorithm is the algorithm used for wrapping
	WrappingAlgorithm string `json:"wrapping_algorithm"`

	// Format is the export format used
	Format string `json:"format"`

	// Timestamp is when the export occurred
	Timestamp time.Time `json:"timestamp"`
}

// UnwrapKeyRequest requests key unwrapping.
type UnwrapKeyRequest struct {
	// KeyID is the identifier for the new key after unwrapping
	KeyID string `json:"key_id"`

	// WrappedKey is the wrapped key material
	WrappedKey []byte `json:"wrapped_key"`

	// WrappingKeyID is the key used to unwrap
	WrappingKeyID string `json:"wrapping_key_id"`

	// WrappingAlgorithm is the algorithm to use for unwrapping
	WrappingAlgorithm string `json:"wrapping_algorithm"`

	// Algorithm is the algorithm of the unwrapped key
	Algorithm string `json:"algorithm"`

	// SourceKeyID is the original key ID that was exported (required for AAD verification)
	SourceKeyID string `json:"source_key_id,omitempty"`

	// SourceVersion is the version of the source key that was exported (required for AAD verification)
	SourceVersion uint64 `json:"source_version,omitempty"`

	// KeyAttributes specifies the unwrapped key attributes from go-xkms
	KeyAttributes *kctypes.KeyAttributes `json:"key_attributes,omitempty"`

	// Name is the name for the unwrapped key
	Name string `json:"name,omitempty"`
}

// UnwrapKeyResponse returns the unwrapped key handle.
type UnwrapKeyResponse struct {
	// KeyID is the new key identifier
	KeyID string `json:"key_id"`

	// Version is the key version
	Version uint64 `json:"version"`

	// Algorithm is the key algorithm
	Algorithm KeyAlgorithm `json:"algorithm"`

	// Status is the key status
	Status KeyStatus `json:"status"`

	// Key is the key handle (optional)
	Key *KeyHandle `json:"key,omitempty"`

	// Timestamp is when the unwrap occurred
	Timestamp time.Time `json:"timestamp"`
}

// SetKeySafetyRequest requests setting key safety.
type SetKeySafetyRequest struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`

	// MaxOperations is the maximum number of operations
	MaxOperations uint64 `json:"max_operations,omitempty"`

	// MaxBytes is the maximum bytes to process
	MaxBytes uint64 `json:"max_bytes,omitempty"`

	// WarningPercent is the warning threshold percentage
	WarningPercent int `json:"warning_percent,omitempty"`

	// AutoRotatePercent is the auto-rotate threshold percentage
	AutoRotatePercent int `json:"auto_rotate_percent,omitempty"`
}

// AEADSafetyConfigDTO is the data transfer object for AEAD safety configuration.
type AEADSafetyConfigDTO struct {
	// MaxOperations is the maximum number of operations
	MaxOperations uint64 `json:"max_operations,omitempty"`

	// MaxBytes is the maximum bytes to process
	MaxBytes uint64 `json:"max_bytes,omitempty"`

	// WarningPercent is the warning threshold percentage
	WarningPercent int `json:"warning_percent,omitempty"`

	// AutoRotatePercent is the auto-rotate threshold percentage
	AutoRotatePercent int `json:"auto_rotate_percent,omitempty"`
}

// SetKeySafetyResponse confirms key safety was set.
type SetKeySafetyResponse struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`

	// Config is the safety configuration that was set
	Config AEADSafetyConfigDTO `json:"config"`

	// Timestamp is when the safety was set
	Timestamp time.Time `json:"timestamp"`
}

// ClearKeySafetyRequest requests clearing key safety config.
type ClearKeySafetyRequest struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`
}

// ClearKeySafetyResponse confirms key safety config was cleared.
type ClearKeySafetyResponse struct {
	// KeyID is the key identifier
	KeyID string `json:"key_id"`

	// Timestamp is when the safety was cleared
	Timestamp time.Time `json:"timestamp"`
}

// KeyVersionInfo represents information about a specific key version.
type KeyVersionInfo struct {
	// KeyID is the parent key identifier (populated from response context)
	KeyID string `json:"key_id,omitempty"`

	// Version is the key version number
	Version uint64 `json:"version"`

	// Status is the version status
	Status KeyStatus `json:"status"`

	// Algorithm is the key algorithm
	Algorithm KeyAlgorithm `json:"algorithm"`

	// CreatedAt is when the version was created
	CreatedAt time.Time `json:"created_at"`

	// DisabledAt is when the version was disabled (optional)
	DisabledAt *time.Time `json:"disabled_at,omitempty"`

	// GracePeriodJobID is the scheduler job ID that will disable this version.
	// Empty if no grace period is scheduled.
	GracePeriodJobID string `json:"grace_period_job_id,omitempty"`

	// GracePeriodDisableAt is when this version will be automatically disabled.
	// Zero if no grace period is scheduled.
	GracePeriodDisableAt int64 `json:"grace_period_disable_at,omitempty"`
}

// =============================================================================
// Backend Request/Response Types
// =============================================================================

// BackendStatusResponse contains information about the KMS backend.
type BackendStatusResponse struct {
	// BackendType is the backend type (tpm2, awskms, azurekms, gcpkms, software, etc.)
	BackendType string `json:"backend_type"`

	// Status is the backend status (healthy, degraded, unavailable)
	Status string `json:"status"`

	// Device is the device path for TPM backends
	Device string `json:"device,omitempty"`

	// Region is the region for cloud KMS backends
	Region string `json:"region,omitempty"`

	// KeyID is the key identifier
	KeyID string `json:"key_id,omitempty"`

	// PCRBanks are the PCR banks available (TPM only)
	PCRBanks []string `json:"pcr_banks,omitempty"`

	// PCRRegisters are the PCR registers in use (TPM only)
	PCRRegisters []int `json:"pcr_registers,omitempty"`

	// Capabilities indicate what operations the backend supports
	Capabilities map[string]bool `json:"capabilities"`

	// Timestamp is when the status was retrieved
	Timestamp time.Time `json:"timestamp"`
}

// BackendTestResponse contains the result of a backend test cycle.
type BackendTestResponse struct {
	// Success indicates if the test succeeded
	Success bool `json:"success"`

	// Duration is how long the test took
	Duration time.Duration `json:"duration"`

	// Message contains additional information or error details
	Message string `json:"message,omitempty"`

	// Timestamp is when the test was performed
	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// Application Secrets Types - Bulk Secret Retrieval by Path Prefix
// =============================================================================

// GetSecretsByPrefixRequest fetches all secrets matching a path prefix.
// This is useful for applications that need to mount all their secrets at once.
type GetSecretsByPrefixRequest struct {
	// Prefix is the path prefix to match (e.g., "/apps/myapp/")
	Prefix string `json:"prefix"`

	// IncludeData includes the secret data in the response (default: true)
	// Set to false for metadata-only listing
	IncludeData bool `json:"include_data"`

	// Limit is the maximum number of secrets to return
	Limit int `json:"limit,omitempty"`

	// Cursor is the pagination cursor for subsequent requests
	Cursor string `json:"cursor,omitempty"`
}

// GetSecretsByPrefixResponse returns all secrets matching a prefix.
type GetSecretsByPrefixResponse struct {
	// Secrets maps path to secret data (key -> value)
	Secrets map[string]map[string][]byte `json:"secrets"`

	// Metadata maps path to secret metadata
	Metadata map[string]map[string]string `json:"metadata,omitempty"`

	// Version is the aggregate version for cache invalidation
	Version int64 `json:"version"`

	// NextCursor is the pagination cursor for the next page
	NextCursor string `json:"next_cursor,omitempty"`

	// HasMore indicates if there are more secrets
	HasMore bool `json:"has_more"`

	// TotalCount is the total number of secrets matching the prefix
	TotalCount int `json:"total_count"`

	// Timestamp is when the response was generated
	Timestamp time.Time `json:"timestamp"`
}

// WatchSecretsRequest watches for secret changes under a prefix.
type WatchSecretsRequest struct {
	// Prefix is the path prefix to watch (e.g., "/apps/myapp/")
	Prefix string `json:"prefix"`

	// StartVersion starts watching from this version (0 for current)
	StartVersion int64 `json:"start_version,omitempty"`
}

// SecretChangeType indicates what kind of change occurred.
type SecretChangeType string

const (
	// SecretChangeCreated indicates a new secret was created.
	SecretChangeCreated SecretChangeType = "created"

	// SecretChangeUpdated indicates an existing secret was updated.
	SecretChangeUpdated SecretChangeType = "updated"

	// SecretChangeDeleted indicates a secret was deleted.
	SecretChangeDeleted SecretChangeType = "deleted"
)

// SecretChange represents a change to a secret.
type SecretChange struct {
	// Path is the full path of the changed secret
	Path string `json:"path"`

	// ChangeType indicates what happened (created, updated, deleted)
	ChangeType SecretChangeType `json:"change_type"`

	// Data is the new secret data (nil for deletes)
	Data map[string][]byte `json:"data,omitempty"`

	// Metadata is the secret metadata
	Metadata map[string]string `json:"metadata,omitempty"`

	// Version is the version after this change
	Version int64 `json:"version"`

	// Timestamp is when the change occurred
	Timestamp time.Time `json:"timestamp"`
}

// SetDefaultKeyRequest requests setting a key as the system default.
type SetDefaultKeyRequest struct {
	// Name is the key name to set as default
	Name string `json:"name"`
}

// SetDefaultKeyResponse confirms a key was set as the system default.
type SetDefaultKeyResponse struct {
	// Name is the key name
	Name string `json:"name"`

	// IsDefault is always true on success
	IsDefault bool `json:"is_default"`

	// PreviousDefault is the name of the previous default key (if any)
	PreviousDefault string `json:"previous_default,omitempty"`

	// Timestamp is when the default was set
	Timestamp time.Time `json:"timestamp"`
}
