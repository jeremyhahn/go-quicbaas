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

//go:build quic

package server

import (
	"crypto/tls"
	"fmt"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	quicapi "github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	dragonerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// QUIC seam injection points for unit testing.
var (
	newQUIC    = quicapi.NewServer
	quicStopFn = func(q *quicapi.Server) error { return q.Stop() }
)

// startQUICServer initializes and starts the QUIC server.
func (s *Server) startQUICServer() error {
	// Skip in embedded mode
	if s.config.Server.Embedded {
		return nil
	}

	// Skip QUIC if not enabled
	if !s.config.Server.QUIC.Enabled {
		s.logger.Info("QUIC server disabled (enable with server.quic.enabled: true)")
		return nil
	}

	// Also skip if port is 0
	if s.config.Server.QUIC.Port == 0 {
		s.logger.Warn("QUIC enabled but port is 0, skipping")
		return nil
	}

	// QUIC mandates TLS at the protocol level; skip when TLS is disabled
	// rather than crashing the entire server.
	if !s.config.Security.TLSEnabled {
		s.logger.Warn("QUIC server requires TLS but TLS is disabled, skipping QUIC")
		return nil
	}

	s.logger.Info("Initializing QUIC server...")

	// Guard against nil TLS config (CA initialization must complete first)
	if s.tlsConfig == nil {
		return &dragonerrors.DragonError{
			Code: dragonerrors.ErrConfiguration,
			Op:   "server.startQUICServer",
			Err:  &api.ConfigurationError{Action: "TLS configuration is required for QUIC server"},
		}
	}

	// Use the CA-backed in-memory TLS config
	tlsConfig := s.tlsConfig.Clone()
	tlsConfig.MinVersion = tls.VersionTLS13
	tlsConfig.NextProtos = []string{"quicbaas-quic"}
	s.logger.Info("Using CA-backed TLS certificates for QUIC")

	// Create QUIC server configuration
	quicConfig := &quicapi.Config{
		ListenAddr:                 fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.QUIC.Port),
		TLSConfig:                  tlsConfig,
		MaxStreams:                 1000,
		MaxIdleTimeout:             30 * time.Second,
		MaxIncomingStreams:         1000,
		MaxIncomingUniStreams:      100,
		KeepAlivePeriod:            15 * time.Second,
		HandshakeIdleTimeout:       5 * time.Second,
		MaxReceiveBufferSize:       1 * 1024 * 1024, // 1MB
		InitialStreamReceiveWindow: 512 * 1024,      // 512KB
		MaxStreamReceiveWindow:     6 * 1024 * 1024, // 6MB
		EnableMetrics:              true,
	}

	// Create QUIC server using QuicBaaS facade
	quicServer, err := newQUIC(s.quicBaaSService, quicConfig, s.logger)
	if err != nil {
		return &dragonerrors.DragonError{
			Code: dragonerrors.ErrInternal,
			Op:   "server.startQUICServer",
			Err:  &api.OperationFailedError{Operation: "create QUIC server", Cause: err},
		}
	}

	// Start QUIC server
	if err := quicServer.Start(); err != nil {
		return &dragonerrors.DragonError{
			Code: dragonerrors.ErrInternal,
			Op:   "server.startQUICServer",
			Err:  &api.OperationFailedError{Operation: "start QUIC server", Cause: err},
		}
	}

	s.quicServer = quicServer
	s.logger.Infof("QUIC server started on %s:%d", s.config.Server.Host, s.config.Server.QUIC.Port)
	return nil
}

// stopQUICServer gracefully shuts down the QUIC server.
func (s *Server) stopQUICServer() error {
	if s.quicServer == nil {
		return nil
	}

	if err := quicStopFn(s.quicServer); err != nil {
		return api.NewOperationFailedError("shutdown QUIC server", err)
	}

	return nil
}
