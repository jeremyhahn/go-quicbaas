//go:build raft

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

package membership

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/discovery"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
	"github.com/jeremyhahn/go-quicbaas/pkg/watch"
)

// benchStorage is a minimal in-memory storage for benchmarks. It avoids
// the thread-safe error-injection plumbing that the test mockStorage carries
// so the hot path stays as lean as possible.
type benchStorage struct {
	data map[string][]byte
	mu   sync.RWMutex
}

func newBenchStorage() *benchStorage {
	return &benchStorage{data: make(map[string][]byte)}
}

func (s *benchStorage) Set(key, value []byte) error {
	s.mu.Lock()
	s.data[string(key)] = value
	s.mu.Unlock()
	return nil
}

func (s *benchStorage) Get(key []byte) ([]byte, error) {
	s.mu.RLock()
	v, ok := s.data[string(key)]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrKeyNotFound
	}
	return v, nil
}

func (s *benchStorage) Delete(key []byte) error {
	s.mu.Lock()
	delete(s.data, string(key))
	s.mu.Unlock()
	return nil
}

func (s *benchStorage) Batch() storage.Batch                                    { return nil }
func (s *benchStorage) NewIterator(_ *storage.IteratorOptions) storage.Iterator { return nil }
func (s *benchStorage) Snapshot() (storage.Snapshot, error)                     { return nil, nil }
func (s *benchStorage) Close() error                                            { return nil }

func (s *benchStorage) List(prefix []byte) ([][]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var keys [][]byte
	p := string(prefix)
	for k := range s.data {
		if len(p) == 0 || len(k) >= len(p) && k[:len(p)] == p {
			keys = append(keys, []byte(k))
		}
	}
	return keys, nil
}

func (s *benchStorage) Exists(key []byte) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data[string(key)]
	return ok, nil
}

// setupBenchManager creates a Manager wired to in-memory dependencies.
// It intentionally avoids t.Helper() / testing.T so it can be called from
// benchmarks without the testing overhead.
func setupBenchManager(b *testing.B, nodeID uint64, opts ...Option) *Manager {
	b.Helper()

	store := newBenchStorage()

	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		b.Fatalf("logger.New: %v", err)
	}

	watchMgr := watch.NewManager()

	peer := discovery.Peer{
		NodeID:   nodeID,
		Address:  fmt.Sprintf("localhost:%d", 9000+nodeID),
		RaftAddr: fmt.Sprintf("localhost:%d", 9000+nodeID),
		Metadata: map[string]string{"region": "us-east-1"},
	}

	config := &Config{
		LeaseTTL:            2 * time.Second,
		KeepAliveInterval:   1 * time.Second,
		HealthCheckInterval: 3 * time.Second,
		MaxRetries:          3,
		RetryBaseDelay:      10 * time.Millisecond,
	}

	mgr, err := NewManager(store, watchMgr, peer, config, log, opts...)
	if err != nil {
		b.Fatalf("NewManager: %v", err)
	}

	return mgr
}

// ---------------------------------------------------------------------------
// Register benchmarks
// ---------------------------------------------------------------------------

// BenchmarkRegister measures the throughput of registering a member. Each
// iteration creates a fresh Manager to avoid "already registered" state.
func BenchmarkRegister(b *testing.B) {
	b.ReportAllocs()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr := setupBenchManager(b, uint64(i+1))
		if err := mgr.Register(ctx); err != nil {
			b.Fatalf("Register: %v", err)
		}
		mgr.Close()
	}
}

// BenchmarkRegisterParallel measures concurrent registration throughput across
// distinct Manager instances.
func BenchmarkRegisterParallel(b *testing.B) {
	b.ReportAllocs()

	ctx := context.Background()
	var nodeCounter uint64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var local uint64
		for pb.Next() {
			// Use a per-goroutine incrementing counter offset by goroutine identity
			// to avoid collisions. Atomics are avoided here intentionally; slight
			// overlap in nodeID is harmless because each Manager is independent.
			local++
			mgr := setupBenchManager(b, nodeCounter+local)
			if err := mgr.Register(ctx); err != nil {
				b.Fatalf("Register: %v", err)
			}
			mgr.Close()
		}
	})
	_ = nodeCounter // silence vet
}

// ---------------------------------------------------------------------------
// Member lookup benchmarks
// ---------------------------------------------------------------------------

