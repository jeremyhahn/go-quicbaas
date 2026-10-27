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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// XKMSCertLoginRoutes are the public go-xkms routes whose credential is the
// TLS client certificate itself. The node middleware authenticates nothing on
// a public route, so no tenant is bound when the handler runs, and go-xkms
// looks the certificate up in the system tenant: a tenant admin's
// certificate, which is bound in its own tenant, is never found there.
var XKMSCertLoginRoutes = []string{
	"/api/v1/auth/login/piv",
}

// certLoginTenantModule binds, on its certificate-login routes, the tenant the
// verified client certificate names before the wrapped handler runs. Every
// other registration and every module extension passes through unchanged.
type certLoginTenantModule struct {
	qrdbsdk.ServiceModule
	qrdbsdk.ModuleMCPToolPermissioner
	qrdbsdk.ModuleAuthFallback
	qrdbsdk.ModuleGRPCPublicMethodLister

	trustDomain string
	routes      map[string]struct{}
}

// NewCertLoginTenantModule wraps mod so that each public route in routes runs
// with the tenant named by the request's verified TLS client certificate,
// resolved by qrdb's certificate-to-tenant rule (qrdbsdk.TenantFromClientCert)
// pinned to trustDomain. That is the binding qrdb documents for a composing
// host that resolves a certificate's user on a public route.
//
// A request with no verified client certificate, or whose certificate names
// no tenant, is passed through unchanged and the wrapped handler decides it.
// A certificate the rule refuses (a SPIFFE SAN from another trust domain, a
// malformed or ambiguous one) is answered 401 without reaching the handler.
// An empty trustDomain binds nothing, as on the module's gRPC surface.
func NewCertLoginTenantModule(mod qrdbsdk.ServiceModule, trustDomain string, routes []string) (qrdbsdk.ServiceModule, error) {
	if mod == nil {
		return nil, &NilModuleError{}
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

	set := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		set[route] = struct{}{}
	}
	return &certLoginTenantModule{
		ServiceModule:                mod,
		ModuleMCPToolPermissioner:    toolPermissions,
		ModuleAuthFallback:           authFallback,
		ModuleGRPCPublicMethodLister: publicGRPC,
		trustDomain:                  trustDomain,
		routes:                       set,
	}, nil
}

// RegisterHTTP forwards the wrapped module's registrations, wrapping the
// public handlers of the certificate-login routes in the tenant binding.
func (m *certLoginTenantModule) RegisterHTTP(r *qrdbsdk.ModuleHTTPRegistrar) {
	if r == nil {
		return
	}
	bound := qrdbsdk.NewModuleHTTPRegistrar(
		r.Router,
		r.Handle,
		func(method, pattern string, h http.HandlerFunc) {
			if _, ok := m.routes[pattern]; ok && m.trustDomain != "" {
				h = m.bindCertTenant(h)
			}
			r.HandlePublic(method, pattern, h)
		},
	)
	m.ServiceModule.RegisterHTTP(bound)
}

// bindCertTenant returns next run under the tenant the verified client
// certificate names.
func (m *certLoginTenantModule) bindCertTenant(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.VerifiedChains[0]) == 0 {
			next(w, r)
			return
		}
		tenantID, _, err := qrdbsdk.TenantFromClientCert(r.TLS.VerifiedChains[0][0], m.trustDomain)
		if err != nil {
			http.Error(w, "authentication failed", http.StatusUnauthorized)
			return
		}
		if tenantID == "" {
			next(w, r)
			return
		}
		ctx := qrdbsdk.ContextWithTenant(r.Context(), qrdbsdk.NormalizeTenantID(tenantID))
		next(w, r.WithContext(ctx))
	}
}
