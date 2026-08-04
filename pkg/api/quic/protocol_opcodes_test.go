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
	"strings"
	"testing"
)

// =============================================================================
// SchedulerSubOpcodeToString Tests
// =============================================================================

func TestSchedulerSubOpcodeToString(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected string
	}{
		{SubOpSchedulerCreateJob, "SchedulerCreateJob"},
		{SubOpSchedulerGetJob, "SchedulerGetJob"},
		{SubOpSchedulerUpdateJob, "SchedulerUpdateJob"},
		{SubOpSchedulerDeleteJob, "SchedulerDeleteJob"},
		{SubOpSchedulerListJobs, "SchedulerListJobs"},
		{SubOpSchedulerTriggerJob, "SchedulerTriggerJob"},
		{SubOpSchedulerPauseJob, "SchedulerPauseJob"},
		{SubOpSchedulerResumeJob, "SchedulerResumeJob"},
		{SubOpSchedulerCancelJob, "SchedulerCancelJob"},
		{SubOpSchedulerGetJobHistory, "SchedulerGetJobHistory"},
		{SubOpSchedulerGetStatus, "SchedulerGetStatus"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := SchedulerSubOpcodeToString(tt.opcode)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestSchedulerSubOpcodeToString_Unknown(t *testing.T) {
	result := SchedulerSubOpcodeToString(0xFF)
	if !strings.HasPrefix(result, "UnknownSchedulerOp") {
		t.Fatalf("Expected UnknownSchedulerOp prefix, got %q", result)
	}
}

// =============================================================================
// VectorExtendedSubOpcodeToString Tests
// =============================================================================

func TestVectorExtendedSubOpcodeToString(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected string
	}{
		{SubOpVectorBatchDelete, "VectorBatchDelete"},
		{SubOpVectorListVectors, "VectorListVectors"},
		{SubOpVectorUpdateMetadata, "VectorUpdateMetadata"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := VectorExtendedSubOpcodeToString(tt.opcode)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestVectorExtendedSubOpcodeToString_Unknown(t *testing.T) {
	result := VectorExtendedSubOpcodeToString(0xFF)
	if !strings.HasPrefix(result, "UnknownVectorExtendedOp") {
		t.Fatalf("Expected UnknownVectorExtendedOp prefix, got %q", result)
	}
}

// =============================================================================
// SecretsSubOpcodeToString Tests
// =============================================================================

func TestSecretsSubOpcodeToString(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected string
	}{
		{OpSecretsSubEnsure, "SecretsEnsure"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := SecretsSubOpcodeToString(tt.opcode)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestSecretsSubOpcodeToString_Unknown(t *testing.T) {
	result := SecretsSubOpcodeToString(0xFF)
	if !strings.HasPrefix(result, "UnknownSecretsSubOp") {
		t.Fatalf("Expected UnknownSecretsSubOp prefix, got %q", result)
	}
}

// =============================================================================
// OpcodeToString - Remaining Uncovered Opcodes
// =============================================================================

func TestOpcodeToString_IAMOperations(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected string
	}{
		{OpIAMCreateTenant, "IAMCreateTenant"},
		{OpIAMGetTenant, "IAMGetTenant"},
		{OpIAMUpdateTenant, "IAMUpdateTenant"},
		{OpIAMDeleteTenant, "IAMDeleteTenant"},
		{OpIAMListTenants, "IAMListTenants"},
		{OpIAMCreateUser, "IAMCreateUser"},
		{OpIAMGetUser, "IAMGetUser"},
		{OpIAMUpdateUser, "IAMUpdateUser"},
		{OpIAMDeleteUser, "IAMDeleteUser"},
		{OpIAMListUsers, "IAMListUsers"},
		{OpIAMCreateGroup, "IAMCreateGroup"},
		{OpIAMGetGroup, "IAMGetGroup"},
		{OpIAMUpdateGroup, "IAMUpdateGroup"},
		{OpIAMDeleteGroup, "IAMDeleteGroup"},
		{OpIAMListGroups, "IAMListGroups"},
		{OpIAMAddUserToGroup, "IAMAddUserToGroup"},
		{OpIAMRemoveUserFromGroup, "IAMRemoveUserFromGroup"},
		{OpIAMCreateRole, "IAMCreateRole"},
		{OpIAMGetRole, "IAMGetRole"},
		{OpIAMUpdateRole, "IAMUpdateRole"},
		{OpIAMDeleteRole, "IAMDeleteRole"},
		{OpIAMListRoles, "IAMListRoles"},
		{OpIAMAssignRole, "IAMAssignRole"},
		{OpIAMRevokeRole, "IAMRevokeRole"},
		{OpIAMRegisterApp, "IAMRegisterApp"},
		{OpIAMGetApp, "IAMGetApp"},
		{OpIAMUpdateApp, "IAMUpdateApp"},
		{OpIAMDeleteApp, "IAMDeleteApp"},
		{OpIAMListApps, "IAMListApps"},
		{OpIAMRotateAppSecret, "IAMRotateAppSecret"},
		{OpIAMCreatePolicy, "IAMCreatePolicy"},
		{OpIAMGetPolicy, "IAMGetPolicy"},
		{OpIAMUpdatePolicy, "IAMUpdatePolicy"},
		{OpIAMDeletePolicy, "IAMDeletePolicy"},
		{OpIAMListPolicies, "IAMListPolicies"},
		{OpIAMValidatePolicy, "IAMValidatePolicy"},
		{OpIAMTestPolicy, "IAMTestPolicy"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := OpcodeToString(tt.opcode)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestOpcodeToString_KMSExtendedOperations(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected string
	}{
		{OpKMSDEKCreate, "KMSDEKCreate"},
		{OpKMSDEKGet, "KMSDEKGet"},
		{OpKMSDEKList, "KMSDEKList"},
		{OpKMSDEKRotate, "KMSDEKRotate"},
		{OpKMSDEKDelete, "KMSDEKDelete"},
		{OpKMSDEKStatus, "KMSDEKStatus"},
		{OpKMSDEKSetDefault, "KMSDEKSetDefault"},
		{OpKMSDEKSetSafety, "KMSDEKSetSafety"},
		{OpKMSDEKClearSafety, "KMSDEKClearSafety"},
		{OpKMSBackendStatus, "KMSBackendStatus"},
		{OpKMSBackendTest, "KMSBackendTest"},
		{OpKMSKeyVersions, "KMSKeyVersions"},
		{OpKMSKeyStatus, "KMSKeyStatus"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := OpcodeToString(tt.opcode)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestOpcodeToString_SecretsExtendedOperations(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected string
	}{
		{OpSecretsGetVersion, "SecretsGetVersion"},
		{OpSecretsSetRotation, "SecretsSetRotation"},
		{OpSecretsGetRotation, "SecretsGetRotation"},
		{OpSecretsUpdateStatus, "SecretsUpdateStatus"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := OpcodeToString(tt.opcode)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestOpcodeToString_VectorOperations(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected string
	}{
		{OpVectorCreateNamespace, "VectorCreateNamespace"},
		{OpVectorDeleteNamespace, "VectorDeleteNamespace"},
		{OpVectorListNamespaces, "VectorListNamespaces"},
		{OpVectorGetNamespaceInfo, "VectorGetNamespaceInfo"},
		{OpVectorUpsert, "VectorUpsert"},
		{OpVectorGet, "VectorGet"},
		{OpVectorDelete, "VectorDelete"},
		{OpVectorBatchUpsert, "VectorBatchUpsert"},
		{OpVectorSearchSimilar, "VectorSearchSimilar"},
		{OpVectorSearchByMetadata, "VectorSearchByMetadata"},
		{OpVectorHybridSearch, "VectorHybridSearch"},
		{OpVectorSearchByID, "VectorSearchByID"},
		{OpVectorRebuildIndex, "VectorRebuildIndex"},
		{OpVectorGetRebuildProgress, "VectorGetRebuildProgress"},
		{OpVectorGetIndexStats, "VectorGetIndexStats"},
		{OpVectorCompact, "VectorCompact"},
		{OpVectorGetCompactionProgress, "VectorGetCompactionProgress"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := OpcodeToString(tt.opcode)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestOpcodeToString_ObjectStorageOperations(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected string
	}{
		{OpOSPutObject, "OSPutObject"},
		{OpOSGetObject, "OSGetObject"},
		{OpOSDeleteObject, "OSDeleteObject"},
		{OpOSHeadObject, "OSHeadObject"},
		{OpOSListObjects, "OSListObjects"},
		{OpOSListByTag, "OSListByTag"},
		{OpOSListByTenant, "OSListByTenant"},
		{OpOSCopyObject, "OSCopyObject"},
		{OpOSReplicateObject, "OSReplicateObject"},
		{OpOSMigrateObject, "OSMigrateObject"},
		{OpOSArchiveObject, "OSArchiveObject"},
		{OpOSRestoreObject, "OSRestoreObject"},
		{OpOSVerifyObject, "OSVerifyObject"},
		{OpOSGetObjectMetadata, "OSGetObjectMetadata"},
		{OpOSUpdateObjectMetadata, "OSUpdateObjectMetadata"},
		{OpOSGetStorageStats, "OSGetStorageStats"},
		{OpOSSetLifecyclePolicy, "OSSetLifecyclePolicy"},
		{OpOSGetLifecyclePolicy, "OSGetLifecyclePolicy"},
		{OpOSGetObjectPolicy, "OSGetObjectPolicy"},
		{OpOSListBackends, "OSListBackends"},
		{OpOSGetBackendHealth, "OSGetBackendHealth"},
		{OpOSSetTenantQuotas, "OSSetTenantQuotas"},
		{OpOSBatchPutObjects, "OSBatchPutObjects"},
		{OpOSBatchGetObjects, "OSBatchGetObjects"},
		{OpOSBatchDeleteObjects, "OSBatchDeleteObjects"},
		{OpOSAdmin, "OSAdmin"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := OpcodeToString(tt.opcode)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestOpcodeToString_FetchAndTransferOperations(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected string
	}{
		{OpFetchObject, "FetchObject"},
		{OpStreamFetchObjects, "StreamFetchObjects"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := OpcodeToString(tt.opcode)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestOpcodeToString_OllamaAndEmbeddingOperations(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected string
	}{
		{OpOllamaEmbed, "OllamaEmbed"},
		{OpOllamaEmbedWithModel, "OllamaEmbedWithModel"},
		{OpOllamaEmbedBatch, "OllamaEmbedBatch"},
		{OpOllamaListModels, "OllamaListModels"},
		{OpOllamaGetModelInfo, "OllamaGetModelInfo"},
		{OpOllamaHealth, "OllamaHealth"},
		{OpEmbeddingGenerate, "EmbeddingGenerate"},
		{OpEmbeddingBatchGenerate, "EmbeddingBatchGenerate"},
		{OpEmbeddingListProviders, "EmbeddingListProviders"},
		{OpEmbeddingGetProvider, "EmbeddingGetProvider"},
		{OpEmbeddingSetDefault, "EmbeddingSetDefault"},
		{OpEmbeddingGetCacheStats, "EmbeddingGetCacheStats"},
		{OpEmbeddingClearCache, "EmbeddingClearCache"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := OpcodeToString(tt.opcode)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestOpcodeToString_RAGAndPEPOperations(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected string
	}{
		{OpRAGQuery, "RAGQuery"},
		{OpRAGBuildContext, "RAGBuildContext"},
		{OpPEPCheck, "PEPCheck"},
		{OpPEPCheckBatch, "PEPCheckBatch"},
		{OpPEPSubscribe, "PEPSubscribe"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := OpcodeToString(tt.opcode)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestOpcodeToString_KMSExtendedKeyOps(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected string
	}{
		{OpKMSSetKeyPolicy, "KMSSetKeyPolicy"},
		{OpKMSGetKeyPolicy, "KMSGetKeyPolicy"},
		{OpKMSSetKeySafety, "KMSSetKeySafety"},
		{OpKMSClearKeySafety, "KMSClearKeySafety"},
		{OpKMSSafetyCheck, "KMSSafetyCheck"},
		{OpKMSSafetyReport, "KMSSafetyReport"},
		{OpKMSExportKey, "KMSExportKey"},
		{OpKMSUnwrapKey, "KMSUnwrapKey"},
		{OpKMSGetKeyMetadata, "KMSGetKeyMetadata"},
		{OpKMSUpdateKeyMetadata, "KMSUpdateKeyMetadata"},
		{OpKMSDEKStartReencryption, "KMSDEKStartReencryption"},
		{OpKMSDEKGetReencryptProgress, "KMSDEKGetReencryptProgress"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := OpcodeToString(tt.opcode)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestOpcodeToString_DynamicSecrets(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected string
	}{
		{OpDynamicSecretsCreateRole, "DynamicSecretsCreateRole"},
		{OpDynamicSecretsGetRole, "DynamicSecretsGetRole"},
		{OpDynamicSecretsListRoles, "DynamicSecretsListRoles"},
		{OpDynamicSecretsDeleteRole, "DynamicSecretsDeleteRole"},
		{OpDynamicSecretsGenerate, "DynamicSecretsGenerate"},
		{OpDynamicSecretsRenewLease, "DynamicSecretsRenewLease"},
		{OpDynamicSecretsRevokeLease, "DynamicSecretsRevokeLease"},
		{OpDynamicSecretsListLeases, "DynamicSecretsListLeases"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := OpcodeToString(tt.opcode)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestOpcodeToString_ParameterStoreAndCert(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected string
	}{
		{OpParameterGet, "ParameterGet"},
		{OpParameterSet, "ParameterSet"},
		{OpParameterDelete, "ParameterDelete"},
		{OpParameterList, "ParameterList"},
		{OpParameterBatchGet, "ParameterBatchGet"},
		{OpParameterBatchSet, "ParameterBatchSet"},
		{OpParameterBatchDelete, "ParameterBatchDelete"},
		{OpParameterSearch, "ParameterSearch"},
		{OpCertIssue, "CertIssue"},
		{OpCertSignCSR, "CertSignCSR"},
		{OpCertRevoke, "CertRevoke"},
		{OpCertVerify, "CertVerify"},
		{OpCertGetCA, "CertGetCA"},
		{OpCertGetTLSConfig, "CertGetTLSConfig"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := OpcodeToString(tt.opcode)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestOpcodeToString_SecretsExtended(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected string
	}{
		{OpSecretsGetByPrefix, "SecretsGetByPrefix"},
		{OpSecretsWatch, "SecretsWatch"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := OpcodeToString(tt.opcode)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}
