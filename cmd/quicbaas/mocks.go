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

// Package main provides the QuicBaaS command-line interface.
package main

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// MockCryptoClient is a mock implementation of CryptoClient for testing.
type MockCryptoClient struct {
	KMSEncryptFunc         func(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error)
	KMSDecryptFunc         func(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error)
	KMSSignFunc            func(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error)
	KMSVerifyFunc          func(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error)
	KMSHashFunc            func(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error)
	KMSGenerateHMACFunc    func(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error)
	KMSVerifyHMACFunc      func(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error)
	KMSRewrapFunc          func(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error)
	KMSGenerateDataKeyFunc func(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error)
	KMSGenerateRandomFunc  func(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error)
	CloseFunc              func() error
}

// KMSEncrypt mocks the KMSEncrypt method.
func (m *MockCryptoClient) KMSEncrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	if m.KMSEncryptFunc != nil {
		return m.KMSEncryptFunc(ctx, req)
	}
	return &service.EncryptResponse{}, nil
}

// KMSDecrypt mocks the KMSDecrypt method.
func (m *MockCryptoClient) KMSDecrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	if m.KMSDecryptFunc != nil {
		return m.KMSDecryptFunc(ctx, req)
	}
	return &service.DecryptResponse{}, nil
}

// KMSSign mocks the KMSSign method.
func (m *MockCryptoClient) KMSSign(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error) {
	if m.KMSSignFunc != nil {
		return m.KMSSignFunc(ctx, req)
	}
	return &service.SignResponse{
		KeyID:     req.KeyID,
		Signature: []byte("mock-signature"),
		Timestamp: time.Now(),
	}, nil
}

// KMSVerify mocks the KMSVerify method.
func (m *MockCryptoClient) KMSVerify(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error) {
	if m.KMSVerifyFunc != nil {
		return m.KMSVerifyFunc(ctx, req)
	}
	return &service.VerifyResponse{
		KeyID:     req.KeyID,
		Valid:     true,
		Timestamp: time.Now(),
	}, nil
}

// KMSHash mocks the KMSHash method.
func (m *MockCryptoClient) KMSHash(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error) {
	if m.KMSHashFunc != nil {
		return m.KMSHashFunc(ctx, req)
	}
	return &service.HashResponse{
		Algorithm: req.Algorithm,
		Hash:      []byte("mock-hash"),
		Timestamp: time.Now(),
	}, nil
}

// KMSGenerateHMAC mocks the KMSGenerateHMAC method.
func (m *MockCryptoClient) KMSGenerateHMAC(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error) {
	if m.KMSGenerateHMACFunc != nil {
		return m.KMSGenerateHMACFunc(ctx, req)
	}
	return &service.HMACResponse{
		KeyID:     req.KeyID,
		HMAC:      []byte("mock-hmac"),
		Timestamp: time.Now(),
	}, nil
}

// KMSVerifyHMAC mocks the KMSVerifyHMAC method.
func (m *MockCryptoClient) KMSVerifyHMAC(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error) {
	if m.KMSVerifyHMACFunc != nil {
		return m.KMSVerifyHMACFunc(ctx, req)
	}
	return &service.VerifyHMACResponse{
		KeyID:     req.KeyID,
		Valid:     true,
		Timestamp: time.Now(),
	}, nil
}

// KMSRewrap mocks the KMSRewrap method.
func (m *MockCryptoClient) KMSRewrap(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error) {
	if m.KMSRewrapFunc != nil {
		return m.KMSRewrapFunc(ctx, req)
	}
	return &service.RewrapResponse{
		KeyID:         req.KeyID,
		OldKeyVersion: 1,
		NewKeyVersion: 2,
		Ciphertext:    req.Ciphertext,
		Timestamp:     time.Now(),
	}, nil
}

// KMSGenerateDataKey mocks the KMSGenerateDataKey method.
func (m *MockCryptoClient) KMSGenerateDataKey(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
	if m.KMSGenerateDataKeyFunc != nil {
		return m.KMSGenerateDataKeyFunc(ctx, req)
	}
	return &service.GenerateDataKeyResponse{}, nil
}

