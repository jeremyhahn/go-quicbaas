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
)

// RouteAuth classifies how a route is authenticated and authorized on the
// unified API server (go-qrdb's multi-protocol server hosting this surface as
// a ServiceModule, pkg/apimodule).
type RouteAuth int

const (
	// RouteAuthPublic serves without authentication (health, version,
	// pre-auth login surfaces).
	RouteAuthPublic RouteAuth = iota
	// RouteAuthSession requires an authenticated identity but carries no
	// mapped RBAC permission; authorization is deferred to the handler.
	RouteAuthSession
	// RouteAuthPermission requires an authenticated identity holding the
	// route's Resource:Action permission.
	RouteAuthPermission
)

// RouteGroup names the middleware profile a route is served under. The
// profiles mirror the historical registration groups in setupRouter: the two
// /api/v1 body-limit groups and the SURVIVING qrdb-wire root mounts.
//
// /v1/credentials (GroupCredentialWire), /users (GroupUsersWire), and
// /v1/users/factors (GroupUserFactorsWire) were REMOVED once go-qrdb's
// unified REST server started serving them natively on the same listener —
// a quicbaas-side re-serving of those exact paths would only shadow or race
// qrdb's own handlers, and none of the three is reachable pre-unseal (see
// pkg/api/sealedwindow's package doc: credentials are a documented sealed-
// window exclusion, and users/factors need the unsealed user store).
//
// GroupBarrierWire, GroupCeremonyWire, and GroupInitWire, by contrast, MUST
// stay: they back the standalone sealed-window REST listener
// (pkg/server/sealed_window_rest.go), which binds its own real TCP listener
// and serves this table's chi router directly — BEFORE go-qrdb's unified
// server exists, since the unified server is itself gated by the same
// barrier this listener exists to unseal. pkg/api/sealedwindow's REST
// allowlist deliberately admits both the /v1/barrier|ceremony/* wire paths
// AND their /api/v1/barrier|init/* mirrors, so both prefixes must remain
// routable.
type RouteGroup int

const (
	// GroupAPI routes live under /api/v1 with the standard request body
	// limit and the shared protected-API authorization chain.
	GroupAPI RouteGroup = iota
	// GroupAPIUpload routes live under /api/v1 with the upload body limit
	// (object storage, storage admin, object transfer, backups, tenant
	// storage quotas).
	GroupAPIUpload
	// GroupBarrierWire routes are the qrdb-wire /v1/barrier root mount,
	// load-bearing for the standalone sealed-window REST listener.
	GroupBarrierWire
	// GroupCeremonyWire routes are the qrdb-wire /v1/ceremony root mount,
	// load-bearing for the standalone sealed-window REST listener.
	GroupCeremonyWire
	// GroupInitWire routes are the qrdb-wire /v1/init root mount,
	// load-bearing for the standalone sealed-window REST listener.
	GroupInitWire
)

// Module-only route groups (below) are consumed EXCLUSIVELY by the unified
// API server's per-route registration (pkg/apimodule RegisterHTTP via
// Routes()); registerRoutes (this file) never iterates them, so the legacy
// in-process listener's dedicated registration owners for these surfaces —
// registerIAMRoutes (iam_register.go), registerACMEDirectoryRoutes
// (handlers_acme.go), the docs/UI mounts and registerDebug (server.go /
// debug.go) — are completely unaffected by their presence in this table.
// Their Group tags exist only so TestRoutes_SurfaceGuard can validate each
// pattern sits under its real mount prefix.
const (
	// GroupIAM routes are the root-mounted, permission-gated IAM management
	// surface: tenant/user/group/role/policy/provider admin, per-tenant
	// barrier lifecycle, GDPR, invite activation, delegated-admin, and the
	// xkms-native WebAuthn ceremony mount. registerIAMRoutes wraps every one
	// of these chi.Route blocks in useAPIAuthzMiddleware, so their
	// Auth/Resource/Action derive from the same PEP mapper
	// (authz_pep_mapping.go) that already names these exact prefixes.
	GroupIAM RouteGroup = iota + 100
	// GroupOAuth2Register routes are the RFC 7591/7592 dynamic client
	// registration surface: POST (create) is permission-gated
	// (tenants:create); the three per-client management verbs are pre-auth
	// via their registration access token (preAuthRouteTemplates). Both
	// derive correctly through classifyRoutes, so this group is tagged
	// separately from GroupIAM only because its mount ("/oauth2/register")
	// is not under /api/v1.
	GroupOAuth2Register
	// GroupOIDCDiscovery routes are the OIDC/OAuth2 discovery documents
	// (.well-known). registerIAMRoutes mounts them at the router root with
	// NO authorization wrapper — they are public by protocol, same as every
	// OIDC issuer's discovery surface — so their Auth is set explicitly
	// (RouteAuthPublic) rather than derived from the PEP mapper, which has
	// no entry for a root-mounted, unwrapped surface.
	GroupOIDCDiscovery
	// GroupOAuth2Core routes are the core OAuth2 token endpoints
	// (authorize/token/token-mtls/userinfo/revoke/introspect).
	// registerIAMRoutes mounts them unwrapped, same as GroupOIDCDiscovery:
	// each endpoint authenticates from credentials carried IN the request
	// (client_secret, the token itself, the mTLS handshake), not a
	// pre-validated platform bearer, so Auth is set explicitly to Public.
	GroupOAuth2Core
	// GroupAPIAuthPublic routes are the /api/v1/auth token-lifecycle +
	// ceremony + self-registration verbs, the federated login/callback
	// redirect, and the mTLS agent-registration bootstrap.
	// registerIAMRoutes/RegisterFederationAuthRoutes/
	// registerFleetEnrollmentPublicRoutes mount all of them unwrapped (no
	// useAPIAuthzMiddleware): each is either a first-contact ceremony (no
	// token exists yet) or authenticates via the credential carried in the
	// request itself, so Auth is set explicitly to Public.
	GroupAPIAuthPublic
	// GroupACMEDirectory routes are the raw RFC 8555 ACME directory, mounted
	// at the router root (registerACMEDirectoryRoutes) so the advertised
	// directory URL follows the RFC 8555 convention. Every endpoint
	// authenticates via JWS + nonce + account key per the ACME protocol
	// itself, carries no platform-token gate on the legacy listener, and is
	// therefore Public here too.
	GroupACMEDirectory
	// GroupDocs routes serve the embedded API documentation (Swagger UI +
	// OpenAPI spec), mounted at the router root with no authorization
	// wrapper.
	GroupDocs
	// GroupUIMount routes serve the embedded Svelte web UI SPA (ui.RegisterUI),
	// mounted at the router root with no authorization wrapper: the login
	// page must render before the caller holds a token.
	GroupUIMount
	// GroupMetricsScrape is the Prometheus scrape endpoint mounted inside
	// the /api/v1 chi group (server.go). Unlike the root-mounted /metrics
	// alias (which IS public), this mount inherits the standard /api/v1
	// authorization chain on the legacy listener, and no PEP entry maps
	// "/api/v1/metrics" specifically, so it derives to the same
	// identity-gated (RouteAuthSession) posture as its
	// /api/v1/metrics/performance sibling — carried over faithfully via
	// classifyRoutes rather than hand-set.
	GroupMetricsScrape
	// GroupDebugModule is the env-gated shard-routing introspection mount
	// (debug.go). Registered on the module surface with the SAME env-var
	// gate the legacy listener checks (QUICBAAS_ENABLE_DEBUG_ROUTES) via its
	// Enabled guard, and the same admin:status permission the PEP mapper
	// already assigns "/api/v1/debug" — never exposed publicly.
	GroupDebugModule
)

// routeGroupMounts maps each qrdb-wire RouteGroup, and each module-only
// RouteGroup (routes_iam.go) whose mount is NOT under /api/v1, onto its root
// mount prefix. The two /api/v1 groups (GroupAPI, GroupAPIUpload) are
// handled structurally by registerRoutes and are deliberately absent.
var routeGroupMounts = map[RouteGroup]string{
	GroupBarrierWire:  "/v1/barrier",
	GroupCeremonyWire: "/v1/ceremony",
	GroupInitWire:     "/v1/init",

	// Module-only groups whose mount is NOT under /api/v1 (routes_iam.go).
	// registerRoutes never consults these entries (its wire-mount loop names
	// only the three groups above); they exist so TestRoutes_SurfaceGuard
	// can validate each module-only route sits under its real root mount.
	GroupOAuth2Register: "/oauth2/register",
	GroupOIDCDiscovery:  "/.well-known",
	GroupOAuth2Core:     "/oauth2",
	GroupACMEDirectory:  "/acme",
	GroupDocs:           "/docs",
	GroupUIMount:        "/ui",
}

// RouteSpec describes one REST route: the wire surface, its authorization
// contract, and how to bind its handler on a constructed *Server. The table
// returned by Routes is the single source of truth for the quicbaas REST
// surface owned by server.go's registration — it is consumed by the legacy
// in-process server (setupRouter -> registerRoutes) and by the unified-API-
// server ServiceModule (pkg/apimodule RegisterHTTP), so a route exists on
// both paths, with the same permission, by construction.
//
// The IAM/OIDC plane (registerIAMRoutes), the raw RFC 8555 ACME directory,
// the embedded UI, the docs mounts, and the integration-only debug surface
// remain dedicated mounts wired directly in setupRouter: they are
// self-contained sub-surfaces with their own registration owners on the
// legacy in-process listener. routes_iam.go contributes the SAME surfaces
// into this table under module-only RouteGroups (GroupIAM and friends,
// above) purely for the unified-API-server ServiceModule
// (pkg/apimodule RegisterHTTP): registerRoutes below never iterates those
// groups, so the legacy registration is untouched and unduplicated.
type RouteSpec struct {
	Method  string
	Pattern string // full path, including the /api/v1 or /v1 wire prefix
	Group   RouteGroup
	Auth    RouteAuth

	// Resource and Action carry the qrdb-canonical RBAC permission for
	// RouteAuthPermission routes. They are derived from the same REST PEP
	// mapping the legacy authorization middleware enforces at request time
	// (authz_pep_mapping.go), so the module-registered permission and the
	// legacy per-request decision can never disagree.
	Resource string
	Action   string

	// Enabled reports whether the route's backing subsystem is configured
	// on s; nil means always enabled. The legacy server skips disabled
	// routes; the module registers the full surface and answers 501 for
	// routes whose subsystem is absent at request time.
	Enabled func(s *Server) bool

	// Handler binds the route's handler on a constructed server.
	Handler func(s *Server) http.HandlerFunc
}

