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
	"os"
	"testing"
	"time"
)

// mockKubernetesResolver is a mock DNS resolver for Kubernetes discovery tests.
type mockKubernetesResolver struct {
	srvRecords []*net.SRV
	srvError   error
	hostMap    map[string][]string // Map of hostname to IPs
	hostError  error
}

func (m *mockKubernetesResolver) LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
	if m.srvError != nil {
		return "", nil, m.srvError
	}
	return "", m.srvRecords, nil
}

func (m *mockKubernetesResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	if m.hostError != nil {
		return nil, m.hostError
	}
	if ips, ok := m.hostMap[host]; ok {
		return ips, nil
	}
	return nil, &net.DNSError{
		Err:        "no such host",
		Name:       host,
		IsNotFound: true,
	}
}

func TestNewKubernetesDiscovery(t *testing.T) {
	tests := []struct {
		name    string
		config  KubernetesConfig
		wantErr bool
	}{
		{
			name: "valid minimal config",
			config: KubernetesConfig{
				ServiceName: "quicbaas",
			},
			wantErr: false,
		},
		{
			name: "valid full config",
			config: KubernetesConfig{
				Namespace:        "production",
				ServiceName:      "quicbaas-raft",
				StatefulSetName:  "quicbaas",
				PodLabelSelector: "app=quicbaas",
				Port:             63001,
				ClusterDomain:    "cluster.local",
				RefreshInterval:  60 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "empty service name",
			config: KubernetesConfig{
				ServiceName: "",
			},
			wantErr: true,
		},
		{
			name: "defaults applied",
			config: KubernetesConfig{
				ServiceName: "quicbaas",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kd, err := NewKubernetesDiscovery(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewKubernetesDiscovery() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if kd == nil {
					t.Fatal("NewKubernetesDiscovery() returned nil without error")
				}

				// Check defaults
				if kd.port == 0 {
					t.Error("port should have default value")
				}

				if kd.clusterDomain == "" {
					t.Error("clusterDomain should have default value")
				}

				if kd.refreshInterval == 0 {
					t.Error("refreshInterval should have default value")
				}

				if kd.resolver == nil {
					t.Error("resolver should not be nil")
				}

				// If StatefulSetName not provided, should default to ServiceName
				if tt.config.StatefulSetName == "" && kd.statefulSetName != tt.config.ServiceName {
					t.Errorf("statefulSetName = %s, want %s", kd.statefulSetName, tt.config.ServiceName)
				}
			}
		})
	}
}

func TestKubernetesDiscovery_DiscoverViaSRV(t *testing.T) {
	// Create mock resolver with StatefulSet pod SRV records
	mockSRVs := []*net.SRV{
		{Target: "quicbaas-0.quicbaas.default.svc.cluster.local.", Port: 63001, Priority: 10, Weight: 10},
		{Target: "quicbaas-1.quicbaas.default.svc.cluster.local.", Port: 63001, Priority: 10, Weight: 10},
		{Target: "quicbaas-2.quicbaas.default.svc.cluster.local.", Port: 63001, Priority: 10, Weight: 10},
	}

	resolver := &mockKubernetesResolver{
		srvRecords: mockSRVs,
	}

	config := KubernetesConfig{
		ServiceName:     "quicbaas",
		Namespace:       "default",
		StatefulSetName: "quicbaas",
		Port:            63001,
		Resolver:        resolver,
	}

	kd, err := NewKubernetesDiscovery(config)
	if err != nil {
		t.Fatalf("NewKubernetesDiscovery() error = %v", err)
	}

	ctx := context.Background()
	peers, err := kd.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(peers) != 3 {
		t.Errorf("Discover() returned %d peers, want 3", len(peers))
	}

	// Verify peer details
	expectedNodeIDs := []uint64{1, 2, 3}
	for i, peer := range peers {
		if peer.NodeID != expectedNodeIDs[i] {
			t.Errorf("peer[%d].NodeID = %d, want %d", i, peer.NodeID, expectedNodeIDs[i])
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
		if _, ok := peer.Metadata["pod"]; !ok {
			t.Errorf("peer[%d] missing pod metadata", i)
		}

		if peer.Metadata["namespace"] != "default" {
			t.Errorf("peer[%d] namespace = %s, want default", i, peer.Metadata["namespace"])
		}

		if peer.Metadata["statefulset"] != "quicbaas" {
			t.Errorf("peer[%d] statefulset = %s, want quicbaas", i, peer.Metadata["statefulset"])
		}
	}
}

func TestKubernetesDiscovery_DiscoverViaARecords(t *testing.T) {
	// Create mock resolver with A record responses
	hostMap := map[string][]string{
		"quicbaas.default.svc.cluster.local":            {"10.0.0.1", "10.0.0.2", "10.0.0.3"},
		"quicbaas-0.quicbaas.default.svc.cluster.local": {"10.0.0.1"},
		"quicbaas-1.quicbaas.default.svc.cluster.local": {"10.0.0.2"},
		"quicbaas-2.quicbaas.default.svc.cluster.local": {"10.0.0.3"},
	}

	resolver := &mockKubernetesResolver{
		srvError: errors.New("SRV not supported"), // Force fallback to A records
		hostMap:  hostMap,
	}

	config := KubernetesConfig{
		ServiceName:     "quicbaas",
		Namespace:       "default",
		StatefulSetName: "quicbaas",
		Port:            63001,
		Resolver:        resolver,
	}

	kd, err := NewKubernetesDiscovery(config)
	if err != nil {
		t.Fatalf("NewKubernetesDiscovery() error = %v", err)
	}

	ctx := context.Background()
	peers, err := kd.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(peers) != 3 {
		t.Errorf("Discover() returned %d peers, want 3", len(peers))
	}

	// Verify node IDs are correct
	for i, peer := range peers {
		expectedNodeID := uint64(i + 1)
		if peer.NodeID != expectedNodeID {
			t.Errorf("peer[%d].NodeID = %d, want %d", i, peer.NodeID, expectedNodeID)
		}
	}
}

func TestKubernetesDiscovery_Cache(t *testing.T) {
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
		RefreshInterval: 1 * time.Hour, // Long cache
	})
	if err != nil {
		t.Fatalf("NewKubernetesDiscovery() error = %v", err)
	}

	kd.resolver = resolver

	ctx := context.Background()

	// First call should query DNS
	peers1, err := kd.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	// Change mock data
	resolver.srvRecords = []*net.SRV{
		{Target: "quicbaas-0.quicbaas.default.svc.cluster.local.", Port: 63001, Priority: 10, Weight: 10},
		{Target: "quicbaas-1.quicbaas.default.svc.cluster.local.", Port: 63001, Priority: 10, Weight: 10},
	}

	// Second call should return cached data
	peers2, err := kd.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(peers2) != len(peers1) {
		t.Errorf("Second Discover() should return cached data, got %d peers, want %d", len(peers2), len(peers1))
	}

	// Invalidate cache
	kd.InvalidateCache()

	// Third call should query DNS again
	peers3, err := kd.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(peers3) != 2 {
		t.Errorf("After cache invalidation, got %d peers, want 2", len(peers3))
	}
}

