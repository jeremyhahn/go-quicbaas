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

package discovery

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// mockResolver is a fake DNS resolver for testing.
type mockResolver struct {
	srvRecords []*net.SRV
	srvError   error
}

func (m *mockResolver) LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
	if m.srvError != nil {
		return "", nil, m.srvError
	}
	return "", m.srvRecords, nil
}

func (m *mockResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	// Default implementation for compatibility with existing tests
	return nil, &net.DNSError{
		Err:        "not implemented in basic mock",
		Name:       host,
		IsNotFound: true,
	}
}

func TestNewDNSDiscovery(t *testing.T) {
	tests := []struct {
		name    string
		config  DNSConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: DNSConfig{
				SRVRecord: "_quicbaas._tcp.example.com",
			},
			wantErr: false,
		},
		{
			name: "valid config with custom refresh",
			config: DNSConfig{
				SRVRecord:       "_raft._tcp.cluster.local",
				RefreshInterval: 60 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "empty SRV record",
			config: DNSConfig{
				SRVRecord: "",
			},
			wantErr: true,
		},
		{
			name: "invalid SRV record - no underscores",
			config: DNSConfig{
				SRVRecord: "quicbaas.tcp.example.com",
			},
			wantErr: true,
		},
		{
			name: "invalid SRV record - missing proto",
			config: DNSConfig{
				SRVRecord: "_quicbaas.example.com",
			},
			wantErr: true,
		},
		{
			name: "invalid SRV record - missing domain",
			config: DNSConfig{
				SRVRecord: "_quicbaas._tcp",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dns, err := NewDNSDiscovery(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewDNSDiscovery() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if dns == nil {
					t.Fatal("NewDNSDiscovery() returned nil without error")
				}

				// Check defaults
				if dns.refreshInterval == 0 {
					t.Error("refreshInterval should have default value")
				}

				if dns.resolver == nil {
					t.Error("resolver should not be nil")
				}
			}
		})
	}
}

func TestDNSDiscovery_Discover(t *testing.T) {
	// Create mock resolver with test data
	mockSRVs := []*net.SRV{
		{Target: "node1.example.com.", Port: 63000, Priority: 10, Weight: 10},
		{Target: "node2.example.com.", Port: 63000, Priority: 10, Weight: 10},
		{Target: "node3.example.com.", Port: 63000, Priority: 10, Weight: 10},
	}

	resolver := &mockResolver{
		srvRecords: mockSRVs,
	}

	config := DNSConfig{
		SRVRecord: "_quicbaas._tcp.example.com",
		Resolver:  resolver,
	}

	dns, err := NewDNSDiscovery(config)
	if err != nil {
		t.Fatalf("NewDNSDiscovery() error = %v", err)
	}

	ctx := context.Background()
	peers, err := dns.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(peers) != 3 {
		t.Errorf("Discover() returned %d peers, want 3", len(peers))
	}

	// Verify peer details
	for i, peer := range peers {
		if peer.NodeID == 0 {
			t.Errorf("peer[%d] has NodeID = 0", i)
		}

		if peer.Address == "" {
			t.Errorf("peer[%d] has empty Address", i)
		}

		if peer.RaftAddr == "" {
			t.Errorf("peer[%d] has empty RaftAddr", i)
		}

		if peer.LastSeen.IsZero() {
			t.Errorf("peer[%d] has zero LastSeen timestamp", i)
		}

		// Check metadata
		if _, ok := peer.Metadata["priority"]; !ok {
			t.Errorf("peer[%d] missing priority metadata", i)
		}

		if _, ok := peer.Metadata["weight"]; !ok {
			t.Errorf("peer[%d] missing weight metadata", i)
		}
	}

	// Verify addresses
	expectedAddresses := []string{
		"node1.example.com:63000",
		"node2.example.com:63000",
		"node3.example.com:63000",
	}

	for i, expected := range expectedAddresses {
		if peers[i].Address != expected {
			t.Errorf("peer[%d].Address = %s, want %s", i, peers[i].Address, expected)
		}
	}
}

