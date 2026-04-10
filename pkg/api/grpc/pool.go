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

// Package grpc provides gRPC client and server implementations for QuicBaaS.
package grpc

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"

	"github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// poolMetrics holds Prometheus metrics for the connection pool.
type poolMetrics struct {
	poolSize           *metrics.Gauge     // Current pool size
	activeConns        *metrics.Gauge     // Connections in use
	idleConns          *metrics.Gauge     // Connections available
	totalRequests      *metrics.Counter   // Total requests served
	connectionReuse    *metrics.Counter   // Reused connections
	connectionCreated  *metrics.Counter   // New connections created
	connectionRecycled *metrics.Counter   // Recycled connections
	healthChecksFailed *metrics.Counter   // Failed health checks
	getLatency         *metrics.Histogram // Time to get connection from pool
}

// newPoolMetrics creates and registers connection pool metrics.
func newPoolMetrics(address string) *poolMetrics {
	return &poolMetrics{
		poolSize: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "pool",
			Name:      "size",
			Help:      "Current connection pool size",
			ConstLabels: map[string]string{
				"address": address,
			},
		}),
		activeConns: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "pool",
			Name:      "active_connections",
			Help:      "Number of active connections in use",
			ConstLabels: map[string]string{
				"address": address,
			},
		}),
		idleConns: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "pool",
			Name:      "idle_connections",
			Help:      "Number of idle connections available",
			ConstLabels: map[string]string{
				"address": address,
			},
		}),
		totalRequests: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "pool",
			Name:      "requests_total",
			Help:      "Total number of requests served by pool",
			ConstLabels: map[string]string{
				"address": address,
			},
		}),
		connectionReuse: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "pool",
			Name:      "connection_reuse_total",
			Help:      "Total number of connection reuses",
			ConstLabels: map[string]string{
				"address": address,
			},
		}),
		connectionCreated: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "pool",
			Name:      "connection_created_total",
			Help:      "Total number of new connections created",
			ConstLabels: map[string]string{
				"address": address,
			},
		}),
		connectionRecycled: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "pool",
			Name:      "connection_recycled_total",
			Help:      "Total number of connections recycled",
			ConstLabels: map[string]string{
				"address": address,
			},
		}),
		healthChecksFailed: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "pool",
			Name:      "health_checks_failed_total",
			Help:      "Total number of failed health checks",
			ConstLabels: map[string]string{
				"address": address,
			},
		}),
		getLatency: metrics.NewHistogram(metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "pool",
			Name:      "get_duration_seconds",
			Help:      "Time to acquire connection from pool",
			Buckets:   []float64{.00001, .00005, .0001, .0005, .001, .005, .01, .05, .1},
			ConstLabels: map[string]string{
				"address": address,
			},
		}),
	}
}

// PoolStats provides statistics about the connection pool.
// These metrics are useful for monitoring pool health and performance.
type PoolStats struct {
	// PoolSize is the current number of connections in the pool
	PoolSize int

	// ActiveConns is the number of connections currently in use
	ActiveConns int

	// IdleConns is the number of idle connections ready for use
	IdleConns int

	// TotalRequests is the cumulative number of requests served
	TotalRequests uint64

	// RecycledConns is the number of connections recycled due to age or request limits
	RecycledConns uint64

	// FailedConns is the number of failed connection attempts
	FailedConns uint64

	// HealthChecksFailed is the number of failed health checks
	HealthChecksFailed uint64
}

// pooledConn represents a single connection in the pool with metadata.
// Each connection tracks its age, usage count, and health status.
type pooledConn struct {
	// conn is the underlying gRPC connection
	conn *grpc.ClientConn

	// client is the QuicBaaS gRPC client stub
	client proto.QuicBaaSClient

	// created is when this connection was established
	created time.Time

	// requests counts the number of requests served by this connection
	requests atomic.Uint64

	// healthy indicates if the connection passed the last health check
	healthy atomic.Bool

	// inUse indicates if the connection is currently being used
	inUse atomic.Bool

	// mu protects connection state during recycling
	mu sync.Mutex
}

