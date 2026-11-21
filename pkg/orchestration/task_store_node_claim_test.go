package orchestration

import (
	"context"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// interleavingKV runs `between` once, just before this node's first write of
// a task record: the moment after it read the record and before it commits.
// That is the window two NODES race in, and recordMu (per process) cannot
// close it.
type interleavingKV struct {
	interfaces.KVStore
	once    sync.Once
	between func()
}

func (k *interleavingKV) Put(ctx context.Context, key string, value []byte) error {
	k.once.Do(k.between)
	return k.KVStore.Put(ctx, key, value)
}

func (k *interleavingKV) CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error) {
	k.once.Do(k.between)
	return forwardCompareAndSwap(ctx, k.KVStore, key, expected, next)
}

func (k *interleavingKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

func (k *interleavingKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	return forwardCompareAndDelete(ctx, k.KVStore, key, expected)
}

// TestClaimTaskSpawn_TwoNodesClaimOnce is #722: two nodes share one KV, each
// with its own task store (and so its own process-local record lock). Node B
// claims the pending task between node A's read and node A's write. Node A's
// write must lose to the store's compare-and-swap, not overwrite B's claim.
func TestClaimTaskSpawn_TwoNodesClaimOnce(t *testing.T) {
	t.Parallel()
	const tenantID, taskID = "tenant-nodes", "task-nodes"
	ctx := context.Background()

	shared := NewMemoryTaskKV()
	nodeB := NewTaskStore(shared, nil)
	if err := nodeB.CreateTask(ctx, &Task{
		ID: taskID, TenantID: tenantID, Subject: "claimable",
		Status: TaskStatusPending, AutoSpawn: true,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	var claimB TaskSpawnClaim
	var errB error
	kvA := &interleavingKV{KVStore: shared}
	kvA.between = func() {
		taskB, err := nodeB.GetTask(ctx, tenantID, taskID)
		if err != nil {
			errB = err
			return
		}
		claimB, errB = ClaimTaskSpawn(ctx, nodeB, taskB, "job-b", 2_000)
	}
	nodeA := NewTaskStore(kvA, nil)

	taskA, err := nodeA.GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	claimA, errA := ClaimTaskSpawn(ctx, nodeA, taskA, "job-a", 1_000)
	if errA != nil || errB != nil {
		t.Fatalf("ClaimTaskSpawn: node A %v, node B %v", errA, errB)
	}

	if !claimB.Claimed {
		t.Fatalf("node B claimed first and must win: %+v", claimB)
	}
	if claimA.Claimed {
		t.Fatalf("node A claimed a task node B already holds: A %+v, B %+v", claimA, claimB)
	}
	if claimA.JobID != "job-b" {
		t.Fatalf("node A must observe node B's attempt, got job %q", claimA.JobID)
	}

	stored, err := nodeB.GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stored.SpawnedJobID != "job-b" || stored.Attempts != 1 {
		t.Fatalf("the record must carry node B's one attempt: job %q, attempts %d", stored.SpawnedJobID, stored.Attempts)
	}
}
