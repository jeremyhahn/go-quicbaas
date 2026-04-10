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
	"encoding/binary"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestNewMulticastDiscovery(t *testing.T) {
	tests := []struct {
		name    string
		config  *MulticastConfig
		wantErr bool
	}{
		{
			name:    "nil config uses defaults",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "default config",
			config:  DefaultMulticastConfig(),
			wantErr: false,
		},
		{
			name: "custom config",
			config: &MulticastConfig{
				MulticastGroup:    "239.255.1.1",
				Port:              55555,
				BroadcastInterval: 5 * time.Second,
				PeerTTL:           20 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "invalid multicast group",
			config: &MulticastConfig{
				MulticastGroup: "192.168.1.1", // Not a multicast address
				Port:           63777,
			},
			wantErr: true,
		},
		{
			name: "invalid IP format",
			config: &MulticastConfig{
				MulticastGroup: "not-an-ip",
				Port:           63777,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			md, err := NewMulticastDiscovery(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewMulticastDiscovery() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && md == nil {
				t.Error("NewMulticastDiscovery() returned nil without error")
			}
		})
	}
}

func TestMulticastDiscovery_Name(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}

	if name := md.Name(); name != "multicast" {
		t.Errorf("Name() = %q, want %q", name, "multicast")
	}
}

func TestMulticastDiscovery_Register(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	tests := []struct {
		name    string
		peer    Peer
		wantErr bool
	}{
		{
			name: "valid peer",
			peer: Peer{
				NodeID:  1,
				Address: "192.168.1.100:63000",
			},
			wantErr: false,
		},
		{
			name: "zero node ID",
			peer: Peer{
				NodeID:  0,
				Address: "192.168.1.100:63000",
			},
			wantErr: true,
		},
		{
			name: "empty address",
			peer: Peer{
				NodeID:  1,
				Address: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			err := md.Register(ctx, tt.peer)
			if (err != nil) != tt.wantErr {
				t.Errorf("Register() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMulticastDiscovery_Deregister(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	// Deregister without start should not error
	ctx := context.Background()
	if err := md.Deregister(ctx); err != nil {
		t.Errorf("Deregister() error = %v, want nil", err)
	}
}

func TestMulticastDiscovery_Close(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}

	// First close should succeed
	if err := md.Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}

	// Second close should also succeed (idempotent)
	if err := md.Close(); err != nil {
		t.Errorf("Close() second call error = %v, want nil", err)
	}
}

func TestMulticastDiscovery_IsRunning(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	// Initially not running
	if md.IsRunning() {
		t.Error("IsRunning() = true before start, want false")
	}
}

func TestMulticastDiscovery_GetPeerCount(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	// Initially zero peers
	if count := md.GetPeerCount(); count != 0 {
		t.Errorf("GetPeerCount() = %d, want 0", count)
	}
}

func TestMulticastDiscovery_MessageEncoding(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	msg := &DiscoveryMessage{
		Type:      MessageAnnounce,
		NodeID:    12345,
		Address:   "192.168.1.100:63000",
		RaftAddr:  "192.168.1.100:63001",
		Timestamp: time.Now().UnixNano(),
		Metadata: map[string]string{
			"region": "us-west-2",
			"zone":   "a",
		},
	}

	// Encode
	data, err := md.encodeMessage(msg)
	if err != nil {
		t.Fatalf("encodeMessage() error = %v", err)
	}

	// Verify header
	if len(data) < 5 {
		t.Fatalf("Encoded message too short: %d bytes", len(data))
	}

	magic := binary.BigEndian.Uint32(data[0:4])
	if magic != magicNumber {
		t.Errorf("Magic number = 0x%X, want 0x%X", magic, magicNumber)
	}

	version := data[4]
	if version != protocolVersion {
		t.Errorf("Protocol version = %d, want %d", version, protocolVersion)
	}

	// Decode
	decoded, err := md.parseMessage(data)
	if err != nil {
		t.Fatalf("parseMessage() error = %v", err)
	}

	if decoded.Type != msg.Type {
		t.Errorf("Type = %d, want %d", decoded.Type, msg.Type)
	}

	if decoded.NodeID != msg.NodeID {
		t.Errorf("NodeID = %d, want %d", decoded.NodeID, msg.NodeID)
	}

	if decoded.Address != msg.Address {
		t.Errorf("Address = %q, want %q", decoded.Address, msg.Address)
	}

	if decoded.RaftAddr != msg.RaftAddr {
		t.Errorf("RaftAddr = %q, want %q", decoded.RaftAddr, msg.RaftAddr)
	}

	if decoded.Metadata["region"] != msg.Metadata["region"] {
		t.Errorf("Metadata[region] = %q, want %q",
			decoded.Metadata["region"], msg.Metadata["region"])
	}
}

func TestMulticastDiscovery_ParseMessage_Invalid(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "too short",
			data: []byte{0x01, 0x02, 0x03},
		},
		{
			name: "wrong magic",
			data: []byte{0x00, 0x00, 0x00, 0x00, 0x01, '{', '}'},
		},
		{
			name: "unsupported version",
			data: func() []byte {
				d := make([]byte, 7)
				binary.BigEndian.PutUint32(d[0:4], magicNumber)
				d[4] = 255 // Very high version
				copy(d[5:], []byte("{}"))
				return d
			}(),
		},
		{
			name: "invalid JSON",
			data: func() []byte {
				d := make([]byte, 10)
				binary.BigEndian.PutUint32(d[0:4], magicNumber)
				d[4] = protocolVersion
				copy(d[5:], []byte("not-json"))
				return d
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := md.parseMessage(tt.data)
			if err == nil {
				t.Error("parseMessage() expected error, got nil")
			}
		})
	}
}

func TestMulticastDiscovery_UpdatePeer(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	// Add a peer via message handling
	msg := &DiscoveryMessage{
		Type:      MessageAnnounce,
		NodeID:    999,
		Address:   "10.0.0.1:63000",
		RaftAddr:  "10.0.0.1:63001",
		Timestamp: time.Now().UnixNano(),
	}

	md.updatePeer(msg)

	// Verify peer was added
	peers := md.getPeers()
	if len(peers) != 1 {
		t.Fatalf("getPeers() returned %d peers, want 1", len(peers))
	}

	if peers[0].NodeID != msg.NodeID {
		t.Errorf("Peer NodeID = %d, want %d", peers[0].NodeID, msg.NodeID)
	}

	if peers[0].Address != msg.Address {
		t.Errorf("Peer Address = %q, want %q", peers[0].Address, msg.Address)
	}

	// Update existing peer
	msg2 := &DiscoveryMessage{
		Type:      MessageAnnounce,
		NodeID:    999,
		Address:   "10.0.0.2:63000", // Changed address
		Timestamp: time.Now().UnixNano(),
	}

	md.updatePeer(msg2)

	peers = md.getPeers()
	if len(peers) != 1 {
		t.Fatalf("getPeers() after update returned %d peers, want 1", len(peers))
	}

	if peers[0].Address != msg2.Address {
		t.Errorf("Updated Peer Address = %q, want %q", peers[0].Address, msg2.Address)
	}
}

func TestMulticastDiscovery_UpdatePeer_Invalid(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	// Zero NodeID should be ignored
	md.updatePeer(&DiscoveryMessage{
		NodeID:  0,
		Address: "10.0.0.1:63000",
	})

	if count := md.GetPeerCount(); count != 0 {
		t.Errorf("GetPeerCount() = %d after zero NodeID, want 0", count)
	}

	// Empty address should be ignored
	md.updatePeer(&DiscoveryMessage{
		NodeID:  1,
		Address: "",
	})

	if count := md.GetPeerCount(); count != 0 {
		t.Errorf("GetPeerCount() = %d after empty address, want 0", count)
	}
}

func TestMulticastDiscovery_RemovePeer(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	// Add peers
	md.updatePeer(&DiscoveryMessage{NodeID: 1, Address: "10.0.0.1:63000"})
	md.updatePeer(&DiscoveryMessage{NodeID: 2, Address: "10.0.0.2:63000"})

	if count := md.GetPeerCount(); count != 2 {
		t.Fatalf("GetPeerCount() = %d, want 2", count)
	}

	// Remove one peer
	md.removePeer(1)

	if count := md.GetPeerCount(); count != 1 {
		t.Errorf("GetPeerCount() after remove = %d, want 1", count)
	}

	// Verify correct peer remains
	peers := md.getPeers()
	if len(peers) != 1 || peers[0].NodeID != 2 {
		t.Errorf("Wrong peer remaining after remove")
	}
}

func TestMulticastDiscovery_RemoveExpiredPeers(t *testing.T) {
	config := &MulticastConfig{
		MulticastGroup: DefaultMulticastGroup,
		Port:           DefaultMulticastPort,
		PeerTTL:        50 * time.Millisecond, // Short TTL for testing
	}

	md, err := NewMulticastDiscovery(config)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	// Add a peer
	md.updatePeer(&DiscoveryMessage{NodeID: 1, Address: "10.0.0.1:63000"})

	if count := md.GetPeerCount(); count != 1 {
		t.Fatalf("GetPeerCount() = %d, want 1", count)
	}

	// Wait for TTL to expire
	time.Sleep(100 * time.Millisecond)

	// Remove expired peers
	md.removeExpiredPeers()

	if count := md.GetPeerCount(); count != 0 {
		t.Errorf("GetPeerCount() after TTL expiry = %d, want 0", count)
	}
}

func TestMulticastDiscovery_HandleMessage(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	// Register self so we can send responses
	ctx := context.Background()
	md.Register(ctx, Peer{
		NodeID:  100,
		Address: "10.0.0.100:63000",
	})

	// Test announce message
	md.handleMessage(&DiscoveryMessage{
		Type:    MessageAnnounce,
		NodeID:  1,
		Address: "10.0.0.1:63000",
	})

	if count := md.GetPeerCount(); count != 1 {
		t.Errorf("After announce: GetPeerCount() = %d, want 1", count)
	}

	// Test response message
	md.handleMessage(&DiscoveryMessage{
		Type:    MessageResponse,
		NodeID:  2,
		Address: "10.0.0.2:63000",
	})

	if count := md.GetPeerCount(); count != 2 {
		t.Errorf("After response: GetPeerCount() = %d, want 2", count)
	}

	// Test leave message
	md.handleMessage(&DiscoveryMessage{
		Type:   MessageLeave,
		NodeID: 1,
	})

	if count := md.GetPeerCount(); count != 1 {
		t.Errorf("After leave: GetPeerCount() = %d, want 1", count)
	}
}

func TestDefaultMulticastConfig(t *testing.T) {
	config := DefaultMulticastConfig()

	if config.MulticastGroup != DefaultMulticastGroup {
		t.Errorf("MulticastGroup = %q, want %q", config.MulticastGroup, DefaultMulticastGroup)
	}

	if config.Port != DefaultMulticastPort {
		t.Errorf("Port = %d, want %d", config.Port, DefaultMulticastPort)
	}

	if config.BroadcastInterval != DefaultBroadcastInterval {
		t.Errorf("BroadcastInterval = %v, want %v", config.BroadcastInterval, DefaultBroadcastInterval)
	}

	if config.PeerTTL != DefaultPeerTTL {
		t.Errorf("PeerTTL = %v, want %v", config.PeerTTL, DefaultPeerTTL)
	}

	if !config.SendQueryOnStart {
		t.Error("SendQueryOnStart = false, want true")
	}
}

func TestMessageTypes(t *testing.T) {
	// Verify message type constants
	if MessageAnnounce != 1 {
		t.Errorf("MessageAnnounce = %d, want 1", MessageAnnounce)
	}
	if MessageQuery != 2 {
		t.Errorf("MessageQuery = %d, want 2", MessageQuery)
	}
	if MessageResponse != 3 {
		t.Errorf("MessageResponse = %d, want 3", MessageResponse)
	}
	if MessageLeave != 4 {
		t.Errorf("MessageLeave = %d, want 4", MessageLeave)
	}
}

func TestDiscoveryMessage_JSONMarshal(t *testing.T) {
	msg := DiscoveryMessage{
		Type:      MessageAnnounce,
		NodeID:    12345,
		Address:   "10.0.0.1:63000",
		RaftAddr:  "10.0.0.1:63001",
		Timestamp: 1234567890,
		Metadata: map[string]string{
			"version": "1.0.0",
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded DiscoveryMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if decoded.Type != msg.Type {
		t.Errorf("Type mismatch: got %d, want %d", decoded.Type, msg.Type)
	}

	if decoded.NodeID != msg.NodeID {
		t.Errorf("NodeID mismatch: got %d, want %d", decoded.NodeID, msg.NodeID)
	}

	if decoded.Address != msg.Address {
		t.Errorf("Address mismatch: got %q, want %q", decoded.Address, msg.Address)
	}
}

func TestMulticastDiscovery_Concurrent(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	var wg sync.WaitGroup
	numGoroutines := 10
	numOps := 100

	// Concurrent updates
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				nodeID := uint64((id * numOps) + j + 1)
				md.updatePeer(&DiscoveryMessage{
					NodeID:  nodeID,
					Address: "10.0.0.1:63000",
				})
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				md.getPeers()
				md.GetPeerCount()
			}
		}()
	}

	wg.Wait()

	// Verify we have all peers
	expected := numGoroutines * numOps
	if count := md.GetPeerCount(); count != expected {
		t.Errorf("GetPeerCount() = %d, want %d", count, expected)
	}
}

// TestMulticastDiscovery_RaftAddrFallback verifies RaftAddr defaults to Address.
func TestMulticastDiscovery_RaftAddrFallback(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	// Add peer without RaftAddr
	msg := &DiscoveryMessage{
		NodeID:  1,
		Address: "10.0.0.1:63000",
		// RaftAddr intentionally empty
	}

	md.updatePeer(msg)

	peers := md.getPeers()
	if len(peers) != 1 {
		t.Fatalf("getPeers() returned %d peers, want 1", len(peers))
	}

	if peers[0].RaftAddr != msg.Address {
		t.Errorf("RaftAddr = %q, want %q (should fallback to Address)",
			peers[0].RaftAddr, msg.Address)
	}
}

// Benchmark tests.
func BenchmarkMulticastDiscovery_UpdatePeer(b *testing.B) {
	md, err := NewMulticastDiscovery(nil)
	if err != nil {
		b.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	msg := &DiscoveryMessage{
		NodeID:    1,
		Address:   "10.0.0.1:63000",
		RaftAddr:  "10.0.0.1:63001",
		Timestamp: time.Now().UnixNano(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg.NodeID = uint64(i + 1)
		md.updatePeer(msg)
	}
}

func BenchmarkMulticastDiscovery_GetPeers(b *testing.B) {
	md, err := NewMulticastDiscovery(nil)
	if err != nil {
		b.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	// Add some peers
	for i := 0; i < 100; i++ {
		md.updatePeer(&DiscoveryMessage{
			NodeID:  uint64(i + 1),
			Address: "10.0.0.1:63000",
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = md.getPeers()
	}
}

func BenchmarkMulticastDiscovery_EncodeMessage(b *testing.B) {
	md, err := NewMulticastDiscovery(nil)
	if err != nil {
		b.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	msg := &DiscoveryMessage{
		Type:      MessageAnnounce,
		NodeID:    12345,
		Address:   "192.168.1.100:63000",
		RaftAddr:  "192.168.1.100:63001",
		Timestamp: time.Now().UnixNano(),
		Metadata: map[string]string{
			"region": "us-west-2",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = md.encodeMessage(msg)
	}
}

func BenchmarkMulticastDiscovery_ParseMessage(b *testing.B) {
	md, err := NewMulticastDiscovery(nil)
	if err != nil {
		b.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	msg := &DiscoveryMessage{
		Type:      MessageAnnounce,
		NodeID:    12345,
		Address:   "192.168.1.100:63000",
		RaftAddr:  "192.168.1.100:63001",
		Timestamp: time.Now().UnixNano(),
	}

	data, _ := md.encodeMessage(msg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = md.parseMessage(data)
	}
}

// NOTE: Multicast loopback integration tests are in the integration test suite
// where proper networking environment can be controlled.
