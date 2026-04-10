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
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// =============================================================================
// convertMessagingError Tests
// =============================================================================

func TestConvertMessagingError_AllCases(t *testing.T) {
	t.Run("nil_error_returns_nil", func(t *testing.T) {
		result := convertMessagingError(nil)
		assert.Nil(t, result)
	})

	t.Run("channel_not_found_error", func(t *testing.T) {
		err := &messaging.ChannelNotFoundError{Channel: "test-channel"}
		result := convertMessagingError(err)
		assert.Equal(t, codes.NotFound, status.Code(result))
	})

	t.Run("channel_exists_error", func(t *testing.T) {
		err := &messaging.ChannelExistsError{Channel: "test-channel"}
		result := convertMessagingError(err)
		assert.Equal(t, codes.AlreadyExists, status.Code(result))
	})

	t.Run("subscription_not_found_error", func(t *testing.T) {
		err := &messaging.SubscriptionNotFoundError{SubscriptionID: "sub-123"}
		result := convertMessagingError(err)
		assert.Equal(t, codes.NotFound, status.Code(result))
	})

	t.Run("invalid_channel_name_error", func(t *testing.T) {
		err := &messaging.InvalidChannelNameError{Name: "bad*name", Reason: "invalid chars"}
		result := convertMessagingError(err)
		assert.Equal(t, codes.InvalidArgument, status.Code(result))
	})

	t.Run("service_not_running_error", func(t *testing.T) {
		err := &messaging.ServiceNotRunningError{Operation: "subscribe"}
		result := convertMessagingError(err)
		assert.Equal(t, codes.Unavailable, status.Code(result))
	})

	t.Run("max_subscriptions_reached_error", func(t *testing.T) {
		err := &messaging.MaxSubscriptionsReachedError{Channel: "test-channel", Max: 100}
		result := convertMessagingError(err)
		assert.Equal(t, codes.ResourceExhausted, status.Code(result))
	})

	t.Run("hub_buffer_full_error", func(t *testing.T) {
		err := &messaging.HubBufferFullError{Channel: "test-channel", BufferSize: 1000}
		result := convertMessagingError(err)
		assert.Equal(t, codes.ResourceExhausted, status.Code(result))
	})

	t.Run("client_disconnected_error", func(t *testing.T) {
		err := &messaging.ClientDisconnectedError{ClientID: "client-123"}
		result := convertMessagingError(err)
		assert.Equal(t, codes.Aborted, status.Code(result))
	})

	t.Run("client_slow_error", func(t *testing.T) {
		err := &messaging.ClientSlowError{ClientID: "client-123", DroppedCount: 500}
		result := convertMessagingError(err)
		assert.Equal(t, codes.ResourceExhausted, status.Code(result))
	})

	t.Run("dragon_error_not_found", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrNotFound,
			Op:   "GetChannel",
			Err:  stderrors.New("not found"),
		}
		result := convertMessagingError(err)
		assert.Equal(t, codes.NotFound, status.Code(result))
	})

	t.Run("dragon_error_already_exists", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrAlreadyExists,
			Op:   "CreateChannel",
			Err:  stderrors.New("exists"),
		}
		result := convertMessagingError(err)
		assert.Equal(t, codes.AlreadyExists, status.Code(result))
	})

	t.Run("dragon_error_invalid_input", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "Subscribe",
			Err:  stderrors.New("invalid"),
		}
		result := convertMessagingError(err)
		assert.Equal(t, codes.InvalidArgument, status.Code(result))
	})

	t.Run("dragon_error_unauthorized", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrUnauthorized,
			Op:   "Subscribe",
			Err:  stderrors.New("unauthorized"),
		}
		result := convertMessagingError(err)
		assert.Equal(t, codes.Unauthenticated, status.Code(result))
	})

	t.Run("dragon_error_forbidden", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrForbidden,
			Op:   "DeleteChannel",
			Err:  stderrors.New("forbidden"),
		}
		result := convertMessagingError(err)
		assert.Equal(t, codes.PermissionDenied, status.Code(result))
	})

	t.Run("dragon_error_timeout", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "Publish",
			Err:  stderrors.New("timeout"),
		}
		result := convertMessagingError(err)
		assert.Equal(t, codes.DeadlineExceeded, status.Code(result))
	})

	t.Run("dragon_error_canceled", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrCanceled,
			Op:   "Subscribe",
			Err:  stderrors.New("canceled"),
		}
		result := convertMessagingError(err)
		assert.Equal(t, codes.Canceled, status.Code(result))
	})

	t.Run("dragon_error_unavailable", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrUnavailable,
			Op:   "Connect",
			Err:  stderrors.New("unavailable"),
		}
		result := convertMessagingError(err)
		assert.Equal(t, codes.Unavailable, status.Code(result))
	})

	t.Run("dragon_error_resource_exhausted", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrResourceExhausted,
			Op:   "Publish",
			Err:  stderrors.New("exhausted"),
		}
		result := convertMessagingError(err)
		assert.Equal(t, codes.ResourceExhausted, status.Code(result))
	})

	t.Run("dragon_error_internal", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "Process",
			Err:  stderrors.New("internal error"),
		}
		result := convertMessagingError(err)
		assert.Equal(t, codes.Internal, status.Code(result))
	})

	t.Run("dragon_error_unknown_code_returns_internal", func(t *testing.T) {
		err := &errors.DragonError{
			Code: errors.ErrorCode(9999),
			Op:   "Unknown",
			Err:  stderrors.New("unknown error code"),
		}
		result := convertMessagingError(err)
		assert.Equal(t, codes.Internal, status.Code(result))
	})

	t.Run("plain_error_returns_internal", func(t *testing.T) {
		err := stderrors.New("some unknown error")
		result := convertMessagingError(err)
		assert.Equal(t, codes.Internal, status.Code(result))
	})
}
