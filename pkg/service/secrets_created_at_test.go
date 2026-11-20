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

package service

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// errVersionHistoryOutage stands in for a store that serves a secret's data
// but cannot list its version history.
var errVersionHistoryOutage = stderrors.New("version history unavailable")

// requireSameInstant fails unless got and want name the same instant.
func requireSameInstant(t *testing.T, want, got time.Time, field string) {
	t.Helper()
	require.Truef(t, want.Equal(got), "%s: want %s, got %s", field, want, got)
}

// TestSecretsService_CreatedAtIsTheStoredWriteTime pins go-quicbaas#179:
// get and list report when the store recorded each write, stable across
// reads, never the time of the read. It runs over the in-memory backend and
// the KV backend that persists secrets in production; the KV backend
// returns no version in its metadata, so it also covers the latest-version
// lookup.
func TestSecretsService_CreatedAtIsTheStoredWriteTime(t *testing.T) {
	backends := map[string]func() secrets.Backend{
		"memory": func() secrets.Backend { return secrets.NewMemoryBackend() },
		"kv":     func() secrets.Backend { return secrets.NewKVBackend(newTenantNamespacedKV()) },
	}
	for name, newBackend := range backends {
		t.Run(name, func(t *testing.T) {
			mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: newBackend()})
			require.NoError(t, err)
			svc := newSecretsService(mgr, newMockLogger())
			ctx := tenantctx.WithSystem(context.Background())

			beforeStore := time.Now()
			_, err = svc.StoreSecret(ctx, &StoreSecretRequest{SecretID: "db-password", Name: "db-password", Data: []byte("one")})
			require.NoError(t, err)
			afterStore := time.Now()

			history, err := svc.ListSecretVersions(ctx, &ListSecretVersionsRequest{SecretID: "db-password"})
			require.NoError(t, err)
			require.Len(t, history.Versions, 1)
			firstWrite := history.Versions[0].CreatedAt
			require.WithinRange(t, firstWrite, beforeStore, afterStore)

			// Two reads report the same stored instant: the write, not the read.
			for read := 0; read < 2; read++ {
				got, err := svc.GetSecret(ctx, &GetSecretRequest{SecretID: "db-password"})
				require.NoError(t, err)
				require.Equal(t, uint64(1), got.Version)
				requireSameInstant(t, firstWrite, got.CreatedAt, "get created_at")

				list, err := svc.ListSecrets(ctx, &ListSecretsRequest{})
				require.NoError(t, err)
				require.Len(t, list.Secrets, 1)
				requireSameInstant(t, firstWrite, list.Secrets[0].CreatedAt, "list created_at")
				requireSameInstant(t, firstWrite, list.Secrets[0].UpdatedAt, "list updated_at")
				require.Equal(t, uint64(1), list.Secrets[0].CurrentVersion)
			}

			// A write moves updated_at and mints a version with its own time;
			// the secret's created_at stays the first write.
			_, err = svc.RotateSecret(ctx, &RotateSecretRequest{SecretID: "db-password", NewData: []byte("two")})
			require.NoError(t, err)
			history, err = svc.ListSecretVersions(ctx, &ListSecretVersionsRequest{SecretID: "db-password"})
			require.NoError(t, err)
			require.Len(t, history.Versions, 2)
			secondWrite := history.Versions[1].CreatedAt
			require.Equal(t, uint64(2), history.Versions[1].Version)
			require.False(t, secondWrite.Before(firstWrite))

			latest, err := svc.GetSecret(ctx, &GetSecretRequest{SecretID: "db-password"})
			require.NoError(t, err)
			require.Equal(t, uint64(2), latest.Version)
			requireSameInstant(t, secondWrite, latest.CreatedAt, "latest version created_at")

			first, err := svc.GetSecret(ctx, &GetSecretRequest{SecretID: "db-password", Version: 1})
			require.NoError(t, err)
			requireSameInstant(t, firstWrite, first.CreatedAt, "version 1 created_at")

			list, err := svc.ListSecrets(ctx, &ListSecretsRequest{})
			require.NoError(t, err)
			require.Len(t, list.Secrets, 1)
			requireSameInstant(t, firstWrite, list.Secrets[0].CreatedAt, "list created_at after write")
			requireSameInstant(t, secondWrite, list.Secrets[0].UpdatedAt, "list updated_at after write")
			require.Equal(t, uint64(2), list.Secrets[0].CurrentVersion)
		})
	}
}

