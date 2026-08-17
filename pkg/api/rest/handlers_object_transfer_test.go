//go:build rest

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

package rest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// =============================================================================
// Object Transfer Registry Tests
// =============================================================================

func TestObjectTransferRegistry(t *testing.T) {
	// Clean up the registry before and after tests
	transferRegistry.mu.Lock()
	transferRegistry.stores = make(map[uint64]ObjectStoreReader)
	transferRegistry.mu.Unlock()
	defer func() {
		transferRegistry.mu.Lock()
		transferRegistry.stores = make(map[uint64]ObjectStoreReader)
		transferRegistry.mu.Unlock()
	}()

	t.Run("RegisterObjectStore adds store to registry", func(t *testing.T) {
		mockStore := &mockObjectStoreReader{data: map[string][]byte{"ref1": []byte("data1")}}
		RegisterObjectStore(1, mockStore)

		store, ok := getObjectStore(1)
		assert.True(t, ok)
		assert.Equal(t, mockStore, store)
	})

	t.Run("getObjectStore returns false for unregistered shard", func(t *testing.T) {
		store, ok := getObjectStore(999)
		assert.False(t, ok)
		assert.Nil(t, store)
	})

	t.Run("UnregisterObjectStore removes store from registry", func(t *testing.T) {
		mockStore := &mockObjectStoreReader{data: map[string][]byte{"ref2": []byte("data2")}}
		RegisterObjectStore(2, mockStore)

		// Verify it's registered
		store, ok := getObjectStore(2)
		assert.True(t, ok)
		assert.NotNil(t, store)

		// Unregister
		UnregisterObjectStore(2)

		// Verify it's gone
		store, ok = getObjectStore(2)
		assert.False(t, ok)
		assert.Nil(t, store)
	})

	t.Run("RegisterObjectStore overwrites existing store", func(t *testing.T) {
		mockStore1 := &mockObjectStoreReader{data: map[string][]byte{"ref3": []byte("data3")}}
		mockStore2 := &mockObjectStoreReader{data: map[string][]byte{"ref4": []byte("data4")}}

		RegisterObjectStore(3, mockStore1)
		RegisterObjectStore(3, mockStore2)

		store, ok := getObjectStore(3)
		assert.True(t, ok)
		assert.Equal(t, mockStore2, store)
	})
}

// =============================================================================
// Error Type Tests
// =============================================================================

func TestShardNotFoundError(t *testing.T) {
	err := &ShardNotFoundError{ShardID: 123}
	assert.Equal(t, "shard not found on this node", err.Error())
}

func TestStreamingNotSupportedError(t *testing.T) {
	err := &StreamingNotSupportedError{}
	assert.Equal(t, "streaming not supported", err.Error())
}

// =============================================================================
// bytesEqual Tests
// =============================================================================

func TestBytesEqual(t *testing.T) {
	t.Run("equal byte slices return true", func(t *testing.T) {
		a := []byte{1, 2, 3, 4, 5}
		b := []byte{1, 2, 3, 4, 5}
		assert.True(t, bytesEqual(a, b))
	})

	t.Run("different length slices return false", func(t *testing.T) {
		a := []byte{1, 2, 3}
		b := []byte{1, 2, 3, 4}
		assert.False(t, bytesEqual(a, b))
	})

	t.Run("different content slices return false", func(t *testing.T) {
		a := []byte{1, 2, 3, 4, 5}
		b := []byte{1, 2, 3, 4, 6}
		assert.False(t, bytesEqual(a, b))
	})

	t.Run("empty slices are equal", func(t *testing.T) {
		var a, b []byte
		assert.True(t, bytesEqual(a, b))
	})

	t.Run("empty vs non-empty returns false", func(t *testing.T) {
		a := []byte{}
		b := []byte{1}
		assert.False(t, bytesEqual(a, b))
	})
}

// =============================================================================
// HandleFetchObject Tests
// =============================================================================

