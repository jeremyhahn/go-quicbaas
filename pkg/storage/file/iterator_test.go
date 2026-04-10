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

	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// -----------------------------------------------------------------------------
// NewIterator() tests
// -----------------------------------------------------------------------------

func TestIterator_CreatesIterator(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	iter := store.NewIterator(nil)

	require.NotNil(t, iter)
	assert.NoError(t, iter.Close())
}

func TestIterator_SeekToFirst_EmptyStorage(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	iter := store.NewIterator(nil)
	defer iter.Close()

	found := iter.SeekToFirst()

	assert.False(t, found)
	assert.False(t, iter.Valid())
}

func TestIterator_SeekToFirst_ReturnsFirstKey(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))

	iter := store.NewIterator(nil)
	defer iter.Close()

	found := iter.SeekToFirst()

	assert.True(t, found)
	assert.True(t, iter.Valid())
	assert.Equal(t, []byte("aaa"), iter.Key())
	assert.Equal(t, []byte("1"), iter.Value())
}

func TestIterator_SeekToLast_EmptyStorage(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	iter := store.NewIterator(nil)
	defer iter.Close()

	found := iter.SeekToLast()

	assert.False(t, found)
	assert.False(t, iter.Valid())
}

func TestIterator_SeekToLast_ReturnsLastKey(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))

	iter := store.NewIterator(nil)
	defer iter.Close()

	found := iter.SeekToLast()

	assert.True(t, found)
	assert.True(t, iter.Valid())
	assert.Equal(t, []byte("ccc"), iter.Key())
	assert.Equal(t, []byte("3"), iter.Value())
}

func TestIterator_Seek_ExistingKey(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))

	iter := store.NewIterator(nil)
	defer iter.Close()

	found := iter.Seek([]byte("bbb"))

	assert.True(t, found)
	assert.True(t, iter.Valid())
	assert.Equal(t, []byte("bbb"), iter.Key())
}

func TestIterator_Seek_NextKey(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))
	require.NoError(t, store.Set([]byte("eee"), []byte("5")))

	iter := store.NewIterator(nil)
	defer iter.Close()

	// Seek to "bbb" should land on "ccc"
	found := iter.Seek([]byte("bbb"))

	assert.True(t, found)
	assert.True(t, iter.Valid())
	assert.Equal(t, []byte("ccc"), iter.Key())
}

func TestIterator_Seek_BeyondLast(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))

	iter := store.NewIterator(nil)
	defer iter.Close()

	found := iter.Seek([]byte("zzz"))

	assert.False(t, found)
	assert.False(t, iter.Valid())
}

func TestIterator_Next_IteratesForward(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))

	iter := store.NewIterator(nil)
	defer iter.Close()

	iter.SeekToFirst()
	assert.Equal(t, []byte("aaa"), iter.Key())

	assert.True(t, iter.Next())
	assert.Equal(t, []byte("bbb"), iter.Key())

	assert.True(t, iter.Next())
	assert.Equal(t, []byte("ccc"), iter.Key())

	assert.False(t, iter.Next())
	assert.False(t, iter.Valid())
}

func TestIterator_Prev_IteratesBackward(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))

	iter := store.NewIterator(nil)
	defer iter.Close()

	iter.SeekToLast()
	assert.Equal(t, []byte("ccc"), iter.Key())

	assert.True(t, iter.Prev())
	assert.Equal(t, []byte("bbb"), iter.Key())

	assert.True(t, iter.Prev())
	assert.Equal(t, []byte("aaa"), iter.Key())

	assert.False(t, iter.Prev())
	assert.False(t, iter.Valid())
}

func TestIterator_LowerBound_FiltersKeys(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))
	require.NoError(t, store.Set([]byte("ddd"), []byte("4")))

	iter := store.NewIterator(&storage.IteratorOptions{
		LowerBound: []byte("bbb"),
	})
	defer iter.Close()

	var keys []string
	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		keys = append(keys, string(iter.Key()))
	}

	assert.Equal(t, []string{"bbb", "ccc", "ddd"}, keys)
}

func TestIterator_UpperBound_FiltersKeys(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))
	require.NoError(t, store.Set([]byte("ddd"), []byte("4")))

	iter := store.NewIterator(&storage.IteratorOptions{
		UpperBound: []byte("ccc"),
	})
	defer iter.Close()

	var keys []string
	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		keys = append(keys, string(iter.Key()))
	}

	assert.Equal(t, []string{"aaa", "bbb"}, keys)
}

