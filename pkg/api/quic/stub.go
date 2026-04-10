//go:build !quic

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

// Package quic provides stub implementations when QUIC support is not compiled.
// Build with -tags quic to enable full QUIC support.
package quic

import (
	"errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ErrQUICNotCompiled is returned when QUIC support is not compiled into the binary.
var ErrQUICNotCompiled = errors.New("quic support not compiled - build with -tags quic")

// Server is a stub type when QUIC support is not compiled.
type Server struct{}

// Config contains configuration for the QUIC server (stub).
type Config struct{}

// ServerOption is a functional option for configuring the Server (stub).
type ServerOption func(*Server)

// DefaultConfig returns a nil Config when QUIC is not compiled.
func DefaultConfig() *Config {
	return nil
}

// NewServer returns an error indicating QUIC support is not compiled.
func NewServer(_ service.QuicBaaS, _ *Config, _ *logger.Logger, _ ...ServerOption) (*Server, error) {
	return nil, ErrQUICNotCompiled
}

// Start returns an error indicating QUIC support is not compiled.
func (s *Server) Start() error {
	return ErrQUICNotCompiled
}

// Stop returns an error indicating QUIC support is not compiled.
func (s *Server) Stop() error {
	return ErrQUICNotCompiled
}

// IsRunning always returns false when QUIC is not compiled.
func (s *Server) IsRunning() bool {
	return false
}

// GetMetrics returns nil when QUIC is not compiled.
func (s *Server) GetMetrics() map[string]interface{} {
	return nil
}

// Client is a stub type when QUIC support is not compiled.
type Client struct{}

// ClientOption configures the QUIC client (stub).
type ClientOption func(*clientConfig)

type clientConfig struct{}

// NewClient returns an error indicating QUIC support is not compiled.
func NewClient(_ string, _ ...ClientOption) (*Client, error) {
	return nil, ErrQUICNotCompiled
}

// Close returns an error indicating QUIC support is not compiled.
func (c *Client) Close() error {
	return ErrQUICNotCompiled
}