func TestHandleFetchObject(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()
	server, err := NewServer(mockSvc, DefaultConfig(), log)
	require.NoError(t, err)
	defer server.Close()

	// Clean up registry before tests
	transferRegistry.mu.Lock()
	transferRegistry.stores = make(map[uint64]ObjectStoreReader)
	transferRegistry.mu.Unlock()
	defer func() {
		transferRegistry.mu.Lock()
		transferRegistry.stores = make(map[uint64]ObjectStoreReader)
		transferRegistry.mu.Unlock()
	}()

	t.Run("returns bad request for invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/object-transfer/fetch", bytes.NewBufferString("not json"))
		w := httptest.NewRecorder()

		server.HandleFetchObject(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns bad request for missing object_ref", func(t *testing.T) {
		reqBody := FetchObjectRESTRequest{
			ShardID: 1,
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/object-transfer/fetch", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		server.HandleFetchObject(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns not found for unregistered shard", func(t *testing.T) {
		reqBody := FetchObjectRESTRequest{
			ShardID:   999,
			ObjectRef: "some-ref",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/object-transfer/fetch", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		server.HandleFetchObject(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp FetchObjectRESTResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.False(t, resp.Found)
		assert.Equal(t, "shard not found on this node", resp.ErrorMessage)
	})

	t.Run("returns object data when found", func(t *testing.T) {
		objectData := []byte("test object data")
		mockStore := &mockObjectStoreReader{data: map[string][]byte{"test-ref": objectData}}
		RegisterObjectStore(10, mockStore)

		reqBody := FetchObjectRESTRequest{
			ShardID:   10,
			ObjectRef: "test-ref",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/object-transfer/fetch", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		server.HandleFetchObject(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp FetchObjectRESTResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.True(t, resp.Found)
		assert.Equal(t, "test-ref", resp.ObjectRef)
		assert.Equal(t, int64(len(objectData)), resp.Size)

		// Decode and verify data
		decodedData, err := base64.StdEncoding.DecodeString(resp.Data)
		require.NoError(t, err)
		assert.Equal(t, objectData, decodedData)
	})

	t.Run("returns not found when object read fails", func(t *testing.T) {
		mockStore := &mockObjectStoreReader{
			data: map[string][]byte{},
			err:  errors.New("object not found"),
		}
		RegisterObjectStore(11, mockStore)

		reqBody := FetchObjectRESTRequest{
			ShardID:   11,
			ObjectRef: "missing-ref",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/object-transfer/fetch", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		server.HandleFetchObject(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp FetchObjectRESTResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.False(t, resp.Found)
		assert.Equal(t, "object not found", resp.ErrorMessage)
	})

	t.Run("validates checksum when provided", func(t *testing.T) {
		objectData := []byte("checksummed data")
		hash := sha256.Sum256(objectData)
		checksum := base64.StdEncoding.EncodeToString(hash[:])

		mockStore := &mockObjectStoreReader{data: map[string][]byte{"checksum-ref": objectData}}
		RegisterObjectStore(12, mockStore)

		reqBody := FetchObjectRESTRequest{
			ShardID:   12,
			ObjectRef: "checksum-ref",
			Checksum:  checksum,
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/object-transfer/fetch", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		server.HandleFetchObject(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp FetchObjectRESTResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.True(t, resp.Found)
	})

	t.Run("returns error for checksum mismatch", func(t *testing.T) {
		objectData := []byte("original data")
		wrongChecksum := base64.StdEncoding.EncodeToString([]byte("wrong checksum"))

		mockStore := &mockObjectStoreReader{data: map[string][]byte{"mismatch-ref": objectData}}
		RegisterObjectStore(13, mockStore)

		reqBody := FetchObjectRESTRequest{
			ShardID:   13,
			ObjectRef: "mismatch-ref",
			Checksum:  wrongChecksum,
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/object-transfer/fetch", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		server.HandleFetchObject(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp FetchObjectRESTResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.True(t, resp.Found)
		assert.Equal(t, "checksum mismatch", resp.ErrorMessage)
	})

	t.Run("returns error for invalid checksum encoding", func(t *testing.T) {
		objectData := []byte("some data")
		mockStore := &mockObjectStoreReader{data: map[string][]byte{"bad-checksum-ref": objectData}}
		RegisterObjectStore(14, mockStore)

		reqBody := FetchObjectRESTRequest{
			ShardID:   14,
			ObjectRef: "bad-checksum-ref",
			Checksum:  "not-valid-base64!!@@",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/object-transfer/fetch", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		server.HandleFetchObject(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// =============================================================================
// HandleStreamFetchObjects Tests
// =============================================================================

func TestHandleStreamFetchObjects(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()
	server, err := NewServer(mockSvc, DefaultConfig(), log)
	require.NoError(t, err)
	defer server.Close()

	// Clean up registry before tests
	transferRegistry.mu.Lock()
	transferRegistry.stores = make(map[uint64]ObjectStoreReader)
	transferRegistry.mu.Unlock()
	defer func() {
		transferRegistry.mu.Lock()
		transferRegistry.stores = make(map[uint64]ObjectStoreReader)
		transferRegistry.mu.Unlock()
	}()

	t.Run("returns bad request for invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/object-transfer/stream-fetch", bytes.NewBufferString("invalid"))
		w := httptest.NewRecorder()

		server.HandleStreamFetchObjects(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns bad request for empty items", func(t *testing.T) {
		reqBody := StreamFetchObjectsRESTRequest{
			ShardID: 1,
			Items:   []ObjectFetchItemREST{},
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/object-transfer/stream-fetch", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		server.HandleStreamFetchObjects(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns not found for unregistered shard", func(t *testing.T) {
		reqBody := StreamFetchObjectsRESTRequest{
			ShardID: 999,
			Items: []ObjectFetchItemREST{
				{ObjectRef: "ref1"},
			},
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/object-transfer/stream-fetch", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		server.HandleStreamFetchObjects(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("streams multiple objects as NDJSON", func(t *testing.T) {
		mockStore := &mockObjectStoreReader{
			data: map[string][]byte{
				"obj1": []byte("data1"),
				"obj2": []byte("data2"),
			},
		}
		RegisterObjectStore(20, mockStore)

		reqBody := StreamFetchObjectsRESTRequest{
			ShardID: 20,
			Items: []ObjectFetchItemREST{
				{ObjectRef: "obj1"},
				{ObjectRef: "obj2"},
			},
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/object-transfer/stream-fetch", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		server.HandleStreamFetchObjects(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/x-ndjson", w.Header().Get("Content-Type"))
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		mockStore := &mockObjectStoreReader{
			data: map[string][]byte{
				"obj1": []byte("data1"),
			},
		}
		RegisterObjectStore(21, mockStore)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		reqBody := StreamFetchObjectsRESTRequest{
			ShardID: 21,
			Items: []ObjectFetchItemREST{
				{ObjectRef: "obj1"},
			},
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/object-transfer/stream-fetch", bytes.NewBuffer(body))
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		server.HandleStreamFetchObjects(w, req)

		// Should return successfully but with empty or partial response
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// =============================================================================
// fetchSingleObjectREST Tests
// =============================================================================

func TestFetchSingleObjectREST(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()
	server, err := NewServer(mockSvc, DefaultConfig(), log)
	require.NoError(t, err)
	defer server.Close()

	t.Run("returns object when found", func(t *testing.T) {
		objectData := []byte("found data")
		mockStore := &mockObjectStoreReader{data: map[string][]byte{"found-ref": objectData}}

		item := ObjectFetchItemREST{ObjectRef: "found-ref"}
		resp := server.fetchSingleObjectREST(mockStore, item)

		assert.True(t, resp.Found)
		assert.Equal(t, "found-ref", resp.ObjectRef)
		assert.Equal(t, int64(len(objectData)), resp.Size)
		assert.Empty(t, resp.ErrorMessage)
	})

	t.Run("returns not found when object missing", func(t *testing.T) {
		mockStore := &mockObjectStoreReader{
			data: map[string][]byte{},
			err:  errors.New("not found"),
		}

		item := ObjectFetchItemREST{ObjectRef: "missing-ref"}
		resp := server.fetchSingleObjectREST(mockStore, item)

		assert.False(t, resp.Found)
		assert.Equal(t, "missing-ref", resp.ObjectRef)
		assert.Equal(t, "not found", resp.ErrorMessage)
	})

	t.Run("returns checksum mismatch error", func(t *testing.T) {
		objectData := []byte("data for checksum test")
		mockStore := &mockObjectStoreReader{data: map[string][]byte{"checksum-test": objectData}}

		// Use a wrong checksum
		wrongChecksum := base64.StdEncoding.EncodeToString([]byte("wrong"))

		item := ObjectFetchItemREST{
			ObjectRef: "checksum-test",
			Checksum:  wrongChecksum,
		}
		resp := server.fetchSingleObjectREST(mockStore, item)

		assert.True(t, resp.Found)
		assert.Equal(t, "checksum mismatch", resp.ErrorMessage)
	})

	t.Run("succeeds with correct checksum", func(t *testing.T) {
		objectData := []byte("data with correct checksum")
		hash := sha256.Sum256(objectData)
		correctChecksum := base64.StdEncoding.EncodeToString(hash[:])

		mockStore := &mockObjectStoreReader{data: map[string][]byte{"correct-checksum": objectData}}

		item := ObjectFetchItemREST{
			ObjectRef: "correct-checksum",
			Checksum:  correctChecksum,
		}
		resp := server.fetchSingleObjectREST(mockStore, item)

		assert.True(t, resp.Found)
		assert.Empty(t, resp.ErrorMessage)
	})

	t.Run("ignores invalid checksum encoding gracefully", func(t *testing.T) {
		objectData := []byte("data with bad checksum format")
		mockStore := &mockObjectStoreReader{data: map[string][]byte{"bad-format": objectData}}

		item := ObjectFetchItemREST{
			ObjectRef: "bad-format",
			Checksum:  "not-valid-base64!!@@",
		}
		resp := server.fetchSingleObjectREST(mockStore, item)

		// Should still return the data since decode fails
		assert.True(t, resp.Found)
		assert.Empty(t, resp.ErrorMessage)
	})
}

// =============================================================================
// Route Registration Tests
// =============================================================================

func TestRegisterObjectTransferRoutes(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()
	server, err := NewServer(mockSvc, DefaultConfig(), log)
	require.NoError(t, err)
	defer server.Close()

	router := chi.NewRouter()
	// mountObjectTransferTestRoutes mirrors the /object-transfer surface
	// routes.go (Routes(), GroupAPI) registers in production; the standalone
	// registerObjectTransferRoutes chi helper was removed once the
	// table-driven registration became the single source of truth.
	router.Route("/object-transfer", func(r chi.Router) {
		r.Post("/fetch", server.HandleFetchObject)
		r.Post("/stream-fetch", server.HandleStreamFetchObjects)
	})

	t.Run("fetch route is registered", func(t *testing.T) {
		reqBody := FetchObjectRESTRequest{ObjectRef: "test"}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/object-transfer/fetch", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Should get a response (either success or error, but not 404)
		// Since no shard is registered, we expect 200 with found=false
		assert.NotEqual(t, http.StatusNotFound, w.Code)
	})

	t.Run("stream-fetch route is registered", func(t *testing.T) {
		reqBody := StreamFetchObjectsRESTRequest{
			ShardID: 1,
			Items:   []ObjectFetchItemREST{},
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/object-transfer/stream-fetch", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Should get bad request since items is empty, but route exists
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// =============================================================================
// Mock Object Store Reader
// =============================================================================

type mockObjectStoreReader struct {
	data map[string][]byte
	err  error
}

func (m *mockObjectStoreReader) ReadObject(objectRef string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	data, ok := m.data[objectRef]
	if !ok {
		return nil, errors.New("object not found")
	}
	return data, nil
}
