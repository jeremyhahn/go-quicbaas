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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
)

// RegisterMCP registers every quicbaas registry tool as a real MCP tool on
// the unified server's shared mcp-go server, carrying the tool's own
// declared input schema (the same schema the bespoke JSON-RPC server
// advertises via tools/list), so the real MCP surface is wire-equivalent to
// the legacy plane tool for tool. Tool names are sanitized into the MCP
// tool-name charset (mcpapi.ToolName); dispatch runs against the original
// registry name.
//
// Dispatch late-binds through the resolver exactly like the gRPC and HTTP
// surfaces: until the MCP wiring composes, every tool answers a tool error.
// Authorization is layered: qrdb's MCP middleware authorizes each tool call
// against the permissions this module declares via MCPToolPermissions
// (below), and the quicbaas-internal sealed-window gate runs inside
// InvokeTool.
func (m *Module) RegisterMCP(srv *mcpserver.MCPServer) {
	if m.resolveMCP == nil {
		return
	}

	for _, spec := range mcpapi.ModuleTools() {
		spec := spec
		schema, err := json.Marshal(spec.InputSchema)
		if err != nil || len(spec.InputSchema) == 0 {
			// A tool without a marshalable schema still registers, with
			// an open object schema, so the surface never silently
			// shrinks.
			schema = json.RawMessage(`{"type":"object"}`)
		}
		tool := mcpgo.NewToolWithRawSchema(spec.Tool, spec.Description, schema)
		srv.AddTool(tool, m.mcpToolHandler(spec))
	}
}

// Compile-time check: the module declares its MCP tool permissions into the
// unified server's single MCP authorization path.
var _ qrdbsdk.ModuleMCPToolPermissioner = (*Module)(nil)

// MCPToolPermissions declares every module tool's permission for the
// unified server's MCP tool registry: permission-mapped tools carry their
// qrdb-canonical resource:action, and public tools carry the identity-gated
// sentinel (authenticated at qrdb's gate, the tool itself is harmless
// discovery/health). Tools with no mapping in the canonical MCP
// authorization tables (authz_qrdb.go) are OMITTED: qrdb's tool registry
// default-denies an undeclared tool, preserving the bespoke gate's
// fail-closed posture for unmapped tools.
func (m *Module) MCPToolPermissions() map[string]qrdbsdk.Permission {
	specs := mcpapi.ModuleTools()
	perms := make(map[string]qrdbsdk.Permission, len(specs))
	for _, spec := range specs {
		switch {
		case spec.Public:
			perms[spec.Tool] = qrdbsdk.PermissionIdentityGated
		case spec.Resource != "" && spec.Action != "":
			perms[spec.Tool] = qrdbsdk.Permission(spec.Resource + ":" + spec.Action)
		}
	}
	return perms
}

// mcpToolHandler adapts one registry tool to an mcp-go tool handler with
// lazy backend resolution.
func (m *Module) mcpToolHandler(spec mcpapi.ToolSpec) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		backend := m.resolveMCP()
		if backend == nil {
			return mcpgo.NewToolResultError(
				"quicbaas mcp surface is not yet available (server still composing)"), nil
		}

		result, err := backend.InvokeTool(ctx, spec.Name, req.GetArguments())
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		out, err := json.Marshal(result)
		if err != nil {
			return mcpgo.NewToolResultError("result encoding failed: " + err.Error()), nil
		}
		return mcpgo.NewToolResultText(string(out)), nil
	}
}
