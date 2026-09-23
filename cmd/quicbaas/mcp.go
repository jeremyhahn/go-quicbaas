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

// Package main provides Model Context Protocol (MCP) CLI commands for QuicBaaS.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/version"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// =============================================================================
// MCP Command Flags
// =============================================================================

var (
	mcpStdio    bool
	mcpToolArgs string
)

// =============================================================================
// Client Plumbing
// =============================================================================

// MCPCaller is the raw JSON-RPC surface the mcp verbs need: requests, plus the
// notifications the MCP lifecycle requires ("notifications/initialized"). It
// is satisfied by the SDK's MCP transport; declaring it here (rather than
// importing a concrete type) keeps the commands testable and follows the
// accept-interfaces rule.
type MCPCaller interface {
	mcptransport.RPCCaller
	mcptransport.RPCNotifier
}

// MCPCallerFactory is a function type for creating raw MCP callers.
type MCPCallerFactory func(cmd *cobra.Command) (MCPCaller, error)

// mcpCallerFactory creates raw MCP callers. Defaults to createSDKMCPCaller.
var mcpCallerFactory MCPCallerFactory = createSDKMCPCaller

// MCPTransportRequiredError indicates the active SDK transport cannot carry raw
// MCP JSON-RPC. Only the MCP transport speaks the protocol natively.
type MCPTransportRequiredError struct {
	Transport string
}

// Error implements the error interface.
func (e *MCPTransportRequiredError) Error() string {
	return "the mcp commands require the MCP transport, but --transport is " + e.Transport +
		" (rerun with --transport mcp, or omit --transport to let the mcp commands select it)"
}

// selectMCPTransport points the SDK at the MCP transport unless the operator
// explicitly chose one with --transport. Every verb in this group speaks MCP,
// so defaulting to it spares callers from repeating `--transport mcp`, while an
// explicit flag still wins (and then fails loudly rather than silently
// switching protocols behind the operator's back).
func selectMCPTransport(cmd *cobra.Command) {
	if cmd != nil {
		if f := cmd.Root().PersistentFlags().Lookup("transport"); f != nil && f.Changed {
			return
		}
	}
	transport = "mcp"
}

// MCPAddressRequiredError indicates --address still names a Unix socket. The
// MCP endpoint is an HTTP listener, so a socket path cannot be dialed.
type MCPAddressRequiredError struct {
	Address string
}

// Error implements the error interface.
func (e *MCPAddressRequiredError) Error() string {
	return "the mcp commands need an MCP HTTP endpoint, but --address is the Unix socket " +
		e.Address + " (pass --address host:port, e.g. --address quicbaas.example.com:11443)"
}

// isUnixSocketAddress reports whether addr names a Unix socket path rather than
// a host:port endpoint.
func isUnixSocketAddress(addr string) bool {
	return strings.HasPrefix(addr, "/") || strings.HasSuffix(addr, ".sock")
}

// createSDKMCPCaller resolves the SDK client's transport into a raw MCP caller.
func createSDKMCPCaller(cmd *cobra.Command) (MCPCaller, error) {
	selectMCPTransport(cmd)

	// The default --address is the Unix socket the other command groups use.
	// Catch it here rather than letting the SDK build "https:///run/....sock".
	if transport == "mcp" && isUnixSocketAddress(address) {
		return nil, &MCPAddressRequiredError{Address: address}
	}

	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}

	caller, ok := sdk.Transport().(MCPCaller)
	if !ok {
		return nil, &MCPTransportRequiredError{Transport: transport}
	}
	return caller, nil
}

// =============================================================================
// Cobra Command Tree
// =============================================================================

// newMCPCommand creates the MCP command group.
func newMCPCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Model Context Protocol client commands",
		Long: `Speak the Model Context Protocol (MCP) to a QuicBaaS server.

The QuicBaaS daemon publishes its full MCP tool registry over HTTP. MCP hosts
such as Claude Code, Claude Desktop, and opencode instead spawn a child process
and speak JSON-RPC 2.0 over its stdin/stdout. "mcp serve --stdio" is that
bridge: it relays each JSON-RPC message between the host's pipe and the
configured server, so the host sees the server's real tool registry with the
server's own authorization applied.

The bridge deliberately owns no tool registry of its own. Tools, resources, and
the authorization gate all live in the daemon; duplicating them client-side
would fork the surface.

Examples:
  # Bridge an MCP host onto a QuicBaaS server
  quicbaas mcp serve --stdio --address quicbaas.example.com:11443

  # Discover and invoke tools interactively
  quicbaas mcp tools
  quicbaas mcp call quicbaas_kv_get --args '{"entityType":"user","id":1}'

Register the bridge with an MCP host via .mcp.json:
  {
    "mcpServers": {
      "quicbaas": {
        "command": "quicbaas",
        "args": ["mcp", "serve", "--stdio", "--address", "quicbaas.example.com:11443"]
      }
    }
  }`,
	}

	cmd.AddCommand(newMCPServeCommand())
	cmd.AddCommand(newMCPToolsCommand())
	cmd.AddCommand(newMCPCallCommand())

	return cmd
}