// BenchmarkGetMember measures single member lookup by NodeID from the
// in-memory members map.
func BenchmarkGetMember(b *testing.B) {
	mgr := setupBenchManager(b, 1)
	defer mgr.Close()

	// Pre-populate members map.
	mgr.membersMu.Lock()
	for i := uint64(1); i <= 100; i++ {
		mgr.members[i] = Member{
			NodeID:   i,
			Address:  fmt.Sprintf("localhost:%d", 9000+i),
			LastSeen: time.Now(),
		}
	}
	mgr.membersMu.Unlock()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = mgr.GetMember(uint64(i%100) + 1)
	}
}

// BenchmarkGetMemberParallel measures concurrent member lookups.
func BenchmarkGetMemberParallel(b *testing.B) {
	mgr := setupBenchManager(b, 1)
	defer mgr.Close()

	mgr.membersMu.Lock()
	for i := uint64(1); i <= 100; i++ {
		mgr.members[i] = Member{
			NodeID:   i,
			Address:  fmt.Sprintf("localhost:%d", 9000+i),
			LastSeen: time.Now(),
		}
	}
	mgr.membersMu.Unlock()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		var j uint64
		for pb.Next() {
			j++
			_, _ = mgr.GetMember(j%100 + 1)
		}
	})
}

// BenchmarkGetMembers measures the cost of snapshotting the full member list.
func BenchmarkGetMembers(b *testing.B) {
	memberCounts := []int{10, 100, 1000}

	for _, count := range memberCounts {
		b.Run(fmt.Sprintf("Members-%d", count), func(b *testing.B) {
			mgr := setupBenchManager(b, 1)
			defer mgr.Close()

			mgr.membersMu.Lock()
			for i := uint64(1); i <= uint64(count); i++ {
				mgr.members[i] = Member{
					NodeID:   i,
					Address:  fmt.Sprintf("localhost:%d", 9000+i),
					LastSeen: time.Now(),
				}
			}
			mgr.membersMu.Unlock()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_ = mgr.GetMembers()
			}
		})
	}
}

// ---------------------------------------------------------------------------
// KeepAlive (lease renewal) benchmarks
// ---------------------------------------------------------------------------

// BenchmarkKeepAliveRenewal benchmarks the critical path of a single lease
// renewal: JSON marshal + storage Set. This isolates the per-tick cost
// without waiting for a real ticker.
func BenchmarkKeepAliveRenewal(b *testing.B) {
	mgr := setupBenchManager(b, 1)
	defer mgr.Close()

	key := fmt.Sprintf("%s%d", MembershipPrefix, mgr.self.NodeID)
	keyBytes := []byte(key)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		mgr.self.LastSeen = time.Now()
		data, err := json.Marshal(mgr.self)
		if err != nil {
			b.Fatalf("json.Marshal: %v", err)
		}
		if err := mgr.store.Set(keyBytes, data); err != nil {
			b.Fatalf("store.Set: %v", err)
		}
	}
}

