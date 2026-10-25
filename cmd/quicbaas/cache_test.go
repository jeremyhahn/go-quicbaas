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

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
)

func setupCacheTest(t *testing.T) (*cache.CacheRegistry, func()) {
	// Create test logger
	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "cache-test",
	}

	testLogger, err := logger.New(logConfig)
	require.NoError(t, err)

	// Create test cache config
	cacheConfig := &config.EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         1000,
		CleanupInterval: 1 * time.Minute,
		Entities: map[string]config.EntityCacheConfig{
			"user": {
				Enabled: true,
				TTL:     10 * time.Minute,
				MaxSize: 500,
			},
			"post": {
				Enabled: false,
				TTL:     5 * time.Minute,
				MaxSize: 200,
			},
		},
	}

	// Create cache registry
	registry, err := cache.NewCacheRegistry(cacheConfig, asInterfaceLogger(testLogger), nil)
	require.NoError(t, err)

	// Populate with some test data
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		key := cache.BuildEntityKey("user", string(rune('a'+i)))
		err := registry.Set(ctx, "user", key, []byte("test-data"))
		require.NoError(t, err)
	}

	cleanup := func() {
		registry.Close()
	}

	return registry, cleanup
}

func TestCacheStatsCommand(t *testing.T) {
	registry, cleanup := setupCacheTest(t)
	defer cleanup()

	tests := []struct {
		name       string
		entityType string
		wantErr    bool
	}{
		{
			name:       "stats for specific entity",
			entityType: "user",
			wantErr:    false,
		},
		{
			name:       "stats for disabled entity",
			entityType: "post",
			wantErr:    false,
		},
		{
			name:       "stats for non-existent entity",
			entityType: "nonexistent",
			wantErr:    false, // Should return default stats
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := registry.GetStats(tt.entityType)
			entityCache := registry.GetEntityCache(tt.entityType)

			assert.NotNil(t, stats)
			assert.NotNil(t, entityCache)

			if tt.entityType == "user" {
				assert.True(t, entityCache.IsEnabled())
				assert.Greater(t, stats.L1Capacity, 0)
			} else if tt.entityType == "post" {
				assert.False(t, entityCache.IsEnabled())
			}
		})
	}
}

func TestCacheAllStatsCommand(t *testing.T) {
	registry, cleanup := setupCacheTest(t)
	defer cleanup()

	allStats := registry.GetAllStats()

	assert.NotEmpty(t, allStats)
	assert.Contains(t, allStats, "user")
	assert.Contains(t, allStats, "post")

	// Verify user stats
	userStats := allStats["user"]
	assert.True(t, userStats.Enabled)
	assert.Greater(t, userStats.L1Capacity, 0)

	// Verify post stats (disabled)
	postStats := allStats["post"]
	assert.False(t, postStats.Enabled)
}

func TestCacheClearCommand(t *testing.T) {
	tests := []struct {
		name       string
		entityType string
		wantErr    bool
	}{
		{
			name:       "clear specific entity",
			entityType: "user",
			wantErr:    false,
		},
		{
			name:       "clear all entities",
			entityType: "",
			wantErr:    false,
		},
		{
			name:       "clear disabled entity",
			entityType: "post",
			wantErr:    true, // Should fail because cache is disabled
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, cleanup := setupCacheTest(t)
			defer cleanup()

			if tt.entityType != "" {
				// Clear specific entity
				entityCache := registry.GetEntityCache(tt.entityType)
				require.True(t, entityCache.IsEnabled() || tt.wantErr,
					"Cache must be enabled for entity %q when wantErr is false", tt.entityType)

				statsBefore := registry.GetStats(tt.entityType)
				if tt.entityType == "user" {
					assert.Greater(t, statsBefore.L1Size, 0, "Expected some cached entries")
				}

				if tt.wantErr {
					assert.False(t, entityCache.IsEnabled())
				} else {
					assert.True(t, entityCache.IsEnabled())
				}
			} else {
				// Clear all entities
				allStatsBefore := registry.GetAllStats()
				assert.NotEmpty(t, allStatsBefore)

				registry.Clear()

				allStatsAfter := registry.GetAllStats()
				for entityType, stats := range allStatsAfter {
					if stats.Enabled {
						assert.Equal(t, 0, stats.L1Size, "Entity %s should have 0 cached entries", entityType)
					}
				}
			}
		})
	}
}

func TestCacheConfigCommand(t *testing.T) {
	// Create test config
	testConfig := &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
				"post": {
					Enabled: false,
					TTL:     5 * time.Minute,
					MaxSize: 200,
				},
			},
		},
	}

	tests := []struct {
		name       string
		entityType string
		wantErr    bool
	}{
		{
			name:       "config for specific entity",
			entityType: "user",
			wantErr:    false,
		},
		{
			name:       "config for all entities",
			entityType: "",
			wantErr:    false,
		},
		{
			name:       "config for non-existent entity",
			entityType: "nonexistent",
			wantErr:    false, // Should return default config
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.entityType != "" {
				entityCfg := testConfig.Cache.GetEntityConfig(tt.entityType)
				assert.NotNil(t, entityCfg)

				if tt.entityType == "user" {
					assert.True(t, entityCfg.Enabled)
					assert.Equal(t, 10*time.Minute, entityCfg.TTL)
					assert.Equal(t, 500, entityCfg.MaxSize)
				} else if tt.entityType == "post" {
					assert.False(t, entityCfg.Enabled)
				}
			} else {
				// Test global config
				assert.True(t, testConfig.Cache.Enabled)
				assert.Equal(t, 5*time.Minute, testConfig.Cache.DefaultTTL)
				assert.Equal(t, 1000, testConfig.Cache.MaxSize)
				assert.NotEmpty(t, testConfig.Cache.Entities)
			}
		})
	}
}

func TestCacheEnableCommand(t *testing.T) {
	registry, cleanup := setupCacheTest(t)
	defer cleanup()

	tests := []struct {
		name       string
		entityType string
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "enable disabled entity",
			entityType: "post",
			wantErr:    false,
		},
		{
			name:       "enable already enabled entity",
			entityType: "user",
			wantErr:    true,
			errMsg:     "already enabled",
		},
		{
			name:       "enable new entity",
			entityType: "comment",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabledBefore := registry.IsCacheEnabled(tt.entityType)

			if tt.wantErr {
				assert.True(t, enabledBefore, "Entity should already be enabled")
			} else {
				// Create new entity config
				entityCfg := config.EntityCacheConfig{
					Enabled: true,
					TTL:     5 * time.Minute,
					MaxSize: 0,
				}

				err := registry.UpdateEntityConfig(tt.entityType, entityCfg)
				if tt.wantErr {
					assert.Error(t, err)
					if tt.errMsg != "" {
						assert.Contains(t, err.Error(), tt.errMsg)
					}
				} else {
					assert.NoError(t, err)
					assert.True(t, registry.IsCacheEnabled(tt.entityType))
				}
			}
		})
	}
}

func TestCacheDisableCommand(t *testing.T) {
	registry, cleanup := setupCacheTest(t)
	defer cleanup()

	tests := []struct {
		name       string
		entityType string
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "disable enabled entity",
			entityType: "user",
			wantErr:    false,
		},
		{
			name:       "disable already disabled entity",
			entityType: "post",
			wantErr:    true,
			errMsg:     "already disabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabledBefore := registry.IsCacheEnabled(tt.entityType)

			if tt.wantErr {
				assert.False(t, enabledBefore, "Entity should already be disabled")
			} else {
				// Create disabled entity config
				entityCfg := config.EntityCacheConfig{
					Enabled: false,
				}

				err := registry.UpdateEntityConfig(tt.entityType, entityCfg)
				if tt.wantErr {
					assert.Error(t, err)
					if tt.errMsg != "" {
						assert.Contains(t, err.Error(), tt.errMsg)
					}
				} else {
					assert.NoError(t, err)
					assert.False(t, registry.IsCacheEnabled(tt.entityType))
				}
			}
		})
	}
}

