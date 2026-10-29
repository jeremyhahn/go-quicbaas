// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// spawnWriteBarrierKV holds the first two writes of one task record until both
// have arrived. Each writer has read the record before it writes, so when the
// barrier opens both are committing over the same version: the state two nodes
// are in when they claim one task at once. Every other write passes straight
// through to the real node.
type spawnWriteBarrierKV struct {
	*nodeJobKV
	key      string
	arrivals atomic.Int32
	release  chan struct{}
}

func newSpawnWriteBarrierKV(t *testing.T, tenantID, taskID string) *spawnWriteBarrierKV {
	t.Helper()
	key, err := taskRecordKey(tenantID, taskID)
	if err != nil {
		t.Fatalf("taskRecordKey: %v", err)
	}
	return &spawnWriteBarrierKV{nodeJobKV: newNodeJobKV(t), key: key, release: make(chan struct{})}
}

// hold blocks a write of the barrier's key until the second one arrives.
func (k *spawnWriteBarrierKV) hold(ctx context.Context, key string) error {
	if key != k.key {
		return nil
	}
	switch k.arrivals.Add(1) {
	case 1:
	case 2:
		close(k.release)
	default:
		return nil
	}
	select {
	case <-k.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (k *spawnWriteBarrierKV) Put(ctx context.Context, key string, value []byte) error {
	if err := k.hold(ctx, key); err != nil {
		return err
	}
	return k.nodeJobKV.Put(ctx, key, value)
}

func (k *spawnWriteBarrierKV) CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error) {
	if err := k.hold(ctx, key); err != nil {
		return false, err
	}
	return k.nodeJobKV.CompareAndSwap(ctx, key, expected, next)
}

// TestClaimTaskSpawn_TwoNodesClaimOneTaskOnce is the go-quicbaas#719
// regression. Two task stores stand for two nodes: each has its own process
// lock, and they share only the qrdb node underneath. Both read the pending
// task, both write their claim over the same version, and exactly one may
// win — the store's compare-and-swap decides, because no lock is shared.
func TestClaimTaskSpawn_TwoNodesClaimOneTaskOnce(t *testing.T) {
	const tenantID = "tenant-fleet-spawn"
	taskID := uniqueTaskID("task-fleet-spawn")
	kv := newSpawnWriteBarrierKV(t, tenantID, taskID)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nodes := [2]TaskStore{
		NewTaskStore(kv, newTransactionArbiter(t)),
		NewTaskStore(kv, newTransactionArbiter(t)),
	}
	if err := nodes[0].CreateTask(ctx, &Task{
		ID: taskID, TenantID: tenantID, Subject: "claimable",
		Status: TaskStatusPending, AutoSpawn: true,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	var (
		done   sync.WaitGroup
		claims [2]TaskSpawnClaim
		errs   [2]error
		jobIDs = [2]string{"job-node-a", "job-node-b"}
	)
	for i := range nodes {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			task := &Task{ID: taskID, TenantID: tenantID}
			claims[i], errs[i] = ClaimTaskSpawn(ctx, nodes[i], task, jobIDs[i], int64(1_000+i))
		}(i)
	}
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("node %d: ClaimTaskSpawn: %v", i, err)
		}
	}
	won := 0
	for _, claim := range claims {
		if claim.Claimed {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("exactly one node may claim the task, %d did: %+v", won, claims)
	}
	if claims[0].JobID != claims[1].JobID {
		t.Fatalf("both nodes must report the task's one job: %q and %q", claims[0].JobID, claims[1].JobID)
	}

	stored, err := nodes[1].GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stored.SpawnedJobID != claims[0].JobID {
		t.Fatalf("the record names job %q, the claims named %q", stored.SpawnedJobID, claims[0].JobID)
	}
	if stored.Attempts != 1 {
		t.Fatalf("one dispatch charges one attempt, got %d", stored.Attempts)
	}
}

// lostSwapKV loses every guarded edit, as a store does when another writer
// commits between each read and write.
type lostSwapKV struct {
	createOnlyKV
}

func (k *lostSwapKV) CompareAndSwap(context.Context, string, []byte, []byte) (bool, error) {
	return false, nil
}

// TestTaskStore_EditsRefuseWhatTheStoreCannotDecide is the error half of
// #719: an edit is never written unguarded. A store that cannot decide a swap
// is refused outright, and a swap lost on every attempt is reported as a
// conflict with the record left as it was.
func TestTaskStore_EditsRefuseWhatTheStoreCannotDecide(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const tenantID, taskID = "tenant-unguarded", "task-unguarded"
	seed := func(t *testing.T, kv interfaces.KVStore) (TaskStore, *Task) {
		t.Helper()
		store := NewTaskStore(kv, NewProcessClaimArbiter())
		if err := store.CreateTask(ctx, &Task{
			ID: taskID, TenantID: tenantID, Subject: "edited", Status: TaskStatusPending,
		}); err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
		task, err := store.GetTask(ctx, tenantID, taskID)
		if err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		return store, task
	}

	t.Run("no guarded swap", func(t *testing.T) {
		t.Parallel()
		store, task := seed(t, &createOnlyKV{KVStore: NewMemoryTaskKV()})
		task.Subject = "overwritten"
		if err := store.UpdateTask(ctx, task); !errors.Is(err, ErrTaskStoreInvalidInput) {
			t.Fatalf("an edit over a store that cannot guard a swap: got %v", err)
		}
	})

	t.Run("swap always lost", func(t *testing.T) {
		t.Parallel()
		store, task := seed(t, &lostSwapKV{createOnlyKV{KVStore: NewMemoryTaskKV()}})
		edit := *task
		edit.Subject = "overwritten"
		if err := store.UpdateTask(ctx, &edit); !errors.Is(err, ErrTaskVersionConflict) {
			t.Fatalf("an edit that never lands must be a conflict: got %v", err)
		}
		if edit.Version != task.Version {
			t.Fatalf("a failed edit must leave the caller's version alone: %d, was %d", edit.Version, task.Version)
		}
		stored, err := store.GetTask(ctx, tenantID, taskID)
		if err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		if stored.Subject != "edited" || stored.Version != task.Version {
			t.Fatalf("a lost edit writes nothing: %+v", stored)
		}
	})
}