// ConnectionPool manages a pool of gRPC connections for high-performance operation.
//
// The pool provides:
//   - Connection reuse to amortize connection establishment overhead
//   - Round-robin load balancing across connections
//   - Automatic health checking and connection recycling
//   - Configurable pool size and connection lifecycle management
//   - Lock-free read path for minimal contention
//   - Comprehensive Prometheus metrics
//
// Performance characteristics:
//   - Get() is lock-free using atomic operations for maximum throughput
//   - Health checks run in background goroutine
//   - Connection recycling is lazy (on-demand during Get())
//   - Graceful shutdown ensures all connections are properly closed
type ConnectionPool struct {
	// conns holds all pooled connections
	conns []*pooledConn

	// current is the round-robin counter (atomic for lock-free reads)
	current atomic.Uint64

	// mu protects pool modifications (resize, close)
	mu sync.RWMutex

	// Configuration
	address     string
	opts        *clientOptions
	minSize     int
	maxSize     int
	maxAge      time.Duration
	maxRequests uint64
	healthCheck time.Duration

	// State tracking
	closed          atomic.Bool
	wg              sync.WaitGroup
	stopHealthCheck chan struct{}

	// Statistics
	totalRequests      atomic.Uint64
	recycledConns      atomic.Uint64
	failedConns        atomic.Uint64
	healthChecksFailed atomic.Uint64

	// Metrics
	metrics *poolMetrics

	// Logger
	logger *logger.Logger
}

// poolOptions holds configuration for the connection pool.
type poolOptions struct {
	minSize     int
	maxSize     int
	maxAge      time.Duration
	maxRequests uint64
	healthCheck time.Duration
	warmupConns bool
	clientOpts  *clientOptions
}

// defaultPoolOptions returns sensible defaults for the connection pool.
func defaultPoolOptions() *poolOptions {
	return &poolOptions{
		minSize:     5,                // Minimum 5 connections for good concurrency
		maxSize:     20,               // Maximum 20 to limit resource usage
		maxAge:      30 * time.Minute, // Recycle connections after 30 minutes
		maxRequests: 100000,           // Recycle after 100K requests to prevent resource leaks
		healthCheck: 30 * time.Second, // Check health every 30 seconds
		warmupConns: true,             // Pre-create minimum connections on startup
		clientOpts:  defaultClientOptions(),
	}
}

// PoolOption configures the connection pool.
type PoolOption func(*poolOptions) error

// WithPoolSize sets the minimum and maximum pool size.
// minSize determines how many connections are pre-created (if warmup enabled).
// maxSize is the upper bound for pool growth (not currently used for dynamic sizing).
func WithPoolSize(minSize, maxSize int) PoolOption {
	return func(opts *poolOptions) error {
		if minSize < 1 {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithPoolSize",
				"minSize must be at least 1, got: %d", minSize,
			)
		}
		if maxSize < minSize {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithPoolSize",
				"maxSize must be >= minSize, got: minSize=%d maxSize=%d", minSize, maxSize,
			)
		}
		opts.minSize = minSize
		opts.maxSize = maxSize
		return nil
	}
}

// WithMaxConnectionAge sets the maximum age for a connection before recycling.
// Connections older than this duration will be recycled on next use.
func WithMaxConnectionAge(duration time.Duration) PoolOption {
	return func(opts *poolOptions) error {
		if duration <= 0 {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithMaxConnectionAge",
				"duration must be positive, got: %v", duration,
			)
		}
		opts.maxAge = duration
		return nil
	}
}

// WithMaxRequestsPerConn sets the maximum number of requests per connection.
// After serving this many requests, the connection will be recycled.
func WithMaxRequestsPerConn(count uint64) PoolOption {
	return func(opts *poolOptions) error {
		if count == 0 {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithMaxRequestsPerConn",
				"count must be positive, got: %d", count,
			)
		}
		opts.maxRequests = count
		return nil
	}
}

// WithHealthCheckInterval sets the interval between health checks.
// Set to 0 to disable periodic health checks (not recommended for production).
func WithHealthCheckInterval(duration time.Duration) PoolOption {
	return func(opts *poolOptions) error {
		if duration < 0 {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithHealthCheckInterval",
				"duration cannot be negative, got: %v", duration,
			)
		}
		opts.healthCheck = duration
		return nil
	}
}

// WithConnectionWarmup enables or disables pre-creating connections on pool startup.
func WithConnectionWarmup(enabled bool) PoolOption {
	return func(opts *poolOptions) error {
		opts.warmupConns = enabled
		return nil
	}
}

// WithPoolClientOptions sets client options for pool connections.
// This allows configuring TLS, timeouts, retries, etc.
func WithPoolClientOptions(clientOpts *clientOptions) PoolOption {
	return func(opts *poolOptions) error {
		if clientOpts == nil {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithPoolClientOptions",
				"clientOpts cannot be nil",
			)
		}
		opts.clientOpts = clientOpts
		return nil
	}
}

