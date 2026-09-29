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

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

func asPrincipal(id string) context.Context {
	return auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: id, Authenticated: true})
}

// Every version records the authenticated principal that minted it, on both
// backends, and a rollback is authored by whoever rolled back, not by the
// author of the version it copies (#862).
func TestBackend_VersionCreatedByIsTheMintingPrincipal(t *testing.T) {
	t.Parallel()

	backends := map[string]func(t *testing.T) Backend{
		"memory":    func(*testing.T) Backend { return NewMemoryBackend() },
		"kv":        func(*testing.T) Backend { return NewKVBackend(newInMemKV()) },
		"encrypted": func(t *testing.T) Backend { return createTestEncryptedBackend(t) },
	}
	for name, mk := range backends {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := mk(t)
			const path = "app/db-password"
			data := map[string][]byte{"data": []byte("v")}

			require.NoError(t, b.Store(asPrincipal("alice"), path, data, nil))
			require.NoError(t, b.Store(asPrincipal("bob"), path, data, nil))
			require.NoError(t, b.Rollback(asPrincipal("carol"), path, 1))
			require.NoError(t, b.Store(context.Background(), path, data, nil))

			versions, err := b.ListVersions(context.Background(), path)
			require.NoError(t, err)
			got := make(map[int]string, len(versions))
			for _, v := range versions {
				got[v.Version] = v.CreatedBy
			}
			require.Equal(t, map[int]string{1: "alice", 2: "bob", 3: "carol", 4: ""}, got)
		})
	}
}

// The Manager hands the backend's authorship through to its callers.
func TestManager_ListVersionsCarriesCreatedBy(t *testing.T) {
	t.Parallel()

	mgr, err := NewManager(&ManagerOptions{Backend: NewMemoryBackend()})
	require.NoError(t, err)
	require.NoError(t, mgr.Store(asPrincipal("alice"), "p", map[string][]byte{"data": []byte("v")}, nil))

	versions, err := mgr.ListVersions(context.Background(), "p")
	require.NoError(t, err)
	require.Len(t, versions, 1)
	require.Equal(t, "alice", versions[0].CreatedBy)
}
