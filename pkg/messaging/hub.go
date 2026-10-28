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
	"sync"
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
	// messagesPublished counts the messages published to this channel. Broadcast
	// increments it when it enqueues a message, so a returned publish is counted
	// before the event loop delivers it; every copy of the channel shares it.
	messagesPublished *atomic.Int64
}

// SubscriberCount returns the current number of subscribers.
func (ch *HubChannel) SubscriberCount() int {
	return ch.subscriberCount
}

// MessagesPublished returns the total number of messages published to this channel.
func (ch *HubChannel) MessagesPublished() int64 {
	if ch.messagesPublished == nil {
		return 0
	}
	return ch.messagesPublished.Load()
}

// SubscribeRequest represents a request to subscribe to a channel.
type SubscribeRequest struct {
	Client   *Client
	Channel  string
	Response chan error

	// Register asks the hub to register the client as part of handling this
	// request, when it is not registered already.
	//
	// IT EXISTS BECAUSE REGISTER-THEN-SUBSCRIBE IS NOT ORDERED ACROSS TWO
	// CHANNELS (#339). RegisterClient queues on h.register and returns; a
	// following SubscribeToChannel queues on h.subscribe. One select drains
	// both, and Go chooses uniformly at random among ready cases — so when
	// both are queued before the hub cycles, the subscribe is handled first
	// and the client is reported unregistered. Measured at 45 failures in 50
	// concurrent subscribes.
	//
	// The hub's loop is single-threaded, so doing both inside ONE request is
	// ordered by construction rather than by hoping. The flag is opt-in
	// precisely so SubscribeToChannel keeps refusing an unregistered client,
	// which is its own tested contract.
	Register bool

	// Op names the exported entry point the caller used.
	//
	// IT IS ON THE REQUEST BECAUSE THE REQUEST OUTLIVES THE CALL (#345). Two
	// entry points share this type, and a request abandoned by a shutdown is
	// answered by discardQueuedRequests, which sees only what is in the queue.
	// Without carrying the op, that answer has to guess, and a drained
	// RegisterAndSubscribe reports a SubscribeToChannel the caller never made.
	Op string
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
	published     sync.Map                    // channel name -> *atomic.Int64, read by Broadcast off the event loop
	running       atomic.Bool
	clientCount   atomic.Int64 // Lock-free client count for safe concurrent reads.
	channelCount  atomic.Int64 // Lock-free channel count for safe concurrent reads.
	// done signals the CURRENT loop to stop, and is replaced for each one.
	//
	// PER-RUN, NOT PER-HUB (#354). A single channel closed once can never be
	// reopened, so a Stop that closed it while no loop was running would
	// poison every later Run — each would see a closed channel and exit
	// immediately. Renewing it when a loop starts keeps stopping one run from
	// deciding anything about the next.
	done chan struct{}

	// stopped is closed whenever the hub's loop is NOT running, and is
	// replaced with a fresh open channel each time Run starts.
	//
	// IT IS WHAT MAKES A QUEUED REQUEST ANSWERABLE (#345). Every synchronous
	// call queues into a buffered channel and then waits on a response only
	// handleX can write — so a request still queued when Run returns had no
	// writer left, and its caller waited forever. Not a timeout, not an error:
	// an unbounded wait on a channel nobody will ever write to, which on a
	// daemon is a shutdown that does not complete.
	//
	// A CLOSED CHANNEL IS THE STARTING STATE, deliberately. A hub that was
	// never started must refuse rather than hang, and "not running yet" and
	// "not running any more" are the same answer to the only question a
	// waiter is asking.
	stopped chan struct{}

	// lifecycleMu serialises every transition of the pair above: Run's renewal
	// of done against Stop's close of it (#354), and Run's reopening of stopped
	// against admit's check-and-enqueue and shutdownAdmission's close (#345).
	// One lock, because they are one transition — a loop is claimed, its stop
	// signal renewed and its admission opened, or none of that happens.
	lifecycleMu sync.Mutex

	bufferSize int

	// onClientUncounted, when non-nil, is called on the hub's goroutine at the
	// instant an unregister has made a client's departure visible to
	// ClientCount, and is the only way to observe that instant from outside.
	//
	// IT IS WHAT MAKES handleUnregister's ORDERING TESTABLE (#588). The guarantee
	// handleUnregister owes is "the count does not drop until the client is
	// closed", and the gap a wrong order opens is a few instructions wide —
	// far too narrow for a test to catch by sampling ClientCount in a loop, as
	// #588 showed by failing roughly one run in ten instead of every run. A
	// hub set up with this hook is handed that instant directly, so the
	// ordering is asserted rather than raced for.
	//
	// Set before Run starts, never after: the hub's goroutine reads it, and
	// the goroutine's creation is what publishes the write.
	onClientUncounted func(*Client)
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
		stopped:       closedChannel(),
		bufferSize:    bufferSize,
	}
}

