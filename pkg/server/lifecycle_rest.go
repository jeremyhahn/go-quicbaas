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

//go:build rest

package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/routing"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// requestTimeout derives the REST handler request timeout from the server
// write timeout. The handler timeout must be strictly less than the write
// timeout so that a timed-out handler can still flush the error response
// before the server closes the TCP connection.
//
// Rule: requestTimeout = writeTimeout − 5s, clamped to [5s, 25s].
// When writeTimeout is zero (not configured), defaults to 25s.
func requestTimeout(writeTimeout time.Duration) time.Duration {
	const (
		minTimeout = 5 * time.Second
		maxTimeout = 25 * time.Second
		margin     = 5 * time.Second
	)
	if writeTimeout <= 0 {
		return maxTimeout
	}
	t := writeTimeout - margin
	if t < minTimeout {
		return minTimeout
	}
	if t > maxTimeout {
		return maxTimeout
	}
	return t
}

// REST seam injection points for unit testing.
var (
	newREST        = restapi.NewServer
	restShutdownFn = func(srv *restapi.Server, ctx context.Context) error { return srv.Shutdown(ctx) }
)

// startRESTServer initializes and starts the REST server.
func (s *Server) startRESTServer() error {
	// Skip in embedded mode
	if s.config.Server.Embedded {
		return nil
	}

	// Skip if REST is not enabled
	if !s.config.Server.REST.Enabled {
		s.logger.Info("REST server disabled (enable with server.rest.enabled: true)")
		return nil
	}

	s.logger.Info("Initializing REST server...")

	// Clone TLS config to avoid concurrent writes from HTTP/2 setup
	// racing with other servers (QUIC, gRPC) that share s.tlsConfig.
	clonedTLSConfig := cloneTLSConfig(s.tlsConfig)

	// Append the operator's client CA pool when iam.mtls.tls_client_auth_ca_file
	// is configured. This extends the TLS handshake to accept certs issued by
	// the operator's PKI in addition to the daemon's own CA. A non-nil error
	// here indicates a malformed or missing PEM file and causes startup to abort.
	if clonedTLSConfig != nil {
		if err := appendOperatorClientCAsFn(s, clonedTLSConfig); err != nil {
			return err
		}
	}

	// Append the PIV login CA pool when iam.piv_login.enabled is true.
	// This enables the TLS handshake to solicit a client certificate from the
	// browser so the server can read r.TLS.PeerCertificates[0] in the PIV handler.
	if clonedTLSConfig != nil && s.config.IAM.PIVLogin.Enabled {
		if err := appendPIVClientCAsFn(s, clonedTLSConfig); err != nil {
			return err
		}
	}

	// Create REST server configuration
	restConfig := &restapi.Config{
		Address:        fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.REST.Port),
		ReadTimeout:    s.config.Server.ReadTimeout,
		WriteTimeout:   s.config.Server.WriteTimeout,
		IdleTimeout:    s.config.Server.IdleTimeout,
		MaxHeaderBytes: 1 << 20, // 1 MB
		MaxConnections: s.config.Server.MaxConnections,
		// RequestTimeout is the maximum time a handler may run before the
		// middleware cancels its context. It must be less than WriteTimeout
		// so the server can still flush the timeout response to the client.
		// Set to 25s: the REST handler gives Raft proposals 25s to commit
		// before returning a retryable 503. The HTTP server's WriteTimeout
		// (default 30s, configurable) allows ~5s to flush the 503 response.
		// Using a fixed 25s avoids a dependency on the WriteTimeout value
		// which may not be properly parsed from some config paths.
		RequestTimeout:    25 * time.Second,
		RateLimit:         0, // No rate limiting by default
		ExposeShardHeader: s.config.Server.ExposeShardHeader,
		NodeID:            s.config.Raft.NodeID,
		// TLS configuration
		TLSEnabled:    s.config.Security.TLSEnabled,
		TLSCertFile:   s.config.Security.TLSCertFile,
		TLSKeyFile:    s.config.Security.TLSKeyFile,
		TLSCAFile:     s.config.Security.TLSCAFile,
		TLSClientAuth: s.config.Security.TLSClientAuth,
		TLSConfig:     clonedTLSConfig,
		// PIV login configuration: thread operator config into REST handler.
		PIVClientCert: restapi.PIVClientCertConfig{
			Enabled:          s.config.IAM.PIVLogin.Enabled,
			TrustedRootsFile: s.config.IAM.PIVLogin.CAFile,
			TrustedIssuers:   s.config.IAM.PIVLogin.TrustedIssuers,
		},
		// SPIFFE trust domain for login tenant derivation (and pinning).
		SPIFFETrustDomain: s.config.IAM.SPIFFETrustDomain,
	}

	// Wire the home-realm discovery resolver over the shared/system store so
	// SPIFFE-less logins (browser/WebAuthn/federated) can resolve their tenant
	// while target tenant barriers are sealed. No-op when iam is not built.
	s.attachHomeRealmResolver(restConfig)

	// Create REST server using QuicBaaS facade
	restServer, err := newREST(s.quicBaaSService, restConfig, s.logger)
	if err != nil {
		return &qberrors.QBError{
			Code: qberrors.ErrInternal,
			Op:   "server.startRESTServer",
			Err:  &ServerCauseError{Message: "failed to create REST server", Cause: err},
		}
	}

	// Apply routing config to REST
	restVectorShardID := s.config.Vector.ShardID
	if restVectorShardID == 0 && s.config.Vector.Enabled {
		restVectorShardID = qbshards.PlatformShardID
	}
	restKMSShardID := s.config.KMS.Raft.ShardID
	if restKMSShardID == 0 {
		restKMSShardID = qbshards.PlatformShardID
	}
	restKVShards := kvShards(s.config.Server.Shards, restVectorShardID, s.config.TimeSeries.ShardID, restKMSShardID)
	if err := validateRouting(restKVShards, s.config.Server.EntityShardPins); err != nil {
		return &qberrors.QBError{
			Code: qberrors.ErrConfiguration,
			Op:   "server.startRESTServer",
			Err:  &ServerCauseError{Message: "invalid routing config", Cause: err},
		}
	}
	r2 := routing.NewWithShards(restKVShards)
	for et, shard := range s.config.Server.EntityShardPins {
		r2.SetEntityShard(et, shard)
	}
	restServer.SetShardRouter(r2)

	// Wire the agent registrar so the mTLS register handler can
	// publish freshly-registered daemons into the fleet registry.
	// The adapter forwards to the same orchestration.AgentRegistry
	// the dispatcher consults for routing.
	if s.agentRegistry != nil {
		registrar := newAgentRegistrarAdapter(s.agentRegistry)
		restServer.SetAgentRegistrar(registrar)

		// Wire the heartbeat self-healing recoverer. When the central
		// daemon restarts and loses its in-memory agent registry, an
		// agent's next heartbeat would normally 404 in a permanent
		// loop; the recoverer re-publishes the agent from its mTLS
		// peer cert so heartbeats become idempotent across restarts.
		// Constructor failures are logged-and-skipped because they
		// indicate a programmer mistake (nil dep) rather than a fatal
		// runtime condition — the original 404 path remains as a
		// safety net.
		recoverer, recErr := restapi.NewIAMHeartbeatRecoverer(restapi.IAMHeartbeatRecovererDeps{
			Registrar: registrar,
			Logger:    s.logger,
		})
		if recErr != nil {
			s.logger.Warnf("agent heartbeat recoverer disabled: %v", recErr)
		} else {
			restServer.SetAgentHeartbeatRecoverer(recoverer)
		}
	}

	// Plumb the gRPC ingest URL the agent-register handler advertises
	// to enrolling daemons. The REST TLS listener only advertises
	// http/1.1 ALPN (intentional, to dodge a Go HTTP/2 record-MAC bug);
	// gRPC requires h2 ALPN, so daemons MUST dial the gRPC listener
	// directly for PushEvents. We only advertise the URL when the
	// gRPC TCP listener is actually enabled; absent that, the daemon
	// skips event forwarding rather than dialing the wrong port.
	if s.config.Server.GRPC.Enabled && s.config.Server.GRPC.Port > 0 {
		restServer.SetEventIngestURL(formatEventIngestURL(
			s.config.Server.Host, s.config.Server.GRPC.Port))
	}

	// Wire the embedded TrustedCA into the REST server so the
	// /api/v1/xkms/ca/* endpoints delegate to go-xkms's production
	// CA handlers instead of returning 503. The REST server builds
	// its own adapter around the CA; we only pass the handle.
	if s.xkmsCA != nil {
		restServer.SetCertificateAuthority(s.xkmsCA)
	}

	// Wire the credential management service into the REST server so the
	// /v1/credentials/* endpoints serve real responses. The service is the
	// same instance the embedded SDK transport dispatches Security().
	// Credentials() calls to, ensuring a uniform credential surface across
	// every transport.
	if s.ceremonyServices != nil && s.ceremonyServices.Credential != nil {
		restServer.SetCredentialService(s.ceremonyServices.Credential)
		s.logger.Info("Credential service wired into REST server")
	}

	// Wire barrier and ceremony services into the REST server so the
	// root-level /v1/barrier/* and /v1/ceremony/* endpoints (which the qrdb
	// SDK REST sub-client targets) serve real responses. These are the same
	// facades the gRPC/QUIC/MCP transports use, keeping barrier/ceremony
	// behaviour symmetric across every transport.
	barrierSvc, ceremonySvc := s.buildBarrierCeremonyServices()
	restServer.SetBarrierService(barrierSvc)
	restServer.SetCeremonyService(ceremonySvc)
	s.logger.Info("Barrier and ceremony services wired into REST server")

	// Wire the seal service into the QuicBaaS facade so that REST
	// handlers can reach it via the service.SealService type assertion.
	// This must run after initBarrier (called earlier in Start) has
	// populated s.sealManager; when seal is disabled, sealManager is
	// nil and we leave the facade's sealService as nil, which causes
	// handlers to respond with ErrUnavailable rather than panic.
	if s.sealManager != nil {
		sealSvc := service.NewSealService(s.sealManager, logger.NewAdapter(s.logger))
		s.quicBaaSService.SetSealService(sealSvc)
		s.logger.Info("Seal service wired into service facade")
	}

	// Wire the RBAC authorization middleware onto the REST server. This mirrors
	// the MCP transport (configureMCPAuth in lifecycle_mcp.go): the same OIDC
	// tokens are validated and the same role model enforced, so an
	// authenticated-but-unprivileged caller is denied protected /api/v1
	// endpoints with 403 while public routes (health, login, oauth2/token,
	// activation) stay open. SetAuthorizationService rebuilds the router with
	// the middleware installed, so it MUST run before Start. The call is a
	// no-op when IAM/OIDC is unavailable (see the rest && !iam stub),
	// preserving the prior no-authz behaviour for storage-less / embedded boots.
	s.configureRESTAuthz(restServer)

	// Start REST server
	address := fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.REST.Port)
	if err := restServer.Start(address); err != nil {
		return &qberrors.QBError{
			Code: qberrors.ErrInternal,
			Op:   "server.startRESTServer",
			Err:  &ServerCauseError{Message: "failed to start REST server", Cause: err},
		}
	}

	s.restServer = restServer

	// Wire mTLS authenticators from operator config (rest && iam builds only;
	// other build combinations compile a no-op stub).
	if err := s.wireIAMMTLSAuthenticators(); err != nil {
		return err
	}

	s.logger.Infof("REST server started on %s", address)
	return nil
}

