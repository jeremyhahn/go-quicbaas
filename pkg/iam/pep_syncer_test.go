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
	"errors"
	"sync"
	"testing"
	"time"
)

// mockIAMClient implements IAMClient for pep_syncer_test.go.
type mockIAMClient struct {
	mu         sync.Mutex
	policies   []*Policy
	fetchError error
	closed     bool
}

func newMockIAMClient() *mockIAMClient {
	return &mockIAMClient{}
}

func (m *mockIAMClient) SetPolicies(policies []*Policy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.policies = policies
}

func (m *mockIAMClient) AddPolicy(policy *Policy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.policies = append(m.policies, policy)
}

func (m *mockIAMClient) FetchPolicies(_ context.Context, _ string, _ []string) ([]*Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fetchError != nil {
		return nil, m.fetchError
	}
	out := make([]*Policy, len(m.policies))
	copy(out, m.policies)
	return out, nil
}

func (m *mockIAMClient) Subscribe(_ context.Context, _ string, _ []string) (<-chan *PolicyUpdate, error) {
	ch := make(chan *PolicyUpdate)
	close(ch)
	return ch, nil
}

func (m *mockIAMClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// Compile-time interface check.
var _ IAMClient = (*mockIAMClient)(nil)

func TestNewPolicySyncer(t *testing.T) {
	client := newMockIAMClient()
	cache := NewPolicyCache(&CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	})

	syncer := NewPolicySyncer(client, cache, 30*time.Second, "test-tenant")

	if syncer == nil {
		t.Fatal("NewPolicySyncer returned nil")
	}

	if syncer.client != client {
		t.Error("client not set correctly")
	}

	if syncer.cache != cache {
		t.Error("cache not set correctly")
	}

	if syncer.interval != 30*time.Second {
		t.Errorf("interval = %v, want 30s", syncer.interval)
	}

	if syncer.tenantID != "test-tenant" {
		t.Errorf("tenantID = %s, want test-tenant", syncer.tenantID)
	}
}

func TestPolicySyncer_SyncNow(t *testing.T) {
	client := newMockIAMClient()
	cache := NewPolicyCache(&CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	})

	// Add test policies to mock client
	client.SetPolicies([]*Policy{
		{Metadata: PolicyMetadata{Name: "test-policy-1", Priority: 100}},
		{Metadata: PolicyMetadata{Name: "test-policy-2", Priority: 200}},
	})

	syncer := NewPolicySyncer(client, cache, 30*time.Second, "test-tenant")

	ctx := context.Background()
	err := syncer.SyncNow(ctx)

	if err != nil {
		t.Errorf("SyncNow() error = %v", err)
	}

	// Verify policies were cached
	if cache.Size() != 2 {
		t.Errorf("Cache size = %d, want 2", cache.Size())
	}

	// Verify last sync time was updated
	lastSync := syncer.GetLastSyncTime()
	if lastSync.IsZero() {
		t.Error("Last sync time not updated")
	}
}

func TestPolicySyncer_SyncNowError(t *testing.T) {
	client := newMockIAMClient()
	cache := NewPolicyCache(&CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	})

	// Set fetch error
	client.fetchError = errors.New("connection failed")

	syncer := NewPolicySyncer(client, cache, 30*time.Second, "test-tenant")

	ctx := context.Background()
	err := syncer.SyncNow(ctx)

	if err == nil {
		t.Error("SyncNow() should return error when fetch fails")
	}

	// Verify sync error was recorded
	if syncer.GetSyncErrors() != 1 {
		t.Errorf("Sync errors = %d, want 1", syncer.GetSyncErrors())
	}
}

