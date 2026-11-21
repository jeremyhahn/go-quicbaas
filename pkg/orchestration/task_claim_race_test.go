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

package orchestration

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Multi-node claim exclusivity
// =============================================================================
//
// This differs from TestTaskStore_ConcurrentClaimHasOneWinner in the one way
// that matters: that test races goroutines through ONE store, so recordMu
// serializes them and it passes whether or not anything decides the contest.
// These race SEPARATE STORE INSTANCES, each with its own arbiter, over one
// shared backing store — which is what a fleet is. Every node holds its own
// keyedMutex, and a mutex on one node arbitrates nothing on another.

// nineWorkerRace is the go-frost#6 case at its measured width: nine agents were
// dispatched onto one task and all nine claimed it, because each ran in its own
// process and the claim was decided by a process-local lock.
const nineWorkerRace = 9

// barrierWait bounds the read barrier below. Reaching it means fewer
// contenders read the record than the test expected, which is a broken test
// rather than a slow one, so it fails loudly instead of hanging the package.
const barrierWait = 30 * time.Second

// claimBarrierKV holds every contender at the point it has READ the task record
// but not yet written it, releasing them only once all of them have read.
//
// This is what makes the race deterministic instead of statistical. Left to the
// scheduler, the nine goroutines interleave differently every run: the defect
// reproduced 13 times in 20 under -race and NOT AT ALL in a plain -count=1 run,
// which is a regression detector that reports "fixed" most of the time. The
// barrier reproduces the exact interleaving the defect needs — every node
// decides from the same snapshot — so a store that arbitrates locally yields
// nine winners every single run, and one that arbitrates on the transaction
// plane yields exactly one.
type claimBarrierKV struct {
	interfaces.KVStore

	// key is the task record every contender reads. Only reads of this key
	// count toward the barrier; the store's other traffic passes through.
	key string

	mu      sync.Mutex
	arrived int
	gate    chan struct{}
}

// PutIfAbsent forwards the guarded create. The claim barrier only intercepts
// reads, but embedding interfaces.KVStore would otherwise strip the capability
// and make every create through this wrapper fail — see forwardPutIfAbsent.
func (k *claimBarrierKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

// CompareAndSwap forwards the guarded edit every task record write commits
// through, for the same reason.
func (k *claimBarrierKV) CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error) {
	return forwardCompareAndSwap(ctx, k.KVStore, key, expected, next)
}

func newClaimBarrierKV(inner interfaces.KVStore, key string) *claimBarrierKV {
	return &claimBarrierKV{KVStore: inner, key: key, gate: make(chan struct{})}
}

// Get serves the read, then blocks the caller until every contender has read.
func (k *claimBarrierKV) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := k.KVStore.Get(ctx, key)
	if key != k.key {
		return data, err
	}

	k.mu.Lock()
	k.arrived++
	// Equality rather than >=, so a later read cannot close an already-closed
	// gate. arrived only increases, so this is true exactly once.
	if k.arrived == nineWorkerRace {
		close(k.gate)
	}
	k.mu.Unlock()

	select {
	case <-k.gate:
	case <-time.After(barrierWait):
	}
	return data, err
}

// claimRaceCluster models a fleet: one backing store shared by every node, and
// one store instance per node, each with its own arbiter and therefore its own
// keyedMutex.
type claimRaceCluster struct {
	nodes []TaskStore
}

// newClaimRaceCluster builds a cluster whose nodes arbitrate through the real
// transaction plane, and holds them all at the read barrier.
//
// The task is seeded before the barrier is installed, so seeding does not
// consume one of its slots.
func newClaimRaceCluster(t *testing.T, tenantID, taskID string) *claimRaceCluster {
	t.Helper()

	shared := NewMemoryTaskKV()
	seedTask(t, NewTaskStore(shared, NewProcessClaimArbiter()), tenantID, taskID)

	key, err := taskRecordKey(tenantID, taskID)
	if err != nil {
		t.Fatalf("taskRecordKey: %v", err)
	}
	barrier := newClaimBarrierKV(shared, key)

	n := claimNode(t)
	cluster := &claimRaceCluster{nodes: make([]TaskStore, nineWorkerRace)}
	for i := range cluster.nodes {
		// One arbiter per node, as nine processes would have. They agree only
		// because the transaction plane decides, not because they share memory.
		arbiter, err := NewTransactionClaimArbiter(n.Transaction, n.KV)
		if err != nil {
			t.Fatalf("NewTransactionClaimArbiter: %v", err)
		}
		cluster.nodes[i] = NewTaskStore(barrier, arbiter)
	}
	return cluster
}

