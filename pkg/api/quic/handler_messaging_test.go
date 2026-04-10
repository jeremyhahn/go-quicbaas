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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// =============================================================================
// Messaging Dispatch Tests
// =============================================================================

func TestMsgDispatch_MissingSubOpcode(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMsgDispatch(ctx, &Request{
		Opcode:  OpMsg,
		Payload: []byte{},
	})
	if resp.Status == StatusOK {
		t.Fatal("Expected error for missing sub-opcode")
	}
}

func TestMsgDispatch_UnknownSubOpcode(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMsgDispatch(ctx, &Request{
		Opcode:  OpMsg,
		Payload: []byte{0xFF}, // Unknown sub-opcode
	})
	if resp.Status == StatusOK {
		t.Fatal("Expected error for unknown sub-opcode")
	}
}

// =============================================================================
// Messaging Handler - Service Unavailable Tests
// =============================================================================

func TestMsgCreateChannel_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&MsgCreateChannelRequest{Name: "test-channel"})
	_, err := h.handleMsgCreateChannel(ctx, payload)
	if err == nil {
		t.Fatal("Expected error when msgService is nil")
	}
}

func TestMsgDeleteChannel_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&MsgDeleteChannelRequest{Name: "test-channel"})
	_, err := h.handleMsgDeleteChannel(ctx, payload)
	if err == nil {
		t.Fatal("Expected error when msgService is nil")
	}
}

func TestMsgGetChannel_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&MsgGetChannelRequest{Name: "test-channel"})
	_, err := h.handleMsgGetChannel(ctx, payload)
	if err == nil {
		t.Fatal("Expected error when msgService is nil")
	}
}

func TestMsgListChannels_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	_, err := h.handleMsgListChannels(ctx, []byte("{}"))
	if err == nil {
		t.Fatal("Expected error when msgService is nil")
	}
}

func TestMsgSubscribe_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&MsgSubscribeRequest{Channel: "test"})
	_, err := h.handleMsgSubscribe(ctx, payload)
	if err == nil {
		t.Fatal("Expected error when msgService is nil")
	}
}

func TestMsgUnsubscribe_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&MsgUnsubscribeRequest{SubscriptionID: "sub-1"})
	_, err := h.handleMsgUnsubscribe(ctx, payload)
	if err == nil {
		t.Fatal("Expected error when msgService is nil")
	}
}

func TestMsgPublish_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&MsgPublishRequest{Channel: "test", Payload: "data"})
	_, err := h.handleMsgPublish(ctx, payload)
	if err == nil {
		t.Fatal("Expected error when msgService is nil")
	}
}

func TestMsgPublishDelta_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&MsgPublishDeltaRequest{Channel: "test", Old: "a", New: "b"})
	_, err := h.handleMsgPublishDelta(ctx, payload)
	if err == nil {
		t.Fatal("Expected error when msgService is nil")
	}
}

func TestMsgGetStats_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	_, err := h.handleMsgGetStats(ctx, []byte("{}"))
	if err == nil {
		t.Fatal("Expected error when msgService is nil")
	}
}

func TestMsgDedupStats_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&MsgDedupStatsRequest{Channel: "test"})
	_, err := h.handleMsgDedupStats(ctx, payload)
	if err == nil {
		t.Fatal("Expected error when msgService is nil")
	}
}

// =============================================================================
// Messaging Handler - Decode Error Tests
// createHandlerWithMsgService creates a handler with a non-nil messaging service
// to bypass nil checks and reach decode logic.
// =============================================================================

func createHandlerWithMsgService() *Handler {
	h := createTestHandler()
	h.msgService = &messaging.PushService{}
	return h
}

