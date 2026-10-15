// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// messaging_extra_test.go covers the Messaging MCP handler's transform paths
// that the end-to-end client tests do not exercise: delete/subscribe/
// unsubscribe/publish-delta requests, subscription/dedup-stats responses,
// payload encoding, and the typed error paths.

package mcp

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

func newMsgHandler(t *testing.T) *messagingHandler {
	t.Helper()
	tr, err := New(&transport.Config{Address: "http://localhost:8080"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	return newMessagingHandler(tr)
}

func TestMessagingTransformDeleteChannel(t *testing.T) {
	h := newMsgHandler(t)
	args, err := h.transformRequest("DeleteChannel", &messaging.DeleteChannelRequest{Name: "ch"})
	if err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}
	if args["name"] != "ch" {
		t.Errorf("name = %v", args["name"])
	}
}

func TestMessagingTransformSubscribe(t *testing.T) {
	h := newMsgHandler(t)
	args, err := h.transformRequest("Subscribe", &messaging.SubscribeRequest{
		Channel:    "ch",
		SessionID:  "sess",
		BufferSize: 32,
		Filter: &messaging.SubscriptionFilter{
			KeyPrefix:  "ns:",
			EventTypes: []messaging.EventType{messaging.EventTypePut, messaging.EventTypeDelete},
		},
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if args["channel"] != "ch" || args["session_id"] != "sess" {
		t.Errorf("args = %v", args)
	}
	if args["filter_key_prefix"] != "ns:" {
		t.Errorf("filter_key_prefix = %v", args["filter_key_prefix"])
	}
	if args["filter_event_types"] != `["PUT","DELETE"]` {
		t.Errorf("filter_event_types = %v", args["filter_event_types"])
	}
}

// TestMessagingSubscribeKeepsKeyPrefixRoundTrip pins that an MCP subscriber's
// filter key prefix reaches the subscribe tool as filter_key_prefix.
func TestMessagingSubscribeKeepsKeyPrefixRoundTrip(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_msg_subscribe",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["filter_key_prefix"] != "orders/eu/" {
				t.Errorf("filter_key_prefix = %v, want orders/eu/", args["filter_key_prefix"])
			}
		},
		result: map[string]interface{}{
			"subscription_id": "sub-1",
			"channel":         "orders",
			"client_id":       "client-1",
			"created_at":      "2023-11-14T22:13:20Z",
		},
	})
	defer cleanup()

	_, err := messaging.NewClient(tr).Subscribe(context.Background(), &messaging.SubscribeRequest{
		Channel: "orders",
		Filter:  &messaging.SubscriptionFilter{KeyPrefix: "orders/eu/"},
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
}

func TestMessagingTransformUnsubscribe(t *testing.T) {
	h := newMsgHandler(t)
	args, err := h.transformRequest("Unsubscribe", &messaging.UnsubscribeRequest{SubscriptionID: "sub-1"})
	if err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	if args["subscription_id"] != "sub-1" {
		t.Errorf("subscription_id = %v", args["subscription_id"])
	}
}

func TestMessagingTransformPublishDelta(t *testing.T) {
	h := newMsgHandler(t)
	args, err := h.transformRequest("PublishDelta", &messaging.PublishDeltaRequest{
		Channel:   "ch",
		Old:       map[string]interface{}{"v": 1},
		New:       map[string]interface{}{"v": 2},
		MessageID: "m1",
		DedupKey:  "d1",
	})
	if err != nil {
		t.Fatalf("PublishDelta: %v", err)
	}
	if args["channel"] != "ch" || args["old_state"] != `{"v":1}` || args["new_state"] != `{"v":2}` {
		t.Errorf("args = %v", args)
	}
	if args["message_id"] != "m1" || args["dedup_key"] != "d1" {
		t.Errorf("dedup args = %v", args)
	}
}

func TestMessagingTransformPublishMetadata(t *testing.T) {
	h := newMsgHandler(t)
	args, err := h.transformRequest("Publish", &messaging.PublishRequest{
		Channel:  "ch",
		Payload:  []byte{0x00, 0x01, 0x02}, // non-UTF8 -> base64
		Metadata: map[string]string{"k": "v"},
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if args["payload"] != "AAEC" {
		t.Errorf("payload = %v (want base64)", args["payload"])
	}
	if args["metadata"] != `{"k":"v"}` {
		t.Errorf("metadata = %v", args["metadata"])
	}
}

func TestMessagingGetDedupStatsRoundTrip(t *testing.T) {
	ctx := context.Background()

	t.Run("channel figures", func(t *testing.T) {
		tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
			wantTool: "quicbaas_msg_dedup_stats",
			assertArgs: func(t *testing.T, args map[string]interface{}) {
				if args["channel"] != "orders" {
					t.Errorf("channel argument = %v, want orders", args["channel"])
				}
			},
			result: map[string]interface{}{
				"channel": "orders", "available": true,
				"total_messages": 10, "total_duplicates": 4,
			},
		})
		defer cleanup()

		got, err := messaging.NewClient(tr).GetDedupStats(ctx, "orders")
		if err != nil {
			t.Fatalf("GetDedupStats: %v", err)
		}
		// The tool reports 10 unique messages and 4 duplicates.
		want := messaging.DedupStats{TotalChecks: 14, DuplicatesDetected: 4, UniqueMessages: 10}
		if *got != want {
			t.Errorf("dedup stats = %+v, want %+v", *got, want)
		}
	})

	t.Run("unknown channel", func(t *testing.T) {
		tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
			wantTool:      "quicbaas_msg_dedup_stats",
			result:        map[string]interface{}{"error": "channel not found"},
			toolError:     true,
			toolErrorCode: MCPResourceNotFound,
		})
		defer cleanup()

		_, err := messaging.NewClient(tr).GetDedupStats(ctx, "missing")
		if !errors.IsNotFound(err) {
			t.Errorf("err = %v, want a NotFound error", err)
		}
	})

	t.Run("missing channel", func(t *testing.T) {
		h := newMsgHandler(t)
		if _, err := h.transformRequest("GetDedupStats", &messaging.GetDedupStatsRequest{}); !errors.IsInvalidInput(err) {
			t.Errorf("empty channel: err = %v, want an InvalidInput error", err)
		}
		if _, err := h.transformRequest("GetDedupStats", struct{}{}); !errors.IsInvalidInput(err) {
			t.Errorf("wrong request type: err = %v, want an InvalidInput error", err)
		}
	})
}

