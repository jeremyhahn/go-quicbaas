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
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// Default configuration values for WebSocketClient.
const (
	DefaultClientSendBufferSize = 256
	DefaultClientCodec          = "json"
)

// ClientConnection abstracts the underlying connection for testing and
// protocol flexibility. Implementations may wrap WebSocket, QUIC streams,
// or other transport mechanisms.
type ClientConnection interface {
	// Read reads the next message from the connection.
	// Returns the message data or an error if the read fails.
	Read(ctx context.Context) ([]byte, error)

	// Write sends data to the connection.
	// Returns an error if the write fails.
	Write(ctx context.Context, data []byte) error

	// Close closes the underlying connection.
	Close() error
}

// ClientStats contains statistics for a connected client.
type ClientStats struct {
	ID              string
	MessagesSent    uint64
	MessagesDropped uint64
	ChannelCount    int
	LastActivity    time.Time
	ConnectedAt     time.Time
}

// WebSocketClient represents a connected subscriber with read/write pumps.
// It manages the lifecycle of a single client connection, handling
// message routing, channel subscriptions, and graceful shutdown.
type WebSocketClient struct {
	// ID is the unique identifier for this client
	ID string

	// hub is the reference to the notification hub
	hub *NotificationHub

	// conn is the abstracted connection interface
	conn ClientConnection

	// send is the buffered outbound message channel
	send chan *ClientMessage

	// channels contains the set of subscribed channel names
	channels map[string]bool

	// codec is the preferred serialization format
	codec string

	// session is an optional session identifier
	session string

	// ctx is the client's context for cancellation
	ctx context.Context

	// cancel cancels the client context
	cancel context.CancelFunc

	// logger is the structured logger
	logger *logger.Logger

	// mu protects the channels map
	mu sync.RWMutex

	// messagesSent tracks the number of messages sent
	messagesSent atomic.Uint64

	// messagesDropped tracks the number of messages dropped due to backpressure
	messagesDropped atomic.Uint64

	// lastActivity stores the Unix timestamp of last activity
	lastActivity atomic.Int64

	// connectedAt is the time the client connected
	connectedAt time.Time

	// closed indicates whether the client has been stopped
	closed atomic.Bool

	// hubClient is the underlying hub client for integration
	hubClient *Client
}