// NewConnectionPool creates a new gRPC connection pool.
//
// The pool pre-creates minSize connections (if warmup is enabled) and maintains
// them with periodic health checks. Connections are automatically recycled when
// they exceed maxAge or maxRequests.
//
// Example usage:
//
//	pool, err := NewConnectionPool("localhost:50051",
//	    WithPoolSize(10, 50),
//	    WithMaxConnectionAge(5*time.Minute),
//	    WithMaxRequestsPerConn(10000),
//	)
//	if err != nil {
//	    return err
//	}
//	defer pool.Close()
//
//	conn, err := pool.Get()
//	if err != nil {
//	    return err
//	}
//	// Use conn.client for gRPC calls
func NewConnectionPool(address string, opts ...PoolOption) (*ConnectionPool, error) {
	// Apply default options
	poolOpts := defaultPoolOptions()
	for _, opt := range opts {
		if err := opt(poolOpts); err != nil {
			return nil, err
		}
	}

	// Create logger if not provided
	if poolOpts.clientOpts.logger == nil {
		logConfig := logger.LogConfig{
			Level:  "info",
			Format: "json",
			Output: "stdout",
		}
		log, err := logger.New(logConfig)
		if err != nil {
			return nil, errors.Wrapf(
				err,
				errors.ErrConfiguration,
				"grpc.NewConnectionPool",
				"failed to create logger",
			)
		}
		poolOpts.clientOpts.logger = log
	}

	pool := &ConnectionPool{
		address:         address,
		opts:            poolOpts.clientOpts,
		minSize:         poolOpts.minSize,
		maxSize:         poolOpts.maxSize,
		maxAge:          poolOpts.maxAge,
		maxRequests:     poolOpts.maxRequests,
		healthCheck:     poolOpts.healthCheck,
		stopHealthCheck: make(chan struct{}),
		logger:          poolOpts.clientOpts.logger,
		metrics:         newPoolMetrics(address),
	}

	// Pre-create connections if warmup is enabled
	if poolOpts.warmupConns {
		pool.conns = make([]*pooledConn, poolOpts.minSize)
		for i := 0; i < poolOpts.minSize; i++ {
			conn, err := pool.createConnection(context.Background())
			if err != nil {
				// Close any connections created so far
				_ = pool.Close()
				return nil, errors.Wrapf(
					err,
					errors.ErrUnavailable,
					"grpc.NewConnectionPool",
					"failed to create initial connection %d/%d", i+1, poolOpts.minSize,
				)
			}
			pool.conns[i] = conn
		}
	} else {
		// Initialize empty pool
		pool.conns = make([]*pooledConn, 0, poolOpts.minSize)
	}

	// Initialize metrics
	pool.updateMetrics()

	// Start health check goroutine if enabled
	if pool.healthCheck > 0 {
		pool.wg.Add(1)
		go pool.healthCheckLoop()
	}

	pool.logger.WithFields(map[string]interface{}{
		"address":      address,
		"min_size":     pool.minSize,
		"max_size":     pool.maxSize,
		"max_age":      pool.maxAge,
		"max_requests": pool.maxRequests,
		"health_check": pool.healthCheck,
		"warmup_conns": poolOpts.warmupConns,
	}).Info("connection pool created successfully")

	return pool, nil
}

// createConnection establishes a new gRPC connection.
func (p *ConnectionPool) createConnection(ctx context.Context) (*pooledConn, error) {
	// Build dial options
	dialOpts, err := p.opts.buildDialOptions()
	if err != nil {
		return nil, errors.Wrapf(
			err,
			errors.ErrConfiguration,
			"grpc.ConnectionPool.createConnection",
			"failed to build dial options",
		)
	}

	// Create context with timeout
	dialCtx, cancel := context.WithTimeout(ctx, p.opts.timeout)
	defer cancel()

	// Establish connection
	conn, err := grpc.DialContext(dialCtx, p.address, dialOpts...)
	if err != nil {
		p.failedConns.Add(1)
		return nil, errors.Wrapf(
			err,
			errors.ErrUnavailable,
			"grpc.ConnectionPool.createConnection",
			"failed to dial %s", p.address,
		)
	}

	// Create pooled connection wrapper
	pc := &pooledConn{
		conn:    conn,
		client:  proto.NewQuicBaaSClient(conn),
		created: time.Now(),
	}
	pc.healthy.Store(true)
	pc.inUse.Store(false)

	// Track metrics
	p.metrics.connectionCreated.Inc()

	return pc, nil
}

