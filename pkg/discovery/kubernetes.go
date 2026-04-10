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
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// KubernetesDiscovery implements discovery for Kubernetes StatefulSet deployments
// It uses DNS-based discovery via headless services to find pods in the cluster
// This is the recommended approach for StatefulSet-based Raft clusters.
type KubernetesDiscovery struct {
	// namespace is the Kubernetes namespace where the StatefulSet is deployed
	namespace string

	// serviceName is the headless service name (required for DNS discovery)
	serviceName string

	// statefulSetName is the name of the StatefulSet
	statefulSetName string

	// podLabelSelector is optional pod label selector for filtering
	podLabelSelector string

	// port is the Raft communication port
	port int

	// clusterDomain is the Kubernetes cluster domain (default: cluster.local)
	clusterDomain string

	// refreshInterval is how often to refresh the peer list
	refreshInterval time.Duration

	// resolver is the DNS resolver to use
	resolver DNSResolver

	// cachedPeers stores the last discovered peers
	cachedPeers []Peer
	cacheTime   time.Time
	cacheMutex  sync.RWMutex
}

// KubernetesConfig contains configuration for Kubernetes discovery.
type KubernetesConfig struct {
	// Namespace is the Kubernetes namespace (default: auto-detected from env)
	Namespace string

	// ServiceName is the headless service name (required)
	ServiceName string

	// StatefulSetName is the StatefulSet name (optional, defaults to ServiceName)
	StatefulSetName string

	// PodLabelSelector is optional pod label selector
	PodLabelSelector string

	// Port is the Raft communication port (default: 63001)
	Port int

	// ClusterDomain is the Kubernetes cluster domain (default: cluster.local)
	ClusterDomain string

	// RefreshInterval is how often to refresh the DNS records
	// Default: 30 seconds
	RefreshInterval time.Duration

	// Resolver is a custom DNS resolver (optional)
	// If nil, uses system default resolver
	Resolver DNSResolver
}

// KubernetesPodNameMismatchError is returned when a pod name does not match the expected StatefulSet.
type KubernetesPodNameMismatchError struct {
	PodName         string
	StatefulSetName string
}

// Error implements the error interface.
func (e *KubernetesPodNameMismatchError) Error() string {
	return "pod " + e.PodName + " does not match StatefulSet " + e.StatefulSetName
}

// KubernetesPodOrdinalError is returned when a pod name has an invalid ordinal.
type KubernetesPodOrdinalError struct {
	PodName string
	Cause   error
}

// Error implements the error interface.
func (e *KubernetesPodOrdinalError) Error() string {
	return "invalid ordinal in pod name " + e.PodName + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *KubernetesPodOrdinalError) Unwrap() error {
	return e.Cause
}

// KubernetesSRVLookupError is returned when an SRV lookup fails.
type KubernetesSRVLookupError struct {
	Cause error
}

// Error implements the error interface.
func (e *KubernetesSRVLookupError) Error() string {
	return "SRV lookup failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *KubernetesSRVLookupError) Unwrap() error {
	return e.Cause
}

// KubernetesNoSRVRecordsError is returned when no SRV records are found.
type KubernetesNoSRVRecordsError struct{}

// Error implements the error interface.
func (e *KubernetesNoSRVRecordsError) Error() string {
	return "no SRV records found"
}

// KubernetesServiceLookupError is returned when a service DNS lookup fails.
type KubernetesServiceLookupError struct {
	ServiceFQDN string
	Cause       error
}

// Error implements the error interface.
func (e *KubernetesServiceLookupError) Error() string {
	return "failed to lookup service " + e.ServiceFQDN + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *KubernetesServiceLookupError) Unwrap() error {
	return e.Cause
}

// KubernetesNoIPsError is returned when no IPs are found for a service.
type KubernetesNoIPsError struct {
	ServiceFQDN string
}

// Error implements the error interface.
func (e *KubernetesNoIPsError) Error() string {
	return "no IPs found for service " + e.ServiceFQDN
}

