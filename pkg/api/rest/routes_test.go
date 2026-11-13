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
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// baseRouteTableSize is the number of routes the table carries independent of
// build-tag-conditional contributions: the 455 surfaces converted from the
// historical setupRouter registrations, the cluster CA secret-delivery route,
// and the 160 IAM/OIDC + ACME directory + docs + UI + metrics-scrape +
// debug-mount surfaces contributed by routes_iam.go (Wave A of the unified-
// API-server cutover: these surfaces gained module registration in addition
// to their existing legacy-listener registration, see routes_iam.go's
// package doc comment), MINUS the 16 qrdb-wire proxy routes removed once
// go-qrdb's unified REST server started serving /v1/credentials (8),
// /users/by-username (1), and /v1/users/factors/* (7) natively on the same
// listener. GroupBarrierWire (14) / GroupCeremonyWire (8) /
// GroupInitWire (1) stay in the table: they back the standalone
// sealed-window REST listener (pkg/server/sealed_window_rest.go), which
// exists before go-qrdb's unified server does. Plus 1 for the restored RFC
// 9728 protected-resource metadata route (/.well-known/oauth-protected-
// resource, iam_protected_resource_metadata.go), the MCP client-auth
// discovery plane recovered as a module public route (unified-API-server
// login-plane plan, P6b). Plus 1 for POST /api/v1/iam/audit
// (RecordAuditEvent, iam_audit.go), the remote audit-emission endpoint
// closing the last hole in the unified audit plane (P5.3). MINUS 3 for the
// P3.3 de-collision: refresh/logout/validate/login-piv (4) dropped from
// apiAuthPublicRouteSpecs (a composed node's xkms module now owns those
// /api/v1/auth/* paths as part of its own session-login plane), PLUS 4 for
// all four verbs relocated into the IAM namespace rather than dropped --
// login-piv (POST /api/v1/iam/login/piv), refresh (POST /api/v1/iam/refresh,
// go-quicbaas#221: quicbaas still owns the login ceremony, so it must own the
// renewal of the credential that ceremony issues), and validate + logout
// (POST /api/v1/iam/{validate,logout}, go-quicbaas#256: the SPA calls both
// verbs literally, so leaving them on xkms's plane made the daemon reject the
// bearer it had just minted) -- net 0. The rag build tag contributes
// ragRouteSpecs() on top. NOTE: this count is Routes(), the full table
// (legacy listener + module surface); ModuleRoutes() (routes.go), which
// additionally excludes /api/v1/barrier/* (14) and /api/v1/init/* (9) from
// the module-only surface, is 23 SMALLER and has its own guard
// (TestModuleRoutes_ExcludesBarrierAndInit below). PLUS 3 for the SDLC
// module's binding surface (/api/v1/sdlc/projects/{projectID}/binding,
// handlers_sdlc.go, go-quicbaas#569): read, bind, unbind. PLUS 3 for the
// WebAuthn credential administration surface
// (/api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials,
// iam_webauthn_credentials.go, go-quicbaas#273): list, delete-one, delete-all.
// PLUS 1 for PUT /api/v1/secrets/* (go-quicbaas#280): the secret-status verb
// the REST client had always called and no route served, and which gRPC and MCP
// both expose — enable/disable hardcode one status each and cannot reach
// `deleted`. PLUS 1 for the SDLC delivery read
// (/api/v1/sdlc/tasks/{taskID}/delivery, handlers_sdlc.go, go-quicbaas#569):
// what a task's attempt published, so the pull request is reachable from
// the task without a module client.
// PLUS 2 for the orchestration task lifecycle verbs
// (POST /api/v1/orchestrator/tasks/{taskID}/block and /readmit,
// handlers_orchestrator.go, go-quicbaas#552): a task parked until a person
// answers, and the person's answer. PLUS 1 for built-in role reconciliation
// (POST /api/v1/tenants/{tenant_id}/roles/reconcile, iam_roles.go,
// go-quicbaas#573): the only write that may re-permission a built-in role,
// since qrdb refuses an ordinary update on a system one.
// PLUS 1 for the task delete verb
// (DELETE /api/v1/orchestrator/tasks/{taskID}, handlers_orchestrator.go,
// go-quicbaas#612): the store verb that had no route, and with it the only
// way to release a task's source reservation so its issue can be adopted
// again.
// PLUS 1 for the pooled-account credential release
// (POST /api/v1/agent-accounts/{id}/materialize, handlers_account.go,
// go-quicbaas#734): the one account route whose response body is secret
// material, and the only way a pooled credential reaches the worker host that
// runs the agent it belongs to.
// PLUS 1 for the consumption ledger
// (GET /api/v1/orchestrator/cost, handlers_orchestrator.go,
// go-quicbaas#794): what the fleet has spent, read from the ledger the
// engine already writes.
// PLUS 2 for project onboarding
// (POST /api/v1/sdlc/projects/{projectID}/onboarding and
// GET /api/v1/sdlc/projects/{projectID}/conformance, handlers_sdlc.go,
// go-quicbaas#877): repair and report, deliberately two routes under two
// permissions so no GET can rewrite a repository's configuration.
// PLUS 3 for the per-pull-request reviewer override
// (GET, PUT and DELETE /api/v1/sdlc/projects/{projectID}/pulls/{number}/reviewer,
// handlers_sdlc.go): an operator choosing which reviewer profile judges one
// pull request — read under sdlc:read, written under sdlc:bind.
// PLUS 2 for the worker image refresh
// (GET /api/v1/sdlc/worker-image and POST /api/v1/sdlc/worker-image/refresh,
// handlers_sdlc_worker_image.go, go-quicbaas#1021): report and request, again
// under two permissions of the cross-tenant worker_images family.
// PLUS 2 for the attention inbox
// (GET /api/v1/orchestrator/attention and
// POST /api/v1/orchestrator/attention/{taskID}/resolve, go-quicbaas#1020):
// everything only an operator can clear, and the one verb that clears it.
// PLUS 1 for a project's open work
// (GET /api/v1/sdlc/projects/{projectID}/open-work, handlers_sdlc.go): the
// agent's open pull requests and the labelled issues, read from the forge.
// MINUS 8 for the tenant group routes (/api/v1/tenants/{tenant_id}/groups…):
// quicbaas keeps no groups; identity is qrdb's.
// PLUS 2 for filing an issue and listing collaborators
// (POST /api/v1/sdlc/projects/{projectID}/issues under sdlc:issue_create and
// GET /api/v1/sdlc/projects/{projectID}/collaborators under sdlc:read).
// PLUS 1 for adopting an issue (POST /api/v1/sdlc/projects/{projectID}/adopt,
// handlers_sdlc_adopt.go): the operator verb that hands an issue no task
// holds back to the factory, under orchestrator:task_create.
// PLUS 12 for the tenant AI agent catalog (/api/v1/agents and
// /api/v1/agent-pools: list, create, get, update, delete and used-by of each).
// PLUS 5 for the service registry (/api/v1/registry: register, renew,
// deregister, resolve and the SSE watch, handlers_registry.go, 13 G9), under
// the service_registry family's register and read verbs.
// PLUS 12 for the scheduler's workload placement surface
// (handlers_scheduler_placement.go, go-quicbaas#1143): eight node routes
// under /api/v1/scheduler/nodes (register, list, get, deregister, renew,
// cordon, uncordon, drain) and four placement routes under
// /api/v1/scheduler/placements (place, list, get, release), in the
// "placement" permission family.
// PLUS 8 for the attestation Verifier (/api/v1/attestation/endorsements,
// /reference-values and /appraise-quote, handlers_attestation_verifier.go):
// managing what it trusts under attestation:provision, reading and
// appraising under attestation:verify.
// PLUS 1 for tenant provisioning (POST /api/v1/tenants/{tenant_id}/provision,
// iam_tenants.go, go-quicbaas#358): the control-plane onboarding path that
// creates a tenant with its first admin when public registration is off.
// PLUS 1 for the FIDO2 add-credential compat leg
// (/api/v1/xkms/fido2/credentials/begin, handlers_xkms_fido2.go) under
// user_authfactors:add.
// PLUS 1 for the project by-name lookup (GET /api/v1/projects/by-name/{name},
// go-quicbaas#1261) under project:read, the REST face of GetProjectByName.
// PLUS 1 for repository onboarding (POST /api/v1/sdlc/repos/{repo}/onboarding,
// go-quicbaas#1041) under sdlc:bind.
// PLUS 1 for the messaging watch (POST /api/v1/messaging/watch, issue #185)
// under messaging:watch, the REST face of Messaging.WatchChannel.
// PLUS 8 for the prompt store's verbs (/api/v1/prompts/{set}[/{name}...],
// handlers_prompt.go, go-quicbaas#805) under prompts:read, prompts:history
// and prompts:write.
// PLUS 1 for the atomic batch (POST /api/v1/batch/execute, handlers.go, go-quicbaas#1537) under batch:execute.
const baseRouteTableSize = 673

