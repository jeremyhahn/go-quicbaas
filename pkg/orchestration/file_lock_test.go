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

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
)

// =============================================================================
// Test Helpers: In-memory KVStore
// =============================================================================

// testFileLockKVStore is a simple in-memory KVStore for file lock testing.
type testFileLockKVStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newTestFileLockKVStore() *testFileLockKVStore {
	return &testFileLockKVStore{data: make(map[string][]byte)}
}

func (s *testFileLockKVStore) Put(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = append([]byte(nil), value...)
	return nil
}

func (s *testFileLockKVStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	if !ok {
		return nil, qerrors.NotFound("testFileLockKVStore.Get", key)
	}
	return append([]byte(nil), v...), nil
}

func (s *testFileLockKVStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func (s *testFileLockKVStore) Scan(_ context.Context, prefix string, fn func(string, []byte) error) error {
	// Snapshot matching entries under read lock, then invoke callback outside
	// the lock to avoid deadlock when the callback calls Delete or Put.
	s.mu.RLock()
	type entry struct {
		key   string
		value []byte
	}
	var entries []entry
	for k, v := range s.data {
		if strings.HasPrefix(k, prefix) {
			entries = append(entries, entry{key: k, value: append([]byte(nil), v...)})
		}
	}
	s.mu.RUnlock()

	for _, e := range entries {
		if err := fn(e.key, e.value); err != nil {
			return err
		}
	}
	return nil
}

func (s *testFileLockKVStore) List(_ context.Context, prefix string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var keys []string
	for k := range s.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (s *testFileLockKVStore) Exists(_ context.Context, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data[key]
	return ok, nil
}

// =============================================================================
// Test Helpers: In-memory LeaseManager
// =============================================================================

// testFileLockLeaseManager is a simple in-memory LeaseManager for file lock testing.
type testFileLockLeaseManager struct {
	mu     sync.RWMutex
	leases map[string]*lease.Lease
	seq    int
}

func newTestFileLockLeaseManager() *testFileLockLeaseManager {
	return &testFileLockLeaseManager{leases: make(map[string]*lease.Lease)}
}

func (m *testFileLockLeaseManager) CreateLease(_ context.Context, path string, ttl time.Duration, opts *lease.LeaseOptions) (*lease.Lease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	id := "test_lease_" + strings.ReplaceAll(path, ":", "_") + "_" + itoa(m.seq)
	now := time.Now()
	l := &lease.Lease{
		LeaseID:     id,
		SecretPath:  path,
		IssuedAt:    now,
		ExpiresAt:   now.Add(ttl),
		TTL:         ttl,
		Renewable:   true,
		MaxRenewals: 0,
	}
	if opts != nil {
		l.Renewable = opts.Renewable
		l.MaxRenewals = opts.MaxRenewals
		l.Metadata = opts.Metadata
	}
	m.leases[id] = l
	return l, nil
}

func (m *testFileLockLeaseManager) RenewLease(_ context.Context, leaseID string, increment time.Duration) (*lease.Lease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.leases[leaseID]
	if !ok {
		return nil, errors.New("lease not found")
	}
	l.ExpiresAt = time.Now().Add(increment)
	l.RenewCount++
	return l, nil
}

// copyLease returns a deep copy of a lease to avoid data races when the caller
// reads fields concurrently with mutations (e.g., RevokeLease setting RevokedAt).
func copyLease(l *lease.Lease) *lease.Lease {
	cp := *l
	if l.RevokedAt != nil {
		t := *l.RevokedAt
		cp.RevokedAt = &t
	}
	if l.Metadata != nil {
		cp.Metadata = make(map[string]string, len(l.Metadata))
		for k, v := range l.Metadata {
			cp.Metadata[k] = v
		}
	}
	return &cp
}

func (m *testFileLockLeaseManager) GetLease(_ context.Context, leaseID string) (*lease.Lease, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	l, ok := m.leases[leaseID]
	if !ok {
		return nil, errors.New("lease not found")
	}
	return copyLease(l), nil
}

func (m *testFileLockLeaseManager) RevokeLease(_ context.Context, leaseID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.leases[leaseID]
	if !ok {
		return errors.New("lease not found")
	}
	now := time.Now()
	l.RevokedAt = &now
	return nil
}

func (m *testFileLockLeaseManager) Validate(_ context.Context, leaseID string) (*lease.Lease, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	l, ok := m.leases[leaseID]
	if !ok {
		return nil, lease.NewLeaseNotFoundError("testFileLockLeaseManager.Validate", leaseID)
	}
	if l.RevokedAt != nil {
		return nil, lease.NewLeaseRevokedError("testFileLockLeaseManager.Validate", leaseID, *l.RevokedAt)
	}
	if l.State() == lease.LeaseStateExpired {
		return nil, lease.NewLeaseExpiredError("testFileLockLeaseManager.Validate", leaseID, l.ExpiresAt)
	}
	return copyLease(l), nil
}

func (m *testFileLockLeaseManager) ReapExpired(_ context.Context) (int, int, error) {
	return 0, 0, nil
}

func (m *testFileLockLeaseManager) RevokePrefix(_ context.Context, _ string) (int, error) {
	return 0, nil
}

func (m *testFileLockLeaseManager) ListLeases(_ context.Context, _ string) ([]*lease.Lease, error) {
	return nil, nil
}

func (m *testFileLockLeaseManager) StartExpirationLoop(_ context.Context) error {
	return nil
}

func (m *testFileLockLeaseManager) Close() error {
	return nil
}

// leasesForPath reports how many leases the manager has ever created for a
// secret path. A lock key that carries more than one has been handed to more
// than one holder.
func (m *testFileLockLeaseManager) leasesForPath(path string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, l := range m.leases {
		if l.SecretPath == path {
			n++
		}
	}
	return n
}

// expireLease forces a lease to expire for testing stale-lock scenarios.
func (m *testFileLockLeaseManager) expireLease(leaseID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.leases[leaseID]
	if ok {
		l.ExpiresAt = time.Now().Add(-1 * time.Hour)
	}
}

// =============================================================================
// Test Helpers: Mock RaftNode
// =============================================================================

// testFileLockRaftNode is a minimal RaftNode mock for fencing token tests.
type testFileLockRaftNode struct {
	interfaces.RaftNode
	term uint64
}

func (n *testFileLockRaftNode) GetShardRaftState(_ uint64) (uint64, uint64, error) {
	return 0, n.term, nil
}

// =============================================================================
// Test Helpers: Mock Logger
// =============================================================================

// testFileLockLogger is a no-op logger for file lock testing.
type testFileLockLogger struct{}

func (l *testFileLockLogger) Debug(_ string, _ ...any)                              {}
func (l *testFileLockLogger) Debugf(_ string, _ ...interface{})                     {}
func (l *testFileLockLogger) Info(_ string, _ ...any)                               {}
func (l *testFileLockLogger) Infof(_ string, _ ...interface{})                      {}
func (l *testFileLockLogger) Warn(_ string, _ ...any)                               {}
func (l *testFileLockLogger) Warnf(_ string, _ ...interface{})                      {}
func (l *testFileLockLogger) Error(_ string, _ ...any)                              {}
func (l *testFileLockLogger) Errorf(_ string, _ ...interface{})                     {}
func (l *testFileLockLogger) Fatal(_ string)                                        {}
func (l *testFileLockLogger) Fatalf(_ string, _ ...interface{})                     {}
func (l *testFileLockLogger) WithField(_ string, _ interface{}) interfaces.Logger   { return l }
func (l *testFileLockLogger) WithFields(_ map[string]interface{}) interfaces.Logger { return l }
func (l *testFileLockLogger) WithError(_ error) interfaces.Logger                   { return l }

// =============================================================================
// Test Helper: default service constructor
// =============================================================================

func newTestFileLockSvc(raftNode interfaces.RaftNode) (FileLockService, *testFileLockKVStore, *testFileLockLeaseManager) {
	kv := newTestFileLockKVStore()
	lm := newTestFileLockLeaseManager()
	settings := FileLockSettings{
		LeaseTTL:          "5m",
		HeartbeatInterval: "30s",
		MaxRenewals:       60,
	}
	svc := NewFileLockService(kv, lm, raftNode, 1, settings, IsolationShared, &testFileLockLogger{})
	return svc, kv, lm
}

// fileLockIsolationModes is every workspace isolation mode a lock service can
// be built with. Lock enforcement is identical across all of them, so the
// Acquire tests run against the whole set rather than against whichever single
// mode a change happens to be about.
var fileLockIsolationModes = []WorkspaceIsolation{IsolationShared, IsolationWorktree}

// newTestFileLockSvcIsolated builds a standalone (non-Raft) service in the
// given isolation mode.
func newTestFileLockSvcIsolated(isolation WorkspaceIsolation) (FileLockService, *testFileLockKVStore, *testFileLockLeaseManager) {
	kv := newTestFileLockKVStore()
	lm := newTestFileLockLeaseManager()
	settings := FileLockSettings{
		LeaseTTL:          "5m",
		HeartbeatInterval: "30s",
		MaxRenewals:       60,
	}
	svc := NewFileLockService(kv, lm, nil, 1, settings, isolation, &testFileLockLogger{})
	return svc, kv, lm
}

// =============================================================================
// Acquire Tests
// =============================================================================

func TestFileLock_Acquire_Success(t *testing.T) {
	svc, _, _ := newTestFileLockSvc(nil)
	ctx := context.Background()

	lock, err := svc.Acquire(ctx, "tenant1", "proj1", "/src/main.go", "agent-a")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if lock == nil {
		t.Fatal("expected lock, got nil")
	}
	if lock.AgentID != "agent-a" {
		t.Errorf("expected agent-a, got %s", lock.AgentID)
	}
	if lock.TenantID != "tenant1" {
		t.Errorf("expected tenant1, got %s", lock.TenantID)
	}
	if lock.ProjectID != "proj1" {
		t.Errorf("expected proj1, got %s", lock.ProjectID)
	}
	if lock.FilePath != "/src/main.go" {
		t.Errorf("expected /src/main.go, got %s", lock.FilePath)
	}
	if lock.LeaseID == "" {
		t.Error("expected non-empty lease ID")
	}
	if lock.FencingToken != 0 {
		t.Errorf("expected fencing token 0 in standalone mode, got %d", lock.FencingToken)
	}
	if lock.Isolation != "shared" {
		t.Errorf("expected shared isolation, got %s", lock.Isolation)
	}
	if lock.AcquiredAt == 0 {
		t.Error("expected non-zero acquired_at")
	}
}

func TestFileLock_Acquire_MissingFields(t *testing.T) {
	svc, _, _ := newTestFileLockSvc(nil)
	ctx := context.Background()

	tests := []struct {
		name      string
		tenantID  string
		projectID string
		filePath  string
		agentID   string
		wantField string
	}{
		{"missing tenant_id", "", "proj1", "/main.go", "a", "tenant_id"},
		{"missing project_id", "t1", "", "/main.go", "a", "project_id"},
		{"missing file_path", "t1", "proj1", "", "a", "file_path"},
		{"missing agent_id", "t1", "proj1", "/main.go", "", "agent_id"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Acquire(ctx, tc.tenantID, tc.projectID, tc.filePath, tc.agentID)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, ErrFileLockValidation) {
				t.Errorf("expected ErrFileLockValidation, got %v", err)
			}
			var valErr *FileLockValidationError
			if !errors.As(err, &valErr) {
				t.Fatalf("expected FileLockValidationError, got %T", err)
			}
			if valErr.Field != tc.wantField {
				t.Errorf("expected field %s, got %s", tc.wantField, valErr.Field)
			}
		})
	}
}

