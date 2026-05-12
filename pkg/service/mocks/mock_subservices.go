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

package mocks

import (
	"context"
	"time"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// MockKMSService
// =============================================================================

// MockKMSService is a mock implementation of service.KMSService.
type MockKMSService struct {
	GetSupportedAlgorithmsFunc  func(ctx context.Context) ([]string, error)
	GenerateKeyFunc             func(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error)
	ImportKeyFunc               func(ctx context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error)
	GetKeyFunc                  func(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error)
	GetPublicKeyFunc            func(ctx context.Context, req *service.GetPublicKeyRequest) (*service.GetPublicKeyResponse, error)
	ListKeysFunc                func(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error)
	RotateKeyFunc               func(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error)
	RevokeKeyFunc               func(ctx context.Context, req *service.RevokeKeyRequest) (*service.RevokeKeyResponse, error)
	DeleteKeyFunc               func(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error)
	GetKeyMetadataFunc          func(ctx context.Context, req *service.GetKeyMetadataRequest) (*service.GetKeyMetadataResponse, error)
	UpdateKeyMetadataFunc       func(ctx context.Context, req *service.UpdateKeyMetadataRequest) (*service.UpdateKeyMetadataResponse, error)
	UpdateKeyStatusFunc         func(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error)
	ListKeyVersionsFunc         func(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error)
	SetKeyPolicyFunc            func(ctx context.Context, req *service.SetKeyPolicyRequest) (*service.SetKeyPolicyResponse, error)
	GetKeyPolicyFunc            func(ctx context.Context, req *service.GetKeyPolicyRequest) (*service.GetKeyPolicyResponse, error)
	CreateDEKFunc               func(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error)
	GetDEKFunc                  func(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error)
	ListDEKsFunc                func(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error)
	RotateDEKFunc               func(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error)
	DeleteDEKFunc               func(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error)
	GetDEKStatusFunc            func(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error)
	SetDefaultDEKFunc           func(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error)
	SetDEKSafetyFunc            func(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error)
	ClearDEKSafetyFunc          func(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error)
	StartReencryptionFunc       func(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error)
	GetReencryptionProgressFunc func(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error)
	BackendStatusFunc           func(ctx context.Context) (*service.BackendStatusResponse, error)
	BackendTestFunc             func(ctx context.Context) (*service.BackendTestResponse, error)
	ExportKeyFunc               func(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error)
	UnwrapKeyFunc               func(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error)
	SetKeySafetyFunc            func(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error)
	ClearKeySafetyFunc          func(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error)
	SafetyCheckFunc             func(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error)
	SafetyReportFunc            func(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error)
	SetDefaultKeyFunc           func(ctx context.Context, req *service.SetDefaultKeyRequest) (*service.SetDefaultKeyResponse, error)
}

var _ service.KMSService = (*MockKMSService)(nil)

func (m *MockKMSService) GetSupportedAlgorithms(ctx context.Context) ([]string, error) {
	if m.GetSupportedAlgorithmsFunc != nil {
		return m.GetSupportedAlgorithmsFunc(ctx)
	}
	return []string{"AES-256-GCM"}, nil
}

func (m *MockKMSService) GenerateKey(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
	if m.GenerateKeyFunc != nil {
		return m.GenerateKeyFunc(ctx, req)
	}
	return &service.GenerateKeyResponse{}, nil
}

func (m *MockKMSService) ImportKey(ctx context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
	if m.ImportKeyFunc != nil {
		return m.ImportKeyFunc(ctx, req)
	}
	return &service.ImportKeyResponse{}, nil
}

func (m *MockKMSService) GetKey(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
	if m.GetKeyFunc != nil {
		return m.GetKeyFunc(ctx, req)
	}
	return &service.GetKeyResponse{}, nil
}

func (m *MockKMSService) GetPublicKey(ctx context.Context, req *service.GetPublicKeyRequest) (*service.GetPublicKeyResponse, error) {
	if m.GetPublicKeyFunc != nil {
		return m.GetPublicKeyFunc(ctx, req)
	}
	return &service.GetPublicKeyResponse{}, nil
}

func (m *MockKMSService) ListKeys(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error) {
	if m.ListKeysFunc != nil {
		return m.ListKeysFunc(ctx, req)
	}
	return &service.ListKeysResponse{}, nil
}

func (m *MockKMSService) RotateKey(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
	if m.RotateKeyFunc != nil {
		return m.RotateKeyFunc(ctx, req)
	}
	return &service.RotateKeyResponse{}, nil
}

func (m *MockKMSService) RevokeKey(ctx context.Context, req *service.RevokeKeyRequest) (*service.RevokeKeyResponse, error) {
	if m.RevokeKeyFunc != nil {
		return m.RevokeKeyFunc(ctx, req)
	}
	return &service.RevokeKeyResponse{}, nil
}

func (m *MockKMSService) DeleteKey(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
	if m.DeleteKeyFunc != nil {
		return m.DeleteKeyFunc(ctx, req)
	}
	return &service.DeleteKeyResponse{}, nil
}

func (m *MockKMSService) GetKeyMetadata(ctx context.Context, req *service.GetKeyMetadataRequest) (*service.GetKeyMetadataResponse, error) {
	if m.GetKeyMetadataFunc != nil {
		return m.GetKeyMetadataFunc(ctx, req)
	}
	return &service.GetKeyMetadataResponse{}, nil
}

func (m *MockKMSService) UpdateKeyMetadata(ctx context.Context, req *service.UpdateKeyMetadataRequest) (*service.UpdateKeyMetadataResponse, error) {
	if m.UpdateKeyMetadataFunc != nil {
		return m.UpdateKeyMetadataFunc(ctx, req)
	}
	return &service.UpdateKeyMetadataResponse{}, nil
}

func (m *MockKMSService) UpdateKeyStatus(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
	if m.UpdateKeyStatusFunc != nil {
		return m.UpdateKeyStatusFunc(ctx, req)
	}
	return &service.UpdateKeyStatusResponse{}, nil
}

func (m *MockKMSService) ListKeyVersions(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
	if m.ListKeyVersionsFunc != nil {
		return m.ListKeyVersionsFunc(ctx, req)
	}
	return &service.ListKeyVersionsResponse{}, nil
}

func (m *MockKMSService) SetKeyPolicy(ctx context.Context, req *service.SetKeyPolicyRequest) (*service.SetKeyPolicyResponse, error) {
	if m.SetKeyPolicyFunc != nil {
		return m.SetKeyPolicyFunc(ctx, req)
	}
	return &service.SetKeyPolicyResponse{}, nil
}

func (m *MockKMSService) GetKeyPolicy(ctx context.Context, req *service.GetKeyPolicyRequest) (*service.GetKeyPolicyResponse, error) {
	if m.GetKeyPolicyFunc != nil {
		return m.GetKeyPolicyFunc(ctx, req)
	}
	return &service.GetKeyPolicyResponse{}, nil
}

func (m *MockKMSService) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	if m.CreateDEKFunc != nil {
		return m.CreateDEKFunc(ctx, req)
	}
	return &service.CreateDEKResponse{}, nil
}

func (m *MockKMSService) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	if m.GetDEKFunc != nil {
		return m.GetDEKFunc(ctx, req)
	}
	return &service.GetDEKResponse{}, nil
}

func (m *MockKMSService) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	if m.ListDEKsFunc != nil {
		return m.ListDEKsFunc(ctx, req)
	}
	return &service.ListDEKsResponse{}, nil
}

func (m *MockKMSService) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	if m.RotateDEKFunc != nil {
		return m.RotateDEKFunc(ctx, req)
	}
	return &service.RotateDEKResponse{}, nil
}

func (m *MockKMSService) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	if m.DeleteDEKFunc != nil {
		return m.DeleteDEKFunc(ctx, req)
	}
	return &service.DeleteDEKResponse{}, nil
}

func (m *MockKMSService) GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	if m.GetDEKStatusFunc != nil {
		return m.GetDEKStatusFunc(ctx, req)
	}
	return &service.GetDEKStatusResponse{}, nil
}

func (m *MockKMSService) SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	if m.SetDefaultDEKFunc != nil {
		return m.SetDefaultDEKFunc(ctx, req)
	}
	return &service.SetDefaultDEKResponse{}, nil
}

func (m *MockKMSService) SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	if m.SetDEKSafetyFunc != nil {
		return m.SetDEKSafetyFunc(ctx, req)
	}
	return &service.SetDEKSafetyResponse{}, nil
}

func (m *MockKMSService) ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	if m.ClearDEKSafetyFunc != nil {
		return m.ClearDEKSafetyFunc(ctx, req)
	}
	return &service.ClearDEKSafetyResponse{}, nil
}

func (m *MockKMSService) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	if m.StartReencryptionFunc != nil {
		return m.StartReencryptionFunc(ctx, req)
	}
	return &service.StartReencryptionResponse{}, nil
}

func (m *MockKMSService) GetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	if m.GetReencryptionProgressFunc != nil {
		return m.GetReencryptionProgressFunc(ctx, req)
	}
	return &service.GetReencryptionProgressResponse{}, nil
}

func (m *MockKMSService) BackendStatus(ctx context.Context) (*service.BackendStatusResponse, error) {
	if m.BackendStatusFunc != nil {
		return m.BackendStatusFunc(ctx)
	}
	return &service.BackendStatusResponse{}, nil
}

func (m *MockKMSService) BackendTest(ctx context.Context) (*service.BackendTestResponse, error) {
	if m.BackendTestFunc != nil {
		return m.BackendTestFunc(ctx)
	}
	return &service.BackendTestResponse{}, nil
}

func (m *MockKMSService) ExportKey(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
	if m.ExportKeyFunc != nil {
		return m.ExportKeyFunc(ctx, req)
	}
	return &service.ExportKeyResponse{}, nil
}

func (m *MockKMSService) UnwrapKey(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
	if m.UnwrapKeyFunc != nil {
		return m.UnwrapKeyFunc(ctx, req)
	}
	return &service.UnwrapKeyResponse{}, nil
}

func (m *MockKMSService) SetKeySafety(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
	if m.SetKeySafetyFunc != nil {
		return m.SetKeySafetyFunc(ctx, req)
	}
	return &service.SetKeySafetyResponse{}, nil
}

func (m *MockKMSService) ClearKeySafety(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
	if m.ClearKeySafetyFunc != nil {
		return m.ClearKeySafetyFunc(ctx, req)
	}
	return &service.ClearKeySafetyResponse{}, nil
}

