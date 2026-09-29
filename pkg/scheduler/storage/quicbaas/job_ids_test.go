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

package quicbaas

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// =============================================================================
// Job ID allocation
// =============================================================================

// idTestConfig is a scheduler configuration whose tick never fires during a
// test, so an engine started here only allocates.
func idTestConfig() *scheduler.SchedulerConfig {
	cfg := scheduler.DefaultConfig()
	cfg.TickInterval = time.Hour
	return cfg
}

// futureJob returns a one-shot job that will not come due during a test.
func futureJob(name string) *scheduler.Job {
	return &scheduler.Job{
		Name:      name,
		Type:      scheduler.JobTypeOneShot,
		Handler:   "noop",
		OwnerType: scheduler.OwnerTypeTenant,
		OwnerID:   "acme",
		ExecuteAt: time.Now().Add(24 * time.Hour),
	}
}

// startEngine starts engine and stops it when the test ends.
func startEngine(t *testing.T, engine *scheduler.SchedulerEngine) {
	t.Helper()
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := engine.Stop(ctx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
}

// scheduleIDs schedules n jobs on engine and returns their IDs.
func scheduleIDs(t *testing.T, engine *scheduler.SchedulerEngine, n int) []uint64 {
	t.Helper()
	ids := make([]uint64, 0, n)
	for i := 0; i < n; i++ {
		job := futureJob(fmt.Sprintf("job-%d", i))
		if err := engine.ScheduleJob(context.Background(), job); err != nil {
			t.Fatalf("ScheduleJob: %v", err)
		}
		ids = append(ids, job.ID)
	}
	return ids
}

// storedCounter reads the persisted counter record from kv.
func storedCounter(t *testing.T, kv *mockKVStore) uint64 {
	t.Helper()
	data, err := kv.Get(context.Background(), JobIDCounterKey)
	if err != nil {
		t.Fatalf("reading the counter: %v", err)
	}
	last, err := decodeJobIDCounter(data)
	if err != nil {
		t.Fatalf("decoding the counter: %v", err)
	}
	return last
}

// TestJobIDs_ARestartedEngineDoesNotReuseIDs is the defect: a restarted node's
// counter started from zero and reissued the IDs of existing jobs, and
// CreateJob overwrote them. A fresh engine over the same store must continue
// above every ID the previous one handed out, and the first job must survive.
func TestJobIDs_ARestartedEngineDoesNotReuseIDs(t *testing.T) {
	kv := newMockKVStore()

	first := scheduler.New(idTestConfig(), NewStorage(kv))
	startEngine(t, first)
	before := scheduleIDs(t, first, 3)

	// A restart: a new engine over a new Storage, sharing nothing in memory
	// with the first — only the store.
	restarted := scheduler.New(idTestConfig(), NewStorage(kv))
	startEngine(t, restarted)
	after := scheduleIDs(t, restarted, 3)

	highest := before[len(before)-1]
	for _, id := range after {
		if id <= highest {
			t.Fatalf("restarted engine reissued ID %d (previous engine reached %d)", id, highest)
		}
	}

	job, err := NewStorage(kv).GetJob(context.Background(), before[0], "acme")
	if err != nil {
		t.Fatalf("GetJob(%d): %v", before[0], err)
	}
	if job.Name != "job-0" {
		t.Errorf("job %d was overwritten: name %q", before[0], job.Name)
	}
}

// TestJobIDs_EveryAllocationIsPersisted pins that the counter is written on
// each allocation, not periodically: an allocation that is not durable is one
// a crash can hand out again.
func TestJobIDs_EveryAllocationIsPersisted(t *testing.T) {
	kv := newMockKVStore()
	storage := NewStorage(kv)
	ctx := context.Background()

	for want := uint64(1); want <= 3; want++ {
		job := futureJob("persisted")
		if err := storage.CreateJob(ctx, job); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}
		if job.ID != want {
			t.Fatalf("allocated ID %d, want %d", job.ID, want)
		}
		if got := storedCounter(t, kv); got != want {
			t.Fatalf("persisted counter %d after allocating %d", got, want)
		}
	}
}

