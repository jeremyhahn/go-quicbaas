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
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// mockClientConnection implements ClientConnection for testing.
type mockClientConnection struct {
	mu           sync.Mutex
	readData     [][]byte
	readIndex    int
	readErr      error
	readBlocking bool
	readCh       chan []byte
	writtenData  [][]byte
	writeErr     error
	closed       bool
	closeErr     error
	closeCalled  int
	readDelay    time.Duration
	writeDelay   time.Duration
	onRead       func() error
	onWrite      func(data []byte) error
}

func newMockConnection() *mockClientConnection {
	return &mockClientConnection{
		readData:    make([][]byte, 0),
		writtenData: make([][]byte, 0),
		readCh:      make(chan []byte, 10),
	}
}

func (m *mockClientConnection) Read(ctx context.Context) ([]byte, error) {
	if m.onRead != nil {
		if err := m.onRead(); err != nil {
			return nil, err
		}
	}

	if m.readDelay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.readDelay):
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.readErr != nil {
		return nil, m.readErr
	}

	if m.closed {
		return nil, errors.New("connection closed")
	}

	// If using blocking channel mode
	if m.readBlocking {
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			m.mu.Lock()
			return nil, ctx.Err()
		case data, ok := <-m.readCh:
			m.mu.Lock()
			if !ok {
				return nil, errors.New("connection closed")
			}
			return data, nil
		}
	}

	// Non-blocking mode using readData slice
	if m.readIndex >= len(m.readData) {
		// Block until context is done or more data is provided
		m.mu.Unlock()
		<-ctx.Done()
		m.mu.Lock()
		return nil, ctx.Err()
	}

	data := m.readData[m.readIndex]
	m.readIndex++
	return data, nil
}

func (m *mockClientConnection) Write(ctx context.Context, data []byte) error {
	if m.onWrite != nil {
		if err := m.onWrite(data); err != nil {
			return err
		}
	}

	if m.writeDelay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.writeDelay):
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.writeErr != nil {
		return m.writeErr
	}

	if m.closed {
		return errors.New("connection closed")
	}

	// Copy data to avoid aliasing issues
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	m.writtenData = append(m.writtenData, dataCopy)
	return nil
}

func (m *mockClientConnection) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closeCalled++
	if m.closeErr != nil {
		return m.closeErr
	}

	if !m.closed {
		m.closed = true
		close(m.readCh)
	}
	return nil
}

func (m *mockClientConnection) addReadData(data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readData = append(m.readData, data)
}

func (m *mockClientConnection) setReadBlocking(blocking bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readBlocking = blocking
}

func (m *mockClientConnection) sendReadData(data []byte) {
	m.readCh <- data
}

func (m *mockClientConnection) getWrittenData() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writtenData
}

func (m *mockClientConnection) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// newClientTestLogger creates a logger for client tests.
func newClientTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "console",
		Output: "stdout",
	})
	require.NoError(t, err)
	return log
}

func TestNewWebSocketClient(t *testing.T) {
	t.Parallel()

	t.Run("creates client with default options", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		client := NewWebSocketClient(hub, conn, log)

		require.NotNil(t, client)
		assert.NotEmpty(t, client.ID)
		assert.Equal(t, DefaultClientCodec, client.GetCodec())
		assert.Empty(t, client.GetSession())
		assert.False(t, client.IsClosed())
		assert.Equal(t, 0, len(client.GetChannels()))
	})

	t.Run("creates client with custom ID", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		client := NewWebSocketClient(hub, conn, log, WithClientID("custom-id"))

		require.NotNil(t, client)
		assert.Equal(t, "custom-id", client.ID)
	})

	t.Run("creates client with custom codec", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		client := NewWebSocketClient(hub, conn, log, WithClientCodec("msgpack"))

		require.NotNil(t, client)
		assert.Equal(t, "msgpack", client.GetCodec())
	})

	t.Run("creates client with session", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		client := NewWebSocketClient(hub, conn, log, WithClientSession("session-123"))

		require.NotNil(t, client)
		assert.Equal(t, "session-123", client.GetSession())
	})

	t.Run("creates client with custom send buffer", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		client := NewWebSocketClient(hub, conn, log, WithClientSendBuffer(512))

		require.NotNil(t, client)
		assert.Equal(t, 512, cap(client.send))
	})

	t.Run("ignores invalid send buffer size", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		client := NewWebSocketClient(hub, conn, log, WithClientSendBuffer(-1))

		require.NotNil(t, client)
		// Should use default buffer size
		assert.Equal(t, DefaultClientSendBufferSize, cap(client.send))
	})
}