func (m *MockKMSService) SafetyCheck(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
	if m.SafetyCheckFunc != nil {
		return m.SafetyCheckFunc(ctx, req)
	}
	return &service.SafetyCheckResponse{}, nil
}

func (m *MockKMSService) SafetyReport(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
	if m.SafetyReportFunc != nil {
		return m.SafetyReportFunc(ctx, req)
	}
	return &service.SafetyReportResponse{}, nil
}

func (m *MockKMSService) SetDefaultKey(ctx context.Context, req *service.SetDefaultKeyRequest) (*service.SetDefaultKeyResponse, error) {
	if m.SetDefaultKeyFunc != nil {
		return m.SetDefaultKeyFunc(ctx, req)
	}
	return &service.SetDefaultKeyResponse{}, nil
}

// =============================================================================
// MockSecretsService
// =============================================================================

// MockSecretsService is a mock implementation of service.SecretsService.
type MockSecretsService struct {
	StoreSecretFunc          func(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error)
	GetSecretFunc            func(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error)
	ListSecretsFunc          func(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error)
	DeleteSecretFunc         func(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error)
	GetSecretVersionFunc     func(ctx context.Context, req *service.GetSecretVersionRequest) (*service.GetSecretVersionResponse, error)
	ListSecretVersionsFunc   func(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error)
	RollbackSecretFunc       func(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error)
	RotateSecretFunc         func(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error)
	SetRotationPolicyFunc    func(ctx context.Context, req *service.SetRotationPolicyRequest) (*service.SetRotationPolicyResponse, error)
	GetRotationPolicyFunc    func(ctx context.Context, req *service.GetRotationPolicyRequest) (*service.GetRotationPolicyResponse, error)
	UpdateSecretStatusFunc   func(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error)
	GetSecretsByPrefixFunc   func(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error)
	WatchSecretsByPrefixFunc func(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error)
	EnsureSecretFunc         func(ctx context.Context, req *service.EnsureSecretRequest) (*service.EnsureSecretResponse, error)
}

var _ service.SecretsService = (*MockSecretsService)(nil)

func (m *MockSecretsService) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	if m.StoreSecretFunc != nil {
		return m.StoreSecretFunc(ctx, req)
	}
	return &service.StoreSecretResponse{}, nil
}

func (m *MockSecretsService) EnsureSecret(ctx context.Context, req *service.EnsureSecretRequest) (*service.EnsureSecretResponse, error) {
	if m.EnsureSecretFunc != nil {
		return m.EnsureSecretFunc(ctx, req)
	}
	return &service.EnsureSecretResponse{}, nil
}

func (m *MockSecretsService) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	if m.GetSecretFunc != nil {
		return m.GetSecretFunc(ctx, req)
	}
	return &service.GetSecretResponse{}, nil
}

func (m *MockSecretsService) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	if m.ListSecretsFunc != nil {
		return m.ListSecretsFunc(ctx, req)
	}
	return &service.ListSecretsResponse{}, nil
}

func (m *MockSecretsService) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	if m.DeleteSecretFunc != nil {
		return m.DeleteSecretFunc(ctx, req)
	}
	return &service.DeleteSecretResponse{}, nil
}

func (m *MockSecretsService) GetSecretVersion(ctx context.Context, req *service.GetSecretVersionRequest) (*service.GetSecretVersionResponse, error) {
	if m.GetSecretVersionFunc != nil {
		return m.GetSecretVersionFunc(ctx, req)
	}
	return &service.GetSecretVersionResponse{}, nil
}

func (m *MockSecretsService) ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	if m.ListSecretVersionsFunc != nil {
		return m.ListSecretVersionsFunc(ctx, req)
	}
	return &service.ListSecretVersionsResponse{}, nil
}

func (m *MockSecretsService) RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	if m.RollbackSecretFunc != nil {
		return m.RollbackSecretFunc(ctx, req)
	}
	return &service.RollbackSecretResponse{}, nil
}

func (m *MockSecretsService) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	if m.RotateSecretFunc != nil {
		return m.RotateSecretFunc(ctx, req)
	}
	return &service.RotateSecretResponse{}, nil
}

func (m *MockSecretsService) SetRotationPolicy(ctx context.Context, req *service.SetRotationPolicyRequest) (*service.SetRotationPolicyResponse, error) {
	if m.SetRotationPolicyFunc != nil {
		return m.SetRotationPolicyFunc(ctx, req)
	}
	return &service.SetRotationPolicyResponse{}, nil
}

func (m *MockSecretsService) GetRotationPolicy(ctx context.Context, req *service.GetRotationPolicyRequest) (*service.GetRotationPolicyResponse, error) {
	if m.GetRotationPolicyFunc != nil {
		return m.GetRotationPolicyFunc(ctx, req)
	}
	return &service.GetRotationPolicyResponse{}, nil
}

func (m *MockSecretsService) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	if m.UpdateSecretStatusFunc != nil {
		return m.UpdateSecretStatusFunc(ctx, req)
	}
	return &service.UpdateSecretStatusResponse{}, nil
}

func (m *MockSecretsService) GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	if m.GetSecretsByPrefixFunc != nil {
		return m.GetSecretsByPrefixFunc(ctx, req)
	}
	return &service.GetSecretsByPrefixResponse{}, nil
}

func (m *MockSecretsService) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	if m.WatchSecretsByPrefixFunc != nil {
		return m.WatchSecretsByPrefixFunc(ctx, req)
	}
	ch := make(chan *service.SecretChange)
	close(ch)
	return ch, nil
}

// =============================================================================
// MockCryptoService
// =============================================================================

// MockCryptoService is a mock implementation of service.CryptoService.
type MockCryptoService struct {
	EncryptFunc                func(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error)
	DecryptFunc                func(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error)
	RewrapFunc                 func(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error)
	EncryptBatchFunc           func(ctx context.Context, req *service.EncryptBatchRequest) (*service.EncryptBatchResponse, error)
	DecryptBatchFunc           func(ctx context.Context, req *service.DecryptBatchRequest) (*service.DecryptBatchResponse, error)
	GenerateDataKeyFunc        func(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error)
	GenerateWrappedDataKeyFunc func(ctx context.Context, req *service.GenerateWrappedDataKeyRequest) (*service.GenerateWrappedDataKeyResponse, error)
	SignFunc                   func(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error)
	VerifyFunc                 func(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error)
	GenerateHMACFunc           func(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error)
	VerifyHMACFunc             func(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error)
	GenerateRandomFunc         func(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error)
	HashFunc                   func(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error)
}

var _ service.CryptoService = (*MockCryptoService)(nil)

func (m *MockCryptoService) Encrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	if m.EncryptFunc != nil {
		return m.EncryptFunc(ctx, req)
	}
	return &service.EncryptResponse{}, nil
}

func (m *MockCryptoService) Decrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	if m.DecryptFunc != nil {
		return m.DecryptFunc(ctx, req)
	}
	return &service.DecryptResponse{}, nil
}

func (m *MockCryptoService) Rewrap(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error) {
	if m.RewrapFunc != nil {
		return m.RewrapFunc(ctx, req)
	}
	return &service.RewrapResponse{}, nil
}

func (m *MockCryptoService) EncryptBatch(ctx context.Context, req *service.EncryptBatchRequest) (*service.EncryptBatchResponse, error) {
	if m.EncryptBatchFunc != nil {
		return m.EncryptBatchFunc(ctx, req)
	}
	return &service.EncryptBatchResponse{}, nil
}

func (m *MockCryptoService) DecryptBatch(ctx context.Context, req *service.DecryptBatchRequest) (*service.DecryptBatchResponse, error) {
	if m.DecryptBatchFunc != nil {
		return m.DecryptBatchFunc(ctx, req)
	}
	return &service.DecryptBatchResponse{}, nil
}

func (m *MockCryptoService) GenerateDataKey(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
	if m.GenerateDataKeyFunc != nil {
		return m.GenerateDataKeyFunc(ctx, req)
	}
	return &service.GenerateDataKeyResponse{}, nil
}

func (m *MockCryptoService) GenerateWrappedDataKey(ctx context.Context, req *service.GenerateWrappedDataKeyRequest) (*service.GenerateWrappedDataKeyResponse, error) {
	if m.GenerateWrappedDataKeyFunc != nil {
		return m.GenerateWrappedDataKeyFunc(ctx, req)
	}
	return &service.GenerateWrappedDataKeyResponse{}, nil
}

func (m *MockCryptoService) Sign(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error) {
	if m.SignFunc != nil {
		return m.SignFunc(ctx, req)
	}
	return &service.SignResponse{}, nil
}

func (m *MockCryptoService) Verify(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error) {
	if m.VerifyFunc != nil {
		return m.VerifyFunc(ctx, req)
	}
	return &service.VerifyResponse{}, nil
}

func (m *MockCryptoService) GenerateHMAC(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error) {
	if m.GenerateHMACFunc != nil {
		return m.GenerateHMACFunc(ctx, req)
	}
	return &service.HMACResponse{}, nil
}

func (m *MockCryptoService) VerifyHMAC(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error) {
	if m.VerifyHMACFunc != nil {
		return m.VerifyHMACFunc(ctx, req)
	}
	return &service.VerifyHMACResponse{}, nil
}

func (m *MockCryptoService) GenerateRandom(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
	if m.GenerateRandomFunc != nil {
		return m.GenerateRandomFunc(ctx, req)
	}
	return &service.GenerateRandomResponse{}, nil
}

func (m *MockCryptoService) Hash(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error) {
	if m.HashFunc != nil {
		return m.HashFunc(ctx, req)
	}
	return &service.HashResponse{}, nil
}

// =============================================================================
// MockVectorService
// =============================================================================