// TestJobIDs_SeedsAboveJobsThatPredateTheCounter covers the upgrade: a store
// written before the counter was persisted holds jobs and no counter. Starting
// from zero would reissue their IDs, so the first initialisation seeds from
// the highest ID in use — in any tenant — and ignores fixed system-job IDs and
// keys that are not job records.
func TestJobIDs_SeedsAboveJobsThatPredateTheCounter(t *testing.T) {
	kv := newMockKVStore()
	ctx := context.Background()
	for _, key := range []string{
		SystemJobsPrefix + "5",
		fmt.Sprintf(TenantJobsPrefixFmt, "acme") + "41",
		fmt.Sprintf(TenantJobsPrefixFmt, "beta") + "12",
		SystemJobsPrefix + strconv.FormatUint(scheduler.SystemJobIDFloor+1, 10),
		"t:acme:app/myscheduler/jobs/900",                       // not a job record
		fmt.Sprintf(TenantExecutionsPrefixFmt, "acme") + "77/1", // an execution
	} {
		if err := kv.Put(ctx, key, []byte("{}")); err != nil {
			t.Fatalf("Put(%s): %v", key, err)
		}
	}

	storage := NewStorage(kv)
	if err := storage.InitializeJobIDCounter(ctx); err != nil {
		t.Fatalf("InitializeJobIDCounter: %v", err)
	}
	if got := storedCounter(t, kv); got != 41 {
		t.Fatalf("seeded counter %d, want 41", got)
	}

	job := futureJob("after-seed")
	if err := storage.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if job.ID != 42 {
		t.Errorf("first allocation after seeding = %d, want 42", job.ID)
	}
}

// TestJobIDs_AnEmptyCounterIsSeededNotZeroed covers stores that answer a
// missing key with no bytes rather than a not-found error: the empty counter
// is treated as absent, which means seeded from the job records — never
// restarted from zero over them.
func TestJobIDs_AnEmptyCounterIsSeededNotZeroed(t *testing.T) {
	kv := newMockKVStore()
	ctx := context.Background()
	kv.data[JobIDCounterKey] = []byte{}
	kv.data[fmt.Sprintf(TenantJobsPrefixFmt, "acme")+"17"] = []byte("{}")

	storage := NewStorage(kv)
	if err := storage.InitializeJobIDCounter(ctx); err != nil {
		t.Fatalf("InitializeJobIDCounter: %v", err)
	}
	job := futureJob("after-empty")
	if err := storage.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if job.ID != 18 {
		t.Errorf("first allocation over an empty counter = %d, want 18", job.ID)
	}
}

// TestJobIDs_ConcurrentLocalAllocationsAreUnique pins that the single-node
// allocator serializes the increment with the persist: no two callers get the
// same ID, and the durable counter ends at the highest one handed out.
func TestJobIDs_ConcurrentLocalAllocationsAreUnique(t *testing.T) {
	kv := newMockKVStore()
	storage := NewStorage(kv)
	const n = 64

	ids := make(chan uint64, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job := futureJob("concurrent")
			if err := storage.CreateJob(context.Background(), job); err != nil {
				errs <- err
				return
			}
			ids <- job.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatalf("CreateJob: %v", err)
	}

	seen := make(map[uint64]bool, n)
	for id := range ids {
		if seen[id] {
			t.Fatalf("ID %d allocated twice", id)
		}
		seen[id] = true
	}
	if got := storedCounter(t, kv); got != n {
		t.Errorf("persisted counter %d, want %d", got, n)
	}
}