// Run starts the single-threaded event loop.
// It processes all operations via channels to avoid mutex contention.
// This method blocks until the context is cancelled or Stop is called.
func (h *NotificationHub) Run(ctx context.Context) {
	// THE CLAIM, THE RENEWAL AND THE ADMISSION ARE ONE STEP, under the lock
	// both Stop and admit take. Two separate defects were found here in review,
	// on either side of the same window, and both are a flag published before
	// the state it advertises is established.
	//
	// Split from the renewal, a Stop is LOST (#354). Run marked the hub running
	// and THEN renewed h.done; a Stop landing between the two took the lock, saw
	// running, closed the OLD channel and returned — and Run then installed a
	// fresh open one and waited on that instead. The shutdown was accepted and
	// silently discarded, leaving the hub up until its context was cancelled.
	//
	// Split from the admission, the flag LIES (#345). Run claimed running and
	// opened admission afterwards, so a caller that saw IsRunning() true in
	// between — which is exactly what PushService.Start and these tests use as
	// the readiness signal — reached admit while the signal was still the closed
	// one, and was told the hub had stopped by a hub that had just started.
	//
	// Holding the lock across all of it means Stop cannot observe a running hub
	// until h.done is already the channel this loop will select on, and a caller
	// may still read IsRunning() without the lock but its admit then blocks here
	// until admission is genuinely open.
	//
	// THE LOOP'S LIFETIME, PUBLISHED. Opened here and closed on every exit —
	// context cancellation, an explicit Stop, or a panic — so a caller waiting
	// on a response it can no longer receive is told, instead of waiting
	// forever (#345). Closed AFTER the loop has left, so no handler is still
	// running when a waiter wakes and takes its final look.
	h.lifecycleMu.Lock()
	if !h.running.CompareAndSwap(false, true) {
		h.lifecycleMu.Unlock()
		h.logger.Warn("hub is already running")
		return
	}
	h.done = make(chan struct{})
	done := h.done
	// Anything left by a previous loop belongs to callers already told the hub
	// had stopped, and must not be performed now — discarded while admission is
	// still closed, so nothing admitted by THIS loop can be caught by it.
	h.discardQueuedRequests()
	h.stopped = make(chan struct{})
	h.lifecycleMu.Unlock()

	// THE EXIT ORDER IS DELIBERATE AND IS THE OPPOSITE HALF OF THE SAME RULE.
	// Defers run last-registered-first, so admission closes and the queues
	// drain BEFORE running goes false. That leaves a moment where IsRunning()
	// still reports true and admission is already closed — a caller is told the
	// hub stopped, which is TRUE, it is merely early. Reversed, there would be
	// a moment where IsRunning() reports false while admission is still open,
	// and a request would be admitted into a loop that is leaving. Of the two
	// windows the honest one is the one that refuses.
	defer h.running.Store(false)
	defer h.shutdownAdmission()

	h.logger.Info("notification hub started")

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("notification hub shutting down due to context cancellation")
			h.cleanup()
			return

		case <-done:
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
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()

	// STILL A NO-OP WHILE NOTHING IS RUNNING, which is the half that is easy
	// to lose while fixing the other one. Closing unconditionally would make a
	// Stop issued before the first Run — or after a context-driven exit —
	// permanently disable the hub.
	if !h.running.Load() {
		return
	}

	// IDEMPOTENT, BECAUSE load-then-close IS NOT. Two callers could both read
	// running as true and both reach the close, and the second close PANICS —
	// taking down whichever goroutine called Stop rather than the hub.
	// PushService.Stop has the same shape above it, and overlapping shutdowns
	// are what restarting a daemon produces.
	select {
	case <-h.done:
		// Already closed by a concurrent Stop.
	default:
		close(h.done)
	}
}

