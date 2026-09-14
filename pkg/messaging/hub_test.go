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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
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
		// The loop has to be there before an asynchronous register is admitted:
		// a register issued against a hub with no loop is refused, not queued
		// for whichever loop starts next (#696).
		require.Eventually(t, hub.IsRunning, testTimeout, testPollInterval)

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
		// The loop has to be there before an asynchronous register is admitted:
		// a register issued against a hub with no loop is refused, not queued
		// for whichever loop starts next (#696).
		require.Eventually(t, hub.IsRunning, testTimeout, testPollInterval)

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
		// The loop has to be there before an asynchronous register is admitted:
		// a register issued against a hub with no loop is refused, not queued
		// for whichever loop starts next (#696).
		require.Eventually(t, hub.IsRunning, testTimeout, testPollInterval)

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

// TestNotificationHub_StopIsIdempotent: `if running { close(done) }` is a
// load-then-act, so two callers can both reach the close and the second panics
// — taking down whichever goroutine called Stop rather than the hub.
// PushService.Stop has the same shape above it, and overlapping shutdowns are
// what restarting a daemon produces (#354).
//
// TWO SEQUENTIAL STOPS, NOT A RACE OF EIGHT. Concurrent callers were the
// motivating case, but a race makes a poor gate: eight goroutines did not
// reproduce the double close on an idle machine, which would have made this
// test decoration. The deterministic form is the same defect without the
// timing — the second Stop runs while `running` is still true, because Run
// clears it in a defer that has not executed yet.
//
// Mutation evidence: restore `if h.running.Load() { close(h.done) }` and this
// panics with "close of closed channel" on the second call, every run.
func TestNotificationHub_StopIsIdempotent(t *testing.T) {
	t.Parallel()

	hub := NewNotificationHub(newTestLogger(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)
	require.Eventually(t, hub.IsRunning, testTimeout, testPollInterval)

	// Back to back, so the loop has not yet run its deferred running.Store.
	hub.Stop()
	hub.Stop()

	require.Eventually(t, func() bool { return !hub.IsRunning() }, testTimeout, testPollInterval)
}

// TestNotificationHub_StopDoesNotPoisonALaterRun is the half that is easy to
// lose while fixing the other one, and was caught in review rather than by me.
//
// Making Stop unconditional — a plain sync.Once around close(h.done) — fixes
// the double close and silently breaks something adjacent: a channel closed
// once cannot be reopened, so a Stop issued while nothing is running would
// leave every later Run seeing a closed channel and exiting immediately. Both
// orderings below go through the PUBLIC Stop path, which is the one
// PushService.Stop uses; the existing tests only exercised context
// cancellation and would not have noticed.
//
// TWO INDEPENDENT GUARDS HOLD THIS, and the subtests gate different ones —
// measured, because the first version of this comment claimed one mutation
// that did not kill:
//
//   - "run, stop, run again" gates the RENEWAL. Remove renewDone from Run and
//     it fails: the hub cannot be restarted.
//   - "stop before the first run" gates the INACTIVE NO-OP, and only once the
//     renewal is gone too — with renewal present, a Stop before any Run closes
//     a channel the first Run then replaces, so nothing is observable. Remove
//     both and it fails: the hub never starts.
//
// So the renewal is what makes a restart work, and the no-op is a second,
// independent reason a Stop while idle decides nothing about a later run.
func TestNotificationHub_StopDoesNotPoisonALaterRun(t *testing.T) {
	t.Parallel()

	t.Run("stop before the first run", func(t *testing.T) {
		t.Parallel()
		hub := NewNotificationHub(newTestLogger(t))
		hub.Stop() // nothing is running: this must decide nothing

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)
		require.Eventually(t, hub.IsRunning, testTimeout, testPollInterval,
			"a Stop issued before the first Run left the hub unable to start")
	})

	t.Run("run, stop, run again", func(t *testing.T) {
		t.Parallel()
		hub := NewNotificationHub(newTestLogger(t))

		first, cancelFirst := context.WithCancel(context.Background())
		go hub.Run(first)
		require.Eventually(t, hub.IsRunning, testTimeout, testPollInterval)
		hub.Stop()
		require.Eventually(t, func() bool { return !hub.IsRunning() }, testTimeout, testPollInterval)
		cancelFirst()

		// A service restart: the same hub, a second loop.
		second, cancelSecond := context.WithCancel(context.Background())
		defer cancelSecond()
		go hub.Run(second)
		require.Eventually(t, hub.IsRunning, testTimeout, testPollInterval,
			"the hub could not be restarted after a Stop")
	})
}

