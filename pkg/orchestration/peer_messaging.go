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

package orchestration

import (
	"context"
	"errors"
)

// =============================================================================
// Peer Message
// =============================================================================

// PeerMessage represents a message in an agent's inbox.
type PeerMessage struct {
	ID        string            `json:"id"`
	From      string            `json:"from"`
	To        string            `json:"to"`
	Action    string            `json:"action"`
	Channel   string            `json:"channel"`
	Content   string            `json:"content"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Timestamp int64             `json:"timestamp"`
	TTL       int64             `json:"ttl,omitempty"`
}

// =============================================================================
// Peer Messaging Service Interface
// =============================================================================

// PeerMessagingService handles agent-to-agent message routing with KV-backed
// persistence, backpressure via inbox policies, idempotent delivery via dedup
// keys, and real-time subscriber notifications.
type PeerMessagingService interface {
	// Send delivers a message to the recipient agent's inbox.
	// Returns ErrDuplicateMessage if the message ID was already delivered,
	// ErrInboxFull if the inbox is at capacity and policy is "reject",
	// or ErrMessageTooLarge if the serialized message exceeds MaxMessageSize.
	Send(ctx context.Context, msg *PeerMessage) error

	// Poll returns messages in the recipient's inbox with timestamps >= since.
	// Messages are returned in chronological order (oldest first).
	Poll(ctx context.Context, tenantID, agentID string, since int64) ([]*PeerMessage, error)

	// Subscribe returns a channel that receives new messages delivered to the
	// specified agent. The channel is buffered and non-blocking on the send side.
	// Only one subscription per agent is active at a time; subsequent calls
	// replace the previous subscription channel.
	Subscribe(ctx context.Context, tenantID, agentID string) (<-chan *PeerMessage, error)

	// Acknowledge removes a message from the agent's inbox. The dedup key is
	// retained to prevent redelivery. Returns nil if the message does not exist.
	Acknowledge(ctx context.Context, tenantID, agentID, msgID string) error

	// InboxSize returns the number of messages currently in the agent's inbox.
	InboxSize(ctx context.Context, tenantID, agentID string) (int, error)
}

// =============================================================================
// Inbox Policy
// =============================================================================

// InboxPolicy controls backpressure behavior for peer messaging inboxes.
type InboxPolicy struct {
	// MaxMessages is the maximum number of messages allowed in an inbox.
	MaxMessages int

	// MaxMessageSize is the maximum serialized size (bytes) of a single message.
	MaxMessageSize int

	// OverflowAction determines behavior when the inbox is full.
	// "reject" returns ErrInboxFull; "drop_oldest" evicts the oldest message.
	OverflowAction string
}

// =============================================================================
// Sentinel Errors
// =============================================================================

var (
	// ErrInboxFull indicates the recipient's inbox is at capacity.
	ErrInboxFull = errors.New("peer messaging: inbox full")

	// ErrMessageTooLarge indicates the message exceeds the maximum allowed size.
	ErrMessageTooLarge = errors.New("peer messaging: message too large")

	// ErrDuplicateMessage indicates the message ID was already delivered.
	ErrDuplicateMessage = errors.New("peer messaging: duplicate message")

	// ErrPeerMessageValidation indicates a peer message failed validation.
	ErrPeerMessageValidation = errors.New("peer messaging: validation failed")

	// ErrPeerInbox indicates a KV store operation on the inbox failed.
	ErrPeerInbox = errors.New("peer messaging: inbox operation failed")

	// ErrPeerSubscription indicates a subscription operation failed.
	ErrPeerSubscription = errors.New("peer messaging: subscription failed")
)

// =============================================================================
// Typed Errors
// =============================================================================

// InboxFullError indicates the recipient's inbox is at capacity and the
// overflow policy is "reject".
type InboxFullError struct {
	TenantID string
	AgentID  string
	Size     int
	Max      int
}

// Error implements the error interface.
func (e *InboxFullError) Error() string {
	return ErrInboxFull.Error() + ": tenant=" + e.TenantID + " agent=" + e.AgentID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *InboxFullError) Unwrap() error {
	return ErrInboxFull
}

// MessageTooLargeError indicates the serialized message exceeds MaxMessageSize.
type MessageTooLargeError struct {
	Size int
	Max  int
}

// Error implements the error interface.
func (e *MessageTooLargeError) Error() string {
	return ErrMessageTooLarge.Error()
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *MessageTooLargeError) Unwrap() error {
	return ErrMessageTooLarge
}

// DuplicateMessageError indicates the message ID was already delivered to
// the recipient's inbox.
type DuplicateMessageError struct {
	TenantID string
	AgentID  string
	MsgID    string
}

// Error implements the error interface.
func (e *DuplicateMessageError) Error() string {
	return ErrDuplicateMessage.Error() + ": id=" + e.MsgID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *DuplicateMessageError) Unwrap() error {
	return ErrDuplicateMessage
}

// PeerMessageValidationError indicates a peer message failed field validation.
type PeerMessageValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *PeerMessageValidationError) Error() string {
	return ErrPeerMessageValidation.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *PeerMessageValidationError) Unwrap() error {
	return ErrPeerMessageValidation
}

// PeerInboxError indicates a KV store operation on the inbox failed.
type PeerInboxError struct {
	Op    string
	Cause error
}

// Error implements the error interface.
func (e *PeerInboxError) Error() string {
	if e.Cause != nil {
		return ErrPeerInbox.Error() + ": " + e.Op + ": " + e.Cause.Error()
	}
	return ErrPeerInbox.Error() + ": " + e.Op
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *PeerInboxError) Unwrap() error {
	return ErrPeerInbox
}

// PeerSubscriptionError indicates a subscription operation failed.
type PeerSubscriptionError struct {
	TenantID string
	AgentID  string
	Message  string
}

// Error implements the error interface.
func (e *PeerSubscriptionError) Error() string {
	return ErrPeerSubscription.Error() + ": tenant=" + e.TenantID + " agent=" + e.AgentID + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *PeerSubscriptionError) Unwrap() error {
	return ErrPeerSubscription
}
