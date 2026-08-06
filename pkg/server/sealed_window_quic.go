//go:build quic

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
	"crypto/tls"
	"fmt"
	"time"

	quicapi "github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
)

// startSealedQUICServer brings up the sealed-phase QUIC listener on the same
// address the full boot will use, composed over the minimal sealed facade
// and gated with the constant-sealed probe + the canonical QUIC allowlists
// (native opcode + qrdb-quic bridge). Only the barrier / ceremony / health
// surface is reachable; everything else is rejected with StatusUnavailable.
//
// No authorization gate is constructed in this phase (see
// startSealedRESTServer for the rationale).
func (s *Server) startSealedQUICServer() error {
	if !s.config.Server.QUIC.Enabled || s.config.Server.QUIC.Port == 0 {
		return nil
	}
	// QUIC mandates TLS; the full boot skips QUIC without TLS, mirror that.
	if !s.config.Security.TLSEnabled || s.tlsConfig == nil {
		s.logger.Warn("Sealed-window QUIC server requires TLS; skipping QUIC during the sealed window")
		return nil
	}

	tlsConfig := s.tlsConfig.Clone()
	tlsConfig.MinVersion = tls.VersionTLS13
	tlsConfig.NextProtos = []string{"quicbaas-quic", quicapi.QRDBALPN}

	quicConfig := &quicapi.Config{
		ListenAddr:                 fmt.Sprintf("%s:%d", s.config.Server.ResolveBindHost(s.config.Server.QUIC.BindAddress), s.config.Server.QUIC.Port),
		TLSConfig:                  tlsConfig,
		MaxStreams:                 1000,
		MaxIdleTimeout:             30 * time.Second,
		MaxIncomingStreams:         1000,
		MaxIncomingUniStreams:      100,
		KeepAlivePeriod:            15 * time.Second,
		HandshakeIdleTimeout:       5 * time.Second,
		MaxReceiveBufferSize:       1 * 1024 * 1024,
		InitialStreamReceiveWindow: 512 * 1024,
		MaxStreamReceiveWindow:     6 * 1024 * 1024,
		EnableMetrics:              true,
	}

	quicServer, err := newQUIC(s.sealedWindowFacade, quicConfig, s.logger)
	if err != nil {
		return &SealedWindowBootError{Stage: "sealed quic server", Cause: err}
	}

	// Barrier / ceremony / credential wiring: same facades as every other
	// transport, resolved through the sealed-window client.
	barrierSvc, ceremonySvc := s.buildBarrierCeremonyServices()
	quicServer.SetBarrierService(barrierSvc)
	quicServer.SetCeremonyService(ceremonySvc)
	if s.ceremonyServices != nil && s.ceremonyServices.Credential != nil {
		quicServer.SetQRDBCredentialService(s.ceremonyServices.Credential)
	}

	// Constant-sealed gate on both dispatch paths (native opcodes and the
	// qrdb-quic ALPN bridge).
	quicServer.SetSealedWindowGate(constantSealedProbe{},
		sealedwindow.QUICAllowlist(), sealedwindow.QRDBQUICAllowlist())

	if err := quicServer.Start(); err != nil {
		return &SealedWindowBootError{Stage: "sealed quic listener", Cause: err}
	}

	s.quicServer = quicServer
	s.logger.Infof("Sealed-window QUIC server started on %s", quicConfig.ListenAddr)
	return nil
}
