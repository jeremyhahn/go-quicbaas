package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
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

// requireOIDCKeysAgree checks that every key's blob in the store has its
// metadata record and every metadata record its blob.
// errOIDCResponseLost is the error oidcLostResponseTx returns in place of a
// committed transaction's response.
var errOIDCResponseLost = errors.New("test: transaction response lost")

// oidcLostResponseTx loses the response of the first committed transaction
// that puts key: the write is applied, and the caller gets an error.
type oidcLostResponseTx struct {
	transport.TransactionClient
	key  string
	lost bool
}

func (l *oidcLostResponseTx) ExecuteTransaction(ctx context.Context, req *transport.TransactionRequest, opts ...transport.WriteOption) (*transport.TransactionResponse, error) {
	resp, err := l.TransactionClient.ExecuteTransaction(ctx, req, opts...)
	if err != nil || resp == nil || !resp.Succeeded || l.lost {
		return resp, err
	}
	for _, op := range req.SuccessOps {
		if op.Type == transport.OpPut && string(op.Key) == l.key {
			l.lost = true
			return nil, errOIDCResponseLost
		}
	}
	return resp, nil
}

func requireOIDCKeysAgree(t *testing.T, store map[string]string) {
	t.Helper()
	metaPrefix := oidcSigningKeysPrefix + oidcKeyMetadataPrefix
	blobPrefix := oidcSigningKeysPrefix + "keys/" // xkmsstorage.KeyPath: keys/{id}.key
	for k := range store {
		if id, ok := strings.CutPrefix(k, metaPrefix); ok {
			require.Contains(t, store, oidcSigningKeysPrefix+xkmsstorage.KeyPath(id), "metadata %s has its blob", id)
		}
		if rest, ok := strings.CutPrefix(k, blobPrefix); ok {
			id := strings.TrimSuffix(rest, ".key")
			require.Contains(t, store, metaPrefix+id, "blob %s has its metadata", id)
		}
	}
}

// requireOIDCRecordAgrees boots a node on the shared store and checks that
// it signs with kid and loads kid's public key from its blob.
func requireOIDCRecordAgrees(t *testing.T, kv xkmssdk.StorageBackend, tx transport.TransactionClient, kid string) {
	t.Helper()
	n := bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv, tx)
	current, _, _ := n.Current()
	require.Equal(t, kid, current)
	_, err := n.PublicForKID(kid)
	require.NoError(t, err)
}

func TestOIDCSigningStore_LeftoverMetadataRecoveryIsIdempotent(t *testing.T) {
	for _, backend := range oidcRotationBackends {
		t.Run(backend+"/repeat", func(t *testing.T) {
			t.Cleanup(dbxkms.Reset)
			kv, tx := oidcReplicatedKVOn(t, backend)
			a := bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv, tx)
			v1, _, _ := a.Current()
			next := oidcLeftoverRotation(t, kv, tx)
			metaKey := oidcKeyMetadataPrefix + next.ID()

			// The holder writes the key's blob, and its first metadata Put
			// replaces the leftover.
			ctx, unlock := holdOIDCSigningRotation(t, kv)
			holder, err := newOIDCSigningServiceBackend(kv, tx)
			require.NoError(t, err)
			_, err = holder.GenerateECDSA(ctx, next)
			require.NoError(t, err)
			afterFirst := snapshotKV(t, kv)
			requireOIDCKeysAgree(t, afterFirst)
			meta, ok := afterFirst[oidcSigningKeysPrefix+metaKey]
			require.True(t, ok, "the recovery wrote the metadata")

			// It applies the same metadata Put again under the same lease,
			// as a retry would: the Put succeeds and changes nothing.
			keys, err := newOIDCKeyMaterialStore(kv, tx)
			require.NoError(t, err)
			require.NoError(t, keys.Put(ctx, metaKey, []byte(meta)))
			require.Equal(t, afterFirst, snapshotKV(t, kv))
			unlock()
			requireOIDCRecordAgrees(t, kv, tx, v1)

			// A full rotation from the peer, then a second one, succeed on
			// the recovered store, and each leaves it in agreement.
			b := bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv, tx)
			require.NoError(t, b.Rotate())
			v2, _, _ := b.Current()
			require.NotEqual(t, v1, v2)
			requireOIDCKeysAgree(t, snapshotKV(t, kv))
			requireOIDCRecordAgrees(t, kv, tx, v2)

			require.NoError(t, b.Rotate())
			v3, _, _ := b.Current()
			require.NotEqual(t, v2, v3)
			requireOIDCKeysAgree(t, snapshotKV(t, kv))
			requireOIDCRecordAgrees(t, kv, tx, v3)
			require.NoError(t, a.Refresh())
			_, err = a.PublicForKID(v3)
			require.NoError(t, err)
		})

		t.Run(backend+"/lost response", func(t *testing.T) {
			t.Cleanup(dbxkms.Reset)
			kv, tx := oidcReplicatedKVOn(t, backend)
			a := bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv, tx)
			v1, _, _ := a.Current()
			next := oidcLeftoverRotation(t, kv, tx)
			blobKey := xkmsstorage.KeyPath(next.ID())
			metaKey := oidcKeyMetadataPrefix + next.ID()
			meta := snapshotKV(t, kv)[oidcSigningKeysPrefix+metaKey]

			// The holder writes the key's blob; its recovery metadata Put
			// commits, but the response is lost, so the holder does not
			// know it wrote.
			ctx, unlock := holdOIDCSigningRotation(t, kv)
			priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			require.NoError(t, err)
			blob, err := x509.MarshalPKCS8PrivateKey(priv)
			require.NoError(t, err)
			lossy, err := newOIDCKeyMaterialStore(kv, &oidcLostResponseTx{TransactionClient: tx, key: oidcSigningKeysPrefix + metaKey})
			require.NoError(t, err)
			require.NoError(t, lossy.Put(ctx, blobKey, blob))
			require.ErrorIs(t, lossy.Put(ctx, metaKey, []byte(meta)), errOIDCResponseLost)
			afterLost := snapshotKV(t, kv)
			requireOIDCKeysAgree(t, afterLost)

			// It retries the same Put twice under the same lease: once not
			// knowing the first committed, once after that retry
			// succeeded. Both succeed and change nothing.
			keys, err := newOIDCKeyMaterialStore(kv, tx)
			require.NoError(t, err)
			for range 2 {
				require.NoError(t, keys.Put(ctx, metaKey, []byte(meta)))
				require.Equal(t, afterLost, snapshotKV(t, kv))
			}
			unlock()
			requireOIDCRecordAgrees(t, kv, tx, v1)
		})
	}
}
