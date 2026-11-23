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

package security

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/jeremyhahn/go-objstore/pkg/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockObjStore is a mock implementation of common.Storage for testing.
type mockObjStore struct {
	getFunc        func(key string) (io.ReadCloser, error)
	putFunc        func(key string, data io.Reader) error
	deleteFunc     func(key string) error
	listFunc       func(prefix string) ([]string, error)
	existsFunc     func(ctx context.Context, key string) (bool, error)
	closeFunc      func() error
	implementClose bool
}

func (m *mockObjStore) Configure(settings map[string]string) error {
	return nil
}

func (m *mockObjStore) Put(key string, data io.Reader) error {
	if m.putFunc != nil {
		return m.putFunc(key, data)
	}
	return nil
}

func (m *mockObjStore) PutWithContext(ctx context.Context, key string, data io.Reader) error {
	return m.Put(key, data)
}

func (m *mockObjStore) PutWithMetadata(ctx context.Context, key string, data io.Reader, metadata *common.Metadata) error {
	return m.Put(key, data)
}

func (m *mockObjStore) Get(key string) (io.ReadCloser, error) {
	if m.getFunc != nil {
		return m.getFunc(key)
	}
	return nil, errors.New("not implemented")
}

func (m *mockObjStore) GetWithContext(ctx context.Context, key string) (io.ReadCloser, error) {
	return m.Get(key)
}

func (m *mockObjStore) GetMetadata(ctx context.Context, key string) (*common.Metadata, error) {
	return nil, nil
}

func (m *mockObjStore) UpdateMetadata(ctx context.Context, key string, metadata *common.Metadata) error {
	return nil
}

func (m *mockObjStore) Delete(key string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(key)
	}
	return nil
}

func (m *mockObjStore) DeleteWithContext(ctx context.Context, key string) error {
	return m.Delete(key)
}

func (m *mockObjStore) Exists(ctx context.Context, key string) (bool, error) {
	if m.existsFunc != nil {
		return m.existsFunc(ctx, key)
	}
	return false, nil
}

func (m *mockObjStore) List(prefix string) ([]string, error) {
	if m.listFunc != nil {
		return m.listFunc(prefix)
	}
	return nil, nil
}

func (m *mockObjStore) ListWithContext(ctx context.Context, prefix string) ([]string, error) {
	return m.List(prefix)
}

func (m *mockObjStore) ListWithOptions(ctx context.Context, opts *common.ListOptions) (*common.ListResult, error) {
	_, err := m.List(opts.Prefix)
	if err != nil {
		return nil, err
	}
	return &common.ListResult{Objects: nil}, err
}

func (m *mockObjStore) Archive(key string, destination common.Archiver) error {
	return nil
}

// LifecycleManager methods
func (m *mockObjStore) AddPolicy(policy common.LifecyclePolicy) error {
	return nil
}

func (m *mockObjStore) RemovePolicy(id string) error {
	return nil
}

func (m *mockObjStore) GetPolicies() ([]common.LifecyclePolicy, error) {
	return nil, nil
}

func (m *mockObjStore) Close() error {
	if m.implementClose && m.closeFunc != nil {
		return m.closeFunc()
	}
	return errors.New("close not implemented")
}

// closableReader wraps an io.Reader to implement io.ReadCloser.
type closableReader struct {
	*bytes.Reader
	closeFunc func() error
}

func (c *closableReader) Close() error {
	if c.closeFunc != nil {
		return c.closeFunc()
	}
	return nil
}

func TestNewObjStoreAdapter(t *testing.T) {
	mock := &mockObjStore{}
	adapter := NewObjStoreAdapter(mock)

	require.NotNil(t, adapter)
	assert.IsType(t, &ObjStoreAdapter{}, adapter)
}

func TestObjStoreAdapter_Get_Success(t *testing.T) {
	ctx := context.Background()
	testData := []byte("test-key-data")
	mock := &mockObjStore{
		getFunc: func(key string) (io.ReadCloser, error) {
			return &closableReader{Reader: bytes.NewReader(testData)}, nil
		},
	}

	adapter := NewObjStoreAdapter(mock)
	data, err := adapter.Get(ctx, "test-key")

	assert.NoError(t, err)
	assert.Equal(t, testData, data)
}

