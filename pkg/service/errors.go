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

// Package service provides error handling utilities for the QuicBaaS service facade.
// This package bridges the gap between internal errors and protocol-specific errors,
// enabling consistent error handling across gRPC, REST, and other API transports.
package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// Standard Service Error Types
// =============================================================================

// These are sentinel errors that can be used with errors.Is for error checking.
// They wrap the underlying QBError with contextual information.
var (
	// ErrNotFound indicates that the requested entity was not found.
	// Used when a Get/Query operation finds no matching entity.
	ErrNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "service",
		Err:  &api.SentinelError{Message: "entity not found"},
	}

	// ErrAlreadyExists indicates that an entity already exists.
	// Used when attempting to create an entity with a duplicate key.
	ErrAlreadyExists = &dberrors.QBError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "service",
		Err:  &api.SentinelError{Message: "entity already exists"},
	}

	// ErrInvalidInput indicates invalid request parameters.
	// Used when request validation fails due to missing or malformed data.
	ErrInvalidInput = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "service",
		Err:  &api.SentinelError{Message: "invalid input parameters"},
	}

	// ErrUnauthorized indicates authentication failed.
	// Used when the request lacks valid authentication credentials.
	ErrUnauthorized = &dberrors.QBError{
		Code: dberrors.ErrUnauthorized,
		Op:   "service",
		Err:  &api.SentinelError{Message: "authentication required"},
	}

	// ErrForbidden indicates authorization failed.
	// Used when the authenticated user lacks permission for the operation.
	ErrForbidden = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "service",
		Err:  &api.SentinelError{Message: "operation not permitted"},
	}

	// ErrTimeout indicates an operation timed out.
	// Used when an operation exceeds its deadline or timeout threshold.
	ErrTimeout = &dberrors.QBError{
		Code: dberrors.ErrTimeout,
		Op:   "service",
		Err:  &api.SentinelError{Message: "operation timed out"},
	}

	// ErrRaft indicates a Raft consensus error.
	// Used when Raft operations fail (proposal, leader election, etc.).
	ErrRaft = &dberrors.QBError{
		Code: dberrors.ErrRaft,
		Op:   "service",
		Err:  &api.SentinelError{Message: "raft consensus error"},
	}

	// ErrStorage indicates a storage layer error.
	// Used when the underlying storage engine encounters an error.
	ErrStorage = &dberrors.QBError{
		Code: dberrors.ErrStorage,
		Op:   "service",
		Err:  &api.SentinelError{Message: "storage layer error"},
	}

	// ErrInternal indicates an internal server error.
	// Used for unexpected errors that should not normally occur.
	ErrInternal = &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   "service",
		Err:  &api.SentinelError{Message: "internal server error"},
	}

	// ErrUnavailable indicates the service is unavailable.
	// Used when the service cannot handle requests (e.g., during shutdown).
	ErrUnavailable = &dberrors.QBError{
		Code: dberrors.ErrUnavailable,
		Op:   "service",
		Err:  &api.SentinelError{Message: "service unavailable"},
	}

	// ErrLeaderNotAvailable indicates no leader is available for a shard.
	// Used when a write operation cannot proceed due to no elected leader.
	ErrLeaderNotAvailable = &dberrors.QBError{
		Code: dberrors.ErrLeaderNotAvailable,
		Op:   "service",
		Err:  &api.SentinelError{Message: "no leader available for shard"},
	}

	// ErrConflict indicates a conflict with existing state.
	// Used for optimistic locking failures and version conflicts.
	ErrConflict = &dberrors.QBError{
		Code: dberrors.ErrConflict,
		Op:   "service",
		Err:  &api.SentinelError{Message: "conflict with existing state"},
	}

	// ErrPreconditionFailed indicates a precondition check failed.
	// Used in transactions when compare conditions fail.
	ErrPreconditionFailed = &dberrors.QBError{
		Code: dberrors.ErrPreconditionFailed,
		Op:   "service",
		Err:  &api.SentinelError{Message: "precondition check failed"},
	}

	// ErrResourceExhausted indicates resources are exhausted.
	// Used when rate limits are exceeded or system resources are full.
	ErrResourceExhausted = &dberrors.QBError{
		Code: dberrors.ErrResourceExhausted,
		Op:   "service",
		Err:  &api.SentinelError{Message: "resource exhausted"},
	}

	// ErrCanceled indicates the operation was canceled.
	// Used when a context is canceled before the operation completes.
	ErrCanceled = &dberrors.QBError{
		Code: dberrors.ErrCanceled,
		Op:   "service",
		Err:  &api.SentinelError{Message: "operation canceled"},
	}

	// ErrClusterNotReady indicates the cluster is not ready.
	// Used when cluster initialization is incomplete.
	ErrClusterNotReady = &dberrors.QBError{
		Code: dberrors.ErrClusterNotReady,
		Op:   "service",
		Err:  &api.SentinelError{Message: "cluster not ready"},
	}
)

