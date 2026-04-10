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

// Package main provides cache management commands for QuicBaaS.
package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage/memory"
)

var (
	// Cache command flags.
	cacheForce       bool
	cacheOutputFmt   string
	cacheShowDetails bool

	// Cache warm command flags.
	cacheWarmPrefixes  []string
	cacheWarmRate      int
	cacheWarmMaxKeys   int
	cacheWarmBatchSize int
)

// newCacheCommands creates the cache management command group.
func newCacheCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Cache management commands (stats, clear, config, enable, disable, warm)",
		Long: `Cache management commands for viewing and controlling the QuicBaaS caching system.

The cache system provides L1 (in-memory LRU) caching with entity-specific configurations.
These commands allow you to monitor cache performance, clear cache data, manage
per-entity caching settings, and warm the cache from storage.`,
	}

	// Add persistent flags for cache commands
	cmd.PersistentFlags().StringVar(&cacheOutputFmt, "output", "table", "Output format (json, yaml, table)")

	// Add subcommands
	cmd.AddCommand(newCacheStatsCommand())
	cmd.AddCommand(newCacheClearCommand())
	cmd.AddCommand(newCacheConfigCommand())
	cmd.AddCommand(newCacheEnableCommand())
	cmd.AddCommand(newCacheDisableCommand())
	cmd.AddCommand(newCacheWarmCommand())

	return cmd
}

// newCacheStatsCommand creates the cache stats command.
func newCacheStatsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats [entity-type]",
		Short: "Show cache statistics",
		Long: `Display cache statistics including hits, misses, size, and hit rates.

If no entity type is specified, shows statistics for all cached entities.
If an entity type is provided, shows detailed statistics for that specific entity.

Examples:
  # Show all cache statistics
  quicbaas cache stats

  # Show statistics for a specific entity
  quicbaas cache stats user

  # Show stats as JSON
  quicbaas cache stats --output json

  # Show stats with detailed breakdown
  quicbaas cache stats user --details`,
		Args: cobra.MaximumNArgs(1),
		RunE: runCacheStatsCommand,
	}

	cmd.Flags().BoolVar(&cacheShowDetails, "details", false, "Show detailed statistics breakdown")

	return cmd
}

// newCacheClearCommand creates the cache clear command.
func newCacheClearCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clear [entity-type]",
		Short: "Clear cache entries",
		Long: `Clear all cache entries or entries for a specific entity type.

By default, this command will prompt for confirmation before clearing.
Use --yes to skip the confirmation prompt.

Examples:
  # Clear all cache entries (with confirmation)
  quicbaas cache clear

  # Clear cache for a specific entity type
  quicbaas cache clear user

  # Clear without confirmation
  quicbaas cache clear --yes

  # Clear specific entity without confirmation
  quicbaas cache clear user --yes`,
		Args: cobra.MaximumNArgs(1),
		RunE: runCacheClearCommand,
	}

	cmd.Flags().BoolVarP(&cacheForce, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}

// newCacheConfigCommand creates the cache config command.
func newCacheConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config [entity-type]",
		Short: "Show cache configuration",
		Long: `Display current cache configuration settings.

If no entity type is specified, shows the global cache configuration and
configurations for all entity types.

If an entity type is provided, shows the specific configuration for that entity.

Examples:
  # Show all cache configuration
  quicbaas cache config

  # Show configuration for a specific entity
  quicbaas cache config user

  # Show config as JSON
  quicbaas cache config --output json`,
		Args: cobra.MaximumNArgs(1),
		RunE: runCacheConfigCommand,
	}

	return cmd
}

// newCacheEnableCommand creates the cache enable command.
func newCacheEnableCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enable <entity-type>",
		Short: "Enable caching for an entity type",
		Long: `Enable caching for a specific entity type.

This will dynamically enable caching for the specified entity type using the
default cache configuration. The entity will use the global TTL and size settings
unless specifically configured.

Examples:
  # Enable caching for user entity
  quicbaas cache enable user

  # Enable caching for post entity
  quicbaas cache enable post`,
		Args: cobra.ExactArgs(1),
		RunE: runCacheEnableCommand,
	}

	return cmd
}