// TestJobIDs_AFailedPersistHandsOutNothing pins the order: the ID is returned
// only after the store accepted it. A failed write must neither return an ID
// nor skip one.
func TestJobIDs_AFailedPersistHandsOutNothing(t *testing.T) {
	kv := newMockKVStore()
	storage := NewStorage(kv)
	ctx := context.Background()
	if err := storage.InitializeJobIDCounter(ctx); err != nil {
		t.Fatalf("InitializeJobIDCounter: %v", err)
	}

	cause := errors.New("no leader")
	kv.putErr = cause
	job := futureJob("unpersisted")
	err := storage.CreateJob(ctx, job)
	var storageErr *scheduler.ErrStorageError
	if !errors.As(err, &storageErr) || storageErr.Op != "AllocateJobID.put" {
		t.Fatalf("expected an AllocateJobID.put storage error, got %T: %v", err, err)
	}
	if !errors.Is(err, cause) {
		t.Errorf("the storage error must carry its cause, got %v", err)
	}
	if job.ID != 0 {
		t.Errorf("a failed allocation assigned ID %d", job.ID)
	}

	kv.putErr = nil
	if err := storage.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if job.ID != 1 {
		t.Errorf("allocation after a failed persist = %d, want 1", job.ID)
	}
}

// TestJobIDs_InitializationFailuresAreTyped covers every way the counter can
// fail to load. Each must be a storage error, never a silent start from zero.
func TestJobIDs_InitializationFailuresAreTyped(t *testing.T) {
	cause := errors.New("store unavailable")
	tests := []struct {
		name    string
		arrange func(kv *mockKVStore)
		op      string
		is      error
	}{
		{
			name:    "read failure",
			arrange: func(kv *mockKVStore) { kv.getErr = cause },
			op:      "InitializeJobIDCounter.get",
			is:      cause,
		},
		{
			name: "non-numeric counter",
			arrange: func(kv *mockKVStore) {
				kv.data[JobIDCounterKey] = []byte(`"seven"`)
			},
			op: "InitializeJobIDCounter.decode",
		},
		{
			name:    "seed scan failure",
			arrange: func(kv *mockKVStore) { kv.scanErr = cause },
			op:      "InitializeJobIDCounter.scan",
			is:      cause,
		},
		{
			name:    "seed persist failure",
			arrange: func(kv *mockKVStore) { kv.putErr = cause },
			op:      "InitializeJobIDCounter.put",
			is:      cause,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kv := newMockKVStore()
			tt.arrange(kv)

			err := NewStorage(kv).InitializeJobIDCounter(context.Background())
			var storageErr *scheduler.ErrStorageError
			if !errors.As(err, &storageErr) {
				t.Fatalf("expected ErrStorageError, got %T: %v", err, err)
			}
			if storageErr.Op != tt.op {
				t.Errorf("Op = %q, want %q", storageErr.Op, tt.op)
			}
			if tt.is != nil && !errors.Is(err, tt.is) {
				t.Errorf("error %v does not carry %v", err, tt.is)
			}
		})
	}
}

// TestJobIDs_AStorageErrorAtInitFailsEngineStart pins that an engine whose
// store cannot produce the counter does not start, and says why with a typed
// error. A started engine would allocate from zero.
func TestJobIDs_AStorageErrorAtInitFailsEngineStart(t *testing.T) {
	kv := newMockKVStore()
	cause := errors.New("barrier sealed")
	kv.getErr = cause
	engine := scheduler.New(idTestConfig(), NewStorage(kv))

	err := engine.Start(context.Background())
	var storageErr scheduler.ErrStorageError
	if !errors.As(err, &storageErr) || storageErr.Op != "InitializeJobIDCounter" {
		t.Fatalf("expected an InitializeJobIDCounter storage error, got %T: %v", err, err)
	}
	if !errors.Is(err, cause) {
		t.Errorf("the start error must carry its cause, got %v", err)
	}
	if engine.IsRunning() {
		t.Fatal("an engine that could not load its counter reports running")
	}
	if err := engine.ScheduleJob(context.Background(), futureJob("refused")); !errors.As(err, new(scheduler.SchedulerStoppedError)) {
		t.Errorf("ScheduleJob on an unstarted engine = %v, want SchedulerStoppedError", err)
	}
}