// frozenPermission is one routes_permissions_generated.go table entry: the
// authorization contract the REST PEP mapper computed for a route the LAST
// time the freeze was regenerated, before the mapper was deleted (see the
// package-level provenance comment in routes_permissions_generated.go).
type frozenPermission struct {
	auth     RouteAuth
	resource string
	action   string
}

// applyFrozenPermissions stamps every spec's Auth/Resource/Action from
// frozenRoutePermissions (routes_permissions_generated.go), the one-time
// codegen snapshot of the (now-deleted) REST PEP mapper's output. Routes()
// calls this instead of any live derivation: the unified-API-server's
// per-route registration and the local platform-token fallback
// (pkg/apimodule module_http.go RegisterHTTP) both consume the frozen
// Resource/Action directly.
//
// A route present in rawRouteSpecs but absent from the generated table means
// the freeze is stale (a route was added or renamed without extending the
// table); this fails LOUDLY (panic, caught immediately by every test that
// calls Routes()) rather than silently defaulting to identity-gated, which
// would be a fail-open drift on a security-relevant table.
//
// THE TABLE IS MAINTAINED BY HAND. scripts/genroutepermissions was deleted
// with the PEP mapper it derived from at the authz cutover; the instruction to
// run it outlived it, in the panic text below, and pointed at least one author
// at a command that no longer exists at the moment they were extending a
// security-relevant table (#299).
func applyFrozenPermissions(specs []RouteSpec) []RouteSpec {
	for i := range specs {
		key := specs[i].Method + " " + specs[i].Pattern
		fp, ok := frozenRoutePermissions[key]
		if !ok {
			panic("rest: route " + key + " has no frozen permission entry; " +
				"add one by hand to routes_permissions_generated.go following the " +
				"\"resource:action\" vocabulary of the neighbouring entries, then update " +
				"pkg/api/rest/testdata/route_permissions.golden and route_surface.golden — " +
				"or, if the route is behind a feature tag, their _<tag>.golden fragments " +
				"(e.g. route_permissions_rag.golden). There is no generator")
		}
		specs[i].Auth = fp.auth
		specs[i].Resource = fp.resource
		specs[i].Action = fp.action
	}
	return specs
}