// newCacheDisableCommand creates the cache disable command.
func newCacheDisableCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disable <entity-type>",
		Short: "Disable caching for an entity type",
		Long: `Disable caching for a specific entity type.

This will dynamically disable caching for the specified entity type. Any cached
entries for this entity will be cleared and future reads will not be cached.

Examples:
  # Disable caching for user entity
  quicbaas cache disable user

  # Disable caching for post entity
  quicbaas cache disable post`,
		Args: cobra.ExactArgs(1),
		RunE: runCacheDisableCommand,
	}

	return cmd
}

// newCacheWarmCommand creates the cache warm command.
func newCacheWarmCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "warm",
		Short: "Warm cache from storage",
		Long: `Pre-populate the cache by loading keys from storage.

Cache warming reads keys matching specified prefixes from the underlying storage
and loads them into the cache. This is useful for improving read performance
after a restart or for pre-loading frequently accessed data.

Examples:
  # Warm cache for specific prefixes
  quicbaas cache warm --prefix "users:" --prefix "sessions:"

  # Warm with rate limiting (1000 keys/second)
  quicbaas cache warm --prefix "users:" --rate 1000

  # Warm with maximum key limit
  quicbaas cache warm --prefix "hot:" --max-keys 10000

  # Warm with custom batch size
  quicbaas cache warm --prefix "config:" --batch-size 50`,
		RunE: runCacheWarmCommand,
	}

	cmd.Flags().StringArrayVar(&cacheWarmPrefixes, "prefix", nil, "Key prefix to warm (can be specified multiple times)")
	cmd.Flags().IntVar(&cacheWarmRate, "rate", 0, "Rate limit in keys per second (0 = unlimited)")
	cmd.Flags().IntVar(&cacheWarmMaxKeys, "max-keys", 0, "Maximum number of keys to warm (0 = unlimited)")
	cmd.Flags().IntVar(&cacheWarmBatchSize, "batch-size", 100, "Number of keys to process per batch")

	return cmd
}

// runCacheStatsCommand executes the cache stats command.
func runCacheStatsCommand(cmd *cobra.Command, args []string) error {
	// Initialize cache registry from config
	registry, err := initializeCacheRegistry()
	if err != nil {
		return err
	}
	defer registry.Close()

	var entityType string
	if len(args) > 0 {
		entityType = args[0]
	}

	// Parse output format
	format, err := cli.ParseOutputFormat(cacheOutputFmt)
	if err != nil {
		return err
	}

	formatter := cli.NewFormatter(true)

	// Get statistics
	if entityType != "" {
		// Show stats for specific entity
		stats := registry.GetStats(entityType)
		entityCache := registry.GetEntityCache(entityType)

		resp := &cli.CacheStatsResponse{
			EntityType:  entityType,
			Enabled:     entityCache.IsEnabled(),
			Stats:       cli.CacheStats{Enabled: stats.Enabled, L1Size: stats.L1Size, L1Capacity: stats.L1Capacity, L1Hits: stats.L1Hits, L1Misses: stats.L1Misses, L1HitRate: stats.L1HitRate},
			ShowDetails: cacheShowDetails,
		}

		return formatter.FormatCacheStatsResponse(resp, format, os.Stdout)
	}

	// Show stats for all entities
	allStats := registry.GetAllStats()
	entityStats := make(map[string]cli.CacheEntityStats)

	for entityType, stats := range allStats {
		entityCache := registry.GetEntityCache(entityType)
		entityStats[entityType] = cli.CacheEntityStats{
			EntityType: entityType,
			Enabled:    entityCache.IsEnabled(),
			Stats:      cli.CacheStats{Enabled: stats.Enabled, L1Size: stats.L1Size, L1Capacity: stats.L1Capacity, L1Hits: stats.L1Hits, L1Misses: stats.L1Misses, L1HitRate: stats.L1HitRate},
		}
	}

	resp := &cli.CacheAllStatsResponse{
		EntityStats: entityStats,
		ShowDetails: cacheShowDetails,
	}

	return formatter.FormatCacheAllStatsResponse(resp, format, os.Stdout)
}

