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

// Supported codec identifiers for message serialization.
const (
	CodecJSON     = "json"
	CodecMsgpack  = "msgpack"
	CodecProtobuf = "protobuf"
)

// EventType represents the type of event for filtering subscriptions.
type EventType int

const (
	// EventTypePut indicates a key was created or updated.
	EventTypePut EventType = iota
	// EventTypeDelete indicates a key was deleted.
	EventTypeDelete
)

// String returns the string representation of the event type.
func (et EventType) String() string {
	switch et {
	case EventTypePut:
		return "PUT"
	case EventTypeDelete:
		return "DELETE"
	default:
		return "UNKNOWN"
	}
}

// MessageType defines the type of push notification message.
type MessageType string

// Message type constants.
const (
	// MessageTypeSubscribe requests subscription to a channel.
	MessageTypeSubscribe MessageType = "subscribe"

	// MessageTypeUnsubscribe requests unsubscription from a channel.
	MessageTypeUnsubscribe MessageType = "unsubscribe"

	// MessageTypePublish publishes a message to a channel.
	MessageTypePublish MessageType = "publish"

	// MessageTypeNotification is a push notification from server to client.
	MessageTypeNotification MessageType = "notification"

	// MessageTypePing is a keepalive ping message.
	MessageTypePing MessageType = "ping"

	// MessageTypePong is a keepalive pong response.
	MessageTypePong MessageType = "pong"

	// MessageTypeAck acknowledges receipt of a message.
	MessageTypeAck MessageType = "ack"

	// MessageTypeError indicates an error occurred.
	MessageTypeError MessageType = "error"

	// MessageTypeData represents a data message for internal hub routing.
	MessageTypeData MessageType = "data"

	// MessageTypeControl represents a control message.
	MessageTypeControl MessageType = "control"

	// MessageTypeHeartbeat represents a heartbeat message.
	MessageTypeHeartbeat MessageType = "heartbeat"
)

// OpType represents the type of operation in a delta payload.
type OpType string

const (
	// OpSet sets a field to a new value.
	OpSet OpType = "set"
	// OpDelete removes a field.
	OpDelete OpType = "delete"
	// OpAppend appends to an array or string field.
	OpAppend OpType = "append"
	// OpIncrement increments a numeric field.
	OpIncrement OpType = "increment"
)

// String returns the string representation of the operation type.
func (ot OpType) String() string {
	return string(ot)
}

