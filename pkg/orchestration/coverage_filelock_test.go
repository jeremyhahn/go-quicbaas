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

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// file_lock_store.go — KV error-path coverage
// =============================================================================

// faultyFileLockKVStore wraps a real in-memory KV store but can be told to
// fail specific operations, exercising the file-lock store's error branches.
type faultyFileLockKVStore struct {
	inner   *testFileLockKVStore
	getErr  error
	putErr  error
	delErr  error
	scanErr error
	badJSON bool // when true, Get returns a corrupt JSON payload
}

func newFaultyFileLockKVStore() *faultyFileLockKVStore {
	return &faultyFileLockKVStore{inner: newTestFileLockKVStore()}
}

func (s *faultyFileLockKVStore) Put(ctx context.Context, key string, value []byte) error {
	if s.putErr != nil {
		return s.putErr
	}
	return s.inner.Put(ctx, key, value)
}

func (s *faultyFileLockKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.badJSON {
		return []byte("{not-json"), nil
	}
	return s.inner.Get(ctx, key)
}

func (s *faultyFileLockKVStore) Delete(ctx context.Context, key string) error {
	if s.delErr != nil {
		return s.delErr
	}
	return s.inner.Delete(ctx, key)
}

func (s *faultyFileLockKVStore) Scan(ctx context.Context, prefix string, fn func(string, []byte) error) error {
	if s.scanErr != nil {
		return s.scanErr
	}
	return s.inner.Scan(ctx, prefix, fn)
}

func (s *faultyFileLockKVStore) List(ctx context.Context, prefix string) ([]string, error) {
	return s.inner.List(ctx, prefix)
}

func (s *faultyFileLockKVStore) Exists(ctx context.Context, key string) (bool, error) {
	return s.inner.Exists(ctx, key)
}

func newFaultyFileLockSvc(kv interfaces.KVStore) FileLockService {
	lm := newTestFileLockLeaseManager()
	settings := FileLockSettings{
		LeaseTTL:          "5m",
		HeartbeatInterval: "30s",
		MaxRenewals:       60,
	}
	return NewFileLockService(kv, lm, nil, 1, settings, IsolationShared, &testFileLockLogger{})
}

func TestFileLock_Acquire_PutError(t *testing.T) {
	kv := newFaultyFileLockKVStore()
	kv.putErr = errors.New("kv put down")
	svc := newFaultyFileLockSvc(kv)

	_, err := svc.Acquire(context.Background(), "t1", "p1", "/file.go", "agent-a")
	if !errors.Is(err, kv.putErr) {
		t.Fatalf("expected put error to propagate, got %v", err)
	}
}

func TestFileLock_Acquire_LoadError(t *testing.T) {
	kv := newFaultyFileLockKVStore()
	kv.getErr = errors.New("kv get down")
	svc := newFaultyFileLockSvc(kv)

	_, err := svc.Acquire(context.Background(), "t1", "p1", "/file.go", "agent-a")
	if !errors.Is(err, kv.getErr) {
		t.Fatalf("expected get error to propagate from loadLock, got %v", err)
	}
}

func TestFileLock_Release_LoadError(t *testing.T) {
	kv := newFaultyFileLockKVStore()
	kv.getErr = errors.New("kv get down")
	svc := newFaultyFileLockSvc(kv)

	if err := svc.Release(context.Background(), "t1", "filelock:t1:p1:x"); !errors.Is(err, kv.getErr) {
		t.Fatalf("expected get error to propagate, got %v", err)
	}
}

func TestFileLock_Release_DeleteError(t *testing.T) {
	kv := newFaultyFileLockKVStore()
	svc := newFaultyFileLockSvc(kv)
	ctx := context.Background()

	lock, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-a")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	kv.delErr = errors.New("kv delete down")
	if err := svc.Release(ctx, "t1", lock.LockKey); !errors.Is(err, kv.delErr) {
		t.Fatalf("expected delete error to propagate, got %v", err)
	}
}

func TestFileLock_Renew_LoadError(t *testing.T) {
	kv := newFaultyFileLockKVStore()
	kv.getErr = errors.New("kv get down")
	svc := newFaultyFileLockSvc(kv)

	if err := svc.Renew(context.Background(), "t1", "filelock:t1:p1:x"); !errors.Is(err, kv.getErr) {
		t.Fatalf("expected get error to propagate, got %v", err)
	}
}

func TestFileLock_IsLocked_LoadError(t *testing.T) {
	kv := newFaultyFileLockKVStore()
	kv.getErr = errors.New("kv get down")
	svc := newFaultyFileLockSvc(kv)

	if _, _, err := svc.IsLocked(context.Background(), "t1", "p1", "/file.go"); !errors.Is(err, kv.getErr) {
		t.Fatalf("expected get error to propagate, got %v", err)
	}
}

func TestFileLock_IsLocked_CorruptJSON(t *testing.T) {
	kv := newFaultyFileLockKVStore()
	svc := newFaultyFileLockSvc(kv)
	ctx := context.Background()

	// Seed a valid lock, then make Get return corrupt JSON.
	if _, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-a"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	kv.badJSON = true

	if _, _, err := svc.IsLocked(ctx, "t1", "p1", "/file.go"); err == nil {
		t.Fatal("expected JSON unmarshal error from loadLock")
	}
}