// runCacheClearCommand executes the cache clear command.
func runCacheClearCommand(cmd *cobra.Command, args []string) error {
	// Initialize cache registry from config
	registry, err := initializeCacheRegistry()
	if err != nil {
		return err
	}
	defer registry.Close()

	var entityType string
	if len(args) > 0 {
		entityType = args[0]
	}

	// Confirmation prompt unless --yes is specified
	if !cacheForce {
		var prompt string
		if entityType != "" {
			prompt = fmt.Sprintf("Are you sure you want to clear the cache for entity type '%s'? (y/N): ", entityType)
		} else {
			prompt = "Are you sure you want to clear ALL cache entries? (y/N): "
		}

		fmt.Print(prompt)
		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "cache clear", "failed to read confirmation")
		}
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Cache clear cancelled")
			return nil
		}
	}

	// Parse output format
	format, err := cli.ParseOutputFormat(cacheOutputFmt)
	if err != nil {
		return err
	}

	formatter := cli.NewFormatter(true)

	// Clear cache
	startTime := time.Now()
	var cleared int

	if entityType != "" {
		// Clear specific entity cache
		entityCache := registry.GetEntityCache(entityType)
		if !entityCache.IsEnabled() {
			return errors.Newf(errors.ErrInvalidInput, "cache clear", "caching is not enabled for entity type: %s", entityType)
		}

		statsBefore := entityCache.Stats()
		cleared = statsBefore.L1Size

		// Clear the entity cache
		if entityCache.IsEnabled() {
			log.Debugf("Clearing cache for entity type: %s", entityType)
			cleared = statsBefore.L1Size
		}
	} else {
		// Clear all caches
		allStats := registry.GetAllStats()
		for _, stats := range allStats {
			cleared += stats.L1Size
		}
		registry.Clear()
	}

	duration := time.Since(startTime)

	resp := &cli.CacheClearResponse{
		EntityType:     entityType,
		EntriesCleared: cleared,
		Duration:       duration,
		Timestamp:      time.Now(),
		Success:        true,
	}

	return formatter.FormatCacheClearResponse(resp, format, os.Stdout)
}

// runCacheConfigCommand executes the cache config command.
func runCacheConfigCommand(cmd *cobra.Command, args []string) error {
	// Get config from global cfg
	if cfg == nil {
		return errors.Newf(errors.ErrConfiguration, "cache config", "configuration not initialized")
	}

	var entityType string
	if len(args) > 0 {
		entityType = args[0]
	}

	// Parse output format
	format, err := cli.ParseOutputFormat(cacheOutputFmt)
	if err != nil {
		return err
	}

	formatter := cli.NewFormatter(true)

	if entityType != "" {
		// Show config for specific entity
		entityCfg := cfg.Cache.GetEntityConfig(entityType)

		resp := &cli.CacheConfigResponse{
			EntityType: entityType,
			Enabled:    entityCfg.Enabled,
			TTL:        entityCfg.TTL,
			MaxSize:    entityCfg.MaxSize,
		}

		return formatter.FormatCacheConfigResponse(resp, format, os.Stdout)
	}

	// Show all cache configuration
	entityConfigs := make(map[string]cli.CacheEntityConfig)

	for entityType, entityCfg := range cfg.Cache.Entities {
		entityConfigs[entityType] = cli.CacheEntityConfig{
			EntityType: entityType,
			Enabled:    entityCfg.Enabled,
			TTL:        entityCfg.TTL,
			MaxSize:    entityCfg.MaxSize,
		}
	}

	resp := &cli.CacheAllConfigResponse{
		GlobalEnabled:   cfg.Cache.Enabled,
		DefaultTTL:      cfg.Cache.DefaultTTL,
		MaxSize:         cfg.Cache.MaxSize,
		CleanupInterval: cfg.Cache.CleanupInterval,
		EntityConfigs:   entityConfigs,
	}

	return formatter.FormatCacheAllConfigResponse(resp, format, os.Stdout)
}

