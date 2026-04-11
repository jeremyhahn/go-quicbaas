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

package qrdb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qbStorage "github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// mockStorageError is a typed error for mock storage failures.
type mockStorageError struct {
	Op string
}

func (e *mockStorageError) Error() string {
	return "mock storage error: " + e.Op
}

// mockBatch records calls and returns configurable errors.
type mockBatch struct {
	setKeys     [][]byte
	setValues   [][]byte
	deleteKeys  [][]byte
	committed   bool
	rolledBack  bool
	closed      bool
	setErr      error
	deleteErr   error
	commitErr   error
	rollbackErr error
	closeErr    error
}

func (b *mockBatch) Set(key, value []byte) error {
	if b.setErr != nil {
		return b.setErr
	}
	b.setKeys = append(b.setKeys, key)
	b.setValues = append(b.setValues, value)
	return nil
}

func (b *mockBatch) Delete(key []byte) error {
	if b.deleteErr != nil {
		return b.deleteErr
	}
	b.deleteKeys = append(b.deleteKeys, key)
	return nil
}

func (b *mockBatch) Commit() error {
	if b.commitErr != nil {
		return b.commitErr
	}
	b.committed = true
	return nil
}

func (b *mockBatch) Rollback() error {
	if b.rollbackErr != nil {
		return b.rollbackErr
	}
	b.rolledBack = true
	return nil
}

func (b *mockBatch) Close() error {
	if b.closeErr != nil {
		return b.closeErr
	}
	b.closed = true
	return nil
}

// mockIterator is a minimal iterator mock for testing adapter delegation.
type mockIterator struct {
	valid   bool
	key     []byte
	value   []byte
	seeked  bool
	errVal  error
	closedV bool
}

func (m *mockIterator) SeekToFirst() bool { m.seeked = true; return m.valid }
func (m *mockIterator) SeekToLast() bool  { return m.valid }
func (m *mockIterator) Seek([]byte) bool  { return m.valid }
func (m *mockIterator) Next() bool        { return false }
func (m *mockIterator) Prev() bool        { return false }
func (m *mockIterator) Valid() bool       { return m.valid }
func (m *mockIterator) Key() []byte       { return m.key }
func (m *mockIterator) Value() []byte     { return m.value }
func (m *mockIterator) Error() error      { return m.errVal }
func (m *mockIterator) Close() error      { m.closedV = true; return nil }

// mockSnapshot records calls and returns configurable results.
type mockSnapshot struct {
	getKey   []byte
	getValue []byte
	getErr   error
	iterOpts *qbStorage.IteratorOptions
	iterVal  qbStorage.Iterator
	closedV  bool
	closeErr error
}

func (s *mockSnapshot) Get(key []byte) ([]byte, error) {
	s.getKey = key
	return s.getValue, s.getErr
}

func (s *mockSnapshot) NewIterator(opts *qbStorage.IteratorOptions) qbStorage.Iterator {
	s.iterOpts = opts
	if s.iterVal != nil {
		return s.iterVal
	}
	return &mockIterator{}
}

func (s *mockSnapshot) Close() error {
	s.closedV = true
	return s.closeErr
}

// mockStorage implements quicbaas storage.Storage for testing the adapter.
type mockStorage struct {
	data      map[string][]byte
	getErr    error
	setErr    error
	deleteErr error
	batchVal  *mockBatch
	iterOpts  *qbStorage.IteratorOptions
	iterVal   qbStorage.Iterator
	snapVal   *mockSnapshot
	snapErr   error
	closedV   bool
	closeErr  error
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		data:     make(map[string][]byte),
		batchVal: &mockBatch{},
		snapVal:  &mockSnapshot{},
	}
}

func (s *mockStorage) Get(key []byte) ([]byte, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	val, ok := s.data[string(key)]
	if !ok {
		return nil, nil
	}
	return val, nil
}

func (s *mockStorage) Set(key, value []byte) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.data[string(key)] = value
	return nil
}

func (s *mockStorage) Delete(key []byte) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.data, string(key))
	return nil
}

func (s *mockStorage) Batch() qbStorage.Batch {
	return s.batchVal
}

func (s *mockStorage) NewIterator(opts *qbStorage.IteratorOptions) qbStorage.Iterator {
	s.iterOpts = opts
	if s.iterVal != nil {
		return s.iterVal
	}
	return &mockIterator{}
}

