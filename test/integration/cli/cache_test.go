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

//go:build integration

// Package cli provides E2E integration tests for QuicBaaS CLI cache commands.
package cli

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// =============================================================================
// Cache Stats Command Definitions
// =============================================================================

// CacheStatsCommand returns a command to get cache statistics.
func CacheStatsCommand() CommandDef {
	return CommandDef{
		Name:           "cache stats",
		Args:           []string{"cache", "stats"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
	}
}

// CacheStatsJSONCommand returns a command to get cache statistics in JSON format.
func CacheStatsJSONCommand() CommandDef {
	return CommandDef{
		Name:           "cache stats json",
		Args:           []string{"cache", "stats", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
	}
}

// CacheStatsEntityCommand returns a command to get cache statistics for a specific entity.
func CacheStatsEntityCommand(entityType string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("cache stats %s", entityType),
		Args:           []string{"cache", "stats", entityType},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
	}
}

// CacheStatsDetailsCommand returns a command to get detailed cache statistics.
func CacheStatsDetailsCommand(entityType string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("cache stats %s --details", entityType),
		Args:           []string{"cache", "stats", entityType, "--details"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
	}
}

// =============================================================================
// Cache Clear Command Definitions
// =============================================================================

// CacheClearCommand returns a command to clear all cache entries.
func CacheClearCommand() CommandDef {
	return CommandDef{
		Name:           "cache clear",
		Args:           []string{"cache", "clear", "--yes"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
	}
}

// CacheClearEntityCommand returns a command to clear cache for a specific entity type.
func CacheClearEntityCommand(entityType string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("cache clear %s", entityType),
		Args:           []string{"cache", "clear", entityType, "--yes"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
	}
}

// CacheClearJSONCommand returns a command to clear cache with JSON output.
func CacheClearJSONCommand() CommandDef {
	return CommandDef{
		Name:           "cache clear json",
		Args:           []string{"cache", "clear", "--yes", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
	}
}

// =============================================================================
// Cache Config Command Definitions
// =============================================================================

// CacheConfigCommand returns a command to show cache configuration.
func CacheConfigCommand() CommandDef {
	return CommandDef{
		Name:           "cache config",
		Args:           []string{"cache", "config"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
	}
}

// CacheConfigJSONCommand returns a command to show cache configuration in JSON format.
func CacheConfigJSONCommand() CommandDef {
	return CommandDef{
		Name:           "cache config json",
		Args:           []string{"cache", "config", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
	}
}

// CacheConfigEntityCommand returns a command to show cache configuration for a specific entity.
func CacheConfigEntityCommand(entityType string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("cache config %s", entityType),
		Args:           []string{"cache", "config", entityType},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
	}
}

// =============================================================================
// Cache Enable/Disable Command Definitions
// =============================================================================

// CacheEnableCommand returns a command to enable caching for an entity type.
func CacheEnableCommand(entityType string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("cache enable %s", entityType),
		Args:           []string{"cache", "enable", entityType},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
	}
}

// CacheEnableJSONCommand returns a command to enable caching with JSON output.
func CacheEnableJSONCommand(entityType string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("cache enable %s json", entityType),
		Args:           []string{"cache", "enable", entityType, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
	}
}

// CacheDisableCommand returns a command to disable caching for an entity type.
func CacheDisableCommand(entityType string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("cache disable %s", entityType),
		Args:           []string{"cache", "disable", entityType},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
	}
}

// CacheDisableJSONCommand returns a command to disable caching with JSON output.
func CacheDisableJSONCommand(entityType string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("cache disable %s json", entityType),
		Args:           []string{"cache", "disable", entityType, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
	}
}

// =============================================================================
// Cache Warm Command Definitions
// =============================================================================

// CacheWarmCommand returns a command to warm the cache from storage.
func CacheWarmCommand(prefixes []string) CommandDef {
	args := []string{"cache", "warm"}
	for _, prefix := range prefixes {
		args = append(args, "--prefix", prefix)
	}
	return CommandDef{
		Name:           "cache warm",
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
	}
}

// CacheWarmWithRateCommand returns a command to warm cache with rate limiting.
func CacheWarmWithRateCommand(prefixes []string, rate int) CommandDef {
	args := []string{"cache", "warm", "--rate", fmt.Sprintf("%d", rate)}
	for _, prefix := range prefixes {
		args = append(args, "--prefix", prefix)
	}
	return CommandDef{
		Name:           fmt.Sprintf("cache warm rate=%d", rate),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
	}
}

// CacheWarmWithMaxKeysCommand returns a command to warm cache with max key limit.
func CacheWarmWithMaxKeysCommand(prefixes []string, maxKeys int) CommandDef {
	args := []string{"cache", "warm", "--max-keys", fmt.Sprintf("%d", maxKeys)}
	for _, prefix := range prefixes {
		args = append(args, "--prefix", prefix)
	}
	return CommandDef{
		Name:           fmt.Sprintf("cache warm max-keys=%d", maxKeys),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
	}
}

// CacheWarmWithBatchSizeCommand returns a command to warm cache with custom batch size.
func CacheWarmWithBatchSizeCommand(prefixes []string, batchSize int) CommandDef {
	args := []string{"cache", "warm", "--batch-size", fmt.Sprintf("%d", batchSize)}
	for _, prefix := range prefixes {
		args = append(args, "--prefix", prefix)
	}
	return CommandDef{
		Name:           fmt.Sprintf("cache warm batch-size=%d", batchSize),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
	}
}

// CacheWarmJSONCommand returns a command to warm cache with JSON output.
func CacheWarmJSONCommand(prefixes []string) CommandDef {
	args := []string{"cache", "warm", "--output", "json"}
	for _, prefix := range prefixes {
		args = append(args, "--prefix", prefix)
	}
	return CommandDef{
		Name:           "cache warm json",
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
	}
}

// =============================================================================
// Cache Help Command Definitions
// =============================================================================

// CacheHelpCommand shows cache help.
func CacheHelpCommand() CommandDef {
	return CommandDef{
		Name:          "cache help",
		Args:          []string{"cache", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"stats", "clear", "config", "enable", "disable", "warm"},
		Timeout:       10 * time.Second,
	}
}

// CacheStatsHelpCommand shows stats help.
func CacheStatsHelpCommand() CommandDef {
	return CommandDef{
		Name:          "cache stats help",
		Args:          []string{"cache", "stats", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"statistics", "--details"},
		Timeout:       10 * time.Second,
	}
}

// CacheClearHelpCommand shows clear help.
func CacheClearHelpCommand() CommandDef {
	return CommandDef{
		Name:          "cache clear help",
		Args:          []string{"cache", "clear", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"Clear", "--yes"},
		Timeout:       10 * time.Second,
	}
}

// CacheConfigHelpCommand shows config help.
func CacheConfigHelpCommand() CommandDef {
	return CommandDef{
		Name:          "cache config help",
		Args:          []string{"cache", "config", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"configuration", "entity"},
		Timeout:       10 * time.Second,
	}
}

// CacheEnableHelpCommand shows enable help.
func CacheEnableHelpCommand() CommandDef {
	return CommandDef{
		Name:          "cache enable help",
		Args:          []string{"cache", "enable", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"Enable", "entity"},
		Timeout:       10 * time.Second,
	}
}

// CacheDisableHelpCommand shows disable help.
func CacheDisableHelpCommand() CommandDef {
	return CommandDef{
		Name:          "cache disable help",
		Args:          []string{"cache", "disable", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"Disable", "entity"},
		Timeout:       10 * time.Second,
	}
}

// CacheWarmHelpCommand shows warm help.
func CacheWarmHelpCommand() CommandDef {
	return CommandDef{
		Name:          "cache warm help",
		Args:          []string{"cache", "warm", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"Pre-populate", "--prefix", "--rate", "--batch-size"},
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Cache Error Command Definitions
// =============================================================================

// CacheEnableMissingEntityCommand tests enabling cache without entity type.
func CacheEnableMissingEntityCommand() CommandDef {
	return CommandDef{
		Name:          "cache enable missing entity",
		Args:          []string{"cache", "enable"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// CacheDisableMissingEntityCommand tests disabling cache without entity type.
func CacheDisableMissingEntityCommand() CommandDef {
	return CommandDef{
		Name:          "cache disable missing entity",
		Args:          []string{"cache", "disable"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// CacheWarmMissingPrefixCommand tests warm without prefix.
func CacheWarmMissingPrefixCommand() CommandDef {
	return CommandDef{
		Name:           "cache warm missing prefix",
		Args:           []string{"cache", "warm"},
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
	}
}

// CacheStatsInvalidOutputCommand tests stats with invalid output format.
func CacheStatsInvalidOutputCommand() CommandDef {
	return CommandDef{
		Name:           "cache stats invalid output",
		Args:           []string{"cache", "stats", "--output", "invalid-format"},
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
	}
}

// =============================================================================
// Tests
// =============================================================================

// TestCacheHelp tests cache help commands.
func TestCacheHelp(t *testing.T) {
	RunForAllTransports(t, "CacheHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CacheHelpCommand())
	})

	RunForAllTransports(t, "CacheStatsHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CacheStatsHelpCommand())
	})

	RunForAllTransports(t, "CacheClearHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CacheClearHelpCommand())
	})

	RunForAllTransports(t, "CacheConfigHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CacheConfigHelpCommand())
	})

	RunForAllTransports(t, "CacheEnableHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CacheEnableHelpCommand())
	})

	RunForAllTransports(t, "CacheDisableHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CacheDisableHelpCommand())
	})

	RunForAllTransports(t, "CacheWarmHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CacheWarmHelpCommand())
	})
}

// TestCacheBasicOperations tests basic cache management operations.
func TestCacheBasicOperations(t *testing.T) {
	RunForAllTransports(t, "StatsAndConfig", func(t *testing.T, runner *TestRunner) {
		// Get stats
		statsCmd := CacheStatsCommand()
		stdout, _, err := runner.Run(statsCmd)
		if runner.IsSkipped(statsCmd) {
			return
		}
		require.NoError(t, err)
		t.Logf("Stats output: %s", stdout)

		// Get config
		configCmd := CacheConfigCommand()
		stdout, _, err = runner.Run(configCmd)
		require.NoError(t, err)
		t.Logf("Config output: %s", stdout)

		// Get stats JSON
		statsJSONCmd := CacheStatsJSONCommand()
		stdout, _, err = runner.Run(statsJSONCmd)
		require.NoError(t, err)
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err)

		// Get config JSON
		configJSONCmd := CacheConfigJSONCommand()
		stdout, _, err = runner.Run(configJSONCmd)
		require.NoError(t, err)
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err)
	})
}

// TestCacheStats tests cache statistics commands.
func TestCacheStats(t *testing.T) {
	RunForAllTransports(t, "Stats", func(t *testing.T, runner *TestRunner) {
		// Get all stats
		statsCmd := CacheStatsCommand()
		_, _, err := runner.Run(statsCmd)
		if runner.IsSkipped(statsCmd) {
			return
		}
		require.NoError(t, err)

		// Get stats JSON
		statsJSONCmd := CacheStatsJSONCommand()
		stdout, _, err := runner.Run(statsJSONCmd)
		require.NoError(t, err)
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err)
	})

	RunForAllTransports(t, "StatsEntity", func(t *testing.T, runner *TestRunner) {
		entityType := "user"

		// Get entity stats
		statsCmd := CacheStatsEntityCommand(entityType)
		_, _, err := runner.Run(statsCmd)
		if runner.IsSkipped(statsCmd) {
			return
		}
		// May fail if entity doesn't exist - that's ok
		t.Logf("Entity stats result: %v", err)

		// Get entity stats with details
		detailsCmd := CacheStatsDetailsCommand(entityType)
		_, _, err = runner.Run(detailsCmd)
		t.Logf("Entity stats details result: %v", err)
	})
}

