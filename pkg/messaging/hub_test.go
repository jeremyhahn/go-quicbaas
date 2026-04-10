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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

const (
	// testTimeout is the maximum time to wait for async operations in tests.
	testTimeout = 2 * time.Second
	// testPollInterval is the polling interval for Eventually assertions.
	testPollInterval = 10 * time.Millisecond
)

// newTestLogger creates a logger suitable for testing.
func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "console",
		Output: "stdout",
	})
	require.NoError(t, err)
	return log
}

func TestNewNotificationHub(t *testing.T) {
	t.Parallel()

	t.Run("creates hub with default buffer size", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		require.NotNil(t, hub)
		assert.False(t, hub.IsRunning())
		assert.Equal(t, 0, hub.ClientCount())
		assert.Equal(t, 0, hub.ChannelCount())
	})

	t.Run("creates hub with custom buffer size", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHubWithBufferSize(log, 512)

		require.NotNil(t, hub)
		assert.Equal(t, 512, hub.bufferSize)
	})

	t.Run("uses default buffer size for invalid value", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHubWithBufferSize(log, 0)

		require.NotNil(t, hub)
		assert.Equal(t, DefaultBufferSize, hub.bufferSize)
	})

	t.Run("uses default buffer size for negative value", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHubWithBufferSize(log, -10)

		require.NotNil(t, hub)
		assert.Equal(t, DefaultBufferSize, hub.bufferSize)
	})
}

func TestNotificationHub_RunStop(t *testing.T) {
	t.Parallel()

	t.Run("starts and stops cleanly", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Signal channel to know when Run exits
		done := make(chan struct{})
		go func() {
			hub.Run(ctx)
			close(done)
		}()

		// Wait for hub to start
		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		// Stop via context cancellation
		cancel()

		// Wait for hub to stop
		select {
		case <-done:
			// Hub stopped
		case <-time.After(testTimeout):
			t.Fatal("hub did not stop in time")
		}

		assert.False(t, hub.IsRunning())
	})

	t.Run("stops via Stop method", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx := context.Background()
		done := make(chan struct{})
		go func() {
			hub.Run(ctx)
			close(done)
		}()

		// Wait for hub to start
		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		// Stop via Stop method
		hub.Stop()

		// Wait for hub to stop
		select {
		case <-done:
			// Expected
		case <-time.After(testTimeout):
			t.Fatal("hub did not stop in time")
		}
		assert.False(t, hub.IsRunning())
	})

	t.Run("does not start twice", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		done1 := make(chan struct{})
		go func() {
			hub.Run(ctx)
			close(done1)
		}()

		// Wait for hub to start
		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		// Second Run should return immediately
		done2 := make(chan struct{})
		go func() {
			hub.Run(ctx)
			close(done2)
		}()

		// Second Run should complete quickly since hub is already running
		select {
		case <-done2:
			// Expected
		case <-time.After(testTimeout):
			t.Fatal("second Run did not return")
		}

		// First Run should still be running
		assert.True(t, hub.IsRunning())

		cancel()
		<-done1
	})
}

