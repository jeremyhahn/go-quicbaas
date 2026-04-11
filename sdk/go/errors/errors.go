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

// Package errors provides typed errors for the QuicBaaS SDK.
package errors

import (
	"errors"
	"fmt"
)

// QBError is the base error type for all SDK errors.
type QBError struct {
	// Code is the error code category.
	Code ErrorCode

	// Op is the operation that failed.
	Op string

	// Message is the human-readable error message.
	Message string

	// Err is the underlying error, if any.
	Err error

	// Context contains additional error context.
	Context map[string]interface{}
}

// Error implements the error interface.
func (e *QBError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %s: %v", e.Code, e.Op, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s: %s", e.Code, e.Op, e.Message)
}

// Unwrap returns the underlying error.
func (e *QBError) Unwrap() error {
	return e.Err
}

// Is reports whether the error matches the target.
func (e *QBError) Is(target error) bool {
	t, ok := target.(*QBError)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// IsRetryable returns true if the error can be retried.
func (e *QBError) IsRetryable() bool {
	return e.Code.IsRetryable()
}

// WithContext adds context to the error.
func (e *QBError) WithContext(key string, value interface{}) *QBError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

// New creates a new QBError.
func New(code ErrorCode, op, message string) *QBError {
	return &QBError{
		Code:    code,
		Op:      op,
		Message: message,
	}
}

// Wrap wraps an existing error with a QBError.
func Wrap(err error, code ErrorCode, op, message string) *QBError {
	return &QBError{
		Code:    code,
		Op:      op,
		Message: message,
		Err:     err,
	}
}

// NewInvalidInput creates an invalid input error.
func NewInvalidInput(op, message string) *QBError {
	return New(CodeInvalidInput, op, message)
}

// NewNotFound creates a not found error.
func NewNotFound(op, message string) *QBError {
	return New(CodeNotFound, op, message)
}

// NewAlreadyExists creates an already exists error.
func NewAlreadyExists(op, message string) *QBError {
	return New(CodeAlreadyExists, op, message)
}

// NewUnauthorized creates an unauthorized error.
func NewUnauthorized(op, message string) *QBError {
	return New(CodeUnauthorized, op, message)
}

// NewForbidden creates a forbidden error.
func NewForbidden(op, message string) *QBError {
	return New(CodeForbidden, op, message)
}

// NewConflict creates a conflict error.
func NewConflict(op, message string) *QBError {
	return New(CodeConflict, op, message)
}

// NewInternal creates an internal error.
func NewInternal(op, message string) *QBError {
	return New(CodeInternal, op, message)
}

// NewUnavailable creates an unavailable error.
func NewUnavailable(op, message string) *QBError {
	return New(CodeUnavailable, op, message)
}

// NewTimeout creates a timeout error.
func NewTimeout(op, message string) *QBError {
	return New(CodeTimeout, op, message)
}

// NewCanceled creates a canceled error.
func NewCanceled(op, message string) *QBError {
	return New(CodeCanceled, op, message)
}

// NewConnection creates a connection error.
func NewConnection(op, message string) *QBError {
	return New(CodeConnection, op, message)
}

// NewSealed creates a sealed error.
func NewSealed(op, message string) *QBError {
	return New(CodeSealed, op, message)
}

// NewNotInitialized creates a not initialized error.
func NewNotInitialized(op, message string) *QBError {
	return New(CodeNotInitialized, op, message)
}

// NewConfiguration creates a configuration error.
func NewConfiguration(op, message string) *QBError {
	return New(CodeConfiguration, op, message)
}

// NewResourceExhausted creates a resource exhausted error.
func NewResourceExhausted(op, message string) *QBError {
	return New(CodeResourceExhausted, op, message)
}

// NewUnimplemented creates an unimplemented error.
func NewUnimplemented(op, message string) *QBError {
	return New(CodeUnimplemented, op, message)
}

// NewPreconditionFailed creates a precondition failed error.
func NewPreconditionFailed(op, message string) *QBError {
	return New(CodePreconditionFailed, op, message)
}

// NewRateLimited creates a rate limited error.
func NewRateLimited(op, message string) *QBError {
	return New(CodeRateLimited, op, message)
}

// NewTransport creates a transport error.
func NewTransport(op, message string) *QBError {
	return New(CodeTransport, op, message)
}

// Helper functions for error inspection

// Code returns the error code from an error.
// Returns CodeUnknown if the error is not a QBError.
func Code(err error) ErrorCode {
	var de *QBError
	if errors.As(err, &de) {
		return de.Code
	}
	return CodeUnknown
}

// IsNotFound returns true if the error is a not found error.
func IsNotFound(err error) bool {
	return Code(err) == CodeNotFound
}

// IsAlreadyExists returns true if the error is an already exists error.
func IsAlreadyExists(err error) bool {
	return Code(err) == CodeAlreadyExists
}

// IsInvalidInput returns true if the error is an invalid input error.
func IsInvalidInput(err error) bool {
	return Code(err) == CodeInvalidInput
}

// IsUnauthorized returns true if the error is an unauthorized error.
func IsUnauthorized(err error) bool {
	return Code(err) == CodeUnauthorized
}

// IsForbidden returns true if the error is a forbidden error.
func IsForbidden(err error) bool {
	return Code(err) == CodeForbidden
}

// IsConflict returns true if the error is a conflict error.
func IsConflict(err error) bool {
	return Code(err) == CodeConflict
}

// IsTimeout returns true if the error is a timeout error.
func IsTimeout(err error) bool {
	return Code(err) == CodeTimeout
}

// IsUnavailable returns true if the error is an unavailable error.
func IsUnavailable(err error) bool {
	return Code(err) == CodeUnavailable
}

// IsCanceled returns true if the error is a canceled error.
func IsCanceled(err error) bool {
	return Code(err) == CodeCanceled
}

// IsSealed returns true if the error is a sealed error.
func IsSealed(err error) bool {
	return Code(err) == CodeSealed
}

// IsUnimplemented returns true if the error is an unimplemented error.
func IsUnimplemented(err error) bool {
	return Code(err) == CodeUnimplemented
}

// IsRetryable returns true if the error can be retried.
func IsRetryable(err error) bool {
	var de *QBError
	if errors.As(err, &de) {
		return de.IsRetryable()
	}
	return false
}

// IsClientError returns true if the error is a client-side error.
func IsClientError(err error) bool {
	return Code(err).IsClientError()
}

// IsServerError returns true if the error is a server-side error.
func IsServerError(err error) bool {
	return Code(err).IsServerError()
}

// Op returns the operation from an error.
func Op(err error) string {
	var de *QBError
	if errors.As(err, &de) {
		return de.Op
	}
	return ""
}

// Message returns the message from an error.
func Message(err error) string {
	var de *QBError
	if errors.As(err, &de) {
		return de.Message
	}
	return err.Error()
}
