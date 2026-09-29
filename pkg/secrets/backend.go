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

// Package secrets provides secret storage and retrieval functionality for QuicBaaS.
// It supports multiple backend implementations including internal storage, HashiCorp Vault,
// Kubernetes Secrets, environment variables, and file-based storage.
package secrets

import (
	"context"
	"errors"
	"time"
)

// Backend defines the interface for secrets storage backends.
// All implementations must be thread-safe and handle context cancellation properly.
type Backend interface {
	// Store saves a secret at the specified path.
	// data contains the secret key-value pairs.
	// metadata contains non-sensitive information about the secret.
	Store(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error

	// Get retrieves a secret from the specified path.
	// Returns the secret data and metadata.
	Get(ctx context.Context, path string) (data map[string][]byte, metadata map[string]string, err error)

	// GetVersion retrieves a specific version of a secret.
	GetVersion(ctx context.Context, path string, version int) (data map[string][]byte, metadata map[string]string, err error)

	// List returns all secret paths matching the prefix.
	List(ctx context.Context, prefix string) ([]string, error)

	// Delete removes a secret (soft delete, keeps versions).
	Delete(ctx context.Context, path string) error

	// Destroy permanently removes all versions of a secret.
	Destroy(ctx context.Context, path string) error

	// ListVersions returns metadata for all versions of a secret.
	ListVersions(ctx context.Context, path string) ([]*VersionMetadata, error)

	// Rollback restores a previous version as the current version.
	Rollback(ctx context.Context, path string, version int) error

	// Type returns the backend type identifier.
	// Valid types: "internal", "vault", "k8s", "env", "file"
	Type() string

	// Close releases any resources held by the backend.
	Close() error
}

// SecretEntry represents a secret with its data and metadata.
type SecretEntry struct {
	// Path is the unique path/key for the secret.
	Path string

	// Data contains the secret key-value pairs.
	Data map[string][]byte

	// Metadata contains non-sensitive information about the secret.
	Metadata map[string]string

	// Version is the current version number.
	Version int

	// Deleted indicates if the secret has been soft-deleted.
	Deleted bool

	// CreatedAt is when the secret was first created.
	CreatedAt time.Time

	// UpdatedAt is when the secret was last updated.
	UpdatedAt time.Time

	// CreatedBy is the authenticated principal that minted this version,
	// empty when it was written with no principal in context (#862).
	CreatedBy string

	// DeletedAt is when the secret was deleted (nil if not deleted).
	DeletedAt *time.Time
}

// VersionMetadata contains metadata about a specific version of a secret.
type VersionMetadata struct {
	// Version is the version number.
	Version int

	// CreatedAt is when this version was created.
	CreatedAt time.Time

	// CreatedBy is the authenticated principal that minted this version,
	// empty when it was written with no principal in context (#862).
	CreatedBy string

	// Deleted indicates if this version has been destroyed.
	Deleted bool

	// DeletedAt is when this version was destroyed (nil if not destroyed).
	DeletedAt *time.Time
}

// Common errors returned by Backend implementations.
var (
	// ErrSecretNotFound indicates the requested secret does not exist.
	ErrSecretNotFound = errors.New("secrets: secret not found")

	// ErrSecretDeleted indicates the secret has been deleted.
	ErrSecretDeleted = errors.New("secrets: secret has been deleted")

	// ErrVersionNotFound indicates the requested version does not exist.
	ErrVersionNotFound = errors.New("secrets: version not found")

	// ErrVersionSpaceExhausted indicates version allocation reached the
	// integer ceiling and refuses to wrap: a wrapped allocation would write
	// a negative version key that every scan ignores, silently corrupting
	// the path's history.
	ErrVersionSpaceExhausted = errors.New("secrets: version space exhausted")

	// ErrWriteConflict indicates a guarded (storage-arbitrated) write was
	// refused because a concurrent writer moved the record first. The
	// caller's observation is stale; re-read and re-derive.
	ErrWriteConflict = errors.New("secrets: concurrent writer moved the record")

	// ErrNilCallback indicates a guarded-use read was given no callback.
	ErrNilCallback = errors.New("secrets: nil callback")

	// ErrSecretKeyAbsent indicates the secret record exists but holds no
	// (nonempty) value under the requested key. Distinct from
	// ErrSecretNotFound: the path is present, the field is not.
	ErrSecretKeyAbsent = errors.New("secrets: key absent or empty in secret record")

	// ErrInvalidPath indicates the path is malformed or empty.
	ErrInvalidPath = errors.New("secrets: invalid path")

	// ErrEmptyData indicates no data was provided for storage.
	ErrEmptyData = errors.New("secrets: secret data cannot be empty")

	// ErrBackendUnavailable indicates the backend is not currently accessible.
	ErrBackendUnavailable = errors.New("secrets: backend unavailable")

	// ErrVersionDestroyed indicates the version has been permanently destroyed.
	ErrVersionDestroyed = errors.New("secrets: version has been destroyed")

	// ErrNilOptions indicates nil options were provided.
	ErrNilOptions = errors.New("secrets: nil options")

	// ErrNilStore indicates nil store was provided.
	ErrNilStore = errors.New("secrets: nil store")

	// ErrNilEncryptor indicates nil encryptor was provided.
	ErrNilEncryptor = errors.New("secrets: nil encryptor")

	// ErrUnsupportedAlgorithm indicates an unsupported encryption algorithm.
	ErrUnsupportedAlgorithm = errors.New("secrets: unsupported algorithm")

	// ErrSerializationFailed indicates serialization of data failed.
	ErrSerializationFailed = errors.New("secrets: serialization failed")

	// ErrDeserializationFailed indicates deserialization of data failed.
	ErrDeserializationFailed = errors.New("secrets: deserialization failed")

	// ErrMissingKeyIdentifier indicates a required key identifier was not provided.
	ErrMissingKeyIdentifier = errors.New("secrets: missing key identifier")

	// ErrEncryptionFailed indicates encryption of secret data failed.
	ErrEncryptionFailed = errors.New("secrets: encryption failed")

	// ErrDecryptionFailed indicates decryption of secret data failed.
	ErrDecryptionFailed = errors.New("secrets: decryption failed")

	// ErrVersionMustBePositive indicates version must be a positive integer.
	ErrVersionMustBePositive = errors.New("secrets: version must be positive")

	// ErrCloseBackendFailed indicates closing the backend failed.
	ErrCloseBackendFailed = errors.New("secrets: failed to close backend")
)

// SecretsSentinelCauseError wraps a sentinel error with a cause using multi-unwrap.
type SecretsSentinelCauseError struct {
	Sentinel error
	Cause    error
}

// Error implements the error interface.
func (e *SecretsSentinelCauseError) Error() string {
	return e.Sentinel.Error() + ": " + e.Cause.Error()
}

// Unwrap returns both errors for multi-unwrap support (Go 1.20+).
func (e *SecretsSentinelCauseError) Unwrap() []error {
	return []error{e.Sentinel, e.Cause}
}

// SecretsCauseError wraps a secrets operation error with a cause.
type SecretsCauseError struct {
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *SecretsCauseError) Error() string {
	return e.Message + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *SecretsCauseError) Unwrap() error {
	return e.Cause
}
