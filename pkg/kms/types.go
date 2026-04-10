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

package kms

import (
	"context"
	stderrors "errors"
	"time"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// =============================================================================
// Errors
// =============================================================================

// ErrKeyNotFound indicates a key was not found.
var ErrKeyNotFound = stderrors.New("key not found")

// ErrNilStorage indicates storage is nil.
var ErrNilStorage = stderrors.New("storage cannot be nil")

// =============================================================================
// Key State
// =============================================================================

// KeyState represents the lifecycle state of a key in the backend.
type KeyState string

const (
	// KeyStateEnabled indicates the key is active and can be used.
	KeyStateEnabled KeyState = "enabled"

	// KeyStateDisabled indicates the key is temporarily disabled.
	KeyStateDisabled KeyState = "disabled"

	// KeyStatePendingDeletion indicates the key is scheduled for deletion.
	KeyStatePendingDeletion KeyState = "pending_deletion"

	// KeyStateDeleted indicates the key has been permanently deleted.
	KeyStateDeleted KeyState = "deleted"
)

// =============================================================================
// Key Usage
// =============================================================================

// KeyUsage represents permitted operations for a key.
type KeyUsage string

const (
	// KeyUsageEncrypt allows encryption operations.
	KeyUsageEncrypt KeyUsage = "encrypt"

	// KeyUsageDecrypt allows decryption operations.
	KeyUsageDecrypt KeyUsage = "decrypt"

	// KeyUsageSign allows signing operations.
	KeyUsageSign KeyUsage = "sign"

	// KeyUsageVerify allows signature verification.
	KeyUsageVerify KeyUsage = "verify"

	// KeyUsageWrap allows key wrapping operations.
	KeyUsageWrap KeyUsage = "wrap"

	// KeyUsageUnwrap allows key unwrapping operations.
	KeyUsageUnwrap KeyUsage = "unwrap"

	// KeyUsageDeriveKey allows key derivation.
	KeyUsageDeriveKey KeyUsage = "derive_key"
)

// =============================================================================
// Rotation Policy
// =============================================================================

// RotationPolicy defines automatic key rotation settings.
type RotationPolicy struct {
	// Enabled indicates if automatic rotation is enabled.
	Enabled bool `json:"enabled"`

	// Interval is the duration between rotations.
	Interval time.Duration `json:"interval"`

	// NextRotation is when the next rotation is scheduled.
	NextRotation time.Time `json:"next_rotation"`

	// RetainVersions is how many old versions to keep.
	RetainVersions int `json:"retain_versions"`

	// AutoRotate indicates if rotation happens automatically.
	AutoRotate bool `json:"auto_rotate"`
}

// =============================================================================
// Key Metadata (Backend)
// =============================================================================

// KeyMetadata contains non-sensitive metadata about a key as stored in the backend.
// This is the internal representation used by Backend implementations.
type KeyMetadata struct {
	// KeyID is the unique identifier
	KeyID string `json:"key_id"`

	// Name is the human-readable key name
	Name string `json:"name,omitempty"`

	// Description provides context about the key
	Description string `json:"description,omitempty"`

	// Algorithm is the cryptographic algorithm
	Algorithm string `json:"algorithm"`

	// State is the current key state
	State KeyState `json:"state"`

	// Version is the current key version
	Version int `json:"version"`

	// Exportable indicates if the key can be exported
	Exportable bool `json:"exportable"`

	// DeletionProtection prevents accidental deletion
	DeletionProtection bool `json:"deletion_protection"`

	// Created is when the key was created
	Created time.Time `json:"created_at"`

	// Updated is when the key was last modified
	Updated time.Time `json:"updated_at"`

	// LastUsedAt is when the key was last used for crypto operations
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`

	// UsageCount tracks total operations
	UsageCount uint64 `json:"usage_count"`

	// Usage specifies permitted operations
	Usage []KeyUsage `json:"usage,omitempty"`

	// RotationPolicy is the automatic rotation policy
	RotationPolicy *RotationPolicy `json:"rotation_policy,omitempty"`

	// Tags are key-value pairs for organization
	Tags map[string]string `json:"tags,omitempty"`

	// Owner is the owning user or service
	Owner string `json:"owner,omitempty"`
}

// =============================================================================
// Backend Interface
// =============================================================================

// Backend defines the interface that KMS storage backends must implement.
// Implementations include: memory, local file, Vault, AWS KMS, etc.
type Backend interface {
	// Type returns the backend type identifier (e.g., BackendTypeSoftware, BackendTypeVault).
	// Uses config.BackendType which is an alias for go-xkms's types.BackendType.
	Type() config.BackendType

	// IsAvailable checks if the backend is currently available.
	IsAvailable(ctx context.Context) bool

	// SupportedAlgorithms returns the list of cryptographic algorithms
	// supported by this backend. Different backends may support different
	// algorithms (e.g., ChaCha20-Poly1305 is only supported by the software backend).
	SupportedAlgorithms(ctx context.Context) []string

	// CreateKey creates a new cryptographic key with the specified attributes.
	// attrs contains the key configuration from go-xkms including:
	//   - CN: common name/identifier for the key
	//   - KeyAlgorithm: public key algorithm (RSA, ECDSA, Ed25519)
	//   - SymmetricAlgorithm: symmetric algorithm (AES-GCM, ChaCha20-Poly1305)
	//   - StoreType: backend storage type
	//   - RSAAttributes/ECCAttributes: algorithm-specific configuration
	CreateKey(ctx context.Context, attrs *kctypes.KeyAttributes) (*KeyMetadata, error)

	// ImportKey imports external key material.
	// Options map contains backend-specific import options.
	ImportKey(ctx context.Context, keyID string, material []byte, algorithm string, options map[string]interface{}) (*KeyMetadata, error)

	// GetKeyMetadata retrieves metadata for a key.
	GetKeyMetadata(ctx context.Context, keyID string) (*KeyMetadata, error)

	// ListKeys lists keys matching the prefix.
	// Returns metadata list, next cursor for pagination, and any error.
	ListKeys(ctx context.Context, prefix string, limit int) ([]*KeyMetadata, string, error)

	// RotateKey rotates a key to a new version.
	RotateKey(ctx context.Context, keyID string) (*KeyMetadata, error)

	// DisableKey temporarily disables a key.
	DisableKey(ctx context.Context, keyID string) error

	// EnableKey enables a disabled key.
	EnableKey(ctx context.Context, keyID string) error

	// DeleteKey schedules a key for deletion or permanently deletes it.
	DeleteKey(ctx context.Context, keyID string) error

	// Encrypt encrypts plaintext using the specified key.
	// AAD is optional additional authenticated data.
	Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error)

	// Decrypt decrypts ciphertext using the specified key.
	// AAD must match what was used during encryption.
	Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error)

	// Sign creates a digital signature for the digest.
	// Algorithm specifies the signing algorithm (e.g., "SHA256").
	Sign(ctx context.Context, keyID string, digest []byte, algorithm string) ([]byte, error)

	// Verify verifies a digital signature.
	// Algorithm must match what was used during signing.
	Verify(ctx context.Context, keyID string, digest, signature []byte, algorithm string) (bool, error)

	// GenerateDataKey generates a new data encryption key (DEK).
	// Returns the plaintext key, wrapped key, and any error.
	GenerateDataKey(ctx context.Context, keyID string, keyBytes int) (plaintext, wrapped []byte, err error)

	// ExportPublicKey exports the DER-encoded public key for an asymmetric key.
	// Returns an error if the key is symmetric or the public key cannot be exported.
	ExportPublicKey(ctx context.Context, keyID string) ([]byte, error)

	// Close releases any resources held by the backend.
	Close() error
}
