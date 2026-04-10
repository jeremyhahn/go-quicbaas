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
	"encoding/hex"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock Object Transfer Service
// =============================================================================

type mockObjectTransferService struct {
	mock.Mock
}

func (m *mockObjectTransferService) FetchObject(ctx context.Context, req *service.FetchObjectRequest) (*service.FetchObjectResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.FetchObjectResponse), args.Error(1)
}

func (m *mockObjectTransferService) StreamFetchObjects(ctx context.Context, req *service.StreamFetchObjectsRequest) (*service.StreamFetchObjectsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.StreamFetchObjectsResponse), args.Error(1)
}

// =============================================================================
// Test Helpers
// =============================================================================

func newTestObjectTransferRegistry(mockSvc *mockObjectTransferService) *ToolRegistry {
	mockQuicBaaS := &MockQuicBaaS{}
	mockQuicBaaS.objectTransferSvc = mockSvc

	log, _ := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})

	return &ToolRegistry{
		service: mockQuicBaaS,
		logger:  log,
	}
}

func newTestObjectTransferRegistryNilService() *ToolRegistry {
	mockQuicBaaS := &MockQuicBaaS{}

	log, _ := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})

	return &ToolRegistry{
		service: mockQuicBaaS,
		logger:  log,
	}
}

// =============================================================================
// Tool Registration Tests
// =============================================================================

func TestRegisterObjectTransferTools(t *testing.T) {
	t.Run("registers object transfer tools", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)
		require.NotNil(t, registry)

		// Register tools
		registry.tools = make([]ToolDefinition, 0)
		registry.registerObjectTransferTools()

		toolNames := make(map[string]bool)
		for _, tool := range registry.tools {
			toolNames[tool.Name] = true
		}

		// Verify object transfer tools are registered
		assert.True(t, toolNames["quicbaas_object_transfer_fetch"], "object_transfer_fetch should be registered")
		assert.True(t, toolNames["quicbaas_object_transfer_stream_fetch"], "object_transfer_stream_fetch should be registered")
	})

	t.Run("fetch tool has correct schema", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		registry.tools = make([]ToolDefinition, 0)
		registry.registerObjectTransferTools()

		var fetchTool *ToolDefinition
		for _, tool := range registry.tools {
			if tool.Name == "quicbaas_object_transfer_fetch" {
				fetchTool = &tool
				break
			}
		}

		require.NotNil(t, fetchTool)
		assert.Contains(t, fetchTool.Description, "Fetch a single object")

		// Verify schema has required fields
		schema := fetchTool.InputSchema
		props, ok := schema["properties"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, props, "shard_id")
		assert.Contains(t, props, "object_ref")
		assert.Contains(t, props, "checksum")
		assert.Contains(t, props, "request_id")

		required, ok := schema["required"].([]string)
		require.True(t, ok)
		assert.Contains(t, required, "shard_id")
		assert.Contains(t, required, "object_ref")
	})

	t.Run("stream_fetch tool has correct schema", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		registry.tools = make([]ToolDefinition, 0)
		registry.registerObjectTransferTools()

		var streamFetchTool *ToolDefinition
		for _, tool := range registry.tools {
			if tool.Name == "quicbaas_object_transfer_stream_fetch" {
				streamFetchTool = &tool
				break
			}
		}

		require.NotNil(t, streamFetchTool)
		assert.Contains(t, streamFetchTool.Description, "Fetch multiple objects")

		// Verify schema has required fields
		schema := streamFetchTool.InputSchema
		props, ok := schema["properties"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, props, "shard_id")
		assert.Contains(t, props, "items")

		required, ok := schema["required"].([]string)
		require.True(t, ok)
		assert.Contains(t, required, "shard_id")
		assert.Contains(t, required, "items")
	})
}

// =============================================================================
// isObjectTransferTool Tests
// =============================================================================

func TestIsObjectTransferTool(t *testing.T) {
	t.Run("returns true for object transfer tools", func(t *testing.T) {
		assert.True(t, isObjectTransferTool("quicbaas_object_transfer_fetch"))
		assert.True(t, isObjectTransferTool("quicbaas_object_transfer_stream_fetch"))
	})

	t.Run("returns false for non-object transfer tools", func(t *testing.T) {
		assert.False(t, isObjectTransferTool("quicbaas_object_put"))
		assert.False(t, isObjectTransferTool("quicbaas_object_get"))
		assert.False(t, isObjectTransferTool("quicbaas_kms_encrypt"))
		assert.False(t, isObjectTransferTool("unknown_tool"))
	})
}

// =============================================================================
// Execute Fetch Object Tests
// =============================================================================

