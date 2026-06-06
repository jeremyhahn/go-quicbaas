//go:build mcp

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

package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock DEK Service
// =============================================================================

// MockDEKService mocks DEK operations for testing.
type MockDEKService struct {
	mock.Mock
}

// CreateDEK mocks DEK creation.
func (m *MockDEKService) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.CreateDEKResponse), args.Error(1)
}

// GetDEK mocks DEK retrieval.
func (m *MockDEKService) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetDEKResponse), args.Error(1)
}

// ListDEKs mocks DEK listing.
func (m *MockDEKService) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ListDEKsResponse), args.Error(1)
}

// RotateDEK mocks DEK rotation.
func (m *MockDEKService) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.RotateDEKResponse), args.Error(1)
}

// DeleteDEK mocks DEK deletion.
func (m *MockDEKService) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.DeleteDEKResponse), args.Error(1)
}

// GetDEKStatus mocks DEK status retrieval.
func (m *MockDEKService) GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetDEKStatusResponse), args.Error(1)
}

// SetDefaultDEK mocks setting default DEK.
func (m *MockDEKService) SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.SetDefaultDEKResponse), args.Error(1)
}

// SetDEKSafety mocks setting DEK safety limits.
func (m *MockDEKService) SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.SetDEKSafetyResponse), args.Error(1)
}

// ClearDEKSafety mocks clearing DEK safety limits.
func (m *MockDEKService) ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ClearDEKSafetyResponse), args.Error(1)
}

// GetReencryptionProgress mocks getting re-encryption progress.
func (m *MockDEKService) GetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetReencryptionProgressResponse), args.Error(1)
}

// testQuicBaaSWithDEK wraps MockQuicBaaS and provides DEK service implementations.
// The kmsSvc field is stored so that mock expectations on StartReencryption persist
// across multiple KMS() calls.
type testQuicBaaSWithDEK struct {
	*MockQuicBaaS
	dekSvc *MockDEKService
	kmsSvc *mockKMSServiceWithDEK
}

// CreateDEK implements DEK creation.
func (m *testQuicBaaSWithDEK) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	return m.dekSvc.CreateDEK(ctx, req)
}

// GetDEK implements DEK retrieval.
func (m *testQuicBaaSWithDEK) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	return m.dekSvc.GetDEK(ctx, req)
}

// ListDEKs implements DEK listing.
func (m *testQuicBaaSWithDEK) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	return m.dekSvc.ListDEKs(ctx, req)
}

// RotateDEK implements DEK rotation.
func (m *testQuicBaaSWithDEK) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	return m.dekSvc.RotateDEK(ctx, req)
}

// DeleteDEK implements DEK deletion.
func (m *testQuicBaaSWithDEK) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	return m.dekSvc.DeleteDEK(ctx, req)
}

// GetDEKStatus implements DEK status retrieval.
func (m *testQuicBaaSWithDEK) GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	return m.dekSvc.GetDEKStatus(ctx, req)
}

// SetDefaultDEK implements setting default DEK.
func (m *testQuicBaaSWithDEK) SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	return m.dekSvc.SetDefaultDEK(ctx, req)
}

// SetDEKSafety implements setting DEK safety limits.
func (m *testQuicBaaSWithDEK) SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	return m.dekSvc.SetDEKSafety(ctx, req)
}

// ClearDEKSafety implements clearing DEK safety limits.
func (m *testQuicBaaSWithDEK) ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	return m.dekSvc.ClearDEKSafety(ctx, req)
}

// mockKMSServiceWithDEK wraps MockDEKService and implements service.KMSService.
// StartReencryption is backed by testify mock so individual tests can set expectations.
type mockKMSServiceWithDEK struct {
	mock.Mock
	dekSvc *MockDEKService
}

// DEK methods - delegate to MockDEKService.
func (m *mockKMSServiceWithDEK) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	return m.dekSvc.CreateDEK(ctx, req)
}

func (m *mockKMSServiceWithDEK) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	return m.dekSvc.GetDEK(ctx, req)
}

func (m *mockKMSServiceWithDEK) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	return m.dekSvc.ListDEKs(ctx, req)
}

func (m *mockKMSServiceWithDEK) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	return m.dekSvc.RotateDEK(ctx, req)
}

func (m *mockKMSServiceWithDEK) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	return m.dekSvc.DeleteDEK(ctx, req)
}

func (m *mockKMSServiceWithDEK) GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	return m.dekSvc.GetDEKStatus(ctx, req)
}

func (m *mockKMSServiceWithDEK) SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	return m.dekSvc.SetDefaultDEK(ctx, req)
}

func (m *mockKMSServiceWithDEK) SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	return m.dekSvc.SetDEKSafety(ctx, req)
}

func (m *mockKMSServiceWithDEK) ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	return m.dekSvc.ClearDEKSafety(ctx, req)
}

