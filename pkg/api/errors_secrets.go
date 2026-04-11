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

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// Secrets / DEK / Crypto Error Types
// =============================================================================

// SentinelWrapError wraps a sentinel error with an underlying cause.
// It supports multi-unwrap so errors.Is works with both the sentinel and cause.
type SentinelWrapError struct {
	Sentinel error
	Cause    error
}

// Error implements the error interface.
func (e *SentinelWrapError) Error() string {
	return fmt.Sprintf("%v: %v", e.Sentinel, e.Cause)
}

// Unwrap returns both errors for multi-unwrap support (Go 1.20+).
func (e *SentinelWrapError) Unwrap() []error {
	return []error{e.Sentinel, e.Cause}
}

// NewSentinelWrapError creates a QBError that wraps a sentinel with a cause.
func NewSentinelWrapError(op string, code dberrors.ErrorCode, sentinel, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: code,
		Op:   op,
		Err:  &SentinelWrapError{Sentinel: sentinel, Cause: cause},
	}
}

// CryptoOperationError indicates a cryptographic operation failed.
type CryptoOperationError struct {
	Operation string
	Cause     error
}

// Error implements the error interface.
func (e *CryptoOperationError) Error() string {
	return fmt.Sprintf("%s: %v", e.Operation, e.Cause)
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *CryptoOperationError) Unwrap() error {
	return e.Cause
}

// NewCryptoOperationError creates a QBError for a crypto operation failure.
func NewCryptoOperationError(op, operation string, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrEncryption,
		Op:   op,
		Err:  &CryptoOperationError{Operation: operation, Cause: cause},
	}
}

// DEKSizeMismatchError indicates a DEK key size does not match the algorithm requirement.
type DEKSizeMismatchError struct {
	Expected  int
	Algorithm string
}

// Error implements the error interface.
func (e *DEKSizeMismatchError) Error() string {
	return fmt.Sprintf("DEK size mismatch: expected %d bytes for %s",
		e.Expected, e.Algorithm)
}

// NewDEKSizeMismatchError creates a QBError for a DEK size mismatch.
func NewDEKSizeMismatchError(op string, expected int, algorithm string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrEncryption,
		Op:   op,
		Err:  &DEKSizeMismatchError{Expected: expected, Algorithm: algorithm},
	}
}

// DEKNotFoundError indicates a DEK was not found with the given identifier.
type DEKNotFoundError struct {
	DEKID string
}

// Error implements the error interface.
func (e *DEKNotFoundError) Error() string {
	return "DEK not found: " + e.DEKID
}

// NewDEKNotFoundError creates a QBError for a missing DEK.
func NewDEKNotFoundError(op, dekid string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   op,
		Err:  &DEKNotFoundError{DEKID: dekid},
	}
}

// DEKVersionNotFoundError indicates a specific DEK version was not found.
type DEKVersionNotFoundError struct {
	DEKID   string
	Version uint64
}

// Error implements the error interface.
func (e *DEKVersionNotFoundError) Error() string {
	return fmt.Sprintf("DEK version not found: %s version %d", e.DEKID, e.Version)
}

// NewDEKVersionNotFoundError creates a QBError for a missing DEK version.
func NewDEKVersionNotFoundError(op, dekid string, version uint64) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   op,
		Err:  &DEKVersionNotFoundError{DEKID: dekid, Version: version},
	}
}

// DEKOperationError indicates a DEK operation failed (load, marshal, store).
type DEKOperationError struct {
	Operation string
	Cause     error
}

// Error implements the error interface.
func (e *DEKOperationError) Error() string {
	return fmt.Sprintf("%s: %v", e.Operation, e.Cause)
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *DEKOperationError) Unwrap() error {
	return e.Cause
}

// NewDEKOperationError creates a QBError for a DEK operation failure.
func NewDEKOperationError(op, operation string, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &DEKOperationError{Operation: operation, Cause: cause},
	}
}

// NilDEKError indicates a DEK was nil when required.
type NilDEKError struct{}

