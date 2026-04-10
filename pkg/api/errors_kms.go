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

package api

import (
	"fmt"
	"strconv"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// KMS Error Types
// =============================================================================

// BackendRoutingError indicates a failure routing to a KMS backend.
type BackendRoutingError struct {
	Cause error
}

// Error implements the error interface.
func (e *BackendRoutingError) Error() string {
	return "failed to route to backend: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *BackendRoutingError) Unwrap() error {
	return e.Cause
}

// NewBackendRoutingError creates a DragonError for a backend routing failure.
func NewBackendRoutingError(op string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrUnavailable,
		Op:   op,
		Err:  &BackendRoutingError{Cause: cause},
	}
}

// InvalidAlgorithmError indicates an unsupported or invalid cryptographic algorithm.
type InvalidAlgorithmError struct {
	Algorithm string
}

// Error implements the error interface.
func (e *InvalidAlgorithmError) Error() string {
	return "unsupported algorithm: " + e.Algorithm
}

// NewInvalidAlgorithmError creates a DragonError for an invalid algorithm.
func NewInvalidAlgorithmError(op, algorithm string) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &InvalidAlgorithmError{Algorithm: algorithm},
	}
}

// KeyDisabledError indicates a key is disabled and cannot be used.
type KeyDisabledError struct {
	KeyID string
}

// Error implements the error interface.
func (e *KeyDisabledError) Error() string {
	return "key " + e.KeyID + " is disabled"
}

// NewKeyDisabledError creates a DragonError for a disabled key.
func NewKeyDisabledError(op, keyID string) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &KeyDisabledError{KeyID: keyID},
	}
}

// KeyDeletedError indicates a key has been deleted and cannot be used.
type KeyDeletedError struct {
	KeyID string
}

// Error implements the error interface.
func (e *KeyDeletedError) Error() string {
	return "key " + e.KeyID + " is deleted"
}

// NewKeyDeletedError creates a DragonError for a deleted key.
func NewKeyDeletedError(op, keyID string) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &KeyDeletedError{KeyID: keyID},
	}
}

// KeyPendingDeletionError indicates a key is pending deletion and cannot be used.
type KeyPendingDeletionError struct {
	KeyID string
}

// Error implements the error interface.
func (e *KeyPendingDeletionError) Error() string {
	return "key " + e.KeyID + " is pending deletion"
}

// NewKeyPendingDeletionError creates a DragonError for a key pending deletion.
func NewKeyPendingDeletionError(op, keyID string) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &KeyPendingDeletionError{KeyID: keyID},
	}
}

// AlgorithmOperationUnsupportedError indicates an algorithm does not support the requested operation.
type AlgorithmOperationUnsupportedError struct {
	Algorithm string
	Operation string
}

// Error implements the error interface.
func (e *AlgorithmOperationUnsupportedError) Error() string {
	return "algorithm " + e.Algorithm + " does not support operation " + e.Operation
}

// NewAlgorithmOperationUnsupportedError creates a DragonError for an unsupported algorithm operation.
func NewAlgorithmOperationUnsupportedError(op, algorithm, operation string) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &AlgorithmOperationUnsupportedError{Algorithm: algorithm, Operation: operation},
	}
}

// UnknownAlgorithmError indicates an unrecognized algorithm name.
type UnknownAlgorithmError struct {
	Name string
}

// Error implements the error interface.
func (e *UnknownAlgorithmError) Error() string {
	return "unknown algorithm: " + e.Name
}

// NewUnknownAlgorithmError creates a DragonError for an unknown algorithm.
func NewUnknownAlgorithmError(op, name string) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &UnknownAlgorithmError{Name: name},
	}
}

// AlgorithmSigningUnsupportedError indicates an algorithm does not support signing.
type AlgorithmSigningUnsupportedError struct {
	Algorithm string
}

// Error implements the error interface.
func (e *AlgorithmSigningUnsupportedError) Error() string {
	return "algorithm " + e.Algorithm + " does not support signing"
}

// NewAlgorithmSigningUnsupportedError creates a DragonError for an algorithm that does not support signing.
func NewAlgorithmSigningUnsupportedError(op, algorithm string) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &AlgorithmSigningUnsupportedError{Algorithm: algorithm},
	}
}

// FailoverExhaustedError indicates all failover attempts have been exhausted.
type FailoverExhaustedError struct {
	Attempts int
	Cause    error
}

// Error implements the error interface.
func (e *FailoverExhaustedError) Error() string {
	return "all failover attempts failed after " + strconv.Itoa(e.Attempts) + " tries: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *FailoverExhaustedError) Unwrap() error {
	return e.Cause
}