// MockVectorService is a mock implementation of service.VectorService.
type MockVectorService struct {
	CreateNamespaceFunc       func(ctx context.Context, req *service.CreateNamespaceRequest) (*service.CreateNamespaceResponse, error)
	DeleteNamespaceFunc       func(ctx context.Context, req *service.DeleteNamespaceRequest) (*service.DeleteNamespaceResponse, error)
	ListNamespacesFunc        func(ctx context.Context, req *service.ListNamespacesRequest) (*service.ListNamespacesResponse, error)
	GetNamespaceInfoFunc      func(ctx context.Context, req *service.GetNamespaceInfoRequest) (*service.GetNamespaceInfoResponse, error)
	UpsertVectorFunc          func(ctx context.Context, req *service.UpsertVectorRequest) (*service.UpsertVectorResponse, error)
	GetVectorFunc             func(ctx context.Context, req *service.GetVectorRequest) (*service.GetVectorResponse, error)
	DeleteVectorFunc          func(ctx context.Context, req *service.DeleteVectorRequest) (*service.DeleteVectorResponse, error)
	BatchUpsertFunc           func(ctx context.Context, req *service.BatchUpsertRequest) (*service.BatchUpsertResponse, error)
	BatchDeleteVectorsFunc    func(ctx context.Context, req *service.BatchDeleteVectorsRequest) (*service.BatchDeleteVectorsResponse, error)
	ListVectorsFunc           func(ctx context.Context, req *service.ListVectorsRequest) (*service.ListVectorsResponse, error)
	UpdateMetadataFunc        func(ctx context.Context, req *service.UpdateVectorMetadataRequest) (*service.UpdateVectorMetadataResponse, error)
	SearchSimilarFunc         func(ctx context.Context, req *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error)
	SearchByMetadataFunc      func(ctx context.Context, req *service.SearchByMetadataRequest) (*service.SearchByMetadataResponse, error)
	HybridSearchFunc          func(ctx context.Context, req *service.HybridSearchRequest) (*service.HybridSearchResponse, error)
	SearchByIDFunc            func(ctx context.Context, req *service.SearchByIDRequest) (*service.SearchByIDResponse, error)
	RebuildIndexFunc          func(ctx context.Context, req *service.RebuildIndexRequest) (*service.RebuildIndexResponse, error)
	GetRebuildProgressFunc    func(ctx context.Context, req *service.GetRebuildProgressRequest) (*service.GetRebuildProgressResponse, error)
	GetIndexStatsFunc         func(ctx context.Context, req *service.GetIndexStatsRequest) (*service.GetIndexStatsResponse, error)
	CompactNamespaceFunc      func(ctx context.Context, req *service.CompactNamespaceRequest) (*service.CompactNamespaceResponse, error)
	GetCompactionProgressFunc func(ctx context.Context, req *service.GetCompactionProgressRequest) (*service.GetCompactionProgressResponse, error)
}

var _ service.VectorService = (*MockVectorService)(nil)

func (m *MockVectorService) CreateNamespace(ctx context.Context, req *service.CreateNamespaceRequest) (*service.CreateNamespaceResponse, error) {
	if m.CreateNamespaceFunc != nil {
		return m.CreateNamespaceFunc(ctx, req)
	}
	return &service.CreateNamespaceResponse{Success: true}, nil
}

func (m *MockVectorService) DeleteNamespace(ctx context.Context, req *service.DeleteNamespaceRequest) (*service.DeleteNamespaceResponse, error) {
	if m.DeleteNamespaceFunc != nil {
		return m.DeleteNamespaceFunc(ctx, req)
	}
	return &service.DeleteNamespaceResponse{Success: true}, nil
}

func (m *MockVectorService) ListNamespaces(ctx context.Context, req *service.ListNamespacesRequest) (*service.ListNamespacesResponse, error) {
	if m.ListNamespacesFunc != nil {
		return m.ListNamespacesFunc(ctx, req)
	}
	return &service.ListNamespacesResponse{}, nil
}

func (m *MockVectorService) GetNamespaceInfo(ctx context.Context, req *service.GetNamespaceInfoRequest) (*service.GetNamespaceInfoResponse, error) {
	if m.GetNamespaceInfoFunc != nil {
		return m.GetNamespaceInfoFunc(ctx, req)
	}
	return &service.GetNamespaceInfoResponse{}, nil
}

func (m *MockVectorService) UpsertVector(ctx context.Context, req *service.UpsertVectorRequest) (*service.UpsertVectorResponse, error) {
	if m.UpsertVectorFunc != nil {
		return m.UpsertVectorFunc(ctx, req)
	}
	return &service.UpsertVectorResponse{Success: true}, nil
}

func (m *MockVectorService) GetVector(ctx context.Context, req *service.GetVectorRequest) (*service.GetVectorResponse, error) {
	if m.GetVectorFunc != nil {
		return m.GetVectorFunc(ctx, req)
	}
	return &service.GetVectorResponse{}, nil
}

func (m *MockVectorService) DeleteVector(ctx context.Context, req *service.DeleteVectorRequest) (*service.DeleteVectorResponse, error) {
	if m.DeleteVectorFunc != nil {
		return m.DeleteVectorFunc(ctx, req)
	}
	return &service.DeleteVectorResponse{Success: true}, nil
}

func (m *MockVectorService) BatchUpsert(ctx context.Context, req *service.BatchUpsertRequest) (*service.BatchUpsertResponse, error) {
	if m.BatchUpsertFunc != nil {
		return m.BatchUpsertFunc(ctx, req)
	}
	return &service.BatchUpsertResponse{Success: true}, nil
}

func (m *MockVectorService) BatchDeleteVectors(ctx context.Context, req *service.BatchDeleteVectorsRequest) (*service.BatchDeleteVectorsResponse, error) {
	if m.BatchDeleteVectorsFunc != nil {
		return m.BatchDeleteVectorsFunc(ctx, req)
	}
	return &service.BatchDeleteVectorsResponse{Success: true}, nil
}

func (m *MockVectorService) ListVectors(ctx context.Context, req *service.ListVectorsRequest) (*service.ListVectorsResponse, error) {
	if m.ListVectorsFunc != nil {
		return m.ListVectorsFunc(ctx, req)
	}
	return &service.ListVectorsResponse{}, nil
}

func (m *MockVectorService) UpdateMetadata(ctx context.Context, req *service.UpdateVectorMetadataRequest) (*service.UpdateVectorMetadataResponse, error) {
	if m.UpdateMetadataFunc != nil {
		return m.UpdateMetadataFunc(ctx, req)
	}
	return &service.UpdateVectorMetadataResponse{Success: true}, nil
}

func (m *MockVectorService) SearchSimilar(ctx context.Context, req *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error) {
	if m.SearchSimilarFunc != nil {
		return m.SearchSimilarFunc(ctx, req)
	}
	return &service.SearchSimilarResponse{}, nil
}

func (m *MockVectorService) SearchByMetadata(ctx context.Context, req *service.SearchByMetadataRequest) (*service.SearchByMetadataResponse, error) {
	if m.SearchByMetadataFunc != nil {
		return m.SearchByMetadataFunc(ctx, req)
	}
	return &service.SearchByMetadataResponse{}, nil
}

func (m *MockVectorService) HybridSearch(ctx context.Context, req *service.HybridSearchRequest) (*service.HybridSearchResponse, error) {
	if m.HybridSearchFunc != nil {
		return m.HybridSearchFunc(ctx, req)
	}
	return &service.HybridSearchResponse{}, nil
}

func (m *MockVectorService) SearchByID(ctx context.Context, req *service.SearchByIDRequest) (*service.SearchByIDResponse, error) {
	if m.SearchByIDFunc != nil {
		return m.SearchByIDFunc(ctx, req)
	}
	return &service.SearchByIDResponse{}, nil
}

func (m *MockVectorService) RebuildIndex(ctx context.Context, req *service.RebuildIndexRequest) (*service.RebuildIndexResponse, error) {
	if m.RebuildIndexFunc != nil {
		return m.RebuildIndexFunc(ctx, req)
	}
	return &service.RebuildIndexResponse{}, nil
}

func (m *MockVectorService) GetRebuildProgress(ctx context.Context, req *service.GetRebuildProgressRequest) (*service.GetRebuildProgressResponse, error) {
	if m.GetRebuildProgressFunc != nil {
		return m.GetRebuildProgressFunc(ctx, req)
	}
	return &service.GetRebuildProgressResponse{}, nil
}

func (m *MockVectorService) GetIndexStats(ctx context.Context, req *service.GetIndexStatsRequest) (*service.GetIndexStatsResponse, error) {
	if m.GetIndexStatsFunc != nil {
		return m.GetIndexStatsFunc(ctx, req)
	}
	return &service.GetIndexStatsResponse{}, nil
}

func (m *MockVectorService) CompactNamespace(ctx context.Context, req *service.CompactNamespaceRequest) (*service.CompactNamespaceResponse, error) {
	if m.CompactNamespaceFunc != nil {
		return m.CompactNamespaceFunc(ctx, req)
	}
	return &service.CompactNamespaceResponse{}, nil
}

func (m *MockVectorService) GetCompactionProgress(ctx context.Context, req *service.GetCompactionProgressRequest) (*service.GetCompactionProgressResponse, error) {
	if m.GetCompactionProgressFunc != nil {
		return m.GetCompactionProgressFunc(ctx, req)
	}
	return &service.GetCompactionProgressResponse{}, nil
}

// =============================================================================
// MockSchedulerService
// =============================================================================

// MockSchedulerService is a mock implementation of service.SchedulerService.
type MockSchedulerService struct {
	CreateJobFunc     func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error)
	GetJobFunc        func(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error)
	UpdateJobFunc     func(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error)
	DeleteJobFunc     func(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error)
	ListJobsFunc      func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error)
	TriggerJobFunc    func(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error)
	PauseJobFunc      func(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error)
	ResumeJobFunc     func(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error)
	CancelJobFunc     func(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error)
	GetJobHistoryFunc func(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error)
	GetStatusFunc     func(ctx context.Context) (*service.GetSchedulerStatusResponse, error)
}

var _ service.SchedulerService = (*MockSchedulerService)(nil)

func (m *MockSchedulerService) CreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	if m.CreateJobFunc != nil {
		return m.CreateJobFunc(ctx, req)
	}
	return &service.CreateSchedulerJobResponse{Timestamp: time.Now()}, nil
}

func (m *MockSchedulerService) GetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	if m.GetJobFunc != nil {
		return m.GetJobFunc(ctx, req)
	}
	return &service.GetSchedulerJobResponse{Timestamp: time.Now()}, nil
}

func (m *MockSchedulerService) UpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	if m.UpdateJobFunc != nil {
		return m.UpdateJobFunc(ctx, req)
	}
	return &service.UpdateSchedulerJobResponse{Timestamp: time.Now()}, nil
}

func (m *MockSchedulerService) DeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	if m.DeleteJobFunc != nil {
		return m.DeleteJobFunc(ctx, req)
	}
	return &service.DeleteSchedulerJobResponse{Deleted: true, Timestamp: time.Now()}, nil
}

