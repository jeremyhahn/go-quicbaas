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

package service

import (
	"context"
	stderrors "errors"
	"net/http"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// Sentinel Error Tests
// =============================================================================

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		expectedCode dberrors.ErrorCode
		expectedOp   string
	}{
		{
			name:         "ErrNotFound",
			err:          ErrNotFound,
			expectedCode: dberrors.ErrNotFound,
			expectedOp:   "service",
		},
		{
			name:         "ErrAlreadyExists",
			err:          ErrAlreadyExists,
			expectedCode: dberrors.ErrAlreadyExists,
			expectedOp:   "service",
		},
		{
			name:         "ErrInvalidInput",
			err:          ErrInvalidInput,
			expectedCode: dberrors.ErrInvalidInput,
			expectedOp:   "service",
		},
		{
			name:         "ErrUnauthorized",
			err:          ErrUnauthorized,
			expectedCode: dberrors.ErrUnauthorized,
			expectedOp:   "service",
		},
		{
			name:         "ErrForbidden",
			err:          ErrForbidden,
			expectedCode: dberrors.ErrForbidden,
			expectedOp:   "service",
		},
		{
			name:         "ErrTimeout",
			err:          ErrTimeout,
			expectedCode: dberrors.ErrTimeout,
			expectedOp:   "service",
		},
		{
			name:         "ErrRaft",
			err:          ErrRaft,
			expectedCode: dberrors.ErrRaft,
			expectedOp:   "service",
		},
		{
			name:         "ErrStorage",
			err:          ErrStorage,
			expectedCode: dberrors.ErrStorage,
			expectedOp:   "service",
		},
		{
			name:         "ErrInternal",
			err:          ErrInternal,
			expectedCode: dberrors.ErrInternal,
			expectedOp:   "service",
		},
		{
			name:         "ErrUnavailable",
			err:          ErrUnavailable,
			expectedCode: dberrors.ErrUnavailable,
			expectedOp:   "service",
		},
		{
			name:         "ErrLeaderNotAvailable",
			err:          ErrLeaderNotAvailable,
			expectedCode: dberrors.ErrLeaderNotAvailable,
			expectedOp:   "service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dErr *dberrors.QBError
			if !dberrors.As(tt.err, &dErr) {
				t.Fatalf("expected QBError, got %T", tt.err)
			}

			if dErr.Code != tt.expectedCode {
				t.Errorf("expected code %v, got %v", tt.expectedCode, dErr.Code)
			}

			if dErr.Op != tt.expectedOp {
				t.Errorf("expected op %q, got %q", tt.expectedOp, dErr.Op)
			}

			if dErr.Error() == "" {
				t.Error("expected non-empty error message")
			}
		})
	}
}

// =============================================================================
// WrapError Tests
// =============================================================================

func TestWrapError(t *testing.T) {
	t.Run("wrap plain error", func(t *testing.T) {
		baseErr := stderrors.New("base error")
		wrapped := WrapError(baseErr, dberrors.ErrStorage, "service.Put", "storage write failed")

		var dErr *dberrors.QBError
		if !dberrors.As(wrapped, &dErr) {
			t.Fatal("expected QBError")
		}

		if dErr.Code != dberrors.ErrStorage {
			t.Errorf("expected code %v, got %v", dberrors.ErrStorage, dErr.Code)
		}

		if dErr.Op != "service.Put" {
			t.Errorf("expected op 'service.Put', got %q", dErr.Op)
		}

		if !stderrors.Is(wrapped, baseErr) {
			t.Error("wrapped error should preserve error chain")
		}
	})

	t.Run("wrap nil error", func(t *testing.T) {
		wrapped := WrapError(nil, dberrors.ErrInternal, "service.Get", "test")
		if wrapped != nil {
			t.Error("wrapping nil should return nil")
		}
	})

	t.Run("wrap existing QBError", func(t *testing.T) {
		baseErr := &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "storage.Get",
			Err:  stderrors.New("entity not found"),
		}

		wrapped := WrapError(baseErr, dberrors.ErrStorage, "service.Get", "failed to retrieve")

		var dErr *dberrors.QBError
		if !dberrors.As(wrapped, &dErr) {
			t.Fatal("expected QBError")
		}

		if dErr.Code != dberrors.ErrStorage {
			t.Errorf("expected code %v, got %v", dberrors.ErrStorage, dErr.Code)
		}

		// Should preserve error chain
		if !stderrors.Is(wrapped, baseErr) {
			t.Error("wrapped error should preserve error chain")
		}
	})

	t.Run("empty message", func(t *testing.T) {
		baseErr := stderrors.New("base error")
		wrapped := WrapError(baseErr, dberrors.ErrInternal, "service.Test", "")

		if !stderrors.Is(wrapped, baseErr) {
			t.Error("wrapped error should preserve error chain")
		}
	})
}

