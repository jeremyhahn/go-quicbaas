package iam

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// knownKIDDueForRefresh sets up a node holding its current key, with the
// routine refresh interval expired at every check and every version-record
// read stalled, and returns the node and the kid it holds.
func knownKIDDueForRefresh(t *testing.T) (*sharedSigningNode, *countingStorage, string) {
	t.Helper()
	records := setupSharedSigningXKMS(t)
	n := newSharedSigningNode(t, "")
	kid, _, _ := n.global.Current()
	var hours atomic.Int64
	prev := refreshNow
	refreshNow = func() time.Time { return time.Now().Add(time.Duration(hours.Add(1)) * time.Hour) }
	t.Cleanup(func() { refreshNow = prev })
	t.Cleanup(func() { waitForUnknownKIDRefresh(n.global) })
	stallStore(t, records)
	return n, records, kid
}

// TestSigningKeyManager_KnownKIDRefreshLeavesOnItsContext: a lookup of a
// kid the manager holds, whose routine refresh is due while the store is
// stalled, returns at once with its context's error when its request is
// canceled.
func TestSigningKeyManager_KnownKIDRefreshLeavesOnItsContext(t *testing.T) {
	n, records, kid := knownKIDDueForRefresh(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	res := lookup(n, ctx, kid)
	awaitStoreRead(t, records)
	cancel()
	got := await(t, res, promptly, "the canceled known-kid lookup")
	require.ErrorIs(t, got.err, context.Canceled)
}

// TestSigningKeyManager_KnownKIDRefreshIsBounded: the routine refresh of a
// known-kid lookup runs under the unknown-kid refresh's bound, so a stalled
// store holds no lookup past it, even one whose request is never canceled;
// each gets the typed refresh-timeout error.
func TestSigningKeyManager_KnownKIDRefreshIsBounded(t *testing.T) {
	const bound = 100 * time.Millisecond
	prev := unknownKIDRefreshBound
	unknownKIDRefreshBound = bound
	t.Cleanup(func() { unknownKIDRefreshBound = prev })
	n, _, kid := knownKIDDueForRefresh(t)

	const lookups = 4
	results := make([]<-chan lookupResult, 0, lookups)
	for range lookups {
		results = append(results, lookup(n, context.Background(), kid))
	}
	for i, r := range results {
		got := await(t, r, promptly, "a known-kid lookup on the bounded refresh")
		var timeout *SigningKeyRefreshTimeoutError
		require.ErrorAs(t, got.err, &timeout, "lookup %d", i)
		require.Equal(t, OIDCSigningKeyCN, timeout.CN)
		require.Equal(t, bound, timeout.Timeout)
	}
}