func TestFileLock_WaitForRelease_NotLocked(t *testing.T) {
	kv := newFaultyFileLockKVStore()
	svc := newFaultyFileLockSvc(kv)

	// No lock present → returns nil immediately.
	if err := svc.WaitForRelease(context.Background(), "t1", "filelock:t1:p1:absent"); err != nil {
		t.Fatalf("expected nil for absent lock, got %v", err)
	}
}

func TestFileLock_WaitForRelease_LoadError(t *testing.T) {
	kv := newFaultyFileLockKVStore()
	kv.getErr = errors.New("kv get down")
	svc := newFaultyFileLockSvc(kv)

	if err := svc.WaitForRelease(context.Background(), "t1", "filelock:t1:p1:x"); !errors.Is(err, kv.getErr) {
		t.Fatalf("expected get error to propagate, got %v", err)
	}
}

func TestFileLock_WaitForRelease_LeaseExpired(t *testing.T) {
	svc, _, lm := newTestFileLockSvc(nil)
	ctx := context.Background()

	lock, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-a")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// Expire the lease so WaitForRelease sees the lock as already released.
	lm.expireLease(lock.LeaseID)

	if err := svc.WaitForRelease(ctx, "t1", lock.LockKey); err != nil {
		t.Fatalf("expected nil once lease expired, got %v", err)
	}
}

func TestFileLock_ListLocks_ScanError(t *testing.T) {
	kv := newFaultyFileLockKVStore()
	kv.scanErr = errors.New("kv scan down")
	svc := newFaultyFileLockSvc(kv)

	if _, err := svc.ListLocks(context.Background(), "t1", "p1"); !errors.Is(err, kv.scanErr) {
		t.Fatalf("expected scan error to propagate, got %v", err)
	}
}

func TestFileLock_ListLocks_SkipsStaleAndEmpty(t *testing.T) {
	svc, _, lm := newTestFileLockSvc(nil)
	ctx := context.Background()

	// Empty project: ListLocks returns a non-nil empty slice.
	locks, err := svc.ListLocks(ctx, "t1", "empty")
	if err != nil {
		t.Fatalf("ListLocks: %v", err)
	}
	if locks == nil || len(locks) != 0 {
		t.Fatalf("expected non-nil empty slice, got %v", locks)
	}

	// Acquire two locks; expire one so the stale-cleanup branch runs.
	l1, err := svc.Acquire(ctx, "t1", "p1", "/a.go", "agent-a")
	if err != nil {
		t.Fatalf("Acquire a: %v", err)
	}
	if _, err := svc.Acquire(ctx, "t1", "p1", "/b.go", "agent-b"); err != nil {
		t.Fatalf("Acquire b: %v", err)
	}
	lm.expireLease(l1.LeaseID)

	locks, err = svc.ListLocks(ctx, "t1", "p1")
	if err != nil {
		t.Fatalf("ListLocks: %v", err)
	}
	if len(locks) != 1 {
		t.Fatalf("expected 1 active lock after stale cleanup, got %d", len(locks))
	}
}

// =============================================================================
// control_loop.go — agent capability filtering helpers
// =============================================================================

func TestFilterAgentsByCapability(t *testing.T) {
	agents := []*FleetNode{
		{ID: "a1", Capabilities: []string{"claude", "ollama"}},
		{ID: "a2", Capabilities: []string{"codex"}},
		nil, // must be skipped without panicking
		{ID: "a3", Capabilities: []string{"ollama"}},
	}

	got := filterFleetNodesByCapability(agents, "ollama")
	if len(got) != 2 {
		t.Fatalf("expected 2 agents with ollama capability, got %d", len(got))
	}

	none := filterFleetNodesByCapability(agents, "nonexistent")
	if len(none) != 0 {
		t.Fatalf("expected no agents, got %d", len(none))
	}
}

func TestAgentExposesBackend(t *testing.T) {
	// Primary backend match.
	primary := &FleetNode{ID: "a1", Backend: "claude"}
	if !FleetNodeServesBackend(primary, "claude") {
		t.Fatal("expected primary backend match")
	}
	// Capability match.
	viaCap := &FleetNode{ID: "a2", Backend: "claude", Capabilities: []string{"ollama"}}
	if !FleetNodeServesBackend(viaCap, "ollama") {
		t.Fatal("expected capability backend match")
	}
	// No match.
	if FleetNodeServesBackend(viaCap, "codex") {
		t.Fatal("expected no match for codex")
	}
}

// =============================================================================
// session_log.go — ensureTimestamp
// =============================================================================

func TestEnsureTimestamp(t *testing.T) {
	// Zero timestamp gets stamped.
	ev := &SessionEvent{}
	ensureTimestamp(ev)
	if ev.Timestamp.IsZero() {
		t.Fatal("ensureTimestamp must set a non-zero timestamp")
	}

	// Existing timestamp is preserved.
	fixed := ev.Timestamp
	ensureTimestamp(ev)
	if !ev.Timestamp.Equal(fixed) {
		t.Fatal("ensureTimestamp must not overwrite a non-zero timestamp")
	}
}
