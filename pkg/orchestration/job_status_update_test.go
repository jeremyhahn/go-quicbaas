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
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/cas"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// Node-backed job store
// =============================================================================

// nodeJobKV is the job store over the package's real qrdb node: reads through
// its KV plane, guarded swaps through its transaction plane. It is what lets
// the race below be decided by the store rather than by a double that agrees
// with the code under test.
type nodeJobKV struct {
	interfaces.KVStore
	reader cas.Reader
	writer *cas.Writer
}

func newNodeJobKV(t *testing.T) *nodeJobKV {
	t.Helper()
	n := claimNode(t)
	writer, err := cas.NewWriter(n.Transaction, n.KV, nil)
	if err != nil {
		t.Fatalf("cas.NewWriter: %v", err)
	}
	return &nodeJobKV{reader: n.KV, writer: writer}
}

func (k *nodeJobKV) Put(ctx context.Context, key string, value []byte) error {
	return k.reader.Put(ctx, key, value)
}

func (k *nodeJobKV) Get(ctx context.Context, key string) ([]byte, error) {
	return k.reader.Get(ctx, key)
}

func (k *nodeJobKV) CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error) {
	return k.writer.Swap(ctx, key, expected, next)
}

func (k *nodeJobKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return k.writer.Create(ctx, key, value)
}

// =============================================================================
// The race
// =============================================================================

// TestUpdateJobStatus_ConcurrentWritersAllLand is the go-quicbaas#837
// regression: writers released together each add a distinct child to one job
// record on a REAL transaction plane, and every child must be on the record at
// the end. A read-modify-Put keeps only the last writer's copy.
func TestUpdateJobStatus_ConcurrentWritersAllLand(t *testing.T) {
	// Within maxJobStatusSwapAttempts: a writer loses at most once per rival.
	const writers = 6
	kv := newNodeJobKV(t)
	svc := &agentServiceImpl{kvStore: kv, logger: &controlLoopMockLogger{}}
	tenantID := "acme"
	jobID := uniqueTaskID("job-837")
	seedJobStatus(t, svc, tenantID, &JobStatus{JobID: jobID, TenantID: tenantID, State: JobStateExecuting})

	var (
		wg       sync.WaitGroup
		start    = make(chan struct{})
		failures = make([]error, writers)
		want     = make([]string, writers)
	)
	for i := 0; i < writers; i++ {
		want[i] = fmt.Sprintf("child-%d", i)
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			failures[i] = svc.updateJobStatus(tenantID, jobID, func(status *JobStatus) bool {
				status.ChildJobIDs = append(status.ChildJobIDs, want[i])
				return true
			})
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range failures {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}
	got, err := svc.loadJobStatus(tenantID, jobID)
	if err != nil {
		t.Fatalf("loadJobStatus: %v", err)
	}
	children := append([]string(nil), got.ChildJobIDs...)
	sort.Strings(children)
	if fmt.Sprint(children) != fmt.Sprint(want) {
		t.Fatalf("children = %v, want every writer's %v", children, want)
	}
}

// =============================================================================
// Contention, fallback and failure
// =============================================================================

// interposingKV is a guarded store in which another writer commits between
// every read and swap, so no swap can ever win.
type interposingKV struct {
	*qrdbsdk.MemoryKVStore
	swaps atomic.Int32
}

func (k *interposingKV) CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error) {
	n := k.swaps.Add(1)
	rival, err := json.Marshal(&JobStatus{JobID: "job-1", Iteration: int(n)})
	if err != nil {
		return false, err
	}
	if err := k.MemoryKVStore.Put(ctx, key, rival); err != nil {
		return false, err
	}
	return k.MemoryKVStore.CompareAndSwap(ctx, key, expected, next)
}