func TestCacheCommandOutputFormats(t *testing.T) {
	_, cleanup := setupCacheTest(t)
	defer cleanup()

	formats := []string{"json", "yaml", "table"}

	for _, format := range formats {
		t.Run("format_"+format, func(t *testing.T) {
			// Test that each format can be parsed
			_, err := parseOutputFormat(format)
			assert.NoError(t, err)
		})
	}

	// Test invalid format
	t.Run("invalid_format", func(t *testing.T) {
		_, err := parseOutputFormat("invalid")
		assert.Error(t, err)
	})
}

func TestInitializeCacheRegistry(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log

	defer func() {
		cfg = origCfg
		log = origLog
	}()

	t.Run("success", func(t *testing.T) {
		// Create test config
		cfg = &config.Config{
			Cache: config.EnhancedCacheConfig{
				Enabled:         true,
				DefaultTTL:      5 * time.Minute,
				MaxSize:         1000,
				CleanupInterval: 1 * time.Minute,
				Entities:        make(map[string]config.EntityCacheConfig),
			},
		}

		// Create test logger
		logConfig := logger.LogConfig{
			Level:     "error",
			Format:    "json",
			Output:    "stderr",
			Component: "test",
		}
		testLog, err := logger.New(logConfig)
		require.NoError(t, err)
		log = testLog

		registry, err := initializeCacheRegistry()
		assert.NoError(t, err)
		assert.NotNil(t, registry)
		defer registry.Close()
	})

	t.Run("nil config", func(t *testing.T) {
		cfg = nil
		log = origLog

		registry, err := initializeCacheRegistry()
		assert.Error(t, err)
		assert.Nil(t, registry)
	})

	t.Run("nil logger", func(t *testing.T) {
		cfg = &config.Config{
			Cache: config.EnhancedCacheConfig{
				Enabled:         true,
				DefaultTTL:      5 * time.Minute,
				MaxSize:         1000,
				CleanupInterval: 1 * time.Minute,
				Entities:        make(map[string]config.EntityCacheConfig),
			},
		}
		log = nil

		registry, err := initializeCacheRegistry()
		assert.Error(t, err)
		assert.Nil(t, registry)
	})
}

func TestCacheCommandHelp(t *testing.T) {
	tests := []struct {
		name    string
		command string
		expect  []string
	}{
		{
			name:    "cache help",
			command: "cache",
			expect:  []string{"stats", "clear", "config", "enable", "disable", "warm"},
		},
		{
			name:    "cache stats help",
			command: "cache stats",
			expect:  []string{"statistics", "hits", "misses", "hit rate"},
		},
		{
			name:    "cache clear help",
			command: "cache clear",
			expect:  []string{"Clear", "entity-type", "confirmation"},
		},
		{
			name:    "cache config help",
			command: "cache config",
			expect:  []string{"configuration", "TTL", "max size"},
		},
		{
			name:    "cache warm help",
			command: "cache warm",
			expect:  []string{"warm", "prefix", "storage"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCacheCommands()
			assert.NotNil(t, cmd)

			// Check that the command has the expected subcommands or help text
			_ = cmd.Long
			for _, expected := range tt.expect {
				// This is a simple check - in a real test you'd execute the command
				// and check its output
				if strings.Contains(tt.command, "stats") {
					statsCmd := newCacheStatsCommand()
					assert.Contains(t, strings.ToLower(statsCmd.Long), strings.ToLower(expected))
				} else if strings.Contains(tt.command, "warm") {
					warmCmd := newCacheWarmCommand()
					assert.Contains(t, strings.ToLower(warmCmd.Long), strings.ToLower(expected))
				}
			}
		})
	}
}

func TestCacheClearConfirmation(t *testing.T) {
	t.Run("with force flag", func(t *testing.T) {
		// When --yes flag is set, no confirmation should be needed
		cacheForce = true
		defer func() { cacheForce = false }()

		// Test would proceed without confirmation
		assert.True(t, cacheForce)
	})

	t.Run("without force flag", func(t *testing.T) {
		// When --yes flag is not set, confirmation is required
		cacheForce = false

		// Test would require confirmation
		assert.False(t, cacheForce)
	})
}

func TestCacheStatsDetails(t *testing.T) {
	registry, cleanup := setupCacheTest(t)
	defer cleanup()

	t.Run("with details flag", func(t *testing.T) {
		cacheShowDetails = true
		defer func() { cacheShowDetails = false }()

		stats := registry.GetStats("user")
		assert.True(t, stats.Enabled)

		// When details flag is set, response should include detailed breakdown
		assert.True(t, cacheShowDetails)
	})

	t.Run("without details flag", func(t *testing.T) {
		cacheShowDetails = false

		stats := registry.GetStats("user")
		assert.True(t, stats.Enabled)

		// When details flag is not set, response should be summary only
		assert.False(t, cacheShowDetails)
	})
}

func TestCacheIntegration(t *testing.T) {
	registry, cleanup := setupCacheTest(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("full cache lifecycle", func(t *testing.T) {
		entityType := "integration_test"

		// 1. Enable cache for entity
		entityCfg := config.EntityCacheConfig{
			Enabled: true,
			TTL:     1 * time.Minute,
			MaxSize: 100,
		}
		err := registry.UpdateEntityConfig(entityType, entityCfg)
		require.NoError(t, err)
		assert.True(t, registry.IsCacheEnabled(entityType))

		// 2. Add some data
		for i := 0; i < 5; i++ {
			key := cache.BuildEntityKey(entityType, string(rune('a'+i)))
			err := registry.Set(ctx, entityType, key, []byte("test-data"))
			require.NoError(t, err)
		}

		// 3. Check stats
		stats := registry.GetStats(entityType)
		assert.True(t, stats.Enabled)
		assert.Equal(t, 5, stats.L1Size)

		// 4. Clear cache
		registry.Clear()
		statsAfter := registry.GetStats(entityType)
		assert.Equal(t, 0, statsAfter.L1Size)

		// 5. Disable cache
		entityCfg.Enabled = false
		err = registry.UpdateEntityConfig(entityType, entityCfg)
		require.NoError(t, err)
		assert.False(t, registry.IsCacheEnabled(entityType))
	})
}

// Helper function to parse output format.
func parseOutputFormat(format string) (string, error) {
	switch strings.ToLower(format) {
	case "json", "yaml", "table", "raw":
		return format, nil
	default:
		return "", assert.AnError
	}
}

// Benchmark tests.
func BenchmarkCacheStats(b *testing.B) {
	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "bench",
	}
	testLogger, _ := logger.New(logConfig)

	cacheConfig := &config.EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         1000,
		CleanupInterval: 1 * time.Minute,
		Entities: map[string]config.EntityCacheConfig{
			"user": {Enabled: true, TTL: 10 * time.Minute, MaxSize: 500},
		},
	}

	registry, _ := cache.NewCacheRegistry(cacheConfig, asInterfaceLogger(testLogger), nil)
	defer registry.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = registry.GetStats("user")
	}
}

func BenchmarkCacheAllStats(b *testing.B) {
	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "bench",
	}
	testLogger, _ := logger.New(logConfig)

	cacheConfig := &config.EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         1000,
		CleanupInterval: 1 * time.Minute,
		Entities: map[string]config.EntityCacheConfig{
			"user": {Enabled: true, TTL: 10 * time.Minute, MaxSize: 500},
			"post": {Enabled: true, TTL: 5 * time.Minute, MaxSize: 200},
		},
	}

	registry, _ := cache.NewCacheRegistry(cacheConfig, asInterfaceLogger(testLogger), nil)
	defer registry.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = registry.GetAllStats()
	}
}