// =============================================================================
// Serve Command
// =============================================================================

// newMCPServeCommand creates the mcp serve subcommand.
func newMCPServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Bridge an MCP host's stdio pipe onto a QuicBaaS server",
		Long: `Read JSON-RPC 2.0 messages from stdin, forward them to the configured
QuicBaaS MCP endpoint, and write each response to stdout.

The bridge is transparent: "initialize", "tools/list", "tools/call", and every
other method reach the server verbatim, so the host negotiates directly with the
server. Authentication uses the same stored platform token and TLS material as
every other CLI command, honouring --address, --tls-ca, --tls-cert, --tls-key,
and --timeout.

Notifications (JSON-RPC messages with no "id"), including the host's
"notifications/initialized", are forwarded on the same session. They take no
reply, so a notification the server rejects is reported on stderr.

If the server reports the session expired (HTTP 404, "session terminated") for
any request other than "initialize", the bridge replays the host's last
successful "initialize" and "notifications/initialized" once and retries the
request, so a session the server swept mid-conversation does not break the
host. If that recovery fails, the original error is returned for the request.

The command blocks until stdin reaches EOF or the process is interrupted.

Examples:
  quicbaas mcp serve --stdio --address quicbaas.example.com:11443
  quicbaas mcp serve --stdio --address localhost:11443 --tls-ca ./data/certs/ca-chain.crt`,
		Args: cobra.NoArgs,
		RunE: runMCPServe,
	}

	cmd.Flags().BoolVar(&mcpStdio, "stdio", false,
		"Serve JSON-RPC over stdin/stdout (required; reserved for future transports)")

	return cmd
}

// StdioRequiredError indicates `mcp serve` was invoked without --stdio.
type StdioRequiredError struct{}

// Error implements the error interface.
func (e *StdioRequiredError) Error() string {
	return "mcp serve requires --stdio: it is the only transport an MCP host can spawn"
}

// runMCPServe executes the mcp serve command.
func runMCPServe(cmd *cobra.Command, args []string) error {
	if !mcpStdio {
		return formatError(&StdioRequiredError{})
	}

	caller, err := mcpCallerFactory(cmd)
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return serveMCPStdio(ctx, caller, os.Stdin, os.Stdout, os.Stderr)
}

// =============================================================================
// Session Handshake
// =============================================================================

// mcpClientName identifies the CLI in the "initialize" clientInfo it sends.
const mcpClientName = "quicbaas-cli"

// Lifecycle methods of the MCP handshake.
const (
	mcpMethodInitialize  = "initialize"
	mcpMethodInitialized = "notifications/initialized"
)

