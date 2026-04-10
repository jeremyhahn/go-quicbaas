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
	"time"
)

// Default values for service options.
const (
	// DefaultServiceBufferSize is the default buffer size for the service.
	DefaultServiceBufferSize = 1024

	// DefaultMaxSubscriptions is the default maximum number of subscriptions per client.
	DefaultMaxSubscriptions = 100

	// DefaultDedupWindow is the default deduplication time window.
	DefaultDedupWindow = 5 * time.Minute

	// DefaultCodec is the default codec for message serialization.
	DefaultCodec = CodecJSON

	// DefaultChannelBufferSize is the default buffer size for channels.
	DefaultChannelBufferSize = 256

	// DefaultMaxHistory is the default number of messages to retain for history.
	DefaultMaxHistory = 100
)

// serviceOptions holds configuration for the PushService.
type serviceOptions struct {
	bufferSize       int
	maxSubscriptions int
	dedupEnabled     bool
	dedupWindow      time.Duration
	defaultCodec     string
}

// defaultServiceOptions returns default service options.
func defaultServiceOptions() *serviceOptions {
	return &serviceOptions{
		bufferSize:       DefaultServiceBufferSize,
		maxSubscriptions: DefaultMaxSubscriptions,
		dedupEnabled:     false,
		dedupWindow:      DefaultDedupWindow,
		defaultCodec:     DefaultCodec,
	}
}

// ServiceOption configures the PushService.
type ServiceOption func(*serviceOptions)

// WithServiceBufferSize sets the buffer size for the service.
// This affects the internal channel buffer for message distribution.
func WithServiceBufferSize(size int) ServiceOption {
	return func(o *serviceOptions) {
		if size > 0 {
			o.bufferSize = size
		}
	}
}

// WithMaxSubscriptions sets the maximum number of subscriptions per client.
// A value of 0 means unlimited.
func WithMaxSubscriptions(max int) ServiceOption {
	return func(o *serviceOptions) {
		if max >= 0 {
			o.maxSubscriptions = max
		}
	}
}

// WithDedup enables or disables message deduplication.
// When enabled, duplicate messages within the dedup window are filtered.
func WithDedup(enabled bool) ServiceOption {
	return func(o *serviceOptions) {
		o.dedupEnabled = enabled
	}
}

// WithDedupWindow sets the time window for deduplication.
// Messages with the same dedup key within this window are considered duplicates.
func WithDedupWindow(window time.Duration) ServiceOption {
	return func(o *serviceOptions) {
		if window > 0 {
			o.dedupWindow = window
		}
	}
}

// WithDefaultCodec sets the default codec for message serialization.
// Valid values: "json", "msgpack", "protobuf".
func WithDefaultCodec(codec string) ServiceOption {
	return func(o *serviceOptions) {
		if codec != "" {
			o.defaultCodec = codec
		}
	}
}

// createChannelOptions holds configuration for channel creation.
type createChannelOptions struct {
	codec      string
	persistent bool
	maxHistory int
	bufferSize int
	maxSubs    int
	ttl        time.Duration
}

// defaultCreateChannelOptions returns default channel creation options.
func defaultCreateChannelOptions() *createChannelOptions {
	return &createChannelOptions{
		codec:      "",
		persistent: false,
		maxHistory: 0,
		bufferSize: DefaultChannelBufferSize,
		maxSubs:    0,
		ttl:        0,
	}
}

// CreateChannelOption configures channel creation.
type CreateChannelOption func(*createChannelOptions)

// WithCodec sets the default codec for the channel.
// If not set, the service default codec is used.
func WithCodec(codec string) CreateChannelOption {
	return func(o *createChannelOptions) {
		if codec != "" {
			o.codec = codec
		}
	}
}

// WithPersistence enables message persistence for the channel.
// When enabled, messages are stored for replay to new subscribers.
func WithPersistence(enabled bool) CreateChannelOption {
	return func(o *createChannelOptions) {
		o.persistent = enabled
	}
}

// WithMaxHistory sets the maximum number of messages to retain for history.
// Only effective when persistence is enabled.
func WithMaxHistory(max int) CreateChannelOption {
	return func(o *createChannelOptions) {
		if max >= 0 {
			o.maxHistory = max
		}
	}
}

// WithChannelBufferSize sets the buffer size for the channel.
func WithChannelBufferSize(size int) CreateChannelOption {
	return func(o *createChannelOptions) {
		if size > 0 {
			o.bufferSize = size
		}
	}
}

// WithChannelMaxSubscribers sets the maximum number of subscribers for the channel.
// A value of 0 means unlimited.
func WithChannelMaxSubscribers(max int) CreateChannelOption {
	return func(o *createChannelOptions) {
		if max >= 0 {
			o.maxSubs = max
		}
	}
}

// WithChannelTTL sets the message TTL for the channel.
// Messages older than this duration are automatically removed.
func WithChannelTTL(ttl time.Duration) CreateChannelOption {
	return func(o *createChannelOptions) {
		if ttl >= 0 {
			o.ttl = ttl
		}
	}
}

