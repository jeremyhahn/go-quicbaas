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

// This file is the REST Policy Enforcement Point (PEP) mapping: it translates an
// HTTP (path, method) pair into the qrdb-canonical (resource, action) pair whose
// colon join is the permission the platformtoken validator enforces against the
// token's scp claim.
//
// qrdb's permission registry uses colon-joined "resource:action" permissions
// (e.g. "kv:get", "keys:create", "tenant_barrier:init"), so this mapper
// must emit a resource and an action whose join is a REGISTERED qrdb permission.
// A coarse read/write that does not correspond to a registered action would be
// denied by qrdb, so the mapping is per (path-prefix, method) and intentionally
// explicit.
//
// Resource names are qrdb-canonical, NOT the broker names the legacy
// extractAuthzResource emitted. The Stage-0 correspondence:
//
//	legacy broker name      ->  qrdb-canonical resource
//	-------------------------------------------------------
//	keys (KV data plane)    ->  kv          (kv:get / kv:put / kv:delete / kv:list)
//	kms / crypto            ->  keys        (cryptographic key operations)
//	certificate             ->  certs
//	secrets / credential    ->  credentials
//	barrier                 ->  tenant_barrier / seal (per-op granular)
//	iam users               ->  users
//	iam roles               ->  roles
//	iam tenants             ->  tenants
//	timeseries              ->  timeseries
//	vector                  ->  vector
//	watch                   ->  watch
//	transactions            ->  transaction
//	backups                 ->  backup
//
// Stage 0.5 additionally maps the broker-only data-plane surfaces that qrdb now
// registers a permission family for (go-qrdb/pkg/permission/broker_dataplane_scope.go):
// object storage (objstore), orchestrator, messaging, scheduler, parameters,
// dynamic_secrets, project, ollama. These previously returned ok=false and were
// blanket-denied under RequireAuthz; they now resolve to a registered qrdb perm.
//
// Barrier surface (Stage 1): both /api/v1/barrier and the qrdb-SDK root-mount
// /v1/barrier carry per-operation granular permissions that mirror the gRPC
// canon (pkg/api/grpc/authz_mapping.go lines 259-276). The 13 registered
// operations are encoded in barrierGranularOps and wired into pepSpecialRules
// for both prefixes so that POST /barrier/rekey authorizes as seal:rekey (not
// the coarse tenant_barrier:init a flat-prefix entry would produce).
// pepMapping retains a coarse-fallback entry for both prefixes so any future
// unregistered sub-paths degrade to a mapped (not unmapped/denied) posture.
//
// Unmapped surfaces return ok=false; the caller decides the posture for those
// (see the middleware). This keeps the mapping a closed, registered set rather
// than guessing an action that qrdb would reject.

// methodActions maps an HTTP method to the qrdb action verb for a given
// resource family. Each resource family that follows the standard
// read/create/delete REST shape reuses one of these tables.

// crudActions is the standard mapping for resources whose qrdb verbs are
// get/create/delete/list — used by the KV data plane and the IAM management
// surfaces. The collection vs item distinction (list vs get) is resolved by the
// caller from the path shape.
type crudVerbs struct {
	// get is the action for a GET on a single item.
	get string
	// list is the action for a GET on a collection.
	list string
	// create is the action for POST/PUT/PATCH.
	create string
	// del is the action for DELETE.
	del string
}

// pepResource describes how one REST path prefix maps onto a qrdb resource.
type pepResource struct {
	// resource is the qrdb-canonical resource name.
	resource string
	// verbs is the method->action mapping for this resource.
	verbs crudVerbs
}

// barrierGranularOp encodes one granular barrier operation: the path suffix
// that uniquely identifies the endpoint, the HTTP method, and the qrdb-canonical
// (resource, action). The scope string is "resource:action" for use by the
// default-deny gate without an extra allocation.
type barrierGranularOp struct {
	suffix   string
	method   string
	resource string
	action   string
	scope    string // resource + ":" + action, pre-computed
}

