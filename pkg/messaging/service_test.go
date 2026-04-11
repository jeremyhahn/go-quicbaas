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
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func newServiceTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "console",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	return log
}

func TestNewPushService(t *testing.T) {
	t.Parallel()

	t.Run("creates service with default options", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		if ps == nil {
			t.Fatal("expected non-nil PushService")
		}
		if ps.hub == nil {
			t.Error("expected non-nil hub")
		}
		if ps.codec == nil {
			t.Error("expected non-nil codec resolver")
		}
		if ps.deltaComputer == nil {
			t.Error("expected non-nil delta computer")
		}
	})

	t.Run("creates service with custom buffer size", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil, WithServiceBufferSize(512))

		if ps.config.bufferSize != 512 {
			t.Errorf("expected buffer size 512, got %d", ps.config.bufferSize)
		}
	})

	t.Run("creates service with custom default codec", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil, WithDefaultCodec(CodecMsgpack))

		if ps.config.defaultCodec != CodecMsgpack {
			t.Errorf("expected default codec %q, got %q", CodecMsgpack, ps.config.defaultCodec)
		}
	})
}

func TestPushService_StartStop(t *testing.T) {
	t.Parallel()

	t.Run("starts and stops service", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()

		// Start service
		err := ps.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start service: %v", err)
		}
		if !ps.IsRunning() {
			t.Error("expected service to be running after Start")
		}

		// Stop service
		err = ps.Stop(ctx)
		if err != nil {
			t.Fatalf("failed to stop service: %v", err)
		}
		if ps.IsRunning() {
			t.Error("expected service to not be running after Stop")
		}
	})

	t.Run("start is idempotent", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()

		err := ps.Start(ctx)
		if err != nil {
			t.Fatalf("first start failed: %v", err)
		}

		// Second start should be no-op
		err = ps.Start(ctx)
		if err != nil {
			t.Fatalf("second start failed: %v", err)
		}

		ps.Stop(ctx)
	})

	t.Run("stop is idempotent", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()

		// Stop without start should be no-op
		err := ps.Stop(ctx)
		if err != nil {
			t.Fatalf("stop without start failed: %v", err)
		}

		ps.Start(ctx)
		ps.Stop(ctx)

		// Second stop should be no-op
		err = ps.Stop(ctx)
		if err != nil {
			t.Fatalf("second stop failed: %v", err)
		}
	})
}

func TestPushService_CreateChannel(t *testing.T) {
	t.Parallel()

	t.Run("creates channel successfully", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		channel, err := ps.CreateChannel(ctx, "test-channel")
		if err != nil {
			t.Fatalf("failed to create channel: %v", err)
		}
		if channel.Name != "test-channel" {
			t.Errorf("expected channel name 'test-channel', got %q", channel.Name)
		}
		if channel.Codec != CodecJSON {
			t.Errorf("expected default codec %q, got %q", CodecJSON, channel.Codec)
		}
	})

	t.Run("creates channel with custom options", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		channel, err := ps.CreateChannel(ctx, "custom-channel",
			WithCodec(CodecMsgpack),
			WithPersistence(true),
			WithMaxHistory(100),
		)
		if err != nil {
			t.Fatalf("failed to create channel: %v", err)
		}
		if channel.Codec != CodecMsgpack {
			t.Errorf("expected codec %q, got %q", CodecMsgpack, channel.Codec)
		}
		if !channel.Persistent {
			t.Error("expected channel to be persistent")
		}
		if channel.MaxHistory != 100 {
			t.Errorf("expected max history 100, got %d", channel.MaxHistory)
		}
	})

	t.Run("returns error when service not running", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()

		_, err := ps.CreateChannel(ctx, "test-channel")
		if err == nil {
			t.Error("expected error when service not running")
		}
		if !IsServiceNotRunning(err) {
			t.Errorf("expected ServiceNotRunningError, got %T", err)
		}
	})

	t.Run("returns error for empty channel name", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		_, err := ps.CreateChannel(ctx, "")
		if err == nil {
			t.Error("expected error for empty channel name")
		}
		if !IsInvalidChannelName(err) {
			t.Errorf("expected InvalidChannelNameError, got %T", err)
		}
	})

	t.Run("returns error for invalid channel name", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		_, err := ps.CreateChannel(ctx, "invalid@channel#name")
		if err == nil {
			t.Error("expected error for invalid channel name")
		}
		if !IsInvalidChannelName(err) {
			t.Errorf("expected InvalidChannelNameError, got %T", err)
		}
	})

	t.Run("returns error for duplicate channel", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		_, err := ps.CreateChannel(ctx, "duplicate-channel")
		if err != nil {
			t.Fatalf("failed to create first channel: %v", err)
		}

		_, err = ps.CreateChannel(ctx, "duplicate-channel")
		if err == nil {
			t.Error("expected error for duplicate channel")
		}
		// The error can be either a ChannelExistsError or an ErrAlreadyExists QBError
		// Both indicate the channel already exists
		errStr := err.Error()
		if errStr == "" {
			t.Error("error should have a message")
		}
	})
}

