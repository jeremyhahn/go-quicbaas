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
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// Multi-node create exclusivity
// =============================================================================
//
// CreateTask USED TO probe with Exists and then write with an UNCONDITIONAL
// Put, both under recordMu — a pair that is atomic in one process and nothing
// at all across a fleet, because a mutex on one node is invisible to another.
// It now issues a single guarded create that the store decides. These tests are
// what keep it that way.
//
// The consequence was worse than a duplicate create, which is why the harness
// asserts on the record rather than on return values. A late Put did not merely
// re-create the task, it OVERWROTE one that had since been claimed — resetting
// Status to pending, emptying AssignedTo and rewinding Version to 1. The claim
// was erased, and the next sweep dispatched a second worker onto work already
// running. That is go-frost#6 reached through a different verb.

// holdBeforeWrite lets a test freeze one node at the moment it is about to
// write the record — the window the defect lives in.
//
// IT HOLDS AT THE SAME LOGICAL POINT IN BOTH THE FIXED AND THE BROKEN STORE,
// which is what lets one test judge both. The two take different routes there:
// the broken store probes with Exists and then Puts, so the hold goes AFTER the
// read; the fixed store issues a single guarded PutIfAbsent, so the hold goes
// BEFORE it. Either way the node is parked having decided nothing and written
// nothing, which is exactly the state the erasure needs. A harness that hooked
// only Exists would hold nothing once the probe was gone, and would pass
// vacuously against the fix.
//
// Deterministic by construction rather than by scheduling luck: the held node
// signals that it has parked, and resumes only when the test releases it.
type holdBeforeWrite struct {
	interfaces.KVStore

	// key is the record whose probe is intercepted. Every other key passes
	// through untouched.
	key string

	// parked closes once the node is in the window, so the test knows it is
	// waiting there rather than merely started.
	parked chan struct{}

	// release lets the held node proceed to its write.
	release chan struct{}

	once sync.Once
}

func newHoldBeforeWrite(inner interfaces.KVStore, key string) *holdBeforeWrite {
	return &holdBeforeWrite{
		KVStore: inner,
		key:     key,
		parked:  make(chan struct{}),
		release: make(chan struct{}),
	}
}

// park signals that this node has reached the window and waits to be released.
func (k *holdBeforeWrite) park() {
	k.once.Do(func() { close(k.parked) })
	select {
	case <-k.release:
	case <-time.After(barrierWait):
	}
}

// Exists serves the probe and then parks — the broken store's route into the
// window, where the read is already done and the write has not happened.
func (k *holdBeforeWrite) Exists(ctx context.Context, key string) (bool, error) {
	exists, err := k.KVStore.Exists(ctx, key)
	if key == k.key {
		k.park()
	}
	return exists, err
}

// PutIfAbsent parks and then writes — the fixed store's route into the same
// window, where the guarded write has been decided on but not yet issued.
func (k *holdBeforeWrite) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	if key == k.key {
		k.park()
	}
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