// KMSGenerateRandom mocks the KMSGenerateRandom method.
func (m *MockCryptoClient) KMSGenerateRandom(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
	if m.KMSGenerateRandomFunc != nil {
		return m.KMSGenerateRandomFunc(ctx, req)
	}
	return &service.GenerateRandomResponse{
		RandomBytes: make([]byte, req.NumBytes),
		Timestamp:   time.Now(),
	}, nil
}

// Close mocks the Close method.
func (m *MockCryptoClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// MockKMSClient is a mock implementation of KMSClient for testing.
type MockKMSClient struct {
	KMSGenerateKeyFunc     func(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error)
	KMSImportKeyFunc       func(ctx context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error)
	KMSGetKeyFunc          func(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error)
	KMSListKeysFunc        func(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error)
	KMSRotateKeyFunc       func(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error)
	KMSDeleteKeyFunc       func(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error)
	KMSEncryptFunc         func(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error)
	KMSDecryptFunc         func(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error)
	KMSUpdateKeyStatusFunc func(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error)
	KMSListKeyVersionsFunc func(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error)
	KMSExportKeyFunc       func(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error)
	KMSUnwrapKeyFunc       func(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error)
	KMSSetKeySafetyFunc    func(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error)
	KMSClearKeySafetyFunc  func(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error)
	CloseFunc              func() error
}

// KMSGenerateKey mocks the KMSGenerateKey method.
func (m *MockKMSClient) KMSGenerateKey(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
	if m.KMSGenerateKeyFunc != nil {
		return m.KMSGenerateKeyFunc(ctx, req)
	}
	return &service.GenerateKeyResponse{}, nil
}

// KMSImportKey mocks the KMSImportKey method.
func (m *MockKMSClient) KMSImportKey(ctx context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
	if m.KMSImportKeyFunc != nil {
		return m.KMSImportKeyFunc(ctx, req)
	}
	return &service.ImportKeyResponse{}, nil
}

// KMSGetKey mocks the KMSGetKey method.
func (m *MockKMSClient) KMSGetKey(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
	if m.KMSGetKeyFunc != nil {
		return m.KMSGetKeyFunc(ctx, req)
	}
	return &service.GetKeyResponse{}, nil
}

// KMSListKeys mocks the KMSListKeys method.
func (m *MockKMSClient) KMSListKeys(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error) {
	if m.KMSListKeysFunc != nil {
		return m.KMSListKeysFunc(ctx, req)
	}
	return &service.ListKeysResponse{}, nil
}

// KMSRotateKey mocks the KMSRotateKey method.
func (m *MockKMSClient) KMSRotateKey(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
	if m.KMSRotateKeyFunc != nil {
		return m.KMSRotateKeyFunc(ctx, req)
	}
	return &service.RotateKeyResponse{}, nil
}

// KMSDeleteKey mocks the KMSDeleteKey method.
func (m *MockKMSClient) KMSDeleteKey(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
	if m.KMSDeleteKeyFunc != nil {
		return m.KMSDeleteKeyFunc(ctx, req)
	}
	return &service.DeleteKeyResponse{}, nil
}

// KMSEncrypt mocks the KMSEncrypt method.
func (m *MockKMSClient) KMSEncrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	if m.KMSEncryptFunc != nil {
		return m.KMSEncryptFunc(ctx, req)
	}
	return &service.EncryptResponse{}, nil
}

// KMSDecrypt mocks the KMSDecrypt method.
func (m *MockKMSClient) KMSDecrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	if m.KMSDecryptFunc != nil {
		return m.KMSDecryptFunc(ctx, req)
	}
	return &service.DecryptResponse{}, nil
}

// KMSUpdateKeyStatus mocks the KMSUpdateKeyStatus method.
func (m *MockKMSClient) KMSUpdateKeyStatus(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
	if m.KMSUpdateKeyStatusFunc != nil {
		return m.KMSUpdateKeyStatusFunc(ctx, req)
	}
	return nil, nil
}

