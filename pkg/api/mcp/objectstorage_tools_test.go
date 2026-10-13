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
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock Object Storage Service
// =============================================================================

type mockObjectStorageService struct {
	mock.Mock
}

func (m *mockObjectStorageService) PutObject(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.PutObjectResponse), args.Error(1)
}

func (m *mockObjectStorageService) GetObject(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetObjectResponse), args.Error(1)
}

func (m *mockObjectStorageService) DeleteObject(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.DeleteObjectResponse), args.Error(1)
}

func (m *mockObjectStorageService) HeadObject(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.HeadObjectResponse), args.Error(1)
}

func (m *mockObjectStorageService) ListObjects(ctx context.Context, req *service.ListObjectsRequest) (*service.ListObjectsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ListObjectsResponse), args.Error(1)
}

func (m *mockObjectStorageService) ListByTag(ctx context.Context, req *service.ListByTagRequest) (*service.ListByTagResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ListByTagResponse), args.Error(1)
}

func (m *mockObjectStorageService) ListByTenant(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ListByTenantResponse), args.Error(1)
}

func (m *mockObjectStorageService) CopyObject(ctx context.Context, req *service.CopyObjectRequest) (*service.CopyObjectResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.CopyObjectResponse), args.Error(1)
}

func (m *mockObjectStorageService) ReplicateObject(ctx context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ReplicateObjectResponse), args.Error(1)
}

func (m *mockObjectStorageService) MigrateObject(ctx context.Context, req *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.MigrateObjectResponse), args.Error(1)
}

func (m *mockObjectStorageService) ArchiveObject(ctx context.Context, req *service.ArchiveObjectRequest) (*service.ArchiveObjectResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ArchiveObjectResponse), args.Error(1)
}

func (m *mockObjectStorageService) RestoreObject(ctx context.Context, req *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.RestoreObjectResponse), args.Error(1)
}

func (m *mockObjectStorageService) VerifyObject(ctx context.Context, req *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.VerifyObjectResponse), args.Error(1)
}

func (m *mockObjectStorageService) SetObjectPolicy(ctx context.Context, req *service.SetObjectPolicyRequest) (*service.SetObjectPolicyResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.SetObjectPolicyResponse), args.Error(1)
}

func (m *mockObjectStorageService) GetObjectPolicy(ctx context.Context, req *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetObjectPolicyResponse), args.Error(1)
}

func (m *mockObjectStorageService) ListBackends(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ListBackendsResponse), args.Error(1)
}

func (m *mockObjectStorageService) GetBackendHealth(ctx context.Context, req *service.GetBackendHealthRequest) (*service.GetBackendHealthResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetBackendHealthResponse), args.Error(1)
}

func (m *mockObjectStorageService) GetTenantUsage(ctx context.Context, req *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetTenantUsageResponse), args.Error(1)
}

func (m *mockObjectStorageService) GetTenantQuotas(ctx context.Context, req *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetTenantQuotasResponse), args.Error(1)
}

func (m *mockObjectStorageService) SetTenantQuotas(ctx context.Context, req *service.SetTenantQuotasRequest) (*service.SetTenantQuotasResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.SetTenantQuotasResponse), args.Error(1)
}

func (m *mockObjectStorageService) BatchPutObjects(ctx context.Context, req *service.BatchPutObjectsRequest) (*service.BatchPutObjectsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.BatchPutObjectsResponse), args.Error(1)
}

func (m *mockObjectStorageService) BatchGetObjects(ctx context.Context, req *service.BatchGetObjectsRequest) (*service.BatchGetObjectsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.BatchGetObjectsResponse), args.Error(1)
}

func (m *mockObjectStorageService) BatchDeleteObjects(ctx context.Context, req *service.BatchDeleteObjectsRequest) (*service.BatchDeleteObjectsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.BatchDeleteObjectsResponse), args.Error(1)
}

// =============================================================================
// Test Helpers
// =============================================================================

func newTestObjectStorageRegistry(mockSvc *mockObjectStorageService) *ToolRegistry {
	mockQuicBaaS := &MockQuicBaaS{}
	mockQuicBaaS.objectStorageSvc = mockSvc

	return &ToolRegistry{
		service: mockQuicBaaS,
	}
}