// TestRoutes_SurfaceGuard asserts the table's structural invariants: exact
// size, absolute patterns, no duplicate method+pattern, and a handler binder
// on every entry.
func TestRoutes_SurfaceGuard(t *testing.T) {
	specs := Routes()

	want := baseRouteTableSize + len(ragRouteSpecs())
	if len(specs) != want {
		t.Errorf("route table size: got %d, want %d", len(specs), want)
	}

	seen := map[string]struct{}{}
	for _, spec := range specs {
		if spec.Method == "" {
			t.Errorf("route %q has empty method", spec.Pattern)
		}
		if !strings.HasPrefix(spec.Pattern, "/") {
			t.Errorf("route pattern not absolute: %q", spec.Pattern)
		}
		if spec.Handler == nil {
			t.Errorf("route %s %s has no handler binder", spec.Method, spec.Pattern)
		}
		key := spec.Method + " " + spec.Pattern
		if _, dup := seen[key]; dup {
			t.Errorf("duplicate route: %s", key)
		}
		seen[key] = struct{}{}

		if mount, wire := routeGroupMounts[spec.Group]; wire {
			if !strings.HasPrefix(spec.Pattern, mount) {
				t.Errorf("route %s %s not under its group mount %s", spec.Method, spec.Pattern, mount)
			}
		} else if !strings.HasPrefix(spec.Pattern, "/api/v1/") {
			t.Errorf("route %s %s in an /api/v1 group but not under /api/v1", spec.Method, spec.Pattern)
		}
	}
}

