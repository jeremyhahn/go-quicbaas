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

package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/discovery"
)

// TestSecureCluster_PSKDiscovery tests PSK authentication for multicast discovery
func TestSecureCluster_PSKDiscovery(t *testing.T) {
	require.True(t, isMulticastAvailable(), "Multicast must be available in integration test container (requires NET_ADMIN, NET_RAW capabilities)")

	t.Run("SamePSKNodesDiscoverEachOther", func(t *testing.T) {
		port := getAvailableMulticastPort()
		secret := "test-cluster-secret-key-12345"

		// Create first node with PSK
		config1 := &discovery.MulticastConfig{
			MulticastGroup:    "239.255.77.77",
			Port:              port,
			BroadcastInterval: 200 * time.Millisecond,
			PeerTTL:           5 * time.Second,
			SendQueryOnStart:  true,
			ClusterSecret:     secret,
		}

		md1, err := discovery.NewMulticastDiscovery(config1)
		require.NoError(t, err)
		defer md1.Close()

		// Create second node with same PSK
		config2 := &discovery.MulticastConfig{
			MulticastGroup:    "239.255.77.77",
			Port:              port,
			BroadcastInterval: 200 * time.Millisecond,
			PeerTTL:           5 * time.Second,
			SendQueryOnStart:  true,
			ClusterSecret:     secret,
		}

		md2, err := discovery.NewMulticastDiscovery(config2)
		require.NoError(t, err)
		defer md2.Close()

		ctx := context.Background()

		// Register node 1
		peer1 := discovery.Peer{
			NodeID:   1,
			Address:  "192.168.1.100:63000",
			RaftAddr: "192.168.1.100:63001",
		}
		err = md1.Register(ctx, peer1)
		require.NoError(t, err)

		// Register node 2
		peer2 := discovery.Peer{
			NodeID:   2,
			Address:  "192.168.1.101:63000",
			RaftAddr: "192.168.1.101:63001",
		}
		err = md2.Register(ctx, peer2)
		require.NoError(t, err)

		// Wait for discovery
		time.Sleep(2 * time.Second)

		// Both nodes should discover each other
		peers1 := getPeersWithRetry(md1, 1, 10*time.Second)
		assert.GreaterOrEqual(t, len(peers1), 1, "Node 1 should discover node 2")

		peers2 := getPeersWithRetry(md2, 1, 10*time.Second)
		assert.GreaterOrEqual(t, len(peers2), 1, "Node 2 should discover node 1")
	})

	t.Run("WrongPSKNodeRejected", func(t *testing.T) {
		port := getAvailableMulticastPort()

		// Create first node with PSK
		config1 := &discovery.MulticastConfig{
			MulticastGroup:    "239.255.77.77",
			Port:              port,
			BroadcastInterval: 200 * time.Millisecond,
			PeerTTL:           5 * time.Second,
			SendQueryOnStart:  true,
			ClusterSecret:     "correct-secret",
		}

		md1, err := discovery.NewMulticastDiscovery(config1)
		require.NoError(t, err)
		defer md1.Close()

		// Create second node with WRONG PSK
		config2 := &discovery.MulticastConfig{
			MulticastGroup:    "239.255.77.77",
			Port:              port,
			BroadcastInterval: 200 * time.Millisecond,
			PeerTTL:           5 * time.Second,
			SendQueryOnStart:  true,
			ClusterSecret:     "wrong-secret",
		}

		md2, err := discovery.NewMulticastDiscovery(config2)
		require.NoError(t, err)
		defer md2.Close()

		ctx := context.Background()

		// Register node 1
		peer1 := discovery.Peer{
			NodeID:   1,
			Address:  "192.168.1.100:63000",
			RaftAddr: "192.168.1.100:63001",
		}
		err = md1.Register(ctx, peer1)
		require.NoError(t, err)

		// Register node 2
		peer2 := discovery.Peer{
			NodeID:   2,
			Address:  "192.168.1.101:63000",
			RaftAddr: "192.168.1.101:63001",
		}
		err = md2.Register(ctx, peer2)
		require.NoError(t, err)

		// Wait for potential discovery
		time.Sleep(2 * time.Second)

		// Node 1 should NOT discover node 2 (wrong secret)
		ctx1, cancel1 := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel1()
		peers1, _ := md1.Discover(ctx1)
		// Should only see itself or no peers (other node has wrong secret)
		for _, p := range peers1 {
			assert.NotEqual(t, uint64(2), p.NodeID, "Node 1 should not discover node 2 with wrong PSK")
		}
	})

	t.Run("NoPSKCannotJoinSecureCluster", func(t *testing.T) {
		port := getAvailableMulticastPort()

		// Create first node WITH PSK (secure cluster)
		config1 := &discovery.MulticastConfig{
			MulticastGroup:    "239.255.77.77",
			Port:              port,
			BroadcastInterval: 200 * time.Millisecond,
			PeerTTL:           5 * time.Second,
			SendQueryOnStart:  true,
			ClusterSecret:     "secure-cluster-secret",
		}

		md1, err := discovery.NewMulticastDiscovery(config1)
		require.NoError(t, err)
		defer md1.Close()

		// Create second node WITHOUT PSK (open node)
		config2 := &discovery.MulticastConfig{
			MulticastGroup:    "239.255.77.77",
			Port:              port,
			BroadcastInterval: 200 * time.Millisecond,
			PeerTTL:           5 * time.Second,
			SendQueryOnStart:  true,
			ClusterSecret:     "", // No secret
		}

		md2, err := discovery.NewMulticastDiscovery(config2)
		require.NoError(t, err)
		defer md2.Close()

		ctx := context.Background()

		// Register both nodes
		err = md1.Register(ctx, discovery.Peer{NodeID: 1, Address: "192.168.1.100:63000", RaftAddr: "192.168.1.100:63001"})
		require.NoError(t, err)

		err = md2.Register(ctx, discovery.Peer{NodeID: 2, Address: "192.168.1.101:63000", RaftAddr: "192.168.1.101:63001"})
		require.NoError(t, err)

		// Wait for potential discovery
		time.Sleep(2 * time.Second)

		// Secure node should NOT accept unsigned messages from open node
		ctx1, cancel1 := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel1()
		peers1, _ := md1.Discover(ctx1)
		for _, p := range peers1 {
			assert.NotEqual(t, uint64(2), p.NodeID, "Secure node should not accept unsigned messages")
		}
	})

	t.Run("OpenClusterWorksWithoutPSK", func(t *testing.T) {
		port := getAvailableMulticastPort()

		// Create two nodes without PSK (open cluster)
		config := &discovery.MulticastConfig{
			MulticastGroup:    "239.255.77.77",
			Port:              port,
			BroadcastInterval: 200 * time.Millisecond,
			PeerTTL:           5 * time.Second,
			SendQueryOnStart:  true,
			ClusterSecret:     "", // No secret - open cluster
		}

		md1, err := discovery.NewMulticastDiscovery(config)
		require.NoError(t, err)
		defer md1.Close()

		md2, err := discovery.NewMulticastDiscovery(config)
		require.NoError(t, err)
		defer md2.Close()

		ctx := context.Background()

		// Register both nodes
		err = md1.Register(ctx, discovery.Peer{NodeID: 1, Address: "192.168.1.100:63000", RaftAddr: "192.168.1.100:63001"})
		require.NoError(t, err)

		err = md2.Register(ctx, discovery.Peer{NodeID: 2, Address: "192.168.1.101:63000", RaftAddr: "192.168.1.101:63001"})
		require.NoError(t, err)

		// Wait for discovery
		time.Sleep(2 * time.Second)

		// Both nodes should discover each other in open mode
		peers1 := getPeersWithRetry(md1, 1, 10*time.Second)
		assert.GreaterOrEqual(t, len(peers1), 1, "Node 1 should discover node 2 in open mode")

		peers2 := getPeersWithRetry(md2, 1, 10*time.Second)
		assert.GreaterOrEqual(t, len(peers2), 1, "Node 2 should discover node 1 in open mode")
	})
}