func TestWebSocketClient_StartStop(t *testing.T) {
	t.Parallel()

	t.Run("starts and stops cleanly", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()
		conn.setReadBlocking(true)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewWebSocketClient(hub, conn, log)
		client.Start()

		// Wait for client to be registered with hub
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		// Stop the client
		client.Stop()

		// Verify client is stopped
		assert.True(t, client.IsClosed())
		assert.True(t, conn.isClosed())

		// Wait for hub to reflect unregistration
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 0
		}, testTimeout, testPollInterval)
	})

	t.Run("stop is idempotent", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()
		conn.setReadBlocking(true)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewWebSocketClient(hub, conn, log)
		client.Start()

		// Stop multiple times - should not panic
		client.Stop()
		client.Stop()
		client.Stop()

		assert.True(t, client.IsClosed())
	})

	t.Run("handles connection close error gracefully", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()
		conn.setReadBlocking(true)
		conn.closeErr = errors.New("close error")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewWebSocketClient(hub, conn, log)
		client.Start()

		// Stop should not panic even with close error
		client.Stop()

		assert.True(t, client.IsClosed())
	})
}

func TestWebSocketClient_Subscribe(t *testing.T) {
	t.Parallel()

	t.Run("subscribes to channel", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()
		conn.setReadBlocking(true)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewWebSocketClient(hub, conn, log)
		client.Start()

		// Wait for client registration
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		err := client.Subscribe("test-channel")
		require.NoError(t, err)

		assert.True(t, client.IsSubscribed("test-channel"))
		channels := client.GetChannels()
		assert.Contains(t, channels, "test-channel")

		client.Stop()
	})

	t.Run("returns error for empty channel", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		client := NewWebSocketClient(hub, conn, log)

		err := client.Subscribe("")
		require.Error(t, err)
	})
}

func TestWebSocketClient_Unsubscribe(t *testing.T) {
	t.Parallel()

	t.Run("unsubscribes from channel", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()
		conn.setReadBlocking(true)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewWebSocketClient(hub, conn, log)
		client.Start()

		// Wait for client registration
		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		err := client.Subscribe("test-channel")
		require.NoError(t, err)

		err = client.Unsubscribe("test-channel")
		require.NoError(t, err)

		assert.False(t, client.IsSubscribed("test-channel"))

		client.Stop()
	})

	t.Run("returns error for empty channel", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		client := NewWebSocketClient(hub, conn, log)

		err := client.Unsubscribe("")
		require.Error(t, err)
	})
}

func TestWebSocketClient_Send(t *testing.T) {
	t.Parallel()

	t.Run("sends message successfully", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		client := NewWebSocketClient(hub, conn, log)

		msg := &ClientMessage{
			Type:    ClientMessageTypeNotify,
			Channel: "test",
			Payload: []byte(`{"test":"data"}`),
		}

		err := client.Send(msg)
		require.NoError(t, err)

		// Message should be in the send channel
		select {
		case received := <-client.send:
			assert.Equal(t, msg, received)
		default:
			t.Fatal("expected message in send channel")
		}
	})

	t.Run("returns error when closed", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()
		conn.setReadBlocking(true)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewWebSocketClient(hub, conn, log)
		client.Start()
		client.Stop()

		msg := &ClientMessage{
			Type:    ClientMessageTypeNotify,
			Channel: "test",
		}

		err := client.Send(msg)
		require.Error(t, err)
	})

	t.Run("drops message when buffer is full", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		// Create client with very small buffer
		client := NewWebSocketClient(hub, conn, log, WithClientSendBuffer(1))

		msg := &ClientMessage{
			Type:    ClientMessageTypeNotify,
			Channel: "test",
		}

		// Fill the buffer
		err := client.Send(msg)
		require.NoError(t, err)

		// This should trigger backpressure
		err = client.Send(msg)
		require.Error(t, err)

		stats := client.GetStats()
		assert.Equal(t, uint64(1), stats.MessagesDropped)
	})
}

