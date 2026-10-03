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

package mcp

import "strings"

// This file maps MCP tool names onto qrdb-canonical (resource, action)
// permission tuples. The qrdb policy decision point joins the tuple into the
// registered "resource:action" permission (e.g. "kv:put"); the vocabulary here
// MUST match go-qrdb/pkg/permission/constants.go exactly. It mirrors the REST
// PEP mapping, the gRPC method mapping, and the QUIC opcode mapping, which
// established the broker resource -> qrdb resource translation (kv, keys, certs,
// credentials, tenant_barrier, users, roles, tenants, timeseries, vector,
// transaction, batch, watch, backup, ceremony, admin).
//
// A tool that has no entry is reported unmapped (ok=false): under RequireAuthz
// the MCP authz gate is default-deny, so an unmapped protected tool is denied
// rather than admitted.
//
// Stage 0.5 maps the broker-only data-plane surfaces that qrdb now registers a
// permission family for (go-qrdb/pkg/permission/broker_dataplane_scope.go):
// object storage (objstore), orchestrator, scheduler, parameters,
// dynamic_secrets, project, ollama.
//
// The surfaces below are mapped onto the SAME qrdb permission their REST and
// gRPC siblings require, so one grant authorizes the same operation identically
// on every transport (KMS / crypto / DEK -> keys:* + backends:read, the tenant
// secret vault -> secrets:*, messaging -> messaging:*, embedding ->
// embedding:*, RAG -> rag:*, the go-xkms compatibility tools -> the xkmsOps
// families, and the IAM sub-surfaces that have a REST rule).
//
// Surfaces the qrdb permission registry still does NOT define a resource family
// for, and which therefore have no sibling rule to copy, remain intentionally
// absent and denied under RequireAuthz. They are enumerated with their reason in
// unmappedTools (authz_qrdb_unmapped.go) and pinned by
// TestMCPPermissions_EveryToolMappedOrPublic.
//
// Barrier tools (qrdb_barrier_*) are mapped EXACTLY, one entry per tool,
// mirroring the per-operation granularity of the gRPC mapping in
// pkg/api/grpc/authz_mapping.go (lines 259–276). The former "qrdb_barrier"
// prefix catch-all has been removed: any future qrdb_barrier_* tool that lacks
// an explicit entry is default-denied, not silently admitted at a read
// permission.

// qrdbToolResource is a qrdb-canonical (resource, action) tuple.
type qrdbToolResource struct {
	resource string
	action   string
}

