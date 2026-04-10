//go:build iam

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

package iam

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestPolicyChangeType tests PolicyChangeType string conversion.
func TestPolicyChangeType(t *testing.T) {
	tests := []struct {
		name     string
		pct      PolicyChangeType
		expected string
	}{
		{"Created", PolicyChangeCreated, "created"},
		{"Updated", PolicyChangeUpdated, "updated"},
		{"Deleted", PolicyChangeDeleted, "deleted"},
		{"Snapshot", PolicyChangeSnapshot, "snapshot"},
		{"Unknown", PolicyChangeType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pct.String(); got != tt.expected {
				t.Errorf("PolicyChangeType.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestPolicyChangeEventMarshal tests PolicyChangeEvent binary serialization.
func TestPolicyChangeEventMarshal(t *testing.T) {
	event := PolicyChangeEvent{
		Type:      PolicyChangeCreated,
		PolicyID:  "policy-123",
		TenantID:  "tenant-abc",
		Version:   42,
		Timestamp: time.Now().UTC().Truncate(time.Second),
		Data:      []byte(`{"name":"test-policy"}`),
		NodeID:    "node-1",
	}

	// Marshal
	data, err := event.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}

	// Unmarshal
	var decoded PolicyChangeEvent
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary failed: %v", err)
	}

	// Verify fields
	if decoded.Type != event.Type {
		t.Errorf("Type mismatch: got %v, want %v", decoded.Type, event.Type)
	}
	if decoded.PolicyID != event.PolicyID {
		t.Errorf("PolicyID mismatch: got %v, want %v", decoded.PolicyID, event.PolicyID)
	}
	if decoded.TenantID != event.TenantID {
		t.Errorf("TenantID mismatch: got %v, want %v", decoded.TenantID, event.TenantID)
	}
	if decoded.Version != event.Version {
		t.Errorf("Version mismatch: got %v, want %v", decoded.Version, event.Version)
	}
	if decoded.NodeID != event.NodeID {
		t.Errorf("NodeID mismatch: got %v, want %v", decoded.NodeID, event.NodeID)
	}
}

// TestPolicyChangeEventUnmarshalInvalid tests unmarshaling invalid data.
func TestPolicyChangeEventUnmarshalInvalid(t *testing.T) {
	var event PolicyChangeEvent
	err := event.UnmarshalBinary([]byte("not valid json"))
	if err == nil {
		t.Error("UnmarshalBinary should fail with invalid JSON")
	}
}

// TestCacheSyncConfig tests configuration validation.
func TestCacheSyncConfig(t *testing.T) {
	t.Run("DefaultConfig", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")

		if config.NodeID != "node-1" {
			t.Errorf("NodeID = %v, want node-1", config.NodeID)
		}
		if config.TenantID != "tenant-1" {
			t.Errorf("TenantID = %v, want tenant-1", config.TenantID)
		}
		if config.SyncInterval != 30*time.Second {
			t.Errorf("SyncInterval = %v, want 30s", config.SyncInterval)
		}
		if config.RetryInterval != 5*time.Second {
			t.Errorf("RetryInterval = %v, want 5s", config.RetryInterval)
		}
		if config.MaxRetries != 3 {
			t.Errorf("MaxRetries = %v, want 3", config.MaxRetries)
		}
		if config.BufferSize != 1000 {
			t.Errorf("BufferSize = %v, want 1000", config.BufferSize)
		}
		if !config.EnableMetrics {
			t.Error("EnableMetrics should be true")
		}
	})

	t.Run("ValidConfig", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		if err := config.Validate(); err != nil {
			t.Errorf("Valid config should pass validation: %v", err)
		}
	})

	t.Run("MissingNodeID", func(t *testing.T) {
		config := DefaultCacheSyncConfig("", "tenant-1")
		err := config.Validate()
		if err == nil {
			t.Error("Config with empty NodeID should fail validation")
		}
		var syncErr *InvalidSyncConfigError
		if !errors.As(err, &syncErr) {
			t.Errorf("Expected InvalidSyncConfigError, got %T", err)
		}
	})

	t.Run("MissingTenantID", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "")
		err := config.Validate()
		if err == nil {
			t.Error("Config with empty TenantID should fail validation")
		}
	})

	t.Run("ZeroSyncInterval", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		config.SyncInterval = 0
		err := config.Validate()
		if err == nil {
			t.Error("Config with zero SyncInterval should fail validation")
		}
	})

	t.Run("NegativeSyncInterval", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		config.SyncInterval = -1 * time.Second
		err := config.Validate()
		if err == nil {
			t.Error("Config with negative SyncInterval should fail validation")
		}
	})

	t.Run("ZeroRetryInterval", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		config.RetryInterval = 0
		err := config.Validate()
		if err == nil {
			t.Error("Config with zero RetryInterval should fail validation")
		}
	})

	t.Run("NegativeMaxRetries", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		config.MaxRetries = -1
		err := config.Validate()
		if err == nil {
			t.Error("Config with negative MaxRetries should fail validation")
		}
	})

	t.Run("ZeroBufferSize", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		config.BufferSize = 0
		err := config.Validate()
		if err == nil {
			t.Error("Config with zero BufferSize should fail validation")
		}
	})
}

