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
	"errors"
	"runtime"
	"strings"
	"sync"
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
		sub := mustSubscribe(ctx, t, ps, "pub-channel")

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
		sub := mustSubscribe(ctx, t, ps, "delta-channel")

		client := mustSubscriptionClient(t, ps, sub.ID)

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
		mustSubscribe(ctx, t, ps, "stats-channel-1")
		mustSubscribe(ctx, t, ps, "stats-channel-1")
		mustSubscribe(ctx, t, ps, "stats-channel-2")

		// The hub processes registrations on its own goroutine (Run consumes
		// h.register), so these counts are eventually consistent -- reading
		// them straight after Subscribe assumes the hub has already been
		// scheduled, which holds on an idle machine and not on a loaded
		// runner. Poll to the expected state instead, and report whatever it
		// last saw if it never gets there (testing-guidelines.md: never treat
		// elapsed time as a synchronization primitive).
		var stats *Stats
		deadline := time.Now().Add(5 * time.Second)
		for {
			stats = ps.GetStats()
			if stats != nil && stats.TotalChannels == 2 && stats.TotalSubscriptions == 3 {
				break
			}
			if time.Now().After(deadline) {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}

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

// mustSubscribe subscribes and fails the test WITH THE REASON when it cannot.
//
// THE BLAST RADIUS IS WHY THIS EXISTS, not any one cause. Subscribe returns
// (nil, err) for conditions these tests do not control — a service that is not
// running, a hub that is overloaded — and a test that discards that error then
// dereferences nil. In a PARALLEL subtest that is not a failed assertion: it is
// a SIGSEGV, which aborts the whole test binary and takes every other test in
// the package with it. One failure here reddens the Race Detector leg for pull
// requests that touch nothing in this package (#337).
//
// The occurrence that exposed it was the hub's register/subscribe ordering
// window, fixed in this same change (#339) — so through PushService.Subscribe,
// which always hands the hub a freshly created and connected client, that
// particular error should no longer arrive. It remains reachable on the hub's
// own surface, where RegisterAndSubscribe still refuses a client that has been
// closed. That is exactly why the helper is written against the CLASS: the next
// cause will not be that one, and the cost of discarding an error must not be
// the package again.
//
// t.Fatalf names the reason and stops one subtest. The panic named nothing and
// stopped all of them.
func mustSubscribe(ctx context.Context, t *testing.T, ps *PushService, channel string, opts ...SubscribeOption) *Subscription {
	t.Helper()
	sub, err := ps.Subscribe(ctx, channel, opts...)
	if err != nil {
		t.Fatalf("Subscribe(%q): %v", channel, err)
	}
	if sub == nil {
		t.Fatalf("Subscribe(%q) returned no subscription and no error", channel)
	}
	return sub
}

// mustSubscriptionClient resolves a subscription's client, for the same reason.
func mustSubscriptionClient(t *testing.T, ps *PushService, subscriptionID string) *Client {
	t.Helper()
	client, found := ps.GetSubscriptionClient(subscriptionID)
	if !found {
		t.Fatalf("GetSubscriptionClient(%q): subscription client not found", subscriptionID)
	}
	if client == nil {
		t.Fatalf("GetSubscriptionClient(%q) reported found with a nil client", subscriptionID)
	}
	return client
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
		sub := mustSubscribe(ctx, t, ps, "getsub-channel")

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
		sub1 := mustSubscribe(ctx, t, ps, "delete-sub-channel")
		sub2 := mustSubscribe(ctx, t, ps, "delete-sub-channel")

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
		mustSubscribe(ctx, t, ps, "count-channel")
		mustSubscribe(ctx, t, ps, "count-channel")

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

// TestPushService_Publish_WhenNotRunning tests Publish when service is not running.
func TestPushService_Publish_WhenNotRunning(t *testing.T) {
	log := newTestLogger(t)
	ps := NewPushService(log, nil)

	_, err := ps.Publish(context.Background(), "test", "data")
	assert.Error(t, err)
	assert.True(t, IsServiceNotRunning(err))
}

// TestNotificationHub_RegisterAndSubscribe_IsOrderedWithTheWindowForcedOpen is
// #339's real gate, and it does not depend on the machine being busy.
//
// FORCING THE WINDOW BEATS WAITING FOR IT. The defect needs both messages queued
// before the hub cycles, which on an idle box almost never happens — a
// concurrency smoke test passes with the fix reverted (measured: 3 for 3), so as
// a gate it is decoration. Here the requests are queued while the loop cannot
// service them and it is released afterwards, so both are ready on the very next
// cycle every time. That is the exact condition, produced deterministically
// rather than hoped for.
//
// HOW THE WINDOW IS HELD OPEN CHANGED WITH #345, and the reason is worth keeping.
// This test used to queue against a hub that had not been started yet and start
// it afterwards. admit now refuses a request to a hub that is not running — that
// refusal IS #345's feature, "not running yet" and "not running any more" being
// the same answer — so nothing ever reached the queue and the spin waiting for it
// never ended. The request is now admitted by a genuinely running hub whose loop
// is wedged inside a handler, which reproduces the same condition through a door
// that is still open. Nothing about what is being gated changed.
//
// With the two-call form the hub's select then picks between them at random —
// about half these iterations report the client unregistered. With one request
// there is nothing to order.
//
// Mutation evidence: replace the RegisterAndSubscribe call below with
// RegisterClient followed by SubscribeToChannel and this fails, naming the
// iteration and the error. Measured after the rewrite: 20 of 20 runs fail.
func TestNotificationHub_RegisterAndSubscribe_IsOrderedWithTheWindowForcedOpen(t *testing.T) {
	t.Parallel()

	const iterations = 20
	for i := 0; i < iterations; i++ {
		log := newServiceTestLogger(t)
		hub := NewNotificationHub(log)
		client := NewClient("racer", DefaultBufferSize)

		ctx, cancel := context.WithCancel(context.Background())

		// A running hub that cannot service anything, so the work below is
		// admitted for real and still waits.
		startHubLoop(ctx, t, hub)
		release := wedgeHubLoop(t, hub)

		result := make(chan error, 1)
		go func() { result <- hub.RegisterAndSubscribe(client, "ordered-channel") }()

		// Wait for the request to be sitting in the queue, and distinguish the
		// two ways that can fail to happen. A REFUSAL returns on result without
		// ever queuing, and means the precondition is gone — that is what admit
		// does to a hub that is not running, and it is how this test failed
		// silently before it was rewritten. Merely being SLOW to schedule is not
		// a defect, so the deadline is generous: this waits on a goroutine
		// starting, not on the property under test, and a loaded machine can
		// take a while. Only one of the two is a finding, so they get different
		// messages.
		admissionDeadline := time.After(30 * time.Second)
		for len(hub.subscribe) == 0 {
			select {
			case err := <-result:
				t.Fatalf("iteration %d: the call returned without ever being queued, so the window never opened: %v", i, err)
			case <-admissionDeadline:
				t.Fatalf("iteration %d: request still not queued after 30s; the hub is running=%v", i, hub.IsRunning())
			default:
				runtime.Gosched()
			}
		}

		// Only now can the loop look at what is waiting for it.
		release()

		err := <-result
		cancel()
		if err != nil {
			t.Fatalf("iteration %d: RegisterAndSubscribe against a hub that had both messages queued: %v", i, err)
		}
	}
}

// TestPushService_Subscribe_NeverReportsItsOwnClientUnregistered exercises the
// real Subscribe path under concurrency.
//
// IT IS NOT THE GATE FOR THE ORDERING, and saying so is the point: it passed
// three times out of three with the fix reverted on an idle machine, because
// the defect needs the hub to be behind when the messages arrive. On a
// saturated box the same shape failed 45 of 50. A test whose ability to fail
// depends on who else is using the machine cannot be relied on to catch a
// regression — TestNotificationHub_RegisterAndSubscribe_IsOrderedWithTheWindowForcedOpen
// above is what does that.
//
// What this one is worth keeping for: it drives the actual PushService.Subscribe
// path end to end, so a future change that stops using the ordered request but
// still compiles is caught here whenever the machine is loaded enough — and it
// documents the concurrency the subsystem is expected to survive.
func TestPushService_Subscribe_NeverReportsItsOwnClientUnregistered(t *testing.T) {
	t.Parallel()

	log := newServiceTestLogger(t)
	ps := NewPushService(log, nil)
	ctx := context.Background()
	if err := ps.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = ps.Stop(ctx) }()

	const concurrency = 50
	var mu sync.Mutex
	var failures []error

	var wg sync.WaitGroup
	release := make(chan struct{})
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-release // all at once, so the hub is behind when they arrive
			sub, err := ps.Subscribe(ctx, "ordering-channel")
			if err == nil && sub != nil {
				return
			}
			mu.Lock()
			failures = append(failures, err)
			mu.Unlock()
		}()
	}
	close(release)
	wg.Wait()

	if len(failures) != 0 {
		t.Fatalf("%d of %d subscribes against a running service failed; first: %v",
			len(failures), concurrency, failures[0])
	}
}

