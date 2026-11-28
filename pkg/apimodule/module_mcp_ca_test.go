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

package apimodule

import (
	"context"
	"encoding/json"
	"testing"

	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-xkms/sdk/go/transport"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/xkmsca"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	testca "github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
)

// The xkms.ca.* tools serve quicbaas's embedded CA through the shared xkmsca
// adapter, so the combined MCP server must declare and register them: a
// tools/call through the registered mcp-go server reaches the CA servicer.
func TestRegisterMCP_ServesTheXKMSCATools(t *testing.T) {
	servicer := xkmsca.New(testca.New(t, "module-mcp-ca"), "example.test")
	backend := newMCPBackend(t)
	backend.SetCAService(servicer)
	m := newMCPModule(t, func() *mcpapi.Server { return backend })

	perms := m.MCPToolPermissions()
	for _, tool := range []string{
		"xkms_ca_getBundle", "xkms_ca_getCert", "xkms_ca_signCSR", "xkms_ca_issueCert",
		"xkms_ca_revokeCert", "xkms_ca_generateCRL", "xkms_ca_isRevoked",
	} {
		_, declared := perms[tool]
		assert.True(t, declared, "CA tool %q must be declared", tool)
	}
	assert.Equal(t, qrdbsdk.Permission("ca:read"), perms["xkms_ca_getBundle"])
	_, declared := perms["xkms_generateKey"]
	assert.False(t, declared, "the rest of the xkms. family stays excluded")

	srv := mcpserver.NewMCPServer("test", "0.0.0", mcpserver.WithToolCapabilities(true))
	m.RegisterMCP(srv)

	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")
	raw := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"xkms_ca_getBundle","arguments":{}}}`)
	resp := srv.HandleMessage(ctx, raw)
	require.NotNil(t, resp)

	out, err := json.Marshal(resp)
	require.NoError(t, err)
	var decoded struct {
		Error  *json.RawMessage `json:"error"`
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(out, &decoded))
	require.Nil(t, decoded.Error, "tools/call must not fail: %s", out)
	require.False(t, decoded.Result.IsError, "CA tool must succeed: %s", out)
	require.Len(t, decoded.Result.Content, 1, "result: %s", out)

	want, err := servicer.GetCABundle(ctx, &transport.GetCABundleRequest{})
	require.NoError(t, err)
	wantJSON, err := json.Marshal(want)
	require.NoError(t, err)
	assert.JSONEq(t, string(wantJSON), decoded.Result.Content[0].Text)
}
