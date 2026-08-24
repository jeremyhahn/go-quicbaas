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
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// DefaultBufferSize is the default channel buffer size for the hub.
const DefaultBufferSize = 256

// DefaultSlowConsumerTimeout is the timeout for detecting slow consumers.
const DefaultSlowConsumerTimeout = 5 * time.Second

// Client represents a connected subscriber client for the hub.
type Client struct {
	// ID is the unique client identifier.
	ID string
	// Send is the channel for sending messages to this client.
	Send chan *Message
	// Done signals when the client has disconnected.
	Done chan struct{}
	// Metadata contains optional client metadata.
	Metadata map[string]string
	// subscriptions tracks which channels this client is subscribed to.
	subscriptions map[string]bool
	// connected indicates if the client is currently connected.
	connected atomic.Bool
}

// NewClient creates a new client with the specified ID and buffer size.
func NewClient(id string, bufferSize int) *Client {
	if bufferSize <= 0 {
		bufferSize = DefaultBufferSize
	}
	c := &Client{
		ID:            id,
		Send:          make(chan *Message, bufferSize),
		Done:          make(chan struct{}),
		Metadata:      make(map[string]string),
		subscriptions: make(map[string]bool),
	}
	c.connected.Store(true)
	return c
}

// IsConnected returns true if the client is currently connected.
func (c *Client) IsConnected() bool {
	return c.connected.Load()
}

// Close marks the client as disconnected and closes its channels.
func (c *Client) Close() {
	if c.connected.CompareAndSwap(true, false) {
		close(c.Done)
	}
}

// channelOptions holds configuration options for a channel.
type channelOptions struct {
	// BufferSize is the message buffer size for the channel.
	BufferSize int
	// MaxSubscribers is the maximum number of subscribers allowed.
	MaxSubscribers int
	// Persistent indicates if messages should be persisted.
	Persistent bool
	// TTL is the time-to-live for messages in this channel.
	TTL time.Duration
}

// defaultChannelOptions returns the default channel options.
func defaultChannelOptions() *channelOptions {
	return &channelOptions{
		BufferSize:     DefaultBufferSize,
		MaxSubscribers: 0, // unlimited
		Persistent:     false,
		TTL:            0, // no expiration
	}
}

// ChannelOption is a functional option for configuring a channel.
type ChannelOption func(*channelOptions)

// WithBufferSize sets the buffer size for a channel.
func WithBufferSize(size int) ChannelOption {
	return func(o *channelOptions) {
		if size > 0 {
			o.BufferSize = size
		}
	}
}

// WithMaxSubscribers sets the maximum number of subscribers for a channel.
func WithMaxSubscribers(max int) ChannelOption {
	return func(o *channelOptions) {
		if max >= 0 {
			o.MaxSubscribers = max
		}
	}
}

// WithPersistent enables message persistence for a channel.
func WithPersistent(persistent bool) ChannelOption {
	return func(o *channelOptions) {
		o.Persistent = persistent
	}
}

// WithTTL sets the message TTL for a channel.
func WithTTL(ttl time.Duration) ChannelOption {
	return func(o *channelOptions) {
		if ttl >= 0 {
			o.TTL = ttl
		}
	}
}

// HubChannel represents a named notification channel in the hub.
type HubChannel struct {
	// Name is the channel name.
	Name string
	// Options contains the channel configuration.
	Options *channelOptions
	// CreatedAt is when the channel was created.
	CreatedAt time.Time
	// subscriberCount tracks the number of subscribers (updated in event loop).
	subscriberCount int
	// messagesPublished tracks the number of messages published (updated in event loop).
	messagesPublished int64
}

// SubscriberCount returns the current number of subscribers.
func (ch *HubChannel) SubscriberCount() int {
	return ch.subscriberCount
}

// MessagesPublished returns the total number of messages published to this channel.
func (ch *HubChannel) MessagesPublished() int64 {
	return ch.messagesPublished
}

// SubscribeRequest represents a request to subscribe to a channel.
type SubscribeRequest struct {
	Client   *Client
	Channel  string
	Response chan error
}

