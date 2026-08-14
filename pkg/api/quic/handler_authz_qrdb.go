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
// OpBarrier (0xC0) and OpCeremony (0xC1) are sub-op dispatch opcodes: the first
// byte of the payload selects the operation, and each operation requires its own
// distinct qrdb-canonical permission (see barrierSubOpPermissions and
// ceremonySubOpPermissions below). getQRDBPermission resolves the payload sub-op
// rather than admitting an entire privileged surface under a single read-only
// permission. Authorizing OpCeremony at the surface level under ceremony:status
// was a privilege-escalation bug: qrdb grants ceremony:status to the read-only
// Auditor role, while CeremonyInitialize / CeremonySignCSR /
// CeremonyEnrollExternalOfficer are Security-Officer-exclusive, so a reader
// could mint the CA and subvert the Shamir quorum.
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

// ceremonySubOpPermissions maps each SubOpCeremony* constant to the
// qrdb-canonical permission it requires. The values MUST match
// go-qrdb/pkg/permission/constants.go and the gRPC authz mapping
// (pkg/api/grpc/authz_mapping.go quicbaas.ceremony.v1.Ceremony and
// qrdb.v1.QRDBService Ceremony* entries) byte-for-byte.
//
// This table is the authoritative permission spec for the OpCeremony surface on
// the QUIC transport. A sub-op absent from this map results in ok=false
// (default-deny). Every key here must have a corresponding entry in
// ceremonySubOpHandlers (handler_ceremony.go) and vice versa.
//
// The initialization ceremony mints the CA, the server TLS identity and the
// Security Officer credential, signs arbitrary CSRs from the ceremony CA, seals
// Shamir shares to caller-supplied keys and claims officer shares. Those verbs
// are Security-Officer-exclusive in qrdb, whereas ceremony:status is granted to
// the read-only Auditor role. Resolving the whole surface to ceremony:status
// therefore handed a reader the trust root; only the two genuine reads (status
// and officer listing) carry ceremony:status here.
var ceremonySubOpPermissions = map[byte]qrdbPermission{
	SubOpCeremonyInitialize:            {"ceremony", "initialize"},
	SubOpCeremonyStatus:                {"ceremony", "status"},
	SubOpCeremonyClaimCertBegin:        {"ceremony", "claim_cert"},
	SubOpCeremonyClaimCertComplete:     {"ceremony", "claim_cert"},
	SubOpCeremonyBeginClaimShare:       {"ceremony", "claim_share_begin"},
	SubOpCeremonyCompleteClaimShare:    {"ceremony", "claim_share_complete"},
	SubOpCeremonySignCSR:               {"ceremony", "sign_csr"},
	SubOpCeremonyListOfficers:          {"ceremony", "status"},
	SubOpCeremonyEnrollExternalOfficer: {"ceremony", "enroll_custodian"},
}

// accountSubOpPermissions maps each pooled-provider-account sub-op (carried on
// the OpOrchestrator dispatch byte, see protocol.go) to the qrdb-canonical
// permission it requires. The values MUST match the gRPC mapping
// (pkg/api/grpc/authz_mapping.go) and the REST PEP mapping
// (pkg/api/rest/authz_pep_mapping.go) exactly.
//
// Accounts custody credential material, so they authorize with the qrdb
// credentials:* family — NOT the surface-level orchestrator:status the rest of
// OpOrchestrator uses. Without this table an account write would authorize
// under a materially weaker permission than the same operation on REST/gRPC.
var accountSubOpPermissions = map[byte]qrdbPermission{
	SubOpAccountCreate:     {"credentials", "store"},
	SubOpAccountGet:        {"credentials", "retrieve"},
	SubOpAccountList:       {"credentials", "list"},
	SubOpAccountUpdate:     {"credentials", "store"},
	SubOpAccountDelete:     {"credentials", "delete"},
	SubOpAccountRotate:     {"credentials", "rotate"},
	SubOpAccountRevoke:     {"credentials", "delete"},
	SubOpAccountSetDefault: {"credentials", "store"},
	SubOpAccountUsage:      {"credentials", "retrieve"},
}