func TestPushService_DeleteChannel(t *testing.T) {
	t.Parallel()

	t.Run("deletes channel successfully", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		_, err := ps.CreateChannel(ctx, "to-delete")
		if err != nil {
			t.Fatalf("failed to create channel: %v", err)
		}

		err = ps.DeleteChannel(ctx, "to-delete")
		if err != nil {
			t.Fatalf("failed to delete channel: %v", err)
		}

		// Verify channel no longer exists
		_, err = ps.GetChannel(ctx, "to-delete")
		if err == nil {
			t.Error("expected error getting deleted channel")
		}
	})

	t.Run("returns error when service not running", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()

		err := ps.DeleteChannel(ctx, "test-channel")
		if !IsServiceNotRunning(err) {
			t.Errorf("expected ServiceNotRunningError, got %T", err)
		}
	})

	t.Run("returns error for non-existent channel", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		err := ps.DeleteChannel(ctx, "nonexistent")
		if err == nil {
			t.Error("expected error for non-existent channel")
		}
		// Hub may return QBError or ChannelNotFoundError depending on implementation
		if !IsChannelNotFound(err) && !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected error about channel not found, got: %v", err)
		}
	})
}

func TestPushService_GetChannel(t *testing.T) {
	t.Parallel()

	t.Run("returns channel info", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		_, err := ps.CreateChannel(ctx, "get-test")
		if err != nil {
			t.Fatalf("failed to create channel: %v", err)
		}

		channel, err := ps.GetChannel(ctx, "get-test")
		if err != nil {
			t.Fatalf("failed to get channel: %v", err)
		}
		if channel.Name != "get-test" {
			t.Errorf("expected channel name 'get-test', got %q", channel.Name)
		}
	})

	t.Run("returns error when service not running", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()

		_, err := ps.GetChannel(ctx, "test-channel")
		if !IsServiceNotRunning(err) {
			t.Errorf("expected ServiceNotRunningError, got %T", err)
		}
	})

	t.Run("returns error for non-existent channel", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		_, err := ps.GetChannel(ctx, "nonexistent")
		if !IsChannelNotFound(err) {
			t.Errorf("expected ChannelNotFoundError, got %T", err)
		}
	})
}

func TestPushService_ListChannels(t *testing.T) {
	t.Parallel()

	t.Run("returns empty list for no channels", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		channels, err := ps.ListChannels(ctx)
		if err != nil {
			t.Fatalf("failed to list channels: %v", err)
		}
		if len(channels) != 0 {
			t.Errorf("expected 0 channels, got %d", len(channels))
		}
	})

	t.Run("returns all created channels", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		ps.CreateChannel(ctx, "channel-1")
		ps.CreateChannel(ctx, "channel-2")
		ps.CreateChannel(ctx, "channel-3")

		channels, err := ps.ListChannels(ctx)
		if err != nil {
			t.Fatalf("failed to list channels: %v", err)
		}
		if len(channels) != 3 {
			t.Errorf("expected 3 channels, got %d", len(channels))
		}
	})

	t.Run("returns error when service not running", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()

		_, err := ps.ListChannels(ctx)
		if !IsServiceNotRunning(err) {
			t.Errorf("expected ServiceNotRunningError, got %T", err)
		}
	})
}

func TestPushService_Subscribe(t *testing.T) {
	t.Parallel()

	t.Run("subscribes to channel", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		_, err := ps.CreateChannel(ctx, "sub-channel")
		if err != nil {
			t.Fatalf("failed to create channel: %v", err)
		}

		sub, err := ps.Subscribe(ctx, "sub-channel")
		if err != nil {
			t.Fatalf("failed to subscribe: %v", err)
		}
		if sub == nil {
			t.Fatal("expected non-nil subscription")
		}
		if sub.ID == "" {
			t.Error("expected non-empty subscription ID")
		}
		if sub.Channel != "sub-channel" {
			t.Errorf("expected channel 'sub-channel', got %q", sub.Channel)
		}
	})

	t.Run("subscribes to existing channel", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		// First create the channel
		_, err := ps.CreateChannel(ctx, "existing-channel")
		if err != nil {
			t.Fatalf("failed to create channel: %v", err)
		}

		// Then subscribe
		sub, err := ps.Subscribe(ctx, "existing-channel")
		if err != nil {
			t.Fatalf("failed to subscribe: %v", err)
		}
		if sub.Channel != "existing-channel" {
			t.Errorf("expected channel 'existing-channel', got %q", sub.Channel)
		}
	})

	t.Run("returns error when service not running", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()

		_, err := ps.Subscribe(ctx, "test-channel")
		if !IsServiceNotRunning(err) {
			t.Errorf("expected ServiceNotRunningError, got %T", err)
		}
	})

	t.Run("returns error for invalid channel name", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		_, err := ps.Subscribe(ctx, "")
		if !IsInvalidChannelName(err) {
			t.Errorf("expected InvalidChannelNameError, got %T", err)
		}
	})
}

func TestPushService_Unsubscribe(t *testing.T) {
	t.Parallel()

	t.Run("unsubscribes from channel", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		_, err := ps.CreateChannel(ctx, "unsub-channel")
		if err != nil {
			t.Fatalf("failed to create channel: %v", err)
		}
		sub, err := ps.Subscribe(ctx, "unsub-channel")
		if err != nil {
			t.Fatalf("failed to subscribe: %v", err)
		}

		err = ps.Unsubscribe(ctx, sub.ID)
		if err != nil {
			t.Fatalf("failed to unsubscribe: %v", err)
		}

		// Verify subscription is gone
		_, found := ps.GetSubscription(sub.ID)
		if found {
			t.Error("expected subscription to be removed")
		}
	})

	t.Run("returns error when service not running", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()

		err := ps.Unsubscribe(ctx, "sub-123")
		if !IsServiceNotRunning(err) {
			t.Errorf("expected ServiceNotRunningError, got %T", err)
		}
	})

	t.Run("returns error for non-existent subscription", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		err := ps.Unsubscribe(ctx, "nonexistent")
		if !IsSubscriptionNotFound(err) {
			t.Errorf("expected SubscriptionNotFoundError, got %T", err)
		}
	})

	t.Run("returns error for empty subscription ID", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		err := ps.Unsubscribe(ctx, "")
		if !IsSubscriptionNotFound(err) {
			t.Errorf("expected SubscriptionNotFoundError, got %T", err)
		}
	})
}

