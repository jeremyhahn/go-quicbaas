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

// Package main: command-handler coverage for mcp.go.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock
// =============================================================================

// recordedCall captures one forwarded JSON-RPC request.
type recordedCall struct {
	Method string
	Params json.RawMessage
}

// mockMCPCaller implements MCPCaller and records everything forwarded to it.
type mockMCPCaller struct {
	calls  []recordedCall
	result json.RawMessage
	err    error
}

func (m *mockMCPCaller) CallRPC(_ context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	m.calls = append(m.calls, recordedCall{Method: method, Params: params})
	if m.err != nil {
		return nil, m.err
	}
	if m.result == nil {
		return json.RawMessage(`{}`), nil
	}
	return m.result, nil
}

// withMockMCPCaller installs an mcpCallerFactory returning the supplied mock.
func withMockMCPCaller(t *testing.T, mc *mockMCPCaller) {
	t.Helper()
	orig := mcpCallerFactory
	origTimeout := globalTimeout
	mcpCallerFactory = func(*cobra.Command) (MCPCaller, error) { return mc, nil }
	if globalTimeout == 0 {
		globalTimeout = 5 * time.Second
	}
	t.Cleanup(func() {
		mcpCallerFactory = orig
		globalTimeout = origTimeout
	})
}

// withFailingMCPCallerFactory installs an mcpCallerFactory that always fails.
func withFailingMCPCallerFactory(t *testing.T, err error) {
	t.Helper()
	orig := mcpCallerFactory
	mcpCallerFactory = func(*cobra.Command) (MCPCaller, error) { return nil, err }
	t.Cleanup(func() { mcpCallerFactory = orig })
}

// =============================================================================
// Command Tree
// =============================================================================

// TestNewMCPCommand_Tree verifies the MCP verbs are wired.
func TestNewMCPCommand_Tree(t *testing.T) {
	cmd := newMCPCommand()

	got := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		got[sub.Name()] = true
	}
	for _, name := range []string{"serve", "tools", "call"} {
		assert.True(t, got[name], "missing mcp subcommand %q", name)
	}
}

// =============================================================================
// Stdio Bridge
// =============================================================================

// TestServeMCPStdio_ForwardsRequests verifies each request is forwarded
// verbatim and answered with a JSON-RPC response carrying the original ID.
func TestServeMCPStdio_ForwardsRequests(t *testing.T) {
	mc := &mockMCPCaller{result: json.RawMessage(`{"tools":[{"name":"quicbaas_health"}]}`)}

	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var out bytes.Buffer

	require.NoError(t, serveMCPStdio(context.Background(), mc, in, &out))

	require.Len(t, mc.calls, 2)
	assert.Equal(t, "initialize", mc.calls[0].Method)
	assert.JSONEq(t, `{"protocolVersion":"2024-11-05"}`, string(mc.calls[0].Params))
	assert.Equal(t, "tools/list", mc.calls[1].Method)

	lines := nonEmptyLines(out.String())
	require.Len(t, lines, 2)

	var first mcpResponse
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first))
	assert.Equal(t, "2.0", first.JSONRPC)
	assert.Equal(t, "1", string(first.ID))
	assert.Nil(t, first.Error)
	assert.Contains(t, string(first.Result), "quicbaas_health")
}

// TestServeMCPStdio_NotificationsNotForwarded verifies ID-less messages are
// swallowed: each bridged call is an independent HTTP request, so there is no
// session for a notification to update and the server would answer
// "method not found".
func TestServeMCPStdio_NotificationsNotForwarded(t *testing.T) {
	mc := &mockMCPCaller{}

	in := strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	var out bytes.Buffer

	require.NoError(t, serveMCPStdio(context.Background(), mc, in, &out))

	assert.Empty(t, mc.calls, "notifications must not be forwarded")
	assert.Empty(t, out.String(), "notifications must not produce a response")
}