// TestFileLock_Acquire_LiveLockOfAnotherAgentIsRefusedInEveryIsolationMode is
// the mode-check test. Acquire once granted a live lock to a second agent
// whenever the workflow declared IsolationWorktree, on the reasoning that
// separate worktrees cannot collide on a file. An implementation that brings
// that weakening back passes the "shared" case and fails the "worktree" one.
//
// The weakening is wrong on its own terms: this service locks an opaque
// (tenant, project, path) triple and cannot tell a path a per-agent worktree
// privatises from one that stays shared, and refs, the object database and a
// submodule's checkout stay shared however the working directories are split.
func TestFileLock_Acquire_LiveLockOfAnotherAgentIsRefusedInEveryIsolationMode(t *testing.T) {
	for _, isolation := range fileLockIsolationModes {
		t.Run(string(isolation), func(t *testing.T) {
			svc, _, _ := newTestFileLockSvcIsolated(isolation)
			ctx := context.Background()

			if _, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-a"); err != nil {
				t.Fatalf("first acquire failed: %v", err)
			}

			_, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-b")
			if err == nil {
				t.Fatalf("isolation %q granted /file.go to agent-b while agent-a's "+
					"lease was still live: two agents now believe they hold it "+
					"exclusively and will write over each other", isolation)
			}
			if !errors.Is(err, ErrFileLocked) {
				t.Fatalf("expected ErrFileLocked, got %v", err)
			}

			var lockedErr *FileLockedError
			if !errors.As(err, &lockedErr) {
				t.Fatalf("expected *FileLockedError, got %T", err)
			}
			if lockedErr.AgentID != "agent-a" {
				t.Errorf("expected holder agent-a, got %s", lockedErr.AgentID)
			}
			if lockedErr.FilePath != "/file.go" {
				t.Errorf("expected path /file.go, got %s", lockedErr.FilePath)
			}
		})
	}
}

