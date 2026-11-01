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

func TestInitializeCacheRegistry(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log

	defer func() {
		cfg = origCfg
		log = origLog
	}()

	t.Run("success", func(t *testing.T) {
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

		assert.True(t, registry.GetEntityCache("user").IsEnabled())
	})

	t.Run("nil config", func(t *testing.T) {
		cfg = nil
		log = origLog

		registry, err := initializeCacheRegistry()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "configuration not initialized")
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
		require.Error(t, err)
		assert.Contains(t, err.Error(), "logger not initialized")
		assert.Nil(t, registry)
	})
}

func TestCacheCommandHelp(t *testing.T) {
	tests := []struct {
		name   string
		long   string
		expect []string
	}{
		{
			name:   "cache stats help",
			long:   newCacheStatsCommand().Long,
			expect: []string{"statistics", "hits", "misses", "hit rate"},
		},
		{
			name:   "cache warm help",
			long:   newCacheWarmCommand().Long,
			expect: []string{"warm", "prefix", "storage"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, expected := range tt.expect {
				assert.Contains(t, strings.ToLower(tt.long), strings.ToLower(expected))
			}
		})
	}
}

// Helper function to parse output format.
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
	origFmt := cacheOutputFmt
	origDetails := cacheShowDetails

	defer func() {
		cfg = origCfg
		log = origLog
		cacheOutputFmt = origFmt
		cacheShowDetails = origDetails
	}()

	log = createTestLoggerForCoveragePlus(t)

	tests := []struct {
		name         string
		args         []string
		disabledPost bool
		outputFormat string
		showDetails  bool
		expectError  bool
	}{
		{
			name:         "stats for all entities with table format",
			args:         []string{},
			outputFormat: "table",
		},
		{
			name:         "stats for all entities with a disabled entity",
			args:         []string{},
			disabledPost: true,
			outputFormat: "table",
		},
		{
			name:         "stats for specific entity",
			args:         []string{"user"},
			outputFormat: "table",
		},
		{
			name:         "stats with json format",
			args:         []string{},
			outputFormat: "json",
		},
		{
			name:         "stats with yaml format",
			args:         []string{},
			outputFormat: "yaml",
		},
		{
			name:         "stats with details",
			args:         []string{"user"},
			outputFormat: "table",
			showDetails:  true,
		},
		{
			name:         "stats with details json",
			args:         []string{"user"},
			outputFormat: "json",
			showDetails:  true,
		},
		{
			name:         "invalid output format",
			args:         []string{},
			outputFormat: "invalid-format",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entities := map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
			}
			if tt.disabledPost {
				entities["post"] = config.EntityCacheConfig{
					Enabled: false,
					TTL:     5 * time.Minute,
					MaxSize: 200,
				}
			}
			cfg = &config.Config{
				Cache: config.EnhancedCacheConfig{
					Enabled:         true,
					DefaultTTL:      5 * time.Minute,
					MaxSize:         1000,
					CleanupInterval: 1 * time.Minute,
					Entities:        entities,
				},
			}
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
	origFmt := cacheOutputFmt
	origForce := cacheForce

	defer func() {
		cfg = origCfg
		log = origLog
		cacheOutputFmt = origFmt
		cacheForce = origForce
	}()

	log = createTestLoggerForCoveragePlus(t)

	tests := []struct {
		name         string
		args         []string
		postDisabled bool
		outputFormat string
		expectError  bool
		errorMsg     string
	}{
		{
			name:         "clear all with force",
			args:         []string{},
			outputFormat: "table",
		},
		{
			name:         "clear specific entity with force",
			args:         []string{"user"},
			outputFormat: "table",
		},
		{
			name:         "clear specific entity json",
			args:         []string{"user"},
			outputFormat: "json",
		},
		{
			name:         "clear second entity json",
			args:         []string{"post"},
			outputFormat: "json",
		},
		{
			name:         "clear with json output",
			args:         []string{},
			outputFormat: "json",
		},
		{
			name:         "clear with yaml output",
			args:         []string{},
			outputFormat: "yaml",
		},
		{
			name:         "clear disabled entity",
			args:         []string{"post"},
			postDisabled: true,
			outputFormat: "table",
			expectError:  true,
			errorMsg:     "caching is not enabled",
		},
		{
			name:         "invalid output format",
			args:         []string{},
			outputFormat: "invalid-format",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
							Enabled: !tt.postDisabled,
							TTL:     5 * time.Minute,
							MaxSize: 200,
						},
					},
				},
			}
			cmd := newCacheClearCommand()

			cacheForce = true
			cacheOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
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
	origFmt := cacheOutputFmt

	defer func() {
		cfg = origCfg
		log = origLog
		cacheOutputFmt = origFmt
	}()

	log = createTestLoggerForCoveragePlus(t)

	tests := []struct {
		name         string
		args         []string
		nilConfig    bool
		outputFormat string
		expectError  bool
		errorMsg     string
	}{
		{
			name:         "config for all entities",
			args:         []string{},
			outputFormat: "table",
		},
		{
			name:         "config for specific entity",
			args:         []string{"user"},
			outputFormat: "table",
		},
		{
			name:         "config with json output",
			args:         []string{},
			outputFormat: "json",
		},
		{
			name:         "config with yaml output",
			args:         []string{},
			outputFormat: "yaml",
		},
		{
			name:         "nil config",
			args:         []string{},
			nilConfig:    true,
			outputFormat: "table",
			expectError:  true,
			errorMsg:     "configuration not initialized",
		},
		{
			name:         "invalid output format",
			args:         []string{},
			outputFormat: "invalid",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			if tt.nilConfig {
				cfg = nil
			}
			cmd := newCacheConfigCommand()

			cacheOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
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
	origFmt := cacheOutputFmt

	defer func() {
		cfg = origCfg
		log = origLog
		cacheOutputFmt = origFmt
	}()

	log = createTestLoggerForCoveragePlus(t)

	tests := []struct {
		name          string
		entity        string
		entityEnabled bool
		outputFormat  string
		expectError   bool
	}{
		{
			name:         "enable disabled entity",
			entity:       "post",
			outputFormat: "table",
		},
		{
			name:         "enable disabled entity json",
			entity:       "article",
			outputFormat: "json",
		},
		{
			name:          "enable already enabled entity",
			entity:        "user",
			entityEnabled: true,
			outputFormat:  "table",
		},
		{
			name:         "invalid output format",
			entity:       "test",
			outputFormat: "invalid",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg = &config.Config{
				Cache: config.EnhancedCacheConfig{
					Enabled:         true,
					DefaultTTL:      5 * time.Minute,
					MaxSize:         1000,
					CleanupInterval: 1 * time.Minute,
					Entities: map[string]config.EntityCacheConfig{
						tt.entity: {
							Enabled: tt.entityEnabled,
							TTL:     5 * time.Minute,
							MaxSize: 200,
						},
					},
				},
			}
			cmd := newCacheEnableCommand()

			cacheOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, []string{tt.entity})

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunCacheEnableCommandJSON tests enabling cache with JSON output separately.
// TestRunCacheDisableCommand tests the runCacheDisableCommand function.
func TestRunCacheDisableCommand(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt

	defer func() {
		cfg = origCfg
		log = origLog
		cacheOutputFmt = origFmt
	}()

	log = createTestLoggerForCoveragePlus(t)

	tests := []struct {
		name          string
		entity        string
		entityEnabled bool
		outputFormat  string
		expectError   bool
	}{
		{
			name:          "disable enabled entity",
			entity:        "user",
			entityEnabled: true,
			outputFormat:  "table",
		},
		{
			name:          "disable enabled entity json",
			entity:        "profile",
			entityEnabled: true,
			outputFormat:  "json",
		},
		{
			name:          "disable enabled entity yaml",
			entity:        "user",
			entityEnabled: true,
			outputFormat:  "yaml",
		},
		{
			name:         "disable already disabled entity",
			entity:       "post",
			outputFormat: "json",
		},
		{
			name:          "invalid output format",
			entity:        "test",
			entityEnabled: true,
			outputFormat:  "invalid",
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg = &config.Config{
				Cache: config.EnhancedCacheConfig{
					Enabled:         true,
					DefaultTTL:      5 * time.Minute,
					MaxSize:         1000,
					CleanupInterval: 1 * time.Minute,
					Entities: map[string]config.EntityCacheConfig{
						tt.entity: {
							Enabled: tt.entityEnabled,
							TTL:     10 * time.Minute,
							MaxSize: 500,
						},
					},
				},
			}
			cmd := newCacheDisableCommand()

			cacheOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, []string{tt.entity})

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunCacheDisableCommandJSON tests disabling cache with JSON output separately.
// TestCacheWarmCommand tests the newCacheWarmCommand function.
func TestCacheWarmCommand(t *testing.T) {
	cmd := newCacheWarmCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "warm", cmd.Use)
	assert.Contains(t, cmd.Long, "Pre-populate the cache")

	// Verify flags are registered with their defaults
	prefixFlag := cmd.Flags().Lookup("prefix")
	require.NotNil(t, prefixFlag)
	assert.Equal(t, "stringArray", prefixFlag.Value.Type())

	rateFlag := cmd.Flags().Lookup("rate")
	require.NotNil(t, rateFlag)
	assert.Equal(t, "int", rateFlag.Value.Type())
	assert.Equal(t, "0", rateFlag.DefValue)

	maxKeysFlag := cmd.Flags().Lookup("max-keys")
	require.NotNil(t, maxKeysFlag)
	assert.Equal(t, "int", maxKeysFlag.Value.Type())
	assert.Equal(t, "0", maxKeysFlag.DefValue)

	batchSizeFlag := cmd.Flags().Lookup("batch-size")
	require.NotNil(t, batchSizeFlag)
	assert.Equal(t, "100", batchSizeFlag.DefValue)
}

// TestCacheWarmCommandNoPrefixes tests warm command with no prefixes.
// TestCacheWarmCommandValidation tests warm command validation.
func TestCacheWarmCommandValidation(t *testing.T) {
	// Save original values
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

	cacheCfg := &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
		},
	}

	tests := []struct {
		name         string
		cfg          *config.Config
		nilLog       bool
		prefixes     []string
		outputFormat string
		errContains  string
	}{
		{
			name:        "nil config",
			cfg:         nil,
			prefixes:    []string{"test:"},
			errContains: "configuration not initialized",
		},
		{
			name:        "nil logger",
			cfg:         cacheCfg,
			nilLog:      true,
			prefixes:    []string{"test:"},
			errContains: "logger not initialized",
		},
		{
			name:        "no prefixes",
			cfg:         cacheCfg,
			prefixes:    nil,
			errContains: "no prefixes specified",
		},
		{
			name:        "empty prefixes",
			cfg:         cacheCfg,
			prefixes:    []string{},
			errContains: "no prefixes specified",
		},
		{
			name:         "invalid output format",
			cfg:          cacheCfg,
			prefixes:     []string{"test:"},
			outputFormat: "invalid-format",
			errContains:  "invalid-format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Creating the command resets the flag globals to their
			// defaults, so the test inputs are set after it.
			cmd := newCacheWarmCommand()

			cfg = tt.cfg
			log = createTestLoggerForCoveragePlus(t)
			if tt.nilLog {
				log = nil
			}
			cacheWarmPrefixes = tt.prefixes
			cacheOutputFmt = tt.outputFormat

			err := runCacheWarmCommand(cmd, []string{})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

// TestCacheWarmFlags tests cache warm command flags.
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
// TestRunCacheClearCommandOutputFormats tests different output formats.
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
// =============================================================================
// Test Helper for Config
// =============================================================================

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
func (m *mockSDKClientForClusterCovPlusTest) CustodianGroup() (quicbaas.QRDBCustodianGroupClient, error) {
	return nil, nil
}
func (m *mockSDKClientForClusterCovPlusTest) Share() (quicbaas.QRDBShareClient, error) {
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

func TestCreateSDKClient(t *testing.T) {
	origAddr := address
	origTransport := transport
	defer func() {
		address = origAddr
		transport = origTransport
		resetSDKClient()
	}()

	t.Run("returns existing client", func(t *testing.T) {
		resetSDKClient()

		mock := &mockSDKClientForClusterCovPlusTest{}
		var client quicbaas.Client = mock
		sdkClientHolder.Store(&client)

		retrieved, err := createSDKClient()
		require.NoError(t, err)
		assert.Same(t, mock, retrieved)
	})

	t.Run("empty address", func(t *testing.T) {
		resetSDKClient()
		address = ""
		transport = "grpc"

		client, err := createSDKClient()

		require.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "empty")
	})
}

func TestBuildSDKClientOptions(t *testing.T) {
	origAddr := address
	origTransport := transport
	origTimeout := globalTimeout
	defer func() {
		address = origAddr
		transport = origTransport
		globalTimeout = origTimeout
	}()

	tests := []struct {
		name      string
		transport string
		address   string
		timeout   time.Duration
		// mayFail marks transports whose setup can fail on the host; a
		// failure must still name the transport.
		mayFail     bool
		expectError bool
	}{
		{name: "rest", transport: "rest", timeout: 10 * time.Second},
		{name: "quic", transport: "quic", timeout: 5 * time.Second},
		{name: "unix", transport: "unix", address: "/tmp/quicbaas.sock", timeout: 5 * time.Second},
		{name: "mcp", transport: "mcp", timeout: 5 * time.Second},
		{name: "embedded", transport: "embedded", timeout: 5 * time.Second, mayFail: true},
		{name: "unsupported", transport: "unsupported", timeout: 5 * time.Second, expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			address = origAddr
			if tt.address != "" {
				address = tt.address
			}
			transport = tt.transport
			globalTimeout = tt.timeout

			opts, err := buildSDKClientOptions()

			switch {
			case tt.expectError:
				require.Error(t, err)
				assert.Nil(t, opts)
				var unsupportedErr *UnsupportedTransportError
				require.ErrorAs(t, err, &unsupportedErr)
				msg := err.Error()
				assert.Contains(t, msg, "unsupported transport")
				assert.Contains(t, msg, tt.transport)
				assert.Contains(t, msg, "grpc")
				assert.Contains(t, msg, "rest")
			case tt.mayFail && err != nil:
				assert.Contains(t, err.Error(), tt.transport)
			default:
				require.NoError(t, err)
				assert.NotEmpty(t, opts)
			}
		})
	}
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
}

// =============================================================================
// Client Options Coverage Tests
// =============================================================================

func TestCloseSDKClient_NoClientCovPlus(t *testing.T) {
	defer func() {
		resetSDKClient()
	}()

	resetSDKClient()
	embeddedServerHolder.Store(nil)

	err := closeSDKClient()

	assert.NoError(t, err)
}