func (m *mockKMSServiceWithDEK) GetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	return m.dekSvc.GetReencryptionProgress(ctx, req)
}

// KMS methods - stub implementations to satisfy KMSService interface.
func (m *mockKMSServiceWithDEK) GetSupportedAlgorithms(_ context.Context) ([]string, error) {
	return []string{"AES-256-GCM"}, nil
}

func (m *mockKMSServiceWithDEK) GenerateKey(_ context.Context, _ *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) ImportKey(_ context.Context, _ *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) GetKey(_ context.Context, _ *service.GetKeyRequest) (*service.GetKeyResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) ListKeys(_ context.Context, _ *service.ListKeysRequest) (*service.ListKeysResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) RotateKey(_ context.Context, _ *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) RevokeKey(_ context.Context, _ *service.RevokeKeyRequest) (*service.RevokeKeyResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) DeleteKey(_ context.Context, _ *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) GetKeyMetadata(_ context.Context, _ *service.GetKeyMetadataRequest) (*service.GetKeyMetadataResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) UpdateKeyMetadata(_ context.Context, _ *service.UpdateKeyMetadataRequest) (*service.UpdateKeyMetadataResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) UpdateKeyStatus(_ context.Context, _ *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) SetKeyPolicy(_ context.Context, _ *service.SetKeyPolicyRequest) (*service.SetKeyPolicyResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) GetKeyPolicy(_ context.Context, _ *service.GetKeyPolicyRequest) (*service.GetKeyPolicyResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) ListKeyVersions(_ context.Context, _ *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.StartReencryptionResponse), args.Error(1)
}

func (m *mockKMSServiceWithDEK) BackendStatus(_ context.Context) (*service.BackendStatusResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) BackendTest(_ context.Context) (*service.BackendTestResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) ExportKey(_ context.Context, _ *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) UnwrapKey(_ context.Context, _ *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) SetKeySafety(_ context.Context, _ *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) ClearKeySafety(_ context.Context, _ *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) SafetyCheck(_ context.Context, _ *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) SafetyReport(_ context.Context, _ *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) SetDefaultKey(_ context.Context, _ *service.SetDefaultKeyRequest) (*service.SetDefaultKeyResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) GetPublicKey(_ context.Context, _ *service.GetPublicKeyRequest) (*service.GetPublicKeyResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) DeleteKeyPolicy(_ context.Context, _ *service.DeleteKeyPolicyRequest) (*service.DeleteKeyPolicyResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) SetKeyRotationPolicy(_ context.Context, _ *service.SetKeyRotationPolicyRequest) (*service.SetKeyRotationPolicyResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) GetKeyRotationPolicy(_ context.Context, _ *service.GetKeyRotationPolicyRequest) (*service.GetKeyRotationPolicyResponse, error) {
	return nil, nil
}

func (m *mockKMSServiceWithDEK) DeleteKeyRotationPolicy(_ context.Context, _ *service.DeleteKeyRotationPolicyRequest) (*service.DeleteKeyRotationPolicyResponse, error) {
	return nil, nil
}

// KMS returns the stored KMS mock so that StartReencryption expectations
// registered on kmsSvc are visible across multiple KMS() calls.
func (m *testQuicBaaSWithDEK) KMS() service.KMSService {
	return m.kmsSvc
}

// GetReencryptionProgress implements getting re-encryption progress.
func (m *testQuicBaaSWithDEK) GetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	return m.dekSvc.GetReencryptionProgress(ctx, req)
}

func newTestQuicBaaSWithDEK() *testQuicBaaSWithDEK {
	dekSvc := &MockDEKService{}
	return &testQuicBaaSWithDEK{
		MockQuicBaaS: &MockQuicBaaS{},
		dekSvc:       dekSvc,
		kmsSvc:       &mockKMSServiceWithDEK{dekSvc: dekSvc},
	}
}

// =============================================================================
// DEK Tool Tests - Success Cases
// =============================================================================

func TestDEKToolsCreate(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithDEK()
	now := time.Now()

	mockSvc.dekSvc.On("CreateDEK", mock.Anything, mock.MatchedBy(func(req *service.CreateDEKRequest) bool {
		return req.Path == "tenant-123/" && req.Algorithm == "AES-256-GCM"
	})).Return(&service.CreateDEKResponse{
		DEK: &service.DEKMetadata{
			Path:        "tenant-123/",
			Algorithm:   "AES-256-GCM",
			Version:     1,
			Status:      "active",
			IsDefault:   false,
			CreatedAt:   now,
			Description: "Test DEK",
		},
		Timestamp: now,
	}, nil)

	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"path":        "tenant-123/",
		"algorithm":   "AES-256-GCM",
		"description": "Test DEK",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_dek_create", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")

	dek, ok := resultMap["dek"].(map[string]interface{})
	require.True(t, ok, "dek should be a map")
	assert.Equal(t, "tenant-123/", dek["path"])
	assert.Equal(t, "AES-256-GCM", dek["algorithm"])
	assert.Equal(t, float64(1), dek["version"])

	mockSvc.dekSvc.AssertExpectations(t)
}