// TestRoutes_PublicRoutes pins the exact public sub-surface: only the health
// probes and the version endpoint serve unauthenticated. Anything else
// showing up here is a pre-auth exposure regression.
func TestRoutes_PublicRoutes(t *testing.T) {
	want := map[string]struct{}{
		"GET /api/v1/health":       {},
		"GET /api/v1/health/live":  {},
		"GET /api/v1/health/ready": {},
		"GET /api/v1/version":      {},

		// OIDC/OAuth2 discovery (GroupOIDCDiscovery) + core token endpoints
		// (GroupOAuth2Core): public by protocol, unwrapped on the legacy
		// listener (routes_iam.go oidcDiscoveryRouteSpecs / oauth2CoreRouteSpecs).
		"GET /.well-known/openid-configuration":       {},
		"GET /.well-known/oauth-authorization-server": {},
		"GET /.well-known/oauth-protected-resource":   {},
		"GET /.well-known/jwks.json":                  {},
		"GET /oauth2/authorize":                       {},
		"POST /oauth2/token":                          {},
		"POST /oauth2/token-mtls":                     {},
		"GET /oauth2/userinfo":                        {},
		"POST /oauth2/revoke":                         {},
		"POST /oauth2/introspect":                     {},

		// RFC 7592 dynamic client management: pre-auth via the per-client
		// registration access token (GroupOAuth2Register, PEP-derived).
		"GET /oauth2/register/{client_id}":    {},
		"PUT /oauth2/register/{client_id}":    {},
		"DELETE /oauth2/register/{client_id}": {},

		// /api/v1/auth login/self-registration ceremonies, federated
		// login/callback, and mTLS agent-registration bootstrap
		// (GroupAPIAuthPublic): unwrapped on the legacy listener. The token-
		// lifecycle verbs (refresh/logout/validate) and login/piv were
		// removed from here by P3.3 (apiAuthPublicRouteSpecs no longer
		// carries them -- a composed node's xkms module owns those
		// /api/v1/auth/* paths; the legacy listener alone keeps serving them
		// via iam.go's independent hand-mount, which this module-surface
		// table does not feed).
		"POST /api/v1/auth/login/begin":                                   {},
		"POST /api/v1/auth/login/finish":                                  {},
		"POST /api/v1/auth/register/begin":                                {},
		"POST /api/v1/auth/register/finish":                               {},
		"GET /api/v1/auth/providers/{tenant_id}/{provider_name}/login":    {},
		"GET /api/v1/auth/providers/{tenant_id}/{provider_name}/callback": {},
		"POST /api/v1/fleet/enroll":                                       {},

		// Invite activation + WebAuthn login ceremony (GroupIAM, PEP-derived
		// via preAuthRouteTemplates). login/piv (P3.3) is quicbaas's own
		// OIDC-session PIV login, moved here from /api/v1/auth/login/piv to
		// clear the collision with xkms's module; refresh is the same move
		// for the same reason (go-quicbaas#221) -- the opaque refresh token
		// in the body is the credential, and no valid bearer exists at
		// renewal time by definition.
		"GET /api/v1/iam/{tenant_id}/invites/{token}":                        {},
		"POST /api/v1/iam/{tenant_id}/invites/{token}/activate/fido2/begin":  {},
		"POST /api/v1/iam/{tenant_id}/invites/{token}/activate/fido2/finish": {},
		"POST /api/v1/iam/{tenant_id}/invites/{token}/activate/piv":          {},
		"POST /api/v1/iam/login/piv":                                         {},
		"POST /api/v1/iam/refresh":                                           {},
		"POST /api/v1/iam/{tenant_id}/webauthn/login/begin":                  {},
		"POST /api/v1/iam/{tenant_id}/webauthn/login/finish":                 {},

		// Raw RFC 8555 ACME directory (GroupACMEDirectory): JWS-self-
		// authenticating per the ACME protocol, unwrapped on the legacy
		// listener.
		"GET /acme/directory":            {},
		"HEAD /acme/directory":           {},
		"GET /acme/new-nonce":            {},
		"HEAD /acme/new-nonce":           {},
		"POST /acme/new-account":         {},
		"POST /acme/account/{id}":        {},
		"POST /acme/new-order":           {},
		"POST /acme/order/{id}":          {},
		"POST /acme/order/{id}/finalize": {},
		"POST /acme/orders/{id}":         {},
		"POST /acme/authz/{id}":          {},
		"POST /acme/challenge/{id}":      {},
		"POST /acme/cert/{id}":           {},
		"POST /acme/revoke-cert":         {},
		"POST /acme/key-change":          {},
		"GET /acme/ca-bundle":            {},

		// Forge webhook ingress. PUBLIC AT THE PEP LAYER BY NECESSITY: a
		// forge holds no platform identity and can present none, so an
		// identity gate would refuse every legitimate delivery rather than
		// secure anything. Its authentication is the HMAC signature over the
		// body, verified fail-closed in the handler — a project with no
		// configured secret refuses, and unknown-project, missing-secret and
		// bad-signature are deliberately indistinguishable to the caller.
		// Listed here so that being public is a decision on this allowlist
		// rather than a property acquired by adding a route.
		"POST /api/v1/sdlc/webhook/{projectID}": {},

		// Docs (GroupDocs) + embedded UI (GroupUIMount): unwrapped on the
		// legacy listener.
		"GET /docs":              {},
		"GET /docs/":             {},
		"GET /docs/openapi.yaml": {},
		"GET /ui":                {},
		"GET /ui/*":              {},
	}
	got := map[string]struct{}{}
	for _, spec := range Routes() {
		if spec.Auth == RouteAuthPublic {
			got[spec.Method+" "+spec.Pattern] = struct{}{}
		}
	}
	for key := range want {
		if _, ok := got[key]; !ok {
			t.Errorf("expected public route missing: %s", key)
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("unexpected public route: %s", key)
		}
	}
}

