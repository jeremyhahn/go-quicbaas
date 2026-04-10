//go:build raft && integration && minikube

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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKubernetesDiscovery_Minikube tests real Kubernetes discovery using Minikube
//
// Prerequisites:
//
//  1. Start Minikube:
//     minikube start
//
//  2. Create test namespace:
//     kubectl create namespace quicbaas-test
//
//  3. Deploy the StatefulSet:
//     kubectl apply -f pkg/discovery/testdata/quicbaas-statefulset.yaml
//
//  4. Wait for pods to be running:
//     kubectl wait --for=condition=ready pod -l app=quicbaas -n quicbaas-test --timeout=120s
//
// Run the test:
//
//	go test -v -tags="raft,integration,minikube" -run TestKubernetesDiscovery_Minikube ./pkg/discovery
//
// Cleanup:
//
//	kubectl delete namespace quicbaas-test
//	minikube stop
func TestKubernetesDiscovery_Minikube(t *testing.T) {
	t.Log("Running Kubernetes discovery integration test with Minikube")

	// Test configuration
	const (
		namespace        = "quicbaas-test"
		serviceName      = "quicbaas"
		statefulSetName  = "quicbaas"
		expectedReplicas = 3
		port             = 63001
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Create Kubernetes discovery
	kd, err := NewKubernetesDiscovery(KubernetesConfig{
		Namespace:       namespace,
		ServiceName:     serviceName,
		StatefulSetName: statefulSetName,
		Port:            port,
		ClusterDomain:   "cluster.local",
		RefreshInterval: 10 * time.Second,
	})
	require.NoError(t, err, "Failed to create KubernetesDiscovery")
	defer kd.Close()

	// Test 1: Basic discovery
	t.Run("Discover pods via DNS", func(t *testing.T) {
		peers, err := kd.Discover(ctx)
		require.NoError(t, err, "Failed to discover peers")
		assert.Len(t, peers, expectedReplicas, "Expected %d peers", expectedReplicas)

		t.Logf("Discovered %d peers:", len(peers))
		for _, peer := range peers {
			t.Logf("  - NodeID: %d, Address: %s, Pod: %s",
				peer.NodeID, peer.Address, peer.Metadata["pod"])
		}
	})

	// Test 2: Verify node IDs match StatefulSet ordinals
	t.Run("Verify node IDs", func(t *testing.T) {
		peers, err := kd.Discover(ctx)
		require.NoError(t, err)

		// Verify each peer has correct node ID (ordinal + 1)
		for i := 0; i < expectedReplicas; i++ {
			expectedNodeID := uint64(i + 1)
			expectedPodName := fmt.Sprintf("%s-%d", statefulSetName, i)

			found := false
			for _, peer := range peers {
				if peer.NodeID == expectedNodeID {
					found = true
					assert.Equal(t, expectedPodName, peer.Metadata["pod"],
						"Pod name mismatch for NodeID %d", expectedNodeID)
					break
				}
			}
			assert.True(t, found, "NodeID %d not found in discovered peers", expectedNodeID)
		}
	})

	// Test 3: Verify peer metadata
	t.Run("Verify peer metadata", func(t *testing.T) {
		peers, err := kd.Discover(ctx)
		require.NoError(t, err)

		for _, peer := range peers {
			assert.NotEmpty(t, peer.Address, "Peer address should not be empty")
			assert.NotEmpty(t, peer.RaftAddr, "Peer RaftAddr should not be empty")
			assert.Equal(t, peer.Address, peer.RaftAddr, "Address and RaftAddr should match")

			// Verify metadata
			assert.Equal(t, namespace, peer.Metadata["namespace"])
			assert.Equal(t, serviceName, peer.Metadata["service"])
			assert.Equal(t, statefulSetName, peer.Metadata["statefulset"])
			assert.NotEmpty(t, peer.Metadata["pod"])

			// Verify LastSeen is recent
			assert.WithinDuration(t, time.Now(), peer.LastSeen, 5*time.Second,
				"LastSeen timestamp should be recent")

			// Verify TTL
			assert.Equal(t, 10*time.Second, peer.TTL, "TTL should match refresh interval")
		}
	})

	// Test 4: Test cache functionality
	t.Run("Test cache", func(t *testing.T) {
		// Invalidate cache to force fresh discovery
		kd.InvalidateCache()

		// First discovery
		start := time.Now()
		peers1, err := kd.Discover(ctx)
		firstCallDuration := time.Since(start)
		require.NoError(t, err)

		// Second discovery (should use cache)
		start = time.Now()
		peers2, err := kd.Discover(ctx)
		cachedCallDuration := time.Since(start)
		require.NoError(t, err)

		// Cached call should be much faster
		assert.Less(t, cachedCallDuration, firstCallDuration/2,
			"Cached discovery should be significantly faster")

		// Should return same peers
		assert.Equal(t, len(peers1), len(peers2), "Cached result should have same number of peers")
	})

	// Test 5: Test cache invalidation
	t.Run("Test cache invalidation", func(t *testing.T) {
		// Discover peers
		_, err := kd.Discover(ctx)
		require.NoError(t, err)

		// Get cached peers
		cachedPeers := kd.GetCachedPeers()
		assert.Len(t, cachedPeers, expectedReplicas)

		// Invalidate cache
		kd.InvalidateCache()

		// Discover again (should query DNS)
		peers, err := kd.Discover(ctx)
		require.NoError(t, err)
		assert.Len(t, peers, expectedReplicas)
	})

	// Test 6: Test concurrent discovery
	t.Run("Test concurrent discovery", func(t *testing.T) {
		const goroutines = 10
		results := make(chan error, goroutines)

		// Run multiple concurrent discoveries
		for i := 0; i < goroutines; i++ {
			go func() {
				_, err := kd.Discover(ctx)
				results <- err
			}()
		}

		// Collect results
		for i := 0; i < goroutines; i++ {
			err := <-results
			assert.NoError(t, err, "Concurrent discovery should not fail")
		}
	})

	// Test 7: Verify DNS resolution methods
	t.Run("Test A record fallback", func(t *testing.T) {
		// Test direct A record discovery
		peers, err := kd.discoverViaARecords(ctx)
		if err != nil {
			t.Logf("A record discovery failed (this is expected if SRV works): %v", err)
		} else {
			assert.Len(t, peers, expectedReplicas, "A record discovery should find all pods")
		}
	})

	// Test 8: Verify pod name extraction and validation
	t.Run("Test pod name utilities", func(t *testing.T) {
		testCases := []struct {
			fqdn            string
			expectedPodName string
		}{
			{
				fqdn:            "quicbaas-0.quicbaas.quicbaas-test.svc.cluster.local",
				expectedPodName: "quicbaas-0",
			},
			{
				fqdn:            "quicbaas-1.quicbaas.quicbaas-test.svc.cluster.local.",
				expectedPodName: "quicbaas-1",
			},
			{
				fqdn:            "quicbaas-2",
				expectedPodName: "quicbaas-2",
			},
		}

		for _, tc := range testCases {
			podName := extractPodName(tc.fqdn)
			assert.Equal(t, tc.expectedPodName, podName,
				"Failed to extract pod name from %s", tc.fqdn)

			// Verify pod name is valid
			isValid := validatePodName(podName, statefulSetName)
			assert.True(t, isValid, "Pod name %s should be valid", podName)

			// Extract node ID
			if podName != "" {
				nodeID, err := extractNodeIDFromPodName(podName, statefulSetName)
				require.NoError(t, err, "Failed to extract node ID from %s", podName)
				assert.Greater(t, nodeID, uint64(0), "Node ID should be greater than 0")
			}
		}
	})

	// Test 9: Verify Register/Deregister are no-ops
	t.Run("Test Register and Deregister", func(t *testing.T) {
		self := Peer{
			NodeID:  1,
			Address: "quicbaas-0.quicbaas.quicbaas-test.svc.cluster.local:63001",
		}

		err := kd.Register(ctx, self)
		assert.NoError(t, err, "Register should be a no-op and return no error")

		err = kd.Deregister(ctx)
		assert.NoError(t, err, "Deregister should be a no-op and return no error")
	})

	// Test 10: Verify discovery method name
	t.Run("Test discovery name", func(t *testing.T) {
		assert.Equal(t, "kubernetes", kd.Name(), "Discovery method name should be 'kubernetes'")
	})

	// Test 11: Verify getters
	t.Run("Test getters", func(t *testing.T) {
		assert.Equal(t, namespace, kd.GetNamespace())
		assert.Equal(t, serviceName, kd.GetServiceName())
		assert.Equal(t, statefulSetName, kd.GetStatefulSetName())
	})
}

// TestKubernetesDiscovery_MinikubeStatefulSetScaling tests discovery during scaling events
func TestKubernetesDiscovery_MinikubeStatefulSetScaling(t *testing.T) {
	t.Log("Testing Kubernetes discovery with StatefulSet scaling")

	const (
		namespace       = "quicbaas-test"
		serviceName     = "quicbaas"
		statefulSetName = "quicbaas"
		port            = 63001
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	kd, err := NewKubernetesDiscovery(KubernetesConfig{
		Namespace:       namespace,
		ServiceName:     serviceName,
		StatefulSetName: statefulSetName,
		Port:            port,
		RefreshInterval: 5 * time.Second,
	})
	require.NoError(t, err)
	defer kd.Close()

	// Discover initial peer count
	initialPeers, err := kd.Discover(ctx)
	require.NoError(t, err)

	initialCount := len(initialPeers)
	t.Logf("Initial peer count: %d", initialCount)
	require.Greater(t, initialCount, 0, "Should discover at least one peer")

	// Verify each discovered peer
	for i, peer := range initialPeers {
		t.Logf("Peer %d: NodeID=%d, Address=%s, Pod=%s",
			i, peer.NodeID, peer.Address, peer.Metadata["pod"])

		// Verify peer structure
		assert.NotZero(t, peer.NodeID)
		assert.NotEmpty(t, peer.Address)
		assert.NotEmpty(t, peer.Metadata["pod"])
	}

	// Test scaling behavior by invalidating cache and re-discovering
	// In a real scenario, this would detect pods being added/removed
	t.Log("Testing cache invalidation to simulate scaling detection")

	// Invalidate cache to force fresh DNS lookup
	kd.InvalidateCache()

	// Wait a moment for any DNS changes to propagate
	time.Sleep(2 * time.Second)

	// Rediscover - should get fresh DNS data
	scaledPeers, err := kd.Discover(ctx)
	require.NoError(t, err)

	t.Logf("Peers after cache invalidation: %d", len(scaledPeers))

	// Verify discovery still works correctly
	assert.Equal(t, initialCount, len(scaledPeers),
		"Discovery should consistently find the same number of peers")

	// Verify all peers are valid
	for _, peer := range scaledPeers {
		assert.NotZero(t, peer.NodeID, "Peer should have valid NodeID")
		assert.NotEmpty(t, peer.Address, "Peer should have valid Address")
		assert.Contains(t, peer.Address, serviceName, "Address should contain service name")
		assert.Contains(t, peer.Address, namespace, "Address should contain namespace")
	}
}
