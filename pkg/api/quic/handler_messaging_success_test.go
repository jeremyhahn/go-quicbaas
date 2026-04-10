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

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// createStartedPushService creates a PushService that is properly started
// and ready for use in tests.
func createStartedPushService(t *testing.T) *messaging.PushService {
	t.Helper()
	cfg := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "quic-msg-test",
	}
	log, err := logger.New(cfg)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	svc := messaging.NewPushService(log, nil)
	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("failed to start push service: %v", err)
	}
	return svc
}

// createHandlerWithStartedMsgService creates a handler with a started messaging service.
func createHandlerWithStartedMsgService(t *testing.T) (*Handler, func()) {
	t.Helper()
	msgSvc := createStartedPushService(t)
	h := NewHandler(&mockService{}, nil, nil, nil, nil, createTestLogger(), WithMsgService(msgSvc))
	cleanup := func() {
		msgSvc.Stop(context.Background())
	}
	return h, cleanup
}

// TestMsgCreateChannel_Success tests successful channel creation through the handler.
func TestMsgCreateChannel_Success(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	payload, err := EncodePayload(&MsgCreateChannelRequest{
		Name:       "test-channel",
		BufferSize: 100,
	})
	if err != nil {
		t.Fatalf("failed to encode: %v", err)
	}

	result, err := h.handleMsgCreateChannel(ctx, payload)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestMsgCreateChannel_WithAllOptions tests channel creation with all options set.
func TestMsgCreateChannel_WithAllOptions(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	payload, err := EncodePayload(&MsgCreateChannelRequest{
		Name:       "options-channel",
		Codec:      "json",
		Persistent: true,
		BufferSize: 50,
		MaxHistory: 100,
		TTLSeconds: 3600,
	})
	if err != nil {
		t.Fatalf("failed to encode: %v", err)
	}

	result, err := h.handleMsgCreateChannel(ctx, payload)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestMsgDeleteChannel_Success tests successful channel deletion.
func TestMsgDeleteChannel_Success(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	// First create the channel
	createPayload, _ := EncodePayload(&MsgCreateChannelRequest{Name: "to-delete"})
	_, err := h.handleMsgCreateChannel(ctx, createPayload)
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	// Then delete it
	deletePayload, _ := EncodePayload(&MsgDeleteChannelRequest{Name: "to-delete"})
	result, err := h.handleMsgDeleteChannel(ctx, deletePayload)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestMsgDeleteChannel_NotFound tests deleting a non-existent channel.
func TestMsgDeleteChannel_NotFound(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	payload, _ := EncodePayload(&MsgDeleteChannelRequest{Name: "nonexistent"})
	_, err := h.handleMsgDeleteChannel(ctx, payload)
	if err == nil {
		t.Fatal("expected error for non-existent channel")
	}
}

// TestMsgGetChannel_Success tests successful channel retrieval.
func TestMsgGetChannel_Success(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	// Create channel first
	createPayload, _ := EncodePayload(&MsgCreateChannelRequest{Name: "get-channel"})
	_, err := h.handleMsgCreateChannel(ctx, createPayload)
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	// Get channel info
	getPayload, _ := EncodePayload(&MsgGetChannelRequest{Name: "get-channel"})
	result, err := h.handleMsgGetChannel(ctx, getPayload)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestMsgGetChannel_NotFound tests getting a non-existent channel.
func TestMsgGetChannel_NotFound(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	payload, _ := EncodePayload(&MsgGetChannelRequest{Name: "nonexistent"})
	_, err := h.handleMsgGetChannel(ctx, payload)
	if err == nil {
		t.Fatal("expected error for non-existent channel")
	}
}

// TestMsgListChannels_Success tests successful channel listing.
func TestMsgListChannels_Success(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	// Create a channel
	createPayload, _ := EncodePayload(&MsgCreateChannelRequest{Name: "list-channel"})
	_, err := h.handleMsgCreateChannel(ctx, createPayload)
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	// List channels
	result, err := h.handleMsgListChannels(ctx, []byte("{}"))
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestMsgSubscribe_Success tests successful subscription.
func TestMsgSubscribe_Success(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	// Create channel first
	createPayload, _ := EncodePayload(&MsgCreateChannelRequest{Name: "sub-channel"})
	_, err := h.handleMsgCreateChannel(ctx, createPayload)
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	// Subscribe
	subPayload, _ := EncodePayload(&MsgSubscribeRequest{
		Channel:   "sub-channel",
		SessionID: "session-1",
	})
	result, err := h.handleMsgSubscribe(ctx, subPayload)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestMsgSubscribe_WithFilterOptions tests subscription with filter options.
func TestMsgSubscribe_WithFilterOptions(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	// Create channel first
	createPayload, _ := EncodePayload(&MsgCreateChannelRequest{Name: "filter-channel"})
	_, err := h.handleMsgCreateChannel(ctx, createPayload)
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	// Subscribe with filter options
	subPayload, _ := EncodePayload(&MsgSubscribeRequest{
		Channel:    "filter-channel",
		BufferSize: 50,
		EventTypes: []string{"PUT", "DELETE"},
		KeyPrefix:  "user:",
	})
	result, err := h.handleMsgSubscribe(ctx, subPayload)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestMsgSubscribe_WithEventTypeCaseInsensitive tests event type parsing.
func TestMsgSubscribe_WithEventTypeCaseInsensitive(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	createPayload, _ := EncodePayload(&MsgCreateChannelRequest{Name: "case-channel"})
	_, err := h.handleMsgCreateChannel(ctx, createPayload)
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	subPayload, _ := EncodePayload(&MsgSubscribeRequest{
		Channel:    "case-channel",
		EventTypes: []string{"put", "delete"},
		KeyPrefix:  "prefix:",
	})
	result, err := h.handleMsgSubscribe(ctx, subPayload)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestMsgUnsubscribe_Success tests successful unsubscription.
func TestMsgUnsubscribe_Success(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	// Create channel and subscribe
	createPayload, _ := EncodePayload(&MsgCreateChannelRequest{Name: "unsub-channel"})
	_, err := h.handleMsgCreateChannel(ctx, createPayload)
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	subPayload, _ := EncodePayload(&MsgSubscribeRequest{Channel: "unsub-channel"})
	subResult, err := h.handleMsgSubscribe(ctx, subPayload)
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}

	// Decode subscription response to get ID
	var subResp MsgSubscribeResponse
	if err := DecodePayload(subResult, &subResp); err != nil {
		t.Fatalf("failed to decode subscription response: %v", err)
	}

	// Unsubscribe
	unsubPayload, _ := EncodePayload(&MsgUnsubscribeRequest{SubscriptionID: subResp.SubscriptionID})
	result, err := h.handleMsgUnsubscribe(ctx, unsubPayload)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestMsgPublish_Success tests successful message publishing.
func TestMsgPublish_Success(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	// Create channel
	createPayload, _ := EncodePayload(&MsgCreateChannelRequest{Name: "pub-channel"})
	_, err := h.handleMsgCreateChannel(ctx, createPayload)
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	// Publish message
	pubPayload, _ := EncodePayload(&MsgPublishRequest{
		Channel:   "pub-channel",
		Payload:   "test-message",
		MessageID: "msg-1",
		DedupKey:  "dedup-1",
		Metadata:  map[string]string{"key": "value"},
	})
	result, err := h.handleMsgPublish(ctx, pubPayload)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestMsgPublishDelta_Success tests successful delta publishing.
func TestMsgPublishDelta_Success(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	// Create channel
	createPayload, _ := EncodePayload(&MsgCreateChannelRequest{Name: "delta-channel"})
	_, err := h.handleMsgCreateChannel(ctx, createPayload)
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	// Publish delta
	deltaPayload, _ := EncodePayload(&MsgPublishDeltaRequest{
		Channel:  "delta-channel",
		Old:      map[string]string{"name": "old"},
		New:      map[string]string{"name": "new"},
		Metadata: map[string]string{"author": "test"},
	})
	result, err := h.handleMsgPublishDelta(ctx, deltaPayload)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestMsgGetStats_Success tests successful stats retrieval.
func TestMsgGetStats_Success(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	result, err := h.handleMsgGetStats(ctx, []byte("{}"))
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestMsgDedupStats_NilDedupStore tests dedup stats when dedup is not configured.
func TestMsgDedupStats_NilDedupStore(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	payload, _ := EncodePayload(&MsgDedupStatsRequest{Channel: "test"})
	_, err := h.handleMsgDedupStats(ctx, payload)
	if err == nil {
		t.Fatal("expected error when dedup store is nil")
	}
}

// TestMsgDispatch_Success_ViaDispatcher tests dispatching through handleMsgDispatch with success.
func TestMsgDispatch_Success_ViaDispatcher(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	// Create a channel via dispatch
	channelPayload, _ := EncodePayload(&MsgCreateChannelRequest{Name: "dispatch-channel"})
	fullPayload := append([]byte{SubOpMsgCreateChannel}, channelPayload...)

	resp := h.handleMsgDispatch(ctx, &Request{
		Opcode:  OpMsg,
		Payload: fullPayload,
	})
	if resp.Status != StatusOK {
		t.Fatalf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

// TestMsgDispatch_GetStats_ViaDispatcher tests stats retrieval through dispatch.
func TestMsgDispatch_GetStats_ViaDispatcher(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()
	ctx := context.Background()

	statsPayload := []byte("{}")
	fullPayload := append([]byte{SubOpMsgGetStats}, statsPayload...)

	resp := h.handleMsgDispatch(ctx, &Request{
		Opcode:  OpMsg,
		Payload: fullPayload,
	})
	if resp.Status != StatusOK {
		t.Fatalf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}