// KMSListKeyVersions mocks the KMSListKeyVersions method.
func (m *MockKMSClient) KMSListKeyVersions(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
	if m.KMSListKeyVersionsFunc != nil {
		return m.KMSListKeyVersionsFunc(ctx, req)
	}
	return nil, nil
}

// KMSExportKey mocks the KMSExportKey method.
func (m *MockKMSClient) KMSExportKey(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
	if m.KMSExportKeyFunc != nil {
		return m.KMSExportKeyFunc(ctx, req)
	}
	return nil, nil
}

// KMSUnwrapKey mocks the KMSUnwrapKey method.
func (m *MockKMSClient) KMSUnwrapKey(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
	if m.KMSUnwrapKeyFunc != nil {
		return m.KMSUnwrapKeyFunc(ctx, req)
	}
	return nil, nil
}

// KMSSetKeySafety mocks the KMSSetKeySafety method.
func (m *MockKMSClient) KMSSetKeySafety(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
	if m.KMSSetKeySafetyFunc != nil {
		return m.KMSSetKeySafetyFunc(ctx, req)
	}
	return nil, nil
}

// KMSClearKeySafety mocks the KMSClearKeySafety method.
func (m *MockKMSClient) KMSClearKeySafety(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
	if m.KMSClearKeySafetyFunc != nil {
		return m.KMSClearKeySafetyFunc(ctx, req)
	}
	return nil, nil
}

// Close mocks the Close method.
func (m *MockKMSClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// MockSealClient is a mock implementation of SealClient for testing.
type MockSealClient struct {
	SealStatusFunc func(ctx context.Context) (*service.SealStatusResponse, error)
	SealInitFunc   func(ctx context.Context, req *service.SealInitRequest) (*service.SealInitResponse, error)
	UnsealFunc     func(ctx context.Context, req *service.UnsealRequest) (*service.UnsealResponse, error)
	SealFunc       func(ctx context.Context) error
	RekeyFunc      func(ctx context.Context, req *service.RekeyRequest) (*service.RekeyResponse, error)
	CloseFunc      func() error
}

// SealStatus mocks the SealStatus method.
func (m *MockSealClient) SealStatus(ctx context.Context) (*service.SealStatusResponse, error) {
	if m.SealStatusFunc != nil {
		return m.SealStatusFunc(ctx)
	}
	// Return unimplemented error by default since seal service is not yet implemented
	return nil, errors.Newf(errors.ErrUnimplemented, "seal.status", "Seal service not yet implemented")
}

// SealInit mocks the SealInit method.
func (m *MockSealClient) SealInit(ctx context.Context, req *service.SealInitRequest) (*service.SealInitResponse, error) {
	if m.SealInitFunc != nil {
		return m.SealInitFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "seal.init", "Seal service not yet implemented")
}

// Unseal mocks the Unseal method.
func (m *MockSealClient) Unseal(ctx context.Context, req *service.UnsealRequest) (*service.UnsealResponse, error) {
	if m.UnsealFunc != nil {
		return m.UnsealFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "seal.unseal", "Seal service not yet implemented")
}

// Seal mocks the Seal method.
func (m *MockSealClient) Seal(ctx context.Context) error {
	if m.SealFunc != nil {
		return m.SealFunc(ctx)
	}
	return errors.Newf(errors.ErrUnimplemented, "seal.seal", "Seal service not yet implemented")
}

// Rekey mocks the Rekey method.
func (m *MockSealClient) Rekey(ctx context.Context, req *service.RekeyRequest) (*service.RekeyResponse, error) {
	if m.RekeyFunc != nil {
		return m.RekeyFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "seal.rekey", "Seal service not yet implemented")
}

// Close mocks the Close method.
func (m *MockSealClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// MockSecretsClient is a mock implementation of SecretsClient for testing.
type MockSecretsClient struct {
	StoreSecretFunc          func(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error)
	GetSecretFunc            func(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error)
	ListSecretsFunc          func(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error)
	DeleteSecretFunc         func(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error)
	ListSecretVersionsFunc   func(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error)
	RollbackSecretFunc       func(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error)
	RotateSecretFunc         func(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error)
	UpdateSecretStatusFunc   func(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error)
	GetSecretsByPrefixFunc   func(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error)
	WatchSecretsByPrefixFunc func(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error)
	CloseFunc                func() error
}

// StoreSecret mocks the StoreSecret method.
func (m *MockSecretsClient) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	if m.StoreSecretFunc != nil {
		return m.StoreSecretFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "secrets.store", "Secrets service not yet implemented")
}

// GetSecret mocks the GetSecret method.
func (m *MockSecretsClient) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	if m.GetSecretFunc != nil {
		return m.GetSecretFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "secrets.get", "Secrets service not yet implemented")
}