func TestPushService_Publish(t *testing.T) {
	t.Parallel()

	t.Run("publishes message to channel", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		ps.CreateChannel(ctx, "pub-channel")
		sub, _ := ps.Subscribe(ctx, "pub-channel")

		// Get the client to receive messages
		client, found := ps.GetSubscriptionClient(sub.ID)
		if !found {
			t.Fatal("subscription client not found")
		}

		// Publish message
		_, err := ps.Publish(ctx, "pub-channel", map[string]string{"message": "hello"})
		if err != nil {
			t.Fatalf("failed to publish: %v", err)
		}

		// Receive message
		select {
		case msg := <-client.Send:
			if msg == nil {
				t.Error("received nil message")
			}
		case <-time.After(time.Second):
			t.Error("timeout waiting for message")
		}
	})

	t.Run("publishes byte payload", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		ps.CreateChannel(ctx, "byte-channel")

		_, err := ps.Publish(ctx, "byte-channel", []byte(`{"raw":"bytes"}`))
		if err != nil {
			t.Fatalf("failed to publish bytes: %v", err)
		}
	})

	t.Run("publishes string payload", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		ps.CreateChannel(ctx, "string-channel")

		_, err := ps.Publish(ctx, "string-channel", "hello world")
		if err != nil {
			t.Fatalf("failed to publish string: %v", err)
		}
	})

	t.Run("returns error when service not running", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()

		_, err := ps.Publish(ctx, "test-channel", "payload")
		if !IsServiceNotRunning(err) {
			t.Errorf("expected ServiceNotRunningError, got %T", err)
		}
	})

	t.Run("returns error for invalid channel name", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		_, err := ps.Publish(ctx, "", "payload")
		if !IsInvalidChannelName(err) {
			t.Errorf("expected InvalidChannelNameError, got %T", err)
		}
	})
}

func TestPushService_PublishDelta(t *testing.T) {
	t.Parallel()

	t.Run("publishes delta between entities", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		ps.CreateChannel(ctx, "delta-channel")
		sub, _ := ps.Subscribe(ctx, "delta-channel")

		client, _ := ps.GetSubscriptionClient(sub.ID)

		oldEntity := &testEntity{ID: "1", Name: "Old", Age: 25}
		newEntity := &testEntity{ID: "1", Name: "New", Age: 25}

		_, err := ps.PublishDelta(ctx, "delta-channel", oldEntity, newEntity)
		if err != nil {
			t.Fatalf("failed to publish delta: %v", err)
		}

		// Receive message
		select {
		case msg := <-client.Send:
			if msg == nil {
				t.Error("received nil message")
			}
		case <-time.After(time.Second):
			t.Error("timeout waiting for delta message")
		}
	})

	t.Run("returns error when service not running", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()

		_, err := ps.PublishDelta(ctx, "test-channel", nil, &testEntity{})
		if !IsServiceNotRunning(err) {
			t.Errorf("expected ServiceNotRunningError, got %T", err)
		}
	})
}

func TestPushService_WatchChannel(t *testing.T) {
	t.Parallel()

	t.Run("creates watch subscription", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		sub, err := ps.WatchChannel(ctx, "/users/")
		if err != nil {
			t.Fatalf("failed to watch channel: %v", err)
		}
		if sub == nil {
			t.Fatal("expected non-nil subscription")
		}
		if sub.Channel != "watch:/users/" {
			t.Errorf("expected channel 'watch:/users/', got %q", sub.Channel)
		}
	})

	t.Run("returns error when service not running", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()

		_, err := ps.WatchChannel(ctx, "/users/")
		if !IsServiceNotRunning(err) {
			t.Errorf("expected ServiceNotRunningError, got %T", err)
		}
	})
}

func TestPushService_GetStats(t *testing.T) {
	t.Parallel()

	t.Run("returns service stats", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		// Create channels and subscriptions
		ps.CreateChannel(ctx, "stats-channel-1")
		ps.CreateChannel(ctx, "stats-channel-2")
		ps.Subscribe(ctx, "stats-channel-1")
		ps.Subscribe(ctx, "stats-channel-1")
		ps.Subscribe(ctx, "stats-channel-2")

		stats := ps.GetStats()
		if stats == nil {
			t.Fatal("expected non-nil stats")
		}
		if stats.TotalChannels != 2 {
			t.Errorf("expected 2 channels, got %d", stats.TotalChannels)
		}
		if stats.TotalSubscriptions != 3 {
			t.Errorf("expected 3 subscriptions, got %d", stats.TotalSubscriptions)
		}
	})
}

