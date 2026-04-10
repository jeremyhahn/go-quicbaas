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
	"net"
	"testing"
	"time"
)

// TestMulticastDiscoveryPSK_EncodeDecodeWithSecret tests message encoding
// and decoding with PSK authentication enabled.
func TestMulticastDiscoveryPSK_EncodeDecodeWithSecret(t *testing.T) {
	secret := "my-cluster-secret-key"

	config := &MulticastConfig{
		MulticastGroup:    DefaultMulticastGroup,
		Port:              DefaultMulticastPort,
		BroadcastInterval: DefaultBroadcastInterval,
		PeerTTL:           DefaultPeerTTL,
		ClusterSecret:     secret,
	}

	md, err := NewMulticastDiscovery(config)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	msg := &DiscoveryMessage{
		Type:      MessageAnnounce,
		NodeID:    12345,
		Address:   "192.168.1.100:8080",
		RaftAddr:  "192.168.1.100:9000",
		Timestamp: time.Now().UnixNano(),
		Metadata: map[string]string{
			"region": "us-east",
		},
	}

	// Encode the message
	encoded, err := md.encodeMessage(msg)
	if err != nil {
		t.Fatalf("Failed to encode message: %v", err)
	}

	// Check wire format: magic(4) + version(1) + sig_len(2) + signature(32) + payload
	if len(encoded) < 4+1+2+32 {
		t.Fatalf("Encoded message too short: %d bytes", len(encoded))
	}

	// Verify magic number
	magic := binary.BigEndian.Uint32(encoded[0:4])
	if magic != magicNumber {
		t.Errorf("Invalid magic number: 0x%08X", magic)
	}

	// Verify version
	if encoded[4] != protocolVersion {
		t.Errorf("Invalid version: %d", encoded[4])
	}

	// Verify signature length
	sigLen := binary.BigEndian.Uint16(encoded[5:7])
	if sigLen != 32 {
		t.Errorf("Expected signature length 32, got %d", sigLen)
	}

	// Parse the message back
	parsed, err := md.parseMessage(encoded)
	if err != nil {
		t.Fatalf("Failed to parse message: %v", err)
	}

	// Verify parsed message matches original
	if parsed.Type != msg.Type {
		t.Errorf("Type mismatch: got %v, want %v", parsed.Type, msg.Type)
	}
	if parsed.NodeID != msg.NodeID {
		t.Errorf("NodeID mismatch: got %v, want %v", parsed.NodeID, msg.NodeID)
	}
	if parsed.Address != msg.Address {
		t.Errorf("Address mismatch: got %v, want %v", parsed.Address, msg.Address)
	}
	if parsed.RaftAddr != msg.RaftAddr {
		t.Errorf("RaftAddr mismatch: got %v, want %v", parsed.RaftAddr, msg.RaftAddr)
	}
}

// TestMulticastDiscoveryPSK_EncodeDecodeWithoutSecret tests message encoding
// and decoding without PSK authentication (backward compatibility).
func TestMulticastDiscoveryPSK_EncodeDecodeWithoutSecret(t *testing.T) {
	config := &MulticastConfig{
		MulticastGroup:    DefaultMulticastGroup,
		Port:              DefaultMulticastPort,
		BroadcastInterval: DefaultBroadcastInterval,
		PeerTTL:           DefaultPeerTTL,
		ClusterSecret:     "", // No secret
	}

	md, err := NewMulticastDiscovery(config)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	msg := &DiscoveryMessage{
		Type:      MessageQuery,
		NodeID:    67890,
		Address:   "10.0.0.5:8080",
		Timestamp: time.Now().UnixNano(),
	}

	// Encode the message
	encoded, err := md.encodeMessage(msg)
	if err != nil {
		t.Fatalf("Failed to encode message: %v", err)
	}

	// Check wire format without signature: magic(4) + version(1) + payload
	if len(encoded) < 5 {
		t.Fatalf("Encoded message too short: %d bytes", len(encoded))
	}

	// Parse the message back
	parsed, err := md.parseMessage(encoded)
	if err != nil {
		t.Fatalf("Failed to parse message: %v", err)
	}

	// Verify parsed message matches original
	if parsed.Type != msg.Type {
		t.Errorf("Type mismatch: got %v, want %v", parsed.Type, msg.Type)
	}
	if parsed.NodeID != msg.NodeID {
		t.Errorf("NodeID mismatch: got %v, want %v", parsed.NodeID, msg.NodeID)
	}
}