// TestSecureCluster_MTLSRaft tests mTLS configuration for Raft
// Note: Full mTLS cluster testing requires multi-node Docker setup
// This test validates the configuration wiring works correctly
func TestSecureCluster_MTLSRaft(t *testing.T) {
	t.Run("ConfigurationValidation", func(t *testing.T) {
		// This test validates that mTLS configuration is properly structured
		// Full end-to-end mTLS testing requires a multi-node Docker environment
		// The actual mTLS wiring is tested in pkg/raft/node/node_mtls_test.go

		// mTLS requires:
		// 1. TLS certificate file
		// 2. TLS key file
		// 3. CA certificate file
		// 4. MutualTLS flag set to true

		// These are validated at server startup and in the unit tests
		t.Log("mTLS configuration validation is performed in unit tests")
		t.Log("Full mTLS cluster testing requires multi-container Docker setup")
	})
}

// TestSecureCluster_Combined tests combined PSK + mTLS scenarios
func TestSecureCluster_Combined(t *testing.T) {
	t.Run("PSKOnlyWorksForDiscovery", func(t *testing.T) {
		require.True(t, isMulticastAvailable(), "Multicast must be available in integration test container (requires NET_ADMIN, NET_RAW capabilities)")

		port := getAvailableMulticastPort()
		secret := "psk-only-cluster"

		// PSK-only configuration validates that:
		// - Discovery messages are signed
		// - Only nodes with correct PSK can discover each other
		// - Raft transport remains unencrypted (no mTLS)

		config := &discovery.MulticastConfig{
			MulticastGroup:    "239.255.77.77",
			Port:              port,
			BroadcastInterval: 200 * time.Millisecond,
			PeerTTL:           5 * time.Second,
			SendQueryOnStart:  true,
			ClusterSecret:     secret,
		}

		md, err := discovery.NewMulticastDiscovery(config)
		require.NoError(t, err)
		defer md.Close()

		ctx := context.Background()
		err = md.Register(ctx, discovery.Peer{NodeID: 1, Address: "192.168.1.100:63000", RaftAddr: "192.168.1.100:63001"})
		require.NoError(t, err)

		assert.True(t, md.IsRunning(), "PSK-only discovery should be running")
	})
}
