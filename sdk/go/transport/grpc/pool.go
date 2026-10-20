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

package grpc

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// PoolConfig holds configuration for the connection pool.
type PoolConfig struct {
	// MinSize is the minimum number of connections to maintain.
	MinSize int

	// MaxSize is the maximum number of connections allowed.
	MaxSize int

	// MaxAge is the maximum age of a connection before it's recycled.
	MaxAge time.Duration

	// HealthInterval is the interval between health checks.
	HealthInterval time.Duration
}

// DefaultPoolConfig returns sensible default pool configuration.
func DefaultPoolConfig() *PoolConfig {
	return &PoolConfig{
		MinSize:        5,
		MaxSize:        50,
		MaxAge:         time.Hour,
		HealthInterval: 30 * time.Second,
	}
}

// pooledConn wraps a gRPC connection with metadata.
type pooledConn struct {
	conn      *grpc.ClientConn
	createdAt time.Time
	inUse     atomic.Bool
}

// Pool manages a pool of gRPC connections.
type Pool struct {
	address  string
	dialOpts []grpc.DialOption
	cfg      *PoolConfig

	mu          sync.Mutex
	connections []*pooledConn
	closed      atomic.Bool

	// Health check goroutine
	healthStop chan struct{}
	healthWg   sync.WaitGroup
}

// NewPool creates a new connection pool.
func NewPool(address string, dialOpts []grpc.DialOption, cfg *PoolConfig) (*Pool, error) {
	if cfg == nil {
		cfg = DefaultPoolConfig()
	}

	if cfg.MinSize <= 0 {
		return nil, errors.NewInvalidInput("NewPool", "MinSize must be positive")
	}
	if cfg.MaxSize < cfg.MinSize {
		return nil, errors.NewInvalidInput("NewPool", "MaxSize must be >= MinSize")
	}

	p := &Pool{
		address:     address,
		dialOpts:    dialOpts,
		cfg:         cfg,
		connections: make([]*pooledConn, 0, cfg.MaxSize),
		healthStop:  make(chan struct{}),
	}

	// Pre-create minimum connections
	for i := 0; i < cfg.MinSize; i++ {
		pc, err := p.createConnection()
		if err != nil {
			// Close any connections we created
			p.closeAll()
			return nil, errors.Wrap(err, errors.CodeConnection, "NewPool",
				"failed to create initial connections")
		}
		p.connections = append(p.connections, pc)
	}

	// Start health check goroutine
	if cfg.HealthInterval > 0 {
		p.healthWg.Add(1)
		go p.healthCheckLoop()
	}

	return p, nil
}

// createConnection creates a new pooled connection.
// Without grpc.WithBlock(), dial returns immediately and the actual TCP
// connection is established lazily on the first RPC call.
func (p *Pool) createConnection() (*pooledConn, error) {
	conn, err := grpc.DialContext(context.Background(), p.address, p.dialOpts...)
	if err != nil {
		return nil, err
	}

	return &pooledConn{
		conn:      conn,
		createdAt: time.Now(),
	}, nil
}

