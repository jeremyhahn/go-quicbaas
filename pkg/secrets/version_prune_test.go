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
