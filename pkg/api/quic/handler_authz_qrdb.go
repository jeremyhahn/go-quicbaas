//go:build quic

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

package quic

// This file maps QUIC opcodes onto qrdb-canonical (resource, action) permission
// tuples. The qrdb policy decision point joins the tuple into the registered
// "resource:action" permission (e.g. "kv:put"); the vocabulary here MUST match
// go-qrdb/pkg/permission/constants.go exactly. It mirrors the REST PEP mapping
// (pkg/api/rest/authz_pep_mapping.go) and the gRPC method mapping, which
// established the broker resource -> qrdb resource translation (kv, keys, certs,
// credentials, tenant_barrier, users, roles, tenants, timeseries, vector,
// transaction, batch, watch, backup, seal, ceremony, admin).
//
// An opcode that has no entry is reported unmapped (ok=false): under
// RequireAuthz the QUIC authz gate is default-deny, so an unmapped protected
// opcode is denied rather than admitted. quicbaas-specific surfaces that the
// qrdb permission registry does NOT define a resource family for — object
// storage, orchestrator, parameter store, embedding, ollama, dynamic secrets,
// cache, messaging — are intentionally absent and denied under RequireAuthz.

type qrdbPermission struct {
	resource string
	action   string
}

// quicOpcodePermissions maps each protected opcode to the qrdb-canonical
// permission it requires. Every value is a registered qrdb permission.
var quicOpcodePermissions = map[byte]qrdbPermission{
	// KV
	OpGet:    {"kv", "get"},
	OpList:   {"kv", "list"},
	OpPut:    {"kv", "put"},
	OpDelete: {"kv", "delete"},
	OpQuery:  {"kv", "range"},

	// Batch
	OpBatchGet:    {"batch", "execute"},
	OpBatchPut:    {"batch", "execute"},
	OpBatchDelete: {"batch", "execute"},

	// Index
	OpScanIndex: {"index", "scan"},

	// Transaction
	OpTransaction: {"transaction", "execute"},

	// Cluster admin
	OpClusterStatus:      {"admin", "status"},
	OpGetLeader:          {"admin", "status"},
	OpListNodes:          {"admin", "peers"},
	OpRebalanceStatus:    {"rebalance", "report"},
	OpRebalancePlan:      {"rebalance", "plan"},
	OpAddNode:            {"admin", "add_member"},
	OpRemoveNode:         {"admin", "remove_member"},
	OpCreateShard:        {"admin", "start_shard"},
	OpRebalanceCluster:   {"rebalance", "execute"},
	OpCancelMigration:    {"admin", "drain"},
	OpNodeReplaceRequest: {"admin", "add_member"},

	// Backup
	OpListBackups:   {"backup", "list"},
	OpCreateBackup:  {"backup", "create"},
	OpRestoreBackup: {"backup", "restore"},
	OpVerifyBackup:  {"backup", "verify"},
	OpDeleteBackup:  {"backup", "delete"},

	// Auth (token lifecycle)
	OpLogout:        {"user_authfactors", "login"},
	OpRefreshToken:  {"user_authfactors", "login"},
	OpValidateToken: {"user_authfactors", "login"},

	// RBAC users/roles
	OpListUsers:  {"users", "list"},
	OpListRoles:  {"roles", "list"},
	OpCreateUser: {"users", "create"},
	OpAssignRole: {"roles", "assign"},
	OpDeleteUser: {"users", "delete"},
	OpRevokeRole: {"roles", "unassign"},
	OpCreateRole: {"roles", "create"},
	OpDeleteRole: {"roles", "delete"},

	// Time-series
	OpAggregate:            {"timeseries", "aggregate"},
	OpTimeSeriesQuery:      {"timeseries", "query"},
	OpTSQuery:              {"timeseries", "query"},
	OpTSGetRetentionPolicy: {"timeseries", "get_retention"},
	OpTSListMetrics:        {"timeseries", "list_metrics"},
	OpTSWritePoint:         {"timeseries", "write_point"},
	OpTSWriteBatch:         {"timeseries", "write_batch"},
	OpTSSetRetentionPolicy: {"timeseries", "set_retention"},
	OpTSDeleteSeries:       {"timeseries", "delete_series"},

	// Operations management
	OpGetMigrationProgress: {"admin", "status"},
	OpGetUpgradeStatus:     {"admin", "status"},
	OpMigrateData:          {"admin", "drain"},
	OpReplaceNode:          {"admin", "add_member"},
	OpUpgradeNode:          {"admin", "drain"},

	// Barrier dispatch (top-level; sub-ops enforced by the BarrierServicer facade)
	OpBarrier: {"tenant_barrier", "status"},

	// Ceremony dispatch
	OpCeremony: {"ceremony", "status"},

	// KMS -> keys
	OpKMSGetKey:      {"keys", "get"},
	OpKMSListKeys:    {"keys", "list"},
	OpKMSKeyVersions: {"keys", "list"},
	OpKMSGenerateKey: {"keys", "create"},
	OpKMSImportKey:   {"keys", "import"},
	OpKMSRotateKey:   {"keys", "rotate"},
	OpKMSEncrypt:     {"keys", "encrypt"},
	OpKMSDecrypt:     {"keys", "decrypt"},
	OpKMSKeyStatus:   {"keys", "get"},
	OpKMSDeleteKey:   {"keys", "delete"},

	// KMS DEK -> keys
	OpKMSDEKGet:                  {"keys", "get"},
	OpKMSDEKList:                 {"keys", "list"},
	OpKMSDEKStatus:               {"keys", "get"},
	OpKMSDEKGetReencryptProgress: {"keys", "get"},
	OpKMSDEKCreate:               {"keys", "create"},
	OpKMSDEKRotate:               {"keys", "rotate"},
	OpKMSDEKSetDefault:           {"keys", "rotate"},
	OpKMSDEKSetSafety:            {"keys", "rotate"},
	OpKMSDEKClearSafety:          {"keys", "rotate"},
	OpKMSDEKStartReencryption:    {"keys", "rotate"},
	OpKMSDEKDelete:               {"keys", "delete"},

	// KMS backend -> backends
	OpKMSBackendStatus: {"backends", "read"},
	OpKMSBackendTest:   {"backends", "read"},

	// Secrets -> credentials
	OpSecretsGet:          {"credentials", "retrieve"},
	OpSecretsList:         {"credentials", "list"},
	OpSecretsListVersions: {"credentials", "list_versions"},
	OpSecretsGetVersion:   {"credentials", "retrieve_version"},
	OpSecretsGetRotation:  {"credentials", "list"},
	OpSecretsGetByPrefix:  {"credentials", "list"},
	OpSecretsWatch:        {"credentials", "list"},
	OpSecretsStore:        {"credentials", "store"},
	OpSecretsRollback:     {"credentials", "submit"},
	OpSecretsRotate:       {"credentials", "rotate"},
	OpSecretsSetRotation:  {"credentials", "strategy"},
	OpSecretsUpdateStatus: {"credentials", "submit"},
	OpSecretsDelete:       {"credentials", "delete"},

	// Crypto -> keys
	OpCryptoEncrypt:    {"keys", "encrypt"},
	OpCryptoDecrypt:    {"keys", "decrypt"},
	OpCryptoRewrap:     {"keys", "unwrap"},
	OpCryptoSign:       {"keys", "sign"},
	OpCryptoVerify:     {"keys", "verify"},
	OpCryptoHash:       {"keys", "sign"},
	OpCryptoRandom:     {"keys", "derive"},
	OpCryptoDataKey:    {"keys", "derive"},
	OpCryptoHMAC:       {"keys", "sign"},
	OpCryptoVerifyHMAC: {"keys", "verify"},

	// Vector
	OpVectorListNamespaces:        {"vector", "namespace_list"},
	OpVectorGetNamespaceInfo:      {"vector", "namespace_info"},
	OpVectorGet:                   {"vector", "get"},
	OpVectorSearchSimilar:         {"vector", "search_similar"},
	OpVectorSearchByMetadata:      {"vector", "search_by_metadata"},
	OpVectorHybridSearch:          {"vector", "hybrid_search"},
	OpVectorSearchByID:            {"vector", "search_by_id"},
	OpVectorGetRebuildProgress:    {"vector", "get_rebuild_progress"},
	OpVectorGetIndexStats:         {"vector", "get_index_stats"},
	OpVectorGetCompactionProgress: {"vector", "get_compaction_progress"},
	OpVectorCreateNamespace:       {"vector", "namespace_create"},
	OpVectorUpsert:                {"vector", "upsert"},
	OpVectorBatchUpsert:           {"vector", "batch_upsert"},
	OpVectorRebuildIndex:          {"vector", "rebuild_index"},
	OpVectorCompact:               {"vector", "compact"},
	OpVectorDeleteNamespace:       {"vector", "namespace_delete"},
	OpVectorDelete:                {"vector", "delete"},

	// Certificate -> certs
	OpCertVerify:       {"certs", "read"},
	OpCertGetCA:        {"certs", "read"},
	OpCertGetTLSConfig: {"certs", "read"},
	OpCertIssue:        {"certs", "generate"},
	OpCertSignCSR:      {"certs", "generate"},
	OpCertRevoke:       {"certs", "revoke"},

	// Trust -> certs
	OpTrustCrossSignedRootPool: {"certs", "read"},
	OpTrustAutoImportAIA:       {"certs", "generate"},
	OpTrustPublishDANE:         {"certs", "generate"},
}

// getQRDBPermission returns the qrdb-canonical (resource, action) for the opcode
// and whether a mapping exists. An unmapped opcode has ok=false: the default-deny
// authz gate denies it under RequireAuthz. IAM opcodes are resolved via the
// build-tag-conditional getIAMQRDBPermission.
func getQRDBPermission(opcode byte) (resource, action string, ok bool) {
	if perm, found := quicOpcodePermissions[opcode]; found {
		return perm.resource, perm.action, true
	}
	if res, act, handled := getIAMQRDBPermission(opcode); handled {
		return res, act, true
	}
	return "", "", false
}