func TestWebSocketClient_GetStats(t *testing.T) {
	t.Parallel()

	t.Run("returns correct stats", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		client := NewWebSocketClient(hub, conn, log, WithClientID("stats-client"))

		// Subscribe to a channel
		client.mu.Lock()
		client.channels["channel-1"] = true
		client.channels["channel-2"] = true
		client.mu.Unlock()

		stats := client.GetStats()

		assert.Equal(t, "stats-client", stats.ID)
		assert.Equal(t, uint64(0), stats.MessagesSent)
		assert.Equal(t, uint64(0), stats.MessagesDropped)
		assert.Equal(t, 2, stats.ChannelCount)
		assert.False(t, stats.ConnectedAt.IsZero())
	})
}

func TestWebSocketClient_UpdateActivity(t *testing.T) {
	t.Parallel()

	t.Run("updates last activity timestamp", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		client := NewWebSocketClient(hub, conn, log)

		beforeUpdate := time.Now().Unix() - 1
		client.UpdateActivity()
		afterUpdate := time.Now().Unix() + 1

		stats := client.GetStats()
		lastActivity := stats.LastActivity.Unix()

		assert.GreaterOrEqual(t, lastActivity, beforeUpdate)
		assert.LessOrEqual(t, lastActivity, afterUpdate)
	})
}

func TestWebSocketClient_HandleMessage(t *testing.T) {
	t.Parallel()

	t.Run("handles subscribe message", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()
		conn.setReadBlocking(true)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewWebSocketClient(hub, conn, log)
		client.Start()

		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		msg := &ClientMessage{
			Type:    ClientMessageTypeSubscribe,
			Channel: "test-channel",
		}
		data, _ := json.Marshal(msg)

		err := client.handleMessage(data)
		require.NoError(t, err)

		assert.True(t, client.IsSubscribed("test-channel"))

		client.Stop()
	})

	t.Run("handles unsubscribe message", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()
		conn.setReadBlocking(true)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewWebSocketClient(hub, conn, log)
		client.Start()

		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		// First subscribe
		err := client.Subscribe("test-channel")
		require.NoError(t, err)

		// Then unsubscribe via message
		msg := &ClientMessage{
			Type:    ClientMessageTypeUnsubscribe,
			Channel: "test-channel",
		}
		data, _ := json.Marshal(msg)

		err = client.handleMessage(data)
		require.NoError(t, err)

		assert.False(t, client.IsSubscribed("test-channel"))

		client.Stop()
	})

	t.Run("handles ping message", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		client := NewWebSocketClient(hub, conn, log)

		msg := &ClientMessage{
			Type: ClientMessageTypePing,
		}
		data, _ := json.Marshal(msg)

		err := client.handleMessage(data)
		require.NoError(t, err)

		// Pong should be queued
		select {
		case pong := <-client.send:
			assert.Equal(t, ClientMessageTypePong, pong.Type)
		default:
			t.Fatal("expected pong in send channel")
		}
	})

	t.Run("handles publish message", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()
		conn.setReadBlocking(true)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewWebSocketClient(hub, conn, log)
		client.Start()

		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		// Create a channel first
		_, err := hub.CreateChannel("test-channel")
		require.NoError(t, err)

		msg := &ClientMessage{
			Type:    ClientMessageTypePublish,
			Channel: "test-channel",
			Payload: []byte(`{"test":"data"}`),
		}
		data, _ := json.Marshal(msg)

		err = client.handleMessage(data)
		require.NoError(t, err)

		client.Stop()
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		client := NewWebSocketClient(hub, conn, log)

		err := client.handleMessage([]byte("not json"))
		require.Error(t, err)
	})

	t.Run("returns error for unknown message type", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		client := NewWebSocketClient(hub, conn, log)

		msg := &ClientMessage{
			Type: "unknown-type",
		}
		data, _ := json.Marshal(msg)

		err := client.handleMessage(data)
		require.Error(t, err)
	})

	t.Run("returns error for subscribe without channel", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		client := NewWebSocketClient(hub, conn, log)

		msg := &ClientMessage{
			Type:    ClientMessageTypeSubscribe,
			Channel: "",
		}
		data, _ := json.Marshal(msg)

		err := client.handleMessage(data)
		require.Error(t, err)
	})

	t.Run("returns error for unsubscribe without channel", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		client := NewWebSocketClient(hub, conn, log)

		msg := &ClientMessage{
			Type:    ClientMessageTypeUnsubscribe,
			Channel: "",
		}
		data, _ := json.Marshal(msg)

		err := client.handleMessage(data)
		require.Error(t, err)
	})

	t.Run("returns error for publish without hub", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		conn := newMockConnection()

		// Create client without hub
		client := NewWebSocketClient(nil, conn, log)

		msg := &ClientMessage{
			Type:    ClientMessageTypePublish,
			Channel: "test-channel",
			Payload: []byte(`{"test":"data"}`),
		}
		data, _ := json.Marshal(msg)

		err := client.handleMessage(data)
		require.Error(t, err)
	})
}

