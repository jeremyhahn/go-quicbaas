//go:build !integration

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

// A stale holder resolves the v1 record under its lease and is paused past
// the lease; the successor takes the lease over, generates K--v2 and commits
// the v2 record. The stale holder then lists the material, sees the
// successor's K--v2 bytes and, against the v1 record it resolved, deletes
// K--v2 as an orphan. The delete is refused with the fenced error and the
// successor's committed key stays in place: bytes alone no longer fence it.
func TestOIDCSigningStore_StaleHolderDeleteKeepsSuccessorsCommittedKey(t *testing.T) {
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

	backendA, err := newOIDCSigningServiceBackend(kv, tx)
	require.NoError(t, err)
	listed, err := backendA.ListKeys(ctxA)
	require.NoError(t, err)
	var orphan *xkmssdk.KeyAttributes
	for _, attrs := range listed {
		if attrs.CN == iam.OIDCSigningKeyCN+"--v2" {
			orphan = attrs
		}
	}
	require.NotNil(t, orphan, "the successor's K--v2 is in the shared store")

	var fenced *OIDCRotationFencedError
	require.ErrorAs(t, backendA.DeleteKey(ctxA, orphan), &fenced)
	require.Equal(t, afterB, snapshotKV(t, kv), "the stale holder deleted the successor's committed key")

	c := bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv, tx)
	kidC, _, _ := c.Current()
	require.Equal(t, v2, kidC)
	pubC, err := c.PublicForKID(context.Background(), v2)
	require.NoError(t, err)
	require.Equal(t, pubB, pubC)
}

// Each of the three preconditions of a fenced material delete refuses it on
// its own: the lease, the version record (present or absent) and the bytes.
func TestOIDCKeyMaterialStore_FencedDeleteChecksLeaseRecordAndBytes(t *testing.T) {
	name := oidcSystemKey("", "oidc-test-material")
	record := "system" + oidcSigningVersionSegment + iam.OIDCSigningKeyCN
	leaseKey := oidcRotationLeasePrefix + "system/" + iam.OIDCSigningKeyCN
	observed := []byte("observed bytes")
	for _, tc := range []struct {
		name     string
		record   []byte
		change   func(t *testing.T, kv, versions xkmssdk.StorageBackend)
		deleted  bool
		leftWith []byte
	}{
		{name: "all hold with a record", record: []byte("v1 record"), deleted: true},
		{name: "all hold with no record", deleted: true},
		{
			name: "lease taken over", record: []byte("v1 record"), leftWith: observed,
			change: func(t *testing.T, kv, _ xkmssdk.StorageBackend) {
				require.NoError(t, kv.Put(context.Background(), leaseKey, oidcRotationLeaseValue("successor", time.Minute)))
			},
		},
		{
			name: "lease released", record: []byte("v1 record"), leftWith: observed,
			change: func(t *testing.T, kv, _ xkmssdk.StorageBackend) {
				require.NoError(t, kv.Delete(context.Background(), leaseKey))
			},
		},
		{
			name: "record committed by another holder", record: []byte("v1 record"), leftWith: observed,
			change: func(t *testing.T, _, versions xkmssdk.StorageBackend) {
				require.NoError(t, versions.Put(context.Background(), record, []byte("v2 record")))
			},
		},
		{
			name: "record created by another holder", leftWith: observed,
			change: func(t *testing.T, _, versions xkmssdk.StorageBackend) {
				require.NoError(t, versions.Put(context.Background(), record, []byte("v2 record")))
			},
		},
		{
			name: "bytes replaced", record: []byte("v1 record"), leftWith: []byte("successor bytes"),
			change: func(t *testing.T, kv, _ xkmssdk.StorageBackend) {
				require.NoError(t, kv.Put(context.Background(), oidcSigningKeysPrefix+name, []byte("successor bytes")))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kv, tx := oidcReplicatedKV(t)
			guarded, err := guardedOIDCStore(kv)
			require.NoError(t, err)
			versions, err := xkmssdk.NewStoragePrefixBackend(kv, oidcSigningVersionsPrefix)
			require.NoError(t, err)
			guardedVersions, err := guardedOIDCStore(versions)
			require.NoError(t, err)
			if tc.record != nil {
				require.NoError(t, versions.Put(context.Background(), record, tc.record))
			}
			require.NoError(t, kv.Put(context.Background(), oidcSigningKeysPrefix+name, observed))

			ctx, unlock, err := newOIDCRotationLock(guarded, guardedVersions)(context.Background(), "", iam.OIDCSigningKeyCN)
			require.NoError(t, err)
			t.Cleanup(unlock)
			keys, err := newOIDCKeyMaterialStore(kv, tx)
			require.NoError(t, err)
			got, err := keys.Get(ctx, name)
			require.NoError(t, err)
			require.Equal(t, observed, got)
			if tc.change != nil {
				tc.change(t, kv, versions)
			}

			err = keys.Delete(ctx, name)
			exists, existsErr := kv.Exists(context.Background(), oidcSigningKeysPrefix+name)
			require.NoError(t, existsErr)
			if tc.deleted {
				require.NoError(t, err)
				require.False(t, exists)
				return
			}
			var fenced *OIDCRotationFencedError
			require.ErrorAs(t, err, &fenced)
			left, getErr := kv.Get(context.Background(), oidcSigningKeysPrefix+name)
			require.NoError(t, getErr)
			require.Equal(t, tc.leftWith, left)
		})
	}
}

// A store without a transaction plane cannot fence a material delete, so
// setup refuses it rather than fall back to a bytes-only delete.
func TestOIDCKeyMaterialStore_RequiresTransactions(t *testing.T) {
	_, err := newOIDCKeyMaterialStore(xkmssdk.NewStorageMemory(), nil)
	var missing *OIDCSigningStoreTransactionsError
	require.ErrorAs(t, err, &missing)

	s := &Server{config: &config.Config{}}
	require.ErrorAs(t, s.useReplicatedOIDCSigningStore(xkmssdk.NewStorageMemory(), nil), &missing)
}
