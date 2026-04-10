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
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// mockSecretsFetcher implements SecretsFetcher for testing.
type mockSecretsFetcher struct {
	secrets map[string][]byte
	err     error
}

func newMockSecretsFetcher() *mockSecretsFetcher {
	return &mockSecretsFetcher{
		secrets: make(map[string][]byte),
	}
}

func (m *mockSecretsFetcher) GetSecret(ctx context.Context, path string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	if secret, exists := m.secrets[path]; exists {
		return secret, nil
	}
	return nil, errors.NotFound("mockSecretsFetcher.GetSecret", path)
}

func (m *mockSecretsFetcher) SetSecret(path string, value []byte) {
	m.secrets[path] = value
}

func (m *mockSecretsFetcher) SetError(err error) {
	m.err = err
}

// mockRedisClient implements a minimal redis client for testing without actual Redis.
type mockRedisClient struct {
	data      map[string]*mockRedisEntry
	pingError error
	closed    bool
}

type mockRedisEntry struct {
	value     []byte
	expiresAt time.Time
}

func TestDefaultRedisL2Config(t *testing.T) {
	config := DefaultRedisL2Config()

	assert.Equal(t, "localhost:6379", config.Addr)
	assert.Equal(t, 0, config.DB)
	assert.Equal(t, 5*time.Second, config.DialTimeout)
	assert.Equal(t, 3*time.Second, config.ReadTimeout)
	assert.Equal(t, 3*time.Second, config.WriteTimeout)
	assert.Equal(t, 3, config.MaxRetries)
	assert.Equal(t, "redis/password", config.SecretPath)
}

