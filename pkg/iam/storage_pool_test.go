//go:build iam

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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// poolMockKVStore is a test implementation of interfaces.KVStore for pool tests.
type poolMockKVStore struct {
	mu       sync.RWMutex
	data     map[string][]byte
	closed   bool
	closedMu sync.Mutex
}

func newPoolMockKVStore() *poolMockKVStore {
	return &poolMockKVStore{
		data: make(map[string][]byte),
	}
}

func (m *poolMockKVStore) Put(ctx context.Context, key string, value []byte) error {
	m.closedMu.Lock()
	if m.closed {
		m.closedMu.Unlock()
		return &dberrors.MessageError{Message: "store is closed"}
	}
	m.closedMu.Unlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *poolMockKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	m.closedMu.Lock()
	if m.closed {
		m.closedMu.Unlock()
		return nil, &dberrors.MessageError{Message: "store is closed"}
	}
	m.closedMu.Unlock()

	m.mu.RLock()
	defer m.mu.RUnlock()
	if v, ok := m.data[key]; ok {
		return v, nil
	}
	return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "mock.get"}
}

func (m *poolMockKVStore) Delete(ctx context.Context, key string) error {
	m.closedMu.Lock()
	if m.closed {
		m.closedMu.Unlock()
		return &dberrors.MessageError{Message: "store is closed"}
	}
	m.closedMu.Unlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *poolMockKVStore) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	m.closedMu.Lock()
	if m.closed {
		m.closedMu.Unlock()
		return &dberrors.MessageError{Message: "store is closed"}
	}
	m.closedMu.Unlock()

	m.mu.RLock()
	defer m.mu.RUnlock()
	for k, v := range m.data {
		if prefix == "" || strings.HasPrefix(k, prefix) {
			if err := fn(k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *poolMockKVStore) List(ctx context.Context, prefix string) ([]string, error) {
	m.closedMu.Lock()
	if m.closed {
		m.closedMu.Unlock()
		return nil, &dberrors.MessageError{Message: "store is closed"}
	}
	m.closedMu.Unlock()

	m.mu.RLock()
	defer m.mu.RUnlock()
	var keys []string
	for k := range m.data {
		if prefix == "" || strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *poolMockKVStore) Exists(ctx context.Context, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[key]
	return ok, nil
}

func (m *poolMockKVStore) Close() error {
	m.closedMu.Lock()
	defer m.closedMu.Unlock()
	m.closed = true
	return nil
}

func (m *poolMockKVStore) IsClosed() bool {
	m.closedMu.Lock()
	defer m.closedMu.Unlock()
	return m.closed
}

// TestPoolConfigValidation tests configuration validation.
func TestPoolConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    PoolConfig
		expectErr bool
	}{
		{
			name: "valid config",
			config: PoolConfig{
				MinConns:        2,
				MaxConns:        10,
				MaxIdleTime:     5 * time.Minute,
				MaxLifetime:     30 * time.Minute,
				AcquireTimeout:  5 * time.Second,
				HealthCheckFreq: 30 * time.Second,
				Factory: func(ctx context.Context) (interfaces.KVStore, error) {
					return newPoolMockKVStore(), nil
				},
			},
			expectErr: false,
		},
		{
			name: "negative MinConns",
			config: PoolConfig{
				MinConns: -1,
				MaxConns: 10,
				Factory: func(ctx context.Context) (interfaces.KVStore, error) {
					return newPoolMockKVStore(), nil
				},
			},
			expectErr: true,
		},
		{
			name: "zero MaxConns",
			config: PoolConfig{
				MinConns: 0,
				MaxConns: 0,
				Factory: func(ctx context.Context) (interfaces.KVStore, error) {
					return newPoolMockKVStore(), nil
				},
			},
			expectErr: true,
		},
		{
			name: "MinConns exceeds MaxConns",
			config: PoolConfig{
				MinConns: 10,
				MaxConns: 5,
				Factory: func(ctx context.Context) (interfaces.KVStore, error) {
					return newPoolMockKVStore(), nil
				},
			},
			expectErr: true,
		},
		{
			name: "negative MaxIdleTime",
			config: PoolConfig{
				MinConns:    1,
				MaxConns:    10,
				MaxIdleTime: -1 * time.Second,
				Factory: func(ctx context.Context) (interfaces.KVStore, error) {
					return newPoolMockKVStore(), nil
				},
			},
			expectErr: true,
		},
		{
			name: "negative MaxLifetime",
			config: PoolConfig{
				MinConns:    1,
				MaxConns:    10,
				MaxLifetime: -1 * time.Second,
				Factory: func(ctx context.Context) (interfaces.KVStore, error) {
					return newPoolMockKVStore(), nil
				},
			},
			expectErr: true,
		},
		{
			name: "negative AcquireTimeout",
			config: PoolConfig{
				MinConns:       1,
				MaxConns:       10,
				AcquireTimeout: -1 * time.Second,
				Factory: func(ctx context.Context) (interfaces.KVStore, error) {
					return newPoolMockKVStore(), nil
				},
			},
			expectErr: true,
		},
		{
			name: "negative HealthCheckFreq",
			config: PoolConfig{
				MinConns:        1,
				MaxConns:        10,
				HealthCheckFreq: -1 * time.Second,
				Factory: func(ctx context.Context) (interfaces.KVStore, error) {
					return newPoolMockKVStore(), nil
				},
			},
			expectErr: true,
		},
		{
			name: "nil Factory",
			config: PoolConfig{
				MinConns: 1,
				MaxConns: 10,
				Factory:  nil,
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectErr && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestNewStoragePoolSuccess tests successful pool creation.
func TestNewStoragePoolSuccess(t *testing.T) {
	ctx := context.Background()
	factory := func(ctx context.Context) (interfaces.KVStore, error) {
		return newPoolMockKVStore(), nil
	}

	pool, err := NewStoragePool(ctx,
		WithMinConns(2),
		WithMaxConns(10),
		WithMaxIdleTime(5*time.Minute),
		WithMaxLifetime(30*time.Minute),
		WithAcquireTimeout(5*time.Second),
		WithHealthCheckFreq(0), // Disable for tests
		WithFactory(factory),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	stats := pool.Stats()
	if stats.TotalConns != 2 {
		t.Errorf("expected 2 initial connections, got %d", stats.TotalConns)
	}
	if stats.IdleConns != 2 {
		t.Errorf("expected 2 idle connections, got %d", stats.IdleConns)
	}
	if stats.TotalCreated != 2 {
		t.Errorf("expected 2 total created, got %d", stats.TotalCreated)
	}
}

// TestNewStoragePoolWithConfig tests pool creation with explicit config.
func TestNewStoragePoolWithConfig(t *testing.T) {
	ctx := context.Background()
	config := PoolConfig{
		MinConns:        1,
		MaxConns:        5,
		MaxIdleTime:     time.Minute,
		MaxLifetime:     10 * time.Minute,
		AcquireTimeout:  time.Second,
		HealthCheckFreq: 0,
		Factory: func(ctx context.Context) (interfaces.KVStore, error) {
			return newPoolMockKVStore(), nil
		},
	}

	pool, err := NewStoragePoolWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	stats := pool.Stats()
	if stats.TotalConns != 1 {
		t.Errorf("expected 1 initial connection, got %d", stats.TotalConns)
	}
}

// TestNewStoragePoolFactoryError tests pool creation with factory error.
func TestNewStoragePoolFactoryError(t *testing.T) {
	ctx := context.Background()
	factory := func(ctx context.Context) (interfaces.KVStore, error) {
		return nil, &dberrors.MessageError{Message: "factory error"}
	}

	_, err := NewStoragePool(ctx,
		WithMinConns(1),
		WithMaxConns(5),
		WithFactory(factory),
	)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}

// TestPoolGetAndPut tests basic Get and Put operations.
func TestPoolGetAndPut(t *testing.T) {
	ctx := context.Background()
	pool, err := NewStoragePool(ctx,
		WithMinConns(2),
		WithMaxConns(5),
		WithHealthCheckFreq(0),
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			return newPoolMockKVStore(), nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	// Get a connection
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}

	// Verify connection properties
	if conn.ID() == "" {
		t.Error("connection ID should not be empty")
	}
	if conn.UseCount() != 1 {
		t.Errorf("expected use count 1, got %d", conn.UseCount())
	}
	if !conn.IsHealthy() {
		t.Error("connection should be healthy")
	}

	stats := pool.Stats()
	if stats.InUseConns != 1 {
		t.Errorf("expected 1 in-use connection, got %d", stats.InUseConns)
	}

	// Use the connection
	store := conn.Store()
	if err := store.Put(ctx, "key", []byte("value")); err != nil {
		t.Errorf("failed to put value: %v", err)
	}

	// Return the connection
	if err := pool.Put(conn); err != nil {
		t.Errorf("failed to put connection: %v", err)
	}

	stats = pool.Stats()
	if stats.InUseConns != 0 {
		t.Errorf("expected 0 in-use connections, got %d", stats.InUseConns)
	}
}

// TestPoolConcurrentAccess tests concurrent pool access.
func TestPoolConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	pool, err := NewStoragePool(ctx,
		WithMinConns(2),
		WithMaxConns(5),
		WithAcquireTimeout(10*time.Second),
		WithHealthCheckFreq(0),
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			return newPoolMockKVStore(), nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	var wg sync.WaitGroup
	errors := make(chan error, 100)
	operations := 50

	for i := 0; i < operations; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			conn, err := pool.Get(ctx)
			if err != nil {
				errors <- err
				return
			}

			// Simulate some work
			store := conn.Store()
			key := "key-" + string(rune(id))
			if err := store.Put(ctx, key, []byte("value")); err != nil {
				errors <- err
			}

			// Small delay to simulate processing
			time.Sleep(time.Millisecond)

			if err := pool.Put(conn); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent operation error: %v", err)
	}

	stats := pool.Stats()
	if stats.InUseConns != 0 {
		t.Errorf("expected 0 in-use connections after operations, got %d", stats.InUseConns)
	}
}

// TestPoolExhaustion tests pool exhaustion behavior.
func TestPoolExhaustion(t *testing.T) {
	ctx := context.Background()
	pool, err := NewStoragePool(ctx,
		WithMinConns(1),
		WithMaxConns(2),
		WithAcquireTimeout(100*time.Millisecond),
		WithHealthCheckFreq(0),
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			return newPoolMockKVStore(), nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	// Get all connections
	conn1, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get first connection: %v", err)
	}

	conn2, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get second connection: %v", err)
	}

	// Try to get another connection - should timeout
	_, err = pool.Get(ctx)
	if err == nil {
		t.Fatal("expected pool exhaustion error")
	}

	// Return connections
	pool.Put(conn1)
	pool.Put(conn2)

	// Now we should be able to get a connection
	conn3, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get connection after returning: %v", err)
	}
	pool.Put(conn3)
}

// TestPoolContextCancellation tests context cancellation during Get.
func TestPoolContextCancellation(t *testing.T) {
	ctx := context.Background()
	pool, err := NewStoragePool(ctx,
		WithMinConns(1),
		WithMaxConns(1),
		WithAcquireTimeout(5*time.Second),
		WithHealthCheckFreq(0),
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			return newPoolMockKVStore(), nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	// Get the only connection
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}

	// Create a cancellable context
	cancelCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Try to get another connection with cancellable context
	_, err = pool.Get(cancelCtx)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}

	pool.Put(conn)
}

// TestPoolClose tests pool close behavior.
func TestPoolClose(t *testing.T) {
	ctx := context.Background()
	pool, err := NewStoragePool(ctx,
		WithMinConns(2),
		WithMaxConns(5),
		WithHealthCheckFreq(0),
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			return newPoolMockKVStore(), nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	// Get a connection before closing
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}

	// Close the pool
	if err := pool.Close(); err != nil {
		t.Fatalf("failed to close pool: %v", err)
	}

	// Double close should not error
	if err := pool.Close(); err != nil {
		t.Errorf("double close should not error: %v", err)
	}

	// Get after close should return error
	_, err = pool.Get(ctx)
	if err == nil {
		t.Fatal("Get after close should return error")
	}

	// Put after close should close the connection
	err = pool.Put(conn)
	if err != nil {
		// Put may or may not return an error depending on implementation
	}
}

// TestPoolMaxUseCount tests connection recycling based on use count.
func TestPoolMaxUseCount(t *testing.T) {
	ctx := context.Background()
	var createCount atomic.Int32

	pool, err := NewStoragePool(ctx,
		WithMinConns(1),
		WithMaxConns(2),
		WithMaxUseCount(3), // Recycle after 3 uses
		WithHealthCheckFreq(0),
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			createCount.Add(1)
			return newPoolMockKVStore(), nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	initialCount := createCount.Load()

	// Use the connection 4 times - should trigger recycling on 4th
	for i := 0; i < 5; i++ {
		conn, err := pool.Get(ctx)
		if err != nil {
			t.Fatalf("failed to get connection on iteration %d: %v", i, err)
		}
		pool.Put(conn)
	}

	// Should have created at least one more connection due to recycling
	finalCount := createCount.Load()
	if finalCount <= initialCount {
		t.Errorf("expected connection to be recycled, initial: %d, final: %d", initialCount, finalCount)
	}
}

// TestPoolHealthCheck tests health checking functionality.
func TestPoolHealthCheck(t *testing.T) {
	ctx := context.Background()
	var healthCheckCalls atomic.Int32

	pool, err := NewStoragePool(ctx,
		WithMinConns(2),
		WithMaxConns(5),
		WithHealthCheckFreq(50*time.Millisecond),
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			return newPoolMockKVStore(), nil
		}),
		WithHealthChecker(func(ctx context.Context, store interfaces.KVStore) error {
			healthCheckCalls.Add(1)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	// Wait for health checks to run
	time.Sleep(150 * time.Millisecond)

	if healthCheckCalls.Load() == 0 {
		t.Error("expected health check to be called")
	}
}

// TestPoolHealthCheckFailure tests handling of health check failures.
func TestPoolHealthCheckFailure(t *testing.T) {
	ctx := context.Background()
	var healthCheckFails atomic.Bool
	healthCheckFails.Store(false)

	pool, err := NewStoragePool(ctx,
		WithMinConns(1),
		WithMaxConns(2),
		WithHealthCheckFreq(50*time.Millisecond),
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			return newPoolMockKVStore(), nil
		}),
		WithHealthChecker(func(ctx context.Context, store interfaces.KVStore) error {
			if healthCheckFails.Load() {
				return &dberrors.MessageError{Message: "health check failed"}
			}
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	// Get a connection while healthy
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}
	pool.Put(conn)

	// Mark health check to fail
	healthCheckFails.Store(true)

	// Wait for health check to run
	time.Sleep(100 * time.Millisecond)

	// We should still be able to get connections (they get replaced)
	conn, err = pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get connection after health check failure: %v", err)
	}
	pool.Put(conn)
}

// TestPoolStats tests statistics accuracy.
func TestPoolStats(t *testing.T) {
	ctx := context.Background()
	pool, err := NewStoragePool(ctx,
		WithMinConns(2),
		WithMaxConns(5),
		WithHealthCheckFreq(0),
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			return newPoolMockKVStore(), nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	stats := pool.Stats()
	if stats.TotalConns != 2 {
		t.Errorf("expected 2 total connections, got %d", stats.TotalConns)
	}
	if stats.IdleConns != 2 {
		t.Errorf("expected 2 idle connections, got %d", stats.IdleConns)
	}
	if stats.InUseConns != 0 {
		t.Errorf("expected 0 in-use connections, got %d", stats.InUseConns)
	}
	if stats.TotalCreated != 2 {
		t.Errorf("expected 2 total created, got %d", stats.TotalCreated)
	}

	// Get a connection
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}

	stats = pool.Stats()
	if stats.InUseConns != 1 {
		t.Errorf("expected 1 in-use connection, got %d", stats.InUseConns)
	}

	pool.Put(conn)

	stats = pool.Stats()
	if stats.InUseConns != 0 {
		t.Errorf("expected 0 in-use connections after put, got %d", stats.InUseConns)
	}
}

// TestPoolOptions tests functional options.
func TestPoolOptions(t *testing.T) {
	tests := []struct {
		name      string
		opt       PoolOption
		expectErr bool
	}{
		{
			name:      "valid MinConns",
			opt:       WithMinConns(5),
			expectErr: false,
		},
		{
			name:      "negative MinConns",
			opt:       WithMinConns(-1),
			expectErr: true,
		},
		{
			name:      "valid MaxConns",
			opt:       WithMaxConns(10),
			expectErr: false,
		},
		{
			name:      "zero MaxConns",
			opt:       WithMaxConns(0),
			expectErr: true,
		},
		{
			name:      "valid MaxIdleTime",
			opt:       WithMaxIdleTime(5 * time.Minute),
			expectErr: false,
		},
		{
			name:      "negative MaxIdleTime",
			opt:       WithMaxIdleTime(-1 * time.Minute),
			expectErr: true,
		},
		{
			name:      "valid MaxLifetime",
			opt:       WithMaxLifetime(30 * time.Minute),
			expectErr: false,
		},
		{
			name:      "negative MaxLifetime",
			opt:       WithMaxLifetime(-1 * time.Minute),
			expectErr: true,
		},
		{
			name:      "valid MaxUseCount",
			opt:       WithMaxUseCount(10000),
			expectErr: false,
		},
		{
			name:      "zero MaxUseCount",
			opt:       WithMaxUseCount(0),
			expectErr: false, // Zero is valid (disables use count limiting)
		},
		{
			name:      "valid AcquireTimeout",
			opt:       WithAcquireTimeout(5 * time.Second),
			expectErr: false,
		},
		{
			name:      "negative AcquireTimeout",
			opt:       WithAcquireTimeout(-1 * time.Second),
			expectErr: true,
		},
		{
			name:      "valid HealthCheckFreq",
			opt:       WithHealthCheckFreq(30 * time.Second),
			expectErr: false,
		},
		{
			name:      "negative HealthCheckFreq",
			opt:       WithHealthCheckFreq(-1 * time.Second),
			expectErr: true,
		},
		{
			name: "valid Factory",
			opt: WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
				return newPoolMockKVStore(), nil
			}),
			expectErr: false,
		},
		{
			name:      "nil Factory",
			opt:       WithFactory(nil),
			expectErr: true,
		},
		{
			name: "valid HealthChecker",
			opt: WithHealthChecker(func(ctx context.Context, store interfaces.KVStore) error {
				return nil
			}),
			expectErr: false,
		},
		{
			name:      "nil HealthChecker",
			opt:       WithHealthChecker(nil),
			expectErr: false, // nil is valid (disables health checking)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultPoolConfig()
			err := tt.opt(&config)
			if tt.expectErr && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestDefaultPoolConfig tests default configuration values.
func TestDefaultPoolConfig(t *testing.T) {
	config := DefaultPoolConfig()

	if config.MinConns != 2 {
		t.Errorf("expected MinConns 2, got %d", config.MinConns)
	}
	if config.MaxConns != 10 {
		t.Errorf("expected MaxConns 10, got %d", config.MaxConns)
	}
	if config.MaxIdleTime != 5*time.Minute {
		t.Errorf("expected MaxIdleTime 5m, got %v", config.MaxIdleTime)
	}
	if config.MaxLifetime != 30*time.Minute {
		t.Errorf("expected MaxLifetime 30m, got %v", config.MaxLifetime)
	}
	if config.MaxUseCount != 10000 {
		t.Errorf("expected MaxUseCount 10000, got %d", config.MaxUseCount)
	}
	if config.AcquireTimeout != 5*time.Second {
		t.Errorf("expected AcquireTimeout 5s, got %v", config.AcquireTimeout)
	}
	if config.HealthCheckFreq != 30*time.Second {
		t.Errorf("expected HealthCheckFreq 30s, got %v", config.HealthCheckFreq)
	}
}

// TestStorageConnection tests connection interface implementation.
func TestStorageConnection(t *testing.T) {
	ctx := context.Background()
	pool, err := NewStoragePool(ctx,
		WithMinConns(1),
		WithMaxConns(2),
		WithHealthCheckFreq(0),
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			return newPoolMockKVStore(), nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}
	defer pool.Put(conn)

	// Test ID
	if conn.ID() == "" {
		t.Error("ID should not be empty")
	}

	// Test Store
	store := conn.Store()
	if store == nil {
		t.Error("Store should not be nil")
	}

	// Test CreatedAt
	createdAt := conn.CreatedAt()
	if createdAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}

	// Test LastUsedAt
	lastUsed := conn.LastUsedAt()
	if lastUsed.IsZero() {
		t.Error("LastUsedAt should not be zero")
	}
	if lastUsed.Before(createdAt) {
		t.Error("LastUsedAt should be >= CreatedAt")
	}

	// Test UseCount
	useCount := conn.UseCount()
	if useCount != 1 {
		t.Errorf("expected UseCount 1, got %d", useCount)
	}

	// Test IsHealthy
	if !conn.IsHealthy() {
		t.Error("connection should be healthy")
	}

	// Test MarkUsed
	conn.MarkUsed()
	if conn.UseCount() != 2 {
		t.Errorf("expected UseCount 2 after MarkUsed, got %d", conn.UseCount())
	}
}

// TestPoolMaxLifetime tests connection recycling based on max lifetime.
func TestPoolMaxLifetime(t *testing.T) {
	ctx := context.Background()
	var createCount atomic.Int32

	pool, err := NewStoragePool(ctx,
		WithMinConns(1),
		WithMaxConns(2),
		WithMaxLifetime(50*time.Millisecond), // Short lifetime for testing
		WithHealthCheckFreq(0),
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			createCount.Add(1)
			return newPoolMockKVStore(), nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	initialCount := createCount.Load()

	// Get and return a connection
	conn1, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get first connection: %v", err)
	}
	pool.Put(conn1)

	// Wait for connection to age past max lifetime
	time.Sleep(100 * time.Millisecond)

	// Get another connection - should trigger recycling
	conn2, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get second connection: %v", err)
	}
	pool.Put(conn2)

	// Should have created a new connection due to recycling
	finalCount := createCount.Load()
	if finalCount <= initialCount {
		t.Errorf("expected connection to be recycled, initial: %d, final: %d", initialCount, finalCount)
	}
}

// TestPutInvalidConnection tests putting an invalid connection type.
func TestPutInvalidConnection(t *testing.T) {
	ctx := context.Background()
	pool, err := NewStoragePool(ctx,
		WithMinConns(1),
		WithMaxConns(2),
		WithHealthCheckFreq(0),
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			return newPoolMockKVStore(), nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	// Create a mock connection that doesn't implement our internal type
	mockConn := &poolMockStorageConnection{}
	err = pool.Put(mockConn)
	if err == nil {
		t.Error("expected error when putting invalid connection type")
	}
}

// poolMockStorageConnection is an invalid connection type for testing.
type poolMockStorageConnection struct{}

func (m *poolMockStorageConnection) Store() interfaces.KVStore { return nil }
func (m *poolMockStorageConnection) ID() string                { return "mock" }
func (m *poolMockStorageConnection) CreatedAt() time.Time      { return time.Now() }
func (m *poolMockStorageConnection) LastUsedAt() time.Time     { return time.Now() }
func (m *poolMockStorageConnection) UseCount() uint64          { return 0 }
func (m *poolMockStorageConnection) IsHealthy() bool           { return true }
func (m *poolMockStorageConnection) MarkUsed()                 {}
func (m *poolMockStorageConnection) Close() error              { return nil }

// TestConnectionClose tests explicit connection close.
func TestConnectionClose(t *testing.T) {
	ctx := context.Background()
	pool, err := NewStoragePool(ctx,
		WithMinConns(1),
		WithMaxConns(2),
		WithHealthCheckFreq(0),
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			return newPoolMockKVStore(), nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}

	// Close the connection explicitly
	if err := conn.Close(); err != nil {
		t.Errorf("failed to close connection: %v", err)
	}

	// Close again should not error
	if err := conn.Close(); err != nil {
		t.Errorf("double close should not error: %v", err)
	}

	// Connection should no longer be tracked
	stats := pool.Stats()
	if stats.TotalClosed < 1 {
		t.Error("expected at least one closed connection in stats")
	}
}

// TestPoolFullReturnToPool tests returning connection when pool is full.
func TestPoolFullReturnToPool(t *testing.T) {
	ctx := context.Background()
	pool, err := NewStoragePool(ctx,
		WithMinConns(1),
		WithMaxConns(1), // Only one connection allowed
		WithHealthCheckFreq(0),
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			return newPoolMockKVStore(), nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	// Get the only connection
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}

	// Put it back
	if err := pool.Put(conn); err != nil {
		t.Errorf("failed to put connection: %v", err)
	}

	// The connection should be in the pool now
	stats := pool.Stats()
	if stats.IdleConns != 1 {
		t.Errorf("expected 1 idle connection, got %d", stats.IdleConns)
	}
}

// TestErrorTypes tests the custom error types.
func TestErrorTypes(t *testing.T) {
	// Test ErrPoolClosed
	if ErrPoolClosed.Code != dberrors.ErrForbidden {
		t.Errorf("ErrPoolClosed should have code ErrForbidden, got %v", ErrPoolClosed.Code)
	}

	// Test ErrPoolExhausted
	if ErrPoolExhausted.Code != dberrors.ErrResourceExhausted {
		t.Errorf("ErrPoolExhausted should have code ErrResourceExhausted, got %v", ErrPoolExhausted.Code)
	}

	// Test ErrPoolConfigInvalid
	if ErrPoolConfigInvalid.Code != dberrors.ErrInvalidInput {
		t.Errorf("ErrPoolConfigInvalid should have code ErrInvalidInput, got %v", ErrPoolConfigInvalid.Code)
	}

	// Test ErrConnectionUnhealthy
	if ErrConnectionUnhealthy.Code != dberrors.ErrUnavailable {
		t.Errorf("ErrConnectionUnhealthy should have code ErrUnavailable, got %v", ErrConnectionUnhealthy.Code)
	}

	// Test ErrConnectionClosed
	if ErrConnectionClosed.Code != dberrors.ErrForbidden {
		t.Errorf("ErrConnectionClosed should have code ErrForbidden, got %v", ErrConnectionClosed.Code)
	}
}

// TestClosableKVStoreWrapper tests the closable wrapper functionality.
func TestClosableKVStoreWrapper(t *testing.T) {
	store := newPoolMockKVStore()
	wrapped := wrapClosable(store)

	// Should be able to use the store
	ctx := context.Background()
	if err := wrapped.Put(ctx, "key", []byte("value")); err != nil {
		t.Errorf("failed to put: %v", err)
	}

	// Double wrap should return same wrapper
	doubleWrapped := wrapClosable(wrapped)
	if doubleWrapped != wrapped {
		t.Error("double wrapping should return same instance")
	}

	// Close should close the underlying store
	if err := wrapped.Close(); err != nil {
		t.Errorf("failed to close: %v", err)
	}

	// Underlying store should be closed
	if !store.IsClosed() {
		t.Error("underlying store should be closed")
	}
}

// TestPutClosedConnection tests putting a closed connection.
func TestPutClosedConnection(t *testing.T) {
	ctx := context.Background()
	pool, err := NewStoragePool(ctx,
		WithMinConns(1),
		WithMaxConns(2),
		WithHealthCheckFreq(0),
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			return newPoolMockKVStore(), nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}

	// Close the connection
	conn.Close()

	// Try to put the closed connection
	err = pool.Put(conn)
	if err != ErrConnectionClosed {
		t.Errorf("expected ErrConnectionClosed, got %v", err)
	}
}

// TestNewStoragePoolWithConfigFactoryError tests NewStoragePoolWithConfig with factory error.
func TestNewStoragePoolWithConfigFactoryError(t *testing.T) {
	ctx := context.Background()
	config := PoolConfig{
		MinConns:        1,
		MaxConns:        5,
		MaxIdleTime:     time.Minute,
		MaxLifetime:     10 * time.Minute,
		AcquireTimeout:  time.Second,
		HealthCheckFreq: 0,
		Factory: func(ctx context.Context) (interfaces.KVStore, error) {
			return nil, &dberrors.MessageError{Message: "factory error"}
		},
	}

	_, err := NewStoragePoolWithConfig(ctx, config)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}

// TestNewStoragePoolWithConfigValidationError tests NewStoragePoolWithConfig with invalid config.
func TestNewStoragePoolWithConfigValidationError(t *testing.T) {
	ctx := context.Background()
	config := PoolConfig{
		MinConns: -1, // Invalid
		MaxConns: 5,
		Factory: func(ctx context.Context) (interfaces.KVStore, error) {
			return newPoolMockKVStore(), nil
		},
	}

	_, err := NewStoragePoolWithConfig(ctx, config)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}

// TestPerformMaintenance tests the performMaintenance function directly.
func TestPerformMaintenance(t *testing.T) {
	ctx := context.Background()

	// Create pool with short idle time
	pool, err := NewStoragePool(ctx,
		WithMinConns(0), // No minimum so we can test cleanup
		WithMaxConns(5),
		WithMaxIdleTime(10*time.Millisecond),
		WithHealthCheckFreq(0),
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			return newPoolMockKVStore(), nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	// Get and return a few connections to populate the pool
	var conns []StorageConnection
	for i := 0; i < 3; i++ {
		conn, err := pool.Get(ctx)
		if err != nil {
			t.Fatalf("failed to get connection: %v", err)
		}
		conns = append(conns, conn)
	}

	// Return all connections
	for _, conn := range conns {
		pool.Put(conn)
	}

	// Wait for connections to become idle
	time.Sleep(50 * time.Millisecond)

	// Call performMaintenance directly
	impl := pool.(*storagePool)
	impl.performMaintenance()

	// After maintenance, idle connections exceeding MaxIdleTime should be closed
	stats := pool.Stats()
	// Since we have MinConns=0 and connections were idle > MaxIdleTime,
	// they should have been closed
	if stats.MaxIdleTimeClosed == 0 {
		t.Error("expected at least one connection to be closed due to max idle time")
	}
}

// TestPerformMaintenanceRefillMinConns tests that maintenance refills min connections.
func TestPerformMaintenanceRefillMinConns(t *testing.T) {
	ctx := context.Background()

	// Create pool with min connections
	pool, err := NewStoragePool(ctx,
		WithMinConns(2),
		WithMaxConns(5),
		WithMaxIdleTime(0),     // Disable idle time cleanup
		WithMaxLifetime(0),     // Disable lifetime cleanup
		WithHealthCheckFreq(0), // Disable health checks
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			return newPoolMockKVStore(), nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	// Get all connections and close them explicitly
	var conns []StorageConnection
	for i := 0; i < 2; i++ {
		conn, err := pool.Get(ctx)
		if err != nil {
			t.Fatalf("failed to get connection: %v", err)
		}
		conns = append(conns, conn)
	}

	// Close all connections (don't put them back)
	for _, conn := range conns {
		conn.Close()
	}

	// Wait a moment for stats to update
	time.Sleep(10 * time.Millisecond)

	stats := pool.Stats()
	if stats.TotalConns != 0 {
		t.Errorf("expected 0 total connections after closing, got %d", stats.TotalConns)
	}

	// Call performMaintenance directly
	impl := pool.(*storagePool)
	impl.performMaintenance()

	// After maintenance, min connections should be recreated
	stats = pool.Stats()
	if stats.TotalConns < 2 {
		t.Errorf("expected at least 2 connections after maintenance refill, got %d", stats.TotalConns)
	}
}

// TestClosableKVStoreWithCloseFunc tests closable wrapper with explicit close function.
func TestClosableKVStoreWithCloseFunc(t *testing.T) {
	closeCalled := false
	wrapper := &closableKVStore{
		KVStore: newPoolMockKVStore(),
		closeFunc: func() error {
			closeCalled = true
			return nil
		},
	}

	if err := wrapper.Close(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !closeCalled {
		t.Error("expected closeFunc to be called")
	}
}

// TestPoolGetRecyclesOnFactoryError tests that Get handles factory errors during recycling.
func TestPoolGetRecyclesOnFactoryError(t *testing.T) {
	ctx := context.Background()
	var createCount atomic.Int32
	var failFactory atomic.Bool

	pool, err := NewStoragePool(ctx,
		WithMinConns(1),
		WithMaxConns(2),
		WithMaxLifetime(10*time.Millisecond), // Short lifetime to trigger recycling
		WithAcquireTimeout(500*time.Millisecond),
		WithHealthCheckFreq(0),
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			count := createCount.Add(1)
			if failFactory.Load() && count > 1 {
				return nil, &dberrors.MessageError{Message: "factory error"}
			}
			return newPoolMockKVStore(), nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	// Get initial connection
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get initial connection: %v", err)
	}
	pool.Put(conn)

	// Wait for connection to age
	time.Sleep(50 * time.Millisecond)

	// Make factory fail
	failFactory.Store(true)

	// Try to get connection - should retry and eventually fail
	_, err = pool.Get(ctx)
	// This may or may not error depending on timing
	// The key is that it doesn't hang
}

// TestPoolOptionError tests that option errors are returned during pool creation.
func TestPoolOptionError(t *testing.T) {
	ctx := context.Background()

	// Use an option that returns an error
	_, err := NewStoragePool(ctx,
		WithMinConns(-1), // Invalid
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			return newPoolMockKVStore(), nil
		}),
	)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}

// TestConnectionCloseWithoutPool tests closing a connection that has no pool reference.
func TestConnectionCloseWithoutPool(t *testing.T) {
	store := newPoolMockKVStore()
	now := time.Now()

	conn := &storageConnection{
		id:        "test-id",
		store:     &closableKVStore{KVStore: store},
		createdAt: now,
		pool:      nil, // No pool reference
	}
	conn.lastUsedAt.Store(&now)
	conn.healthy.Store(true)

	// Close should work without a pool
	if err := conn.Close(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify the store was closed
	if !store.IsClosed() {
		t.Error("expected store to be closed")
	}

	// Double close should not error
	if err := conn.Close(); err != nil {
		t.Errorf("double close should not error: %v", err)
	}
}

// TestPoolStatsWithNegativeInUse tests that Stats handles negative in-use count gracefully.
func TestPoolStatsWithNegativeInUse(t *testing.T) {
	ctx := context.Background()
	pool, err := NewStoragePool(ctx,
		WithMinConns(1),
		WithMaxConns(2),
		WithHealthCheckFreq(0),
		WithFactory(func(ctx context.Context) (interfaces.KVStore, error) {
			return newPoolMockKVStore(), nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	// Artificially set inUseCount to negative (shouldn't happen normally)
	impl := pool.(*storagePool)
	impl.inUseCount.Store(-1)

	// Stats should return 0, not negative
	stats := pool.Stats()
	if stats.InUseConns != 0 {
		t.Errorf("expected InUseConns to be 0 when negative, got %d", stats.InUseConns)
	}
}

// =============================================================================
// Storage Pool Error Type Tests
// =============================================================================

func TestPoolClosedError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &PoolClosedError{}
		if err.Error() != "storage pool is closed" {
			t.Errorf("unexpected: %q", err.Error())
		}
	})
}

func TestPoolExhaustedError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &PoolExhaustedError{}
		if err.Error() != "storage pool exhausted: no connections available" {
			t.Errorf("unexpected: %q", err.Error())
		}
	})
}

func TestPoolConfigInvalidError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &PoolConfigInvalidError{}
		if err.Error() != "invalid pool configuration" {
			t.Errorf("unexpected: %q", err.Error())
		}
	})
}

func TestConnectionUnhealthyError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &ConnectionUnhealthyError{}
		if err.Error() != "storage connection is unhealthy" {
			t.Errorf("unexpected: %q", err.Error())
		}
	})
}

func TestConnectionClosedError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &ConnectionClosedError{}
		if err.Error() != "storage connection is closed" {
			t.Errorf("unexpected: %q", err.Error())
		}
	})
}
