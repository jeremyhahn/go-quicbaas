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
	"strings"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// XKMS Error Types
// =============================================================================

// XKMSNotInitializedError indicates the xkms has not been initialized.
type XKMSNotInitializedError struct{}

// Error implements the error interface.
func (e *XKMSNotInitializedError) Error() string {
	return "quicbaas xkms not initialized"
}

// Is supports errors.Is matching by type.
func (e *XKMSNotInitializedError) Is(target error) bool {
	_, ok := target.(*XKMSNotInitializedError)
	return ok
}

// NewXKMSNotInitializedError creates a DragonError for an uninitialized xkms.
func NewXKMSNotInitializedError(op string) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrPreconditionFailed,
		Op:   op,
		Err:  &XKMSNotInitializedError{},
	}
}

// XKMSAlreadyInitializedError indicates the xkms is already initialized.
type XKMSAlreadyInitializedError struct{}

// Error implements the error interface.
func (e *XKMSAlreadyInitializedError) Error() string {
	return "quicbaas xkms already initialized"
}

// Is supports errors.Is matching by type.
func (e *XKMSAlreadyInitializedError) Is(target error) bool {
	_, ok := target.(*XKMSAlreadyInitializedError)
	return ok
}

// XKMSInvalidConfigError indicates an invalid xkms configuration.
type XKMSInvalidConfigError struct {
	Detail string
}

// Error implements the error interface.
func (e *XKMSInvalidConfigError) Error() string {
	if e.Detail != "" {
		return "invalid xkms configuration: " + e.Detail
	}
	return "invalid xkms configuration"
}

// Is supports errors.Is matching by type.
func (e *XKMSInvalidConfigError) Is(target error) bool {
	_, ok := target.(*XKMSInvalidConfigError)
	return ok
}

// XKMSInitializationError indicates the underlying xkms library failed to initialize.
type XKMSInitializationError struct {
	Cause error
}

// Error implements the error interface.
func (e *XKMSInitializationError) Error() string {
	return "failed to initialize go-xkms: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *XKMSInitializationError) Unwrap() error {
	return e.Cause
}

// NewXKMSInitializationError creates a DragonError for an xkms initialization failure.
func NewXKMSInitializationError(cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "xkms.initialize",
		Err:  &XKMSInitializationError{Cause: cause},
	}
}

// SealingError indicates a sealing operation failed.
type SealingError struct {
	Cause error
}

// Error implements the error interface.
func (e *SealingError) Error() string {
	if e.Cause != nil {
		return "sealing operation failed: " + e.Cause.Error()
	}
	return "sealing operation failed"
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *SealingError) Unwrap() error {
	return e.Cause
}

// Is supports errors.Is matching by type.
func (e *SealingError) Is(target error) bool {
	_, ok := target.(*SealingError)
	return ok
}

// UnsealingError indicates an unsealing operation failed.
type UnsealingError struct {
	Detail string
	Cause  error
}

// Error implements the error interface.
func (e *UnsealingError) Error() string {
	if e.Detail != "" {
		return "unsealing operation failed: " + e.Detail
	}
	if e.Cause != nil {
		return "unsealing operation failed: " + e.Cause.Error()
	}
	return "unsealing operation failed"
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *UnsealingError) Unwrap() error {
	return e.Cause
}

// Is supports errors.Is matching by type.
func (e *UnsealingError) Is(target error) bool {
	_, ok := target.(*UnsealingError)
	return ok
}

// XKMSEncryptionError indicates an encryption operation failed.
type XKMSEncryptionError struct {
	Cause error
}

// Error implements the error interface.
func (e *XKMSEncryptionError) Error() string {
	if e.Cause != nil {
		return "encryption operation failed: " + e.Cause.Error()
	}
	return "encryption operation failed"
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *XKMSEncryptionError) Unwrap() error {
	return e.Cause
}

// Is supports errors.Is matching by type.
func (e *XKMSEncryptionError) Is(target error) bool {
	_, ok := target.(*XKMSEncryptionError)
	return ok
}

// XKMSDecryptionError indicates a decryption operation failed.
type XKMSDecryptionError struct {
	Detail string
	Cause  error
}

// Error implements the error interface.
func (e *XKMSDecryptionError) Error() string {
	if e.Detail != "" {
		return "decryption operation failed: " + e.Detail
	}
	if e.Cause != nil {
		return "decryption operation failed: " + e.Cause.Error()
	}
	return "decryption operation failed"
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *XKMSDecryptionError) Unwrap() error {
	return e.Cause
}

