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
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Leader-churn simulation
// =============================================================================
//
// docs/18 V4: during Raft leader churn the deposed leader has not yet learned
// it lost leadership while the new leader has already been elected, so both
// nodes' LeaderChecker answers true for the same tick. Before the firing claim
// both nodes dispatched the same due job and the handler ran twice.
//
// The tests below reconstruct exactly that state: two SchedulerEngines sharing
// one replicated store, both believing they are the leader, both seeing the
// same due job in the same tick.

// churnFixture is the two-node leader-churn scene: one shared store, two
// engines that both answer "I am the leader", and one recurring job that is
// due right now.
type churnFixture struct {
	storage *mockJobStorage
	nodeA   *SchedulerEngine
	nodeB   *SchedulerEngine
	firing  time.Time

	// runsA and runsB count handler invocations per node, so a double fire is
	// attributable rather than merely visible.
	runsA *atomicCounter
	runsB *atomicCounter
}

// atomicCounter is a minimal counter the handlers share; the scheduler's own
// success counters do not distinguish "did not execute" from "executed and
// failed to persist", which is the distinction these tests turn on.
type atomicCounter struct {
	mu sync.Mutex
	n  int
}

func (c *atomicCounter) inc()     { c.mu.Lock(); c.n++; c.mu.Unlock() }
func (c *atomicCounter) get() int { c.mu.Lock(); defer c.mu.Unlock(); return c.n }
func newCounter() *atomicCounter  { return &atomicCounter{} }

// newChurnFixture builds the scene. barrier, when non-nil, is released by the
// storage once both nodes have arrived at their claim attempt, which forces the
// two claims to overlap instead of running one after the other.
func newChurnFixture(t *testing.T, barrier *claimBarrier) *churnFixture {
	t.Helper()

	storage := newMockJobStorage()
	storage.claimBarrier = barrier
	if barrier != nil {
		// Hold both nodes at the due-job read as well. Otherwise the winner
		// can finish and push NextRun into the future before the other node
		// even looks, and the test would pass for the wrong reason.
		storage.dueBarrier = newClaimBarrier(2)
	}

	firing := time.Now().Add(-time.Minute)
	job := &Job{
		ID:        1,
		Name:      "acme-renewal",
		OwnerType: OwnerTypeSystem,
		Type:      JobTypeRecurring,
		Schedule:  "0 * * * *",
		Handler:   "renew",
		Status:    JobStatusPending,
		NextRun:   firing,
	}
	storage.jobs[job.ID] = copyJob(job)

	cfg := DefaultConfig()
	cfg.MaxConcurrentJobs = 4

	runsA, runsB := newCounter(), newCounter()

	// Both nodes report leadership: this IS the churn window.
	nodeA, err := NewWithClustering(cfg, storage, "node-A", &fakeLeader{isLeader: true})
	require.NoError(t, err)
	nodeB, err := NewWithClustering(cfg, storage, "node-B", &fakeLeader{isLeader: true})
	require.NoError(t, err)

	require.NoError(t, nodeA.RegisterHandler("renew", func(context.Context, *Job) (*JobResult, error) {
		runsA.inc()
		return &JobResult{Success: true}, nil
	}))
	require.NoError(t, nodeB.RegisterHandler("renew", func(context.Context, *Job) (*JobResult, error) {
		runsB.inc()
		return &JobResult{Success: true}, nil
	}))

	return &churnFixture{
		storage: storage,
		nodeA:   nodeA,
		nodeB:   nodeB,
		firing:  firing,
		runsA:   runsA,
		runsB:   runsB,
	}
}

// tickBoth runs one scheduler tick on both nodes concurrently and waits for
// every dispatched execution to finish. No sleeps: processDueJobs registers its
// executions on the engine WaitGroup, so waiting on that group is exact.
func (f *churnFixture) tickBoth(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	var started sync.WaitGroup
	started.Add(2)
	go func() { defer started.Done(); f.nodeA.processDueJobs(ctx) }()
	go func() { defer started.Done(); f.nodeB.processDueJobs(ctx) }()
	started.Wait()

	f.nodeA.wg.Wait()
	f.nodeB.wg.Wait()
}

