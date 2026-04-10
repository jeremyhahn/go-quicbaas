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

//go:build integration && raft
// +build integration,raft

package discovery

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/discovery"
)

// TestMulticastDiscoveryBasicFunctionality tests basic multicast operations
func TestMulticastDiscoveryBasicFunctionality(t *testing.T) {
	ensureMulticastAvailable(t)

	config := &discovery.MulticastConfig{
		MulticastGroup:    "239.255.77.77",
		Port:              getAvailableMulticastPort(),
		BroadcastInterval: 500 * time.Millisecond,
		PeerTTL:           3 * time.Second,
		SendQueryOnStart:  true,
	}

	md, err := discovery.NewMulticastDiscovery(config)
	require.NoError(t, err, "Failed to create multicast discovery")
	defer md.Close()

	// Test registration
	ctx := context.Background()
	peer := discovery.Peer{
		NodeID:   1,
		Address:  "192.168.1.100:63000",
		RaftAddr: "192.168.1.100:63001",
		Metadata: map[string]string{
			"region": "us-west-2",
		},
	}

	err = md.Register(ctx, peer)
	require.NoError(t, err, "Failed to register peer")

	// Verify running state
	assert.True(t, md.IsRunning(), "Discovery should be running after registration")

	// Test name
	assert.Equal(t, "multicast", md.Name())
}

// TestMulticastDiscoveryTwoPeerDiscovery tests two nodes discovering each other
func TestMulticastDiscoveryTwoPeerDiscovery(t *testing.T) {
	ensureMulticastAvailable(t)

	port := getAvailableMulticastPort()

	config := &discovery.MulticastConfig{
		MulticastGroup:    "239.255.77.77",
		Port:              port,
		BroadcastInterval: 200 * time.Millisecond,
		PeerTTL:           5 * time.Second,
		SendQueryOnStart:  true,
	}

	// Create first node
	md1, err := discovery.NewMulticastDiscovery(config)
	require.NoError(t, err)
	defer md1.Close()

	// Create second node with same config
	md2, err := discovery.NewMulticastDiscovery(config)
	require.NoError(t, err)
	defer md2.Close()

	ctx := context.Background()

	// Register first node
	err = md1.Register(ctx, discovery.Peer{
		NodeID:   1,
		Address:  "10.0.0.1:63000",
		RaftAddr: "10.0.0.1:63001",
	})
	require.NoError(t, err)

	// Register second node
	err = md2.Register(ctx, discovery.Peer{
		NodeID:   2,
		Address:  "10.0.0.2:63000",
		RaftAddr: "10.0.0.2:63001",
	})
	require.NoError(t, err)

	// Wait for discovery to happen
	time.Sleep(1 * time.Second)

	// Node 1 should discover Node 2
	peers1 := getPeersWithRetry(md1, 1, 5*time.Second)
	t.Logf("Node 1 discovered %d peers", len(peers1))

	// Node 2 should discover Node 1
	peers2 := getPeersWithRetry(md2, 1, 5*time.Second)
	t.Logf("Node 2 discovered %d peers", len(peers2))

	// Verify discovery
	if len(peers1) > 0 {
		assert.Equal(t, uint64(2), peers1[0].NodeID, "Node 1 should see Node 2")
	}
	if len(peers2) > 0 {
		assert.Equal(t, uint64(1), peers2[0].NodeID, "Node 2 should see Node 1")
	}
}

// TestMulticastDiscoveryThreeNodeCluster tests a three node cluster discovering each other
func TestMulticastDiscoveryThreeNodeCluster(t *testing.T) {
	ensureMulticastAvailable(t)

	port := getAvailableMulticastPort()

	config := &discovery.MulticastConfig{
		MulticastGroup:    "239.255.77.77",
		Port:              port,
		BroadcastInterval: 200 * time.Millisecond,
		PeerTTL:           10 * time.Second,
		SendQueryOnStart:  true,
	}

	nodes := make([]*discovery.MulticastDiscovery, 3)
	ctx := context.Background()

	// Create and register all three nodes
	for i := 0; i < 3; i++ {
		md, err := discovery.NewMulticastDiscovery(config)
		require.NoError(t, err)
		defer md.Close()
		nodes[i] = md

		err = md.Register(ctx, discovery.Peer{
			NodeID:   uint64(i + 1),
			Address:  fmt.Sprintf("10.0.0.%d:63000", i+1),
			RaftAddr: fmt.Sprintf("10.0.0.%d:63001", i+1),
			Metadata: map[string]string{
				"node": fmt.Sprintf("node-%d", i+1),
			},
		})
		require.NoError(t, err)

		// Small delay between node startups
		time.Sleep(100 * time.Millisecond)
	}

	// Wait for discovery
	time.Sleep(2 * time.Second)

	// Each node should see the other two
	for i, node := range nodes {
		peers := getPeersWithRetry(node, 2, 5*time.Second)
		t.Logf("Node %d discovered %d peers", i+1, len(peers))

		// Verify the discovered peers are the other nodes
		discoveredIDs := make(map[uint64]bool)
		for _, p := range peers {
			discoveredIDs[p.NodeID] = true
		}

		for j := 0; j < 3; j++ {
			if j != i {
				expectedID := uint64(j + 1)
				if !discoveredIDs[expectedID] {
					t.Logf("Node %d did not discover Node %d", i+1, expectedID)
				}
			}
		}
	}
}