// =============================================================================
// Object CRUD Tests
// =============================================================================

func TestExecuteObjectPut(t *testing.T) {
	t.Run("success with text data", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("PutObject", mock.Anything, mock.AnythingOfType("*service.PutObjectRequest")).
			Return(&service.PutObjectResponse{
				Key:       "test:object.txt",
				ObjectID:  12345,
				Checksum:  []byte{0xab, 0xcd, 0xef},
				Size:      1024,
				Backend:   "local",
				Version:   1,
				CreatedAt: time.Now().UnixNano(),
			}, nil)

		args := map[string]interface{}{
			"key":          "test:object.txt",
			"value":        "hello world",
			"content_type": "text/plain",
		}

		result, err := registry.executeObjectPut(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap := result.(map[string]interface{})
		assert.Equal(t, "test:object.txt", resultMap["key"])
		assert.NotNil(t, resultMap["checksum"])
		mockSvc.AssertExpectations(t)
	})

	t.Run("success with base64 data", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("PutObject", mock.Anything, mock.AnythingOfType("*service.PutObjectRequest")).
			Return(&service.PutObjectResponse{
				Key:       "test:binary.bin",
				ObjectID:  12346,
				Checksum:  []byte{0x12, 0x34},
				Size:      64,
				Backend:   "s3",
				Version:   1,
				CreatedAt: time.Now().UnixNano(),
			}, nil)

		binaryData := []byte{0x00, 0x01, 0x02, 0x03, 0x04}
		args := map[string]interface{}{
			"key":            "test:binary.bin",
			"value":          base64.StdEncoding.EncodeToString(binaryData),
			"content_type":   "application/octet-stream",
			"target_backend": "s3",
		}

		result, err := registry.executeObjectPut(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)
		mockSvc.AssertExpectations(t)
	})

	t.Run("success with tags", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("PutObject", mock.Anything, mock.MatchedBy(func(req *service.PutObjectRequest) bool {
			return req.Tags != nil && req.Tags["env"] == "prod"
		})).Return(&service.PutObjectResponse{
			Key:      "test:tagged.txt",
			ObjectID: 12347,
			Checksum: []byte{0xde, 0xad},
			Size:     256,
			Backend:  "local",
			Version:  1,
		}, nil)

		args := map[string]interface{}{
			"key":   "test:tagged.txt",
			"value": "tagged data",
			"tags": map[string]interface{}{
				"env":    "prod",
				"owner":  "team-a",
				"status": "active",
			},
		}

		result, err := registry.executeObjectPut(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)
		mockSvc.AssertExpectations(t)
	})

	t.Run("missing key", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		args := map[string]interface{}{
			"value": "test",
		}

		_, err := registry.executeObjectPut(context.Background(), args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key is required")
	})

	t.Run("missing value", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		args := map[string]interface{}{
			"key": "test:object.txt",
		}

		_, err := registry.executeObjectPut(context.Background(), args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "value is required")
	})

	t.Run("service error", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("PutObject", mock.Anything, mock.AnythingOfType("*service.PutObjectRequest")).
			Return(nil, errors.New("storage backend unavailable"))

		args := map[string]interface{}{
			"key":   "test:object.txt",
			"value": "test data",
		}

		_, err := registry.executeObjectPut(context.Background(), args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "storage backend unavailable")
	})
}

