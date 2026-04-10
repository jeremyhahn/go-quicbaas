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
// Build with -tags mcp to enable full MCP functionality.
package mcp

import (
	"errors"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ErrMCPNotCompiled is returned when MCP functions are called without the mcp build tag.
var ErrMCPNotCompiled = errors.New("mcp support not compiled - build with -tags mcp")

// Server is a stub for the MCP server when MCP support is not compiled.
type Server struct{}

// Config contains MCP server configuration (stub).
type Config struct {
	ServerName               string
	ServerVersion            string
	Transport                string
	HTTPAddr                 string
	RequestTimeout           time.Duration
	MaxConcurrentRequests    int
	AuthorizationService     interfaces.AuthorizationService
	AuthManager              interfaces.AuthManager
	MCPAuth                  *MCPAuthConfig
	EnableClientRegistration bool
}

// MCPAuthConfig contains MCP authorization configuration (stub).
type MCPAuthConfig struct {
	ResourceURI         string
	AuthorizationServer string
	Scopes              []string
}

// NewServer returns an error because MCP support is not compiled.
func NewServer(_ service.QuicBaaS, _ *Config, _ *logger.Logger) *Server {
	return nil
}

// DefaultConfig returns nil because MCP support is not compiled.
func DefaultConfig() *Config {
	return nil
}

// Start returns an error because MCP support is not compiled.
func (s *Server) Start() error {
	return ErrMCPNotCompiled
}

// Stop returns an error because MCP support is not compiled.
func (s *Server) Stop() error {
	return ErrMCPNotCompiled
}

// SetAuthorizationService is a no-op because MCP support is not compiled.
func (s *Server) SetAuthorizationService(_ interfaces.AuthorizationService, _ interfaces.AuthManager) {
}

// SetMCPAuthConfig is a no-op because MCP support is not compiled.
func (s *Server) SetMCPAuthConfig(_ *MCPAuthConfig) {
}

// SetAppRegistrar is a no-op because MCP support is not compiled.
func (s *Server) SetAppRegistrar(_ AppRegistrar) {
}

// EnableClientRegistrationEndpoint is a no-op because MCP support is not compiled.
func (s *Server) EnableClientRegistrationEndpoint(_ bool) {
}

// AppRegistrar is a stub interface for application registration.
type AppRegistrar interface{}
