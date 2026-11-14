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

package file

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// -----------------------------------------------------------------------------
// Batch() tests
// -----------------------------------------------------------------------------

func TestBatch_Set_AddsOperation(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	batch := storage.Batch()

	err := batch.Set([]byte("key1"), []byte("value1"))
	require.NoError(t, err)

	err = batch.Set([]byte("key2"), []byte("value2"))
	require.NoError(t, err)

	// Verify operations are pending
	fb := batch.(*fileBatch)
	assert.Equal(t, 2, fb.Len())
}

func TestBatch_Delete_AddsOperation(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Pre-populate storage
	require.NoError(t, storage.Set([]byte("key1"), []byte("value1")))

	batch := storage.Batch()

	err := batch.Delete([]byte("key1"))
	require.NoError(t, err)

	fb := batch.(*fileBatch)
	assert.Equal(t, 1, fb.Len())
}

func TestBatch_Commit_AppliesSetOperations(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	batch := storage.Batch()
	require.NoError(t, batch.Set([]byte("key1"), []byte("value1")))
	require.NoError(t, batch.Set([]byte("key2"), []byte("value2")))
	require.NoError(t, batch.Set([]byte("key3"), []byte("value3")))

	err := batch.Commit()
	require.NoError(t, err)

	// Verify values are stored
	val1, err := storage.Get([]byte("key1"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value1"), val1)

	val2, err := storage.Get([]byte("key2"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value2"), val2)

	val3, err := storage.Get([]byte("key3"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value3"), val3)
}

func TestBatch_Commit_AppliesDeleteOperations(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Pre-populate
	require.NoError(t, storage.Set([]byte("key1"), []byte("value1")))
	require.NoError(t, storage.Set([]byte("key2"), []byte("value2")))

	batch := storage.Batch()
	require.NoError(t, batch.Delete([]byte("key1")))
	require.NoError(t, batch.Set([]byte("key3"), []byte("value3")))

	err := batch.Commit()
	require.NoError(t, err)

	// key1 should be deleted
	_, err = storage.Get([]byte("key1"))
	assert.True(t, errors.IsNotFound(err))

	// key2 should still exist
	val2, err := storage.Get([]byte("key2"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value2"), val2)

	// key3 should exist
	val3, err := storage.Get([]byte("key3"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value3"), val3)
}

func TestBatch_Commit_CannotCommitTwice(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	batch := storage.Batch()
	require.NoError(t, batch.Set([]byte("key1"), []byte("value1")))

	err := batch.Commit()
	require.NoError(t, err)

	// Second commit should fail
	err = batch.Commit()
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
	assert.Contains(t, err.Error(), "already committed")
}

func TestBatch_Rollback_DiscardsOperations(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	batch := storage.Batch()
	require.NoError(t, batch.Set([]byte("key1"), []byte("value1")))
	require.NoError(t, batch.Set([]byte("key2"), []byte("value2")))

	err := batch.Rollback()
	require.NoError(t, err)

	// Values should not exist
	_, err = storage.Get([]byte("key1"))
	assert.True(t, errors.IsNotFound(err))

	_, err = storage.Get([]byte("key2"))
	assert.True(t, errors.IsNotFound(err))
}

func TestBatch_Rollback_CannotRollbackAfterCommit(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	batch := storage.Batch()
	require.NoError(t, batch.Set([]byte("key1"), []byte("value1")))

	err := batch.Commit()
	require.NoError(t, err)

	err = batch.Rollback()
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

func TestBatch_Close_RollsBackIfNotCommitted(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	batch := storage.Batch()
	require.NoError(t, batch.Set([]byte("key1"), []byte("value1")))

	err := batch.Close()
	require.NoError(t, err)

	// Value should not exist
	_, err = storage.Get([]byte("key1"))
	assert.True(t, errors.IsNotFound(err))
}

func TestBatch_Close_NoopIfAlreadyCommitted(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	batch := storage.Batch()
	require.NoError(t, batch.Set([]byte("key1"), []byte("value1")))

	err := batch.Commit()
	require.NoError(t, err)

	err = batch.Close()
	require.NoError(t, err)

	// Value should exist
	val, err := storage.Get([]byte("key1"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value1"), val)
}

func TestBatch_Set_RejectsEmptyKey(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	batch := storage.Batch()

	err := batch.Set([]byte{}, []byte("value"))
	require.Error(t, err)
	assert.True(t, errors.IsInvalidInput(err))
}

func TestBatch_Delete_RejectsEmptyKey(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	batch := storage.Batch()

	err := batch.Delete([]byte{})
	require.Error(t, err)
	assert.True(t, errors.IsInvalidInput(err))
}

func TestBatch_Set_CopiesKeyAndValue(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	batch := storage.Batch()

	key := []byte("key1")
	value := []byte("value1")

	require.NoError(t, batch.Set(key, value))

	// Modify original slices
	key[0] = 'X'
	value[0] = 'Y'

	err := batch.Commit()
	require.NoError(t, err)

	// Value should be original, not modified
	result, err := storage.Get([]byte("key1"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value1"), result)
}

func TestBatch_Set_OnClosedBatch_ReturnsError(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	batch := storage.Batch()
	require.NoError(t, batch.Close())

	err := batch.Set([]byte("key"), []byte("value"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
	assert.Contains(t, err.Error(), "closed")
}

func TestBatch_Delete_OnClosedBatch_ReturnsError(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	batch := storage.Batch()
	require.NoError(t, batch.Close())

	err := batch.Delete([]byte("key"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
	assert.Contains(t, err.Error(), "closed")
}

func TestBatch_Commit_OnClosedBatch_ReturnsError(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	batch := storage.Batch()
	require.NoError(t, batch.Close())

	err := batch.Commit()
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
	assert.Contains(t, err.Error(), "closed")
}

func TestBatch_Rollback_OnClosedBatch_ReturnsError(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	batch := storage.Batch()
	require.NoError(t, batch.Close())

	err := batch.Rollback()
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
	assert.Contains(t, err.Error(), "closed")
}

func TestBatch_Close_IsIdempotent(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	batch := storage.Batch()

	err := batch.Close()
	require.NoError(t, err)

	err = batch.Close()
	require.NoError(t, err)
}

func TestBatch_MixedOperations_CommitsInOrder(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Pre-populate
	require.NoError(t, storage.Set([]byte("existing"), []byte("old")))

	batch := storage.Batch()
	require.NoError(t, batch.Set([]byte("key1"), []byte("first")))
	require.NoError(t, batch.Set([]byte("key1"), []byte("second"))) // Overwrite
	require.NoError(t, batch.Delete([]byte("existing")))
	require.NoError(t, batch.Set([]byte("new"), []byte("value")))

	err := batch.Commit()
	require.NoError(t, err)

	// key1 should have the second value
	val, err := storage.Get([]byte("key1"))
	require.NoError(t, err)
	assert.Equal(t, []byte("second"), val)

	// existing should be deleted
	_, err = storage.Get([]byte("existing"))
	assert.True(t, errors.IsNotFound(err))

	// new should exist
	val, err = storage.Get([]byte("new"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value"), val)
}

func TestBatch_Set_AfterCommit_ReturnsError(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	batch := storage.Batch()
	require.NoError(t, batch.Set([]byte("key1"), []byte("value1")))
	require.NoError(t, batch.Commit())

	err := batch.Set([]byte("key2"), []byte("value2"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
	assert.Contains(t, err.Error(), "committed")
}

func TestBatch_Delete_AfterCommit_ReturnsError(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	batch := storage.Batch()
	require.NoError(t, batch.Set([]byte("key1"), []byte("value1")))
	require.NoError(t, batch.Commit())

	err := batch.Delete([]byte("key1"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
	assert.Contains(t, err.Error(), "committed")
}