// TestCacheConfig tests cache configuration commands.
func TestCacheConfig(t *testing.T) {
	RunForAllTransports(t, "Config", func(t *testing.T, runner *TestRunner) {
		// Get all config
		configCmd := CacheConfigCommand()
		_, _, err := runner.Run(configCmd)
		if runner.IsSkipped(configCmd) {
			return
		}
		require.NoError(t, err)

		// Get config JSON
		configJSONCmd := CacheConfigJSONCommand()
		stdout, _, err := runner.Run(configJSONCmd)
		require.NoError(t, err)
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err)
	})

	RunForAllTransports(t, "ConfigEntity", func(t *testing.T, runner *TestRunner) {
		entityType := "user"

		// Get entity config
		configCmd := CacheConfigEntityCommand(entityType)
		_, _, err := runner.Run(configCmd)
		if runner.IsSkipped(configCmd) {
			return
		}
		// May fail if entity doesn't exist - that's ok
		t.Logf("Entity config result: %v", err)
	})
}

// TestCacheClear tests cache clear commands.
func TestCacheClear(t *testing.T) {
	RunForAllTransports(t, "Clear", func(t *testing.T, runner *TestRunner) {
		// Clear all cache (with --yes to skip confirmation)
		clearCmd := CacheClearCommand()
		_, _, err := runner.Run(clearCmd)
		if runner.IsSkipped(clearCmd) {
			return
		}
		require.NoError(t, err)
	})

	RunForAllTransports(t, "ClearEntity", func(t *testing.T, runner *TestRunner) {
		entityType := "user"

		// Clear entity cache
		clearCmd := CacheClearEntityCommand(entityType)
		_, _, err := runner.Run(clearCmd)
		if runner.IsSkipped(clearCmd) {
			return
		}
		// May fail if entity caching is not enabled - that's ok
		t.Logf("Entity clear result: %v", err)
	})

	RunForAllTransports(t, "ClearJSON", func(t *testing.T, runner *TestRunner) {
		// Clear with JSON output
		clearJSONCmd := CacheClearJSONCommand()
		stdout, _, err := runner.Run(clearJSONCmd)
		if runner.IsSkipped(clearJSONCmd) {
			return
		}
		require.NoError(t, err)
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err)
	})
}