// TestFileLock_Acquire_RefusalLeavesTheHoldersLeaseAndRecordIntact names what
// granting the override cost, which is more than an interleaved write.
//
// A granted override created a SECOND lease on the same lock key and pointed
// the stored record at it. The original holder, which had been told nothing,
// then finished its critical section normally — and its Release revoked the
// NEWCOMER's lease and deleted the newcomer's lock while the newcomer was
// still editing, while its Renew extended a lease it did not hold. Refusing
// keeps one lease, one record and one holder per key, so each agent's Renew
// and Release act on their own lock and nobody else's.
func TestFileLock_Acquire_RefusalLeavesTheHoldersLeaseAndRecordIntact(t *testing.T) {
	for _, isolation := range fileLockIsolationModes {
		t.Run(string(isolation), func(t *testing.T) {
			svc, _, lm := newTestFileLockSvcIsolated(isolation)
			ctx := context.Background()

			held, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-a")
			if err != nil {
				t.Fatalf("first acquire failed: %v", err)
			}
			if held.Isolation != string(isolation) {
				t.Errorf("expected the lock to record isolation %q, got %q", isolation, held.Isolation)
			}

			if _, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-b"); err == nil {
				t.Fatalf("isolation %q granted the lock to agent-b", isolation)
			}

			if n := lm.leasesForPath(held.LockKey); n != 1 {
				t.Fatalf("expected exactly 1 lease on the lock key, got %d: a second "+
					"lease means agent-a's Release would revoke agent-b's", n)
			}

			stored, active, err := svc.IsLocked(ctx, "t1", "p1", "/file.go")
			if err != nil {
				t.Fatalf("IsLocked failed: %v", err)
			}
			if !active {
				t.Fatal("expected the lock to still be held")
			}
			if stored.AgentID != "agent-a" {
				t.Errorf("expected the record to still name agent-a, got %s", stored.AgentID)
			}
			if stored.LeaseID != held.LeaseID {
				t.Errorf("expected the record to still carry agent-a's lease %s, got %s",
					held.LeaseID, stored.LeaseID)
			}

			// Agent-a's renewal therefore extends agent-a's own lease.
			if err := svc.Renew(ctx, "t1", held.LockKey); err != nil {
				t.Fatalf("holder could not renew its own lock: %v", err)
			}

			// And its release ends its own critical section, after which the
			// lock is genuinely available to the agent that was refused.
			if err := svc.Release(ctx, "t1", held.LockKey); err != nil {
				t.Fatalf("holder could not release its own lock: %v", err)
			}
			taken, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-b")
			if err != nil {
				t.Fatalf("expected agent-b to acquire after release, got %v", err)
			}
			if taken.AgentID != "agent-b" {
				t.Errorf("expected agent-b, got %s", taken.AgentID)
			}
		})
	}
}

