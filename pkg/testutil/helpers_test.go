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

// mockMethodTest checks that one MockQuicBaaS method returns what the test
// programmed with On: the response on success, errTest and a nil response
// on error.
type mockMethodTest struct {
	method  string
	success func(t *testing.T)
	failure func(t *testing.T)
}

// mockMethod builds the success and error cases for a MockQuicBaaS method
// that takes (ctx, *Req) and returns (*Resp, error).
func mockMethod[Req, Resp any](method string, call func(*MockQuicBaaS, context.Context, *Req) (*Resp, error)) mockMethodTest {
	return mockMethodTest{
		method: method,
		success: func(t *testing.T) {
			m := NewMockQuicBaaS()
			ctx := context.Background()
			req := new(Req)
			expected := new(Resp)
			m.On(method, ctx, req).Return(expected, nil)

			resp, err := call(m, ctx, req)
			assert.NoError(t, err)
			assert.Equal(t, expected, resp)
			m.AssertExpectations(t)
		},
		failure: func(t *testing.T) {
			m := NewMockQuicBaaS()
			ctx := context.Background()
			req := new(Req)
			m.On(method, ctx, req).Return((*Resp)(nil), errTest)

			resp, err := call(m, ctx, req)
			assert.ErrorIs(t, err, errTest)
			assert.Nil(t, resp)
			m.AssertExpectations(t)
		},
	}
}

func runMockMethodTests(t *testing.T, tests []mockMethodTest) {
	t.Helper()
	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			t.Run("success", tc.success)
			t.Run("error", tc.failure)
		})
	}
}

func TestMockQuicBaaS_KMSMethods(t *testing.T) {
	runMockMethodTests(t, []mockMethodTest{
		mockMethod("GetPublicKey", (*MockQuicBaaS).GetPublicKey),
		mockMethod("GetKeyMetadata", (*MockQuicBaaS).GetKeyMetadata),
		mockMethod("UpdateKeyMetadata", (*MockQuicBaaS).UpdateKeyMetadata),
		mockMethod("UpdateKeyStatus", (*MockQuicBaaS).UpdateKeyStatus),
		mockMethod("ListKeyVersions", (*MockQuicBaaS).ListKeyVersions),
		mockMethod("SetKeyPolicy", (*MockQuicBaaS).SetKeyPolicy),
		mockMethod("GetKeyPolicy", (*MockQuicBaaS).GetKeyPolicy),
		mockMethod("CreateDEK", (*MockQuicBaaS).CreateDEK),
		mockMethod("GetDEK", (*MockQuicBaaS).GetDEK),
		mockMethod("ListDEKs", (*MockQuicBaaS).ListDEKs),
		mockMethod("RotateDEK", (*MockQuicBaaS).RotateDEK),
		mockMethod("DeleteDEK", (*MockQuicBaaS).DeleteDEK),
		mockMethod("GetDEKStatus", (*MockQuicBaaS).GetDEKStatus),
		mockMethod("SetDefaultDEK", (*MockQuicBaaS).SetDefaultDEK),
		mockMethod("SetDEKSafety", (*MockQuicBaaS).SetDEKSafety),
		mockMethod("ClearDEKSafety", (*MockQuicBaaS).ClearDEKSafety),
		mockMethod("StartReencryption", (*MockQuicBaaS).StartReencryption),
		mockMethod("GetReencryptionProgress", (*MockQuicBaaS).GetReencryptionProgress),
		mockMethod("ExportKey", (*MockQuicBaaS).ExportKey),
		mockMethod("UnwrapKey", (*MockQuicBaaS).UnwrapKey),
		mockMethod("SetKeySafety", (*MockQuicBaaS).SetKeySafety),
		mockMethod("ClearKeySafety", (*MockQuicBaaS).ClearKeySafety),
		mockMethod("SafetyCheck", (*MockQuicBaaS).SafetyCheck),
		mockMethod("SafetyReport", (*MockQuicBaaS).SafetyReport),
	})
}