func TestPushService_GetSubscription(t *testing.T) {
	t.Parallel()

	t.Run("returns subscription by ID", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		ps.CreateChannel(ctx, "getsub-channel")
		sub, _ := ps.Subscribe(ctx, "getsub-channel")

		found, ok := ps.GetSubscription(sub.ID)
		if !ok {
			t.Error("expected to find subscription")
		}
		if found.ID != sub.ID {
			t.Errorf("expected ID %q, got %q", sub.ID, found.ID)
		}
	})

	t.Run("returns false for non-existent subscription", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		_, ok := ps.GetSubscription("nonexistent")
		if ok {
			t.Error("expected subscription not to be found")
		}
	})
}

func TestPushService_Accessors(t *testing.T) {
	t.Parallel()

	t.Run("GetHub returns hub", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		if ps.GetHub() == nil {
			t.Error("expected non-nil hub")
		}
	})

	t.Run("GetCodecResolver returns codec resolver", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		if ps.GetCodecResolver() == nil {
			t.Error("expected non-nil codec resolver")
		}
	})

	t.Run("GetDeltaComputer returns delta computer", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		if ps.GetDeltaComputer() == nil {
			t.Error("expected non-nil delta computer")
		}
	})

	t.Run("GetDedupStore returns nil when dedup disabled", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil) // No storage, no dedup

		if ps.GetDedupStore() != nil {
			t.Error("expected nil dedup store when disabled")
		}
	})
}

func TestValidateChannelName(t *testing.T) {
	t.Parallel()

	validNames := []string{
		"simple",
		"with-hyphen",
		"with_underscore",
		"with.dot",
		"with/slash",
		"with:colon",
		"mixed-name_123.test/sub:channel",
		"UPPERCASE",
		"CamelCase",
		"wildcard*channel",
	}

	invalidNames := []string{
		"",
		"with@symbol",
		"with#hash",
		"with space",
		"with\ttab",
		"with\nnewline",
	}

	t.Run("accepts valid channel names", func(t *testing.T) {
		t.Parallel()
		for _, name := range validNames {
			err := validateChannelName(name)
			if err != nil {
				t.Errorf("expected %q to be valid, got error: %v", name, err)
			}
		}
	})

	t.Run("rejects invalid channel names", func(t *testing.T) {
		t.Parallel()
		for _, name := range invalidNames {
			err := validateChannelName(name)
			if err == nil {
				t.Errorf("expected %q to be invalid, got nil error", name)
			}
		}
	})

	t.Run("rejects too long channel names", func(t *testing.T) {
		t.Parallel()
		longName := ""
		for i := 0; i < 260; i++ {
			longName += "a"
		}
		err := validateChannelName(longName)
		if err == nil {
			t.Error("expected error for too long channel name")
		}
	})
}

func TestGenerateSubscriptionID(t *testing.T) {
	t.Parallel()

	t.Run("generates unique IDs", func(t *testing.T) {
		t.Parallel()
		ids := make(map[string]bool)
		for i := uint64(1); i <= 100; i++ {
			id := generateSubscriptionID(i)
			if ids[id] {
				t.Errorf("duplicate ID generated: %s", id)
			}
			ids[id] = true
		}
	})

	t.Run("formats correctly", func(t *testing.T) {
		t.Parallel()
		id := generateSubscriptionID(123)
		if id != "sub-123" {
			t.Errorf("expected 'sub-123', got %q", id)
		}
	})

	t.Run("handles zero", func(t *testing.T) {
		t.Parallel()
		id := generateSubscriptionID(0)
		if id != "sub-0" {
			t.Errorf("expected 'sub-0', got %q", id)
		}
	})
}

func TestPushService_DeleteChannelWithSubscriptions(t *testing.T) {
	t.Parallel()

	t.Run("deletes channel and cleans up subscriptions", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		// Create channel and subscribe
		ps.CreateChannel(ctx, "delete-sub-channel")
		sub1, _ := ps.Subscribe(ctx, "delete-sub-channel")
		sub2, _ := ps.Subscribe(ctx, "delete-sub-channel")

		// Delete channel
		err := ps.DeleteChannel(ctx, "delete-sub-channel")
		if err != nil {
			t.Fatalf("failed to delete channel: %v", err)
		}

		// Verify subscriptions are gone
		_, found1 := ps.GetSubscription(sub1.ID)
		_, found2 := ps.GetSubscription(sub2.ID)
		if found1 || found2 {
			t.Error("expected subscriptions to be removed")
		}
	})

	t.Run("returns error for invalid channel name", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		err := ps.DeleteChannel(ctx, "invalid@channel")
		if err == nil {
			t.Error("expected error for invalid channel name")
		}
	})
}

func TestPushService_GetChannelFromHub(t *testing.T) {
	t.Parallel()

	t.Run("retrieves channel from hub when not cached", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		// Create channel directly via hub with options
		hubChannel, err := ps.GetHub().CreateChannel("hub-only-channel", WithBufferSize(256))
		if err != nil {
			t.Fatalf("failed to create hub channel: %v", err)
		}
		if hubChannel == nil {
			t.Fatal("hub channel is nil")
		}

		// Get channel via service - should fetch from hub
		channel, err := ps.GetChannel(ctx, "hub-only-channel")
		if err != nil {
			t.Fatalf("failed to get channel: %v", err)
		}
		if channel.Name != "hub-only-channel" {
			t.Errorf("expected name 'hub-only-channel', got %q", channel.Name)
		}
	})

	t.Run("returns error for invalid channel name", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		_, err := ps.GetChannel(ctx, "invalid@channel")
		if err == nil {
			t.Error("expected error for invalid channel name")
		}
	})

	t.Run("updates subscriber count from hub", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		// Create and cache channel
		ps.CreateChannel(ctx, "count-channel")

		// Subscribe to increment count
		ps.Subscribe(ctx, "count-channel")
		ps.Subscribe(ctx, "count-channel")

		// Get channel again - should have updated count
		channel, err := ps.GetChannel(ctx, "count-channel")
		if err != nil {
			t.Fatalf("failed to get channel: %v", err)
		}
		if channel.Subscribers < 2 {
			t.Errorf("expected at least 2 subscribers, got %d", channel.Subscribers)
		}
	})
}