// rotateSubOpPermissions maps the session account rotation sub-op (carried on
// the OpOrchestrator dispatch byte at 0x20, see protocol.go) to the
// qrdb-canonical permission it requires. The value MUST match the gRPC mapping
// (pkg/api/grpc/authz_mapping.go RotateSessionAccount), the REST PEP mapping
// (pkg/api/rest/authz_pep_mapping.go "/rotate-account") and the MCP mapping
// (pkg/api/mcp/authz_qrdb.go quicbaas_orchestrator_rotate_account) exactly.
//
// Rotation closes the session's agent process and reconstitutes it against a
// newly selected pooled credential, so it authorizes with the write-strength
// orchestrator:spawn — the very permission the session was created with — and
// NOT the surface-level orchestrator:status the rest of OpOrchestrator uses.
// Without this table an operator holding only read access could move a session
// onto another tenant-owned account's credential.
var rotateSubOpPermissions = map[byte]qrdbPermission{
	SubOpOrchestratorRotateAccount: {"orchestrator", "spawn"},
}

// projectSubOpPermissions maps each project sub-op (carried on the
// OpOrchestrator dispatch byte, see protocol.go) to the qrdb-canonical
// permission it requires. The values MUST match the gRPC mapping
// (pkg/api/grpc/authz_mapping.go) and the REST PEP mapping
// (pkg/api/rest/authz_pep_mapping.go) exactly.
//
// Projects are their own resource with full CRUD plus the two role verbs, so
// they authorize with project:* — NOT the surface-level orchestrator:status
// the rest of OpOrchestrator uses. Without this table a project delete would
// authorize under a materially weaker permission than the same operation on
// REST or gRPC.
var projectSubOpPermissions = map[byte]qrdbPermission{
	SubOpProjectCreate:          {"project", "create"},
	SubOpProjectGet:             {"project", "read"},
	SubOpProjectGetByName:       {"project", "read"},
	SubOpProjectList:            {"project", "list"},
	SubOpProjectUpdate:          {"project", "update"},
	SubOpProjectDelete:          {"project", "delete"},
	SubOpProjectGrantAgentRole:  {"project", "grant"},
	SubOpProjectRevokeAgentRole: {"project", "revoke"},
}

// attestationSubOpPermissions maps each attestation sub-op (carried on the
// OpOrchestrator dispatch byte, see protocol.go) to the qrdb-canonical
// permission it requires. The values MUST match the gRPC mapping
// (pkg/api/grpc/authz_mapping.go) and the MCP mapping
// (pkg/api/mcp/authz_qrdb.go) exactly.
//
// Attestation has its own resource vocabulary: the verbs answer "is this host
// and this workload in the state we approved?", which is neither a
// certificate operation nor an orchestrator status read. Verification is a
// pure computation over caller-supplied material and carries the
// read-strength attestation:verify; AttestContainer drives the registered
// attestor — extending a PCR and asking the host TPM for a signed quote — and
// carries the write-strength attestation:attest.
var attestationSubOpPermissions = map[byte]qrdbPermission{
	SubOpAttestationVerify:          {"attestation", "verify"},
	SubOpAttestationVerifyChain:     {"attestation", "verify"},
	SubOpAttestationAttestContainer: {"attestation", "attest"},
}

// pivSubOpPermissions maps each PIV sub-op (carried on the OpOrchestrator
// dispatch byte, see protocol.go) to the qrdb-canonical permission it
// requires. The values MUST match the REST xkms compatibility mapping
// (pkg/api/rest/authz_pep_mapping.go xkmsOps) and the MCP mapping
// (pkg/api/mcp/authz_qrdb.go xkms.piv.*) exactly: slot/cert reads are
// piv:read, mutations are piv:write / piv:delete, and CSR generation — a
// private-key use — is piv:use.
var pivSubOpPermissions = map[byte]qrdbPermission{
	SubOpPIVListSlots:   {"piv", "read"},
	SubOpPIVGetCert:     {"piv", "read"},
	SubOpPIVExportCert:  {"piv", "read"},
	SubOpPIVStoreCert:   {"piv", "write"},
	SubOpPIVImportCert:  {"piv", "write"},
	SubOpPIVGenerateKey: {"piv", "write"},
	SubOpPIVDeleteCert:  {"piv", "delete"},
	SubOpPIVGenerateCSR: {"piv", "use"},
}

