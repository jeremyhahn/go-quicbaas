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
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// Channel represents a notification channel returned by the Service.
type Channel struct {
	// Name is the unique channel identifier.
	Name string

	// Codec is the default codec for messages on this channel.
	Codec string

	// CreatedAt is when the channel was created.
	CreatedAt time.Time

	// Subscribers is the current count of active subscribers.
	Subscribers int64

	// MessagesPublished is the number of messages published to this channel.
	MessagesPublished int64

	// Persistent indicates whether messages should be persisted for replay.
	Persistent bool

	// MaxHistory is the maximum number of messages to retain for replay.
	// Zero means no history is retained.
	MaxHistory int

	// BufferSize is the message buffer size for the channel.
	BufferSize int

	// TTL is the time-to-live for messages in this channel.
	TTL time.Duration
}

// PublishResult contains the result of a publish operation.
type PublishResult struct {
	// MessageID is the unique identifier for the published message.
	MessageID string
	// IsDuplicate indicates whether the message was detected as a duplicate.
	IsDuplicate bool
}

// Service defines the push notification service interface.
type Service interface {
	// Channel operations
	CreateChannel(ctx context.Context, name string, opts ...CreateChannelOption) (*Channel, error)
	DeleteChannel(ctx context.Context, name string) error
	GetChannel(ctx context.Context, name string) (*Channel, error)
	ListChannels(ctx context.Context) ([]*Channel, error)

	// Subscription operations
	Subscribe(ctx context.Context, channel string, opts ...SubscribeOption) (*Subscription, error)
	Unsubscribe(ctx context.Context, subscriptionID string) error

	// Publishing
	Publish(ctx context.Context, channel string, payload interface{}, opts ...PublishOption) (*PublishResult, error)
	PublishDelta(ctx context.Context, channel string, old, new interface{}, opts ...PublishOption) (*PublishResult, error)

	// Watch manager integration
	WatchChannel(ctx context.Context, keyPrefix string, opts ...SubscribeOption) (*Subscription, error)

	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	IsRunning() bool

	// Stats
	GetStats() *Stats
}

// subscriptionEntry tracks a subscription in the service.
type subscriptionEntry struct {
	subscription *Subscription
	client       *Client
	options      *subscribeOptions
}

// PushService implements the Service interface.
type PushService struct {
	logger        *logger.Logger
	hub           *NotificationHub
	storage       storage.Storage // For persistence and dedup
	codec         *CodecResolver  // Codec resolution
	deltaComputer *DeltaComputer  // For delta computation
	dedup         *DedupStore     // Deduplication
	running       atomic.Bool
	subscriptions sync.Map // subscriptionID -> *subscriptionEntry
	channels      sync.Map // channelName -> *Channel (channel metadata cache)
	idSeq         atomic.Uint64
	config        *serviceOptions

	// Context for lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
}

