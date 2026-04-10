//go:build unix

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

package objectstorage

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockObjectStoreReader implements ObjectStoreReader for testing.
type mockObjectStoreReader struct {
	readObjectFunc func(objectRef string) ([]byte, error)
}

func (m *mockObjectStoreReader) ReadObject(objectRef string) ([]byte, error) {
	if m.readObjectFunc != nil {
		return m.readObjectFunc(objectRef)
	}
	return []byte("test-data"), nil
}

func TestHandleFetchObject(t *testing.T) {
	mockService := &mockObjectStorageService{}
	logger := &testLogger{}

	handler, err := NewHandler(mockService, nil, logger)
	require.NoError(t, err)

	// Register a mock object store
	testData := []byte("test-object-data")
	mockStore := &mockObjectStoreReader{
		readObjectFunc: func(objectRef string) ([]byte, error) {
			if objectRef == "shard-1/object-123" {
				return testData, nil
			}
			return nil, errors.New("object not found")
		},
	}
	handler.RegisterObjectStore(1, mockStore)

	// Test successful fetch
	req := &FetchObjectRequest{
		ShardID:   1,
		ObjectRef: "shard-1/object-123",
	}
	reqData, err := EncodePayload(req)
	require.NoError(t, err)

	respData, err := handler.handleFetchObject(context.Background(), reqData)
	require.NoError(t, err)

	var resp FetchObjectResponse
	err = DecodePayload(respData, &resp)
	require.NoError(t, err)

	assert.True(t, resp.Found)
	assert.Equal(t, "shard-1/object-123", resp.ObjectRef)
	assert.Equal(t, testData, resp.Data)
	assert.Equal(t, int64(len(testData)), resp.Size)
	assert.Empty(t, resp.ErrorMessage)

	// Verify checksum
	hash := sha256.Sum256(testData)
	assert.Equal(t, hash[:], resp.Checksum)
}

func TestHandleFetchObjectNotFound(t *testing.T) {
	mockService := &mockObjectStorageService{}
	logger := &testLogger{}

	handler, err := NewHandler(mockService, nil, logger)
	require.NoError(t, err)

	// Register a mock object store that returns not found
	mockStore := &mockObjectStoreReader{
		readObjectFunc: func(objectRef string) ([]byte, error) {
			return nil, errors.New("object not found in storage")
		},
	}
	handler.RegisterObjectStore(1, mockStore)

	req := &FetchObjectRequest{
		ShardID:   1,
		ObjectRef: "shard-1/nonexistent",
	}
	reqData, err := EncodePayload(req)
	require.NoError(t, err)

	respData, err := handler.handleFetchObject(context.Background(), reqData)
	require.NoError(t, err)

	var resp FetchObjectResponse
	err = DecodePayload(respData, &resp)
	require.NoError(t, err)

	assert.False(t, resp.Found)
	assert.Equal(t, "shard-1/nonexistent", resp.ObjectRef)
	assert.Nil(t, resp.Data)
	assert.NotEmpty(t, resp.ErrorMessage)
}

func TestHandleFetchObjectShardNotFound(t *testing.T) {
	mockService := &mockObjectStorageService{}
	logger := &testLogger{}

	handler, err := NewHandler(mockService, nil, logger)
	require.NoError(t, err)

	// Do not register any object store for shard 99
	req := &FetchObjectRequest{
		ShardID:   99,
		ObjectRef: "shard-99/object-123",
	}
	reqData, err := EncodePayload(req)
	require.NoError(t, err)

	respData, err := handler.handleFetchObject(context.Background(), reqData)
	require.NoError(t, err)

	var resp FetchObjectResponse
	err = DecodePayload(respData, &resp)
	require.NoError(t, err)

	assert.False(t, resp.Found)
	assert.Contains(t, resp.ErrorMessage, "shard 99 not found")
}

func TestHandleFetchObjectMissingObjectRef(t *testing.T) {
	mockService := &mockObjectStorageService{}
	logger := &testLogger{}

	handler, err := NewHandler(mockService, nil, logger)
	require.NoError(t, err)

	req := &FetchObjectRequest{
		ShardID:   1,
		ObjectRef: "", // Empty object ref
	}
	reqData, err := EncodePayload(req)
	require.NoError(t, err)

	_, err = handler.handleFetchObject(context.Background(), reqData)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "object_ref is required")
}