func TestUpdateJobStatus_ExhaustedRetriesReturnTypedError(t *testing.T) {
	kv := &interposingKV{MemoryKVStore: qrdbsdk.NewMemoryKVStore()}
	svc := &agentServiceImpl{kvStore: kv, logger: &controlLoopMockLogger{}}
	seedJobStatus(t, svc, "acme", &JobStatus{JobID: "job-1"})

	err := svc.updateJobStatus("acme", "job-1", func(status *JobStatus) bool {
		status.ChildJobIDs = append(status.ChildJobIDs, "child")
		return true
	})

	var contended *JobStatusContendedError
	if !errors.As(err, &contended) {
		t.Fatalf("err = %v, want *JobStatusContendedError", err)
	}
	if contended.Attempts != maxJobStatusSwapAttempts {
		t.Errorf("attempts = %d, want %d", contended.Attempts, maxJobStatusSwapAttempts)
	}
	if !errors.Is(err, ErrJobStatusContended) || !errors.Is(err, ErrJobPersistenceFailed) {
		t.Errorf("err = %v, want it to match both sentinels", err)
	}
	if got := kv.swaps.Load(); got != maxJobStatusSwapAttempts {
		t.Errorf("swaps = %d, want one per attempt (%d)", got, maxJobStatusSwapAttempts)
	}
	stored, loadErr := svc.loadJobStatus("acme", "job-1")
	if loadErr != nil {
		t.Fatalf("loadJobStatus: %v", loadErr)
	}
	if len(stored.ChildJobIDs) != 0 {
		t.Errorf("children = %v, want the mutation NOT applied", stored.ChildJobIDs)
	}
}

// countingSwapKV counts guarded swaps and can fail them.
type countingSwapKV struct {
	*qrdbsdk.MemoryKVStore
	swaps   atomic.Int32
	swapErr error
}

func (k *countingSwapKV) CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error) {
	k.swaps.Add(1)
	if k.swapErr != nil {
		return false, k.swapErr
	}
	return k.MemoryKVStore.CompareAndSwap(ctx, key, expected, next)
}

func TestUpdateJobStatus_NothingToWriteSkipsTheSwap(t *testing.T) {
	kv := &countingSwapKV{MemoryKVStore: qrdbsdk.NewMemoryKVStore()}
	svc := &agentServiceImpl{kvStore: kv, logger: &controlLoopMockLogger{}}
	seedJobStatus(t, svc, "acme", &JobStatus{JobID: "job-1"})

	if err := svc.updateJobStatus("acme", "job-1", func(*JobStatus) bool { return false }); err != nil {
		t.Fatalf("updateJobStatus: %v", err)
	}
	if got := kv.swaps.Load(); got != 0 {
		t.Errorf("swaps = %d, want none for a mutation with nothing to write", got)
	}
}

func TestUpdateJobStatus_SwapFailureIsReturned(t *testing.T) {
	cause := errors.New("store unavailable")
	kv := &countingSwapKV{MemoryKVStore: qrdbsdk.NewMemoryKVStore(), swapErr: cause}
	svc := &agentServiceImpl{kvStore: kv, logger: &controlLoopMockLogger{}}
	seedJobStatus(t, svc, "acme", &JobStatus{JobID: "job-1"})

	err := svc.updateJobStatus("acme", "job-1", func(*JobStatus) bool { return true })

	var persistence *JobPersistenceError
	if !errors.As(err, &persistence) || !errors.Is(err, cause) {
		t.Fatalf("err = %v, want a JobPersistenceError wrapping the swap failure", err)
	}
	if jobStatusAbsent(err) {
		t.Error("a failed swap must not read as an absent job")
	}
}

func TestUpdateJobStatus_AbsentRecordReadsAsAbsent(t *testing.T) {
	svc := &agentServiceImpl{kvStore: kvstore.NewMemory(), logger: &controlLoopMockLogger{}}

	called := false
	err := svc.updateJobStatus("acme", "job-missing", func(*JobStatus) bool {
		called = true
		return true
	})

	if !jobStatusAbsent(err) {
		t.Fatalf("err = %v, want one jobStatusAbsent recognises", err)
	}
	if called {
		t.Error("mutate ran against a record the store does not hold")
	}
}

