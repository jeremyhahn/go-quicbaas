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

// Example: Cluster Discovery
//
// This example demonstrates the various cluster discovery mechanisms available
// in QuicBaaS for automatic peer detection and cluster formation:
//
// - DNS-based discovery using SRV records (production environments)
// - Multicast/broadcast discovery for zero-configuration LANs
// - Static member configuration for fixed infrastructure
// - PSK (Pre-Shared Key) authentication for secure discovery
// - Dynamic cluster membership with health checking
// - Discovery manager for coordinating multiple methods with failover
//
// Discovery Architecture:
//
// QuicBaaS supports multiple discovery methods that can be combined and
// prioritized. The discovery manager tries each method in order until
// one succeeds in finding enough peers to form a quorum.
//
//	                 +-------------------+
//	                 | Discovery Manager |
//	                 +--------+----------+
//	                          |
//	    +---------------------+---------------------+
//	    |                     |                     |
//	+---v---+           +-----v-----+         +-----v-----+
//	| DNS   |           | Multicast |         |   Static  |
//	+-------+           +-----------+         +-----------+
//	    |                     |                     |
//	    v                     v                     v
//	SRV Records         UDP Broadcast         Config File
//	(Consul/CoreDNS)    (LAN only)            (Manual)
//
// Prerequisites:
//   - For DNS discovery: Configure SRV records in your DNS infrastructure
//   - For multicast: Nodes must be on the same network segment
//   - For static: Know the addresses of all cluster members in advance
//
// Usage:
//
//	# Run DNS discovery example
//	go run main.go -mode dns -srv "_quicbaas._tcp.cluster.local"
//
//	# Run multicast discovery example (LAN environments)
//	go run main.go -mode multicast -node-id 1 -address "192.168.1.100:63001"
//
//	# Run multicast with PSK authentication
//	go run main.go -mode multicast -node-id 1 -address "192.168.1.100:63001" -psk "my-cluster-secret"
//
//	# Run static discovery example
//	go run main.go -mode static -peers "1:192.168.1.100:63001,2:192.168.1.101:63001,3:192.168.1.102:63001"
//
//	# Run combined discovery with manager (production recommended)
//	go run main.go -mode manager -node-id 1 -address "192.168.1.100:63001"
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/discovery"
	dragonerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// Command-line flags for configuring discovery behavior.
var (
	mode              = flag.String("mode", "static", "Discovery mode: dns, multicast, static, kubernetes, manager")
	nodeID            = flag.Uint64("node-id", 1, "This node's unique ID (required for multicast and manager modes)")
	address           = flag.String("address", "localhost:63001", "This node's address for Raft communication")
	srvRecord         = flag.String("srv", "_quicbaas._tcp.cluster.local", "DNS SRV record to query (dns mode)")
	peers             = flag.String("peers", "", "Comma-separated peer list: nodeID:host:port (static mode)")
	psk               = flag.String("psk", "", "Pre-shared key for authenticated discovery (multicast mode)")
	multicastGroup    = flag.String("multicast-group", discovery.DefaultMulticastGroup, "Multicast group address")
	multicastPort     = flag.Int("multicast-port", discovery.DefaultMulticastPort, "Multicast port")
	k8sService        = flag.String("k8s-service", "quicbaas", "Kubernetes headless service name")
	k8sNamespace      = flag.String("k8s-namespace", "", "Kubernetes namespace (auto-detected if empty)")
	discoveryTimeout  = flag.Duration("timeout", 30*time.Second, "Discovery timeout")
	minPeers          = flag.Int("min-peers", 3, "Minimum peers required for quorum")
	refreshInterval   = flag.Duration("refresh", 5*time.Second, "Peer list refresh interval")
	broadcastInterval = flag.Duration("broadcast", 2*time.Second, "Multicast broadcast interval")
	peerTTL           = flag.Duration("peer-ttl", 10*time.Second, "Peer TTL before expiration")
)

func main() {
	flag.Parse()

	fmt.Println("=== QuicBaaS Cluster Discovery Example ===")
	fmt.Printf("Mode:    %s\n", *mode)
	fmt.Printf("Node ID: %d\n", *nodeID)
	fmt.Printf("Address: %s\n", *address)
	fmt.Println()

	// Create context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle OS signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nReceived shutdown signal...")
		cancel()
	}()

	// Run the appropriate discovery example based on mode
	var err error
	switch *mode {
	case "dns":
		err = runDNSDiscoveryExample(ctx)
	case "multicast":
		err = runMulticastDiscoveryExample(ctx)
	case "static":
		err = runStaticDiscoveryExample(ctx)
	case "kubernetes":
		err = runKubernetesDiscoveryExample(ctx)
	case "manager":
		err = runDiscoveryManagerExample(ctx)
	default:
		log.Fatalf("Unknown discovery mode: %s", *mode)
	}

	if err != nil {
		log.Fatalf("Discovery failed: %v", err)
	}

	fmt.Println("\n=== Example Complete ===")
}

