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

//go:build !mcp

package server

import (
	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
)

// ModuleMCPServer is unavailable without the mcp build tag: the module's MCP
// hook (pkg/apimodule module_mcp.go) is equally mcp-gated, so no caller can
// observe a surface here.
func (s *Server) ModuleMCPServer() *mcpapi.Server {
	return nil
}
