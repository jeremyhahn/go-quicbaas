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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// =============================================================================
// qrdb.v1.QRDBService composition registration
//
// QuicBaaS embeds go-qrdb (Phase 4 composition). The qrdb SDK clients used
// by Security().Barrier(), Security().Ceremony(), and Security().Credentials()
// dial the qrdb.v1.QRDBService gRPC interface. To keep that surface reachable
// when callers connect through quicbaasd's gRPC listener, this file registers
// qrdb's QRDBServiceServer on the same grpc.Server instance, alongside the
// quicbaas-namespaced services.
//
// Each qrdb sub-service (barrier, ceremony, credentials, etc.) is only wired
// in when the corresponding service implementation is available on the
// running quicbaasd process. Missing services dispatch to the qrdb default
// codes.Unimplemented handler rather than crashing the server.
// =============================================================================

// registerQRDBService registers qrdb's QRDBServiceServer onto the given gRPC
// server. The qrdb proto surface lives in the qrdb.v1 namespace, so it can
// coexist with the quicbaas.v1 services already registered on the same
// grpc.Server. Sub-services are conditionally wired based on which
// implementations are available.
func (s *Server) registerQRDBService(grpcServer *ggrpc.Server) {
	opts := s.buildQRDBRegisterOptions()
	qrdbsdk.RegisterGRPCService(grpcServer, nil, nil, opts...)
	s.logger.Info("Registered qrdb.v1.QRDBService on gRPC server (credential/barrier/ceremony/user/user-auth-factor surface)")
}

// buildQRDBRegisterOptions assembles the qrdbsdk.GRPCRegisterOption slice from
// the services that have been wired into the running quicbaasd process. The
// credential and ceremony services are sourced from the bootstrap ceremony
// services bundle (see buildBootstrapCeremonyServices) which is the same
// bundle the SDK embedded transport uses for Security().Credentials() and
// Security().Ceremony() dispatch. The barrier service wraps the platform-shard
// data-plane barrier client — the same barrier the MCP, QUIC, and REST
// barrier servers operate.
//
// Each register option is appended only when its backing service is wired,
// so absent services dispatch to the qrdb codes.Unimplemented handler rather
// than panicking on a nil receiver.
func (s *Server) buildQRDBRegisterOptions() []qrdbsdk.GRPCRegisterOption {
	var opts []qrdbsdk.GRPCRegisterOption

	if cs := s.qrdbCredentialServicer(); cs != nil {
		opts = append(opts, qrdbsdk.WithRegisterCredentialService(cs))
	}

	if cs := s.qrdbCeremonyServicer(); cs != nil {
		opts = append(opts, qrdbsdk.WithRegisterCeremonyService(cs))
	}

	if bs := s.qrdbBarrierServicer(); bs != nil {
		opts = append(opts, qrdbsdk.WithRegisterBarrierService(bs))
	}

	// User + UserAuthFactor services back the qrdb SDK's
	// Security().Auth().Factors() surface: resolveSubjectUserID reads the
	// user store and factor enrollment writes the UserAuthFactor store the
	// unified-login ceremony resolves against. The proxies resolve the
	// embedded node servicer PER CALL (see qrdb_user_servicers.go) so the
	// surface comes online after Unseal on the seal-enabled boot path
	// instead of being captured as nil at registration time.
	opts = append(opts,
		qrdbsdk.WithRegisterUserService(newQRDBUserServicerProxy(s)),
		qrdbsdk.WithRegisterUserAuthFactorService(newQRDBUserAuthFactorServicerProxy(s)),
	)

	return opts
}

// qrdbSealSOPINInterceptor guards the qrdb.v1 BarrierSeal registered by
// registerQRDBService with the SO PIN backend the BarrierService facade
// verifies against. The qrdb SDK's register options take no PIN backend,
// so the guard sits on the gRPC server chain, which the Unix socket shares.
func (s *Server) qrdbSealSOPINInterceptor() ggrpc.UnaryServerInterceptor {
	return grpcapi.UnaryBarrierSealSOPINInterceptor(s.pinBackend)
}

// qrdbCredentialServicer returns the credential management service used by
// the qrdb gRPC handlers. Returns nil when the bootstrap ceremony wiring was
// skipped (e.g. on cluster modes that do not run the init flow), in which
// case the qrdb credential RPCs respond with codes.Unimplemented.
func (s *Server) qrdbCredentialServicer() qrdbsdk.ServiceCredentialServicer {
	if s.ceremonyServices == nil {
		return nil
	}
	return s.ceremonyServices.Credential
}

// qrdbCeremonyServicer returns the ceremony management service used by the
// qrdb gRPC handlers. It is sourced from the same bootstrap ceremony bundle
// as the credential service. Returns nil when the bootstrap ceremony wiring
// was skipped, in which case the qrdb ceremony RPCs respond with
// codes.Unimplemented.
func (s *Server) qrdbCeremonyServicer() qrdbsdk.ServiceCeremonyServicer {
	if s.ceremonyServices == nil {
		return nil
	}
	return s.ceremonyServices.Ceremony
}

// qrdbBarrierServicer returns the barrier management service used by the qrdb
// gRPC (and Unix-over-gRPC) handlers. It is backed by the qrdb SDK barrier
// client bound to the platform shard — the SAME data-plane barrier the MCP,
// QUIC, and REST barrier servers operate through buildBarrierCeremonyServices.
// Routing every transport through the one platform-shard barrier keeps barrier
// state consistent: `barrier` CLI verbs over gRPC/Unix hit the exact same
// barrier instance as MCP/QUIC/REST, with no split-brain state.
//
// The previous implementation sourced the barrier from the quicbaas seal
// manager's SystemBarrier(), which is a no-op compatibility shim returning
// nil. That left WithRegisterBarrierService unapplied, so the qrdb gRPC
// barrier handler reported "barrier service is not configured" (Unimplemented)
// for the gRPC and Unix transports.
//
// Returns nil only when the platform-shard barrier client is genuinely
// unavailable (qrdbClient not yet wired, or shard resolution fails), in which
// case the qrdb barrier RPCs respond with codes.Unimplemented — preserving the
// clean fallback for misconfigured deployments.
func (s *Server) qrdbBarrierServicer() qrdbsdk.BarrierServicer {
	// Sealed-window boot phase: the node is sealed and its own SDK client
	// does not exist yet. s.ceremonyServices carries the node's OWN
	// seal-layer servicer instances (BaasBarrierService = qrdbsdk.
	// BarrierServicer), so the qrdb.v1 barrier surface serves the same
	// barrier every other transport does during the window — directly,
	// with no client round-trip.
	if s.sealedWindowClient != nil && s.ceremonyServices != nil && s.ceremonyServices.Barrier != nil {
		return s.ceremonyServices.Barrier
	}
	// Seal-disabled state: the node's SDK client carries no barrier
	// servicer, so the qrdb.v1 barrier surface operates on the bootstrap
	// ceremony client — the same instances every other transport serves.
	if s.bootstrapCeremonyClient != nil {
		return newQRDBBarrierClientServicer(s.bootstrapCeremonyClient.Barrier())
	}
	if s.qrdbClient == nil {
		return nil
	}
	platformClient, err := s.qrdbClient.ForShard(qbshards.PlatformShardID)
	if err != nil || platformClient == nil {
		if err != nil {
			s.logger.Warnf("qrdb gRPC barrier servicer not wired (platform shard unavailable): %v", err)
		}
		return nil
	}
	// Barrier() type-asserts the transport, so it either panics or returns a
	// non-nil interface; there is no nil case left to guard here.
	return newQRDBBarrierClientServicer(platformClient.Barrier())
}
