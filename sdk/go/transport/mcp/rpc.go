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
	"encoding/json"
	stderrors "errors"
	"time"

	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// RPCCaller is implemented by transports that can carry an opaque JSON-RPC 2.0
// call to a Model Context Protocol server. It exists so callers that speak MCP
// natively — an MCP host bridging its stdio pipe onto a remote QuicBaaS server,
// or a tool inspector — can reach the server's real tool registry without the
// SDK's service/method-to-tool-name mapping getting in the way.
//
// Only the MCP transport implements it; other transports carry no MCP framing.
type RPCCaller interface {
	CallRPC(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
}

// RPCNotifier is implemented by transports that can carry an opaque JSON-RPC
// 2.0 notification (a message with no id, which takes no reply) to a Model
// Context Protocol server. The MCP lifecycle needs it: a client completes the
// handshake by sending "notifications/initialized" after a successful
// "initialize", before any other request.
type RPCNotifier interface {
	NotifyRPC(ctx context.Context, method string, params json.RawMessage) error
}

// Compile-time assertions that the MCP transport satisfies RPCCaller and
// RPCNotifier.
var (
	_ RPCCaller   = (*Transport)(nil)
	_ RPCNotifier = (*Transport)(nil)
)

// CallRPC sends an opaque JSON-RPC 2.0 request to the connected MCP server and
// returns the raw "result" member of the response.
//
// Unlike Request, CallRPC performs no tool-name mapping, no argument
// conversion, and no implicit "initialize" handshake: the method and params are
// forwarded verbatim. Callers that already speak MCP (for example a stdio-to-
// HTTP bridge) own the protocol handshake themselves and must be able to send
// "initialize", "tools/list", and "tools/call" exactly as their peer wrote them.
//
// A JSON-RPC error response is returned as a typed SDK error; params may be nil
// for methods that take none.
func (t *Transport) CallRPC(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	if t.closed.Load() {
		return nil, errors.NewConnection("mcp.CallRPC", "transport is closed")
	}
	if method == "" {
		return nil, errors.NewInvalidInput("mcp.CallRPC", "method is required")
	}

	// A nil json.RawMessage marshals to the literal "null"; drop the member
	// entirely instead so parameterless methods send no "params" at all.
	var rpcParams interface{}
	if len(params) > 0 {
		rpcParams = params
	}

	return t.sendRPC(ctx, method, rpcParams)
}

// IsSessionTerminated reports whether err (as returned by CallRPC) means the
// server rejected the request because it carried no live MCP session: the
// Streamable HTTP server answers any non-"initialize" request without a
// recognized Mcp-Session-Id with HTTP 404, which the MCP client surfaces as a
// session-terminated error. The remedy is to send "initialize" again and
// retry; a caller that forwards another peer's frames (such as a stdio
// bridge) uses this to recover from a session the server expired.
func IsSessionTerminated(err error) bool {
	return stderrors.Is(err, mcptransport.ErrSessionTerminated)
}

// NotifyRPC sends an opaque JSON-RPC 2.0 notification to the connected MCP
// server over the same session as CallRPC. Like CallRPC it performs no
// mapping: the method and params are forwarded as the caller wrote them.
// params, when present, must be a JSON object (the only shape JSON-RPC allows
// for named notification params); it may be nil for notifications that take
// none.
func (t *Transport) NotifyRPC(ctx context.Context, method string, params json.RawMessage) error {
	if t.closed.Load() {
		return errors.NewConnection("mcp.NotifyRPC", "transport is closed")
	}
	if method == "" {
		return errors.NewInvalidInput("mcp.NotifyRPC", "method is required")
	}

	notification := mcpgo.JSONRPCNotification{
		JSONRPC:      "2.0",
		Notification: mcpgo.Notification{Method: method},
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &notification.Params); err != nil {
			return errors.Wrap(err, errors.CodeInvalidInput, "mcp.NotifyRPC", "params must be a JSON object")
		}
	}

	raw, err := t.ensureRawTransport(ctx)
	if err != nil {
		return err
	}
	if err := raw.SendNotification(ctx, notification); err != nil {
		return t.convertHTTPError(err, "mcp.NotifyRPC")
	}

	t.lastHealth.Store(time.Now().UnixNano())
	return nil
}
