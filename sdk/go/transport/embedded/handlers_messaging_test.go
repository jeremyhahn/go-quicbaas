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

package embedded

import (
	"context"
	stderrors "errors"
	"reflect"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/messagingwatch"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	pkgmessaging "github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage/file"
	messagingSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// messagingMock serves a messaging service; nil means none is configured.
type messagingMock struct {
	*mockQuicBaaS
	msg pkgmessaging.Service
}

func (m *messagingMock) Messaging() pkgmessaging.Service { return m.msg }

func newMessagingTransport(t *testing.T, msg pkgmessaging.Service) *Transport {
	t.Helper()
	tr, err := New(&messagingMock{mockQuicBaaS: &mockQuicBaaS{}, msg: msg})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return tr
}

// newPushService returns an in-memory push service; started controls whether
// it accepts requests.
func newPushService(t *testing.T, started bool) *pkgmessaging.PushService {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "console", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	ps := pkgmessaging.NewPushService(log, nil)
	if started {
		if err := ps.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		t.Cleanup(func() {
			if err := ps.Stop(context.Background()); err != nil {
				t.Errorf("Stop: %v", err)
			}
		})
	}
	return ps
}

// messagingFixture is the state each round-trip case starts from: a channel
// named "orders" with one subscription.
type messagingFixture struct {
	client messagingSDK.Client
	subID  string
}

func newMessagingFixture(t *testing.T) messagingFixture {
	t.Helper()
	client := messagingSDK.NewClient(newMessagingTransport(t, newPushService(t, true)))
	ctx := context.Background()
	if _, err := client.CreateChannel(ctx, &messagingSDK.CreateChannelRequest{Name: "orders"}); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	sub, err := client.Subscribe(ctx, &messagingSDK.SubscribeRequest{Channel: "orders"})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	return messagingFixture{client: client, subID: sub.ID}
}

// messagingCases calls every messaging.Client method except GetDedupStats,
// which needs a dedup-enabled service (see TestEmbeddedMessaging_GetDedupStats).
// fail drives the same method into a typed error that isWant must accept.
var messagingCases = []struct {
	method string
	call   func(ctx context.Context, f messagingFixture) (interface{}, error)
	check  func(t *testing.T, got interface{})
	fail   func(ctx context.Context, f messagingFixture) error
	isWant func(error) bool
}{
	{
		method: "CreateChannel",
		call: func(ctx context.Context, f messagingFixture) (interface{}, error) {
			return f.client.CreateChannel(ctx, &messagingSDK.CreateChannelRequest{Name: "audit",
				Codec: messagingSDK.CodecMsgpack, Persistent: true, MaxHistory: 10, BufferSize: 64, TTL: time.Minute})
		},
		check: func(t *testing.T, got interface{}) {
			ch := got.(*messagingSDK.Channel)
			if ch.Name != "audit" || ch.Codec != messagingSDK.CodecMsgpack || !ch.Persistent ||
				ch.MaxHistory != 10 || ch.CreatedAt.IsZero() {
				t.Errorf("channel = %+v, want the requested options applied", ch)
			}
		},
		fail: func(ctx context.Context, f messagingFixture) error {
			_, err := f.client.CreateChannel(ctx, &messagingSDK.CreateChannelRequest{Name: "orders"})
			return err
		},
		isWant: errors.IsAlreadyExists,
	},
	{
		method: "DeleteChannel",
		call: func(ctx context.Context, f messagingFixture) (interface{}, error) {
			return nil, f.client.DeleteChannel(ctx, "orders")
		},
		fail: func(ctx context.Context, f messagingFixture) error {
			return f.client.DeleteChannel(ctx, "missing")
		},
		isWant: errors.IsNotFound,
	},
	{
		method: "GetChannel",
		call: func(ctx context.Context, f messagingFixture) (interface{}, error) {
			return f.client.GetChannel(ctx, "orders")
		},
		check: func(t *testing.T, got interface{}) {
			if ch := got.(*messagingSDK.Channel); ch.Name != "orders" || ch.Subscribers != 1 {
				t.Errorf("channel = %+v, want orders with one subscriber", ch)
			}
		},
		fail: func(ctx context.Context, f messagingFixture) error {
			_, err := f.client.GetChannel(ctx, "missing")
			return err
		},
		isWant: errors.IsNotFound,
	},
	{
		method: "ListChannels",
		call: func(ctx context.Context, f messagingFixture) (interface{}, error) {
			return f.client.ListChannels(ctx)
		},
		check: func(t *testing.T, got interface{}) {
			channels := got.(*messagingSDK.ListChannelsResponse).Channels
			if len(channels) != 1 || channels[0].Name != "orders" {
				t.Errorf("channels = %+v, want only orders", channels)
			}
		},
	},
	{
		method: "Subscribe",
		call: func(ctx context.Context, f messagingFixture) (interface{}, error) {
			return f.client.Subscribe(ctx, &messagingSDK.SubscribeRequest{Channel: "orders",
				Filter: &messagingSDK.SubscriptionFilter{EventTypes: []messagingSDK.EventType{messagingSDK.EventTypeDelete}}})
		},
		check: func(t *testing.T, got interface{}) {
			sub := got.(*messagingSDK.Subscription)
			wantTypes := []messagingSDK.EventType{messagingSDK.EventTypeDelete}
			if sub.ID == "" || sub.Channel != "orders" || sub.Filters == nil ||
				!reflect.DeepEqual(sub.Filters.EventTypes, wantTypes) {
				t.Errorf("subscription = %+v, want one on orders filtered to deletes", sub)
			}
		},
		fail: func(ctx context.Context, f messagingFixture) error {
			_, err := f.client.Subscribe(ctx, &messagingSDK.SubscribeRequest{Channel: "invalid@channel#name"})
			return err
		},
		isWant: errors.IsInvalidInput,
	},
	{
		method: "Unsubscribe",
		call: func(ctx context.Context, f messagingFixture) (interface{}, error) {
			return nil, f.client.Unsubscribe(ctx, f.subID)
		},
		fail: func(ctx context.Context, f messagingFixture) error {
			return f.client.Unsubscribe(ctx, "missing")
		},
		isWant: errors.IsNotFound,
	},
	{
		method: "Publish",
		call: func(ctx context.Context, f messagingFixture) (interface{}, error) {
			return nil, f.client.Publish(ctx, &messagingSDK.PublishRequest{Channel: "orders", Payload: []byte(`{"id":1}`),
				MessageID: "m-1", Metadata: map[string]string{"k": "v"}})
		},
		fail: func(ctx context.Context, f messagingFixture) error {
			return f.client.Publish(ctx, &messagingSDK.PublishRequest{Channel: "missing", Payload: []byte("x")})
		},
		isWant: errors.IsNotFound,
	},
	{
		method: "PublishDelta",
		call: func(ctx context.Context, f messagingFixture) (interface{}, error) {
			return nil, f.client.PublishDelta(ctx, &messagingSDK.PublishDeltaRequest{Channel: "orders",
				Old: map[string]interface{}{"n": 1}, New: map[string]interface{}{"n": 2}})
		},
		fail: func(ctx context.Context, f messagingFixture) error {
			return f.client.PublishDelta(ctx, &messagingSDK.PublishDeltaRequest{Channel: "missing",
				Old: map[string]interface{}{"n": 1}, New: map[string]interface{}{"n": 2}})
		},
		isWant: errors.IsNotFound,
	},
	{
		method: "WatchChannel",
		call: func(ctx context.Context, f messagingFixture) (interface{}, error) {
			return f.client.WatchChannel(ctx, &messagingSDK.WatchChannelRequest{KeyPrefix: "users/"})
		},
		check: func(t *testing.T, got interface{}) {
			if sub := got.(*messagingSDK.Subscription); sub.ID == "" {
				t.Errorf("watch subscription = %+v, want an ID", sub)
			}
		},
	},
	{
		method: "GetStats",
		call: func(ctx context.Context, f messagingFixture) (interface{}, error) {
			return f.client.GetStats(ctx)
		},
		check: func(t *testing.T, got interface{}) {
			if stats := got.(*messagingSDK.Stats); stats.TotalChannels != 1 {
				t.Errorf("stats = %+v, want one channel", stats)
			}
		},
	},
	{
		method: "GetChannelStats",
		call: func(ctx context.Context, f messagingFixture) (interface{}, error) {
			return f.client.GetChannelStats(ctx, "orders")
		},
		check: func(t *testing.T, got interface{}) {
			if stats := got.(*messagingSDK.ChannelStats); stats.Name != "orders" || stats.Subscribers != 1 {
				t.Errorf("channel stats = %+v, want orders with one subscriber", stats)
			}
		},
		fail: func(ctx context.Context, f messagingFixture) error {
			_, err := f.client.GetChannelStats(ctx, "missing")
			return err
		},
		isWant: errors.IsNotFound,
	},
}

func TestEmbeddedMessaging_RoundTrip(t *testing.T) {
	ctx := context.Background()
	for _, tc := range messagingCases {
		t.Run(tc.method, func(t *testing.T) {
			got, err := tc.call(ctx, newMessagingFixture(t))
			if err != nil {
				t.Fatalf("%s: %v", tc.method, err)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
			if tc.fail == nil {
				return
			}
			if err := tc.fail(ctx, newMessagingFixture(t)); !tc.isWant(err) {
				t.Errorf("%s: err = %v, want a typed error", tc.method, err)
			}
		})
	}
}

// TestEmbeddedMessaging_StoppedService covers the methods without a
// not-found path: a stopped service reports Unavailable.
func TestEmbeddedMessaging_StoppedService(t *testing.T) {
	client := messagingSDK.NewClient(newMessagingTransport(t, newPushService(t, false)))
	ctx := context.Background()

	if _, err := client.ListChannels(ctx); !errors.IsUnavailable(err) {
		t.Errorf("ListChannels: err = %v, want an Unavailable error", err)
	}
	if _, err := client.WatchChannel(ctx, &messagingSDK.WatchChannelRequest{KeyPrefix: "users/"}); !errors.IsUnavailable(err) {
		t.Errorf("WatchChannel: err = %v, want an Unavailable error", err)
	}
}

// TestEmbeddedMessaging_NotConfigured pins that every method reports a
// missing messaging service instead of dereferencing it.
func TestEmbeddedMessaging_NotConfigured(t *testing.T) {
	tr := newMessagingTransport(t, nil)
	for method, req := range map[string]interface{}{
		"CreateChannel":   &messagingSDK.CreateChannelRequest{Name: "orders"},
		"DeleteChannel":   &messagingSDK.DeleteChannelRequest{Name: "orders"},
		"GetChannel":      &messagingSDK.GetChannelRequest{Name: "orders"},
		"ListChannels":    &messagingSDK.ListChannelsRequest{},
		"Subscribe":       &messagingSDK.SubscribeRequest{Channel: "orders"},
		"Unsubscribe":     &messagingSDK.UnsubscribeRequest{SubscriptionID: "s"},
		"Publish":         &messagingSDK.PublishRequest{Channel: "orders"},
		"PublishDelta":    &messagingSDK.PublishDeltaRequest{Channel: "orders"},
		"WatchChannel":    &messagingSDK.WatchChannelRequest{KeyPrefix: "k"},
		"GetStats":        &struct{}{},
		"GetDedupStats":   &messagingSDK.GetDedupStatsRequest{Channel: "orders"},
		"GetChannelStats": &struct{ Channel string }{Channel: "orders"},
	} {
		_, err := tr.Request(context.Background(), messagingServiceName, method, req)
		var notConfigured *ServiceNotConfiguredError
		if !stderrors.As(err, &notConfigured) {
			t.Errorf("%s: err = %v, want ServiceNotConfiguredError", method, err)
		}
	}
}

func TestEmbeddedMessaging_InvalidRequests(t *testing.T) {
	tr := newMessagingTransport(t, newPushService(t, true))
	for method := range messagingSDKMethods() {
		if method == "GetStats" {
			continue // GetStats takes no request fields, so any request is valid.
		}
		_, err := tr.Request(context.Background(), messagingServiceName, method, "bad")
		var badType *InvalidRequestTypeError
		if !stderrors.As(err, &badType) && !errors.IsInvalidInput(err) {
			t.Errorf("%s with a bad request: err = %v, want a request-type error", method, err)
		}
	}
}

// TestEmbeddedMessaging_GetDedupStats pins that the SDK client's
// GetDedupStats reports the named channel's own dedup figures, and that a
// missing or unknown channel is refused with a typed error.
func TestEmbeddedMessaging_GetDedupStats(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "console", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	storCfg := file.DefaultConfig()
	storCfg.DataDir = t.TempDir()
	storCfg.SyncWrites = false
	stor, err := file.New(storCfg, log)
	if err != nil {
		t.Fatalf("file.New: %v", err)
	}
	ps := pkgmessaging.NewPushService(log, stor, pkgmessaging.WithDedup(true))
	ctx := context.Background()
	if err := ps.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := ps.Stop(context.Background()); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	client := messagingSDK.NewClient(newMessagingTransport(t, ps))

	// orders sees one message three times; audit sees one message once.
	publishes := []struct{ channel, key string }{
		{"orders", "k-1"}, {"orders", "k-1"}, {"orders", "k-1"}, {"audit", "k-2"},
	}
	for _, name := range []string{"orders", "audit"} {
		if _, err := client.CreateChannel(ctx, &messagingSDK.CreateChannelRequest{Name: name}); err != nil {
			t.Fatalf("CreateChannel %s: %v", name, err)
		}
	}
	for _, p := range publishes {
		req := &messagingSDK.PublishRequest{Channel: p.channel, Payload: []byte(`{"id":1}`), DedupKey: p.key}
		if err := client.Publish(ctx, req); err != nil {
			t.Fatalf("Publish %s: %v", p.channel, err)
		}
	}

	got, err := client.GetDedupStats(ctx, "orders")
	if err != nil {
		t.Fatalf("GetDedupStats: %v", err)
	}
	want := messagingSDK.DedupStats{TotalChecks: 3, DuplicatesDetected: 2, UniqueMessages: 1}
	if *got != want {
		t.Errorf("GetDedupStats(orders) = %+v, want %+v", *got, want)
	}

	if _, err := client.GetDedupStats(ctx, ""); !errors.IsInvalidInput(err) {
		t.Errorf("GetDedupStats without a channel: err = %v, want an InvalidInput error", err)
	}
	if _, err := client.GetDedupStats(ctx, "missing"); !errors.IsNotFound(err) {
		t.Errorf("GetDedupStats for an unknown channel: err = %v, want a NotFound error", err)
	}
}

// TestEmbeddedMessaging_SubscribeKeepsKeyPrefix pins that an embedded
// subscriber's filter key prefix reaches the messaging service's subscription.
func TestEmbeddedMessaging_SubscribeKeepsKeyPrefix(t *testing.T) {
	fx := newMessagingFixture(t)

	sub, err := fx.client.Subscribe(context.Background(), &messagingSDK.SubscribeRequest{
		Channel: "orders",
		Filter:  &messagingSDK.SubscriptionFilter{KeyPrefix: "orders/eu/"},
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if sub.Filters == nil || sub.Filters.KeyPrefix != "orders/eu/" {
		t.Errorf("subscription filters = %+v, want key prefix orders/eu/", sub.Filters)
	}
}

// TestEmbeddedMessaging_WatchChannel pins that the embedded transport serves
// WatchChannel through the shared watch mapping: every request field reaches
// the service, and an empty key prefix is refused as invalid input.
func TestEmbeddedMessaging_WatchChannel(t *testing.T) {
	ctx := context.Background()
	client := messagingSDK.NewClient(newMessagingTransport(t, newPushService(t, true)))

	sub, err := client.WatchChannel(ctx, &messagingSDK.WatchChannelRequest{
		KeyPrefix: "orders/",
		SessionID: "sess-1",
		Filter: &messagingSDK.SubscriptionFilter{
			EventTypes: []messagingSDK.EventType{messagingSDK.EventTypeDelete},
			KeyPrefix:  "orders/eu/",
		},
	})
	if err != nil {
		t.Fatalf("WatchChannel: %v", err)
	}
	if sub.Channel != messagingwatch.ChannelName("orders/") || sub.ClientID != "sess-1" {
		t.Errorf("subscription = %+v, want channel %s and client sess-1", *sub, messagingwatch.ChannelName("orders/"))
	}
	wantFilter := &messagingSDK.SubscriptionFilter{
		EventTypes: []messagingSDK.EventType{messagingSDK.EventTypeDelete},
		KeyPrefix:  "orders/eu/",
	}
	if !reflect.DeepEqual(sub.Filters, wantFilter) {
		t.Errorf("subscription filters = %+v, want %+v", sub.Filters, wantFilter)
	}

	if _, err := client.WatchChannel(ctx, &messagingSDK.WatchChannelRequest{}); !errors.IsInvalidInput(err) {
		t.Errorf("WatchChannel without a key prefix: err = %v, want an InvalidInput error", err)
	}
}

// messagingSDKMethods lists the methods of messaging.Client.
func messagingSDKMethods() map[string]bool {
	clientType := reflect.TypeOf((*messagingSDK.Client)(nil)).Elem()
	methods := make(map[string]bool, clientType.NumMethod())
	for i := 0; i < clientType.NumMethod(); i++ {
		methods[clientType.Method(i).Name] = true
	}
	return methods
}

// TestEmbeddedMessaging_CoversClient fails when a messaging.Client method has
// no entry in the embedded handler table.
func TestEmbeddedMessaging_CoversClient(t *testing.T) {
	for method := range messagingSDKMethods() {
		if _, ok := unaryHandlers[messagingServiceName+"/"+method]; !ok {
			t.Errorf("messaging.Client.%s has no embedded handler", method)
		}
	}
}
