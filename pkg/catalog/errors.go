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

// Package catalog provides the object storage catalog for QuicBaaS.
// It manages metadata for objects stored across multiple storage backends.
package catalog

import (
	"encoding/hex"
	"errors"
	"strconv"
)

// Sentinel errors for common error conditions.
var (
	// ErrConfigNil indicates a nil configuration was provided.
	ErrConfigNil = errors.New("config cannot be nil")

	// ErrStorageConfigNil indicates a nil storage config was provided.
	ErrStorageConfigNil = errors.New("storage config cannot be nil")

	// ErrStorageNil indicates a nil storage was provided.
	ErrStorageNil = errors.New("storage cannot be nil")

	// ErrEntryNil indicates a nil entry was provided.
	ErrEntryNil = errors.New("entry cannot be nil")

	// ErrUpdaterNil indicates a nil updater function was provided.
	ErrUpdaterNil = errors.New("updater function cannot be nil")

	// ErrLocationNil indicates a nil location was provided.
	ErrLocationNil = errors.New("location cannot be nil")

	// ErrStoreNameEmpty indicates an empty store name was provided.
	ErrStoreNameEmpty = errors.New("store name cannot be empty")

	// ErrCannotRemovePrimary indicates an attempt to remove the primary store.
	ErrCannotRemovePrimary = errors.New("cannot remove primary store; set a new primary first")

	// ErrInvalidLocationStatus indicates an invalid location status.
	ErrInvalidLocationStatus = errors.New("invalid location status")

	// ErrInvalidStatus indicates an invalid entry status.
	ErrInvalidStatus = errors.New("invalid entry status")

	// ErrTenantEmpty indicates an empty tenant ID was provided.
	ErrTenantEmpty = errors.New("tenant ID cannot be empty")

	// ErrTagEmpty indicates an empty tag name was provided.
	ErrTagEmpty = errors.New("tag name cannot be empty")

	// ErrKeyIDEmpty indicates an empty encryption key ID was provided.
	ErrKeyIDEmpty = errors.New("encryption key ID cannot be empty")

	// ErrTargetsEmpty indicates an empty targets list was provided.
	ErrTargetsEmpty = errors.New("replication targets cannot be empty")

	// ErrCatalogNil indicates a nil catalog manager was provided.
	ErrCatalogNil = errors.New("catalog manager cannot be nil")

	// ErrACLNil indicates a nil ACL was provided.
	ErrACLNil = errors.New("ACL cannot be nil")

	// ErrRequestNil indicates a nil request was provided.
	ErrRequestNil = errors.New("request cannot be nil")

	// ErrNilDEKResponse indicates a nil DEK response was received.
	ErrNilDEKResponse = errors.New("nil DEK response")

	// ErrEmptyDEKPath indicates the DEK path is empty.
	ErrEmptyDEKPath = errors.New("empty DEK path")

	// ErrEmptyKeyMaterial indicates the DEK response contained no key material.
	ErrEmptyKeyMaterial = errors.New("DEK response contains no key material")

	// ErrVerificationDataMismatch indicates verification failed due to data mismatch.
	ErrVerificationDataMismatch = errors.New("verification failed: data mismatch")
)

// ErrorCode represents error codes for catalog operations.
type ErrorCode string