// barrierGranularOps is the single source of truth for all 13 barrier
// operations registered on both /api/v1/barrier and /v1/barrier. The table
// mirrors the gRPC canon in pkg/api/grpc/authz_mapping.go lines 259-276.
// Suffixes are ordered longest-first so that the tier-2 suffix matching
// (HasSuffix) resolves ambiguities correctly — e.g. "/shamir/initialize"
// (len 19) beats "/initialize" (len 11) for the same path.
var barrierGranularOps = []barrierGranularOp{
	// Longest suffixes first to guarantee correct tier-2 wins.
	{"/shamir/initialize", http.MethodPost, "seal", "initialize_shamir", "seal:initialize_shamir"},
	{"/shamir/unseal-shares", http.MethodPost, "barrier", "unseal-share", "barrier:unseal-share"},
	{"/shamir/unseal-share", http.MethodPost, "barrier", "unseal-share", "barrier:unseal-share"},
	{"/recovery/generate", http.MethodPost, "seal", "generate_recovery", "seal:generate_recovery"},
	{"/recovery/recover", http.MethodPost, "seal", "recover_with_keys", "seal:recover_with_keys"},
	{"/recovery/status", http.MethodGet, "seal", "status", "seal:status"},
	{"/recovery", http.MethodDelete, "seal", "generate_recovery", "seal:generate_recovery"},
	{"/root-token", http.MethodPost, "seal", "bootstrap", "seal:bootstrap"},
	{"/rekey", http.MethodPost, "seal", "rekey", "seal:rekey"},
	{"/initialize", http.MethodPost, "tenant_barrier", "init", "tenant_barrier:init"},
	{"/unseal", http.MethodPost, "tenant_barrier", "unseal", "tenant_barrier:unseal"},
	{"/seal", http.MethodPost, "tenant_barrier", "seal", "tenant_barrier:seal"},
	{"/status", http.MethodGet, "tenant_barrier", "status", "tenant_barrier:status"},
}

// barrierPrefixes lists every path prefix for the barrier surface — both the
// canonical API mount and the qrdb-SDK root-wire mount.
var barrierPrefixes = []string{
	"/api/v1/barrier",
	"/v1/barrier",
}

// buildBarrierSpecialRules generates the pepSpecialRule entries for the barrier
// surface from barrierGranularOps × barrierPrefixes. Each (prefix, op) pair
// becomes one pepSpecialRule, keeping the 13-op source of truth in a single
// slice and avoiding 26-entry hand-written duplication.
func buildBarrierSpecialRules() []pepSpecialRule {
	rules := make([]pepSpecialRule, 0, len(barrierPrefixes)*len(barrierGranularOps))
	for _, prefix := range barrierPrefixes {
		for _, op := range barrierGranularOps {
			rules = append(rules, pepSpecialRule{
				method:   op.method,
				prefix:   prefix,
				suffix:   op.suffix,
				resource: op.resource,
				action:   op.action,
			})
		}
	}
	return rules
}