func TestHandleFetchObjectChecksumMismatch(t *testing.T) {
	mockService := &mockObjectStorageService{}
	logger := &testLogger{}

	handler, err := NewHandler(mockService, nil, logger)
	require.NoError(t, err)

	testData := []byte("test-object-data")
	mockStore := &mockObjectStoreReader{
		readObjectFunc: func(objectRef string) ([]byte, error) {
			return testData, nil
		},
	}
	handler.RegisterObjectStore(1, mockStore)

	// Send with wrong checksum
	wrongChecksum := sha256.Sum256([]byte("different-data"))
	req := &FetchObjectRequest{
		ShardID:   1,
		ObjectRef: "shard-1/object-123",
		Checksum:  wrongChecksum[:],
	}
	reqData, err := EncodePayload(req)
	require.NoError(t, err)

	respData, err := handler.handleFetchObject(context.Background(), reqData)
	require.NoError(t, err)

	var resp FetchObjectResponse
	err = DecodePayload(respData, &resp)
	require.NoError(t, err)

	assert.True(t, resp.Found)
	assert.Equal(t, "checksum mismatch", resp.ErrorMessage)
	assert.Nil(t, resp.Data)
}

func TestHandleFetchObjectChecksumValid(t *testing.T) {
	mockService := &mockObjectStorageService{}
	logger := &testLogger{}

	handler, err := NewHandler(mockService, nil, logger)
	require.NoError(t, err)

	testData := []byte("test-object-data")
	mockStore := &mockObjectStoreReader{
		readObjectFunc: func(objectRef string) ([]byte, error) {
			return testData, nil
		},
	}
	handler.RegisterObjectStore(1, mockStore)

	// Send with correct checksum
	correctChecksum := sha256.Sum256(testData)
	req := &FetchObjectRequest{
		ShardID:   1,
		ObjectRef: "shard-1/object-123",
		Checksum:  correctChecksum[:],
	}
	reqData, err := EncodePayload(req)
	require.NoError(t, err)

	respData, err := handler.handleFetchObject(context.Background(), reqData)
	require.NoError(t, err)

	var resp FetchObjectResponse
	err = DecodePayload(respData, &resp)
	require.NoError(t, err)

	assert.True(t, resp.Found)
	assert.Empty(t, resp.ErrorMessage)
	assert.Equal(t, testData, resp.Data)
}

func TestHandleStreamFetchObjects(t *testing.T) {
	mockService := &mockObjectStorageService{}
	logger := &testLogger{}

	handler, err := NewHandler(mockService, nil, logger)
	require.NoError(t, err)

	// Register a mock object store with multiple objects
	objects := map[string][]byte{
		"obj-1": []byte("data-1"),
		"obj-2": []byte("data-2"),
		"obj-3": []byte("data-3"),
	}
	mockStore := &mockObjectStoreReader{
		readObjectFunc: func(objectRef string) ([]byte, error) {
			if data, ok := objects[objectRef]; ok {
				return data, nil
			}
			return nil, errors.New("object not found")
		},
	}
	handler.RegisterObjectStore(1, mockStore)

	req := &StreamFetchObjectsRequest{
		ShardID: 1,
		Items: []*ObjectFetchItem{
			{ObjectRef: "obj-1"},
			{ObjectRef: "obj-2"},
			{ObjectRef: "obj-3"},
		},
	}
	reqData, err := EncodePayload(req)
	require.NoError(t, err)

	respData, err := handler.handleStreamFetchObjects(context.Background(), reqData)
	require.NoError(t, err)

	var resp StreamFetchObjectsResponse
	err = DecodePayload(respData, &resp)
	require.NoError(t, err)

	assert.Equal(t, 3, resp.Succeeded)
	assert.Equal(t, 0, resp.Failed)
	assert.Len(t, resp.Results, 3)

	for _, result := range resp.Results {
		assert.True(t, result.Found)
		assert.Empty(t, result.ErrorMessage)
		assert.NotNil(t, result.Data)
	}
}

func TestHandleStreamFetchObjectsPartialFailure(t *testing.T) {
	mockService := &mockObjectStorageService{}
	logger := &testLogger{}

	handler, err := NewHandler(mockService, nil, logger)
	require.NoError(t, err)

	// Register a mock object store where some objects exist and some don't
	objects := map[string][]byte{
		"obj-1": []byte("data-1"),
		"obj-3": []byte("data-3"),
	}
	mockStore := &mockObjectStoreReader{
		readObjectFunc: func(objectRef string) ([]byte, error) {
			if data, ok := objects[objectRef]; ok {
				return data, nil
			}
			return nil, errors.New("object not found")
		},
	}
	handler.RegisterObjectStore(1, mockStore)

	req := &StreamFetchObjectsRequest{
		ShardID: 1,
		Items: []*ObjectFetchItem{
			{ObjectRef: "obj-1"},
			{ObjectRef: "obj-2"}, // Does not exist
			{ObjectRef: "obj-3"},
		},
	}
	reqData, err := EncodePayload(req)
	require.NoError(t, err)

	respData, err := handler.handleStreamFetchObjects(context.Background(), reqData)
	require.NoError(t, err)

	var resp StreamFetchObjectsResponse
	err = DecodePayload(respData, &resp)
	require.NoError(t, err)

	assert.Equal(t, 2, resp.Succeeded)
	assert.Equal(t, 1, resp.Failed)
	assert.Len(t, resp.Results, 3)

	// Check individual results
	assert.True(t, resp.Results[0].Found)
	assert.False(t, resp.Results[1].Found)
	assert.True(t, resp.Results[2].Found)
}