// TestServeMCPStdio_TransportErrorBecomesRPCError verifies a transport failure
// is reported as a JSON-RPC error for that request instead of terminating the
// bridge or desynchronizing the stream.
func TestServeMCPStdio_TransportErrorBecomesRPCError(t *testing.T) {
	mc := &mockMCPCaller{err: stderrors.New("upstream unavailable")}

	in := strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"tools/list"}` + "\n")
	var out bytes.Buffer

	require.NoError(t, serveMCPStdio(context.Background(), mc, in, &out))

	var resp mcpResponse
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp))
	require.NotNil(t, resp.Error)
	assert.Equal(t, mcpCodeInternalError, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "upstream unavailable")
	assert.Equal(t, "7", string(resp.ID))
}

// TestServeMCPStdio_MissingMethod verifies a request with an ID but no method
// is answered with an invalid-request error rather than forwarded.
func TestServeMCPStdio_MissingMethod(t *testing.T) {
	mc := &mockMCPCaller{}

	in := strings.NewReader(`{"jsonrpc":"2.0","id":3}` + "\n")
	var out bytes.Buffer

	require.NoError(t, serveMCPStdio(context.Background(), mc, in, &out))

	assert.Empty(t, mc.calls)

	var resp mcpResponse
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp))
	require.NotNil(t, resp.Error)
	assert.Equal(t, mcpCodeInvalidRequest, resp.Error.Code)
}

// TestServeMCPStdio_MalformedInput verifies unparseable stdin is a hard error,
// since the bridge can no longer trust message framing.
func TestServeMCPStdio_MalformedInput(t *testing.T) {
	mc := &mockMCPCaller{}
	var out bytes.Buffer

	err := serveMCPStdio(context.Background(), mc, strings.NewReader("not json at all"), &out)
	require.Error(t, err)

	var decErr *MCPBridgeDecodeError
	assert.True(t, stderrors.As(err, &decErr))
}

// TestServeMCPStdio_CancelledContext verifies a cancelled context stops the
// bridge before it consumes further input.
func TestServeMCPStdio_CancelledContext(t *testing.T) {
	mc := &mockMCPCaller{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var out bytes.Buffer
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n")

	require.NoError(t, serveMCPStdio(ctx, mc, in, &out))
	assert.Empty(t, mc.calls)
}

// =============================================================================
// Serve Command
// =============================================================================

// TestRunMCPServe_RequiresStdio verifies serve refuses to run without --stdio.
func TestRunMCPServe_RequiresStdio(t *testing.T) {
	mc := &mockMCPCaller{}
	withMockMCPCaller(t, mc)

	cmd := newMCPServeCommand()
	mcpStdio = false
	t.Cleanup(func() { mcpStdio = false })

	err := runMCPServe(cmd, nil)
	require.Error(t, err)

	var stdioErr *StdioRequiredError
	assert.True(t, stderrors.As(err, &stdioErr))
}

// TestRunMCPServe_FactoryError verifies a transport construction failure is
// surfaced instead of leaving the host waiting on a dead pipe.
func TestRunMCPServe_FactoryError(t *testing.T) {
	withFailingMCPCallerFactory(t, stderrors.New("no mcp transport"))

	cmd := newMCPServeCommand()
	cmd.SetContext(context.Background())
	mcpStdio = true
	t.Cleanup(func() { mcpStdio = false })

	err := runMCPServe(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no mcp transport")
}

// =============================================================================
// Tools Command
// =============================================================================

func TestRunMCPTools_Happy(t *testing.T) {
	mc := &mockMCPCaller{result: json.RawMessage(`{"tools":[{"name":"quicbaas_health"}]}`)}
	withMockMCPCaller(t, mc)

	require.NoError(t, runMCPTools(newMCPToolsCommand(), nil))

	require.Len(t, mc.calls, 1)
	assert.Equal(t, "tools/list", mc.calls[0].Method)
	assert.Nil(t, mc.calls[0].Params, "tools/list takes no params")
}

func TestRunMCPTools_TransportError(t *testing.T) {
	withMockMCPCaller(t, &mockMCPCaller{err: stderrors.New("tools boom")})

	require.Error(t, runMCPTools(newMCPToolsCommand(), nil))
}

func TestRunMCPTools_FactoryError(t *testing.T) {
	withFailingMCPCallerFactory(t, stderrors.New("factory boom"))

	require.Error(t, runMCPTools(newMCPToolsCommand(), nil))
}

// =============================================================================
// Call Command
// =============================================================================

func TestRunMCPCall_Happy(t *testing.T) {
	mc := &mockMCPCaller{result: json.RawMessage(`{"content":[]}`)}
	withMockMCPCaller(t, mc)

	cmd := newMCPCallCommand()
	mcpToolArgs = `{"key":"k1"}`
	t.Cleanup(func() { mcpToolArgs = "" })

	require.NoError(t, runMCPCall(cmd, []string{"quicbaas_kv_get"}))

	require.Len(t, mc.calls, 1)
	assert.Equal(t, "tools/call", mc.calls[0].Method)
	assert.JSONEq(t, `{"name":"quicbaas_kv_get","arguments":{"key":"k1"}}`, string(mc.calls[0].Params))
}

// TestRunMCPCall_InvalidArgs verifies malformed --args is rejected locally.
func TestRunMCPCall_InvalidArgs(t *testing.T) {
	mc := &mockMCPCaller{}
	withMockMCPCaller(t, mc)

	cmd := newMCPCallCommand()
	mcpToolArgs = `{not json}`
	t.Cleanup(func() { mcpToolArgs = "" })

	err := runMCPCall(cmd, []string{"quicbaas_kv_get"})
	require.Error(t, err)

	var argErr *InvalidMCPToolArgsError
	assert.True(t, stderrors.As(err, &argErr))
	assert.Empty(t, mc.calls, "invalid args must not reach the server")
}

func TestBuildMCPToolCallParams(t *testing.T) {
	params, err := buildMCPToolCallParams("quicbaas_health", "")
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"quicbaas_health"}`, string(params))

	params, err = buildMCPToolCallParams("quicbaas_kv_get", `{"id":1}`)
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"quicbaas_kv_get","arguments":{"id":1}}`, string(params))

	_, err = buildMCPToolCallParams("quicbaas_kv_get", `[1,2,3]`)
	require.Error(t, err, "a JSON array is not a valid arguments object")
}

// =============================================================================
// Transport Selection
// =============================================================================

// TestSelectMCPTransport verifies the group defaults to the MCP transport but
// never overrides an explicit --transport.
func TestSelectMCPTransport(t *testing.T) {
	orig := transport
	t.Cleanup(func() { transport = orig })

	root := &cobra.Command{Use: "quicbaas"}
	root.PersistentFlags().StringVar(&transport, "transport", "unix", "")
	sub := &cobra.Command{Use: "mcp"}
	root.AddCommand(sub)

	transport = "unix"
	selectMCPTransport(sub)
	assert.Equal(t, "mcp", transport, "an unset --transport must default to mcp")

	require.NoError(t, root.PersistentFlags().Set("transport", "grpc"))
	selectMCPTransport(sub)
	assert.Equal(t, "grpc", transport, "an explicit --transport must win")
}

// TestMCPTransportRequiredError_Message verifies the error names the offending
// transport and the fix.
func TestMCPTransportRequiredError_Message(t *testing.T) {
	err := &MCPTransportRequiredError{Transport: "grpc"}
	assert.Contains(t, err.Error(), "grpc")
	assert.Contains(t, err.Error(), "--transport mcp")
}

// =============================================================================
// Result Rendering
// =============================================================================

func TestWriteMCPResult(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeMCPResult(json.RawMessage(`{"a":1}`), &buf))
	assert.Contains(t, buf.String(), `"a": 1`, "JSON results must be re-indented")

	buf.Reset()
	require.NoError(t, writeMCPResult(nil, &buf))
	assert.Equal(t, "{}\n", buf.String())

	// A non-JSON payload is emitted untouched rather than dropped.
	buf.Reset()
	require.NoError(t, writeMCPResult(json.RawMessage(`<<not json>>`), &buf))
	assert.Contains(t, buf.String(), "not json")
}

// nonEmptyLines splits s into its non-blank lines.
func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// TestCreateSDKMCPCaller_RejectsUnixSocketAddress verifies the default Unix
// socket address is caught with an actionable error instead of being handed to
// the SDK, which would build an undialable "https:///path.sock" URL.
func TestCreateSDKMCPCaller_RejectsUnixSocketAddress(t *testing.T) {
	origTransport := transport
	origAddress := address
	t.Cleanup(func() {
		transport = origTransport
		address = origAddress
	})

	root := &cobra.Command{Use: "quicbaas"}
	root.PersistentFlags().StringVar(&transport, "transport", "unix", "")
	sub := &cobra.Command{Use: "mcp"}
	root.AddCommand(sub)

	transport = "unix"
	address = "/home/user/quicbaas.sock"

	caller, err := createSDKMCPCaller(sub)
	require.Error(t, err)
	assert.Nil(t, caller)

	var addrErr *MCPAddressRequiredError
	require.True(t, stderrors.As(err, &addrErr))
	assert.Contains(t, err.Error(), "--address host:port")
}

// TestIsUnixSocketAddress covers both the socket and host:port shapes.
func TestIsUnixSocketAddress(t *testing.T) {
	assert.True(t, isUnixSocketAddress("/run/quicbaas.sock"))
	assert.True(t, isUnixSocketAddress("relative/quicbaas.sock"))
	assert.False(t, isUnixSocketAddress("quicbaas.example.com:11443"))
	assert.False(t, isUnixSocketAddress("localhost:11443"))
}
