package server

import (
	"context"
	"strings"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// oidcLeasedWriteRig is one replicated store with the writers a rotation
// uses under its lease: the version router for the record and the material
// store for the key material, and the rotation lock both are fenced by.
type oidcLeasedWriteRig struct {
	kv       xkmssdk.StorageBackend
	versions xkmssdk.StorageBackend
	guarded  oidcGuardedKV
	router   *keyVersionRouter
	keys     *oidcKeyMaterialStore
	lock     iam.RotationLock
	record   string
}

func newOIDCLeasedWriteRig(t *testing.T) *oidcLeasedWriteRig {
	t.Helper()
	kv, tx := oidcReplicatedKV(t)
	guarded, err := guardedOIDCStore(kv)
	require.NoError(t, err)
	versions, err := xkmssdk.NewStoragePrefixBackend(kv, oidcSigningVersionsPrefix)
	require.NoError(t, err)
	guardedVersions, err := guardedOIDCStore(versions)
	require.NoError(t, err)
	router := newKeyVersionRouter(xkmssdk.NewStorageMemory())
	router.setReplicated(versions, tx)
	keys, err := newOIDCKeyMaterialStore(kv, tx)
	require.NoError(t, err)
	return &oidcLeasedWriteRig{
		kv:       kv,
		versions: versions,
		guarded:  guarded,
		router:   router,
		keys:     keys,
		lock:     newOIDCRotationLock(guarded, guardedVersions),
		record:   "system" + oidcSigningVersionSegment + iam.OIDCSigningKeyCN,
	}
}

// hold takes the rotation lease of the signing key.
func (r *oidcLeasedWriteRig) hold(t *testing.T) context.Context {
	t.Helper()
	ctx, unlock, err := r.lock(context.Background(), "", iam.OIDCSigningKeyCN)
	require.NoError(t, err)
	t.Cleanup(unlock)
	return ctx
}

// takeOver pauses the current holder past its lease, its lease value
// running out unrenewed, and lets a successor take the lease over.
func (r *oidcLeasedWriteRig) takeOver(t *testing.T) context.Context {
	t.Helper()
	leaseKey := oidcRotationLeasePrefix + "system/" + iam.OIDCSigningKeyCN
	held, err := r.guarded.Get(context.Background(), leaseKey)
	require.NoError(t, err)
	_, owner, _ := strings.Cut(string(held), " ")
	swapped, _, err := r.guarded.CompareAndSwap(context.Background(), leaseKey, held, oidcRotationLeaseValue(owner, -time.Second))
	require.NoError(t, err)
	require.True(t, swapped)
	return r.hold(t)
}

// value returns the bytes at a full storage key, or nil when it is absent.
func (r *oidcLeasedWriteRig) value(t *testing.T, key string) []byte {
	t.Helper()
	exists, err := r.kv.Exists(context.Background(), key)
	require.NoError(t, err)
	if !exists {
		return nil
	}
	got, err := r.kv.Get(context.Background(), key)
	require.NoError(t, err)
	return got
}

// A creates K--v2 under its lease and is paused past it. B takes the lease
// over and removes A's K--v2, which no record names. A resumes before B
// commits: A's commit of the v2 record is refused with the fenced error and
// the record is unchanged, so no record names the material B removed.
func TestOIDCSigningStore_StaleHolderRecordCommitAfterSuccessorCleanup(t *testing.T) {
	r := newOIDCLeasedWriteRig(t)
	require.NoError(t, r.versions.Put(context.Background(), r.record, []byte("r1")))

	ctxA := r.hold(t)
	require.NoError(t, r.keys.Put(ctxA, oidcSystemKey("v2", "k"), []byte("a2")))

	ctxB := r.takeOver(t)
	got, err := r.keys.Get(ctxB, oidcSystemKey("v2", "k"))
	require.NoError(t, err)
	require.Equal(t, []byte("a2"), got)
	require.NoError(t, r.keys.Delete(ctxB, oidcSystemKey("v2", "k")))

	var fenced *OIDCRotationFencedError
	require.ErrorAs(t, r.router.Put(ctxA, r.record, []byte("r2")), &fenced)
	require.Equal(t, []byte("r1"), r.value(t, oidcSigningVersionsPrefix+r.record), "the stale holder's record commit landed")
	require.Nil(t, r.value(t, oidcSigningKeysPrefix+oidcSystemKey("v2", "k")))
}

// Every write the fence guards needs the lease in the same transaction:
// each one passes while its writer holds the lease and is refused with the
// fenced error, changing nothing, once the lease was taken over.
func TestOIDCSigningStore_FencedWritesNeedTheLease(t *testing.T) {
	for _, tc := range []struct {
		name   string
		write  func(t *testing.T, r *oidcLeasedWriteRig, ctx context.Context) error
		key    func(r *oidcLeasedWriteRig) string
		before []byte
		after  []byte
	}{
		{
			name: "version-record commit",
			write: func(_ *testing.T, r *oidcLeasedWriteRig, ctx context.Context) error {
				return r.router.Put(ctx, r.record, []byte("r2"))
			},
			key:    func(r *oidcLeasedWriteRig) string { return oidcSigningVersionsPrefix + r.record },
			before: []byte("r1"), after: []byte("r2"),
		},
		{
			name: "material overwrite by Put",
			write: func(_ *testing.T, r *oidcLeasedWriteRig, ctx context.Context) error {
				return r.keys.Put(ctx, oidcSystemKey("", "k"), []byte("a1"))
			},
			key:    func(*oidcLeasedWriteRig) string { return oidcSigningKeysPrefix + oidcSystemKey("", "k") },
			before: []byte("m1"), after: []byte("a1"),
		},
		{
			name: "material creation by Put",
			write: func(_ *testing.T, r *oidcLeasedWriteRig, ctx context.Context) error {
				return r.keys.Put(ctx, oidcSystemKey("v2", "k"), []byte("a2"))
			},
			key:   func(*oidcLeasedWriteRig) string { return oidcSigningKeysPrefix + oidcSystemKey("v2", "k") },
			after: []byte("a2"),
		},
		{
			name: "material creation by PutIfAbsent",
			write: func(t *testing.T, r *oidcLeasedWriteRig, ctx context.Context) error {
				created, err := r.keys.PutIfAbsent(ctx, oidcSystemKey("v2", "k"), []byte("a2"))
				if err == nil {
					require.True(t, created)
				}
				return err
			},
			key:   func(*oidcLeasedWriteRig) string { return oidcSigningKeysPrefix + oidcSystemKey("v2", "k") },
			after: []byte("a2"),
		},
		{
			name: "material delete",
			write: func(_ *testing.T, r *oidcLeasedWriteRig, ctx context.Context) error {
				return r.keys.Delete(ctx, oidcSystemKey("", "k"))
			},
			key:    func(*oidcLeasedWriteRig) string { return oidcSigningKeysPrefix + oidcSystemKey("", "k") },
			before: []byte("m1"),
		},
	} {
		for _, takenOver := range []bool{false, true} {
			name := tc.name + "/lease held"
			if takenOver {
				name = tc.name + "/lease taken over"
			}
			t.Run(name, func(t *testing.T) {
				r := newOIDCLeasedWriteRig(t)
				require.NoError(t, r.versions.Put(context.Background(), r.record, []byte("r1")))
				require.NoError(t, r.kv.Put(context.Background(), oidcSigningKeysPrefix+oidcSystemKey("", "k"), []byte("m1")))
				ctxA := r.hold(t)
				got, err := r.keys.Get(ctxA, oidcSystemKey("", "k"))
				require.NoError(t, err)
				require.Equal(t, []byte("m1"), got)
				if !takenOver {
					require.NoError(t, tc.write(t, r, ctxA))
					require.Equal(t, tc.after, r.value(t, tc.key(r)))
					return
				}
				r.takeOver(t)
				var fenced *OIDCRotationFencedError
				require.ErrorAs(t, tc.write(t, r, ctxA), &fenced)
				require.Equal(t, tc.before, r.value(t, tc.key(r)), "a writer whose lease was taken over wrote")
			})
		}
	}
}

// A fenced PutIfAbsent of material that already exists reports it was not
// created and leaves the bytes, while its writer holds the lease.
func TestOIDCSigningStore_FencedPutIfAbsentOfExistingMaterial(t *testing.T) {
	r := newOIDCLeasedWriteRig(t)
	require.NoError(t, r.kv.Put(context.Background(), oidcSigningKeysPrefix+oidcSystemKey("v2", "k"), []byte("peer")))
	ctx := r.hold(t)
	created, err := r.keys.PutIfAbsent(ctx, oidcSystemKey("v2", "k"), []byte("mine"))
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, []byte("peer"), r.value(t, oidcSigningKeysPrefix+oidcSystemKey("v2", "k")))
}
