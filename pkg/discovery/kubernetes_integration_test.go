//go:build raft && integration

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

package discovery

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestKubernetesDiscovery_ConcurrentDiscovery tests concurrent discovery calls.
// This is an integration test because it uses goroutines with time.Sleep for synchronization.
func TestKubernetesDiscovery_ConcurrentDiscovery(t *testing.T) {
	mockSRVs := []*net.SRV{
		{Target: "quicbaas-0.quicbaas.default.svc.cluster.local.", Port: 63001, Priority: 10, Weight: 10},
		{Target: "quicbaas-1.quicbaas.default.svc.cluster.local.", Port: 63001, Priority: 10, Weight: 10},
	}

	resolver := &mockKubernetesResolver{
		srvRecords: mockSRVs,
	}

	kd, err := NewKubernetesDiscovery(KubernetesConfig{
		ServiceName:     "quicbaas",
		Namespace:       "default",
		StatefulSetName: "quicbaas",
		Resolver:        resolver,
	})
	if err != nil {
		t.Fatalf("NewKubernetesDiscovery() error = %v", err)
	}

	ctx := context.Background()

	// Run concurrent Discover calls
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_, err := kd.Discover(ctx)
				if err != nil {
					t.Errorf("concurrent Discover() error = %v", err)
				}
				time.Sleep(1 * time.Millisecond)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestKubernetesDiscovery_CacheExpiration tests cache expiration with time delays.
// This is an integration test because it uses time.Sleep to wait for cache expiration.
func TestKubernetesDiscovery_CacheExpiration(t *testing.T) {
	mockSRVs := []*net.SRV{
		{Target: "quicbaas-0.quicbaas.default.svc.cluster.local.", Port: 63001, Priority: 10, Weight: 10},
	}

	resolver := &mockKubernetesResolver{
		srvRecords: mockSRVs,
	}

	kd, err := NewKubernetesDiscovery(KubernetesConfig{
		ServiceName:     "quicbaas",
		Namespace:       "default",
		StatefulSetName: "quicbaas",
		RefreshInterval: 100 * time.Millisecond, // Short cache
	})
	if err != nil {
		t.Fatalf("NewKubernetesDiscovery() error = %v", err)
	}

	kd.resolver = resolver

	ctx := context.Background()

	// First call
	peers1, err := kd.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	// Change mock data
	resolver.srvRecords = []*net.SRV{
		{Target: "quicbaas-0.quicbaas.default.svc.cluster.local.", Port: 63001, Priority: 10, Weight: 10},
		{Target: "quicbaas-1.quicbaas.default.svc.cluster.local.", Port: 63001, Priority: 10, Weight: 10},
	}

	// Wait for cache to expire
	time.Sleep(150 * time.Millisecond)

	// Second call should get new data
	peers2, err := kd.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(peers2) == len(peers1) {
		t.Error("Cache should have expired and returned new data")
	}

	if len(peers2) != 2 {
		t.Errorf("After cache expiration, got %d peers, want 2", len(peers2))
	}
}

// TestKubernetesDiscovery_StatefulSetScaling tests discovery during scaling events.
// This is an integration test because it uses time.Sleep to wait for cache expiration.
func TestKubernetesDiscovery_StatefulSetScaling(t *testing.T) {
	// Test scenario: StatefulSet starts with 2 pods, scales to 3
	initialSRVs := []*net.SRV{
		{Target: "quicbaas-0.quicbaas.default.svc.cluster.local.", Port: 63001, Priority: 10, Weight: 10},
		{Target: "quicbaas-1.quicbaas.default.svc.cluster.local.", Port: 63001, Priority: 10, Weight: 10},
	}

	resolver := &mockKubernetesResolver{
		srvRecords: initialSRVs,
	}

	kd, err := NewKubernetesDiscovery(KubernetesConfig{
		ServiceName:     "quicbaas",
		Namespace:       "default",
		StatefulSetName: "quicbaas",
		RefreshInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewKubernetesDiscovery() error = %v", err)
	}

	kd.resolver = resolver

	ctx := context.Background()

	// Initial discovery - 2 pods
	peers1, err := kd.Discover(ctx)
	if err != nil {
		t.Fatalf("Initial Discover() error = %v", err)
	}

	if len(peers1) != 2 {
		t.Errorf("Initial discovery returned %d peers, want 2", len(peers1))
	}

	// Simulate scaling event - add third pod
	resolver.srvRecords = []*net.SRV{
		{Target: "quicbaas-0.quicbaas.default.svc.cluster.local.", Port: 63001, Priority: 10, Weight: 10},
		{Target: "quicbaas-1.quicbaas.default.svc.cluster.local.", Port: 63001, Priority: 10, Weight: 10},
		{Target: "quicbaas-2.quicbaas.default.svc.cluster.local.", Port: 63001, Priority: 10, Weight: 10},
	}

	// Wait for cache to expire
	time.Sleep(60 * time.Millisecond)

	// Second discovery - 3 pods
	peers2, err := kd.Discover(ctx)
	if err != nil {
		t.Fatalf("Second Discover() error = %v", err)
	}

	if len(peers2) != 3 {
		t.Errorf("After scaling, returned %d peers, want 3", len(peers2))
	}

	// Verify new pod has correct node ID
	if peers2[2].NodeID != 3 {
		t.Errorf("New pod NodeID = %d, want 3", peers2[2].NodeID)
	}
}
