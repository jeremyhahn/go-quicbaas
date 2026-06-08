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
	"net/http"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"

	qrdbspiffe "github.com/jeremyhahn/go-qrdb/pkg/auth/spiffe"
)

// EnforceDefaultDeny is the default-deny scope -> RBAC authorization gate for the
// REST surface. Unlike EnforceTokenBinding (which lets token-less requests pass
// through), this middleware DENIES any request that is not explicitly permitted:
//
//  1. Public paths (health probes, metrics, version, OIDC discovery/jwks/oauth2,
//     login/ceremony/register, federation login/callback, invite activation, ACME
//     directory, UI, docs) are allowed without a token. See isDefaultDenyPublic.
//
//  2. A verified mTLS service identity (a SPIFFE "svc" principal whose trust
//     domain matches the operator-pinned domain) is allowed. This is the cluster
//     control/data plane: admin PIV bearer and OAuth2 client_credentials service
//     accounts present a verified client certificate, not necessarily a platform
//     token, so they are recognized by their verified mTLS identity rather than
//     being 403'd by the standalone RBAC role manager. See clusterServicePrincipal.
//
//  3. Every other request MUST present a valid platform token (401 if absent or
//     invalid). The token is validated through the same m.validate seam the other
//     gates use, so DPoP cnf-binding (RFC 9449) and mTLS cnf binding (RFC 8705)
//     are enforced fail-closed for sender-constrained tokens. The required scope
//     for the request path is then enforced from the declarative defaultDenyScope
//     table (403 on missing scope). The verified principal + AuthorizedContext are
//     injected for downstream handlers.
//
// It is installed on the TOP router (not the /api/v1 chi group) as a
// path-predicate so it also covers root-mounted protected surfaces — IAM
// management (/api/v1/tenants, /api/v1/iam/*) and the qrdb-wire mounts
// (/v1/credentials|barrier|ceremony|init) — which a gate scoped to the /api/v1
// group would miss.
//
// pinnedTrustDomain is the operator-configured SPIFFE trust domain used to verify
// the cluster service principal's certificate (fail-closed on mismatch). When
// empty, mTLS-svc short-circuiting is disabled and such callers fall through to
// the token requirement.
func (m *AuthzMiddleware) EnforceDefaultDeny(pinnedTrustDomain string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path

			// (a) Public path -> allow without authentication.
			if isDefaultDenyPublic(path) {
				next.ServeHTTP(w, r)
				return
			}

			// (b) Verified cluster mTLS service principal -> allow. This is the
			// control/data plane: a "svc" SPIFFE identity on a verified client
			// certificate chaining to the pinned trust domain.
			if clusterServicePrincipal(r, pinnedTrustDomain) {
				next.ServeHTTP(w, r)
				return
			}

			// (c) Otherwise a valid platform token is required.
			token, err := extractBearerToken(r)
			if err != nil {
				m.logUnauthorized(r, "default-deny: no bearer token presented")
				respondUnauthorized(w, r, "authentication required")
				return
			}

			ctx := r.Context()
			subject, authzCtx, err := m.validate(ctx, r, token)
			if err != nil {
				m.logUnauthorized(r, "default-deny: token validation failed: "+err.Error())
				respondUnauthorized(w, r, "invalid_token")
				return
			}

			// Enforce the required scope for this path. When the path maps to a
			// known protected surface the token must carry the corresponding
			// "resource:action" scope; missing scope is a 403. A path with no scope
			// mapping requires a valid token only (authentication, not a specific
			// capability) — the path is already non-public and non-cluster.
			if required := requiredScopeForRequest(path, r.Method); required != "" {
				if authzCtx == nil {
					// The validator is not DPoP/platform-aware (e.g. a non-iam
					// build): no scope set is available to authorize against, so a
					// scoped surface fails closed.
					m.logUnauthorized(r, "default-deny: scope required but no platform context")
					respondForbidden(w, r, "insufficient_scope")
					return
				}
				if scopeErr := authzCtx.RequireScope(required); scopeErr != nil {
					m.logUnauthorized(r, "default-deny: missing scope "+required)
					respondForbidden(w, r, "insufficient_scope: "+required)
					return
				}
			}

			ctx = injectPrincipal(ctx, subject, authzCtx)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// clusterServicePrincipal reports whether the request presents a verified mTLS
// client certificate carrying a tenant-scoped SPIFFE "svc" identity in the
// operator-pinned trust domain. It uses r.TLS.VerifiedChains (the chains the TLS
// stack actually verified against the configured CA pool), NOT raw
// PeerCertificates, so an unverified or self-presented certificate never grants
// cluster access. It is fail-closed: any missing handshake state, empty pinned
// domain, trust-domain mismatch, or non-service kind returns false.
func clusterServicePrincipal(r *http.Request, pinnedTrustDomain string) bool {
	if pinnedTrustDomain == "" {
		return false
	}
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
		return false
	}
	chain := r.TLS.VerifiedChains[0]
	if len(chain) == 0 {
		return false
	}
	leaf := chain[0]

	id, err := qrdbspiffe.TenantFromCert(leaf, pinnedTrustDomain)
	if err != nil {
		return false
	}
	return id.Kind == qrdbspiffe.KindService
}

