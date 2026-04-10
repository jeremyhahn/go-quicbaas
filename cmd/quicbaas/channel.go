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

// Package main provides channel CLI commands for QuicBaaS MCP integration.
package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/version"
)

// Channel serve command flags.
var (
	channelName            string
	channelAddress         string
	channelEnableReply     bool
	channelEnablePermRelay bool
	channelInstructions    string
)

// newChannelCommand creates the channel management command group.
func newChannelCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channel",
		Short: "Channel server for Claude Code integration",
		Long: `Manage the QuicBaaS MCP channel server that pushes events into
Claude Code sessions for distributed agent coordination.

The channel server acts as an MCP (Model Context Protocol) server that
communicates over stdio using JSON-RPC 2.0. It connects to a QuicBaaS
server to receive events and exposes them as MCP tools and notifications.

Usage in .mcp.json:
  {
    "command": "quicbaas",
    "args": ["channel", "serve", "--address", "quicbaas:9090"]
  }`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return &errors.DragonError{
				Code: errors.ErrInvalidInput,
				Op:   "channel",
				Err:  ErrNoCommand,
			}
		},
	}

	cmd.AddCommand(newChannelServeCommand())
	return cmd
}

// newChannelServeCommand creates the serve subcommand that starts the
// channel server as a standalone MCP process over stdio.
func newChannelServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP channel server over stdio",
		Long: `Start the QuicBaaS channel server as an MCP (Model Context Protocol)
process that communicates over stdin/stdout using JSON-RPC 2.0.

This command is designed to be spawned by Claude Code or other MCP-compatible
clients. It connects to a QuicBaaS server for event streaming and exposes
coordination tools for distributed agent workflows.

Features:
  - Event streaming from QuicBaaS to Claude Code sessions
  - Optional two-way reply tool for bidirectional communication
  - Permission relay for coordinating tool approvals across agents
  - Custom system prompt instructions injection

The server blocks and reads/writes JSON-RPC over stdio until terminated.

Examples:
  # Basic channel server
  quicbaas channel serve --address localhost:9090

  # Named channel with reply support
  quicbaas channel serve --name myagent --address quicbaas:9090 --enable-reply

  # With custom instructions
  quicbaas channel serve --address quicbaas:9090 \
    --instructions "You are a deployment coordinator"`,
		RunE: runChannelServe,
	}

	cmd.Flags().StringVar(&channelName, "name", "quicbaas",
		"Channel server name (used for identification in the MCP protocol)")
	cmd.Flags().StringVar(&channelAddress, "address", "",
		"QuicBaaS server address to connect to for events (host:port)")
	cmd.Flags().BoolVar(&channelEnableReply, "enable-reply", false,
		"Enable the two-way reply tool for bidirectional communication")
	cmd.Flags().BoolVar(&channelEnablePermRelay, "enable-permission-relay", false,
		"Enable permission relay for coordinating tool approvals across agents")
	cmd.Flags().StringVar(&channelInstructions, "instructions", "",
		"Custom instructions to inject into Claude's system prompt")

	return cmd
}

// runChannelServe starts the MCP channel server over stdio.
func runChannelServe(cmd *cobra.Command, args []string) error {
	if channelAddress == "" {
		return &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "channel.serve",
			Err:  &ChannelAddressRequiredError{},
		}
	}

	ctx, cancel := signal.NotifyContext(
		cmd.Context(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()

	return serveChannelStdio(ctx)
}

// serveChannelStdio runs the MCP channel server, reading JSON-RPC from stdin
// and writing responses to stdout. It blocks until the context is cancelled.
func serveChannelStdio(ctx context.Context) error {
	encoder := json.NewEncoder(os.Stdout)
	decoder := json.NewDecoder(os.Stdin)

	// Channel for signaling context completion.
	done := ctx.Done()

	for {
		select {
		case <-done:
			return nil
		default:
		}

		var msg channelMessage
		if err := decoder.Decode(&msg); err != nil {
			// EOF means the client disconnected.
			if err.Error() == "EOF" {
				return nil
			}
			return &errors.DragonError{
				Code: errors.ErrInternal,
				Op:   "channel.serve.decode",
				Err:  &ChannelDecodeError{Cause: err},
			}
		}

		response := handleChannelMessage(ctx, &msg)
		if response != nil {
			if err := encoder.Encode(response); err != nil {
				return &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "channel.serve.encode",
					Err:  &ChannelEncodeError{Cause: err},
				}
			}
		}
	}
}

// channelMessage represents a JSON-RPC 2.0 message for the MCP protocol.
type channelMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

// channelResponse represents a JSON-RPC 2.0 response.
type channelResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      json.RawMessage  `json:"id,omitempty"`
	Result  interface{}      `json:"result,omitempty"`
	Error   *channelRPCError `json:"error,omitempty"`
}

// channelRPCError represents a JSON-RPC 2.0 error object.
type channelRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// channelHandlerFunc is the signature for MCP method handlers.
type channelHandlerFunc func(ctx context.Context, msg *channelMessage) *channelResponse

// handlers is a map-based dispatch table for MCP methods.
var channelHandlers = map[string]channelHandlerFunc{
	"initialize": handleInitialize,
	"ping":       handlePing,
}

// handleChannelMessage dispatches an incoming JSON-RPC message to the
// appropriate handler using map-based dispatch for O(1) lookup.
func handleChannelMessage(ctx context.Context, msg *channelMessage) *channelResponse {
	handler, ok := channelHandlers[msg.Method]
	if !ok {
		return &channelResponse{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Error: &channelRPCError{
				Code:    -32601,
				Message: "method not found: " + msg.Method,
			},
		}
	}
	return handler(ctx, msg)
}

// handleInitialize responds to the MCP initialize request with server
// capabilities and metadata.
func handleInitialize(_ context.Context, msg *channelMessage) *channelResponse {
	result := map[string]interface{}{
		"server_info": map[string]string{
			"name":    channelName,
			"version": version.Short(),
		},
		"capabilities": map[string]interface{}{
			"reply":            channelEnableReply,
			"permission_relay": channelEnablePermRelay,
		},
	}

	if channelInstructions != "" {
		result["instructions"] = channelInstructions
	}

	return &channelResponse{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Result:  result,
	}
}

// handlePing responds to a ping request.
func handlePing(_ context.Context, msg *channelMessage) *channelResponse {
	return &channelResponse{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Result:  map[string]string{"status": "ok"},
	}
}

// ChannelAddressRequiredError indicates the --address flag is required.
type ChannelAddressRequiredError struct{}

// Error implements the error interface.
func (e *ChannelAddressRequiredError) Error() string {
	return "address is required: use --address to specify the QuicBaaS server"
}

// ChannelDecodeError indicates a failure to decode a JSON-RPC message from stdin.
type ChannelDecodeError struct {
	Cause error
}

// Error implements the error interface.
func (e *ChannelDecodeError) Error() string {
	return "failed to decode JSON-RPC message: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *ChannelDecodeError) Unwrap() error {
	return e.Cause
}

// ChannelEncodeError indicates a failure to encode a JSON-RPC response to stdout.
type ChannelEncodeError struct {
	Cause error
}

// Error implements the error interface.
func (e *ChannelEncodeError) Error() string {
	return "failed to encode JSON-RPC response: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *ChannelEncodeError) Unwrap() error {
	return e.Cause
}