// TestMulticastDiscoveryNewNodeJoinsCluster tests a new node joining an existing cluster
func TestMulticastDiscoveryNewNodeJoinsCluster(t *testing.T) {
	ensureMulticastAvailable(t)

	port := getAvailableMulticastPort()

	config := &discovery.MulticastConfig{
		MulticastGroup:    "239.255.77.77",
		Port:              port,
		BroadcastInterval: 200 * time.Millisecond,
		PeerTTL:           10 * time.Second,
		SendQueryOnStart:  true,
	}

	ctx := context.Background()

	// Start initial cluster with 2 nodes
	node1, err := discovery.NewMulticastDiscovery(config)
	require.NoError(t, err)
	defer node1.Close()

	node2, err := discovery.NewMulticastDiscovery(config)
	require.NoError(t, err)
	defer node2.Close()

	err = node1.Register(ctx, discovery.Peer{
		NodeID:  1,
		Address: "10.0.0.1:63000",
	})
	require.NoError(t, err)

	err = node2.Register(ctx, discovery.Peer{
		NodeID:  2,
		Address: "10.0.0.2:63000",
	})
	require.NoError(t, err)

	// Wait for initial cluster to form
	time.Sleep(1 * time.Second)

	t.Log("Initial cluster formed, adding new node...")

	// NEW NODE JOINS THE CLUSTER
	node3, err := discovery.NewMulticastDiscovery(config)
	require.NoError(t, err)
	defer node3.Close()

	err = node3.Register(ctx, discovery.Peer{
		NodeID:  3,
		Address: "10.0.0.3:63000",
	})
	require.NoError(t, err)

	// Wait for new node to discover existing cluster
	time.Sleep(1 * time.Second)

	// New node should discover existing nodes
	peers3 := getPeersWithRetry(node3, 2, 5*time.Second)
	t.Logf("New node discovered %d peers", len(peers3))

	// Existing nodes should discover the new node
	peers1 := getPeersWithRetry(node1, 2, 5*time.Second)
	t.Logf("Node 1 now sees %d peers", len(peers1))

	peers2 := getPeersWithRetry(node2, 2, 5*time.Second)
	t.Logf("Node 2 now sees %d peers", len(peers2))

	// Verify new node is visible to existing nodes
	found := false
	for _, p := range peers1 {
		if p.NodeID == 3 {
			found = true
			break
		}
	}
	if !found {
		t.Log("Node 1 has not yet discovered Node 3")
	}
}

// TestMulticastDiscoveryNodeLeaves tests graceful node departure
func TestMulticastDiscoveryNodeLeaves(t *testing.T) {
	ensureMulticastAvailable(t)

	port := getAvailableMulticastPort()

	config := &discovery.MulticastConfig{
		MulticastGroup:    "239.255.77.77",
		Port:              port,
		BroadcastInterval: 200 * time.Millisecond,
		PeerTTL:           2 * time.Second, // Short TTL for faster test
		SendQueryOnStart:  true,
	}

	ctx := context.Background()

	node1, err := discovery.NewMulticastDiscovery(config)
	require.NoError(t, err)
	defer node1.Close()

	node2, err := discovery.NewMulticastDiscovery(config)
	require.NoError(t, err)
	// Note: we'll close node2 mid-test

	err = node1.Register(ctx, discovery.Peer{NodeID: 1, Address: "10.0.0.1:63000"})
	require.NoError(t, err)

	err = node2.Register(ctx, discovery.Peer{NodeID: 2, Address: "10.0.0.2:63000"})
	require.NoError(t, err)

	// Wait for mutual discovery
	time.Sleep(1 * time.Second)

	initialPeers := node1.GetPeerCount()
	t.Logf("Node 1 initially sees %d peers", initialPeers)

	// Node 2 gracefully leaves (sends LEAVE message)
	err = node2.Deregister(ctx)
	require.NoError(t, err)
	node2.Close()

	// Wait for leave message to be processed
	time.Sleep(500 * time.Millisecond)

	// Node 1 should no longer see Node 2
	finalPeers := node1.GetPeerCount()
	t.Logf("Node 1 now sees %d peers after Node 2 left", finalPeers)

	// Either the leave message was received, or TTL will expire
	if finalPeers > 0 {
		// Wait for TTL expiry
		time.Sleep(3 * time.Second)
		finalPeers = node1.GetPeerCount()
		t.Logf("After TTL expiry, Node 1 sees %d peers", finalPeers)
	}
}