const (
	// ErrCodeKeyEmpty indicates an empty key was provided.
	ErrCodeKeyEmpty ErrorCode = "KEY_EMPTY"

	// ErrCodeKeyTooLong indicates a key exceeds the maximum length.
	ErrCodeKeyTooLong ErrorCode = "KEY_TOO_LONG"

	// ErrCodeKeyInvalid indicates a key contains invalid characters or format.
	ErrCodeKeyInvalid ErrorCode = "KEY_INVALID"

	// ErrCodeNamespaceEmpty indicates an empty namespace was provided.
	ErrCodeNamespaceEmpty ErrorCode = "NAMESPACE_EMPTY"

	// ErrCodeNamespaceInvalid indicates a namespace contains invalid characters or format.
	ErrCodeNamespaceInvalid ErrorCode = "NAMESPACE_INVALID"

	// ErrCodeNamespaceTooLong indicates a namespace exceeds the maximum length.
	ErrCodeNamespaceTooLong ErrorCode = "NAMESPACE_TOO_LONG"

	// ErrCodePathEmpty indicates an empty path was provided.
	ErrCodePathEmpty ErrorCode = "PATH_EMPTY"

	// ErrCodePathInvalid indicates a path contains invalid characters or format.
	ErrCodePathInvalid ErrorCode = "PATH_INVALID"

	// ErrCodePathTooLong indicates a path exceeds the maximum length.
	ErrCodePathTooLong ErrorCode = "PATH_TOO_LONG"

	// ErrCodeEntryNotFound indicates a catalog entry was not found.
	ErrCodeEntryNotFound ErrorCode = "ENTRY_NOT_FOUND"

	// ErrCodeEntryExists indicates a catalog entry already exists.
	ErrCodeEntryExists ErrorCode = "ENTRY_EXISTS"

	// ErrCodeVersionMismatch indicates a version conflict during update.
	ErrCodeVersionMismatch ErrorCode = "VERSION_MISMATCH"

	// ErrCodeEncodingFailed indicates encoding of catalog data failed.
	ErrCodeEncodingFailed ErrorCode = "ENCODING_FAILED"

	// ErrCodeDecodingFailed indicates decoding of catalog data failed.
	ErrCodeDecodingFailed ErrorCode = "DECODING_FAILED"

	// ErrCodeStorageFailed indicates a storage operation failed.
	ErrCodeStorageFailed ErrorCode = "STORAGE_FAILED"

	// ErrCodeBackendNotFound indicates a storage backend was not found.
	ErrCodeBackendNotFound ErrorCode = "BACKEND_NOT_FOUND"

	// ErrCodeBackendUnhealthy indicates a storage backend is unhealthy.
	ErrCodeBackendUnhealthy ErrorCode = "BACKEND_UNHEALTHY"

	// ErrCodeLocationNotFound indicates a storage location was not found.
	ErrCodeLocationNotFound ErrorCode = "LOCATION_NOT_FOUND"

	// ErrCodePolicyInvalid indicates an invalid policy configuration.
	ErrCodePolicyInvalid ErrorCode = "POLICY_INVALID"

	// ErrCodeQuotaExceeded indicates a quota limit has been exceeded.
	ErrCodeQuotaExceeded ErrorCode = "QUOTA_EXCEEDED"

	// ErrCodeChecksumMismatch indicates data integrity failure.
	ErrCodeChecksumMismatch ErrorCode = "CHECKSUM_MISMATCH"

	// ErrCodeQueueFull indicates an operation queue is full.
	ErrCodeQueueFull ErrorCode = "QUEUE_FULL"

	// ErrCodeCatalogClosed indicates the catalog has been closed.
	ErrCodeCatalogClosed ErrorCode = "CATALOG_CLOSED"

	// ErrCodeInvalidLocation indicates an invalid storage location.
	ErrCodeInvalidLocation ErrorCode = "INVALID_LOCATION"

	// ErrCodeInvalidEntry indicates an invalid catalog entry.
	ErrCodeInvalidEntry ErrorCode = "INVALID_ENTRY"

	// ErrCodeIndexFailed indicates an index operation failed.
	ErrCodeIndexFailed ErrorCode = "INDEX_FAILED"

	// ErrCodeACLInvalidAction indicates an invalid ACL action was specified.
	ErrCodeACLInvalidAction ErrorCode = "ACL_INVALID_ACTION"

	// ErrCodeACLPrincipalEmpty indicates an empty principal was provided.
	ErrCodeACLPrincipalEmpty ErrorCode = "ACL_PRINCIPAL_EMPTY"

	// ErrCodeACLPermissionsEmpty indicates empty permissions were provided.
	ErrCodeACLPermissionsEmpty ErrorCode = "ACL_PERMISSIONS_EMPTY"

	// ErrCodeACLCannotRevokeOwner indicates an attempt to revoke owner access.
	ErrCodeACLCannotRevokeOwner ErrorCode = "ACL_CANNOT_REVOKE_OWNER"

	// ErrCodeACLOwnerRequired indicates owner is required for ACL.
	ErrCodeACLOwnerRequired ErrorCode = "ACL_OWNER_REQUIRED"

	// ErrCodeACLAccessDenied indicates access was denied.
	ErrCodeACLAccessDenied ErrorCode = "ACL_ACCESS_DENIED"
)

