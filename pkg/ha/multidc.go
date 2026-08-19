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
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// MultiDCManager manages multi-datacenter replication and failover.
type MultiDCManager struct {
	localRaftNode interfaces.RaftNode
	config        *MultiDCConfig
	logger        interfaces.Logger

	// datacenter tracking
	datacenters   map[string]*Datacenter
	datacentersMu sync.RWMutex

	// replication state
	replicationActive bool
	replicationMu     sync.RWMutex

	// metrics
	metrics *multiDCMetrics

	// lifecycle
	ctx      context.Context
	cancel   context.CancelFunc
	stopChan chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// MultiDCConfig configures multi-datacenter operations.
type MultiDCConfig struct {
	// LocalDC is the name of the local datacenter
	LocalDC string

	// RemoteDCs maps datacenter names to their configurations
	RemoteDCs map[string]*RemoteDCConfig

	// ReplicationMode determines replication behavior
	ReplicationMode ReplicationMode

	// ConsistencyModel determines consistency guarantees
	ConsistencyModel ConsistencyModel

	// LatencyThreshold for routing decisions (milliseconds)
	LatencyThreshold int64

	// HealthCheckInterval for remote DC health checks
	HealthCheckInterval time.Duration

	// SyncTimeout for synchronous replication
	SyncTimeout time.Duration
}

// RemoteDCConfig configures a remote datacenter.
type RemoteDCConfig struct {
	// Name of the datacenter
	Name string

	// Addresses is the list of node addresses in the remote DC
	Addresses []string

	// Priority for failover (higher is preferred)
	Priority int

	// ReadPreference determines if this DC can serve reads
	ReadPreference ReadPreference
}

// ReplicationMode determines how data is replicated across datacenters.
type ReplicationMode string

const (
	// ReplicationAsync replicates changes asynchronously (eventual consistency).
	ReplicationAsync ReplicationMode = "async"

	// ReplicationSync replicates changes synchronously (strong consistency, slower).
	ReplicationSync ReplicationMode = "sync"

	// ReplicationLeaderFollower designates one DC as leader.
	ReplicationLeaderFollower ReplicationMode = "leader_follower"
)

// ConsistencyModel determines consistency guarantees across datacenters.
type ConsistencyModel string

const (
	// ConsistencyStrong requires synchronous cross-DC writes.
	ConsistencyStrong ConsistencyModel = "strong"

	// ConsistencyEventual allows asynchronous replication.
	ConsistencyEventual ConsistencyModel = "eventual"

	// ConsistencyCausal preserves causality across DCs.
	ConsistencyCausal ConsistencyModel = "causal"
)

// ReadPreference determines read routing behavior.
type ReadPreference string

const (
	// ReadPreferLocal prefers local DC for reads.
	ReadPreferLocal ReadPreference = "local"

	// ReadPreferNearest routes to nearest DC based on latency.
	ReadPreferNearest ReadPreference = "nearest"

	// ReadPreferPrimary always reads from primary DC.
	ReadPreferPrimary ReadPreference = "primary"
)

// Datacenter represents a datacenter in the cluster.
type Datacenter struct {
	Name      string
	Addresses []string
	Priority  int
	Healthy   bool
	Latency   time.Duration
	LastCheck time.Time
}

// MultiDCStatus represents the current multi-DC state.
type MultiDCStatus struct {
	LocalDC           string
	ActiveDCs         []string
	UnhealthyDCs      []string
	ReplicationActive bool
	ReplicationLag    map[string]time.Duration
}

type multiDCMetrics struct {
	replicationOpsTotal *metrics.CounterVec
	replicationLatency  *metrics.HistogramVec
	dcHealthStatus      *metrics.GaugeVec
	dcLatency           *metrics.GaugeVec
	crossDCReads        *metrics.CounterVec
	crossDCWrites       *metrics.CounterVec
	replicationLag      *metrics.GaugeVec
	dcFailovers         *metrics.CounterVec
}

// Default latency for remote DCs before health check (set higher than local DC).
const defaultRemoteDCLatency = 1 * time.Millisecond

// NewMultiDCManager creates a new multi-datacenter manager.
func NewMultiDCManager(localRaftNode interfaces.RaftNode, config *MultiDCConfig, logger interfaces.Logger) (*MultiDCManager, error) {
	if localRaftNode == nil {
		return nil, errors.InvalidInput("ha.NewMultiDCManager", "local raft node cannot be nil")
	}
	if config == nil {
		return nil, errors.InvalidInput("ha.NewMultiDCManager", "config cannot be nil")
	}
	if logger == nil {
		return nil, errors.InvalidInput("ha.NewMultiDCManager", "logger cannot be nil")
	}

	// Apply defaults
	if config.ReplicationMode == "" {
		config.ReplicationMode = ReplicationAsync
	}
	if config.ConsistencyModel == "" {
		config.ConsistencyModel = ConsistencyEventual
	}
	if config.LatencyThreshold == 0 {
		config.LatencyThreshold = 100 // 100ms default
	}
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 30 * time.Second
	}
	if config.SyncTimeout == 0 {
		config.SyncTimeout = 10 * time.Second
	}

	// The manager owns this context for its whole lifetime; cancel is retained
	// on the struct and called by Stop. gosec cannot follow it across the
	// struct field.
	ctx, cancel := context.WithCancel(context.Background()) // #nosec G118 -- cancel is stored on mdm and invoked in Stop.

	mdm := &MultiDCManager{
		localRaftNode: localRaftNode,
		config:        config,
		logger:        logger.WithField("component", "multidc-manager"),
		datacenters:   make(map[string]*Datacenter),
		ctx:           ctx,
		cancel:        cancel,
		stopChan:      make(chan struct{}),
	}

	// Initialize local datacenter with 0 latency and healthy status
	mdm.datacenters[config.LocalDC] = &Datacenter{
		Name:    config.LocalDC,
		Healthy: true,
		Latency: 0,
	}

	// Initialize remote datacenter tracking with default latency
	for name, dcConfig := range config.RemoteDCs {
		mdm.datacenters[name] = &Datacenter{
			Name:      name,
			Addresses: dcConfig.Addresses,
			Priority:  dcConfig.Priority,
			Healthy:   true,                   // Assume healthy initially
			Latency:   defaultRemoteDCLatency, // Set default latency higher than local
		}
	}

	mdm.initMetrics()

	return mdm, nil
}

