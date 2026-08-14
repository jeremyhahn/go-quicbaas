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

// barrierGranularOps is the single source of truth for all barrier operations
// registered on both /api/v1/barrier and /v1/barrier. The table mirrors the
// gRPC canon in pkg/api/grpc/authz_mapping.go. Suffixes are ordered
// longest-first so that the tier-2 suffix matching (HasSuffix) resolves
// ambiguities correctly — e.g. "/shamir/initialize" (len 19) beats
// "/initialize" (len 11) for the same path.
var barrierGranularOps = []barrierGranularOp{
	// Longest suffixes first to guarantee correct tier-2 wins.
	{"/shamir/initialize", http.MethodPost, "seal", "initialize_shamir", "seal:initialize_shamir"},
	{"/shamir/unseal-shares", http.MethodPost, "barrier", "unseal-share", "barrier:unseal-share"},
	{"/shamir/unseal-share", http.MethodPost, "barrier", "unseal-share", "barrier:unseal-share"},
	{"/recovery/generate", http.MethodPost, "seal", "generate_recovery", "seal:generate_recovery"},
	{"/recovery/recover", http.MethodPost, "seal", "recover_with_keys", "seal:recover_with_keys"},
	{"/recovery/status", http.MethodGet, "seal", "status", "seal:status"},
	{"/enable-encryption", http.MethodPost, "seal", "enable_encryption", "seal:enable_encryption"},
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

	// Secret side-surfaces mounted as SIBLINGS of /api/v1/secrets to avoid chi
	// wildcard conflicts (server.go). They are NOT covered by the
	// "/api/v1/secrets" entry above: longestPEPPrefix requires prefix+"/", and
	// "/api/v1/secrets-versions" is not a sub-path of "/api/v1/secrets". Each
	// therefore needs its own entry or the validator gate default-denies the
	// whole surface (the GUI "view versions" action was a hard 403).
	//
	// Only GET is mounted on all three. The write verbs are still declared —
	// with the STRONGEST credentials write actions — so a future POST/DELETE
	// mount cannot silently inherit a read-strength permission.
	"/api/v1/secrets-versions": {resource: "credentials", verbs: crudVerbs{get: "list_versions", list: "list_versions", create: "store", del: "delete"}},
	"/api/v1/secrets-prefix":   {resource: "credentials", verbs: crudVerbs{get: "retrieve", list: "list", create: "store", del: "delete"}},
	"/api/v1/secrets-watch":    {resource: "credentials", verbs: crudVerbs{get: "retrieve", list: "retrieve", create: "store", del: "delete"}},

	// Trust composition surface (handlers_trust.go: cross-signed root pool, AIA
	// auto-import, DANE publication). qrdb registers a dedicated broker trust
	// family for exactly these artifacts — trust:read / trust:write
	// (go-qrdb/pkg/permission/broker_scope.go TrustRead / TrustWrite), granted
	// to the built-in Admin role — so the surface authorizes with that family
	// rather than the coarser certs:* it would otherwise borrow. Same family the
	// sibling /api/v1/bootstrap trust-anchor surface already uses.
	"/api/v1/trust": {resource: "trust", verbs: crudVerbs{get: "read", list: "read", create: "write", del: "write"}},

	// Attestation surface (handlers_attestation.go: statement verification,
	// chain verification, container attestation). It gets its own resource
	// family rather than borrowing certs:* or trust:*: the verbs answer "is
	// this host and this workload in the state we approved?", and a principal
	// allowed to read the certificate store has no business driving a TPM
	// quote or reading a host's PCR values, which fingerprint its boot state.
	//
	// Every mounted route is a POST, so this coarse entry deliberately maps
	// create to the STRONGEST action (attest). The two read-strength verbs are
	// downgraded individually in pepSpecialRules below; a future sub-path
	// added without a rule therefore inherits the strong permission rather
	// than silently becoming reachable with a read-strength one.
	"/api/v1/attestation": {resource: "attestation", verbs: crudVerbs{get: "verify", list: "verify", create: "attest", del: "attest"}},

	// ACME CONTROL PLANE (handlers_acme.go registerACMERoutes: register-challenge,
	// issue, new-order, finalize, renew). These are the QuicBaaS broker-as-CA
	// client verbs behind Security().ACME(); qrdb documents acme:write as
	// covering "ACME accounts, orders, and issuance", which is precisely this
	// surface. The RAW RFC 8555 endpoints mounted at the router ROOT (/acme/*)
	// are a different surface entirely: they authenticate per RFC 8555 (JWS +
	// nonce + account key / EAB), carry no platform-token gate, and are NOT
	// mapped here.
	"/api/v1/acme": {resource: "acme", verbs: crudVerbs{get: "read", list: "read", create: "write", del: "write"}},

	// go-xkms compatibility surface (routes_xkms.go) — lets the go-xkms SDK REST
	// transport talk to quicbaasd. Coarse fallback only: every mounted endpoint
	// resolves granularly via xkmsOps (pepSpecialRules) to the registered
	// keys:* / certs:* / ca:* / piv:* / pin:* / backends:* family for its verb.
	// The longer "/api/v1/xkms/v1/ssh" prefix below still wins for the SSH alias.
	"/api/v1/xkms": {resource: "keys", verbs: crudVerbs{get: "get", list: "list", create: "create", del: "delete"}},

	// Legacy seal lifecycle (server.go /api/v1/seal) — superseded by
	// /api/v1/barrier but still the REST projection of the Security().Seal()
	// SDK service in all three SDKs (sdk/go/transport/rest mapSealMethod,
	// sdk/typescript barrier legacy path, sdk/javascript clients/seal.js), so
	// the routes stay mounted and must authorize. The nine registered
	// operations resolve via legacySealOps (pepSpecialRules); this coarse entry
	// mirrors the barrierScopeTable fallback for unregistered sub-paths.
	"/api/v1/seal": {resource: "tenant_barrier", verbs: crudVerbs{get: "status", list: "status", create: "seal", del: "seal"}},

	// Atomic batch data plane (/api/v1/batch/{put,get,delete}). qrdb registers a
	// dedicated batch:execute permission and the gRPC canon uses it for every
	// Batch* method (pkg/api/grpc/authz_mapping.go), so REST authorizes identically.
	"/api/v1/batch": {resource: "batch", verbs: crudVerbs{get: "execute", list: "execute", create: "execute", del: "execute"}},

	// Secondary-index query plane. GET /query/{type}/{index} calls
	// service.Query, which the gRPC canon authorizes as kv:range; POST
	// /query/range calls service.ScanIndex, authorized as index:scan via the
	// special rule below. Mirroring the gRPC canon keeps a permission grant
	// equivalent across transports.
	"/api/v1/query": {resource: "kv", verbs: crudVerbs{get: "range", list: "range", create: "range", del: "range"}},

	// Tenant barrier lifecycle — coarse fallback for both mounts. The 13
	// registered operations resolve via pepSpecialRules (barrierGranularOps)
	// before this fallback is consulted, so the fallback only fires for any
	// future sub-paths not yet in barrierGranularOps.
	"/api/v1/barrier": {resource: "tenant_barrier", verbs: crudVerbs{get: "status", list: "status", create: "init", del: "seal"}},
	"/v1/barrier":     {resource: "tenant_barrier", verbs: crudVerbs{get: "status", list: "status", create: "init", del: "seal"}},

	// qrdb-wire user lookup + user-login factor mounts at the router root —
	// same root-wire precedent as /v1/barrier above. Verbs map onto the
	// qrdb-registered users:* and user_authfactors:* permission families.
	"/users":            {resource: "users", verbs: crudVerbs{get: "read", list: "list", create: "create", del: "delete"}},
	"/v1/users/factors": {resource: "user_authfactors", verbs: crudVerbs{get: "get", list: "list", create: "add", del: "remove"}},

	// qrdb-wire versioned named-credential rotation mount at the router root
	// (server.go /v1/credentials) — same root-wire precedent as /v1/barrier.
	// The registered per-endpoint actions resolve via credentialWireOps
	// (pepSpecialRules) so REST authorizes the same credentials:* verb as the
	// gRPC qrdb.v1.QRDBService Credential* canon; this coarse entry covers
	// the collection list (GET /v1/credentials -> credentials:list), the
	// bare DELETE /{name} (credentials:delete), and any future sub-paths.
	"/v1/credentials": {resource: "credentials", verbs: crudVerbs{get: "retrieve", list: "list", create: "store", del: "delete"}},

	// Initialization-ceremony surfaces: the qrdb-wire root mounts
	// (/v1/ceremony + /v1/init, server.go) and the canonical /api/v1/init
	// mirror. The registered per-endpoint actions resolve via
	// ceremonyGranularOps (pepSpecialRules) so REST authorizes the same
	// ceremony:* verb as the gRPC quicbaas.ceremony.v1 / qrdb.v1 canon;
	// these coarse entries keep any future sub-paths mapped (degrading to
	// the same ceremony family the defaultDenyScopeTable uses) rather than
	// unmapped/denied.
	"/v1/ceremony": {resource: "ceremony", verbs: crudVerbs{get: "status", list: "status", create: "initialize", del: "initialize"}},
	"/v1/init":     {resource: "ceremony", verbs: crudVerbs{get: "status", list: "status", create: "initialize", del: "initialize"}},
	"/api/v1/init": {resource: "ceremony", verbs: crudVerbs{get: "status", list: "status", create: "initialize", del: "initialize"}},

	// RFC 7591 Dynamic Client Registration (root-mounted /oauth2/register in
	// iam.go). An OAuth2 client is an authentication principal owned by a
	// tenant, and apps are administered under the tenants surface elsewhere in
	// this table, so registering one authorizes as tenants:create. Only the
	// CREATE verb is enforced here: the RFC 7592 management verbs
	// (GET/PUT/DELETE /oauth2/register/{client_id}) authenticate solely with the
	// per-client registration access token and are declared pre-auth in
	// authz_public_routes.go, so they never reach this mapper.
	"/oauth2/register": {resource: "tenants", verbs: crudVerbs{get: "read", list: "read", create: "create", del: "delete"}},

	// IAM management surfaces.
	"/api/v1/users":   {resource: "users", verbs: crudVerbs{get: "read", list: "list", create: "create", del: "delete"}},
	"/api/v1/roles":   {resource: "roles", verbs: crudVerbs{get: "read", list: "list", create: "create", del: "delete"}},
	"/api/v1/tenants": {resource: "tenants", verbs: crudVerbs{get: "read", list: "list", create: "create", del: "delete"}},

	// Delegated-admin surface (root-mounted /api/v1/admin in iam_admin.go):
	// admin-assignment CRUD, permission checks and impersonation. An admin
	// assignment binds an administrative role to a user within a scope, so the
	// surface maps onto the qrdb roles:* family (create/update -> roles:assign,
	// delete -> roles:unassign). The non-CRUD endpoints (check-permission,
	// impersonation) resolve via the "/api/v1/admin" pepSpecialRulesBase block
	// before this coarse fallback is consulted.
	"/api/v1/admin": {resource: "roles", verbs: crudVerbs{get: "read", list: "list", create: "assign", del: "unassign"}},

	// IAM utility group (root-mounted /api/v1/iam in iam.go): sessions, audit,
	// PEP access-check and self-registration approval surfaces. Every registered
	// endpoint resolves via the "/api/v1/iam" pepSpecialRulesBase block; this
	// coarse fallback keeps any future sub-path mapped (degrading to the same
	// tenants family the defaultDenyScopeTable uses for /api/v1/iam) rather than
	// unmapped/denied. The invite-activation mount (/api/v1/iam/invites) is the
	// only pre-auth surface under this prefix; it is a separate unwrapped mount.
	"/api/v1/iam": {resource: "tenants", verbs: crudVerbs{get: "read", list: "list", create: "create", del: "delete"}},

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

	// Pooled provider accounts (/api/v1/agent-accounts). An account custodies
	// credential material (an Anthropic / Codex / OpenCode login), so it
	// authorizes with the qrdb credentials:* vocabulary — the same family the
	// /api/v1/secrets and /v1/credentials surfaces above use — rather than the
	// orchestrator:* family, which has no CRUD verbs. qrdb registers no
	// account:* family; when one is added these entries should move to it.
	//
	// The item read takes credentials:retrieve (the strongest read verb) even
	// though no account response carries secret material: over-restricting a
	// read is the safe direction, and it keeps a single "who may see this
	// account at all" grant. The non-CRUD leaf actions (rotate / revoke /
	// default / usage) are refined in pepSpecialRulesBase below.
	"/api/v1/agent-accounts": {resource: "credentials", verbs: crudVerbs{get: "retrieve", list: "list", create: "store", del: "delete"}},

	// Ollama embedding inference.
	"/api/v1/ollama": {resource: "ollama", verbs: crudVerbs{get: "health", list: "model_list", create: "embed", del: "embed"}},

	// Claude Code hook callbacks (handlers_hooks.go: task-created,
	// task-completed, teammate-idle). Each hook creates an orchestrator task and
	// publishes a messaging event on the caller's behalf, which is exactly the
	// registered orchestrator:hook capability the sibling
	// POST /api/v1/orchestrator/.../hooks rule already uses.
	"/api/v1/hooks": {resource: "orchestrator", verbs: crudVerbs{get: "hook", list: "hook", create: "hook", del: "hook"}},

	// Shard-routing introspection (env-gated debug mount, debug.go). Reads the
	// cluster's shard placement, so it authorizes as the same admin:status the
	// cluster control plane uses.
	"/api/v1/debug": {resource: "admin", verbs: crudVerbs{get: "status", list: "status", create: "status", del: "status"}},

	// Cluster + cache control plane (Raft administration). Mirrors the gRPC
	// canon in pkg/api/grpc/authz_mapping.go: reads authorize as admin:status,
	// membership writes as admin:add_member / admin:remove_member. The rebalance,
	// migrate and upgrade write actions carry finer permissions via pepSpecialRules.
	"/api/v1/cluster": {resource: "admin", verbs: crudVerbs{get: "status", list: "status", create: "add_member", del: "remove_member"}},
	"/api/v1/cache":   {resource: "admin", verbs: crudVerbs{get: "status", list: "status", create: "capabilities", del: "capabilities"}},

	// Vector data plane. The qrdb-registered vector permission family is wide;
	// the coarse CRUD shape here maps reads -> vector:get, collection reads ->
	// vector:namespace_list, writes -> vector:upsert, deletes -> vector:delete.
	// The non-CRUD search and namespace verbs resolve via pepSpecialRules.
	"/api/v1/vector": {resource: "vector", verbs: crudVerbs{get: "get", list: "namespace_list", create: "upsert", del: "delete"}},

	// Embedding generation + provider management (UI: EmbeddingsView). The SPA
	// uses the singular /api/v1/embedding surface. Reads list models/providers,
	// writes generate embeddings.
	"/api/v1/embedding": {resource: "embedding", verbs: crudVerbs{get: "list_models", list: "list_models", create: "embed", del: "embed"}},

	// Retrieval-augmented generation (UI: RAGView). Reads list/query documents,
	// writes ingest, deletes remove a document.
	"/api/v1/rag": {resource: "rag", verbs: crudVerbs{get: "query", list: "list_documents", create: "ingest", del: "delete_document"}},

	// Trust-anchor bootstrap (UI: BootstrapView). First-contact CA discovery:
	// POST /api/v1/bootstrap/{auto,dane,noise,spki} establishes a trust anchor,
	// which IS a trust-write operation -> trust:write; a future read of the
	// surface authorizes as trust:read. Both are registered qrdb broker perms
	// (go-qrdb/pkg/permission/broker_scope.go: TrustRead / TrustWrite).
	"/api/v1/bootstrap": {resource: "trust", verbs: crudVerbs{get: "read", list: "read", create: "write", del: "write"}},

	// SSH key management — proxied to the xkms SSH SDK. SSH keys are
	// cryptographic keys; these map onto the qrdb keys permission family
	// (the nearest registered permissions — no ssh:* family exists in the qrdb
	// registry yet; a future qrdb ssh:* permission grant should replace this
	// mapping). The coarse fallback here covers any future sub-paths not yet in
	// sshOps; all current endpoints resolve via buildSSHSpecialRules.
	"/api/v1/ssh":         {resource: "keys", verbs: crudVerbs{get: "get", list: "list", create: "create", del: "delete"}},
	"/api/v1/xkms/v1/ssh": {resource: "keys", verbs: crudVerbs{get: "get", list: "list", create: "create", del: "delete"}},
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

// pepOp encodes one endpoint action of a single-prefix surface: the path suffix
// that identifies the endpoint, the HTTP method, and the qrdb-canonical
// (resource, action). Surfaces whose endpoints all live under one mount (SSH,
// the go-xkms compatibility API, the legacy seal API) declare their ops as a
// []pepOp and expand them into pepSpecialRules with a build* helper, keeping one
// authoritative per-endpoint table per surface.
type pepOp struct {
	suffix   string
	method   string
	resource string
	action   string
}

// sshOps defines the per-endpoint mappings for the SSH key management surface
// (go-xkms SSH SDK). No ssh:* permission family was registered in qrdb when this
// mapping was written, so SSH key management maps to the nearest registered keys
// permissions; a future migration to the now-registered ssh:read / ssh:write /
// ssh:delete family should replace it. Suffixes are ordered longest-first so the
// tier-based matching resolves overlaps correctly (e.g. "/keys/import" beats
// "/keys/").
var sshOps = []pepOp{
	// Key CRUD, import, sign.
	{"/keys/import", http.MethodPost, "keys", "import"},
	{"/keys/", http.MethodGet, "keys", "get"},
	{"/keys/", http.MethodDelete, "keys", "delete"},
	{"/keys", http.MethodGet, "keys", "list"},
	{"/keys", http.MethodPost, "keys", "create"},
	{"/sign", http.MethodPost, "keys", "sign"},
	// Known-hosts CRUD + verify.
	{"/known-hosts/", http.MethodGet, "keys", "get"},
	{"/known-hosts/", http.MethodDelete, "keys", "delete"},
	{"/known-hosts", http.MethodGet, "keys", "list"},
	{"/known-hosts", http.MethodPost, "keys", "create"},
	// /verify matches both /known-hosts/{host}/verify and /authorized/verify.
	{"/verify", http.MethodPost, "keys", "verify"},
	// Authorized-keys emit: generating the authorized_keys file is a read/export.
	{"/authorized", http.MethodPost, "keys", "get"},
	// Authorized-keys CRUD. The longer "/authorized/keys" suffixes win over
	// the bare "/keys" key-CRUD suffixes within the same match tier, so an
	// authorized-key write never authorizes as a private-key write.
	{"/authorized/keys", http.MethodPost, "keys", "create"},
	{"/authorized/keys", http.MethodGet, "keys", "list"},
	{"/authorized/keys/", http.MethodGet, "keys", "get"},
	{"/authorized/keys/", http.MethodDelete, "keys", "delete"},
}

// sshPrefixes lists both the canonical SSH mount and the xkms-SDK-client alias.
// The alias lets CLIs set XKMSD_URL to the quicbaasd base and reuse the go-xkms
// REST SSH client unchanged (it expects the /api/v1/xkms/v1/ssh path prefix).
var sshPrefixes = []string{
	"/api/v1/ssh",
	"/api/v1/xkms/v1/ssh",
}

// xkmsPrefix is the mount point of the go-xkms compatibility surface
// (routes_xkms.go). It also prefixes the "/api/v1/xkms/v1/ssh" SSH alias; the
// SSH rules carry the longer prefix and identical (resource, action) values
// wherever a suffix is shared, so the two rule sets never disagree.
const xkmsPrefix = "/api/v1/xkms"

// xkmsOps maps every endpoint of the go-xkms compatibility surface to its
// registered qrdb permission. Each family is the one qrdb registers for the
// underlying capability rather than a single coarse keys:* blanket:
//
//	key lifecycle + crypto  -> keys:*      (create/list/get/delete/rotate/
//	                                        sign/verify/encrypt/decrypt)
//	backend inventory       -> backends:*  (list/read)
//	certificate store       -> certs:*     (generate/list/read/revoke)
//	certificate authority   -> ca:read + certs:generate / certs:revoke
//	PIV slots               -> piv:*       (read/write/delete/use)
//	PIN administration      -> pin:*       (read/write)
//	health probe            -> admin:health
//
// Envelope seal/unseal on this surface is DATA sealing (the request carries the
// plaintext), not barrier lifecycle, so it authorizes as keys:encrypt /
// keys:decrypt — never as the seal:* barrier family.
//
// Suffixes rely on the matchSpecialRule tiering: a longer suffix wins within a
// tier, so "/ca/sign" beats "/sign" and "/versions/enable-all" beats "/enable".
var xkmsOps = []pepOp{
	// Health.
	{"/health", http.MethodGet, "admin", "health"},

	// Backend inventory.
	{"/backends/", http.MethodGet, "backends", "read"},
	{"/backends", http.MethodGet, "backends", "list"},

	// Key lifecycle. "/keys/" (mid-path, longer) resolves the single-key verbs
	// while "/keys" (leaf) stays the collection.
	{"/keys/", http.MethodGet, "keys", "get"},
	{"/keys/", http.MethodDelete, "keys", "delete"},
	{"/keys", http.MethodGet, "keys", "list"},
	{"/keys", http.MethodPost, "keys", "create"},
	{"/rotate", http.MethodPost, "keys", "rotate"},

	// Key crypto operations.
	{"/sign", http.MethodPost, "keys", "sign"},
	{"/verify", http.MethodPost, "keys", "verify"},
	{"/encrypt", http.MethodPost, "keys", "encrypt"},
	{"/decrypt", http.MethodPost, "keys", "decrypt"},

	// Key versions. Enabling / disabling a version mutates key state, which is
	// the keys:rotate capability (the same verb the KMS metadata update uses).
	{"/versions", http.MethodGet, "keys", "list"},
	{"/versions/enable-all", http.MethodPost, "keys", "rotate"},
	{"/versions/disable-all", http.MethodPost, "keys", "rotate"},
	{"/enable", http.MethodPost, "keys", "rotate"},
	{"/disable", http.MethodPost, "keys", "rotate"},

	// Certificate store.
	{"/certs/", http.MethodGet, "certs", "read"},
	{"/certs/", http.MethodHead, "certs", "read"},
	{"/certs/", http.MethodDelete, "certs", "revoke"},
	{"/certs", http.MethodGet, "certs", "list"},
	{"/certs", http.MethodPost, "certs", "generate"},

	// Envelope (data) sealing.
	{"/seal/capability", http.MethodGet, "keys", "list"},
	{"/seal", http.MethodPost, "keys", "encrypt"},
	{"/unseal", http.MethodPost, "keys", "decrypt"},

	// FIDO2 ceremony (tenant-scoped WebAuthn over the xkms wire). Enrollment
	// is factor enrollment (the canonical /api/v1/iam webauthn mount
	// precedent); authentication proves an enrolled factor, a read.
	{"/fido2/register/begin", http.MethodPost, "user_authfactors", "add"},
	{"/fido2/register/finish", http.MethodPost, "user_authfactors", "add"},
	{"/fido2/authenticate/begin", http.MethodPost, "user_authfactors", "get"},
	{"/fido2/authenticate/finish", http.MethodPost, "user_authfactors", "get"},

	// PIV slots. "/cert" covers the slot certificate verbs; "/ca/cert" is
	// longer and therefore wins for the CA certificate read below.
	{"/piv/slots", http.MethodGet, "piv", "read"},
	{"/cert", http.MethodGet, "piv", "read"},
	{"/cert", http.MethodPut, "piv", "write"},
	{"/cert", http.MethodDelete, "piv", "delete"},
	{"/key", http.MethodPost, "piv", "write"},
	{"/csr", http.MethodPost, "piv", "use"},

	// Certificate authority. Reading the CA bundle/certificate is a CA read;
	// signing / issuing mints a certificate; revoke and CRL generation are the
	// revocation lifecycle.
	{"/ca/bundle", http.MethodGet, "ca", "read"},
	{"/ca/cert", http.MethodGet, "ca", "read"},
	{"/ca/revoked/", http.MethodGet, "certs", "read"},
	{"/ca/sign", http.MethodPost, "certs", "generate"},
	{"/ca/issue", http.MethodPost, "certs", "generate"},
	{"/ca/revoke", http.MethodPost, "certs", "revoke"},
	{"/ca/crl", http.MethodPost, "certs", "revoke"},

	// PIN administration. Verification reads PIN state; setting, changing and
	// unlocking mutate it. The trailing "/pin/" rules are the surface fallback
	// for any future sub-path.
	{"/pin/so/verify", http.MethodPost, "pin", "read"},
	{"/pin/user/verify", http.MethodPost, "pin", "read"},
	{"/pin/lockout", http.MethodGet, "pin", "read"},
	{"/pin/", http.MethodGet, "pin", "read"},
	{"/pin/", http.MethodPost, "pin", "write"},
}

// buildXKMSSpecialRules generates the pepSpecialRule entries for the go-xkms
// compatibility surface from xkmsOps, mirroring buildSSHSpecialRules.
func buildXKMSSpecialRules() []pepSpecialRule {
	return buildPrefixRules(xkmsPrefix, xkmsOps)
}

// legacySealPrefix is the mount point of the deprecated seal surface.
const legacySealPrefix = "/api/v1/seal"

// legacySealOps maps the legacy /api/v1/seal endpoints (server.go) to the same
// granular qrdb scopes the canonical /api/v1/barrier surface uses. The surface
// is deprecated — every handler stamps RFC 8594 Deprecation/Sunset headers —
// but NOT dead: it is the REST projection of the Security().Seal() SDK service
// in the Go, TypeScript and JavaScript SDKs, so it must authorize rather than
// be default-denied. It is the single source of truth for the legacy seal
// scopes: the default-deny gate (authz_default_deny.go) reads this same table.
var legacySealOps = []pepOp{
	{"/recovery/generate", http.MethodPost, "seal", "generate_recovery"},
	{"/recovery", http.MethodDelete, "seal", "generate_recovery"},
	{"/root/generate", http.MethodPost, "seal", "bootstrap"},
	{"/rotate-root", http.MethodPost, "seal", "rekey"},
	{"/rekey", http.MethodPost, "seal", "rekey"},
	{"/status", http.MethodGet, "tenant_barrier", "status"},
	{"/init", http.MethodPost, "tenant_barrier", "init"},
	{"/unseal", http.MethodPost, "tenant_barrier", "unseal"},
	{"/seal", http.MethodPost, "tenant_barrier", "seal"},
}

// buildLegacySealSpecialRules generates the pepSpecialRule entries for the
// legacy /api/v1/seal surface from legacySealOps.
func buildLegacySealSpecialRules() []pepSpecialRule {
	return buildPrefixRules(legacySealPrefix, legacySealOps)
}

// buildPrefixRules expands a single-mount op table into pepSpecialRule entries
// scoped to prefix.
func buildPrefixRules(prefix string, ops []pepOp) []pepSpecialRule {
	rules := make([]pepSpecialRule, 0, len(ops))
	for _, op := range ops {
		rules = append(rules, pepSpecialRule{
			method:   op.method,
			prefix:   prefix,
			suffix:   op.suffix,
			resource: op.resource,
			action:   op.action,
		})
	}
	return rules
}

// buildSSHSpecialRules generates the pepSpecialRule entries for the SSH surface
// from sshOps × sshPrefixes, keeping the per-endpoint source of truth in a
// single slice and avoiding hand-written duplication.
func buildSSHSpecialRules() []pepSpecialRule {
	rules := make([]pepSpecialRule, 0, len(sshPrefixes)*len(sshOps))
	for _, prefix := range sshPrefixes {
		for _, op := range sshOps {
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

// credentialWireOps maps the non-CRUD endpoints of the qrdb-wire
// /v1/credentials mount (server.go) to their registered qrdb credentials:*
// actions, mirroring the gRPC canon byte-for-byte
// (pkg/api/grpc/authz_mapping.go qrdb.v1.QRDBService Credential* entries) so
// a permission grant authorizes the same credential verb identically on REST
// and gRPC. Suffixes are ordered so the tier-based matching resolves the
// /versions vs /versions/{version} overlap: "/versions" wins at tier 2
// (path ends with it) for the version listing, while "/versions/" wins at
// tier 1 (longer contained segment) for a single-version retrieve.
var credentialWireOps = []pepSpecialRule{
	{http.MethodGet, "/v1/credentials", "/strategy", "credentials", "strategy"},
	{http.MethodPost, "/v1/credentials", "/submit", "credentials", "submit"},
	{http.MethodPost, "/v1/credentials", "/rotate", "credentials", "rotate"},
	{http.MethodPost, "/v1/credentials", "/gc", "credentials", "gc_expired"},
	{http.MethodGet, "/v1/credentials", "/versions", "credentials", "list_versions"},
	{http.MethodGet, "/v1/credentials", "/versions/", "credentials", "retrieve_version"},
}

// ceremonyGranularOp encodes one initialization-ceremony operation: the path
// suffix that uniquely identifies the endpoint, the HTTP method, and the
// registered qrdb ceremony action. Shared across every ceremony mount via
// ceremonyPrefixes (barrierGranularOps precedent).
type ceremonyGranularOp struct {
	suffix string
	method string
	action string
}

// ceremonyGranularOps is the single source of truth for the ceremony
// operations registered on the /v1/ceremony + /v1/init wire mounts and the
// /api/v1/init mirror. The actions mirror the gRPC canon
// (pkg/api/grpc/authz_mapping.go quicbaas.ceremony.v1.Ceremony /
// qrdb.v1.QRDBService Ceremony* entries) byte-for-byte. A suffix that is not
// mounted under a given prefix is inert (the router 404s first).
var ceremonyGranularOps = []ceremonyGranularOp{
	{"/enroll-external-officer", http.MethodPost, "enroll_custodian"},
	{"/officer-share/complete", http.MethodPost, "claim_share_complete"},
	{"/officer-share/begin", http.MethodPost, "claim_share_begin"},
	{"/claim-cert/complete", http.MethodPost, "claim_cert"},
	{"/claim-cert/begin", http.MethodPost, "claim_cert"},
	{"/initialize", http.MethodPost, "initialize"},
	{"/officers", http.MethodGet, "status"},
	{"/sign-csr", http.MethodPost, "sign_csr"},
	{"/status", http.MethodGet, "status"},
}

// ceremonyPrefixes lists every path prefix the ceremony surface is mounted
// under: the two qrdb-wire root mounts and the canonical /api/v1 mirror.
var ceremonyPrefixes = []string{
	"/v1/ceremony",
	"/v1/init",
	"/api/v1/init",
}

// buildCeremonySpecialRules generates the pepSpecialRule entries for the
// ceremony surface from ceremonyGranularOps × ceremonyPrefixes, keeping the
// per-endpoint source of truth in a single slice (barrier/SSH precedent).
func buildCeremonySpecialRules() []pepSpecialRule {
	rules := make([]pepSpecialRule, 0, len(ceremonyPrefixes)*len(ceremonyGranularOps))
	for _, prefix := range ceremonyPrefixes {
		for _, op := range ceremonyGranularOps {
			rules = append(rules, pepSpecialRule{
				method:   op.method,
				prefix:   prefix,
				suffix:   op.suffix,
				resource: "ceremony",
				action:   op.action,
			})
		}
	}
	return rules
}

// pepSpecialRulesBase holds the broker data-plane endpoints whose action is not
// a plain CRUD verb. It is combined with the barrier-granular rules
// (buildBarrierSpecialRules) into pepSpecialRules at package init time so the
// barrier surface has a single authoritative source of truth for both the PEP
// mapping and the default-deny gate (see authz_default_deny.go).
// Each (resource, action) here is a registered qrdb permission
// (go-qrdb/pkg/permission/broker_dataplane_scope.go).
var pepSpecialRulesBase = []pepSpecialRule{
	// ---- Secondary-index range scan ----
	// POST /api/v1/query/range calls service.ScanIndex, which the gRPC canon
	// authorizes as index:scan; the coarse /api/v1/query entry keeps the
	// index-equality GET on kv:range, matching gRPC QuicBaaS/Query.
	{http.MethodPost, "/api/v1/query", "/range", "index", "scan"},

	// ---- Certificate extension verbs (handlers_certificate_extensions.go) ----
	// Cross-signing mints a CA certificate: it re-certifies a foreign ROOT's
	// subject and public key with basicConstraints CA:TRUE. qrdb documents the
	// boundary explicitly — "Holding certs:generate must not imply the
	// authority to mint a new signing CA" (go-qrdb/pkg/permission/
	// acme_admin_scope.go) — so cross-sign authorizes with ca:create, not the
	// leaf-issuance certs:generate the rest of /api/v1/certificate uses.
	//
	// Chain performs the CA's Authority Information Access walk, which issues
	// outbound HTTP fetches and imports the retrieved issuers into the CA
	// certificate store. That is the same mutation POST /api/v1/trust/
	// auto-import-aia performs, so it carries the same write-strength verb
	// rather than a read verb a lower-privileged principal could use to drive
	// the importer.
	//
	// Device identity issuance mints TCG EK / AK / IEEE 802.1AR IDevID
	// certificates. These are END-ENTITY certificates, not CAs, so they stay
	// in the leaf-issuance family: certs:generate.
	// ---- Attestation read-strength verbs ----
	// Verify and VerifyChain are pure computations over caller-supplied
	// material: they mutate nothing and touch no hardware, so they carry
	// attestation:verify rather than the coarse-entry attestation:attest that
	// POST /api/v1/attestation/attest-container needs. Values MUST match the
	// gRPC canon (pkg/api/grpc/authz_mapping.go), the QUIC sub-op table
	// (pkg/api/quic/handler_authz_qrdb.go) and the MCP mapping
	// (pkg/api/mcp/authz_qrdb.go).
	{http.MethodPost, "/api/v1/attestation", "/verify", "attestation", "verify"},
	{http.MethodPost, "/api/v1/attestation", "/verify-chain", "attestation", "verify"},

	{http.MethodPost, "/api/v1/certificate", "/cross-sign", "ca", "create"},
	{http.MethodPost, "/api/v1/certificate", "/chain", "certs", "generate"},
	{http.MethodPost, "/api/v1/certificate", "/device-identity", "certs", "generate"},

	// ---- KMS key administration sub-actions ----
	// These mirror the gRPC canon (authz_mapping.go KeyManagement entries) so
	// REST and gRPC authorize the same verb identically: RevokeKey -> keys:delete,
	// UpdateKeyMetadata -> keys:rotate, GetSupportedAlgorithms -> keys:list.
	// GET /keys/{name}/metadata and /keys/{name}/public resolve to keys:get via
	// the coarse /api/v1/kms prefix mapping and need no special rule.
	{http.MethodPost, "/api/v1/kms", "/revoke", "keys", "delete"},
	{http.MethodPut, "/api/v1/kms", "/metadata", "keys", "rotate"},
	{http.MethodGet, "/api/v1/kms", "/algorithms", "keys", "list"},

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
	// Moving a session onto a different pooled account closes its agent process
	// and reconstitutes it against a newly selected credential — a spawn's
	// authority, not a close's or a status read's. It deliberately shares the
	// spawn verb: that is exactly the permission the session was created with,
	// and no weaker orchestrator verb may authorize consuming another account's
	// credential. Mirrors the gRPC canon (RotateSessionAccount), the QUIC
	// sub-op table (rotateSubOpPermissions) and the MCP mapping
	// (quicbaas_orchestrator_rotate_account).
	{http.MethodPost, "/api/v1/orchestrator", "/rotate-account", "orchestrator", "spawn"},
	{http.MethodPost, "/api/v1/orchestrator", "/tasks", "orchestrator", "task_create"},
	{http.MethodPost, "/api/v1/orchestrator", "/jobs", "orchestrator", "job_start"},
	{http.MethodPost, "/api/v1/orchestrator", "/agents", "orchestrator", "agent_register"},
	// Agent lifecycle admin: disable maps to agent_remove (admin restriction on an
	// agent slot); enable maps to agent_register (restoring the agent to service).
	// These use the nearest qrdb-registered permissions since orchestrator:agent_disable
	// and orchestrator:agent_enable are not yet in the qrdb permission registry.
	{http.MethodPost, "/api/v1/orchestrator", "/disable", "orchestrator", "agent_remove"},
	{http.MethodPost, "/api/v1/orchestrator", "/enable", "orchestrator", "agent_register"},

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

	// ---- Pooled provider account leaf actions ----
	// Rotation replaces the stored credential envelope -> credentials:rotate,
	// matching the /v1/credentials "/rotate" rule. Revocation excludes the
	// account from every selection strategy, a destructive lifecycle
	// transition, so it takes the delete-grade verb. Marking an account the
	// tenant default is a metadata write -> credentials:store. Usage is an
	// item read -> credentials:retrieve.
	{http.MethodPost, "/api/v1/agent-accounts", "/rotate", "credentials", "rotate"},
	{http.MethodPost, "/api/v1/agent-accounts", "/revoke", "credentials", "delete"},
	{http.MethodPost, "/api/v1/agent-accounts", "/default", "credentials", "store"},
	{http.MethodGet, "/api/v1/agent-accounts", "/usage", "credentials", "retrieve"},

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

	// ---- Cluster control plane: non-membership write actions ----
	// Mirrors pkg/api/grpc/authz_mapping.go so REST and gRPC authorize cluster
	// operations identically (multi-protocol parity). The coarse /api/v1/cluster
	// entry already maps reads -> admin:status and node add/remove -> admin
	// add_member/remove_member; these refine the data-movement verbs.
	{http.MethodPost, "/api/v1/cluster", "/rebalance", "rebalance", "execute"},
	{http.MethodGet, "/api/v1/cluster", "/rebalance/plan", "rebalance", "plan"},
	{http.MethodGet, "/api/v1/cluster", "/rebalance/status", "rebalance", "report"},
	{http.MethodPost, "/api/v1/cluster", "/migrate", "admin", "drain"},
	{http.MethodDelete, "/api/v1/cluster", "/migrate", "admin", "drain"},
	{http.MethodPost, "/api/v1/cluster", "/upgrade", "admin", "drain"},
	{http.MethodPut, "/api/v1/cluster", "/replace", "admin", "add_member"},

	// ---- Vector non-CRUD: search + namespace + index + admin ----
	{http.MethodGet, "/api/v1/vector", "/health", "vector", "namespace_list"},
	{http.MethodPost, "/api/v1/vector", "/search/similar", "vector", "search_similar"},
	{http.MethodPost, "/api/v1/vector", "/search/by-id", "vector", "search_by_id"},
	{http.MethodPost, "/api/v1/vector", "/hybrid-search", "vector", "hybrid_search"},
	{http.MethodPost, "/api/v1/vector", "/search-by-metadata", "vector", "search_by_metadata"},
	{http.MethodGet, "/api/v1/vector", "/namespaces", "vector", "namespace_list"},
	{http.MethodPost, "/api/v1/vector", "/namespaces", "vector", "namespace_create"},
	{http.MethodDelete, "/api/v1/vector", "/namespaces/", "vector", "namespace_delete"},
	{http.MethodPost, "/api/v1/vector", "/index/rebuild", "vector", "rebuild_index"},
	{http.MethodGet, "/api/v1/vector", "/index/progress", "vector", "get_rebuild_progress"},
	{http.MethodGet, "/api/v1/vector", "/stats", "vector", "get_index_stats"},
	{http.MethodPost, "/api/v1/vector", "/compact", "vector", "compact"},
	{http.MethodPost, "/api/v1/vector", "/vectors/batch", "vector", "batch_upsert"},
	{http.MethodDelete, "/api/v1/vector", "/vectors/batch", "vector", "batch_delete"},
	{http.MethodPost, "/api/v1/vector", "/vectors", "vector", "upsert"},
	{http.MethodGet, "/api/v1/vector", "/vectors", "vector", "list"},

	// ---- Embedding generation + provider management ----
	{http.MethodPost, "/api/v1/embedding", "/generate", "embedding", "embed"},
	{http.MethodGet, "/api/v1/embedding", "/providers", "embedding", "list_models"},
	{http.MethodPut, "/api/v1/embedding", "/providers/default", "embedding", "list_models"},
	{http.MethodPut, "/api/v1/embedding", "/status", "embedding", "list_models"},

	// ---- RAG document lifecycle + query ----
	{http.MethodPost, "/api/v1/rag", "/query", "rag", "query"},
	{http.MethodPost, "/api/v1/rag", "/context", "rag", "query"},
	{http.MethodGet, "/api/v1/rag", "/documents", "rag", "list_documents"},
	{http.MethodPost, "/api/v1/rag", "/documents", "rag", "ingest"},
	{http.MethodPost, "/api/v1/rag", "/ingest", "rag", "ingest"},
	{http.MethodPost, "/api/v1/rag", "/reindex", "rag", "ingest"},
	{http.MethodDelete, "/api/v1/rag", "/documents/", "rag", "delete_document"},

	// ---- IAM utility group (/api/v1/iam: sessions / audit / PEP checks) ----
	// Root-mounted in iam.go outside the /api/v1 chi group; it carries the same
	// useAPIAuthzMiddleware chain as /api/v1/tenants, so every endpoint must
	// resolve to a registered qrdb permission (go-qrdb/pkg/permission).
	//
	// Pending self-registration review transitions a user's state: approve ->
	// active (users:enable), reject -> disabled (users:disable).
	{http.MethodPost, "/api/v1/iam", "/approve", "users", "enable"},
	{http.MethodPost, "/api/v1/iam", "/reject", "users", "disable"},
	// OAuth2 client-credential validation reads the tenant's registered app.
	// Apps are managed under the tenants surface (/api/v1/tenants/{id}/apps
	// authorizes as tenants:*), so validation authorizes as a tenants read.
	{http.MethodPost, "/api/v1/iam", "/validate-client", "tenants", "read"},
	// PEP access checks resolve a subject's effective permissions.
	{http.MethodPost, "/api/v1/iam", "/check/batch", "permissions", "resolve"},
	{http.MethodPost, "/api/v1/iam", "/batch-check", "permissions", "resolve"},
	{http.MethodPost, "/api/v1/iam", "/check", "permissions", "resolve"},
	// Audit log: filtered listing -> audit:query, single event -> audit:read.
	// "/audit/" (item) is longer than "/audit" (collection) so audit:read wins
	// for /audit/{id} while /audit stays audit:query (scheduler /jobs pattern).
	{http.MethodGet, "/api/v1/iam", "/audit/", "audit", "read"},
	{http.MethodGet, "/api/v1/iam", "/audit", "audit", "query"},
	// Retention pruning: the qrdb registry has no audit:delete yet, so the gate
	// requires the nearest registered audit-family permission; the handler
	// additionally enforces the fine-grained audit:delete AdminManager check
	// before deleting. A future qrdb audit:delete registration should replace
	// this mapping.
	{http.MethodDelete, "/api/v1/iam", "/audit", "audit", "query"},
	// Login-session management operates on the caller's own user record:
	// session reads -> users:read, revocations mutate session state ->
	// users:update. No session:* family exists in the qrdb registry; a future
	// registration should replace this mapping.
	{http.MethodPost, "/api/v1/iam", "/sessions/revoke-others", "users", "update"},
	{http.MethodGet, "/api/v1/iam", "/sessions/", "users", "read"},
	{http.MethodDelete, "/api/v1/iam", "/sessions/", "users", "update"},
	{http.MethodGet, "/api/v1/iam", "/sessions", "users", "read"},
	{http.MethodDelete, "/api/v1/iam", "/sessions", "users", "update"},

	// ---- Canonical xkms WebAuthn enrollment (/api/v1/iam/{tenant}/webauthn) ----
	// The mount registered by MountXKMSWebAuthnRoutes is root-level and carries
	// the same useAPIAuthzMiddleware chain as the rest of the IAM surface.
	// Registration IS authenticator (factor) enrollment, so it authorizes with
	// the same qrdb user_authfactors:* vocabulary the /v1/users/factors wire
	// mount uses (POST -> add, read -> get) rather than the coarse tenants:*
	// the "/api/v1/iam" prefix would otherwise apply. The sibling LOGIN routes
	// never reach this table: they are pre-auth (authz_public_routes.go).
	{http.MethodPost, "/api/v1/iam", "/webauthn/registration/begin", "user_authfactors", "add"},
	{http.MethodPost, "/api/v1/iam", "/webauthn/registration/finish", "user_authfactors", "add"},
	{http.MethodGet, "/api/v1/iam", "/webauthn/registration/status", "user_authfactors", "get"},

	// ---- Delegated admin (/api/v1/admin) ----
	// Assignment reads split collection vs item the same way the scheduler
	// /jobs rules do: "/assignments/" (mid-path, longer) wins for a single
	// assignment while "/assignments" (leaf) stays the collection listing.
	{http.MethodGet, "/api/v1/admin", "/assignments/", "roles", "read"},
	{http.MethodGet, "/api/v1/admin", "/assignments", "roles", "list"},
	// A delegated-admin permission check resolves a subject's effective
	// permissions, exactly like the /api/v1/iam PEP endpoints.
	{http.MethodPost, "/api/v1/admin", "/check-permission", "permissions", "resolve"},
	// Impersonation mutates who a session acts as. No session:* or
	// impersonation:* family exists in the qrdb registry, so it uses the same
	// users:read / users:update vocabulary the /api/v1/iam session rules use;
	// the handler additionally enforces the AdminManager's own checks. A future
	// qrdb impersonation:* registration should replace this mapping.
	{http.MethodPost, "/api/v1/admin", "/impersonation/start", "users", "update"},
	{http.MethodPost, "/api/v1/admin", "/end", "users", "update"},
	{http.MethodGet, "/api/v1/admin", "/impersonations", "users", "read"},
	{http.MethodGet, "/api/v1/admin", "/impersonation", "users", "read"},

	// ---- Tenant-scoped user session administration ----
	// DELETE /api/v1/tenants/{t}/users/{u}/sessions (admin revoke-all — the
	// REST projection of the parameterized SDK Session/RevokeAll verb, gRPC
	// SessionService/RevokeAllSessions) mutates the target user's session
	// state, so it authorizes as users:update — the same vocabulary as the
	// self-scoped /api/v1/iam/sessions revocations — rather than the
	// surface-wide tenants:delete the prefix mapping would otherwise apply.
	{http.MethodDelete, "/api/v1/tenants", "/sessions", "users", "update"},

	// ---- Per-tenant barrier lifecycle ----
	// These leaf actions (tier 2) win over the surface-wide "/api/v1/tenants"
	// prefix mapping (resource "tenants"), so per-tenant barrier ops authorize
	// as the tenant_barrier resource. unseal-share maps to the unseal action.
	{http.MethodPost, "/api/v1/tenants", "/barrier/enable-encryption", "tenant_barrier", "enable_encryption"},
	{http.MethodPost, "/api/v1/tenants", "/barrier/init", "tenant_barrier", "init"},
	{http.MethodPost, "/api/v1/tenants", "/barrier/unseal-share", "tenant_barrier", "unseal"},
	{http.MethodPost, "/api/v1/tenants", "/barrier/unseal", "tenant_barrier", "unseal"},
	{http.MethodPost, "/api/v1/tenants", "/barrier/seal", "tenant_barrier", "seal"},
	{http.MethodGet, "/api/v1/tenants", "/barrier/status", "tenant_barrier", "status"},
	// ---- Per-tenant engine governance ----
	// PATCH /api/v1/tenants/{id}/quotas and /entitlements are privileged
	// plan-management operations that govern engine-side resource limits and
	// feature access. They win over the surface-wide "tenants:update" mapping.
	{http.MethodPatch, "/api/v1/tenants", "/quotas", "tenants", "set_quotas"},
	{http.MethodPatch, "/api/v1/tenants", "/entitlements", "tenants", "set_entitlements"},
}

// pepSpecialRules is the active special-rule list consulted by
// matchSpecialRule. It is the union of pepSpecialRulesBase (all non-barrier
// broker surfaces), the barrier-granular rules for both /api/v1/barrier and
// /v1/barrier generated from barrierGranularOps, the ceremony rules for the
// /v1/ceremony + /v1/init + /api/v1/init mounts generated from
// ceremonyGranularOps, the /v1/credentials qrdb-wire rules, the SSH rules, the
// go-xkms compatibility rules (xkmsOps) and the legacy /api/v1/seal rules
// (legacySealOps). Combining them here keeps a single source of truth per
// surface.
var pepSpecialRules = func() []pepSpecialRule {
	barrier := buildBarrierSpecialRules()
	ceremony := buildCeremonySpecialRules()
	ssh := buildSSHSpecialRules()
	xkms := buildXKMSSpecialRules()
	legacySeal := buildLegacySealSpecialRules()
	combined := make([]pepSpecialRule, 0,
		len(pepSpecialRulesBase)+len(barrier)+len(ceremony)+
			len(credentialWireOps)+len(ssh)+len(xkms)+len(legacySeal))
	combined = append(combined, pepSpecialRulesBase...)
	combined = append(combined, barrier...)
	combined = append(combined, ceremony...)
	combined = append(combined, credentialWireOps...)
	combined = append(combined, ssh...)
	combined = append(combined, xkms...)
	combined = append(combined, legacySeal...)
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
