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

package errors

// ErrorCode represents a categorized error code.
type ErrorCode int

const (
	// CodeUnknown is an unknown error.
	CodeUnknown ErrorCode = iota

	// Client errors (4xx equivalent)

	// CodeInvalidInput indicates invalid input parameters.
	CodeInvalidInput

	// CodeNotFound indicates the requested resource was not found.
	CodeNotFound

	// CodeAlreadyExists indicates the resource already exists.
	CodeAlreadyExists

	// CodeUnauthorized indicates authentication is required.
	CodeUnauthorized

	// CodeForbidden indicates the operation is not permitted.
	CodeForbidden

	// CodeConflict indicates a conflict with the current state.
	CodeConflict

	// CodePreconditionFailed indicates a precondition was not met.
	CodePreconditionFailed

	// CodeRateLimited indicates the client is rate limited.
	CodeRateLimited

	// Server errors (5xx equivalent)

	// CodeInternal indicates an internal server error.
	CodeInternal

	// CodeUnavailable indicates the service is unavailable.
	CodeUnavailable

	// CodeTimeout indicates the operation timed out.
	CodeTimeout

	// CodeCanceled indicates the operation was canceled.
	CodeCanceled

	// CodeUnimplemented indicates the operation is not implemented.
	CodeUnimplemented

	// CodeResourceExhausted indicates resources are exhausted.
	CodeResourceExhausted

	// Specialized errors

	// CodeSealed indicates the server is sealed.
	CodeSealed

	// CodeNotInitialized indicates the server is not initialized.
	CodeNotInitialized

	// CodeConfiguration indicates a configuration error.
	CodeConfiguration

	// CodeConnection indicates a connection error.
	CodeConnection

	// CodeTransport indicates a transport-level error.
	CodeTransport
)

// String returns the string representation of the error code.
func (c ErrorCode) String() string {
	switch c {
	case CodeUnknown:
		return "UNKNOWN"
	case CodeInvalidInput:
		return "INVALID_INPUT"
	case CodeNotFound:
		return "NOT_FOUND"
	case CodeAlreadyExists:
		return "ALREADY_EXISTS"
	case CodeUnauthorized:
		return "UNAUTHORIZED"
	case CodeForbidden:
		return "FORBIDDEN"
	case CodeConflict:
		return "CONFLICT"
	case CodePreconditionFailed:
		return "PRECONDITION_FAILED"
	case CodeRateLimited:
		return "RATE_LIMITED"
	case CodeInternal:
		return "INTERNAL"
	case CodeUnavailable:
		return "UNAVAILABLE"
	case CodeTimeout:
		return "TIMEOUT"
	case CodeCanceled:
		return "CANCELED"
	case CodeUnimplemented:
		return "UNIMPLEMENTED"
	case CodeResourceExhausted:
		return "RESOURCE_EXHAUSTED"
	case CodeSealed:
		return "SEALED"
	case CodeNotInitialized:
		return "NOT_INITIALIZED"
	case CodeConfiguration:
		return "CONFIGURATION"
	case CodeConnection:
		return "CONNECTION"
	case CodeTransport:
		return "TRANSPORT"
	default:
		return "UNKNOWN"
	}
}

// IsRetryable returns true if errors with this code can be retried.
func (c ErrorCode) IsRetryable() bool {
	switch c {
	case CodeUnavailable, CodeTimeout, CodeResourceExhausted, CodeRateLimited:
		return true
	default:
		return false
	}
}

// IsClientError returns true if this is a client-side error.
func (c ErrorCode) IsClientError() bool {
	switch c {
	case CodeInvalidInput, CodeNotFound, CodeAlreadyExists,
		CodeUnauthorized, CodeForbidden, CodeConflict,
		CodePreconditionFailed, CodeRateLimited:
		return true
	default:
		return false
	}
}

// IsServerError returns true if this is a server-side error.
func (c ErrorCode) IsServerError() bool {
	switch c {
	case CodeInternal, CodeUnavailable, CodeTimeout,
		CodeUnimplemented, CodeResourceExhausted:
		return true
	default:
		return false
	}
}
