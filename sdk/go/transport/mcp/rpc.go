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

// Compile-time assertion that the MCP transport satisfies RPCCaller.
var _ RPCCaller = (*Transport)(nil)

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