// rawRouteSpecs returns every route whose Auth/Resource/Action is PEP-
// derived (frozen in routes_permissions_generated.go, see
// applyFrozenPermissions): the historical setupRouter surfaces, the cluster
// CA secret route, the rag build-tag variant, and the IAM-management/
// oauth2-register/metrics-scrape/debug-mount surfaces contributed by
// routes_iam.go. It excludes the explicitly-public root mounts (OIDC
// discovery, ACME directory, docs, UI, ...), which were never PEP-derived —
// Routes() appends those separately with their Auth already set.
func rawRouteSpecs() []RouteSpec {
	specs := []RouteSpec{

		// --- GroupAPI ---
		{Method: "POST", Pattern: "/api/v1/acme/finalize", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleACMEFinalizeOrder }},
		{Method: "POST", Pattern: "/api/v1/acme/issue", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleACMEIssue }},
		{Method: "POST", Pattern: "/api/v1/acme/new-order", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleACMENewOrder }},
		{Method: "POST", Pattern: "/api/v1/acme/register-challenge", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleACMERegisterChallenge }},
		{Method: "POST", Pattern: "/api/v1/acme/renew", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleACMERenew }},
		{Method: "GET", Pattern: "/api/v1/agent-accounts/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListAccounts }},
		{Method: "POST", Pattern: "/api/v1/agent-accounts/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCreateAccount }},
		{Method: "DELETE", Pattern: "/api/v1/agent-accounts/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteAccount }},
		{Method: "GET", Pattern: "/api/v1/agent-accounts/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetAccount }},
		{Method: "PUT", Pattern: "/api/v1/agent-accounts/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleUpdateAccount }},
		{Method: "POST", Pattern: "/api/v1/agent-accounts/{id}/default", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSetDefaultAccount }},
		{Method: "POST", Pattern: "/api/v1/agent-accounts/{id}/materialize", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleMaterializeAccountCredential }},
		{Method: "POST", Pattern: "/api/v1/agent-accounts/{id}/reservations", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleReserveAccountSession }},
		{Method: "DELETE", Pattern: "/api/v1/agent-accounts/{id}/reservations/{rid}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleReleaseAccountSession }},
		{Method: "POST", Pattern: "/api/v1/agent-accounts/{id}/revoke", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRevokeAccount }},
		{Method: "POST", Pattern: "/api/v1/agent-accounts/{id}/rotate", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRotateAccountCredential }},
		{Method: "GET", Pattern: "/api/v1/agent-accounts/{id}/usage", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetAccountUsage }},
		{Method: "POST", Pattern: "/api/v1/attestation/attest-container", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleAttestationAttestContainer }},
		{Method: "POST", Pattern: "/api/v1/attestation/verify", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleAttestationVerify }},
		{Method: "POST", Pattern: "/api/v1/attestation/verify-chain", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleAttestationVerifyChain }},
		{Method: "POST", Pattern: "/api/v1/attestation/appraise-quote", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleAttestationAppraiseQuote }},
		{Method: "GET", Pattern: "/api/v1/attestation/endorsements", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleAttestationListEndorsements }},
		{Method: "PUT", Pattern: "/api/v1/attestation/endorsements/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleAttestationPutEndorsement }},
		{Method: "DELETE", Pattern: "/api/v1/attestation/endorsements/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleAttestationDeleteEndorsement }},
		{Method: "GET", Pattern: "/api/v1/attestation/reference-values", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleAttestationListReferenceValues }},
		{Method: "GET", Pattern: "/api/v1/attestation/reference-values/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleAttestationGetReferenceValues }},
		{Method: "PUT", Pattern: "/api/v1/attestation/reference-values/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleAttestationPutReferenceValues }},
		{Method: "DELETE", Pattern: "/api/v1/attestation/reference-values/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleAttestationDeleteReferenceValues }},
		{Method: "POST", Pattern: "/api/v1/barrier/enable-encryption", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierEnableEncryption }},
		{Method: "POST", Pattern: "/api/v1/barrier/initialize", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierInitialize }},
		{Method: "DELETE", Pattern: "/api/v1/barrier/recovery", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierRecoveryDelete }},
		{Method: "POST", Pattern: "/api/v1/barrier/recovery/generate", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierRecoveryGenerate }},
		{Method: "POST", Pattern: "/api/v1/barrier/recovery/recover", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierRecoveryRecover }},
		{Method: "GET", Pattern: "/api/v1/barrier/recovery/status", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierRecoveryStatus }},
		{Method: "POST", Pattern: "/api/v1/barrier/rekey", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierRekey }},
		{Method: "POST", Pattern: "/api/v1/barrier/root-token", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierRootToken }},
		{Method: "POST", Pattern: "/api/v1/barrier/seal", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierSeal }},
		{Method: "POST", Pattern: "/api/v1/barrier/shamir/initialize", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierShamirInitialize }},
		{Method: "POST", Pattern: "/api/v1/barrier/shamir/unseal-share", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierShamirUnsealShare }},
		{Method: "POST", Pattern: "/api/v1/barrier/shamir/unseal-shares", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierShamirUnsealShares }},
		{Method: "GET", Pattern: "/api/v1/barrier/status", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierStatus }},
		{Method: "POST", Pattern: "/api/v1/barrier/unseal", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierUnseal }},
		{Method: "POST", Pattern: "/api/v1/batch/delete", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBatchDelete }},
		{Method: "POST", Pattern: "/api/v1/batch/execute", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBatchExecute }},
		{Method: "POST", Pattern: "/api/v1/batch/get", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBatchGet }},
		{Method: "POST", Pattern: "/api/v1/batch/put", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBatchPut }},
		{Method: "POST", Pattern: "/api/v1/bootstrap/auto", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBootstrapAuto }},
		{Method: "POST", Pattern: "/api/v1/bootstrap/dane", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBootstrapDane }},
		{Method: "POST", Pattern: "/api/v1/bootstrap/noise", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBootstrapNoise }},
		{Method: "POST", Pattern: "/api/v1/bootstrap/spki", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBootstrapSpki }},
		{Method: "POST", Pattern: "/api/v1/cache/clear", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleClearCache }},
		{Method: "GET", Pattern: "/api/v1/cache/config", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetCacheConfig }},
		{Method: "PUT", Pattern: "/api/v1/cache/config", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleUpdateCacheConfig }},
		{Method: "GET", Pattern: "/api/v1/cache/stats", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetCacheStats }},
		{Method: "GET", Pattern: "/api/v1/certificate/ca", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetCACertificate }},
		{Method: "GET", Pattern: "/api/v1/certificate/ca/bundle", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCABundle }},
		{Method: "GET", Pattern: "/api/v1/certificate/ca/info", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCAInfo }},
		{Method: "GET", Pattern: "/api/v1/certificate/ca/list", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListCertificates }},
		{Method: "POST", Pattern: "/api/v1/certificate/chain", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCertificateChain }},
		{Method: "POST", Pattern: "/api/v1/certificate/cross-sign", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCertificateCrossSign }},
		{Method: "POST", Pattern: "/api/v1/certificate/device-identity", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCertificateDeviceIdentity }},
		{Method: "POST", Pattern: "/api/v1/certificate/issue", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleIssueCertificate }},
		{Method: "POST", Pattern: "/api/v1/certificate/revoke", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRevokeCertificate }},
		{Method: "POST", Pattern: "/api/v1/certificate/sign-csr", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSignCSR }},
		{Method: "POST", Pattern: "/api/v1/certificate/tls-config", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetTLSConfig }},
		{Method: "POST", Pattern: "/api/v1/certificate/verify", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleVerifyCertificate }},
		{Method: "GET", Pattern: "/api/v1/cluster/leader", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetClusterLeader }},
		{Method: "GET", Pattern: "/api/v1/cluster/leader/{shardID}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetLeader }},
		{Method: "DELETE", Pattern: "/api/v1/cluster/migrate/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCancelMigration }},
		{Method: "POST", Pattern: "/api/v1/cluster/migrate/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleStartMigration }},
		{Method: "GET", Pattern: "/api/v1/cluster/migrate/status", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetMigrationStatus }},
		{Method: "GET", Pattern: "/api/v1/cluster/nodes/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListNodes }},
		{Method: "POST", Pattern: "/api/v1/cluster/nodes/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleAddNode }},
		{Method: "GET", Pattern: "/api/v1/cluster/nodes/non-leader", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListNonLeaderNodes }},
		{Method: "DELETE", Pattern: "/api/v1/cluster/nodes/{nodeId}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRemoveNode }},
		{Method: "PUT", Pattern: "/api/v1/cluster/nodes/{nodeId}/replace", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleReplaceNode }},
		{Method: "POST", Pattern: "/api/v1/cluster/rebalance/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleStartRebalance }},
		{Method: "GET", Pattern: "/api/v1/cluster/rebalance/plan", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetRebalancePlan }},
		{Method: "GET", Pattern: "/api/v1/cluster/rebalance/status", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetRebalanceStatus }},
		{Method: "GET", Pattern: "/api/v1/cluster/shards", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleClusterShards }},
		{Method: "GET", Pattern: "/api/v1/cluster/status", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleClusterStatus }},
		{Method: "POST", Pattern: "/api/v1/cluster/upgrade/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleStartUpgrade }},
		{Method: "GET", Pattern: "/api/v1/cluster/upgrade/status", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetUpgradeStatus }},
		{Method: "POST", Pattern: "/api/v1/crypto/datakey/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCryptoDataKey }},
		{Method: "POST", Pattern: "/api/v1/crypto/decrypt/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCryptoDecrypt }},
		{Method: "POST", Pattern: "/api/v1/crypto/decrypt/{name}/batch", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCryptoBatchDecrypt }},
		{Method: "POST", Pattern: "/api/v1/crypto/encrypt/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCryptoEncrypt }},
		{Method: "POST", Pattern: "/api/v1/crypto/encrypt/{name}/batch", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCryptoBatchEncrypt }},
		{Method: "POST", Pattern: "/api/v1/crypto/hash", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCryptoHash }},
		{Method: "POST", Pattern: "/api/v1/crypto/random", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCryptoRandom }},
		{Method: "POST", Pattern: "/api/v1/crypto/rewrap/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCryptoRewrap }},
		{Method: "POST", Pattern: "/api/v1/crypto/sign/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCryptoSign }},
		{Method: "POST", Pattern: "/api/v1/crypto/verify/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCryptoVerify }},
		{Method: "POST", Pattern: "/api/v1/embedding/batch", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleEmbeddingBatchGenerate }},
		{Method: "DELETE", Pattern: "/api/v1/embedding/cache", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleEmbeddingClearCache }},
		{Method: "GET", Pattern: "/api/v1/embedding/cache/stats", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleEmbeddingGetCacheStats }},
		{Method: "POST", Pattern: "/api/v1/embedding/generate", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleEmbeddingGenerate }},
		{Method: "GET", Pattern: "/api/v1/embedding/providers", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleEmbeddingListProviders }},
		{Method: "PUT", Pattern: "/api/v1/embedding/providers/default", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleEmbeddingSetDefaultProvider }},
		{Method: "PUT", Pattern: "/api/v1/embedding/providers/{name}/status", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleEmbeddingSetProviderStatus }},
		{Method: "GET", Pattern: "/api/v1/embedding/providers/{provider}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleEmbeddingGetProviderInfo }},
		{Method: "GET", Pattern: "/api/v1/entities/{type}/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleList }},
		{Method: "POST", Pattern: "/api/v1/entities/{type}/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandlePut }},
		{Method: "DELETE", Pattern: "/api/v1/entities/{type}/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDelete }},
		{Method: "GET", Pattern: "/api/v1/entities/{type}/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGet }},
		{Method: "PUT", Pattern: "/api/v1/entities/{type}/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandlePut }},
		{Method: "GET", Pattern: "/api/v1/entity-types", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListEntityTypes }},
		{Method: "GET", Pattern: "/api/v1/health", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleHealth }},
		{Method: "GET", Pattern: "/api/v1/health/live", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleLiveness }},
		{Method: "GET", Pattern: "/api/v1/health/ready", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleReadiness }},
		{Method: "POST", Pattern: "/api/v1/hooks/task-completed", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleHookTaskCompleted }},
		{Method: "POST", Pattern: "/api/v1/hooks/task-created", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleHookTaskCreated }},
		{Method: "POST", Pattern: "/api/v1/hooks/teammate-idle", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleHookTeammateIdle }},
		{Method: "POST", Pattern: "/api/v1/init/claim-cert/begin", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCeremonyClaimCertBegin }},
		{Method: "POST", Pattern: "/api/v1/init/claim-cert/complete", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCeremonyClaimCertComplete }},
		{Method: "POST", Pattern: "/api/v1/init/enroll-external-officer", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCeremonyEnrollExternalOfficer }},
		{Method: "POST", Pattern: "/api/v1/init/initialize", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCeremonyInitialize }},
		{Method: "POST", Pattern: "/api/v1/init/officer-share/begin", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCeremonyBeginClaimShare }},
		{Method: "POST", Pattern: "/api/v1/init/officer-share/complete", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCeremonyCompleteClaimShare }},
		{Method: "GET", Pattern: "/api/v1/init/officers", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCeremonyListOfficers }},
		{Method: "POST", Pattern: "/api/v1/init/sign-csr", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCeremonySignCSR }},
		{Method: "GET", Pattern: "/api/v1/init/status", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCeremonyStatus }},
		{Method: "GET", Pattern: "/api/v1/kms/algorithms", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetSupportedAlgorithms }},
		{Method: "GET", Pattern: "/api/v1/kms/backend/status", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBackendStatus }},
		{Method: "POST", Pattern: "/api/v1/kms/backend/test", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBackendTest }},
		{Method: "POST", Pattern: "/api/v1/kms/decrypt/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleKMSDecrypt }},
		{Method: "GET", Pattern: "/api/v1/kms/deks/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListDEKs }},
		{Method: "POST", Pattern: "/api/v1/kms/deks/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCreateDEK }},
		{Method: "DELETE", Pattern: "/api/v1/kms/deks/*", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.handleDEKDeleteAction }},
		{Method: "GET", Pattern: "/api/v1/kms/deks/*", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.handleDEKGetAction }},
		{Method: "POST", Pattern: "/api/v1/kms/deks/*", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.handleDEKPostAction }},
		{Method: "PUT", Pattern: "/api/v1/kms/deks/*", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.handleDEKPutAction }},
		{Method: "PUT", Pattern: "/api/v1/kms/deks/default", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSetDefaultDEKFromBody }},
		{Method: "POST", Pattern: "/api/v1/kms/encrypt/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleKMSEncrypt }},
		{Method: "GET", Pattern: "/api/v1/kms/keys", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListKeys }},
		{Method: "POST", Pattern: "/api/v1/kms/keys", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGenerateKey }},
		{Method: "POST", Pattern: "/api/v1/kms/keys/import", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleImportKey }},
		{Method: "POST", Pattern: "/api/v1/kms/keys/unwrap", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleUnwrapKey }},
		{Method: "DELETE", Pattern: "/api/v1/kms/keys/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteKey }},
		{Method: "GET", Pattern: "/api/v1/kms/keys/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetKey }},
		{Method: "PUT", Pattern: "/api/v1/kms/keys/{name}/default", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSetDefaultKey }},
		{Method: "POST", Pattern: "/api/v1/kms/keys/{name}/export", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleExportKey }},
		{Method: "GET", Pattern: "/api/v1/kms/keys/{name}/metadata", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetKeyMetadata }},
		{Method: "PUT", Pattern: "/api/v1/kms/keys/{name}/metadata", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleUpdateKeyMetadata }},
		{Method: "GET", Pattern: "/api/v1/kms/keys/{name}/policy", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetKeyPolicy }},
		{Method: "PUT", Pattern: "/api/v1/kms/keys/{name}/policy", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSetKeyPolicy }},
		{Method: "GET", Pattern: "/api/v1/kms/keys/{name}/public", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetPublicKey }},
		{Method: "POST", Pattern: "/api/v1/kms/keys/{name}/revoke", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRevokeKey }},
		{Method: "POST", Pattern: "/api/v1/kms/keys/{name}/rotate", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRotateKey }},
		{Method: "DELETE", Pattern: "/api/v1/kms/keys/{name}/rotation-policy", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteKeyRotationPolicy }},
		{Method: "GET", Pattern: "/api/v1/kms/keys/{name}/rotation-policy", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetKeyRotationPolicy }},
		{Method: "PUT", Pattern: "/api/v1/kms/keys/{name}/rotation-policy", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSetKeyRotationPolicy }},
		{Method: "DELETE", Pattern: "/api/v1/kms/keys/{name}/safety", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleClearKeySafety }},
		{Method: "PUT", Pattern: "/api/v1/kms/keys/{name}/safety", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSetKeySafety }},
		{Method: "PUT", Pattern: "/api/v1/kms/keys/{name}/status", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleUpdateKeyStatus }},
		{Method: "GET", Pattern: "/api/v1/kms/keys/{name}/versions", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListKeyVersions }},
		{Method: "GET", Pattern: "/api/v1/kms/safety/check", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSafetyCheck }},
		{Method: "GET", Pattern: "/api/v1/kms/safety/report", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSafetyReport }},
		{Method: "GET", Pattern: "/api/v1/kv/{type}/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleKVList }},
		{Method: "DELETE", Pattern: "/api/v1/kv/{type}/{key}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleKVDelete }},
		{Method: "GET", Pattern: "/api/v1/kv/{type}/{key}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleKVGet }},
		{Method: "POST", Pattern: "/api/v1/kv/{type}/{key}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleKVPut }},
		{Method: "PUT", Pattern: "/api/v1/kv/{type}/{key}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleKVPut }},
		{Method: "GET", Pattern: "/api/v1/messaging/channels", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListMessagingChannels }},
		{Method: "POST", Pattern: "/api/v1/messaging/channels", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCreateMessagingChannel }},
		{Method: "DELETE", Pattern: "/api/v1/messaging/channels/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteMessagingChannel }},
		{Method: "GET", Pattern: "/api/v1/messaging/channels/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetMessagingChannel }},
		{Method: "GET", Pattern: "/api/v1/messaging/dedup/stats/{channel}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleMessagingDedupStats }},
		{Method: "POST", Pattern: "/api/v1/messaging/publish-delta/{channel}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleMessagingPublishDelta }},
		{Method: "POST", Pattern: "/api/v1/messaging/publish/{channel}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleMessagingPublish }},
		{Method: "GET", Pattern: "/api/v1/messaging/stats", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleMessagingStats }},
		{Method: "GET", Pattern: "/api/v1/messaging/stats/{channel}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleMessagingChannelStats }},
		{Method: "POST", Pattern: "/api/v1/messaging/subscribe", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleMessagingSubscribe }},
		{Method: "DELETE", Pattern: "/api/v1/messaging/subscribe/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleMessagingUnsubscribe }},
		{Method: "POST", Pattern: "/api/v1/messaging/watch", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleMessagingWatch }},
		{Method: "GET", Pattern: "/api/v1/metrics/performance", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandlePerformanceMetrics }},
		{Method: "POST", Pattern: "/api/v1/ollama/embed", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOllamaEmbed }},
		{Method: "POST", Pattern: "/api/v1/ollama/embed/batch", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOllamaEmbedBatch }},
		{Method: "GET", Pattern: "/api/v1/ollama/health", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOllamaHealth }},
		{Method: "GET", Pattern: "/api/v1/ollama/models", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOllamaModelList }},
		{Method: "GET", Pattern: "/api/v1/ollama/models/{model}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOllamaModelInfo }},
		{Method: "GET", Pattern: "/api/v1/fleet", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListFleetNodes }},
		{Method: "POST", Pattern: "/api/v1/fleet", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRegisterFleetNode }},
		{Method: "DELETE", Pattern: "/api/v1/fleet/{nodeID}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRemoveFleetNode }},
		{Method: "GET", Pattern: "/api/v1/fleet/{nodeID}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetFleetNode }},
		{Method: "GET", Pattern: "/api/v1/fleet/{nodeID}/backends/{backendName}/models", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListFleetNodeBackendModels }},
		{Method: "POST", Pattern: "/api/v1/fleet/{nodeID}/disable", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDisableFleetNode }},
		{Method: "POST", Pattern: "/api/v1/fleet/{nodeID}/enable", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleEnableFleetNode }},
		{Method: "POST", Pattern: "/api/v1/fleet/{nodeID}/heartbeat", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleFleetNodeHeartbeat }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/attention", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListAttention }},
		{Method: "POST", Pattern: "/api/v1/orchestrator/attention/{taskID}/resolve", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleResolveAttention }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/backends", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorListBackends }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/cost", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorCost }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/spend-policy", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetTenantSpendPolicy }},
		{Method: "PUT", Pattern: "/api/v1/orchestrator/spend-policy", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSetTenantSpendPolicy }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/cost-settings", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetCostSettings }},
		{Method: "PUT", Pattern: "/api/v1/orchestrator/cost-settings", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSetCostSettings }},
		// The tenant AI agent catalog: Agents and the pools slots fail over along.
		{Method: "GET", Pattern: "/api/v1/agents", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListAgents }},
		{Method: "POST", Pattern: "/api/v1/agents", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCreateAgent }},
		{Method: "GET", Pattern: "/api/v1/agents/{agentID}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetAgent }},
		{Method: "PUT", Pattern: "/api/v1/agents/{agentID}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleUpdateAgent }},
		{Method: "DELETE", Pattern: "/api/v1/agents/{agentID}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteAgent }},
		{Method: "GET", Pattern: "/api/v1/agents/{agentID}/used-by", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleAgentUsedBy }},
		{Method: "GET", Pattern: "/api/v1/agent-pools", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListAgentPools }},
		{Method: "POST", Pattern: "/api/v1/agent-pools", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCreateAgentPool }},
		{Method: "GET", Pattern: "/api/v1/agent-pools/{poolID}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetAgentPool }},
		{Method: "PUT", Pattern: "/api/v1/agent-pools/{poolID}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleUpdateAgentPool }},
		{Method: "DELETE", Pattern: "/api/v1/agent-pools/{poolID}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteAgentPool }},
		{Method: "GET", Pattern: "/api/v1/agent-pools/{poolID}/used-by", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleAgentPoolUsedBy }},
		{Method: "POST", Pattern: "/api/v1/orchestrator/discover", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorDiscover }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/health", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorHealth }},
		{Method: "POST", Pattern: "/api/v1/orchestrator/hooks", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorHookEvent }},
		{Method: "POST", Pattern: "/api/v1/orchestrator/inference", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorInference }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/jobs", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorListJobs }},
		{Method: "POST", Pattern: "/api/v1/orchestrator/jobs", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorStartJob }},
		{Method: "DELETE", Pattern: "/api/v1/orchestrator/jobs/{jobID}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorCancelJob }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/jobs/{jobID}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorGetJobStatus }},
		{Method: "POST", Pattern: "/api/v1/orchestrator/jobs/{jobID}/children", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorSpawnChildJob }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/jobs/{jobID}/events", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorSubscribeJobEvents }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/runners", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorListRunners }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/sessions", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorListSessions }},
		{Method: "POST", Pattern: "/api/v1/orchestrator/sessions", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorSpawn }},
		{Method: "DELETE", Pattern: "/api/v1/orchestrator/sessions/{sessionID}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorCloseSession }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/sessions/{sessionID}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorSessionStatus }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/sessions/{sessionID}/events", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorSessionEvents }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/sessions/{sessionID}/history", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorSessionHistory }},
		{Method: "POST", Pattern: "/api/v1/orchestrator/sessions/{sessionID}/prompt", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorPrompt }},
		{Method: "POST", Pattern: "/api/v1/orchestrator/sessions/{sessionID}/rotate-account", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorRotateSessionAccount }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/tasks", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorListTasks }},
		{Method: "POST", Pattern: "/api/v1/orchestrator/tasks", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorCreateTask }},
		{Method: "POST", Pattern: "/api/v1/orchestrator/tasks/decompose", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorDecomposeTask }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/tasks/{taskID}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorGetTask }},
		{Method: "DELETE", Pattern: "/api/v1/orchestrator/tasks/{taskID}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorDeleteTask }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/tasks/{taskID}/artifacts", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorListArtifacts }},
		{Method: "POST", Pattern: "/api/v1/orchestrator/tasks/{taskID}/artifacts", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorSubmitArtifacts }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/tasks/{taskID}/artifacts/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorGetArtifact }},
		{Method: "POST", Pattern: "/api/v1/orchestrator/tasks/{taskID}/claim", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorClaimTask }},
		{Method: "POST", Pattern: "/api/v1/orchestrator/tasks/{taskID}/complete", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorCompleteTask }},
		{Method: "POST", Pattern: "/api/v1/orchestrator/tasks/{taskID}/block", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorBlockTask }},
		{Method: "POST", Pattern: "/api/v1/orchestrator/tasks/{taskID}/readmit", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorReadmitTask }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/workflows", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorListWorkflows }},
		{Method: "POST", Pattern: "/api/v1/orchestrator/workflows", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorCreateWorkflow }},
		{Method: "DELETE", Pattern: "/api/v1/orchestrator/workflows/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorDeleteWorkflow }},
		{Method: "GET", Pattern: "/api/v1/orchestrator/workflows/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorGetWorkflow }},
		{Method: "PUT", Pattern: "/api/v1/orchestrator/workflows/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOrchestratorUpdateWorkflow }},
		{Method: "GET", Pattern: "/api/v1/parameters/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListParameters }},
		{Method: "DELETE", Pattern: "/api/v1/parameters/*", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteParameter }},
		{Method: "GET", Pattern: "/api/v1/parameters/*", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetParameter }},
		{Method: "PUT", Pattern: "/api/v1/parameters/*", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSetParameter }},
		{Method: "POST", Pattern: "/api/v1/parameters/batch/delete", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteParametersBatch }},
		{Method: "POST", Pattern: "/api/v1/parameters/batch/get", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetParametersBatch }},
		{Method: "POST", Pattern: "/api/v1/parameters/batch/set", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSetParametersBatch }},
		{Method: "POST", Pattern: "/api/v1/parameters/search", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSearchParametersByTags }},
		{Method: "GET", Pattern: "/api/v1/projects/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListProjects }},
		{Method: "POST", Pattern: "/api/v1/projects/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCreateProject }},
		{Method: "GET", Pattern: "/api/v1/projects/by-name/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetProjectByName }},
		{Method: "DELETE", Pattern: "/api/v1/projects/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteProject }},
		{Method: "GET", Pattern: "/api/v1/projects/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetProject }},
		{Method: "PUT", Pattern: "/api/v1/projects/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleUpdateProject }},
		{Method: "DELETE", Pattern: "/api/v1/projects/{id}/permissions/{node_id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRevokeFleetNodeRole }},
		{Method: "PUT", Pattern: "/api/v1/projects/{id}/permissions/{node_id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGrantFleetNodeRole }},
		// The prompt store's eight verbs (handlers_prompt.go), addressed by
		// the tenant_id, scope and project_id query parameters.
		{Method: "GET", Pattern: "/api/v1/prompts/{set}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListPrompts }},
		{Method: "DELETE", Pattern: "/api/v1/prompts/{set}/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleResetPrompt }},
		{Method: "GET", Pattern: "/api/v1/prompts/{set}/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetPrompt }},
		{Method: "PUT", Pattern: "/api/v1/prompts/{set}/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandlePutPrompt }},
		{Method: "GET", Pattern: "/api/v1/prompts/{set}/{name}/diff", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDiffPrompt }},
		{Method: "GET", Pattern: "/api/v1/prompts/{set}/{name}/effective", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetEffectivePrompt }},
		{Method: "GET", Pattern: "/api/v1/prompts/{set}/{name}/history", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetPromptHistory }},
		{Method: "POST", Pattern: "/api/v1/prompts/{set}/{name}/rollback", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRollbackPrompt }},
		{Method: "POST", Pattern: "/api/v1/query/range", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRangeQuery }},
		{Method: "GET", Pattern: "/api/v1/query/{type}/{index}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleQuery }},
		{Method: "POST", Pattern: "/api/v1/registry/instances", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRegistryRegister }},
		{Method: "POST", Pattern: "/api/v1/registry/leases/{lease_id}/renew", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRegistryRenew }},
		{Method: "DELETE", Pattern: "/api/v1/registry/leases/{lease_id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRegistryDeregister }},
		{Method: "GET", Pattern: "/api/v1/registry/services/{service}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRegistryResolve }},
		{Method: "GET", Pattern: "/api/v1/registry/services/{service}/watch", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRegistryWatch }},
		{Method: "GET", Pattern: "/api/v1/scheduler/jobs", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListSchedulerJobs }},
		{Method: "POST", Pattern: "/api/v1/scheduler/jobs", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCreateSchedulerJob }},
		{Method: "DELETE", Pattern: "/api/v1/scheduler/jobs/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteSchedulerJob }},
		{Method: "GET", Pattern: "/api/v1/scheduler/jobs/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetSchedulerJob }},
		{Method: "PUT", Pattern: "/api/v1/scheduler/jobs/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleUpdateSchedulerJob }},
		{Method: "POST", Pattern: "/api/v1/scheduler/jobs/{id}/cancel", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCancelSchedulerJob }},
		{Method: "GET", Pattern: "/api/v1/scheduler/jobs/{id}/history", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetSchedulerJobHistory }},
		{Method: "POST", Pattern: "/api/v1/scheduler/jobs/{id}/pause", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandlePauseSchedulerJob }},
		{Method: "POST", Pattern: "/api/v1/scheduler/jobs/{id}/resume", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleResumeSchedulerJob }},
		{Method: "POST", Pattern: "/api/v1/scheduler/jobs/{id}/trigger", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleTriggerSchedulerJob }},
		{Method: "GET", Pattern: "/api/v1/scheduler/status", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetSchedulerStatus }},
		{Method: "POST", Pattern: "/api/v1/scheduler/nodes", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRegisterPlacementNode }},
		{Method: "GET", Pattern: "/api/v1/scheduler/nodes", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListPlacementNodes }},
		{Method: "GET", Pattern: "/api/v1/scheduler/nodes/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetPlacementNode }},
		{Method: "DELETE", Pattern: "/api/v1/scheduler/nodes/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeregisterPlacementNode }},
		{Method: "POST", Pattern: "/api/v1/scheduler/nodes/{id}/renew", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRenewPlacementNode }},
		{Method: "POST", Pattern: "/api/v1/scheduler/nodes/{id}/cordon", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCordonPlacementNode }},
		{Method: "POST", Pattern: "/api/v1/scheduler/nodes/{id}/uncordon", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleUncordonPlacementNode }},
		{Method: "POST", Pattern: "/api/v1/scheduler/nodes/{id}/drain", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDrainPlacementNode }},
		{Method: "POST", Pattern: "/api/v1/scheduler/placements", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandlePlaceWorkload }},
		{Method: "GET", Pattern: "/api/v1/scheduler/placements", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListPlacements }},
		{Method: "GET", Pattern: "/api/v1/scheduler/placements/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetPlacement }},
		{Method: "DELETE", Pattern: "/api/v1/scheduler/placements/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleReleasePlacement }},
		// The forge webhook ingress. PUBLIC at the PEP layer by necessity and
		// by design: a forge holds no platform identity and can present none,
		// so demanding one would make the endpoint unreachable rather than
		// secure. Its authentication is the HMAC signature over the body,
		// verified fail-closed inside the handler — a missing secret refuses.
		{Method: "POST", Pattern: "/api/v1/sdlc/webhook/{projectID}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSDLCWebhook }},
		// The SDLC module's surface: a project's forge binding and a task's
		// delivery. Permission-gated in the caller's own tenant; see
		// handlers_sdlc.go. Tasks, jobs and sessions are the orchestrator's.
		{Method: "GET", Pattern: "/api/v1/sdlc/forges", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListSDLCForges }},
		{Method: "GET", Pattern: "/api/v1/sdlc/projects/{projectID}/binding", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetSDLCBinding }},
		{Method: "PUT", Pattern: "/api/v1/sdlc/projects/{projectID}/binding", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBindSDLCProject }},
		{Method: "DELETE", Pattern: "/api/v1/sdlc/projects/{projectID}/binding", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleUnbindSDLCProject }},
		{Method: "POST", Pattern: "/api/v1/sdlc/projects/{projectID}/onboarding", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOnboardSDLCProject }},
		{Method: "POST", Pattern: "/api/v1/sdlc/repos/{repo}/onboarding", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleOnboardSDLCRepo }},
		{Method: "GET", Pattern: "/api/v1/sdlc/projects/{projectID}/conformance", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetSDLCConformance }},
		{Method: "GET", Pattern: "/api/v1/sdlc/projects/{projectID}/open-work", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetSDLCOpenWork }},
		{Method: "GET", Pattern: "/api/v1/sdlc/projects/{projectID}/pulls/{number}/reviewer", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetSDLCReviewer }},
		{Method: "PUT", Pattern: "/api/v1/sdlc/projects/{projectID}/pulls/{number}/reviewer", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSetSDLCReviewer }},
		{Method: "DELETE", Pattern: "/api/v1/sdlc/projects/{projectID}/pulls/{number}/reviewer", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleClearSDLCReviewer }},
		{Method: "POST", Pattern: "/api/v1/sdlc/projects/{projectID}/issues", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleFileSDLCIssue }},
		{Method: "GET", Pattern: "/api/v1/sdlc/projects/{projectID}/collaborators", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListSDLCCollaborators }},
		{Method: "POST", Pattern: "/api/v1/sdlc/projects/{projectID}/adopt", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleAdoptSDLCIssue }},
		{Method: "GET", Pattern: "/api/v1/sdlc/tasks/{taskID}/delivery", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetSDLCDelivery }},
		// The worker image refresh: host verbs over every tenant's bindings
		// (handlers_sdlc_worker_image.go).
		{Method: "GET", Pattern: "/api/v1/sdlc/worker-image", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetSDLCWorkerImage }},
		{Method: "POST", Pattern: "/api/v1/sdlc/worker-image/refresh", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRefreshSDLCWorkerImage }},
		{Method: "GET", Pattern: "/api/v1/secrets-prefix/*", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetSecretsByPrefix }},
		{Method: "GET", Pattern: "/api/v1/secrets-versions/*", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListSecretVersions }},
		{Method: "GET", Pattern: "/api/v1/secrets-watch/*", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleWatchSecrets }},
		{Method: "GET", Pattern: "/api/v1/secrets/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListSecrets }},
		{Method: "DELETE", Pattern: "/api/v1/secrets/*", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteSecret }},
		{Method: "GET", Pattern: "/api/v1/secrets/*", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetSecretOrVersions }},
		{Method: "POST", Pattern: "/api/v1/secrets/*", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.handleSecretsPostAction }},
		{Method: "PUT", Pattern: "/api/v1/secrets/*", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.handleSecretsPutAction }},
		{Method: "POST", Pattern: "/api/v1/secrets/dynamic/credentials/{role}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGenerateDynamicCredential }},
		{Method: "GET", Pattern: "/api/v1/secrets/dynamic/leases", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListDynamicSecretLeases }},
		{Method: "DELETE", Pattern: "/api/v1/secrets/dynamic/leases/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRevokeDynamicSecretLease }},
		{Method: "POST", Pattern: "/api/v1/secrets/dynamic/leases/{id}/renew", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRenewDynamicSecretLease }},
		{Method: "GET", Pattern: "/api/v1/secrets/dynamic/roles", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListDynamicSecretRoles }},
		{Method: "POST", Pattern: "/api/v1/secrets/dynamic/roles", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCreateDynamicSecretRole }},
		{Method: "DELETE", Pattern: "/api/v1/secrets/dynamic/roles/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteDynamicSecretRole }},
		{Method: "GET", Pattern: "/api/v1/secrets/dynamic/roles/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetDynamicSecretRole }},
		{Method: "POST", Pattern: "/api/v1/ssh/authorized", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleEmitAuthorized }},
		{Method: "GET", Pattern: "/api/v1/ssh/authorized/keys", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListAuthorizedKeys }},
		{Method: "POST", Pattern: "/api/v1/ssh/authorized/keys", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleAddAuthorizedKey }},
		{Method: "DELETE", Pattern: "/api/v1/ssh/authorized/keys/{scope}/{owner}/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRemoveAuthorizedKey }},
		{Method: "GET", Pattern: "/api/v1/ssh/authorized/keys/{scope}/{owner}/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetAuthorizedKey }},
		{Method: "POST", Pattern: "/api/v1/ssh/authorized/verify", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleVerifyAuthorized }},
		{Method: "GET", Pattern: "/api/v1/ssh/keys", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListSSHKeys }},
		{Method: "POST", Pattern: "/api/v1/ssh/keys", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGenerateSSHKey }},
		{Method: "POST", Pattern: "/api/v1/ssh/keys/import", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleImportSSHKey }},
		{Method: "DELETE", Pattern: "/api/v1/ssh/keys/{scope}/{owner}/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteSSHKey }},
		{Method: "GET", Pattern: "/api/v1/ssh/keys/{scope}/{owner}/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetSSHKey }},
		{Method: "GET", Pattern: "/api/v1/ssh/keys/{scope}/{owner}/{name}/public", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleExportSSHKeyPublic }},
		{Method: "POST", Pattern: "/api/v1/ssh/keys/{scope}/{owner}/{name}/sign", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSignSSHDigest }},
		{Method: "GET", Pattern: "/api/v1/ssh/known-hosts", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListKnownHosts }},
		{Method: "POST", Pattern: "/api/v1/ssh/known-hosts", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleAddKnownHost }},
		{Method: "DELETE", Pattern: "/api/v1/ssh/known-hosts/{host}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRemoveKnownHost }},
		{Method: "POST", Pattern: "/api/v1/ssh/known-hosts/{host}/verify", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleVerifyKnownHost }},
		{Method: "GET", Pattern: "/api/v1/timeseries/metrics", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListMetrics }},
		{Method: "POST", Pattern: "/api/v1/timeseries/query", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleTSQuery }},
		{Method: "GET", Pattern: "/api/v1/timeseries/retention/{metric}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetRetentionPolicy }},
		{Method: "PUT", Pattern: "/api/v1/timeseries/retention/{metric}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSetRetentionPolicy }},
		{Method: "DELETE", Pattern: "/api/v1/timeseries/series", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteSeries }},
		{Method: "POST", Pattern: "/api/v1/timeseries/write", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleWritePoint }},
		{Method: "POST", Pattern: "/api/v1/timeseries/write-batch", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleWriteBatch }},
		{Method: "POST", Pattern: "/api/v1/transaction", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleTransaction }},
		{Method: "POST", Pattern: "/api/v1/trust/auto-import-aia", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleTrustAutoImportAIA }},
		{Method: "GET", Pattern: "/api/v1/trust/cross-signed-roots", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleTrustCrossSignedRootPool }},
		{Method: "POST", Pattern: "/api/v1/trust/publish-dane", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleTrustPublishDANE }},
		{Method: "POST", Pattern: "/api/v1/vector/admin/compact", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCompactNamespace }},
		{Method: "GET", Pattern: "/api/v1/vector/admin/compact/progress", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetCompactionProgress }},
		{Method: "POST", Pattern: "/api/v1/vector/admin/export/{namespace}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleExportVectors }},
		{Method: "POST", Pattern: "/api/v1/vector/admin/import/{namespace}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleImportVectors }},
		{Method: "GET", Pattern: "/api/v1/vector/admin/stats/global", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetGlobalStats }},
		{Method: "GET", Pattern: "/api/v1/vector/cluster/stats", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetGlobalStats }},
		{Method: "GET", Pattern: "/api/v1/vector/health", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleVectorHealth }},
		{Method: "POST", Pattern: "/api/v1/vector/hybrid-search", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleHybridSearch }},
		{Method: "GET", Pattern: "/api/v1/vector/namespaces/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListNamespaces }},
		{Method: "POST", Pattern: "/api/v1/vector/namespaces/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleCreateNamespace }},
		{Method: "DELETE", Pattern: "/api/v1/vector/namespaces/{namespace}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteNamespace }},
		{Method: "GET", Pattern: "/api/v1/vector/namespaces/{namespace}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetNamespaceInfo }},
		{Method: "PUT", Pattern: "/api/v1/vector/namespaces/{namespace}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleUpdateNamespace }},
		{Method: "GET", Pattern: "/api/v1/vector/namespaces/{namespace}/index/progress", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetIndexProgress }},
		{Method: "POST", Pattern: "/api/v1/vector/namespaces/{namespace}/index/rebuild", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRebuildIndex }},
		{Method: "POST", Pattern: "/api/v1/vector/namespaces/{namespace}/search/by-id/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSearchByID }},
		{Method: "POST", Pattern: "/api/v1/vector/namespaces/{namespace}/search/similar", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSearchSimilar }},
		{Method: "GET", Pattern: "/api/v1/vector/namespaces/{namespace}/stats", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetNamespaceStats }},
		{Method: "GET", Pattern: "/api/v1/vector/namespaces/{namespace}/vectors/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListVectors }},
		{Method: "POST", Pattern: "/api/v1/vector/namespaces/{namespace}/vectors/", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleUpsertVector }},
		{Method: "DELETE", Pattern: "/api/v1/vector/namespaces/{namespace}/vectors/batch", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBatchDeleteVectors }},
		{Method: "POST", Pattern: "/api/v1/vector/namespaces/{namespace}/vectors/batch", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleBatchUpsertVectors }},
		{Method: "DELETE", Pattern: "/api/v1/vector/namespaces/{namespace}/vectors/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteVector }},
		{Method: "GET", Pattern: "/api/v1/vector/namespaces/{namespace}/vectors/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetVector }},
		{Method: "PUT", Pattern: "/api/v1/vector/namespaces/{namespace}/vectors/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleUpsertVector }},
		{Method: "PUT", Pattern: "/api/v1/vector/namespaces/{namespace}/vectors/{id}/metadata", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleUpdateMetadata }},
		{Method: "POST", Pattern: "/api/v1/vector/search-by-metadata", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSearchByMetadata }},
		{Method: "GET", Pattern: "/api/v1/version", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleVersion }},
		{Method: "GET", Pattern: "/api/v1/xkms/backends", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSListBackends }},
		{Method: "GET", Pattern: "/api/v1/xkms/backends/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSGetBackend }},
		{Method: "GET", Pattern: "/api/v1/xkms/ca/bundle", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSGetCABundle }},
		{Method: "GET", Pattern: "/api/v1/xkms/ca/cert", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSGetCACertificate }},
		{Method: "POST", Pattern: "/api/v1/xkms/ca/crl", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSGenerateCRL }},
		{Method: "POST", Pattern: "/api/v1/xkms/ca/issue", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSIssueCertificate }},
		{Method: "POST", Pattern: "/api/v1/xkms/ca/revoke", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSRevokeCertificate }},
		{Method: "GET", Pattern: "/api/v1/xkms/ca/revoked/{serial}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSIsRevoked }},
		{Method: "POST", Pattern: "/api/v1/xkms/ca/sign", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSSignCSR }},
		{Method: "GET", Pattern: "/api/v1/xkms/certs", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSListCerts }},
		{Method: "POST", Pattern: "/api/v1/xkms/certs", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSSaveCert }},
		{Method: "DELETE", Pattern: "/api/v1/xkms/certs/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSDeleteCert }},
		{Method: "GET", Pattern: "/api/v1/xkms/certs/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSGetCert }},
		{Method: "HEAD", Pattern: "/api/v1/xkms/certs/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSCertExists }},
		{Method: "POST", Pattern: "/api/v1/xkms/fido2/authenticate/begin", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSFIDO2BeginAuthentication }},
		{Method: "POST", Pattern: "/api/v1/xkms/fido2/authenticate/finish", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSFIDO2FinishAuthentication }},
		{Method: "POST", Pattern: "/api/v1/xkms/fido2/register/begin", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSFIDO2BeginRegistration }},
		{Method: "POST", Pattern: "/api/v1/xkms/fido2/register/finish", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSFIDO2FinishRegistration }},
		{Method: "POST", Pattern: "/api/v1/xkms/fido2/credentials/begin", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSFIDO2BeginAddCredential }},
		{Method: "GET", Pattern: "/api/v1/xkms/health", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSHealth }},
		{Method: "GET", Pattern: "/api/v1/xkms/keys", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSListKeys }},
		{Method: "POST", Pattern: "/api/v1/xkms/keys", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSGenerateKey }},
		{Method: "DELETE", Pattern: "/api/v1/xkms/keys/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSDeleteKey }},
		{Method: "GET", Pattern: "/api/v1/xkms/keys/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSGetKey }},
		{Method: "POST", Pattern: "/api/v1/xkms/keys/{id}/decrypt", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSDecrypt }},
		{Method: "POST", Pattern: "/api/v1/xkms/keys/{id}/encrypt", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSEncrypt }},
		{Method: "POST", Pattern: "/api/v1/xkms/keys/{id}/rotate", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSRotateKey }},
		{Method: "POST", Pattern: "/api/v1/xkms/keys/{id}/sign", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSSign }},
		{Method: "POST", Pattern: "/api/v1/xkms/keys/{id}/verify", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSVerify }},
		{Method: "GET", Pattern: "/api/v1/xkms/keys/{id}/versions", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSListKeyVersions }},
		{Method: "POST", Pattern: "/api/v1/xkms/keys/{id}/versions/disable-all", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSDisableAllKeyVersions }},
		{Method: "POST", Pattern: "/api/v1/xkms/keys/{id}/versions/enable-all", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSEnableAllKeyVersions }},
		{Method: "POST", Pattern: "/api/v1/xkms/keys/{id}/versions/{version}/disable", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSDisableKeyVersion }},
		{Method: "POST", Pattern: "/api/v1/xkms/keys/{id}/versions/{version}/enable", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSEnableKeyVersion }},
		{Method: "GET", Pattern: "/api/v1/xkms/pin/lockout", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSGetLockoutStatus }},
		{Method: "POST", Pattern: "/api/v1/xkms/pin/lockout/reset", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSResetLockout }},
		{Method: "POST", Pattern: "/api/v1/xkms/pin/so/change", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSChangeSOPIN }},
		{Method: "POST", Pattern: "/api/v1/xkms/pin/so/set", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSSetSOPIN }},
		{Method: "POST", Pattern: "/api/v1/xkms/pin/so/verify", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSVerifySOPIN }},
		{Method: "POST", Pattern: "/api/v1/xkms/pin/user/change", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSChangeUserPIN }},
		{Method: "POST", Pattern: "/api/v1/xkms/pin/user/set", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSSetUserPIN }},
		{Method: "POST", Pattern: "/api/v1/xkms/pin/user/verify", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSVerifyUserPIN }},
		{Method: "GET", Pattern: "/api/v1/xkms/piv/slots", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSListPIVSlots }},
		{Method: "DELETE", Pattern: "/api/v1/xkms/piv/slots/{slot}/cert", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSDeletePIVCertificate }},
		{Method: "GET", Pattern: "/api/v1/xkms/piv/slots/{slot}/cert", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSGetPIVCertificate }},
		{Method: "PUT", Pattern: "/api/v1/xkms/piv/slots/{slot}/cert", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSStorePIVCertificate }},
		{Method: "POST", Pattern: "/api/v1/xkms/piv/slots/{slot}/csr", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSGeneratePIVCSR }},
		{Method: "POST", Pattern: "/api/v1/xkms/piv/slots/{slot}/key", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSGeneratePIVKey }},
		{Method: "POST", Pattern: "/api/v1/xkms/seal", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSSeal }},
		{Method: "GET", Pattern: "/api/v1/xkms/seal/capability", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSCanSeal }},
		{Method: "POST", Pattern: "/api/v1/xkms/unseal", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleXKMSUnseal }},
		{Method: "POST", Pattern: "/api/v1/xkms/v1/ssh/authorized", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleEmitAuthorized }},
		{Method: "GET", Pattern: "/api/v1/xkms/v1/ssh/authorized/keys", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListAuthorizedKeys }},
		{Method: "POST", Pattern: "/api/v1/xkms/v1/ssh/authorized/keys", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleAddAuthorizedKey }},
		{Method: "DELETE", Pattern: "/api/v1/xkms/v1/ssh/authorized/keys/{scope}/{owner}/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRemoveAuthorizedKey }},
		{Method: "GET", Pattern: "/api/v1/xkms/v1/ssh/authorized/keys/{scope}/{owner}/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetAuthorizedKey }},
		{Method: "POST", Pattern: "/api/v1/xkms/v1/ssh/authorized/verify", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleVerifyAuthorized }},
		{Method: "GET", Pattern: "/api/v1/xkms/v1/ssh/keys", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListSSHKeys }},
		{Method: "POST", Pattern: "/api/v1/xkms/v1/ssh/keys", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGenerateSSHKey }},
		{Method: "POST", Pattern: "/api/v1/xkms/v1/ssh/keys/import", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleImportSSHKey }},
		{Method: "DELETE", Pattern: "/api/v1/xkms/v1/ssh/keys/{scope}/{owner}/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteSSHKey }},
		{Method: "GET", Pattern: "/api/v1/xkms/v1/ssh/keys/{scope}/{owner}/{name}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetSSHKey }},
		{Method: "GET", Pattern: "/api/v1/xkms/v1/ssh/keys/{scope}/{owner}/{name}/public", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleExportSSHKeyPublic }},
		{Method: "POST", Pattern: "/api/v1/xkms/v1/ssh/keys/{scope}/{owner}/{name}/sign", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleSignSSHDigest }},
		{Method: "GET", Pattern: "/api/v1/xkms/v1/ssh/known-hosts", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleListKnownHosts }},
		{Method: "POST", Pattern: "/api/v1/xkms/v1/ssh/known-hosts", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleAddKnownHost }},
		{Method: "DELETE", Pattern: "/api/v1/xkms/v1/ssh/known-hosts/{host}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRemoveKnownHost }},
		{Method: "POST", Pattern: "/api/v1/xkms/v1/ssh/known-hosts/{host}/verify", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleVerifyKnownHost }},

		// --- GroupAPIUpload ---
		{Method: "POST", Pattern: "/api/v1/admin/object-backends", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleCreateObjectBackend }},
		{Method: "DELETE", Pattern: "/api/v1/admin/object-backends/{name}", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteObjectBackend }},
		{Method: "PUT", Pattern: "/api/v1/admin/object-backends/{name}", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleUpdateObjectBackend }},
		{Method: "POST", Pattern: "/api/v1/admin/object-backends/{name}/compact", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleCompactObjectBackend }},
		{Method: "PUT", Pattern: "/api/v1/admin/object-backends/{name}/status", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleSetObjectBackendStatus }},
		{Method: "POST", Pattern: "/api/v1/admin/objects/repair", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleRepairObject }},
		{Method: "POST", Pattern: "/api/v1/admin/storage/gc", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleRunGarbageCollection }},
		{Method: "GET", Pattern: "/api/v1/admin/storage/gc/progress", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetGarbageCollectionProgress }},
		{Method: "GET", Pattern: "/api/v1/admin/storage/stats", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetStorageStats }},
		{Method: "GET", Pattern: "/api/v1/backups", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleListBackups }},
		{Method: "POST", Pattern: "/api/v1/backups", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleCreateBackup }},
		{Method: "DELETE", Pattern: "/api/v1/backups/{id}", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleDeleteBackup }},
		{Method: "POST", Pattern: "/api/v1/backups/{id}/restore", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleRestoreBackup }},
		{Method: "GET", Pattern: "/api/v1/backups/{id}/status", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleBackupStatus }},
		{Method: "POST", Pattern: "/api/v1/backups/{id}/verify", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleVerifyBackup }},
		{Method: "GET", Pattern: "/api/v1/object-backends", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleListObjectBackends }},
		{Method: "GET", Pattern: "/api/v1/object-backends/{name}/health", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetObjectBackendHealth }},
		{Method: "POST", Pattern: "/api/v1/object-transfer/fetch", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleFetchObject }},
		{Method: "POST", Pattern: "/api/v1/object-transfer/stream-fetch", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleStreamFetchObjects }},
		{Method: "GET", Pattern: "/api/v1/objects/", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleListObjects }},
		{Method: "DELETE", Pattern: "/api/v1/objects/*", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.handleObjectAction }},
		{Method: "GET", Pattern: "/api/v1/objects/*", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.handleObjectAction }},
		{Method: "HEAD", Pattern: "/api/v1/objects/*", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.handleObjectAction }},
		{Method: "POST", Pattern: "/api/v1/objects/*", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.handleObjectAction }},
		{Method: "PUT", Pattern: "/api/v1/objects/*", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.handleObjectAction }},
		{Method: "POST", Pattern: "/api/v1/objects/batch", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleBatchPutObjects }},
		{Method: "POST", Pattern: "/api/v1/objects/batch/delete", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleBatchDeleteObjects }},
		{Method: "POST", Pattern: "/api/v1/objects/batch/get", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleBatchGetObjects }},
		{Method: "GET", Pattern: "/api/v1/objects/by-tag", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleListByTag }},
		{Method: "GET", Pattern: "/api/v1/objects/by-tenant/{tenant}", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleListByTenant }},
		{Method: "GET", Pattern: "/api/v1/tenants/{tenant}/quotas", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetTenantQuotas }},
		{Method: "PUT", Pattern: "/api/v1/tenants/{tenant}/quotas", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleSetTenantQuotas }},
		{Method: "GET", Pattern: "/api/v1/tenants/{tenant}/usage", Group: GroupAPIUpload, Handler: func(s *Server) http.HandlerFunc { return s.HandleGetTenantUsage }},

		// --- GroupBarrierWire ---
		// Load-bearing for the standalone sealed-window REST listener
		// (pkg/server/sealed_window_rest.go); see the RouteGroup doc comment.
		{Method: "POST", Pattern: "/v1/barrier/enable-encryption", Group: GroupBarrierWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierWireEnableEncryption }},
		{Method: "POST", Pattern: "/v1/barrier/initialize", Group: GroupBarrierWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierInitialize }},
		{Method: "DELETE", Pattern: "/v1/barrier/recovery", Group: GroupBarrierWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierRecoveryDelete }},
		{Method: "POST", Pattern: "/v1/barrier/recovery/generate", Group: GroupBarrierWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierRecoveryGenerate }},
		{Method: "POST", Pattern: "/v1/barrier/recovery/recover", Group: GroupBarrierWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierRecoveryRecover }},
		{Method: "GET", Pattern: "/v1/barrier/recovery/status", Group: GroupBarrierWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierRecoveryStatus }},
		{Method: "POST", Pattern: "/v1/barrier/rekey", Group: GroupBarrierWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierRekey }},
		{Method: "POST", Pattern: "/v1/barrier/root-token", Group: GroupBarrierWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierRootToken }},
		{Method: "POST", Pattern: "/v1/barrier/seal", Group: GroupBarrierWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierSeal }},
		{Method: "POST", Pattern: "/v1/barrier/shamir/initialize", Group: GroupBarrierWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierShamirInitialize }},
		{Method: "POST", Pattern: "/v1/barrier/shamir/unseal-share", Group: GroupBarrierWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierShamirUnsealShare }},
		{Method: "POST", Pattern: "/v1/barrier/shamir/unseal-shares", Group: GroupBarrierWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierShamirUnsealShares }},
		{Method: "GET", Pattern: "/v1/barrier/status", Group: GroupBarrierWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierStatus }},
		{Method: "POST", Pattern: "/v1/barrier/unseal", Group: GroupBarrierWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleBarrierUnseal }},

		// --- GroupCeremonyWire ---
		// Load-bearing for the standalone sealed-window REST listener; see
		// the RouteGroup doc comment.
		{Method: "POST", Pattern: "/v1/ceremony/claim-cert/begin", Group: GroupCeremonyWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleCeremonyClaimCertBegin }},
		{Method: "POST", Pattern: "/v1/ceremony/claim-cert/complete", Group: GroupCeremonyWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleCeremonyClaimCertComplete }},
		{Method: "POST", Pattern: "/v1/ceremony/initialize", Group: GroupCeremonyWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleCeremonyInitialize }},
		{Method: "POST", Pattern: "/v1/ceremony/officer-share/begin", Group: GroupCeremonyWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleCeremonyBeginClaimShare }},
		{Method: "POST", Pattern: "/v1/ceremony/officer-share/complete", Group: GroupCeremonyWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleCeremonyCompleteClaimShare }},
		{Method: "GET", Pattern: "/v1/ceremony/officers", Group: GroupCeremonyWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleCeremonyListOfficers }},
		{Method: "POST", Pattern: "/v1/ceremony/sign-csr", Group: GroupCeremonyWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleCeremonySignCSR }},
		{Method: "GET", Pattern: "/v1/ceremony/status", Group: GroupCeremonyWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleCeremonyStatus }},

		// --- GroupInitWire ---
		// Load-bearing for the standalone sealed-window REST listener; see
		// the RouteGroup doc comment.
		{Method: "POST", Pattern: "/v1/init/enroll-external-officer", Group: GroupInitWire, Handler: func(s *Server) http.HandlerFunc { return s.HandleCeremonyEnrollExternalOfficer }},
	}

	// Cluster CA secret delivery (tpm-kek): mounted only when the active
	// cluster CA key strategy delivers post-enrollment material.
	specs = append(specs, RouteSpec{
		Method:  "POST",
		Pattern: "/api/v1/cluster/secret",
		Group:   GroupAPI,
		Enabled: func(s *Server) bool { return s.clusterSecretHandler != nil },
		Handler: func(s *Server) http.HandlerFunc { return s.clusterSecretHandler },
	})

	// RAG surface: contributed by the rag build-tag variant.
	specs = append(specs, ragRouteSpecs()...)

	// Module-only surfaces (routes_iam.go) that DO derive their Auth from the
	// PEP mapper the same way the surfaces above do: every one of these chi
	// mounts is wrapped in useAPIAuthzMiddleware on the legacy listener, so
	// they belong in rawRouteSpecs and are frozen by applyFrozenPermissions
	// (Routes()) exactly like every other entry above.
	specs = append(specs, iamManagedRouteSpecs()...)
	specs = append(specs, oauth2RegisterRouteSpecs()...)
	specs = append(specs, metricsScrapeRouteSpecs()...)
	specs = append(specs, debugModuleRouteSpecs()...)

	return specs
}