// TestCreateTask_LateCreateDoesNotEraseAnotherNodesClaim is the damage case:
// not "two nodes both created", but "a node's create destroyed a claim that had
// already been won and was already being worked".
//
// The sequence is the one that costs money. Node B probes and finds the task
// absent. Before B writes, node A creates the task AND claims it — and a claim
// is a full Raft transaction, so that window is wide, not theoretical. B's
// unconditional Put then lands a fresh pending record on top: the claim is
// gone, the assignee is gone, and the version is back to 1 as though nothing
// had ever happened. Nothing in the system records that a claim was destroyed.
//
// The assertion is on the SURVIVING RECORD, not on B's return value, because
// the damage is what the next dispatcher will read. A store that returned an
// error to B while still overwriting the record would pass an error-only check
// and dispatch a second worker anyway.
func TestCreateTask_LateCreateDoesNotEraseAnotherNodesClaim(t *testing.T) {
	ctx := context.Background()
	shared := NewMemoryTaskKV()
	taskID := uniqueTaskID("erased")

	key, err := taskRecordKey("acme", taskID)
	if err != nil {
		t.Fatalf("taskRecordKey: %v", err)
	}

	// Node A writes straight through; node B is held in the window between its
	// probe and its write. Separate stores with separate arbiters, as separate
	// processes would be.
	nodeA := NewTaskStore(shared, newTransactionArbiter(t))
	held := newHoldBeforeWrite(shared, key)
	nodeB := NewTaskStore(held, newTransactionArbiter(t))

	var (
		wg      sync.WaitGroup
		createB error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		createB = nodeB.CreateTask(ctx, &Task{
			ID: taskID, TenantID: "acme", Subject: "node B's create",
		})
	}()

	// Wait until B is genuinely parked in the window, so the ordering under
	// test is the one that ran rather than the one that was hoped for.
	select {
	case <-held.parked:
	case <-time.After(barrierWait):
		t.Fatal("node B never reached the window before its write")
	}

	if err := nodeA.CreateTask(ctx, &Task{
		ID: taskID, TenantID: "acme", Subject: "node A's create",
	}); err != nil {
		t.Fatalf("node A create: %v", err)
	}
	if err := nodeA.ClaimTask(ctx, "acme", taskID, "agent-a"); err != nil {
		t.Fatalf("node A claim: %v", err)
	}

	close(held.release)
	wg.Wait()

	// The record must still show A's claim. This is the assertion that prints
	// the erasure.
	survivor, err := nodeA.GetTask(ctx, "acme", taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if survivor.Status != TaskStatusClaimed || survivor.AssignedTo != "agent-a" {
		t.Fatalf("node B's late create ERASED node A's claim: "+
			"record is status=%q assigned_to=%q version=%d, want status=%q assigned_to=%q; "+
			"the next sweep dispatches a second worker onto running work",
			survivor.Status, survivor.AssignedTo, survivor.Version,
			TaskStatusClaimed, "agent-a")
	}

	// And B must have been told why it lost, with the typed error, so a caller
	// can tell "already exists" from a backend failure without reading text.
	var exists *TaskAlreadyExistsError
	if !errors.As(createB, &exists) {
		t.Fatalf("node B's losing create returned %T: %v, want *TaskAlreadyExistsError",
			createB, createB)
	}
	if exists.TaskID != taskID {
		t.Errorf("the refusal names task %q, want %q", exists.TaskID, taskID)
	}
}

// TestCreateTask_RefusesAStoreThatCannotGuardACreate proves the fail-closed
// assertion: a backing store without the guarded verb makes CreateTask refuse
// rather than fall back to probing and hoping.
//
// This is the guard on the whole fix. Exists-then-Put is exactly the race being
// removed, so a store that silently emulated the guarantee would reintroduce
// the erasure in the one configuration nobody tests. kvstore.NewMemory wraps a
// raw storage engine with no conditional write, which makes it the honest
// stand-in for any store that cannot decide a create.
func TestCreateTask_RefusesAStoreThatCannotGuardACreate(t *testing.T) {
	t.Parallel()

	store := NewTaskStore(kvstore.NewMemory(), NewProcessClaimArbiter())

	err := store.CreateTask(context.Background(), &Task{ID: "ungarded", TenantID: "acme"})
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %T: %v", err, err)
	}

	// And nothing was written. A store that refused the call while still
	// leaving a record behind would be the worst of both: the caller believes
	// the create failed and the record is there for the next reader.
	if _, getErr := store.GetTask(context.Background(), "acme", "ungarded"); !errors.Is(getErr, ErrTaskNotFound) {
		t.Errorf("a refused create left a record behind: %v", getErr)
	}
}

// createBarrierKV holds every contender at the point it is about to write the
// record, releasing them only once all of them have arrived.
//
// Same dual-route rule as holdBeforeWrite, for the same reason: the broken
// store arrives via Exists and the fixed one via PutIfAbsent, and gathering
// them at whichever verb they use is what makes one test judge both. Every node
// then acts from the same snapshot, so an unguarded create yields N winners
// every run and a guarded one yields exactly one.
type createBarrierKV struct {
	interfaces.KVStore

	key   string
	nodes int

	mu      sync.Mutex
	arrived int
	gate    chan struct{}
}

