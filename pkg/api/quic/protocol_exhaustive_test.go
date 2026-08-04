//go:build quic

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TestOpcodeToString_Exhaustive tests OpcodeToString with all defined opcodes.
func TestOpcodeToString_Exhaustive(t *testing.T) {
	tests := []struct {
		name     string
		opcode   byte
		expected string
	}{
		// Scheduler Operations (prefix-dispatched)
		{"OpScheduler", OpScheduler, "Scheduler"},

		// KV Operations
		{"OpPut", OpPut, "Put"},
		{"OpGet", OpGet, "Get"},
		{"OpDelete", OpDelete, "Delete"},
		{"OpList", OpList, "List"},

		// Additional Secrets Operations
		{"OpSecretsGetByPrefix", OpSecretsGetByPrefix, "SecretsGetByPrefix"},
		{"OpSecretsWatch", OpSecretsWatch, "SecretsWatch"},

		// DEK Re-encryption Operations
		{"OpKMSDEKStartReencryption", OpKMSDEKStartReencryption, "KMSDEKStartReencryption"},
		{"OpKMSDEKGetReencryptProgress", OpKMSDEKGetReencryptProgress, "KMSDEKGetReencryptProgress"},

		// Extended KMS Operations
		{"OpKMSExportKey", OpKMSExportKey, "KMSExportKey"},
		{"OpKMSUnwrapKey", OpKMSUnwrapKey, "KMSUnwrapKey"},
		{"OpKMSGetKeyMetadata", OpKMSGetKeyMetadata, "KMSGetKeyMetadata"},
		{"OpKMSUpdateKeyMetadata", OpKMSUpdateKeyMetadata, "KMSUpdateKeyMetadata"},
		{"OpKMSSetKeyPolicy", OpKMSSetKeyPolicy, "KMSSetKeyPolicy"},
		{"OpKMSGetKeyPolicy", OpKMSGetKeyPolicy, "KMSGetKeyPolicy"},
		{"OpKMSSetKeySafety", OpKMSSetKeySafety, "KMSSetKeySafety"},

		// Batch Operations
		{"OpBatchPut", OpBatchPut, "BatchPut"},
		{"OpBatchGet", OpBatchGet, "BatchGet"},
		{"OpBatchDelete", OpBatchDelete, "BatchDelete"},

		// Extended KMS Safety Operations
		{"OpKMSClearKeySafety", OpKMSClearKeySafety, "KMSClearKeySafety"},
		{"OpKMSSafetyCheck", OpKMSSafetyCheck, "KMSSafetyCheck"},
		{"OpKMSSafetyReport", OpKMSSafetyReport, "KMSSafetyReport"},

		// Vector Operations
		{"OpVectorCreateNamespace", OpVectorCreateNamespace, "VectorCreateNamespace"},
		{"OpVectorDeleteNamespace", OpVectorDeleteNamespace, "VectorDeleteNamespace"},
		{"OpVectorListNamespaces", OpVectorListNamespaces, "VectorListNamespaces"},
		{"OpVectorGetNamespaceInfo", OpVectorGetNamespaceInfo, "VectorGetNamespaceInfo"},
		{"OpVectorUpsert", OpVectorUpsert, "VectorUpsert"},
		{"OpVectorGet", OpVectorGet, "VectorGet"},
		{"OpVectorDelete", OpVectorDelete, "VectorDelete"},
		{"OpVectorBatchUpsert", OpVectorBatchUpsert, "VectorBatchUpsert"},
		{"OpVectorSearchSimilar", OpVectorSearchSimilar, "VectorSearchSimilar"},
		{"OpVectorSearchByMetadata", OpVectorSearchByMetadata, "VectorSearchByMetadata"},
		{"OpVectorHybridSearch", OpVectorHybridSearch, "VectorHybridSearch"},
		{"OpVectorSearchByID", OpVectorSearchByID, "VectorSearchByID"},
		{"OpVectorRebuildIndex", OpVectorRebuildIndex, "VectorRebuildIndex"},
		{"OpVectorGetRebuildProgress", OpVectorGetRebuildProgress, "VectorGetRebuildProgress"},
		{"OpVectorGetIndexStats", OpVectorGetIndexStats, "VectorGetIndexStats"},
		{"OpVectorCompact", OpVectorCompact, "VectorCompact"},
		{"OpVectorGetCompactionProgress", OpVectorGetCompactionProgress, "VectorGetCompactionProgress"},

		// Transaction Operations
		{"OpTransaction", OpTransaction, "Transaction"},

		// ObjectStorage Operations
		{"OpOSPutObject", OpOSPutObject, "OSPutObject"},
		{"OpOSGetObject", OpOSGetObject, "OSGetObject"},
		{"OpOSDeleteObject", OpOSDeleteObject, "OSDeleteObject"},
		{"OpOSHeadObject", OpOSHeadObject, "OSHeadObject"},
		{"OpOSListObjects", OpOSListObjects, "OSListObjects"},
		{"OpOSListByTag", OpOSListByTag, "OSListByTag"},
		{"OpOSListByTenant", OpOSListByTenant, "OSListByTenant"},
		{"OpOSCopyObject", OpOSCopyObject, "OSCopyObject"},
		{"OpOSReplicateObject", OpOSReplicateObject, "OSReplicateObject"},
		{"OpOSMigrateObject", OpOSMigrateObject, "OSMigrateObject"},
		{"OpOSArchiveObject", OpOSArchiveObject, "OSArchiveObject"},
		{"OpOSRestoreObject", OpOSRestoreObject, "OSRestoreObject"},
		{"OpOSVerifyObject", OpOSVerifyObject, "OSVerifyObject"},
		{"OpOSGetObjectMetadata", OpOSGetObjectMetadata, "OSGetObjectMetadata"},
		{"OpOSUpdateObjectMetadata", OpOSUpdateObjectMetadata, "OSUpdateObjectMetadata"},
		{"OpOSGetStorageStats", OpOSGetStorageStats, "OSGetStorageStats"},
		{"OpOSSetLifecyclePolicy", OpOSSetLifecyclePolicy, "OSSetLifecyclePolicy"},
		{"OpOSGetLifecyclePolicy", OpOSGetLifecyclePolicy, "OSGetLifecyclePolicy"},

		// Query Operations
		{"OpQuery", OpQuery, "Query"},
		{"OpScanIndex", OpScanIndex, "ScanIndex"},

		// Ollama Operations
		{"OpOllamaEmbed", OpOllamaEmbed, "OllamaEmbed"},
		{"OpOllamaEmbedWithModel", OpOllamaEmbedWithModel, "OllamaEmbedWithModel"},
		{"OpOllamaEmbedBatch", OpOllamaEmbedBatch, "OllamaEmbedBatch"},
		{"OpOllamaListModels", OpOllamaListModels, "OllamaListModels"},
		{"OpOllamaGetModelInfo", OpOllamaGetModelInfo, "OllamaGetModelInfo"},
		{"OpOllamaHealth", OpOllamaHealth, "OllamaHealth"},

		// Cluster Operations
		{"OpClusterStatus", OpClusterStatus, "ClusterStatus"},
		{"OpGetLeader", OpGetLeader, "GetLeader"},
		{"OpAddNode", OpAddNode, "AddNode"},
		{"OpRemoveNode", OpRemoveNode, "RemoveNode"},
		{"OpCreateShard", OpCreateShard, "CreateShard"},
		{"OpRebalanceCluster", OpRebalanceCluster, "RebalanceCluster"},
		{"OpListNodes", OpListNodes, "ListNodes"},
		{"OpRebalanceStatus", OpRebalanceStatus, "RebalanceStatus"},
		{"OpRebalancePlan", OpRebalancePlan, "RebalancePlan"},
		{"OpCancelMigration", OpCancelMigration, "CancelMigration"},
		{"OpNodeReplaceRequest", OpNodeReplaceRequest, "NodeReplaceRequest"},

		// Embedding Operations
		{"OpEmbeddingGenerate", OpEmbeddingGenerate, "EmbeddingGenerate"},
		{"OpEmbeddingBatchGenerate", OpEmbeddingBatchGenerate, "EmbeddingBatchGenerate"},
		{"OpEmbeddingListProviders", OpEmbeddingListProviders, "EmbeddingListProviders"},
		{"OpEmbeddingGetProvider", OpEmbeddingGetProvider, "EmbeddingGetProvider"},
		{"OpEmbeddingSetDefault", OpEmbeddingSetDefault, "EmbeddingSetDefault"},
		{"OpEmbeddingGetCacheStats", OpEmbeddingGetCacheStats, "EmbeddingGetCacheStats"},
		{"OpEmbeddingClearCache", OpEmbeddingClearCache, "EmbeddingClearCache"},

		// Backup Operations
		{"OpCreateBackup", OpCreateBackup, "CreateBackup"},
		{"OpListBackups", OpListBackups, "ListBackups"},
		{"OpRestoreBackup", OpRestoreBackup, "RestoreBackup"},
		{"OpDeleteBackup", OpDeleteBackup, "DeleteBackup"},
		{"OpVerifyBackup", OpVerifyBackup, "VerifyBackup"},

		// RAG Operations
		{"OpRAGQuery", OpRAGQuery, "RAGQuery"},
		{"OpRAGBuildContext", OpRAGBuildContext, "RAGBuildContext"},

		// PEP Operations
		{"OpPEPCheck", OpPEPCheck, "PEPCheck"},
		{"OpPEPCheckBatch", OpPEPCheckBatch, "PEPCheckBatch"},
		{"OpPEPSubscribe", OpPEPSubscribe, "PEPSubscribe"},

		// ObjectStorage Extended Operations
		{"OpOSGetObjectPolicy", OpOSGetObjectPolicy, "OSGetObjectPolicy"},
		{"OpOSListBackends", OpOSListBackends, "OSListBackends"},
		{"OpOSGetBackendHealth", OpOSGetBackendHealth, "OSGetBackendHealth"},
		{"OpOSSetTenantQuotas", OpOSSetTenantQuotas, "OSSetTenantQuotas"},
		{"OpOSBatchPutObjects", OpOSBatchPutObjects, "OSBatchPutObjects"},
		{"OpOSBatchGetObjects", OpOSBatchGetObjects, "OSBatchGetObjects"},
		{"OpOSBatchDeleteObjects", OpOSBatchDeleteObjects, "OSBatchDeleteObjects"},

		// Auth Operations
		{"OpLogout", OpLogout, "Logout"},
		{"OpRefreshToken", OpRefreshToken, "RefreshToken"},
		{"OpValidateToken", OpValidateToken, "ValidateToken"},

		// Dynamic Secrets Operations
		{"OpDynamicSecretsCreateRole", OpDynamicSecretsCreateRole, "DynamicSecretsCreateRole"},
		{"OpDynamicSecretsGetRole", OpDynamicSecretsGetRole, "DynamicSecretsGetRole"},
		{"OpDynamicSecretsListRoles", OpDynamicSecretsListRoles, "DynamicSecretsListRoles"},
		{"OpDynamicSecretsDeleteRole", OpDynamicSecretsDeleteRole, "DynamicSecretsDeleteRole"},
		{"OpDynamicSecretsGenerate", OpDynamicSecretsGenerate, "DynamicSecretsGenerate"},
		{"OpDynamicSecretsRenewLease", OpDynamicSecretsRenewLease, "DynamicSecretsRenewLease"},
		{"OpDynamicSecretsRevokeLease", OpDynamicSecretsRevokeLease, "DynamicSecretsRevokeLease"},
		{"OpDynamicSecretsListLeases", OpDynamicSecretsListLeases, "DynamicSecretsListLeases"},

		// RBAC Operations
		{"OpCreateUser", OpCreateUser, "CreateUser"},
		{"OpDeleteUser", OpDeleteUser, "DeleteUser"},
		{"OpListUsers", OpListUsers, "ListUsers"},
		{"OpCreateRole", OpCreateRole, "CreateRole"},
		{"OpDeleteRole", OpDeleteRole, "DeleteRole"},
		{"OpListRoles", OpListRoles, "ListRoles"},
		{"OpAssignRole", OpAssignRole, "AssignRole"},
		{"OpRevokeRole", OpRevokeRole, "RevokeRole"},

		// Parameter Store Operations
		{"OpParameterGet", OpParameterGet, "ParameterGet"},
		{"OpParameterSet", OpParameterSet, "ParameterSet"},
		{"OpParameterDelete", OpParameterDelete, "ParameterDelete"},
		{"OpParameterList", OpParameterList, "ParameterList"},
		{"OpParameterBatchGet", OpParameterBatchGet, "ParameterBatchGet"},
		{"OpParameterBatchSet", OpParameterBatchSet, "ParameterBatchSet"},
		{"OpParameterBatchDelete", OpParameterBatchDelete, "ParameterBatchDelete"},
		{"OpParameterSearch", OpParameterSearch, "ParameterSearch"},

		// Cache Operations
		{"OpGetCacheStats", OpGetCacheStats, "GetCacheStats"},
		{"OpClearCache", OpClearCache, "ClearCache"},
		{"OpGetCacheConfig", OpGetCacheConfig, "GetCacheConfig"},
		{"OpUpdateCacheConfig", OpUpdateCacheConfig, "UpdateCacheConfig"},

		// Certificate Operations
		{"OpCertIssue", OpCertIssue, "CertIssue"},
		{"OpCertSignCSR", OpCertSignCSR, "CertSignCSR"},
		{"OpCertRevoke", OpCertRevoke, "CertRevoke"},
		{"OpCertVerify", OpCertVerify, "CertVerify"},
		{"OpCertGetCA", OpCertGetCA, "CertGetCA"},
		{"OpCertGetTLSConfig", OpCertGetTLSConfig, "CertGetTLSConfig"},

		// Health Operations
		{"OpHealth", OpHealth, "Health"},
		{"OpLiveness", OpLiveness, "Liveness"},
		{"OpReadiness", OpReadiness, "Readiness"},

		// IAM Tenant Operations
		{"OpIAMCreateTenant", OpIAMCreateTenant, "IAMCreateTenant"},
		{"OpIAMGetTenant", OpIAMGetTenant, "IAMGetTenant"},
		{"OpIAMUpdateTenant", OpIAMUpdateTenant, "IAMUpdateTenant"},
		{"OpIAMDeleteTenant", OpIAMDeleteTenant, "IAMDeleteTenant"},
		{"OpIAMListTenants", OpIAMListTenants, "IAMListTenants"},

		// IAM User Operations
		{"OpIAMCreateUser", OpIAMCreateUser, "IAMCreateUser"},
		{"OpIAMGetUser", OpIAMGetUser, "IAMGetUser"},
		{"OpIAMUpdateUser", OpIAMUpdateUser, "IAMUpdateUser"},
		{"OpIAMDeleteUser", OpIAMDeleteUser, "IAMDeleteUser"},
		{"OpIAMListUsers", OpIAMListUsers, "IAMListUsers"},

		// IAM Group Operations
		{"OpIAMCreateGroup", OpIAMCreateGroup, "IAMCreateGroup"},
		{"OpIAMGetGroup", OpIAMGetGroup, "IAMGetGroup"},
		{"OpIAMUpdateGroup", OpIAMUpdateGroup, "IAMUpdateGroup"},
		{"OpIAMDeleteGroup", OpIAMDeleteGroup, "IAMDeleteGroup"},
		{"OpIAMListGroups", OpIAMListGroups, "IAMListGroups"},
		{"OpIAMAddUserToGroup", OpIAMAddUserToGroup, "IAMAddUserToGroup"},
		{"OpIAMRemoveUserFromGroup", OpIAMRemoveUserFromGroup, "IAMRemoveUserFromGroup"},

		// Time-Series Operations
		{"OpAggregate", OpAggregate, "Aggregate"},
		{"OpTimeSeriesQuery", OpTimeSeriesQuery, "TimeSeriesQuery"},
		{"OpTSWritePoint", OpTSWritePoint, "TSWritePoint"},
		{"OpTSWriteBatch", OpTSWriteBatch, "TSWriteBatch"},
		{"OpTSQuery", OpTSQuery, "TSQuery"},
		{"OpTSDeleteSeries", OpTSDeleteSeries, "TSDeleteSeries"},
		{"OpTSGetRetentionPolicy", OpTSGetRetentionPolicy, "TSGetRetentionPolicy"},
		{"OpTSSetRetentionPolicy", OpTSSetRetentionPolicy, "TSSetRetentionPolicy"},
		{"OpTSListMetrics", OpTSListMetrics, "TSListMetrics"},

		// ObjectTransfer Operations
		{"OpFetchObject", OpFetchObject, "FetchObject"},
		{"OpStreamFetchObjects", OpStreamFetchObjects, "StreamFetchObjects"},

		// Operations Management
		{"OpMigrateData", OpMigrateData, "MigrateData"},
		{"OpGetMigrationProgress", OpGetMigrationProgress, "GetMigrationProgress"},
		{"OpReplaceNode", OpReplaceNode, "ReplaceNode"},
		{"OpUpgradeNode", OpUpgradeNode, "UpgradeNode"},
		{"OpGetUpgradeStatus", OpGetUpgradeStatus, "GetUpgradeStatus"},

		// IAM Role Operations
		{"OpIAMCreateRole", OpIAMCreateRole, "IAMCreateRole"},
		{"OpIAMGetRole", OpIAMGetRole, "IAMGetRole"},
		{"OpIAMUpdateRole", OpIAMUpdateRole, "IAMUpdateRole"},
		{"OpIAMDeleteRole", OpIAMDeleteRole, "IAMDeleteRole"},
		{"OpIAMListRoles", OpIAMListRoles, "IAMListRoles"},
		{"OpIAMAssignRole", OpIAMAssignRole, "IAMAssignRole"},
		{"OpIAMRevokeRole", OpIAMRevokeRole, "IAMRevokeRole"},

		// IAM App Operations
		{"OpIAMRegisterApp", OpIAMRegisterApp, "IAMRegisterApp"},
		{"OpIAMGetApp", OpIAMGetApp, "IAMGetApp"},
		{"OpIAMUpdateApp", OpIAMUpdateApp, "IAMUpdateApp"},
		{"OpIAMDeleteApp", OpIAMDeleteApp, "IAMDeleteApp"},
		{"OpIAMListApps", OpIAMListApps, "IAMListApps"},
		{"OpIAMRotateAppSecret", OpIAMRotateAppSecret, "IAMRotateAppSecret"},

		// Barrier / Ceremony Operations (sub-op dispatched)
		{"OpBarrier", OpBarrier, "Barrier"},
		{"OpCeremony", OpCeremony, "Ceremony"},

		// IAM Policy Operations
		{"OpIAMCreatePolicy", OpIAMCreatePolicy, "IAMCreatePolicy"},
		{"OpIAMGetPolicy", OpIAMGetPolicy, "IAMGetPolicy"},
		{"OpIAMUpdatePolicy", OpIAMUpdatePolicy, "IAMUpdatePolicy"},
		{"OpIAMDeletePolicy", OpIAMDeletePolicy, "IAMDeletePolicy"},
		{"OpIAMListPolicies", OpIAMListPolicies, "IAMListPolicies"},
		{"OpIAMValidatePolicy", OpIAMValidatePolicy, "IAMValidatePolicy"},
		{"OpIAMTestPolicy", OpIAMTestPolicy, "IAMTestPolicy"},

		// ObjectStorage Admin Operations
		{"OpOSAdmin", OpOSAdmin, "OSAdmin"},

		// KMS Operations
		{"OpKMSGenerateKey", OpKMSGenerateKey, "KMSGenerateKey"},
		{"OpKMSImportKey", OpKMSImportKey, "KMSImportKey"},
		{"OpKMSGetKey", OpKMSGetKey, "KMSGetKey"},
		{"OpKMSListKeys", OpKMSListKeys, "KMSListKeys"},
		{"OpKMSRotateKey", OpKMSRotateKey, "KMSRotateKey"},
		{"OpKMSDeleteKey", OpKMSDeleteKey, "KMSDeleteKey"},
		{"OpKMSEncrypt", OpKMSEncrypt, "KMSEncrypt"},
		{"OpKMSDecrypt", OpKMSDecrypt, "KMSDecrypt"},

		// KMS DEK Operations
		{"OpKMSDEKCreate", OpKMSDEKCreate, "KMSDEKCreate"},
		{"OpKMSDEKGet", OpKMSDEKGet, "KMSDEKGet"},
		{"OpKMSDEKList", OpKMSDEKList, "KMSDEKList"},
		{"OpKMSDEKRotate", OpKMSDEKRotate, "KMSDEKRotate"},
		{"OpKMSDEKDelete", OpKMSDEKDelete, "KMSDEKDelete"},
		{"OpKMSDEKStatus", OpKMSDEKStatus, "KMSDEKStatus"},
		{"OpKMSDEKSetDefault", OpKMSDEKSetDefault, "KMSDEKSetDefault"},
		{"OpKMSDEKSetSafety", OpKMSDEKSetSafety, "KMSDEKSetSafety"},

		// Secrets Operations
		{"OpSecretsStore", OpSecretsStore, "SecretsStore"},
		{"OpSecretsGet", OpSecretsGet, "SecretsGet"},
		{"OpSecretsList", OpSecretsList, "SecretsList"},
		{"OpSecretsDelete", OpSecretsDelete, "SecretsDelete"},
		{"OpSecretsListVersions", OpSecretsListVersions, "SecretsListVersions"},
		{"OpSecretsRollback", OpSecretsRollback, "SecretsRollback"},
		{"OpSecretsRotate", OpSecretsRotate, "SecretsRotate"},

		// KMS Backend and Extended DEK Operations
		{"OpKMSDEKClearSafety", OpKMSDEKClearSafety, "KMSDEKClearSafety"},
		{"OpKMSBackendStatus", OpKMSBackendStatus, "KMSBackendStatus"},
		{"OpKMSBackendTest", OpKMSBackendTest, "KMSBackendTest"},
		{"OpKMSKeyVersions", OpKMSKeyVersions, "KMSKeyVersions"},
		{"OpKMSKeyStatus", OpKMSKeyStatus, "KMSKeyStatus"},

		// Extended Secrets Operations
		{"OpSecretsGetVersion", OpSecretsGetVersion, "SecretsGetVersion"},
		{"OpSecretsSetRotation", OpSecretsSetRotation, "SecretsSetRotation"},
		{"OpSecretsGetRotation", OpSecretsGetRotation, "SecretsGetRotation"},
		{"OpSecretsUpdateStatus", OpSecretsUpdateStatus, "SecretsUpdateStatus"},

		// Secrets Sub-Dispatch Operations
		{"OpSecretsSubDispatch", OpSecretsSubDispatch, "SecretsSubDispatch"},

		// Transit Operations
		{"OpCryptoEncrypt", OpCryptoEncrypt, "CryptoEncrypt"},
		{"OpCryptoDecrypt", OpCryptoDecrypt, "CryptoDecrypt"},
		{"OpCryptoRewrap", OpCryptoRewrap, "CryptoRewrap"},
		{"OpCryptoSign", OpCryptoSign, "CryptoSign"},
		{"OpCryptoVerify", OpCryptoVerify, "CryptoVerify"},
		{"OpCryptoHash", OpCryptoHash, "CryptoHash"},
		{"OpCryptoRandom", OpCryptoRandom, "CryptoRandom"},
		{"OpCryptoDataKey", OpCryptoDataKey, "CryptoDataKey"},
		{"OpCryptoHMAC", OpCryptoHMAC, "CryptoHMAC"},
		{"OpCryptoVerifyHMAC", OpCryptoVerifyHMAC, "CryptoVerifyHMAC"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := OpcodeToString(tc.opcode)
			if result != tc.expected {
				t.Errorf("OpcodeToString(0x%02X) = %q, expected %q", tc.opcode, result, tc.expected)
			}
		})
	}
}