func (s *mockStorage) Snapshot() (qbStorage.Snapshot, error) {
	if s.snapErr != nil {
		return nil, s.snapErr
	}
	return s.snapVal, nil
}

func (s *mockStorage) Close() error {
	if s.closeErr != nil {
		return s.closeErr
	}
	s.closedV = true
	return nil
}

func (s *mockStorage) Exists(key []byte) (bool, error) {
	_, ok := s.data[string(key)]
	return ok, nil
}

func (s *mockStorage) List(prefix []byte) ([][]byte, error) {
	var keys [][]byte
	p := string(prefix)
	for k := range s.data {
		if len(p) == 0 || (len(k) >= len(p) && k[:len(p)] == p) {
			keys = append(keys, []byte(k))
		}
	}
	return keys, nil
}

// ---------------------------------------------------------------------------
// TimeSeriesInvalidQueryError tests
// ---------------------------------------------------------------------------

func TestTimeSeriesInvalidQueryError_Error(t *testing.T) {
	err := &TimeSeriesInvalidQueryError{QueryType: "QueryRich"}
	assert.Equal(t, "timeseries: invalid query type for QueryRich: expected *timeseries.QueryRequest", err.Error())
}

func TestTimeSeriesInvalidQueryError_DifferentQueryType(t *testing.T) {
	err := &TimeSeriesInvalidQueryError{QueryType: "SomeOther"}
	assert.Contains(t, err.Error(), "SomeOther")
	assert.Contains(t, err.Error(), "expected *timeseries.QueryRequest")
}

func TestTimeSeriesInvalidQueryError_ImplementsError(t *testing.T) {
	var err error = &TimeSeriesInvalidQueryError{QueryType: "test"}
	assert.NotNil(t, err)
	assert.NotEmpty(t, err.Error())
}

// ---------------------------------------------------------------------------
// StorageAdapter tests
// ---------------------------------------------------------------------------