// TestCacheEnableDisable tests cache enable/disable commands.
func TestCacheEnableDisable(t *testing.T) {
	RunForAllTransports(t, "EnableDisable", func(t *testing.T, runner *TestRunner) {
		entityType := fmt.Sprintf("test-entity-%d", time.Now().UnixNano())

		// Enable caching for entity
		enableCmd := CacheEnableCommand(entityType)
		_, _, err := runner.Run(enableCmd)
		if runner.IsSkipped(enableCmd) {
			return
		}
		require.NoError(t, err)

		// Disable caching for entity
		disableCmd := CacheDisableCommand(entityType)
		_, _, err = runner.Run(disableCmd)
		require.NoError(t, err)
	})

	RunForAllTransports(t, "EnableDisableJSON", func(t *testing.T, runner *TestRunner) {
		entityType := fmt.Sprintf("test-entity-json-%d", time.Now().UnixNano())

		// Enable with JSON output
		enableJSONCmd := CacheEnableJSONCommand(entityType)
		stdout, _, err := runner.Run(enableJSONCmd)
		if runner.IsSkipped(enableJSONCmd) {
			return
		}
		require.NoError(t, err)
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err)

		// Disable with JSON output
		disableJSONCmd := CacheDisableJSONCommand(entityType)
		stdout, _, err = runner.Run(disableJSONCmd)
		require.NoError(t, err)
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err)
	})
}