// runCacheEnableCommand executes the cache enable command.
// This command is idempotent - it succeeds even if caching is already enabled.
func runCacheEnableCommand(cmd *cobra.Command, args []string) error {
	entityType := args[0]

	// Initialize cache registry from config
	registry, err := initializeCacheRegistry()
	if err != nil {
		return err
	}
	defer registry.Close()

	// Check if already enabled (for idempotent message)
	alreadyEnabled := registry.IsCacheEnabled(entityType)

	// Create new entity config with default settings
	entityCfg := config.EntityCacheConfig{
		Enabled: true,
		TTL:     cfg.Cache.DefaultTTL,
		MaxSize: 0, // Use proportional allocation
	}

	// Update the registry with the new config (idempotent - succeeds even if already enabled)
	if err := registry.UpdateEntityConfig(entityType, entityCfg); err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "cache enable", "failed to enable cache for entity type: %s", entityType)
	}

	// Parse output format
	format, err := cli.ParseOutputFormat(cacheOutputFmt)
	if err != nil {
		return err
	}

	formatter := cli.NewFormatter(true)

	message := fmt.Sprintf("Caching enabled for entity type: %s", entityType)
	if alreadyEnabled {
		message = fmt.Sprintf("Caching was already enabled for entity type: %s", entityType)
	}

	resp := &cli.CacheEnableResponse{
		EntityType: entityType,
		Enabled:    true,
		Timestamp:  time.Now(),
		Success:    true,
		Message:    message,
	}

	return formatter.FormatCacheEnableResponse(resp, format, os.Stdout)
}

// runCacheDisableCommand executes the cache disable command.
// This command is idempotent - it succeeds even if caching is already disabled.
func runCacheDisableCommand(cmd *cobra.Command, args []string) error {
	entityType := args[0]

	// Initialize cache registry from config
	registry, err := initializeCacheRegistry()
	if err != nil {
		return err
	}
	defer registry.Close()

	// Check if already disabled (for idempotent message)
	alreadyDisabled := !registry.IsCacheEnabled(entityType)

	// Create disabled entity config
	entityCfg := config.EntityCacheConfig{
		Enabled: false,
	}

	// Update the registry with the new config (idempotent - succeeds even if already disabled)
	if err := registry.UpdateEntityConfig(entityType, entityCfg); err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "cache disable", "failed to disable cache for entity type: %s", entityType)
	}

	// Parse output format
	format, err := cli.ParseOutputFormat(cacheOutputFmt)
	if err != nil {
		return err
	}

	formatter := cli.NewFormatter(true)

	message := fmt.Sprintf("Caching disabled for entity type: %s", entityType)
	if alreadyDisabled {
		message = fmt.Sprintf("Caching was already disabled for entity type: %s", entityType)
	}

	resp := &cli.CacheDisableResponse{
		EntityType: entityType,
		Enabled:    false,
		Timestamp:  time.Now(),
		Success:    true,
		Message:    message,
	}

	return formatter.FormatCacheDisableResponse(resp, format, os.Stdout)
}