// TestNotificationHub_StopIsNotLostRacingStartup gates the lock scope: a Stop
// that observes a running hub must close the channel that hub's loop is
// selecting on.
//
// Split, the claim and the renewal lose a Stop. Run marked the hub running and
// then renewed h.done, so a Stop landing between the two saw a running hub,
// closed the OLD channel and returned — and Run waited on a fresh one. The
// shutdown was accepted and silently discarded.
//
// PROBABILISTIC, AND WORTH IT — stated plainly because this package argues
// elsewhere against buying probability instead of a guarantee. The distinction
// is WHICH WAY it is wrong. A single iteration observes the defect a few percent
// of the time, so 300 of them fire essentially always; and it does not fail
// while the code is correct, because a correct Stop closes the channel the loop
// is on and the loop leaves at once. Measured, both directions: 4 to 11 losses
// per 300 with the claim and renewal split; **0 in 1500** with them joined.
//
// TWO DETAILS DO ALL THE WORK, and an earlier draft got both wrong.
//
//   - THE SPIN IS TIGHT ON PURPOSE. Yielding with runtime.Gosched between
//     checks hands the hub goroutine the chance to finish renewing before Stop
//     is called, which closes the very window under test — that version detected
//     the defect ZERO times in 900 iterations. Spinning hot is how a few
//     instructions of race get hit at all.
//   - THE DEADLINE IS GENEROUS ON PURPOSE. An earlier draft used 200ms, and on
//     a loaded runner a CORRECT shutdown can miss a deadline that tight, which
//     is the wall-clock failure this package has spent the day removing. At
//     testTimeout only a genuinely lost Stop can reach it.
func TestNotificationHub_StopIsNotLostRacingStartup(t *testing.T) {
	t.Parallel()

	const iterations = 300
	for i := 0; i < iterations; i++ {
		hub := NewNotificationHub(newTestLogger(t))
		ctx, cancel := context.WithCancel(context.Background())
		go hub.Run(ctx)

		// Stop the instant the hub claims to be running — the window is
		// between that claim and the renewal.
		for !hub.IsRunning() { //nolint:revive // tight by design; see above
		}
		hub.Stop()

		deadline := time.Now().Add(testTimeout)
		for hub.IsRunning() && time.Now().Before(deadline) { //nolint:revive // tight by design
		}
		stillRunning := hub.IsRunning()
		cancel()
		if stillRunning {
			t.Fatalf("iteration %d: the hub was still running %s after Stop; the stop was accepted and discarded",
				i, testTimeout)
		}
	}
}

// TestNotificationHub_RestartStartsFromACleanSlate is the populated case, and
// it exists because the empty one made it look covered (#354, found in review).
//
// TestNotificationHub_StopDoesNotPoisonALaterRun restarts a hub that never had
// a client or a channel, so it proves the loop starts and nothing more. Every
// piece of state a restart INHERITS is invisible to it. cleanup clears the
// maps, and the counts derived from them were left behind — so a second Run
// began with empty maps and non-zero counts, and one new client reported two.
//
// The defect is older than the restart path; what is new is that the path
// exists at all. Renewing `done` per run turned "a second Run is impossible"
// into "a second Run is supported", and everything a second Run can observe
// became reachable in the same stroke. This test asserts the observable half:
// after a restart the hub reports what it actually holds.
//
// Mutation evidence: remove the two Store(0) calls from cleanup and this fails
// with clients=1 channels=1 before anything has been registered.
func TestNotificationHub_RestartStartsFromACleanSlate(t *testing.T) {
	t.Parallel()

	hub := NewNotificationHub(newTestLogger(t))

	first, cancelFirst := context.WithCancel(context.Background())
	go hub.Run(first)
	require.Eventually(t, hub.IsRunning, testTimeout, testPollInterval)

	// Populate it: a registered client and a channel it subscribes to.
	client := NewClient("before-restart", DefaultBufferSize)
	hub.RegisterClient(client)
	require.Eventually(t, func() bool { return hub.ClientCount() == 1 }, testTimeout, testPollInterval)
	if err := hub.SubscribeToChannel(client, "before-restart-channel"); err != nil {
		t.Fatalf("SubscribeToChannel: %v", err)
	}
	require.Eventually(t, func() bool { return hub.ChannelCount() == 1 }, testTimeout, testPollInterval)

	hub.Stop()
	require.Eventually(t, func() bool { return !hub.IsRunning() }, testTimeout, testPollInterval)
	cancelFirst()

	second, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	go hub.Run(second)
	require.Eventually(t, hub.IsRunning, testTimeout, testPollInterval)

	// What the restarted hub reports must match what it holds: nothing.
	if got := hub.ClientCount(); got != 0 {
		t.Errorf("ClientCount = %d after restart, want 0; the count outlived the clients", got)
	}
	if got := hub.ChannelCount(); got != 0 {
		t.Errorf("ChannelCount = %d after restart, want 0; the count outlived the channels", got)
	}
	if _, found := hub.GetChannel("before-restart-channel"); found {
		t.Error("a channel from before the restart is still reported")
	}
	if listed := hub.ListChannels(); len(listed) != 0 {
		t.Errorf("ListChannels = %d entries after restart, want 0", len(listed))
	}

	// And a fresh registration counts from zero, not from the stale total.
	fresh := NewClient("after-restart", DefaultBufferSize)
	hub.RegisterClient(fresh)
	require.Eventually(t, func() bool { return hub.ClientCount() == 1 }, testTimeout, testPollInterval,
		"one client after a restart should be one, not one plus whatever the previous run left")
}