func TestMockQuicBaaS_CryptoMethods(t *testing.T) {
	runMockMethodTests(t, []mockMethodTest{
		mockMethod("EncryptBatch", (*MockQuicBaaS).EncryptBatch),
		mockMethod("DecryptBatch", (*MockQuicBaaS).DecryptBatch),
		mockMethod("GenerateDataKey", (*MockQuicBaaS).GenerateDataKey),
		mockMethod("GenerateWrappedDataKey", (*MockQuicBaaS).GenerateWrappedDataKey),
		mockMethod("GenerateHMAC", (*MockQuicBaaS).GenerateHMAC),
		mockMethod("VerifyHMAC", (*MockQuicBaaS).VerifyHMAC),
	})
}

func TestMockQuicBaaS_VectorMethods(t *testing.T) {
	runMockMethodTests(t, []mockMethodTest{
		mockMethod("DeleteNamespace", (*MockQuicBaaS).DeleteNamespace),
		mockMethod("ListNamespaces", (*MockQuicBaaS).ListNamespaces),
		mockMethod("GetNamespaceInfo", (*MockQuicBaaS).GetNamespaceInfo),
		mockMethod("GetVector", (*MockQuicBaaS).GetVector),
		mockMethod("DeleteVector", (*MockQuicBaaS).DeleteVector),
		mockMethod("BatchUpsert", (*MockQuicBaaS).BatchUpsert),
		mockMethod("BatchDeleteVectors", (*MockQuicBaaS).BatchDeleteVectors),
		mockMethod("ListVectors", (*MockQuicBaaS).ListVectors),
		mockMethod("UpdateMetadata", (*MockQuicBaaS).UpdateMetadata),
		mockMethod("SearchByMetadata", (*MockQuicBaaS).SearchByMetadata),
		mockMethod("HybridSearch", (*MockQuicBaaS).HybridSearch),
		mockMethod("SearchByID", (*MockQuicBaaS).SearchByID),
		mockMethod("RebuildIndex", (*MockQuicBaaS).RebuildIndex),
		mockMethod("GetRebuildProgress", (*MockQuicBaaS).GetRebuildProgress),
		mockMethod("GetIndexStats", (*MockQuicBaaS).GetIndexStats),
		mockMethod("CompactNamespace", (*MockQuicBaaS).CompactNamespace),
		mockMethod("GetCompactionProgress", (*MockQuicBaaS).GetCompactionProgress),
	})
}

func TestMockQuicBaaS_DynamicSecretsMethods(t *testing.T) {
	runMockMethodTests(t, []mockMethodTest{
		mockMethod("GetRole", (*MockQuicBaaS).GetRole),
		mockMethod("ListRoles", (*MockQuicBaaS).ListRoles),
		mockMethod("DeleteRole", (*MockQuicBaaS).DeleteRole),
		mockMethod("RevokeLease", (*MockQuicBaaS).RevokeLease),
		mockMethod("ListLeases", (*MockQuicBaaS).ListLeases),
	})
}

