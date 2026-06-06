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
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestCreateTempDir(t *testing.T) {
	t.Run("creates_valid_temp_directory", func(t *testing.T) {
		dir := CreateTempDir(t)
		require.NotEmpty(t, dir)
		assert.Contains(t, dir, "quicbaas-test-")
	})

	t.Run("returns_unique_directories", func(t *testing.T) {
		dir1 := CreateTempDir(t)
		dir2 := CreateTempDir(t)
		assert.NotEqual(t, dir1, dir2)
	})
}

func TestWaitForCondition_ConditionMetImmediately(t *testing.T) {
	err := WaitForCondition(100*time.Millisecond, 10*time.Millisecond, func() bool {
		return true
	})
	assert.NoError(t, err)
}

func TestWaitForCondition_ConditionMetAfterDelay(t *testing.T) {
	var counter atomic.Int32
	err := WaitForCondition(500*time.Millisecond, 10*time.Millisecond, func() bool {
		return counter.Add(1) >= 3
	})
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, counter.Load(), int32(3))
}

func TestWaitForCondition_Timeout(t *testing.T) {
	err := WaitForCondition(50*time.Millisecond, 10*time.Millisecond, func() bool {
		return false
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConditionTimeout))
}

func TestErrConditionTimeout_IsSentinelError(t *testing.T) {
	assert.Equal(t, "condition was not met within timeout", ErrConditionTimeout.Error())
}

// =============================================================================
// Consolidated from coverage_boost_80_test.go
// =============================================================================

// ===========================================================================
// KMS Service Methods - Additional Coverage
// ===========================================================================