// pepMapping is the table-driven REST PEP. Keys are /api/v1 path prefixes; the
// longest matching prefix wins. Every (resource, action) pair produced here is
// a registered qrdb permission (see go-qrdb/pkg/permission/constants.go).
var pepMapping = map[string]pepResource{
	// KV data plane: /api/v1/keys and /api/v1/kv both address key-value data;
	// the entity store and entity-type surfaces are structured views over the
	// same KV data plane (matching the defaultDenyScopeTable kv mapping).
	"/api/v1/keys":         {resource: "kv", verbs: crudVerbs{get: "get", list: "list", create: "put", del: "delete"}},
	"/api/v1/kv":           {resource: "kv", verbs: crudVerbs{get: "get", list: "list", create: "put", del: "delete"}},
	"/api/v1/entities":     {resource: "kv", verbs: crudVerbs{get: "get", list: "list", create: "put", del: "delete"}},
	"/api/v1/entity-types": {resource: "kv", verbs: crudVerbs{get: "get", list: "list", create: "put", del: "delete"}},

	// Cryptographic key operations.
	"/api/v1/kms":    {resource: "keys", verbs: crudVerbs{get: "get", list: "list", create: "create", del: "delete"}},
	"/api/v1/crypto": {resource: "keys", verbs: crudVerbs{get: "get", list: "list", create: "create", del: "delete"}},

	// Certificate management.
	"/api/v1/certificate":  {resource: "certs", verbs: crudVerbs{get: "read", list: "list", create: "generate", del: "revoke"}},
	"/api/v1/certificates": {resource: "certs", verbs: crudVerbs{get: "read", list: "list", create: "generate", del: "revoke"}},

	// Secret / named-credential rotation.
	"/api/v1/secrets": {resource: "credentials", verbs: crudVerbs{get: "retrieve", list: "list", create: "store", del: "delete"}},

	// Tenant barrier lifecycle — coarse fallback for both mounts. The 13
	// registered operations resolve via pepSpecialRules (barrierGranularOps)
	// before this fallback is consulted, so the fallback only fires for any
	// future sub-paths not yet in barrierGranularOps.
	"/api/v1/barrier": {resource: "tenant_barrier", verbs: crudVerbs{get: "status", list: "status", create: "init", del: "seal"}},
	"/v1/barrier":     {resource: "tenant_barrier", verbs: crudVerbs{get: "status", list: "status", create: "init", del: "seal"}},

	// IAM management surfaces.
	"/api/v1/users":   {resource: "users", verbs: crudVerbs{get: "read", list: "list", create: "create", del: "delete"}},
	"/api/v1/roles":   {resource: "roles", verbs: crudVerbs{get: "read", list: "list", create: "create", del: "delete"}},
	"/api/v1/tenants": {resource: "tenants", verbs: crudVerbs{get: "read", list: "list", create: "create", del: "delete"}},

	// Data-service surfaces.
	"/api/v1/timeseries":   {resource: "timeseries", verbs: crudVerbs{get: "query", list: "query", create: "write", del: "delete_series"}},
	"/api/v1/watch":        {resource: "watch", verbs: crudVerbs{get: "watch", list: "watch", create: "watch", del: "watch"}},
	"/api/v1/transactions": {resource: "transaction", verbs: crudVerbs{get: "execute", list: "execute", create: "execute", del: "execute"}},
	"/api/v1/transaction":  {resource: "transaction", verbs: crudVerbs{get: "execute", list: "execute", create: "execute", del: "execute"}},
	"/api/v1/backups":      {resource: "backup", verbs: crudVerbs{get: "read", list: "list", create: "create", del: "delete"}},

	// ---- Stage 0.5: broker-only data-plane surfaces (qrdb-registered) ----

	// Object storage. The bare /objects/* CRUD shape maps onto objstore verbs;
	// non-CRUD object actions and admin sub-paths are handled by pepSpecialRules.
	"/api/v1/objects":         {resource: "objstore", verbs: crudVerbs{get: "get", list: "list", create: "put", del: "delete"}},
	"/api/v1/object-backends": {resource: "objstore", verbs: crudVerbs{get: "read_backend", list: "read_backend", create: "admin", del: "admin"}},
	"/api/v1/object-transfer": {resource: "objstore", verbs: crudVerbs{get: "get", list: "get", create: "get", del: "delete"}},

	// Orchestrator. The most common CRUD-ish entry points map here; the
	// sub-action POSTs (claim/complete/decompose/...) are in pepSpecialRules.
	"/api/v1/orchestrator": {resource: "orchestrator", verbs: crudVerbs{get: "status", list: "list", create: "spawn", del: "close"}},

	// Messaging. Channel CRUD; publish/subscribe live in pepSpecialRules.
	"/api/v1/messaging": {resource: "messaging", verbs: crudVerbs{get: "channel_read", list: "channel_list", create: "channel_create", del: "channel_delete"}},

	// Scheduler. Job CRUD; lifecycle sub-actions live in pepSpecialRules.
	"/api/v1/scheduler": {resource: "scheduler", verbs: crudVerbs{get: "read", list: "list", create: "create", del: "delete"}},

	// Parameter store.
	"/api/v1/parameters": {resource: "parameters", verbs: crudVerbs{get: "read", list: "list", create: "write", del: "delete"}},

	// Dynamic secrets (mounted at /api/v1/secrets/dynamic).
	"/api/v1/secrets/dynamic": {resource: "dynamic_secrets", verbs: crudVerbs{get: "role_read", list: "role_list", create: "role_create", del: "role_delete"}},

	// Project + membership.
	"/api/v1/projects": {resource: "project", verbs: crudVerbs{get: "read", list: "list", create: "create", del: "delete"}},

	// Ollama embedding inference.
	"/api/v1/ollama": {resource: "ollama", verbs: crudVerbs{get: "health", list: "model_list", create: "embed", del: "embed"}},
}