func TestNotificationHub_CreateChannel(t *testing.T) {
	t.Parallel()

	t.Run("creates channel successfully", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		ch, err := hub.CreateChannel("test-channel")
		require.NoError(t, err)
		require.NotNil(t, ch)
		assert.Equal(t, "test-channel", ch.Name)
		assert.Equal(t, 0, ch.SubscriberCount())
		assert.Equal(t, 1, hub.ChannelCount())
	})

	t.Run("creates channel with options", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		ch, err := hub.CreateChannel("test-channel",
			WithBufferSize(128),
			WithMaxSubscribers(10),
			WithPersistent(true),
		)
		require.NoError(t, err)
		require.NotNil(t, ch)
		assert.Equal(t, 128, ch.Options.BufferSize)
		assert.Equal(t, 10, ch.Options.MaxSubscribers)
		assert.True(t, ch.Options.Persistent)
	})

	t.Run("returns error for empty channel name", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		ch, err := hub.CreateChannel("")
		require.Error(t, err)
		assert.Nil(t, ch)
		assert.True(t, errors.Is(err, ErrEmptyChannelName))
	})

	t.Run("returns error for duplicate channel", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		_, err := hub.CreateChannel("test-channel")
		require.NoError(t, err)

		ch, err := hub.CreateChannel("test-channel")
		require.Error(t, err)
		assert.Nil(t, ch)
		assert.True(t, errors.Is(err, ErrChannelExists))
	})

	t.Run("retrieves channel info", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		_, err := hub.CreateChannel("test-channel")
		require.NoError(t, err)

		ch, found := hub.GetChannel("test-channel")
		require.True(t, found)
		assert.Equal(t, "test-channel", ch.Name)
	})

	t.Run("returns not found for non-existent channel", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		ch, found := hub.GetChannel("non-existent")
		assert.False(t, found)
		assert.Nil(t, ch)
	})

	t.Run("lists all channels", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		_, err := hub.CreateChannel("channel-1")
		require.NoError(t, err)
		_, err = hub.CreateChannel("channel-2")
		require.NoError(t, err)

		channels := hub.ListChannels()
		assert.Len(t, channels, 2)
	})

	t.Run("deletes channel", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		_, err := hub.CreateChannel("test-channel")
		require.NoError(t, err)
		assert.Equal(t, 1, hub.ChannelCount())

		err = hub.DeleteChannel("test-channel")
		require.NoError(t, err)
		assert.Equal(t, 0, hub.ChannelCount())
	})

	t.Run("delete non-existent channel returns error", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		err := hub.DeleteChannel("non-existent")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrChannelNotFound))
	})
}

func TestNotificationHub_SubscribeToChannel(t *testing.T) {
	t.Parallel()

	t.Run("subscribes to existing channel", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewClient("client-1", DefaultBufferSize)
		hub.RegisterClient(client)

		// Wait for registration
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		_, err := hub.CreateChannel("test-channel")
		require.NoError(t, err)

		err = hub.SubscribeToChannel(client, "test-channel")
		require.NoError(t, err)

		ch, found := hub.GetChannel("test-channel")
		require.True(t, found)
		assert.Equal(t, 1, ch.SubscriberCount())
	})

	t.Run("auto-creates channel on subscribe", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewClient("client-1", DefaultBufferSize)
		hub.RegisterClient(client)

		// Wait for registration
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		err := hub.SubscribeToChannel(client, "auto-created-channel")
		require.NoError(t, err)

		ch, found := hub.GetChannel("auto-created-channel")
		require.True(t, found)
		assert.Equal(t, 1, ch.SubscriberCount())
	})

	t.Run("returns error for nil client", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		err := hub.SubscribeToChannel(nil, "test-channel")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNilClient))
	})

	t.Run("returns error for empty channel name", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewClient("client-1", DefaultBufferSize)
		hub.RegisterClient(client)

		err := hub.SubscribeToChannel(client, "")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrEmptyChannelName))
	})

	t.Run("returns error for unregistered client", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewClient("unregistered-client", DefaultBufferSize)

		err := hub.SubscribeToChannel(client, "test-channel")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrClientNotRegistered))
	})

	t.Run("enforces max subscribers limit", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		_, err := hub.CreateChannel("limited-channel", WithMaxSubscribers(1))
		require.NoError(t, err)

		client1 := NewClient("client-1", DefaultBufferSize)
		hub.RegisterClient(client1)

		// Wait for registration
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		err = hub.SubscribeToChannel(client1, "limited-channel")
		require.NoError(t, err)

		client2 := NewClient("client-2", DefaultBufferSize)
		hub.RegisterClient(client2)

		// Wait for registration
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 2
		}, testTimeout, testPollInterval)

		err = hub.SubscribeToChannel(client2, "limited-channel")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrMaxSubscribersReached))
	})

	t.Run("unsubscribes from channel", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewClient("client-1", DefaultBufferSize)
		hub.RegisterClient(client)

		// Wait for registration
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		_, err := hub.CreateChannel("test-channel")
		require.NoError(t, err)

		err = hub.SubscribeToChannel(client, "test-channel")
		require.NoError(t, err)

		err = hub.UnsubscribeFromChannel(client, "test-channel")
		require.NoError(t, err)

		ch, found := hub.GetChannel("test-channel")
		require.True(t, found)
		assert.Equal(t, 0, ch.SubscriberCount())
	})

	t.Run("unsubscribe returns error for non-existent channel", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewClient("client-1", DefaultBufferSize)
		hub.RegisterClient(client)

		// Wait for registration
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		err := hub.UnsubscribeFromChannel(client, "non-existent")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrChannelNotFound))
	})
}

