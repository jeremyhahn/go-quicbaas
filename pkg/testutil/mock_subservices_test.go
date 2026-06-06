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
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ===========================================================================
// Secrets Service Methods
// ===========================================================================

func TestMockQuicBaaS_StoreSecret_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.StoreSecretRequest{}
	expected := &service.StoreSecretResponse{}
	m.On("StoreSecret", ctx, req).Return(expected, nil)

	resp, err := m.StoreSecret(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_StoreSecret_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.StoreSecretRequest{}
	m.On("StoreSecret", ctx, req).Return((*service.StoreSecretResponse)(nil), errTest)

	resp, err := m.StoreSecret(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetSecret_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetSecretRequest{}
	expected := &service.GetSecretResponse{}
	m.On("GetSecret", ctx, req).Return(expected, nil)

	resp, err := m.GetSecret(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetSecret_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetSecretRequest{}
	m.On("GetSecret", ctx, req).Return((*service.GetSecretResponse)(nil), errTest)

	resp, err := m.GetSecret(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListSecrets_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListSecretsRequest{}
	expected := &service.ListSecretsResponse{}
	m.On("ListSecrets", ctx, req).Return(expected, nil)

	resp, err := m.ListSecrets(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListSecrets_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListSecretsRequest{}
	m.On("ListSecrets", ctx, req).Return((*service.ListSecretsResponse)(nil), errTest)

	resp, err := m.ListSecrets(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteSecret_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteSecretRequest{}
	expected := &service.DeleteSecretResponse{}
	m.On("DeleteSecret", ctx, req).Return(expected, nil)

	resp, err := m.DeleteSecret(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteSecret_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteSecretRequest{}
	m.On("DeleteSecret", ctx, req).Return((*service.DeleteSecretResponse)(nil), errTest)

	resp, err := m.DeleteSecret(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetSecretVersion_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetSecretVersionRequest{}
	expected := &service.GetSecretVersionResponse{}
	m.On("GetSecretVersion", ctx, req).Return(expected, nil)

	resp, err := m.GetSecretVersion(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetSecretVersion_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetSecretVersionRequest{}
	m.On("GetSecretVersion", ctx, req).Return((*service.GetSecretVersionResponse)(nil), errTest)

	resp, err := m.GetSecretVersion(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListSecretVersions_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListSecretVersionsRequest{}
	expected := &service.ListSecretVersionsResponse{}
	m.On("ListSecretVersions", ctx, req).Return(expected, nil)

	resp, err := m.ListSecretVersions(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListSecretVersions_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListSecretVersionsRequest{}
	m.On("ListSecretVersions", ctx, req).Return((*service.ListSecretVersionsResponse)(nil), errTest)

	resp, err := m.ListSecretVersions(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RollbackSecret_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RollbackSecretRequest{}
	expected := &service.RollbackSecretResponse{}
	m.On("RollbackSecret", ctx, req).Return(expected, nil)

	resp, err := m.RollbackSecret(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RollbackSecret_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RollbackSecretRequest{}
	m.On("RollbackSecret", ctx, req).Return((*service.RollbackSecretResponse)(nil), errTest)

	resp, err := m.RollbackSecret(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RotateSecret_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RotateSecretRequest{}
	expected := &service.RotateSecretResponse{}
	m.On("RotateSecret", ctx, req).Return(expected, nil)

	resp, err := m.RotateSecret(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RotateSecret_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RotateSecretRequest{}
	m.On("RotateSecret", ctx, req).Return((*service.RotateSecretResponse)(nil), errTest)

	resp, err := m.RotateSecret(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetRotationPolicy_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SetRotationPolicyRequest{}
	expected := &service.SetRotationPolicyResponse{}
	m.On("SetRotationPolicy", ctx, req).Return(expected, nil)

	resp, err := m.SetRotationPolicy(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetRotationPolicy_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SetRotationPolicyRequest{}
	m.On("SetRotationPolicy", ctx, req).Return((*service.SetRotationPolicyResponse)(nil), errTest)

	resp, err := m.SetRotationPolicy(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetRotationPolicy_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetRotationPolicyRequest{}
	expected := &service.GetRotationPolicyResponse{}
	m.On("GetRotationPolicy", ctx, req).Return(expected, nil)

	resp, err := m.GetRotationPolicy(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetRotationPolicy_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetRotationPolicyRequest{}
	m.On("GetRotationPolicy", ctx, req).Return((*service.GetRotationPolicyResponse)(nil), errTest)

	resp, err := m.GetRotationPolicy(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_UpdateSecretStatus_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UpdateSecretStatusRequest{}
	expected := &service.UpdateSecretStatusResponse{}
	m.On("UpdateSecretStatus", ctx, req).Return(expected, nil)

	resp, err := m.UpdateSecretStatus(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_UpdateSecretStatus_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UpdateSecretStatusRequest{}
	m.On("UpdateSecretStatus", ctx, req).Return((*service.UpdateSecretStatusResponse)(nil), errTest)

	resp, err := m.UpdateSecretStatus(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetSecretsByPrefix_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetSecretsByPrefixRequest{}
	expected := &service.GetSecretsByPrefixResponse{}
	m.On("GetSecretsByPrefix", ctx, req).Return(expected, nil)

	resp, err := m.GetSecretsByPrefix(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetSecretsByPrefix_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetSecretsByPrefixRequest{}
	m.On("GetSecretsByPrefix", ctx, req).Return((*service.GetSecretsByPrefixResponse)(nil), errTest)

	resp, err := m.GetSecretsByPrefix(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_WatchSecretsByPrefix_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.WatchSecretsRequest{}
	ch := make(chan *service.SecretChange, 1)
	var readCh <-chan *service.SecretChange = ch
	m.On("WatchSecretsByPrefix", ctx, req).Return(readCh, nil)

	result, err := m.WatchSecretsByPrefix(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, readCh, result)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_WatchSecretsByPrefix_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.WatchSecretsRequest{}
	m.On("WatchSecretsByPrefix", ctx, req).Return((<-chan *service.SecretChange)(nil), errTest)

	result, err := m.WatchSecretsByPrefix(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, result)
	m.AssertExpectations(t)
}

// ===========================================================================
// KMS Service Methods
// ===========================================================================

func TestMockQuicBaaS_GetSupportedAlgorithms_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	algos := []string{"AES-256", "RSA-2048"}
	m.On("GetSupportedAlgorithms", ctx).Return(algos, nil)

	resp, err := m.GetSupportedAlgorithms(ctx)
	assert.NoError(t, err)
	assert.Equal(t, algos, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetSupportedAlgorithms_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	m.On("GetSupportedAlgorithms", ctx).Return([]string(nil), errTest)

	resp, err := m.GetSupportedAlgorithms(ctx)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GenerateKey_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GenerateKeyRequest{}
	expected := &service.GenerateKeyResponse{}
	m.On("GenerateKey", ctx, req).Return(expected, nil)

	resp, err := m.GenerateKey(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GenerateKey_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GenerateKeyRequest{}
	m.On("GenerateKey", ctx, req).Return((*service.GenerateKeyResponse)(nil), errTest)

	resp, err := m.GenerateKey(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ImportKey_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ImportKeyRequest{}
	expected := &service.ImportKeyResponse{}
	m.On("ImportKey", ctx, req).Return(expected, nil)

	resp, err := m.ImportKey(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ImportKey_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ImportKeyRequest{}
	m.On("ImportKey", ctx, req).Return((*service.ImportKeyResponse)(nil), errTest)

	resp, err := m.ImportKey(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetKey_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetKeyRequest{}
	expected := &service.GetKeyResponse{}
	m.On("GetKey", ctx, req).Return(expected, nil)

	resp, err := m.GetKey(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetKey_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetKeyRequest{}
	m.On("GetKey", ctx, req).Return((*service.GetKeyResponse)(nil), errTest)

	resp, err := m.GetKey(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListKeys_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListKeysRequest{}
	expected := &service.ListKeysResponse{}
	m.On("ListKeys", ctx, req).Return(expected, nil)

	resp, err := m.ListKeys(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListKeys_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListKeysRequest{}
	m.On("ListKeys", ctx, req).Return((*service.ListKeysResponse)(nil), errTest)

	resp, err := m.ListKeys(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RotateKey_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RotateKeyRequest{}
	expected := &service.RotateKeyResponse{}
	m.On("RotateKey", ctx, req).Return(expected, nil)

	resp, err := m.RotateKey(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RotateKey_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RotateKeyRequest{}
	m.On("RotateKey", ctx, req).Return((*service.RotateKeyResponse)(nil), errTest)

	resp, err := m.RotateKey(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RevokeKey_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RevokeKeyRequest{}
	expected := &service.RevokeKeyResponse{}
	m.On("RevokeKey", ctx, req).Return(expected, nil)

	resp, err := m.RevokeKey(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RevokeKey_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RevokeKeyRequest{}
	m.On("RevokeKey", ctx, req).Return((*service.RevokeKeyResponse)(nil), errTest)

	resp, err := m.RevokeKey(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteKey_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteKeyRequest{}
	expected := &service.DeleteKeyResponse{}
	m.On("DeleteKey", ctx, req).Return(expected, nil)

	resp, err := m.DeleteKey(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteKey_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteKeyRequest{}
	m.On("DeleteKey", ctx, req).Return((*service.DeleteKeyResponse)(nil), errTest)

	resp, err := m.DeleteKey(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ===========================================================================
// Crypto Service Methods
// ===========================================================================

func TestMockQuicBaaS_Encrypt_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.EncryptRequest{}
	expected := &service.EncryptResponse{}
	m.On("Encrypt", ctx, req).Return(expected, nil)

	resp, err := m.Encrypt(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Encrypt_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.EncryptRequest{}
	m.On("Encrypt", ctx, req).Return((*service.EncryptResponse)(nil), errTest)

	resp, err := m.Encrypt(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Decrypt_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DecryptRequest{}
	expected := &service.DecryptResponse{}
	m.On("Decrypt", ctx, req).Return(expected, nil)

	resp, err := m.Decrypt(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Decrypt_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DecryptRequest{}
	m.On("Decrypt", ctx, req).Return((*service.DecryptResponse)(nil), errTest)

	resp, err := m.Decrypt(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Sign_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SignRequest{}
	expected := &service.SignResponse{}
	m.On("Sign", ctx, req).Return(expected, nil)

	resp, err := m.Sign(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Sign_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SignRequest{}
	m.On("Sign", ctx, req).Return((*service.SignResponse)(nil), errTest)

	resp, err := m.Sign(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Verify_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.VerifyRequest{}
	expected := &service.VerifyResponse{}
	m.On("Verify", ctx, req).Return(expected, nil)

	resp, err := m.Verify(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Verify_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.VerifyRequest{}
	m.On("Verify", ctx, req).Return((*service.VerifyResponse)(nil), errTest)

	resp, err := m.Verify(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Hash_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.HashRequest{}
	expected := &service.HashResponse{}
	m.On("Hash", ctx, req).Return(expected, nil)

	resp, err := m.Hash(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Hash_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.HashRequest{}
	m.On("Hash", ctx, req).Return((*service.HashResponse)(nil), errTest)

	resp, err := m.Hash(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Rewrap_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RewrapRequest{}
	expected := &service.RewrapResponse{}
	m.On("Rewrap", ctx, req).Return(expected, nil)

	resp, err := m.Rewrap(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Rewrap_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RewrapRequest{}
	m.On("Rewrap", ctx, req).Return((*service.RewrapResponse)(nil), errTest)

	resp, err := m.Rewrap(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GenerateRandom_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GenerateRandomRequest{}
	expected := &service.GenerateRandomResponse{}
	m.On("GenerateRandom", ctx, req).Return(expected, nil)

	resp, err := m.GenerateRandom(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GenerateRandom_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GenerateRandomRequest{}
	m.On("GenerateRandom", ctx, req).Return((*service.GenerateRandomResponse)(nil), errTest)

	resp, err := m.GenerateRandom(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ===========================================================================
// Vector Service Methods
// ===========================================================================

func TestMockQuicBaaS_CreateNamespace_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CreateNamespaceRequest{}
	expected := &service.CreateNamespaceResponse{}
	m.On("CreateNamespace", ctx, req).Return(expected, nil)

	resp, err := m.CreateNamespace(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CreateNamespace_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CreateNamespaceRequest{}
	m.On("CreateNamespace", ctx, req).Return((*service.CreateNamespaceResponse)(nil), errTest)

	resp, err := m.CreateNamespace(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_UpsertVector_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UpsertVectorRequest{}
	expected := &service.UpsertVectorResponse{}
	m.On("UpsertVector", ctx, req).Return(expected, nil)

	resp, err := m.UpsertVector(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_UpsertVector_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UpsertVectorRequest{}
	m.On("UpsertVector", ctx, req).Return((*service.UpsertVectorResponse)(nil), errTest)

	resp, err := m.UpsertVector(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SearchSimilar_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SearchSimilarRequest{}
	expected := &service.SearchSimilarResponse{}
	m.On("SearchSimilar", ctx, req).Return(expected, nil)

	resp, err := m.SearchSimilar(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SearchSimilar_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SearchSimilarRequest{}
	m.On("SearchSimilar", ctx, req).Return((*service.SearchSimilarResponse)(nil), errTest)

	resp, err := m.SearchSimilar(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ===========================================================================
// Dynamic Secrets Service Methods
// ===========================================================================

func TestMockQuicBaaS_CreateRole_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CreateRoleRequest{}
	expected := &service.CreateRoleResponse{}
	m.On("CreateRole", ctx, req).Return(expected, nil)

	resp, err := m.CreateRole(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CreateRole_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CreateRoleRequest{}
	m.On("CreateRole", ctx, req).Return((*service.CreateRoleResponse)(nil), errTest)

	resp, err := m.CreateRole(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GenerateCredential_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GenerateCredentialRequest{}
	expected := &service.GenerateCredentialResponse{}
	m.On("GenerateCredential", ctx, req).Return(expected, nil)

	resp, err := m.GenerateCredential(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GenerateCredential_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GenerateCredentialRequest{}
	m.On("GenerateCredential", ctx, req).Return((*service.GenerateCredentialResponse)(nil), errTest)

	resp, err := m.GenerateCredential(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RenewLease_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RenewLeaseRequest{}
	expected := &service.RenewLeaseResponse{}
	m.On("RenewLease", ctx, req).Return(expected, nil)

	resp, err := m.RenewLease(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RenewLease_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RenewLeaseRequest{}
	m.On("RenewLease", ctx, req).Return((*service.RenewLeaseResponse)(nil), errTest)

	resp, err := m.RenewLease(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ===========================================================================
// Object Storage Service Methods
// ===========================================================================

func TestMockQuicBaaS_PutObject_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.PutObjectRequest{}
	expected := &service.PutObjectResponse{}
	m.On("PutObject", ctx, req).Return(expected, nil)

	resp, err := m.PutObject(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_PutObject_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.PutObjectRequest{}
	m.On("PutObject", ctx, req).Return((*service.PutObjectResponse)(nil), errTest)

	resp, err := m.PutObject(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetObject_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetObjectRequest{}
	expected := &service.GetObjectResponse{}
	m.On("GetObject", ctx, req).Return(expected, nil)

	resp, err := m.GetObject(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetObject_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetObjectRequest{}
	m.On("GetObject", ctx, req).Return((*service.GetObjectResponse)(nil), errTest)

	resp, err := m.GetObject(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteObject_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteObjectRequest{}
	expected := &service.DeleteObjectResponse{}
	m.On("DeleteObject", ctx, req).Return(expected, nil)

	resp, err := m.DeleteObject(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteObject_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteObjectRequest{}
	m.On("DeleteObject", ctx, req).Return((*service.DeleteObjectResponse)(nil), errTest)

	resp, err := m.DeleteObject(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ===========================================================================
// Parameter Store Service Methods
// ===========================================================================

func TestMockQuicBaaS_GetParameter_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &interfaces.GetParameterRequest{}
	expected := &interfaces.GetParameterResponse{}
	m.On("GetParameter", ctx, req).Return(expected, nil)

	resp, err := m.GetParameter(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetParameter_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &interfaces.GetParameterRequest{}
	m.On("GetParameter", ctx, req).Return((*interfaces.GetParameterResponse)(nil), errTest)

	resp, err := m.GetParameter(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetParameter_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &interfaces.SetParameterRequest{}
	expected := &interfaces.SetParameterResponse{}
	m.On("SetParameter", ctx, req).Return(expected, nil)

	resp, err := m.SetParameter(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetParameter_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &interfaces.SetParameterRequest{}
	m.On("SetParameter", ctx, req).Return((*interfaces.SetParameterResponse)(nil), errTest)

	resp, err := m.SetParameter(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ===========================================================================
// Scheduler Service Methods
// ===========================================================================

func TestMockQuicBaaS_CreateJob_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CreateSchedulerJobRequest{}
	expected := &service.CreateSchedulerJobResponse{}
	m.On("CreateJob", ctx, req).Return(expected, nil)

	resp, err := m.CreateJob(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CreateJob_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CreateSchedulerJobRequest{}
	m.On("CreateJob", ctx, req).Return((*service.CreateSchedulerJobResponse)(nil), errTest)

	resp, err := m.CreateJob(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetStatus_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	expected := &service.GetSchedulerStatusResponse{}
	m.On("GetStatus", ctx).Return(expected, nil)

	resp, err := m.GetStatus(ctx)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetStatus_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	m.On("GetStatus", ctx).Return((*service.GetSchedulerStatusResponse)(nil), errTest)

	resp, err := m.GetStatus(ctx)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ===========================================================================
// Object Transfer Service Methods
// ===========================================================================

func TestMockQuicBaaS_FetchObject_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.FetchObjectRequest{}
	expected := &service.FetchObjectResponse{}
	m.On("FetchObject", ctx, req).Return(expected, nil)

	resp, err := m.FetchObject(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_FetchObject_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.FetchObjectRequest{}
	m.On("FetchObject", ctx, req).Return((*service.FetchObjectResponse)(nil), errTest)

	resp, err := m.FetchObject(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ===========================================================================
// Object Storage Admin Methods
// ===========================================================================

func TestMockQuicBaaS_CreateBackend_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CreateBackendRequest{}
	expected := &service.CreateBackendResponse{}
	m.On("CreateBackend", ctx, req).Return(expected, nil)

	resp, err := m.CreateBackend(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CreateBackend_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CreateBackendRequest{}
	m.On("CreateBackend", ctx, req).Return((*service.CreateBackendResponse)(nil), errTest)

	resp, err := m.CreateBackend(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ===========================================================================
// KMS Backend Status Methods
// ===========================================================================

func TestMockQuicBaaS_BackendStatus_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	expected := &service.BackendStatusResponse{}
	m.On("BackendStatus", ctx).Return(expected, nil)

	resp, err := m.BackendStatus(ctx)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_BackendStatus_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	m.On("BackendStatus", ctx).Return((*service.BackendStatusResponse)(nil), errTest)

	resp, err := m.BackendStatus(ctx)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_BackendTest_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	expected := &service.BackendTestResponse{}
	m.On("BackendTest", ctx).Return(expected, nil)

	resp, err := m.BackendTest(ctx)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_BackendTest_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	m.On("BackendTest", ctx).Return((*service.BackendTestResponse)(nil), errTest)

	resp, err := m.BackendTest(ctx)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}