func TestExecuteObjectGet(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("GetObject", mock.Anything, mock.AnythingOfType("*service.GetObjectRequest")).
			Return(&service.GetObjectResponse{
				Key:         "test:object.txt",
				Value:       []byte("hello world"),
				ContentType: "text/plain",
				Size:        11,
				Checksum:    []byte{0xab, 0xcd},
				Version:     1,
				Tags:        map[string]string{"env": "test"},
				TenantID:    "tenant-1",
				CreatedAt:   time.Now().UnixNano(),
				ModifiedAt:  time.Now().UnixNano(),
				Backend:     "local",
			}, nil)

		args := map[string]interface{}{
			"key":              "test:object.txt",
			"include_metadata": true,
		}

		result, err := registry.executeObjectGet(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap := result.(map[string]interface{})
		assert.Equal(t, "test:object.txt", resultMap["key"])
		assert.NotEmpty(t, resultMap["value"]) // base64 encoded

		// Decode the value
		decodedValue, err := base64.StdEncoding.DecodeString(resultMap["value"].(string))
		require.NoError(t, err)
		assert.Equal(t, "hello world", string(decodedValue))
		mockSvc.AssertExpectations(t)
	})

	t.Run("missing key", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		args := map[string]interface{}{}

		_, err := registry.executeObjectGet(context.Background(), args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key is required")
	})

	t.Run("not found", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("GetObject", mock.Anything, mock.AnythingOfType("*service.GetObjectRequest")).
			Return(nil, errors.New("object not found"))

		args := map[string]interface{}{
			"key": "test:nonexistent.txt",
		}

		_, err := registry.executeObjectGet(context.Background(), args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestExecuteObjectDelete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("DeleteObject", mock.Anything, mock.AnythingOfType("*service.DeleteObjectRequest")).
			Return(&service.DeleteObjectResponse{
				Key:       "test:object.txt",
				Success:   true,
				Size:      1024,
				Timestamp: time.Now().UnixNano(),
			}, nil)

		args := map[string]interface{}{
			"key": "test:object.txt",
		}

		result, err := registry.executeObjectDelete(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap := result.(map[string]interface{})
		assert.Equal(t, true, resultMap["success"])
		mockSvc.AssertExpectations(t)
	})

	t.Run("missing key", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		args := map[string]interface{}{}

		_, err := registry.executeObjectDelete(context.Background(), args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key is required")
	})
}

func TestExecuteObjectHead(t *testing.T) {
	t.Run("object exists", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("HeadObject", mock.Anything, mock.AnythingOfType("*service.HeadObjectRequest")).
			Return(&service.HeadObjectResponse{
				Key:          "test:object.txt",
				Exists:       true,
				ContentType:  "text/plain",
				Size:         1024,
				Checksum:     []byte{0xab, 0xcd},
				Version:      3,
				Status:       "active",
				Backend:      "local",
				ReplicaCount: 2,
				CreatedAt:    time.Now().UnixNano(),
				ModifiedAt:   time.Now().UnixNano(),
			}, nil)

		args := map[string]interface{}{
			"key": "test:object.txt",
		}

		result, err := registry.executeObjectHead(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap := result.(map[string]interface{})
		assert.Equal(t, true, resultMap["exists"])
		assert.Equal(t, "text/plain", resultMap["content_type"])
		mockSvc.AssertExpectations(t)
	})

	t.Run("object does not exist", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("HeadObject", mock.Anything, mock.AnythingOfType("*service.HeadObjectRequest")).
			Return(&service.HeadObjectResponse{
				Key:    "test:nonexistent.txt",
				Exists: false,
			}, nil)

		args := map[string]interface{}{
			"key": "test:nonexistent.txt",
		}

		result, err := registry.executeObjectHead(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap := result.(map[string]interface{})
		assert.Equal(t, false, resultMap["exists"])
	})

	t.Run("missing key", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		args := map[string]interface{}{}

		_, err := registry.executeObjectHead(context.Background(), args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key is required")
	})
}

// =============================================================================
// Listing Tests
// =============================================================================

func TestExecuteObjectList(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("ListObjects", mock.Anything, mock.AnythingOfType("*service.ListObjectsRequest")).
			Return(&service.ListObjectsResponse{
				Objects: []*service.ObjectSummary{
					{Key: "test:obj1.txt", Size: 100, ContentType: "text/plain", Version: 1},
					{Key: "test:obj2.txt", Size: 200, ContentType: "text/plain", Version: 1},
				},
				NextCursor: "cursor123",
				HasMore:    true,
				TotalCount: 50,
				Timestamp:  time.Now().UnixNano(),
			}, nil)

		args := map[string]interface{}{
			"prefix": "test:",
			"limit":  float64(10),
		}

		result, err := registry.executeObjectList(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap := result.(map[string]interface{})
		objects := resultMap["objects"].([]interface{})
		assert.Len(t, objects, 2)
		assert.Equal(t, true, resultMap["has_more"])
		mockSvc.AssertExpectations(t)
	})
}