// TestInvalidSyncConfigError tests the error type.
func TestInvalidSyncConfigError(t *testing.T) {
	err := &InvalidSyncConfigError{Reason: "test reason"}
	expected := "invalid sync configuration: test reason"
	if err.Error() != expected {
		t.Errorf("Error() = %v, want %v", err.Error(), expected)
	}
}

// TestErrSyncFailed tests the sync failed error type.
func TestErrSyncFailed(t *testing.T) {
	t.Run("WithCause", func(t *testing.T) {
		cause := errors.New("underlying error")
		err := &SyncFailedError{
			Operation: "test operation",
			Cause:     cause,
		}
		expected := "sync failed during test operation: underlying error"
		if err.Error() != expected {
			t.Errorf("Error() = %v, want %v", err.Error(), expected)
		}
		if !errors.Is(err, cause) {
			t.Error("Unwrap should return the cause")
		}
	})

	t.Run("WithoutCause", func(t *testing.T) {
		err := &SyncFailedError{
			Operation: "test operation",
		}
		expected := "sync failed during test operation"
		if err.Error() != expected {
			t.Errorf("Error() = %v, want %v", err.Error(), expected)
		}
	})
}

// TestCacheVersionTracker tests the version tracker.
func TestCacheVersionTracker(t *testing.T) {
	t.Run("InitialVersion", func(t *testing.T) {
		tracker := NewCacheVersionTracker()
		if tracker.GetVersion() != 0 {
			t.Errorf("Initial version should be 0, got %d", tracker.GetVersion())
		}
	})

	t.Run("IncrementVersion", func(t *testing.T) {
		tracker := NewCacheVersionTracker()
		v1 := tracker.IncrementVersion()
		if v1 != 1 {
			t.Errorf("First increment should return 1, got %d", v1)
		}
		v2 := tracker.IncrementVersion()
		if v2 != 2 {
			t.Errorf("Second increment should return 2, got %d", v2)
		}
	})

	t.Run("TenantVersion", func(t *testing.T) {
		tracker := NewCacheVersionTracker()

		// Initial tenant version should be 0
		if tracker.GetTenantVersion("tenant-1") != 0 {
			t.Error("Initial tenant version should be 0")
		}

		// Set tenant version
		tracker.SetTenantVersion("tenant-1", 10)
		if tracker.GetTenantVersion("tenant-1") != 10 {
			t.Errorf("Tenant version should be 10, got %d", tracker.GetTenantVersion("tenant-1"))
		}

		// Global version should be updated
		if tracker.GetVersion() < 10 {
			t.Errorf("Global version should be at least 10, got %d", tracker.GetVersion())
		}

		// Other tenant should still be 0
		if tracker.GetTenantVersion("tenant-2") != 0 {
			t.Error("Other tenant version should be 0")
		}
	})

	t.Run("InvalidationCallback", func(t *testing.T) {
		tracker := NewCacheVersionTracker()

		var called atomic.Bool
		var capturedTenant, capturedPolicy string
		var capturedVersion int64

		tracker.RegisterInvalidationCallback(func(tenantID, policyID string, version int64) {
			called.Store(true)
			capturedTenant = tenantID
			capturedPolicy = policyID
			capturedVersion = version
		})

		tracker.NotifyInvalidation("tenant-1", "policy-1", 42)

		if !called.Load() {
			t.Error("Callback should have been called")
		}
		if capturedTenant != "tenant-1" {
			t.Errorf("Tenant = %v, want tenant-1", capturedTenant)
		}
		if capturedPolicy != "policy-1" {
			t.Errorf("Policy = %v, want policy-1", capturedPolicy)
		}
		if capturedVersion != 42 {
			t.Errorf("Version = %v, want 42", capturedVersion)
		}
	})

	t.Run("MultipleCallbacks", func(t *testing.T) {
		tracker := NewCacheVersionTracker()

		var count atomic.Int32

		tracker.RegisterInvalidationCallback(func(tenantID, policyID string, version int64) {
			count.Add(1)
		})
		tracker.RegisterInvalidationCallback(func(tenantID, policyID string, version int64) {
			count.Add(1)
		})

		tracker.NotifyInvalidation("tenant-1", "policy-1", 1)

		if count.Load() != 2 {
			t.Errorf("Both callbacks should be called, count = %d", count.Load())
		}
	})

	t.Run("ClearCallbacks", func(t *testing.T) {
		tracker := NewCacheVersionTracker()

		var called atomic.Bool
		tracker.RegisterInvalidationCallback(func(tenantID, policyID string, version int64) {
			called.Store(true)
		})

		tracker.ClearCallbacks()
		tracker.NotifyInvalidation("tenant-1", "policy-1", 1)

		if called.Load() {
			t.Error("Callback should not be called after clear")
		}
	})

	t.Run("ConcurrentUpdates", func(t *testing.T) {
		tracker := NewCacheVersionTracker()

		var wg sync.WaitGroup
		const numGoroutines = 100
		const numUpdates = 100

		wg.Add(numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				defer wg.Done()
				for j := 0; j < numUpdates; j++ {
					tracker.IncrementVersion()
					tracker.SetTenantVersion("tenant-1", int64(id*numUpdates+j))
				}
			}(i)
		}

		wg.Wait()

		// Version should be at least numGoroutines * numUpdates
		if tracker.GetVersion() < numGoroutines*numUpdates {
			t.Errorf("Version should be at least %d, got %d", numGoroutines*numUpdates, tracker.GetVersion())
		}
	})
}