// TestNotificationHub_EveryQueuedRequestIsAnswerable is #345: a request that
// can be queued must be answerable.
//
// EVERY synchronous call is driven, not the one that was reported. They all
// share the shape — queue into a buffered channel, then read a response only a
// handler writes — so a request still queued when Run returns had no writer
// left and its caller waited forever. Not a timeout: an unbounded wait on a
// channel nobody will ever write to, which on a daemon is a shutdown that never
// completes. Measured before the fix: 6 of 6 hung.
//
// The table is the point. A guard written at one call site would have left the
// other five, and the next call added would have been the sixth again.
//
// WHAT THIS ACTUALLY GATES IS THE REFUSAL, NOT THE DRAIN, and the correction is
// worth more than the original claim. Every row waits for !IsRunning() before it
// calls, so admit refuses each one BEFORE anything is queued and the request
// never reaches awaitHub at all. An earlier version of this comment claimed that
// deleting the h.stopSignal() case from awaitHub made every row hang. That was
// true when it was written and stopped being true when admit's pre-check landed
// in the same PR: re-run against that mutation, all 6 rows pass. The comment had
// outlived the code it described.
//
// The path this one cannot see — a request ACCEPTED while the loop was running
// and then abandoned when Run returned — is gated by
// TestNotificationHub_AnAcceptedRequestIsAnsweredWhenTheLoopAbandonsIt.
func TestNotificationHub_EveryQueuedRequestIsAnswerable(t *testing.T) {
	t.Parallel()

	log := newTestLogger(t)
	hub := NewNotificationHub(log)
	client := NewClient("waiter", DefaultBufferSize)

	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)
	require.Eventually(t, hub.IsRunning, testTimeout, testPollInterval)
	hub.RegisterClient(client)
	require.Eventually(t, func() bool { return hub.ClientCount() == 1 }, testTimeout, testPollInterval)

	// The loop leaves. Everything queued after this has no writer.
	cancel()
	require.Eventually(t, func() bool { return !hub.IsRunning() }, testTimeout, testPollInterval)

	for _, tc := range []struct {
		name string
		call func()
	}{
		{"SubscribeToChannel", func() { _ = hub.SubscribeToChannel(client, "c") }},
		{"UnsubscribeFromChannel", func() { _ = hub.UnsubscribeFromChannel(client, "c") }},
		{"CreateChannel", func() { _, _ = hub.CreateChannel("c") }},
		{"DeleteChannel", func() { _ = hub.DeleteChannel("c") }},
		{"GetChannel", func() { _, _ = hub.GetChannel("c") }},
		{"ListChannels", func() { _ = hub.ListChannels() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			returned := make(chan struct{})
			go func() { tc.call(); close(returned) }()
			select {
			case <-returned:
			case <-time.After(testTimeout):
				t.Fatalf("%s never returned after the hub stopped; its caller waits forever", tc.name)
			}
		})
	}
}

// perQueueBatch is how many callers each synchronous API gets in the abandoned-
// request tests. One per queue would be enough for the forced case; the batch
// exists for the second subtest, where the loop is free to service what it can.
const perQueueBatch = 16

// startHubLoop runs hub's loop and does not return until it reports running.
func startHubLoop(ctx context.Context, t *testing.T, hub *NotificationHub) {
	t.Helper()

	go hub.Run(ctx)
	require.Eventually(t, hub.IsRunning, testTimeout, testPollInterval)
}

