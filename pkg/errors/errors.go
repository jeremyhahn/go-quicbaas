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

// Package errors provides structured error handling for QuicBaaS.
// It is a facade over the go-qrdb error framework, re-exporting all symbols
// so that QuicBaaS-internal packages need only import this package.
package errors

import (
	stderrors "errors"
	"fmt"
	"time"

	dberrors "github.com/jeremyhahn/go-qrdb/pkg/errors"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// ---------------------------------------------------------------------------
// Core type aliases — identical to dberrors, zero overhead
// ---------------------------------------------------------------------------

// ErrorCode classifies errors into categories.
type ErrorCode = dberrors.ErrorCode

// QBError is the structured QuicBaaS error type with code, operation, and context.
type QBError = dberrors.QRDBError

// QuotaExceededError is returned when a resource quota is exceeded.
type QuotaExceededError = dberrors.QuotaExceededError

// MessageError is a simple typed error wrapping a message string.
type MessageError = dberrors.MessageError

// WrappedMessageError is a message error that wraps an underlying error.
type WrappedMessageError = dberrors.WrappedMessageError

// ParameterNotFoundError indicates a required parameter was not found.
type ParameterNotFoundError = dberrors.ParameterNotFoundError

// ---------------------------------------------------------------------------
// Error code constants
// ---------------------------------------------------------------------------

const (
	// ErrUnknown represents an unknown or unspecified error.
	ErrUnknown = dberrors.ErrUnknown

	// ErrNotFound indicates that the requested resource was not found.
	ErrNotFound = dberrors.ErrNotFound

	// ErrConflict indicates a conflict with existing state (e.g., duplicate key).
	ErrConflict = dberrors.ErrConflict

	// ErrTimeout indicates that an operation timed out.
	ErrTimeout = dberrors.ErrTimeout

	// ErrInternal represents an internal server error.
	ErrInternal = dberrors.ErrInternal

	// ErrInvalidInput indicates invalid input parameters.
	ErrInvalidInput = dberrors.ErrInvalidInput

	// ErrUnauthorized indicates authentication is required.
	ErrUnauthorized = dberrors.ErrUnauthorized

	// ErrForbidden indicates the operation is not permitted.
	ErrForbidden = dberrors.ErrForbidden

	// ErrUnavailable indicates the service is unavailable.
	ErrUnavailable = dberrors.ErrUnavailable

	// ErrStorage indicates a storage layer error.
	ErrStorage = dberrors.ErrStorage

	// ErrRaft indicates a Raft consensus error.
	ErrRaft = dberrors.ErrRaft

	// ErrEncryption indicates an encryption/decryption error.
	ErrEncryption = dberrors.ErrEncryption

	// ErrDecryption indicates a decryption error.
	ErrDecryption = dberrors.ErrDecryption

	// ErrValidation indicates a validation error.
	ErrValidation = dberrors.ErrValidation

	// ErrConfiguration indicates a configuration error.
	ErrConfiguration = dberrors.ErrConfiguration

	// ErrCanceled indicates the operation was canceled.
	ErrCanceled = dberrors.ErrCanceled

	// ErrDeadlineExceeded indicates the deadline was exceeded.
	ErrDeadlineExceeded = dberrors.ErrDeadlineExceeded

	// ErrAlreadyExists indicates the resource already exists.
	ErrAlreadyExists = dberrors.ErrAlreadyExists

	// ErrResourceExhausted indicates resources are exhausted.
	ErrResourceExhausted = dberrors.ErrResourceExhausted

	// ErrPreconditionFailed indicates a precondition check failed.
	ErrPreconditionFailed = dberrors.ErrPreconditionFailed

	// ErrAborted indicates the operation was aborted.
	ErrAborted = dberrors.ErrAborted

	// ErrOutOfRange indicates a value is out of range.
	ErrOutOfRange = dberrors.ErrOutOfRange

	// ErrUnimplemented indicates the operation is not implemented.
	ErrUnimplemented = dberrors.ErrUnimplemented

	// ErrDataLoss indicates unrecoverable data loss or corruption.
	ErrDataLoss = dberrors.ErrDataLoss

	// ErrSnapshot indicates a snapshot operation error.
	ErrSnapshot = dberrors.ErrSnapshot

	// ErrReplication indicates a replication error.
	ErrReplication = dberrors.ErrReplication

	// ErrLeaderNotAvailable indicates no Raft leader is available.
	ErrLeaderNotAvailable = dberrors.ErrLeaderNotAvailable

	// ErrClusterNotReady indicates the cluster is not ready.
	ErrClusterNotReady = dberrors.ErrClusterNotReady

	// ErrQuorumLost indicates the cluster has lost quorum.
	ErrQuorumLost = dberrors.ErrQuorumLost

	// ErrSplitBrain indicates a split-brain scenario has been detected.
	ErrSplitBrain = dberrors.ErrSplitBrain

	// ErrRateLimited indicates the operation was rate limited.
	ErrRateLimited = dberrors.ErrRateLimited

	// ErrNotImplemented indicates functionality is not yet implemented.
	ErrNotImplemented = dberrors.ErrNotImplemented

	// ErrNotAllowed indicates the operation is not allowed.
	ErrNotAllowed = dberrors.ErrNotAllowed

	// ErrSystemKeyspaceViolation indicates an attempt to access the reserved system keyspace.
	ErrSystemKeyspaceViolation = dberrors.ErrSystemKeyspaceViolation

	// ErrCrossTenantAccess indicates an attempt to access another tenant's data.
	ErrCrossTenantAccess = dberrors.ErrCrossTenantAccess

	// ErrMissingTenantContext indicates tenant context is required but not present.
	ErrMissingTenantContext = dberrors.ErrMissingTenantContext

	// ErrMissingJustification indicates a justification is required but not provided.
	ErrMissingJustification = dberrors.ErrMissingJustification

	// ErrMissingUserContext indicates user context is required but not present.
	ErrMissingUserContext = dberrors.ErrMissingUserContext

	// ErrCircuitOpen indicates the circuit breaker is open and rejecting requests.
	ErrCircuitOpen = dberrors.ErrCircuitOpen
)

// ---------------------------------------------------------------------------
// Constructors
// ---------------------------------------------------------------------------

// New creates a new QBError with the specified code, operation, and message.
var New = dberrors.New

// Newf creates a new QBError with formatted message.
var Newf = dberrors.Newf

// Wrap wraps an existing error with QuicBaaS error context.
var Wrap = dberrors.Wrap

// Wrapf wraps an error with additional formatted context.
var Wrapf = dberrors.Wrapf

// ---------------------------------------------------------------------------
// Error inspection helpers
// ---------------------------------------------------------------------------

// Is checks if an error matches a specific error code.
var Is = dberrors.Is

// As is a helper for errors.As with QBError.
var As = dberrors.As

// Code extracts the error code from an error, or returns ErrUnknown.
var Code = dberrors.Code

// Operation extracts the operation from an error, or returns empty string.
var Operation = dberrors.Operation

// IsRetryable checks if an error is retryable.
var IsRetryable = dberrors.IsRetryable

// GetContext retrieves context from a QBError.
var GetContext = dberrors.GetContext

// ---------------------------------------------------------------------------
// Common error constructors
// ---------------------------------------------------------------------------

// NotFound creates a not found error.
var NotFound = dberrors.NotFound

// AlreadyExists creates an already exists error.
var AlreadyExists = dberrors.AlreadyExists

// InvalidInput creates an invalid input error.
var InvalidInput = dberrors.InvalidInput

// InvalidInputf creates an invalid input error with formatting.
var InvalidInputf = dberrors.InvalidInputf

// Timeout creates a timeout error.
var Timeout = dberrors.Timeout

// Internal creates an internal error.
var Internal = dberrors.Internal

// Internalf creates an internal error with formatting.
var Internalf = dberrors.Internalf

// Unavailable creates an unavailable error.
var Unavailable = dberrors.Unavailable

// Unavailablef creates an unavailable error with formatting.
var Unavailablef = dberrors.Unavailablef

// NotImplemented creates a not implemented error.
var NotImplemented = dberrors.NotImplemented

// CircuitOpen creates a circuit open error indicating the circuit breaker is rejecting requests.
var CircuitOpen = dberrors.CircuitOpen

// ---------------------------------------------------------------------------
// Category predicate helpers
// ---------------------------------------------------------------------------

// IsCircuitOpen returns true if the error is a circuit open error.
var IsCircuitOpen = dberrors.IsCircuitOpen

// IsCanceled returns true if the error is a Canceled error.
var IsCanceled = dberrors.IsCanceled

// IsInvalidInput returns true if the error is an InvalidInput error.
var IsInvalidInput = dberrors.IsInvalidInput

// IsResourceExhausted returns true if the error is a ResourceExhausted error.
var IsResourceExhausted = dberrors.IsResourceExhausted

// IsSystemKeyspaceViolation returns true if the error is a system keyspace violation.
var IsSystemKeyspaceViolation = dberrors.IsSystemKeyspaceViolation

// IsCrossTenantAccess returns true if the error is a cross-tenant access violation.
var IsCrossTenantAccess = dberrors.IsCrossTenantAccess

// IsMissingTenantContext returns true if the error is a missing tenant context error.
var IsMissingTenantContext = dberrors.IsMissingTenantContext

// IsQuotaExceeded returns true if the error is a quota exceeded error.
var IsQuotaExceeded = dberrors.IsQuotaExceeded

// IsMissingJustification returns true if the error is a missing justification error.
var IsMissingJustification = dberrors.IsMissingJustification

// IsMissingUserContext returns true if the error is a missing user context error.
var IsMissingUserContext = dberrors.IsMissingUserContext

// IsNotFound returns true if the error is a NotFound error.
// It recognizes both QuicBaaS's QBError{ErrNotFound} and the qrdb
// SDK's transport.KeyNotFoundError, which is returned when KV lookups
// traverse the qrdb service layer via the embedded transport.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	var qe *QBError
	if stderrors.As(err, &qe) {
		return qe.Code == ErrNotFound
	}
	var knf *transport.KeyNotFoundError
	return stderrors.As(err, &knf)
}

