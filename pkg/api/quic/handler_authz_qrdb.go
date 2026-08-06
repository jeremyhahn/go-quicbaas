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
// opcode is denied rather than admitted.
//
// Stage 0.5 maps the broker-only data-plane surfaces that qrdb now registers a
// permission family for (go-qrdb/pkg/permission/broker_dataplane_scope.go):
// object storage (objstore), orchestrator, scheduler, parameters,
// dynamic_secrets, ollama. The scheduler and orchestrator opcodes are single
// dispatch opcodes (sub-op in payload[0]), so they authorize at the surface
// level.
//
// OpBarrier (0xC0) is a sub-op dispatch opcode: the first byte of the payload
// selects one of the 13 SubOpBarrier* operations, each of which requires its
// own distinct qrdb-canonical permission (see barrierSubOpPermissions below).
// getQRDBPermission resolves the payload sub-op rather than admitting the entire
// barrier surface under a single read-only permission. Unlike OpCeremony and
// OpOrchestrator — which authorize at the surface level — OpBarrier is
// sub-op-aware; the comment "(sub-ops enforced by the BarrierServicer facade)"
// was incorrect and has been removed.
//
// Surfaces still without a qrdb resource family — embedding, cache — remain
// intentionally absent and denied under RequireAuthz.

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

	// Ceremony dispatch (sub-op in payload; authorized at the surface level)
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
	// Secrets sub-dispatch (EnsureSecret and future sub-ops): authorized at the
	// surface level as a write; the underlying EnsureSecret merges read + write.
	OpSecretsSubDispatch: {"credentials", "store"},

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

	// ---- Stage 0.5: broker data-plane surfaces (qrdb-registered) ----

	// Object storage -> objstore
	OpOSPutObject:            {"objstore", "put"},
	OpOSGetObject:            {"objstore", "get"},
	OpOSDeleteObject:         {"objstore", "delete"},
	OpOSHeadObject:           {"objstore", "head"},
	OpOSListObjects:          {"objstore", "list"},
	OpOSListByTag:            {"objstore", "list"},
	OpOSListByTenant:         {"objstore", "list"},
	OpOSCopyObject:           {"objstore", "copy"},
	OpOSReplicateObject:      {"objstore", "replicate"},
	OpOSMigrateObject:        {"objstore", "migrate"},
	OpOSArchiveObject:        {"objstore", "archive"},
	OpOSRestoreObject:        {"objstore", "restore"},
	OpOSVerifyObject:         {"objstore", "verify"},
	OpOSGetObjectMetadata:    {"objstore", "head"},
	OpOSUpdateObjectMetadata: {"objstore", "put"},
	OpOSGetStorageStats:      {"objstore", "read_stats"},
	OpOSSetLifecyclePolicy:   {"objstore", "write_policy"},
	OpOSGetLifecyclePolicy:   {"objstore", "read_policy"},
	OpOSGetObjectPolicy:      {"objstore", "read_policy"},
	OpOSListBackends:         {"objstore", "read_backend"},
	OpOSGetBackendHealth:     {"objstore", "read_backend"},
	OpOSSetTenantQuotas:      {"objstore", "tenant_quota_write"},
	OpOSBatchPutObjects:      {"objstore", "batch"},
	OpOSBatchGetObjects:      {"objstore", "batch"},
	OpOSBatchDeleteObjects:   {"objstore", "batch"},
	OpOSAdmin:                {"objstore", "admin"},

	// Ollama -> ollama
	OpOllamaHealth:         {"ollama", "health"},
	OpOllamaListModels:     {"ollama", "model_list"},
	OpOllamaGetModelInfo:   {"ollama", "model_info"},
	OpOllamaEmbed:          {"ollama", "embed"},
	OpOllamaEmbedWithModel: {"ollama", "embed"},
	OpOllamaEmbedBatch:     {"ollama", "embed"},

	// Dynamic secrets -> dynamic_secrets
	OpDynamicSecretsCreateRole:  {"dynamic_secrets", "role_create"},
	OpDynamicSecretsGetRole:     {"dynamic_secrets", "role_read"},
	OpDynamicSecretsListRoles:   {"dynamic_secrets", "role_list"},
	OpDynamicSecretsDeleteRole:  {"dynamic_secrets", "role_delete"},
	OpDynamicSecretsGenerate:    {"dynamic_secrets", "generate"},
	OpDynamicSecretsRenewLease:  {"dynamic_secrets", "lease_renew"},
	OpDynamicSecretsRevokeLease: {"dynamic_secrets", "lease_revoke"},
	OpDynamicSecretsListLeases:  {"dynamic_secrets", "lease_list"},

	// Parameter store -> parameters
	OpParameterGet:         {"parameters", "read"},
	OpParameterSet:         {"parameters", "write"},
	OpParameterDelete:      {"parameters", "delete"},
	OpParameterList:        {"parameters", "list"},
	OpParameterBatchGet:    {"parameters", "read"},
	OpParameterBatchSet:    {"parameters", "write"},
	OpParameterBatchDelete: {"parameters", "delete"},
	OpParameterSearch:      {"parameters", "search"},

	// Scheduler dispatch (sub-op in payload; authorized at the surface level)
	OpScheduler: {"scheduler", "status"},

	// Orchestrator dispatch (sub-op in payload; authorized at the surface level)
	OpOrchestrator: {"orchestrator", "status"},
}

