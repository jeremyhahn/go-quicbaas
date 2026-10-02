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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestFromGRPCError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		expectedCode dberrors.ErrorCode
		expectedOK   bool
	}{
		{
			name:         "not found",
			err:          status.Error(codes.NotFound, "not found"),
			expectedCode: dberrors.ErrNotFound,
			expectedOK:   true,
		},
		{
			name:         "canceled",
			err:          status.Error(codes.Canceled, "canceled"),
			expectedCode: dberrors.ErrCanceled,
			expectedOK:   true,
		},
		{
			name:         "unknown",
			err:          status.Error(codes.Unknown, "unknown"),
			expectedCode: dberrors.ErrUnknown,
			expectedOK:   true,
		},
		{
			name:         "invalid argument",
			err:          status.Error(codes.InvalidArgument, "invalid"),
			expectedCode: dberrors.ErrInvalidInput,
			expectedOK:   true,
		},
		{
			name:         "deadline exceeded",
			err:          status.Error(codes.DeadlineExceeded, "timeout"),
			expectedCode: dberrors.ErrDeadlineExceeded,
			expectedOK:   true,
		},
		{
			name:         "already exists",
			err:          status.Error(codes.AlreadyExists, "exists"),
			expectedCode: dberrors.ErrAlreadyExists,
			expectedOK:   true,
		},
		{
			name:         "permission denied",
			err:          status.Error(codes.PermissionDenied, "forbidden"),
			expectedCode: dberrors.ErrForbidden,
			expectedOK:   true,
		},
		{
			name:         "resource exhausted",
			err:          status.Error(codes.ResourceExhausted, "exhausted"),
			expectedCode: dberrors.ErrResourceExhausted,
			expectedOK:   true,
		},
		{
			name:         "failed precondition",
			err:          status.Error(codes.FailedPrecondition, "precondition"),
			expectedCode: dberrors.ErrPreconditionFailed,
			expectedOK:   true,
		},
		{
			name:         "aborted",
			err:          status.Error(codes.Aborted, "aborted"),
			expectedCode: dberrors.ErrAborted,
			expectedOK:   true,
		},
		{
			name:         "out of range",
			err:          status.Error(codes.OutOfRange, "out of range"),
			expectedCode: dberrors.ErrOutOfRange,
			expectedOK:   true,
		},
		{
			name:         "unimplemented",
			err:          status.Error(codes.Unimplemented, "not implemented"),
			expectedCode: dberrors.ErrUnimplemented,
			expectedOK:   true,
		},
		{
			name:         "internal",
			err:          status.Error(codes.Internal, "internal error"),
			expectedCode: dberrors.ErrInternal,
			expectedOK:   true,
		},
		{
			name:         "unavailable",
			err:          status.Error(codes.Unavailable, "unavailable"),
			expectedCode: dberrors.ErrUnavailable,
			expectedOK:   true,
		},
		{
			name:         "data loss",
			err:          status.Error(codes.DataLoss, "data loss"),
			expectedCode: dberrors.ErrDataLoss,
			expectedOK:   true,
		},
		{
			name:         "unauthenticated",
			err:          status.Error(codes.Unauthenticated, "unauthorized"),
			expectedCode: dberrors.ErrUnauthorized,
			expectedOK:   true,
		},
		{
			name:         "non-grpc error",
			err:          errors.New("generic error"),
			expectedCode: dberrors.ErrUnknown,
			expectedOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, ok := FromGRPCError(tt.err)
			assert.Equal(t, tt.expectedOK, ok)
			if tt.expectedOK {
				assert.Equal(t, tt.expectedCode, code)
			}
		})
	}
}

func TestToGRPCError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		expectedCode codes.Code
	}{
		{
			name:         "nil error",
			err:          nil,
			expectedCode: codes.OK,
		},
		{
			name:         "grpc error passthrough",
			err:          status.Error(codes.NotFound, "not found"),
			expectedCode: codes.NotFound,
		},
		{
			name: "qb not found error",
			err: &dberrors.QBError{
				Code: dberrors.ErrNotFound,
				Op:   "test",
				Err:  errors.New("not found"),
			},
			expectedCode: codes.NotFound,
		},
		{
			name: "qb conflict error",
			err: &dberrors.QBError{
				Code: dberrors.ErrConflict,
				Op:   "test",
				Err:  errors.New("conflict"),
			},
			expectedCode: codes.AlreadyExists,
		},
		{
			name: "qb timeout error",
			err: &dberrors.QBError{
				Code: dberrors.ErrTimeout,
				Op:   "test",
				Err:  errors.New("timeout"),
			},
			expectedCode: codes.DeadlineExceeded,
		},
		{
			name: "qb invalid input error",
			err: &dberrors.QBError{
				Code: dberrors.ErrInvalidInput,
				Op:   "test",
				Err:  errors.New("invalid"),
			},
			expectedCode: codes.InvalidArgument,
		},
		{
			name: "qb unauthorized error",
			err: &dberrors.QBError{
				Code: dberrors.ErrUnauthorized,
				Op:   "test",
				Err:  errors.New("unauthorized"),
			},
			expectedCode: codes.Unauthenticated,
		},
		{
			name: "qb forbidden error",
			err: &dberrors.QBError{
				Code: dberrors.ErrForbidden,
				Op:   "test",
				Err:  errors.New("forbidden"),
			},
			expectedCode: codes.PermissionDenied,
		},
		{
			name: "qb unavailable error",
			err: &dberrors.QBError{
				Code: dberrors.ErrUnavailable,
				Op:   "test",
				Err:  errors.New("unavailable"),
			},
			expectedCode: codes.Unavailable,
		},
		{
			name:         "unknown error type",
			err:          errors.New("generic error"),
			expectedCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ToGRPCError(tt.err)

			if tt.err == nil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				st, ok := status.FromError(result)
				assert.True(t, ok)
				assert.Equal(t, tt.expectedCode, st.Code())
			}
		})
	}
}

