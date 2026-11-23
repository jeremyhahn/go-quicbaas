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
	"context"
	"strings"

	ggrpc "google.golang.org/grpc"

	qrdbgrpc "github.com/jeremyhahn/go-qrdb/pkg/api/grpc"
	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
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
// implementations are available. The data services (KV, MVCC, batch,
// transaction, collections and index) resolve the embedded node's
// per-(tenant, database) registry on each call, and fail closed while the
// node is absent or sealed.
func (s *Server) registerQRDBService(grpcServer *ggrpc.Server) {
	opts := s.buildQRDBRegisterOptions()
	opts = append(opts, qrdbgrpc.WithRegisterServiceRegistry(newQRDBDataRegistryProxy(s)))
	qrdbsdk.RegisterGRPCService(grpcServer, nil, nil, opts...)
	s.logger.Info("Registered qrdb.v1.QRDBService on gRPC server (data/credential/barrier/ceremony/user/user-auth-factor surface)")
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

	opts = append(opts,
		// The barrier proxy resolves s.qrdbBarrierServicer() PER CALL (see
		// qrdb_barrier_servicer_proxy.go): the node's TCP gRPC server registers
		// before the qrdb client, bootstrap ceremony client or sealed-window
		// services are wired, so a servicer captured here would be nil. Until one
		// is wired the proxy answers Unimplemented "barrier service is not
		// configured", as an unregistered barrier did.
		qrdbsdk.WithRegisterBarrierService(newQRDBBarrierServicerProxy(s)),
		// User + UserAuthFactor services back the qrdb SDK's
		// Security().Auth().Factors() surface: resolveSubjectUserID reads the
		// user store and factor enrollment writes the UserAuthFactor store the
		// unified-login ceremony resolves against. The proxies resolve the
		// embedded node servicer PER CALL (see qrdb_user_servicers.go) so the
		// surface comes online after Unseal on the seal-enabled boot path
		// instead of being captured as nil at registration time.
		qrdbsdk.WithRegisterUserService(newQRDBUserServicerProxy(s)),
		qrdbsdk.WithRegisterUserAuthFactorService(newQRDBUserAuthFactorServicerProxy(s)),
		// The time-series registry backs the qrdb.v1 time-series RPCs that
		// the SDK's Search().QRDBTimeSeries() calls. go-qrdb's handler
		// resolves each RPC's servicer per (tenant, database) through it, as
		// go-qrdb's own transports do, so one tenant never reads or
		// overwrites another's metrics or retention. It resolves the node's
		// registry PER CALL for the same reason as the user proxies: the
		// node has no server until Unseal on the seal-enabled boot path.
		qrdbgrpc.WithRegisterServiceRegistry(newQRDBTimeSeriesRegistry(s)),
	)

	return opts
}

// qrdbTimeSeriesRegistry is the go-qrdb service.Registry the broker's qrdb.v1
// gRPC and Unix surface resolves its data-plane RPCs through. It serves the
// time-series family only: TimeSeriesService delegates to the embedded node's
// cached scoped registry, which wraps a shared-shard tenant in a namespaced,
// quota-checked servicer. Every other family answers
// qrdbgrpc.ServiceNotConfiguredError, which is what those RPCs answered when
// no servicer was registered for them.
type qrdbTimeSeriesRegistry struct {
	holder *qrdbScopedRegistryHolder
}

var _ qrdbservice.Registry = (*qrdbTimeSeriesRegistry)(nil)

func newQRDBTimeSeriesRegistry(s *Server) *qrdbTimeSeriesRegistry {
	return &qrdbTimeSeriesRegistry{holder: &qrdbScopedRegistryHolder{s: s}}
}

func (r *qrdbTimeSeriesRegistry) TimeSeriesService(tenantID, db string) (qrdbservice.TimeSeriesServicer, error) {
	return r.holder.timeSeriesService(tenantID, db)
}

func (r *qrdbTimeSeriesRegistry) KVService(string, string) (qrdbservice.KVServicer, error) {
	return nil, &qrdbgrpc.ServiceNotConfiguredError{Service: "kv"}
}

