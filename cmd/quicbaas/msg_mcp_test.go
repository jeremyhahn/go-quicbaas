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

package main

import (
	"context"
	"testing"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	"github.com/jeremyhahn/go-quicbaas/pkg/apimodule"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	pkgmessaging "github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// withMCPMessaging points the msg commands at a real push service served
// over the production MCP module and the SDK MCP transport.
func withMCPMessaging(t *testing.T) {
	t.Helper()

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "console", Output: "stdout"})
	require.NoError(t, err)
	ps := pkgmessaging.NewPushService(log, nil)
	require.NoError(t, ps.Start(context.Background()))

	svc := &testutil.MockQuicBaaS{}
	svc.On("Messaging").Return(ps).Maybe()
	backend := mcpapi.NewServer(svc, mcpapi.DefaultConfig(), log)
	module, err := apimodule.NewModule(func() service.QuicBaaS { return svc }, log,
		apimodule.WithMCPServer(func() *mcpapi.Server { return backend }))
	require.NoError(t, err)
	srv := mcpserver.NewMCPServer("quicbaas-msg-test", "1.0.0")
	module.RegisterMCP(srv)
	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv)

	tr, err := mcptransport.New(&sdktransport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	require.NoError(t, err)
	client := quicbaas.NewClientWithTransport(tr)

	origFactory, origTimeout := sdkClientFactory, globalTimeout
	sdkClientFactory = func() (quicbaas.Client, error) { return client, nil }
	globalTimeout = 10 * time.Second
	t.Cleanup(func() {
		sdkClientFactory, globalTimeout = origFactory, origTimeout
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
		httpSrv.Close()
		if err := ps.Stop(context.Background()); err != nil {
			t.Errorf("stop push service: %v", err)
		}
	})
}

// seedOrders creates the orders channel with one subscriber and two
// published messages.
func seedOrders(t *testing.T) {
	t.Helper()
	mustRunMsg(t, "create-channel", "orders")
	mustRunMsg(t, "subscribe", "orders")
	mustRunMsg(t, "publish", "orders", "--payload", "one")
	mustRunMsg(t, "publish", "orders", "--payload", "two")
}

// TestMsgChannelInfo_MCP drives msg channel-info over the MCP transport and
// pins its channel statistics to the embedded transport's.
func TestMsgChannelInfo_MCP(t *testing.T) {
	withEmbeddedMessaging(t)
	seedOrders(t)
	embedded := msgChannelInfoJSON(t, "orders")

	withMCPMessaging(t)
	seedOrders(t)
	overMCP := msgChannelInfoJSON(t, "orders")

	require.NotNil(t, overMCP.Stats)
	assert.Equal(t, "orders", overMCP.Stats.Name)
	assert.Equal(t, int64(1), overMCP.Stats.Subscribers)
	assert.Equal(t, int64(2), overMCP.Stats.MessagesPublished)
	assert.Equal(t, embedded.Stats, overMCP.Stats, "channel stats differ between embedded and MCP")

	_, err := runMsg(t, "", "channel-info", "missing")
	assert.Error(t, err)
}

// TestMsgListSubscriptions_MCPChannel drives list-subscriptions --channel,
// which reads the channel's subscriber count, over the MCP transport.
func TestMsgListSubscriptions_MCPChannel(t *testing.T) {
	withMCPMessaging(t)
	seedOrders(t)

	out := mustRunMsg(t, "list-subscriptions", "--channel", "orders")
	assert.Contains(t, out, "orders")

	_, err := runMsg(t, "", "list-subscriptions", "--channel", "missing")
	assert.Error(t, err)
}
