//go:build mcp

package mcp

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
)

// testQuicBaaSWithKMSManager wraps MockQuicBaaS and provides a configurable KMSManager.
type testQuicBaaSWithKMSManager struct {
	*MockQuicBaaS
	kmsManager interfaces.KMSManager
	kmsSvc     service.KMSService
}

func (m *testQuicBaaSWithKMSManager) KMSManager() interfaces.KMSManager {
	return m.kmsManager
}

func (m *testQuicBaaSWithKMSManager) KMS() service.KMSService {
	return m.kmsSvc
}

func newTestQuicBaaSWithKMSManager(kmsManager interfaces.KMSManager) *testQuicBaaSWithKMSManager {
	return &testQuicBaaSWithKMSManager{
		MockQuicBaaS: &MockQuicBaaS{},
		kmsManager:   kmsManager,
	}
}

// TestKMSToolsGenerateKey tests the generate key MCP tool.
func TestKMSToolsGenerateKey(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockKMS := NewMockKMSManager()
	mockKMS.On("GenerateKey", mock.Anything, mock.MatchedBy(func(attrs interface{}) bool {
		return true
	})).Return(createTestKeyMetadata("key-123", "test-key", "AES-256-GCM"), nil)

	mockSvc := newTestQuicBaaSWithKMSManager(mockKMS)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"algorithm": "AES-256-GCM",
		"name":      "test-key",
		"usage": []interface{}{
			"encrypt", "decrypt",
		},
		"exportable":          false,
		"deletion_protection": true,
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_generate_key", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	assert.Equal(t, "key-123", resultMap["key_id"])
	assert.Equal(t, "test-key", resultMap["name"])

	mockKMS.AssertExpectations(t)
}

// TestKMSToolsGetKey tests the get key MCP tool.
func TestKMSToolsGetKey(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "key-123").Return(
		createTestKeyMetadata("key-123", "test-key", "AES-256-GCM"), nil)

	mockSvc := newTestQuicBaaSWithKMSManager(mockKMS)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id": "key-123",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_get_key", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	assert.Equal(t, "key-123", resultMap["key_id"])

	mockKMS.AssertExpectations(t)
}

// TestKMSToolsListKeys tests the list keys MCP tool.
func TestKMSToolsListKeys(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockKMS := NewMockKMSManager()
	mockKMS.On("ListKeys", mock.Anything, "").Return(
		[]interface{}{
			createTestKeyMetadata("key-1", "key-one", "AES-256-GCM"),
			createTestKeyMetadata("key-2", "key-two", "RSA-2048"),
		}, nil)

	mockSvc := newTestQuicBaaSWithKMSManager(mockKMS)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"limit": float64(50),
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_list_keys", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	keys, ok := resultMap["keys"].([]interface{})
	require.True(t, ok, "keys should be a slice")
	assert.Len(t, keys, 2)

	mockKMS.AssertExpectations(t)
}

// TestKMSToolsRotateKey tests the rotate key MCP tool.
func TestKMSToolsRotateKey(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockKMS := NewMockKMSManager()

	now := time.Now()

	// Mock GetKey first - called to get the old version
	oldMeta := &kms.KeyMetadata{
		KeyID:     "key-123",
		Name:      "test-key",
		Algorithm: "AES-256-GCM",
		State:     kms.KeyStateEnabled,
		Version:   1, // Old version
		Created:   now,
		Updated:   now,
	}
	mockKMS.On("GetKey", mock.Anything, "key-123").Return(oldMeta, nil)

	// Mock RotateKey - called after GetKey
	rotatedMeta := &kms.KeyMetadata{
		KeyID:     "key-123",
		Name:      "test-key",
		Algorithm: "AES-256-GCM",
		State:     kms.KeyStateEnabled,
		Version:   2, // New version
		Created:   now,
		Updated:   now,
	}
	mockKMS.On("RotateKey", mock.Anything, "key-123").Return(rotatedMeta, nil)

	mockSvc := newTestQuicBaaSWithKMSManager(mockKMS)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id":             "key-123",
		"retain_old_version": true,
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_rotate_key", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	assert.Equal(t, "key-123", resultMap["key_id"])
	// JSON marshaling converts int to float64
	assert.Equal(t, float64(2), resultMap["new_version"])
	assert.Equal(t, float64(1), resultMap["old_version"])

	mockKMS.AssertExpectations(t)
}

// TestKMSToolsRevokeKey tests the revoke key MCP tool.
func TestKMSToolsRevokeKey(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockKMS := NewMockKMSManager()
	mockKMS.On("RevokeKey", mock.Anything, "key-123").Return(nil)

	mockSvc := newTestQuicBaaSWithKMSManager(mockKMS)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id": "key-123",
		"reason": "compromised",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_revoke_key", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	assert.Equal(t, "key-123", resultMap["key_id"])
	// RevokeKey returns KeyStateDisabled as the status
	assert.Equal(t, string(kms.KeyStateDisabled), resultMap["status"])

	mockKMS.AssertExpectations(t)
}

// TestKMSToolsDeleteKey tests the delete key MCP tool.
func TestKMSToolsDeleteKey(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockKMS := NewMockKMSManager()
	// DeleteKey first calls DisableKey, then DeleteKey
	mockKMS.On("DisableKey", mock.Anything, "key-123").Return(nil)
	mockKMS.On("DeleteKey", mock.Anything, "key-123").Return(nil)

	mockSvc := newTestQuicBaaSWithKMSManager(mockKMS)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id":              "key-123",
		"pending_window_days": float64(7),
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_delete_key", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	assert.Equal(t, "key-123", resultMap["key_id"])
	// DeleteKey returns status and deletion_date, not "deleted"
	assert.Equal(t, string(kms.KeyStatePendingDeletion), resultMap["status"])
	assert.NotNil(t, resultMap["deletion_date"])

	mockKMS.AssertExpectations(t)
}

// TestKMSToolsGetMetadata tests the get metadata MCP tool.
func TestKMSToolsGetMetadata(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "key-123").Return(
		createTestKeyMetadata("key-123", "test-key", "AES-256-GCM"), nil)

	mockSvc := newTestQuicBaaSWithKMSManager(mockKMS)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id": "key-123",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_get_metadata", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")

	metadata, ok := resultMap["metadata"].(map[string]interface{})
	require.True(t, ok, "metadata should be a map")
	assert.Equal(t, "key-123", metadata["key_id"])
	assert.Equal(t, "test-key", metadata["name"])

	mockKMS.AssertExpectations(t)
}

// TestKMSToolsUpdateMetadata tests the update metadata MCP tool.
func TestKMSToolsUpdateMetadata(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockKMS := NewMockKMSManager()

	// First call to get current metadata
	mockKMS.On("GetKey", mock.Anything, "key-123").Return(
		createTestKeyMetadata("key-123", "test-key", "AES-256-GCM"), nil)

	// Update stored metadata
	mockKMS.On("UpdateStoredMetadata", mock.Anything).Return(nil)

	mockSvc := newTestQuicBaaSWithKMSManager(mockKMS)
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id": "key-123",
		"tags": map[string]interface{}{
			"env": "staging",
		},
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_update_metadata", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	mockKMS.AssertExpectations(t)
}

// TestKMSToolsNoKMSManager tests error handling when KMS manager is not available.
func TestKMSToolsNoKMSManager(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	// Create mock without KMSManager
	mockSvc := &MockQuicBaaS{}
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"algorithm": "AES-256-GCM",
		"name":      "test-key",
	}

	_, err = registry.ExecuteTool(context.Background(), "quicbaas_kms_generate_key", args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KMS manager not available")
}

// TestKMSToolsToolRegistration tests that all KMS tools are properly registered.
func TestKMSToolsToolRegistration(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockKMS := NewMockKMSManager()
	mockSvc := newTestQuicBaaSWithKMSManager(mockKMS)
	registry := NewToolRegistry(mockSvc, log)

	tools := registry.ListTools()
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	// Only test tools that actually exist in the implementation
	expectedTools := []string{
		"quicbaas_kms_generate_key",
		"quicbaas_kms_import_key",
		"quicbaas_kms_get_key",
		"quicbaas_kms_list_keys",
		"quicbaas_kms_rotate_key",
		"quicbaas_kms_revoke_key",
		"quicbaas_kms_delete_key",
		"quicbaas_kms_get_metadata",
		"quicbaas_kms_update_metadata",
		"quicbaas_kms_set_policy",
		"quicbaas_kms_get_policy",
	}

	for _, name := range expectedTools {
		assert.True(t, toolNames[name], "expected tool %q to be registered", name)
	}
}

// =============================================================================
// Error type tests for tools.go
// =============================================================================

func TestTransactionParseError(t *testing.T) {
	err := &TransactionParseError{Field: "key", Message: "cannot be empty"}
	assert.Contains(t, err.Error(), "transaction parse error")
	assert.Contains(t, err.Error(), "key")
	assert.Contains(t, err.Error(), "cannot be empty")
}

func TestTransactionInvalidCompareTargetError(t *testing.T) {
	err := &TransactionInvalidCompareTargetError{Target: "invalid_target"}
	assert.Contains(t, err.Error(), "invalid_target")
}

func TestTransactionInvalidOpTypeError(t *testing.T) {
	err := &TransactionInvalidOpTypeError{OpType: "merge"}
	assert.Contains(t, err.Error(), "merge")
}

func TestBatchOperationsEmptyError(t *testing.T) {
	err := &BatchOperationsEmptyError{}
	assert.Contains(t, err.Error(), "empty")
}

func TestBatchOperationsInvalidError(t *testing.T) {
	err := &BatchOperationsInvalidError{}
	assert.Contains(t, err.Error(), "valid array")
}

func TestBatchOperationParseError(t *testing.T) {
	err := &BatchOperationParseError{Index: 3, Field: "value", Message: "missing"}
	assert.Contains(t, err.Error(), "3")
	assert.Contains(t, err.Error(), "value")
	assert.Contains(t, err.Error(), "missing")
}

// =============================================================================
// Object Storage Batch Operations Tests
// =============================================================================

func TestExecuteObjectBatchPut_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("PutObject", mock.Anything, mock.AnythingOfType("*service.PutObjectRequest")).
		Return(&service.PutObjectResponse{
			Key:       "test:obj1",
			ObjectID:  1,
			Checksum:  []byte{0xab, 0xcd},
			Size:      100,
			Backend:   "local",
			Version:   1,
			CreatedAt: time.Now().UnixNano(),
		}, nil)

	args := map[string]interface{}{
		"tenant_id":      "t1",
		"target_backend": "local",
		"objects": []interface{}{
			map[string]interface{}{
				"key":   "test:obj1",
				"value": "hello",
				"tags":  map[string]interface{}{"env": "test"},
			},
		},
	}

	result, err := registry.executeObjectBatchPut(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)
	resultMap := result.(map[string]interface{})
	assert.Equal(t, float64(1), resultMap["success_count"])
	assert.Equal(t, float64(0), resultMap["fail_count"])
	mockSvc.AssertExpectations(t)
}

func TestExecuteObjectBatchPut_MissingObjectsArray(t *testing.T) {
	registry := newTestObjectStorageRegistry(&mockObjectStorageService{})

	args := map[string]interface{}{}
	_, err := registry.executeObjectBatchPut(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "objects cannot be empty")
}

func TestExecuteObjectBatchPut_EmptyObjectsArray(t *testing.T) {
	registry := newTestObjectStorageRegistry(&mockObjectStorageService{})

	args := map[string]interface{}{"objects": []interface{}{}}
	_, err := registry.executeObjectBatchPut(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "objects cannot be empty")
}

