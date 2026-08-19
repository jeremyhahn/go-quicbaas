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

package ha

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// PartitionDetector monitors network connectivity and handles network partitions.
type PartitionDetector struct {
	raftNode interfaces.RaftNode
	config   *PartitionConfig
	logger   interfaces.Logger

	// partition state
	currentPartition *Partition
	partitionMu      sync.RWMutex

	// peer tracking
	peers   map[uint64]*PeerState
	peersMu sync.RWMutex

	// read-only mode
	readOnlyMode   bool
	readOnlyModeMu sync.RWMutex

	// metrics
	metrics *partitionMetrics

	// lifecycle
	ctx      context.Context
	cancel   context.CancelFunc
	stopChan chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// PartitionConfig configures partition detection and handling.
type PartitionConfig struct {
	// DetectionInterval is how often to check for partitions
	DetectionInterval time.Duration

	// PeerTimeout is how long before considering a peer unreachable
	PeerTimeout time.Duration

	// HealthCheckTimeout is the timeout for individual health checks
	HealthCheckTimeout time.Duration

	// AutoRecovery enables automatic partition recovery
	AutoRecovery bool

	// ReadOnlyOnMinority puts node in read-only mode if in minority partition
	ReadOnlyOnMinority bool

	// Peers is the map of peer nodeID to address
	Peers map[uint64]string

	// ClusterCA is the certificate pool used to verify peer TLS certificates
	// during health probes. When nil, the system trust store is used. Set this
	// to the cluster's internal CA pool so peer certificates are verified
	// without relying on public CAs.
	ClusterCA *x509.CertPool
}

// Partition represents a network partition.
type Partition struct {
	DetectedAt time.Time
	Healed     bool
	HealedAt   time.Time
	Majority   []uint64 // Nodes in majority partition
	Minority   []uint64 // Nodes in minority partition
	Isolated   []uint64 // Completely isolated nodes
}

// PeerState tracks the state of a peer node.
type PeerState struct {
	NodeID      uint64
	Address     string
	LastSeen    time.Time
	Reachable   bool
	RTT         time.Duration
	LastChecked time.Time
}

// PartitionStatus represents the current partition state.
type PartitionStatus struct {
	Partitioned    bool
	Partition      *Partition
	ReadOnlyMode   bool
	ReachablePeers int
	TotalPeers     int
}

type partitionMetrics struct {
	partitionDetections  *metrics.Counter
	partitionHeals       *metrics.Counter
	partitionChecksTotal *metrics.CounterVec
	peerReachability     *metrics.GaugeVec
	partitionStatus      *metrics.Gauge
	readOnlyMode         *metrics.Gauge
	peerRTT              *metrics.HistogramVec
}

// NewPartitionDetector creates a new partition detector.
func NewPartitionDetector(raftNode interfaces.RaftNode, config *PartitionConfig, logger interfaces.Logger) (*PartitionDetector, error) {
	if raftNode == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "ha.NewPartitionDetector",
			Err:  &errors.MessageError{Message: "raft node cannot be nil"},
		}
	}
	if config == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "ha.NewPartitionDetector",
			Err:  &errors.MessageError{Message: "config cannot be nil"},
		}
	}
	if logger == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "ha.NewPartitionDetector",
			Err:  &errors.MessageError{Message: "logger cannot be nil"},
		}
	}

	// Apply defaults
	if config.DetectionInterval == 0 {
		config.DetectionInterval = 10 * time.Second
	}
	if config.PeerTimeout == 0 {
		config.PeerTimeout = 30 * time.Second
	}
	if config.HealthCheckTimeout == 0 {
		config.HealthCheckTimeout = 5 * time.Second
	}

	// The detector owns this context for its whole lifetime; cancel is retained
	// on the struct and called by Stop. gosec cannot follow it across the
	// struct field.
	ctx, cancel := context.WithCancel(context.Background()) // #nosec G118 -- cancel is stored on pd and invoked in Stop.

	pd := &PartitionDetector{
		raftNode: raftNode,
		config:   config,
		logger:   logger.WithField("component", "partition-detector"),
		peers:    make(map[uint64]*PeerState),
		ctx:      ctx,
		cancel:   cancel,
		stopChan: make(chan struct{}),
	}

	// Initialize peer states
	for nodeID, address := range config.Peers {
		pd.peers[nodeID] = &PeerState{
			NodeID:    nodeID,
			Address:   address,
			Reachable: true,
			LastSeen:  time.Now(),
		}
	}

	pd.initMetrics()

	return pd, nil
}