func TestWrapErrorf(t *testing.T) {
	t.Run("formatted message", func(t *testing.T) {
		baseErr := stderrors.New("base error")
		wrapped := WrapErrorf(baseErr, dberrors.ErrNotFound, "service.Get",
			"entity %s/%d not found", "user", uint64(123))

		var dErr *dberrors.QBError
		if !dberrors.As(wrapped, &dErr) {
			t.Fatal("expected QBError")
		}

		if dErr.Code != dberrors.ErrNotFound {
			t.Errorf("expected code %v, got %v", dberrors.ErrNotFound, dErr.Code)
		}

		errMsg := wrapped.Error()
		if errMsg == "" {
			t.Error("expected non-empty error message")
		}
	})

	t.Run("wrap nil error", func(t *testing.T) {
		wrapped := WrapErrorf(nil, dberrors.ErrInternal, "service.Get", "test %s", "param")
		if wrapped != nil {
			t.Error("wrapping nil should return nil")
		}
	})
}

// =============================================================================
// NewError Tests
// =============================================================================

func TestNewError(t *testing.T) {
	t.Run("create new error", func(t *testing.T) {
		err := NewError(dberrors.ErrInvalidInput, "service.Validate", "entity type cannot be empty")

		var dErr *dberrors.QBError
		if !dberrors.As(err, &dErr) {
			t.Fatal("expected QBError")
		}

		if dErr.Code != dberrors.ErrInvalidInput {
			t.Errorf("expected code %v, got %v", dberrors.ErrInvalidInput, dErr.Code)
		}

		if dErr.Op != "service.Validate" {
			t.Errorf("expected op 'service.Validate', got %q", dErr.Op)
		}

		errMsg := err.Error()
		if errMsg == "" {
			t.Error("expected non-empty error message")
		}
	})
}

func TestNewErrorf(t *testing.T) {
	t.Run("create formatted error", func(t *testing.T) {
		err := NewErrorf(dberrors.ErrInvalidInput, "service.Validate",
			"invalid entity type: %s", "invalid_type")

		var dErr *dberrors.QBError
		if !dberrors.As(err, &dErr) {
			t.Fatal("expected QBError")
		}

		if dErr.Code != dberrors.ErrInvalidInput {
			t.Errorf("expected code %v, got %v", dberrors.ErrInvalidInput, dErr.Code)
		}
	})
}