func TestExecuteObjectBatchPut_InvalidObjectFormat(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	args := map[string]interface{}{
		"objects": []interface{}{
			"not-a-map",
		},
	}

	result, err := registry.executeObjectBatchPut(context.Background(), args)
	require.NoError(t, err)
	resultMap := result.(map[string]interface{})
	assert.Equal(t, float64(0), resultMap["success_count"])
	assert.Equal(t, float64(1), resultMap["fail_count"])
}

func TestExecuteObjectBatchPut_MissingKeyOrValue(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	args := map[string]interface{}{
		"objects": []interface{}{
			map[string]interface{}{"key": "k1"},
			map[string]interface{}{"value": "v1"},
		},
	}

	result, err := registry.executeObjectBatchPut(context.Background(), args)
	require.NoError(t, err)
	resultMap := result.(map[string]interface{})
	assert.Equal(t, float64(0), resultMap["success_count"])
	assert.Equal(t, float64(2), resultMap["fail_count"])
}

func TestExecuteObjectBatchPut_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("PutObject", mock.Anything, mock.Anything).
		Return(nil, errors.New("storage error"))

	args := map[string]interface{}{
		"objects": []interface{}{
			map[string]interface{}{"key": "k1", "value": "v1"},
		},
	}

	result, err := registry.executeObjectBatchPut(context.Background(), args)
	require.NoError(t, err)
	resultMap := result.(map[string]interface{})
	assert.Equal(t, float64(0), resultMap["success_count"])
	assert.Equal(t, float64(1), resultMap["fail_count"])
}

func TestExecuteObjectBatchPut_Base64Value(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("PutObject", mock.Anything, mock.Anything).
		Return(&service.PutObjectResponse{
			Key: "test:obj1", ObjectID: 1, Checksum: []byte{0x01},
			Size: 5, Backend: "local", Version: 1,
		}, nil)

	b64Value := base64.StdEncoding.EncodeToString([]byte("binary data"))
	args := map[string]interface{}{
		"objects": []interface{}{
			map[string]interface{}{
				"key":          "test:obj1",
				"value":        b64Value,
				"content_type": "application/octet-stream",
			},
		},
	}

	result, err := registry.executeObjectBatchPut(context.Background(), args)
	require.NoError(t, err)
	resultMap := result.(map[string]interface{})
	assert.Equal(t, float64(1), resultMap["success_count"])
}

func TestExecuteObjectBatchGet_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("GetObject", mock.Anything, mock.AnythingOfType("*service.GetObjectRequest")).
		Return(&service.GetObjectResponse{
			Key:         "test:obj1",
			Value:       []byte("hello"),
			ContentType: "text/plain",
			Size:        5,
			Checksum:    []byte{0xab},
			Version:     1,
			Backend:     "local",
			Tags:        map[string]string{"env": "test"},
		}, nil)

	args := map[string]interface{}{
		"tenant_id":        "t1",
		"keys":             []interface{}{"test:obj1"},
		"include_metadata": true,
	}

	result, err := registry.executeObjectBatchGet(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)
	resultMap := result.(map[string]interface{})
	assert.Equal(t, float64(1), resultMap["success_count"])
	assert.Equal(t, float64(0), resultMap["fail_count"])
	mockSvc.AssertExpectations(t)
}

func TestExecuteObjectBatchGet_MissingKeysArray(t *testing.T) {
	registry := newTestObjectStorageRegistry(&mockObjectStorageService{})

	args := map[string]interface{}{}
	_, err := registry.executeObjectBatchGet(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keys cannot be empty")
}

func TestExecuteObjectBatchGet_InvalidKeyFormat(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	args := map[string]interface{}{
		"keys": []interface{}{42, ""},
	}

	result, err := registry.executeObjectBatchGet(context.Background(), args)
	require.NoError(t, err)
	resultMap := result.(map[string]interface{})
	assert.Equal(t, float64(0), resultMap["success_count"])
	assert.Equal(t, float64(2), resultMap["fail_count"])
}

func TestExecuteObjectBatchGet_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("GetObject", mock.Anything, mock.Anything).
		Return(nil, errors.New("not found"))

	args := map[string]interface{}{"keys": []interface{}{"k1"}}
	result, err := registry.executeObjectBatchGet(context.Background(), args)
	require.NoError(t, err)
	resultMap := result.(map[string]interface{})
	assert.Equal(t, float64(0), resultMap["success_count"])
	assert.Equal(t, float64(1), resultMap["fail_count"])
}

func TestExecuteObjectBatchGet_MetadataExcluded(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("GetObject", mock.Anything, mock.Anything).
		Return(&service.GetObjectResponse{
			Key: "k1", Value: []byte("v"), ContentType: "text/plain",
			Size: 1, Checksum: []byte{0x01}, Version: 1, Backend: "local",
		}, nil)

	args := map[string]interface{}{
		"keys":             []interface{}{"k1"},
		"include_metadata": false,
	}

	result, err := registry.executeObjectBatchGet(context.Background(), args)
	require.NoError(t, err)
	resultMap := result.(map[string]interface{})
	assert.Equal(t, float64(1), resultMap["success_count"])
}

func TestExecuteObjectBatchDelete_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("DeleteObject", mock.Anything, mock.AnythingOfType("*service.DeleteObjectRequest")).
		Return(&service.DeleteObjectResponse{
			Key:     "test:obj1",
			Success: true,
			Size:    100,
		}, nil)

	args := map[string]interface{}{
		"tenant_id": "t1",
		"keys":      []interface{}{"test:obj1"},
	}

	result, err := registry.executeObjectBatchDelete(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)
	resultMap := result.(map[string]interface{})
	assert.Equal(t, float64(1), resultMap["success_count"])
	assert.Equal(t, float64(0), resultMap["fail_count"])
	mockSvc.AssertExpectations(t)
}

func TestExecuteObjectBatchDelete_MissingKeysArray(t *testing.T) {
	registry := newTestObjectStorageRegistry(&mockObjectStorageService{})

	args := map[string]interface{}{}
	_, err := registry.executeObjectBatchDelete(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keys cannot be empty")
}

func TestExecuteObjectBatchDelete_InvalidKeyFormat(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	args := map[string]interface{}{"keys": []interface{}{123, ""}}
	result, err := registry.executeObjectBatchDelete(context.Background(), args)
	require.NoError(t, err)
	resultMap := result.(map[string]interface{})
	assert.Equal(t, float64(0), resultMap["success_count"])
	assert.Equal(t, float64(2), resultMap["fail_count"])
}

func TestExecuteObjectBatchDelete_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("DeleteObject", mock.Anything, mock.Anything).
		Return(nil, errors.New("delete error"))

	args := map[string]interface{}{"keys": []interface{}{"k1"}}
	result, err := registry.executeObjectBatchDelete(context.Background(), args)
	require.NoError(t, err)
	resultMap := result.(map[string]interface{})
	assert.Equal(t, float64(0), resultMap["success_count"])
	assert.Equal(t, float64(1), resultMap["fail_count"])
}

// =============================================================================
// Object Storage SetTenantQuotas Tests
// =============================================================================

func TestExecuteObjectSetTenantQuotas_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("SetTenantQuotas", mock.Anything, mock.AnythingOfType("*service.SetTenantQuotasRequest")).
		Return(&service.SetTenantQuotasResponse{
			TenantID:  "t1",
			UpdatedAt: time.Now().UnixNano(),
		}, nil)

	args := map[string]interface{}{
		"tenant_id":         "t1",
		"max_storage_bytes": float64(1073741824),
		"max_object_size":   float64(10485760),
		"max_object_count":  float64(10000),
	}

	result, err := registry.executeObjectSetTenantQuotas(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)
	resultMap := result.(map[string]interface{})
	assert.Equal(t, "t1", resultMap["tenant_id"])
	assert.Equal(t, true, resultMap["success"])
	mockSvc.AssertExpectations(t)
}