// TestFiringClaim_LeaderChurn_ExecutesExactlyOnce is the docs/18 V4 regression.
// Two nodes both believe they are the leader for the same firing; exactly one
// handler invocation may happen cluster-wide.
func TestFiringClaim_LeaderChurn_ExecutesExactlyOnce(t *testing.T) {
	f := newChurnFixture(t, newClaimBarrier(2))

	f.tickBoth(t)

	total := f.runsA.get() + f.runsB.get()
	assert.Equal(t, 1, total,
		"a firing must execute at most once cluster-wide; node-A ran %d, node-B ran %d",
		f.runsA.get(), f.runsB.get())

	// Assert the store's ledger too, not just the handler count. Run counts
	// alone depend on execution timing — if the winner finishes and advances
	// the job before the loser's pre-flight re-read, the loser can decline for
	// reasons that have nothing to do with the claim, and the test would pass
	// even with the claim removed. Exactly one granted claim is true whenever
	// the gate is present and false the moment it is not.
	assert.Len(t, f.storage.claims(), 1,
		"the store must have granted exactly one claim for this firing")
}

// TestFiringClaim_LeaderChurn_LoserLostTheClaim asserts the REASON the second
// node stood down. Counting executions alone would pass just as well if the
// loser had crashed, found no due job, or been starved by the semaphore — this
// test pins the cause to the store's claim verdict.
func TestFiringClaim_LeaderChurn_LoserLostTheClaim(t *testing.T) {
	f := newChurnFixture(t, newClaimBarrier(2))

	f.tickBoth(t)

	// Both nodes attempted the claim: neither stood down for some earlier
	// reason such as the leader gate or an empty due-job list.
	assert.Equal(t, 2, f.storage.claimAttempts(),
		"both nodes must reach the claim; a node that never attempted it stood down for the wrong reason")

	// The store granted exactly one claim, and it is recorded against the
	// firing the job was actually due for.
	claims := f.storage.claims()
	require.Len(t, claims, 1, "the store must grant exactly one claim per firing")
	granted := claims[0]
	assert.Equal(t, uint64(1), granted.JobID)
	assert.True(t, granted.FiringTime.Equal(f.firing),
		"claim key must be the job's scheduled firing time, not a local clock reading")

	// Identify winner and loser from the claim record, then assert each
	// engine's own accounting agrees with the store's verdict.
	winner, loser := f.nodeA, f.nodeB
	winnerRuns, loserRuns := f.runsA, f.runsB
	if granted.Owner == "node-B" {
		winner, loser = f.nodeB, f.nodeA
		winnerRuns, loserRuns = f.runsB, f.runsA
	}
	require.Contains(t, []string{"node-A", "node-B"}, granted.Owner)

	assert.Equal(t, uint64(1), winner.FiringClaimsWon(), "the winner must record the claim it won")
	assert.Equal(t, uint64(0), winner.FiringClaimsLost())
	assert.Equal(t, 1, winnerRuns.get(), "the winner must be the node that ran the handler")

	assert.Equal(t, uint64(1), loser.FiringClaimsLost(),
		"the loser must record that it stood down because it lost the claim")
	assert.Equal(t, uint64(0), loser.FiringClaimsWon())
	assert.Equal(t, uint64(0), loser.FiringClaimErrors(),
		"losing a claim is a verdict, not an error")
	assert.Equal(t, 0, loserRuns.get(), "the loser must not have run the handler")
}

// TestFiringClaim_SameFiringNeverRunsTwiceAcrossTicks covers the churn variant
// where the two nodes do not overlap: the deposed leader ticks after the new
// one already fired and persisted nothing (job still pending). The claim is a
// record, not a moment, so the late node still loses.
func TestFiringClaim_SameFiringNeverRunsTwiceAcrossTicks(t *testing.T) {
	f := newChurnFixture(t, nil)

	ctx := context.Background()
	f.nodeA.processDueJobs(ctx)
	f.nodeA.wg.Wait()

	// Put the job back to pending at the same firing time: this is the state
	// a node that lost leadership mid-write leaves behind.
	f.storage.mu.Lock()
	f.storage.jobs[1].Status = JobStatusPending
	f.storage.jobs[1].NextRun = f.firing
	f.storage.mu.Unlock()

	f.nodeB.processDueJobs(ctx)
	f.nodeB.wg.Wait()

	assert.Equal(t, 1, f.runsA.get(), "node-A owns the firing it claimed")
	assert.Equal(t, 0, f.runsB.get(), "node-B must not re-run a firing already claimed")
	assert.Equal(t, uint64(1), f.nodeB.FiringClaimsLost(),
		"node-B stood down because the claim record already existed")
}