// TestFileLock_Acquire_StaleLock_Overwritten covers the other half of the
// rule: what a refusal is NOT. An expired lease is not a holder, so the lock
// is reclaimable in every isolation mode. Were this to start refusing, one
// crashed agent would strand a path for as long as the record survived.
func TestFileLock_Acquire_StaleLock_Overwritten(t *testing.T) {
	for _, isolation := range fileLockIsolationModes {
		t.Run(string(isolation), func(t *testing.T) {
			svc, _, lm := newTestFileLockSvcIsolated(isolation)
			ctx := context.Background()

			lock1, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-a")
			if err != nil {
				t.Fatalf("first acquire failed: %v", err)
			}

			lm.expireLease(lock1.LeaseID)

			lock2, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-b")
			if err != nil {
				t.Fatalf("expected stale lock overwrite, got %v", err)
			}
			if lock2.AgentID != "agent-b" {
				t.Errorf("expected agent-b, got %s", lock2.AgentID)
			}
		})
	}
}

// TestFileLock_Acquire_SameAgent_Idempotent asserts the refusal is scoped to
// another agent. A holder re-acquiring its own lock is a no-op it is entitled
// to, in every isolation mode.
func TestFileLock_Acquire_SameAgent_Idempotent(t *testing.T) {
	for _, isolation := range fileLockIsolationModes {
		t.Run(string(isolation), func(t *testing.T) {
			svc, _, _ := newTestFileLockSvcIsolated(isolation)
			ctx := context.Background()

			if _, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-a"); err != nil {
				t.Fatalf("first acquire failed: %v", err)
			}

			lock, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-a")
			if err != nil {
				t.Fatalf("expected idempotent re-acquire, got %v", err)
			}
			if lock.AgentID != "agent-a" {
				t.Errorf("expected agent-a, got %s", lock.AgentID)
			}
		})
	}
}

