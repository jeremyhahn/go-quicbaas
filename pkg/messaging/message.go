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
	"encoding/json"
	"time"
)

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

// Message represents a push notification message exchanged between
// clients and the notification hub.
type Message struct {
	// ID is a unique identifier for the message
	ID string `json:"id,omitempty"`

	// Type indicates the message type
	Type MessageType `json:"type"`

	// Channel is the target channel for the message
	Channel string `json:"channel,omitempty"`

	// Payload contains the message data
	Payload []byte `json:"payload,omitempty"`

	// Timestamp is when the message was created
	Timestamp time.Time `json:"timestamp,omitempty"`

	// Source is the originator of the message
	Source string `json:"source,omitempty"`

	// Metadata contains optional key-value metadata
	Metadata map[string]string `json:"metadata,omitempty"`

	// Dedup contains deduplication metadata for the message.
	// This is populated when deduplication is enabled and provides
	// information about whether the message was a duplicate.
	Dedup *DedupInfo `json:"dedup,omitempty"`
}

// Marshal serializes the message to JSON bytes.
func (m *Message) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

// Unmarshal deserializes JSON bytes into the message.
func (m *Message) Unmarshal(data []byte) error {
	return json.Unmarshal(data, m)
}

// Clone creates a deep copy of the message.
func (m *Message) Clone() *Message {
	clone := &Message{
		ID:        m.ID,
		Type:      m.Type,
		Channel:   m.Channel,
		Timestamp: m.Timestamp,
		Source:    m.Source,
	}

	if m.Payload != nil {
		clone.Payload = make([]byte, len(m.Payload))
		copy(clone.Payload, m.Payload)
	}

	if m.Metadata != nil {
		clone.Metadata = make(map[string]string, len(m.Metadata))
		for k, v := range m.Metadata {
			clone.Metadata[k] = v
		}
	}

	if m.Dedup != nil {
		clone.Dedup = &DedupInfo{
			IsDuplicate: m.Dedup.IsDuplicate,
			Count:       m.Dedup.Count,
			FirstSeen:   m.Dedup.FirstSeen,
		}
	}

	return clone
}

// NewSubscribeMessage creates a new subscribe message for the given channel.
func NewSubscribeMessage(channel string) *Message {
	return &Message{
		Type:      MessageTypeSubscribe,
		Channel:   channel,
		Timestamp: time.Now(),
	}
}

// NewUnsubscribeMessage creates a new unsubscribe message for the given channel.
func NewUnsubscribeMessage(channel string) *Message {
	return &Message{
		Type:      MessageTypeUnsubscribe,
		Channel:   channel,
		Timestamp: time.Now(),
	}
}

// NewPublishMessage creates a new publish message with the given channel and payload.
func NewPublishMessage(channel string, payload []byte) *Message {
	return &Message{
		Type:      MessageTypePublish,
		Channel:   channel,
		Payload:   payload,
		Timestamp: time.Now(),
	}
}

// NewNotificationMessage creates a new notification message.
func NewNotificationMessage(channel string, payload []byte) *Message {
	return &Message{
		Type:      MessageTypeNotification,
		Channel:   channel,
		Payload:   payload,
		Timestamp: time.Now(),
	}
}

// NewPingMessage creates a new ping message.
func NewPingMessage() *Message {
	return &Message{
		Type:      MessageTypePing,
		Timestamp: time.Now(),
	}
}

// NewPongMessage creates a new pong message.
func NewPongMessage() *Message {
	return &Message{
		Type:      MessageTypePong,
		Timestamp: time.Now(),
	}
}

// NewErrorMessage creates a new error message.
func NewErrorMessage(errMsg string) *Message {
	return &Message{
		Type:      MessageTypeError,
		Payload:   []byte(errMsg),
		Timestamp: time.Now(),
	}
}

// NewDataMessage creates a new data message for internal hub routing.
func NewDataMessage(channel string, payload []byte) *Message {
	return &Message{
		Type:      MessageTypeData,
		Channel:   channel,
		Payload:   payload,
		Timestamp: time.Now(),
	}
}