func TestHandleStreamFetchObjectsShardNotFound(t *testing.T) {
	mockService := &mockObjectStorageService{}
	logger := &testLogger{}

	handler, err := NewHandler(mockService, nil, logger)
	require.NoError(t, err)

	// Do not register any object store for shard 99
	req := &StreamFetchObjectsRequest{
		ShardID: 99,
		Items: []*ObjectFetchItem{
			{ObjectRef: "obj-1"},
		},
	}
	reqData, err := EncodePayload(req)
	require.NoError(t, err)

	_, err = handler.handleStreamFetchObjects(context.Background(), reqData)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "shard 99 not found")
}

func TestHandleStreamFetchObjectsEmptyItems(t *testing.T) {
	mockService := &mockObjectStorageService{}
	logger := &testLogger{}

	handler, err := NewHandler(mockService, nil, logger)
	require.NoError(t, err)

	req := &StreamFetchObjectsRequest{
		ShardID: 1,
		Items:   []*ObjectFetchItem{}, // Empty items list
	}
	reqData, err := EncodePayload(req)
	require.NoError(t, err)

	_, err = handler.handleStreamFetchObjects(context.Background(), reqData)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "items list is required")
}

func TestHandleStreamFetchObjectsWithChecksums(t *testing.T) {
	mockService := &mockObjectStorageService{}
	logger := &testLogger{}

	handler, err := NewHandler(mockService, nil, logger)
	require.NoError(t, err)

	testData := []byte("test-data")
	correctChecksum := sha256.Sum256(testData)
	wrongChecksum := sha256.Sum256([]byte("wrong"))

	mockStore := &mockObjectStoreReader{
		readObjectFunc: func(objectRef string) ([]byte, error) {
			return testData, nil
		},
	}
	handler.RegisterObjectStore(1, mockStore)

	req := &StreamFetchObjectsRequest{
		ShardID: 1,
		Items: []*ObjectFetchItem{
			{ObjectRef: "obj-1", Checksum: correctChecksum[:]},
			{ObjectRef: "obj-2", Checksum: wrongChecksum[:]}, // Wrong checksum
		},
	}
	reqData, err := EncodePayload(req)
	require.NoError(t, err)

	respData, err := handler.handleStreamFetchObjects(context.Background(), reqData)
	require.NoError(t, err)

	var resp StreamFetchObjectsResponse
	err = DecodePayload(respData, &resp)
	require.NoError(t, err)

	assert.Equal(t, 1, resp.Succeeded)
	assert.Equal(t, 1, resp.Failed)

	// First item should succeed
	assert.True(t, resp.Results[0].Found)
	assert.Empty(t, resp.Results[0].ErrorMessage)
	assert.Equal(t, testData, resp.Results[0].Data)

	// Second item should fail checksum verification
	assert.True(t, resp.Results[1].Found)
	assert.Equal(t, "checksum mismatch", resp.Results[1].ErrorMessage)
}

func TestRegisterUnregisterObjectStore(t *testing.T) {
	mockService := &mockObjectStorageService{}
	logger := &testLogger{}

	handler, err := NewHandler(mockService, nil, logger)
	require.NoError(t, err)

	mockStore := &mockObjectStoreReader{}

	// Register object store
	handler.RegisterObjectStore(1, mockStore)

	// Verify it's registered
	store, ok := handler.objectStores.get(1)
	assert.True(t, ok)
	assert.Equal(t, mockStore, store)

	// Unregister object store
	handler.UnregisterObjectStore(1)

	// Verify it's unregistered
	_, ok = handler.objectStores.get(1)
	assert.False(t, ok)
}

