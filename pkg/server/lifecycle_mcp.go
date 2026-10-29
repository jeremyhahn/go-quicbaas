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

//go:build mcp

package server

import (
	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	qbsealedwindow "github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
)

// newMCP is a seam injection point for unit testing buildMCPServer.
var newMCP = mcpapi.NewServer

// buildMCPServer assembles a fully wired *mcpapi.Server tool registry from
// the composed server state. quicbaas runs no MCP listener of its own: the
// real Model Context Protocol surface is served by go-qrdb's unified
// multi-protocol server (mcp-go StreamableHTTP), which dispatches into this
// tool registry through the unified-API-server module (pkg/apimodule
// RegisterMCP / ModuleMCPServer below). buildMCPServer wires every backing
// service the registry's tools need so the module's real MCP tools serve
// live responses, not placeholder errors.
func (s *Server) buildMCPServer() (*mcpapi.Server, error) {
	mcpConfig := &mcpapi.Config{
		ServerName:    "QuicBaaS",
		ServerVersion: "1.0.0",
	}

	// Create MCP tool registry using the QuicBaaS facade.
	mcpServer := newMCP(s.quicBaaSService, mcpConfig, s.logger)

	// Install the probe-driven sealed-window gate on the tool dispatcher. A
	// pass-through while the barrier is unsealed; the instant the barrier is
	// sealed (post-boot `barrier seal`), only the canonical sealed-window
	// tool surface remains callable.
	mcpServer.SetSealedWindowGate(s.SealedWindowProbe(), s.OperationalProbe(),
		qbsealedwindow.MCPAllowlist())

	// Wire the qrdb credential management service into the MCP server so
	// the qrdb_credential_* tools serve real responses against the same
	// in-process service used by the embedded SDK transport.
	if s.ceremonyServices != nil && s.ceremonyServices.Credential != nil {
		mcpServer.SetCredentialService(s.ceremonyServices.Credential)
		s.logger.Info("Credential service wired into MCP server")
	}

	// Wire the qrdb barrier and ceremony service facades into the MCP server
	// so the qrdb_barrier_* / qrdb_ceremony_* tools serve real responses. The
	// facades are built identically to the gRPC transport (see
	// buildBarrierCeremonyServices) so behaviour is symmetric across every
	// transport. When the qrdb client is not yet online the facades carry a
	// nil transport and each tool short-circuits with a typed
	// service-unavailable error instead of a nil-pointer panic.
	barrierSvc, ceremonySvc := s.buildBarrierCeremonyServices()
	mcpServer.SetBarrierService(barrierSvc)
	mcpServer.SetCeremonyService(ceremonySvc)
	s.logger.Info("Barrier and Ceremony services wired into MCP server")

	// Wire the per-tenant barrier facade into the MCP server so the
	// qrdb_tenant_barrier_* tools serve real responses. Same facade as
	// gRPC/REST — quicbaas owns no barrier logic. When nil (qrdb not yet
	// online) the tools return TenantBarrierServiceUnavailableError.
	mcpServer.SetTenantBarrierService(s.buildTenantBarrierService())
	s.logger.Info("Tenant barrier service wired into MCP server")

	// Wire the qrdb user + user auth-factor lazy proxies into the MCP
	// server so the qrdb_user_get* / qrdb_user_authfactor_* tools serve
	// real responses. The proxies resolve the embedded node's servicers
	// PER CALL (the services are nil until Unseal completes the boot on
	// seal-enabled paths, so eager capture is forbidden); while sealed,
	// calls fail closed with the typed QRDBUserServiceUnavailableError.
	mcpServer.SetUserService(newQRDBUserServicerProxy(s))
	mcpServer.SetUserAuthFactorService(newQRDBUserAuthFactorServicerProxy(s))
	mcpServer.SetRoleAssignmentAuthorizer(s.capturedNodeAuthorizer)
	s.logger.Info("User and UserAuthFactor services wired into MCP server")

	return mcpServer, nil
}

// ModuleMCPServer resolves the *mcpapi.Server backing the unified-API-server
// module's real MCP tools (pkg/apimodule RegisterMCP). It lazily constructs
// the tool registry (buildMCPServer) on first use, once, and memoizes it —
// every module tool call after that shares the SAME registry state. Returns
// nil while the QuicBaaS facade is still composing — module tools answer a
// tool error until then.
func (s *Server) ModuleMCPServer() *mcpapi.Server {
	s.mu.RLock()
	composing := s.quicBaaSService == nil
	s.mu.RUnlock()
	if composing {
		return nil
	}

	s.moduleMCPOnce.Do(func() {
		srv, err := s.buildMCPServer()
		if err != nil {
			s.logger.Errorf("module MCP binding: server construction failed: %v", err)
			return
		}
		s.mu.Lock()
		s.moduleMCP = srv
		s.mu.Unlock()
	})

	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.moduleMCP
}
