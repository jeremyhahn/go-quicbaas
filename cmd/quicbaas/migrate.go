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

// Package main provides the QuicBaaS command-line interface.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage/memory"
)

var (
	// Migration flags.
	migrateSourceURL      string
	migrateDestURL        string
	migrateBatchSize      int
	migrateConcurrency    int
	migrateVerify         bool
	migrateDeleteSource   bool
	migrateRateLimit      int
	migrateCheckpoint     time.Duration
	migrateOutputFormat   string
	migrateFollowProgress bool
)

// Global migration manager.
var currentMigration *MigrationManager

// newMigrateCommands creates the migration command group.
func newMigrateCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Data migration operations",
		Long: `Commands for managing data migration between storage systems.

These commands allow you to migrate data from one storage backend to another,
with support for verification, rate limiting, and progress tracking.`,
	}

	// Add subcommands
	cmd.AddCommand(newMigrateStartCommand())
	cmd.AddCommand(newMigrateStatusCommand())
	cmd.AddCommand(newMigrateCancelCommand())

	return cmd
}

// newMigrateStartCommand creates the migrate start command.
func newMigrateStartCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start --source <url> --dest <url>",
		Short: "Start data migration",
		Long: `Start migrating data from a source storage to a destination storage.

The migration can be configured with various options including:
  - Batch size for processing keys in groups
  - Concurrency for parallel workers
  - Data verification after migration
  - Source data deletion after successful migration
  - Rate limiting to control bandwidth usage
  - Checkpoint intervals for progress tracking

Storage URLs can be in the format:
  - memory://

Examples:
  # Basic migration
  quicbaas migrate start --source memory:// --dest memory://

  # Migration with verification and rate limiting
  quicbaas migrate start \
    --source memory:// \
    --dest memory:// \
    --verify \
    --rate-limit 10240 \
    --batch-size 1000

  # Follow migration progress
  quicbaas migrate start \
    --source memory:// \
    --dest memory:// \
    --follow`,
		RunE: runMigrateStart,
	}

	cmd.Flags().StringVar(&migrateSourceURL, "source", "", "Source storage URL (required)")
	cmd.Flags().StringVar(&migrateDestURL, "dest", "", "Destination storage URL (required)")
	cmd.Flags().IntVar(&migrateBatchSize, "batch-size", 1000, "Number of keys per batch")
	cmd.Flags().IntVar(&migrateConcurrency, "concurrency", 4, "Number of parallel workers")
	cmd.Flags().BoolVar(&migrateVerify, "verify", true, "Verify data after migration")
	cmd.Flags().BoolVar(&migrateDeleteSource, "delete-source", false, "Delete source data after successful migration")
	cmd.Flags().IntVar(&migrateRateLimit, "rate-limit", 0, "Rate limit in KB/s (0 = unlimited)")
	cmd.Flags().DurationVar(&migrateCheckpoint, "checkpoint", 30*time.Second, "Checkpoint interval")
	cmd.Flags().StringVar(&migrateOutputFormat, "output", "table", "Output format (json, table)")
	cmd.Flags().BoolVar(&migrateFollowProgress, "follow", false, "Follow migration progress until completion")

	cmd.MarkFlagRequired("source")
	cmd.MarkFlagRequired("dest")

	return cmd
}

// runMigrateStart executes the migrate start command.
func runMigrateStart(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Parse storage URLs and create storage instances
	sourceStorage, err := createStorageFromURL(migrateSourceURL)
	if err != nil {
		return api.NewOperationFailedError("create source storage", err)
	}
	defer sourceStorage.Close()

	destStorage, err := createStorageFromURL(migrateDestURL)
	if err != nil {
		return api.NewOperationFailedError("create destination storage", err)
	}
	defer destStorage.Close()

	// Create migration configuration
	migrationConfig := &MigrationConfig{
		BatchSize:          migrateBatchSize,
		Concurrency:        migrateConcurrency,
		VerifyData:         migrateVerify,
		DeleteSource:       migrateDeleteSource,
		RateLimitKBps:      migrateRateLimit,
		CheckpointInterval: migrateCheckpoint,
	}

	// Create migration manager
	migrationMgr, err := NewMigrationManager(
		sourceStorage,
		destStorage,
		migrationConfig,
		asInterfaceLogger(log),
	)
	if err != nil {
		return api.NewOperationFailedError("create migration manager", err)
	}

	currentMigration = migrationMgr

	fmt.Println("Starting data migration...")
	fmt.Printf("  Source:      %s\n", migrateSourceURL)
	fmt.Printf("  Destination: %s\n", migrateDestURL)
	fmt.Printf("  Batch size:  %d\n", migrateBatchSize)
	fmt.Printf("  Concurrency: %d\n", migrateConcurrency)
	fmt.Printf("  Verify:      %v\n", migrateVerify)
	fmt.Println()

	// Start migration in a goroutine if following progress
	var resultChan chan *MigrationResult
	var errChan chan error

	if migrateFollowProgress {
		resultChan = make(chan *MigrationResult, 1)
		errChan = make(chan error, 1)

		go func() {
			result, err := migrationMgr.Migrate(ctx)
			if err != nil {
				errChan <- err
			} else {
				resultChan <- result
			}
		}()

		// Display progress updates
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case result := <-resultChan:
				ticker.Stop()
				return displayMigrationResult(result)

			case err := <-errChan:
				ticker.Stop()
				return api.NewOperationFailedError("migration", err)

			case <-ticker.C:
				progress := migrationMgr.GetProgress()
				displayMigrationProgress(progress)
			}
		}
	} else {
		// Run synchronously
		result, err := migrationMgr.Migrate(ctx)
		if err != nil {
			return api.NewOperationFailedError("migration", err)
		}

		return displayMigrationResult(result)
	}
}