// TestNotificationHub_RegisterAndSubscribe_KeepsTheStrictFormStrict pins the
// half of #339's fix that is easy to lose: registering as part of the subscribe
// is OPT-IN, so SubscribeToChannel still refuses a client nobody registered.
//
// Without the opt-in the fix would have silently deleted that contract — which
// hub_test.go already asserts — and any caller relying on the refusal would have
// started succeeding.
func TestNotificationHub_RegisterAndSubscribe_KeepsTheStrictFormStrict(t *testing.T) {
	t.Parallel()

	log := newServiceTestLogger(t)
	hub := NewNotificationHub(log)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)
	for !hub.IsRunning() {
		time.Sleep(time.Millisecond)
	}

	strict := NewClient("never-registered", DefaultBufferSize)
	if err := hub.SubscribeToChannel(strict, "strict-channel"); !errors.Is(err, ErrClientNotRegistered) {
		t.Fatalf("SubscribeToChannel err = %v, want ErrClientNotRegistered for an unregistered client", err)
	}

	joined := NewClient("registers-itself", DefaultBufferSize)
	if err := hub.RegisterAndSubscribe(joined, "strict-channel"); err != nil {
		t.Fatalf("RegisterAndSubscribe: %v", err)
	}

	// And it registered exactly once: subscribing again is not a second client.
	before := hub.ClientCount()
	if err := hub.RegisterAndSubscribe(joined, "second-channel"); err != nil {
		t.Fatalf("RegisterAndSubscribe (second channel): %v", err)
	}
	if after := hub.ClientCount(); after != before {
		t.Fatalf("client count went %d -> %d; registering an already-registered client counted it twice",
			before, after)
	}
}

