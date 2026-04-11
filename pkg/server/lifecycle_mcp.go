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
	if !s.config.Server.MCP.Enabled {
		s.logger.Debug("MCP server disabled in configuration")
		return nil
	}

	s.logger.Info("Starting MCP server...")

	// Create MCP server configuration
	mcpConfig := &mcpapi.Config{
		ServerName:     "QuicBaaS",
		ServerVersion:  "1.0.0",
		Transport:      "http",
		HTTPAddr:       fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.MCP.Port),
		RequestTimeout: 30 * time.Second,
	}

	// Create MCP server using QuicBaaS facade
	mcpServer := newMCP(s.quicBaaSService, mcpConfig, s.logger)

	// Configure auth services if available from IAM components.
	// This allows the MCP server to validate tokens for authenticated operations
	// while still allowing public tools (like quicbaas_login) to work without auth.
	s.configureMCPAuth(mcpServer)

	// Start MCP server in a goroutine (non-blocking for HTTP transport)
	go func() {
		if err := mcpServer.Start(); err != nil {
			s.logger.WithError(err).Error("MCP server stopped with error")
		}
	}()

	s.mcpServer = mcpServer
	s.logger.Infof("MCP server started on %s:%d", s.config.Server.Host, s.config.Server.MCP.Port)
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
func (s *Server) configureMCPAuth(mcpServer *mcpapi.Server) {
	// Interface to get the internal auth manager for MCP token validation.
	// The auth manager provides token validation for authenticated operations.
	type internalAuthManagerGetter interface {
		GetInternalAuthManager() interfaces.AuthManager
	}

	// Try to get auth manager from the service via type assertion.
	// The auth manager is set on the service when IAM components are initialized.
	if getter, ok := s.quicBaaSService.(internalAuthManagerGetter); ok {
		if authManager := getter.GetInternalAuthManager(); authManager != nil {
			s.logger.Debug("Configuring MCP server with auth manager from service")
			mcpServer.SetAuthorizationService(nil, authManager)
		}
	}
}