// fido2SubOpPermissions maps the FIDO2 REGISTRATION sub-ops (carried on the
// OpOrchestrator dispatch byte, see protocol.go) to the qrdb-canonical
// permission they require. Registration IS authenticator (factor)
// enrollment, so it authorizes with the same user_authfactors:add the REST
// canonical mount uses (pkg/api/rest/authz_pep_mapping.go webauthn rules).
//
// The authentication ceremony verbs (SubOpFIDO2BeginAuthentication /
// SubOpFIDO2FinishAuthentication) are deliberately ABSENT: they are pre-auth
// and bypass the authz gate via isPublicSubOperation (handler_fido2.go),
// exactly like OpAuthBeginLogin / OpAuthFinishLogin. Were they ever to reach
// this table they would fail closed under default-deny.
var fido2SubOpPermissions = map[byte]qrdbPermission{
	SubOpFIDO2BeginRegistration:  {"user_authfactors", "add"},
	SubOpFIDO2FinishRegistration: {"user_authfactors", "add"},
}

// sshSubOpPermissions maps each SSH sub-op (carried on the OpOrchestrator
// dispatch byte, see protocol.go) to the qrdb-canonical permission it
// requires. The values MUST match the gRPC mapping
// (pkg/api/grpc/authz_mapping.go) and the REST PEP mapping
// (pkg/api/rest/authz_pep_mapping.go sshOps) exactly.
//
// SSH assets are cryptographic keys, so they authorize with the qrdb keys:*
// family — NOT the surface-level orchestrator:status the rest of
// OpOrchestrator uses. Without this table an SSH key delete would authorize
// under a materially weaker permission than the same operation on REST/gRPC.
var sshSubOpPermissions = map[byte]qrdbPermission{
	SubOpSSHGenerateKey:     {"keys", "create"},
	SubOpSSHImportKey:       {"keys", "import"},
	SubOpSSHGetKey:          {"keys", "get"},
	SubOpSSHListKeys:        {"keys", "list"},
	SubOpSSHDeleteKey:       {"keys", "delete"},
	SubOpSSHExportPublicKey: {"keys", "get"},
	SubOpSSHSignDigest:      {"keys", "sign"},

	SubOpSSHAddKnownHost:    {"keys", "create"},
	SubOpSSHListKnownHosts:  {"keys", "list"},
	SubOpSSHRemoveKnownHost: {"keys", "delete"},

	SubOpSSHAddAuthorizedKey:    {"keys", "create"},
	SubOpSSHListAuthorizedKeys:  {"keys", "list"},
	SubOpSSHRemoveAuthorizedKey: {"keys", "delete"},
	SubOpSSHEmitAuthorizedKey:   {"keys", "get"},
}

// certExtSubOpPermissions maps each certificate-extension sub-op (carried on
// the OpCertExt dispatch byte, see protocol.go) to the qrdb-canonical
// permission it requires. The values MUST match the gRPC mapping
// (pkg/api/grpc/authz_mapping.go), the REST PEP mapping
// (pkg/api/rest/authz_pep_mapping.go) and the MCP mapping
// (pkg/api/mcp/authz_qrdb.go) exactly.
//
// CrossSign mints a CA certificate — it re-certifies a foreign ROOT's subject
// and public key with basicConstraints CA:TRUE — so it authorizes with
// ca:create. qrdb states the boundary explicitly: "Holding certs:generate must
// not imply the authority to mint a new signing CA"
// (go-qrdb/pkg/permission/acme_admin_scope.go).
//
// Chain performs the CA's Authority Information Access walk, which fetches
// issuer certificates over the network and imports them into the certificate
// store — the same mutation OpTrustAutoImportAIA performs, hence the same
// write-strength verb.
//
// IssueDeviceIdentity mints TCG EK / AK / IEEE 802.1AR IDevID certificates.
// Those are END-ENTITY certificates, so they stay in the leaf-issuance family.
var certExtSubOpPermissions = map[byte]qrdbPermission{
	SubOpCertExtCrossSign:           {"ca", "create"},
	SubOpCertExtChain:               {"certs", "generate"},
	SubOpCertExtIssueDeviceIdentity: {"certs", "generate"},
}