// IsRunning returns true if the hub is currently running.
func (h *NotificationHub) IsRunning() bool {
	return h.running.Load()
}

// RegisterClient registers a new client with the hub.
// This is a non-blocking operation.
//
// ADMITTED LIKE EVERY OTHER REQUEST (#696). These two queues were bare
// non-blocking sends with no liveness check, and their contents — buffered, and
// not drained by discardQueuedRequests — simply outlived the loop they were
// meant for. Run then delivered a register queued against a DEPARTED loop to
// the NEXT one, which inserted into its fresh registry a client whose caller
// had been told the hub had stopped and which the previous loop's cleanup had
// already closed: a closed client, counted, in a hub that never knew it.
// Admission is what makes "the hub did not take it" true at the moment the
// caller is told so, and the drain below is what keeps it true afterwards.
//
// THE REFUSAL IS LOGGED, NOT RETURNED, because this signature has no error to
// carry one and the asynchronous hand-off is the point — callers that need the
// registration to have happened use RegisterAndSubscribe, which is synchronous
// and does report.
func (h *NotificationHub) RegisterClient(client *Client) {
	if client == nil {
		return
	}
	if err := h.admit("push.RegisterClient", func() bool {
		select {
		case h.register <- client:
			return true
		default:
			return false
		}
	}); err != nil {
		h.logger.WithError(err).WithField("client_id", client.ID).
			Warn("client registration dropped")
	}
}

// UnregisterClient removes a client from the hub.
// This is a non-blocking operation.
//
// A REFUSED UNREGISTER STILL CLOSES THE CLIENT (#696), which is the half of the
// unregister a hub with no loop can still honour. Dropping it silently left the
// caller holding a client it had been promised the hub was finished with, and
// left the request in a queue for a later loop that would find the client
// absent and do nothing — benign only by accident, and litter in the capacity
// the next loop needs either way.
func (h *NotificationHub) UnregisterClient(client *Client) {
	if client == nil {
		return
	}
	if err := h.admit("push.UnregisterClient", func() bool {
		select {
		case h.unregister <- client:
			return true
		default:
			return false
		}
	}); err != nil {
		h.logger.WithError(err).WithField("client_id", client.ID).
			Warn("client unregistration dropped")
		client.Close()
	}
}

// SubscribeToChannel subscribes a client to a channel.
// This is a synchronous operation that waits for the response.
func (h *NotificationHub) SubscribeToChannel(client *Client, channel string) error {
	if err := validateSubscribeTarget(client, channel); err != nil {
		return err
	}
	return h.sendSubscribe(client, channel, false, "push.SubscribeToChannel")
}

// validateSubscribeTarget refuses the arguments no subscribe can act on.
func validateSubscribeTarget(client *Client, channel string) error {
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
	return nil
}

// RegisterAndSubscribe registers the client if it is not registered already and
// subscribes it to the channel, as ONE request the hub handles in order.
//
// THE TWO-CALL FORM IS A RACE, and this method exists because it cannot be
// fixed at the call site. RegisterClient queues asynchronously on h.register
// and SubscribeToChannel queues on h.subscribe; the hub's single select drains
// both and Go picks uniformly at random among ready cases, so a caller that
// registers and then subscribes can have the subscribe handled first and be
// told its client is not registered (#339). No amount of care in the caller
// orders two independent channels — the ordering has to happen where both are
// consumed.
//
// Callers that genuinely want the strict check — a subscribe that must FAIL for
// an unregistered client — keep using SubscribeToChannel.
func (h *NotificationHub) RegisterAndSubscribe(client *Client, channel string) error {
	if err := validateSubscribeTarget(client, channel); err != nil {
		return err
	}
	return h.sendSubscribe(client, channel, true, "push.RegisterAndSubscribe")
}