func TestGRPCCodeToErrorCode(t *testing.T) {
	tests := []struct {
		name         string
		grpcCode     codes.Code
		expectedCode dberrors.ErrorCode
	}{
		{
			name:         "OK should map to unknown",
			grpcCode:     codes.OK,
			expectedCode: dberrors.ErrUnknown,
		},
		{
			name:         "Canceled",
			grpcCode:     codes.Canceled,
			expectedCode: dberrors.ErrCanceled,
		},
		{
			name:         "Unknown",
			grpcCode:     codes.Unknown,
			expectedCode: dberrors.ErrUnknown,
		},
		{
			name:         "InvalidArgument",
			grpcCode:     codes.InvalidArgument,
			expectedCode: dberrors.ErrInvalidInput,
		},
		{
			name:         "DeadlineExceeded",
			grpcCode:     codes.DeadlineExceeded,
			expectedCode: dberrors.ErrDeadlineExceeded,
		},
		{
			name:         "NotFound",
			grpcCode:     codes.NotFound,
			expectedCode: dberrors.ErrNotFound,
		},
		{
			name:         "AlreadyExists",
			grpcCode:     codes.AlreadyExists,
			expectedCode: dberrors.ErrAlreadyExists,
		},
		{
			name:         "PermissionDenied",
			grpcCode:     codes.PermissionDenied,
			expectedCode: dberrors.ErrForbidden,
		},
		{
			name:         "ResourceExhausted",
			grpcCode:     codes.ResourceExhausted,
			expectedCode: dberrors.ErrResourceExhausted,
		},
		{
			name:         "FailedPrecondition",
			grpcCode:     codes.FailedPrecondition,
			expectedCode: dberrors.ErrPreconditionFailed,
		},
		{
			name:         "Aborted",
			grpcCode:     codes.Aborted,
			expectedCode: dberrors.ErrAborted,
		},
		{
			name:         "OutOfRange",
			grpcCode:     codes.OutOfRange,
			expectedCode: dberrors.ErrOutOfRange,
		},
		{
			name:         "Unimplemented",
			grpcCode:     codes.Unimplemented,
			expectedCode: dberrors.ErrUnimplemented,
		},
		{
			name:         "Internal",
			grpcCode:     codes.Internal,
			expectedCode: dberrors.ErrInternal,
		},
		{
			name:         "Unavailable",
			grpcCode:     codes.Unavailable,
			expectedCode: dberrors.ErrUnavailable,
		},
		{
			name:         "DataLoss",
			grpcCode:     codes.DataLoss,
			expectedCode: dberrors.ErrDataLoss,
		},
		{
			name:         "Unauthenticated",
			grpcCode:     codes.Unauthenticated,
			expectedCode: dberrors.ErrUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := grpcCodeToErrorCode(tt.grpcCode)
			assert.Equal(t, tt.expectedCode, result)
		})
	}
}