func TestKubernetesDiscovery_NoPeersFound(t *testing.T) {
	resolver := &mockKubernetesResolver{
		srvError:  errors.New("no SRV records"),
		hostError: errors.New("no A records"),
	}

	kd, err := NewKubernetesDiscovery(KubernetesConfig{
		ServiceName:     "quicbaas",
		Namespace:       "default",
		StatefulSetName: "quicbaas",
	})
	if err != nil {
		t.Fatalf("NewKubernetesDiscovery() error = %v", err)
	}

	kd.resolver = resolver

	ctx := context.Background()
	_, err = kd.Discover(ctx)
	if err == nil {
		t.Error("Discover() should return error when no peers found")
	}
}

func TestKubernetesDiscovery_RegisterDeregister(t *testing.T) {
	kd, err := NewKubernetesDiscovery(KubernetesConfig{
		ServiceName: "quicbaas",
	})
	if err != nil {
		t.Fatalf("NewKubernetesDiscovery() error = %v", err)
	}

	ctx := context.Background()
	self := Peer{
		NodeID:  1,
		Address: "quicbaas-0.quicbaas.default.svc.cluster.local:63001",
	}

	// These should all be no-ops and return nil
	if err := kd.Register(ctx, self); err != nil {
		t.Errorf("Register() error = %v, want nil", err)
	}

	if err := kd.Deregister(ctx); err != nil {
		t.Errorf("Deregister() error = %v, want nil", err)
	}
}

func TestKubernetesDiscovery_Close(t *testing.T) {
	kd, err := NewKubernetesDiscovery(KubernetesConfig{
		ServiceName: "quicbaas",
	})
	if err != nil {
		t.Fatalf("NewKubernetesDiscovery() error = %v", err)
	}

	// Add some cached peers
	kd.cachedPeers = []Peer{
		{NodeID: 1, Address: "quicbaas-0.quicbaas.default.svc.cluster.local:63001"},
	}

	if err := kd.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Cache should be cleared
	if len(kd.cachedPeers) != 0 {
		t.Error("Close() should clear cached peers")
	}
}