// raceClaim releases every node at once and returns each node's verdict.
func (c *claimRaceCluster) raceClaim(ctx context.Context, tenantID, taskID string) []error {
	var (
		wg      sync.WaitGroup
		start   = make(chan struct{})
		verdict = make([]error, len(c.nodes))
	)
	for i, node := range c.nodes {
		wg.Add(1)
		go func(i int, node TaskStore) {
			defer wg.Done()
			<-start
			verdict[i] = node.ClaimTask(ctx, tenantID, taskID, claimRaceAgent(i))
		}(i, node)
	}
	close(start)
	wg.Wait()
	return verdict
}

// claimRaceAgent names the agent running on node i. The name carries the node
// index so a loser's error can be checked to name the winner rather than merely
// to be non-nil.
func claimRaceAgent(node int) string { return "agent-" + strconv.Itoa(node) }

// TestClaimTask_NineWorkersOnOneTaskProduceOneWinner is the regression test for
// the SDLC module's founding failure: nine workers claimed go-frost#6 at once and
// all nine proceeded, because ClaimTask decided the transition with a
// read-modify-write held under a process-local mutex. Nine processes hold nine
// mutexes, so nine readers all saw the task pending and nine writers all wrote
// it claimed.
//
// The assertion is on the REASON, not only the count. A losing node must be
// told the task is already claimed and BY WHOM — that is what lets a dispatcher
// distinguish "someone else has this" from "the store is broken" and stand down
// rather than retry. A loss reported as a bare error, or as an invalid-state
// error, would leave that decision to string matching.
func TestClaimTask_NineWorkersOnOneTaskProduceOneWinner(t *testing.T) {
	ctx := context.Background()
	taskID := uniqueTaskID("frost-6-store")
	cluster := newClaimRaceCluster(t, "acme", taskID)

	verdicts := cluster.raceClaim(ctx, "acme", taskID)

	winner := -1
	for node, err := range verdicts {
		if err == nil {
			if winner >= 0 {
				t.Fatalf("two nodes claimed one task: %s and %s both won",
					claimRaceAgent(winner), claimRaceAgent(node))
			}
			winner = node
		}
	}
	if winner < 0 {
		t.Fatalf("no node claimed the task; verdicts: %v", verdicts)
	}

	for node, err := range verdicts {
		if node == winner {
			continue
		}
		var claimed *TaskAlreadyClaimedError
		if !errors.As(err, &claimed) {
			t.Fatalf("node %d lost the race but was not told the task was claimed: got %T: %v",
				node, err, err)
		}
		if claimed.ClaimedBy != claimRaceAgent(winner) {
			t.Errorf("node %d was told the task is held by %q, but %q won it",
				node, claimed.ClaimedBy, claimRaceAgent(winner))
		}
		if claimed.RequestingBy != claimRaceAgent(node) {
			t.Errorf("node %d's rejection names %q as the requester, want %q",
				node, claimed.RequestingBy, claimRaceAgent(node))
		}
	}

	// The record itself must agree with the verdicts: one claim, one assignee,
	// one version bump. A record naming a node that was told it lost would mean
	// the losers stood down and the work is assigned to nobody who is running.
	claimed, err := cluster.nodes[0].GetTask(ctx, "acme", taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if claimed.AssignedTo != claimRaceAgent(winner) {
		t.Errorf("record is assigned to %q, but %q won the claim",
			claimed.AssignedTo, claimRaceAgent(winner))
	}
	if claimed.Status != TaskStatusClaimed {
		t.Errorf("record status is %q after a won claim, want %q", claimed.Status, TaskStatusClaimed)
	}
	if claimed.Version != 2 {
		t.Errorf("record took %d writes for one claim; a second writer got through", claimed.Version-1)
	}
}

// TestClaimTask_RefusesWithoutAnArbiter proves a store wired without an arbiter
// fails closed rather than deciding the claim on its own.
//
// This is the guard on the whole design. The defect being removed is a claim
// settled by a process-local lock, so a store that could fall back to one when
// its arbiter is missing would reintroduce it in the one configuration nobody
// tests — a fleet whose wiring silently regressed.
func TestClaimTask_RefusesWithoutAnArbiter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewTaskStore(NewMemoryTaskKV(), nil)
	seedTask(t, store, "acme", "unarbitrated")

	err := store.ClaimTask(ctx, "acme", "unarbitrated", "agent-a")
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %T: %v", err, err)
	}

	// And nothing was written: a refused claim must not leave the task looking
	// taken, or the next contender is refused by a claim that never happened.
	task, getErr := store.GetTask(ctx, "acme", "unarbitrated")
	if getErr != nil {
		t.Fatalf("GetTask: %v", getErr)
	}
	if task.Status != TaskStatusPending {
		t.Errorf("a refused claim left the task %q, want %q", task.Status, TaskStatusPending)
	}
	if task.AssignedTo != "" {
		t.Errorf("a refused claim assigned the task to %q", task.AssignedTo)
	}
}