func TestErrorCodeToGRPCCode(t *testing.T) {
	tests := []struct {
		name         string
		errorCode    dberrors.ErrorCode
		expectedCode codes.Code
	}{
		{
			name:         "ErrUnknown",
			errorCode:    dberrors.ErrUnknown,
			expectedCode: codes.Unknown,
		},
		{
			name:         "ErrNotFound",
			errorCode:    dberrors.ErrNotFound,
			expectedCode: codes.NotFound,
		},
		{
			name:         "ErrConflict",
			errorCode:    dberrors.ErrConflict,
			expectedCode: codes.AlreadyExists,
		},
		{
			name:         "ErrTimeout",
			errorCode:    dberrors.ErrTimeout,
			expectedCode: codes.DeadlineExceeded,
		},
		{
			name:         "ErrInternal",
			errorCode:    dberrors.ErrInternal,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrInvalidInput",
			errorCode:    dberrors.ErrInvalidInput,
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "ErrUnauthorized",
			errorCode:    dberrors.ErrUnauthorized,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "ErrForbidden",
			errorCode:    dberrors.ErrForbidden,
			expectedCode: codes.PermissionDenied,
		},
		{
			name:         "ErrUnavailable",
			errorCode:    dberrors.ErrUnavailable,
			expectedCode: codes.Unavailable,
		},
		{
			name:         "ErrStorage",
			errorCode:    dberrors.ErrStorage,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrRaft",
			errorCode:    dberrors.ErrRaft,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrEncryption",
			errorCode:    dberrors.ErrEncryption,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrDecryption",
			errorCode:    dberrors.ErrDecryption,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrValidation",
			errorCode:    dberrors.ErrValidation,
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "ErrConfiguration",
			errorCode:    dberrors.ErrConfiguration,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "ErrCanceled",
			errorCode:    dberrors.ErrCanceled,
			expectedCode: codes.Canceled,
		},
		{
			name:         "ErrDeadlineExceeded",
			errorCode:    dberrors.ErrDeadlineExceeded,
			expectedCode: codes.DeadlineExceeded,
		},
		{
			name:         "ErrAlreadyExists",
			errorCode:    dberrors.ErrAlreadyExists,
			expectedCode: codes.AlreadyExists,
		},
		{
			name:         "ErrResourceExhausted",
			errorCode:    dberrors.ErrResourceExhausted,
			expectedCode: codes.ResourceExhausted,
		},
		{
			name:         "ErrPreconditionFailed",
			errorCode:    dberrors.ErrPreconditionFailed,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "ErrAborted",
			errorCode:    dberrors.ErrAborted,
			expectedCode: codes.Aborted,
		},
		{
			name:         "ErrOutOfRange",
			errorCode:    dberrors.ErrOutOfRange,
			expectedCode: codes.OutOfRange,
		},
		{
			name:         "ErrUnimplemented",
			errorCode:    dberrors.ErrUnimplemented,
			expectedCode: codes.Unimplemented,
		},
		{
			name:         "ErrDataLoss",
			errorCode:    dberrors.ErrDataLoss,
			expectedCode: codes.DataLoss,
		},
		{
			name:         "ErrSnapshot",
			errorCode:    dberrors.ErrSnapshot,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrReplication",
			errorCode:    dberrors.ErrReplication,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrLeaderNotAvailable",
			errorCode:    dberrors.ErrLeaderNotAvailable,
			expectedCode: codes.Unavailable,
		},
		{
			name:         "ErrClusterNotReady",
			errorCode:    dberrors.ErrClusterNotReady,
			expectedCode: codes.Unavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := errorCodeToGRPCCode(tt.errorCode)
			assert.Equal(t, tt.expectedCode, result)
		})
	}
}

// Test roundtrip conversion.
func TestErrorConversionRoundtrip(t *testing.T) {
	tests := []struct {
		name      string
		errorCode dberrors.ErrorCode
	}{
		{"NotFound", dberrors.ErrNotFound},
		{"InvalidInput", dberrors.ErrInvalidInput},
		{"Unauthorized", dberrors.ErrUnauthorized},
		{"Unavailable", dberrors.ErrUnavailable},
		{"DeadlineExceeded", dberrors.ErrDeadlineExceeded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert to gRPC code
			grpcCode := errorCodeToGRPCCode(tt.errorCode)

			// Convert back
			resultCode := grpcCodeToErrorCode(grpcCode)

			// Should match original
			assert.Equal(t, tt.errorCode, resultCode)
		})
	}
}

// Benchmarks.
func BenchmarkFromGRPCError(b *testing.B) {
	err := status.Error(codes.NotFound, "not found")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = FromGRPCError(err)
	}
}

func BenchmarkToGRPCError(b *testing.B) {
	err := &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "test",
		Err:  errors.New("not found"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ToGRPCError(err)
	}
}

func BenchmarkGRPCCodeToErrorCode(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = grpcCodeToErrorCode(codes.NotFound)
	}
}

func BenchmarkErrorCodeToGRPCCode(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = errorCodeToGRPCCode(dberrors.ErrNotFound)
	}
}

// --- Merged from errors_exhaustive_test.go ---