// wedgeHubLoop parks an already-running loop inside a handler, so that admission
// stays open while nothing can be serviced.
//
// IT IS THE ONLY WAY LEFT TO QUEUE A REQUEST AND WATCH IT WAIT. Before #345 a
// test could queue against a hub that had not started yet and then start it;
// admit now refuses a request to a hub that is not running, by design, so that
// construction produces a refusal rather than a queued request and any test
// spinning for one spins forever. Wedging a RUNNING loop is the replacement:
// the request is admitted for real, and still cannot be serviced.
//
// The wedge is a getChannel request whose response channel is UNBUFFERED and
// never read: handleGetChannel always sends before returning, so the loop parks
// on that send. Returned release frees it and is safe to call more than once and
// on any exit path, which is what keeps a failed assertion from leaving the hub
// goroutine wedged for the rest of the run.
func wedgeHubLoop(t *testing.T, hub *NotificationHub) (release func()) {
	t.Helper()

	// WAIT OUT RUN'S PREAMBLE BEFORE OFFERING THE WEDGE, or the wedge deadlocks
	// the hub instead of parking it.
	//
	// Run holds lifecycleMu across the claim, the drain of a previous loop's
	// leftovers, and the reopening of admission — and running goes true INSIDE
	// that window, so IsRunning() is already true while the preamble is still
	// mid-flight. A blocker offered in that gap is taken by the PREAMBLE DRAIN
	// rather than by the loop, and discardQueuedRequests then blocks forever
	// sending to a response channel that is deliberately unbuffered and unread
	// — while holding lifecycleMu. Every later admit blocks on that lock, so
	// the symptom is not a parked loop but a hub that accepts nothing, which is
	// how it presented: "request still not queued, hub is running=true".
	//
	// Taking the lock here is the barrier. It cannot be acquired until the
	// preamble has released it, and the caller has already waited for
	// IsRunning(), so Run has certainly claimed by now. After this, nothing but
	// the loop itself reads h.getChannel.
	hub.lifecycleMu.Lock()
	hub.lifecycleMu.Unlock() //nolint:staticcheck // the barrier is the point

	blocker := &getChannelRequest{Name: "blocker", Response: make(chan *getChannelResponse)}
	hub.getChannel <- blocker
	require.Eventually(t, func() bool { return len(hub.getChannel) == 0 }, testTimeout, testPollInterval,
		"the loop never picked up the blocking request, so it is not parked in a handler")

	var once sync.Once
	return func() {
		once.Do(func() {
			select {
			case <-blocker.Response:
			case <-time.After(testTimeout):
			}
		})
	}
}

// parkHubLoop starts hub's loop, registers client, and only then wedges the loop.
//
// The registration happens BEFORE the wedge on purpose: a wedged loop services
// nothing, so a client registered after it would never reach the hub's client
// set and the callers would be exercising a hub with no client.
func parkHubLoop(ctx context.Context, t *testing.T, hub *NotificationHub, client *Client) (release func()) {
	t.Helper()

	startHubLoop(ctx, t, hub)
	hub.RegisterClient(client)
	require.Eventually(t, func() bool { return hub.ClientCount() == 1 }, testTimeout, testPollInterval)

	return wedgeHubLoop(t, hub)
}

// admitBatchWhileParked launches perQueueBatch callers on each synchronous API
// and does not return until every one of them is provably sitting in the hub's
// queues. It reports the channel each caller announces itself on, and the total.
func admitBatchWhileParked(t *testing.T, hub *NotificationHub, client *Client) (returned <-chan string, total int) {
	t.Helper()

	calls := []struct {
		name string
		call func()
	}{
		{"SubscribeToChannel", func() { _ = hub.SubscribeToChannel(client, "c") }},
		{"UnsubscribeFromChannel", func() { _ = hub.UnsubscribeFromChannel(client, "c") }},
		{"CreateChannel", func() { _, _ = hub.CreateChannel("c") }},
		{"DeleteChannel", func() { _ = hub.DeleteChannel("c") }},
		{"GetChannel", func() { _, _ = hub.GetChannel("c") }},
		{"ListChannels", func() { _ = hub.ListChannels() }},
	}

	total = len(calls) * perQueueBatch
	announced := make(chan string, total)
	for _, tc := range calls {
		for i := 0; i < perQueueBatch; i++ {
			go func() { tc.call(); announced <- tc.name }()
		}
	}

	// Provably admitted, not presumed admitted. Each of these requests passed
	// admit while the loop was alive, which is the precondition the whole test
	// depends on; without this the callers could all have been refused and the
	// test would still see them return.
	require.Eventually(t, func() bool {
		return len(hub.subscribe) == perQueueBatch && len(hub.unsubscribe) == perQueueBatch &&
			len(hub.createChannel) == perQueueBatch && len(hub.deleteChannel) == perQueueBatch &&
			len(hub.getChannel) == perQueueBatch && len(hub.listChannels) == perQueueBatch
	}, testTimeout, testPollInterval, "requests were not admitted while the hub was running")

	return announced, total
}

