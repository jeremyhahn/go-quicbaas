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
	"testing"
)

func TestStaticDiscovery_NewStaticDiscovery(t *testing.T) {
	tests := []struct {
		name      string
		addresses []string
		wantErr   bool
		wantCount int
	}{
		{
			name: "valid single peer",
			addresses: []string{
				"1:192.168.1.100:63000",
			},
			wantErr:   false,
			wantCount: 1,
		},
		{
			name: "valid multiple peers",
			addresses: []string{
				"1:192.168.1.100:63000",
				"2:192.168.1.101:63000",
				"3:192.168.1.102:63000",
			},
			wantErr:   false,
			wantCount: 3,
		},
		{
			name: "valid with hostname",
			addresses: []string{
				"1:node1.example.com:63000",
				"2:node2.example.com:63000",
			},
			wantErr:   false,
			wantCount: 2,
		},
		{
			name:      "empty list",
			addresses: []string{},
			wantErr:   true,
		},
		{
			name: "invalid format - missing port",
			addresses: []string{
				"1:192.168.1.100",
			},
			wantErr: true,
		},
		{
			name: "invalid format - missing node ID",
			addresses: []string{
				"192.168.1.100:63000",
			},
			wantErr: true,
		},
		{
			name: "invalid node ID - zero",
			addresses: []string{
				"0:192.168.1.100:63000",
			},
			wantErr: true,
		},
		{
			name: "invalid node ID - non-numeric",
			addresses: []string{
				"abc:192.168.1.100:63000",
			},
			wantErr: true,
		},
		{
			name: "invalid port - non-numeric",
			addresses: []string{
				"1:192.168.1.100:abc",
			},
			wantErr: true,
		},
		{
			name: "invalid port - out of range",
			addresses: []string{
				"1:192.168.1.100:99999",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sd, err := NewStaticDiscovery(tt.addresses)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewStaticDiscovery() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if sd == nil {
					t.Fatal("NewStaticDiscovery() returned nil without error")
				}

				if len(sd.peers) != tt.wantCount {
					t.Errorf("NewStaticDiscovery() peer count = %d, want %d", len(sd.peers), tt.wantCount)
				}

				// Verify all peers have required fields
				for i, peer := range sd.peers {
					if peer.NodeID == 0 {
						t.Errorf("peer[%d] has NodeID = 0", i)
					}
					if peer.Address == "" {
						t.Errorf("peer[%d] has empty Address", i)
					}
					if peer.RaftAddr == "" {
						t.Errorf("peer[%d] has empty RaftAddr", i)
					}
					if peer.Address != peer.RaftAddr {
						t.Errorf("peer[%d] Address != RaftAddr (%s != %s)", i, peer.Address, peer.RaftAddr)
					}
				}
			}
		})
	}
}

func TestStaticDiscovery_Discover(t *testing.T) {
	addresses := []string{
		"1:192.168.1.100:63000",
		"2:192.168.1.101:63000",
		"3:192.168.1.102:63000",
	}

	sd, err := NewStaticDiscovery(addresses)
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	ctx := context.Background()
	peers, err := sd.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(peers) != 3 {
		t.Errorf("Discover() returned %d peers, want 3", len(peers))
	}

	// Verify LastSeen is set
	for i, peer := range peers {
		if peer.LastSeen.IsZero() {
			t.Errorf("peer[%d] has zero LastSeen timestamp", i)
		}

		if peer.TTL != 0 {
			t.Errorf("peer[%d] has non-zero TTL (static peers don't expire)", i)
		}
	}

	// Verify peer details
	expectedNodeIDs := []uint64{1, 2, 3}
	for i, expectedID := range expectedNodeIDs {
		if peers[i].NodeID != expectedID {
			t.Errorf("peer[%d].NodeID = %d, want %d", i, peers[i].NodeID, expectedID)
		}
	}
}

