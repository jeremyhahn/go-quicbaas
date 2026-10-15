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

package quic

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// messagingRoute is the REST route a messaging method reaches over HTTP/3,
// and the JSON the test server answers it with.
type messagingRoute struct {
	method, path, body string
}

// messagingRoutes maps every messaging.Client method QUIC serves to its route.
var messagingRoutes = map[string]messagingRoute{
	"CreateChannel": {http.MethodPost, "/api/v1/messaging/channels", `{"channel":{"name":"orders","codec":"json","created_at":1700000000}}`},
	"DeleteChannel": {http.MethodDelete, "/api/v1/messaging/channels/orders", `{}`},
	"GetChannel":    {http.MethodGet, "/api/v1/messaging/channels/orders", `{"channel":{"name":"orders","subscribers":2}}`},
	"ListChannels":  {http.MethodGet, "/api/v1/messaging/channels", `{"channels":[{"name":"orders"}],"total":1}`},
	"Subscribe":     {http.MethodPost, "/api/v1/messaging/subscribe", `{"subscription":{"id":"sub-1","channel":"orders"}}`},
	"WatchChannel": {http.MethodPost, "/api/v1/messaging/watch", `{"subscription":{"id":"sub-1","channel":"watch:orders/",` +
		`"client_id":"sess-1","created_at":1700000000},"channel_name":"watch:orders/","timestamp":1700000001}`},
	"Unsubscribe":     {http.MethodDelete, "/api/v1/messaging/subscribe/sub-1", `{}`},
	"Publish":         {http.MethodPost, "/api/v1/messaging/publish/orders", `{}`},
	"PublishDelta":    {http.MethodPost, "/api/v1/messaging/publish-delta/orders", `{}`},
	"GetStats":        {http.MethodGet, "/api/v1/messaging/stats", `{"total_channels":3}`},
	"GetChannelStats": {http.MethodGet, "/api/v1/messaging/stats/orders", `{"name":"orders","subscribers":2}`},
	"GetDedupStats":   {http.MethodGet, "/api/v1/messaging/dedup/stats/orders", `{"channel":"orders","total_messages":10,"total_duplicates":4}`},
}

// messagingCalls invokes each served method through the SDK client.
var messagingCalls = map[string]func(ctx context.Context, tr *Transport) (interface{}, error){
	"CreateChannel": func(ctx context.Context, tr *Transport) (interface{}, error) {
		return messaging.NewClient(tr).CreateChannel(ctx, &messaging.CreateChannelRequest{Name: "orders"})
	},
	"DeleteChannel": func(ctx context.Context, tr *Transport) (interface{}, error) {
		return nil, messaging.NewClient(tr).DeleteChannel(ctx, "orders")
	},
	"GetChannel": func(ctx context.Context, tr *Transport) (interface{}, error) {
		return messaging.NewClient(tr).GetChannel(ctx, "orders")
	},
	"ListChannels": func(ctx context.Context, tr *Transport) (interface{}, error) {
		return messaging.NewClient(tr).ListChannels(ctx)
	},
	"Subscribe": func(ctx context.Context, tr *Transport) (interface{}, error) {
		return messaging.NewClient(tr).Subscribe(ctx, &messaging.SubscribeRequest{Channel: "orders"})
	},
	"WatchChannel": func(ctx context.Context, tr *Transport) (interface{}, error) {
		return messaging.NewClient(tr).WatchChannel(ctx, &messaging.WatchChannelRequest{KeyPrefix: "orders/"})
	},
	"Unsubscribe": func(ctx context.Context, tr *Transport) (interface{}, error) {
		return nil, messaging.NewClient(tr).Unsubscribe(ctx, "sub-1")
	},
	"Publish": func(ctx context.Context, tr *Transport) (interface{}, error) {
		return nil, messaging.NewClient(tr).Publish(ctx, &messaging.PublishRequest{Channel: "orders", Payload: []byte(`{"id":1}`)})
	},
	"PublishDelta": func(ctx context.Context, tr *Transport) (interface{}, error) {
		return nil, messaging.NewClient(tr).PublishDelta(ctx, &messaging.PublishDeltaRequest{Channel: "orders", Old: 1, New: 2})
	},
	"GetStats": func(ctx context.Context, tr *Transport) (interface{}, error) {
		return messaging.NewClient(tr).GetStats(ctx)
	},
	"GetChannelStats": func(ctx context.Context, tr *Transport) (interface{}, error) {
		return messaging.NewClient(tr).GetChannelStats(ctx, "orders")
	},
	"GetDedupStats": func(ctx context.Context, tr *Transport) (interface{}, error) {
		return messaging.NewClient(tr).GetDedupStats(ctx, "orders")
	},
}

