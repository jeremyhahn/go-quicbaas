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
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// XKMSCertLoginRoutes are the public go-xkms routes whose credential is the
// TLS client certificate itself. The node middleware authenticates nothing on
// a public route, so no tenant is bound when the handler runs, and go-xkms
// looks the certificate up in the system tenant: a tenant admin's
// certificate, which is bound in its own tenant, is never found there.
var XKMSCertLoginRoutes = []string{
	"/api/v1/auth/login/piv",
}

// certLoginTenantModule refuses, on its certificate-login routes, a verified
// client certificate that has been revoked, then binds the tenant the
// certificate names before the wrapped handler runs. Every other registration
// and every module extension passes through unchanged.
type certLoginTenantModule struct {
	qrdbsdk.ServiceModule
	qrdbsdk.ModuleMCPToolPermissioner
	qrdbsdk.ModuleAuthFallback
	qrdbsdk.ModuleGRPCPublicMethodLister

	trustDomain string
	routes      map[string]struct{}
	revocation  qrdbsdk.RevocationChecker
}

// NilRevocationCheckerError reports a certificate-login wrapper built without
// a revocation checker. The login route is public, so nothing upstream of the
// wrapped handler checks revocation: a wrapper without a checker would mint
// tokens for revoked certificates.
type NilRevocationCheckerError struct{}

// Error describes the missing checker.
func (e *NilRevocationCheckerError) Error() string {
	return "apimodule: certificate-login wrapper requires a non-nil revocation checker"
}

// NewCertLoginTenantModule wraps mod so that each public route in routes first
// refuses a verified TLS client certificate that revocation reports revoked,
// and then runs with the tenant that certificate names, resolved by qrdb's
// certificate-to-tenant rule (qrdbsdk.TenantFromClientCert) pinned to
// trustDomain. That is the binding qrdb documents for a composing host that
// resolves a certificate's user on a public route.
//
// The route is public, so qrdb's authenticator, which consults its
// RevocationChecker on every authenticated request, never runs on it: the
// wrapper applies the same contract itself. A revoked certificate is answered
// 401 and a revocation lookup that fails is answered 503, as qrdb maps
// RevocationCheckError, so the login fails closed in both cases.
//
// A request with no verified client certificate is passed through unchanged
// and the wrapped handler decides it, as is one whose certificate names no
// tenant. A certificate the rule refuses (a SPIFFE SAN from another trust
// domain, a malformed or ambiguous one) is answered 401 without reaching the
// handler. An empty trustDomain binds no tenant, as on the module's gRPC
// surface, but revocation is still checked.
func NewCertLoginTenantModule(mod qrdbsdk.ServiceModule, trustDomain string, routes []string, revocation qrdbsdk.RevocationChecker) (qrdbsdk.ServiceModule, error) {
	if mod == nil {
		return nil, &NilModuleError{}
	}
	if revocation == nil {
		return nil, &NilRevocationCheckerError{}
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
		revocation:                   revocation,
	}, nil
}

// RegisterHTTP forwards the wrapped module's registrations, wrapping the
// public handlers of the certificate-login routes in the revocation check and
// the tenant binding.
func (m *certLoginTenantModule) RegisterHTTP(r *qrdbsdk.ModuleHTTPRegistrar) {
	if r == nil {
		return
	}
	bound := qrdbsdk.NewModuleHTTPRegistrar(
		r.Router,
		r.Handle,
		func(method, pattern string, h http.HandlerFunc) {
			if _, ok := m.routes[pattern]; ok {
				h = m.bindCertTenant(h)
			}
			r.HandlePublic(method, pattern, h)
		},
	)
	m.ServiceModule.RegisterHTTP(bound)
}

// bindCertTenant returns next run under the tenant the verified client
// certificate names, once revocation has cleared that certificate.
func (m *certLoginTenantModule) bindCertTenant(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.VerifiedChains[0]) == 0 {
			next(w, r)
			return
		}
		leaf := r.TLS.VerifiedChains[0][0]
		revoked, err := m.revocation.IsCertRevoked(r.Context(), leaf)
		if err != nil {
			http.Error(w, "certificate revocation status unavailable", http.StatusServiceUnavailable)
			return
		}
		if revoked {
			http.Error(w, "authentication failed", http.StatusUnauthorized)
			return
		}
		if m.trustDomain == "" {
			next(w, r)
			return
		}
		tenantID, _, err := qrdbsdk.TenantFromClientCert(leaf, m.trustDomain)
		if err != nil {
			http.Error(w, "authentication failed", http.StatusUnauthorized)
			return
		}
		if tenantID == "" {
			next(w, r)
			return
		}
		// The subject OU carries the tenant too, and must agree with the
		// SAN (#1388, go-qrdb#777).
		if err := iam.CheckCertOUTenant(leaf, tenantID); err != nil {
			http.Error(w, "authentication failed", http.StatusUnauthorized)
			return
		}
		ctx := qrdbsdk.ContextWithTenant(r.Context(), qrdbsdk.NormalizeTenantID(tenantID))
		next(w, r.WithContext(ctx))
	}
}
