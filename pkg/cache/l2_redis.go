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

//go:build l2_redis

package cache

import (
	"context"
	"crypto/tls"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

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
type RedisL2Config struct {
	// Addr is the Redis server address (host:port)
	Addr string

	// Password is the Redis password for authentication
	// If SecretsFetcher is provided, this field will be ignored
	Password string

	// DB is the Redis database number to use (0-15)
	DB int

	// PoolSize is the maximum number of socket connections
	// Default: 10 per CPU
	PoolSize int

	// MinIdleConns is the minimum number of idle connections
	// Default: 0
	MinIdleConns int

	// MaxIdleConns is the maximum number of idle connections
	// Default: 0 (no limit)
	MaxIdleConns int

	// ConnMaxIdleTime is the maximum amount of time a connection may be idle
	// Default: 30 minutes
	ConnMaxIdleTime time.Duration

	// ConnMaxLifetime is the maximum lifetime of a connection
	// Default: 0 (no limit)
	ConnMaxLifetime time.Duration

	// DialTimeout is the timeout for establishing new connections
	// Default: 5 seconds
	DialTimeout time.Duration

	// ReadTimeout is the timeout for socket reads
	// Default: 3 seconds
	ReadTimeout time.Duration

	// WriteTimeout is the timeout for socket writes
	// Default: 3 seconds
	WriteTimeout time.Duration

	// PoolTimeout is the amount of time client waits for connection if all are busy
	// Default: ReadTimeout + 1 second
	PoolTimeout time.Duration

	// MaxRetries is the maximum number of retries before giving up
	// Default: 3
	MaxRetries int

	// MinRetryBackoff is the minimum backoff between retries
	// Default: 8 milliseconds
	MinRetryBackoff time.Duration

	// MaxRetryBackoff is the maximum backoff between retries
	// Default: 512 milliseconds
	MaxRetryBackoff time.Duration

	// TLSConfig specifies the TLS configuration for secure connections
	// Optional: nil disables TLS
	TLSConfig *tls.Config

	// SecretsFetcher is an optional interface for retrieving the Redis password
	// from QuicBaaS's internal secrets system. If provided, it will be used
	// instead of the Password field.
	SecretsFetcher SecretsFetcher

	// SecretPath is the path to the Redis password in the secrets system
	// Only used if SecretsFetcher is provided
	// Default: "redis/password"
	SecretPath string
}

// DefaultRedisL2Config returns a Redis L2 cache configuration with sensible defaults.
func DefaultRedisL2Config() RedisL2Config {
	return RedisL2Config{
		Addr:            "localhost:6379",
		DB:              0,
		PoolSize:        0, // 10 per CPU
		MinIdleConns:    0,
		MaxIdleConns:    0,
		ConnMaxIdleTime: 30 * time.Minute,
		ConnMaxLifetime: 0,
		DialTimeout:     5 * time.Second,
		ReadTimeout:     3 * time.Second,
		WriteTimeout:    3 * time.Second,
		PoolTimeout:     0, // ReadTimeout + 1 second
		MaxRetries:      3,
		MinRetryBackoff: 8 * time.Millisecond,
		MaxRetryBackoff: 512 * time.Millisecond,
		SecretPath:      "redis/password",
	}
}

// Validate checks if the Redis L2 configuration is valid.
func (c *RedisL2Config) Validate() error {
	if c.Addr == "" {
		return errors.InvalidInput("RedisL2Config.Validate", "address cannot be empty")
	}
	if c.DB < 0 || c.DB > 15 {
		return errors.InvalidInput("RedisL2Config.Validate", "database must be between 0 and 15")
	}
	if c.PoolSize < 0 {
		return errors.InvalidInput("RedisL2Config.Validate", "pool size cannot be negative")
	}
	if c.MinIdleConns < 0 {
		return errors.InvalidInput("RedisL2Config.Validate", "min idle connections cannot be negative")
	}
	if c.MaxIdleConns < 0 {
		return errors.InvalidInput("RedisL2Config.Validate", "max idle connections cannot be negative")
	}
	if c.ConnMaxIdleTime < 0 {
		return errors.InvalidInput("RedisL2Config.Validate", "connection max idle time cannot be negative")
	}
	if c.ConnMaxLifetime < 0 {
		return errors.InvalidInput("RedisL2Config.Validate", "connection max lifetime cannot be negative")
	}
	if c.DialTimeout <= 0 {
		return errors.InvalidInput("RedisL2Config.Validate", "dial timeout must be positive")
	}
	if c.ReadTimeout <= 0 {
		return errors.InvalidInput("RedisL2Config.Validate", "read timeout must be positive")
	}
	if c.WriteTimeout <= 0 {
		return errors.InvalidInput("RedisL2Config.Validate", "write timeout must be positive")
	}
	if c.MaxRetries < 0 {
		return errors.InvalidInput("RedisL2Config.Validate", "max retries cannot be negative")
	}
	if c.MinRetryBackoff < 0 {
		return errors.InvalidInput("RedisL2Config.Validate", "min retry backoff cannot be negative")
	}
	if c.MaxRetryBackoff < 0 {
		return errors.InvalidInput("RedisL2Config.Validate", "max retry backoff cannot be negative")
	}
	if c.SecretsFetcher != nil && c.SecretPath == "" {
		return errors.InvalidInput("RedisL2Config.Validate", "secret path cannot be empty when secrets fetcher is provided")
	}
	return nil
}

// RedisL2Cache implements the L2Cache interface using Redis.
type RedisL2Cache struct {
	client *redis.Client
	config RedisL2Config
	logger interfaces.Logger
	mu     sync.RWMutex
	closed bool
}

// NewRedisL2Cache creates a new Redis L2 cache instance.
func NewRedisL2Cache(ctx context.Context, config RedisL2Config, logger interfaces.Logger) (*RedisL2Cache, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.Wrapf(err, errors.ErrInvalidInput, "NewRedisL2Cache", "invalid redis l2 config")
	}

	if logger == nil {
		return nil, errors.InvalidInput("NewRedisL2Cache", "logger cannot be nil")
	}

	// Retrieve password from secrets if fetcher is provided
	password := config.Password
	if config.SecretsFetcher != nil {
		secretBytes, err := config.SecretsFetcher.GetSecret(ctx, config.SecretPath)
		if err != nil {
			return nil, errors.Wrapf(err, errors.ErrInternal, "NewRedisL2Cache", "failed to retrieve redis password from secrets")
		}
		password = string(secretBytes)
	}

	// Create Redis client options
	opts := &redis.Options{
		Addr:            config.Addr,
		Password:        password,
		DB:              config.DB,
		PoolSize:        config.PoolSize,
		MinIdleConns:    config.MinIdleConns,
		MaxIdleConns:    config.MaxIdleConns,
		ConnMaxIdleTime: config.ConnMaxIdleTime,
		ConnMaxLifetime: config.ConnMaxLifetime,
		DialTimeout:     config.DialTimeout,
		ReadTimeout:     config.ReadTimeout,
		WriteTimeout:    config.WriteTimeout,
		PoolTimeout:     config.PoolTimeout,
		MaxRetries:      config.MaxRetries,
		MinRetryBackoff: config.MinRetryBackoff,
		MaxRetryBackoff: config.MaxRetryBackoff,
		TLSConfig:       config.TLSConfig,
	}

	client := redis.NewClient(opts)

	// Verify connection
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, errors.Wrapf(err, errors.ErrUnavailable, "NewRedisL2Cache", "failed to connect to redis")
	}

	l2 := &RedisL2Cache{
		client: client,
		config: config,
		logger: logger,
	}

	logger.WithFields(map[string]interface{}{
		"addr": config.Addr,
		"db":   config.DB,
	}).Info("Redis L2 cache initialized")

	return l2, nil
}

