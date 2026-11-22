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

package ociregistry

import (
	"fmt"
	"time"
)

// AttestationRequiredError is returned when a client attempts to access
// the registry without providing attestation evidence.
type AttestationRequiredError struct{}

func (e AttestationRequiredError) Error() string {
	return "registry: attestation evidence required for registry access"
}

// AttestationFailedError is returned when attestation verification fails.
type AttestationFailedError struct {
	Reason string
}

func (e AttestationFailedError) Error() string {
	return fmt.Sprintf("registry: attestation verification failed: %s", e.Reason)
}

// InvalidQuoteError is returned when a TPM quote is malformed or cannot be verified.
type InvalidQuoteError struct {
	Reason string
	Cause  error
}

func (e InvalidQuoteError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("registry: invalid TPM quote: %v", e.Cause)
	}
	if e.Reason != "" {
		return fmt.Sprintf("registry: invalid TPM quote: %s", e.Reason)
	}
	return "registry: invalid TPM quote"
}

func (e InvalidQuoteError) Unwrap() error {
	return e.Cause
}

// AccessDeniedError is returned when access is denied based on policy evaluation.
type AccessDeniedError struct {
	Subject  string
	Resource string
	Reason   string
}

func (e AccessDeniedError) Error() string {
	if e.Subject != "" && e.Resource != "" {
		return fmt.Sprintf("registry: access denied: subject %s cannot access %s: %s", e.Subject, e.Resource, e.Reason)
	}
	return fmt.Sprintf("registry: access denied: %s", e.Reason)
}

// ImageNotFoundError is returned when a requested image does not exist.
type ImageNotFoundError struct {
	Reference string
}

func (e ImageNotFoundError) Error() string {
	return fmt.Sprintf("registry: image not found: %s", e.Reference)
}

// ImageExistsError is returned when attempting to push an image that already exists
// and overwrite is not allowed.
type ImageExistsError struct {
	Reference string
}

func (e ImageExistsError) Error() string {
	return fmt.Sprintf("registry: image already exists: %s", e.Reference)
}

// DecryptionFailedError is returned when image decryption fails.
type DecryptionFailedError struct {
	Reference string
	LayerID   string
	Cause     error
}

func (e DecryptionFailedError) Error() string {
	if e.LayerID != "" && e.Cause != nil {
		return fmt.Sprintf("registry: failed to decrypt layer %s: %v", e.LayerID, e.Cause)
	}
	if e.Reference != "" && e.Cause != nil {
		return fmt.Sprintf("registry: failed to decrypt image %s: %v", e.Reference, e.Cause)
	}
	if e.Cause != nil {
		return fmt.Sprintf("registry: decryption failed: %v", e.Cause)
	}
	return "registry: decryption failed"
}

func (e DecryptionFailedError) Unwrap() error {
	return e.Cause
}

// EncryptionFailedError is returned when image encryption fails.
type EncryptionFailedError struct {
	Reference string
	LayerID   string
	Cause     error
}

func (e EncryptionFailedError) Error() string {
	if e.LayerID != "" && e.Cause != nil {
		return fmt.Sprintf("registry: failed to encrypt layer %s: %v", e.LayerID, e.Cause)
	}
	if e.Reference != "" && e.Cause != nil {
		return fmt.Sprintf("registry: failed to encrypt image %s: %v", e.Reference, e.Cause)
	}
	if e.Cause != nil {
		return fmt.Sprintf("registry: encryption failed: %v", e.Cause)
	}
	return "registry: encryption failed"
}

func (e EncryptionFailedError) Unwrap() error {
	return e.Cause
}

// ErrStorageError is returned when a storage operation fails.
type ErrStorageError struct {
	Operation string
	Key       string
	Cause     error
}

func (e ErrStorageError) Error() string {
	if e.Key != "" && e.Cause != nil {
		return fmt.Sprintf("registry: storage error during %s for %s: %v", e.Operation, e.Key, e.Cause)
	}
	if e.Cause != nil {
		return fmt.Sprintf("registry: storage error during %s: %v", e.Operation, e.Cause)
	}
	return fmt.Sprintf("registry: storage error during %s", e.Operation)
}

func (e ErrStorageError) Unwrap() error {
	return e.Cause
}

// RegistryNotInitializedError is returned when operations are attempted
// on an uninitialized registry.
type RegistryNotInitializedError struct{}

func (e RegistryNotInitializedError) Error() string {
	return "registry: registry not initialized"
}

