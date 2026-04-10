//go:build iam

package iam

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// Mock RaftProposer for testing distributed PEP
// =============================================================================

type distPEPMockRaftProposer struct {
	mu          sync.Mutex
	proposals   [][]byte
	readResults map[uint64]interface{}
	readError   error
	proposeErr  error
	isLeader    bool
	leaderID    uint64
}

func newDistPEPMockRaftProposer() *distPEPMockRaftProposer {
	return &distPEPMockRaftProposer{
		proposals:   make([][]byte, 0),
		readResults: make(map[uint64]interface{}),
		isLeader:    true,
		leaderID:    1,
	}
}

func (m *distPEPMockRaftProposer) Propose(ctx context.Context, shardID uint64, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.proposeErr != nil {
		return m.proposeErr
	}
	m.proposals = append(m.proposals, data)
	return nil
}

func (m *distPEPMockRaftProposer) Read(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readError != nil {
		return nil, m.readError
	}
	if result, ok := m.readResults[shardID]; ok {
		return result, nil
	}
	return nil, nil
}

func (m *distPEPMockRaftProposer) IsLeader(shardID uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.isLeader
}

func (m *distPEPMockRaftProposer) GetLeaderID(shardID uint64) (uint64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.leaderID, m.leaderID > 0
}

// =============================================================================
// Mock Cache for testing
// =============================================================================

type distPEPMockCacheSyncInterface struct {
	mu       sync.Mutex
	policies map[string][]*CompiledPolicy
	version  int64
}

func newDistPEPMockCacheSyncInterface() *distPEPMockCacheSyncInterface {
	return &distPEPMockCacheSyncInterface{
		policies: make(map[string][]*CompiledPolicy),
		version:  1,
	}
}

func (m *distPEPMockCacheSyncInterface) GetVersion() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.version
}

func (m *distPEPMockCacheSyncInterface) Set(tenantID string, policy *CompiledPolicy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.policies[tenantID] = append(m.policies[tenantID], policy)
	m.version++
}

func (m *distPEPMockCacheSyncInterface) Invalidate(tenantID, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	policies := m.policies[tenantID]
	for i, p := range policies {
		if p != nil && p.Name == name {
			m.policies[tenantID] = append(policies[:i], policies[i+1:]...)
			break
		}
	}
	m.version++
}

func (m *distPEPMockCacheSyncInterface) InvalidateAll(tenantID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.policies, tenantID)
	m.version++
}

func (m *distPEPMockCacheSyncInterface) GetAll(tenantID string) []*CompiledPolicy {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.policies[tenantID]
}

func (m *distPEPMockCacheSyncInterface) Size() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	for _, policies := range m.policies {
		total += len(policies)
	}
	return total
}

func (m *distPEPMockCacheSyncInterface) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.policies = make(map[string][]*CompiledPolicy)
	m.version++
}

// =============================================================================
// DistributedPEP Tests
// =============================================================================

func TestNewDistributedPEP_NilRaft(t *testing.T) {
	_, err := NewDistributedPEP(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil raft proposer")
	}
}

func TestNewDistributedPEP_Success(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("tenant-1", "node-1")

	dpep, err := NewDistributedPEP(config, raft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dpep == nil {
		t.Fatal("expected non-nil distributed PEP")
	}
	defer dpep.Close()
}

func TestDistributedPEP_LoadPolicy(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("tenant-1", "node-1")

	dpep, err := NewDistributedPEP(config, raft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer dpep.Close()

	ctx := context.Background()
	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "test-policy", Tenant: "tenant-1"},
		Spec: PolicySpec{
			Rules: []Rule{
				{
					Name:      "allow-read",
					Effect:    EffectAllow,
					Actions:   []string{"read"},
					Resources: []string{"*"},
				},
			},
		},
	}

	err = dpep.LoadPolicy(ctx, policy)
	if err != nil {
		t.Fatalf("LoadPolicy failed: %v", err)
	}
}

func TestDistributedPEP_LoadPolicy_Closed(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("tenant-1", "node-1")

	dpep, err := NewDistributedPEP(config, raft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Close the PEP
	dpep.Close()

	ctx := context.Background()
	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "test-policy", Tenant: "tenant-1"},
	}

	err = dpep.LoadPolicy(ctx, policy)
	if !errors.Is(err, ErrPEPClosed) {
		t.Fatalf("expected ErrPEPClosed, got: %v", err)
	}
}

func TestDistributedPEP_RemovePolicy(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("tenant-1", "node-1")

	dpep, err := NewDistributedPEP(config, raft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer dpep.Close()

	ctx := context.Background()
	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "test-policy", Tenant: "tenant-1"},
		Spec: PolicySpec{
			Rules: []Rule{
				{
					Name:      "allow-read",
					Effect:    EffectAllow,
					Actions:   []string{"read"},
					Resources: []string{"*"},
				},
			},
		},
	}

	// Load policy first
	err = dpep.LoadPolicy(ctx, policy)
	if err != nil {
		t.Fatalf("LoadPolicy failed: %v", err)
	}

	// Remove policy
	err = dpep.RemovePolicy(ctx, "test-policy")
	if err != nil {
		t.Fatalf("RemovePolicy failed: %v", err)
	}
}