// NewFailoverExhaustedError creates a DragonError for exhausted failover attempts.
func NewFailoverExhaustedError(op string, attempts int, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrUnavailable,
		Op:   op,
		Err:  &FailoverExhaustedError{Attempts: attempts, Cause: cause},
	}
}

// BackendRegistrationError indicates a failure registering a backend.
type BackendRegistrationError struct {
	Name  string
	Cause error
}

// Error implements the error interface.
func (e *BackendRegistrationError) Error() string {
	return "failed to register backend " + e.Name + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *BackendRegistrationError) Unwrap() error {
	return e.Cause
}

// NewBackendRegistrationError creates a DragonError for a backend registration failure.
func NewBackendRegistrationError(op, name string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &BackendRegistrationError{Name: name, Cause: cause},
	}
}

// NoBackendsAvailableError indicates no backends are available.
type NoBackendsAvailableError struct {
	Source string
}

// Error implements the error interface.
func (e *NoBackendsAvailableError) Error() string {
	return "no backends available in " + e.Source
}

// NewNoBackendsAvailableError creates a DragonError for no available backends.
func NewNoBackendsAvailableError(op, source string) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrUnavailable,
		Op:   op,
		Err:  &NoBackendsAvailableError{Source: source},
	}
}

// NoBackendsRegisteredError indicates no backends were successfully registered.
type NoBackendsRegisteredError struct{}

// Error implements the error interface.
func (e *NoBackendsRegisteredError) Error() string {
	return "no backends were successfully registered"
}

// NewNoBackendsRegisteredError creates a DragonError for no registered backends.
func NewNoBackendsRegisteredError(op string) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrUnavailable,
		Op:   op,
		Err:  &NoBackendsRegisteredError{},
	}
}

// RegistryCloseError indicates a failure closing the backend registry.
type RegistryCloseError struct {
	Cause error
}

// Error implements the error interface.
func (e *RegistryCloseError) Error() string {
	return "failed to close registry: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *RegistryCloseError) Unwrap() error {
	return e.Cause
}

// NewRegistryCloseError creates a DragonError for a registry close failure.
func NewRegistryCloseError(op string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &RegistryCloseError{Cause: cause},
	}
}

// SymmetricKeyGenerationError indicates a failure generating a symmetric key.
type SymmetricKeyGenerationError struct {
	Cause error
}

// Error implements the error interface.
func (e *SymmetricKeyGenerationError) Error() string {
	return "failed to generate symmetric key: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *SymmetricKeyGenerationError) Unwrap() error {
	return e.Cause
}

// NewSymmetricKeyGenerationError creates a DragonError for a symmetric key generation failure.
func NewSymmetricKeyGenerationError(op string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &SymmetricKeyGenerationError{Cause: cause},
	}
}

// AsymmetricKeyGenerationError indicates a failure generating an asymmetric key.
type AsymmetricKeyGenerationError struct {
	Cause error
}

// Error implements the error interface.
func (e *AsymmetricKeyGenerationError) Error() string {
	return "failed to generate asymmetric key: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *AsymmetricKeyGenerationError) Unwrap() error {
	return e.Cause
}

// NewAsymmetricKeyGenerationError creates a DragonError for an asymmetric key generation failure.
func NewAsymmetricKeyGenerationError(op string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &AsymmetricKeyGenerationError{Cause: cause},
	}
}

// KeyVersionGenerationError indicates a failure generating a new key version.
type KeyVersionGenerationError struct {
	Cause error
}

// Error implements the error interface.
func (e *KeyVersionGenerationError) Error() string {
	return "failed to generate new key version: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *KeyVersionGenerationError) Unwrap() error {
	return e.Cause
}

// NewKeyVersionGenerationError creates a DragonError for a key version generation failure.
func NewKeyVersionGenerationError(op string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &KeyVersionGenerationError{Cause: cause},
	}
}

// KeyDeletionError indicates a failure deleting a key.
type KeyDeletionError struct {
	Cause error
}

// Error implements the error interface.
func (e *KeyDeletionError) Error() string {
	return "failed to delete key: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *KeyDeletionError) Unwrap() error {
	return e.Cause
}

// NewKeyDeletionError creates a DragonError for a key deletion failure.
func NewKeyDeletionError(op string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &KeyDeletionError{Cause: cause},
	}
}

// KeyMetadataRetrievalError indicates a failure retrieving key metadata.
type KeyMetadataRetrievalError struct {
	Cause error
}

// Error implements the error interface.
func (e *KeyMetadataRetrievalError) Error() string {
	return "failed to get key metadata: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *KeyMetadataRetrievalError) Unwrap() error {
	return e.Cause
}