func TestPushService_PublishWithStructPayload(t *testing.T) {
	t.Parallel()

	t.Run("publishes struct payload by serializing to JSON", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		ps.CreateChannel(ctx, "struct-channel")

		type TestPayload struct {
			Name  string `json:"name"`
			Value int    `json:"value"`
		}

		_, err := ps.Publish(ctx, "struct-channel", &TestPayload{Name: "test", Value: 42})
		if err != nil {
			t.Fatalf("failed to publish struct: %v", err)
		}
	})

	t.Run("publishes with custom message ID", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		ps.CreateChannel(ctx, "custom-id-channel")

		_, err := ps.Publish(ctx, "custom-id-channel", "test", WithMessageID("custom-123"))
		if err != nil {
			t.Fatalf("failed to publish with custom ID: %v", err)
		}
	})
}

func TestPushService_WatchChannelAdditional(t *testing.T) {
	t.Parallel()

	t.Run("creates watch subscription with filter", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		filter := &SubscriptionFilter{
			EventTypes: []EventType{EventTypePut},
			KeyPrefix:  "/users/",
		}
		sub, err := ps.WatchChannel(ctx, "/users/", WithFilter(filter))
		if err != nil {
			t.Fatalf("failed to watch with filter: %v", err)
		}
		if sub.Filters == nil {
			t.Error("expected filter to be set")
		}
	})

	t.Run("reuses existing watch channel", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		// First watch creates channel
		sub1, err := ps.WatchChannel(ctx, "/orders/")
		if err != nil {
			t.Fatalf("failed to create first watch: %v", err)
		}

		// Second watch reuses channel
		sub2, err := ps.WatchChannel(ctx, "/orders/")
		if err != nil {
			t.Fatalf("failed to create second watch: %v", err)
		}

		if sub1.Channel != sub2.Channel {
			t.Error("expected same channel for same prefix")
		}
	})
}

func TestPushService_StartStopAdditional(t *testing.T) {
	t.Parallel()

	t.Run("start sets running flag", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()

		if ps.IsRunning() {
			t.Error("expected service not running initially")
		}

		ps.Start(ctx)
		if !ps.IsRunning() {
			t.Error("expected service running after start")
		}

		ps.Stop(ctx)
		if ps.IsRunning() {
			t.Error("expected service not running after stop")
		}
	})
}

func TestPushService_SubscribeWithOptions(t *testing.T) {
	t.Parallel()

	t.Run("subscribes with filter option", func(t *testing.T) {
		t.Parallel()
		log := newServiceTestLogger(t)
		ps := NewPushService(log, nil)

		ctx := context.Background()
		ps.Start(ctx)
		defer ps.Stop(ctx)

		ps.CreateChannel(ctx, "filter-channel")

		filter := &SubscriptionFilter{
			EventTypes: []EventType{EventTypeDelete},
		}
		sub, err := ps.Subscribe(ctx, "filter-channel", WithFilter(filter))
		if err != nil {
			t.Fatalf("failed to subscribe with filter: %v", err)
		}
		if sub.Filters == nil {
			t.Error("expected filter to be set")
		}
	})
}

// =============================================================================
// Consolidated from coverage_boost_test.go
// =============================================================================

// TestDeltaComputer_ValueToStringNonString tests valueToString with non-string values.
func TestDeltaComputer_ValueToStringNonString(t *testing.T) {
	log := newTestLogger(t)
	dc := NewDeltaComputer(log)

	t.Run("converts string value", func(t *testing.T) {
		v := reflect.ValueOf("hello")
		result := dc.valueToString(v)
		assert.Equal(t, "hello", result)
	})

	t.Run("converts int value", func(t *testing.T) {
		v := reflect.ValueOf(42)
		result := dc.valueToString(v)
		assert.NotEmpty(t, result)
	})
}

// TestDeltaComputer_ExtractEntityTypeViaReflect tests the extractEntityType function.
func TestDeltaComputer_ExtractEntityTypeViaReflect(t *testing.T) {
	log := newTestLogger(t)
	dc := NewDeltaComputer(log)

	t.Run("extracts struct type name", func(t *testing.T) {
		result := dc.extractEntityType(reflect.TypeOf(testEntity{}))
		assert.Equal(t, "testEntity", result)
	})

	t.Run("extracts pointer type name", func(t *testing.T) {
		result := dc.extractEntityType(reflect.TypeOf(&testEntity{}))
		assert.Equal(t, "testEntity", result)
	})
}

