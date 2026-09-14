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
// persistence, backpressure via inbox policies, exactly-once delivery decided
// by a guarded claim, and real-time subscriber notifications.
type PeerMessagingService interface {
	// Send delivers a message to the recipient agent's inbox EXACTLY ONCE.
	//
	// Delivery of one message ID is decided by a guarded claim rather than
	// suppressed after the fact, so two attempts racing across two nodes
	// produce one inbox entry and one winner. Returns ErrDuplicateMessage
	// when this ID was already delivered, ErrDeliveryInFlight — retryable —
	// when another attempt holds the claim and has not yet recorded a
	// delivery, ErrInboxFull if the inbox is at capacity and policy is
	// "reject", or ErrMessageTooLarge if the serialized message exceeds
	// MaxMessageSize.
	Send(ctx context.Context, msg *PeerMessage) error

	// Poll returns messages in the recipient's inbox with timestamps >= since.
	// Messages are returned in chronological order (oldest first).
	Poll(ctx context.Context, tenantID, agentID string, since int64) ([]*PeerMessage, error)

	// Subscribe returns a channel that receives new messages delivered to the
	// specified agent. The channel is buffered and non-blocking on the send side.
	// Only one subscription per agent is active at a time; subsequent calls
	// replace the previous subscription channel.
	Subscribe(ctx context.Context, tenantID, agentID string) (<-chan *PeerMessage, error)

	// Acknowledge removes a message from the agent's inbox. The delivered
	// marker is retained, so a re-send of an acknowledged message is still
	// refused as a duplicate. Returns nil if the message does not exist.
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
	//
	// IT IS APPROXIMATE, AND SAYING SO IS THE HONEST CONTRACT. Delivery is
	// serialized per message ID, not per inbox: two DIFFERENT messages hold
	// two different claims and can therefore both observe the same last free
	// slot and both take it. Ordering the check differently does not fix
	// that — an exact limit needs a guarded counter for the inbox itself,
	// which is a different mechanism than the one deciding delivery. An
	// inbox may briefly sit over its limit; it does not grow without bound,
	// because the next delivery sees the real size.
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

	// ErrDeliveryInFlight indicates another attempt holds this message's
	// delivery claim and has not recorded a delivery.
	ErrDeliveryInFlight = errors.New("peer messaging: delivery claim held, no delivery recorded")

	// ErrPeerInboxUnguarded indicates the backing store cannot decide a
	// contested delivery.
	ErrPeerInboxUnguarded = errors.New("peer messaging: store cannot decide a contested delivery")

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
//
// This is a statement about a DELIVERY THAT HAPPENED, not about a key that
// happens to be present. It is returned only against the durable delivered
// marker Send writes after the inbox write lands, so it survives both
// Acknowledge — which removes the inbox entry and deliberately keeps the
// marker — and a redelivery carrying a different timestamp. An attempt that
// merely lost the claim to another in-flight attempt gets
// DeliveryInFlightError instead, because those two states say different things
// and only one of them means the message arrived.
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

// DeliveryInFlightError reports an attempt that lost this message's delivery
// claim to a holder that has not recorded a delivery.
//
// IT IS RETRYABLE, AND IT IS DELIBERATELY NOT A DUPLICATE. Claim held with no
// delivered marker has two causes — a holder still in flight, and a holder that
// crashed between winning the claim and recording delivery — and they leave
// byte-identical key states, so no read distinguishes them. Reporting a
// duplicate here would tell a caller its message arrived when it may never
// have, which is the one answer this design refuses to give. A holder that
// crashed leaves the claim stalled until it is reaped (WP10, run-is-authority),
// so the residue is a loud retry rather than a silent loss.
type DeliveryInFlightError struct {
	TenantID string
	AgentID  string
	MsgID    string
}

// Error implements the error interface.
func (e *DeliveryInFlightError) Error() string {
	return ErrDeliveryInFlight.Error() + ": id=" + e.MsgID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *DeliveryInFlightError) Unwrap() error {
	return ErrDeliveryInFlight
}

// PeerInboxUnguardedError reports a store that cannot decide a contested
// delivery, and therefore cannot deliver at all.
//
// A store lacking the guarded verbs gets NO EMULATION. Probe-then-write is the
// exact race exactly-once delivery exists to remove, so a store that cannot
// decide a claim makes Send refuse rather than deliver on a hope.
type PeerInboxUnguardedError struct {
	Message string
}

// Error implements the error interface.
func (e *PeerInboxUnguardedError) Error() string {
	return ErrPeerInboxUnguarded.Error() + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *PeerInboxUnguardedError) Unwrap() error {
	return ErrPeerInboxUnguarded
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
