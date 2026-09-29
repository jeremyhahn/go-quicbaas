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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Test doubles
// =============================================================================

// forwardPutIfAbsent delegates a guarded create to the wrapped store.
//
// EMBEDDING interfaces.KVStore DOES NOT CARRY THIS VERB. Embedding an interface
// promotes exactly that interface's method set, and the guarded verbs are a
// separate capability discovered by type assertion — so a wrapper is silently
// NOT a guardedCreator however atomic the store inside it is, and every create
// through it would be refused. Each wrapper below therefore forwards
// explicitly. This is the cost of capability-by-assertion, and it is worth
// naming: the alternative is a wrapper that quietly loses a guarantee its
// inner store provides.
func forwardPutIfAbsent(ctx context.Context, inner interfaces.KVStore, key string, value []byte) (bool, error) {
	creator, ok := inner.(guardedCreator)
	if !ok {
		return false, errors.New("test wrapper: inner store cannot guard a create")
	}
	return creator.PutIfAbsent(ctx, key, value)
}

// forwardCompareAndDelete delegates a guarded delete to the wrapped store, for
// the same reason forwardPutIfAbsent exists.
func forwardCompareAndDelete(ctx context.Context, inner interfaces.KVStore, key string, expected []byte) (bool, error) {
	deleter, ok := inner.(guardedDeleter)
	if !ok {
		return false, errors.New("test wrapper: inner store cannot guard a delete")
	}
	return deleter.CompareAndDelete(ctx, key, expected)
}

// forwardCompareAndSwap is forwardCompareAndDelete for the guarded edit, which
// every account record write now commits through.
func forwardCompareAndSwap(ctx context.Context, inner interfaces.KVStore, key string, expected, next []byte) (bool, error) {
	swapper, ok := inner.(guardedSwapper)
	if !ok {
		return false, errors.New("test wrapper: inner store cannot guard an edit")
	}
	return swapper.CompareAndSwap(ctx, key, expected, next)
}

// taskFaultKV injects a failure into one KV verb while delegating every other
// verb to the real store. It embeds interfaces.KVStore so a new verb added to
// the contract keeps compiling here and keeps behaving like the real store —
// but see forwardPutIfAbsent for the capability that embedding cannot carry.
type taskFaultKV struct {
	interfaces.KVStore

	putErr    error
	getErr    error
	deleteErr error
	scanErr   error
	listErr   error
	existsErr error

	// deletePrefix narrows deleteErr to keys under one prefix, which is how a
	// failure to reclaim an artifact payload is separated from a failure to
	// delete the record itself. An empty prefix fails every delete.
	deletePrefix string
}

func newTaskFaultKV() *taskFaultKV {
	return &taskFaultKV{KVStore: NewMemoryTaskKV()}
}

// PutIfAbsent forwards the guarded create, honouring the injected write
// failure: a create is a write, so a store failing writes must fail creates.
func (k *taskFaultKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	if k.putErr != nil {
		return false, k.putErr
	}
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

func (k *taskFaultKV) Put(ctx context.Context, key string, value []byte) error {
	if k.putErr != nil {
		return k.putErr
	}
	return k.KVStore.Put(ctx, key, value)
}

func (k *taskFaultKV) Get(ctx context.Context, key string) ([]byte, error) {
	if k.getErr != nil {
		return nil, k.getErr
	}
	return k.KVStore.Get(ctx, key)
}

func (k *taskFaultKV) Delete(ctx context.Context, key string) error {
	if k.deleteErr != nil && strings.HasPrefix(key, k.deletePrefix) {
		return k.deleteErr
	}
	return k.KVStore.Delete(ctx, key)
}

// taskCancelScanKV cancels the request context from inside Scan, which is how a
// caller giving up mid-listing looks to the store.
type taskCancelScanKV struct {
	interfaces.KVStore
	cancel context.CancelFunc
}

// PutIfAbsent forwards the guarded create; this wrapper only alters Scan.
func (k *taskCancelScanKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

func (k *taskCancelScanKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	k.cancel()
	return k.KVStore.Scan(ctx, prefix, fn)
}

func (k *taskFaultKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	if k.scanErr != nil {
		return k.scanErr
	}
	return k.KVStore.Scan(ctx, prefix, fn)
}

func (k *taskFaultKV) List(ctx context.Context, prefix string) ([]string, error) {
	if k.listErr != nil {
		return nil, k.listErr
	}
	return k.KVStore.List(ctx, prefix)
}

func (k *taskFaultKV) Exists(ctx context.Context, key string) (bool, error) {
	if k.existsErr != nil {
		return false, k.existsErr
	}
	return k.KVStore.Exists(ctx, key)
}

// taskAuditRecorder captures the store audit trail for assertions.
type taskAuditRecorder struct {
	mu     sync.Mutex
	events []StoreAuditEvent
}

func (r *taskAuditRecorder) RecordStoreEvent(_ context.Context, event StoreAuditEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *taskAuditRecorder) drain() []StoreAuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	events := r.events
	r.events = nil
	return events
}

// seedTask creates one pending task and fails the test if creation fails.
func seedTask(t *testing.T, store TaskStore, tenantID, taskID string) *Task {
	t.Helper()

	task := &Task{ID: taskID, TenantID: tenantID, Subject: "seed " + taskID}
	if err := store.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("seed task %s/%s: %v", tenantID, taskID, err)
	}
	return task
}

// =============================================================================
// Constructor Tests
// =============================================================================

func TestNewInMemoryTaskStore(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	if store == nil {
		t.Fatal("expected non-nil store")
	}
}

// TestNewTaskStore_NilKV proves a store built without a backing KV refuses work
// instead of panicking: a nil KV is a wiring bug, not a storage mode.
func TestNewTaskStore_NilKV(t *testing.T) {
	t.Parallel()

	store := NewTaskStore(nil, NewProcessClaimArbiter())
	err := store.CreateTask(context.Background(), &Task{ID: "t1", TenantID: "ten"})
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

// TestNewInMemoryTaskStoreWithKV_DefaultInlineLimit proves a non-positive limit
// selects the package default rather than offloading every artifact.
func TestNewInMemoryTaskStoreWithKV_DefaultInlineLimit(t *testing.T) {
	t.Parallel()

	kv := NewMemoryTaskKV()
	store := NewInMemoryTaskStoreWithKV(kv, 0)
	seedTask(t, store, "ten", "task-limit")

	ctx := context.Background()
	err := store.SubmitTaskArtifacts(ctx, "ten", "task-limit", []Artifact{
		{Kind: ArtifactKindInline, Name: "small.txt", Content: []byte("under the default limit")},
	})
	if err != nil {
		t.Fatalf("SubmitTaskArtifacts: %v", err)
	}

	list, err := store.ListTaskArtifacts(ctx, "ten", "task-limit")
	if err != nil {
		t.Fatalf("ListTaskArtifacts: %v", err)
	}
	if len(list) != 1 || list[0].Kind != ArtifactKindInline {
		t.Fatalf("expected the artifact to stay inline under the default limit, got %+v", list)
	}
}

// =============================================================================
// CRUD round-trip over an injected KV
// =============================================================================

// TestTaskStore_CRUDRoundTrip walks a task through its whole lifecycle over an
// injected in-memory KV and checks the record the store hands back at each step.
func TestTaskStore_CRUDRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewTaskStore(NewMemoryTaskKV(), NewProcessClaimArbiter())

	created := &Task{
		ID:          "task-crud",
		TenantID:    "ten",
		TeamName:    "backend",
		Subject:     "round trip",
		Description: "walk the lifecycle",
		CreatedBy:   "user-1",
		Metadata:    map[string]string{"k": "v"},
	}
	if err := store.CreateTask(ctx, created); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	got, err := store.GetTask(ctx, "ten", "task-crud")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Subject != "round trip" || got.TeamName != "backend" || got.Metadata["k"] != "v" {
		t.Fatalf("record round-tripped incorrectly: %+v", got)
	}
	if got.Version != 1 || got.Status != TaskStatusPending {
		t.Fatalf("expected version 1 + pending, got version=%d status=%s", got.Version, got.Status)
	}

	got.Description = "updated"
	if err := store.UpdateTask(ctx, got); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if err := store.ClaimTask(ctx, "ten", "task-crud", "agent-1"); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	if err := store.CompleteTask(ctx, "ten", "task-crud", &TaskResult{Output: "ok"}); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	final, err := store.GetTask(ctx, "ten", "task-crud")
	if err != nil {
		t.Fatalf("GetTask after complete: %v", err)
	}
	if final.Status != TaskStatusCompleted || final.Description != "updated" || final.AssignedTo != "agent-1" {
		t.Fatalf("unexpected final record: %+v", final)
	}
	if final.Version != 4 {
		t.Fatalf("expected version 4 after create+update+claim+complete, got %d", final.Version)
	}

	listed, err := store.ListTasks(ctx, "ten", nil)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "task-crud" {
		t.Fatalf("expected the single created task, got %d results", len(listed))
	}

	if err := store.DeleteTask(ctx, "ten", "task-crud"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if _, err := store.GetTask(ctx, "ten", "task-crud"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound after delete, got %v", err)
	}
}

// =============================================================================
// Durability: the store keeps no private state
// =============================================================================

// TestTaskStore_RestartSeesEverything rebuilds the store over the same KV and
// requires the new instance to serve every record and artifact the old one
// wrote. It is the regression test for any field that caches data outside the KV.
func TestTaskStore_RestartSeesEverything(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kv := NewMemoryTaskKV()
	large := strings.Repeat("L", 64)

	original := NewTaskStore(kv, NewProcessClaimArbiter(), WithInlineArtifactLimit(16))
	seedTask(t, original, "ten", "task-a")
	seedTask(t, original, "ten", "task-b")

	if err := original.ClaimTask(ctx, "ten", "task-b", "agent-7"); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	err := original.SubmitTaskArtifacts(ctx, "ten", "task-a", []Artifact{
		{Kind: ArtifactKindInline, Name: "small.txt", Content: []byte("inline")},
		{Kind: ArtifactKindInline, Name: "big.bin", Content: []byte(large)},
	})
	if err != nil {
		t.Fatalf("SubmitTaskArtifacts: %v", err)
	}

	// A brand new store over the same KV, sharing nothing with the first.
	restarted := NewTaskStore(kv, NewProcessClaimArbiter(), WithInlineArtifactLimit(16))

	listed, err := restarted.ListTasks(ctx, "ten", nil)
	if err != nil {
		t.Fatalf("ListTasks after restart: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("expected 2 tasks after restart, got %d", len(listed))
	}

	claimed, err := restarted.GetTask(ctx, "ten", "task-b")
	if err != nil {
		t.Fatalf("GetTask after restart: %v", err)
	}
	if claimed.Status != TaskStatusClaimed || claimed.AssignedTo != "agent-7" || claimed.Version != 2 {
		t.Fatalf("claim state lost across restart: %+v", claimed)
	}

	for _, tc := range []struct {
		name string
		want string
	}{
		{name: "small.txt", want: "inline"},
		{name: "big.bin", want: large},
	} {
		artifact, err := restarted.GetTaskArtifact(ctx, "ten", "task-a", tc.name)
		if err != nil {
			t.Fatalf("GetTaskArtifact(%s) after restart: %v", tc.name, err)
		}
		if string(artifact.Content) != tc.want {
			t.Errorf("artifact %s = %q, want %q", tc.name, artifact.Content, tc.want)
		}
	}
}

// =============================================================================
// Tenant isolation at the store layer
// =============================================================================

// TestTaskStore_TenantIsolation proves that no verb reaches another tenant's
// record even when the caller knows its exact task ID.
func TestTaskStore_TenantIsolation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kv := NewMemoryTaskKV()
	store := NewTaskStore(kv, NewProcessClaimArbiter(), WithInlineArtifactLimit(8))

	seedTask(t, store, "tenant-a", "a-task")
	victim := seedTask(t, store, "tenant-b", "b-task")

	payload := strings.Repeat("B", 32)
	err := store.SubmitTaskArtifacts(ctx, "tenant-b", "b-task", []Artifact{
		{Kind: ArtifactKindInline, Name: "secret.bin", Content: []byte(payload)},
	})
	if err != nil {
		t.Fatalf("seed artifact: %v", err)
	}

	// Every verb is invoked as tenant-a against tenant-b's task ID.
	foreign := &Task{ID: victim.ID, TenantID: "tenant-a", Version: 1}
	cases := []struct {
		name string
		call func() error
	}{
		{"GetTask", func() error { _, err := store.GetTask(ctx, "tenant-a", "b-task"); return err }},
		{"UpdateTask", func() error { return store.UpdateTask(ctx, foreign) }},
		{"ClaimTask", func() error { return store.ClaimTask(ctx, "tenant-a", "b-task", "agent-x") }},
		{"CompleteTask", func() error {
			return store.CompleteTask(ctx, "tenant-a", "b-task", &TaskResult{Output: "stolen"})
		}},
		{"FailTask", func() error { return store.FailTask(ctx, "tenant-a", "b-task", "sabotage") }},
		{"DeleteTask", func() error { return store.DeleteTask(ctx, "tenant-a", "b-task") }},
		{"GetTaskArtifact", func() error {
			_, err := store.GetTaskArtifact(ctx, "tenant-a", "b-task", "secret.bin")
			return err
		}},
		{"ListTaskArtifacts", func() error {
			_, err := store.ListTaskArtifacts(ctx, "tenant-a", "b-task")
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, ErrTaskNotFound) {
				t.Fatalf("expected ErrTaskNotFound, got %v", err)
			}
		})
	}

	// tenant-b's task and artifact must be untouched.
	survivor, err := store.GetTask(ctx, "tenant-b", "b-task")
	if err != nil {
		t.Fatalf("victim task lost: %v", err)
	}
	if survivor.Status != TaskStatusPending || survivor.AssignedTo != "" {
		t.Fatalf("victim task was mutated: %+v", survivor)
	}

	listA, err := store.ListTasks(ctx, "tenant-a", nil)
	if err != nil {
		t.Fatalf("ListTasks(tenant-a): %v", err)
	}
	if len(listA) != 1 || listA[0].ID != "a-task" {
		t.Fatalf("tenant-a listing leaked foreign tasks: %+v", listA)
	}

	if err := store.DeleteTenantTasks(ctx, "tenant-a"); err != nil {
		t.Fatalf("DeleteTenantTasks(tenant-a): %v", err)
	}
	if _, err := store.GetTask(ctx, "tenant-b", "b-task"); err != nil {
		t.Fatalf("cascade delete removed tenant-b's task: %v", err)
	}
	artifact, err := store.GetTaskArtifact(ctx, "tenant-b", "b-task", "secret.bin")
	if err != nil {
		t.Fatalf("cascade delete removed tenant-b's artifact payload: %v", err)
	}
	if string(artifact.Content) != payload {
		t.Fatalf("tenant-b artifact content changed: %q", artifact.Content)
	}
}