// mockRaftProposer implements RaftProposer for testing.
type mockRaftProposer struct {
	proposeFunc   func(ctx context.Context, shardID uint64, data []byte) error
	readFunc      func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error)
	isLeaderFunc  func(shardID uint64) bool
	getLeaderFunc func(shardID uint64) (uint64, bool)
}

func (m *mockRaftProposer) Propose(ctx context.Context, shardID uint64, data []byte) error {
	if m.proposeFunc != nil {
		return m.proposeFunc(ctx, shardID, data)
	}
	return nil
}

func (m *mockRaftProposer) Read(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
	if m.readFunc != nil {
		return m.readFunc(ctx, shardID, query)
	}
	return nil, nil
}

func (m *mockRaftProposer) IsLeader(shardID uint64) bool {
	if m.isLeaderFunc != nil {
		return m.isLeaderFunc(shardID)
	}
	return false
}

func (m *mockRaftProposer) GetLeaderID(shardID uint64) (uint64, bool) {
	if m.getLeaderFunc != nil {
		return m.getLeaderFunc(shardID)
	}
	return 0, false
}

// mockCacheSyncInterface implements CacheSyncInterface for testing.
type mockCacheSyncInterface struct {
	mu           sync.RWMutex
	version      int64
	policies     map[string]map[string]*CompiledPolicy // tenant -> name -> policy
	invalidated  []string
	allCleared   []string
	setCount     atomic.Int32
	invalidCount atomic.Int32
}

func newMockCache() *mockCacheSyncInterface {
	return &mockCacheSyncInterface{
		policies: make(map[string]map[string]*CompiledPolicy),
	}
}

func (m *mockCacheSyncInterface) GetVersion() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.version
}

func (m *mockCacheSyncInterface) Set(tenantID string, policy *CompiledPolicy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.policies[tenantID] == nil {
		m.policies[tenantID] = make(map[string]*CompiledPolicy)
	}
	m.policies[tenantID][policy.Name] = policy
	m.version++
	m.setCount.Add(1)
}

func (m *mockCacheSyncInterface) Invalidate(tenantID, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.policies[tenantID] != nil {
		delete(m.policies[tenantID], name)
	}
	m.invalidated = append(m.invalidated, tenantID+":"+name)
	m.version++
	m.invalidCount.Add(1)
}

func (m *mockCacheSyncInterface) InvalidateAll(tenantID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.policies, tenantID)
	m.allCleared = append(m.allCleared, tenantID)
	m.version++
}

func (m *mockCacheSyncInterface) GetAll(tenantID string) []*CompiledPolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*CompiledPolicy
	if tenantPolicies, ok := m.policies[tenantID]; ok {
		for _, p := range tenantPolicies {
			result = append(result, p)
		}
	}
	return result
}

func (m *mockCacheSyncInterface) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, tenantPolicies := range m.policies {
		count += len(tenantPolicies)
	}
	return count
}

func (m *mockCacheSyncInterface) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.policies = make(map[string]map[string]*CompiledPolicy)
	m.version++
}

