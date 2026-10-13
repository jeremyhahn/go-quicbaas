package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tests in this file inject the cross-process interleaving the
// process-global stripe lock cannot prevent: another process writes the
// pointer between this backend's pointer read and its pointer write
// (go-quicbaas#120). The competing writer lands straight on the shared
// store, because an in-process second backend would block on the very
// stripe lock a second process does not share.
//
// The shared store is inMemKV, whose three guarded verbs are atomic under
// one mutex. pkg/kvstore.Store arbitrates CompareAndSwap but has no
// PutIfAbsent or CompareAndDelete, so it is not a guardedKVStore, and
// emulating those verbs wrapper-side would claim an atomicity it lacks.

const casPath = "app/secret"

// newRacedBackend returns a backend over a guarded store whose first
// pointer write runs race before reaching the shared store.
func newRacedBackend(base *inMemKV) (*KVBackend, *guardedHookedKV) {
	kv := &guardedHookedKV{hookedKV: &hookedKV{base: base}}
	return NewKVBackend(kv), kv
}

// otherProcessPublishes writes version's entry holding payload and a live
// pointer naming it, as a competing Store in another process would, and
// returns the pointer bytes it wrote.
func otherProcessPublishes(t *testing.T, base *inMemKV, version int, payload string) []byte {
	t.Helper()
	ctx := context.Background()
	entry, err := json.Marshal(&kvEntry{Path: casPath, Version: version,
		Data: map[string][]byte{"k": []byte(payload)}})
	require.NoError(t, err)
	require.NoError(t, base.Put(ctx, versionKey(casPath, version), entry))
	return otherProcessWritesPointer(t, base, &kvCurrentEntry{Version: version})
}

// otherProcessWritesPointer writes cur as the pointer and returns its bytes.
func otherProcessWritesPointer(t *testing.T, base *inMemKV, cur *kvCurrentEntry) []byte {
	t.Helper()
	raw, err := json.Marshal(cur)
	require.NoError(t, err)
	require.NoError(t, base.Put(context.Background(), currentKey(casPath), raw))
	return raw
}

// requireUpdateConflict asserts err is the refused pointer UPDATE.
func requireUpdateConflict(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, ErrWriteConflict, "the stale writer must fail closed")
	var conflict *PointerConflictError
	require.True(t, errors.As(err, &conflict), "the refusal must name its cause: %v", err)
	assert.False(t, conflict.FirstPublish, "an update refusal is not a first-publish refusal")
}

// requireNoEntryHolding asserts no version entry of casPath holds payload:
// the losing writer unreserved what it reserved.
func requireNoEntryHolding(t *testing.T, base *inMemKV, payload string, ignoreVersion int) {
	t.Helper()
	require.NoError(t, base.Scan(context.Background(), kvBackendKeyPrefix+casPath+"/v/",
		func(key string, raw []byte) error {
			var entry kvEntry
			require.NoError(t, json.Unmarshal(raw, &entry))
			if entry.Version != ignoreVersion {
				require.NotEqual(t, []byte(payload), entry.Data["k"],
					"the losing writer's reserved entry at %s survived its refusal", key)
			}
			return nil
		}))
}

func TestKVBackend_GuardedConcurrentStoresAcknowledgeOne(t *testing.T) {
	base := newInMemKV()
	b, kv := newRacedBackend(base)
	ctx := context.Background()
	require.NoError(t, b.Store(ctx, casPath, map[string][]byte{"k": []byte("v1")}, nil))

	var winnerPointer []byte
	kv.onWriteKey = "/current"
	kv.onWrite = func() { winnerPointer = otherProcessPublishes(t, base, 9, "winner") }

	err := b.Store(ctx, casPath, map[string][]byte{"k": []byte("loser")}, nil)
	requireUpdateConflict(t, err)

	assert.Equal(t, winnerPointer, mustRawGet(t, base, currentKey(casPath)),
		"the pointer must name the winner's version, untouched by the loser")
	data, _, err := b.Get(ctx, casPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("winner"), data["k"])
	requireNoEntryHolding(t, base, "loser", 0)
}