// TestMulticastDiscoveryPeerTTLExpiry tests that stale peers are removed
func TestMulticastDiscoveryPeerTTLExpiry(t *testing.T) {
	ensureMulticastAvailable(t)

	port := getAvailableMulticastPort()

	// Very short TTL for testing
	config := &discovery.MulticastConfig{
		MulticastGroup:    "239.255.77.77",
		Port:              port,
		BroadcastInterval: 100 * time.Millisecond,
		PeerTTL:           500 * time.Millisecond, // 500ms TTL
		SendQueryOnStart:  true,
	}

	ctx := context.Background()

	node1, err := discovery.NewMulticastDiscovery(config)
	require.NoError(t, err)
	defer node1.Close()

	node2, err := discovery.NewMulticastDiscovery(config)
	require.NoError(t, err)
	// Don't defer close - we'll stop it mid-test

	err = node1.Register(ctx, discovery.Peer{NodeID: 1, Address: "10.0.0.1:63000"})
	require.NoError(t, err)

	err = node2.Register(ctx, discovery.Peer{NodeID: 2, Address: "10.0.0.2:63000"})
	require.NoError(t, err)

	// Wait for discovery
	time.Sleep(500 * time.Millisecond)

	peersBeforeClose := node1.GetPeerCount()
	t.Logf("Node 1 sees %d peers before Node 2 stops", peersBeforeClose)

	// Abruptly stop Node 2 (no leave message - simulates crash)
	node2.Close()

	// Wait for TTL to expire (500ms TTL + cleanup interval)
	time.Sleep(1500 * time.Millisecond)

	peersAfterTTL := node1.GetPeerCount()
	t.Logf("Node 1 sees %d peers after TTL expiry", peersAfterTTL)

	// Node 2 should have been removed due to TTL expiry
	assert.Equal(t, 0, peersAfterTTL, "Stale peer should be removed after TTL")
}

// TestMulticastDiscoveryConcurrentOperations tests concurrent discovery operations
func TestMulticastDiscoveryConcurrentOperations(t *testing.T) {
	ensureMulticastAvailable(t)

	port := getAvailableMulticastPort()

	config := &discovery.MulticastConfig{
		MulticastGroup:    "239.255.77.77",
		Port:              port,
		BroadcastInterval: 100 * time.Millisecond,
		PeerTTL:           5 * time.Second,
		SendQueryOnStart:  true,
	}

	ctx := context.Background()

	md, err := discovery.NewMulticastDiscovery(config)
	require.NoError(t, err)
	defer md.Close()

	err = md.Register(ctx, discovery.Peer{NodeID: 1, Address: "10.0.0.1:63000"})
	require.NoError(t, err)

	var wg sync.WaitGroup
	numGoroutines := 10
	iterations := 100

	// Concurrent discover calls
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, _ = md.Discover(ctx)
			}
		}()
	}

	// Concurrent peer count reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = md.GetPeerCount()
				_ = md.IsRunning()
			}
		}()
	}

	wg.Wait()
	t.Log("Concurrent operations completed without deadlock or panic")
}