// requireAllAnswered fails with a count rather than hanging when a caller is
// left waiting on a loop that has gone.
func requireAllAnswered(t *testing.T, returned <-chan string, total int) {
	t.Helper()

	answered := make(map[string]int)
	for i := 0; i < total; i++ {
		select {
		case name := <-returned:
			answered[name]++
		case <-time.After(testTimeout):
			t.Fatalf("only %d of %d accepted requests were answered; the rest are waiting on a loop that has gone. answered so far: %v",
				i, total, answered)
		}
	}
}

// TestNotificationHub_AnAcceptedRequestIsAnsweredWhenTheLoopAbandonsIt drives
// the half of #345 that refusal cannot reach: a request admitted while a loop
// was running, and still queued when that loop stopped servicing it.
//
// THE PRECONDITION IS FORCED, NOT WAITED FOR. The loop is wedged inside a
// handler (see parkHubLoop), so every request below is admitted for real and
// none can be serviced. The queue lengths are asserted before anything else
// happens, so "these requests were accepted by a running hub" is established
// rather than assumed — no sleeps, and no polling for a microsecond window.
//
// THE TWO MECHANISMS ARE REDUNDANT BY DESIGN AND NEITHER SUBTEST CAN TELL THEM
// APART. An abandoned request is answered either by discardQueuedRequests, or —
// if that never ran — by awaitHub waking on the stop signal. Measured against
// the forced subtest, each alone: delete awaitHub's stop branch and it passes
// (the drain answers); delete the drain from shutdownAdmission and it passes
// (the wait answers); delete BOTH and it fails, 20 of 20 runs, answering 0 of 96
// callers. Control, unmutated: 20 of 20 pass. So the honest claim is that it
// gates the pair. Attributing the property to one of them is beyond any test
// that observes only what the caller sees, because the two are meant to produce
// the same answer — and the control is what distinguishes that failure from a
// hang this test's own construction could have caused.
func TestNotificationHub_AnAcceptedRequestIsAnsweredWhenTheLoopAbandonsIt(t *testing.T) {
	t.Parallel()

	// THIS SUBTEST IS THE GATE, and it is deterministic. Shutting admission down
	// while the loop is still wedged reproduces the state Run's own exit
	// produces — admission closed, running still true, requests in the queues —
	// without depending on which case a select happens to choose.
	t.Run("shutdown abandons them", func(t *testing.T) {
		t.Parallel()

		hub := NewNotificationHub(newTestLogger(t))
		client := NewClient("waiter", DefaultBufferSize)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		release := parkHubLoop(ctx, t, hub, client)
		defer release()

		returned, total := admitBatchWhileParked(t, hub, client)

		// The production helper Run defers, called at the one moment its effect
		// is unambiguous. Every caller below was admitted before this line.
		hub.shutdownAdmission()

		requireAllAnswered(t, returned, total)
	})

	// THIS SUBTEST IS NOT THE GATE, and says so because it cannot be one. It
	// drives the same property through Run's real exit, which is what production
	// does — but on release the loop faces a select where cancellation and every
	// queued request are ready at once, and Go chooses uniformly. It may service
	// all of them and abandon none, in which case this passes without exercising
	// anything. Kept because the realistic path is worth running; never cited as
	// evidence, because it can pass for the wrong reason.
	t.Run("run exit abandons them - not the gate", func(t *testing.T) {
		t.Parallel()

		hub := NewNotificationHub(newTestLogger(t))
		client := NewClient("waiter", DefaultBufferSize)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		release := parkHubLoop(ctx, t, hub, client)
		defer release()

		returned, total := admitBatchWhileParked(t, hub, client)

		cancel()
		release()

		requireAllAnswered(t, returned, total)
	})
}