// =============================================================================
// Release Tests
// =============================================================================

func TestFileLock_Release_Success(t *testing.T) {
	svc, kv, _ := newTestFileLockSvc(nil)
	ctx := context.Background()

	lock, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-a")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	err = svc.Release(ctx, "t1", lock.LockKey)
	if err != nil {
		t.Fatalf("release failed: %v", err)
	}

	// Verify lock is removed from KV.
	exists, _ := kv.Exists(ctx, lock.LockKey)
	if exists {
		t.Error("expected lock to be removed from KV store")
	}
}

func TestFileLock_Release_NotFound(t *testing.T) {
	svc, _, _ := newTestFileLockSvc(nil)
	ctx := context.Background()

	err := svc.Release(ctx, "t1", "filelock:t1:p1:nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrLockNotFound) {
		t.Errorf("expected ErrLockNotFound, got %v", err)
	}
}

func TestFileLock_Release_NotOwner(t *testing.T) {
	// In the current implementation, Release uses tenantID + lockKey and does not
	// verify caller identity (the orchestrator controls access). This test verifies
	// that a lock can be released when the lock key is known.
	svc, _, _ := newTestFileLockSvc(nil)
	ctx := context.Background()

	lock, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-a")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Release with a different "context" but same lockKey should succeed
	// because the orchestrator mediates access control externally.
	err = svc.Release(ctx, "t1", lock.LockKey)
	if err != nil {
		t.Fatalf("expected release to succeed, got %v", err)
	}
}