func TestMsgCreateChannel_DecodeError(t *testing.T) {
	h := createHandlerWithMsgService()
	ctx := context.Background()

	_, err := h.handleMsgCreateChannel(ctx, []byte("{bad"))
	if err == nil {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestMsgDeleteChannel_DecodeError(t *testing.T) {
	h := createHandlerWithMsgService()
	ctx := context.Background()

	_, err := h.handleMsgDeleteChannel(ctx, []byte("{bad"))
	if err == nil {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestMsgGetChannel_DecodeError(t *testing.T) {
	h := createHandlerWithMsgService()
	ctx := context.Background()

	_, err := h.handleMsgGetChannel(ctx, []byte("{bad"))
	if err == nil {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestMsgSubscribe_DecodeError(t *testing.T) {
	h := createHandlerWithMsgService()
	ctx := context.Background()

	_, err := h.handleMsgSubscribe(ctx, []byte("{bad"))
	if err == nil {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestMsgUnsubscribe_DecodeError(t *testing.T) {
	h := createHandlerWithMsgService()
	ctx := context.Background()

	_, err := h.handleMsgUnsubscribe(ctx, []byte("{bad"))
	if err == nil {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestMsgPublish_DecodeError(t *testing.T) {
	h := createHandlerWithMsgService()
	ctx := context.Background()

	_, err := h.handleMsgPublish(ctx, []byte("{bad"))
	if err == nil {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestMsgPublishDelta_DecodeError(t *testing.T) {
	h := createHandlerWithMsgService()
	ctx := context.Background()

	_, err := h.handleMsgPublishDelta(ctx, []byte("{bad"))
	if err == nil {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestMsgDedupStats_DecodeError(t *testing.T) {
	h := createHandlerWithMsgService()
	ctx := context.Background()

	_, err := h.handleMsgDedupStats(ctx, []byte("{bad"))
	if err == nil {
		t.Fatal("Expected error for invalid payload")
	}
}

// =============================================================================
// Messaging Dispatch via handleMsgDispatch - All Sub-Opcodes
// =============================================================================

func TestMsgDispatch_AllSubOpcodes_ServiceUnavailable(t *testing.T) {
	h := createTestHandler() // no msgService set
	ctx := context.Background()

	tests := []struct {
		name      string
		subOpcode byte
	}{
		{"CreateChannel", SubOpMsgCreateChannel},
		{"DeleteChannel", SubOpMsgDeleteChannel},
		{"GetChannel", SubOpMsgGetChannel},
		{"ListChannels", SubOpMsgListChannels},
		{"Subscribe", SubOpMsgSubscribe},
		{"Unsubscribe", SubOpMsgUnsubscribe},
		{"Publish", SubOpMsgPublish},
		{"PublishDelta", SubOpMsgPublishDelta},
		{"GetStats", SubOpMsgGetStats},
		{"DedupStats", SubOpMsgDedupStats},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := append([]byte{tt.subOpcode}, []byte("{}")...)
			resp := h.handleMsgDispatch(ctx, &Request{
				Opcode:  OpMsg,
				Payload: payload,
			})
			if resp.Status == StatusOK {
				t.Fatalf("Expected error for %s with nil msgService", tt.name)
			}
		})
	}
}

// =============================================================================
// MsgSubOpcodeToString Tests
// =============================================================================

func TestMsgSubOpcodeToString(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected string
	}{
		{SubOpMsgCreateChannel, "MsgCreateChannel"},
		{SubOpMsgDeleteChannel, "MsgDeleteChannel"},
		{SubOpMsgGetChannel, "MsgGetChannel"},
		{SubOpMsgListChannels, "MsgListChannels"},
		{SubOpMsgSubscribe, "MsgSubscribe"},
		{SubOpMsgUnsubscribe, "MsgUnsubscribe"},
		{SubOpMsgPublish, "MsgPublish"},
		{SubOpMsgPublishDelta, "MsgPublishDelta"},
		{SubOpMsgGetStats, "MsgGetStats"},
		{SubOpMsgDedupStats, "MsgDedupStats"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := MsgSubOpcodeToString(tt.opcode)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestMsgSubOpcodeToString_Unknown(t *testing.T) {
	result := MsgSubOpcodeToString(0xFF)
	if result == "" {
		t.Fatal("Expected non-empty string for unknown opcode")
	}
}

// =============================================================================
// Messaging Service Error Path Tests
// =============================================================================

// TestMsgListChannels_SuccessPath verifies that handleMsgListChannels
// returns successfully on a healthy push service.
func TestMsgListChannels_SuccessPath(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	result, err := h.handleMsgListChannels(ctx, []byte("{}"))
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestMsgUnsubscribe_InvalidSubscriptionID verifies handleMsgUnsubscribe
// returns an error for a non-existent subscription ID.
func TestMsgUnsubscribe_InvalidSubscriptionID(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	payload, _ := EncodePayload(&MsgUnsubscribeRequest{SubscriptionID: "nonexistent-id"})
	_, err := h.handleMsgUnsubscribe(ctx, payload)
	if err == nil {
		t.Fatal("expected error for non-existent subscription ID")
	}
}

// TestMsgPublish_NonExistentChannel verifies handleMsgPublish returns an
// error when publishing to a channel that does not exist.
func TestMsgPublish_NonExistentChannel(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	payload, _ := EncodePayload(&MsgPublishRequest{
		Channel: "nonexistent-channel",
		Payload: "test",
	})
	_, err := h.handleMsgPublish(ctx, payload)
	if err == nil {
		t.Fatal("expected error for non-existent channel")
	}
}

// TestMsgPublishDelta_NonExistentChannel verifies handleMsgPublishDelta
// returns an error when publishing to a non-existent channel.
func TestMsgPublishDelta_NonExistentChannel(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	payload, _ := EncodePayload(&MsgPublishDeltaRequest{
		Channel: "nonexistent-channel",
		Old:     map[string]string{"k": "old"},
		New:     map[string]string{"k": "new"},
	})
	_, err := h.handleMsgPublishDelta(ctx, payload)
	if err == nil {
		t.Fatal("expected error for non-existent channel")
	}
}

// TestMsgSubscribe_AutoCreateChannel verifies that handleMsgSubscribe
// succeeds even when the channel does not pre-exist, as the PushService
// auto-creates channels on subscribe.
func TestMsgSubscribe_AutoCreateChannel(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	payload, _ := EncodePayload(&MsgSubscribeRequest{Channel: "auto-created"})
	result, err := h.handleMsgSubscribe(ctx, payload)
	if err != nil {
		t.Fatalf("expected subscribe to succeed (auto-create), got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestMsgCreateChannel_MinimalRequest verifies handleMsgCreateChannel
// handles a valid minimal request.
func TestMsgCreateChannel_MinimalRequest(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	// Create with just a name (no optional fields) to test the zero-value paths
	payload, _ := EncodePayload(&MsgCreateChannelRequest{Name: "minimal"})
	result, err := h.handleMsgCreateChannel(ctx, payload)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestMsgDispatch_DedupStats_ViaDispatcher tests dedup stats dispatch
// with no dedup store configured.
func TestMsgDispatch_DedupStats_ViaDispatcher(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	dedupPayload, _ := EncodePayload(&MsgDedupStatsRequest{Channel: "test"})
	fullPayload := append([]byte{SubOpMsgDedupStats}, dedupPayload...)

	resp := h.handleMsgDispatch(ctx, &Request{
		Opcode:  OpMsg,
		Payload: fullPayload,
	})
	// Should fail because no dedup store is configured
	if resp.Status == StatusOK {
		t.Fatal("expected error when dedup store is nil")
	}
}