// TestGrpcCodeToErrorCode_AllGRPCCodes exhaustively tests grpcCodeToErrorCode with ALL gRPC status codes.
func TestGrpcCodeToErrorCode_AllGRPCCodes(t *testing.T) {
	tests := []struct {
		name         string
		grpcCode     codes.Code
		expectedCode dberrors.ErrorCode
	}{
		// Explicitly mapped codes
		{
			name:         "OK maps to ErrUnknown",
			grpcCode:     codes.OK,
			expectedCode: dberrors.ErrUnknown,
		},
		{
			name:         "Canceled maps to ErrCanceled",
			grpcCode:     codes.Canceled,
			expectedCode: dberrors.ErrCanceled,
		},
		{
			name:         "Unknown maps to ErrUnknown",
			grpcCode:     codes.Unknown,
			expectedCode: dberrors.ErrUnknown,
		},
		{
			name:         "InvalidArgument maps to ErrInvalidInput",
			grpcCode:     codes.InvalidArgument,
			expectedCode: dberrors.ErrInvalidInput,
		},
		{
			name:         "DeadlineExceeded maps to ErrDeadlineExceeded",
			grpcCode:     codes.DeadlineExceeded,
			expectedCode: dberrors.ErrDeadlineExceeded,
		},
		{
			name:         "NotFound maps to ErrNotFound",
			grpcCode:     codes.NotFound,
			expectedCode: dberrors.ErrNotFound,
		},
		{
			name:         "AlreadyExists maps to ErrAlreadyExists",
			grpcCode:     codes.AlreadyExists,
			expectedCode: dberrors.ErrAlreadyExists,
		},
		{
			name:         "PermissionDenied maps to ErrForbidden",
			grpcCode:     codes.PermissionDenied,
			expectedCode: dberrors.ErrForbidden,
		},
		{
			name:         "ResourceExhausted maps to ErrResourceExhausted",
			grpcCode:     codes.ResourceExhausted,
			expectedCode: dberrors.ErrResourceExhausted,
		},
		{
			name:         "FailedPrecondition maps to ErrPreconditionFailed",
			grpcCode:     codes.FailedPrecondition,
			expectedCode: dberrors.ErrPreconditionFailed,
		},
		{
			name:         "Aborted maps to ErrAborted",
			grpcCode:     codes.Aborted,
			expectedCode: dberrors.ErrAborted,
		},
		{
			name:         "OutOfRange maps to ErrOutOfRange",
			grpcCode:     codes.OutOfRange,
			expectedCode: dberrors.ErrOutOfRange,
		},
		{
			name:         "Unimplemented maps to ErrUnimplemented",
			grpcCode:     codes.Unimplemented,
			expectedCode: dberrors.ErrUnimplemented,
		},
		{
			name:         "Internal maps to ErrInternal",
			grpcCode:     codes.Internal,
			expectedCode: dberrors.ErrInternal,
		},
		{
			name:         "Unavailable maps to ErrUnavailable",
			grpcCode:     codes.Unavailable,
			expectedCode: dberrors.ErrUnavailable,
		},
		{
			name:         "DataLoss maps to ErrDataLoss",
			grpcCode:     codes.DataLoss,
			expectedCode: dberrors.ErrDataLoss,
		},
		{
			name:         "Unauthenticated maps to ErrUnauthorized",
			grpcCode:     codes.Unauthenticated,
			expectedCode: dberrors.ErrUnauthorized,
		},
		// Test default case with an invalid/unknown code
		{
			name:         "Unknown code 999 maps to ErrInternal (default)",
			grpcCode:     codes.Code(999),
			expectedCode: dberrors.ErrInternal,
		},
		{
			name:         "Unknown code 100 maps to ErrInternal (default)",
			grpcCode:     codes.Code(100),
			expectedCode: dberrors.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := grpcCodeToErrorCode(tt.grpcCode)
			assert.Equal(t, tt.expectedCode, result)
		})
	}
}