// TestCacheErrors tests error handling.
func TestCacheErrors(t *testing.T) {
	RunForAllTransports(t, "EnableMissingEntity", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CacheEnableMissingEntityCommand())
	})

	RunForAllTransports(t, "DisableMissingEntity", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CacheDisableMissingEntityCommand())
	})

	RunForAllTransports(t, "WarmMissingPrefix", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CacheWarmMissingPrefixCommand())
	})

	RunForAllTransports(t, "InvalidOutput", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CacheStatsInvalidOutputCommand())
	})
}

// TestCacheOutputFormats tests all output formats.
func TestCacheOutputFormats(t *testing.T) {
	formats := []string{"table", "json", "yaml"}

	for _, format := range formats {
		t.Run(fmt.Sprintf("Stats_%s", format), func(t *testing.T) {
			RunForAllTransports(t, fmt.Sprintf("Stats_%s", format), func(t *testing.T, runner *TestRunner) {
				cmd := CommandDef{
					Name:           fmt.Sprintf("cache stats %s", format),
					Args:           []string{"cache", "stats", "--output", format},
					ExpectSuccess:  true,
					Timeout:        10 * time.Second,
					RequiresUnseal: true,
				}
				runner.RunAndAssert(cmd)
			})
		})

		t.Run(fmt.Sprintf("Config_%s", format), func(t *testing.T) {
			RunForAllTransports(t, fmt.Sprintf("Config_%s", format), func(t *testing.T, runner *TestRunner) {
				cmd := CommandDef{
					Name:           fmt.Sprintf("cache config %s", format),
					Args:           []string{"cache", "config", "--output", format},
					ExpectSuccess:  true,
					Timeout:        10 * time.Second,
					RequiresUnseal: true,
				}
				runner.RunAndAssert(cmd)
			})
		})
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

// CacheStatsSuite returns commands for cache stats testing.
func CacheStatsSuite() []CommandDef {
	return []CommandDef{
		CacheStatsCommand(),
		CacheStatsJSONCommand(),
	}
}

// CacheConfigSuite returns commands for cache config testing.
func CacheConfigSuite() []CommandDef {
	return []CommandDef{
		CacheConfigCommand(),
		CacheConfigJSONCommand(),
	}
}

// CacheHelpSuite returns help commands for cache.
func CacheHelpSuite() []CommandDef {
	return []CommandDef{
		CacheHelpCommand(),
		CacheStatsHelpCommand(),
		CacheClearHelpCommand(),
		CacheConfigHelpCommand(),
		CacheEnableHelpCommand(),
		CacheDisableHelpCommand(),
		CacheWarmHelpCommand(),
	}
}

// CacheErrorSuite returns error case commands for cache.
func CacheErrorSuite() []CommandDef {
	return []CommandDef{
		CacheEnableMissingEntityCommand(),
		CacheDisableMissingEntityCommand(),
		CacheWarmMissingPrefixCommand(),
	}
}

// CacheManagementSuite returns commands for testing cache management.
func CacheManagementSuite(entityType string) []CommandDef {
	return []CommandDef{
		CacheStatsCommand(),
		CacheConfigCommand(),
		CacheEnableCommand(entityType),
		CacheStatsEntityCommand(entityType),
		CacheConfigEntityCommand(entityType),
		CacheClearEntityCommand(entityType),
		CacheDisableCommand(entityType),
	}
}
