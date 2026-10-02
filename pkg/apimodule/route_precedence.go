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

package apimodule

import (
	"net/http"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// =============================================================================
// Route precedence between co-hosted ServiceModules
//
// QuicBaaS is the composer: it mounts BOTH its own ServiceModule and go-xkms's
// onto the SAME unified qrdb server (pkg/server/qrdb_node.go
// unifiedModuleServerOptions). Two modules cannot both claim a
// (method, pattern) pair — qrdb's registrar fails construction loudly on a
// duplicate — so where their route tables overlap, the composer must decide
// which module owns the path.
//
// /api/v1/barrier/* is such an overlap, and it is the one that produced
// go-quicbaas#405: the xkms module claimed it, its handler answered
// `503 {"error":"rest: barrier not configured"}` on a node whose barrier was
// configured AND unsealed, and quicbaas's own barrier routes — which delegate
// to the go-qrdb SDK BarrierClient that gRPC/unix/MCP/QUIC/embedded already
// serve — were excluded from the module surface to avoid the collision.
//
// go-xkms itself documents that surface as host-inert: its own
// pkg/api/rest/host.go NewHostServer states "Barrier, custodian, ceremony,
// SSH, and CA surfaces are xkmsd-only and stay disabled". Its route table
// registers them anyway (they use the unconditional `perm` helper rather than
// the enable-gated `permIf`), so the host has to drop them. This wrapper is
// how the composer does that, without reimplementing or forking anything.
// =============================================================================

// XKMSRouteExclusions are the HTTP routes the co-hosted go-xkms
// ServiceModule must NOT claim on a QuicBaaS-composed unified server. An entry
// ending in "/" excludes that whole subtree; any other entry excludes exactly
// that pattern.
//
// /api/v1/barrier/ is no longer excluded: go-xkms stopped registering its
// own barrier routes in a host composition, so quicbaas's canonical
// system-barrier surface (go-qrdb's barrier, go-quicbaas#405) has no
// co-hosted claimant left. TestRouteExcludingModule_XKMSBarrierCollisionIsReal
// pins that, so the exclusion comes back if go-xkms ever registers them again.
//
// /api/v1/cluster/status is the composed node's cluster view. QuicBaaS's
// handler (pkg/api/rest HandleClusterStatus) answers from the service
// layer's ClusterStatus across every shard the node hosts — node ID, Raft
// address, leader, members, per-shard state, health — in the flat shape the
// quicbaas SDK clients and UI decode. go-xkms v0.30's same-path handler
// reports a single shard wrapped as {"status":{...}}, a different wire shape
// no quicbaas consumer reads. go-xkms's /api/v1/cluster/members has no
// quicbaas counterpart and stays mounted.
//
// /api/v1/tenants/ is the tenant control plane, which is quicbaas's over the
// embedded qrdb node (CLAUDE.md: "System admin provisions tenants minimally
// (control plane)"), per-tenant barrier included. go-xkms registers nine
// routes under it, every one enable-gated on tenant handlers that its host
// server never builds (they need a barrier registry, and the barrier surfaces
// are xkmsd-only), so on a composed node each one can only answer 501. Four
// of them name the tenant wildcard {tenantID} where quicbaas's name it
// {tenant_id}; the two spellings are different registrar keys, so neither
// the registrar's duplicate check nor the overlap test below saw a collision,
// and the router served xkms's 501 for GET /api/v1/tenants/{id}/barrier/status
// on a node whose tenant barrier answers normally everywhere else.
//
// /api/v1/backups/ is the composed node's backup surface, quicbaas's
// (pkg/api/rest handlers_backup.go over the service layer's BackupManager:
// list, create, delete, restore, status, verify), which the quicbaas SDK and
// UI decode. go-xkms v0.47 (go-xkms#728) registers its own node-backup
// table on the same paths for xkmsd; on a composed node four of its routes
// collide outright (GET/POST /api/v1/backups, DELETE /{id}, POST
// /{id}/verify) and the rest (GET /{id}, POST /restore) would answer a
// different wire shape beside quicbaas's. A live qrdb registrar panics on
// the duplicates, so the whole subtree stays quicbaas's.
var XKMSRouteExclusions = []string{
	"/api/v1/backups/",
	"/api/v1/cluster/status",
	"/api/v1/tenants/",
}

// ModuleExtensionMissingError reports a co-hosted ServiceModule that does not
// implement an optional extension the wrapper must forward. The wrapper cannot
// forward what the module does not implement, and silently answering the
// extension itself would strip a real declaration (an MCP tool's permission, a
// public gRPC method, an authentication fallback) and fail closed at runtime in
// a way that looks like a policy decision. Construction fails loudly instead.
type ModuleExtensionMissingError struct {
	// Module is the wrapped module's Name().
	Module string

	// Extension names the qrdb SDK optional extension interface.
	Extension string
}

// Error describes the missing extension.
func (e *ModuleExtensionMissingError) Error() string {
	return "apimodule: co-hosted module " + e.Module + " does not implement " + e.Extension +
		"; route-precedence wrapping would drop its declarations"
}

// NilModuleError reports a nil module passed to NewRouteExcludingModule.
type NilModuleError struct{}

// Error describes the nil module.
func (e *NilModuleError) Error() string {
	return "apimodule: route-excluding wrapper requires a non-nil ServiceModule"
}

// EmptyRouteExclusionsError reports a wrapper built with no exclusions. A
// wrapper that excludes nothing is pure indirection that hides the composer's
// intent, so it is rejected rather than accepted as a no-op.
type EmptyRouteExclusionsError struct{}

// Error describes the empty exclusion set.
func (e *EmptyRouteExclusionsError) Error() string {
	return "apimodule: route-excluding wrapper requires at least one route exclusion"
}

// routeExcludingModule wraps a co-hosted ServiceModule and drops its HTTP
// registrations matching the configured exclusions. Every other hook —
// gRPC, MCP, Start/Stop — and every optional qrdb SDK extension is forwarded
// verbatim, so the wrapper changes exactly one thing: which module owns the
// excluded HTTP paths.
type routeExcludingModule struct {
	qrdbsdk.ServiceModule

	exclusions []string

	toolPermissions qrdbsdk.ModuleMCPToolPermissioner
	authFallback    qrdbsdk.ModuleAuthFallback
	publicGRPC      qrdbsdk.ModuleGRPCPublicMethodLister
}

// Compile-time proof the wrapper carries the full co-hosted module contract:
// the base ServiceModule plus all three optional extensions the unified server
// type-asserts for.
var (
	_ qrdbsdk.ServiceModule                = (*routeExcludingModule)(nil)
	_ qrdbsdk.ModuleMCPToolPermissioner    = (*routeExcludingModule)(nil)
	_ qrdbsdk.ModuleAuthFallback           = (*routeExcludingModule)(nil)
	_ qrdbsdk.ModuleGRPCPublicMethodLister = (*routeExcludingModule)(nil)
)

// NewRouteExcludingModule wraps mod so its RegisterHTTP registrations whose
// pattern matches any of exclusions are dropped, and returns the wrapper as a
// ServiceModule ready for qrdbsdk.WithModule.
//
// An exclusion ending in "/" is a subtree: it matches every pattern it is a
// literal prefix of, and also the collection path formed by trimming the
// trailing slash (so "/api/v1/barrier/" excludes "/api/v1/barrier" too). Any
// other exclusion matches exactly one pattern, so excluding
// "/api/v1/cluster/status" leaves "/api/v1/cluster/members" mounted.
//
// The wrapped module MUST implement all three optional qrdb SDK extensions
// (MCP tool permissions, authentication fallback, public gRPC methods);
// construction returns a ModuleExtensionMissingError otherwise, because the
// wrapper's static method set would otherwise answer for an extension the
// module never declared.
func NewRouteExcludingModule(mod qrdbsdk.ServiceModule, exclusions []string) (qrdbsdk.ServiceModule, error) {
	if mod == nil {
		return nil, &NilModuleError{}
	}
	if len(exclusions) == 0 {
		return nil, &EmptyRouteExclusionsError{}
	}

	toolPermissions, ok := mod.(qrdbsdk.ModuleMCPToolPermissioner)
	if !ok {
		return nil, &ModuleExtensionMissingError{Module: mod.Name(), Extension: "ModuleMCPToolPermissioner"}
	}
	authFallback, ok := mod.(qrdbsdk.ModuleAuthFallback)
	if !ok {
		return nil, &ModuleExtensionMissingError{Module: mod.Name(), Extension: "ModuleAuthFallback"}
	}
	publicGRPC, ok := mod.(qrdbsdk.ModuleGRPCPublicMethodLister)
	if !ok {
		return nil, &ModuleExtensionMissingError{Module: mod.Name(), Extension: "ModuleGRPCPublicMethodLister"}
	}

	return &routeExcludingModule{
		ServiceModule:   mod,
		exclusions:      append([]string(nil), exclusions...),
		toolPermissions: toolPermissions,
		authFallback:    authFallback,
		publicGRPC:      publicGRPC,
	}, nil
}

// RegisterHTTP forwards the wrapped module's registrations through a
// substitute registrar that drops the excluded patterns. The substitute shares
// the host's live chi.Router, so a module registering straight on the router
// (bypassing Handle/HandlePublic) is unaffected — qrdb's own default-deny
// startup assertion is what catches those.
func (m *routeExcludingModule) RegisterHTTP(r *qrdbsdk.ModuleHTTPRegistrar) {
	if r == nil {
		return
	}
	filtered := qrdbsdk.NewModuleHTTPRegistrar(
		r.Router,
		func(method, pattern string, perm qrdbsdk.Permission, h http.HandlerFunc) {
			if m.excluded(pattern) {
				return
			}
			r.Handle(method, pattern, perm, h)
		},
		func(method, pattern string, h http.HandlerFunc) {
			if m.excluded(pattern) {
				return
			}
			r.HandlePublic(method, pattern, h)
		},
	)
	m.ServiceModule.RegisterHTTP(filtered)
}

// MCPToolPermissions forwards the wrapped module's tool permission
// declarations unchanged.
func (m *routeExcludingModule) MCPToolPermissions() map[string]qrdbsdk.Permission {
	return m.toolPermissions.MCPToolPermissions()
}

// AuthenticateHTTP forwards the wrapped module's authentication fallback
// unchanged.
func (m *routeExcludingModule) AuthenticateHTTP(r *http.Request) (*qrdbsdk.Identity, error) {
	return m.authFallback.AuthenticateHTTP(r)
}

// PublicGRPCMethods forwards the wrapped module's public gRPC method
// declarations unchanged.
func (m *routeExcludingModule) PublicGRPCMethods() []string {
	return m.publicGRPC.PublicGRPCMethods()
}

// excluded reports whether pattern matches one of the wrapper's exclusions:
// a subtree exclusion (trailing "/") by prefix or as its collection path, any
// other exclusion exactly.
func (m *routeExcludingModule) excluded(pattern string) bool {
	for _, exclusion := range m.exclusions {
		if pattern == strings.TrimSuffix(exclusion, "/") {
			return true
		}
		if strings.HasSuffix(exclusion, "/") && strings.HasPrefix(pattern, exclusion) {
			return true
		}
	}
	return false
}