// ---------------------------------------------------------------------------
// Structured error constructors
// ---------------------------------------------------------------------------

// NewQuotaExceededError creates a quota-exceeded error.
var NewQuotaExceededError = dberrors.NewQuotaExceededError

// NewSystemKeyspaceViolationError creates an error for system keyspace access attempts.
var NewSystemKeyspaceViolationError = dberrors.NewSystemKeyspaceViolationError

// NewCrossTenantAccessError creates an error for cross-tenant access attempts.
var NewCrossTenantAccessError = dberrors.NewCrossTenantAccessError

// NewMissingTenantContextError creates an error for missing tenant context.
var NewMissingTenantContextError = dberrors.NewMissingTenantContextError

// NewMissingJustificationError creates an error for missing justification.
var NewMissingJustificationError = dberrors.NewMissingJustificationError

// NewMissingUserContextError creates an error for missing user context.
var NewMissingUserContextError = dberrors.NewMissingUserContextError

// ---------------------------------------------------------------------------
// Parameter error types — QuicBaaS-specific, not in go-qrdb SDK
// ---------------------------------------------------------------------------

// ParameterExistsError indicates the parameter already exists.
type ParameterExistsError struct {
	Path string
}

// Error implements error interface.
func (e *ParameterExistsError) Error() string {
	return fmt.Sprintf("parameter already exists: %s", e.Path)
}