func TestNotificationHub_Broadcast(t *testing.T) {
	t.Parallel()

	t.Run("broadcasts to channel subscribers", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewClient("client-1", DefaultBufferSize)
		hub.RegisterClient(client)

		// Wait for registration
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		_, err := hub.CreateChannel("test-channel")
		require.NoError(t, err)

		err = hub.SubscribeToChannel(client, "test-channel")
		require.NoError(t, err)

		msg := NewNotificationMessage("test-channel", []byte("hello"))
		err = hub.Broadcast(msg)
		require.NoError(t, err)

		// Receive message
		select {
		case received := <-client.Send:
			assert.Equal(t, "test-channel", received.Channel)
			assert.Equal(t, []byte("hello"), received.Payload)
		case <-time.After(testTimeout):
			t.Fatal("did not receive message")
		}
	})

	t.Run("broadcasts to all clients when no channel specified", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client1 := NewClient("client-1", DefaultBufferSize)
		client2 := NewClient("client-2", DefaultBufferSize)
		hub.RegisterClient(client1)
		hub.RegisterClient(client2)

		// Wait for registrations
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 2
		}, testTimeout, testPollInterval)

		msg := &Message{
			Type:    MessageTypeNotification,
			Payload: []byte("broadcast to all"),
		}
		err := hub.Broadcast(msg)
		require.NoError(t, err)

		// Both clients should receive the message
		select {
		case received := <-client1.Send:
			assert.Equal(t, []byte("broadcast to all"), received.Payload)
		case <-time.After(testTimeout):
			t.Fatal("client1 did not receive message")
		}

		select {
		case received := <-client2.Send:
			assert.Equal(t, []byte("broadcast to all"), received.Payload)
		case <-time.After(testTimeout):
			t.Fatal("client2 did not receive message")
		}
	})

	t.Run("returns error for nil message", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		err := hub.Broadcast(nil)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNilMessage))
	})

	t.Run("sets timestamp if not provided", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewClient("client-1", DefaultBufferSize)
		hub.RegisterClient(client)

		msg := &Message{
			Type:    MessageTypeNotification,
			Payload: []byte("test"),
		}
		assert.True(t, msg.Timestamp.IsZero())

		err := hub.Broadcast(msg)
		require.NoError(t, err)

		assert.False(t, msg.Timestamp.IsZero())
	})

	t.Run("does not deliver to non-subscribed clients", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		subscribedClient := NewClient("subscribed", DefaultBufferSize)
		nonSubscribedClient := NewClient("non-subscribed", DefaultBufferSize)
		hub.RegisterClient(subscribedClient)
		hub.RegisterClient(nonSubscribedClient)

		// Wait for registrations
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 2
		}, testTimeout, testPollInterval)

		_, err := hub.CreateChannel("test-channel")
		require.NoError(t, err)

		err = hub.SubscribeToChannel(subscribedClient, "test-channel")
		require.NoError(t, err)

		msg := NewNotificationMessage("test-channel", []byte("hello"))
		err = hub.Broadcast(msg)
		require.NoError(t, err)

		// Subscribed client receives message
		select {
		case <-subscribedClient.Send:
			// Expected
		case <-time.After(testTimeout):
			t.Fatal("subscribed client did not receive message")
		}

		// Non-subscribed client should not receive message
		select {
		case <-nonSubscribedClient.Send:
			t.Fatal("non-subscribed client should not receive message")
		default:
			// Expected - no message
		}
	})
}