func TestKubernetesDiscovery_GetCachedPeers(t *testing.T) {
	kd, err := NewKubernetesDiscovery(KubernetesConfig{
		ServiceName: "quicbaas",
	})
	if err != nil {
		t.Fatalf("NewKubernetesDiscovery() error = %v", err)
	}

	// Set cached peers
	kd.cachedPeers = []Peer{
		{NodeID: 1, Address: "quicbaas-0.quicbaas.default.svc.cluster.local:63001"},
		{NodeID: 2, Address: "quicbaas-1.quicbaas.default.svc.cluster.local:63001"},
	}

	peers := kd.GetCachedPeers()
	if len(peers) != 2 {
		t.Errorf("GetCachedPeers() returned %d peers, want 2", len(peers))
	}

	// Verify we got a copy
	peers[0].NodeID = 999

	if kd.cachedPeers[0].NodeID == 999 {
		t.Error("GetCachedPeers() did not return a copy")
	}
}

func TestKubernetesDiscovery_Name(t *testing.T) {
	kd, err := NewKubernetesDiscovery(KubernetesConfig{
		ServiceName: "quicbaas",
	})
	if err != nil {
		t.Fatalf("NewKubernetesDiscovery() error = %v", err)
	}

	if name := kd.Name(); name != "kubernetes" {
		t.Errorf("Name() = %q, want %q", name, "kubernetes")
	}
}

func TestKubernetesDiscovery_Getters(t *testing.T) {
	config := KubernetesConfig{
		ServiceName:     "quicbaas-raft",
		Namespace:       "production",
		StatefulSetName: "quicbaas",
	}

	kd, err := NewKubernetesDiscovery(config)
	if err != nil {
		t.Fatalf("NewKubernetesDiscovery() error = %v", err)
	}

	if kd.GetNamespace() != "production" {
		t.Errorf("GetNamespace() = %s, want production", kd.GetNamespace())
	}

	if kd.GetServiceName() != "quicbaas-raft" {
		t.Errorf("GetServiceName() = %s, want quicbaas-raft", kd.GetServiceName())
	}

	if kd.GetStatefulSetName() != "quicbaas" {
		t.Errorf("GetStatefulSetName() = %s, want quicbaas", kd.GetStatefulSetName())
	}
}

func TestDetectKubernetesNamespace(t *testing.T) {
	// Save original env
	origPodNS := os.Getenv("POD_NAMESPACE")
	origNS := os.Getenv("NAMESPACE")
	defer func() {
		os.Setenv("POD_NAMESPACE", origPodNS)
		os.Setenv("NAMESPACE", origNS)
	}()

	tests := []struct {
		name       string
		podNS      string
		ns         string
		wantResult string
	}{
		{
			name:       "POD_NAMESPACE set",
			podNS:      "production",
			ns:         "",
			wantResult: "production",
		},
		{
			name:       "NAMESPACE set",
			podNS:      "",
			ns:         "staging",
			wantResult: "staging",
		},
		{
			name:       "POD_NAMESPACE takes precedence",
			podNS:      "prod",
			ns:         "staging",
			wantResult: "prod",
		},
		{
			name:       "no env vars set",
			podNS:      "",
			ns:         "",
			wantResult: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear env
			os.Unsetenv("POD_NAMESPACE")
			os.Unsetenv("NAMESPACE")

			// Set test env
			if tt.podNS != "" {
				os.Setenv("POD_NAMESPACE", tt.podNS)
			}
			if tt.ns != "" {
				os.Setenv("NAMESPACE", tt.ns)
			}

			result := detectKubernetesNamespace()
			if result != tt.wantResult {
				t.Errorf("detectKubernetesNamespace() = %q, want %q", result, tt.wantResult)
			}
		})
	}
}

func TestExtractPodName(t *testing.T) {
	tests := []struct {
		name    string
		fqdn    string
		wantPod string
	}{
		{
			name:    "full FQDN",
			fqdn:    "quicbaas-0.quicbaas.default.svc.cluster.local",
			wantPod: "quicbaas-0",
		},
		{
			name:    "with trailing dot",
			fqdn:    "quicbaas-1.quicbaas.default.svc.cluster.local.",
			wantPod: "quicbaas-1",
		},
		{
			name:    "short name",
			fqdn:    "quicbaas-2",
			wantPod: "quicbaas-2",
		},
		{
			name:    "empty",
			fqdn:    "",
			wantPod: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPodName(tt.fqdn)
			if result != tt.wantPod {
				t.Errorf("extractPodName(%q) = %q, want %q", tt.fqdn, result, tt.wantPod)
			}
		})
	}
}