func (m *MockSchedulerService) ListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	if m.ListJobsFunc != nil {
		return m.ListJobsFunc(ctx, req)
	}
	return &service.ListSchedulerJobsResponse{Timestamp: time.Now()}, nil
}

func (m *MockSchedulerService) TriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	if m.TriggerJobFunc != nil {
		return m.TriggerJobFunc(ctx, req)
	}
	return &service.TriggerSchedulerJobResponse{Timestamp: time.Now()}, nil
}

func (m *MockSchedulerService) PauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	if m.PauseJobFunc != nil {
		return m.PauseJobFunc(ctx, req)
	}
	return &service.PauseSchedulerJobResponse{Timestamp: time.Now()}, nil
}

func (m *MockSchedulerService) ResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	if m.ResumeJobFunc != nil {
		return m.ResumeJobFunc(ctx, req)
	}
	return &service.ResumeSchedulerJobResponse{Timestamp: time.Now()}, nil
}

func (m *MockSchedulerService) CancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	if m.CancelJobFunc != nil {
		return m.CancelJobFunc(ctx, req)
	}
	return &service.CancelSchedulerJobResponse{Timestamp: time.Now()}, nil
}

func (m *MockSchedulerService) GetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	if m.GetJobHistoryFunc != nil {
		return m.GetJobHistoryFunc(ctx, req)
	}
	return &service.GetSchedulerJobHistoryResponse{Timestamp: time.Now()}, nil
}

func (m *MockSchedulerService) GetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	if m.GetStatusFunc != nil {
		return m.GetStatusFunc(ctx)
	}
	return &service.GetSchedulerStatusResponse{Running: true, Timestamp: time.Now()}, nil
}

// =============================================================================
// MockMessagingService
// =============================================================================

// MockMessagingService is a mock implementation of messaging.Service.
type MockMessagingService struct {
	CreateChannelFunc func(ctx context.Context, name string, opts ...messaging.CreateChannelOption) (*messaging.Channel, error)
	DeleteChannelFunc func(ctx context.Context, name string) error
	GetChannelFunc    func(ctx context.Context, name string) (*messaging.Channel, error)
	ListChannelsFunc  func(ctx context.Context) ([]*messaging.Channel, error)
	SubscribeFunc     func(ctx context.Context, channel string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error)
	UnsubscribeFunc   func(ctx context.Context, subscriptionID string) error
	PublishFunc       func(ctx context.Context, channel string, payload interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error)
	PublishDeltaFunc  func(ctx context.Context, channel string, old, new interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error)
	WatchChannelFunc  func(ctx context.Context, keyPrefix string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error)
	StartFunc         func(ctx context.Context) error
	StopFunc          func(ctx context.Context) error
	IsRunningFunc     func() bool
	GetStatsFunc      func() *messaging.Stats
}

var _ messaging.Service = (*MockMessagingService)(nil)

func (m *MockMessagingService) CreateChannel(ctx context.Context, name string, opts ...messaging.CreateChannelOption) (*messaging.Channel, error) {
	if m.CreateChannelFunc != nil {
		return m.CreateChannelFunc(ctx, name, opts...)
	}
	return &messaging.Channel{Name: name, CreatedAt: time.Now()}, nil
}

func (m *MockMessagingService) DeleteChannel(ctx context.Context, name string) error {
	if m.DeleteChannelFunc != nil {
		return m.DeleteChannelFunc(ctx, name)
	}
	return nil
}

func (m *MockMessagingService) GetChannel(ctx context.Context, name string) (*messaging.Channel, error) {
	if m.GetChannelFunc != nil {
		return m.GetChannelFunc(ctx, name)
	}
	return &messaging.Channel{Name: name, CreatedAt: time.Now()}, nil
}

func (m *MockMessagingService) ListChannels(ctx context.Context) ([]*messaging.Channel, error) {
	if m.ListChannelsFunc != nil {
		return m.ListChannelsFunc(ctx)
	}
	return []*messaging.Channel{}, nil
}

func (m *MockMessagingService) Subscribe(ctx context.Context, channel string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
	if m.SubscribeFunc != nil {
		return m.SubscribeFunc(ctx, channel, opts...)
	}
	return &messaging.Subscription{ID: "mock-sub-1", Channel: channel, CreatedAt: time.Now()}, nil
}

func (m *MockMessagingService) Unsubscribe(ctx context.Context, subscriptionID string) error {
	if m.UnsubscribeFunc != nil {
		return m.UnsubscribeFunc(ctx, subscriptionID)
	}
	return nil
}

func (m *MockMessagingService) Publish(ctx context.Context, channel string, payload interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
	if m.PublishFunc != nil {
		return m.PublishFunc(ctx, channel, payload, opts...)
	}
	return &messaging.PublishResult{MessageID: "mock-id"}, nil
}

func (m *MockMessagingService) PublishDelta(ctx context.Context, channel string, old, new interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
	if m.PublishDeltaFunc != nil {
		return m.PublishDeltaFunc(ctx, channel, old, new, opts...)
	}
	return &messaging.PublishResult{MessageID: "mock-id"}, nil
}

func (m *MockMessagingService) WatchChannel(ctx context.Context, keyPrefix string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
	if m.WatchChannelFunc != nil {
		return m.WatchChannelFunc(ctx, keyPrefix, opts...)
	}
	return &messaging.Subscription{ID: "mock-watch-1", Channel: "watch:" + keyPrefix, CreatedAt: time.Now()}, nil
}

func (m *MockMessagingService) Start(ctx context.Context) error {
	if m.StartFunc != nil {
		return m.StartFunc(ctx)
	}
	return nil
}

func (m *MockMessagingService) Stop(ctx context.Context) error {
	if m.StopFunc != nil {
		return m.StopFunc(ctx)
	}
	return nil
}

func (m *MockMessagingService) IsRunning() bool {
	if m.IsRunningFunc != nil {
		return m.IsRunningFunc()
	}
	return true
}

func (m *MockMessagingService) GetStats() *messaging.Stats {
	if m.GetStatsFunc != nil {
		return m.GetStatsFunc()
	}
	return &messaging.Stats{}
}

// =============================================================================
// MockEmbeddingService
// =============================================================================

// MockEmbeddingService is a mock implementation of service.EmbeddingService.
type MockEmbeddingService struct {
	GenerateEmbeddingFunc       func(ctx context.Context, req *service.GenerateEmbeddingRequest) (*service.GenerateEmbeddingResponse, error)
	BatchGenerateEmbeddingsFunc func(ctx context.Context, req *service.BatchGenerateEmbeddingsRequest) (*service.BatchGenerateEmbeddingsResponse, error)
	ListProvidersFunc           func(ctx context.Context, req *service.ListProvidersRequest) (*service.ListProvidersResponse, error)
	GetProviderInfoFunc         func(ctx context.Context, req *service.GetProviderInfoRequest) (*service.GetProviderInfoResponse, error)
	SetDefaultProviderFunc      func(ctx context.Context, req *service.SetDefaultProviderRequest) (*service.SetDefaultProviderResponse, error)
	GetCacheStatsFunc           func(ctx context.Context, req *service.GetEmbeddingCacheStatsRequest) (*service.GetEmbeddingCacheStatsResponse, error)
	ClearCacheFunc              func(ctx context.Context, req *service.ClearEmbeddingCacheRequest) (*service.ClearEmbeddingCacheResponse, error)
	SetProviderStatusFunc       func(ctx context.Context, req *service.SetProviderStatusRequest) (*service.SetProviderStatusResponse, error)
}

var _ service.EmbeddingService = (*MockEmbeddingService)(nil)

func (m *MockEmbeddingService) GenerateEmbedding(ctx context.Context, req *service.GenerateEmbeddingRequest) (*service.GenerateEmbeddingResponse, error) {
	if m.GenerateEmbeddingFunc != nil {
		return m.GenerateEmbeddingFunc(ctx, req)
	}
	return &service.GenerateEmbeddingResponse{Embedding: make([]float32, 768), Timestamp: time.Now()}, nil
}

func (m *MockEmbeddingService) BatchGenerateEmbeddings(ctx context.Context, req *service.BatchGenerateEmbeddingsRequest) (*service.BatchGenerateEmbeddingsResponse, error) {
	if m.BatchGenerateEmbeddingsFunc != nil {
		return m.BatchGenerateEmbeddingsFunc(ctx, req)
	}
	return &service.BatchGenerateEmbeddingsResponse{Timestamp: time.Now()}, nil
}

func (m *MockEmbeddingService) ListProviders(ctx context.Context, req *service.ListProvidersRequest) (*service.ListProvidersResponse, error) {
	if m.ListProvidersFunc != nil {
		return m.ListProvidersFunc(ctx, req)
	}
	return &service.ListProvidersResponse{Timestamp: time.Now()}, nil
}

func (m *MockEmbeddingService) GetProviderInfo(ctx context.Context, req *service.GetProviderInfoRequest) (*service.GetProviderInfoResponse, error) {
	if m.GetProviderInfoFunc != nil {
		return m.GetProviderInfoFunc(ctx, req)
	}
	return &service.GetProviderInfoResponse{Timestamp: time.Now()}, nil
}

func (m *MockEmbeddingService) SetDefaultProvider(ctx context.Context, req *service.SetDefaultProviderRequest) (*service.SetDefaultProviderResponse, error) {
	if m.SetDefaultProviderFunc != nil {
		return m.SetDefaultProviderFunc(ctx, req)
	}
	return &service.SetDefaultProviderResponse{Timestamp: time.Now()}, nil
}

func (m *MockEmbeddingService) GetCacheStats(ctx context.Context, req *service.GetEmbeddingCacheStatsRequest) (*service.GetEmbeddingCacheStatsResponse, error) {
	if m.GetCacheStatsFunc != nil {
		return m.GetCacheStatsFunc(ctx, req)
	}
	return &service.GetEmbeddingCacheStatsResponse{Timestamp: time.Now()}, nil
}

func (m *MockEmbeddingService) ClearCache(ctx context.Context, req *service.ClearEmbeddingCacheRequest) (*service.ClearEmbeddingCacheResponse, error) {
	if m.ClearCacheFunc != nil {
		return m.ClearCacheFunc(ctx, req)
	}
	return &service.ClearEmbeddingCacheResponse{Timestamp: time.Now()}, nil
}