func TestObjStoreAdapter_Get_ObjStoreError(t *testing.T) {
	ctx := context.Background()
	expectedErr := errors.New("objstore get error")
	mock := &mockObjStore{
		getFunc: func(key string) (io.ReadCloser, error) {
			return nil, expectedErr
		},
	}

	adapter := NewObjStoreAdapter(mock)
	data, err := adapter.Get(ctx, "test-key")

	assert.Error(t, err)
	assert.Nil(t, data)
	assert.Equal(t, expectedErr, err)
}

func TestObjStoreAdapter_Get_ReadError(t *testing.T) {
	ctx := context.Background()
	readErr := errors.New("read error")
	mock := &mockObjStore{
		getFunc: func(key string) (io.ReadCloser, error) {
			return &closableReader{
				Reader: bytes.NewReader(nil),
				closeFunc: func() error {
					// Simulate a reader that fails on read
					return nil
				},
			}, nil
		},
	}

	// Create a failing reader
	failingReader := &failingReadCloser{err: readErr}
	mock.getFunc = func(key string) (io.ReadCloser, error) {
		return failingReader, nil
	}

	adapter := NewObjStoreAdapter(mock)
	data, err := adapter.Get(ctx, "test-key")

	assert.Error(t, err)
	assert.Nil(t, data)
	assert.Contains(t, err.Error(), "read data failed")
}

func TestObjStoreAdapter_Put_Success(t *testing.T) {
	ctx := context.Background()
	testValue := []byte("test-value")
	var capturedKey string
	var capturedData []byte

	mock := &mockObjStore{
		putFunc: func(key string, data io.Reader) error {
			capturedKey = key
			capturedData, _ = io.ReadAll(data)
			return nil
		},
	}

	adapter := NewObjStoreAdapter(mock)
	err := adapter.Put(ctx, "test-key", testValue)

	assert.NoError(t, err)
	assert.Equal(t, "test-key", capturedKey)
	assert.Equal(t, testValue, capturedData)
}

func TestObjStoreAdapter_Put_Error(t *testing.T) {
	ctx := context.Background()
	expectedErr := errors.New("put error")
	mock := &mockObjStore{
		putFunc: func(key string, data io.Reader) error {
			return expectedErr
		},
	}

	adapter := NewObjStoreAdapter(mock)
	err := adapter.Put(ctx, "test-key", []byte("value"))

	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
}

func TestObjStoreAdapter_Delete_Success(t *testing.T) {
	ctx := context.Background()
	var deletedKey string
	mock := &mockObjStore{
		deleteFunc: func(key string) error {
			deletedKey = key
			return nil
		},
	}

	adapter := NewObjStoreAdapter(mock)
	err := adapter.Delete(ctx, "test-key")

	assert.NoError(t, err)
	assert.Equal(t, "test-key", deletedKey)
}

func TestObjStoreAdapter_Delete_Error(t *testing.T) {
	ctx := context.Background()
	expectedErr := errors.New("delete error")
	mock := &mockObjStore{
		deleteFunc: func(key string) error {
			return expectedErr
		},
	}

	adapter := NewObjStoreAdapter(mock)
	err := adapter.Delete(ctx, "test-key")

	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
}

func TestObjStoreAdapter_List_Success(t *testing.T) {
	ctx := context.Background()
	expectedKeys := []string{"key1", "key2", "key3"}
	var capturedPrefix string

	mock := &mockObjStore{
		listFunc: func(prefix string) ([]string, error) {
			capturedPrefix = prefix
			return expectedKeys, nil
		},
	}

	adapter := NewObjStoreAdapter(mock)
	keys, err := adapter.List(ctx, "test-prefix")

	assert.NoError(t, err)
	assert.Equal(t, "test-prefix", capturedPrefix)
	assert.Equal(t, expectedKeys, keys)
}

func TestObjStoreAdapter_List_Error(t *testing.T) {
	ctx := context.Background()
	expectedErr := errors.New("list error")
	mock := &mockObjStore{
		listFunc: func(prefix string) ([]string, error) {
			return nil, expectedErr
		},
	}

	adapter := NewObjStoreAdapter(mock)
	keys, err := adapter.List(ctx, "prefix")

	assert.Error(t, err)
	assert.Nil(t, keys)
	assert.Equal(t, expectedErr, err)
}

func TestObjStoreAdapter_List_EmptyResult(t *testing.T) {
	ctx := context.Background()
	mock := &mockObjStore{
		listFunc: func(prefix string) ([]string, error) {
			return []string{}, nil
		},
	}

	adapter := NewObjStoreAdapter(mock)
	keys, err := adapter.List(ctx, "prefix")

	assert.NoError(t, err)
	assert.Empty(t, keys)
}