func TestDEKToolsGet(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithDEK()
	now := time.Now()

	mockSvc.dekSvc.On("GetDEK", mock.Anything, mock.MatchedBy(func(req *service.GetDEKRequest) bool {
		return req.Path == "tenant-123/"
	})).Return(&service.GetDEKResponse{
		DEK: &service.DEKMetadata{
			Path:      "tenant-123/",
			Algorithm: "AES-256-GCM",
			Version:   2,
			Status:    "active",
			IsDefault: true,
			CreatedAt: now,
		},
		Timestamp: now,
	}, nil)

	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"path": "tenant-123/",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_dek_get", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")

	dek, ok := resultMap["dek"].(map[string]interface{})
	require.True(t, ok, "dek should be a map")
	assert.Equal(t, "tenant-123/", dek["path"])
	assert.Equal(t, float64(2), dek["version"])

	mockSvc.dekSvc.AssertExpectations(t)
}

func TestDEKToolsList(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithDEK()
	now := time.Now()

	mockSvc.dekSvc.On("ListDEKs", mock.Anything, mock.MatchedBy(func(req *service.ListDEKsRequest) bool {
		return req.Prefix == "tenant-" && req.Recursive == true
	})).Return(&service.ListDEKsResponse{
		DEKs: []*service.DEKMetadata{
			{Path: "tenant-1/", Algorithm: "AES-256-GCM", Version: 1, Status: "active", CreatedAt: now},
			{Path: "tenant-2/", Algorithm: "AES-256-GCM", Version: 1, Status: "active", CreatedAt: now},
		},
		TotalCount: 2,
		HasMore:    false,
		Timestamp:  now,
	}, nil)

	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"prefix":    "tenant-",
		"recursive": true,
		"limit":     float64(100),
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_dek_list", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")

	deks, ok := resultMap["deks"].([]interface{})
	require.True(t, ok, "deks should be a slice")
	assert.Len(t, deks, 2)

	mockSvc.dekSvc.AssertExpectations(t)
}

func TestDEKToolsRotate(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithDEK()
	now := time.Now()

	mockSvc.dekSvc.On("RotateDEK", mock.Anything, mock.MatchedBy(func(req *service.RotateDEKRequest) bool {
		return req.Path == "tenant-123/" && req.Background == true
	})).Return(&service.RotateDEKResponse{
		Path:       "tenant-123/",
		OldVersion: 1,
		NewVersion: 2,
		Timestamp:  now,
	}, nil)

	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"path":       "tenant-123/",
		"background": true,
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_dek_rotate", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	assert.Equal(t, "tenant-123/", resultMap["path"])
	assert.Equal(t, float64(1), resultMap["old_version"])
	assert.Equal(t, float64(2), resultMap["new_version"])

	mockSvc.dekSvc.AssertExpectations(t)
}

func TestDEKToolsDelete(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithDEK()
	now := time.Now()

	mockSvc.dekSvc.On("DeleteDEK", mock.Anything, mock.MatchedBy(func(req *service.DeleteDEKRequest) bool {
		return req.Path == "tenant-123/" && req.Force == true
	})).Return(&service.DeleteDEKResponse{
		Path:      "tenant-123/",
		Deleted:   true,
		Timestamp: now,
	}, nil)

	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"path":  "tenant-123/",
		"force": true,
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_dek_delete", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	assert.Equal(t, "tenant-123/", resultMap["path"])
	assert.Equal(t, true, resultMap["deleted"])

	mockSvc.dekSvc.AssertExpectations(t)
}

func TestDEKToolsStatus(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithDEK()
	now := time.Now()

	mockSvc.dekSvc.On("GetDEKStatus", mock.Anything, mock.MatchedBy(func(req *service.GetDEKStatusRequest) bool {
		return req.Path == "tenant-123/"
	})).Return(&service.GetDEKStatusResponse{
		DEK: &service.DEKMetadata{
			Path:       "tenant-123/",
			Algorithm:  "AES-256-GCM",
			Version:    3,
			Status:     "active",
			UsageCount: 1500,
			BytesUsed:  1024 * 1024 * 50,
			CreatedAt:  now,
		},
		ReencryptionProgress: nil,
		Timestamp:            now,
	}, nil)

	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"path": "tenant-123/",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_dek_status", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")

	dek, ok := resultMap["dek"].(map[string]interface{})
	require.True(t, ok, "dek should be a map")
	assert.Equal(t, "tenant-123/", dek["path"])
	assert.Equal(t, float64(1500), dek["usage_count"])

	mockSvc.dekSvc.AssertExpectations(t)
}

