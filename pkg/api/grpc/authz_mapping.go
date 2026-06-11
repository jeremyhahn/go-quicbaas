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

package grpc

import (
	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"google.golang.org/grpc"
)

// brokerServiceDescs is the canonical list of gRPC service descriptors that the
// QuicBaaS broker registers. The exhaustive guard test
// (TestGRPCPermissions_EveryMethodMapped) iterates this slice and verifies that
// every method in every descriptor is either mapped in grpcMethodPermissions or
// declared public in publicGRPCMethods. Any new service registered by the
// broker MUST be added here.
//
// Services whose methods are intentionally unmapped (embedding, RAG, agent
// runtime) are NOT listed here; they are denied by default-deny and their
// absence from this slice documents that posture explicitly.
var brokerServiceDescs = []grpc.ServiceDesc{
	pb.QuicBaaS_ServiceDesc,
	pb.Barrier_ServiceDesc,
	pb.TenantBarrier_ServiceDesc,
	pb.Ceremony_ServiceDesc,
	pb.Login_ServiceDesc,
	pb.KeyManagement_ServiceDesc,
	pb.SecretsManagement_ServiceDesc,
	pb.Crypto_ServiceDesc,
	pb.DEKManagement_ServiceDesc,
	pb.CertificateService_ServiceDesc,
	pb.TrustService_ServiceDesc,
	pb.VectorDB_ServiceDesc,
	pb.VectorAdmin_ServiceDesc,
	pb.ObjectStorageService_ServiceDesc,
	pb.ObjectStorageAdmin_ServiceDesc,
	pb.ObjectTransferService_ServiceDesc,
	pb.OrchestratorService_ServiceDesc,
	pb.MessagingService_ServiceDesc,
	pb.SchedulerService_ServiceDesc,
	pb.ParameterStore_ServiceDesc,
	pb.DynamicSecretsService_ServiceDesc,
	pb.ProjectService_ServiceDesc,
	pb.OllamaService_ServiceDesc,
}

// This file maps gRPC FullMethod names onto qrdb-canonical (resource, action)
// permission tuples. The qrdb policy decision point joins the tuple into the
// registered "resource:action" permission (e.g. "kv:put"); the vocabulary here
// MUST match go-qrdb/pkg/permission/constants.go exactly. It mirrors the REST
// PEP mapping (pkg/api/rest/authz_pep_mapping.go), which established the broker
// resource -> qrdb resource translation (kv, keys, certs, credentials,
// tenant_barrier, users, roles, tenants, ...).
//
// The map is exact-match by FullMethod. A method that has no entry is reported
// unmapped: under RequireAuthz the gRPC authz interceptor is default-deny, so an
// unmapped protected method is denied rather than admitted (there is no
// registered qrdb permission to admit it). This is the same posture the REST PEP
// takes for an unmapped path.
//
// Stage 0.5 maps the broker-only data-plane surfaces that qrdb now registers a
// permission family for (go-qrdb/pkg/permission/broker_dataplane_scope.go):
// object storage (objstore), orchestrator, messaging, scheduler, parameter
// store (parameters), dynamic_secrets, project, ollama. Before Stage 0.5 these
// were unmapped and blanket-denied under RequireAuthz default-deny.
//
// Surfaces that the qrdb permission registry still does NOT define a resource
// family for — embedding, agent runtime, RAG — remain intentionally absent and
// denied under RequireAuthz default-deny. Granting them requires first
// registering a qrdb permission family for the surface; inventing a name here
// would not resolve against the PDP.

// publicGRPCMethods are reachable without authentication. Health and the login
// ceremony (which authenticates the caller via the mTLS handshake / signature,
// before any bearer token exists) bypass the authz gate entirely.
var publicGRPCMethods = map[string]bool{
	"/quicbaas.v1.QuicBaaS/Health":         true,
	"/quicbaas.v1.QuicBaaS/Login":          true, // legacy unauthenticated login entry point
	"/quicbaas.login.v1.Login/BeginLogin":  true,
	"/quicbaas.login.v1.Login/FinishLogin": true,
}

// methodPermission is a qrdb-canonical (resource, action) tuple.
type methodPermission struct {
	resource string
	action   string
}

