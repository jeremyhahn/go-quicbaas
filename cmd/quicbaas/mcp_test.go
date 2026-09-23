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
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcpclienttransport "github.com/mark3labs/mcp-go/client/transport"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// =============================================================================
// Mock
// =============================================================================

// recordedCall captures one forwarded JSON-RPC request or notification.
type recordedCall struct {
	Method       string
	Params       json.RawMessage
	Notification bool
}

// mockMCPCaller implements MCPCaller and records everything forwarded to it.
type mockMCPCaller struct {
	calls  []recordedCall
	result json.RawMessage
	err    error
	// respond, when set, scripts the reply to the n-th recorded call
	// (0-based, notifications included) and takes precedence over result/err.
	respond func(n int, method string) (json.RawMessage, error)
	// notifyErr is returned by every NotifyRPC.
	notifyErr error
}

func (m *mockMCPCaller) NotifyRPC(_ context.Context, method string, params json.RawMessage) error {
	m.calls = append(m.calls, recordedCall{Method: method, Params: params, Notification: true})
	return m.notifyErr
}

// methods lists the recorded methods in order.
func (m *mockMCPCaller) methods() []string {
	out := make([]string, 0, len(m.calls))
	for _, c := range m.calls {
		out = append(out, c.Method)
	}
	return out
}

func (m *mockMCPCaller) CallRPC(_ context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	m.calls = append(m.calls, recordedCall{Method: method, Params: params})
	if m.respond != nil {
		return m.respond(len(m.calls)-1, method)
	}
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

	require.NoError(t, serveMCPStdio(context.Background(), mc, in, &out, io.Discard))

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

// TestServeMCPStdio_NotificationsForwarded verifies ID-less messages — the
// host's "notifications/initialized" in particular, which completes the MCP
// handshake — are forwarded to the server and answered with nothing.
func TestServeMCPStdio_NotificationsForwarded(t *testing.T) {
	mc := &mockMCPCaller{}

	in := strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":3}}` + "\n")
	var out bytes.Buffer

	require.NoError(t, serveMCPStdio(context.Background(), mc, in, &out, io.Discard))

	require.Len(t, mc.calls, 2)
	assert.True(t, mc.calls[0].Notification)
	assert.Equal(t, "notifications/initialized", mc.calls[0].Method)
	assert.True(t, mc.calls[1].Notification)
	assert.JSONEq(t, `{"requestId":3}`, string(mc.calls[1].Params))
	assert.Empty(t, out.String(), "a notification takes no reply")
}

// TestServeMCPStdio_NotificationFailureReportedOnStderr verifies a rejected
// notification is reported on errOut and never written to the stream.
func TestServeMCPStdio_NotificationFailureReportedOnStderr(t *testing.T) {
	mc := &mockMCPCaller{notifyErr: stderrors.New("notify boom")}

	in := strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0"}` + "\n")
	var out, errOut bytes.Buffer

	require.NoError(t, serveMCPStdio(context.Background(), mc, in, &out, &errOut))

	require.Len(t, mc.calls, 1, "a message with neither id nor method is not forwarded")
	assert.Empty(t, out.String())
	assert.Contains(t, errOut.String(), "notifications/initialized failed: notify boom")
	assert.Contains(t, errOut.String(), "neither id nor method")
}

// TestServeMCPStdio_TransportErrorBecomesRPCError verifies a transport failure
// is reported as a JSON-RPC error for that request instead of terminating the
// bridge or desynchronizing the stream.
func TestServeMCPStdio_TransportErrorBecomesRPCError(t *testing.T) {
	mc := &mockMCPCaller{err: stderrors.New("upstream unavailable")}

	in := strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"tools/list"}` + "\n")
	var out bytes.Buffer

	require.NoError(t, serveMCPStdio(context.Background(), mc, in, &out, io.Discard))

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

	require.NoError(t, serveMCPStdio(context.Background(), mc, in, &out, io.Discard))

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

	err := serveMCPStdio(context.Background(), mc, strings.NewReader("not json at all"), &out, io.Discard)
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

	require.NoError(t, serveMCPStdio(ctx, mc, in, &out, io.Discard))
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

	assert.Equal(t, []string{"initialize", "notifications/initialized", "tools/list"}, mc.methods(),
		"the full session handshake must precede tools/list")
	assert.True(t, mc.calls[1].Notification)
	assert.Nil(t, mc.calls[2].Params, "tools/list takes no params")
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

	assert.Equal(t, []string{"initialize", "notifications/initialized", "tools/call"}, mc.methods(),
		"the full session handshake must precede tools/call")
	assert.JSONEq(t, `{"name":"quicbaas_kv_get","arguments":{"key":"k1"}}`, string(mc.calls[2].Params))
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

// =============================================================================
// Session Handshake
// =============================================================================

// sessionTerminatedErr mirrors how the SDK surfaces the server's 404 for a
// request carrying no live session.
func sessionTerminatedErr() error {
	return sdkerrors.Wrap(fmt.Errorf("failed to send request: %w", mcpclienttransport.ErrSessionTerminated),
		sdkerrors.CodeConnection, "mcp.sendRPC", "HTTP request failed")
}

// TestMcpInitializeParams verifies the CLI's handshake is spec-compliant.
func TestMcpInitializeParams(t *testing.T) {
	raw, err := mcpInitializeParams()
	require.NoError(t, err)

	var params struct {
		ProtocolVersion string                 `json:"protocolVersion"`
		Capabilities    map[string]interface{} `json:"capabilities"`
		ClientInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"clientInfo"`
	}
	require.NoError(t, json.Unmarshal(raw, &params))
	assert.Equal(t, mcptransport.MCPVersion, params.ProtocolVersion)
	assert.NotNil(t, params.Capabilities)
	assert.Equal(t, mcpClientName, params.ClientInfo.Name)
	assert.NotEmpty(t, params.ClientInfo.Version)
}