func TestExecuteObjectListByTag(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("ListByTag", mock.Anything, mock.AnythingOfType("*service.ListByTagRequest")).
			Return(&service.ListByTagResponse{
				Objects: []*service.ObjectSummary{
					{Key: "test:tagged1.txt", Size: 100},
					{Key: "test:tagged2.txt", Size: 200},
				},
				HasMore:    false,
				TotalCount: 2,
			}, nil)

		args := map[string]interface{}{
			"tag_name":  "env",
			"tag_value": "production",
		}

		result, err := registry.executeObjectListByTag(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap := result.(map[string]interface{})
		objects := resultMap["objects"].([]interface{})
		assert.Len(t, objects, 2)
		mockSvc.AssertExpectations(t)
	})

	t.Run("missing tag_name", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		args := map[string]interface{}{
			"tag_value": "production",
		}

		_, err := registry.executeObjectListByTag(context.Background(), args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tag_name is required")
	})
}

func TestExecuteObjectListByTenant(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("ListByTenant", mock.Anything, mock.AnythingOfType("*service.ListByTenantRequest")).
			Return(&service.ListByTenantResponse{
				Objects: []*service.ObjectSummary{
					{Key: "tenant1:file1.txt", Size: 100},
					{Key: "tenant1:file2.txt", Size: 200},
				},
				HasMore:    false,
				TotalCount: 2,
			}, nil)

		args := map[string]interface{}{
			"tenant_id": "tenant1",
		}

		result, err := registry.executeObjectListByTenant(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)
		mockSvc.AssertExpectations(t)
	})

	t.Run("missing tenant_id", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		args := map[string]interface{}{}

		_, err := registry.executeObjectListByTenant(context.Background(), args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tenant_id is required")
	})
}

// =============================================================================
// Copy Tests
// =============================================================================

func TestExecuteObjectCopy(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("CopyObject", mock.Anything, mock.AnythingOfType("*service.CopyObjectRequest")).
			Return(&service.CopyObjectResponse{
				Key:       "dest:copy.txt",
				ObjectID:  12345,
				Size:      1024,
				Checksum:  []byte{0xab, 0xcd},
				Version:   1,
				Backend:   "local",
				CreatedAt: time.Now().UnixNano(),
			}, nil)

		args := map[string]interface{}{
			"source_key":      "src:original.txt",
			"destination_key": "dest:copy.txt",
		}

		result, err := registry.executeObjectCopy(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap := result.(map[string]interface{})
		assert.Equal(t, "dest:copy.txt", resultMap["key"])
		mockSvc.AssertExpectations(t)
	})

	t.Run("missing source_key", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		args := map[string]interface{}{
			"destination_key": "dest:copy.txt",
		}

		_, err := registry.executeObjectCopy(context.Background(), args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "source_key is required")
	})

	t.Run("missing destination_key", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		args := map[string]interface{}{
			"source_key": "src:original.txt",
		}

		_, err := registry.executeObjectCopy(context.Background(), args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "destination_key is required")
	})
}

// =============================================================================
// Replication & Migration Tests
// =============================================================================

func TestExecuteObjectReplicate(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("ReplicateObject", mock.Anything, mock.AnythingOfType("*service.ReplicateObjectRequest")).
			Return(&service.ReplicateObjectResponse{
				Key:    "test:object.txt",
				Status: "completed",
				Results: map[string]*service.ReplicationResult{
					"backend-a": {Success: true, Duration: 100},
					"backend-b": {Success: true, Duration: 150},
				},
				Timestamp: time.Now().UnixNano(),
			}, nil)

		args := map[string]interface{}{
			"key":     "test:object.txt",
			"targets": []interface{}{"backend-a", "backend-b"},
		}

		result, err := registry.executeObjectReplicate(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap := result.(map[string]interface{})
		assert.Equal(t, "completed", resultMap["status"])
		mockSvc.AssertExpectations(t)
	})

	t.Run("missing targets", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		args := map[string]interface{}{
			"key": "test:object.txt",
		}

		_, err := registry.executeObjectReplicate(context.Background(), args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "targets cannot be empty")
	})
}

