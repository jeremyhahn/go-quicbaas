package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pruneBackends is every backend that implements VersionPruner.
func pruneBackends(t *testing.T) map[string]interface {
	ConditionalVersionedBackend
	VersionPruner
} {
	t.Helper()
	return map[string]interface {
		ConditionalVersionedBackend
		VersionPruner
	}{
		"memory":    NewMemoryBackend(),
		"kv":        NewKVBackend(newInMemKV()),
		"encrypted": newGuardedEncryptedBackend(t, newGuardedMockKV(), newTestMockSecretsEncryptor()),
	}
}

// pruneHookKV is a guarded store that runs onCompareAndDelete once, before the
// first compare-and-delete reaches the store: the window between a prune's
// scan and its deletes.
type pruneHookKV struct {
	*inMemKV
	onCompareAndDelete func()
	// onScan runs once, before the next Scan reads anything.
	onScan func()
	// onGet runs once, before the first Get of onGetKey.
	onGetKey string
	onGet    func()
	// onCompareAndSwap runs once, before the first CompareAndSwap of
	// onCompareAndSwapKey.
	onCompareAndSwapKey string
	onCompareAndSwap    func()
}

func (k *pruneHookKV) CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error) {
	if hook := k.onCompareAndSwap; hook != nil && key == k.onCompareAndSwapKey {
		k.onCompareAndSwap, k.onCompareAndSwapKey = nil, ""
		hook()
	}
	return k.inMemKV.CompareAndSwap(ctx, key, expected, next)
}

func (k *pruneHookKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	if hook := k.onScan; hook != nil {
		k.onScan = nil
		hook()
	}
	return k.inMemKV.Scan(ctx, prefix, fn)
}

func (k *pruneHookKV) Get(ctx context.Context, key string) ([]byte, error) {
	if hook := k.onGet; hook != nil && key == k.onGetKey {
		k.onGet, k.onGetKey = nil, ""
		hook()
	}
	return k.inMemKV.Get(ctx, key)
}

func (k *pruneHookKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	if hook := k.onCompareAndDelete; hook != nil {
		k.onCompareAndDelete = nil
		hook()
	}
	return k.inMemKV.CompareAndDelete(ctx, key, expected)
}

// TestKVBackend_PruneSparesARecreatedSecret: node A scans the history to
// prune, node B destroys the secret and recreates version 1 before A's
// deletes land. A's deletes name the records it scanned, so B's new version
// 1 survives and the acknowledged secret stays readable on both nodes.
func TestKVBackend_PruneSparesARecreatedSecret(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &pruneHookKV{inMemKV: newInMemKV()}
	nodeA, nodeB := NewKVBackend(store), NewKVBackend(store)
	nodeA.writeLocks = new([32]sync.Mutex)
	nodeB.writeLocks = new([32]sync.Mutex)
	const path = "nodes/recreated"
	for i := 1; i <= 3; i++ {
		_, err := nodeA.StoreVersion(ctx, path, map[string][]byte{"k": []byte(fmt.Sprintf("old%d", i))}, nil)
		require.NoError(t, err)
	}

	store.onCompareAndDelete = func() {
		require.NoError(t, nodeB.Destroy(ctx, path))
		require.NoError(t, nodeB.Store(ctx, path, map[string][]byte{"k": []byte("new")}, nil))
	}
	removed, err := nodeA.PruneVersions(ctx, path, 1)
	require.NoError(t, err)
	assert.Zero(t, removed, "the scanned records were already gone; nothing else is deleted")

	for name, node := range map[string]*KVBackend{"A": nodeA, "B": nodeB} {
		data, _, err := node.Get(ctx, path)
		require.NoError(t, err, "node %s reads the recreated secret", name)
		assert.Equal(t, "new", string(data["k"]))
	}
}

