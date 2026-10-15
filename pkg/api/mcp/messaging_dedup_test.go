//go:build mcp

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage/file"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// =============================================================================
// Messaging Dedup Stats Tests
// =============================================================================

func TestHandleMsgDedupStats_MissingChannel(t *testing.T) {
	log := newTestLogger(t)
	svc := &mockService{}
	reg := &ToolRegistry{service: svc, logger: log}

	msgSvc := &mockMessagingService{}

	_, err := reg.handleMsgDedupStats(context.Background(), msgSvc, map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "channel")
}

// TestHandleMsgDedupStats_UnknownChannel pins that the tool refuses a channel
// that does not exist, and that the refusal reaches MCP clients as NotFound.
func TestHandleMsgDedupStats_UnknownChannel(t *testing.T) {
	reg := &ToolRegistry{service: &mockService{}, logger: newTestLogger(t)}
	msgSvc := &mockMessagingService{
		getFunc: func(context.Context, string) (*messaging.Channel, error) {
			return nil, messaging.ErrChannelNotFound
		},
	}

	_, err := reg.handleMsgDedupStats(context.Background(), msgSvc, map[string]interface{}{"channel": "missing"})
	require.Error(t, err)
	assert.Equal(t, mcptransport.MCPResourceNotFound, toolErrorCode(err))

	// Any other lookup failure is a server fault, not a refusal.
	msgSvc.getFunc = func(context.Context, string) (*messaging.Channel, error) {
		return nil, context.DeadlineExceeded
	}
	_, err = reg.handleMsgDedupStats(context.Background(), msgSvc, map[string]interface{}{"channel": "orders"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, mcptransport.JSONRPCInternalError, toolErrorCode(err))
}

// TestHandleMsgDedupStats_ChannelFigures pins that the tool reads the
// channel's own figures from the push service's dedup store.
func TestHandleMsgDedupStats_ChannelFigures(t *testing.T) {
	log := newTestLogger(t)
	storCfg := file.DefaultConfig()
	storCfg.DataDir = t.TempDir()
	storCfg.SyncWrites = false
	stor, err := file.New(storCfg, log)
	require.NoError(t, err)
	ps := messaging.NewPushService(log, stor, messaging.WithDedup(true))
	ctx := context.Background()
	require.NoError(t, ps.Start(ctx))
	t.Cleanup(func() {
		assert.NoError(t, ps.Stop(context.Background()))
	})

	// orders sees one message three times; audit sees one message once.
	for _, name := range []string{"orders", "audit"} {
		_, err := ps.CreateChannel(ctx, name)
		require.NoError(t, err)
	}
	publishes := []struct{ channel, key string }{
		{"orders", "k-1"}, {"orders", "k-1"}, {"orders", "k-1"}, {"audit", "k-2"},
	}
	for _, p := range publishes {
		_, err := ps.Publish(ctx, p.channel, map[string]int{"id": 1}, messaging.WithDedupKey(p.key))
		require.NoError(t, err)
	}

	reg := &ToolRegistry{service: &mockService{}, logger: log}
	result, err := reg.handleMsgDedupStats(ctx, ps, map[string]interface{}{"channel": "orders"})
	require.NoError(t, err)
	stats, ok := result.(map[string]interface{})
	require.True(t, ok, "result is %T, want a map", result)
	assert.Equal(t, true, stats["available"])
	assert.Equal(t, "orders", stats["channel"])
	assert.Equal(t, int64(1), stats["total_messages"])
	assert.Equal(t, int64(2), stats["total_duplicates"])

	// The push service's own not-found error is the typed one, not the
	// sentinel; it must still reach MCP clients as NotFound.
	_, err = reg.handleMsgDedupStats(ctx, ps, map[string]interface{}{"channel": "missing"})
	require.Error(t, err)
	assert.Equal(t, mcptransport.MCPResourceNotFound, toolErrorCode(err))
}

// TestMsgDedupStatsPermission pins that the dedup tool requires the same
// messaging:stats permission as the REST route and the gRPC method.
func TestMsgDedupStatsPermission(t *testing.T) {
	perm, ok := qrdbToolExactMapping["quicbaas_msg_dedup_stats"]
	require.True(t, ok, "quicbaas_msg_dedup_stats has no permission mapping")
	assert.Equal(t, qrdbToolResource{"messaging", "stats"}, perm)
}

func TestHandleMsgDedupStats_NotPushService(t *testing.T) {
	log := newTestLogger(t)
	svc := &mockService{}
	reg := &ToolRegistry{service: svc, logger: log}

	msgSvc := &mockMessagingService{}

	// mockMessagingService is not *messaging.PushService, so should return error
	_, err := reg.handleMsgDedupStats(context.Background(), msgSvc, map[string]interface{}{
		"channel": "test-channel",
	})
	require.Error(t, err)
}