// cloneTLSConfig returns a clone of cfg, or nil if cfg is nil.
// It explicitly sets NextProtos to HTTP/1.1 only to prevent Go's automatic
// HTTP/2 enablement, which causes "tls: bad record MAC" errors when clients
// connect with ForceAttemptHTTP2: false.
func cloneTLSConfig(cfg *tls.Config) *tls.Config {
	if cfg == nil {
		return nil
	}
	cloned := cfg.Clone()
	cloned.NextProtos = []string{"http/1.1"}
	return cloned
}

// stopRESTServer gracefully shuts down the REST server.
func (s *Server) stopRESTServer(ctx context.Context) error {
	if s.restServer == nil {
		return nil
	}

	if err := restShutdownFn(s.restServer, ctx); err != nil {
		return &qberrors.QBError{
			Code: qberrors.ErrInternal,
			Op:   "server.stopRESTServer",
			Err:  &ServerCauseError{Message: "failed to shutdown REST server", Cause: err},
		}
	}

	return nil
}

// formatEventIngestURL builds the "https://host:port" URL the REST
// agent-register handler advertises to enrolling daemons as the gRPC
// PushEvents target. The host is the configured listener host
// verbatim; if it is a wildcard (0.0.0.0 / ::) the handler rewrites
// it per-request to the agent's view of the daemon (the request's
// Host header) so the daemon dials a target it can reach.
func formatEventIngestURL(host string, port int) string {
	return fmt.Sprintf("https://%s:%d", host, port)
}
