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

// TestDNSDiscovery_CacheExpiration tests cache expiration with time delays.
// This is an integration test because it uses time.Sleep to wait for cache expiration.
func TestDNSDiscovery_CacheExpiration(t *testing.T) {
	mockSRVs := []*net.SRV{
		{Target: "node1.example.com.", Port: 63000, Priority: 10, Weight: 10},
	}

	resolver := &mockResolver{
		srvRecords: mockSRVs,
	}

	dns, err := NewDNSDiscovery(DNSConfig{
		SRVRecord:       "_quicbaas._tcp.example.com",
		RefreshInterval: 100 * time.Millisecond, // Short cache
	})
	if err != nil {
		t.Fatalf("NewDNSDiscovery() error = %v", err)
	}

	dns.resolver = resolver

	ctx := context.Background()

	// First call
	peers1, err := dns.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	// Change mock data
	resolver.srvRecords = []*net.SRV{
		{Target: "node1.example.com.", Port: 63000, Priority: 10, Weight: 10},
		{Target: "node2.example.com.", Port: 63000, Priority: 10, Weight: 10},
	}

	// Wait for cache to expire
	time.Sleep(150 * time.Millisecond)

	// Second call should get new data
	peers2, err := dns.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(peers2) == len(peers1) {
		t.Error("Cache should have expired and returned new data")
	}
}