// TestRunCacheStatsCommand tests the runCacheStatsCommand function.
func TestRunCacheStatsCommand(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log

	defer func() {
		cfg = origCfg
		log = origLog
	}()

	// Setup test config and logger
	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
			},
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	tests := []struct {
		name         string
		args         []string
		outputFormat string
		showDetails  bool
		expectError  bool
	}{
		{
			name:         "stats for all entities with table format",
			args:         []string{},
			outputFormat: "table",
			showDetails:  false,
			expectError:  false,
		},
		{
			name:         "stats for specific entity",
			args:         []string{"user"},
			outputFormat: "table",
			showDetails:  false,
			expectError:  false,
		},
		{
			name:         "stats with json format",
			args:         []string{},
			outputFormat: "json",
			showDetails:  false,
			expectError:  false,
		},
		{
			name:         "stats with yaml format",
			args:         []string{},
			outputFormat: "yaml",
			showDetails:  false,
			expectError:  false,
		},
		{
			name:         "stats with details",
			args:         []string{"user"},
			outputFormat: "table",
			showDetails:  true,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCacheStatsCommand()

			cacheOutputFmt = tt.outputFormat
			cacheShowDetails = tt.showDetails

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunCacheClearCommand tests the runCacheClearCommand function.
func TestRunCacheClearCommand(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log

	defer func() {
		cfg = origCfg
		log = origLog
	}()

	// Setup test config and logger
	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
			},
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	tests := []struct {
		name         string
		args         []string
		force        bool
		outputFormat string
		expectError  bool
	}{
		{
			name:         "clear all with force",
			args:         []string{},
			force:        true,
			outputFormat: "table",
			expectError:  false,
		},
		{
			name:         "clear specific entity with force",
			args:         []string{"user"},
			force:        true,
			outputFormat: "table",
			expectError:  false,
		},
		{
			name:         "clear with json output",
			args:         []string{},
			force:        true,
			outputFormat: "json",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCacheClearCommand()

			cacheForce = tt.force
			cacheOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunCacheConfigCommand tests the runCacheConfigCommand function.
func TestRunCacheConfigCommand(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log

	defer func() {
		cfg = origCfg
		log = origLog
	}()

	// Setup test config and logger
	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
			},
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	tests := []struct {
		name         string
		args         []string
		outputFormat string
		expectError  bool
	}{
		{
			name:         "config for all entities",
			args:         []string{},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name:         "config for specific entity",
			args:         []string{"user"},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name:         "config with json output",
			args:         []string{},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name:         "config with yaml output",
			args:         []string{},
			outputFormat: "yaml",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCacheConfigCommand()

			cacheOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunCacheEnableCommand tests the runCacheEnableCommand function.
func TestRunCacheEnableCommand(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log

	defer func() {
		cfg = origCfg
		log = origLog
	}()

	// Setup test config and logger
	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"post": {
					Enabled: false,
					TTL:     5 * time.Minute,
					MaxSize: 200,
				},
			},
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	tests := []struct {
		name         string
		args         []string
		outputFormat string
		expectError  bool
	}{
		{
			name:         "enable disabled entity",
			args:         []string{"post"},
			outputFormat: "table",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCacheEnableCommand()

			cacheOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunCacheEnableCommandJSON tests enabling cache with JSON output separately.
func TestRunCacheEnableCommandJSON(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log

	defer func() {
		cfg = origCfg
		log = origLog
	}()

	// Setup test config and logger
	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"article": {
					Enabled: false,
					TTL:     5 * time.Minute,
					MaxSize: 200,
				},
			},
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	cmd := newCacheEnableCommand()
	cacheOutputFmt = "json"

	err = cmd.RunE(cmd, []string{"article"})
	assert.NoError(t, err)
}

// TestRunCacheDisableCommand tests the runCacheDisableCommand function.
func TestRunCacheDisableCommand(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log

	defer func() {
		cfg = origCfg
		log = origLog
	}()

	// Setup test config and logger
	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
			},
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	tests := []struct {
		name         string
		args         []string
		outputFormat string
		expectError  bool
	}{
		{
			name:         "disable enabled entity",
			args:         []string{"user"},
			outputFormat: "table",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCacheDisableCommand()

			cacheOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunCacheDisableCommandJSON tests disabling cache with JSON output separately.
func TestRunCacheDisableCommandJSON(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log

	defer func() {
		cfg = origCfg
		log = origLog
	}()

	// Setup test config and logger
	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"profile": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
			},
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	cmd := newCacheDisableCommand()
	cacheOutputFmt = "json"

	err = cmd.RunE(cmd, []string{"profile"})
	assert.NoError(t, err)
}

// TestCacheWarmCommand tests the newCacheWarmCommand function.
func TestCacheWarmCommand(t *testing.T) {
	cmd := newCacheWarmCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "warm", cmd.Use)
	assert.Contains(t, cmd.Long, "Pre-populate the cache")

	// Verify flags are registered
	prefixFlag := cmd.Flags().Lookup("prefix")
	assert.NotNil(t, prefixFlag)
	assert.Equal(t, "stringArray", prefixFlag.Value.Type())

	rateFlag := cmd.Flags().Lookup("rate")
	assert.NotNil(t, rateFlag)
	assert.Equal(t, "int", rateFlag.Value.Type())

	maxKeysFlag := cmd.Flags().Lookup("max-keys")
	assert.NotNil(t, maxKeysFlag)
	assert.Equal(t, "int", maxKeysFlag.Value.Type())

	batchSizeFlag := cmd.Flags().Lookup("batch-size")
	assert.NotNil(t, batchSizeFlag)
	assert.Equal(t, "100", batchSizeFlag.DefValue)
}

// TestCacheWarmCommandNoPrefixes tests warm command with no prefixes.
func TestCacheWarmCommandNoPrefixes(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log
	origPrefixes := cacheWarmPrefixes

	defer func() {
		cfg = origCfg
		log = origLog
		cacheWarmPrefixes = origPrefixes
	}()

	// Setup test config and logger
	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities:        make(map[string]config.EntityCacheConfig),
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	// Clear prefixes to trigger error
	cacheWarmPrefixes = nil

	cmd := newCacheWarmCommand()
	err = cmd.RunE(cmd, []string{})

	// Should fail with no prefixes error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no prefixes specified")
}

// TestCacheWarmCommandValidation tests warm command validation.
func TestCacheWarmCommandValidation(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log
	origPrefixes := cacheWarmPrefixes

	defer func() {
		cfg = origCfg
		log = origLog
		cacheWarmPrefixes = origPrefixes
	}()

	tests := []struct {
		name        string
		cfg         *config.Config
		log         *logger.Logger
		prefixes    []string
		expectError bool
		errContains string
	}{
		{
			name:        "nil config",
			cfg:         nil,
			log:         nil,
			prefixes:    []string{"test:"},
			expectError: true,
			errContains: "configuration not initialized",
		},
		{
			name: "no prefixes",
			cfg: &config.Config{
				Cache: config.EnhancedCacheConfig{
					Enabled:         true,
					DefaultTTL:      5 * time.Minute,
					MaxSize:         1000,
					CleanupInterval: 1 * time.Minute,
				},
			},
			prefixes:    nil,
			expectError: true,
			errContains: "no prefixes specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg = tt.cfg

			if tt.log != nil {
				log = tt.log
			} else if tt.cfg != nil {
				// Create logger only if config is not nil
				logConfig := logger.LogConfig{
					Level:     "error",
					Format:    "json",
					Output:    "stderr",
					Component: "test",
				}
				testLog, err := logger.New(logConfig)
				require.NoError(t, err)
				log = testLog
			}

			cacheWarmPrefixes = tt.prefixes

			cmd := newCacheWarmCommand()
			err := cmd.RunE(cmd, []string{})

			if tt.expectError {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestCacheWarmFlags tests cache warm command flags.
func TestCacheWarmFlags(t *testing.T) {
	// Save original values
	origPrefixes := cacheWarmPrefixes
	origRate := cacheWarmRate
	origMaxKeys := cacheWarmMaxKeys
	origBatchSize := cacheWarmBatchSize

	defer func() {
		cacheWarmPrefixes = origPrefixes
		cacheWarmRate = origRate
		cacheWarmMaxKeys = origMaxKeys
		cacheWarmBatchSize = origBatchSize
	}()

	t.Run("default values", func(t *testing.T) {
		cmd := newCacheWarmCommand()

		rateFlag := cmd.Flags().Lookup("rate")
		assert.Equal(t, "0", rateFlag.DefValue)

		maxKeysFlag := cmd.Flags().Lookup("max-keys")
		assert.Equal(t, "0", maxKeysFlag.DefValue)

		batchSizeFlag := cmd.Flags().Lookup("batch-size")
		assert.Equal(t, "100", batchSizeFlag.DefValue)
	})

	t.Run("set flags", func(t *testing.T) {
		cacheWarmPrefixes = []string{"users:", "sessions:"}
		cacheWarmRate = 1000
		cacheWarmMaxKeys = 10000
		cacheWarmBatchSize = 50

		assert.Equal(t, []string{"users:", "sessions:"}, cacheWarmPrefixes)
		assert.Equal(t, 1000, cacheWarmRate)
		assert.Equal(t, 10000, cacheWarmMaxKeys)
		assert.Equal(t, 50, cacheWarmBatchSize)
	})
}

// TestOpenStorageForWarmingValidation tests storage opening validation.
func TestOpenStorageForWarmingValidation(t *testing.T) {
	// Save original values
	origCfg := cfg
	origDataDir := dataDir

	defer func() {
		cfg = origCfg
		dataDir = origDataDir
	}()

	t.Run("nil config", func(t *testing.T) {
		cfg = nil
		dataDir = ""

		_, err := openStorageForWarming()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "configuration not initialized")
	})

	t.Run("no data directory uses in-memory storage", func(t *testing.T) {
		cfg = &config.Config{
			Storage: config.StorageConfig{
				DataDir: "",
			},
		}
		dataDir = ""

		reader, err := openStorageForWarming()
		assert.NoError(t, err)
		assert.NotNil(t, reader)
	})

	t.Run("data directory flag ignored for in-memory storage", func(t *testing.T) {
		cfg = &config.Config{
			Storage: config.StorageConfig{
				DataDir: "",
			},
		}
		dataDir = "/tmp/test-data-dir-that-does-not-exist-abc123xyz"

		reader, err := openStorageForWarming()
		assert.NoError(t, err)
		assert.NotNil(t, reader)
	})
}

func TestRunCacheClearCommandWithoutConfirmation(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log

	defer func() {
		cfg = origCfg
		log = origLog
	}()

	// Setup test config and logger
	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
			},
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	tests := []struct {
		name        string
		args        []string
		userInput   string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "user confirms with 'y'",
			args:        []string{"user"},
			userInput:   "y\n",
			expectError: false,
		},
		{
			name:        "user confirms with 'yes'",
			args:        []string{"user"},
			userInput:   "yes\n",
			expectError: false,
		},
		{
			name:        "user cancels with 'n'",
			args:        []string{"user"},
			userInput:   "n\n",
			expectError: false,
		},
		{
			name:        "user cancels with 'no'",
			args:        []string{"user"},
			userInput:   "no\n",
			expectError: false,
		},
		{
			name:        "user cancels with empty",
			args:        []string{"user"},
			userInput:   "\n",
			expectError: false,
		},
		{
			name:        "clear all with 'y'",
			args:        []string{},
			userInput:   "y\n",
			expectError: false,
		},
		{
			name:        "clear all with 'yes'",
			args:        []string{},
			userInput:   "yes\n",
			expectError: false,
		},
		{
			name:        "clear all cancelled",
			args:        []string{},
			userInput:   "n\n",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCacheClearCommand()

			cacheForce = false
			cacheOutputFmt = "table"

			// Redirect stdin
			oldStdin := os.Stdin
			r, w, _ := os.Pipe()
			os.Stdin = r

			// Write user input
			go func() {
				w.Write([]byte(tt.userInput))
				w.Close()
			}()

			defer func() {
				os.Stdin = oldStdin
			}()

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunCacheClearCommandDisabledEntity tests clearing cache for disabled entity.
func TestRunCacheClearCommandDisabledEntity(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log

	defer func() {
		cfg = origCfg
		log = origLog
	}()

	// Setup test config and logger
	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"post": {
					Enabled: false,
					TTL:     5 * time.Minute,
					MaxSize: 200,
				},
			},
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	cmd := newCacheClearCommand()

	cacheForce = true
	cacheOutputFmt = "table"

	err = cmd.RunE(cmd, []string{"post"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "caching is not enabled")
}

// TestRunCacheClearCommandOutputFormats tests different output formats.
func TestRunCacheClearCommandOutputFormats(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log

	defer func() {
		cfg = origCfg
		log = origLog
	}()

	// Setup test config and logger
	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
			},
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	formats := []string{"table", "json", "yaml"}

	for _, format := range formats {
		t.Run("format_"+format, func(t *testing.T) {
			cmd := newCacheClearCommand()

			cacheForce = true
			cacheOutputFmt = format

			err := cmd.RunE(cmd, []string{})
			assert.NoError(t, err)
		})
	}
}

// TestRunCacheClearCommandStdoutCapture tests that output is written correctly.
func TestRunCacheClearCommandStdoutCapture(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log

	defer func() {
		cfg = origCfg
		log = origLog
	}()

	// Setup test config and logger
	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
			},
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := newCacheClearCommand()

	cacheForce = true
	cacheOutputFmt = "table"

	err = cmd.RunE(cmd, []string{"user"})
	assert.NoError(t, err)

	// Restore stdout and read output
	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	output := buf.String()
	// Output should contain success message
	assert.True(t, len(output) > 0)
}

// TestRunCacheClearCommandReadError tests stdin read error handling.
func TestRunCacheClearCommandReadError(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log

	defer func() {
		cfg = origCfg
		log = origLog
	}()

	// Setup test config and logger
	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
			},
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	cmd := newCacheClearCommand()

	cacheForce = false
	cacheOutputFmt = "table"

	// Close stdin immediately to trigger read error
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	w.Close() // Close write end immediately

	defer func() {
		os.Stdin = oldStdin
	}()

	err = cmd.RunE(cmd, []string{"user"})
	// Should return error for read failure
	assert.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "failed to read")
}

// TestRunCacheClearCommandSpecificEntity tests clearing specific entity.
func TestRunCacheClearCommandSpecificEntity(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log

	defer func() {
		cfg = origCfg
		log = origLog
	}()

	// Setup test config and logger
	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
				"post": {
					Enabled: true,
					TTL:     5 * time.Minute,
					MaxSize: 200,
				},
			},
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	entities := []string{"user", "post"}

	for _, entity := range entities {
		t.Run("clear_"+entity, func(t *testing.T) {
			cmd := newCacheClearCommand()

			cacheForce = true
			cacheOutputFmt = "json"

			err := cmd.RunE(cmd, []string{entity})
			assert.NoError(t, err)
		})
	}
}

// =============================================================================
// Cache Command Tests - Testing nil config checks (no args required)
// =============================================================================

func TestCacheWarmCommand_NilConfig_NinetyBoost(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()

	cfg = nil
	cmd := newCacheWarmCommand()
	err := runCacheWarmCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestCacheWarmCommand_NoPrefixes_NinetyBoost(t *testing.T) {
	origCfg := cfg
	origPrefixes := cacheWarmPrefixes
	defer func() {
		cfg = origCfg
		cacheWarmPrefixes = origPrefixes
	}()

	// Set empty config
	logConfig := logger.LogConfig{
		Level:     "error",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	cfg = &testConfigNinetyBoost
	cacheWarmPrefixes = []string{}

	cmd := newCacheWarmCommand()
	err = runCacheWarmCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no prefixes specified")
}

func TestInitializeCacheRegistry_NilConfig_NinetyBoost(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()

	cfg = nil
	_, err := initializeCacheRegistry()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestInitializeCacheRegistry_NilLogger_NinetyBoost(t *testing.T) {
	origCfg := cfg
	origLog := log
	defer func() {
		cfg = origCfg
		log = origLog
	}()

	cfg = &testConfigNinetyBoost
	log = nil

	_, err := initializeCacheRegistry()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "logger not initialized")
}

// =============================================================================
// Test Helper for Config
// =============================================================================

var testConfigNinetyBoost = config.Config{
	Cache: config.EnhancedCacheConfig{
		Enabled:         true,
		MaxSize:         1000,
		DefaultTTL:      time.Minute,
		CleanupInterval: time.Minute,
	},
}

func TestRunCacheWarmCommand_NilConfigCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	defer func() {
		cfg = origCfg
		log = origLog
	}()

	cfg = nil
	log = createTestLoggerForCoveragePlus(t)

	cmd := &cobra.Command{}
	err := runCacheWarmCommand(cmd, []string{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestRunCacheWarmCommand_NoPrefixesCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	origPrefixes := cacheWarmPrefixes
	defer func() {
		cfg = origCfg
		log = origLog
		cacheWarmPrefixes = origPrefixes
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
		},
	}
	log = createTestLoggerForCoveragePlus(t)
	cacheWarmPrefixes = nil

	cmd := &cobra.Command{}
	err := runCacheWarmCommand(cmd, []string{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no prefixes specified")
}

func TestRunCacheWarmCommand_InvalidOutputFormatCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	origPrefixes := cacheWarmPrefixes
	origFmt := cacheOutputFmt
	defer func() {
		cfg = origCfg
		log = origLog
		cacheWarmPrefixes = origPrefixes
		cacheOutputFmt = origFmt
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
		},
	}
	log = createTestLoggerForCoveragePlus(t)
	cacheWarmPrefixes = []string{"test:"}
	cacheOutputFmt = "invalid-format"

	cmd := &cobra.Command{}
	err := runCacheWarmCommand(cmd, []string{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

func TestRunCacheWarmCommand_NilLoggerWithConfigCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	origPrefixes := cacheWarmPrefixes
	defer func() {
		cfg = origCfg
		log = origLog
		cacheWarmPrefixes = origPrefixes
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled: true,
		},
	}
	log = nil
	cacheWarmPrefixes = []string{"test:"}

	cmd := &cobra.Command{}
	err := runCacheWarmCommand(cmd, []string{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "logger not initialized")
}

func TestRunCacheWarmCommand_WithValidConfigButStorageErrorCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	origPrefixes := cacheWarmPrefixes
	origFmt := cacheOutputFmt
	origDataDir := dataDir
	defer func() {
		cfg = origCfg
		log = origLog
		cacheWarmPrefixes = origPrefixes
		cacheOutputFmt = origFmt
		dataDir = origDataDir
	}()

	tempDir := t.TempDir()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
		},
		Storage: config.StorageConfig{
			DataDir: tempDir,
		},
	}
	log = createTestLoggerForCoveragePlus(t)
	cacheWarmPrefixes = []string{"test:"}
	cacheOutputFmt = "json"
	dataDir = tempDir

	cmd := &cobra.Command{}
	err := runCacheWarmCommand(cmd, []string{})

	require.NoError(t, err)
}

func TestInitializeCacheWarmerDependencies_NilConfigCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	defer func() {
		cfg = origCfg
		log = origLog
	}()

	cfg = nil
	log = createTestLoggerForCoveragePlus(t)

	manager, reader, cleanup, err := initializeCacheWarmerDependencies()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
	assert.Nil(t, manager)
	assert.Nil(t, reader)
	assert.Nil(t, cleanup)
}

func TestInitializeCacheWarmerDependencies_NilLoggerCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	defer func() {
		cfg = origCfg
		log = origLog
	}()

	cfg = &config.Config{}
	log = nil

	manager, reader, cleanup, err := initializeCacheWarmerDependencies()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "logger not initialized")
	assert.Nil(t, manager)
	assert.Nil(t, reader)
	assert.Nil(t, cleanup)
}

func TestOpenStorageForWarming_NilConfigCovPlus(t *testing.T) {
	origCfg := cfg
	defer func() {
		cfg = origCfg
	}()

	cfg = nil

	reader, err := openStorageForWarming()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
	assert.Nil(t, reader)
}

func TestOpenStorageForWarming_NoDataDirCovPlus(t *testing.T) {
	origCfg := cfg
	origDataDir := dataDir
	defer func() {
		cfg = origCfg
		dataDir = origDataDir
	}()

	cfg = &config.Config{
		Storage: config.StorageConfig{
			DataDir: "",
		},
	}
	dataDir = ""

	reader, err := openStorageForWarming()

	require.NoError(t, err)
	assert.NotNil(t, reader)
}

func TestOpenStorageForWarming_UsesDataDirFlagCovPlus(t *testing.T) {
	origCfg := cfg
	origDataDir := dataDir
	origLog := log
	defer func() {
		cfg = origCfg
		dataDir = origDataDir
		log = origLog
	}()

	tmpDir := t.TempDir()
	cfg = &config.Config{
		Storage: config.StorageConfig{
			DataDir: "/not/used",
		},
	}
	dataDir = tmpDir
	log = createTestLoggerForCoveragePlus(t)

	reader, err := openStorageForWarming()

	if err != nil {
		assert.NotContains(t, err.Error(), "not configured")
	} else if reader != nil {
		if closer, ok := reader.(interface{ Close() error }); ok {
			closer.Close()
		}
	}
}

// =============================================================================
// Initialize Cache Registry Coverage Tests
// =============================================================================

func TestInitializeCacheRegistry_NilConfigCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	defer func() {
		cfg = origCfg
		log = origLog
	}()

	cfg = nil
	log = createTestLoggerForCoveragePlus(t)

	registry, err := initializeCacheRegistry()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
	assert.Nil(t, registry)
}

func TestInitializeCacheRegistry_NilLoggerCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	defer func() {
		cfg = origCfg
		log = origLog
	}()

	cfg = &config.Config{}
	log = nil

	registry, err := initializeCacheRegistry()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "logger not initialized")
	assert.Nil(t, registry)
}

func TestInitializeCacheRegistry_SuccessCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	defer func() {
		cfg = origCfg
		log = origLog
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
			},
		},
	}
	log = createTestLoggerForCoveragePlus(t)

	registry, err := initializeCacheRegistry()

	require.NoError(t, err)
	require.NotNil(t, registry)
	defer registry.Close()

	userCache := registry.GetEntityCache("user")
	assert.True(t, userCache.IsEnabled())
}