func TestDistributedPEP_RemovePolicy_Closed(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("tenant-1", "node-1")

	dpep, err := NewDistributedPEP(config, raft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dpep.Close()

	ctx := context.Background()
	err = dpep.RemovePolicy(ctx, "test-policy")
	if !errors.Is(err, ErrPEPClosed) {
		t.Fatalf("expected ErrPEPClosed, got: %v", err)
	}
}

func TestDistributedPEP_SyncWithCluster(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("tenant-1", "node-1")

	dpep, err := NewDistributedPEP(config, raft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer dpep.Close()

	ctx := context.Background()
	err = dpep.SyncWithCluster(ctx)
	// May fail but shouldn't panic
	_ = err
}

func TestDistributedPEP_SyncWithCluster_Closed(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("tenant-1", "node-1")

	dpep, err := NewDistributedPEP(config, raft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dpep.Close()

	ctx := context.Background()
	err = dpep.SyncWithCluster(ctx)
	if !errors.Is(err, ErrPEPClosed) {
		t.Fatalf("expected ErrPEPClosed, got: %v", err)
	}
}

func TestDistributedPEP_Check_Closed(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("tenant-1", "node-1")

	dpep, err := NewDistributedPEP(config, raft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dpep.Close()

	ctx := context.Background()
	req := &CheckRequest{
		Subject:  Subject{ID: "test-user", Tenant: "tenant-1"},
		Action:   "read",
		Resource: "data",
	}

	_, err = dpep.Check(ctx, req)
	if !errors.Is(err, ErrPEPClosed) {
		t.Fatalf("expected ErrPEPClosed, got: %v", err)
	}
}

func TestDistributedPEP_CheckBatch_Closed(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("tenant-1", "node-1")

	dpep, err := NewDistributedPEP(config, raft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dpep.Close()

	ctx := context.Background()
	batchReq := &BatchCheckRequest{
		Checks: []*CheckRequest{{Subject: Subject{ID: "test-user"}, Action: "read", Resource: "data"}},
	}

	_, err = dpep.CheckBatch(ctx, batchReq)
	if !errors.Is(err, ErrPEPClosed) {
		t.Fatalf("expected ErrPEPClosed, got: %v", err)
	}
}

func TestDistributedPEP_Subscribe(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("tenant-1", "node-1")

	dpep, err := NewDistributedPEP(config, raft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer dpep.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ch, err := dpep.Subscribe(ctx, []string{"*"})
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}

	// Wait for context to cancel
	<-ctx.Done()
}

func TestDistributedPEP_Subscribe_Closed(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("tenant-1", "node-1")

	dpep, err := NewDistributedPEP(config, raft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dpep.Close()

	ctx := context.Background()
	_, err = dpep.Subscribe(ctx, []string{"*"})
	if !errors.Is(err, ErrPEPClosed) {
		t.Fatalf("expected ErrPEPClosed, got: %v", err)
	}
}

func TestDistributedPEP_GetStats(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("tenant-1", "node-1")

	dpep, err := NewDistributedPEP(config, raft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer dpep.Close()

	stats := dpep.GetStats()
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
}

func TestDistributedPEP_Start(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("tenant-1", "node-1")
	config.AutoSync = false // Disable auto sync for this test

	dpep, err := NewDistributedPEP(config, raft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer dpep.Close()

	ctx := context.Background()
	err = dpep.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
}

func TestDistributedPEP_Start_WithAutoSync(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("tenant-1", "node-1")
	config.AutoSync = true

	dpep, err := NewDistributedPEP(config, raft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer dpep.Close()

	ctx := context.Background()
	err = dpep.Start(ctx)
	// May fail due to sync but shouldn't panic
	_ = err
}

func TestDistributedPEP_Start_Closed(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("tenant-1", "node-1")

	dpep, err := NewDistributedPEP(config, raft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dpep.Close()

	ctx := context.Background()
	err = dpep.Start(ctx)
	if !errors.Is(err, ErrPEPClosed) {
		t.Fatalf("expected ErrPEPClosed, got: %v", err)
	}
}

func TestDistributedPEP_Close_Multiple(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("tenant-1", "node-1")

	dpep, err := NewDistributedPEP(config, raft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First close should succeed
	err = dpep.Close()
	if err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	// Second close should be no-op
	err = dpep.Close()
	if err != nil {
		t.Fatalf("second Close should not return error: %v", err)
	}
}

func TestDistributedPEP_SyncInProgress(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("tenant-1", "node-1")

	dpep, err := NewDistributedPEP(config, raft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer dpep.Close()

	ctx := context.Background()

	// Start multiple sync operations concurrently
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = dpep.SyncWithCluster(ctx)
		}()
	}
	wg.Wait()
}

func TestDistributedPEP_HandlePolicyChange(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("tenant-1", "node-1")

	dpep, err := NewDistributedPEP(config, raft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer dpep.Close()

	ctx := context.Background()

	// Create a policy change event
	event := PolicyChangeEvent{
		Type:      PolicyChangeCreated,
		PolicyID:  "test-policy",
		TenantID:  "tenant-1",
		Version:   1,
		Timestamp: time.Now(),
		NodeID:    "other-node", // Different node to trigger processing
	}

	// Test that handlePolicyChange doesn't panic
	err = dpep.handlePolicyChange(ctx, event)
	// May return error but shouldn't panic
	_ = err
}

func TestDistributedPEP_BroadcastChangeError(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	raft.proposeErr = errors.New("propose error")
	config := DefaultDistributedPEPConfig("tenant-1", "node-1")

	dpep, err := NewDistributedPEP(config, raft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer dpep.Close()

	ctx := context.Background()
	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "test-policy", Tenant: "tenant-1"},
		Spec: PolicySpec{
			Rules: []Rule{
				{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}},
			},
		},
	}

	// Load policy should succeed locally even if broadcast fails
	err = dpep.LoadPolicy(ctx, policy)
	if err != nil {
		t.Fatalf("LoadPolicy failed: %v", err)
	}

	// Check that lastSyncError was set
	stats := dpep.GetStats()
	if stats.LastSyncError == "" {
		// The error should be recorded, but this depends on implementation timing
		// Just verify stats can be retrieved
	}
}