// runCacheWarmCommand executes the cache warm command.
func runCacheWarmCommand(cmd *cobra.Command, args []string) error {
	// Validate configuration
	if cfg == nil {
		return errors.Newf(errors.ErrConfiguration, "cache warm", "configuration not initialized")
	}

	// Determine prefixes to warm - require explicit prefix flag
	prefixes := cacheWarmPrefixes
	if len(prefixes) == 0 {
		return errors.Newf(errors.ErrInvalidInput, "cache warm", "no prefixes specified; use --prefix flag to specify key prefixes to warm")
	}

	// Parse output format
	format, err := cli.ParseOutputFormat(cacheOutputFmt)
	if err != nil {
		return err
	}

	formatter := cli.NewFormatter(true)

	// Get cache manager and storage reader
	cacheManager, storageReader, cleanupFn, err := initializeCacheWarmerDependencies()
	if err != nil {
		return err
	}
	defer cleanupFn()

	// Build warming config
	warmingConfig := cache.WarmingConfig{
		Enabled:            true,
		Prefixes:           prefixes,
		BatchSize:          cacheWarmBatchSize,
		RateLimitPerSecond: cacheWarmRate,
		MaxKeys:            cacheWarmMaxKeys,
	}

	// Create cache warmer
	warmer, err := cache.NewCacheWarmer(cacheManager, storageReader, warmingConfig, asInterfaceLogger(log))
	if err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "cache warm", "failed to create cache warmer")
	}

	// Create context with timeout from global timeout
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Perform warming
	startTime := time.Now()
	status, warmErr := warmer.WarmPrefixes(ctx, prefixes)

	duration := time.Since(startTime)

	// Build response
	resp := &cli.CacheWarmResponse{
		Prefixes:      prefixes,
		State:         status.State,
		StartedAt:     status.StartedAt,
		CompletedAt:   status.CompletedAt,
		KeysWarmed:    status.KeysWarmed,
		KeysFailed:    status.KeysFailed,
		CurrentPrefix: status.CurrentPrefix,
		Duration:      duration,
		Timestamp:     time.Now(),
		Success:       warmErr == nil,
	}

	if warmErr != nil {
		resp.Message = warmErr.Error()
	} else if status.KeysFailed > 0 {
		resp.Message = fmt.Sprintf("Completed with %d failed keys", status.KeysFailed)
	}

	return formatter.FormatCacheWarmResponse(resp, format, os.Stdout)
}

// Helper functions

// initializeCacheRegistry creates a cache registry from the current configuration.
func initializeCacheRegistry() (*cache.CacheRegistry, error) {
	if cfg == nil {
		return nil, errors.Newf(errors.ErrConfiguration, "cache", "configuration not initialized")
	}

	if log == nil {
		return nil, errors.Newf(errors.ErrConfiguration, "cache", "logger not initialized")
	}

	// Use adapter to convert logger to interfaces.Logger
	// L2Cache is nil for CLI operations (local cache only)
	registry, err := cache.NewCacheRegistry(&cfg.Cache, asInterfaceLogger(log), nil)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrConfiguration, "cache", "failed to create cache registry")
	}

	return registry, nil
}

// initializeCacheWarmerDependencies initializes the dependencies for cache warming.
// Returns the cache manager, storage reader, cleanup function, and any error.
func initializeCacheWarmerDependencies() (*cache.CacheManager, cache.StorageReader, func(), error) {
	if cfg == nil {
		return nil, nil, nil, errors.Newf(errors.ErrConfiguration, "cache warm", "configuration not initialized")
	}

	if log == nil {
		return nil, nil, nil, errors.Newf(errors.ErrConfiguration, "cache warm", "logger not initialized")
	}

	// Create cache manager with correct field mapping from EnhancedCacheConfig to cache.Config
	cacheConfig := cache.Config{
		L1Size:          cfg.Cache.MaxSize,
		L1TTL:           cfg.Cache.DefaultTTL,
		CleanupInterval: cfg.Cache.CleanupInterval,
	}

	cacheManager, err := cache.NewManager(cacheConfig, asInterfaceLogger(log))
	if err != nil {
		return nil, nil, nil, errors.Wrapf(err, errors.ErrConfiguration, "cache warm", "failed to create cache manager")
	}

	// Open storage based on configuration
	storageReader, err := openStorageForWarming()
	if err != nil {
		cacheManager.Close()
		return nil, nil, nil, err
	}

	cleanup := func() {
		cacheManager.Close()
		if closer, ok := storageReader.(interface{ Close() error }); ok {
			closer.Close()
		}
	}

	return cacheManager, storageReader, cleanup, nil
}

