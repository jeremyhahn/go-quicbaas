//go:build quic

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

package quic

import (
	"context"
	"fmt"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// =============================================================================
// Messaging Sub-Operation Codes
// =============================================================================

// Messaging sub-operation codes.
// Uses dedicated opcode OpMsg with sub-operation dispatch via payload[0].
// Range 0x20-0x2F is reserved for messaging operations.
const (
	SubOpMsgCreateChannel byte = 0x20
	SubOpMsgDeleteChannel byte = 0x21
	SubOpMsgGetChannel    byte = 0x22
	SubOpMsgListChannels  byte = 0x23
	SubOpMsgSubscribe     byte = 0x24
	SubOpMsgUnsubscribe   byte = 0x25
	SubOpMsgPublish       byte = 0x26
	SubOpMsgPublishDelta  byte = 0x27
	SubOpMsgGetStats      byte = 0x28
	SubOpMsgDedupStats    byte = 0x29
)

// =============================================================================
// Messaging Service Request/Response Types
// =============================================================================

// MsgCreateChannelRequest represents a request to create a notification channel.
type MsgCreateChannelRequest struct {
	Name       string `json:"name"`
	Codec      string `json:"codec,omitempty"`
	Persistent bool   `json:"persistent,omitempty"`
	BufferSize int    `json:"buffer_size,omitempty"`
	MaxHistory int    `json:"max_history,omitempty"`
	TTLSeconds int64  `json:"ttl_seconds,omitempty"`
}

// MsgDeleteChannelRequest represents a request to delete a notification channel.
type MsgDeleteChannelRequest struct {
	Name string `json:"name"`
}

// MsgGetChannelRequest represents a request to get channel information.
type MsgGetChannelRequest struct {
	Name string `json:"name"`
}

// MsgListChannelsRequest represents a request to list all channels.
type MsgListChannelsRequest struct{}

// MsgSubscribeRequest represents a request to subscribe to a channel.
type MsgSubscribeRequest struct {
	Channel    string   `json:"channel"`
	SessionID  string   `json:"session_id,omitempty"`
	BufferSize int      `json:"buffer_size,omitempty"`
	EventTypes []string `json:"event_types,omitempty"`
	KeyPrefix  string   `json:"key_prefix,omitempty"`
}

// MsgSubscribeResponse represents a subscription response.
type MsgSubscribeResponse struct {
	SubscriptionID string `json:"subscription_id"`
	Channel        string `json:"channel"`
	ClientID       string `json:"client_id"`
}

// MsgUnsubscribeRequest represents a request to unsubscribe.
type MsgUnsubscribeRequest struct {
	SubscriptionID string `json:"subscription_id"`
}

// MsgPublishRequest represents a request to publish a message.
type MsgPublishRequest struct {
	Channel   string            `json:"channel"`
	Payload   interface{}       `json:"payload"`
	MessageID string            `json:"message_id,omitempty"`
	DedupKey  string            `json:"dedup_key,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// MsgPublishResponse represents the response from a publish operation.
type MsgPublishResponse struct {
	MessageID   string `json:"message_id"`
	IsDuplicate bool   `json:"is_duplicate"`
}

// MsgPublishDeltaRequest represents a request to publish a delta message.
type MsgPublishDeltaRequest struct {
	Channel  string            `json:"channel"`
	Old      interface{}       `json:"old"`
	New      interface{}       `json:"new"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// MsgGetStatsRequest represents a request to get messaging service statistics.
type MsgGetStatsRequest struct{}

// MsgGetStatsResponse represents messaging service statistics.
type MsgGetStatsResponse struct {
	TotalChannels      int64 `json:"total_channels"`
	TotalSubscriptions int64 `json:"total_subscriptions"`
	MessagesPublished  int64 `json:"messages_published"`
	MessagesDelivered  int64 `json:"messages_delivered"`
}

// MsgDedupStatsRequest represents a request to get deduplication statistics.
type MsgDedupStatsRequest struct {
	Channel string `json:"channel"`
}

// MsgDedupStatsResponse represents deduplication statistics.
type MsgDedupStatsResponse struct {
	Channel         string `json:"channel"`
	TotalMessages   int64  `json:"total_messages"`
	TotalDuplicates int64  `json:"total_duplicates"`
}

// =============================================================================
// Messaging Handler Map-Based Dispatch
// =============================================================================

// msgHandlerFunc is the signature for messaging sub-operation handlers.
type msgHandlerFunc func(h *Handler, ctx context.Context, payload []byte) ([]byte, error)

// msgHandlers provides O(1) constant-time lookup for messaging sub-operation handlers.
var msgHandlers = map[byte]msgHandlerFunc{
	SubOpMsgCreateChannel: (*Handler).handleMsgCreateChannel,
	SubOpMsgDeleteChannel: (*Handler).handleMsgDeleteChannel,
	SubOpMsgGetChannel:    (*Handler).handleMsgGetChannel,
	SubOpMsgListChannels:  (*Handler).handleMsgListChannels,
	SubOpMsgSubscribe:     (*Handler).handleMsgSubscribe,
	SubOpMsgUnsubscribe:   (*Handler).handleMsgUnsubscribe,
	SubOpMsgPublish:       (*Handler).handleMsgPublish,
	SubOpMsgPublishDelta:  (*Handler).handleMsgPublishDelta,
	SubOpMsgGetStats:      (*Handler).handleMsgGetStats,
	SubOpMsgDedupStats:    (*Handler).handleMsgDedupStats,
}

// =============================================================================
// Messaging Dispatch Handler
// =============================================================================

// handleMsgDispatch routes messaging operations based on sub-opcode in payload.
// The messaging service uses OpMsg as a dispatch opcode with sub-operation
// dispatch via the first byte of the payload.
func (h *Handler) handleMsgDispatch(ctx context.Context, req *Request) *Response {
	// Payload must contain at least sub-opcode byte
	if len(req.Payload) < 1 {
		return h.errorResponse(api.NewMissingSubOpcodeError(
			"quic.handleMsgDispatch", "messaging"))
	}

	// Extract sub-opcode and remaining payload
	subOpcode := req.Payload[0]
	subPayload := req.Payload[1:] // Skip sub-opcode byte

	// O(1) lookup for handler
	handler, ok := msgHandlers[subOpcode]
	if !ok {
		return h.errorResponse(api.NewUnknownSubOpcodeError(
			"quic.handleMsgDispatch", "messaging", subOpcode))
	}

	// Execute handler
	respPayload, err := handler(h, ctx, subPayload)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: respPayload}
}

// =============================================================================
// Messaging Channel Operations Handlers
// =============================================================================

// handleMsgCreateChannel creates a new notification channel.
func (h *Handler) handleMsgCreateChannel(ctx context.Context, payload []byte) ([]byte, error) {
	if h.msgService == nil {
		return nil, api.NewServiceUnavailableError("messaging")
	}

	var req MsgCreateChannelRequest
	if err := DecodePayload(payload, &req); err != nil {
		return nil, err
	}

	// Build create channel options
	var opts []messaging.CreateChannelOption
	if req.Codec != "" {
		opts = append(opts, messaging.WithCodec(req.Codec))
	}
	if req.Persistent {
		opts = append(opts, messaging.WithPersistence(true))
	}
	if req.BufferSize > 0 {
		opts = append(opts, messaging.WithChannelBufferSize(req.BufferSize))
	}
	if req.MaxHistory > 0 {
		opts = append(opts, messaging.WithMaxHistory(req.MaxHistory))
	}
	if req.TTLSeconds > 0 {
		opts = append(opts, messaging.WithChannelTTL(time.Duration(req.TTLSeconds)*time.Second))
	}

	channel, err := h.msgService.CreateChannel(ctx, req.Name, opts...)
	if err != nil {
		return nil, err
	}

	return EncodePayload(channel)
}

// handleMsgDeleteChannel deletes a notification channel.
func (h *Handler) handleMsgDeleteChannel(ctx context.Context, payload []byte) ([]byte, error) {
	if h.msgService == nil {
		return nil, api.NewServiceUnavailableError("messaging")
	}

	var req MsgDeleteChannelRequest
	if err := DecodePayload(payload, &req); err != nil {
		return nil, err
	}

	if err := h.msgService.DeleteChannel(ctx, req.Name); err != nil {
		return nil, err
	}

	return EncodePayload(map[string]bool{"success": true})
}

// handleMsgGetChannel retrieves channel information.
func (h *Handler) handleMsgGetChannel(ctx context.Context, payload []byte) ([]byte, error) {
	if h.msgService == nil {
		return nil, api.NewServiceUnavailableError("messaging")
	}

	var req MsgGetChannelRequest
	if err := DecodePayload(payload, &req); err != nil {
		return nil, err
	}

	channel, err := h.msgService.GetChannel(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	return EncodePayload(channel)
}

// handleMsgListChannels lists all notification channels.
func (h *Handler) handleMsgListChannels(ctx context.Context, payload []byte) ([]byte, error) {
	if h.msgService == nil {
		return nil, api.NewServiceUnavailableError("messaging")
	}

	channels, err := h.msgService.ListChannels(ctx)
	if err != nil {
		return nil, err
	}

	return EncodePayload(channels)
}

// =============================================================================
// Messaging Subscription Operations Handlers
// =============================================================================

// handleMsgSubscribe subscribes to a notification channel.
func (h *Handler) handleMsgSubscribe(ctx context.Context, payload []byte) ([]byte, error) {
	if h.msgService == nil {
		return nil, api.NewServiceUnavailableError("messaging")
	}

	var req MsgSubscribeRequest
	if err := DecodePayload(payload, &req); err != nil {
		return nil, err
	}

	// Build subscribe options
	var opts []messaging.SubscribeOption
	if req.SessionID != "" {
		opts = append(opts, messaging.WithSession(req.SessionID))
	}
	if req.BufferSize > 0 {
		opts = append(opts, messaging.WithSubscriptionBufferSize(req.BufferSize))
	}
	if req.KeyPrefix != "" || len(req.EventTypes) > 0 {
		filter := &messaging.SubscriptionFilter{
			KeyPrefix: req.KeyPrefix,
		}
		// Convert event type strings to EventType values
		for _, et := range req.EventTypes {
			switch et {
			case "PUT", "put":
				filter.EventTypes = append(filter.EventTypes, messaging.EventTypePut)
			case "DELETE", "delete":
				filter.EventTypes = append(filter.EventTypes, messaging.EventTypeDelete)
			}
		}
		opts = append(opts, messaging.WithFilter(filter))
	}

	subscription, err := h.msgService.Subscribe(ctx, req.Channel, opts...)
	if err != nil {
		return nil, err
	}

	resp := MsgSubscribeResponse{
		SubscriptionID: subscription.ID,
		Channel:        subscription.Channel,
		ClientID:       subscription.ClientID,
	}

	return EncodePayload(resp)
}

// handleMsgUnsubscribe removes a subscription.
func (h *Handler) handleMsgUnsubscribe(ctx context.Context, payload []byte) ([]byte, error) {
	if h.msgService == nil {
		return nil, api.NewServiceUnavailableError("messaging")
	}

	var req MsgUnsubscribeRequest
	if err := DecodePayload(payload, &req); err != nil {
		return nil, err
	}

	if err := h.msgService.Unsubscribe(ctx, req.SubscriptionID); err != nil {
		return nil, err
	}

	return EncodePayload(map[string]bool{"success": true})
}

// =============================================================================
// Messaging Publishing Operations Handlers
// =============================================================================

// handleMsgPublish publishes a message to a channel.
func (h *Handler) handleMsgPublish(ctx context.Context, payload []byte) ([]byte, error) {
	if h.msgService == nil {
		return nil, api.NewServiceUnavailableError("messaging")
	}

	var req MsgPublishRequest
	if err := DecodePayload(payload, &req); err != nil {
		return nil, err
	}

	// Build publish options
	var opts []messaging.PublishOption
	if req.MessageID != "" {
		opts = append(opts, messaging.WithMessageID(req.MessageID))
	}
	if req.DedupKey != "" {
		opts = append(opts, messaging.WithDedupKey(req.DedupKey))
	}
	if req.Metadata != nil {
		opts = append(opts, messaging.WithMetadata(req.Metadata))
	}

	result, err := h.msgService.Publish(ctx, req.Channel, req.Payload, opts...)
	if err != nil {
		return nil, err
	}

	return EncodePayload(MsgPublishResponse{
		MessageID:   result.MessageID,
		IsDuplicate: result.IsDuplicate,
	})
}

// handleMsgPublishDelta publishes a delta message to a channel.
func (h *Handler) handleMsgPublishDelta(ctx context.Context, payload []byte) ([]byte, error) {
	if h.msgService == nil {
		return nil, api.NewServiceUnavailableError("messaging")
	}

	var req MsgPublishDeltaRequest
	if err := DecodePayload(payload, &req); err != nil {
		return nil, err
	}

	// Build publish options
	var opts []messaging.PublishOption
	if req.Metadata != nil {
		opts = append(opts, messaging.WithMetadata(req.Metadata))
	}

	result, err := h.msgService.PublishDelta(ctx, req.Channel, req.Old, req.New, opts...)
	if err != nil {
		return nil, err
	}

	return EncodePayload(MsgPublishResponse{
		MessageID:   result.MessageID,
		IsDuplicate: result.IsDuplicate,
	})
}

// =============================================================================
// Messaging Statistics Operations Handlers
// =============================================================================

// handleMsgGetStats retrieves messaging service statistics.
func (h *Handler) handleMsgGetStats(ctx context.Context, payload []byte) ([]byte, error) {
	if h.msgService == nil {
		return nil, api.NewServiceUnavailableError("messaging")
	}

	stats := h.msgService.GetStats()

	resp := MsgGetStatsResponse{
		TotalChannels:      stats.TotalChannels,
		TotalSubscriptions: stats.TotalSubscriptions,
		MessagesPublished:  stats.MessagesPublished,
		MessagesDelivered:  stats.MessagesDelivered,
	}

	return EncodePayload(resp)
}

// handleMsgDedupStats retrieves deduplication statistics for a channel.
func (h *Handler) handleMsgDedupStats(ctx context.Context, payload []byte) ([]byte, error) {
	if h.msgService == nil {
		return nil, api.NewServiceUnavailableError("messaging")
	}

	var req MsgDedupStatsRequest
	if err := DecodePayload(payload, &req); err != nil {
		return nil, err
	}

	// Get dedup store from messaging service
	dedupStore := h.msgService.GetDedupStore()
	if dedupStore == nil {
		return nil, api.NewServiceUnavailableError("deduplication")
	}

	stats, err := dedupStore.GetStats(ctx, req.Channel)
	if err != nil {
		return nil, err
	}

	resp := MsgDedupStatsResponse{
		Channel:         stats.Channel,
		TotalMessages:   stats.TotalMessages,
		TotalDuplicates: stats.TotalDuplicates,
	}

	return EncodePayload(resp)
}

// =============================================================================
// Messaging Opcode String Helpers
// =============================================================================

// MsgSubOpcodeToString returns a human-readable name for a messaging sub-opcode.
func MsgSubOpcodeToString(subOpcode byte) string {
	switch subOpcode {
	case SubOpMsgCreateChannel:
		return "MsgCreateChannel"
	case SubOpMsgDeleteChannel:
		return "MsgDeleteChannel"
	case SubOpMsgGetChannel:
		return "MsgGetChannel"
	case SubOpMsgListChannels:
		return "MsgListChannels"
	case SubOpMsgSubscribe:
		return "MsgSubscribe"
	case SubOpMsgUnsubscribe:
		return "MsgUnsubscribe"
	case SubOpMsgPublish:
		return "MsgPublish"
	case SubOpMsgPublishDelta:
		return "MsgPublishDelta"
	case SubOpMsgGetStats:
		return "MsgGetStats"
	case SubOpMsgDedupStats:
		return "MsgDedupStats"
	default:
		return fmt.Sprintf("UnknownMsgOp(0x%02X)", subOpcode)
	}
}