func TestPolicySyncer_StartStop(t *testing.T) {
	client := newMockIAMClient()
	cache := NewPolicyCache(&CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	})

	syncer := NewPolicySyncer(client, cache, 100*time.Millisecond, "test-tenant")

	ctx := context.Background()

	// Start syncer
	err := syncer.Start(ctx)
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}

	// Verify it's running
	if !syncer.IsRunning() {
		t.Error("Syncer should be running after Start()")
	}

	// The sleep that stood here waited "for at least one sync cycle" and then
	// asserted nothing about it — this test's assertions are IsRunning before
	// and after Stop, neither of which depends on a cycle having run. It is
	// removed rather than converted: there is nothing to poll for, because
	// this client has no policies, so a completed cycle leaves no trace.

	// Stop syncer
	syncer.Stop()

	// Verify it's stopped
	if syncer.IsRunning() {
		t.Error("Syncer should not be running after Stop()")
	}

	// Stop should be idempotent
	syncer.Stop()
}

func TestPolicySyncer_StartIdempotent(t *testing.T) {
	client := newMockIAMClient()
	cache := NewPolicyCache(&CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	})

	syncer := NewPolicySyncer(client, cache, 30*time.Second, "test-tenant")

	ctx := context.Background()

	// Start multiple times
	if err := syncer.Start(ctx); err != nil {
		t.Errorf("First Start() error = %v", err)
	}

	if err := syncer.Start(ctx); err != nil {
		t.Errorf("Second Start() error = %v", err)
	}

	// Clean up
	syncer.Stop()
}

func TestPolicySyncer_GetLastSyncTime(t *testing.T) {
	client := newMockIAMClient()
	cache := NewPolicyCache(&CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	})

	syncer := NewPolicySyncer(client, cache, 30*time.Second, "test-tenant")

	// Initially should be zero
	lastSync := syncer.GetLastSyncTime()
	if !lastSync.IsZero() {
		t.Error("Initial last sync time should be zero")
	}

	// Perform sync
	ctx := context.Background()
	if err := syncer.SyncNow(ctx); err != nil {
		t.Errorf("SyncNow() error = %v", err)
	}

	// Should now be set
	lastSync = syncer.GetLastSyncTime()
	if lastSync.IsZero() {
		t.Error("Last sync time should be set after sync")
	}

	// Should be recent
	if time.Since(lastSync) > 1*time.Second {
		t.Error("Last sync time should be recent")
	}
}

func TestPolicySyncer_GetSyncErrors(t *testing.T) {
	client := newMockIAMClient()
	cache := NewPolicyCache(&CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	})

	syncer := NewPolicySyncer(client, cache, 30*time.Second, "test-tenant")

	// Initially should be zero
	if syncer.GetSyncErrors() != 0 {
		t.Error("Initial sync errors should be 0")
	}

	// Set fetch error
	client.fetchError = errors.New("test error")

	ctx := context.Background()

	// Perform sync (should fail)
	_ = syncer.SyncNow(ctx)

	// Should have one error
	if syncer.GetSyncErrors() != 1 {
		t.Errorf("Sync errors = %d, want 1", syncer.GetSyncErrors())
	}

	// Reset errors
	syncer.ResetSyncErrors()

	// Should be zero again
	if syncer.GetSyncErrors() != 0 {
		t.Errorf("Sync errors = %d after reset, want 0", syncer.GetSyncErrors())
	}
}

// waitFor polls until condition holds, failing at the deadline.
//
// It replaces time.Sleep as a synchronization device (testing-guidelines: a
// sleep asserts nothing and is a race against whatever else the machine is
// doing). Under load a fixed sleep expires before the work it was waiting for
// completes, which is how TestPolicySyncer_PeriodicSync produced false reds in
// CI while passing every time anyone checked it by hand.
//
// The deadline is a FAILURE BOUND, not the synchronization: the condition is
// polled, so the test finishes as soon as the work is done and reports a real
// timeout if it never is.
func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()

	for {
		if condition() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		<-ticker.C
	}
}