// TestNotificationHub_RegisterAndSubscribe_DoesNotResurrectAnUnregisteredClient
// is the hazard the ordering fix introduced and had to close (found in review of
// #339).
//
// handleUnregister removes a client and CLOSES it. "Register the client if it is
// absent" would therefore undo a deliberate removal whenever an unregister and a
// subscribe race: the client returns to the registry disconnected, is counted,
// and is handed messages nothing will ever read. Being absent is not on its own
// evidence that a client SHOULD be registered — being absent and still connected
// is.
//
// Mutation evidence: drop the IsConnected condition from handleSubscribe and
// this fails, with the closed client back in the registry.
func TestNotificationHub_RegisterAndSubscribe_DoesNotResurrectAnUnregisteredClient(t *testing.T) {
	t.Parallel()

	log := newServiceTestLogger(t)
	hub := NewNotificationHub(log)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)
	for !hub.IsRunning() {
		runtime.Gosched()
	}

	client := NewClient("departing", DefaultBufferSize)
	if err := hub.RegisterAndSubscribe(client, "leaving-channel"); err != nil {
		t.Fatalf("RegisterAndSubscribe: %v", err)
	}

	// The client leaves: unregistered, and closed by the hub. Waiting for the
	// count to reach zero is enough to know the close has happened, because
	// handleUnregister closes the client before it stops counting it and both
	// are sequentially consistent atomics (#588). It was not enough when the
	// count dropped first, and this wait is where that showed up.
	hub.UnregisterClient(client)
	for hub.ClientCount() != 0 {
		runtime.Gosched()
	}
	if client.IsConnected() {
		t.Fatal("precondition: unregistering should have closed the client")
	}

	// A subscribe that was already in flight when it left must NOT bring it back.
	err := hub.RegisterAndSubscribe(client, "leaving-channel")
	if !errors.Is(err, ErrClientNotRegistered) {
		t.Fatalf("err = %v, want ErrClientNotRegistered; a closed client was registered again", err)
	}
	if got := hub.ClientCount(); got != 0 {
		t.Fatalf("client count = %d, want 0; the hub resurrected a disconnected client", got)
	}
}

