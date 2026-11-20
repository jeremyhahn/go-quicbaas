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

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Mock KV Transport for kvParameterManager tests
// =============================================================================

// mockKVTransport implements transport.Transport and transport.KVClient
// for unit testing the kvParameterManager with the qrdb SDK client.
type mockKVTransport struct {
	getFn    func(ctx context.Context, key string) ([]byte, error)
	putFn    func(ctx context.Context, key string, value []byte) error
	deleteFn func(ctx context.Context, key string) error
	listFn   func(ctx context.Context, prefix string) ([]string, error)
	scanFn   func(ctx context.Context, prefix string) (map[string][]byte, error)
}

func (m *mockKVTransport) Connect(_ context.Context) error { return nil }
func (m *mockKVTransport) Close() error                    { return nil }
func (m *mockKVTransport) Healthy(_ context.Context) bool  { return true }
func (m *mockKVTransport) Request(_ context.Context, _ string, _, _ any) error {
	return nil
}

func (m *mockKVTransport) Get(ctx context.Context, key string, _ ...qrdbtransport.ReadOption) ([]byte, error) {
	if m.getFn != nil {
		return m.getFn(ctx, key)
	}
	return nil, &qrdbtransport.KeyNotFoundError{Key: key}
}

func (m *mockKVTransport) Put(ctx context.Context, key string, value []byte, _ ...qrdbtransport.WriteOption) error {
	if m.putFn != nil {
		return m.putFn(ctx, key, value)
	}
	return nil
}

func (m *mockKVTransport) Delete(ctx context.Context, key string, _ ...qrdbtransport.WriteOption) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, key)
	}
	return nil
}

func (m *mockKVTransport) List(ctx context.Context, prefix string, _ ...qrdbtransport.ReadOption) ([]string, error) {
	if m.listFn != nil {
		return m.listFn(ctx, prefix)
	}
	return nil, nil
}

func (m *mockKVTransport) Scan(ctx context.Context, prefix string, _ ...qrdbtransport.ReadOption) (map[string][]byte, error) {
	if m.scanFn != nil {
		return m.scanFn(ctx, prefix)
	}
	return nil, nil
}

func (m *mockKVTransport) Exists(_ context.Context, _ string, _ ...qrdbtransport.ReadOption) (bool, error) {
	return false, nil
}

// mockParamClientProvider implements QRDBClientProvider for testing.
type mockParamClientProvider struct {
	transport   *mockKVTransport
	forShardErr error
}

func (p *mockParamClientProvider) ForShard(_ uint64) (*qrdbsdk.Client, error) {
	if p.forShardErr != nil {
		return nil, p.forShardErr
	}
	client, err := qrdbsdk.NewClient(p.transport)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (p *mockParamClientProvider) Close() error { return nil }

// =============================================================================
// Test Helpers for kvParameterManager
// =============================================================================

func newTestKVParameterManager(t *testing.T, provider QRDBClientProvider) *kvParameterManager {
	t.Helper()
	return newKVParameterManager(provider, 1, createTestLogger(t))
}

func newTestKVParamManagerNoLogger(provider QRDBClientProvider) *kvParameterManager {
	return newKVParameterManager(provider, 1, nil)
}

func newMockProvider(transport *mockKVTransport) *mockParamClientProvider {
	return &mockParamClientProvider{transport: transport}
}

// =============================================================================
// paramKey Tests
// =============================================================================

func TestKVParameterManager_ParamKey(t *testing.T) {
	pm := newTestKVParameterManager(t, newMockProvider(&mockKVTransport{}))
	key := pm.paramKey("/app/config/db-host")
	assert.Equal(t, "param:/app/config/db-host", key)
}

func TestKVParameterManager_ParamKey_EmptyPath(t *testing.T) {
	pm := newTestKVParameterManager(t, newMockProvider(&mockKVTransport{}))
	key := pm.paramKey("")
	assert.Equal(t, "param:", key)
}

// =============================================================================
// Store Tests
// =============================================================================

func TestKVParameterManager_Store_Success(t *testing.T) {
	transport := &mockKVTransport{
		getFn: func(_ context.Context, _ string) ([]byte, error) {
			return nil, &qrdbtransport.KeyNotFoundError{Key: "test"}
		},
		putFn: func(_ context.Context, _ string, _ []byte) error {
			return nil
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))
	ctx := context.Background()

	err := pm.Store(ctx, "/app/db-host", []byte("localhost"), nil, false)
	require.NoError(t, err)
}

func TestKVParameterManager_Store_ClosedManager(t *testing.T) {
	pm := newTestKVParameterManager(t, newMockProvider(&mockKVTransport{}))
	pm.closed.Store(true)

	err := pm.Store(context.Background(), "/app/key", []byte("val"), nil, false)
	require.Error(t, err)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrUnavailable, dErr.Code)
}

