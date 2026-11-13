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
	"context"
	stderrors "errors"
	"strconv"

	"google.golang.org/grpc/status"

	apigrpc "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	pkgerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	pkgsdlc "github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	grpctransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/grpc"
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

// AppUpdateDecodeError indicates an app update request that could not be
// decoded into the IAM update options. Cause is the encoding failure.
type AppUpdateDecodeError struct {
	Cause error
}

// Error implements the error interface.
func (e *AppUpdateDecodeError) Error() string {
	return "INVALID_INPUT: embedded: decode app update request: " + e.Cause.Error()
}

// Unwrap returns the encoding failure.
func (e *AppUpdateDecodeError) Unwrap() error {
	return e.Cause
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

// UnsupportedPolicyFieldError reports a policy request that sets a
// structured SDK field the embedded transport cannot translate into the
// engine's policy: structured conditions, rule subjects or policy targets.
// Storing the policy without the field would grant more than the caller
// asked for, so the request is refused. Field names the offending field.
type UnsupportedPolicyFieldError struct {
	Service string
	Method  string
	Field   string
}

// Error implements the error interface.
func (e *UnsupportedPolicyFieldError) Error() string {
	if e.Field == "effect" || e.Field == "enabled" {
		return "embedded: " + e.Service + "." + e.Method + ": " + e.Field +
			" is not supported; stored policies are enabled and deny by default"
	}
	return "embedded: " + e.Service + "." + e.Method + ": " + e.Field +
		" is not supported; describe the policy in spec_yaml instead"
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

// InvalidVectorIDError indicates a vector id that is not a decimal uint64,
// the form of the qrdb vector id. Err is the strconv failure.
type InvalidVectorIDError struct {
	Service string
	Method  string
	Value   string
	Err     error
}

// Error implements the error interface.
func (e *InvalidVectorIDError) Error() string {
	return "INVALID_INPUT: embedded: " + e.Service + "/" + e.Method + ": vector id " +
		strconv.Quote(e.Value) + " is not a decimal uint64: " + e.Err.Error()
}

// Unwrap returns the underlying strconv error.
func (e *InvalidVectorIDError) Unwrap() error { return e.Err }

// InvalidTransactionKeyError indicates a transaction client key that is not
// the decimal uint64 entity id the QuicBaaS service addresses entities by.
// Err is the strconv failure.
type InvalidTransactionKeyError struct {
	Key string
	Err error
}

// Error implements error.
func (e *InvalidTransactionKeyError) Error() string {
	return "INVALID_INPUT: embedded: quicbaas.Transaction/Execute: key " +
		strconv.Quote(e.Key) + " is not a decimal entity id: " + e.Err.Error()
}

// Unwrap returns the underlying strconv error.
func (e *InvalidTransactionKeyError) Unwrap() error { return e.Err }

// UnsupportedTransactionFieldError indicates a transaction compare target,
// compare result or operation type that has no counterpart on the other side
// of the transaction client to QuicBaaS service conversion.
type UnsupportedTransactionFieldError struct {
	Field string
	Value string
}

// Error implements error.
func (e *UnsupportedTransactionFieldError) Error() string {
	return "INVALID_INPUT: embedded: quicbaas.Transaction/Execute: " + e.Field + " " +
		strconv.Quote(e.Value) + " has no counterpart"
}

// UnsupportedVectorFilterError indicates a metadata filter operator the
// vector service cannot evaluate; it matches on equality only.
type UnsupportedVectorFilterError struct {
	Service  string
	Method   string
	Key      string
	Operator string
}

// Error implements the error interface.
func (e *UnsupportedVectorFilterError) Error() string {
	return "INVALID_INPUT: embedded: " + e.Service + "/" + e.Method + ": metadata filter on " +
		strconv.Quote(e.Key) + " uses unsupported operator " + strconv.Quote(e.Operator) + " (only \"eq\")"
}

// MixedVectorNamespaceError indicates a batch whose vectors span more than
// one namespace; the vector service batches per namespace.
type MixedVectorNamespaceError struct {
	Service string
	Method  string
	First   string
	Other   string
}

// Error implements the error interface.
func (e *MixedVectorNamespaceError) Error() string {
	return "INVALID_INPUT: embedded: " + e.Service + "/" + e.Method + ": batch mixes namespaces " +
		strconv.Quote(e.First) + " and " + strconv.Quote(e.Other)
}

// InvalidPageTokenError indicates a page token that is not a non-negative
// decimal offset.
type InvalidPageTokenError struct {
	Service string
	Method  string
	Value   string
}

// Error implements the error interface.
func (e *InvalidPageTokenError) Error() string {
	return "INVALID_INPUT: embedded: " + e.Service + "/" + e.Method + ": page token " +
		strconv.Quote(e.Value) + " is not a non-negative decimal offset"
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

// EmptyServiceResponseError indicates the service returned neither a
// response nor an error. Reporting it keeps a missing result from reaching
// the client as a zero-valued response that reads as a real answer.
type EmptyServiceResponseError struct {
	Service string
	Method  string
}

// Error implements the error interface.
func (e *EmptyServiceResponseError) Error() string {
	return "INTERNAL: embedded: service returned no response for: " + e.Service + "/" + e.Method
}

// NewEmptyServiceResponseError creates a new EmptyServiceResponseError.
func NewEmptyServiceResponseError(service, method string) *EmptyServiceResponseError {
	return &EmptyServiceResponseError{
		Service: service,
		Method:  method,
	}
}

// IncompleteStreamError indicates a client stream ended or began without a
// message the verb requires, such as an upload's metadata or its last chunk.
type IncompleteStreamError struct {
	Service string
	Method  string
	Reason  string
}

// Error implements the error interface.
func (e *IncompleteStreamError) Error() string {
	return "INVALID_ARGUMENT: embedded: incomplete stream for " + e.Service + "/" + e.Method + ": " + e.Reason
}

// NewIncompleteStreamError creates a new IncompleteStreamError.
func NewIncompleteStreamError(service, method, reason string) *IncompleteStreamError {
	return &IncompleteStreamError{
		Service: service,
		Method:  method,
		Reason:  reason,
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

// JWKEncodeError reports a signing key the OIDC provider publishes that could
// not be encoded as an RFC 7517 JWK for the SDK.
type JWKEncodeError struct {
	Cause error
}

// Error implements the error interface.
func (e *JWKEncodeError) Error() string {
	return "INTERNAL: embedded: quicbaas.Auth/GetJWKS: encode JWK: " + e.Cause.Error()
}

// Unwrap returns the encoding failure.
func (e *JWKEncodeError) Unwrap() error {
	return e.Cause
}

// NewJWKEncodeError creates a new JWKEncodeError.
func NewJWKEncodeError(cause error) *JWKEncodeError {
	return &JWKEncodeError{Cause: cause}
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
		policyField   *UnsupportedPolicyFieldError
		nested        *iam.NestedConditionError
		policyYAML    *iam.PolicyYAMLError
	)
	switch {
	case stderrors.As(err, &accessDenied):
		return errors.CodeForbidden, true
	case stderrors.As(err, &notFound), stderrors.As(err, &unresolved):
		return errors.CodeNotFound, true
	case stderrors.As(err, &alreadyExists):
		return errors.CodeAlreadyExists, true
	case stderrors.As(err, &invalid), stderrors.As(err, &roleInvalid),
		stderrors.As(err, &forge), stderrors.As(err, &strategy),
		stderrors.As(err, &policyField), stderrors.As(err, &nested),
		stderrors.As(err, &policyYAML):
		return errors.CodeInvalidInput, true
	}
	for _, class := range domainSentinelCodes {
		if stderrors.Is(err, class.sentinel) {
			return class.code, true
		}
	}
	return errors.CodeUnknown, false
}

// classifyServiceError is the embedded transport's error boundary: every unary
// and stream handler error passes through it, so an embedded caller reads the
// same SDK code a network caller does. An error already coded by the SDK
// passes through unchanged. A project or SDLC service error, a pkg/errors
// (go-qrdb) error, or one of this transport's own typed errors takes the code
// the network transports report for it. Anything else is CodeInternal. Every
// converted error keeps the original as its cause, so errors.Is and errors.As
// still reach it.
func classifyServiceError(svcName, method string, err error) error {
	if err == nil {
		return nil
	}
	var coded *errors.QBError
	if stderrors.As(err, &coded) {
		return err
	}
	if code, ok := domainErrorCode(err); ok {
		return errors.Wrap(err, code, "embedded."+svcName+"."+method, "service request failed")
	}
	var serviceErr *pkgerrors.QBError
	if stderrors.As(err, &serviceErr) {
		return errors.Wrap(err, serviceErrorCode(serviceErr), serviceErr.Op, serviceErrorMessage(serviceErr))
	}
	if code, ok := transportErrorCode(err); ok {
		return errors.Wrap(err, code, "embedded."+svcName+"."+method, err.Error())
	}
	return errors.Wrap(err, errors.CodeInternal, "embedded."+svcName+"."+method, "service request failed")
}

// serviceErrorCode returns the SDK code a gRPC caller reads for a pkg/errors
// error. It composes the server's pkg/errors-to-gRPC table with the client's
// gRPC-to-SDK table rather than keeping a third table that could drift.
func serviceErrorCode(err *pkgerrors.QBError) errors.ErrorCode {
	return grpctransport.StatusErrorCode(status.Convert(apigrpc.ToGRPCError(err)))
}

// serviceErrorMessage returns a pkg/errors error's message without its op and
// code, which the SDK error carries in its own fields.
func serviceErrorMessage(err *pkgerrors.QBError) string {
	if err.Err == nil {
		return err.Code.String()
	}
	return err.Err.Error()
}

// transportErrorCode classes the typed errors this transport's handlers
// return, with the code each one's message already names, and a context
// cancellation or deadline the way a network caller reads it.
func transportErrorCode(err error) (errors.ErrorCode, bool) {
	var (
		unknownMethod   *UnknownMethodError
		invalidRequest  *InvalidRequestTypeError
		invalidUserID   *InvalidUserIDError
		notConfigured   *ServiceNotConfiguredError
		streamNotServed *StreamNotSupportedError
		denied          *PermissionDeniedError
		incomplete      *IncompleteStreamError
	)
	switch {
	case stderrors.As(err, &invalidRequest), stderrors.As(err, &invalidUserID), stderrors.As(err, &incomplete):
		return errors.CodeInvalidInput, true
	case stderrors.As(err, &denied):
		return errors.CodeForbidden, true
	case stderrors.As(err, &notConfigured):
		return errors.CodeUnavailable, true
	case stderrors.As(err, &unknownMethod), stderrors.As(err, &streamNotServed):
		return errors.CodeUnimplemented, true
	case stderrors.Is(err, context.Canceled):
		return errors.CodeCanceled, true
	case stderrors.Is(err, context.DeadlineExceeded):
		return errors.CodeTimeout, true
	}
	return errors.CodeUnknown, false
}