// NewKubernetesDiscovery creates a new Kubernetes-based discovery method.
func NewKubernetesDiscovery(config KubernetesConfig) (*KubernetesDiscovery, error) {
	if config.ServiceName == "" {
		return nil, &DiscoveryError{
			Op:     "NewKubernetesDiscovery",
			Method: "kubernetes",
			Err:    "service_name cannot be empty",
		}
	}

	// Auto-detect namespace from environment if not provided
	namespace := config.Namespace
	if namespace == "" {
		namespace = detectKubernetesNamespace()
		if namespace == "" {
			namespace = "quicbaas" // Default fallback
		}
	}

	// Default StatefulSet name to service name if not provided
	statefulSetName := config.StatefulSetName
	if statefulSetName == "" {
		statefulSetName = config.ServiceName
	}

	// Set defaults
	if config.Port == 0 {
		config.Port = 63001 // Default Raft port
	}

	if config.ClusterDomain == "" {
		config.ClusterDomain = "cluster.local"
	}

	if config.RefreshInterval == 0 {
		config.RefreshInterval = 30 * time.Second
	}

	if config.Resolver == nil {
		config.Resolver = net.DefaultResolver
	}

	return &KubernetesDiscovery{
		namespace:        namespace,
		serviceName:      config.ServiceName,
		statefulSetName:  statefulSetName,
		podLabelSelector: config.PodLabelSelector,
		port:             config.Port,
		clusterDomain:    config.ClusterDomain,
		refreshInterval:  config.RefreshInterval,
		resolver:         config.Resolver,
		cachedPeers:      make([]Peer, 0),
	}, nil
}

// Name returns the name of this discovery method.
func (k *KubernetesDiscovery) Name() string {
	return "kubernetes"
}

// Discover queries Kubernetes DNS to find StatefulSet pods.
func (k *KubernetesDiscovery) Discover(ctx context.Context) ([]Peer, error) {
	// Check cache first
	k.cacheMutex.RLock()
	if time.Since(k.cacheTime) < k.refreshInterval && len(k.cachedPeers) > 0 {
		peers := make([]Peer, len(k.cachedPeers))
		copy(peers, k.cachedPeers)
		k.cacheMutex.RUnlock()
		return peers, nil
	}
	k.cacheMutex.RUnlock()

	// Try DNS SRV discovery first (most reliable for headless services)
	peers, err := k.discoverViaSRV(ctx)
	if err != nil {
		// Fallback to A record discovery
		peers, err = k.discoverViaARecords(ctx)
		if err != nil {
			return nil, &DiscoveryError{
				Op:     "Discover",
				Method: "kubernetes",
				Err:    "all discovery methods failed: " + err.Error(),
			}
		}
	}

	if len(peers) == 0 {
		return nil, &DiscoveryError{
			Op:     "Discover",
			Method: "kubernetes",
			Err:    "no peers found in StatefulSet",
		}
	}

	// Update cache
	k.cacheMutex.Lock()
	k.cachedPeers = peers
	k.cacheTime = time.Now()
	k.cacheMutex.Unlock()

	return peers, nil
}

// discoverViaSRV uses DNS SRV records to discover pods
// This works with headless services that have SRV records configured.
func (k *KubernetesDiscovery) discoverViaSRV(ctx context.Context) ([]Peer, error) {
	// Construct SRV record name
	// Format: _port._proto.service.namespace.svc.cluster.local
	service := "raft" // Service name for SRV
	proto := "tcp"
	name := k.serviceName + "." + k.namespace + ".svc." + k.clusterDomain

	// Query SRV records
	_, srvRecords, err := k.resolver.LookupSRV(ctx, service, proto, name)
	if err != nil {
		return nil, &KubernetesSRVLookupError{Cause: err}
	}

	if len(srvRecords) == 0 {
		return nil, &KubernetesNoSRVRecordsError{}
	}

	// Convert SRV records to Peers
	peers := make([]Peer, 0, len(srvRecords))
	now := time.Now()

	for _, srv := range srvRecords {
		// Extract pod name from target
		// Format: pod-0.service.namespace.svc.cluster.local.
		target := strings.TrimSuffix(srv.Target, ".")
		podName := extractPodName(target)

		// Extract node ID from pod ordinal
		nodeID, err := extractNodeIDFromPodName(podName, k.statefulSetName)
		if err != nil {
			// Skip pods that don't match our StatefulSet
			continue
		}

		// Construct address
		address := net.JoinHostPort(target, strconv.Itoa(int(srv.Port)))

		peer := Peer{
			NodeID:   nodeID,
			Address:  address,
			RaftAddr: address,
			Metadata: map[string]string{
				"pod":         podName,
				"namespace":   k.namespace,
				"service":     k.serviceName,
				"statefulset": k.statefulSetName,
				"priority":    strconv.Itoa(int(srv.Priority)),
				"weight":      strconv.Itoa(int(srv.Weight)),
			},
			LastSeen: now,
			TTL:      k.refreshInterval,
		}

		peers = append(peers, peer)
	}

	return peers, nil
}

