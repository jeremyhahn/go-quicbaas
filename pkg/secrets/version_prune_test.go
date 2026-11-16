package secrets

import (
	"context"
	"fmt"
	"sync"
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
				assert.Len(t, versions, 4, "the write landed on the pruned history")
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
