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
	stderrors "errors"
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	pkgsdlc "github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
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

// InvalidUserIDError indicates a request user id that is not a non-zero
// decimal uint64, the form of the iam user id. Err is the strconv failure, or
// nil when the value parsed to zero.
type InvalidUserIDError struct {
	Service string
	Method  string
	Value   string
	Err     error
}

// Error implements the error interface.
func (e *InvalidUserIDError) Error() string {
	msg := "INVALID_INPUT: embedded: " + e.Service + "/" + e.Method + ": user id " +
		strconv.Quote(e.Value) + " is not a non-zero decimal uint64"
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap returns the underlying strconv error, if any.
func (e *InvalidUserIDError) Unwrap() error { return e.Err }

// parseIAMUserID converts a request's string user id into the iam uint64 id.
func parseIAMUserID(service, method, value string) (uint64, error) {
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 {
		return 0, &InvalidUserIDError{Service: service, Method: method, Value: value, Err: err}
	}
	return id, nil
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

// domainSentinelCodes classes the project and SDLC sentinels a service
// returns the way the gRPC, REST and MCP transports do, so an embedded caller
// reads an unknown project or binding as CodeNotFound like every other caller.
var domainSentinelCodes = []struct {
	sentinel error
	code     errors.ErrorCode
}{
	{pkgsdlc.ErrNotFound, errors.CodeNotFound},
	{orchestration.ErrProjectNotFound, errors.CodeNotFound},
	{orchestration.ErrTaskNotFound, errors.CodeNotFound},
	{pkgsdlc.ErrAlreadyExists, errors.CodeAlreadyExists},
	{pkgsdlc.ErrVersionConflict, errors.CodeAlreadyExists},
	{pkgsdlc.ErrWebhookRouteConflict, errors.CodeAlreadyExists},
	{orchestration.ErrTaskSourceAlreadyAdopted, errors.CodeAlreadyExists},
	{pkgsdlc.ErrAdoptionRefused, errors.CodePreconditionFailed},
	{orchestration.ErrTaskInvalidState, errors.CodePreconditionFailed},
	{pkgsdlc.ErrAdoptionIncomplete, errors.CodeUnavailable},
	{pkgsdlc.ErrUnavailable, errors.CodeUnavailable},
	{pkgsdlc.ErrInvalidInput, errors.CodeInvalidInput},
	{pkgsdlc.ErrReview, errors.CodeInvalidInput},
	{orchestration.ErrWorkflowNotFound, errors.CodeNotFound},
	{orchestration.ErrWorkflowAlreadyExists, errors.CodeAlreadyExists},
	{orchestration.ErrWorkflowValidation, errors.CodeInvalidInput},
	{orchestration.ErrInvalidInput, errors.CodeInvalidInput},
}

// domainErrorCode classes a project or SDLC service error, reporting false
// for any other error.
func domainErrorCode(err error) (errors.ErrorCode, bool) {
	var (
		accessDenied  *service.ProjectAccessDeniedError
		notFound      *service.ProjectNotFoundError
		unresolved    *service.ProjectUnresolvedError
		alreadyExists *service.ProjectAlreadyExistsError
		invalid       *service.ProjectInvalidError
		roleInvalid   *service.ProjectRoleInvalidError
		forge         *pkgsdlc.ForgeNotRegisteredError
		strategy      *pkgsdlc.StrategyNotRegisteredError
	)
	switch {
	case stderrors.As(err, &accessDenied):
		return errors.CodeForbidden, true
	case stderrors.As(err, &notFound), stderrors.As(err, &unresolved):
		return errors.CodeNotFound, true
	case stderrors.As(err, &alreadyExists):
		return errors.CodeAlreadyExists, true
	case stderrors.As(err, &invalid), stderrors.As(err, &roleInvalid),
		stderrors.As(err, &forge), stderrors.As(err, &strategy):
		return errors.CodeInvalidInput, true
	}
	for _, class := range domainSentinelCodes {
		if stderrors.Is(err, class.sentinel) {
			return class.code, true
		}
	}
	return errors.CodeUnknown, false
}

// classifyServiceError gives a project or SDLC service error the SDK code the
// network transports report for it, keeping the service error as the cause so
// errors.As still finds it. Any other error, including one already coded, is
// returned unchanged.
func classifyServiceError(svcName, method string, err error) error {
	if err == nil {
		return nil
	}
	var coded *errors.QBError
	if stderrors.As(err, &coded) {
		return err
	}
	code, ok := domainErrorCode(err)
	if !ok {
		return err
	}
	return errors.Wrap(err, code, "embedded."+svcName+"."+method, "service request failed")
}