// discoverViaARecords uses A record lookups for each expected pod
// This is a fallback when SRV records are not available.
func (k *KubernetesDiscovery) discoverViaARecords(ctx context.Context) ([]Peer, error) {
	// For StatefulSet pods, we need to query individual pod DNS names
	// Format: pod-name.service-name.namespace.svc.cluster.local

	// First, try to determine the number of replicas by querying the service
	serviceFQDN := k.serviceName + "." + k.namespace + ".svc." + k.clusterDomain

	// Lookup all IPs for the headless service
	ips, err := k.resolver.LookupHost(ctx, serviceFQDN)
	if err != nil {
		return nil, &KubernetesServiceLookupError{ServiceFQDN: serviceFQDN, Cause: err}
	}

	if len(ips) == 0 {
		return nil, &KubernetesNoIPsError{ServiceFQDN: serviceFQDN}
	}

	// Create peers from discovered IPs
	peers := make([]Peer, 0, len(ips))
	now := time.Now()

	// Try to discover pods by ordinal (common StatefulSet pattern)
	// We'll try up to 10 pods (configurable later if needed)
	maxPods := 10
	foundPods := make(map[string]bool)

	for i := 0; i < maxPods; i++ {
		podName := k.statefulSetName + "-" + strconv.Itoa(i)
		podFQDN := podName + "." + k.serviceName + "." + k.namespace + ".svc." + k.clusterDomain

		// Try to resolve this pod
		podIPs, err := k.resolver.LookupHost(ctx, podFQDN)
		if err != nil {
			// Pod doesn't exist, stop searching
			break
		}

		if len(podIPs) == 0 {
			break
		}

		// Node ID is ordinal + 1 (Raft node IDs start at 1)
		nodeID := uint64(i + 1) //nolint:gosec // G115: i is small index from pod ordinals

		// Use the pod FQDN as the address
		address := net.JoinHostPort(podFQDN, strconv.Itoa(k.port))

		peer := Peer{
			NodeID:   nodeID,
			Address:  address,
			RaftAddr: address,
			Metadata: map[string]string{
				"pod":         podName,
				"namespace":   k.namespace,
				"service":     k.serviceName,
				"statefulset": k.statefulSetName,
				"ip":          podIPs[0],
			},
			LastSeen: now,
			TTL:      k.refreshInterval,
		}

		peers = append(peers, peer)
		foundPods[podIPs[0]] = true
	}

	return peers, nil
}

// Register is a no-op for Kubernetes discovery
// Pod registration is handled by Kubernetes DNS automatically.
func (k *KubernetesDiscovery) Register(ctx context.Context, self Peer) error {
	// No-op - Kubernetes handles pod registration via DNS
	return nil
}

// Deregister is a no-op for Kubernetes discovery.
func (k *KubernetesDiscovery) Deregister(ctx context.Context) error {
	// No-op - Kubernetes handles pod deregistration via DNS
	return nil
}

// Close releases resources.
func (k *KubernetesDiscovery) Close() error {
	k.cacheMutex.Lock()
	defer k.cacheMutex.Unlock()

	k.cachedPeers = nil
	return nil
}

