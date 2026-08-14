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

package iam

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Compile-time interface checks.
var (
	_ StoragePool       = (*storagePool)(nil)
	_ StorageConnection = (*storageConnection)(nil)
)

// storagePool implements the StoragePool interface using channels for
// lock-free connection acquisition and a background goroutine for maintenance.
type storagePool struct {
	config PoolConfig

	// Pool state
	conns      chan *storageConnection // Channel-based pool for lock-free Get
	allConns   map[string]*storageConnection
	allConnsMu sync.RWMutex

	// Lifecycle management
	closed     atomic.Bool
	closedChan chan struct{}
	wg         sync.WaitGroup

	// Statistics (atomic for lock-free access)
	waitCount          atomic.Int64
	waitDuration       atomic.Int64 // nanoseconds
	maxIdleTimeClosed  atomic.Int64
	maxLifetimeClosed  atomic.Int64
	maxUseCountClosed  atomic.Int64
	healthChecksClosed atomic.Int64
	totalCreated       atomic.Int64
	totalClosed        atomic.Int64
	inUseCount         atomic.Int32
}

// storageConnection implements the StorageConnection interface.
type storageConnection struct {
	id         string
	store      interfaces.KVStore
	createdAt  time.Time
	lastUsedAt atomic.Pointer[time.Time]
	useCount   atomic.Uint64
	healthy    atomic.Bool
	closed     atomic.Bool
	pool       *storagePool // Reference back to pool for metrics
}