func (m *MockEmbeddingService) SetProviderStatus(ctx context.Context, req *service.SetProviderStatusRequest) (*service.SetProviderStatusResponse, error) {
	if m.SetProviderStatusFunc != nil {
		return m.SetProviderStatusFunc(ctx, req)
	}
	return &service.SetProviderStatusResponse{Name: req.Name, Enabled: req.Enabled, Timestamp: time.Now()}, nil
}

// =============================================================================
// MockOllamaService
// =============================================================================

// MockOllamaService is a mock implementation of service.OllamaService.
type MockOllamaService struct {
	EmbedFunc          func(ctx context.Context, req *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error)
	EmbedWithModelFunc func(ctx context.Context, req *service.OllamaEmbedWithModelRequest) (*service.OllamaEmbedResponse, error)
	EmbedBatchFunc     func(ctx context.Context, req *service.OllamaEmbedBatchRequest) (*service.OllamaEmbedBatchResponse, error)
	ListModelsFunc     func(ctx context.Context, req *service.OllamaListModelsRequest) (*service.OllamaListModelsResponse, error)
	GetModelInfoFunc   func(ctx context.Context, req *service.OllamaGetModelInfoRequest) (*service.OllamaGetModelInfoResponse, error)
	HealthFunc         func(ctx context.Context, req *service.OllamaHealthRequest) (*service.OllamaHealthResponse, error)
	IsAvailableFunc    func() bool
	DefaultModelFunc   func() string
	BaseURLFunc        func() string
}

var _ service.OllamaService = (*MockOllamaService)(nil)

func (m *MockOllamaService) Embed(ctx context.Context, req *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
	if m.EmbedFunc != nil {
		return m.EmbedFunc(ctx, req)
	}
	return &service.OllamaEmbedResponse{Embedding: make([]float32, 768), Timestamp: time.Now()}, nil
}

func (m *MockOllamaService) EmbedWithModel(ctx context.Context, req *service.OllamaEmbedWithModelRequest) (*service.OllamaEmbedResponse, error) {
	if m.EmbedWithModelFunc != nil {
		return m.EmbedWithModelFunc(ctx, req)
	}
	return &service.OllamaEmbedResponse{Embedding: make([]float32, 768), Model: req.Model, Timestamp: time.Now()}, nil
}

func (m *MockOllamaService) EmbedBatch(ctx context.Context, req *service.OllamaEmbedBatchRequest) (*service.OllamaEmbedBatchResponse, error) {
	if m.EmbedBatchFunc != nil {
		return m.EmbedBatchFunc(ctx, req)
	}
	return &service.OllamaEmbedBatchResponse{Timestamp: time.Now()}, nil
}

func (m *MockOllamaService) ListModels(ctx context.Context, req *service.OllamaListModelsRequest) (*service.OllamaListModelsResponse, error) {
	if m.ListModelsFunc != nil {
		return m.ListModelsFunc(ctx, req)
	}
	return &service.OllamaListModelsResponse{Timestamp: time.Now()}, nil
}

func (m *MockOllamaService) GetModelInfo(ctx context.Context, req *service.OllamaGetModelInfoRequest) (*service.OllamaGetModelInfoResponse, error) {
	if m.GetModelInfoFunc != nil {
		return m.GetModelInfoFunc(ctx, req)
	}
	return &service.OllamaGetModelInfoResponse{Model: req.Model, Timestamp: time.Now()}, nil
}

func (m *MockOllamaService) Health(ctx context.Context, req *service.OllamaHealthRequest) (*service.OllamaHealthResponse, error) {
	if m.HealthFunc != nil {
		return m.HealthFunc(ctx, req)
	}
	return &service.OllamaHealthResponse{Available: true, Timestamp: time.Now()}, nil
}

func (m *MockOllamaService) IsAvailable() bool {
	if m.IsAvailableFunc != nil {
		return m.IsAvailableFunc()
	}
	return true
}

func (m *MockOllamaService) DefaultModel() string {
	if m.DefaultModelFunc != nil {
		return m.DefaultModelFunc()
	}
	return "nomic-embed-text"
}

func (m *MockOllamaService) BaseURL() string {
	if m.BaseURLFunc != nil {
		return m.BaseURLFunc()
	}
	return "http://localhost:11434"
}

// =============================================================================
// MockCertificateService
// =============================================================================

// MockCertificateService is a mock implementation of service.CertificateService.
type MockCertificateService struct {
	IssueCertificateFunc  func(ctx context.Context, req *service.IssueCertificateRequest) (*service.IssueCertificateResponse, error)
	SignCSRFunc           func(ctx context.Context, req *service.SignCSRRequest) (*service.SignCSRResponse, error)
	RevokeCertificateFunc func(ctx context.Context, req *service.RevokeCertificateRequest) (*service.RevokeCertificateResponse, error)
	VerifyCertificateFunc func(ctx context.Context, req *service.VerifyCertificateRequest) (*service.VerifyCertificateResponse, error)
	GetCACertificateFunc  func(ctx context.Context, req *service.GetCACertificateRequest) (*service.GetCACertificateResponse, error)
	GetTLSConfigFunc      func(ctx context.Context, req *service.GetTLSConfigRequest) (*service.GetTLSConfigResponse, error)
	IsInitializedFunc     func() bool
	CAServiceFunc         func() xkmstransport.Client
	GetCABundleFunc       func(ctx context.Context, req *service.GetCABundleRequest) (*service.GetCABundleResponse, error)
	GenerateCRLFunc       func(ctx context.Context, req *service.GenerateCRLRequest) (*service.GenerateCRLResponse, error)
	IsRevokedFunc         func(ctx context.Context, req *service.IsRevokedRequest) (*service.IsRevokedResponse, error)
}

var _ service.CertificateService = (*MockCertificateService)(nil)

func (m *MockCertificateService) IssueCertificate(ctx context.Context, req *service.IssueCertificateRequest) (*service.IssueCertificateResponse, error) {
	if m.IssueCertificateFunc != nil {
		return m.IssueCertificateFunc(ctx, req)
	}
	return &service.IssueCertificateResponse{}, nil
}

func (m *MockCertificateService) SignCSR(ctx context.Context, req *service.SignCSRRequest) (*service.SignCSRResponse, error) {
	if m.SignCSRFunc != nil {
		return m.SignCSRFunc(ctx, req)
	}
	return &service.SignCSRResponse{}, nil
}

func (m *MockCertificateService) RevokeCertificate(ctx context.Context, req *service.RevokeCertificateRequest) (*service.RevokeCertificateResponse, error) {
	if m.RevokeCertificateFunc != nil {
		return m.RevokeCertificateFunc(ctx, req)
	}
	return &service.RevokeCertificateResponse{Success: true}, nil
}

func (m *MockCertificateService) VerifyCertificate(ctx context.Context, req *service.VerifyCertificateRequest) (*service.VerifyCertificateResponse, error) {
	if m.VerifyCertificateFunc != nil {
		return m.VerifyCertificateFunc(ctx, req)
	}
	return &service.VerifyCertificateResponse{Valid: true}, nil
}

func (m *MockCertificateService) GetCACertificate(ctx context.Context, req *service.GetCACertificateRequest) (*service.GetCACertificateResponse, error) {
	if m.GetCACertificateFunc != nil {
		return m.GetCACertificateFunc(ctx, req)
	}
	return &service.GetCACertificateResponse{}, nil
}

func (m *MockCertificateService) GetTLSConfig(ctx context.Context, req *service.GetTLSConfigRequest) (*service.GetTLSConfigResponse, error) {
	if m.GetTLSConfigFunc != nil {
		return m.GetTLSConfigFunc(ctx, req)
	}
	return &service.GetTLSConfigResponse{}, nil
}

func (m *MockCertificateService) IsInitialized() bool {
	if m.IsInitializedFunc != nil {
		return m.IsInitializedFunc()
	}
	return true
}

func (m *MockCertificateService) CAService() xkmstransport.Client {
	if m.CAServiceFunc != nil {
		return m.CAServiceFunc()
	}
	return nil
}

func (m *MockCertificateService) GetCABundle(ctx context.Context, req *service.GetCABundleRequest) (*service.GetCABundleResponse, error) {
	if m.GetCABundleFunc != nil {
		return m.GetCABundleFunc(ctx, req)
	}
	return &service.GetCABundleResponse{}, nil
}

func (m *MockCertificateService) GenerateCRL(ctx context.Context, req *service.GenerateCRLRequest) (*service.GenerateCRLResponse, error) {
	if m.GenerateCRLFunc != nil {
		return m.GenerateCRLFunc(ctx, req)
	}
	return &service.GenerateCRLResponse{}, nil
}

func (m *MockCertificateService) IsRevoked(ctx context.Context, req *service.IsRevokedRequest) (*service.IsRevokedResponse, error) {
	if m.IsRevokedFunc != nil {
		return m.IsRevokedFunc(ctx, req)
	}
	return &service.IsRevokedResponse{}, nil
}

// =============================================================================
// MockDynamicSecretsService
// =============================================================================

// MockDynamicSecretsService is a mock implementation of service.DynamicSecretsService.
type MockDynamicSecretsService struct {
	CreateRoleFunc         func(ctx context.Context, req *service.CreateRoleRequest) (*service.CreateRoleResponse, error)
	GetRoleFunc            func(ctx context.Context, req *service.GetRoleRequest) (*service.GetRoleResponse, error)
	ListRolesFunc          func(ctx context.Context, req *service.ListRolesRequest) (*service.ListRolesResponse, error)
	DeleteRoleFunc         func(ctx context.Context, req *service.DeleteRoleRequest) (*service.DeleteRoleResponse, error)
	GenerateCredentialFunc func(ctx context.Context, req *service.GenerateCredentialRequest) (*service.GenerateCredentialResponse, error)
	RenewLeaseFunc         func(ctx context.Context, req *service.RenewLeaseRequest) (*service.RenewLeaseResponse, error)
	RevokeLeaseFunc        func(ctx context.Context, req *service.RevokeLeaseRequest) (*service.RevokeLeaseResponse, error)
	ListLeasesFunc         func(ctx context.Context, req *service.ListLeasesRequest) (*service.ListLeasesResponse, error)
}

var _ service.DynamicSecretsService = (*MockDynamicSecretsService)(nil)

func (m *MockDynamicSecretsService) CreateRole(ctx context.Context, req *service.CreateRoleRequest) (*service.CreateRoleResponse, error) {
	if m.CreateRoleFunc != nil {
		return m.CreateRoleFunc(ctx, req)
	}
	return &service.CreateRoleResponse{}, nil
}

