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

package service

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	quicbaasErrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// metadataRecordingBackend records the metadata of every write that reaches
// the backend, so a test can see what the service asked the store to keep.
type metadataRecordingBackend struct {
	secrets.VersionedBackend

	mu      sync.Mutex
	written []map[string]string
}

func (b *metadataRecordingBackend) record(metadata map[string]string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	copied := make(map[string]string, len(metadata))
	for k, v := range metadata {
		copied[k] = v
	}
	b.written = append(b.written, copied)
}

func (b *metadataRecordingBackend) Store(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error {
	b.record(metadata)
	return b.VersionedBackend.Store(ctx, path, data, metadata)
}

func (b *metadataRecordingBackend) StoreVersion(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) (int, error) {
	b.record(metadata)
	return b.VersionedBackend.StoreVersion(ctx, path, data, metadata)
}

// writes returns a snapshot of the recorded write metadata.
func (b *metadataRecordingBackend) writes() []map[string]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]map[string]string(nil), b.written...)
}

// versionTestBackends are the backends the version tests run over: the
// in-memory backend and the KV backend that persists secrets in production.
func versionTestBackends() map[string]func() secrets.VersionedBackend {
	return map[string]func() secrets.VersionedBackend{
		"memory": func() secrets.VersionedBackend { return secrets.NewMemoryBackend() },
		"kv":     func() secrets.VersionedBackend { return secrets.NewKVBackend(newTenantNamespacedKV()) },
	}
}

// runReleasedTogether runs write(i) for every writer at once: each goroutine
// blocks on one channel, closed after all of them have started.
func runReleasedTogether(writers int, write func(i int)) {
	release := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-release
			write(i)
		}()
	}
	close(release)
	wg.Wait()
}

// TestSecretsService_ConcurrentWritesReportTheBackendVersion pins
// go-quicbaas#179: concurrent writers to one secret each report the version
// the backend assigned to their own write, never a number predicted from a
// count of the history, and the service never asks the store to keep a
// version of its own in the metadata.
func TestSecretsService_ConcurrentWritesReportTheBackendVersion(t *testing.T) {
	const writers = 8
	for name, newBackend := range versionTestBackends() {
		t.Run(name, func(t *testing.T) {
			backend := &metadataRecordingBackend{VersionedBackend: newBackend()}
			mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: backend})
			require.NoError(t, err)
			svc := newSecretsService(mgr, newMockLogger())
			ctx := tenantctx.WithSystem(context.Background())

			stored := make([]uint64, writers)
			storeErrs := make([]error, writers)
			runReleasedTogether(writers, func(i int) {
				resp, err := svc.StoreSecret(ctx, &StoreSecretRequest{
					SecretID: "db-password", Name: "db-password",
					Data: []byte("store-" + strconv.Itoa(i)),
				})
				storeErrs[i] = err
				if err == nil {
					stored[i] = resp.Version
				}
			})

			rotated := make([]*RotateSecretResponse, writers)
			rotateErrs := make([]error, writers)
			runReleasedTogether(writers, func(i int) {
				rotated[i], rotateErrs[i] = svc.RotateSecret(ctx, &RotateSecretRequest{
					SecretID: "db-password", NewData: []byte("rotate-" + strconv.Itoa(i)),
				})
			})

			reported := make(map[uint64]string, 2*writers)
			requireOwnVersion := func(version uint64, data string) {
				t.Helper()
				require.NotContainsf(t, reported, version, "two writers report version %d", version)
				reported[version] = data
				got, err := svc.GetSecretVersion(ctx, &GetSecretVersionRequest{SecretID: "db-password", Version: version})
				require.NoError(t, err)
				require.Equalf(t, data, string(got.Version.Data), "version %d holds another writer's data", version)
			}
			for i := 0; i < writers; i++ {
				require.NoError(t, storeErrs[i])
				requireOwnVersion(stored[i], "store-"+strconv.Itoa(i))
			}
			for i := 0; i < writers; i++ {
				require.NoError(t, rotateErrs[i])
				require.Less(t, rotated[i].OldVersion, rotated[i].NewVersion)
				requireOwnVersion(rotated[i].NewVersion, "rotate-"+strconv.Itoa(i))
			}

			for _, metadata := range backend.writes() {
				require.NotContains(t, metadata, "version", "the service wrote a version into the stored metadata")
			}
		})
	}
}