// NewKeyMetadataRetrievalError creates a DragonError for a key metadata retrieval failure.
func NewKeyMetadataRetrievalError(op string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &KeyMetadataRetrievalError{Cause: cause},
	}
}

// EncryptionOperationError indicates an encryption operation failed.
type EncryptionOperationError struct {
	Cause error
}

// Error implements the error interface.
func (e *EncryptionOperationError) Error() string {
	return "encryption failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *EncryptionOperationError) Unwrap() error {
	return e.Cause
}

// NewEncryptionOperationError creates a DragonError for an encryption operation failure.
func NewEncryptionOperationError(op string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrEncryption,
		Op:   op,
		Err:  &EncryptionOperationError{Cause: cause},
	}
}

// DecryptionOperationError indicates a decryption operation failed.
type DecryptionOperationError struct {
	Cause error
}

// Error implements the error interface.
func (e *DecryptionOperationError) Error() string {
	return "decryption failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *DecryptionOperationError) Unwrap() error {
	return e.Cause
}

// NewDecryptionOperationError creates a DragonError for a decryption operation failure.
func NewDecryptionOperationError(op string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrDecryption,
		Op:   op,
		Err:  &DecryptionOperationError{Cause: cause},
	}
}

// CiphertextDeserializationError indicates a failure deserializing ciphertext.
type CiphertextDeserializationError struct {
	Cause error
}

// Error implements the error interface.
func (e *CiphertextDeserializationError) Error() string {
	return "failed to deserialize ciphertext: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *CiphertextDeserializationError) Unwrap() error {
	return e.Cause
}

// NewCiphertextDeserializationError creates a DragonError for a ciphertext deserialization failure.
func NewCiphertextDeserializationError(op string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrDecryption,
		Op:   op,
		Err:  &CiphertextDeserializationError{Cause: cause},
	}
}

// SignerRetrievalError indicates a failure retrieving a cryptographic signer.
type SignerRetrievalError struct {
	Cause error
}

// Error implements the error interface.
func (e *SignerRetrievalError) Error() string {
	return "failed to get signer: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *SignerRetrievalError) Unwrap() error {
	return e.Cause
}

// NewSignerRetrievalError creates a DragonError for a signer retrieval failure.
func NewSignerRetrievalError(op string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &SignerRetrievalError{Cause: cause},
	}
}

// SigningOperationError indicates a signing operation failed.
type SigningOperationError struct {
	Cause error
}

// Error implements the error interface.
func (e *SigningOperationError) Error() string {
	return "signing failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *SigningOperationError) Unwrap() error {
	return e.Cause
}

// NewSigningOperationError creates a DragonError for a signing operation failure.
func NewSigningOperationError(op string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &SigningOperationError{Cause: cause},
	}
}

// UnsupportedPublicKeyTypeError indicates an unsupported public key type was encountered.
type UnsupportedPublicKeyTypeError struct {
	TypeName string
}

// Error implements the error interface.
func (e *UnsupportedPublicKeyTypeError) Error() string {
	return "unsupported public key type: " + e.TypeName
}

// NewUnsupportedPublicKeyTypeError creates a DragonError for an unsupported public key type.
func NewUnsupportedPublicKeyTypeError(op, typeName string) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &UnsupportedPublicKeyTypeError{TypeName: typeName},
	}
}

// RandomGenerationError indicates a failure generating random bytes.
type RandomGenerationError struct {
	Cause error
}

// Error implements the error interface.
func (e *RandomGenerationError) Error() string {
	return "failed to generate random bytes: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *RandomGenerationError) Unwrap() error {
	return e.Cause
}

// NewRandomGenerationError creates a DragonError for a random generation failure.
func NewRandomGenerationError(op string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &RandomGenerationError{Cause: cause},
	}
}

// DEKWrappingError indicates a failure wrapping a data encryption key.
type DEKWrappingError struct {
	Cause error
}

// Error implements the error interface.
func (e *DEKWrappingError) Error() string {
	return "failed to wrap DEK: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *DEKWrappingError) Unwrap() error {
	return e.Cause
}

// NewDEKWrappingError creates a DragonError for a DEK wrapping failure.
func NewDEKWrappingError(op string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrEncryption,
		Op:   op,
		Err:  &DEKWrappingError{Cause: cause},
	}
}

// NoPublicComponentError indicates a key has no public component.
type NoPublicComponentError struct{}

// Error implements the error interface.
func (e *NoPublicComponentError) Error() string {
	return "key has no public component"
}

// NewNoPublicComponentError creates a DragonError for a missing public component.
func NewNoPublicComponentError(op string) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &NoPublicComponentError{},
	}
}