// =============================================================================
// Renew Tests
// =============================================================================

func TestFileLock_Renew_Success(t *testing.T) {
	svc, _, lm := newTestFileLockSvc(nil)
	ctx := context.Background()

	lock, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-a")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	err = svc.Renew(ctx, "t1", lock.LockKey)
	if err != nil {
		t.Fatalf("renew failed: %v", err)
	}

	// Verify lease was renewed.
	lm.mu.RLock()
	l := lm.leases[lock.LeaseID]
	renewCount := l.RenewCount
	lm.mu.RUnlock()
	if renewCount != 1 {
		t.Errorf("expected renew count 1, got %d", renewCount)
	}
}

func TestFileLock_Renew_NotFound(t *testing.T) {
	svc, _, _ := newTestFileLockSvc(nil)
	ctx := context.Background()

	err := svc.Renew(ctx, "t1", "filelock:t1:p1:nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrLockNotFound) {
		t.Errorf("expected ErrLockNotFound, got %v", err)
	}
}

// =============================================================================
// IsLocked Tests
// =============================================================================

func TestFileLock_IsLocked_Active(t *testing.T) {
	svc, _, _ := newTestFileLockSvc(nil)
	ctx := context.Background()

	_, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-a")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	lock, locked, err := svc.IsLocked(ctx, "t1", "p1", "/file.go")
	if err != nil {
		t.Fatalf("IsLocked failed: %v", err)
	}
	if !locked {
		t.Error("expected locked=true")
	}
	if lock == nil {
		t.Fatal("expected lock, got nil")
	}
	if lock.AgentID != "agent-a" {
		t.Errorf("expected agent-a, got %s", lock.AgentID)
	}
}

func TestFileLock_IsLocked_NotLocked(t *testing.T) {
	svc, _, _ := newTestFileLockSvc(nil)
	ctx := context.Background()

	lock, locked, err := svc.IsLocked(ctx, "t1", "p1", "/file.go")
	if err != nil {
		t.Fatalf("IsLocked failed: %v", err)
	}
	if locked {
		t.Error("expected locked=false")
	}
	if lock != nil {
		t.Error("expected nil lock")
	}
}

func TestFileLock_IsLocked_ExpiredLease(t *testing.T) {
	svc, kv, lm := newTestFileLockSvc(nil)
	ctx := context.Background()

	acquired, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-a")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Expire the lease.
	lm.expireLease(acquired.LeaseID)

	lock, locked, err := svc.IsLocked(ctx, "t1", "p1", "/file.go")
	if err != nil {
		t.Fatalf("IsLocked failed: %v", err)
	}
	if locked {
		t.Error("expected locked=false for expired lease")
	}
	if lock != nil {
		t.Error("expected nil lock for expired lease")
	}

	// Verify stale lock was cleaned up.
	exists, _ := kv.Exists(ctx, acquired.LockKey)
	if exists {
		t.Error("expected stale lock to be cleaned up")
	}
}

// =============================================================================
// ListLocks Tests
// =============================================================================

func TestFileLock_ListLocks_Success(t *testing.T) {
	svc, _, _ := newTestFileLockSvc(nil)
	ctx := context.Background()

	_, err := svc.Acquire(ctx, "t1", "p1", "/a.go", "agent-a")
	if err != nil {
		t.Fatalf("acquire 1 failed: %v", err)
	}
	_, err = svc.Acquire(ctx, "t1", "p1", "/b.go", "agent-b")
	if err != nil {
		t.Fatalf("acquire 2 failed: %v", err)
	}

	locks, err := svc.ListLocks(ctx, "t1", "p1")
	if err != nil {
		t.Fatalf("ListLocks failed: %v", err)
	}
	if len(locks) != 2 {
		t.Errorf("expected 2 locks, got %d", len(locks))
	}
}

