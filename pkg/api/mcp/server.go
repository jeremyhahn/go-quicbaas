//go:build mcp

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

// Package mcp carries the QuicBaaS Model Context Protocol tool surface: the
// tool registry (registerTools, ExecuteTool) that implements every
// quicbaas_* / qrdb_* MCP tool over the service.QuicBaaS facade, and the
// authorization metadata (IsPublicTool, getToolQRDBPermission) that the
// unified-API-server module (pkg/apimodule, module_support.go) reads to
// register the real MCP tool surface on go-qrdb's mcp-go server.
//
// This package no longer runs a JSON-RPC transport of its own: the bespoke
// HTTP/stdio JSON-RPC 2.0 listener, its RFC 7591/8414/9728 endpoints, and its
// platform-token authorization checker were retired when quicbaas adopted
// go-qrdb's unified multi-protocol server. The real MCP surface is served by
// go-qrdb's mcp-go StreamableHTTP server; qrdb's MCP tool registry
// authorizes each call against the permissions pkg/apimodule declares
// (MCPToolPermissions) before InvokeTool ever runs.
package mcp

import (
	"sync"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Server carries the QuicBaaS MCP tool registry and the cross-transport
// sealed-window gate. It is composed by pkg/server (buildMCPServer) and
// handed to the unified-API-server module (pkg/apimodule) via a resolver so
// InvokeTool (module_support.go) can dispatch real MCP tool calls against
// the SAME registry state on every server transport.
type Server struct {
	service service.QuicBaaS
	logger  *logger.Logger
	config  *Config

	// sealedProbe + sealedToolAllow implement the sealed-window gate on the
	// tools/call dispatcher (see SetSealedWindowGate). While the probe
	// reports sealed, only allowlisted tools are callable.
	sealedMu        sync.RWMutex
	sealedProbe     SealedWindowProbe
	sealedToolAllow map[string]struct{}

	// sealedBypass is the shared sealed-window authentication-bypass
	// predicate (probe + canonical-table lookup), derived by
	// SetSealedWindowGate from the same probe as the gate above. Guarded by
	// sealedMu with the gate state.
	sealedBypass *sealedwindow.AuthBypass

	// tools is the registry backing InvokeTool, ModuleTools, and every
	// quicbaas_* / qrdb_* tool call the unified server dispatches.
	tools *ToolRegistry
}

// Config carries the cosmetic identity of the MCP tool surface. It no
// longer configures a transport: the unified server owns the listener,
// TLS, and request-timeout policy for every protocol it serves, MCP
// included.
type Config struct {
	// ServerName is the MCP server name reported by go-qrdb's mcp-go server.
	ServerName string

	// ServerVersion is the QuicBaaS version reported alongside ServerName.
	ServerVersion string
}

// NewServer creates a new MCP tool-registry server instance.
func NewServer(svc service.QuicBaaS, config *Config, log *logger.Logger) *Server {
	if config == nil {
		config = DefaultConfig()
	}

	if log == nil {
		log, _ = logger.New(logger.LogConfig{
			Level:     "info",
			Format:    "json",
			Output:    "stdout",
			Component: "mcp",
		})
	}

	return &Server{
		service: svc,
		logger:  log,
		config:  config,
		tools:   NewToolRegistry(svc, log),
	}
}

// DefaultConfig returns default MCP server configuration.
func DefaultConfig() *Config {
	return &Config{
		ServerName:    "QuicBaaS MCP Server",
		ServerVersion: "1.0.0",
	}
}

// SetBarrierService installs the BarrierServicer used by every
// quicbaas_barrier_* MCP tool. Lifecycle wiring calls this once the qrdb SDK
// BarrierClient is online. Passing nil takes the barrier surface offline;
// tools then return BarrierServiceUnavailableError so callers can distinguish
// "no service" from a transient failure.
func (s *Server) SetBarrierService(svc service.BarrierServicer) {
	if s.tools != nil {
		s.tools.SetBarrierService(svc)
	}
}

// SetCeremonyService installs the CeremonyServicer used by every
// quicbaas_ceremony_* MCP tool. Lifecycle wiring calls this once the qrdb
// SDK CeremonyClient is online. Passing nil takes the ceremony surface
// offline; tools then return CeremonyServiceUnavailableError.
func (s *Server) SetCeremonyService(svc service.CeremonyServicer) {
	if s.tools != nil {
		s.tools.SetCeremonyService(svc)
	}
}

// SetCredentialService installs the CredentialServicer used by every
// qrdb_credential_* MCP tool. Lifecycle wiring calls this once the qrdb
// credential service is online. Passing nil takes the credential surface
// offline; tools then return CredentialServiceUnavailableError.
func (s *Server) SetCredentialService(svc qrdbsdk.ServiceCredentialServicer) {
	if s.tools != nil {
		s.tools.SetCredentialService(svc)
	}
}

// SetTenantBarrierService installs the TenantBarrierServicer used by every
// qrdb_tenant_barrier_* MCP tool. Lifecycle wiring calls this once the qrdb
// SDK TenantClient is online. Passing nil takes the per-tenant barrier surface
// offline; tools then return TenantBarrierServiceUnavailableError so callers
// can distinguish "no service" from a transient failure.
func (s *Server) SetTenantBarrierService(svc service.TenantBarrierServicer) {
	if s.tools != nil {
		s.tools.SetTenantBarrierService(svc)
	}
}

// SetTenantGovernance installs the mcpTenantGovernancer used by the
// qrdb_tenant_update_quotas and qrdb_tenant_update_entitlements MCP tools.
// Lifecycle wiring calls this once the iam.DefaultTenantManager is online.
// Passing nil takes the governance surface offline; tools then return
// GovernanceServiceUnavailableError.
func (s *Server) SetTenantGovernance(g mcpTenantGovernancer) {
	if s.tools != nil {
		s.tools.SetTenantGovernance(g)
	}
}