// qrdbToolExactMapping maps exact MCP tool names to the qrdb permission they
// require. Every value is a registered qrdb permission.
var qrdbToolExactMapping = map[string]qrdbToolResource{
	// Service registry. Its own resource family (pkg/permission): register,
	// renew and deregister carry the write verb, resolve and the watch long
	// poll the read verb. Values MUST match pkg/api/grpc/authz_mapping.go and
	// the REST frozen route table.
	"quicbaas_registry_register":   {"service_registry", "register"},
	"quicbaas_registry_renew":      {"service_registry", "register"},
	"quicbaas_registry_deregister": {"service_registry", "register"},
	"quicbaas_registry_resolve":    {"service_registry", "read"},
	"quicbaas_registry_watch":      {"service_registry", "read"},

	// KV. The quicbaas_ names here are the ones the registry actually
	// registers; the quicbaas_get / quicbaas_put / quicbaas_set /
	// quicbaas_scan / quicbaas_list / quicbaas_delete aliases that used to sit
	// beside them named no registered tool and no dispatch case, so they were
	// pure dead entries — the same defect as the eight user/role entries below,
	// and removed with them in issue #905.
	// TestMCPPermissions_EveryMappedToolIsRegistered now makes that class of
	// entry impossible to add back.
	"quicbaas_query":        {"kv", "range"},
	"quicbaas_scan_index":   {"index", "scan"},
	"quicbaas_batch_get":    {"batch", "execute"},
	"quicbaas_batch_put":    {"batch", "execute"},
	"quicbaas_batch_delete": {"batch", "execute"},
	"qrdb_get":              {"kv", "get"},
	"qrdb_list":             {"kv", "list"},
	"qrdb_put":              {"kv", "put"},
	"qrdb_delete":           {"kv", "delete"},

	// Transaction
	"quicbaas_transaction": {"transaction", "execute"},

	// Cluster admin
	"quicbaas_cluster_status":     {"admin", "status"},
	"quicbaas_get_leader":         {"admin", "status"},
	"quicbaas_add_node":           {"admin", "add_member"},
	"quicbaas_remove_node":        {"admin", "remove_member"},
	"quicbaas_create_shard":       {"admin", "start_shard"},
	"quicbaas_rebalance_cluster":  {"rebalance", "execute"},
	"quicbaas_rebalance_status":   {"rebalance", "report"},
	"quicbaas_migrate_data":       {"admin", "drain"},
	"quicbaas_migration_progress": {"admin", "status"},
	"quicbaas_replace_node":       {"admin", "add_member"},
	"quicbaas_upgrade_node":       {"admin", "drain"},

	// Backup
	"quicbaas_backup_create":  {"backup", "create"},
	"quicbaas_backup_list":    {"backup", "list"},
	"quicbaas_backup_restore": {"backup", "restore"},
	"quicbaas_backup_delete":  {"backup", "delete"},
	"quicbaas_backup_verify":  {"backup", "verify"},

	// Role management. ROLES ONLY: user administration is the quicbaas_iam_user_*
	// family further down, which is the single user surface. The eight entries
	// removed here in issue #905 were either mapped to tools that were never
	// registered (quicbaas_user_* and quicbaas_role_create/delete/list/assign) or
	// mapped to the qrdb_user_create/delete/list tools that routed through an
	// auth manager production never wired, so they could not succeed. The four
	// that remain back live tools (pkg/service/rbac_role_store.go).
	"qrdb_role_create":     {"roles", "create"},
	"qrdb_role_delete":     {"roles", "delete"},
	"qrdb_role_list":       {"roles", "list"},
	"qrdb_role_assign":     {"roles", "assign"},
	"quicbaas_role_revoke": {"roles", "unassign"},

	// qrdb user lookup + user-login factor surface (qrdb mirror; see
	// tools_qrdb_users.go). Lookups map to the users:* read family; factor
	// verbs map 1:1 onto the qrdb-registered user_authfactors:* family —
	// the same vocabulary the REST scope rules use for /users and
	// /v1/users/factors.
	"qrdb_user_get":                    {"users", "read"},
	"qrdb_user_get_by_username":        {"users", "read"},
	"qrdb_user_get_by_cert_thumbprint": {"users", "get_by_cert_thumbprint"},
	"qrdb_user_authfactor_add":         {"user_authfactors", "add"},
	"qrdb_user_authfactor_list":        {"user_authfactors", "list"},
	"qrdb_user_authfactor_get":         {"user_authfactors", "get"},
	"qrdb_user_authfactor_remove":      {"user_authfactors", "remove"},
	"qrdb_user_authfactor_rotate":      {"user_authfactors", "rotate"},
	"qrdb_user_authfactor_relabel":     {"user_authfactors", "relabel"},
	"qrdb_user_authfactor_login":       {"user_authfactors", "login"},

	// Barrier (qrdb mirror) — exact per-operation mapping, mirroring gRPC
	// authz_mapping.go lines 259-276. Each tool gets its own minimum-privilege
	// permission; the former "qrdb_barrier" prefix catch-all is gone so an
	// unmapped future barrier tool is default-denied.
	"qrdb_barrier_status":                   {"tenant_barrier", "status"},
	"qrdb_barrier_initialize":               {"tenant_barrier", "init"},
	"qrdb_barrier_unseal":                   {"tenant_barrier", "unseal"},
	"qrdb_barrier_seal":                     {"tenant_barrier", "seal"},
	"qrdb_barrier_shamir_initialize":        {"seal", "initialize_shamir"},
	"qrdb_barrier_shamir_unseal_share":      {"barrier", "unseal-share"},
	"qrdb_barrier_shamir_unseal_shares":     {"barrier", "unseal-share"},
	"qrdb_barrier_rekey":                    {"seal", "rekey"},
	"qrdb_barrier_recovery_generate":        {"seal", "generate_recovery"},
	"qrdb_barrier_recovery_recover":         {"seal", "recover_with_keys"},
	"qrdb_barrier_recovery_delete":          {"seal", "generate_recovery"},
	"qrdb_barrier_recovery_status":          {"seal", "status"},
	"qrdb_barrier_root_token":               {"seal", "bootstrap"},
	"qrdb_barrier_enable_encryption":        {"seal", "enable_encryption"},
	"qrdb_tenant_barrier_enable_encryption": {"tenant_barrier", "enable_encryption"},
	"qrdb_tenant_update_quotas":             {"tenants", "set_quotas"},
	"qrdb_tenant_update_entitlements":       {"tenants", "set_entitlements"},

	// Cache (admin capabilities)
	"quicbaas_cache_stats": {"admin", "status"},
	"quicbaas_cache_clear": {"admin", "capabilities"},

	// Aggregate / time-series
	"quicbaas_aggregate": {"timeseries", "aggregate"},

	// Auth (logout)
	"quicbaas_logout": {"user_authfactors", "login"},

	// ---- Stage 0.5: broker data-plane surfaces (qrdb-registered) ----

	// Object storage -> objstore
	"quicbaas_object_put":                   {"objstore", "put"},
	"quicbaas_object_get":                   {"objstore", "get"},
	"quicbaas_object_delete":                {"objstore", "delete"},
	"quicbaas_object_list":                  {"objstore", "list"},
	"quicbaas_object_list_by_tag":           {"objstore", "list"},
	"quicbaas_object_list_by_tenant":        {"objstore", "list"},
	"quicbaas_object_head":                  {"objstore", "head"},
	"quicbaas_object_copy":                  {"objstore", "copy"},
	"quicbaas_object_archive":               {"objstore", "archive"},
	"quicbaas_object_restore":               {"objstore", "restore"},
	"quicbaas_object_verify":                {"objstore", "verify"},
	"quicbaas_object_replicate":             {"objstore", "replicate"},
	"quicbaas_object_migrate":               {"objstore", "migrate"},
	"quicbaas_object_batch_put":             {"objstore", "batch"},
	"quicbaas_object_batch_get":             {"objstore", "batch"},
	"quicbaas_object_batch_delete":          {"objstore", "batch"},
	"quicbaas_object_get_policy":            {"objstore", "read_policy"},
	"quicbaas_object_set_policy":            {"objstore", "write_policy"},
	"quicbaas_object_list_backends":         {"objstore", "read_backend"},
	"quicbaas_object_backend_health":        {"objstore", "read_backend"},
	"quicbaas_object_tenant_usage":          {"objstore", "tenant_usage"},
	"quicbaas_object_tenant_quotas":         {"objstore", "tenant_quota_read"},
	"quicbaas_object_set_tenant_quotas":     {"objstore", "tenant_quota_write"},
	"quicbaas_object_transfer_fetch":        {"objstore", "get"},
	"quicbaas_object_transfer_stream_fetch": {"objstore", "get"},

	// Object storage admin -> objstore
	"quicbaas_object_admin_create_backend":     {"objstore", "admin"},
	"quicbaas_object_admin_update_backend":     {"objstore", "admin"},
	"quicbaas_object_admin_delete_backend":     {"objstore", "admin"},
	"quicbaas_object_admin_set_backend_status": {"objstore", "admin"},
	"quicbaas_object_admin_get_storage_stats":  {"objstore", "read_stats"},
	"quicbaas_object_admin_run_gc":             {"objstore", "admin"},
	"quicbaas_object_admin_get_gc_progress":    {"objstore", "admin"},
	"quicbaas_object_admin_repair_object":      {"objstore", "admin"},
	"quicbaas_object_admin_compact_backend":    {"objstore", "admin"},

	// Orchestrator -> orchestrator
	"quicbaas_orchestrator_spawn":                {"orchestrator", "spawn"},
	"quicbaas_orchestrator_prompt":               {"orchestrator", "prompt"},
	"quicbaas_orchestrator_status":               {"orchestrator", "status"},
	"quicbaas_orchestrator_session_history":      {"orchestrator", "status"},
	"quicbaas_orchestrator_session_conversation": {"orchestrator", "status"},
	"quicbaas_orchestrator_cost":                 {"orchestrator", "status"},
	"quicbaas_orchestrator_spend_policy_get":     {"orchestrator", "status"},
	"quicbaas_orchestrator_spend_policy_set":     {"spend_policy", "write"},
	// A tenant's runtime cost settings (go-quicbaas#1058). Replacing them is
	// a QuicBaaS family: go-qrdb owns the orchestrator family outright.
	"quicbaas_orchestrator_cost_settings_get": {"orchestrator", "status"},
	"quicbaas_orchestrator_cost_settings_set": {"cost_settings", "write"},
	// The tenant AI agent catalog, mirroring /api/v1/agents and
	// /api/v1/agent-pools: reads agents:read, every change agents:write.
	"quicbaas_agents_list":         {"agents", "read"},
	"quicbaas_agents_get":          {"agents", "read"},
	"quicbaas_agents_used_by":      {"agents", "read"},
	"quicbaas_agents_create":       {"agents", "write"},
	"quicbaas_agents_update":       {"agents", "write"},
	"quicbaas_agents_delete":       {"agents", "write"},
	"quicbaas_agent_pools_list":    {"agents", "read"},
	"quicbaas_agent_pools_get":     {"agents", "read"},
	"quicbaas_agent_pools_used_by": {"agents", "read"},
	"quicbaas_agent_pools_create":  {"agents", "write"},
	"quicbaas_agent_pools_update":  {"agents", "write"},
	"quicbaas_agent_pools_delete":  {"agents", "write"},
	"quicbaas_orchestrator_close":  {"orchestrator", "close"},
	// Rotation closes the session's agent process and reconstitutes it against
	// a newly selected pooled credential — a spawn's authority, not a close's.
	// Mirrors the gRPC canon (RotateSessionAccount), the REST PEP rule
	// ("/rotate-account") and the QUIC sub-op table (rotateSubOpPermissions).
	"quicbaas_orchestrator_rotate_account":    {"orchestrator", "spawn"},
	"quicbaas_orchestrator_list":              {"orchestrator", "list"},
	"quicbaas_orchestrator_list_sessions":     {"orchestrator", "list"},
	"quicbaas_orchestrator_task_create":       {"orchestrator", "task_create"},
	"quicbaas_orchestrator_task_block":        {"orchestrator", "task_block"},
	"quicbaas_orchestrator_task_readmit":      {"orchestrator", "task_readmit"},
	"quicbaas_orchestrator_task_delete":       {"orchestrator", "task_create"},
	"quicbaas_orchestrator_attention_list":    {"orchestrator", "task_list"},
	"quicbaas_orchestrator_attention_resolve": {"orchestrator", "task_readmit"},
	"quicbaas_orchestrator_decompose":         {"orchestrator", "task_decompose"},
	"quicbaas_orchestrator_start_job":         {"orchestrator", "job_start"},
	"quicbaas_orchestrator_get_job_status":    {"orchestrator", "job_read"},
	"quicbaas_orchestrator_list_jobs":         {"orchestrator", "job_list"},
	"quicbaas_orchestrator_cancel_job":        {"orchestrator", "job_cancel"},
	"quicbaas_orchestrator_list_backends":     {"orchestrator", "backend_list"},
	"quicbaas_orchestrator_list_runners":      {"orchestrator", "status"},
	"quicbaas_orchestrator_discover":          {"orchestrator", "discover"},
	"quicbaas_orchestrator_health":            {"orchestrator", "health"},
	"quicbaas_orchestrator_inference":         {"orchestrator", "inference"},
	"quicbaas_orchestrator_register":          {"orchestrator", "agent_register"},
	"quicbaas_orchestrator_cache_get":         {"orchestrator", "cache_read"},
	"quicbaas_orchestrator_cache_put":         {"orchestrator", "cache_write"},
	"quicbaas_orchestrator_broadcast":         {"orchestrator", "broadcast"},

	// Fleet + task-artifact tools keep their historical quicbaas_fleet_* /
	// quicbaas_task_* names but belong to the same orchestrator surface, so they
	// map onto the qrdb-registered orchestrator agent_* / artifact_read family.
	// Without an entry they were default-denied and never appeared in tools/list.
	"quicbaas_fleet_register":      {"orchestrator", "agent_register"},
	"quicbaas_fleet_list":          {"orchestrator", "agent_list"},
	"quicbaas_fleet_status":        {"orchestrator", "agent_read"},
	"quicbaas_fleet_remove":        {"orchestrator", "agent_remove"},
	"quicbaas_fleet_heartbeat":     {"orchestrator", "agent_heartbeat"},
	"quicbaas_task_get_artifact":   {"orchestrator", "artifact_read"},
	"quicbaas_task_list_artifacts": {"orchestrator", "artifact_read"},

	// SSH -> keys. SSH assets are cryptographic keys, so they authorize with
	// the qrdb keys:* family — byte-for-byte the same (resource, action)
	// pairs the REST PEP (pkg/api/rest/authz_pep_mapping.go sshOps), the gRPC
	// map (pkg/api/grpc/authz_mapping.go), and the QUIC sub-op map
	// (pkg/api/quic/handler_authz_qrdb.go sshSubOpPermissions) assign. One
	// grant authorizes the same SSH verb identically on every transport.
	"quicbaas_ssh_key_generate":      {"keys", "create"},
	"quicbaas_ssh_key_import":        {"keys", "import"},
	"quicbaas_ssh_key_get":           {"keys", "get"},
	"quicbaas_ssh_key_list":          {"keys", "list"},
	"quicbaas_ssh_key_delete":        {"keys", "delete"},
	"quicbaas_ssh_key_export":        {"keys", "get"},
	"quicbaas_ssh_key_sign":          {"keys", "sign"},
	"quicbaas_ssh_known_host_add":    {"keys", "create"},
	"quicbaas_ssh_known_host_list":   {"keys", "list"},
	"quicbaas_ssh_known_host_remove": {"keys", "delete"},
	"quicbaas_ssh_authorized_add":    {"keys", "create"},
	"quicbaas_ssh_authorized_list":   {"keys", "list"},
	"quicbaas_ssh_authorized_remove": {"keys", "delete"},
	"quicbaas_ssh_authorized_emit":   {"keys", "get"},

	// Scheduler -> scheduler
	"quicbaas_scheduler_create_job":  {"scheduler", "create"},
	"quicbaas_scheduler_get_job":     {"scheduler", "read"},
	"quicbaas_scheduler_list_jobs":   {"scheduler", "list"},
	"quicbaas_scheduler_update_job":  {"scheduler", "update"},
	"quicbaas_scheduler_delete_job":  {"scheduler", "delete"},
	"quicbaas_scheduler_trigger_job": {"scheduler", "trigger"},
	"quicbaas_scheduler_pause_job":   {"scheduler", "pause"},
	"quicbaas_scheduler_resume_job":  {"scheduler", "resume"},
	"quicbaas_scheduler_cancel_job":  {"scheduler", "cancel"},
	"quicbaas_scheduler_job_history": {"scheduler", "history"},
	"quicbaas_scheduler_status":      {"scheduler", "status"},

	// Workload placement carries its own "placement" family (qrdb owns
	// "scheduler"), with the same read / place / manage split as the gRPC
	// and REST tables.
	"quicbaas_scheduler_get_node":        {"placement", "read"},
	"quicbaas_scheduler_list_nodes":      {"placement", "read"},
	"quicbaas_scheduler_get_placement":   {"placement", "read"},
	"quicbaas_scheduler_list_placements": {"placement", "read"},
	"quicbaas_scheduler_place":           {"placement", "place"},
	"quicbaas_scheduler_release":         {"placement", "place"},
	"quicbaas_scheduler_register_node":   {"placement", "manage"},
	"quicbaas_scheduler_renew_node":      {"placement", "manage"},
	"quicbaas_scheduler_deregister_node": {"placement", "manage"},
	"quicbaas_scheduler_cordon_node":     {"placement", "manage"},
	"quicbaas_scheduler_uncordon_node":   {"placement", "manage"},
	"quicbaas_scheduler_drain_node":      {"placement", "manage"},

	// Parameter store -> parameters
	"quicbaas_parameter_get":          {"parameters", "read"},
	"quicbaas_parameter_set":          {"parameters", "write"},
	"quicbaas_parameter_delete":       {"parameters", "delete"},
	"quicbaas_parameter_list":         {"parameters", "list"},
	"quicbaas_parameter_batch_get":    {"parameters", "read"},
	"quicbaas_parameter_batch_set":    {"parameters", "write"},
	"quicbaas_parameter_batch_delete": {"parameters", "delete"},
	"quicbaas_parameter_search":       {"parameters", "search"},

	// Dynamic secrets -> dynamic_secrets
	"quicbaas_dynamic_secrets_create_role":  {"dynamic_secrets", "role_create"},
	"quicbaas_dynamic_secrets_get_role":     {"dynamic_secrets", "role_read"},
	"quicbaas_dynamic_secrets_list_roles":   {"dynamic_secrets", "role_list"},
	"quicbaas_dynamic_secrets_delete_role":  {"dynamic_secrets", "role_delete"},
	"quicbaas_dynamic_secrets_generate":     {"dynamic_secrets", "generate"},
	"quicbaas_dynamic_secrets_renew_lease":  {"dynamic_secrets", "lease_renew"},
	"quicbaas_dynamic_secrets_revoke_lease": {"dynamic_secrets", "lease_revoke"},
	"quicbaas_dynamic_secrets_list_leases":  {"dynamic_secrets", "lease_list"},

	// Project -> project (tools are registered without a quicbaas_ prefix)
	"project_create":            {"project", "create"},
	"project_get":               {"project", "read"},
	"project_list":              {"project", "list"},
	"project_update":            {"project", "update"},
	"project_delete":            {"project", "delete"},
	"project_grant_fleet_node":  {"project", "grant"},
	"project_revoke_fleet_node": {"project", "revoke"},

	// SDLC module -> sdlc (tools are registered without a quicbaas_ prefix).
	// Mirrors the REST rules for /api/v1/sdlc/projects/{projectID}/binding,
	// /api/v1/sdlc/tasks/{taskID}/delivery and /api/v1/sdlc/forges.
	"sdlc_bind":     {"sdlc", "bind"},
	"sdlc_unbind":   {"sdlc", "unbind"},
	"sdlc_binding":  {"sdlc", "read"},
	"sdlc_delivery": {"sdlc", "read"},
	"sdlc_forges":   {"sdlc", "read"},
	// sdlc_onboard WRITES to the forge, so it carries the same bind
	// permission as PUT .../binding; sdlc_conformance only reads. The split
	// is the point: a reader may ask what state a repository is in without
	// being able to change it.
	"sdlc_onboard":     {"sdlc", "bind"},
	"sdlc_conformance": {"sdlc", "read"},
	"sdlc_open_work":   {"sdlc", "read"},
	// Choosing which reviewer judges a pull request is a decision about the
	// project's review: writing an override carries sdlc:bind like the
	// binding it refines, reading one sdlc:read.
	"sdlc_reviewer":       {"sdlc", "read"},
	"sdlc_reviewer_set":   {"sdlc", "bind"},
	"sdlc_reviewer_clear": {"sdlc", "bind"},
	// Filing an issue on the bound forge is an audited operator write with
	// its own action, as POST .../issues is; the collaborator listing reads.
	"sdlc_issue_create":  {"sdlc", "issue_create"},
	"sdlc_collaborators": {"sdlc", "read"},
	// Adopting an issue creates its task, and is gated as task creation is,
	// mirroring POST /api/v1/sdlc/projects/{projectID}/adopt.
	"sdlc_adopt_issue": {"orchestrator", "task_create"},
	// The worker image refresh: a host verb over every tenant's bindings,
	// gated on the cross-tenant worker_images:* family exactly as
	// GET /api/v1/sdlc/worker-image and POST .../refresh are.
	"quicbaas_sdlc_worker_image":         {"worker_images", "read"},
	"quicbaas_sdlc_worker_image_refresh": {"worker_images", "refresh"},

	// Pooled provider accounts -> credentials. An account custodies credential
	// material (an Anthropic / Codex / OpenCode login), so it authorizes with
	// the qrdb credentials:* family — matching the REST PEP mapping
	// (pkg/api/rest/authz_pep_mapping.go) and the gRPC mapping
	// (pkg/api/grpc/authz_mapping.go) exactly. qrdb registers no account:*
	// family; when one is added these entries should move to it.
	"quicbaas_agent_account_create": {"credentials", "store"},
	"quicbaas_agent_account_get":    {"agent_accounts", "read"},
	"quicbaas_agent_account_list":   {"agent_accounts", "list"},
	"quicbaas_agent_account_update": {"credentials", "store"},
	"quicbaas_agent_account_delete": {"credentials", "delete"},
	"quicbaas_agent_account_rotate": {"credentials", "rotate"},
	// Materializing RELEASES the envelope, so it takes QuicBaaS's own
	// agent_accounts:materialize rather than a credentials:* grant — which is
	// registered ScopeCross (system tenant only) and would deny the
	// tenant-scoped fleet node daemon this verb exists for.
	"quicbaas_agent_account_materialize": {"agent_accounts", "materialize"},
	// The session reservation is the release's companion; see
	// pkg/api/grpc/authz_mapping.go.
	"quicbaas_agent_account_reserve_session": {"agent_accounts", "materialize"},
	"quicbaas_agent_account_release_session": {"agent_accounts", "materialize"},
	"quicbaas_agent_account_revoke":          {"credentials", "delete"},
	"quicbaas_agent_account_set_default":     {"credentials", "store"},
	"quicbaas_agent_account_usage":           {"agent_accounts", "read"},

	// Certificate extensions -> ca / certs. The mapping MUST match the gRPC
	// mapping (pkg/api/grpc/authz_mapping.go), the REST PEP mapping
	// (pkg/api/rest/authz_pep_mapping.go) and the QUIC sub-op mapping
	// (pkg/api/quic/handler_authz_qrdb.go) exactly.
	//
	// Cross-signing mints a CA certificate (basicConstraints CA:TRUE over a
	// foreign ROOT's key), which qrdb documents as strictly more privileged
	// than leaf issuance, so it authorizes with ca:create. The AIA chain walk
	// fetches and imports issuer certificates, the same mutation
	// quicbaas_trust_auto_import_aia performs, so it carries the same
	// write-strength verb. Device identity issuance mints END-ENTITY TCG
	// EK / AK / IDevID certificates and stays on certs:generate.
	"quicbaas_certificate_cross_sign":            {"ca", "create"},
	"quicbaas_certificate_chain":                 {"certs", "generate"},
	"quicbaas_certificate_issue_device_identity": {"certs", "generate"},

	// Ollama -> ollama
	"quicbaas_ollama_health":      {"ollama", "health"},
	"quicbaas_ollama_model_list":  {"ollama", "model_list"},
	"quicbaas_ollama_model_info":  {"ollama", "model_info"},
	"quicbaas_ollama_embed":       {"ollama", "embed"},
	"quicbaas_ollama_embed_batch": {"ollama", "embed"},

	// Versioned named-credential rotation -> credentials:*. One exact entry per
	// tool in credentialToolNames (tools_credentials.go), copied byte-for-byte
	// from the gRPC canon (pkg/api/grpc/authz_mapping.go
	// qrdb.v1.QRDBService/Credential* entries) and matching the /v1/credentials
	// REST rules.
	//
	// There is deliberately NO "qrdb_credential" prefix catch-all: it collapsed
	// submit / rotate / gc_expired onto credentials:store, and the shorter
	// sibling prefixes swallowed list_versions and retrieve_version, so five
	// distinct verbs authorized under a permission that was not theirs. All five
	// are registered qrdb permissions, so there is nothing to approximate.
	"qrdb_credential_submit":           {"credentials", "submit"},
	"qrdb_credential_strategy":         {"credentials", "strategy"},
	"qrdb_credential_delete":           {"credentials", "delete"},
	"qrdb_credential_list":             {"credentials", "list"},
	"qrdb_credential_rotate":           {"credentials", "rotate"},
	"qrdb_credential_retrieve_version": {"credentials", "retrieve_version"},
	"qrdb_credential_list_versions":    {"credentials", "list_versions"},
	"qrdb_credential_gc_expired":       {"credentials", "gc_expired"},

	// Certificate (base surface) -> certs. These six tools front exactly the
	// six quicbaas.certificate.v1.CertificateService RPCs, so they carry the
	// gRPC mapping's (resource, action) pairs verbatim
	// (pkg/api/grpc/authz_mapping.go): issuance and CSR signing MINT a leaf
	// certificate (certs:generate), revocation drives the CRL (certs:revoke),
	// and verification / CA-certificate / TLS-config retrieval are reads
	// (certs:read). Without these entries the default-deny gate hid the whole
	// base certificate surface from tools/list while the three certificate
	// EXTENSION tools below were visible.
	"quicbaas_certificate_issue":          {"certs", "generate"},
	"quicbaas_certificate_sign_csr":       {"certs", "generate"},
	"quicbaas_certificate_revoke":         {"certs", "revoke"},
	"quicbaas_certificate_verify":         {"certs", "read"},
	"quicbaas_certificate_get_ca":         {"certs", "read"},
	"quicbaas_certificate_get_tls_config": {"certs", "read"},

	// Initialization ceremony -> ceremony:*. One exact entry per tool, copied
	// byte-for-byte from the gRPC canon (pkg/api/grpc/authz_mapping.go
	// quicbaas.ceremony.v1.Ceremony and qrdb.v1.QRDBService Ceremony* entries)
	// and matching ceremonyGranularOps on REST
	// (pkg/api/rest/authz_pep_mapping.go).
	//
	// There is deliberately NO "qrdb_ceremony" prefix catch-all: it resolved the
	// entire WRITE surface to the read-strength ceremony:status, which qrdb
	// grants to the read-only Auditor role, while CeremonyInitialize /
	// CeremonySignCSR / CeremonyEnrollExternalOfficer are Security-Officer
	// exclusive. A reader could therefore mint the CA, sign arbitrary certs and
	// subvert the Shamir quorum. A future ceremony tool must fail closed here,
	// not inherit a read.
	"qrdb_ceremony_initialize":              {"ceremony", "initialize"},
	"qrdb_ceremony_status":                  {"ceremony", "status"},
	"qrdb_ceremony_claim_cert_begin":        {"ceremony", "claim_cert"},
	"qrdb_ceremony_claim_cert_complete":     {"ceremony", "claim_cert"},
	"qrdb_ceremony_begin_claim_share":       {"ceremony", "claim_share_begin"},
	"qrdb_ceremony_complete_claim_share":    {"ceremony", "claim_share_complete"},
	"qrdb_ceremony_sign_csr":                {"ceremony", "sign_csr"},
	"qrdb_ceremony_enroll_external_officer": {"ceremony", "enroll_custodian"},
	// Officer listing is a read (quicbaas.ceremony.v1.Ceremony/
	// CeremonyListOfficers, and the GET /officers REST rule). It keeps the
	// quicbaas_ prefix because it has no qrdb mirror.
	"quicbaas_ceremony_list_officers": {"ceremony", "status"},

	// Cache configuration -> admin, matching
	// /quicbaas.v1.QuicBaaS/GetCacheConfig and UpdateCacheConfig.
	"quicbaas_cache_config_get":    {"admin", "status"},
	"quicbaas_cache_config_update": {"admin", "capabilities"},

	// Fleet node lifecycle admin. Mirrors the REST rules for
	// POST /api/v1/fleet/{nodeID}/disable and /enable: qrdb registers no
	// orchestrator:agent_disable / orchestrator:agent_enable, so both
	// transports use the nearest registered pair.
	"quicbaas_fleet_disable": {"orchestrator", "agent_remove"},
	"quicbaas_fleet_enable":  {"orchestrator", "agent_register"},

	// PEP access checks -> permissions:resolve, matching the REST rules for
	// POST /api/v1/iam/check and /check/batch.
	"quicbaas_pep_check":       {"permissions", "resolve"},
	"quicbaas_pep_check_batch": {"permissions", "resolve"},

	// KMS key lifecycle -> keys / backends. One entry per tool, taken from the
	// quicbaas.kms.v1.KeyManagement gRPC block verbatim, so a KMS grant
	// authorizes the same verb on gRPC and MCP. Reads stay on keys:get /
	// keys:list; state mutations that are not create/delete use keys:rotate,
	// exactly as gRPC does.
	"quicbaas_kms_generate_key":             {"keys", "create"},
	"quicbaas_kms_import_key":               {"keys", "import"},
	"quicbaas_kms_export_key":               {"keys", "export"},
	"quicbaas_kms_unwrap_key":               {"keys", "unwrap"},
	"quicbaas_kms_get_key":                  {"keys", "get"},
	"quicbaas_kms_get_public_key":           {"keys", "get"},
	"quicbaas_kms_get_metadata":             {"keys", "get"},
	"quicbaas_kms_get_policy":               {"keys", "get"},
	"quicbaas_kms_get_rotation_policy":      {"keys", "get"},
	"quicbaas_kms_safety_check":             {"keys", "get"},
	"quicbaas_kms_safety_report":            {"keys", "get"},
	"quicbaas_kms_list_keys":                {"keys", "list"},
	"quicbaas_kms_key_versions":             {"keys", "list"},
	"quicbaas_kms_get_supported_algorithms": {"keys", "list"},
	"quicbaas_kms_rotate_key":               {"keys", "rotate"},
	"quicbaas_kms_set_policy":               {"keys", "rotate"},
	"quicbaas_kms_set_rotation_policy":      {"keys", "rotate"},
	"quicbaas_kms_set_key_safety":           {"keys", "rotate"},
	"quicbaas_kms_clear_key_safety":         {"keys", "rotate"},
	"quicbaas_kms_update_metadata":          {"keys", "rotate"},
	"quicbaas_kms_key_status_update":        {"keys", "rotate"},
	"quicbaas_kms_delete_key":               {"keys", "delete"},
	"quicbaas_kms_revoke_key":               {"keys", "delete"},
	"quicbaas_kms_delete_rotation_policy":   {"keys", "delete"},
	"quicbaas_kms_encrypt":                  {"keys", "encrypt"},
	"quicbaas_kms_decrypt":                  {"keys", "decrypt"},
	"quicbaas_kms_backend_status":           {"backends", "read"},
	"quicbaas_kms_backend_test":             {"backends", "read"},

	// Crypto -> keys, taken from the quicbaas.kms.v1.Crypto gRPC block
	// verbatim. Data-key generation derives new key material (keys:derive),
	// the wrapped variant additionally wraps it (keys:wrap), rewrap unwraps
	// under a new KEK (keys:unwrap), and the HMAC / hash verbs reuse the
	// keys:sign / keys:verify pair gRPC assigns them.
	"quicbaas_crypto_encrypt":         {"keys", "encrypt"},
	"quicbaas_crypto_encrypt_batch":   {"keys", "encrypt"},
	"quicbaas_crypto_decrypt":         {"keys", "decrypt"},
	"quicbaas_crypto_decrypt_batch":   {"keys", "decrypt"},
	"quicbaas_crypto_sign":            {"keys", "sign"},
	"quicbaas_crypto_hmac":            {"keys", "sign"},
	"quicbaas_crypto_hash":            {"keys", "sign"},
	"quicbaas_crypto_verify":          {"keys", "verify"},
	"quicbaas_crypto_verify_hmac":     {"keys", "verify"},
	"quicbaas_crypto_rewrap":          {"keys", "unwrap"},
	"quicbaas_crypto_datakey":         {"keys", "derive"},
	"quicbaas_crypto_datakey_wrapped": {"keys", "wrap"},
	"quicbaas_crypto_random":          {"keys", "derive"},

	// Per-path DEK management -> keys, taken from the
	// quicbaas.kms.v1.DEKManagement gRPC block verbatim.
	"quicbaas_dek_create":                    {"keys", "create"},
	"quicbaas_dek_get":                       {"keys", "get"},
	"quicbaas_dek_status":                    {"keys", "get"},
	"quicbaas_dek_get_reencryption_progress": {"keys", "get"},
	"quicbaas_dek_list":                      {"keys", "list"},
	"quicbaas_dek_rotate":                    {"keys", "rotate"},
	"quicbaas_dek_set_default":               {"keys", "rotate"},
	"quicbaas_dek_set_safety":                {"keys", "rotate"},
	"quicbaas_dek_clear_safety":              {"keys", "rotate"},
	"quicbaas_dek_start_reencryption":        {"keys", "rotate"},
	"quicbaas_dek_delete":                    {"keys", "delete"},

	// The tenant secret vault -> secrets, taken from the
	// quicbaas.kms.v1.SecretsManagement gRPC block verbatim
	// (pkg/api/grpc/authz_mapping.go) and agreeing with the eight REST
	// "/api/v1/secrets" routes.
	//
	// NOT credentials:*. That family guards qrdb's named-credential store,
	// which has no tenant namespacing and is therefore registered ScopeCross —
	// system tenant only — while every tool below reads or writes under the
	// CALLER's tenant (pkg/service/secrets_tenant.go secretsTenantScopedID).
	// Mapped to credentials:* the vault was unreachable for every tenant
	// identity on this transport (go-quicbaas#534, #682); the family that
	// describes it is the ScopeTenantOnly secrets:* family qrdb declares for
	// exactly this consumer (go-qrdb/pkg/permission/broker_scope.go).
	//
	// The action split is the sibling transports' split: list enumerates names,
	// read returns secret material or its metadata (a version listing and the
	// prefix / watch fetches return values), write is every mutation, delete is
	// removal. TestSecretsVaultMCPPermissions pins it.
	"quicbaas_secrets_store":               {"secrets", "write"},
	"quicbaas_secrets_ensure":              {"secrets", "write"},
	"quicbaas_secrets_get":                 {"secrets", "read"},
	"quicbaas_secrets_list":                {"secrets", "list"},
	"quicbaas_secrets_get_prefix":          {"secrets", "read"},
	"quicbaas_secrets_watch":               {"secrets", "read"},
	"quicbaas_secrets_get_rotation_policy": {"secrets", "read"},
	"quicbaas_secrets_versions":            {"secrets", "read"},
	"quicbaas_secrets_rotate":              {"secrets", "write"},
	"quicbaas_secrets_set_rotation_policy": {"secrets", "write"},
	"quicbaas_secrets_rollback":            {"secrets", "write"},
	"quicbaas_secrets_update_status":       {"secrets", "write"},
	"quicbaas_secrets_delete":              {"secrets", "delete"},

	// Messaging -> messaging, taken from the
	// quicbaas.messaging.v1.MessagingService gRPC block verbatim.
	"quicbaas_msg_create_channel": {"messaging", "channel_create"},
	"quicbaas_msg_get_channel":    {"messaging", "channel_read"},
	"quicbaas_msg_list_channels":  {"messaging", "channel_list"},
	"quicbaas_msg_delete_channel": {"messaging", "channel_delete"},
	"quicbaas_msg_publish":        {"messaging", "publish"},
	"quicbaas_msg_publish_delta":  {"messaging", "publish"},
	"quicbaas_msg_subscribe":      {"messaging", "subscribe"},
	"quicbaas_msg_unsubscribe":    {"messaging", "unsubscribe"},
	"quicbaas_msg_stats":          {"messaging", "stats"},
	"quicbaas_msg_dedup_stats":    {"messaging", "stats"},

	// Vector index administration and the namespace / stats verbs that the
	// prefix table would otherwise resolve too coarsely. Values come from the
	// quicbaas.vector.v1.VectorAdmin and VectorDB gRPC blocks verbatim, so a
	// principal holding only vector:get cannot read cluster statistics on MCP
	// while gRPC requires vector:get_index_stats.
	"quicbaas_vector_admin_get_cluster_stats":       {"vector", "get_index_stats"},
	"quicbaas_vector_admin_compact_namespace":       {"vector", "compact"},
	"quicbaas_vector_admin_get_compaction_progress": {"vector", "get_compaction_progress"},
	"quicbaas_vector_admin_export_vectors":          {"vector", "list"},
	"quicbaas_vector_admin_import_vectors":          {"vector", "batch_upsert"},
	"quicbaas_vector_update_namespace":              {"vector", "namespace_create"},
	"quicbaas_vector_get_stats":                     {"vector", "get_index_stats"},
	"quicbaas_vector_get_index_progress":            {"vector", "get_rebuild_progress"},
	"quicbaas_vector_health":                        {"vector", "namespace_list"},

	// Embedding -> embedding. qrdb registers embedding:embed (write) and
	// embedding:list_models (read); the REST surface
	// (pkg/api/rest/authz_pep_mapping.go "/api/v1/embedding") assigns exactly
	// those two verbs, and provider inspection / default selection are reads of
	// the provider inventory.
	"quicbaas_embedding_generate":       {"embedding", "embed"},
	"quicbaas_embedding_batch":          {"embedding", "embed"},
	"quicbaas_embedding_cache_clear":    {"embedding", "embed"},
	"quicbaas_embedding_list_providers": {"embedding", "list_models"},
	"quicbaas_embedding_get_provider":   {"embedding", "list_models"},
	"quicbaas_embedding_set_default":    {"embedding", "list_models"},
	"quicbaas_embedding_cache_stats":    {"embedding", "list_models"},

	// RAG -> rag, matching the REST rules for /api/v1/rag
	// (pkg/api/rest/authz_pep_mapping.go): retrieval and context assembly are
	// rag:query, indexing is rag:ingest, and the index-status probe is a read
	// of the same surface (rag:query, the REST GET verb for /api/v1/rag).
	"quicbaas_rag_query":         {"rag", "query"},
	"quicbaas_rag_build_context": {"rag", "query"},
	"quicbaas_rag_index_status":  {"rag", "query"},
	"quicbaas_rag_index_file":    {"rag", "ingest"},
	"quicbaas_rag_index_project": {"rag", "ingest"},

	// IAM sub-surfaces that have a REST rule. Values mirror
	// pkg/api/rest/authz_pep_mapping.go exactly:
	//
	//   users / roles / tenants CRUD  -> the same qrdb family REST assigns to
	//                                    /api/v1/users, /roles and /tenants;
	//   OAuth2 apps                   -> tenants:* (REST administers apps under
	//                                    /api/v1/tenants/{id}/apps);
	//   login sessions                -> users:read / users:update (no
	//                                    session:* family is registered);
	//   impersonation                 -> users:read / users:update (no
	//                                    impersonation:* family is registered);
	//   audit                         -> audit:query (filtered list, and the
	//                                    retention prune, which has no
	//                                    audit:delete to require) / audit:read;
	//   delegated-admin assignments   -> roles:read / list / assign / unassign;
	//   permission checks             -> permissions:resolve.
	"quicbaas_iam_user_create":      {"users", "create"},
	"quicbaas_iam_user_get":         {"users", "read"},
	"quicbaas_iam_user_list":        {"users", "list"},
	"quicbaas_iam_user_update":      {"users", "update"},
	"quicbaas_iam_user_delete":      {"users", "delete"},
	"quicbaas_iam_user_assign_role": {"roles", "assign"},
	"quicbaas_iam_user_revoke_role": {"roles", "unassign"},

	// WebAuthn credential administration. These are admin verbs over
	// another user's authenticators, so they carry the same user-admin
	// grants the neighbouring user verbs do — read to see what is stored,
	// delete to remove it — rather than a WebAuthn-ceremony permission,
	// which belongs to the self-service enrollment path.
	"quicbaas_iam_webauthn_credentials_list":       {"users", "read"},
	"quicbaas_iam_webauthn_credential_delete":      {"users", "delete"},
	"quicbaas_iam_webauthn_credentials_delete_all": {"users", "delete"},

	"quicbaas_iam_role_create": {"roles", "create"},
	"quicbaas_iam_role_get":    {"roles", "read"},
	"quicbaas_iam_role_list":   {"roles", "list"},
	"quicbaas_iam_role_update": {"roles", "update"},
	"quicbaas_iam_role_delete": {"roles", "delete"},

	// Built-in role reconciliation carries the role-CREATE grant rather than a
	// verb of its own. A dedicated "roles:reconcile" would be absent from every
	// role seeded before it existed, so nobody would hold the grant needed to
	// repair that exact absence — go-qrdb's guidance on issue #573. Whoever may
	// author a role may converge the built-in ones.
	"quicbaas_iam_role_reconcile": {"roles", "create"},

	"quicbaas_iam_tenant_create": {"tenants", "create"},
	"quicbaas_iam_tenant_get":    {"tenants", "read"},
	"quicbaas_iam_tenant_list":   {"tenants", "list"},
	"quicbaas_iam_tenant_update": {"tenants", "update"},
	"quicbaas_iam_tenant_delete": {"tenants", "delete"},

	"quicbaas_iam_app_register":      {"tenants", "create"},
	"quicbaas_iam_app_get":           {"tenants", "read"},
	"quicbaas_iam_app_list":          {"tenants", "list"},
	"quicbaas_iam_app_update":        {"tenants", "update"},
	"quicbaas_iam_app_rotate_secret": {"tenants", "update"},
	"quicbaas_iam_app_delete":        {"tenants", "delete"},

	"quicbaas_iam_session_get":             {"users", "read"},
	"quicbaas_iam_session_list":            {"users", "read"},
	"quicbaas_iam_session_touch":           {"users", "update"},
	"quicbaas_iam_session_revoke":          {"users", "update"},
	"quicbaas_iam_session_revoke_all":      {"users", "update"},
	"quicbaas_iam_session_revoke_others":   {"users", "update"},
	"quicbaas_iam_impersonation_start":     {"users", "update"},
	"quicbaas_iam_impersonation_end":       {"users", "update"},
	"quicbaas_iam_impersonation_list":      {"users", "read"},
	"quicbaas_iam_audit_query":             {"audit", "query"},
	"quicbaas_iam_audit_get":               {"audit", "read"},
	"quicbaas_iam_audit_delete":            {"audit", "query"},
	"quicbaas_iam_admin_assignment_create": {"roles", "assign"},
	"quicbaas_iam_admin_assignment_update": {"roles", "assign"},
	"quicbaas_iam_admin_assignment_delete": {"roles", "unassign"},
	"quicbaas_iam_admin_assignment_get":    {"roles", "read"},
	"quicbaas_iam_admin_assignment_list":   {"roles", "list"},
	"quicbaas_iam_admin_check_permission":  {"permissions", "resolve"},

	// go-xkms compatibility tools -> the same families the REST go-xkms
	// surface uses (pkg/api/rest/authz_pep_mapping.go xkmsOps), endpoint for
	// endpoint. Envelope seal/unseal on this surface is DATA sealing (the
	// request carries the plaintext), so it authorizes as keys:encrypt /
	// keys:decrypt and never as the seal:* barrier family — the same boundary
	// REST draws.
	"xkms.generateKey":  {"keys", "create"},
	"xkms.getKey":       {"keys", "get"},
	"xkms.listKeys":     {"keys", "list"},
	"xkms.deleteKey":    {"keys", "delete"},
	"xkms.rotateKey":    {"keys", "rotate"},
	"xkms.sign":         {"keys", "sign"},
	"xkms.verify":       {"keys", "verify"},
	"xkms.encrypt":      {"keys", "encrypt"},
	"xkms.decrypt":      {"keys", "decrypt"},
	"xkms.seal":         {"keys", "encrypt"},
	"xkms.unseal":       {"keys", "decrypt"},
	"xkms.canSeal":      {"keys", "list"},
	"xkms.listBackends": {"backends", "list"},

	"xkms.getCert":       {"certs", "read"},
	"xkms.certExists":    {"certs", "read"},
	"xkms.getCertChain":  {"certs", "read"},
	"xkms.listCerts":     {"certs", "list"},
	"xkms.saveCert":      {"certs", "generate"},
	"xkms.saveCertChain": {"certs", "generate"},
	"xkms.deleteCert":    {"certs", "revoke"},

	"xkms.ca.getBundle":   {"ca", "read"},
	"xkms.ca.getCert":     {"ca", "read"},
	"xkms.ca.isRevoked":   {"certs", "read"},
	"xkms.ca.signCSR":     {"certs", "generate"},
	"xkms.ca.issueCert":   {"certs", "generate"},
	"xkms.ca.revokeCert":  {"certs", "revoke"},
	"xkms.ca.generateCRL": {"certs", "revoke"},

	"xkms.piv.listSlots":   {"piv", "read"},
	"xkms.piv.getCert":     {"piv", "read"},
	"xkms.piv.exportCert":  {"piv", "read"},
	"xkms.piv.storeCert":   {"piv", "write"},
	"xkms.piv.importCert":  {"piv", "write"},
	"xkms.piv.generateKey": {"piv", "write"},
	"xkms.piv.deleteCert":  {"piv", "delete"},
	"xkms.piv.generateCSR": {"piv", "use"},

	// FIDO2 / WebAuthn ceremony. Registration IS authenticator (factor)
	// enrollment, so it carries the same user_authfactors:add the REST
	// canonical mount resolves (pkg/api/rest/authz_pep_mapping.go). The
	// authentication verbs stay behind authorization on MCP — REST and QUIC
	// treat them as pre-auth, but widening the UNAUTHENTICATED MCP tool
	// surface is an owner decision (see the quicbaas_login_begin precedent
	// in authz_qrdb_unmapped.go); authenticated callers use them for
	// step-up verification of an enrolled factor, a factor read.
	"xkms.fido2.beginRegistration":    {"user_authfactors", "add"},
	"xkms.fido2.finishRegistration":   {"user_authfactors", "add"},
	"xkms.fido2.beginAuthentication":  {"user_authfactors", "get"},
	"xkms.fido2.finishAuthentication": {"user_authfactors", "get"},

	"xkms.pin.verifySO":      {"pin", "read"},
	"xkms.pin.verifyUser":    {"pin", "read"},
	"xkms.pin.lockoutStatus": {"pin", "read"},
	"xkms.pin.setSO":         {"pin", "write"},
	"xkms.pin.setUser":       {"pin", "write"},
	"xkms.pin.changeSO":      {"pin", "write"},
	"xkms.pin.changeUser":    {"pin", "write"},
	"xkms.pin.resetLockout":  {"pin", "write"},
}

