//go:build mcp

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

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// newWatchToolRegistry returns a tool registry backed by svc.
func newWatchToolRegistry(t *testing.T, svc messaging.Service) *ToolRegistry {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	return NewToolRegistry(&extendedMockService{messagingSvc: svc}, log)
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

// TestMsgWatchChannelTool_ReturnsEveryField pins every field the watch tool
// returns, against the real in-memory messaging service.
func TestMsgWatchChannelTool_ReturnsEveryField(t *testing.T) {
	registry := newWatchToolRegistry(t, newWatchPushService(t))

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_msg_watch_channel", map[string]interface{}{
		"key_prefix":         "orders/",
		"session_id":         "sess-1",
		"buffer_size":        "8",
		"filter_key_prefix":  "orders/eu/",
		"filter_event_types": `["put","DELETE"]`,
	})
	require.NoError(t, err)
	got, ok := result.(map[string]interface{})
	require.True(t, ok, "result is %T, want a map", result)

	assert.NotEmpty(t, got["subscription_id"])
	assert.Equal(t, "watch:orders/", got["channel"])
	assert.Equal(t, "sess-1", got["client_id"])
	assert.Equal(t, "watch:orders/", got["channel_name"])
	for _, key := range []string{"created_at", "timestamp"} {
		value, isString := got[key].(string)
		require.True(t, isString, "%s is %T, want an RFC 3339 string", key, got[key])
		_, err := time.Parse(time.RFC3339, value)
		assert.NoError(t, err, "%s = %q", key, value)
	}
}

// TestMsgWatchChannelTool_Refusals pins that bad input is refused as invalid
// input rather than opening a watch.
func TestMsgWatchChannelTool_Refusals(t *testing.T) {
	registry := newWatchToolRegistry(t, newWatchPushService(t))

	t.Run("empty key prefix", func(t *testing.T) {
		_, err := registry.ExecuteTool(context.Background(), "quicbaas_msg_watch_channel",
			map[string]interface{}{"key_prefix": ""})
		var required *api.RequiredFieldError
		require.ErrorAs(t, err, &required)
		assert.Equal(t, "key_prefix", required.Field)
	})

	for name, eventTypes := range map[string]string{
		"malformed event types": `PUT`,
		"unknown event type":    `["RENAME"]`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := registry.ExecuteTool(context.Background(), "quicbaas_msg_watch_channel",
				map[string]interface{}{"key_prefix": "orders/", "filter_event_types": eventTypes})
			var invalid *api.InvalidInputError
			assert.ErrorAs(t, err, &invalid)
		})
	}
}

// TestMsgWatchChannelTool_RequiresWatchPermission pins the permission the
// tool is authorized under.
func TestMsgWatchChannelTool_RequiresWatchPermission(t *testing.T) {
	resource, action, ok := getToolQRDBPermission("quicbaas_msg_watch_channel")
	require.True(t, ok, "quicbaas_msg_watch_channel has no permission mapping")
	assert.Equal(t, "messaging:watch", resource+":"+action)
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

// TestMsgSubscribeTool_KeyPrefixReachesService pins that the subscribe tool's
// filter_key_prefix becomes the filter of the subscription the messaging
// service creates.
func TestMsgSubscribeTool_KeyPrefixReachesService(t *testing.T) {
	ctx := context.Background()
	svc := newWatchPushService(t)
	_, err := svc.CreateChannel(ctx, "orders")
	require.NoError(t, err)
	recorder := &subscriptionRecorder{Service: svc, subscriptions: make(chan *messaging.Subscription, 1)}
	registry := newWatchToolRegistry(t, recorder)

	_, err = registry.ExecuteTool(ctx, "quicbaas_msg_subscribe", map[string]interface{}{
		"channel":           "orders",
		"filter_key_prefix": "orders/eu/",
	})
	require.NoError(t, err)

	select {
	case sub := <-recorder.subscriptions:
		require.NotNil(t, sub.Filters)
		assert.Equal(t, "orders/eu/", sub.Filters.KeyPrefix)
	default:
		t.Fatal("the messaging service created no subscription")
	}
}
