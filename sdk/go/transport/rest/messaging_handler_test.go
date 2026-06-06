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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	messagingclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
)

func TestMessagingRESTHandler_CreateChannel_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/messaging/channels" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body messagingCreateChannelRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Name != "events" {
			t.Errorf("expected name 'events', got %q", body.Name)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(messagingChannelEnvelope{
			Channel: &messagingChannelWire{Name: "events", Codec: "json", CreatedAt: 1700000000, Subscribers: 3, TTLSeconds: 60},
		})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Messaging", "CreateChannel", &messagingclient.CreateChannelRequest{
		Name:  "events",
		Codec: "json",
	})
	if err != nil {
		t.Fatalf("CreateChannel failed: %v", err)
	}
	ch := result.(*messagingclient.Channel)
	if ch.Name != "events" || ch.Subscribers != 3 {
		t.Errorf("unexpected channel: %+v", ch)
	}
}

func TestMessagingRESTHandler_GetChannel_SubstitutesName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/messaging/channels/orders" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(messagingChannelEnvelope{Channel: &messagingChannelWire{Name: "orders"}})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Messaging", "GetChannel", &struct{ Name string }{Name: "orders"})
	if err != nil {
		t.Fatalf("GetChannel failed: %v", err)
	}
	if result.(*messagingclient.Channel).Name != "orders" {
		t.Errorf("unexpected channel name")
	}
}

func TestMessagingRESTHandler_Publish_SubstitutesChannelAndReturnsNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/messaging/publish/topic" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"published":true}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Messaging", "Publish", &messagingclient.PublishRequest{
		Channel: "topic",
		Payload: []byte(`{"k":"v"}`),
	})
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestMessagingRESTHandler_GetStats_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/messaging/stats" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(messagingStatsWire{TotalChannels: 5, MessagesPublished: 42})
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Messaging", "GetStats", &struct{}{})
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	stats := result.(*messagingclient.Stats)
	if stats.TotalChannels != 5 || stats.MessagesPublished != 42 {
		t.Errorf("unexpected stats: %+v", stats)
	}
}

func TestMessagingRESTHandler_CreateChannel_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"exists","code":"conflict"}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Messaging", "CreateChannel", &messagingclient.CreateChannelRequest{Name: "x"})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

func TestMessagingRESTHandler_GetChannel_MalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{bad`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Messaging", "GetChannel", &struct{ Name string }{Name: "x"})
	if err == nil {
		t.Fatal("expected decode error")
	}
}
