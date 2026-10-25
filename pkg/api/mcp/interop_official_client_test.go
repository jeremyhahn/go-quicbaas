//go:build mcp

// Interop leg for issue #203: the server's MCP tool surface, served over the
// streamable HTTP transport, driven by an independent client. The server and
// sdk/go/transport/mcp both use mark3labs/mcp-go, so every other MCP test is
// that library talking to itself; this leg uses the official MCP Go SDK
// (github.com/modelcontextprotocol/go-sdk) as the client instead.
//
// The server side mirrors production composition: an mcp-go MCPServer built
// with the same options go-qrdb's unified API server uses, with this
// repository's module tools registered on it through apimodule.RegisterMCP.

package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	mcpservertest "github.com/mark3labs/mcp-go/server/servertest"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	"github.com/jeremyhahn/go-quicbaas/pkg/apimodule"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// interopServerName and interopServerVersion are the implementation info
// go-qrdb's unified API server hands to mcpserver.NewMCPServer.
const (
	interopServerName    = "qrdb"
	interopServerVersion = "1.0.0"
)

// connectOfficialClient serves the module tool surface on a loopback
// httptest server and connects the official MCP Go SDK client to it. A failed
// initialize handshake fails the test here, so no later assertion can pass
// against a session that never came up.
func connectOfficialClient(t *testing.T) *mcp.ClientSession {
	t.Helper()

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	svc := testutil.NewMockQuicBaaS()
	svc.On("Liveness", mock.Anything, mock.Anything).
		Return(&service.LivenessResponse{Alive: true}, nil).Maybe()
	backend := mcpapi.NewServer(svc, mcpapi.DefaultConfig(), log)

	module, err := apimodule.NewModule(func() service.QuicBaaS { return svc }, log,
		apimodule.WithMCPServer(func() *mcpapi.Server { return backend }))
	require.NoError(t, err)

	srv := mcpserver.NewMCPServer(interopServerName, interopServerVersion,
		mcpserver.WithToolCapabilities(false))
	module.RegisterMCP(srv)

	httpSrv := mcpservertest.NewTestStreamableHTTPServer(srv)
	t.Cleanup(httpSrv.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "go-sdk-interop", Version: "1.8.0"}, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   httpSrv.URL,
		HTTPClient: httpSrv.Client(),
	}, nil)
	require.NoError(t, err, "official MCP client failed the initialize handshake")
	t.Cleanup(func() { require.NoError(t, session.Close()) })
	return session
}

func callContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestOfficialClient_Initialize(t *testing.T) {
	session := connectOfficialClient(t)

	init := session.InitializeResult()
	require.NotNil(t, init, "session has no initialize result")
	require.Equal(t, mcpgo.LATEST_PROTOCOL_VERSION, init.ProtocolVersion,
		"negotiated protocol version is not the server's latest")
	require.Contains(t, mcp.SupportedProtocolVersions(), init.ProtocolVersion,
		"negotiated protocol version is not one the official client supports")
	require.NotNil(t, init.ServerInfo, "initialize result has no serverInfo")
	require.Equal(t, interopServerName, init.ServerInfo.Name)
	require.Equal(t, interopServerVersion, init.ServerInfo.Version)
	require.NotNil(t, init.Capabilities, "initialize result has no capabilities")
	require.NotNil(t, init.Capabilities.Tools, "server did not advertise the tools capability")
}

func TestOfficialClient_ListTools(t *testing.T) {
	session := connectOfficialClient(t)
	ctx := callContext(t)

	var listed []string
	params := &mcp.ListToolsParams{}
	for {
		res, err := session.ListTools(ctx, params)
		require.NoError(t, err)
		for _, tool := range res.Tools {
			require.NotNil(t, tool.InputSchema, "tool %q has no inputSchema", tool.Name)
			listed = append(listed, tool.Name)
		}
		if res.NextCursor == "" {
			break
		}
		params = &mcp.ListToolsParams{Cursor: res.NextCursor}
	}

	specs := mcpapi.ModuleTools()
	want := make([]string, 0, len(specs))
	for _, spec := range specs {
		want = append(want, spec.Tool)
	}
	slices.Sort(listed)
	slices.Sort(want)
	require.Equal(t, want, listed, "tools/list differs from the server's tool registry")
}

func TestOfficialClient_CallTool(t *testing.T) {
	session := connectOfficialClient(t)

	res, err := session.CallTool(callContext(t), &mcp.CallToolParams{
		Name:      "quicbaas_liveness",
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "liveness returned a tool error: %+v", res.Content)
	require.Len(t, res.Content, 1)

	text, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok, "content is %T, want *mcp.TextContent", res.Content[0])
	var body struct{ Alive bool }
	require.NoError(t, json.Unmarshal([]byte(text.Text), &body), "tool text is not JSON: %q", text.Text)
	require.True(t, body.Alive)
}

func TestOfficialClient_CallUnknownTool(t *testing.T) {
	session := connectOfficialClient(t)

	res, err := session.CallTool(callContext(t), &mcp.CallToolParams{
		Name:      "quicbaas_no_such_tool",
		Arguments: map[string]any{},
	})
	require.Error(t, err, "unknown tool returned a result: %+v", res)

	var wireErr *jsonrpc.Error
	require.True(t, errors.As(err, &wireErr), "unknown tool failed outside JSON-RPC: %v", err)
	require.Equal(t, int64(jsonrpc.CodeInvalidParams), wireErr.Code)
	require.Contains(t, wireErr.Message, "quicbaas_no_such_tool")
}
