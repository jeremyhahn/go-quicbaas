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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// TestGetResourceAndAction_KVOperations tests all KV operation opcodes
func TestGetResourceAndAction_KVOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		{"OpGet", OpGet, interfaces.ResourceKeys, interfaces.ActionRead},
		{"OpList", OpList, interfaces.ResourceKeys, interfaces.ActionRead},
		{"OpPut", OpPut, interfaces.ResourceKeys, interfaces.ActionWrite},
		{"OpDelete", OpDelete, interfaces.ResourceKeys, interfaces.ActionDelete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// TestGetResourceAndAction_BatchOperations tests all batch operation opcodes
func TestGetResourceAndAction_BatchOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		{"OpBatchGet", OpBatchGet, interfaces.ResourceKeys, interfaces.ActionRead},
		{"OpBatchPut", OpBatchPut, interfaces.ResourceKeys, interfaces.ActionWrite},
		{"OpBatchDelete", OpBatchDelete, interfaces.ResourceKeys, interfaces.ActionDelete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// TestGetResourceAndAction_TransactionOperations tests transaction operation
func TestGetResourceAndAction_TransactionOperations(t *testing.T) {
	resource, action := GetResourceAndActionForTest(OpTransaction)
	if resource != interfaces.ResourceTransactions {
		t.Errorf("expected resource %q, got %q", interfaces.ResourceTransactions, resource)
	}
	if action != interfaces.ActionWrite {
		t.Errorf("expected action %q, got %q", interfaces.ActionWrite, action)
	}
}

// TestGetResourceAndAction_QueryOperations tests query operation opcodes
func TestGetResourceAndAction_QueryOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		{"OpQuery", OpQuery, interfaces.ResourceKeys, interfaces.ActionRead},
		{"OpScanIndex", OpScanIndex, interfaces.ResourceKeys, interfaces.ActionRead},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// TestGetResourceAndAction_ClusterOperations tests all cluster operation opcodes
func TestGetResourceAndAction_ClusterOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		// Read operations
		{"OpClusterStatus", OpClusterStatus, interfaces.ResourceCluster, interfaces.ActionRead},
		{"OpGetLeader", OpGetLeader, interfaces.ResourceCluster, interfaces.ActionRead},
		{"OpListNodes", OpListNodes, interfaces.ResourceCluster, interfaces.ActionRead},
		{"OpRebalanceStatus", OpRebalanceStatus, interfaces.ResourceCluster, interfaces.ActionRead},
		{"OpRebalancePlan", OpRebalancePlan, interfaces.ResourceCluster, interfaces.ActionRead},
		// Admin operations
		{"OpAddNode", OpAddNode, interfaces.ResourceCluster, interfaces.ActionAdmin},
		{"OpRemoveNode", OpRemoveNode, interfaces.ResourceCluster, interfaces.ActionAdmin},
		{"OpCreateShard", OpCreateShard, interfaces.ResourceCluster, interfaces.ActionAdmin},
		{"OpRebalanceCluster", OpRebalanceCluster, interfaces.ResourceCluster, interfaces.ActionAdmin},
		{"OpCancelMigration", OpCancelMigration, interfaces.ResourceCluster, interfaces.ActionAdmin},
		{"OpNodeReplaceRequest", OpNodeReplaceRequest, interfaces.ResourceCluster, interfaces.ActionAdmin},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// TestGetResourceAndAction_BackupOperations tests all backup operation opcodes
func TestGetResourceAndAction_BackupOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		{"OpListBackups", OpListBackups, interfaces.ResourceBackups, interfaces.ActionRead},
		{"OpCreateBackup", OpCreateBackup, interfaces.ResourceBackups, interfaces.ActionWrite},
		{"OpRestoreBackup", OpRestoreBackup, interfaces.ResourceBackups, interfaces.ActionWrite},
		{"OpVerifyBackup", OpVerifyBackup, interfaces.ResourceBackups, interfaces.ActionWrite},
		{"OpDeleteBackup", OpDeleteBackup, interfaces.ResourceBackups, interfaces.ActionDelete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// TestGetResourceAndAction_AuthOperations tests auth operation opcodes
func TestGetResourceAndAction_AuthOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		{"OpLogout", OpLogout, "auth", interfaces.ActionWrite},
		{"OpRefreshToken", OpRefreshToken, "auth", interfaces.ActionWrite},
		{"OpValidateToken", OpValidateToken, "auth", interfaces.ActionWrite},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// TestGetResourceAndAction_RBACOperations tests all RBAC operation opcodes
func TestGetResourceAndAction_RBACOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		// Read
		{"OpListUsers", OpListUsers, interfaces.ResourceUsers, interfaces.ActionRead},
		{"OpListRoles", OpListRoles, interfaces.ResourceUsers, interfaces.ActionRead},
		// Write
		{"OpCreateUser", OpCreateUser, interfaces.ResourceUsers, interfaces.ActionWrite},
		{"OpAssignRole", OpAssignRole, interfaces.ResourceUsers, interfaces.ActionWrite},
		// Delete
		{"OpDeleteUser", OpDeleteUser, interfaces.ResourceUsers, interfaces.ActionDelete},
		{"OpRevokeRole", OpRevokeRole, interfaces.ResourceUsers, interfaces.ActionDelete},
		// Roles
		{"OpCreateRole", OpCreateRole, interfaces.ResourceRoles, interfaces.ActionWrite},
		{"OpDeleteRole", OpDeleteRole, interfaces.ResourceRoles, interfaces.ActionDelete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// TestGetResourceAndAction_CacheOperations tests all cache operation opcodes
func TestGetResourceAndAction_CacheOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		{"OpGetCacheStats", OpGetCacheStats, "cache", interfaces.ActionRead},
		{"OpGetCacheConfig", OpGetCacheConfig, "cache", interfaces.ActionRead},
		{"OpClearCache", OpClearCache, "cache", interfaces.ActionAdmin},
		{"OpUpdateCacheConfig", OpUpdateCacheConfig, "cache", interfaces.ActionAdmin},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// TestGetResourceAndAction_TimeSeriesOperations tests all time-series operation opcodes
func TestGetResourceAndAction_TimeSeriesOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		// Read
		{"OpAggregate", OpAggregate, interfaces.ResourceTimeseries, interfaces.ActionRead},
		{"OpTimeSeriesQuery", OpTimeSeriesQuery, interfaces.ResourceTimeseries, interfaces.ActionRead},
		{"OpTSQuery", OpTSQuery, interfaces.ResourceTimeseries, interfaces.ActionRead},
		{"OpTSGetRetentionPolicy", OpTSGetRetentionPolicy, interfaces.ResourceTimeseries, interfaces.ActionRead},
		{"OpTSListMetrics", OpTSListMetrics, interfaces.ResourceTimeseries, interfaces.ActionRead},
		// Write
		{"OpTSWritePoint", OpTSWritePoint, interfaces.ResourceTimeseries, interfaces.ActionWrite},
		{"OpTSWriteBatch", OpTSWriteBatch, interfaces.ResourceTimeseries, interfaces.ActionWrite},
		{"OpTSSetRetentionPolicy", OpTSSetRetentionPolicy, interfaces.ResourceTimeseries, interfaces.ActionWrite},
		// Delete
		{"OpTSDeleteSeries", OpTSDeleteSeries, interfaces.ResourceTimeseries, interfaces.ActionDelete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// TestGetResourceAndAction_OperationsManagement tests operations management opcodes
func TestGetResourceAndAction_OperationsManagement(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		// Read
		{"OpGetMigrationProgress", OpGetMigrationProgress, interfaces.ResourceCluster, interfaces.ActionRead},
		{"OpGetUpgradeStatus", OpGetUpgradeStatus, interfaces.ResourceCluster, interfaces.ActionRead},
		// Admin
		{"OpMigrateData", OpMigrateData, interfaces.ResourceCluster, interfaces.ActionAdmin},
		{"OpReplaceNode", OpReplaceNode, interfaces.ResourceCluster, interfaces.ActionAdmin},
		{"OpUpgradeNode", OpUpgradeNode, interfaces.ResourceCluster, interfaces.ActionAdmin},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// TestGetResourceAndAction_BarrierCeremonyOperations replaces the deprecated
// SealOperations test. After Phase 8c the QUIC layer routes barrier and
// ceremony traffic through the single OpBarrier (0xC0) and OpCeremony (0xC1)
// dispatchers, each mapped to its dedicated resource and ActionAdmin.
func TestGetResourceAndAction_BarrierCeremonyOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		{"OpBarrier", OpBarrier, interfaces.ResourceBarrier, interfaces.ActionAdmin},
		{"OpCeremony", OpCeremony, interfaces.ResourceCeremony, interfaces.ActionAdmin},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// TestGetResourceAndAction_KMSOperations tests all KMS operation opcodes
func TestGetResourceAndAction_KMSOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		// Read
		{"OpKMSGetKey", OpKMSGetKey, interfaces.ResourceKMS, interfaces.ActionRead},
		{"OpKMSListKeys", OpKMSListKeys, interfaces.ResourceKMS, interfaces.ActionRead},
		{"OpKMSKeyVersions", OpKMSKeyVersions, interfaces.ResourceKMS, interfaces.ActionRead},
		// Write
		{"OpKMSGenerateKey", OpKMSGenerateKey, interfaces.ResourceKMS, interfaces.ActionWrite},
		{"OpKMSImportKey", OpKMSImportKey, interfaces.ResourceKMS, interfaces.ActionWrite},
		{"OpKMSRotateKey", OpKMSRotateKey, interfaces.ResourceKMS, interfaces.ActionWrite},
		{"OpKMSEncrypt", OpKMSEncrypt, interfaces.ResourceKMS, interfaces.ActionWrite},
		{"OpKMSDecrypt", OpKMSDecrypt, interfaces.ResourceKMS, interfaces.ActionWrite},
		{"OpKMSKeyStatus", OpKMSKeyStatus, interfaces.ResourceKMS, interfaces.ActionWrite},
		// Delete
		{"OpKMSDeleteKey", OpKMSDeleteKey, interfaces.ResourceKMS, interfaces.ActionDelete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// TestGetResourceAndAction_KMSDEKOperations tests all KMS DEK operation opcodes
func TestGetResourceAndAction_KMSDEKOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		// Read
		{"OpKMSDEKGet", OpKMSDEKGet, interfaces.ResourceKMS, interfaces.ActionRead},
		{"OpKMSDEKList", OpKMSDEKList, interfaces.ResourceKMS, interfaces.ActionRead},
		{"OpKMSDEKStatus", OpKMSDEKStatus, interfaces.ResourceKMS, interfaces.ActionRead},
		// Write
		{"OpKMSDEKCreate", OpKMSDEKCreate, interfaces.ResourceKMS, interfaces.ActionWrite},
		{"OpKMSDEKRotate", OpKMSDEKRotate, interfaces.ResourceKMS, interfaces.ActionWrite},
		{"OpKMSDEKSetDefault", OpKMSDEKSetDefault, interfaces.ResourceKMS, interfaces.ActionWrite},
		{"OpKMSDEKSetSafety", OpKMSDEKSetSafety, interfaces.ResourceKMS, interfaces.ActionWrite},
		{"OpKMSDEKClearSafety", OpKMSDEKClearSafety, interfaces.ResourceKMS, interfaces.ActionWrite},
		// Delete
		{"OpKMSDEKDelete", OpKMSDEKDelete, interfaces.ResourceKMS, interfaces.ActionDelete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// TestGetResourceAndAction_KMSBackendOperations tests KMS backend operation opcodes
func TestGetResourceAndAction_KMSBackendOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		{"OpKMSBackendStatus", OpKMSBackendStatus, interfaces.ResourceKMS, interfaces.ActionRead},
		{"OpKMSBackendTest", OpKMSBackendTest, interfaces.ResourceKMS, interfaces.ActionRead},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// TestGetResourceAndAction_SecretsOperations tests all secrets operation opcodes
func TestGetResourceAndAction_SecretsOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		// Read
		{"OpSecretsGet", OpSecretsGet, interfaces.ResourceSecrets, interfaces.ActionRead},
		{"OpSecretsList", OpSecretsList, interfaces.ResourceSecrets, interfaces.ActionRead},
		{"OpSecretsListVersions", OpSecretsListVersions, interfaces.ResourceSecrets, interfaces.ActionRead},
		// Write
		{"OpSecretsStore", OpSecretsStore, interfaces.ResourceSecrets, interfaces.ActionWrite},
		{"OpSecretsRollback", OpSecretsRollback, interfaces.ResourceSecrets, interfaces.ActionWrite},
		{"OpSecretsRotate", OpSecretsRotate, interfaces.ResourceSecrets, interfaces.ActionWrite},
		// Delete
		{"OpSecretsDelete", OpSecretsDelete, interfaces.ResourceSecrets, interfaces.ActionDelete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// TestGetResourceAndAction_CryptoOperations tests all crypto operation opcodes
func TestGetResourceAndAction_CryptoOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		{"OpCryptoEncrypt", OpCryptoEncrypt, interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"OpCryptoDecrypt", OpCryptoDecrypt, interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"OpCryptoRewrap", OpCryptoRewrap, interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"OpCryptoSign", OpCryptoSign, interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"OpCryptoVerify", OpCryptoVerify, interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"OpCryptoHash", OpCryptoHash, interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"OpCryptoRandom", OpCryptoRandom, interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"OpCryptoDataKey", OpCryptoDataKey, interfaces.ResourceCrypto, interfaces.ActionWrite},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// --- Content moved from resource_action_extended_test.go ---

// =============================================================================
// Vector Operations Tests
// =============================================================================

func TestGetResourceAndAction_VectorOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		// Read operations
		{"OpVectorListNamespaces", OpVectorListNamespaces, interfaces.ResourceVector, interfaces.ActionRead},
		{"OpVectorGetNamespaceInfo", OpVectorGetNamespaceInfo, interfaces.ResourceVector, interfaces.ActionRead},
		{"OpVectorGet", OpVectorGet, interfaces.ResourceVector, interfaces.ActionRead},
		{"OpVectorSearchSimilar", OpVectorSearchSimilar, interfaces.ResourceVector, interfaces.ActionRead},
		{"OpVectorSearchByMetadata", OpVectorSearchByMetadata, interfaces.ResourceVector, interfaces.ActionRead},
		{"OpVectorHybridSearch", OpVectorHybridSearch, interfaces.ResourceVector, interfaces.ActionRead},
		{"OpVectorSearchByID", OpVectorSearchByID, interfaces.ResourceVector, interfaces.ActionRead},
		{"OpVectorGetRebuildProgress", OpVectorGetRebuildProgress, interfaces.ResourceVector, interfaces.ActionRead},
		{"OpVectorGetIndexStats", OpVectorGetIndexStats, interfaces.ResourceVector, interfaces.ActionRead},
		{"OpVectorGetCompactionProgress", OpVectorGetCompactionProgress, interfaces.ResourceVector, interfaces.ActionRead},
		// Write operations
		{"OpVectorCreateNamespace", OpVectorCreateNamespace, interfaces.ResourceVector, interfaces.ActionWrite},
		{"OpVectorUpsert", OpVectorUpsert, interfaces.ResourceVector, interfaces.ActionWrite},
		{"OpVectorBatchUpsert", OpVectorBatchUpsert, interfaces.ResourceVector, interfaces.ActionWrite},
		{"OpVectorRebuildIndex", OpVectorRebuildIndex, interfaces.ResourceVector, interfaces.ActionWrite},
		{"OpVectorCompact", OpVectorCompact, interfaces.ResourceVector, interfaces.ActionWrite},
		// Delete operations
		{"OpVectorDeleteNamespace", OpVectorDeleteNamespace, interfaces.ResourceVector, interfaces.ActionDelete},
		{"OpVectorDelete", OpVectorDelete, interfaces.ResourceVector, interfaces.ActionDelete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// =============================================================================
// ObjectStorage Operations Tests
// =============================================================================

func TestGetResourceAndAction_ObjectStorageOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		// Read operations
		{"OpOSGetObject", OpOSGetObject, interfaces.ResourceObjectStore, interfaces.ActionRead},
		{"OpOSHeadObject", OpOSHeadObject, interfaces.ResourceObjectStore, interfaces.ActionRead},
		{"OpOSListObjects", OpOSListObjects, interfaces.ResourceObjectStore, interfaces.ActionRead},
		{"OpOSListByTag", OpOSListByTag, interfaces.ResourceObjectStore, interfaces.ActionRead},
		{"OpOSListByTenant", OpOSListByTenant, interfaces.ResourceObjectStore, interfaces.ActionRead},
		{"OpOSGetObjectMetadata", OpOSGetObjectMetadata, interfaces.ResourceObjectStore, interfaces.ActionRead},
		{"OpOSGetStorageStats", OpOSGetStorageStats, interfaces.ResourceObjectStore, interfaces.ActionRead},
		{"OpOSGetLifecyclePolicy", OpOSGetLifecyclePolicy, interfaces.ResourceObjectStore, interfaces.ActionRead},
		// Write operations
		{"OpOSPutObject", OpOSPutObject, interfaces.ResourceObjectStore, interfaces.ActionWrite},
		{"OpOSCopyObject", OpOSCopyObject, interfaces.ResourceObjectStore, interfaces.ActionWrite},
		{"OpOSReplicateObject", OpOSReplicateObject, interfaces.ResourceObjectStore, interfaces.ActionWrite},
		{"OpOSMigrateObject", OpOSMigrateObject, interfaces.ResourceObjectStore, interfaces.ActionWrite},
		{"OpOSArchiveObject", OpOSArchiveObject, interfaces.ResourceObjectStore, interfaces.ActionWrite},
		{"OpOSRestoreObject", OpOSRestoreObject, interfaces.ResourceObjectStore, interfaces.ActionWrite},
		{"OpOSVerifyObject", OpOSVerifyObject, interfaces.ResourceObjectStore, interfaces.ActionWrite},
		{"OpOSUpdateObjectMetadata", OpOSUpdateObjectMetadata, interfaces.ResourceObjectStore, interfaces.ActionWrite},
		{"OpOSSetLifecyclePolicy", OpOSSetLifecyclePolicy, interfaces.ResourceObjectStore, interfaces.ActionWrite},
		// Delete operations
		{"OpOSDeleteObject", OpOSDeleteObject, interfaces.ResourceObjectStore, interfaces.ActionDelete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

func TestGetResourceAndAction_ObjectStorageExtendedOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		// Extended Read operations
		{"OpOSGetObjectPolicy", OpOSGetObjectPolicy, interfaces.ResourceObjectStore, interfaces.ActionRead},
		{"OpOSListBackends", OpOSListBackends, interfaces.ResourceObjectStore, interfaces.ActionRead},
		{"OpOSGetBackendHealth", OpOSGetBackendHealth, interfaces.ResourceObjectStore, interfaces.ActionRead},
		// Batch Write
		{"OpOSBatchPutObjects", OpOSBatchPutObjects, interfaces.ResourceObjectStore, interfaces.ActionWrite},
		// Batch Read
		{"OpOSBatchGetObjects", OpOSBatchGetObjects, interfaces.ResourceObjectStore, interfaces.ActionRead},
		// Batch Delete
		{"OpOSBatchDeleteObjects", OpOSBatchDeleteObjects, interfaces.ResourceObjectStore, interfaces.ActionDelete},
		// Admin
		{"OpOSSetTenantQuotas", OpOSSetTenantQuotas, interfaces.ResourceObjectStore, interfaces.ActionAdmin},
		{"OpOSAdmin", OpOSAdmin, interfaces.ResourceObjectStore, interfaces.ActionAdmin},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

func TestGetResourceAndAction_ObjectTransferOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		{"OpFetchObject", OpFetchObject, interfaces.ResourceObjectStore, interfaces.ActionRead},
		{"OpStreamFetchObjects", OpStreamFetchObjects, interfaces.ResourceObjectStore, interfaces.ActionRead},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// =============================================================================
// Embedding Operations Tests
// =============================================================================

func TestGetResourceAndAction_EmbeddingOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		// Read operations
		{"OpEmbeddingListProviders", OpEmbeddingListProviders, interfaces.ResourceVector, interfaces.ActionRead},
		{"OpEmbeddingGetProvider", OpEmbeddingGetProvider, interfaces.ResourceVector, interfaces.ActionRead},
		{"OpEmbeddingGetCacheStats", OpEmbeddingGetCacheStats, interfaces.ResourceVector, interfaces.ActionRead},
		// Write operations
		{"OpEmbeddingGenerate", OpEmbeddingGenerate, interfaces.ResourceVector, interfaces.ActionWrite},
		{"OpEmbeddingBatchGenerate", OpEmbeddingBatchGenerate, interfaces.ResourceVector, interfaces.ActionWrite},
		{"OpEmbeddingSetDefault", OpEmbeddingSetDefault, interfaces.ResourceVector, interfaces.ActionWrite},
		{"OpEmbeddingClearCache", OpEmbeddingClearCache, interfaces.ResourceVector, interfaces.ActionWrite},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// =============================================================================
// Ollama Operations Tests
// =============================================================================

func TestGetResourceAndAction_OllamaOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		// Read operations
		{"OpOllamaListModels", OpOllamaListModels, interfaces.ResourceVector, interfaces.ActionRead},
		{"OpOllamaGetModelInfo", OpOllamaGetModelInfo, interfaces.ResourceVector, interfaces.ActionRead},
		{"OpOllamaHealth", OpOllamaHealth, interfaces.ResourceVector, interfaces.ActionRead},
		// Write operations
		{"OpOllamaEmbed", OpOllamaEmbed, interfaces.ResourceVector, interfaces.ActionWrite},
		{"OpOllamaEmbedWithModel", OpOllamaEmbedWithModel, interfaces.ResourceVector, interfaces.ActionWrite},
		{"OpOllamaEmbedBatch", OpOllamaEmbedBatch, interfaces.ResourceVector, interfaces.ActionWrite},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// =============================================================================
// RAG Operations Tests
// =============================================================================

func TestGetResourceAndAction_RAGOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		{"OpRAGQuery", OpRAGQuery, interfaces.ResourceVector, interfaces.ActionRead},
		{"OpRAGBuildContext", OpRAGBuildContext, interfaces.ResourceVector, interfaces.ActionRead},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// =============================================================================
// Dynamic Secrets Operations Tests
// =============================================================================

func TestGetResourceAndAction_DynamicSecretsOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		// Read operations
		{"OpDynamicSecretsGetRole", OpDynamicSecretsGetRole, interfaces.ResourceSecrets, interfaces.ActionRead},
		{"OpDynamicSecretsListRoles", OpDynamicSecretsListRoles, interfaces.ResourceSecrets, interfaces.ActionRead},
		{"OpDynamicSecretsListLeases", OpDynamicSecretsListLeases, interfaces.ResourceSecrets, interfaces.ActionRead},
		// Write operations
		{"OpDynamicSecretsCreateRole", OpDynamicSecretsCreateRole, interfaces.ResourceSecrets, interfaces.ActionWrite},
		{"OpDynamicSecretsGenerate", OpDynamicSecretsGenerate, interfaces.ResourceSecrets, interfaces.ActionWrite},
		{"OpDynamicSecretsRenewLease", OpDynamicSecretsRenewLease, interfaces.ResourceSecrets, interfaces.ActionWrite},
		// Delete operations
		{"OpDynamicSecretsDeleteRole", OpDynamicSecretsDeleteRole, interfaces.ResourceSecrets, interfaces.ActionDelete},
		{"OpDynamicSecretsRevokeLease", OpDynamicSecretsRevokeLease, interfaces.ResourceSecrets, interfaces.ActionDelete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// =============================================================================
// Certificate Operations Tests
// =============================================================================

func TestGetResourceAndAction_CertificateOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		// Read operations
		{"OpCertVerify", OpCertVerify, interfaces.ResourceCertificate, interfaces.ActionRead},
		{"OpCertGetCA", OpCertGetCA, interfaces.ResourceCertificate, interfaces.ActionRead},
		{"OpCertGetTLSConfig", OpCertGetTLSConfig, interfaces.ResourceCertificate, interfaces.ActionRead},
		// Write operations
		{"OpCertIssue", OpCertIssue, interfaces.ResourceCertificate, interfaces.ActionWrite},
		{"OpCertSignCSR", OpCertSignCSR, interfaces.ResourceCertificate, interfaces.ActionWrite},
		// Delete operations
		{"OpCertRevoke", OpCertRevoke, interfaces.ResourceCertificate, interfaces.ActionDelete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// =============================================================================
// Parameter Store Operations Tests
// =============================================================================

func TestGetResourceAndAction_ParameterStoreOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		// Read operations
		{"OpParameterGet", OpParameterGet, interfaces.ResourceParameters, interfaces.ActionRead},
		{"OpParameterList", OpParameterList, interfaces.ResourceParameters, interfaces.ActionRead},
		{"OpParameterBatchGet", OpParameterBatchGet, interfaces.ResourceParameters, interfaces.ActionRead},
		{"OpParameterSearch", OpParameterSearch, interfaces.ResourceParameters, interfaces.ActionRead},
		// Write operations
		{"OpParameterSet", OpParameterSet, interfaces.ResourceParameters, interfaces.ActionWrite},
		{"OpParameterBatchSet", OpParameterBatchSet, interfaces.ResourceParameters, interfaces.ActionWrite},
		// Delete operations
		{"OpParameterDelete", OpParameterDelete, interfaces.ResourceParameters, interfaces.ActionDelete},
		{"OpParameterBatchDelete", OpParameterBatchDelete, interfaces.ResourceParameters, interfaces.ActionDelete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// =============================================================================
// Additional Missing Secrets Operations Tests
// =============================================================================

func TestGetResourceAndAction_AdditionalSecretsOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		// Additional read operations
		{"OpSecretsGetVersion", OpSecretsGetVersion, interfaces.ResourceSecrets, interfaces.ActionRead},
		{"OpSecretsGetRotation", OpSecretsGetRotation, interfaces.ResourceSecrets, interfaces.ActionRead},
		{"OpSecretsGetByPrefix", OpSecretsGetByPrefix, interfaces.ResourceSecrets, interfaces.ActionRead},
		{"OpSecretsWatch", OpSecretsWatch, interfaces.ResourceSecrets, interfaces.ActionRead},
		// Additional write operations
		{"OpSecretsSetRotation", OpSecretsSetRotation, interfaces.ResourceSecrets, interfaces.ActionWrite},
		{"OpSecretsUpdateStatus", OpSecretsUpdateStatus, interfaces.ResourceSecrets, interfaces.ActionWrite},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// =============================================================================
// Additional Missing KMS DEK Operations Tests
// =============================================================================

func TestGetResourceAndAction_AdditionalKMSDEKOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		{"OpKMSDEKGetReencryptProgress", OpKMSDEKGetReencryptProgress, interfaces.ResourceKMS, interfaces.ActionRead},
		{"OpKMSDEKStartReencryption", OpKMSDEKStartReencryption, interfaces.ResourceKMS, interfaces.ActionWrite},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// =============================================================================
// Additional Crypto Operations Tests
// =============================================================================

func TestGetResourceAndAction_AdditionalCryptoOperations(t *testing.T) {
	tests := []struct {
		name             string
		opcode           byte
		expectedResource string
		expectedAction   string
	}{
		{"OpCryptoHMAC", OpCryptoHMAC, interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"OpCryptoVerifyHMAC", OpCryptoVerifyHMAC, interfaces.ResourceCrypto, interfaces.ActionWrite},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := GetResourceAndActionForTest(tt.opcode)
			if resource != tt.expectedResource {
				t.Errorf("expected resource %q, got %q", tt.expectedResource, resource)
			}
			if action != tt.expectedAction {
				t.Errorf("expected action %q, got %q", tt.expectedAction, action)
			}
		})
	}
}

// =============================================================================
// Unknown Opcode Tests
// =============================================================================

func TestGetResourceAndAction_UnknownOpcode(t *testing.T) {
	// Test with an opcode that should fall through to default
	resource, action := GetResourceAndActionForTest(0x00)
	// The default case returns "unknown" and ActionRead
	if resource != "unknown" {
		t.Errorf("expected resource %q for unknown opcode, got %q", "unknown", resource)
	}
	if action != interfaces.ActionRead {
		t.Errorf("expected action %q for unknown opcode, got %q", interfaces.ActionRead, action)
	}
}