func TestFileLock_ListLocks_Empty(t *testing.T) {
	svc, _, _ := newTestFileLockSvc(nil)
	ctx := context.Background()

	locks, err := svc.ListLocks(ctx, "t1", "p1")
	if err != nil {
		t.Fatalf("ListLocks failed: %v", err)
	}
	if len(locks) != 0 {
		t.Errorf("expected 0 locks, got %d", len(locks))
	}
}

func TestFileLock_ListLocks_FiltersExpiredLeases(t *testing.T) {
	svc, _, lm := newTestFileLockSvc(nil)
	ctx := context.Background()

	lock1, err := svc.Acquire(ctx, "t1", "p1", "/a.go", "agent-a")
	if err != nil {
		t.Fatalf("acquire 1 failed: %v", err)
	}
	_, err = svc.Acquire(ctx, "t1", "p1", "/b.go", "agent-b")
	if err != nil {
		t.Fatalf("acquire 2 failed: %v", err)
	}

	// Expire one lease.
	lm.expireLease(lock1.LeaseID)

	locks, err := svc.ListLocks(ctx, "t1", "p1")
	if err != nil {
		t.Fatalf("ListLocks failed: %v", err)
	}
	if len(locks) != 1 {
		t.Errorf("expected 1 active lock, got %d", len(locks))
	}
	if len(locks) == 1 && locks[0].AgentID != "agent-b" {
		t.Errorf("expected agent-b, got %s", locks[0].AgentID)
	}
}

// =============================================================================
// Fencing Token Tests
// =============================================================================

func TestFileLock_FencingToken_Standalone(t *testing.T) {
	svc, _, _ := newTestFileLockSvc(nil)
	ctx := context.Background()

	lock, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-a")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	if lock.FencingToken != 0 {
		t.Errorf("expected fencing token 0 in standalone mode, got %d", lock.FencingToken)
	}
}

func TestFileLock_FencingToken_Valid(t *testing.T) {
	raft := &testFileLockRaftNode{term: 5}
	svc, _, _ := newTestFileLockSvc(raft)
	ctx := context.Background()

	lock, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-a")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	if lock.FencingToken != 5 {
		t.Errorf("expected fencing token 5, got %d", lock.FencingToken)
	}

	// Renewing should succeed because term hasn't advanced.
	err = svc.Renew(ctx, "t1", lock.LockKey)
	if err != nil {
		t.Fatalf("renew failed: %v", err)
	}
}

func TestFileLock_FencingToken_Stale(t *testing.T) {
	raft := &testFileLockRaftNode{term: 5}
	svc, kv, _ := newTestFileLockSvc(raft)
	ctx := context.Background()

	lock, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-a")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Advance the Raft term.
	raft.term = 10

	// Manually inject the lock with old fencing token to simulate staleness.
	lock.FencingToken = 3
	data, _ := json.Marshal(lock)
	_ = kv.Put(ctx, lock.LockKey, data)

	// Renewing should fail with stale fencing token.
	err = svc.Renew(ctx, "t1", lock.LockKey)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrStaleLock) {
		t.Errorf("expected ErrStaleLock, got %v", err)
	}
	var staleErr *StaleLockError
	if !errors.As(err, &staleErr) {
		t.Fatalf("expected StaleLockError, got %T", err)
	}
	if staleErr.LockTerm != 3 {
		t.Errorf("expected lock term 3, got %d", staleErr.LockTerm)
	}
	if staleErr.CurrentTerm != 10 {
		t.Errorf("expected current term 10, got %d", staleErr.CurrentTerm)
	}
}

// =============================================================================
// WaitForRelease Tests
// =============================================================================

