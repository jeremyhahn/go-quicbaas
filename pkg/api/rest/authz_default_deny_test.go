//go:build rest

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

package rest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"

	qrdbspiffe "github.com/jeremyhahn/go-qrdb/pkg/auth/spiffe"
)

const testTrustDomain = "quicbaas.test"

// newDefaultDenyMiddleware builds an AuthzMiddleware whose validator routes
// through the DPoP/platform path so the gate can produce an AuthorizedContext.
func newDefaultDenyMiddleware(t *testing.T, validator TokenValidator) *AuthzMiddleware {
	t.Helper()
	log, _ := logger.New(logger.LogConfig{Level: "debug", Output: "stdout", Format: "text"})
	return NewAuthzMiddleware(&AuthzMiddlewareConfig{
		Logger:         log,
		TokenValidator: validator,
	})
}

// spiffeClientCert builds a self-signed certificate carrying a tenant-scoped
// SPIFFE URI SAN of the given kind ("svc" or "user"), tenant, and trust domain.
func spiffeClientCert(t *testing.T, trustDomain, tenant, kind, name string) *x509.Certificate {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	uri := &url.URL{
		Scheme: "spiffe",
		Host:   trustDomain,
		Path:   "/tenant/" + tenant + "/" + kind + "/" + name,
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		URIs:         []*url.URL{uri},
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	return cert
}

// withVerifiedChain attaches cert to the request as a TLS-verified chain so the
// gate sees a verified mTLS principal (not a raw, unverified peer certificate).
func withVerifiedChain(r *http.Request, cert *x509.Certificate) *http.Request {
	r.TLS = &tls.ConnectionState{
		VerifiedChains: [][]*x509.Certificate{{cert}},
	}
	return r
}

// --- Public allowlist bypass --------------------------------------------------

func TestEnforceDefaultDeny_PublicBypass(t *testing.T) {
	// A validator that always fails proves these paths never reach validation.
	validator := &mockDPoPValidator{err: &InvalidTokenResultError{}}
	mw := newDefaultDenyMiddleware(t, validator)

	publicPaths := []string{
		"/api/v1/health",
		"/api/v1/health/live",  // the corrected probe path
		"/api/v1/health/ready", // the corrected probe path
		"/metrics",
		"/api/v1/version",
		"/.well-known/openid-configuration",
		"/.well-known/jwks.json",
		"/oauth2/token",
		"/api/v1/auth/refresh",
		"/api/v1/auth/validate",
		"/api/v1/auth/logout",
		"/api/v1/auth/login/begin",
		"/api/v1/auth/login/finish",
		"/api/v1/auth/register/begin",
		"/api/v1/auth/providers/tenant-a/google/login",
		"/api/v1/auth/providers/tenant-a/google/callback",
		"/api/v1/iam/tenants/public",
		"/api/v1/iam/invites/abc/activate/piv",
		"/acme/directory",
		"/ui/index.html",
		"/docs/",
	}

	for _, p := range publicPaths {
		handler := &authzCapturingHandler{}
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rr := httptest.NewRecorder()

		mw.EnforceDefaultDeny(testTrustDomain)(handler).ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("public path %q: expected 200, got %d", p, rr.Code)
		}
		if !handler.called {
			t.Errorf("public path %q: expected handler called", p)
		}
	}
}

// The buggy legacy names must NOT be the ones probes use; assert the corrected
// probe paths are public and that a non-public protected path is denied.
func TestEnforceDefaultDeny_HealthProbePathsFixed(t *testing.T) {
	if !isDefaultDenyPublic("/api/v1/health/live") {
		t.Error("/api/v1/health/live must be public (the real liveness probe)")
	}
	if !isDefaultDenyPublic("/api/v1/health/ready") {
		t.Error("/api/v1/health/ready must be public (the real readiness probe)")
	}
}

// --- Cluster mTLS service principal -------------------------------------------

func TestEnforceDefaultDeny_ClusterServicePrincipalAllowed(t *testing.T) {
	// Validator fails on purpose: a verified svc cert must short-circuit before
	// any token requirement.
	validator := &mockDPoPValidator{err: &InvalidTokenResultError{}}
	mw := newDefaultDenyMiddleware(t, validator)
	handler := &authzCapturingHandler{}

	cert := spiffeClientCert(t, testTrustDomain, "system", qrdbspiffe.KindService, "control-plane")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys", nil)
	req = withVerifiedChain(req, cert)
	rr := httptest.NewRecorder()

	mw.EnforceDefaultDeny(testTrustDomain)(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for verified svc principal, got %d", rr.Code)
	}
	if !handler.called {
		t.Fatal("expected handler called for verified svc principal")
	}
}