// ClientMessage represents a message exchanged over the WebSocket connection.
// This uses string-based message types for wire protocol compatibility.
type ClientMessage struct {
	// ID is a unique identifier for the message
	ID string `json:"id,omitempty"`

	// Type indicates the message type
	Type ClientMessageType `json:"type"`

	// Channel is the target channel for the message
	Channel string `json:"channel,omitempty"`

	// Payload contains the message data
	Payload json.RawMessage `json:"payload,omitempty"`

	// Timestamp is when the message was created
	Timestamp time.Time `json:"timestamp,omitempty"`

	// Source is the originator of the message
	Source string `json:"source,omitempty"`

	// Metadata contains optional key-value metadata
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ClientMessageType defines the type of client message.
type ClientMessageType string

// Client message type constants for wire protocol.
const (
	ClientMessageTypeSubscribe   ClientMessageType = "subscribe"
	ClientMessageTypeUnsubscribe ClientMessageType = "unsubscribe"
	ClientMessageTypePublish     ClientMessageType = "publish"
	ClientMessageTypeNotify      ClientMessageType = "notify"
	ClientMessageTypePing        ClientMessageType = "ping"
	ClientMessageTypePong        ClientMessageType = "pong"
	ClientMessageTypeAck         ClientMessageType = "ack"
	ClientMessageTypeError       ClientMessageType = "error"
)

// WebSocketClientOption configures a WebSocketClient.
type WebSocketClientOption func(*WebSocketClient)

// WithClientCodec sets the preferred codec for message serialization.
func WithClientCodec(codec string) WebSocketClientOption {
	return func(c *WebSocketClient) {
		c.codec = codec
	}
}

// WithClientSession sets an optional session identifier for the client.
func WithClientSession(session string) WebSocketClientOption {
	return func(c *WebSocketClient) {
		c.session = session
	}
}

// WithClientSendBuffer sets the size of the outbound message buffer.
func WithClientSendBuffer(size int) WebSocketClientOption {
	return func(c *WebSocketClient) {
		if size > 0 {
			c.send = make(chan *ClientMessage, size)
		}
	}
}

// WithClientID sets a specific client ID instead of generating one.
func WithClientID(id string) WebSocketClientOption {
	return func(c *WebSocketClient) {
		c.ID = id
	}
}

// NewWebSocketClient creates a new WebSocketClient with the given connection and options.
// The client is not started automatically; call Start() to begin processing.
func NewWebSocketClient(hub *NotificationHub, conn ClientConnection, log *logger.Logger, opts ...WebSocketClientOption) *WebSocketClient {
	ctx, cancel := context.WithCancel(context.Background())
	clientID := uuid.New().String()

	c := &WebSocketClient{
		ID:          clientID,
		hub:         hub,
		conn:        conn,
		send:        make(chan *ClientMessage, DefaultClientSendBufferSize),
		channels:    make(map[string]bool),
		codec:       DefaultClientCodec,
		ctx:         ctx,
		cancel:      cancel,
		logger:      log.WithField("component", "push.client"),
		connectedAt: time.Now(),
	}

	// Apply options
	for _, opt := range opts {
		opt(c)
	}

	// Update logger with client ID
	c.logger = c.logger.WithField("client_id", c.ID)
	if c.session != "" {
		c.logger = c.logger.WithField("session", c.session)
	}

	// Create the underlying hub client for integration with NotificationHub
	c.hubClient = NewClient(c.ID, DefaultClientSendBufferSize)

	// Initialize last activity
	c.UpdateActivity()

	return c
}

// Start begins the read and write pumps in separate goroutines.
// This method returns immediately; the pumps run until the client
// is stopped or the connection is closed.
func (c *WebSocketClient) Start() {
	// Register with hub if available
	if c.hub != nil {
		c.hub.RegisterClient(c.hubClient)
	}

	go c.writePump()
	go c.readPump()
	go c.hubBridge()

	c.logger.Debug("client started")
}

// Stop gracefully shuts down the client, closing the connection
// and signaling the pumps to terminate.
func (c *WebSocketClient) Stop() {
	// Use atomic to ensure we only stop once
	if !c.closed.CompareAndSwap(false, true) {
		return
	}

	c.logger.Debug("stopping client")

	// Cancel context to signal pumps
	c.cancel()

	// Close the connection
	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			c.logger.WithError(err).Debug("error closing connection")
		}
	}

	// Unregister from hub if available
	if c.hub != nil && c.hubClient != nil {
		c.hub.UnregisterClient(c.hubClient)
	}

	// Close hub client
	if c.hubClient != nil {
		c.hubClient.Close()
	}

	c.logger.Debug("client stopped")
}

// readPump continuously reads messages from the connection and processes them.
// It runs until the context is canceled or the connection is closed.
func (c *WebSocketClient) readPump() {
	defer func() {
		c.Stop()
	}()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			data, err := c.conn.Read(c.ctx)
			if err != nil {
				// Check if this is a graceful shutdown
				if c.ctx.Err() != nil {
					return
				}
				c.logger.WithError(err).Debug("read error")
				return
			}

			c.UpdateActivity()

			if err := c.handleMessage(data); err != nil {
				c.logger.WithError(err).Debug("error handling message")
			}
		}
	}
}

// writePump continuously writes messages from the send channel to the connection.
// It runs until the context is canceled or the send channel is closed.
func (c *WebSocketClient) writePump() {
	defer func() {
		c.Stop()
	}()

	for {
		select {
		case <-c.ctx.Done():
			return
		case msg, ok := <-c.send:
			if !ok {
				// Channel closed
				return
			}

			data, err := c.encodeMessage(msg)
			if err != nil {
				c.logger.WithError(err).Debug("error encoding message")
				continue
			}

			if err := c.conn.Write(c.ctx, data); err != nil {
				if c.ctx.Err() != nil {
					return
				}
				c.logger.WithError(err).Debug("write error")
				return
			}

			c.messagesSent.Add(1)
			c.UpdateActivity()
		}
	}
}

// hubBridge bridges messages from the hub client to the WebSocket connection.
// It converts hub messages to client messages and sends them to the send channel.
func (c *WebSocketClient) hubBridge() {
	if c.hubClient == nil {
		return
	}

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.hubClient.Done:
			return
		case hubMsg, ok := <-c.hubClient.Send:
			if !ok {
				return
			}

			// Convert hub message to client message
			clientMsg := &ClientMessage{
				ID:        hubMsg.ID,
				Type:      ClientMessageTypeNotify,
				Channel:   hubMsg.Channel,
				Payload:   hubMsg.Payload,
				Timestamp: hubMsg.Timestamp,
				Metadata:  hubMsg.Metadata,
			}

			// Send to the WebSocket write pump
			if err := c.Send(clientMsg); err != nil {
				c.logger.WithError(err).Debug("error bridging hub message")
			}
		}
	}
}