// TestKVBackend_PruneSparesTheRollbackTargetBehindAPendingReservation: node
// A reserves version 2 and pauses before publishing its pointer; node B
// reserves and publishes version 3 and prunes with keep=2. A's reservation
// is not history (B's version 3 replaced version 1), so the prune keeps
// version 1, the rollback target, and A's publication then fails.
func TestKVBackend_PruneSparesTheRollbackTargetBehindAPendingReservation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &pruneHookKV{inMemKV: newInMemKV()}
	nodeA, nodeB := NewKVBackend(store), NewKVBackend(store)
	nodeA.writeLocks = new([32]sync.Mutex)
	nodeB.writeLocks = new([32]sync.Mutex)
	const path = "nodes/pending"
	require.NoError(t, nodeA.Store(ctx, path, map[string][]byte{"k": []byte("seed")}, nil))

	store.onCompareAndSwapKey = currentKey(path)
	store.onCompareAndSwap = func() {
		v, err := nodeB.StoreVersion(ctx, path, map[string][]byte{"k": []byte("b")}, nil)
		require.NoError(t, err)
		require.Equal(t, 3, v, "B allocates past A's reserved version 2")
		removed, err := nodeB.PruneVersions(ctx, path, 2)
		require.NoError(t, err)
		assert.Zero(t, removed, "A's unpublished version 2 is not retained history")
	}
	_, err := nodeA.StoreVersion(ctx, path, map[string][]byte{"k": []byte("a")}, nil)
	require.ErrorIs(t, err, ErrWriteConflict)

	assert.Equal(t, []int{1, 3}, liveVersions(t, nodeA, path))
	data, _, err := nodeA.GetVersion(ctx, path, 1)
	require.NoError(t, err, "version 1 is still the rollback target")
	assert.Equal(t, "seed", string(data["k"]))
	require.NoError(t, nodeA.Rollback(ctx, path, 1))
	data, _, err = nodeA.Get(ctx, path)
	require.NoError(t, err)
	assert.Equal(t, "seed", string(data["k"]))
}

// TestKVBackend_PruneCountsSparseHistoryByRecord: a writer that loses
// publication removes its reservation, so the history under a pointer at 3
// can be versions 1 and 3 only. Retention counts the records: keep=2 keeps
// both (version 1 is the only rollback target), and keep=1 prunes only it.
func TestKVBackend_PruneCountsSparseHistoryByRecord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &pruneHookKV{inMemKV: newInMemKV()}
	b := NewKVBackend(store)
	const path = "nodes/sparse"
	for i := 1; i <= 3; i++ {
		_, err := b.StoreVersion(ctx, path, map[string][]byte{"k": []byte(fmt.Sprintf("v%d", i))}, nil)
		require.NoError(t, err)
	}
	// The lost reservation's unwind: version 2's record is gone.
	require.NoError(t, store.Delete(ctx, versionKey(path, 2)))

	removed, err := b.PruneVersions(ctx, path, 2)
	require.NoError(t, err)
	assert.Zero(t, removed, "two records exist, so keep=2 deletes nothing")
	data, _, err := b.GetVersion(ctx, path, 1)
	require.NoError(t, err, "version 1 is still the rollback target")
	assert.Equal(t, "v1", string(data["k"]))

	removed, err = b.PruneVersions(ctx, path, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, removed, "keep=1 prunes the one older record")
	_, _, err = b.GetVersion(ctx, path, 1)
	assert.Error(t, err, "version 1 is pruned")
	data, _, err = b.Get(ctx, path)
	require.NoError(t, err)
	assert.Equal(t, "v3", string(data["k"]))
}

// getCountingKV counts the point reads of version records.
type getCountingKV struct {
	*inMemKV
	versionGets atomic.Int64
}

func (s *getCountingKV) Get(ctx context.Context, key string) ([]byte, error) {
	if strings.Contains(key, "/v/") {
		s.versionGets.Add(1)
	}
	return s.inMemKV.Get(ctx, key)
}