func (m *MockDynamicSecretsService) GetRole(ctx context.Context, req *service.GetRoleRequest) (*service.GetRoleResponse, error) {
	if m.GetRoleFunc != nil {
		return m.GetRoleFunc(ctx, req)
	}
	return &service.GetRoleResponse{}, nil
}

func (m *MockDynamicSecretsService) ListRoles(ctx context.Context, req *service.ListRolesRequest) (*service.ListRolesResponse, error) {
	if m.ListRolesFunc != nil {
		return m.ListRolesFunc(ctx, req)
	}
	return &service.ListRolesResponse{}, nil
}

func (m *MockDynamicSecretsService) DeleteRole(ctx context.Context, req *service.DeleteRoleRequest) (*service.DeleteRoleResponse, error) {
	if m.DeleteRoleFunc != nil {
		return m.DeleteRoleFunc(ctx, req)
	}
	return &service.DeleteRoleResponse{}, nil
}

func (m *MockDynamicSecretsService) GenerateCredential(ctx context.Context, req *service.GenerateCredentialRequest) (*service.GenerateCredentialResponse, error) {
	if m.GenerateCredentialFunc != nil {
		return m.GenerateCredentialFunc(ctx, req)
	}
	return &service.GenerateCredentialResponse{}, nil
}

func (m *MockDynamicSecretsService) RenewLease(ctx context.Context, req *service.RenewLeaseRequest) (*service.RenewLeaseResponse, error) {
	if m.RenewLeaseFunc != nil {
		return m.RenewLeaseFunc(ctx, req)
	}
	return &service.RenewLeaseResponse{}, nil
}

func (m *MockDynamicSecretsService) RevokeLease(ctx context.Context, req *service.RevokeLeaseRequest) (*service.RevokeLeaseResponse, error) {
	if m.RevokeLeaseFunc != nil {
		return m.RevokeLeaseFunc(ctx, req)
	}
	return &service.RevokeLeaseResponse{}, nil
}

func (m *MockDynamicSecretsService) ListLeases(ctx context.Context, req *service.ListLeasesRequest) (*service.ListLeasesResponse, error) {
	if m.ListLeasesFunc != nil {
		return m.ListLeasesFunc(ctx, req)
	}
	return &service.ListLeasesResponse{}, nil
}

// =============================================================================
// MockClusterDEKService
// =============================================================================

// MockClusterDEKService is a mock implementation of service.ClusterDEKService.
type MockClusterDEKService struct {
	InitializeClusterDEKFunc  func(ctx context.Context, req *service.InitClusterDEKRequest) (*service.InitClusterDEKResponse, error)
	GetClusterDEKStatusFunc   func(ctx context.Context) (*service.ClusterDEKStatusResponse, error)
	RotateClusterDEKFunc      func(ctx context.Context, req *service.RotateClusterDEKRequest) (*service.RotateClusterDEKResponse, error)
	SyncNodeDEKFunc           func(ctx context.Context, req *service.SyncNodeDEKRequest) (*service.SyncNodeDEKResponse, error)
	GetNodeDEKStatusFunc      func(ctx context.Context, req *service.GetNodeDEKStatusRequest) (*service.GetNodeDEKStatusResponse, error)
	StartReencryptionFunc     func(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error)
	GetReencryptionStatusFunc func(ctx context.Context) (*service.ReencryptionStatusResponse, error)
	CancelReencryptionFunc    func(ctx context.Context) error
	RecoverDEKFunc            func(ctx context.Context, req *service.RecoverDEKRequest) (*service.RecoverDEKResponse, error)
}

var _ service.ClusterDEKService = (*MockClusterDEKService)(nil)

func (m *MockClusterDEKService) InitializeClusterDEK(ctx context.Context, req *service.InitClusterDEKRequest) (*service.InitClusterDEKResponse, error) {
	if m.InitializeClusterDEKFunc != nil {
		return m.InitializeClusterDEKFunc(ctx, req)
	}
	return &service.InitClusterDEKResponse{Version: 1, CreatedAt: time.Now()}, nil
}

func (m *MockClusterDEKService) GetClusterDEKStatus(ctx context.Context) (*service.ClusterDEKStatusResponse, error) {
	if m.GetClusterDEKStatusFunc != nil {
		return m.GetClusterDEKStatusFunc(ctx)
	}
	return &service.ClusterDEKStatusResponse{Initialized: true, CurrentVersion: 1}, nil
}

func (m *MockClusterDEKService) RotateClusterDEK(ctx context.Context, req *service.RotateClusterDEKRequest) (*service.RotateClusterDEKResponse, error) {
	if m.RotateClusterDEKFunc != nil {
		return m.RotateClusterDEKFunc(ctx, req)
	}
	return &service.RotateClusterDEKResponse{NewVersion: 2, OldVersion: 1, RotatedAt: time.Now()}, nil
}

func (m *MockClusterDEKService) SyncNodeDEK(ctx context.Context, req *service.SyncNodeDEKRequest) (*service.SyncNodeDEKResponse, error) {
	if m.SyncNodeDEKFunc != nil {
		return m.SyncNodeDEKFunc(ctx, req)
	}
	return &service.SyncNodeDEKResponse{NodeID: req.NodeID, SyncedAt: time.Now()}, nil
}

func (m *MockClusterDEKService) GetNodeDEKStatus(ctx context.Context, req *service.GetNodeDEKStatusRequest) (*service.GetNodeDEKStatusResponse, error) {
	if m.GetNodeDEKStatusFunc != nil {
		return m.GetNodeDEKStatusFunc(ctx, req)
	}
	return &service.GetNodeDEKStatusResponse{NodeID: req.NodeID, Status: service.NodeDEKSynced}, nil
}

func (m *MockClusterDEKService) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	if m.StartReencryptionFunc != nil {
		return m.StartReencryptionFunc(ctx, req)
	}
	return &service.StartReencryptionResponse{Started: true, Timestamp: time.Now()}, nil
}

func (m *MockClusterDEKService) GetReencryptionStatus(ctx context.Context) (*service.ReencryptionStatusResponse, error) {
	if m.GetReencryptionStatusFunc != nil {
		return m.GetReencryptionStatusFunc(ctx)
	}
	return &service.ReencryptionStatusResponse{State: service.ReencryptionCompleted}, nil
}

func (m *MockClusterDEKService) CancelReencryption(ctx context.Context) error {
	if m.CancelReencryptionFunc != nil {
		return m.CancelReencryptionFunc(ctx)
	}
	return nil
}

func (m *MockClusterDEKService) RecoverDEK(ctx context.Context, req *service.RecoverDEKRequest) (*service.RecoverDEKResponse, error) {
	if m.RecoverDEKFunc != nil {
		return m.RecoverDEKFunc(ctx, req)
	}
	return &service.RecoverDEKResponse{Success: true, RecoveredAt: time.Now()}, nil
}

// =============================================================================
// MockObjectStorageService
// =============================================================================

// MockObjectStorageService is a mock implementation of service.ObjectStorageService.
type MockObjectStorageService struct {
	PutObjectFunc          func(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error)
	GetObjectFunc          func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error)
	DeleteObjectFunc       func(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error)
	HeadObjectFunc         func(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error)
	ListObjectsFunc        func(ctx context.Context, req *service.ListObjectsRequest) (*service.ListObjectsResponse, error)
	ListByTagFunc          func(ctx context.Context, req *service.ListByTagRequest) (*service.ListByTagResponse, error)
	ListByTenantFunc       func(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error)
	CopyObjectFunc         func(ctx context.Context, req *service.CopyObjectRequest) (*service.CopyObjectResponse, error)
	ReplicateObjectFunc    func(ctx context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error)
	MigrateObjectFunc      func(ctx context.Context, req *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error)
	ArchiveObjectFunc      func(ctx context.Context, req *service.ArchiveObjectRequest) (*service.ArchiveObjectResponse, error)
	RestoreObjectFunc      func(ctx context.Context, req *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error)
	VerifyObjectFunc       func(ctx context.Context, req *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error)
	SetObjectPolicyFunc    func(ctx context.Context, req *service.SetObjectPolicyRequest) (*service.SetObjectPolicyResponse, error)
	GetObjectPolicyFunc    func(ctx context.Context, req *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error)
	ListBackendsFunc       func(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error)
	GetBackendHealthFunc   func(ctx context.Context, req *service.GetBackendHealthRequest) (*service.GetBackendHealthResponse, error)
	GetTenantUsageFunc     func(ctx context.Context, req *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error)
	GetTenantQuotasFunc    func(ctx context.Context, req *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error)
	SetTenantQuotasFunc    func(ctx context.Context, req *service.SetTenantQuotasRequest) (*service.SetTenantQuotasResponse, error)
	BatchPutObjectsFunc    func(ctx context.Context, req *service.BatchPutObjectsRequest) (*service.BatchPutObjectsResponse, error)
	BatchGetObjectsFunc    func(ctx context.Context, req *service.BatchGetObjectsRequest) (*service.BatchGetObjectsResponse, error)
	BatchDeleteObjectsFunc func(ctx context.Context, req *service.BatchDeleteObjectsRequest) (*service.BatchDeleteObjectsResponse, error)
}

var _ service.ObjectStorageService = (*MockObjectStorageService)(nil)

func (m *MockObjectStorageService) PutObject(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
	if m.PutObjectFunc != nil {
		return m.PutObjectFunc(ctx, req)
	}
	return &service.PutObjectResponse{}, nil
}

func (m *MockObjectStorageService) GetObject(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
	if m.GetObjectFunc != nil {
		return m.GetObjectFunc(ctx, req)
	}
	return &service.GetObjectResponse{}, nil
}

func (m *MockObjectStorageService) DeleteObject(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error) {
	if m.DeleteObjectFunc != nil {
		return m.DeleteObjectFunc(ctx, req)
	}
	return &service.DeleteObjectResponse{}, nil
}

func (m *MockObjectStorageService) HeadObject(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error) {
	if m.HeadObjectFunc != nil {
		return m.HeadObjectFunc(ctx, req)
	}
	return &service.HeadObjectResponse{}, nil
}

func (m *MockObjectStorageService) ListObjects(ctx context.Context, req *service.ListObjectsRequest) (*service.ListObjectsResponse, error) {
	if m.ListObjectsFunc != nil {
		return m.ListObjectsFunc(ctx, req)
	}
	return &service.ListObjectsResponse{}, nil
}