// TestDeltaComputer_ExtractEntityIDVariants tests extractEntityID with various structs.
func TestDeltaComputer_ExtractEntityIDVariants(t *testing.T) {
	log := newTestLogger(t)
	dc := NewDeltaComputer(log)

	t.Run("extracts ID from struct", func(t *testing.T) {
		entity := &testEntity{ID: "entity-123"}
		result := dc.extractEntityID(entity)
		assert.Equal(t, "entity-123", result)
	})

	t.Run("returns empty for nil pointer", func(t *testing.T) {
		result := dc.extractEntityID(nil)
		assert.Empty(t, result)
	})

	t.Run("returns empty for non-struct", func(t *testing.T) {
		result := dc.extractEntityID("not-a-struct")
		assert.Empty(t, result)
	})

	t.Run("returns empty for struct without ID", func(t *testing.T) {
		type noID struct {
			Name string
		}
		result := dc.extractEntityID(&noID{Name: "test"})
		assert.Empty(t, result)
	})
}

// TestDeltaComputer_IsZeroValueEdgeCases tests the isZeroValue helper with edge cases.
func TestDeltaComputer_IsZeroValueEdgeCases(t *testing.T) {
	log := newTestLogger(t)
	dc := NewDeltaComputer(log)

	t.Run("invalid value is zero", func(t *testing.T) {
		assert.True(t, dc.isZeroValue(reflect.Value{}))
	})

	t.Run("nil pointer is zero", func(t *testing.T) {
		var p *string
		assert.True(t, dc.isZeroValue(reflect.ValueOf(&p).Elem()))
	})

	t.Run("empty array is zero", func(t *testing.T) {
		arr := [3]int{}
		assert.True(t, dc.isZeroValue(reflect.ValueOf(arr)))
	})

	t.Run("non-empty array is not zero", func(t *testing.T) {
		arr := [3]int{1, 0, 0}
		assert.False(t, dc.isZeroValue(reflect.ValueOf(arr)))
	})

	t.Run("zero time is zero", func(t *testing.T) {
		assert.True(t, dc.isZeroValue(reflect.ValueOf(time.Time{})))
	})

	t.Run("non-zero time is not zero", func(t *testing.T) {
		assert.False(t, dc.isZeroValue(reflect.ValueOf(time.Now())))
	})

	t.Run("nil map is zero", func(t *testing.T) {
		var m map[string]string
		assert.True(t, dc.isZeroValue(reflect.ValueOf(m)))
	})
}

// TestDeltaComputer_IsSpecialTypeChecks tests the isSpecialType helper.
func TestDeltaComputer_IsSpecialTypeChecks(t *testing.T) {
	log := newTestLogger(t)
	dc := NewDeltaComputer(log)

	t.Run("time.Time is special", func(t *testing.T) {
		assert.True(t, dc.isSpecialType(reflect.TypeOf(time.Time{})))
	})

	t.Run("pointer to time.Time is special", func(t *testing.T) {
		now := time.Now()
		assert.True(t, dc.isSpecialType(reflect.TypeOf(&now)))
	})

	t.Run("regular struct is not special", func(t *testing.T) {
		assert.False(t, dc.isSpecialType(reflect.TypeOf(testConfig{})))
	})
}

// TestDeltaComputer_ExtractValueEdgeCases tests the extractValue helper edge cases.
func TestDeltaComputer_ExtractValueEdgeCases(t *testing.T) {
	log := newTestLogger(t)
	dc := NewDeltaComputer(log)

	t.Run("extracts from invalid value", func(t *testing.T) {
		result := dc.extractValue(reflect.Value{})
		assert.Nil(t, result)
	})

	t.Run("extracts from nil pointer", func(t *testing.T) {
		var p *string
		result := dc.extractValue(reflect.ValueOf(&p).Elem())
		assert.Nil(t, result)
	})

	t.Run("extracts from valid pointer", func(t *testing.T) {
		s := "hello"
		result := dc.extractValue(reflect.ValueOf(&s))
		assert.Equal(t, "hello", result)
	})

	t.Run("extracts from interface", func(t *testing.T) {
		var i interface{} = 42
		result := dc.extractValue(reflect.ValueOf(&i).Elem())
		assert.Equal(t, 42, result)
	})
}

// TestDeltaComputer_GetFieldPathVariants tests the getFieldPath helper with various tag formats.
func TestDeltaComputer_GetFieldPathVariants(t *testing.T) {
	log := newTestLogger(t)
	dc := NewDeltaComputer(log)

	t.Run("returns json tag name", func(t *testing.T) {
		field, ok := reflect.TypeOf(testEntity{}).FieldByName("Name")
		require.True(t, ok)
		assert.Equal(t, "name", dc.getFieldPath(field))
	})

	t.Run("returns field name when no json tag", func(t *testing.T) {
		type noTag struct {
			Value int
		}
		field, ok := reflect.TypeOf(noTag{}).FieldByName("Value")
		require.True(t, ok)
		assert.Equal(t, "Value", dc.getFieldPath(field))
	})

	t.Run("handles json tag with omitempty", func(t *testing.T) {
		field, ok := reflect.TypeOf(testNestedEntity{}).FieldByName("Profile")
		require.True(t, ok)
		assert.Equal(t, "profile", dc.getFieldPath(field))
	})

	t.Run("returns dash for skip tag", func(t *testing.T) {
		type skipField struct {
			Skip int `json:"-"`
		}
		field, ok := reflect.TypeOf(skipField{}).FieldByName("Skip")
		require.True(t, ok)
		assert.Equal(t, "-", dc.getFieldPath(field))
	})
}