// TestNewRaftCacheSyncer tests creating a new RaftCacheSyncer.
func TestNewRaftCacheSyncer(t *testing.T) {
	t.Run("ValidParams", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		defer syncer.Close()

		if syncer == nil {
			t.Fatal("Syncer should not be nil")
		}
	})

	t.Run("NilConfig", func(t *testing.T) {
		raft := &mockRaftProposer{}
		cache := newMockCache()

		_, err := NewRaftCacheSyncer(nil, raft, cache)
		if err == nil {
			t.Error("Should fail with nil config")
		}
	})

	t.Run("InvalidConfig", func(t *testing.T) {
		config := &CacheSyncConfig{} // Invalid - missing required fields
		raft := &mockRaftProposer{}
		cache := newMockCache()

		_, err := NewRaftCacheSyncer(config, raft, cache)
		if err == nil {
			t.Error("Should fail with invalid config")
		}
	})

	t.Run("NilRaft", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		cache := newMockCache()

		_, err := NewRaftCacheSyncer(config, nil, cache)
		if err == nil {
			t.Error("Should fail with nil raft")
		}
	})

	t.Run("NilCache", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{}

		_, err := NewRaftCacheSyncer(config, raft, nil)
		if err == nil {
			t.Error("Should fail with nil cache")
		}
	})

	t.Run("WithOptions", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{}
		cache := newMockCache()
		compiler := NewPolicyCompiler()
		tracker := NewCacheVersionTracker()

		syncer, err := NewRaftCacheSyncer(
			config, raft, cache,
			WithShardID(42),
			WithCompiler(compiler),
			WithVersionTracker(tracker),
		)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		defer syncer.Close()

		if syncer.shardID != 42 {
			t.Errorf("ShardID = %d, want 42", syncer.shardID)
		}
	})
}

// TestRaftCacheSyncerNotifyPolicyChange tests policy change notification.
func TestRaftCacheSyncerNotifyPolicyChange(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		var proposedData []byte
		raft := &mockRaftProposer{
			proposeFunc: func(ctx context.Context, shardID uint64, data []byte) error {
				proposedData = data
				return nil
			},
		}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		defer syncer.Close()

		event := PolicyChangeEvent{
			Type:     PolicyChangeCreated,
			PolicyID: "policy-1",
			TenantID: "tenant-1",
			Version:  1,
			Data:     []byte(`{}`),
		}

		err = syncer.NotifyPolicyChange(context.Background(), event)
		if err != nil {
			t.Fatalf("NotifyPolicyChange failed: %v", err)
		}

		if proposedData == nil {
			t.Error("Raft propose should have been called")
		}
	})

	t.Run("ProposeError", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		config.MaxRetries = 0 // No retries
		raft := &mockRaftProposer{
			proposeFunc: func(ctx context.Context, shardID uint64, data []byte) error {
				return errors.New("propose failed")
			},
		}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		defer syncer.Close()

		event := PolicyChangeEvent{
			Type:     PolicyChangeCreated,
			PolicyID: "policy-1",
			TenantID: "tenant-1",
		}

		err = syncer.NotifyPolicyChange(context.Background(), event)
		if err == nil {
			t.Error("NotifyPolicyChange should fail when propose fails")
		}

		var syncErr *SyncFailedError
		if !errors.As(err, &syncErr) {
			t.Errorf("Expected ErrSyncFailed, got %T", err)
		}
	})

	t.Run("ClosedSyncer", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		syncer.Close()

		event := PolicyChangeEvent{
			Type:     PolicyChangeCreated,
			PolicyID: "policy-1",
		}

		err = syncer.NotifyPolicyChange(context.Background(), event)
		if !errors.Is(err, ErrSyncerClosed) {
			t.Errorf("Expected ErrSyncerClosed, got %v", err)
		}
	})

	t.Run("ContextCancelled", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		config.MaxRetries = 5
		config.RetryInterval = 100 * time.Millisecond

		attemptCount := 0
		raft := &mockRaftProposer{
			proposeFunc: func(ctx context.Context, shardID uint64, data []byte) error {
				attemptCount++
				return errors.New("always fail")
			},
		}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		defer syncer.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()

		event := PolicyChangeEvent{
			Type:     PolicyChangeCreated,
			PolicyID: "policy-1",
		}

		err = syncer.NotifyPolicyChange(ctx, event)
		if err == nil {
			t.Error("NotifyPolicyChange should fail when context cancelled")
		}
	})
}

