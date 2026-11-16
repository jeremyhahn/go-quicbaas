package secrets

import (
	"context"
	"fmt"
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