func TestDEKToolsSetDefault(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithDEK()
	now := time.Now()

	mockSvc.dekSvc.On("SetDefaultDEK", mock.Anything, mock.MatchedBy(func(req *service.SetDefaultDEKRequest) bool {
		return req.Path == "tenant-123/"
	})).Return(&service.SetDefaultDEKResponse{
		Path:            "tenant-123/",
		PreviousDefault: "/",
		Timestamp:       now,
	}, nil)

	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"path": "tenant-123/",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_dek_set_default", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	assert.Equal(t, "tenant-123/", resultMap["path"])
	assert.Equal(t, "/", resultMap["previous_default"])

	mockSvc.dekSvc.AssertExpectations(t)
}

func TestDEKToolsSetSafety(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithDEK()
	now := time.Now()

	mockSvc.dekSvc.On("SetDEKSafety", mock.Anything, mock.MatchedBy(func(req *service.SetDEKSafetyRequest) bool {
		return req.Path == "tenant-123/" && req.MaxOperations == 1000000
	})).Return(&service.SetDEKSafetyResponse{
		Path: "tenant-123/",
		Config: &service.DEKSafetyConfig{
			MaxOperations:  1000000,
			MaxBytes:       1024 * 1024 * 1024,
			WarningPercent: 80,
		},
		Timestamp: now,
	}, nil)

	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"path":            "tenant-123/",
		"max_operations":  float64(1000000),
		"max_bytes":       float64(1024 * 1024 * 1024),
		"warning_percent": float64(80),
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_dek_set_safety", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	assert.Equal(t, "tenant-123/", resultMap["path"])

	config, ok := resultMap["config"].(map[string]interface{})
	require.True(t, ok, "config should be a map")
	assert.Equal(t, float64(1000000), config["max_operations"])

	mockSvc.dekSvc.AssertExpectations(t)
}

func TestDEKToolsClearSafety(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithDEK()
	now := time.Now()

	mockSvc.dekSvc.On("ClearDEKSafety", mock.Anything, mock.MatchedBy(func(req *service.ClearDEKSafetyRequest) bool {
		return req.Path == "tenant-123/"
	})).Return(&service.ClearDEKSafetyResponse{
		Path:      "tenant-123/",
		Cleared:   true,
		Timestamp: now,
	}, nil)

	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"path": "tenant-123/",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_dek_clear_safety", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	assert.Equal(t, "tenant-123/", resultMap["path"])
	assert.Equal(t, true, resultMap["cleared"])

	mockSvc.dekSvc.AssertExpectations(t)
}

func TestDEKToolsStartReencryption(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithDEK()
	now := time.Now()

	mockSvc.kmsSvc.On("StartReencryption", mock.Anything, mock.MatchedBy(func(req *service.StartReencryptionRequest) bool {
		return req.Path == "tenant-123/" && req.FromVersion == 1 && req.ToVersion == 2
	})).Return(&service.StartReencryptionResponse{
		OperationID:    "reencrypt-op-123",
		Path:           "tenant-123/",
		Started:        true,
		EstimatedItems: 5000,
		Timestamp:      now,
		StartedAt:      now,
		TotalEntities:  5000,
	}, nil)

	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"path":         "tenant-123/",
		"from_version": float64(1),
		"to_version":   float64(2),
		"batch_size":   float64(100),
		"concurrency":  float64(4),
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_dek_start_reencryption", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	assert.Equal(t, "reencrypt-op-123", resultMap["operation_id"])
	assert.Equal(t, true, resultMap["started"])

	mockSvc.kmsSvc.AssertExpectations(t)
}

func TestDEKToolsGetReencryptionProgress(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithDEK()
	now := time.Now()

	mockSvc.dekSvc.On("GetReencryptionProgress", mock.Anything, mock.MatchedBy(func(req *service.GetReencryptionProgressRequest) bool {
		return req.Path == "tenant-123/" && req.Verbose == true
	})).Return(&service.GetReencryptionProgressResponse{
		Path:        "tenant-123/",
		OperationID: "reencrypt-op-123",
		Progress: &service.ReencryptionProgress{
			Status:          "running",
			InProgress:      true,
			TotalKeys:       5000,
			ProcessedKeys:   2500,
			PercentComplete: 50.0,
			StartedAt:       &now,
		},
		Status:    "running",
		Timestamp: now,
	}, nil)

	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"path":    "tenant-123/",
		"verbose": true,
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_dek_get_reencryption_progress", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	assert.Equal(t, "tenant-123/", resultMap["path"])
	assert.Equal(t, "running", resultMap["status"])

	progress, ok := resultMap["progress"].(map[string]interface{})
	require.True(t, ok, "progress should be a map")
	assert.Equal(t, float64(50), progress["percent_complete"])

	mockSvc.dekSvc.AssertExpectations(t)
}

