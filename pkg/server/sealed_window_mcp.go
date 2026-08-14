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

package server

import (
	"time"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
)

// startSealedMCPServer brings up the sealed-phase MCP listener on the same
// address the full boot will use, composed over the minimal sealed facade
// and gated with the constant-sealed probe + the canonical MCP tool
// allowlist. Only the qrdb_barrier_* / qrdb_ceremony_* unseal-flow tools are
// callable; every other tool is rejected with the sealed envelope.
//
// No authorization gate is constructed in this phase (see
// startSealedRESTServer for the rationale). The listener bind is
// synchronous: a bind failure aborts the sealed boot (process-fatal
// transport construction).
func (s *Server) startSealedMCPServer() error {
	if !s.config.Server.MCP.Enabled {
		return nil
	}

	mcpConfig := &mcpapi.Config{
		ServerName:     "QuicBaaS",
		ServerVersion:  "1.0.0",
		Transport:      "http",
		HTTPAddr:       sealedListenAddress(s.config, s.config.Server.MCP.Port),
		RequestTimeout: 30 * time.Second,
		TLSConfig:      s.tlsConfig,
	}

	mcpServer := newMCP(s.sealedWindowFacade, mcpConfig, s.logger)

	// Barrier / ceremony / credential wiring: same facades as every other
	// transport, resolved through the sealed-window client.
	barrierSvc, ceremonySvc := s.buildBarrierCeremonyServices()
	mcpServer.SetBarrierService(barrierSvc)
	mcpServer.SetCeremonyService(ceremonySvc)
	if s.ceremonyServices != nil && s.ceremonyServices.Credential != nil {
		mcpServer.SetCredentialService(s.ceremonyServices.Credential)
	}

	// Constant-sealed gate on the tool dispatcher.
	mcpServer.SetSealedWindowGate(constantSealedProbe{}, s.OperationalProbe(),
		sealedwindow.MCPAllowlist())

	if err := mcpServer.Start(); err != nil {
		return &SealedWindowBootError{Stage: "sealed mcp listener", Cause: err}
	}

	s.mcpServer = mcpServer
	s.logger.Infof("Sealed-window MCP server started on %s", mcpConfig.HTTPAddr)
	return nil
}
