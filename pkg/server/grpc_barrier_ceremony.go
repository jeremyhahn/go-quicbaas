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

package server

import (
	ggrpc "google.golang.org/grpc"

	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// =============================================================================
// Barrier + Ceremony gRPC service registration
//
// QuicBaaS itself does not own the storage barrier or the initialization
// ceremony — both are owned by the embedded go-qrdb instance. To keep the
// gRPC surface symmetric with REST, we expose Barrier and Ceremony gRPC
// services whose handlers delegate to the QuicBaaS BarrierServicer and
// CeremonyServicer facades. Those facades, in turn, delegate to the qrdb
// SDK BarrierClient / CeremonyClient bound to the platform shard.
//
// The services are always registered. When the underlying qrdb client is
// not yet available (e.g. embedded mode where the ShardedClient is nil)
// the facades are constructed with a nil transport client; every RPC then
// short-circuits with codes.Unavailable so callers receive a clean
// "service not yet wired" status instead of an Unimplemented error.
// =============================================================================

// registerBarrierCeremonyServices registers the Barrier and Ceremony
// gRPC service implementations against the supplied grpc.Server. The
// returned facades are wired against the qrdb SDK client bound to the
// platform shard.
func (s *Server) registerBarrierCeremonyServices(grpcServer *ggrpc.Server) {
	barrierSvc, ceremonySvc := s.buildBarrierCeremonyServices()
	grpcapi.RegisterBarrierServer(grpcServer, barrierSvc, s.logger)
	grpcapi.RegisterCeremonyServer(grpcServer, ceremonySvc, s.logger)
	s.logger.Info("Registered Barrier and Ceremony gRPC services")
}

// buildBarrierCeremonyServices constructs the BarrierServicer and
// CeremonyServicer facades from the qrdb SDK client bound to the platform
// shard. When the qrdb client is unavailable (typical pre-wire state) the
// facades are constructed with a nil transport which is handled gracefully
// by the service layer.
func (s *Server) buildBarrierCeremonyServices() (service.BarrierServicer, service.CeremonyServicer) {
	if s.qrdbClient == nil {
		return service.NewBarrierService(nil), service.NewCeremonyService(nil)
	}
	platformClient, err := s.qrdbClient.ForShard(qbshards.PlatformShardID)
	if err != nil || platformClient == nil {
		if err != nil {
			s.logger.Warnf("Barrier/Ceremony gRPC services bound to nil transport: %v", err)
		}
		return service.NewBarrierService(nil), service.NewCeremonyService(nil)
	}
	return service.NewBarrierService(platformClient.Barrier()),
		service.NewCeremonyService(platformClient.Ceremony())
}