// pepSpecialRule overrides the CRUD-shaped pepMapping for a non-CRUD endpoint
// whose action cannot be inferred from the HTTP method alone (e.g. a POST that
// triggers a job rather than creating one). Rules are matched by HTTP method and
// a path suffix (after stripping a trailing slash); the first match in
// pepSpecialRules wins, and the suffix list is ordered most-specific-first so a
// sub-path like ".../jobs/{id}/trigger" is checked before the bare CRUD shape.
type pepSpecialRule struct {
	// method is the HTTP method this rule applies to.
	method string
	// prefix is the /api/v1 path prefix that scopes this rule to one surface.
	prefix string
	// suffix is matched against the request path (a request matches when its
	// path ends with suffix). An empty suffix matches any path under prefix.
	suffix string
	// resource is the qrdb-canonical resource name.
	resource string
	// action is the qrdb-canonical action.
	action string
}

// pepSpecialRulesBase holds the broker data-plane endpoints whose action is not
// a plain CRUD verb. It is combined with the barrier-granular rules
// (buildBarrierSpecialRules) into pepSpecialRules at package init time so the
// barrier surface has a single authoritative source of truth for both the PEP
// mapping and the default-deny gate (see authz_default_deny.go).
// Each (resource, action) here is a registered qrdb permission
// (go-qrdb/pkg/permission/broker_dataplane_scope.go).
var pepSpecialRulesBase = []pepSpecialRule{
	// ---- Scheduler job lifecycle sub-actions ----
	{http.MethodGet, "/api/v1/scheduler", "/jobs/", "scheduler", "read"},
	{http.MethodGet, "/api/v1/scheduler", "/jobs", "scheduler", "list"},
	{http.MethodPost, "/api/v1/scheduler", "/jobs", "scheduler", "create"},
	{http.MethodDelete, "/api/v1/scheduler", "/jobs/", "scheduler", "delete"},
	{http.MethodPost, "/api/v1/scheduler", "/trigger", "scheduler", "trigger"},
	{http.MethodPost, "/api/v1/scheduler", "/pause", "scheduler", "pause"},
	{http.MethodPost, "/api/v1/scheduler", "/resume", "scheduler", "resume"},
	{http.MethodPost, "/api/v1/scheduler", "/cancel", "scheduler", "cancel"},
	{http.MethodGet, "/api/v1/scheduler", "/history", "scheduler", "history"},
	{http.MethodGet, "/api/v1/scheduler", "/status", "scheduler", "status"},
	{http.MethodPut, "/api/v1/scheduler", "", "scheduler", "update"},

	// ---- Messaging publish / subscribe ----
	{http.MethodGet, "/api/v1/messaging", "/channels/", "messaging", "channel_read"},
	{http.MethodGet, "/api/v1/messaging", "/channels", "messaging", "channel_list"},
	{http.MethodPost, "/api/v1/messaging", "/channels", "messaging", "channel_create"},
	{http.MethodDelete, "/api/v1/messaging", "/channels/", "messaging", "channel_delete"},
	{http.MethodPost, "/api/v1/messaging", "/subscribe", "messaging", "subscribe"},
	{http.MethodDelete, "/api/v1/messaging", "/subscribe", "messaging", "unsubscribe"},
	{http.MethodPost, "/api/v1/messaging", "/publish", "messaging", "publish"},
	{http.MethodPost, "/api/v1/messaging", "/publish-delta", "messaging", "publish"},
	{http.MethodGet, "/api/v1/messaging", "/stats", "messaging", "stats"},
	{http.MethodGet, "/api/v1/messaging", "/dedup/stats", "messaging", "stats"},

	// ---- Orchestrator sub-actions ----
	{http.MethodPost, "/api/v1/orchestrator", "/prompt", "orchestrator", "prompt"},
	{http.MethodPost, "/api/v1/orchestrator", "/claim", "orchestrator", "task_claim"},
	{http.MethodPost, "/api/v1/orchestrator", "/complete", "orchestrator", "task_complete"},
	{http.MethodPost, "/api/v1/orchestrator", "/decompose", "orchestrator", "task_decompose"},
	{http.MethodPost, "/api/v1/orchestrator", "/heartbeat", "orchestrator", "agent_heartbeat"},
	{http.MethodPost, "/api/v1/orchestrator", "/children", "orchestrator", "job_start"},
	{http.MethodGet, "/api/v1/orchestrator", "/events", "orchestrator", "status"},
	{http.MethodGet, "/api/v1/orchestrator", "/artifacts", "orchestrator", "artifact_read"},
	{http.MethodPost, "/api/v1/orchestrator", "/artifacts", "orchestrator", "artifact_submit"},
	{http.MethodGet, "/api/v1/orchestrator", "/health", "orchestrator", "health"},
	{http.MethodGet, "/api/v1/orchestrator", "/backends", "orchestrator", "backend_list"},
	{http.MethodGet, "/api/v1/orchestrator", "/workflows", "orchestrator", "workflow_read"},
	{http.MethodPost, "/api/v1/orchestrator", "/workflows", "orchestrator", "workflow_write"},
	{http.MethodPut, "/api/v1/orchestrator", "/workflows", "orchestrator", "workflow_write"},
	{http.MethodDelete, "/api/v1/orchestrator", "/workflows", "orchestrator", "workflow_write"},
	{http.MethodPost, "/api/v1/orchestrator", "/discover", "orchestrator", "discover"},
	{http.MethodPost, "/api/v1/orchestrator", "/hooks", "orchestrator", "hook"},
	{http.MethodPost, "/api/v1/orchestrator", "/inference", "orchestrator", "inference"},
	{http.MethodPost, "/api/v1/orchestrator", "/sessions", "orchestrator", "spawn"},
	{http.MethodPost, "/api/v1/orchestrator", "/tasks", "orchestrator", "task_create"},
	{http.MethodPost, "/api/v1/orchestrator", "/jobs", "orchestrator", "job_start"},
	{http.MethodPost, "/api/v1/orchestrator", "/agents", "orchestrator", "agent_register"},

	// ---- Object storage non-CRUD actions ----
	{http.MethodPost, "/api/v1/objects", "/batch/get", "objstore", "batch"},
	{http.MethodPost, "/api/v1/objects", "/batch/delete", "objstore", "batch"},
	{http.MethodPost, "/api/v1/objects", "/batch", "objstore", "batch"},
	{http.MethodHead, "/api/v1/objects", "", "objstore", "head"},
	{http.MethodPut, "/api/v1/objects", "/quotas", "objstore", "tenant_quota_write"},
	{http.MethodGet, "/api/v1/objects", "/quotas", "objstore", "tenant_quota_read"},
	{http.MethodGet, "/api/v1/objects", "/usage", "objstore", "tenant_usage"},
	{http.MethodGet, "/api/v1/object-backends", "/health", "objstore", "read_backend"},
	{http.MethodPost, "/api/v1/object-transfer", "/fetch", "objstore", "get"},
	{http.MethodPost, "/api/v1/object-transfer", "/stream-fetch", "objstore", "get"},

	// ---- Dynamic secrets generation + lease lifecycle ----
	{http.MethodGet, "/api/v1/secrets/dynamic", "/roles/", "dynamic_secrets", "role_read"},
	{http.MethodGet, "/api/v1/secrets/dynamic", "/roles", "dynamic_secrets", "role_list"},
	{http.MethodPost, "/api/v1/secrets/dynamic", "/roles", "dynamic_secrets", "role_create"},
	{http.MethodDelete, "/api/v1/secrets/dynamic", "/roles/", "dynamic_secrets", "role_delete"},
	{http.MethodPost, "/api/v1/secrets/dynamic", "/renew", "dynamic_secrets", "lease_renew"},
	{http.MethodDelete, "/api/v1/secrets/dynamic", "/leases/", "dynamic_secrets", "lease_revoke"},
	{http.MethodGet, "/api/v1/secrets/dynamic", "/leases", "dynamic_secrets", "lease_list"},
	{http.MethodPost, "/api/v1/secrets/dynamic", "/credentials/", "dynamic_secrets", "generate"},

	// ---- Parameter store batch / search ----
	{http.MethodPost, "/api/v1/parameters", "/batch/get", "parameters", "read"},
	{http.MethodPost, "/api/v1/parameters", "/batch/set", "parameters", "write"},
	{http.MethodPost, "/api/v1/parameters", "/batch/delete", "parameters", "delete"},
	{http.MethodPost, "/api/v1/parameters", "/search", "parameters", "search"},

	// ---- Project membership grant / revoke ----
	{http.MethodPut, "/api/v1/projects", "/permissions/", "project", "grant"},
	{http.MethodDelete, "/api/v1/projects", "/permissions/", "project", "revoke"},
	{http.MethodPut, "/api/v1/projects", "", "project", "update"},

	// ---- Ollama embedding ----
	{http.MethodPost, "/api/v1/ollama", "/embed/batch", "ollama", "embed"},
	{http.MethodPost, "/api/v1/ollama", "/embed", "ollama", "embed"},
	// "/models/" (item) is longer than "/models" (collection) so model_info
	// wins for /models/{model} while /models stays model_list.
	{http.MethodGet, "/api/v1/ollama", "/models/", "ollama", "model_info"},
	{http.MethodGet, "/api/v1/ollama", "/models", "ollama", "model_list"},
	{http.MethodGet, "/api/v1/ollama", "/health", "ollama", "health"},

	// ---- Per-tenant barrier lifecycle ----
	// These leaf actions (tier 2) win over the surface-wide "/api/v1/tenants"
	// prefix mapping (resource "tenants"), so per-tenant barrier ops authorize
	// as the tenant_barrier resource. unseal-share maps to the unseal action.
	{http.MethodPost, "/api/v1/tenants", "/barrier/init", "tenant_barrier", "init"},
	{http.MethodPost, "/api/v1/tenants", "/barrier/unseal-share", "tenant_barrier", "unseal"},
	{http.MethodPost, "/api/v1/tenants", "/barrier/unseal", "tenant_barrier", "unseal"},
	{http.MethodPost, "/api/v1/tenants", "/barrier/seal", "tenant_barrier", "seal"},
	{http.MethodGet, "/api/v1/tenants", "/barrier/status", "tenant_barrier", "status"},
}