// TestUpdateJobStatus_UnguardedStoreIsRefused proves a store without the swap
// verb gets no write at all: a plain Put is the lost-update path this replaced.
func TestUpdateJobStatus_UnguardedStoreIsRefused(t *testing.T) {
	inner := newMockKVStore()
	seedJobStatus(t, &agentServiceImpl{kvStore: inner, logger: &controlLoopMockLogger{}},
		"acme", &JobStatus{JobID: "job-1"})
	svc := &agentServiceImpl{kvStore: &unguardedKV{KVStore: inner}, logger: &controlLoopMockLogger{}}

	called := false
	err := svc.updateJobStatus("acme", "job-1", func(status *JobStatus) bool {
		called = true
		status.ChildJobIDs = append(status.ChildJobIDs, "child")
		return true
	})

	var unguarded *JobStoreUnguardedError
	if !errors.As(err, &unguarded) || unguarded.JobID != "job-1" {
		t.Fatalf("err = %v, want *JobStoreUnguardedError for job-1", err)
	}
	if !errors.Is(err, ErrJobStoreUnguarded) || !errors.Is(err, ErrJobPersistenceFailed) {
		t.Errorf("err = %v, want it to match both sentinels", err)
	}
	if called {
		t.Error("mutate ran against a store that cannot commit it safely")
	}
	stored, loadErr := svc.loadJobStatus("acme", "job-1")
	if loadErr != nil {
		t.Fatalf("loadJobStatus: %v", loadErr)
	}
	if len(stored.ChildJobIDs) != 0 {
		t.Errorf("children = %v, want the record untouched", stored.ChildJobIDs)
	}
}

func TestUpdateJobStatus_NoStoreIsANoOp(t *testing.T) {
	svc := &agentServiceImpl{logger: &controlLoopMockLogger{}}
	if err := svc.updateJobStatus("acme", "job-1", func(*JobStatus) bool { return true }); err != nil {
		t.Fatalf("updateJobStatus without a store = %v, want nil", err)
	}
}

// =============================================================================
// Fields this build does not know
// =============================================================================

// TestUpdateJobStatus_UnknownFieldSurvivesAnAmendment stores a record carrying
// a field JobStatus does not declare — what a newer daemon sharing the store
// writes — and amends it: the field must still be on the record afterwards.
func TestUpdateJobStatus_UnknownFieldSurvivesAnAmendment(t *testing.T) {
	kv := qrdbsdk.NewMemoryKVStore()
	svc := &agentServiceImpl{kvStore: kv, logger: &controlLoopMockLogger{}}
	key := jobPersistenceKey("acme", "job-1")
	stored := []byte(`{"job_id":"job-1","state":"executing","future_field":{"n":7}}`)
	if err := kv.Put(context.Background(), key, stored); err != nil {
		t.Fatalf("Put: %v", err)
	}

	err := svc.updateJobStatus("acme", "job-1", func(status *JobStatus) bool {
		status.ChildJobIDs = append(status.ChildJobIDs, "child")
		return true
	})
	if err != nil {
		t.Fatalf("updateJobStatus: %v", err)
	}

	raw, err := kv.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var record map[string]json.RawMessage
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("stored record is not a JSON object: %v", err)
	}
	if got := string(record["future_field"]); got != `{"n":7}` {
		t.Errorf("future_field = %q, want the stored {\"n\":7} kept", got)
	}
	if got := string(record["child_job_ids"]); got != `["child"]` {
		t.Errorf("child_job_ids = %q, want the amendment applied", got)
	}
}

// TestEncodeJobStatusOver_ClearedFieldDoesNotKeepItsStoredValue pins the half
// of the merge that is easy to get wrong: a field the status has emptied is
// omitted from its encoding, and must not fall through to the stored value.
func TestEncodeJobStatusOver_ClearedFieldDoesNotKeepItsStoredValue(t *testing.T) {
	held := []byte(`{"job_id":"job-1","session_ids":["s1"],"future_field":1}`)

	next, err := encodeJobStatusOver(held, &JobStatus{JobID: "job-1"})
	if err != nil {
		t.Fatalf("encodeJobStatusOver: %v", err)
	}

	var record map[string]json.RawMessage
	if err := json.Unmarshal(next, &record); err != nil {
		t.Fatalf("encoded record is not a JSON object: %v", err)
	}
	if ids, ok := record["session_ids"]; ok {
		t.Errorf("session_ids = %s, want it gone with the status's empty list", ids)
	}
	if got := string(record["future_field"]); got != "1" {
		t.Errorf("future_field = %q, want it kept", got)
	}
}