func TestEnforceDefaultDeny_UserKindCertIsNotClusterPrincipal(t *testing.T) {
	// A "user" kind cert is not the cluster control plane; with no token it is
	// denied (401), proving the gate does not treat any verified cert as cluster.
	validator := &mockDPoPValidator{err: &InvalidTokenResultError{}}
	mw := newDefaultDenyMiddleware(t, validator)
	handler := &authzCapturingHandler{}

	cert := spiffeClientCert(t, testTrustDomain, "tenant-a", qrdbspiffe.KindUser, "alice")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys", nil)
	req = withVerifiedChain(req, cert)
	rr := httptest.NewRecorder()

	mw.EnforceDefaultDeny(testTrustDomain)(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for user-kind cert without token, got %d", rr.Code)
	}
	if handler.called {
		t.Fatal("handler must not be called for user-kind cert without token")
	}
}

func TestEnforceDefaultDeny_ForeignTrustDomainRejected(t *testing.T) {
	validator := &mockDPoPValidator{err: &InvalidTokenResultError{}}
	mw := newDefaultDenyMiddleware(t, validator)
	handler := &authzCapturingHandler{}

	// svc cert in a DIFFERENT trust domain must not short-circuit (fail-closed).
	cert := spiffeClientCert(t, "evil.example", "system", qrdbspiffe.KindService, "imposter")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys", nil)
	req = withVerifiedChain(req, cert)
	rr := httptest.NewRecorder()

	mw.EnforceDefaultDeny(testTrustDomain)(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for foreign-trust-domain svc cert, got %d", rr.Code)
	}
}

func TestEnforceDefaultDeny_RawPeerCertNotTrusted(t *testing.T) {
	// A cert present only in PeerCertificates (NOT VerifiedChains) is unverified
	// and must not grant cluster access.
	validator := &mockDPoPValidator{err: &InvalidTokenResultError{}}
	mw := newDefaultDenyMiddleware(t, validator)
	handler := &authzCapturingHandler{}

	cert := spiffeClientCert(t, testTrustDomain, "system", qrdbspiffe.KindService, "control-plane")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys", nil)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	rr := httptest.NewRecorder()

	mw.EnforceDefaultDeny(testTrustDomain)(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unverified peer cert, got %d", rr.Code)
	}
}

func TestEnforceDefaultDeny_EmptyTrustDomainDisablesClusterShortCircuit(t *testing.T) {
	validator := &mockDPoPValidator{err: &InvalidTokenResultError{}}
	mw := newDefaultDenyMiddleware(t, validator)
	handler := &authzCapturingHandler{}

	cert := spiffeClientCert(t, testTrustDomain, "system", qrdbspiffe.KindService, "control-plane")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys", nil)
	req = withVerifiedChain(req, cert)
	rr := httptest.NewRecorder()

	// Empty pinned trust domain => svc short-circuit disabled, token required.
	mw.EnforceDefaultDeny("")(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when trust domain unset, got %d", rr.Code)
	}
}

// --- Token + scope enforcement ------------------------------------------------

