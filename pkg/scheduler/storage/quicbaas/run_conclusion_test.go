package quicbaas

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// swappingKV is the mock store with the compare-and-swap the run guard
// needs. beforeSwap, when set, runs once as the first swap arrives: whatever
// it does happened between ConcludeRun's read and its swap.
type swappingKV struct {
	*mockKVStore
	beforeSwap func()
	swaps      int
}

func (k *swappingKV) CompareAndSwap(_ context.Context, key string, expected, next []byte) (bool, error) {
	if hook := k.beforeSwap; hook != nil {
		k.beforeSwap = nil
		hook()
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.swaps++
	held, ok := k.data[key]
	if !ok || !bytes.Equal(held, expected) {
		return false, nil
	}
	k.data[key] = append([]byte(nil), next...)
	return true, nil
}

func newSwappingStorage() (*ClusteredStorage, *swappingKV) {
	kv := &swappingKV{mockKVStore: newMockKVStore()}
	return &ClusteredStorage{Storage: &Storage{kv: kv}}, kv
}

const runTenant = "tenant-a"

// putRunningJob stores a job Running the run started at started.
func putRunningJob(t *testing.T, kv *swappingKV, id uint64, started time.Time) {
	t.Helper()
	job := &scheduler.Job{
		ID: id, OwnerType: scheduler.OwnerTypeTenant, OwnerID: runTenant,
		Status: scheduler.JobStatusRunning, RunStartedAt: started,
	}
	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := kv.Put(context.Background(), keyForJobByID(id, runTenant), data); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

func heldJob(t *testing.T, kv *swappingKV, id uint64) (*scheduler.Job, bool) {
	t.Helper()
	data, err := kv.Get(context.Background(), keyForJobByID(id, runTenant))
	if err != nil {
		return nil, false
	}
	var job scheduler.Job
	if err := json.Unmarshal(data, &job); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &job, true
}

// concludeAs is the transition an outcome of run takes: it holds only while
// the stored record still runs that run, unconcluded.
func concludeAs(run *scheduler.RunConclusion, owner string) scheduler.RunTransition {
	return func(held *scheduler.Job) (*scheduler.Job, bool) {
		if held.Status != scheduler.JobStatusRunning || !held.RunStartedAt.Equal(run.StartedAt) ||
			held.ConcludedRunAt.Equal(run.StartedAt) {
			return nil, false
		}
		next := *held
		next.Status, next.RunCount = scheduler.JobStatusPending, held.RunCount+1
		next.ConcludedRunAt, next.ConcludedBy = run.StartedAt, owner
		return &next, true
	}
}

func runOf(id uint64, started time.Time) *scheduler.RunConclusion {
	return &scheduler.RunConclusion{JobID: id, TenantID: runTenant, Firing: started, StartedAt: started}
}

// The first writer of a run concludes it in the job record itself; a second
// writer of the same run then loses, and the record carries one outcome.
func TestConcludeRun_OneWriterPerRun(t *testing.T) {
	storage, kv := newSwappingStorage()
	started := time.Unix(1_800_000_000, 0)
	putRunningJob(t, kv, 1, started)
	run := runOf(1, started)

	if won, err := storage.ConcludeRun(context.Background(), run, concludeAs(run, "recovery@node-2")); err != nil || !won {
		t.Fatalf("first writer: won=%v err=%v", won, err)
	}
	if won, err := storage.ConcludeRun(context.Background(), run, concludeAs(run, "execution@node-1")); err != nil || won {
		t.Fatalf("second writer of the same run: won=%v err=%v, want a loss", won, err)
	}
	job, ok := heldJob(t, kv, 1)
	if !ok || job.Status != scheduler.JobStatusPending || job.RunCount != 1 || job.ConcludedBy != "recovery@node-2" {
		t.Fatalf("record after two writers: %+v", job)
	}
}

// The outcome and the arbitration are one write: once ConcludeRun wins, the
// job's transition is stored, so no window leaves a won run still Running.
func TestConcludeRun_TheWinWritesTheTransition(t *testing.T) {
	storage, kv := newSwappingStorage()
	started := time.Unix(1_800_000_000, 0)
	putRunningJob(t, kv, 2, started)
	run := runOf(2, started)
	if won, err := storage.ConcludeRun(context.Background(), run, concludeAs(run, "o")); err != nil || !won {
		t.Fatalf("ConcludeRun: won=%v err=%v", won, err)
	}
	if job, _ := heldJob(t, kv, 2); job.Status == scheduler.JobStatusRunning || !job.ConcludedRunAt.Equal(started) {
		t.Fatalf("a won run is still open: %+v", job)
	}
}

// A run is named by when it started, not by its firing: a retry started
// later than an early trigger concludes even though it fires earlier.
func TestConcludeRun_ARetryOfAnEarlyTriggerConcludes(t *testing.T) {
	storage, kv := newSwappingStorage()
	now := time.Unix(1_800_000_000, 0)
	retryStarted := now.Add(time.Minute)
	putRunningJob(t, kv, 3, retryStarted)
	retry := &scheduler.RunConclusion{JobID: 3, TenantID: runTenant, Firing: now.Add(30 * time.Second), StartedAt: retryStarted}
	if won, err := storage.ConcludeRun(context.Background(), retry, concludeAs(retry, "o")); err != nil || !won {
		t.Fatalf("the retry did not conclude: won=%v err=%v", won, err)
	}
	// The early trigger's own outcome, named by its earlier start, loses.
	early := &scheduler.RunConclusion{JobID: 3, TenantID: runTenant, Firing: now.Add(time.Hour), StartedAt: now}
	if won, err := storage.ConcludeRun(context.Background(), early, concludeAs(early, "o")); err != nil || won {
		t.Fatalf("a superseded run concluded: won=%v err=%v", won, err)
	}
}

// A job force-deleted before or during the conclusion is never recreated.
func TestConcludeRun_AForceDeletedJobIsNotRecreated(t *testing.T) {
	started := time.Unix(1_800_000_000, 0)
	t.Run("deleted before", func(t *testing.T) {
		storage, kv := newSwappingStorage()
		run := runOf(4, started)
		if won, err := storage.ConcludeRun(context.Background(), run, concludeAs(run, "o")); err != nil || won {
			t.Fatalf("an absent job concluded: won=%v err=%v", won, err)
		}
		if _, ok := heldJob(t, kv, 4); ok {
			t.Fatal("the conclusion created the job")
		}
	})
	t.Run("deleted between the read and the swap", func(t *testing.T) {
		storage, kv := newSwappingStorage()
		putRunningJob(t, kv, 5, started)
		kv.beforeSwap = func() {
			if err := storage.Storage.DeleteJob(context.Background(), 5, runTenant); err != nil {
				t.Errorf("DeleteJob: %v", err)
			}
		}
		run := runOf(5, started)
		if won, err := storage.ConcludeRun(context.Background(), run, concludeAs(run, "recovery@node-2")); err != nil || won {
			t.Fatalf("a deleted job's run concluded: won=%v err=%v", won, err)
		}
		if _, ok := heldJob(t, kv, 5); ok {
			t.Fatal("the conclusion recreated a force-deleted job")
		}
	})
}

// A record changed between the read and the swap is decided again against
// what storage now holds.
func TestConcludeRun_AContendedSwapDecidesAgain(t *testing.T) {
	storage, kv := newSwappingStorage()
	started := time.Unix(1_800_000_000, 0)
	putRunningJob(t, kv, 6, started)
	run := runOf(6, started)
	kv.beforeSwap = func() {
		if won, err := storage.ConcludeRun(context.Background(), run, concludeAs(run, "execution@node-1")); err != nil || !won {
			t.Errorf("the contender: won=%v err=%v", won, err)
		}
	}
	if won, err := storage.ConcludeRun(context.Background(), run, concludeAs(run, "recovery@node-2")); err != nil || won {
		t.Fatalf("the writer that lost the swap: won=%v err=%v", won, err)
	}
	if job, _ := heldJob(t, kv, 6); job.ConcludedBy != "execution@node-1" || job.RunCount != 1 {
		t.Fatalf("record after a contended swap: %+v", job)
	}
}

func TestConcludeRun_RefusesAnIncompleteRun(t *testing.T) {
	storage, _ := newSwappingStorage()
	started := time.Unix(1_800_000_000, 0)
	accept := func(held *scheduler.Job) (*scheduler.Job, bool) { return held, true }
	for name, tc := range map[string]struct {
		run        *scheduler.RunConclusion
		transition scheduler.RunTransition
	}{
		"nil":           {nil, accept},
		"no job":        {runOf(0, started), accept},
		"no started":    {runOf(9, time.Time{}), accept},
		"no transition": {runOf(9, started), nil},
	} {
		_, err := storage.ConcludeRun(context.Background(), tc.run, tc.transition)
		var storageErr *scheduler.ErrStorageError
		if !errors.As(err, &storageErr) {
			t.Errorf("%s: err = %v, want a storage error", name, err)
		}
	}
}

// A store with no compare-and-swap cannot guard a run, so it refuses.
func TestConcludeRun_RefusesAStoreWithoutCompareAndSwap(t *testing.T) {
	storage := &ClusteredStorage{Storage: &Storage{kv: newMockKVStore()}}
	run := runOf(10, time.Unix(1_800_000_000, 0))
	_, err := storage.ConcludeRun(context.Background(), run, concludeAs(run, "o"))
	if !errors.Is(err, ErrNoCompareAndSwap) {
		t.Fatalf("err = %v, want ErrNoCompareAndSwap", err)
	}
}