// TestMulticastDiscoveryPSK_RejectInvalidSignature tests that messages
// with invalid signatures are rejected.
func TestMulticastDiscoveryPSK_RejectInvalidSignature(t *testing.T) {
	config := &MulticastConfig{
		MulticastGroup: DefaultMulticastGroup,
		Port:           DefaultMulticastPort,
		ClusterSecret:  "correct-secret",
	}

	md, err := NewMulticastDiscovery(config)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	msg := &DiscoveryMessage{
		Type:      MessageAnnounce,
		NodeID:    11111,
		Address:   "192.168.1.50:8080",
		Timestamp: time.Now().UnixNano(),
	}

	// Encode with correct secret
	encoded, err := md.encodeMessage(msg)
	if err != nil {
		t.Fatalf("Failed to encode message: %v", err)
	}

	// Corrupt the signature
	if len(encoded) > 10 {
		encoded[10] ^= 0xFF // Flip bits in signature
	}

	// Parsing should fail
	_, err = md.parseMessage(encoded)
	if err == nil {
		t.Error("Expected error when parsing message with corrupted signature")
	}
}

// TestMulticastDiscoveryPSK_RejectWrongSecret tests that messages signed
// with a different secret are rejected.
func TestMulticastDiscoveryPSK_RejectWrongSecret(t *testing.T) {
	// Create two discovery instances with different secrets
	config1 := &MulticastConfig{
		MulticastGroup: DefaultMulticastGroup,
		Port:           DefaultMulticastPort,
		ClusterSecret:  "secret-1",
	}
	md1, err := NewMulticastDiscovery(config1)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery 1: %v", err)
	}
	defer md1.Close()

	config2 := &MulticastConfig{
		MulticastGroup: DefaultMulticastGroup,
		Port:           DefaultMulticastPort,
		ClusterSecret:  "secret-2",
	}
	md2, err := NewMulticastDiscovery(config2)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery 2: %v", err)
	}
	defer md2.Close()

	msg := &DiscoveryMessage{
		Type:      MessageResponse,
		NodeID:    22222,
		Address:   "10.0.1.100:8080",
		Timestamp: time.Now().UnixNano(),
	}

	// Encode with secret-1
	encoded, err := md1.encodeMessage(msg)
	if err != nil {
		t.Fatalf("Failed to encode message: %v", err)
	}

	// Try to parse with secret-2 (should fail)
	_, err = md2.parseMessage(encoded)
	if err == nil {
		t.Error("Expected error when parsing message signed with different secret")
	}
}

// TestMulticastDiscoveryPSK_RejectUnauthenticatedMessage tests that when
// PSK is enabled, unauthenticated messages are rejected.
func TestMulticastDiscoveryPSK_RejectUnauthenticatedMessage(t *testing.T) {
	// Create instance without secret
	configNoAuth := &MulticastConfig{
		MulticastGroup: DefaultMulticastGroup,
		Port:           DefaultMulticastPort,
		ClusterSecret:  "",
	}
	mdNoAuth, err := NewMulticastDiscovery(configNoAuth)
	if err != nil {
		t.Fatalf("Failed to create unauthenticated MulticastDiscovery: %v", err)
	}
	defer mdNoAuth.Close()

	// Create instance with secret
	configAuth := &MulticastConfig{
		MulticastGroup: DefaultMulticastGroup,
		Port:           DefaultMulticastPort,
		ClusterSecret:  "my-secret",
	}
	mdAuth, err := NewMulticastDiscovery(configAuth)
	if err != nil {
		t.Fatalf("Failed to create authenticated MulticastDiscovery: %v", err)
	}
	defer mdAuth.Close()

	msg := &DiscoveryMessage{
		Type:      MessageAnnounce,
		NodeID:    33333,
		Address:   "172.16.0.10:8080",
		Timestamp: time.Now().UnixNano(),
	}

	// Encode without authentication
	unauthEncoded, err := mdNoAuth.encodeMessage(msg)
	if err != nil {
		t.Fatalf("Failed to encode unauthenticated message: %v", err)
	}

	// Try to parse with authenticated instance (should fail)
	_, err = mdAuth.parseMessage(unauthEncoded)
	if err == nil {
		t.Error("Expected error when parsing unauthenticated message with PSK enabled")
	}
}

