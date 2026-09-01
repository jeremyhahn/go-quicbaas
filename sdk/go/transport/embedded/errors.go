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

package embedded

import (
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// Error types for the embedded transport.
var (
	// ErrTransportClosed indicates the transport has been closed.
	ErrTransportClosed = errors.NewConnection("embedded", "transport is closed")

	// ErrNilService indicates a nil service was provided.
	ErrNilService = errors.NewInvalidInput("embedded.New", "service is required")

	// ErrStreamClosed indicates the stream has been closed.
	ErrStreamClosed = errors.NewConnection("embedded.stream", "stream is closed")

	// ErrSendOnClosedStream indicates an attempt to send on a closed stream.
	ErrSendOnClosedStream = errors.NewConnection("embedded.stream.Send", "cannot send on closed stream")

	// ErrRecvOnClosedStream indicates an attempt to receive on a closed stream.
	ErrRecvOnClosedStream = errors.NewConnection("embedded.stream.Recv", "cannot receive on closed stream")
)

// UnknownMethodError indicates the method is not supported.
type UnknownMethodError struct {
	Service string
	Method  string
}

// Error implements the error interface.
func (e *UnknownMethodError) Error() string {
	return "UNIMPLEMENTED: embedded: unknown method: " + e.Service + "/" + e.Method
}

// NewUnknownMethodError creates a new UnknownMethodError.
func NewUnknownMethodError(service, method string) *UnknownMethodError {
	return &UnknownMethodError{
		Service: service,
		Method:  method,
	}
}

// InvalidRequestTypeError indicates the request type is invalid for the method.
type InvalidRequestTypeError struct {
	Service      string
	Method       string
	ExpectedType string
	ActualType   string
}

// Error implements the error interface.
func (e *InvalidRequestTypeError) Error() string {
	return "INVALID_INPUT: embedded: invalid request type for " + e.Service + "/" + e.Method +
		": expected " + e.ExpectedType + ", got " + e.ActualType
}

// NewInvalidRequestTypeError creates a new InvalidRequestTypeError.
func NewInvalidRequestTypeError(service, method, expected, actual string) *InvalidRequestTypeError {
	return &InvalidRequestTypeError{
		Service:      service,
		Method:       method,
		ExpectedType: expected,
		ActualType:   actual,
	}
}

// ServiceNotConfiguredError indicates a sub-service is not configured.
type ServiceNotConfiguredError struct {
	Service string
}

// Error implements the error interface.
func (e *ServiceNotConfiguredError) Error() string {
	return "UNAVAILABLE: embedded: service not configured: " + e.Service
}

// NewServiceNotConfiguredError creates a new ServiceNotConfiguredError.
func NewServiceNotConfiguredError(serviceName string) *ServiceNotConfiguredError {
	return &ServiceNotConfiguredError{
		Service: serviceName,
	}
}

// StreamNotSupportedError indicates streaming is not supported for the method.
type StreamNotSupportedError struct {
	Service string
	Method  string
}

// Error implements the error interface.
func (e *StreamNotSupportedError) Error() string {
	return "UNIMPLEMENTED: embedded: streaming not supported for: " + e.Service + "/" + e.Method
}

// NewStreamNotSupportedError creates a new StreamNotSupportedError.
func NewStreamNotSupportedError(service, method string) *StreamNotSupportedError {
	return &StreamNotSupportedError{
		Service: service,
		Method:  method,
	}
}

// PermissionDeniedError indicates the authenticated subject on the request
// context is not permitted to act on the named resource. It exists so the
// embedded transport can refuse a cross-subject operation with the same
// outcome the REST handlers produce (HTTP 403) rather than silently serving
// another principal's data.
type PermissionDeniedError struct {
	Service string
	Method  string
	Reason  string
}

// Error implements the error interface.
func (e *PermissionDeniedError) Error() string {
	return "PERMISSION_DENIED: embedded: " + e.Service + "/" + e.Method + ": " + e.Reason
}

// NewPermissionDeniedError creates a new PermissionDeniedError.
func NewPermissionDeniedError(service, method, reason string) *PermissionDeniedError {
	return &PermissionDeniedError{
		Service: service,
		Method:  method,
		Reason:  reason,
	}
}