// qrdbToolPrefixMapping maps prefix-based tool groups to qrdb permissions. It is
// consulted after the exact map; the most specific surfaces are listed.
var qrdbToolPrefixMapping = map[string]qrdbToolResource{
	// Trust -> certs
	// Attestation gets its own resource vocabulary rather than borrowing
	// certs:*. The verbs answer "is this host and this workload in the
	// state we approved?", and driving a TPM quote is not a certificate
	// operation. Values MUST match pkg/api/grpc/authz_mapping.go and
	// pkg/api/quic/handler_authz_qrdb.go.
	"quicbaas_attestation_verify":           {"attestation", "verify"},
	"quicbaas_attestation_verify_chain":     {"attestation", "verify"},
	"quicbaas_attestation_attest_container": {"attestation", "attest"},
	// The Verifier: provision (Admin) manages what it trusts; verify reads
	// those records and appraises evidence against them.
	"quicbaas_attestation_put_endorsement":         {"attestation", "provision"},
	"quicbaas_attestation_list_endorsements":       {"attestation", "verify"},
	"quicbaas_attestation_delete_endorsement":      {"attestation", "provision"},
	"quicbaas_attestation_put_reference_values":    {"attestation", "provision"},
	"quicbaas_attestation_get_reference_values":    {"attestation", "verify"},
	"quicbaas_attestation_list_reference_values":   {"attestation", "verify"},
	"quicbaas_attestation_delete_reference_values": {"attestation", "provision"},
	"quicbaas_attestation_appraise_quote":          {"attestation", "verify"},

	"quicbaas_trust_cross_signed_root_pool": {"certs", "read"},
	"quicbaas_trust_auto_import_aia":        {"certs", "generate"},
	"quicbaas_trust_publish_dane":           {"certs", "generate"},

	// Seal
	"quicbaas_seal":   {"seal", "seal"},
	"quicbaas_unseal": {"seal", "unseal"},

	// Timeseries
	"quicbaas_timeseries_query":  {"timeseries", "query"},
	"quicbaas_timeseries_write":  {"timeseries", "write"},
	"quicbaas_timeseries_delete": {"timeseries", "delete_series"},
	"qrdb_ts_query":              {"timeseries", "query"},
	"qrdb_ts_list":               {"timeseries", "list_metrics"},
	"qrdb_ts_get":                {"timeseries", "get_retention"},
	"qrdb_ts_write":              {"timeseries", "write"},
	"qrdb_ts_set":                {"timeseries", "set_retention"},
	"qrdb_ts_delete":             {"timeseries", "delete_series"},

	// Vector (quicbaas + qrdb mirror)
	"quicbaas_vector_search":   {"vector", "search_similar"},
	"quicbaas_vector_get":      {"vector", "get"},
	"quicbaas_vector_upsert":   {"vector", "upsert"},
	"quicbaas_vector_insert":   {"vector", "upsert"},
	"quicbaas_vector_delete":   {"vector", "delete"},
	"qrdb_vector_search":       {"vector", "search_similar"},
	"qrdb_vector_get":          {"vector", "get"},
	"qrdb_vector_list":         {"vector", "list"},
	"qrdb_vector_upsert":       {"vector", "upsert"},
	"qrdb_vector_update":       {"vector", "update_metadata"},
	"qrdb_vector_batch_upsert": {"vector", "batch_upsert"},
	"qrdb_vector_create":       {"vector", "namespace_create"},
	"qrdb_vector_rebuild":      {"vector", "rebuild_index"},
	"qrdb_vector_delete":       {"vector", "delete"},
	"qrdb_vector_batch_delete": {"vector", "batch_delete"},

	// Per-tenant barrier -> tenant_barrier
	"qrdb_tenant_barrier_init":         {"tenant_barrier", "init"},
	"qrdb_tenant_barrier_unseal":       {"tenant_barrier", "unseal"},
	"qrdb_tenant_barrier_unseal_share": {"tenant_barrier", "unseal"},
	"qrdb_tenant_barrier_seal":         {"tenant_barrier", "seal"},
	"qrdb_tenant_barrier_status":       {"tenant_barrier", "status"},
}

