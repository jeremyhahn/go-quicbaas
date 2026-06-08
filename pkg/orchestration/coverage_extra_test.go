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
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// -----------------------------------------------------------------------------
// project_store: context cancellation and input-validation branches
// -----------------------------------------------------------------------------

// TestProjectStore_GetByName_CancelledContext covers the ctx.Err() early-return.
func TestProjectStore_GetByName_CancelledContext(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.GetByName(ctx, "tenant-1", "name"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// TestProjectStore_GetByName_EmptyTenantID covers the empty-tenant guard.
func TestProjectStore_GetByName_EmptyTenantID(t *testing.T) {
	store := NewInMemoryProjectStore()
	_, err := store.GetByName(context.Background(), "", "name")
	var inv *ProjectInvalidError
	if !errors.As(err, &inv) || inv.Field != "tenant_id" {
		t.Fatalf("want ProjectInvalidError(tenant_id), got %v", err)
	}
}

// TestProjectStore_Update_CancelledContext covers Update's ctx.Err() guard.
func TestProjectStore_Update_CancelledContext(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Update(ctx, newTestProject("p1", "tenant-1", "n")); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// TestProjectStore_Update_EmptyID covers Update's empty-ID validation.
func TestProjectStore_Update_EmptyID(t *testing.T) {
	store := NewInMemoryProjectStore()
	err := store.Update(context.Background(), &Project{TenantID: "tenant-1"})
	var inv *ProjectInvalidError
	if !errors.As(err, &inv) || inv.Field != "id" {
		t.Fatalf("want ProjectInvalidError(id), got %v", err)
	}
}

// TestProjectStore_Update_EmptyTenantID covers Update's empty-tenant validation.
func TestProjectStore_Update_EmptyTenantID(t *testing.T) {
	store := NewInMemoryProjectStore()
	err := store.Update(context.Background(), &Project{ID: "p1"})
	var inv *ProjectInvalidError
	if !errors.As(err, &inv) || inv.Field != "tenant_id" {
		t.Fatalf("want ProjectInvalidError(tenant_id), got %v", err)
	}
}

// TestProjectStore_Update_NameConflict covers the name-index conflict branch:
// renaming a project to a name already held by a sibling project in the same
// tenant must surface ProjectAlreadyExistsError and must not mutate state.
func TestProjectStore_Update_NameConflict(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()
	if err := store.Create(ctx, newTestProject("p1", "tenant-1", "alpha")); err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	if err := store.Create(ctx, newTestProject("p2", "tenant-1", "beta")); err != nil {
		t.Fatalf("create beta: %v", err)
	}

	// Attempt to rename p2 -> "alpha" (already taken by p1).
	rename := newTestProject("p2", "tenant-1", "alpha")
	err := store.Update(ctx, rename)
	if !errors.Is(err, ErrProjectAlreadyExists) {
		t.Fatalf("want ErrProjectAlreadyExists, got %v", err)
	}

	// p2 must still be reachable under its original name "beta".
	if _, gErr := store.GetByName(ctx, "tenant-1", "beta"); gErr != nil {
		t.Fatalf("p2 should still resolve under original name beta: %v", gErr)
	}
}

// TestProjectStore_ListByTenant_CancelledContext covers ListByTenant's guard.
func TestProjectStore_ListByTenant_CancelledContext(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.ListByTenant(ctx, "tenant-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// session_log: matchesFilter false branches
// -----------------------------------------------------------------------------

// TestMatchesFilter covers every rejection dimension of matchesFilter plus the
// nil-filter match-all case.
func TestMatchesFilter(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	meta := &SessionMeta{
		Backend:   "ollama",
		Status:    SessionStateActive,
		CreatedAt: base,
		Labels:    map[string]string{"env": "prod"},
	}

	tests := []struct {
		name   string
		filter *SessionFilter
		want   bool
	}{
		{"nil filter matches", nil, true},
		{"empty filter matches", &SessionFilter{}, true},
		{"backend match", &SessionFilter{Backend: "ollama"}, true},
		{"backend mismatch", &SessionFilter{Backend: "codex"}, false},
		{"status mismatch", &SessionFilter{Status: SessionStateClosed}, false},
		{"created-after excludes older", &SessionFilter{CreatedAfter: base.Add(time.Hour)}, false},
		{"created-after includes", &SessionFilter{CreatedAfter: base.Add(-time.Hour)}, true},
		{"created-before excludes newer", &SessionFilter{CreatedBefore: base.Add(-time.Hour)}, false},
		{"created-before includes", &SessionFilter{CreatedBefore: base.Add(time.Hour)}, true},
		{"label match", &SessionFilter{Labels: map[string]string{"env": "prod"}}, true},
		{"label value mismatch", &SessionFilter{Labels: map[string]string{"env": "dev"}}, false},
		{"label key missing", &SessionFilter{Labels: map[string]string{"region": "us"}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesFilter(meta, tc.filter); got != tc.want {
				t.Fatalf("matchesFilter()=%v, want %v", got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// session_log_kv: PutMeta input validation
// -----------------------------------------------------------------------------

// TestKVSessionEventLog_PutMeta_Validation covers the nil / missing-field guard
// at the top of PutMeta.
func TestKVSessionEventLog_PutMeta_Validation(t *testing.T) {
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: newMemKVStore()})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	ctx := context.Background()

	cases := []struct {
		name string
		meta *SessionMeta
	}{
		{"nil meta", nil},
		{"missing session id", &SessionMeta{TenantID: "t"}},
		{"missing tenant id", &SessionMeta{SessionID: "s"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := log.PutMeta(ctx, tc.meta); err == nil {
				t.Fatal("expected validation error from PutMeta")
			}
		})
	}
}

// -----------------------------------------------------------------------------
// peer_messaging_store: Poll skip-branches for malformed inbox entries
// -----------------------------------------------------------------------------

// TestPeerMessaging_Poll_SkipsMalformedEntries covers Poll's three in-loop skip
// branches: an unparseable inbox key, a too-old (ts < since) entry, and a
// corrupt (non-JSON) value. Only the well-formed, in-window message survives.
func TestPeerMessaging_Poll_SkipsMalformedEntries(t *testing.T) {
	svc, store := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	// Well-formed message at ts=5000 (recipient agent-b).
	if err := svc.Send(ctx, testMessage("good", "a", "b", "advisory", 5000)); err != nil {
		t.Fatalf("send good: %v", err)
	}

	prefix := "inbox:" + testTenantID + ":b:"
	// Unparseable key: no numeric timestamp segment after the prefix.
	if err := store.Put(ctx, prefix+"not-a-timestamp", []byte(`{}`)); err != nil {
		t.Fatalf("put unparseable: %v", err)
	}
	// Corrupt value at a well-formed key (ts=6000) -> JSON unmarshal fails.
	if err := store.Put(ctx, prefix+"6000:corrupt", []byte("{not json")); err != nil {
		t.Fatalf("put corrupt: %v", err)
	}

	// Poll with since=1000: the well-formed message is returned; the malformed
	// and corrupt entries are skipped without error.
	msgs, err := svc.Poll(ctx, testTenantID, "b", 1000)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if len(msgs) != 1 || msgs[0].ID != "good" {
		t.Fatalf("expected only the well-formed message, got %+v", msgs)
	}

	// Poll with a since above the only good message excludes it (ts < since).
	msgs, err = svc.Poll(ctx, testTenantID, "b", 9000)
	if err != nil {
		t.Fatalf("poll high-since: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no messages with since=9000, got %d", len(msgs))
	}
}

// -----------------------------------------------------------------------------
// cost_tracker_store: collectAgentUsage skip-branches
// -----------------------------------------------------------------------------

// TestCostTracker_CollectAgentUsage_SkipsCorrupt covers collectAgentUsage's
// corrupt-entry skip branch: a well-formed 8-byte counter is summed while a
// short (corrupt) counter under the same job is skipped without failing.
func TestCostTracker_CollectAgentUsage_SkipsCorrupt(t *testing.T) {
	store := newPeerTestKVStore()
	tracker := NewCostTracker(store, &peerTestLogger{}).(*kvCostTracker)
	ctx := context.Background()

	prefix := costKeyPrefix + "t1" + jobSegment + "job1" + agentSegment
	// Valid agent counter.
	if err := store.Put(ctx, prefix+"agent-good", int64ToBytes(42)); err != nil {
		t.Fatalf("put good: %v", err)
	}
	// Corrupt counter (not 8 bytes) -> bytesToInt64 fails -> skipped.
	if err := store.Put(ctx, prefix+"agent-corrupt", []byte{0x01, 0x02}); err != nil {
		t.Fatalf("put corrupt: %v", err)
	}

	usage, err := tracker.collectAgentUsage(ctx, "t1", "job1")
	if err != nil {
		t.Fatalf("collectAgentUsage: %v", err)
	}
	if usage["agent-good"] != 42 {
		t.Fatalf("want agent-good=42, got %d", usage["agent-good"])
	}
	if _, ok := usage["agent-corrupt"]; ok {
		t.Fatal("corrupt entry must be skipped, not included")
	}
}

// -----------------------------------------------------------------------------
// workflow_loader: Register persistor failure rolls back in-memory state
// -----------------------------------------------------------------------------

// TestWorkflowLoader_Register_PersistorErrorRollsBack covers the branch where
// the persistor Save fails during Register: the error is surfaced and the
// in-memory entry is removed so the cache stays consistent with storage.
func TestWorkflowLoader_Register_PersistorErrorRollsBack(t *testing.T) {
	persistor := newMemWorkflowPersistor()
	persistor.saveErr = errors.New("storage unavailable")

	loader := NewWorkflowLoader()
	loader.SetPersistor(persistor)

	def := newValidWorkflowDef("wf")
	if err := loader.Register(def); err == nil {
		t.Fatal("expected Register to surface the persistor error")
	}

	// Rollback: the workflow must NOT be retrievable from the in-memory cache.
	if _, err := loader.Get("wf"); err == nil {
		t.Fatal("workflow should have been rolled back out of the cache")
	}

	// And a subsequent Register (with a healthy persistor) must succeed,
	// proving the name slot was freed.
	persistor.saveErr = nil
	if err := loader.Register(newValidWorkflowDef("wf")); err != nil {
		t.Fatalf("re-register after rollback: %v", err)
	}
}

// -----------------------------------------------------------------------------
// task_store: NewInMemoryTaskStoreWithKV default-limit branch
// -----------------------------------------------------------------------------

// TestNewInMemoryTaskStoreWithKV_DefaultLimit covers the limit<=0 branch where
// the package-level InlineLimit default is applied.
func TestNewInMemoryTaskStoreWithKV_DefaultLimit(t *testing.T) {
	ts := NewInMemoryTaskStoreWithKV(newMemKVStore(), 0)
	store, ok := ts.(*inMemoryTaskStore)
	if !ok {
		t.Fatalf("unexpected store type %T", ts)
	}
	if store.inlineLimit != InlineLimit {
		t.Fatalf("want default inlineLimit=%d, got %d", InlineLimit, store.inlineLimit)
	}
}

// -----------------------------------------------------------------------------
// project_store: Get validation + unmarshalEntry error branches
// -----------------------------------------------------------------------------

// TestProjectStore_Get_EmptyProjectID covers Get's empty-projectID guard.
func TestProjectStore_Get_EmptyProjectID(t *testing.T) {
	store := NewInMemoryProjectStore()
	_, err := store.Get(context.Background(), "tenant-1", "")
	var inv *ProjectInvalidError
	if !errors.As(err, &inv) || inv.Field != "id" {
		t.Fatalf("want ProjectInvalidError(id), got %v", err)
	}
}

// TestProjectStore_Get_CancelledContext covers Get's ctx.Err() guard.
func TestProjectStore_Get_CancelledContext(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Get(ctx, "tenant-1", "p1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// TestProjectStore_UnmarshalEntry_WrongType covers unmarshalEntry's branch where
// the stored sync.Map value is not a *projectEntry. We inject a bogus value
// directly into the concrete store then read it back through Get.
func TestProjectStore_UnmarshalEntry_WrongType(t *testing.T) {
	store := NewInMemoryProjectStore().(*inMemoryProjectStore)
	store.projects.Store(projectKey("tenant-1", "p1"), "not-a-project-entry")

	_, err := store.Get(context.Background(), "tenant-1", "p1")
	var se *ProjectStoreError
	if !errors.As(err, &se) {
		t.Fatalf("want *ProjectStoreError for wrong-typed entry, got %T: %v", err, err)
	}
}

// TestProjectStore_UnmarshalEntry_BadJSON covers unmarshalEntry's branch where
// the stored entry holds non-JSON bytes.
func TestProjectStore_UnmarshalEntry_BadJSON(t *testing.T) {
	store := NewInMemoryProjectStore().(*inMemoryProjectStore)
	store.projects.Store(projectKey("tenant-1", "p1"), &projectEntry{data: []byte("{not json")})

	_, err := store.Get(context.Background(), "tenant-1", "p1")
	var se *ProjectStoreError
	if !errors.As(err, &se) {
		t.Fatalf("want *ProjectStoreError for corrupt entry, got %T: %v", err, err)
	}
}

// -----------------------------------------------------------------------------
// session_log_kv: ListSessions skip / filter / limit branches
// -----------------------------------------------------------------------------

// TestKVSessionEventLog_ListSessions_Branches exercises ListSessions' in-scan
// branches: non-meta keys are skipped, corrupt meta values are skipped, the
// filter excludes non-matching metas, and the limit truncates the result set.
func TestKVSessionEventLog_ListSessions_Branches(t *testing.T) {
	kv := newMemKVStore()
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: kv})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	ctx := context.Background()

	// Three well-formed sessions, two on the "ollama" backend.
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	mk := func(id, backend string, created time.Time) *SessionMeta {
		return &SessionMeta{SessionID: id, TenantID: "t", Backend: backend, Status: SessionStateActive, CreatedAt: created}
	}
	if err := log.PutMeta(ctx, mk("s1", "ollama", base)); err != nil {
		t.Fatalf("put s1: %v", err)
	}
	if err := log.PutMeta(ctx, mk("s2", "ollama", base.Add(time.Hour))); err != nil {
		t.Fatalf("put s2: %v", err)
	}
	if err := log.PutMeta(ctx, mk("s3", "codex", base.Add(2*time.Hour))); err != nil {
		t.Fatalf("put s3: %v", err)
	}

	// A non-meta key under the same tenant prefix (an event) must be skipped.
	if err := kv.Put(ctx, log.eventKey("t", "s1", 1), []byte(`{"seq":1}`)); err != nil {
		t.Fatalf("put event key: %v", err)
	}
	// A corrupt meta value must be skipped without aborting the scan.
	if err := kv.Put(ctx, log.metaKey("t", "s4-corrupt"), []byte("{not json")); err != nil {
		t.Fatalf("put corrupt meta: %v", err)
	}

	// Filter to ollama only; limit to 1 -> newest ollama session (s2) wins.
	metas, err := log.ListSessions(ctx, "t", &SessionFilter{Backend: "ollama"}, 1)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("limit=1 should truncate to one result, got %d", len(metas))
	}
	if metas[0].SessionID != "s2" {
		t.Fatalf("newest ollama session should be s2, got %s", metas[0].SessionID)
	}

	// Without a limit, exactly the two ollama sessions are returned (codex s3,
	// the event key, and the corrupt meta are all excluded).
	all, err := log.ListSessions(ctx, "t", &SessionFilter{Backend: "ollama"}, 0)
	if err != nil {
		t.Fatalf("ListSessions all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 ollama sessions, got %d", len(all))
	}
}

// -----------------------------------------------------------------------------
// Fault-injecting KVStore: surfaces store-layer error branches that the plain
// in-memory fakes never reach. Each op can be forced to fail independently.
// -----------------------------------------------------------------------------

type faultKVStore struct {
	memKVStore
	putErr    error
	getErr    error
	delErr    error
	scanErr   error
	listErr   error
	existsErr error
}

func newFaultKVStore() *faultKVStore {
	s := &faultKVStore{}
	s.m = map[string][]byte{}
	return s
}

func (s *faultKVStore) Put(ctx context.Context, key string, value []byte) error {
	if s.putErr != nil {
		return s.putErr
	}
	return s.memKVStore.Put(ctx, key, value)
}

func (s *faultKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.memKVStore.Get(ctx, key)
}

func (s *faultKVStore) Delete(ctx context.Context, key string) error {
	if s.delErr != nil {
		return s.delErr
	}
	return s.memKVStore.Delete(ctx, key)
}

func (s *faultKVStore) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	if s.scanErr != nil {
		return s.scanErr
	}
	return s.memKVStore.Scan(ctx, prefix, fn)
}

func (s *faultKVStore) List(ctx context.Context, prefix string) ([]string, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.memKVStore.List(ctx, prefix)
}

func (s *faultKVStore) Exists(ctx context.Context, key string) (bool, error) {
	if s.existsErr != nil {
		return false, s.existsErr
	}
	return s.memKVStore.Exists(ctx, key)
}

// TestPeerMessaging_InboxSize_ListError covers InboxSize's List-error branch.
func TestPeerMessaging_InboxSize_ListError(t *testing.T) {
	kv := newFaultKVStore()
	kv.listErr = errors.New("kv down")
	svc := NewPeerMessagingService(kv, defaultPolicy(), &peerTestLogger{})

	_, err := svc.InboxSize(context.Background(), "t", "agent-a")
	var ie *PeerInboxError
	if !errors.As(err, &ie) || ie.Op != "inbox_size" {
		t.Fatalf("want PeerInboxError(inbox_size), got %v", err)
	}
}

// TestPeerMessaging_Poll_ScanError covers Poll's Scan-error branch.
func TestPeerMessaging_Poll_ScanError(t *testing.T) {
	kv := newFaultKVStore()
	kv.scanErr = errors.New("scan failed")
	svc := NewPeerMessagingService(kv, defaultPolicy(), &peerTestLogger{})

	_, err := svc.Poll(context.Background(), "t", "agent-a", 0)
	var ie *PeerInboxError
	if !errors.As(err, &ie) || ie.Op != "poll_scan" {
		t.Fatalf("want PeerInboxError(poll_scan), got %v", err)
	}
}

// TestPeerMessaging_Send_DedupCheckError covers Send's Exists-error branch.
func TestPeerMessaging_Send_DedupCheckError(t *testing.T) {
	kv := newFaultKVStore()
	kv.existsErr = errors.New("exists failed")
	svc := NewPeerMessagingService(kv, defaultPolicy(), &peerTestLogger{})

	err := svc.Send(context.Background(), testMessage("m1", "a", "b", "advisory", 1000))
	var ie *PeerInboxError
	if !errors.As(err, &ie) || ie.Op != "dedup_check" {
		t.Fatalf("want PeerInboxError(dedup_check), got %v", err)
	}
}

// TestPeerMessaging_Send_PutMessageError covers Send's put_message branch where
// the inbox write fails (Exists/InboxSize succeed first).
func TestPeerMessaging_Send_PutMessageError(t *testing.T) {
	kv := newFaultKVStore()
	kv.putErr = errors.New("put failed")
	svc := NewPeerMessagingService(kv, defaultPolicy(), &peerTestLogger{})

	err := svc.Send(context.Background(), testMessage("m1", "a", "b", "advisory", 1000))
	var ie *PeerInboxError
	if !errors.As(err, &ie) || ie.Op != "put_message" {
		t.Fatalf("want PeerInboxError(put_message), got %v", err)
	}
}

// -----------------------------------------------------------------------------
// session_log_kv: Append validation + Put-error branches
// -----------------------------------------------------------------------------

// TestKVSessionEventLog_Append_NilEvent covers Append's nil-event guard.
func TestKVSessionEventLog_Append_NilEvent(t *testing.T) {
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: newMemKVStore()})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	var ae *SessionLogAppendError
	if err := log.Append(context.Background(), nil); !errors.As(err, &ae) {
		t.Fatalf("want *SessionLogAppendError for nil event, got %v", err)
	}
}

// TestKVSessionEventLog_Append_MissingFields covers Append's required-field guard.
func TestKVSessionEventLog_Append_MissingFields(t *testing.T) {
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: newMemKVStore()})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	var ae *SessionLogAppendError
	if err := log.Append(context.Background(), newPromptEvent("", "s", 1, "x")); !errors.As(err, &ae) {
		t.Fatalf("want *SessionLogAppendError for missing tenant, got %v", err)
	}
}

// TestKVSessionEventLog_Append_PutError covers Append's kv.Put failure branch:
// sequence validation passes (empty session -> latest=0, seq=1) but the event
// write fails, surfacing a SessionLogAppendError that wraps the store error.
func TestKVSessionEventLog_Append_PutError(t *testing.T) {
	kv := newFaultKVStore()
	kv.putErr = errors.New("kv put down")
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: kv})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	var ae *SessionLogAppendError
	if err := log.Append(context.Background(), newPromptEvent("t", "s", 1, "x")); !errors.As(err, &ae) {
		t.Fatalf("want *SessionLogAppendError wrapping put error, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// cost_tracker_store: GetJobUsage readCounter generic-error branch
// -----------------------------------------------------------------------------

// TestCostTracker_GetJobUsage_ReadCounterError covers the branch where reading
// the job total fails with a non-not-found error, wrapped as UsageRetrievalError.
func TestCostTracker_GetJobUsage_ReadCounterError(t *testing.T) {
	kv := newFaultKVStore()
	kv.getErr = errors.New("kv get down") // not a not-found error
	tracker := NewCostTracker(kv, &peerTestLogger{})

	_, err := tracker.GetJobUsage(context.Background(), "t1", "job1")
	var ure *UsageRetrievalError
	if !errors.As(err, &ure) {
		t.Fatalf("want *UsageRetrievalError, got %T: %v", err, err)
	}
}

// TestCostTracker_CollectAgentUsage_SkipsEmptyAgentID covers the empty-agent-ID
// skip branch: a key equal to the prefix (no agent segment) is ignored.
func TestCostTracker_CollectAgentUsage_SkipsEmptyAgentID(t *testing.T) {
	store := newPeerTestKVStore()
	tracker := NewCostTracker(store, &peerTestLogger{}).(*kvCostTracker)
	ctx := context.Background()

	prefix := costKeyPrefix + "t1" + jobSegment + "job1" + agentSegment
	// Key exactly equal to the prefix -> agentID == "" -> skipped.
	if err := store.Put(ctx, prefix, int64ToBytes(7)); err != nil {
		t.Fatalf("put empty-agent key: %v", err)
	}
	if err := store.Put(ctx, prefix+"real", int64ToBytes(9)); err != nil {
		t.Fatalf("put real agent: %v", err)
	}

	usage, err := tracker.collectAgentUsage(ctx, "t1", "job1")
	if err != nil {
		t.Fatalf("collectAgentUsage: %v", err)
	}
	if len(usage) != 1 || usage["real"] != 9 {
		t.Fatalf("expected only {real:9}, got %+v", usage)
	}
}

// -----------------------------------------------------------------------------
// file_lock_store: Raft GetShardRaftState error branches
// -----------------------------------------------------------------------------

// errRaftNode is a RaftNode whose GetShardRaftState always fails, exercising
// the "cannot determine fencing term" branches in Acquire and
// validateFencingToken. All other RaftNode methods are inherited from the
// embedded nil interface and must not be called by these paths.
type errRaftNode struct {
	interfaces.RaftNode
}

func (errRaftNode) GetShardRaftState(uint64) (uint64, uint64, error) {
	return 0, 0, errors.New("raft state unavailable")
}

// TestFileLock_Acquire_RaftStateError covers Acquire's branch where the Raft
// term cannot be read: the lock is still granted with a zero fencing token.
func TestFileLock_Acquire_RaftStateError(t *testing.T) {
	svc, _, _ := newTestFileLockSvc(errRaftNode{})
	lock, err := svc.Acquire(context.Background(), "t1", "p1", "/file.go", "agent-a")
	if err != nil {
		t.Fatalf("acquire should succeed despite raft error: %v", err)
	}
	if lock.FencingToken != 0 {
		t.Fatalf("want zero fencing token when raft state is unreadable, got %d", lock.FencingToken)
	}
}

// TestFileLock_ValidateFencingToken_RaftError covers validateFencingToken's
// branch where GetShardRaftState fails: validation is skipped (returns nil) so
// the operation is allowed to proceed.
func TestFileLock_ValidateFencingToken_RaftError(t *testing.T) {
	svc, _, _ := newTestFileLockSvc(errRaftNode{})
	store := svc.(*fileLockStore)

	// A lock with a non-zero fencing token forces the raft lookup.
	if err := store.validateFencingToken(&FileLock{LockKey: "k", FencingToken: 7}); err != nil {
		t.Fatalf("validateFencingToken should allow the op when raft state errors, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// session_log_kv: GetMeta / LatestSequence / Range store-error branches
// -----------------------------------------------------------------------------

// TestKVSessionEventLog_GetMeta_GenericError covers GetMeta's non-not-found
// store error branch (returned verbatim, not translated to the sentinel).
func TestKVSessionEventLog_GetMeta_GenericError(t *testing.T) {
	kv := newFaultKVStore()
	kv.getErr = errors.New("kv get down") // not a not-found error
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: kv})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	_, gErr := log.GetMeta(context.Background(), "t", "s")
	if gErr == nil || errors.Is(gErr, ErrSessionLogNotFound) {
		t.Fatalf("want raw store error, got %v", gErr)
	}
}

// TestKVSessionEventLog_GetMeta_CorruptValue covers GetMeta's unmarshal-error
// branch when the stored meta value is not valid JSON.
func TestKVSessionEventLog_GetMeta_CorruptValue(t *testing.T) {
	kv := newMemKVStore()
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: kv})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	ctx := context.Background()
	if err := kv.Put(ctx, log.metaKey("t", "s"), []byte("{not json")); err != nil {
		t.Fatalf("seed corrupt meta: %v", err)
	}
	if _, gErr := log.GetMeta(ctx, "t", "s"); gErr == nil {
		t.Fatal("expected unmarshal error from corrupt meta")
	}
}

// TestKVSessionEventLog_LatestSequence_GetMetaError covers LatestSequence's
// branch where GetMeta returns a non-not-found error (propagated, not swallowed).
func TestKVSessionEventLog_LatestSequence_GetMetaError(t *testing.T) {
	kv := newFaultKVStore()
	kv.getErr = errors.New("kv get down")
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: kv})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	if _, lErr := log.LatestSequence(context.Background(), "t", "s"); lErr == nil {
		t.Fatal("expected LatestSequence to propagate the store error")
	}
}

// TestKVSessionEventLog_LatestSequence_ScanError covers LatestSequence's
// events-scan error branch: GetMeta reports not-found (so it falls through to
// the scan), and the scan itself fails.
func TestKVSessionEventLog_LatestSequence_ScanError(t *testing.T) {
	kv := newFaultKVStore()
	// Get returns (nil,nil) -> GetMeta treats it as not-found and falls through.
	kv.scanErr = errors.New("scan down")
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: kv})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	if _, lErr := log.LatestSequence(context.Background(), "t", "s"); lErr == nil {
		t.Fatal("expected LatestSequence to surface the scan error")
	}
}

