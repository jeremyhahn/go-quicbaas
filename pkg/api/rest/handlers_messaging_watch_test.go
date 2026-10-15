//go:build rest

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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// serveMessagingWatch posts body to the watch handler of a server backed by
// svc and returns the recorded response.
func serveMessagingWatch(t *testing.T, svc messaging.Service, body string) *httptest.ResponseRecorder {
	t.Helper()
	server, mock, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	mock.On("Messaging").Return(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/watch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.HandleMessagingWatch(w, req)
	return w
}

// newWatchPushService starts an in-memory messaging service for one test.
func newWatchPushService(t *testing.T) *messaging.PushService {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	svc := messaging.NewPushService(log, nil)
	ctx := context.Background()
	require.NoError(t, svc.Start(ctx))
	t.Cleanup(func() {
		assert.NoError(t, svc.Stop(ctx))
	})
	return svc
}

// TestHandleMessagingWatch_Success pins every response field the watch
// returns, against the real in-memory messaging service.
func TestHandleMessagingWatch_Success(t *testing.T) {
	body := `{"key_prefix":"orders/","session_id":"sess-1","buffer_size":8,` +
		`"filter":{"event_types":["put"],"key_prefix":"orders/eu/"}}`
	w := serveMessagingWatch(t, newWatchPushService(t), body)
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	var resp MessagingWatchRESTResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.Subscription)
	assert.NotEmpty(t, resp.Subscription.ID)
	assert.Equal(t, "watch:orders/", resp.Subscription.Channel)
	assert.Equal(t, "sess-1", resp.Subscription.ClientID)
	assert.NotZero(t, resp.Subscription.CreatedAt)
	assert.Equal(t, "watch:orders/", resp.ChannelName)
	assert.NotZero(t, resp.Timestamp)
}

// TestHandleMessagingWatch_Refusals pins the refusals the handler answers
// before or instead of opening a watch.
func TestHandleMessagingWatch_Refusals(t *testing.T) {
	cases := []struct {
		name string
		svc  messaging.Service
		body string
		want int
	}{
		{"empty key prefix", &stubMessagingService{}, `{"key_prefix":""}`, http.StatusBadRequest},
		{"invalid JSON body", &stubMessagingService{}, `{invalid`, http.StatusBadRequest},
		{"unknown event type", &stubMessagingService{},
			`{"key_prefix":"orders/","filter":{"event_types":["RENAME"]}}`, http.StatusBadRequest},
		{"messaging not configured", nil, `{"key_prefix":"orders/"}`, http.StatusNotImplemented},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := serveMessagingWatch(t, tc.svc, tc.body)
			assert.Equal(t, tc.want, w.Code, "body: %s", w.Body.String())
		})
	}
}

// subscriptionRecorder hands the test every subscription the wrapped
// messaging service creates through Subscribe.
type subscriptionRecorder struct {
	messaging.Service
	subscriptions chan *messaging.Subscription
}

func (r *subscriptionRecorder) Subscribe(ctx context.Context, channel string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
	sub, err := r.Service.Subscribe(ctx, channel, opts...)
	if err == nil {
		r.subscriptions <- sub
	}
	return sub, err
}

// TestHandleMessagingSubscribe_KeyPrefixReachesService pins that the subscribe
// route's key_prefix becomes the filter of the subscription the messaging
// service creates.
func TestHandleMessagingSubscribe_KeyPrefixReachesService(t *testing.T) {
	svc := newWatchPushService(t)
	_, err := svc.CreateChannel(context.Background(), "orders")
	require.NoError(t, err)
	recorder := &subscriptionRecorder{Service: svc, subscriptions: make(chan *messaging.Subscription, 1)}

	server, mock, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	mock.On("Messaging").Return(recorder)
	body := `{"channel":"orders","event_types":["PUT"],"key_prefix":"orders/eu/"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.HandleMessagingSubscribe(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	select {
	case sub := <-recorder.subscriptions:
		require.NotNil(t, sub.Filters)
		assert.Equal(t, "orders/eu/", sub.Filters.KeyPrefix)
		assert.Equal(t, []messaging.EventType{messaging.EventTypePut}, sub.Filters.EventTypes)
	default:
		t.Fatal("the messaging service created no subscription")
	}
}
