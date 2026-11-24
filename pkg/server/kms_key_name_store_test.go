package server

import (
	"context"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"
)

// Two nodes sharing a KMS backend share one replicated keyspace: a record
// node A writes after the switch resolves on node B, whose own local store
// never saw it.
func TestKMSKeyNameRouter_RecordsAreSharedAcrossNodes(t *testing.T) {
	ctx := context.Background()
	replicated := xkmssdk.NewStorageMemory()
	a := newKMSKeyNameRouter(xkmssdk.NewStorageMemory())
	b := newKMSKeyNameRouter(xkmssdk.NewStorageMemory())
	a.setReplicated(replicated)
	b.setReplicated(replicated)

	require.NoError(t, a.Put(ctx, "owners/software/k", []byte("tenant/acme/k")))
	got, err := b.Get(ctx, "owners/software/k")
	require.NoError(t, err, "node B must resolve the record node A wrote")
	require.Equal(t, "tenant/acme/k", string(got))

	stored, err := b.PutIfAbsent(ctx, "owners/software/k", []byte("tenant/other/k"))
	require.NoError(t, err)
	require.False(t, stored, "node B must see node A's claim")
}

// A record an earlier version wrote node-locally stays readable after the
// switch and is copied into the replicated store, so other nodes see it too;
// deleting it removes both copies.
func TestKMSKeyNameRouter_LegacyLocalRecordsAreAdopted(t *testing.T) {
	ctx := context.Background()
	local := xkmssdk.NewStorageMemory()
	require.NoError(t, local.Put(ctx, "owners/software/k", []byte("tenant/acme/k")))
	replicated := xkmssdk.NewStorageMemory()
	r := newKMSKeyNameRouter(local)
	r.setReplicated(replicated)

	got, err := r.Get(ctx, "owners/software/k")
	require.NoError(t, err)
	require.Equal(t, "tenant/acme/k", string(got))
	copied, err := replicated.Get(ctx, "owners/software/k")
	require.NoError(t, err, "the legacy record must be copied to the replicated store")
	require.Equal(t, "tenant/acme/k", string(copied))

	require.NoError(t, r.Delete(ctx, "owners/software/k"))
	_, err = r.Get(ctx, "owners/software/k")
	require.True(t, isKeyNameNotFound(err), "a deleted record must not resurface from the local store: %v", err)
	require.True(t, isKeyNameNotFound(r.Delete(ctx, "owners/software/k")))
}

func TestKMSKeyNameRouter_CompareAndDeleteForwards(t *testing.T) {
	ctx := context.Background()
	const key = "owners/software/k"
	local := xkmssdk.NewStorageMemory()
	replicated := xkmssdk.NewStorageMemory()
	r := newKMSKeyNameRouter(local)

	// Local only: a different value is a guarded miss, the same one deletes.
	require.NoError(t, local.Put(ctx, key, []byte("tenant/acme/k")))
	deleted, err := r.CompareAndDelete(ctx, key, []byte("tenant/acme/other"))
	require.NoError(t, err)
	require.False(t, deleted)
	deleted, err = r.CompareAndDelete(ctx, key, []byte("tenant/acme/k"))
	require.NoError(t, err)
	require.True(t, deleted)

	// Replicated: the record and its legacy copy both go.
	r.setReplicated(replicated)
	require.NoError(t, local.Put(ctx, key, []byte("tenant/acme/k")))
	require.NoError(t, replicated.Put(ctx, key, []byte("tenant/acme/k")))
	deleted, err = r.CompareAndDelete(ctx, key, []byte("tenant/acme/other"))
	require.NoError(t, err)
	require.False(t, deleted)
	_, err = replicated.Get(ctx, key)
	require.NoError(t, err, "a guarded miss must leave the record")
	deleted, err = r.CompareAndDelete(ctx, key, []byte("tenant/acme/k"))
	require.NoError(t, err)
	require.True(t, deleted)
	_, err = r.Get(ctx, key)
	require.True(t, isKeyNameNotFound(err), "a dropped record must not resurface from the local store: %v", err)
}

// Before the service has its datastore the router serves the local store.
func TestKMSKeyNameRouter_LocalUntilReplicated(t *testing.T) {
	ctx := context.Background()
	local := xkmssdk.NewStorageMemory()
	r := newKMSKeyNameRouter(local)
	require.NoError(t, r.Put(ctx, "owners/software/k", []byte("v")))
	ok, err := local.Exists(ctx, "owners/software/k")
	require.NoError(t, err)
	require.True(t, ok)

	s := &Server{kmsKeyNames: r}
	require.NoError(t, s.useReplicatedKMSKeyNames(), "no datastore yet: nothing to switch")
	primary, legacy := r.stores()
	require.Equal(t, xkmssdk.StorageBackend(local), primary)
	require.Nil(t, legacy)
}