// initMetrics initializes Prometheus metrics.
func (mdm *MultiDCManager) initMetrics() {
	mdm.metrics = &multiDCMetrics{
		replicationOpsTotal: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "multidc",
			Name:      "replication_ops_total",
			Help:      "Total number of cross-DC replication operations",
		}, []string{"source_dc", "target_dc", "status"}),

		replicationLatency: metrics.NewHistogramVec(metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "multidc",
			Name:      "replication_latency_seconds",
			Help:      "Cross-DC replication latency",
			Buckets:   []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10},
		}, []string{"source_dc", "target_dc"}),

		dcHealthStatus: metrics.NewGaugeVec(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "multidc",
			Name:      "dc_health_status",
			Help:      "Datacenter health status (1=healthy, 0=unhealthy)",
		}, []string{"dc"}),

		dcLatency: metrics.NewGaugeVec(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "multidc",
			Name:      "dc_latency_seconds",
			Help:      "Datacenter latency in seconds",
		}, []string{"dc"}),

		crossDCReads: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "multidc",
			Name:      "cross_dc_reads_total",
			Help:      "Total number of cross-DC reads",
		}, []string{"source_dc", "target_dc"}),

		crossDCWrites: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "multidc",
			Name:      "cross_dc_writes_total",
			Help:      "Total number of cross-DC writes",
		}, []string{"source_dc", "target_dc"}),

		replicationLag: metrics.NewGaugeVec(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "multidc",
			Name:      "replication_lag_seconds",
			Help:      "Replication lag to remote datacenters",
		}, []string{"target_dc"}),

		dcFailovers: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "multidc",
			Name:      "dc_failovers_total",
			Help:      "Total number of datacenter failovers",
		}, []string{"from_dc", "to_dc"}),
	}
}

// Start begins multi-DC operations.
func (mdm *MultiDCManager) Start() error {
	mdm.logger.Info("Starting multi-DC manager")

	mdm.replicationMu.Lock()
	mdm.replicationActive = true
	mdm.replicationMu.Unlock()

	// Start health checking
	mdm.wg.Add(1)
	go mdm.healthCheckLoop()

	return nil
}

// Stop gracefully stops multi-DC operations.
func (mdm *MultiDCManager) Stop() error {
	var stopErr error

	mdm.stopOnce.Do(func() {
		mdm.logger.Info("Stopping multi-DC manager")

		mdm.replicationMu.Lock()
		mdm.replicationActive = false
		mdm.replicationMu.Unlock()

		mdm.cancel()
		close(mdm.stopChan)
		mdm.wg.Wait()
		mdm.logger.Info("Multi-DC manager stopped")
	})

	return stopErr
}