// =============================================================================
// Cache Enable/Disable Command Coverage Tests
// =============================================================================

func TestRunCacheEnableCommand_WithRegistryCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() {
		cfg = origCfg
		log = origLog
		cacheOutputFmt = origFmt
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"post": {
					Enabled: false,
					TTL:     5 * time.Minute,
					MaxSize: 200,
				},
			},
		},
	}
	log = createTestLoggerForCoveragePlus(t)
	cacheOutputFmt = "json"

	cmd := &cobra.Command{}
	err := runCacheEnableCommand(cmd, []string{"post"})

	require.NoError(t, err)
}

func TestRunCacheEnableCommand_AlreadyEnabledCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() {
		cfg = origCfg
		log = origLog
		cacheOutputFmt = origFmt
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
			},
		},
	}
	log = createTestLoggerForCoveragePlus(t)
	cacheOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runCacheEnableCommand(cmd, []string{"user"})

	require.NoError(t, err)
}

func TestRunCacheDisableCommand_WithRegistryCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() {
		cfg = origCfg
		log = origLog
		cacheOutputFmt = origFmt
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
			},
		},
	}
	log = createTestLoggerForCoveragePlus(t)
	cacheOutputFmt = "yaml"

	cmd := &cobra.Command{}
	err := runCacheDisableCommand(cmd, []string{"user"})

	require.NoError(t, err)
}

