package iam

import (
	"context"
	"crypto"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// promptly is how long a waiter whose request is done may take to return.
const promptly = time.Second

// lookupResult is one unknown-kid lookup's outcome.
type lookupResult struct {
	pub crypto.PublicKey
	err error
}

// lookup resolves kid on node n through the bearer path under ctx.
func lookup(n *sharedSigningNode, ctx context.Context, kid string) <-chan lookupResult {
	out := make(chan lookupResult, 1)
	go func() {
		pub, err := n.provider.PublicKeyForKID(ctx, kid)
		out <- lookupResult{pub: pub, err: err}
	}()
	return out
}

// await returns the lookup's result, failing the test when it is not back
// within d.
func await(t *testing.T, r <-chan lookupResult, d time.Duration, what string) lookupResult {
	t.Helper()
	select {
	case res := <-r:
		return res
	case <-time.After(d):
		t.Fatalf("%s did not return within %v", what, d)
		return lookupResult{}
	}
}

// waitForUnknownKIDRefresh waits until m has no unknown-kid or routine
// refresh in flight and nothing holds rotateMu, so a test ends with no refresh still reading
// the store it tears down.
func waitForUnknownKIDRefresh(m *SigningKeyManager) {
	for {
		if m.rotateMu.TryLock() {
			m.unknownKIDMu.Lock()
			idle := m.unknownKIDFlight == nil
			m.unknownKIDMu.Unlock()
			m.routineMu.Lock()
			idle = idle && m.routineFlight == nil
			m.routineMu.Unlock()
			m.rotateMu.Unlock()
			if idle {
				return
			}
		}
		runtime.Gosched()
	}
}

// peerRotated sets up nodes a and b on the shared backend, with b refreshed
// and a rotated since, and returns the kid of a's new key: b does not hold
// it until an unknown-kid refresh re-reads the version record.
func peerRotated(t *testing.T) (*countingStorage, *sharedSigningNode, *sharedSigningNode, string) {
	t.Helper()
	records := setupSharedSigningXKMS(t)
	a, b := newSharedSigningNode(t, ""), newSharedSigningNode(t, "")
	require.NoError(t, b.global.Refresh())
	require.NoError(t, a.global.Rotate())
	kid, _, _ := a.global.Current()
	t.Cleanup(func() { waitForUnknownKIDRefresh(b.global) })
	return records, a, b, kid
}

// stallStore blocks every version-record read until the returned release is
// called (also at cleanup).
func stallStore(t *testing.T, records *countingStorage) (release func()) {
	t.Helper()
	gate := make(chan struct{})
	records.gets.Store(0)
	records.gate.Store(&gate)
	release = sync.OnceFunc(func() {
		records.gate.Store(nil)
		close(gate)
	})
	t.Cleanup(release)
	return release
}

// awaitStoreRead waits until the stalled store has been read.
func awaitStoreRead(t *testing.T, records *countingStorage) {
	t.Helper()
	deadline := time.Now().Add(promptly)
	for records.gets.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no refresh read the version record")
		}
		runtime.Gosched()
	}
}

// rotationAwaitingLease starts a rotation on n whose lease is held elsewhere:
// it holds n's rotateMu until the returned release is called (also at
// cleanup).
func rotationAwaitingLease(t *testing.T, n *sharedSigningNode) (release func()) {
	t.Helper()
	waiting, lease := make(chan struct{}), make(chan struct{})
	SetRotationLock(sharedSigningBackend, func(ctx context.Context, _, _ string) (context.Context, func(), error) {
		close(waiting)
		<-lease
		return ctx, func() {}, nil
	})
	t.Cleanup(func() { SetRotationLock(sharedSigningBackend, nil) })
	rotated := make(chan error, 1)
	go func() { rotated <- n.global.Rotate() }()
	release = sync.OnceFunc(func() {
		close(lease)
		<-rotated
	})
	t.Cleanup(release)
	select {
	case <-waiting:
	case <-time.After(promptly):
		t.Fatal("the rotation never asked for its lease")
	}
	return release
}