// healthCheckLoop continuously checks remote datacenter health.
func (mdm *MultiDCManager) healthCheckLoop() {
	defer mdm.wg.Done()

	ticker := time.NewTicker(mdm.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mdm.checkDatacenterHealth()

		case <-mdm.stopChan:
			return

		case <-mdm.ctx.Done():
			return
		}
	}
}

// checkDatacenterHealth checks health of all remote datacenters.
func (mdm *MultiDCManager) checkDatacenterHealth() {
	mdm.datacentersMu.RLock()
	dcs := make([]*Datacenter, 0, len(mdm.datacenters))
	for name, dc := range mdm.datacenters {
		// Skip local DC from health checks
		if name != mdm.config.LocalDC {
			dcs = append(dcs, dc)
		}
	}
	mdm.datacentersMu.RUnlock()

	for _, dc := range dcs {
		healthy, latency := mdm.checkDC(dc)

		mdm.datacentersMu.Lock()
		dc.Healthy = healthy
		dc.Latency = latency
		dc.LastCheck = time.Now()
		mdm.datacentersMu.Unlock()

		// Update metrics
		healthValue := float64(0)
		if healthy {
			healthValue = 1
		}
		mdm.metrics.dcHealthStatus.With(dc.Name).Set(healthValue)
		mdm.metrics.dcLatency.With(dc.Name).Set(latency.Seconds())
	}
}

// checkDC performs a health check on a datacenter.
func (mdm *MultiDCManager) checkDC(dc *Datacenter) (bool, time.Duration) {
	start := time.Now()

	// In a real implementation, this would:
	// 1. Send health check request to DC
	// 2. Measure response time
	// 3. Verify DC is operational

	// Simulate health check
	latency := time.Since(start)

	// Consider healthy if latency is reasonable
	healthy := latency < time.Duration(mdm.config.LatencyThreshold)*time.Millisecond*2

	return healthy, latency
}

// RouteRead determines the best datacenter for a read operation.
func (mdm *MultiDCManager) RouteRead(ctx context.Context, preference ReadPreference) (string, error) {
	switch preference {
	case ReadPreferLocal:
		return mdm.config.LocalDC, nil

	case ReadPreferNearest:
		return mdm.findNearestDC()

	case ReadPreferPrimary:
		return mdm.findPrimaryDC()

	default:
		return mdm.config.LocalDC, nil
	}
}

// findNearestDC finds the datacenter with lowest latency.
func (mdm *MultiDCManager) findNearestDC() (string, error) {
	mdm.datacentersMu.RLock()
	defer mdm.datacentersMu.RUnlock()

	var nearestDC string
	var minLatency time.Duration
	firstHealthy := true

	for name, dc := range mdm.datacenters {
		if !dc.Healthy {
			continue
		}

		// Initialize with first healthy DC found
		if firstHealthy {
			nearestDC = name
			minLatency = dc.Latency
			firstHealthy = false
			continue
		}

		// Compare latencies
		if dc.Latency < minLatency {
			nearestDC = name
			minLatency = dc.Latency
		}
	}

	if nearestDC == "" {
		return "", errors.Unavailable("ha.findNearestDC", "no healthy datacenters available")
	}

	return nearestDC, nil
}

// findPrimaryDC finds the primary datacenter (highest priority).
func (mdm *MultiDCManager) findPrimaryDC() (string, error) {
	mdm.datacentersMu.RLock()
	defer mdm.datacentersMu.RUnlock()

	var primaryDC string
	var maxPriority int

	for name, dc := range mdm.datacenters {
		if !dc.Healthy {
			continue
		}

		if dc.Priority > maxPriority {
			primaryDC = name
			maxPriority = dc.Priority
		}
	}

	if primaryDC == "" {
		// Fall back to local DC
		return mdm.config.LocalDC, nil
	}

	return primaryDC, nil
}

