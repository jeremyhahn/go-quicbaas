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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
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

			// Sealed-window authentication bypass: while the barrier is
			// sealed the sealed-window allowlist IS the entire control
			// surface (a sealed IAM plane cannot mint or validate tokens),
			// so the allowlisted unseal-flow paths that passed the sealed
			// gate above are admitted without a token (audited, reason
			// "sealed-window allowlist"). Unsealed, this is always false
			// and the barrier verbs keep their granular scopes below.
			if m.sealedBypass.BypassesRESTPath(path) {
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
					// The validator is not DPoP/platform-aware: no scope set is
					// available to authorize against, so a scoped surface fails
					// closed.
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

	id, err := qrdbsdk.TenantFromCert(leaf, pinnedTrustDomain)
	if err != nil {
		return false
	}
	return id.Kind == qrdbsdk.SpiffeKindService
}

// defaultDenyPublicPrefixes are the path prefixes that bypass authorization under
// the default-deny gate. A request path matches when it equals a prefix or is a
// sub-path of it (prefix + "/..."). These are the endpoints that must remain
// reachable before a caller holds a token: liveness/readiness probes, metrics,
// build info, the OIDC/OAuth2 discovery and token surface, the login/registration
// ceremonies that mint tokens, federated login redirects + callbacks, invite
// activation, the RFC 8555 ACME directory, and the human-facing UI/docs.
var defaultDenyPublicPrefixes = []string{
	// Health probes. The real routes are /api/v1/health, /api/v1/health/live and
	// /api/v1/health/ready (server.go). publicEndpoints (authz_middleware.go)
	// now names the same three: its dead /health/liveness + /health/readiness
	// entries, which never matched any mounted probe, were corrected.
	"/api/v1/health",
	"/api/v1/health/live",
	"/api/v1/health/ready",

	// Metrics (both the root scrape mount and the /api/v1 group).
	"/metrics",
	"/api/v1/metrics",

	// Build info (consumed by the GUI dashboard).
	"/api/v1/version",

	// OIDC / OAuth2 discovery + token surface (root-mounted). The endpoints are
	// named individually rather than by a blanket "/oauth2" prefix: that prefix
	// also covered the Dynamic Client Registration surface, making POST
	// /oauth2/register anonymously reachable. Each entry below either needs no
	// credential (discovery) or authenticates with the credential carried IN the
	// request (client_secret / access token / refresh token). The RFC 7592
	// client-configuration paths are public via preAuthRouteTemplates instead,
	// which cannot widen to the sibling create route.
	"/.well-known/openid-configuration",
	"/.well-known/oauth-authorization-server",
	"/.well-known/jwks.json",
	"/oauth2/authorize",
	"/oauth2/token",
	"/oauth2/token-mtls",
	"/oauth2/userinfo",
	"/oauth2/revoke",
	"/oauth2/introspect",

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
	// The WebAuthn ceremonies are mounted per tenant, not under /api/v1/auth;
	// preAuthRouteTemplates admits their two login legs. The former
	// "/api/v1/auth/webauthn" prefix matched no route, so it is deleted here too
	// — the two allowlists must name the same real routes
	// (TestAuthzTablesAgreeOnPublicSurface).
	"/api/v1/auth/login/piv",
	"/api/v1/auth/login/begin",
	"/api/v1/auth/login/finish",
	"/api/v1/auth/register/begin",
	"/api/v1/auth/register/finish",

	// Federated login redirect + IdP callback (no bearer available yet).
	"/api/v1/auth/providers",

	// Invite activation (FIDO2 / PIV) is NOT listed here. Its paths carry the
	// tenant as a segment ("/api/v1/iam/{tenant}/invites/{token}/..."), which a
	// literal prefix cannot express, so it is matched by the pre-auth route
	// TEMPLATES via isPreAuthRoute above. Adding a literal "/api/v1/iam" prefix
	// here instead would open every IAM route pre-auth.

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
// It consults the literal defaultDenyPublicPrefixes above plus the tenant-scoped
// pre-auth route templates (authz_public_routes.go), whose variable tenant
// segment a literal prefix cannot express. isAuthzPublicEndpoint consults the
// same templates, so the default-deny gate and the per-group gates agree on what
// is reachable pre-auth.
func isDefaultDenyPublic(path string) bool {
	if isPreAuthRoute(path) {
		return true
	}
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
// crypto -> keys (the DEK routes are mounted UNDER /api/v1/kms/deks and are
// covered by the kms prefix; a standalone "/api/v1/dek" prefix was never
// mounted and has been removed); the KV data plane -> kv; secrets/credentials ->
// credentials; certificates/trust/acme -> certs; barrier/seal ->
// tenant_barrier; IAM tenant admin -> tenants. The single vocabulary means the
// scp the issuer mints (qrdb perm Names) is matched verbatim here.
//
// The barrier and legacy seal prefixes are NOT in this table. Instead,
// requiredScopeForRequest consults barrierScopeForRequest first, which
// resolves the 13 registered operations to their granular scopes and falls
// back to the coarse scope entries below only for truly unrecognised sub-paths.
var defaultDenyScopeTable = map[string]defaultDenyScopeRule{
	// Crypto / key-management plane (qrdb resource "keys").
	"/api/v1/kms":     {read: "keys:get", write: "keys:create"},
	"/api/v1/crypto":  {read: "keys:get", write: "keys:create"},
	"/api/v1/secrets": {read: "credentials:retrieve", write: "credentials:store"},

	// Secret side-surfaces mounted as SIBLINGS of /api/v1/secrets (server.go).
	// Prefix matching requires prefix+"/", so "/api/v1/secrets" does NOT cover
	// "/api/v1/secrets-versions" and each needs its own rule — the same reason
	// they each need their own pepMapping entry.
	"/api/v1/secrets-versions": {read: "credentials:list_versions", write: "credentials:store"},
	"/api/v1/secrets-prefix":   {read: "credentials:retrieve", write: "credentials:store"},
	"/api/v1/secrets-watch":    {read: "credentials:retrieve", write: "credentials:store"},

	// Certificate control plane (qrdb resource "certs").
	"/api/v1/certificates": {read: "certs:read", write: "certs:generate"},

	// Trust composition + ACME control plane. qrdb registers dedicated broker
	// families for both (broker_scope.go TrustRead/TrustWrite, ACMERead/
	// ACMEWrite), so these agree with the pepMapping entries for the same
	// prefixes rather than borrowing the coarser certs:* verbs.
	"/api/v1/trust": {read: "trust:read", write: "trust:write"},
	"/api/v1/acme":  {read: "acme:read", write: "acme:write"},

	// go-xkms compatibility surface. Coarse only: the validator posture resolves
	// every endpoint granularly via xkmsOps (authz_pep_mapping.go), and this
	// coarse rule is never weaker than the granular one.
	"/api/v1/xkms": {read: "keys:get", write: "keys:create"},

	// Data plane (KV-family surfaces; qrdb resource "kv"). /api/v1/keys is the
	// KV data plane (NOT cryptographic keys) per the PEP correspondence.
	"/api/v1/keys":         {read: "kv:get", write: "kv:put"},
	"/api/v1/entities":     {read: "kv:get", write: "kv:put"},
	"/api/v1/kv":           {read: "kv:get", write: "kv:put"},
	"/api/v1/query":        {read: "kv:range", write: "kv:range"},
	"/api/v1/entity-types": {read: "kv:get", write: "kv:get"},
	"/api/v1/transaction":  {read: "transaction:execute", write: "transaction:execute"},
	"/api/v1/batch":        {read: "batch:execute", write: "batch:execute"},
	"/api/v1/timeseries":   {read: "timeseries:query", write: "timeseries:write"},

	// IAM management surface (tenant/user/role/policy admin; qrdb resource "tenants").
	// The canonical WebAuthn enrollment routes under /api/v1/iam/{tenant}/webauthn
	// fall under the coarse "/api/v1/iam" rule here (a literal prefix cannot
	// express the tenant segment), so this posture demands the stronger
	// tenants:create for them; the validator posture resolves them granularly to
	// user_authfactors:add via the PEP (authz_pep_mapping.go). Both are
	// fail-closed — the coarse rule is never weaker than the granular one.
	"/api/v1/tenants": {read: "tenants:read", write: "tenants:create"},
	"/api/v1/iam":     {read: "tenants:read", write: "tenants:create"},

	// Shard-routing introspection (env-gated debug mount) — cluster status.
	"/api/v1/debug": {read: "admin:status", write: "admin:status"},

	// Delegated-admin surface (root-mounted /api/v1/admin): admin-assignment
	// CRUD, permission checks and impersonation. Mirrors the qrdb roles:* family
	// the PEP mapping uses for the same prefix.
	"/api/v1/admin": {read: "roles:read", write: "roles:assign"},

	// RFC 7591 Dynamic Client Registration (root-mounted /oauth2/register).
	// Creating an OAuth2 client is a tenant-administration write, so it demands
	// the same tenants:create the PEP mapping assigns it. The RFC 7592
	// management verbs under this prefix never reach the table: they are
	// pre-auth (preAuthRouteTemplates) and gated by their registration access
	// token instead.
	"/oauth2/register": {read: "tenants:read", write: "tenants:create"},

	// qrdb-wire mounts at the router root.
	"/v1/credentials": {read: "credentials:retrieve", write: "credentials:store"},
	"/v1/ceremony":    {read: "ceremony:status", write: "ceremony:initialize"},
	"/v1/init":        {read: "ceremony:status", write: "ceremony:initialize"},
	"/api/v1/init":    {read: "ceremony:status", write: "ceremony:initialize"},

	// qrdb-wire user lookup + user-login factor mounts at the router root
	// (server.go /users + /v1/users/factors). Resource names are the
	// qrdb-registered users:* and user_authfactors:* permission families
	// (go-qrdb/pkg/permission/constants.go).
	"/users":            {read: "users:read", write: "users:create"},
	"/v1/users/factors": {read: "user_authfactors:get", write: "user_authfactors:add"},
}

// barrierScopeTable maps each barrier prefix to a coarse fallback scope rule
// used when no granular op in barrierGranularOps matches the path+method.
// The coarse entries cover the barrier surface for unanticipated future
// sub-paths so they degrade to authenticated+scoped rather than unmapped.
var barrierScopeTable = map[string]defaultDenyScopeRule{
	// API mount — canonical broker barrier surface.
	"/api/v1/barrier": {read: "tenant_barrier:status", write: "tenant_barrier:init"},
	// Root-wire mount — qrdb SDK compatibility.
	"/v1/barrier": {read: "tenant_barrier:status", write: "tenant_barrier:init"},
	// Legacy seal surface (server.go). The shared legacySealOps table
	// (authz_pep_mapping.go) covers the registered sub-paths; the fallback here
	// catches truly-unknown sub-paths and mirrors the coarse "/api/v1/seal"
	// pepMapping entry.
	legacySealPrefix: {read: "tenant_barrier:status", write: "tenant_barrier:seal"},
}

// barrierScopeForRequest returns the granular qrdb "resource:action" scope for
// requests targeting either the /api/v1/barrier or /v1/barrier (canonical) or
// /api/v1/seal (legacy) surfaces. It consults barrierGranularOps (for the two
// canonical mounts) and legacySealOps (for the legacy mount) using the same
// tier-2 HasSuffix matching as matchSpecialRule, then falls back to the coarse
// barrierScopeTable entry when no granular op matches. Returns "" when the
// path does not belong to any barrier-family prefix.
func barrierScopeForRequest(path, method string) string {
	// Identify which barrier-family prefix owns this path.
	var matchedPrefix string
	for prefix := range barrierScopeTable {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			if len(prefix) > len(matchedPrefix) {
				matchedPrefix = prefix
			}
		}
	}
	if matchedPrefix == "" {
		return ""
	}

	// Compute the path suffix relative to the matched prefix so suffix
	// matching works identically for /api/v1/barrier and /v1/barrier.
	rel := path[len(matchedPrefix):]

	// Legacy seal surface — consult the shared legacySealOps table owned by the
	// PEP mapping (authz_pep_mapping.go) so this gate and the validator gate
	// resolve the legacy seal verbs to the SAME scope by construction.
	if matchedPrefix == legacySealPrefix {
		for _, op := range legacySealOps {
			if !strings.EqualFold(op.method, method) {
				continue
			}
			if strings.HasSuffix(rel, op.suffix) {
				return op.resource + ":" + op.action
			}
		}
		// Coarse fallback for unregistered legacy seal sub-paths.
		coarse := barrierScopeTable[matchedPrefix]
		switch mapHTTPMethodToAction(method) {
		case interfaces.ActionWrite, interfaces.ActionDelete:
			return coarse.write
		default:
			return coarse.read
		}
	}

	// Canonical barrier mounts — consult barrierGranularOps.
	// Use the same longest-suffix-wins logic as matchSpecialRule tier 2.
	bestLen := -1
	var bestScope string
	for _, op := range barrierGranularOps {
		if !strings.EqualFold(op.method, method) {
			continue
		}
		if !strings.HasSuffix(rel, op.suffix) {
			continue
		}
		if len(op.suffix) > bestLen {
			bestLen = len(op.suffix)
			bestScope = op.scope
		}
	}
	if bestLen >= 0 {
		return bestScope
	}

	// Coarse fallback for unregistered barrier sub-paths.
	coarse := barrierScopeTable[matchedPrefix]
	switch mapHTTPMethodToAction(method) {
	case interfaces.ActionWrite, interfaces.ActionDelete:
		return coarse.write
	default:
		return coarse.read
	}
}

// requiredScopeForRequest returns the qrdb-canonical "resource:action" scope
// required for the given path + HTTP method, or "" when the path is not in the
// scope table (a valid token is required but no specific capability). It
// resolves the barrier-family surfaces (both canonical mounts and the legacy
// /api/v1/seal surface) to per-operation granular scopes via
// barrierScopeForRequest before consulting the coarse defaultDenyScopeTable,
// ensuring that rekey, recovery, root-token and Shamir init operations are
// gated with their canonical registered permission rather than the coarse
// tenant_barrier:init fallback. Longest-prefix matching applies to all other
// surfaces.
func requiredScopeForRequest(path, method string) string {
	path = strings.TrimSuffix(path, "/")

	// Barrier-family surfaces are resolved granularly first.
	if scope := barrierScopeForRequest(path, method); scope != "" {
		return scope
	}

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