// runDNSDiscoveryExample demonstrates DNS-based cluster discovery.
// DNS discovery is ideal for production environments where nodes are registered
// in a service discovery system like Consul, CoreDNS, or standard DNS.
//
// DNS SRV Record Format:
//
//	_service._proto.domain
//	Example: _quicbaas._tcp.cluster.local
//
// The SRV records should contain entries for each cluster node with:
//   - Priority: Lower values are preferred
//   - Weight: For load balancing among equal priority nodes
//   - Port: The Raft communication port
//   - Target: The hostname of the node
func runDNSDiscoveryExample(ctx context.Context) error {
	fmt.Println("--- DNS-Based Discovery ---")
	fmt.Println()
	fmt.Println("DNS discovery queries SRV records to find cluster peers.")
	fmt.Println("This is the recommended approach for production deployments")
	fmt.Println("with service discovery systems like Consul or CoreDNS.")
	fmt.Println()

	// Step 1: Validate SRV record format
	fmt.Println("Step 1: Validating SRV record configuration...")
	fmt.Printf("  SRV Record: %s\n", *srvRecord)

	if *srvRecord == "" {
		return &dragonerrors.MessageError{Message: "SRV record cannot be empty"}
	}

	// Check SRV record format
	if !strings.HasPrefix(*srvRecord, "_") {
		fmt.Println("  Warning: SRV records typically start with underscore")
		fmt.Println("  Expected format: _service._proto.domain")
	}

	// Step 2: Create DNS discovery instance
	fmt.Println("\nStep 2: Creating DNS discovery instance...")

	dnsConfig := discovery.DNSConfig{
		SRVRecord:       *srvRecord,
		RefreshInterval: *refreshInterval,
		Resolver:        net.DefaultResolver, // Use system DNS resolver
	}

	dnsDiscovery, err := discovery.NewDNSDiscovery(dnsConfig)
	if err != nil {
		return &dragonerrors.WrappedMessageError{Message: "failed to create DNS discovery", Cause: err}
	}
	defer dnsDiscovery.Close()

	fmt.Printf("  DNS Discovery created successfully\n")
	fmt.Printf("  Method name: %s\n", dnsDiscovery.Name())
	fmt.Printf("  Refresh interval: %s\n", *refreshInterval)

	// Step 3: Perform discovery
	fmt.Println("\nStep 3: Discovering peers via DNS...")

	discoveryCtx, cancel := context.WithTimeout(ctx, *discoveryTimeout)
	defer cancel()

	discoveredPeers, err := dnsDiscovery.Discover(discoveryCtx)
	if err != nil {
		fmt.Printf("  Discovery failed: %v\n", err)
		fmt.Println()
		fmt.Println("  Troubleshooting tips:")
		fmt.Println("    1. Verify DNS SRV records exist using:")
		fmt.Printf("       dig SRV %s\n", *srvRecord)
		fmt.Println("    2. Ensure DNS server is reachable")
		fmt.Println("    3. Check that the SRV record format is correct")
		return err
	}

	printDiscoveredPeers("DNS", discoveredPeers)

	// Step 4: Demonstrate cache behavior
	fmt.Println("\nStep 4: Demonstrating cache behavior...")
	fmt.Println("  DNS discovery caches results to reduce DNS queries.")

	// Get cached peers (should return immediately from cache)
	cachedPeers := dnsDiscovery.GetCachedPeers()
	fmt.Printf("  Cached peers: %d\n", len(cachedPeers))

	// Invalidate cache
	fmt.Println("  Invalidating cache...")
	dnsDiscovery.InvalidateCache()

	fmt.Println("  Cache invalidated. Next Discover() will query DNS.")

	// Step 5: Show example DNS configuration
	fmt.Println("\nStep 5: Example DNS Configuration")
	fmt.Println()
	printDNSConfigurationExample()

	return nil
}