// channelNamePattern validates channel names.
// Allowed: alphanumeric, underscore, hyphen, period, slash, colon (for hierarchical naming).
var channelNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_\-./:\*]+$`)

// NewPushService creates a new PushService with the given logger, storage, and options.
func NewPushService(log *logger.Logger, stor storage.Storage, opts ...ServiceOption) *PushService {
	config := ApplyServiceOptions(opts...)

	hub := NewNotificationHubWithBufferSize(log, config.bufferSize)

	var codec *CodecResolver
	if log != nil {
		codec = NewCodecResolver(log, WithResolverDefaultCodec(config.defaultCodec))
	}

	var deltaComputer *DeltaComputer
	if log != nil {
		deltaComputer = NewDeltaComputer(log)
	}

	var dedup *DedupStore
	if config.dedupEnabled && stor != nil && log != nil {
		dedup = NewDedupStore(stor, log, WithDedupStoreWindow(config.dedupWindow))
	}

	return &PushService{
		logger:        log.WithField("component", "push-service"),
		hub:           hub,
		storage:       stor,
		codec:         codec,
		deltaComputer: deltaComputer,
		dedup:         dedup,
		config:        config,
	}
}

// generateID generates a unique subscription ID.
func (ps *PushService) generateID() string {
	seq := ps.idSeq.Add(1)
	return generateSubscriptionID(seq)
}

// generateSubscriptionID creates a subscription ID from a sequence number.
func generateSubscriptionID(seq uint64) string {
	return "sub-" + formatUint64(seq)
}

// formatUint64 formats a uint64 as a string without importing strconv.
func formatUint64(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte(n%10) + '0'
		n /= 10
	}
	return string(buf[i:])
}

// validateChannelName validates a channel name.
func validateChannelName(name string) error {
	if name == "" {
		return NewInvalidChannelNameError("push.validateChannelName", name, "channel name cannot be empty")
	}
	if len(name) > 256 {
		return NewInvalidChannelNameError("push.validateChannelName", name, "channel name exceeds maximum length of 256 characters")
	}
	if !channelNamePattern.MatchString(name) {
		return NewInvalidChannelNameError("push.validateChannelName", name, "channel name contains invalid characters")
	}
	return nil
}

// Start starts the push service.
func (ps *PushService) Start(ctx context.Context) error {
	if ps.running.Load() {
		return nil // Already running
	}

	ps.ctx, ps.cancel = context.WithCancel(ctx) // #nosec G118 -- cancel stored in PushService, called in Stop()

	// Start the notification hub in a goroutine
	go ps.hub.Run(ps.ctx)

	// Wait for hub to start
	for i := 0; i < 100; i++ {
		if ps.hub.IsRunning() {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
			// Continue waiting
		}
	}

	if !ps.hub.IsRunning() {
		return NewServiceNotRunningError("push.Start", "hub failed to start")
	}

	ps.running.Store(true)
	ps.logger.Info("push service started")

	return nil
}

// Stop stops the push service.
func (ps *PushService) Stop(ctx context.Context) error {
	if !ps.running.Load() {
		return nil // Already stopped
	}

	// Cancel the service context
	if ps.cancel != nil {
		ps.cancel()
	}

	// Stop the notification hub
	ps.hub.Stop()

	// Wait for hub to stop with timeout
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(5 * time.Second)
	}

	for ps.hub.IsRunning() {
		if time.Now().After(deadline) {
			ps.logger.Warn("timeout waiting for hub to stop")
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
			// Continue waiting
		}
	}

	// Clean up subscriptions
	ps.subscriptions.Range(func(key, value interface{}) bool {
		entry, ok := value.(*subscriptionEntry)
		if ok && entry.client != nil {
			entry.client.Close()
		}
		ps.subscriptions.Delete(key)
		return true
	})

	ps.running.Store(false)
	ps.logger.Info("push service stopped")

	return nil
}

// IsRunning returns true if the push service is running.
func (ps *PushService) IsRunning() bool {
	return ps.running.Load()
}

// CreateChannel creates a new notification channel.
func (ps *PushService) CreateChannel(ctx context.Context, name string, opts ...CreateChannelOption) (*Channel, error) {
	if !ps.running.Load() {
		return nil, NewServiceNotRunningError("push.CreateChannel", "create channel")
	}

	if err := validateChannelName(name); err != nil {
		return nil, err
	}

	options := ApplyCreateChannelOptions(opts...)

	// Determine codec
	codec := options.codec
	if codec == "" {
		codec = ps.config.defaultCodec
	}

	// Create hub channel with options
	hubChannel, err := ps.hub.CreateChannel(name,
		WithBufferSize(options.bufferSize),
		WithMaxSubscribers(options.maxSubs),
		WithPersistent(options.persistent),
		WithTTL(options.ttl),
	)
	if err != nil {
		return nil, err
	}

	// Create and cache channel metadata
	channel := &Channel{
		Name:        hubChannel.Name,
		Codec:       codec,
		CreatedAt:   hubChannel.CreatedAt,
		Subscribers: int64(hubChannel.SubscriberCount()),
		Persistent:  options.persistent,
		MaxHistory:  options.maxHistory,
		BufferSize:  options.bufferSize,
		TTL:         options.ttl,
	}

	ps.channels.Store(name, channel)

	ps.logger.WithFields(map[string]interface{}{
		"channel":    name,
		"persistent": options.persistent,
		"codec":      codec,
	}).Debug("channel created")

	return channel, nil
}

// DeleteChannel deletes a notification channel.
func (ps *PushService) DeleteChannel(ctx context.Context, name string) error {
	if !ps.running.Load() {
		return NewServiceNotRunningError("push.DeleteChannel", "delete channel")
	}

	if err := validateChannelName(name); err != nil {
		return err
	}

	// Delete from hub
	if err := ps.hub.DeleteChannel(name); err != nil {
		return err
	}

	// Remove from cache
	ps.channels.Delete(name)

	// Remove all subscriptions for this channel
	ps.subscriptions.Range(func(key, value interface{}) bool {
		entry, ok := value.(*subscriptionEntry)
		if ok && entry.subscription.Channel == name {
			if entry.client != nil {
				entry.client.Close()
			}
			ps.subscriptions.Delete(key)
		}
		return true
	})

	ps.logger.WithField("channel", name).Debug("channel deleted")

	return nil
}

// GetChannel returns channel information by name.
func (ps *PushService) GetChannel(ctx context.Context, name string) (*Channel, error) {
	if !ps.running.Load() {
		return nil, NewServiceNotRunningError("push.GetChannel", "get channel")
	}

	if err := validateChannelName(name); err != nil {
		return nil, err
	}

	// Check cache first.
	if cached, ok := ps.channels.Load(name); ok {
		channel, _ := cached.(*Channel)
		// Update dynamic counts from hub.
		hubChannel, found := ps.hub.GetChannel(name)
		if found {
			channel.Subscribers = int64(hubChannel.SubscriberCount())
			channel.MessagesPublished = hubChannel.MessagesPublished()
		}
		return channel, nil
	}

	// Check hub
	hubChannel, found := ps.hub.GetChannel(name)
	if !found {
		return nil, NewChannelNotFoundError("push.GetChannel", name)
	}

	// Build channel from hub data
	channel := &Channel{
		Name:              hubChannel.Name,
		Codec:             ps.config.defaultCodec,
		CreatedAt:         hubChannel.CreatedAt,
		Subscribers:       int64(hubChannel.SubscriberCount()),
		MessagesPublished: hubChannel.MessagesPublished(),
		Persistent:        hubChannel.Options.Persistent,
		BufferSize:        hubChannel.Options.BufferSize,
		TTL:               hubChannel.Options.TTL,
	}

	// Cache it
	ps.channels.Store(name, channel)

	return channel, nil
}

// ListChannels returns all notification channels.
func (ps *PushService) ListChannels(ctx context.Context) ([]*Channel, error) {
	if !ps.running.Load() {
		return nil, NewServiceNotRunningError("push.ListChannels", "list channels")
	}

	hubChannels := ps.hub.ListChannels()
	channels := make([]*Channel, 0, len(hubChannels))

	for _, hubChannel := range hubChannels {
		// Check cache for additional metadata.
		var codec string
		var maxHistory int
		if cached, ok := ps.channels.Load(hubChannel.Name); ok {
			cachedChannel, _ := cached.(*Channel)
			codec = cachedChannel.Codec
			maxHistory = cachedChannel.MaxHistory
		} else {
			codec = ps.config.defaultCodec
		}

		channel := &Channel{
			Name:        hubChannel.Name,
			Codec:       codec,
			CreatedAt:   hubChannel.CreatedAt,
			Subscribers: int64(hubChannel.SubscriberCount()),
			Persistent:  hubChannel.Options.Persistent,
			MaxHistory:  maxHistory,
			BufferSize:  hubChannel.Options.BufferSize,
			TTL:         hubChannel.Options.TTL,
		}
		channels = append(channels, channel)
	}

	return channels, nil
}

// Subscribe subscribes to a notification channel.
func (ps *PushService) Subscribe(ctx context.Context, channel string, opts ...SubscribeOption) (*Subscription, error) {
	if !ps.running.Load() {
		return nil, NewServiceNotRunningError("push.Subscribe", "subscribe")
	}

	if err := validateChannelName(channel); err != nil {
		return nil, err
	}

	options := ApplySubscribeOptions(opts...)

	// Generate subscription ID
	subscriptionID := ps.generateID()

	// Create a client for this subscription
	clientID := subscriptionID
	if options.sessionID != "" {
		clientID = options.sessionID
	}
	client := NewClient(clientID, options.bufferSize)

	// Register client with hub
	ps.hub.RegisterClient(client)

	// Subscribe client to channel
	if err := ps.hub.SubscribeToChannel(client, channel); err != nil {
		ps.hub.UnregisterClient(client)
		return nil, err
	}

	// Create subscription
	subscription := &Subscription{
		ID:        subscriptionID,
		Channel:   channel,
		ClientID:  clientID,
		CreatedAt: time.Now(),
		Filters:   options.filter,
	}

	// Store subscription entry
	entry := &subscriptionEntry{
		subscription: subscription,
		client:       client,
		options:      options,
	}
	ps.subscriptions.Store(subscriptionID, entry)

	ps.logger.WithFields(map[string]interface{}{
		"subscription_id": subscriptionID,
		"channel":         channel,
		"client_id":       clientID,
	}).Debug("subscription created")

	return subscription, nil
}

// Unsubscribe removes a subscription.
func (ps *PushService) Unsubscribe(ctx context.Context, subscriptionID string) error {
	if !ps.running.Load() {
		return NewServiceNotRunningError("push.Unsubscribe", "unsubscribe")
	}

	if subscriptionID == "" {
		return NewSubscriptionNotFoundError("push.Unsubscribe", subscriptionID)
	}

	// Load and delete subscription.
	value, ok := ps.subscriptions.LoadAndDelete(subscriptionID)
	if !ok {
		return NewSubscriptionNotFoundError("push.Unsubscribe", subscriptionID)
	}

	entry, _ := value.(*subscriptionEntry)

	// Unsubscribe from channel
	if err := ps.hub.UnsubscribeFromChannel(entry.client, entry.subscription.Channel); err != nil {
		ps.logger.WithError(err).WithField("subscription_id", subscriptionID).Warn("error unsubscribing from channel")
	}

	// Unregister client
	ps.hub.UnregisterClient(entry.client)
	entry.client.Close()

	ps.logger.WithFields(map[string]interface{}{
		"subscription_id": subscriptionID,
		"channel":         entry.subscription.Channel,
	}).Debug("subscription removed")

	return nil
}

// Publish publishes a message to a channel.
func (ps *PushService) Publish(ctx context.Context, channel string, payload interface{}, opts ...PublishOption) (*PublishResult, error) {
	if !ps.running.Load() {
		return nil, NewServiceNotRunningError("push.Publish", "publish")
	}

	if err := validateChannelName(channel); err != nil {
		return nil, err
	}

	// Verify the channel exists before publishing
	_, found := ps.hub.GetChannel(channel)
	if !found {
		if _, cached := ps.channels.Load(channel); !cached {
			return nil, NewChannelNotFoundError("push.Publish", channel)
		}
	}

	options := ApplyPublishOptions(opts...)

	// Serialize payload
	var data []byte
	var serializeErr error
	switch p := payload.(type) {
	case []byte:
		data = p
	case string:
		data = []byte(p)
	default:
		data, serializeErr = json.Marshal(payload)
		if serializeErr != nil {
			return nil, NewInvalidMessageIDError("push.Publish", options.messageID, "failed to serialize payload")
		}
	}

	// Generate message ID if not provided
	messageID := options.messageID
	if messageID == "" {
		messageID = ps.generateID()
	}

	// Create message
	msg := &Message{
		ID:        messageID,
		Type:      MessageTypeNotification,
		Channel:   channel,
		Payload:   data,
		Timestamp: time.Now(),
		Metadata:  options.metadata,
	}

	// Check for duplicates if dedup is enabled
	if ps.dedup != nil && options.dedupKey != "" {
		dedupInfo, err := ps.dedup.CheckAndStore(ctx, channel, msg, options.dedupKey)
		if err != nil {
			ps.logger.WithError(err).WithField("channel", channel).Warn("dedup check failed")
			// Continue with publish on dedup error
		} else if dedupInfo != nil && dedupInfo.IsDuplicate {
			ps.logger.WithFields(map[string]interface{}{
				"channel":    channel,
				"message_id": messageID,
				"dedup_key":  options.dedupKey,
				"count":      dedupInfo.Count,
			}).Debug("duplicate message detected, skipping publish")
			return &PublishResult{MessageID: messageID, IsDuplicate: true}, nil
		}
	}

	// Broadcast via hub
	if err := ps.hub.Broadcast(msg); err != nil {
		return nil, err
	}

	ps.logger.WithFields(map[string]interface{}{
		"channel":    channel,
		"message_id": messageID,
	}).Debug("message published")

	return &PublishResult{MessageID: messageID, IsDuplicate: false}, nil
}

// PublishDelta publishes a delta message to a channel.
func (ps *PushService) PublishDelta(ctx context.Context, channel string, old, new interface{}, opts ...PublishOption) (*PublishResult, error) {
	if !ps.running.Load() {
		return nil, NewServiceNotRunningError("push.PublishDelta", "publish delta")
	}

	if err := validateChannelName(channel); err != nil {
		return nil, err
	}

	if ps.deltaComputer == nil {
		// Fall back to full publish if no delta computer
		return ps.Publish(ctx, channel, new, opts...)
	}

	// Compute delta using the Compute method
	delta, err := ps.deltaComputer.Compute(old, new)
	if err != nil {
		// Fall back to full publish on delta computation error
		ps.logger.WithError(err).Debug("delta computation failed, falling back to full publish")
		return ps.Publish(ctx, channel, new, opts...)
	}

	// Publish delta
	return ps.Publish(ctx, channel, delta, opts...)
}

// WatchChannel creates a subscription that watches for changes to keys with the given prefix.
// This integrates the push service with the watch manager for key change notifications.
func (ps *PushService) WatchChannel(ctx context.Context, keyPrefix string, opts ...SubscribeOption) (*Subscription, error) {
	if !ps.running.Load() {
		return nil, NewServiceNotRunningError("push.WatchChannel", "watch channel")
	}

	// Create a channel name based on the key prefix
	channelName := "watch:" + keyPrefix

	// Ensure the channel exists
	_, err := ps.GetChannel(ctx, channelName)
	if err != nil {
		if !IsChannelNotFound(err) {
			return nil, err
		}
		// Create the watch channel
		_, err = ps.CreateChannel(ctx, channelName, WithPersistence(false))
		if err != nil && !IsChannelExists(err) {
			return nil, err
		}
	}

	// Merge filter with key prefix
	mergedOpts := make([]SubscribeOption, 0, len(opts)+1)
	mergedOpts = append(mergedOpts, opts...)

	// Add key prefix filter if not already specified
	hasFilter := false
	for _, opt := range opts {
		testOpts := &subscribeOptions{}
		opt(testOpts)
		if testOpts.filter != nil && testOpts.filter.KeyPrefix != "" {
			hasFilter = true
			break
		}
	}
	if !hasFilter {
		mergedOpts = append(mergedOpts, WithFilter(&SubscriptionFilter{
			KeyPrefix: keyPrefix,
		}))
	}

	// Subscribe to the watch channel
	return ps.Subscribe(ctx, channelName, mergedOpts...)
}

// GetStats returns statistics about the push service.
func (ps *PushService) GetStats() *Stats {
	stats := &Stats{
		TotalChannels:      int64(ps.hub.ChannelCount()),
		TotalSubscriptions: 0,
	}

	// Count subscriptions
	ps.subscriptions.Range(func(key, value interface{}) bool {
		stats.TotalSubscriptions++
		return true
	})

	return stats
}

// GetSubscription returns a subscription by ID.
func (ps *PushService) GetSubscription(subscriptionID string) (*Subscription, bool) {
	value, ok := ps.subscriptions.Load(subscriptionID)
	if !ok {
		return nil, false
	}
	entry, _ := value.(*subscriptionEntry)
	return entry.subscription, true
}

// GetSubscriptionClient returns the client for a subscription.
// This is useful for receiving messages from the subscription.
func (ps *PushService) GetSubscriptionClient(subscriptionID string) (*Client, bool) {
	value, ok := ps.subscriptions.Load(subscriptionID)
	if !ok {
		return nil, false
	}
	entry, _ := value.(*subscriptionEntry)
	return entry.client, true
}

// GetHub returns the underlying NotificationHub.
// This is useful for advanced use cases that need direct hub access.
func (ps *PushService) GetHub() *NotificationHub {
	return ps.hub
}

// GetCodecResolver returns the codec resolver.
func (ps *PushService) GetCodecResolver() *CodecResolver {
	return ps.codec
}

// GetDedupStore returns the deduplication store.
// Returns nil if deduplication is not enabled.
func (ps *PushService) GetDedupStore() *DedupStore {
	return ps.dedup
}

// GetDeltaComputer returns the delta computer.
func (ps *PushService) GetDeltaComputer() *DeltaComputer {
	return ps.deltaComputer
}