// TestMessagingWatchChannelRoundTrip pins the watch round trip through the
// SDK client: every request field reaches the tool arguments, and every
// subscription field the tool returns reaches the caller.
func TestMessagingWatchChannelRoundTrip(t *testing.T) {
	ctx := context.Background()

	t.Run("every field", func(t *testing.T) {
		tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
			wantTool: "quicbaas_msg_watch_channel",
			assertArgs: func(t *testing.T, args map[string]interface{}) {
				want := map[string]interface{}{
					"key_prefix":         "orders/",
					"session_id":         "sess-1",
					"buffer_size":        float64(8),
					"filter_key_prefix":  "orders/eu/",
					"filter_event_types": `["DELETE"]`,
				}
				if !reflect.DeepEqual(args, want) {
					t.Errorf("arguments = %v, want %v", args, want)
				}
			},
			result: map[string]interface{}{
				"subscription_id": "watch-1",
				"channel":         "watch:orders/",
				"client_id":       "sess-1",
				"created_at":      "2023-11-14T22:13:20Z",
				"channel_name":    "watch:orders/",
				"timestamp":       "2023-11-14T22:13:21Z",
			},
		})
		defer cleanup()

		sub, err := messaging.NewClient(tr).WatchChannel(ctx, &messaging.WatchChannelRequest{
			KeyPrefix:  "orders/",
			SessionID:  "sess-1",
			BufferSize: 8,
			Filter: &messaging.SubscriptionFilter{
				EventTypes: []messaging.EventType{messaging.EventTypeDelete},
				KeyPrefix:  "orders/eu/",
			},
		})
		if err != nil {
			t.Fatalf("WatchChannel: %v", err)
		}
		if sub.ID != "watch-1" || sub.Channel != "watch:orders/" || sub.ClientID != "sess-1" ||
			!sub.CreatedAt.Equal(time.Unix(1700000000, 0)) {
			t.Errorf("subscription = %+v", *sub)
		}
	})

	t.Run("empty key prefix", func(t *testing.T) {
		tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{wantTool: "quicbaas_msg_watch_channel"})
		defer cleanup()

		_, err := messaging.NewClient(tr).WatchChannel(ctx, &messaging.WatchChannelRequest{})
		if !errors.IsInvalidInput(err) {
			t.Errorf("err = %v, want an InvalidInput error", err)
		}
	})

	t.Run("wrong request type", func(t *testing.T) {
		if _, err := newMsgHandler(t).transformRequest("WatchChannel", struct{}{}); !errors.IsInvalidInput(err) {
			t.Errorf("err = %v, want an InvalidInput error", err)
		}
	})
}

