//go:build rest

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

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// startSealedRESTServer brings up the sealed-phase REST listener on the same
// address the full boot will use, composed over the minimal sealed facade
// and gated with the constant-sealed probe + the canonical REST allowlist.
// Only the barrier / ceremony / init / bootstrap / health surface is
// reachable; everything else returns 503 with qrdb's sealed envelope.
//
// No authorization gate is constructed in this phase: there is no OIDC
// provider while sealed, the sealed-window gate is the control, and the
// allowlisted handlers enforce their own credentials (officer cert + nonce
// checks) exactly as on a qrdb node.
func (s *Server) startSealedRESTServer() error {
	if !s.config.Server.REST.Enabled {
		return nil
	}

	clonedTLSConfig := cloneTLSConfig(s.tlsConfig)

	// One address for both the server config and Start: the sealed-phase
	// bind host (loopback by default). Server.Host and REST.BindAddress do
	// not apply to the pre-unseal surface.
	address := sealedListenAddress(s.config, s.config.Server.REST.Port)

	restConfig := &restapi.Config{
		Address:           address,
		ReadTimeout:       s.config.Server.ReadTimeout,
		WriteTimeout:      s.config.Server.WriteTimeout,
		IdleTimeout:       s.config.Server.IdleTimeout,
		MaxHeaderBytes:    1 << 20,
		MaxConnections:    s.config.Server.MaxConnections,
		RequestTimeout:    25 * time.Second,
		NodeID:            s.config.Raft.NodeID,
		TLSEnabled:        s.config.Security.TLSEnabled,
		TLSConfig:         clonedTLSConfig,
		SPIFFETrustDomain: s.config.IAM.SPIFFETrustDomain,
	}

	restServer, err := newREST(s.sealedWindowFacade, restConfig, s.logger)
	if err != nil {
		return &SealedWindowBootError{Stage: "sealed rest server", Cause: err}
	}

	// Barrier / ceremony wiring: same facades as every other transport,
	// resolved through the sealed-window client. Credential management is
	// NOT wired here: pkg/api/sealedwindow's REST allowlist deliberately
	// excludes /v1/credentials from the sealed window (qrdb keeps
	// versioned-credential rotation off the sealed window; the ceremony
	// flows deliver boot credentials instead), and quicbaas's REST server no
	// longer carries a credential facade at all now that go-qrdb's unified
	// server serves /v1/credentials natively post-unseal.
	barrierSvc, ceremonySvc := s.buildBarrierCeremonyServices()
	restServer.SetBarrierService(barrierSvc)
	restServer.SetCeremonyService(ceremonySvc)

	// Trust bootstrap is stateless composition over go-xkms; first-touch CA
	// discovery must work in every daemon state.
	restServer.SetBootstrapService(service.NewBootstrapService(nil))

	// Constant-sealed gate: this listener only ever serves the sealed
	// window; it is torn down when the barrier unseals and the full boot
	// replaces it.
	restServer.SetSealedWindowGate(constantSealedProbe{}, s.OperationalProbe(),
		sealedwindow.RESTAllowlist())

	if err := restServer.Start(address); err != nil {
		return &SealedWindowBootError{Stage: "sealed rest listener", Cause: err}
	}

	s.restServer = restServer
	s.logger.Infof("Sealed-window REST server started on %s", address)
	return nil
}