// TestOpcodeToString_UnknownOpcodes_Exhaustive tests OpcodeToString with unknown opcodes.
func TestOpcodeToString_UnknownOpcodes_Exhaustive(t *testing.T) {
	// Test various undefined opcodes to ensure they return "Unknown(0xXX)"
	unknownOpcodes := []byte{0xFD, 0xFE, 0xFF}

	for _, opcode := range unknownOpcodes {
		t.Run(fmt.Sprintf("Unknown_0x%02X", opcode), func(t *testing.T) {
			result := OpcodeToString(opcode)
			expected := fmt.Sprintf("Unknown(0x%02X)", opcode)
			if result != expected {
				t.Errorf("OpcodeToString(0x%02X) = %q, expected %q", opcode, result, expected)
			}
		})
	}
}

// TestSchedulerSubOpcodeToString_Exhaustive tests all scheduler sub-opcodes.
func TestSchedulerSubOpcodeToString_Exhaustive(t *testing.T) {
	tests := []struct {
		name      string
		subOpcode byte
		expected  string
	}{
		{"SubOpSchedulerCreateJob", SubOpSchedulerCreateJob, "SchedulerCreateJob"},
		{"SubOpSchedulerGetJob", SubOpSchedulerGetJob, "SchedulerGetJob"},
		{"SubOpSchedulerUpdateJob", SubOpSchedulerUpdateJob, "SchedulerUpdateJob"},
		{"SubOpSchedulerDeleteJob", SubOpSchedulerDeleteJob, "SchedulerDeleteJob"},
		{"SubOpSchedulerListJobs", SubOpSchedulerListJobs, "SchedulerListJobs"},
		{"SubOpSchedulerTriggerJob", SubOpSchedulerTriggerJob, "SchedulerTriggerJob"},
		{"SubOpSchedulerPauseJob", SubOpSchedulerPauseJob, "SchedulerPauseJob"},
		{"SubOpSchedulerResumeJob", SubOpSchedulerResumeJob, "SchedulerResumeJob"},
		{"SubOpSchedulerCancelJob", SubOpSchedulerCancelJob, "SchedulerCancelJob"},
		{"SubOpSchedulerGetJobHistory", SubOpSchedulerGetJobHistory, "SchedulerGetJobHistory"},
		{"SubOpSchedulerGetStatus", SubOpSchedulerGetStatus, "SchedulerGetStatus"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := SchedulerSubOpcodeToString(tc.subOpcode)
			if result != tc.expected {
				t.Errorf("SchedulerSubOpcodeToString(0x%02X) = %q, expected %q", tc.subOpcode, result, tc.expected)
			}
		})
	}
}