// BenchmarkKeepAliveRenewalParallel benchmarks concurrent lease renewal writes
// from multiple goroutines (simulates multi-node contention on storage).
func BenchmarkKeepAliveRenewalParallel(b *testing.B) {
	mgr := setupBenchManager(b, 1)
	defer mgr.Close()

	key := fmt.Sprintf("%s%d", MembershipPrefix, mgr.self.NodeID)
	keyBytes := []byte(key)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		selfCopy := mgr.self
		for pb.Next() {
			selfCopy.LastSeen = time.Now()
			data, err := json.Marshal(selfCopy)
			if err != nil {
				b.Fatalf("json.Marshal: %v", err)
			}
			if err := mgr.store.Set(keyBytes, data); err != nil {
				b.Fatalf("store.Set: %v", err)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Member add / remove (watch event handling)
// ---------------------------------------------------------------------------

// BenchmarkHandleMemberAdded benchmarks the hot path of adding a new member
// via the watch event handler.
func BenchmarkHandleMemberAdded(b *testing.B) {
	mgr := setupBenchManager(b, 1)
	defer mgr.Close()

	members := make([]Member, b.N)
	for i := range members {
		members[i] = Member{
			NodeID:       uint64(i + 2),
			Address:      fmt.Sprintf("localhost:%d", 10000+i),
			RegisteredAt: time.Now(),
			LastSeen:     time.Now(),
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		mgr.handleMemberAdded(members[i])
	}
}

// BenchmarkHandleMemberRemoved benchmarks the removal path.
func BenchmarkHandleMemberRemoved(b *testing.B) {
	mgr := setupBenchManager(b, 1)
	defer mgr.Close()

	// Pre-populate members.
	mgr.membersMu.Lock()
	for i := uint64(2); i <= uint64(b.N+1); i++ {
		mgr.members[i] = Member{
			NodeID:   i,
			Address:  fmt.Sprintf("localhost:%d", 10000+i),
			LastSeen: time.Now(),
		}
	}
	mgr.membersMu.Unlock()

	members := make([]Member, b.N)
	for i := range members {
		members[i] = Member{NodeID: uint64(i + 2)}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		mgr.handleMemberRemoved(members[i])
	}
}

// BenchmarkHandleWatchEvent benchmarks end-to-end watch event deserialization
// and member addition.
func BenchmarkHandleWatchEvent(b *testing.B) {
	mgr := setupBenchManager(b, 1)
	defer mgr.Close()

	// Pre-serialize a member for each iteration.
	events := make([]watch.Event, b.N)
	for i := range events {
		member := Member{
			NodeID:       uint64(i + 2),
			Address:      fmt.Sprintf("localhost:%d", 10000+i),
			RegisteredAt: time.Now(),
			LastSeen:     time.Now(),
		}
		data, err := json.Marshal(member)
		if err != nil {
			b.Fatalf("json.Marshal: %v", err)
		}
		events[i] = watch.Event{
			Type:  watch.EventTypePut,
			Key:   []byte(fmt.Sprintf("%s%d", MembershipPrefix, member.NodeID)),
			Value: data,
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		mgr.handleWatchEvent(events[i])
	}
}

// ---------------------------------------------------------------------------
// Health check benchmarks
// ---------------------------------------------------------------------------

// BenchmarkPerformHealthCheck benchmarks the health check sweep across the
// members map.
func BenchmarkPerformHealthCheck(b *testing.B) {
	memberCounts := []int{10, 100, 1000}

	for _, count := range memberCounts {
		b.Run(fmt.Sprintf("Members-%d", count), func(b *testing.B) {
			mgr := setupBenchManager(b, 1)
			defer mgr.Close()

			now := time.Now()
			mgr.membersMu.Lock()
			for i := uint64(2); i <= uint64(count+1); i++ {
				mgr.members[i] = Member{
					NodeID:   i,
					Address:  fmt.Sprintf("localhost:%d", 9000+i),
					LastSeen: now, // all healthy
				}
			}
			mgr.membersMu.Unlock()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				mgr.performHealthCheck()
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Concurrent member access benchmarks (RWMutex contention)
// ---------------------------------------------------------------------------

// BenchmarkConcurrentReadWrite measures mixed read/write contention on the
// members map. 80% reads (GetMember) and 20% writes (handleMemberAdded).
func BenchmarkConcurrentReadWrite(b *testing.B) {
	mgr := setupBenchManager(b, 1)
	defer mgr.Close()

	// Seed with some members so reads can hit.
	mgr.membersMu.Lock()
	for i := uint64(1); i <= 100; i++ {
		mgr.members[i] = Member{
			NodeID:   i,
			Address:  fmt.Sprintf("localhost:%d", 9000+i),
			LastSeen: time.Now(),
		}
	}
	mgr.membersMu.Unlock()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		var j uint64
		for pb.Next() {
			j++
			if j%5 == 0 {
				// 20% writes
				mgr.handleMemberAdded(Member{
					NodeID:   j%100 + 1,
					Address:  fmt.Sprintf("localhost:%d", 9000+j%100+1),
					LastSeen: time.Now(),
				})
			} else {
				// 80% reads
				_, _ = mgr.GetMember(j%100 + 1)
			}
		}
	})
}

// BenchmarkConcurrentReadHeavy measures read-heavy contention (95% reads,
// 5% writes).
func BenchmarkConcurrentReadHeavy(b *testing.B) {
	mgr := setupBenchManager(b, 1)
	defer mgr.Close()

	mgr.membersMu.Lock()
	for i := uint64(1); i <= 100; i++ {
		mgr.members[i] = Member{
			NodeID:   i,
			Address:  fmt.Sprintf("localhost:%d", 9000+i),
			LastSeen: time.Now(),
		}
	}
	mgr.membersMu.Unlock()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		var j uint64
		for pb.Next() {
			j++
			if j%20 == 0 {
				mgr.handleMemberAdded(Member{
					NodeID:   j%100 + 1,
					Address:  fmt.Sprintf("localhost:%d", 9000+j%100+1),
					LastSeen: time.Now(),
				})
			} else {
				_, _ = mgr.GetMember(j%100 + 1)
			}
		}
	})
}

// BenchmarkConcurrentWriteHeavy measures write-heavy contention (80% writes,
// 20% reads).
func BenchmarkConcurrentWriteHeavy(b *testing.B) {
	mgr := setupBenchManager(b, 1)
	defer mgr.Close()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		var j uint64
		for pb.Next() {
			j++
			if j%5 != 0 {
				mgr.handleMemberAdded(Member{
					NodeID:   j%100 + 1,
					Address:  fmt.Sprintf("localhost:%d", 9000+j%100+1),
					LastSeen: time.Now(),
				})
			} else {
				_, _ = mgr.GetMember(j%100 + 1)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// NewManager / Close lifecycle benchmarks
// ---------------------------------------------------------------------------

// BenchmarkNewManager measures the cost of creating a new Manager.
func BenchmarkNewManager(b *testing.B) {
	store := newBenchStorage()

	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		b.Fatalf("logger.New: %v", err)
	}

	watchMgr := watch.NewManager()

	peer := discovery.Peer{
		NodeID:   1,
		Address:  "localhost:9001",
		RaftAddr: "localhost:9001",
		Metadata: map[string]string{"region": "us-east-1"},
	}

	config := DefaultConfig()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		mgr, err := NewManager(store, watchMgr, peer, config, log)
		if err != nil {
			b.Fatalf("NewManager: %v", err)
		}
		mgr.Close()
	}
}

// BenchmarkNewManagerWithRaft measures the cost of creating a Manager with
// Raft integration enabled.
func BenchmarkNewManagerWithRaft(b *testing.B) {
	store := newBenchStorage()

	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		b.Fatalf("logger.New: %v", err)
	}

	watchMgr := watch.NewManager()

	peer := discovery.Peer{
		NodeID:   1,
		Address:  "localhost:9001",
		RaftAddr: "localhost:9001",
	}

	config := DefaultConfig()
	raftNode := newMockRaftNode(1)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		mgr, err := NewManager(store, watchMgr, peer, config, log, WithRaftIntegration(raftNode, 100))
		if err != nil {
			b.Fatalf("NewManager: %v", err)
		}
		mgr.Close()
	}
}

// ---------------------------------------------------------------------------
// IsLeader benchmarks
// ---------------------------------------------------------------------------

// BenchmarkIsLeader measures the cost of the leader check path.
func BenchmarkIsLeader(b *testing.B) {
	raftNode := newMockRaftNode(1)
	mgr := setupBenchManager(b, 1, WithRaftIntegration(raftNode, 100))
	defer mgr.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = mgr.isLeader()
	}
}

// BenchmarkIsLeaderParallel measures concurrent leader check contention.
func BenchmarkIsLeaderParallel(b *testing.B) {
	raftNode := newMockRaftNode(1)
	mgr := setupBenchManager(b, 1, WithRaftIntegration(raftNode, 100))
	defer mgr.Close()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = mgr.isLeader()
		}
	})
}

