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

//go:build !l2_redis

// This file provides stub implementations when Redis L2 cache support is not compiled in.
// To enable Redis support, build with: go build -tags l2_redis

package cache

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// SecretsFetcher defines the interface for retrieving secrets from QuicBaaS's
// internal secrets management system. This abstraction allows the Redis client
// to securely retrieve credentials without hardcoding them in configuration.
type SecretsFetcher interface {
	// GetSecret retrieves a secret value by path.
	// The path typically follows a hierarchical structure like "redis/password".
	// Returns an error if the secret does not exist or cannot be retrieved.
	GetSecret(ctx context.Context, path string) ([]byte, error)
}

// RedisL2Config defines configuration for the Redis L2 cache.
// This is a stub - Redis support is not compiled in.
type RedisL2Config struct {
	// Addr is the Redis server address (host:port)
	Addr string

	// Password is the Redis password for authentication
	// If SecretsFetcher is provided, this field will be ignored
	Password string

	// DB is the Redis database number to use (0-15)
	DB int

	// Username for Redis 6+ ACL authentication
	Username string

	// TLSConfig enables TLS for the Redis connection
	TLSConfig *tls.Config

	// SecretsFetcher provides a way to retrieve secrets from QuicBaaS's
	// internal secrets management system instead of using plain text password
	SecretsFetcher SecretsFetcher

	// SecretPath is the path to the Redis password secret in QuicBaaS's secrets store
	// Only used if SecretsFetcher is provided
	SecretPath string

	// Connection pool settings
	PoolSize     int           // Maximum number of socket connections
	MinIdleConns int           // Minimum number of idle connections
	MaxIdleConns int           // Maximum number of idle connections
	PoolTimeout  time.Duration // Connection pool timeout

	// Timeouts
	DialTimeout  time.Duration // Dial timeout for new connections
	ReadTimeout  time.Duration // Timeout for socket reads
	WriteTimeout time.Duration // Timeout for socket writes

	// Retry settings
	MaxRetries      int           // Maximum number of retries before giving up
	MinRetryBackoff time.Duration // Minimum backoff between retries
	MaxRetryBackoff time.Duration // Maximum backoff between retries
}

// DefaultRedisL2Config returns the default Redis L2 cache configuration.
// This is a stub - Redis support is not compiled in.
func DefaultRedisL2Config() RedisL2Config {
	return RedisL2Config{
		Addr:            "localhost:6379",
		DB:              0,
		PoolSize:        10,
		MinIdleConns:    2,
		MaxIdleConns:    5,
		PoolTimeout:     5 * time.Second,
		DialTimeout:     5 * time.Second,
		ReadTimeout:     3 * time.Second,
		WriteTimeout:    3 * time.Second,
		MaxRetries:      3,
		MinRetryBackoff: 8 * time.Millisecond,
		MaxRetryBackoff: 512 * time.Millisecond,
	}
}

// Validate checks if the Redis configuration is valid.
// This is a stub - Redis support is not compiled in.
func (c *RedisL2Config) Validate() error {
	return errors.New(errors.ErrNotImplemented, "RedisL2Config.Validate", "Redis L2 cache support not compiled in (build with -tags l2_redis)")
}

// RedisL2Cache implements the L2Cache interface using Redis.
// This is a stub - Redis support is not compiled in.
type RedisL2Cache struct{}

// NewRedisL2Cache creates a new Redis-backed L2 cache.
// This is a stub - Redis support is not compiled in.
// Build with -tags l2_redis to enable Redis support.
func NewRedisL2Cache(ctx context.Context, config RedisL2Config, logger interfaces.Logger) (*RedisL2Cache, error) {
	return nil, errors.New(errors.ErrNotImplemented, "NewRedisL2Cache", "Redis L2 cache support not compiled in (build with -tags l2_redis)")
}

// Get retrieves a value from Redis by key.
// This is a stub - Redis support is not compiled in.
func (r *RedisL2Cache) Get(ctx context.Context, key string) ([]byte, error) {
	return nil, errors.New(errors.ErrNotImplemented, "RedisL2Cache.Get", "Redis L2 cache support not compiled in (build with -tags l2_redis)")
}

// Set stores a value in Redis with the specified TTL.
// This is a stub - Redis support is not compiled in.
func (r *RedisL2Cache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return errors.New(errors.ErrNotImplemented, "RedisL2Cache.Set", "Redis L2 cache support not compiled in (build with -tags l2_redis)")
}

// Delete removes a value from Redis by key.
// This is a stub - Redis support is not compiled in.
func (r *RedisL2Cache) Delete(ctx context.Context, key string) error {
	return errors.New(errors.ErrNotImplemented, "RedisL2Cache.Delete", "Redis L2 cache support not compiled in (build with -tags l2_redis)")
}

// Ping checks if Redis is accessible.
// This is a stub - Redis support is not compiled in.
func (r *RedisL2Cache) Ping(ctx context.Context) error {
	return errors.New(errors.ErrNotImplemented, "RedisL2Cache.Ping", "Redis L2 cache support not compiled in (build with -tags l2_redis)")
}

// Close closes the Redis connection.
// This is a stub - Redis support is not compiled in.
func (r *RedisL2Cache) Close() error {
	return nil
}

// RedisPoolStats is a stub for Redis pool statistics.
// This is a stub - Redis support is not compiled in.
type RedisPoolStats struct {
	Hits       uint32
	Misses     uint32
	Timeouts   uint32
	TotalConns uint32
	IdleConns  uint32
	StaleConns uint32
}

// PoolStats returns connection pool statistics.
// This is a stub - Redis support is not compiled in.
func (r *RedisL2Cache) PoolStats() *RedisPoolStats {
	return nil
}

// FlushDB removes all keys from the current database.
// This is a stub - Redis support is not compiled in.
func (r *RedisL2Cache) FlushDB(ctx context.Context) error {
	return errors.New(errors.ErrNotImplemented, "RedisL2Cache.FlushDB", "Redis L2 cache support not compiled in (build with -tags l2_redis)")
}

// DBSize returns the number of keys in the current database.
// This is a stub - Redis support is not compiled in.
func (r *RedisL2Cache) DBSize(ctx context.Context) (int64, error) {
	return 0, errors.New(errors.ErrNotImplemented, "RedisL2Cache.DBSize", "Redis L2 cache support not compiled in (build with -tags l2_redis)")
}