// =============================================================================
// DEK Tool Tests - Nil Service Error Cases
// =============================================================================

func TestDEKToolsServiceNotAvailable(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	// Create mock without DEK service methods
	mockSvc := &MockQuicBaaS{}
	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	tests := []struct {
		name     string
		tool     string
		args     map[string]interface{}
		errParts []string
	}{
		{
			name: "dek_create not available",
			tool: "quicbaas_dek_create",
			args: map[string]interface{}{
				"path":      "tenant-123/",
				"algorithm": "AES-256-GCM",
			},
			errParts: []string{"not available"},
		},
		{
			name:     "dek_get not available",
			tool:     "quicbaas_dek_get",
			args:     map[string]interface{}{"path": "tenant-123/"},
			errParts: []string{"not available"},
		},
		{
			name:     "dek_list not available",
			tool:     "quicbaas_dek_list",
			args:     map[string]interface{}{"prefix": "tenant-"},
			errParts: []string{"not available"},
		},
		{
			name:     "dek_rotate not available",
			tool:     "quicbaas_dek_rotate",
			args:     map[string]interface{}{"path": "tenant-123/"},
			errParts: []string{"not available"},
		},
		{
			name:     "dek_delete not available",
			tool:     "quicbaas_dek_delete",
			args:     map[string]interface{}{"path": "tenant-123/", "force": true},
			errParts: []string{"not available"},
		},
		{
			name:     "dek_status not available",
			tool:     "quicbaas_dek_status",
			args:     map[string]interface{}{"path": "tenant-123/"},
			errParts: []string{"not available"},
		},
		{
			name:     "dek_set_default not available",
			tool:     "quicbaas_dek_set_default",
			args:     map[string]interface{}{"path": "tenant-123/"},
			errParts: []string{"not available"},
		},
		{
			name:     "dek_set_safety not available",
			tool:     "quicbaas_dek_set_safety",
			args:     map[string]interface{}{"path": "tenant-123/", "max_operations": float64(1000)},
			errParts: []string{"not available"},
		},
		{
			name:     "dek_clear_safety not available",
			tool:     "quicbaas_dek_clear_safety",
			args:     map[string]interface{}{"path": "tenant-123/"},
			errParts: []string{"not available"},
		},
		{
			name: "dek_start_reencryption not available",
			tool: "quicbaas_dek_start_reencryption",
			args: map[string]interface{}{
				"path":         "tenant-123/",
				"from_version": float64(1),
				"to_version":   float64(2),
			},
			errParts: []string{"not available", "nil"},
		},
		{
			name:     "dek_get_reencryption_progress not available",
			tool:     "quicbaas_dek_get_reencryption_progress",
			args:     map[string]interface{}{"path": "tenant-123/"},
			errParts: []string{"not available"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tt.tool, tt.args)
			require.Error(t, err)
			assert.Nil(t, result)

			// Check that error message contains at least one of the expected parts
			errMsg := err.Error()
			foundPart := false
			for _, part := range tt.errParts {
				if assert.Contains(t, errMsg, part) {
					foundPart = true
					break
				}
			}
			if !foundPart && len(tt.errParts) > 0 {
				t.Errorf("expected error to contain one of %v, got: %s", tt.errParts, errMsg)
			}
		})
	}
}

// =============================================================================
// DEK Tool Tests - Error Propagation Cases
// =============================================================================