// Routes returns the full REST route table: rawRouteSpecs with its
// Auth/Resource/Action stamped from the FROZEN permission snapshot
// (applyFrozenPermissions, routes_permissions_generated.go — a one-time
// codegen capture of the deleted REST PEP mapper's output, not a live
// derivation), plus the explicitly-public root mounts appended with their
// Auth already set. Order is stable (grouped by middleware profile, then by
// pattern, then by the module-only groups in registration order), so tests
// can assert the exact surface. Build-tag-conditional surfaces contribute
// through their own spec functions (ragRouteSpecs), keeping the served
// table identical to the compiled handler set.
func Routes() []RouteSpec {
	specs := applyFrozenPermissions(rawRouteSpecs())

	// Module-only surfaces that are genuinely public-by-protocol: the legacy
	// listener mounts every one of these at the router root with NO
	// authorization wrapper at all (registerIAMRoutes / handlers_acme.go /
	// setupRouter's docs+UI registrations), so there was never a per-request
	// PEP decision to derive from. Their Auth is set explicitly on the
	// RouteSpec literal (routes_iam.go) rather than frozen from the mapper.
	specs = append(specs, oidcDiscoveryRouteSpecs()...)
	specs = append(specs, oauth2CoreRouteSpecs()...)
	specs = append(specs, apiAuthPublicRouteSpecs()...)
	specs = append(specs, acmeDirectoryRouteSpecs()...)
	specs = append(specs, docsRouteSpecs()...)
	specs = append(specs, uiMountRouteSpecs()...)

	return specs
}