// TestJobIDs_ExhaustionNeverReachesSystemIDs pins the disjointness of the two
// ranges: allocation refuses to hand out SystemJobIDFloor rather than giving a
// tenant job the ID of a system job.
func TestJobIDs_ExhaustionNeverReachesSystemIDs(t *testing.T) {
	kv := newMockKVStore()
	kv.data[JobIDCounterKey] = encodeJobIDCounter(scheduler.SystemJobIDFloor - 1)
	storage := NewStorage(kv)

	job := futureJob("one-too-many")
	err := storage.CreateJob(context.Background(), job)
	if !errors.Is(err, ErrJobIDSpaceExhausted) {
		t.Fatalf("expected ErrJobIDSpaceExhausted, got %v", err)
	}
	if job.ID != 0 {
		t.Errorf("exhausted allocation assigned ID %d", job.ID)
	}
}

// =============================================================================
// Clustered allocation, on a real qrdb node
// =============================================================================

// newClusteredStoreOver builds a clustered store over the shared node with the
// given job-record store.
func newClusteredStoreOver(t *testing.T, kv *mockKVStore) *ClusteredStorage {
	t.Helper()
	n := node(t)
	storage, err := NewClusteredStorage(kv, n.Transaction, n.KV)
	if err != nil {
		t.Fatalf("NewClusteredStorage: %v", err)
	}
	return storage
}

// TestClusteredJobIDs_TwoEnginesNeverAllocateTheSameID is the cluster half of
// the defect: two nodes allocating at once must never both advance the counter
// from one value. Both engines share the job records and the claim store, as
// two nodes of one cluster do, and allocate concurrently through go-qrdb's own
// transaction plane.
func TestClusteredJobIDs_TwoEnginesNeverAllocateTheSameID(t *testing.T) {
	kv := newMockKVStore()
	engines := make([]*scheduler.SchedulerEngine, 2)
	for i := range engines {
		engine, err := scheduler.NewWithClustering(idTestConfig(), newClusteredStoreOver(t, kv), fmt.Sprintf("node-%d", i), nil)
		if err != nil {
			t.Fatalf("NewWithClustering: %v", err)
		}
		startEngine(t, engine)
		engines[i] = engine
	}

	const perEngine = 12
	ids := make(chan uint64, 2*perEngine)
	errs := make(chan error, 2*perEngine)
	var wg sync.WaitGroup
	for _, engine := range engines {
		for i := 0; i < perEngine; i++ {
			wg.Add(1)
			go func(e *scheduler.SchedulerEngine) {
				defer wg.Done()
				job := futureJob("clustered")
				if err := e.ScheduleJob(context.Background(), job); err != nil {
					errs <- err
					return
				}
				ids <- job.ID
			}(engine)
		}
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatalf("ScheduleJob: %v", err)
	}

	seen := make(map[uint64]bool, 2*perEngine)
	for id := range ids {
		if seen[id] {
			t.Fatalf("ID %d allocated by two engines", id)
		}
		seen[id] = true
	}
	if len(seen) != 2*perEngine {
		t.Errorf("allocated %d distinct IDs, want %d", len(seen), 2*perEngine)
	}
}

// TestClusteredJobIDs_ARestartedNodeContinues pins restart continuity on the
// clustered path: the counter lives in the claim store, so a fresh store and
// engine continue above what the previous one allocated.
func TestClusteredJobIDs_ARestartedNodeContinues(t *testing.T) {
	kv := newMockKVStore()

	first, err := scheduler.NewWithClustering(idTestConfig(), newClusteredStoreOver(t, kv), "node-a", nil)
	if err != nil {
		t.Fatalf("NewWithClustering: %v", err)
	}
	startEngine(t, first)
	before := scheduleIDs(t, first, 2)

	restarted, err := scheduler.NewWithClustering(idTestConfig(), newClusteredStoreOver(t, kv), "node-a", nil)
	if err != nil {
		t.Fatalf("NewWithClustering: %v", err)
	}
	startEngine(t, restarted)
	after := scheduleIDs(t, restarted, 2)

	if after[0] <= before[len(before)-1] {
		t.Fatalf("restarted clustered engine allocated %d, not above %d", after[0], before[len(before)-1])
	}
}