// newMigrateStatusCommand creates the migrate status command.
func newMigrateStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check migration progress",
		Long: `Check the progress of an ongoing migration operation.

This command displays real-time statistics about the migration including:
  - Number of keys processed
  - Bytes transferred
  - Current rate (keys/second)
  - Elapsed time
  - Estimated remaining time

Examples:
  # Check migration status
  quicbaas migrate status

  # Check migration status with JSON output
  quicbaas migrate status --output json`,
		RunE: runMigrateStatus,
	}

	cmd.Flags().StringVar(&migrateOutputFormat, "output", "table", "Output format (json, table)")

	return cmd
}

// runMigrateStatus executes the migrate status command.
func runMigrateStatus(cmd *cobra.Command, args []string) error {
	if currentMigration == nil {
		return NewNoActiveMigrationError("migrate status")
	}

	progress := currentMigration.GetProgress()

	switch migrateOutputFormat {
	case "json":
		data, err := json.MarshalIndent(progress, "", "  ")
		if err != nil {
			return api.NewMarshalError(err)
		}
		fmt.Println(string(data))

	case "table":
		displayMigrationProgress(progress)

	default:
		return NewUnsupportedOutputFormatError("migrate status", migrateOutputFormat)
	}

	return nil
}

// newMigrateCancelCommand creates the migrate cancel command.
func newMigrateCancelCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cancel",
		Short: "Cancel ongoing migration",
		Long: `Cancel an ongoing migration operation and optionally rollback changes.

This command will stop the migration process. If rollback is enabled,
it will attempt to delete any data that was migrated to the destination.

Warning: Rollback is a best-effort operation and may not be able to
restore the system to its exact previous state.

Examples:
  # Cancel migration
  quicbaas migrate cancel

  # Cancel migration with rollback
  quicbaas migrate cancel --rollback`,
		RunE: runMigrateCancel,
	}

	var rollback bool
	cmd.Flags().BoolVar(&rollback, "rollback", false, "Rollback migrated data")

	return cmd
}

// runMigrateCancel executes the migrate cancel command.
func runMigrateCancel(cmd *cobra.Command, args []string) error {
	if currentMigration == nil {
		return NewNoActiveMigrationError("migrate cancel")
	}

	rollback, _ := cmd.Flags().GetBool("rollback")

	fmt.Println("Cancelling migration...")

	if rollback {
		fmt.Println("Rolling back migrated data...")
		ctx := context.Background()
		if err := currentMigration.Rollback(ctx); err != nil {
			return api.NewOperationFailedError("rollback", err)
		}
		fmt.Println("Rollback completed successfully")
	}

	currentMigration = nil
	fmt.Println("Migration cancelled")

	return nil
}

// Helper functions

// createStorageFromURL creates a storage instance from a URL.
func createStorageFromURL(url string) (interfaces.Storage, error) {
	parts := strings.SplitN(url, "://", 2)
	if len(parts) != 2 {
		return nil, NewInvalidStorageURLError("createStorageFromURL", url)
	}

	scheme := parts[0]

	switch scheme {
	case "memory":
		// Create in-memory storage with wrapper
		memStorage := memory.New(log)
		return NewMemoryStorageAdapter(memStorage), nil

	default:
		return nil, NewUnsupportedStorageSchemeError("createStorageFromURL", scheme)
	}
}

// MemoryStorageAdapter adapts memory.MemoryStorage to interfaces.Storage.
type MemoryStorageAdapter struct {
	mem *memory.MemoryStorage
}

// NewMemoryStorageAdapter creates a new adapter.
func NewMemoryStorageAdapter(mem *memory.MemoryStorage) *MemoryStorageAdapter {
	return &MemoryStorageAdapter{mem: mem}
}

