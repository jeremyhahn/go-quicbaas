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

// testharness_test.go provides newFakeMCPServer, a shared test double for
// SDK handler tests that need to exercise a real MCP tool call end to end:
// it stands up a REAL in-process Model Context Protocol server
// (github.com/mark3labs/mcp-go/server, Streamable HTTP) registering exactly
// one tool, and wires a *Transport to it. This replaced a hand-rolled
// JSON-RPC httptest double when quicbaas adopted the real MCP protocol; the
// contract (mcpToolExpectation / newFakeMCPServer) is unchanged so handler
// tests built on it did not need to change.

package mcp

import (
	"context"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	mcpservertest "github.com/mark3labs/mcp-go/server/servertest"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mcpToolExpectation captures what a test expects the SDK to send and what
// the fake server should return.
type mcpToolExpectation struct {
	// wantTool is the MCP tool name the SDK must call. The fake server
	// registers exactly this tool; a call under any other name fails with a
	// real "tool not found" MCP error.
	wantTool string
	// assertArgs runs custom assertions on the decoded tool arguments.
	assertArgs func(t *testing.T, args map[string]interface{})
	// result is the JSON object the tool returns inside its text content.
	// When toolError is true the result text is surfaced as a tool error.
	result    map[string]interface{}
	toolError bool
	// toolErrorCode, when set, is sent as the tool error's structured
	// content in the {"code": N} shape the quicbaas server emits.
	toolErrorCode int
	// rawText overrides result with a literal text content payload
	// (used to test malformed/non-JSON responses).
	rawText string
}

// newFakeMCPServer starts a real in-process MCP server answering the
// protocol handshake and a single tools/call for exp.wantTool, applying exp.
// It returns a transport wired to the server and a cleanup func.
func newFakeMCPServer(t *testing.T, exp mcpToolExpectation) (*Transport, func()) {
	t.Helper()

	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	srv.AddTool(mcpgo.NewTool(exp.wantTool), func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if req.Params.Name != exp.wantTool {
			t.Errorf("tool name = %q, want %q", req.Params.Name, exp.wantTool)
		}
		if exp.assertArgs != nil {
			exp.assertArgs(t, req.GetArguments())
		}

		text := exp.rawText
		if text == "" {
			text = string(mustMarshalJSON(exp.result))
		}
		if exp.toolError {
			result := mcpgo.NewToolResultError(text)
			if exp.toolErrorCode != 0 {
				result.StructuredContent = map[string]interface{}{"code": exp.toolErrorCode}
			}
			return result, nil
		}
		return mcpgo.NewToolResultText(text), nil
	})

	httpSrv := mcpservertest.NewTestStreamableHTTPServer(srv)

	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	if err != nil {
		httpSrv.Close()
		t.Fatalf("failed to create transport: %v", err)
	}
	return tr, func() {
		_ = tr.Close()
		httpSrv.Close()
	}
}