func TestExecuteObjectMigrate(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("MigrateObject", mock.Anything, mock.AnythingOfType("*service.MigrateObjectRequest")).
			Return(&service.MigrateObjectResponse{
				Key:               "test:object.txt",
				SourceBackend:     "local",
				TargetBackend:     "s3",
				Success:           true,
				DeletedFromSource: true,
				Timestamp:         time.Now().UnixNano(),
			}, nil)

		args := map[string]interface{}{
			"key":            "test:object.txt",
			"target_backend": "s3",
		}

		result, err := registry.executeObjectMigrate(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap := result.(map[string]interface{})
		assert.Equal(t, true, resultMap["success"])
		mockSvc.AssertExpectations(t)
	})

	t.Run("missing target_backend", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		args := map[string]interface{}{
			"key": "test:object.txt",
		}

		_, err := registry.executeObjectMigrate(context.Background(), args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "target_backend is required")
	})
}

// =============================================================================
// Archive & Restore Tests
// =============================================================================

func TestExecuteObjectArchive(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("ArchiveObject", mock.Anything, mock.AnythingOfType("*service.ArchiveObjectRequest")).
			Return(&service.ArchiveObjectResponse{
				Key:                "test:object.txt",
				ArchiveStore:       "glacier",
				Success:            true,
				DeletedFromPrimary: true,
				ArchivedAt:         time.Now().UnixNano(),
			}, nil)

		args := map[string]interface{}{
			"key":          "test:object.txt",
			"target_store": "glacier",
		}

		result, err := registry.executeObjectArchive(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap := result.(map[string]interface{})
		assert.Equal(t, true, resultMap["success"])
		mockSvc.AssertExpectations(t)
	})
}

func TestExecuteObjectRestore(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("RestoreObject", mock.Anything, mock.AnythingOfType("*service.RestoreObjectRequest")).
			Return(&service.RestoreObjectResponse{
				Key:        "test:object.txt",
				Backend:    "local",
				Success:    true,
				RestoredAt: time.Now().UnixNano(),
			}, nil)

		args := map[string]interface{}{
			"key": "test:object.txt",
		}

		result, err := registry.executeObjectRestore(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap := result.(map[string]interface{})
		assert.Equal(t, true, resultMap["success"])
		mockSvc.AssertExpectations(t)
	})
}

// =============================================================================
// Verification Tests
// =============================================================================

func TestExecuteObjectVerify(t *testing.T) {
	t.Run("success - all valid", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("VerifyObject", mock.Anything, mock.AnythingOfType("*service.VerifyObjectRequest")).
			Return(&service.VerifyObjectResponse{
				Key:   "test:object.txt",
				Valid: true,
				Results: map[string]*service.VerificationResult{
					"local": {Valid: true, ChecksumMatch: true, SizeMatch: true},
					"s3":    {Valid: true, ChecksumMatch: true, SizeMatch: true},
				},
				Repaired:  false,
				Timestamp: time.Now().UnixNano(),
			}, nil)

		args := map[string]interface{}{
			"key":    "test:object.txt",
			"stores": []interface{}{"local", "s3"},
		}

		result, err := registry.executeObjectVerify(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap := result.(map[string]interface{})
		assert.Equal(t, true, resultMap["valid"])
		mockSvc.AssertExpectations(t)
	})
}

// =============================================================================
// Policy Tests
// =============================================================================

func TestExecuteObjectSetPolicy(t *testing.T) {
	t.Run("success with all policies", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("SetObjectPolicy", mock.Anything, mock.AnythingOfType("*service.SetObjectPolicyRequest")).
			Return(&service.SetObjectPolicyResponse{
				Key:       "test:object.txt",
				Success:   true,
				Version:   2,
				Timestamp: time.Now().UnixNano(),
			}, nil)

		args := map[string]interface{}{
			"key": "test:object.txt",
			"replication": map[string]interface{}{
				"enabled":       true,
				"min_replicas":  float64(3),
				"target_stores": []interface{}{"s3", "gcs"},
			},
			"archive": map[string]interface{}{
				"enabled":    true,
				"after_days": float64(90),
			},
			"retention": map[string]interface{}{
				"enabled": true,
				"days":    float64(365),
			},
		}

		result, err := registry.executeObjectSetPolicy(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap := result.(map[string]interface{})
		assert.Equal(t, true, resultMap["success"])
		mockSvc.AssertExpectations(t)
	})
}