// UnsubscribeRequest represents a request to unsubscribe from a channel.
type UnsubscribeRequest struct {
	Client   *Client
	Channel  string
	Response chan error
}

// CreateChannelRequest represents a request to create a new channel.
type CreateChannelRequest struct {
	Name     string
	Options  *channelOptions
	Response chan *CreateChannelResponse
}

// CreateChannelResponse contains the result of a channel creation request.
type CreateChannelResponse struct {
	Channel *HubChannel
	Err     error
}

// DeleteChannelRequest represents a request to delete a channel.
type DeleteChannelRequest struct {
	Name     string
	Response chan error
}

// getChannelRequest represents a request to get channel information.
type getChannelRequest struct {
	Name     string
	Response chan *getChannelResponse
}

// getChannelResponse contains the result of a get channel request.
type getChannelResponse struct {
	Channel *HubChannel
	Found   bool
}

// listChannelsRequest represents a request to list all channels.
type listChannelsRequest struct {
	Response chan []*HubChannel
}

// NotificationHub coordinates all clients using channel-based pub/sub.
// It runs a single goroutine event loop with no mutexes in the hot path.
type NotificationHub struct {
	logger        *logger.Logger
	clients       map[*Client]bool            // Active subscribers
	channels      map[string]map[*Client]bool // channel -> subscribers
	broadcast     chan *Message               // Inbound messages
	register      chan *Client                // New client registrations
	unregister    chan *Client                // Client removal
	subscribe     chan *SubscribeRequest      // Channel subscriptions
	unsubscribe   chan *UnsubscribeRequest    // Channel unsubscriptions
	createChannel chan *CreateChannelRequest  // Channel creation
	deleteChannel chan *DeleteChannelRequest  // Channel deletion
	getChannel    chan *getChannelRequest     // Get channel info
	listChannels  chan *listChannelsRequest   // List all channels
	channelInfo   map[string]*HubChannel      // Channel metadata
	running       atomic.Bool
	clientCount   atomic.Int64 // Lock-free client count for safe concurrent reads.
	channelCount  atomic.Int64 // Lock-free channel count for safe concurrent reads.
	done          chan struct{}
	bufferSize    int
}

// NewNotificationHub creates a new NotificationHub with the given logger.
func NewNotificationHub(log *logger.Logger) *NotificationHub {
	return NewNotificationHubWithBufferSize(log, DefaultBufferSize)
}

// NewNotificationHubWithBufferSize creates a new NotificationHub with custom buffer size.
func NewNotificationHubWithBufferSize(log *logger.Logger, bufferSize int) *NotificationHub {
	if bufferSize <= 0 {
		bufferSize = DefaultBufferSize
	}
	return &NotificationHub{
		logger:        log,
		clients:       make(map[*Client]bool),
		channels:      make(map[string]map[*Client]bool),
		broadcast:     make(chan *Message, bufferSize),
		register:      make(chan *Client, bufferSize),
		unregister:    make(chan *Client, bufferSize),
		subscribe:     make(chan *SubscribeRequest, bufferSize),
		unsubscribe:   make(chan *UnsubscribeRequest, bufferSize),
		createChannel: make(chan *CreateChannelRequest, bufferSize),
		deleteChannel: make(chan *DeleteChannelRequest, bufferSize),
		getChannel:    make(chan *getChannelRequest, bufferSize),
		listChannels:  make(chan *listChannelsRequest, bufferSize),
		channelInfo:   make(map[string]*HubChannel),
		done:          make(chan struct{}),
		bufferSize:    bufferSize,
	}
}

