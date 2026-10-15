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

package grpc

import (
	"context"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// fakeMessagingServer answers every MessagingService RPC with fixed data, or
// with codes.NotFound when fail is set.
type fakeMessagingServer struct {
	pb.UnimplementedMessagingServiceServer
	fail bool
}

func (f *fakeMessagingServer) err() error {
	if f.fail {
		return status.Error(codes.NotFound, "channel not found")
	}
	return nil
}

func testProtoChannel(name string) *pb.Channel {
	return &pb.Channel{Name: name, Codec: "json", CreatedAt: 1700000000, Subscribers: 2,
		Persistent: true, MaxHistory: 10, BufferSize: 64, TtlSeconds: 30}
}

func testProtoSubscription(channel string) *pb.Subscription {
	return &pb.Subscription{Id: "sub-1", Channel: channel, ClientId: "client-1", CreatedAt: 1700000000,
		Filters: &pb.SubscriptionFilter{EventTypes: []pb.EventType{pb.EventType_EVENT_TYPE_DELETE}, KeyPrefix: "k/"}}
}

func (f *fakeMessagingServer) CreateChannel(_ context.Context, req *pb.MessagingCreateChannelRequest) (*pb.MessagingCreateChannelResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &pb.MessagingCreateChannelResponse{Channel: testProtoChannel(req.GetName())}, nil
}

func (f *fakeMessagingServer) DeleteChannel(context.Context, *pb.MessagingDeleteChannelRequest) (*pb.MessagingDeleteChannelResponse, error) {
	return &pb.MessagingDeleteChannelResponse{}, f.err()
}

func (f *fakeMessagingServer) GetChannel(_ context.Context, req *pb.MessagingGetChannelRequest) (*pb.MessagingGetChannelResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &pb.MessagingGetChannelResponse{Channel: testProtoChannel(req.GetName())}, nil
}

func (f *fakeMessagingServer) ListChannels(context.Context, *pb.MessagingListChannelsRequest) (*pb.MessagingListChannelsResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &pb.MessagingListChannelsResponse{Channels: []*pb.Channel{testProtoChannel("a"), testProtoChannel("b")}}, nil
}

func (f *fakeMessagingServer) Subscribe(_ context.Context, req *pb.MessagingSubscribeRequest) (*pb.MessagingSubscribeResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &pb.MessagingSubscribeResponse{Subscription: testProtoSubscription(req.GetChannel())}, nil
}

func (f *fakeMessagingServer) Unsubscribe(context.Context, *pb.MessagingUnsubscribeRequest) (*pb.MessagingUnsubscribeResponse, error) {
	return &pb.MessagingUnsubscribeResponse{}, f.err()
}

func (f *fakeMessagingServer) Publish(context.Context, *pb.MessagingPublishRequest) (*pb.MessagingPublishResponse, error) {
	return &pb.MessagingPublishResponse{}, f.err()
}

func (f *fakeMessagingServer) PublishDelta(context.Context, *pb.MessagingPublishDeltaRequest) (*pb.MessagingPublishDeltaResponse, error) {
	return &pb.MessagingPublishDeltaResponse{}, f.err()
}

func (f *fakeMessagingServer) WatchChannel(_ context.Context, req *pb.MessagingWatchChannelRequest) (*pb.MessagingWatchChannelResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &pb.MessagingWatchChannelResponse{Subscription: testProtoSubscription(req.GetKeyPrefix())}, nil
}

func (f *fakeMessagingServer) GetStats(context.Context, *pb.MessagingGetStatsRequest) (*pb.MessagingGetStatsResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &pb.MessagingGetStatsResponse{Stats: &pb.Stats{TotalChannels: 3, MessagesPublished: 7}}, nil
}

func (f *fakeMessagingServer) GetDedupStats(_ context.Context, req *pb.MessagingGetDedupStatsRequest) (*pb.MessagingGetDedupStatsResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &pb.MessagingGetDedupStatsResponse{Stats: &pb.DedupStats{Channel: req.GetChannel(), TotalMessages: 10, TotalDuplicates: 4}}, nil
}

func (f *fakeMessagingServer) GetChannelStats(_ context.Context, req *pb.MessagingGetChannelStatsRequest) (*pb.MessagingGetChannelStatsResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &pb.MessagingGetChannelStatsResponse{Stats: &pb.ChannelStats{Name: req.GetName(), Subscribers: 2, MessagesPublished: 5, MessagesInHistory: 3}}, nil
}

// messagingHandlerTransport routes the messaging client straight into a
// messagingHandler, so the tests exercise the client and the gRPC handler
// together over a real gRPC connection.
type messagingHandlerTransport struct{ h *messagingHandler }

func (m messagingHandlerTransport) Request(ctx context.Context, _, method string, req interface{}) (interface{}, error) {
	return m.h.handleRequest(ctx, method, req)
}

func (messagingHandlerTransport) RequestStream(context.Context, string, string, interface{}) (transport.Stream, error) {
	return nil, nil
}
func (messagingHandlerTransport) Conn() interface{} { return nil }
func (messagingHandlerTransport) Close() error      { return nil }
func (messagingHandlerTransport) Healthy() bool     { return true }

func newMessagingHandlerWithServer(t *testing.T, fail bool) *messagingHandler {
	t.Helper()
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterMessagingServiceServer(s, &fakeMessagingServer{fail: fail})
	})
	return newMessagingHandler(srv.dial(t))
}