func TestWebSocketClient_WritePump(t *testing.T) {
	t.Parallel()

	t.Run("writes messages to connection", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()
		conn.setReadBlocking(true)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewWebSocketClient(hub, conn, log)
		client.Start()

		// Send a message via the send channel
		msg := &ClientMessage{
			Type:    ClientMessageTypeNotify,
			Channel: "test",
			Payload: []byte(`{"hello":"world"}`),
		}

		err := client.Send(msg)
		require.NoError(t, err)

		// Wait for the message to be written
		require.Eventually(t, func() bool {
			written := conn.getWrittenData()
			return len(written) > 0
		}, testTimeout, testPollInterval)

		client.Stop()
	})

	t.Run("stops on write error", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()
		conn.setReadBlocking(true)
		conn.writeErr = errors.New("write error")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewWebSocketClient(hub, conn, log)
		client.Start()

		// Send a message that will fail
		msg := &ClientMessage{
			Type:    ClientMessageTypeNotify,
			Channel: "test",
		}
		_ = client.Send(msg)

		// Client should stop due to write error
		require.Eventually(t, func() bool {
			return client.IsClosed()
		}, testTimeout, testPollInterval)
	})
}

func TestWebSocketClient_ReadPump(t *testing.T) {
	t.Parallel()

	t.Run("processes incoming messages", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()
		conn.setReadBlocking(true)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewWebSocketClient(hub, conn, log)
		client.Start()

		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		// Send a subscribe message through the connection
		msg := &ClientMessage{
			Type:    ClientMessageTypeSubscribe,
			Channel: "test-channel",
		}
		data, _ := json.Marshal(msg)
		conn.sendReadData(data)

		// Wait for subscription to be processed
		require.Eventually(t, func() bool {
			return client.IsSubscribed("test-channel")
		}, testTimeout, testPollInterval)

		client.Stop()
	})

	t.Run("stops on read error", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()
		conn.readErr = errors.New("read error")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewWebSocketClient(hub, conn, log)
		client.Start()

		// Client should stop due to read error
		require.Eventually(t, func() bool {
			return client.IsClosed()
		}, testTimeout, testPollInterval)
	})
}

func TestWebSocketClient_HubBridge(t *testing.T) {
	t.Parallel()

	t.Run("handles nil hubClient gracefully", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		conn := newMockConnection()

		// Create client without hub - hubClient will not work the same way
		client := NewWebSocketClient(nil, conn, log)
		client.hubClient = nil

		// hubBridge checks for nil and returns at once rather than panicking
		client.hubBridge()
	})

	t.Run("stops when explicitly stopped", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()
		conn.setReadBlocking(true)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewWebSocketClient(hub, conn, log)
		client.Start()

		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		// Stop client explicitly and verify it's closed
		client.Stop()

		require.Eventually(t, func() bool {
			return client.IsClosed()
		}, testTimeout, testPollInterval)
	})

	t.Run("stops when hubClient Done channel closes", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()
		conn.setReadBlocking(true)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewWebSocketClient(hub, conn, log)
		client.Start()

		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		// Close the hubClient
		client.hubClient.Close()

		// The hubBridge should exit gracefully
		client.Stop()
	})

	t.Run("converts hub messages to client messages", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()
		conn.setReadBlocking(true)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		require.Eventually(t, func() bool {
			return hub.IsRunning()
		}, testTimeout, testPollInterval)

		client := NewWebSocketClient(hub, conn, log)
		client.Start()

		require.Eventually(t, func() bool {
			return hub.ClientCount() == 1
		}, testTimeout, testPollInterval)

		// Directly send a message to the hubClient's Send channel
		hubMsg := &Message{
			ID:        "hub-msg-1",
			Type:      MessageTypeNotification,
			Channel:   "test-channel",
			Payload:   []byte(`{"key":"value"}`),
			Timestamp: time.Now(),
		}

		// Send directly to hubClient to test the bridge conversion
		select {
		case client.hubClient.Send <- hubMsg:
		default:
			t.Fatal("could not send to hubClient")
		}

		// Wait for the message to be written to the connection
		require.Eventually(t, func() bool {
			written := conn.getWrittenData()
			if len(written) == 0 {
				return false
			}
			// Verify the written data contains our message
			var msg ClientMessage
			if err := json.Unmarshal(written[0], &msg); err != nil {
				return false
			}
			return msg.Type == ClientMessageTypeNotify &&
				msg.Channel == "test-channel" &&
				msg.ID == "hub-msg-1"
		}, testTimeout, testPollInterval)

		client.Stop()
	})
}