// Get returns a connection from the pool using round-robin selection.
//
// This is a lock-free operation in the common case (no recycling needed).
// The returned connection is ready to use for gRPC calls via conn.client.
//
// Connections are automatically recycled if they:
//   - Exceed maxAge
//   - Exceed maxRequests
//   - Are marked unhealthy by health checks
//
// If a connection needs recycling, it's replaced synchronously before returning.
func (p *ConnectionPool) Get() (*pooledConn, error) {
	start := time.Now()
	defer func() {
		p.metrics.getLatency.Observe(time.Since(start).Seconds())
	}()

	if p.closed.Load() {
		return nil, errors.Newf(
			errors.ErrInvalidInput,
			"grpc.ConnectionPool.Get",
			"pool is closed",
		)
	}

	p.mu.RLock()
	if len(p.conns) == 0 {
		p.mu.RUnlock()
		return nil, errors.Newf(
			errors.ErrInternal,
			"grpc.ConnectionPool.Get",
			"pool has no connections",
		)
	}

	// Lock-free round-robin selection
	idx := p.current.Add(1) % uint64(len(p.conns))
	conn := p.conns[idx]
	p.mu.RUnlock()

	// Check if connection needs recycling
	if p.needsRecycling(conn) {
		// Recycle synchronously (rare path, so lock is acceptable)
		newConn, err := p.recycleConnection(conn)
		if err != nil {
			return nil, errors.Wrapf(
				err,
				errors.ErrUnavailable,
				"grpc.ConnectionPool.Get",
				"failed to recycle connection",
			)
		}
		conn = newConn
	} else {
		// Track connection reuse (only if not recycled)
		p.metrics.connectionReuse.Inc()
	}

	// Mark as in use
	conn.inUse.Store(true)
	conn.requests.Add(1)
	p.totalRequests.Add(1)

	// Update metrics
	p.metrics.totalRequests.Inc()
	p.updateMetrics()

	return conn, nil
}

// needsRecycling checks if a connection should be recycled.
func (p *ConnectionPool) needsRecycling(conn *pooledConn) bool {
	// Check health status
	if !conn.healthy.Load() {
		return true
	}

	// Check age
	if time.Since(conn.created) > p.maxAge {
		return true
	}

	// Check request count
	if conn.requests.Load() >= p.maxRequests {
		return true
	}

	// Check gRPC connection state
	state := conn.conn.GetState()
	if state == connectivity.TransientFailure || state == connectivity.Shutdown {
		return true
	}

	return false
}

// recycleConnection replaces an old connection with a new one.
func (p *ConnectionPool) recycleConnection(oldConn *pooledConn) (*pooledConn, error) {
	oldConn.mu.Lock()
	defer oldConn.mu.Unlock()

	// Double-check if still needs recycling (another goroutine might have done it)
	if !p.needsRecycling(oldConn) {
		return oldConn, nil
	}

	// Create new connection
	newConn, err := p.createConnection(context.Background())
	if err != nil {
		return nil, err
	}

	// Replace in pool
	p.mu.Lock()
	for i, conn := range p.conns {
		if conn == oldConn {
			p.conns[i] = newConn
			break
		}
	}
	p.mu.Unlock()

	// Close old connection
	if err := oldConn.conn.Close(); err != nil {
		p.logger.WithFields(map[string]interface{}{
			"error":   err.Error(),
			"created": oldConn.created,
		}).Warn("failed to close old connection during recycling")
	}

	p.recycledConns.Add(1)
	p.metrics.connectionRecycled.Inc()

	p.logger.WithFields(map[string]interface{}{
		"old_created":    oldConn.created,
		"old_requests":   oldConn.requests.Load(),
		"new_created":    newConn.created,
		"recycled_total": p.recycledConns.Load(),
	}).Debug("connection recycled")

	return newConn, nil
}

// checkHealth performs a health check on a connection.
func (p *ConnectionPool) checkHealth(conn *pooledConn) bool {
	// Check gRPC connectivity state
	state := conn.conn.GetState()
	if state == connectivity.TransientFailure || state == connectivity.Shutdown {
		return false
	}

	// Optionally perform a Health RPC call
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &proto.HealthRequest{
		IncludeDetails: false,
	}

	resp, err := conn.client.Health(ctx, req)
	if err != nil {
		return false
	}

	return resp.Status == proto.HealthStatus_HEALTH_STATUS_HEALTHY
}

