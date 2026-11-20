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
	"testing"

	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// passthroughSecretsEncryptor is a SecretsEncryptor that stores the
// plaintext as the ciphertext, so the encrypted backend can run without a
// key manager.
type passthroughSecretsEncryptor struct{}

func (passthroughSecretsEncryptor) Encrypt(_ context.Context, keyIdentifier string, plaintext []byte, opts *interfaces.EncryptOptions) (*interfaces.EncryptedBlob, error) {
	blob := &interfaces.EncryptedBlob{
		KeyIdentifier: keyIdentifier,
		Algorithm:     interfaces.DefaultAlgorithm,
		Ciphertext:    append([]byte(nil), plaintext...),
	}
	if opts != nil {
		if opts.Algorithm != "" {
			blob.Algorithm = opts.Algorithm
		}
		blob.AAD = opts.AAD
	}
	return blob, nil
}

func (passthroughSecretsEncryptor) Decrypt(_ context.Context, blob *interfaces.EncryptedBlob) ([]byte, error) {
	return append([]byte(nil), blob.Ciphertext...), nil
}

func (passthroughSecretsEncryptor) ValidateKeyIdentifier(context.Context, string) error {
	return nil
}

// absenceReportingKV reports a missing key as the KVStore contract's
// QBError with ErrNotFound, which the encrypted backend reads as absence.
type absenceReportingKV struct {
	*tenantNamespacedKV
}

func (kv absenceReportingKV) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := kv.tenantNamespacedKV.Get(ctx, key)
	if (err != nil && qberrors.IsNotFound(err)) || (err == nil && data == nil) {
		return nil, &qberrors.QBError{Code: qberrors.ErrNotFound, Op: "absenceReportingKV.Get", Err: err}
	}
	return data, err
}

// prunedSecretBackends returns one fresh instance of each secrets backend.
func prunedSecretBackends(t *testing.T) map[string]secrets.Backend {
	t.Helper()
	encrypted, err := secrets.NewEncryptedBackend(&secrets.EncryptedBackendOptions{
		Store:                absenceReportingKV{tenantNamespacedKV: newTenantNamespacedKV()},
		Encryptor:            passthroughSecretsEncryptor{},
		DefaultKeyIdentifier: string(secrets.DEKIDGlobal),
	})
	require.NoError(t, err)
	return map[string]secrets.Backend{
		"memory":    secrets.NewMemoryBackend(),
		"kv":        secrets.NewKVBackend(newTenantNamespacedKV()),
		"encrypted": encrypted,
	}
}

// pruneAfterGetVersionManager runs afterGetVersion once, after the first
// versioned read: a write that prunes the version just read lands between
// that read and the history read.
type pruneAfterGetVersionManager struct {
	*secrets.Manager
	afterGetVersion func(ctx context.Context, path string)
}

func (m *pruneAfterGetVersionManager) GetVersion(ctx context.Context, path string, version int) (map[string][]byte, map[string]string, error) {
	data, metadata, err := m.Manager.GetVersion(ctx, path, version)
	if hook := m.afterGetVersion; hook != nil {
		m.afterGetVersion = nil
		hook(ctx, path)
	}
	return data, metadata, err
}

func newPrunedSecretsManager(t *testing.T, b secrets.Backend) *secrets.Manager {
	t.Helper()
	mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: b, MaxVersions: 1})
	require.NoError(t, err)
	return mgr
}

func TestSecretsService_PrunedVersionIsNotFound(t *testing.T) {
	for name, b := range prunedSecretBackends(t) {
		t.Run(name, func(t *testing.T) {
			svc := newSecretsService(newPrunedSecretsManager(t, b), newMockLogger())
			ctx := tenantctx.WithSystem(context.Background())

			_, err := svc.StoreSecret(ctx, &StoreSecretRequest{SecretID: "db-password", Name: "db-password", Data: []byte("one")})
			require.NoError(t, err)
			_, err = svc.RotateSecret(ctx, &RotateSecretRequest{SecretID: "db-password", NewData: []byte("two")})
			require.NoError(t, err)

			_, err = svc.GetSecret(ctx, &GetSecretRequest{SecretID: "db-password", Version: 1})
			require.Error(t, err)
			require.Equal(t, qberrors.ErrNotFound, qberrors.Code(err), "GetSecret of a pruned version: %v", err)

			_, err = svc.GetSecretVersion(ctx, &GetSecretVersionRequest{SecretID: "db-password", Version: 1})
			require.Error(t, err)
			require.Equal(t, qberrors.ErrNotFound, qberrors.Code(err), "GetSecretVersion of a pruned version: %v", err)

			got, err := svc.GetSecret(ctx, &GetSecretRequest{SecretID: "db-password"})
			require.NoError(t, err)
			require.Equal(t, []byte("two"), got.Data)
			require.Equal(t, uint64(2), got.Version)
		})
	}
}

func TestSecretsService_VersionedReadPrunedBeforeHistoryIsNotFound(t *testing.T) {
	for name, b := range prunedSecretBackends(t) {
		t.Run(name, func(t *testing.T) {
			base := newPrunedSecretsManager(t, b)
			ctx := tenantctx.WithSystem(context.Background())
			_, err := newSecretsService(base, newMockLogger()).StoreSecret(ctx, &StoreSecretRequest{SecretID: "db-password", Name: "db-password", Data: []byte("one")})
			require.NoError(t, err)

			mgr := &pruneAfterGetVersionManager{Manager: base}
			mgr.afterGetVersion = func(ctx context.Context, path string) {
				require.NoError(t, base.Store(ctx, path, map[string][]byte{"data": []byte("two")}, nil))
			}
			svc := newSecretsService(mgr, newMockLogger())

			_, err = svc.GetSecret(ctx, &GetSecretRequest{SecretID: "db-password", Version: 1})
			require.Error(t, err)
			require.Equal(t, qberrors.ErrNotFound, qberrors.Code(err), "version pruned during the read: %v", err)
		})
	}
}

func TestSecretsService_LatestReadRetriesADestroyedVersion(t *testing.T) {
	for name, b := range prunedSecretBackends(t) {
		t.Run(name, func(t *testing.T) {
			base := newPrunedSecretsManager(t, b)
			ctx := tenantctx.WithSystem(context.Background())
			_, err := newSecretsService(base, newMockLogger()).StoreSecret(ctx, &StoreSecretRequest{SecretID: "db-password", Name: "db-password", Data: []byte("one")})
			require.NoError(t, err)

			mgr := &prunedVersionGetManager{Manager: base}
			mgr.pruneOnce = func(ctx context.Context, path string) int {
				require.NoError(t, base.Store(ctx, path, map[string][]byte{"data": []byte("two")}, nil))
				return 1
			}
			svc := newSecretsService(mgr, newMockLogger())

			got, err := svc.GetSecret(ctx, &GetSecretRequest{SecretID: "db-password"})
			require.NoError(t, err)
			require.Equal(t, []byte("two"), got.Data)
			require.Equal(t, uint64(2), got.Version)
		})
	}
}
