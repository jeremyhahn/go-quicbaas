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

package grpc

import (
	stderrors "errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Sentinel errors for gRPC client operations.
var (
	// ErrRequestNil indicates a nil request was provided.
	ErrRequestNil = stderrors.New("request cannot be nil")

	// ErrSealNotInitialized indicates the seal manager is not initialized.
	ErrSealNotInitialized = stderrors.New("not initialized")

	// ErrBarrierSealed indicates the barrier is sealed.
	ErrBarrierSealed = stderrors.New("barrier is sealed")

	// ErrPeerCredUnsupported indicates SO_PEERCRED is not supported on this platform.
	ErrPeerCredUnsupported = stderrors.New("SO_PEERCRED is only supported on Linux")
)

// FromGRPCError converts a gRPC status code to a QuicBaaS error code.
func FromGRPCError(err error) (errors.ErrorCode, bool) {
	s, ok := status.FromError(err)
	if !ok {
		return errors.ErrUnknown, false
	}

	code := grpcCodeToErrorCode(s.Code())
	return code, true
}

// ToGRPCError converts a QuicBaaS error to a gRPC status error.
func ToGRPCError(err error) error {
	if err == nil {
		return nil
	}

	// Check if it's already a gRPC status error
	if _, ok := status.FromError(err); ok {
		return err
	}

	// Extract QuicBaaS error code
	var errCode errors.ErrorCode
	var dbErr *errors.DragonError
	if stderrors.As(err, &dbErr) {
		errCode = dbErr.Code
	} else {
		errCode = errors.ErrInternal
	}

	grpcCode := errorCodeToGRPCCode(errCode)
	return status.Error(grpcCode, err.Error())
}

// convertError converts a gRPC status error to a DragonError.
// This is used by the client to convert gRPC errors back to application errors.
func convertError(err error) error {
	if err == nil {
		return nil
	}

	s, ok := status.FromError(err)
	if !ok {
		// Not a gRPC error, wrap it
		return &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "grpc",
			Err:  err,
		}
	}

	errCode := grpcCodeToErrorCode(s.Code())
	return &errors.DragonError{
		Code: errCode,
		Op:   "grpc",
		Err:  stderrors.New(s.Message()),
	}
}

// grpcCodeToErrorCode maps gRPC status codes to QuicBaaS error codes.
func grpcCodeToErrorCode(code codes.Code) errors.ErrorCode {
	switch code {
	case codes.OK:
		return errors.ErrUnknown // Should never happen for errors
	case codes.Canceled:
		return errors.ErrCanceled
	case codes.Unknown:
		return errors.ErrUnknown
	case codes.InvalidArgument:
		return errors.ErrInvalidInput
	case codes.DeadlineExceeded:
		return errors.ErrDeadlineExceeded
	case codes.NotFound:
		return errors.ErrNotFound
	case codes.AlreadyExists:
		return errors.ErrAlreadyExists
	case codes.PermissionDenied:
		return errors.ErrForbidden
	case codes.ResourceExhausted:
		return errors.ErrResourceExhausted
	case codes.FailedPrecondition:
		return errors.ErrPreconditionFailed
	case codes.Aborted:
		return errors.ErrAborted
	case codes.OutOfRange:
		return errors.ErrOutOfRange
	case codes.Unimplemented:
		return errors.ErrUnimplemented
	case codes.Internal:
		return errors.ErrInternal
	case codes.Unavailable:
		return errors.ErrUnavailable
	case codes.DataLoss:
		return errors.ErrDataLoss
	case codes.Unauthenticated:
		return errors.ErrUnauthorized
	default:
		return errors.ErrInternal
	}
}

// errorCodeToGRPCCode maps QuicBaaS error codes to gRPC status codes.
func errorCodeToGRPCCode(code errors.ErrorCode) codes.Code {
	switch code {
	case errors.ErrUnknown:
		return codes.Unknown
	case errors.ErrNotFound:
		return codes.NotFound
	case errors.ErrConflict:
		return codes.AlreadyExists
	case errors.ErrTimeout:
		return codes.DeadlineExceeded
	case errors.ErrInternal:
		return codes.Internal
	case errors.ErrInvalidInput:
		return codes.InvalidArgument
	case errors.ErrUnauthorized:
		return codes.Unauthenticated
	case errors.ErrForbidden:
		return codes.PermissionDenied
	case errors.ErrUnavailable:
		return codes.Unavailable
	case errors.ErrStorage:
		return codes.Internal
	case errors.ErrRaft:
		return codes.Internal
	case errors.ErrEncryption, errors.ErrDecryption:
		return codes.Internal
	case errors.ErrValidation:
		return codes.InvalidArgument
	case errors.ErrConfiguration:
		return codes.FailedPrecondition
	case errors.ErrCanceled:
		return codes.Canceled
	case errors.ErrDeadlineExceeded:
		return codes.DeadlineExceeded
	case errors.ErrAlreadyExists:
		return codes.AlreadyExists
	case errors.ErrResourceExhausted:
		return codes.ResourceExhausted
	case errors.ErrPreconditionFailed:
		return codes.FailedPrecondition
	case errors.ErrAborted:
		return codes.Aborted
	case errors.ErrOutOfRange:
		return codes.OutOfRange
	case errors.ErrUnimplemented:
		return codes.Unimplemented
	case errors.ErrDataLoss:
		return codes.DataLoss
	case errors.ErrSnapshot, errors.ErrReplication:
		return codes.Internal
	case errors.ErrLeaderNotAvailable, errors.ErrClusterNotReady:
		return codes.Unavailable
	default:
		return codes.Internal
	}
}