// TestErrorCodeToGRPCCode_AllErrorCodes exhaustively tests errorCodeToGRPCCode with ALL QuicBaaS error codes.
func TestErrorCodeToGRPCCode_AllErrorCodes(t *testing.T) {
	tests := []struct {
		name         string
		errorCode    dberrors.ErrorCode
		expectedCode codes.Code
	}{
		// Explicitly mapped error codes
		{
			name:         "ErrUnknown maps to codes.Unknown",
			errorCode:    dberrors.ErrUnknown,
			expectedCode: codes.Unknown,
		},
		{
			name:         "ErrNotFound maps to codes.NotFound",
			errorCode:    dberrors.ErrNotFound,
			expectedCode: codes.NotFound,
		},
		{
			name:         "ErrConflict maps to codes.AlreadyExists",
			errorCode:    dberrors.ErrConflict,
			expectedCode: codes.AlreadyExists,
		},
		{
			name:         "ErrTimeout maps to codes.DeadlineExceeded",
			errorCode:    dberrors.ErrTimeout,
			expectedCode: codes.DeadlineExceeded,
		},
		{
			name:         "ErrInternal maps to codes.Internal",
			errorCode:    dberrors.ErrInternal,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrInvalidInput maps to codes.InvalidArgument",
			errorCode:    dberrors.ErrInvalidInput,
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "ErrUnauthorized maps to codes.Unauthenticated",
			errorCode:    dberrors.ErrUnauthorized,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "ErrForbidden maps to codes.PermissionDenied",
			errorCode:    dberrors.ErrForbidden,
			expectedCode: codes.PermissionDenied,
		},
		{
			name:         "ErrUnavailable maps to codes.Unavailable",
			errorCode:    dberrors.ErrUnavailable,
			expectedCode: codes.Unavailable,
		},
		{
			name:         "ErrStorage maps to codes.Internal",
			errorCode:    dberrors.ErrStorage,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrRaft maps to codes.Internal",
			errorCode:    dberrors.ErrRaft,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrEncryption maps to codes.Internal",
			errorCode:    dberrors.ErrEncryption,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrDecryption maps to codes.Internal",
			errorCode:    dberrors.ErrDecryption,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrValidation maps to codes.InvalidArgument",
			errorCode:    dberrors.ErrValidation,
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "ErrConfiguration maps to codes.FailedPrecondition",
			errorCode:    dberrors.ErrConfiguration,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "ErrCanceled maps to codes.Canceled",
			errorCode:    dberrors.ErrCanceled,
			expectedCode: codes.Canceled,
		},
		{
			name:         "ErrDeadlineExceeded maps to codes.DeadlineExceeded",
			errorCode:    dberrors.ErrDeadlineExceeded,
			expectedCode: codes.DeadlineExceeded,
		},
		{
			name:         "ErrAlreadyExists maps to codes.AlreadyExists",
			errorCode:    dberrors.ErrAlreadyExists,
			expectedCode: codes.AlreadyExists,
		},
		{
			name:         "ErrResourceExhausted maps to codes.ResourceExhausted",
			errorCode:    dberrors.ErrResourceExhausted,
			expectedCode: codes.ResourceExhausted,
		},
		{
			name:         "ErrPreconditionFailed maps to codes.FailedPrecondition",
			errorCode:    dberrors.ErrPreconditionFailed,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "ErrAborted maps to codes.Aborted",
			errorCode:    dberrors.ErrAborted,
			expectedCode: codes.Aborted,
		},
		{
			name:         "ErrOutOfRange maps to codes.OutOfRange",
			errorCode:    dberrors.ErrOutOfRange,
			expectedCode: codes.OutOfRange,
		},
		{
			name:         "ErrUnimplemented maps to codes.Unimplemented",
			errorCode:    dberrors.ErrUnimplemented,
			expectedCode: codes.Unimplemented,
		},
		{
			name:         "ErrDataLoss maps to codes.DataLoss",
			errorCode:    dberrors.ErrDataLoss,
			expectedCode: codes.DataLoss,
		},
		{
			name:         "ErrSnapshot maps to codes.Internal",
			errorCode:    dberrors.ErrSnapshot,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrReplication maps to codes.Internal",
			errorCode:    dberrors.ErrReplication,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrLeaderNotAvailable maps to codes.Unavailable",
			errorCode:    dberrors.ErrLeaderNotAvailable,
			expectedCode: codes.Unavailable,
		},
		{
			name:         "ErrClusterNotReady maps to codes.Unavailable",
			errorCode:    dberrors.ErrClusterNotReady,
			expectedCode: codes.Unavailable,
		},
		// Test error codes not explicitly mapped (should fall through to default)
		{
			name:         "ErrQuorumLost maps to codes.Internal (default)",
			errorCode:    dberrors.ErrQuorumLost,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrSplitBrain maps to codes.Internal (default)",
			errorCode:    dberrors.ErrSplitBrain,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrRateLimited maps to codes.Internal (default)",
			errorCode:    dberrors.ErrRateLimited,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrNotImplemented maps to codes.Internal (default)",
			errorCode:    dberrors.ErrNotImplemented,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrNotAllowed maps to codes.Internal (default)",
			errorCode:    dberrors.ErrNotAllowed,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrSystemKeyspaceViolation maps to codes.Internal (default)",
			errorCode:    dberrors.ErrSystemKeyspaceViolation,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrCrossTenantAccess maps to codes.PermissionDenied",
			errorCode:    dberrors.ErrCrossTenantAccess,
			expectedCode: codes.PermissionDenied,
		},
		{
			name:         "ErrMissingTenantContext maps to codes.Internal (default)",
			errorCode:    dberrors.ErrMissingTenantContext,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrMissingJustification maps to codes.Internal (default)",
			errorCode:    dberrors.ErrMissingJustification,
			expectedCode: codes.Internal,
		},
		{
			name:         "ErrMissingUserContext maps to codes.Internal (default)",
			errorCode:    dberrors.ErrMissingUserContext,
			expectedCode: codes.Internal,
		},
		// Test arbitrary unknown error code
		{
			name:         "Unknown ErrorCode 9999 maps to codes.Internal (default)",
			errorCode:    dberrors.ErrorCode(9999),
			expectedCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := errorCodeToGRPCCode(tt.errorCode)
			assert.Equal(t, tt.expectedCode, result)
		})
	}
}

// TestFromGRPCError_NonStatusErrorsAndNil tests FromGRPCError with non-gRPC errors and nil.
func TestFromGRPCError_NonStatusErrorsAndNil(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		expectedCode dberrors.ErrorCode
		expectedOK   bool
	}{
		{
			name:         "nil error returns ErrUnknown and true",
			err:          nil,
			expectedCode: dberrors.ErrUnknown,
			expectedOK:   true, // status.FromError(nil) returns (status with code OK, true)
		},
		{
			name:         "plain error returns ErrUnknown and false",
			err:          errors.New("plain error"),
			expectedCode: dberrors.ErrUnknown,
			expectedOK:   false,
		},
		{
			name:         "wrapped plain error returns ErrUnknown and false",
			err:          errors.New("wrapped: " + "inner error"),
			expectedCode: dberrors.ErrUnknown,
			expectedOK:   false,
		},
		{
			name:         "QBError without gRPC status returns ErrUnknown and false",
			err:          &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test", Err: errors.New("not found")},
			expectedCode: dberrors.ErrUnknown,
			expectedOK:   false,
		},
		{
			name:         "gRPC status error returns proper code and true",
			err:          status.Error(codes.NotFound, "not found"),
			expectedCode: dberrors.ErrNotFound,
			expectedOK:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, ok := FromGRPCError(tt.err)
			assert.Equal(t, tt.expectedOK, ok, "ok mismatch")
			assert.Equal(t, tt.expectedCode, code, "code mismatch")
		})
	}
}