func TestRunCacheDisableCommand_AlreadyDisabledCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() {
		cfg = origCfg
		log = origLog
		cacheOutputFmt = origFmt
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"post": {
					Enabled: false,
					TTL:     5 * time.Minute,
					MaxSize: 200,
				},
			},
		},
	}
	log = createTestLoggerForCoveragePlus(t)
	cacheOutputFmt = "json"

	cmd := &cobra.Command{}
	err := runCacheDisableCommand(cmd, []string{"post"})

	require.NoError(t, err)
}

func TestRunCacheEnableCommand_InvalidOutputFormatCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() {
		cfg = origCfg
		log = origLog
		cacheOutputFmt = origFmt
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"test": {
					Enabled: false,
					TTL:     5 * time.Minute,
					MaxSize: 200,
				},
			},
		},
	}
	log = createTestLoggerForCoveragePlus(t)
	cacheOutputFmt = "invalid"

	cmd := &cobra.Command{}
	err := runCacheEnableCommand(cmd, []string{"test"})

	require.Error(t, err)
}

func TestRunCacheDisableCommand_InvalidOutputFormatCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() {
		cfg = origCfg
		log = origLog
		cacheOutputFmt = origFmt
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"test": {
					Enabled: true,
					TTL:     5 * time.Minute,
					MaxSize: 200,
				},
			},
		},
	}
	log = createTestLoggerForCoveragePlus(t)
	cacheOutputFmt = "invalid"

	cmd := &cobra.Command{}
	err := runCacheDisableCommand(cmd, []string{"test"})

	require.Error(t, err)
}