// pepSpecialRules is the active special-rule list consulted by
// matchSpecialRule. It is the union of pepSpecialRulesBase (all non-barrier
// broker surfaces) and the barrier-granular rules for both /api/v1/barrier and
// /v1/barrier generated from barrierGranularOps. Combining them here keeps
// a single source of truth for the 13 barrier operations.
var pepSpecialRules = func() []pepSpecialRule {
	barrier := buildBarrierSpecialRules()
	combined := make([]pepSpecialRule, 0, len(pepSpecialRulesBase)+len(barrier))
	combined = append(combined, pepSpecialRulesBase...)
	combined = append(combined, barrier...)
	return combined
}()

// mapRequestToPermission translates a request (path, method) into the
// qrdb-canonical (resource, action). ok is false when no canonical mapping
// exists for the path; callers decide the posture for unmapped surfaces.
//
// pepSpecialRules are consulted first so non-CRUD endpoints (job triggers,
// publishes, batch operations, ...) resolve to their tailored action; the
// CRUD-shaped pepMapping is the fallback for the standard resource shapes.
//
// The collection-vs-item distinction is structural: a GET on the bare resource
// prefix (e.g. /api/v1/keys) is a list; a GET on a sub-path (e.g.
// /api/v1/keys/{id}) is a get. This matches qrdb's distinct list vs get
// permissions.
func mapRequestToPermission(path, method string) (resource, action string, ok bool) {
	normalized := strings.TrimSuffix(path, "/")

	if r, a, found := matchSpecialRule(normalized, method); found {
		return r, a, true
	}

	prefix, entry, found := longestPEPPrefix(normalized)
	if !found {
		return "", "", false
	}

	isCollection := normalized == prefix
	action, ok = actionForMethod(method, entry.verbs, isCollection)
	if !ok {
		return "", "", false
	}
	return entry.resource, action, true
}