// NewStoragePool creates a new storage pool with the given options.
func NewStoragePool(ctx context.Context, opts ...PoolOption) (StoragePool, error) {
	config := DefaultPoolConfig()

	for _, opt := range opts {
		if err := opt(&config); err != nil {
			return nil, err
		}
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	pool := &storagePool{
		config:     config,
		conns:      make(chan *storageConnection, config.MaxConns),
		allConns:   make(map[string]*storageConnection),
		closedChan: make(chan struct{}),
	}

	// Pre-create minimum connections
	for i := 0; i < config.MinConns; i++ {
		conn, err := pool.createConnection(ctx)
		if err != nil {
			// Clean up any connections we created using non-blocking drain
			pool.closed.Store(true)
			close(pool.closedChan)
			pool.drainAndCloseConnections()
			return nil, &dberrors.QBError{
				Code: dberrors.ErrInternal,
				Op:   "iam.storage.pool.new",
				Err:  &ConnectionCreationError{Index: i + 1, Total: config.MinConns, Cause: err},
			}
		}
		pool.conns <- conn
	}

	// Start health check goroutine if configured
	if config.HealthCheckFreq > 0 {
		pool.wg.Add(1)
		go pool.healthCheckLoop()
	}

	// Start maintenance goroutine for idle connection cleanup
	pool.wg.Add(1)
	go pool.maintenanceLoop()

	return pool, nil
}

// NewStoragePoolWithConfig creates a new storage pool with an explicit configuration.
func NewStoragePoolWithConfig(ctx context.Context, config PoolConfig) (StoragePool, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	pool := &storagePool{
		config:     config,
		conns:      make(chan *storageConnection, config.MaxConns),
		allConns:   make(map[string]*storageConnection),
		closedChan: make(chan struct{}),
	}

	// Pre-create minimum connections
	for i := 0; i < config.MinConns; i++ {
		conn, err := pool.createConnection(ctx)
		if err != nil {
			// Clean up any connections we created using non-blocking drain
			pool.closed.Store(true)
			close(pool.closedChan)
			pool.drainAndCloseConnections()
			return nil, &dberrors.QBError{
				Code: dberrors.ErrInternal,
				Op:   "iam.storage.pool.new",
				Err:  &ConnectionCreationError{Index: i + 1, Total: config.MinConns, Cause: err},
			}
		}
		pool.conns <- conn
	}

	// Start health check goroutine if configured
	if config.HealthCheckFreq > 0 {
		pool.wg.Add(1)
		go pool.healthCheckLoop()
	}

	// Start maintenance goroutine for idle connection cleanup
	pool.wg.Add(1)
	go pool.maintenanceLoop()

	return pool, nil
}

// drainAndCloseConnections drains connections from the channel and closes them.
// This uses non-blocking receives to avoid hanging.
func (p *storagePool) drainAndCloseConnections() {
	for {
		select {
		case conn := <-p.conns:
			if conn != nil {
				_ = conn.Close()
			}
		default:
			return
		}
	}
}

// Get retrieves a storage connection from the pool.
func (p *storagePool) Get(ctx context.Context) (StorageConnection, error) {
	if p.closed.Load() {
		return nil, ErrPoolClosed
	}

	startWait := time.Now()

	// Calculate deadline for acquire timeout
	var deadline time.Time
	if p.config.AcquireTimeout > 0 {
		deadline = startWait.Add(p.config.AcquireTimeout)
	}

	for {
		select {
		case conn := <-p.conns:
			// Got a connection from the pool
			waitDuration := time.Since(startWait)
			if waitDuration > time.Millisecond {
				p.waitCount.Add(1)
				p.waitDuration.Add(int64(waitDuration))
			}

			// Check if connection needs recycling
			if p.needsRecycling(conn) {
				p.closeConnection(conn, "recycling")
				// Try to create a new connection
				newConn, err := p.createConnection(ctx)
				if err != nil {
					// Put the slot back for another attempt
					continue
				}
				conn = newConn
			}

			// Verify health if checker is configured
			if p.config.HealthChecker != nil && !conn.healthy.Load() {
				p.closeConnection(conn, "unhealthy")
				p.healthChecksClosed.Add(1)
				continue
			}

			conn.MarkUsed()
			p.inUseCount.Add(1)
			return conn, nil

		case <-ctx.Done():
			// Context cancelled
			return nil, &dberrors.QBError{
				Code: dberrors.ErrCanceled,
				Op:   "iam.storage.pool.get",
				Err:  ctx.Err(),
			}

		case <-p.closedChan:
			return nil, ErrPoolClosed

		default:
			// No connection available immediately
			// Check if we can create a new one
			p.allConnsMu.RLock()
			totalConns := len(p.allConns)
			p.allConnsMu.RUnlock()

			if totalConns < p.config.MaxConns {
				conn, err := p.createConnection(ctx)
				if err == nil {
					conn.MarkUsed()
					p.inUseCount.Add(1)
					return conn, nil
				}
				// Failed to create, fall through to wait
			}

			// Wait for a connection or timeout
			if p.config.AcquireTimeout > 0 && time.Now().After(deadline) {
				return nil, ErrPoolExhausted
			}

			// Brief sleep before retry to avoid busy loop
			select {
			case <-time.After(10 * time.Millisecond):
			case <-ctx.Done():
				return nil, &dberrors.QBError{
					Code: dberrors.ErrCanceled,
					Op:   "iam.storage.pool.get",
					Err:  ctx.Err(),
				}
			case <-p.closedChan:
				return nil, ErrPoolClosed
			}
		}
	}
}

// Put returns a storage connection to the pool.
func (p *storagePool) Put(conn StorageConnection) error {
	if p.closed.Load() {
		// Pool is closed, just close the connection
		return conn.Close()
	}

	impl, ok := conn.(*storageConnection)
	if !ok {
		return &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "iam.storage.pool.put",
			Err:  &InvalidConnectionTypeError{},
		}
	}

	if impl.closed.Load() {
		return ErrConnectionClosed
	}

	p.inUseCount.Add(-1)

	// Check if connection needs recycling
	if p.needsRecycling(impl) {
		p.closeConnection(impl, "recycling")
		return nil
	}

	// Try to return to pool
	select {
	case p.conns <- impl:
		return nil
	default:
		// Pool is full, close the connection
		p.closeConnection(impl, "pool full")
		return nil
	}
}

// Close closes all connections in the pool.
func (p *storagePool) Close() error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil // Already closed
	}

	close(p.closedChan)

	// Wait for background goroutines to finish
	p.wg.Wait()

	// Close all connections in the channel
	close(p.conns)
	for conn := range p.conns {
		p.closeConnection(conn, "pool closing")
	}

	// Close any remaining tracked connections
	p.allConnsMu.Lock()
	for _, conn := range p.allConns {
		if !conn.closed.Load() {
			if closer, ok := conn.store.(interface{ Close() error }); ok {
				_ = closer.Close()
			}
			conn.closed.Store(true)
			p.totalClosed.Add(1)
		}
	}
	p.allConns = nil
	p.allConnsMu.Unlock()

	return nil
}