// Run starts the single-threaded event loop.
// It processes all operations via channels to avoid mutex contention.
// This method blocks until the context is cancelled or Stop is called.
func (h *NotificationHub) Run(ctx context.Context) {
	if !h.running.CompareAndSwap(false, true) {
		h.logger.Warn("hub is already running")
		return
	}
	defer h.running.Store(false)

	h.logger.Info("notification hub started")

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("notification hub shutting down due to context cancellation")
			h.cleanup()
			return

		case <-h.done:
			h.logger.Info("notification hub shutting down due to stop signal")
			h.cleanup()
			return

		case client := <-h.register:
			h.handleRegister(client)

		case client := <-h.unregister:
			h.handleUnregister(client)

		case req := <-h.subscribe:
			h.handleSubscribe(req)

		case req := <-h.unsubscribe:
			h.handleUnsubscribe(req)

		case req := <-h.createChannel:
			h.handleCreateChannel(req)

		case req := <-h.deleteChannel:
			h.handleDeleteChannel(req)

		case req := <-h.getChannel:
			h.handleGetChannel(req)

		case req := <-h.listChannels:
			h.handleListChannels(req)

		case msg := <-h.broadcast:
			h.handleBroadcast(msg)
		}
	}
}

// Stop gracefully stops the notification hub.
func (h *NotificationHub) Stop() {
	if h.running.Load() {
		close(h.done)
	}
}

// IsRunning returns true if the hub is currently running.
func (h *NotificationHub) IsRunning() bool {
	return h.running.Load()
}

// RegisterClient registers a new client with the hub.
// This is a non-blocking operation.
func (h *NotificationHub) RegisterClient(client *Client) {
	if client == nil {
		return
	}
	select {
	case h.register <- client:
		// Successfully queued
	default:
		h.logger.Warn("register channel full, client registration dropped")
	}
}

// UnregisterClient removes a client from the hub.
// This is a non-blocking operation.
func (h *NotificationHub) UnregisterClient(client *Client) {
	if client == nil {
		return
	}
	select {
	case h.unregister <- client:
		// Successfully queued
	default:
		h.logger.Warn("unregister channel full, client unregistration dropped")
	}
}

// SubscribeToChannel subscribes a client to a channel.
// This is a synchronous operation that waits for the response.
func (h *NotificationHub) SubscribeToChannel(client *Client, channel string) error {
	if client == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "push.SubscribeToChannel",
			Err:  ErrNilClient,
		}
	}
	if channel == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "push.SubscribeToChannel",
			Err:  ErrEmptyChannelName,
		}
	}

	req := &SubscribeRequest{
		Client:   client,
		Channel:  channel,
		Response: make(chan error, 1),
	}

	select {
	case h.subscribe <- req:
		// Request sent, wait for response
	default:
		return &errors.QBError{
			Code: errors.ErrResourceExhausted,
			Op:   "push.SubscribeToChannel",
			Err:  ErrHubOverloaded,
		}
	}

	return <-req.Response
}

// UnsubscribeFromChannel unsubscribes a client from a channel.
// This is a synchronous operation that waits for the response.
func (h *NotificationHub) UnsubscribeFromChannel(client *Client, channel string) error {
	if client == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "push.UnsubscribeFromChannel",
			Err:  ErrNilClient,
		}
	}
	if channel == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "push.UnsubscribeFromChannel",
			Err:  ErrEmptyChannelName,
		}
	}

	req := &UnsubscribeRequest{
		Client:   client,
		Channel:  channel,
		Response: make(chan error, 1),
	}

	select {
	case h.unsubscribe <- req:
		// Request sent, wait for response
	default:
		return &errors.QBError{
			Code: errors.ErrResourceExhausted,
			Op:   "push.UnsubscribeFromChannel",
			Err:  ErrHubOverloaded,
		}
	}

	return <-req.Response
}

// CreateChannel creates a new notification channel.
// This is a synchronous operation that waits for the response.
func (h *NotificationHub) CreateChannel(name string, opts ...ChannelOption) (*HubChannel, error) {
	if name == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "push.CreateChannel",
			Err:  ErrEmptyChannelName,
		}
	}

	options := defaultChannelOptions()
	for _, opt := range opts {
		opt(options)
	}

	req := &CreateChannelRequest{
		Name:     name,
		Options:  options,
		Response: make(chan *CreateChannelResponse, 1),
	}

	select {
	case h.createChannel <- req:
		// Request sent, wait for response
	default:
		return nil, &errors.QBError{
			Code: errors.ErrResourceExhausted,
			Op:   "push.CreateChannel",
			Err:  ErrHubOverloaded,
		}
	}

	resp := <-req.Response
	return resp.Channel, resp.Err
}