func newCreateBarrierKV(inner interfaces.KVStore, key string, nodes int) *createBarrierKV {
	return &createBarrierKV{
		KVStore: inner,
		key:     key,
		nodes:   nodes,
		gate:    make(chan struct{}),
	}
}

// arrive counts this node in and blocks until every contender has arrived.
func (k *createBarrierKV) arrive() {
	k.mu.Lock()
	k.arrived++
	// Equality rather than >=, so a later arrival cannot close a closed gate.
	if k.arrived == k.nodes {
		close(k.gate)
	}
	k.mu.Unlock()

	select {
	case <-k.gate:
	case <-time.After(barrierWait):
	}
}

// Exists serves the probe and then gathers — the broken store's route.
func (k *createBarrierKV) Exists(ctx context.Context, key string) (bool, error) {
	exists, err := k.KVStore.Exists(ctx, key)
	if key == k.key {
		k.arrive()
	}
	return exists, err
}

// PutIfAbsent gathers and then writes — the fixed store's route.
func (k *createBarrierKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	if key == k.key {
		k.arrive()
	}
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

// TestCreateTask_NineNodesCreatingOneTaskProduceOneRecord is the create-side
// counterpart to the nine-worker claim race, at the same width and for the same
// reason: nine processes, one identifier, every one of them having probed
// before any of them writes.
//
// Exactly one create must win. The losers must receive
// *TaskAlreadyExistsError — not silence, and not a backend error — because a
// dispatcher that cannot tell "someone else already made this" from "the store
// is broken" will either retry forever or give up on work that exists.
func TestCreateTask_NineNodesCreatingOneTaskProduceOneRecord(t *testing.T) {
	ctx := context.Background()
	shared := NewMemoryTaskKV()
	taskID := uniqueTaskID("create-race")

	key, err := taskRecordKey("acme", taskID)
	if err != nil {
		t.Fatalf("taskRecordKey: %v", err)
	}
	barrier := newCreateBarrierKV(shared, key, nineWorkerRace)

	nodes := make([]TaskStore, nineWorkerRace)
	for i := range nodes {
		nodes[i] = NewTaskStore(barrier, newTransactionArbiter(t))
	}

	var (
		wg       sync.WaitGroup
		start    = make(chan struct{})
		verdicts = make([]error, nineWorkerRace)
	)
	for i, node := range nodes {
		wg.Add(1)
		go func(i int, node TaskStore) {
			defer wg.Done()
			<-start
			verdicts[i] = node.CreateTask(ctx, &Task{
				ID:       taskID,
				TenantID: "acme",
				Subject:  "created by " + claimRaceAgent(i),
			})
		}(i, node)
	}
	close(start)
	wg.Wait()

	winner := -1
	for node, err := range verdicts {
		if err == nil {
			if winner >= 0 {
				t.Fatalf("two nodes created one task: %s and %s both succeeded",
					claimRaceAgent(winner), claimRaceAgent(node))
			}
			winner = node
		}
	}
	if winner < 0 {
		t.Fatalf("no node created the task; verdicts: %v", verdicts)
	}

	for node, err := range verdicts {
		if node == winner {
			continue
		}
		var exists *TaskAlreadyExistsError
		if !errors.As(err, &exists) {
			t.Fatalf("node %d lost the create race but got %T: %v, want *TaskAlreadyExistsError",
				node, err, err)
		}
	}

	// One create means one record at its initial version. A version above 1 is
	// a second writer that got through and overwrote the first.
	created, err := nodes[0].GetTask(ctx, "acme", taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if created.Version != 1 {
		t.Errorf("the record is at version %d after one create; %d writers got through",
			created.Version, created.Version)
	}
	if created.Subject != "created by "+claimRaceAgent(winner) {
		t.Errorf("the surviving record was written by %q, but %q won the race",
			created.Subject, "created by "+claimRaceAgent(winner))
	}
}