// matchSpecialRule returns the most specific pepSpecialRule whose method matches
// and whose prefix scopes the path. Matches are ranked so that the deepest,
// most-specific endpoint action wins:
//
//	tier 2 — the rule suffix is the END of the path (an exact endpoint action,
//	         e.g. ".../jobs/{id}/trigger" ends with "/trigger");
//	tier 1 — the rule suffix appears mid-path as a path segment (a sub-resource
//	         collection, e.g. ".../sessions/{id}/prompt" contains "/sessions/");
//	tier 0 — the rule has an empty suffix (the surface-wide fallback).
//
// Within a tier the longer suffix wins. This guarantees a leaf action like
// "prompt" beats a containing sub-resource like "sessions" for the same path.
func matchSpecialRule(path, method string) (resource, action string, ok bool) {
	var (
		bestTier  = -1
		bestLen   = -1
		bestRes   string
		bestAct   string
		bestFound bool
	)
	for _, rule := range pepSpecialRules {
		if !strings.EqualFold(rule.method, method) {
			continue
		}
		if path != rule.prefix && !strings.HasPrefix(path, rule.prefix+"/") {
			continue
		}
		tier := specialRuleTier(path, rule.suffix)
		if tier < 0 {
			continue
		}
		if tier > bestTier || (tier == bestTier && len(rule.suffix) > bestLen) {
			bestTier = tier
			bestLen = len(rule.suffix)
			bestRes = rule.resource
			bestAct = rule.action
			bestFound = true
		}
	}
	return bestRes, bestAct, bestFound
}