func TestDEKToolsServiceError(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("dek_create error propagation", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		mockSvc.dekSvc.On("CreateDEK", mock.Anything, mock.Anything).Return(
			nil, errors.New("failed to create DEK: path already exists"))
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_create", map[string]interface{}{
			"path":      "existing/",
			"algorithm": "AES-256-GCM",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path already exists")
		mockSvc.dekSvc.AssertExpectations(t)
	})

	t.Run("dek_get error propagation", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		mockSvc.dekSvc.On("GetDEK", mock.Anything, mock.Anything).Return(
			nil, errors.New("DEK not found"))
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_get", map[string]interface{}{
			"path": "nonexistent/",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "DEK not found")
		mockSvc.dekSvc.AssertExpectations(t)
	})

	t.Run("dek_list error propagation", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		mockSvc.dekSvc.On("ListDEKs", mock.Anything, mock.Anything).Return(
			nil, errors.New("database connection failed"))
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_list", map[string]interface{}{
			"prefix": "tenant-",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "database connection failed")
		mockSvc.dekSvc.AssertExpectations(t)
	})

	t.Run("dek_rotate error propagation", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		mockSvc.dekSvc.On("RotateDEK", mock.Anything, mock.Anything).Return(
			nil, errors.New("rotation in progress"))
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_rotate", map[string]interface{}{
			"path":       "tenant-123/",
			"background": true,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rotation in progress")
		mockSvc.dekSvc.AssertExpectations(t)
	})

	t.Run("dek_delete error propagation", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		mockSvc.dekSvc.On("DeleteDEK", mock.Anything, mock.Anything).Return(
			nil, errors.New("DEK is in use"))
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_delete", map[string]interface{}{
			"path":  "tenant-123/",
			"force": false,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "DEK is in use")
		mockSvc.dekSvc.AssertExpectations(t)
	})

	t.Run("dek_status error propagation", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		mockSvc.dekSvc.On("GetDEKStatus", mock.Anything, mock.Anything).Return(
			nil, errors.New("internal error"))
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_status", map[string]interface{}{
			"path": "tenant-123/",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "internal error")
		mockSvc.dekSvc.AssertExpectations(t)
	})

	t.Run("dek_set_default error propagation", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		mockSvc.dekSvc.On("SetDefaultDEK", mock.Anything, mock.Anything).Return(
			nil, errors.New("path does not exist"))
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_set_default", map[string]interface{}{
			"path": "nonexistent/",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path does not exist")
		mockSvc.dekSvc.AssertExpectations(t)
	})

	t.Run("dek_set_safety error propagation", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		mockSvc.dekSvc.On("SetDEKSafety", mock.Anything, mock.Anything).Return(
			nil, errors.New("invalid safety config"))
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_set_safety", map[string]interface{}{
			"path":           "tenant-123/",
			"max_operations": float64(1000),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid safety config")
		mockSvc.dekSvc.AssertExpectations(t)
	})

	t.Run("dek_clear_safety error propagation", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		mockSvc.dekSvc.On("ClearDEKSafety", mock.Anything, mock.Anything).Return(
			nil, errors.New("no safety config to clear"))
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_clear_safety", map[string]interface{}{
			"path": "tenant-123/",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no safety config to clear")
		mockSvc.dekSvc.AssertExpectations(t)
	})

	t.Run("dek_start_reencryption error propagation", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		mockSvc.kmsSvc.On("StartReencryption", mock.Anything, mock.Anything).Return(
			nil, errors.New("reencryption already in progress"))
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_start_reencryption", map[string]interface{}{
			"path":         "tenant-123/",
			"from_version": float64(1),
			"to_version":   float64(2),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reencryption already in progress")
		mockSvc.kmsSvc.AssertExpectations(t)
	})

	t.Run("dek_get_reencryption_progress error propagation", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		mockSvc.dekSvc.On("GetReencryptionProgress", mock.Anything, mock.Anything).Return(
			nil, errors.New("operation not found"))
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_get_reencryption_progress", map[string]interface{}{
			"path": "tenant-123/",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "operation not found")
		mockSvc.dekSvc.AssertExpectations(t)
	})
}

// =============================================================================
// DEK Tool Tests - Invalid Input Cases
// =============================================================================

