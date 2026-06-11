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

// Package iam provides identity and access management for QuicBaaS.
package iam

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// StoragePool manages a pool of storage connections for the IAM storage layer.
// It provides connection reuse, health checking, and lifecycle management.
type StoragePool interface {
	// Get retrieves a storage connection from the pool.
	// The connection should be returned via Put when no longer needed.
	// Returns ErrPoolClosed if the pool has been closed.
	// Returns ErrPoolExhausted if no connections are available and acquire timeout expires.
	Get(ctx context.Context) (StorageConnection, error)

	// Put returns a storage connection to the pool.
	// If the connection is unhealthy, it will be discarded.
	// Returns ErrPoolClosed if the pool has been closed.
	Put(conn StorageConnection) error

	// Close closes all connections in the pool and releases resources.
	// After Close is called, Get will return ErrPoolClosed.
	Close() error

	// Stats returns current pool statistics.
	Stats() PoolStats
}

// StorageConnection represents a single connection to the storage backend.
// It wraps a KVStore with connection metadata for pool management.
type StorageConnection interface {
	// Store returns the underlying KVStore for storage operations.
	Store() interfaces.KVStore

	// ID returns the unique connection identifier.
	ID() string

	// CreatedAt returns when this connection was created.
	CreatedAt() time.Time

	// LastUsedAt returns when this connection was last used.
	LastUsedAt() time.Time

	// UseCount returns the number of times this connection has been used.
	UseCount() uint64

	// IsHealthy returns whether the connection is healthy.
	IsHealthy() bool

	// MarkUsed updates the last used timestamp and increments the use count.
	MarkUsed()

	// Close closes the underlying connection.
	Close() error
}

// PoolConfig contains configuration options for the storage pool.
type PoolConfig struct {
	// MinConns is the minimum number of connections to maintain in the pool.
	// These connections are pre-created during pool initialization.
	// Default: 2
	MinConns int

	// MaxConns is the maximum number of connections the pool can hold.
	// Once this limit is reached, Get will block until a connection is available.
	// Default: 10
	MaxConns int

	// MaxIdleTime is the maximum time a connection can remain idle before being closed.
	// Idle connections are checked periodically and closed if they exceed this duration.
	// Default: 5 minutes
	MaxIdleTime time.Duration

	// MaxLifetime is the maximum lifetime of a connection before it is recycled.
	// Connections older than this duration are closed and replaced.
	// Default: 30 minutes
	MaxLifetime time.Duration

	// MaxUseCount is the maximum number of times a connection can be used.
	// After this many uses, the connection is closed and replaced.
	// Set to 0 to disable use count limiting.
	// Default: 10000
	MaxUseCount uint64

	// AcquireTimeout is the maximum time to wait when acquiring a connection.
	// If no connection becomes available within this timeout, ErrPoolExhausted is returned.
	// Default: 5 seconds
	AcquireTimeout time.Duration

	// HealthCheckFreq is the frequency at which idle connections are health checked.
	// Set to 0 to disable periodic health checks.
	// Default: 30 seconds
	HealthCheckFreq time.Duration

	// Factory is the function used to create new storage connections.
	// This is required and must be provided.
	Factory ConnectionFactory

	// HealthChecker is an optional function to verify connection health.
	// If nil, connections are assumed to be healthy.
	HealthChecker HealthCheckFunc
}

// ConnectionFactory creates a new storage connection.
type ConnectionFactory func(ctx context.Context) (interfaces.KVStore, error)

// HealthCheckFunc checks if a storage connection is healthy.
type HealthCheckFunc func(ctx context.Context, store interfaces.KVStore) error

// PoolStats contains statistics about the storage pool.
type PoolStats struct {
	// TotalConns is the current total number of connections in the pool.
	TotalConns int

	// IdleConns is the number of idle connections available for use.
	IdleConns int

	// InUseConns is the number of connections currently in use.
	InUseConns int

	// WaitCount is the total number of times Get had to wait for a connection.
	WaitCount int64

	// WaitDuration is the total time spent waiting for connections.
	WaitDuration time.Duration

	// MaxIdleTimeClosed is the number of connections closed due to max idle time.
	MaxIdleTimeClosed int64

	// MaxLifetimeClosed is the number of connections closed due to max lifetime.
	MaxLifetimeClosed int64

	// MaxUseCountClosed is the number of connections closed due to max use count.
	MaxUseCountClosed int64

	// HealthChecksClosed is the number of connections closed due to failed health checks.
	HealthChecksClosed int64

	// TotalCreated is the total number of connections created since pool initialization.
	TotalCreated int64

	// TotalClosed is the total number of connections closed since pool initialization.
	TotalClosed int64
}

// Pool-specific error types.
var (
	// ErrPoolClosed is returned when attempting to use a closed pool.
	ErrPoolClosed = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.storage.pool",
		Err:  &PoolClosedError{},
	}

	// ErrPoolExhausted is returned when no connections are available within the timeout.
	ErrPoolExhausted = &dberrors.QBError{
		Code: dberrors.ErrResourceExhausted,
		Op:   "iam.storage.pool",
		Err:  &PoolExhaustedError{},
	}

	// ErrPoolConfigInvalid is returned when pool configuration is invalid.
	ErrPoolConfigInvalid = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.storage.pool.config",
		Err:  &PoolConfigInvalidError{},
	}

	// ErrConnectionUnhealthy is returned when a connection fails health check.
	ErrConnectionUnhealthy = &dberrors.QBError{
		Code: dberrors.ErrUnavailable,
		Op:   "iam.storage.pool.health",
		Err:  &ConnectionUnhealthyError{},
	}

	// ErrConnectionClosed is returned when attempting to use a closed connection.
	ErrConnectionClosed = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.storage.connection",
		Err:  &ConnectionClosedError{},
	}
)