func TestExecuteObjectTransferFetch(t *testing.T) {
	ctx := context.Background()

	t.Run("successful fetch with found object", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		objectData := []byte("test object data")
		checksum := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20}

		mockSvc.On("FetchObject", mock.Anything, mock.MatchedBy(func(req *service.FetchObjectRequest) bool {
			return req.ShardID == 1 && req.ObjectRef == "namespace:test/object.bin"
		})).Return(&service.FetchObjectResponse{
			ObjectRef:  "namespace:test/object.bin",
			Data:       objectData,
			Checksum:   checksum,
			Size:       int64(len(objectData)),
			Compressed: false,
			Found:      true,
		}, nil)

		args := map[string]interface{}{
			"shard_id":   float64(1),
			"object_ref": "namespace:test/object.bin",
		}

		result, err := registry.executeObjectTransferFetch(ctx, args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)

		assert.Equal(t, "namespace:test/object.bin", resultMap["object_ref"])
		assert.Equal(t, true, resultMap["found"])
		assert.Equal(t, base64.StdEncoding.EncodeToString(objectData), resultMap["data"])
		assert.Equal(t, hex.EncodeToString(checksum), resultMap["checksum"])
		assert.Equal(t, float64(len(objectData)), resultMap["size"])
		assert.Equal(t, false, resultMap["compressed"])

		mockSvc.AssertExpectations(t)
	})

	t.Run("fetch with object not found", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		mockSvc.On("FetchObject", mock.Anything, mock.Anything).Return(&service.FetchObjectResponse{
			ObjectRef:    "namespace:test/missing.bin",
			Found:        false,
			ErrorMessage: "object not found",
		}, nil)

		args := map[string]interface{}{
			"shard_id":   float64(1),
			"object_ref": "namespace:test/missing.bin",
		}

		result, err := registry.executeObjectTransferFetch(ctx, args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)

		assert.Equal(t, false, resultMap["found"])
		assert.Equal(t, "object not found", resultMap["error_message"])

		mockSvc.AssertExpectations(t)
	})

	t.Run("fetch with checksum verification", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		objectData := []byte("test data with checksum")
		expectedChecksum := []byte{0xaa, 0xbb, 0xcc, 0xdd}

		mockSvc.On("FetchObject", mock.Anything, mock.MatchedBy(func(req *service.FetchObjectRequest) bool {
			return req.ShardID == 2 && req.ObjectRef == "ns:path/file.dat" && len(req.Checksum) == 4
		})).Return(&service.FetchObjectResponse{
			ObjectRef: "ns:path/file.dat",
			Data:      objectData,
			Checksum:  expectedChecksum,
			Size:      int64(len(objectData)),
			Found:     true,
		}, nil)

		args := map[string]interface{}{
			"shard_id":   float64(2),
			"object_ref": "ns:path/file.dat",
			"checksum":   base64.StdEncoding.EncodeToString(expectedChecksum),
		}

		result, err := registry.executeObjectTransferFetch(ctx, args)
		require.NoError(t, err)
		require.NotNil(t, result)

		mockSvc.AssertExpectations(t)
	})

	t.Run("fetch with compressed object", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		compressedData := []byte{0x1f, 0x8b, 0x08} // gzip magic number prefix

		mockSvc.On("FetchObject", mock.Anything, mock.Anything).Return(&service.FetchObjectResponse{
			ObjectRef:       "ns:compressed.gz",
			Data:            compressedData,
			Checksum:        []byte{0x00},
			Size:            int64(len(compressedData)),
			Compressed:      true,
			CompressionAlgo: "gzip",
			Found:           true,
		}, nil)

		args := map[string]interface{}{
			"shard_id":   float64(1),
			"object_ref": "ns:compressed.gz",
		}

		result, err := registry.executeObjectTransferFetch(ctx, args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)

		assert.Equal(t, true, resultMap["compressed"])
		assert.Equal(t, "gzip", resultMap["compression_algo"])

		mockSvc.AssertExpectations(t)
	})

	t.Run("error when service unavailable", func(t *testing.T) {
		registry := newTestObjectTransferRegistryNilService()

		args := map[string]interface{}{
			"shard_id":   float64(1),
			"object_ref": "ns:test.bin",
		}

		_, err := registry.executeObjectTransferFetch(ctx, args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "object transfer service not available")
	})

	t.Run("error when shard_id missing", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		args := map[string]interface{}{
			"object_ref": "ns:test.bin",
		}

		_, err := registry.executeObjectTransferFetch(ctx, args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shard_id is required")
	})

	t.Run("error when object_ref missing", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		args := map[string]interface{}{
			"shard_id": float64(1),
		}

		_, err := registry.executeObjectTransferFetch(ctx, args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "object_ref is required")
	})

	t.Run("error when checksum encoding invalid", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		args := map[string]interface{}{
			"shard_id":   float64(1),
			"object_ref": "ns:test.bin",
			"checksum":   "not-valid-base64!!!",
		}

		_, err := registry.executeObjectTransferFetch(ctx, args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid base64 checksum")
	})

	t.Run("service error propagated", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		mockSvc.On("FetchObject", mock.Anything, mock.Anything).Return(nil, stderrors.New("network timeout"))

		args := map[string]interface{}{
			"shard_id":   float64(1),
			"object_ref": "ns:test.bin",
		}

		_, err := registry.executeObjectTransferFetch(ctx, args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "network timeout")

		mockSvc.AssertExpectations(t)
	})
}