// Stats returns current pool statistics.
func (p *storagePool) Stats() PoolStats {
	p.allConnsMu.RLock()
	totalConns := len(p.allConns)
	p.allConnsMu.RUnlock()

	inUse := int(p.inUseCount.Load())
	if inUse < 0 {
		inUse = 0
	}

	return PoolStats{
		TotalConns:         totalConns,
		IdleConns:          len(p.conns),
		InUseConns:         inUse,
		WaitCount:          p.waitCount.Load(),
		WaitDuration:       time.Duration(p.waitDuration.Load()),
		MaxIdleTimeClosed:  p.maxIdleTimeClosed.Load(),
		MaxLifetimeClosed:  p.maxLifetimeClosed.Load(),
		MaxUseCountClosed:  p.maxUseCountClosed.Load(),
		HealthChecksClosed: p.healthChecksClosed.Load(),
		TotalCreated:       p.totalCreated.Load(),
		TotalClosed:        p.totalClosed.Load(),
	}
}

// createConnection creates a new storage connection.
func (p *storagePool) createConnection(ctx context.Context) (*storageConnection, error) {
	store, err := p.config.Factory(ctx)
	if err != nil {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "iam.storage.pool.create",
			Err:  &FactoryFailedError{Cause: err},
		}
	}

	// Wrap store to make it closable if not already
	closableStore := wrapClosable(store)

	now := time.Now()
	conn := &storageConnection{
		id:        uuid.New().String(),
		store:     closableStore,
		createdAt: now,
		pool:      p,
	}
	conn.lastUsedAt.Store(&now)
	conn.healthy.Store(true)

	p.allConnsMu.Lock()
	p.allConns[conn.id] = conn
	p.allConnsMu.Unlock()

	p.totalCreated.Add(1)

	return conn, nil
}

// closeConnection closes a connection and updates statistics.
// The alreadyClosed parameter indicates if closed flag was already set by the caller.
func (p *storagePool) closeConnection(conn *storageConnection, reason string) {
	// Try to set closed flag, but proceed with cleanup even if already set
	wasOpen := conn.closed.CompareAndSwap(false, true)

	// Only close the store if we're the one who set the flag
	if wasOpen {
		if closer, ok := conn.store.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}

	// Always remove from tracking and update stats (idempotent via map delete)
	p.allConnsMu.Lock()
	_, tracked := p.allConns[conn.id]
	delete(p.allConns, conn.id)
	p.allConnsMu.Unlock()

	// Only increment totalClosed if this connection was actually tracked
	if tracked {
		p.totalClosed.Add(1)

		switch reason {
		case "max idle time":
			p.maxIdleTimeClosed.Add(1)
		case "max lifetime":
			p.maxLifetimeClosed.Add(1)
		case "max use count":
			p.maxUseCountClosed.Add(1)
		case "unhealthy":
			p.healthChecksClosed.Add(1)
		}
	}
}

// needsRecycling checks if a connection needs to be recycled.
func (p *storagePool) needsRecycling(conn *storageConnection) bool {
	now := time.Now()

	// Check max lifetime
	if p.config.MaxLifetime > 0 && now.Sub(conn.createdAt) > p.config.MaxLifetime {
		return true
	}

	// Check max use count
	if p.config.MaxUseCount > 0 && conn.useCount.Load() >= p.config.MaxUseCount {
		return true
	}

	// Check health
	if !conn.healthy.Load() {
		return true
	}

	return false
}

// healthCheckLoop periodically checks connection health.
func (p *storagePool) healthCheckLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.HealthCheckFreq)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.performHealthChecks()
		case <-p.closedChan:
			return
		}
	}
}

// performHealthChecks checks the health of all connections.
func (p *storagePool) performHealthChecks() {
	if p.config.HealthChecker == nil {
		return
	}

	p.allConnsMu.RLock()
	conns := make([]*storageConnection, 0, len(p.allConns))
	for _, conn := range p.allConns {
		conns = append(conns, conn)
	}
	p.allConnsMu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, conn := range conns {
		if conn.closed.Load() {
			continue
		}

		err := p.config.HealthChecker(ctx, conn.store)
		if err != nil {
			conn.healthy.Store(false)
		} else {
			conn.healthy.Store(true)
		}
	}
}