// ListSecrets mocks the ListSecrets method.
func (m *MockSecretsClient) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	if m.ListSecretsFunc != nil {
		return m.ListSecretsFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "secrets.list", "Secrets service not yet implemented")
}

// DeleteSecret mocks the DeleteSecret method.
func (m *MockSecretsClient) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	if m.DeleteSecretFunc != nil {
		return m.DeleteSecretFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "secrets.delete", "Secrets service not yet implemented")
}

// ListSecretVersions mocks the ListSecretVersions method.
func (m *MockSecretsClient) ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	if m.ListSecretVersionsFunc != nil {
		return m.ListSecretVersionsFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "secrets.versions", "Secrets service not yet implemented")
}

// RollbackSecret mocks the RollbackSecret method.
func (m *MockSecretsClient) RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	if m.RollbackSecretFunc != nil {
		return m.RollbackSecretFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "secrets.rollback", "Secrets service not yet implemented")
}

// RotateSecret mocks the RotateSecret method.
func (m *MockSecretsClient) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	if m.RotateSecretFunc != nil {
		return m.RotateSecretFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "secrets.rotate", "Secrets service not yet implemented")
}

// UpdateSecretStatus mocks the UpdateSecretStatus method.
func (m *MockSecretsClient) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	if m.UpdateSecretStatusFunc != nil {
		return m.UpdateSecretStatusFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "secrets.updateStatus", "Secrets service not yet implemented")
}

// Close mocks the Close method.
func (m *MockSecretsClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// GetSecretsByPrefix mocks the GetSecretsByPrefix method.
func (m *MockSecretsClient) GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	if m.GetSecretsByPrefixFunc != nil {
		return m.GetSecretsByPrefixFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "secrets.getByPrefix", "Secrets service not yet implemented")
}

// WatchSecretsByPrefix mocks the WatchSecretsByPrefix method.
func (m *MockSecretsClient) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	if m.WatchSecretsByPrefixFunc != nil {
		return m.WatchSecretsByPrefixFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "secrets.watchByPrefix", "Secrets service not yet implemented")
}

// MockTimeSeriesClient is a mock implementation of TimeSeriesClient for testing.
type MockTimeSeriesClient struct {
	TSWritePointFunc         func(ctx context.Context, req *cli.WritePointRequest) (*cli.WritePointResponse, error)
	TSWriteBatchFunc         func(ctx context.Context, req *cli.WriteBatchRequest) (*cli.WriteBatchResponse, error)
	TSQueryFunc              func(ctx context.Context, req *cli.TSQueryRequest) (*cli.TSQueryResponse, error)
	TSDeleteSeriesFunc       func(ctx context.Context, req *cli.TSDeleteRequest) (*cli.TSDeleteResponse, error)
	TSGetRetentionPolicyFunc func(ctx context.Context, req *cli.TSGetRetentionPolicyRequest) (*cli.TSGetRetentionPolicyResponse, error)
	TSSetRetentionPolicyFunc func(ctx context.Context, req *cli.TSSetRetentionPolicyRequest) (*cli.TSSetRetentionPolicyResponse, error)
	TSListMetricsFunc        func(ctx context.Context, req *cli.TSListMetricsRequest) (*cli.TSListMetricsResponse, error)
	CloseFunc                func() error
}

// TSWritePoint mocks the TSWritePoint method.
func (m *MockTimeSeriesClient) TSWritePoint(ctx context.Context, req *cli.WritePointRequest) (*cli.WritePointResponse, error) {
	if m.TSWritePointFunc != nil {
		return m.TSWritePointFunc(ctx, req)
	}
	return &cli.WritePointResponse{}, nil
}