// RegistryClosedError is returned when operations are attempted on a closed registry.
type RegistryClosedError struct{}

func (e RegistryClosedError) Error() string {
	return "registry: registry is closed"
}

// InvalidImageRefError is returned when an image reference is malformed.
type InvalidImageRefError struct {
	Reference string
	Reason    string
}

func (e InvalidImageRefError) Error() string {
	return fmt.Sprintf("registry: invalid image reference %q: %s", e.Reference, e.Reason)
}

// BlobNotFoundError is returned when a specific blob is not found.
type BlobNotFoundError struct {
	Digest string
}

func (e BlobNotFoundError) Error() string {
	return fmt.Sprintf("registry: blob not found: %s", e.Digest)
}

// ManifestNotFoundError is returned when an image manifest is not found.
type ManifestNotFoundError struct {
	Reference string
}

func (e ManifestNotFoundError) Error() string {
	return fmt.Sprintf("registry: manifest not found: %s", e.Reference)
}

// InvalidManifestError is returned when a manifest is malformed.
type InvalidManifestError struct {
	Reference string
	Reason    string
}

func (e InvalidManifestError) Error() string {
	return fmt.Sprintf("registry: invalid manifest for %s: %s", e.Reference, e.Reason)
}

// KeyNotFoundError is returned when an encryption key is not found.
type KeyNotFoundError struct {
	KeyID string
}

func (e KeyNotFoundError) Error() string {
	return fmt.Sprintf("registry: encryption key not found: %s", e.KeyID)
}

// KeyCreationFailedError is returned when key creation fails.
type KeyCreationFailedError struct {
	KeyID string
	Cause error
}

func (e KeyCreationFailedError) Error() string {
	if e.KeyID != "" && e.Cause != nil {
		return fmt.Sprintf("registry: failed to create key %s: %v", e.KeyID, e.Cause)
	}
	if e.Cause != nil {
		return fmt.Sprintf("registry: key creation failed: %v", e.Cause)
	}
	return "registry: key creation failed"
}

func (e KeyCreationFailedError) Unwrap() error {
	return e.Cause
}

// KeyWrapFailedError is returned when DEK wrapping fails.
type KeyWrapFailedError struct {
	KeyID string
	Cause error
}

func (e KeyWrapFailedError) Error() string {
	if e.KeyID != "" && e.Cause != nil {
		return fmt.Sprintf("registry: failed to wrap DEK with key %s: %v", e.KeyID, e.Cause)
	}
	if e.Cause != nil {
		return fmt.Sprintf("registry: DEK wrap failed: %v", e.Cause)
	}
	return "registry: DEK wrap failed"
}

func (e KeyWrapFailedError) Unwrap() error {
	return e.Cause
}

// KeyUnwrapFailedError is returned when DEK unwrapping fails.
type KeyUnwrapFailedError struct {
	KeyID string
	Cause error
}

func (e KeyUnwrapFailedError) Error() string {
	if e.KeyID != "" && e.Cause != nil {
		return fmt.Sprintf("registry: failed to unwrap DEK with key %s: %v", e.KeyID, e.Cause)
	}
	if e.Cause != nil {
		return fmt.Sprintf("registry: DEK unwrap failed: %v", e.Cause)
	}
	return "registry: DEK unwrap failed"
}

func (e KeyUnwrapFailedError) Unwrap() error {
	return e.Cause
}

// InvalidConfigError is returned when registry configuration is invalid.
type InvalidConfigError struct {
	Field  string
	Reason string
}

func (e InvalidConfigError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("registry: invalid configuration for %s: %s", e.Field, e.Reason)
	}
	return fmt.Sprintf("registry: invalid configuration: %s", e.Reason)
}

// PCRMismatchError is returned when PCR values do not match expected values.
type PCRMismatchError struct {
	PCRIndex int
	Expected []byte
	Actual   []byte
}

func (e PCRMismatchError) Error() string {
	return fmt.Sprintf("registry: PCR %d mismatch: expected %x, got %x", e.PCRIndex, e.Expected, e.Actual)
}

// AIKNotAllowedError is returned when an AIK is not in the allowed list.
type AIKNotAllowedError struct {
	AIK string
}

func (e AIKNotAllowedError) Error() string {
	return fmt.Sprintf("registry: AIK not in allowed list: %s", e.AIK)
}

// QuicBaaSConnectionError is returned when connection to QuicBaaS fails.
type QuicBaaSConnectionError struct {
	Addr  string
	Cause error
}