// TestFiringClaim_NextFiringIsANewClaim proves a recurring job keeps firing:
// once it advances to its next scheduled time the same node claims again and
// runs again.
//
// This asserts the ENGINE's contract against the mock claimer, not the
// production key layout. The real store keeps one monotonic high-water mark per
// job (see pkg/scheduler/storage/quicbaas); the mock keeps a record per firing
// because that makes engine assertions legible. Do not read the record count
// here as a statement about production.
func TestFiringClaim_NextFiringIsANewClaim(t *testing.T) {
	f := newChurnFixture(t, nil)

	ctx := context.Background()
	f.nodeA.processDueJobs(ctx)
	f.nodeA.wg.Wait()
	require.Equal(t, 1, f.runsA.get())

	next := f.firing.Add(30 * time.Second)
	f.storage.mu.Lock()
	f.storage.jobs[1].Status = JobStatusPending
	f.storage.jobs[1].NextRun = next
	f.storage.mu.Unlock()

	f.nodeA.processDueJobs(ctx)
	f.nodeA.wg.Wait()

	assert.Equal(t, 2, f.runsA.get(), "a new firing time is a new claim")
	assert.Equal(t, uint64(2), f.nodeA.FiringClaimsWon())
	assert.Len(t, f.storage.claims(), 2, "the mock records each granted firing separately")
}

// TestFiringClaim_StoreFailureSuppressesExecution is the fail-closed direction.
// A claim the store could not decide is not a licence to fire: for a
// non-idempotent handler an unnecessary skip costs a cycle, a double fire costs
// a duplicate certificate.
func TestFiringClaim_StoreFailureSuppressesExecution(t *testing.T) {
	f := newChurnFixture(t, nil)
	claimErr := errors.New("raft proposal timed out")
	f.storage.setClaimErr(claimErr)

	f.nodeA.processDueJobs(context.Background())
	f.nodeA.wg.Wait()

	assert.Equal(t, 0, f.runsA.get(), "an undecided claim must not execute")
	assert.Equal(t, uint64(1), f.nodeA.FiringClaimErrors(),
		"the failure must be recorded as a claim error, not silently swallowed")
	assert.Equal(t, uint64(0), f.nodeA.FiringClaimsWon())
	assert.Equal(t, uint64(0), f.nodeA.FiringClaimsLost(),
		"an undecided claim is not a lost claim")
}

// TestFiringClaim_ManualTriggerBypassesTheClaim documents the deliberate
// boundary: TriggerJob is an operator action against a specific node with no
// scheduled firing time, so it is not gated by the scheduled-dispatch claim.
func TestFiringClaim_ManualTriggerBypassesTheClaim(t *testing.T) {
	f := newChurnFixture(t, nil)

	f.nodeA.triggerJobByID(context.Background(), triggerRequest{jobID: 1})
	f.nodeA.wg.Wait()

	assert.Equal(t, 1, f.runsA.get(), "a manual trigger executes on the node it was sent to")
	assert.Equal(t, 0, f.storage.claimAttempts(), "manual triggers do not take a scheduled-firing claim")
}