// newMessagingHTTP3Transport starts an HTTP/3 server that answers every
// messaging route, or 404s every request when fail is set.
func newMessagingHTTP3Transport(t *testing.T, fail bool) (*Transport, <-chan messagingRoute) {
	t.Helper()
	hits := make(chan messagingRoute, 1)
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits <- messagingRoute{method: r.Method, path: r.URL.Path}
		w.Header().Set("Content-Type", "application/json")
		if fail {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"channel not found"}`))
			return
		}
		for _, route := range messagingRoutes {
			if route.method == r.Method && route.path == r.URL.Path {
				_, _ = w.Write([]byte(route.body))
				return
			}
		}
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(shutdown)

	tr, err := New(testTransportConfig(addr))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })
	return tr, hits
}

// TestRequest_MessagingOverHTTP3 round-trips every served messaging method:
// the request reaches its REST route, and a 404 surfaces as a NotFound error.
func TestRequest_MessagingOverHTTP3(t *testing.T) {
	ok, okHits := newMessagingHTTP3Transport(t, false)
	failing, failHits := newMessagingHTTP3Transport(t, true)
	ctx := context.Background()

	for method, call := range messagingCalls {
		t.Run(method, func(t *testing.T) {
			resp, err := call(ctx, ok)
			require.NoError(t, err)
			want := messagingRoutes[method]
			assert.Equal(t, messagingRoute{method: want.method, path: want.path}, <-okHits)

			switch r := resp.(type) {
			case *messaging.Channel:
				assert.Equal(t, "orders", r.Name)
			case *messaging.ListChannelsResponse:
				require.Len(t, r.Channels, 1)
				assert.Equal(t, "orders", r.Channels[0].Name)
			case *messaging.Subscription:
				assert.Equal(t, "sub-1", r.ID)
			case *messaging.Stats:
				assert.Equal(t, int64(3), r.TotalChannels)
			case *messaging.ChannelStats:
				assert.Equal(t, int64(2), r.Subscribers)
			case *messaging.DedupStats:
				// The route reports 10 unique messages and 4 duplicates.
				assert.Equal(t, messaging.DedupStats{TotalChecks: 14, DuplicatesDetected: 4, UniqueMessages: 10}, *r)
			}

			_, err = call(ctx, failing)
			<-failHits
			assert.True(t, errors.IsNotFound(err), "%s: err = %v, want a NotFound error", method, err)
		})
	}
}

// TestRequest_MessagingDedupStatsNeedsChannel pins that GetDedupStats without
// a channel is refused with a typed error before anything goes on the wire.
func TestRequest_MessagingDedupStatsNeedsChannel(t *testing.T) {
	tr, hits := newMessagingHTTP3Transport(t, false)

	_, err := messaging.NewClient(tr).GetDedupStats(context.Background(), "")
	assert.True(t, errors.IsInvalidInput(err), "err = %v, want an InvalidInput error", err)
	assert.Empty(t, hits, "a request without a channel reached the server")
}

// TestRequest_MessagingWatchChannelOverHTTP3 pins that every subscription
// field the watch route returns reaches the SDK caller over HTTP/3.
func TestRequest_MessagingWatchChannelOverHTTP3(t *testing.T) {
	tr, hits := newMessagingHTTP3Transport(t, false)

	sub, err := messaging.NewClient(tr).WatchChannel(context.Background(),
		&messaging.WatchChannelRequest{KeyPrefix: "orders/", SessionID: "sess-1"})
	require.NoError(t, err)
	assert.Equal(t, messagingRoute{method: http.MethodPost, path: "/api/v1/messaging/watch"}, <-hits)

	assert.Equal(t, "sub-1", sub.ID)
	assert.Equal(t, "watch:orders/", sub.Channel)
	assert.Equal(t, "sess-1", sub.ClientID)
	assert.True(t, sub.CreatedAt.Equal(time.Unix(1700000000, 0)), "created_at = %v", sub.CreatedAt)
}

// TestRequest_MessagingWatchChannelNeedsKeyPrefix pins that a watch without
// a key prefix is refused as invalid input before it reaches the server.
func TestRequest_MessagingWatchChannelNeedsKeyPrefix(t *testing.T) {
	tr, hits := newMessagingHTTP3Transport(t, false)

	_, err := messaging.NewClient(tr).WatchChannel(context.Background(), &messaging.WatchChannelRequest{})
	assert.True(t, errors.IsInvalidInput(err), "err = %v, want an InvalidInput error", err)
	assert.Empty(t, hits, "a watch without a key prefix reached the server")
}

// TestRequest_MessagingSubscribeKeepsKeyPrefixOverHTTP3 pins that a QUIC
// subscriber's filter key prefix reaches the subscribe route, as it does on
// gRPC and MCP.
func TestRequest_MessagingSubscribeKeepsKeyPrefixOverHTTP3(t *testing.T) {
	bodies := make(chan map[string]interface{}, 1)
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		bodies <- body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"subscription":{"id":"sub-1","channel":"orders","client_id":"client-1","created_at":1700000000}}`))
	}))
	t.Cleanup(shutdown)
	tr, err := New(testTransportConfig(addr))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })

	_, err = messaging.NewClient(tr).Subscribe(context.Background(), &messaging.SubscribeRequest{
		Channel: "orders",
		Filter:  &messaging.SubscriptionFilter{KeyPrefix: "orders/eu/"},
	})
	require.NoError(t, err)
	assert.Equal(t, "orders/eu/", (<-bodies)["key_prefix"])
}

// TestRequest_MessagingOverHTTP3CoversClient fails when a messaging.Client
// method has neither a QUIC route nor a recorded reason it cannot have one.
func TestRequest_MessagingOverHTTP3CoversClient(t *testing.T) {
	clientType := reflect.TypeOf((*messaging.Client)(nil)).Elem()
	for i := 0; i < clientType.NumMethod(); i++ {
		name := clientType.Method(i).Name
		_, routed := messagingRoutes[name]
		_, called := messagingCalls[name]
		assert.True(t, routed && called, "messaging.Client.%s has no QUIC route", name)
	}
}