// TestKVSessionEventLog_Range_ScanError covers Range's scan-error branch.
func TestKVSessionEventLog_Range_ScanError(t *testing.T) {
	kv := newFaultKVStore()
	kv.scanErr = errors.New("scan down")
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: kv})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	if _, _, rErr := log.Range(context.Background(), "t", "s", 1, 10); rErr == nil {
		t.Fatal("expected Range to surface the scan error")
	}
}

// -----------------------------------------------------------------------------
// agent_registry: GetAgent / ListAgents corrupt-entry branches
// -----------------------------------------------------------------------------

// TestAgentRegistry_GetAgent_WrongType covers GetAgent's branch where the
// stored value is not an *agentEntry.
func TestAgentRegistry_GetAgent_WrongType(t *testing.T) {
	reg := NewInMemoryAgentRegistry().(*inMemoryAgentRegistry)
	reg.agents.Store(agentKey("t", "a"), "not-an-entry")

	_, err := reg.GetAgent(context.Background(), "t", "a")
	var re *AgentRegistryError
	if !errors.As(err, &re) || !errors.Is(re, ErrAgentRegistryMarshal) {
		t.Fatalf("want AgentRegistryError(marshal), got %v", err)
	}
}

// TestAgentRegistry_GetAgent_CorruptJSON covers GetAgent's unmarshal-error branch.
func TestAgentRegistry_GetAgent_CorruptJSON(t *testing.T) {
	reg := NewInMemoryAgentRegistry().(*inMemoryAgentRegistry)
	reg.agents.Store(agentKey("t", "a"), &agentEntry{data: []byte("{not json")})

	_, err := reg.GetAgent(context.Background(), "t", "a")
	var re *AgentRegistryError
	if !errors.As(err, &re) || !errors.Is(re, ErrAgentRegistryMarshal) {
		t.Fatalf("want AgentRegistryError(marshal), got %v", err)
	}
}