// TestNotificationHub_ADrainedRequestNamesTheCallTheCallerMade found in review
// of the rebase, and it is the shutdown half of #339's attribution.
//
// Two exported calls share h.subscribe, and the drain sees only what is in the
// queue. Hard-coding an op there made a drained RegisterAndSubscribe report a
// SubscribeToChannel the caller never made — right on the admission path and
// wrong on the shutdown path, which is the worse of the two places to be wrong,
// because it only misleads once something has already gone wrong.
//
// THE DRAIN IS CALLED DIRECTLY, AND THAT IS WHAT MAKES THIS DETERMINISTIC.
// Going through shutdownAdmission would close the stop signal first, and
// awaitHub would then race the drain: waking on the signal returns an error
// carrying awaitHub's own op, which is correct whether or not the drain is, so
// the test would pass against the defect about half the time. Draining while
// admission is still open leaves the drained answer as the only one available.
//
// Mutation evidence: restore stopped("push.SubscribeToChannel") in
// discardQueuedRequests and this fails, naming the op it got.
func TestNotificationHub_ADrainedRequestNamesTheCallTheCallerMade(t *testing.T) {
	t.Parallel()

	hub := NewNotificationHub(newTestLogger(t))
	client := NewClient("racer", DefaultBufferSize)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startHubLoop(ctx, t, hub)
	release := wedgeHubLoop(t, hub)
	defer release()

	result := make(chan error, 1)
	go func() { result <- hub.RegisterAndSubscribe(client, "ordered-channel") }()
	require.Eventually(t, func() bool { return len(hub.subscribe) == 1 }, testTimeout, testPollInterval,
		"the request was never admitted, so there was nothing to drain")

	hub.discardQueuedRequests()

	select {
	case err := <-result:
		var qberr *qberrors.QBError
		require.ErrorAs(t, err, &qberr, "a drained request should report a QBError")
		require.Equal(t, "push.RegisterAndSubscribe", qberr.Op,
			"the drain named an operation the caller never called")
	case <-time.After(testTimeout):
		t.Fatal("the drained request was never answered")
	}
}

// TestNotificationHub_AStoppedHubReportsItselfRatherThanGuessing: the answer a
// waiter gets when the loop is gone should name that, so a caller can tell
// "the hub is down" from "your request was refused".
func TestNotificationHub_AStoppedHubReportsItselfRatherThanGuessing(t *testing.T) {
	t.Parallel()

	log := newTestLogger(t)
	hub := NewNotificationHub(log)
	client := NewClient("waiter", DefaultBufferSize)

	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)
	require.Eventually(t, hub.IsRunning, testTimeout, testPollInterval)
	hub.RegisterClient(client)
	require.Eventually(t, func() bool { return hub.ClientCount() == 1 }, testTimeout, testPollInterval)
	cancel()
	require.Eventually(t, func() bool { return !hub.IsRunning() }, testTimeout, testPollInterval)

	err := hub.SubscribeToChannel(client, "c")
	if !errors.Is(err, ErrHubStopped) {
		t.Fatalf("err = %v, want ErrHubStopped", err)
	}
}