// TestSecretsService_CreatedAtUnreadableHistoryIsNotTheReadTime pins the
// failure side of go-quicbaas#179: when the store cannot say when a secret
// was written, get fails and list leaves the secret out, rather than either
// reporting the time of the read.
func TestSecretsService_CreatedAtUnreadableHistoryIsNotTheReadTime(t *testing.T) {
	mgr := newMockSecretsManager()
	svc := newSecretsService(mgr, newMockLogger())
	ctx := tenantctx.WithSystem(context.Background())

	_, err := svc.StoreSecret(ctx, &StoreSecretRequest{SecretID: "db-password", Name: "db-password", Data: []byte("one")})
	require.NoError(t, err)
	mgr.listVersionsFn = func(context.Context, string) ([]*interfaces.SecretVersionMetadata, error) {
		return nil, errVersionHistoryOutage
	}

	got, err := svc.GetSecret(ctx, &GetSecretRequest{SecretID: "db-password"})
	require.ErrorIs(t, err, errVersionHistoryOutage)
	require.Nil(t, got)

	list, err := svc.ListSecrets(ctx, &ListSecretsRequest{})
	require.NoError(t, err)
	require.Empty(t, list.Secrets)
}

// TestSecretsService_CreatedAtMissingVersionFails pins that a version the
// store holds data for but no write time is reported as an error, not
// given a substitute time.
func TestSecretsService_CreatedAtMissingVersionFails(t *testing.T) {
	mgr := newMockSecretsManager()
	svc := newSecretsService(mgr, newMockLogger())
	ctx := tenantctx.WithSystem(context.Background())

	_, err := svc.StoreSecret(ctx, &StoreSecretRequest{SecretID: "db-password", Name: "db-password", Data: []byte("one")})
	require.NoError(t, err)
	mgr.listVersionsFn = func(context.Context, string) ([]*interfaces.SecretVersionMetadata, error) {
		return []*interfaces.SecretVersionMetadata{{Version: 7, CreatedAt: time.Unix(1, 0)}}, nil
	}

	got, err := svc.GetSecret(ctx, &GetSecretRequest{SecretID: "db-password", Version: 1})
	require.Error(t, err)
	require.Nil(t, got)
}

// writeAfterGetManager is a secrets manager whose next latest-version read
// runs afterGet once the backend has answered, before the service goes on to
// read the version history: the window a concurrent writer lands in.
type writeAfterGetManager struct {
	*secrets.Manager
	afterGet func(ctx context.Context, path string)
}

func (m *writeAfterGetManager) Get(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
	data, metadata, err := m.Manager.Get(ctx, path)
	if hook := m.afterGet; hook != nil {
		m.afterGet = nil
		hook(ctx, path)
	}
	return data, metadata, err
}

// TestSecretsService_GetReportsTheVersionItReturned pins the version and
// created_at of a latest-version read to the data it returned, even when a
// write lands between the backend read and the history read (go-quicbaas#179).
func TestSecretsService_GetReportsTheVersionItReturned(t *testing.T) {
	backends := map[string]func() secrets.Backend{
		"memory": func() secrets.Backend { return secrets.NewMemoryBackend() },
		"kv":     func() secrets.Backend { return secrets.NewKVBackend(newTenantNamespacedKV()) },
	}
	for name, newBackend := range backends {
		t.Run(name, func(t *testing.T) {
			base, err := secrets.NewManager(&secrets.ManagerOptions{Backend: newBackend()})
			require.NoError(t, err)
			mgr := &writeAfterGetManager{Manager: base}
			svc := newSecretsService(mgr, newMockLogger())
			ctx := tenantctx.WithSystem(context.Background())

			// Seed through the manager, as writers outside StoreSecret do, so
			// the stored metadata carries no version of its own: the version
			// reported must come from the backend's read.
			tenantID, err := secretsScopedTenant(ctx, "", "test")
			require.NoError(t, err)
			scopedID := secretsTenantScopedID(tenantID, "db-password")
			require.NoError(t, base.Store(ctx, scopedID, map[string][]byte{"data": []byte("one")}, nil))
			history, err := svc.ListSecretVersions(ctx, &ListSecretVersionsRequest{SecretID: "db-password"})
			require.NoError(t, err)
			require.Len(t, history.Versions, 1)
			firstWrite := history.Versions[0].CreatedAt

			wrote := make(chan struct{})
			mgr.afterGet = func(ctx context.Context, path string) {
				defer close(wrote)
				data := map[string][]byte{"data": []byte("two")}
				require.NoError(t, base.Store(ctx, path, data, nil))
			}

			got, err := svc.GetSecret(ctx, &GetSecretRequest{SecretID: "db-password"})
			require.NoError(t, err)
			<-wrote
			require.Equal(t, []byte("one"), got.Data)
			require.Equal(t, uint64(1), got.Version)
			requireSameInstant(t, firstWrite, got.CreatedAt, "created_at of the returned version")

			// The concurrent write did land: the next read sees it.
			latest, err := svc.GetSecret(ctx, &GetSecretRequest{SecretID: "db-password"})
			require.NoError(t, err)
			require.Equal(t, []byte("two"), latest.Data)
			require.Equal(t, uint64(2), latest.Version)
		})
	}
}