// TestRaftCacheSyncerSubscribe tests subscription functionality.
func TestRaftCacheSyncerSubscribe(t *testing.T) {
	t.Run("SuccessfulSubscribe", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{
			isLeaderFunc: func(shardID uint64) bool { return true },
		}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		defer syncer.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		handlerCalled := make(chan struct{})
		handler := func(ctx context.Context, event PolicyChangeEvent) error {
			close(handlerCalled)
			return nil
		}

		err = syncer.Subscribe(ctx, handler)
		if err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}

		status := syncer.GetSyncStatus()
		if !status.IsSubscribed {
			t.Error("Should be subscribed")
		}
	})

	t.Run("NilHandler", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		defer syncer.Close()

		err = syncer.Subscribe(context.Background(), nil)
		if err == nil {
			t.Error("Subscribe should fail with nil handler")
		}
	})

	t.Run("AlreadySubscribed", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{
			isLeaderFunc: func(shardID uint64) bool { return true },
		}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		defer syncer.Close()

		ctx := context.Background()
		handler := func(ctx context.Context, event PolicyChangeEvent) error { return nil }

		err = syncer.Subscribe(ctx, handler)
		if err != nil {
			t.Fatalf("First subscribe failed: %v", err)
		}

		err = syncer.Subscribe(ctx, handler)
		if !errors.Is(err, ErrAlreadySubscribed) {
			t.Errorf("Expected ErrAlreadySubscribed, got %v", err)
		}
	})

	t.Run("ClosedSyncer", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		syncer.Close()

		handler := func(ctx context.Context, event PolicyChangeEvent) error { return nil }
		err = syncer.Subscribe(context.Background(), handler)
		if !errors.Is(err, ErrSyncerClosed) {
			t.Errorf("Expected ErrSyncerClosed, got %v", err)
		}
	})
}

// TestRaftCacheSyncerUnsubscribe tests unsubscription functionality.
func TestRaftCacheSyncerUnsubscribe(t *testing.T) {
	t.Run("SuccessfulUnsubscribe", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{
			isLeaderFunc: func(shardID uint64) bool { return true },
		}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		defer syncer.Close()

		ctx := context.Background()
		handler := func(ctx context.Context, event PolicyChangeEvent) error { return nil }

		err = syncer.Subscribe(ctx, handler)
		if err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}

		err = syncer.Unsubscribe(ctx)
		if err != nil {
			t.Fatalf("Unsubscribe failed: %v", err)
		}

		status := syncer.GetSyncStatus()
		if status.IsSubscribed {
			t.Error("Should not be subscribed")
		}
	})

	t.Run("NotSubscribed", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		defer syncer.Close()

		err = syncer.Unsubscribe(context.Background())
		if !errors.Is(err, ErrNotSubscribed) {
			t.Errorf("Expected ErrNotSubscribed, got %v", err)
		}
	})

	t.Run("ClosedSyncer", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		syncer.Close()

		err = syncer.Unsubscribe(context.Background())
		if !errors.Is(err, ErrSyncerClosed) {
			t.Errorf("Expected ErrSyncerClosed, got %v", err)
		}
	})
}

// TestRaftCacheSyncerSyncFromLeader tests syncing from the leader.
func TestRaftCacheSyncerSyncFromLeader(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")

		// Create a test policy
		policy := &Policy{
			APIVersion: "iam.quicbaas.io/v1",
			Kind:       "Policy",
			Metadata: PolicyMetadata{
				Name:   "test-policy",
				Tenant: "tenant-1",
			},
			Spec: PolicySpec{
				Rules: []Rule{
					{
						Name:   "rule1",
						Effect: EffectAllow,
						Actions: []string{
							"read",
						},
						Resources: []string{
							"*",
						},
					},
				},
			},
		}
		policyData, _ := json.Marshal(policy)

		raft := &mockRaftProposer{
			readFunc: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
				return &syncSnapshot{
					TenantID: "tenant-1",
					Version:  10,
					Policies: [][]byte{policyData},
				}, nil
			},
			getLeaderFunc: func(shardID uint64) (uint64, bool) {
				return 2, true
			},
		}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		defer syncer.Close()

		err = syncer.SyncFromLeader(context.Background())
		if err != nil {
			t.Fatalf("SyncFromLeader failed: %v", err)
		}

		// Verify cache was updated
		if cache.Size() != 1 {
			t.Errorf("Cache should have 1 policy, got %d", cache.Size())
		}

		status := syncer.GetSyncStatus()
		if status.LastSyncVersion != 10 {
			t.Errorf("LastSyncVersion = %d, want 10", status.LastSyncVersion)
		}
	})

	t.Run("ReadError", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{
			readFunc: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
				return nil, errors.New("read failed")
			},
		}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		defer syncer.Close()

		err = syncer.SyncFromLeader(context.Background())
		if err == nil {
			t.Error("SyncFromLeader should fail on read error")
		}
	})

	t.Run("InvalidResult", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{
			readFunc: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
				return "invalid type", nil
			},
		}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		defer syncer.Close()

		err = syncer.SyncFromLeader(context.Background())
		if err == nil {
			t.Error("SyncFromLeader should fail with invalid result type")
		}
	})

	t.Run("NilSnapshot", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{
			readFunc: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
				return (*syncSnapshot)(nil), nil
			},
		}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		defer syncer.Close()

		err = syncer.SyncFromLeader(context.Background())
		if err == nil {
			t.Error("SyncFromLeader should fail with nil snapshot")
		}
	})

	t.Run("ClosedSyncer", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		syncer.Close()

		err = syncer.SyncFromLeader(context.Background())
		if !errors.Is(err, ErrSyncerClosed) {
			t.Errorf("Expected ErrSyncerClosed, got %v", err)
		}
	})
}