// TestKVBackend_PruneKeepsTheCreationTimeAndBoundsListing: a prune keeps the
// secret's first version as a timestamp tombstone, so its creation time
// survives retention, and listing the history reads only the surviving
// records, not one per version the path ever had.
func TestKVBackend_PruneKeepsTheCreationTimeAndBoundsListing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &getCountingKV{inMemKV: newInMemKV()}
	b := NewKVBackend(store)
	const path = "app/rotated"

	require.NoError(t, b.Store(ctx, path, map[string][]byte{"k": []byte("v1")}, nil))
	first, err := b.ListVersions(ctx, path)
	require.NoError(t, err)
	require.Len(t, first, 1)
	created := first[0].CreatedAt

	const writes = 200
	for i := 2; i <= writes; i++ {
		require.NoError(t, b.Store(ctx, path, map[string][]byte{"k": []byte(fmt.Sprintf("v%d", i))}, nil))
		_, err := b.PruneVersions(ctx, path, 1)
		require.NoError(t, err)
	}

	store.versionGets.Store(0)
	versions, err := b.ListVersions(ctx, path)
	require.NoError(t, err)
	assert.LessOrEqual(t, store.versionGets.Load(), int64(2),
		"listing reads the surviving records, not every retired version number")
	require.Len(t, versions, 2, "the creation tombstone and the retained version")
	assert.Equal(t, 1, versions[0].Version)
	assert.True(t, versions[0].Deleted, "version 1 is pruned to a tombstone")
	assert.True(t, created.Equal(versions[0].CreatedAt), "the first write's time survives retention")
	assert.Equal(t, writes, versions[1].Version)
	assert.False(t, versions[1].Deleted)

	_, _, err = b.GetVersion(ctx, path, 1)
	require.Error(t, err, "the tombstone holds no payload")
	data, _, err := b.Get(ctx, path)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("v%d", writes), string(data["k"]))
	err = b.Rollback(ctx, path, 1)
	require.Error(t, err, "a tombstone is no rollback target")

	// A further prune finds nothing to remove: the tombstone never counts.
	removed, err := b.PruneVersions(ctx, path, 1)
	require.NoError(t, err)
	assert.Zero(t, removed)
}

// TestPruneVersions_BoundsTheHistory holds every backend to the KV
// backend's retention: after many writes each followed by a prune, the
// history is the creation tombstone plus the retained versions, not an
// emptied slot for every version ever written.
func TestPruneVersions_BoundsTheHistory(t *testing.T) {
	t.Parallel()
	backends := pruneBackends(t)
	encKV := newGuardedMockKV()
	backends["encrypted"] = newGuardedEncryptedBackend(t, encKV, newTestMockSecretsEncryptor())
	for name, b := range backends {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			const path = "app/rotated"

			require.NoError(t, b.Store(ctx, path, map[string][]byte{"k": []byte("v1")}, nil))
			first, err := b.ListVersions(ctx, path)
			require.NoError(t, err)
			require.Len(t, first, 1)
			created := first[0].CreatedAt

			const writes = 200
			for i := 2; i <= writes; i++ {
				require.NoError(t, b.Store(ctx, path, map[string][]byte{"k": []byte(fmt.Sprintf("v%d", i))}, nil))
				_, err := b.PruneVersions(ctx, path, 1)
				require.NoError(t, err)
			}

			versions, err := b.ListVersions(ctx, path)
			require.NoError(t, err)
			require.Len(t, versions, 2, "the creation tombstone and the retained version")
			assert.Equal(t, 1, versions[0].Version)
			assert.True(t, versions[0].Deleted, "version 1 is pruned to a tombstone")
			assert.True(t, created.Equal(versions[0].CreatedAt), "the first write's time survives retention")
			assert.Equal(t, writes, versions[1].Version)
			assert.False(t, versions[1].Deleted)

			if name == "encrypted" {
				raw, err := encKV.Get(ctx, secretKey(path))
				require.NoError(t, err)
				var record secretVersions
				require.NoError(t, json.Unmarshal(raw, &record))
				assert.LessOrEqual(t, len(record.Versions), 2, "the stored record holds the tombstone and the retained version")
			}

			for _, v := range []int{1, 150} {
				data, _, err := b.GetVersion(ctx, path, v)
				require.Error(t, err, "version %d was pruned", v)
				assert.Nil(t, data)
			}
			data, _, err := b.Get(ctx, path)
			require.NoError(t, err)
			assert.Equal(t, fmt.Sprintf("v%d", writes), string(data["k"]))
			require.Error(t, b.Rollback(ctx, path, 1), "a tombstone is no rollback target")

			removed, err := b.PruneVersions(ctx, path, 1)
			require.NoError(t, err)
			assert.Zero(t, removed, "the tombstone never counts")

			require.NoError(t, b.Store(ctx, path, map[string][]byte{"k": []byte("v201")}, nil))
			assert.Equal(t, []int{writes, writes + 1}, liveVersions(t, b, path))
			_, err = b.PruneVersions(ctx, path, 1)
			require.NoError(t, err)
			assert.Equal(t, []int{writes + 1}, liveVersions(t, b, path))
		})
	}
}

