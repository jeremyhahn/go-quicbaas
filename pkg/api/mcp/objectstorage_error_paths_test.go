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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Object Storage Service Error Path Tests
//
// These tests cover the service-level error return paths in object storage
// handlers, ensuring proper error propagation when the underlying service fails.
// =============================================================================

func TestExecuteObjectDelete_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("DeleteObject", mock.Anything, mock.AnythingOfType("*service.DeleteObjectRequest")).
		Return(nil, &errors.MessageError{Message: "object not found"})

	_, err := registry.executeObjectDelete(context.Background(), map[string]interface{}{
		"key": "test:missing",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "object not found")
}

func TestExecuteObjectHead_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("HeadObject", mock.Anything, mock.AnythingOfType("*service.HeadObjectRequest")).
		Return(nil, &errors.MessageError{Message: "backend unavailable"})

	_, err := registry.executeObjectHead(context.Background(), map[string]interface{}{
		"key": "test:object",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backend unavailable")
}

func TestExecuteObjectHead_ExistsWithTags(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("HeadObject", mock.Anything, mock.AnythingOfType("*service.HeadObjectRequest")).
		Return(&service.HeadObjectResponse{
			Key:          "test:tagged",
			Exists:       true,
			ContentType:  "text/plain",
			Size:         1024,
			Checksum:     []byte{0xab, 0xcd},
			Version:      2,
			Status:       "active",
			Backend:      "local",
			ReplicaCount: 3,
			CreatedAt:    time.Now().UnixNano(),
			ModifiedAt:   time.Now().UnixNano(),
			LastAccessAt: time.Now().UnixNano(),
			Tags:         map[string]string{"env": "prod"},
			TenantID:     "tenant-1",
		}, nil)

	result, err := registry.executeObjectHead(context.Background(), map[string]interface{}{
		"key": "test:tagged",
	})
	require.NoError(t, err)
	resultMap := result.(map[string]interface{})
	assert.Equal(t, true, resultMap["exists"])
	assert.Equal(t, "tenant-1", resultMap["tenant_id"])
	assert.NotNil(t, resultMap["tags"])
}

func TestExecuteObjectList_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("ListObjects", mock.Anything, mock.AnythingOfType("*service.ListObjectsRequest")).
		Return(nil, &errors.MessageError{Message: "storage backend offline"})

	_, err := registry.executeObjectList(context.Background(), map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage backend offline")
}

func TestExecuteObjectList_WithTags(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("ListObjects", mock.Anything, mock.AnythingOfType("*service.ListObjectsRequest")).
		Return(&service.ListObjectsResponse{
			Objects: []*service.ObjectSummary{
				{
					Key:         "test:obj1",
					Size:        512,
					ContentType: "text/plain",
					Tags:        map[string]string{"env": "test"},
				},
			},
			HasMore:    true,
			NextCursor: "cursor-abc",
			TotalCount: 100,
			Timestamp:  time.Now().Unix(),
		}, nil)

	result, err := registry.executeObjectList(context.Background(), map[string]interface{}{
		"include_tags": true,
	})
	require.NoError(t, err)
	resultMap := result.(map[string]interface{})
	assert.Equal(t, true, resultMap["has_more"])
}

func TestExecuteObjectListByTag_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("ListByTag", mock.Anything, mock.AnythingOfType("*service.ListByTagRequest")).
		Return(nil, &errors.MessageError{Message: "tag index unavailable"})

	_, err := registry.executeObjectListByTag(context.Background(), map[string]interface{}{
		"tag_name": "env",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tag index unavailable")
}

func TestExecuteObjectListByTenant_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("ListByTenant", mock.Anything, mock.AnythingOfType("*service.ListByTenantRequest")).
		Return(nil, &errors.MessageError{Message: "tenant not found"})

	_, err := registry.executeObjectListByTenant(context.Background(), map[string]interface{}{
		"tenant_id": "missing-tenant",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant not found")
}

func TestExecuteObjectCopy_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("CopyObject", mock.Anything, mock.AnythingOfType("*service.CopyObjectRequest")).
		Return(nil, &errors.MessageError{Message: "source not found"})

	_, err := registry.executeObjectCopy(context.Background(), map[string]interface{}{
		"source_key":      "src:key",
		"destination_key": "dst:key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source not found")
}

func TestExecuteObjectReplicate_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("ReplicateObject", mock.Anything, mock.AnythingOfType("*service.ReplicateObjectRequest")).
		Return(nil, &errors.MessageError{Message: "replication failed"})

	_, err := registry.executeObjectReplicate(context.Background(), map[string]interface{}{
		"key":     "test:key",
		"targets": []interface{}{"store-a"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "replication failed")
}

func TestExecuteObjectMigrate_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("MigrateObject", mock.Anything, mock.AnythingOfType("*service.MigrateObjectRequest")).
		Return(nil, &errors.MessageError{Message: "migration failed"})

	_, err := registry.executeObjectMigrate(context.Background(), map[string]interface{}{
		"key":            "test:key",
		"target_backend": "archive",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migration failed")
}

func TestExecuteObjectArchive_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("ArchiveObject", mock.Anything, mock.AnythingOfType("*service.ArchiveObjectRequest")).
		Return(nil, &errors.MessageError{Message: "archive store full"})

	_, err := registry.executeObjectArchive(context.Background(), map[string]interface{}{
		"key": "test:key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "archive store full")
}

func TestExecuteObjectArchive_WithDeleteFromPrimary(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("ArchiveObject", mock.Anything, mock.MatchedBy(func(req *service.ArchiveObjectRequest) bool {
		return !req.DeleteFromPrimary
	})).Return(&service.ArchiveObjectResponse{
		Key:                "test:key",
		ArchiveStore:       "cold-store",
		Success:            true,
		DeletedFromPrimary: false,
		ArchivedAt:         time.Now().Unix(),
	}, nil)

	result, err := registry.executeObjectArchive(context.Background(), map[string]interface{}{
		"key":                 "test:key",
		"delete_from_primary": false,
	})
	require.NoError(t, err)
	resultMap := result.(map[string]interface{})
	assert.Equal(t, false, resultMap["deleted_from_primary"])
}

func TestExecuteObjectRestore_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("RestoreObject", mock.Anything, mock.AnythingOfType("*service.RestoreObjectRequest")).
		Return(nil, &errors.MessageError{Message: "restore failed"})

	_, err := registry.executeObjectRestore(context.Background(), map[string]interface{}{
		"key": "test:key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "restore failed")
}

func TestExecuteObjectVerify_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("VerifyObject", mock.Anything, mock.AnythingOfType("*service.VerifyObjectRequest")).
		Return(nil, &errors.MessageError{Message: "verify failed"})

	_, err := registry.executeObjectVerify(context.Background(), map[string]interface{}{
		"key": "test:key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verify failed")
}

func TestExecuteObjectSetPolicy_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("SetObjectPolicy", mock.Anything, mock.AnythingOfType("*service.SetObjectPolicyRequest")).
		Return(nil, &errors.MessageError{Message: "invalid policy"})

	_, err := registry.executeObjectSetPolicy(context.Background(), map[string]interface{}{
		"key": "test:key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid policy")
}

func TestExecuteObjectGetPolicy_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("GetObjectPolicy", mock.Anything, mock.AnythingOfType("*service.GetObjectPolicyRequest")).
		Return(nil, &errors.MessageError{Message: "no policy found"})

	_, err := registry.executeObjectGetPolicy(context.Background(), map[string]interface{}{
		"key": "test:key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no policy found")
}

func TestExecuteObjectListBackends_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("ListBackends", mock.Anything, mock.AnythingOfType("*service.ListBackendsRequest")).
		Return(nil, &errors.MessageError{Message: "backend list error"})

	_, err := registry.executeObjectListBackends(context.Background(), map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backend list error")
}

func TestExecuteObjectListBackends_WithHealth(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("ListBackends", mock.Anything, mock.AnythingOfType("*service.ListBackendsRequest")).
		Return(&service.ListBackendsResponse{
			Backends: []*service.BackendInfo{
				{
					Name:        "local-store",
					Type:        "local",
					Enabled:     true,
					Default:     true,
					Tier:        "hot",
					Region:      "us-east-1",
					ObjectCount: 500,
					TotalSize:   1024 * 1024,
					Health: &service.BackendHealthInfo{
						Healthy:   true,
						Latency:   5,
						LastCheck: time.Now().Unix(),
						Error:     "",
					},
				},
			},
			Timestamp: time.Now().Unix(),
		}, nil)

	result, err := registry.executeObjectListBackends(context.Background(), map[string]interface{}{
		"include_health": true,
	})
	require.NoError(t, err)
	resultMap := result.(map[string]interface{})
	assert.Contains(t, resultMap, "backends")
}

func TestExecuteObjectBackendHealth_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("GetBackendHealth", mock.Anything, mock.AnythingOfType("*service.GetBackendHealthRequest")).
		Return(nil, &errors.MessageError{Message: "health check failed"})

	_, err := registry.executeObjectBackendHealth(context.Background(), map[string]interface{}{
		"name": "local-store",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "health check failed")
}

func TestExecuteObjectTenantUsage_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("GetTenantUsage", mock.Anything, mock.AnythingOfType("*service.GetTenantUsageRequest")).
		Return(nil, &errors.MessageError{Message: "tenant not found"})

	_, err := registry.executeObjectTenantUsage(context.Background(), map[string]interface{}{
		"tenant_id": "missing-tenant",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant not found")
}

func TestExecuteObjectTenantQuotas_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("GetTenantQuotas", mock.Anything, mock.AnythingOfType("*service.GetTenantQuotasRequest")).
		Return(nil, &errors.MessageError{Message: "quota lookup failed"})

	_, err := registry.executeObjectTenantQuotas(context.Background(), map[string]interface{}{
		"tenant_id": "test-tenant",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quota lookup failed")
}

func TestExecuteObjectSetTenantQuotas_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	registry := newTestObjectStorageRegistry(mockSvc)

	mockSvc.On("SetTenantQuotas", mock.Anything, mock.AnythingOfType("*service.SetTenantQuotasRequest")).
		Return(nil, &errors.MessageError{Message: "quota update failed"})

	_, err := registry.executeObjectSetTenantQuotas(context.Background(), map[string]interface{}{
		"tenant_id":         "test-tenant",
		"max_storage_bytes": float64(1024 * 1024 * 1024),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quota update failed")
}
