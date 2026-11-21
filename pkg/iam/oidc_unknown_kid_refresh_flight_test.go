package iam

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSigningKeyManager_UnknownKIDRefreshWorkersNeverOverlap: while a
// rotation holds rotateMu awaiting its lease, unknown-kid lookups arrive
// across several of the refresh's bounds. Each refresh worker gives up on
// rotateMu at its bound, and no new worker starts while an earlier one is
// still alive, so at most one is ever alive; every lookup returns.
func TestSigningKeyManager_UnknownKIDRefreshWorkersNeverOverlap(t *testing.T) {
	var mu sync.Mutex
	live, peak, started := 0, 0, 0
	prevHook := unknownKIDWorkerHook
	unknownKIDWorkerHook = func(delta int) {
		mu.Lock()
		defer mu.Unlock()
		live += delta
		if delta > 0 {
			started++
		}
		peak = max(peak, live)
	}
	t.Cleanup(func() { unknownKIDWorkerHook = prevHook })

	_, _, b, kid := peerRotated(t)
	const bound = 100 * time.Millisecond
	prev := unknownKIDRefreshBound
	unknownKIDRefreshBound = bound
	t.Cleanup(func() { unknownKIDRefreshBound = prev })
	rotationAwaitingLease(t, b)

	const windows = 5
	var results []<-chan lookupResult
	tick := time.NewTicker(bound / 4)
	defer tick.Stop()
	for end := time.Now().Add(windows * bound); time.Now().Before(end); <-tick.C {
		results = append(results, lookup(b, context.Background(), kid))
	}
	for i, r := range results {
		res := await(t, r, promptly, "an unknown-kid lookup while a rotation holds rotateMu")
		var timeout *UnknownKIDRefreshTimeoutError
		if errors.As(res.err, &timeout) {
			require.Equal(t, bound, timeout.Timeout, "lookup %d", i)
			continue
		}
		// A lookup between two refreshes is rate-limited and misses.
		require.ErrorAs(t, res.err, new(*UnknownKeyIDError), "lookup %d", i)
	}

	mu.Lock()
	defer mu.Unlock()
	require.Greater(t, started, 1, "no refresh started after the first gave up")
	require.Equal(t, 1, peak, "refresh workers alive at once")
}