// TestNotificationHub_UnregisterClosesClientBeforeUncountingIt is the
// regression test for #588.
//
// ClientCount is the ONLY signal a caller outside the hub's goroutine has that
// an asynchronous UnregisterClient finished, so the count dropping has to mean
// the unregister is done — including the close the hub owes the client.
// handleUnregister decremented first and closed second, so a caller that waited
// for the count and then looked at the client could find it still connected.
//
// THE GAP IS A FEW INSTRUCTIONS WIDE, which is why this test does not sample
// ClientCount in a loop the way the resurrection test above does. Sampling
// catches the wrong order in roughly one run in ten on an idle host — enough
// for #588 to turn an unrelated pull request red and not nearly enough to be
// the test that guards the ordering. The hub instead hands the instant over:
// onClientUncounted fires exactly where the count becomes observably zero, and
// the client must already be closed when it does. Wrong order, and this fails
// every run.
func TestNotificationHub_UnregisterClosesClientBeforeUncountingIt(t *testing.T) {
	t.Parallel()

	log := newServiceTestLogger(t)
	hub := NewNotificationHub(log)

	// Buffered so the hub's goroutine never blocks on the observation, and
	// sized for one because only the first departure is under test.
	connectedWhenUncounted := make(chan bool, 1)
	hub.onClientUncounted = func(c *Client) {
		select {
		case connectedWhenUncounted <- c.IsConnected():
		default:
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)
	for !hub.IsRunning() {
		runtime.Gosched()
	}

	client := NewClient("uncounted-last", DefaultBufferSize)
	if err := hub.RegisterAndSubscribe(client, "departures"); err != nil {
		t.Fatalf("RegisterAndSubscribe: %v", err)
	}

	hub.UnregisterClient(client)

	select {
	case stillConnected := <-connectedWhenUncounted:
		if stillConnected {
			t.Fatal("client was still connected at the instant ClientCount stopped counting it; " +
				"a caller waiting on the count would act on a client the hub had not closed")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the hub never processed the unregister")
	}

	if got := hub.ClientCount(); got != 0 {
		t.Fatalf("client count = %d, want 0", got)
	}
	if client.IsConnected() {
		t.Fatal("client is still connected after the hub uncounted it")
	}
}

// TestNotificationHub_CleanupClosesEveryClientItStopsCounting holds the shutdown
// path to the same rule as handleUnregister (#588).
//
// cleanup zeroes ClientCount outright rather than decrementing per client, so
// the same promise applies to it: nothing may observe a zero count while a
// client the hub was counting is still open. Waiting for IsRunning() to go
// false is a sound way to observe that, because Run's deferred running.Store
// is registered before the loop and therefore runs after cleanup has returned.
func TestNotificationHub_CleanupClosesEveryClientItStopsCounting(t *testing.T) {
	t.Parallel()

	log := newServiceTestLogger(t)
	hub := NewNotificationHub(log)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)
	for !hub.IsRunning() {
		runtime.Gosched()
	}

	clients := []*Client{
		NewClient("shutdown-a", DefaultBufferSize),
		NewClient("shutdown-b", DefaultBufferSize),
		NewClient("shutdown-c", DefaultBufferSize),
	}
	for _, client := range clients {
		if err := hub.RegisterAndSubscribe(client, "closing-time"); err != nil {
			t.Fatalf("RegisterAndSubscribe(%s): %v", client.ID, err)
		}
	}
	if got := hub.ClientCount(); got != len(clients) {
		t.Fatalf("client count = %d, want %d before shutdown", got, len(clients))
	}

	cancel()
	for hub.IsRunning() {
		runtime.Gosched()
	}

	if got := hub.ClientCount(); got != 0 {
		t.Fatalf("client count = %d, want 0 after cleanup", got)
	}
	for _, client := range clients {
		if client.IsConnected() {
			t.Fatalf("client %s is still connected after cleanup zeroed the count", client.ID)
		}
	}
}

// TestPushService_StoppedHubIsDistinguishableFromEmptyHub is the regression
// test for #352: at the PushService boundary a hub that cannot answer must not
// look like a hub with nothing to say.
//
// THE STATE UNDER TEST IS REACHABLE WITHOUT ANYONE CALLING Stop. PushService
// runs the hub's loop on the context Start was handed, so cancelling that
// context ends the loop while ps.running stays set — and PushService.Stop
// holds the same flag set for up to five seconds while it waits for the loop
// to leave. Every assertion below is made against a service that reports
// itself running.
func TestPushService_StoppedHubIsDistinguishableFromEmptyHub(t *testing.T) {
	t.Parallel()

	// runningServiceWithStoppedHub returns a service whose hub loop has ended
	// and which still reports IsRunning, holding one channel in its metadata
	// cache so the cache-fallback paths are exercised rather than skipped.
	runningServiceWithStoppedHub := func(t *testing.T) *PushService {
		t.Helper()
		ps := NewPushService(newServiceTestLogger(t), nil)

		hubCtx, cancelHub := context.WithCancel(context.Background())
		require.NoError(t, ps.Start(hubCtx))
		t.Cleanup(func() { _ = ps.Stop(context.Background()) })
		_, err := ps.CreateChannel(context.Background(), "cached-channel")
		require.NoError(t, err)

		cancelHub()
		require.Eventually(t, func() bool { return !ps.hub.IsRunning() },
			5*time.Second, time.Millisecond, "hub loop did not exit after its context was cancelled")
		require.True(t, ps.IsRunning(),
			"precondition: the service must still report running, or this test is not about #352")
		return ps
	}

	t.Run("ListChannels reports unavailable rather than an empty list", func(t *testing.T) {
		t.Parallel()
		ps := runningServiceWithStoppedHub(t)

		channels, err := ps.ListChannels(context.Background())

		require.Error(t, err, "a stopped hub must not be reported as a successful listing")
		require.ErrorIs(t, err, ErrHubStopped)
		require.Nil(t, channels)
	})

	t.Run("a healthy empty hub still lists successfully", func(t *testing.T) {
		t.Parallel()
		ps := NewPushService(newServiceTestLogger(t), nil)
		ctx := context.Background()
		require.NoError(t, ps.Start(ctx))
		defer func() { _ = ps.Stop(ctx) }()

		channels, err := ps.ListChannels(ctx)

		// The other half of the distinction: emptiness is still an answer, and
		// the two cases must not collapse back onto each other.
		require.NoError(t, err)
		require.Empty(t, channels)
	})

	t.Run("GetChannel reports unavailable rather than not-found", func(t *testing.T) {
		t.Parallel()
		ps := runningServiceWithStoppedHub(t)

		channel, err := ps.GetChannel(context.Background(), "never-created")

		require.Error(t, err)
		require.ErrorIs(t, err, ErrHubStopped)
		require.False(t, IsChannelNotFound(err),
			"a hub that never looked has not established that the channel is missing")
		require.Nil(t, channel)
	})

	t.Run("GetChannel does not answer a stopped hub from its cache", func(t *testing.T) {
		t.Parallel()
		ps := runningServiceWithStoppedHub(t)

		channel, err := ps.GetChannel(context.Background(), "cached-channel")

		require.Error(t, err, "cached metadata is not evidence that the hub is up")
		require.ErrorIs(t, err, ErrHubStopped)
		require.Nil(t, channel)
	})

	t.Run("Publish does not continue on its cache after a stopped hub", func(t *testing.T) {
		t.Parallel()
		ps := runningServiceWithStoppedHub(t)

		result, err := ps.Publish(context.Background(), "cached-channel", []byte("payload"))

		require.Error(t, err)
		require.ErrorIs(t, err, ErrHubStopped)
		require.Nil(t, result)
	})
}

// newReleaseObservingService returns a started PushService whose hub reports
// every client it stops counting, and the channel those reports arrive on.
//
// The hub's client registry is owned by its goroutine and has no synchronous
// reader, so a test cannot look inside it without racing. onClientUncounted is
// the hub's own report of a departure and fires immediately after
// handleUnregister has deleted the client from that registry (#588), which
// makes it the sound way to assert removal — and, being a report rather than a
// sample, it needs no polling to see. The hook must be installed before Run
// starts, which is why the service is started here rather than by the caller.
func newReleaseObservingService(t *testing.T, capacity int) (*PushService, <-chan string) {
	t.Helper()

	ps := NewPushService(newServiceTestLogger(t), nil)

	// Buffered to capacity so the hub's goroutine never blocks reporting, and
	// unbuffered sends are never dropped silently: an overflowing hook would
	// turn "no further departures" into an assertion that cannot fail.
	uncounted := make(chan string, capacity)
	ps.hub.onClientUncounted = func(c *Client) {
		uncounted <- c.ID
	}

	ctx := context.Background()
	if err := ps.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := ps.Stop(context.Background()); err != nil {
			t.Errorf("Stop during cleanup: %v", err)
		}
	})

	return ps, uncounted
}

