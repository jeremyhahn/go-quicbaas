// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package secrets

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// versionedTestBackends are the production backends, each of which must
// report the version it assigned to a write.
func versionedTestBackends() map[string]func(t *testing.T) VersionedBackend {
	return map[string]func(t *testing.T) VersionedBackend{
		"memory":    func(*testing.T) VersionedBackend { return NewMemoryBackend() },
		"kv":        func(*testing.T) VersionedBackend { return NewKVBackend(newInMemKV()) },
		"encrypted": func(t *testing.T) VersionedBackend { return createTestEncryptedBackend(t) },
	}
}

// TestBackends_StoreVersionReportsTheAssignedVersion holds every backend to
// the write half of the version contract: StoreVersion returns the version
// the backend recorded for that write (go-quicbaas#179).
func TestBackends_StoreVersionReportsTheAssignedVersion(t *testing.T) {
	for name, newBackend := range versionedTestBackends() {
		t.Run(name, func(t *testing.T) {
			b := newBackend(t)
			ctx := context.Background()

			for want := 1; want <= 2; want++ {
				got, err := b.StoreVersion(ctx, "secret/pass", map[string][]byte{"pass": []byte("v")}, nil)
				require.NoError(t, err)
				assert.Equal(t, want, got)
			}
			versions, err := b.ListVersions(ctx, "secret/pass")
			require.NoError(t, err)
			require.Len(t, versions, 2)
			assert.Equal(t, 2, versions[1].Version)
		})
	}
}

// TestBackends_StoreVersionRefusesInvalidWrites covers the refusals: no
// version is reported for a write that did not happen.
func TestBackends_StoreVersionRefusesInvalidWrites(t *testing.T) {
	for name, newBackend := range versionedTestBackends() {
		t.Run(name, func(t *testing.T) {
			b := newBackend(t)
			ctx := context.Background()

			got, err := b.StoreVersion(ctx, "", map[string][]byte{"pass": []byte("v")}, nil)
			require.ErrorIs(t, err, ErrInvalidPath)
			assert.Zero(t, got)

			got, err = b.StoreVersion(ctx, "secret/pass", nil, nil)
			require.ErrorIs(t, err, ErrEmptyData)
			assert.Zero(t, got)
		})
	}
}

// TestKVBackend_StoreVersionAfterPrunedVersion pins why the version must come
// from the write: once a version entry is gone, the count of the remaining
// history no longer names the next version the backend allocates.
func TestKVBackend_StoreVersionAfterPrunedVersion(t *testing.T) {
	kv := newInMemKV()
	b := NewKVBackend(kv)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		_, err := b.StoreVersion(ctx, "secret/pass", map[string][]byte{"pass": []byte("v")}, nil)
		require.NoError(t, err)
	}
	require.NoError(t, kv.Delete(ctx, versionKey("secret/pass", 1)))
	versions, err := b.ListVersions(ctx, "secret/pass")
	require.NoError(t, err)
	require.Len(t, versions, 1)

	got, err := b.StoreVersion(ctx, "secret/pass", map[string][]byte{"pass": []byte("v3")}, nil)
	require.NoError(t, err)
	assert.Equal(t, 3, got)
}

// TestManager_StoreVersionReportsTheBackendVersion covers both manager
// entry points over a versioned backend.
func TestManager_StoreVersionReportsTheBackendVersion(t *testing.T) {
	m, err := NewManager(&ManagerOptions{Backend: NewMemoryBackend()})
	require.NoError(t, err)
	ctx := context.Background()
	data := map[string][]byte{"pass": []byte("v")}

	got, err := m.StoreVersion(ctx, "secret/pass", data, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, got)

	got, err = m.StoreVersionWithOptions(ctx, "secret/pass", data, nil, &interfaces.StoreOptions{KeyIdentifier: "key-1"})
	require.NoError(t, err)
	assert.Equal(t, 2, got)
}

// TestManager_StoreVersionRefusesWhatItCannotName covers the refusals: an
// invalid write, a backend that reports no version, and a backend that
// would apply options but cannot report the version it assigned.
func TestManager_StoreVersionRefusesWhatItCannotName(t *testing.T) {
	ctx := context.Background()
	data := map[string][]byte{"pass": []byte("v")}

	versioned, err := NewManager(&ManagerOptions{Backend: NewMemoryBackend()})
	require.NoError(t, err)
	_, err = versioned.StoreVersion(ctx, "", data, nil)
	require.ErrorIs(t, err, ErrInvalidPath)
	_, err = versioned.StoreVersion(ctx, "secret/pass", nil, nil)
	require.ErrorIs(t, err, ErrEmptyData)
	done, cancel := context.WithCancel(ctx)
	cancel()
	_, err = versioned.StoreVersion(done, "secret/pass", data, nil)
	require.ErrorIs(t, err, context.Canceled)

	unversioned, err := NewManager(&ManagerOptions{Backend: &mockBackend{secrets: make(map[string]*SecretEntry)}})
	require.NoError(t, err)
	got, err := unversioned.StoreVersion(ctx, "secret/pass", data, nil)
	require.ErrorIs(t, err, ErrVersionNotReported)
	assert.Zero(t, got)

	withOptions, err := NewManager(&ManagerOptions{Backend: &mockBackendWithOptions{mockBackend: &mockBackend{secrets: make(map[string]*SecretEntry)}}})
	require.NoError(t, err)
	got, err = withOptions.StoreVersionWithOptions(ctx, "secret/pass", data, nil, &interfaces.StoreOptions{KeyIdentifier: "key-1"})
	require.ErrorIs(t, err, ErrVersionNotReported)
	assert.Zero(t, got)
	_, _, err = withOptions.Get(ctx, "secret/pass")
	require.Error(t, err, "a refused write must not be stored")
}