// moduleExcludedPathPrefixes lists the Routes() patterns that must NOT be
// registered on the unified-API-server ServiceModule surface.
//
// /api/v1/init/* is the ceremony plane. On a composed node the co-hosted
// go-xkms ServiceModule registers its own /api/v1/init/* ceremony routes
// (enable-gated on its init handlers), so quicbaas's copies would
// route-collide at module-construction time. Which module owns the composed
// node's init plane is an open decision (go-quicbaas#405); until it is made,
// quicbaas's copies stay off the module surface.
//
// /api/v1/barrier/* is deliberately NOT excluded any more. It used to be, on
// the premise that "the legacy in-process listener keeps serving them" — but
// startRESTServer no longer binds a listener (see lifecycle_rest.go: REST
// network serving runs exclusively on the unified server), so the exclusion
// left the canonical barrier surface owned by the xkms module, whose
// same-path handlers operate xkms's OWN, host-inert barrier and answered
// `503 rest: barrier not configured` on a node whose barrier was configured
// and unsealed (go-quicbaas#405). The composer now drops the xkms module's
// barrier registrations instead (pkg/apimodule route_precedence.go), so
// quicbaas's handlers — which delegate to the go-qrdb SDK BarrierClient every
// other transport already serves — own the path.
var moduleExcludedPathPrefixes = []string{
	"/api/v1/init/",
}