// String returns the string representation of the error code.
func (c ErrorCode) String() string {
	return string(c)
}

// CatalogError is the base error type for catalog operations.
type CatalogError struct {
	Code    ErrorCode
	Message string
	Key     string
	Cause   error
}

// Error implements the error interface.
func (e *CatalogError) Error() string {
	var result string
	if e.Key != "" {
		result = string(e.Code) + ": " + e.Message + " (key=" + e.Key + ")"
	} else {
		result = string(e.Code) + ": " + e.Message
	}
	if e.Cause != nil {
		result = result + ": " + e.Cause.Error()
	}
	return result
}

// Unwrap returns the underlying cause for errors.Unwrap support.
func (e *CatalogError) Unwrap() error {
	return e.Cause
}

// Is implements error comparison for errors.Is.
func (e *CatalogError) Is(target error) bool {
	if target == nil {
		return false
	}

	var ce *CatalogError
	if errors.As(target, &ce) {
		return e.Code == ce.Code
	}

	if e.Cause != nil {
		return errors.Is(e.Cause, target)
	}

	return false
}

// NewKeyEmptyError creates a new error for empty key.
func NewKeyEmptyError() *CatalogError {
	return &CatalogError{
		Code:    ErrCodeKeyEmpty,
		Message: "key cannot be empty",
	}
}

// NewKeyTooLongError creates a new error for key exceeding maximum length.
func NewKeyTooLongError(keyLen, maxLen int) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeKeyTooLong,
		Message: "key length " + strconv.Itoa(keyLen) + " exceeds maximum " + strconv.Itoa(maxLen),
	}
}

// NewKeyInvalidError creates a new error for invalid key format.
func NewKeyInvalidError(key string, reason string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeKeyInvalid,
		Message: "invalid key: " + reason,
		Key:     key,
	}
}

// NewKeyInvalidFormatError creates a new error for invalid key format.
func NewKeyInvalidFormatError(key string, reason string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeKeyInvalid,
		Message: "invalid key format: " + reason,
		Key:     key,
	}
}

// NewNamespaceEmptyError creates a new error for empty namespace.
func NewNamespaceEmptyError() *CatalogError {
	return &CatalogError{
		Code:    ErrCodeNamespaceEmpty,
		Message: "namespace cannot be empty",
	}
}

// NewNamespaceInvalidError creates a new error for invalid namespace format.
func NewNamespaceInvalidError(namespace string, reason string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeNamespaceInvalid,
		Message: "invalid namespace '" + namespace + "': " + reason,
	}
}

// NewInvalidNamespaceError creates a new error for invalid namespace.
func NewInvalidNamespaceError(namespace string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeNamespaceInvalid,
		Message: "invalid namespace '" + namespace + "': must start with letter or digit, contain only alphanumeric, hyphen, or underscore",
	}
}

// NewNamespaceTooLongError creates a new error for namespace exceeding maximum length.
func NewNamespaceTooLongError(namespace string, nsLen, maxLen int) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeNamespaceTooLong,
		Message: "namespace '" + namespace + "' length " + strconv.Itoa(nsLen) + " exceeds maximum " + strconv.Itoa(maxLen),
	}
}

// NewPathEmptyError creates a new error for empty path.
func NewPathEmptyError() *CatalogError {
	return &CatalogError{
		Code:    ErrCodePathEmpty,
		Message: "path cannot be empty",
	}
}

// NewPathInvalidError creates a new error for invalid path format.
func NewPathInvalidError(path string, reason string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodePathInvalid,
		Message: "invalid path '" + path + "': " + reason,
	}
}

// NewInvalidPathError creates a new error for invalid path.
func NewInvalidPathError(path string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodePathInvalid,
		Message: "invalid path '" + path + "': must not start/end with slash, contain consecutive slashes, or have control characters",
	}
}