// Is supports errors.Is matching by type.
func (e *XKMSDecryptionError) Is(target error) bool {
	_, ok := target.(*XKMSDecryptionError)
	return ok
}

// XKMSKeyGenerationError indicates key generation failed in the xkms layer.
type XKMSKeyGenerationError struct {
	Cause error
}

// Error implements the error interface.
func (e *XKMSKeyGenerationError) Error() string {
	if e.Cause != nil {
		return "key generation failed: " + e.Cause.Error()
	}
	return "key generation failed"
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *XKMSKeyGenerationError) Unwrap() error {
	return e.Cause
}

// Is supports errors.Is matching by type.
func (e *XKMSKeyGenerationError) Is(target error) bool {
	_, ok := target.(*XKMSKeyGenerationError)
	return ok
}

// XKMSKeyNotFoundError indicates a key could not be found.
type XKMSKeyNotFoundError struct {
	Cause error
}

// Error implements the error interface.
func (e *XKMSKeyNotFoundError) Error() string {
	if e.Cause != nil {
		return "key not found: " + e.Cause.Error()
	}
	return "key not found"
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *XKMSKeyNotFoundError) Unwrap() error {
	return e.Cause
}

// Is supports errors.Is matching by type.
func (e *XKMSKeyNotFoundError) Is(target error) bool {
	_, ok := target.(*XKMSKeyNotFoundError)
	return ok
}

// KeyImportError indicates a key import operation failed.
type KeyImportError struct {
	Detail string
	Cause  error
}

// Error implements the error interface.
func (e *KeyImportError) Error() string {
	if e.Detail != "" && e.Cause != nil {
		return "key import failed: " + e.Detail + ": " + e.Cause.Error()
	}
	if e.Detail != "" {
		return "key import failed: " + e.Detail
	}
	if e.Cause != nil {
		return "key import failed: " + e.Cause.Error()
	}
	return "key import failed"
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *KeyImportError) Unwrap() error {
	return e.Cause
}

// Is supports errors.Is matching by type.
func (e *KeyImportError) Is(target error) bool {
	_, ok := target.(*KeyImportError)
	return ok
}

// NewKeyImportError creates a DragonError for a key import failure.
func NewKeyImportError(op, keyType string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &KeyImportError{Detail: keyType, Cause: cause},
	}
}

// SigningError indicates a signing operation failed.
type SigningError struct {
	Cause error
}

// Error implements the error interface.
func (e *SigningError) Error() string {
	if e.Cause != nil {
		return "signing operation failed: " + e.Cause.Error()
	}
	return "signing operation failed"
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *SigningError) Unwrap() error {
	return e.Cause
}

// Is supports errors.Is matching by type.
func (e *SigningError) Is(target error) bool {
	_, ok := target.(*SigningError)
	return ok
}

// VerificationError indicates a verification operation failed.
type VerificationError struct {
	Cause error
}

// Error implements the error interface.
func (e *VerificationError) Error() string {
	if e.Cause != nil {
		return "verification operation failed: " + e.Cause.Error()
	}
	return "verification operation failed"
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *VerificationError) Unwrap() error {
	return e.Cause
}

// Is supports errors.Is matching by type.
func (e *VerificationError) Is(target error) bool {
	_, ok := target.(*VerificationError)
	return ok
}

// CertificateOperationError indicates a certificate operation failed.
type CertificateOperationError struct {
	Cause error
}

// Error implements the error interface.
func (e *CertificateOperationError) Error() string {
	if e.Cause != nil {
		return "certificate operation failed: " + e.Cause.Error()
	}
	return "certificate operation failed"
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *CertificateOperationError) Unwrap() error {
	return e.Cause
}

// Is supports errors.Is matching by type.
func (e *CertificateOperationError) Is(target error) bool {
	_, ok := target.(*CertificateOperationError)
	return ok
}

// InvalidKeyIDError indicates an invalid key ID.
type InvalidKeyIDError struct{}

// Error implements the error interface.
func (e *InvalidKeyIDError) Error() string {
	return "invalid key ID"
}

// Is supports errors.Is matching by type.
func (e *InvalidKeyIDError) Is(target error) bool {
	_, ok := target.(*InvalidKeyIDError)
	return ok
}

// RNGNotInitializedError indicates the RNG has not been initialized.
type RNGNotInitializedError struct{}