// TestTaskStore_TenantIsolation_PrefixNeighbours covers the case a naive prefix
// scan gets wrong: one tenant ID is a lexical prefix of another.
func TestTaskStore_TenantIsolation_PrefixNeighbours(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewTaskStore(NewMemoryTaskKV(), NewProcessClaimArbiter(), WithInlineArtifactLimit(4))

	seedTask(t, store, "t1", "shared-id")
	seedTask(t, store, "t10", "shared-id")
	seedTask(t, store, "t10", "extra")

	err := store.SubmitTaskArtifacts(ctx, "t10", "shared-id", []Artifact{
		{Kind: ArtifactKindInline, Name: "keep.bin", Content: []byte("t10 payload")},
	})
	if err != nil {
		t.Fatalf("seed artifact: %v", err)
	}

	listed, err := store.ListTasks(ctx, "t1", nil)
	if err != nil {
		t.Fatalf("ListTasks(t1): %v", err)
	}
	if len(listed) != 1 || listed[0].TenantID != "t1" {
		t.Fatalf("t1 listing captured t10 records: %+v", listed)
	}

	if err := store.DeleteTenantTasks(ctx, "t1"); err != nil {
		t.Fatalf("DeleteTenantTasks(t1): %v", err)
	}

	remaining, err := store.ListTasks(ctx, "t10", nil)
	if err != nil {
		t.Fatalf("ListTasks(t10): %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("expected t10 to keep both tasks, got %d", len(remaining))
	}
	artifact, err := store.GetTaskArtifact(ctx, "t10", "shared-id", "keep.bin")
	if err != nil {
		t.Fatalf("t10 artifact removed by t1 cascade: %v", err)
	}
	if string(artifact.Content) != "t10 payload" {
		t.Fatalf("t10 artifact content changed: %q", artifact.Content)
	}
}

// TestTaskStore_ForeignRecordUnderTenantKeyIsHidden covers the defence-in-depth
// check: a record filed under one tenant's key that claims another owner is
// reported as absent rather than returned.
func TestTaskStore_ForeignRecordUnderTenantKeyIsHidden(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kv := NewMemoryTaskKV()
	store := NewTaskStore(kv, NewProcessClaimArbiter())

	planted, err := json.Marshal(&Task{ID: "planted", TenantID: "tenant-b", Subject: "not yours"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := kv.Put(ctx, "orch/task/tenant-a/planted", planted); err != nil {
		t.Fatalf("plant record: %v", err)
	}

	if _, err := store.GetTask(ctx, "tenant-a", "planted"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
	listed, err := store.ListTasks(ctx, "tenant-a", nil)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("expected the foreign record to be skipped, got %+v", listed)
	}
}

// TestTaskStore_ListTasksSkipsCorruptRecord proves one unreadable record does
// not hide the rest of the tenant's queue.
func TestTaskStore_ListTasksSkipsCorruptRecord(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kv := NewMemoryTaskKV()
	store := NewTaskStore(kv, NewProcessClaimArbiter())

	seedTask(t, store, "ten", "good")
	if err := kv.Put(ctx, "orch/task/ten/corrupt", []byte("{not json")); err != nil {
		t.Fatalf("plant corrupt record: %v", err)
	}

	listed, err := store.ListTasks(ctx, "ten", nil)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "good" {
		t.Fatalf("expected only the readable task, got %+v", listed)
	}
}

// TestTaskStore_ListTaskRecordsReportsCorruptRecord proves an unreadable
// record is REPORTED rather than silently dropped, while the readable records
// beside it — before and after it in key order — are still returned
// (go-quicbaas#848).
func TestTaskStore_ListTaskRecordsReportsCorruptRecord(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kv := NewMemoryTaskKV()
	store := NewTaskStore(kv, NewProcessClaimArbiter())

	seedTask(t, store, "ten", "alpha")
	seedTask(t, store, "ten", "zulu")
	if err := kv.Put(ctx, "orch/task/ten/corrupt", []byte("{not json")); err != nil {
		t.Fatalf("plant corrupt record: %v", err)
	}

	listing, err := store.ListTaskRecords(ctx, "ten", nil)
	if err != nil {
		t.Fatalf("ListTaskRecords: %v", err)
	}
	if len(listing.Tasks) != 2 || listing.Tasks[0].ID != "alpha" || listing.Tasks[1].ID != "zulu" {
		t.Fatalf("expected both readable tasks, got %+v", listing.Tasks)
	}
	if len(listing.Unreadable) != 1 || listing.Unreadable[0] != "corrupt" {
		t.Fatalf("expected the corrupt record to be named, got %v", listing.Unreadable)
	}
	if listing.Complete() {
		t.Fatal("a listing with an unreadable record reported itself complete")
	}

	incomplete := listing.Err("ten")
	if !errors.Is(incomplete, ErrTaskListingIncomplete) {
		t.Fatalf("expected ErrTaskListingIncomplete, got %v", incomplete)
	}
	var typed *TaskListingIncompleteError
	if !errors.As(incomplete, &typed) || typed.TenantID != "ten" || len(typed.TaskIDs) != 1 || typed.TaskIDs[0] != "corrupt" {
		t.Fatalf("expected a typed error naming the record, got %#v", incomplete)
	}
	if !strings.Contains(incomplete.Error(), "corrupt") {
		t.Fatalf("the error does not name the record: %v", incomplete)
	}
}

// TestTaskStore_ListTaskRecordsReportsCorruptRecordWhateverTheFilter proves a
// filtered listing still reports an unreadable record: its fields cannot be
// read, so nothing can prove it falls outside the filter.
func TestTaskStore_ListTaskRecordsReportsCorruptRecordWhateverTheFilter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kv := NewMemoryTaskKV()
	store := NewTaskStore(kv, NewProcessClaimArbiter())

	seedTask(t, store, "ten", "good")
	if err := kv.Put(ctx, "orch/task/ten/corrupt", []byte("{not json")); err != nil {
		t.Fatalf("plant corrupt record: %v", err)
	}

	listing, err := store.ListTaskRecords(ctx, "ten", &TaskFilter{ProjectID: "no-such-project"})
	if err != nil {
		t.Fatalf("ListTaskRecords: %v", err)
	}
	if len(listing.Tasks) != 0 {
		t.Fatalf("expected no matching task, got %+v", listing.Tasks)
	}
	if len(listing.Unreadable) != 1 || listing.Unreadable[0] != "corrupt" {
		t.Fatalf("expected the corrupt record to be named, got %v", listing.Unreadable)
	}
}

// TestTaskStore_ListTaskRecordsCompleteListing proves a listing with nothing
// unreadable says so, and carries no error.
func TestTaskStore_ListTaskRecordsCompleteListing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewTaskStore(NewMemoryTaskKV(), NewProcessClaimArbiter())
	seedTask(t, store, "ten", "good")

	listing, err := store.ListTaskRecords(ctx, "ten", nil)
	if err != nil {
		t.Fatalf("ListTaskRecords: %v", err)
	}
	if !listing.Complete() || listing.Err("ten") != nil {
		t.Fatalf("expected a complete listing, got unreadable=%v", listing.Unreadable)
	}
	if len(listing.Tasks) != 1 {
		t.Fatalf("expected the one task, got %+v", listing.Tasks)
	}
}

// =============================================================================
// Key-segment rejection
// =============================================================================

// TestTaskStore_KeySegmentRejection proves an identifier that would break the
// key layout is refused by every verb, in both error vocabularies.
func TestTaskStore_KeySegmentRejection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewInMemoryTaskStore()

	cases := []struct {
		name     string
		tenantID string
		taskID   string
	}{
		{"empty tenant", "", "task-1"},
		{"separator in tenant", "ten/ant", "task-1"},
		{"empty task", "ten", ""},
		{"separator in task", "ten", "task/1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			getErr := func() error {
				_, err := store.GetTask(ctx, tc.tenantID, tc.taskID)
				return err
			}()

			// Verbs whose rejection always comes from the key layout, so both
			// error vocabularies must hold.
			keyLayout := map[string]error{
				"GetTask":      getErr,
				"DeleteTask":   store.DeleteTask(ctx, tc.tenantID, tc.taskID),
				"ClaimTask":    store.ClaimTask(ctx, tc.tenantID, tc.taskID, "agent-1"),
				"CompleteTask": store.CompleteTask(ctx, tc.tenantID, tc.taskID, &TaskResult{}),
				"FailTask":     store.FailTask(ctx, tc.tenantID, tc.taskID, "reason"),
			}
			for verb, err := range keyLayout {
				if !errors.Is(err, ErrTaskStoreInvalidInput) {
					t.Errorf("%s: expected ErrTaskStoreInvalidInput, got %v", verb, err)
				}
				if !errors.Is(err, ErrInvalidKeySegment) {
					t.Errorf("%s: expected ErrInvalidKeySegment, got %v", verb, err)
				}
			}

			// Verbs that reject an empty identifier with their own guard before
			// the key is built. Either way the caller sees invalid input.
			storeGuard := map[string]error{
				"CreateTask": store.CreateTask(ctx, &Task{ID: tc.taskID, TenantID: tc.tenantID}),
				"UpdateTask": store.UpdateTask(ctx, &Task{ID: tc.taskID, TenantID: tc.tenantID}),
			}
			for verb, err := range storeGuard {
				if !errors.Is(err, ErrTaskStoreInvalidInput) {
					t.Errorf("%s: expected ErrTaskStoreInvalidInput, got %v", verb, err)
				}
			}
		})
	}

	if _, err := store.ListTasks(ctx, "ten/ant", nil); !errors.Is(err, ErrInvalidKeySegment) {
		t.Errorf("ListTasks: expected ErrInvalidKeySegment, got %v", err)
	}
	if err := store.DeleteTenantTasks(ctx, "ten/ant"); !errors.Is(err, ErrInvalidKeySegment) {
		t.Errorf("DeleteTenantTasks: expected ErrInvalidKeySegment, got %v", err)
	}
}

// TestTaskStore_ArtifactNameIsEncodedNotRejected documents the rule chosen for
// the artifact-name segment: names are file-like labels, so a name containing
// the key separator is encoded rather than refused, and an empty name is
// rejected by artifact validation before it can reach the key layout.
func TestTaskStore_ArtifactNameIsEncodedNotRejected(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kv := NewMemoryTaskKV()
	store := NewTaskStore(kv, NewProcessClaimArbiter(), WithInlineArtifactLimit(4))
	seedTask(t, store, "ten", "task-names")

	payload := strings.Repeat("p", 32)
	err := store.SubmitTaskArtifacts(ctx, "ten", "task-names", []Artifact{
		{Kind: ArtifactKindInline, Name: "src/main.go", Content: []byte(payload)},
	})
	if err != nil {
		t.Fatalf("SubmitTaskArtifacts: %v", err)
	}

	artifact, err := store.GetTaskArtifact(ctx, "ten", "task-names", "src/main.go")
	if err != nil {
		t.Fatalf("GetTaskArtifact: %v", err)
	}
	if string(artifact.Content) != payload {
		t.Fatalf("content = %q, want %q", artifact.Content, payload)
	}

	// The encoded name must stay one segment below the task's artifact prefix.
	keys, err := kv.List(ctx, "orch/taskartifact/ten/task-names/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 payload key, got %v", keys)
	}
	if suffix := strings.TrimPrefix(keys[0], "orch/taskartifact/ten/task-names/"); strings.Contains(suffix, "/") {
		t.Fatalf("artifact name segment must not contain the separator: %q", suffix)
	}

	err = store.SubmitTaskArtifacts(ctx, "ten", "task-names", []Artifact{
		{Kind: ArtifactKindInline, Name: "", Content: []byte("x")},
	})
	var refErr *ArtifactRefInvalidError
	if !errors.As(err, &refErr) {
		t.Fatalf("expected *ArtifactRefInvalidError for an empty name, got %T: %v", err, err)
	}
}

// =============================================================================
// Concurrency
// =============================================================================

// TestTaskStore_ConcurrentClaimHasOneWinner proves the keyed mutex serializes
// the read-modify-write: exactly one of N racing agents claims the task.
func TestTaskStore_ConcurrentClaimHasOneWinner(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewInMemoryTaskStore()
	seedTask(t, store, "ten", "task-race")

	const claimers = 16
	var (
		wg      sync.WaitGroup
		start   = make(chan struct{})
		results = make(chan error, claimers)
	)

	for i := 0; i < claimers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			results <- store.ClaimTask(ctx, "ten", "task-race", "agent-"+string(rune('a'+n)))
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)

	var winners, alreadyClaimed int
	for err := range results {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, ErrTaskAlreadyClaimed):
			alreadyClaimed++
		default:
			t.Errorf("unexpected claim error: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly 1 winner, got %d", winners)
	}
	if alreadyClaimed != claimers-1 {
		t.Fatalf("expected %d already-claimed losers, got %d", claimers-1, alreadyClaimed)
	}

	claimed, err := store.GetTask(ctx, "ten", "task-race")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if claimed.Version != 2 {
		t.Fatalf("expected exactly one version bump, got %d", claimed.Version)
	}
}

// TestTaskStore_ConcurrentUpdateDetectsStaleVersion proves the version counter
// is what makes a lost update visible: two writers holding the same snapshot
// cannot both win.
func TestTaskStore_ConcurrentUpdateDetectsStaleVersion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewInMemoryTaskStore()
	seedTask(t, store, "ten", "task-cas")

	first, err := store.GetTask(ctx, "ten", "task-cas")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	second, err := store.GetTask(ctx, "ten", "task-cas")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	first.Subject = "written by first"
	second.Subject = "written by second"

	var (
		wg      sync.WaitGroup
		start   = make(chan struct{})
		results = make(chan error, 2)
	)
	for _, snapshot := range []*Task{first, second} {
		wg.Add(1)
		go func(task *Task) {
			defer wg.Done()
			<-start
			results <- store.UpdateTask(ctx, task)
		}(snapshot)
	}
	close(start)
	wg.Wait()
	close(results)

	var applied, conflicted int
	for err := range results {
		switch {
		case err == nil:
			applied++
		case errors.Is(err, ErrTaskVersionConflict):
			conflicted++
		default:
			t.Errorf("unexpected update error: %v", err)
		}
	}
	if applied != 1 || conflicted != 1 {
		t.Fatalf("expected 1 applied + 1 conflict, got %d applied and %d conflicts", applied, conflicted)
	}

	final, err := store.GetTask(ctx, "ten", "task-cas")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if final.Version != 2 {
		t.Fatalf("expected exactly one version bump, got %d", final.Version)
	}
}

// =============================================================================
// Artifact offload
// =============================================================================

// TestTaskStore_ArtifactOffload proves an oversized artifact is written to its
// own key, referenced from the record, resolved on read, withheld from the
// listing, and reclaimed when the task is deleted.
func TestTaskStore_ArtifactOffload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kv := NewMemoryTaskKV()
	store := NewTaskStore(kv, NewProcessClaimArbiter(), WithInlineArtifactLimit(8))
	seedTask(t, store, "ten", "task-offload")

	payload := strings.Repeat("x", 64)
	err := store.SubmitTaskArtifacts(ctx, "ten", "task-offload", []Artifact{
		{Kind: ArtifactKindInline, Name: "small", Content: []byte("tiny")},
		{Kind: ArtifactKindInline, Name: "large", Content: []byte(payload)},
	})
	if err != nil {
		t.Fatalf("SubmitTaskArtifacts: %v", err)
	}

	listed, err := store.ListTaskArtifacts(ctx, "ten", "task-offload")
	if err != nil {
		t.Fatalf("ListTaskArtifacts: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(listed))
	}
	byName := make(map[string]Artifact, len(listed))
	for _, artifact := range listed {
		byName[artifact.Name] = artifact
	}
	if byName["small"].Kind != ArtifactKindInline || string(byName["small"].Content) != "tiny" {
		t.Errorf("small artifact should stay inline: %+v", byName["small"])
	}
	large := byName["large"]
	if large.Kind != ArtifactKindKVKey {
		t.Errorf("large artifact kind = %q, want kv_key", large.Kind)
	}
	if large.Content != nil {
		t.Error("ListTaskArtifacts must not resolve offloaded content")
	}
	if !strings.HasPrefix(large.Ref.Path, "orch/taskartifact/ten/task-offload/") {
		t.Errorf("offloaded ref %q is outside the task's artifact subtree", large.Ref.Path)
	}

	stored, err := kv.Get(ctx, large.Ref.Path)
	if err != nil {
		t.Fatalf("payload key missing: %v", err)
	}
	if string(stored) != payload {
		t.Errorf("payload = %q, want %q", stored, payload)
	}

	resolved, err := store.GetTaskArtifact(ctx, "ten", "task-offload", "large")
	if err != nil {
		t.Fatalf("GetTaskArtifact: %v", err)
	}
	if string(resolved.Content) != payload || resolved.Size != int64(len(payload)) {
		t.Errorf("resolved artifact = %d bytes (size field %d), want %d", len(resolved.Content), resolved.Size, len(payload))
	}

	// Resubmitting the same name overwrites rather than appending.
	err = store.SubmitTaskArtifacts(ctx, "ten", "task-offload", []Artifact{
		{Kind: ArtifactKindInline, Name: "large", Content: []byte(strings.Repeat("y", 64))},
	})
	if err != nil {
		t.Fatalf("resubmit: %v", err)
	}
	listed, err = store.ListTaskArtifacts(ctx, "ten", "task-offload")
	if err != nil {
		t.Fatalf("ListTaskArtifacts after resubmit: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("expected the resubmission to overwrite, got %d artifacts", len(listed))
	}

	if err := store.DeleteTask(ctx, "ten", "task-offload"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	keys, err := kv.List(ctx, "orch/taskartifact/ten/task-offload/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected offloaded payloads to be reclaimed, got %v", keys)
	}
}

// TestTaskStore_ForeignArtifactRefIsNotDereferenced proves a kv_key ref that
// points outside the task's own artifact subtree is returned unresolved instead
// of turning GetTaskArtifact into an arbitrary read of the shared keyspace.
func TestTaskStore_ForeignArtifactRefIsNotDereferenced(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kv := NewMemoryTaskKV()
	store := NewTaskStore(kv, NewProcessClaimArbiter())
	seedTask(t, store, "tenant-a", "victim")
	seedTask(t, store, "tenant-b", "attacker")

	err := store.SubmitTaskArtifacts(ctx, "tenant-b", "attacker", []Artifact{
		{Kind: ArtifactKindKVKey, Name: "leak", Ref: ArtifactRef{Path: "orch/task/tenant-a/victim"}},
	})
	if err != nil {
		t.Fatalf("SubmitTaskArtifacts: %v", err)
	}

	artifact, err := store.GetTaskArtifact(ctx, "tenant-b", "attacker", "leak")
	if err != nil {
		t.Fatalf("GetTaskArtifact: %v", err)
	}
	if artifact.Content != nil {
		t.Fatalf("foreign ref was dereferenced: %q", artifact.Content)
	}
}

// TestTaskStore_MissingArtifactPayload proves a record that outlived its
// payload reports the artifact as absent rather than as a backend failure.
func TestTaskStore_MissingArtifactPayload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kv := NewMemoryTaskKV()
	store := NewTaskStore(kv, NewProcessClaimArbiter(), WithInlineArtifactLimit(4))
	seedTask(t, store, "ten", "task-orphan")

	err := store.SubmitTaskArtifacts(ctx, "ten", "task-orphan", []Artifact{
		{Kind: ArtifactKindInline, Name: "gone", Content: []byte(strings.Repeat("z", 32))},
	})
	if err != nil {
		t.Fatalf("SubmitTaskArtifacts: %v", err)
	}

	keys, err := kv.List(ctx, "orch/taskartifact/ten/task-orphan/")
	if err != nil || len(keys) != 1 {
		t.Fatalf("expected 1 payload key, got %v (err %v)", keys, err)
	}
	if err := kv.Delete(ctx, keys[0]); err != nil {
		t.Fatalf("Delete payload: %v", err)
	}

	_, err = store.GetTaskArtifact(ctx, "ten", "task-orphan", "gone")
	var notFound *ArtifactNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *ArtifactNotFoundError, got %T: %v", err, err)
	}
}