// TestClusteredJobIDs_SeedsAnAbsentCounterFromJobRecords covers the clustered
// upgrade: no counter in the claim store, existing jobs in the record store.
// The first node to initialise seeds from the highest ID in use.
func TestClusteredJobIDs_SeedsAnAbsentCounterFromJobRecords(t *testing.T) {
	ctx := context.Background()
	n := node(t)
	if err := n.KV.Delete(ctx, JobIDCounterKey); err != nil {
		t.Fatalf("clearing the counter: %v", err)
	}

	kv := newMockKVStore()
	const existing = 1 << 40
	key := fmt.Sprintf(TenantJobsPrefixFmt, "acme") + strconv.FormatUint(existing, 10)
	if err := kv.Put(ctx, key, []byte("{}")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	storage := newClusteredStoreOver(t, kv)
	if err := storage.InitializeJobIDCounter(ctx); err != nil {
		t.Fatalf("InitializeJobIDCounter: %v", err)
	}
	job := futureJob("after-seed")
	if err := storage.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if job.ID != existing+1 {
		t.Errorf("first clustered allocation = %d, want %d", job.ID, existing+1)
	}
}

// failingReader is the claim store's read path failing, which is how an
// unreachable or leaderless qrdb presents to the allocator.
type failingReader struct {
	transport.KVClient
	err error
}

// Get fails every read.
func (r failingReader) Get(context.Context, string, ...transport.ReadOption) ([]byte, error) {
	return nil, r.err
}

// TestClusteredJobIDs_AStorageErrorAtInitFailsEngineStart pins the clustered
// start: a claim store that cannot answer stops the engine with a typed error.
func TestClusteredJobIDs_AStorageErrorAtInitFailsEngineStart(t *testing.T) {
	n := node(t)
	cause := errors.New("no leader")
	storage, err := NewClusteredStorage(newMockKVStore(), n.Transaction, failingReader{KVClient: n.KV, err: cause})
	if err != nil {
		t.Fatalf("NewClusteredStorage: %v", err)
	}
	engine, err := scheduler.NewWithClustering(idTestConfig(), storage, "node-a", nil)
	if err != nil {
		t.Fatalf("NewWithClustering: %v", err)
	}

	err = engine.Start(context.Background())
	var storageErr *scheduler.ErrStorageError
	if !errors.As(err, &storageErr) || storageErr.Op != "AllocateJobID.read" {
		t.Fatalf("expected an AllocateJobID.read storage error, got %T: %v", err, err)
	}
	if !errors.Is(err, cause) {
		t.Errorf("the start error must carry its cause, got %v", err)
	}
	if engine.IsRunning() {
		t.Fatal("an engine that could not load its counter reports running")
	}
}

// TestClusteredJobIDs_AnUnreadableCounterFailsClosed pins that an empty
// counter record in the claim store is refused, not read as zero.
func TestClusteredJobIDs_AnUnreadableCounterFailsClosed(t *testing.T) {
	ctx := context.Background()
	n := node(t)
	if err := n.KV.Put(ctx, JobIDCounterKey, []byte{}); err != nil {
		t.Fatalf("planting an empty counter: %v", err)
	}
	t.Cleanup(func() {
		if err := n.KV.Delete(context.Background(), JobIDCounterKey); err != nil {
			t.Errorf("clearing the counter: %v", err)
		}
	})

	err := newClusteredStoreOver(t, newMockKVStore()).CreateJob(ctx, futureJob("refused"))
	if !errors.Is(err, ErrUnreadableJobIDCounter) {
		t.Fatalf("expected ErrUnreadableJobIDCounter, got %v", err)
	}
}