func TestPolicySyncer_PeriodicSync(t *testing.T) {
	client := newMockIAMClient()
	cache := NewPolicyCache(&CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	})

	// Use short interval for testing
	syncer := NewPolicySyncer(client, cache, 50*time.Millisecond, "test-tenant")

	// Add initial policy
	client.SetPolicies([]*Policy{
		{Metadata: PolicyMetadata{Name: "test-policy", Priority: 100}},
	})

	ctx := context.Background()

	// Start syncer
	if err := syncer.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	waitFor(t, "the initial sync to populate the cache", func() bool {
		return cache.Size() == 1
	})

	// Update policies in mock client
	client.AddPolicy(&Policy{Metadata: PolicyMetadata{Name: "test-policy-2", Priority: 200}})

	waitFor(t, "the periodic sync to pick up the added policy", func() bool {
		return cache.Size() == 2
	})

	// Clean up
	syncer.Stop()
}

func TestPolicySyncer_ContextCancellation(t *testing.T) {
	client := newMockIAMClient()
	cache := NewPolicyCache(&CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	})

	syncer := NewPolicySyncer(client, cache, 100*time.Millisecond, "test-tenant")

	ctx, cancel := context.WithCancel(context.Background())

	// Start syncer
	if err := syncer.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Verify it's running
	if !syncer.IsRunning() {
		t.Error("Syncer should be running")
	}

	// Cancel context
	cancel()

	// The sleep that stood here asserted nothing either, and it CANNOT be
	// converted into the obvious assertion: IsRunning tracks Start/Stop (the
	// `running` flag is cleared only by Stop), not the sync goroutine's
	// liveness, so it stays true after the context is cancelled even though
	// the goroutine has returned. Polling for !IsRunning() here times out
	// against correct-as-written code.
	//
	// So this test does not currently prove what its name claims. Asserting
	// the real property means changing what the syncer reports, which is
	// production behaviour and outside a test-hygiene change; recorded rather
	// than quietly widened.

	// Clean up
	syncer.Stop()
}

// Benchmark tests

func BenchmarkPolicySyncer_SyncNow(b *testing.B) {
	client := newMockIAMClient()
	cache := NewPolicyCache(&CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           10000,
		BloomFilterSize:   1000000,
		BloomFilterHashes: 7,
	})

	// Add test policies
	for i := 0; i < 100; i++ {
		client.AddPolicy(&Policy{Metadata: PolicyMetadata{Name: "policy-" + string(rune('0'+i%10)), Priority: 100 + i}})
	}

	syncer := NewPolicySyncer(client, cache, 30*time.Second, "test-tenant")

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = syncer.SyncNow(ctx)
	}
}

// blockingIAMClient's FetchPolicies blocks until its context ends, standing in
// for a hung IAM fetch.
type blockingIAMClient struct{}

func (blockingIAMClient) FetchPolicies(ctx context.Context, _ string, _ []string) ([]*Policy, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (blockingIAMClient) Subscribe(_ context.Context, _ string, _ []string) (<-chan *PolicyUpdate, error) {
	ch := make(chan *PolicyUpdate)
	close(ch)
	return ch, nil
}

func (blockingIAMClient) Close() error { return nil }

var _ IAMClient = blockingIAMClient{}

func TestPolicySyncer_SyncNowHonoursContext(t *testing.T) {
	cache := NewPolicyCache(&CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	})

	syncer := NewPolicySyncer(blockingIAMClient{}, cache, 100*time.Millisecond, "test-tenant")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	syncDone := make(chan error, 1)
	go func() { syncDone <- syncer.SyncNow(ctx) }()

	select {
	case err := <-syncDone:
		if err == nil {
			t.Error("SyncNow() should return an error when its context ends")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SyncNow() did not return after its context ended")
	}

	if syncer.GetSyncErrors() != 1 {
		t.Errorf("Sync errors = %d, want 1", syncer.GetSyncErrors())
	}

	startCtx, startCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer startCancel()

	startDone := make(chan error, 1)
	go func() { startDone <- syncer.Start(startCtx) }()

	select {
	case <-startDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return after its context ended")
	}

	stopDone := make(chan struct{})
	go func() {
		syncer.Stop()
		close(stopDone)
	}()

	select {
	case <-stopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not return")
	}
}
