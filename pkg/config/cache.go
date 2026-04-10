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

// Package config provides configuration structures for QuicBaaS cache system.
package config

import (
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// Well-known internal secret path for Redis password.
// This constant references the path defined in pkg/secrets/internal_store.go.
const (
	InternalSecretRedisPassword = "cache/redis/password"
)

// EntityCacheConfig defines cache configuration for a specific entity type.
type EntityCacheConfig struct {
	// Enabled controls whether caching is enabled for this entity type
	Enabled bool `mapstructure:"enabled"`

	// TTL is the time-to-live for cached entries of this entity type
	// If not set, the default TTL from CacheConfig will be used
	TTL time.Duration `mapstructure:"ttl"`

	// MaxSize is the maximum number of entries to cache for this entity type
	// If not set, the cache will use a proportional share of the global max_size
	MaxSize int `mapstructure:"max_size"`

	// WriteThrough controls whether writes to this entity type should be
	// immediately written through to L2 cache
	WriteThrough *bool `mapstructure:"write_through"`
}

// EnhancedCacheConfig provides entity-level cache configuration support.
type EnhancedCacheConfig struct {
	// Enabled globally enables or disables the cache system
	Enabled bool `mapstructure:"enabled"`

	// DefaultTTL is the default time-to-live for cached entries
	DefaultTTL time.Duration `mapstructure:"default_ttl"`

	// MaxSize is the global maximum number of entries across all entity types
	MaxSize int `mapstructure:"max_size"`

	// CleanupInterval is how often expired entries should be cleaned up
	CleanupInterval time.Duration `mapstructure:"cleanup_interval"`

	// WriteThrough controls the default write-through behavior
	WriteThrough bool `mapstructure:"write_through"`

	// L2Enabled enables the L2 (Redis) cache layer
	L2Enabled bool `mapstructure:"l2_enabled"`

	// L2TTL is the time-to-live for L2 cache entries
	L2TTL time.Duration `mapstructure:"l2_ttl"`

	// L2RedisAddr is the Redis server address for L2 cache
	L2RedisAddr string `mapstructure:"l2_redis_addr"`

	// L2RedisPassword is the Redis password (plain text).
	// DEPRECATED: Use L2RedisUseSecrets=true instead for secure credential management.
	// This field is maintained for backward compatibility but should not be used
	// in production deployments. Consider migrating to the internal secrets store.
	L2RedisPassword string `mapstructure:"l2_redis_password"`

	// L2RedisDB is the Redis database number
	L2RedisDB int `mapstructure:"l2_redis_db"`

	// L2RedisUseSecrets controls whether to use the internal secrets store
	// for Redis credentials instead of plain text L2RedisPassword.
	// When true, the Redis password will be retrieved from the path specified
	// in L2RedisSecretPath using QuicBaaS's internal secrets management system.
	L2RedisUseSecrets bool `mapstructure:"l2_redis_use_secrets"`

	// L2RedisSecretPath is the path in the internal secrets store where the
	// Redis password is stored. This is only used when L2RedisUseSecrets is true.
	// Default: "cache/redis/password" (InternalSecretRedisPassword constant)
	L2RedisSecretPath string `mapstructure:"l2_redis_secret_path"`

	// Entities maps entity type names to their specific cache configurations
	// Example: "user", "post", "comment"
	Entities map[string]EntityCacheConfig `mapstructure:"entities"`
}

// DefaultEnhancedCacheConfig returns the default enhanced cache configuration.
func DefaultEnhancedCacheConfig() EnhancedCacheConfig {
	return EnhancedCacheConfig{
		Enabled:           true,
		DefaultTTL:        5 * time.Minute,
		MaxSize:           10000,
		CleanupInterval:   1 * time.Minute,
		WriteThrough:      true,
		L2Enabled:         false,
		L2TTL:             30 * time.Minute,
		L2RedisDB:         0,
		L2RedisUseSecrets: false,
		L2RedisSecretPath: InternalSecretRedisPassword,
		Entities:          make(map[string]EntityCacheConfig),
	}
}

// Validate checks if the enhanced cache configuration is valid.
func (c *EnhancedCacheConfig) Validate() error {
	if c.MaxSize < 0 {
		return api.NewNonNegativeValueRequiredError("config.EnhancedCacheConfig.Validate", "cache max_size")
	}

	if c.DefaultTTL < 0 {
		return api.NewNonNegativeValueRequiredError("config.EnhancedCacheConfig.Validate", "cache default_ttl")
	}

	if c.CleanupInterval <= 0 {
		return api.NewPositiveValueRequiredError("config.EnhancedCacheConfig.Validate", "cache cleanup_interval")
	}

	if c.L2Enabled {
		if c.L2RedisAddr == "" {
			return api.NewEmptyWhenEnabledError("config.EnhancedCacheConfig.Validate", "l2_redis_addr", "L2 cache")
		}
		if c.L2TTL < 0 {
			return api.NewNonNegativeValueRequiredError("config.EnhancedCacheConfig.Validate", "l2_ttl")
		}
		if c.L2RedisDB < 0 {
			return api.NewNonNegativeValueRequiredError("config.EnhancedCacheConfig.Validate", "l2_redis_db")
		}

		// Validate Redis authentication configuration:
		// When using internal secrets, require a valid secret path.
		// When not using secrets, plain text password may be empty for no-auth Redis.
		if c.L2RedisUseSecrets && c.L2RedisSecretPath == "" {
			return api.NewRequiredWhenEnabledError("config.EnhancedCacheConfig.Validate",
				"l2_redis_secret_path", "l2_redis_use_secrets")
		}
	}

	// Validate entity-specific configurations
	for entityType, entityConfig := range c.Entities {
		if err := c.validateEntityConfig(entityType, entityConfig); err != nil {
			return api.NewEntityConfigValidationError("config.EnhancedCacheConfig.Validate", entityType, err)
		}
	}

	return nil
}

// validateEntityConfig validates an entity-specific cache configuration.
func (c *EnhancedCacheConfig) validateEntityConfig(entityType string, cfg EntityCacheConfig) error {
	if entityType == "" {
		return api.NewRequiredFieldError("entity type")
	}

	if cfg.TTL < 0 {
		return api.NewNonNegativeValueRequiredError("config.EnhancedCacheConfig.validateEntityConfig", "ttl")
	}

	if cfg.MaxSize < 0 {
		return api.NewNonNegativeValueRequiredError("config.EnhancedCacheConfig.validateEntityConfig", "max_size")
	}

	return nil
}

// GetEntityConfig returns the cache configuration for a specific entity type.
// If no specific configuration exists, it returns a default configuration based on global settings.
func (c *EnhancedCacheConfig) GetEntityConfig(entityType string) EntityCacheConfig {
	if cfg, exists := c.Entities[entityType]; exists {
		// Fill in defaults for unset values
		return c.mergeWithDefaults(cfg)
	}

	// Return default configuration for this entity
	return c.defaultEntityConfig()
}

// mergeWithDefaults fills in default values for unset entity config fields.
func (c *EnhancedCacheConfig) mergeWithDefaults(cfg EntityCacheConfig) EntityCacheConfig {
	merged := cfg

	// Use default TTL if not set
	if cfg.TTL == 0 {
		merged.TTL = c.DefaultTTL
	}

	// Use default write-through if not set
	if cfg.WriteThrough == nil {
		merged.WriteThrough = &c.WriteThrough
	}

	// MaxSize remains as configured (0 means use proportional allocation)

	return merged
}

// defaultEntityConfig returns a default entity configuration based on global settings.
func (c *EnhancedCacheConfig) defaultEntityConfig() EntityCacheConfig {
	return EntityCacheConfig{
		Enabled:      c.Enabled,
		TTL:          c.DefaultTTL,
		MaxSize:      0, // 0 means use proportional allocation
		WriteThrough: &c.WriteThrough,
	}
}

// IsEntityCacheEnabled returns whether caching is enabled for a specific entity type.
func (c *EnhancedCacheConfig) IsEntityCacheEnabled(entityType string) bool {
	if !c.Enabled {
		return false
	}

	cfg := c.GetEntityConfig(entityType)
	return cfg.Enabled
}

// GetEntityTTL returns the TTL for a specific entity type.
func (c *EnhancedCacheConfig) GetEntityTTL(entityType string) time.Duration {
	cfg := c.GetEntityConfig(entityType)
	if cfg.TTL > 0 {
		return cfg.TTL
	}
	return c.DefaultTTL
}

// GetEntityMaxSize returns the max size for a specific entity type.
func (c *EnhancedCacheConfig) GetEntityMaxSize(entityType string) int {
	cfg := c.GetEntityConfig(entityType)
	if cfg.MaxSize > 0 {
		return cfg.MaxSize
	}
	// Return a proportional share of the global max size
	// If no entities are configured, use the global max size
	if len(c.Entities) == 0 {
		return c.MaxSize
	}
	// Simple proportional allocation
	return c.MaxSize / (len(c.Entities) + 1)
}

// GetEntityWriteThrough returns the write-through setting for a specific entity type.
func (c *EnhancedCacheConfig) GetEntityWriteThrough(entityType string) bool {
	cfg := c.GetEntityConfig(entityType)
	if cfg.WriteThrough != nil {
		return *cfg.WriteThrough
	}
	return c.WriteThrough
}

// Clone creates a deep copy of the enhanced cache configuration.
func (c *EnhancedCacheConfig) Clone() *EnhancedCacheConfig {
	if c == nil {
		return nil
	}

	clone := *c

	// Deep copy the entities map
	clone.Entities = make(map[string]EntityCacheConfig, len(c.Entities))
	for k, v := range c.Entities {
		entityClone := v
		// Deep copy the WriteThrough pointer
		if v.WriteThrough != nil {
			wt := *v.WriteThrough
			entityClone.WriteThrough = &wt
		}
		clone.Entities[k] = entityClone
	}

	return &clone
}