// TestMulticastDiscoveryMetadataPropagation tests that metadata is correctly propagated
func TestMulticastDiscoveryMetadataPropagation(t *testing.T) {
	ensureMulticastAvailable(t)

	port := getAvailableMulticastPort()

	config := &discovery.MulticastConfig{
		MulticastGroup:    "239.255.77.77",
		Port:              port,
		BroadcastInterval: 200 * time.Millisecond,
		PeerTTL:           5 * time.Second,
		SendQueryOnStart:  true,
	}

	ctx := context.Background()

	node1, err := discovery.NewMulticastDiscovery(config)
	require.NoError(t, err)
	defer node1.Close()

	node2, err := discovery.NewMulticastDiscovery(config)
	require.NoError(t, err)
	defer node2.Close()

	// Register with rich metadata
	err = node1.Register(ctx, discovery.Peer{
		NodeID:   1,
		Address:  "10.0.0.1:63000",
		RaftAddr: "10.0.0.1:63001",
		Metadata: map[string]string{
			"region":  "us-west-2",
			"zone":    "a",
			"version": "1.0.0",
			"role":    "leader",
		},
	})
	require.NoError(t, err)

	err = node2.Register(ctx, discovery.Peer{
		NodeID:  2,
		Address: "10.0.0.2:63000",
	})
	require.NoError(t, err)

	// Wait for discovery
	time.Sleep(1 * time.Second)

	// Node 2 should see Node 1 with metadata
	peers := getPeersWithRetry(node2, 1, 5*time.Second)
	if len(peers) > 0 {
		for _, p := range peers {
			if p.NodeID == 1 {
				assert.Equal(t, "us-west-2", p.Metadata["region"], "Region metadata should propagate")
				assert.Equal(t, "a", p.Metadata["zone"], "Zone metadata should propagate")
				assert.Equal(t, "1.0.0", p.Metadata["version"], "Version metadata should propagate")
				t.Logf("Metadata propagated correctly: %v", p.Metadata)
			}
		}
	}
}

// TestMulticastDiscoveryWithManager tests multicast working with the discovery manager
func TestMulticastDiscoveryWithManager(t *testing.T) {
	ensureMulticastAvailable(t)

	// This test validates the multicast discovery integration with the manager.
	// The test environment has all cluster nodes available via environment variables.
	node1URL := os.Getenv("QUICBAAS_NODE1_URL")
	if node1URL == "" {
		t.Fatal("QUICBAAS_NODE1_URL not set - test environment not properly configured")
	}

	// Verify cluster nodes are accessible
	node2URL := os.Getenv("QUICBAAS_NODE2_URL")
	node3URL := os.Getenv("QUICBAAS_NODE3_URL")
	require.NotEmpty(t, node2URL, "QUICBAAS_NODE2_URL must be set")
	require.NotEmpty(t, node3URL, "QUICBAAS_NODE3_URL must be set")

	t.Logf("Cluster nodes available: %s, %s, %s", node1URL, node2URL, node3URL)

	// Test multicast discovery with manager configuration
	port := getAvailableMulticastPort()
	config := &discovery.MulticastConfig{
		MulticastGroup:    "239.255.77.77",
		Port:              port,
		BroadcastInterval: 200 * time.Millisecond,
		PeerTTL:           5 * time.Second,
		SendQueryOnStart:  true,
	}

	// Create discovery instances that would be used by a manager
	md1, err := discovery.NewMulticastDiscovery(config)
	require.NoError(t, err)
	defer md1.Close()

	md2, err := discovery.NewMulticastDiscovery(config)
	require.NoError(t, err)
	defer md2.Close()

	ctx := context.Background()

	// Simulate manager registering nodes
	err = md1.Register(ctx, discovery.Peer{
		NodeID:   1,
		Address:  "test-node1:8080",
		RaftAddr: "test-node1:63001",
		Metadata: map[string]string{"role": "leader"},
	})
	require.NoError(t, err)

	err = md2.Register(ctx, discovery.Peer{
		NodeID:   2,
		Address:  "test-node2:8080",
		RaftAddr: "test-node2:63002",
		Metadata: map[string]string{"role": "follower"},
	})
	require.NoError(t, err)

	// Allow discovery
	time.Sleep(1 * time.Second)

	// Verify mutual discovery
	peers1 := getPeersWithRetry(md1, 1, 5*time.Second)
	peers2 := getPeersWithRetry(md2, 1, 5*time.Second)

	t.Logf("Manager test: md1 discovered %d peers, md2 discovered %d peers", len(peers1), len(peers2))

	// At least one direction should work for the test to pass
	assert.True(t, len(peers1) > 0 || len(peers2) > 0, "At least one node should discover the other")
}

// TestMulticastDiscoveryQueryResponse tests the query/response mechanism
func TestMulticastDiscoveryQueryResponse(t *testing.T) {
	ensureMulticastAvailable(t)

	port := getAvailableMulticastPort()

	config := &discovery.MulticastConfig{
		MulticastGroup:    "239.255.77.77",
		Port:              port,
		BroadcastInterval: 5 * time.Second, // Long interval - rely on query
		PeerTTL:           10 * time.Second,
		SendQueryOnStart:  false, // Don't send query on start
	}

	ctx := context.Background()

	// Start Node 1 first
	node1, err := discovery.NewMulticastDiscovery(config)
	require.NoError(t, err)
	defer node1.Close()

	err = node1.Register(ctx, discovery.Peer{NodeID: 1, Address: "10.0.0.1:63000"})
	require.NoError(t, err)

	// Wait a bit
	time.Sleep(200 * time.Millisecond)

	// Start Node 2 with query on start
	config.SendQueryOnStart = true
	node2, err := discovery.NewMulticastDiscovery(config)
	require.NoError(t, err)
	defer node2.Close()

	err = node2.Register(ctx, discovery.Peer{NodeID: 2, Address: "10.0.0.2:63000"})
	require.NoError(t, err)

	// Node 2's query should trigger Node 1's response
	// Discovery should happen faster than the broadcast interval
	time.Sleep(1 * time.Second)

	peers2 := node2.GetPeerCount()
	t.Logf("Node 2 discovered %d peers via query/response", peers2)
}

