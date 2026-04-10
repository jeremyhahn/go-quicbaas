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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// convertKMSError Tests
// =============================================================================

func TestConvertKMSError_AllCases(t *testing.T) {
	t.Run("nil_error_returns_nil", func(t *testing.T) {
		result := convertKMSError(nil)
		assert.Nil(t, result)
	})

	t.Run("plain_error_returns_internal", func(t *testing.T) {
		err := stderrors.New("some unknown error")
		result := convertKMSError(err)
		assert.Equal(t, codes.Internal, status.Code(result))
	})

	t.Run("dragon_error_not_found", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrNotFound,
			Op:   "GetKey",
			Err:  stderrors.New("key not found"),
		}
		result := convertKMSError(err)
		assert.Equal(t, codes.NotFound, status.Code(result))
	})

	t.Run("dragon_error_already_exists", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrAlreadyExists,
			Op:   "CreateKey",
			Err:  stderrors.New("key already exists"),
		}
		result := convertKMSError(err)
		assert.Equal(t, codes.AlreadyExists, status.Code(result))
	})

	t.Run("dragon_error_invalid_input", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "GenerateKey",
			Err:  stderrors.New("invalid key spec"),
		}
		result := convertKMSError(err)
		assert.Equal(t, codes.InvalidArgument, status.Code(result))
	})

	t.Run("dragon_error_unauthorized", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrUnauthorized,
			Op:   "DecryptKey",
			Err:  stderrors.New("unauthorized"),
		}
		result := convertKMSError(err)
		assert.Equal(t, codes.Unauthenticated, status.Code(result))
	})

	t.Run("dragon_error_forbidden", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrForbidden,
			Op:   "DeleteKey",
			Err:  stderrors.New("forbidden"),
		}
		result := convertKMSError(err)
		assert.Equal(t, codes.PermissionDenied, status.Code(result))
	})

	t.Run("dragon_error_timeout", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "RotateKey",
			Err:  stderrors.New("timeout"),
		}
		result := convertKMSError(err)
		assert.Equal(t, codes.DeadlineExceeded, status.Code(result))
	})

	t.Run("dragon_error_resource_exhausted", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrResourceExhausted,
			Op:   "ImportKey",
			Err:  stderrors.New("too many keys"),
		}
		result := convertKMSError(err)
		assert.Equal(t, codes.ResourceExhausted, status.Code(result))
	})

	t.Run("dragon_error_unavailable", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrUnavailable,
			Op:   "GetKey",
			Err:  stderrors.New("service unavailable"),
		}
		result := convertKMSError(err)
		assert.Equal(t, codes.Unavailable, status.Code(result))
	})

	t.Run("dragon_error_internal", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "GenerateKey",
			Err:  stderrors.New("internal error"),
		}
		result := convertKMSError(err)
		assert.Equal(t, codes.Internal, status.Code(result))
	})

	t.Run("dragon_error_unknown_code_returns_internal", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrorCode(9999),
			Op:   "Operation",
			Err:  stderrors.New("unknown error"),
		}
		result := convertKMSError(err)
		assert.Equal(t, codes.Internal, status.Code(result))
	})
}

// =============================================================================
// Additional KMS Error Edge Cases
// =============================================================================

func TestConvertKMSError_EdgeCases(t *testing.T) {
	t.Run("dragon_error_with_nil_inner_error", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrNotFound,
			Op:   "GetKey",
			Err:  nil,
		}
		result := convertKMSError(err)
		assert.Equal(t, codes.NotFound, status.Code(result))
	})

	t.Run("dragon_error_with_empty_op", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "",
			Err:  stderrors.New("invalid"),
		}
		result := convertKMSError(err)
		assert.Equal(t, codes.InvalidArgument, status.Code(result))
	})

	t.Run("wrapped_dragon_error", func(t *testing.T) {
		innerErr := &errors.DragonError{
			Code: errors.ErrNotFound,
			Op:   "GetKey",
			Err:  stderrors.New("key not found"),
		}
		wrappedErr := errors.Wrapf(innerErr, errors.ErrInternal, "WrapOp", "wrapped message")
		result := convertKMSError(wrappedErr)
		// The wrapped error should use its own code (ErrInternal)
		assert.Equal(t, codes.Internal, status.Code(result))
	})
}