func TestMockQuicBaaS_GetPublicKey_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetPublicKeyRequest{}
	expected := &service.GetPublicKeyResponse{}
	m.On("GetPublicKey", ctx, req).Return(expected, nil)

	resp, err := m.GetPublicKey(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetPublicKey_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetPublicKeyRequest{}
	m.On("GetPublicKey", ctx, req).Return((*service.GetPublicKeyResponse)(nil), errTest)

	resp, err := m.GetPublicKey(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetKeyMetadata_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetKeyMetadataRequest{}
	expected := &service.GetKeyMetadataResponse{}
	m.On("GetKeyMetadata", ctx, req).Return(expected, nil)

	resp, err := m.GetKeyMetadata(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetKeyMetadata_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetKeyMetadataRequest{}
	m.On("GetKeyMetadata", ctx, req).Return((*service.GetKeyMetadataResponse)(nil), errTest)

	resp, err := m.GetKeyMetadata(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_UpdateKeyMetadata_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UpdateKeyMetadataRequest{}
	expected := &service.UpdateKeyMetadataResponse{}
	m.On("UpdateKeyMetadata", ctx, req).Return(expected, nil)

	resp, err := m.UpdateKeyMetadata(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_UpdateKeyMetadata_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UpdateKeyMetadataRequest{}
	m.On("UpdateKeyMetadata", ctx, req).Return((*service.UpdateKeyMetadataResponse)(nil), errTest)

	resp, err := m.UpdateKeyMetadata(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_UpdateKeyStatus_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UpdateKeyStatusRequest{}
	expected := &service.UpdateKeyStatusResponse{}
	m.On("UpdateKeyStatus", ctx, req).Return(expected, nil)

	resp, err := m.UpdateKeyStatus(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_UpdateKeyStatus_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UpdateKeyStatusRequest{}
	m.On("UpdateKeyStatus", ctx, req).Return((*service.UpdateKeyStatusResponse)(nil), errTest)

	resp, err := m.UpdateKeyStatus(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListKeyVersions_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListKeyVersionsRequest{}
	expected := &service.ListKeyVersionsResponse{}
	m.On("ListKeyVersions", ctx, req).Return(expected, nil)

	resp, err := m.ListKeyVersions(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListKeyVersions_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListKeyVersionsRequest{}
	m.On("ListKeyVersions", ctx, req).Return((*service.ListKeyVersionsResponse)(nil), errTest)

	resp, err := m.ListKeyVersions(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetKeyPolicy_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SetKeyPolicyRequest{}
	expected := &service.SetKeyPolicyResponse{}
	m.On("SetKeyPolicy", ctx, req).Return(expected, nil)

	resp, err := m.SetKeyPolicy(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetKeyPolicy_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SetKeyPolicyRequest{}
	m.On("SetKeyPolicy", ctx, req).Return((*service.SetKeyPolicyResponse)(nil), errTest)

	resp, err := m.SetKeyPolicy(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetKeyPolicy_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetKeyPolicyRequest{}
	expected := &service.GetKeyPolicyResponse{}
	m.On("GetKeyPolicy", ctx, req).Return(expected, nil)

	resp, err := m.GetKeyPolicy(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetKeyPolicy_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetKeyPolicyRequest{}
	m.On("GetKeyPolicy", ctx, req).Return((*service.GetKeyPolicyResponse)(nil), errTest)

	resp, err := m.GetKeyPolicy(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CreateDEK_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CreateDEKRequest{}
	expected := &service.CreateDEKResponse{}
	m.On("CreateDEK", ctx, req).Return(expected, nil)

	resp, err := m.CreateDEK(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CreateDEK_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CreateDEKRequest{}
	m.On("CreateDEK", ctx, req).Return((*service.CreateDEKResponse)(nil), errTest)

	resp, err := m.CreateDEK(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetDEK_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetDEKRequest{}
	expected := &service.GetDEKResponse{}
	m.On("GetDEK", ctx, req).Return(expected, nil)

	resp, err := m.GetDEK(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetDEK_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetDEKRequest{}
	m.On("GetDEK", ctx, req).Return((*service.GetDEKResponse)(nil), errTest)

	resp, err := m.GetDEK(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListDEKs_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListDEKsRequest{}
	expected := &service.ListDEKsResponse{}
	m.On("ListDEKs", ctx, req).Return(expected, nil)

	resp, err := m.ListDEKs(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListDEKs_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListDEKsRequest{}
	m.On("ListDEKs", ctx, req).Return((*service.ListDEKsResponse)(nil), errTest)

	resp, err := m.ListDEKs(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RotateDEK_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RotateDEKRequest{}
	expected := &service.RotateDEKResponse{}
	m.On("RotateDEK", ctx, req).Return(expected, nil)

	resp, err := m.RotateDEK(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RotateDEK_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RotateDEKRequest{}
	m.On("RotateDEK", ctx, req).Return((*service.RotateDEKResponse)(nil), errTest)

	resp, err := m.RotateDEK(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteDEK_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteDEKRequest{}
	expected := &service.DeleteDEKResponse{}
	m.On("DeleteDEK", ctx, req).Return(expected, nil)

	resp, err := m.DeleteDEK(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteDEK_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteDEKRequest{}
	m.On("DeleteDEK", ctx, req).Return((*service.DeleteDEKResponse)(nil), errTest)

	resp, err := m.DeleteDEK(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetDEKStatus_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetDEKStatusRequest{}
	expected := &service.GetDEKStatusResponse{}
	m.On("GetDEKStatus", ctx, req).Return(expected, nil)

	resp, err := m.GetDEKStatus(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetDEKStatus_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetDEKStatusRequest{}
	m.On("GetDEKStatus", ctx, req).Return((*service.GetDEKStatusResponse)(nil), errTest)

	resp, err := m.GetDEKStatus(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetDefaultDEK_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SetDefaultDEKRequest{}
	expected := &service.SetDefaultDEKResponse{}
	m.On("SetDefaultDEK", ctx, req).Return(expected, nil)

	resp, err := m.SetDefaultDEK(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetDefaultDEK_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SetDefaultDEKRequest{}
	m.On("SetDefaultDEK", ctx, req).Return((*service.SetDefaultDEKResponse)(nil), errTest)

	resp, err := m.SetDefaultDEK(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetDEKSafety_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SetDEKSafetyRequest{}
	expected := &service.SetDEKSafetyResponse{}
	m.On("SetDEKSafety", ctx, req).Return(expected, nil)

	resp, err := m.SetDEKSafety(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetDEKSafety_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SetDEKSafetyRequest{}
	m.On("SetDEKSafety", ctx, req).Return((*service.SetDEKSafetyResponse)(nil), errTest)

	resp, err := m.SetDEKSafety(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ClearDEKSafety_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ClearDEKSafetyRequest{}
	expected := &service.ClearDEKSafetyResponse{}
	m.On("ClearDEKSafety", ctx, req).Return(expected, nil)

	resp, err := m.ClearDEKSafety(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ClearDEKSafety_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ClearDEKSafetyRequest{}
	m.On("ClearDEKSafety", ctx, req).Return((*service.ClearDEKSafetyResponse)(nil), errTest)

	resp, err := m.ClearDEKSafety(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_StartReencryption_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.StartReencryptionRequest{}
	expected := &service.StartReencryptionResponse{}
	m.On("StartReencryption", ctx, req).Return(expected, nil)

	resp, err := m.StartReencryption(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_StartReencryption_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.StartReencryptionRequest{}
	m.On("StartReencryption", ctx, req).Return((*service.StartReencryptionResponse)(nil), errTest)

	resp, err := m.StartReencryption(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetReencryptionProgress_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetReencryptionProgressRequest{}
	expected := &service.GetReencryptionProgressResponse{}
	m.On("GetReencryptionProgress", ctx, req).Return(expected, nil)

	resp, err := m.GetReencryptionProgress(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetReencryptionProgress_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetReencryptionProgressRequest{}
	m.On("GetReencryptionProgress", ctx, req).Return((*service.GetReencryptionProgressResponse)(nil), errTest)

	resp, err := m.GetReencryptionProgress(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ExportKey_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ExportKeyRequest{}
	expected := &service.ExportKeyResponse{}
	m.On("ExportKey", ctx, req).Return(expected, nil)

	resp, err := m.ExportKey(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ExportKey_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ExportKeyRequest{}
	m.On("ExportKey", ctx, req).Return((*service.ExportKeyResponse)(nil), errTest)

	resp, err := m.ExportKey(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_UnwrapKey_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UnwrapKeyRequest{}
	expected := &service.UnwrapKeyResponse{}
	m.On("UnwrapKey", ctx, req).Return(expected, nil)

	resp, err := m.UnwrapKey(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_UnwrapKey_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UnwrapKeyRequest{}
	m.On("UnwrapKey", ctx, req).Return((*service.UnwrapKeyResponse)(nil), errTest)

	resp, err := m.UnwrapKey(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetKeySafety_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SetKeySafetyRequest{}
	expected := &service.SetKeySafetyResponse{}
	m.On("SetKeySafety", ctx, req).Return(expected, nil)

	resp, err := m.SetKeySafety(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetKeySafety_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SetKeySafetyRequest{}
	m.On("SetKeySafety", ctx, req).Return((*service.SetKeySafetyResponse)(nil), errTest)

	resp, err := m.SetKeySafety(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ClearKeySafety_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ClearKeySafetyRequest{}
	expected := &service.ClearKeySafetyResponse{}
	m.On("ClearKeySafety", ctx, req).Return(expected, nil)

	resp, err := m.ClearKeySafety(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ClearKeySafety_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ClearKeySafetyRequest{}
	m.On("ClearKeySafety", ctx, req).Return((*service.ClearKeySafetyResponse)(nil), errTest)

	resp, err := m.ClearKeySafety(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SafetyCheck_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SafetyCheckRequest{}
	expected := &service.SafetyCheckResponse{}
	m.On("SafetyCheck", ctx, req).Return(expected, nil)

	resp, err := m.SafetyCheck(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SafetyCheck_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SafetyCheckRequest{}
	m.On("SafetyCheck", ctx, req).Return((*service.SafetyCheckResponse)(nil), errTest)

	resp, err := m.SafetyCheck(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SafetyReport_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SafetyReportRequest{}
	expected := &service.SafetyReportResponse{}
	m.On("SafetyReport", ctx, req).Return(expected, nil)

	resp, err := m.SafetyReport(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SafetyReport_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SafetyReportRequest{}
	m.On("SafetyReport", ctx, req).Return((*service.SafetyReportResponse)(nil), errTest)

	resp, err := m.SafetyReport(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ===========================================================================
// Crypto Service Methods - Additional Coverage
// ===========================================================================

func TestMockQuicBaaS_EncryptBatch_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.EncryptBatchRequest{}
	expected := &service.EncryptBatchResponse{}
	m.On("EncryptBatch", ctx, req).Return(expected, nil)

	resp, err := m.EncryptBatch(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_EncryptBatch_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.EncryptBatchRequest{}
	m.On("EncryptBatch", ctx, req).Return((*service.EncryptBatchResponse)(nil), errTest)

	resp, err := m.EncryptBatch(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DecryptBatch_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DecryptBatchRequest{}
	expected := &service.DecryptBatchResponse{}
	m.On("DecryptBatch", ctx, req).Return(expected, nil)

	resp, err := m.DecryptBatch(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DecryptBatch_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DecryptBatchRequest{}
	m.On("DecryptBatch", ctx, req).Return((*service.DecryptBatchResponse)(nil), errTest)

	resp, err := m.DecryptBatch(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GenerateDataKey_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GenerateDataKeyRequest{}
	expected := &service.GenerateDataKeyResponse{}
	m.On("GenerateDataKey", ctx, req).Return(expected, nil)

	resp, err := m.GenerateDataKey(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GenerateDataKey_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GenerateDataKeyRequest{}
	m.On("GenerateDataKey", ctx, req).Return((*service.GenerateDataKeyResponse)(nil), errTest)

	resp, err := m.GenerateDataKey(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GenerateWrappedDataKey_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GenerateWrappedDataKeyRequest{}
	expected := &service.GenerateWrappedDataKeyResponse{}
	m.On("GenerateWrappedDataKey", ctx, req).Return(expected, nil)

	resp, err := m.GenerateWrappedDataKey(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GenerateWrappedDataKey_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GenerateWrappedDataKeyRequest{}
	m.On("GenerateWrappedDataKey", ctx, req).Return((*service.GenerateWrappedDataKeyResponse)(nil), errTest)

	resp, err := m.GenerateWrappedDataKey(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GenerateHMAC_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.HMACRequest{}
	expected := &service.HMACResponse{}
	m.On("GenerateHMAC", ctx, req).Return(expected, nil)

	resp, err := m.GenerateHMAC(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GenerateHMAC_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.HMACRequest{}
	m.On("GenerateHMAC", ctx, req).Return((*service.HMACResponse)(nil), errTest)

	resp, err := m.GenerateHMAC(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_VerifyHMAC_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.VerifyHMACRequest{}
	expected := &service.VerifyHMACResponse{}
	m.On("VerifyHMAC", ctx, req).Return(expected, nil)

	resp, err := m.VerifyHMAC(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_VerifyHMAC_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.VerifyHMACRequest{}
	m.On("VerifyHMAC", ctx, req).Return((*service.VerifyHMACResponse)(nil), errTest)

	resp, err := m.VerifyHMAC(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ===========================================================================
// Vector Service Methods - Additional Coverage
// ===========================================================================

func TestMockQuicBaaS_DeleteNamespace_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteNamespaceRequest{}
	expected := &service.DeleteNamespaceResponse{}
	m.On("DeleteNamespace", ctx, req).Return(expected, nil)

	resp, err := m.DeleteNamespace(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteNamespace_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteNamespaceRequest{}
	m.On("DeleteNamespace", ctx, req).Return((*service.DeleteNamespaceResponse)(nil), errTest)

	resp, err := m.DeleteNamespace(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListNamespaces_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListNamespacesRequest{}
	expected := &service.ListNamespacesResponse{}
	m.On("ListNamespaces", ctx, req).Return(expected, nil)

	resp, err := m.ListNamespaces(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListNamespaces_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListNamespacesRequest{}
	m.On("ListNamespaces", ctx, req).Return((*service.ListNamespacesResponse)(nil), errTest)

	resp, err := m.ListNamespaces(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetNamespaceInfo_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetNamespaceInfoRequest{}
	expected := &service.GetNamespaceInfoResponse{}
	m.On("GetNamespaceInfo", ctx, req).Return(expected, nil)

	resp, err := m.GetNamespaceInfo(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetNamespaceInfo_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetNamespaceInfoRequest{}
	m.On("GetNamespaceInfo", ctx, req).Return((*service.GetNamespaceInfoResponse)(nil), errTest)

	resp, err := m.GetNamespaceInfo(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetVector_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetVectorRequest{}
	expected := &service.GetVectorResponse{}
	m.On("GetVector", ctx, req).Return(expected, nil)

	resp, err := m.GetVector(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetVector_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetVectorRequest{}
	m.On("GetVector", ctx, req).Return((*service.GetVectorResponse)(nil), errTest)

	resp, err := m.GetVector(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteVector_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteVectorRequest{}
	expected := &service.DeleteVectorResponse{}
	m.On("DeleteVector", ctx, req).Return(expected, nil)

	resp, err := m.DeleteVector(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteVector_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteVectorRequest{}
	m.On("DeleteVector", ctx, req).Return((*service.DeleteVectorResponse)(nil), errTest)

	resp, err := m.DeleteVector(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_BatchUpsert_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.BatchUpsertRequest{}
	expected := &service.BatchUpsertResponse{}
	m.On("BatchUpsert", ctx, req).Return(expected, nil)

	resp, err := m.BatchUpsert(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_BatchUpsert_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.BatchUpsertRequest{}
	m.On("BatchUpsert", ctx, req).Return((*service.BatchUpsertResponse)(nil), errTest)

	resp, err := m.BatchUpsert(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_BatchDeleteVectors_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.BatchDeleteVectorsRequest{}
	expected := &service.BatchDeleteVectorsResponse{}
	m.On("BatchDeleteVectors", ctx, req).Return(expected, nil)

	resp, err := m.BatchDeleteVectors(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_BatchDeleteVectors_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.BatchDeleteVectorsRequest{}
	m.On("BatchDeleteVectors", ctx, req).Return((*service.BatchDeleteVectorsResponse)(nil), errTest)

	resp, err := m.BatchDeleteVectors(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListVectors_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListVectorsRequest{}
	expected := &service.ListVectorsResponse{}
	m.On("ListVectors", ctx, req).Return(expected, nil)

	resp, err := m.ListVectors(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListVectors_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListVectorsRequest{}
	m.On("ListVectors", ctx, req).Return((*service.ListVectorsResponse)(nil), errTest)

	resp, err := m.ListVectors(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_UpdateMetadata_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UpdateVectorMetadataRequest{}
	expected := &service.UpdateVectorMetadataResponse{}
	m.On("UpdateMetadata", ctx, req).Return(expected, nil)

	resp, err := m.UpdateMetadata(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_UpdateMetadata_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UpdateVectorMetadataRequest{}
	m.On("UpdateMetadata", ctx, req).Return((*service.UpdateVectorMetadataResponse)(nil), errTest)

	resp, err := m.UpdateMetadata(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SearchByMetadata_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SearchByMetadataRequest{}
	expected := &service.SearchByMetadataResponse{}
	m.On("SearchByMetadata", ctx, req).Return(expected, nil)

	resp, err := m.SearchByMetadata(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SearchByMetadata_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SearchByMetadataRequest{}
	m.On("SearchByMetadata", ctx, req).Return((*service.SearchByMetadataResponse)(nil), errTest)

	resp, err := m.SearchByMetadata(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_HybridSearch_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.HybridSearchRequest{}
	expected := &service.HybridSearchResponse{}
	m.On("HybridSearch", ctx, req).Return(expected, nil)

	resp, err := m.HybridSearch(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_HybridSearch_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.HybridSearchRequest{}
	m.On("HybridSearch", ctx, req).Return((*service.HybridSearchResponse)(nil), errTest)

	resp, err := m.HybridSearch(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SearchByID_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SearchByIDRequest{}
	expected := &service.SearchByIDResponse{}
	m.On("SearchByID", ctx, req).Return(expected, nil)

	resp, err := m.SearchByID(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SearchByID_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SearchByIDRequest{}
	m.On("SearchByID", ctx, req).Return((*service.SearchByIDResponse)(nil), errTest)

	resp, err := m.SearchByID(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RebuildIndex_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RebuildIndexRequest{}
	expected := &service.RebuildIndexResponse{}
	m.On("RebuildIndex", ctx, req).Return(expected, nil)

	resp, err := m.RebuildIndex(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RebuildIndex_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RebuildIndexRequest{}
	m.On("RebuildIndex", ctx, req).Return((*service.RebuildIndexResponse)(nil), errTest)

	resp, err := m.RebuildIndex(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetRebuildProgress_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetRebuildProgressRequest{}
	expected := &service.GetRebuildProgressResponse{}
	m.On("GetRebuildProgress", ctx, req).Return(expected, nil)

	resp, err := m.GetRebuildProgress(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetRebuildProgress_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetRebuildProgressRequest{}
	m.On("GetRebuildProgress", ctx, req).Return((*service.GetRebuildProgressResponse)(nil), errTest)

	resp, err := m.GetRebuildProgress(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetIndexStats_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetIndexStatsRequest{}
	expected := &service.GetIndexStatsResponse{}
	m.On("GetIndexStats", ctx, req).Return(expected, nil)

	resp, err := m.GetIndexStats(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetIndexStats_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetIndexStatsRequest{}
	m.On("GetIndexStats", ctx, req).Return((*service.GetIndexStatsResponse)(nil), errTest)

	resp, err := m.GetIndexStats(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CompactNamespace_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CompactNamespaceRequest{}
	expected := &service.CompactNamespaceResponse{}
	m.On("CompactNamespace", ctx, req).Return(expected, nil)

	resp, err := m.CompactNamespace(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CompactNamespace_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CompactNamespaceRequest{}
	m.On("CompactNamespace", ctx, req).Return((*service.CompactNamespaceResponse)(nil), errTest)

	resp, err := m.CompactNamespace(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetCompactionProgress_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetCompactionProgressRequest{}
	expected := &service.GetCompactionProgressResponse{}
	m.On("GetCompactionProgress", ctx, req).Return(expected, nil)

	resp, err := m.GetCompactionProgress(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetCompactionProgress_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetCompactionProgressRequest{}
	m.On("GetCompactionProgress", ctx, req).Return((*service.GetCompactionProgressResponse)(nil), errTest)

	resp, err := m.GetCompactionProgress(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ===========================================================================
// Dynamic Secrets Service Methods - Additional Coverage
// ===========================================================================

func TestMockQuicBaaS_GetRole_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetRoleRequest{}
	expected := &service.GetRoleResponse{}
	m.On("GetRole", ctx, req).Return(expected, nil)

	resp, err := m.GetRole(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetRole_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetRoleRequest{}
	m.On("GetRole", ctx, req).Return((*service.GetRoleResponse)(nil), errTest)

	resp, err := m.GetRole(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListRoles_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListRolesRequest{}
	expected := &service.ListRolesResponse{}
	m.On("ListRoles", ctx, req).Return(expected, nil)

	resp, err := m.ListRoles(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListRoles_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListRolesRequest{}
	m.On("ListRoles", ctx, req).Return((*service.ListRolesResponse)(nil), errTest)

	resp, err := m.ListRoles(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteRole_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteRoleRequest{}
	expected := &service.DeleteRoleResponse{}
	m.On("DeleteRole", ctx, req).Return(expected, nil)

	resp, err := m.DeleteRole(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteRole_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteRoleRequest{}
	m.On("DeleteRole", ctx, req).Return((*service.DeleteRoleResponse)(nil), errTest)

	resp, err := m.DeleteRole(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RevokeLease_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RevokeLeaseRequest{}
	expected := &service.RevokeLeaseResponse{}
	m.On("RevokeLease", ctx, req).Return(expected, nil)

	resp, err := m.RevokeLease(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RevokeLease_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RevokeLeaseRequest{}
	m.On("RevokeLease", ctx, req).Return((*service.RevokeLeaseResponse)(nil), errTest)

	resp, err := m.RevokeLease(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListLeases_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListLeasesRequest{}
	expected := &service.ListLeasesResponse{}
	m.On("ListLeases", ctx, req).Return(expected, nil)

	resp, err := m.ListLeases(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListLeases_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListLeasesRequest{}
	m.On("ListLeases", ctx, req).Return((*service.ListLeasesResponse)(nil), errTest)

	resp, err := m.ListLeases(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ===========================================================================
// Object Storage Service Methods - Additional Coverage
// ===========================================================================

func TestMockQuicBaaS_HeadObject_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.HeadObjectRequest{}
	expected := &service.HeadObjectResponse{}
	m.On("HeadObject", ctx, req).Return(expected, nil)

	resp, err := m.HeadObject(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_HeadObject_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.HeadObjectRequest{}
	m.On("HeadObject", ctx, req).Return((*service.HeadObjectResponse)(nil), errTest)

	resp, err := m.HeadObject(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListObjects_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListObjectsRequest{}
	expected := &service.ListObjectsResponse{}
	m.On("ListObjects", ctx, req).Return(expected, nil)

	resp, err := m.ListObjects(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListObjects_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListObjectsRequest{}
	m.On("ListObjects", ctx, req).Return((*service.ListObjectsResponse)(nil), errTest)

	resp, err := m.ListObjects(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListByTag_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListByTagRequest{}
	expected := &service.ListByTagResponse{}
	m.On("ListByTag", ctx, req).Return(expected, nil)

	resp, err := m.ListByTag(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListByTag_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListByTagRequest{}
	m.On("ListByTag", ctx, req).Return((*service.ListByTagResponse)(nil), errTest)

	resp, err := m.ListByTag(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListByTenant_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListByTenantRequest{}
	expected := &service.ListByTenantResponse{}
	m.On("ListByTenant", ctx, req).Return(expected, nil)

	resp, err := m.ListByTenant(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListByTenant_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListByTenantRequest{}
	m.On("ListByTenant", ctx, req).Return((*service.ListByTenantResponse)(nil), errTest)

	resp, err := m.ListByTenant(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CopyObject_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CopyObjectRequest{}
	expected := &service.CopyObjectResponse{}
	m.On("CopyObject", ctx, req).Return(expected, nil)

	resp, err := m.CopyObject(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CopyObject_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CopyObjectRequest{}
	m.On("CopyObject", ctx, req).Return((*service.CopyObjectResponse)(nil), errTest)

	resp, err := m.CopyObject(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ReplicateObject_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ReplicateObjectRequest{}
	expected := &service.ReplicateObjectResponse{}
	m.On("ReplicateObject", ctx, req).Return(expected, nil)

	resp, err := m.ReplicateObject(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ReplicateObject_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ReplicateObjectRequest{}
	m.On("ReplicateObject", ctx, req).Return((*service.ReplicateObjectResponse)(nil), errTest)

	resp, err := m.ReplicateObject(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_MigrateObject_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.MigrateObjectRequest{}
	expected := &service.MigrateObjectResponse{}
	m.On("MigrateObject", ctx, req).Return(expected, nil)

	resp, err := m.MigrateObject(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_MigrateObject_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.MigrateObjectRequest{}
	m.On("MigrateObject", ctx, req).Return((*service.MigrateObjectResponse)(nil), errTest)

	resp, err := m.MigrateObject(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ArchiveObject_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ArchiveObjectRequest{}
	expected := &service.ArchiveObjectResponse{}
	m.On("ArchiveObject", ctx, req).Return(expected, nil)

	resp, err := m.ArchiveObject(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ArchiveObject_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ArchiveObjectRequest{}
	m.On("ArchiveObject", ctx, req).Return((*service.ArchiveObjectResponse)(nil), errTest)

	resp, err := m.ArchiveObject(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RestoreObject_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RestoreObjectRequest{}
	expected := &service.RestoreObjectResponse{}
	m.On("RestoreObject", ctx, req).Return(expected, nil)

	resp, err := m.RestoreObject(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RestoreObject_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RestoreObjectRequest{}
	m.On("RestoreObject", ctx, req).Return((*service.RestoreObjectResponse)(nil), errTest)

	resp, err := m.RestoreObject(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_VerifyObject_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.VerifyObjectRequest{}
	expected := &service.VerifyObjectResponse{}
	m.On("VerifyObject", ctx, req).Return(expected, nil)

	resp, err := m.VerifyObject(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_VerifyObject_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.VerifyObjectRequest{}
	m.On("VerifyObject", ctx, req).Return((*service.VerifyObjectResponse)(nil), errTest)

	resp, err := m.VerifyObject(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetObjectPolicy_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SetObjectPolicyRequest{}
	expected := &service.SetObjectPolicyResponse{}
	m.On("SetObjectPolicy", ctx, req).Return(expected, nil)

	resp, err := m.SetObjectPolicy(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetObjectPolicy_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SetObjectPolicyRequest{}
	m.On("SetObjectPolicy", ctx, req).Return((*service.SetObjectPolicyResponse)(nil), errTest)

	resp, err := m.SetObjectPolicy(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetObjectPolicy_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetObjectPolicyRequest{}
	expected := &service.GetObjectPolicyResponse{}
	m.On("GetObjectPolicy", ctx, req).Return(expected, nil)

	resp, err := m.GetObjectPolicy(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetObjectPolicy_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetObjectPolicyRequest{}
	m.On("GetObjectPolicy", ctx, req).Return((*service.GetObjectPolicyResponse)(nil), errTest)

	resp, err := m.GetObjectPolicy(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListBackends_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListBackendsRequest{}
	expected := &service.ListBackendsResponse{}
	m.On("ListBackends", ctx, req).Return(expected, nil)

	resp, err := m.ListBackends(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListBackends_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListBackendsRequest{}
	m.On("ListBackends", ctx, req).Return((*service.ListBackendsResponse)(nil), errTest)

	resp, err := m.ListBackends(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetBackendHealth_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetBackendHealthRequest{}
	expected := &service.GetBackendHealthResponse{}
	m.On("GetBackendHealth", ctx, req).Return(expected, nil)

	resp, err := m.GetBackendHealth(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetBackendHealth_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetBackendHealthRequest{}
	m.On("GetBackendHealth", ctx, req).Return((*service.GetBackendHealthResponse)(nil), errTest)

	resp, err := m.GetBackendHealth(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetTenantUsage_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetTenantUsageRequest{}
	expected := &service.GetTenantUsageResponse{}
	m.On("GetTenantUsage", ctx, req).Return(expected, nil)

	resp, err := m.GetTenantUsage(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetTenantUsage_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetTenantUsageRequest{}
	m.On("GetTenantUsage", ctx, req).Return((*service.GetTenantUsageResponse)(nil), errTest)

	resp, err := m.GetTenantUsage(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetTenantQuotas_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetTenantQuotasRequest{}
	expected := &service.GetTenantQuotasResponse{}
	m.On("GetTenantQuotas", ctx, req).Return(expected, nil)

	resp, err := m.GetTenantQuotas(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetTenantQuotas_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetTenantQuotasRequest{}
	m.On("GetTenantQuotas", ctx, req).Return((*service.GetTenantQuotasResponse)(nil), errTest)

	resp, err := m.GetTenantQuotas(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetTenantQuotas_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SetTenantQuotasRequest{}
	expected := &service.SetTenantQuotasResponse{}
	m.On("SetTenantQuotas", ctx, req).Return(expected, nil)

	resp, err := m.SetTenantQuotas(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetTenantQuotas_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SetTenantQuotasRequest{}
	m.On("SetTenantQuotas", ctx, req).Return((*service.SetTenantQuotasResponse)(nil), errTest)

	resp, err := m.SetTenantQuotas(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_BatchPutObjects_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.BatchPutObjectsRequest{}
	expected := &service.BatchPutObjectsResponse{}
	m.On("BatchPutObjects", ctx, req).Return(expected, nil)

	resp, err := m.BatchPutObjects(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_BatchPutObjects_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.BatchPutObjectsRequest{}
	m.On("BatchPutObjects", ctx, req).Return((*service.BatchPutObjectsResponse)(nil), errTest)

	resp, err := m.BatchPutObjects(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_BatchGetObjects_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.BatchGetObjectsRequest{}
	expected := &service.BatchGetObjectsResponse{}
	m.On("BatchGetObjects", ctx, req).Return(expected, nil)

	resp, err := m.BatchGetObjects(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_BatchGetObjects_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.BatchGetObjectsRequest{}
	m.On("BatchGetObjects", ctx, req).Return((*service.BatchGetObjectsResponse)(nil), errTest)

	resp, err := m.BatchGetObjects(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_BatchDeleteObjects_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.BatchDeleteObjectsRequest{}
	expected := &service.BatchDeleteObjectsResponse{}
	m.On("BatchDeleteObjects", ctx, req).Return(expected, nil)

	resp, err := m.BatchDeleteObjects(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_BatchDeleteObjects_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.BatchDeleteObjectsRequest{}
	m.On("BatchDeleteObjects", ctx, req).Return((*service.BatchDeleteObjectsResponse)(nil), errTest)

	resp, err := m.BatchDeleteObjects(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ===========================================================================
// Object Storage Admin Service Methods - Additional Coverage
// ===========================================================================

func TestMockQuicBaaS_UpdateBackend_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UpdateBackendRequest{}
	expected := &service.UpdateBackendResponse{}
	m.On("UpdateBackend", ctx, req).Return(expected, nil)

	resp, err := m.UpdateBackend(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_UpdateBackend_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UpdateBackendRequest{}
	m.On("UpdateBackend", ctx, req).Return((*service.UpdateBackendResponse)(nil), errTest)

	resp, err := m.UpdateBackend(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteBackend_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteBackendRequest{}
	expected := &service.DeleteBackendResponse{}
	m.On("DeleteBackend", ctx, req).Return(expected, nil)

	resp, err := m.DeleteBackend(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteBackend_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteBackendRequest{}
	m.On("DeleteBackend", ctx, req).Return((*service.DeleteBackendResponse)(nil), errTest)

	resp, err := m.DeleteBackend(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetBackendStatus_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SetBackendStatusRequest{}
	expected := &service.SetBackendStatusResponse{}
	m.On("SetBackendStatus", ctx, req).Return(expected, nil)

	resp, err := m.SetBackendStatus(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetBackendStatus_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SetBackendStatusRequest{}
	m.On("SetBackendStatus", ctx, req).Return((*service.SetBackendStatusResponse)(nil), errTest)

	resp, err := m.SetBackendStatus(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetStorageStats_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetStorageStatsRequest{}
	expected := &service.GetStorageStatsResponse{}
	m.On("GetStorageStats", ctx, req).Return(expected, nil)

	resp, err := m.GetStorageStats(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetStorageStats_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetStorageStatsRequest{}
	m.On("GetStorageStats", ctx, req).Return((*service.GetStorageStatsResponse)(nil), errTest)

	resp, err := m.GetStorageStats(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RunGarbageCollection_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RunGarbageCollectionRequest{}
	expected := &service.RunGarbageCollectionResponse{}
	m.On("RunGarbageCollection", ctx, req).Return(expected, nil)

	resp, err := m.RunGarbageCollection(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RunGarbageCollection_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RunGarbageCollectionRequest{}
	m.On("RunGarbageCollection", ctx, req).Return((*service.RunGarbageCollectionResponse)(nil), errTest)

	resp, err := m.RunGarbageCollection(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetGarbageCollectionProgress_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetGarbageCollectionProgressRequest{}
	expected := &service.GetGarbageCollectionProgressResponse{}
	m.On("GetGarbageCollectionProgress", ctx, req).Return(expected, nil)

	resp, err := m.GetGarbageCollectionProgress(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetGarbageCollectionProgress_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetGarbageCollectionProgressRequest{}
	m.On("GetGarbageCollectionProgress", ctx, req).Return((*service.GetGarbageCollectionProgressResponse)(nil), errTest)

	resp, err := m.GetGarbageCollectionProgress(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RepairObject_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RepairObjectRequest{}
	expected := &service.RepairObjectResponse{}
	m.On("RepairObject", ctx, req).Return(expected, nil)

	resp, err := m.RepairObject(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RepairObject_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RepairObjectRequest{}
	m.On("RepairObject", ctx, req).Return((*service.RepairObjectResponse)(nil), errTest)

	resp, err := m.RepairObject(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CompactBackend_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CompactBackendRequest{}
	expected := &service.CompactBackendResponse{}
	m.On("CompactBackend", ctx, req).Return(expected, nil)

	resp, err := m.CompactBackend(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CompactBackend_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CompactBackendRequest{}
	m.On("CompactBackend", ctx, req).Return((*service.CompactBackendResponse)(nil), errTest)

	resp, err := m.CompactBackend(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ===========================================================================
// Parameter Store Service Methods - Additional Coverage
// ===========================================================================

func TestMockQuicBaaS_DeleteParameter_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &interfaces.DeleteParameterRequest{}
	expected := &interfaces.DeleteParameterResponse{}
	m.On("DeleteParameter", ctx, req).Return(expected, nil)

	resp, err := m.DeleteParameter(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteParameter_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &interfaces.DeleteParameterRequest{}
	m.On("DeleteParameter", ctx, req).Return((*interfaces.DeleteParameterResponse)(nil), errTest)

	resp, err := m.DeleteParameter(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetParameters_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &interfaces.GetParametersRequest{}
	expected := &interfaces.GetParametersResponse{}
	m.On("GetParameters", ctx, req).Return(expected, nil)

	resp, err := m.GetParameters(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetParameters_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &interfaces.GetParametersRequest{}
	m.On("GetParameters", ctx, req).Return((*interfaces.GetParametersResponse)(nil), errTest)

	resp, err := m.GetParameters(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetParameters_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &interfaces.SetParametersRequest{}
	expected := &interfaces.SetParametersResponse{}
	m.On("SetParameters", ctx, req).Return(expected, nil)

	resp, err := m.SetParameters(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetParameters_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &interfaces.SetParametersRequest{}
	m.On("SetParameters", ctx, req).Return((*interfaces.SetParametersResponse)(nil), errTest)

	resp, err := m.SetParameters(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteParameters_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &interfaces.DeleteParametersRequest{}
	expected := &interfaces.DeleteParametersResponse{}
	m.On("DeleteParameters", ctx, req).Return(expected, nil)

	resp, err := m.DeleteParameters(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteParameters_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &interfaces.DeleteParametersRequest{}
	m.On("DeleteParameters", ctx, req).Return((*interfaces.DeleteParametersResponse)(nil), errTest)

	resp, err := m.DeleteParameters(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListParameters_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &interfaces.ListParametersRequest{}
	expected := &interfaces.ListParametersResponse{}
	m.On("ListParameters", ctx, req).Return(expected, nil)

	resp, err := m.ListParameters(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListParameters_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &interfaces.ListParametersRequest{}
	m.On("ListParameters", ctx, req).Return((*interfaces.ListParametersResponse)(nil), errTest)

	resp, err := m.ListParameters(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SearchByTags_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &interfaces.SearchByTagsRequest{}
	expected := &interfaces.SearchByTagsResponse{}
	m.On("SearchByTags", ctx, req).Return(expected, nil)

	resp, err := m.SearchByTags(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SearchByTags_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &interfaces.SearchByTagsRequest{}
	m.On("SearchByTags", ctx, req).Return((*interfaces.SearchByTagsResponse)(nil), errTest)

	resp, err := m.SearchByTags(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ===========================================================================
// Object Transfer Service Methods - Additional Coverage
// ===========================================================================

func TestMockQuicBaaS_StreamFetchObjects_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.StreamFetchObjectsRequest{}
	expected := &service.StreamFetchObjectsResponse{}
	m.On("StreamFetchObjects", ctx, req).Return(expected, nil)

	resp, err := m.StreamFetchObjects(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_StreamFetchObjects_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.StreamFetchObjectsRequest{}
	m.On("StreamFetchObjects", ctx, req).Return((*service.StreamFetchObjectsResponse)(nil), errTest)

	resp, err := m.StreamFetchObjects(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ===========================================================================
// Scheduler Service Methods - Additional Coverage
// ===========================================================================

func TestMockQuicBaaS_GetJob_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetSchedulerJobRequest{}
	expected := &service.GetSchedulerJobResponse{}
	m.On("GetJob", ctx, req).Return(expected, nil)

	resp, err := m.GetJob(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetJob_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetSchedulerJobRequest{}
	m.On("GetJob", ctx, req).Return((*service.GetSchedulerJobResponse)(nil), errTest)

	resp, err := m.GetJob(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_UpdateJob_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UpdateSchedulerJobRequest{}
	expected := &service.UpdateSchedulerJobResponse{}
	m.On("UpdateJob", ctx, req).Return(expected, nil)

	resp, err := m.UpdateJob(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_UpdateJob_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UpdateSchedulerJobRequest{}
	m.On("UpdateJob", ctx, req).Return((*service.UpdateSchedulerJobResponse)(nil), errTest)

	resp, err := m.UpdateJob(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteJob_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteSchedulerJobRequest{}
	expected := &service.DeleteSchedulerJobResponse{}
	m.On("DeleteJob", ctx, req).Return(expected, nil)

	resp, err := m.DeleteJob(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteJob_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteSchedulerJobRequest{}
	m.On("DeleteJob", ctx, req).Return((*service.DeleteSchedulerJobResponse)(nil), errTest)

	resp, err := m.DeleteJob(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListJobs_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListSchedulerJobsRequest{}
	expected := &service.ListSchedulerJobsResponse{}
	m.On("ListJobs", ctx, req).Return(expected, nil)

	resp, err := m.ListJobs(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListJobs_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListSchedulerJobsRequest{}
	m.On("ListJobs", ctx, req).Return((*service.ListSchedulerJobsResponse)(nil), errTest)

	resp, err := m.ListJobs(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_TriggerJob_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.TriggerSchedulerJobRequest{}
	expected := &service.TriggerSchedulerJobResponse{}
	m.On("TriggerJob", ctx, req).Return(expected, nil)

	resp, err := m.TriggerJob(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_TriggerJob_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.TriggerSchedulerJobRequest{}
	m.On("TriggerJob", ctx, req).Return((*service.TriggerSchedulerJobResponse)(nil), errTest)

	resp, err := m.TriggerJob(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_PauseJob_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.PauseSchedulerJobRequest{}
	expected := &service.PauseSchedulerJobResponse{}
	m.On("PauseJob", ctx, req).Return(expected, nil)

	resp, err := m.PauseJob(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_PauseJob_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.PauseSchedulerJobRequest{}
	m.On("PauseJob", ctx, req).Return((*service.PauseSchedulerJobResponse)(nil), errTest)

	resp, err := m.PauseJob(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ResumeJob_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ResumeSchedulerJobRequest{}
	expected := &service.ResumeSchedulerJobResponse{}
	m.On("ResumeJob", ctx, req).Return(expected, nil)

	resp, err := m.ResumeJob(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ResumeJob_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ResumeSchedulerJobRequest{}
	m.On("ResumeJob", ctx, req).Return((*service.ResumeSchedulerJobResponse)(nil), errTest)

	resp, err := m.ResumeJob(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CancelJob_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CancelSchedulerJobRequest{}
	expected := &service.CancelSchedulerJobResponse{}
	m.On("CancelJob", ctx, req).Return(expected, nil)

	resp, err := m.CancelJob(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CancelJob_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CancelSchedulerJobRequest{}
	m.On("CancelJob", ctx, req).Return((*service.CancelSchedulerJobResponse)(nil), errTest)

	resp, err := m.CancelJob(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetJobHistory_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetSchedulerJobHistoryRequest{}
	expected := &service.GetSchedulerJobHistoryResponse{}
	m.On("GetJobHistory", ctx, req).Return(expected, nil)

	resp, err := m.GetJobHistory(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetJobHistory_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetSchedulerJobHistoryRequest{}
	m.On("GetJobHistory", ctx, req).Return((*service.GetSchedulerJobHistoryResponse)(nil), errTest)

	resp, err := m.GetJobHistory(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}