func (m *MockObjectStorageService) ListByTag(ctx context.Context, req *service.ListByTagRequest) (*service.ListByTagResponse, error) {
	if m.ListByTagFunc != nil {
		return m.ListByTagFunc(ctx, req)
	}
	return &service.ListByTagResponse{}, nil
}

func (m *MockObjectStorageService) ListByTenant(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error) {
	if m.ListByTenantFunc != nil {
		return m.ListByTenantFunc(ctx, req)
	}
	return &service.ListByTenantResponse{}, nil
}

func (m *MockObjectStorageService) CopyObject(ctx context.Context, req *service.CopyObjectRequest) (*service.CopyObjectResponse, error) {
	if m.CopyObjectFunc != nil {
		return m.CopyObjectFunc(ctx, req)
	}
	return &service.CopyObjectResponse{}, nil
}

func (m *MockObjectStorageService) ReplicateObject(ctx context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error) {
	if m.ReplicateObjectFunc != nil {
		return m.ReplicateObjectFunc(ctx, req)
	}
	return &service.ReplicateObjectResponse{}, nil
}

func (m *MockObjectStorageService) MigrateObject(ctx context.Context, req *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error) {
	if m.MigrateObjectFunc != nil {
		return m.MigrateObjectFunc(ctx, req)
	}
	return &service.MigrateObjectResponse{}, nil
}

func (m *MockObjectStorageService) ArchiveObject(ctx context.Context, req *service.ArchiveObjectRequest) (*service.ArchiveObjectResponse, error) {
	if m.ArchiveObjectFunc != nil {
		return m.ArchiveObjectFunc(ctx, req)
	}
	return &service.ArchiveObjectResponse{}, nil
}

func (m *MockObjectStorageService) RestoreObject(ctx context.Context, req *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error) {
	if m.RestoreObjectFunc != nil {
		return m.RestoreObjectFunc(ctx, req)
	}
	return &service.RestoreObjectResponse{}, nil
}

func (m *MockObjectStorageService) VerifyObject(ctx context.Context, req *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error) {
	if m.VerifyObjectFunc != nil {
		return m.VerifyObjectFunc(ctx, req)
	}
	return &service.VerifyObjectResponse{}, nil
}

func (m *MockObjectStorageService) SetObjectPolicy(ctx context.Context, req *service.SetObjectPolicyRequest) (*service.SetObjectPolicyResponse, error) {
	if m.SetObjectPolicyFunc != nil {
		return m.SetObjectPolicyFunc(ctx, req)
	}
	return &service.SetObjectPolicyResponse{}, nil
}

func (m *MockObjectStorageService) GetObjectPolicy(ctx context.Context, req *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error) {
	if m.GetObjectPolicyFunc != nil {
		return m.GetObjectPolicyFunc(ctx, req)
	}
	return &service.GetObjectPolicyResponse{}, nil
}

func (m *MockObjectStorageService) ListBackends(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error) {
	if m.ListBackendsFunc != nil {
		return m.ListBackendsFunc(ctx, req)
	}
	return &service.ListBackendsResponse{}, nil
}

func (m *MockObjectStorageService) GetBackendHealth(ctx context.Context, req *service.GetBackendHealthRequest) (*service.GetBackendHealthResponse, error) {
	if m.GetBackendHealthFunc != nil {
		return m.GetBackendHealthFunc(ctx, req)
	}
	return &service.GetBackendHealthResponse{}, nil
}

func (m *MockObjectStorageService) GetTenantUsage(ctx context.Context, req *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error) {
	if m.GetTenantUsageFunc != nil {
		return m.GetTenantUsageFunc(ctx, req)
	}
	return &service.GetTenantUsageResponse{}, nil
}

func (m *MockObjectStorageService) GetTenantQuotas(ctx context.Context, req *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error) {
	if m.GetTenantQuotasFunc != nil {
		return m.GetTenantQuotasFunc(ctx, req)
	}
	return &service.GetTenantQuotasResponse{}, nil
}

func (m *MockObjectStorageService) SetTenantQuotas(ctx context.Context, req *service.SetTenantQuotasRequest) (*service.SetTenantQuotasResponse, error) {
	if m.SetTenantQuotasFunc != nil {
		return m.SetTenantQuotasFunc(ctx, req)
	}
	return &service.SetTenantQuotasResponse{}, nil
}

func (m *MockObjectStorageService) BatchPutObjects(ctx context.Context, req *service.BatchPutObjectsRequest) (*service.BatchPutObjectsResponse, error) {
	if m.BatchPutObjectsFunc != nil {
		return m.BatchPutObjectsFunc(ctx, req)
	}
	return &service.BatchPutObjectsResponse{}, nil
}

func (m *MockObjectStorageService) BatchGetObjects(ctx context.Context, req *service.BatchGetObjectsRequest) (*service.BatchGetObjectsResponse, error) {
	if m.BatchGetObjectsFunc != nil {
		return m.BatchGetObjectsFunc(ctx, req)
	}
	return &service.BatchGetObjectsResponse{}, nil
}

func (m *MockObjectStorageService) BatchDeleteObjects(ctx context.Context, req *service.BatchDeleteObjectsRequest) (*service.BatchDeleteObjectsResponse, error) {
	if m.BatchDeleteObjectsFunc != nil {
		return m.BatchDeleteObjectsFunc(ctx, req)
	}
	return &service.BatchDeleteObjectsResponse{}, nil
}

// =============================================================================
// MockObjectStorageAdminService
// =============================================================================

// MockObjectStorageAdminService is a mock implementation of service.ObjectStorageAdminService.
type MockObjectStorageAdminService struct {
	CreateBackendFunc                func(ctx context.Context, req *service.CreateBackendRequest) (*service.CreateBackendResponse, error)
	UpdateBackendFunc                func(ctx context.Context, req *service.UpdateBackendRequest) (*service.UpdateBackendResponse, error)
	DeleteBackendFunc                func(ctx context.Context, req *service.DeleteBackendRequest) (*service.DeleteBackendResponse, error)
	SetBackendStatusFunc             func(ctx context.Context, req *service.SetBackendStatusRequest) (*service.SetBackendStatusResponse, error)
	GetStorageStatsFunc              func(ctx context.Context, req *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error)
	RunGarbageCollectionFunc         func(ctx context.Context, req *service.RunGarbageCollectionRequest) (*service.RunGarbageCollectionResponse, error)
	GetGarbageCollectionProgressFunc func(ctx context.Context, req *service.GetGarbageCollectionProgressRequest) (*service.GetGarbageCollectionProgressResponse, error)
	RepairObjectFunc                 func(ctx context.Context, req *service.RepairObjectRequest) (*service.RepairObjectResponse, error)
	CompactBackendFunc               func(ctx context.Context, req *service.CompactBackendRequest) (*service.CompactBackendResponse, error)
}

var _ service.ObjectStorageAdminService = (*MockObjectStorageAdminService)(nil)

func (m *MockObjectStorageAdminService) CreateBackend(ctx context.Context, req *service.CreateBackendRequest) (*service.CreateBackendResponse, error) {
	if m.CreateBackendFunc != nil {
		return m.CreateBackendFunc(ctx, req)
	}
	return &service.CreateBackendResponse{}, nil
}

func (m *MockObjectStorageAdminService) UpdateBackend(ctx context.Context, req *service.UpdateBackendRequest) (*service.UpdateBackendResponse, error) {
	if m.UpdateBackendFunc != nil {
		return m.UpdateBackendFunc(ctx, req)
	}
	return &service.UpdateBackendResponse{}, nil
}

func (m *MockObjectStorageAdminService) DeleteBackend(ctx context.Context, req *service.DeleteBackendRequest) (*service.DeleteBackendResponse, error) {
	if m.DeleteBackendFunc != nil {
		return m.DeleteBackendFunc(ctx, req)
	}
	return &service.DeleteBackendResponse{}, nil
}

func (m *MockObjectStorageAdminService) SetBackendStatus(ctx context.Context, req *service.SetBackendStatusRequest) (*service.SetBackendStatusResponse, error) {
	if m.SetBackendStatusFunc != nil {
		return m.SetBackendStatusFunc(ctx, req)
	}
	return &service.SetBackendStatusResponse{}, nil
}

func (m *MockObjectStorageAdminService) GetStorageStats(ctx context.Context, req *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error) {
	if m.GetStorageStatsFunc != nil {
		return m.GetStorageStatsFunc(ctx, req)
	}
	return &service.GetStorageStatsResponse{}, nil
}

func (m *MockObjectStorageAdminService) RunGarbageCollection(ctx context.Context, req *service.RunGarbageCollectionRequest) (*service.RunGarbageCollectionResponse, error) {
	if m.RunGarbageCollectionFunc != nil {
		return m.RunGarbageCollectionFunc(ctx, req)
	}
	return &service.RunGarbageCollectionResponse{}, nil
}

func (m *MockObjectStorageAdminService) GetGarbageCollectionProgress(ctx context.Context, req *service.GetGarbageCollectionProgressRequest) (*service.GetGarbageCollectionProgressResponse, error) {
	if m.GetGarbageCollectionProgressFunc != nil {
		return m.GetGarbageCollectionProgressFunc(ctx, req)
	}
	return &service.GetGarbageCollectionProgressResponse{}, nil
}

func (m *MockObjectStorageAdminService) RepairObject(ctx context.Context, req *service.RepairObjectRequest) (*service.RepairObjectResponse, error) {
	if m.RepairObjectFunc != nil {
		return m.RepairObjectFunc(ctx, req)
	}
	return &service.RepairObjectResponse{}, nil
}

func (m *MockObjectStorageAdminService) CompactBackend(ctx context.Context, req *service.CompactBackendRequest) (*service.CompactBackendResponse, error) {
	if m.CompactBackendFunc != nil {
		return m.CompactBackendFunc(ctx, req)
	}
	return &service.CompactBackendResponse{}, nil
}

// =============================================================================
// MockParameterStoreService
// =============================================================================

// MockParameterStoreService is a mock implementation of service.ParameterStoreService.
type MockParameterStoreService struct {
	GetParameterFunc     func(ctx context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error)
	SetParameterFunc     func(ctx context.Context, req *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error)
	DeleteParameterFunc  func(ctx context.Context, req *interfaces.DeleteParameterRequest) (*interfaces.DeleteParameterResponse, error)
	GetParametersFunc    func(ctx context.Context, req *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error)
	SetParametersFunc    func(ctx context.Context, req *interfaces.SetParametersRequest) (*interfaces.SetParametersResponse, error)
	DeleteParametersFunc func(ctx context.Context, req *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error)
	ListParametersFunc   func(ctx context.Context, req *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error)
	SearchByTagsFunc     func(ctx context.Context, req *interfaces.SearchByTagsRequest) (*interfaces.SearchByTagsResponse, error)
}

