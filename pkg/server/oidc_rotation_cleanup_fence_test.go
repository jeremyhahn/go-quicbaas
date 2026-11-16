package server

import (
	"context"
	"strings"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// TestOIDCSigningStore_StaleHolderCleanupKeepsPeerKey proves a stale
// holder's cleanup cannot remove a peer's committed key. Node A, with its
// own software backend over the shared store, takes the rotation lease and
// generates the next version, then is paused past its lease. Node B takes
// over: it replaces A's orphan and commits its own next version. When A
// resumes, its record commit is fenced and its backend's DeleteKey of the
// version it generated (go-xkms's cleanup of an unrecorded key) is fenced
// too, so B's key material stays, the record names B's key, a fresh node
// loads it, and later rotations from either node succeed.
func TestOIDCSigningStore_StaleHolderCleanupKeepsPeerKey(t *testing.T) {
	t.Cleanup(dbxkms.Reset)
	kv, tx := oidcReplicatedKV(t)
	a := bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv, tx)
	b := bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv, tx)
	v1, _, _ := a.Current()

	guarded, err := guardedOIDCStore(kv)
	require.NoError(t, err)
	versions, err := xkmssdk.NewStoragePrefixBackend(kv, oidcSigningVersionsPrefix)
	require.NoError(t, err)
	guardedVersions, err := guardedOIDCStore(versions)
	require.NoError(t, err)
	ctxA, unlockA, err := newOIDCRotationLock(guarded, guardedVersions)(context.Background(), "", iam.OIDCSigningKeyCN)
	require.NoError(t, err)
	t.Cleanup(unlockA)

	backendA, err := newOIDCSigningServiceBackend(kv, tx)
	require.NoError(t, err)
	listed, err := backendA.ListKeys(ctxA)
	require.NoError(t, err)
	var current *xkmssdk.KeyAttributes
	for _, attrs := range listed {
		if attrs.CN == iam.OIDCSigningKeyCN {
			current = attrs
		}
	}
	require.NotNil(t, current, "the signing key is in the shared store")
	next := *current
	next.CN = iam.OIDCSigningKeyCN + "--v2"
	_, err = backendA.GenerateECDSA(ctxA, &next)
	require.NoError(t, err)

	// A is paused past its lease: its lease value runs out unrenewed.
	leaseKey := oidcRotationLeasePrefix + "system/" + iam.OIDCSigningKeyCN
	held, err := guarded.Get(context.Background(), leaseKey)
	require.NoError(t, err)
	_, owner, _ := strings.Cut(string(held), " ")
	swapped, _, err := guarded.CompareAndSwap(context.Background(), leaseKey, held, oidcRotationLeaseValue(owner, -time.Second))
	require.NoError(t, err)
	require.True(t, swapped)

	require.NoError(t, b.Rotate())
	v2, _, _ := b.Current()
	require.NotEqual(t, v1, v2)
	pubB, err := b.PublicForKID(context.Background(), v2)
	require.NoError(t, err)
	afterB := snapshotKV(t, kv)

	router := newKeyVersionRouter(xkmssdk.NewStorageMemory())
	router.setReplicated(versions, tx)
	record := "system" + oidcSigningVersionSegment + iam.OIDCSigningKeyCN
	var fenced *OIDCRotationFencedError
	require.ErrorAs(t, router.Put(ctxA, record, []byte("node A's record")), &fenced)
	require.ErrorAs(t, backendA.DeleteKey(ctxA, &next), &fenced)
	require.Equal(t, afterB, snapshotKV(t, kv), "the stale holder's cleanup changed the store")

	c := bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv, tx)
	kidC, _, _ := c.Current()
	require.Equal(t, v2, kidC)
	pubC, err := c.PublicForKID(context.Background(), v2)
	require.NoError(t, err)
	require.Equal(t, pubB, pubC)

	require.NoError(t, a.Refresh())
	require.NoError(t, a.Rotate())
	v3, _, _ := a.Current()
	require.NotEqual(t, v2, v3)
	require.NoError(t, b.Refresh())
	require.NoError(t, b.Rotate())
	v4, _, _ := b.Current()
	require.NotEqual(t, v3, v4)
}

// TestOIDCSigningStore_FencedDeleteOfUnseenMaterialIsRefused proves that
// under the fence a Delete of key material removes only bytes the holder
// observed or wrote: a key it never saw, or one that changed since it read
// it, is refused with *OIDCRotationFencedError and left in place.
func TestOIDCSigningStore_FencedDeleteOfUnseenMaterialIsRefused(t *testing.T) {
	ctx := context.Background()
	kv, tx := oidcReplicatedKV(t)
	keys, err := newOIDCKeyMaterialStore(kv, tx)
	require.NoError(t, err)
	_, err = keys.PutIfAbsent(ctx, oidcSystemKey("", "unseen"), []byte("peer"))
	require.NoError(t, err)
	_, err = keys.PutIfAbsent(ctx, oidcSystemKey("", "changed"), []byte("old"))
	require.NoError(t, err)

	var refused *OIDCRotationFencedError
	leaseless := context.WithValue(ctx, oidcVersionFenceKey{}, &oidcVersionFence{key: "record"})
	_, err = keys.Get(leaseless, oidcSystemKey("", "unseen"))
	require.NoError(t, err)
	require.ErrorAs(t, keys.Delete(leaseless, oidcSystemKey("", "unseen")), &refused, "a fence that holds no lease cannot delete")

	guarded, err := guardedOIDCStore(kv)
	require.NoError(t, err)
	versions, err := xkmssdk.NewStoragePrefixBackend(kv, oidcSigningVersionsPrefix)
	require.NoError(t, err)
	guardedVersions, err := guardedOIDCStore(versions)
	require.NoError(t, err)
	fenced, unlock, err := newOIDCRotationLock(guarded, guardedVersions)(ctx, "", iam.OIDCSigningKeyCN)
	require.NoError(t, err)
	t.Cleanup(unlock)
	require.ErrorAs(t, keys.Delete(fenced, oidcSystemKey("", "unseen")), &refused)
	_, err = keys.Get(fenced, oidcSystemKey("", "changed"))
	require.NoError(t, err)
	require.NoError(t, kv.Put(ctx, oidcSigningKeysPrefix+oidcSystemKey("", "changed"), []byte("new")))
	require.ErrorAs(t, keys.Delete(fenced, oidcSystemKey("", "changed")), &refused)
	require.ErrorAs(t, keys.Put(fenced, oidcSystemKey("", "changed"), []byte("mine")), &refused)
	got, err := keys.Get(ctx, oidcSystemKey("", "changed"))
	require.NoError(t, err)
	require.Equal(t, []byte("new"), got)
	got, err = keys.Get(ctx, oidcSystemKey("", "unseen"))
	require.NoError(t, err)
	require.Equal(t, []byte("peer"), got)

	require.NoError(t, keys.Put(fenced, oidcSystemKey("v2", "mine"), []byte("m")))
	require.NoError(t, keys.Delete(fenced, oidcSystemKey("v2", "mine")))
	exists, err := keys.Exists(ctx, oidcSystemKey("v2", "mine"))
	require.NoError(t, err)
	require.False(t, exists)
}