func (a *MemoryStorageAdapter) Get(key []byte) ([]byte, error) {
	return a.mem.Get(key)
}

func (a *MemoryStorageAdapter) Set(key, value []byte) error {
	return a.mem.Set(key, value)
}

func (a *MemoryStorageAdapter) Delete(key []byte) error {
	return a.mem.Delete(key)
}

func (a *MemoryStorageAdapter) GetWithContext(ctx context.Context, key []byte) ([]byte, error) {
	return a.mem.Get(key)
}

func (a *MemoryStorageAdapter) SetWithContext(ctx context.Context, key, value []byte) error {
	return a.mem.Set(key, value)
}

func (a *MemoryStorageAdapter) Batch() storage.Batch {
	// Memory storage doesn't have real batches, return a simple wrapper
	return &memoryBatchAdapter{storage: a.mem}
}

func (a *MemoryStorageAdapter) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	// Create a simple iterator from scan results
	return &memoryIteratorAdapter{storage: a.mem, opts: opts}
}

func (a *MemoryStorageAdapter) Snapshot() (storage.Snapshot, error) {
	return &memorySnapshotAdapter{data: a.mem.Snapshot()}, nil
}

func (a *MemoryStorageAdapter) Compact(start, end []byte) error {
	// Memory storage doesn't need compaction
	return nil
}

func (a *MemoryStorageAdapter) Flush() error {
	// Memory storage doesn't need flushing
	return nil
}

func (a *MemoryStorageAdapter) Close() error {
	return a.mem.Close()
}

// Simple batch implementation for memory storage.
type memoryBatchAdapter struct {
	storage *memory.MemoryStorage
	ops     []func() error
}

func (b *memoryBatchAdapter) Set(key, value []byte) error {
	b.ops = append(b.ops, func() error {
		return b.storage.Set(key, value)
	})
	return nil
}

func (b *memoryBatchAdapter) Delete(key []byte) error {
	b.ops = append(b.ops, func() error {
		return b.storage.Delete(key)
	})
	return nil
}

func (b *memoryBatchAdapter) Commit() error {
	for _, op := range b.ops {
		if err := op(); err != nil {
			return err
		}
	}
	b.ops = nil
	return nil
}

func (b *memoryBatchAdapter) Rollback() error {
	b.ops = nil
	return nil
}

func (b *memoryBatchAdapter) Close() error {
	return b.Rollback()
}

// Simple iterator implementation.
type memoryIteratorAdapter struct {
	storage *memory.MemoryStorage
	opts    *storage.IteratorOptions
	results [][2][]byte
	index   int
}

func (it *memoryIteratorAdapter) SeekToFirst() bool {
	var lowerBound, upperBound []byte
	if it.opts != nil {
		lowerBound = it.opts.LowerBound
		upperBound = it.opts.UpperBound
	}
	results, _ := it.storage.Scan(lowerBound, upperBound)
	it.results = results
	it.index = 0
	return len(it.results) > 0
}

func (it *memoryIteratorAdapter) SeekToLast() bool {
	var lowerBound, upperBound []byte
	if it.opts != nil {
		lowerBound = it.opts.LowerBound
		upperBound = it.opts.UpperBound
	}
	results, _ := it.storage.Scan(lowerBound, upperBound)
	it.results = results
	it.index = len(it.results) - 1
	return len(it.results) > 0
}

func (it *memoryIteratorAdapter) Seek(target []byte) bool {
	var upperBound []byte
	if it.opts != nil {
		upperBound = it.opts.UpperBound
	}
	results, _ := it.storage.Scan(target, upperBound)
	it.results = results
	it.index = 0
	return len(it.results) > 0
}

func (it *memoryIteratorAdapter) Next() bool {
	if it.index+1 < len(it.results) {
		it.index++
		return true
	}
	return false
}

func (it *memoryIteratorAdapter) Prev() bool {
	if it.index > 0 {
		it.index--
		return true
	}
	return false
}

func (it *memoryIteratorAdapter) Valid() bool {
	return it.index >= 0 && it.index < len(it.results)
}

func (it *memoryIteratorAdapter) Key() []byte {
	if it.Valid() {
		return it.results[it.index][0]
	}
	return nil
}

func (it *memoryIteratorAdapter) Value() []byte {
	if it.Valid() {
		return it.results[it.index][1]
	}
	return nil
}

func (it *memoryIteratorAdapter) Error() error {
	return nil
}

func (it *memoryIteratorAdapter) Close() error {
	it.results = nil
	return nil
}

// Simple snapshot implementation.
type memorySnapshotAdapter struct {
	data map[string][]byte
}

func (s *memorySnapshotAdapter) Get(key []byte) ([]byte, error) {
	if val, ok := s.data[string(key)]; ok {
		result := make([]byte, len(val))
		copy(result, val)
		return result, nil
	}
	return nil, NewKeyNotFoundError("snapshot.Get")
}