// TestSchedulerSubOpcodeToString_UnknownSubOpcodes_Exhaustive tests unknown scheduler sub-opcodes.
func TestSchedulerSubOpcodeToString_UnknownSubOpcodes_Exhaustive(t *testing.T) {
	unknownSubOpcodes := []byte{0x00, 0x0C, 0x0D, 0x10, 0xFF}

	for _, subOpcode := range unknownSubOpcodes {
		t.Run(fmt.Sprintf("Unknown_0x%02X", subOpcode), func(t *testing.T) {
			result := SchedulerSubOpcodeToString(subOpcode)
			expected := fmt.Sprintf("UnknownSchedulerOp(0x%02X)", subOpcode)
			if result != expected {
				t.Errorf("SchedulerSubOpcodeToString(0x%02X) = %q, expected %q", subOpcode, result, expected)
			}
		})
	}
}

// TestVectorExtendedSubOpcodeToString_Exhaustive tests all vector extended sub-opcodes.
func TestVectorExtendedSubOpcodeToString_Exhaustive(t *testing.T) {
	tests := []struct {
		name      string
		subOpcode byte
		expected  string
	}{
		{"SubOpVectorBatchDelete", SubOpVectorBatchDelete, "VectorBatchDelete"},
		{"SubOpVectorListVectors", SubOpVectorListVectors, "VectorListVectors"},
		{"SubOpVectorUpdateMetadata", SubOpVectorUpdateMetadata, "VectorUpdateMetadata"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := VectorExtendedSubOpcodeToString(tc.subOpcode)
			if result != tc.expected {
				t.Errorf("VectorExtendedSubOpcodeToString(0x%02X) = %q, expected %q", tc.subOpcode, result, tc.expected)
			}
		})
	}
}