// Get retrieves a connection from the pool.
func (p *Pool) Get() (*grpc.ClientConn, error) {
	if p.closed.Load() {
		return nil, errors.NewConnection("Pool.Get", "pool is closed")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Find an available healthy connection
	for _, pc := range p.connections {
		if !pc.inUse.Load() && p.isHealthy(pc) && !p.isExpired(pc) {
			pc.inUse.Store(true)
			return pc.conn, nil
		}
	}

	// No available connection, create new one if under max
	if len(p.connections) < p.cfg.MaxSize {
		pc, err := p.createConnection()
		if err != nil {
			return nil, errors.Wrap(err, errors.CodeConnection, "Pool.Get",
				"failed to create new connection")
		}
		pc.inUse.Store(true)
		p.connections = append(p.connections, pc)
		return pc.conn, nil
	}

	// Pool is exhausted
	return nil, errors.NewResourceExhausted("Pool.Get", "connection pool exhausted")
}

// Put returns a connection to the pool.
func (p *Pool) Put(conn *grpc.ClientConn) {
	if p.closed.Load() {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pc := range p.connections {
		if pc.conn == conn {
			if decidePut(pc.conn.GetState(), p.isExpired(pc)) == putReplace {
				go p.replaceConnection(pc)
			} else {
				pc.inUse.Store(false)
			}
			return
		}
	}
}

// putDecision is what Put does with a connection handed back to the pool.
type putDecision int

const (
	// putRelease marks the connection free; it stays in the pool.
	putRelease putDecision = iota
	// putReplace closes the connection and dials a replacement.
	putReplace
)

// decidePut replaces a returned connection only when it can no longer be
// used: it is shut down or older than MaxAge. A connection passing through
// Connecting or TransientFailure is released: gRPC reconnects it on its own,
// Get skips it until it is Ready or Idle, and the health-check loop replaces
// it if it stays down.
func decidePut(state connectivity.State, expired bool) putDecision {
	if expired || state == connectivity.Shutdown {
		return putReplace
	}
	return putRelease
}

// replaceConnection replaces an unhealthy or expired connection.
func (p *Pool) replaceConnection(old *pooledConn) {
	// Close old connection
	old.conn.Close()

	// Create replacement
	newPC, err := p.createConnection()
	if err != nil {
		// Failed to create replacement, just remove from pool
		p.removeConnection(old)
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Find and replace
	for i, pc := range p.connections {
		if pc == old {
			p.connections[i] = newPC
			return
		}
	}

	// Old connection was already removed, add new one
	if len(p.connections) < p.cfg.MaxSize {
		p.connections = append(p.connections, newPC)
	} else {
		newPC.conn.Close()
	}
}

// removeConnection removes a connection from the pool.
func (p *Pool) removeConnection(target *pooledConn) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i, pc := range p.connections {
		if pc == target {
			p.connections = append(p.connections[:i], p.connections[i+1:]...)
			return
		}
	}
}

// isHealthy checks if a connection is healthy.
func (p *Pool) isHealthy(pc *pooledConn) bool {
	state := pc.conn.GetState()
	return state == connectivity.Ready || state == connectivity.Idle
}

// isExpired checks if a connection has exceeded max age.
func (p *Pool) isExpired(pc *pooledConn) bool {
	if p.cfg.MaxAge <= 0 {
		return false
	}
	return time.Since(pc.createdAt) > p.cfg.MaxAge
}

// healthCheckLoop runs periodic health checks.
func (p *Pool) healthCheckLoop() {
	defer p.healthWg.Done()

	ticker := time.NewTicker(p.cfg.HealthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.healthStop:
			return
		case <-ticker.C:
			p.performHealthCheck()
		}
	}
}

// performHealthCheck checks and maintains pool health.
func (p *Pool) performHealthCheck() {
	if p.closed.Load() {
		return
	}

	p.mu.Lock()
	var toReplace []*pooledConn

	// Identify unhealthy or expired connections
	for _, pc := range p.connections {
		if !pc.inUse.Load() && (p.isExpired(pc) || !p.isHealthy(pc)) {
			toReplace = append(toReplace, pc)
		}
	}
	p.mu.Unlock()

	// Replace unhealthy connections
	for _, pc := range toReplace {
		p.replaceConnection(pc)
	}

	// Ensure minimum connections
	p.ensureMinConnections()
}

// ensureMinConnections ensures the pool has minimum connections.
func (p *Pool) ensureMinConnections() {
	p.mu.Lock()
	needed := p.cfg.MinSize - len(p.connections)
	p.mu.Unlock()

	for i := 0; i < needed; i++ {
		pc, err := p.createConnection()
		if err != nil {
			continue // Best effort
		}

		p.mu.Lock()
		if len(p.connections) < p.cfg.MaxSize {
			p.connections = append(p.connections, pc)
		} else {
			pc.conn.Close()
		}
		p.mu.Unlock()
	}
}

// Healthy returns true if the pool has healthy connections.
func (p *Pool) Healthy() bool {
	if p.closed.Load() {
		return false
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pc := range p.connections {
		if p.isHealthy(pc) {
			return true
		}
	}

	return false
}

// Size returns the current number of connections in the pool.
func (p *Pool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.connections)
}

// Close closes the pool and all connections.
func (p *Pool) Close() error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}

	// Stop health check
	close(p.healthStop)
	p.healthWg.Wait()

	p.closeAll()
	return nil
}

// closeAll closes all connections.
func (p *Pool) closeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pc := range p.connections {
		pc.conn.Close()
	}
	p.connections = nil
}
