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

package testutil

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Secrets Service Methods (returned by Secrets() accessor)
// =============================================================================

func (m *MockQuicBaaS) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.StoreSecretResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) EnsureSecret(ctx context.Context, req *service.EnsureSecretRequest) (*service.EnsureSecretResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.EnsureSecretResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetSecretResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ListSecretsResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.DeleteSecretResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetSecretVersion(ctx context.Context, req *service.GetSecretVersionRequest) (*service.GetSecretVersionResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetSecretVersionResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ListSecretVersionsResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RollbackSecretResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RotateSecretResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) SetRotationPolicy(ctx context.Context, req *service.SetRotationPolicyRequest) (*service.SetRotationPolicyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SetRotationPolicyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetRotationPolicy(ctx context.Context, req *service.GetRotationPolicyRequest) (*service.GetRotationPolicyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetRotationPolicyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.UpdateSecretStatusResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetSecretsByPrefixResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	args := m.Called(ctx, req)
	ch, _ := args.Get(0).(<-chan *service.SecretChange)
	return ch, args.Error(1)
}

// =============================================================================
// KMS Service Methods (returned by KMS() accessor when KMSService field is nil)
// =============================================================================

func (m *MockQuicBaaS) GetSupportedAlgorithms(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	resp, _ := args.Get(0).([]string)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GenerateKey(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GenerateKeyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ImportKey(ctx context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ImportKeyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetKey(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetKeyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetPublicKey(ctx context.Context, req *service.GetPublicKeyRequest) (*service.GetPublicKeyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetPublicKeyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ListKeys(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ListKeysResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) RotateKey(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RotateKeyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) RevokeKey(ctx context.Context, req *service.RevokeKeyRequest) (*service.RevokeKeyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RevokeKeyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) DeleteKey(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.DeleteKeyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetKeyMetadata(ctx context.Context, req *service.GetKeyMetadataRequest) (*service.GetKeyMetadataResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetKeyMetadataResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) UpdateKeyMetadata(ctx context.Context, req *service.UpdateKeyMetadataRequest) (*service.UpdateKeyMetadataResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.UpdateKeyMetadataResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) UpdateKeyStatus(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.UpdateKeyStatusResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ListKeyVersions(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ListKeyVersionsResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) SetKeyPolicy(ctx context.Context, req *service.SetKeyPolicyRequest) (*service.SetKeyPolicyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SetKeyPolicyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetKeyPolicy(ctx context.Context, req *service.GetKeyPolicyRequest) (*service.GetKeyPolicyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetKeyPolicyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.CreateDEKResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetDEKResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ListDEKsResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RotateDEKResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.DeleteDEKResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetDEKStatusResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SetDefaultDEKResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SetDEKSafetyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ClearDEKSafetyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.StartReencryptionResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetReencryptionProgressResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) BackendStatus(ctx context.Context) (*service.BackendStatusResponse, error) {
	args := m.Called(ctx)
	resp, _ := args.Get(0).(*service.BackendStatusResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) BackendTest(ctx context.Context) (*service.BackendTestResponse, error) {
	args := m.Called(ctx)
	resp, _ := args.Get(0).(*service.BackendTestResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ExportKey(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ExportKeyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) UnwrapKey(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.UnwrapKeyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) SetKeySafety(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SetKeySafetyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ClearKeySafety(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ClearKeySafetyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) SafetyCheck(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SafetyCheckResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) SafetyReport(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SafetyReportResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) SetDefaultKey(ctx context.Context, req *service.SetDefaultKeyRequest) (*service.SetDefaultKeyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SetDefaultKeyResponse)
	return resp, args.Error(1)
}

// =============================================================================
// Crypto Service Methods (returned by Crypto() accessor)
// =============================================================================

func (m *MockQuicBaaS) Encrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.EncryptResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) Decrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.DecryptResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) Rewrap(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RewrapResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) EncryptBatch(ctx context.Context, req *service.EncryptBatchRequest) (*service.EncryptBatchResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.EncryptBatchResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) DecryptBatch(ctx context.Context, req *service.DecryptBatchRequest) (*service.DecryptBatchResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.DecryptBatchResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GenerateDataKey(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GenerateDataKeyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GenerateWrappedDataKey(ctx context.Context, req *service.GenerateWrappedDataKeyRequest) (*service.GenerateWrappedDataKeyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GenerateWrappedDataKeyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) Sign(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SignResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) Verify(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.VerifyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GenerateHMAC(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.HMACResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) VerifyHMAC(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.VerifyHMACResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GenerateRandom(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GenerateRandomResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) Hash(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.HashResponse)
	return resp, args.Error(1)
}

// =============================================================================
// Vector Service Methods (returned by Vector() accessor when VectorService field is nil)
// =============================================================================

func (m *MockQuicBaaS) CreateNamespace(ctx context.Context, req *service.CreateNamespaceRequest) (*service.CreateNamespaceResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.CreateNamespaceResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) DeleteNamespace(ctx context.Context, req *service.DeleteNamespaceRequest) (*service.DeleteNamespaceResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.DeleteNamespaceResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ListNamespaces(ctx context.Context, req *service.ListNamespacesRequest) (*service.ListNamespacesResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ListNamespacesResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetNamespaceInfo(ctx context.Context, req *service.GetNamespaceInfoRequest) (*service.GetNamespaceInfoResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetNamespaceInfoResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) UpsertVector(ctx context.Context, req *service.UpsertVectorRequest) (*service.UpsertVectorResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.UpsertVectorResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetVector(ctx context.Context, req *service.GetVectorRequest) (*service.GetVectorResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetVectorResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) DeleteVector(ctx context.Context, req *service.DeleteVectorRequest) (*service.DeleteVectorResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.DeleteVectorResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) BatchUpsert(ctx context.Context, req *service.BatchUpsertRequest) (*service.BatchUpsertResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.BatchUpsertResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) BatchDeleteVectors(ctx context.Context, req *service.BatchDeleteVectorsRequest) (*service.BatchDeleteVectorsResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.BatchDeleteVectorsResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ListVectors(ctx context.Context, req *service.ListVectorsRequest) (*service.ListVectorsResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ListVectorsResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) UpdateMetadata(ctx context.Context, req *service.UpdateVectorMetadataRequest) (*service.UpdateVectorMetadataResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.UpdateVectorMetadataResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) SearchSimilar(ctx context.Context, req *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SearchSimilarResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) SearchByMetadata(ctx context.Context, req *service.SearchByMetadataRequest) (*service.SearchByMetadataResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SearchByMetadataResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) HybridSearch(ctx context.Context, req *service.HybridSearchRequest) (*service.HybridSearchResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.HybridSearchResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) SearchByID(ctx context.Context, req *service.SearchByIDRequest) (*service.SearchByIDResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SearchByIDResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) RebuildIndex(ctx context.Context, req *service.RebuildIndexRequest) (*service.RebuildIndexResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RebuildIndexResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetRebuildProgress(ctx context.Context, req *service.GetRebuildProgressRequest) (*service.GetRebuildProgressResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetRebuildProgressResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetIndexStats(ctx context.Context, req *service.GetIndexStatsRequest) (*service.GetIndexStatsResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetIndexStatsResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) CompactNamespace(ctx context.Context, req *service.CompactNamespaceRequest) (*service.CompactNamespaceResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.CompactNamespaceResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetCompactionProgress(ctx context.Context, req *service.GetCompactionProgressRequest) (*service.GetCompactionProgressResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetCompactionProgressResponse)
	return resp, args.Error(1)
}

// =============================================================================
// Dynamic Secrets Service Methods (returned by DynamicSecrets() accessor)
// =============================================================================

func (m *MockQuicBaaS) CreateRole(ctx context.Context, req *service.CreateRoleRequest) (*service.CreateRoleResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.CreateRoleResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetRole(ctx context.Context, req *service.GetRoleRequest) (*service.GetRoleResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetRoleResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ListRoles(ctx context.Context, req *service.ListRolesRequest) (*service.ListRolesResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ListRolesResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) DeleteRole(ctx context.Context, req *service.DeleteRoleRequest) (*service.DeleteRoleResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.DeleteRoleResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GenerateCredential(ctx context.Context, req *service.GenerateCredentialRequest) (*service.GenerateCredentialResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GenerateCredentialResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) RenewLease(ctx context.Context, req *service.RenewLeaseRequest) (*service.RenewLeaseResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RenewLeaseResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) RevokeLease(ctx context.Context, req *service.RevokeLeaseRequest) (*service.RevokeLeaseResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RevokeLeaseResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ListLeases(ctx context.Context, req *service.ListLeasesRequest) (*service.ListLeasesResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ListLeasesResponse)
	return resp, args.Error(1)
}

// =============================================================================
// Cluster DEK Service Methods (returned by ClusterDEK() accessor)
// =============================================================================

func (m *MockQuicBaaS) InitializeClusterDEK(ctx context.Context, req *service.InitClusterDEKRequest) (*service.InitClusterDEKResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.InitClusterDEKResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetClusterDEKStatus(ctx context.Context) (*service.ClusterDEKStatusResponse, error) {
	args := m.Called(ctx)
	resp, _ := args.Get(0).(*service.ClusterDEKStatusResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) RotateClusterDEK(ctx context.Context, req *service.RotateClusterDEKRequest) (*service.RotateClusterDEKResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RotateClusterDEKResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) SyncNodeDEK(ctx context.Context, req *service.SyncNodeDEKRequest) (*service.SyncNodeDEKResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SyncNodeDEKResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetNodeDEKStatus(ctx context.Context, req *service.GetNodeDEKStatusRequest) (*service.GetNodeDEKStatusResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetNodeDEKStatusResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetReencryptionStatus(ctx context.Context) (*service.ReencryptionStatusResponse, error) {
	args := m.Called(ctx)
	resp, _ := args.Get(0).(*service.ReencryptionStatusResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) CancelReencryption(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockQuicBaaS) RecoverDEK(ctx context.Context, req *service.RecoverDEKRequest) (*service.RecoverDEKResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RecoverDEKResponse)
	return resp, args.Error(1)
}

// =============================================================================
// Object Storage Service Methods (returned by ObjectStorageService() accessor)
// =============================================================================

func (m *MockQuicBaaS) PutObject(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.PutObjectResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetObject(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetObjectResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) DeleteObject(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.DeleteObjectResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) HeadObject(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.HeadObjectResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ListObjects(ctx context.Context, req *service.ListObjectsRequest) (*service.ListObjectsResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ListObjectsResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ListByTag(ctx context.Context, req *service.ListByTagRequest) (*service.ListByTagResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ListByTagResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ListByTenant(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ListByTenantResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) CopyObject(ctx context.Context, req *service.CopyObjectRequest) (*service.CopyObjectResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.CopyObjectResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ReplicateObject(ctx context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ReplicateObjectResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) MigrateObject(ctx context.Context, req *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.MigrateObjectResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ArchiveObject(ctx context.Context, req *service.ArchiveObjectRequest) (*service.ArchiveObjectResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ArchiveObjectResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) RestoreObject(ctx context.Context, req *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RestoreObjectResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) VerifyObject(ctx context.Context, req *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.VerifyObjectResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) SetObjectPolicy(ctx context.Context, req *service.SetObjectPolicyRequest) (*service.SetObjectPolicyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SetObjectPolicyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetObjectPolicy(ctx context.Context, req *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetObjectPolicyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ListBackends(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ListBackendsResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetBackendHealth(ctx context.Context, req *service.GetBackendHealthRequest) (*service.GetBackendHealthResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetBackendHealthResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetTenantUsage(ctx context.Context, req *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetTenantUsageResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetTenantQuotas(ctx context.Context, req *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetTenantQuotasResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) SetTenantQuotas(ctx context.Context, req *service.SetTenantQuotasRequest) (*service.SetTenantQuotasResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SetTenantQuotasResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) BatchPutObjects(ctx context.Context, req *service.BatchPutObjectsRequest) (*service.BatchPutObjectsResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.BatchPutObjectsResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) BatchGetObjects(ctx context.Context, req *service.BatchGetObjectsRequest) (*service.BatchGetObjectsResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.BatchGetObjectsResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) BatchDeleteObjects(ctx context.Context, req *service.BatchDeleteObjectsRequest) (*service.BatchDeleteObjectsResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.BatchDeleteObjectsResponse)
	return resp, args.Error(1)
}

// =============================================================================
// Object Storage Admin Service Methods
// =============================================================================

func (m *MockQuicBaaS) CreateBackend(ctx context.Context, req *service.CreateBackendRequest) (*service.CreateBackendResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.CreateBackendResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) UpdateBackend(ctx context.Context, req *service.UpdateBackendRequest) (*service.UpdateBackendResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.UpdateBackendResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) DeleteBackend(ctx context.Context, req *service.DeleteBackendRequest) (*service.DeleteBackendResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.DeleteBackendResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) SetBackendStatus(ctx context.Context, req *service.SetBackendStatusRequest) (*service.SetBackendStatusResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SetBackendStatusResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetStorageStats(ctx context.Context, req *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetStorageStatsResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) RunGarbageCollection(ctx context.Context, req *service.RunGarbageCollectionRequest) (*service.RunGarbageCollectionResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RunGarbageCollectionResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetGarbageCollectionProgress(ctx context.Context, req *service.GetGarbageCollectionProgressRequest) (*service.GetGarbageCollectionProgressResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetGarbageCollectionProgressResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) RepairObject(ctx context.Context, req *service.RepairObjectRequest) (*service.RepairObjectResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RepairObjectResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) CompactBackend(ctx context.Context, req *service.CompactBackendRequest) (*service.CompactBackendResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.CompactBackendResponse)
	return resp, args.Error(1)
}

// =============================================================================
// Parameter Store Service Methods
// =============================================================================

func (m *MockQuicBaaS) GetParameter(ctx context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*interfaces.GetParameterResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) SetParameter(ctx context.Context, req *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*interfaces.SetParameterResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) DeleteParameter(ctx context.Context, req *interfaces.DeleteParameterRequest) (*interfaces.DeleteParameterResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*interfaces.DeleteParameterResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetParameters(ctx context.Context, req *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*interfaces.GetParametersResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) SetParameters(ctx context.Context, req *interfaces.SetParametersRequest) (*interfaces.SetParametersResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*interfaces.SetParametersResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) DeleteParameters(ctx context.Context, req *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*interfaces.DeleteParametersResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ListParameters(ctx context.Context, req *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*interfaces.ListParametersResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) SearchByTags(ctx context.Context, req *interfaces.SearchByTagsRequest) (*interfaces.SearchByTagsResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*interfaces.SearchByTagsResponse)
	return resp, args.Error(1)
}

// =============================================================================
// Object Transfer Service Methods
// =============================================================================

func (m *MockQuicBaaS) FetchObject(ctx context.Context, req *service.FetchObjectRequest) (*service.FetchObjectResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.FetchObjectResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) StreamFetchObjects(ctx context.Context, req *service.StreamFetchObjectsRequest) (*service.StreamFetchObjectsResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.StreamFetchObjectsResponse)
	return resp, args.Error(1)
}

// =============================================================================
// Scheduler Service Methods (returned by Scheduler() accessor)
// =============================================================================

func (m *MockQuicBaaS) CreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.CreateSchedulerJobResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetSchedulerJobResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) UpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.UpdateSchedulerJobResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) DeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.DeleteSchedulerJobResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ListSchedulerJobsResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) TriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.TriggerSchedulerJobResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) PauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.PauseSchedulerJobResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ResumeSchedulerJobResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) CancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.CancelSchedulerJobResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetSchedulerJobHistoryResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	args := m.Called(ctx)
	resp, _ := args.Get(0).(*service.GetSchedulerStatusResponse)
	return resp, args.Error(1)
}
