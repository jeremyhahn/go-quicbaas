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

package messaging

import (
	"errors"
	"strings"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestChannelNotFoundError(t *testing.T) {
	t.Parallel()

	t.Run("error message contains channel name", func(t *testing.T) {
		t.Parallel()
		err := &ChannelNotFoundError{Channel: "test-channel"}
		expected := "push channel not found: test-channel"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("NewChannelNotFoundError creates QBError", func(t *testing.T) {
		t.Parallel()
		err := NewChannelNotFoundError("test.op", "my-channel")
		if err == nil {
			t.Fatal("expected non-nil error")
		}
		if err.Code != ErrCodePushChannelNotFound {
			t.Errorf("expected code %v, got %v", ErrCodePushChannelNotFound, err.Code)
		}
		if err.Op != "test.op" {
			t.Errorf("expected op %q, got %q", "test.op", err.Op)
		}
		if channel, ok := err.Context["channel"]; !ok || channel != "my-channel" {
			t.Errorf("expected context channel 'my-channel', got %v", channel)
		}
	})

	t.Run("IsChannelNotFound returns true for ChannelNotFoundError", func(t *testing.T) {
		t.Parallel()
		err := &ChannelNotFoundError{Channel: "test"}
		if !IsChannelNotFound(err) {
			t.Error("expected IsChannelNotFound to return true")
		}
	})

	t.Run("IsChannelNotFound returns true for QBError with correct code", func(t *testing.T) {
		t.Parallel()
		err := NewChannelNotFoundError("test", "channel")
		if !IsChannelNotFound(err) {
			t.Error("expected IsChannelNotFound to return true for QBError")
		}
	})

	t.Run("IsChannelNotFound returns true for sentinel error", func(t *testing.T) {
		t.Parallel()
		if !IsChannelNotFound(ErrChannelNotFound) {
			t.Error("expected IsChannelNotFound to return true for sentinel error")
		}
	})

	t.Run("IsChannelNotFound returns false for nil", func(t *testing.T) {
		t.Parallel()
		if IsChannelNotFound(nil) {
			t.Error("expected IsChannelNotFound to return false for nil")
		}
	})

	t.Run("IsChannelNotFound returns false for other errors", func(t *testing.T) {
		t.Parallel()
		if IsChannelNotFound(&dberrors.MessageError{Message: "random error"}) {
			t.Error("expected IsChannelNotFound to return false for other errors")
		}
	})
}

func TestChannelExistsError(t *testing.T) {
	t.Parallel()

	t.Run("error message contains channel name", func(t *testing.T) {
		t.Parallel()
		err := &ChannelExistsError{Channel: "existing-channel"}
		expected := "push channel already exists: existing-channel"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("NewChannelExistsError creates QBError", func(t *testing.T) {
		t.Parallel()
		err := NewChannelExistsError("test.op", "my-channel")
		if err.Code != ErrCodePushChannelExists {
			t.Errorf("expected code %v, got %v", ErrCodePushChannelExists, err.Code)
		}
	})

	t.Run("IsChannelExists returns true for ChannelExistsError", func(t *testing.T) {
		t.Parallel()
		err := &ChannelExistsError{Channel: "test"}
		if !IsChannelExists(err) {
			t.Error("expected IsChannelExists to return true")
		}
	})

	t.Run("IsChannelExists returns false for nil", func(t *testing.T) {
		t.Parallel()
		if IsChannelExists(nil) {
			t.Error("expected IsChannelExists to return false for nil")
		}
	})
}

func TestHubBufferFullError(t *testing.T) {
	t.Parallel()

	t.Run("error message contains channel", func(t *testing.T) {
		t.Parallel()
		err := &HubBufferFullError{Channel: "busy-channel", BufferSize: 256}
		expected := "push hub buffer full for channel: busy-channel"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("NewHubBufferFullError creates retryable QBError", func(t *testing.T) {
		t.Parallel()
		err := NewHubBufferFullError("test.op", "channel", 512)
		if err.Code != ErrCodePushHubBufferFull {
			t.Errorf("expected code %v, got %v", ErrCodePushHubBufferFull, err.Code)
		}
		if !err.Retry {
			t.Error("expected Retry to be true for buffer full error")
		}
		if bs, ok := err.Context["buffer_size"]; !ok || bs != 512 {
			t.Errorf("expected buffer_size context, got %v", bs)
		}
	})

	t.Run("IsHubBufferFull returns true for HubBufferFullError", func(t *testing.T) {
		t.Parallel()
		err := &HubBufferFullError{Channel: "test", BufferSize: 100}
		if !IsHubBufferFull(err) {
			t.Error("expected IsHubBufferFull to return true")
		}
	})

	t.Run("IsHubBufferFull returns false for nil", func(t *testing.T) {
		t.Parallel()
		if IsHubBufferFull(nil) {
			t.Error("expected IsHubBufferFull to return false for nil")
		}
	})
}

func TestClientDisconnectedError(t *testing.T) {
	t.Parallel()

	t.Run("error message contains client ID", func(t *testing.T) {
		t.Parallel()
		err := &ClientDisconnectedError{ClientID: "client-123"}
		expected := "push client disconnected: client-123"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("IsClientDisconnected returns true for ClientDisconnectedError", func(t *testing.T) {
		t.Parallel()
		err := &ClientDisconnectedError{ClientID: "test"}
		if !IsClientDisconnected(err) {
			t.Error("expected IsClientDisconnected to return true")
		}
	})

	t.Run("IsClientDisconnected returns false for nil", func(t *testing.T) {
		t.Parallel()
		if IsClientDisconnected(nil) {
			t.Error("expected IsClientDisconnected to return false for nil")
		}
	})
}

func TestClientSlowError(t *testing.T) {
	t.Parallel()

	t.Run("error message contains client ID", func(t *testing.T) {
		t.Parallel()
		err := &ClientSlowError{ClientID: "slow-client", DroppedCount: 5, BufferCapacity: 100}
		expected := "push client too slow: slow-client"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("IsClientSlow returns true for ClientSlowError", func(t *testing.T) {
		t.Parallel()
		err := &ClientSlowError{ClientID: "test"}
		if !IsClientSlow(err) {
			t.Error("expected IsClientSlow to return true")
		}
	})

	t.Run("IsClientSlow returns false for nil", func(t *testing.T) {
		t.Parallel()
		if IsClientSlow(nil) {
			t.Error("expected IsClientSlow to return false for nil")
		}
	})
}

func TestInvalidCodecError(t *testing.T) {
	t.Parallel()

	t.Run("error message contains codec name", func(t *testing.T) {
		t.Parallel()
		err := &InvalidCodecError{Codec: "unknown-codec"}
		expected := "invalid push codec: unknown-codec"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("IsInvalidCodec returns true for InvalidCodecError", func(t *testing.T) {
		t.Parallel()
		err := &InvalidCodecError{Codec: "test"}
		if !IsInvalidCodec(err) {
			t.Error("expected IsInvalidCodec to return true")
		}
	})

	t.Run("IsInvalidCodec returns false for nil", func(t *testing.T) {
		t.Parallel()
		if IsInvalidCodec(nil) {
			t.Error("expected IsInvalidCodec to return false for nil")
		}
	})
}

func TestSubscriptionNotFoundError(t *testing.T) {
	t.Parallel()

	t.Run("error message contains subscription ID", func(t *testing.T) {
		t.Parallel()
		err := &SubscriptionNotFoundError{SubscriptionID: "sub-123"}
		expected := "push subscription not found: sub-123"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("IsSubscriptionNotFound returns true for SubscriptionNotFoundError", func(t *testing.T) {
		t.Parallel()
		err := &SubscriptionNotFoundError{SubscriptionID: "test"}
		if !IsSubscriptionNotFound(err) {
			t.Error("expected IsSubscriptionNotFound to return true")
		}
	})

	t.Run("IsSubscriptionNotFound returns false for nil", func(t *testing.T) {
		t.Parallel()
		if IsSubscriptionNotFound(nil) {
			t.Error("expected IsSubscriptionNotFound to return false for nil")
		}
	})
}

func TestDedupKeyExistsError(t *testing.T) {
	t.Parallel()

	t.Run("error message contains key", func(t *testing.T) {
		t.Parallel()
		err := &DedupKeyExistsError{Key: "hash-abc123", FirstSeen: "2025-01-01", Count: 5}
		expected := "push dedup key exists: hash-abc123"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("IsDedupKeyExists returns true for DedupKeyExistsError", func(t *testing.T) {
		t.Parallel()
		err := &DedupKeyExistsError{Key: "test"}
		if !IsDedupKeyExists(err) {
			t.Error("expected IsDedupKeyExists to return true")
		}
	})

	t.Run("IsDedupKeyExists returns false for nil", func(t *testing.T) {
		t.Parallel()
		if IsDedupKeyExists(nil) {
			t.Error("expected IsDedupKeyExists to return false for nil")
		}
	})
}

func TestServiceNotRunningError(t *testing.T) {
	t.Parallel()

	t.Run("error message contains operation", func(t *testing.T) {
		t.Parallel()
		err := &ServiceNotRunningError{Operation: "publish"}
		expected := "push service not running, cannot perform: publish"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("IsServiceNotRunning returns true for ServiceNotRunningError", func(t *testing.T) {
		t.Parallel()
		err := &ServiceNotRunningError{Operation: "test"}
		if !IsServiceNotRunning(err) {
			t.Error("expected IsServiceNotRunning to return true")
		}
	})

	t.Run("IsServiceNotRunning returns false for nil", func(t *testing.T) {
		t.Parallel()
		if IsServiceNotRunning(nil) {
			t.Error("expected IsServiceNotRunning to return false for nil")
		}
	})
}

func TestInvalidChannelNameError(t *testing.T) {
	t.Parallel()

	t.Run("error message contains name and reason", func(t *testing.T) {
		t.Parallel()
		err := &InvalidChannelNameError{Name: "bad@channel", Reason: "contains invalid characters"}
		expected := "invalid push channel name 'bad@channel': contains invalid characters"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("IsInvalidChannelName returns true for InvalidChannelNameError", func(t *testing.T) {
		t.Parallel()
		err := &InvalidChannelNameError{Name: "test", Reason: "reason"}
		if !IsInvalidChannelName(err) {
			t.Error("expected IsInvalidChannelName to return true")
		}
	})

	t.Run("IsInvalidChannelName returns false for nil", func(t *testing.T) {
		t.Parallel()
		if IsInvalidChannelName(nil) {
			t.Error("expected IsInvalidChannelName to return false for nil")
		}
	})
}

func TestInvalidMessageIDError(t *testing.T) {
	t.Parallel()

	t.Run("error message contains ID and reason", func(t *testing.T) {
		t.Parallel()
		err := &InvalidMessageIDError{ID: "invalid-id", Reason: "too long"}
		expected := "invalid push message ID 'invalid-id': too long"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("IsInvalidMessageID returns true for InvalidMessageIDError", func(t *testing.T) {
		t.Parallel()
		err := &InvalidMessageIDError{ID: "test", Reason: "reason"}
		if !IsInvalidMessageID(err) {
			t.Error("expected IsInvalidMessageID to return true")
		}
	})

	t.Run("IsInvalidMessageID returns false for nil", func(t *testing.T) {
		t.Parallel()
		if IsInvalidMessageID(nil) {
			t.Error("expected IsInvalidMessageID to return false for nil")
		}
	})
}

func TestMaxSubscriptionsReachedError(t *testing.T) {
	t.Parallel()

	t.Run("error message contains channel", func(t *testing.T) {
		t.Parallel()
		err := &MaxSubscriptionsReachedError{Channel: "popular", Max: 1000}
		expected := "maximum subscriptions reached for channel: popular"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("IsMaxSubscriptionsReached returns true for MaxSubscriptionsReachedError", func(t *testing.T) {
		t.Parallel()
		err := &MaxSubscriptionsReachedError{Channel: "test", Max: 100}
		if !IsMaxSubscriptionsReached(err) {
			t.Error("expected IsMaxSubscriptionsReached to return true")
		}
	})

	t.Run("IsMaxSubscriptionsReached returns false for nil", func(t *testing.T) {
		t.Parallel()
		if IsMaxSubscriptionsReached(nil) {
			t.Error("expected IsMaxSubscriptionsReached to return false for nil")
		}
	})
}

func TestClientNotRegisteredError(t *testing.T) {
	t.Parallel()

	t.Run("error message contains client ID", func(t *testing.T) {
		t.Parallel()
		err := &ClientNotRegisteredError{ClientID: "unknown-client"}
		expected := "push client not registered: unknown-client"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("IsClientNotRegistered returns true for ClientNotRegisteredError", func(t *testing.T) {
		t.Parallel()
		err := &ClientNotRegisteredError{ClientID: "test"}
		if !IsClientNotRegistered(err) {
			t.Error("expected IsClientNotRegistered to return true")
		}
	})

	t.Run("IsClientNotRegistered returns false for nil", func(t *testing.T) {
		t.Parallel()
		if IsClientNotRegistered(nil) {
			t.Error("expected IsClientNotRegistered to return false for nil")
		}
	})

	t.Run("IsClientNotRegistered returns false for unrelated error", func(t *testing.T) {
		t.Parallel()
		if IsClientNotRegistered(&ChannelExistsError{}) {
			t.Error("expected IsClientNotRegistered to return false for unrelated error")
		}
	})
}

func TestSentinelErrors(t *testing.T) {
	t.Parallel()

	t.Run("sentinel errors are distinct", func(t *testing.T) {
		t.Parallel()
		sentinels := []error{
			ErrNilClient,
			ErrNilMessage,
			ErrEmptyChannelName,
			ErrChannelNotFound,
			ErrChannelExists,
			ErrClientNotRegistered,
			ErrMaxSubscribersReached,
			ErrHubOverloaded,
			ErrHubNotRunning,
			ErrClientDisconnected,
			ErrSlowConsumer,
			ErrInvalidCodec,
			ErrSubscriptionNotFound,
			ErrDedupKeyExists,
			ErrServiceNotRunning,
			ErrInvalidChannelName,
			ErrInvalidMessageID,
			ErrMaxSubscriptionsReached,
		}

		// Check all errors are non-nil
		for i, err := range sentinels {
			if err == nil {
				t.Errorf("sentinel error at index %d is nil", i)
			}
		}

		// Check no duplicates
		seen := make(map[string]bool)
		for _, err := range sentinels {
			msg := err.Error()
			if seen[msg] {
				t.Errorf("duplicate sentinel error message: %q", msg)
			}
			seen[msg] = true
		}
	})
}

func TestErrorWrapping(t *testing.T) {
	t.Parallel()

	t.Run("QBError wraps underlying error", func(t *testing.T) {
		t.Parallel()
		underlying := &ChannelNotFoundError{Channel: "test"}
		qbErr := NewChannelNotFoundError("test.op", "test")

		// Verify the underlying error is wrapped
		if !errors.As(qbErr, &underlying) {
			t.Error("expected QBError to wrap ChannelNotFoundError")
		}
	})

	t.Run("errors.Is works with wrapped errors", func(t *testing.T) {
		t.Parallel()
		err := NewChannelNotFoundError("test.op", "test")
		wrapped := &dberrors.WrappedMessageError{Message: "operation failed", Cause: err}

		if !IsChannelNotFound(wrapped) {
			t.Error("expected IsChannelNotFound to work with wrapped errors")
		}
	})
}

func TestAdditionalErrorConstructors(t *testing.T) {
	t.Parallel()

	t.Run("NewClientDisconnectedError creates QBError", func(t *testing.T) {
		t.Parallel()
		err := NewClientDisconnectedError("test.op", "client-123")
		if err == nil {
			t.Fatal("expected non-nil error")
		}
		if !strings.Contains(err.Error(), "client-123") {
			t.Errorf("error should contain client ID: %v", err)
		}
	})

	t.Run("NewClientSlowError creates QBError", func(t *testing.T) {
		t.Parallel()
		err := NewClientSlowError("test.op", "slow-client", 5, 256)
		if err == nil {
			t.Fatal("expected non-nil error")
		}
		if !strings.Contains(err.Error(), "slow-client") {
			t.Errorf("error should contain client ID: %v", err)
		}
	})

	t.Run("NewDedupKeyExistsError creates QBError", func(t *testing.T) {
		t.Parallel()
		err := NewDedupKeyExistsError("test.op", "dedup-key-123", "2026-01-01T00:00:00Z", 3)
		if err == nil {
			t.Fatal("expected non-nil error")
		}
		if !strings.Contains(err.Error(), "dedup-key-123") {
			t.Errorf("error should contain key: %v", err)
		}
	})

	t.Run("NewInvalidMessageIDError creates QBError", func(t *testing.T) {
		t.Parallel()
		err := NewInvalidMessageIDError("test.op", "bad-id", "invalid format")
		if err == nil {
			t.Fatal("expected non-nil error")
		}
		if !strings.Contains(err.Error(), "bad-id") {
			t.Errorf("error should contain message ID: %v", err)
		}
	})

	t.Run("NewMaxSubscriptionsReachedError creates QBError", func(t *testing.T) {
		t.Parallel()
		err := NewMaxSubscriptionsReachedError("test.op", "full-channel", 100)
		if err == nil {
			t.Fatal("expected non-nil error")
		}
		if !strings.Contains(err.Error(), "full-channel") {
			t.Errorf("error should contain channel name: %v", err)
		}
	})

	t.Run("NewClientNotRegisteredError creates QBError", func(t *testing.T) {
		t.Parallel()
		err := NewClientNotRegisteredError("test.op", "unregistered-client")
		if err == nil {
			t.Fatal("expected non-nil error")
		}
		if !strings.Contains(err.Error(), "unregistered-client") {
			t.Errorf("error should contain client ID: %v", err)
		}
	})
}

func TestIsErrorHelperFunctions(t *testing.T) {
	t.Parallel()

	t.Run("IsChannelExists", func(t *testing.T) {
		t.Parallel()
		// Direct error type
		if !IsChannelExists(&ChannelExistsError{Channel: "test"}) {
			t.Error("expected true for ChannelExistsError")
		}
		// QBError
		if !IsChannelExists(NewChannelExistsError("op", "test")) {
			t.Error("expected true for QBError")
		}
		// Sentinel error
		if !IsChannelExists(ErrChannelExists) {
			t.Error("expected true for sentinel")
		}
		// nil
		if IsChannelExists(nil) {
			t.Error("expected false for nil")
		}
		// Other error
		if IsChannelExists(&dberrors.MessageError{Message: "other"}) {
			t.Error("expected false for other error")
		}
	})

	t.Run("IsHubBufferFull", func(t *testing.T) {
		t.Parallel()
		if !IsHubBufferFull(&HubBufferFullError{BufferSize: 256}) {
			t.Error("expected true for HubBufferFullError")
		}
		if !IsHubBufferFull(NewHubBufferFullError("op", "test-channel", 256)) {
			t.Error("expected true for QBError")
		}
		if !IsHubBufferFull(ErrHubOverloaded) {
			t.Error("expected true for sentinel")
		}
		if IsHubBufferFull(nil) {
			t.Error("expected false for nil")
		}
	})

	t.Run("IsClientDisconnected", func(t *testing.T) {
		t.Parallel()
		if !IsClientDisconnected(&ClientDisconnectedError{ClientID: "test"}) {
			t.Error("expected true for ClientDisconnectedError")
		}
		if !IsClientDisconnected(NewClientDisconnectedError("op", "test")) {
			t.Error("expected true for QBError")
		}
		if IsClientDisconnected(nil) {
			t.Error("expected false for nil")
		}
		if IsClientDisconnected(&ChannelExistsError{}) {
			t.Error("expected false for unrelated error type")
		}
	})

	t.Run("IsClientSlow", func(t *testing.T) {
		t.Parallel()
		if !IsClientSlow(&ClientSlowError{ClientID: "test"}) {
			t.Error("expected true for ClientSlowError")
		}
		if !IsClientSlow(NewClientSlowError("op", "test", 5, 256)) {
			t.Error("expected true for QBError")
		}
		if IsClientSlow(nil) {
			t.Error("expected false for nil")
		}
		if IsClientSlow(&ChannelExistsError{}) {
			t.Error("expected false for unrelated error type")
		}
	})

	t.Run("IsInvalidCodec", func(t *testing.T) {
		t.Parallel()
		if !IsInvalidCodec(&InvalidCodecError{Codec: "bad"}) {
			t.Error("expected true for InvalidCodecError")
		}
		if !IsInvalidCodec(NewInvalidCodecError("op", "bad")) {
			t.Error("expected true for QBError")
		}
		if IsInvalidCodec(nil) {
			t.Error("expected false for nil")
		}
		if IsInvalidCodec(&ChannelExistsError{}) {
			t.Error("expected false for unrelated error type")
		}
	})

	t.Run("IsSubscriptionNotFound", func(t *testing.T) {
		t.Parallel()
		if !IsSubscriptionNotFound(&SubscriptionNotFoundError{SubscriptionID: "sub-1"}) {
			t.Error("expected true for SubscriptionNotFoundError")
		}
		if !IsSubscriptionNotFound(NewSubscriptionNotFoundError("op", "sub-1")) {
			t.Error("expected true for QBError")
		}
		if IsSubscriptionNotFound(nil) {
			t.Error("expected false for nil")
		}
		if IsSubscriptionNotFound(&ChannelExistsError{}) {
			t.Error("expected false for unrelated error type")
		}
	})

	t.Run("IsDedupKeyExists", func(t *testing.T) {
		t.Parallel()
		if !IsDedupKeyExists(&DedupKeyExistsError{Key: "key-1"}) {
			t.Error("expected true for DedupKeyExistsError")
		}
		if !IsDedupKeyExists(NewDedupKeyExistsError("op", "key-1", "2026-01-01", 1)) {
			t.Error("expected true for QBError")
		}
		if IsDedupKeyExists(nil) {
			t.Error("expected false for nil")
		}
		if IsDedupKeyExists(&ChannelExistsError{}) {
			t.Error("expected false for unrelated error type")
		}
	})

	t.Run("IsServiceNotRunning", func(t *testing.T) {
		t.Parallel()
		if !IsServiceNotRunning(&ServiceNotRunningError{}) {
			t.Error("expected true for ServiceNotRunningError")
		}
		if !IsServiceNotRunning(NewServiceNotRunningError("op", "publish")) {
			t.Error("expected true for QBError")
		}
		if !IsServiceNotRunning(ErrServiceNotRunning) {
			t.Error("expected true for sentinel")
		}
		if IsServiceNotRunning(nil) {
			t.Error("expected false for nil")
		}
	})

	t.Run("IsInvalidChannelName", func(t *testing.T) {
		t.Parallel()
		if !IsInvalidChannelName(&InvalidChannelNameError{Name: "bad"}) {
			t.Error("expected true for InvalidChannelNameError")
		}
		if !IsInvalidChannelName(NewInvalidChannelNameError("op", "bad", "reason")) {
			t.Error("expected true for QBError")
		}
		if !IsInvalidChannelName(ErrInvalidChannelName) {
			t.Error("expected true for sentinel")
		}
		if IsInvalidChannelName(nil) {
			t.Error("expected false for nil")
		}
	})

	t.Run("IsInvalidMessageID", func(t *testing.T) {
		t.Parallel()
		if !IsInvalidMessageID(&InvalidMessageIDError{ID: "bad"}) {
			t.Error("expected true for InvalidMessageIDError")
		}
		if !IsInvalidMessageID(NewInvalidMessageIDError("op", "bad", "reason")) {
			t.Error("expected true for QBError")
		}
		if IsInvalidMessageID(nil) {
			t.Error("expected false for nil")
		}
		if IsInvalidMessageID(&ChannelExistsError{}) {
			t.Error("expected false for unrelated error type")
		}
	})

	t.Run("IsMaxSubscriptionsReached", func(t *testing.T) {
		t.Parallel()
		if !IsMaxSubscriptionsReached(&MaxSubscriptionsReachedError{Channel: "ch", Max: 10}) {
			t.Error("expected true for MaxSubscriptionsReachedError")
		}
		if !IsMaxSubscriptionsReached(NewMaxSubscriptionsReachedError("op", "ch", 10)) {
			t.Error("expected true for QBError")
		}
		if !IsMaxSubscriptionsReached(ErrMaxSubscriptionsReached) {
			t.Error("expected true for sentinel")
		}
		if IsMaxSubscriptionsReached(nil) {
			t.Error("expected false for nil")
		}
	})
}

// TestIsErrorCheckers_BareQBErrorCodePath tests each Is* function with a
// bare QBError that has the correct code but NO inner typed error.
// This exercises the QBError code-check branch (de.Code == ErrCodePush*)
// which is not reached when using New*Error constructors because errors.As
// finds the inner typed error first.
func TestIsErrorCheckers_BareQBErrorCodePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		checker func(error) bool
		code    dberrors.ErrorCode
	}{
		{"IsChannelNotFound", IsChannelNotFound, ErrCodePushChannelNotFound},
		{"IsChannelExists", IsChannelExists, ErrCodePushChannelExists},
		{"IsHubBufferFull", IsHubBufferFull, ErrCodePushHubBufferFull},
		{"IsClientDisconnected", IsClientDisconnected, ErrCodePushClientDisconnected},
		{"IsClientSlow", IsClientSlow, ErrCodePushClientSlow},
		{"IsInvalidCodec", IsInvalidCodec, ErrCodePushInvalidCodec},
		{"IsSubscriptionNotFound", IsSubscriptionNotFound, ErrCodePushSubscriptionNotFound},
		{"IsDedupKeyExists", IsDedupKeyExists, ErrCodePushDedupKeyExists},
		{"IsServiceNotRunning", IsServiceNotRunning, ErrCodePushServiceNotRunning},
		{"IsInvalidChannelName", IsInvalidChannelName, ErrCodePushInvalidChannelName},
		{"IsInvalidMessageID", IsInvalidMessageID, ErrCodePushInvalidMessageID},
		{"IsMaxSubscriptionsReached", IsMaxSubscriptionsReached, ErrCodePushMaxSubscriptionsReached},
		{"IsClientNotRegistered", IsClientNotRegistered, ErrCodePushClientNotRegistered},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Create a QBError with the correct code but no inner typed error.
			// This forces the code-check branch to execute since errors.As won't
			// match the specific typed error first.
			bareErr := &dberrors.QBError{
				Code: tc.code,
				Op:   "test",
			}
			if !tc.checker(bareErr) {
				t.Errorf("expected %s to return true for bare QBError with code %d", tc.name, tc.code)
			}
		})
	}
}