// =============================================================================
// Error Code Dispatch Maps
// =============================================================================

// errorCodeToGRPCCodeMap provides O(1) lookup from QuicBaaS error codes to gRPC status codes.
// This is the authoritative mapping used throughout the service layer.
var errorCodeToGRPCCodeMap = map[dberrors.ErrorCode]codes.Code{
	dberrors.ErrUnknown:            codes.Unknown,
	dberrors.ErrNotFound:           codes.NotFound,
	dberrors.ErrConflict:           codes.AlreadyExists,
	dberrors.ErrAlreadyExists:      codes.AlreadyExists,
	dberrors.ErrTimeout:            codes.DeadlineExceeded,
	dberrors.ErrDeadlineExceeded:   codes.DeadlineExceeded,
	dberrors.ErrInternal:           codes.Internal,
	dberrors.ErrInvalidInput:       codes.InvalidArgument,
	dberrors.ErrValidation:         codes.InvalidArgument,
	dberrors.ErrUnauthorized:       codes.Unauthenticated,
	dberrors.ErrForbidden:          codes.PermissionDenied,
	dberrors.ErrNotAllowed:         codes.PermissionDenied,
	dberrors.ErrUnavailable:        codes.Unavailable,
	dberrors.ErrLeaderNotAvailable: codes.Unavailable,
	dberrors.ErrClusterNotReady:    codes.Unavailable,
	dberrors.ErrStorage:            codes.Internal,
	dberrors.ErrRaft:               codes.Internal,
	dberrors.ErrEncryption:         codes.Internal,
	dberrors.ErrDecryption:         codes.Internal,
	dberrors.ErrConfiguration:      codes.FailedPrecondition,
	dberrors.ErrCanceled:           codes.Canceled,
	dberrors.ErrResourceExhausted:  codes.ResourceExhausted,
	dberrors.ErrRateLimited:        codes.ResourceExhausted,
	dberrors.ErrPreconditionFailed: codes.FailedPrecondition,
	dberrors.ErrAborted:            codes.Aborted,
	dberrors.ErrOutOfRange:         codes.OutOfRange,
	dberrors.ErrUnimplemented:      codes.Unimplemented,
	dberrors.ErrNotImplemented:     codes.Unimplemented,
	dberrors.ErrDataLoss:           codes.DataLoss,
	dberrors.ErrSnapshot:           codes.Internal,
	dberrors.ErrReplication:        codes.Internal,
	dberrors.ErrQuorumLost:         codes.Unavailable,
	dberrors.ErrSplitBrain:         codes.Unavailable,
}

// grpcCodeToErrorCodeMap provides O(1) lookup from gRPC status codes to QuicBaaS error codes.
var grpcCodeToErrorCodeMap = map[codes.Code]dberrors.ErrorCode{
	codes.OK:                 dberrors.ErrUnknown,
	codes.Canceled:           dberrors.ErrCanceled,
	codes.Unknown:            dberrors.ErrUnknown,
	codes.InvalidArgument:    dberrors.ErrInvalidInput,
	codes.DeadlineExceeded:   dberrors.ErrDeadlineExceeded,
	codes.NotFound:           dberrors.ErrNotFound,
	codes.AlreadyExists:      dberrors.ErrAlreadyExists,
	codes.PermissionDenied:   dberrors.ErrForbidden,
	codes.ResourceExhausted:  dberrors.ErrResourceExhausted,
	codes.FailedPrecondition: dberrors.ErrPreconditionFailed,
	codes.Aborted:            dberrors.ErrAborted,
	codes.OutOfRange:         dberrors.ErrOutOfRange,
	codes.Unimplemented:      dberrors.ErrUnimplemented,
	codes.Internal:           dberrors.ErrInternal,
	codes.Unavailable:        dberrors.ErrUnavailable,
	codes.DataLoss:           dberrors.ErrDataLoss,
	codes.Unauthenticated:    dberrors.ErrUnauthorized,
}