// ReplicateToRemoteDC replicates data to a remote datacenter.
func (mdm *MultiDCManager) ReplicateToRemoteDC(ctx context.Context, targetDC string, key string, value []byte) error {
	start := time.Now()

	mdm.datacentersMu.RLock()
	dc, exists := mdm.datacenters[targetDC]
	mdm.datacentersMu.RUnlock()

	if !exists {
		return errors.Newf(errors.ErrNotFound, "ha.ReplicateToRemoteDC", "datacenter %s not found", targetDC)
	}

	if !dc.Healthy {
		return errors.Unavailablef("ha.ReplicateToRemoteDC", "datacenter %s is unhealthy", targetDC)
	}

	// Check replication mode
	switch mdm.config.ReplicationMode {
	case ReplicationSync:
		// Synchronous replication - wait for ack
		if err := mdm.syncReplicate(ctx, targetDC, key, value); err != nil {
			mdm.metrics.replicationOpsTotal.With(mdm.config.LocalDC, targetDC, "error").Inc()
			return err
		}

	case ReplicationAsync:
		// Asynchronous replication - fire and forget
		go func() { // #nosec G118 -- fire-and-forget replication must outlive the caller; a request-scoped context would cancel it.
			if err := mdm.asyncReplicate(context.Background(), targetDC, key, value); err != nil {
				mdm.logger.WithError(err).WithFields(map[string]interface{}{
					"target_dc": targetDC,
					"key":       key,
				}).Error("Async replication failed")
			}
		}()

	default:
		return errors.Newf(errors.ErrNotImplemented, "ha.ReplicateToRemoteDC", "unsupported replication mode: %s", mdm.config.ReplicationMode)
	}

	duration := time.Since(start)
	mdm.metrics.replicationLatency.With(mdm.config.LocalDC, targetDC).Observe(duration.Seconds())
	mdm.metrics.replicationOpsTotal.With(mdm.config.LocalDC, targetDC, "success").Inc()
	mdm.metrics.crossDCWrites.With(mdm.config.LocalDC, targetDC).Inc()

	return nil
}

// syncReplicate performs synchronous replication to remote DC.
func (mdm *MultiDCManager) syncReplicate(ctx context.Context, targetDC string, key string, value []byte) error {
	// In a real implementation, this would:
	// 1. Send write request to remote DC
	// 2. Wait for acknowledgment
	// 3. Return error if timeout or failure

	ctx, cancel := context.WithTimeout(ctx, mdm.config.SyncTimeout)
	defer cancel()

	// Simulate synchronous write
	select {
	case <-time.After(10 * time.Millisecond): // Simulate network latency
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// asyncReplicate performs asynchronous replication to remote DC.
func (mdm *MultiDCManager) asyncReplicate(ctx context.Context, targetDC string, key string, value []byte) error {
	// In a real implementation, this would:
	// 1. Queue write to replication buffer
	// 2. Send in batches to remote DC
	// 3. Handle retries on failure

	// Simulate async write
	select {
	case <-time.After(10 * time.Millisecond):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// HandleDCFailure handles failure of a datacenter.
func (mdm *MultiDCManager) HandleDCFailure(ctx context.Context, failedDC string) error {
	mdm.logger.WithField("dc", failedDC).Warn("Handling datacenter failure")

	// Find alternative DC
	alternateDC, err := mdm.findPrimaryDC()
	if err != nil {
		return err
	}

	mdm.metrics.dcFailovers.With(failedDC, alternateDC).Inc()

	mdm.logger.WithFields(map[string]interface{}{
		"failed_dc":    failedDC,
		"alternate_dc": alternateDC,
	}).Info("Datacenter failover complete")

	return nil
}

// GetStatus returns the current multi-DC status.
func (mdm *MultiDCManager) GetStatus() MultiDCStatus {
	mdm.datacentersMu.RLock()
	defer mdm.datacentersMu.RUnlock()

	mdm.replicationMu.RLock()
	replicationActive := mdm.replicationActive
	mdm.replicationMu.RUnlock()

	activeDCs := make([]string, 0)
	unhealthyDCs := make([]string, 0)
	replicationLag := make(map[string]time.Duration)

	for name, dc := range mdm.datacenters {
		if dc.Healthy {
			activeDCs = append(activeDCs, name)
		} else {
			unhealthyDCs = append(unhealthyDCs, name)
		}
		replicationLag[name] = dc.Latency
	}

	return MultiDCStatus{
		LocalDC:           mdm.config.LocalDC,
		ActiveDCs:         activeDCs,
		UnhealthyDCs:      unhealthyDCs,
		ReplicationActive: replicationActive,
		ReplicationLag:    replicationLag,
	}
}

// GetDatacenterInfo returns information about a specific datacenter.
func (mdm *MultiDCManager) GetDatacenterInfo(name string) (*Datacenter, error) {
	mdm.datacentersMu.RLock()
	defer mdm.datacentersMu.RUnlock()

	dc, exists := mdm.datacenters[name]
	if !exists {
		return nil, errors.Newf(errors.ErrNotFound, "ha.GetDatacenterInfo", "datacenter %s not found", name)
	}

	// Return a copy
	dcCopy := *dc
	return &dcCopy, nil
}