// =============================================================================
// Audit
// =============================================================================

// TestTaskStore_AuditTrail proves every mutating verb emits exactly one
// correctly shaped event and every read emits none.
func TestTaskStore_AuditTrail(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	recorder := &taskAuditRecorder{}
	store := NewTaskStore(NewMemoryTaskKV(), NewProcessClaimArbiter(), WithStoreAuditor(recorder))

	mutations := []struct {
		name   string
		action string
		taskID string
		call   func() error
	}{
		{"CreateTask", AuditActionCreate, "task-audit", func() error {
			return store.CreateTask(ctx, &Task{ID: "task-audit", TenantID: "ten"})
		}},
		{"UpdateTask", AuditActionUpdate, "task-audit", func() error {
			current, err := store.GetTask(ctx, "ten", "task-audit")
			if err != nil {
				return err
			}
			recorder.drain() // the read above must not have recorded anything
			current.Subject = "audited"
			return store.UpdateTask(ctx, current)
		}},
		{"ClaimTask", AuditActionUpdate, "task-audit", func() error {
			return store.ClaimTask(ctx, "ten", "task-audit", "agent-1")
		}},
		{"SubmitTaskArtifacts", AuditActionUpdate, "task-audit", func() error {
			return store.SubmitTaskArtifacts(ctx, "ten", "task-audit", []Artifact{
				{Kind: ArtifactKindInline, Name: "a", Content: []byte("b")},
			})
		}},
		{"CompleteTask", AuditActionUpdate, "task-audit", func() error {
			return store.CompleteTask(ctx, "ten", "task-audit", &TaskResult{Output: "ok"})
		}},
		{"DeleteTask", AuditActionDelete, "task-audit", func() error {
			return store.DeleteTask(ctx, "ten", "task-audit")
		}},
	}

	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			recorder.drain()
			if err := tc.call(); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			events := recorder.drain()
			if len(events) != 1 {
				t.Fatalf("expected exactly 1 audit event, got %d", len(events))
			}
			want := StoreAuditEvent{
				TenantID:   "ten",
				Resource:   AuditResourceTask,
				ResourceID: tc.taskID,
				Action:     tc.action,
				Success:    true,
			}
			if events[0] != want {
				t.Fatalf("event = %+v, want %+v", events[0], want)
			}
		})
	}

	t.Run("FailTask", func(t *testing.T) {
		seedTask(t, store, "ten", "task-audit-fail")
		if err := store.ClaimTask(ctx, "ten", "task-audit-fail", "agent-1"); err != nil {
			t.Fatalf("ClaimTask: %v", err)
		}
		recorder.drain()
		if err := store.FailTask(ctx, "ten", "task-audit-fail", "boom"); err != nil {
			t.Fatalf("FailTask: %v", err)
		}
		events := recorder.drain()
		if len(events) != 1 || events[0].Action != AuditActionUpdate || !events[0].Success {
			t.Fatalf("unexpected FailTask audit trail: %+v", events)
		}
	})

	t.Run("reads emit nothing", func(t *testing.T) {
		seedTask(t, store, "ten", "task-audit-read")
		recorder.drain()

		if _, err := store.GetTask(ctx, "ten", "task-audit-read"); err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		if _, err := store.ListTasks(ctx, "ten", nil); err != nil {
			t.Fatalf("ListTasks: %v", err)
		}
		if _, err := store.ListTaskArtifacts(ctx, "ten", "task-audit-read"); err != nil {
			t.Fatalf("ListTaskArtifacts: %v", err)
		}
		if _, err := store.GetTaskArtifact(ctx, "ten", "task-audit-read", "none"); err == nil {
			t.Fatal("expected ArtifactNotFoundError")
		}
		if events := recorder.drain(); len(events) != 0 {
			t.Fatalf("reads recorded %d audit events", len(events))
		}
	})

	t.Run("failures are recorded", func(t *testing.T) {
		recorder.drain()
		if err := store.ClaimTask(ctx, "ten", "no-such-task", "agent-1"); err == nil {
			t.Fatal("expected ErrTaskNotFound")
		}
		events := recorder.drain()
		if len(events) != 1 {
			t.Fatalf("expected 1 audit event, got %d", len(events))
		}
		if events[0].Success || events[0].ResourceID != "no-such-task" || events[0].Action != AuditActionUpdate {
			t.Fatalf("unexpected failure event: %+v", events[0])
		}
	})

	t.Run("cascade delete records one event per task", func(t *testing.T) {
		cascade := NewTaskStore(NewMemoryTaskKV(), NewProcessClaimArbiter(), WithStoreAuditor(recorder))
		seedTask(t, cascade, "bulk", "one")
		seedTask(t, cascade, "bulk", "two")
		recorder.drain()

		if err := cascade.DeleteTenantTasks(ctx, "bulk"); err != nil {
			t.Fatalf("DeleteTenantTasks: %v", err)
		}
		events := recorder.drain()
		if len(events) != 2 {
			t.Fatalf("expected 2 delete events, got %d", len(events))
		}
		for _, event := range events {
			if event.Action != AuditActionDelete || event.TenantID != "bulk" || !event.Success {
				t.Fatalf("unexpected cascade event: %+v", event)
			}
			if event.ResourceID != "one" && event.ResourceID != "two" {
				t.Fatalf("unexpected cascade resource ID: %q", event.ResourceID)
			}
		}
	})
}

// =============================================================================
// Backend failure paths
// =============================================================================

// TestTaskStore_BackendFailures proves every verb reports a KV outage as a
// backend failure instead of masking it as a missing or corrupt record.
func TestTaskStore_BackendFailures(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	outage := errors.New("kv unavailable")

	// setup seeds a healthy store, then arms one fault on the shared KV.
	newStore := func(t *testing.T, arm func(*taskFaultKV)) TaskStore {
		t.Helper()
		kv := newTaskFaultKV()
		store := NewTaskStore(kv, NewProcessClaimArbiter(), WithInlineArtifactLimit(4))
		seedTask(t, store, "ten", "task-1")
		if err := store.ClaimTask(ctx, "ten", "task-1", "agent-1"); err != nil {
			t.Fatalf("seed claim: %v", err)
		}
		if err := store.SubmitTaskArtifacts(ctx, "ten", "task-1", []Artifact{
			{Kind: ArtifactKindInline, Name: "big", Content: []byte(strings.Repeat("q", 32))},
		}); err != nil {
			t.Fatalf("seed artifact: %v", err)
		}
		arm(kv)
		return store
	}

	cases := []struct {
		name string
		arm  func(*taskFaultKV)
		call func(TaskStore) error
	}{
		// There is no "CreateTask probe" case any more, and its absence is the
		// point: the guarded create issues ONE operation, so there is no probe
		// left to fail independently of the write. The case below covers the
		// whole create path.
		{"CreateTask guarded write", func(k *taskFaultKV) { k.putErr = outage }, func(s TaskStore) error {
			return s.CreateTask(ctx, &Task{ID: "task-2", TenantID: "ten"})
		}},
		{"GetTask", func(k *taskFaultKV) { k.getErr = outage }, func(s TaskStore) error {
			_, err := s.GetTask(ctx, "ten", "task-1")
			return err
		}},
		{"UpdateTask", func(k *taskFaultKV) { k.putErr = outage }, func(s TaskStore) error {
			current, err := s.GetTask(ctx, "ten", "task-1")
			if err != nil {
				return err
			}
			return s.UpdateTask(ctx, current)
		}},
		{"ListTasks", func(k *taskFaultKV) { k.scanErr = outage }, func(s TaskStore) error {
			_, err := s.ListTasks(ctx, "ten", nil)
			return err
		}},
		{"ClaimTask", func(k *taskFaultKV) { k.getErr = outage }, func(s TaskStore) error {
			return s.ClaimTask(ctx, "ten", "task-1", "agent-2")
		}},
		{"CompleteTask", func(k *taskFaultKV) { k.putErr = outage }, func(s TaskStore) error {
			return s.CompleteTask(ctx, "ten", "task-1", &TaskResult{Output: "ok"})
		}},
		{"FailTask", func(k *taskFaultKV) { k.putErr = outage }, func(s TaskStore) error {
			return s.FailTask(ctx, "ten", "task-1", "boom")
		}},
		{"SubmitTaskArtifacts", func(k *taskFaultKV) { k.putErr = outage }, func(s TaskStore) error {
			return s.SubmitTaskArtifacts(ctx, "ten", "task-1", []Artifact{
				{Kind: ArtifactKindInline, Name: "big2", Content: []byte(strings.Repeat("r", 32))},
			})
		}},
		{"GetTaskArtifact", func(k *taskFaultKV) { k.getErr = outage }, func(s TaskStore) error {
			_, err := s.GetTaskArtifact(ctx, "ten", "task-1", "big")
			return err
		}},
		{"ListTaskArtifacts", func(k *taskFaultKV) { k.getErr = outage }, func(s TaskStore) error {
			_, err := s.ListTaskArtifacts(ctx, "ten", "task-1")
			return err
		}},
		{"DeleteTask record", func(k *taskFaultKV) { k.deleteErr = outage }, func(s TaskStore) error {
			return s.DeleteTask(ctx, "ten", "task-1")
		}},
		{"DeleteTask payloads", func(k *taskFaultKV) { k.listErr = outage }, func(s TaskStore) error {
			return s.DeleteTask(ctx, "ten", "task-1")
		}},
		{"DeleteTenantTasks scan", func(k *taskFaultKV) { k.scanErr = outage }, func(s TaskStore) error {
			return s.DeleteTenantTasks(ctx, "ten")
		}},
		{"DeleteTenantTasks payloads", func(k *taskFaultKV) { k.listErr = outage }, func(s TaskStore) error {
			return s.DeleteTenantTasks(ctx, "ten")
		}},
		{"DeleteTenantTasks record", func(k *taskFaultKV) { k.deleteErr = outage }, func(s TaskStore) error {
			return s.DeleteTenantTasks(ctx, "ten")
		}},
		{"DeleteTask payload delete", func(k *taskFaultKV) {
			k.deleteErr = outage
			k.deletePrefix = taskArtifactKeyspace
		}, func(s TaskStore) error {
			return s.DeleteTask(ctx, "ten", "task-1")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newStore(t, tc.arm)
			err := tc.call(store)
			if !errors.Is(err, ErrTaskStoreBackend) {
				t.Fatalf("expected ErrTaskStoreBackend, got %v", err)
			}
		})
	}
}

// TestTaskStore_CancelledContext proves every verb honours a cancelled context
// before touching the backing store.
func TestTaskStore_CancelledContext(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	seedTask(t, store, "ten", "task-ctx")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := map[string]error{
		"CreateTask":   store.CreateTask(ctx, &Task{ID: "task-new", TenantID: "ten"}),
		"UpdateTask":   store.UpdateTask(ctx, &Task{ID: "task-ctx", TenantID: "ten", Version: 1}),
		"DeleteTask":   store.DeleteTask(ctx, "ten", "task-ctx"),
		"ClaimTask":    store.ClaimTask(ctx, "ten", "task-ctx", "agent-1"),
		"CompleteTask": store.CompleteTask(ctx, "ten", "task-ctx", &TaskResult{}),
		"FailTask":     store.FailTask(ctx, "ten", "task-ctx", "reason"),
		"SubmitArts":   store.SubmitTaskArtifacts(ctx, "ten", "task-ctx", nil),
		"DeleteTenant": store.DeleteTenantTasks(ctx, "ten"),
	}
	for name, err := range calls {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s: expected context.Canceled, got %v", name, err)
		}
	}

	if _, err := store.GetTask(ctx, "ten", "task-ctx"); !errors.Is(err, context.Canceled) {
		t.Errorf("GetTask: expected context.Canceled, got %v", err)
	}
	if _, err := store.ListTasks(ctx, "ten", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("ListTasks: expected context.Canceled, got %v", err)
	}
	if _, err := store.GetTaskArtifact(ctx, "ten", "task-ctx", "a"); !errors.Is(err, context.Canceled) {
		t.Errorf("GetTaskArtifact: expected context.Canceled, got %v", err)
	}
	if _, err := store.ListTaskArtifacts(ctx, "ten", "task-ctx"); !errors.Is(err, context.Canceled) {
		t.Errorf("ListTaskArtifacts: expected context.Canceled, got %v", err)
	}
}

// TestTaskStore_ScanCancellation proves a context cancelled while the listing is
// in flight is reported as a cancellation rather than as a backend failure.
func TestTaskStore_ScanCancellation(t *testing.T) {
	t.Parallel()

	seedCtx := context.Background()
	base := NewMemoryTaskKV()
	seedStore := NewTaskStore(base, NewProcessClaimArbiter())
	seedTask(t, seedStore, "ten", "task-1")

	for _, tc := range []struct {
		name string
		call func(context.Context, TaskStore) error
	}{
		{"ListTasks", func(ctx context.Context, s TaskStore) error {
			_, err := s.ListTasks(ctx, "ten", nil)
			return err
		}},
		{"DeleteTenantTasks", func(ctx context.Context, s TaskStore) error {
			return s.DeleteTenantTasks(ctx, "ten")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(seedCtx)
			defer cancel()

			store := NewTaskStore(&taskCancelScanKV{KVStore: base, cancel: cancel}, NewProcessClaimArbiter())
			if err := tc.call(ctx, store); !errors.Is(err, context.Canceled) {
				t.Fatalf("expected context.Canceled, got %v", err)
			}
		})
	}
}

// TestTaskStore_CascadeSkipsMalformedKey proves a key written straight into the
// keyspace with no task identifier is skipped rather than abandoning the rest
// of the cascade.
func TestTaskStore_CascadeSkipsMalformedKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kv := NewMemoryTaskKV()
	store := NewTaskStore(kv, NewProcessClaimArbiter())
	seedTask(t, store, "ten", "task-1")

	if err := kv.Put(ctx, "orch/task/ten/", []byte("{}")); err != nil {
		t.Fatalf("plant malformed key: %v", err)
	}

	if err := store.DeleteTenantTasks(ctx, "ten"); err != nil {
		t.Fatalf("DeleteTenantTasks: %v", err)
	}
	if _, err := store.GetTask(ctx, "ten", "task-1"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("the well-formed task should still have been deleted, got %v", err)
	}
}