func TestObjStoreAdapter_Exists_True(t *testing.T) {
	ctx := context.Background()
	mock := &mockObjStore{
		existsFunc: func(ctx context.Context, key string) (bool, error) {
			return true, nil
		},
	}

	adapter := NewObjStoreAdapter(mock)
	exists, err := adapter.Exists(ctx, "test-key")

	assert.NoError(t, err)
	assert.True(t, exists)
}

func TestObjStoreAdapter_Exists_False(t *testing.T) {
	ctx := context.Background()
	mock := &mockObjStore{
		existsFunc: func(ctx context.Context, key string) (bool, error) {
			return false, nil
		},
	}

	adapter := NewObjStoreAdapter(mock)
	exists, err := adapter.Exists(ctx, "test-key")

	assert.NoError(t, err)
	assert.False(t, exists)
}

func TestObjStoreAdapter_Exists_Error(t *testing.T) {
	ctx := context.Background()
	expectedErr := errors.New("exists error")
	mock := &mockObjStore{
		existsFunc: func(ctx context.Context, key string) (bool, error) {
			return false, expectedErr
		},
	}

	adapter := NewObjStoreAdapter(mock)
	exists, err := adapter.Exists(ctx, "test-key")

	assert.Error(t, err)
	assert.False(t, exists)
	assert.Equal(t, expectedErr, err)
}

func TestObjStoreAdapter_Close(t *testing.T) {
	closeErr := errors.New("close error")

	tests := []struct {
		name       string
		storage    common.Storage
		wantErr    error
		wantCalled bool
	}{
		{name: "closer succeeds", wantCalled: true},
		{name: "closer fails", wantErr: closeErr, wantCalled: true},
		{name: "storage without closer", storage: &minimalStorage{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			closeCalled := false
			storage := tt.storage
			if storage == nil {
				storage = &mockObjStore{
					implementClose: true,
					closeFunc: func() error {
						closeCalled = true
						return tt.wantErr
					},
				}
			}

			err := NewObjStoreAdapter(storage).Close()

			assert.Equal(t, tt.wantErr, err)
			assert.Equal(t, tt.wantCalled, closeCalled)
		})
	}
}

// failingReadCloser simulates an io.ReadCloser that fails on Read.
type failingReadCloser struct {
	err error
}

func (f *failingReadCloser) Read(p []byte) (int, error) {
	return 0, f.err
}

func (f *failingReadCloser) Close() error {
	return nil
}

func TestObjStoreAdapter_Integration(t *testing.T) {
	ctx := context.Background()

	// Test a complete workflow: Put -> Exists -> Get -> Delete
	store := make(map[string][]byte)

	mock := &mockObjStore{
		putFunc: func(key string, data io.Reader) error {
			byteData, err := io.ReadAll(data)
			if err != nil {
				return err
			}
			store[key] = byteData
			return nil
		},
		getFunc: func(key string) (io.ReadCloser, error) {
			data, ok := store[key]
			if !ok {
				return nil, errors.New("key not found")
			}
			return &closableReader{Reader: bytes.NewReader(data)}, nil
		},
		existsFunc: func(ctx context.Context, key string) (bool, error) {
			_, exists := store[key]
			return exists, nil
		},
		deleteFunc: func(key string) error {
			delete(store, key)
			return nil
		},
		listFunc: func(prefix string) ([]string, error) {
			var keys []string
			for k := range store {
				if len(prefix) == 0 || bytes.HasPrefix([]byte(k), []byte(prefix)) {
					keys = append(keys, k)
				}
			}
			return keys, nil
		},
	}

	adapter := NewObjStoreAdapter(mock)

	// Put a value
	testKey := "integration/test"
	testValue := []byte("integration-test-value")
	err := adapter.Put(ctx, testKey, testValue)
	require.NoError(t, err)

	// Check it exists
	exists, err := adapter.Exists(ctx, testKey)
	require.NoError(t, err)
	assert.True(t, exists)

	// Get the value
	retrieved, err := adapter.Get(ctx, testKey)
	require.NoError(t, err)
	assert.Equal(t, testValue, retrieved)

	// List with prefix
	keys, err := adapter.List(ctx, "integration/")
	require.NoError(t, err)
	assert.Contains(t, keys, testKey)

	// Delete the value
	err = adapter.Delete(ctx, testKey)
	require.NoError(t, err)

	// Verify it no longer exists
	exists, err = adapter.Exists(ctx, testKey)
	require.NoError(t, err)
	assert.False(t, exists)
}