// defaultDenyPublicPrefixes are the path prefixes that bypass authorization under
// the default-deny gate. A request path matches when it equals a prefix or is a
// sub-path of it (prefix + "/..."). These are the endpoints that must remain
// reachable before a caller holds a token: liveness/readiness probes, metrics,
// build info, the OIDC/OAuth2 discovery and token surface, the login/registration
// ceremonies that mint tokens, federated login redirects + callbacks, invite
// activation, the RFC 8555 ACME directory, and the human-facing UI/docs.
var defaultDenyPublicPrefixes = []string{
	// Health probes. NOTE: the real routes are /health/live and /health/ready
	// (server.go), not the /health/liveness + /health/readiness names the legacy
	// publicEndpoints list used — those never matched the mounted probes.
	"/api/v1/health",
	"/api/v1/health/live",
	"/api/v1/health/ready",

	// Metrics (both the root scrape mount and the /api/v1 group).
	"/metrics",
	"/api/v1/metrics",

	// Build info (consumed by the GUI dashboard).
	"/api/v1/version",

	// OIDC / OAuth2 discovery + token surface (root-mounted).
	"/.well-known/openid-configuration",
	"/.well-known/oauth-authorization-server",
	"/.well-known/jwks.json",
	"/oauth2",

	// Token lifecycle + hardware-attested / ceremony login + self-registration.
	// The ceremonies authenticate the caller; no bearer exists yet.
	"/api/v1/auth/refresh",
	// Token introspection (validate) and revocation (logout) authenticate via
	// the token carried IN the request itself (body/header), exactly like
	// /oauth2/introspect and /oauth2/revoke. They must remain reachable without
	// the gate pre-validating a bearer so a caller can check or revoke an
	// arbitrary (possibly already-invalid) token. The handlers fail closed on a
	// missing/invalid token themselves.
	"/api/v1/auth/validate",
	"/api/v1/auth/logout",
	"/api/v1/auth/webauthn",
	"/api/v1/auth/login/piv",
	"/api/v1/auth/login/begin",
	"/api/v1/auth/login/finish",
	"/api/v1/auth/register/begin",
	"/api/v1/auth/register/finish",

	// Federated login redirect + IdP callback (no bearer available yet).
	"/api/v1/auth/providers",

	// Public tenant listing for the login UI tenant selector.
	"/api/v1/iam/tenants/public",

	// Invite activation (FIDO2 / PIV) — the activation IS the bootstrap.
	"/api/v1/iam/invites",

	// Agent enrollment bootstrap — the daemon authenticates via its mTLS
	// client certificate on the handshake; the handler mints credentials.
	"/api/v1/agents/register",

	// RFC 8555 ACME directory (root-mounted per the ACME URL convention).
	"/acme/directory",

	// Human-facing UI + API docs.
	"/ui",
	"/docs",
}