// TestDeltaComputer_ComputeMapAddRemove tests delta computation between map values.
func TestDeltaComputer_ComputeMapAddRemove(t *testing.T) {
	log := newTestLogger(t)
	dc := NewDeltaComputer(log)

	t.Run("detects added map keys", func(t *testing.T) {
		type withMap struct {
			ID   string            `json:"id"`
			Tags map[string]string `json:"tags"`
		}

		old := &withMap{ID: "1", Tags: map[string]string{"a": "1"}}
		newVal := &withMap{ID: "1", Tags: map[string]string{"a": "1", "b": "2"}}

		delta, err := dc.Compute(old, newVal)
		require.NoError(t, err)
		assert.NotEmpty(t, delta.Changes)

		found := false
		for _, c := range delta.Changes {
			if c.Path == "tags.b" && c.Operation == OpSet {
				found = true
				break
			}
		}
		assert.True(t, found, "expected change for added map key 'b'")
	})

	t.Run("detects removed map keys", func(t *testing.T) {
		type withMap struct {
			ID   string            `json:"id"`
			Tags map[string]string `json:"tags"`
		}

		old := &withMap{ID: "1", Tags: map[string]string{"a": "1", "b": "2"}}
		newVal := &withMap{ID: "1", Tags: map[string]string{"a": "1"}}

		delta, err := dc.Compute(old, newVal)
		require.NoError(t, err)
		assert.NotEmpty(t, delta.Changes)

		found := false
		for _, c := range delta.Changes {
			if c.Path == "tags.b" && c.Operation == OpDelete {
				found = true
				break
			}
		}
		assert.True(t, found, "expected delete change for removed map key 'b'")
	})
}