func TestIterator_BothBounds_FiltersKeys(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))
	require.NoError(t, store.Set([]byte("ddd"), []byte("4")))
	require.NoError(t, store.Set([]byte("eee"), []byte("5")))

	iter := store.NewIterator(&storage.IteratorOptions{
		LowerBound: []byte("bbb"),
		UpperBound: []byte("ddd"),
	})
	defer iter.Close()

	var keys []string
	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		keys = append(keys, string(iter.Key()))
	}

	assert.Equal(t, []string{"bbb", "ccc"}, keys)
}

func TestIterator_Reverse_IteratesBackward(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))

	iter := store.NewIterator(&storage.IteratorOptions{
		Reverse: true,
	})
	defer iter.Close()

	var keys []string
	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		keys = append(keys, string(iter.Key()))
	}

	assert.Equal(t, []string{"ccc", "bbb", "aaa"}, keys)
}

func TestIterator_OnlyKeys_ReturnsNilValue(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	iter := store.NewIterator(&storage.IteratorOptions{
		OnlyKeys: true,
	})
	defer iter.Close()

	iter.SeekToFirst()
	assert.True(t, iter.Valid())
	assert.Equal(t, []byte("key"), iter.Key())
	assert.Nil(t, iter.Value())
}

func TestIterator_Key_ReturnsNilWhenInvalid(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	iter := store.NewIterator(nil)
	defer iter.Close()

	// Iterator is invalid before seeking
	assert.Nil(t, iter.Key())
}

func TestIterator_Value_ReturnsNilWhenInvalid(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	iter := store.NewIterator(nil)
	defer iter.Close()

	// Iterator is invalid before seeking
	assert.Nil(t, iter.Value())
}

func TestIterator_Error_ReturnsNilOnSuccess(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	iter := store.NewIterator(nil)
	defer iter.Close()

	iter.SeekToFirst()
	assert.Nil(t, iter.Error())
}

func TestIterator_Close_MarksAsInvalid(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	iter := store.NewIterator(nil)
	iter.SeekToFirst()
	assert.True(t, iter.Valid())

	err := iter.Close()
	require.NoError(t, err)

	assert.False(t, iter.Valid())
}

func TestIterator_SeekToFirst_OnClosedIterator(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	iter := store.NewIterator(nil)
	require.NoError(t, iter.Close())

	found := iter.SeekToFirst()

	assert.False(t, found)
	assert.False(t, iter.Valid())
}

func TestIterator_Next_OnClosedIterator(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	iter := store.NewIterator(nil)
	iter.SeekToFirst()
	require.NoError(t, iter.Close())

	result := iter.Next()

	assert.False(t, result)
	assert.False(t, iter.Valid())
}

func TestIterator_Prev_OnClosedIterator(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	iter := store.NewIterator(nil)
	iter.SeekToLast()
	require.NoError(t, iter.Close())

	result := iter.Prev()

	assert.False(t, result)
	assert.False(t, iter.Valid())
}

func TestIterator_Key_ReturnsCopy(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	iter := store.NewIterator(nil)
	defer iter.Close()

	iter.SeekToFirst()
	key1 := iter.Key()
	key2 := iter.Key()

	// Modify key1
	key1[0] = 'X'

	// key2 should be unchanged
	assert.Equal(t, []byte("key"), key2)
}

func TestIterator_Next_InvalidAfterExhausted(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	iter := store.NewIterator(nil)
	defer iter.Close()

	iter.SeekToFirst()
	iter.Next() // Move past last

	// Should stay invalid
	assert.False(t, iter.Valid())
	assert.False(t, iter.Next())
}

func TestIterator_Prev_InvalidAfterExhausted(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	iter := store.NewIterator(nil)
	defer iter.Close()

	iter.SeekToFirst()
	iter.Prev() // Move before first

	// Should stay invalid
	assert.False(t, iter.Valid())
	assert.False(t, iter.Prev())
}

func TestIterator_WithHierarchicalKeys(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("users/001"), []byte("user1")))
	require.NoError(t, store.Set([]byte("users/002"), []byte("user2")))
	require.NoError(t, store.Set([]byte("orders/001"), []byte("order1")))

	iter := store.NewIterator(&storage.IteratorOptions{
		LowerBound: []byte("users/"),
		UpperBound: []byte("users0"), // "users0" > "users/"
	})
	defer iter.Close()

	var keys []string
	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		keys = append(keys, string(iter.Key()))
	}

	assert.Equal(t, []string{"users/001", "users/002"}, keys)
}

func TestIterator_Error_ReturnsErrorOnStorageClosed(t *testing.T) {
	store := createTestStorage(t)
	require.NoError(t, store.Set([]byte("key"), []byte("value")))
	require.NoError(t, store.Close())

	iter := store.NewIterator(nil)
	defer iter.Close()

	// The error should be captured during iterator creation
	assert.NotNil(t, iter.Error())
}