func (s *memorySnapshotAdapter) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	// Simple iterator over snapshot data
	return &snapshotIteratorAdapter{data: s.data, opts: opts}
}

func (s *memorySnapshotAdapter) Close() error {
	s.data = nil
	return nil
}

type snapshotIteratorAdapter struct {
	data  map[string][]byte
	opts  *storage.IteratorOptions
	keys  []string
	index int
}

func (it *snapshotIteratorAdapter) SeekToFirst() bool {
	it.keys = make([]string, 0, len(it.data))
	for k := range it.data {
		it.keys = append(it.keys, k)
	}
	it.index = 0
	return len(it.keys) > 0
}

func (it *snapshotIteratorAdapter) SeekToLast() bool {
	it.SeekToFirst()
	it.index = len(it.keys) - 1
	return len(it.keys) > 0
}

func (it *snapshotIteratorAdapter) Seek(target []byte) bool {
	it.SeekToFirst()
	// Simple linear search
	for i, k := range it.keys {
		if k >= string(target) {
			it.index = i
			return true
		}
	}
	return false
}

func (it *snapshotIteratorAdapter) Next() bool {
	if it.index+1 < len(it.keys) {
		it.index++
		return true
	}
	return false
}

func (it *snapshotIteratorAdapter) Prev() bool {
	if it.index > 0 {
		it.index--
		return true
	}
	return false
}

func (it *snapshotIteratorAdapter) Valid() bool {
	return it.index >= 0 && it.index < len(it.keys)
}

func (it *snapshotIteratorAdapter) Key() []byte {
	if it.Valid() {
		return []byte(it.keys[it.index])
	}
	return nil
}

func (it *snapshotIteratorAdapter) Value() []byte {
	if it.Valid() {
		return it.data[it.keys[it.index]]
	}
	return nil
}

func (it *snapshotIteratorAdapter) Error() error {
	return nil
}

func (it *snapshotIteratorAdapter) Close() error {
	it.keys = nil
	return nil
}

// displayMigrationProgress displays migration progress.
func displayMigrationProgress(progress *MigrationProgress) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, "\n=== Migration Progress ===")
	fmt.Fprintf(w, "Keys Processed:\t%d\n", progress.KeysProcessed)
	fmt.Fprintf(w, "Bytes Processed:\t%s\n", formatBytes(progress.BytesProcessed))
	fmt.Fprintf(w, "Elapsed Time:\t%s\n", progress.ElapsedTime.Round(time.Second))
	fmt.Fprintf(w, "Current Rate:\t%.2f keys/sec\n", progress.CurrentRate)

	if progress.EstimatedTotal > 0 {
		fmt.Fprintf(w, "Progress:\t%.2f%%\n", progress.PercentComplete)
		fmt.Fprintf(w, "Estimated Remaining:\t%s\n", progress.EstimatedRemaining.Round(time.Second))
	}

	fmt.Fprintln(w)
}

// displayMigrationResult displays migration result.
func displayMigrationResult(result *MigrationResult) error {
	switch migrateOutputFormat {
	case "json":
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return api.NewMarshalError(err)
		}
		fmt.Println(string(data))

	case "table":
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		defer w.Flush()

		fmt.Fprintln(w, "\n=== Migration Complete ===")
		fmt.Fprintf(w, "Keys Processed:\t%d\n", result.KeysProcessed)
		fmt.Fprintf(w, "Keys Failed:\t%d\n", result.KeysFailed)
		fmt.Fprintf(w, "Bytes Transferred:\t%s\n", formatBytes(result.BytesTransferred))
		fmt.Fprintf(w, "Duration:\t%s\n", result.Duration.Round(time.Second))
		fmt.Fprintf(w, "Start Time:\t%s\n", result.StartTime.Format(time.RFC3339))
		fmt.Fprintf(w, "End Time:\t%s\n", result.EndTime.Format(time.RFC3339))

		if len(result.Errors) > 0 {
			fmt.Fprintf(w, "\nErrors:\t%d\n", len(result.Errors))
			for i, err := range result.Errors {
				if i < 10 { // Show first 10 errors
					fmt.Fprintf(w, "  %d.\t%v\n", i+1, err)
				}
			}
			if len(result.Errors) > 10 {
				fmt.Fprintf(w, "  ...\t(%d more errors)\n", len(result.Errors)-10)
			}
		}

		fmt.Fprintln(w)

		if result.KeysFailed > 0 {
			return NewMigrationPartialFailureError("displayMigrationResult", result.KeysFailed)
		}

	default:
		return NewUnsupportedOutputFormatError("displayMigrationResult", migrateOutputFormat)
	}

	return nil
}
