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
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// -----------------------------------------------------------------------------
// project_store: context cancellation and input-validation branches
// -----------------------------------------------------------------------------

// TestProjectStore_BadInput covers each method's cancelled-context guard and
// its empty-ID and empty-tenant validation, one row per method and bad input.
func TestProjectStore_BadInput(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	bg := context.Background()

	tests := []struct {
		name      string
		call      func(ProjectStore) error
		wantField string // empty means the error must be context.Canceled
	}{
		{"Get/cancelled", func(s ProjectStore) error {
			_, err := s.Get(cancelled, "tenant-1", "p1")
			return err
		}, ""},
		{"Get/empty id", func(s ProjectStore) error {
			_, err := s.Get(bg, "tenant-1", "")
			return err
		}, "id"},
		{"GetByName/cancelled", func(s ProjectStore) error {
			_, err := s.GetByName(cancelled, "tenant-1", "name")
			return err
		}, ""},
		{"GetByName/empty tenant", func(s ProjectStore) error {
			_, err := s.GetByName(bg, "", "name")
			return err
		}, "tenant_id"},
		{"Update/cancelled", func(s ProjectStore) error {
			return s.Update(cancelled, newTestProject("p1", "tenant-1", "n"))
		}, ""},
		{"Update/empty id", func(s ProjectStore) error {
			return s.Update(bg, &Project{TenantID: "tenant-1"})
		}, "id"},
		{"Update/empty tenant", func(s ProjectStore) error {
			return s.Update(bg, &Project{ID: "p1"})
		}, "tenant_id"},
		{"ListByTenant/cancelled", func(s ProjectStore) error {
			_, err := s.ListByTenant(cancelled, "tenant-1")
			return err
		}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call(NewInMemoryProjectStore())
			if tc.wantField == "" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("want context.Canceled, got %v", err)
				}
				return
			}
			var inv *ProjectInvalidError
			if !errors.As(err, &inv) || inv.Field != tc.wantField {
				t.Fatalf("want ProjectInvalidError(%s), got %v", tc.wantField, err)
			}
		})
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
// skip branches: a well-formed 8-byte counter is summed, while a short
// (corrupt) counter and a key with no agent segment under the same job are
// skipped without failing.
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
	// Key exactly equal to the prefix -> agentID == "" -> skipped.
	if err := store.Put(ctx, prefix, int64ToBytes(7)); err != nil {
		t.Fatalf("put empty-agent key: %v", err)
	}

	usage, err := tracker.collectAgentUsage(ctx, "t1", "job1")
	if err != nil {
		t.Fatalf("collectAgentUsage: %v", err)
	}
	if len(usage) != 1 || usage["agent-good"] != 42 {
		t.Fatalf("expected only {agent-good:42}, got %+v", usage)
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
	if err := loader.Register(SystemTenantID, def); err == nil {
		t.Fatal("expected Register to surface the persistor error")
	}

	// Rollback: the workflow must NOT be retrievable from the in-memory cache.
	if _, err := loader.Get(SystemTenantID, "wf"); err == nil {
		t.Fatal("workflow should have been rolled back out of the cache")
	}

	// And a subsequent Register (with a healthy persistor) must succeed,
	// proving the name slot was freed.
	persistor.saveErr = nil
	if err := loader.Register(SystemTenantID, newValidWorkflowDef("wf")); err != nil {
		t.Fatalf("re-register after rollback: %v", err)
	}
}

// -----------------------------------------------------------------------------
// task_store: NewInMemoryTaskStoreWithKV default-limit branch
// -----------------------------------------------------------------------------