// =============================================================================
// Error Wrapping Utilities
// =============================================================================

// WrapError wraps an error with a QBError, adding operation context and error code.
// This function is the primary way to add service-layer context to errors.
//
// Parameters:
//   - err: The underlying error to wrap
//   - code: The QuicBaaS error code
//   - op: The operation that failed (e.g., "service.Put")
//   - message: Additional context message
//
// Returns:
//   - A new QBError with the specified context
//
// Example:
//
//	err := storage.Get(key)
//	if err != nil {
//	    return WrapError(err, dberrors.ErrStorage, "service.Get", "failed to retrieve entity")
//	}
func WrapError(err error, code dberrors.ErrorCode, op string, message string) error {
	if err == nil {
		return nil
	}

	// If it's already a QBError, preserve the original error chain
	var dErr *dberrors.QBError
	if dberrors.As(err, &dErr) {
		return &dberrors.QBError{
			Code: code,
			Op:   op,
			Err:  err,
		}
	}

	// Wrap plain error with context
	wrappedErr := err
	if message != "" {
		wrappedErr = &api.WrappedMessageError{Message: message, Cause: err}
	}

	return &dberrors.QBError{
		Code: code,
		Op:   op,
		Err:  wrappedErr,
	}
}

// WrapErrorf wraps an error with formatted message using fmt.Sprintf semantics.
// This is a convenience wrapper around WrapError for formatted messages.
//
// Example:
//
//	return WrapErrorf(err, dberrors.ErrNotFound, "service.Get",
//	    "entity %s/%d not found", entityType, id)
func WrapErrorf(err error, code dberrors.ErrorCode, op string, format string, args ...interface{}) error {
	if err == nil {
		return nil
	}

	message := fmt.Sprintf(format, args...)
	return WrapError(err, code, op, message)
}

// NewError creates a new QBError with the specified code, operation, and message.
// Use this when creating errors without an underlying cause.
//
// Example:
//
//	return NewError(dberrors.ErrInvalidInput, "service.ValidatePutRequest",
//	    "entity type cannot be empty")
func NewError(code dberrors.ErrorCode, op string, message string) error {
	return &dberrors.QBError{
		Code: code,
		Op:   op,
		Err:  &api.MessageError{Message: message},
	}
}

// NewErrorf creates a new QBError with formatted message.
// This is a convenience wrapper around NewError for formatted messages.
//
// Example:
//
//	return NewErrorf(dberrors.ErrInvalidInput, "service.Put",
//	    "invalid entity type: %s", entityType)
func NewErrorf(code dberrors.ErrorCode, op string, format string, args ...interface{}) error {
	message := fmt.Sprintf(format, args...)
	return NewError(code, op, message)
}

// =============================================================================
// Error Type Checking Utilities
// =============================================================================

// IsNotFound returns true if the error is a NotFound error.
// This checks both service-layer and storage-layer not-found errors.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	return dberrors.IsNotFound(err)
}

// IsTimeout returns true if the error is a Timeout or DeadlineExceeded error.
// This includes context deadline exceeded and explicit timeout errors.
func IsTimeout(err error) bool {
	if err == nil {
		return false
	}

	var dErr *dberrors.QBError
	if dberrors.As(err, &dErr) {
		return dErr.Code == dberrors.ErrTimeout || dErr.Code == dberrors.ErrDeadlineExceeded
	}

	// Check for standard context timeout
	return errors.Is(err, context.DeadlineExceeded)
}

// IsUnavailable returns true if the error indicates service unavailability.
// This includes cluster not ready, no leader, and explicit unavailable errors.
func IsUnavailable(err error) bool {
	if err == nil {
		return false
	}

	var dErr *dberrors.QBError
	if dberrors.As(err, &dErr) {
		return dErr.Code == dberrors.ErrUnavailable ||
			dErr.Code == dberrors.ErrLeaderNotAvailable ||
			dErr.Code == dberrors.ErrClusterNotReady
	}

	return false
}

// IsUnauthorized returns true if the error is an authentication error.
func IsUnauthorized(err error) bool {
	if err == nil {
		return false
	}

	var dErr *dberrors.QBError
	if dberrors.As(err, &dErr) {
		return dErr.Code == dberrors.ErrUnauthorized
	}

	return false
}