// barrierSubOpPermissions maps each SubOpBarrier* constant to the
// qrdb-canonical permission it requires. The values MUST match
// go-qrdb/pkg/permission/constants.go and the gRPC authz mapping
// (pkg/api/grpc/authz_mapping.go lines 259-275) byte-for-byte.
//
// This table is the authoritative permission spec for the OpBarrier surface
// on the QUIC transport. A sub-op absent from this map results in ok=false
// (default-deny). Every key here must have a corresponding entry in
// barrierSubOpHandlers (handler_barrier.go) and vice versa.
var barrierSubOpPermissions = map[byte]qrdbPermission{
	SubOpBarrierStatus:                   {"tenant_barrier", "status"},
	SubOpBarrierInitialize:               {"tenant_barrier", "init"},
	SubOpBarrierUnseal:                   {"tenant_barrier", "unseal"},
	SubOpBarrierSeal:                     {"tenant_barrier", "seal"},
	SubOpBarrierShamirInitialize:         {"seal", "initialize_shamir"},
	SubOpBarrierShamirUnsealShare:        {"barrier", "unseal-share"},
	SubOpBarrierShamirUnsealShares:       {"barrier", "unseal-share"},
	SubOpBarrierRekey:                    {"seal", "rekey"},
	SubOpBarrierRecoveryGenerate:         {"seal", "generate_recovery"},
	SubOpBarrierRecoveryRecover:          {"seal", "recover_with_keys"},
	SubOpBarrierRecoveryDelete:           {"seal", "generate_recovery"},
	SubOpBarrierRecoveryStatus:           {"seal", "status"},
	SubOpBarrierRootToken:                {"seal", "bootstrap"},
	SubOpBarrierEnableEncryption:         {"seal", "enable_encryption"},
	SubOpBarrierUpdateTenantQuotas:       {"tenants", "set_quotas"},
	SubOpBarrierUpdateTenantEntitlements: {"tenants", "set_entitlements"},
}

// getQRDBPermission returns the qrdb-canonical (resource, action) for the
// opcode and whether a mapping exists. An unmapped opcode has ok=false: the
// default-deny authz gate denies it under RequireAuthz. IAM opcodes are
// resolved via the build-tag-conditional getIAMQRDBPermission.
//
// For OpBarrier the resolution is sub-op-aware: payload[0] selects the
// sub-operation, which is looked up in barrierSubOpPermissions. An empty
// payload or an unknown sub-op results in ok=false (default-deny). All other
// dispatch opcodes (OpCeremony, OpScheduler, OpOrchestrator) authorize at
// the surface level and the payload parameter is ignored for them.
func getQRDBPermission(opcode byte, payload []byte) (resource, action string, ok bool) {
	if opcode == OpBarrier {
		if len(payload) < 1 {
			return "", "", false
		}
		if perm, found := barrierSubOpPermissions[payload[0]]; found {
			return perm.resource, perm.action, true
		}
		return "", "", false
	}
	if perm, found := quicOpcodePermissions[opcode]; found {
		return perm.resource, perm.action, true
	}
	if res, act, handled := getIAMQRDBPermission(opcode); handled {
		return res, act, true
	}
	return "", "", false
}