// runMulticastDiscoveryExample demonstrates multicast-based cluster discovery.
// Multicast discovery enables zero-configuration peer detection on LANs.
// Nodes broadcast their presence and listen for announcements from others.
//
// Security Consideration:
// By default, multicast discovery is unauthenticated. In untrusted networks,
// use PSK authentication to ensure only authorized nodes can join.
func runMulticastDiscoveryExample(ctx context.Context) error {
	fmt.Println("--- Multicast/Broadcast Discovery ---")
	fmt.Println()
	fmt.Println("Multicast discovery enables zero-configuration peer detection.")
	fmt.Println("Nodes broadcast their presence via UDP multicast and discover")
	fmt.Println("other nodes automatically. Ideal for development and testing.")
	fmt.Println()

	// Validate node configuration
	if *nodeID == 0 {
		return &dragonerrors.MessageError{Message: "node-id is required for multicast discovery"}
	}

	if *address == "" {
		return &dragonerrors.MessageError{Message: "address is required for multicast discovery"}
	}

	// Step 1: Create multicast configuration
	fmt.Println("Step 1: Configuring multicast discovery...")

	multicastConfig := &discovery.MulticastConfig{
		MulticastGroup:    *multicastGroup,
		Port:              *multicastPort,
		BroadcastInterval: *broadcastInterval,
		PeerTTL:           *peerTTL,
		SendQueryOnStart:  true, // Request immediate responses from other nodes
	}

	// Configure PSK authentication if provided
	if *psk != "" {
		multicastConfig.ClusterSecret = *psk
		fmt.Printf("  PSK authentication: ENABLED\n")
		fmt.Println("  Messages will be signed with HMAC-SHA256")
	} else {
		fmt.Println("  PSK authentication: DISABLED (open discovery)")
		fmt.Println("  Warning: Any node on the network can join")
	}

	fmt.Printf("  Multicast group: %s\n", *multicastGroup)
	fmt.Printf("  Port: %d\n", *multicastPort)
	fmt.Printf("  Broadcast interval: %s\n", *broadcastInterval)
	fmt.Printf("  Peer TTL: %s\n", *peerTTL)

	// Step 2: Create multicast discovery instance
	fmt.Println("\nStep 2: Creating multicast discovery instance...")

	multicastDiscovery, err := discovery.NewMulticastDiscovery(multicastConfig)
	if err != nil {
		return &dragonerrors.WrappedMessageError{Message: "failed to create multicast discovery", Cause: err}
	}
	defer multicastDiscovery.Close()

	fmt.Printf("  Multicast Discovery created successfully\n")
	fmt.Printf("  Method name: %s\n", multicastDiscovery.Name())

	// Step 3: Register this node
	fmt.Println("\nStep 3: Registering this node...")

	self := discovery.Peer{
		NodeID:   *nodeID,
		Address:  *address,
		RaftAddr: *address,
		Metadata: map[string]string{
			"started_at": time.Now().Format(time.RFC3339),
			"version":    "1.0.0",
			"role":       "voter",
		},
		LastSeen: time.Now(),
		TTL:      *peerTTL,
	}

	if err := multicastDiscovery.Register(ctx, self); err != nil {
		return &dragonerrors.WrappedMessageError{Message: "failed to register node", Cause: err}
	}

	fmt.Printf("  Registered as node %d at %s\n", self.NodeID, self.Address)

	// Step 4: Wait for peer discovery
	fmt.Println("\nStep 4: Discovering peers via multicast...")
	fmt.Println("  Broadcasting presence and listening for other nodes...")
	fmt.Printf("  Waiting up to %s for peers...\n", *discoveryTimeout)
	fmt.Println()

	// Create periodic discovery ticker
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	discoveryCtx, cancel := context.WithTimeout(ctx, *discoveryTimeout)
	defer cancel()

	iteration := 0
	for {
		select {
		case <-discoveryCtx.Done():
			fmt.Println("\n  Discovery timeout reached")
			peers, _ := multicastDiscovery.Discover(ctx)
			if len(peers) > 0 {
				printDiscoveredPeers("Multicast", peers)
			} else {
				fmt.Println("  No peers discovered (this may be normal if running alone)")
			}
			goto cleanup

		case <-ticker.C:
			iteration++
			peers, err := multicastDiscovery.Discover(ctx)
			if err != nil {
				fmt.Printf("  [%d] Discovery error: %v\n", iteration, err)
				continue
			}

			peerCount := multicastDiscovery.GetPeerCount()
			fmt.Printf("  [%d] Discovered peers: %d\n", iteration, peerCount)

			// List each peer
			for _, peer := range peers {
				fmt.Printf("      - Node %d: %s (last seen: %s)\n",
					peer.NodeID,
					peer.Address,
					peer.LastSeen.Format(time.RFC3339))
			}

			// Check if we have enough peers for quorum
			if peerCount >= *minPeers-1 { // -1 because we don't count ourselves
				fmt.Printf("\n  Quorum achieved: %d peers + self = %d total\n",
					peerCount, peerCount+1)
				goto cleanup
			}
		}
	}

cleanup:
	// Step 5: Graceful deregistration
	fmt.Println("\nStep 5: Deregistering node...")

	if err := multicastDiscovery.Deregister(ctx); err != nil {
		fmt.Printf("  Warning: Deregistration failed: %v\n", err)
	} else {
		fmt.Println("  Node deregistered successfully")
		fmt.Println("  Leave message sent to other peers")
	}

	// Step 6: Show PSK security information
	fmt.Println("\nStep 6: Security Considerations")
	printMulticastSecurityInfo()

	return nil
}