// Channel represents a notification channel.
type Channel struct {
	// Name is the unique channel identifier.
	Name string

	// Codec is the default codec for messages on this channel.
	Codec string

	// CreatedAt is when the channel was created.
	CreatedAt time.Time

	// Subscribers is the current count of active subscribers.
	Subscribers int64

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

// Subscription represents a client's subscription to a channel.
type Subscription struct {
	// ID is the unique subscription identifier.
	ID string

	// Channel is the name of the subscribed channel.
	Channel string

	// ClientID identifies the subscribing client.
	ClientID string

	// CreatedAt is when the subscription was created.
	CreatedAt time.Time

	// Filters contains optional event filtering configuration.
	Filters *SubscriptionFilter
}

// SubscriptionFilter defines filtering criteria for subscriptions.
type SubscriptionFilter struct {
	// EventTypes restricts events to the specified types.
	// An empty slice means all event types are accepted.
	EventTypes []EventType

	// KeyPrefix restricts events to keys matching the specified prefix.
	// This is primarily used for watch channels.
	// An empty string means all keys are accepted.
	KeyPrefix string
}

// Message represents a push notification message.
type Message struct {
	// ID is a unique identifier for the message.
	ID string

	// Type indicates the message type.
	Type MessageType

	// Channel is the target channel for the message.
	Channel string

	// Payload contains the message data.
	Payload []byte

	// Timestamp is when the message was created.
	Timestamp time.Time

	// Source is the originator of the message.
	Source string

	// Metadata contains optional key-value metadata.
	Metadata map[string]string

	// Dedup contains deduplication metadata for the message.
	Dedup *DedupInfo
}

// DedupInfo contains deduplication metadata for a message.
type DedupInfo struct {
	// IsDuplicate indicates whether this message was a duplicate.
	IsDuplicate bool

	// Count is the total number of times this message has been received.
	Count int64

	// FirstSeen is when this message was first received.
	FirstSeen time.Time
}

// DeltaPayload represents changes to an entity for efficient delta updates.
type DeltaPayload struct {
	// EntityID is the unique identifier of the entity being modified.
	EntityID string `json:"entity_id,omitempty"`

	// EntityType describes the type of entity.
	EntityType string `json:"entity_type,omitempty"`

	// Version is the new version number after changes.
	Version uint64 `json:"version"`

	// PrevVersion is the version number before changes.
	PrevVersion uint64 `json:"prev_version"`

	// Timestamp is when the changes were made.
	Timestamp time.Time `json:"timestamp"`

	// Changes is the list of field modifications.
	Changes []FieldChange `json:"changes"`
}

// FieldChange represents a single field modification in a delta payload.
type FieldChange struct {
	// Path is the JSON path to the modified field (e.g., "user.address.city").
	Path string `json:"path"`

	// Operation is the type of modification.
	Operation OpType `json:"op"`

	// Value is the new value for the field.
	// This is nil for delete operations.
	Value interface{} `json:"value,omitempty"`

	// OldValue is the previous value of the field.
	// This is only populated when include_old_values is enabled.
	OldValue interface{} `json:"old_value,omitempty"`
}

// Stats contains statistics about the push service.
type Stats struct {
	// TotalChannels is the number of active channels.
	TotalChannels int64

	// TotalSubscriptions is the total number of active subscriptions.
	TotalSubscriptions int64

	// MessagesPublished is the total number of messages published.
	MessagesPublished int64

	// MessagesDelivered is the total number of messages delivered to subscribers.
	MessagesDelivered int64

	// DedupHits is the number of duplicate messages that were deduplicated.
	DedupHits int64

	// DroppedMessages is the number of messages dropped due to slow consumers.
	DroppedMessages int64

	// BufferUtilization is the current buffer usage as a percentage (0.0 to 1.0).
	BufferUtilization float64
}

// DedupStats contains deduplication statistics.
type DedupStats struct {
	// TotalChecks is the total number of deduplication checks performed.
	TotalChecks int64

	// DuplicatesDetected is the number of duplicate messages detected.
	DuplicatesDetected int64

	// UniqueMessages is the number of unique messages processed.
	UniqueMessages int64

	// CacheSize is the current size of the deduplication cache.
	CacheSize int64

	// WindowDuration is the deduplication time window.
	WindowDuration time.Duration
}

// ChannelStats contains statistics for a specific channel.
type ChannelStats struct {
	// Name is the channel name.
	Name string

	// Subscribers is the current subscriber count.
	Subscribers int64

	// MessagesPublished is the number of messages published to this channel.
	MessagesPublished int64

	// MessagesInHistory is the number of messages currently in history.
	MessagesInHistory int
}

// CreateChannelRequest is a request to create a channel.
type CreateChannelRequest struct {
	Name       string
	Codec      string
	Persistent bool
	MaxHistory int
	BufferSize int
	MaxSubs    int
	TTL        time.Duration
}

// DeleteChannelRequest is a request to delete a channel.
type DeleteChannelRequest struct {
	Name string
}

// GetChannelRequest is a request to get a channel.
type GetChannelRequest struct {
	Name string
}

// ListChannelsRequest is a request to list channels.
type ListChannelsRequest struct{}

// ListChannelsResponse is the response from listing channels.
type ListChannelsResponse struct {
	Channels []*Channel
}

// SubscribeRequest is a request to subscribe to a channel.
type SubscribeRequest struct {
	Channel             string
	SessionID           string
	DeltaMode           bool
	IncludeOldValues    bool
	DedupMetadata       bool
	DedupSkipDuplicates bool
	BufferSize          int
	Filter              *SubscriptionFilter
}

// UnsubscribeRequest is a request to unsubscribe.
type UnsubscribeRequest struct {
	SubscriptionID string
}

// PublishRequest is a request to publish a message.
type PublishRequest struct {
	Channel   string
	Payload   []byte
	MessageID string
	DedupKey  string
	Codec     string
	Metadata  map[string]string
}

// PublishDeltaRequest is a request to publish a delta.
type PublishDeltaRequest struct {
	Channel   string
	Old       interface{}
	New       interface{}
	MessageID string
	DedupKey  string
	Codec     string
	Metadata  map[string]string
}

// GetDedupStatsRequest is a request for a channel's deduplication statistics.
type GetDedupStatsRequest struct {
	Channel string
}

// WatchChannelRequest is a request to watch a channel.
type WatchChannelRequest struct {
	KeyPrefix           string
	SessionID           string
	DeltaMode           bool
	IncludeOldValues    bool
	DedupMetadata       bool
	DedupSkipDuplicates bool
	BufferSize          int
	Filter              *SubscriptionFilter
}

// Internal request/response types for transport layer.

type deleteChannelResponse struct{}

type unsubscribeResponse struct{}

type publishResponse struct{}

type publishDeltaResponse struct{}

type getStatsRequest struct{}

// GetChannelStatsRequest is a request for a channel's statistics.
type GetChannelStatsRequest struct {
	Channel string
}