// Error implements the error interface.
func (e *NilDEKError) Error() string {
	return "DEK cannot be nil"
}

// NewNilDEKError creates a QBError for a nil DEK.
func NewNilDEKError(op string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &NilDEKError{},
	}
}

// SecretStoreOperationError indicates a secret store operation failed.
type SecretStoreOperationError struct {
	Operation string
	Cause     error
}

// Error implements the error interface.
func (e *SecretStoreOperationError) Error() string {
	return fmt.Sprintf("%s: %v", e.Operation, e.Cause)
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *SecretStoreOperationError) Unwrap() error {
	return e.Cause
}

// NewSecretStoreOperationError creates a QBError for a secret store operation failure.
func NewSecretStoreOperationError(op, operation string, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &SecretStoreOperationError{Operation: operation, Cause: cause},
	}
}

// PathTraversalError indicates a key contains path traversal.
type PathTraversalError struct {
	Sentinel error
	Detail   string
}

// Error implements the error interface.
func (e *PathTraversalError) Error() string {
	return fmt.Sprintf("%v: %s", e.Sentinel, e.Detail)
}

// Unwrap returns the sentinel for errors.Is support.
func (e *PathTraversalError) Unwrap() error {
	return e.Sentinel
}

// NewPathTraversalError creates a QBError for a path traversal attempt.
func NewPathTraversalError(op string, sentinel error, detail string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &PathTraversalError{Sentinel: sentinel, Detail: detail},
	}
}

// VersionPositiveError indicates a version number must be positive.
type VersionPositiveError struct{}

// Error implements the error interface.
func (e *VersionPositiveError) Error() string {
	return "version must be positive"
}

// NewVersionPositiveError creates a QBError for a non-positive version.
func NewVersionPositiveError(op string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &VersionPositiveError{},
	}
}

// BackendCloseError indicates a backend failed to close.
type BackendCloseError struct {
	Cause error
}

// Error implements the error interface.
func (e *BackendCloseError) Error() string {
	return fmt.Sprintf("failed to close backend: %v", e.Cause)
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *BackendCloseError) Unwrap() error {
	return e.Cause
}

// NewBackendCloseError creates a QBError for a backend close failure.
func NewBackendCloseError(op string, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &BackendCloseError{Cause: cause},
	}
}

// InvalidBlobError indicates an encrypted blob is invalid.
type InvalidBlobError struct {
	Cause error
}

// Error implements the error interface.
func (e *InvalidBlobError) Error() string {
	return fmt.Sprintf("invalid blob: %v", e.Cause)
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *InvalidBlobError) Unwrap() error {
	return e.Cause
}

// NewInvalidBlobError creates a QBError for an invalid encrypted blob.
func NewInvalidBlobError(op string, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &InvalidBlobError{Cause: cause},
	}
}

// DEKRetrievalError indicates a failure to retrieve a DEK.
type DEKRetrievalError struct {
	Cause error
}

// Error implements the error interface.
func (e *DEKRetrievalError) Error() string {
	return fmt.Sprintf("failed to get DEK: %v", e.Cause)
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *DEKRetrievalError) Unwrap() error {
	return e.Cause
}

// NewDEKRetrievalError creates a QBError for a DEK retrieval failure.
func NewDEKRetrievalError(op string, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &DEKRetrievalError{Cause: cause},
	}
}

// AuthenticationFailedError indicates AEAD authentication failed during decryption.
type AuthenticationFailedError struct {
	Cause error
}

// Error implements the error interface.
func (e *AuthenticationFailedError) Error() string {
	return fmt.Sprintf("authentication failed: %v", e.Cause)
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *AuthenticationFailedError) Unwrap() error {
	return e.Cause
}

// NewAuthenticationFailedError creates a QBError for an AEAD auth failure.
func NewAuthenticationFailedError(op string, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrDecryption,
		Op:   op,
		Err:  &AuthenticationFailedError{Cause: cause},
	}
}