// TestNewInMemoryTaskStoreWithKV_DefaultLimit covers the limit<=0 branch where
// the package-level InlineLimit default is applied. The default is observable
// through the artifact path alone: an artifact at the limit stays embedded in
// the task record, and one byte more is offloaded to its own KV key.
func TestNewInMemoryTaskStoreWithKV_DefaultLimit(t *testing.T) {
	const (
		tenantID = "tenant-1"
		taskID   = "task-inline-limit"
	)

	kv := NewMemoryTaskKV()
	store := NewInMemoryTaskStoreWithKV(kv, 0)
	ctx := context.Background()
	newTaskWithID(t, store, tenantID, taskID)

	err := store.SubmitTaskArtifacts(ctx, tenantID, taskID, []Artifact{
		{Kind: ArtifactKindInline, Name: "at-limit", Content: make([]byte, InlineLimit)},
		{Kind: ArtifactKindInline, Name: "over-limit", Content: make([]byte, InlineLimit+1)},
	})
	if err != nil {
		t.Fatalf("SubmitTaskArtifacts: %v", err)
	}

	stored, err := store.ListTaskArtifacts(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("ListTaskArtifacts: %v", err)
	}
	byName := make(map[string]Artifact, len(stored))
	for _, artifact := range stored {
		byName[artifact.Name] = artifact
	}
	if len(byName) != 2 {
		t.Fatalf("want 2 stored artifacts, got %d: %+v", len(byName), stored)
	}

	// At the limit: still inline, content intact inside the task record.
	atLimit := byName["at-limit"]
	if atLimit.Kind != ArtifactKindInline {
		t.Fatalf("artifact at the default limit must stay inline, got kind %q", atLimit.Kind)
	}
	if int64(len(atLimit.Content)) != InlineLimit {
		t.Fatalf("inline content = %d bytes, want %d", len(atLimit.Content), InlineLimit)
	}
	atLimitKey, err := taskArtifactKey(tenantID, taskID, "at-limit")
	if err != nil {
		t.Fatalf("taskArtifactKey(at-limit): %v", err)
	}
	offloaded, err := kv.Exists(ctx, atLimitKey)
	if err != nil {
		t.Fatalf("kv.Exists(at-limit): %v", err)
	}
	if offloaded {
		t.Fatal("artifact at the default limit must not be written to its own key")
	}

	// One byte over: offloaded to its own key, referenced from the record.
	overLimit := byName["over-limit"]
	if overLimit.Kind != ArtifactKindKVKey {
		t.Fatalf("artifact over the default limit must be offloaded, got kind %q", overLimit.Kind)
	}
	overLimitKey, err := taskArtifactKey(tenantID, taskID, "over-limit")
	if err != nil {
		t.Fatalf("taskArtifactKey(over-limit): %v", err)
	}
	if overLimit.Ref.Path != overLimitKey {
		t.Fatalf("ref path = %q, want %q", overLimit.Ref.Path, overLimitKey)
	}
	payload, err := kv.Get(ctx, overLimitKey)
	if err != nil {
		t.Fatalf("kv.Get(over-limit): %v", err)
	}
	if int64(len(payload)) != InlineLimit+1 {
		t.Fatalf("offloaded payload = %d bytes, want %d", len(payload), InlineLimit+1)
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

	// putIfAbsentErr and compareAndDeleteErr fail the guarded verbs
	// independently of the plain ones, so a test can fault the claim without
	// faulting the delivery that follows it.
	putIfAbsentErr      error
	compareAndDeleteErr error

	// putIfAbsentErrPrefix narrows putIfAbsentErr to one keyspace. Peer
	// delivery takes a guarded write twice — the claim, then the delivered
	// marker — and the two failures mean opposite things, so a test has to be
	// able to fail exactly one of them. Empty fails every key.
	putIfAbsentErrPrefix string
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

// PutIfAbsent and CompareAndDelete give this fixture the guarded-write
// capability peer delivery requires. They operate on the embedded store's map
// directly rather than through Put/Delete, so a test faulting the plain verbs
// still gets a working claim — which is the whole point of faulting one step of
// a sequence at a time.
func (s *faultKVStore) PutIfAbsent(_ context.Context, key string, value []byte) (bool, error) {
	if s.putIfAbsentErr != nil && strings.HasPrefix(key, s.putIfAbsentErrPrefix) {
		return false, s.putIfAbsentErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.m[key]; exists {
		return false, nil
	}
	c := make([]byte, len(value))
	copy(c, value)
	s.m[key] = c
	return true, nil
}

func (s *faultKVStore) CompareAndDelete(_ context.Context, key string, expected []byte) (bool, error) {
	if s.compareAndDeleteErr != nil {
		return false, s.compareAndDeleteErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	held, exists := s.m[key]
	if !exists || !bytes.Equal(held, expected) {
		return false, nil
	}
	delete(s.m, key)
	return true, nil
}

func (s *faultKVStore) Exists(ctx context.Context, key string) (bool, error) {
	if s.existsErr != nil {
		return false, s.existsErr
	}
	return s.memKVStore.Exists(ctx, key)
}

// TestPeerMessaging_StoreErrors covers each store failure the service reports
// as a PeerInboxError, one row per method and failing store verb.
func TestPeerMessaging_StoreErrors(t *testing.T) {
	kvDown := errors.New("kv down")
	ctx := context.Background()

	tests := []struct {
		name   string
		fault  func(*faultKVStore)
		call   func(PeerMessagingService) error
		wantOp string
	}{
		{"InboxSize/list error",
			func(kv *faultKVStore) { kv.listErr = kvDown },
			func(svc PeerMessagingService) error {
				_, err := svc.InboxSize(ctx, "t", "agent-a")
				return err
			}, "inbox_size"},
		{"Poll/scan error",
			func(kv *faultKVStore) { kv.scanErr = kvDown },
			func(svc PeerMessagingService) error {
				_, err := svc.Poll(ctx, "t", "agent-a", 0)
				return err
			}, "poll_scan"},
		{"Send/dedup check error",
			func(kv *faultKVStore) { kv.existsErr = kvDown },
			func(svc PeerMessagingService) error {
				return svc.Send(ctx, testMessage("m1", "a", "b", "advisory", 1000))
			}, "dedup_check"},
		{"Send/put message error",
			// Exists and InboxSize succeed first; the inbox write fails.
			func(kv *faultKVStore) { kv.putErr = kvDown },
			func(svc PeerMessagingService) error {
				return svc.Send(ctx, testMessage("m1", "a", "b", "advisory", 1000))
			}, "put_message"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kv := newFaultKVStore()
			tc.fault(kv)
			svc := NewPeerMessagingService(kv, defaultPolicy(), &peerTestLogger{})

			var ie *PeerInboxError
			if err := tc.call(svc); !errors.As(err, &ie) || ie.Op != tc.wantOp {
				t.Fatalf("want PeerInboxError(%s), got %v", tc.wantOp, err)
			}
		})
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
// session_log_kv: input validation and store-error branches
// -----------------------------------------------------------------------------

// TestKVSessionEventLog_Errors covers each method's input validation and its
// store-failure branches, one row per method and failure.
func TestKVSessionEventLog_Errors(t *testing.T) {
	kvDown := errors.New("kv down")
	ctx := context.Background()

	tests := []struct {
		name string
		// fault configures the store before the call; nil leaves it healthy.
		fault func(*faultKVStore)
		// seedCorruptMeta stores a non-JSON meta value for session t/s.
		seedCorruptMeta bool
		call            func(*KVSessionEventLog) error
		// wantAppendErr requires a *SessionLogAppendError.
		wantAppendErr bool
		// wantNotSentinel requires the error not to read as not-found.
		wantNotSentinel bool
	}{
		{name: "Append/nil event",
			call:          func(l *KVSessionEventLog) error { return l.Append(ctx, nil) },
			wantAppendErr: true},
		{name: "Append/missing tenant",
			call:          func(l *KVSessionEventLog) error { return l.Append(ctx, newPromptEvent("", "s", 1, "x")) },
			wantAppendErr: true},
		{name: "Append/put error",
			fault:         func(kv *faultKVStore) { kv.putErr = kvDown },
			call:          func(l *KVSessionEventLog) error { return l.Append(ctx, newPromptEvent("t", "s", 1, "x")) },
			wantAppendErr: true},
		{name: "PutMeta/nil meta",
			call: func(l *KVSessionEventLog) error { return l.PutMeta(ctx, nil) }},
		{name: "PutMeta/missing session id",
			call: func(l *KVSessionEventLog) error { return l.PutMeta(ctx, &SessionMeta{TenantID: "t"}) }},
		{name: "PutMeta/missing tenant id",
			call: func(l *KVSessionEventLog) error { return l.PutMeta(ctx, &SessionMeta{SessionID: "s"}) }},
		{name: "GetMeta/store error",
			fault: func(kv *faultKVStore) { kv.getErr = kvDown },
			call: func(l *KVSessionEventLog) error {
				_, err := l.GetMeta(ctx, "t", "s")
				return err
			},
			wantNotSentinel: true},
		{name: "GetMeta/corrupt value",
			seedCorruptMeta: true,
			call: func(l *KVSessionEventLog) error {
				_, err := l.GetMeta(ctx, "t", "s")
				return err
			}},
		{name: "LatestSequence/meta read error",
			fault: func(kv *faultKVStore) { kv.getErr = kvDown },
			call: func(l *KVSessionEventLog) error {
				_, err := l.LatestSequence(ctx, "t", "s")
				return err
			}},
		{name: "LatestSequence/scan error",
			// No meta is stored, so LatestSequence falls through to the scan.
			fault: func(kv *faultKVStore) { kv.scanErr = kvDown },
			call: func(l *KVSessionEventLog) error {
				_, err := l.LatestSequence(ctx, "t", "s")
				return err
			}},
		{name: "Range/scan error",
			fault: func(kv *faultKVStore) { kv.scanErr = kvDown },
			call: func(l *KVSessionEventLog) error {
				_, _, err := l.Range(ctx, "t", "s", 1, 10)
				return err
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kv := newFaultKVStore()
			log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: kv})
			if err != nil {
				t.Fatalf("NewKVSessionEventLog: %v", err)
			}
			if tc.seedCorruptMeta {
				if err := kv.Put(ctx, log.metaKey("t", "s"), []byte("{not json")); err != nil {
					t.Fatalf("seed corrupt meta: %v", err)
				}
			}
			if tc.fault != nil {
				tc.fault(kv)
			}

			err = tc.call(log)
			if err == nil {
				t.Fatal("expected an error")
			}
			var ae *SessionLogAppendError
			if tc.wantAppendErr && !errors.As(err, &ae) {
				t.Fatalf("want *SessionLogAppendError, got %v", err)
			}
			if tc.wantNotSentinel && errors.Is(err, ErrSessionLogNotFound) {
				t.Fatalf("want the raw store error, got the not-found sentinel: %v", err)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// agent_registry: corrupt-record branches on the mutating read paths
// -----------------------------------------------------------------------------

// seedCorruptAgent writes bytes that are not a valid agent record at the key
// the registry itself derives for (tenantID, agentID), so the corruption is
// observed exactly where the registry looks for the record.
func seedCorruptAgent(t *testing.T, kv interfaces.KVStore, tenantID, agentID string) {
	t.Helper()
	key, err := fleetNodeKey(tenantID, agentID)
	if err != nil {
		t.Fatalf("fleetNodeKey(%s, %s): %v", tenantID, agentID, err)
	}
	if err := kv.Put(context.Background(), key, []byte("{not json")); err != nil {
		t.Fatalf("seed corrupt agent record: %v", err)
	}
}

// TestAgentRegistry_CorruptRecord covers the decode-failure branch on the
// registry's mutating paths. Each one loads the current record before writing
// its successor, so a record the registry cannot decode must abort the mutation
// under the marshal sentinel rather than be silently overwritten. (GetFleetNode's
// read path and the ListFleetNodes skip branch are covered by agent_registry_test.go.)
func TestAgentRegistry_CorruptRecord(t *testing.T) {
	const (
		tenantID = "t"
		agentID  = "a"
	)

	cases := []struct {
		name   string
		invoke func(context.Context, FleetRegistry) error
	}{
		{
			name: "UpdateFleetNode",
			invoke: func(ctx context.Context, reg FleetRegistry) error {
				return reg.UpdateFleetNode(ctx, &FleetNode{ID: agentID, TenantID: tenantID})
			},
		},
		{
			name: "RemoveFleetNode",
			invoke: func(ctx context.Context, reg FleetRegistry) error {
				return reg.RemoveFleetNode(ctx, tenantID, agentID)
			},
		},
		{
			name: "Heartbeat",
			invoke: func(ctx context.Context, reg FleetRegistry) error {
				return reg.Heartbeat(ctx, tenantID, agentID, nil)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kv := NewMemoryTaskKV()
			seedCorruptAgent(t, kv, tenantID, agentID)

			err := tc.invoke(context.Background(), NewFleetRegistry(kv))
			var re *FleetRegistryError
			if !errors.As(err, &re) || !errors.Is(re, ErrFleetRegistryMarshal) {
				t.Fatalf("want FleetRegistryError(marshal), got %T: %v", err, err)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// session_store: corrupt-record branches
// -----------------------------------------------------------------------------

// TestAgentStore_CorruptRecord covers the decode-failure branch of the single
// read and of the scan: a corrupt record is reported under the marshal
// sentinel rather than surfacing as an empty session or a shortened listing.
func TestAgentStore_CorruptRecord(t *testing.T) {
	cases := []struct {
		name   string
		invoke func(context.Context, AgentStore) error
	}{
		{
			name: "GetSession",
			invoke: func(ctx context.Context, store AgentStore) error {
				_, err := store.GetSession(ctx, "", "sess-1")
				return err
			},
		},
		{
			name: "ListSessions",
			invoke: func(ctx context.Context, store AgentStore) error {
				_, err := store.ListSessions(ctx, "system")
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kv := kvstore.NewMemory()
			if err := kv.Put(context.Background(), "orch/session/system/sess-1", []byte("{not json")); err != nil {
				t.Fatalf("seed corrupt record: %v", err)
			}

			err := tc.invoke(context.Background(), NewAgentStore(kv))
			var se *AgentStoreError
			if !errors.As(err, &se) || !errors.Is(se, ErrAgentStoreMarshal) {
				t.Fatalf("want AgentStoreError(marshal), got %v", err)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// task_store: corrupt-record branches
// -----------------------------------------------------------------------------

// seedCorruptTask writes bytes that are not a valid task record at the key the
// store itself derives for (tenantID, taskID).
func seedCorruptTask(t *testing.T, kv interfaces.KVStore, tenantID, taskID string) {
	t.Helper()
	key, err := taskRecordKey(tenantID, taskID)
	if err != nil {
		t.Fatalf("taskRecordKey(%s, %s): %v", tenantID, taskID, err)
	}
	if err := kv.Put(context.Background(), key, []byte("{not json")); err != nil {
		t.Fatalf("seed corrupt task record: %v", err)
	}
}

// TestTaskStore_CorruptRecord covers the decode-failure branch of every task
// store method that reads a single task record, including the locked
// read-modify-write transitions. A record the store cannot decode must be
// reported under the marshal sentinel rather than acted upon.
func TestTaskStore_CorruptRecord(t *testing.T) {
	const (
		tenantID = "t"
		taskID   = "task-1"
	)

	cases := []struct {
		name   string
		invoke func(context.Context, TaskStore) error
	}{
		{
			name: "GetTask",
			invoke: func(ctx context.Context, store TaskStore) error {
				_, err := store.GetTask(ctx, tenantID, taskID)
				return err
			},
		},
		{
			name: "UpdateTask",
			invoke: func(ctx context.Context, store TaskStore) error {
				return store.UpdateTask(ctx, &Task{ID: taskID, TenantID: tenantID})
			},
		},
		{
			name: "ClaimTask",
			invoke: func(ctx context.Context, store TaskStore) error {
				return store.ClaimTask(ctx, tenantID, taskID, "agent-a")
			},
		},
		{
			name: "DeleteTask",
			invoke: func(ctx context.Context, store TaskStore) error {
				return store.DeleteTask(ctx, tenantID, taskID)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kv := kvstore.NewMemory()
			seedCorruptTask(t, kv, tenantID, taskID)

			err := tc.invoke(context.Background(), NewInMemoryTaskStoreWithKV(kv, 0))
			var tse *TaskStoreError
			if !errors.As(err, &tse) || !errors.Is(tse, ErrTaskStoreMarshal) {
				t.Fatalf("want TaskStoreError(marshal), got %T: %v", err, err)
			}
		})
	}
}

// TestTaskStore_ListTasks_SkipsCorruptRecord covers ListTasks' in-scan
// decode-failure branch: one unreadable record must not hide the rest of the
// tenant's work queue.
func TestTaskStore_ListTasks_SkipsCorruptRecord(t *testing.T) {
	kv := NewMemoryTaskKV()
	store := NewInMemoryTaskStoreWithKV(kv, 0)
	ctx := context.Background()

	newTaskWithID(t, store, "t", "task-healthy")
	seedCorruptTask(t, kv, "t", "task-corrupt")

	tasks, err := store.ListTasks(ctx, "t", nil)
	if err != nil {
		t.Fatalf("ListTasks must survive one corrupt record, got %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "task-healthy" {
		t.Fatalf("want only the healthy task, got %+v", tasks)
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

// TestFileLock_LoadLockError covers the branch of each method that loads the
// lock first, where loadLock fails with a non-not-found store error.
func TestFileLock_LoadLockError(t *testing.T) {
	cases := []struct {
		name   string
		invoke func(context.Context, *fileLockStore) error
	}{
		{"Release", func(ctx context.Context, svc *fileLockStore) error {
			return svc.Release(ctx, "t", "lock-key")
		}},
		{"Renew", func(ctx context.Context, svc *fileLockStore) error {
			return svc.Renew(ctx, "t", "lock-key")
		}},
		{"WaitForRelease", func(ctx context.Context, svc *fileLockStore) error {
			return svc.WaitForRelease(ctx, "t", "lock-key")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kv := newFaultKVStore()
			kv.getErr = errors.New("kv get down")
			svc := newFaultFileLockSvc(t, kv)

			if err := tc.invoke(context.Background(), svc); err == nil {
				t.Fatalf("expected %s to surface the loadLock error", tc.name)
			}
		})
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