// TestAgentRegistry_ListAgents_CorruptJSON covers ListAgents' unmarshal-error
// branch (a corrupt entry aborts the range with a typed error).
func TestAgentRegistry_ListAgents_CorruptJSON(t *testing.T) {
	reg := NewInMemoryAgentRegistry().(*inMemoryAgentRegistry)
	reg.agents.Store(agentKey("t", "a"), &agentEntry{data: []byte("{not json")})

	_, err := reg.ListAgents(context.Background(), "t", nil)
	var re *AgentRegistryError
	if !errors.As(err, &re) || !errors.Is(re, ErrAgentRegistryMarshal) {
		t.Fatalf("want AgentRegistryError(marshal), got %v", err)
	}
}

// -----------------------------------------------------------------------------
// session_store: GetSession corrupt-entry branches
// -----------------------------------------------------------------------------

// TestAgentStore_GetSession_WrongType covers GetSession's branch where the
// stored value is not a []byte.
func TestAgentStore_GetSession_WrongType(t *testing.T) {
	store := NewInMemoryAgentStore().(*inMemoryAgentStore)
	store.sessions.Store("sess-1", 12345) // wrong type

	_, err := store.GetSession(context.Background(), "sess-1")
	var se *AgentStoreError
	if !errors.As(err, &se) || !errors.Is(se, ErrAgentStoreMarshal) {
		t.Fatalf("want AgentStoreError(marshal), got %v", err)
	}
}