func TestExecuteObjectGetPolicy(t *testing.T) {
	t.Run("success with policies", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("GetObjectPolicy", mock.Anything, mock.AnythingOfType("*service.GetObjectPolicyRequest")).
			Return(&service.GetObjectPolicyResponse{
				Key: "test:object.txt",
				ReplicationPolicy: &catalog.ReplicationPolicy{
					Enabled:      true,
					MinReplicas:  3,
					TargetStores: []string{"s3", "gcs"},
				},
				ArchivePolicy: &catalog.ArchivePolicy{
					Enabled:          true,
					ArchiveAfterDays: 90,
				},
				RetentionPolicy: &catalog.RetentionPolicy{
					Enabled:       true,
					RetentionDays: 365,
				},
				Timestamp: time.Now().UnixNano(),
			}, nil)

		args := map[string]interface{}{
			"key": "test:object.txt",
		}

		result, err := registry.executeObjectGetPolicy(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap := result.(map[string]interface{})
		assert.NotNil(t, resultMap["replication"])
		assert.NotNil(t, resultMap["archive"])
		assert.NotNil(t, resultMap["retention"])
		mockSvc.AssertExpectations(t)
	})
}

// =============================================================================
// Backend Management Tests
// =============================================================================

func TestExecuteObjectListBackends(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("ListBackends", mock.Anything, mock.AnythingOfType("*service.ListBackendsRequest")).
			Return(&service.ListBackendsResponse{
				Backends: []*service.BackendInfo{
					{Name: "local", Type: "local", Enabled: true, Default: true},
					{Name: "s3-us-east", Type: "s3", Enabled: true},
				},
				Timestamp: time.Now().UnixNano(),
			}, nil)

		args := map[string]interface{}{}

		result, err := registry.executeObjectListBackends(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap := result.(map[string]interface{})
		backends := resultMap["backends"].([]interface{})
		assert.Len(t, backends, 2)
		mockSvc.AssertExpectations(t)
	})
}

func TestExecuteObjectBackendHealth(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("GetBackendHealth", mock.Anything, mock.AnythingOfType("*service.GetBackendHealthRequest")).
			Return(&service.GetBackendHealthResponse{
				Name:      "s3-us-east",
				Healthy:   true,
				Latency:   50,
				LastCheck: time.Now().UnixNano(),
				Status:    "operational",
			}, nil)

		args := map[string]interface{}{
			"name": "s3-us-east",
		}

		result, err := registry.executeObjectBackendHealth(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap := result.(map[string]interface{})
		assert.Equal(t, true, resultMap["healthy"])
		mockSvc.AssertExpectations(t)
	})

	t.Run("missing name", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		args := map[string]interface{}{}

		_, err := registry.executeObjectBackendHealth(context.Background(), args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name is required")
	})
}

// =============================================================================
// Tenant Management Tests
// =============================================================================

func TestExecuteObjectTenantUsage(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("GetTenantUsage", mock.Anything, mock.AnythingOfType("*service.GetTenantUsageRequest")).
			Return(&service.GetTenantUsageResponse{
				TenantID:      "tenant-1",
				ObjectCount:   1000,
				TotalSize:     1024 * 1024 * 100,
				ActiveCount:   950,
				ArchivedCount: 50,
				Timestamp:     time.Now().UnixNano(),
			}, nil)

		args := map[string]interface{}{
			"tenant_id": "tenant-1",
		}

		result, err := registry.executeObjectTenantUsage(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap := result.(map[string]interface{})
		assert.Equal(t, "tenant-1", resultMap["tenant_id"])
		mockSvc.AssertExpectations(t)
	})

	t.Run("missing tenant_id", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		args := map[string]interface{}{}

		_, err := registry.executeObjectTenantUsage(context.Background(), args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tenant_id is required")
	})
}