// minimalStorage implements common.Storage without a Close method.
type minimalStorage struct {
	getFunc    func(key string) (io.ReadCloser, error)
	putFunc    func(key string, data io.Reader) error
	deleteFunc func(key string) error
}

func (m *minimalStorage) Configure(settings map[string]string) error { return nil }
func (m *minimalStorage) Put(key string, data io.Reader) error {
	if m.putFunc != nil {
		return m.putFunc(key, data)
	}
	return nil
}
func (m *minimalStorage) PutWithContext(ctx context.Context, key string, data io.Reader) error {
	return m.Put(key, data)
}
func (m *minimalStorage) PutWithMetadata(ctx context.Context, key string, data io.Reader, metadata *common.Metadata) error {
	return m.Put(key, data)
}
func (m *minimalStorage) Get(key string) (io.ReadCloser, error) {
	if m.getFunc != nil {
		return m.getFunc(key)
	}
	return nil, errors.New("not implemented")
}
func (m *minimalStorage) GetWithContext(ctx context.Context, key string) (io.ReadCloser, error) {
	return m.Get(key)
}
func (m *minimalStorage) GetMetadata(ctx context.Context, key string) (*common.Metadata, error) {
	return nil, nil
}
func (m *minimalStorage) UpdateMetadata(ctx context.Context, key string, metadata *common.Metadata) error {
	return nil
}
func (m *minimalStorage) Delete(key string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(key)
	}
	return nil
}
func (m *minimalStorage) DeleteWithContext(ctx context.Context, key string) error {
	return m.Delete(key)
}
func (m *minimalStorage) Exists(ctx context.Context, key string) (bool, error) { return false, nil }
func (m *minimalStorage) List(prefix string) ([]string, error)                 { return nil, nil }
func (m *minimalStorage) ListWithContext(ctx context.Context, prefix string) ([]string, error) {
	return m.List(prefix)
}
func (m *minimalStorage) ListWithOptions(ctx context.Context, opts *common.ListOptions) (*common.ListResult, error) {
	return &common.ListResult{}, nil
}
func (m *minimalStorage) Archive(key string, destination common.Archiver) error { return nil }
func (m *minimalStorage) AddPolicy(policy common.LifecyclePolicy) error         { return nil }
func (m *minimalStorage) RemovePolicy(id string) error                          { return nil }
func (m *minimalStorage) GetPolicies() ([]common.LifecyclePolicy, error)        { return nil, nil }

// Note: No Close() method

// Additional edge case tests for completeness
func TestObjStoreAdapter_Get_EmptyData(t *testing.T) {
	ctx := context.Background()
	// Test getting empty data
	mock := &mockObjStore{
		getFunc: func(key string) (io.ReadCloser, error) {
			return &closableReader{Reader: bytes.NewReader([]byte{})}, nil
		},
	}

	adapter := NewObjStoreAdapter(mock)
	data, err := adapter.Get(ctx, "empty-key")

	assert.NoError(t, err)
	assert.Empty(t, data)
}

func TestObjStoreAdapter_Put_EmptyValue(t *testing.T) {
	ctx := context.Background()
	// Test putting empty value
	var capturedData []byte
	mock := &mockObjStore{
		putFunc: func(key string, data io.Reader) error {
			capturedData, _ = io.ReadAll(data)
			return nil
		},
	}

	adapter := NewObjStoreAdapter(mock)
	err := adapter.Put(ctx, "empty-key", []byte{})

	assert.NoError(t, err)
	assert.Empty(t, capturedData)
}

func TestObjStoreAdapter_List_WithEmptyPrefix(t *testing.T) {
	ctx := context.Background()
	// Test listing with empty prefix
	expectedKeys := []string{"key1", "key2", "key3"}
	mock := &mockObjStore{
		listFunc: func(prefix string) ([]string, error) {
			assert.Empty(t, prefix)
			return expectedKeys, nil
		},
	}

	adapter := NewObjStoreAdapter(mock)
	keys, err := adapter.List(ctx, "")

	assert.NoError(t, err)
	assert.Equal(t, expectedKeys, keys)
}