// runStaticDiscoveryExample demonstrates static peer configuration.
// Static discovery is the simplest method - peers are known at startup.
// This is suitable for small, fixed clusters where IP addresses are stable.
func runStaticDiscoveryExample(ctx context.Context) error {
	fmt.Println("--- Static Peer Discovery ---")
	fmt.Println()
	fmt.Println("Static discovery uses a pre-configured list of peers.")
	fmt.Println("This is the simplest method, suitable for fixed infrastructure")
	fmt.Println("where cluster membership doesn't change frequently.")
	fmt.Println()

	// Step 1: Parse peer addresses
	fmt.Println("Step 1: Configuring static peers...")

	var peerAddresses []string
	if *peers != "" {
		peerAddresses = strings.Split(*peers, ",")
	} else {
		// Use default example peers if none provided
		peerAddresses = []string{
			"1:192.168.1.100:63001",
			"2:192.168.1.101:63001",
			"3:192.168.1.102:63001",
		}
		fmt.Println("  No peers specified, using example configuration:")
	}

	for _, addr := range peerAddresses {
		fmt.Printf("    - %s\n", addr)
	}

	// Step 2: Create static discovery instance
	fmt.Println("\nStep 2: Creating static discovery instance...")

	staticDiscovery, err := discovery.NewStaticDiscovery(peerAddresses)
	if err != nil {
		return &dragonerrors.WrappedMessageError{Message: "failed to create static discovery", Cause: err}
	}
	defer staticDiscovery.Close()

	fmt.Printf("  Static Discovery created successfully\n")
	fmt.Printf("  Method name: %s\n", staticDiscovery.Name())
	fmt.Printf("  Initial peer count: %d\n", len(staticDiscovery.GetPeers()))

	// Step 3: Perform discovery
	fmt.Println("\nStep 3: Discovering peers (returns static list)...")

	discoveredPeers, err := staticDiscovery.Discover(ctx)
	if err != nil {
		return &dragonerrors.WrappedMessageError{Message: "discovery failed", Cause: err}
	}

	printDiscoveredPeers("Static", discoveredPeers)

	// Step 4: Demonstrate dynamic peer management
	fmt.Println("\nStep 4: Demonstrating dynamic peer management...")

	// Add a new peer
	fmt.Println("  Adding a new peer to the cluster...")
	newPeer := discovery.Peer{
		NodeID:   4,
		Address:  "192.168.1.103:63001",
		RaftAddr: "192.168.1.103:63001",
		Metadata: map[string]string{
			"added_dynamically": "true",
		},
	}

	if err := staticDiscovery.AddPeer(newPeer); err != nil {
		fmt.Printf("  Failed to add peer: %v\n", err)
	} else {
		fmt.Printf("  Added peer: Node %d at %s\n", newPeer.NodeID, newPeer.Address)
	}

	// List updated peers
	updatedPeers := staticDiscovery.GetPeers()
	fmt.Printf("  Updated peer count: %d\n", len(updatedPeers))

	// Remove a peer
	fmt.Println("\n  Removing peer from the cluster...")
	if err := staticDiscovery.RemovePeer(4); err != nil {
		fmt.Printf("  Failed to remove peer: %v\n", err)
	} else {
		fmt.Println("  Removed peer: Node 4")
	}

	finalPeers := staticDiscovery.GetPeers()
	fmt.Printf("  Final peer count: %d\n", len(finalPeers))

	// Step 5: Show when to use static discovery
	fmt.Println("\nStep 5: When to Use Static Discovery")
	printStaticDiscoveryGuidance()

	return nil
}

