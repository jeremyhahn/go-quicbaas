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
// Surfaces the qrdb permission registry still does NOT define a resource family
// for — KMS, crypto, agent, embedding, RAG — remain intentionally absent and
// denied under RequireAuthz.
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
	// KV (quicbaas + qrdb mirror)
	"quicbaas_get":          {"kv", "get"},
	"quicbaas_list":         {"kv", "list"},
	"quicbaas_query":        {"kv", "range"},
	"quicbaas_scan":         {"kv", "range"},
	"quicbaas_scan_index":   {"index", "scan"},
	"quicbaas_batch_get":    {"batch", "execute"},
	"quicbaas_put":          {"kv", "put"},
	"quicbaas_batch_put":    {"batch", "execute"},
	"quicbaas_set":          {"kv", "put"},
	"quicbaas_delete":       {"kv", "delete"},
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

	// User / role management (quicbaas + qrdb mirror)
	"quicbaas_user_create": {"users", "create"},
	"quicbaas_user_delete": {"users", "delete"},
	"quicbaas_user_list":   {"users", "list"},
	"quicbaas_role_create": {"roles", "create"},
	"quicbaas_role_delete": {"roles", "delete"},
	"quicbaas_role_list":   {"roles", "list"},
	"quicbaas_role_assign": {"roles", "assign"},
	"quicbaas_role_revoke": {"roles", "unassign"},
	"qrdb_user_create":     {"users", "create"},
	"qrdb_user_delete":     {"users", "delete"},
	"qrdb_user_list":       {"users", "list"},
	"qrdb_role_create":     {"roles", "create"},
	"qrdb_role_delete":     {"roles", "delete"},
	"qrdb_role_list":       {"roles", "list"},
	"qrdb_role_assign":     {"roles", "assign"},

	// Barrier (qrdb mirror) — exact per-operation mapping, mirroring gRPC
	// authz_mapping.go lines 259-276. Each tool gets its own minimum-privilege
	// permission; the former "qrdb_barrier" prefix catch-all is gone so an
	// unmapped future barrier tool is default-denied.
	"qrdb_barrier_status":               {"tenant_barrier", "status"},
	"qrdb_barrier_initialize":           {"tenant_barrier", "init"},
	"qrdb_barrier_unseal":               {"tenant_barrier", "unseal"},
	"qrdb_barrier_seal":                 {"tenant_barrier", "seal"},
	"qrdb_barrier_shamir_initialize":    {"seal", "initialize_shamir"},
	"qrdb_barrier_shamir_unseal_share":  {"barrier", "unseal-share"},
	"qrdb_barrier_shamir_unseal_shares": {"barrier", "unseal-share"},
	"qrdb_barrier_rekey":                {"seal", "rekey"},
	"qrdb_barrier_recovery_generate":    {"seal", "generate_recovery"},
	"qrdb_barrier_recovery_recover":     {"seal", "recover_with_keys"},
	"qrdb_barrier_recovery_delete":      {"seal", "generate_recovery"},
	"qrdb_barrier_recovery_status":      {"seal", "status"},
	"qrdb_barrier_root_token":           {"seal", "bootstrap"},

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
	"quicbaas_orchestrator_spawn":          {"orchestrator", "spawn"},
	"quicbaas_orchestrator_prompt":         {"orchestrator", "prompt"},
	"quicbaas_orchestrator_status":         {"orchestrator", "status"},
	"quicbaas_orchestrator_close":          {"orchestrator", "close"},
	"quicbaas_orchestrator_list":           {"orchestrator", "list"},
	"quicbaas_orchestrator_list_sessions":  {"orchestrator", "list"},
	"quicbaas_orchestrator_task_create":    {"orchestrator", "task_create"},
	"quicbaas_orchestrator_decompose":      {"orchestrator", "task_decompose"},
	"quicbaas_orchestrator_start_job":      {"orchestrator", "job_start"},
	"quicbaas_orchestrator_get_job_status": {"orchestrator", "job_read"},
	"quicbaas_orchestrator_list_jobs":      {"orchestrator", "job_list"},
	"quicbaas_orchestrator_cancel_job":     {"orchestrator", "job_cancel"},
	"quicbaas_orchestrator_list_backends":  {"orchestrator", "backend_list"},
	"quicbaas_orchestrator_discover":       {"orchestrator", "discover"},
	"quicbaas_orchestrator_health":         {"orchestrator", "health"},
	"quicbaas_orchestrator_inference":      {"orchestrator", "inference"},
	"quicbaas_orchestrator_register":       {"orchestrator", "agent_register"},
	"quicbaas_orchestrator_cache_get":      {"orchestrator", "cache_read"},
	"quicbaas_orchestrator_cache_put":      {"orchestrator", "cache_write"},
	"quicbaas_orchestrator_broadcast":      {"orchestrator", "broadcast"},

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
	"project_create":       {"project", "create"},
	"project_get":          {"project", "read"},
	"project_list":         {"project", "list"},
	"project_update":       {"project", "update"},
	"project_delete":       {"project", "delete"},
	"project_grant_agent":  {"project", "grant"},
	"project_revoke_agent": {"project", "revoke"},

	// Ollama -> ollama
	"quicbaas_ollama_health":      {"ollama", "health"},
	"quicbaas_ollama_model_list":  {"ollama", "model_list"},
	"quicbaas_ollama_model_info":  {"ollama", "model_info"},
	"quicbaas_ollama_embed":       {"ollama", "embed"},
	"quicbaas_ollama_embed_batch": {"ollama", "embed"},
}

// qrdbToolPrefixMapping maps prefix-based tool groups to qrdb permissions. It is
// consulted after the exact map; the most specific surfaces are listed.
var qrdbToolPrefixMapping = map[string]qrdbToolResource{
	// Trust -> certs
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

	// Credentials (qrdb mirror) -> credentials
	"qrdb_credential_list":     {"credentials", "list"},
	"qrdb_credential_retrieve": {"credentials", "retrieve"},
	"qrdb_credential_strategy": {"credentials", "strategy"},
	"qrdb_credential_delete":   {"credentials", "delete"},
	"qrdb_credential":          {"credentials", "store"},

	// Per-tenant barrier -> tenant_barrier
	"qrdb_tenant_barrier_init":         {"tenant_barrier", "init"},
	"qrdb_tenant_barrier_unseal":       {"tenant_barrier", "unseal"},
	"qrdb_tenant_barrier_unseal_share": {"tenant_barrier", "unseal"},
	"qrdb_tenant_barrier_seal":         {"tenant_barrier", "seal"},
	"qrdb_tenant_barrier_status":       {"tenant_barrier", "status"},

	// Ceremony (qrdb mirror) -> ceremony
	"qrdb_ceremony_status": {"ceremony", "status"},
	"qrdb_ceremony":        {"ceremony", "status"},
}

// getToolQRDBPermission returns the qrdb-canonical (resource, action) for the
// tool and whether a mapping exists. Exact match is preferred over prefix match.
// Among prefix matches the LONGEST matching prefix wins, so a specific tool
// (e.g. "qrdb_credential_list") resolves to its own entry rather than a shorter
// surface prefix (e.g. "qrdb_credential"). Map iteration order is otherwise
// non-deterministic, so longest-prefix selection is required for a stable
// decision. An unmapped tool has ok=false: the default-deny authz gate denies
// it under RequireAuthz.
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