// ---------------------------------------------------------------------------
// Member JSON serialization benchmarks
// ---------------------------------------------------------------------------

// BenchmarkMemberMarshal measures JSON marshaling cost for a Member struct.
func BenchmarkMemberMarshal(b *testing.B) {
	member := Member{
		NodeID:       42,
		Address:      "localhost:9042",
		RaftAddr:     "localhost:9042",
		RegisteredAt: time.Now(),
		LastSeen:     time.Now(),
		Metadata:     map[string]string{"region": "us-east-1", "zone": "a"},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(member)
		if err != nil {
			b.Fatalf("json.Marshal: %v", err)
		}
	}
}

// BenchmarkMemberUnmarshal measures JSON unmarshaling cost for a Member struct.
func BenchmarkMemberUnmarshal(b *testing.B) {
	member := Member{
		NodeID:       42,
		Address:      "localhost:9042",
		RaftAddr:     "localhost:9042",
		RegisteredAt: time.Now(),
		LastSeen:     time.Now(),
		Metadata:     map[string]string{"region": "us-east-1", "zone": "a"},
	}

	data, err := json.Marshal(member)
	if err != nil {
		b.Fatalf("json.Marshal: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var m Member
		if err := json.Unmarshal(data, &m); err != nil {
			b.Fatalf("json.Unmarshal: %v", err)
		}
	}
}