// awaitUncounted collects the next n departures the hub reports.
func awaitUncounted(t *testing.T, uncounted <-chan string, n int) map[string]bool {
	t.Helper()

	got := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		select {
		case id := <-uncounted:
			got[id] = true
		case <-time.After(30 * time.Second):
			t.Fatalf("the hub reported only %d of %d departures: %v", len(got), n, got)
		}
	}
	return got
}

// TestPushService_DeleteChannelUnregistersItsSubscribersFromTheHub is the
// regression test for #673.
//
// DeleteChannel closed each subscriber's client and stopped there. The hub was
// never told, so the client stayed in the registry and in ClientCount while
// closed: the count overstated the live clients for the life of the hub, every
// all-clients broadcast kept iterating clients that can never be delivered to,
// and a service that creates and deletes channels accumulated them without
// bound. Measured before the fix: three subscribers, one channel deleted, zero
// unregisters and a count still reading 3.
func TestPushService_DeleteChannelUnregistersItsSubscribersFromTheHub(t *testing.T) {
	t.Parallel()

	ps, uncounted := newReleaseObservingService(t, 8)
	ctx := context.Background()

	for _, name := range []string{"doomed", "surviving"} {
		if _, err := ps.CreateChannel(ctx, name); err != nil {
			t.Fatalf("CreateChannel(%q): %v", name, err)
		}
	}
	doomedA := mustSubscribe(ctx, t, ps, "doomed")
	doomedB := mustSubscribe(ctx, t, ps, "doomed")
	survivor := mustSubscribe(ctx, t, ps, "surviving")

	if got := ps.hub.ClientCount(); got != 3 {
		t.Fatalf("client count = %d, want 3 before the delete", got)
	}
	survivorClient, ok := ps.GetSubscriptionClient(survivor.ID)
	if !ok {
		t.Fatal("the surviving subscription has no client")
	}

	if err := ps.DeleteChannel(ctx, "doomed"); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}

	// Exactly the deleted channel's subscribers leave the registry, and the
	// count is read only after the hub has reported both — the unregister is
	// asynchronous, so the report is what says the count is final.
	departed := awaitUncounted(t, uncounted, 2)
	for _, sub := range []*Subscription{doomedA, doomedB} {
		if !departed[sub.ClientID] {
			t.Errorf("the hub never unregistered %s; it is closed and still counted", sub.ClientID)
		}
	}
	if departed[survivor.ClientID] {
		t.Error("deleting one channel unregistered a client subscribed to another")
	}

	if got := ps.hub.ClientCount(); got != 1 {
		t.Errorf("client count = %d, want 1 (only the surviving subscriber)", got)
	}
	if !survivorClient.IsConnected() {
		t.Error("the surviving subscription's client was closed by an unrelated delete")
	}
	if _, found := ps.GetSubscription(survivor.ID); !found {
		t.Error("the surviving subscription was removed by an unrelated delete")
	}
}

// TestPushService_DeleteChannelWithNoSubscribersUnregistersNothing is the edge
// of #673's fix: sweeping subscriptions must release only the ones that belong
// to the deleted channel, and a channel nobody subscribed to has none.
//
// The negative half is asserted without waiting on the absence of an event.
// Unregisters travel one FIFO queue to one goroutine, so a spurious unregister
// from the delete would necessarily be enqueued before the sentinel's — and
// therefore reported before it. Seeing the sentinel first proves no other
// client departed.
func TestPushService_DeleteChannelWithNoSubscribersUnregistersNothing(t *testing.T) {
	t.Parallel()

	ps, uncounted := newReleaseObservingService(t, 8)
	ctx := context.Background()

	for _, name := range []string{"deserted", "occupied"} {
		if _, err := ps.CreateChannel(ctx, name); err != nil {
			t.Fatalf("CreateChannel(%q): %v", name, err)
		}
	}
	sentinel := mustSubscribe(ctx, t, ps, "occupied")

	if err := ps.DeleteChannel(ctx, "deserted"); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}
	if got := ps.hub.ClientCount(); got != 1 {
		t.Errorf("client count = %d, want 1 after deleting an unsubscribed channel", got)
	}

	if err := ps.Unsubscribe(ctx, sentinel.ID); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	if first := awaitUncounted(t, uncounted, 1); !first[sentinel.ClientID] {
		t.Errorf("the first client the hub unregistered was %v, want only the sentinel %s; "+
			"deleting an unsubscribed channel released a client it does not own", first, sentinel.ClientID)
	}
	if got := ps.hub.ClientCount(); got != 0 {
		t.Errorf("client count = %d, want 0", got)
	}
}

// TestPushService_DeleteChannelOnAMissingChannelReleasesNoClient covers the
// error path of the sweep #673 added to: DeleteChannel must fail before it
// touches anything when the hub does not have the channel.
func TestPushService_DeleteChannelOnAMissingChannelReleasesNoClient(t *testing.T) {
	t.Parallel()

	ps, uncounted := newReleaseObservingService(t, 8)
	ctx := context.Background()

	if _, err := ps.CreateChannel(ctx, "occupied"); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	sentinel := mustSubscribe(ctx, t, ps, "occupied")

	// Matched on the sentinel rather than through IsChannelNotFound: the hub
	// reports a missing channel as a QBError carrying the generic not-found
	// code, and that predicate answers false for it. Filed separately; this
	// test is about the sweep, not the classifier.
	err := ps.DeleteChannel(ctx, "never-created")
	if !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("DeleteChannel on a missing channel: got %v, want channel-not-found", err)
	}
	if got := ps.hub.ClientCount(); got != 1 {
		t.Errorf("client count = %d, want 1 after a delete that failed", got)
	}

	if err := ps.Unsubscribe(ctx, sentinel.ID); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	if first := awaitUncounted(t, uncounted, 1); !first[sentinel.ClientID] {
		t.Errorf("the first client the hub unregistered was %v, want only the sentinel %s; "+
			"a failed delete released a client", first, sentinel.ClientID)
	}
}