func TestClient_NewClient(t *testing.T) {
	t.Parallel()

	t.Run("creates client with specified buffer size", func(t *testing.T) {
		t.Parallel()
		client := NewClient("test-client", 128)

		require.NotNil(t, client)
		assert.Equal(t, "test-client", client.ID)
		assert.True(t, client.IsConnected())
		assert.NotNil(t, client.Send)
		assert.NotNil(t, client.Done)
		assert.NotNil(t, client.Metadata)
	})

	t.Run("uses default buffer size for zero value", func(t *testing.T) {
		t.Parallel()
		client := NewClient("test-client", 0)

		require.NotNil(t, client)
		// Channel was created with default buffer size
		assert.Equal(t, DefaultBufferSize, cap(client.Send))
	})

	t.Run("uses default buffer size for negative value", func(t *testing.T) {
		t.Parallel()
		client := NewClient("test-client", -10)

		require.NotNil(t, client)
		assert.Equal(t, DefaultBufferSize, cap(client.Send))
	})

	t.Run("close marks client as disconnected", func(t *testing.T) {
		t.Parallel()
		client := NewClient("test-client", DefaultBufferSize)

		assert.True(t, client.IsConnected())
		client.Close()
		assert.False(t, client.IsConnected())
	})

	t.Run("close is idempotent", func(t *testing.T) {
		t.Parallel()
		client := NewClient("test-client", DefaultBufferSize)

		client.Close()
		assert.False(t, client.IsConnected())

		// Second close should not panic
		client.Close()
		assert.False(t, client.IsConnected())
	})

	t.Run("done channel closes on client close", func(t *testing.T) {
		t.Parallel()
		client := NewClient("test-client", DefaultBufferSize)

		client.Close()

		select {
		case <-client.Done:
			// Expected - channel is closed
		default:
			t.Fatal("Done channel should be closed")
		}
	})
}

func TestNotificationHub_ClientRegistration(t *testing.T) {
	t.Parallel()

	t.Run("registers and unregisters client", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewClient("client-1", DefaultBufferSize)
		hub.RegisterClient(client)

		// Wait for registration to be processed
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		hub.UnregisterClient(client)

		// Wait for unregistration to be processed
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 0
		}, testTimeout, testPollInterval)
	})

	t.Run("ignores nil client registration", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		hub.RegisterClient(nil)

		// Should not crash and client count should remain 0
		assert.Equal(t, 0, hub.ClientCount())
	})

	t.Run("ignores nil client unregistration", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		hub.UnregisterClient(nil)

		// Should not crash
		assert.Equal(t, 0, hub.ClientCount())
	})

	t.Run("unregister removes client from all channels", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		hub := NewNotificationHub(log)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewClient("client-1", DefaultBufferSize)
		hub.RegisterClient(client)

		// Wait for registration
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		_, err := hub.CreateChannel("channel-1")
		require.NoError(t, err)
		_, err = hub.CreateChannel("channel-2")
		require.NoError(t, err)

		err = hub.SubscribeToChannel(client, "channel-1")
		require.NoError(t, err)
		err = hub.SubscribeToChannel(client, "channel-2")
		require.NoError(t, err)

		hub.UnregisterClient(client)

		// Wait for unregistration
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 0
		}, testTimeout, testPollInterval)

		// Verify subscriber counts are updated
		ch1, _ := hub.GetChannel("channel-1")
		ch2, _ := hub.GetChannel("channel-2")
		assert.Equal(t, 0, ch1.SubscriberCount())
		assert.Equal(t, 0, ch2.SubscriberCount())
	})
}