// DeleteChannel deletes a notification channel.
// This is a synchronous operation that waits for the response.
func (h *NotificationHub) DeleteChannel(name string) error {
	if name == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "push.DeleteChannel",
			Err:  ErrEmptyChannelName,
		}
	}

	req := &DeleteChannelRequest{
		Name:     name,
		Response: make(chan error, 1),
	}

	select {
	case h.deleteChannel <- req:
		// Request sent, wait for response
	default:
		return &errors.QBError{
			Code: errors.ErrResourceExhausted,
			Op:   "push.DeleteChannel",
			Err:  ErrHubOverloaded,
		}
	}

	return <-req.Response
}

// Broadcast sends a message to all subscribers or to a specific channel.
// This is a non-blocking operation with backpressure handling.
func (h *NotificationHub) Broadcast(msg *Message) error {
	if msg == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "push.Broadcast",
			Err:  ErrNilMessage,
		}
	}

	// Set timestamp if not already set
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	select {
	case h.broadcast <- msg:
		return nil
	default:
		h.logger.Warn("broadcast channel full, message dropped")
		return &errors.QBError{
			Code: errors.ErrResourceExhausted,
			Op:   "push.Broadcast",
			Err:  ErrHubOverloaded,
		}
	}
}

// GetChannel returns channel information by name.
// This is a synchronous operation that waits for the response.
func (h *NotificationHub) GetChannel(name string) (*HubChannel, bool) {
	if name == "" {
		return nil, false
	}

	req := &getChannelRequest{
		Name:     name,
		Response: make(chan *getChannelResponse, 1),
	}

	select {
	case h.getChannel <- req:
		// Request sent, wait for response
	default:
		return nil, false
	}

	resp := <-req.Response
	return resp.Channel, resp.Found
}

// ListChannels returns a list of all channels.
// This is a synchronous operation that waits for the response.
func (h *NotificationHub) ListChannels() []*HubChannel {
	req := &listChannelsRequest{
		Response: make(chan []*HubChannel, 1),
	}

	select {
	case h.listChannels <- req:
		// Request sent, wait for response
	default:
		return nil
	}

	return <-req.Response
}

// ClientCount returns the number of connected clients.
func (h *NotificationHub) ClientCount() int {
	return int(h.clientCount.Load())
}

// ChannelCount returns the number of channels.
func (h *NotificationHub) ChannelCount() int {
	return int(h.channelCount.Load())
}

// handleRegister processes a client registration.
func (h *NotificationHub) handleRegister(client *Client) {
	if _, exists := h.clients[client]; exists {
		h.logger.WithField("client_id", client.ID).Debug("client already registered")
		return
	}

	h.clients[client] = true
	h.clientCount.Add(1)
	h.logger.WithField("client_id", client.ID).Debug("client registered")
}

// handleUnregister processes a client unregistration.
func (h *NotificationHub) handleUnregister(client *Client) {
	if _, exists := h.clients[client]; !exists {
		return
	}

	// Remove client from all channel subscriptions
	for channelName, subscribers := range h.channels {
		if subscribers[client] {
			delete(subscribers, client)
			delete(client.subscriptions, channelName)
			// Update subscriber count
			if info, exists := h.channelInfo[channelName]; exists {
				info.subscriberCount = len(subscribers)
			}
		}
	}

	delete(h.clients, client)
	h.clientCount.Add(-1)
	client.Close()
	h.logger.WithField("client_id", client.ID).Debug("client unregistered")
}