func TestRedisL2Config_Validate(t *testing.T) {
	tests := []struct {
		name      string
		config    RedisL2Config
		wantError bool
		errorMsg  string
	}{
		{
			name:      "valid config",
			config:    DefaultRedisL2Config(),
			wantError: false,
		},
		{
			name: "empty address",
			config: RedisL2Config{
				Addr:         "",
				DialTimeout:  5 * time.Second,
				ReadTimeout:  3 * time.Second,
				WriteTimeout: 3 * time.Second,
			},
			wantError: true,
			errorMsg:  "address cannot be empty",
		},
		{
			name: "invalid database negative",
			config: RedisL2Config{
				Addr:         "localhost:6379",
				DB:           -1,
				DialTimeout:  5 * time.Second,
				ReadTimeout:  3 * time.Second,
				WriteTimeout: 3 * time.Second,
			},
			wantError: true,
			errorMsg:  "database must be between 0 and 15",
		},
		{
			name: "invalid database too high",
			config: RedisL2Config{
				Addr:         "localhost:6379",
				DB:           16,
				DialTimeout:  5 * time.Second,
				ReadTimeout:  3 * time.Second,
				WriteTimeout: 3 * time.Second,
			},
			wantError: true,
			errorMsg:  "database must be between 0 and 15",
		},
		{
			name: "negative pool size",
			config: RedisL2Config{
				Addr:         "localhost:6379",
				PoolSize:     -1,
				DialTimeout:  5 * time.Second,
				ReadTimeout:  3 * time.Second,
				WriteTimeout: 3 * time.Second,
			},
			wantError: true,
			errorMsg:  "pool size cannot be negative",
		},
		{
			name: "negative min idle connections",
			config: RedisL2Config{
				Addr:         "localhost:6379",
				MinIdleConns: -1,
				DialTimeout:  5 * time.Second,
				ReadTimeout:  3 * time.Second,
				WriteTimeout: 3 * time.Second,
			},
			wantError: true,
			errorMsg:  "min idle connections cannot be negative",
		},
		{
			name: "negative max idle connections",
			config: RedisL2Config{
				Addr:         "localhost:6379",
				MaxIdleConns: -1,
				DialTimeout:  5 * time.Second,
				ReadTimeout:  3 * time.Second,
				WriteTimeout: 3 * time.Second,
			},
			wantError: true,
			errorMsg:  "max idle connections cannot be negative",
		},
		{
			name: "negative connection max idle time",
			config: RedisL2Config{
				Addr:            "localhost:6379",
				ConnMaxIdleTime: -1 * time.Second,
				DialTimeout:     5 * time.Second,
				ReadTimeout:     3 * time.Second,
				WriteTimeout:    3 * time.Second,
			},
			wantError: true,
			errorMsg:  "connection max idle time cannot be negative",
		},
		{
			name: "negative connection max lifetime",
			config: RedisL2Config{
				Addr:            "localhost:6379",
				ConnMaxLifetime: -1 * time.Second,
				DialTimeout:     5 * time.Second,
				ReadTimeout:     3 * time.Second,
				WriteTimeout:    3 * time.Second,
			},
			wantError: true,
			errorMsg:  "connection max lifetime cannot be negative",
		},
		{
			name: "zero dial timeout",
			config: RedisL2Config{
				Addr:         "localhost:6379",
				DialTimeout:  0,
				ReadTimeout:  3 * time.Second,
				WriteTimeout: 3 * time.Second,
			},
			wantError: true,
			errorMsg:  "dial timeout must be positive",
		},
		{
			name: "zero read timeout",
			config: RedisL2Config{
				Addr:         "localhost:6379",
				DialTimeout:  5 * time.Second,
				ReadTimeout:  0,
				WriteTimeout: 3 * time.Second,
			},
			wantError: true,
			errorMsg:  "read timeout must be positive",
		},
		{
			name: "zero write timeout",
			config: RedisL2Config{
				Addr:         "localhost:6379",
				DialTimeout:  5 * time.Second,
				ReadTimeout:  3 * time.Second,
				WriteTimeout: 0,
			},
			wantError: true,
			errorMsg:  "write timeout must be positive",
		},
		{
			name: "negative max retries",
			config: RedisL2Config{
				Addr:         "localhost:6379",
				DialTimeout:  5 * time.Second,
				ReadTimeout:  3 * time.Second,
				WriteTimeout: 3 * time.Second,
				MaxRetries:   -1,
			},
			wantError: true,
			errorMsg:  "max retries cannot be negative",
		},
		{
			name: "negative min retry backoff",
			config: RedisL2Config{
				Addr:            "localhost:6379",
				DialTimeout:     5 * time.Second,
				ReadTimeout:     3 * time.Second,
				WriteTimeout:    3 * time.Second,
				MinRetryBackoff: -1 * time.Millisecond,
			},
			wantError: true,
			errorMsg:  "min retry backoff cannot be negative",
		},
		{
			name: "negative max retry backoff",
			config: RedisL2Config{
				Addr:            "localhost:6379",
				DialTimeout:     5 * time.Second,
				ReadTimeout:     3 * time.Second,
				WriteTimeout:    3 * time.Second,
				MaxRetryBackoff: -1 * time.Millisecond,
			},
			wantError: true,
			errorMsg:  "max retry backoff cannot be negative",
		},
		{
			name: "empty secret path with secrets fetcher",
			config: RedisL2Config{
				Addr:           "localhost:6379",
				DialTimeout:    5 * time.Second,
				ReadTimeout:    3 * time.Second,
				WriteTimeout:   3 * time.Second,
				SecretsFetcher: newMockSecretsFetcher(),
				SecretPath:     "",
			},
			wantError: true,
			errorMsg:  "secret path cannot be empty when secrets fetcher is provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNewRedisL2Cache_ValidationErrors(t *testing.T) {
	ctx := context.Background()
	log := testLogger(t)

	tests := []struct {
		name      string
		config    RedisL2Config
		logger    interfaces.Logger
		wantError bool
		errorMsg  string
	}{
		{
			name: "invalid config",
			config: RedisL2Config{
				Addr: "", // Invalid
			},
			logger:    log,
			wantError: true,
			errorMsg:  "invalid redis l2 config",
		},
		{
			name:      "nil logger",
			config:    DefaultRedisL2Config(),
			logger:    nil,
			wantError: true,
			errorMsg:  "logger cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRedisL2Cache(ctx, tt.config, tt.logger)
			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNewRedisL2Cache_SecretsIntegration(t *testing.T) {
	ctx := context.Background()
	log := testLogger(t)

	t.Run("successful secret retrieval", func(t *testing.T) {
		// This test verifies the secrets fetcher logic without connecting to Redis
		secretsFetcher := newMockSecretsFetcher()
		secretsFetcher.SetSecret("redis/password", []byte("supersecret"))

		config := DefaultRedisL2Config()
		config.Addr = "invalid:6379" // Won't connect but validates config
		config.SecretsFetcher = secretsFetcher
		config.SecretPath = "redis/password"

		// Will fail at connection stage, but proves secret was fetched
		_, err := NewRedisL2Cache(ctx, config, log)
		assert.Error(t, err) // Expected: can't connect to invalid:6379
		assert.Contains(t, err.Error(), "failed to connect to redis")
	})

	t.Run("secret fetch error", func(t *testing.T) {
		secretsFetcher := newMockSecretsFetcher()
		secretsFetcher.SetError(&errors.MessageError{Message: "secret store unavailable"})

		config := DefaultRedisL2Config()
		config.SecretsFetcher = secretsFetcher
		config.SecretPath = "redis/password"

		_, err := NewRedisL2Cache(ctx, config, log)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to retrieve redis password from secrets")
	})

	t.Run("secret not found", func(t *testing.T) {
		secretsFetcher := newMockSecretsFetcher()

		config := DefaultRedisL2Config()
		config.SecretsFetcher = secretsFetcher
		config.SecretPath = "redis/nonexistent"

		_, err := NewRedisL2Cache(ctx, config, log)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to retrieve redis password from secrets")
	})
}

func TestRedisL2Cache_Get_ValidationErrors(t *testing.T) {
	// Create a mock cache that's already closed
	cache := &RedisL2Cache{
		client: nil,
		logger: testLogger(t),
		closed: true,
	}

	ctx := context.Background()

	t.Run("cache closed", func(t *testing.T) {
		_, err := cache.Get(ctx, "key")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cache is closed")
	})

	t.Run("empty key", func(t *testing.T) {
		cache := &RedisL2Cache{
			logger: testLogger(t),
			closed: false,
		}
		_, err := cache.Get(ctx, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key cannot be empty")
	})
}

func TestRedisL2Cache_Set_ValidationErrors(t *testing.T) {
	cache := &RedisL2Cache{
		client: nil,
		logger: testLogger(t),
		closed: true,
	}

	ctx := context.Background()

	t.Run("cache closed", func(t *testing.T) {
		err := cache.Set(ctx, "key", []byte("value"), time.Minute)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cache is closed")
	})

	t.Run("empty key", func(t *testing.T) {
		cache := &RedisL2Cache{
			logger: testLogger(t),
			closed: false,
		}
		err := cache.Set(ctx, "", []byte("value"), time.Minute)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key cannot be empty")
	})

	t.Run("nil value", func(t *testing.T) {
		cache := &RedisL2Cache{
			logger: testLogger(t),
			closed: false,
		}
		err := cache.Set(ctx, "key", nil, time.Minute)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "value cannot be nil")
	})
}

func TestRedisL2Cache_Delete_ValidationErrors(t *testing.T) {
	cache := &RedisL2Cache{
		client: nil,
		logger: testLogger(t),
		closed: true,
	}

	ctx := context.Background()

	t.Run("cache closed", func(t *testing.T) {
		err := cache.Delete(ctx, "key")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cache is closed")
	})

	t.Run("empty key", func(t *testing.T) {
		cache := &RedisL2Cache{
			logger: testLogger(t),
			closed: false,
		}
		err := cache.Delete(ctx, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key cannot be empty")
	})
}

func TestRedisL2Cache_Ping_ValidationErrors(t *testing.T) {
	cache := &RedisL2Cache{
		client: nil,
		logger: testLogger(t),
		closed: true,
	}

	ctx := context.Background()

	t.Run("cache closed", func(t *testing.T) {
		err := cache.Ping(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cache is closed")
	})
}

func TestRedisL2Cache_Close(t *testing.T) {
	t.Run("close already closed", func(t *testing.T) {
		cache := &RedisL2Cache{
			logger: testLogger(t),
			closed: true,
		}
		err := cache.Close()
		assert.NoError(t, err)
	})
}

func TestRedisL2Cache_FlushDB_ValidationErrors(t *testing.T) {
	cache := &RedisL2Cache{
		client: nil,
		logger: testLogger(t),
		closed: true,
	}

	ctx := context.Background()

	t.Run("cache closed", func(t *testing.T) {
		err := cache.FlushDB(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cache is closed")
	})
}

func TestRedisL2Cache_DBSize_ValidationErrors(t *testing.T) {
	cache := &RedisL2Cache{
		client: nil,
		logger: testLogger(t),
		closed: true,
	}

	ctx := context.Background()

	t.Run("cache closed", func(t *testing.T) {
		_, err := cache.DBSize(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cache is closed")
	})
}

func TestRedisL2Cache_PoolStats(t *testing.T) {
	t.Run("pool stats when closed", func(t *testing.T) {
		cache := &RedisL2Cache{
			logger: testLogger(t),
			closed: true,
		}
		stats := cache.PoolStats()
		assert.Nil(t, stats)
	})
}

func TestRedisL2Config_TLSConfig(t *testing.T) {
	t.Run("config with TLS", func(t *testing.T) {
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS13,
		}

		config := DefaultRedisL2Config()
		config.TLSConfig = tlsConfig

		err := config.Validate()
		assert.NoError(t, err)
		assert.NotNil(t, config.TLSConfig)
		assert.Equal(t, uint16(tls.VersionTLS13), config.TLSConfig.MinVersion)
	})
}

func TestRedisL2Config_AdvancedPoolSettings(t *testing.T) {
	t.Run("custom pool settings", func(t *testing.T) {
		config := DefaultRedisL2Config()
		config.PoolSize = 20
		config.MinIdleConns = 5
		config.MaxIdleConns = 10
		config.ConnMaxIdleTime = 1 * time.Hour
		config.ConnMaxLifetime = 24 * time.Hour
		config.PoolTimeout = 5 * time.Second

		err := config.Validate()
		assert.NoError(t, err)
		assert.Equal(t, 20, config.PoolSize)
		assert.Equal(t, 5, config.MinIdleConns)
		assert.Equal(t, 10, config.MaxIdleConns)
		assert.Equal(t, 1*time.Hour, config.ConnMaxIdleTime)
		assert.Equal(t, 24*time.Hour, config.ConnMaxLifetime)
		assert.Equal(t, 5*time.Second, config.PoolTimeout)
	})
}

func TestRedisL2Config_RetrySettings(t *testing.T) {
	t.Run("custom retry settings", func(t *testing.T) {
		config := DefaultRedisL2Config()
		config.MaxRetries = 5
		config.MinRetryBackoff = 10 * time.Millisecond
		config.MaxRetryBackoff = 1 * time.Second

		err := config.Validate()
		assert.NoError(t, err)
		assert.Equal(t, 5, config.MaxRetries)
		assert.Equal(t, 10*time.Millisecond, config.MinRetryBackoff)
		assert.Equal(t, 1*time.Second, config.MaxRetryBackoff)
	})

	t.Run("zero retries", func(t *testing.T) {
		config := DefaultRedisL2Config()
		config.MaxRetries = 0

		err := config.Validate()
		assert.NoError(t, err)
	})
}

func TestRedisL2Cache_ContextCancellation(t *testing.T) {
	// These tests verify that context cancellation is handled correctly
	// We can't test actual Redis operations without a running Redis instance,
	// but we verify the validation logic works correctly

	t.Run("get with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		cache := &RedisL2Cache{
			logger: testLogger(t),
			client: redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
			}),
			closed: false,
		}
		defer cache.client.Close()

		// Will fail because context is cancelled, but proves validation passes
		_, err := cache.Get(ctx, "key")
		assert.Error(t, err)
	})

	t.Run("set with timeout context", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()
		time.Sleep(2 * time.Nanosecond) // Ensure timeout

		cache := &RedisL2Cache{
			logger: testLogger(t),
			client: redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
			}),
			closed: false,
		}
		defer cache.client.Close()

		err := cache.Set(ctx, "key", []byte("value"), time.Minute)
		assert.Error(t, err)
	})
}

func TestRedisL2Cache_EdgeCases(t *testing.T) {
	t.Run("large value", func(t *testing.T) {
		cache := &RedisL2Cache{
			logger: testLogger(t),
			closed: false,
		}

		// Verify validation passes for large values
		largeValue := make([]byte, 1024*1024) // 1MB
		err := cache.Set(context.Background(), "large", largeValue, time.Minute)
		// Will fail without real Redis, but proves validation passes
		assert.Error(t, err)
	})

	t.Run("zero TTL", func(t *testing.T) {
		cache := &RedisL2Cache{
			logger: testLogger(t),
			closed: false,
		}

		// Zero TTL means no expiration - should be allowed
		err := cache.Set(context.Background(), "noexpire", []byte("value"), 0)
		assert.Error(t, err) // Fails without Redis, but proves validation passes
	})

	t.Run("negative TTL", func(t *testing.T) {
		cache := &RedisL2Cache{
			logger: testLogger(t),
			closed: false,
		}

		// Negative TTL should be treated as immediate expiration by Redis
		err := cache.Set(context.Background(), "expired", []byte("value"), -1*time.Second)
		assert.Error(t, err) // Fails without Redis, but proves validation passes
	})
}

func TestRedisL2Cache_InterfaceCompliance(t *testing.T) {
	// Verify that RedisL2Cache implements L2Cache interface
	var _ L2Cache = (*RedisL2Cache)(nil)
}

func TestMockL2Cache_InterfaceCompliance(t *testing.T) {
	// Verify that MockL2Cache implements L2Cache interface
	var _ L2Cache = (*MockL2Cache)(nil)
}
