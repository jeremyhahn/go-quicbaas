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

package kms

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// newKeyNameGuardXKMS stands up the same go-xkms stack as initXKMSForTest
// and returns the key storage, so a test can seed it and read the marker.
func newKeyNameGuardXKMS(t *testing.T) xkmssdk.StorageBackend {
	t.Helper()

	dbxkms.Reset()
	t.Cleanup(dbxkms.Reset)

	memStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)
	certStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)

	swBackend, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{
		KeyStorage: memStorage,
	})
	require.NoError(t, err)

	ks, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     swBackend,
		CertStorage: certStorage,
	})
	require.NoError(t, err)

	err = dbxkms.Initialize(&dbxkms.Config{
		DefaultBackend: "software",
		Backends: map[string]xkmssdk.ServiceBackend{
			"software": ks,
		},
		Logger: &mockLogger{},
	})
	require.NoError(t, err)

	return memStorage
}

func TestInitializeFromXKMS_RefusesLegacyEncodedKeyName(t *testing.T) {
	ctx := context.Background()
	store := newKeyNameGuardXKMS(t)

	// A legacy store: the literal name was written before the mapping, so it
	// is stored as is, and it reads as tenant acme's key tenant/acme/k.
	const legacy = "qbx.dGVuYW50L2FjbWUvaw"
	kb := newRealXKMSBackendForTest(t)
	attrs := kb.algorithmToKeyAttributes(legacy, AlgAES256GCM)
	attrs.StoreType = xkmssdk.StoreType(string(config.BackendTypeSoftware))
	_, err := dbxkms.GenerateSymmetricKey(ctx, "software", attrs)
	require.NoError(t, err)

	factory, err := NewBackendFactory(&BackendFactoryConfig{Logger: &mockLogger{}, KeyNameStore: store})
	require.NoError(t, err)

	err = factory.InitializeFromXKMS(ctx, nil)
	require.ErrorIs(t, err, ErrLegacyXKMSKeyName)

	var legacyErr *LegacyXKMSKeyNamesError
	require.True(t, errors.As(err, &legacyErr))
	require.Contains(t, legacyErr.Names, legacy)

	_, err = store.Get(ctx, xkmsKeyNameMappingMarker)
	require.ErrorIs(t, err, xkmssdk.ErrStorageNotFound, "a refused open writes no marker")
}

func TestInitializeFromXKMS_FreshStoreWritesMarkerAndAcceptsEncodedKeys(t *testing.T) {
	ctx := context.Background()
	store := newKeyNameGuardXKMS(t)

	first, err := NewBackendFactory(&BackendFactoryConfig{Logger: &mockLogger{}, KeyNameStore: store})
	require.NoError(t, err)
	require.NoError(t, first.InitializeFromXKMS(ctx, nil))

	_, err = store.Get(ctx, xkmsKeyNameMappingMarker)
	require.NoError(t, err, "a clean open writes the marker")

	kb := newRealXKMSBackendForTest(t)
	_, err = kb.CreateKey(ctx, kb.algorithmToKeyAttributes("tenant/acme/k", AlgAES256GCM))
	require.NoError(t, err)

	second, err := NewBackendFactory(&BackendFactoryConfig{Logger: &mockLogger{}, KeyNameStore: store})
	require.NoError(t, err)
	require.NoError(t, second.InitializeFromXKMS(ctx, nil), "a marked store opens without a scan")

	keys, _, err := kb.ListKeys(ctx, "", 0)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Equal(t, "tenant/acme/k", keys[0].KeyID)
}

// TestCheckXKMSKeyNames_RemovedKeyDirHoldsNoKeys runs the scan over a file
// key store whose directory is gone: a missing directory holds no keys, so
// the scan passes and marks the store instead of failing the open.
func TestCheckXKMSKeyNames_RemovedKeyDirHoldsNoKeys(t *testing.T) {
	ctx := context.Background()

	dbxkms.Reset()
	t.Cleanup(dbxkms.Reset)

	keyDir := filepath.Join(t.TempDir(), "ca-keys")
	fileStorage, err := xkmssdk.NewFileStorageBackend(keyDir)
	require.NoError(t, err)
	certStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)

	swBackend, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{
		KeyStorage: fileStorage,
	})
	require.NoError(t, err)
	ks, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     swBackend,
		CertStorage: certStorage,
	})
	require.NoError(t, err)
	require.NoError(t, dbxkms.Initialize(&dbxkms.Config{
		DefaultBackend: "software",
		Backends:       map[string]xkmssdk.ServiceBackend{"software": ks},
		Logger:         &mockLogger{},
	}))

	require.NoError(t, os.RemoveAll(keyDir))

	marker, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)
	require.NoError(t, checkXKMSKeyNames(ctx, marker, []string{"software"}),
		"a removed key directory holds no keys")

	_, err = marker.Get(ctx, xkmsKeyNameMappingMarker)
	require.NoError(t, err, "a store with no keys is marked clean")
}