// NewPathTooLongError creates a new error for path exceeding maximum length.
func NewPathTooLongError(path string, pathLen, maxLen int) *CatalogError {
	return &CatalogError{
		Code:    ErrCodePathTooLong,
		Message: "path length " + strconv.Itoa(pathLen) + " exceeds maximum " + strconv.Itoa(maxLen),
	}
}

// NewEntryNotFoundError creates a new error for missing catalog entry.
func NewEntryNotFoundError(key string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeEntryNotFound,
		Message: "catalog entry not found",
		Key:     key,
	}
}

// NewEntryExistsError creates a new error for duplicate catalog entry.
func NewEntryExistsError(key string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeEntryExists,
		Message: "catalog entry already exists",
		Key:     key,
	}
}

// NewVersionMismatchError creates a new error for version conflict.
func NewVersionMismatchError(key string, expected, actual uint64) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeVersionMismatch,
		Message: "version mismatch: expected " + strconv.FormatUint(expected, 10) + ", got " + strconv.FormatUint(actual, 10),
		Key:     key,
	}
}

// NewEncodingError creates a new error for encoding failure.
func NewEncodingError(key string, cause error) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeEncodingFailed,
		Message: "failed to encode catalog entry",
		Key:     key,
		Cause:   cause,
	}
}

// NewDecodingError creates a new error for decoding failure.
func NewDecodingError(key string, cause error) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeDecodingFailed,
		Message: "failed to decode catalog entry",
		Key:     key,
		Cause:   cause,
	}
}

// NewStorageError creates a new error for storage operation failure.
func NewStorageError(operation string, cause error) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeStorageFailed,
		Message: "storage operation failed: " + operation,
		Cause:   cause,
	}
}

// NewBackendNotFoundError creates a new error for missing storage backend.
func NewBackendNotFoundError(backend string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeBackendNotFound,
		Message: "storage backend not found: " + backend,
	}
}

// NewBackendUnhealthyError creates a new error for unhealthy storage backend.
func NewBackendUnhealthyError(backend string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeBackendUnhealthy,
		Message: "storage backend unhealthy: " + backend,
	}
}

// NewLocationNotFoundError creates a new error for missing storage location.
func NewLocationNotFoundError(key, backend string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeLocationNotFound,
		Message: "location not found on backend '" + backend + "'",
		Key:     key,
	}
}

// NewPolicyInvalidError creates a new error for invalid policy configuration.
func NewPolicyInvalidError(policyType, reason string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodePolicyInvalid,
		Message: "invalid " + policyType + " policy: " + reason,
	}
}

// NewQuotaExceededError creates a new error for quota limit exceeded.
func NewQuotaExceededError(tenantID, quotaType string, limit, current int64) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeQuotaExceeded,
		Message: "quota exceeded for tenant '" + tenantID + "': " + quotaType + " limit " + strconv.FormatInt(limit, 10) + ", current " + strconv.FormatInt(current, 10),
	}
}

// NewChecksumMismatchError creates a new error for data integrity failure.
func NewChecksumMismatchError(key string, expected, actual []byte) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeChecksumMismatch,
		Message: "checksum mismatch: expected " + hex.EncodeToString(expected) + ", got " + hex.EncodeToString(actual),
		Key:     key,
	}
}

// NewQueueFullError creates a new error for full operation queue.
func NewQueueFullError(queueType string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeQueueFull,
		Message: queueType + " queue is full",
	}
}

// NewCatalogClosedError creates a new error for closed catalog.
func NewCatalogClosedError() *CatalogError {
	return &CatalogError{
		Code:    ErrCodeCatalogClosed,
		Message: "catalog is closed",
	}
}

// NewInvalidLocationError creates a new error for invalid storage location.
func NewInvalidLocationError(store, reason string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeInvalidLocation,
		Message: "invalid location for store '" + store + "': " + reason,
	}
}

// NewInvalidEntryError creates a new error for invalid catalog entry.
func NewInvalidEntryError(field, reason string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeInvalidEntry,
		Message: "invalid entry field '" + field + "': " + reason,
	}
}

// NewIndexError creates a new error for index operation failure.
func NewIndexError(indexType, key string, cause error) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeIndexFailed,
		Message: "failed to update " + indexType + " index",
		Key:     key,
		Cause:   cause,
	}
}