// handleSubscribe processes a channel subscription request.
func (h *NotificationHub) handleSubscribe(req *SubscribeRequest) {
	defer close(req.Response)

	// Honour the register-before-subscribe order the caller expressed (#76).
	// A caller registers a client and THEN subscribes it (PushService.Subscribe;
	// WebSocketClient.Start followed by Subscribe). RegisterClient enqueues the
	// client on the buffered h.register channel and returns; SubscribeToChannel
	// then enqueues this request on the separate buffered h.subscribe channel.
	// The Run loop selects across the two channels independently, so when both
	// are buffer-ready Go may pick this subscribe before the matching register —
	// and the guard below would then reject a client the caller has already
	// registered. Because the register send happens-before the subscribe send in
	// the caller (same goroutine, sequential channel sends), the matching
	// registration is already queued on h.register by the time this subscribe is
	// handled. The hub goroutine is the sole receiver of h.register.
	//
	// Drain EXACTLY the registrations buffered at entry — snapshot the length
	// once and receive that many — never "until empty". Concurrent RegisterClient
	// callers can refill h.register without bound; a drain-until-empty loop would
	// then never return and would starve shutdown, unregister, broadcast and
	// every other subscribe (the whole Run loop blocks in here). The snapshot is
	// sufficient: the matching registration is among the entries present at entry
	// (happens-before), and receives cannot block because this is the only
	// receiver and there are at least n buffered. Registrations that arrive after
	// the snapshot are handled by the Run loop on a later iteration.
	for n := len(h.register); n > 0; n-- {
		h.handleRegister(<-h.register)
	}

	// Check if client is registered
	if _, exists := h.clients[req.Client]; !exists {
		req.Response <- &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "push.handleSubscribe",
			Err:  ErrClientNotRegistered,
		}
		return
	}

	// Auto-create channel if it doesn't exist
	if _, exists := h.channelInfo[req.Channel]; !exists {
		h.channelInfo[req.Channel] = &HubChannel{
			Name:      req.Channel,
			Options:   defaultChannelOptions(),
			CreatedAt: time.Now(),
		}
		h.channels[req.Channel] = make(map[*Client]bool)
		h.channelCount.Add(1)
	}

	// Check max subscribers limit
	channelInfo := h.channelInfo[req.Channel]
	if channelInfo.Options.MaxSubscribers > 0 {
		currentCount := len(h.channels[req.Channel])
		if currentCount >= channelInfo.Options.MaxSubscribers {
			req.Response <- &errors.QBError{
				Code: errors.ErrResourceExhausted,
				Op:   "push.handleSubscribe",
				Err:  ErrMaxSubscribersReached,
			}
			return
		}
	}

	// Add subscription
	h.channels[req.Channel][req.Client] = true
	req.Client.subscriptions[req.Channel] = true
	channelInfo.subscriberCount = len(h.channels[req.Channel])

	h.logger.WithFields(map[string]interface{}{
		"client_id": req.Client.ID,
		"channel":   req.Channel,
	}).Debug("client subscribed to channel")

	req.Response <- nil
}

// handleUnsubscribe processes a channel unsubscription request.
func (h *NotificationHub) handleUnsubscribe(req *UnsubscribeRequest) {
	defer close(req.Response)

	// Check if client is registered
	if _, exists := h.clients[req.Client]; !exists {
		req.Response <- &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "push.handleUnsubscribe",
			Err:  ErrClientNotRegistered,
		}
		return
	}

	// Check if channel exists
	subscribers, channelExists := h.channels[req.Channel]
	if !channelExists {
		req.Response <- &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "push.handleUnsubscribe",
			Err:  ErrChannelNotFound,
		}
		return
	}

	// Remove subscription
	delete(subscribers, req.Client)
	delete(req.Client.subscriptions, req.Channel)

	// Update subscriber count
	if info, exists := h.channelInfo[req.Channel]; exists {
		info.subscriberCount = len(subscribers)
	}

	h.logger.WithFields(map[string]interface{}{
		"client_id": req.Client.ID,
		"channel":   req.Channel,
	}).Debug("client unsubscribed from channel")

	req.Response <- nil
}

// handleCreateChannel processes a channel creation request.
func (h *NotificationHub) handleCreateChannel(req *CreateChannelRequest) {
	defer close(req.Response)

	// Check if channel already exists
	if _, exists := h.channelInfo[req.Name]; exists {
		req.Response <- &CreateChannelResponse{
			Err: &errors.QBError{
				Code: errors.ErrAlreadyExists,
				Op:   "push.handleCreateChannel",
				Err:  ErrChannelExists,
			},
		}
		return
	}

	// Create the channel
	channel := &HubChannel{
		Name:      req.Name,
		Options:   req.Options,
		CreatedAt: time.Now(),
	}

	h.channelInfo[req.Name] = channel
	h.channels[req.Name] = make(map[*Client]bool)
	h.channelCount.Add(1)

	h.logger.WithField("channel", req.Name).Debug("channel created")

	req.Response <- &CreateChannelResponse{
		Channel: channel,
	}
}