// prunedVersionGetManager makes the latest-version read lose its version to a
// concurrent write's retention prune between the pointer read and the
// version fetch: the first Get writes a new version (pruning the old one
// under max_versions=1) and then fetches the version it had read the
// pointer of.
type prunedVersionGetManager struct {
	*secrets.Manager
	pruneOnce func(ctx context.Context, path string) int
}

func (m *prunedVersionGetManager) Get(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
	if hook := m.pruneOnce; hook != nil {
		m.pruneOnce = nil
		readVersion := hook(ctx, path)
		return m.Manager.GetVersion(ctx, path, readVersion)
	}
	return m.Manager.Get(ctx, path)
}

// TestSecretsService_GetSurvivesAConcurrentPrune: with max_versions=1 a
// write that lands during a latest-version read prunes the version the read
// was returning. The secret exists throughout, so the read is not an error:
// it is retried whole and returns the version that replaced it, with that
// version's write time.
func TestSecretsService_GetSurvivesAConcurrentPrune(t *testing.T) {
	seed := func(t *testing.T) (*secrets.Manager, string, context.Context) {
		t.Helper()
		base, err := secrets.NewManager(&secrets.ManagerOptions{
			Backend:     secrets.NewKVBackend(newTenantNamespacedKV()),
			MaxVersions: 1,
		})
		require.NoError(t, err)
		ctx := tenantctx.WithSystem(context.Background())
		tenantID, err := secretsScopedTenant(ctx, "", "test")
		require.NoError(t, err)
		scopedID := secretsTenantScopedID(tenantID, "db-password")
		require.NoError(t, base.Store(ctx, scopedID, map[string][]byte{"data": []byte("one")}, nil))
		return base, scopedID, ctx
	}
	requireReplacement := func(t *testing.T, svc SecretsService, ctx context.Context) {
		t.Helper()
		got, err := svc.GetSecret(ctx, &GetSecretRequest{SecretID: "db-password"})
		require.NoError(t, err)
		require.Equal(t, []byte("two"), got.Data)
		require.Equal(t, uint64(2), got.Version)
		history, err := svc.ListSecretVersions(ctx, &ListSecretVersionsRequest{SecretID: "db-password"})
		require.NoError(t, err)
		// Version 1 was pruned: at most its timestamp tombstone remains,
		// and version 2 is the only version with data.
		var live []*SecretVersion
		for _, v := range history.Versions {
			if v.DeletedAt == nil {
				live = append(live, v)
			}
		}
		require.Len(t, live, 1, "version 1 was pruned")
		require.Equal(t, uint64(2), live[0].Version)
		requireSameInstant(t, live[0].CreatedAt, got.CreatedAt, "created_at of the returned version")
	}

	t.Run("pruned before the write-time read", func(t *testing.T) {
		base, _, ctx := seed(t)
		mgr := &writeAfterGetManager{Manager: base}
		mgr.afterGet = func(ctx context.Context, path string) {
			require.NoError(t, base.Store(ctx, path, map[string][]byte{"data": []byte("two")}, nil))
		}
		requireReplacement(t, newSecretsService(mgr, newMockLogger()), ctx)
	})

	t.Run("pruned between the pointer and the version read", func(t *testing.T) {
		base, _, ctx := seed(t)
		mgr := &prunedVersionGetManager{Manager: base}
		mgr.pruneOnce = func(ctx context.Context, path string) int {
			require.NoError(t, base.Store(ctx, path, map[string][]byte{"data": []byte("two")}, nil))
			return 1
		}
		requireReplacement(t, newSecretsService(mgr, newMockLogger()), ctx)
	})

	t.Run("a requested version that was pruned is not found", func(t *testing.T) {
		base, scopedID, ctx := seed(t)
		require.NoError(t, base.Store(ctx, scopedID, map[string][]byte{"data": []byte("two")}, nil))
		svc := newSecretsService(base, newMockLogger())
		_, err := svc.GetSecret(ctx, &GetSecretRequest{SecretID: "db-password", Version: 1})
		require.Error(t, err)
		require.Equal(t, qberrors.ErrNotFound, qberrors.Code(err))
	})
}