// initMetrics initializes Prometheus metrics.
func (pd *PartitionDetector) initMetrics() {
	pd.metrics = &partitionMetrics{
		partitionDetections: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "partition_detections_total",
			Help:      "Total number of network partitions detected",
		}),

		partitionHeals: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "partition_heals_total",
			Help:      "Total number of partition heal events",
		}),

		partitionChecksTotal: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "partition_checks_total",
			Help:      "Total number of partition checks by status",
		}, []string{"status"}),

		peerReachability: metrics.NewGaugeVec(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "peer_reachability",
			Help:      "Peer reachability status (1=reachable, 0=unreachable)",
		}, []string{"node_id"}),

		partitionStatus: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "partition_status",
			Help:      "Current partition status (1=partitioned, 0=normal)",
		}),

		readOnlyMode: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "read_only_mode",
			Help:      "Read-only mode status (1=enabled, 0=disabled)",
		}),

		peerRTT: metrics.NewHistogramVec(metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "ha",
			Name:      "peer_rtt_seconds",
			Help:      "Peer round-trip time in seconds",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
		}, []string{"node_id"}),
	}
}

// Start begins monitoring for network partitions.
func (pd *PartitionDetector) Start() error {
	pd.logger.Info("Starting partition detector")

	pd.wg.Add(1)
	go pd.monitorLoop()

	return nil
}

// Stop gracefully stops the partition detector.
func (pd *PartitionDetector) Stop() error {
	var stopErr error

	pd.stopOnce.Do(func() {
		pd.logger.Info("Stopping partition detector")
		pd.cancel()
		close(pd.stopChan)
		pd.wg.Wait()
		pd.logger.Info("Partition detector stopped")
	})

	return stopErr
}

// monitorLoop continuously monitors for network partitions.
func (pd *PartitionDetector) monitorLoop() {
	defer pd.wg.Done()

	ticker := time.NewTicker(pd.config.DetectionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := pd.checkForPartition(); err != nil {
				pd.logger.WithError(err).Error("Partition check failed")
			}

		case <-pd.stopChan:
			return

		case <-pd.ctx.Done():
			return
		}
	}
}

// checkForPartition checks if a network partition exists.
func (pd *PartitionDetector) checkForPartition() error {
	ctx, cancel := context.WithTimeout(pd.ctx, pd.config.HealthCheckTimeout)
	defer cancel()

	partition, err := pd.DetectPartition(ctx)
	if err != nil {
		pd.metrics.partitionChecksTotal.With("error").Inc()
		return err
	}

	pd.partitionMu.Lock()
	wasPartitioned := pd.currentPartition != nil && !pd.currentPartition.Healed
	isPartitioned := partition != nil
	pd.partitionMu.Unlock()

	switch {
	case isPartitioned && !wasPartitioned:
		// New partition detected
		pd.metrics.partitionDetections.Inc()
		pd.metrics.partitionStatus.Set(1)
		pd.metrics.partitionChecksTotal.With("partitioned").Inc()

		pd.logger.WithFields(map[string]interface{}{
			"majority": partition.Majority,
			"minority": partition.Minority,
			"isolated": partition.Isolated,
		}).Warn("Network partition detected")

		// Handle partition
		if err := pd.handlePartition(ctx, partition); err != nil {
			pd.logger.WithError(err).Error("Failed to handle partition")
		}
	case !isPartitioned && wasPartitioned:
		// Partition healed
		pd.metrics.partitionHeals.Inc()
		pd.metrics.partitionStatus.Set(0)
		pd.metrics.partitionChecksTotal.With("healed").Inc()

		pd.logger.Info("Network partition healed")

		// Handle recovery
		if pd.config.AutoRecovery {
			if err := pd.recoverFromPartition(ctx); err != nil {
				pd.logger.WithError(err).Error("Failed to recover from partition")
			}
		}
	case !isPartitioned:
		pd.metrics.partitionChecksTotal.With("normal").Inc()
	}

	return nil
}

// DetectPartition monitors network connectivity and detects partitions.
func (pd *PartitionDetector) DetectPartition(ctx context.Context) (*Partition, error) {
	// Check peer reachability
	reachability := pd.checkPeerReachability(ctx)

	// Analyze connectivity
	if pd.isPartitioned(reachability) {
		partition := &Partition{
			DetectedAt: time.Now(),
			Majority:   pd.findMajorityPartition(reachability),
			Minority:   pd.findMinorityPartition(reachability),
			Isolated:   pd.findIsolatedNodes(reachability),
		}

		pd.partitionMu.Lock()
		pd.currentPartition = partition
		pd.partitionMu.Unlock()

		return partition, nil
	}

	// No partition detected
	pd.partitionMu.Lock()
	if pd.currentPartition != nil && !pd.currentPartition.Healed {
		pd.currentPartition.Healed = true
		pd.currentPartition.HealedAt = time.Now()
	}
	pd.partitionMu.Unlock()

	return nil, nil
}