func TestDNSDiscovery_DiscoverCache(t *testing.T) {
	mockSRVs := []*net.SRV{
		{Target: "node1.example.com.", Port: 63000, Priority: 10, Weight: 10},
	}

	resolver := &mockResolver{
		srvRecords: mockSRVs,
	}

	dns, err := NewDNSDiscovery(DNSConfig{
		SRVRecord:       "_quicbaas._tcp.example.com",
		RefreshInterval: 1 * time.Hour, // Long cache
	})
	if err != nil {
		t.Fatalf("NewDNSDiscovery() error = %v", err)
	}

	dns.resolver = resolver

	ctx := context.Background()

	// First call should query DNS
	peers1, err := dns.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	// Change the mock data
	resolver.srvRecords = []*net.SRV{
		{Target: "node1.example.com.", Port: 63000, Priority: 10, Weight: 10},
		{Target: "node2.example.com.", Port: 63000, Priority: 10, Weight: 10},
	}

	// Second call should return cached data (not updated)
	peers2, err := dns.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(peers2) != len(peers1) {
		t.Errorf("Second Discover() should return cached data, got %d peers, want %d", len(peers2), len(peers1))
	}

	// Invalidate cache
	dns.InvalidateCache()

	// Third call should query DNS again
	peers3, err := dns.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(peers3) != 2 {
		t.Errorf("After cache invalidation, got %d peers, want 2", len(peers3))
	}
}

func TestDNSDiscovery_DiscoverError(t *testing.T) {
	resolver := &mockResolver{
		srvError: errors.New("DNS query failed"),
	}

	dns, err := NewDNSDiscovery(DNSConfig{
		SRVRecord: "_quicbaas._tcp.example.com",
	})
	if err != nil {
		t.Fatalf("NewDNSDiscovery() error = %v", err)
	}

	dns.resolver = resolver

	ctx := context.Background()
	_, err = dns.Discover(ctx)
	if err == nil {
		t.Error("Discover() should return error when DNS query fails")
	}
}

func TestDNSDiscovery_DiscoverNoRecords(t *testing.T) {
	resolver := &mockResolver{
		srvRecords: []*net.SRV{}, // Empty result
	}

	dns, err := NewDNSDiscovery(DNSConfig{
		SRVRecord: "_quicbaas._tcp.example.com",
	})
	if err != nil {
		t.Fatalf("NewDNSDiscovery() error = %v", err)
	}

	dns.resolver = resolver

	ctx := context.Background()
	_, err = dns.Discover(ctx)
	if err == nil {
		t.Error("Discover() should return error when no SRV records found")
	}
}

func TestDNSDiscovery_RegisterDeregister(t *testing.T) {
	dns, err := NewDNSDiscovery(DNSConfig{
		SRVRecord: "_quicbaas._tcp.example.com",
	})
	if err != nil {
		t.Fatalf("NewDNSDiscovery() error = %v", err)
	}

	ctx := context.Background()
	self := Peer{
		NodeID:  1,
		Address: "192.168.1.100:63000",
	}

	// These should all be no-ops and return nil
	if err := dns.Register(ctx, self); err != nil {
		t.Errorf("Register() error = %v, want nil", err)
	}

	if err := dns.Deregister(ctx); err != nil {
		t.Errorf("Deregister() error = %v, want nil", err)
	}
}

func TestDNSDiscovery_Close(t *testing.T) {
	dns, err := NewDNSDiscovery(DNSConfig{
		SRVRecord: "_quicbaas._tcp.example.com",
	})
	if err != nil {
		t.Fatalf("NewDNSDiscovery() error = %v", err)
	}

	// Add some cached peers
	dns.cachedPeers = []Peer{
		{NodeID: 1, Address: "node1.example.com:63000"},
	}

	if err := dns.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Cache should be cleared
	if len(dns.cachedPeers) != 0 {
		t.Error("Close() should clear cached peers")
	}
}

func TestDNSDiscovery_GetCachedPeers(t *testing.T) {
	dns, err := NewDNSDiscovery(DNSConfig{
		SRVRecord: "_quicbaas._tcp.example.com",
	})
	if err != nil {
		t.Fatalf("NewDNSDiscovery() error = %v", err)
	}

	// Set cached peers
	dns.cachedPeers = []Peer{
		{NodeID: 1, Address: "node1.example.com:63000"},
		{NodeID: 2, Address: "node2.example.com:63000"},
	}

	peers := dns.GetCachedPeers()
	if len(peers) != 2 {
		t.Errorf("GetCachedPeers() returned %d peers, want 2", len(peers))
	}

	// Verify we got a copy (modifying it shouldn't affect internal state)
	peers[0].NodeID = 999

	if dns.cachedPeers[0].NodeID == 999 {
		t.Error("GetCachedPeers() did not return a copy")
	}
}

func TestDNSDiscovery_Name(t *testing.T) {
	dns, err := NewDNSDiscovery(DNSConfig{
		SRVRecord: "_quicbaas._tcp.example.com",
	})
	if err != nil {
		t.Fatalf("NewDNSDiscovery() error = %v", err)
	}

	if name := dns.Name(); name != "dns" {
		t.Errorf("Name() = %q, want %q", name, "dns")
	}
}