// drainUnregisterQueue reports the clients the service asked the hub to
// unregister and that no loop was left to collect.
//
// SOUND ONLY ONCE THE HUB HAS STOPPED, which is the case this exists for.
// Run's deferred running.Store(false) is registered first and therefore runs
// last, so IsRunning() reporting false means the loop has returned and nothing
// else can read the queue; the drain below is then the whole of what was
// asked. While a loop is running the queue is a race and says nothing.
func drainUnregisterQueue(t *testing.T, hub *NotificationHub) map[string]bool {
	t.Helper()

	asked := make(map[string]bool)
	for {
		select {
		case client := <-hub.unregister:
			asked[client.ID] = true
		default:
			return asked
		}
	}
}

// hubPark holds a hub's event loop inside a handler so a test can observe what
// the rest of the service does while that loop is still, technically, running.
//
// THE STATE IS NOT REACHABLE ANY OTHER WAY. Both of PushService.Stop's early
// exits from its wait — the expired deadline and the cancelled context — are
// defined by the hub OUTLIVING the wait, and a hub asked to stop leaves in
// microseconds. Nothing a test can say makes it linger; the only lever is to
// give its single goroutine something it cannot finish.
//
// onClientUncounted IS THAT LEVER, and it is not a back door: the hub already
// exposes it so #588's ordering could be asserted from the hub's own goroutine
// rather than raced for. Parking there means the loop is stopped at a real
// point in a real handler, holding no lifecycle lock, exactly as it would be
// mid-handler when a slow Stop gives up on it.
type hubPark struct {
	anchorID    string
	entered     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
}