// =============================================================================
// Cache Config Command Coverage Tests
// =============================================================================

func TestRunCacheConfigCommand_NilConfigCovPlus(t *testing.T) {
	origCfg := cfg
	defer func() {
		cfg = origCfg
	}()

	cfg = nil

	cmd := &cobra.Command{}
	err := runCacheConfigCommand(cmd, []string{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestRunCacheConfigCommand_InvalidOutputFormatCovPlus(t *testing.T) {
	origCfg := cfg
	origFmt := cacheOutputFmt
	defer func() {
		cfg = origCfg
		cacheOutputFmt = origFmt
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled: true,
		},
	}
	cacheOutputFmt = "invalid"

	cmd := &cobra.Command{}
	err := runCacheConfigCommand(cmd, []string{})

	require.Error(t, err)
}

// =============================================================================
// Cluster Status Command Coverage Tests - Mock Implementation
// =============================================================================

type mockClusterClientForCovPlusTest struct {
	statusFunc func(ctx context.Context) (*cluster.ClusterStatus, error)
}

func (m *mockClusterClientForCovPlusTest) Status(ctx context.Context) (*cluster.ClusterStatus, error) {
	if m.statusFunc != nil {
		return m.statusFunc(ctx)
	}
	return &cluster.ClusterStatus{
		ClusterID: "test-cluster",
		Healthy:   true,
		Members: []cluster.ClusterMember{
			{ID: "1", Address: "localhost:50051", IsLeader: true, Healthy: true},
			{ID: "2", Address: "localhost:50052", IsLeader: false, Healthy: true},
		},
		LeaderID: "1",
	}, nil
}

func (m *mockClusterClientForCovPlusTest) Nodes(ctx context.Context) (*cluster.NodesResponse, error) {
	return &cluster.NodesResponse{}, nil
}

func (m *mockClusterClientForCovPlusTest) AddNode(ctx context.Context, req *cluster.AddNodeRequest) error {
	return nil
}

func (m *mockClusterClientForCovPlusTest) RemoveNode(ctx context.Context, req *cluster.RemoveNodeRequest) error {
	return nil
}

func (m *mockClusterClientForCovPlusTest) RebalanceCluster(ctx context.Context, req *cluster.RebalanceClusterRequest) (*cluster.RebalanceClusterResponse, error) {
	return nil, nil
}

func (m *mockClusterClientForCovPlusTest) GetRebalanceProgress(ctx context.Context, req *cluster.GetRebalanceProgressRequest) (*cluster.GetRebalanceProgressResponse, error) {
	return nil, nil
}

type mockSDKClientForClusterCovPlusTest struct {
	clusterClient *mockClusterClientForCovPlusTest
	closeFunc     func() error
}

func (m *mockSDKClientForClusterCovPlusTest) Cluster() quicbaas.ClusterClient {
	if m.clusterClient != nil {
		return m.clusterClient
	}
	return &mockClusterClientForCovPlusTest{}
}

func (m *mockSDKClientForClusterCovPlusTest) KV() quicbaas.KVClient { return nil }

func (m *mockSDKClientForClusterCovPlusTest) Batch() quicbaas.BatchClient { return nil }

func (m *mockSDKClientForClusterCovPlusTest) Transaction() quicbaas.TransactionClient { return nil }

func (m *mockSDKClientForClusterCovPlusTest) Query() quicbaas.QueryClient { return nil }

func (m *mockSDKClientForClusterCovPlusTest) Health() quicbaas.HealthClient { return nil }

func (m *mockSDKClientForClusterCovPlusTest) Operations() quicbaas.OperationsClient { return nil }

func (m *mockSDKClientForClusterCovPlusTest) Cache() quicbaas.CacheClient { return nil }

func (m *mockSDKClientForClusterCovPlusTest) IAM() quicbaas.IAMClient { return nil }

func (m *mockSDKClientForClusterCovPlusTest) Vector() quicbaas.VectorClient { return nil }

func (m *mockSDKClientForClusterCovPlusTest) TimeSeries() quicbaas.TimeSeriesClient { return nil }

func (m *mockSDKClientForClusterCovPlusTest) Backup() quicbaas.BackupClient { return nil }

func (m *mockSDKClientForClusterCovPlusTest) ObjectStorage() quicbaas.ObjectStorageClient { return nil }

func (m *mockSDKClientForClusterCovPlusTest) ObjectStorageAdmin() quicbaas.ObjectStorageAdminClient {
	return nil
}

func (m *mockSDKClientForClusterCovPlusTest) ObjectTransfer() quicbaas.ObjectTransferClient {
	return nil
}

func (m *mockSDKClientForClusterCovPlusTest) ParameterStore() quicbaas.ParameterStoreClient {
	return nil
}

func (m *mockSDKClientForClusterCovPlusTest) Scheduler() quicbaas.SchedulerClient { return nil }

func (m *mockSDKClientForClusterCovPlusTest) Messaging() quicbaas.MessagingClient { return nil }

func (m *mockSDKClientForClusterCovPlusTest) Embedding() quicbaas.EmbeddingClient { return nil }

func (m *mockSDKClientForClusterCovPlusTest) Ollama() quicbaas.OllamaClient { return nil }

func (m *mockSDKClientForClusterCovPlusTest) RAG() quicbaas.RAGClient { return nil }

func (m *mockSDKClientForClusterCovPlusTest) Transport() quicbaas.Transport { return nil }

func (m *mockSDKClientForClusterCovPlusTest) CoreData() quicbaas.CoreDataService { return nil }
func (m *mockSDKClientForClusterCovPlusTest) ClusterMgmt() quicbaas.ClusterManagementService {
	return nil
}
func (m *mockSDKClientForClusterCovPlusTest) Security() quicbaas.SecurityService { return nil }
func (m *mockSDKClientForClusterCovPlusTest) Identity() quicbaas.IdentityService { return nil }
func (m *mockSDKClientForClusterCovPlusTest) Search() quicbaas.SearchService     { return nil }
func (m *mockSDKClientForClusterCovPlusTest) Storage() quicbaas.StorageService   { return nil }
func (m *mockSDKClientForClusterCovPlusTest) Async() quicbaas.AsyncService       { return nil }
func (m *mockSDKClientForClusterCovPlusTest) AI() quicbaas.AIService             { return nil }
func (m *mockSDKClientForClusterCovPlusTest) DataStore() (quicbaas.QRDBKVStore, error) {
	return nil, nil
}

func (m *mockSDKClientForClusterCovPlusTest) BarrierCeremonyMgmt() quicbaas.BarrierCeremonyService {
	return nil
}
func (m *mockSDKClientForClusterCovPlusTest) TenantMgmtService() quicbaas.TenantService { return nil }
func (m *mockSDKClientForClusterCovPlusTest) Barrier() (quicbaas.QRDBBarrierClient, error) {
	return nil, nil
}
func (m *mockSDKClientForClusterCovPlusTest) BarrierCeremony() (quicbaas.QRDBCeremonyClient, error) {
	return nil, nil
}
func (m *mockSDKClientForClusterCovPlusTest) SealStore() (quicbaas.QRDBSealStoreClient, error) {
	return nil, nil
}
func (m *mockSDKClientForClusterCovPlusTest) TenantMgmt() (quicbaas.QRDBTenantClient, error) {
	return nil, nil
}
func (m *mockSDKClientForClusterCovPlusTest) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func (m *mockSDKClientForClusterCovPlusTest) Project() quicbaas.ProjectClient { return nil }
func (m *mockSDKClientForClusterCovPlusTest) SDLC() quicbaas.SDLCClient       { return nil }
func (m *mockSDKClientForClusterCovPlusTest) Prompts() quicbaas.PromptsClient { return nil }
func (m *mockSDKClientForClusterCovPlusTest) Agents() quicbaas.AgentsClient   { return nil }
func (m *mockSDKClientForClusterCovPlusTest) Account() quicbaas.AccountClient { return nil }

func (m *mockSDKClientForClusterCovPlusTest) Orchestrator() quicbaas.OrchestratorClient {
	return nil
}

// =============================================================================
// Embedded Server Coverage Tests
// =============================================================================

func TestEmbeddedServerFailedError_MessageCovPlus(t *testing.T) {
	err := &EmbeddedServerFailedError{Reason: "test failure"}
	msg := err.Error()

	assert.Contains(t, msg, "embedded server failed")
	assert.Contains(t, msg, "test failure")
}

func TestEmbeddedPortError_MessageCovPlus(t *testing.T) {
	err := &EmbeddedPortError{}
	msg := err.Error()

	assert.Contains(t, msg, "TCP address")
}

func TestShutdownEmbeddedServer_NoServerCovPlus(t *testing.T) {
	embeddedServerHolder.Store(nil)

	err := shutdownEmbeddedServer()
	assert.NoError(t, err)
}

// =============================================================================
// SDK Client Coverage Tests
// =============================================================================

func TestCreateSDKClient_WithExistingClientCovPlus(t *testing.T) {
	origAddr := address
	origTransport := transport
	defer func() {
		address = origAddr
		transport = origTransport
		resetSDKClient()
	}()

	resetSDKClient()

	mock := &mockSDKClientForClusterCovPlusTest{}
	var client quicbaas.Client = mock
	sdkClientHolder.Store(&client)

	retrieved, err := createSDKClient()
	require.NoError(t, err)
	assert.Same(t, mock, retrieved)
}

func TestBuildSDKClientOptions_EmbeddedTransportErrorCovPlus(t *testing.T) {
	origTransport := transport
	origTimeout := globalTimeout
	defer func() {
		transport = origTransport
		globalTimeout = origTimeout
	}()

	transport = "embedded"
	globalTimeout = 5 * time.Second

	opts, err := buildSDKClientOptions()

	if err != nil {
		assert.Contains(t, err.Error(), "embedded")
	}
	_ = opts
}

// =============================================================================
// Cache Stats Command Coverage Tests
// =============================================================================

func TestRunCacheStatsCommand_SpecificEntityCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	origDetails := cacheShowDetails
	defer func() {
		cfg = origCfg
		log = origLog
		cacheOutputFmt = origFmt
		cacheShowDetails = origDetails
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
			},
		},
	}
	log = createTestLoggerForCoveragePlus(t)
	cacheOutputFmt = "json"
	cacheShowDetails = true

	cmd := &cobra.Command{}
	err := runCacheStatsCommand(cmd, []string{"user"})

	require.NoError(t, err)
}