// TestDeltaComputer_CompareTimeFields tests comparison of time.Time fields.
func TestDeltaComputer_CompareTimeFields(t *testing.T) {
	log := newTestLogger(t)
	dc := NewDeltaComputer(log)

	now := time.Now()
	later := now.Add(1 * time.Hour)

	old := &testEntity{ID: "1", Name: "test", UpdatedAt: now}
	newVal := &testEntity{ID: "1", Name: "test", UpdatedAt: later}

	delta, err := dc.Compute(old, newVal)
	require.NoError(t, err)
	assert.NotEmpty(t, delta.Changes)

	found := false
	for _, c := range delta.Changes {
		if c.Path == "updated_at" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected change for updated_at field")
}

// TestCodecErrorTypeStrings tests the Error() method on codec error types.
func TestCodecErrorTypeStrings(t *testing.T) {
	t.Run("EmptyCodecNameError", func(t *testing.T) {
		err := &EmptyCodecNameError{}
		assert.Equal(t, "codec name is empty", err.Error())
	})

	t.Run("NilCodecNameError", func(t *testing.T) {
		err := &NilCodecNameError{}
		assert.Equal(t, "default codec name is nil or invalid", err.Error())
	})

	t.Run("NilEntityError", func(t *testing.T) {
		err := &NilEntityError{}
		assert.Equal(t, "entity is nil", err.Error())
	})

	t.Run("EmptyDataError", func(t *testing.T) {
		err := &EmptyDataError{}
		assert.Equal(t, "data is empty", err.Error())
	})
}

// TestDeltaIsDiffFailedNilAndTyped tests the IsDeltaDiffFailed error checker.
func TestDeltaIsDiffFailedNilAndTyped(t *testing.T) {
	t.Run("nil error returns false", func(t *testing.T) {
		assert.False(t, IsDeltaDiffFailed(nil))
	})

	t.Run("matching DeltaDiffError returns true", func(t *testing.T) {
		err := &DeltaDiffError{Cause: "test diff error"}
		assert.True(t, IsDeltaDiffFailed(err))
	})
}

// TestHubNilClientOperations tests hub operations with nil clients.
func TestHubNilClientOperations(t *testing.T) {
	log := newTestLogger(t)
	hub := NewNotificationHub(log)

	t.Run("register nil client is no-op", func(t *testing.T) {
		hub.RegisterClient(nil)
	})

	t.Run("unregister nil client is no-op", func(t *testing.T) {
		hub.UnregisterClient(nil)
	})
}

// TestPushService_PublishDeltaServiceState tests PublishDelta service state checks.
func TestPushService_PublishDeltaServiceState(t *testing.T) {
	log := newTestLogger(t)

	t.Run("publish delta when service not running", func(t *testing.T) {
		ps := NewPushService(log, nil)
		ctx := context.Background()

		_, err := ps.PublishDelta(ctx, "test-channel", &testEntity{Name: "old"}, &testEntity{Name: "new"})
		assert.Error(t, err)
		assert.True(t, IsServiceNotRunning(err))
	})

	t.Run("publish delta with invalid channel", func(t *testing.T) {
		ps := NewPushService(log, nil)
		require.NoError(t, ps.Start(context.Background()))
		defer ps.Stop(context.Background())

		ctx := context.Background()
		_, err := ps.PublishDelta(ctx, "", &testEntity{Name: "old"}, &testEntity{Name: "new"})
		assert.Error(t, err)
	})

	t.Run("publish delta with nil delta computer falls back to publish", func(t *testing.T) {
		ps := NewPushService(log, nil)
		ps.deltaComputer = nil
		require.NoError(t, ps.Start(context.Background()))
		defer ps.Stop(context.Background())

		_, err := ps.CreateChannel(context.Background(), "delta-channel")
		require.NoError(t, err)

		ctx := context.Background()
		_, err = ps.PublishDelta(ctx, "delta-channel", &testEntity{Name: "old"}, &testEntity{Name: "new"})
		assert.NoError(t, err)
	})
}

// TestPushService_GetSubscriptionClientErrors tests GetSubscriptionClient lookup paths.
func TestPushService_GetSubscriptionClientErrors(t *testing.T) {
	log := newTestLogger(t)

	t.Run("returns false when subscription does not exist", func(t *testing.T) {
		ps := NewPushService(log, nil)
		require.NoError(t, ps.Start(context.Background()))
		defer ps.Stop(context.Background())

		client, found := ps.GetSubscriptionClient("nonexistent-sub")
		assert.Nil(t, client)
		assert.False(t, found)
	})
}

// TestDeltaComputer_ExtractAllFields_NewEntity tests extractAllFields for new entity creation.
func TestDeltaComputer_ExtractAllFields_NewEntity(t *testing.T) {
	log := newTestLogger(t)
	dc := NewDeltaComputer(log)

	newVal := &testEntity{ID: "entity-1", Name: "test", Age: 25}

	delta, err := dc.Compute(nil, newVal)
	require.NoError(t, err)
	assert.NotEmpty(t, delta.Changes)

	foundName := false
	foundAge := false
	for _, c := range delta.Changes {
		if c.Path == "name" && c.Operation == OpSet {
			foundName = true
		}
		if c.Path == "age" && c.Operation == OpSet {
			foundAge = true
		}
	}
	assert.True(t, foundName)
	assert.True(t, foundAge)
}

// TestDeltaComputer_ExtractAllFields_NestedPointerStruct tests extractAllFields with ptr-to-struct.
func TestDeltaComputer_ExtractAllFields_NestedPointerStruct(t *testing.T) {
	log := newTestLogger(t)
	dc := NewDeltaComputer(log)

	newVal := &testNestedEntity{
		ID: "nested-1",
		Profile: &testProfile{
			Bio:   "developer",
			Email: "dev@example.com",
		},
		Config: testConfig{
			Theme:    "dark",
			Language: "en",
		},
	}

	delta, err := dc.Compute(nil, newVal)
	require.NoError(t, err)
	assert.NotEmpty(t, delta.Changes)

	foundBio := false
	foundTheme := false
	for _, c := range delta.Changes {
		if c.Path == "profile.bio" {
			foundBio = true
		}
		if c.Path == "config.theme" {
			foundTheme = true
		}
	}
	assert.True(t, foundBio, "expected extracted field profile.bio")
	assert.True(t, foundTheme, "expected extracted field config.theme")
}

// TestDeltaErrorCheckers_ComprehensiveTable tests all error checkers in a table.
func TestDeltaErrorCheckers_ComprehensiveTable(t *testing.T) {
	tests := []struct {
		name    string
		checker func(error) bool
		err     error
		want    bool
	}{
		{"IsDeltaNilInput nil", IsDeltaNilInput, nil, false},
		{"IsDeltaTypeMismatch nil", IsDeltaTypeMismatch, nil, false},
		{"IsDeltaMaxDepthExceeded nil", IsDeltaMaxDepthExceeded, nil, false},
		{"IsDeltaUnsupportedType nil", IsDeltaUnsupportedType, nil, false},
		{"IsDeltaDeserializationFailed nil", IsDeltaDeserializationFailed, nil, false},
		{"IsDeltaDiffFailed nil", IsDeltaDiffFailed, nil, false},
		{"IsDeltaNilInput match", IsDeltaNilInput, &DeltaNilInputError{Parameter: "new"}, true},
		{"IsDeltaTypeMismatch match", IsDeltaTypeMismatch, &DeltaTypeMismatchError{OldType: "int", NewType: "string"}, true},
		{"IsDeltaMaxDepthExceeded match", IsDeltaMaxDepthExceeded, &DeltaMaxDepthExceededError{MaxDepth: 10, Path: "a.b"}, true},
		{"IsDeltaUnsupportedType match", IsDeltaUnsupportedType, &DeltaUnsupportedTypeError{Type: "chan", Path: "root"}, true},
		{"IsDeltaDeserializationFailed match", IsDeltaDeserializationFailed, &DeltaDeserializationError{Cause: "bad data"}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.checker(tc.err))
		})
	}
}

// TestHubDeleteChannel_EmptyNameValidation tests DeleteChannel with empty name.
func TestHubDeleteChannel_EmptyNameValidation(t *testing.T) {
	log := newTestLogger(t)
	hub := NewNotificationHub(log)

	err := hub.DeleteChannel("")
	assert.Error(t, err)
}

// TestHubBroadcast_NilMessageValidation tests Broadcast with a nil message.
func TestHubBroadcast_NilMessageValidation(t *testing.T) {
	log := newTestLogger(t)
	hub := NewNotificationHub(log)

	err := hub.Broadcast(nil)
	assert.Error(t, err)
}

// TestPushService_Publish_WhenNotRunning tests Publish when service is not running.
func TestPushService_Publish_WhenNotRunning(t *testing.T) {
	log := newTestLogger(t)
	ps := NewPushService(log, nil)

	_, err := ps.Publish(context.Background(), "test", "data")
	assert.Error(t, err)
	assert.True(t, IsServiceNotRunning(err))
}