func (e QuicBaaSConnectionError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("registry: failed to connect to QuicBaaS at %s: %v", e.Addr, e.Cause)
	}
	return fmt.Sprintf("registry: failed to connect to QuicBaaS at %s", e.Addr)
}

func (e QuicBaaSConnectionError) Unwrap() error {
	return e.Cause
}

// SessionExpiredError is returned when a session has expired.
type SessionExpiredError struct {
	SessionID string
}

func (e SessionExpiredError) Error() string {
	if e.SessionID != "" {
		return fmt.Sprintf("registry: session %s has expired", e.SessionID)
	}
	return "registry: session has expired"
}

// SessionNotFoundError is returned when a session cannot be found.
type SessionNotFoundError struct {
	SessionID string
}

func (e SessionNotFoundError) Error() string {
	if e.SessionID != "" {
		return fmt.Sprintf("registry: session %s not found", e.SessionID)
	}
	return "registry: session not found"
}

// InvalidSessionError is returned when session data is invalid.
type InvalidSessionError struct {
	Reason string
}

func (e InvalidSessionError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("registry: invalid session: %s", e.Reason)
	}
	return "registry: invalid session"
}

// OperationTimeoutError is returned when an operation times out.
type OperationTimeoutError struct {
	Operation string
}

func (e OperationTimeoutError) Error() string {
	return fmt.Sprintf("registry: operation timed out: %s", e.Operation)
}

// DigestMismatchError is returned when a content digest does not match expected value.
type DigestMismatchError struct {
	Expected string
	Actual   string
}

func (e DigestMismatchError) Error() string {
	return fmt.Sprintf("registry: digest mismatch: expected %s, got %s", e.Expected, e.Actual)
}

// NilClientError is returned when a required client is nil.
type NilClientError struct {
	Client string
}

func (e NilClientError) Error() string {
	if e.Client != "" {
		return fmt.Sprintf("registry: %s client is nil", e.Client)
	}
	return "registry: client is nil"
}

// ObjectStoreTTLError is returned when an object-store write asks for an
// expiry: objects live until they are deleted.
type ObjectStoreTTLError struct {
	TTL time.Duration
}

func (e ObjectStoreTTLError) Error() string {
	return fmt.Sprintf("registry: object storage does not expire objects (ttl %s)", e.TTL)
}

// NilContextError is returned when a nil context is provided.
type NilContextError struct {
	Operation string
}

func (e NilContextError) Error() string {
	if e.Operation != "" {
		return fmt.Sprintf("registry: nil context provided to %s", e.Operation)
	}
	return "registry: nil context provided"
}

// ImageEmptyError is returned when an image has no layers.
type ImageEmptyError struct{}

func (e ImageEmptyError) Error() string {
	return "registry: image has no layers"
}

// PolicyEvaluationFailedError is returned when OPA policy evaluation fails.
type PolicyEvaluationFailedError struct {
	Policy string
	Cause  error
}

func (e PolicyEvaluationFailedError) Error() string {
	if e.Policy != "" && e.Cause != nil {
		return fmt.Sprintf("registry: policy %s evaluation failed: %v", e.Policy, e.Cause)
	}
	if e.Cause != nil {
		return fmt.Sprintf("registry: policy evaluation failed: %v", e.Cause)
	}
	return "registry: policy evaluation failed"
}

func (e PolicyEvaluationFailedError) Unwrap() error {
	return e.Cause
}

// InvalidDigestError is returned when a digest is invalid.
type InvalidDigestError struct {
	Digest string
	Reason string
}

func (e InvalidDigestError) Error() string {
	if e.Digest != "" && e.Reason != "" {
		return fmt.Sprintf("registry: invalid digest %s: %s", e.Digest, e.Reason)
	}
	if e.Digest != "" {
		return fmt.Sprintf("registry: invalid digest: %s", e.Digest)
	}
	return "registry: invalid digest"
}

// ImageAlreadyExistsError is returned by Push when the reference is
// already populated and the caller did not request overwrite.
type ImageAlreadyExistsError struct {
	Reference string
}

func (e ImageAlreadyExistsError) Error() string {
	return fmt.Sprintf("registry: image already exists: %s", e.Reference)
}

// StorageNotConfiguredError is returned by Registry methods when no
// QuicBaaSStorage backend was wired at construction time. The
// daemon's Phase 5 wiring uses registry.WithStorage(...) — if you
// see this in production it means the boot flow forgot to call it.
type StorageNotConfiguredError struct {
	Op string
}