// healthCheckLoop periodically checks connection health.
func (p *ConnectionPool) healthCheckLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.healthCheck)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.performHealthChecks()
		case <-p.stopHealthCheck:
			return
		}
	}
}

// performHealthChecks checks all connections in the pool.
func (p *ConnectionPool) performHealthChecks() {
	p.mu.RLock()
	conns := make([]*pooledConn, len(p.conns))
	copy(conns, p.conns)
	p.mu.RUnlock()

	for _, conn := range conns {
		// Skip connections currently in use
		if conn.inUse.Load() {
			continue
		}

		healthy := p.checkHealth(conn)
		wasHealthy := conn.healthy.Swap(healthy)

		if !healthy {
			p.healthChecksFailed.Add(1)
			p.metrics.healthChecksFailed.Inc()

			if wasHealthy {
				p.logger.WithFields(map[string]interface{}{
					"created":  conn.created,
					"requests": conn.requests.Load(),
				}).Warn("connection marked unhealthy during health check")
			}
		} else if !wasHealthy {
			// Connection recovered
			p.logger.WithFields(map[string]interface{}{
				"created":  conn.created,
				"requests": conn.requests.Load(),
			}).Info("connection marked healthy after recovery")
		}
	}

	// Update pool metrics after health check
	p.updateMetrics()
}

// updateMetrics updates gauge metrics to reflect current pool state.
func (p *ConnectionPool) updateMetrics() {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var active, idle int
	for _, conn := range p.conns {
		if conn.inUse.Load() {
			active++
		} else {
			idle++
		}
	}

	p.metrics.poolSize.Set(float64(len(p.conns)))
	p.metrics.activeConns.Set(float64(active))
	p.metrics.idleConns.Set(float64(idle))
}

// Stats returns current pool statistics.
func (p *ConnectionPool) Stats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := PoolStats{
		PoolSize:           len(p.conns),
		TotalRequests:      p.totalRequests.Load(),
		RecycledConns:      p.recycledConns.Load(),
		FailedConns:        p.failedConns.Load(),
		HealthChecksFailed: p.healthChecksFailed.Load(),
	}

	// Count active/idle connections
	for _, conn := range p.conns {
		if conn.inUse.Load() {
			stats.ActiveConns++
		} else {
			stats.IdleConns++
		}
	}

	return stats
}

// Close closes all connections in the pool and stops the health check goroutine.
//
// This should be called when the pool is no longer needed to release resources.
// Once closed, the pool cannot be reused.
func (p *ConnectionPool) Close() error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil // Already closed
	}

	// Stop health check goroutine
	if p.stopHealthCheck != nil {
		close(p.stopHealthCheck)
	}
	p.wg.Wait()

	// Close all connections
	p.mu.Lock()
	defer p.mu.Unlock()

	var lastErr error
	for i, conn := range p.conns {
		if conn != nil && conn.conn != nil {
			if err := conn.conn.Close(); err != nil {
				lastErr = err
				if p.logger != nil {
					p.logger.WithFields(map[string]interface{}{
						"error": err.Error(),
						"index": i,
					}).Warn("failed to close connection during pool shutdown")
				}
			}
		}
	}

	// Final metrics update
	if p.metrics != nil {
		p.metrics.poolSize.Set(0)
		p.metrics.activeConns.Set(0)
		p.metrics.idleConns.Set(0)
	}

	if p.logger != nil {
		p.logger.WithFields(map[string]interface{}{
			"total_requests":  p.totalRequests.Load(),
			"recycled_conns":  p.recycledConns.Load(),
			"failed_conns":    p.failedConns.Load(),
			"health_failures": p.healthChecksFailed.Load(),
		}).Info("connection pool closed")
	}

	if lastErr != nil {
		return errors.Wrapf(
			lastErr,
			errors.ErrInternal,
			"grpc.ConnectionPool.Close",
			"failed to close one or more connections",
		)
	}

	return nil
}

// String returns a string representation of the pool for debugging.
func (p *ConnectionPool) String() string {
	stats := p.Stats()
	return fmt.Sprintf(
		"ConnectionPool{address=%s, size=%d, active=%d, idle=%d, requests=%d, recycled=%d}",
		p.address,
		stats.PoolSize,
		stats.ActiveConns,
		stats.IdleConns,
		stats.TotalRequests,
		stats.RecycledConns,
	)
}