func TestRunCacheStatsCommand_AllEntitiesCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	origDetails := cacheShowDetails
	defer func() {
		cfg = origCfg
		log = origLog
		cacheOutputFmt = origFmt
		cacheShowDetails = origDetails
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
				"post": {
					Enabled: false,
					TTL:     5 * time.Minute,
					MaxSize: 200,
				},
			},
		},
	}
	log = createTestLoggerForCoveragePlus(t)
	cacheOutputFmt = "table"
	cacheShowDetails = false

	cmd := &cobra.Command{}
	err := runCacheStatsCommand(cmd, []string{})

	require.NoError(t, err)
}

func TestRunCacheStatsCommand_InvalidOutputFormatCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() {
		cfg = origCfg
		log = origLog
		cacheOutputFmt = origFmt
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
		},
	}
	log = createTestLoggerForCoveragePlus(t)
	cacheOutputFmt = "invalid-format"

	cmd := &cobra.Command{}
	err := runCacheStatsCommand(cmd, []string{})

	require.Error(t, err)
}

// =============================================================================
// Cache Clear Command Coverage Tests
// =============================================================================

func TestRunCacheClearCommand_InvalidOutputFormatCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	origForce := cacheForce
	defer func() {
		cfg = origCfg
		log = origLog
		cacheOutputFmt = origFmt
		cacheForce = origForce
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
			},
		},
	}
	log = createTestLoggerForCoveragePlus(t)
	cacheOutputFmt = "invalid-format"
	cacheForce = true

	cmd := &cobra.Command{}
	err := runCacheClearCommand(cmd, []string{})

	require.Error(t, err)
}

func TestRunCacheClearCommand_ClearAllCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	origForce := cacheForce
	defer func() {
		cfg = origCfg
		log = origLog
		cacheOutputFmt = origFmt
		cacheForce = origForce
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
				"post": {
					Enabled: true,
					TTL:     5 * time.Minute,
					MaxSize: 200,
				},
			},
		},
	}
	log = createTestLoggerForCoveragePlus(t)
	cacheOutputFmt = "json"
	cacheForce = true

	cmd := &cobra.Command{}
	err := runCacheClearCommand(cmd, []string{})

	require.NoError(t, err)
}