// runKubernetesDiscoveryExample demonstrates Kubernetes StatefulSet discovery.
// This method uses headless services and DNS to discover pods in a StatefulSet.
func runKubernetesDiscoveryExample(ctx context.Context) error {
	fmt.Println("--- Kubernetes Discovery ---")
	fmt.Println()
	fmt.Println("Kubernetes discovery uses headless services to find StatefulSet pods.")
	fmt.Println("This is the recommended approach for Kubernetes deployments.")
	fmt.Println()

	// Step 1: Configure Kubernetes discovery
	fmt.Println("Step 1: Configuring Kubernetes discovery...")

	k8sConfig := discovery.KubernetesConfig{
		ServiceName:     *k8sService,
		Namespace:       *k8sNamespace,
		Port:            63001,
		ClusterDomain:   "cluster.local",
		RefreshInterval: *refreshInterval,
	}

	if k8sConfig.Namespace == "" {
		fmt.Println("  Namespace: auto-detect (from environment)")
	} else {
		fmt.Printf("  Namespace: %s\n", k8sConfig.Namespace)
	}
	fmt.Printf("  Service: %s\n", k8sConfig.ServiceName)
	fmt.Printf("  Port: %d\n", k8sConfig.Port)

	// Step 2: Create Kubernetes discovery instance
	fmt.Println("\nStep 2: Creating Kubernetes discovery instance...")

	k8sDiscovery, err := discovery.NewKubernetesDiscovery(k8sConfig)
	if err != nil {
		return &dragonerrors.WrappedMessageError{Message: "failed to create Kubernetes discovery", Cause: err}
	}
	defer k8sDiscovery.Close()

	fmt.Printf("  Kubernetes Discovery created successfully\n")
	fmt.Printf("  Method name: %s\n", k8sDiscovery.Name())
	fmt.Printf("  Detected namespace: %s\n", k8sDiscovery.GetNamespace())
	fmt.Printf("  StatefulSet: %s\n", k8sDiscovery.GetStatefulSetName())

	// Step 3: Perform discovery
	fmt.Println("\nStep 3: Discovering pods via Kubernetes DNS...")
	fmt.Println("  Note: This will only work when running inside Kubernetes")

	discoveryCtx, cancel := context.WithTimeout(ctx, *discoveryTimeout)
	defer cancel()

	discoveredPeers, err := k8sDiscovery.Discover(discoveryCtx)
	if err != nil {
		fmt.Printf("  Discovery failed: %v\n", err)
		fmt.Println()
		fmt.Println("  This is expected when running outside Kubernetes.")
		fmt.Println("  When running in a pod, DNS discovery will find StatefulSet peers.")
	} else {
		printDiscoveredPeers("Kubernetes", discoveredPeers)
	}

	// Step 4: Show Kubernetes deployment example
	fmt.Println("\nStep 4: Kubernetes Deployment Configuration")
	printKubernetesDeploymentExample()

	return nil
}