// TestToGRPCError_NilInput tests ToGRPCError specifically with nil input.
func TestToGRPCError_NilInput(t *testing.T) {
	result := ToGRPCError(nil)
	assert.Nil(t, result, "ToGRPCError(nil) should return nil")
}

// TestToGRPCError_VariousErrorTypes tests ToGRPCError with various error types.
func TestToGRPCError_VariousErrorTypes(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		expectedCode codes.Code
		checkMessage bool
		message      string
	}{
		{
			name:         "gRPC status error passes through unchanged",
			err:          status.Error(codes.NotFound, "resource not found"),
			expectedCode: codes.NotFound,
			checkMessage: true,
			message:      "resource not found",
		},
		{
			name: "QBError with ErrNotFound",
			err: &dberrors.QBError{
				Code: dberrors.ErrNotFound,
				Op:   "storage.Get",
				Err:  errors.New("key not found"),
			},
			expectedCode: codes.NotFound,
			checkMessage: true,
			message:      "storage.Get: not_found: key not found",
		},
		{
			name: "QBError with ErrInternal",
			err: &dberrors.QBError{
				Code: dberrors.ErrInternal,
				Op:   "storage.Write",
				Err:  errors.New("disk full"),
			},
			expectedCode: codes.Internal,
		},
		{
			name: "QBError with ErrUnavailable",
			err: &dberrors.QBError{
				Code: dberrors.ErrUnavailable,
				Op:   "raft.Propose",
				Err:  errors.New("no leader"),
			},
			expectedCode: codes.Unavailable,
		},
		{
			name: "QBError with ErrStorage",
			err: &dberrors.QBError{
				Code: dberrors.ErrStorage,
				Op:   "pebble.Write",
				Err:  errors.New("write failed"),
			},
			expectedCode: codes.Internal,
		},
		{
			name: "QBError with ErrRaft",
			err: &dberrors.QBError{
				Code: dberrors.ErrRaft,
				Op:   "raft.Apply",
				Err:  errors.New("consensus failed"),
			},
			expectedCode: codes.Internal,
		},
		{
			name: "QBError with ErrEncryption",
			err: &dberrors.QBError{
				Code: dberrors.ErrEncryption,
				Op:   "crypto.Encrypt",
				Err:  errors.New("encryption failed"),
			},
			expectedCode: codes.Internal,
		},
		{
			name: "QBError with ErrDecryption",
			err: &dberrors.QBError{
				Code: dberrors.ErrDecryption,
				Op:   "crypto.Decrypt",
				Err:  errors.New("decryption failed"),
			},
			expectedCode: codes.Internal,
		},
		{
			name: "QBError with ErrValidation",
			err: &dberrors.QBError{
				Code: dberrors.ErrValidation,
				Op:   "api.Validate",
				Err:  errors.New("invalid field"),
			},
			expectedCode: codes.InvalidArgument,
		},
		{
			name: "QBError with ErrConfiguration",
			err: &dberrors.QBError{
				Code: dberrors.ErrConfiguration,
				Op:   "config.Load",
				Err:  errors.New("missing required field"),
			},
			expectedCode: codes.FailedPrecondition,
		},
		{
			name: "QBError with ErrSnapshot",
			err: &dberrors.QBError{
				Code: dberrors.ErrSnapshot,
				Op:   "snapshot.Create",
				Err:  errors.New("snapshot failed"),
			},
			expectedCode: codes.Internal,
		},
		{
			name: "QBError with ErrReplication",
			err: &dberrors.QBError{
				Code: dberrors.ErrReplication,
				Op:   "replication.Sync",
				Err:  errors.New("replication lag"),
			},
			expectedCode: codes.Internal,
		},
		{
			name: "QBError with ErrLeaderNotAvailable",
			err: &dberrors.QBError{
				Code: dberrors.ErrLeaderNotAvailable,
				Op:   "raft.GetLeader",
				Err:  errors.New("no leader elected"),
			},
			expectedCode: codes.Unavailable,
		},
		{
			name: "QBError with ErrClusterNotReady",
			err: &dberrors.QBError{
				Code: dberrors.ErrClusterNotReady,
				Op:   "cluster.Join",
				Err:  errors.New("cluster initializing"),
			},
			expectedCode: codes.Unavailable,
		},
		{
			name:         "plain error maps to codes.Internal",
			err:          errors.New("unexpected error"),
			expectedCode: codes.Internal,
			checkMessage: true,
			message:      "unexpected error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ToGRPCError(tt.err)
			require.NotNil(t, result)

			st, ok := status.FromError(result)
			require.True(t, ok, "result should be a gRPC status error")
			assert.Equal(t, tt.expectedCode, st.Code())

			if tt.checkMessage {
				assert.Equal(t, tt.message, st.Message())
			}
		})
	}
}

// TestConvertError_NilInput tests convertError specifically with nil input.
func TestConvertError_NilInput(t *testing.T) {
	result := convertError(nil)
	assert.Nil(t, result, "convertError(nil) should return nil")
}

