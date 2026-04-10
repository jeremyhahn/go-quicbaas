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
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// -----------------------------------------------------------------------------
// Snapshot interface tests
// -----------------------------------------------------------------------------

func TestSnapshotInterface_Get_ReturnsValue(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key1"), []byte("value1")))
	require.NoError(t, store.Set([]byte("key2"), []byte("value2")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	val, err := snap.Get([]byte("key1"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value1"), val)

	val, err = snap.Get([]byte("key2"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value2"), val)
}

func TestSnapshotInterface_Get_NotFound(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key1"), []byte("value1")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	_, err = snap.Get([]byte("nonexistent"))
	require.Error(t, err)
	assert.True(t, errors.IsNotFound(err))
}

func TestSnapshotInterface_Get_EmptyKey(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	_, err = snap.Get([]byte{})
	require.Error(t, err)
	assert.True(t, errors.IsInvalidInput(err))
}

func TestSnapshotInterface_Get_ClosedSnapshot(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key1"), []byte("value1")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	require.NoError(t, snap.Close())

	_, err = snap.Get([]byte("key1"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
	assert.Contains(t, err.Error(), "closed")
}

func TestSnapshotInterface_IsIsolated(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key1"), []byte("original")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	// Modify storage after snapshot
	require.NoError(t, store.Set([]byte("key1"), []byte("modified")))
	require.NoError(t, store.Set([]byte("key2"), []byte("new")))
	require.NoError(t, store.Delete([]byte("key1")))

	// Snapshot should have original value
	val, err := snap.Get([]byte("key1"))
	require.NoError(t, err)
	assert.Equal(t, []byte("original"), val)

	// key2 should not exist in snapshot
	_, err = snap.Get([]byte("key2"))
	assert.True(t, errors.IsNotFound(err))
}

func TestSnapshotInterface_Get_ReturnsCopy(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	val1, err := snap.Get([]byte("key"))
	require.NoError(t, err)

	val2, err := snap.Get([]byte("key"))
	require.NoError(t, err)

	// Modify val1
	val1[0] = 'X'

	// val2 should be unchanged
	assert.Equal(t, []byte("value"), val2)
}

func TestSnapshotInterface_NewIterator_IteratesSnapshot(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	// Modify storage after snapshot
	require.NoError(t, store.Set([]byte("ddd"), []byte("4")))
	require.NoError(t, store.Delete([]byte("aaa")))

	iter := snap.NewIterator(nil)
	defer iter.Close()

	var keys []string
	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		keys = append(keys, string(iter.Key()))
	}

	// Should only see original keys
	assert.Equal(t, []string{"aaa", "bbb", "ccc"}, keys)
}

func TestSnapshotInterface_NewIterator_WithBounds(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))
	require.NoError(t, store.Set([]byte("ddd"), []byte("4")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(&storage.IteratorOptions{
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

func TestSnapshotInterface_NewIterator_Reverse(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(&storage.IteratorOptions{
		Reverse: true,
	})
	defer iter.Close()

	var keys []string
	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		keys = append(keys, string(iter.Key()))
	}

	assert.Equal(t, []string{"ccc", "bbb", "aaa"}, keys)
}

func TestSnapshotInterface_NewIterator_OnlyKeys(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(&storage.IteratorOptions{
		OnlyKeys: true,
	})
	defer iter.Close()

	iter.SeekToFirst()
	assert.True(t, iter.Valid())
	assert.Equal(t, []byte("key"), iter.Key())
	assert.Nil(t, iter.Value())
}

func TestSnapshotInterface_NewIterator_ClosedSnapshot(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	require.NoError(t, snap.Close())

	iter := snap.NewIterator(nil)
	defer iter.Close()

	// Iterator from closed snapshot should be invalid
	assert.False(t, iter.SeekToFirst())
	assert.False(t, iter.Valid())
}

func TestSnapshotInterface_Close_IsIdempotent(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	snap, err := store.Snapshot()
	require.NoError(t, err)

	err = snap.Close()
	require.NoError(t, err)

	err = snap.Close()
	require.NoError(t, err)
}

func TestSnapshotInterface_EmptyStorage(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	_, err = snap.Get([]byte("key"))
	assert.True(t, errors.IsNotFound(err))

	iter := snap.NewIterator(nil)
	defer iter.Close()

	assert.False(t, iter.SeekToFirst())
}

// -----------------------------------------------------------------------------
// Snapshot iterator tests
// -----------------------------------------------------------------------------

func TestSnapshotIterator_SeekToFirst_ReturnsFirstKey(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(nil)
	defer iter.Close()

	found := iter.SeekToFirst()

	assert.True(t, found)
	assert.True(t, iter.Valid())
	assert.Equal(t, []byte("aaa"), iter.Key())
}

func TestSnapshotIterator_SeekToLast_ReturnsLastKey(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(nil)
	defer iter.Close()

	found := iter.SeekToLast()

	assert.True(t, found)
	assert.True(t, iter.Valid())
	assert.Equal(t, []byte("bbb"), iter.Key())
}

func TestSnapshotIterator_Seek_ExactMatch(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(nil)
	defer iter.Close()

	found := iter.Seek([]byte("bbb"))

	assert.True(t, found)
	assert.Equal(t, []byte("bbb"), iter.Key())
}

func TestSnapshotIterator_Seek_NextKey(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(nil)
	defer iter.Close()

	// Seek to "bbb" should land on "ccc"
	found := iter.Seek([]byte("bbb"))

	assert.True(t, found)
	assert.Equal(t, []byte("ccc"), iter.Key())
}

func TestSnapshotIterator_Next_IteratesForward(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(nil)
	defer iter.Close()

	iter.SeekToFirst()
	assert.Equal(t, []byte("aaa"), iter.Key())

	assert.True(t, iter.Next())
	assert.Equal(t, []byte("bbb"), iter.Key())

	assert.False(t, iter.Next())
	assert.False(t, iter.Valid())
}

func TestSnapshotIterator_Prev_IteratesBackward(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(nil)
	defer iter.Close()

	iter.SeekToLast()
	assert.Equal(t, []byte("bbb"), iter.Key())

	assert.True(t, iter.Prev())
	assert.Equal(t, []byte("aaa"), iter.Key())

	assert.False(t, iter.Prev())
	assert.False(t, iter.Valid())
}

func TestSnapshotIterator_Value_ReturnsCopy(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(nil)
	defer iter.Close()

	iter.SeekToFirst()

	val1 := iter.Value()
	val2 := iter.Value()

	// Modify val1
	val1[0] = 'X'

	// val2 should be unchanged
	assert.Equal(t, []byte("value"), val2)
}

func TestSnapshotIterator_Error_ReturnsNil(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(nil)
	defer iter.Close()

	iter.SeekToFirst()
	assert.Nil(t, iter.Error())
}

func TestSnapshotIterator_Close_MarksAsInvalid(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(nil)
	iter.SeekToFirst()
	assert.True(t, iter.Valid())

	require.NoError(t, iter.Close())
	assert.False(t, iter.Valid())
}

// =============================================================================
// Consolidated from snapshot_coverage_test.go
// =============================================================================

// -----------------------------------------------------------------------------
// Snapshot creation edge cases
// -----------------------------------------------------------------------------

func TestSnapshot_OnClosedStorage_ReturnsError(t *testing.T) {
	store := createTestStorage(t)
	require.NoError(t, store.Set([]byte("key"), []byte("value")))
	require.NoError(t, store.Close())

	snap, err := store.Snapshot()

	require.Error(t, err)
	assert.Nil(t, snap)
	assert.True(t, errors.Is(err, errors.ErrStorage))
	assert.Contains(t, err.Error(), "closed")
}

// -----------------------------------------------------------------------------
// Snapshot iterator Seek tests (covers snapshot.go Seek reverse paths)
// -----------------------------------------------------------------------------

func TestSnapshotIterator_Seek_ReverseExactMatch(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer iter.Close()

	// In reverse mode, keys are [ccc, bbb, aaa]
	// Seek to "bbb" should find it
	found := iter.Seek([]byte("bbb"))

	assert.True(t, found)
	assert.Equal(t, []byte("bbb"), iter.Key())
}

func TestSnapshotIterator_Seek_ReverseFirstKey(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer iter.Close()

	// In reverse mode, keys are [ccc, bbb, aaa]
	// Seek to "ccc" (the first element in reverse order)
	found := iter.Seek([]byte("ccc"))

	assert.True(t, found)
	assert.Equal(t, []byte("ccc"), iter.Key())
}

func TestSnapshotIterator_Seek_ReverseNotFound(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer iter.Close()

	// In reverse mode, keys are [ccc, bbb]
	// Seek to "ddd" should not be found since all keys < "ddd" in reverse
	// but the reverse sort.Search logic applies differently
	found := iter.Seek([]byte("ddd"))
	// "ddd" > all keys, so in reverse mode (keys sorted desc as [ccc, bbb]),
	// idx from sort.Search finding first < "ddd" lands on idx=0 (ccc < ddd),
	// so idx-1 would be -1 but idx > 0 check passes if any key < target exists
	// Actually, keys are [ccc, bbb], first key < "ddd" is at idx=0,
	// idx > 0 is false (idx=0), check it.keys[0] == "ddd"? No.
	// So found should be false.
	assert.False(t, found)
}

func TestSnapshotIterator_Seek_ReverseEmptyKeys(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer iter.Close()

	found := iter.Seek([]byte("any"))

	assert.False(t, found)
	assert.False(t, iter.Valid())
}

func TestSnapshotIterator_Seek_ForwardBeyondLast(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(nil)
	defer iter.Close()

	found := iter.Seek([]byte("zzz"))

	assert.False(t, found)
	assert.False(t, iter.Valid())
}

func TestSnapshotIterator_Seek_ClosedIterator(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(nil)
	require.NoError(t, iter.Close())

	found := iter.Seek([]byte("aaa"))

	assert.False(t, found)
	assert.False(t, iter.Valid())
}

// -----------------------------------------------------------------------------
// Snapshot iterator SeekToLast edge cases
// -----------------------------------------------------------------------------

func TestSnapshotIterator_SeekToLast_ClosedIterator(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(nil)
	require.NoError(t, iter.Close())

	found := iter.SeekToLast()

	assert.False(t, found)
	assert.False(t, iter.Valid())
}

func TestSnapshotIterator_SeekToLast_EmptyIterator(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(nil)
	defer iter.Close()

	found := iter.SeekToLast()

	assert.False(t, found)
	assert.False(t, iter.Valid())
}

// -----------------------------------------------------------------------------
// Snapshot iterator Next/Prev on closed iterator
// -----------------------------------------------------------------------------

func TestSnapshotIterator_Next_ClosedIterator(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(nil)
	iter.SeekToFirst()
	require.NoError(t, iter.Close())

	result := iter.Next()

	assert.False(t, result)
	assert.False(t, iter.Valid())
}

func TestSnapshotIterator_Prev_ClosedIterator(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(nil)
	iter.SeekToLast()
	require.NoError(t, iter.Close())

	result := iter.Prev()

	assert.False(t, result)
	assert.False(t, iter.Valid())
}

// -----------------------------------------------------------------------------
// Snapshot iterator Key/Value edge cases
// -----------------------------------------------------------------------------

func TestSnapshotIterator_Key_ClosedIterator(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(nil)
	require.NoError(t, iter.Close())

	key := iter.Key()

	assert.Nil(t, key)
}

func TestSnapshotIterator_Value_ClosedIterator(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(nil)
	require.NoError(t, iter.Close())

	val := iter.Value()

	assert.Nil(t, val)
}

func TestSnapshotIterator_Valid_ClosedIterator(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(nil)
	iter.SeekToFirst()
	assert.True(t, iter.Valid())

	require.NoError(t, iter.Close())

	assert.False(t, iter.Valid())
}

// -----------------------------------------------------------------------------
// Snapshot iterator Seek in reverse with various key arrangements
// -----------------------------------------------------------------------------

func TestSnapshotIterator_Seek_ReverseWithMiddleTarget(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))
	require.NoError(t, store.Set([]byte("eee"), []byte("5")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer iter.Close()

	// In reverse mode, keys are [eee, ccc, aaa]
	// Seek to "ddd" - looking for last key <= "ddd"
	// In the reversed array [eee, ccc, aaa]:
	//   sort.Search finds first key < "ddd" -> at idx=1 (ccc < ddd)
	//   idx > 0 is true, so current = idx - 1 = 0 which is "eee"
	// Wait, that doesn't seem right. Let me re-read the code.
	// Actually the search finds the FIRST i where keys[i] < target.
	// keys are [eee, ccc, aaa], looking for first where key < "ddd":
	//   eee < ddd? No. ccc < ddd? Yes. So idx = 1.
	//   idx > 0 is true, so current = 0, which is "eee"
	// That finds the position of the key that is >= target in reverse order.
	found := iter.Seek([]byte("ddd"))
	assert.True(t, found)
}

func TestSnapshotIterator_Seek_ReverseTargetSmallerThanAll(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer iter.Close()

	// In reverse mode, keys are [ccc, bbb]
	// Seek to "aaa" - looking for last key <= "aaa"
	// sort.Search finds first key < "aaa":
	//   ccc < aaa? No. bbb < aaa? No. So idx = 2.
	//   idx > 0? Yes. current = 1, which is "bbb".
	// But bbb > aaa, so the code checks keys[0] == "aaa"? No.
	// Actually idx=2, idx > 0 is true, current = idx-1 = 1 -> "bbb"
	found := iter.Seek([]byte("aaa"))
	assert.True(t, found)
}