// TestRaftCacheSyncerDeliverEvent tests event delivery.
func TestRaftCacheSyncerDeliverEvent(t *testing.T) {
	t.Run("DeliverWhileSubscribed", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		config.BufferSize = 10
		raft := &mockRaftProposer{
			isLeaderFunc: func(shardID uint64) bool { return true },
		}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		defer syncer.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		eventReceived := make(chan PolicyChangeEvent, 1)
		handler := func(ctx context.Context, event PolicyChangeEvent) error {
			select {
			case eventReceived <- event:
			default:
			}
			return nil
		}

		err = syncer.Subscribe(ctx, handler)
		if err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}

		// Create and deliver event
		policy := &Policy{
			APIVersion: "iam.quicbaas.io/v1",
			Kind:       "Policy",
			Metadata: PolicyMetadata{
				Name:   "test-policy",
				Tenant: "tenant-1",
			},
			Spec: PolicySpec{
				Rules: []Rule{
					{
						Name:      "rule1",
						Effect:    EffectAllow,
						Actions:   []string{"read"},
						Resources: []string{"*"},
					},
				},
			},
		}
		policyData, _ := json.Marshal(policy)

		event := PolicyChangeEvent{
			Type:     PolicyChangeCreated,
			PolicyID: "test-policy",
			TenantID: "tenant-1",
			Version:  1,
			Data:     policyData,
			NodeID:   "node-2", // Different node
		}

		syncer.DeliverEvent(event)

		// Wait for event processing with timeout
		select {
		case received := <-eventReceived:
			if received.PolicyID != event.PolicyID {
				t.Errorf("PolicyID = %v, want %v", received.PolicyID, event.PolicyID)
			}
		case <-time.After(1 * time.Second):
			t.Error("Timeout waiting for event")
		}
	})

	t.Run("SkipSameNodeEvents", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{
			isLeaderFunc: func(shardID uint64) bool { return true },
		}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		defer syncer.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		handlerCalled := false
		handler := func(ctx context.Context, event PolicyChangeEvent) error {
			handlerCalled = true
			return nil
		}

		err = syncer.Subscribe(ctx, handler)
		if err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}

		// Event from same node should be skipped
		event := PolicyChangeEvent{
			Type:     PolicyChangeCreated,
			PolicyID: "test-policy",
			TenantID: "tenant-1",
			NodeID:   "node-1", // Same node
		}

		syncer.DeliverEvent(event)

		time.Sleep(100 * time.Millisecond)

		if handlerCalled {
			t.Error("Handler should not be called for same-node events")
		}
	})

	t.Run("SkipDifferentTenantEvents", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{
			isLeaderFunc: func(shardID uint64) bool { return true },
		}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		defer syncer.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		handlerCalled := false
		handler := func(ctx context.Context, event PolicyChangeEvent) error {
			handlerCalled = true
			return nil
		}

		err = syncer.Subscribe(ctx, handler)
		if err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}

		// Event from different tenant should be skipped
		event := PolicyChangeEvent{
			Type:     PolicyChangeCreated,
			PolicyID: "test-policy",
			TenantID: "tenant-2", // Different tenant
			NodeID:   "node-2",
		}

		syncer.DeliverEvent(event)

		time.Sleep(100 * time.Millisecond)

		if handlerCalled {
			t.Error("Handler should not be called for different tenant events")
		}
	})

	t.Run("DeliverWhileClosed", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}
		syncer.Close()

		// Should not panic
		syncer.DeliverEvent(PolicyChangeEvent{})
	})
}

// TestRaftCacheSyncerClose tests closing the syncer.
func TestRaftCacheSyncerClose(t *testing.T) {
	t.Run("CloseOnce", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}

		err = syncer.Close()
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		// Second close should be no-op
		err = syncer.Close()
		if err != nil {
			t.Fatalf("Second close failed: %v", err)
		}
	})

	t.Run("CloseWhileSubscribed", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{
			isLeaderFunc: func(shardID uint64) bool { return true },
		}
		cache := newMockCache()

		syncer, err := NewRaftCacheSyncer(config, raft, cache)
		if err != nil {
			t.Fatalf("NewRaftCacheSyncer failed: %v", err)
		}

		ctx := context.Background()
		handler := func(ctx context.Context, event PolicyChangeEvent) error { return nil }

		err = syncer.Subscribe(ctx, handler)
		if err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}

		err = syncer.Close()
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		status := syncer.GetSyncStatus()
		if status.IsSubscribed {
			t.Error("Should not be subscribed after close")
		}
	})
}

