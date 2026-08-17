//go:build !mcp

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

// Package mcp provides stub implementations when MCP support is not compiled.
// Build with -tags mcp to enable the full MCP tool registry.
package mcp

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Server is a stub for the MCP tool-registry server when MCP support is not
// compiled.
type Server struct{}

// Config contains MCP server configuration (stub).
type Config struct {
	ServerName    string
	ServerVersion string
}

// NewServer returns nil because MCP support is not compiled.
func NewServer(_ service.QuicBaaS, _ *Config, _ *logger.Logger) *Server {
	return nil
}

// DefaultConfig returns nil because MCP support is not compiled.
func DefaultConfig() *Config {
	return nil
}

// SetBarrierService is a no-op because MCP support is not compiled.
func (s *Server) SetBarrierService(_ service.BarrierServicer) {}

// SetCeremonyService is a no-op because MCP support is not compiled.
func (s *Server) SetCeremonyService(_ service.CeremonyServicer) {}

// SetTenantBarrierService is a no-op because MCP support is not compiled.
func (s *Server) SetTenantBarrierService(_ service.TenantBarrierServicer) {}