// TestNotificationHub_NeverStartedRefusesRatherThanHangs: "not running yet" and
// "not running any more" are the same answer to the only question a waiter is
// asking, which is why the signal starts closed.
func TestNotificationHub_NeverStartedRefusesRatherThanHangs(t *testing.T) {
	t.Parallel()

	hub := NewNotificationHub(newTestLogger(t))
	client := NewClient("waiter", DefaultBufferSize)

	returned := make(chan error, 1)
	go func() { returned <- hub.SubscribeToChannel(client, "c") }()
	select {
	case err := <-returned:
		if !errors.Is(err, ErrHubStopped) {
			t.Fatalf("err = %v, want ErrHubStopped", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("SubscribeToChannel hung against a hub that was never started")
	}
}

// TestNotificationHub_ARefusedRequestIsNeverPerformedLater is the hole a
// stop-signal alone leaves, found in review of #345.
//
// Answering "the hub stopped" while the request sits in a buffered channel is
// worse than the hang it replaces: PushService.Start re-runs Run on the SAME
// hub, so the next loop would perform work whose caller was already told it had
// failed. A hang is at least honest. Refusing before the enqueue, and draining
// the queues at both ends of a loop, is what makes the refusal true.
//
// WHAT THIS GATES IS THE PAIR, and an earlier version of this comment claimed
// otherwise. It said the way-IN drain was what made the refusal true and the
// pre-check merely earned its place elsewhere. That was measured before admit
// centralised the pre-check, and admit invalidated it: re-measured, removing
// EITHER mechanism alone leaves this test passing, and only removing both fails
// it — the ghost channel then exists after the restart despite its caller having
// been told it did not.
//
// They cover for each other. Refusing at admission means nothing stale is ever
// queued; draining on the way in means anything stale that was queued is thrown
// away before the new loop can perform it. Each alone is sufficient here, which
// is why neither shows up as load-bearing on its own, and why no test that
// watches only the caller can attribute the property to one of them.
//
// The pre-check does carry something alone, and that is measurable separately:
// remove it and TestNotificationHub_AStoppedHubKeepsSayingStoppedRatherThanOverloaded
// fails, because a stopped hub then fills its queues and reports overload
// instead of naming itself.
func TestNotificationHub_ARefusedRequestIsNeverPerformedLater(t *testing.T) {
	t.Parallel()

	log := newTestLogger(t)
	hub := NewNotificationHub(log)

	// One loop runs and stops, the way a service Start/Stop cycle does.
	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)
	require.Eventually(t, hub.IsRunning, testTimeout, testPollInterval)
	cancel()
	require.Eventually(t, func() bool { return !hub.IsRunning() }, testTimeout, testPollInterval)

	// A caller asks while nothing is running and is refused.
	if _, err := hub.CreateChannel("ghost-channel"); !errors.Is(err, ErrHubStopped) {
		t.Fatalf("CreateChannel err = %v, want ErrHubStopped", err)
	}

	// The service restarts. The refused work must NOT be waiting to happen.
	restartCtx, restartCancel := context.WithCancel(context.Background())
	defer restartCancel()
	go hub.Run(restartCtx)
	require.Eventually(t, hub.IsRunning, testTimeout, testPollInterval)

	// Give a stale request every chance to be picked up before we look.
	require.Never(t, func() bool {
		_, found := hub.GetChannel("ghost-channel")
		return found
	}, 250*time.Millisecond, testPollInterval,
		"a channel the caller was told was not created exists after the restart")
}

// TestNotificationHub_AStoppedHubKeepsSayingStoppedRatherThanOverloaded is what
// the pre-check buys, stated as what it actually does rather than what it looks
// like it does.
//
// Refusing before the enqueue is NOT what keeps a refused request from being
// performed later — the drain does that. What it prevents is a stopped hub
// filling its own 256-slot queues with requests nobody will ever handle and
// then reporting ErrHubOverloaded, which sends a reader looking for load that
// does not exist. Measured: with the pre-check, 306 of 306 calls report
// stopped; without it, 256 do and the remaining 50 report overloaded.
func TestNotificationHub_AStoppedHubKeepsSayingStoppedRatherThanOverloaded(t *testing.T) {
	t.Parallel()

	hub := NewNotificationHub(newTestLogger(t))
	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)
	require.Eventually(t, hub.IsRunning, testTimeout, testPollInterval)
	cancel()
	require.Eventually(t, func() bool { return !hub.IsRunning() }, testTimeout, testPollInterval)

	// Past the queue's capacity, so an unguarded enqueue would start reporting
	// the queue rather than the hub.
	for i := 0; i < DefaultBufferSize+50; i++ {
		if _, err := hub.CreateChannel("c"); !errors.Is(err, ErrHubStopped) {
			t.Fatalf("call %d: err = %v, want ErrHubStopped; a stopped hub reported its queue instead of itself", i, err)
		}
	}
}

// TestNotificationHub_ChannelMissesAnswerTheirOwnPredicate is the end-to-end
// half of #695: the errors the hub itself produces must satisfy the public
// predicate that exists to classify them.
//
// THE TABLE-DRIVEN TEST NEXT DOOR CANNOT SEE THIS. It asserts the shape the
// hub is believed to build; this asserts the shape the hub actually builds, and
// #695 was exactly a disagreement between the two. Both handlers that can miss
// a channel are driven, because they construct their refusals separately and a
// fix applied to one predicate is no guarantee about the other's error.
func TestNotificationHub_ChannelMissesAnswerTheirOwnPredicate(t *testing.T) {
	t.Parallel()

	hub := NewNotificationHub(newTestLogger(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)
	require.Eventually(t, hub.IsRunning, testTimeout, testPollInterval)

	client := NewClient("predicate-probe", DefaultBufferSize)
	require.NoError(t, hub.RegisterAndSubscribe(client, "a-channel-that-exists"))

	t.Run("DeleteChannel", func(t *testing.T) {
		err := hub.DeleteChannel("never-created")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrChannelNotFound)
		require.True(t, IsChannelNotFound(err),
			"the hub reported %v and IsChannelNotFound denied it", err)
	})

	t.Run("UnsubscribeFromChannel", func(t *testing.T) {
		err := hub.UnsubscribeFromChannel(client, "never-created")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrChannelNotFound)
		require.True(t, IsChannelNotFound(err),
			"the hub reported %v and IsChannelNotFound denied it", err)
	})

	t.Run("a channel that exists is not reported as a miss", func(t *testing.T) {
		err := hub.DeleteChannel("a-channel-that-exists")
		require.NoError(t, err)
		require.False(t, IsChannelNotFound(err))
	})
}