func TestKVBackend_GuardedStoreLosesToAConcurrentSoftDelete(t *testing.T) {
	base := newInMemKV()
	b, kv := newRacedBackend(base)
	ctx := context.Background()
	require.NoError(t, b.Store(ctx, casPath, map[string][]byte{"k": []byte("v1")}, nil))

	deletedAt := time.Now().UTC()
	var deletePointer []byte
	kv.onWriteKey = "/current"
	kv.onWrite = func() {
		deletePointer = otherProcessWritesPointer(t, base,
			&kvCurrentEntry{Version: 1, Deleted: true, DeletedAt: &deletedAt})
	}

	err := b.Store(ctx, casPath, map[string][]byte{"k": []byte("loser")}, nil)
	requireUpdateConflict(t, err)

	assert.Equal(t, deletePointer, mustRawGet(t, base, currentKey(casPath)),
		"the soft delete's pointer must stand whole, not blended with the Store")
	_, _, err = b.Get(ctx, casPath)
	require.ErrorIs(t, err, ErrSecretDeleted, "the path must stay soft-deleted")
	requireNoEntryHolding(t, base, "loser", 0)
}

func TestKVBackend_GuardedSoftDeleteLosesToAConcurrentStore(t *testing.T) {
	base := newInMemKV()
	b, kv := newRacedBackend(base)
	ctx := context.Background()
	require.NoError(t, b.Store(ctx, casPath, map[string][]byte{"k": []byte("v1")}, nil))

	var winnerPointer []byte
	kv.onWriteKey = "/current"
	kv.onWrite = func() { winnerPointer = otherProcessPublishes(t, base, 2, "winner") }

	requireUpdateConflict(t, b.Delete(ctx, casPath))

	assert.Equal(t, winnerPointer, mustRawGet(t, base, currentKey(casPath)),
		"the Store's pointer must stand whole, not marked deleted by the loser")
	data, _, err := b.Get(ctx, casPath)
	require.NoError(t, err, "the acknowledged Store must stay readable")
	assert.Equal(t, []byte("winner"), data["k"])
}

func TestKVBackend_GuardedRollbackLosesToAConcurrentStore(t *testing.T) {
	base := newInMemKV()
	b, kv := newRacedBackend(base)
	ctx := context.Background()
	require.NoError(t, b.Store(ctx, casPath, map[string][]byte{"k": []byte("restored")}, nil))
	require.NoError(t, b.Store(ctx, casPath, map[string][]byte{"k": []byte("v2")}, nil))

	var winnerPointer []byte
	kv.onWriteKey = "/current"
	kv.onWrite = func() { winnerPointer = otherProcessPublishes(t, base, 9, "winner") }

	requireUpdateConflict(t, b.Rollback(ctx, casPath, 1))

	assert.Equal(t, winnerPointer, mustRawGet(t, base, currentKey(casPath)),
		"the rollback must not overwrite the winner's pointer")
	data, _, err := b.Get(ctx, casPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("winner"), data["k"])
	// Version 1 legitimately holds the rollback's payload; any other entry
	// holding it is the rollback's stranded reservation.
	requireNoEntryHolding(t, base, "restored", 1)
}

func TestKVBackend_GuardedPointerWriteFailureIsNotAConflict(t *testing.T) {
	store := &errKVStore{inMemKV: newInMemKV()}
	b := NewKVBackend(store)
	ctx := context.Background()
	require.NoError(t, b.Store(ctx, casPath, map[string][]byte{"k": []byte("v1")}, nil))

	store.putErrOn = "/current"
	store.putErr = errStoreScan
	err := b.Delete(ctx, casPath)
	require.ErrorIs(t, err, errStoreScan, "a store failure must surface as itself")
	assert.NotErrorIs(t, err, ErrWriteConflict, "a store failure is not a lost race")
}