// TestPruneVersions_RefusesAStoreWithoutCAS: a prune on a store that cannot
// arbitrate it could delete or overwrite another instance's acknowledged
// write, so it is refused and the history is untouched.
func TestPruneVersions_RefusesAStoreWithoutCAS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	backends := map[string]interface {
		ConditionalVersionedBackend
		VersionPruner
	}{
		"kv":        NewKVBackend(&plainKV{base: newInMemKV()}),
		"encrypted": createTestEncryptedBackend(t),
	}
	for name, b := range backends {
		t.Run(name, func(t *testing.T) {
			for i := 1; i <= 3; i++ {
				_, err := b.StoreVersion(ctx, "app/s", map[string][]byte{"k": []byte(fmt.Sprintf("v%d", i))}, nil)
				require.NoError(t, err)
			}
			_, err := b.PruneVersions(ctx, "app/s", 1)
			require.ErrorIs(t, err, ErrPruneUnsupported)
			versions, err := b.ListVersions(ctx, "app/s")
			require.NoError(t, err)
			assert.Len(t, versions, 3, "nothing is pruned")
		})
	}
}

// TestEncryptedBackend_PruneLosesToADeleteOnAnotherInstance: another
// instance deletes the secret after this instance read the history to prune
// and before it publishes. The prune's compare-and-swap refuses, so the
// stale history is not saved and the secret stays deleted.
func TestEncryptedBackend_PruneLosesToADeleteOnAnotherInstance(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newGuardedMockKV()
	enc := newTestMockSecretsEncryptor()
	pruner := newGuardedEncryptedBackend(t, store, enc)
	other := newGuardedEncryptedBackend(t, store, enc)
	for i := 1; i <= 3; i++ {
		_, err := pruner.StoreVersion(ctx, "app/s", map[string][]byte{"k": []byte(fmt.Sprintf("v%d", i))}, nil)
		require.NoError(t, err)
	}

	store.onCAS = func() { require.NoError(t, other.Delete(ctx, "app/s")) }
	removed, err := pruner.PruneVersions(ctx, "app/s", 1)
	require.NoError(t, err)
	assert.Zero(t, removed, "the prune lost to the delete")
	_, _, err = pruner.Get(ctx, "app/s")
	require.ErrorIs(t, err, ErrSecretDeleted, "the delete is not reverted")
}

// TestKVBackend_PruneSparesASecretRecreatedBeforeTheScan: node A reads the
// pointer (version 3, so versions 1 and 2 are doomed), then node B destroys
// the secret and recreates version 1 before A's scan. The scan captures B's
// new version 1 under A's stale ceiling; the pointer's changed lifetime makes
// A delete nothing, so B's acknowledged secret keeps its data.
func TestKVBackend_PruneSparesASecretRecreatedBeforeTheScan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &pruneHookKV{inMemKV: newInMemKV()}
	nodeA, nodeB := NewKVBackend(store), NewKVBackend(store)
	nodeA.writeLocks = new([32]sync.Mutex)
	nodeB.writeLocks = new([32]sync.Mutex)
	const path = "nodes/recreated-early"
	for i := 1; i <= 3; i++ {
		_, err := nodeA.StoreVersion(ctx, path, map[string][]byte{"k": []byte(fmt.Sprintf("old%d", i))}, nil)
		require.NoError(t, err)
	}

	store.onScan = func() {
		require.NoError(t, nodeB.Destroy(ctx, path))
		require.NoError(t, nodeB.Store(ctx, path, map[string][]byte{"k": []byte("new")}, nil))
	}
	removed, err := nodeA.PruneVersions(ctx, path, 1)
	require.NoError(t, err)
	assert.Zero(t, removed, "the scan belongs to another lifetime; nothing is deleted")

	for name, node := range map[string]*KVBackend{"A": nodeA, "B": nodeB} {
		data, _, err := node.Get(ctx, path)
		require.NoError(t, err, "node %s reads the recreated secret", name)
		assert.Equal(t, "new", string(data["k"]))
	}
}

