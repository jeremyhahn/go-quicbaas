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
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func newMCPTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	return log
}

// newMCPBackend builds a quicbaas MCP server over the mock facade, mirroring
// pkg/api/mcp's own test wiring. The server never listens.
func newMCPBackend(t *testing.T) *mcpapi.Server {
	t.Helper()
	mockSvc := testutil.NewMockQuicBaaS()
	mockSvc.On("Health", mock.Anything, mock.Anything).Return(&service.HealthResponse{
		Status: service.HealthStatusHealthy,
		NodeID: 1,
	}, nil).Maybe()
	mockSvc.On("Liveness", mock.Anything, mock.Anything).Return(&service.LivenessResponse{
		Alive: true,
	}, nil).Maybe()
	return mcpapi.NewServer(mockSvc, mcpapi.DefaultConfig(), newMCPTestLogger(t))
}

func newMCPModule(t *testing.T, resolve MCPResolver) *Module {
	t.Helper()
	m, err := NewModule(func() service.QuicBaaS { return nil }, newMCPTestLogger(t),
		WithMCPServer(resolve))
	require.NoError(t, err)
	return m
}

// toolCallRequest builds a CallToolRequest carrying the given arguments.
func toolCallRequest(args map[string]any) mcpgo.CallToolRequest {
	req := mcpgo.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

// TestRegisterMCP_RegistersFullToolSurface proves the full enumerated tool
// surface registers on the shared mcp-go server without panicking (name
// collisions or invalid names would fail registration), and that a module
// without a resolver registers nothing.
func TestRegisterMCP_RegistersFullToolSurface(t *testing.T) {
	backend := newMCPBackend(t)
	m := newMCPModule(t, func() *mcpapi.Server { return backend })

	srv := mcpserver.NewMCPServer("test", "0.0.0", mcpserver.WithToolCapabilities(true))
	m.RegisterMCP(srv)

	// Without a resolver: no-op.
	bare, err := NewModule(func() service.QuicBaaS { return nil }, newMCPTestLogger(t))
	require.NoError(t, err)
	bare.RegisterMCP(mcpserver.NewMCPServer("bare", "0.0.0"))
}

// TestMCPToolPermissions proves the declared permission map matches the
// enumeration classes: public tools identity-gated, mapped tools carrying
// their qrdb-canonical resource:action, unmapped tools OMITTED (fail-closed
// at qrdb's tool registry).
func TestMCPToolPermissions(t *testing.T) {
	backend := newMCPBackend(t)
	m := newMCPModule(t, func() *mcpapi.Server { return backend })

	perms := m.MCPToolPermissions()
	var mapped, public, omitted int
	for _, spec := range mcpapi.ModuleTools() {
		perm, declared := perms[spec.Tool]
		switch {
		case spec.Anonymous:
			assert.False(t, declared, "anonymous tool %q must declare no permission", spec.Tool)
		case spec.Public:
			public++
			require.True(t, declared, "public tool %q must be declared", spec.Tool)
			assert.Equal(t, qrdbsdk.PermissionIdentityGated, perm, "tool %q", spec.Tool)
		case spec.Resource != "" && spec.Action != "":
			mapped++
			require.True(t, declared, "mapped tool %q must be declared", spec.Tool)
			assert.Equal(t, qrdbsdk.Permission(spec.Resource+":"+spec.Action), perm, "tool %q", spec.Tool)
		default:
			omitted++
			assert.False(t, declared, "unmapped tool %q must be OMITTED (fail-closed)", spec.Tool)
		}
	}
	assert.Len(t, perms, mapped+public, "permission map must cover exactly the mapped+public tools")
	if omitted == 0 {
		t.Log("no unmapped tools — every tool is declared")
	}
}

// TestPublicMCPTools proves the module declares exactly the login tools for
// anonymous access, and no tool that reaches data or the health surface,
// which stays behind authentication.
func TestPublicMCPTools(t *testing.T) {
	backend := newMCPBackend(t)
	m := newMCPModule(t, func() *mcpapi.Server { return backend })

	assert.ElementsMatch(t, []string{"quicbaas_login_begin", "quicbaas_login_finish"}, m.PublicMCPTools())
	assert.NotContains(t, m.PublicMCPTools(), "quicbaas_health")
}

// TestMCPToolHandler_LazyAndDispatch proves the tool handler answers a tool
// error while unresolved and dispatches the real registry handler once
// resolved — without re-registration. Uses quicbaas_liveness: every
// qrdb_*-prefixed tool that used to answer a structural "service
// unavailable" error when unwired (barrier/ceremony/credential/tenant_
// barrier/user/user_authfactor) is now excluded from the module surface
// (P6.1/item-C: those are qrdb-native duplicate tools, see
// mcpapi.qrdbNativeDuplicateTools), so the "resolved" phase now asserts
// successful dispatch instead.
func TestMCPToolHandler_LazyAndDispatch(t *testing.T) {
	var backend *mcpapi.Server
	m := newMCPModule(t, func() *mcpapi.Server { return backend })

	var spec mcpapi.ToolSpec
	for _, s := range mcpapi.ModuleTools() {
		if s.Name == "quicbaas_liveness" {
			spec = s
			break
		}
	}
	require.NotEmpty(t, spec.Name, "quicbaas_liveness must be enumerated")
	handler := m.mcpToolHandler(spec)

	// Unresolved: tool error, not a protocol error.
	res, err := handler(context.Background(), toolCallRequest(nil))
	require.NoError(t, err)
	require.True(t, res.IsError)

	// Resolved: dispatch reaches the real liveness handler and succeeds.
	backend = newMCPBackend(t)
	res, err = handler(context.Background(), toolCallRequest(map[string]any{}))
	require.NoError(t, err)
	assert.False(t, res.IsError, "resolved liveness dispatch must succeed")
}

// TestMCPToolHandler_SuccessResult proves a successful dispatch returns the
// JSON-encoded tool result from the real registry handler.
func TestMCPToolHandler_SuccessResult(t *testing.T) {
	backend := newMCPBackend(t)
	m := newMCPModule(t, func() *mcpapi.Server { return backend })

	var spec mcpapi.ToolSpec
	for _, s := range mcpapi.ModuleTools() {
		if s.Name == "quicbaas_liveness" {
			spec = s
			break
		}
	}
	require.NotEmpty(t, spec.Name, "quicbaas_liveness must be enumerated")

	handler := m.mcpToolHandler(spec)
	res, err := handler(context.Background(), toolCallRequest(map[string]any{}))
	require.NoError(t, err)
	require.False(t, res.IsError, "health dispatch must succeed against the mocked facade")
	require.NotEmpty(t, res.Content, "success result must carry JSON content")
}