func TestMcpInitialize_Success(t *testing.T) {
	mc := &mockMCPCaller{}

	require.NoError(t, mcpInitialize(context.Background(), mc))

	assert.Equal(t, []string{"initialize", "notifications/initialized"}, mc.methods())
	assert.False(t, mc.calls[0].Notification)
	assert.Contains(t, string(mc.calls[0].Params), mcptransport.MCPVersion)
	assert.True(t, mc.calls[1].Notification, "initialized must be a notification, not a request")
}

// TestMcpInitialize_NotificationError verifies a failed
// "notifications/initialized" fails the handshake.
func TestMcpInitialize_NotificationError(t *testing.T) {
	mc := &mockMCPCaller{notifyErr: stderrors.New("notify boom")}

	err := mcpInitialize(context.Background(), mc)
	require.Error(t, err)

	var initErr *MCPInitializeError
	require.True(t, stderrors.As(err, &initErr))
	assert.Contains(t, err.Error(), "notify boom")
}

func TestMcpInitialize_TransportError(t *testing.T) {
	mc := &mockMCPCaller{err: stderrors.New("init boom")}

	err := mcpInitialize(context.Background(), mc)
	require.Error(t, err)

	var initErr *MCPInitializeError
	require.True(t, stderrors.As(err, &initErr))
	assert.Contains(t, err.Error(), "init boom")
}

// TestRunMCPCall_InitializeFailure verifies a failed handshake stops the
// command before the tool is invoked.
func TestRunMCPCall_InitializeFailure(t *testing.T) {
	mc := &mockMCPCaller{err: stderrors.New("init boom")}
	withMockMCPCaller(t, mc)

	require.Error(t, runMCPCall(newMCPCallCommand(), []string{"quicbaas_health"}))
	assert.Equal(t, []string{"initialize"}, mc.methods())
}

// decodeBridgeResponses parses every JSON-RPC response the bridge wrote.
func decodeBridgeResponses(t *testing.T, out string) []mcpResponse {
	t.Helper()
	var resps []mcpResponse
	for _, line := range nonEmptyLines(out) {
		var r mcpResponse
		require.NoError(t, json.Unmarshal([]byte(line), &r))
		resps = append(resps, r)
	}
	return resps
}