func (e StorageNotConfiguredError) Error() string {
	return fmt.Sprintf("registry: storage not configured for %s", e.Op)
}

// EncryptionNotConfiguredError is returned when a caller requests
// per-image encryption but the encryption pipeline has not been
// wired into the Registry. Encryption + signing land in a follow-up
// after the unencrypted-pipeline ships.
type EncryptionNotConfiguredError struct {
	Reference string
}

func (e EncryptionNotConfiguredError) Error() string {
	return fmt.Sprintf("registry: encryption not configured (ref=%s)", e.Reference)
}

// SigningNotConfiguredError is returned when a push asks for the image to be
// signed and the registry was built without a SigningKMSClient (WithSigner):
// it refuses rather than storing an unsigned image the caller believes is
// signed.
type SigningNotConfiguredError struct {
	Reference string
}

func (e SigningNotConfiguredError) Error() string {
	return fmt.Sprintf("registry: image signing not configured (ref=%s)", e.Reference)
}

// ManifestSigningFailedError is returned when the signer cannot sign a
// manifest with KeyID. Push stores nothing when it is returned.
type ManifestSigningFailedError struct {
	KeyID string
	Err   error
}

func (e ManifestSigningFailedError) Error() string {
	return fmt.Sprintf("registry: sign manifest with key %s: %v", e.KeyID, e.Err)
}

func (e ManifestSigningFailedError) Unwrap() error {
	return e.Err
}

// SignatureVerificationFailedError is returned by a Pull that asked for a
// verified manifest when the signature cannot be verified: no verifier is
// configured, the manifest is unsigned, or the signature does not match.
// Pull returns no content with it.
type SignatureVerificationFailedError struct {
	Reason string
	Cause  error
}

func (e SignatureVerificationFailedError) Error() string {
	if e.Cause == nil {
		return "registry: manifest signature verification failed: " + e.Reason
	}
	return fmt.Sprintf("registry: manifest signature verification failed: %s: %v", e.Reason, e.Cause)
}

func (e SignatureVerificationFailedError) Unwrap() error {
	return e.Cause
}

// SigningClientRequiredError is returned by NewSDKSigner when the SDK KMS or
// crypto client is nil.
type SigningClientRequiredError struct{}

func (SigningClientRequiredError) Error() string {
	return "registry: the manifest signer needs the SDK KMS and crypto clients"
}

// InvalidSigningRequestError is returned when a sign request has no key ID
// or a digest that is not SHA-256 sized.
type InvalidSigningRequestError struct {
	Reason string
}

func (e InvalidSigningRequestError) Error() string {
	return "registry: invalid signing request: " + e.Reason
}

// SigningKeyUnavailableError is returned when the SDK KMS has no key with the
// ID a push names. The signer never creates the key.
type SigningKeyUnavailableError struct {
	KeyID string
	Err   error
}

func (e SigningKeyUnavailableError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("registry: signing key %s not found in the KMS", e.KeyID)
	}
	return fmt.Sprintf("registry: signing key %s not available in the KMS: %v", e.KeyID, e.Err)
}

func (e SigningKeyUnavailableError) Unwrap() error {
	return e.Err
}

// SigningKeyUnsupportedError is returned when the KMS key a push names cannot
// sign manifests: its algorithm is not ECDSA P-256 or P-384. The KMS itself
// enforces the key's usage and policies when it signs.
type SigningKeyUnsupportedError struct {
	KeyID  string
	Reason string
}

func (e SigningKeyUnsupportedError) Error() string {
	return fmt.Sprintf("registry: signing key %s cannot sign manifests: %s", e.KeyID, e.Reason)
}

// MessageError is a typed error carrying a fully formatted message with no
// underlying cause. It replaces inline formatted-error constructions that do not wrap
// another error.
type MessageError struct {
	Msg string
}

// Error implements the error interface.
func (e *MessageError) Error() string {
	return e.Msg
}

// WrappedError wraps an underlying cause with a contextual message while
// preserving errors.Is / errors.As matching through Unwrap. It replaces
// inline formatted-error constructions that wrap another error.
type WrappedError struct {
	Msg string
	Err error
}

// Error implements the error interface.
func (e *WrappedError) Error() string {
	return e.Msg + ": " + e.Err.Error()
}

// Unwrap returns the wrapped cause.
func (e *WrappedError) Unwrap() error {
	return e.Err
}