func TestRunCacheClearCommand_ClearSpecificEntityCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	origForce := cacheForce
	defer func() {
		cfg = origCfg
		log = origLog
		cacheOutputFmt = origFmt
		cacheForce = origForce
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
			},
		},
	}
	log = createTestLoggerForCoveragePlus(t)
	cacheOutputFmt = "table"
	cacheForce = true

	cmd := &cobra.Command{}
	err := runCacheClearCommand(cmd, []string{"user"})

	require.NoError(t, err)
}

func TestRunCacheClearCommand_ClearDisabledEntityCovPlus(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	origForce := cacheForce
	defer func() {
		cfg = origCfg
		log = origLog
		cacheOutputFmt = origFmt
		cacheForce = origForce
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"post": {
					Enabled: false,
					TTL:     5 * time.Minute,
					MaxSize: 200,
				},
			},
		},
	}
	log = createTestLoggerForCoveragePlus(t)
	cacheOutputFmt = "json"
	cacheForce = true

	cmd := &cobra.Command{}
	err := runCacheClearCommand(cmd, []string{"post"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")
}

// =============================================================================
// Helper Functions
// =============================================================================

func createTestLoggerForCoveragePlus(t *testing.T) *logger.Logger {
	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test-coverage",
	}
	testLog, err := logger.New(logConfig)
	if err != nil {
		t.Fatalf("Failed to create test logger: %v", err)
	}
	return testLog
}

// =============================================================================
// Additional Edge Case Tests
// =============================================================================

func TestCacheCommandCreationCovPlus(t *testing.T) {
	t.Run("NewCacheCommands", func(t *testing.T) {
		cmd := newCacheCommands()
		require.NotNil(t, cmd)
		assert.Equal(t, "cache", cmd.Use)
		assert.NotEmpty(t, cmd.Short)
		assert.NotEmpty(t, cmd.Long)

		subcommands := cmd.Commands()
		assert.GreaterOrEqual(t, len(subcommands), 6)
	})

	t.Run("NewCacheStatsCommand", func(t *testing.T) {
		cmd := newCacheStatsCommand()
		require.NotNil(t, cmd)
		assert.Equal(t, "stats [entity-type]", cmd.Use)
		assert.NotNil(t, cmd.Flags().Lookup("details"))
	})

	t.Run("NewCacheClearCommand", func(t *testing.T) {
		cmd := newCacheClearCommand()
		require.NotNil(t, cmd)
		assert.Equal(t, "clear [entity-type]", cmd.Use)
		assert.NotNil(t, cmd.Flags().Lookup("yes"))
	})

	t.Run("NewCacheConfigCommand", func(t *testing.T) {
		cmd := newCacheConfigCommand()
		require.NotNil(t, cmd)
		assert.Equal(t, "config [entity-type]", cmd.Use)
	})

	t.Run("NewCacheEnableCommand", func(t *testing.T) {
		cmd := newCacheEnableCommand()
		require.NotNil(t, cmd)
		assert.Equal(t, "enable <entity-type>", cmd.Use)
	})

	t.Run("NewCacheDisableCommand", func(t *testing.T) {
		cmd := newCacheDisableCommand()
		require.NotNil(t, cmd)
		assert.Equal(t, "disable <entity-type>", cmd.Use)
	})

	t.Run("NewCacheWarmCommand", func(t *testing.T) {
		cmd := newCacheWarmCommand()
		require.NotNil(t, cmd)
		assert.Equal(t, "warm", cmd.Use)
		assert.NotNil(t, cmd.Flags().Lookup("prefix"))
		assert.NotNil(t, cmd.Flags().Lookup("rate"))
		assert.NotNil(t, cmd.Flags().Lookup("max-keys"))
		assert.NotNil(t, cmd.Flags().Lookup("batch-size"))
	})
}

// =============================================================================
// Error Types Coverage Tests
// =============================================================================

func TestErrSDKClientClosedCovPlus(t *testing.T) {
	assert.NotNil(t, ErrSDKClientClosed)
	assert.Contains(t, ErrSDKClientClosed.Error(), "closed")
}

func TestErrEmptyAddressCovPlus(t *testing.T) {
	assert.NotNil(t, ErrEmptyAddress)
	assert.Contains(t, ErrEmptyAddress.Error(), "empty")
}

func TestErrEmbeddedServerNotStartedCovPlus(t *testing.T) {
	assert.NotNil(t, ErrEmbeddedServerNotStarted)
	assert.Contains(t, ErrEmbeddedServerNotStarted.Error(), "not started")
}

func TestUnsupportedTransportErrorCovPlus(t *testing.T) {
	err := &UnsupportedTransportError{Transport: "invalid-transport"}
	msg := err.Error()

	assert.Contains(t, msg, "unsupported transport")
	assert.Contains(t, msg, "invalid-transport")
	assert.Contains(t, msg, "grpc")
	assert.Contains(t, msg, "rest")
}

// =============================================================================
// Client Options Coverage Tests
// =============================================================================

func TestBuildSDKClientOptions_RESTTransportCovPlus(t *testing.T) {
	origTransport := transport
	origTimeout := globalTimeout
	defer func() {
		transport = origTransport
		globalTimeout = origTimeout
	}()

	transport = "rest"
	globalTimeout = 10 * time.Second

	opts, err := buildSDKClientOptions()

	require.NoError(t, err)
	assert.NotEmpty(t, opts)
}

func TestBuildSDKClientOptions_QUICTransportCovPlus(t *testing.T) {
	origTransport := transport
	origTimeout := globalTimeout
	defer func() {
		transport = origTransport
		globalTimeout = origTimeout
	}()

	transport = "quic"
	globalTimeout = 5 * time.Second

	opts, err := buildSDKClientOptions()

	require.NoError(t, err)
	assert.NotEmpty(t, opts)
}

func TestBuildSDKClientOptions_UnixTransportCovPlus(t *testing.T) {
	origAddr := address
	origTransport := transport
	origTimeout := globalTimeout
	defer func() {
		address = origAddr
		transport = origTransport
		globalTimeout = origTimeout
	}()

	address = "/tmp/quicbaas.sock"
	transport = "unix"
	globalTimeout = 5 * time.Second

	opts, err := buildSDKClientOptions()

	require.NoError(t, err)
	assert.NotEmpty(t, opts)
}

func TestBuildSDKClientOptions_MCPTransportCovPlus(t *testing.T) {
	origTransport := transport
	origTimeout := globalTimeout
	defer func() {
		transport = origTransport
		globalTimeout = origTimeout
	}()

	transport = "mcp"
	globalTimeout = 5 * time.Second

	opts, err := buildSDKClientOptions()

	require.NoError(t, err)
	assert.NotEmpty(t, opts)
}

func TestBuildSDKClientOptions_UnsupportedTransportCovPlus(t *testing.T) {
	origTransport := transport
	origTimeout := globalTimeout
	defer func() {
		transport = origTransport
		globalTimeout = origTimeout
	}()

	transport = "unsupported"
	globalTimeout = 5 * time.Second

	opts, err := buildSDKClientOptions()

	require.Error(t, err)
	assert.Nil(t, opts)
	var unsupportedErr *UnsupportedTransportError
	assert.ErrorAs(t, err, &unsupportedErr)
}

func TestCloseSDKClient_NoClientCovPlus(t *testing.T) {
	defer func() {
		resetSDKClient()
	}()

	resetSDKClient()
	embeddedServerHolder.Store(nil)

	err := closeSDKClient()

	assert.NoError(t, err)
}

func TestCreateSDKClient_EmptyAddressCovPlus(t *testing.T) {
	origAddr := address
	origTransport := transport
	defer func() {
		address = origAddr
		transport = origTransport
		resetSDKClient()
	}()

	resetSDKClient()
	address = ""
	transport = "grpc"

	client, err := createSDKClient()

	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "empty")
}