// TestTaskArtifactKeys_Rejections proves the artifact keyspace refuses the same
// malformed identifiers as the record keyspace, and refuses an empty artifact
// name outright since there is no encoding of "nothing" that yields a segment.
func TestTaskArtifactKeys_Rejections(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		call func() (string, error)
	}{
		{"prefix rejects bad tenant", func() (string, error) { return taskArtifactPrefix("a/b", "task-1") }},
		{"prefix rejects bad task", func() (string, error) { return taskArtifactPrefix("ten", "") }},
		{"tenant prefix rejects bad tenant", func() (string, error) { return tenantArtifactPrefix("") }},
		{"key rejects bad task", func() (string, error) { return taskArtifactKey("ten", "a/b", "art") }},
		{"key rejects empty name", func() (string, error) { return taskArtifactKey("ten", "task-1", "") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, err := tc.call()
			if key != "" {
				t.Errorf("expected no key, got %q", key)
			}
			if !errors.Is(err, ErrInvalidKeySegment) {
				t.Errorf("expected ErrInvalidKeySegment, got %v", err)
			}
			if !errors.Is(err, ErrTaskStoreInvalidInput) {
				t.Errorf("expected ErrTaskStoreInvalidInput, got %v", err)
			}
		})
	}

	key, err := taskArtifactKey("ten", "task-1", "src/main.go")
	if err != nil {
		t.Fatalf("taskArtifactKey: %v", err)
	}
	if want := "orch/taskartifact/ten/task-1/"; !strings.HasPrefix(key, want) {
		t.Errorf("key %q must live under %q", key, want)
	}
}

// =============================================================================
// CreateTask Tests
// =============================================================================

func TestCreateTask_Success(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:          "task-1",
		TenantID:    "tenant-a",
		TeamName:    "backend",
		Subject:     "Implement API",
		Description: "Build the REST API endpoint",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if task.Version != 1 {
		t.Errorf("expected version 1, got %d", task.Version)
	}
	if task.Status != TaskStatusPending {
		t.Errorf("expected status pending, got %s", task.Status)
	}
	if task.CreatedAt == 0 {
		t.Error("expected created_at to be set")
	}
	if task.UpdatedAt == 0 {
		t.Error("expected updated_at to be set")
	}

	// Verify retrieval
	got, err := store.GetTask(context.Background(), "tenant-a", "task-1")
	if err != nil {
		t.Fatalf("expected no error on get, got %v", err)
	}
	if got.ID != "task-1" {
		t.Errorf("expected task ID 'task-1', got %q", got.ID)
	}
	if got.TeamName != "backend" {
		t.Errorf("expected team 'backend', got %q", got.TeamName)
	}
}

func TestCreateTask_DuplicateID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-dup",
		TenantID: "tenant-a",
		Subject:  "First task",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("expected no error on first create, got %v", err)
	}

	duplicate := &Task{
		ID:       "task-dup",
		TenantID: "tenant-a",
		Subject:  "Duplicate task",
	}

	err = store.CreateTask(context.Background(), duplicate)
	if err == nil {
		t.Fatal("expected error for duplicate ID")
	}
	if !errors.Is(err, ErrTaskAlreadyExists) {
		t.Fatalf("expected ErrTaskAlreadyExists, got %v", err)
	}
}