// =============================================================================
// Execute Stream Fetch Objects Tests
// =============================================================================

func TestExecuteObjectTransferStreamFetch(t *testing.T) {
	ctx := context.Background()

	t.Run("successful stream fetch multiple objects", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		mockSvc.On("StreamFetchObjects", mock.Anything, mock.MatchedBy(func(req *service.StreamFetchObjectsRequest) bool {
			return req.ShardID == 1 && len(req.Items) == 2
		})).Return(&service.StreamFetchObjectsResponse{
			Objects: []service.FetchObjectResponse{
				{
					ObjectRef: "ns:obj1.bin",
					Data:      []byte("data1"),
					Checksum:  []byte{0x01},
					Size:      5,
					Found:     true,
				},
				{
					ObjectRef: "ns:obj2.bin",
					Data:      []byte("data2"),
					Checksum:  []byte{0x02},
					Size:      5,
					Found:     true,
				},
			},
			SuccessCount: 2,
			FailureCount: 0,
			TotalCount:   2,
		}, nil)

		args := map[string]interface{}{
			"shard_id": float64(1),
			"items": []interface{}{
				map[string]interface{}{"object_ref": "ns:obj1.bin"},
				map[string]interface{}{"object_ref": "ns:obj2.bin"},
			},
		}

		result, err := registry.executeObjectTransferStreamFetch(ctx, args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)

		assert.Equal(t, float64(2), resultMap["success_count"])
		assert.Equal(t, float64(0), resultMap["failure_count"])
		assert.Equal(t, float64(2), resultMap["total_count"])

		objects, ok := resultMap["objects"].([]interface{})
		require.True(t, ok)
		assert.Len(t, objects, 2)

		mockSvc.AssertExpectations(t)
	})

	t.Run("stream fetch with partial failures", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		mockSvc.On("StreamFetchObjects", mock.Anything, mock.Anything).Return(&service.StreamFetchObjectsResponse{
			Objects: []service.FetchObjectResponse{
				{
					ObjectRef: "ns:found.bin",
					Data:      []byte("found data"),
					Checksum:  []byte{0x01},
					Size:      10,
					Found:     true,
				},
				{
					ObjectRef:    "ns:missing.bin",
					Found:        false,
					ErrorMessage: "object not found",
				},
			},
			SuccessCount: 1,
			FailureCount: 1,
			TotalCount:   2,
		}, nil)

		args := map[string]interface{}{
			"shard_id": float64(1),
			"items": []interface{}{
				map[string]interface{}{"object_ref": "ns:found.bin"},
				map[string]interface{}{"object_ref": "ns:missing.bin"},
			},
		}

		result, err := registry.executeObjectTransferStreamFetch(ctx, args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok)

		assert.Equal(t, float64(1), resultMap["success_count"])
		assert.Equal(t, float64(1), resultMap["failure_count"])

		mockSvc.AssertExpectations(t)
	})

	t.Run("stream fetch with checksums", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		checksum1 := []byte{0xaa, 0xbb}
		checksum2 := []byte{0xcc, 0xdd}

		mockSvc.On("StreamFetchObjects", mock.Anything, mock.MatchedBy(func(req *service.StreamFetchObjectsRequest) bool {
			return len(req.Items) == 2 &&
				len(req.Items[0].Checksum) == 2 &&
				len(req.Items[1].Checksum) == 2
		})).Return(&service.StreamFetchObjectsResponse{
			Objects: []service.FetchObjectResponse{
				{ObjectRef: "ns:obj1.bin", Data: []byte("d1"), Checksum: checksum1, Size: 2, Found: true},
				{ObjectRef: "ns:obj2.bin", Data: []byte("d2"), Checksum: checksum2, Size: 2, Found: true},
			},
			SuccessCount: 2,
			FailureCount: 0,
			TotalCount:   2,
		}, nil)

		args := map[string]interface{}{
			"shard_id": float64(1),
			"items": []interface{}{
				map[string]interface{}{
					"object_ref": "ns:obj1.bin",
					"checksum":   base64.StdEncoding.EncodeToString(checksum1),
				},
				map[string]interface{}{
					"object_ref": "ns:obj2.bin",
					"checksum":   base64.StdEncoding.EncodeToString(checksum2),
				},
			},
		}

		result, err := registry.executeObjectTransferStreamFetch(ctx, args)
		require.NoError(t, err)
		require.NotNil(t, result)

		mockSvc.AssertExpectations(t)
	})

	t.Run("error when service unavailable", func(t *testing.T) {
		registry := newTestObjectTransferRegistryNilService()

		args := map[string]interface{}{
			"shard_id": float64(1),
			"items": []interface{}{
				map[string]interface{}{"object_ref": "ns:test.bin"},
			},
		}

		_, err := registry.executeObjectTransferStreamFetch(ctx, args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "object transfer service not available")
	})

	t.Run("error when shard_id missing", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		args := map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{"object_ref": "ns:test.bin"},
			},
		}

		_, err := registry.executeObjectTransferStreamFetch(ctx, args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shard_id is required")
	})

	t.Run("error when items missing", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		args := map[string]interface{}{
			"shard_id": float64(1),
		}

		_, err := registry.executeObjectTransferStreamFetch(ctx, args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "items cannot be empty")
	})

	t.Run("error when items empty", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		args := map[string]interface{}{
			"shard_id": float64(1),
			"items":    []interface{}{},
		}

		_, err := registry.executeObjectTransferStreamFetch(ctx, args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "items cannot be empty")
	})

	t.Run("error when item object_ref missing", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		args := map[string]interface{}{
			"shard_id": float64(1),
			"items": []interface{}{
				map[string]interface{}{"checksum": "AAAA"},
			},
		}

		_, err := registry.executeObjectTransferStreamFetch(ctx, args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "item 0 object_ref is required")
	})

	t.Run("error when item checksum invalid", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		args := map[string]interface{}{
			"shard_id": float64(1),
			"items": []interface{}{
				map[string]interface{}{
					"object_ref": "ns:test.bin",
					"checksum":   "not-valid-base64!!!",
				},
			},
		}

		_, err := registry.executeObjectTransferStreamFetch(ctx, args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "item 0: checksum encoding")
	})

	t.Run("error when item not a map", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		args := map[string]interface{}{
			"shard_id": float64(1),
			"items": []interface{}{
				"not a map",
			},
		}

		_, err := registry.executeObjectTransferStreamFetch(ctx, args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "item 0 is not a valid object")
	})

	t.Run("service error propagated", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		mockSvc.On("StreamFetchObjects", mock.Anything, mock.Anything).Return(nil, stderrors.New("shard not found"))

		args := map[string]interface{}{
			"shard_id": float64(999),
			"items": []interface{}{
				map[string]interface{}{"object_ref": "ns:test.bin"},
			},
		}

		_, err := registry.executeObjectTransferStreamFetch(ctx, args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shard not found")

		mockSvc.AssertExpectations(t)
	})
}