func TestDEKToolsInvalidInput(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("dek_create empty path", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		mockSvc.dekSvc.On("CreateDEK", mock.Anything, mock.MatchedBy(func(req *service.CreateDEKRequest) bool {
			return req.Path == ""
		})).Return(nil, errors.New("path is required"))
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_create", map[string]interface{}{
			"path":      "",
			"algorithm": "AES-256-GCM",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("dek_list with excessive limit capped", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		mockSvc.dekSvc.On("ListDEKs", mock.Anything, mock.MatchedBy(func(req *service.ListDEKsRequest) bool {
			// Verify limit is capped at 10000
			return req.Limit <= 10000
		})).Return(&service.ListDEKsResponse{
			DEKs:      []*service.DEKMetadata{},
			Timestamp: time.Now(),
		}, nil)
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_list", map[string]interface{}{
			"prefix": "tenant-",
			"limit":  float64(100000), // Excessive limit
		})
		require.NoError(t, err)
		mockSvc.dekSvc.AssertExpectations(t)
	})

	t.Run("dek_delete without force", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		mockSvc.dekSvc.On("DeleteDEK", mock.Anything, mock.MatchedBy(func(req *service.DeleteDEKRequest) bool {
			return req.Force == false
		})).Return(&service.DeleteDEKResponse{
			Path:      "tenant-123/",
			Deleted:   true,
			Timestamp: time.Now(),
		}, nil)
		registry := NewToolRegistry(mockSvc, log)

		// Test that force=false is passed correctly
		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_delete", map[string]interface{}{
			"path":  "tenant-123/",
			"force": false,
		})
		require.NoError(t, err)
		mockSvc.dekSvc.AssertExpectations(t)
	})

	t.Run("dek_set_safety with zero values", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		mockSvc.dekSvc.On("SetDEKSafety", mock.Anything, mock.MatchedBy(func(req *service.SetDEKSafetyRequest) bool {
			return req.MaxOperations == 0 && req.MaxBytes == 0
		})).Return(&service.SetDEKSafetyResponse{
			Path:      "tenant-123/",
			Timestamp: time.Now(),
		}, nil)
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_set_safety", map[string]interface{}{
			"path":           "tenant-123/",
			"max_operations": float64(0),
			"max_bytes":      float64(0),
		})
		require.NoError(t, err)
		mockSvc.dekSvc.AssertExpectations(t)
	})

	t.Run("dek_start_reencryption with custom batch and concurrency", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		now := time.Now()
		mockSvc.kmsSvc.On("StartReencryption", mock.Anything, mock.MatchedBy(func(req *service.StartReencryptionRequest) bool {
			return req.BatchSize == 500 && req.Concurrency == 8 && req.ThrottleRate == 1000
		})).Return(&service.StartReencryptionResponse{
			OperationID:   "op-123",
			Started:       true,
			Timestamp:     now,
			StartedAt:     now,
			TotalEntities: 10000,
		}, nil)
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_start_reencryption", map[string]interface{}{
			"path":          "tenant-123/",
			"from_version":  float64(1),
			"to_version":    float64(2),
			"batch_size":    float64(500),
			"concurrency":   float64(8),
			"throttle_rate": float64(1000),
		})
		require.NoError(t, err)
		mockSvc.kmsSvc.AssertExpectations(t)
	})

	t.Run("dek_get_reencryption_progress with operation_id", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		now := time.Now()
		mockSvc.dekSvc.On("GetReencryptionProgress", mock.Anything, mock.MatchedBy(func(req *service.GetReencryptionProgressRequest) bool {
			return req.OperationID == "op-specific-123"
		})).Return(&service.GetReencryptionProgressResponse{
			OperationID: "op-specific-123",
			Status:      "completed",
			Timestamp:   now,
		}, nil)
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_get_reencryption_progress", map[string]interface{}{
			"operation_id": "op-specific-123",
		})
		require.NoError(t, err)
		mockSvc.dekSvc.AssertExpectations(t)
	})

	t.Run("dek_create with all optional fields", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		now := time.Now()
		mockSvc.dekSvc.On("CreateDEK", mock.Anything, mock.MatchedBy(func(req *service.CreateDEKRequest) bool {
			return req.Path == "tenant/app/" &&
				req.Algorithm == "CHACHA20-POLY1305" &&
				req.Description == "Production DEK" &&
				req.MaxOperations == 1000000 &&
				req.MaxBytes == 1073741824 &&
				req.WarningPercent == 90
		})).Return(&service.CreateDEKResponse{
			DEK: &service.DEKMetadata{
				Path:        "tenant/app/",
				Algorithm:   "CHACHA20-POLY1305",
				Description: "Production DEK",
				Version:     1,
				CreatedAt:   now,
			},
			Timestamp: now,
		}, nil)
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_create", map[string]interface{}{
			"path":            "tenant/app/",
			"algorithm":       "CHACHA20-POLY1305",
			"description":     "Production DEK",
			"max_operations":  float64(1000000),
			"max_bytes":       float64(1073741824),
			"warning_percent": float64(90),
		})
		require.NoError(t, err)
		mockSvc.dekSvc.AssertExpectations(t)
	})

	t.Run("dek_delete with migrate_to option", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		now := time.Now()
		mockSvc.dekSvc.On("DeleteDEK", mock.Anything, mock.MatchedBy(func(req *service.DeleteDEKRequest) bool {
			return req.Path == "old-tenant/" && req.MigrateTo == "new-tenant/" && req.Force == true
		})).Return(&service.DeleteDEKResponse{
			Path:      "old-tenant/",
			Deleted:   true,
			Timestamp: now,
		}, nil)
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_delete", map[string]interface{}{
			"path":       "old-tenant/",
			"force":      true,
			"migrate_to": "new-tenant/",
		})
		require.NoError(t, err)
		mockSvc.dekSvc.AssertExpectations(t)
	})

	t.Run("dek_start_reencryption with force flag", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		now := time.Now()
		mockSvc.kmsSvc.On("StartReencryption", mock.Anything, mock.MatchedBy(func(req *service.StartReencryptionRequest) bool {
			return req.Force == true
		})).Return(&service.StartReencryptionResponse{
			OperationID:   "op-forced",
			Started:       true,
			Timestamp:     now,
			StartedAt:     now,
			TotalEntities: 5000,
		}, nil)
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_start_reencryption", map[string]interface{}{
			"path":         "tenant-123/",
			"from_version": float64(1),
			"to_version":   float64(2),
			"force":        true,
		})
		require.NoError(t, err)
		mockSvc.kmsSvc.AssertExpectations(t)
	})
}

// =============================================================================
// DEK Tool Tests - Tool Registration Verification
// =============================================================================