// messagingRoundTripCases calls every messaging.Client method except
// GetDedupStats, whose request carries no channel (see
// TestMessagingHandler_GetDedupStats).
var messagingRoundTripCases = []struct {
	method string
	call   func(ctx context.Context, c messaging.Client) (interface{}, error)
	check  func(t *testing.T, got interface{})
}{
	{
		method: "CreateChannel",
		call: func(ctx context.Context, c messaging.Client) (interface{}, error) {
			return c.CreateChannel(ctx, &messaging.CreateChannelRequest{Name: "orders", Codec: messaging.CodecJSON,
				MaxHistory: 10, BufferSize: 64, TTL: 30 * time.Second})
		},
		check: func(t *testing.T, got interface{}) {
			ch := got.(*messaging.Channel)
			want := &messaging.Channel{Name: "orders", Codec: "json", CreatedAt: time.Unix(1700000000, 0),
				Subscribers: 2, Persistent: true, MaxHistory: 10, BufferSize: 64, TTL: 30 * time.Second}
			if !reflect.DeepEqual(ch, want) {
				t.Errorf("channel = %+v, want %+v", ch, want)
			}
		},
	},
	{
		method: "DeleteChannel",
		call: func(ctx context.Context, c messaging.Client) (interface{}, error) {
			return nil, c.DeleteChannel(ctx, "orders")
		},
	},
	{
		method: "GetChannel",
		call: func(ctx context.Context, c messaging.Client) (interface{}, error) {
			return c.GetChannel(ctx, "orders")
		},
		check: func(t *testing.T, got interface{}) {
			if name := got.(*messaging.Channel).Name; name != "orders" {
				t.Errorf("channel name = %q, want orders", name)
			}
		},
	},
	{
		method: "ListChannels",
		call: func(ctx context.Context, c messaging.Client) (interface{}, error) {
			return c.ListChannels(ctx)
		},
		check: func(t *testing.T, got interface{}) {
			channels := got.(*messaging.ListChannelsResponse).Channels
			if len(channels) != 2 || channels[0].Name != "a" || channels[1].Name != "b" {
				t.Errorf("channels = %+v, want a and b", channels)
			}
		},
	},
	{
		method: "Subscribe",
		call: func(ctx context.Context, c messaging.Client) (interface{}, error) {
			return c.Subscribe(ctx, &messaging.SubscribeRequest{Channel: "orders",
				Filter: &messaging.SubscriptionFilter{EventTypes: []messaging.EventType{messaging.EventTypeDelete}}})
		},
		check: func(t *testing.T, got interface{}) {
			sub := got.(*messaging.Subscription)
			want := &messaging.Subscription{ID: "sub-1", Channel: "orders", ClientID: "client-1",
				CreatedAt: time.Unix(1700000000, 0),
				Filters:   &messaging.SubscriptionFilter{EventTypes: []messaging.EventType{messaging.EventTypeDelete}, KeyPrefix: "k/"}}
			if !reflect.DeepEqual(sub, want) {
				t.Errorf("subscription = %+v, want %+v", sub, want)
			}
		},
	},
	{
		method: "Unsubscribe",
		call: func(ctx context.Context, c messaging.Client) (interface{}, error) {
			return nil, c.Unsubscribe(ctx, "sub-1")
		},
	},
	{
		method: "Publish",
		call: func(ctx context.Context, c messaging.Client) (interface{}, error) {
			return nil, c.Publish(ctx, &messaging.PublishRequest{Channel: "orders", Payload: []byte("hi")})
		},
	},
	{
		method: "PublishDelta",
		call: func(ctx context.Context, c messaging.Client) (interface{}, error) {
			return nil, c.PublishDelta(ctx, &messaging.PublishDeltaRequest{Channel: "orders", Old: 1, New: 2})
		},
	},
	{
		method: "WatchChannel",
		call: func(ctx context.Context, c messaging.Client) (interface{}, error) {
			return c.WatchChannel(ctx, &messaging.WatchChannelRequest{KeyPrefix: "users/"})
		},
		check: func(t *testing.T, got interface{}) {
			if ch := got.(*messaging.Subscription).Channel; ch != "users/" {
				t.Errorf("subscription channel = %q, want users/", ch)
			}
		},
	},
	{
		method: "GetStats",
		call: func(ctx context.Context, c messaging.Client) (interface{}, error) {
			return c.GetStats(ctx)
		},
		check: func(t *testing.T, got interface{}) {
			stats := got.(*messaging.Stats)
			if stats.TotalChannels != 3 || stats.MessagesPublished != 7 {
				t.Errorf("stats = %+v, want 3 channels and 7 published", stats)
			}
		},
	},
	{
		method: "GetChannelStats",
		call: func(ctx context.Context, c messaging.Client) (interface{}, error) {
			return c.GetChannelStats(ctx, "orders")
		},
		check: func(t *testing.T, got interface{}) {
			want := &messaging.ChannelStats{Name: "orders", Subscribers: 2, MessagesPublished: 5, MessagesInHistory: 3}
			if stats := got.(*messaging.ChannelStats); !reflect.DeepEqual(stats, want) {
				t.Errorf("channel stats = %+v, want %+v", stats, want)
			}
		},
	},
}