// runDiscoveryManagerExample demonstrates the discovery manager.
// The manager coordinates multiple discovery methods with prioritization and failover.
// This is the recommended approach for production deployments.
func runDiscoveryManagerExample(ctx context.Context) error {
	fmt.Println("--- Discovery Manager (Production Recommended) ---")
	fmt.Println()
	fmt.Println("The Discovery Manager coordinates multiple discovery methods.")
	fmt.Println("It tries each method in order until one finds enough peers")
	fmt.Println("to form a quorum. This provides resilience and flexibility.")
	fmt.Println()

	// Validate node configuration
	if *nodeID == 0 {
		return &dragonerrors.MessageError{Message: "node-id is required for manager mode"}
	}

	// Step 1: Create discovery methods
	fmt.Println("Step 1: Creating discovery methods...")

	var methods []discovery.Method

	// Method 1: DNS Discovery (highest priority for production)
	fmt.Println("  Creating DNS discovery method...")
	dnsDiscovery, err := discovery.NewDNSDiscovery(discovery.DNSConfig{
		SRVRecord:       *srvRecord,
		RefreshInterval: *refreshInterval,
	})
	if err != nil {
		fmt.Printf("    Warning: DNS discovery creation failed: %v\n", err)
	} else {
		methods = append(methods, dnsDiscovery)
		fmt.Printf("    Added: %s (SRV: %s)\n", dnsDiscovery.Name(), *srvRecord)
	}

	// Method 2: Kubernetes Discovery
	fmt.Println("  Creating Kubernetes discovery method...")
	k8sDiscovery, err := discovery.NewKubernetesDiscovery(discovery.KubernetesConfig{
		ServiceName: *k8sService,
		Namespace:   *k8sNamespace,
	})
	if err != nil {
		fmt.Printf("    Warning: Kubernetes discovery creation failed: %v\n", err)
	} else {
		methods = append(methods, k8sDiscovery)
		fmt.Printf("    Added: %s (service: %s)\n", k8sDiscovery.Name(), *k8sService)
	}

	// Method 3: Multicast Discovery (LAN fallback)
	fmt.Println("  Creating multicast discovery method...")
	multicastConfig := &discovery.MulticastConfig{
		MulticastGroup:    *multicastGroup,
		Port:              *multicastPort,
		BroadcastInterval: *broadcastInterval,
		PeerTTL:           *peerTTL,
		SendQueryOnStart:  true,
	}
	if *psk != "" {
		multicastConfig.ClusterSecret = *psk
	}

	multicastDiscovery, err := discovery.NewMulticastDiscovery(multicastConfig)
	if err != nil {
		fmt.Printf("    Warning: Multicast discovery creation failed: %v\n", err)
	} else {
		methods = append(methods, multicastDiscovery)
		fmt.Printf("    Added: %s (group: %s)\n", multicastDiscovery.Name(), *multicastGroup)
	}

	// Method 4: Static Discovery (last resort fallback)
	if *peers != "" {
		fmt.Println("  Creating static discovery method...")
		peerAddresses := strings.Split(*peers, ",")
		staticDiscovery, err := discovery.NewStaticDiscovery(peerAddresses)
		if err != nil {
			fmt.Printf("    Warning: Static discovery creation failed: %v\n", err)
		} else {
			methods = append(methods, staticDiscovery)
			fmt.Printf("    Added: %s (%d peers)\n", staticDiscovery.Name(), len(peerAddresses))
		}
	}

	if len(methods) == 0 {
		return &dragonerrors.MessageError{Message: "no discovery methods could be created"}
	}

	// Step 2: Create discovery manager configuration
	fmt.Println("\nStep 2: Configuring discovery manager...")

	managerConfig := &discovery.Config{
		Methods:          methods,
		MinPeers:         *minPeers,
		DiscoveryTimeout: *discoveryTimeout,
		RetryInterval:    *refreshInterval,
	}

	fmt.Printf("  Minimum peers for quorum: %d\n", managerConfig.MinPeers)
	fmt.Printf("  Discovery timeout: %s\n", managerConfig.DiscoveryTimeout)
	fmt.Printf("  Retry interval: %s\n", managerConfig.RetryInterval)
	fmt.Printf("  Discovery methods: %d\n", len(managerConfig.Methods))
	fmt.Println("  Method priority (first to last):")
	for i, m := range managerConfig.Methods {
		fmt.Printf("    %d. %s\n", i+1, m.Name())
	}

	// Step 3: Create logger for manager
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "text",
		Output: "stdout",
	})
	if err != nil {
		return &dragonerrors.WrappedMessageError{Message: "failed to create logger", Cause: err}
	}

	// Step 4: Create discovery manager
	fmt.Println("\nStep 3: Creating discovery manager...")

	self := discovery.Peer{
		NodeID:   *nodeID,
		Address:  *address,
		RaftAddr: *address,
		Metadata: map[string]string{
			"started_at": time.Now().Format(time.RFC3339),
			"version":    "1.0.0",
		},
	}

	manager, err := discovery.NewManager(managerConfig, self, log)
	if err != nil {
		return &dragonerrors.WrappedMessageError{Message: "failed to create discovery manager", Cause: err}
	}
	defer manager.Close()

	fmt.Println("  Discovery Manager created successfully")

	// Step 4: Bootstrap the cluster
	fmt.Println("\nStep 4: Bootstrapping cluster...")
	fmt.Println("  Attempting discovery with each method in priority order...")

	bootstrapCtx, cancel := context.WithTimeout(ctx, *discoveryTimeout)
	defer cancel()

	if err := manager.Bootstrap(bootstrapCtx); err != nil {
		fmt.Printf("  Bootstrap failed: %v\n", err)
		fmt.Println()
		fmt.Println("  This is expected in a demo environment without real peers.")
		fmt.Println("  In production, ensure at least one discovery method can find peers.")
	} else {
		fmt.Println("  Bootstrap successful!")
		peers := manager.GetPeers()
		printDiscoveredPeers("Manager", peers)
	}

	// Step 5: Show manager statistics
	fmt.Println("\nStep 5: Manager Statistics")
	stats := manager.GetStats()
	fmt.Printf("  Peer count: %d\n", stats.PeerCount)
	fmt.Printf("  Discovery methods: %d\n", stats.DiscoveryMethods)
	fmt.Printf("  Self: Node %d at %s\n", stats.Self.NodeID, stats.Self.Address)

	// Step 6: Demonstrate health checking with periodic discovery
	fmt.Println("\nStep 6: Health Checking with Periodic Discovery")
	fmt.Println("  In production, periodically refresh the peer list to detect:")
	fmt.Println("    - New nodes joining the cluster")
	fmt.Println("    - Failed nodes leaving the cluster")
	fmt.Println("    - Network partitions and recovery")
	fmt.Println()

	// Demonstrate WaitForQuorum (with short timeout for demo)
	fmt.Println("  Demonstrating WaitForQuorum (with 5s timeout for demo)...")
	quorumCtx, quorumCancel := context.WithTimeout(ctx, 5*time.Second)
	defer quorumCancel()

	peers, err := manager.WaitForQuorum(quorumCtx)
	if err != nil {
		fmt.Printf("  WaitForQuorum: %v (expected in demo)\n", err)
	} else {
		fmt.Printf("  WaitForQuorum: Found %d peers\n", len(peers))
	}

	// Step 7: Show production recommendations
	fmt.Println("\nStep 7: Production Recommendations")
	printProductionRecommendations()

	return nil
}