// PublicKeyMarshalError indicates a failure marshaling a public key.
type PublicKeyMarshalError struct {
	Cause error
}

// Error implements the error interface.
func (e *PublicKeyMarshalError) Error() string {
	return "failed to marshal public key: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *PublicKeyMarshalError) Unwrap() error {
	return e.Cause
}

// NewPublicKeyMarshalError creates a DragonError for a public key marshaling failure.
func NewPublicKeyMarshalError(op string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &PublicKeyMarshalError{Cause: cause},
	}
}

// CacheLimitExceededError indicates a cache limit was exceeded.
type CacheLimitExceededError struct {
	LimitType string
	Value     string
	Limit     string
}

// Error implements the error interface.
func (e *CacheLimitExceededError) Error() string {
	return e.LimitType + ": " + e.Value + " exceeds " + e.Limit
}

// NewCacheLimitExceededError creates a DragonError for a cache limit violation.
func NewCacheLimitExceededError(op string, sentinel error, limitType, value, limit string) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &CacheLimitExceededError{LimitType: limitType, Value: value, Limit: limit},
	}
}

// CacheCloseError indicates a failure closing the key cache.
type CacheCloseError struct {
	Cause error
}

// Error implements the error interface.
func (e *CacheCloseError) Error() string {
	return "failed to close key cache: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *CacheCloseError) Unwrap() error {
	return e.Cause
}

// NewCacheCloseError creates a DragonError for a cache close failure.
func NewCacheCloseError(op string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &CacheCloseError{Cause: cause},
	}
}

// UnsupportedHashAlgorithmError indicates an unsupported hash algorithm was requested.
type UnsupportedHashAlgorithmError struct {
	Algorithm string
}

// Error implements the error interface.
func (e *UnsupportedHashAlgorithmError) Error() string {
	return "unsupported hash algorithm: " + e.Algorithm
}

// NewUnsupportedHashAlgorithmError creates a DragonError for an unsupported hash algorithm.
func NewUnsupportedHashAlgorithmError(op, algorithm string) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &UnsupportedHashAlgorithmError{Algorithm: algorithm},
	}
}

// DEKDecryptionError indicates a failure decrypting a data encryption key.
type DEKDecryptionError struct {
	Cause error
}

// Error implements the error interface.
func (e *DEKDecryptionError) Error() string {
	return "failed to decrypt DEK: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *DEKDecryptionError) Unwrap() error {
	return e.Cause
}

// NewDEKDecryptionError creates a DragonError for a DEK decryption failure.
func NewDEKDecryptionError(op string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrDecryption,
		Op:   op,
		Err:  &DEKDecryptionError{Cause: cause},
	}
}

// KeyRotationError indicates a key rotation operation failed.
type KeyRotationError struct {
	Cause error
}

// Error implements the error interface.
func (e *KeyRotationError) Error() string {
	return "key rotation failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *KeyRotationError) Unwrap() error {
	return e.Cause
}

// NewKeyRotationError creates a DragonError for a key rotation failure.
func NewKeyRotationError(op string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &KeyRotationError{Cause: cause},
	}
}

// InitializationError indicates a failure during component initialization.
type InitializationError struct {
	Component string
	Cause     error
}

// Error implements the error interface.
func (e *InitializationError) Error() string {
	return fmt.Sprintf("failed to initialize %s: %v", e.Component, e.Cause)
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *InitializationError) Unwrap() error {
	return e.Cause
}

// NewInitializationError creates a DragonError for an initialization failure.
func NewInitializationError(op, component string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &InitializationError{Component: component, Cause: cause},
	}
}

// ComponentCreationError indicates a failure creating a component.
type ComponentCreationError struct {
	Component string
	Cause     error
}

// Error implements the error interface.
func (e *ComponentCreationError) Error() string {
	return fmt.Sprintf("failed to create %s: %v", e.Component, e.Cause)
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *ComponentCreationError) Unwrap() error {
	return e.Cause
}

// NewComponentCreationError creates a DragonError for a component creation failure.
func NewComponentCreationError(op, component string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &ComponentCreationError{Component: component, Cause: cause},
	}
}

// RandomKeyGenerationError indicates a failure generating a random key.
type RandomKeyGenerationError struct {
	Cause error
}

// Error implements the error interface.
func (e *RandomKeyGenerationError) Error() string {
	return "failed to generate random key: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *RandomKeyGenerationError) Unwrap() error {
	return e.Cause
}

// NewRandomKeyGenerationError creates a DragonError for a random key generation failure.
func NewRandomKeyGenerationError(op string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &RandomKeyGenerationError{Cause: cause},
	}
}