// TestConvertError_VariousErrorTypes tests convertError with various error types.
func TestConvertError_VariousErrorTypes(t *testing.T) {
	tests := []struct {
		name            string
		err             error
		expectedCode    dberrors.ErrorCode
		expectedOp      string
		expectedMessage string
	}{
		{
			name:            "gRPC NotFound error",
			err:             status.Error(codes.NotFound, "key not found"),
			expectedCode:    dberrors.ErrNotFound,
			expectedOp:      "grpc",
			expectedMessage: "key not found",
		},
		{
			name:            "gRPC Internal error",
			err:             status.Error(codes.Internal, "internal server error"),
			expectedCode:    dberrors.ErrInternal,
			expectedOp:      "grpc",
			expectedMessage: "internal server error",
		},
		{
			name:            "gRPC InvalidArgument error",
			err:             status.Error(codes.InvalidArgument, "bad request"),
			expectedCode:    dberrors.ErrInvalidInput,
			expectedOp:      "grpc",
			expectedMessage: "bad request",
		},
		{
			name:            "gRPC Unauthenticated error",
			err:             status.Error(codes.Unauthenticated, "missing credentials"),
			expectedCode:    dberrors.ErrUnauthorized,
			expectedOp:      "grpc",
			expectedMessage: "missing credentials",
		},
		{
			name:            "gRPC PermissionDenied error",
			err:             status.Error(codes.PermissionDenied, "access denied"),
			expectedCode:    dberrors.ErrForbidden,
			expectedOp:      "grpc",
			expectedMessage: "access denied",
		},
		{
			name:            "gRPC Unavailable error",
			err:             status.Error(codes.Unavailable, "service unavailable"),
			expectedCode:    dberrors.ErrUnavailable,
			expectedOp:      "grpc",
			expectedMessage: "service unavailable",
		},
		{
			name:            "gRPC Canceled error",
			err:             status.Error(codes.Canceled, "operation canceled"),
			expectedCode:    dberrors.ErrCanceled,
			expectedOp:      "grpc",
			expectedMessage: "operation canceled",
		},
		{
			name:            "gRPC DeadlineExceeded error",
			err:             status.Error(codes.DeadlineExceeded, "deadline exceeded"),
			expectedCode:    dberrors.ErrDeadlineExceeded,
			expectedOp:      "grpc",
			expectedMessage: "deadline exceeded",
		},
		{
			name:            "gRPC AlreadyExists error",
			err:             status.Error(codes.AlreadyExists, "resource exists"),
			expectedCode:    dberrors.ErrAlreadyExists,
			expectedOp:      "grpc",
			expectedMessage: "resource exists",
		},
		{
			name:            "gRPC ResourceExhausted error",
			err:             status.Error(codes.ResourceExhausted, "rate limit exceeded"),
			expectedCode:    dberrors.ErrResourceExhausted,
			expectedOp:      "grpc",
			expectedMessage: "rate limit exceeded",
		},
		{
			name:            "gRPC FailedPrecondition error",
			err:             status.Error(codes.FailedPrecondition, "precondition not met"),
			expectedCode:    dberrors.ErrPreconditionFailed,
			expectedOp:      "grpc",
			expectedMessage: "precondition not met",
		},
		{
			name:            "gRPC Aborted error",
			err:             status.Error(codes.Aborted, "transaction aborted"),
			expectedCode:    dberrors.ErrAborted,
			expectedOp:      "grpc",
			expectedMessage: "transaction aborted",
		},
		{
			name:            "gRPC OutOfRange error",
			err:             status.Error(codes.OutOfRange, "index out of bounds"),
			expectedCode:    dberrors.ErrOutOfRange,
			expectedOp:      "grpc",
			expectedMessage: "index out of bounds",
		},
		{
			name:            "gRPC Unimplemented error",
			err:             status.Error(codes.Unimplemented, "method not implemented"),
			expectedCode:    dberrors.ErrUnimplemented,
			expectedOp:      "grpc",
			expectedMessage: "method not implemented",
		},
		{
			name:            "gRPC DataLoss error",
			err:             status.Error(codes.DataLoss, "data corrupted"),
			expectedCode:    dberrors.ErrDataLoss,
			expectedOp:      "grpc",
			expectedMessage: "data corrupted",
		},
		{
			name:            "gRPC Unknown error",
			err:             status.Error(codes.Unknown, "unknown error"),
			expectedCode:    dberrors.ErrUnknown,
			expectedOp:      "grpc",
			expectedMessage: "unknown error",
		},
		// Note: status.Error(codes.OK, ...) returns nil, so that case
		// is covered by TestConvertError_NilInput
		{
			name:         "plain error wraps with ErrInternal",
			err:          errors.New("unexpected failure"),
			expectedCode: dberrors.ErrInternal,
			expectedOp:   "grpc",
		},
		{
			name:         "wrapped plain error wraps with ErrInternal",
			err:          errors.New("context: nested error"),
			expectedCode: dberrors.ErrInternal,
			expectedOp:   "grpc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertError(tt.err)
			require.NotNil(t, result)

			var de *dberrors.QBError
			require.True(t, errors.As(result, &de), "result should be a QBError")
			assert.Equal(t, tt.expectedCode, de.Code)
			assert.Equal(t, tt.expectedOp, de.Op)

			if tt.expectedMessage != "" {
				assert.Contains(t, de.Err.Error(), tt.expectedMessage)
			}
		})
	}
}

