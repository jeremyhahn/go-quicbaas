//go:build integration

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
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/id"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// The combined MCP server is go-qrdb's unified API server with quicbaas's
// tool registry composed in. These tests drive it on a real cluster node
// through the SDK's MCP transport, the way an MCP client reaches it
// (go-quicbaas#1926).

func TestMain(m *testing.M) {
	code := m.Run()
	if err := helpers.ReleaseDataPlaneTenants(); err != nil {
		fmt.Fprintf(os.Stderr, "release data-plane tenants: %v\n", err)
	}
	os.Exit(code)
}

const bearerScheme = "Bearer "

func mcpEndpoint(t *testing.T) string {
	t.Helper()
	endpoints := helpers.GetMCPEndpoints()
	require.NotEmpty(t, endpoints, "no MCP endpoint configured")
	return endpoints[0]
}

func newCombinedMCPClient(t *testing.T, opts ...quicbaas.Option) quicbaas.Client {
	t.Helper()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err)
	opts = append([]quicbaas.Option{
		quicbaas.WithMCP(), quicbaas.WithTLSConfig(tlsCfg), quicbaas.WithTimeout(30 * time.Second),
	}, opts...)
	client, err := quicbaas.NewClient(mcpEndpoint(t), opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func tenantOperatorBearer(t *testing.T) string {
	t.Helper()
	md, err := helpers.TenantOperatorGRPCCredentials(t).GetRequestMetadata(context.Background())
	require.NoError(t, err)
	token, ok := strings.CutPrefix(md["authorization"], bearerScheme)
	require.True(t, ok && token != "", "tenant operator credentials sent no bearer")
	return token
}

// An authenticated caller reaches quicbaas's composed tools on the node's
// MCP listener: readiness, then a KV round trip in its own tenant.
func TestCombinedMCPServer_AuthenticatedRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := newCombinedMCPClient(t, quicbaas.WithAuthToken(tenantOperatorBearer(t)))

	_, err := client.Health().Readiness(ctx)
	require.NoError(t, err, "readiness over the combined MCP server")

	key := id.HashString(fmt.Sprintf("mcp-combined-%d", time.Now().UnixNano()))
	payload := []byte(`{"via":"mcp"}`)
	_, err = client.KV().Put(ctx, &service.PutRequest{EntityType: "mcp_combined", ID: key, Data: payload})
	require.NoError(t, err, "KV put over the combined MCP server")

	got, err := client.KV().Get(ctx, &service.GetRequest{
		EntityType: "mcp_combined", ID: key, Consistency: service.ConsistencyLevelLinearizable,
	})
	require.NoError(t, err, "KV get over the combined MCP server")
	assert.True(t, got.Found)
	assert.JSONEq(t, string(payload), string(got.Data))
}

// A caller with no identity reaches nothing gated: the server refuses it at
// the MCP handshake or at the call, and a KV read never succeeds.
func TestCombinedMCPServer_AnonymousCallerRefused(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err)
	client, err := quicbaas.NewClient(mcpEndpoint(t),
		quicbaas.WithMCP(), quicbaas.WithTLSConfig(tlsCfg), quicbaas.WithTimeout(30*time.Second))
	if err != nil {
		return // refused at the handshake
	}
	t.Cleanup(func() { _ = client.Close() })

	_, err = client.KV().Get(ctx, &service.GetRequest{EntityType: "mcp_combined", ID: 1})
	require.Error(t, err, "an anonymous MCP caller must not read KV")
}
