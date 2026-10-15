// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	messagingclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// messagingChannelStatsReq mirrors the internal request the SDK builds for
// GetChannelStats: a struct exposing an exported Channel field.
type messagingChannelStatsReq struct {
	Channel string
}

// TestMessagingHandlerMethodsHappyPaths drives the Messaging methods that lacked
// coverage, asserting the HTTP method and path each builds.
func TestMessagingHandlerMethodsHappyPaths(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/messaging/subscribe", "/api/v1/messaging/watch":
			_, _ = w.Write([]byte(`{"subscription":{"id":"s1","channel":"c1","client_id":"cl1","created_at":1700000000}}`))
		case "/api/v1/messaging/channels":
			_, _ = w.Write([]byte(`{"channels":[{"name":"c1","codec":"json","created_at":1700000000}],"total":1}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	cases := []struct {
		name       string
		method     string
		req        interface{}
		wantMethod string
		wantPath   string
	}{
		{"ListChannels", "ListChannels",
			&messagingclient.ListChannelsRequest{},
			http.MethodGet, "/api/v1/messaging/channels"},
		{"DeleteChannel", "DeleteChannel",
			&messagingclient.DeleteChannelRequest{Name: "c1"},
			http.MethodDelete, "/api/v1/messaging/channels/c1"},
		{"Subscribe", "Subscribe",
			&messagingclient.SubscribeRequest{Channel: "c1", SessionID: "sess", BufferSize: 8},
			http.MethodPost, "/api/v1/messaging/subscribe"},
		{"WatchChannel", "WatchChannel",
			&messagingclient.WatchChannelRequest{KeyPrefix: "orders/"},
			http.MethodPost, "/api/v1/messaging/watch"},
		{"Unsubscribe", "Unsubscribe",
			&messagingclient.UnsubscribeRequest{SubscriptionID: "s1"},
			http.MethodDelete, "/api/v1/messaging/subscribe/s1"},
		{"PublishDelta", "PublishDelta",
			&messagingclient.PublishDeltaRequest{Channel: "c1", Old: map[string]int{"v": 1}, New: map[string]int{"v": 2}},
			http.MethodPost, "/api/v1/messaging/publish-delta/c1"},
		{"GetChannelStats", "GetChannelStats",
			&messagingChannelStatsReq{Channel: "c1"},
			http.MethodGet, "/api/v1/messaging/stats/c1"},
		{"GetDedupStats", "GetDedupStats",
			&messagingclient.GetDedupStatsRequest{Channel: "c1"},
			http.MethodGet, "/api/v1/messaging/dedup/stats/c1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotMethod, gotPath = "", ""
			_, err := tr.Request(ctx, "Messaging", tc.method, tc.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotMethod != tc.wantMethod {
				t.Errorf("HTTP method: want %s, got %s", tc.wantMethod, gotMethod)
			}
			if gotPath != tc.wantPath {
				t.Errorf("path: want %s, got %s", tc.wantPath, gotPath)
			}
		})
	}
}

// TestMessagingHandlerMethodsValidation exercises the request-required and
// field-required guards on the previously-uncovered methods.
func TestMessagingHandlerMethodsValidation(t *testing.T) {
	tr := newMappingTransport(t)
	ctx := context.Background()

	cases := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"DeleteChannelNoName", "DeleteChannel", &messagingclient.DeleteChannelRequest{}},
		{"SubscribeBadType", "Subscribe", "x"},
		{"SubscribeNoChannel", "Subscribe", &messagingclient.SubscribeRequest{}},
		{"WatchChannelBadType", "WatchChannel", "x"},
		{"WatchChannelNoKeyPrefix", "WatchChannel", &messagingclient.WatchChannelRequest{}},
		{"UnsubscribeNoID", "Unsubscribe", &messagingclient.UnsubscribeRequest{}},
		{"PublishDeltaBadType", "PublishDelta", 1},
		{"PublishDeltaNoChannel", "PublishDelta", &messagingclient.PublishDeltaRequest{}},
		{"GetChannelStatsNoChannel", "GetChannelStats", &messagingChannelStatsReq{}},
		{"GetDedupStatsNoChannel", "GetDedupStats", &messagingclient.GetDedupStatsRequest{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tr.Request(ctx, "Messaging", tc.method, tc.req)
			if err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

// TestMessagingHandlerSubscribeNoSubscription verifies the no-subscription
// server-response error path.
func TestMessagingHandlerSubscribeNoSubscription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), "Messaging", "Subscribe",
		&messagingclient.SubscribeRequest{Channel: "c1"})
	if err == nil {
		t.Fatal("expected error when server returns no subscription")
	}
}

// TestMessagingClientGetDedupStats pins that the SDK client's GetDedupStats
// reaches the per-channel REST route and returns that channel's figures, and
// that a missing or unknown channel is refused with a typed error.
func TestMessagingClientGetDedupStats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/messaging/dedup/stats/orders" {
			w.WriteHeader(http.StatusNotFound)
			if _, err := w.Write([]byte(`{"error":"channel not found"}`)); err != nil {
				t.Errorf("write: %v", err)
			}
			return
		}
		if _, err := w.Write([]byte(`{"channel":"orders","total_messages":10,"total_duplicates":4}`)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer server.Close()

	client := messagingclient.NewClient(newTestTransport(t, server))
	ctx := context.Background()

	got, err := client.GetDedupStats(ctx, "orders")
	if err != nil {
		t.Fatalf("GetDedupStats: %v", err)
	}
	// The server reports 10 unique messages and 4 duplicates.
	want := messagingclient.DedupStats{TotalChecks: 14, DuplicatesDetected: 4, UniqueMessages: 10}
	if *got != want {
		t.Errorf("dedup stats = %+v, want %+v", *got, want)
	}

	if _, err := client.GetDedupStats(ctx, ""); !errors.IsInvalidInput(err) {
		t.Errorf("GetDedupStats without a channel: err = %v, want an InvalidInput error", err)
	}
	if _, err := client.GetDedupStats(ctx, "missing"); !errors.IsNotFound(err) {
		t.Errorf("GetDedupStats for an unknown channel: err = %v, want a NotFound error", err)
	}
}

// TestMessagingClientWatchChannel pins the watch round trip through the SDK
// client: every request field reaches the server's wire form, and every
// subscription field the server returns reaches the caller.
func TestMessagingClientWatchChannel(t *testing.T) {
	var got messagingWatchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/messaging/watch" {
			t.Errorf("request = %s %s, want POST /api/v1/messaging/watch", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		body := `{"subscription":{"id":"watch-1","channel":"watch:orders/","client_id":"sess-1",` +
			`"created_at":1700000000},"channel_name":"watch:orders/","timestamp":1700000001}`
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer server.Close()

	client := messagingclient.NewClient(newTestTransport(t, server))
	sub, err := client.WatchChannel(context.Background(), &messagingclient.WatchChannelRequest{
		KeyPrefix:  "orders/",
		SessionID:  "sess-1",
		BufferSize: 8,
		Filter: &messagingclient.SubscriptionFilter{
			EventTypes: []messagingclient.EventType{messagingclient.EventTypeDelete},
			KeyPrefix:  "orders/eu/",
		},
	})
	if err != nil {
		t.Fatalf("WatchChannel: %v", err)
	}

	wantReq := messagingWatchRequest{
		KeyPrefix:  "orders/",
		SessionID:  "sess-1",
		BufferSize: 8,
		Filter:     &messagingFilterWire{EventTypes: []string{"DELETE"}, KeyPrefix: "orders/eu/"},
	}
	if !reflect.DeepEqual(got, wantReq) {
		t.Errorf("request = %+v, want %+v", got, wantReq)
	}
	wantSub := messagingclient.Subscription{
		ID:        "watch-1",
		Channel:   "watch:orders/",
		ClientID:  "sess-1",
		CreatedAt: time.Unix(1700000000, 0),
	}
	if sub.ID != wantSub.ID || sub.Channel != wantSub.Channel || sub.ClientID != wantSub.ClientID ||
		!sub.CreatedAt.Equal(wantSub.CreatedAt) {
		t.Errorf("subscription = %+v, want %+v", *sub, wantSub)
	}
}

// TestMessagingClientSubscribeKeepsKeyPrefix pins that Subscribe sends the
// filter's key prefix as the subscribe route's key_prefix field, so a REST
// subscriber gets the same filtered stream a gRPC or MCP subscriber does.
func TestMessagingClientSubscribeKeepsKeyPrefix(t *testing.T) {
	var got map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/messaging/subscribe" {
			t.Errorf("request = %s %s, want POST /api/v1/messaging/subscribe", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		body := `{"subscription":{"id":"sub-1","channel":"orders","client_id":"sess-1",` +
			`"created_at":1700000000},"timestamp":1700000001}`
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer server.Close()

	client := messagingclient.NewClient(newTestTransport(t, server))
	_, err := client.Subscribe(context.Background(), &messagingclient.SubscribeRequest{
		Channel:   "orders",
		SessionID: "sess-1",
		Filter: &messagingclient.SubscriptionFilter{
			EventTypes: []messagingclient.EventType{messagingclient.EventTypePut},
			KeyPrefix:  "orders/eu/",
		},
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	want := map[string]interface{}{
		"channel":     "orders",
		"session_id":  "sess-1",
		"event_types": []interface{}{"PUT"},
		"key_prefix":  "orders/eu/",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("request = %+v, want %+v", got, want)
	}
}

// TestMessagingClientWatchChannelRefusesEmptyKeyPrefix pins that an empty key
// prefix is refused as invalid input before any request is sent.
func TestMessagingClientWatchChannelRefusesEmptyKeyPrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := messagingclient.NewClient(newTestTransport(t, server))
	_, err := client.WatchChannel(context.Background(), &messagingclient.WatchChannelRequest{})
	if !errors.IsInvalidInput(err) {
		t.Errorf("err = %v, want an InvalidInput error", err)
	}
}