func TestFileLock_WaitForRelease_AlreadyUnlocked(t *testing.T) {
	svc, _, _ := newTestFileLockSvc(nil)
	ctx := context.Background()

	// Waiting for a lock that doesn't exist should return immediately.
	err := svc.WaitForRelease(ctx, "t1", "filelock:t1:p1:nonexistent")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestFileLock_WaitForRelease_ReleasedDuringWait(t *testing.T) {
	svc, _, _ := newTestFileLockSvc(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	lock, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-a")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Release the lock asynchronously after a short delay.
	done := make(chan struct{})
	go func() {
		// Use a timer instead of time.Sleep per conventions.
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		<-timer.C
		_ = svc.Release(ctx, "t1", lock.LockKey)
		close(done)
	}()

	// WaitForRelease should return when the lock is released.
	err = svc.WaitForRelease(ctx, "t1", lock.LockKey)
	if err != nil {
		t.Fatalf("WaitForRelease failed: %v", err)
	}

	<-done
}

func TestFileLock_WaitForRelease_ContextCancelled(t *testing.T) {
	svc, _, _ := newTestFileLockSvc(nil)
	ctx := context.Background()

	_, err := svc.Acquire(ctx, "t1", "p1", "/file.go", "agent-a")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Cancel context quickly.
	waitCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	lockKey := buildLockKey("t1", "p1", "/file.go")
	err = svc.WaitForRelease(waitCtx, "t1", lockKey)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
}

// =============================================================================
// Error Type Tests
// =============================================================================

func TestFileLockedError_ErrorString(t *testing.T) {
	err := &FileLockedError{AgentID: "a1", FilePath: "/f.go", AcquiredAt: 123}
	if !strings.Contains(err.Error(), "a1") {
		t.Error("expected agent ID in error string")
	}
	if !errors.Is(err, ErrFileLocked) {
		t.Error("expected unwrap to ErrFileLocked")
	}
}

func TestLockNotFoundError_ErrorString(t *testing.T) {
	err := &LockNotFoundError{LockKey: "k1"}
	if !strings.Contains(err.Error(), "k1") {
		t.Error("expected lock key in error string")
	}
	if !errors.Is(err, ErrLockNotFound) {
		t.Error("expected unwrap to ErrLockNotFound")
	}
}

func TestStaleLockError_ErrorString(t *testing.T) {
	err := &StaleLockError{LockKey: "k1", LockTerm: 3, CurrentTerm: 7}
	if !strings.Contains(err.Error(), "k1") {
		t.Error("expected lock key in error string")
	}
	if !errors.Is(err, ErrStaleLock) {
		t.Error("expected unwrap to ErrStaleLock")
	}
}

func TestLockReleaseNotOwnerError_ErrorString(t *testing.T) {
	err := &LockReleaseNotOwnerError{LockKey: "k1", OwnerID: "o1", CallerID: "c1"}
	if !strings.Contains(err.Error(), "o1") {
		t.Error("expected owner in error string")
	}
	if !errors.Is(err, ErrLockReleaseNotOwner) {
		t.Error("expected unwrap to ErrLockReleaseNotOwner")
	}
}

func TestFileLockValidationError_ErrorString(t *testing.T) {
	err := &FileLockValidationError{Field: "tenant_id", Message: "required"}
	if !strings.Contains(err.Error(), "tenant_id") {
		t.Error("expected field in error string")
	}
	if !errors.Is(err, ErrFileLockValidation) {
		t.Error("expected unwrap to ErrFileLockValidation")
	}
}

// =============================================================================
// buildLockKey Tests
// =============================================================================

func TestBuildLockKey_Deterministic(t *testing.T) {
	key1 := buildLockKey("t1", "p1", "/file.go")
	key2 := buildLockKey("t1", "p1", "/file.go")
	if key1 != key2 {
		t.Errorf("expected deterministic keys, got %s != %s", key1, key2)
	}
}

func TestBuildLockKey_DifferentPaths(t *testing.T) {
	key1 := buildLockKey("t1", "p1", "/file.go")
	key2 := buildLockKey("t1", "p1", "/other.go")
	if key1 == key2 {
		t.Error("expected different keys for different file paths")
	}
}

func TestBuildLockKey_ContainsPrefix(t *testing.T) {
	key := buildLockKey("t1", "p1", "/file.go")
	if !strings.HasPrefix(key, "filelock:t1:p1:") {
		t.Errorf("expected prefix filelock:t1:p1:, got %s", key)
	}
}
