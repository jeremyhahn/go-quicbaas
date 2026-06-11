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
	"testing"
	"time"
)

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
	client.SetPolicies([]*CompiledPolicy{
		{
			Name:     "test-policy-1",
			Priority: 100,
		},
		{
			Name:     "test-policy-2",
			Priority: 200,
		},
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

	// Wait for at least one sync cycle
	time.Sleep(150 * time.Millisecond)

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
	client.SetPolicies([]*CompiledPolicy{
		{
			Name:     "test-policy",
			Priority: 100,
		},
	})

	ctx := context.Background()

	// Start syncer
	if err := syncer.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for initial sync
	time.Sleep(100 * time.Millisecond)

	// Verify initial policy was synced
	if cache.Size() != 1 {
		t.Errorf("Cache size = %d after initial sync, want 1", cache.Size())
	}

	// Update policies in mock client
	client.AddPolicy(&CompiledPolicy{
		Name:     "test-policy-2",
		Priority: 200,
	})

	// Wait for next sync
	time.Sleep(100 * time.Millisecond)

	// Verify new policy was synced
	if cache.Size() != 2 {
		t.Errorf("Cache size = %d after periodic sync, want 2", cache.Size())
	}

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

	// Wait a bit for syncer to stop
	time.Sleep(150 * time.Millisecond)

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
		client.AddPolicy(&CompiledPolicy{
			Name:     "policy-" + string(rune('0'+i%10)),
			Priority: 100 + i,
		})
	}

	syncer := NewPolicySyncer(client, cache, 30*time.Second, "test-tenant")

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = syncer.SyncNow(ctx)
	}
}