// printDiscoveredPeers displays information about discovered peers.
func printDiscoveredPeers(method string, peers []discovery.Peer) {
	fmt.Printf("\n  Discovered Peers via %s: %d\n", method, len(peers))
	fmt.Println("  " + strings.Repeat("-", 60))

	if len(peers) == 0 {
		fmt.Println("  (no peers discovered)")
		return
	}

	for _, peer := range peers {
		fmt.Printf("  Node %d:\n", peer.NodeID)
		fmt.Printf("    Address:   %s\n", peer.Address)
		fmt.Printf("    Raft Addr: %s\n", peer.RaftAddr)
		fmt.Printf("    Last Seen: %s\n", peer.LastSeen.Format(time.RFC3339))
		if peer.TTL > 0 {
			fmt.Printf("    TTL:       %s\n", peer.TTL)
		}
		if len(peer.Metadata) > 0 {
			fmt.Println("    Metadata:")
			for k, v := range peer.Metadata {
				fmt.Printf("      %s: %s\n", k, v)
			}
		}
	}
	fmt.Println("  " + strings.Repeat("-", 60))
}

// printDNSConfigurationExample shows example DNS configurations.
func printDNSConfigurationExample() {
	fmt.Println("  Example DNS SRV Records (BIND format):")
	fmt.Println()
	fmt.Println("  ; QuicBaaS cluster SRV records")
	fmt.Println("  _quicbaas._tcp.cluster.local. 300 IN SRV 10 100 63001 node1.cluster.local.")
	fmt.Println("  _quicbaas._tcp.cluster.local. 300 IN SRV 10 100 63001 node2.cluster.local.")
	fmt.Println("  _quicbaas._tcp.cluster.local. 300 IN SRV 10 100 63001 node3.cluster.local.")
	fmt.Println()
	fmt.Println("  ; A records for each node")
	fmt.Println("  node1.cluster.local. 300 IN A 192.168.1.100")
	fmt.Println("  node2.cluster.local. 300 IN A 192.168.1.101")
	fmt.Println("  node3.cluster.local. 300 IN A 192.168.1.102")
	fmt.Println()
	fmt.Println("  For Consul DNS:")
	fmt.Println("    SRV Record: _quicbaas._tcp.service.consul")
	fmt.Println()
	fmt.Println("  For CoreDNS/Kubernetes:")
	fmt.Println("    SRV Record: _raft._tcp.quicbaas.default.svc.cluster.local")
}

// printMulticastSecurityInfo displays security considerations for multicast.
func printMulticastSecurityInfo() {
	fmt.Println()
	fmt.Println("  PSK Authentication:")
	fmt.Println("    - When ClusterSecret is set, all messages are HMAC-SHA256 signed")
	fmt.Println("    - Recipients verify the signature before accepting messages")
	fmt.Println("    - Invalid signatures cause the message to be silently dropped")
	fmt.Println("    - All nodes in the cluster must use the same PSK")
	fmt.Println()
	fmt.Println("  Security Recommendations:")
	fmt.Println("    - Always use PSK in production environments")
	fmt.Println("    - Use a strong, randomly generated secret (min 32 bytes)")
	fmt.Println("    - Rotate the PSK periodically (requires coordinated restart)")
	fmt.Println("    - Consider network-level isolation (VLANs) in addition to PSK")
	fmt.Println()
	fmt.Println("  Generate a secure PSK:")
	fmt.Println("    openssl rand -base64 32")
}