func TestMessagingTransformResponseSubscriptionAndDedup(t *testing.T) {
	h := newMsgHandler(t)

	sub, err := h.transformResponse("Subscribe", map[string]interface{}{
		"subscription_id": "s1", "channel": "ch", "client_id": "c1"})
	if err != nil {
		t.Fatalf("Subscribe response: %v", err)
	}
	if sub.(*messaging.Subscription).ID != "s1" {
		t.Errorf("subscription mismatch: %+v", sub)
	}

	dedup, err := h.transformResponse("GetDedupStats", map[string]interface{}{
		"total_messages": float64(100), "total_duplicates": float64(7)})
	if err != nil {
		t.Fatalf("GetDedupStats response: %v", err)
	}
	ds := dedup.(*messaging.DedupStats)
	// total_messages counts unique messages, so every check is one of the
	// 100 unique messages or one of the 7 duplicates.
	if ds.TotalChecks != 107 || ds.DuplicatesDetected != 7 || ds.UniqueMessages != 100 {
		t.Errorf("dedup stats mismatch: %+v", ds)
	}
}

func TestMessagingVoidResponses(t *testing.T) {
	h := newMsgHandler(t)
	for _, m := range []string{"DeleteChannel", "Unsubscribe", "Publish", "PublishDelta"} {
		out, err := h.transformResponse(m, map[string]interface{}{"ok": true})
		if err != nil || out != nil {
			t.Errorf("%s void response = %v, %v", m, out, err)
		}
	}
}

func TestMessagingTransformResponseErrors(t *testing.T) {
	h := newMsgHandler(t)
	for _, m := range []string{"CreateChannel", "ListChannels", "Subscribe", "GetStats", "GetDedupStats"} {
		if _, err := h.transformResponse(m, "scalar"); err == nil {
			t.Errorf("%s on scalar must error", m)
		}
	}
	if _, err := h.transformResponse("Bogus", nil); err == nil {
		t.Error("unsupported method response must error")
	}
}

func TestMessagingTransformRequestInvalidTypes(t *testing.T) {
	h := newMsgHandler(t)
	for _, m := range []string{"CreateChannel", "DeleteChannel", "GetChannel",
		"Subscribe", "Unsubscribe", "Publish", "PublishDelta"} {
		if _, err := h.transformRequest(m, "wrong"); err == nil {
			t.Errorf("%s with wrong type must error", m)
		}
	}
}

func TestEncodePayload(t *testing.T) {
	if encodePayload(nil) != "" {
		t.Error("empty payload should yield empty string")
	}
	if got := encodePayload([]byte(`{"a":1}`)); got != `{"a":1}` {
		t.Errorf("JSON payload should round-trip as text, got %q", got)
	}
	if got := encodePayload([]byte{0xff, 0xfe}); got != "//4=" {
		t.Errorf("binary payload base64 = %q", got)
	}
}