// TestEncryptedBackend_StaleWriterCannotRestoreAPrunedVersion: a writer on
// one instance loads the unpruned history and is paused before publishing;
// another instance prunes it. The writer's publication is refused, it
// reloads the pruned history and applies itself to that, so the destroyed
// versions stay destroyed and the write still lands.
func TestEncryptedBackend_StaleWriterCannotRestoreAPrunedVersion(t *testing.T) {
	t.Parallel()
	writers := map[string]func(ctx context.Context, eb *EncryptedBackend) error{
		"store": func(ctx context.Context, eb *EncryptedBackend) error {
			return eb.Store(ctx, "app/s", map[string][]byte{"k": []byte("v4")}, nil)
		},
		"delete": func(ctx context.Context, eb *EncryptedBackend) error {
			return eb.Delete(ctx, "app/s")
		},
		"rollback": func(ctx context.Context, eb *EncryptedBackend) error {
			return eb.Rollback(ctx, "app/s", 3)
		},
	}
	for name, write := range writers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			store := newGuardedMockKV()
			enc := newTestMockSecretsEncryptor()
			writer := newGuardedEncryptedBackend(t, store, enc)
			pruner := newGuardedEncryptedBackend(t, store, enc)
			for i := 1; i <= 3; i++ {
				_, err := writer.StoreVersion(ctx, "app/s", map[string][]byte{"k": []byte(fmt.Sprintf("v%d", i))}, nil)
				require.NoError(t, err)
			}

			store.onCAS = func() {
				removed, err := pruner.PruneVersions(ctx, "app/s", 1)
				require.NoError(t, err)
				require.Equal(t, 2, removed)
			}
			require.NoError(t, write(ctx, writer))

			for v := 1; v <= 2; v++ {
				_, _, err := writer.GetVersion(ctx, "app/s", v)
				require.ErrorIs(t, err, ErrVersionDestroyed, "pruned version %d stays destroyed", v)
			}
			versions, err := writer.ListVersions(ctx, "app/s")
			require.NoError(t, err)
			switch name {
			case "store", "rollback":
				// The creation tombstone, the retained version 3 and the new 4.
				require.Len(t, versions, 3, "the write landed on the pruned history")
				assert.Equal(t, []int{1, 3, 4}, []int{versions[0].Version, versions[1].Version, versions[2].Version})
			case "delete":
				_, _, err := writer.Get(ctx, "app/s")
				require.ErrorIs(t, err, ErrSecretDeleted, "the delete landed on the pruned history")
			}
		})
	}
}

// TestManager_GetSurvivesAWriteThatPrunesTheVersionItRead: with max_versions
// 1, a reader on node A reads the pointer (version 1); before it reads that
// version, node B's Manager writes version 2 and prunes version 1. The read
// follows the moved pointer instead of failing on a secret that existed
// throughout. An explicit read of the pruned version is still not found.
func TestManager_GetSurvivesAWriteThatPrunesTheVersionItRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &pruneHookKV{inMemKV: newInMemKV()}
	backendA, backendB := NewKVBackend(store), NewKVBackend(store)
	backendA.writeLocks = new([32]sync.Mutex)
	backendB.writeLocks = new([32]sync.Mutex)
	managerA, err := NewManager(&ManagerOptions{Backend: backendA, MaxVersions: 1})
	require.NoError(t, err)
	managerB, err := NewManager(&ManagerOptions{Backend: backendB, MaxVersions: 1})
	require.NoError(t, err)
	const path = "nodes/read-during-prune"
	require.NoError(t, managerA.Store(ctx, path, map[string][]byte{"k": []byte("one")}, nil))

	store.onGetKey = versionKey(path, 1)
	store.onGet = func() {
		require.NoError(t, managerB.Store(ctx, path, map[string][]byte{"k": []byte("two")}, nil))
	}
	data, metadata, err := managerA.Get(ctx, path)
	require.NoError(t, err, "the secret existed throughout")
	assert.Equal(t, "two", string(data["k"]))
	assert.Equal(t, "2", metadata["version"])

	_, _, err = backendA.GetVersion(ctx, path, 1)
	require.ErrorIs(t, err, ErrVersionNotFound, "an explicit read of a pruned version is not retried")
}

// liveVersions lists the version numbers of path that still hold data.
func liveVersions(t *testing.T, b Backend, path string) []int {
	t.Helper()
	versions, err := b.ListVersions(context.Background(), path)
	require.NoError(t, err)
	var live []int
	for _, v := range versions {
		if !v.Deleted {
			live = append(live, v.Version)
		}
	}
	return live
}