// getToolQRDBPermission returns the qrdb-canonical (resource, action) for the
// tool and whether a mapping exists. Exact match is preferred over prefix match.
// Among prefix matches the LONGEST matching prefix wins, so a specific tool
// (e.g. "qrdb_tenant_barrier_unseal_share") resolves to its own entry rather
// than a shorter surface prefix (e.g. "qrdb_tenant_barrier_unseal"). Map
// iteration order is otherwise non-deterministic, so longest-prefix selection is
// required for a stable decision. An unmapped tool has ok=false: the
// default-deny authz gate denies it under RequireAuthz.
//
// The privileged barrier, ceremony and credential surfaces have no prefix
// catch-all at all: every tool carries its own exact entry, so a future tool on
// those surfaces fails closed instead of inheriting a read permission.
func getToolQRDBPermission(toolName string) (resource, action string, ok bool) {
	if m, found := qrdbToolExactMapping[toolName]; found {
		return m.resource, m.action, true
	}
	var (
		bestLen = -1
		best    qrdbToolResource
		found   bool
	)
	for prefix, m := range qrdbToolPrefixMapping {
		if strings.HasPrefix(toolName, prefix) && len(prefix) > bestLen {
			bestLen = len(prefix)
			best = m
			found = true
		}
	}
	if found {
		return best.resource, best.action, true
	}
	return "", "", false
}