// IsForbidden returns true if the error is an authorization error.
func IsForbidden(err error) bool {
	if err == nil {
		return false
	}

	var dErr *dberrors.QBError
	if dberrors.As(err, &dErr) {
		return dErr.Code == dberrors.ErrForbidden
	}

	return false
}

// IsConflict returns true if the error is a conflict error.
// This includes duplicate keys and version conflicts.
func IsConflict(err error) bool {
	if err == nil {
		return false
	}

	var dErr *dberrors.QBError
	if dberrors.As(err, &dErr) {
		return dErr.Code == dberrors.ErrConflict || dErr.Code == dberrors.ErrAlreadyExists
	}

	return false
}

// IsInvalidInput returns true if the error is an invalid input error.
// This includes validation errors and malformed requests.
func IsInvalidInput(err error) bool {
	if err == nil {
		return false
	}
	return dberrors.IsInvalidInput(err)
}

// IsCanceled returns true if the error is a cancellation error.
// This includes context cancellation and explicit cancel errors.
func IsCanceled(err error) bool {
	if err == nil {
		return false
	}

	if dberrors.IsCanceled(err) {
		return true
	}

	// Check for standard context cancellation
	return errors.Is(err, context.Canceled)
}

// IsRetryable returns true if the error is potentially retryable.
// This checks the error code's retryability flag.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	var dErr *dberrors.QBError
	if dberrors.As(err, &dErr) {
		return dErr.IsRetryable()
	}

	return false
}

// IsRaftError returns true if the error is a Raft consensus error.
func IsRaftError(err error) bool {
	if err == nil {
		return false
	}

	var dErr *dberrors.QBError
	if dberrors.As(err, &dErr) {
		return dErr.Code == dberrors.ErrRaft
	}

	return false
}

// IsStorageError returns true if the error is a storage layer error.
func IsStorageError(err error) bool {
	if err == nil {
		return false
	}

	var dErr *dberrors.QBError
	if dberrors.As(err, &dErr) {
		return dErr.Code == dberrors.ErrStorage
	}

	return false
}

// =============================================================================
// Protocol-Specific Error Mapping
// =============================================================================

// ToGRPCError converts a service error to a gRPC status error.
// This function maps QuicBaaS error codes to appropriate gRPC status codes
// while preserving error messages and metadata.
//
// Mapping:
//   - ErrNotFound -> codes.NotFound
//   - ErrInvalidInput -> codes.InvalidArgument
//   - ErrUnauthorized -> codes.Unauthenticated
//   - ErrForbidden -> codes.PermissionDenied
//   - ErrTimeout -> codes.DeadlineExceeded
//   - ErrUnavailable -> codes.Unavailable
//   - ErrConflict -> codes.AlreadyExists
//   - ErrPreconditionFailed -> codes.FailedPrecondition
//   - ErrResourceExhausted -> codes.ResourceExhausted
//   - ErrCanceled -> codes.Canceled
//   - Others -> codes.Internal
func ToGRPCError(err error) error {
	if err == nil {
		return nil
	}

	// Check if it's already a gRPC status error
	if _, ok := status.FromError(err); ok {
		return err
	}

	// Extract error code
	var errCode dberrors.ErrorCode
	var dErr *dberrors.QBError
	if dberrors.As(err, &dErr) {
		errCode = dErr.Code
	} else {
		errCode = dberrors.ErrInternal
	}

	// Map to gRPC code
	grpcCode := errorCodeToGRPCCode(errCode)
	return status.Error(grpcCode, err.Error())
}

// ToHTTPStatus converts a service error to an HTTP status code.
// This function maps QuicBaaS error codes to appropriate HTTP status codes.
//
// Mapping:
//   - ErrNotFound -> 404 Not Found
//   - ErrInvalidInput -> 400 Bad Request
//   - ErrUnauthorized -> 401 Unauthorized
//   - ErrForbidden -> 403 Forbidden
//   - ErrTimeout -> 408 Request Timeout
//   - ErrConflict -> 409 Conflict
//   - ErrPreconditionFailed -> 412 Precondition Failed
//   - ErrResourceExhausted -> 429 Too Many Requests
//   - ErrUnavailable -> 503 Service Unavailable
//   - Others -> 500 Internal Server Error
func ToHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}

	var dErr *dberrors.QBError
	if dberrors.As(err, &dErr) {
		return dErr.Code.HTTPStatus()
	}

	return http.StatusInternalServerError
}