// sendSubscribe queues one subscribe request and waits for the hub's answer.
//
// op names the exported entry point the caller used, so a refusal attributes
// itself to the call that was actually made rather than to whichever of the two
// happens to share this body.
func (h *NotificationHub) sendSubscribe(client *Client, channel string, register bool, op string) error {
	req := &SubscribeRequest{
		Client:   client,
		Channel:  channel,
		Response: make(chan error, 1),
		Register: register,
		Op:       op,
	}

	if err := h.admit(op, func() bool {
		select {
		case h.subscribe <- req:
			return true
		default:
			return false
		}
	}); err != nil {
		return err
	}

	err, waitErr := awaitHub(h, op, req.Response)
	if waitErr != nil {
		return waitErr
	}
	return err
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

	if err := h.admit("push.UnsubscribeFromChannel", func() bool {
		select {
		case h.unsubscribe <- req:
			return true
		default:
			return false
		}
	}); err != nil {
		return err
	}

	err, waitErr := awaitHub(h, "push.UnsubscribeFromChannel", req.Response)
	if waitErr != nil {
		return waitErr
	}
	return err
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

	if err := h.admit("push.CreateChannel", func() bool {
		select {
		case h.createChannel <- req:
			return true
		default:
			return false
		}
	}); err != nil {
		return nil, err
	}

	resp, waitErr := awaitHub(h, "push.CreateChannel", req.Response)
	if waitErr != nil {
		return nil, waitErr
	}
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

	if err := h.admit("push.DeleteChannel", func() bool {
		select {
		case h.deleteChannel <- req:
			return true
		default:
			return false
		}
	}); err != nil {
		return err
	}

	err, waitErr := awaitHub(h, "push.DeleteChannel", req.Response)
	if waitErr != nil {
		return waitErr
	}
	return err
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
		h.countPublished(msg.Channel)
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
//
// A HUB THAT CANNOT ANSWER IS REPORTED AS NOT FOUND, because this signature
// carries no error — a stopped hub and a full queue both come back the same as
// a name nobody ever created. Callers that can act on the difference use
// getChannelE instead (#352); this remains for callers that genuinely only
// want to know whether the channel is there right now.
// This is a synchronous operation that waits for the response.
func (h *NotificationHub) GetChannel(name string) (*HubChannel, bool) {
	channel, found, _ := h.getChannelE(name)
	return channel, found
}

// getChannelE is GetChannel with the reason a query went unanswered.
//
// THE ERROR AND THE "NOT FOUND" ARE DIFFERENT ANSWERS. found is only
// meaningful when err is nil: a non-nil err means the hub did not look, so
// (nil, false, err) says nothing about whether the channel exists. Reading
// found without checking err first is how a shutdown becomes "no such
// channel" at a boundary that could have said "unavailable" (#352).
func (h *NotificationHub) getChannelE(name string) (*HubChannel, bool, error) {
	if name == "" {
		return nil, false, nil
	}

	req := &getChannelRequest{
		Name:     name,
		Response: make(chan *getChannelResponse, 1),
	}

	if err := h.admit("push.GetChannel", func() bool {
		select {
		case h.getChannel <- req:
			return true
		default:
			return false
		}
	}); err != nil {
		return nil, false, err
	}

	resp, waitErr := awaitHub(h, "push.GetChannel", req.Response)
	if waitErr != nil {
		return nil, false, waitErr
	}
	return resp.Channel, resp.Found, nil
}

// ListChannels returns a list of all channels.
//
// A HUB THAT CANNOT ANSWER LISTS NOTHING, for the same reason GetChannel
// reports not-found: there is no error to return here. Callers that must tell
// an empty hub from an unavailable one use listChannelsE (#352).
// This is a synchronous operation that waits for the response.
func (h *NotificationHub) ListChannels() []*HubChannel {
	channels, _ := h.listChannelsE()
	return channels
}

// listChannelsE is ListChannels with the reason a query went unanswered.
//
// A nil slice with a nil error is an empty hub; a nil slice with a non-nil
// error is a hub that never looked. Only the first of those is an answer.
func (h *NotificationHub) listChannelsE() ([]*HubChannel, error) {
	req := &listChannelsRequest{
		Response: make(chan []*HubChannel, 1),
	}

	if err := h.admit("push.ListChannels", func() bool {
		select {
		case h.listChannels <- req:
			return true
		default:
			return false
		}
	}); err != nil {
		return nil, err
	}

	channels, waitErr := awaitHub(h, "push.ListChannels", req.Response)
	if waitErr != nil {
		return nil, waitErr
	}
	return channels, nil
}

// ClientCount returns the number of connected clients.
func (h *NotificationHub) ClientCount() int {
	return int(h.clientCount.Load())
}

// ChannelCount returns the number of channels.
func (h *NotificationHub) ChannelCount() int {
	return int(h.channelCount.Load())
}

// discardQueuedRequests answers every request sitting in the hub's queues with
// ErrHubStopped and leaves the queues empty.
//
// RUN AT BOTH ENDS OF A LOOP, and the second one is the one that is easy to
// miss. On the way OUT it answers requests that were accepted and can no longer
// be handled. On the way IN it discards anything left over from a PREVIOUS
// loop — because a caller that was already told "stopped" must not have its
// work performed by the next loop to start. Without that, a CreateChannel could
// report failure and create the channel anyway once the service restarted,
// which is a worse answer than the hang this all replaces: a hang is honest.
//
// Every response channel is buffered with room for one, so answering never
// blocks — which is what matters here, because the caller may still be waiting
// (the way OUT abandons live callers) or may have already gone (the way IN
// discards a previous loop's leftovers). The drain must not care which.
//
// THE REGISTRATION QUEUES ARE DRAINED HERE TOO, and they are the reason this
// function is not only about answering waiters (#696). Nobody waits on a
// register or an unregister — both are asynchronous hand-offs — so it is easy
// to read them as having nothing to abandon. What they leave behind is WORK,
// and work performed by a loop the caller was never talking to is worse than an
// unanswered request: the caller cannot even tell it happened.
//
// THE OPERATION COMES FROM THE REQUEST, NOT FROM THE QUEUE. Every other queue
// has exactly one exported entry point, so naming it here is unambiguous;
// h.subscribe has two, and hard-coding either one made a drained
// RegisterAndSubscribe report a SubscribeToChannel it never called. Attribution
// that is right on the admission path and wrong on the shutdown path is worse
// than none, because it is only wrong when something has already gone wrong.
func (h *NotificationHub) discardQueuedRequests() {
	stopped := func(op string) error {
		return &errors.QBError{Code: errors.ErrUnavailable, Op: op, Err: ErrHubStopped}
	}
	for {
		select {
		case client := <-h.register:
			// DROPPED, NOT PERFORMED. The caller was told the hub had stopped,
			// and performing it now is what resurrected a closed client into
			// the next loop's registry (#696). The client is NOT closed here:
			// the hub never took it, and a registration that did not happen
			// says nothing about a client its owner still holds.
			h.logger.WithField("client_id", client.ID).
				Debug("queued client registration discarded")
		case client := <-h.unregister:
			// DISCARDED, BUT THE CLIENT IS STILL CLOSED — the half of an
			// unregister that needs no registry, and the half the caller was
			// promised. Idempotent, so a client cleanup has already closed is
			// unaffected.
			client.Close()
			h.logger.WithField("client_id", client.ID).
				Debug("queued client unregistration discarded")
		case req := <-h.subscribe:
			req.Response <- stopped(req.Op)
			close(req.Response)
		case req := <-h.unsubscribe:
			req.Response <- stopped("push.UnsubscribeFromChannel")
			close(req.Response)
		case req := <-h.createChannel:
			req.Response <- &CreateChannelResponse{Err: stopped("push.CreateChannel")}
			close(req.Response)
		case req := <-h.deleteChannel:
			req.Response <- stopped("push.DeleteChannel")
			close(req.Response)
		case req := <-h.getChannel:
			req.Response <- &getChannelResponse{}
			close(req.Response)
		case req := <-h.listChannels:
			req.Response <- nil
			close(req.Response)
		default:
			return
		}
	}
}

// shutdownAdmission closes admission and then empties what it accepted.
//
// THE ORDER IS THE FIX (#345, second round). Draining first and closing after
// leaves a window: a caller can pass the liveness check, the loop can exit and
// drain, the caller can then enqueue, and its wait can observe the now-closed
// signal — so it is told the hub stopped while its request sits in a queue for
// the next loop to perform. That is the lie this change exists to avoid, and
// rarity does not change its kind.
//
// Closing under the lifecycle lock first means no admission can succeed after
// this point, so the drain that follows is the last word on what was accepted.
func (h *NotificationHub) shutdownAdmission() {
	h.closeStopSignal()
	h.discardQueuedRequests()
}

// admit queues one request, or refuses because the hub cannot handle it.
//
// THE LIVENESS CHECK AND THE ENQUEUE ARE ONE STEP, under the lock shutdown
// takes to close admission. Split, they are a check-then-act across a
// lifecycle transition, and the request can land after the drain that was
// supposed to be final. Joined, a request is either admitted while a loop is
// there to handle it, or refused — never both.
//
// enqueue reports whether the non-blocking send succeeded; it never blocks, so
// holding the lock across it costs nothing.
func (h *NotificationHub) admit(op string, enqueue func() bool) error {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()

	select {
	case <-h.stopped:
		return &errors.QBError{Code: errors.ErrUnavailable, Op: op, Err: ErrHubStopped}
	default:
	}
	if !enqueue() {
		return &errors.QBError{Code: errors.ErrResourceExhausted, Op: op, Err: ErrHubOverloaded}
	}
	return nil
}

// closedChannel returns a channel that is already closed.
func closedChannel() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// closeStopSignal marks the loop as gone, waking every waiter.
func (h *NotificationHub) closeStopSignal() {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	select {
	case <-h.stopped:
		// Already closed; closing twice panics.
	default:
		close(h.stopped)
	}
}

// stopSignal reports the channel that closes when the current loop ends.
func (h *NotificationHub) stopSignal() <-chan struct{} {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	return h.stopped
}

// awaitHub waits for a queued request's answer, or reports that the hub is
// gone.
//
// THE ONE PLACE THE WAIT HAPPENS, on purpose. Six exported calls queue a
// request and then read its response, and a guard written six times is a guard
// that will be five places the next time somebody adds a seventh. The rule is
// the type's, not the call site's: a request that can be queued must be
// answerable.
//
// THE SECOND LOOK IS NOT BELT AND BRACES. When the loop exits it closes the
// signal AFTER leaving, so a handler may have written an answer that is sitting
// in the buffered response channel while the signal is already closed — and
// select would then choose between them at random. Draining the response once
// more before reporting a stop means a request that WAS answered reports its
// answer, and only a genuinely abandoned one reports the stop.
func awaitHub[T any](h *NotificationHub, op string, response <-chan T) (T, error) {
	var zero T
	// A request that reaches this wait was ADMITTED while a loop existed to
	// handle it (see admit), so the only way it goes unanswered is the loop
	// leaving — which is what the stop branch below is for.
	select {
	case value := <-response:
		return value, nil
	case <-h.stopSignal():
		select {
		case value := <-response:
			return value, nil
		default:
			return zero, &errors.QBError{
				Code: errors.ErrUnavailable,
				Op:   op,
				Err:  ErrHubStopped,
			}
		}
	}
}

// handleRegister processes a client registration.
func (h *NotificationHub) handleRegister(client *Client) {
	if !h.registerClient(client) {
		h.logger.WithField("client_id", client.ID).Debug("client already registered")
	}
}

// registerClient adds a client to the hub's registry, reporting whether it was
// newly added. Callers run on the hub's own goroutine.
//
// Factored out so registration has ONE definition: handleSubscribe performs it
// too for a request that asks (see SubscribeRequest.Register), and a second
// spelling would be where the client count and the map stopped agreeing.
func (h *NotificationHub) registerClient(client *Client) bool {
	if _, exists := h.clients[client]; exists {
		return false
	}
	h.clients[client] = true
	h.clientCount.Add(1)
	h.logger.WithField("client_id", client.ID).Debug("client registered")
	return true
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

	// CLOSE FIRST, UNCOUNT LAST (#588). ClientCount is the only signal a caller
	// outside the hub's goroutine has that an asynchronous unregister finished,
	// so the count must not drop until the unregister has done everything it
	// owes — and what it owes is the close.
	//
	// Decrementing before closing published "this client is gone" while the
	// client was still connected, and a caller that waited for the count and
	// then acted on the client saw a departed client that had not been closed:
	// a window of a few instructions, but one CI hit reliably enough to fail an
	// unrelated pull request. Both fields are sync/atomic and therefore
	// sequentially consistent, so an observer that sees the decrement is
	// guaranteed to see the close that precedes it here.
	client.Close()
	delete(h.clients, client)
	h.uncountClient(client)
	h.logger.WithField("client_id", client.ID).Debug("client unregistered")
}

// uncountClient makes a departing client's removal visible to ClientCount, and
// must be the LAST thing an unregister does to it.
//
// Factored out so the decrement has one definition and the observation hook
// cannot drift away from it: the hook's whole contract is "called at the
// instant the count drops", and a hook that is merely a neighbouring statement
// stops meaning that the moment the statements around it are reordered — which
// is precisely the edit this exists to catch.
func (h *NotificationHub) uncountClient(client *Client) {
	h.clientCount.Add(-1)
	if h.onClientUncounted != nil {
		h.onClientUncounted(client)
	}
}

// handleSubscribe processes a channel subscription request.
func (h *NotificationHub) handleSubscribe(req *SubscribeRequest) {
	defer close(req.Response)

	// Check if client is registered — or register it, when the request asked
	// for both in one ordered step (#339).
	//
	// A DISCONNECTED CLIENT IS NEVER REGISTERED BACK, and that condition is
	// what keeps this from resurrecting the dead. handleUnregister removes a
	// client and CLOSES it, and an unregister racing a subscribe is the one
	// interleaving where "register it if absent" would undo a deliberate
	// removal: the client would return to h.clients disconnected, be counted,
	// and be handed messages nothing will ever read. Absent AND disconnected
	// therefore gets the same refusal it got before this request type existed.
	if _, exists := h.clients[req.Client]; !exists {
		if !req.Register || !req.Client.IsConnected() {
			req.Response <- &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "push.handleSubscribe",
				Err:  ErrClientNotRegistered,
			}
			return
		}
		h.registerClient(req.Client)
	}

	// Auto-create channel if it doesn't exist
	if _, exists := h.channelInfo[req.Channel]; !exists {
		h.channelInfo[req.Channel] = &HubChannel{
			Name:              req.Channel,
			messagesPublished: h.newPublishCounter(req.Channel),
			Options:           defaultChannelOptions(),
			CreatedAt:         time.Now(),
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

	channel.messagesPublished = h.newPublishCounter(req.Name)
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
	h.published.Delete(req.Name)
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
	} else {
		// Send to all clients
		targets = h.clients
	}

	for client := range targets {
		h.sendToClient(client, msg)
	}
}

// newPublishCounter creates the publish counter of a new channel and registers
// it for Broadcast. It runs on the event loop, where channels are created.
func (h *NotificationHub) newPublishCounter(name string) *atomic.Int64 {
	counter := &atomic.Int64{}
	h.published.Store(name, counter)
	return counter
}

// countPublished counts an enqueued message against its channel. Broadcast
// calls it once the message is queued, so a stats read after a returned
// publish sees it even before the event loop delivers the message.
func (h *NotificationHub) countPublished(channel string) {
	if channel == "" {
		return
	}
	if counter, ok := h.published.Load(channel); ok {
		counter.(*atomic.Int64).Add(1)
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
	// Close all client connections FIRST, for the reason handleUnregister does
	// (#588): ClientCount falling to zero must mean every client it was
	// counting is closed, and the counts below are what publish that.
	for client := range h.clients {
		client.Close()
	}

	// Clear all maps
	h.clients = make(map[*Client]bool)
	h.channels = make(map[string]map[*Client]bool)
	h.channelInfo = make(map[string]*HubChannel)
	h.published.Clear()

	// AND THE COUNTS DERIVED FROM THEM, in the same place, because they are
	// not independent facts — ClientCount and ChannelCount exist only to
	// report the size of the maps above without taking the loop's goroutine.
	// Cleared apart from the maps they would drift, and the drift is silent:
	// nothing reads a counter and a map together to notice.
	//
	// REACHABLE ONLY SINCE RESTART BECAME POSSIBLE (#354, found in review).
	// The stale counts were always written; before `done` was renewed per run
	// a second Run could not start, so nothing could ever observe them. Making
	// a previously impossible path possible opens every piece of state along
	// it — the counters were not introduced here, the way to reach them was.
	h.clientCount.Store(0)
	h.channelCount.Store(0)

	h.logger.Info("notification hub cleanup completed")
}