// TestVectorExtendedSubOpcodeToString_UnknownSubOpcodes_Exhaustive tests unknown vector extended sub-opcodes.
func TestVectorExtendedSubOpcodeToString_UnknownSubOpcodes_Exhaustive(t *testing.T) {
	unknownSubOpcodes := []byte{0x00, 0x01, 0x3F, 0x43, 0x44, 0xFF}

	for _, subOpcode := range unknownSubOpcodes {
		t.Run(fmt.Sprintf("Unknown_0x%02X", subOpcode), func(t *testing.T) {
			result := VectorExtendedSubOpcodeToString(subOpcode)
			expected := fmt.Sprintf("UnknownVectorExtendedOp(0x%02X)", subOpcode)
			if result != expected {
				t.Errorf("VectorExtendedSubOpcodeToString(0x%02X) = %q, expected %q", subOpcode, result, expected)
			}
		})
	}
}

// TestErrorToStatus_QBErrors_Exhaustive tests ErrorToStatus with various QBError codes.
func TestErrorToStatus_QBErrors_Exhaustive(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedStatus byte
	}{
		{
			name:           "nil error",
			err:            nil,
			expectedStatus: StatusOK,
		},
		{
			name: "ErrNotFound",
			err: &errors.QBError{
				Code: errors.ErrNotFound,
				Op:   "test",
			},
			expectedStatus: StatusNotFound,
		},
		{
			name: "ErrUnauthorized",
			err: &errors.QBError{
				Code: errors.ErrUnauthorized,
				Op:   "test",
			},
			expectedStatus: StatusUnauthorized,
		},
		{
			name: "ErrInvalidInput",
			err: &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "test",
			},
			expectedStatus: StatusInvalidArgument,
		},
		{
			name: "ErrAlreadyExists",
			err: &errors.QBError{
				Code: errors.ErrAlreadyExists,
				Op:   "test",
			},
			expectedStatus: StatusAlreadyExists,
		},
		{
			name: "ErrTimeout",
			err: &errors.QBError{
				Code: errors.ErrTimeout,
				Op:   "test",
			},
			expectedStatus: StatusTimeout,
		},
		{
			name: "ErrForbidden",
			err: &errors.QBError{
				Code: errors.ErrForbidden,
				Op:   "test",
			},
			expectedStatus: StatusPermissionDenied,
		},
		{
			name: "ErrResourceExhausted",
			err: &errors.QBError{
				Code: errors.ErrResourceExhausted,
				Op:   "test",
			},
			expectedStatus: StatusResourceExhausted,
		},
		{
			name: "ErrUnavailable",
			err: &errors.QBError{
				Code: errors.ErrUnavailable,
				Op:   "test",
			},
			expectedStatus: StatusUnavailable,
		},
		{
			name: "ErrInternal (default)",
			err: &errors.QBError{
				Code: errors.ErrInternal,
				Op:   "test",
			},
			expectedStatus: StatusInternalError,
		},
		{
			name: "ErrStorage (falls to default)",
			err: &errors.QBError{
				Code: errors.ErrStorage,
				Op:   "test",
			},
			expectedStatus: StatusInternalError,
		},
		{
			name: "ErrRaft (falls to default)",
			err: &errors.QBError{
				Code: errors.ErrRaft,
				Op:   "test",
			},
			expectedStatus: StatusInternalError,
		},
		{
			name: "ErrEncryption (falls to default)",
			err: &errors.QBError{
				Code: errors.ErrEncryption,
				Op:   "test",
			},
			expectedStatus: StatusInternalError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ErrorToStatus(tc.err)
			if result != tc.expectedStatus {
				t.Errorf("ErrorToStatus() = 0x%02X, expected 0x%02X", result, tc.expectedStatus)
			}
		})
	}
}

