// Copyright (c) 2025 Jeremy Hahn

package server

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// shortLeaseTTL makes the rotation lease expire quickly.
func shortLeaseTTL(t *testing.T, ttl time.Duration) {
	t.Helper()
	prev := oidcRotationLeaseTTL
	oidcRotationLeaseTTL = ttl
	t.Cleanup(func() { oidcRotationLeaseTTL = prev })
}

// setLeaseHooks installs the lease test hooks for one test.
func setLeaseHooks(t *testing.T, acquired func(), renew func(context.Context)) {
	t.Helper()
	prevAcquired, prevRenew := oidcRotationLeaseAcquiredHook, oidcRotationLeaseRenewHook
	if acquired != nil {
		oidcRotationLeaseAcquiredHook = acquired
	}
	if renew != nil {
		oidcRotationLeaseRenewHook = renew
	}
	t.Cleanup(func() {
		oidcRotationLeaseAcquiredHook, oidcRotationLeaseRenewHook = prevAcquired, prevRenew
	})
}

// Node A takes the rotation lease and is paused past the TTL, renewals
// included. Node B takes the lease over and rotates to v2. When A resumes,
// its commit of the version record is fenced: A gets the typed error, the
// record stays at B's v2, and every record B left, its key material among
// them, is as B left it.
func TestOIDCSigningStore_StaleLeaseHolderCommitIsFenced(t *testing.T) {
	t.Cleanup(dbxkms.Reset)
	kv, tx := oidcReplicatedKV(t)
	a := bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv, tx)
	b := bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv, tx)
	v1, _, _ := a.Current()
	shortLeaseTTL(t, 300*time.Millisecond)

	aPaused, resumeA := make(chan struct{}), make(chan struct{})
	var acquisitions atomic.Int32
	setLeaseHooks(t, func() {
		if acquisitions.Add(1) == 1 {
			close(aPaused)
			<-resumeA
		}
	}, func(ctx context.Context) {
		// A paused node renews nothing; a lease ends with its holder.
		<-ctx.Done()
	})

	aErr := make(chan error, 1)
	go func() { aErr <- a.Rotate() }()
	<-aPaused
	time.Sleep(oidcRotationLeaseTTL + 100*time.Millisecond)

	require.NoError(t, b.Rotate())
	v2, _, _ := b.Current()
	require.NotEqual(t, v1, v2)
	afterB := snapshotKV(t, kv)

	close(resumeA)
	var fenced *OIDCRotationFencedError
	require.ErrorAs(t, <-aErr, &fenced)

	after := snapshotKV(t, kv)
	record := oidcSigningVersionsPrefix + "system" + oidcSigningVersionSegment + iam.OIDCSigningKeyCN
	require.Contains(t, afterB, record)
	require.Equal(t, afterB[record], after[record])
	for k, v := range afterB {
		require.Equal(t, v, after[k], k)
	}

	c := bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv, tx)
	kidC, _, _ := c.Current()
	require.Equal(t, v2, kidC)
	_, err := c.PublicForKID(context.Background(), v1)
	require.NoError(t, err)
}

// While the lease is held it is renewed, so it outlives its TTL; when a
// renewal is refused because another holder took the lease, the lease's
// context is canceled and the release leaves the other holder's lease.
func TestOIDCRotationLock_LostRenewalCancelsContext(t *testing.T) {
	shortLeaseTTL(t, 300*time.Millisecond)
	kv := guardedMemoryKV(t)
	lock := newOIDCRotationLock(kv, kv)
	key := oidcRotationLeasePrefix + "system/" + iam.OIDCSigningKeyCN

	ctx, unlock, err := lock(context.Background(), "", iam.OIDCSigningKeyCN)
	require.NoError(t, err)
	select {
	case <-ctx.Done():
		t.Fatal("lease context canceled while its renewals succeed")
	case <-time.After(2 * oidcRotationLeaseTTL):
	}
	held, err := kv.Get(context.Background(), key)
	require.NoError(t, err)
	require.False(t, oidcRotationLeaseExpired(held))

	other := []byte(time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano) + " other")
	require.NoError(t, kv.Put(context.Background(), key, other))
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("a refused renewal did not cancel the lease context")
	}
	unlock()
	held, err = kv.Get(context.Background(), key)
	require.NoError(t, err)
	require.Equal(t, other, held)
}

// SigningKeyManager.Rotate runs the rotation under the lease's context: a
// rotation whose lease is already lost writes nothing.
func TestOIDCSigningStore_RotateRunsUnderLeaseContext(t *testing.T) {
	t.Cleanup(dbxkms.Reset)
	kv, tx := oidcReplicatedKV(t)
	a := bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv, tx)
	before := snapshotKV(t, kv)
	t.Cleanup(func() { iam.SetRotationLock(oidcSigningBackendName, nil) })
	iam.SetRotationLock(oidcSigningBackendName, func(ctx context.Context, _, _ string) (context.Context, func(), error) {
		lost, cancel := context.WithCancel(ctx)
		cancel()
		return lost, func() {}, nil
	})

	require.ErrorIs(t, a.Rotate(), context.Canceled)
	require.Equal(t, before, snapshotKV(t, kv))
}