// TestRoutes_SessionRoutes pins the identity-gated (mapped-permission-less)
// sub-surface. A route falling out of the PEP mapping lands here and is
// served identity-gated on the unified server — this pin makes any such
// regression explicit.
func TestRoutes_SessionRoutes(t *testing.T) {
	want := map[string]struct{}{
		"GET /api/v1/metrics/performance": {},
		// Prometheus scrape mount (GroupMetricsScrape): no PEP entry maps
		// "/api/v1/metrics" specifically, so it derives to the same
		// identity-gated posture as its "performance" sibling above (see
		// routes_iam.go metricsScrapeRouteSpecs).
		"GET /api/v1/metrics/": {},
		// Token-lifecycle verbs relocated into the IAM namespace
		// (go-quicbaas#256, routes_iam.go iamManagedRouteSpecs). Both act ON
		// a bearer that already exists, so the identity gate belongs in the
		// middleware and no RBAC permission applies: validating or revoking
		// YOUR OWN session is not a privileged action, and the handler reads
		// the subject from the token, not from a path parameter. Being
		// non-public is also what attaches the module's AuthenticateHTTP
		// fallback, the seam that validates a quicbaas-minted bearer at all.
		"POST /api/v1/iam/validate": {},
		"POST /api/v1/iam/logout":   {},
		// The go-qrdb wire mount takes its scope from the body tenant_id, so
		// no one registration-site permission describes it; the handler
		// authorizes the selected scope itself (authorizeBarrierScope,
		// go-quicbaas#990). Its end-to-end authorization is pinned in
		// pkg/apimodule module_barrier_enable_encryption_authz_test.go.
		"POST /v1/barrier/enable-encryption": {},
	}
	got := map[string]struct{}{}
	for _, spec := range Routes() {
		if spec.Auth == RouteAuthSession {
			got[spec.Method+" "+spec.Pattern] = struct{}{}
		}
	}
	for key := range want {
		if _, ok := got[key]; !ok {
			t.Errorf("expected session route missing: %s", key)
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("unexpected session (unmapped) route: %s", key)
		}
	}
}