// PoolClosedError indicates the storage pool is closed.
type PoolClosedError struct{}

// Error implements the error interface.
func (e *PoolClosedError) Error() string {
	return "storage pool is closed"
}

// PoolExhaustedError indicates no connections are available in the pool.
type PoolExhaustedError struct{}

// Error implements the error interface.
func (e *PoolExhaustedError) Error() string {
	return "storage pool exhausted: no connections available"
}

// PoolConfigInvalidError indicates the pool configuration is invalid.
type PoolConfigInvalidError struct{}

// Error implements the error interface.
func (e *PoolConfigInvalidError) Error() string {
	return "invalid pool configuration"
}

// ConnectionUnhealthyError indicates a storage connection is unhealthy.
type ConnectionUnhealthyError struct{}

// Error implements the error interface.
func (e *ConnectionUnhealthyError) Error() string {
	return "storage connection is unhealthy"
}

// ConnectionClosedError indicates a storage connection is closed.
type ConnectionClosedError struct{}

// Error implements the error interface.
func (e *ConnectionClosedError) Error() string {
	return "storage connection is closed"
}

// DefaultPoolConfig returns a PoolConfig with sensible defaults.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MinConns:        2,
		MaxConns:        10,
		MaxIdleTime:     5 * time.Minute,
		MaxLifetime:     30 * time.Minute,
		MaxUseCount:     10000,
		AcquireTimeout:  5 * time.Second,
		HealthCheckFreq: 30 * time.Second,
	}
}

// Validate validates the pool configuration.
func (c *PoolConfig) Validate() error {
	if c.MinConns < 0 {
		return api.NewInvalidInputError("MinConns cannot be negative")
	}
	if c.MaxConns < 1 {
		return api.NewInvalidInputError("MaxConns must be at least 1")
	}
	if c.MinConns > c.MaxConns {
		return api.NewInvalidInputError("MinConns cannot exceed MaxConns")
	}
	if c.MaxIdleTime < 0 {
		return api.NewInvalidInputError("MaxIdleTime cannot be negative")
	}
	if c.MaxLifetime < 0 {
		return api.NewInvalidInputError("MaxLifetime cannot be negative")
	}
	if c.AcquireTimeout < 0 {
		return api.NewInvalidInputError("AcquireTimeout cannot be negative")
	}
	if c.HealthCheckFreq < 0 {
		return api.NewInvalidInputError("HealthCheckFreq cannot be negative")
	}
	if c.Factory == nil {
		return api.NewRequiredFieldError("Factory")
	}
	return nil
}

// PoolOption is a functional option for configuring the storage pool.
type PoolOption func(*PoolConfig) error

// WithMinConns sets the minimum number of connections.
func WithMinConns(n int) PoolOption {
	return func(c *PoolConfig) error {
		if n < 0 {
			return api.NewInvalidInputError("MinConns cannot be negative")
		}
		c.MinConns = n
		return nil
	}
}

// WithMaxConns sets the maximum number of connections.
func WithMaxConns(n int) PoolOption {
	return func(c *PoolConfig) error {
		if n < 1 {
			return api.NewInvalidInputError("MaxConns must be at least 1")
		}
		c.MaxConns = n
		return nil
	}
}

// WithMaxIdleTime sets the maximum idle time for connections.
func WithMaxIdleTime(d time.Duration) PoolOption {
	return func(c *PoolConfig) error {
		if d < 0 {
			return api.NewInvalidInputError("MaxIdleTime cannot be negative")
		}
		c.MaxIdleTime = d
		return nil
	}
}

// WithMaxLifetime sets the maximum lifetime for connections.
func WithMaxLifetime(d time.Duration) PoolOption {
	return func(c *PoolConfig) error {
		if d < 0 {
			return api.NewInvalidInputError("MaxLifetime cannot be negative")
		}
		c.MaxLifetime = d
		return nil
	}
}

// WithMaxUseCount sets the maximum use count for connections.
func WithMaxUseCount(n uint64) PoolOption {
	return func(c *PoolConfig) error {
		c.MaxUseCount = n
		return nil
	}
}

// WithAcquireTimeout sets the acquire timeout.
func WithAcquireTimeout(d time.Duration) PoolOption {
	return func(c *PoolConfig) error {
		if d < 0 {
			return api.NewInvalidInputError("AcquireTimeout cannot be negative")
		}
		c.AcquireTimeout = d
		return nil
	}
}

// WithHealthCheckFreq sets the health check frequency.
func WithHealthCheckFreq(d time.Duration) PoolOption {
	return func(c *PoolConfig) error {
		if d < 0 {
			return api.NewInvalidInputError("HealthCheckFreq cannot be negative")
		}
		c.HealthCheckFreq = d
		return nil
	}
}

// WithFactory sets the connection factory.
func WithFactory(f ConnectionFactory) PoolOption {
	return func(c *PoolConfig) error {
		if f == nil {
			return api.NewRequiredFieldError("Factory")
		}
		c.Factory = f
		return nil
	}
}

// WithHealthChecker sets the health check function.
func WithHealthChecker(h HealthCheckFunc) PoolOption {
	return func(c *PoolConfig) error {
		c.HealthChecker = h
		return nil
	}
}