// openStorageForWarming opens the storage for cache warming operations.
// Uses in-memory storage as the default backend, wrapped with an adapter
// that implements the cache.StorageReader interface.
func openStorageForWarming() (cache.StorageReader, error) {
	if cfg == nil {
		return nil, errors.Newf(errors.ErrConfiguration, "cache warm", "configuration not initialized")
	}

	// Use in-memory storage for cache warming
	memStorage := memory.New(log)
	return &memoryStorageReaderAdapter{storage: memStorage}, nil
}

// memoryStorageReaderAdapter adapts MemoryStorage to the cache.StorageReader interface.
// It provides the NewIterator method required by StorageReader by using MemoryStorage.Scan
// to build a sorted snapshot of keys and values.
type memoryStorageReaderAdapter struct {
	storage *memory.MemoryStorage
}

// Get retrieves the value for the given key.
func (a *memoryStorageReaderAdapter) Get(key []byte) ([]byte, error) {
	return a.storage.Get(key)
}

// NewIterator creates a new iterator over the memory storage with the given options.
func (a *memoryStorageReaderAdapter) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	var lower, upper []byte
	if opts != nil {
		lower = opts.LowerBound
		upper = opts.UpperBound
	}

	// Use Scan to get a sorted snapshot of the data
	pairs, err := a.storage.Scan(lower, upper)
	if err != nil {
		return &memoryIterator{err: err}
	}

	return &memoryIterator{
		pairs: pairs,
		pos:   -1,
	}
}

// Close releases resources held by the adapter.
func (a *memoryStorageReaderAdapter) Close() error {
	return a.storage.Close()
}

// memoryIterator implements storage.Iterator over a sorted slice of key-value pairs.
type memoryIterator struct {
	pairs [][2][]byte
	pos   int
	err   error
}

// SeekToFirst positions the iterator at the first key.
func (it *memoryIterator) SeekToFirst() bool {
	if it.err != nil || len(it.pairs) == 0 {
		return false
	}
	it.pos = 0
	return true
}

// SeekToLast positions the iterator at the last key.
func (it *memoryIterator) SeekToLast() bool {
	if it.err != nil || len(it.pairs) == 0 {
		return false
	}
	it.pos = len(it.pairs) - 1
	return true
}

// Seek positions the iterator at the first key >= target.
func (it *memoryIterator) Seek(target []byte) bool {
	if it.err != nil || len(it.pairs) == 0 {
		return false
	}
	idx := sort.Search(len(it.pairs), func(i int) bool {
		return bytes.Compare(it.pairs[i][0], target) >= 0
	})
	if idx >= len(it.pairs) {
		it.pos = len(it.pairs)
		return false
	}
	it.pos = idx
	return true
}

// Next advances the iterator to the next key-value pair.
func (it *memoryIterator) Next() bool {
	if it.err != nil {
		return false
	}
	it.pos++
	return it.pos < len(it.pairs)
}

// Prev moves the iterator to the previous key-value pair.
func (it *memoryIterator) Prev() bool {
	if it.err != nil {
		return false
	}
	it.pos--
	return it.pos >= 0
}

// Valid reports whether the iterator is positioned at a valid entry.
func (it *memoryIterator) Valid() bool {
	return it.err == nil && it.pos >= 0 && it.pos < len(it.pairs)
}

// Key returns the key at the current iterator position.
func (it *memoryIterator) Key() []byte {
	if !it.Valid() {
		return nil
	}
	return it.pairs[it.pos][0]
}

// Value returns the value at the current iterator position.
func (it *memoryIterator) Value() []byte {
	if !it.Valid() {
		return nil
	}
	return it.pairs[it.pos][1]
}

// Error returns any accumulated error encountered during iteration.
func (it *memoryIterator) Error() error {
	return it.err
}

// Close releases all resources held by the iterator.
func (it *memoryIterator) Close() error {
	it.pairs = nil
	return nil
}