// =============================================================================
// Execute Tool Router Tests
// =============================================================================

func TestExecuteObjectTransferTool(t *testing.T) {
	ctx := context.Background()

	t.Run("routes to fetch handler", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		mockSvc.On("FetchObject", mock.Anything, mock.Anything).Return(&service.FetchObjectResponse{
			ObjectRef: "ns:test.bin",
			Found:     true,
			Data:      []byte("test"),
			Checksum:  []byte{0x01},
			Size:      4,
		}, nil)

		args := map[string]interface{}{
			"shard_id":   float64(1),
			"object_ref": "ns:test.bin",
		}

		result, err := registry.executeObjectTransferTool(ctx, "quicbaas_object_transfer_fetch", args)
		require.NoError(t, err)
		require.NotNil(t, result)

		mockSvc.AssertExpectations(t)
	})

	t.Run("routes to stream_fetch handler", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		mockSvc.On("StreamFetchObjects", mock.Anything, mock.Anything).Return(&service.StreamFetchObjectsResponse{
			Objects:      []service.FetchObjectResponse{},
			SuccessCount: 0,
			FailureCount: 0,
			TotalCount:   0,
		}, nil)

		args := map[string]interface{}{
			"shard_id": float64(1),
			"items": []interface{}{
				map[string]interface{}{"object_ref": "ns:test.bin"},
			},
		}

		result, err := registry.executeObjectTransferTool(ctx, "quicbaas_object_transfer_stream_fetch", args)
		require.NoError(t, err)
		require.NotNil(t, result)

		mockSvc.AssertExpectations(t)
	})

	t.Run("returns error for unknown tool", func(t *testing.T) {
		mockSvc := new(mockObjectTransferService)
		registry := newTestObjectTransferRegistry(mockSvc)

		_, err := registry.executeObjectTransferTool(ctx, "quicbaas_object_transfer_unknown", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown tool: quicbaas_object_transfer_unknown")
	})
}