// NewACLInvalidActionError creates a new error for invalid ACL action.
func NewACLInvalidActionError(action string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeACLInvalidAction,
		Message: "invalid ACL action: " + action,
	}
}

// NewACLPrincipalEmptyError creates a new error for empty principal.
func NewACLPrincipalEmptyError() *CatalogError {
	return &CatalogError{
		Code:    ErrCodeACLPrincipalEmpty,
		Message: "principal cannot be empty",
	}
}

// NewACLPermissionsEmptyError creates a new error for empty permissions.
func NewACLPermissionsEmptyError() *CatalogError {
	return &CatalogError{
		Code:    ErrCodeACLPermissionsEmpty,
		Message: "permissions cannot be empty",
	}
}

// NewACLCannotRevokeOwnerError creates a new error for attempting to revoke owner access.
func NewACLCannotRevokeOwnerError() *CatalogError {
	return &CatalogError{
		Code:    ErrCodeACLCannotRevokeOwner,
		Message: "cannot revoke owner's access",
	}
}

// NewACLOwnerRequiredError creates a new error when owner is required.
func NewACLOwnerRequiredError() *CatalogError {
	return &CatalogError{
		Code:    ErrCodeACLOwnerRequired,
		Message: "owner is required for ACL",
	}
}

// NewACLAccessDeniedError creates a new error for access denied.
func NewACLAccessDeniedError(principal, objectKey, action string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeACLAccessDenied,
		Message: "access denied for principal '" + principal + "' to perform '" + action + "'",
		Key:     objectKey,
	}
}

// IsNotFound returns true if the error is an entry not found error.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		return ce.Code == ErrCodeEntryNotFound
	}
	return false
}

// IsVersionMismatch returns true if the error is a version mismatch error.
func IsVersionMismatch(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		return ce.Code == ErrCodeVersionMismatch
	}
	return false
}

// IsQuotaExceeded returns true if the error is a quota exceeded error.
func IsQuotaExceeded(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		return ce.Code == ErrCodeQuotaExceeded
	}
	return false
}

// IsCatalogClosed returns true if the error is a catalog closed error.
func IsCatalogClosed(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		return ce.Code == ErrCodeCatalogClosed
	}
	return false
}

// IsBackendError returns true if the error is a backend-related error.
func IsBackendError(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		return ce.Code == ErrCodeBackendNotFound || ce.Code == ErrCodeBackendUnhealthy
	}
	return false
}

// IsIndexError returns true if the error is an index operation error.
func IsIndexError(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		return ce.Code == ErrCodeIndexFailed
	}
	return false
}

// IsValidationError returns true if the error is a validation-related error.
func IsValidationError(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		switch ce.Code {
		case ErrCodeKeyEmpty, ErrCodeKeyTooLong, ErrCodeKeyInvalid,
			ErrCodeNamespaceEmpty, ErrCodeNamespaceInvalid, ErrCodeNamespaceTooLong,
			ErrCodePathEmpty, ErrCodePathInvalid, ErrCodePathTooLong,
			ErrCodePolicyInvalid, ErrCodeInvalidLocation, ErrCodeInvalidEntry:
			return true
		}
	}
	return false
}

// IsStorageError returns true if the error is a storage-related error.
func IsStorageError(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		switch ce.Code {
		case ErrCodeStorageFailed, ErrCodeEncodingFailed, ErrCodeDecodingFailed,
			ErrCodeChecksumMismatch, ErrCodeLocationNotFound, ErrCodeIndexFailed:
			return true
		}
	}
	return false
}

// IsACLError returns true if the error is an ACL-related error.
func IsACLError(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		switch ce.Code {
		case ErrCodeACLInvalidAction, ErrCodeACLPrincipalEmpty, ErrCodeACLPermissionsEmpty,
			ErrCodeACLCannotRevokeOwner, ErrCodeACLOwnerRequired, ErrCodeACLAccessDenied:
			return true
		}
	}
	return false
}

// IsAccessDenied returns true if the error is an access denied error.
func IsAccessDenied(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		return ce.Code == ErrCodeACLAccessDenied
	}
	return false
}