// specialRuleTier ranks how a rule suffix matches a path: 2 = path ends with
// suffix (leaf endpoint action), 1 = suffix appears as a mid-path segment,
// 0 = empty suffix (surface fallback), -1 = no match.
func specialRuleTier(path, suffix string) int {
	if suffix == "" {
		return 0
	}
	if strings.HasSuffix(path, suffix) {
		return 2
	}
	if strings.Contains(path, suffix) {
		return 1
	}
	return -1
}

// longestPEPPrefix returns the longest /api/v1 prefix in pepMapping that matches
// path, along with its mapping entry.
func longestPEPPrefix(path string) (string, pepResource, bool) {
	var (
		bestPrefix string
		bestEntry  pepResource
		found      bool
	)
	for prefix, entry := range pepMapping {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			if len(prefix) > len(bestPrefix) {
				bestPrefix = prefix
				bestEntry = entry
				found = true
			}
		}
	}
	return bestPrefix, bestEntry, found
}

// actionForMethod resolves the qrdb action for an HTTP method against a
// resource's verb table. A GET on a collection maps to list; a GET on an item
// maps to get. ok is false for any HTTP method outside the registered set
// (e.g. TRACE / CONNECT): the PEP is a closed, registered set, so an unknown
// verb has no canonical action and must be treated as unmapped -> denied, never
// silently coerced to a read.
func actionForMethod(method string, verbs crudVerbs, isCollection bool) (action string, ok bool) {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		if isCollection {
			return verbs.list, true
		}
		return verbs.get, true
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return verbs.create, true
	case http.MethodDelete:
		return verbs.del, true
	default:
		return "", false
	}
}
