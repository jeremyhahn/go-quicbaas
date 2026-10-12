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

package quicbaas

import (
	mcpserver "github.com/mark3labs/mcp-go/server"
	"google.golang.org/grpc"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/apimodule"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
)

// go-quicbaas's qrdb ServiceModule surface, re-exported so downstream hosts
// (go-trusted-platform) that embed a qrdb node can register the quicbaas
// broker capability plane via qrdbsdk.WithModule without importing
// go-quicbaas/pkg. The stack-wide convention is one module per provider
// project, named after the project; quicbaas's registers as
// ServiceModuleName ("quicbaas").

// ServiceModuleName is the unique name quicbaas's module registers under.
const ServiceModuleName = apimodule.ModuleName

// ServiceModule is go-quicbaas's broker capability plane (Scheduler,
// Orchestrator, Project, Account, SSH, Messaging, Embedding) as a qrdb
// ServiceModule.
type ServiceModule = apimodule.Module

// ServiceModuleOption configures optional ServiceModule behavior at
// construction time.
type ServiceModuleOption = apimodule.Option

// ServiceModuleResolver returns the composed service.QuicBaaS facade, or nil
// while the host is still composing it; the module binds lazily on the first
// RPC.
type ServiceModuleResolver = apimodule.ServiceResolver

// NewServiceModule builds go-quicbaas's ServiceModule around the QuicBaaS
// facade resolver. A nil resolver or logger is a composition error and fails
// construction.
var NewServiceModule = apimodule.NewModule

// WithServiceModuleNodeInfo supplies the node identity (nodeID, version) the
// module's Embedding facade constructor requires.
var WithServiceModuleNodeInfo = apimodule.WithNodeInfo

// =============================================================================
// Module-host seam — mounting a HOST's own ServiceModule
// =============================================================================
//
// The declarations above expose go-quicbaas's OWN module. The ones below are
// the other half: the stack-owned types a downstream host needs to WRITE a
// module of its own and mount it, without importing go-qrdb, go-xkms or
// go-quicbaas/pkg. Transport-specific THIRD-PARTY types are not re-exported
// wholesale: a module implementing RegisterGRPC still imports
// google.golang.org/grpc for its generated pb.Register<X>Server, RegisterMCP
// builds tools with mcp-go's own types, and a test that constructs a capture
// registrar supplies its own chi.Router.
//
// The canonical shape is quicbaas.WithModule passed to quicbaas.NewServer.
// go-quicbaas is the composer (ownership doc 00 L3/L7): building the unified
// qrdb node means assembling the single audit sink, the module shard router,
// the late-binding REST/MCP resolvers, go-xkms's module and the unified TLS
// config — state that lives inside the quicbaas Server. go-qrdb's SDK
// deliberately publishes only the server OPTIONS and no NewServer for exactly
// that reason, so there is no qrdb server constructor to re-export here; a
// host that called one would have to rebuild quicbaas's composition root.
// Mounting through quicbaas.NewServer keeps one node, one authorizer, one
// audit plane and one module registration point.
//
// Transport coverage a host must plan around:
//
//   - QUIC is reached only through RegisterQUIC, which BaseHostModule leaves a
//     no-op. go-qrdb's QUIC transport is a frame protocol, so RegisterHTTP does
//     not reach it; instead the QUIC server hands RegisterQUIC a registrar that
//     projects each unary method of a gRPC service descriptor onto its frames,
//     so a module usually implements it by delegating to RegisterGRPC. Every
//     projected method must declare a (qrdb.v1.permission) option: one without
//     panics the QUIC server at construction (its default-deny assertion),
//     except a method the module also lists as public, which QUIC skips because
//     it has no unauthenticated path. Streaming methods are not projected.
//   - The embedded (in-process) transport. That surface is each capability
//     owner's own SDK; a host calls its module's Go API directly in-process
//     instead of routing through the node.

// HostModule is the go-qrdb ServiceModule interface a downstream host
// implements to mount its own capability plane on the unified node. One
// registration reaches several transports: RegisterGRPC serves gRPC and unix,
// RegisterHTTP serves REST, RegisterMCP serves MCP. Embed BaseHostModule and
// override only the hooks the module actually uses.
type HostModule = qrdbsdk.ServiceModule

// BaseHostModule provides no-op defaults for every HostModule hook except
// Name. Embed it so a module keeps compiling as go-qrdb adds hooks.
type BaseHostModule = qrdbsdk.BaseModule

// ModuleHTTPRegistrar is the permission-recording registration surface a
// module's RegisterHTTP receives. Every route MUST go through Handle (which
// declares the route's permission at the registration site) or HandlePublic
// (reserved for genuine first-contact surfaces): those feed the same
// authorization registry and the same default-deny startup assertion as the
// node's built-in routes. A route registered straight onto the embedded
// chi.Router bypasses that bookkeeping and the node refuses to start.
type ModuleHTTPRegistrar = qrdbsdk.ModuleHTTPRegistrar

// NewModuleHTTPRegistrar builds a permission-recording registrar around a chi
// router and a pair of route/public callbacks. The node builds its own; this
// is for module tests that want to capture what a module registers.
var NewModuleHTTPRegistrar = qrdbsdk.NewModuleHTTPRegistrar