func (r *qrdbTimeSeriesRegistry) MVCCService(string, string) (qrdbservice.MVCCKVServicer, error) {
	return nil, &qrdbgrpc.ServiceNotConfiguredError{Service: "mvcc"}
}

func (r *qrdbTimeSeriesRegistry) WatchService(string, string) (qrdbservice.WatchServicer, error) {
	return nil, &qrdbgrpc.ServiceNotConfiguredError{Service: "watch"}
}

func (r *qrdbTimeSeriesRegistry) LeaseService(string, string) (qrdbservice.LeaseServicer, error) {
	return nil, &qrdbgrpc.ServiceNotConfiguredError{Service: "lease"}
}

func (r *qrdbTimeSeriesRegistry) IndexService(string, string) (qrdbservice.IndexServicer, error) {
	return nil, &qrdbgrpc.ServiceNotConfiguredError{Service: "index"}
}

func (r *qrdbTimeSeriesRegistry) VectorService(string, string) (qrdbservice.VectorServicer, error) {
	return nil, &qrdbgrpc.ServiceNotConfiguredError{Service: "vector"}
}

func (r *qrdbTimeSeriesRegistry) TransactionService(string, string) (qrdbservice.TransactionServicer, error) {
	return nil, &qrdbgrpc.ServiceNotConfiguredError{Service: "transaction"}
}

func (r *qrdbTimeSeriesRegistry) BatchService(string, string) (qrdbservice.BatchServicer, error) {
	return nil, &qrdbgrpc.ServiceNotConfiguredError{Service: "batch"}
}

func (r *qrdbTimeSeriesRegistry) TableService(string, string) (qrdbservice.TableServicer, error) {
	return nil, &qrdbgrpc.ServiceNotConfiguredError{Service: "table"}
}

func (r *qrdbTimeSeriesRegistry) SequenceService(string, string) (qrdbservice.SequenceServicer, error) {
	return nil, &qrdbgrpc.ServiceNotConfiguredError{Service: "sequence"}
}

func (r *qrdbTimeSeriesRegistry) PrivilegeService(string, string) (qrdbservice.PrivilegeServicer, error) {
	return nil, &qrdbgrpc.ServiceNotConfiguredError{Service: "privilege"}
}

func (r *qrdbTimeSeriesRegistry) CollectionService(string, string) (qrdbservice.CollectionServicer, error) {
	return nil, &qrdbgrpc.ServiceNotConfiguredError{Service: "collection"}
}

func (r *qrdbTimeSeriesRegistry) LockService(string, string) (qrdbservice.LockServicer, error) {
	return nil, &qrdbgrpc.ServiceNotConfiguredError{Service: "lock"}
}

// qrdbSealSOPINInterceptor guards the qrdb.v1 BarrierSeal, BarrierRekey and
// BarrierRecoveryGenerate registered by
// registerQRDBService with the SO PIN backend the BarrierService facade
// verifies against. The qrdb SDK's register options take no PIN backend,
// so the guard sits on the gRPC server chain, which the Unix socket shares.
func (s *Server) qrdbSealSOPINInterceptor() ggrpc.UnaryServerInterceptor {
	return grpcapi.UnaryBarrierSealSOPINInterceptor(s.pinBackend)
}

// qrdbServiceMethodPrefix is the full-method prefix of go-qrdb's
// qrdb.v1.QRDBService, whose time-series handlers resolve their servicer per
// (tenant, database).
const qrdbServiceMethodPrefix = "/qrdb.v1.QRDBService/"

// qrdbDatabaseScopeInterceptor applies go-qrdb's own database-scope
// interceptor to qrdb.v1.QRDBService calls only, so the x-qrdb-database
// metadata selects the database the handler resolves through the scoped
// service registry. Calls to any other service pass through unchanged.
func qrdbDatabaseScopeInterceptor(ctx context.Context, req any, info *ggrpc.UnaryServerInfo, handler ggrpc.UnaryHandler) (any, error) {
	if strings.HasPrefix(info.FullMethod, qrdbServiceMethodPrefix) {
		return qrdbgrpc.DatabaseScopeUnaryInterceptor(ctx, req, info, handler)
	}
	return handler(ctx, req)
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