// TestErrorToStatus_ErrorMessagePatterns_Exhaustive tests ErrorToStatus with non-QBError errors
// that contain specific patterns in their error messages.
func TestErrorToStatus_ErrorMessagePatterns_Exhaustive(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedStatus byte
	}{
		{
			name:           "error with not found pattern",
			err:            &errors.MessageError{Message: "resource not found"},
			expectedStatus: StatusNotFound,
		},
		{
			name:           "error with Not Found pattern (case insensitive)",
			err:            &errors.MessageError{Message: "User Not Found in system"},
			expectedStatus: StatusNotFound,
		},
		{
			name:           "error with already exists pattern",
			err:            &errors.MessageError{Message: "key already exists"},
			expectedStatus: StatusAlreadyExists,
		},
		{
			name:           "error with Already Exists pattern (case insensitive)",
			err:            &errors.MessageError{Message: "Resource Already Exists"},
			expectedStatus: StatusAlreadyExists,
		},
		{
			name:           "error with unauthorized pattern",
			err:            &errors.MessageError{Message: "unauthorized access"},
			expectedStatus: StatusUnauthorized,
		},
		{
			name:           "error with authentication pattern",
			err:            &errors.MessageError{Message: "authentication required"},
			expectedStatus: StatusUnauthorized,
		},
		{
			name:           "error with forbidden pattern",
			err:            &errors.MessageError{Message: "forbidden action"},
			expectedStatus: StatusPermissionDenied,
		},
		{
			name:           "error with permission denied pattern",
			err:            &errors.MessageError{Message: "permission denied for resource"},
			expectedStatus: StatusPermissionDenied,
		},
		{
			name:           "generic error falls to internal",
			err:            &errors.MessageError{Message: "something went wrong"},
			expectedStatus: StatusInternalError,
		},
		{
			name:           "empty error message",
			err:            &errors.MessageError{Message: ""},
			expectedStatus: StatusInternalError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ErrorToStatus(tc.err)
			if result != tc.expectedStatus {
				t.Errorf("ErrorToStatus() = 0x%02X, expected 0x%02X", result, tc.expectedStatus)
			}
		})
	}
}