func TestStaticDiscovery_RegisterDeregister(t *testing.T) {
	addresses := []string{"1:192.168.1.100:63000"}
	sd, err := NewStaticDiscovery(addresses)
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	ctx := context.Background()
	self := Peer{
		NodeID:  1,
		Address: "192.168.1.100:63000",
	}

	// These should all be no-ops and return nil
	if err := sd.Register(ctx, self); err != nil {
		t.Errorf("Register() error = %v, want nil", err)
	}

	if err := sd.Deregister(ctx); err != nil {
		t.Errorf("Deregister() error = %v, want nil", err)
	}

	if err := sd.Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

func TestStaticDiscovery_AddPeer(t *testing.T) {
	addresses := []string{"1:192.168.1.100:63000"}
	sd, err := NewStaticDiscovery(addresses)
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	// Add a new peer
	newPeer := Peer{
		NodeID:  2,
		Address: "192.168.1.101:63000",
	}

	if err := sd.AddPeer(newPeer); err != nil {
		t.Fatalf("AddPeer() error = %v", err)
	}

	// Verify peer was added
	if len(sd.peers) != 2 {
		t.Errorf("peer count = %d, want 2", len(sd.peers))
	}

	// Try to add duplicate
	if err := sd.AddPeer(newPeer); err == nil {
		t.Error("AddPeer() with duplicate should return error")
	}

	// Try to add peer with zero NodeID
	invalidPeer := Peer{
		NodeID:  0,
		Address: "192.168.1.102:63000",
	}
	if err := sd.AddPeer(invalidPeer); err == nil {
		t.Error("AddPeer() with zero NodeID should return error")
	}

	// Try to add peer with empty address
	invalidPeer2 := Peer{
		NodeID:  3,
		Address: "",
	}
	if err := sd.AddPeer(invalidPeer2); err == nil {
		t.Error("AddPeer() with empty address should return error")
	}
}

func TestStaticDiscovery_RemovePeer(t *testing.T) {
	addresses := []string{
		"1:192.168.1.100:63000",
		"2:192.168.1.101:63000",
		"3:192.168.1.102:63000",
	}

	sd, err := NewStaticDiscovery(addresses)
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	// Remove a peer
	if err := sd.RemovePeer(2); err != nil {
		t.Fatalf("RemovePeer() error = %v", err)
	}

	// Verify peer was removed
	if len(sd.peers) != 2 {
		t.Errorf("peer count = %d, want 2", len(sd.peers))
	}

	// Verify correct peer was removed
	for _, peer := range sd.peers {
		if peer.NodeID == 2 {
			t.Error("peer with NodeID 2 should have been removed")
		}
	}

	// Try to remove non-existent peer
	if err := sd.RemovePeer(99); err == nil {
		t.Error("RemovePeer() with non-existent peer should return error")
	}
}

func TestStaticDiscovery_GetPeers(t *testing.T) {
	addresses := []string{
		"1:192.168.1.100:63000",
		"2:192.168.1.101:63000",
	}

	sd, err := NewStaticDiscovery(addresses)
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	peers := sd.GetPeers()
	if len(peers) != 2 {
		t.Errorf("GetPeers() returned %d peers, want 2", len(peers))
	}

	// Verify we got a copy (modifying it shouldn't affect internal state)
	peers[0].NodeID = 999

	if sd.peers[0].NodeID == 999 {
		t.Error("GetPeers() did not return a copy - internal state was modified")
	}
}

func TestStaticDiscovery_Name(t *testing.T) {
	addresses := []string{"1:192.168.1.100:63000"}
	sd, err := NewStaticDiscovery(addresses)
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	if name := sd.Name(); name != "static" {
		t.Errorf("Name() = %q, want %q", name, "static")
	}
}

func TestStaticDiscovery_MultipleDiscoverCalls(t *testing.T) {
	addresses := []string{
		"1:192.168.1.100:63000",
		"2:192.168.1.101:63000",
	}

	sd, err := NewStaticDiscovery(addresses)
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	ctx := context.Background()

	// Call Discover multiple times
	for i := 0; i < 5; i++ {
		peers, err := sd.Discover(ctx)
		if err != nil {
			t.Fatalf("Discover() call %d error = %v", i, err)
		}

		if len(peers) != 2 {
			t.Errorf("Discover() call %d returned %d peers, want 2", i, len(peers))
		}
	}
}

func TestStaticDiscovery_ContextCancellation(t *testing.T) {
	addresses := []string{"1:192.168.1.100:63000"}
	sd, err := NewStaticDiscovery(addresses)
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	// Static discovery should succeed even with canceled context
	// (since it doesn't do any I/O)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	peers, err := sd.Discover(ctx)
	if err != nil {
		t.Errorf("Discover() with canceled context error = %v, want nil", err)
	}

	if len(peers) != 1 {
		t.Errorf("Discover() returned %d peers, want 1", len(peers))
	}
}

// TestStaticDiscovery_ConcurrentAccess moved to static_integration_test.go

func TestNewStaticDiscoveryFromPeers(t *testing.T) {
	peers := []Peer{
		{NodeID: 1, Address: "192.168.1.100:63000"},
		{NodeID: 2, Address: "192.168.1.101:63000"},
		{NodeID: 3, Address: "192.168.1.102:63000"},
	}

	sd, err := NewStaticDiscoveryFromPeers(peers)
	if err != nil {
		t.Fatalf("NewStaticDiscoveryFromPeers() error = %v", err)
	}

	if len(sd.peers) != 3 {
		t.Errorf("peer count = %d, want 3", len(sd.peers))
	}

	// Verify RaftAddr was set
	for i, peer := range sd.peers {
		if peer.RaftAddr == "" {
			t.Errorf("peer[%d] has empty RaftAddr", i)
		}
		if peer.RaftAddr != peer.Address {
			t.Errorf("peer[%d] RaftAddr != Address", i)
		}
	}

	// Test with invalid peers
	invalidTests := []struct {
		name  string
		peers []Peer
	}{
		{
			name:  "empty list",
			peers: []Peer{},
		},
		{
			name: "zero NodeID",
			peers: []Peer{
				{NodeID: 0, Address: "192.168.1.100:63000"},
			},
		},
		{
			name: "empty address",
			peers: []Peer{
				{NodeID: 1, Address: ""},
			},
		},
	}

	for _, tt := range invalidTests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewStaticDiscoveryFromPeers(tt.peers)
			if err == nil {
				t.Error("NewStaticDiscoveryFromPeers() should return error for invalid peers")
			}
		})
	}
}