// TestRaftCacheSyncerGetSyncStatus tests status retrieval.
func TestRaftCacheSyncerGetSyncStatus(t *testing.T) {
	config := DefaultCacheSyncConfig("node-1", "tenant-1")
	raft := &mockRaftProposer{}
	cache := newMockCache()

	syncer, err := NewRaftCacheSyncer(config, raft, cache)
	if err != nil {
		t.Fatalf("NewRaftCacheSyncer failed: %v", err)
	}
	defer syncer.Close()

	status := syncer.GetSyncStatus()
	if status == nil {
		t.Fatal("Status should not be nil")
	}

	if status.IsSubscribed {
		t.Error("Should not be subscribed initially")
	}

	// Note: EventsProcessed and SyncErrors use global metrics (shared across tests)
	// so we can only verify they are non-negative, not that they are zero.
	if status.EventsProcessed < 0 {
		t.Errorf("EventsProcessed = %d, should be non-negative", status.EventsProcessed)
	}

	if status.SyncErrors < 0 {
		t.Errorf("SyncErrors = %d, should be non-negative", status.SyncErrors)
	}

	// Verify lastSyncVersion is accessible
	_ = status.LastSyncVersion

	// Verify SyncLatencyNs is non-negative
	if status.SyncLatencyNs < 0 {
		t.Errorf("SyncLatencyNs = %d, should be non-negative", status.SyncLatencyNs)
	}
}