// TestNotificationHub_RegistrationQueuesDoNotCrossLoops is the regression test
// for #696.
//
// Every other request goes through admit and is drained by
// discardQueuedRequests; the register and unregister queues did neither. Both
// are buffered, so their contents outlived the loop they were meant for, and a
// register queued against a departed loop was handed to the NEXT one — which
// inserted a client whose caller had been told the hub had stopped, and which
// the previous loop's cleanup had already closed, into a fresh registry and
// counted it.
//
// THE BARRIER IN EACH CASE IS A SYNCHRONOUS CALL, not IsRunning. Run marks the
// hub running BEFORE it discards the previous loop's leftovers, so IsRunning
// going true says nothing about the drain. A call that admit accepts has waited
// on the lifecycle lock Run holds across the whole claim, so it cannot be
// observed until the drain is done.
func TestNotificationHub_RegistrationQueuesDoNotCrossLoops(t *testing.T) {
	t.Parallel()

	// runningHub starts a loop and returns once admission is open, which is
	// strictly after Run's entry drain.
	runningHub := func(t *testing.T, hub *NotificationHub, barrier string) context.CancelFunc {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		go hub.Run(ctx)
		require.Eventually(t, hub.IsRunning, testTimeout, testPollInterval)
		_, err := hub.CreateChannel(barrier)
		require.NoError(t, err, "the loop did not reach a state where it answers")
		return cancel
	}

	t.Run("a register issued after the loop has left is refused, not queued", func(t *testing.T) {
		t.Parallel()
		hub := NewNotificationHub(newTestLogger(t))

		cancel := runningHub(t, hub, "first")
		cancel()
		require.Eventually(t, func() bool { return !hub.IsRunning() }, testTimeout, testPollInterval)

		// The client its owner has already given up on, which is what the hub
		// closed on its way out.
		victim := NewClient("registered-too-late", DefaultBufferSize)
		victim.Close()
		hub.RegisterClient(victim)

		// Safe to read: no loop exists to consume it.
		require.Zero(t, len(hub.register),
			"a register was queued for a hub with no loop to handle it")

		cancelNext := runningHub(t, hub, "second")
		defer cancelNext()

		require.Equal(t, 0, hub.ClientCount(),
			"the next loop counted a client its caller was told the hub had refused")
		require.ErrorIs(t, hub.SubscribeToChannel(victim, "second"), ErrClientNotRegistered,
			"the next loop's registry holds a client from before it started")
	})

	t.Run("a register left by a previous loop is discarded rather than performed", func(t *testing.T) {
		t.Parallel()
		hub := NewNotificationHub(newTestLogger(t))

		// ENQUEUED DIRECTLY, BECAUSE THE PUBLIC API CANNOT PRODUCE THIS STATE ON
		// DEMAND. It is the state a live loop leaves behind when it is stopped
		// with a register still in flight — but at that instant the loop's
		// select has both `done` and the register ready and chooses uniformly
		// at random, so a test that queued one through RegisterClient would be
		// asserting a coin flip. The queue's contents are the whole subject
		// here, so the queue is where the test starts.
		ghost := NewClient("ghost-from-a-previous-loop", DefaultBufferSize)
		ghost.Close()
		hub.register <- ghost

		cancel := runningHub(t, hub, "fresh")
		defer cancel()

		require.Equal(t, 0, hub.ClientCount(),
			"a leftover registration was performed by a loop its caller never reached")
		require.ErrorIs(t, hub.SubscribeToChannel(ghost, "fresh"), ErrClientNotRegistered,
			"a closed client from a previous loop is in the new loop's registry")
	})

	t.Run("a discarded unregister still closes its client", func(t *testing.T) {
		t.Parallel()
		hub := NewNotificationHub(newTestLogger(t))

		orphan := NewClient("orphaned-unregister", DefaultBufferSize)
		hub.unregister <- orphan
		require.True(t, orphan.IsConnected(), "precondition: the client starts connected")

		cancel := runningHub(t, hub, "fresh")
		defer cancel()

		require.False(t, orphan.IsConnected(),
			"the drain dropped an unregister without doing the half of it that needs no registry")
	})

	t.Run("an unregister refused by a stopped hub still closes its client", func(t *testing.T) {
		t.Parallel()
		hub := NewNotificationHub(newTestLogger(t))

		cancel := runningHub(t, hub, "first")
		cancel()
		require.Eventually(t, func() bool { return !hub.IsRunning() }, testTimeout, testPollInterval)

		// Refusing and discarding are the same situation reached by different
		// races, so they owe the caller the same thing.
		orphan := NewClient("unregistered-too-late", DefaultBufferSize)
		hub.UnregisterClient(orphan)

		require.Zero(t, len(hub.unregister),
			"an unregister was queued for a hub with no loop to handle it")
		require.False(t, orphan.IsConnected(),
			"a refused unregister left its client connected; a discarded one closes it")
	})
}