// grpcMethodPermissions maps each protected gRPC FullMethod to the qrdb-canonical
// permission it requires. Every value is a registered qrdb permission (the join
// resource:action exists in go-qrdb/pkg/permission/constants.go).
var grpcMethodPermissions = map[string]methodPermission{
	// ---- quicbaas.v1.QuicBaaS: KV ----
	"/quicbaas.v1.QuicBaaS/Get":            {"kv", "get"},
	"/quicbaas.v1.QuicBaaS/List":           {"kv", "list"},
	"/quicbaas.v1.QuicBaaS/Query":          {"kv", "range"},
	"/quicbaas.v1.QuicBaaS/ScanIndex":      {"index", "scan"},
	"/quicbaas.v1.QuicBaaS/Put":            {"kv", "put"},
	"/quicbaas.v1.QuicBaaS/Delete":         {"kv", "delete"},
	"/quicbaas.v1.QuicBaaS/BatchGet":       {"batch", "execute"},
	"/quicbaas.v1.QuicBaaS/BatchPut":       {"batch", "execute"},
	"/quicbaas.v1.QuicBaaS/BatchDelete":    {"batch", "execute"},
	"/quicbaas.v1.QuicBaaS/StreamBatchPut": {"batch", "execute"},
	"/quicbaas.v1.QuicBaaS/Transaction":    {"transaction", "execute"},
	"/quicbaas.v1.QuicBaaS/Watch":          {"watch", "watch"},

	// ---- quicbaas.v1.QuicBaaS: cluster admin ----
	"/quicbaas.v1.QuicBaaS/ClusterStatus":        {"admin", "status"},
	"/quicbaas.v1.QuicBaaS/GetLeader":            {"admin", "status"},
	"/quicbaas.v1.QuicBaaS/ListEntityTypes":      {"admin", "status"},
	"/quicbaas.v1.QuicBaaS/AddNode":              {"admin", "add_member"},
	"/quicbaas.v1.QuicBaaS/RemoveNode":           {"admin", "remove_member"},
	"/quicbaas.v1.QuicBaaS/CreateShard":          {"admin", "start_shard"},
	"/quicbaas.v1.QuicBaaS/RebalanceCluster":     {"rebalance", "execute"},
	"/quicbaas.v1.QuicBaaS/GetRebalanceProgress": {"rebalance", "report"},
	"/quicbaas.v1.QuicBaaS/MigrateData":          {"admin", "drain"},
	"/quicbaas.v1.QuicBaaS/GetMigrationProgress": {"admin", "status"},
	"/quicbaas.v1.QuicBaaS/ReplaceNode":          {"admin", "add_member"},
	"/quicbaas.v1.QuicBaaS/UpgradeNode":          {"admin", "drain"},
	"/quicbaas.v1.QuicBaaS/GetUpgradeStatus":     {"admin", "status"},

	// ---- quicbaas.v1.QuicBaaS: cache (admin capabilities) ----
	"/quicbaas.v1.QuicBaaS/GetCacheStats":     {"admin", "status"},
	"/quicbaas.v1.QuicBaaS/GetCacheConfig":    {"admin", "status"},
	"/quicbaas.v1.QuicBaaS/ClearCache":        {"admin", "capabilities"},
	"/quicbaas.v1.QuicBaaS/UpdateCacheConfig": {"admin", "capabilities"},

	// ---- quicbaas.v1.QuicBaaS: backup ----
	"/quicbaas.v1.QuicBaaS/CreateBackup":  {"backup", "create"},
	"/quicbaas.v1.QuicBaaS/ListBackups":   {"backup", "list"},
	"/quicbaas.v1.QuicBaaS/DeleteBackup":  {"backup", "delete"},
	"/quicbaas.v1.QuicBaaS/RestoreBackup": {"backup", "restore"},
	"/quicbaas.v1.QuicBaaS/VerifyBackup":  {"backup", "verify"},

	// ---- quicbaas.v1.QuicBaaS: user / role management ----
	"/quicbaas.v1.QuicBaaS/CreateUser": {"users", "create"},
	"/quicbaas.v1.QuicBaaS/DeleteUser": {"users", "delete"},
	"/quicbaas.v1.QuicBaaS/ListUsers":  {"users", "list"},
	"/quicbaas.v1.QuicBaaS/CreateRole": {"roles", "create"},
	"/quicbaas.v1.QuicBaaS/DeleteRole": {"roles", "delete"},
	"/quicbaas.v1.QuicBaaS/ListRoles":  {"roles", "list"},
	"/quicbaas.v1.QuicBaaS/AssignRole": {"roles", "assign"},
	"/quicbaas.v1.QuicBaaS/RevokeRole": {"roles", "unassign"},

	// ---- quicbaas.v1.QuicBaaS: auth (token lifecycle) ----
	"/quicbaas.v1.QuicBaaS/Logout":        {"user_authfactors", "login"},
	"/quicbaas.v1.QuicBaaS/RefreshToken":  {"user_authfactors", "login"},
	"/quicbaas.v1.QuicBaaS/ValidateToken": {"user_authfactors", "login"},

	// ---- quicbaas.v1.QuicBaaS: seal ----
	"/quicbaas.v1.QuicBaaS/Seal":       {"seal", "seal"},
	"/quicbaas.v1.QuicBaaS/Unseal":     {"seal", "unseal"},
	"/quicbaas.v1.QuicBaaS/SealInit":   {"seal", "bootstrap"},
	"/quicbaas.v1.QuicBaaS/SealRekey":  {"seal", "rekey"},
	"/quicbaas.v1.QuicBaaS/SealStatus": {"seal", "status"},

	// ---- quicbaas.v1.QuicBaaS: time-series ----
	"/quicbaas.v1.QuicBaaS/Aggregate":            {"timeseries", "aggregate"},
	"/quicbaas.v1.QuicBaaS/TimeSeriesQuery":      {"timeseries", "query"},
	"/quicbaas.v1.QuicBaaS/TSQuery":              {"timeseries", "query"},
	"/quicbaas.v1.QuicBaaS/TSGetRetentionPolicy": {"timeseries", "get_retention"},
	"/quicbaas.v1.QuicBaaS/TSListMetrics":        {"timeseries", "list_metrics"},
	"/quicbaas.v1.QuicBaaS/TSWritePoint":         {"timeseries", "write_point"},
	"/quicbaas.v1.QuicBaaS/TSWriteBatch":         {"timeseries", "write_batch"},
	"/quicbaas.v1.QuicBaaS/TSSetRetentionPolicy": {"timeseries", "set_retention"},
	"/quicbaas.v1.QuicBaaS/TSDeleteSeries":       {"timeseries", "delete_series"},

	// ---- quicbaas.kms.v1.KeyManagement (cryptographic keys) ----
	"/quicbaas.kms.v1.KeyManagement/GenerateKey":            {"keys", "create"},
	"/quicbaas.kms.v1.KeyManagement/ImportKey":              {"keys", "import"},
	"/quicbaas.kms.v1.KeyManagement/ExportKey":              {"keys", "export"},
	"/quicbaas.kms.v1.KeyManagement/UnwrapKey":              {"keys", "unwrap"},
	"/quicbaas.kms.v1.KeyManagement/GetKey":                 {"keys", "get"},
	"/quicbaas.kms.v1.KeyManagement/GetKeyMetadata":         {"keys", "get"},
	"/quicbaas.kms.v1.KeyManagement/GetKeyPolicy":           {"keys", "get"},
	"/quicbaas.kms.v1.KeyManagement/GetSupportedAlgorithms": {"keys", "list"},
	"/quicbaas.kms.v1.KeyManagement/ListKeys":               {"keys", "list"},
	"/quicbaas.kms.v1.KeyManagement/ListKeyVersions":        {"keys", "list"},
	"/quicbaas.kms.v1.KeyManagement/RotateKey":              {"keys", "rotate"},
	"/quicbaas.kms.v1.KeyManagement/DeleteKey":              {"keys", "delete"},
	"/quicbaas.kms.v1.KeyManagement/DeleteKeyPolicy":        {"keys", "delete"},
	"/quicbaas.kms.v1.KeyManagement/RevokeKey":              {"keys", "delete"},
	"/quicbaas.kms.v1.KeyManagement/SetKeyPolicy":           {"keys", "rotate"},
	"/quicbaas.kms.v1.KeyManagement/SetKeySafety":           {"keys", "rotate"},
	"/quicbaas.kms.v1.KeyManagement/ClearKeySafety":         {"keys", "rotate"},
	"/quicbaas.kms.v1.KeyManagement/UpdateKeyMetadata":      {"keys", "rotate"},
	"/quicbaas.kms.v1.KeyManagement/UpdateKeyStatus":        {"keys", "rotate"},
	"/quicbaas.kms.v1.KeyManagement/SafetyCheck":            {"keys", "get"},
	"/quicbaas.kms.v1.KeyManagement/SafetyReport":           {"keys", "get"},
	"/quicbaas.kms.v1.KeyManagement/BackendStatus":          {"backends", "read"},
	"/quicbaas.kms.v1.KeyManagement/BackendTest":            {"backends", "read"},

	// ---- quicbaas.kms.v1.Crypto ----
	"/quicbaas.kms.v1.Crypto/Encrypt":                {"keys", "encrypt"},
	"/quicbaas.kms.v1.Crypto/EncryptBatch":           {"keys", "encrypt"},
	"/quicbaas.kms.v1.Crypto/Decrypt":                {"keys", "decrypt"},
	"/quicbaas.kms.v1.Crypto/DecryptBatch":           {"keys", "decrypt"},
	"/quicbaas.kms.v1.Crypto/Sign":                   {"keys", "sign"},
	"/quicbaas.kms.v1.Crypto/Verify":                 {"keys", "verify"},
	"/quicbaas.kms.v1.Crypto/Rewrap":                 {"keys", "unwrap"},
	"/quicbaas.kms.v1.Crypto/GenerateDataKey":        {"keys", "derive"},
	"/quicbaas.kms.v1.Crypto/GenerateWrappedDataKey": {"keys", "wrap"},
	"/quicbaas.kms.v1.Crypto/GenerateHMAC":           {"keys", "sign"},
	"/quicbaas.kms.v1.Crypto/VerifyHMAC":             {"keys", "verify"},
	"/quicbaas.kms.v1.Crypto/Hash":                   {"keys", "sign"},
	"/quicbaas.kms.v1.Crypto/GenerateRandom":         {"keys", "derive"},

	// ---- quicbaas.kms.v1.DEKManagement ----
	"/quicbaas.kms.v1.DEKManagement/CreateDEK":               {"keys", "create"},
	"/quicbaas.kms.v1.DEKManagement/GetDEK":                  {"keys", "get"},
	"/quicbaas.kms.v1.DEKManagement/ListDEKs":                {"keys", "list"},
	"/quicbaas.kms.v1.DEKManagement/RotateDEK":               {"keys", "rotate"},
	"/quicbaas.kms.v1.DEKManagement/DeleteDEK":               {"keys", "delete"},
	"/quicbaas.kms.v1.DEKManagement/SetDefaultDEK":           {"keys", "rotate"},
	"/quicbaas.kms.v1.DEKManagement/GetDEKStatus":            {"keys", "get"},
	"/quicbaas.kms.v1.DEKManagement/SetDEKSafety":            {"keys", "rotate"},
	"/quicbaas.kms.v1.DEKManagement/ClearDEKSafety":          {"keys", "rotate"},
	"/quicbaas.kms.v1.DEKManagement/StartReencryption":       {"keys", "rotate"},
	"/quicbaas.kms.v1.DEKManagement/GetReencryptionProgress": {"keys", "get"},

	// ---- quicbaas.kms.v1.SecretsManagement (named credentials) ----
	"/quicbaas.kms.v1.SecretsManagement/StoreSecret":          {"credentials", "store"},
	"/quicbaas.kms.v1.SecretsManagement/GetSecret":            {"credentials", "retrieve"},
	"/quicbaas.kms.v1.SecretsManagement/ListSecrets":          {"credentials", "list"},
	"/quicbaas.kms.v1.SecretsManagement/DeleteSecret":         {"credentials", "delete"},
	"/quicbaas.kms.v1.SecretsManagement/RotateSecret":         {"credentials", "rotate"},
	"/quicbaas.kms.v1.SecretsManagement/ListSecretVersions":   {"credentials", "list_versions"},
	"/quicbaas.kms.v1.SecretsManagement/GetSecretVersion":     {"credentials", "retrieve_version"},
	"/quicbaas.kms.v1.SecretsManagement/RollbackSecret":       {"credentials", "submit"},
	"/quicbaas.kms.v1.SecretsManagement/SetRotationPolicy":    {"credentials", "strategy"},
	"/quicbaas.kms.v1.SecretsManagement/GetRotationPolicy":    {"credentials", "list"},
	"/quicbaas.kms.v1.SecretsManagement/GetSecretsByPrefix":   {"credentials", "list"},
	"/quicbaas.kms.v1.SecretsManagement/UpdateSecretStatus":   {"credentials", "submit"},
	"/quicbaas.kms.v1.SecretsManagement/WatchSecretsByPrefix": {"credentials", "list"},

	// ---- quicbaas.certificate.v1.CertificateService ----
	"/quicbaas.certificate.v1.CertificateService/IssueCertificate":  {"certs", "generate"},
	"/quicbaas.certificate.v1.CertificateService/SignCSR":           {"certs", "generate"},
	"/quicbaas.certificate.v1.CertificateService/RevokeCertificate": {"certs", "revoke"},
	"/quicbaas.certificate.v1.CertificateService/VerifyCertificate": {"certs", "read"},
	"/quicbaas.certificate.v1.CertificateService/GetCACertificate":  {"certs", "read"},
	"/quicbaas.certificate.v1.CertificateService/GetTLSConfig":      {"certs", "read"},

	// ---- quicbaas.certificate.v1.TrustService ----
	"/quicbaas.certificate.v1.TrustService/CrossSignedRootPool": {"certs", "read"},
	"/quicbaas.certificate.v1.TrustService/AutoImportAIA":       {"certs", "generate"},
	"/quicbaas.certificate.v1.TrustService/PublishDANE":         {"certs", "generate"},

	// ---- quicbaas.barrier.v1.Barrier (system barrier lifecycle) ----
	"/quicbaas.barrier.v1.Barrier/BarrierStatus":                {"tenant_barrier", "status"},
	"/quicbaas.barrier.v1.Barrier/BarrierInitialize":            {"tenant_barrier", "init"},
	"/quicbaas.barrier.v1.Barrier/BarrierUnseal":                {"tenant_barrier", "unseal"},
	"/quicbaas.barrier.v1.Barrier/BarrierSeal":                  {"tenant_barrier", "seal"},
	"/quicbaas.barrier.v1.Barrier/BarrierRekey":                 {"seal", "rekey"},
	"/quicbaas.barrier.v1.Barrier/BarrierRootToken":             {"seal", "bootstrap"},
	"/quicbaas.barrier.v1.Barrier/BarrierRecoveryStatus":        {"seal", "status"},
	"/quicbaas.barrier.v1.Barrier/BarrierRecoveryGenerate":      {"seal", "generate_recovery"},
	"/quicbaas.barrier.v1.Barrier/BarrierRecoveryRecover":       {"seal", "recover_with_keys"},
	"/quicbaas.barrier.v1.Barrier/BarrierRecoveryDelete":        {"seal", "generate_recovery"},
	"/quicbaas.barrier.v1.Barrier/BarrierShamirInitialize":      {"seal", "initialize_shamir"},
	"/quicbaas.barrier.v1.Barrier/BarrierShamirUnsealShare":     {"barrier", "unseal-share"},
	"/quicbaas.barrier.v1.Barrier/BarrierShamirUnsealShares":    {"barrier", "unseal-share"},
	"/quicbaas.barrier.v1.Barrier/BarrierShamirListShares":      {"seal", "status"},
	"/quicbaas.barrier.v1.Barrier/BarrierShamirVerify":          {"seal", "status"},
	"/quicbaas.barrier.v1.Barrier/BarrierShamirDeleteShare":     {"seal", "rekey"},
	"/quicbaas.barrier.v1.Barrier/BarrierShamirDeleteAllShares": {"seal", "rekey"},

	// ---- quicbaas.tenant.v1.TenantBarrier (per-tenant barrier lifecycle) ----
	"/quicbaas.tenant.v1.TenantBarrier/Init":        {"tenant_barrier", "init"},
	"/quicbaas.tenant.v1.TenantBarrier/Unseal":      {"tenant_barrier", "unseal"},
	"/quicbaas.tenant.v1.TenantBarrier/UnsealShare": {"tenant_barrier", "unseal"},
	"/quicbaas.tenant.v1.TenantBarrier/Seal":        {"tenant_barrier", "seal"},
	"/quicbaas.tenant.v1.TenantBarrier/Status":      {"tenant_barrier", "status"},

	// ---- quicbaas.ceremony.v1.Ceremony ----
	"/quicbaas.ceremony.v1.Ceremony/CeremonyInitialize":            {"ceremony", "initialize"},
	"/quicbaas.ceremony.v1.Ceremony/CeremonyStatus":                {"ceremony", "status"},
	"/quicbaas.ceremony.v1.Ceremony/CeremonyEnrollExternalOfficer": {"ceremony", "enroll_custodian"},
	"/quicbaas.ceremony.v1.Ceremony/CeremonyListOfficers":          {"ceremony", "status"},
	"/quicbaas.ceremony.v1.Ceremony/CeremonyClaimCertBegin":        {"ceremony", "claim_cert"},
	"/quicbaas.ceremony.v1.Ceremony/CeremonyClaimCertComplete":     {"ceremony", "claim_cert"},
	"/quicbaas.ceremony.v1.Ceremony/CeremonyClaimShare":            {"ceremony", "claim_share_begin"},
	"/quicbaas.ceremony.v1.Ceremony/CeremonySignCSR":               {"ceremony", "sign_csr"},

	// ---- quicbaas.vector.v1.VectorDB ----
	"/quicbaas.vector.v1.VectorDB/CreateNamespace":       {"vector", "namespace_create"},
	"/quicbaas.vector.v1.VectorDB/DeleteNamespace":       {"vector", "namespace_delete"},
	"/quicbaas.vector.v1.VectorDB/ListNamespaces":        {"vector", "namespace_list"},
	"/quicbaas.vector.v1.VectorDB/GetNamespaceInfo":      {"vector", "namespace_info"},
	"/quicbaas.vector.v1.VectorDB/Upsert":                {"vector", "upsert"},
	"/quicbaas.vector.v1.VectorDB/Get":                   {"vector", "get"},
	"/quicbaas.vector.v1.VectorDB/Delete":                {"vector", "delete"},
	"/quicbaas.vector.v1.VectorDB/BatchUpsert":           {"vector", "batch_upsert"},
	"/quicbaas.vector.v1.VectorDB/BatchDelete":           {"vector", "batch_delete"},
	"/quicbaas.vector.v1.VectorDB/List":                  {"vector", "list"},
	"/quicbaas.vector.v1.VectorDB/UpdateMetadata":        {"vector", "update_metadata"},
	"/quicbaas.vector.v1.VectorDB/SearchSimilar":         {"vector", "search_similar"},
	"/quicbaas.vector.v1.VectorDB/SearchByMetadata":      {"vector", "search_by_metadata"},
	"/quicbaas.vector.v1.VectorDB/HybridSearch":          {"vector", "hybrid_search"},
	"/quicbaas.vector.v1.VectorDB/SearchById":            {"vector", "search_by_id"},
	"/quicbaas.vector.v1.VectorDB/UpsertVector":          {"vector", "upsert"},
	"/quicbaas.vector.v1.VectorDB/GetVector":             {"vector", "get"},
	"/quicbaas.vector.v1.VectorDB/DeleteVector":          {"vector", "delete"},
	"/quicbaas.vector.v1.VectorDB/BatchDeleteVectors":    {"vector", "batch_delete"},
	"/quicbaas.vector.v1.VectorDB/BatchUpsertVectors":    {"vector", "batch_upsert"},
	"/quicbaas.vector.v1.VectorDB/SearchByID":            {"vector", "search_by_id"},
	"/quicbaas.vector.v1.VectorDB/ListVectors":           {"vector", "list"},
	"/quicbaas.vector.v1.VectorDB/UpdateNamespace":       {"vector", "namespace_create"},
	"/quicbaas.vector.v1.VectorDB/GetVectorStats":        {"vector", "get_index_stats"},
	"/quicbaas.vector.v1.VectorDB/GetIndexProgress":      {"vector", "get_rebuild_progress"},
	"/quicbaas.vector.v1.VectorDB/Health":                {"vector", "namespace_list"},
	"/quicbaas.vector.v1.VectorDB/RebuildIndex":          {"vector", "rebuild_index"},
	"/quicbaas.vector.v1.VectorDB/GetRebuildProgress":    {"vector", "get_rebuild_progress"},
	"/quicbaas.vector.v1.VectorDB/GetIndexStats":         {"vector", "get_index_stats"},
	"/quicbaas.vector.v1.VectorDB/Compact":               {"vector", "compact"},
	"/quicbaas.vector.v1.VectorDB/GetCompactionProgress": {"vector", "get_compaction_progress"},

	// ---- quicbaas.vector.v1.VectorAdmin (index lifecycle + cluster management) ----
	"/quicbaas.vector.v1.VectorAdmin/RebuildIndex":          {"vector", "rebuild_index"},
	"/quicbaas.vector.v1.VectorAdmin/GetRebuildProgress":    {"vector", "get_rebuild_progress"},
	"/quicbaas.vector.v1.VectorAdmin/GetIndexStats":         {"vector", "get_index_stats"},
	"/quicbaas.vector.v1.VectorAdmin/Compact":               {"vector", "compact"},
	"/quicbaas.vector.v1.VectorAdmin/CompactNamespace":      {"vector", "compact"},
	"/quicbaas.vector.v1.VectorAdmin/GetCompactionProgress": {"vector", "get_compaction_progress"},
	"/quicbaas.vector.v1.VectorAdmin/GetClusterStats":       {"vector", "get_index_stats"},
	"/quicbaas.vector.v1.VectorAdmin/CreateScalarIndex":     {"vector", "rebuild_index"},
	"/quicbaas.vector.v1.VectorAdmin/DropScalarIndex":       {"vector", "rebuild_index"},
	"/quicbaas.vector.v1.VectorAdmin/ListScalarIndexes":     {"vector", "get_index_stats"},
	"/quicbaas.vector.v1.VectorAdmin/ExportVectors":         {"vector", "list"},
	"/quicbaas.vector.v1.VectorAdmin/ImportVectors":         {"vector", "batch_upsert"},

	// ---- Stage 0.5: broker data-plane surfaces (qrdb-registered) ----

	// ---- quicbaas.objectstorage.v1.ObjectStorageService ----
	"/quicbaas.objectstorage.v1.ObjectStorageService/PutObject":                    {"objstore", "put"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/StreamPutObject":              {"objstore", "put"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/GetObject":                    {"objstore", "get"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/StreamGetObject":              {"objstore", "get"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/DeleteObject":                 {"objstore", "delete"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/HeadObject":                   {"objstore", "head"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/ListObjects":                  {"objstore", "list"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/ListByTag":                    {"objstore", "list"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/ListByTenant":                 {"objstore", "list"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/CopyObject":                   {"objstore", "copy"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/ArchiveObject":                {"objstore", "archive"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/RestoreObject":                {"objstore", "restore"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/VerifyObject":                 {"objstore", "verify"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/ReplicateObject":              {"objstore", "replicate"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/MigrateObject":                {"objstore", "migrate"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/BatchPutObjects":              {"objstore", "batch"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/BatchGetObjects":              {"objstore", "batch"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/BatchDeleteObjects":           {"objstore", "batch"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/GetObjectPolicy":              {"objstore", "read_policy"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/SetObjectPolicy":              {"objstore", "write_policy"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/ListBackends":                 {"objstore", "read_backend"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/GetBackendHealth":             {"objstore", "read_backend"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/GetObjectStorageTenantUsage":  {"objstore", "tenant_usage"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/GetObjectStorageTenantQuotas": {"objstore", "tenant_quota_read"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/SetObjectStorageTenantQuotas": {"objstore", "tenant_quota_write"},

	// ---- quicbaas.objectstorage.v1.ObjectStorageAdmin ----
	"/quicbaas.objectstorage.v1.ObjectStorageAdmin/CreateBackend":                {"objstore", "admin"},
	"/quicbaas.objectstorage.v1.ObjectStorageAdmin/UpdateBackend":                {"objstore", "admin"},
	"/quicbaas.objectstorage.v1.ObjectStorageAdmin/DeleteBackend":                {"objstore", "admin"},
	"/quicbaas.objectstorage.v1.ObjectStorageAdmin/SetBackendStatus":             {"objstore", "admin"},
	"/quicbaas.objectstorage.v1.ObjectStorageAdmin/GetStorageStats":              {"objstore", "read_stats"},
	"/quicbaas.objectstorage.v1.ObjectStorageAdmin/RunGarbageCollection":         {"objstore", "admin"},
	"/quicbaas.objectstorage.v1.ObjectStorageAdmin/GetGarbageCollectionProgress": {"objstore", "admin"},
	"/quicbaas.objectstorage.v1.ObjectStorageAdmin/RepairObject":                 {"objstore", "admin"},
	"/quicbaas.objectstorage.v1.ObjectStorageAdmin/CompactBackend":               {"objstore", "admin"},

	// ---- quicbaas.v1.ObjectTransferService ----
	"/quicbaas.v1.ObjectTransferService/FetchObject":        {"objstore", "get"},
	"/quicbaas.v1.ObjectTransferService/StreamFetchObjects": {"objstore", "get"},

	// ---- quicbaas.orchestrator.v1.OrchestratorService ----
	"/quicbaas.orchestrator.v1.OrchestratorService/SpawnSession":           {"orchestrator", "spawn"},
	"/quicbaas.orchestrator.v1.OrchestratorService/SpawnChildJob":          {"orchestrator", "job_start"},
	"/quicbaas.orchestrator.v1.OrchestratorService/PromptSession":          {"orchestrator", "prompt"},
	"/quicbaas.orchestrator.v1.OrchestratorService/SessionStatus":          {"orchestrator", "status"},
	"/quicbaas.orchestrator.v1.OrchestratorService/CloseSession":           {"orchestrator", "close"},
	"/quicbaas.orchestrator.v1.OrchestratorService/ListSessions":           {"orchestrator", "list"},
	"/quicbaas.orchestrator.v1.OrchestratorService/SubscribeSessionEvents": {"orchestrator", "status"},
	"/quicbaas.orchestrator.v1.OrchestratorService/CreateTask":             {"orchestrator", "task_create"},
	"/quicbaas.orchestrator.v1.OrchestratorService/GetTask":                {"orchestrator", "task_read"},
	"/quicbaas.orchestrator.v1.OrchestratorService/ListTasks":              {"orchestrator", "task_list"},
	"/quicbaas.orchestrator.v1.OrchestratorService/ClaimTask":              {"orchestrator", "task_claim"},
	"/quicbaas.orchestrator.v1.OrchestratorService/CompleteTask":           {"orchestrator", "task_complete"},
	"/quicbaas.orchestrator.v1.OrchestratorService/DecomposeTask":          {"orchestrator", "task_decompose"},
	"/quicbaas.orchestrator.v1.OrchestratorService/SubmitTaskArtifacts":    {"orchestrator", "artifact_submit"},
	"/quicbaas.orchestrator.v1.OrchestratorService/GetTaskArtifact":        {"orchestrator", "artifact_read"},
	"/quicbaas.orchestrator.v1.OrchestratorService/ListTaskArtifacts":      {"orchestrator", "artifact_read"},
	"/quicbaas.orchestrator.v1.OrchestratorService/StartJob":               {"orchestrator", "job_start"},
	"/quicbaas.orchestrator.v1.OrchestratorService/GetJobStatus":           {"orchestrator", "job_read"},
	"/quicbaas.orchestrator.v1.OrchestratorService/ListJobs":               {"orchestrator", "job_list"},
	"/quicbaas.orchestrator.v1.OrchestratorService/CancelJob":              {"orchestrator", "job_cancel"},
	"/quicbaas.orchestrator.v1.OrchestratorService/SubscribeJobEvents":     {"orchestrator", "job_read"},
	"/quicbaas.orchestrator.v1.OrchestratorService/ListBackends":           {"orchestrator", "backend_list"},
	"/quicbaas.orchestrator.v1.OrchestratorService/Health":                 {"orchestrator", "health"},
	"/quicbaas.orchestrator.v1.OrchestratorService/Inference":              {"orchestrator", "inference"},
	"/quicbaas.orchestrator.v1.OrchestratorService/InferenceStream":        {"orchestrator", "inference"},
	"/quicbaas.orchestrator.v1.OrchestratorService/RegisterAgent":          {"orchestrator", "agent_register"},
	"/quicbaas.orchestrator.v1.OrchestratorService/GetAgent":               {"orchestrator", "agent_read"},
	"/quicbaas.orchestrator.v1.OrchestratorService/ListAgents":             {"orchestrator", "agent_list"},
	"/quicbaas.orchestrator.v1.OrchestratorService/RemoveAgent":            {"orchestrator", "agent_remove"},
	"/quicbaas.orchestrator.v1.OrchestratorService/AgentHeartbeat":         {"orchestrator", "agent_heartbeat"},
	"/quicbaas.orchestrator.v1.OrchestratorService/PushEvents":             {"orchestrator", "hook"},

	// ---- quicbaas.messaging.v1.MessagingService ----
	"/quicbaas.messaging.v1.MessagingService/CreateChannel":       {"messaging", "channel_create"},
	"/quicbaas.messaging.v1.MessagingService/GetChannel":          {"messaging", "channel_read"},
	"/quicbaas.messaging.v1.MessagingService/ListChannels":        {"messaging", "channel_list"},
	"/quicbaas.messaging.v1.MessagingService/DeleteChannel":       {"messaging", "channel_delete"},
	"/quicbaas.messaging.v1.MessagingService/Publish":             {"messaging", "publish"},
	"/quicbaas.messaging.v1.MessagingService/PublishDelta":        {"messaging", "publish"},
	"/quicbaas.messaging.v1.MessagingService/Subscribe":           {"messaging", "subscribe"},
	"/quicbaas.messaging.v1.MessagingService/Unsubscribe":         {"messaging", "unsubscribe"},
	"/quicbaas.messaging.v1.MessagingService/WatchChannel":        {"messaging", "watch"},
	"/quicbaas.messaging.v1.MessagingService/StreamNotifications": {"messaging", "watch"},
	"/quicbaas.messaging.v1.MessagingService/GetStats":            {"messaging", "stats"},
	"/quicbaas.messaging.v1.MessagingService/GetChannelStats":     {"messaging", "stats"},
	"/quicbaas.messaging.v1.MessagingService/GetDedupStats":       {"messaging", "stats"},

	// ---- quicbaas.scheduler.v1.SchedulerService ----
	"/quicbaas.scheduler.v1.SchedulerService/CreateJob":          {"scheduler", "create"},
	"/quicbaas.scheduler.v1.SchedulerService/GetJob":             {"scheduler", "read"},
	"/quicbaas.scheduler.v1.SchedulerService/ListJobs":           {"scheduler", "list"},
	"/quicbaas.scheduler.v1.SchedulerService/UpdateJob":          {"scheduler", "update"},
	"/quicbaas.scheduler.v1.SchedulerService/DeleteJob":          {"scheduler", "delete"},
	"/quicbaas.scheduler.v1.SchedulerService/TriggerJob":         {"scheduler", "trigger"},
	"/quicbaas.scheduler.v1.SchedulerService/PauseJob":           {"scheduler", "pause"},
	"/quicbaas.scheduler.v1.SchedulerService/ResumeJob":          {"scheduler", "resume"},
	"/quicbaas.scheduler.v1.SchedulerService/CancelJob":          {"scheduler", "cancel"},
	"/quicbaas.scheduler.v1.SchedulerService/GetJobHistory":      {"scheduler", "history"},
	"/quicbaas.scheduler.v1.SchedulerService/GetSchedulerStatus": {"scheduler", "status"},

	// ---- quicbaas.parameterstore.v1.ParameterStore ----
	"/quicbaas.parameterstore.v1.ParameterStore/GetParameter":     {"parameters", "read"},
	"/quicbaas.parameterstore.v1.ParameterStore/GetParameters":    {"parameters", "read"},
	"/quicbaas.parameterstore.v1.ParameterStore/SetParameter":     {"parameters", "write"},
	"/quicbaas.parameterstore.v1.ParameterStore/SetParameters":    {"parameters", "write"},
	"/quicbaas.parameterstore.v1.ParameterStore/DeleteParameter":  {"parameters", "delete"},
	"/quicbaas.parameterstore.v1.ParameterStore/DeleteParameters": {"parameters", "delete"},
	"/quicbaas.parameterstore.v1.ParameterStore/ListParameters":   {"parameters", "list"},
	"/quicbaas.parameterstore.v1.ParameterStore/SearchByTags":     {"parameters", "search"},

	// ---- quicbaas.dynamicsecrets.v1.DynamicSecretsService ----
	"/quicbaas.dynamicsecrets.v1.DynamicSecretsService/CreateRole":         {"dynamic_secrets", "role_create"},
	"/quicbaas.dynamicsecrets.v1.DynamicSecretsService/GetRole":            {"dynamic_secrets", "role_read"},
	"/quicbaas.dynamicsecrets.v1.DynamicSecretsService/ListRoles":          {"dynamic_secrets", "role_list"},
	"/quicbaas.dynamicsecrets.v1.DynamicSecretsService/DeleteRole":         {"dynamic_secrets", "role_delete"},
	"/quicbaas.dynamicsecrets.v1.DynamicSecretsService/GenerateCredential": {"dynamic_secrets", "generate"},
	"/quicbaas.dynamicsecrets.v1.DynamicSecretsService/RenewLease":         {"dynamic_secrets", "lease_renew"},
	"/quicbaas.dynamicsecrets.v1.DynamicSecretsService/RevokeLease":        {"dynamic_secrets", "lease_revoke"},
	"/quicbaas.dynamicsecrets.v1.DynamicSecretsService/ListLeases":         {"dynamic_secrets", "lease_list"},

	// ---- quicbaas.project.v1.ProjectService ----
	"/quicbaas.project.v1.ProjectService/CreateProject":    {"project", "create"},
	"/quicbaas.project.v1.ProjectService/GetProject":       {"project", "read"},
	"/quicbaas.project.v1.ProjectService/GetProjectByName": {"project", "read"},
	"/quicbaas.project.v1.ProjectService/ListProjects":     {"project", "list"},
	"/quicbaas.project.v1.ProjectService/UpdateProject":    {"project", "update"},
	"/quicbaas.project.v1.ProjectService/DeleteProject":    {"project", "delete"},

	// ---- quicbaas.ollama.v1.OllamaService ----
	"/quicbaas.ollama.v1.OllamaService/Health":         {"ollama", "health"},
	"/quicbaas.ollama.v1.OllamaService/ListModels":     {"ollama", "model_list"},
	"/quicbaas.ollama.v1.OllamaService/GetModelInfo":   {"ollama", "model_info"},
	"/quicbaas.ollama.v1.OllamaService/Embed":          {"ollama", "embed"},
	"/quicbaas.ollama.v1.OllamaService/EmbedBatch":     {"ollama", "embed"},
	"/quicbaas.ollama.v1.OllamaService/EmbedWithModel": {"ollama", "embed"},
}

// grpcResourceAndAction returns the qrdb-canonical (resource, action) for the
// gRPC FullMethod and whether a mapping exists. An unmapped method has ok=false:
// the default-deny authz gate denies it under RequireAuthz.
func grpcResourceAndAction(fullMethod string) (resource, action string, ok bool) {
	if perm, found := grpcMethodPermissions[fullMethod]; found {
		return perm.resource, perm.action, true
	}
	return "", "", false
}

// isPublicGRPCMethod reports whether the gRPC method bypasses authorization.
func isPublicGRPCMethod(fullMethod string) bool {
	return publicGRPCMethods[fullMethod]
}