// isDefaultDenyPublic reports whether path is on the default-deny allowlist.
func isDefaultDenyPublic(path string) bool {
	for _, prefix := range defaultDenyPublicPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// defaultDenyScopeRule binds a path prefix to the scope required for read versus
// mutating access. The scope grammar is the qrdb-canonical colon
// "resource:action" (e.g. "keys:get", "tenant_barrier:init") so it matches the
// scp claim the issuer mints from a subject's qrdb-resolved permissions and the
// (resource, action) pairs the REST PEP (authz_pep_mapping.go) passes to qrdb's
// PDP. A read verb maps to the resource's canonical read action and a mutating
// verb (POST/PUT/PATCH/DELETE) to its canonical write action.
type defaultDenyScopeRule struct {
	read  string
	write string
}

// defaultDenyScopeTable maps protected REST path prefixes to their required
// qrdb-canonical "resource:action" scopes. Longest-prefix wins (see
// requiredScopeForRequest) so nested surfaces resolve to the most specific
// rule. Public + cluster-principal requests short-circuit before this table is
// consulted.
//
// A path absent from the table requires a valid token but no specific scope:
// authentication without a declared capability gate. The table covers the
// security-sensitive surfaces (KMS / Secrets / Crypto / Certificate / DEK / KV /
// IAM-write / barrier / credential / ceremony) called out in the design.
//
// Resource names are qrdb-canonical and agree with authz_pep_mapping.go: kms /
// crypto / dek -> keys; the KV data plane -> kv; secrets/credentials ->
// credentials; certificates/trust/acme -> certs; barrier/seal ->
// tenant_barrier; IAM tenant admin -> tenants. The single vocabulary means the
// scp the issuer mints (qrdb perm Names) is matched verbatim here.
var defaultDenyScopeTable = map[string]defaultDenyScopeRule{
	// Crypto / key-management plane (qrdb resource "keys").
	"/api/v1/kms":     {read: "keys:get", write: "keys:create"},
	"/api/v1/crypto":  {read: "keys:get", write: "keys:create"},
	"/api/v1/dek":     {read: "keys:get", write: "keys:create"},
	"/api/v1/secrets": {read: "credentials:retrieve", write: "credentials:store"},
	"/api/v1/seal":    {read: "tenant_barrier:status", write: "tenant_barrier:seal"},

	// Certificate / trust / ACME control plane (qrdb resource "certs").
	"/api/v1/certificates": {read: "certs:read", write: "certs:generate"},
	"/api/v1/trust":        {read: "certs:read", write: "certs:generate"},
	"/api/v1/acme":         {read: "certs:read", write: "certs:generate"},

	// Data plane (KV-family surfaces; qrdb resource "kv"). /api/v1/keys is the
	// KV data plane (NOT cryptographic keys) per the PEP correspondence.
	"/api/v1/keys":         {read: "kv:get", write: "kv:put"},
	"/api/v1/entities":     {read: "kv:get", write: "kv:put"},
	"/api/v1/kv":           {read: "kv:get", write: "kv:put"},
	"/api/v1/query":        {read: "kv:get", write: "kv:get"},
	"/api/v1/entity-types": {read: "kv:get", write: "kv:get"},
	"/api/v1/transaction":  {read: "transaction:execute", write: "transaction:execute"},
	"/api/v1/batch":        {read: "kv:put", write: "kv:put"},
	"/api/v1/timeseries":   {read: "timeseries:query", write: "timeseries:write"},

	// IAM management surface (tenant/user/role/policy admin; qrdb resource "tenants").
	"/api/v1/tenants": {read: "tenants:read", write: "tenants:create"},
	"/api/v1/iam":     {read: "tenants:read", write: "tenants:create"},

	// qrdb-wire mounts at the router root.
	"/v1/credentials": {read: "credentials:retrieve", write: "credentials:store"},
	"/v1/barrier":     {read: "tenant_barrier:status", write: "tenant_barrier:init"},
	"/api/v1/barrier": {read: "tenant_barrier:status", write: "tenant_barrier:init"},
	"/v1/ceremony":    {read: "ceremony:status", write: "ceremony:initialize"},
	"/v1/init":        {read: "ceremony:status", write: "ceremony:initialize"},
	"/api/v1/init":    {read: "ceremony:status", write: "ceremony:initialize"},
}

// requiredScopeForRequest returns the qrdb-canonical "resource:action" scope
// required for the given path + HTTP method, or "" when the path is not in the
// scope table (a valid token is required but no specific capability). It
// performs longest-prefix matching so the most specific rule applies to nested
// paths.
func requiredScopeForRequest(path, method string) string {
	path = strings.TrimSuffix(path, "/")

	bestLen := -1
	var best defaultDenyScopeRule
	for prefix, rule := range defaultDenyScopeTable {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			if len(prefix) > bestLen {
				bestLen = len(prefix)
				best = rule
			}
		}
	}
	if bestLen < 0 {
		return ""
	}

	switch mapHTTPMethodToAction(method) {
	case interfaces.ActionWrite, interfaces.ActionDelete:
		return best.write
	default:
		return best.read
	}
}
