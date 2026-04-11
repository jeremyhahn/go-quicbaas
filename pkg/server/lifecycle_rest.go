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
	dragonerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/routing"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

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

	// Create REST server configuration
	restConfig := &restapi.Config{
		Address:           fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.REST.Port),
		ReadTimeout:       s.config.Server.ReadTimeout,
		WriteTimeout:      s.config.Server.WriteTimeout,
		IdleTimeout:       s.config.Server.IdleTimeout,
		MaxHeaderBytes:    1 << 20, // 1 MB
		MaxConnections:    s.config.Server.MaxConnections,
		RequestTimeout:    30 * time.Second,
		RateLimit:         0, // No rate limiting by default
		ExposeShardHeader: s.config.Server.ExposeShardHeader,
		// TLS configuration
		TLSEnabled:    s.config.Security.TLSEnabled,
		TLSCertFile:   s.config.Security.TLSCertFile,
		TLSKeyFile:    s.config.Security.TLSKeyFile,
		TLSCAFile:     s.config.Security.TLSCAFile,
		TLSClientAuth: s.config.Security.TLSClientAuth,
		// Clone TLS config to avoid concurrent writes from HTTP/2 setup
		// racing with other servers (QUIC, gRPC) that share s.tlsConfig.
		TLSConfig: cloneTLSConfig(s.tlsConfig),
	}

	// Create REST server using QuicBaaS facade
	restServer, err := newREST(s.quicBaaSService, restConfig, s.logger)
	if err != nil {
		return &dragonerrors.QBError{
			Code: dragonerrors.ErrInternal,
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
		return &dragonerrors.QBError{
			Code: dragonerrors.ErrConfiguration,
			Op:   "server.startRESTServer",
			Err:  &ServerCauseError{Message: "invalid routing config", Cause: err},
		}
	}
	r2 := routing.NewWithShards(restKVShards)
	for et, shard := range s.config.Server.EntityShardPins {
		r2.SetEntityShard(et, shard)
	}
	restServer.SetShardRouter(r2)

	// Start REST server
	address := fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.REST.Port)
	if err := restServer.Start(address); err != nil {
		return &dragonerrors.QBError{
			Code: dragonerrors.ErrInternal,
			Op:   "server.startRESTServer",
			Err:  &ServerCauseError{Message: "failed to start REST server", Cause: err},
		}
	}

	s.restServer = restServer
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
		return &dragonerrors.QBError{
			Code: dragonerrors.ErrInternal,
			Op:   "server.stopRESTServer",
			Err:  &ServerCauseError{Message: "failed to shutdown REST server", Cause: err},
		}
	}

	return nil
}
