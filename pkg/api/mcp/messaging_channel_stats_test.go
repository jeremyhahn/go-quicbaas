//go:build mcp

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

	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// TestHandleMsgChannelStats_ChannelFigures drives the tool against a real
// push service: a known channel reports its figures and a missing one is
// refused as NotFound.
func TestHandleMsgChannelStats_ChannelFigures(t *testing.T) {
	log := newTestLogger(t)
	ps := messaging.NewPushService(log, nil)
	ctx := context.Background()
	require.NoError(t, ps.Start(ctx))
	t.Cleanup(func() {
		assert.NoError(t, ps.Stop(context.Background()))
	})

	_, err := ps.CreateChannel(ctx, "orders")
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err := ps.Publish(ctx, "orders", map[string]int{"id": i})
		require.NoError(t, err)
	}

	reg := &ToolRegistry{service: &mockService{}, logger: log}
	// Publish returns once the hub has queued the message, and the hub
	// counts it on its own loop, so the count is read until it arrives.
	var stats map[string]interface{}
	require.Eventually(t, func() bool {
		result, err := reg.handleMsgChannelStats(ctx, ps, map[string]interface{}{"channel": "orders"})
		if !assert.NoError(t, err) {
			return false
		}
		got, ok := result.(map[string]interface{})
		if !assert.True(t, ok, "result is %T, want a map", result) {
			return false
		}
		stats = got
		return stats["messages_published"] == int64(2)
	}, 5*time.Second, 10*time.Millisecond, "messages_published never reached 2")
	assert.Equal(t, "orders", stats["name"])
	assert.Equal(t, int64(0), stats["subscribers"])
	assert.Equal(t, int64(2), stats["messages_published"])

	// The push service's own not-found error is the typed one, not the
	// sentinel; it must still reach MCP clients as NotFound.
	_, err = reg.handleMsgChannelStats(ctx, ps, map[string]interface{}{"channel": "missing"})
	require.Error(t, err)
	assert.Equal(t, mcptransport.MCPResourceNotFound, toolErrorCode(err))
}

func TestHandleMsgChannelStats_MissingChannel(t *testing.T) {
	reg := &ToolRegistry{service: &mockService{}, logger: newTestLogger(t)}

	_, err := reg.handleMsgChannelStats(context.Background(), &mockMessagingService{}, map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "channel")
}

func TestHandleMsgChannelStats_LookupFailure(t *testing.T) {
	reg := &ToolRegistry{service: &mockService{}, logger: newTestLogger(t)}
	msgSvc := &mockMessagingService{
		getFunc: func(context.Context, string) (*messaging.Channel, error) {
			return nil, context.DeadlineExceeded
		},
	}

	// Any lookup failure other than an unknown channel is a server fault.
	_, err := reg.handleMsgChannelStats(context.Background(), msgSvc, map[string]interface{}{"channel": "orders"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, mcptransport.JSONRPCInternalError, toolErrorCode(err))
}

// TestMsgChannelStatsPermission pins that the channel stats tool requires
// the same messaging:stats permission as the REST route and the gRPC method.
func TestMsgChannelStatsPermission(t *testing.T) {
	perm, ok := qrdbToolExactMapping["quicbaas_msg_channel_stats"]
	require.True(t, ok, "quicbaas_msg_channel_stats has no permission mapping")
	assert.Equal(t, qrdbToolResource{"messaging", "stats"}, perm)
}