// TestSigningKeyManager_UnknownKIDWaiterLeavesOnItsContext: while the
// unknown-kid refresh is held by a stalled store, a lookup whose request is
// canceled returns at once with the context's error, and the refresh, which
// that canceled lookup started, still completes for the lookup still waiting.
func TestSigningKeyManager_UnknownKIDWaiterLeavesOnItsContext(t *testing.T) {
	records, a, b, kid := peerRotated(t)
	release := stallStore(t, records)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := lookup(b, ctx, kid)
	awaitStoreRead(t, records)
	second := lookup(b, context.Background(), kid)
	// Give the second lookup time to reach the in-flight refresh.
	time.Sleep(20 * time.Millisecond)

	cancel()
	res := await(t, first, promptly, "the canceled lookup")
	require.ErrorIs(t, res.err, context.Canceled)
	select {
	case res := <-second:
		t.Fatalf("the waiting lookup returned before the refresh completed: %v", res.err)
	default:
	}

	release()
	res = await(t, second, promptly, "the waiting lookup")
	require.NoError(t, res.err, "the canceled lookup aborted the shared refresh")
	_, signer, _ := a.global.Current()
	require.Equal(t, signer.Public(), res.pub)
}

// TestSigningKeyManager_UnknownKIDMissLeavesARotationAwaitingItsLease: a
// rotation waiting on another node's lease holds rotateMu; a lookup missing a
// kid meanwhile returns at once when its request is canceled, rather than
// queueing on rotateMu behind the lease.
func TestSigningKeyManager_UnknownKIDMissLeavesARotationAwaitingItsLease(t *testing.T) {
	_, _, b, kid := peerRotated(t)
	rotationAwaitingLease(t, b)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	miss := lookup(b, ctx, kid)
	time.Sleep(20 * time.Millisecond)
	cancel()
	res := await(t, miss, promptly, "the canceled lookup behind the rotation")
	require.ErrorIs(t, res.err, context.Canceled)
}

// TestSigningKeyManager_UnknownKIDRefreshIsBounded: the shared unknown-kid
// refresh runs under its own bound, so neither a stalled store nor a rotation
// waiting on another node's lease holds the lookups waiting on it past the
// bound, even when no request of theirs is ever canceled; each gets the typed
// refresh-timeout error.
func TestSigningKeyManager_UnknownKIDRefreshIsBounded(t *testing.T) {
	for name, hold := range map[string]func(t *testing.T, records *countingStorage, b *sharedSigningNode){
		"stalled store": func(t *testing.T, records *countingStorage, _ *sharedSigningNode) {
			stallStore(t, records)
		},
		"rotation awaiting its lease": func(t *testing.T, _ *countingStorage, b *sharedSigningNode) {
			rotationAwaitingLease(t, b)
		},
	} {
		t.Run(name, func(t *testing.T) {
			records, _, b, kid := peerRotated(t)
			const bound = 100 * time.Millisecond
			prev := unknownKIDRefreshBound
			unknownKIDRefreshBound = bound
			t.Cleanup(func() { unknownKIDRefreshBound = prev })
			hold(t, records, b)

			const lookups = 8
			results := make([]<-chan lookupResult, 0, lookups)
			for range lookups {
				results = append(results, lookup(b, context.Background(), kid))
			}
			for i, r := range results {
				res := await(t, r, promptly, "a lookup waiting on the bounded refresh")
				var timeout *UnknownKIDRefreshTimeoutError
				require.ErrorAs(t, res.err, &timeout, "lookup %d", i)
				require.Equal(t, OIDCSigningKeyCN, timeout.CN)
				require.Equal(t, bound, timeout.Timeout)
			}
		})
	}
}

// TestSigningKeyManager_UnknownKIDRefreshBoundIsSeconds pins the production
// bound: long enough for a healthy store and a short lease wait, short enough
// that a stall cannot pin authentication.
func TestSigningKeyManager_UnknownKIDRefreshBoundIsSeconds(t *testing.T) {
	require.Equal(t, unknownKIDRefreshTimeout, unknownKIDRefreshBound)
	require.GreaterOrEqual(t, unknownKIDRefreshTimeout, time.Second)
	require.LessOrEqual(t, unknownKIDRefreshTimeout, 5*time.Second)
}
