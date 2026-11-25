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
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

// tenantKeyIDOfLength returns tenant/<128-char tenant>/<nameLen-char name>.
func tenantKeyIDOfLength(nameLen int) string {
	return "tenant/" + strings.Repeat("t", 128) + "/" + strings.Repeat("k", nameLen)
}

// requireNoXKMSMaterialOrRecord checks that no AES-256-GCM key and no
// ownership record exist for keyID.
func requireNoXKMSMaterialOrRecord(t *testing.T, ctx context.Context, kb *XKMSBackend, store xkmssdk.StorageBackend, keyID string) {
	t.Helper()
	_, err := dbxkms.GetSymmetricKey(ctx, kb.buildFullKeyID(keyID, AlgAES256GCM))
	require.Error(t, err)
	ok, err := store.Exists(ctx, xkmsKeyOwnerRecord(kb.xkmsKeyIdentity(kb.xkmsKeyAttributes(keyID, AlgAES256GCM))))
	require.NoError(t, err)
	require.False(t, ok)
}

// storableXKMSAttrs returns the go-xkms attributes CreateKey stores the
// AES-256-GCM key keyID under: the encoded name and the backend's store type.
func storableXKMSAttrs(kb *XKMSBackend, keyID string) *xkmssdk.KeyAttributes {
	xattrs := *kb.algorithmToKeyAttributes(keyID, AlgAES256GCM)
	xattrs.CN = xkmsKeyName(keyID)
	if xattrs.StoreType == "" {
		xattrs.StoreType = xkmssdk.StoreType(string(kb.backendType))
	}
	return &xattrs
}

// longestStorableTenantKeyLen returns the longest name length n for which the
// AES-256-GCM key tenantKeyIDOfLength(n) passes checkXKMSKeyStorage, and
// requires that n+1 is refused.
func longestStorableTenantKeyLen(t *testing.T, kb *XKMSBackend) int {
	t.Helper()
	longest := 0
	for n := 1; n <= 60; n++ {
		if kb.checkXKMSKeyStorage(storableXKMSAttrs(kb, tenantKeyIDOfLength(n))) == nil {
			longest = n
		}
	}
	require.Positive(t, longest, "some tenant key name must fit the file store")
	err := kb.checkXKMSKeyStorage(storableXKMSAttrs(kb, tenantKeyIDOfLength(longest+1)))
	require.ErrorIs(t, err, ErrXKMSKeyIDTooLong)
	return longest
}

func TestXKMSBackend_CreateKey_KeyAtStorageLimitIsAccepted(t *testing.T) {
	ctx := context.Background()
	store := newKeyNameGuardXKMS(t)
	kb := newOwnerTestBackend(t, ctx, store)
	id := tenantKeyIDOfLength(longestStorableTenantKeyLen(t, kb))

	_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgAES256GCM))
	require.NoError(t, err)
	ct, err := kb.Encrypt(ctx, id, []byte("at the limit"), nil)
	require.NoError(t, err)
	pt, err := kb.Decrypt(ctx, id, ct, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("at the limit"), pt)

	// _v2 takes the stored name past the limit, so the rotation is refused
	// before any material exists and the key stays at version 1.
	_, err = kb.RotateKey(ctx, id)
	require.ErrorIs(t, err, ErrXKMSKeyIDTooLong)
	requireNoXKMSMaterialOrRecord(t, ctx, kb, store, kb.buildVersionedKeyID(id, 2))
	meta, err := kb.GetKeyMetadata(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 1, meta.Version)
}

func TestXKMSBackend_CreateKey_KeyNameOverLimitIsRefusedBeforeKeyMaterial(t *testing.T) {
	ctx := context.Background()
	store := newKeyNameGuardXKMS(t)
	kb := newOwnerTestBackend(t, ctx, store)

	// 52: the go-xkms name passes checkXKMSKeyName, but the stored file name
	// is too long. 53: the go-xkms name itself is over 255 characters.
	for _, nameLen := range []int{52, 53} {
		id := tenantKeyIDOfLength(nameLen)
		_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgAES256GCM))
		require.ErrorIs(t, err, ErrXKMSKeyIDTooLong, "name length %d", nameLen)
		requireNoXKMSMaterialOrRecord(t, ctx, kb, store, id)
	}
}

func TestXKMSBackend_ImportKey_KeyNameOverLimitIsRefusedBeforeKeyMaterial(t *testing.T) {
	ctx := context.Background()
	store := newKeyNameGuardXKMS(t)
	kb := newOwnerTestBackend(t, ctx, store)
	id := tenantKeyIDOfLength(52)

	_, err := kb.ImportKey(ctx, id, make([]byte, 32), AlgAES256GCM, nil)
	require.ErrorIs(t, err, ErrXKMSKeyIDTooLong)
	requireNoXKMSMaterialOrRecord(t, ctx, kb, store, id)
}

// TestXKMSBackend_FileStorage_KeyAtStorageLimitRoundTripsAndLongerIsRefused
// runs the storage limit on the file store production uses, with the
// ownership records on disk too: the longest storable key works, and a longer
// one is refused as invalid input rather than failing in the filesystem.
func TestXKMSBackend_FileStorage_KeyAtStorageLimitRoundTripsAndLongerIsRefused(t *testing.T) {
	ctx := context.Background()

	dbxkms.Reset()
	t.Cleanup(dbxkms.Reset)

	keyDir := filepath.Join(t.TempDir(), "keys")
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

	kb := newOwnerTestBackend(t, ctx, fileStorage)

	id := tenantKeyIDOfLength(longestStorableTenantKeyLen(t, kb))
	_, err = kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgAES256GCM))
	require.NoError(t, err)
	ct, err := kb.Encrypt(ctx, id, []byte("on disk"), nil)
	require.NoError(t, err)
	_, err = kb.RotateKey(ctx, id)
	require.ErrorIs(t, err, ErrXKMSKeyIDTooLong)
	pt, err := kb.Decrypt(ctx, id, ct, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("on disk"), pt)

	refused := tenantKeyIDOfLength(52)
	_, err = kb.CreateKey(ctx, kb.algorithmToKeyAttributes(refused, AlgAES256GCM))
	require.ErrorIs(t, err, ErrXKMSKeyIDTooLong)
	_, err = kb.ImportKey(ctx, refused, make([]byte, 32), AlgAES256GCM, nil)
	require.ErrorIs(t, err, ErrXKMSKeyIDTooLong)
	// The record of a refused ID cannot be probed on this store (its name is
	// what the filesystem refuses), so the walk below shows it is absent.
	_, err = dbxkms.GetSymmetricKey(ctx, kb.buildFullKeyID(refused, AlgAES256GCM))
	require.Error(t, err)

	refusedName := xkmsKeyName(refused)
	require.NoError(t, filepath.WalkDir(keyDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		require.LessOrEqual(t, len(d.Name()), 255, path)
		require.NotContains(t, d.Name(), refusedName, path)
		return nil
	}))
}