func TestDEKToolsToolRegistration(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithDEK()
	registry := NewToolRegistry(mockSvc, log)

	tools := registry.ListTools()
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	expectedTools := []string{
		"quicbaas_dek_create",
		"quicbaas_dek_get",
		"quicbaas_dek_list",
		"quicbaas_dek_rotate",
		"quicbaas_dek_delete",
		"quicbaas_dek_status",
		"quicbaas_dek_set_default",
		"quicbaas_dek_set_safety",
		"quicbaas_dek_clear_safety",
		"quicbaas_dek_start_reencryption",
		"quicbaas_dek_get_reencryption_progress",
	}

	for _, name := range expectedTools {
		assert.True(t, toolNames[name], "expected tool %q to be registered", name)
	}
}

// =============================================================================
// DEK Tool Tests - Edge Cases
// =============================================================================

func TestDEKToolsEdgeCases(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("dek_rotate without background flag defaults correctly", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		now := time.Now()
		mockSvc.dekSvc.On("RotateDEK", mock.Anything, mock.MatchedBy(func(req *service.RotateDEKRequest) bool {
			// Background should be false when not specified (default)
			return req.Path == "tenant-123/" && req.Background == false
		})).Return(&service.RotateDEKResponse{
			Path:       "tenant-123/",
			OldVersion: 1,
			NewVersion: 2,
			Timestamp:  now,
		}, nil)
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_rotate", map[string]interface{}{
			"path": "tenant-123/",
			// background not specified
		})
		require.NoError(t, err)
		mockSvc.dekSvc.AssertExpectations(t)
	})

	t.Run("dek_list without optional parameters", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		now := time.Now()
		mockSvc.dekSvc.On("ListDEKs", mock.Anything, mock.MatchedBy(func(req *service.ListDEKsRequest) bool {
			return req.Prefix == "" && req.Recursive == false && req.Limit == 0
		})).Return(&service.ListDEKsResponse{
			DEKs:      []*service.DEKMetadata{},
			Timestamp: now,
		}, nil)
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_list", map[string]interface{}{})
		require.NoError(t, err)
		mockSvc.dekSvc.AssertExpectations(t)
	})

	t.Run("dek_get_reencryption_progress without verbose", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		now := time.Now()
		mockSvc.dekSvc.On("GetReencryptionProgress", mock.Anything, mock.MatchedBy(func(req *service.GetReencryptionProgressRequest) bool {
			return req.Verbose == false
		})).Return(&service.GetReencryptionProgressResponse{
			Status:    "idle",
			Timestamp: now,
		}, nil)
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_get_reencryption_progress", map[string]interface{}{
			"path": "tenant-123/",
		})
		require.NoError(t, err)
		mockSvc.dekSvc.AssertExpectations(t)
	})

	t.Run("dek_start_reencryption with default batch and concurrency", func(t *testing.T) {
		mockSvc := newTestQuicBaaSWithDEK()
		now := time.Now()
		mockSvc.kmsSvc.On("StartReencryption", mock.Anything, mock.MatchedBy(func(req *service.StartReencryptionRequest) bool {
			// Default values: batch_size=100, concurrency=4, throttle_rate=0
			return req.BatchSize == 100 && req.Concurrency == 4 && req.ThrottleRate == 0
		})).Return(&service.StartReencryptionResponse{
			OperationID:   "op-default",
			Started:       true,
			Timestamp:     now,
			StartedAt:     now,
			TotalEntities: 1000,
		}, nil)
		registry := NewToolRegistry(mockSvc, log)

		_, err := registry.ExecuteTool(ctx, "quicbaas_dek_start_reencryption", map[string]interface{}{
			"from_version": float64(1),
			"to_version":   float64(2),
			// batch_size, concurrency, throttle_rate not specified - should use defaults
		})
		require.NoError(t, err)
		mockSvc.kmsSvc.AssertExpectations(t)
	})
}

// =============================================================================
// DEK Tool Tests - Start Reencryption Nil KMS
// =============================================================================

// TestDEKToolsStartReencryptionNilKMSService verifies that executing
// quicbaas_dek_start_reencryption returns an error when the KMS service is not
// configured. The live code routes StartReencryption through KMS().StartReencryption,
// so a nil KMS → service unavailable.
func TestDEKToolsStartReencryptionNilKMSService(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	// MockQuicBaaS.KMS() returns nil (via embedded mockService.KMS()), so
	// getDEKService() → getKMSService() returns ErrServiceUnavailable.
	mockSvc := &MockQuicBaaS{}
	registry := NewToolRegistry(mockSvc, log)

	_, err = registry.ExecuteTool(context.Background(), "quicbaas_dek_start_reencryption", map[string]interface{}{
		"path":         "tenant-123/",
		"from_version": float64(1),
		"to_version":   float64(2),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}