// subscribeOptions holds configuration for subscriptions.
type subscribeOptions struct {
	filter              *SubscriptionFilter
	sessionID           string
	deltaMode           bool
	includeOldValues    bool
	dedupMetadata       bool
	dedupSkipDuplicates bool
	bufferSize          int
}

// defaultSubscribeOptions returns default subscription options.
func defaultSubscribeOptions() *subscribeOptions {
	return &subscribeOptions{
		filter:              nil,
		sessionID:           "",
		deltaMode:           false,
		includeOldValues:    false,
		dedupMetadata:       false,
		dedupSkipDuplicates: false,
		bufferSize:          DefaultChannelBufferSize,
	}
}

// SubscribeOption configures subscriptions.
type SubscribeOption func(*subscribeOptions)

// WithFilter sets the subscription filter for event filtering.
// The filter controls which events are delivered to the subscriber.
func WithFilter(filter *SubscriptionFilter) SubscribeOption {
	return func(o *subscribeOptions) {
		o.filter = filter
	}
}

// WithSession sets a session ID for the subscription.
// This can be used for subscription tracking and resumption.
func WithSession(sessionID string) SubscribeOption {
	return func(o *subscribeOptions) {
		if sessionID != "" {
			o.sessionID = sessionID
		}
	}
}

// WithDeltaMode enables delta mode for the subscription.
// In delta mode, only changes to entities are transmitted.
func WithDeltaMode(enabled bool) SubscribeOption {
	return func(o *subscribeOptions) {
		o.deltaMode = enabled
	}
}

// WithIncludeOldValues enables inclusion of old values in delta payloads.
// This is only effective when delta mode is enabled.
func WithIncludeOldValues(enabled bool) SubscribeOption {
	return func(o *subscribeOptions) {
		o.includeOldValues = enabled
	}
}

// WithDedupMetadata enables deduplication metadata in messages.
// When enabled, messages include information about whether they were duplicates.
func WithDedupMetadata(enabled bool) SubscribeOption {
	return func(o *subscribeOptions) {
		o.dedupMetadata = enabled
	}
}

// WithDedupSkipDuplicates enables automatic skipping of duplicate messages.
// When enabled, duplicate messages are not delivered to the subscriber.
func WithDedupSkipDuplicates(enabled bool) SubscribeOption {
	return func(o *subscribeOptions) {
		o.dedupSkipDuplicates = enabled
	}
}

// WithSubscriptionBufferSize sets the buffer size for the subscription.
func WithSubscriptionBufferSize(size int) SubscribeOption {
	return func(o *subscribeOptions) {
		if size > 0 {
			o.bufferSize = size
		}
	}
}

// publishOptions holds configuration for publishing.
type publishOptions struct {
	messageID string
	dedupKey  string
	codec     string
	metadata  map[string]string
}

// defaultPublishOptions returns default publish options.
func defaultPublishOptions() *publishOptions {
	return &publishOptions{
		messageID: "",
		dedupKey:  "",
		codec:     "",
		metadata:  nil,
	}
}

// PublishOption configures publishing.
type PublishOption func(*publishOptions)

// WithMessageID sets a custom message ID.
// If not set, a UUID will be generated.
func WithMessageID(id string) PublishOption {
	return func(o *publishOptions) {
		if id != "" {
			o.messageID = id
		}
	}
}

// WithDedupKey sets the deduplication key for the message.
// Messages with the same dedup key within the dedup window are considered duplicates.
func WithDedupKey(key string) PublishOption {
	return func(o *publishOptions) {
		if key != "" {
			o.dedupKey = key
		}
	}
}

// WithPublishCodec sets the codec for the published message.
// If not set, the channel default codec is used.
func WithPublishCodec(codec string) PublishOption {
	return func(o *publishOptions) {
		if codec != "" {
			o.codec = codec
		}
	}
}

// WithMetadata sets metadata key-value pairs for the message.
func WithMetadata(metadata map[string]string) PublishOption {
	return func(o *publishOptions) {
		o.metadata = metadata
	}
}

// WithMetadataKV adds a single metadata key-value pair to the message.
func WithMetadataKV(key, value string) PublishOption {
	return func(o *publishOptions) {
		if o.metadata == nil {
			o.metadata = make(map[string]string)
		}
		o.metadata[key] = value
	}
}

// ApplyServiceOptions applies service options and returns the configured options.
func ApplyServiceOptions(opts ...ServiceOption) *serviceOptions {
	options := defaultServiceOptions()
	for _, opt := range opts {
		opt(options)
	}
	return options
}

// ApplyCreateChannelOptions applies channel creation options and returns the configured options.
func ApplyCreateChannelOptions(opts ...CreateChannelOption) *createChannelOptions {
	options := defaultCreateChannelOptions()
	for _, opt := range opts {
		opt(options)
	}
	return options
}

// ApplySubscribeOptions applies subscription options and returns the configured options.
func ApplySubscribeOptions(opts ...SubscribeOption) *subscribeOptions {
	options := defaultSubscribeOptions()
	for _, opt := range opts {
		opt(options)
	}
	return options
}

// ApplyPublishOptions applies publish options and returns the configured options.
func ApplyPublishOptions(opts ...PublishOption) *publishOptions {
	options := defaultPublishOptions()
	for _, opt := range opts {
		opt(options)
	}
	return options
}