// TestAgentStore_GetSession_CorruptJSON covers GetSession's unmarshal-error branch.
func TestAgentStore_GetSession_CorruptJSON(t *testing.T) {
	store := NewInMemoryAgentStore().(*inMemoryAgentStore)
	store.sessions.Store("sess-1", []byte("{not json"))

	_, err := store.GetSession(context.Background(), "sess-1")
	var se *AgentStoreError
	if !errors.As(err, &se) || !errors.Is(se, ErrAgentStoreMarshal) {
		t.Fatalf("want AgentStoreError(marshal), got %v", err)
	}
}

// -----------------------------------------------------------------------------
// task_store: ListTasks corrupt-entry branch
// -----------------------------------------------------------------------------

// TestTaskStore_ListTasks_CorruptJSON covers ListTasks' unmarshal-error branch.
func TestTaskStore_ListTasks_CorruptJSON(t *testing.T) {
	ts := NewInMemoryTaskStoreWithKV(newMemKVStore(), 0).(*inMemoryTaskStore)
	ts.tasks.Store(taskKey("t", "task-1"), &taskEntry{data: []byte("{not json")})

	_, err := ts.ListTasks(context.Background(), "t", nil)
	var tse *TaskStoreError
	if !errors.As(err, &tse) || !errors.Is(tse, ErrTaskStoreMarshal) {
		t.Fatalf("want TaskStoreError(marshal), got %v", err)
	}
}