// checkPeerReachability checks reachability of all peers.
func (pd *PartitionDetector) checkPeerReachability(ctx context.Context) map[uint64]bool {
	results := make(map[uint64]bool)

	pd.peersMu.RLock()
	peers := make([]*PeerState, 0, len(pd.peers))
	for _, peer := range pd.peers {
		peers = append(peers, peer)
	}
	pd.peersMu.RUnlock()

	// Check each peer in parallel
	var wg sync.WaitGroup
	resultsCh := make(chan struct {
		nodeID    uint64
		reachable bool
		rtt       time.Duration
	}, len(peers))

	for _, peer := range peers {
		wg.Add(1)
		go func(p *PeerState) {
			defer wg.Done()
			reachable, rtt := pd.pingPeer(ctx, p)
			resultsCh <- struct {
				nodeID    uint64
				reachable bool
				rtt       time.Duration
			}{p.NodeID, reachable, rtt}
		}(peer)
	}

	wg.Wait()
	close(resultsCh)

	// Collect results
	for result := range resultsCh {
		results[result.nodeID] = result.reachable

		// Update peer state
		pd.peersMu.Lock()
		if peer, exists := pd.peers[result.nodeID]; exists {
			peer.Reachable = result.reachable
			peer.LastChecked = time.Now()
			if result.reachable {
				peer.LastSeen = time.Now()
				peer.RTT = result.rtt
			}
		}
		pd.peersMu.Unlock()

		// Update metrics
		nodeIDStr := fmt.Sprintf("%d", result.nodeID)
		if result.reachable {
			pd.metrics.peerReachability.With(nodeIDStr).Set(1)
			pd.metrics.peerRTT.With(nodeIDStr).Observe(result.rtt.Seconds())
		} else {
			pd.metrics.peerReachability.With(nodeIDStr).Set(0)
		}
	}

	return results
}

// healthURL builds the GET /health URL for a peer. The peer address is
// expected to be a host:port string (as supplied in PartitionConfig.Peers).
// It uses HTTPS unless the address already carries an explicit scheme.
func healthURL(address string) string {
	if strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://") {
		return strings.TrimRight(address, "/") + "/health"
	}
	return "https://" + address + "/health"
}

// pingPeer performs a real HTTP GET /health probe against the peer's REST API
// endpoint. It uses the HealthCheckTimeout from the detector config (bound by
// any deadline already present on ctx). Returns (true, rtt) when the peer
// responds with HTTP 200 or 503 — 503 indicates a sealed/not-ready node that
// is still reachable, mirroring the semantics used by the qrdb REST transport's
// reachable() helper. Any transport error or non-2xx/503 status is treated as
// unreachable.
//
// TLS certificate verification is intentionally skipped for the health probe:
// partition detection is a liveness signal, not a security boundary, and peer
// certificates may be self-signed within the cluster. Mutual authentication for
// data-plane operations is handled separately at the transport layer.
func (pd *PartitionDetector) pingPeer(ctx context.Context, peer *PeerState) (bool, time.Duration) {
	start := time.Now()

	timeout := pd.config.HealthCheckTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	// Scope the request to the health-check timeout, respecting any tighter
	// deadline already set on the parent context.
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	url := healthURL(peer.Address)

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url, nil)
	if err != nil {
		pd.logger.WithFields(map[string]interface{}{
			"node_id": peer.NodeID,
			"address": peer.Address,
			"error":   err.Error(),
		}).Debug("pingPeer: failed to build health request")
		return false, time.Since(start)
	}

	// Use an ad-hoc client per probe; sharing a persistent client across all
	// ping goroutines would require additional locking without meaningful benefit
	// given the low frequency of health checks.
	//
	// Peer certificates are verified against ClusterCA when provided, or
	// against the system trust store otherwise. InsecureSkipVerify is
	// never used — peer liveness is a security boundary because a spoofed
	// health response could cause a healthy minority to incorrectly enter
	// read-only mode.
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    pd.config.ClusterCA, // nil → system pool
				MinVersion: tls.VersionTLS13,
			},
		},
	}

	resp, err := client.Do(req)
	rtt := time.Since(start)
	if err != nil {
		pd.logger.WithFields(map[string]interface{}{
			"node_id": peer.NodeID,
			"address": peer.Address,
			"error":   err.Error(),
		}).Debug("pingPeer: health request failed")
		return false, rtt
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	// 200 = healthy; 503 = sealed/not-ready but the process is alive.
	reachable := resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusServiceUnavailable

	if !reachable {
		pd.logger.WithFields(map[string]interface{}{
			"node_id":     peer.NodeID,
			"address":     peer.Address,
			"status_code": resp.StatusCode,
		}).Debug("pingPeer: peer returned unexpected health status")
	}

	return reachable, rtt
}

// isPartitioned determines if the cluster is partitioned based on reachability.
func (pd *PartitionDetector) isPartitioned(reachability map[uint64]bool) bool {
	unreachableCount := 0
	for _, reachable := range reachability {
		if !reachable {
			unreachableCount++
		}
	}

	// If any peers are unreachable, we might be partitioned
	return unreachableCount > 0
}

