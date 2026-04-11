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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// protoToConsistencyLevel Tests
// =============================================================================

func TestProtoToConsistencyLevel_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.ConsistencyLevel
		expected qrdbtransport.ConsistencyLevel
	}{
		{
			name:     "strong_consistency",
			input:    pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG,
			expected: qrdbtransport.ConsistencyStrong,
		},
		{
			name:     "linearizable_consistency",
			input:    pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
			expected: qrdbtransport.ConsistencyLinearizable,
		},
		{
			name:     "eventual_consistency",
			input:    pb.ConsistencyLevel_CONSISTENCY_LEVEL_EVENTUAL,
			expected: qrdbtransport.ConsistencyEventual,
		},
		{
			name:     "unspecified_defaults_to_linearizable",
			input:    pb.ConsistencyLevel_CONSISTENCY_LEVEL_UNSPECIFIED,
			expected: qrdbtransport.ConsistencyLinearizable,
		},
		{
			name:     "unknown_value_defaults_to_linearizable",
			input:    pb.ConsistencyLevel(999),
			expected: qrdbtransport.ConsistencyLinearizable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoToConsistencyLevel(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// consistencyLevelToProto Tests
// =============================================================================

func TestConsistencyLevelToProto_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    qrdbtransport.ConsistencyLevel
		expected pb.ConsistencyLevel
	}{
		{
			name:     "strong_to_proto",
			input:    qrdbtransport.ConsistencyStrong,
			expected: pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG,
		},
		{
			name:     "linearizable_to_proto",
			input:    qrdbtransport.ConsistencyLinearizable,
			expected: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
		},
		{
			name:     "eventual_to_proto",
			input:    qrdbtransport.ConsistencyEventual,
			expected: pb.ConsistencyLevel_CONSISTENCY_LEVEL_EVENTUAL,
		},
		{
			name:     "unknown_defaults_to_linearizable",
			input:    qrdbtransport.ConsistencyLevel(255),
			expected: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := consistencyLevelToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// mapServiceErrorCode Tests
// =============================================================================

func TestMapServiceErrorCode_AllCodes(t *testing.T) {
	tests := []struct {
		name     string
		input    errors.ErrorCode
		expected codes.Code
	}{
		{
			name:     "not_found",
			input:    errors.ErrNotFound,
			expected: codes.NotFound,
		},
		{
			name:     "conflict",
			input:    errors.ErrConflict,
			expected: codes.AlreadyExists,
		},
		{
			name:     "already_exists",
			input:    errors.ErrAlreadyExists,
			expected: codes.AlreadyExists,
		},
		{
			name:     "timeout",
			input:    errors.ErrTimeout,
			expected: codes.DeadlineExceeded,
		},
		{
			name:     "deadline_exceeded",
			input:    errors.ErrDeadlineExceeded,
			expected: codes.DeadlineExceeded,
		},
		{
			name:     "invalid_input",
			input:    errors.ErrInvalidInput,
			expected: codes.InvalidArgument,
		},
		{
			name:     "validation",
			input:    errors.ErrValidation,
			expected: codes.InvalidArgument,
		},
		{
			name:     "out_of_range",
			input:    errors.ErrOutOfRange,
			expected: codes.InvalidArgument,
		},
		{
			name:     "unauthorized",
			input:    errors.ErrUnauthorized,
			expected: codes.Unauthenticated,
		},
		{
			name:     "forbidden",
			input:    errors.ErrForbidden,
			expected: codes.PermissionDenied,
		},
		{
			name:     "unavailable",
			input:    errors.ErrUnavailable,
			expected: codes.Unavailable,
		},
		{
			name:     "leader_not_available",
			input:    errors.ErrLeaderNotAvailable,
			expected: codes.Unavailable,
		},
		{
			name:     "cluster_not_ready",
			input:    errors.ErrClusterNotReady,
			expected: codes.Unavailable,
		},
		{
			name:     "unimplemented",
			input:    errors.ErrUnimplemented,
			expected: codes.Unimplemented,
		},
		{
			name:     "resource_exhausted",
			input:    errors.ErrResourceExhausted,
			expected: codes.ResourceExhausted,
		},
		{
			name:     "precondition_failed",
			input:    errors.ErrPreconditionFailed,
			expected: codes.FailedPrecondition,
		},
		{
			name:     "aborted",
			input:    errors.ErrAborted,
			expected: codes.Aborted,
		},
		{
			name:     "canceled",
			input:    errors.ErrCanceled,
			expected: codes.Canceled,
		},
		{
			name:     "data_loss",
			input:    errors.ErrDataLoss,
			expected: codes.DataLoss,
		},
		{
			name:     "unknown_code_defaults_to_internal",
			input:    errors.ErrorCode(9999),
			expected: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapServiceErrorCode(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// toServiceError Tests
// =============================================================================

func TestToServiceError_AllCases(t *testing.T) {
	t.Run("nil_error_returns_nil", func(t *testing.T) {
		result := toServiceError(nil)
		assert.Nil(t, result)
	})

	t.Run("grpc_status_error_returns_as_is", func(t *testing.T) {
		originalErr := status.Error(codes.NotFound, "not found")
		result := toServiceError(originalErr)
		require.Error(t, result)
		assert.Equal(t, codes.NotFound, status.Code(result))
	})

	t.Run("qb_error_maps_correctly", func(t *testing.T) {
		qbErr := &errors.QBError{
			Code: errors.ErrForbidden,
			Op:   "test_operation",
			Err:  stderrors.New("access denied"),
		}
		result := toServiceError(qbErr)
		require.Error(t, result)
		assert.Equal(t, codes.PermissionDenied, status.Code(result))
	})

	t.Run("plain_error_wraps_as_internal", func(t *testing.T) {
		plainErr := stderrors.New("some unknown error")
		result := toServiceError(plainErr)
		require.Error(t, result)
		assert.Equal(t, codes.Internal, status.Code(result))
	})

	t.Run("wrapped_qb_error", func(t *testing.T) {
		qbErr := &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "get",
			Err:  stderrors.New("entity not found"),
		}
		result := toServiceError(qbErr)
		require.Error(t, result)
		assert.Equal(t, codes.NotFound, status.Code(result))
	})

	t.Run("qb_error_timeout", func(t *testing.T) {
		qbErr := &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "operation",
			Err:  stderrors.New("operation timed out"),
		}
		result := toServiceError(qbErr)
		require.Error(t, result)
		assert.Equal(t, codes.DeadlineExceeded, status.Code(result))
	})

	t.Run("qb_error_unavailable", func(t *testing.T) {
		qbErr := &errors.QBError{
			Code: errors.ErrUnavailable,
			Op:   "connect",
			Err:  stderrors.New("service unavailable"),
		}
		result := toServiceError(qbErr)
		require.Error(t, result)
		assert.Equal(t, codes.Unavailable, status.Code(result))
	})
}
