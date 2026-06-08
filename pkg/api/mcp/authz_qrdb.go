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
// rather than admitted. quicbaas-specific surfaces that the qrdb permission
// registry does NOT define a resource family for — KMS, crypto, orchestrator,
// project, agent, object storage, parameter store, embedding, ollama, RAG,
// dynamic secrets — are intentionally absent and denied under RequireAuthz.

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

	// Cache (admin capabilities)
	"quicbaas_cache_stats": {"admin", "status"},
	"quicbaas_cache_clear": {"admin", "capabilities"},

	// Aggregate / time-series
	"quicbaas_aggregate": {"timeseries", "aggregate"},

	// Auth (logout)
	"quicbaas_logout": {"user_authfactors", "login"},
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

	// Barrier (qrdb mirror) -> tenant_barrier
	"qrdb_barrier_status":          {"tenant_barrier", "status"},
	"qrdb_barrier_recovery_status": {"seal", "status"},
	"qrdb_barrier_root_token":      {"seal", "bootstrap"},
	"qrdb_barrier":                 {"tenant_barrier", "status"},

	// Ceremony (qrdb mirror) -> ceremony
	"qrdb_ceremony_status": {"ceremony", "status"},
	"qrdb_ceremony":        {"ceremony", "status"},
}

// getToolQRDBPermission returns the qrdb-canonical (resource, action) for the
// tool and whether a mapping exists. Exact match is preferred over prefix match.
// An unmapped tool has ok=false: the default-deny authz gate denies it under
// RequireAuthz.
func getToolQRDBPermission(toolName string) (resource, action string, ok bool) {
	if m, found := qrdbToolExactMapping[toolName]; found {
		return m.resource, m.action, true
	}
	for prefix, m := range qrdbToolPrefixMapping {
		if strings.HasPrefix(toolName, prefix) {
			return m.resource, m.action, true
		}
	}
	return "", "", false
}
