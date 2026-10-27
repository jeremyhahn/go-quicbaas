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

// Package apimodule registers go-quicbaas's broker capability plane into
// go-qrdb's unified multi-protocol server as a qrdbsdk.ServiceModule (the
// Phase 6 seam of plans/unified-api-server-2026-08-14.md). The stack-wide
// convention is one module per provider project, named after the project:
// go-quicbaas exports this Module ("quicbaas"); go-xkms and
// go-trusted-platform export their own, and any daemon hosting an embedded
// qrdb node composes the modules it needs via qrdbsdk.WithModule.
//
// Module carries two families of generated gRPC services, all composed
// around the service.QuicBaaS facade:
//
//   - The broker capability plane — Scheduler, Orchestrator, Project,
//     Account, SSH, Messaging, and Embedding — the same server
//     implementations the legacy gRPC transport registers in
//     pkg/server/lifecycle.go.
//   - The quicbaas-native core plane — QuicBaaS (KV/cluster/backup/
//     RBAC/seal/time-series and the legacy unauthenticated Login RPC),
//     Login (the unified BeginLogin/FinishLogin ceremony), and FIDO2Service
//     (the tenant-scoped WebAuthn ceremony that backs quicbaas's own login
//     plane). These three are genuinely quicbaas-owned authentication and
//     data-plane surfaces, not proxies onto another project's module.
//
// One family of legacy gRPC services is deliberately NOT registered here even
// though pkg/server/lifecycle.go serves it on the legacy transport:
//
//   - qrdb-proxy planes (Barrier, TenantBarrier, Ceremony, VectorDB,
//     VectorAdmin): their handlers forward every verb through the embedded
//     go-qrdb node's own SDK client (qrdbtransport.*Client) or mirror
//     go-qrdb's own pkg/service surface verbatim. qrdb serves the identical
//     capability natively on the unified server; registering quicbaas's
//     wrapper here would duplicate that surface under a redundant proto
//     name.
//
// SecretsManagement IS registered here even though its handler delegates to
// the composed go-xkms library (pkg/service/kms.go's Secrets() facade
// accessor). It is NOT a qrdb-proxy or xkms-proxy plane: it is quicbaas's OWN
// public wire API (a quicbaas.v1 proto the quicbaas SDK's gRPC transport
// dials directly, pkg/api/grpc/client_secrets*.go) — genuinely native, not a
// wrapper over an xkms capability of the same shape.
//
// KeyManagement, DEKManagement, Crypto, CertificateService, TrustService,
// AttestationService, and PIVService are registered here too. go-xkms's own
// ServiceModule ("xkms") serves its canonical xkms.v1 KeystoreService on the
// same unified server, but the quicbaas SDK's gRPC transport
// (sdk/go/transport/grpc/{kms,crypto,certificate,trust,attestation,piv}
// _handler.go) speaks these quicbaas.v1 services, and the node's port is the
// only TCP gRPC listener there is: without them `quicbaas --transport grpc
// kms key generate` answers Unimplemented (#1960). The two surfaces carry
// different proto service names, so they do not collide.
//
// FIDO2Service is the other xkms-backed surface registered here: quicbaas's
// own tenant-scoped LOGIN ceremony (the WebAuthn analogue of the Login
// service, both part of quicbaas's own authentication plane) — genuinely
// native, not a wrapper.
//
// A further set of quicbaas-native broker data-plane surfaces — ParameterStore,
// DynamicSecretsService, ObjectStorageService, ObjectStorageAdmin, and
// OllamaService — are likewise quicbaas-owned wire API, not qrdb-proxy planes
// (their handlers operate on quicbaas's own storage/broker layer, exactly
// like the seven broker services above) and are registered here too, each
// nil-gated on its own facade accessor exactly like the broker planes:
//
//   - ParameterStore and DynamicSecretsService and ObjectStorage(Admin) wrap
//     service.ParameterStoreService / DynamicSecrets() / ObjectStorageService()
//     / ObjectStorageAdminService() with the same grpcapi constructors
//     pkg/server/lifecycle.go's legacy gRPC transport uses.
//   - OllamaService is additionally build-tag-gated: its concrete
//     grpcapi.NewOllamaServer constructor only exists under -tags rag (see
//     module_ollama_rag.go / module_ollama_norag.go), mirroring
//     lifecycle_rag.go's registerOllamaService / lifecycle_rag_noop.go split.
//     Without the rag tag the capability answers Unavailable regardless of
//     configuration, matching the legacy listener's own no-op registration.
//
// AgentCatalogService, SDLCService, RegistryService, FederationService and
// SessionService are quicbaas-native too and are registered here (#1696), so
// the node's TCP gRPC port serves them as the Unix listener does. The first
// three are built from the facade. Federation and Session need IAM state the
// facade does not expose, so the host builds them and passes the SAME
// instances it registers on the Unix listener (WithHostGRPCServers). The
// other iam.v1 services (IAMService, PEPService, MFAService, AuditService)
// stay off: most of their RPCs carry no (qrdb.v1.permission) option, and an
// unannotated RPC is default-denied here.
//
// ObjectTransferService is deliberately NOT registered here. Unlike the five
// above, it has no facade-backed gRPC implementation to reuse: the only
// existing pb.ObjectTransferServiceServer (pkg/api/grpc/server_object_transfer.go)
// is a standalone per-shard ObjectStoreReader registry (RegisterStore/
// UnregisterStore) that no production code path ever populates — on the
// legacy gRPC transport, the REST transport (pkg/api/rest/handlers_object_transfer.go),
// and the Unix transport alike — and it is architecturally disjoint from the
// service.ObjectTransferService facade interface this module would otherwise
// bind through (quicBaaS.ObjectTransferService(), whose backing field is
// likewise never assigned in production). Registering it would mean
// inventing a new production implementation and a new facade-to-pb
// translation layer, not wiring an existing one — a genuine capability gap
// for a future step, not a mechanical registration like the other five.
//
// When the embedded qrdb node's server transports are enabled (the later
// cutover wave), these capability planes are served by qrdb's one server.
// Registration is ADDITIVE: quicbaas's legacy transport stacks keep serving
// exactly as today.
//
// Authorization: on the unified server, per-method authorization is enforced
// by qrdb's reflection interceptor reading the option (qrdb.v1.permission)
// annotation on each RPC (api/proto/*.proto, generated from the
// grpcMethodPermissions table by scripts/protoannotate) — an unannotated RPC
// is DEFAULT-DENY there, matching the legacy interceptor's posture for
// unmapped methods. The handlers therefore carry no second in-handler RBAC
// pass: the interceptor is the gate. The small set of methods the legacy
// gRPC interceptor exempts from authentication entirely (Health, the login
// ceremony, and the FIDO2 authentication ceremony) are declared through
// PublicGRPCMethods, below.
package apimodule