// TestMulticastDiscoveryPSK_AcceptAuthenticatedMessage tests that authenticated
// instances can communicate with each other when using the same secret.
func TestMulticastDiscoveryPSK_AcceptAuthenticatedMessage(t *testing.T) {
	secret := "shared-cluster-secret"

	config1 := &MulticastConfig{
		MulticastGroup: DefaultMulticastGroup,
		Port:           DefaultMulticastPort,
		ClusterSecret:  secret,
	}
	md1, err := NewMulticastDiscovery(config1)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery 1: %v", err)
	}
	defer md1.Close()

	config2 := &MulticastConfig{
		MulticastGroup: DefaultMulticastGroup,
		Port:           DefaultMulticastPort,
		ClusterSecret:  secret,
	}
	md2, err := NewMulticastDiscovery(config2)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery 2: %v", err)
	}
	defer md2.Close()

	msg := &DiscoveryMessage{
		Type:      MessageAnnounce,
		NodeID:    44444,
		Address:   "192.168.100.200:8080",
		RaftAddr:  "192.168.100.200:9000",
		Timestamp: time.Now().UnixNano(),
		Metadata: map[string]string{
			"datacenter": "dc1",
			"rack":       "r1",
		},
	}

	// Encode with md1
	encoded, err := md1.encodeMessage(msg)
	if err != nil {
		t.Fatalf("Failed to encode message: %v", err)
	}

	// Parse with md2 (should succeed)
	parsed, err := md2.parseMessage(encoded)
	if err != nil {
		t.Fatalf("Failed to parse authenticated message: %v", err)
	}

	// Verify message content
	if parsed.NodeID != msg.NodeID {
		t.Errorf("NodeID mismatch: got %v, want %v", parsed.NodeID, msg.NodeID)
	}
	if parsed.Address != msg.Address {
		t.Errorf("Address mismatch: got %v, want %v", parsed.Address, msg.Address)
	}
	if parsed.Metadata["datacenter"] != "dc1" {
		t.Error("Metadata not preserved correctly")
	}
}

// TestMulticastDiscoveryPSK_EmptySecretBackwardCompatibility tests that
// empty secret behaves like unauthenticated mode (backward compatibility).
func TestMulticastDiscoveryPSK_EmptySecretBackwardCompatibility(t *testing.T) {
	config1 := &MulticastConfig{
		MulticastGroup: DefaultMulticastGroup,
		Port:           DefaultMulticastPort,
		ClusterSecret:  "",
	}
	md1, err := NewMulticastDiscovery(config1)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery 1: %v", err)
	}
	defer md1.Close()

	config2 := &MulticastConfig{
		MulticastGroup: DefaultMulticastGroup,
		Port:           DefaultMulticastPort,
		// ClusterSecret not set (defaults to empty)
	}
	md2, err := NewMulticastDiscovery(config2)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery 2: %v", err)
	}
	defer md2.Close()

	msg := &DiscoveryMessage{
		Type:      MessageQuery,
		NodeID:    55555,
		Address:   "10.10.10.10:8080",
		Timestamp: time.Now().UnixNano(),
	}

	// Encode with md1
	encoded, err := md1.encodeMessage(msg)
	if err != nil {
		t.Fatalf("Failed to encode message: %v", err)
	}

	// Parse with md2 (should succeed - both have empty secret)
	parsed, err := md2.parseMessage(encoded)
	if err != nil {
		t.Fatalf("Failed to parse message: %v", err)
	}

	if parsed.NodeID != msg.NodeID {
		t.Errorf("NodeID mismatch: got %v, want %v", parsed.NodeID, msg.NodeID)
	}
}