func TestValidateSRVRecord(t *testing.T) {
	tests := []struct {
		name    string
		record  string
		wantErr bool
	}{
		{
			name:    "valid standard format",
			record:  "_quicbaas._tcp.example.com",
			wantErr: false,
		},
		{
			name:    "valid with subdomain",
			record:  "_raft._tcp.cluster.local",
			wantErr: false,
		},
		{
			name:    "valid consul format",
			record:  "_db._tcp.service.consul",
			wantErr: false,
		},
		{
			name:    "valid udp protocol",
			record:  "_service._udp.example.com",
			wantErr: false,
		},
		{
			name:    "empty record",
			record:  "",
			wantErr: true,
		},
		{
			name:    "no underscore prefix",
			record:  "quicbaas._tcp.example.com",
			wantErr: true,
		},
		{
			name:    "missing proto underscore",
			record:  "_quicbaas.tcp.example.com",
			wantErr: true,
		},
		{
			name:    "too few parts",
			record:  "_service._tcp",
			wantErr: true,
		},
		{
			name:    "no domain",
			record:  "_service._proto",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSRVRecord(tt.record)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSRVRecord() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseSRVRecord(t *testing.T) {
	tests := []struct {
		name        string
		record      string
		wantService string
		wantProto   string
		wantName    string
		wantErr     bool
	}{
		{
			name:        "standard format",
			record:      "_quicbaas._tcp.example.com",
			wantService: "quicbaas",
			wantProto:   "tcp",
			wantName:    "example.com",
			wantErr:     false,
		},
		{
			name:        "with subdomain",
			record:      "_raft._tcp.cluster.local",
			wantService: "raft",
			wantProto:   "tcp",
			wantName:    "cluster.local",
			wantErr:     false,
		},
		{
			name:        "consul format",
			record:      "_db._tcp.service.consul",
			wantService: "db",
			wantProto:   "tcp",
			wantName:    "service.consul",
			wantErr:     false,
		},
		{
			name:    "invalid - too few parts",
			record:  "_service._tcp",
			wantErr: true,
		},
		{
			name:    "invalid - empty",
			record:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, proto, name, err := parseSRVRecord(tt.record)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSRVRecord() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if service != tt.wantService {
					t.Errorf("service = %q, want %q", service, tt.wantService)
				}
				if proto != tt.wantProto {
					t.Errorf("proto = %q, want %q", proto, tt.wantProto)
				}
				if name != tt.wantName {
					t.Errorf("name = %q, want %q", name, tt.wantName)
				}
			}
		})
	}
}

func TestNewDNSDiscoveryFromSRV(t *testing.T) {
	dns, err := NewDNSDiscoveryFromSRV("_quicbaas._tcp.example.com")
	if err != nil {
		t.Fatalf("NewDNSDiscoveryFromSRV() error = %v", err)
	}

	if dns == nil {
		t.Fatal("NewDNSDiscoveryFromSRV() returned nil without error")
	}

	if dns.srvRecord != "_quicbaas._tcp.example.com" {
		t.Errorf("srvRecord = %q, want %q", dns.srvRecord, "_quicbaas._tcp.example.com")
	}

	// Test with invalid record
	_, err = NewDNSDiscoveryFromSRV("invalid")
	if err == nil {
		t.Error("NewDNSDiscoveryFromSRV() should return error for invalid record")
	}
}

func TestDNSDiscovery_ContextCancellation(t *testing.T) {
	// Create a resolver that respects context cancellation
	contextAwareResolver := &mockResolver{
		srvRecords: []*net.SRV{
			{Target: "node1.example.com.", Port: 63000, Priority: 10, Weight: 10},
		},
	}

	dns, err := NewDNSDiscovery(DNSConfig{
		SRVRecord: "_quicbaas._tcp.example.com",
	})
	if err != nil {
		t.Fatalf("NewDNSDiscovery() error = %v", err)
	}

	dns.resolver = contextAwareResolver

	// First, populate the cache with a valid context
	ctx := context.Background()
	_, err = dns.Discover(ctx)
	if err != nil {
		t.Fatalf("Initial Discover() error = %v", err)
	}

	// Now try with canceled context - should return cached data
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should succeed using cached data
	peers, err := dns.Discover(canceledCtx)
	if err != nil {
		t.Errorf("Discover() with canceled context should succeed using cache, got error: %v", err)
	}

	if len(peers) != 1 {
		t.Errorf("Expected 1 peer from cache, got %d", len(peers))
	}
}

// TestDNSDiscovery_CacheExpiration moved to dns_integration_test.go