// handleDeleteChannel processes a channel deletion request.
func (h *NotificationHub) handleDeleteChannel(req *DeleteChannelRequest) {
	defer close(req.Response)

	// Check if channel exists
	subscribers, exists := h.channels[req.Name]
	if !exists {
		req.Response <- &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "push.handleDeleteChannel",
			Err:  ErrChannelNotFound,
		}
		return
	}

	// Remove all subscriptions for this channel
	for client := range subscribers {
		delete(client.subscriptions, req.Name)
	}

	// Delete the channel
	delete(h.channels, req.Name)
	delete(h.channelInfo, req.Name)
	h.channelCount.Add(-1)

	h.logger.WithField("channel", req.Name).Debug("channel deleted")

	req.Response <- nil
}

// handleGetChannel processes a get channel request.
func (h *NotificationHub) handleGetChannel(req *getChannelRequest) {
	defer close(req.Response)

	channel, exists := h.channelInfo[req.Name]
	if !exists {
		req.Response <- &getChannelResponse{
			Found: false,
		}
		return
	}

	// Return a copy to prevent external modification
	channelCopy := &HubChannel{
		Name:              channel.Name,
		Options:           channel.Options,
		CreatedAt:         channel.CreatedAt,
		subscriberCount:   channel.subscriberCount,
		messagesPublished: channel.messagesPublished,
	}

	req.Response <- &getChannelResponse{
		Channel: channelCopy,
		Found:   true,
	}
}

// handleListChannels processes a list channels request.
func (h *NotificationHub) handleListChannels(req *listChannelsRequest) {
	defer close(req.Response)

	result := make([]*HubChannel, 0, len(h.channelInfo))
	for _, channel := range h.channelInfo {
		// Return copies to prevent external modification
		channelCopy := &HubChannel{
			Name:              channel.Name,
			Options:           channel.Options,
			CreatedAt:         channel.CreatedAt,
			subscriberCount:   channel.subscriberCount,
			messagesPublished: channel.messagesPublished,
		}
		result = append(result, channelCopy)
	}

	req.Response <- result
}

// handleBroadcast processes a broadcast message.
func (h *NotificationHub) handleBroadcast(msg *Message) {
	var targets map[*Client]bool

	if msg.Channel != "" {
		// Send to specific channel subscribers
		subscribers, exists := h.channels[msg.Channel]
		if !exists {
			h.logger.WithField("channel", msg.Channel).Debug("broadcast to non-existent channel")
			return
		}
		targets = subscribers
		// Track per-channel publish count
		if info, ok := h.channelInfo[msg.Channel]; ok {
			info.messagesPublished++
		}
	} else {
		// Send to all clients
		targets = h.clients
	}

	for client := range targets {
		h.sendToClient(client, msg)
	}
}

// sendToClient sends a message to a specific client with slow consumer protection.
func (h *NotificationHub) sendToClient(client *Client, msg *Message) {
	if !client.IsConnected() {
		return
	}

	select {
	case client.Send <- msg:
		// Message sent successfully
	default:
		// Client is slow - disconnect it
		h.logger.WithField("client_id", client.ID).Warn("slow consumer detected, disconnecting client")
		h.handleUnregister(client)
	}
}

// cleanup performs cleanup when the hub is shutting down.
func (h *NotificationHub) cleanup() {
	// Close all client connections
	for client := range h.clients {
		client.Close()
	}

	// Clear all maps
	h.clients = make(map[*Client]bool)
	h.channels = make(map[string]map[*Client]bool)
	h.channelInfo = make(map[string]*HubChannel)

	h.logger.Info("notification hub cleanup completed")
}