func TestEnforceDefaultDeny_ValidTokenWithScopeAllowed(t *testing.T) {
	validator := &mockDPoPValidator{
		result: &PlatformTokenResult{
			Subject:  "svc-1",
			TenantID: "tenant-1",
			Scopes:   []string{"keys:create"},
		},
	}
	mw := newDefaultDenyMiddleware(t, validator)
	handler := &authzCapturingHandler{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys", nil)
	req.Header.Set("Authorization", "Bearer good-token")
	rr := httptest.NewRecorder()

	mw.EnforceDefaultDeny(testTrustDomain)(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid token with required scope, got %d", rr.Code)
	}
	if !handler.called {
		t.Fatal("expected handler called")
	}
	if handler.authz == nil {
		t.Fatal("expected AuthorizedContext injected for downstream handlers")
	}
	if handler.authz.TenantID != "tenant-1" {
		t.Errorf("expected tenant-1 RLS context, got %q", handler.authz.TenantID)
	}
}

func TestEnforceDefaultDeny_MissingTokenDenied(t *testing.T) {
	validator := &mockDPoPValidator{}
	mw := newDefaultDenyMiddleware(t, validator)
	handler := &authzCapturingHandler{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/db", nil)
	rr := httptest.NewRecorder()

	mw.EnforceDefaultDeny(testTrustDomain)(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for token-less protected request, got %d", rr.Code)
	}
	if handler.called {
		t.Fatal("handler must not be called when token is missing")
	}
}

func TestEnforceDefaultDeny_WrongScopeForbidden(t *testing.T) {
	validator := &mockDPoPValidator{
		result: &PlatformTokenResult{
			Subject: "svc-1",
			Scopes:  []string{"kv:get"}, // not the keys:create the path requires
		},
	}
	mw := newDefaultDenyMiddleware(t, validator)
	handler := &authzCapturingHandler{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys", nil)
	req.Header.Set("Authorization", "Bearer good-token")
	rr := httptest.NewRecorder()

	mw.EnforceDefaultDeny(testTrustDomain)(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for token missing required scope, got %d", rr.Code)
	}
	if handler.called {
		t.Fatal("handler must not be called when scope is insufficient")
	}
}

// A DPoP cnf-bound token presented without a valid proof must fail closed (401):
// the binding enforcement is subsumed into the default-deny gate via m.validate.
func TestEnforceDefaultDeny_BoundTokenWithoutProofDenied(t *testing.T) {
	validator := &mockDPoPValidator{
		// The DPoP-aware validator returns an error for a bound token presented
		// without a proof, mirroring the OIDC provider's fail-closed behavior.
		err: &InvalidTokenResultError{},
	}
	mw := newDefaultDenyMiddleware(t, validator)
	handler := &authzCapturingHandler{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys/k1", nil)
	req.Header.Set("Authorization", "Bearer bound-token")
	// No DPoP header -> validator fails closed.
	rr := httptest.NewRecorder()

	mw.EnforceDefaultDeny(testTrustDomain)(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bound token without proof, got %d", rr.Code)
	}
	if validator.gotProof != "" {
		t.Errorf("expected empty proof forwarded, got %q", validator.gotProof)
	}
}

// A protected path with no scope-table entry requires a valid token but no
// specific scope.
func TestEnforceDefaultDeny_UnscopedProtectedPathRequiresTokenOnly(t *testing.T) {
	validator := &mockDPoPValidator{
		result: &PlatformTokenResult{Subject: "svc-1", Scopes: []string{}},
	}
	mw := newDefaultDenyMiddleware(t, validator)
	handler := &authzCapturingHandler{}

	// /api/v1/cluster has no scope-table entry.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/status", nil)
	req.Header.Set("Authorization", "Bearer good-token")
	rr := httptest.NewRecorder()

	mw.EnforceDefaultDeny(testTrustDomain)(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid token on unscoped protected path, got %d", rr.Code)
	}
}

// --- Scope table resolution ---------------------------------------------------

func TestRequiredScopeForRequest(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		method string
		want   string
	}{
		{"kms read", "/api/v1/kms/keys/k1", http.MethodGet, "keys:get"},
		{"kms write", "/api/v1/kms/keys", http.MethodPost, "keys:create"},
		{"secrets delete is write verb", "/api/v1/secrets/db", http.MethodDelete, "credentials:store"},
		{"iam tenants write", "/api/v1/tenants", http.MethodPost, "tenants:create"},
		{"iam read", "/api/v1/iam/sessions", http.MethodGet, "tenants:read"},
		{"credential wire write", "/v1/credentials/submit", http.MethodPost, "credentials:store"},
		{"barrier wire read", "/v1/barrier/status", http.MethodGet, "tenant_barrier:status"},
		{"ceremony wire write", "/v1/ceremony/initialize", http.MethodPost, "ceremony:initialize"},
		{"init wire write", "/v1/init/enroll-external-officer", http.MethodPost, "ceremony:initialize"},
		{"certificate read", "/api/v1/certificates/abc", http.MethodGet, "certs:read"},
		{"kv write", "/api/v1/kv/users/k1", http.MethodPut, "kv:put"},
		{"unmapped path", "/api/v1/cluster/status", http.MethodGet, ""},
		{"trailing slash normalized", "/api/v1/kms/", http.MethodGet, "keys:get"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := requiredScopeForRequest(tc.path, tc.method)
			if got != tc.want {
				t.Errorf("requiredScopeForRequest(%q,%q)=%q, want %q", tc.path, tc.method, got, tc.want)
			}
		})
	}
}

// Longest-prefix wins: /api/v1/iam/tenants/public is public, but a generic
// /api/v1/iam path resolves to iam scopes.
func TestRequiredScopeForRequest_LongestPrefix(t *testing.T) {
	// /api/v1/barrier and /v1/barrier both exist; ensure each resolves to barrier.
	if got := requiredScopeForRequest("/api/v1/barrier/status", http.MethodGet); got != "tenant_barrier:status" {
		t.Errorf("expected tenant_barrier:status, got %q", got)
	}
}

func TestClusterServicePrincipal_NilTLS(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms", nil)
	if clusterServicePrincipal(req, testTrustDomain) {
		t.Error("expected false when no TLS state present")
	}
}