// TestConvertError_PreservesGRPCMessage verifies that convertError preserves the original gRPC message.
func TestConvertError_PreservesGRPCMessage(t *testing.T) {
	originalMessage := "specific error message with details"
	grpcErr := status.Error(codes.InvalidArgument, originalMessage)

	result := convertError(grpcErr)
	require.NotNil(t, result)

	var de *dberrors.QBError
	require.True(t, errors.As(result, &de))
	assert.Equal(t, originalMessage, de.Err.Error())
}

// TestGrpcCodeMapping_Completeness verifies that all standard gRPC codes are handled.
func TestGrpcCodeMapping_Completeness(t *testing.T) {
	// All standard gRPC status codes as of gRPC 1.x
	standardGRPCCodes := []codes.Code{
		codes.OK,
		codes.Canceled,
		codes.Unknown,
		codes.InvalidArgument,
		codes.DeadlineExceeded,
		codes.NotFound,
		codes.AlreadyExists,
		codes.PermissionDenied,
		codes.ResourceExhausted,
		codes.FailedPrecondition,
		codes.Aborted,
		codes.OutOfRange,
		codes.Unimplemented,
		codes.Internal,
		codes.Unavailable,
		codes.DataLoss,
		codes.Unauthenticated,
	}

	for _, code := range standardGRPCCodes {
		t.Run(code.String(), func(t *testing.T) {
			// Should not panic and should return a valid error code (non-negative)
			result := grpcCodeToErrorCode(code)
			assert.GreaterOrEqual(t, int(result), 0, "code %v should map to a valid error code", code)
		})
	}
}

// TestErrorCodeMapping_Completeness verifies that all QuicBaaS error codes are handled.
func TestErrorCodeMapping_Completeness(t *testing.T) {
	// All QuicBaaS error codes
	allErrorCodes := []dberrors.ErrorCode{
		dberrors.ErrUnknown,
		dberrors.ErrNotFound,
		dberrors.ErrConflict,
		dberrors.ErrTimeout,
		dberrors.ErrInternal,
		dberrors.ErrInvalidInput,
		dberrors.ErrUnauthorized,
		dberrors.ErrForbidden,
		dberrors.ErrUnavailable,
		dberrors.ErrStorage,
		dberrors.ErrRaft,
		dberrors.ErrEncryption,
		dberrors.ErrDecryption,
		dberrors.ErrValidation,
		dberrors.ErrConfiguration,
		dberrors.ErrCanceled,
		dberrors.ErrDeadlineExceeded,
		dberrors.ErrAlreadyExists,
		dberrors.ErrResourceExhausted,
		dberrors.ErrPreconditionFailed,
		dberrors.ErrAborted,
		dberrors.ErrOutOfRange,
		dberrors.ErrUnimplemented,
		dberrors.ErrDataLoss,
		dberrors.ErrSnapshot,
		dberrors.ErrReplication,
		dberrors.ErrLeaderNotAvailable,
		dberrors.ErrClusterNotReady,
		dberrors.ErrQuorumLost,
		dberrors.ErrSplitBrain,
		dberrors.ErrRateLimited,
		dberrors.ErrNotImplemented,
		dberrors.ErrNotAllowed,
		dberrors.ErrSystemKeyspaceViolation,
		dberrors.ErrCrossTenantAccess,
		dberrors.ErrMissingTenantContext,
		dberrors.ErrMissingJustification,
		dberrors.ErrMissingUserContext,
	}

	for _, errCode := range allErrorCodes {
		t.Run(errCode.String(), func(t *testing.T) {
			// Should not panic and should return a valid gRPC code
			result := errorCodeToGRPCCode(errCode)
			// Verify it's a known gRPC code by checking it's within valid range (0-16)
			assert.LessOrEqual(t, uint32(result), uint32(16), "error code %v should map to a valid gRPC code", errCode)
		})
	}
}

// TestToGRPCError_AlreadyGRPCStatus verifies passthrough behavior for existing gRPC status errors.
func TestToGRPCError_AlreadyGRPCStatus(t *testing.T) {
	originalErr := status.Error(codes.NotFound, "original message")
	result := ToGRPCError(originalErr)

	// Should be the exact same error (passthrough)
	assert.Equal(t, originalErr, result)
}

// TestFromGRPCError_AllStandardCodes tests FromGRPCError with all standard gRPC codes.
func TestFromGRPCError_AllStandardCodes(t *testing.T) {
	expectedMappings := map[codes.Code]dberrors.ErrorCode{
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

	for grpcCode, expectedDBCode := range expectedMappings {
		t.Run(grpcCode.String(), func(t *testing.T) {
			grpcErr := status.Error(grpcCode, "test message")
			code, ok := FromGRPCError(grpcErr)
			assert.True(t, ok)
			assert.Equal(t, expectedDBCode, code)
		})
	}
}