// findMajorityPartition identifies nodes in the majority partition.
func (pd *PartitionDetector) findMajorityPartition(reachability map[uint64]bool) []uint64 {
	majority := make([]uint64, 0)
	for nodeID, reachable := range reachability {
		if reachable {
			majority = append(majority, nodeID)
		}
	}
	return majority
}

// findMinorityPartition identifies nodes in the minority partition.
func (pd *PartitionDetector) findMinorityPartition(reachability map[uint64]bool) []uint64 {
	minority := make([]uint64, 0)
	for nodeID, reachable := range reachability {
		if !reachable {
			minority = append(minority, nodeID)
		}
	}
	return minority
}

// findIsolatedNodes identifies completely isolated nodes.
func (pd *PartitionDetector) findIsolatedNodes(reachability map[uint64]bool) []uint64 {
	// In a simple implementation, isolated nodes are same as unreachable
	return pd.findMinorityPartition(reachability)
}

// handlePartition responds to a detected partition.
func (pd *PartitionDetector) handlePartition(ctx context.Context, partition *Partition) error {
	// Check if we're in the minority partition
	if pd.config.ReadOnlyOnMinority && pd.isInMinority(partition) {
		return pd.enterReadOnlyMode()
	}

	return nil
}

// isInMinority checks if the local node is in the minority partition.
func (pd *PartitionDetector) isInMinority(partition *Partition) bool {
	// In Raft, minority nodes lose quorum and cannot make progress
	// If we can't reach majority of peers, we're in minority
	totalPeers := len(pd.config.Peers) + 1        // +1 for self
	reachablePeers := len(partition.Majority) + 1 // +1 for self
	quorum := (totalPeers / 2) + 1

	return reachablePeers < quorum
}

// enterReadOnlyMode puts the node in read-only mode.
func (pd *PartitionDetector) enterReadOnlyMode() error {
	pd.readOnlyModeMu.Lock()
	defer pd.readOnlyModeMu.Unlock()

	if pd.readOnlyMode {
		return nil // Already in read-only mode
	}

	pd.readOnlyMode = true
	pd.metrics.readOnlyMode.Set(1)

	pd.logger.Warn("Entering read-only mode due to minority partition")

	return nil
}

// exitReadOnlyMode exits read-only mode.
func (pd *PartitionDetector) exitReadOnlyMode() error {
	pd.readOnlyModeMu.Lock()
	defer pd.readOnlyModeMu.Unlock()

	if !pd.readOnlyMode {
		return nil // Already in read-write mode
	}

	pd.readOnlyMode = false
	pd.metrics.readOnlyMode.Set(0)

	pd.logger.Info("Exiting read-only mode, partition healed")

	return nil
}

// IsReadOnlyMode returns true if the node is in read-only mode.
func (pd *PartitionDetector) IsReadOnlyMode() bool {
	pd.readOnlyModeMu.RLock()
	defer pd.readOnlyModeMu.RUnlock()
	return pd.readOnlyMode
}

// recoverFromPartition handles recovery after partition heals.
func (pd *PartitionDetector) recoverFromPartition(ctx context.Context) error {
	pd.logger.Info("Recovering from partition")

	// Exit read-only mode if enabled
	if err := pd.exitReadOnlyMode(); err != nil {
		return err
	}

	// Reconcile state with majority partition
	// In a real implementation, this would involve:
	// 1. Checking for conflicting writes
	// 2. Resolving conflicts using conflict resolution strategy
	// 3. Syncing state with majority

	pd.logger.Info("Partition recovery completed")

	return nil
}

// GetStatus returns the current partition status.
func (pd *PartitionDetector) GetStatus() PartitionStatus {
	pd.partitionMu.RLock()
	partition := pd.currentPartition
	pd.partitionMu.RUnlock()

	partitioned := partition != nil && !partition.Healed

	pd.peersMu.RLock()
	reachableCount := 0
	totalPeers := len(pd.peers)
	for _, peer := range pd.peers {
		if peer.Reachable {
			reachableCount++
		}
	}
	pd.peersMu.RUnlock()

	return PartitionStatus{
		Partitioned:    partitioned,
		Partition:      partition,
		ReadOnlyMode:   pd.IsReadOnlyMode(),
		ReachablePeers: reachableCount,
		TotalPeers:     totalPeers,
	}
}

// GetPeerStates returns the current state of all peers.
func (pd *PartitionDetector) GetPeerStates() map[uint64]PeerState {
	pd.peersMu.RLock()
	defer pd.peersMu.RUnlock()

	states := make(map[uint64]PeerState, len(pd.peers))
	for nodeID, peer := range pd.peers {
		states[nodeID] = *peer
	}
	return states
}