func TestEncodeJobStatusOver_MalformedHeldRecordIsAnError(t *testing.T) {
	if _, err := encodeJobStatusOver([]byte(`["not","an","object"]`), &JobStatus{JobID: "job-1"}); err == nil {
		t.Fatal("encodeJobStatusOver over a non-object record = nil error, want one")
	}
}

func TestEncodeJobStatusOver_NoHeldRecordEncodesTheStatus(t *testing.T) {
	next, err := encodeJobStatusOver(nil, &JobStatus{JobID: "job-1", State: JobStateExecuting})
	if err != nil {
		t.Fatalf("encodeJobStatusOver: %v", err)
	}
	var decoded JobStatus
	if err := json.Unmarshal(next, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.JobID != "job-1" || decoded.State != JobStateExecuting {
		t.Errorf("decoded = %+v, want the status round-tripped", decoded)
	}
}

// =============================================================================
// Whole-snapshot writes
// =============================================================================

// TestStoreJobStatus_SnapshotKeepsTheAmendments is the snapshot half of
// go-quicbaas#837: the job's run publishes a snapshot built before a child was
// spawned and before a session concluded, and neither amendment may be undone
// by it, while every field the run owns still lands.
func TestStoreJobStatus_SnapshotKeepsTheAmendments(t *testing.T) {
	svc := &agentServiceImpl{kvStore: newMockKVStore(), logger: &controlLoopMockLogger{}}
	seedJobStatus(t, svc, "acme", &JobStatus{
		JobID: "job-1", State: JobStateExecuting, SessionIDs: []string{"s1", "s2"},
	})
	concluded := JobAuditEvent{Timestamp: 20, Type: JobAuditTypeSessionConcluded, Details: "session s1 concluded: exited"}
	err := svc.updateJobStatus("acme", "job-1", func(status *JobStatus) bool {
		status.ChildJobIDs = append(status.ChildJobIDs, "child-1")
		status.ParentJobID, status.SpawnedBy, status.SpawnedAt = "parent", "agent-1", 5
		status.SessionIDs = []string{"s2"}
		status.Events = append(status.Events, concluded)
		return true
	})
	if err != nil {
		t.Fatalf("updateJobStatus: %v", err)
	}

	own := JobAuditEvent{Timestamp: 30, Type: "iteration", Details: "iteration 2"}
	snapshot := &JobStatus{
		JobID: "job-1", State: JobStateCompleted, Iteration: 2,
		SessionIDs: []string{"s1", "s2"}, Events: []JobAuditEvent{own},
	}
	if err := svc.storeJobStatus("acme", snapshot); err != nil {
		t.Fatalf("storeJobStatus: %v", err)
	}

	got, err := svc.loadJobStatus("acme", "job-1")
	if err != nil {
		t.Fatalf("loadJobStatus: %v", err)
	}
	if got.State != JobStateCompleted || got.Iteration != 2 {
		t.Errorf("state, iteration = %s, %d, want the snapshot's completed, 2", got.State, got.Iteration)
	}
	if fmt.Sprint(got.ChildJobIDs) != "[child-1]" {
		t.Errorf("children = %v, want the spawned child kept", got.ChildJobIDs)
	}
	if got.ParentJobID != "parent" || got.SpawnedBy != "agent-1" || got.SpawnedAt != 5 {
		t.Errorf("lineage = %q, %q, %d, want the stored lineage kept", got.ParentJobID, got.SpawnedBy, got.SpawnedAt)
	}
	if fmt.Sprint(got.SessionIDs) != "[s2]" {
		t.Errorf("sessions = %v, want the concluded s1 kept off", got.SessionIDs)
	}
	if fmt.Sprint(got.Events) != fmt.Sprint([]JobAuditEvent{concluded, own}) {
		t.Errorf("events = %v, want the conclusion and the run's own event in time order", got.Events)
	}
	if fmt.Sprint(snapshot.SessionIDs) != "[s1 s2]" || len(snapshot.Events) != 1 {
		t.Errorf("the caller's snapshot was modified: %+v", snapshot)
	}
}

// TestStoreJobStatus_ConcurrentSnapshotsAndAmendmentsAllLand races snapshot
// writes against child spawns on a REAL transaction plane: every child must be
// on the record at the end, whichever write the store took last.
func TestStoreJobStatus_ConcurrentSnapshotsAndAmendmentsAllLand(t *testing.T) {
	const spawners = 3
	kv := newNodeJobKV(t)
	svc := &agentServiceImpl{kvStore: kv, logger: &controlLoopMockLogger{}}
	tenantID := "acme"
	jobID := uniqueTaskID("job-837-snap")
	seedJobStatus(t, svc, tenantID, &JobStatus{JobID: jobID, TenantID: tenantID, State: JobStateExecuting})

	var (
		wg       sync.WaitGroup
		start    = make(chan struct{})
		failures = make([]error, 2*spawners)
		want     = make([]string, spawners)
	)
	for i := 0; i < spawners; i++ {
		want[i] = fmt.Sprintf("child-%d", i)
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			<-start
			failures[i] = svc.updateJobStatus(tenantID, jobID, func(status *JobStatus) bool {
				status.ChildJobIDs = append(status.ChildJobIDs, want[i])
				return true
			})
		}(i)
		go func(i int) {
			defer wg.Done()
			<-start
			failures[spawners+i] = svc.storeJobStatus(tenantID, &JobStatus{
				JobID: jobID, TenantID: tenantID, State: JobStateExecuting, Iteration: i + 1,
			})
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range failures {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}
	got, err := svc.loadJobStatus(tenantID, jobID)
	if err != nil {
		t.Fatalf("loadJobStatus: %v", err)
	}
	children := append([]string(nil), got.ChildJobIDs...)
	sort.Strings(children)
	if fmt.Sprint(children) != fmt.Sprint(want) {
		t.Fatalf("children = %v, want every spawn's %v", children, want)
	}
}

// lostCreateKV is a guarded store in which a rival creates the record between
// the first absent read and the create, so the create loses.
type lostCreateKV struct {
	*qrdbsdk.MemoryKVStore
	rival   []byte
	creates atomic.Int32
}

func (k *lostCreateKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	if k.creates.Add(1) == 1 {
		if err := k.MemoryKVStore.Put(ctx, key, k.rival); err != nil {
			return false, err
		}
	}
	return k.MemoryKVStore.PutIfAbsent(ctx, key, value)
}

// TestStoreJobStatus_ALostCreateMergesOntoTheWinner proves the first write is
// guarded too: a record that appeared after the read is merged with, not
// overwritten.
func TestStoreJobStatus_ALostCreateMergesOntoTheWinner(t *testing.T) {
	rival, err := json.Marshal(&JobStatus{JobID: "job-1", ChildJobIDs: []string{"child-1"}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	kv := &lostCreateKV{MemoryKVStore: qrdbsdk.NewMemoryKVStore(), rival: rival}
	svc := &agentServiceImpl{kvStore: kv, logger: &controlLoopMockLogger{}}

	if err := svc.storeJobStatus("acme", &JobStatus{JobID: "job-1", State: JobStateExecuting}); err != nil {
		t.Fatalf("storeJobStatus: %v", err)
	}

	got, err := svc.loadJobStatus("acme", "job-1")
	if err != nil {
		t.Fatalf("loadJobStatus: %v", err)
	}
	if got.State != JobStateExecuting || fmt.Sprint(got.ChildJobIDs) != "[child-1]" {
		t.Errorf("record = state %s, children %v; want the snapshot over the winner's child", got.State, got.ChildJobIDs)
	}
}

func TestStoreJobStatus_ExhaustedRetriesReturnTypedError(t *testing.T) {
	kv := &interposingKV{MemoryKVStore: qrdbsdk.NewMemoryKVStore()}
	svc := &agentServiceImpl{kvStore: kv, logger: &controlLoopMockLogger{}}
	seedJobStatus(t, svc, "acme", &JobStatus{JobID: "job-1"})

	err := svc.storeJobStatus("acme", &JobStatus{JobID: "job-1", State: JobStateCompleted})

	var contended *JobStatusContendedError
	if !errors.As(err, &contended) || contended.Attempts != maxJobStatusSwapAttempts {
		t.Fatalf("err = %v, want *JobStatusContendedError after %d attempts", err, maxJobStatusSwapAttempts)
	}
}

// TestStoreJobStatus_UnguardedStoreIsRefused proves a snapshot gets no plain
// Put either: the record is left exactly as it was.
func TestStoreJobStatus_UnguardedStoreIsRefused(t *testing.T) {
	inner := newMockKVStore()
	seedJobStatus(t, &agentServiceImpl{kvStore: inner, logger: &controlLoopMockLogger{}},
		"acme", &JobStatus{JobID: "job-1", State: JobStateExecuting})
	svc := &agentServiceImpl{kvStore: &unguardedKV{KVStore: inner}, logger: &controlLoopMockLogger{}}

	err := svc.storeJobStatus("acme", &JobStatus{JobID: "job-1", State: JobStateCompleted})

	var unguarded *JobStoreUnguardedError
	if !errors.As(err, &unguarded) || unguarded.JobID != "job-1" {
		t.Fatalf("err = %v, want *JobStoreUnguardedError for job-1", err)
	}
	stored, loadErr := svc.loadJobStatus("acme", "job-1")
	if loadErr != nil {
		t.Fatalf("loadJobStatus: %v", loadErr)
	}
	if stored.State != JobStateExecuting {
		t.Errorf("state = %s, want the record untouched", stored.State)
	}
}

// TestStoreJobStatus_AnUnreadableRecordIsNotOverwritten pins that a record the
// store holds but this build cannot decode is reported, not replaced.
func TestStoreJobStatus_AnUnreadableRecordIsNotOverwritten(t *testing.T) {
	kv := newMockKVStore()
	svc := &agentServiceImpl{kvStore: kv, logger: &controlLoopMockLogger{}}
	key := jobPersistenceKey("acme", "job-1")
	if err := kv.Put(context.Background(), key, []byte("{not json")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	err := svc.storeJobStatus("acme", &JobStatus{JobID: "job-1"})

	var persistence *JobPersistenceError
	if !errors.As(err, &persistence) || jobStatusAbsent(err) {
		t.Fatalf("err = %v, want a JobPersistenceError that is not an absence", err)
	}
	raw, getErr := kv.Get(context.Background(), key)
	if getErr != nil || string(raw) != "{not json" {
		t.Errorf("record = (%q, %v), want it left as stored", raw, getErr)
	}
}

func TestMergeAuditTrails_KeepsARepeatedEventTwice(t *testing.T) {
	tick := JobAuditEvent{Timestamp: 1, Type: "tick"}
	got := mergeAuditTrails([]JobAuditEvent{tick}, []JobAuditEvent{tick, tick})
	if len(got) != 2 {
		t.Fatalf("events = %v, want the stored second tick added once", got)
	}
}

func TestMergeAuditTrails_NothingMissingReturnsOwn(t *testing.T) {
	own := []JobAuditEvent{{Timestamp: 2, Type: "b"}, {Timestamp: 1, Type: "a"}}
	got := mergeAuditTrails(own, own[:1])
	if fmt.Sprint(got) != fmt.Sprint(own) {
		t.Fatalf("events = %v, want own unchanged and unsorted %v", got, own)
	}
}

func TestSessionHasConclusionEvent_DoesNotMatchALongerSessionID(t *testing.T) {
	status := &JobStatus{Events: []JobAuditEvent{
		{Type: JobAuditTypeSessionConcluded, Details: "session s10 concluded: exited"},
	}}
	if sessionHasConclusionEvent(status, "s1") {
		t.Error("s10's ending matched session s1")
	}
	if !sessionHasConclusionEvent(status, "s10") {
		t.Error("s10's ending did not match session s10")
	}
}