// TestStatusToError_Exhaustive tests StatusToError with all defined status codes.
func TestStatusToError_Exhaustive(t *testing.T) {
	tests := []struct {
		name         string
		status       byte
		message      string
		expectedCode errors.ErrorCode
		expectNil    bool
	}{
		{
			name:      "StatusOK returns nil",
			status:    StatusOK,
			message:   "success",
			expectNil: true,
		},
		{
			name:         "StatusNotFound",
			status:       StatusNotFound,
			message:      "resource not found",
			expectedCode: errors.ErrNotFound,
		},
		{
			name:         "StatusUnauthorized",
			status:       StatusUnauthorized,
			message:      "authentication required",
			expectedCode: errors.ErrUnauthorized,
		},
		{
			name:         "StatusInvalidArgument",
			status:       StatusInvalidArgument,
			message:      "invalid input",
			expectedCode: errors.ErrInvalidInput,
		},
		{
			name:         "StatusAlreadyExists",
			status:       StatusAlreadyExists,
			message:      "key already exists",
			expectedCode: errors.ErrAlreadyExists,
		},
		{
			name:         "StatusTimeout",
			status:       StatusTimeout,
			message:      "operation timed out",
			expectedCode: errors.ErrTimeout,
		},
		{
			name:         "StatusPermissionDenied",
			status:       StatusPermissionDenied,
			message:      "access forbidden",
			expectedCode: errors.ErrForbidden,
		},
		{
			name:         "StatusResourceExhausted",
			status:       StatusResourceExhausted,
			message:      "quota exceeded",
			expectedCode: errors.ErrResourceExhausted,
		},
		{
			name:         "StatusUnavailable",
			status:       StatusUnavailable,
			message:      "service unavailable",
			expectedCode: errors.ErrUnavailable,
		},
		{
			name:         "StatusInternalError",
			status:       StatusInternalError,
			message:      "internal server error",
			expectedCode: errors.ErrInternal,
		},
		{
			name:         "StatusInvalidRequest (default)",
			status:       StatusInvalidRequest,
			message:      "invalid request",
			expectedCode: errors.ErrInternal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := StatusToError(tc.status, tc.message)

			if tc.expectNil {
				if result != nil {
					t.Errorf("StatusToError() expected nil, got %v", result)
				}
				return
			}

			if result == nil {
				t.Fatalf("StatusToError() returned nil, expected error")
			}

			var qbErr *errors.QBError
			if !errors.As(result, &qbErr) {
				t.Fatalf("StatusToError() returned non-QBError: %T", result)
			}

			if qbErr.Code != tc.expectedCode {
				t.Errorf("StatusToError().Code = %v, expected %v", qbErr.Code, tc.expectedCode)
			}

			if qbErr.Op != "quic.protocol" {
				t.Errorf("StatusToError().Op = %q, expected %q", qbErr.Op, "quic.protocol")
			}

			if qbErr.Err == nil {
				t.Error("StatusToError().Err is nil, expected error message")
			} else if !strings.Contains(qbErr.Err.Error(), tc.message) {
				t.Errorf("StatusToError().Err = %q, expected to contain %q", qbErr.Err.Error(), tc.message)
			}
		})
	}
}