// maintenanceLoop periodically cleans up idle connections.
func (p *storagePool) maintenanceLoop() {
	defer p.wg.Done()

	// Run maintenance every minute
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.performMaintenance()
		case <-p.closedChan:
			return
		}
	}
}

// performMaintenance cleans up idle connections and maintains min pool size.
func (p *storagePool) performMaintenance() {
	now := time.Now()

	// Collect connections that need cleanup
	var toClose []*storageConnection

	// Drain and re-add connections, filtering out expired ones
	n := len(p.conns)
drain:
	for i := 0; i < n; i++ {
		select {
		case conn := <-p.conns:
			// Check if connection is too old or idle
			lastUsed := conn.lastUsedAt.Load()
			if lastUsed != nil && p.config.MaxIdleTime > 0 && now.Sub(*lastUsed) > p.config.MaxIdleTime {
				toClose = append(toClose, conn)
				continue
			}
			if p.config.MaxLifetime > 0 && now.Sub(conn.createdAt) > p.config.MaxLifetime {
				toClose = append(toClose, conn)
				continue
			}
			// Put back in pool
			select {
			case p.conns <- conn:
			default:
				toClose = append(toClose, conn)
			}
		default:
			// No more connections to check
			break drain
		}
	}

	// Close expired connections
	for _, conn := range toClose {
		p.closeConnection(conn, "max idle time")
	}

	// Ensure minimum connections
	p.allConnsMu.RLock()
	currentConns := len(p.allConns)
	p.allConnsMu.RUnlock()

	if currentConns < p.config.MinConns {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

	refill:
		for i := currentConns; i < p.config.MinConns; i++ {
			conn, err := p.createConnection(ctx)
			if err != nil {
				break
			}
			select {
			case p.conns <- conn:
			default:
				p.closeConnection(conn, "pool full")
				break refill
			}
		}
	}
}

// closableKVStore wraps a KVStore with a Close method.
type closableKVStore struct {
	interfaces.KVStore
	closeFunc func() error
}

// Close closes the underlying store.
func (c *closableKVStore) Close() error {
	if c.closeFunc != nil {
		return c.closeFunc()
	}
	// Check if underlying store has Close method
	if closer, ok := c.KVStore.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// wrapClosable wraps a KVStore to ensure it has a Close method.
func wrapClosable(store interfaces.KVStore) *closableKVStore {
	if c, ok := store.(*closableKVStore); ok {
		return c
	}
	return &closableKVStore{KVStore: store}
}

// StorageConnection implementation.

// Store returns the underlying KVStore.
func (c *storageConnection) Store() interfaces.KVStore {
	return c.store
}

// ID returns the connection identifier.
func (c *storageConnection) ID() string {
	return c.id
}

// CreatedAt returns when the connection was created.
func (c *storageConnection) CreatedAt() time.Time {
	return c.createdAt
}

// LastUsedAt returns when the connection was last used.
func (c *storageConnection) LastUsedAt() time.Time {
	t := c.lastUsedAt.Load()
	if t == nil {
		return c.createdAt
	}
	return *t
}

// UseCount returns the number of times the connection has been used.
func (c *storageConnection) UseCount() uint64 {
	return c.useCount.Load()
}

// IsHealthy returns whether the connection is healthy.
func (c *storageConnection) IsHealthy() bool {
	return c.healthy.Load()
}

// MarkUsed updates the last used timestamp and increments the use count.
func (c *storageConnection) MarkUsed() {
	now := time.Now()
	c.lastUsedAt.Store(&now)
	c.useCount.Add(1)
}

// Close closes the connection and removes it from the pool.
func (c *storageConnection) Close() error {
	if c.pool != nil {
		// Let the pool handle closing and stats
		c.pool.closeConnection(c, "explicit close")
	} else if c.closed.CompareAndSwap(false, true) {
		// No pool, close directly
		if closer, ok := c.store.(interface{ Close() error }); ok {
			return closer.Close()
		}
	}
	return nil
}