// TestMulticastDiscoveryPSK_MessageTooShortForAuth tests parsing of
// truncated authenticated messages.
func TestMulticastDiscoveryPSK_MessageTooShortForAuth(t *testing.T) {
	config := &MulticastConfig{
		MulticastGroup: DefaultMulticastGroup,
		Port:           DefaultMulticastPort,
		ClusterSecret:  "secret",
	}
	md, err := NewMulticastDiscovery(config)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "only header",
			data: []byte{0x44, 0x52, 0x41, 0x47, 0x01}, // magic + version
		},
		{
			name: "header + partial sig_len",
			data: []byte{0x44, 0x52, 0x41, 0x47, 0x01, 0x00}, // magic + version + 1 byte
		},
		{
			name: "header + sig_len but no signature",
			data: []byte{0x44, 0x52, 0x41, 0x47, 0x01, 0x00, 0x20}, // magic + version + sig_len=32
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := md.parseMessage(tt.data)
			if err == nil {
				t.Error("Expected error when parsing truncated message")
			}
		})
	}
}

// TestMulticastDiscoveryPSK_ZeroLengthSignature tests rejection of messages
// with zero-length signature field.
func TestMulticastDiscoveryPSK_ZeroLengthSignature(t *testing.T) {
	config := &MulticastConfig{
		MulticastGroup: DefaultMulticastGroup,
		Port:           DefaultMulticastPort,
		ClusterSecret:  "secret",
	}
	md, err := NewMulticastDiscovery(config)
	if err != nil {
		t.Fatalf("Failed to create MulticastDiscovery: %v", err)
	}
	defer md.Close()

	// Create message with sig_len = 0
	payload, _ := json.Marshal(&DiscoveryMessage{
		Type:      MessageAnnounce,
		NodeID:    12345,
		Address:   "192.168.1.1:8080",
		Timestamp: time.Now().UnixNano(),
	})

	data := make([]byte, 4+1+2+len(payload))
	binary.BigEndian.PutUint32(data[0:4], magicNumber)
	data[4] = protocolVersion
	binary.BigEndian.PutUint16(data[5:7], 0) // sig_len = 0
	copy(data[7:], payload)

	_, err = md.parseMessage(data)
	if err == nil {
		t.Error("Expected error when parsing message with zero-length signature")
	}
}