// TestStatusToError_UnknownStatusCodes_Exhaustive tests StatusToError with undefined status codes.
func TestStatusToError_UnknownStatusCodes_Exhaustive(t *testing.T) {
	unknownStatuses := []byte{0x0B, 0x0C, 0x10, 0xFF}

	for _, status := range unknownStatuses {
		t.Run(fmt.Sprintf("Unknown_0x%02X", status), func(t *testing.T) {
			result := StatusToError(status, "unknown error")

			if result == nil {
				t.Fatalf("StatusToError() returned nil, expected error")
			}

			var qbErr *errors.QBError
			if !errors.As(result, &qbErr) {
				t.Fatalf("StatusToError() returned non-QBError: %T", result)
			}

			// Unknown status codes should map to ErrInternal
			if qbErr.Code != errors.ErrInternal {
				t.Errorf("StatusToError().Code = %v, expected %v for unknown status", qbErr.Code, errors.ErrInternal)
			}
		})
	}
}

// TestStatusToError_EmptyMessage_Exhaustive tests StatusToError with empty message.
func TestStatusToError_EmptyMessage_Exhaustive(t *testing.T) {
	result := StatusToError(StatusNotFound, "")

	if result == nil {
		t.Fatalf("StatusToError() returned nil, expected error")
	}

	var qbErr *errors.QBError
	if !errors.As(result, &qbErr) {
		t.Fatalf("StatusToError() returned non-QBError: %T", result)
	}

	if qbErr.Code != errors.ErrNotFound {
		t.Errorf("StatusToError().Code = %v, expected %v", qbErr.Code, errors.ErrNotFound)
	}

	if qbErr.Err == nil {
		t.Error("StatusToError().Err should not be nil even with empty message")
	}
}

// TestErrorToStatus_WrappedQBError_Exhaustive tests ErrorToStatus with wrapped QBErrors.
func TestErrorToStatus_WrappedQBError_Exhaustive(t *testing.T) {
	baseErr := &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "test",
		Err:  &errors.MessageError{Message: "resource not found"},
	}

	wrappedErr := &errors.WrappedMessageError{Message: "wrapper", Cause: baseErr}

	result := ErrorToStatus(wrappedErr)
	if result != StatusNotFound {
		t.Errorf("ErrorToStatus() with wrapped error = 0x%02X, expected 0x%02X", result, StatusNotFound)
	}
}

// TestStatusConstantsValues_Exhaustive verifies status constant values are correct.
func TestStatusConstantsValues_Exhaustive(t *testing.T) {
	tests := []struct {
		name     string
		status   byte
		expected byte
	}{
		{"StatusOK", StatusOK, 0x00},
		{"StatusInvalidRequest", StatusInvalidRequest, 0x01},
		{"StatusNotFound", StatusNotFound, 0x02},
		{"StatusUnauthorized", StatusUnauthorized, 0x03},
		{"StatusInternalError", StatusInternalError, 0x04},
		{"StatusTimeout", StatusTimeout, 0x05},
		{"StatusAlreadyExists", StatusAlreadyExists, 0x06},
		{"StatusPermissionDenied", StatusPermissionDenied, 0x07},
		{"StatusResourceExhausted", StatusResourceExhausted, 0x08},
		{"StatusInvalidArgument", StatusInvalidArgument, 0x09},
		{"StatusUnavailable", StatusUnavailable, 0x0A},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.status != tc.expected {
				t.Errorf("%s = 0x%02X, expected 0x%02X", tc.name, tc.status, tc.expected)
			}
		})
	}
}

