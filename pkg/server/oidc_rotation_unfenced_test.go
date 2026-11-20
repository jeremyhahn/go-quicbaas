package server

import (
	"context"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestOIDCSigningStore_UnfencedWritesAreRefused proves the storage seam is
// the one gate on oidc-replicated rotation state: without the rotation fence
// in the context, an unconditional Put of the version record, and a Put or a
// Delete of key material, are refused with *OIDCRotationUnfencedError and
// leave the store as it was, while a PutIfAbsent of an absent record or key
// (first creation) passes.
func TestOIDCSigningStore_UnfencedWritesAreRefused(t *testing.T) {
	ctx := context.Background()
	kv, tx := oidcReplicatedKV(t)
	versions, err := xkmssdk.NewStoragePrefixBackend(kv, oidcSigningVersionsPrefix)
	require.NoError(t, err)
	router := newKeyVersionRouter(xkmssdk.NewStorageMemory())
	router.setReplicated(versions, tx)
	keys, err := newOIDCKeyMaterialStore(kv, tx)
	require.NoError(t, err)

	record := "system" + oidcSigningVersionSegment + iam.OIDCSigningKeyCN
	ok, err := router.PutIfAbsent(ctx, record, []byte("r1"))
	require.NoError(t, err)
	require.True(t, ok, "first creation of the version record passes")
	ok, err = keys.PutIfAbsent(ctx, oidcSystemKey("", "k"), []byte("m1"))
	require.NoError(t, err)
	require.True(t, ok, "first creation of key material passes")
	guarded, err := guardedOIDCStore(kv)
	require.NoError(t, err)
	guardedVersions, err := guardedOIDCStore(versions)
	require.NoError(t, err)
	lock := newOIDCRotationLock(guarded, guardedVersions)
	planted, release, err := lock(ctx, "", iam.OIDCSigningKeyCN)
	require.NoError(t, err)
	require.NoError(t, keys.Put(planted, oidcSystemKey("v2", "k"), []byte("m2")))
	release()
	before := snapshotKV(t, kv)

	var unfenced *OIDCRotationUnfencedError
	require.ErrorAs(t, router.Put(ctx, record, []byte("r2")), &unfenced)
	require.Equal(t, record, unfenced.Key)
	require.ErrorAs(t, router.Put(ctx, "system"+oidcSigningVersionSegment+"absent", []byte("r")), &unfenced)
	require.ErrorAs(t, keys.Put(ctx, oidcSystemKey("v2", "k"), []byte("other")), &unfenced)
	require.ErrorAs(t, keys.Put(ctx, oidcSystemKey("v3", "k"), []byte("m3")), &unfenced)
	_, err = keys.PutIfAbsent(ctx, oidcSystemKey("v4", "k"), []byte("m4"))
	require.ErrorAs(t, err, &unfenced, "a rotation version is created only under the fence")
	require.ErrorAs(t, keys.Delete(ctx, oidcSystemKey("v2", "k")), &unfenced)
	require.Equal(t, before, snapshotKV(t, kv), "a refused write changes nothing")

	// The signing key's first creation through Put passes; an overwrite of
	// it does not.
	require.NoError(t, keys.Put(ctx, "meta/k", []byte("a1")))
	require.ErrorAs(t, keys.Put(ctx, "meta/k", []byte("a2")), &unfenced)
	got, err := keys.Get(ctx, "meta/k")
	require.NoError(t, err)
	require.Equal(t, []byte("a1"), got)

	// A record outside oidc-replicated is not rotation state of this store.
	require.NoError(t, router.Put(ctx, "system/key-versions/software/other", []byte("x")))

	// Under the fence the rotation's writes pass.
	fenced, unlock, err := lock(ctx, "", iam.OIDCSigningKeyCN)
	require.NoError(t, err)
	t.Cleanup(unlock)
	require.NoError(t, keys.Put(fenced, oidcSystemKey("v3", "k"), []byte("m3")))
	require.NoError(t, router.Put(fenced, record, []byte("r2")))
	got, err = versions.Get(ctx, record)
	require.NoError(t, err)
	require.Equal(t, []byte("r2"), got)
}