func TestKVParameterManager_StoreWithOptions_EmptyPath(t *testing.T) {
	pm := newTestKVParameterManager(t, newMockProvider(&mockKVTransport{}))

	err := pm.StoreWithOptions(context.Background(), "", []byte("val"), nil, false, nil)
	require.Error(t, err)
}

func TestKVParameterManager_StoreWithOptions_AlreadyExists(t *testing.T) {
	transport := &mockKVTransport{
		getFn: func(_ context.Context, _ string) ([]byte, error) {
			return []byte("existing"), nil // exists
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	err := pm.StoreWithOptions(context.Background(), "/app/key", []byte("val"), nil, false, nil)
	require.Error(t, err)
}

func TestKVParameterManager_StoreWithOptions_OverwriteExisting(t *testing.T) {
	transport := &mockKVTransport{
		getFn: func(_ context.Context, _ string) ([]byte, error) {
			return []byte("existing"), nil
		},
		putFn: func(_ context.Context, _ string, _ []byte) error {
			return nil
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	err := pm.StoreWithOptions(context.Background(), "/app/key", []byte("new-val"), nil, true, nil)
	require.NoError(t, err)
}

func TestKVParameterManager_StoreWithOptions_WithMetadata(t *testing.T) {
	transport := &mockKVTransport{
		getFn: func(_ context.Context, _ string) ([]byte, error) {
			return nil, &qrdbtransport.KeyNotFoundError{Key: "test"}
		},
		putFn: func(_ context.Context, _ string, _ []byte) error {
			return nil
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))
	ctx := context.Background()

	meta := &interfaces.ParameterMetadata{
		Type:       "SecureString",
		Tags:       map[string]string{"env": "prod"},
		TTLSeconds: 3600,
	}

	err := pm.StoreWithOptions(ctx, "/app/secret", []byte("secret-val"), meta, false, nil)
	require.NoError(t, err)
}

func TestKVParameterManager_StoreWithOptions_PutFails(t *testing.T) {
	transport := &mockKVTransport{
		getFn: func(_ context.Context, _ string) ([]byte, error) {
			return nil, &qrdbtransport.KeyNotFoundError{Key: "test"}
		},
		putFn: func(_ context.Context, _ string, _ []byte) error {
			return errors.New("raft unavailable")
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	err := pm.StoreWithOptions(context.Background(), "/app/key", []byte("val"), nil, false, nil)
	require.Error(t, err)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInternal, dErr.Code)
}

func TestKVParameterManager_StoreWithOptions_NilLogger(t *testing.T) {
	transport := &mockKVTransport{
		getFn: func(_ context.Context, _ string) ([]byte, error) {
			return nil, &qrdbtransport.KeyNotFoundError{Key: "test"}
		},
		putFn: func(_ context.Context, _ string, _ []byte) error {
			return nil
		},
	}
	pm := newTestKVParamManagerNoLogger(newMockProvider(transport))

	// Should not panic with nil logger
	err := pm.StoreWithOptions(context.Background(), "/app/key", []byte("val"), nil, false, nil)
	require.NoError(t, err)
}

func TestKVParameterManager_StoreWithOptions_ClientProviderError(t *testing.T) {
	provider := &mockParamClientProvider{
		transport:   &mockKVTransport{},
		forShardErr: errors.New("provider unavailable"),
	}
	pm := newTestKVParameterManager(t, provider)

	err := pm.StoreWithOptions(context.Background(), "/app/key", []byte("val"), nil, false, nil)
	require.Error(t, err)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInternal, dErr.Code)
}

// =============================================================================
// Get Tests
// =============================================================================

func TestKVParameterManager_Get_Success(t *testing.T) {
	stored := &storedParameter{
		Value: []byte("test-value"),
		Metadata: &interfaces.ParameterMetadata{
			Type:      "string",
			Tags:      map[string]string{"env": "test"},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	data, err := json.Marshal(stored)
	require.NoError(t, err)

	transport := &mockKVTransport{
		getFn: func(_ context.Context, _ string) ([]byte, error) {
			return data, nil
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	value, meta, err := pm.Get(context.Background(), "/app/key", false)
	require.NoError(t, err)
	assert.Equal(t, []byte("test-value"), value)
	assert.Equal(t, "string", meta.Type)
}

func TestKVParameterManager_Get_ClosedManager(t *testing.T) {
	pm := newTestKVParameterManager(t, newMockProvider(&mockKVTransport{}))
	pm.closed.Store(true)

	_, _, err := pm.Get(context.Background(), "/app/key", false)
	require.Error(t, err)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrUnavailable, dErr.Code)
}

func TestKVParameterManager_Get_NotFound(t *testing.T) {
	transport := &mockKVTransport{
		getFn: func(_ context.Context, key string) ([]byte, error) {
			return nil, &qrdbtransport.KeyNotFoundError{Key: key}
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	_, _, err := pm.Get(context.Background(), "/app/missing", false)
	require.Error(t, err)
}

func TestKVParameterManager_Get_ClientProviderError(t *testing.T) {
	provider := &mockParamClientProvider{
		transport:   &mockKVTransport{},
		forShardErr: errors.New("provider unavailable"),
	}
	pm := newTestKVParameterManager(t, provider)

	_, _, err := pm.Get(context.Background(), "/app/key", false)
	require.Error(t, err)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInternal, dErr.Code)
}

func TestKVParameterManager_Get_InvalidJSON(t *testing.T) {
	transport := &mockKVTransport{
		getFn: func(_ context.Context, _ string) ([]byte, error) {
			return []byte("not-valid-json"), nil
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	_, _, err := pm.Get(context.Background(), "/app/key", false)
	require.Error(t, err)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInternal, dErr.Code)
}

func TestKVParameterManager_Get_Expired(t *testing.T) {
	pastTime := time.Now().Add(-1 * time.Hour)
	stored := &storedParameter{
		Value: []byte("expired-value"),
		Metadata: &interfaces.ParameterMetadata{
			Type:      "string",
			Tags:      map[string]string{},
			ExpiresAt: &pastTime,
			CreatedAt: time.Now().Add(-2 * time.Hour),
			UpdatedAt: time.Now().Add(-2 * time.Hour),
		},
	}
	data, err := json.Marshal(stored)
	require.NoError(t, err)

	transport := &mockKVTransport{
		getFn: func(_ context.Context, _ string) ([]byte, error) {
			return data, nil
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	_, _, err = pm.Get(context.Background(), "/app/expired-key", false)
	require.Error(t, err)
}

// =============================================================================
// Delete Tests
// =============================================================================

func TestKVParameterManager_Delete_Success(t *testing.T) {
	transport := &mockKVTransport{
		getFn: func(_ context.Context, _ string) ([]byte, error) {
			return []byte("exists"), nil
		},
		deleteFn: func(_ context.Context, _ string) error {
			return nil
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	err := pm.Delete(context.Background(), "/app/key")
	require.NoError(t, err)
}

func TestKVParameterManager_Delete_ClosedManager(t *testing.T) {
	pm := newTestKVParameterManager(t, newMockProvider(&mockKVTransport{}))
	pm.closed.Store(true)

	err := pm.Delete(context.Background(), "/app/key")
	require.Error(t, err)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrUnavailable, dErr.Code)
}

func TestKVParameterManager_Delete_NotFound(t *testing.T) {
	transport := &mockKVTransport{
		getFn: func(_ context.Context, key string) ([]byte, error) {
			return nil, &qrdbtransport.KeyNotFoundError{Key: key}
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	err := pm.Delete(context.Background(), "/app/missing")
	require.Error(t, err)
}

func TestKVParameterManager_Delete_DeleteFails(t *testing.T) {
	transport := &mockKVTransport{
		getFn: func(_ context.Context, _ string) ([]byte, error) {
			return []byte("exists"), nil
		},
		deleteFn: func(_ context.Context, _ string) error {
			return errors.New("raft error")
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	err := pm.Delete(context.Background(), "/app/key")
	require.Error(t, err)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInternal, dErr.Code)
}

func TestKVParameterManager_Delete_NilLogger(t *testing.T) {
	transport := &mockKVTransport{
		getFn: func(_ context.Context, _ string) ([]byte, error) {
			return []byte("exists"), nil
		},
		deleteFn: func(_ context.Context, _ string) error {
			return nil
		},
	}
	pm := newTestKVParamManagerNoLogger(newMockProvider(transport))

	err := pm.Delete(context.Background(), "/app/key")
	require.NoError(t, err)
}

func TestKVParameterManager_Delete_ClientProviderError(t *testing.T) {
	provider := &mockParamClientProvider{
		transport:   &mockKVTransport{},
		forShardErr: errors.New("provider unavailable"),
	}
	pm := newTestKVParameterManager(t, provider)

	err := pm.Delete(context.Background(), "/app/key")
	require.Error(t, err)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInternal, dErr.Code)
}

// =============================================================================
// List Tests
// =============================================================================

func TestKVParameterManager_List_ClosedManager(t *testing.T) {
	pm := newTestKVParameterManager(t, newMockProvider(&mockKVTransport{}))
	pm.closed.Store(true)

	_, _, err := pm.List(context.Background(), "", true, 100, "")
	require.Error(t, err)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrUnavailable, dErr.Code)
}

func TestKVParameterManager_List_EmptyResult(t *testing.T) {
	transport := &mockKVTransport{
		scanFn: func(_ context.Context, _ string) (map[string][]byte, error) {
			return nil, errors.New("no results")
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	paths, cursor, err := pm.List(context.Background(), "", true, 100, "")
	require.NoError(t, err)
	assert.Empty(t, paths)
	assert.Empty(t, cursor)
}

func TestKVParameterManager_List_WithResults(t *testing.T) {
	transport := &mockKVTransport{
		scanFn: func(_ context.Context, _ string) (map[string][]byte, error) {
			return map[string][]byte{
				"param:/app/db-host": []byte("val1"),
				"param:/app/db-port": []byte("val2"),
			}, nil
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	paths, _, err := pm.List(context.Background(), "/app/", true, 100, "")
	require.NoError(t, err)
	assert.Len(t, paths, 2)
	assert.Contains(t, paths, "/app/db-host")
	assert.Contains(t, paths, "/app/db-port")
}

func TestKVParameterManager_List_WithLimit(t *testing.T) {
	transport := &mockKVTransport{
		scanFn: func(_ context.Context, _ string) (map[string][]byte, error) {
			return map[string][]byte{
				"param:/a": []byte("v1"),
				"param:/b": []byte("v2"),
				"param:/c": []byte("v3"),
			}, nil
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	paths, cursor, err := pm.List(context.Background(), "/", true, 2, "")
	require.NoError(t, err)
	assert.Len(t, paths, 2)
	assert.NotEmpty(t, cursor)
}

func TestKVParameterManager_List_WithCursor(t *testing.T) {
	transport := &mockKVTransport{
		scanFn: func(_ context.Context, _ string) (map[string][]byte, error) {
			return map[string][]byte{
				"param:/a": []byte("v1"),
				"param:/b": []byte("v2"),
				"param:/c": []byte("v3"),
			}, nil
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	paths, _, err := pm.List(context.Background(), "/", true, 100, "/b")
	require.NoError(t, err)
	// Only /c should be returned (paths after cursor "/b")
	assert.Len(t, paths, 1)
	assert.Equal(t, "/c", paths[0])
}

func TestKVParameterManager_List_NonRecursive(t *testing.T) {
	transport := &mockKVTransport{
		scanFn: func(_ context.Context, _ string) (map[string][]byte, error) {
			return map[string][]byte{
				"param:/app/key1":     []byte("v1"),
				"param:/app/sub/key2": []byte("v2"),
			}, nil
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	paths, _, err := pm.List(context.Background(), "/app/", false, 100, "")
	require.NoError(t, err)
	// Non-recursive: should exclude nested /app/sub/key2
	assert.Len(t, paths, 1)
	assert.Equal(t, "/app/key1", paths[0])
}

// =============================================================================
// GetBatch Tests
// =============================================================================

func TestKVParameterManager_GetBatch_Success(t *testing.T) {
	stored := &storedParameter{
		Value: []byte("val1"),
		Metadata: &interfaces.ParameterMetadata{
			Type:      "string",
			Tags:      map[string]string{},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	data, err := json.Marshal(stored)
	require.NoError(t, err)

	transport := &mockKVTransport{
		getFn: func(_ context.Context, key string) ([]byte, error) {
			if key == "param:/app/key1" {
				return data, nil
			}
			return nil, &qrdbtransport.KeyNotFoundError{Key: key}
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	results, errs, err := pm.GetBatch(context.Background(), []string{"/app/key1", "/app/missing"}, false)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Len(t, errs, 1)
	assert.Contains(t, results, "/app/key1")
	assert.Contains(t, errs, "/app/missing")
}

func TestKVParameterManager_GetBatch_AllNotFound(t *testing.T) {
	transport := &mockKVTransport{
		getFn: func(_ context.Context, key string) ([]byte, error) {
			return nil, &qrdbtransport.KeyNotFoundError{Key: key}
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	results, errs, err := pm.GetBatch(context.Background(), []string{"/a", "/b"}, false)
	require.NoError(t, err)
	assert.Empty(t, results)
	assert.Len(t, errs, 2)
}

// =============================================================================
// StoreBatch Tests
// =============================================================================

func TestKVParameterManager_StoreBatch_Success(t *testing.T) {
	transport := &mockKVTransport{
		getFn: func(_ context.Context, key string) ([]byte, error) {
			return nil, &qrdbtransport.KeyNotFoundError{Key: key}
		},
		putFn: func(_ context.Context, _ string, _ []byte) error {
			return nil
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	entries := []*interfaces.ParameterEntry{
		{Path: "/app/key1", Value: []byte("val1")},
		{Path: "/app/key2", Value: []byte("val2")},
	}

	err := pm.StoreBatch(context.Background(), entries, false, nil)
	require.NoError(t, err)
}

func TestKVParameterManager_StoreBatch_PartialFailure(t *testing.T) {
	callCount := 0
	transport := &mockKVTransport{
		getFn: func(_ context.Context, key string) ([]byte, error) {
			return nil, &qrdbtransport.KeyNotFoundError{Key: key}
		},
		putFn: func(_ context.Context, _ string, _ []byte) error {
			callCount++
			if callCount > 1 {
				return errors.New("raft error")
			}
			return nil
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	entries := []*interfaces.ParameterEntry{
		{Path: "/app/key1", Value: []byte("val1")},
		{Path: "/app/key2", Value: []byte("val2")},
	}

	err := pm.StoreBatch(context.Background(), entries, false, nil)
	require.Error(t, err)
}

// =============================================================================
// DeleteBatch Tests
// =============================================================================

func TestKVParameterManager_DeleteBatch_Success(t *testing.T) {
	transport := &mockKVTransport{
		getFn: func(_ context.Context, _ string) ([]byte, error) {
			return []byte("exists"), nil
		},
		deleteFn: func(_ context.Context, _ string) error {
			return nil
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	results, errs, err := pm.DeleteBatch(context.Background(), []string{"/app/key1", "/app/key2"})
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.True(t, results["/app/key1"])
	assert.True(t, results["/app/key2"])
	assert.Empty(t, errs)
}

func TestKVParameterManager_DeleteBatch_PartialFailure(t *testing.T) {
	transport := &mockKVTransport{
		getFn: func(_ context.Context, key string) ([]byte, error) {
			if key == "param:/app/key1" {
				return []byte("exists"), nil
			}
			return nil, &qrdbtransport.KeyNotFoundError{Key: key}
		},
		deleteFn: func(_ context.Context, _ string) error {
			return nil
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	results, errs, err := pm.DeleteBatch(context.Background(), []string{"/app/key1", "/app/missing"})
	require.NoError(t, err)
	assert.True(t, results["/app/key1"])
	assert.False(t, results["/app/missing"])
	assert.Len(t, errs, 1)
}

// =============================================================================
// SearchByTags Tests
// =============================================================================

func TestKVParameterManager_SearchByTags_ClosedManager(t *testing.T) {
	pm := newTestKVParameterManager(t, newMockProvider(&mockKVTransport{}))
	pm.closed.Store(true)

	_, _, err := pm.SearchByTags(context.Background(), map[string]string{"env": "prod"}, "", 100, "")
	require.Error(t, err)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrUnavailable, dErr.Code)
}

func TestKVParameterManager_SearchByTags_NoMatches(t *testing.T) {
	transport := &mockKVTransport{
		scanFn: func(_ context.Context, _ string) (map[string][]byte, error) {
			return nil, errors.New("no results")
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	paths, _, err := pm.SearchByTags(context.Background(), map[string]string{"env": "prod"}, "", 100, "")
	require.NoError(t, err)
	assert.Empty(t, paths)
}

// =============================================================================
// GetBackendType Tests
// =============================================================================

func TestKVParameterManager_GetBackendType(t *testing.T) {
	pm := newTestKVParameterManager(t, newMockProvider(&mockKVTransport{}))
	assert.Equal(t, "kv", pm.GetBackendType())
}

func TestKVParameterManager_GetBackendType_IsConsistent(t *testing.T) {
	pm := newTestKVParameterManager(t, newMockProvider(&mockKVTransport{}))
	first := pm.GetBackendType()
	second := pm.GetBackendType()
	assert.Equal(t, first, second)
}

// =============================================================================
// Close Tests
// =============================================================================

func TestKVParameterManager_Close_Success(t *testing.T) {
	pm := newTestKVParameterManager(t, newMockProvider(&mockKVTransport{}))
	err := pm.Close()
	require.NoError(t, err)
	assert.True(t, pm.closed.Load())
}

func TestKVParameterManager_Close_Idempotent(t *testing.T) {
	pm := newTestKVParameterManager(t, newMockProvider(&mockKVTransport{}))
	err := pm.Close()
	require.NoError(t, err)

	err = pm.Close()
	require.NoError(t, err)
}

func TestKVParameterManager_Close_NilLogger(t *testing.T) {
	pm := newTestKVParamManagerNoLogger(newMockProvider(&mockKVTransport{}))
	err := pm.Close()
	require.NoError(t, err)
}

// =============================================================================
// ScanKeys Tests
// =============================================================================

func TestKVParameterManager_ScanKeys_ScanError(t *testing.T) {
	transport := &mockKVTransport{
		scanFn: func(_ context.Context, _ string) (map[string][]byte, error) {
			return nil, errors.New("scan failed")
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	keys, err := pm.scanKeys(context.Background(), "param:")
	require.NoError(t, err) // Scan errors return nil, nil
	assert.Nil(t, keys)
}

func TestKVParameterManager_ScanKeys_NilLogger(t *testing.T) {
	transport := &mockKVTransport{
		scanFn: func(_ context.Context, _ string) (map[string][]byte, error) {
			return nil, errors.New("scan failed")
		},
	}
	pm := newTestKVParamManagerNoLogger(newMockProvider(transport))

	keys, err := pm.scanKeys(context.Background(), "param:")
	require.NoError(t, err)
	assert.Nil(t, keys)
}

func TestKVParameterManager_ScanKeys_ClientProviderError(t *testing.T) {
	provider := &mockParamClientProvider{
		transport:   &mockKVTransport{},
		forShardErr: errors.New("provider unavailable"),
	}
	pm := newTestKVParameterManager(t, provider)

	keys, err := pm.scanKeys(context.Background(), "param:")
	require.NoError(t, err) // Client errors in scanKeys return nil, nil
	assert.Nil(t, keys)
}

func TestKVParameterManager_ScanKeys_EmptyResult(t *testing.T) {
	transport := &mockKVTransport{
		scanFn: func(_ context.Context, _ string) (map[string][]byte, error) {
			return map[string][]byte{}, nil
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	keys, err := pm.scanKeys(context.Background(), "param:")
	require.NoError(t, err)
	assert.Empty(t, keys)
}
