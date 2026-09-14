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

// XKMSBarrierRouteExclusions are the HTTP path prefixes the co-hosted go-xkms
// ServiceModule must NOT claim on a QuicBaaS-composed unified server.
//
// /api/v1/barrier/ is the canonical QuicBaaS system-barrier surface. The
// barrier is go-qrdb's (CLAUDE.md: "Identity / barrier / bootstrap are qrdb's,
// not quicbaas's"), quicbaas's handlers reach it through the qrdb SDK
// BarrierClient, and every other transport already answers from that same
// facade. The xkms module's same-path handlers operate xkms's OWN barrier,
// which a host composition never wires — so leaving them mounted reports
// "no barrier" for a barrier that exists.
var XKMSBarrierRouteExclusions = []string{
	"/api/v1/barrier/",
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
	return "apimodule: route-excluding wrapper requires at least one path prefix"
}

// routeExcludingModule wraps a co-hosted ServiceModule and drops its HTTP
// registrations under the configured path prefixes. Every other hook —
// gRPC, MCP, Start/Stop — and every optional qrdb SDK extension is forwarded
// verbatim, so the wrapper changes exactly one thing: which module owns the
// excluded HTTP paths.
type routeExcludingModule struct {
	qrdbsdk.ServiceModule

	prefixes []string

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
// pattern falls under any of prefixes are dropped, and returns the wrapper as
// a ServiceModule ready for qrdbsdk.WithModule.
//
// A prefix matches a pattern that has it as a literal prefix, and also the
// exact collection path formed by trimming the prefix's trailing slash (so
// "/api/v1/barrier/" excludes "/api/v1/barrier" too).
//
// The wrapped module MUST implement all three optional qrdb SDK extensions
// (MCP tool permissions, authentication fallback, public gRPC methods);
// construction returns a ModuleExtensionMissingError otherwise, because the
// wrapper's static method set would otherwise answer for an extension the
// module never declared.
func NewRouteExcludingModule(mod qrdbsdk.ServiceModule, prefixes []string) (qrdbsdk.ServiceModule, error) {
	if mod == nil {
		return nil, &NilModuleError{}
	}
	if len(prefixes) == 0 {
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
		prefixes:        append([]string(nil), prefixes...),
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

// excluded reports whether pattern falls under one of the wrapper's prefixes.
func (m *routeExcludingModule) excluded(pattern string) bool {
	for _, prefix := range m.prefixes {
		if strings.HasPrefix(pattern, prefix) {
			return true
		}
		if pattern == strings.TrimSuffix(prefix, "/") {
			return true
		}
	}
	return false
}