// Error implements the error interface.
func (e *RNGNotInitializedError) Error() string {
	return "quicbaas RNG not initialized"
}

// Is supports errors.Is matching by type.
func (e *RNGNotInitializedError) Is(target error) bool {
	_, ok := target.(*RNGNotInitializedError)
	return ok
}

// RNGError indicates a random number generation failure.
type RNGError struct {
	Detail string
	Cause  error
}

// Error implements the error interface.
func (e *RNGError) Error() string {
	if e.Detail != "" {
		return "random number generation failed: " + e.Detail
	}
	if e.Cause != nil {
		return "random number generation failed: " + e.Cause.Error()
	}
	return "random number generation failed"
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *RNGError) Unwrap() error {
	return e.Cause
}

// Is supports errors.Is matching by type.
func (e *RNGError) Is(target error) bool {
	_, ok := target.(*RNGError)
	return ok
}

// RNGInitializationError indicates the RNG failed to initialize.
type RNGInitializationError struct {
	Detail string
	Cause  error
}

// Error implements the error interface.
func (e *RNGInitializationError) Error() string {
	if e.Detail != "" {
		return e.Detail + ": " + e.Cause.Error()
	}
	return "failed to initialize RNG: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *RNGInitializationError) Unwrap() error {
	return e.Cause
}

// JWTSigningError indicates a JWT signing operation failed.
type JWTSigningError struct {
	Detail string
	Cause  error
}

// Error implements the error interface.
func (e *JWTSigningError) Error() string {
	if e.Detail != "" {
		return "jwt signing failed: " + e.Detail
	}
	if e.Cause != nil {
		return "jwt signing failed: " + e.Cause.Error()
	}
	return "jwt signing failed"
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *JWTSigningError) Unwrap() error {
	return e.Cause
}

// Is supports errors.Is matching by type.
func (e *JWTSigningError) Is(target error) bool {
	_, ok := target.(*JWTSigningError)
	return ok
}

// JWTVerificationError indicates a JWT verification operation failed.
type JWTVerificationError struct {
	Detail string
	Cause  error
}

// Error implements the error interface.
func (e *JWTVerificationError) Error() string {
	if e.Detail != "" {
		return "jwt verification failed: " + e.Detail
	}
	if e.Cause != nil {
		return "jwt verification failed: " + e.Cause.Error()
	}
	return "jwt verification failed"
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *JWTVerificationError) Unwrap() error {
	return e.Cause
}

// Is supports errors.Is matching by type.
func (e *JWTVerificationError) Is(target error) bool {
	_, ok := target.(*JWTVerificationError)
	return ok
}

// JWTParsingError indicates a JWT parsing operation failed.
type JWTParsingError struct {
	Detail string
	Cause  error
}

// Error implements the error interface.
func (e *JWTParsingError) Error() string {
	if e.Detail != "" {
		return "jwt parsing failed: " + e.Detail
	}
	if e.Cause != nil {
		return "jwt parsing failed: " + e.Cause.Error()
	}
	return "jwt parsing failed"
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *JWTParsingError) Unwrap() error {
	return e.Cause
}

// Is supports errors.Is matching by type.
func (e *JWTParsingError) Is(target error) bool {
	_, ok := target.(*JWTParsingError)
	return ok
}

// UnsupportedBackendOperationError indicates a backend does not support a specific operation.
type UnsupportedBackendOperationError struct {
	Backend   string
	Operation string
}

// Error implements the error interface.
func (e *UnsupportedBackendOperationError) Error() string {
	return "backend " + e.Backend + " does not support " + e.Operation
}

// UnexpectedKeyTypeError indicates a key has an unexpected cryptographic type.
type UnexpectedKeyTypeError struct {
	Expected string
	Actual   string
}

// Error implements the error interface.
func (e *UnexpectedKeyTypeError) Error() string {
	return e.Expected + " requires matching key type, got " + e.Actual
}

// CloseError indicates one or more errors occurred during resource cleanup.
type CloseError struct {
	Errors []error
}

// Error implements the error interface.
func (e *CloseError) Error() string {
	msgs := make([]string, len(e.Errors))
	for i, err := range e.Errors {
		msgs[i] = err.Error()
	}
	return "close errors: " + strings.Join(msgs, "; ")
}

// Unwrap returns all underlying errors for errors.Is and errors.As support.
func (e *CloseError) Unwrap() []error {
	return e.Errors
}
