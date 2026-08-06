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

// registerBarrierCeremonyServices registers the Barrier, Ceremony, and
// TenantBarrier gRPC service implementations against the supplied grpc.Server.
// The facades are wired against the qrdb SDK client bound to the platform shard.
func (s *Server) registerBarrierCeremonyServices(grpcServer *ggrpc.Server) {
	barrierSvc, ceremonySvc := s.buildBarrierCeremonyServices()
	grpcapi.RegisterBarrierServer(grpcServer, barrierSvc, s.logger)
	grpcapi.RegisterCeremonyServer(grpcServer, ceremonySvc, s.logger)
	s.logger.Info("Registered Barrier and Ceremony gRPC services")

	// Register the per-tenant barrier gRPC service. The handler delegates to
	// the TenantBarrierServicer facade which forwards to the embedded qrdb
	// TenantClient — quicbaas owns no barrier logic. When the qrdb client is
	// not yet available (typical pre-wire state) the facade is nil; every RPC
	// then short-circuits with codes.Unavailable instead of Unimplemented.
	tenantBarrierSvc := s.buildTenantBarrierService()
	tbServer := grpcapi.NewTenantBarrierGRPCServer(tenantBarrierSvc, s.logger)
	// Wire the BarrierServicer so EnableEncryption can delegate to the node
	// migrator for tenant-scoped at-rest encryption migration.
	tbServer.SetBarrierService(barrierSvc)
	grpcapi.RegisterTenantBarrierServerDirect(grpcServer, tbServer)
	s.logger.Info("Registered TenantBarrier gRPC service")
}

// buildBarrierCeremonyServices constructs the BarrierServicer and
// CeremonyServicer facades over the qrdb SDK client that carries the active
// barrier/ceremony servicers:
//
//   - Seal enabled: the platform-shard client (the node's SDK client) — the
//     node registered its data-plane barrier on its own embedded transport.
//   - Seal disabled: the bootstrap ceremony client built in startService —
//     the node's transport carries NO barrier/ceremony servicers in this
//     state, so the bootstrap-window stack (in-memory barrier + ceremony)
//     is the single canonical source for these verbs.
//
// When neither client is available (typical pre-wire state in unit tests)
// the facades are constructed with a nil transport which is handled
// gracefully by the service layer.
//
// When the embedded qrdb node is available it is injected as the
// EncryptionMigrator so EnableEncryption delegates to the real in-process
// implementation. The BarrierClient transport stubs EnableEncryption with
// NotImplementedError, so the node path is the only functional one.
func (s *Server) buildBarrierCeremonyServices() (service.BarrierServicer, service.CeremonyServicer) {
	var (
		barrierSvc  = service.NewBarrierService(nil)
		ceremonySvc = service.NewCeremonyService(nil)
	)

	switch {
	case s.sealedWindowClient != nil:
		// Sealed-window boot phase: the node is sealed and its own SDK
		// client does not exist yet. The sealed-window client wraps the
		// node's OWN seal-layer servicer instances (the ones the node
		// reuses when Unseal completes the boot), so every transport
		// serves ONE barrier with no split-brain state during the window.
		barrierSvc = service.NewBarrierService(s.sealedWindowClient.Barrier())
		ceremonySvc = service.NewCeremonyService(s.sealedWindowClient.Ceremony())
	case s.bootstrapCeremonyClient != nil:
		barrierSvc = service.NewBarrierService(s.bootstrapCeremonyClient.Barrier())
		ceremonySvc = service.NewCeremonyService(s.bootstrapCeremonyClient.Ceremony())
	case s.qrdbClient != nil:
		platformClient, err := s.qrdbClient.ForShard(qbshards.PlatformShardID)
		if err != nil {
			s.logger.Warnf("Barrier/Ceremony services bound to nil transport: %v", err)
		} else if platformClient != nil {
			barrierSvc = service.NewBarrierService(platformClient.Barrier())
			ceremonySvc = service.NewCeremonyService(platformClient.Ceremony())
		}
	}

	// Wire the embedded node as the EncryptionMigrator. The BarrierClient
	// transport stubs BarrierEnableEncryption with NotImplementedError; only
	// the in-process node carries the real migration implementation.
	if s.qrdbNode != nil {
		barrierSvc.SetMigrator(s.qrdbNode)
	}

	return barrierSvc, ceremonySvc
}