// =============================================================================
// Error Type Checking Tests
// =============================================================================

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "not found error",
			err:      ErrNotFound,
			expected: true,
		},
		{
			name: "wrapped not found",
			err: &dberrors.QBError{
				Code: dberrors.ErrNotFound,
				Op:   "test",
				Err:  stderrors.New("not found"),
			},
			expected: true,
		},
		{
			name: "different error",
			err: &dberrors.QBError{
				Code: dberrors.ErrInternal,
				Op:   "test",
				Err:  stderrors.New("internal error"),
			},
			expected: false,
		},
		{
			name:     "plain error",
			err:      stderrors.New("plain error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsNotFound(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsTimeout(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "timeout error",
			err:      ErrTimeout,
			expected: true,
		},
		{
			name: "deadline exceeded",
			err: &dberrors.QBError{
				Code: dberrors.ErrDeadlineExceeded,
				Op:   "test",
				Err:  stderrors.New("deadline exceeded"),
			},
			expected: true,
		},
		{
			name:     "context deadline",
			err:      context.DeadlineExceeded,
			expected: true,
		},
		{
			name:     "different error",
			err:      ErrInternal,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsTimeout(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsUnavailable(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "unavailable error",
			err:      ErrUnavailable,
			expected: true,
		},
		{
			name:     "leader not available",
			err:      ErrLeaderNotAvailable,
			expected: true,
		},
		{
			name:     "cluster not ready",
			err:      ErrClusterNotReady,
			expected: true,
		},
		{
			name:     "different error",
			err:      ErrInternal,
			expected: false,
		},
		{
			name:     "plain error",
			err:      stderrors.New("plain error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsUnavailable(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsUnauthorized(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "unauthorized error",
			err:      ErrUnauthorized,
			expected: true,
		},
		{
			name:     "different error",
			err:      ErrForbidden,
			expected: false,
		},
		{
			name:     "plain error",
			err:      stderrors.New("plain error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsUnauthorized(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsForbidden(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "forbidden error",
			err:      ErrForbidden,
			expected: true,
		},
		{
			name:     "different error",
			err:      ErrUnauthorized,
			expected: false,
		},
		{
			name:     "plain error",
			err:      stderrors.New("plain error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsForbidden(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsConflict(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "conflict error",
			err:      ErrConflict,
			expected: true,
		},
		{
			name:     "already exists",
			err:      ErrAlreadyExists,
			expected: true,
		},
		{
			name:     "different error",
			err:      ErrInternal,
			expected: false,
		},
		{
			name:     "plain error",
			err:      stderrors.New("plain error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsConflict(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsInvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "invalid input error",
			err:      ErrInvalidInput,
			expected: true,
		},
		{
			name:     "different error",
			err:      ErrInternal,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsInvalidInput(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsCanceled(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "canceled error",
			err:      ErrCanceled,
			expected: true,
		},
		{
			name:     "context canceled",
			err:      context.Canceled,
			expected: true,
		},
		{
			name:     "different error",
			err:      ErrInternal,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsCanceled(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "timeout is retryable",
			err:      ErrTimeout,
			expected: true,
		},
		{
			name:     "unavailable is retryable",
			err:      ErrUnavailable,
			expected: true,
		},
		{
			name:     "not found is not retryable",
			err:      ErrNotFound,
			expected: false,
		},
		{
			name:     "invalid input is not retryable",
			err:      ErrInvalidInput,
			expected: false,
		},
		{
			name:     "plain error",
			err:      stderrors.New("plain error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsRetryable(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsRaftError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "raft error",
			err:      ErrRaft,
			expected: true,
		},
		{
			name:     "different error",
			err:      ErrStorage,
			expected: false,
		},
		{
			name:     "plain error",
			err:      stderrors.New("plain error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsRaftError(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsStorageError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "storage error",
			err:      ErrStorage,
			expected: true,
		},
		{
			name:     "different error",
			err:      ErrRaft,
			expected: false,
		},
		{
			name:     "plain error",
			err:      stderrors.New("plain error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsStorageError(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// =============================================================================
// gRPC Error Mapping Tests
// =============================================================================

func TestToGRPCError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		expectedCode codes.Code
		passThrough  bool
	}{
		{
			name:         "nil error",
			err:          nil,
			expectedCode: codes.OK,
		},
		{
			name:         "not found",
			err:          ErrNotFound,
			expectedCode: codes.NotFound,
		},
		{
			name:         "invalid input",
			err:          ErrInvalidInput,
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "unauthorized",
			err:          ErrUnauthorized,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "forbidden",
			err:          ErrForbidden,
			expectedCode: codes.PermissionDenied,
		},
		{
			name:         "timeout",
			err:          ErrTimeout,
			expectedCode: codes.DeadlineExceeded,
		},
		{
			name:         "unavailable",
			err:          ErrUnavailable,
			expectedCode: codes.Unavailable,
		},
		{
			name:         "conflict",
			err:          ErrConflict,
			expectedCode: codes.AlreadyExists,
		},
		{
			name:         "already exists",
			err:          ErrAlreadyExists,
			expectedCode: codes.AlreadyExists,
		},
		{
			name:         "precondition failed",
			err:          ErrPreconditionFailed,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "resource exhausted",
			err:          ErrResourceExhausted,
			expectedCode: codes.ResourceExhausted,
		},
		{
			name:         "canceled",
			err:          ErrCanceled,
			expectedCode: codes.Canceled,
		},
		{
			name:         "internal",
			err:          ErrInternal,
			expectedCode: codes.Internal,
		},
		{
			name:         "raft error",
			err:          ErrRaft,
			expectedCode: codes.Internal,
		},
		{
			name:         "storage error",
			err:          ErrStorage,
			expectedCode: codes.Internal,
		},
		{
			name:         "leader not available",
			err:          ErrLeaderNotAvailable,
			expectedCode: codes.Unavailable,
		},
		{
			name:         "plain error",
			err:          stderrors.New("plain error"),
			expectedCode: codes.Internal,
		},
		{
			name:         "gRPC status error passes through unchanged",
			err:          status.Error(codes.NotFound, "not found"),
			expectedCode: codes.NotFound,
			passThrough:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grpcErr := ToGRPCError(tt.err)

			if tt.passThrough && grpcErr != tt.err {
				t.Error("expected gRPC error to be preserved")
			}

			if tt.err == nil {
				if grpcErr != nil {
					t.Error("expected nil error")
				}
				return
			}

			st, ok := status.FromError(grpcErr)
			if !ok {
				t.Fatal("expected gRPC status error")
			}

			if st.Code() != tt.expectedCode {
				t.Errorf("expected code %v, got %v", tt.expectedCode, st.Code())
			}
		})
	}
}

func TestFromGRPCError(t *testing.T) {
	tests := []struct {
		name         string
		grpcErr      error
		expectedCode dberrors.ErrorCode
	}{
		{
			name:         "nil error",
			grpcErr:      nil,
			expectedCode: dberrors.ErrUnknown,
		},
		{
			name:         "not found",
			grpcErr:      status.Error(codes.NotFound, "not found"),
			expectedCode: dberrors.ErrNotFound,
		},
		{
			name:         "invalid argument",
			grpcErr:      status.Error(codes.InvalidArgument, "invalid"),
			expectedCode: dberrors.ErrInvalidInput,
		},
		{
			name:         "unauthenticated",
			grpcErr:      status.Error(codes.Unauthenticated, "auth required"),
			expectedCode: dberrors.ErrUnauthorized,
		},
		{
			name:         "permission denied",
			grpcErr:      status.Error(codes.PermissionDenied, "forbidden"),
			expectedCode: dberrors.ErrForbidden,
		},
		{
			name:         "deadline exceeded",
			grpcErr:      status.Error(codes.DeadlineExceeded, "timeout"),
			expectedCode: dberrors.ErrDeadlineExceeded,
		},
		{
			name:         "unavailable",
			grpcErr:      status.Error(codes.Unavailable, "unavailable"),
			expectedCode: dberrors.ErrUnavailable,
		},
		{
			name:         "already exists",
			grpcErr:      status.Error(codes.AlreadyExists, "exists"),
			expectedCode: dberrors.ErrAlreadyExists,
		},
		{
			name:         "internal",
			grpcErr:      status.Error(codes.Internal, "internal error"),
			expectedCode: dberrors.ErrInternal,
		},
		{
			name:         "resource exhausted",
			grpcErr:      status.Error(codes.ResourceExhausted, "kms.Encrypt: resource_exhausted: quota spent"),
			expectedCode: dberrors.ErrResourceExhausted,
		},
		{
			name:         "rate limited with op",
			grpcErr:      status.Error(codes.ResourceExhausted, "kms.Encrypt: rate_limited: slow down"),
			expectedCode: dberrors.ErrRateLimited,
		},
		{
			name:         "rate limited without op",
			grpcErr:      status.Error(codes.ResourceExhausted, "rate_limited: slow down"),
			expectedCode: dberrors.ErrRateLimited,
		},
		{
			name:         "rate_limited only in the message text",
			grpcErr:      status.Error(codes.ResourceExhausted, "kms.Encrypt: resource_exhausted: rate_limited"),
			expectedCode: dberrors.ErrResourceExhausted,
		},
		{
			name:         "rate_limited code on another status",
			grpcErr:      status.Error(codes.Internal, "rate_limited: slow down"),
			expectedCode: dberrors.ErrInternal,
		},
		{
			name:         "plain error",
			grpcErr:      stderrors.New("plain error"),
			expectedCode: dberrors.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := FromGRPCError(tt.grpcErr)

			if tt.grpcErr == nil {
				if err != nil {
					t.Error("expected nil error")
				}
				return
			}

			var dErr *dberrors.QBError
			if !dberrors.As(err, &dErr) {
				t.Fatal("expected QBError")
			}

			if dErr.Code != tt.expectedCode {
				t.Errorf("expected code %v, got %v", tt.expectedCode, dErr.Code)
			}
		})
	}
}

// =============================================================================
// HTTP Status Mapping Tests
// =============================================================================

func TestToHTTPStatus(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedStatus int
	}{
		{
			name:           "nil error",
			err:            nil,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "not found",
			err:            ErrNotFound,
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "invalid input",
			err:            ErrInvalidInput,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "unauthorized",
			err:            ErrUnauthorized,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "forbidden",
			err:            ErrForbidden,
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "timeout",
			err:            ErrTimeout,
			expectedStatus: http.StatusRequestTimeout,
		},
		{
			name:           "conflict",
			err:            ErrConflict,
			expectedStatus: http.StatusConflict,
		},
		{
			name:           "already exists",
			err:            ErrAlreadyExists,
			expectedStatus: http.StatusConflict,
		},
		{
			name:           "precondition failed",
			err:            ErrPreconditionFailed,
			expectedStatus: http.StatusPreconditionFailed,
		},
		{
			name:           "resource exhausted",
			err:            ErrResourceExhausted,
			expectedStatus: http.StatusTooManyRequests,
		},
		{
			name:           "unavailable",
			err:            ErrUnavailable,
			expectedStatus: http.StatusServiceUnavailable,
		},
		{
			name:           "internal",
			err:            ErrInternal,
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:           "raft error",
			err:            ErrRaft,
			expectedStatus: http.StatusServiceUnavailable,
		},
		{
			name:           "storage error",
			err:            ErrStorage,
			expectedStatus: http.StatusServiceUnavailable,
		},
		{
			name:           "leader not available",
			err:            ErrLeaderNotAvailable,
			expectedStatus: http.StatusServiceUnavailable,
		},
		{
			name:           "plain error",
			err:            stderrors.New("plain error"),
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statusCode := ToHTTPStatus(tt.err)
			if statusCode != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, statusCode)
			}
		})
	}
}

// =============================================================================
// Error Context Tests
// =============================================================================

func TestWithEntityContext(t *testing.T) {
	t.Run("add context to QBError", func(t *testing.T) {
		baseErr := &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "storage.Get",
			Err:  stderrors.New("not found"),
		}

		err := WithEntityContext(baseErr, "user", 123, "service.Get")

		var dErr *dberrors.QBError
		if !dberrors.As(err, &dErr) {
			t.Fatal("expected QBError")
		}

		entityType, ok := dErr.GetContext("entity_type")
		if !ok || entityType != "user" {
			t.Errorf("expected entity_type 'user', got %v", entityType)
		}

		entityID, ok := dErr.GetContext("entity_id")
		if !ok || entityID != uint64(123) {
			t.Errorf("expected entity_id 123, got %v", entityID)
		}

		operation, ok := dErr.GetContext("operation")
		if !ok || operation != "service.Get" {
			t.Errorf("expected operation 'service.Get', got %v", operation)
		}
	})

	t.Run("add context to plain error", func(t *testing.T) {
		baseErr := stderrors.New("plain error")
		err := WithEntityContext(baseErr, "user", 123, "service.Get")

		var dErr *dberrors.QBError
		if !dberrors.As(err, &dErr) {
			t.Fatal("expected QBError")
		}

		entityType, ok := dErr.GetContext("entity_type")
		if !ok || entityType != "user" {
			t.Errorf("expected entity_type 'user', got %v", entityType)
		}
		assert.Contains(t, err.Error(), "plain error")
	})

	t.Run("nil error", func(t *testing.T) {
		err := WithEntityContext(nil, "user", 123, "service.Get")
		if err != nil {
			t.Error("expected nil error")
		}
	})
}

func TestWithShardContext(t *testing.T) {
	t.Run("add shard context", func(t *testing.T) {
		baseErr := &dberrors.QBError{
			Code: dberrors.ErrRaft,
			Op:   "raft.Propose",
			Err:  stderrors.New("proposal failed"),
		}

		err := WithShardContext(baseErr, 1, 100)

		var dErr *dberrors.QBError
		if !dberrors.As(err, &dErr) {
			t.Fatal("expected QBError")
		}

		shardID, ok := dErr.GetContext("shard_id")
		if !ok || shardID != uint64(1) {
			t.Errorf("expected shard_id 1, got %v", shardID)
		}

		nodeID, ok := dErr.GetContext("node_id")
		if !ok || nodeID != uint64(100) {
			t.Errorf("expected node_id 100, got %v", nodeID)
		}
	})

	t.Run("wraps plain error", func(t *testing.T) {
		err := WithShardContext(stderrors.New("plain error"), 2, 300)
		require.NotNil(t, err)
		assert.Contains(t, err.Error(), "plain error")
	})

	t.Run("nil error", func(t *testing.T) {
		err := WithShardContext(nil, 1, 100)
		if err != nil {
			t.Error("expected nil error")
		}
	})
}

func TestWithTimeoutContext(t *testing.T) {
	t.Run("add timeout context", func(t *testing.T) {
		baseErr := &dberrors.QBError{
			Code: dberrors.ErrTimeout,
			Op:   "service.Get",
			Err:  stderrors.New("operation timed out"),
		}

		err := WithTimeoutContext(baseErr, 5000, 5100)

		var dErr *dberrors.QBError
		if !dberrors.As(err, &dErr) {
			t.Fatal("expected QBError")
		}

		timeoutMs, ok := dErr.GetContext("timeout_ms")
		if !ok || timeoutMs != uint32(5000) {
			t.Errorf("expected timeout_ms 5000, got %v", timeoutMs)
		}

		elapsedMs, ok := dErr.GetContext("elapsed_ms")
		if !ok || elapsedMs != int64(5100) {
			t.Errorf("expected elapsed_ms 5100, got %v", elapsedMs)
		}
	})

	t.Run("wraps plain error", func(t *testing.T) {
		err := WithTimeoutContext(stderrors.New("plain error"), 3000, 5000)
		require.NotNil(t, err)
		assert.Contains(t, err.Error(), "plain error")
	})

	t.Run("nil error", func(t *testing.T) {
		err := WithTimeoutContext(nil, 5000, 5100)
		if err != nil {
			t.Error("expected nil error")
		}
	})
}

// =============================================================================
// Error Message Tests
// =============================================================================

func TestErrorMessages(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		shouldEmpty bool
	}{
		{
			name:        "not found",
			err:         ErrNotFound,
			shouldEmpty: false,
		},
		{
			name: "wrapped error",
			err: WrapError(
				stderrors.New("base error"),
				dberrors.ErrStorage,
				"service.Put",
				"storage write failed",
			),
			shouldEmpty: false,
		},
		{
			name: "with context",
			err: WithEntityContext(
				ErrNotFound,
				"user",
				123,
				"service.Get",
			),
			shouldEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errMsg := tt.err.Error()
			isEmpty := errMsg == ""

			if isEmpty != tt.shouldEmpty {
				t.Errorf("expected empty=%v, got message: %q", tt.shouldEmpty, errMsg)
			}
		})
	}
}

func TestErrorCodeToGRPCCode_Comprehensive(t *testing.T) {
	tests := []struct {
		name         string
		errorCode    dberrors.ErrorCode
		expectedCode codes.Code
	}{
		{"Unknown", dberrors.ErrUnknown, codes.Unknown},
		{"NotFound", dberrors.ErrNotFound, codes.NotFound},
		{"Conflict", dberrors.ErrConflict, codes.AlreadyExists},
		{"AlreadyExists", dberrors.ErrAlreadyExists, codes.AlreadyExists},
		{"Timeout", dberrors.ErrTimeout, codes.DeadlineExceeded},
		{"DeadlineExceeded", dberrors.ErrDeadlineExceeded, codes.DeadlineExceeded},
		{"Internal", dberrors.ErrInternal, codes.Internal},
		{"InvalidInput", dberrors.ErrInvalidInput, codes.InvalidArgument},
		{"Validation", dberrors.ErrValidation, codes.InvalidArgument},
		{"Unauthorized", dberrors.ErrUnauthorized, codes.Unauthenticated},
		{"Forbidden", dberrors.ErrForbidden, codes.PermissionDenied},
		{"NotAllowed", dberrors.ErrNotAllowed, codes.PermissionDenied},
		{"Unavailable", dberrors.ErrUnavailable, codes.Unavailable},
		{"LeaderNotAvailable", dberrors.ErrLeaderNotAvailable, codes.Unavailable},
		{"ClusterNotReady", dberrors.ErrClusterNotReady, codes.Unavailable},
		{"Storage", dberrors.ErrStorage, codes.Internal},
		{"Raft", dberrors.ErrRaft, codes.Internal},
		{"Encryption", dberrors.ErrEncryption, codes.Internal},
		{"Decryption", dberrors.ErrDecryption, codes.Internal},
		{"Configuration", dberrors.ErrConfiguration, codes.FailedPrecondition},
		{"Canceled", dberrors.ErrCanceled, codes.Canceled},
		{"ResourceExhausted", dberrors.ErrResourceExhausted, codes.ResourceExhausted},
		{"RateLimited", dberrors.ErrRateLimited, codes.ResourceExhausted},
		{"PreconditionFailed", dberrors.ErrPreconditionFailed, codes.FailedPrecondition},
		{"Aborted", dberrors.ErrAborted, codes.Aborted},
		{"OutOfRange", dberrors.ErrOutOfRange, codes.OutOfRange},
		{"Unimplemented", dberrors.ErrUnimplemented, codes.Unimplemented},
		{"NotImplemented", dberrors.ErrNotImplemented, codes.Unimplemented},
		{"DataLoss", dberrors.ErrDataLoss, codes.DataLoss},
		{"Snapshot", dberrors.ErrSnapshot, codes.Internal},
		{"Replication", dberrors.ErrReplication, codes.Internal},
		{"QuorumLost", dberrors.ErrQuorumLost, codes.Unavailable},
		{"SplitBrain", dberrors.ErrSplitBrain, codes.Unavailable},
		{"UnknownCode", dberrors.ErrorCode(9999), codes.Internal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := errorCodeToGRPCCode(tt.errorCode)
			assert.Equal(t, tt.expectedCode, result)
		})
	}
}

func TestGrpcCodeToErrorCode_Comprehensive(t *testing.T) {
	tests := []struct {
		name          string
		grpcCode      codes.Code
		expectedError dberrors.ErrorCode
	}{
		{"OK", codes.OK, dberrors.ErrUnknown},
		{"Canceled", codes.Canceled, dberrors.ErrCanceled},
		{"Unknown", codes.Unknown, dberrors.ErrUnknown},
		{"InvalidArgument", codes.InvalidArgument, dberrors.ErrInvalidInput},
		{"DeadlineExceeded", codes.DeadlineExceeded, dberrors.ErrDeadlineExceeded},
		{"NotFound", codes.NotFound, dberrors.ErrNotFound},
		{"AlreadyExists", codes.AlreadyExists, dberrors.ErrAlreadyExists},
		{"PermissionDenied", codes.PermissionDenied, dberrors.ErrForbidden},
		{"ResourceExhausted", codes.ResourceExhausted, dberrors.ErrResourceExhausted},
		{"FailedPrecondition", codes.FailedPrecondition, dberrors.ErrPreconditionFailed},
		{"Aborted", codes.Aborted, dberrors.ErrAborted},
		{"OutOfRange", codes.OutOfRange, dberrors.ErrOutOfRange},
		{"Unimplemented", codes.Unimplemented, dberrors.ErrUnimplemented},
		{"Internal", codes.Internal, dberrors.ErrInternal},
		{"Unavailable", codes.Unavailable, dberrors.ErrUnavailable},
		{"DataLoss", codes.DataLoss, dberrors.ErrDataLoss},
		{"Unauthenticated", codes.Unauthenticated, dberrors.ErrUnauthorized},
		{"UnknownCode", codes.Code(99), dberrors.ErrInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := grpcCodeToErrorCode(tt.grpcCode)
			assert.Equal(t, tt.expectedError, result)
		})
	}
}