func TestMockQuicBaaS_ObjectStorageMethods(t *testing.T) {
	runMockMethodTests(t, []mockMethodTest{
		mockMethod("HeadObject", (*MockQuicBaaS).HeadObject),
		mockMethod("ListObjects", (*MockQuicBaaS).ListObjects),
		mockMethod("ListByTag", (*MockQuicBaaS).ListByTag),
		mockMethod("ListByTenant", (*MockQuicBaaS).ListByTenant),
		mockMethod("CopyObject", (*MockQuicBaaS).CopyObject),
		mockMethod("ReplicateObject", (*MockQuicBaaS).ReplicateObject),
		mockMethod("MigrateObject", (*MockQuicBaaS).MigrateObject),
		mockMethod("ArchiveObject", (*MockQuicBaaS).ArchiveObject),
		mockMethod("RestoreObject", (*MockQuicBaaS).RestoreObject),
		mockMethod("VerifyObject", (*MockQuicBaaS).VerifyObject),
		mockMethod("SetObjectPolicy", (*MockQuicBaaS).SetObjectPolicy),
		mockMethod("GetObjectPolicy", (*MockQuicBaaS).GetObjectPolicy),
		mockMethod("ListBackends", (*MockQuicBaaS).ListBackends),
		mockMethod("GetBackendHealth", (*MockQuicBaaS).GetBackendHealth),
		mockMethod("GetTenantUsage", (*MockQuicBaaS).GetTenantUsage),
		mockMethod("GetTenantQuotas", (*MockQuicBaaS).GetTenantQuotas),
		mockMethod("SetTenantQuotas", (*MockQuicBaaS).SetTenantQuotas),
		mockMethod("BatchPutObjects", (*MockQuicBaaS).BatchPutObjects),
		mockMethod("BatchGetObjects", (*MockQuicBaaS).BatchGetObjects),
		mockMethod("BatchDeleteObjects", (*MockQuicBaaS).BatchDeleteObjects),
	})
}

func TestMockQuicBaaS_ObjectStorageAdminMethods(t *testing.T) {
	runMockMethodTests(t, []mockMethodTest{
		mockMethod("UpdateBackend", (*MockQuicBaaS).UpdateBackend),
		mockMethod("DeleteBackend", (*MockQuicBaaS).DeleteBackend),
		mockMethod("SetBackendStatus", (*MockQuicBaaS).SetBackendStatus),
		mockMethod("GetStorageStats", (*MockQuicBaaS).GetStorageStats),
		mockMethod("RunGarbageCollection", (*MockQuicBaaS).RunGarbageCollection),
		mockMethod("GetGarbageCollectionProgress", (*MockQuicBaaS).GetGarbageCollectionProgress),
		mockMethod("RepairObject", (*MockQuicBaaS).RepairObject),
		mockMethod("CompactBackend", (*MockQuicBaaS).CompactBackend),
	})
}

func TestMockQuicBaaS_ParameterStoreMethods(t *testing.T) {
	runMockMethodTests(t, []mockMethodTest{
		mockMethod("DeleteParameter", (*MockQuicBaaS).DeleteParameter),
		mockMethod("GetParameters", (*MockQuicBaaS).GetParameters),
		mockMethod("SetParameters", (*MockQuicBaaS).SetParameters),
		mockMethod("DeleteParameters", (*MockQuicBaaS).DeleteParameters),
		mockMethod("ListParameters", (*MockQuicBaaS).ListParameters),
		mockMethod("SearchByTags", (*MockQuicBaaS).SearchByTags),
	})
}

func TestMockQuicBaaS_ObjectTransferMethods(t *testing.T) {
	runMockMethodTests(t, []mockMethodTest{
		mockMethod("StreamFetchObjects", (*MockQuicBaaS).StreamFetchObjects),
	})
}

func TestMockQuicBaaS_SchedulerMethods(t *testing.T) {
	runMockMethodTests(t, []mockMethodTest{
		mockMethod("GetJob", (*MockQuicBaaS).GetJob),
		mockMethod("UpdateJob", (*MockQuicBaaS).UpdateJob),
		mockMethod("DeleteJob", (*MockQuicBaaS).DeleteJob),
		mockMethod("ListJobs", (*MockQuicBaaS).ListJobs),
		mockMethod("TriggerJob", (*MockQuicBaaS).TriggerJob),
		mockMethod("PauseJob", (*MockQuicBaaS).PauseJob),
		mockMethod("ResumeJob", (*MockQuicBaaS).ResumeJob),
		mockMethod("CancelJob", (*MockQuicBaaS).CancelJob),
		mockMethod("GetJobHistory", (*MockQuicBaaS).GetJobHistory),
	})
}