// armHubPark installs the park on ps's hub. It must be called before Start,
// because the hub's goroutine reads the hook and its creation is what
// publishes the write.
func armHubPark(t *testing.T, ps *PushService) *hubPark {
	t.Helper()

	park := &hubPark{
		anchorID: "hub-park-anchor",
		entered:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	// Only the anchor parks. Every other client the hub uncounts — including
	// the ones a sweep under test releases — must pass straight through, or
	// the park would block the very work it exists to observe.
	ps.hub.onClientUncounted = func(c *Client) {
		if c.ID != park.anchorID {
			return
		}
		close(park.entered)
		<-park.release
	}
	t.Cleanup(park.unpark)
	return park
}

// hold blocks until the hub's loop is parked and cannot make progress.
func (p *hubPark) hold(t *testing.T, ps *PushService) {
	t.Helper()

	anchor := NewClient(p.anchorID, DefaultBufferSize)
	if err := ps.hub.RegisterAndSubscribe(anchor, "hub-park-anchor-channel"); err != nil {
		t.Fatalf("registering the park anchor: %v", err)
	}
	ps.hub.UnregisterClient(anchor)

	select {
	case <-p.entered:
	case <-time.After(30 * time.Second):
		t.Fatal("the hub's loop never reached the park")
	}
	if !ps.hub.IsRunning() {
		t.Fatal("precondition: a parked loop must still report the hub running")
	}
}

// unpark lets the loop go. Idempotent, so the cleanup and an explicit call in
// the body of a test cannot both close the same channel.
func (p *hubPark) unpark() {
	p.releaseOnce.Do(func() { close(p.release) })
}

// TestPushService_StopReleasesEverySubscriptionClientToTheHub is the Stop half
// of #673.
//
// Stop closed each subscription's client and never unregistered it. The hub's
// own cleanup hides that in the ordinary case — it closes every client, clears
// the registry and zeroes the count as the loop leaves — but the wait above
// the sweep breaks on a deadline, and a hub that outlives that deadline is
// still running and goes on counting clients Stop has closed. So the sweep
// must ask, and this asserts both halves: the end state the hub guarantees,
// and the request Stop owes it.
func TestPushService_StopReleasesEverySubscriptionClientToTheHub(t *testing.T) {
	t.Parallel()

	ps := NewPushService(newServiceTestLogger(t), nil)
	ctx := context.Background()
	if err := ps.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	for _, name := range []string{"alpha", "beta"} {
		if _, err := ps.CreateChannel(ctx, name); err != nil {
			t.Fatalf("CreateChannel(%q): %v", name, err)
		}
	}
	subs := []*Subscription{
		mustSubscribe(ctx, t, ps, "alpha"),
		mustSubscribe(ctx, t, ps, "alpha"),
		mustSubscribe(ctx, t, ps, "beta"),
	}
	clients := make([]*Client, 0, len(subs))
	for _, sub := range subs {
		client, ok := ps.GetSubscriptionClient(sub.ID)
		if !ok {
			t.Fatalf("subscription %s has no client", sub.ID)
		}
		clients = append(clients, client)
	}
	if got := ps.hub.ClientCount(); got != len(subs) {
		t.Fatalf("client count = %d, want %d before the stop", got, len(subs))
	}

	if err := ps.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got := ps.hub.ClientCount(); got != 0 {
		t.Errorf("client count = %d, want 0 after the stop", got)
	}
	for _, client := range clients {
		if client.IsConnected() {
			t.Errorf("client %s is still connected after the stop", client.ID)
		}
	}
	ps.subscriptions.Range(func(key, _ interface{}) bool {
		t.Errorf("subscription %v survived the stop", key)
		return true
	})

	// THE ASKING IS ASSERTED WHERE IT IS OBSERVABLE, next door. An ordinary
	// Stop waits for the loop to leave, so by the time the sweep runs the hub's
	// admission is closed and each release is refused rather than queued (#696)
	// — correctly, because a departed loop's cleanup has already emptied the
	// registry and there is nothing left to tell. Reading the unregister queue
	// here would assert that a request Stop no longer needs to make was made.
	// The case the request exists for is a hub still running when the sweep
	// runs, which is what the test below constructs.
	if left := drainUnregisterQueue(t, ps.hub); len(left) != 0 {
		t.Errorf("the unregister queue holds %v after a completed stop; "+
			"a departed loop's queue must be empty", left)
	}
}

// TestPushService_StopHandsEveryClientToAHubThatIsStillRunning is the case the
// sweep's unregister exists for, and the one the test above cannot show.
//
// The hub's cleanup masks a sweep that forgets to unregister: it closes every
// client, clears the registry and zeroes the count as the loop leaves. That
// mask is only there when the loop actually leaves — and Stop's wait has two
// exits that do not wait for it. A hub still running past those is a hub that
// goes on counting clients Stop has closed, forever, and the only thing that
// prevents it is the sweep telling the hub (#673).
//
// The loop is parked rather than merely slow, so the state is produced every
// run rather than sometimes.
func TestPushService_StopHandsEveryClientToAHubThatIsStillRunning(t *testing.T) {
	t.Parallel()

	ps := NewPushService(newServiceTestLogger(t), nil)
	park := armHubPark(t, ps)
	ctx := context.Background()
	if err := ps.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	subs := []*Subscription{
		mustSubscribe(ctx, t, ps, "alpha"),
		mustSubscribe(ctx, t, ps, "beta"),
	}
	clients := make([]*Client, 0, len(subs))
	for _, sub := range subs {
		clients = append(clients, mustSubscriptionClient(t, ps, sub.ID))
	}

	park.hold(t, ps)

	// A cancelled context, so Stop gives up on the parked loop at once instead
	// of holding the test for its five-second default.
	stopCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ps.Stop(stopCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop = %v, want context.Canceled; abandoning a hub is not a clean stop", err)
	}

	// Read while the loop is held: it is the only reader of this queue, and it
	// cannot reach the queue from inside the park.
	asked := drainUnregisterQueue(t, ps.hub)
	for _, sub := range subs {
		if !asked[sub.ClientID] {
			t.Errorf("Stop closed %s without asking the still-running hub to unregister it; "+
				"that hub would count it for the rest of its life", sub.ClientID)
		}
	}
	for _, client := range clients {
		if client.IsConnected() {
			t.Errorf("client %s is still connected after the stop", client.ID)
		}
	}

	park.unpark()
}

// TestPushService_StopWhenAlreadyStoppedReleasesNothingFurther is the edge of
// the Stop half of #673: the sweep runs once, and a second Stop has nothing
// left to release rather than releasing something twice.
func TestPushService_StopWhenAlreadyStoppedReleasesNothingFurther(t *testing.T) {
	t.Parallel()

	ps := NewPushService(newServiceTestLogger(t), nil)
	ctx := context.Background()
	if err := ps.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := ps.CreateChannel(ctx, "alpha"); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	sub := mustSubscribe(ctx, t, ps, "alpha")
	client := mustSubscriptionClient(t, ps, sub.ID)

	if err := ps.Stop(ctx); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	// The first Stop swept it: the client is closed and the subscription is
	// gone. What it did NOT leave is a queued unregister — the loop had already
	// left, so the release was refused rather than queued (#696), which is what
	// makes the emptiness check after the second Stop mean something.
	if client.IsConnected() {
		t.Fatalf("the first Stop left %s connected", sub.ClientID)
	}
	if _, found := ps.GetSubscriptionClient(sub.ID); found {
		t.Fatalf("the first Stop left subscription %s in place", sub.ID)
	}

	if err := ps.Stop(ctx); err != nil {
		t.Errorf("second Stop: got %v, want nil", err)
	}
	if ps.IsRunning() {
		t.Error("the service reports running after two stops")
	}
	if got := ps.hub.ClientCount(); got != 0 {
		t.Errorf("client count = %d, want 0", got)
	}
	if again := drainUnregisterQueue(t, ps.hub); len(again) != 0 {
		t.Errorf("the second Stop asked the hub to unregister %v; it owns no clients", again)
	}
}

// TestPushService_StopWithoutStartReleasesNothing is the other error edge:
// Stop on a service that was never started must not run the sweep at all.
func TestPushService_StopWithoutStartReleasesNothing(t *testing.T) {
	t.Parallel()

	ps := NewPushService(newServiceTestLogger(t), nil)

	if err := ps.Stop(context.Background()); err != nil {
		t.Errorf("Stop without Start: got %v, want nil", err)
	}
	if got := ps.hub.ClientCount(); got != 0 {
		t.Errorf("client count = %d, want 0", got)
	}
	if asked := drainUnregisterQueue(t, ps.hub); len(asked) != 0 {
		t.Errorf("Stop without Start asked the hub to unregister %v", asked)
	}
}

// TestPushService_StopWhoseContextExpiresStillFinishesTheTeardown is the
// regression test for #697.
//
// Stop's wait had an arm that RETURNED ctx.Err() from the middle of the
// function, skipping the subscription sweep and the running flag below it. A
// shutdown with a deadline is the normal way a daemon stops, so the state was
// not exotic: the service stayed marked running with its hub already told to
// stop, admitting Subscribe, Publish and CreateChannel into nothing, its
// subscription clients never released, and Start refusing to restart out of it
// because the flag it checks was still set.
//
// THE DEADLINE STILL REACHES THE CALLER. Finishing the teardown and reporting
// the timeout are not alternatives; the context bounds how long Stop waits for
// the hub, and the error says so after Stop has done what it owed.
func TestPushService_StopWhoseContextExpiresStillFinishesTheTeardown(t *testing.T) {
	t.Parallel()

	ps := NewPushService(newServiceTestLogger(t), nil)
	park := armHubPark(t, ps)
	ctx := context.Background()
	if err := ps.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	subs := []*Subscription{
		mustSubscribe(ctx, t, ps, "alpha"),
		mustSubscribe(ctx, t, ps, "alpha"),
		mustSubscribe(ctx, t, ps, "beta"),
	}
	clients := make([]*Client, 0, len(subs))
	for _, sub := range subs {
		clients = append(clients, mustSubscriptionClient(t, ps, sub.ID))
	}

	// The hub cannot leave, so Stop's wait must give up on it — which is the
	// only way to reach the arm under test.
	park.hold(t, ps)

	stopCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err := ps.Stop(stopCtx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop = %v, want context.Canceled; the caller must still learn the wait was cut short", err)
	}
	if ps.IsRunning() {
		t.Error("the service still reports running after Stop; every call it admits now has no hub behind it")
	}
	for _, client := range clients {
		if client.IsConnected() {
			t.Errorf("subscription client %s was never released", client.ID)
		}
	}
	ps.subscriptions.Range(func(key, _ interface{}) bool {
		t.Errorf("subscription %v survived a Stop whose context expired", key)
		return true
	})

	// RELEASED BEFORE THE REMAINING ASSERTIONS, so that a regression fails
	// rather than hangs. A service that still reports running admits
	// CreateChannel into the hub, and the hub's CreateChannel is synchronous —
	// against a parked loop that is an unbounded wait, which in CI is a
	// timeout with no message rather than the diagnosis below.
	park.unpark()
	require.Eventually(t, func() bool { return !ps.hub.IsRunning() },
		testTimeout, testPollInterval, "the hub's loop never left after the park was released")

	// The flag is not cosmetic: it is what the service's own admission reads.
	if _, err := ps.CreateChannel(context.Background(), "after-the-stop"); !IsServiceNotRunning(err) {
		t.Errorf("CreateChannel after the stop = %v, want service-not-running", err)
	}

	// And the service is not wedged: Start returns early on the running flag,
	// so a Stop that never cleared it made a restart impossible.
	if err := ps.Start(context.Background()); err != nil {
		t.Fatalf("Start after an expired Stop: %v", err)
	}
	t.Cleanup(func() { _ = ps.Stop(context.Background()) })
	if !ps.IsRunning() {
		t.Error("the restarted service does not report running")
	}
}

// TestPushService_StopNamesTheHubItAbandonedAtTheDeadline is the regression
// test for #716: Stop used to break out of its wait when the deadline passed
// and return nil, so an abandoned hub looked like a clean shutdown.
func TestPushService_StopNamesTheHubItAbandonedAtTheDeadline(t *testing.T) {
	t.Parallel()

	ps := NewPushService(newServiceTestLogger(t), nil)
	park := armHubPark(t, ps)
	if err := ps.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	park.hold(t, ps)

	// A deadline already in the past makes the timeout arm the first one Stop
	// reaches, so no clock is waited on.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	err := ps.Stop(ctx)

	var timeout *HubStopTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("Stop = %v, want a HubStopTimeoutError", err)
	}
	if !IsHubStopTimeout(err) || !errors.Is(err, ErrHubStopTimeout) {
		t.Errorf("Stop = %v, want it to match IsHubStopTimeout and ErrHubStopTimeout", err)
	}
	if len(timeout.Hubs) != 1 || timeout.Hubs[0] != "notification-hub" {
		t.Errorf("abandoned hubs = %v, want [notification-hub]", timeout.Hubs)
	}
	if ps.IsRunning() {
		t.Error("the service still reports running after an abandoned-hub Stop")
	}

	park.unpark()
	require.Eventually(t, func() bool { return !ps.hub.IsRunning() },
		testTimeout, testPollInterval, "the hub's loop never left after the park was released")
}

// TestPushService_StopReturnsNilWhenTheHubStopsInTime pins the other half of
// #716: a hub that leaves before the deadline is a clean shutdown.
func TestPushService_StopReturnsNilWhenTheHubStopsInTime(t *testing.T) {
	t.Parallel()

	ps := NewPushService(newServiceTestLogger(t), nil)
	if err := ps.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := ps.Stop(ctx); err != nil {
		t.Errorf("Stop of a healthy hub = %v, want nil", err)
	}
	if IsHubStopTimeout(nil) {
		t.Error("IsHubStopTimeout(nil) = true, want false")
	}
}