// printStaticDiscoveryGuidance shows when to use static discovery.
func printStaticDiscoveryGuidance() {
	fmt.Println()
	fmt.Println("  Best suited for:")
	fmt.Println("    - Small clusters (3-5 nodes) with stable infrastructure")
	fmt.Println("    - Development and testing environments")
	fmt.Println("    - Air-gapped or isolated networks without DNS")
	fmt.Println("    - Fallback when dynamic discovery fails")
	fmt.Println()
	fmt.Println("  Limitations:")
	fmt.Println("    - Requires manual updates when nodes change")
	fmt.Println("    - No automatic detection of failed nodes")
	fmt.Println("    - Configuration must be synchronized across all nodes")
	fmt.Println()
	fmt.Println("  Configuration File Example (YAML):")
	fmt.Println("    discovery:")
	fmt.Println("      method: static")
	fmt.Println("      peers:")
	fmt.Println("        - \"1:192.168.1.100:63001\"")
	fmt.Println("        - \"2:192.168.1.101:63001\"")
	fmt.Println("        - \"3:192.168.1.102:63001\"")
}

// printKubernetesDeploymentExample shows example Kubernetes manifests.
func printKubernetesDeploymentExample() {
	fmt.Println()
	fmt.Println("  StatefulSet with Headless Service:")
	fmt.Println()
	fmt.Println("  apiVersion: v1")
	fmt.Println("  kind: Service")
	fmt.Println("  metadata:")
	fmt.Println("    name: quicbaas")
	fmt.Println("    labels:")
	fmt.Println("      app: quicbaas")
	fmt.Println("  spec:")
	fmt.Println("    clusterIP: None  # Headless service for StatefulSet")
	fmt.Println("    ports:")
	fmt.Println("      - port: 63001")
	fmt.Println("        name: raft")
	fmt.Println("    selector:")
	fmt.Println("      app: quicbaas")
	fmt.Println()
	fmt.Println("  ---")
	fmt.Println("  apiVersion: apps/v1")
	fmt.Println("  kind: StatefulSet")
	fmt.Println("  metadata:")
	fmt.Println("    name: quicbaas")
	fmt.Println("  spec:")
	fmt.Println("    serviceName: quicbaas")
	fmt.Println("    replicas: 3")
	fmt.Println("    selector:")
	fmt.Println("      matchLabels:")
	fmt.Println("        app: quicbaas")
	fmt.Println("    template:")
	fmt.Println("      metadata:")
	fmt.Println("        labels:")
	fmt.Println("          app: quicbaas")
	fmt.Println("      spec:")
	fmt.Println("        containers:")
	fmt.Println("          - name: quicbaas")
	fmt.Println("            image: quicbaas:latest")
	fmt.Println("            env:")
	fmt.Println("              - name: POD_NAMESPACE")
	fmt.Println("                valueFrom:")
	fmt.Println("                  fieldRef:")
	fmt.Println("                    fieldPath: metadata.namespace")
	fmt.Println()
	fmt.Println("  Pod DNS names will be:")
	fmt.Println("    quicbaas-0.quicbaas.default.svc.cluster.local")
	fmt.Println("    quicbaas-1.quicbaas.default.svc.cluster.local")
	fmt.Println("    quicbaas-2.quicbaas.default.svc.cluster.local")
}

// printProductionRecommendations shows best practices for production.
func printProductionRecommendations() {
	fmt.Println()
	fmt.Println("  1. Use Multiple Discovery Methods:")
	fmt.Println("     Configure the manager with multiple methods for resilience.")
	fmt.Println("     Recommended order: DNS -> Kubernetes -> Multicast -> Static")
	fmt.Println()
	fmt.Println("  2. Set Appropriate Timeouts:")
	fmt.Println("     - Discovery timeout: 30-60 seconds for initial bootstrap")
	fmt.Println("     - Retry interval: 5-10 seconds for ongoing discovery")
	fmt.Println("     - Peer TTL: 10-30 seconds for multicast")
	fmt.Println()
	fmt.Println("  3. Configure Quorum Correctly:")
	fmt.Println("     - MinPeers should be (cluster_size / 2) + 1")
	fmt.Println("     - For 3-node cluster: MinPeers = 2")
	fmt.Println("     - For 5-node cluster: MinPeers = 3")
	fmt.Println()
	fmt.Println("  4. Secure Your Discovery:")
	fmt.Println("     - Use PSK for multicast discovery")
	fmt.Println("     - Isolate discovery traffic with network policies")
	fmt.Println("     - Monitor for unauthorized discovery attempts")
	fmt.Println()
	fmt.Println("  5. Health Checking:")
	fmt.Println("     - Periodically refresh peer lists")
	fmt.Println("     - Monitor peer count for unexpected changes")
	fmt.Println("     - Alert on quorum loss conditions")
	fmt.Println()
	fmt.Println("  6. Graceful Shutdown:")
	fmt.Println("     - Always call Deregister() before shutdown")
	fmt.Println("     - Use manager.Close() to cleanup all methods")
	fmt.Println("     - Allow time for leave messages to propagate")
}