func TestClientErrorTypes(t *testing.T) {
	t.Parallel()

	t.Run("UnknownMessageTypeError", func(t *testing.T) {
		t.Parallel()
		err := &UnknownMessageTypeError{Type: "bad-type"}
		assert.Contains(t, err.Error(), "bad-type")
		assert.Contains(t, err.Error(), "unknown message type")
	})

	t.Run("MissingChannelError", func(t *testing.T) {
		t.Parallel()
		err := &MissingChannelError{}
		assert.Contains(t, err.Error(), "channel is required")
	})

	t.Run("NoHubError", func(t *testing.T) {
		t.Parallel()
		err := &NoHubError{}
		assert.Contains(t, err.Error(), "notification hub is not available")
	})

	t.Run("ClientClosedError", func(t *testing.T) {
		t.Parallel()
		err := &ClientClosedError{ClientID: "client-123"}
		assert.Contains(t, err.Error(), "client-123")
		assert.Contains(t, err.Error(), "closed")
	})

	t.Run("BackpressureError", func(t *testing.T) {
		t.Parallel()
		err := &BackpressureError{ClientID: "client-123"}
		assert.Contains(t, err.Error(), "client-123")
		assert.Contains(t, err.Error(), "buffer full")
	})
}

func TestClientMessageTypeConstants(t *testing.T) {
	t.Parallel()

	t.Run("message types have expected values", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, ClientMessageType("subscribe"), ClientMessageTypeSubscribe)
		assert.Equal(t, ClientMessageType("unsubscribe"), ClientMessageTypeUnsubscribe)
		assert.Equal(t, ClientMessageType("publish"), ClientMessageTypePublish)
		assert.Equal(t, ClientMessageType("notify"), ClientMessageTypeNotify)
		assert.Equal(t, ClientMessageType("ping"), ClientMessageTypePing)
		assert.Equal(t, ClientMessageType("pong"), ClientMessageTypePong)
		assert.Equal(t, ClientMessageType("ack"), ClientMessageTypeAck)
		assert.Equal(t, ClientMessageType("error"), ClientMessageTypeError)
	})
}

func TestWebSocketClient_EncodeDecode(t *testing.T) {
	t.Parallel()

	t.Run("encodes message to JSON", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		client := NewWebSocketClient(hub, conn, log)

		msg := &ClientMessage{
			ID:      "msg-1",
			Type:    ClientMessageTypeNotify,
			Channel: "test-channel",
			Payload: []byte(`{"key":"value"}`),
		}

		data, err := client.encodeMessage(msg)
		require.NoError(t, err)
		assert.NotEmpty(t, data)

		// Verify it's valid JSON
		var decoded ClientMessage
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)
		assert.Equal(t, "msg-1", decoded.ID)
	})

	t.Run("decodes JSON to message", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		client := NewWebSocketClient(hub, conn, log)

		data := []byte(`{"id":"msg-2","type":"notify","channel":"test"}`)

		msg, err := client.decodeMessage(data)
		require.NoError(t, err)
		assert.Equal(t, "msg-2", msg.ID)
		assert.Equal(t, ClientMessageTypeNotify, msg.Type)
		assert.Equal(t, "test", msg.Channel)
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		t.Parallel()
		log := newClientTestLogger(t)
		hub := NewNotificationHub(log)
		conn := newMockConnection()

		client := NewWebSocketClient(hub, conn, log)

		_, err := client.decodeMessage([]byte("not json"))
		require.Error(t, err)
	})
}