// errorCodeToGRPCCode maps a QuicBaaS error code to its corresponding gRPC status code
// using O(1) map-based dispatch. Unknown codes default to codes.Internal.
func errorCodeToGRPCCode(code dberrors.ErrorCode) codes.Code {
	if grpcCode, ok := errorCodeToGRPCCodeMap[code]; ok {
		return grpcCode
	}
	return codes.Internal
}

// FromGRPCError converts a gRPC status error to a QuicBaaS error.
// This function is useful when receiving errors from downstream gRPC services.
func FromGRPCError(err error) error {
	if err == nil {
		return nil
	}

	// Check if it's a gRPC status error
	st, ok := status.FromError(err)
	if !ok {
		// Not a gRPC error, wrap it
		return WrapError(err, dberrors.ErrInternal, "service", "unexpected error type")
	}

	// Map gRPC code to QuicBaaS error code
	code := StatusErrorCode(st)

	return &dberrors.QBError{
		Code: code,
		Op:   "service",
		Err:  &api.GRPCMessageError{Message: st.Message()},
	}
}

// grpcCodeToErrorCode maps a gRPC status code to its corresponding QuicBaaS error code
// using O(1) map-based dispatch. Unknown codes default to dberrors.ErrInternal.
func grpcCodeToErrorCode(code codes.Code) dberrors.ErrorCode {
	if errCode, ok := grpcCodeToErrorCodeMap[code]; ok {
		return errCode
	}
	return dberrors.ErrInternal
}

// StatusErrorCode maps a gRPC status to its QuicBaaS error code. The server
// sends both rate_limited and resource_exhausted as ResourceExhausted and names
// the code in the status message, which reads "op: code: message" (or
// "code: message" without an op), so a throttled call reads back as
// rate_limited.
func StatusErrorCode(st *status.Status) dberrors.ErrorCode {
	code := grpcCodeToErrorCode(st.Code())
	if code != dberrors.ErrResourceExhausted {
		return code
	}
	parts := strings.SplitN(st.Message(), ": ", 3)
	for i := 0; i < len(parts) && i < 2; i++ {
		if parts[i] == dberrors.ErrRateLimited.String() {
			return dberrors.ErrRateLimited
		}
	}
	return code
}

// =============================================================================
// Error Context Helpers
// =============================================================================

// WithEntityContext adds entity context to an error.
// This includes entity type, ID, and operation.
func WithEntityContext(err error, entityType string, id uint64, operation string) error {
	if err == nil {
		return nil
	}

	var dErr *dberrors.QBError
	if dberrors.As(err, &dErr) {
		return dErr.
			WithContext("entity_type", entityType).
			WithContext("entity_id", id).
			WithContext("operation", operation)
	}

	// Not a QBError, create one
	return &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   operation,
		Err:  err,
		Context: map[string]interface{}{
			"entity_type": entityType,
			"entity_id":   id,
		},
	}
}

// WithShardContext adds shard context to an error.
// This includes shard ID and node information.
func WithShardContext(err error, shardID uint64, nodeID uint64) error {
	if err == nil {
		return nil
	}

	var dErr *dberrors.QBError
	if dberrors.As(err, &dErr) {
		return dErr.
			WithContext("shard_id", shardID).
			WithContext("node_id", nodeID)
	}

	// Not a QBError, create one
	return &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   "service",
		Err:  err,
		Context: map[string]interface{}{
			"shard_id": shardID,
			"node_id":  nodeID,
		},
	}
}

// WithTimeoutContext adds timeout context to an error.
// This includes the timeout duration and elapsed time.
func WithTimeoutContext(err error, timeoutMs uint32, elapsedMs int64) error {
	if err == nil {
		return nil
	}

	var dErr *dberrors.QBError
	if dberrors.As(err, &dErr) {
		return dErr.
			WithContext("timeout_ms", timeoutMs).
			WithContext("elapsed_ms", elapsedMs)
	}

	// Not a QBError, create one
	return &dberrors.QBError{
		Code: dberrors.ErrTimeout,
		Op:   "service",
		Err:  err,
		Context: map[string]interface{}{
			"timeout_ms": timeoutMs,
			"elapsed_ms": elapsedMs,
		},
	}
}