// ModuleGRPCRegistrar is the registrar a module's RegisterGRPC receives; the
// module calls its own generated pb.Register<X>Server(reg, impl) on it. A
// module still imports google.golang.org/grpc for its generated code — this
// alias only spares it the import in the method signature.
type ModuleGRPCRegistrar = grpc.ServiceRegistrar

// ModuleMCPServer is the shared MCP server a module's RegisterMCP receives
// (the hook takes a *ModuleMCPServer). Adding tools to it uses mcp-go's own
// types, which a module imports directly.
type ModuleMCPServer = mcpserver.MCPServer

// ModuleMCPToolPermissions is the optional HostModule extension a module
// implements to declare its MCP tools' permissions
// (MCPToolPermissions() map[string]Permission). Without it, a module tool
// registered via RegisterMCP stays fail-closed on an authenticated node.
type ModuleMCPToolPermissions = qrdbsdk.ModuleMCPToolPermissioner

// ModuleGRPCPublicMethods is the optional HostModule extension through which
// a module declares which of its gRPC methods must be reachable without
// authentication — the gRPC/unix counterpart of HandlePublic.
type ModuleGRPCPublicMethods = qrdbsdk.ModuleGRPCPublicMethodLister

// ModuleAuthFallback is the optional HostModule extension for a module that
// mints its own session tokens: when the node's REST middleware finds no
// identity on a request targeting the module's routes, it calls the owning
// module's AuthenticateHTTP. The returned identity is enriched and authorized
// by the node's single store-resolved path, so roles never come from the
// token. Implementations must fail closed.
type ModuleAuthFallback = qrdbsdk.ModuleAuthFallback

// ModuleIdentity is the authenticated principal a ModuleAuthFallback returns.
// It is go-qrdb's server-side auth identity, distinct from the login-ceremony
// Identity re-exported for SDK clients.
type ModuleIdentity = qrdbsdk.Identity

// Permission is a qrdb-canonical "resource:action" authorization token. A
// module names one at each Handle registration site and in
// MCPToolPermissions.
type Permission = qrdbsdk.Permission

// PermissionIdentityGated is the sentinel for operations that require an
// authenticated caller but defer authorization to the module's own handler.
const PermissionIdentityGated = qrdbsdk.PermissionIdentityGated

// XKMSServiceModuleName is the name go-xkms's module registers under on every
// composed node. It and ServiceModuleName are reserved: a host module must
// pick a different Name().
const XKMSServiceModuleName = xkmssdk.ServiceModuleName

// WithModule returns a ServerOption that mounts a host-supplied HostModule on
// the unified qrdb node NewServer builds, alongside go-quicbaas's own module
// and go-xkms's. The module's gRPC services, REST routes and MCP tools are
// attached to every transport the node serves, under the node's single
// authorizer and audit sink, and its Start/Stop lifecycle runs with the
// node's. Call it once per module.
//
// NewServer rejects a nil module, an empty Name(), and a name that duplicates
// ServiceModuleName, XKMSServiceModuleName or an earlier registration; node
// construction additionally rejects a REST route another module already
// claims. Each failure is a typed error (see the HostModule*Error aliases
// below) rather than a panic.
var WithModule = server.WithModule

// NilHostModuleError reports a nil module passed to WithModule.
type NilHostModuleError = server.NilHostModuleError

// UnnamedHostModuleError reports a host module whose Name() is empty.
type UnnamedHostModuleError = server.UnnamedHostModuleError

// HostModuleNameConflictError reports a host module whose Name() duplicates a
// module already mounted on the node; Owner names the incumbent.
type HostModuleNameConflictError = server.HostModuleNameConflictError

// HostModuleRouteConflictError reports two modules claiming the same REST
// route shape; Owner and Module name both claimants, and Pattern and
// OwnerPattern carry each module's own spelling of it (they differ when a
// path parameter was renamed).
type HostModuleRouteConflictError = server.HostModuleRouteConflictError

// HostModuleUnregisteredRouteError reports a module route registered straight
// onto the registrar's embedded chi.Router instead of through Handle or
// HandlePublic. Such a route declares no permission and can silently replace
// the handler for a route another module owns, so the mount fails closed.
type HostModuleUnregisteredRouteError = server.HostModuleUnregisteredRouteError

// HostModuleRouterInspectionError reports a failure to enumerate the routes a
// module registered on its router. The mount fails closed rather than
// proceeding on an incomplete picture of the module's REST surface.
type HostModuleRouterInspectionError = server.HostModuleRouterInspectionError

// HostModuleReservedRouteError reports a module route inside go-qrdb's own
// built-in REST namespace. Modules register ahead of built-ins and a built-in
// yields silently to a module that claimed its path, so such a route would take
// over a core endpoint under the module's authorization contract. Namespace the
// module's routes under a prefix of its own.
type HostModuleReservedRouteError = server.HostModuleReservedRouteError

// HostModuleInvalidRouteError reports a route pattern chi itself refuses (one
// that does not begin with "/", an unparseable regex constraint, an
// unsupported method). chi signals these by panicking; the mount converts that
// into this named error.
type HostModuleInvalidRouteError = server.HostModuleInvalidRouteError
