// Copyright (c) 2025 Jeremy Hahn

package server

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// leaseRaceKV wraps a guarded store with hooks that open the windows two
// nodes race in: onGet runs after a read returns, beforeRemove before a
// delete of either kind reaches the store.
type leaseRaceKV struct {
	xkmssdk.StorageBackend
	onGet        func(key string, value []byte)
	beforeRemove func(key string)
}

func (k *leaseRaceKV) Get(ctx context.Context, key string) ([]byte, error) {
	v, err := k.StorageBackend.Get(ctx, key)
	if err == nil && k.onGet != nil {
		k.onGet(key, v)
	}
	return v, err
}

func (k *leaseRaceKV) Delete(ctx context.Context, key string) error {
	if k.beforeRemove != nil {
		k.beforeRemove(key)
	}
	return k.StorageBackend.Delete(ctx, key)
}

func (k *leaseRaceKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	if k.beforeRemove != nil {
		k.beforeRemove(key)
	}
	return k.StorageBackend.(oidcGuardedKV).CompareAndDelete(ctx, key, expected)
}

func (k *leaseRaceKV) CompareAndSwap(ctx context.Context, key string, expected, newValue []byte) (bool, []byte, error) {
	return k.StorageBackend.(oidcGuardedKV).CompareAndSwap(ctx, key, expected, newValue)
}

// guardedMemoryKV returns an in-memory store with the guarded writes.
func guardedMemoryKV(t *testing.T) oidcGuardedKV {
	t.Helper()
	kv, err := guardedOIDCStore(xkmssdk.NewStorageMemory())
	require.NoError(t, err)
	return kv
}

// shortLeaseWait bounds how long a contender waits for a held lease.
func shortLeaseWait(t *testing.T) {
	t.Helper()
	wait := oidcRotationLeaseWait
	oidcRotationLeaseWait = 300 * time.Millisecond
	t.Cleanup(func() { oidcRotationLeaseWait = wait })
}

// Two contenders on one store both observe the same expired lease before
// either removes it. Exactly one holds the lease afterwards: the takeover
// removes only the value it observed, so the second contender cannot delete
// the lease the first has just taken.
func TestOIDCRotationLock_ExpiredLeaseTakeoverHasOneWinner(t *testing.T) {
	shortLeaseWait(t)
	ctx := context.Background()
	inner := xkmssdk.NewStorageMemory()
	cn := iam.OIDCSigningKeyCN
	key := oidcRotationLeasePrefix + "system/" + cn
	stale := []byte(time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano) + " stale")
	require.NoError(t, inner.Put(ctx, key, stale))

	aObserved, bObserved, aDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var observers atomic.Int32
	kv := &leaseRaceKV{StorageBackend: inner}
	kv.onGet = func(k string, v []byte) {
		if k != key || !bytes.Equal(v, stale) {
			return
		}
		switch observers.Add(1) {
		case 1:
			close(aObserved)
			<-bObserved
		case 2:
			close(bObserved)
			<-aDone
		}
	}
	lock := newOIDCRotationLock(kv, kv)

	aErr, bErr := make(chan error, 1), make(chan error, 1)
	go func() {
		_, _, err := lock(ctx, "", cn)
		aErr <- err
		close(aDone)
	}()
	<-aObserved
	go func() {
		_, _, err := lock(ctx, "", cn)
		bErr <- err
	}()

	require.NoError(t, <-aErr)
	require.ErrorAs(t, <-bErr, new(*OIDCRotationBusyError))
	held, err := inner.Get(ctx, key)
	require.NoError(t, err)
	require.NotEqual(t, stale, held)
	require.False(t, oidcRotationLeaseExpired(held))
}

// A holder whose lease moved to another holder before it released leaves
// the new holder's lease in place.
func TestOIDCRotationLock_ReleaseKeepsNewHoldersLease(t *testing.T) {
	ctx := context.Background()
	inner := xkmssdk.NewStorageMemory()
	cn := iam.OIDCSigningKeyCN
	key := oidcRotationLeasePrefix + "system/" + cn
	kv := &leaseRaceKV{StorageBackend: inner}
	lock := newOIDCRotationLock(kv, kv)

	_, unlock, err := lock(ctx, "", cn)
	require.NoError(t, err)
	other := []byte(time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano) + " other")
	kv.beforeRemove = func(k string) {
		if k == key {
			require.NoError(t, inner.Put(ctx, key, other))
		}
	}
	unlock()

	held, err := inner.Get(ctx, key)
	require.NoError(t, err)
	require.Equal(t, other, held)
}

// unguardedKV offers only the base storage verbs.
type unguardedKV struct{ xkmssdk.StorageBackend }

// deleteOnlyKV offers CompareAndDelete but not CompareAndSwap.
type deleteOnlyKV struct{ xkmssdk.StorageBackend }

func (k deleteOnlyKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	return k.StorageBackend.(oidcGuardedKV).CompareAndDelete(ctx, key, expected)
}

// The replicated store is refused at setup, with the typed error, unless it
// offers both guarded verbs the lease and the version-record commit need.
func TestOIDCSigningStore_RequiresGuardedWrites(t *testing.T) {
	for name, kv := range map[string]xkmssdk.StorageBackend{
		"neither":           unguardedKV{xkmssdk.NewStorageMemory()},
		"no CompareAndSwap": deleteOnlyKV{xkmssdk.NewStorageMemory()},
	} {
		t.Run(name, func(t *testing.T) {
			s := &Server{config: &config.Config{}}
			err := s.useReplicatedOIDCSigningStore(kv, nil)
			var missing *OIDCSigningStoreGuardedWritesError
			require.ErrorAs(t, err, &missing)
		})
	}
}
