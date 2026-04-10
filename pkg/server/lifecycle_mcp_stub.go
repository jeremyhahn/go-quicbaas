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

import "errors"

// ErrMCPNotCompiled indicates MCP support was not compiled into the binary.
var ErrMCPNotCompiled = errors.New("mcp support not compiled - build with -tags mcp")

// MCP seam injection points - stub implementations when MCP is not compiled.
// These variables maintain interface compatibility with the mcp build.
//
//nolint:unused // Required for seam injection compatibility with mcp build
var (
	_newMCP    interface{} = nil
	_mcpStopFn interface{} = nil
)

// startMCPServer returns ErrMCPNotCompiled when MCP is not compiled.
func (s *Server) startMCPServer() error {
	if s.config.Server.MCP.Enabled {
		s.logger.Warn("MCP server requested but not compiled - build with -tags mcp")
		return ErrMCPNotCompiled
	}
	return nil
}

// stopMCPServer is a no-op when MCP is not compiled.
func (s *Server) stopMCPServer() error {
	return nil
}