func TestPruneVersions_KeepsOnlyTheNewest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, b := range pruneBackends(t) {
		t.Run(name, func(t *testing.T) {
			for i := 1; i <= 6; i++ {
				require.NoError(t, b.Store(ctx, "app/s", map[string][]byte{"k": []byte(fmt.Sprintf("v%d", i))}, nil))
			}

			removed, err := b.PruneVersions(ctx, "app/s", 2)
			require.NoError(t, err)
			assert.Equal(t, 4, removed)
			assert.Equal(t, []int{5, 6}, liveVersions(t, b, "app/s"))

			for v := 1; v <= 4; v++ {
				data, _, err := b.GetVersion(ctx, "app/s", v)
				require.Error(t, err, "version %d was pruned", v)
				assert.Nil(t, data)
			}
			data, _, err := b.Get(ctx, "app/s")
			require.NoError(t, err)
			assert.Equal(t, []byte("v6"), data["k"])

			// Pruning again removes nothing, and the next write numbers on.
			removed, err = b.PruneVersions(ctx, "app/s", 2)
			require.NoError(t, err)
			assert.Zero(t, removed)
			require.NoError(t, b.Store(ctx, "app/s", map[string][]byte{"k": []byte("v7")}, nil))
			assert.Equal(t, []int{5, 6, 7}, liveVersions(t, b, "app/s"))
		})
	}
}

func TestPruneVersions_RefusesBadArguments(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, b := range pruneBackends(t) {
		t.Run(name, func(t *testing.T) {
			_, err := b.PruneVersions(ctx, "", 1)
			require.ErrorIs(t, err, ErrInvalidPath)
			_, err = b.PruneVersions(ctx, "app/s", 0)
			require.ErrorIs(t, err, ErrVersionMustBePositive)
			_, err = b.PruneVersions(ctx, "app/missing", 1)
			require.ErrorIs(t, err, ErrSecretNotFound)
		})
	}
}

func TestManager_MaxVersionsBoundsEveryWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, b := range pruneBackends(t) {
		t.Run(name, func(t *testing.T) {
			m, err := NewManager(&ManagerOptions{Backend: b, MaxVersions: 3})
			require.NoError(t, err)

			for i := 1; i <= 5; i++ {
				require.NoError(t, m.Store(ctx, "app/s", map[string][]byte{"k": []byte(fmt.Sprintf("v%d", i))}, nil))
			}
			assert.Equal(t, []int{3, 4, 5}, liveVersions(t, b, "app/s"))

			v, err := m.StoreVersion(ctx, "app/s", map[string][]byte{"k": []byte("v6")}, nil)
			require.NoError(t, err)
			assert.Equal(t, 6, v)
			assert.Equal(t, []int{4, 5, 6}, liveVersions(t, b, "app/s"))

			read := readCurrent(t, b, "app/s", 6)
			v, err = m.StoreVersionIfCurrent(ctx, "app/s", read, read.Data, nil)
			require.NoError(t, err)
			assert.Equal(t, 7, v)
			assert.Equal(t, []int{5, 6, 7}, liveVersions(t, b, "app/s"))
		})
	}
}

// TestManager_MaxVersionsBoundsEnsureSecretRegeneration: every soft delete
// followed by EnsureSecret regenerates a new version; max_versions bounds
// that write too, so old generated credentials do not accumulate.
func TestManager_MaxVersionsBoundsEnsureSecretRegeneration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, b := range pruneBackends(t) {
		t.Run(name, func(t *testing.T) {
			m, err := NewManager(&ManagerOptions{Backend: b, MaxVersions: 2})
			require.NoError(t, err)
			spec := &SecretSpec{Length: 8, Encoding: EncodingNumeric}

			for i := 0; i < 5; i++ {
				_, generated, err := m.EnsureSecret(ctx, "app/ensured", spec)
				require.NoError(t, err)
				require.True(t, generated, "cycle %d regenerates after the soft delete", i)
				if i < 4 {
					require.NoError(t, m.Delete(ctx, "app/ensured"))
				}
			}
			// Retention already ran on each regeneration: a prune to the
			// same limit finds nothing left to remove.
			removed, err := m.PruneVersions(ctx, "app/ensured", 2)
			require.NoError(t, err)
			assert.Zero(t, removed, "only the newest max_versions generated values remain")
			_, _, err = b.GetVersion(ctx, "app/ensured", 5)
			require.NoError(t, err, "the current generated value is kept")
		})
	}
}

func TestManager_NoMaxVersionsKeepsEveryVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := NewMemoryBackend()
	m, err := NewManager(&ManagerOptions{Backend: b})
	require.NoError(t, err)
	for i := 1; i <= 4; i++ {
		require.NoError(t, m.Store(ctx, "app/s", map[string][]byte{"k": []byte(fmt.Sprintf("v%d", i))}, nil))
	}
	assert.Equal(t, []int{1, 2, 3, 4}, liveVersions(t, b, "app/s"))
}