// Get retrieves a value from Redis by key.
func (r *RedisL2Cache) Get(ctx context.Context, key string) ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.closed {
		return nil, errors.Internal("RedisL2Cache.Get", "cache is closed")
	}

	if key == "" {
		return nil, errors.InvalidInput("RedisL2Cache.Get", "key cannot be empty")
	}

	result, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, errors.NotFound("RedisL2Cache.Get", key)
		}
		return nil, errors.Wrapf(err, errors.ErrInternal, "RedisL2Cache.Get", "redis get failed")
	}

	r.logger.WithField("key", key).Debug("Redis L2 cache hit")
	return result, nil
}

// Set stores a value in Redis with the specified TTL.
func (r *RedisL2Cache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.closed {
		return errors.Internal("RedisL2Cache.Set", "cache is closed")
	}

	if key == "" {
		return errors.InvalidInput("RedisL2Cache.Set", "key cannot be empty")
	}

	if value == nil {
		return errors.InvalidInput("RedisL2Cache.Set", "value cannot be nil")
	}

	if r.client == nil {
		return errors.Internal("RedisL2Cache.Set", "client not initialized")
	}

	if err := r.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "RedisL2Cache.Set", "redis set failed")
	}

	r.logger.WithFields(map[string]interface{}{
		"key": key,
		"ttl": ttl,
	}).Debug("Redis L2 cache set")

	return nil
}

