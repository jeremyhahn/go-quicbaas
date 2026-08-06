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
	"fmt"
	"time"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	qbsealedwindow "github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// MCP seam injection points for unit testing.
var (
	newMCP    = mcpapi.NewServer
	mcpStopFn = func(m *mcpapi.Server) error { return m.Stop() }
)

// startMCPServer starts the MCP (Model Context Protocol) server if enabled.
func (s *Server) startMCPServer() error {
	// Skip in embedded mode: embedded servers expose no network listeners
	// (gRPC/REST/QUIC apply the same guard), so no MCP HTTP listener — and
	// therefore no MCP authorization gate — is constructed either.
	if s.config.Server.Embedded {
		s.logger.Info("Running in embedded mode - skipping MCP server setup")
		return nil
	}

	if !s.config.Server.MCP.Enabled {
		s.logger.Debug("MCP server disabled in configuration")
		return nil
	}

	s.logger.Info("Starting MCP server...")

	// Create MCP server configuration. The MCP HTTP transport reuses the
	// daemon's in-memory tls.Config (built by initializeCA from OpaqueKeys)
	// so every public listener — REST, gRPC, QUIC, MCP — presents the same
	// CA-chained certificate. Without this the MCP listener would fall back
	// to plain HTTP and clients dialing https://host:11443 would fail with
	// "http: server gave HTTP response to HTTPS client".
	mcpConfig := &mcpapi.Config{
		ServerName:     "QuicBaaS",
		ServerVersion:  "1.0.0",
		Transport:      "http",
		HTTPAddr:       fmt.Sprintf("%s:%d", s.config.Server.ResolveBindHost(s.config.Server.MCP.BindAddress), s.config.Server.MCP.Port),
		RequestTimeout: 30 * time.Second,
		TLSConfig:      s.tlsConfig,
	}

	// Create MCP server using QuicBaaS facade
	mcpServer := newMCP(s.quicBaaSService, mcpConfig, s.logger)

	// Configure auth services if available from IAM components.
	// This allows the MCP server to validate tokens for authenticated operations
	// while still allowing public tools (like quicbaas_login) to work without
	// auth. Under RequireAuthz gate construction is fail-closed: a returned
	// *AuthzGateConstructionError aborts startup.
	if err := s.configureMCPAuth(mcpServer); err != nil {
		return err
	}

	// Install the probe-driven sealed-window gate on the tool dispatcher. A
	// pass-through while the barrier is unsealed; the instant the barrier is
	// sealed (post-boot `barrier seal`), only the canonical sealed-window
	// tool surface remains callable.
	mcpServer.SetSealedWindowGate(s.SealedWindowProbe(), qbsealedwindow.MCPAllowlist())

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
	// gRPC/REST/QUIC — quicbaas owns no barrier logic. When nil (qrdb not
	// yet online) the tools return TenantBarrierServiceUnavailableError.
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
	s.logger.Info("User and UserAuthFactor services wired into MCP server")

	// Start the MCP server. Start is non-blocking for both transports and
	// binds the HTTP listener SYNCHRONOUSLY, so a port conflict or bad
	// address is a startup error that aborts the daemon (process-fatal
	// transport construction) instead of a log line from a goroutine.
	if err := mcpServer.Start(); err != nil {
		return &qberrors.QBError{
			Code: qberrors.ErrInternal,
			Op:   "server.startMCPServer",
			Err:  &ServerCauseError{Message: "failed to start MCP server", Cause: err},
		}
	}

	s.mcpServer = mcpServer
	s.logger.Infof("MCP server started on %s:%d", s.config.Server.ResolveBindHost(s.config.Server.MCP.BindAddress), s.config.Server.MCP.Port)
	return nil
}

// stopMCPServer gracefully shuts down the MCP server.
func (s *Server) stopMCPServer() error {
	if s.mcpServer == nil {
		return nil
	}

	if err := mcpStopFn(s.mcpServer); err != nil {
		return &qberrors.QBError{
			Code: qberrors.ErrInternal,
			Op:   "server.stopMCPServer",
			Err:  &ServerCauseError{Message: "failed to shutdown MCP server", Cause: err},
		}
	}

	return nil
}

// configureMCPAuth configures authentication services on the MCP server.
// This wires up the auth manager from IAM components to enable token validation
// for authenticated MCP operations while still allowing public tools to work.
// Under RequireAuthz gate construction is fail-closed: a returned
// *AuthzGateConstructionError aborts startup.
func (s *Server) configureMCPAuth(mcpServer *mcpapi.Server) error {
	// Interface to get the internal auth manager for MCP token validation.
	// The auth manager provides token validation for authenticated operations.
	type internalAuthManagerGetter interface {
		GetInternalAuthManager() interfaces.AuthManager
	}

	// When RequireAuthz=true, install the qrdb-backed authorization decision
	// point (OIDC token validation + qrdb PDP + canonical tool mapper). When it
	// installs enforcement we skip the legacy token-only wiring below so the two
	// do not fight over the auth checker.
	installed, err := s.configureMCPQRDBAuthz(mcpServer)
	if err != nil {
		return err
	}
	if installed {
		return nil
	}

	// Try to get auth manager from the service via type assertion.
	// The auth manager is set on the service when IAM components are initialized.
	if getter, ok := s.quicBaaSService.(internalAuthManagerGetter); ok {
		if authManager := getter.GetInternalAuthManager(); authManager != nil {
			s.logger.Debug("Configuring MCP server with auth manager from service")
			mcpServer.SetAuthorizationService(nil, authManager)
		}
	}
	return nil
}