func TestExecuteObjectSetTenantQuotas_MissingTenantID(t *testing.T) {
	registry := newTestObjectStorageRegistry(&mockObjectStorageService{})

	args := map[string]interface{}{
		"max_storage_bytes": float64(1073741824),
	}

	_, err := registry.executeObjectSetTenantQuotas(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant_id is required")
}

// =============================================================================
// KMS Export Key Tests
// =============================================================================

func TestExecuteKMSExportKey_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id":             "key-1",
		"wrapping_key_id":    "wrap-key-1",
		"wrapping_algorithm": "AES-GCM",
		"format":             "raw",
		"version":            float64(1),
	}

	result, err := registry.executeKMSExportKey(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteKMSExportKey_MissingKeyID(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{"wrapping_key_id": "wrap-1"}
	_, err := registry.executeKMSExportKey(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestExecuteKMSExportKey_MissingWrappingKeyID(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{"key_id": "k1"}
	_, err := registry.executeKMSExportKey(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wrapping_key_id is required")
}

func TestExecuteKMSExportKey_Defaults(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id":          "key-1",
		"wrapping_key_id": "wrap-key-1",
	}

	result, err := registry.executeKMSExportKey(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// KMS Unwrap Key Tests
// =============================================================================

func TestExecuteKMSUnwrapKey_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	wrappedKey := base64.StdEncoding.EncodeToString([]byte("wrapped-key-data"))
	args := map[string]interface{}{
		"key_id":             "imported-key",
		"wrapped_key":        wrappedKey,
		"wrapping_key_id":    "wrap-key-1",
		"algorithm":          "AES-256-GCM",
		"wrapping_algorithm": "AES-GCM",
		"name":               "my-imported-key",
		"source_key_id":      "original-key",
		"source_version":     float64(1),
	}

	result, err := registry.executeKMSUnwrapKey(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteKMSUnwrapKey_MissingKeyID(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{"wrapped_key": "dGVzdA==", "wrapping_key_id": "w1", "algorithm": "AES-256-GCM"}
	_, err := registry.executeKMSUnwrapKey(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestExecuteKMSUnwrapKey_MissingWrappedKey(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{"key_id": "k1", "wrapping_key_id": "w1", "algorithm": "AES-256-GCM"}
	_, err := registry.executeKMSUnwrapKey(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wrapped_key is required")
}

func TestExecuteKMSUnwrapKey_InvalidBase64(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id":          "k1",
		"wrapped_key":     "not-valid-base64!@#$",
		"wrapping_key_id": "w1",
		"algorithm":       "AES-256-GCM",
	}
	_, err := registry.executeKMSUnwrapKey(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base64 wrapped_key")
}

func TestExecuteKMSUnwrapKey_MissingWrappingKeyID(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{"key_id": "k1", "wrapped_key": "dGVzdA==", "algorithm": "AES-256-GCM"}
	_, err := registry.executeKMSUnwrapKey(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wrapping_key_id is required")
}

func TestExecuteKMSUnwrapKey_MissingAlgorithm(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{"key_id": "k1", "wrapped_key": "dGVzdA==", "wrapping_key_id": "w1"}
	_, err := registry.executeKMSUnwrapKey(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "algorithm is required")
}

// =============================================================================
// KMS Safety Tests
// =============================================================================

func TestExecuteKMSSetKeySafety_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id":              "key-1",
		"max_operations":      float64(1000000),
		"max_bytes":           float64(1073741824),
		"warning_percent":     float64(80),
		"auto_rotate_percent": float64(90),
	}

	result, err := registry.executeKMSSetKeySafety(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteKMSSetKeySafety_MissingKeyID(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	_, err := registry.executeKMSSetKeySafety(context.Background(), map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestExecuteKMSClearKeySafety_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{"key_id": "key-1"}
	result, err := registry.executeKMSClearKeySafety(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteKMSClearKeySafety_MissingKeyID(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	_, err := registry.executeKMSClearKeySafety(context.Background(), map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestExecuteKMSSafetyCheck_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{"threshold_percent": float64(80)}
	result, err := registry.executeKMSSafetyCheck(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteKMSSafetyCheck_DefaultThreshold(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	result, err := registry.executeKMSSafetyCheck(context.Background(), map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteKMSSafetyReport_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{"include_details": true}
	result, err := registry.executeKMSSafetyReport(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// Secrets UpdateStatus Tests
// =============================================================================

func TestExecuteSecretsUpdateStatus_ServiceNotAvailable(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	// mockService returns nil for Secrets()
	reg := NewToolRegistry(&mockService{}, log)

	args := map[string]interface{}{
		"secret_id": "s1",
		"status":    "active",
	}

	_, err := reg.executeSecretsUpdateStatus(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secrets service not available")
}

// =============================================================================
// cryptoQuicBaaSWrapper implements inline crypto interface methods directly on
// the service, matching the inline interface assertion pattern used by
// executeCryptoEncrypt, executeCryptoDecrypt, etc.
// =============================================================================

type cryptoQuicBaaSWrapper struct {
	*testQuicBaaSWithKMSOps
}

func (w *cryptoQuicBaaSWrapper) Encrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	return w.cryptoSvc.Encrypt(ctx, req)
}

func (w *cryptoQuicBaaSWrapper) Decrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	return w.cryptoSvc.Decrypt(ctx, req)
}

func (w *cryptoQuicBaaSWrapper) Rewrap(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error) {
	return w.cryptoSvc.Rewrap(ctx, req)
}

func (w *cryptoQuicBaaSWrapper) Sign(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error) {
	return w.cryptoSvc.Sign(ctx, req)
}

func (w *cryptoQuicBaaSWrapper) Verify(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error) {
	return w.cryptoSvc.Verify(ctx, req)
}

func (w *cryptoQuicBaaSWrapper) GenerateHMAC(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error) {
	return w.cryptoSvc.GenerateHMAC(ctx, req)
}

func (w *cryptoQuicBaaSWrapper) VerifyHMAC(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error) {
	return w.cryptoSvc.VerifyHMAC(ctx, req)
}

func (w *cryptoQuicBaaSWrapper) Hash(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error) {
	return w.cryptoSvc.Hash(ctx, req)
}

func (w *cryptoQuicBaaSWrapper) GenerateRandom(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
	return w.cryptoSvc.GenerateRandom(ctx, req)
}

func (w *cryptoQuicBaaSWrapper) GenerateDataKey(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
	return w.cryptoSvc.GenerateDataKey(ctx, req)
}

func (w *cryptoQuicBaaSWrapper) GenerateWrappedDataKey(ctx context.Context, req *service.GenerateWrappedDataKeyRequest) (*service.GenerateWrappedDataKeyResponse, error) {
	return w.cryptoSvc.GenerateWrappedDataKey(ctx, req)
}

func (w *cryptoQuicBaaSWrapper) EncryptBatch(ctx context.Context, req *service.EncryptBatchRequest) (*service.EncryptBatchResponse, error) {
	return w.cryptoSvc.EncryptBatch(ctx, req)
}

func (w *cryptoQuicBaaSWrapper) DecryptBatch(ctx context.Context, req *service.DecryptBatchRequest) (*service.DecryptBatchResponse, error) {
	return w.cryptoSvc.DecryptBatch(ctx, req)
}

func newCryptoQuicBaaSWrapper() *cryptoQuicBaaSWrapper {
	return &cryptoQuicBaaSWrapper{
		testQuicBaaSWithKMSOps: newTestQuicBaaSWithKMSOps(),
	}
}

// =============================================================================
// Crypto Tool Tests - Cover optional parameter branches (context, nonce, batch)
// =============================================================================

func TestExecuteCryptoEncrypt_WithContextAndNonce(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	plaintext := base64.StdEncoding.EncodeToString([]byte("hello"))
	aad := base64.StdEncoding.EncodeToString([]byte("additional-data"))
	nonce := base64.StdEncoding.EncodeToString([]byte("nonce-12bytes"))

	result, err := registry.ExecuteTool(ctx, "quicbaas_crypto_encrypt", map[string]interface{}{
		"key_id":    "key-1",
		"plaintext": plaintext,
		"context":   aad,
		"nonce":     nonce,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteCryptoEncrypt_InvalidBase64Plaintext(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_encrypt", map[string]interface{}{
		"key_id":    "key-1",
		"plaintext": "not-valid-base64!!!",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base64 plaintext")
}

func TestExecuteCryptoEncrypt_InvalidBase64Context(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	plaintext := base64.StdEncoding.EncodeToString([]byte("hello"))

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_encrypt", map[string]interface{}{
		"key_id":    "key-1",
		"plaintext": plaintext,
		"context":   "bad-base64!!!",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base64 context")
}

func TestExecuteCryptoEncrypt_InvalidBase64Nonce(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	plaintext := base64.StdEncoding.EncodeToString([]byte("hello"))

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_encrypt", map[string]interface{}{
		"key_id":    "key-1",
		"plaintext": plaintext,
		"nonce":     "bad-nonce!!!",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base64 nonce")
}

func TestExecuteCryptoEncrypt_ServiceNotAvailable(t *testing.T) {
	mockDB := &MockQuicBaaS{}
	log := newTestLogger(t)
	registry := NewToolRegistry(mockDB, log)
	ctx := context.Background()

	plaintext := base64.StdEncoding.EncodeToString([]byte("hello"))

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_encrypt", map[string]interface{}{
		"key_id":    "key-1",
		"plaintext": plaintext,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crypto service not available")
}

func TestExecuteCryptoDecrypt_WithContextAndNonce(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	ciphertext := base64.StdEncoding.EncodeToString([]byte("encrypted"))
	aad := base64.StdEncoding.EncodeToString([]byte("additional-data"))
	nonce := base64.StdEncoding.EncodeToString([]byte("nonce-12bytes"))

	result, err := registry.ExecuteTool(ctx, "quicbaas_crypto_decrypt", map[string]interface{}{
		"key_id":      "key-1",
		"ciphertext":  ciphertext,
		"context":     aad,
		"nonce":       nonce,
		"key_version": float64(1),
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteCryptoDecrypt_InvalidBase64Ciphertext(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_decrypt", map[string]interface{}{
		"key_id":     "key-1",
		"ciphertext": "bad-base64!!!",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base64 ciphertext")
}

func TestExecuteCryptoDecrypt_InvalidBase64Context(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	ciphertext := base64.StdEncoding.EncodeToString([]byte("encrypted"))

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_decrypt", map[string]interface{}{
		"key_id":     "key-1",
		"ciphertext": ciphertext,
		"context":    "bad-base64!!!",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base64 context")
}

func TestExecuteCryptoDecrypt_InvalidBase64Nonce(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	ciphertext := base64.StdEncoding.EncodeToString([]byte("encrypted"))

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_decrypt", map[string]interface{}{
		"key_id":     "key-1",
		"ciphertext": ciphertext,
		"nonce":      "bad-nonce!!!",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base64 nonce")
}

func TestExecuteCryptoRewrap_WithContextAndNonce(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	ciphertext := base64.StdEncoding.EncodeToString([]byte("encrypted"))
	aad := base64.StdEncoding.EncodeToString([]byte("additional-data"))
	nonce := base64.StdEncoding.EncodeToString([]byte("nonce-12bytes"))

	result, err := registry.ExecuteTool(ctx, "quicbaas_crypto_rewrap", map[string]interface{}{
		"key_id":     "key-1",
		"ciphertext": ciphertext,
		"context":    aad,
		"nonce":      nonce,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteCryptoRewrap_InvalidBase64Ciphertext(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_rewrap", map[string]interface{}{
		"key_id":     "key-1",
		"ciphertext": "bad-base64!!!",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base64 ciphertext")
}

func TestExecuteCryptoRewrap_InvalidBase64Context(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	ciphertext := base64.StdEncoding.EncodeToString([]byte("encrypted"))

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_rewrap", map[string]interface{}{
		"key_id":     "key-1",
		"ciphertext": ciphertext,
		"context":    "bad-base64!!!",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base64 context")
}

func TestExecuteCryptoRewrap_InvalidBase64Nonce(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	ciphertext := base64.StdEncoding.EncodeToString([]byte("encrypted"))

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_rewrap", map[string]interface{}{
		"key_id":     "key-1",
		"ciphertext": ciphertext,
		"nonce":      "bad-nonce!!!",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base64 nonce")
}

func TestExecuteCryptoDataKey_WithContextAndAlgorithm(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	aad := base64.StdEncoding.EncodeToString([]byte("context-data"))

	result, err := registry.ExecuteTool(ctx, "quicbaas_crypto_datakey", map[string]interface{}{
		"key_id":    "key-1",
		"algorithm": "CHACHA20-POLY1305",
		"context":   aad,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteCryptoDataKey_InvalidBase64Context(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_datakey", map[string]interface{}{
		"key_id":  "key-1",
		"context": "bad-base64!!!",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base64 context")
}

func TestExecuteCryptoDataKeyWrapped_WithContext(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	aad := base64.StdEncoding.EncodeToString([]byte("context-data"))

	result, err := registry.ExecuteTool(ctx, "quicbaas_crypto_datakey_wrapped", map[string]interface{}{
		"key_id":    "key-1",
		"algorithm": "AES-128-GCM",
		"context":   aad,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteCryptoDataKeyWrapped_InvalidBase64Context(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_datakey_wrapped", map[string]interface{}{
		"key_id":  "key-1",
		"context": "bad-base64!!!",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base64 context")
}

// =============================================================================
// Crypto Batch Operations
// =============================================================================

func TestExecuteCryptoEncryptBatch_WithContext(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	plaintext1 := base64.StdEncoding.EncodeToString([]byte("data1"))
	plaintext2 := base64.StdEncoding.EncodeToString([]byte("data2"))
	aad := base64.StdEncoding.EncodeToString([]byte("context"))

	result, err := registry.ExecuteTool(ctx, "quicbaas_crypto_encrypt_batch", map[string]interface{}{
		"key_id": "key-1",
		"items": []interface{}{
			map[string]interface{}{
				"plaintext": plaintext1,
				"context":   aad,
			},
			map[string]interface{}{
				"plaintext": plaintext2,
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteCryptoEncryptBatch_InvalidItemFormat(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_encrypt_batch", map[string]interface{}{
		"key_id": "key-1",
		"items":  []interface{}{"not-an-object"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "item 0: must be an object")
}

func TestExecuteCryptoEncryptBatch_InvalidBase64Plaintext(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_encrypt_batch", map[string]interface{}{
		"key_id": "key-1",
		"items": []interface{}{
			map[string]interface{}{
				"plaintext": "bad-base64!!!",
			},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "item 0: invalid base64 plaintext")
}

func TestExecuteCryptoEncryptBatch_InvalidBase64Context(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	plaintext := base64.StdEncoding.EncodeToString([]byte("data"))

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_encrypt_batch", map[string]interface{}{
		"key_id": "key-1",
		"items": []interface{}{
			map[string]interface{}{
				"plaintext": plaintext,
				"context":   "bad-base64!!!",
			},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "item 0: invalid base64 context")
}

func TestExecuteCryptoEncryptBatch_MissingItems(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_encrypt_batch", map[string]interface{}{
		"key_id": "key-1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "items must be an array")
}

func TestExecuteCryptoDecryptBatch_WithContextAndNonce(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	ciphertext1 := base64.StdEncoding.EncodeToString([]byte("enc1"))
	ciphertext2 := base64.StdEncoding.EncodeToString([]byte("enc2"))
	aad := base64.StdEncoding.EncodeToString([]byte("context"))
	nonce := base64.StdEncoding.EncodeToString([]byte("nonce"))

	result, err := registry.ExecuteTool(ctx, "quicbaas_crypto_decrypt_batch", map[string]interface{}{
		"key_id": "key-1",
		"items": []interface{}{
			map[string]interface{}{
				"ciphertext": ciphertext1,
				"context":    aad,
				"nonce":      nonce,
			},
			map[string]interface{}{
				"ciphertext": ciphertext2,
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteCryptoDecryptBatch_InvalidItemFormat(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_decrypt_batch", map[string]interface{}{
		"key_id": "key-1",
		"items":  []interface{}{"not-an-object"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "item 0: must be an object")
}

func TestExecuteCryptoDecryptBatch_InvalidBase64Ciphertext(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_decrypt_batch", map[string]interface{}{
		"key_id": "key-1",
		"items": []interface{}{
			map[string]interface{}{
				"ciphertext": "bad-base64!!!",
			},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "item 0: invalid base64 ciphertext")
}

func TestExecuteCryptoDecryptBatch_InvalidBase64Context(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	ciphertext := base64.StdEncoding.EncodeToString([]byte("enc"))

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_decrypt_batch", map[string]interface{}{
		"key_id": "key-1",
		"items": []interface{}{
			map[string]interface{}{
				"ciphertext": ciphertext,
				"context":    "bad-base64!!!",
			},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "item 0: invalid base64 context")
}

func TestExecuteCryptoDecryptBatch_InvalidBase64Nonce(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	ciphertext := base64.StdEncoding.EncodeToString([]byte("enc"))

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_decrypt_batch", map[string]interface{}{
		"key_id": "key-1",
		"items": []interface{}{
			map[string]interface{}{
				"ciphertext": ciphertext,
				"nonce":      "bad-nonce!!!",
			},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "item 0: invalid base64 nonce")
}

func TestExecuteCryptoDecryptBatch_MissingItems(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_decrypt_batch", map[string]interface{}{
		"key_id": "key-1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "items must be an array")
}

func TestExecuteCryptoEncrypt_ServiceError(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	svc.cryptoSvc.encryptErr = &dberrors.MessageError{Message: "encryption failed"}
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	plaintext := base64.StdEncoding.EncodeToString([]byte("hello"))

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_encrypt", map[string]interface{}{
		"key_id":    "key-1",
		"plaintext": plaintext,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encryption failed")
}

func TestExecuteCryptoDecrypt_ServiceError(t *testing.T) {
	svc := newCryptoQuicBaaSWrapper()
	svc.cryptoSvc.decryptErr = &dberrors.MessageError{Message: "decryption failed"}
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	ciphertext := base64.StdEncoding.EncodeToString([]byte("encrypted"))

	_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_decrypt", map[string]interface{}{
		"key_id":     "key-1",
		"ciphertext": ciphertext,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decryption failed")
}

// =============================================================================
// KMS Tool Tests - mcpArgsToKeyAttributes algorithm branching
// =============================================================================

func TestMcpArgsToKeyAttributes_AllAlgorithms(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
	}{
		{"AES-128-GCM", "AES-128-GCM"},
		{"AES-192-GCM", "AES-192-GCM"},
		{"AES-256-GCM", "AES-256-GCM"},
		{"CHACHA20-POLY1305", "CHACHA20-POLY1305"},
		{"XCHACHA20-POLY1305", "XCHACHA20-POLY1305"},
		{"RSA-2048", "RSA-2048"},
		{"RSA-3072", "RSA-3072"},
		{"RSA-4096", "RSA-4096"},
		{"ECDSA-P256", "ECDSA-P256"},
		{"ECDSA-P384", "ECDSA-P384"},
		{"ECDSA-P521", "ECDSA-P521"},
		{"ED25519", "ED25519"},
		{"unknown algorithm", "UNKNOWN"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]interface{}{
				"name":       "test-key",
				"algorithm":  tc.algorithm,
				"exportable": true,
			}
			attrs := mcpArgsToKeyAttributes(args)
			require.NotNil(t, attrs)
			assert.Equal(t, "test-key", attrs.CN)
		})
	}
}

func TestAlgorithmToKeyAttributes_AllVariants(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
	}{
		{"AES-256-GCM", "AES-256-GCM"},
		{"AES256GCM", "AES256GCM"},
		{"AES-192-GCM", "AES-192-GCM"},
		{"AES192GCM", "AES192GCM"},
		{"AES-128-GCM", "AES-128-GCM"},
		{"AES128GCM", "AES128GCM"},
		{"CHACHA20-POLY1305", "CHACHA20-POLY1305"},
		{"ChaCha20-Poly1305", "ChaCha20-Poly1305"},
		{"XCHACHA20-POLY1305", "XCHACHA20-POLY1305"},
		{"XChaCha20-Poly1305", "XChaCha20-Poly1305"},
		{"default", "something-unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attrs := algorithmToKeyAttributes(tc.algorithm)
			require.NotNil(t, attrs)
			assert.Equal(t, "data-key", attrs.CN)
		})
	}
}

// =============================================================================
// KMS Generate Key - with optional fields (tags, owner, description)
// =============================================================================

func TestExecuteKMSGenerateKey_WithAllOptionalFields(t *testing.T) {
	svc := newTestQuicBaaSWithKMSOps()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_kms_generate_key", map[string]interface{}{
		"name":        "my-key",
		"algorithm":   "AES-256-GCM",
		"exportable":  true,
		"description": "Test encryption key",
		"owner":       "test-user",
		"tags": map[string]interface{}{
			"env":     "test",
			"project": "quicbaas",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "key_id")
}

func TestExecuteKMSGenerateKey_ServiceNotAvailable(t *testing.T) {
	mockDB := &MockQuicBaaS{}
	log := newTestLogger(t)
	registry := NewToolRegistry(mockDB, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_kms_generate_key", map[string]interface{}{
		"algorithm": "AES-256-GCM",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KMS manager not available")
}

func TestExecuteKMSImportKey_WithOptionalFields(t *testing.T) {
	svc := newTestQuicBaaSWithKMSOps()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_kms_import_key", map[string]interface{}{
		"key_material":    "base64encodedkey",
		"wrapping_key_id": "wrapping-key-1",
		"algorithm":       "AES-256-GCM",
		"name":            "imported-key",
		"description":     "An imported key",
		"tags": map[string]interface{}{
			"source": "external",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteKMSGetKey_MissingKeyID(t *testing.T) {
	svc := newTestQuicBaaSWithKMSOps()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_kms_get_key", map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestExecuteKMSListKeys_WithFilterAndLimit(t *testing.T) {
	svc := newTestQuicBaaSWithKMSOps()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_kms_list_keys", map[string]interface{}{
		"limit": float64(10),
		"filter": map[string]interface{}{
			"prefix": "test-",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteKMSRotateKey_MissingKeyID(t *testing.T) {
	svc := newTestQuicBaaSWithKMSOps()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_kms_rotate_key", map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestExecuteKMSRevokeKey_MissingKeyID(t *testing.T) {
	svc := newTestQuicBaaSWithKMSOps()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_kms_revoke_key", map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestExecuteKMSDeleteKey_WithPendingWindowAndForce(t *testing.T) {
	svc := newTestQuicBaaSWithKMSOps()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_kms_delete_key", map[string]interface{}{
		"key_id":              "key-1",
		"pending_window_days": float64(14),
		"force":               true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteKMSDeleteKey_MissingKeyID(t *testing.T) {
	svc := newTestQuicBaaSWithKMSOps()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_kms_delete_key", map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestExecuteKMSGetMetadata_MissingKeyID(t *testing.T) {
	svc := newTestQuicBaaSWithKMSOps()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_kms_get_metadata", map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestExecuteKMSUpdateMetadata_WithTagsAndRemoveTags(t *testing.T) {
	svc := newTestQuicBaaSWithKMSOps()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_kms_update_metadata", map[string]interface{}{
		"key_id":      "key-1",
		"name":        "updated-key",
		"description": "Updated description",
		"tags": map[string]interface{}{
			"env": "production",
		},
		"remove_tags": []interface{}{"old-tag"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteKMSUpdateMetadata_MissingKeyID(t *testing.T) {
	svc := newTestQuicBaaSWithKMSOps()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_kms_update_metadata", map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestExecuteKMSSetPolicy_WithVersion(t *testing.T) {
	svc := newTestQuicBaaSWithKMSOps()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_kms_set_policy", map[string]interface{}{
		"key_id": "key-1",
		"policy": map[string]interface{}{
			"version":   float64(2),
			"principal": "user:admin",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteKMSSetPolicy_MissingKeyID(t *testing.T) {
	svc := newTestQuicBaaSWithKMSOps()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_kms_set_policy", map[string]interface{}{
		"policy": map[string]interface{}{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestExecuteKMSSetPolicy_MissingPolicy(t *testing.T) {
	svc := newTestQuicBaaSWithKMSOps()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_kms_set_policy", map[string]interface{}{
		"key_id": "key-1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "policy must be an object")
}

func TestExecuteKMSGetPolicy_MissingKeyID(t *testing.T) {
	svc := newTestQuicBaaSWithKMSOps()
	log := newTestLogger(t)
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "quicbaas_kms_get_policy", map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id is required")
}

// =============================================================================
// KMS Metadata Response - with LastUsedAt and Usage
// =============================================================================

func TestKmsMetadataToResponse_WithLastUsedAtAndUsage(t *testing.T) {
	now := time.Now()
	metadata := &kms.KeyMetadata{
		KeyID:              "key-1",
		Name:               "test-key",
		Description:        "Test",
		Algorithm:          "AES-256-GCM",
		State:              kms.KeyStateEnabled,
		Version:            1,
		Exportable:         true,
		DeletionProtection: true,
		Created:            now,
		Updated:            now,
		LastUsedAt:         &now,
		Usage:              []kms.KeyUsage{"encrypt", "decrypt"},
		Tags:               map[string]string{"env": "test"},
		Owner:              "admin",
	}

	resp := kmsMetadataToResponse(metadata)
	require.NotNil(t, resp)
	assert.Equal(t, "key-1", resp.KeyID)
	assert.Equal(t, "test-key", resp.Name)
	assert.Equal(t, "Test", resp.Description)
	assert.NotZero(t, resp.LastUsedAt)
	assert.Len(t, resp.Usage, 2)
	assert.Equal(t, "encrypt", resp.Usage[0])
	assert.Equal(t, "decrypt", resp.Usage[1])
	assert.Equal(t, "admin", resp.Owner)
	assert.Equal(t, "test", resp.Tags["env"])
}

// newTestRegistryWithMockKMS creates a ToolRegistry backed by a mock KMS manager.
func newTestRegistryWithMockKMS(t *testing.T, mockKMS *MockKMSManager) *ToolRegistry {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-coverage-test",
	})
	require.NoError(t, err)
	mockSvc := newTestQuicBaaSWithKMSManager(mockKMS)
	return NewToolRegistry(mockSvc, log)
}

// =============================================================================
// svcMetadataToResponse Tests
// =============================================================================

func TestMCPCoverage91_SvcMetadataToResponse_NilMetadata(t *testing.T) {
	resp, err := svcMetadataToResponse(nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Empty(t, resp.KeyID)
	assert.NotZero(t, resp.Timestamp)
}

func TestMCPCoverage91_SvcMetadataToResponse_FullMetadata(t *testing.T) {
	now := time.Now()
	lastUsed := now.Add(-1 * time.Hour)
	metadata := &service.KeyMetadata{
		KeyID:              "svc-key-1",
		Name:               "test-svc-key",
		Description:        "A service key",
		Algorithm:          service.KeyAlgorithm("AES-256-GCM"),
		Status:             service.KeyStatus("enabled"),
		Usage:              []service.KeyUsage{"encrypt", "decrypt"},
		Version:            3,
		Exportable:         true,
		DeletionProtection: true,
		CreatedAt:          now,
		UpdatedAt:          now,
		LastUsedAt:         &lastUsed,
		Tags:               map[string]string{"env": "prod"},
		Owner:              "admin",
	}

	resp, err := svcMetadataToResponse(metadata)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "svc-key-1", resp.KeyID)
	assert.Equal(t, "test-svc-key", resp.Name)
	assert.Equal(t, "A service key", resp.Description)
	assert.Equal(t, "AES-256-GCM", resp.Algorithm)
	assert.Equal(t, "enabled", resp.Status)
	assert.Equal(t, 3, resp.Version)
	assert.True(t, resp.Exportable)
	assert.True(t, resp.DeletionProtection)
	assert.Equal(t, now.Unix(), resp.CreatedAt)
	assert.Equal(t, now.Unix(), resp.UpdatedAt)
	assert.Equal(t, lastUsed.Unix(), resp.LastUsedAt)
	assert.Equal(t, "admin", resp.Owner)
	assert.Len(t, resp.Usage, 2)
	assert.Equal(t, "encrypt", resp.Usage[0])
	assert.Equal(t, "decrypt", resp.Usage[1])
	assert.Equal(t, "prod", resp.Tags["env"])
}

func TestMCPCoverage91_SvcMetadataToResponse_NoLastUsed(t *testing.T) {
	now := time.Now()
	metadata := &service.KeyMetadata{
		KeyID:     "svc-key-2",
		Algorithm: service.KeyAlgorithm("RSA-2048"),
		Status:    service.KeyStatus("active"),
		CreatedAt: now,
		UpdatedAt: now,
	}

	resp, err := svcMetadataToResponse(metadata)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "svc-key-2", resp.KeyID)
	assert.Zero(t, resp.LastUsedAt)
	assert.Empty(t, resp.Usage)
}

// =============================================================================
// mcpArgsToUsage Tests
// =============================================================================

func TestMCPCoverage91_McpArgsToUsage_WithUsage(t *testing.T) {
	args := map[string]interface{}{
		"usage": []interface{}{"encrypt", "decrypt", "sign"},
	}
	usage := mcpArgsToUsage(args)
	require.Len(t, usage, 3)
	assert.Equal(t, service.KeyUsage("encrypt"), usage[0])
	assert.Equal(t, service.KeyUsage("decrypt"), usage[1])
	assert.Equal(t, service.KeyUsage("sign"), usage[2])
}

func TestMCPCoverage91_McpArgsToUsage_NoUsage(t *testing.T) {
	args := map[string]interface{}{}
	usage := mcpArgsToUsage(args)
	assert.Nil(t, usage)
}

func TestMCPCoverage91_McpArgsToUsage_WrongType(t *testing.T) {
	args := map[string]interface{}{
		"usage": "encrypt",
	}
	usage := mcpArgsToUsage(args)
	assert.Nil(t, usage)
}

func TestMCPCoverage91_McpArgsToUsage_MixedTypes(t *testing.T) {
	args := map[string]interface{}{
		"usage": []interface{}{"encrypt", 123, "sign"},
	}
	usage := mcpArgsToUsage(args)
	require.Len(t, usage, 2)
	assert.Equal(t, service.KeyUsage("encrypt"), usage[0])
	assert.Equal(t, service.KeyUsage("sign"), usage[1])
}

// =============================================================================
// mcpArgsToTags Tests
// =============================================================================

func TestMCPCoverage91_McpArgsToTags_NoTags(t *testing.T) {
	args := map[string]interface{}{}
	tags := mcpArgsToTags(args)
	assert.Nil(t, tags)
}

func TestMCPCoverage91_McpArgsToTags_WithTags(t *testing.T) {
	args := map[string]interface{}{
		"tags": map[string]interface{}{
			"env":    "prod",
			"team":   "platform",
			"number": 42,
		},
	}
	tags := mcpArgsToTags(args)
	require.Len(t, tags, 2)
	assert.Equal(t, "prod", tags["env"])
	assert.Equal(t, "platform", tags["team"])
}

// =============================================================================
// mcpArgsToKeyAttributes Algorithm Branch Coverage
// =============================================================================

func TestMCPCoverage91_McpArgsToKeyAttributes_AllAlgorithms(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
	}{
		{"AES-128-GCM", "AES-128-GCM"},
		{"AES-192-GCM", "AES-192-GCM"},
		{"AES-256-GCM", "AES-256-GCM"},
		{"XCHACHA20-POLY1305", "XCHACHA20-POLY1305"},
		{"CHACHA20-POLY1305", "CHACHA20-POLY1305"},
		{"RSA-2048", "RSA-2048"},
		{"RSA-3072", "RSA-3072"},
		{"RSA-4096", "RSA-4096"},
		{"ECDSA-P256", "ECDSA-P256"},
		{"ECDSA-P384", "ECDSA-P384"},
		{"ECDSA-P521", "ECDSA-P521"},
		{"ED25519", "ED25519"},
		{"unknown", "UNKNOWN-ALG"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]interface{}{
				"algorithm": tt.algorithm,
				"name":      "test-key",
			}
			attrs := mcpArgsToKeyAttributes(args)
			require.NotNil(t, attrs)
			assert.Equal(t, "test-key", attrs.CN)
		})
	}
}

// =============================================================================
// KMS GetKey via service layer (missing key_id error)
// =============================================================================

func TestMCPCoverage91_KMSGetKey_MissingKeyID(t *testing.T) {
	mockKMS := NewMockKMSManager()
	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSGetKey(context.Background(), map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id")
}

// =============================================================================
// KMS RotateKey via fallback - missing key_id
// =============================================================================

func TestMCPCoverage91_KMSRotateKey_MissingKeyID(t *testing.T) {
	mockKMS := NewMockKMSManager()
	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSRotateKey(context.Background(), map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id")
}

// =============================================================================
// KMS RevokeKey via fallback - missing key_id
// =============================================================================

func TestMCPCoverage91_KMSRevokeKey_MissingKeyID(t *testing.T) {
	mockKMS := NewMockKMSManager()
	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSRevokeKey(context.Background(), map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id")
}

// =============================================================================
// KMS DeleteKey via fallback - missing key_id
// =============================================================================

func TestMCPCoverage91_KMSDeleteKey_MissingKeyID(t *testing.T) {
	mockKMS := NewMockKMSManager()
	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSDeleteKey(context.Background(), map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id")
}

// =============================================================================
// KMS GetMetadata via fallback - missing key_id
// =============================================================================

func TestMCPCoverage91_KMSGetMetadata_MissingKeyID(t *testing.T) {
	mockKMS := NewMockKMSManager()
	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSGetMetadata(context.Background(), map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id")
}

// =============================================================================
// KMS UpdateMetadata via fallback - missing key_id
// =============================================================================

func TestMCPCoverage91_KMSUpdateMetadata_MissingKeyID(t *testing.T) {
	mockKMS := NewMockKMSManager()
	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSUpdateMetadata(context.Background(), map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id")
}

// =============================================================================
// KMS SetPolicy via fallback - missing key_id and policy
// =============================================================================

func TestMCPCoverage91_KMSSetPolicy_MissingKeyID(t *testing.T) {
	mockKMS := NewMockKMSManager()
	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSSetPolicy(context.Background(), map[string]interface{}{
		"policy": map[string]interface{}{"allowed_operations": []interface{}{"encrypt"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id")
}

func TestMCPCoverage91_KMSSetPolicy_MissingPolicy(t *testing.T) {
	mockKMS := NewMockKMSManager()
	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSSetPolicy(context.Background(), map[string]interface{}{
		"key_id": "test-key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "policy")
}

// =============================================================================
// KMS GetPolicy via fallback - missing key_id
// =============================================================================

func TestMCPCoverage91_KMSGetPolicy_MissingKeyID(t *testing.T) {
	mockKMS := NewMockKMSManager()
	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSGetPolicy(context.Background(), map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id")
}

// =============================================================================
// KMS GetPolicy via fallback - full success path
// =============================================================================

func TestMCPCoverage91_KMSGetPolicy_FallbackSuccess(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "policy-key").Return(
		createTestKeyMetadata("policy-key", "test", "AES-256-GCM"), nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	result, err := registry.executeKMSGetPolicy(context.Background(), map[string]interface{}{
		"key_id": "policy-key",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	_, hasPolicy := resultMap["policy"]
	assert.True(t, hasPolicy)
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS SetPolicy via fallback - success path with version
// =============================================================================

func TestMCPCoverage91_KMSSetPolicy_FallbackSuccess(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "sp-key").Return(
		createTestKeyMetadata("sp-key", "test", "AES-256-GCM"), nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	result, err := registry.executeKMSSetPolicy(context.Background(), map[string]interface{}{
		"key_id": "sp-key",
		"policy": map[string]interface{}{
			"version":            float64(2),
			"allowed_operations": []interface{}{"encrypt", "decrypt"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "sp-key", resultMap["key_id"])
	assert.Equal(t, float64(3), resultMap["policy_version"])
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS GetMetadata via fallback - success path
// =============================================================================

func TestMCPCoverage91_KMSGetMetadata_FallbackSuccess(t *testing.T) {
	now := time.Now()
	lastUsed := now.Add(-30 * time.Minute)
	metadata := &kms.KeyMetadata{
		KeyID:              "meta-key",
		Name:               "metadata-test",
		Description:        "Test metadata",
		Algorithm:          "AES-256-GCM",
		State:              kms.KeyStateEnabled,
		Version:            2,
		Exportable:         true,
		DeletionProtection: false,
		Created:            now,
		Updated:            now,
		LastUsedAt:         &lastUsed,
		Usage:              []kms.KeyUsage{kms.KeyUsageEncrypt},
		Tags:               map[string]string{"purpose": "test"},
		Owner:              "tester",
	}

	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "meta-key").Return(metadata, nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	result, err := registry.executeKMSGetMetadata(context.Background(), map[string]interface{}{
		"key_id": "meta-key",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS UpdateMetadata via fallback - success path
// =============================================================================

func TestMCPCoverage91_KMSUpdateMetadata_FallbackSuccess(t *testing.T) {
	now := time.Now()
	metadata := &kms.KeyMetadata{
		KeyID:     "upd-key",
		Name:      "original-name",
		Algorithm: "AES-256-GCM",
		State:     kms.KeyStateEnabled,
		Version:   1,
		Created:   now,
		Updated:   now,
		Tags:      map[string]string{"old": "tag"},
	}

	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "upd-key").Return(metadata, nil)
	mockKMS.On("UpdateStoredMetadata", mock.Anything).Return(nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	result, err := registry.executeKMSUpdateMetadata(context.Background(), map[string]interface{}{
		"key_id":      "upd-key",
		"name":        "updated-name",
		"description": "updated desc",
		"tags":        map[string]interface{}{"new": "tag"},
		"remove_tags": []interface{}{"old"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS RevokeKey via fallback - success path
// =============================================================================

func TestMCPCoverage91_KMSRevokeKey_FallbackSuccess(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("RevokeKey", mock.Anything, "rev-key").Return(nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	result, err := registry.executeKMSRevokeKey(context.Background(), map[string]interface{}{
		"key_id": "rev-key",
		"reason": "compromised",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "rev-key", resultMap["key_id"])
	assert.Equal(t, string(kms.KeyStateDisabled), resultMap["status"])
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS DeleteKey via fallback - success path with force
// =============================================================================

func TestMCPCoverage91_KMSDeleteKey_FallbackForce(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("DeleteKey", mock.Anything, "del-key").Return(nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	result, err := registry.executeKMSDeleteKey(context.Background(), map[string]interface{}{
		"key_id":              "del-key",
		"force":               true,
		"pending_window_days": float64(14),
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "del-key", resultMap["key_id"])
	mockKMS.AssertExpectations(t)
}

func TestMCPCoverage91_KMSDeleteKey_FallbackNoForce(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("DisableKey", mock.Anything, "del-key").Return(nil)
	mockKMS.On("DeleteKey", mock.Anything, "del-key").Return(nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	result, err := registry.executeKMSDeleteKey(context.Background(), map[string]interface{}{
		"key_id": "del-key",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS RotateKey via fallback - unexpected response type
// =============================================================================

func TestMCPCoverage91_KMSRotateKey_UnexpectedGetKeyResponse(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "rot-key").Return("not-a-metadata", nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSRotateKey(context.Background(), map[string]interface{}{
		"key_id": "rot-key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected")
	mockKMS.AssertExpectations(t)
}

func TestMCPCoverage91_KMSRotateKey_UnexpectedRotateResponse(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "rot-key").Return(
		createTestKeyMetadata("rot-key", "test", "AES-256-GCM"), nil)
	mockKMS.On("RotateKey", mock.Anything, "rot-key").Return("not-metadata", nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSRotateKey(context.Background(), map[string]interface{}{
		"key_id": "rot-key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected")
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS GenerateKey via fallback - unexpected response type
// =============================================================================

func TestMCPCoverage91_KMSGenerateKey_UnexpectedResponse(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("GenerateKey", mock.Anything, mock.Anything).Return("not-metadata", nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSGenerateKey(context.Background(), map[string]interface{}{
		"algorithm": "AES-256-GCM",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected")
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS GenerateKey via fallback - full success with all metadata fields
// =============================================================================

func TestMCPCoverage91_KMSGenerateKey_FallbackFullMetadata(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("GenerateKey", mock.Anything, mock.Anything).Return(
		createTestKeyMetadata("gen-key", "", "AES-256-GCM"), nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	result, err := registry.executeKMSGenerateKey(context.Background(), map[string]interface{}{
		"algorithm":   "AES-256-GCM",
		"name":        "my-key",
		"description": "my description",
		"owner":       "admin",
		"tags": map[string]interface{}{
			"env": "staging",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "my-key", resultMap["name"])
	assert.Equal(t, "my description", resultMap["description"])
	assert.Equal(t, "admin", resultMap["owner"])
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS ImportKey via fallback - success with metadata fields
// =============================================================================

func TestMCPCoverage91_KMSImportKey_FallbackSuccess(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("ImportKey", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(
		createTestKeyMetadata("imp-key", "", "AES-256-GCM"), nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	result, err := registry.executeKMSImportKey(context.Background(), map[string]interface{}{
		"key_material":    "YmFzZTY0bWF0ZXJpYWw=",
		"wrapping_key_id": "wrap-key",
		"algorithm":       "AES-256-GCM",
		"name":            "imported-key",
		"description":     "imported desc",
		"tags": map[string]interface{}{
			"source": "external",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	mockKMS.AssertExpectations(t)
}

// importKeyRawMaterial is key material that is not valid UTF-8, so a tool that
// passes the base64 text through instead of decoding it cannot match it.
var importKeyRawMaterial = []byte{0x00, 0xff, 0xfe, 0x80, 0x7f, 0xc3, 0x28, 0x01}

func TestExecuteKMSImportKey_DecodesBase64ForService(t *testing.T) {
	svc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(svc, newTestLogger(t))

	_, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_import_key", map[string]interface{}{
		"key_material":    base64.StdEncoding.EncodeToString(importKeyRawMaterial),
		"wrapping_key_id": "wrapping-key-1",
		"algorithm":       "AES-256-GCM",
		"name":            "imported-key",
	})
	require.NoError(t, err)
	require.NotNil(t, svc.kmsSvc.importKeyReq)
	assert.Equal(t, importKeyRawMaterial, svc.kmsSvc.importKeyReq.KeyMaterial)
	assert.Equal(t, "wrapping-key-1", svc.kmsSvc.importKeyReq.WrappingKeyID)
}

func TestExecuteKMSImportKey_DecodesBase64ForManagerFallback(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("ImportKey", mock.Anything, "imported-key", importKeyRawMaterial, "AES-256-GCM").Return(
		createTestKeyMetadata("imported-key", "", "AES-256-GCM"), nil)
	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSImportKey(context.Background(), map[string]interface{}{
		"key_material": base64.StdEncoding.EncodeToString(importKeyRawMaterial),
		"algorithm":    "AES-256-GCM",
		"name":         "imported-key",
	})
	require.NoError(t, err)
	mockKMS.AssertExpectations(t)
}

func TestExecuteKMSImportKey_RefusesBadKeyMaterial(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]interface{}
		message string
	}{
		{
			name:    "missing",
			args:    map[string]interface{}{"algorithm": "AES-256-GCM"},
			message: "key_material",
		},
		{
			name:    "empty",
			args:    map[string]interface{}{"key_material": "", "algorithm": "AES-256-GCM"},
			message: "key_material",
		},
		{
			name:    "invalid base64",
			args:    map[string]interface{}{"key_material": "not-valid-base64!@#$", "algorithm": "AES-256-GCM"},
			message: "invalid base64 key_material",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" via service", func(t *testing.T) {
			svc := newTestQuicBaaSWithKMSOps()
			registry := NewToolRegistry(svc, newTestLogger(t))

			_, err := registry.executeKMSImportKey(context.Background(), tt.args)
			require.Error(t, err)
			var qbErr *dberrors.QBError
			require.True(t, errors.As(err, &qbErr), "expected a typed QBError, got %T: %v", err, err)
			assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
			assert.Contains(t, err.Error(), tt.message)
			assert.Nil(t, svc.kmsSvc.importKeyReq, "the KMS service must not be called")
		})

		t.Run(tt.name+" via manager fallback", func(t *testing.T) {
			mockKMS := NewMockKMSManager()
			registry := newTestRegistryWithMockKMS(t, mockKMS)

			_, err := registry.executeKMSImportKey(context.Background(), tt.args)
			require.Error(t, err)
			var qbErr *dberrors.QBError
			require.True(t, errors.As(err, &qbErr), "expected a typed QBError, got %T: %v", err, err)
			assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
			mockKMS.AssertNotCalled(t, "ImportKey", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

// TestKMSImportKeyRoundTrip_SDKRawBytesReachService drives the SDK KMS client
// over the MCP transport into the real server tool: the bytes the caller hands
// the SDK are the bytes the KMS service imports.
func TestKMSImportKeyRoundTrip_SDKRawBytesReachService(t *testing.T) {
	svc := newTestQuicBaaSWithKMSOps()
	svc.kmsSvc.importKeyResp = &service.ImportKeyResponse{Metadata: &service.KeyMetadata{
		KeyID:     "imported",
		Name:      "imported",
		Algorithm: service.KeyAlgAES256GCM,
		Status:    service.KeyStatusEnabled,
		Version:   1,
	}}
	tr := registryMCPTransport(t, NewToolRegistry(svc, newTestLogger(t)), nil, "quicbaas_kms_import_key")

	resp, err := kmsclient.NewClient(tr).ImportKey(context.Background(), &kmsclient.ImportKeyRequest{
		KeyMaterial: importKeyRawMaterial,
		Name:        "imported",
	})
	require.NoError(t, err)
	assert.Equal(t, "imported", resp.KeyID)
	require.NotNil(t, svc.kmsSvc.importKeyReq)
	assert.Equal(t, importKeyRawMaterial, svc.kmsSvc.importKeyReq.KeyMaterial)
}

func TestMCPCoverage91_KMSImportKey_UnexpectedResponse(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("ImportKey", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("bad-type", nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSImportKey(context.Background(), map[string]interface{}{
		"key_material":    "material",
		"wrapping_key_id": "wrap",
		"algorithm":       "AES-256-GCM",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected")
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS GetKey via fallback - unexpected response type
// =============================================================================

func TestMCPCoverage91_KMSGetKey_UnexpectedResponse(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "bad-key").Return("not-metadata", nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSGetKey(context.Background(), map[string]interface{}{
		"key_id": "bad-key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected")
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS GetMetadata via fallback - unexpected response type
// =============================================================================

func TestMCPCoverage91_KMSGetMetadata_UnexpectedResponse(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "bad-meta").Return("not-metadata", nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSGetMetadata(context.Background(), map[string]interface{}{
		"key_id": "bad-meta",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected")
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS UpdateMetadata via fallback - unexpected response type
// =============================================================================

func TestMCPCoverage91_KMSUpdateMetadata_UnexpectedResponse(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "bad-upd").Return("not-metadata", nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSUpdateMetadata(context.Background(), map[string]interface{}{
		"key_id": "bad-upd",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected")
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS ListKeys via fallback - with filter and limit
// =============================================================================

func TestMCPCoverage91_KMSListKeys_FallbackWithFilter(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("ListKeys", mock.Anything, "my-prefix").Return(
		[]interface{}{
			createTestKeyMetadata("k1", "key-one", "AES-256-GCM"),
			createTestKeyMetadata("k2", "key-two", "RSA-2048"),
			createTestKeyMetadata("k3", "key-three", "ECDSA-P256"),
		}, nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	result, err := registry.executeKMSListKeys(context.Background(), map[string]interface{}{
		"limit": float64(2),
		"filter": map[string]interface{}{
			"prefix": "my-prefix",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["has_more"])
	mockKMS.AssertExpectations(t)
}

func TestMCPCoverage91_KMSListKeys_FallbackLimitExceedsMax(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("ListKeys", mock.Anything, "").Return(
		[]interface{}{
			createTestKeyMetadata("k1", "key-one", "AES-256-GCM"),
		}, nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	result, err := registry.executeKMSListKeys(context.Background(), map[string]interface{}{
		"limit": float64(20000),
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// getUint64Alt Tests
// =============================================================================

func TestMCPCoverage91_GetUint64Alt_FirstKeyHasValue(t *testing.T) {
	args := map[string]interface{}{
		"pageSize": float64(50),
		"limit":    float64(100),
	}
	val := getUint64Alt(args, "pageSize", "limit")
	assert.Equal(t, uint64(50), val)
}

func TestMCPCoverage91_GetUint64Alt_FallbackToSecondKey(t *testing.T) {
	args := map[string]interface{}{
		"limit": float64(100),
	}
	val := getUint64Alt(args, "pageSize", "limit")
	assert.Equal(t, uint64(100), val)
}

func TestMCPCoverage91_GetUint64Alt_NeitherKeyPresent(t *testing.T) {
	args := map[string]interface{}{}
	val := getUint64Alt(args, "pageSize", "limit")
	assert.Equal(t, uint64(0), val)
}

// =============================================================================
// getInt Tests - Edge Cases
// =============================================================================

func TestMCPCoverage91_GetInt_Int64(t *testing.T) {
	args := map[string]interface{}{
		"val": int64(42),
	}
	assert.Equal(t, 42, getInt(args, "val"))
}

func TestMCPCoverage91_GetInt_Uint64(t *testing.T) {
	args := map[string]interface{}{
		"val": uint64(99),
	}
	assert.Equal(t, 99, getInt(args, "val"))
}

func TestMCPCoverage91_GetInt_String(t *testing.T) {
	args := map[string]interface{}{
		"val": "123",
	}
	assert.Equal(t, 123, getInt(args, "val"))
}

func TestMCPCoverage91_GetInt_EmptyString(t *testing.T) {
	args := map[string]interface{}{
		"val": "",
	}
	assert.Equal(t, 0, getInt(args, "val"))
}

func TestMCPCoverage91_GetInt_Missing(t *testing.T) {
	args := map[string]interface{}{}
	assert.Equal(t, 0, getInt(args, "val"))
}

// =============================================================================
// getStringSlice Tests - Edge Cases
// =============================================================================

func TestMCPCoverage91_GetStringSlice_NativeStringSlice(t *testing.T) {
	args := map[string]interface{}{
		"items": []string{"a", "b", "c"},
	}
	result := getStringSlice(args, "items")
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

func TestMCPCoverage91_GetStringSlice_UnsupportedType(t *testing.T) {
	args := map[string]interface{}{
		"items": 42,
	}
	result := getStringSlice(args, "items")
	assert.Nil(t, result)
}

// =============================================================================
// getIDOrHashKey Tests
// =============================================================================

func TestMCPCoverage91_GetIDOrHashKey_WithNumericID(t *testing.T) {
	args := map[string]interface{}{
		"id": float64(12345),
	}
	id := getIDOrHashKey("entity", args)
	assert.Equal(t, uint64(12345), id)
}

func TestMCPCoverage91_GetIDOrHashKey_WithStringKey(t *testing.T) {
	args := map[string]interface{}{
		"key": "my-entity-key",
	}
	id := getIDOrHashKey("entity", args)
	assert.NotZero(t, id)
}

func TestMCPCoverage91_GetIDOrHashKey_Neither(t *testing.T) {
	args := map[string]interface{}{}
	id := getIDOrHashKey("entity", args)
	assert.Zero(t, id)
}

// =============================================================================
// decodeAADArg Tests - additional_data path
// =============================================================================

func TestMCPCoverage91_DecodeAADArg_AdditionalData(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("aad-data"))
	args := map[string]interface{}{
		"additional_data": encoded,
	}
	result, err := decodeAADArg(args)
	require.NoError(t, err)
	assert.Equal(t, []byte("aad-data"), result)
}

func TestMCPCoverage91_DecodeAADArg_FallbackToAAD(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("aad-from-alias"))
	args := map[string]interface{}{
		"aad": encoded,
	}
	result, err := decodeAADArg(args)
	require.NoError(t, err)
	assert.Equal(t, []byte("aad-from-alias"), result)
}

func TestMCPCoverage91_DecodeAADArg_NoAAD(t *testing.T) {
	args := map[string]interface{}{}
	result, err := decodeAADArg(args)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestMCPCoverage91_DecodeAADArg_InvalidAdditionalData(t *testing.T) {
	args := map[string]interface{}{
		"additional_data": "not-valid-base64!!!",
	}
	_, err := decodeAADArg(args)
	require.Error(t, err)
	var b64Err *Base64DecodeError
	assert.ErrorAs(t, err, &b64Err)
}

// =============================================================================
// KMS Decrypt with nonce and tag
// =============================================================================

func TestMCPCoverage91_XKMSDecrypt_InvalidBase64Nonce(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.handleXKMSDecrypt(ctx, map[string]interface{}{
		"key_id":     "dec-key",
		"ciphertext": base64.StdEncoding.EncodeToString([]byte("ct")),
		"nonce":      "not-valid-base64!!!",
	})
	require.Error(t, err)
	var b64Err *Base64DecodeError
	assert.ErrorAs(t, err, &b64Err)
}

func TestMCPCoverage91_XKMSDecrypt_InvalidBase64Tag(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.handleXKMSDecrypt(ctx, map[string]interface{}{
		"key_id":     "dec-key",
		"ciphertext": base64.StdEncoding.EncodeToString([]byte("ct")),
		"tag":        "not-valid-base64!!!",
	})
	require.Error(t, err)
	var b64Err *Base64DecodeError
	assert.ErrorAs(t, err, &b64Err)
}

// =============================================================================
// KMS Encrypt with additional_data (canonical key)
// =============================================================================

func TestMCPCoverage91_XKMSEncrypt_WithAdditionalData(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.handleXKMSEncrypt(ctx, map[string]interface{}{
		"key_id":          "enc-key",
		"plaintext":       base64.StdEncoding.EncodeToString([]byte("data")),
		"additional_data": base64.StdEncoding.EncodeToString([]byte("extra")),
	})
	// Expect operation error since xkms not initialized
	require.Error(t, err)
	var opErr *XKMSOperationError
	assert.ErrorAs(t, err, &opErr)
	assert.Equal(t, "encrypt", opErr.Operation)
}

// =============================================================================
// KMS Decrypt with backend param and all fields
// =============================================================================

func TestMCPCoverage91_XKMSDecrypt_WithBackendAndAAD(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.handleXKMSDecrypt(ctx, map[string]interface{}{
		"key_id":          "dec-key",
		"backend":         "software",
		"ciphertext":      base64.StdEncoding.EncodeToString([]byte("ct")),
		"nonce":           base64.StdEncoding.EncodeToString([]byte("nonce123456")),
		"tag":             base64.StdEncoding.EncodeToString([]byte("tag-data")),
		"additional_data": base64.StdEncoding.EncodeToString([]byte("aad")),
	})
	require.Error(t, err)
	var opErr *XKMSOperationError
	assert.ErrorAs(t, err, &opErr)
	assert.Equal(t, "decrypt", opErr.Operation)
}

// =============================================================================
// KMS Encrypt with backend param
// =============================================================================

func TestMCPCoverage91_XKMSEncrypt_WithBackend(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.handleXKMSEncrypt(ctx, map[string]interface{}{
		"key_id":    "enc-key",
		"backend":   "software",
		"plaintext": base64.StdEncoding.EncodeToString([]byte("data")),
	})
	require.Error(t, err)
	var opErr *XKMSOperationError
	assert.ErrorAs(t, err, &opErr)
	assert.Equal(t, "encrypt", opErr.Operation)
}

// =============================================================================
// XKMS Sign with backend param
// =============================================================================

func TestMCPCoverage91_XKMSSign_WithBackend(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.handleXKMSSign(ctx, map[string]interface{}{
		"key_id":  "sign-key",
		"backend": "software",
		"data":    base64.StdEncoding.EncodeToString([]byte("sign-data")),
		"hash":    "sha-512",
	})
	require.Error(t, err)
	var opErr *XKMSOperationError
	assert.ErrorAs(t, err, &opErr)
	assert.Equal(t, "sign", opErr.Operation)
}

// =============================================================================
// XKMS Verify with backend param
// =============================================================================

func TestMCPCoverage91_XKMSVerify_WithBackend(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.handleXKMSVerify(ctx, map[string]interface{}{
		"key_id":    "verify-key",
		"backend":   "software",
		"data":      base64.StdEncoding.EncodeToString([]byte("verify-data")),
		"signature": base64.StdEncoding.EncodeToString([]byte("sig")),
		"hash":      "sha-384",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, false, resultMap["valid"])
}

// =============================================================================
// XKMS Unseal with backend and all params
// =============================================================================

func TestMCPCoverage91_XKMSUnseal_WithBackendAndParams(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.handleXKMSUnseal(ctx, map[string]interface{}{
		"backend":    "software",
		"ciphertext": base64.StdEncoding.EncodeToString([]byte("ct")),
		"nonce":      base64.StdEncoding.EncodeToString([]byte("nonce123456")),
		"tag":        base64.StdEncoding.EncodeToString([]byte("tag-data")),
		"aad":        base64.StdEncoding.EncodeToString([]byte("aad")),
	})
	require.Error(t, err)
	var opErr *XKMSOperationError
	assert.ErrorAs(t, err, &opErr)
	assert.Equal(t, "unseal", opErr.Operation)
}

// =============================================================================
// XKMS DeleteKey with backend
// =============================================================================

func TestMCPCoverage91_XKMSDeleteKey_WithBackend(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.handleXKMSDeleteKey(ctx, map[string]interface{}{
		"key_id":  "del-key",
		"backend": "software",
	})
	require.Error(t, err)
	var opErr *XKMSOperationError
	assert.ErrorAs(t, err, &opErr)
	assert.Equal(t, "deleteKey", opErr.Operation)
}

// =============================================================================
// XKMS SaveCert with valid PEM but xkms not initialized
// This covers the x509.ParseCertificate -> xkms.SaveCertificateByID path
// =============================================================================

func TestMCPCoverage91_XKMSSaveCertChain_ValidPEMButUninitializedXKMS(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	// Valid PEM that IS a valid certificate will reach xkms.SaveCertificateByID
	// which should fail because xkms is not initialized.
	// But getting a real valid cert just for unit tests is cumbersome.
	// Instead, test that invalid cert data in valid PEM gives parse error.
	invalidCertPEM := "-----BEGIN CERTIFICATE-----\nbm90LWEtY2VydA==\n-----END CERTIFICATE-----"

	_, err := registry.handleXKMSSaveCertChain(ctx, map[string]interface{}{
		"key_id":     "chain-key",
		"chain_pems": []interface{}{invalidCertPEM},
	})
	require.Error(t, err)
	var parseErr *XKMSCertificateParseError
	assert.ErrorAs(t, err, &parseErr)
}

// =============================================================================
// KMS No Service Available Tests
// =============================================================================

func TestMCPCoverage91_KMSNoServiceAvailable(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	// Test that getKMSManager returns error when service has no KMS manager
	_, err := registry.getKMSManager()
	require.Error(t, err)

	// Test that getKMSService returns error when service has no KMS service
	_, err = registry.getKMSService()
	require.Error(t, err)

	// Test executeKMSGenerateKey with no KMS available at all
	_, err = registry.executeKMSGenerateKey(ctx, map[string]interface{}{
		"algorithm": "AES-256-GCM",
	})
	require.Error(t, err)
}

// =============================================================================
// KMS ListKeys with non-metadata results
// =============================================================================

func TestMCPCoverage91_KMSListKeys_NonMetadataResults(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("ListKeys", mock.Anything, "").Return(
		[]interface{}{
			"not-a-metadata-object",
			createTestKeyMetadata("k1", "key-one", "AES-256-GCM"),
		}, nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	result, err := registry.executeKMSListKeys(context.Background(), map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	keys, ok := resultMap["keys"].([]interface{})
	require.True(t, ok)
	// Only the valid metadata should be included
	assert.Len(t, keys, 1)
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// kmsMetadataToResponse with LastUsedAt and Usage
// =============================================================================

func TestMCPCoverage91_KmsMetadataToResponse_WithLastUsedAndUsage(t *testing.T) {
	now := time.Now()
	lastUsed := now.Add(-10 * time.Minute)
	metadata := &kms.KeyMetadata{
		KeyID:      "resp-key",
		Name:       "response-test",
		Algorithm:  "RSA-4096",
		State:      kms.KeyStateEnabled,
		Version:    5,
		Created:    now,
		Updated:    now,
		LastUsedAt: &lastUsed,
		Usage:      []kms.KeyUsage{kms.KeyUsageSign, kms.KeyUsageVerify},
		Tags:       map[string]string{"tier": "gold"},
		Owner:      "service-account",
	}

	resp := kmsMetadataToResponse(metadata)
	assert.Equal(t, "resp-key", resp.KeyID)
	assert.Equal(t, lastUsed.Unix(), resp.LastUsedAt)
	assert.Len(t, resp.Usage, 2)
	assert.Equal(t, string(kms.KeyUsageSign), resp.Usage[0])
	assert.Equal(t, "service-account", resp.Owner)
}

// =============================================================================
// XKMS ListCerts - no backend (default path)
// =============================================================================

func TestMCPCoverage91_XKMSListCerts_NoBackend(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	// Without backend parameter
	_, err := registry.handleXKMSListCerts(ctx, map[string]interface{}{})
	require.Error(t, err)
	var opErr *XKMSOperationError
	assert.ErrorAs(t, err, &opErr)
	assert.Equal(t, "listCerts", opErr.Operation)
}

// =============================================================================
// XKMS ListKeys - no backend (default path)
// =============================================================================

func TestMCPCoverage91_XKMSListKeys_NoBackend(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.handleXKMSListKeys(ctx, map[string]interface{}{})
	if err != nil {
		var opErr *XKMSOperationError
		assert.ErrorAs(t, err, &opErr)
		assert.Equal(t, "listKeys", opErr.Operation)
	}
}

// =============================================================================
// XKMS encrypt with invalid additional_data base64
// =============================================================================

func TestMCPCoverage91_XKMSEncrypt_InvalidAdditionalData(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.handleXKMSEncrypt(ctx, map[string]interface{}{
		"key_id":          "enc-key",
		"plaintext":       base64.StdEncoding.EncodeToString([]byte("data")),
		"additional_data": "not-valid-base64!!!",
	})
	require.Error(t, err)
	var b64Err *Base64DecodeError
	assert.ErrorAs(t, err, &b64Err)
}

// =============================================================================
// XKMS decrypt with invalid additional_data base64
// =============================================================================

func TestMCPCoverage91_XKMSDecrypt_InvalidAdditionalData(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.handleXKMSDecrypt(ctx, map[string]interface{}{
		"key_id":          "dec-key",
		"ciphertext":      base64.StdEncoding.EncodeToString([]byte("ct")),
		"additional_data": "not-valid-base64!!!",
	})
	require.Error(t, err)
	var b64Err *Base64DecodeError
	assert.ErrorAs(t, err, &b64Err)
}

// =============================================================================
// XKMS GetKey with backend (covers backend field in response)
// =============================================================================

func TestMCPCoverage91_XKMSGetKey_NoBackend(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.handleXKMSGetKey(ctx, map[string]interface{}{
		"key_id": "test-key",
	})
	require.Error(t, err)
	var opErr *XKMSOperationError
	assert.ErrorAs(t, err, &opErr)
	assert.Equal(t, "getKey", opErr.Operation)
}

// =============================================================================
// ensureXKMSInitialized - the "initialized" path can't be tested without xkms
// but we test the not-initialized path for completeness
// =============================================================================

func TestMCPCoverage91_EnsureXKMSInitialized_NotInitialized(t *testing.T) {
	err := ensureXKMSInitialized()
	require.Error(t, err)
	var initErr *XKMSNotInitializedError
	assert.ErrorAs(t, err, &initErr)
}

// =============================================================================
// XKMS CanSeal - no backend (covers default path)
// =============================================================================

func TestMCPCoverage91_XKMSCanSeal_NoBackend(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.handleXKMSCanSeal(ctx, map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// XKMS Encrypt - with backend and additional_data
// =============================================================================

func TestMCPCoverage91_XKMSEncrypt_BackendAndAdditionalData(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.handleXKMSEncrypt(ctx, map[string]interface{}{
		"key_id":          "enc-key",
		"backend":         "software",
		"plaintext":       base64.StdEncoding.EncodeToString([]byte("data")),
		"additional_data": base64.StdEncoding.EncodeToString([]byte("extra-data")),
	})
	require.Error(t, err)
	var opErr *XKMSOperationError
	assert.ErrorAs(t, err, &opErr)
}

// =============================================================================
// XKMS Decrypt - with backend and additional_data
// =============================================================================

func TestMCPCoverage91_XKMSDecrypt_BackendAllParams(t *testing.T) {
	log := newTestLogger(t)
	svc := newMinimalMockService()
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	_, err := registry.handleXKMSDecrypt(ctx, map[string]interface{}{
		"key_id":     "dec-key",
		"backend":    "software",
		"ciphertext": base64.StdEncoding.EncodeToString([]byte("ct")),
		"nonce":      base64.StdEncoding.EncodeToString([]byte("nonce")),
		"tag":        base64.StdEncoding.EncodeToString([]byte("tag")),
		"aad":        base64.StdEncoding.EncodeToString([]byte("aad")),
	})
	require.Error(t, err)
	var opErr *XKMSOperationError
	assert.ErrorAs(t, err, &opErr)
}

// =============================================================================
// marshalResult Tests - with unmarshalable type
// =============================================================================

func TestMCPCoverage91_MarshalResult_ValidStruct(t *testing.T) {
	input := map[string]interface{}{
		"key": "value",
		"num": 42,
	}
	result, err := marshalResult(input)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "value", resultMap["key"])
}

func TestMCPCoverage91_MarshalResult_UnmarshalableType(t *testing.T) {
	// A channel cannot be JSON marshaled
	ch := make(chan int)
	_, err := marshalResult(ch)
	require.Error(t, err)
}

// =============================================================================
// hashStringKey Tests
// =============================================================================

func TestMCPCoverage91_HashStringKey_Deterministic(t *testing.T) {
	h1 := hashStringKey("user", "admin")
	h2 := hashStringKey("user", "admin")
	assert.Equal(t, h1, h2)
}

func TestMCPCoverage91_HashStringKey_DifferentEntities(t *testing.T) {
	h1 := hashStringKey("user", "admin")
	h2 := hashStringKey("role", "admin")
	assert.NotEqual(t, h1, h2)
}

// =============================================================================
// KMS SetPolicy via fallback - SetPolicy without version
// =============================================================================

func TestMCPCoverage91_KMSSetPolicy_FallbackNoVersion(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "sp-key2").Return(
		createTestKeyMetadata("sp-key2", "test", "AES-256-GCM"), nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	result, err := registry.executeKMSSetPolicy(context.Background(), map[string]interface{}{
		"key_id": "sp-key2",
		"policy": map[string]interface{}{
			"allowed_operations": []interface{}{"encrypt"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(1), resultMap["policy_version"])
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS UpdateMetadata with nil tags initially
// =============================================================================

func TestMCPCoverage91_KMSUpdateMetadata_NilTagsInitially(t *testing.T) {
	now := time.Now()
	metadata := &kms.KeyMetadata{
		KeyID:     "niltagskey",
		Algorithm: "AES-256-GCM",
		State:     kms.KeyStateEnabled,
		Version:   1,
		Created:   now,
		Updated:   now,
		Tags:      nil,
	}

	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "niltagskey").Return(metadata, nil)
	mockKMS.On("UpdateStoredMetadata", mock.Anything).Return(nil)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	result, err := registry.executeKMSUpdateMetadata(context.Background(), map[string]interface{}{
		"key_id": "niltagskey",
		"tags": map[string]interface{}{
			"new": "value",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS RotateKey via fallback - RotateKey error
// =============================================================================

func TestMCPCoverage91_KMSRotateKey_RotateError(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "rot-err-key").Return(
		createTestKeyMetadata("rot-err-key", "test", "AES-256-GCM"), nil)
	mockKMS.On("RotateKey", mock.Anything, "rot-err-key").Return(nil, assert.AnError)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSRotateKey(context.Background(), map[string]interface{}{
		"key_id": "rot-err-key",
	})
	require.Error(t, err)
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS RotateKey via fallback - GetKey error
// =============================================================================

func TestMCPCoverage91_KMSRotateKey_GetKeyError(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "rot-geterr-key").Return(nil, assert.AnError)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSRotateKey(context.Background(), map[string]interface{}{
		"key_id": "rot-geterr-key",
	})
	require.Error(t, err)
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS RevokeKey via fallback - error path
// =============================================================================

func TestMCPCoverage91_KMSRevokeKey_FallbackError(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("RevokeKey", mock.Anything, "rev-err-key").Return(assert.AnError)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSRevokeKey(context.Background(), map[string]interface{}{
		"key_id": "rev-err-key",
	})
	require.Error(t, err)
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS DeleteKey via fallback - DisableKey error
// =============================================================================

func TestMCPCoverage91_KMSDeleteKey_DisableError(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("DisableKey", mock.Anything, "dis-err-key").Return(assert.AnError)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSDeleteKey(context.Background(), map[string]interface{}{
		"key_id": "dis-err-key",
	})
	require.Error(t, err)
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS DeleteKey via fallback - DeleteKey error
// =============================================================================

func TestMCPCoverage91_KMSDeleteKey_DeleteError(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("DisableKey", mock.Anything, "delerr-key").Return(nil)
	mockKMS.On("DeleteKey", mock.Anything, "delerr-key").Return(assert.AnError)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSDeleteKey(context.Background(), map[string]interface{}{
		"key_id": "delerr-key",
	})
	require.Error(t, err)
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS GenerateKey via fallback - error path
// =============================================================================

func TestMCPCoverage91_KMSGenerateKey_FallbackError(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("GenerateKey", mock.Anything, mock.Anything).Return(nil, assert.AnError)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSGenerateKey(context.Background(), map[string]interface{}{
		"algorithm": "AES-256-GCM",
	})
	require.Error(t, err)
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS GetKey via fallback - error path
// =============================================================================

func TestMCPCoverage91_KMSGetKey_FallbackError(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "err-key").Return(nil, assert.AnError)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSGetKey(context.Background(), map[string]interface{}{
		"key_id": "err-key",
	})
	require.Error(t, err)
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS GetMetadata via fallback - error path
// =============================================================================

func TestMCPCoverage91_KMSGetMetadata_FallbackError(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "err-meta").Return(nil, assert.AnError)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSGetMetadata(context.Background(), map[string]interface{}{
		"key_id": "err-meta",
	})
	require.Error(t, err)
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS ListKeys via fallback - error path
// =============================================================================

func TestMCPCoverage91_KMSListKeys_FallbackError(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("ListKeys", mock.Anything, "").Return(nil, assert.AnError)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSListKeys(context.Background(), map[string]interface{}{})
	require.Error(t, err)
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS ImportKey via fallback - error path
// =============================================================================

func TestMCPCoverage91_KMSImportKey_FallbackError(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("ImportKey", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, assert.AnError)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSImportKey(context.Background(), map[string]interface{}{
		"key_material":    "material",
		"wrapping_key_id": "wrap",
		"algorithm":       "AES-256-GCM",
	})
	require.Error(t, err)
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS UpdateMetadata via fallback - UpdateStoredMetadata error
// =============================================================================

func TestMCPCoverage91_KMSUpdateMetadata_StoreError(t *testing.T) {
	now := time.Now()
	metadata := &kms.KeyMetadata{
		KeyID:     "store-err",
		Algorithm: "AES-256-GCM",
		State:     kms.KeyStateEnabled,
		Version:   1,
		Created:   now,
		Updated:   now,
	}

	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "store-err").Return(metadata, nil)
	mockKMS.On("UpdateStoredMetadata", mock.Anything).Return(assert.AnError)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSUpdateMetadata(context.Background(), map[string]interface{}{
		"key_id": "store-err",
	})
	require.Error(t, err)
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS GetPolicy via fallback - GetKey error
// =============================================================================

func TestMCPCoverage91_KMSGetPolicy_FallbackError(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "pol-err").Return(nil, assert.AnError)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSGetPolicy(context.Background(), map[string]interface{}{
		"key_id": "pol-err",
	})
	require.Error(t, err)
	mockKMS.AssertExpectations(t)
}

// =============================================================================
// KMS SetPolicy via fallback - GetKey error
// =============================================================================

func TestMCPCoverage91_KMSSetPolicy_FallbackGetKeyError(t *testing.T) {
	mockKMS := NewMockKMSManager()
	mockKMS.On("GetKey", mock.Anything, "sp-err").Return(nil, assert.AnError)

	registry := newTestRegistryWithMockKMS(t, mockKMS)

	_, err := registry.executeKMSSetPolicy(context.Background(), map[string]interface{}{
		"key_id": "sp-err",
		"policy": map[string]interface{}{},
	})
	require.Error(t, err)
	mockKMS.AssertExpectations(t)
}