// handleMessage processes an incoming message from the client.
// It decodes the message and dispatches it based on the message type.
func (c *WebSocketClient) handleMessage(data []byte) error {
	msg, err := c.decodeMessage(data)
	if err != nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "client.handleMessage",
			Err:  err,
		}
	}

	// Dispatch based on message type using map-based dispatch
	handler, ok := wsClientMessageHandlers[msg.Type]
	if !ok {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "client.handleMessage",
			Err:  &UnknownMessageTypeError{Type: string(msg.Type)},
		}
	}

	return handler(c, msg)
}

// wsMessageHandler is a function type for handling client messages.
type wsMessageHandler func(c *WebSocketClient, msg *ClientMessage) error

// wsClientMessageHandlers maps message types to their handlers.
var wsClientMessageHandlers = map[ClientMessageType]wsMessageHandler{
	ClientMessageTypeSubscribe:   handleWSSubscribeMessage,
	ClientMessageTypeUnsubscribe: handleWSUnsubscribeMessage,
	ClientMessageTypePublish:     handleWSPublishMessage,
	ClientMessageTypePing:        handleWSPingMessage,
}

// handleWSSubscribeMessage handles a subscribe request from the client.
func handleWSSubscribeMessage(c *WebSocketClient, msg *ClientMessage) error {
	if msg.Channel == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "client.handleSubscribe",
			Err:  &MissingChannelError{},
		}
	}
	return c.Subscribe(msg.Channel)
}

// handleWSUnsubscribeMessage handles an unsubscribe request from the client.
func handleWSUnsubscribeMessage(c *WebSocketClient, msg *ClientMessage) error {
	if msg.Channel == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "client.handleUnsubscribe",
			Err:  &MissingChannelError{},
		}
	}
	return c.Unsubscribe(msg.Channel)
}

// handleWSPublishMessage handles a publish request from the client.
func handleWSPublishMessage(c *WebSocketClient, msg *ClientMessage) error {
	if c.hub == nil {
		return &errors.QBError{
			Code: errors.ErrUnavailable,
			Op:   "client.handlePublish",
			Err:  &NoHubError{},
		}
	}

	// Convert client message to hub message
	hubMsg := &Message{
		ID:        msg.ID,
		Type:      MessageTypeData,
		Channel:   msg.Channel,
		Payload:   msg.Payload,
		Timestamp: msg.Timestamp,
		Metadata:  msg.Metadata,
	}

	return c.hub.Broadcast(hubMsg)
}

// handleWSPingMessage handles a ping message from the client.
func handleWSPingMessage(c *WebSocketClient, _ *ClientMessage) error {
	pong := &ClientMessage{
		Type:      ClientMessageTypePong,
		Timestamp: time.Now(),
	}
	return c.Send(pong)
}

// Subscribe adds the client to a channel.
func (c *WebSocketClient) Subscribe(channel string) error {
	if channel == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "client.Subscribe",
			Err:  &MissingChannelError{},
		}
	}

	c.mu.Lock()
	c.channels[channel] = true
	c.mu.Unlock()

	// Register with hub if available.
	//
	// RegisterAndSubscribe rather than SubscribeToChannel for the same reason
	// PushService.Subscribe uses it (#339): Start queues this client's
	// registration asynchronously, so a subscribe issued before the hub has
	// drained it is answered "client not registered". The window here is wide
	// — Start and a user's subscribe are usually far apart — which is why this
	// never bit, not why it was safe.
	if c.hub != nil && c.hubClient != nil {
		if err := c.hub.RegisterAndSubscribe(c.hubClient, channel); err != nil {
			c.logger.WithError(err).WithField("channel", channel).Debug("error subscribing to channel")
			// Don't return error - local subscription succeeded
		}
	}

	c.logger.WithField("channel", channel).Debug("subscribed to channel")
	return nil
}