// Delete removes a value from Redis by key.
func (r *RedisL2Cache) Delete(ctx context.Context, key string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.closed {
		return errors.Internal("RedisL2Cache.Delete", "cache is closed")
	}

	if key == "" {
		return errors.InvalidInput("RedisL2Cache.Delete", "key cannot be empty")
	}

	if err := r.client.Del(ctx, key).Err(); err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "RedisL2Cache.Delete", "redis delete failed")
	}

	r.logger.WithField("key", key).Debug("Redis L2 cache delete")
	return nil
}

// Ping checks the health and connectivity of Redis.
func (r *RedisL2Cache) Ping(ctx context.Context) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.closed {
		return errors.Internal("RedisL2Cache.Ping", "cache is closed")
	}

	if err := r.client.Ping(ctx).Err(); err != nil {
		return errors.Wrapf(err, errors.ErrUnavailable, "RedisL2Cache.Ping", "redis ping failed")
	}

	return nil
}

// Close closes the Redis connection and releases resources.
func (r *RedisL2Cache) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	r.closed = true

	if err := r.client.Close(); err != nil {
		r.logger.WithError(err).Error("Failed to close Redis client")
		return errors.Wrapf(err, errors.ErrInternal, "RedisL2Cache.Close", "failed to close redis client")
	}

	r.logger.Info("Redis L2 cache closed")
	return nil
}

// PoolStats returns Redis connection pool statistics.
func (r *RedisL2Cache) PoolStats() *redis.PoolStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.closed {
		return nil
	}

	return r.client.PoolStats()
}

// FlushDB removes all keys from the current database.
// Use with caution - this is a destructive operation.
func (r *RedisL2Cache) FlushDB(ctx context.Context) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.closed {
		return errors.Internal("RedisL2Cache.FlushDB", "cache is closed")
	}

	if err := r.client.FlushDB(ctx).Err(); err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "RedisL2Cache.FlushDB", "redis flush db failed")
	}

	r.logger.Warn("Redis database flushed")
	return nil
}

// DBSize returns the number of keys in the current database.
func (r *RedisL2Cache) DBSize(ctx context.Context) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.closed {
		return 0, errors.Internal("RedisL2Cache.DBSize", "cache is closed")
	}

	size, err := r.client.DBSize(ctx).Result()
	if err != nil {
		return 0, errors.Wrapf(err, errors.ErrInternal, "RedisL2Cache.DBSize", "redis db size failed")
	}

	return size, nil
}