// -----------------------------------------------------------------------------
// file_lock_store: loadLock / Delete store-error branches via fault KV
// -----------------------------------------------------------------------------

func newFaultFileLockSvc(t *testing.T, kv *faultKVStore) *fileLockStore {
	t.Helper()
	lm := newTestFileLockLeaseManager()
	settings := FileLockSettings{LeaseTTL: "5m", HeartbeatInterval: "30s", MaxRenewals: 60}
	svc := NewFileLockService(kv, lm, nil, 1, settings, IsolationShared, &testFileLockLogger{})
	return svc.(*fileLockStore)
}

// TestFileLock_Release_LoadLockError covers Release's branch where loadLock
// fails with a non-not-found store error.
func TestFileLock_Release_LoadLockError(t *testing.T) {
	kv := newFaultKVStore()
	kv.getErr = errors.New("kv get down")
	svc := newFaultFileLockSvc(t, kv)

	if err := svc.Release(context.Background(), "t", "lock-key"); err == nil {
		t.Fatal("expected Release to surface the loadLock error")
	}
}

// TestFileLock_Renew_LoadLockError covers Renew's loadLock-error branch.
func TestFileLock_Renew_LoadLockError(t *testing.T) {
	kv := newFaultKVStore()
	kv.getErr = errors.New("kv get down")
	svc := newFaultFileLockSvc(t, kv)

	if err := svc.Renew(context.Background(), "t", "lock-key"); err == nil {
		t.Fatal("expected Renew to surface the loadLock error")
	}
}