// InvalidPathError indicates an invalid parameter path.
type InvalidPathError struct {
	Path   string
	Reason string
}

// Error implements error interface.
func (e *InvalidPathError) Error() string {
	return fmt.Sprintf("invalid parameter path '%s': %s", e.Path, e.Reason)
}

// ParameterExpiredError indicates the parameter has expired.
type ParameterExpiredError struct {
	Path      string
	ExpiredAt time.Time
}

// Error implements error interface.
func (e *ParameterExpiredError) Error() string {
	return fmt.Sprintf("parameter '%s' expired at %s", e.Path, e.ExpiredAt.Format(time.RFC3339))
}

// ParameterEncryptionError indicates encryption/decryption failed.
type ParameterEncryptionError struct {
	Op  string
	Err error
}

// Error implements error interface.
func (e *ParameterEncryptionError) Error() string {
	return fmt.Sprintf("%s: encryption failed: %v", e.Op, e.Err)
}

// Unwrap returns the underlying error.
func (e *ParameterEncryptionError) Unwrap() error {
	return e.Err
}

// ParameterValueTooLargeError indicates the parameter value exceeds size limits.
type ParameterValueTooLargeError struct {
	Path    string
	Size    int
	MaxSize int
}

// Error implements error interface.
func (e *ParameterValueTooLargeError) Error() string {
	return fmt.Sprintf("parameter '%s' value size %d exceeds maximum %d", e.Path, e.Size, e.MaxSize)
}

// ParameterPathTooLongError indicates the parameter path exceeds length limits.
type ParameterPathTooLongError struct {
	Path      string
	Length    int
	MaxLength int
}