// TestSecretsService_StoreAfterPrunedVersionReportsTheBackendVersion pins
// go-quicbaas#179: once a version is gone from the history, the count of the
// remaining versions no longer names the next one. The write reports the
// version the KV backend assigned, which is past every version it ever
// allocated.
func TestSecretsService_StoreAfterPrunedVersionReportsTheBackendVersion(t *testing.T) {
	kv := newTenantNamespacedKV()
	mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: secrets.NewKVBackend(kv)})
	require.NoError(t, err)
	svc := newSecretsService(mgr, newMockLogger())
	ctx := tenantctx.WithSystem(context.Background())

	for i := 1; i <= 3; i++ {
		resp, err := svc.StoreSecret(ctx, &StoreSecretRequest{
			SecretID: "db-password", Name: "db-password", Data: []byte("v" + strconv.Itoa(i)),
		})
		require.NoError(t, err)
		require.Equal(t, uint64(i), resp.Version)
	}
	require.NoError(t, kv.Delete(ctx, "_qb/secrets/db-password/v/1"))
	history, err := svc.ListSecretVersions(ctx, &ListSecretVersionsRequest{SecretID: "db-password"})
	require.NoError(t, err)
	require.Len(t, history.Versions, 2)

	resp, err := svc.StoreSecret(ctx, &StoreSecretRequest{
		SecretID: "db-password", Name: "db-password", Data: []byte("v4"),
	})
	require.NoError(t, err)
	require.Equal(t, uint64(4), resp.Version)

	latest, err := svc.GetSecret(ctx, &GetSecretRequest{SecretID: "db-password"})
	require.NoError(t, err)
	require.Equal(t, uint64(4), latest.Version)
	require.Equal(t, "v4", string(latest.Data))
}

// unversionedSecretsManager exposes only interfaces.SecretsManager, so it
// cannot report the version a write was assigned.
type unversionedSecretsManager struct {
	interfaces.SecretsManager
}

// zeroVersionSecretsManager reports no version for its writes.
type zeroVersionSecretsManager struct {
	*mockSecretsManager
}

func (m *zeroVersionSecretsManager) StoreVersion(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) (int, error) {
	if err := m.mockSecretsManager.Store(ctx, path, data, metadata); err != nil {
		return 0, err
	}
	return 0, nil
}

// requireVersionNotReported asserts err is the internal error the service
// returns when a manager breaks the version contract.
func requireVersionNotReported(t *testing.T, err error, operation string) {
	t.Helper()
	var qbErr *quicbaasErrors.QBError
	require.ErrorAs(t, err, &qbErr)
	require.Equal(t, quicbaasErrors.ErrInternal, qbErr.Code)
	var notReported *SecretVersionNotReportedError
	require.ErrorAs(t, err, &notReported)
	require.Equal(t, operation, notReported.Operation)
}

// TestSecretsService_ManagerReportingNoVersionIsAnInternalError pins
// go-quicbaas#179: the service has no fallback for a manager that does not
// report a version. A write or read without one is a contract violation.
func TestSecretsService_ManagerReportingNoVersionIsAnInternalError(t *testing.T) {
	ctx := tenantctx.WithSystem(context.Background())
	store := &StoreSecretRequest{SecretID: "db-password", Name: "db-password", Data: []byte("v1")}

	t.Run("manager without StoreVersion", func(t *testing.T) {
		svc := newSecretsService(&unversionedSecretsManager{SecretsManager: newMockSecretsManager()}, newMockLogger())
		_, err := svc.StoreSecret(ctx, store)
		requireVersionNotReported(t, err, "write")
	})

	t.Run("StoreVersion reports zero", func(t *testing.T) {
		svc := newSecretsService(&zeroVersionSecretsManager{mockSecretsManager: newMockSecretsManager()}, newMockLogger())
		_, err := svc.StoreSecret(ctx, store)
		requireVersionNotReported(t, err, "write")
	})

	t.Run("read without a version", func(t *testing.T) {
		mgr := newMockSecretsManager()
		mgr.getFn = func(context.Context, string) (map[string][]byte, map[string]string, error) {
			return map[string][]byte{"data": []byte("v1")}, map[string]string{"name": "db-password"}, nil
		}
		svc := newSecretsService(mgr, newMockLogger())

		_, err := svc.GetSecret(ctx, &GetSecretRequest{SecretID: "db-password"})
		requireVersionNotReported(t, err, "read")

		_, err = svc.RotateSecret(ctx, &RotateSecretRequest{SecretID: "db-password", NewData: []byte("v2")})
		requireVersionNotReported(t, err, "read")
	})
}