// routeAuthLabel renders a RouteAuth as the golden file's stable label.
func routeAuthLabel(a RouteAuth) string {
	switch a {
	case RouteAuthPublic:
		return "public"
	case RouteAuthSession:
		return "session"
	case RouteAuthPermission:
		return "permission"
	default:
		return "unknown"
	}
}

// formatRoutePermissionLine renders one Routes() entry in the golden file's
// stable, greppable format: "METHOD PATTERN CLASS RESOURCE:ACTION". RESOURCE
// and ACTION are "-" for public/session routes, which carry none.
func formatRoutePermissionLine(spec RouteSpec) string {
	resource, action := spec.Resource, spec.Action
	if resource == "" {
		resource = "-"
	}
	if action == "" {
		action = "-"
	}
	return fmt.Sprintf("%s %s %s %s:%s", spec.Method, spec.Pattern, routeAuthLabel(spec.Auth), resource, action)
}

// TestRoutes_FrozenPermissionsGolden pins Routes()'s FULL, EXACT
// authorization surface — every route's method, pattern, auth class, and
// (for permission routes) resource:action — against
// testdata/route_permissions.golden. This is the frozen counterpart to the
// live PEP mapper: it does not call authz_pep_mapping.go at all, so it keeps
// passing even after the mapper is deleted at cutover, and any route added,
// renamed, or re-permissioned without updating routes_permissions_generated.go
// and this golden file fails here.
//
// BOTH UPDATES ARE BY HAND. `go generate ./pkg/api/rest` was the procedure
// while scripts/genroutepermissions existed; that generator was deleted with
// the PEP mapper it derived from at the authz cutover, and the instruction
// outlived the tool. It is corrected here because it sent at least one author
// to a command that no longer exists, at the moment they were adding a route
// to a security-relevant table.
func TestRoutes_FrozenPermissionsGolden(t *testing.T) {
	var got []string
	for _, spec := range Routes() {
		got = append(got, formatRoutePermissionLine(spec))
	}
	sort.Strings(got)

	// Base plus whatever the build's feature tags contribute; see
	// readGoldenLines (routes_golden_test.go) for why this is not one file.
	want := readGoldenLines(t, "route_permissions")

	if len(got) != len(want) {
		t.Errorf("route permission surface size: got %d, want %d (update testdata/route_permissions.golden by hand — "+
			"and if the route is behind a feature tag, its testdata/route_permissions_<tag>.golden fragment instead)",
			len(got), len(want))
	}
	wantSet := make(map[string]struct{}, len(want))
	for _, w := range want {
		wantSet[w] = struct{}{}
	}
	gotSet := make(map[string]struct{}, len(got))
	for _, g := range got {
		gotSet[g] = struct{}{}
	}
	for g := range gotSet {
		if _, ok := wantSet[g]; !ok {
			t.Errorf("route permission not in golden surface: %s", g)
		}
	}
	for w := range wantSet {
		if _, ok := gotSet[w]; !ok {
			t.Errorf("golden route permission missing from Routes(): %s", w)
		}
	}
}