func TestMessagingHandler_RoundTrip(t *testing.T) {
	ok := messaging.NewClient(messagingHandlerTransport{newMessagingHandlerWithServer(t, false)})
	failing := messaging.NewClient(messagingHandlerTransport{newMessagingHandlerWithServer(t, true)})

	for _, tc := range messagingRoundTripCases {
		t.Run(tc.method, func(t *testing.T) {
			got, err := tc.call(context.Background(), ok)
			if err != nil {
				t.Fatalf("%s: %v", tc.method, err)
			}
			if tc.check != nil {
				tc.check(t, got)
			}

			if _, err := tc.call(context.Background(), failing); !errors.IsNotFound(err) {
				t.Errorf("%s against a failing server: err = %v, want a NotFound error", tc.method, err)
			}
		})
	}
}

// TestMessagingHandler_GetDedupStats pins that the handler serves dedup stats
// for a request that names a channel, and rejects one that does not — which
// is every request the SDK client sends today.
func TestMessagingHandler_GetDedupStats(t *testing.T) {
	h := newMessagingHandlerWithServer(t, false)

	got, err := h.handleRequest(context.Background(), "GetDedupStats", &struct{ Channel string }{Channel: "orders"})
	if err != nil {
		t.Fatalf("GetDedupStats: %v", err)
	}
	want := &messaging.DedupStats{TotalChecks: 10, DuplicatesDetected: 4, UniqueMessages: 6}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dedup stats = %+v, want %+v", got, want)
	}

	_, err = messaging.NewClient(messagingHandlerTransport{h}).GetDedupStats(context.Background())
	if !errors.IsInvalidInput(err) {
		t.Errorf("GetDedupStats without a channel: err = %v, want an InvalidInput error", err)
	}

	failing := newMessagingHandlerWithServer(t, true)
	_, err = failing.handleRequest(context.Background(), "GetDedupStats", &struct{ Channel string }{Channel: "orders"})
	if !errors.IsNotFound(err) {
		t.Errorf("GetDedupStats against a failing server: err = %v, want a NotFound error", err)
	}
}

func TestMessagingHandler_InvalidRequests(t *testing.T) {
	h := newMessagingHandlerWithServer(t, false)

	for method := range messagingMethods {
		if method == "GetStats" {
			continue // GetStats takes no request fields, so any request is valid.
		}
		t.Run(method, func(t *testing.T) {
			if _, err := h.handleRequest(context.Background(), method, "bad"); !errors.IsInvalidInput(err) {
				t.Errorf("%s with a bad request: err = %v, want an InvalidInput error", method, err)
			}
		})
	}

	if _, err := h.handleRequest(context.Background(), "NoSuchMethod", nil); !errors.IsUnimplemented(err) {
		t.Errorf("unknown method: err = %v, want an Unimplemented error", err)
	}
}

// TestMessagingHandler_CoversClient fails when a messaging.Client method has
// no entry in the gRPC dispatch table.
func TestMessagingHandler_CoversClient(t *testing.T) {
	clientType := reflect.TypeOf((*messaging.Client)(nil)).Elem()
	for i := 0; i < clientType.NumMethod(); i++ {
		name := clientType.Method(i).Name
		if _, ok := messagingMethods[name]; !ok {
			t.Errorf("messaging.Client.%s has no gRPC handler", name)
		}
	}
	if len(messagingMethods) != clientType.NumMethod() {
		t.Errorf("gRPC table has %d methods, messaging.Client has %d", len(messagingMethods), clientType.NumMethod())
	}
}