// TSWriteBatch mocks the TSWriteBatch method.
func (m *MockTimeSeriesClient) TSWriteBatch(ctx context.Context, req *cli.WriteBatchRequest) (*cli.WriteBatchResponse, error) {
	if m.TSWriteBatchFunc != nil {
		return m.TSWriteBatchFunc(ctx, req)
	}
	return &cli.WriteBatchResponse{}, nil
}

// TSQuery mocks the TSQuery method.
func (m *MockTimeSeriesClient) TSQuery(ctx context.Context, req *cli.TSQueryRequest) (*cli.TSQueryResponse, error) {
	if m.TSQueryFunc != nil {
		return m.TSQueryFunc(ctx, req)
	}
	return &cli.TSQueryResponse{}, nil
}

// TSDeleteSeries mocks the TSDeleteSeries method.
func (m *MockTimeSeriesClient) TSDeleteSeries(ctx context.Context, req *cli.TSDeleteRequest) (*cli.TSDeleteResponse, error) {
	if m.TSDeleteSeriesFunc != nil {
		return m.TSDeleteSeriesFunc(ctx, req)
	}
	return &cli.TSDeleteResponse{}, nil
}

// TSGetRetentionPolicy mocks the TSGetRetentionPolicy method.
func (m *MockTimeSeriesClient) TSGetRetentionPolicy(ctx context.Context, req *cli.TSGetRetentionPolicyRequest) (*cli.TSGetRetentionPolicyResponse, error) {
	if m.TSGetRetentionPolicyFunc != nil {
		return m.TSGetRetentionPolicyFunc(ctx, req)
	}
	return &cli.TSGetRetentionPolicyResponse{}, nil
}

// TSSetRetentionPolicy mocks the TSSetRetentionPolicy method.
func (m *MockTimeSeriesClient) TSSetRetentionPolicy(ctx context.Context, req *cli.TSSetRetentionPolicyRequest) (*cli.TSSetRetentionPolicyResponse, error) {
	if m.TSSetRetentionPolicyFunc != nil {
		return m.TSSetRetentionPolicyFunc(ctx, req)
	}
	return &cli.TSSetRetentionPolicyResponse{}, nil
}

// TSListMetrics mocks the TSListMetrics method.
func (m *MockTimeSeriesClient) TSListMetrics(ctx context.Context, req *cli.TSListMetricsRequest) (*cli.TSListMetricsResponse, error) {
	if m.TSListMetricsFunc != nil {
		return m.TSListMetricsFunc(ctx, req)
	}
	return &cli.TSListMetricsResponse{}, nil
}

