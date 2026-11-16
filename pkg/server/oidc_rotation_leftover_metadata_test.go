package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	xkmsstorage "github.com/jeremyhahn/go-xkms/pkg/storage"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// errMetadataDeleteInterrupted is the failure the delete hook injects
// between a key's blob delete and its metadata delete.
var errMetadataDeleteInterrupted = errors.New("test: metadata delete interrupted")

// interruptOIDCMetadataDeletes fails every fenced delete of a metadata
// record until the returned restore runs, so go-xkms's DeleteKey removes the blob and
// leaves the metadata behind.
func interruptOIDCMetadataDeletes(t *testing.T) func() {
	t.Helper()
	prev := oidcMaterialDeleteHook
	oidcMaterialDeleteHook = func(key string) error {
		if strings.HasPrefix(key, "meta/") {
			return errMetadataDeleteInterrupted
		}
		return nil
	}
	restore := func() { oidcMaterialDeleteHook = prev }
	t.Cleanup(restore)
	return restore
}

// holdOIDCSigningRotation takes the system signing key's rotation lease as
// a new holder and returns its fenced context and its release.
func holdOIDCSigningRotation(t *testing.T, kv xkmssdk.StorageBackend) (context.Context, func()) {
	t.Helper()
	guarded, err := guardedOIDCStore(kv)
	require.NoError(t, err)
	versions, err := xkmssdk.NewStoragePrefixBackend(kv, oidcSigningVersionsPrefix)
	require.NoError(t, err)
	guardedVersions, err := guardedOIDCStore(versions)
	require.NoError(t, err)
	ctx, unlock, err := newOIDCRotationLock(guarded, guardedVersions)(context.Background(), "", iam.OIDCSigningKeyCN)
	require.NoError(t, err)
	t.Cleanup(unlock)
	return ctx, unlock
}

// oidcLeftoverRotation holds the system signing key's rotation lease as a
// third holder, generates the key the next rotation names (--v2) and
// deletes it with the metadata delete interrupted: the store keeps that
// key's metadata record without its blob. The holder then releases the
// lease. It returns the key's attributes.
func oidcLeftoverRotation(t *testing.T, kv xkmssdk.StorageBackend, tx transport.TransactionClient) *xkmssdk.KeyAttributes {
	t.Helper()
	ctx, unlock := holdOIDCSigningRotation(t, kv)
	defer unlock()

	backend, err := newOIDCSigningServiceBackend(kv, tx)
	require.NoError(t, err)
	listed, err := backend.ListKeys(ctx)
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
	_, err = backend.GenerateECDSA(ctx, &next)
	require.NoError(t, err)

	restore := interruptOIDCMetadataDeletes(t)
	require.ErrorIs(t, backend.DeleteKey(ctx, &next), errMetadataDeleteInterrupted)
	restore()
	store := snapshotKV(t, kv)
	require.NotContains(t, store, oidcSigningKeysPrefix+xkmsstorage.KeyPath(next.ID()), "the blob is deleted")
	require.Contains(t, store, oidcSigningKeysPrefix+"meta/"+next.ID(), "the metadata is left behind")
	return &next
}

func TestOIDCSigningStore_RotationRecoversLeftoverMetadata(t *testing.T) {
	for _, backend := range oidcRotationBackends {
		t.Run(backend, func(t *testing.T) {
			t.Cleanup(dbxkms.Reset)
			kv, tx := oidcReplicatedKVOn(t, backend)
			a := bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv, tx)
			v1, _, _ := a.Current()

			next := oidcLeftoverRotation(t, kv, tx)

			// Another node rotates: it writes the --v2 blob and replaces the
			// metadata the interrupted delete left.
			b := bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv, tx)
			require.NoError(t, b.Rotate())
			v2, _, _ := b.Current()
			require.NotEqual(t, v1, v2)
			pubB, err := b.PublicForKID(v2)
			require.NoError(t, err)

			// The record, the blob and the metadata agree: a node booted now
			// signs with v2, loads its key, and lists it with its metadata.
			store := snapshotKV(t, kv)
			require.Contains(t, store, oidcSigningKeysPrefix+xkmsstorage.KeyPath(next.ID()))
			require.Contains(t, store, oidcSigningKeysPrefix+"meta/"+next.ID())
			c := bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv, tx)
			kidC, _, _ := c.Current()
			require.Equal(t, v2, kidC)
			pubC, err := c.PublicForKID(v2)
			require.NoError(t, err)
			require.Equal(t, pubB, pubC)
			listing, err := newOIDCSigningServiceBackend(kv, tx)
			require.NoError(t, err)
			listed, err := listing.ListKeys(context.Background())
			require.NoError(t, err)
			var found bool
			for _, attrs := range listed {
				found = found || attrs.ID() == next.ID()
			}
			require.True(t, found, "the rotated key is listed from its metadata")
		})
	}
}

func TestOIDCSigningStore_LeftoverMetadataReplacedOnlyByBlobWriterUnderLease(t *testing.T) {
	for _, backend := range oidcRotationBackends {
		t.Run(backend, func(t *testing.T) {
			t.Cleanup(dbxkms.Reset)
			kv, tx := oidcReplicatedKVOn(t, backend)
			bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv, tx)
			next := oidcLeftoverRotation(t, kv, tx)
			ctx, _ := holdOIDCSigningRotation(t, kv)
			keys, err := newOIDCKeyMaterialStore(kv, tx)
			require.NoError(t, err)
			blobKey := xkmsstorage.KeyPath(next.ID())
			metaKey := "meta/" + next.ID()
			var fenced *OIDCRotationFencedError

			// A holder that did not write the key's blob in this rotation
			// cannot replace its metadata, even under its lease.
			before := snapshotKV(t, kv)
			require.ErrorAs(t, keys.Put(ctx, metaKey, []byte("no blob written")), &fenced)
			require.Equal(t, before, snapshotKV(t, kv))

			// The holder writes the blob, then loses its lease: it still
			// cannot replace the metadata.
			require.NoError(t, keys.Put(ctx, blobKey, []byte("blob")))
			guarded, err := guardedOIDCStore(kv)
			require.NoError(t, err)
			leaseKey := oidcRotationLeasePrefix + "system/" + iam.OIDCSigningKeyCN
			held, err := guarded.Get(context.Background(), leaseKey)
			require.NoError(t, err)
			_, owner, _ := strings.Cut(string(held), " ")
			swapped, _, err := guarded.CompareAndSwap(context.Background(), leaseKey, held, oidcRotationLeaseValue(owner, -time.Second))
			require.NoError(t, err)
			require.True(t, swapped)
			before = snapshotKV(t, kv)
			require.ErrorAs(t, keys.Put(ctx, metaKey, []byte("lease lost")), &fenced)
			require.Equal(t, before, snapshotKV(t, kv))
		})
	}
}