func TestIsObjectTransferOperation(t *testing.T) {
	tests := []struct {
		name     string
		opcode   byte
		expected bool
	}{
		{"FetchObject", OpFetchObject, true},
		{"StreamFetchObjects", OpStreamFetchObjects, true},
		{"PutObject", OpPutObject, false},
		{"GetObject", OpGetObject, false},
		{"AdminCreateBackend", OpAdminCreateBackend, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsObjectTransferOperation(tt.opcode)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOpcodeToStringObjectTransfer(t *testing.T) {
	assert.Equal(t, "FetchObject", OpcodeToString(OpFetchObject))
	assert.Equal(t, "StreamFetchObjects", OpcodeToString(OpStreamFetchObjects))
}

func TestFetchSingleObjectNilItem(t *testing.T) {
	mockService := &mockObjectStorageService{}
	logger := &testLogger{}

	handler, err := NewHandler(mockService, nil, logger)
	require.NoError(t, err)

	mockStore := &mockObjectStoreReader{}

	result := handler.fetchSingleObject(mockStore, nil)
	assert.False(t, result.Found)
	assert.Equal(t, "invalid fetch item", result.ErrorMessage)
}

func TestFetchSingleObjectEmptyRef(t *testing.T) {
	mockService := &mockObjectStorageService{}
	logger := &testLogger{}

	handler, err := NewHandler(mockService, nil, logger)
	require.NoError(t, err)

	mockStore := &mockObjectStoreReader{}

	result := handler.fetchSingleObject(mockStore, &ObjectFetchItem{ObjectRef: ""})
	assert.False(t, result.Found)
	assert.Equal(t, "invalid fetch item", result.ErrorMessage)
}

func TestHandleFetchObjectViaDispatch(t *testing.T) {
	mockService := &mockObjectStorageService{}
	logger := &testLogger{}

	handler, err := NewHandler(mockService, nil, logger)
	require.NoError(t, err)

	testData := []byte("test-data")
	mockStore := &mockObjectStoreReader{
		readObjectFunc: func(objectRef string) ([]byte, error) {
			return testData, nil
		},
	}
	handler.RegisterObjectStore(1, mockStore)

	req := &FetchObjectRequest{
		ShardID:   1,
		ObjectRef: "test-ref",
	}
	reqData, err := EncodePayload(req)
	require.NoError(t, err)

	// Use the main Handle method to ensure dispatch works
	respData, err := handler.Handle(context.Background(), OpFetchObject, reqData)
	require.NoError(t, err)

	var resp FetchObjectResponse
	err = DecodePayload(respData, &resp)
	require.NoError(t, err)

	assert.True(t, resp.Found)
	assert.Equal(t, testData, resp.Data)
}

func TestHandleStreamFetchObjectsViaDispatch(t *testing.T) {
	mockService := &mockObjectStorageService{}
	logger := &testLogger{}

	handler, err := NewHandler(mockService, nil, logger)
	require.NoError(t, err)

	testData := []byte("test-data")
	mockStore := &mockObjectStoreReader{
		readObjectFunc: func(objectRef string) ([]byte, error) {
			return testData, nil
		},
	}
	handler.RegisterObjectStore(1, mockStore)

	req := &StreamFetchObjectsRequest{
		ShardID: 1,
		Items: []*ObjectFetchItem{
			{ObjectRef: "test-ref"},
		},
	}
	reqData, err := EncodePayload(req)
	require.NoError(t, err)

	// Use the main Handle method to ensure dispatch works
	respData, err := handler.Handle(context.Background(), OpStreamFetchObjects, reqData)
	require.NoError(t, err)

	var resp StreamFetchObjectsResponse
	err = DecodePayload(respData, &resp)
	require.NoError(t, err)

	assert.Equal(t, 1, resp.Succeeded)
	assert.Equal(t, 0, resp.Failed)
}

func TestObjectStoreRegistry(t *testing.T) {
	registry := newObjectStoreRegistry()

	// Test register and get
	mockStore := &mockObjectStoreReader{}
	registry.register(1, mockStore)

	store, ok := registry.get(1)
	assert.True(t, ok)
	assert.Equal(t, mockStore, store)

	// Test get non-existent
	_, ok = registry.get(999)
	assert.False(t, ok)

	// Test unregister
	registry.unregister(1)
	_, ok = registry.get(1)
	assert.False(t, ok)
}

func TestHandleFetchObjectContextCancellation(t *testing.T) {
	mockService := &mockObjectStorageService{}
	logger := &testLogger{}

	handler, err := NewHandler(mockService, nil, logger)
	require.NoError(t, err)

	testData := []byte("test-data")
	mockStore := &mockObjectStoreReader{
		readObjectFunc: func(objectRef string) ([]byte, error) {
			return testData, nil
		},
	}
	handler.RegisterObjectStore(1, mockStore)

	// Create a canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := &StreamFetchObjectsRequest{
		ShardID: 1,
		Items: []*ObjectFetchItem{
			{ObjectRef: "obj-1"},
			{ObjectRef: "obj-2"},
		},
	}
	reqData, err := EncodePayload(req)
	require.NoError(t, err)

	_, err = handler.handleStreamFetchObjects(ctx, reqData)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}