// qrdbNativeWireRoutes are the go-qrdb wire routes (GroupBarrierWire,
// GroupCeremonyWire, GroupInitWire) that go-qrdb's unified REST server serves
// natively from go-qrdb v0.25: the /v1/barrier, /v1/ceremony and /v1/init
// mounts are go-qrdb's protocol, and go-qrdb registers them itself on the
// unified server. quicbaas's copies stay in Routes() for the standalone
// sealed-window listener, which serves before the unified server exists, but
// they must not be registered on the unified server too: a REST route has one
// owner there, and go-qrdb's registrar panics on the duplicate. GET
// /v1/ceremony/officers is quicbaas's own and stays on the module surface.
var qrdbNativeWireRoutes = map[string]bool{
	"POST /v1/barrier/enable-encryption":       true,
	"POST /v1/barrier/initialize":              true,
	"DELETE /v1/barrier/recovery":              true,
	"POST /v1/barrier/recovery/generate":       true,
	"POST /v1/barrier/recovery/recover":        true,
	"GET /v1/barrier/recovery/status":          true,
	"POST /v1/barrier/rekey":                   true,
	"POST /v1/barrier/root-token":              true,
	"POST /v1/barrier/seal":                    true,
	"POST /v1/barrier/shamir/initialize":       true,
	"POST /v1/barrier/shamir/unseal-share":     true,
	"POST /v1/barrier/shamir/unseal-shares":    true,
	"GET /v1/barrier/status":                   true,
	"POST /v1/barrier/unseal":                  true,
	"POST /v1/ceremony/claim-cert/begin":       true,
	"POST /v1/ceremony/claim-cert/complete":    true,
	"POST /v1/ceremony/initialize":             true,
	"POST /v1/ceremony/officer-share/begin":    true,
	"POST /v1/ceremony/officer-share/complete": true,
	"POST /v1/ceremony/sign-csr":               true,
	"GET /v1/ceremony/status":                  true,
	"POST /v1/init/enroll-external-officer":    true,
}

// ModuleRoutes returns the subset of Routes() safe to register on the
// unified-API-server ServiceModule surface (pkg/apimodule module_http.go
// RegisterHTTP): every entry EXCEPT the moduleExcludedPathPrefixes routes and
// the qrdbNativeWireRoutes go-qrdb serves there itself.
// The standalone sealed-window REST listener
// (pkg/server/sealed_window_rest.go) consumes Routes() directly, so it is
// unaffected by this filter — barrier and init remain fully served there for
// the sealed-window boot path.
func ModuleRoutes() []RouteSpec {
	all := Routes()
	out := make([]RouteSpec, 0, len(all))
	for _, spec := range all {
		excluded := qrdbNativeWireRoutes[spec.Method+" "+spec.Pattern]
		for _, prefix := range moduleExcludedPathPrefixes {
			if strings.HasPrefix(spec.Pattern, prefix) {
				excluded = true
				break
			}
		}
		if !excluded {
			out = append(out, spec)
		}
	}
	return out
}
