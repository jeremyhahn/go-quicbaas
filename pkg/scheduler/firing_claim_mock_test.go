// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package scheduler

import (
	"context"
	"sync"
	"time"
)

// claimBarrier holds every arriving claim until the expected number of nodes
// have arrived, then releases them all at once. It is how the leader-churn
// tests force two claims to be genuinely concurrent without a sleep: with a
// barrier of 2, neither node can complete its claim until both have started
// one, so a claim implementation that is not atomic will let both through.
type claimBarrier struct {
	mu       sync.Mutex
	expected int
	arrived  int
	release  chan struct{}
}

func newClaimBarrier(expected int) *claimBarrier {
	return &claimBarrier{expected: expected, release: make(chan struct{})}
}

// wait blocks until expected callers have arrived. Callers beyond the expected
// count pass straight through, so a barrier never deadlocks a test that
// dispatches fewer claims than it planned for.
//
// The wait is bounded. If the second contender never arrives — which is exactly
// what a regression that stops one node from reaching the claim looks like — an
// unbounded wait would hang the suite instead of failing it, and a hung suite
// reads as infrastructure trouble rather than as the bug it is. On timeout the
// barrier releases everyone so the test proceeds to its assertions and fails on
// the real discrepancy.
func (b *claimBarrier) wait() {
	b.mu.Lock()
	b.arrived++
	switch {
	case b.arrived == b.expected:
		close(b.release)
		b.mu.Unlock()
		return
	case b.arrived > b.expected:
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()

	timeout := time.NewTimer(barrierWait)
	defer timeout.Stop()
	select {
	case <-b.release:
	case <-timeout.C:
		b.releaseOnce()
	}
}

// releaseOnce opens the barrier for everyone still waiting, at most once.
func (b *claimBarrier) releaseOnce() {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.release:
	default:
		close(b.release)
	}
}

// barrierWait bounds how long a contender waits for its peers. Generous
// relative to the work involved, since it only has to exceed scheduling jitter.
const barrierWait = 30 * time.Second

// ClaimFiring makes mockJobStorage a FiringClaimer for the engine tests.
//
// It models the ENGINE'S CONTRACT, not the production storage design: a claim
// is decided by the store and reported as a verdict, and the engine must honour
// that verdict. It deliberately does NOT model the per-job monotonic mark the
// real store keeps — that design is exercised against a real qrdb transaction
// plane in pkg/scheduler/storage/quicbaas, where it belongs. Reproducing the
// mark here would only test this file's copy of it.
//
// Keeping a record per firing is what makes the engine tests legible: they
// assert which firings were granted and to whom.
func (m *mockJobStorage) ClaimFiring(_ context.Context, claim *FiringClaim) (bool, error) {
	m.claimCount.Add(1)

	m.errMu.RLock()
	claimErr := m.claimErr
	m.errMu.RUnlock()
	if claimErr != nil {
		return false, claimErr
	}

	// Arrive at the barrier before taking the lock: holding the store lock
	// while waiting would serialize the very race the barrier exists to
	// create.
	if m.claimBarrier != nil {
		m.claimBarrier.wait()
	}

	key := firingClaimKey{jobID: claim.JobID, firingNanos: claim.FiringTime.UnixNano()}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.firingClaims == nil {
		m.firingClaims = make(map[firingClaimKey]*FiringClaim)
	}
	if _, held := m.firingClaims[key]; held {
		return false, nil
	}
	stored := *claim
	m.firingClaims[key] = &stored
	return true, nil
}

// firingClaimKey mirrors the storage-layer claim key: a job identity plus the
// scheduled firing instant, which every node derives from the same replicated
// job record rather than from its own clock.
type firingClaimKey struct {
	jobID       uint64
	firingNanos int64
}

// claimAttempts reports how many claims reached the store, including the ones
// that lost. Tests use it to prove a node stood down at the claim rather than
// earlier.
func (m *mockJobStorage) claimAttempts() int {
	return int(m.claimCount.Load())
}

// claims returns the granted claim records.
func (m *mockJobStorage) claims() []*FiringClaim {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*FiringClaim, 0, len(m.firingClaims))
	for _, c := range m.firingClaims {
		cp := *c
		out = append(out, &cp)
	}
	return out
}

// setClaimErr makes every subsequent claim fail, modelling a store that cannot
// reach consensus.
func (m *mockJobStorage) setClaimErr(err error) {
	m.errMu.Lock()
	m.claimErr = err
	m.errMu.Unlock()
}