// TestSecretsService_CreatedAtSurvivesRetention: with max_versions=1 every
// write prunes the version before it, the first one included. The secret's
// created_at is still its first write, on every backend, however many
// retention-triggering writes follow.
func TestSecretsService_CreatedAtSurvivesRetention(t *testing.T) {
	backends := map[string]func() secrets.Backend{
		"memory": func() secrets.Backend { return secrets.NewMemoryBackend() },
		"kv":     func() secrets.Backend { return secrets.NewKVBackend(newTenantNamespacedKV()) },
	}
	for name, newBackend := range backends {
		t.Run(name, func(t *testing.T) {
			mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: newBackend(), MaxVersions: 1})
			require.NoError(t, err)
			svc := newSecretsService(mgr, newMockLogger())
			ctx := tenantctx.WithSystem(context.Background())

			_, err = svc.StoreSecret(ctx, &StoreSecretRequest{SecretID: "db-password", Name: "db-password", Data: []byte("one")})
			require.NoError(t, err)
			list, err := svc.ListSecrets(ctx, &ListSecretsRequest{})
			require.NoError(t, err)
			require.Len(t, list.Secrets, 1)
			firstWrite := list.Secrets[0].CreatedAt

			for i := 2; i <= 4; i++ {
				time.Sleep(2 * time.Millisecond) // each write's time is distinct
				_, err = svc.RotateSecret(ctx, &RotateSecretRequest{SecretID: "db-password", NewData: []byte{byte('0' + i)}})
				require.NoError(t, err)

				list, err = svc.ListSecrets(ctx, &ListSecretsRequest{})
				require.NoError(t, err)
				require.Len(t, list.Secrets, 1)
				requireSameInstant(t, firstWrite, list.Secrets[0].CreatedAt, "list created_at after a pruning write")
				require.True(t, list.Secrets[0].UpdatedAt.After(firstWrite), "updated_at moves with the write")
				require.Equal(t, uint64(i), list.Secrets[0].CurrentVersion)

				got, err := svc.GetSecret(ctx, &GetSecretRequest{SecretID: "db-password"})
				require.NoError(t, err)
				require.Equal(t, uint64(i), got.Version)
				require.Equal(t, []byte{byte('0' + i)}, got.Data)
			}
		})
	}
}

// TestSecretsService_ListVersionsMarksPrunedVersionsDeleted: versions that
// retention pruned are listed with a deletion time, not as available.
func TestSecretsService_ListVersionsMarksPrunedVersionsDeleted(t *testing.T) {
	backends := map[string]func() secrets.Backend{
		"memory": func() secrets.Backend { return secrets.NewMemoryBackend() },
		"kv":     func() secrets.Backend { return secrets.NewKVBackend(newTenantNamespacedKV()) },
	}
	for name, newBackend := range backends {
		t.Run(name, func(t *testing.T) {
			mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: newBackend(), MaxVersions: 1})
			require.NoError(t, err)
			svc := newSecretsService(mgr, newMockLogger())
			ctx := tenantctx.WithSystem(context.Background())

			_, err = svc.StoreSecret(ctx, &StoreSecretRequest{SecretID: "db-password", Name: "db-password", Data: []byte("one")})
			require.NoError(t, err)
			for i := 2; i <= 3; i++ {
				_, err = svc.RotateSecret(ctx, &RotateSecretRequest{SecretID: "db-password", NewData: []byte{byte('0' + i)}})
				require.NoError(t, err)
			}

			resp, err := svc.ListSecretVersions(ctx, &ListSecretVersionsRequest{SecretID: "db-password"})
			require.NoError(t, err)
			byVersion := make(map[uint64]*SecretVersion, len(resp.Versions))
			for _, v := range resp.Versions {
				byVersion[v.Version] = v
			}
			require.Contains(t, byVersion, uint64(3))
			require.Nil(t, byVersion[3].DeletedAt, "the current version is available")
			for v, sv := range byVersion {
				if v != 3 {
					require.NotNil(t, sv.DeletedAt, "pruned version %d is listed as deleted", v)
				}
			}
		})
	}
}