var _ service.ParameterStoreService = (*MockParameterStoreService)(nil)

func (m *MockParameterStoreService) GetParameter(ctx context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error) {
	if m.GetParameterFunc != nil {
		return m.GetParameterFunc(ctx, req)
	}
	return &interfaces.GetParameterResponse{}, nil
}

func (m *MockParameterStoreService) SetParameter(ctx context.Context, req *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error) {
	if m.SetParameterFunc != nil {
		return m.SetParameterFunc(ctx, req)
	}
	return &interfaces.SetParameterResponse{}, nil
}

func (m *MockParameterStoreService) DeleteParameter(ctx context.Context, req *interfaces.DeleteParameterRequest) (*interfaces.DeleteParameterResponse, error) {
	if m.DeleteParameterFunc != nil {
		return m.DeleteParameterFunc(ctx, req)
	}
	return &interfaces.DeleteParameterResponse{}, nil
}

func (m *MockParameterStoreService) GetParameters(ctx context.Context, req *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error) {
	if m.GetParametersFunc != nil {
		return m.GetParametersFunc(ctx, req)
	}
	return &interfaces.GetParametersResponse{}, nil
}

func (m *MockParameterStoreService) SetParameters(ctx context.Context, req *interfaces.SetParametersRequest) (*interfaces.SetParametersResponse, error) {
	if m.SetParametersFunc != nil {
		return m.SetParametersFunc(ctx, req)
	}
	return &interfaces.SetParametersResponse{}, nil
}

func (m *MockParameterStoreService) DeleteParameters(ctx context.Context, req *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error) {
	if m.DeleteParametersFunc != nil {
		return m.DeleteParametersFunc(ctx, req)
	}
	return &interfaces.DeleteParametersResponse{}, nil
}

func (m *MockParameterStoreService) ListParameters(ctx context.Context, req *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error) {
	if m.ListParametersFunc != nil {
		return m.ListParametersFunc(ctx, req)
	}
	return &interfaces.ListParametersResponse{}, nil
}

func (m *MockParameterStoreService) SearchByTags(ctx context.Context, req *interfaces.SearchByTagsRequest) (*interfaces.SearchByTagsResponse, error) {
	if m.SearchByTagsFunc != nil {
		return m.SearchByTagsFunc(ctx, req)
	}
	return &interfaces.SearchByTagsResponse{}, nil
}

// =============================================================================
// MockObjectTransferService
// =============================================================================

// MockObjectTransferService is a mock implementation of service.ObjectTransferService.
type MockObjectTransferService struct {
	FetchObjectFunc        func(ctx context.Context, req *service.FetchObjectRequest) (*service.FetchObjectResponse, error)
	StreamFetchObjectsFunc func(ctx context.Context, req *service.StreamFetchObjectsRequest) (*service.StreamFetchObjectsResponse, error)
}

var _ service.ObjectTransferService = (*MockObjectTransferService)(nil)

func (m *MockObjectTransferService) FetchObject(ctx context.Context, req *service.FetchObjectRequest) (*service.FetchObjectResponse, error) {
	if m.FetchObjectFunc != nil {
		return m.FetchObjectFunc(ctx, req)
	}
	return &service.FetchObjectResponse{Found: true}, nil
}

func (m *MockObjectTransferService) StreamFetchObjects(ctx context.Context, req *service.StreamFetchObjectsRequest) (*service.StreamFetchObjectsResponse, error) {
	if m.StreamFetchObjectsFunc != nil {
		return m.StreamFetchObjectsFunc(ctx, req)
	}
	return &service.StreamFetchObjectsResponse{}, nil
}

// =============================================================================
// MockProjectService
// =============================================================================

// MockProjectService is a mock implementation of service.ProjectService.
type MockProjectService struct {
	CreateProjectFunc       func(ctx context.Context, req *service.CreateProjectRequest) (*service.CreateProjectResponse, error)
	GetProjectFunc          func(ctx context.Context, req *service.GetProjectRequest) (*service.GetProjectResponse, error)
	GetProjectByNameFunc    func(ctx context.Context, req *service.GetProjectByNameRequest) (*service.GetProjectResponse, error)
	UpdateProjectFunc       func(ctx context.Context, req *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error)
	DeleteProjectFunc       func(ctx context.Context, req *service.DeleteProjectRequest) (*service.DeleteProjectResponse, error)
	ListProjectsFunc        func(ctx context.Context, req *service.ListProjectsRequest) (*service.ListProjectsResponse, error)
	AuthorizeAgentFunc      func(ctx context.Context, req *service.AuthorizeAgentRequest) error
	GrantAgentRoleFunc      func(ctx context.Context, req *service.GrantAgentRoleRequest) (*service.GrantAgentRoleResponse, error)
	RevokeAgentRoleFunc     func(ctx context.Context, req *service.RevokeAgentRoleRequest) (*service.RevokeAgentRoleResponse, error)
}

var _ service.ProjectService = (*MockProjectService)(nil)

func (m *MockProjectService) CreateProject(ctx context.Context, req *service.CreateProjectRequest) (*service.CreateProjectResponse, error) {
	if m.CreateProjectFunc != nil {
		return m.CreateProjectFunc(ctx, req)
	}
	return &service.CreateProjectResponse{}, nil
}

func (m *MockProjectService) GetProject(ctx context.Context, req *service.GetProjectRequest) (*service.GetProjectResponse, error) {
	if m.GetProjectFunc != nil {
		return m.GetProjectFunc(ctx, req)
	}
	return &service.GetProjectResponse{}, nil
}

func (m *MockProjectService) GetProjectByName(ctx context.Context, req *service.GetProjectByNameRequest) (*service.GetProjectResponse, error) {
	if m.GetProjectByNameFunc != nil {
		return m.GetProjectByNameFunc(ctx, req)
	}
	return &service.GetProjectResponse{}, nil
}

func (m *MockProjectService) UpdateProject(ctx context.Context, req *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
	if m.UpdateProjectFunc != nil {
		return m.UpdateProjectFunc(ctx, req)
	}
	return &service.UpdateProjectResponse{}, nil
}

func (m *MockProjectService) DeleteProject(ctx context.Context, req *service.DeleteProjectRequest) (*service.DeleteProjectResponse, error) {
	if m.DeleteProjectFunc != nil {
		return m.DeleteProjectFunc(ctx, req)
	}
	return &service.DeleteProjectResponse{Deleted: true}, nil
}

func (m *MockProjectService) ListProjects(ctx context.Context, req *service.ListProjectsRequest) (*service.ListProjectsResponse, error) {
	if m.ListProjectsFunc != nil {
		return m.ListProjectsFunc(ctx, req)
	}
	return &service.ListProjectsResponse{}, nil
}

func (m *MockProjectService) AuthorizeAgent(ctx context.Context, req *service.AuthorizeAgentRequest) error {
	if m.AuthorizeAgentFunc != nil {
		return m.AuthorizeAgentFunc(ctx, req)
	}
	return nil
}

func (m *MockProjectService) GrantAgentRole(ctx context.Context, req *service.GrantAgentRoleRequest) (*service.GrantAgentRoleResponse, error) {
	if m.GrantAgentRoleFunc != nil {
		return m.GrantAgentRoleFunc(ctx, req)
	}
	return &service.GrantAgentRoleResponse{
		ProjectID: req.ProjectID,
		AgentID:   req.AgentID,
		Role:      req.Role,
	}, nil
}

func (m *MockProjectService) RevokeAgentRole(ctx context.Context, req *service.RevokeAgentRoleRequest) (*service.RevokeAgentRoleResponse, error) {
	if m.RevokeAgentRoleFunc != nil {
		return m.RevokeAgentRoleFunc(ctx, req)
	}
	return &service.RevokeAgentRoleResponse{
		ProjectID: req.ProjectID,
		AgentID:   req.AgentID,
	}, nil
}

// =============================================================================
// MockAgentAdminService
// =============================================================================

// MockAgentAdminService is a test double for service.AgentAdminService.
type MockAgentAdminService struct {
	DisableAgentFunc           func(ctx context.Context, req *service.DisableAgentRequest) (*service.DisableAgentResponse, error)
	EnableAgentFunc            func(ctx context.Context, req *service.EnableAgentRequest) (*service.EnableAgentResponse, error)
	GetAgentRegistrationFunc   func(ctx context.Context, req *service.GetAgentRegistrationRequest) (*service.GetAgentRegistrationResponse, error)
	ListAgentRegistrationsFunc func(ctx context.Context, req *service.ListAgentRegistrationsRequest) (*service.ListAgentRegistrationsResponse, error)
}

var _ service.AgentAdminService = (*MockAgentAdminService)(nil)

func (m *MockAgentAdminService) DisableAgent(ctx context.Context, req *service.DisableAgentRequest) (*service.DisableAgentResponse, error) {
	if m.DisableAgentFunc != nil {
		return m.DisableAgentFunc(ctx, req)
	}
	return &service.DisableAgentResponse{AgentID: req.AgentID, Disabled: true}, nil
}

func (m *MockAgentAdminService) EnableAgent(ctx context.Context, req *service.EnableAgentRequest) (*service.EnableAgentResponse, error) {
	if m.EnableAgentFunc != nil {
		return m.EnableAgentFunc(ctx, req)
	}
	return &service.EnableAgentResponse{AgentID: req.AgentID, Enabled: true}, nil
}

func (m *MockAgentAdminService) GetAgentRegistration(ctx context.Context, req *service.GetAgentRegistrationRequest) (*service.GetAgentRegistrationResponse, error) {
	if m.GetAgentRegistrationFunc != nil {
		return m.GetAgentRegistrationFunc(ctx, req)
	}
	return &service.GetAgentRegistrationResponse{}, nil
}

func (m *MockAgentAdminService) ListAgentRegistrations(ctx context.Context, req *service.ListAgentRegistrationsRequest) (*service.ListAgentRegistrationsResponse, error) {
	if m.ListAgentRegistrationsFunc != nil {
		return m.ListAgentRegistrationsFunc(ctx, req)
	}
	return &service.ListAgentRegistrationsResponse{}, nil
}