// Unsubscribe removes the client from a channel.
func (c *WebSocketClient) Unsubscribe(channel string) error {
	if channel == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "client.Unsubscribe",
			Err:  &MissingChannelError{},
		}
	}

	c.mu.Lock()
	delete(c.channels, channel)
	c.mu.Unlock()

	// Unregister from hub if available
	if c.hub != nil && c.hubClient != nil {
		if err := c.hub.UnsubscribeFromChannel(c.hubClient, channel); err != nil {
			c.logger.WithError(err).WithField("channel", channel).Debug("error unsubscribing from channel")
			// Don't return error - local unsubscription succeeded
		}
	}

	c.logger.WithField("channel", channel).Debug("unsubscribed from channel")
	return nil
}

// Send attempts to send a message to the client without blocking.
// If the send buffer is full, the message is dropped and the
// messagesDropped counter is incremented.
func (c *WebSocketClient) Send(msg *ClientMessage) error {
	if c.closed.Load() {
		return &errors.QBError{
			Code: errors.ErrUnavailable,
			Op:   "client.Send",
			Err:  &ClientClosedError{ClientID: c.ID},
		}
	}

	select {
	case c.send <- msg:
		return nil
	default:
		// Buffer full - apply backpressure by dropping
		c.messagesDropped.Add(1)
		c.logger.Debug("message dropped due to backpressure")
		return &errors.QBError{
			Code: errors.ErrResourceExhausted,
			Op:   "client.Send",
			Err:  &BackpressureError{ClientID: c.ID},
		}
	}
}

// GetStats returns the current statistics for this client.
func (c *WebSocketClient) GetStats() ClientStats {
	c.mu.RLock()
	channelCount := len(c.channels)
	c.mu.RUnlock()

	lastActivityUnix := c.lastActivity.Load()
	var lastActivity time.Time
	if lastActivityUnix > 0 {
		lastActivity = time.Unix(lastActivityUnix, 0)
	}

	return ClientStats{
		ID:              c.ID,
		MessagesSent:    c.messagesSent.Load(),
		MessagesDropped: c.messagesDropped.Load(),
		ChannelCount:    channelCount,
		LastActivity:    lastActivity,
		ConnectedAt:     c.connectedAt,
	}
}

// UpdateActivity updates the last activity timestamp to the current time.
func (c *WebSocketClient) UpdateActivity() {
	c.lastActivity.Store(time.Now().Unix())
}

// GetChannels returns a copy of the client's subscribed channels.
func (c *WebSocketClient) GetChannels() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	channels := make([]string, 0, len(c.channels))
	for ch := range c.channels {
		channels = append(channels, ch)
	}
	return channels
}

// IsSubscribed checks if the client is subscribed to a specific channel.
func (c *WebSocketClient) IsSubscribed(channel string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.channels[channel]
}

// GetCodec returns the client's preferred codec.
func (c *WebSocketClient) GetCodec() string {
	return c.codec
}

// GetSession returns the client's session identifier.
func (c *WebSocketClient) GetSession() string {
	return c.session
}

// IsClosed returns true if the client has been stopped.
func (c *WebSocketClient) IsClosed() bool {
	return c.closed.Load()
}

// encodeMessage serializes a message using the client's preferred codec.
func (c *WebSocketClient) encodeMessage(msg *ClientMessage) ([]byte, error) {
	return json.Marshal(msg)
}

// decodeMessage deserializes a message using the client's preferred codec.
func (c *WebSocketClient) decodeMessage(data []byte) (*ClientMessage, error) {
	msg := &ClientMessage{}
	if err := json.Unmarshal(data, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// Custom error types for the push client.

// UnknownMessageTypeError indicates an unknown message type was received.
type UnknownMessageTypeError struct {
	Type string
}

func (e *UnknownMessageTypeError) Error() string {
	return "unknown message type: " + e.Type
}

// MissingChannelError indicates a required channel was not specified.
type MissingChannelError struct{}

func (e *MissingChannelError) Error() string {
	return "channel is required"
}

// NoHubError indicates the hub is not available.
type NoHubError struct{}

func (e *NoHubError) Error() string {
	return "notification hub is not available"
}

// ClientClosedError indicates the client has been closed.
type ClientClosedError struct {
	ClientID string
}

func (e *ClientClosedError) Error() string {
	return "client is closed: " + e.ClientID
}

// BackpressureError indicates a message was dropped due to backpressure.
type BackpressureError struct {
	ClientID string
}

func (e *BackpressureError) Error() string {
	return "send buffer full, message dropped for client: " + e.ClientID
}