func TestCreateTask_NilTask(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.CreateTask(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil task")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestCreateTask_EmptyID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.CreateTask(context.Background(), &Task{TenantID: "t"})
	if err == nil {
		t.Fatal("expected error for empty task ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestCreateTask_EmptyTenantID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.CreateTask(context.Background(), &Task{ID: "t1"})
	if err == nil {
		t.Fatal("expected error for empty tenant ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestCreateTask_CancelledContext(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.CreateTask(ctx, &Task{ID: "t1", TenantID: "t"})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// =============================================================================
// GetTask Tests
// =============================================================================

func TestGetTask_Success(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:          "task-get",
		TenantID:    "tenant-a",
		Subject:     "Get me",
		Description: "Task to retrieve",
		Metadata:    map[string]string{"key": "value"},
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	got, err := store.GetTask(context.Background(), "tenant-a", "task-get")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Subject != "Get me" {
		t.Errorf("expected subject 'Get me', got %q", got.Subject)
	}
	if got.Metadata["key"] != "value" {
		t.Errorf("expected metadata key=value, got %v", got.Metadata)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	_, err := store.GetTask(context.Background(), "tenant-a", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestGetTask_WrongTenant(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-tenant",
		TenantID: "tenant-a",
		Subject:  "Tenant A task",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Try to get with wrong tenant
	_, err = store.GetTask(context.Background(), "tenant-b", "task-tenant")
	if err == nil {
		t.Fatal("expected error for wrong tenant")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound for tenant isolation, got %v", err)
	}
}

func TestGetTask_EmptyTenantID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	_, err := store.GetTask(context.Background(), "", "task-1")
	if err == nil {
		t.Fatal("expected error for empty tenant ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestGetTask_EmptyTaskID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	_, err := store.GetTask(context.Background(), "tenant-a", "")
	if err == nil {
		t.Fatal("expected error for empty task ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestGetTask_CancelledContext(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.GetTask(ctx, "tenant-a", "task-1")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// =============================================================================
// UpdateTask Tests
// =============================================================================

func TestUpdateTask_Success(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-update",
		TenantID: "tenant-a",
		Subject:  "Original",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Retrieve to get the current version
	current, err := store.GetTask(context.Background(), "tenant-a", "task-update")
	if err != nil {
		t.Fatalf("setup get: %v", err)
	}

	current.Subject = "Updated"
	err = store.UpdateTask(context.Background(), current)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify
	got, err := store.GetTask(context.Background(), "tenant-a", "task-update")
	if err != nil {
		t.Fatalf("verify get: %v", err)
	}
	if got.Subject != "Updated" {
		t.Errorf("expected subject 'Updated', got %q", got.Subject)
	}
	if got.Version != 2 {
		t.Errorf("expected version 2, got %d", got.Version)
	}
}

func TestUpdateTask_VersionConflict(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-conflict",
		TenantID: "tenant-a",
		Subject:  "Original",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Get two copies
	copy1, err := store.GetTask(context.Background(), "tenant-a", "task-conflict")
	if err != nil {
		t.Fatalf("setup get 1: %v", err)
	}
	copy2, err := store.GetTask(context.Background(), "tenant-a", "task-conflict")
	if err != nil {
		t.Fatalf("setup get 2: %v", err)
	}

	// Update with first copy succeeds
	copy1.Subject = "Updated by copy1"
	err = store.UpdateTask(context.Background(), copy1)
	if err != nil {
		t.Fatalf("expected first update to succeed, got %v", err)
	}

	// Update with stale copy2 fails
	copy2.Subject = "Updated by copy2"
	err = store.UpdateTask(context.Background(), copy2)
	if err == nil {
		t.Fatal("expected version conflict error")
	}
	if !errors.Is(err, ErrTaskVersionConflict) {
		t.Fatalf("expected ErrTaskVersionConflict, got %v", err)
	}

	var conflictErr *TaskVersionConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected TaskVersionConflictError, got %T", err)
	}
	if conflictErr.TaskID != "task-conflict" {
		t.Errorf("expected task ID 'task-conflict', got %q", conflictErr.TaskID)
	}
}

func TestUpdateTask_NotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "nonexistent",
		TenantID: "tenant-a",
		Version:  1,
	}

	err := store.UpdateTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestUpdateTask_NilTask(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.UpdateTask(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil task")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestUpdateTask_EmptyID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.UpdateTask(context.Background(), &Task{TenantID: "t"})
	if err == nil {
		t.Fatal("expected error for empty task ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

// =============================================================================
// ClaimTask Tests
// =============================================================================

func TestClaimTask_Success(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-claim",
		TenantID: "tenant-a",
		Subject:  "Claim me",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	err = store.ClaimTask(context.Background(), "tenant-a", "task-claim", "agent-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify the claim
	got, err := store.GetTask(context.Background(), "tenant-a", "task-claim")
	if err != nil {
		t.Fatalf("verify get: %v", err)
	}
	if got.Status != TaskStatusClaimed {
		t.Errorf("expected status claimed, got %s", got.Status)
	}
	if got.AssignedTo != "agent-1" {
		t.Errorf("expected assigned_to 'agent-1', got %q", got.AssignedTo)
	}
	if got.ClaimedAt == 0 {
		t.Error("expected claimed_at to be set")
	}
	if got.Version != 2 {
		t.Errorf("expected version 2, got %d", got.Version)
	}
}

func TestClaimTask_AlreadyClaimed(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-double-claim",
		TenantID: "tenant-a",
		Subject:  "Double claim",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// First claim succeeds
	err = store.ClaimTask(context.Background(), "tenant-a", "task-double-claim", "agent-1")
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}

	// Second claim fails because status is no longer pending
	err = store.ClaimTask(context.Background(), "tenant-a", "task-double-claim", "agent-2")
	if err == nil {
		t.Fatal("expected error for already claimed task")
	}
	if !errors.Is(err, ErrTaskAlreadyClaimed) {
		t.Fatalf("expected ErrTaskAlreadyClaimed, got %v", err)
	}
}

func TestClaimTask_NotPending(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-not-pending",
		TenantID: "tenant-a",
		Subject:  "Not pending",
		Status:   TaskStatusCompleted,
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Force the status to completed by updating
	current, err := store.GetTask(context.Background(), "tenant-a", "task-not-pending")
	if err != nil {
		t.Fatalf("setup get: %v", err)
	}
	current.Status = TaskStatusCompleted
	err = store.UpdateTask(context.Background(), current)
	if err != nil {
		t.Fatalf("setup update: %v", err)
	}

	err = store.ClaimTask(context.Background(), "tenant-a", "task-not-pending", "agent-1")
	if err == nil {
		t.Fatal("expected error for non-pending task")
	}
	if !errors.Is(err, ErrTaskInvalidState) {
		t.Fatalf("expected ErrTaskInvalidState, got %v", err)
	}

	var stateErr *TaskInvalidStateError
	if !errors.As(err, &stateErr) {
		t.Fatalf("expected TaskInvalidStateError, got %T", err)
	}
	if stateErr.CurrentState != TaskStatusCompleted {
		t.Errorf("expected current state completed, got %s", stateErr.CurrentState)
	}
}

func TestClaimTask_NotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.ClaimTask(context.Background(), "tenant-a", "nonexistent", "agent-1")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestClaimTask_EmptyAgentID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.ClaimTask(context.Background(), "tenant-a", "task-1", "")
	if err == nil {
		t.Fatal("expected error for empty agent ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

// =============================================================================
// CompleteTask Tests
// =============================================================================

func TestCompleteTask_Success(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-complete",
		TenantID: "tenant-a",
		Subject:  "Complete me",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup create: %v", err)
	}

	// Claim first
	err = store.ClaimTask(context.Background(), "tenant-a", "task-complete", "agent-1")
	if err != nil {
		t.Fatalf("setup claim: %v", err)
	}

	result := &TaskResult{
		Output: "All done",
		Files:  []string{"main.go", "main_test.go"},
		Metadata: map[string]string{
			"lines_changed": "42",
		},
	}

	err = store.CompleteTask(context.Background(), "tenant-a", "task-complete", result)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	got, err := store.GetTask(context.Background(), "tenant-a", "task-complete")
	if err != nil {
		t.Fatalf("verify get: %v", err)
	}
	if got.Status != TaskStatusCompleted {
		t.Errorf("expected status completed, got %s", got.Status)
	}
	if got.Result == nil {
		t.Fatal("expected result to be set")
	}
	if got.Result.Output != "All done" {
		t.Errorf("expected result output 'All done', got %q", got.Result.Output)
	}
	if got.CompletedAt == 0 {
		t.Error("expected completed_at to be set")
	}
}

func TestCompleteTask_NotClaimed(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-unclaimed",
		TenantID: "tenant-a",
		Subject:  "Unclaimed task",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	err = store.CompleteTask(context.Background(), "tenant-a", "task-unclaimed", &TaskResult{Output: "done"})
	if err == nil {
		t.Fatal("expected error for completing unclaimed task")
	}
	if !errors.Is(err, ErrTaskInvalidState) {
		t.Fatalf("expected ErrTaskInvalidState, got %v", err)
	}
}

func TestCompleteTask_NotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.CompleteTask(context.Background(), "tenant-a", "nonexistent", &TaskResult{})
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestCompleteTask_EmptyTaskID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.CompleteTask(context.Background(), "tenant-a", "", &TaskResult{})
	if err == nil {
		t.Fatal("expected error for empty task ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

// =============================================================================
// FailTask Tests
// =============================================================================

func TestFailTask_Success(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-fail",
		TenantID: "tenant-a",
		Subject:  "Fail me",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup create: %v", err)
	}

	err = store.ClaimTask(context.Background(), "tenant-a", "task-fail", "agent-1")
	if err != nil {
		t.Fatalf("setup claim: %v", err)
	}

	err = store.FailTask(context.Background(), "tenant-a", "task-fail", "compilation error")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	got, err := store.GetTask(context.Background(), "tenant-a", "task-fail")
	if err != nil {
		t.Fatalf("verify get: %v", err)
	}
	if got.Status != TaskStatusFailed {
		t.Errorf("expected status failed, got %s", got.Status)
	}
	if got.Result == nil || got.Result.Output != "compilation error" {
		t.Errorf("expected failure reason in result, got %v", got.Result)
	}
}

func TestFailTask_NotClaimed(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-fail-unclaimed",
		TenantID: "tenant-a",
		Subject:  "Unclaimed",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	err = store.FailTask(context.Background(), "tenant-a", "task-fail-unclaimed", "reason")
	if err == nil {
		t.Fatal("expected error for failing unclaimed task")
	}
	if !errors.Is(err, ErrTaskInvalidState) {
		t.Fatalf("expected ErrTaskInvalidState, got %v", err)
	}
}

func TestFailTask_EmptyReason(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.FailTask(context.Background(), "tenant-a", "task-1", "")
	if err == nil {
		t.Fatal("expected error for empty reason")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestFailTask_NotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.FailTask(context.Background(), "tenant-a", "nonexistent", "reason")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

// =============================================================================
// DeleteTask Tests
// =============================================================================

func TestDeleteTask_Success(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	task := &Task{
		ID:       "task-delete",
		TenantID: "tenant-a",
		Subject:  "Delete me",
	}

	err := store.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	err = store.DeleteTask(context.Background(), "tenant-a", "task-delete")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify deletion
	_, err = store.GetTask(context.Background(), "tenant-a", "task-delete")
	if err == nil {
		t.Fatal("expected error after deletion")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestDeleteTask_NotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.DeleteTask(context.Background(), "tenant-a", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestDeleteTask_EmptyTenantID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.DeleteTask(context.Background(), "", "task-1")
	if err == nil {
		t.Fatal("expected error for empty tenant ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

// =============================================================================
// ListTasks Tests
// =============================================================================

func TestListTasks_FilterByStatus(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	tasks := []*Task{
		{ID: "t1", TenantID: "tenant-a", Subject: "Task 1", Status: TaskStatusPending},
		{ID: "t2", TenantID: "tenant-a", Subject: "Task 2", Status: TaskStatusPending},
		{ID: "t3", TenantID: "tenant-a", Subject: "Task 3"},
	}

	for _, task := range tasks {
		err := store.CreateTask(ctx, task)
		if err != nil {
			t.Fatalf("setup create %s: %v", task.ID, err)
		}
	}

	// Claim t1 to change its status
	err := store.ClaimTask(ctx, "tenant-a", "t1", "agent-1")
	if err != nil {
		t.Fatalf("setup claim: %v", err)
	}

	// Filter for pending only
	filter := &TaskFilter{Status: TaskStatusPending}
	results, err := store.ListTasks(ctx, "tenant-a", filter)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 pending tasks, got %d", len(results))
	}

	for _, r := range results {
		if r.Status != TaskStatusPending {
			t.Errorf("expected all results to be pending, got %s for task %s", r.Status, r.ID)
		}
	}
}

func TestListTasks_FilterByTeam(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	tasks := []*Task{
		{ID: "t1", TenantID: "tenant-a", TeamName: "backend", Subject: "Backend task"},
		{ID: "t2", TenantID: "tenant-a", TeamName: "frontend", Subject: "Frontend task"},
		{ID: "t3", TenantID: "tenant-a", TeamName: "backend", Subject: "Another backend"},
	}

	for _, task := range tasks {
		err := store.CreateTask(ctx, task)
		if err != nil {
			t.Fatalf("setup create %s: %v", task.ID, err)
		}
	}

	filter := &TaskFilter{TeamName: "backend"}
	results, err := store.ListTasks(ctx, "tenant-a", filter)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 backend tasks, got %d", len(results))
	}

	for _, r := range results {
		if r.TeamName != "backend" {
			t.Errorf("expected all results to be team 'backend', got %q for task %s", r.TeamName, r.ID)
		}
	}
}

func TestListTasks_FilterByAssignee(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	tasks := []*Task{
		{ID: "t1", TenantID: "tenant-a", Subject: "Task 1"},
		{ID: "t2", TenantID: "tenant-a", Subject: "Task 2"},
	}

	for _, task := range tasks {
		err := store.CreateTask(ctx, task)
		if err != nil {
			t.Fatalf("setup create %s: %v", task.ID, err)
		}
	}

	// Claim only t1
	err := store.ClaimTask(ctx, "tenant-a", "t1", "agent-1")
	if err != nil {
		t.Fatalf("setup claim: %v", err)
	}

	filter := &TaskFilter{AssignedTo: "agent-1"}
	results, err := store.ListTasks(ctx, "tenant-a", filter)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 task assigned to agent-1, got %d", len(results))
	}
	if results[0].ID != "t1" {
		t.Errorf("expected task t1, got %s", results[0].ID)
	}
}

func TestListTasks_TenantIsolation(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	tasks := []*Task{
		{ID: "ta-1", TenantID: "tenant-a", Subject: "Tenant A task 1"},
		{ID: "ta-2", TenantID: "tenant-a", Subject: "Tenant A task 2"},
		{ID: "tb-1", TenantID: "tenant-b", Subject: "Tenant B task"},
	}

	for _, task := range tasks {
		err := store.CreateTask(ctx, task)
		if err != nil {
			t.Fatalf("setup create %s: %v", task.ID, err)
		}
	}

	// List tenant-a tasks
	resultsA, err := store.ListTasks(ctx, "tenant-a", nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resultsA) != 2 {
		t.Fatalf("expected 2 tasks for tenant-a, got %d", len(resultsA))
	}

	// List tenant-b tasks
	resultsB, err := store.ListTasks(ctx, "tenant-b", nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resultsB) != 1 {
		t.Fatalf("expected 1 task for tenant-b, got %d", len(resultsB))
	}

	// Verify no cross-contamination
	for _, r := range resultsA {
		if r.TenantID != "tenant-a" {
			t.Errorf("tenant-a list contains task from tenant %q", r.TenantID)
		}
	}
	for _, r := range resultsB {
		if r.TenantID != "tenant-b" {
			t.Errorf("tenant-b list contains task from tenant %q", r.TenantID)
		}
	}
}

func TestListTasks_EmptyTenantID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	_, err := store.ListTasks(context.Background(), "", nil)
	if err == nil {
		t.Fatal("expected error for empty tenant ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestListTasks_NilFilter(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	err := store.CreateTask(ctx, &Task{ID: "t1", TenantID: "tenant-a", Subject: "Task"})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	results, err := store.ListTasks(ctx, "tenant-a", nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 task with nil filter, got %d", len(results))
	}
}

func TestListTasks_WithLimit(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		id := "t" + string(rune('0'+i))
		err := store.CreateTask(ctx, &Task{ID: id, TenantID: "tenant-a", Subject: "Task"})
		if err != nil {
			t.Fatalf("setup create: %v", err)
		}
	}

	filter := &TaskFilter{Limit: 3}
	results, err := store.ListTasks(ctx, "tenant-a", filter)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) > 3 {
		t.Fatalf("expected at most 3 tasks with limit, got %d", len(results))
	}
}

// =============================================================================
// DeleteTenantTasks Tests
// =============================================================================

func TestDeleteTenantTasks_CascadeDelete(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	// Create tasks for two tenants
	tasks := []*Task{
		{ID: "ta-1", TenantID: "tenant-a", Subject: "Tenant A task 1"},
		{ID: "ta-2", TenantID: "tenant-a", Subject: "Tenant A task 2"},
		{ID: "ta-3", TenantID: "tenant-a", Subject: "Tenant A task 3"},
		{ID: "tb-1", TenantID: "tenant-b", Subject: "Tenant B task"},
	}

	for _, task := range tasks {
		err := store.CreateTask(ctx, task)
		if err != nil {
			t.Fatalf("setup create %s: %v", task.ID, err)
		}
	}

	// Delete all tenant-a tasks
	err := store.DeleteTenantTasks(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify tenant-a tasks are gone
	resultsA, err := store.ListTasks(ctx, "tenant-a", nil)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(resultsA) != 0 {
		t.Fatalf("expected 0 tasks for tenant-a after cascade delete, got %d", len(resultsA))
	}

	// Verify tenant-b tasks remain
	resultsB, err := store.ListTasks(ctx, "tenant-b", nil)
	if err != nil {
		t.Fatalf("list tenant-b: %v", err)
	}
	if len(resultsB) != 1 {
		t.Fatalf("expected 1 task for tenant-b after tenant-a delete, got %d", len(resultsB))
	}
}

func TestDeleteTenantTasks_EmptyTenantID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.DeleteTenantTasks(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty tenant ID")
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestDeleteTenantTasks_NoTasksForTenant(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	// Should succeed even if no tasks exist
	err := store.DeleteTenantTasks(context.Background(), "nonexistent-tenant")
	if err != nil {
		t.Fatalf("expected no error for empty tenant, got %v", err)
	}
}

// =============================================================================
// Task Dependencies Tests
// =============================================================================

func TestTaskDependencies_BlockedUntilResolved(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	// Create dependency task
	depTask := &Task{
		ID:       "dep-task",
		TenantID: "tenant-a",
		Subject:  "Dependency",
	}
	err := store.CreateTask(ctx, depTask)
	if err != nil {
		t.Fatalf("setup create dep: %v", err)
	}

	// Create dependent task
	dependentTask := &Task{
		ID:           "blocked-task",
		TenantID:     "tenant-a",
		Subject:      "Blocked task",
		Dependencies: []string{"dep-task"},
	}
	err = store.CreateTask(ctx, dependentTask)
	if err != nil {
		t.Fatalf("setup create dependent: %v", err)
	}

	// Try to claim the blocked task - should fail because dep is not completed
	err = store.ClaimTask(ctx, "tenant-a", "blocked-task", "agent-1")
	if err == nil {
		t.Fatal("expected error for blocked task")
	}
	if !errors.Is(err, ErrTaskDependencyBlocked) {
		t.Fatalf("expected ErrTaskDependencyBlocked, got %v", err)
	}

	// Complete the dependency
	err = store.ClaimTask(ctx, "tenant-a", "dep-task", "agent-1")
	if err != nil {
		t.Fatalf("claim dep: %v", err)
	}
	err = store.CompleteTask(ctx, "tenant-a", "dep-task", &TaskResult{Output: "done"})
	if err != nil {
		t.Fatalf("complete dep: %v", err)
	}

	// Now claiming the dependent task should succeed
	err = store.ClaimTask(ctx, "tenant-a", "blocked-task", "agent-1")
	if err != nil {
		t.Fatalf("expected claim to succeed after dependency resolved, got %v", err)
	}

	got, err := store.GetTask(ctx, "tenant-a", "blocked-task")
	if err != nil {
		t.Fatalf("verify get: %v", err)
	}
	if got.Status != TaskStatusClaimed {
		t.Errorf("expected status claimed, got %s", got.Status)
	}
}

func TestTaskDependencies_NonexistentDependency(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	task := &Task{
		ID:           "task-bad-dep",
		TenantID:     "tenant-a",
		Subject:      "Bad dependency",
		Dependencies: []string{"nonexistent-dep"},
	}
	err := store.CreateTask(ctx, task)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	err = store.ClaimTask(ctx, "tenant-a", "task-bad-dep", "agent-1")
	if err == nil {
		t.Fatal("expected error for nonexistent dependency")
	}
	if !errors.Is(err, ErrTaskDependencyBlocked) {
		t.Fatalf("expected ErrTaskDependencyBlocked, got %v", err)
	}
}

// =============================================================================
// GenerateTaskID Tests
// =============================================================================

func TestGenerateTaskID_Unique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		id, err := GenerateTaskID()
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if id == "" {
			t.Fatal("expected non-empty ID")
		}
		if len(id) != 32 { // 16 bytes = 32 hex chars
			t.Fatalf("expected 32 hex chars, got %d: %q", len(id), id)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate ID generated: %s", id)
		}
		seen[id] = struct{}{}
	}
}

// =============================================================================
// Error Type Tests
// =============================================================================

func TestTaskNotFoundError(t *testing.T) {
	t.Parallel()

	err := &TaskNotFoundError{TenantID: "t1", TaskID: "task-1"}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected errors.Is to match ErrTaskNotFound")
	}
	msg := err.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestTaskAlreadyExistsError(t *testing.T) {
	t.Parallel()

	err := &TaskAlreadyExistsError{TaskID: "task-1"}
	if !errors.Is(err, ErrTaskAlreadyExists) {
		t.Fatalf("expected errors.Is to match ErrTaskAlreadyExists")
	}
}

func TestTaskAlreadyClaimedError(t *testing.T) {
	t.Parallel()

	err := &TaskAlreadyClaimedError{TaskID: "task-1", ClaimedBy: "a1", RequestingBy: "a2"}
	if !errors.Is(err, ErrTaskAlreadyClaimed) {
		t.Fatalf("expected errors.Is to match ErrTaskAlreadyClaimed")
	}
	msg := err.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestTaskVersionConflictError(t *testing.T) {
	t.Parallel()

	err := &TaskVersionConflictError{TaskID: "task-1", ExpectedVersion: 1, ActualVersion: 2}
	if !errors.Is(err, ErrTaskVersionConflict) {
		t.Fatalf("expected errors.Is to match ErrTaskVersionConflict")
	}
}

func TestTaskDependencyBlockedError(t *testing.T) {
	t.Parallel()

	err := &TaskDependencyBlockedError{TaskID: "task-1", UnresolvedDependency: "dep-1"}
	if !errors.Is(err, ErrTaskDependencyBlocked) {
		t.Fatalf("expected errors.Is to match ErrTaskDependencyBlocked")
	}
}

func TestTaskInvalidStateError(t *testing.T) {
	t.Parallel()

	err := &TaskInvalidStateError{TaskID: "task-1", CurrentState: TaskStatusPending, TargetState: TaskStatusCompleted}
	if !errors.Is(err, ErrTaskInvalidState) {
		t.Fatalf("expected errors.Is to match ErrTaskInvalidState")
	}
	msg := err.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestTaskStoreError(t *testing.T) {
	t.Parallel()

	err := &TaskStoreError{Sentinel: ErrTaskStoreInvalidInput, Message: "test message"}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected errors.Is to match ErrTaskStoreInvalidInput")
	}
	msg := err.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}

	// Test without message
	errNoMsg := &TaskStoreError{Sentinel: ErrTaskStoreMarshal}
	if errNoMsg.Error() != ErrTaskStoreMarshal.Error() {
		t.Errorf("expected sentinel error string, got %q", errNoMsg.Error())
	}
}

// TestTaskKeySegmentError proves the dual-contract error satisfies both the
// store's validation vocabulary and the key layout's.
func TestTaskKeySegmentError(t *testing.T) {
	t.Parallel()

	err := taskKeyError(&KeySegmentError{
		Field:   "tenant_id",
		Value:   "a/b",
		Message: "must not contain /",
	})

	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Error("expected errors.Is to match ErrTaskStoreInvalidInput")
	}
	if !errors.Is(err, ErrInvalidKeySegment) {
		t.Error("expected errors.Is to match ErrInvalidKeySegment")
	}

	var storeErr *TaskStoreError
	if !errors.As(err, &storeErr) {
		t.Error("expected errors.As to yield *TaskStoreError")
	}
	var segmentErr *KeySegmentError
	if !errors.As(err, &segmentErr) {
		t.Error("expected errors.As to yield *KeySegmentError")
	}
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}

	// Anything that is not a key-segment violation passes through untouched.
	other := errors.New("unrelated")
	if got := taskKeyError(other); !errors.Is(got, other) {
		t.Errorf("expected the original error, got %v", got)
	}
}

// =============================================================================
// InProgress State Tests
// =============================================================================

func TestCompleteTask_FromInProgress(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	task := &Task{
		ID:       "task-progress",
		TenantID: "tenant-a",
		Subject:  "In progress task",
	}

	err := store.CreateTask(ctx, task)
	if err != nil {
		t.Fatalf("setup create: %v", err)
	}

	// Claim then update to in_progress
	err = store.ClaimTask(ctx, "tenant-a", "task-progress", "agent-1")
	if err != nil {
		t.Fatalf("setup claim: %v", err)
	}

	current, err := store.GetTask(ctx, "tenant-a", "task-progress")
	if err != nil {
		t.Fatalf("setup get: %v", err)
	}
	current.Status = TaskStatusInProgress
	err = store.UpdateTask(ctx, current)
	if err != nil {
		t.Fatalf("setup update to in_progress: %v", err)
	}

	// Complete from in_progress
	err = store.CompleteTask(ctx, "tenant-a", "task-progress", &TaskResult{Output: "done"})
	if err != nil {
		t.Fatalf("expected no error completing from in_progress, got %v", err)
	}

	got, err := store.GetTask(ctx, "tenant-a", "task-progress")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.Status != TaskStatusCompleted {
		t.Errorf("expected completed, got %s", got.Status)
	}
}

func TestFailTask_FromInProgress(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	task := &Task{
		ID:       "task-fail-progress",
		TenantID: "tenant-a",
		Subject:  "Fail from in progress",
	}

	err := store.CreateTask(ctx, task)
	if err != nil {
		t.Fatalf("setup create: %v", err)
	}

	err = store.ClaimTask(ctx, "tenant-a", "task-fail-progress", "agent-1")
	if err != nil {
		t.Fatalf("setup claim: %v", err)
	}

	current, err := store.GetTask(ctx, "tenant-a", "task-fail-progress")
	if err != nil {
		t.Fatalf("setup get: %v", err)
	}
	current.Status = TaskStatusInProgress
	err = store.UpdateTask(ctx, current)
	if err != nil {
		t.Fatalf("setup update: %v", err)
	}

	err = store.FailTask(ctx, "tenant-a", "task-fail-progress", "timeout")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	got, err := store.GetTask(ctx, "tenant-a", "task-fail-progress")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.Status != TaskStatusFailed {
		t.Errorf("expected failed, got %s", got.Status)
	}
}

// =============================================================================
// ListTasks FilterByCreatedBy Tests
// =============================================================================

func TestListTasks_FilterByCreatedBy(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	tasks := []*Task{
		{ID: "t1", TenantID: "tenant-a", Subject: "Task 1", CreatedBy: "user-1"},
		{ID: "t2", TenantID: "tenant-a", Subject: "Task 2", CreatedBy: "user-2"},
		{ID: "t3", TenantID: "tenant-a", Subject: "Task 3", CreatedBy: "user-1"},
	}

	for _, task := range tasks {
		err := store.CreateTask(ctx, task)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	filter := &TaskFilter{CreatedBy: "user-1"}
	results, err := store.ListTasks(ctx, "tenant-a", filter)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 tasks created by user-1, got %d", len(results))
	}
	for _, r := range results {
		if r.CreatedBy != "user-1" {
			t.Errorf("expected created_by user-1, got %q", r.CreatedBy)
		}
	}
}

// =============================================================================
// Blocked lifecycle, filters and the guarded source index
// =============================================================================

// seedTaskInStatus creates a task and drives it to the requested status
// through the store's own verbs, so every test below starts from a state the
// store itself produced.
func seedTaskInStatus(t *testing.T, store TaskStore, tenantID, taskID string, status TaskStatus) *Task {
	t.Helper()
	ctx := context.Background()
	task := &Task{ID: taskID, TenantID: tenantID, Subject: "seed " + string(status)}
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("seed create: %v", err)
	}
	switch status {
	case TaskStatusPending:
	case TaskStatusClaimed:
		if err := store.ClaimTask(ctx, tenantID, taskID, "agent-seed"); err != nil {
			t.Fatalf("seed claim: %v", err)
		}
	case TaskStatusInProgress:
		if err := store.ClaimTask(ctx, tenantID, taskID, "agent-seed"); err != nil {
			t.Fatalf("seed claim: %v", err)
		}
		current, err := store.GetTask(ctx, tenantID, taskID)
		if err != nil {
			t.Fatalf("seed get: %v", err)
		}
		current.Status = TaskStatusInProgress
		if err := store.UpdateTask(ctx, current); err != nil {
			t.Fatalf("seed in_progress: %v", err)
		}
	case TaskStatusCompleted:
		if err := store.ClaimTask(ctx, tenantID, taskID, "agent-seed"); err != nil {
			t.Fatalf("seed claim: %v", err)
		}
		if err := store.CompleteTask(ctx, tenantID, taskID, &TaskResult{Output: "done"}); err != nil {
			t.Fatalf("seed complete: %v", err)
		}
	case TaskStatusFailed:
		if err := store.ClaimTask(ctx, tenantID, taskID, "agent-seed"); err != nil {
			t.Fatalf("seed claim: %v", err)
		}
		if err := store.FailTask(ctx, tenantID, taskID, "broken"); err != nil {
			t.Fatalf("seed fail: %v", err)
		}
	case TaskStatusCancelled:
		current, err := store.GetTask(ctx, tenantID, taskID)
		if err != nil {
			t.Fatalf("seed get: %v", err)
		}
		current.Status = TaskStatusCancelled
		if err := store.UpdateTask(ctx, current); err != nil {
			t.Fatalf("seed cancelled: %v", err)
		}
	case TaskStatusBlocked:
		if err := store.BlockTask(ctx, tenantID, taskID, TaskBlockKindNeedsInfo, "seed block"); err != nil {
			t.Fatalf("seed block: %v", err)
		}
	default:
		t.Fatalf("seedTaskInStatus: unsupported status %q", status)
	}
	got, err := store.GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("seed verify: %v", err)
	}
	if got.Status != status {
		t.Fatalf("seed produced status %q, want %q", got.Status, status)
	}
	return got
}

func TestTaskTransitionAllowed_Table(t *testing.T) {
	t.Parallel()

	all := []TaskStatus{
		TaskStatusPending, TaskStatusClaimed, TaskStatusInProgress,
		TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled, TaskStatusBlocked,
	}
	allowed := map[TaskStatus][]TaskStatus{
		// pending -> in_progress is the engine's own claim (go-quicbaas#966):
		// a job the engine dispatches has no agent to claim it and no gap
		// between the claim and the start, so ClaimTaskSpawn takes this edge
		// and the task stops reading as queued while its job runs.
		TaskStatusPending:    {TaskStatusClaimed, TaskStatusInProgress, TaskStatusBlocked},
		TaskStatusClaimed:    {TaskStatusInProgress, TaskStatusCompleted, TaskStatusFailed, TaskStatusBlocked},
		TaskStatusInProgress: {TaskStatusCompleted, TaskStatusFailed, TaskStatusBlocked},
		// blocked -> completed is the operator's close-as-done ruling
		// (go-quicbaas#1020), reached only through CloseBlockedTask.
		TaskStatusBlocked: {TaskStatusPending, TaskStatusCompleted, TaskStatusFailed},
		// failed -> pending is the readmission edge (go-quicbaas#945). It was
		// absent, which made `failed` a sink no recovery verb could move and
		// killed nine tasks permanently on a deploy-window fault.
		TaskStatusFailed: {TaskStatusPending},
	}
	for _, from := range all {
		for _, to := range all {
			want := false
			for _, ok := range allowed[from] {
				if ok == to {
					want = true
				}
			}
			if got := taskTransitionAllowed(from, to); got != want {
				t.Errorf("%s -> %s: got %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestBlockTask_FromEveryStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		from    TaskStatus
		allowed bool
	}{
		{TaskStatusPending, true},
		{TaskStatusClaimed, true},
		{TaskStatusInProgress, true},
		{TaskStatusCompleted, false},
		{TaskStatusFailed, false},
		{TaskStatusCancelled, false},
		{TaskStatusBlocked, false},
	}
	for _, tc := range cases {
		t.Run(string(tc.from), func(t *testing.T) {
			t.Parallel()
			store := NewInMemoryTaskStore()
			before := seedTaskInStatus(t, store, "tenant-a", "task-1", tc.from)

			err := store.BlockTask(context.Background(), "tenant-a", "task-1", TaskBlockKindEscalated, "operator must decide")
			if !tc.allowed {
				var invalid *TaskInvalidStateError
				if !errors.As(err, &invalid) {
					t.Fatalf("expected TaskInvalidStateError, got %v", err)
				}
				if invalid.CurrentState != tc.from || invalid.TargetState != TaskStatusBlocked {
					t.Fatalf("error names %s -> %s, want %s -> blocked", invalid.CurrentState, invalid.TargetState, tc.from)
				}
				after, getErr := store.GetTask(context.Background(), "tenant-a", "task-1")
				if getErr != nil {
					t.Fatalf("get: %v", getErr)
				}
				if after.Version != before.Version {
					t.Fatalf("a refused transition must not write: version %d -> %d", before.Version, after.Version)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected block from %s to succeed, got %v", tc.from, err)
			}
			after, getErr := store.GetTask(context.Background(), "tenant-a", "task-1")
			if getErr != nil {
				t.Fatalf("get: %v", getErr)
			}
			if after.Status != TaskStatusBlocked {
				t.Errorf("status = %s, want blocked", after.Status)
			}
			if after.BlockKind != TaskBlockKindEscalated || after.BlockedReason != "operator must decide" {
				t.Errorf("block kind/reason = %s/%q", after.BlockKind, after.BlockedReason)
			}
			if after.StatusChangedAt <= before.StatusChangedAt {
				t.Errorf("StatusChangedAt must advance: %d -> %d", before.StatusChangedAt, after.StatusChangedAt)
			}
			if after.Version != before.Version+1 {
				t.Errorf("version = %d, want %d", after.Version, before.Version+1)
			}
		})
	}
}

func TestBlockTask_InvalidInput(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	seedTaskInStatus(t, store, "tenant-a", "task-1", TaskStatusPending)
	ctx := context.Background()

	err := store.BlockTask(ctx, "tenant-a", "task-1", TaskBlockKind("paused"), "why")
	var badKind *InvalidTaskBlockKindError
	if !errors.As(err, &badKind) || badKind.Kind != "paused" {
		t.Fatalf("expected InvalidTaskBlockKindError for unknown kind, got %v", err)
	}
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("unknown kind must unwrap to ErrTaskStoreInvalidInput, got %v", err)
	}

	err = store.BlockTask(ctx, "tenant-a", "task-1", TaskBlockKindBlocked, "")
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput for empty reason, got %v", err)
	}

	err = store.BlockTask(ctx, "tenant-a", "missing", TaskBlockKindBlocked, "why")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.BlockTask(cancelled, "tenant-a", "task-1", TaskBlockKindBlocked, "why"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	got, getErr := store.GetTask(ctx, "tenant-a", "task-1")
	if getErr != nil {
		t.Fatalf("get: %v", getErr)
	}
	if got.Status != TaskStatusPending {
		t.Fatalf("invalid input must not transition the task, status = %s", got.Status)
	}
}

func TestReadmitTask_FromEveryStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		from    TaskStatus
		allowed bool
	}{
		{TaskStatusBlocked, true},
		{TaskStatusPending, false},
		{TaskStatusClaimed, false},
		{TaskStatusInProgress, false},
		{TaskStatusCompleted, false},
		// READMISSIBLE as of go-quicbaas#945: a task the fleet broke is not a
		// task that failed on its merits, and an operator must be able to say
		// so. completed and cancelled stay terminal.
		{TaskStatusFailed, true},
		{TaskStatusCancelled, false},
	}
	for _, tc := range cases {
		t.Run(string(tc.from), func(t *testing.T) {
			t.Parallel()
			store := NewInMemoryTaskStore()
			seedTaskInStatus(t, store, "tenant-a", "task-1", tc.from)

			err := store.ReadmitTask(context.Background(), "tenant-a", "task-1")
			if !tc.allowed {
				var invalid *TaskInvalidStateError
				if !errors.As(err, &invalid) {
					t.Fatalf("expected TaskInvalidStateError, got %v", err)
				}
				if invalid.TargetState != TaskStatusPending {
					t.Fatalf("target = %s, want pending", invalid.TargetState)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected readmit to succeed, got %v", err)
			}
		})
	}
}

func TestReadmitTask_ClearsBlockClaimAndRetryHold(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()
	seedTaskInStatus(t, store, "tenant-a", "task-1", TaskStatusClaimed)

	// A retry hold set by attempt accounting must not survive readmission.
	held, err := store.GetTask(ctx, "tenant-a", "task-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	held.RetryNotBefore = 1 << 60
	if err := store.UpdateTask(ctx, held); err != nil {
		t.Fatalf("set retry hold: %v", err)
	}
	if err := store.BlockTask(ctx, "tenant-a", "task-1", TaskBlockKindNeedsInfo, "which branch?"); err != nil {
		t.Fatalf("block: %v", err)
	}
	blocked, err := store.GetTask(ctx, "tenant-a", "task-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if err := store.ReadmitTask(ctx, "tenant-a", "task-1"); err != nil {
		t.Fatalf("readmit: %v", err)
	}
	got, err := store.GetTask(ctx, "tenant-a", "task-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != TaskStatusPending {
		t.Errorf("status = %s, want pending", got.Status)
	}
	if got.BlockKind != "" || got.BlockedReason != "" {
		t.Errorf("block must be cleared, got %s/%q", got.BlockKind, got.BlockedReason)
	}
	if got.AssignedTo != "" || got.ClaimedAt != 0 {
		t.Errorf("claim must be cleared, got %q/%d", got.AssignedTo, got.ClaimedAt)
	}
	if got.RetryNotBefore != 0 {
		t.Errorf("retry hold must be cleared, got %d", got.RetryNotBefore)
	}
	if got.StatusChangedAt <= blocked.StatusChangedAt {
		t.Errorf("StatusChangedAt must advance on readmit")
	}

	// And the readmitted task is claimable again.
	if err := store.ClaimTask(ctx, "tenant-a", "task-1", "agent-2"); err != nil {
		t.Fatalf("claim after readmit: %v", err)
	}
}

func TestReadmitTask_NotFoundAndCancelled(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	if err := store.ReadmitTask(context.Background(), "tenant-a", "missing"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.ReadmitTask(cancelled, "tenant-a", "any"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestBlockedTask_FailsButDoesNotCompleteOrClaim(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("fail retires a blocked task", func(t *testing.T) {
		t.Parallel()
		store := NewInMemoryTaskStore()
		seedTaskInStatus(t, store, "tenant-a", "task-1", TaskStatusBlocked)
		if err := store.FailTask(ctx, "tenant-a", "task-1", "abandoned"); err != nil {
			t.Fatalf("fail from blocked: %v", err)
		}
		got, err := store.GetTask(ctx, "tenant-a", "task-1")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Status != TaskStatusFailed || got.Result == nil || got.Result.Output != "abandoned" {
			t.Fatalf("unexpected record after fail: %+v", got)
		}
	})

	t.Run("complete is refused", func(t *testing.T) {
		t.Parallel()
		store := NewInMemoryTaskStore()
		seedTaskInStatus(t, store, "tenant-a", "task-1", TaskStatusBlocked)
		err := store.CompleteTask(ctx, "tenant-a", "task-1", &TaskResult{Output: "no"})
		if !errors.Is(err, ErrTaskInvalidState) {
			t.Fatalf("expected ErrTaskInvalidState, got %v", err)
		}
	})

	t.Run("claim is refused", func(t *testing.T) {
		t.Parallel()
		store := NewInMemoryTaskStore()
		seedTaskInStatus(t, store, "tenant-a", "task-1", TaskStatusBlocked)
		err := store.ClaimTask(ctx, "tenant-a", "task-1", "agent-1")
		if !errors.Is(err, ErrTaskInvalidState) {
			t.Fatalf("expected ErrTaskInvalidState, got %v", err)
		}
	})
}

func TestTaskStore_StatusChangedAt(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()
	created := seedTaskInStatus(t, store, "tenant-a", "task-1", TaskStatusPending)
	if created.StatusChangedAt == 0 || created.StatusChangedAt != created.CreatedAt {
		t.Fatalf("create must stamp StatusChangedAt = CreatedAt, got %d / %d", created.StatusChangedAt, created.CreatedAt)
	}

	// A raw edit that does not change the status keeps the stamp.
	created.Priority = 5
	if err := store.UpdateTask(ctx, created); err != nil {
		t.Fatalf("update: %v", err)
	}
	same, err := store.GetTask(ctx, "tenant-a", "task-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if same.StatusChangedAt != created.StatusChangedAt {
		t.Errorf("non-status edit moved StatusChangedAt: %d -> %d", created.StatusChangedAt, same.StatusChangedAt)
	}

	// A claim moves it.
	if err := store.ClaimTask(ctx, "tenant-a", "task-1", "agent-1"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	claimed, err := store.GetTask(ctx, "tenant-a", "task-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if claimed.StatusChangedAt <= same.StatusChangedAt {
		t.Errorf("claim must advance StatusChangedAt")
	}

	// A raw edit that changes the status moves it too. The stamp is compared
	// against the value read before the edit, since UpdateTask writes through
	// the caller's own record.
	claimedAt := claimed.StatusChangedAt
	claimed.Status = TaskStatusInProgress
	if err := store.UpdateTask(ctx, claimed); err != nil {
		t.Fatalf("update: %v", err)
	}
	inProgress, err := store.GetTask(ctx, "tenant-a", "task-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if inProgress.StatusChangedAt <= claimedAt {
		t.Errorf("status edit via UpdateTask must advance StatusChangedAt")
	}
}

func TestTaskMatchesFilter_ProjectIDAndStatuses(t *testing.T) {
	t.Parallel()

	task := &Task{Status: TaskStatusBlocked, ProjectID: "proj-1", TeamName: "core"}
	cases := []struct {
		name   string
		filter *TaskFilter
		want   bool
	}{
		{"nil filter", nil, true},
		{"project matches", &TaskFilter{ProjectID: "proj-1"}, true},
		{"project differs", &TaskFilter{ProjectID: "proj-2"}, false},
		{"statuses contains", &TaskFilter{Statuses: []TaskStatus{TaskStatusPending, TaskStatusBlocked}}, true},
		{"statuses excludes", &TaskFilter{Statuses: []TaskStatus{TaskStatusPending, TaskStatusClaimed}}, false},
		{"status and statuses both hold", &TaskFilter{Status: TaskStatusBlocked, Statuses: []TaskStatus{TaskStatusBlocked}}, true},
		{"status contradicts statuses", &TaskFilter{Status: TaskStatusPending, Statuses: []TaskStatus{TaskStatusBlocked}}, false},
		{"project and statuses", &TaskFilter{ProjectID: "proj-1", Statuses: []TaskStatus{TaskStatusBlocked}}, true},
		{"project holds, statuses fail", &TaskFilter{ProjectID: "proj-1", Statuses: []TaskStatus{TaskStatusFailed}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := taskMatchesFilter(task, tc.filter); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestListTasks_ProjectIDAndStatusesFilters(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()
	seed := []struct {
		id      string
		project string
		status  TaskStatus
	}{
		{"p1-pending", "proj-1", TaskStatusPending},
		{"p1-blocked", "proj-1", TaskStatusBlocked},
		{"p2-blocked", "proj-2", TaskStatusBlocked},
		{"none-failed", "", TaskStatusFailed},
	}
	for _, s := range seed {
		task := &Task{ID: s.id, TenantID: "tenant-a", Subject: s.id, ProjectID: s.project}
		if err := store.CreateTask(ctx, task); err != nil {
			t.Fatalf("create %s: %v", s.id, err)
		}
		switch s.status {
		case TaskStatusBlocked:
			if err := store.BlockTask(ctx, "tenant-a", s.id, TaskBlockKindBlocked, "waiting"); err != nil {
				t.Fatalf("block %s: %v", s.id, err)
			}
		case TaskStatusFailed:
			if err := store.ClaimTask(ctx, "tenant-a", s.id, "agent"); err != nil {
				t.Fatalf("claim %s: %v", s.id, err)
			}
			if err := store.FailTask(ctx, "tenant-a", s.id, "x"); err != nil {
				t.Fatalf("fail %s: %v", s.id, err)
			}
		}
	}

	ids := func(tasks []*Task) map[string]bool {
		out := make(map[string]bool, len(tasks))
		for _, task := range tasks {
			out[task.ID] = true
		}
		return out
	}

	byProject, err := store.ListTasks(ctx, "tenant-a", &TaskFilter{ProjectID: "proj-1"})
	if err != nil {
		t.Fatalf("list by project: %v", err)
	}
	if got := ids(byProject); len(got) != 2 || !got["p1-pending"] || !got["p1-blocked"] {
		t.Errorf("project filter returned %v", got)
	}

	byStatuses, err := store.ListTasks(ctx, "tenant-a", &TaskFilter{Statuses: []TaskStatus{TaskStatusBlocked, TaskStatusFailed}})
	if err != nil {
		t.Fatalf("list by statuses: %v", err)
	}
	if got := ids(byStatuses); len(got) != 3 || got["p1-pending"] {
		t.Errorf("statuses filter returned %v", got)
	}

	both, err := store.ListTasks(ctx, "tenant-a", &TaskFilter{ProjectID: "proj-2", Statuses: []TaskStatus{TaskStatusBlocked}})
	if err != nil {
		t.Fatalf("list by both: %v", err)
	}
	if got := ids(both); len(got) != 1 || !got["p2-blocked"] {
		t.Errorf("combined filter returned %v", got)
	}

	none, err := store.ListTasks(ctx, "tenant-a", &TaskFilter{ProjectID: "proj-9"})
	if err != nil {
		t.Fatalf("list by unknown project: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("unknown project returned %d tasks", len(none))
	}
}

func TestTaskSourceKind_Validate(t *testing.T) {
	t.Parallel()

	for _, kind := range []TaskSourceKind{TaskSourceKindScheduler, TaskSourceKindOperator, TaskSourceKindJob} {
		if err := kind.Validate(); err != nil {
			t.Errorf("%s: unexpected error %v", kind, err)
		}
	}
	for _, kind := range []TaskSourceKind{"", "cron", "OPERATOR"} {
		err := kind.Validate()
		var invalid *InvalidTaskSourceKindError
		if !errors.As(err, &invalid) || invalid.Kind != kind {
			t.Errorf("%q: expected InvalidTaskSourceKindError, got %v", kind, err)
		}
		if !errors.Is(err, ErrTaskStoreInvalidInput) {
			t.Errorf("%q: must unwrap to ErrTaskStoreInvalidInput", kind)
		}
		if err.Error() == "" {
			t.Errorf("%q: empty error message", kind)
		}
	}
}

func TestTaskBlockKind_Validate(t *testing.T) {
	t.Parallel()

	for _, kind := range []TaskBlockKind{TaskBlockKindNeedsInfo, TaskBlockKindBlocked, TaskBlockKindEscalated} {
		if err := kind.Validate(); err != nil {
			t.Errorf("%s: unexpected error %v", kind, err)
		}
	}
	for _, kind := range []TaskBlockKind{"", "paused", "NEEDS_INFO"} {
		err := kind.Validate()
		var invalid *InvalidTaskBlockKindError
		if !errors.As(err, &invalid) || invalid.Kind != kind {
			t.Errorf("%q: expected InvalidTaskBlockKindError, got %v", kind, err)
		}
		if err.Error() == "" {
			t.Errorf("%q: empty error message", kind)
		}
	}
}

func TestTaskSource_Validate(t *testing.T) {
	t.Parallel()

	var nilSource *TaskSourceRef
	if err := nilSource.Validate(); err != nil {
		t.Errorf("nil source must be valid, got %v", err)
	}
	if err := (&TaskSourceRef{Kind: TaskSourceKindOperator, Ref: "alice"}).Validate(); err != nil {
		t.Errorf("valid source rejected: %v", err)
	}
	if err := (&TaskSourceRef{Kind: "nope", Ref: "x"}).Validate(); !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Errorf("unknown kind: expected ErrTaskStoreInvalidInput, got %v", err)
	}
	if err := (&TaskSourceRef{Kind: TaskSourceKindScheduler}).Validate(); !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Errorf("empty ref: expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestCreateTask_SourceIndex_RefusesSecondAdoption(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()
	source := &TaskSourceRef{Kind: TaskSourceKindOperator, Ref: "owner/repo#12", URL: "https://example.invalid/12"}

	first := &Task{ID: "task-1", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "first", Source: source}
	if err := store.CreateTask(ctx, first); err != nil {
		t.Fatalf("first adoption: %v", err)
	}

	second := &Task{ID: "task-2", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "second", Source: source}
	err := store.CreateTask(ctx, second)
	var adopted *TaskSourceAlreadyAdoptedError
	if !errors.As(err, &adopted) {
		t.Fatalf("expected TaskSourceAlreadyAdoptedError, got %v", err)
	}
	if adopted.TaskID != "task-1" || adopted.ProjectID != "proj-1" || adopted.Kind != TaskSourceKindOperator || adopted.Ref != "owner/repo#12" {
		t.Fatalf("error does not name the holder: %+v", adopted)
	}
	if !errors.Is(err, ErrTaskSourceAlreadyAdopted) {
		t.Fatalf("must unwrap to ErrTaskSourceAlreadyAdopted")
	}
	if !strings.Contains(err.Error(), "task-1") {
		t.Fatalf("message must name the holder: %s", err.Error())
	}
	if _, getErr := store.GetTask(ctx, "tenant-a", "task-2"); !errors.Is(getErr, ErrTaskNotFound) {
		t.Fatalf("refused task must not be written, got %v", getErr)
	}

	// The same source under another project, another tenant, or another
	// kind is a different origin.
	for _, variant := range []*Task{
		{ID: "task-3", TenantID: "tenant-a", ProjectID: "proj-2", Subject: "other project", Source: source},
		{ID: "task-4", TenantID: "tenant-b", ProjectID: "proj-1", Subject: "other tenant", Source: source},
		{ID: "task-5", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "other kind", Source: &TaskSourceRef{Kind: TaskSourceKindScheduler, Ref: "owner/repo#12"}},
		{ID: "task-6", TenantID: "tenant-a", Subject: "unscoped", Source: source},
	} {
		if err := store.CreateTask(ctx, variant); err != nil {
			t.Errorf("%s: %v", variant.Subject, err)
		}
	}

	// And a second unscoped adoption is refused like a scoped one.
	err = store.CreateTask(ctx, &Task{ID: "task-7", TenantID: "tenant-a", Subject: "unscoped again", Source: source})
	if !errors.As(err, &adopted) || adopted.TaskID != "task-6" {
		t.Fatalf("unscoped second adoption: expected holder task-6, got %v", err)
	}
}

func TestCreateTask_SourceIndex_ReleasedOnDelete(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()
	source := &TaskSourceRef{Kind: TaskSourceKindScheduler, Ref: "nightly"}

	if err := store.CreateTask(ctx, &Task{ID: "task-1", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "a", Source: source}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.DeleteTask(ctx, "tenant-a", "task-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	next := &Task{ID: "task-2", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "b", Source: source}
	if err := store.CreateTask(ctx, next); err != nil {
		t.Fatalf("re-adoption after delete must succeed, got %v", err)
	}
	// The delete gave the reservation back, so the new task took nothing
	// over: a predecessor here would mean the entry had outlived the record.
	if next.PredecessorTaskID() != "" {
		t.Fatalf("a deleted task leaves nothing to supersede, got %q", next.PredecessorTaskID())
	}
}

func TestCreateTask_SourceIndex_ReleasedOnDuplicateID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()
	source := &TaskSourceRef{Kind: TaskSourceKindJob, Ref: "job-9"}

	if err := store.CreateTask(ctx, &Task{ID: "task-1", TenantID: "tenant-a", Subject: "a"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Same ID, now with a source: the record create is refused, and the
	// reservation taken for it must be given back.
	err := store.CreateTask(ctx, &Task{ID: "task-1", TenantID: "tenant-a", Subject: "dup", Source: source})
	if !errors.Is(err, ErrTaskAlreadyExists) {
		t.Fatalf("expected ErrTaskAlreadyExists, got %v", err)
	}
	if err := store.CreateTask(ctx, &Task{ID: "task-2", TenantID: "tenant-a", Subject: "b", Source: source}); err != nil {
		t.Fatalf("source must not stay reserved after a refused create, got %v", err)
	}
}

func TestCreateTask_SourceIndex_TenantCascade(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()
	source := &TaskSourceRef{Kind: TaskSourceKindOperator, Ref: "alice"}

	if err := store.CreateTask(ctx, &Task{ID: "task-1", TenantID: "tenant-a", Subject: "a", Source: source}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.DeleteTenantTasks(ctx, "tenant-a"); err != nil {
		t.Fatalf("cascade: %v", err)
	}
	if err := store.CreateTask(ctx, &Task{ID: "task-2", TenantID: "tenant-a", Subject: "b", Source: source}); err != nil {
		t.Fatalf("re-adoption after tenant cascade must succeed, got %v", err)
	}
}

func TestCreateTask_SourceInvalidRefused(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()

	err := store.CreateTask(ctx, &Task{ID: "task-1", TenantID: "tenant-a", Subject: "a", Source: &TaskSourceRef{Kind: "bogus", Ref: "x"}})
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput for unknown kind, got %v", err)
	}
	err = store.CreateTask(ctx, &Task{ID: "task-1", TenantID: "tenant-a", Subject: "a", Source: &TaskSourceRef{Kind: TaskSourceKindOperator}})
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput for empty ref, got %v", err)
	}
	if _, getErr := store.GetTask(ctx, "tenant-a", "task-1"); !errors.Is(getErr, ErrTaskNotFound) {
		t.Fatalf("invalid source must not be written, got %v", getErr)
	}
}

func TestCreateTask_SourceIndex_WithoutReleaserRefuses(t *testing.T) {
	t.Parallel()

	// A store whose KV can guard a create but not a delete cannot give a
	// reservation back, so it must refuse a sourced create up front rather
	// than write a reservation it could never release.
	store := NewTaskStore(&createOnlyKV{KVStore: NewMemoryTaskKV()}, NewProcessClaimArbiter())
	err := store.CreateTask(context.Background(), &Task{
		ID: "task-1", TenantID: "tenant-a", Subject: "a",
		Source: &TaskSourceRef{Kind: TaskSourceKindOperator, Ref: "alice"},
	})
	if !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %v", err)
	}
	// An unsourced create on the same store is unaffected.
	if err := store.CreateTask(context.Background(), &Task{ID: "task-2", TenantID: "tenant-a", Subject: "b"}); err != nil {
		t.Fatalf("unsourced create: %v", err)
	}
}

// createOnlyKV forwards a guarded create and hides the guarded delete.
type createOnlyKV struct {
	interfaces.KVStore
}

func (k *createOnlyKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

func TestTaskSourceIndexKey_Shape(t *testing.T) {
	t.Parallel()

	key, err := taskSourceIndexKey("tenant-a", "proj-1", &TaskSourceRef{Kind: TaskSourceKindOperator, Ref: "owner/repo#12"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(key, "orch/task-source/tenant-a/proj-1/operator/") {
		t.Errorf("unexpected key shape: %s", key)
	}
	if strings.Contains(strings.TrimPrefix(key, "orch/task-source/tenant-a/proj-1/operator/"), "/") {
		t.Errorf("ref must be encoded onto one segment: %s", key)
	}

	unscoped, err := taskSourceIndexKey("tenant-a", "", &TaskSourceRef{Kind: TaskSourceKindOperator, Ref: "alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(unscoped, "orch/task-source/tenant-a/"+taskSourceUnscopedProject+"/operator/") {
		t.Errorf("unexpected unscoped key: %s", unscoped)
	}

	if _, err := taskSourceIndexKey("", "proj-1", &TaskSourceRef{Kind: TaskSourceKindOperator, Ref: "alice"}); !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Errorf("empty tenant: expected ErrTaskStoreInvalidInput, got %v", err)
	}
	if _, err := taskSourceIndexKey("tenant-a", "a/b", &TaskSourceRef{Kind: TaskSourceKindOperator, Ref: "alice"}); !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Errorf("separator in project: expected ErrTaskStoreInvalidInput, got %v", err)
	}
	if _, err := taskSourceIndexKey("tenant-a", "proj-1", &TaskSourceRef{Kind: "nope", Ref: "x"}); !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Errorf("bad kind: expected ErrTaskStoreInvalidInput, got %v", err)
	}
}

func TestTaskSourceAlreadyAdoptedError_Message(t *testing.T) {
	t.Parallel()

	err := &TaskSourceAlreadyAdoptedError{
		TenantID: "t", ProjectID: "p", Kind: TaskSourceKindJob, Ref: "r",
		TaskID: "task-9", HolderStatus: TaskStatusInProgress,
	}
	for _, want := range []string{"tenant=t", "project=p", "kind=job", "ref=r", "task=task-9", "holder_status=in_progress"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err.Error(), want)
		}
	}
	if !errors.Is(err, ErrTaskSourceAlreadyAdopted) {
		t.Error("must unwrap to ErrTaskSourceAlreadyAdopted")
	}

	// A holder whose state could not be read says so by omission rather than
	// by claiming a state it does not have.
	unknown := &TaskSourceAlreadyAdoptedError{TenantID: "t", ProjectID: "p", Kind: TaskSourceKindJob, Ref: "r", TaskID: "task-9"}
	if strings.Contains(unknown.Error(), "holder_status=") {
		t.Errorf("an unknown holder state must not be reported: %s", unknown.Error())
	}
}

// =============================================================================
// A reservation binds only while its holder can still work the source (#652)
// =============================================================================

// sourceTakeoverFaultKV guards a create and a delete like the real store, and
// breaks the guarded delete of a source reservation — the half of a take-over
// that clears a terminal holder. deleteErr fails it outright; swallowDelete
// makes it report that it removed nothing, which is what losing a race to
// another adopter looks like.
type sourceTakeoverFaultKV struct {
	interfaces.KVStore

	deleteErr     error
	swallowDelete bool
}

func (k *sourceTakeoverFaultKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

func (k *sourceTakeoverFaultKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	if strings.HasPrefix(key, taskSourceKeyspace) {
		if k.deleteErr != nil {
			return false, k.deleteErr
		}
		if k.swallowDelete {
			return false, nil
		}
	}
	return forwardCompareAndDelete(ctx, k.KVStore, key, expected)
}

// spendTheAttemptBudget drives a task to the terminal failure a spent attempt
// budget produces, through the same derivation a concluded job goes through.
func spendTheAttemptBudget(t *testing.T, store TaskStore, task *Task) {
	t.Helper()

	task.Attempts = task.EffectiveAttemptBudget()
	applyConclusion(task, attemptConclusion{
		status: &JobStatus{JobID: "job-1", State: JobStateFailed, Error: "the work failed"},
	}, time.Now().UTC())
	if task.Status != TaskStatusFailed || task.Result.Metadata["reason"] != budgetExhaustedReason {
		t.Fatalf("the budget must be spent, got status=%s result=%+v", task.Status, task.Result)
	}
	if err := store.UpdateTask(context.Background(), task); err != nil {
		t.Fatalf("write the terminal task: %v", err)
	}
}

// TestCreateTask_SourceIndex_SupersedesAHolderThatSpentItsBudget is the live
// failure from #652: a task burned every attempt, so re-labelling the issue
// must adopt it again instead of finding a reservation nothing can release.
// The terminal task stays for history and the new task names it.
func TestCreateTask_SourceIndex_SupersedesAHolderThatSpentItsBudget(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()
	source := &TaskSourceRef{Kind: TaskSourceKindOperator, Ref: "owner/repo#53"}

	spent := &Task{ID: "task-1", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "first run", Source: source, AttemptBudget: 3}
	if err := store.CreateTask(ctx, spent); err != nil {
		t.Fatalf("first adoption: %v", err)
	}
	spendTheAttemptBudget(t, store, spent)

	fresh := &Task{ID: "task-2", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "re-labelled", Source: source}
	if err := store.CreateTask(ctx, fresh); err != nil {
		t.Fatalf("re-adoption after a spent budget must succeed, got %v", err)
	}
	if fresh.PredecessorTaskID() != "task-1" {
		t.Fatalf("the new task must name its predecessor, got %q", fresh.PredecessorTaskID())
	}

	stored, err := store.GetTask(ctx, "tenant-a", "task-2")
	if err != nil {
		t.Fatalf("read the new task: %v", err)
	}
	if stored.PredecessorTaskID() != "task-1" {
		t.Fatalf("the predecessor must be on the record, got %q", stored.PredecessorTaskID())
	}
	history, err := store.GetTask(ctx, "tenant-a", "task-1")
	if err != nil {
		t.Fatalf("the terminal task is kept for history: %v", err)
	}
	if history.Status != TaskStatusFailed {
		t.Fatalf("the terminal task must be untouched, got status %s", history.Status)
	}

	// And the new task now holds the reservation itself.
	third := &Task{ID: "task-3", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "third", Source: source}
	var adopted *TaskSourceAlreadyAdoptedError
	if err := store.CreateTask(ctx, third); !errors.As(err, &adopted) || adopted.TaskID != "task-2" {
		t.Fatalf("expected the new task to hold the source, got %v", err)
	}
}

// TestCreateTask_SourceIndex_HolderStillWorkingItKeepsIt: every status a task
// can still leave holds its origin, and the refusal names the holder and the
// state it is in — the two facts a sweep had no way to report (#652).
func TestCreateTask_SourceIndex_HolderStillWorkingItKeepsIt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status TaskStatus
	}{
		{name: "pending", status: TaskStatusPending},
		{name: "claimed", status: TaskStatusClaimed},
		{name: "in progress", status: TaskStatusInProgress},
		{name: "blocked", status: TaskStatusBlocked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := NewInMemoryTaskStore()
			ctx := context.Background()
			source := &TaskSourceRef{Kind: TaskSourceKindOperator, Ref: "owner/repo#" + tc.name}

			holder := &Task{ID: "holder", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "live", Source: source}
			if err := store.CreateTask(ctx, holder); err != nil {
				t.Fatalf("first adoption: %v", err)
			}
			holder.Status = tc.status
			if err := store.UpdateTask(ctx, holder); err != nil {
				t.Fatalf("set status %s: %v", tc.status, err)
			}

			second := &Task{ID: "second", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "second", Source: source}
			err := store.CreateTask(ctx, second)
			var adopted *TaskSourceAlreadyAdoptedError
			if !errors.As(err, &adopted) {
				t.Fatalf("expected TaskSourceAlreadyAdoptedError, got %v", err)
			}
			if adopted.TaskID != "holder" || adopted.HolderStatus != tc.status {
				t.Fatalf("the refusal must name the holder and its state, got %+v", adopted)
			}
			if second.PredecessorTaskID() != "" {
				t.Fatalf("a refused adoption supersedes nothing, got %q", second.PredecessorTaskID())
			}
		})
	}
}

// TestCreateTask_SourceIndex_CompletedHolderReleasesIt: completed is terminal
// exactly as failed is, so an origin whose work finished is adoptable again
// when a person asks for it. Cancelled behaves the same way.
func TestCreateTask_SourceIndex_CompletedHolderReleasesIt(t *testing.T) {
	t.Parallel()

	for _, status := range []TaskStatus{TaskStatusCompleted, TaskStatusCancelled} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()

			store := NewInMemoryTaskStore()
			ctx := context.Background()
			source := &TaskSourceRef{Kind: TaskSourceKindScheduler, Ref: "nightly-" + string(status)}

			holder := &Task{ID: "holder", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "done", Source: source}
			if err := store.CreateTask(ctx, holder); err != nil {
				t.Fatalf("first adoption: %v", err)
			}
			holder.Status = status
			if err := store.UpdateTask(ctx, holder); err != nil {
				t.Fatalf("set status %s: %v", status, err)
			}

			next := &Task{ID: "next", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "again", Source: source}
			if err := store.CreateTask(ctx, next); err != nil {
				t.Fatalf("re-adoption after %s must succeed, got %v", status, err)
			}
			if next.PredecessorTaskID() != "holder" {
				t.Fatalf("the new task must name its predecessor, got %q", next.PredecessorTaskID())
			}
		})
	}
}

// TestCreateTask_SourceIndex_SupersedesAHolderBlockedOnEnvironmentFault is the
// live failure from #739: go-truststrap#54 concluded blocked on
// no_account_resolver, and after the deployment fault was fixed re-labelling
// the issue must adopt it again. A block on a classified environment fault is
// a transient hold, not an operator hold, so the re-adoption supersedes it,
// names it, and records the class it was held on. The blocked task stays for
// history.
func TestCreateTask_SourceIndex_SupersedesAHolderBlockedOnEnvironmentFault(t *testing.T) {
	t.Parallel()

	for _, class := range []string{FaultNoAccountResolver, FaultNoProviderAccount} {
		t.Run(class, func(t *testing.T) {
			t.Parallel()

			store := NewInMemoryTaskStore()
			ctx := context.Background()
			source := &TaskSourceRef{Kind: TaskSourceKindOperator, Ref: "owner/repo#" + class}

			blocked := &Task{ID: "task-1", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "first run", Source: source, AttemptBudget: 3}
			if err := store.CreateTask(ctx, blocked); err != nil {
				t.Fatalf("first adoption: %v", err)
			}
			blocked.Status = TaskStatusBlocked
			blocked.BlockKind = TaskBlockKindBlocked
			blocked.BlockedReason = "the pool had no resolver"
			blocked.BlockFaultClass = class
			if err := store.UpdateTask(ctx, blocked); err != nil {
				t.Fatalf("block on the environment fault: %v", err)
			}

			fresh := &Task{ID: "task-2", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "re-labelled", Source: source}
			if err := store.CreateTask(ctx, fresh); err != nil {
				t.Fatalf("re-adoption after an environment-fault block must succeed, got %v", err)
			}
			if fresh.PredecessorTaskID() != "task-1" {
				t.Fatalf("the new task must name its predecessor, got %q", fresh.PredecessorTaskID())
			}
			if fresh.PredecessorBlockReason() != class {
				t.Fatalf("the new task must record the block class it re-armed, got %q", fresh.PredecessorBlockReason())
			}

			stored, err := store.GetTask(ctx, "tenant-a", "task-2")
			if err != nil {
				t.Fatalf("read the new task: %v", err)
			}
			if stored.PredecessorTaskID() != "task-1" || stored.PredecessorBlockReason() != class {
				t.Fatalf("the predecessor and its block reason must be on the record, got %+v", stored.Metadata)
			}
			history, err := store.GetTask(ctx, "tenant-a", "task-1")
			if err != nil {
				t.Fatalf("the blocked task is kept for history: %v", err)
			}
			if history.Status != TaskStatusBlocked || history.BlockFaultClass != class {
				t.Fatalf("the blocked task must be untouched, got status=%s class=%s", history.Status, history.BlockFaultClass)
			}
		})
	}
}

// TestCreateTask_SourceIndex_HolderBlockedOnNonEnvironmentReasonKeepsIt: a
// block that no environment fault produced is a deliberate operator hold —
// a policy or verification hold — and binds its source. Re-labelling must not
// supersede it; the refusal names the holder and its blocked state (#739).
func TestCreateTask_SourceIndex_HolderBlockedOnNonEnvironmentReasonKeepsIt(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	ctx := context.Background()
	source := &TaskSourceRef{Kind: TaskSourceKindOperator, Ref: "owner/repo#hold"}

	holder := &Task{ID: "holder", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "held", Source: source}
	if err := store.CreateTask(ctx, holder); err != nil {
		t.Fatalf("first adoption: %v", err)
	}
	// A deliberate operator hold: blocked, with a reason but no fault class.
	if err := store.BlockTask(ctx, "tenant-a", "holder", TaskBlockKindBlocked, "waiting on a policy decision"); err != nil {
		t.Fatalf("operator block: %v", err)
	}

	second := &Task{ID: "second", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "second", Source: source}
	err := store.CreateTask(ctx, second)
	var adopted *TaskSourceAlreadyAdoptedError
	if !errors.As(err, &adopted) {
		t.Fatalf("a deliberate operator hold must keep its source, got %v", err)
	}
	if adopted.TaskID != "holder" || adopted.HolderStatus != TaskStatusBlocked {
		t.Fatalf("the refusal must name the held holder, got %+v", adopted)
	}
	if adopted.HolderBlockFaultClass != "" {
		t.Fatalf("an operator hold carries no fault class, got %q", adopted.HolderBlockFaultClass)
	}
	if second.PredecessorTaskID() != "" {
		t.Fatalf("a refused adoption supersedes nothing, got %q", second.PredecessorTaskID())
	}
}

// TestCreateTask_SourceIndex_OrphanedReservationIsAdoptable: a reservation
// that outlived the task that made it — residue a delete could not finish —
// holds nothing back.
func TestCreateTask_SourceIndex_OrphanedReservationIsAdoptable(t *testing.T) {
	t.Parallel()

	kv := NewMemoryTaskKV()
	store := NewTaskStore(kv, NewProcessClaimArbiter())
	ctx := context.Background()
	source := &TaskSourceRef{Kind: TaskSourceKindJob, Ref: "job-orphan"}

	if err := store.CreateTask(ctx, &Task{ID: "task-1", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "a", Source: source}); err != nil {
		t.Fatalf("first adoption: %v", err)
	}
	recordKey, err := taskRecordKey("tenant-a", "task-1")
	if err != nil {
		t.Fatalf("task record key: %v", err)
	}
	// Remove the record behind the store's back, leaving the reservation.
	if err := kv.Delete(ctx, recordKey); err != nil {
		t.Fatalf("orphan the reservation: %v", err)
	}

	next := &Task{ID: "task-2", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "b", Source: source}
	if err := store.CreateTask(ctx, next); err != nil {
		t.Fatalf("an orphaned reservation must not hold the source, got %v", err)
	}
	if next.PredecessorTaskID() != "task-1" {
		t.Fatalf("the new task must still name what it took over from, got %q", next.PredecessorTaskID())
	}
}

// TestCreateTask_SourceIndex_TakeoverFailureIsReported: a take-over whose
// guarded delete fails reports the backend failure rather than writing a task
// that does not hold its origin.
func TestCreateTask_SourceIndex_TakeoverFailureIsReported(t *testing.T) {
	t.Parallel()

	kv := &sourceTakeoverFaultKV{KVStore: NewMemoryTaskKV()}
	store := NewTaskStore(kv, NewProcessClaimArbiter())
	ctx := context.Background()
	source := &TaskSourceRef{Kind: TaskSourceKindJob, Ref: "job-fault"}

	spent := &Task{ID: "task-1", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "a", Source: source, AttemptBudget: 1}
	if err := store.CreateTask(ctx, spent); err != nil {
		t.Fatalf("first adoption: %v", err)
	}
	spendTheAttemptBudget(t, store, spent)

	kv.deleteErr = errors.New("kv unavailable")
	err := store.CreateTask(ctx, &Task{ID: "task-2", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "b", Source: source})
	if !errors.Is(err, ErrTaskStoreBackend) {
		t.Fatalf("expected a backend failure, got %v", err)
	}
	if _, getErr := store.GetTask(ctx, "tenant-a", "task-2"); !errors.Is(getErr, ErrTaskNotFound) {
		t.Fatalf("a task whose take-over failed must not be written, got %v", getErr)
	}
}

// TestCreateTask_SourceIndex_TakeoverLostIsRefused: a take-over that never
// wins the entry — another adopter changed it under every look — refuses the
// create, naming the holder it last saw, instead of writing a second task for
// one origin.
func TestCreateTask_SourceIndex_TakeoverLostIsRefused(t *testing.T) {
	t.Parallel()

	kv := &sourceTakeoverFaultKV{KVStore: NewMemoryTaskKV()}
	store := NewTaskStore(kv, NewProcessClaimArbiter())
	ctx := context.Background()
	source := &TaskSourceRef{Kind: TaskSourceKindJob, Ref: "job-lost"}

	spent := &Task{ID: "task-1", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "a", Source: source, AttemptBudget: 1}
	if err := store.CreateTask(ctx, spent); err != nil {
		t.Fatalf("first adoption: %v", err)
	}
	spendTheAttemptBudget(t, store, spent)

	kv.swallowDelete = true
	err := store.CreateTask(ctx, &Task{ID: "task-2", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "b", Source: source})
	var adopted *TaskSourceAlreadyAdoptedError
	if !errors.As(err, &adopted) {
		t.Fatalf("expected TaskSourceAlreadyAdoptedError, got %v", err)
	}
	if adopted.TaskID != "task-1" || adopted.HolderStatus != TaskStatusFailed {
		t.Fatalf("the refusal must name the holder it last saw: %+v", adopted)
	}
}

// TestTaskSourceReleasable covers the predicate the whole take-over turns on:
// a status a task can never leave releases its origin, every status it can
// still leave holds it, and a blocked holder releases only when its block is a
// transient environment fault (#739).
func TestTaskSourceReleasable(t *testing.T) {
	t.Parallel()

	for _, status := range []TaskStatus{TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled, ""} {
		if !taskSourceReleasable(status, "") {
			t.Errorf("status %q is terminal and must release its source", status)
		}
	}
	for _, status := range []TaskStatus{TaskStatusPending, TaskStatusClaimed, TaskStatusInProgress, "nonsense"} {
		if taskSourceReleasable(status, "") {
			t.Errorf("status %q must keep its source", status)
		}
	}

	// A block with no fault class is a deliberate operator hold and binds.
	if taskSourceReleasable(TaskStatusBlocked, "") {
		t.Error("an operator hold with no fault class must keep its source")
	}
	// A block a non-environment reason produced never has a known fault class,
	// so it stays held.
	if taskSourceReleasable(TaskStatusBlocked, "policy_violation") {
		t.Error("a non-environment block must keep its source")
	}
	// A block a classified environment fault produced releases: the condition
	// is resolved out of band and a re-label re-arms it.
	for _, class := range []string{
		FaultNoAccountResolver,
		FaultNoProviderAccount,
		FaultAccountCredentialMissing,
	} {
		if !taskSourceReleasable(TaskStatusBlocked, class) {
			t.Errorf("a block on environment fault %q must release its source", class)
		}
	}
	for _, class := range EnvironmentalTerminalReasons() {
		if !taskSourceReleasable(TaskStatusBlocked, class) {
			t.Errorf("a block on environmental terminal reason %q must release its source", class)
		}
	}
}

// TestRecordSourcePredecessor covers what it writes and what it must not
// touch: the metadata map handed in may be the one a TaskSource offered its
// spec with, and is not the store's to mutate.
func TestRecordSourcePredecessor(t *testing.T) {
	t.Parallel()

	offered := map[string]string{"label": "ready"}
	task := &Task{ID: "task-2", Metadata: offered}
	recordSourcePredecessor(task, "task-1", FaultNoAccountResolver)
	if task.PredecessorTaskID() != "task-1" {
		t.Fatalf("predecessor not recorded: %+v", task.Metadata)
	}
	if task.PredecessorBlockReason() != FaultNoAccountResolver {
		t.Fatalf("predecessor block reason not recorded: %+v", task.Metadata)
	}
	if task.Metadata["label"] != "ready" {
		t.Fatalf("the offered metadata must be kept: %+v", task.Metadata)
	}
	if _, mutated := offered[taskSourcePredecessorKey]; mutated {
		t.Fatalf("the caller's map must not be written through: %+v", offered)
	}

	bare := &Task{ID: "task-3"}
	if bare.PredecessorTaskID() != "" {
		t.Fatalf("a task with no metadata has no predecessor, got %q", bare.PredecessorTaskID())
	}
	if bare.PredecessorBlockReason() != "" {
		t.Fatalf("a task with no metadata has no predecessor block reason, got %q", bare.PredecessorBlockReason())
	}
	// A terminal predecessor names no block reason, and none is written.
	recordSourcePredecessor(bare, "task-1", "")
	if bare.PredecessorTaskID() != "task-1" {
		t.Fatalf("predecessor not recorded on a task with no metadata: %+v", bare.Metadata)
	}
	if bare.PredecessorBlockReason() != "" {
		t.Fatalf("a terminal predecessor must name no block reason, got %q", bare.PredecessorBlockReason())
	}
}

// TestTaskMatchesFilter_AutoSpawnIsTriState proves the auto_spawn narrowing
// tells "dispatched immediately", "queued for a claim" and "no preference"
// apart — the distinction a plain bool could not carry.
func TestTaskMatchesFilter_AutoSpawnIsTriState(t *testing.T) {
	t.Parallel()

	dispatched := &Task{Status: TaskStatusInProgress, AutoSpawn: true}
	queued := &Task{Status: TaskStatusPending}
	yes, no := true, false

	cases := []struct {
		name   string
		task   *Task
		filter *TaskFilter
		want   bool
	}{
		{"unset lists the dispatched", dispatched, &TaskFilter{}, true},
		{"unset lists the queued", queued, &TaskFilter{}, true},
		{"true keeps the dispatched", dispatched, &TaskFilter{AutoSpawn: &yes}, true},
		{"true drops the queued", queued, &TaskFilter{AutoSpawn: &yes}, false},
		{"false keeps the queued", queued, &TaskFilter{AutoSpawn: &no}, true},
		{"false drops the dispatched", dispatched, &TaskFilter{AutoSpawn: &no}, false},
		{"composes with the status", dispatched, &TaskFilter{AutoSpawn: &yes, Status: TaskStatusInProgress}, true},
		{"one failing narrowing refuses the record", dispatched, &TaskFilter{AutoSpawn: &yes, Status: TaskStatusFailed}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := taskMatchesFilter(tc.task, tc.filter); got != tc.want {
				t.Errorf("taskMatchesFilter() = %v, want %v", got, tc.want)
			}
		})
	}
}