func TestNotificationHub_SlowConsumer(t *testing.T) {
	t.Parallel()

	t.Run("disconnects slow consumer", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		hub := NewNotificationHubWithBufferSize(log, 256)
		go hub.Run(ctx)

		// Create a client with a very small send buffer to trigger slow consumer
		// Use NewClient to properly initialize with connected=true
		client := NewClient("slow-client", 1) // Very small buffer

		hub.RegisterClient(client)
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		_, err := hub.CreateChannel("test-channel")
		require.NoError(t, err)

		err = hub.SubscribeToChannel(client, "test-channel")
		require.NoError(t, err)

		// Fill the client's send buffer to trigger slow consumer detection
		// First message goes through
		client.Send <- &Message{ID: "fill-1"}

		// Now broadcast many messages to trigger slow consumer detection
		for i := 0; i < 10; i++ {
			msg := &Message{
				ID:      "msg-" + string(rune('0'+i)),
				Channel: "test-channel",
				Payload: []byte(`{"test":"data"}`),
			}
			hub.Broadcast(msg)
		}

		// The slow consumer should eventually be disconnected
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 0
		}, testTimeout, testPollInterval)
	})
}

func TestNotificationHub_RegisterAlreadyRegisteredClient(t *testing.T) {
	t.Parallel()

	t.Run("handles registering same client twice", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		hub := NewNotificationHubWithBufferSize(log, 256)
		go hub.Run(ctx)

		client := NewClient("client-1", 256)

		// Register the client once
		hub.RegisterClient(client)
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		// Register the same client again - should not increase count
		hub.RegisterClient(client)

		// Small delay to let the duplicate registration be processed
		time.Sleep(50 * time.Millisecond)

		// Count should still be 1
		assert.Equal(t, 1, hub.ClientCount())
	})
}

func TestNotificationHub_DeleteNonExistentChannel(t *testing.T) {
	t.Parallel()

	t.Run("returns error for non-existent channel", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		hub := NewNotificationHubWithBufferSize(log, 256)
		go hub.Run(ctx)

		err := hub.DeleteChannel("non-existent")
		require.Error(t, err)
	})
}

func TestNotificationHub_GetChannelNotFound(t *testing.T) {
	t.Parallel()

	t.Run("returns nil for non-existent channel", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		hub := NewNotificationHubWithBufferSize(log, 256)
		go hub.Run(ctx)

		ch, found := hub.GetChannel("non-existent")
		assert.False(t, found)
		assert.Nil(t, ch)
	})
}

func TestNotificationHub_UnsubscribeFromNonExistentChannel(t *testing.T) {
	t.Parallel()

	t.Run("returns error when unsubscribing from non-existent channel", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		hub := NewNotificationHubWithBufferSize(log, 256)
		go hub.Run(ctx)

		client := NewClient("client-1", 256)

		hub.RegisterClient(client)
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		err := hub.UnsubscribeFromChannel(client, "non-existent")
		require.Error(t, err)
	})

	t.Run("returns error for nil client", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		hub := NewNotificationHubWithBufferSize(log, 256)
		go hub.Run(ctx)

		err := hub.UnsubscribeFromChannel(nil, "test-channel")
		require.Error(t, err)
	})

	t.Run("returns error for empty channel name", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		hub := NewNotificationHubWithBufferSize(log, 256)
		go hub.Run(ctx)

		client := NewClient("client-1", 256)
		err := hub.UnsubscribeFromChannel(client, "")
		require.Error(t, err)
	})
}

func TestNotificationHub_ListChannelsEmpty(t *testing.T) {
	t.Parallel()

	t.Run("returns empty list when no channels exist", func(t *testing.T) {
		t.Parallel()
		log := newTestLogger(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		hub := NewNotificationHubWithBufferSize(log, 256)
		go hub.Run(ctx)

		channels := hub.ListChannels()
		assert.Empty(t, channels)
	})
}