func TestExtractNodeIDFromPodName(t *testing.T) {
	tests := []struct {
		name            string
		podName         string
		statefulSetName string
		wantNodeID      uint64
		wantErr         bool
	}{
		{
			name:            "pod-0",
			podName:         "quicbaas-0",
			statefulSetName: "quicbaas",
			wantNodeID:      1,
			wantErr:         false,
		},
		{
			name:            "pod-1",
			podName:         "quicbaas-1",
			statefulSetName: "quicbaas",
			wantNodeID:      2,
			wantErr:         false,
		},
		{
			name:            "pod-99",
			podName:         "quicbaas-99",
			statefulSetName: "quicbaas",
			wantNodeID:      100,
			wantErr:         false,
		},
		{
			name:            "wrong prefix",
			podName:         "other-0",
			statefulSetName: "quicbaas",
			wantNodeID:      0,
			wantErr:         true,
		},
		{
			name:            "no ordinal",
			podName:         "quicbaas",
			statefulSetName: "quicbaas",
			wantNodeID:      0,
			wantErr:         true,
		},
		{
			name:            "invalid ordinal",
			podName:         "quicbaas-abc",
			statefulSetName: "quicbaas",
			wantNodeID:      0,
			wantErr:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeID, err := extractNodeIDFromPodName(tt.podName, tt.statefulSetName)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractNodeIDFromPodName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && nodeID != tt.wantNodeID {
				t.Errorf("extractNodeIDFromPodName() = %d, want %d", nodeID, tt.wantNodeID)
			}
		})
	}
}

func TestValidatePodName(t *testing.T) {
	tests := []struct {
		name            string
		podName         string
		statefulSetName string
		wantValid       bool
	}{
		{
			name:            "valid pod-0",
			podName:         "quicbaas-0",
			statefulSetName: "quicbaas",
			wantValid:       true,
		},
		{
			name:            "valid pod-99",
			podName:         "quicbaas-99",
			statefulSetName: "quicbaas",
			wantValid:       true,
		},
		{
			name:            "invalid - wrong prefix",
			podName:         "other-0",
			statefulSetName: "quicbaas",
			wantValid:       false,
		},
		{
			name:            "invalid - no ordinal",
			podName:         "quicbaas",
			statefulSetName: "quicbaas",
			wantValid:       false,
		},
		{
			name:            "invalid - non-numeric ordinal",
			podName:         "quicbaas-abc",
			statefulSetName: "quicbaas",
			wantValid:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validatePodName(tt.podName, tt.statefulSetName)
			if result != tt.wantValid {
				t.Errorf("validatePodName(%q, %q) = %v, want %v", tt.podName, tt.statefulSetName, result, tt.wantValid)
			}
		})
	}
}

func TestNewKubernetesDiscoverySimple(t *testing.T) {
	kd, err := NewKubernetesDiscoverySimple("quicbaas")
	if err != nil {
		t.Fatalf("NewKubernetesDiscoverySimple() error = %v", err)
	}

	if kd == nil {
		t.Fatal("NewKubernetesDiscoverySimple() returned nil")
	}

	if kd.serviceName != "quicbaas" {
		t.Errorf("serviceName = %s, want quicbaas", kd.serviceName)
	}
}

func TestNewKubernetesDiscoveryWithNamespace(t *testing.T) {
	kd, err := NewKubernetesDiscoveryWithNamespace("quicbaas", "production")
	if err != nil {
		t.Fatalf("NewKubernetesDiscoveryWithNamespace() error = %v", err)
	}

	if kd == nil {
		t.Fatal("NewKubernetesDiscoveryWithNamespace() returned nil")
	}

	if kd.serviceName != "quicbaas" {
		t.Errorf("serviceName = %s, want quicbaas", kd.serviceName)
	}

	if kd.namespace != "production" {
		t.Errorf("namespace = %s, want production", kd.namespace)
	}
}

// TestKubernetesDiscovery_ConcurrentDiscovery moved to kubernetes_integration_test.go

// TestKubernetesDiscovery_CacheExpiration moved to kubernetes_integration_test.go

// TestKubernetesDiscovery_StatefulSetScaling moved to kubernetes_integration_test.go

func TestKubernetesDiscovery_ContextCancellation(t *testing.T) {
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
	})
	if err != nil {
		t.Fatalf("NewKubernetesDiscovery() error = %v", err)
	}

	kd.resolver = resolver

	// First, populate the cache
	ctx := context.Background()
	_, err = kd.Discover(ctx)
	if err != nil {
		t.Fatalf("Initial Discover() error = %v", err)
	}

	// Now try with canceled context - should return cached data
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	peers, err := kd.Discover(canceledCtx)
	if err != nil {
		t.Errorf("Discover() with canceled context should succeed using cache, got error: %v", err)
	}

	if len(peers) != 1 {
		t.Errorf("Expected 1 peer from cache, got %d", len(peers))
	}
}