// getQRDBPermission returns the qrdb-canonical (resource, action) for the
// opcode and whether a mapping exists. An unmapped opcode has ok=false: the
// default-deny authz gate denies it under RequireAuthz. IAM opcodes are
// resolved via the build-tag-conditional getIAMQRDBPermission.
//
// For OpBarrier and OpCeremony the resolution is sub-op-aware: payload[0]
// selects the sub-operation, which is looked up in barrierSubOpPermissions or
// ceremonySubOpPermissions. An empty payload or an unknown sub-op results in
// ok=false (default-deny). OpScheduler and the contiguous orchestrator sub-op
// range authorize at the surface level.
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
	// OpCeremony carries the initialization-ceremony sub-ops. The write verbs
	// mint the trust root, so each resolves its own ceremony:* permission
	// instead of the read-strength ceremony:status the whole surface used to
	// resolve.
	if opcode == OpCeremony {
		if len(payload) < 1 {
			return "", "", false
		}
		if perm, found := ceremonySubOpPermissions[payload[0]]; found {
			return perm.resource, perm.action, true
		}
		return "", "", false
	}
	// OpOrchestrator carries six disjoint sub-op vocabularies. The account
	// range (0x30-0x38) resolves against credentials:*, the project range
	// (0x40-0x47) against project:*, and the SSH range (0x50-0x5D) against
	// keys:*; every other sub-op keeps the surface-level orchestrator
	// mapping below.
	if opcode == OpOrchestrator && len(payload) >= 1 && isAccountSubOp(payload[0]) {
		if perm, found := accountSubOpPermissions[payload[0]]; found {
			return perm.resource, perm.action, true
		}
		return "", "", false
	}
	// Session account rotation rides the same dispatch byte at 0x20. It is a
	// write on a session AND a consumption of a pooled credential, so it
	// resolves orchestrator:spawn instead of the surface-level
	// orchestrator:status the contiguous orchestrator range authorizes with.
	if opcode == OpOrchestrator && len(payload) >= 1 && isRotateSubOp(payload[0]) {
		if perm, found := rotateSubOpPermissions[payload[0]]; found {
			return perm.resource, perm.action, true
		}
		return "", "", false
	}
	if opcode == OpOrchestrator && len(payload) >= 1 && isProjectSubOp(payload[0]) {
		if perm, found := projectSubOpPermissions[payload[0]]; found {
			return perm.resource, perm.action, true
		}
		return "", "", false
	}
	// Certificate extensions ride the same dispatch byte in the 0x60-0x62
	// range. Cross-signing is strictly more privileged than leaf issuance, so
	// each verb resolves its own permission instead of the surface-level
	// orchestrator mapping.
	if opcode == OpOrchestrator && len(payload) >= 1 && isCertExtSubOp(payload[0]) {
		if perm, found := certExtSubOpPermissions[payload[0]]; found {
			return perm.resource, perm.action, true
		}
		return "", "", false
	}
	if opcode == OpOrchestrator && len(payload) >= 1 && isSSHSubOp(payload[0]) {
		if perm, found := sshSubOpPermissions[payload[0]]; found {
			return perm.resource, perm.action, true
		}
		return "", "", false
	}
	// Attestation rides the same dispatch byte in the 0x70-0x72 range.
	// Driving a host TPM is materially stronger than reading orchestrator
	// status, so each verb resolves its own attestation:* permission.
	if opcode == OpOrchestrator && len(payload) >= 1 && isAttestationSubOp(payload[0]) {
		if perm, found := attestationSubOpPermissions[payload[0]]; found {
			return perm.resource, perm.action, true
		}
		return "", "", false
	}
	// PIV rides the same dispatch byte in the 0x80-0x87 range. PIV slots
	// hold hardware-backed credentials, so each verb resolves its own
	// piv:* permission rather than the surface-level orchestrator mapping.
	if opcode == OpOrchestrator && len(payload) >= 1 && isPIVSubOp(payload[0]) {
		if perm, found := pivSubOpPermissions[payload[0]]; found {
			return perm.resource, perm.action, true
		}
		return "", "", false
	}
	// FIDO2 rides the same dispatch byte in the 0x90-0x93 range. Only the
	// registration verbs resolve a permission (user_authfactors:add); the
	// authentication ceremony is pre-auth and never reaches this resolver
	// (isPublicSubOperation admits it before the authz gate). An
	// authentication sub-op that somehow lands here fails closed.
	if opcode == OpOrchestrator && len(payload) >= 1 && isFIDO2SubOp(payload[0]) {
		if perm, found := fido2SubOpPermissions[payload[0]]; found {
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