// TestRaftCacheSyncerEventProcessing tests different event types.
func TestRaftCacheSyncerEventProcessing(t *testing.T) {
	createSyncer := func() (*RaftCacheSyncer, *mockCacheSyncInterface) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		raft := &mockRaftProposer{
			isLeaderFunc: func(shardID uint64) bool { return true },
		}
		cache := newMockCache()

		syncer, _ := NewRaftCacheSyncer(config, raft, cache)
		return syncer, cache
	}

	t.Run("CreateEvent", func(t *testing.T) {
		syncer, cache := createSyncer()
		defer syncer.Close()

		policy := &Policy{
			APIVersion: "iam.quicbaas.io/v1",
			Kind:       "Policy",
			Metadata: PolicyMetadata{
				Name:   "test-policy",
				Tenant: "tenant-1",
			},
			Spec: PolicySpec{
				Rules: []Rule{{Name: "rule1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}},
			},
		}
		policyData, _ := json.Marshal(policy)

		event := PolicyChangeEvent{
			Type:     PolicyChangeCreated,
			PolicyID: "test-policy",
			TenantID: "tenant-1",
			Version:  1,
			Data:     policyData,
		}

		err := syncer.applyEventToCache(event)
		if err != nil {
			t.Fatalf("applyEventToCache failed: %v", err)
		}

		if cache.setCount.Load() != 1 {
			t.Errorf("Set should be called once, got %d", cache.setCount.Load())
		}
	})

	t.Run("UpdateEvent", func(t *testing.T) {
		syncer, cache := createSyncer()
		defer syncer.Close()

		policy := &Policy{
			APIVersion: "iam.quicbaas.io/v1",
			Kind:       "Policy",
			Metadata: PolicyMetadata{
				Name:   "test-policy",
				Tenant: "tenant-1",
			},
			Spec: PolicySpec{
				Rules: []Rule{{Name: "rule1", Effect: EffectDeny, Actions: []string{"write"}, Resources: []string{"*"}}},
			},
		}
		policyData, _ := json.Marshal(policy)

		event := PolicyChangeEvent{
			Type:     PolicyChangeUpdated,
			PolicyID: "test-policy",
			TenantID: "tenant-1",
			Version:  2,
			Data:     policyData,
		}

		err := syncer.applyEventToCache(event)
		if err != nil {
			t.Fatalf("applyEventToCache failed: %v", err)
		}

		if cache.setCount.Load() != 1 {
			t.Errorf("Set should be called once, got %d", cache.setCount.Load())
		}
	})

	t.Run("DeleteEvent", func(t *testing.T) {
		syncer, cache := createSyncer()
		defer syncer.Close()

		event := PolicyChangeEvent{
			Type:     PolicyChangeDeleted,
			PolicyID: "test-policy",
			TenantID: "tenant-1",
			Version:  3,
		}

		err := syncer.applyEventToCache(event)
		if err != nil {
			t.Fatalf("applyEventToCache failed: %v", err)
		}

		if cache.invalidCount.Load() != 1 {
			t.Errorf("Invalidate should be called once, got %d", cache.invalidCount.Load())
		}
	})

	t.Run("SnapshotEvent", func(t *testing.T) {
		syncer, cache := createSyncer()
		defer syncer.Close()

		event := PolicyChangeEvent{
			Type:     PolicyChangeSnapshot,
			TenantID: "tenant-1",
			Version:  100,
		}

		err := syncer.applyEventToCache(event)
		if err != nil {
			t.Fatalf("applyEventToCache failed: %v", err)
		}

		if len(cache.allCleared) != 1 {
			t.Errorf("InvalidateAll should be called once, got %d", len(cache.allCleared))
		}
	})

	t.Run("MissingDataForCreate", func(t *testing.T) {
		syncer, _ := createSyncer()
		defer syncer.Close()

		event := PolicyChangeEvent{
			Type:     PolicyChangeCreated,
			PolicyID: "test-policy",
			TenantID: "tenant-1",
			Version:  1,
			Data:     nil, // Missing data
		}

		err := syncer.applyEventToCache(event)
		if err == nil {
			t.Error("Should fail with missing data")
		}
	})

	t.Run("InvalidPolicyData", func(t *testing.T) {
		syncer, _ := createSyncer()
		defer syncer.Close()

		event := PolicyChangeEvent{
			Type:     PolicyChangeCreated,
			PolicyID: "test-policy",
			TenantID: "tenant-1",
			Version:  1,
			Data:     []byte("not valid json"),
		}

		err := syncer.applyEventToCache(event)
		if err == nil {
			t.Error("Should fail with invalid JSON")
		}
	})
}

// TestRetryBehavior tests retry behavior for failed operations.
func TestRetryBehavior(t *testing.T) {
	config := DefaultCacheSyncConfig("node-1", "tenant-1")
	config.MaxRetries = 3
	config.RetryInterval = 10 * time.Millisecond

	attemptCount := 0
	raft := &mockRaftProposer{
		proposeFunc: func(ctx context.Context, shardID uint64, data []byte) error {
			attemptCount++
			if attemptCount < 3 {
				return errors.New("temporary failure")
			}
			return nil // Success on 3rd attempt
		},
	}
	cache := newMockCache()

	syncer, err := NewRaftCacheSyncer(config, raft, cache)
	if err != nil {
		t.Fatalf("NewRaftCacheSyncer failed: %v", err)
	}
	defer syncer.Close()

	event := PolicyChangeEvent{
		Type:     PolicyChangeCreated,
		PolicyID: "test-policy",
	}

	err = syncer.NotifyPolicyChange(context.Background(), event)
	if err != nil {
		t.Errorf("NotifyPolicyChange should succeed after retries: %v", err)
	}

	if attemptCount != 3 {
		t.Errorf("Expected 3 attempts, got %d", attemptCount)
	}
}

// TestConcurrentOperations tests concurrent access to the syncer.
func TestConcurrentOperations(t *testing.T) {
	config := DefaultCacheSyncConfig("node-1", "tenant-1")
	config.BufferSize = 1000

	raft := &mockRaftProposer{
		proposeFunc: func(ctx context.Context, shardID uint64, data []byte) error {
			time.Sleep(1 * time.Millisecond)
			return nil
		},
		isLeaderFunc: func(shardID uint64) bool { return true },
	}
	cache := newMockCache()

	syncer, err := NewRaftCacheSyncer(config, raft, cache)
	if err != nil {
		t.Fatalf("NewRaftCacheSyncer failed: %v", err)
	}
	defer syncer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := func(ctx context.Context, event PolicyChangeEvent) error { return nil }
	if err := syncer.Subscribe(ctx, handler); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	var wg sync.WaitGroup
	const numGoroutines = 10
	const numOperations = 50

	// Concurrent notify operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				event := PolicyChangeEvent{
					Type:     PolicyChangeCreated,
					PolicyID: "policy-" + string(rune(id)) + "-" + string(rune(j)),
					TenantID: "tenant-1",
				}
				syncer.NotifyPolicyChange(ctx, event)
			}
		}(i)
	}

	// Concurrent deliver operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				event := PolicyChangeEvent{
					Type:     PolicyChangeCreated,
					PolicyID: "delivered-policy-" + string(rune(id)) + "-" + string(rune(j)),
					TenantID: "tenant-1",
					NodeID:   "node-2",
					Data:     []byte(`{"metadata":{"name":"test","tenant":"tenant-1"},"spec":{"rules":[{"name":"r1","effect":"allow"}]}}`),
				}
				syncer.DeliverEvent(event)
			}
		}(i)
	}

	// Concurrent status checks
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				syncer.GetSyncStatus()
			}
		}()
	}

	wg.Wait()
}
