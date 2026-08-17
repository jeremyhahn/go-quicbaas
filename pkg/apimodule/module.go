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
// AttestationService, and PIVService are DELIBERATELY NOT registered here
// (Phase 3 of the login-plane plan, corrective to the prior design): they
// were quicbaas.v1 wrappers over the SAME composed go-xkms library
// (xkms.Get(), see pkg/server/qrdb_node.go's xkmsModule composition) that
// go-xkms's OWN ServiceModule ("xkms", go-xkms/sdk/go/apimodule.go) now
// registers on this SAME unified server as its canonical xkms.v1
// KeystoreService surface. Registering both here would duplicate that
// surface under a redundant proto name — the same reasoning that already
// excluded the qrdb-proxy planes above. The quicbaas SDK's gRPC crypto
// clients (sdk/go/transport/grpc/{certificate,trust,attestation,piv,kms,
// crypto}_handler.go) re-export go-xkms's canonical SDK clients as thin
// aliases instead (sdk/go/xkms_clients.go) so a downstream consumer
// (go-trusted-platform) keeps ONE import point for the capability regardless
// of which project's wire contract now serves it. quicbaas's legacy gRPC
// transport (pkg/server/lifecycle.go, lifecycle_certificate_register.go,
// lifecycle_fido2_piv_register.go) is UNCHANGED by this: it still registers
// its own quicbaas.v1 handlers for these seven services, so a caller already
// dialing the legacy listener directly (not through the unified server or
// the SDK) keeps working exactly as before.
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
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
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
}

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
// serving it.
type grpcRegistration struct {
	desc       *grpc.ServiceDesc
	capability string
	pick       func(*grpcServers) any
}

// grpcRegistrations is the full gRPC surface the module registers: the
// quicbaas-native core plane (QuicBaaS, Login, FIDO2Service) followed by the
// seven broker capability services pkg/server/lifecycle.go registers on the
// legacy transport, in the same construction shapes (see bind).
var grpcRegistrations = []grpcRegistration{
	{&pb.QuicBaaS_ServiceDesc, "quicbaas", func(s *grpcServers) any { return s.quicbaas }},
	{&pb.Login_ServiceDesc, "login", func(s *grpcServers) any { return s.login }},
	{&pb.FIDO2Service_ServiceDesc, "fido2", func(s *grpcServers) any { return s.fido2 }},
	{&pb.SchedulerService_ServiceDesc, "scheduler", func(s *grpcServers) any { return s.scheduler }},
	{&pb.OrchestratorService_ServiceDesc, "orchestrator", func(s *grpcServers) any { return s.orchestrator }},
	{&pb.ProjectService_ServiceDesc, "project", func(s *grpcServers) any { return s.project }},
	{&pb.AccountService_ServiceDesc, "account", func(s *grpcServers) any { return s.account }},
	{&pb.SSHService_ServiceDesc, "ssh", func(s *grpcServers) any { return s.ssh }},
	{&pb.MessagingService_ServiceDesc, "messaging", func(s *grpcServers) any { return s.messaging }},
	{&pb.EmbeddingService_ServiceDesc, "embedding", func(s *grpcServers) any { return s.embedding }},
	{&pb.ParameterStore_ServiceDesc, "parameter-store", func(s *grpcServers) any { return s.parameterStore }},
	{&pb.DynamicSecretsService_ServiceDesc, "dynamic-secrets", func(s *grpcServers) any { return s.dynamicSecrets }},
	{&pb.ObjectStorageService_ServiceDesc, "object-storage", func(s *grpcServers) any { return s.objectStorage }},
	{&pb.ObjectStorageAdmin_ServiceDesc, "object-storage-admin", func(s *grpcServers) any { return s.objectStorageAdmin }},
	{&pb.OllamaService_ServiceDesc, "ollama", func(s *grpcServers) any { return s.ollama }},
	{&pb.SecretsManagement_ServiceDesc, "secrets-management", func(s *grpcServers) any { return s.secretsManagement }},
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
		reg.RegisterService(lazyServiceDesc(entry.desc, m.server(entry)), nil)
	}
}

// server returns the per-service implementation resolver: it binds the bundle
// on first use and answers Unavailable while the facade is still composing or
// when the capability is not enabled on this node.
func (m *Module) server(entry grpcRegistration) func() (any, error) {
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
		ssh:      grpcapi.NewSSHServer(service.NewSSHService(svc.SSHClient()), m.log),
	}
	if schedulerSvc := svc.Scheduler(); schedulerSvc != nil {
		built.scheduler = grpcapi.NewSchedulerServer(schedulerSvc, m.log)
	}
	if agentSvc := svc.Agent(); agentSvc != nil {
		built.orchestrator = grpcapi.NewOrchestratorServer(agentSvc, m.log)
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
	//
	// KeyManagement, DEKManagement, Crypto, CertificateService, TrustService,
	// AttestationService, and PIVService are deliberately NOT bound here: see
	// the package doc comment (Phase 3 of the login-plane plan) — go-xkms's
	// own ServiceModule now serves that canonical surface on this same
	// unified server, and quicbaas's legacy gRPC transport is unaffected.
	if secretsSvc := svc.Secrets(); secretsSvc != nil {
		built.secretsManagement = grpcapi.NewSecretsServer(secretsSvc, m.log)
	}

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
func lazyServiceDesc(src *grpc.ServiceDesc, impl func() (any, error)) *grpc.ServiceDesc {
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
				return handler(svc, ctx, dec, interceptor)
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
				return handler(svc, ss)
			},
		})
	}
	return desc
}