// TestSteadyState_ClusterWideJobFiresOnExactlyOneNode is the regression for the
// defect the WP0 audit turned up, which is distinct from the churn race above.
//
// The churn tests cover two nodes that BOTH believe they lead. This covers the
// ordinary case: a healthy cluster with one leader and two followers, no
// election in progress, nothing contended. Before clustering was wired,
// `LeaderChecker` was nil on every node and `shouldExecute` answered true
// everywhere, so a cluster-wide job fired on every node on every tick — not a
// race window, the steady state. A test that only simulated churn would have
// passed throughout.
func TestSteadyState_ClusterWideJobFiresOnExactlyOneNode(t *testing.T) {
	storage := newMockJobStorage()
	firing := time.Now().Add(-time.Minute)
	storage.jobs[1] = copyJob(&Job{
		ID:        1,
		Name:      "acme-renewal",
		OwnerType: OwnerTypeSystem,
		Type:      JobTypeRecurring,
		Schedule:  "0 * * * *",
		Handler:   "renew",
		Status:    JobStatusPending,
		NextRun:   firing,
	})

	cfg := DefaultConfig()
	cfg.MaxConcurrentJobs = 4

	// One leader, two followers — a healthy cluster, no election in flight.
	runs := map[string]*atomicCounter{"leader": newCounter(), "follower-1": newCounter(), "follower-2": newCounter()}
	nodes := make(map[string]*SchedulerEngine, 3)
	for name, isLeader := range map[string]bool{"leader": true, "follower-1": false, "follower-2": false} {
		engine, err := NewWithClustering(cfg, storage, name, &fakeLeader{isLeader: isLeader})
		require.NoError(t, err)
		counter := runs[name]
		require.NoError(t, engine.RegisterHandler("renew", func(context.Context, *Job) (*JobResult, error) {
			counter.inc()
			return &JobResult{Success: true}, nil
		}))
		nodes[name] = engine
	}

	ctx := context.Background()
	var ticking sync.WaitGroup
	ticking.Add(len(nodes))
	for _, engine := range nodes {
		go func(e *SchedulerEngine) { defer ticking.Done(); e.processDueJobs(ctx) }(engine)
	}
	ticking.Wait()
	for _, engine := range nodes {
		engine.wg.Wait()
	}

	assert.Equal(t, 1, runs["leader"].get(), "the leader owns cluster-wide dispatch")
	assert.Equal(t, 0, runs["follower-1"].get(), "a follower must not fire a cluster-wide job")
	assert.Equal(t, 0, runs["follower-2"].get(), "a follower must not fire a cluster-wide job")

	// The followers stood down at the LEADER GATE, before the claim: in steady
	// state the claim is never contended, so a test that only counted claims
	// would not notice the gate disappearing.
	assert.Equal(t, 1, storage.claimAttempts(),
		"only the leader should reach the claim in steady state; more attempts means "+
			"followers are dispatching and only the claim is holding them back")
}

// TestFiringClaim_StaleDispatchDeclinesAfterTheJobMovesOn is the regression for
// a double-fire path the claim alone does not close.
//
// Claiming and executing are separated by a goroutine hop. Node A reads the due
// job at firing T1 and wins the claim for T1 honestly. Before its execution
// goroutine runs, the job moves on — completed and rescheduled, retried onto a
// backoff, or updated — so the record now in front of A is firing T2, which
// node B may already have claimed and be running. A's claim was for a firing
// that no longer exists, and executing the record it finds would double-fire
// through the back door.
//
// The fix carries the claimed firing into executeJob and declines under the
// same lock that guards the running-status write.
func TestFiringClaim_StaleDispatchDeclinesAfterTheJobMovesOn(t *testing.T) {
	f := newChurnFixture(t, nil)
	ctx := context.Background()

	// The job has moved to a later firing than the one being dispatched.
	moved := f.firing.Add(time.Hour)
	f.storage.mu.Lock()
	f.storage.jobs[1].NextRun = moved
	f.storage.mu.Unlock()

	// Dispatch explicitly for the ORIGINAL firing, as a node holding a stale
	// due-list snapshot would.
	f.nodeA.wg.Add(1)
	go func() {
		defer f.nodeA.wg.Done()
		f.nodeA.executeJob(ctx, copyJob(&Job{
			ID: 1, Name: "acme-renewal", OwnerType: OwnerTypeSystem,
			Type: JobTypeRecurring, Handler: "renew",
			Status: JobStatusPending, NextRun: f.firing,
		}), f.firing)
	}()
	f.nodeA.wg.Wait()

	assert.Equal(t, 0, f.runsA.get(),
		"a dispatch whose claimed firing no longer matches the job record must not run — "+
			"that record is a different firing another node may already own")
	assert.Equal(t, uint64(1), f.nodeA.StaleDispatches(),
		"the declined dispatch must be counted; it is a prevented double-fire, not a no-op")
}

// TestFiringClaim_MatchingFiringStillExecutes is the other half: the check must
// decline only genuine mismatches, or it would silently stop the scheduler.
func TestFiringClaim_MatchingFiringStillExecutes(t *testing.T) {
	f := newChurnFixture(t, nil)

	f.nodeA.processDueJobs(context.Background())
	f.nodeA.wg.Wait()

	assert.Equal(t, 1, f.runsA.get(), "a dispatch whose firing still matches must run")
	assert.Equal(t, uint64(0), f.nodeA.StaleDispatches(), "a matching firing is not a stale dispatch")
}
