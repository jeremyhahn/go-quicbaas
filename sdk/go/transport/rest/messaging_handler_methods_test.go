// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	messagingclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
)

// messagingChannelStatsReq mirrors the internal request the SDK builds for
// GetChannelStats / GetDedupStats: a struct exposing an exported Channel field.
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
		case "/api/v1/messaging/subscribe":
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
			&messagingChannelStatsReq{Channel: "c1"},
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
		{"UnsubscribeNoID", "Unsubscribe", &messagingclient.UnsubscribeRequest{}},
		{"PublishDeltaBadType", "PublishDelta", 1},
		{"PublishDeltaNoChannel", "PublishDelta", &messagingclient.PublishDeltaRequest{}},
		{"GetChannelStatsNoChannel", "GetChannelStats", &messagingChannelStatsReq{}},
		{"GetDedupStatsNoChannel", "GetDedupStats", &messagingChannelStatsReq{}},
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
