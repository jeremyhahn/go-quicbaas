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

// Package crypto provides encryption-as-a-service for QuicBaaS.
// It delegates all cryptographic operations to go-xkms.
package crypto

import (
	"time"
)

// KeyScope defines the granularity level for encryption keys.
type KeyScope string

const (
	// ScopeRoot is the root key used for unsealing (Shamir shares).
	ScopeRoot KeyScope = "root"

	// ScopeMaster is the master encryption key that encrypts DEKs.
	ScopeMaster KeyScope = "master"

	// ScopeData is a general data encryption key.
	ScopeData KeyScope = "data"

	// ScopeTable is a per-table encryption key.
	ScopeTable KeyScope = "table"

	// ScopeShard is a per-shard encryption key.
	ScopeShard KeyScope = "shard"

	// ScopeField is a per-field encryption key.
	ScopeField KeyScope = "field"
)

// RotationStrategy defines how key rotation is handled.
type RotationStrategy string

const (
	// RotationImmediate re-encrypts all data immediately on rotation.
	RotationImmediate RotationStrategy = "immediate"

	// RotationEnvelope keeps old DEKs and rewraps with new MEK.
	// Data remains encrypted with original DEK; only DEK encryption changes.
	RotationEnvelope RotationStrategy = "envelope"

	// RotationLazy re-encrypts data on next read/write access.
	RotationLazy RotationStrategy = "lazy"
)

// KeyMetadata describes an encryption key in the hierarchy.
type KeyMetadata struct {
	// ID is the unique identifier for the key.
	ID string

	// Name is the human-readable key name.
	Name string

	// Scope defines the key's granularity level.
	Scope KeyScope

	// Version is the current key version (for rotation).
	Version int

	// Algorithm is the encryption algorithm (e.g., "AES-256-GCM").
	Algorithm string

	// ParentID is the ID of the key that encrypts this key.
	// Empty for root keys.
	ParentID string

	// RotationStrategy defines how rotation is handled.
	RotationStrategy RotationStrategy

	// Entity identifies the owning entity (table, shard, field name).
	Entity string

	// CreatedAt is when the key was created.
	CreatedAt time.Time

	// RotatedAt is when the key was last rotated.
	RotatedAt time.Time

	// ExpiresAt is when the key should be rotated (optional).
	ExpiresAt *time.Time

	// Metadata holds additional key attributes.
	Metadata map[string]string
}

// KeyConfig configures a new key in the hierarchy.
type KeyConfig struct {
	// Name for the key.
	Name string

	// Scope defines the key's granularity.
	Scope KeyScope

	// Algorithm to use (defaults to AES-256-GCM).
	Algorithm string

	// ParentName is the name of the parent key.
	// Required for non-root keys.
	ParentName string

	// RotationStrategy for this key.
	RotationStrategy RotationStrategy

	// Entity identifier (table name, shard ID, field path).
	Entity string

	// AutoRotateAfter triggers automatic rotation after this duration.
	AutoRotateAfter time.Duration

	// Metadata for the key.
	Metadata map[string]string
}

// EncryptedKey holds a key encrypted by its parent.
type EncryptedKey struct {
	// KeyID is the ID of the encrypted key.
	KeyID string

	// EncryptedMaterial is the key material encrypted by the parent key.
	EncryptedMaterial []byte

	// ParentKeyID is the ID of the encrypting key.
	ParentKeyID string

	// ParentVersion is the version of the parent key used.
	ParentVersion int

	// Nonce used for encryption.
	Nonce []byte
}

// DataKey is a plaintext DEK with its encrypted form.
type DataKey struct {
	// Plaintext is the raw key material (should be zeroed after use).
	Plaintext []byte

	// Encrypted is the key encrypted by its parent.
	Encrypted *EncryptedKey

	// Metadata about this key.
	Metadata *KeyMetadata
}

// EncryptRequest is a request to encrypt data.
type EncryptRequest struct {
	// KeyName is the name of the key to use.
	KeyName string

	// Plaintext is the data to encrypt.
	Plaintext []byte

	// Context is additional authenticated data.
	Context []byte

	// KeyVersion forces a specific key version (0 = latest).
	KeyVersion int
}

// EncryptResponse is the result of encryption.
type EncryptResponse struct {
	// Ciphertext is the encrypted data (nonce prepended by go-xkms).
	Ciphertext []byte

	// KeyID used for encryption.
	KeyID string

	// KeyVersion used for encryption.
	KeyVersion int
}

// DecryptRequest is a request to decrypt data.
type DecryptRequest struct {
	// KeyName is the name of the key to use.
	KeyName string

	// Ciphertext is the data to decrypt (nonce prepended by go-xkms).
	Ciphertext []byte

	// Context is additional authenticated data.
	Context []byte

	// KeyVersion that was used for encryption.
	KeyVersion int
}

// DecryptResponse is the result of decryption.
type DecryptResponse struct {
	// Plaintext is the decrypted data.
	Plaintext []byte
}

// RewrapRequest is a request to rewrap ciphertext with a new key version.
type RewrapRequest struct {
	// KeyName is the name of the key.
	KeyName string

	// Ciphertext encrypted with an older version.
	Ciphertext []byte

	// Context is additional authenticated data.
	Context []byte

	// OldKeyVersion that was used for encryption.
	OldKeyVersion int

	// NewKeyVersion to rewrap with (0 = latest).
	NewKeyVersion int
}

// RewrapResponse is the result of rewrapping.
type RewrapResponse struct {
	// Ciphertext encrypted with the new key version.
	Ciphertext []byte

	// NewKeyVersion used for re-encryption.
	NewKeyVersion int
}

// FieldEncryptionConfig defines field-level encryption settings.
type FieldEncryptionConfig struct {
	// FieldPath is the path to the field (e.g., "user.ssn").
	FieldPath string

	// KeyName is the encryption key to use.
	KeyName string

	// Deterministic enables deterministic encryption for searchability.
	// WARNING: Less secure, enables equality comparisons.
	Deterministic bool

	// PreserveFormat maintains the original format (e.g., for credit cards).
	PreserveFormat bool

	// Mask defines how to display the encrypted field (e.g., "****1234").
	Mask string
}

// TableEncryptionConfig defines table-level encryption settings.
type TableEncryptionConfig struct {
	// TableName is the table being configured.
	TableName string

	// KeyName is the default encryption key for the table.
	KeyName string

	// RotationStrategy for this table.
	RotationStrategy RotationStrategy

	// Fields defines field-level encryption overrides.
	Fields map[string]*FieldEncryptionConfig

	// EncryptAllFields encrypts all fields with the table key.
	EncryptAllFields bool
}

// ShardEncryptionConfig defines shard-level encryption settings.
type ShardEncryptionConfig struct {
	// ShardID is the shard identifier.
	ShardID string

	// KeyName is the encryption key for this shard.
	KeyName string

	// RotationStrategy for this shard.
	RotationStrategy RotationStrategy
}