// Error implements error interface.
func (e *ParameterPathTooLongError) Error() string {
	return fmt.Sprintf("parameter path length %d exceeds maximum %d", e.Length, e.MaxLength)
}

// ParameterTagLimitError indicates too many tags on a parameter.
type ParameterTagLimitError struct {
	Path     string
	Count    int
	MaxCount int
}

// Error implements error interface.
func (e *ParameterTagLimitError) Error() string {
	return fmt.Sprintf("parameter '%s' has %d tags, exceeds maximum %d", e.Path, e.Count, e.MaxCount)
}

// NewParameterNotFoundError creates a new ParameterNotFoundError wrapped in QBError.
func NewParameterNotFoundError(path string) *QBError {
	return &QBError{
		Code: ErrNotFound,
		Op:   "parameterstore",
		Err:  &ParameterNotFoundError{Path: path},
	}
}

// NewParameterExistsError creates a new ParameterExistsError wrapped in QBError.
func NewParameterExistsError(path string) *QBError {
	return &QBError{
		Code: ErrAlreadyExists,
		Op:   "parameterstore",
		Err:  &ParameterExistsError{Path: path},
	}
}

// NewInvalidPathError creates a new InvalidPathError wrapped in QBError.
func NewInvalidPathError(path, reason string) *QBError {
	return &QBError{
		Code: ErrInvalidInput,
		Op:   "parameterstore",
		Err:  &InvalidPathError{Path: path, Reason: reason},
	}
}

// NewParameterExpiredError creates a new ParameterExpiredError wrapped in QBError.
func NewParameterExpiredError(path string, expiredAt time.Time) *QBError {
	return &QBError{
		Code: ErrNotFound,
		Op:   "parameterstore",
		Err:  &ParameterExpiredError{Path: path, ExpiredAt: expiredAt},
	}
}

// NewParameterEncryptionError creates a new ParameterEncryptionError wrapped in QBError.
func NewParameterEncryptionError(op string, err error) *QBError {
	return &QBError{
		Code: ErrEncryption,
		Op:   op,
		Err:  &ParameterEncryptionError{Op: op, Err: err},
	}
}

// NewParameterDecryptionError creates a new decryption error wrapped in QBError.
func NewParameterDecryptionError(op string, err error) *QBError {
	return &QBError{
		Code: ErrDecryption,
		Op:   op,
		Err:  &ParameterEncryptionError{Op: op, Err: err},
	}
}

// NewParameterValueTooLargeError creates a new ParameterValueTooLargeError wrapped in QBError.
func NewParameterValueTooLargeError(path string, size, maxSize int) *QBError {
	return &QBError{
		Code: ErrInvalidInput,
		Op:   "parameterstore",
		Err:  &ParameterValueTooLargeError{Path: path, Size: size, MaxSize: maxSize},
	}
}

// NewParameterPathTooLongError creates a new ParameterPathTooLongError wrapped in QBError.
func NewParameterPathTooLongError(path string, length, maxLength int) *QBError {
	return &QBError{
		Code: ErrInvalidInput,
		Op:   "parameterstore",
		Err:  &ParameterPathTooLongError{Path: path, Length: length, MaxLength: maxLength},
	}
}

// NewParameterTagLimitError creates a new ParameterTagLimitError wrapped in QBError.
func NewParameterTagLimitError(path string, count, maxCount int) *QBError {
	return &QBError{
		Code: ErrInvalidInput,
		Op:   "parameterstore",
		Err:  &ParameterTagLimitError{Path: path, Count: count, MaxCount: maxCount},
	}
}

// IsParameterNotFound returns true if the error is a ParameterNotFoundError.
func IsParameterNotFound(err error) bool {
	var pnf *ParameterNotFoundError
	return stderrors.As(err, &pnf)
}

// IsParameterExists returns true if the error is a ParameterExistsError.
func IsParameterExists(err error) bool {
	var pe *ParameterExistsError
	return stderrors.As(err, &pe)
}

// IsParameterExpired returns true if the error is a ParameterExpiredError.
func IsParameterExpired(err error) bool {
	var pe *ParameterExpiredError
	return stderrors.As(err, &pe)
}

// IsInvalidPath returns true if the error is an InvalidPathError.
func IsInvalidPath(err error) bool {
	var ip *InvalidPathError
	return stderrors.As(err, &ip)
}

// IsParameterEncryptionError returns true if the error is a ParameterEncryptionError.
func IsParameterEncryptionError(err error) bool {
	var pe *ParameterEncryptionError
	return stderrors.As(err, &pe)
}
