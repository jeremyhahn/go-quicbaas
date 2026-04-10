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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Crypto – closed-client (ensureConnected) error paths
// =============================================================================

func TestClient_KMSSign_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSSign(context.Background(), &service.SignRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSVerify_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSVerify(context.Background(), &service.VerifyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSHash_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSHash(context.Background(), &service.HashRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSGenerateHMAC_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSGenerateHMAC(context.Background(), &service.HMACRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSVerifyHMAC_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSVerifyHMAC(context.Background(), &service.VerifyHMACRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSRewrap_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSRewrap(context.Background(), &service.RewrapRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSGenerateRandom_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSGenerateRandom(context.Background(), &service.GenerateRandomRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

// =============================================================================
// KMS Key Operations – closed-client (ensureConnected) error paths
// =============================================================================

func TestClient_KMSGenerateKey_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSGenerateKey(context.Background(), &service.GenerateKeyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSImportKey_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSImportKey(context.Background(), &service.ImportKeyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSGetKey_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSGetKey(context.Background(), &service.GetKeyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSRotateKey_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSRotateKey(context.Background(), &service.RotateKeyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSDeleteKey_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSDeleteKey(context.Background(), &service.DeleteKeyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSEncrypt_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSEncrypt(context.Background(), &service.EncryptRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSDecrypt_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSDecrypt(context.Background(), &service.DecryptRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSGenerateDataKey_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSGenerateDataKey(context.Background(), &service.GenerateDataKeyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSUpdateKeyStatus_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSUpdateKeyStatus(context.Background(), &service.UpdateKeyStatusRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSListKeyVersions_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSListKeyVersions(context.Background(), &service.ListKeyVersionsRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSExportKey_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSExportKey(context.Background(), &service.ExportKeyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSUnwrapKey_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSUnwrapKey(context.Background(), &service.UnwrapKeyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSSetKeySafety_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSSetKeySafety(context.Background(), &service.SetKeySafetyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSClearKeySafety_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSClearKeySafety(context.Background(), &service.ClearKeySafetyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

// =============================================================================
// KMS DEK – closed-client (ensureConnected) error paths
// =============================================================================

func TestClient_KMSCreateDEK_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSCreateDEK(context.Background(), &service.CreateDEKRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSGetDEK_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSGetDEK(context.Background(), &service.GetDEKRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSListDEKs_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSListDEKs(context.Background(), &service.ListDEKsRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSRotateDEK_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSRotateDEK(context.Background(), &service.RotateDEKRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSDeleteDEK_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSDeleteDEK(context.Background(), &service.DeleteDEKRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSGetDEKStatus_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSGetDEKStatus(context.Background(), &service.GetDEKStatusRequest{Path: "test"})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSSetDefaultDEK_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSSetDefaultDEK(context.Background(), &service.SetDefaultDEKRequest{Path: "test"})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSSetDEKSafety_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSSetDEKSafety(context.Background(), &service.SetDEKSafetyRequest{Path: "test"})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSClearDEKSafety_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSClearDEKSafety(context.Background(), &service.ClearDEKSafetyRequest{Path: "test"})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSSafetyCheck_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSSafetyCheck(context.Background(), &service.SafetyCheckRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSSafetyReport_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSSafetyReport(context.Background(), &service.SafetyReportRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSStartReencryption_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSStartReencryption(context.Background(), &service.StartReencryptionRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSGetReencryptionProgress_ClosedClient(t *testing.T) {
	t.Parallel()
	client := newClosedClient()
	resp, err := client.KMSGetReencryptionProgress(context.Background(), &service.GetReencryptionProgressRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}