// TestOpcodeConstantsNotDuplicated_Exhaustive verifies opcodes are unique (except for shared prefix dispatch).
func TestOpcodeConstantsNotDuplicated_Exhaustive(t *testing.T) {
	// Map of opcode values to their names
	opcodeMap := make(map[byte][]string)

	opcodes := []struct {
		name  string
		value byte
	}{
		// Note: OpScheduler and OpMsg share 0x00 intentionally for prefix dispatch
		{"OpScheduler", OpScheduler},
		{"OpPut", OpPut},
		{"OpGet", OpGet},
		{"OpDelete", OpDelete},
		{"OpList", OpList},
		{"OpSecretsGetByPrefix", OpSecretsGetByPrefix},
		{"OpSecretsWatch", OpSecretsWatch},
		{"OpKMSDEKStartReencryption", OpKMSDEKStartReencryption},
		{"OpKMSDEKGetReencryptProgress", OpKMSDEKGetReencryptProgress},
		{"OpKMSExportKey", OpKMSExportKey},
		{"OpKMSUnwrapKey", OpKMSUnwrapKey},
		{"OpKMSGetKeyMetadata", OpKMSGetKeyMetadata},
		{"OpKMSUpdateKeyMetadata", OpKMSUpdateKeyMetadata},
		{"OpKMSSetKeyPolicy", OpKMSSetKeyPolicy},
		{"OpKMSGetKeyPolicy", OpKMSGetKeyPolicy},
		{"OpKMSSetKeySafety", OpKMSSetKeySafety},
		{"OpBatchPut", OpBatchPut},
		{"OpBatchGet", OpBatchGet},
		{"OpBatchDelete", OpBatchDelete},
		{"OpKMSClearKeySafety", OpKMSClearKeySafety},
		{"OpKMSSafetyCheck", OpKMSSafetyCheck},
		{"OpKMSSafetyReport", OpKMSSafetyReport},
	}

	for _, op := range opcodes {
		opcodeMap[op.value] = append(opcodeMap[op.value], op.name)
	}

	// Check for duplicates (allowing OpScheduler/OpMsg at 0x00)
	for value, names := range opcodeMap {
		if len(names) > 1 && value != 0x00 {
			t.Errorf("Duplicate opcode value 0x%02X used by: %v", value, names)
		}
	}
}

// TestOSAdminSubOpcodeValues tests ObjectStorage Admin sub-opcode constants.
func TestOSAdminSubOpcodeValues(t *testing.T) {
	tests := []struct {
		name      string
		subOpcode byte
		expected  byte
	}{
		{"SubOpOSAdminCreate", SubOpOSAdminCreate, 0x01},
		{"SubOpOSAdminUpdate", SubOpOSAdminUpdate, 0x02},
		{"SubOpOSAdminDelete", SubOpOSAdminDelete, 0x03},
		{"SubOpOSAdminSetStatus", SubOpOSAdminSetStatus, 0x04},
		{"SubOpOSAdminGetStats", SubOpOSAdminGetStats, 0x05},
		{"SubOpOSAdminRunGC", SubOpOSAdminRunGC, 0x06},
		{"SubOpOSAdminGetGC", SubOpOSAdminGetGC, 0x07},
		{"SubOpOSAdminRepair", SubOpOSAdminRepair, 0x08},
		{"SubOpOSAdminCompact", SubOpOSAdminCompact, 0x09},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.subOpcode != tc.expected {
				t.Errorf("%s = 0x%02X, expected 0x%02X", tc.name, tc.subOpcode, tc.expected)
			}
		})
	}
}

// TestSchedulerSubOpcodeValues tests Scheduler sub-opcode constants.
func TestSchedulerSubOpcodeValues(t *testing.T) {
	tests := []struct {
		name      string
		subOpcode byte
		expected  byte
	}{
		{"SubOpSchedulerCreateJob", SubOpSchedulerCreateJob, 0x01},
		{"SubOpSchedulerGetJob", SubOpSchedulerGetJob, 0x02},
		{"SubOpSchedulerUpdateJob", SubOpSchedulerUpdateJob, 0x03},
		{"SubOpSchedulerDeleteJob", SubOpSchedulerDeleteJob, 0x04},
		{"SubOpSchedulerListJobs", SubOpSchedulerListJobs, 0x05},
		{"SubOpSchedulerTriggerJob", SubOpSchedulerTriggerJob, 0x06},
		{"SubOpSchedulerPauseJob", SubOpSchedulerPauseJob, 0x07},
		{"SubOpSchedulerResumeJob", SubOpSchedulerResumeJob, 0x08},
		{"SubOpSchedulerCancelJob", SubOpSchedulerCancelJob, 0x09},
		{"SubOpSchedulerGetJobHistory", SubOpSchedulerGetJobHistory, 0x0A},
		{"SubOpSchedulerGetStatus", SubOpSchedulerGetStatus, 0x0B},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.subOpcode != tc.expected {
				t.Errorf("%s = 0x%02X, expected 0x%02X", tc.name, tc.subOpcode, tc.expected)
			}
		})
	}
}

// TestVectorExtendedSubOpcodeValues tests Vector extended sub-opcode constants.
func TestVectorExtendedSubOpcodeValues(t *testing.T) {
	tests := []struct {
		name      string
		subOpcode byte
		expected  byte
	}{
		{"SubOpVectorBatchDelete", SubOpVectorBatchDelete, 0x40},
		{"SubOpVectorListVectors", SubOpVectorListVectors, 0x41},
		{"SubOpVectorUpdateMetadata", SubOpVectorUpdateMetadata, 0x42},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.subOpcode != tc.expected {
				t.Errorf("%s = 0x%02X, expected 0x%02X", tc.name, tc.subOpcode, tc.expected)
			}
		})
	}
}