func TestStorageAdapter_Get_Success(t *testing.T) {
	ms := newMockStorage()
	ms.data["key1"] = []byte("value1")
	adapter := NewStorageAdapter(ms)

	val, err := adapter.Get([]byte("key1"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value1"), val)
}

func TestStorageAdapter_Get_NotFound(t *testing.T) {
	ms := newMockStorage()
	adapter := NewStorageAdapter(ms)

	val, err := adapter.Get([]byte("nonexistent"))
	require.NoError(t, err)
	assert.Nil(t, val)
}

func TestStorageAdapter_Get_Error(t *testing.T) {
	ms := newMockStorage()
	ms.getErr = &mockStorageError{Op: "Get"}
	adapter := NewStorageAdapter(ms)

	_, err := adapter.Get([]byte("key1"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mock storage error: Get")
}

func TestStorageAdapter_Set_Success(t *testing.T) {
	ms := newMockStorage()
	adapter := NewStorageAdapter(ms)

	err := adapter.Set([]byte("key1"), []byte("value1"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value1"), ms.data["key1"])
}

func TestStorageAdapter_Set_Error(t *testing.T) {
	ms := newMockStorage()
	ms.setErr = &mockStorageError{Op: "Set"}
	adapter := NewStorageAdapter(ms)

	err := adapter.Set([]byte("key1"), []byte("value1"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mock storage error: Set")
}

func TestStorageAdapter_Delete_Success(t *testing.T) {
	ms := newMockStorage()
	ms.data["key1"] = []byte("value1")
	adapter := NewStorageAdapter(ms)

	err := adapter.Delete([]byte("key1"))
	require.NoError(t, err)
	_, exists := ms.data["key1"]
	assert.False(t, exists)
}

func TestStorageAdapter_Delete_Error(t *testing.T) {
	ms := newMockStorage()
	ms.deleteErr = &mockStorageError{Op: "Delete"}
	adapter := NewStorageAdapter(ms)

	err := adapter.Delete([]byte("key1"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mock storage error: Delete")
}

func TestStorageAdapter_Exists_Success(t *testing.T) {
	ms := newMockStorage()
	ms.data["key1"] = []byte("value1")
	adapter := NewStorageAdapter(ms)

	exists, err := adapter.Exists([]byte("key1"))
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestStorageAdapter_Exists_NotFound(t *testing.T) {
	ms := newMockStorage()
	adapter := NewStorageAdapter(ms)

	exists, err := adapter.Exists([]byte("missing"))
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestStorageAdapter_List_Success(t *testing.T) {
	ms := newMockStorage()
	ms.data["prefix/key1"] = []byte("v1")
	ms.data["prefix/key2"] = []byte("v2")
	ms.data["other/key3"] = []byte("v3")
	adapter := NewStorageAdapter(ms)

	keys, err := adapter.List([]byte("prefix/"))
	require.NoError(t, err)
	assert.Len(t, keys, 2)
}

func TestStorageAdapter_List_EmptyPrefix(t *testing.T) {
	ms := newMockStorage()
	ms.data["key1"] = []byte("v1")
	ms.data["key2"] = []byte("v2")
	adapter := NewStorageAdapter(ms)

	keys, err := adapter.List([]byte(""))
	require.NoError(t, err)
	assert.Len(t, keys, 2)
}

func TestStorageAdapter_Batch_Delegates(t *testing.T) {
	ms := newMockStorage()
	adapter := NewStorageAdapter(ms)

	batch := adapter.Batch()
	require.NotNil(t, batch)
}

func TestStorageAdapter_NewIterator_NilOpts(t *testing.T) {
	ms := newMockStorage()
	adapter := NewStorageAdapter(ms)

	iter := adapter.NewIterator(nil)
	require.NotNil(t, iter)
}

func TestStorageAdapter_NewIterator_WithOpts(t *testing.T) {
	ms := newMockStorage()
	adapter := NewStorageAdapter(ms)

	opts := &qbStorage.IteratorOptions{
		LowerBound: []byte("a"),
		UpperBound: []byte("z"),
		Reverse:    true,
		OnlyKeys:   true,
	}

	iter := adapter.NewIterator(opts)
	require.NotNil(t, iter)

	// Verify the options were passed through.
	require.NotNil(t, ms.iterOpts)
	assert.Equal(t, []byte("a"), ms.iterOpts.LowerBound)
	assert.Equal(t, []byte("z"), ms.iterOpts.UpperBound)
	assert.True(t, ms.iterOpts.Reverse)
	assert.True(t, ms.iterOpts.OnlyKeys)
}

func TestStorageAdapter_Snapshot_Success(t *testing.T) {
	ms := newMockStorage()
	adapter := NewStorageAdapter(ms)

	snap, err := adapter.Snapshot()
	require.NoError(t, err)
	require.NotNil(t, snap)
}

func TestStorageAdapter_Snapshot_Error(t *testing.T) {
	ms := newMockStorage()
	ms.snapErr = &mockStorageError{Op: "Snapshot"}
	adapter := NewStorageAdapter(ms)

	_, err := adapter.Snapshot()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mock storage error: Snapshot")
}

func TestStorageAdapter_Close_Success(t *testing.T) {
	ms := newMockStorage()
	adapter := NewStorageAdapter(ms)

	err := adapter.Close()
	require.NoError(t, err)
	assert.True(t, ms.closedV)
}

func TestStorageAdapter_Close_Error(t *testing.T) {
	ms := newMockStorage()
	ms.closeErr = &mockStorageError{Op: "Close"}
	adapter := NewStorageAdapter(ms)

	err := adapter.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mock storage error: Close")
}

func TestStorageAdapter_ImplementsInterface(t *testing.T) {
	ms := newMockStorage()
	adapter := NewStorageAdapter(ms)
	var _ qbStorage.Storage = adapter
}

func TestStorageAdapter_SetThenGet(t *testing.T) {
	ms := newMockStorage()
	adapter := NewStorageAdapter(ms)

	err := adapter.Set([]byte("roundtrip"), []byte("data"))
	require.NoError(t, err)

	val, err := adapter.Get([]byte("roundtrip"))
	require.NoError(t, err)
	assert.Equal(t, []byte("data"), val)
}

func TestStorageAdapter_DeleteNonexistent(t *testing.T) {
	ms := newMockStorage()
	adapter := NewStorageAdapter(ms)

	// Deleting a key that does not exist should not error.
	err := adapter.Delete([]byte("ghost"))
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Constructor tests
// ---------------------------------------------------------------------------

func TestNewStorageAdapter_ReturnsAdapter(t *testing.T) {
	ms := newMockStorage()
	adapter := NewStorageAdapter(ms)
	require.NotNil(t, adapter)
}

func TestNewStorageAdapter_CompileTimeAssertion(t *testing.T) {
	// Verify compile-time interface satisfaction.
	var _ qbStorage.Storage = (*StorageAdapter)(nil)
}