// TestServeMCPStdio_RecoversExpiredSession verifies the bridge replays the
// host's own initialize and retries once when the server has expired the
// session, answering the host as if nothing happened.
func TestServeMCPStdio_RecoversExpiredSession(t *testing.T) {
	mc := &mockMCPCaller{respond: func(n int, _ string) (json.RawMessage, error) {
		if n == 2 { // the first tools/list: the session was swept
			return nil, sessionTerminatedErr()
		}
		return json.RawMessage(`{"tools":[]}`), nil
	}}

	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}` + "\n" +
			`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var out bytes.Buffer

	require.NoError(t, serveMCPStdio(context.Background(), mc, in, &out, io.Discard))

	assert.Equal(t, []string{
		"initialize", "notifications/initialized", "tools/list", // the host's own traffic
		"initialize", "notifications/initialized", "tools/list", // recovery: full handshake, then retry
	}, mc.methods())
	assert.JSONEq(t, `{"protocolVersion":"2024-11-05"}`, string(mc.calls[3].Params),
		"the replayed initialize must carry the host's own params")
	assert.True(t, mc.calls[4].Notification)

	resps := decodeBridgeResponses(t, out.String())
	require.Len(t, resps, 2)
	assert.Equal(t, "2", string(resps[1].ID))
	assert.Nil(t, resps[1].Error)
	assert.JSONEq(t, `{"tools":[]}`, string(resps[1].Result))
}

// TestServeMCPStdio_RecoveryFailureReturnsOriginalError verifies a failed
// recovery does not loop and reports the original error for the request.
func TestServeMCPStdio_RecoveryFailureReturnsOriginalError(t *testing.T) {
	mc := &mockMCPCaller{respond: func(n int, _ string) (json.RawMessage, error) {
		switch n {
		case 0:
			return json.RawMessage(`{}`), nil
		case 1:
			return nil, sessionTerminatedErr()
		default:
			return nil, stderrors.New("replay boom")
		}
	}}

	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var out bytes.Buffer

	require.NoError(t, serveMCPStdio(context.Background(), mc, in, &out, io.Discard))

	require.Len(t, mc.calls, 3, "one replayed initialize, then give up")
	assert.Equal(t, "initialize", mc.calls[2].Method)

	resps := decodeBridgeResponses(t, out.String())
	require.Len(t, resps, 2)
	require.NotNil(t, resps[1].Error)
	assert.Equal(t, mcpCodeInternalError, resps[1].Error.Code)
	assert.Contains(t, resps[1].Error.Message, "session terminated")
	assert.NotContains(t, resps[1].Error.Message, "replay boom")
}

// TestServeMCPStdio_RecoveryNotificationFailureReturnsOriginalError verifies
// that when the replayed handshake's "notifications/initialized" fails, the
// request is not retried and gets the original error.
func TestServeMCPStdio_RecoveryNotificationFailureReturnsOriginalError(t *testing.T) {
	mc := &mockMCPCaller{respond: func(n int, _ string) (json.RawMessage, error) {
		if n == 1 {
			return nil, sessionTerminatedErr()
		}
		return json.RawMessage(`{}`), nil
	}}

	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var out bytes.Buffer

	// Only the recovery sends a notification in this script, so failing
	// every NotifyRPC fails exactly the replayed handshake.
	mc.notifyErr = stderrors.New("notify boom")
	require.NoError(t, serveMCPStdio(context.Background(), mc, in, &out, io.Discard))

	assert.Equal(t, []string{"initialize", "tools/list", "initialize", "notifications/initialized"}, mc.methods(),
		"no retry after the handshake fails")

	resps := decodeBridgeResponses(t, out.String())
	require.Len(t, resps, 2)
	require.NotNil(t, resps[1].Error)
	assert.Contains(t, resps[1].Error.Message, "session terminated")
	assert.NotContains(t, resps[1].Error.Message, "notify boom")
}

// TestServeMCPStdio_NoPriorInitializeNoRecovery verifies the bridge does not
// invent a handshake the host never made: with no initialize to replay, the
// session-terminated error is reported as-is.
func TestServeMCPStdio_NoPriorInitializeNoRecovery(t *testing.T) {
	mc := &mockMCPCaller{err: sessionTerminatedErr()}

	in := strings.NewReader(`{"jsonrpc":"2.0","id":5,"method":"tools/list"}` + "\n")
	var out bytes.Buffer

	require.NoError(t, serveMCPStdio(context.Background(), mc, in, &out, io.Discard))

	require.Len(t, mc.calls, 1)
	resps := decodeBridgeResponses(t, out.String())
	require.Len(t, resps, 1)
	require.NotNil(t, resps[0].Error)
	assert.Contains(t, resps[0].Error.Message, "session terminated")
}