// Close mocks the Close method.
func (m *MockTimeSeriesClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// Compile-time assertion that mocks implement the interfaces.
var (
	_ CryptoClient     = (*MockCryptoClient)(nil)
	_ KMSClient        = (*MockKMSClient)(nil)
	_ SealClient       = (*MockSealClient)(nil)
	_ SecretsClient    = (*MockSecretsClient)(nil)
	_ TimeSeriesClient = (*MockTimeSeriesClient)(nil)
)

// MockBackendClient is a mock implementation of BackendClient for testing.
type MockBackendClient struct {
	KMSBackendStatusFunc func(ctx context.Context) (*service.BackendStatusResponse, error)
	KMSBackendTestFunc   func(ctx context.Context) (*service.BackendTestResponse, error)
	CloseFunc            func() error
}

// KMSBackendStatus mocks the BackendStatus method.
func (m *MockBackendClient) KMSBackendStatus(ctx context.Context) (*service.BackendStatusResponse, error) {
	if m.KMSBackendStatusFunc != nil {
		return m.KMSBackendStatusFunc(ctx)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "backend.status", "Backend service not yet implemented")
}

// KMSBackendTest mocks the BackendTest method.
func (m *MockBackendClient) KMSBackendTest(ctx context.Context) (*service.BackendTestResponse, error) {
	if m.KMSBackendTestFunc != nil {
		return m.KMSBackendTestFunc(ctx)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "backend.test", "Backend service not yet implemented")
}

// Close mocks the Close method.
func (m *MockBackendClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// MockSafetyClient is a mock implementation of SafetyClient for testing.
type MockSafetyClient struct {
	KMSSafetyCheckFunc  func(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error)
	KMSSafetyReportFunc func(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error)
	CloseFunc           func() error
}

// KMSSafetyCheck mocks the SafetyCheck method.
func (m *MockSafetyClient) KMSSafetyCheck(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
	if m.KMSSafetyCheckFunc != nil {
		return m.KMSSafetyCheckFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "safety.check", "Safety service not yet implemented")
}

// KMSSafetyReport mocks the SafetyReport method.
func (m *MockSafetyClient) KMSSafetyReport(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
	if m.KMSSafetyReportFunc != nil {
		return m.KMSSafetyReportFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "safety.report", "Safety service not yet implemented")
}

// Close mocks the Close method.
func (m *MockSafetyClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// MockBackupClient is a mock implementation of BackupClient for testing.
type MockBackupClient struct {
	CreateBackupFunc  func(ctx context.Context, req *BackupCreateRequest) (*cli.BackupCreateResponse, error)
	ListBackupsFunc   func(ctx context.Context, req *BackupListRequest) (*cli.BackupListResponse, error)
	RestoreBackupFunc func(ctx context.Context, req *BackupRestoreRequest) (*cli.BackupRestoreResponse, error)
	DeleteBackupFunc  func(ctx context.Context, req *BackupDeleteRequest) (*cli.BackupDeleteResponse, error)
	VerifyBackupFunc  func(ctx context.Context, req *BackupVerifyRequest) (*cli.BackupVerifyResponse, error)
	CloseFunc         func() error
}

// CreateBackup mocks the CreateBackup method.
func (m *MockBackupClient) CreateBackup(ctx context.Context, req *BackupCreateRequest) (*cli.BackupCreateResponse, error) {
	if m.CreateBackupFunc != nil {
		return m.CreateBackupFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "backup.create", "Backup service not yet implemented via gRPC")
}

// ListBackups mocks the ListBackups method.
func (m *MockBackupClient) ListBackups(ctx context.Context, req *BackupListRequest) (*cli.BackupListResponse, error) {
	if m.ListBackupsFunc != nil {
		return m.ListBackupsFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "backup.list", "Backup service not yet implemented via gRPC")
}

// RestoreBackup mocks the RestoreBackup method.
func (m *MockBackupClient) RestoreBackup(ctx context.Context, req *BackupRestoreRequest) (*cli.BackupRestoreResponse, error) {
	if m.RestoreBackupFunc != nil {
		return m.RestoreBackupFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "backup.restore", "Backup service not yet implemented via gRPC")
}

// DeleteBackup mocks the DeleteBackup method.
func (m *MockBackupClient) DeleteBackup(ctx context.Context, req *BackupDeleteRequest) (*cli.BackupDeleteResponse, error) {
	if m.DeleteBackupFunc != nil {
		return m.DeleteBackupFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "backup.delete", "Backup service not yet implemented via gRPC")
}

// VerifyBackup mocks the VerifyBackup method.
func (m *MockBackupClient) VerifyBackup(ctx context.Context, req *BackupVerifyRequest) (*cli.BackupVerifyResponse, error) {
	if m.VerifyBackupFunc != nil {
		return m.VerifyBackupFunc(ctx, req)
	}
	return &cli.BackupVerifyResponse{
		BackupID:      req.BackupID,
		Valid:         true,
		ChecksumValid: true,
		ShardsValid:   3,
		Timestamp:     time.Now(),
	}, nil
}

// Close mocks the Close method.
func (m *MockBackupClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// Compile-time assertion that mocks implement the interfaces.
var (
	_ BackendClient = (*MockBackendClient)(nil)
	_ SafetyClient  = (*MockSafetyClient)(nil)
	_ BackupClient  = (*MockBackupClient)(nil)
)