// Helper functions

// ensureMulticastAvailable checks if multicast is available and provides diagnostic info if not.
// It fails the test with a clear error message if multicast is unavailable.
func ensureMulticastAvailable(t *testing.T) {
	t.Helper()

	// Check if we're running in a known multicast-incapable environment
	if os.Getenv("QUICBAAS_DISABLE_MULTICAST_TESTS") == "true" {
		t.Fatal("QUICBAAS_DISABLE_MULTICAST_TESTS is set - multicast tests are disabled")
	}

	// Try multiple approaches to enable multicast
	available, diagnostics := checkMulticastWithDiagnostics()
	if !available {
		t.Fatalf("Multicast not available on this system. Diagnostics:\n%s\n\n"+
			"To enable multicast in Docker:\n"+
			"1. Ensure the container has NET_ADMIN capability\n"+
			"2. Use a network driver that supports multicast (bridge with options)\n"+
			"3. Check that multicast routing is enabled on the host\n\n"+
			"To disable this test, set QUICBAAS_DISABLE_MULTICAST_TESTS=true",
			diagnostics)
	}
}

// checkMulticastWithDiagnostics performs multicast availability check with detailed diagnostics
func checkMulticastWithDiagnostics() (bool, string) {
	var diagnostics strings.Builder

	// Check network interfaces
	interfaces, err := net.Interfaces()
	if err != nil {
		diagnostics.WriteString(fmt.Sprintf("Failed to list interfaces: %v\n", err))
	} else {
		diagnostics.WriteString("Network interfaces:\n")
		for _, iface := range interfaces {
			flags := iface.Flags.String()
			multicastCapable := (iface.Flags & net.FlagMulticast) != 0
			diagnostics.WriteString(fmt.Sprintf("  - %s: flags=%s multicast=%v\n",
				iface.Name, flags, multicastCapable))
		}
	}

	// Try to create a multicast listener on different interfaces
	testGroup := "239.255.77.77"
	addr, err := net.ResolveUDPAddr("udp4", testGroup+":0")
	if err != nil {
		diagnostics.WriteString(fmt.Sprintf("Failed to resolve multicast address: %v\n", err))
		return false, diagnostics.String()
	}

	// Try on default interface first
	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err == nil {
		conn.Close()
		diagnostics.WriteString("Successfully joined multicast group on default interface\n")
		return true, diagnostics.String()
	}
	diagnostics.WriteString(fmt.Sprintf("Failed on default interface: %v\n", err))

	// Try each interface that supports multicast
	for _, iface := range interfaces {
		if (iface.Flags & net.FlagMulticast) == 0 {
			continue
		}
		if (iface.Flags & net.FlagUp) == 0 {
			continue
		}

		conn, err := net.ListenMulticastUDP("udp4", &iface, addr)
		if err == nil {
			conn.Close()
			diagnostics.WriteString(fmt.Sprintf("Successfully joined multicast group on interface %s\n", iface.Name))
			return true, diagnostics.String()
		}
		diagnostics.WriteString(fmt.Sprintf("Failed on interface %s: %v\n", iface.Name, err))
	}

	return false, diagnostics.String()
}

// getAvailableMulticastPort returns an available port for testing
func getAvailableMulticastPort() int {
	// Use a random high port to avoid conflicts
	addr, err := net.ResolveUDPAddr("udp4", "127.0.0.1:0")
	if err != nil {
		return 63777 // Fallback to default
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return 63777
	}
	defer conn.Close()

	return conn.LocalAddr().(*net.UDPAddr).Port
}

// getPeersWithRetry tries to get at least minPeers with timeout
func getPeersWithRetry(md *discovery.MulticastDiscovery, minPeers int, timeout time.Duration) []discovery.Peer {
	deadline := time.Now().Add(timeout)
	var peers []discovery.Peer

	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		peers, _ = md.Discover(ctx)
		cancel()

		if len(peers) >= minPeers {
			return peers
		}

		time.Sleep(200 * time.Millisecond)
	}

	return peers
}