// TestFileLock_WaitForRelease_LoadLockError covers WaitForRelease's branch where
// the initial loadLock fails with a store error.
func TestFileLock_WaitForRelease_LoadLockError(t *testing.T) {
	kv := newFaultKVStore()
	kv.getErr = errors.New("kv get down")
	svc := newFaultFileLockSvc(t, kv)

	if err := svc.WaitForRelease(context.Background(), "t", "lock-key"); err == nil {
		t.Fatal("expected WaitForRelease to surface the loadLock error")
	}
}

// TestFileLock_ListLocks_SkipsCorrupt covers ListLocks' unmarshal-skip branch:
// a corrupt lock value is skipped without aborting the scan, yielding an empty
// (but non-nil) result set.
func TestFileLock_ListLocks_SkipsCorrupt(t *testing.T) {
	kv := newFaultKVStore()
	svc := newFaultFileLockSvc(t, kv)
	ctx := context.Background()

	key := fileLockPrefix + "t:p:corrupt"
	if err := kv.memKVStore.Put(ctx, key, []byte("{not json")); err != nil {
		t.Fatalf("seed corrupt lock: %v", err)
	}

	locks, err := svc.ListLocks(ctx, "t", "p")
	if err != nil {
		t.Fatalf("ListLocks: %v", err)
	}
	if len(locks) != 0 {
		t.Fatalf("corrupt lock should be skipped, got %d locks", len(locks))
	}
}