func TestExecuteObjectTenantQuotas(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockSvc := &mockObjectStorageService{}
		registry := newTestObjectStorageRegistry(mockSvc)

		mockSvc.On("GetTenantQuotas", mock.Anything, mock.AnythingOfType("*service.GetTenantQuotasRequest")).
			Return(&service.GetTenantQuotasResponse{
				TenantID:         "tenant-1",
				MaxStorageBytes:  1024 * 1024 * 1024,
				MaxObjectSize:    100 * 1024 * 1024,
				MaxObjectCount:   10000,
				UsedStorageBytes: 500 * 1024 * 1024,
				UsedObjectCount:  5000,
				Timestamp:        time.Now().UnixNano(),
			}, nil)

		args := map[string]interface{}{
			"tenant_id": "tenant-1",
		}

		result, err := registry.executeObjectTenantQuotas(context.Background(), args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap := result.(map[string]interface{})
		assert.Equal(t, "tenant-1", resultMap["tenant_id"])
		mockSvc.AssertExpectations(t)
	})
}

// =============================================================================
// Service Not Available Tests
// =============================================================================

func TestObjectStorageServiceNotAvailable(t *testing.T) {
	mockQuicBaaS := &MockQuicBaaS{}
	mockQuicBaaS.On("ObjectStorageService").Return(nil)

	registry := &ToolRegistry{
		service: mockQuicBaaS,
	}

	t.Run("put fails when service unavailable", func(t *testing.T) {
		args := map[string]interface{}{
			"key":   "test:object.txt",
			"value": "test",
		}
		_, err := registry.executeObjectPut(context.Background(), args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not available")
	})

	t.Run("get fails when service unavailable", func(t *testing.T) {
		args := map[string]interface{}{
			"key": "test:object.txt",
		}
		_, err := registry.executeObjectGet(context.Background(), args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not available")
	})
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestObjectStorageHelperFunctions(t *testing.T) {
	t.Run("getStringFromMap", func(t *testing.T) {
		m := map[string]interface{}{
			"key1": "value1",
			"key2": 123,
		}
		assert.Equal(t, "value1", getStringFromMap(m, "key1"))
		assert.Equal(t, "", getStringFromMap(m, "key2"))
		assert.Equal(t, "", getStringFromMap(m, "key3"))
	})

	t.Run("getUint64", func(t *testing.T) {
		m := map[string]interface{}{
			"float":    float64(123.5),
			"int":      42,
			"int64":    int64(100),
			"uint64":   uint64(200),
			"string":   "not a number",
			"negative": -7,
		}
		assert.Equal(t, uint64(123), getUint64(m, "float"))
		assert.Equal(t, uint64(42), getUint64(m, "int"))
		assert.Equal(t, uint64(100), getUint64(m, "int64"))
		assert.Equal(t, uint64(200), getUint64(m, "uint64"))
		assert.Equal(t, uint64(0), getUint64(m, "string"))
		assert.Equal(t, uint64(0), getUint64(m, "negative"))
	})

	t.Run("getBoolFromMap", func(t *testing.T) {
		m := map[string]interface{}{
			"true":   true,
			"false":  false,
			"string": "true",
		}
		assert.Equal(t, true, getBoolFromMap(m, "true"))
		assert.Equal(t, false, getBoolFromMap(m, "false"))
		assert.Equal(t, false, getBoolFromMap(m, "string"))
	})
}

// =============================================================================
// Tool Registration Test
// =============================================================================

func TestRegisterObjectStorageTools(t *testing.T) {
	registry := &ToolRegistry{
		tools: make([]ToolDefinition, 0),
	}

	registry.registerObjectStorageTools()

	// Verify all tools are registered
	expectedTools := []string{
		"quicbaas_object_put",
		"quicbaas_object_get",
		"quicbaas_object_delete",
		"quicbaas_object_head",
		"quicbaas_object_list",
		"quicbaas_object_list_by_tag",
		"quicbaas_object_list_by_tenant",
		"quicbaas_object_copy",
		"quicbaas_object_replicate",
		"quicbaas_object_migrate",
		"quicbaas_object_archive",
		"quicbaas_object_restore",
		"quicbaas_object_verify",
		"quicbaas_object_set_policy",
		"quicbaas_object_get_policy",
		"quicbaas_object_list_backends",
		"quicbaas_object_backend_health",
		"quicbaas_object_tenant_usage",
		"quicbaas_object_tenant_quotas",
	}

	registeredNames := make(map[string]bool)
	for _, tool := range registry.tools {
		registeredNames[tool.Name] = true
	}

	for _, expected := range expectedTools {
		assert.True(t, registeredNames[expected], "Tool %s should be registered", expected)
	}
}