// InvalidateCache forces a refresh on the next Discover call.
func (k *KubernetesDiscovery) InvalidateCache() {
	k.cacheMutex.Lock()
	defer k.cacheMutex.Unlock()

	k.cacheTime = time.Time{}
}

// GetCachedPeers returns the currently cached peers without querying DNS.
func (k *KubernetesDiscovery) GetCachedPeers() []Peer {
	k.cacheMutex.RLock()
	defer k.cacheMutex.RUnlock()

	peers := make([]Peer, len(k.cachedPeers))
	copy(peers, k.cachedPeers)
	return peers
}

// GetNamespace returns the configured namespace.
func (k *KubernetesDiscovery) GetNamespace() string {
	return k.namespace
}

// GetServiceName returns the configured service name.
func (k *KubernetesDiscovery) GetServiceName() string {
	return k.serviceName
}

// GetStatefulSetName returns the configured StatefulSet name.
func (k *KubernetesDiscovery) GetStatefulSetName() string {
	return k.statefulSetName
}

// detectKubernetesNamespace attempts to detect the current namespace from environment
// It checks standard Kubernetes environment variables and mounted service account files.
func detectKubernetesNamespace() string {
	// Try environment variables first
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		return ns
	}

	if ns := os.Getenv("NAMESPACE"); ns != "" {
		return ns
	}

	// Try reading from service account mount
	// This is available when running inside a Kubernetes pod
	data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err == nil && len(data) > 0 {
		return strings.TrimSpace(string(data))
	}

	return ""
}

// extractPodName extracts the pod name from a full DNS name
// Input: "quicbaas-0.quicbaas.default.svc.cluster.local"
// Output: "quicbaas-0".
func extractPodName(fqdn string) string {
	parts := strings.Split(fqdn, ".")
	if len(parts) > 0 {
		return parts[0]
	}
	return fqdn
}

// extractNodeIDFromPodName extracts the Raft node ID from a StatefulSet pod name
// StatefulSet pods follow the pattern: {statefulset-name}-{ordinal}
// Node ID = ordinal + 1 (since Raft node IDs start at 1)
// Example: "quicbaas-0" with statefulSetName "quicbaas" returns NodeID 1.
func extractNodeIDFromPodName(podName, statefulSetName string) (uint64, error) {
	// Pod name should be: {statefulSetName}-{ordinal}
	prefix := statefulSetName + "-"
	if !strings.HasPrefix(podName, prefix) {
		return 0, &KubernetesPodNameMismatchError{PodName: podName, StatefulSetName: statefulSetName}
	}

	// Extract ordinal suffix
	ordinalStr := strings.TrimPrefix(podName, prefix)

	// Parse ordinal
	ordinal, err := strconv.ParseUint(ordinalStr, 10, 64)
	if err != nil {
		return 0, &KubernetesPodOrdinalError{PodName: podName, Cause: err}
	}

	// Node ID is ordinal + 1 (Raft node IDs start at 1, not 0)
	return ordinal + 1, nil
}

// validatePodName checks if a pod name matches the expected StatefulSet pattern.
func validatePodName(podName, statefulSetName string) bool {
	// Pattern: {statefulset-name}-{ordinal}
	pattern := "^" + regexp.QuoteMeta(statefulSetName) + `-\d+$`
	matched, err := regexp.MatchString(pattern, podName)
	return err == nil && matched
}

// NewKubernetesDiscoverySimple creates a Kubernetes discovery with minimal config
// This is a convenience function for the most common use case.
func NewKubernetesDiscoverySimple(serviceName string) (*KubernetesDiscovery, error) {
	return NewKubernetesDiscovery(KubernetesConfig{
		ServiceName: serviceName,
	})
}

// NewKubernetesDiscoveryWithNamespace creates a Kubernetes discovery with explicit namespace.
func NewKubernetesDiscoveryWithNamespace(serviceName, namespace string) (*KubernetesDiscovery, error) {
	return NewKubernetesDiscovery(KubernetesConfig{
		ServiceName: serviceName,
		Namespace:   namespace,
	})
}