import (
	"context"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ModuleName is the unique ServiceModule name go-quicbaas's module registers
// under; qrdb's WithModule fails fast on a duplicate.
const ModuleName = "quicbaas"

// NilServiceResolverError is returned by NewModule when constructed without a
// service resolver: the module would have no way to ever reach the QuicBaaS
// service facade, so construction fails instead of registering a dead surface.
type NilServiceResolverError struct{}

// Error implements the error interface.
func (e *NilServiceResolverError) Error() string {
	return "apimodule: NewModule requires a non-nil QuicBaaS service resolver"
}

// NilLoggerError is returned by NewModule when constructed without a logger:
// every broker gRPC server constructor derives a component logger from it at
// bind time (log.WithField), so a nil logger would defer the composition
// error to the first RPC as a panic instead of failing construction.
type NilLoggerError struct{}

// Error implements the error interface.
func (e *NilLoggerError) Error() string {
	return "apimodule: NewModule requires a non-nil logger"
}

// ServiceResolver returns the composed service.QuicBaaS facade, or nil while
// it is not yet available.
//
// It exists because of a construction-order inversion in quicbaasd: the
// embedded qrdb node is built by initStandaloneQRDBNode /
// initClusteredQRDBNode (pkg/server/qrdb_node.go) early in Server.Start — and
// when the node's server transports are enabled, node construction runs the
// full qrdb server start pipeline, invoking every module's RegisterGRPC
// immediately. The QuicBaaS facade, by contrast, is composed much later in
// the start sequence (after KMS, secrets, vector, and the service container
// build). A module holding the facade directly would therefore always
// register nil services. The resolver defers the binding to the first RPC, by
// which point the facade is composed.
type ServiceResolver func() service.QuicBaaS

// RESTResolver returns the *rest.Server whose handler surface backs the
// module's HTTP routes, or nil while the host is still composing it. It is a
// resolver for the same construction-order reason as ServiceResolver:
// RegisterHTTP fires during embedded-node construction, long before the REST
// wiring exists.
type RESTResolver func() *restapi.Server

// MCPResolver returns the *mcp.Server whose tool registry backs the module's
// real MCP tools, or nil while the host is still composing it. It is a
// resolver for the same construction-order reason as ServiceResolver.
type MCPResolver func() *mcpapi.Server

// Option configures optional Module behavior at construction time.
type Option func(*Module)

// WithNodeInfo supplies the node identity the shared gRPC facade constructor
// requires (grpcapi.NewServer's nodeID and version parameters), mirroring the
// arguments the legacy transport passes in pkg/server/lifecycle.go
// (cfg.Raft.NodeID and cfg.Server.Host). Zero values are valid for a
// standalone node.
func WithNodeInfo(nodeID uint64, version string) Option {
	return func(m *Module) {
		m.nodeID = nodeID
		m.version = version
	}
}

// WithRESTServer wires the REST surface resolver consumed by RegisterHTTP
// (module_http.go, rest builds). Without it the module registers no HTTP
// routes.
func WithRESTServer(resolve RESTResolver) Option {
	return func(m *Module) {
		m.resolveREST = resolve
	}
}

// WithMCPServer wires the MCP surface resolver consumed by RegisterMCP
// (module_mcp.go, mcp builds). Without it the module registers no MCP tools.
func WithMCPServer(resolve MCPResolver) Option {
	return func(m *Module) {
		m.resolveMCP = resolve
	}
}

// ShardRouter selects the shard ID that owns a given (entityType, id) pair.
// It matches the unnamed interface the shared *grpcapi.Server facade's
// SetShardRouter accepts (pkg/api/grpc/server.go), so this package can
// accept a caller's concrete router — typically a *routing.Router built
// from server config — without importing pkg/routing (raft-tag-gated)
// itself.
type ShardRouter interface {
	Route(entityType string, id uint64) uint64
}

// WithShardRouting supplies the multi-shard router and the vector-operations
// shard ID the shared *grpcapi.Server facade (backing the QuicBaaS,
// FIDO2Service, and, when enabled, Embedding surfaces) applies at bind time —
// mirroring the SetShardRouter/SetVectorShardID calls
// pkg/server/lifecycle.go's legacy gRPC transport makes on its own facade
// instance, built from the identical s.config.Server.Shards /
// EntityShardPins / Vector.ShardID configuration (pkg/server/qrdb_node.go's
// buildModuleShardRouter). Without this option the facade keeps its own
// NewServer defaults: a single-shard routing.New() router and
// shards.PlatformShardID, unchanged from prior behavior.
func WithShardRouting(router ShardRouter, vectorShardID uint64) Option {
	return func(m *Module) {
		m.shardRouter = router
		m.vectorShardID = vectorShardID
		m.shardRoutingSet = true
	}
}

// WithSPIFFETrustDomain pins the SPIFFE trust domain the module's gRPC
// surfaces read a verified client certificate's tenant under. It is the
// trust domain the daemon's CA issues under (iam.spiffe_trust_domain).
func WithSPIFFETrustDomain(trustDomain string) Option {
	return func(m *Module) {
		m.trustDomain = trustDomain
	}
}

// WithHostGRPCServers mounts the host-built FederationService and
// SessionService servers (see HostGRPCServers) on the node's gRPC server.
// Without it those services answer Unavailable.
func WithHostGRPCServers(resolve HostGRPCResolver) Option {
	return func(m *Module) {
		m.resolveHost = resolve
	}
}

// Module is go-quicbaas's broker capability plane and quicbaas-native core
// plane as a qrdb ServiceModule. It registers the Scheduler, Orchestrator,
// Project, Account, SSH, Messaging, and Embedding broker surfaces, plus the
// quicbaas-native QuicBaaS (core KV/cluster/RBAC/seal/time-series), Login,
// and FIDO2Service surfaces, into the unified server's gRPC-wire transports
// (gRPC and unix). RegisterHTTP (module_http.go, rest builds) registers the
// full table-driven REST surface, and RegisterMCP (module_mcp.go, mcp
// builds) registers every registry tool as a real MCP tool; on tag-less
// builds those hooks stay at the BaseModule no-ops.
//
// Start and Stop are inherited BaseModule no-ops by design: the QuicBaaS
// facade lifecycle (storage, KMS, messaging brokers, orchestrator runtimes)
// is owned by pkg/server, which composes the facade and tears it down — the
// module only exposes it and must not double-manage those resources.
type Module struct {
	qrdbsdk.BaseModule

	resolve     ServiceResolver
	resolveREST RESTResolver
	resolveMCP  MCPResolver
	log         *logger.Logger
	nodeID      uint64
	version     string

	// shardRouter, vectorShardID, and shardRoutingSet carry the WithShardRouting
	// option (if supplied) applied to the shared *grpcapi.Server facade at bind
	// time. shardRoutingSet distinguishes "apply the facade's own defaults"
	// (the option was never supplied) from "apply vectorShardID 0" (an explicit,
	// if unusual, caller choice).
	shardRouter     ShardRouter
	vectorShardID   uint64
	shardRoutingSet bool

	// trustDomain carries the WithSPIFFETrustDomain option.
	trustDomain string

	// resolveHost carries the WithHostGRPCServers option; nil leaves every
	// HostGRPCServers service answering Unavailable.
	resolveHost HostGRPCResolver

	// servers memoizes the lazily-constructed gRPC server bundle. Lock-free:
	// concurrent first calls may both construct, and CompareAndSwap keeps
	// exactly one (construction is cheap and side-effect free).
	servers atomic.Pointer[grpcServers]
}

// Compile-time interface checks.
var (
	_ qrdbsdk.ServiceModule                = (*Module)(nil)
	_ qrdbsdk.ModuleGRPCPublicMethodLister = (*Module)(nil)
)

// grpcServers is the bound broker server bundle. A nil field records a
// capability the composed facade does not enable (the legacy transport skips
// its registration for the same nil); its RPCs answer Unavailable.
//
// quicbaas, fido2, and login are, by contrast, never nil once bind succeeds:
// all three are unconditional surfaces (matching lifecycle.go's own
// unconditional registration of the QuicBaaS, FIDO2Service, and Login
// services), so their "not enabled" case is signaled by the concrete
// handler's own typed errors rather than the module's nil-gate.
type grpcServers struct {
	quicbaas           pb.QuicBaaSServer
	login              pb.LoginServer
	fido2              pb.FIDO2ServiceServer
	scheduler          pb.SchedulerServiceServer
	orchestrator       pb.OrchestratorServiceServer
	project            pb.ProjectServiceServer
	account            pb.AccountServiceServer
	ssh                pb.SSHServiceServer
	messaging          pb.MessagingServiceServer
	embedding          pb.EmbeddingServiceServer
	parameterStore     pb.ParameterStoreServer
	dynamicSecrets     pb.DynamicSecretsServiceServer
	objectStorage      pb.ObjectStorageServiceServer
	objectStorageAdmin pb.ObjectStorageAdminServer
	ollama             pb.OllamaServiceServer
	secretsManagement  pb.SecretsManagementServer
	keyManagement      pb.KeyManagementServer
	dekManagement      pb.DEKManagementServer
	crypto             pb.CryptoServer
	certificate        pb.CertificateServiceServer
	trust              pb.TrustServiceServer
	attestation        pb.AttestationServiceServer
	piv                pb.PIVServiceServer
	prompt             pb.PromptServiceServer
	agentCatalog       pb.AgentCatalogServiceServer
	sdlc               pb.SDLCServiceServer
	registry           pb.RegistryServiceServer
}

// HostGRPCServers carries the gRPC servers the host builds from IAM state
// the service facade does not expose: the federation manager and the session
// storage. The host builds them once and registers the SAME instances on its
// Unix-socket gRPC server, so both listeners serve one session store. A nil
// field answers Unavailable.
type HostGRPCServers struct {
	Federation iampb.FederationServiceServer
	Session    iampb.SessionServiceServer
}

// HostGRPCResolver returns the host's gRPC servers. The module calls it on
// every host-service RPC and caches nothing, so the host owns when the
// servers are built and memoizes them itself. An error (one carrying a gRPC
// status, such as Unavailable while the host is still initialising) is
// returned to the caller unchanged.
type HostGRPCResolver func() (HostGRPCServers, error)

// NewModule builds go-quicbaas's ServiceModule around the QuicBaaS facade
// resolver. A nil resolver or logger is a composition error and fails
// construction.
func NewModule(resolve ServiceResolver, log *logger.Logger, opts ...Option) (*Module, error) {
	if resolve == nil {
		return nil, &NilServiceResolverError{}
	}
	if log == nil {
		return nil, &NilLoggerError{}
	}
	m := &Module{resolve: resolve, log: log}
	for _, opt := range opts {
		opt(m)
	}
	// Contribute the module's permission set into qrdb's registries before
	// the embedded node seeds roles (module_grants.go).
	registerRoleGrants()
	return m, nil
}

// Name uniquely identifies the module.
func (m *Module) Name() string { return ModuleName }

// grpcRegistration binds one generated service descriptor to the bundle field
// serving it, or, for the host-built services, to the HostGRPCServers field
// serving it (hostPick). Exactly one of pick and hostPick is set.
type grpcRegistration struct {
	desc       *grpc.ServiceDesc
	capability string
	pick       func(*grpcServers) any
	hostPick   func(HostGRPCServers) any
}

// grpcRegistrations is the full gRPC surface the module registers: the
// quicbaas-native core plane (QuicBaaS, Login, FIDO2Service) followed by the
// seven broker capability services pkg/server/lifecycle.go registers on the
// legacy transport, in the same construction shapes (see bind).
var grpcRegistrations = []grpcRegistration{
	{desc: &pb.QuicBaaS_ServiceDesc, capability: "quicbaas", pick: func(s *grpcServers) any { return s.quicbaas }},
	{desc: &pb.Login_ServiceDesc, capability: "login", pick: func(s *grpcServers) any { return s.login }},
	{desc: &pb.FIDO2Service_ServiceDesc, capability: "fido2", pick: func(s *grpcServers) any { return s.fido2 }},
	{desc: &pb.SchedulerService_ServiceDesc, capability: "scheduler", pick: func(s *grpcServers) any { return s.scheduler }},
	{desc: &pb.OrchestratorService_ServiceDesc, capability: "orchestrator", pick: func(s *grpcServers) any { return s.orchestrator }},
	{desc: &pb.ProjectService_ServiceDesc, capability: "project", pick: func(s *grpcServers) any { return s.project }},
	{desc: &pb.AccountService_ServiceDesc, capability: "account", pick: func(s *grpcServers) any { return s.account }},
	{desc: &pb.SSHService_ServiceDesc, capability: "ssh", pick: func(s *grpcServers) any { return s.ssh }},
	{desc: &pb.MessagingService_ServiceDesc, capability: "messaging", pick: func(s *grpcServers) any { return s.messaging }},
	{desc: &pb.EmbeddingService_ServiceDesc, capability: "embedding", pick: func(s *grpcServers) any { return s.embedding }},
	{desc: &pb.ParameterStore_ServiceDesc, capability: "parameter-store", pick: func(s *grpcServers) any { return s.parameterStore }},
	{desc: &pb.DynamicSecretsService_ServiceDesc, capability: "dynamic-secrets", pick: func(s *grpcServers) any { return s.dynamicSecrets }},
	{desc: &pb.ObjectStorageService_ServiceDesc, capability: "object-storage", pick: func(s *grpcServers) any { return s.objectStorage }},
	{desc: &pb.ObjectStorageAdmin_ServiceDesc, capability: "object-storage-admin", pick: func(s *grpcServers) any { return s.objectStorageAdmin }},
	{desc: &pb.OllamaService_ServiceDesc, capability: "ollama", pick: func(s *grpcServers) any { return s.ollama }},
	{desc: &pb.SecretsManagement_ServiceDesc, capability: "secrets-management", pick: func(s *grpcServers) any { return s.secretsManagement }},
	{desc: &pb.KeyManagement_ServiceDesc, capability: "kms", pick: func(s *grpcServers) any { return s.keyManagement }},
	{desc: &pb.DEKManagement_ServiceDesc, capability: "dek", pick: func(s *grpcServers) any { return s.dekManagement }},
	{desc: &pb.Crypto_ServiceDesc, capability: "crypto", pick: func(s *grpcServers) any { return s.crypto }},
	{desc: &pb.CertificateService_ServiceDesc, capability: "certificate", pick: func(s *grpcServers) any { return s.certificate }},
	{desc: &pb.TrustService_ServiceDesc, capability: "trust", pick: func(s *grpcServers) any { return s.trust }},
	{desc: &pb.AttestationService_ServiceDesc, capability: "attestation", pick: func(s *grpcServers) any { return s.attestation }},
	{desc: &pb.PIVService_ServiceDesc, capability: "piv", pick: func(s *grpcServers) any { return s.piv }},
	{desc: &pb.PromptService_ServiceDesc, capability: "prompt", pick: func(s *grpcServers) any { return s.prompt }},
	{desc: &pb.AgentCatalogService_ServiceDesc, capability: "agent-catalog", pick: func(s *grpcServers) any { return s.agentCatalog }},
	{desc: &pb.SDLCService_ServiceDesc, capability: "sdlc", pick: func(s *grpcServers) any { return s.sdlc }},
	{desc: &pb.RegistryService_ServiceDesc, capability: "registry", pick: func(s *grpcServers) any { return s.registry }},
	{desc: &iampb.FederationService_ServiceDesc, capability: "federation", hostPick: func(h HostGRPCServers) any { return h.Federation }},
	{desc: &iampb.SessionService_ServiceDesc, capability: "session", hostPick: func(h HostGRPCServers) any { return h.Session }},
}

// publicGRPCMethods is the exact set of this module's gRPC methods the
// legacy gRPC interceptor (pkg/api/grpc/authz_mapping.go publicGRPCMethods)
// admits without authentication: Health (a liveness probe carrying no data),
// the legacy unauthenticated Login RPC, the unified BeginLogin/FinishLogin
// ceremony (the caller holds no bearer until FinishLogin mints one), and the
// FIDO2 authentication ceremony (WebAuthn IS the authentication mechanism —
// the caller holds no token until FinishAuthentication mints one). The FIDO2
// REGISTRATION pair (BeginRegistration/FinishRegistration) is deliberately
// absent: it binds a new authenticator to a caller-supplied identity and
// stays gated behind user_authfactors:add, exactly like the legacy listener.
var publicGRPCMethods = []string{
	"/quicbaas.v1.QuicBaaS/Health",
	"/quicbaas.v1.QuicBaaS/Login",
	"/quicbaas.login.v1.Login/BeginLogin",
	"/quicbaas.login.v1.Login/FinishLogin",
	"/quicbaas.fido2.v1.FIDO2Service/BeginAuthentication",
	"/quicbaas.fido2.v1.FIDO2Service/FinishAuthentication",
	"/quicbaas.iam.v1.FederationService/BeginFederatedLogin",
	"/quicbaas.iam.v1.FederationService/HandleFederatedCallback",
}

// PublicGRPCMethods implements qrdbsdk.ModuleGRPCPublicMethodLister. It
// returns the full gRPC method names that must serve unauthenticated on the
// unified server's gRPC and unix transports, verified against the legacy
// gRPC interceptor's exemption list (pkg/api/grpc/authz_mapping.go). Every
// returned method belongs to a service this module actually registers via
// RegisterGRPC (QuicBaaS, Login, FIDO2Service) — none of the seven broker
// capability services carry a public method.
func (m *Module) PublicGRPCMethods() []string {
	methods := make([]string, len(publicGRPCMethods))
	copy(methods, publicGRPCMethods)
	return methods
}

// RegisterGRPC attaches the module's gRPC surface to the unified server. The
// registered service descriptors are late-bound: each method handler
// resolves its implementation on invocation (see server), so registration is
// valid even though it runs before quicbaasd has composed the QuicBaaS
// facade. Until the resolver yields the facade — and for any broker capability
// the facade does not enable — every RPC answers codes.Unavailable: fail-safe,
// never a nil dereference.
func (m *Module) RegisterGRPC(reg grpc.ServiceRegistrar) {
	for _, entry := range grpcRegistrations {
		reg.RegisterService(lazyServiceDesc(entry.desc, m.server(entry), m.trustDomain), nil)
	}
}

// RegisterQUIC attaches the same late-bound services RegisterGRPC does to
// go-qrdb's QUIC frame transport. The QUIC server's registrar projects each
// unary method, authorized by its (qrdb.v1.permission) option; it skips the
// methods PublicGRPCMethods lists (QUIC is mTLS-only) and every streaming
// method.
func (m *Module) RegisterQUIC(reg grpc.ServiceRegistrar) {
	m.RegisterGRPC(reg)
}

// server returns the per-service implementation resolver: it binds the bundle
// on first use and answers Unavailable while the facade is still composing or
// when the capability is not enabled on this node. Host-built services
// resolve through hostServer instead and never enter the bundle.
func (m *Module) server(entry grpcRegistration) func() (any, error) {
	if entry.hostPick != nil {
		return m.hostServer(entry)
	}
	return func() (any, error) {
		servers, err := m.bind()
		if err != nil {
			return nil, err
		}
		impl := entry.pick(servers)
		if impl == nil {
			return nil, status.Error(codes.Unavailable,
				"quicbaas "+entry.capability+" service is not enabled on this node")
		}
		return impl, nil
	}
}

// hostServer returns the resolver for a host-built service. It asks the
// host on every call and caches nothing here, so a call that arrives while
// the host is still initialising (the resolver's error, typically
// Unavailable) never pins an incomplete server: the host builds and
// memoizes the servers once it is ready. Without WithHostGRPCServers the
// service answers Unavailable.
func (m *Module) hostServer(entry grpcRegistration) func() (any, error) {
	return func() (any, error) {
		unavailable := status.Error(codes.Unavailable,
			"quicbaas "+entry.capability+" service is not enabled on this node")
		if m.resolveHost == nil {
			return nil, unavailable
		}
		host, err := m.resolveHost()
		if err != nil {
			return nil, err
		}
		impl := entry.hostPick(host)
		if impl == nil {
			return nil, unavailable
		}
		return impl, nil
	}
}

// bind returns the memoized broker server bundle, constructing it on first
// use once the resolver yields the composed facade. Each server is built
// exactly the way pkg/server/lifecycle.go builds it for the legacy gRPC
// transport: the same constructors over the same facade accessors, with the
// same nil-gating (a disabled broker capability stays a nil bundle field).
// SSH is unconditional, mirroring lifecycle.go — with no SSH store the
// service verbs report SSHNotConfiguredError, a clearer contract than an
// unregistered service's Unimplemented.
//
// QuicBaaS, FIDO2Service, and Login are likewise unconditional: all three are
// registered by the legacy transport with no availability gate, so they are
// built once svc resolves rather than behind a per-capability nil check. The
// shared *grpcapi.Server facade backs QuicBaaS, FIDO2Service, and — when
// enabled — Embedding, exactly as pkg/server/lifecycle.go's single
// quicbaasService variable backs all three on the legacy transport.
func (m *Module) bind() (*grpcServers, error) {
	if servers := m.servers.Load(); servers != nil {
		return servers, nil
	}
	svc := m.resolve()
	if svc == nil {
		return nil, status.Error(codes.Unavailable,
			"quicbaas service facade is not yet available (server still composing)")
	}

	facade, err := grpcapi.NewServer(svc, m.log, m.nodeID, m.version)
	if err != nil {
		return nil, status.Error(codes.Internal,
			"quicbaas gRPC facade construction failed: "+err.Error())
	}
	// Apply the shard-router wiring the legacy gRPC transport applies to its
	// own facade instance (pkg/server/lifecycle.go's SetShardRouter/
	// SetVectorShardID calls, sourced identically by
	// pkg/server/qrdb_node.go's buildModuleShardRouter). Left at the facade's
	// NewServer defaults (single-shard routing.New(), shards.PlatformShardID)
	// when the host never supplied WithShardRouting.
	if m.shardRoutingSet {
		facade.SetShardRouter(m.shardRouter)
		facade.SetVectorShardID(m.vectorShardID)
	}

	built := &grpcServers{
		quicbaas: facade,
		fido2:    facade,
		login:    grpcapi.NewLoginServer(svc.LoginCeremony(), m.log),
		ssh:      grpcapi.NewSSHServer(service.SSHServiceFor(svc), m.log),
		// The prompt server resolves Prompts() per call and answers
		// Unavailable while no store is mounted, as on the Unix listener.
		prompt: grpcapi.NewPromptServer(svc, m.log),
	}
	if schedulerSvc, placementSvc := svc.Scheduler(), svc.Placement(); schedulerSvc != nil || placementSvc != nil {
		built.scheduler = grpcapi.NewSchedulerServer(schedulerSvc, placementSvc, m.log)
	}
	if agentSvc := svc.Agent(); agentSvc != nil {
		built.orchestrator = grpcapi.NewOrchestratorServer(agentSvc, m.log)
		built.agentCatalog = grpcapi.NewAgentCatalogServer(agentSvc, m.log)
	}
	if projectSvc := svc.Project(); projectSvc != nil {
		built.project = grpcapi.NewProjectServer(projectSvc, m.log)
	}
	if accountSvc := svc.Account(); accountSvc != nil {
		built.account = grpcapi.NewAccountServer(accountSvc, m.log)
	}
	if messagingSvc := svc.Messaging(); messagingSvc != nil {
		built.messaging = grpcapi.NewMessagingServer(messagingSvc, m.log)
	}
	if svc.Embedding() != nil {
		built.embedding = facade
	}
	if parameterSvc := svc.ParameterStoreService(); parameterSvc != nil {
		built.parameterStore = grpcapi.NewParameterStoreServer(parameterSvc, m.log)
	}
	// DynamicSecretsService's handlers live on the same *grpcapi.Server that
	// backs quicbaas/fido2/embedding above (DynamicSecretsWrapper restores the
	// proto method names, which collide with the IAM role verbs already on
	// *Server under bare names) — exactly the indirection
	// pkg/server/lifecycle.go's RegisterDynamicSecretsServiceServer(grpcServer,
	// quicbaasService) call uses.
	if dynSecretsSvc := svc.DynamicSecrets(); dynSecretsSvc != nil {
		built.dynamicSecrets = grpcapi.NewDynamicSecretsWrapper(facade)
	}
	if objSvc := svc.ObjectStorageService(); objSvc != nil {
		built.objectStorage = grpcapi.NewObjectStorageServer(objSvc)
	}
	if objAdminSvc := svc.ObjectStorageAdminService(); objAdminSvc != nil {
		built.objectStorageAdmin = grpcapi.NewObjectStorageAdminServer(objAdminSvc)
	}
	// buildOllamaServer is the rag-tag-gated indirection (module_ollama_rag.go
	// / module_ollama_norag.go): it nil-gates on svc.Ollama() under -tags rag
	// exactly like the other broker capabilities above, and is unconditionally
	// nil without the tag — matching lifecycle_rag_noop.go's registerOllamaService.
	built.ollama = buildOllamaServer(svc.Ollama(), m.log)

	// SecretsManagement is quicbaas's own public wire API over the composed
	// go-xkms library — registered here exactly like the seven broker
	// capability services above, with the SAME nil-gating
	// pkg/server/lifecycle.go applies on the legacy transport.
	if secretsSvc := svc.Secrets(); secretsSvc != nil {
		built.secretsManagement = grpcapi.NewSecretsServer(secretsSvc, m.log)
	}

	// The quicbaas.v1 crypto services the SDK's gRPC transport dials
	// (sdk/go/transport/grpc/{kms,crypto,certificate,trust,attestation,piv}
	// _handler.go), gated exactly as pkg/server/lifecycle.go gates them on
	// the Unix listener (#1960). The certificate, trust, attestation and PIV
	// handlers live on the shared facade.
	if kmsMgr := svc.KMSManager(); kmsMgr != nil {
		// KeyManagement mounts only with the tenant-scoped service: its
		// manager fallbacks take key names verbatim (#1992).
		if kmsSvc := svc.KMS(); kmsSvc != nil {
			built.keyManagement = grpcapi.NewKMSServerWithService(kmsMgr, kmsSvc, m.log)
			built.dekManagement = grpcapi.NewDEKServer(kmsSvc, m.log)
		}
	}
	if cryptoSvc := svc.Crypto(); cryptoSvc != nil {
		built.crypto = grpcapi.NewCryptoServer(cryptoSvc, m.log)
	}
	if svc.Certificate() != nil {
		built.certificate = facade
	}
	if svc.Trust() != nil {
		built.trust = facade
	}
	built.attestation = facade
	built.piv = facade

	// The registry, agent catalog and SDLC services are quicbaas-native, so
	// the node serves them as the Unix listener does (#1696). The SDLC server
	// resolves its binding service per call and answers Unavailable while
	// the module is disabled.
	built.registry = grpcapi.NewRegistryServer(svc.Registry())
	built.sdlc = grpcapi.NewSDLCServer(svc, m.log)

	if m.servers.CompareAndSwap(nil, built) {
		return built, nil
	}
	return m.servers.Load(), nil
}

// lazyServiceDesc copies a generated grpc.ServiceDesc, rebinding every unary
// and stream handler to resolve its implementation through impl at call time
// instead of at registration time. A resolution error is returned to the
// caller as the RPC's status; the wire surface (service name, method set,
// metadata, reflection descriptors) is byte-identical to the generated one.
// Every handler runs with the qrdb identity the unified server verified
// projected onto the quicbaas Subject (auth.ContextWithIdentitySubject), as
// the REST and MCP surfaces do, so services read their caller the same way
// on every transport. When that chain bound no tenant, the tenant of a
// verified client certificate is bound under trustDomain (peerTenantContext).
func lazyServiceDesc(src *grpc.ServiceDesc, impl func() (any, error), trustDomain string) *grpc.ServiceDesc {
	desc := &grpc.ServiceDesc{
		ServiceName: src.ServiceName,
		HandlerType: src.HandlerType,
		Metadata:    src.Metadata,
		Methods:     make([]grpc.MethodDesc, 0, len(src.Methods)),
		Streams:     make([]grpc.StreamDesc, 0, len(src.Streams)),
	}
	for _, md := range src.Methods {
		handler := md.Handler
		desc.Methods = append(desc.Methods, grpc.MethodDesc{
			MethodName: md.MethodName,
			Handler: func(_ any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
				svc, err := impl()
				if err != nil {
					return nil, err
				}
				return handler(svc, ctx, dec, projectingInterceptor(interceptor, trustDomain))
			},
		})
	}
	for _, sd := range src.Streams {
		handler := sd.Handler
		desc.Streams = append(desc.Streams, grpc.StreamDesc{
			StreamName:    sd.StreamName,
			ServerStreams: sd.ServerStreams,
			ClientStreams: sd.ClientStreams,
			Handler: func(_ any, ss grpc.ServerStream) error {
				svc, err := impl()
				if err != nil {
					return err
				}
				ctx, err := peerTenantContext(ss.Context(), trustDomain)
				if err != nil {
					return err
				}
				return handler(svc, &subjectStream{ServerStream: ss, ctx: auth.ContextWithIdentitySubject(ctx)})
			},
		})
	}
	return desc
}

// projectingInterceptor runs the server's unary chain, if any, and projects
// the identity it leaves on the context just before the method runs. A
// generated unary handler calls the chain itself, so the projection has to
// sit innermost: qrdb's auth chain is what binds the identity. The verified
// certificate's tenant is bound at the same point, so it sees what the chain
// bound and never replaces it.
func projectingInterceptor(chain grpc.UnaryServerInterceptor, trustDomain string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		project := func(ctx context.Context, req any) (any, error) {
			ctx, err := peerTenantContext(ctx, trustDomain)
			if err != nil {
				return nil, err
			}
			return handler(auth.ContextWithIdentitySubject(ctx), req)
		}
		if chain == nil {
			return project(ctx, req)
		}
		return chain(ctx, req, info, project)
	}
}

// subjectStream serves a stream under the context its handler reads, the one
// carrying the projected Subject. Stream interceptors wrap the stream
// handler, so the identity is already bound when it runs.
type subjectStream struct {
	grpc.ServerStream
	ctx context.Context
}

// Context returns the projected context.
func (s *subjectStream) Context() context.Context { return s.ctx }