// TestMulticastDiscoveryPSK_Integration tests end-to-end PSK authentication
// with actual peer discovery (if multicast is available).
func TestMulticastDiscoveryPSK_Integration(t *testing.T) {
	// Skip if multicast is not supported
	testAddr, err := net.ResolveUDPAddr("udp4", "239.255.1.1:12345")
	if err != nil {
		t.Skipf("Multicast not supported: %v", err)
	}
	testConn, err := net.ListenMulticastUDP("udp4", nil, testAddr)
	if err != nil {
		t.Skipf("Cannot join multicast group: %v", err)
	}
	testConn.Close()

	secret := "integration-test-secret"

	// Create two nodes with the same secret
	config1 := &MulticastConfig{
		MulticastGroup:    "239.255.77.88",
		Port:              54321,
		BroadcastInterval: 500 * time.Millisecond,
		PeerTTL:           2 * time.Second,
		ClusterSecret:     secret,
	}
	md1, err := NewMulticastDiscovery(config1)
	if err != nil {
		t.Fatalf("Failed to create node 1: %v", err)
	}
	defer md1.Close()

	config2 := &MulticastConfig{
		MulticastGroup:    "239.255.77.88",
		Port:              54321,
		BroadcastInterval: 500 * time.Millisecond,
		PeerTTL:           2 * time.Second,
		ClusterSecret:     secret,
	}
	md2, err := NewMulticastDiscovery(config2)
	if err != nil {
		t.Fatalf("Failed to create node 2: %v", err)
	}
	defer md2.Close()

	// Register nodes
	ctx := context.Background()
	err = md1.Register(ctx, Peer{
		NodeID:   1001,
		Address:  "192.168.1.101:8080",
		RaftAddr: "192.168.1.101:9000",
	})
	if err != nil {
		t.Fatalf("Failed to register node 1: %v", err)
	}

	err = md2.Register(ctx, Peer{
		NodeID:   1002,
		Address:  "192.168.1.102:8080",
		RaftAddr: "192.168.1.102:9000",
	})
	if err != nil {
		t.Fatalf("Failed to register node 2: %v", err)
	}

	// Wait for discovery
	time.Sleep(2 * time.Second)

	// Check that nodes discovered each other
	peers1, err := md1.Discover(ctx)
	if err != nil {
		t.Fatalf("Node 1 discover failed: %v", err)
	}

	peers2, err := md2.Discover(ctx)
	if err != nil {
		t.Fatalf("Node 2 discover failed: %v", err)
	}

	// Each should see the other
	if len(peers1) == 0 {
		t.Error("Node 1 did not discover any peers")
	}

	if len(peers2) == 0 {
		t.Error("Node 2 did not discover any peers")
	}

	// Verify peer details
	foundNode2 := false
	for _, p := range peers1 {
		if p.NodeID == 1002 {
			foundNode2 = true
			if p.Address != "192.168.1.102:8080" {
				t.Errorf("Wrong address for node 2: %s", p.Address)
			}
			break
		}
	}
	if !foundNode2 {
		t.Error("Node 1 did not discover node 2")
	}
}

// BenchmarkEncodeMessageWithPSK benchmarks message encoding with PSK.
func BenchmarkEncodeMessageWithPSK(b *testing.B) {
	config := &MulticastConfig{
		ClusterSecret: "benchmark-secret-key",
	}
	md, _ := NewMulticastDiscovery(config)

	msg := &DiscoveryMessage{
		Type:      MessageAnnounce,
		NodeID:    12345,
		Address:   "192.168.1.100:8080",
		RaftAddr:  "192.168.1.100:9000",
		Timestamp: time.Now().UnixNano(),
		Metadata: map[string]string{
			"region": "us-east-1",
			"az":     "us-east-1a",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		md.encodeMessage(msg)
	}
}

// BenchmarkEncodeMessageWithoutPSK benchmarks message encoding without PSK.
func BenchmarkEncodeMessageWithoutPSK(b *testing.B) {
	config := &MulticastConfig{
		ClusterSecret: "",
	}
	md, _ := NewMulticastDiscovery(config)

	msg := &DiscoveryMessage{
		Type:      MessageAnnounce,
		NodeID:    12345,
		Address:   "192.168.1.100:8080",
		RaftAddr:  "192.168.1.100:9000",
		Timestamp: time.Now().UnixNano(),
		Metadata: map[string]string{
			"region": "us-east-1",
			"az":     "us-east-1a",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		md.encodeMessage(msg)
	}
}

// BenchmarkParseMessageWithPSK benchmarks message parsing with PSK.
func BenchmarkParseMessageWithPSK(b *testing.B) {
	config := &MulticastConfig{
		ClusterSecret: "benchmark-secret-key",
	}
	md, _ := NewMulticastDiscovery(config)

	msg := &DiscoveryMessage{
		Type:      MessageAnnounce,
		NodeID:    12345,
		Address:   "192.168.1.100:8080",
		RaftAddr:  "192.168.1.100:9000",
		Timestamp: time.Now().UnixNano(),
	}

	encoded, _ := md.encodeMessage(msg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		md.parseMessage(encoded)
	}
}