// TestModuleRoutes_ExcludesInitOnly proves ModuleRoutes() (the table
// pkg/apimodule module_http.go's RegisterHTTP consumes) drops every
// /api/v1/init/* entry that Routes() carries, KEEPS every /api/v1/barrier/*
// entry, and leaves every other route untouched.
//
// go-quicbaas#405: barrier used to be excluded too, on the premise that the
// legacy in-process listener kept serving it. That listener no longer binds
// (lifecycle_rest.go startRESTServer: REST network serving runs exclusively
// on the unified server), so the exclusion left the canonical barrier
// surface to the co-hosted xkms module, whose host-inert barrier handler
// answered `503 rest: barrier not configured` on a node whose barrier was
// configured and unsealed. The composer now drops the xkms module's barrier
// registrations instead (pkg/apimodule route_precedence.go).
func TestModuleRoutes_ExcludesInitOnly(t *testing.T) {
	all := Routes()
	module := ModuleRoutes()

	var excludedFromAll, barrierInAll int
	for _, spec := range all {
		if strings.HasPrefix(spec.Pattern, "/api/v1/init/") || qrdbNativeWireRoutes[spec.Method+" "+spec.Pattern] {
			excludedFromAll++
		}
		if strings.HasPrefix(spec.Pattern, "/api/v1/barrier/") {
			barrierInAll++
		}
	}
	if excludedFromAll == 0 {
		t.Fatal("sanity: Routes() must still carry /api/v1/init/* routes")
	}
	if barrierInAll == 0 {
		t.Fatal("sanity: Routes() must still carry /api/v1/barrier/* routes")
	}
	if got, want := len(all)-len(module), excludedFromAll; got != want {
		t.Errorf("ModuleRoutes() size delta: got %d fewer than Routes(), want %d", got, want)
	}

	moduleSet := make(map[string]struct{}, len(module))
	for _, spec := range module {
		moduleSet[spec.Method+" "+spec.Pattern] = struct{}{}
	}
	for _, spec := range all {
		key := spec.Method + " " + spec.Pattern
		excluded := strings.HasPrefix(spec.Pattern, "/api/v1/init/") || qrdbNativeWireRoutes[key]
		_, present := moduleSet[key]
		if excluded && present {
			t.Errorf("ModuleRoutes() must NOT carry %s (module-excluded: init, or a wire route go-qrdb serves natively)", key)
		}
		if !excluded && !present {
			t.Errorf("ModuleRoutes() dropped non-excluded route %s", key)
		}
	}

	// The go-quicbaas#405 acceptance, stated positively: the module surface
	// -- the ONLY REST surface a composed node binds -- carries the whole
	// canonical barrier verb set.
	for _, want := range []string{
		"GET /api/v1/barrier/status",
		"POST /api/v1/barrier/unseal",
		"POST /api/v1/barrier/seal",
		"POST /api/v1/barrier/rekey",
		"POST /api/v1/barrier/recovery/generate",
		"POST /api/v1/barrier/recovery/recover",
		"GET /api/v1/barrier/recovery/status",
		"DELETE /api/v1/barrier/recovery",
		"POST /api/v1/barrier/root-token",
		"POST /api/v1/barrier/initialize",
		"POST /api/v1/barrier/shamir/initialize",
		"POST /api/v1/barrier/shamir/unseal-share",
		"POST /api/v1/barrier/shamir/unseal-shares",
		"POST /api/v1/barrier/enable-encryption",
	} {
		if _, ok := moduleSet[want]; !ok {
			t.Errorf("ModuleRoutes() must carry %s (go-quicbaas#405)", want)
		}
	}
}

// TestLegacyListener_StillServesBarrierAndInit is the item-3 acceptance
// proof: the standalone in-process listener (registerRoutes, server.go),
// which is unaffected by ModuleRoutes filtering the module-only surface
// (pkg/apimodule), still mounts every /api/v1/barrier/* and /api/v1/init/*
// route from the SAME GroupAPI table registerRoutes has always consumed.
// This is the sealed-window boot path's dependency: it must keep working on
// a standalone (non-composed) node even after P3.3 excludes those paths
// from the unified-server module surface.
func TestLegacyListener_StillServesBarrierAndInit(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(testutil.NewMockQuicBaaS(), DefaultConfig(), log)
	if err != nil {
		t.Fatal(err)
	}

	served := map[string]struct{}{}
	walkErr := chi.Walk(srv.router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		served[method+" "+route] = struct{}{}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}

	var checked int
	for _, spec := range Routes() {
		if !strings.HasPrefix(spec.Pattern, "/api/v1/barrier/") && !strings.HasPrefix(spec.Pattern, "/api/v1/init/") {
			continue
		}
		checked++
		if _, ok := served[spec.Method+" "+spec.Pattern]; !ok {
			t.Errorf("legacy listener no longer serves %s %s", spec.Method, spec.Pattern)
		}
	}
	if checked == 0 {
		t.Fatal("sanity: expected at least one /api/v1/barrier|init/* route in Routes()")
	}
}