// mcpInitializeParams builds a spec-compliant "initialize" params object for
// the CLI's own MCP session.
func mcpInitializeParams() (json.RawMessage, error) {
	params := struct {
		ProtocolVersion string                 `json:"protocolVersion"`
		Capabilities    map[string]interface{} `json:"capabilities"`
		ClientInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"clientInfo"`
	}{
		ProtocolVersion: mcptransport.MCPVersion,
		Capabilities:    map[string]interface{}{},
	}
	params.ClientInfo.Name = mcpClientName
	params.ClientInfo.Version = version.Short()

	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, &MCPInitializeError{Cause: err}
	}
	return encoded, nil
}

// mcpInitialize opens an MCP session on caller. The server's Streamable HTTP
// endpoint is session-based: any request other than "initialize" that carries
// no session it issued is rejected with HTTP 404, and CallRPC deliberately
// performs no handshake of its own, so the one-shot verbs must send it.
func mcpInitialize(ctx context.Context, caller MCPCaller) error {
	params, err := mcpInitializeParams()
	if err != nil {
		return err
	}
	return mcpHandshake(ctx, caller, params)
}

// mcpHandshake runs the MCP lifecycle handshake with the given "initialize"
// params: the "initialize" request, then the "notifications/initialized"
// notification the specification requires before any other request.
func mcpHandshake(ctx context.Context, caller MCPCaller, params json.RawMessage) error {
	if _, err := caller.CallRPC(ctx, mcpMethodInitialize, params); err != nil {
		return &MCPInitializeError{Cause: err}
	}
	if err := caller.NotifyRPC(ctx, mcpMethodInitialized, nil); err != nil {
		return &MCPInitializeError{Cause: err}
	}
	return nil
}

// MCPInitializeError indicates the MCP "initialize" handshake failed.
type MCPInitializeError struct {
	Cause error
}

// Error implements the error interface.
func (e *MCPInitializeError) Error() string {
	return "MCP initialize failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *MCPInitializeError) Unwrap() error { return e.Cause }

// =============================================================================
// Stdio Bridge
// =============================================================================

// mcpMessage is an inbound JSON-RPC 2.0 message from the MCP host. A message
// without an ID is a notification and expects no reply.
type mcpMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// mcpResponse is an outbound JSON-RPC 2.0 response to the MCP host.
type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *mcpRPCError    `json:"error,omitempty"`
}

// mcpRPCError is a JSON-RPC 2.0 error object.
type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// JSON-RPC 2.0 error codes used by the bridge.
const (
	mcpCodeInvalidRequest = -32600
	mcpCodeInternalError  = -32603
)

// serveMCPStdio relays JSON-RPC messages between an MCP host and the server
// until in reaches EOF or ctx is cancelled. It never writes anything but
// well-formed JSON-RPC to out: a transport failure becomes an error response
// for the offending request, so one bad call cannot desynchronize the stream.
//
// errOut receives diagnostics that cannot be answered on the stream: a
// notification takes no reply, so a notification the server rejects is
// reported there instead.
func serveMCPStdio(ctx context.Context, caller MCPCaller, in io.Reader, out, errOut io.Writer) error {
	bridge := &mcpStdioBridge{caller: caller, errOut: errOut}
	decoder := json.NewDecoder(in)
	encoder := json.NewEncoder(out)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		var msg mcpMessage
		if err := decoder.Decode(&msg); err != nil {
			// EOF means the host closed the pipe: a clean shutdown.
			if err == io.EOF {
				return nil
			}
			return formatError(&MCPBridgeDecodeError{Cause: err})
		}

		resp := bridge.handle(ctx, &msg)
		if resp == nil {
			continue
		}
		if err := encoder.Encode(resp); err != nil {
			return formatError(&MCPBridgeEncodeError{Cause: err})
		}
	}
}

// mcpStdioBridge relays one host's messages onto a caller. It remembers the
// params of the host's last successful "initialize" so it can re-open a
// session the server expired without involving the host.
type mcpStdioBridge struct {
	caller     MCPCaller
	errOut     io.Writer
	initParams json.RawMessage
	// initialized is true once the host's "initialize" has succeeded, even
	// when it carried no params.
	initialized bool
}

// handle forwards one request and builds its response. It returns nil for
// notifications, which take no reply.
func (b *mcpStdioBridge) handle(ctx context.Context, msg *mcpMessage) *mcpResponse {
	if len(msg.ID) == 0 {
		b.notify(ctx, msg)
		return nil
	}
	if msg.Method == "" {
		return &mcpResponse{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Error:   &mcpRPCError{Code: mcpCodeInvalidRequest, Message: "missing method"},
		}
	}

	result, err := b.forward(ctx, msg)
	if err != nil {
		return &mcpResponse{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Error:   &mcpRPCError{Code: mcpCodeInternalError, Message: err.Error()},
		}
	}

	return &mcpResponse{JSONRPC: "2.0", ID: msg.ID, Result: result}
}

// notify forwards a host notification on the current session. A notification
// takes no reply, so a failure is reported on errOut rather than the stream.
func (b *mcpStdioBridge) notify(ctx context.Context, msg *mcpMessage) {
	if msg.Method == "" {
		fmt.Fprintln(b.errOut, "quicbaas mcp serve: ignoring JSON-RPC message with neither id nor method")
		return
	}
	if err := b.caller.NotifyRPC(ctx, msg.Method, msg.Params); err != nil {
		fmt.Fprintf(b.errOut, "quicbaas mcp serve: notification %s failed: %v\n", msg.Method, err)
	}
}

// forward sends msg to the server. When a non-"initialize" request fails
// because the server no longer recognizes the session, it replays the host's
// last successful "initialize" (followed by "notifications/initialized", which
// completes the handshake) and retries once; if there is nothing to replay
// or the recovery fails, the original error is returned unchanged.
func (b *mcpStdioBridge) forward(ctx context.Context, msg *mcpMessage) (json.RawMessage, error) {
	result, err := b.caller.CallRPC(ctx, msg.Method, msg.Params)
	if err == nil {
		if msg.Method == mcpMethodInitialize {
			b.initParams = msg.Params
			b.initialized = true
		}
		return result, nil
	}

	if msg.Method == mcpMethodInitialize || !b.initialized || !mcptransport.IsSessionTerminated(err) {
		return nil, err
	}

	if initErr := mcpHandshake(ctx, b.caller, b.initParams); initErr != nil {
		return nil, err
	}
	retried, retryErr := b.caller.CallRPC(ctx, msg.Method, msg.Params)
	if retryErr != nil {
		return nil, err
	}
	return retried, nil
}

// MCPBridgeDecodeError indicates a malformed JSON-RPC message on stdin.
type MCPBridgeDecodeError struct {
	Cause error
}

// Error implements the error interface.
func (e *MCPBridgeDecodeError) Error() string {
	return "failed to decode JSON-RPC message from stdin: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *MCPBridgeDecodeError) Unwrap() error { return e.Cause }

// MCPBridgeEncodeError indicates a failure writing a JSON-RPC response to stdout.
type MCPBridgeEncodeError struct {
	Cause error
}

// Error implements the error interface.
func (e *MCPBridgeEncodeError) Error() string {
	return "failed to encode JSON-RPC response to stdout: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *MCPBridgeEncodeError) Unwrap() error { return e.Cause }

// =============================================================================
// Tools Command
// =============================================================================

// newMCPToolsCommand creates the mcp tools subcommand.
func newMCPToolsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tools",
		Short: "List the tools published by the MCP server",
		Long: `Call "tools/list" against the configured QuicBaaS MCP endpoint and print
the server's reply.

Examples:
  quicbaas mcp tools
  quicbaas mcp tools --address quicbaas.example.com:11443`,
		Args: cobra.NoArgs,
		RunE: runMCPTools,
	}
}

// runMCPTools executes the mcp tools command.
func runMCPTools(cmd *cobra.Command, args []string) error {
	caller, err := mcpCallerFactory(cmd)
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	if err := mcpInitialize(ctx, caller); err != nil {
		return formatError(err)
	}

	result, err := caller.CallRPC(ctx, "tools/list", nil)
	if err != nil {
		return formatError(err)
	}

	return writeMCPResult(result, os.Stdout)
}

// =============================================================================
// Call Command
// =============================================================================

// newMCPCallCommand creates the mcp call subcommand.
func newMCPCallCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "call <tool>",
		Short: "Invoke an MCP tool",
		Long: `Call "tools/call" for the named tool and print the server's reply.

Examples:
  quicbaas mcp call quicbaas_health
  quicbaas mcp call quicbaas_kv_get --args '{"entityType":"user","id":1}'`,
		Args: cobra.ExactArgs(1),
		RunE: runMCPCall,
	}

	cmd.Flags().StringVar(&mcpToolArgs, "args", "", "Tool arguments as a JSON object")

	return cmd
}

// InvalidMCPToolArgsError indicates --args was not a JSON object.
type InvalidMCPToolArgsError struct {
	Cause error
}

// Error implements the error interface.
func (e *InvalidMCPToolArgsError) Error() string {
	return "--args must be a JSON object: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *InvalidMCPToolArgsError) Unwrap() error { return e.Cause }

// runMCPCall executes the mcp call command.
func runMCPCall(cmd *cobra.Command, args []string) error {
	params, err := buildMCPToolCallParams(args[0], mcpToolArgs)
	if err != nil {
		return formatError(err)
	}

	caller, err := mcpCallerFactory(cmd)
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	if err := mcpInitialize(ctx, caller); err != nil {
		return formatError(err)
	}

	result, err := caller.CallRPC(ctx, "tools/call", params)
	if err != nil {
		return formatError(err)
	}

	return writeMCPResult(result, os.Stdout)
}

// buildMCPToolCallParams validates the raw --args JSON and assembles the
// "tools/call" params object.
func buildMCPToolCallParams(tool, rawArgs string) (json.RawMessage, error) {
	call := struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments,omitempty"`
	}{Name: tool}

	if rawArgs != "" {
		if err := json.Unmarshal([]byte(rawArgs), &call.Arguments); err != nil {
			return nil, &InvalidMCPToolArgsError{Cause: err}
		}
	}

	encoded, err := json.Marshal(call)
	if err != nil {
		return nil, &InvalidMCPToolArgsError{Cause: err}
	}
	return encoded, nil
}

// writeMCPResult pretty-prints a raw JSON-RPC result. MCP results are already
// JSON, so the CLI re-indents them rather than imposing a table shape on a
// schema the server owns.
func writeMCPResult(result json.RawMessage, w io.Writer) error {
	if len(result) == 0 {
		fmt.Fprintln(w, "{}")
		return nil
	}

	var pretty interface{}
	if err := json.Unmarshal(result, &pretty); err != nil {
		// Not decodable as JSON: emit the server's bytes untouched.
		fmt.Fprintln(w, string(result))
		return nil
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(pretty)
}
